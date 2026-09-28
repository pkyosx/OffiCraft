package main

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// windowBehind is a write by someone else staged in the gap a read-modify-write
// leaves: once the code under test has run the armAfter statement, its next
// BEGIN or write first tries stmt on an independent connection that does not
// wait for locks. Inside a transaction that already holds the write lock the
// attempt is refused at once — there is no gap — and the write is retried in
// the background, where it lands after that transaction ends. Either way the
// write happens exactly once; what a test checks is whether it survived.
type windowBehind struct {
	mu          sync.Mutex
	landedInGap bool
	err         error
	done        chan struct{}
}

func windowWriteBehind(t *testing.T, hook *windowHook, path, armAfter, stmt string, args ...any) *windowBehind {
	t.Helper()
	nowait, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)&_pragma=journal_mode("+sqliteJournalMode+")")
	if err != nil {
		t.Fatalf("open the no-wait writer: %v", err)
	}
	t.Cleanup(func() { nowait.Close() })
	waiting, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		t.Fatalf("open the waiting writer: %v", err)
	}
	t.Cleanup(func() { waiting.Close() })
	b := &windowBehind{done: make(chan struct{})}
	hook.mu.Lock()
	defer hook.mu.Unlock()
	hook.armAfter = armAfter
	hook.fire = func() {
		_, err := nowait.Exec(stmt, args...)
		if err == nil {
			b.mu.Lock()
			b.landedInGap = true
			b.mu.Unlock()
			close(b.done)
			return
		}
		if !strings.Contains(err.Error(), "SQLITE_BUSY") {
			b.mu.Lock()
			b.err = err
			b.mu.Unlock()
			close(b.done)
			return
		}
		go func() {
			defer close(b.done)
			_, err := waiting.Exec(stmt, args...)
			b.mu.Lock()
			b.err = err
			b.mu.Unlock()
		}()
	}
	return b
}

// landed waits for the staged write and reports whether it went in inside the
// gap (true) or only after the transaction under test ended (false).
func (b *windowBehind) landed(t *testing.T) bool {
	t.Helper()
	select {
	case <-b.done:
	case <-time.After(windowRequestDeadline):
		t.Fatalf("the staged write never finished within %s", windowRequestDeadline)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		t.Fatalf("the staged write: %v", b.err)
	}
	return b.landedInGap
}

// A scheduled backup that fails while a watchdog pass is deciding records
// `failed`; the pass must not overwrite it with the healthy verdict it computed
// before that.
func TestABackupFailureRecordedDuringAWatchdogPassSurvivesIt(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	const failed = `{"code":"failed","detail":"scheduled backup failed: disk full — no new retreat point was created",` +
		`"since_ts":1788868800,"checked_ts":1788868800,"newest_backup_ts":1788865200}`
	d, hook, path := windowDAL(t, "split pools")
	dbPath := filepath.Join(t.TempDir(), "officraft.db")
	backupHealthTestWriteBackups(t, dbPath, "officraft-20260908-110000-scheduled.db")
	if err := d.PutSetting(settingBackupWatchdogBaseline, "1788000000.000000"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	if err := d.PutSetting(settingBackupHealth,
		`{"code":"","detail":"","since_ts":0,"checked_ts":1788865200,"newest_backup_ts":1788865200}`); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	behind := windowWriteBehind(t, hook, path, "SELECT value FROM setting",
		`UPDATE setting SET value = ? WHERE key = ?`, failed, settingBackupHealth)

	var err error
	windowWithin(t, "evaluate", func() { _, err = newBackupHealthMonitor(d, dbPath).evaluate(now) })
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	hook.wantFiredOnce(t)
	inGap := behind.landed(t)
	if got := windowSettingText(t, d, settingBackupHealth); got != failed {
		t.Fatalf("stored verdict (failure landed inside the watchdog's gap: %v):\n got %s\nwant %s", inGap, got, failed)
	}
}

func windowSettingText(t *testing.T, d *DAL, key string) string {
	t.Helper()
	if v := windowSettingNow(t, d, key); v != nil {
		return *v
	}
	return "<no row>"
}

// Two first-run kicks racing for the onboarding slot: the one that finds the
// slot free claims it; one whose claim lands after the other's check must not
// overwrite it and install a second time.
func TestAnOnboardingSlotClaimedAfterTheCheckIsNotClaimedAgain(t *testing.T) {
	const other = `{"state":"running","started_at":1700000000,"finished_at":0,"steps":null}`
	d, hook, path := windowDAL(t, "split pools")
	api, _, _, _ := newAPITestServerOn(t, d)
	t.Setenv("OC_NO_ONBOARDING", "")
	var installs atomic.Int32
	run := onboardingRunner{
		installWarden: func(Member) (bootstrapResultDTO, error) {
			installs.Add(1)
			return bootstrapResultDTO{}, errors.New("installer unavailable")
		},
		wardenInstalled: func() bool { return false },
	}
	behind := windowWriteBehind(t, hook, path, "SELECT value FROM setting",
		`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 0) ON CONFLICT (key) DO NOTHING`,
		settingOnboardingReport, other)

	windowWithin(t, "kickFirstRunOnboardingWith", func() { api.kickFirstRunOnboardingWith(run) })

	hook.wantFiredOnce(t)
	if behind.landed(t) {
		if got := windowSettingText(t, d, settingOnboardingReport); got != other {
			t.Fatalf("the slot another kick claimed first was overwritten:\n got %s\nwant %s", got, other)
		}
		time.Sleep(50 * time.Millisecond)
		if n := installs.Load(); n != 0 {
			t.Fatalf("installed %d times on a slot another kick holds, want 0", n)
		}
		return
	}
	report := onboardingTestWaitForTerminalReport(t, api)
	if report.State != onboardingStateFailed || installs.Load() != 1 {
		t.Fatalf("the kick that claimed the slot: report %#v, installs %d; want failed after 1 install",
			report, installs.Load())
	}
}

// A worker released by someone else after the dismissal read it stays released
// as they left it: the dismissal does not release it a second time.
func TestAWorkerReleasedAfterTheDismissalReadItIsNotReleasedAgain(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _, _ := newAPITestServerOn(t, d)
	dalPutTask(t, d, windowOpenTask("T-1"))
	windowBoundWorker(t, d, "T-1")
	dashboard := apiTestListen(t, api, "")
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ? AND kind = 'outsource'",
		`UPDATE member SET roster_status = 'removed', released_ts = 1800000000
		 WHERE id = 'ow-abc123' AND roster_status != 'removed'`)

	windowWithin(t, "dismissOutsourceWorkerByID", func() {
		api.dismissOutsourceWorkerByID("ow-abc123", 1790000000, "owner")
	})

	hook.wantFiredOnce(t)
	inGap := behind.landed(t)
	w, err := d.GetOutsourceWorker("ow-abc123")
	if err != nil || w == nil {
		t.Fatalf("GetOutsourceWorker: %#v, %v", w, err)
	}
	if inGap {
		if w.Status != WorkerStatusReleased || w.ReleasedTS != 1800000000 {
			t.Fatalf("released first by someone else: status %q released_ts %v, want released at 1800000000",
				w.Status, w.ReleasedTS)
		}
		dashboard.wantFrames()
		return
	}
	if w.Status != WorkerStatusReleased || w.ReleasedTS != 1790000000 {
		t.Fatalf("status %q released_ts %v, want released at 1790000000", w.Status, w.ReleasedTS)
	}
}

// Releasing a task's workers reads the workers it flips inside the transaction
// that flips them: one released by someone else in between is neither released
// again nor reported as released by this call.
func TestAWorkerReleasedAfterTheTaskSweepReadItIsNotReleasedAgain(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	dalPutTask(t, d, windowOpenTask("T-1"))
	windowBoundWorker(t, d, "T-1")
	behind := windowWriteBehind(t, hook, path, "linked_task_id = ?",
		`UPDATE member SET roster_status = 'removed', released_ts = 1800000000
		 WHERE id = 'ow-abc123' AND roster_status != 'removed'`)

	var flipped []OutsourceWorker
	var err error
	windowWithin(t, "ReleaseWorkersForTask", func() { flipped, err = d.ReleaseWorkersForTask("T-1", 1790000000) })
	if err != nil {
		t.Fatalf("ReleaseWorkersForTask: %v", err)
	}

	hook.wantFiredOnce(t)
	inGap := behind.landed(t)
	w, err := d.GetOutsourceWorker("ow-abc123")
	if err != nil || w == nil {
		t.Fatalf("GetOutsourceWorker: %#v, %v", w, err)
	}
	wantTS, wantFlipped := 1790000000.0, 1
	if inGap {
		wantTS, wantFlipped = 1800000000.0, 0
	}
	if w.Status != WorkerStatusReleased || w.ReleasedTS != wantTS || len(flipped) != wantFlipped {
		t.Fatalf("released by someone else in the gap: %v; status %q released_ts %v, %d reported released; "+
			"want released_ts %v, %d reported", inGap, w.Status, w.ReleasedTS, len(flipped), wantTS, wantFlipped)
	}
}

// The outsource tick binds a fresh worker to a queued task on the task row as
// it is when the bind lands: a task the owner closed after the tick read it
// stays closed, and no worker is left behind for it.
func TestATaskClosedAfterTheOutsourceTickReadItIsNotAssigned(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _, _ := newAPITestServerOn(t, d)
	dalPutManual(t, d, TaskManual{
		TypeKey: "tm-scheduler", DisplayName: "Scheduler manual", Fields: "[]",
		Assignee: `{"kind":"outsource","runtime":"codex","model":"gpt-5","effort":"high"}`,
	})
	outsourceSchedTestQueuedTask(t, d, "T-1", "tm-scheduler")
	behind := windowWriteBehind(t, hook, path, "FROM task WHERE id = ?", windowClosedSQL, "T-1")

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000100) })

	hook.wantFiredOnce(t)
	behind.landed(t)
	got, err := d.GetTask("T-1")
	if err != nil || got == nil {
		t.Fatalf("GetTask: %#v, %v", got, err)
	}
	if got.Status != TaskStatusDone || got.ClosedTS != 1800000000 || got.ExecutorID != "" {
		t.Fatalf("task after the tick: status %q closed_ts %v executor %q; want done, 1800000000, unbound",
			got.Status, got.ClosedTS, got.ExecutorID)
	}
	workers, err := d.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("ListOutsourceWorkers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("workers minted for a closed task: %d, want 0", len(workers))
	}
}

// windowTickWorker is a worker in the state a test's tick acts on, over the
// windowDAL, with nothing else about it moving.
func windowTickWorker(t *testing.T, d *DAL, set string) (*apiServer, http.Handler, string) {
	t.Helper()
	api, h, _, owner := newAPITestServerOn(t, d)
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
	if _, err := d.wdb.Exec(`UPDATE member SET ` + set + ` WHERE id = 'ow-abc123'`); err != nil {
		t.Fatalf("prepare the worker: %v", err)
	}
	return api, h, owner
}

// A 停止 whose session is confirmed gone is collected by the tick from its list
// read of the worker. An owner 喚醒 that lands after that read stands: the
// collect writes its latch and nothing else.
func TestAnOwnerWakeAfterTheTickReadAStoppedWorkerIsNotUndoneByTheCollect(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d,
		`desired_state = 'offline', stopping_since = 1700000000, stopped_since = 0, last_machine_id = 'm-old'`)
	api.outsourceMu.Lock()
	api.workerOfflineSince["ow-abc123"] = 1700000000
	api.outsourceMu.Unlock()
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE kind = 'outsource'",
		`UPDATE member SET desired_state = 'online', stopping_since = 0, last_machine_id = 'm-new'
		 WHERE id = 'ow-abc123'`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000500) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the owner's wake did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.StoppedSince <= 0 {
		t.Fatalf("premise: the tick did not collect the stop (stopped_since %v)", got.StoppedSince)
	}
	if got.DesiredState != DesiredStateOnline || got.StoppingSince != 0 || got.LastMachineID != "m-new" {
		t.Fatalf("after the collect: desired_state %q stopping_since %v last_machine_id %q; "+
			"want online, 0, m-new (the owner's wake)", got.DesiredState, got.StoppingSince, got.LastMachineID)
	}
}

// A queued 重啟 is spent by the tick once the stop has converged. A release
// that lands after the tick's list read stands: the spend neither revives the
// worker nor rewrites its row.
func TestAWorkerReleasedAfterTheTickReadItDoesNotSpendItsQueuedRestart(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d,
		`desired_state = 'offline', restart_after_stop = 1, stopping_since = 1700000000,
		 stopped_since = 1700000100`)
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE kind = 'outsource'",
		`UPDATE member SET roster_status = 'removed', released_ts = 1800000000 WHERE id = 'ow-abc123'`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000500) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the release did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.RosterStatus != RosterStatusRemoved || got.ReleasedTS != 1800000000 ||
		got.DesiredState != DesiredStateOffline || !got.RestartAfterStop {
		t.Fatalf("after the tick: roster_status %q released_ts %v desired_state %q restart_after_stop %v; "+
			"want removed, 1800000000, offline, still queued", got.RosterStatus, got.ReleasedTS,
			got.DesiredState, got.RestartAfterStop)
	}
}

// CONTROL for the test above: with nothing landing in the gap, the same tick
// spends the queued restart.
func TestATickSpendsAQueuedRestartOnceTheStopHasConverged(t *testing.T) {
	d, _, _ := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d,
		`desired_state = 'offline', restart_after_stop = 1, stopping_since = 1700000000,
		 stopped_since = 1700000100`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000500) })

	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.RosterStatus != RosterStatusActive || got.DesiredState != DesiredStateOnline || got.RestartAfterStop ||
		got.StoppingSince != 0 || got.StoppedSince != 0 || got.LastOp != "start" {
		t.Fatalf("after the tick: roster_status %q desired_state %q restart_after_stop %v stopping_since %v "+
			"stopped_since %v last_op %q; want active, online, spent, 0, 0, start", got.RosterStatus,
			got.DesiredState, got.RestartAfterStop, got.StoppingSince, got.StoppedSince, got.LastOp)
	}
}
