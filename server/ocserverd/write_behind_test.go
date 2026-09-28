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

// The mirror: the watchdog records a verdict while a scheduled backup's outcome
// is being noted; the note must not overwrite it with a verdict it computed
// before that.
func TestAWatchdogVerdictRecordedWhileAnOutcomeIsNotedSurvivesIt(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	const stale = `{"code":"stale","detail":"newest scheduled backup is 13h0m0s old (alarm after 12h0m0s)",` +
		`"since_ts":1788822000,"checked_ts":1788868799,"newest_backup_ts":1788822000}`
	d, hook, path := windowDAL(t, "split pools")
	dbPath := filepath.Join(t.TempDir(), "officraft.db")
	if err := d.PutSetting(settingBackupWatchdogBaseline, "1788000000.000000"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	if err := d.PutSetting(settingBackupHealth,
		`{"code":"","detail":"","since_ts":0,"checked_ts":1788865200,"newest_backup_ts":1788865200}`); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	behind := windowWriteBehind(t, hook, path, "SELECT value FROM setting",
		`UPDATE setting SET value = ? WHERE key = ?`, stale, settingBackupHealth)

	windowWithin(t, "noteScheduledOutcome", func() {
		newBackupHealthMonitor(d, dbPath).noteScheduledOutcome(backupResult{}, errors.New("disk full"), now)
	})

	hook.wantFiredOnce(t)
	inGap := behind.landed(t)
	if got := windowSettingText(t, d, settingBackupHealth); got != stale {
		t.Fatalf("stored verdict (watchdog verdict landed inside the note's gap: %v):\n got %s\nwant %s", inGap, got, stale)
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

// A roster pass stamps a wind-down from the tick's read of the worker. An owner
// stop that lands after that read stands: the pass writes nothing, and the next
// tick decides again on the row as it is.
func TestAnOwnerStopAfterTheTickReadAWorkerIsNotOverwrittenByAContextStamp(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d, `desired_state = 'online', desired_machine_id = 'm-server-self'`)
	session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
	api.gauge.Set("ow-abc123", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE kind = 'outsource'",
		`UPDATE member SET desired_state = 'offline', stopping_since = 1800000000 WHERE id = 'ow-abc123'`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(20000) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the owner's stop did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 ||
		got.RefocusOp != "" || got.RefocusSince != 0 {
		t.Fatalf("after the tick: desired_state %q stopping_since %v refocus_op %q refocus_since %v; "+
			"want offline, 1800000000, no stamp", got.DesiredState, got.StoppingSince, got.RefocusOp, got.RefocusSince)
	}
}

// CONTROL for the test above: with nothing landing in the gap, the same tick
// stamps the context-high wind-down.
func TestATickStampsAContextHighWindDownOnALiveWorker(t *testing.T) {
	d, _, _ := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d, `desired_state = 'online', desired_machine_id = 'm-server-self'`)
	session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
	api.gauge.Set("ow-abc123", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(20000) })

	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.RefocusOp != refocusOpContextHigh || got.RefocusSince != 20000 || got.DesiredState != DesiredStateOnline {
		t.Fatalf("after the tick: refocus_op %q refocus_since %v desired_state %q; want context_high, 20000, online",
			got.RefocusOp, got.RefocusSince, got.DesiredState)
	}
}

// A session's first connect clears its waking badge and records the machine it
// landed on, each from a read of the member. An owner stop that lands after the
// read stands.
func TestConnectEdgeWritesDoNotUndoAnOwnerStopThatLandedAfterTheirRead(t *testing.T) {
	const ownerStop = `UPDATE member SET desired_state = 'offline', stopping_since = 1800000000 WHERE id = 'ow-abc123'`
	for _, tc := range []struct {
		name string
		call func(api *apiServer)
		want func(t *testing.T, got Member)
	}{
		{"first connect clears waking_since", func(api *apiServer) { api.onFirstConnect("ow-abc123") },
			func(t *testing.T, got Member) {
				if got.WakingSince != 0 {
					t.Fatalf("waking_since %v, want cleared", got.WakingSince)
				}
			}},
		{"the landed machine is stamped", func(api *apiServer) { api.stampLandedMachine("ow-abc123", ServerSelfHost) },
			func(t *testing.T, got Member) {
				if got.LastMachineID != ServerSelfHost {
					t.Fatalf("last_machine_id %q, want %q", got.LastMachineID, ServerSelfHost)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, hook, path := windowDAL(t, "split pools")
			api, _, _ := windowTickWorker(t, d,
				`desired_state = 'online', desired_machine_id = 'm-server-self', waking_since = 1700000000`)
			behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?", ownerStop)

			windowWithin(t, tc.name, func() { tc.call(api) })

			hook.wantFiredOnce(t)
			if !behind.landed(t) {
				t.Fatalf("premise: the owner's stop did not land inside the gap")
			}
			got := apiTestMemberRow(t, d, "ow-abc123")
			if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 {
				t.Fatalf("desired_state %q stopping_since %v; want the owner's stop (offline, 1800000000)",
					got.DesiredState, got.StoppingSince)
			}
			tc.want(t, got)
		})
	}
}

// The receipt writers decide from the receipt on the row. A receipt another
// writer lands after that decision is not overwritten by it: the write that
// decided on an older receipt goes first, the newer one stays.
func TestAReceiptLandedAfterTheWorkerReceiptWriterReadIsNotOverwritten(t *testing.T) {
	const newer = `UPDATE member SET last_op = 'start', last_op_ok = 0, last_op_log = '',
		last_op_reason = 'respawn_deferred: a newer attempt', last_op_at = 1800000000 WHERE id = 'ow-abc123'`
	for _, tc := range []struct {
		name  string
		prior string
		call  func(api *apiServer)
	}{
		{"placement-blocked stamp", `last_op = '', last_op_reason = ''`, func(api *apiServer) {
			w := OutsourceWorker{ID: "ow-abc123", Codename: "Contractor"}
			api.stampWorkerPlacementBlocked(&w, "held_down: nothing was started", 1700000000)
		}},
		{"placement-block clear", `last_op = 'start', last_op_ok = 0, last_op_reason = 'respawn_deferred: old',
			last_op_at = 1700000000`, func(api *apiServer) { api.clearWorkerPlacementBlock("ow-abc123") }},
		{"converged-failure clear", `last_op = 'start', last_op_ok = 0, last_op_reason = 'wake_timeout: old',
			last_op_at = 1700000000`, func(api *apiServer) {
			snapshot := OutsourceWorker{ID: "ow-abc123", LastOp: "start", LastOpAt: 1700000000}
			api.clearWorkerConvergedFailureReceipt("ow-abc123", snapshot)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, hook, path := windowDAL(t, "split pools")
			api, _, _ := windowTickWorker(t, d, tc.prior)
			behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ? AND kind = 'outsource'", newer)

			windowWithin(t, tc.name, func() {
				api.outsourceMu.Lock()
				defer api.outsourceMu.Unlock()
				tc.call(api)
			})

			hook.wantFiredOnce(t)
			behind.landed(t)
			got := apiTestMemberRow(t, d, "ow-abc123")
			if got.LastOpReason != "respawn_deferred: a newer attempt" || got.LastOpAt != 1800000000 {
				t.Fatalf("receipt after the writer: reason %q at %v; want the newer attempt at 1800000000",
					got.LastOpReason, got.LastOpAt)
			}
		})
	}
}

// Taking back a close-out latch whose kill went nowhere moves the latch alone:
// anchors another writer set since the collect read the worker stay.
func TestTakingBackACloseOutLatchLeavesTheOtherAnchorsAsTheRowHasThem(t *testing.T) {
	d, _, _ := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d, `desired_state = 'offline', stopping_since = 1700000000, stopped_since = 1700000300`)
	read, err := d.GetOutsourceWorker("ow-abc123")
	if err != nil || read == nil {
		t.Fatalf("GetOutsourceWorker: %v (%v)", read, err)
	}
	if _, err := d.wdb.Exec(`UPDATE member SET stopping_since = 1800000000, refocus_op = 'accelerated_stop'
		WHERE id = 'ow-abc123'`); err != nil {
		t.Fatalf("the later write: %v", err)
	}

	api.outsourceMu.Lock()
	api.restoreWorkerStoppedLatch(read, 0, "stop-offline")
	api.outsourceMu.Unlock()

	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.StoppedSince != 0 || got.StoppingSince != 1800000000 || got.RefocusOp != "accelerated_stop" {
		t.Fatalf("stopped_since %v stopping_since %v refocus_op %q; want 0, 1800000000, accelerated_stop",
			got.StoppedSince, got.StoppingSince, got.RefocusOp)
	}
}

// The roster passes are shared with staff: a staff member the owner stops
// after the reconcile tick read the roster keeps that stop, and the context
// stamp the tick decided on the older read is not written.
func TestAnOwnerStopAfterTheReconcileTickReadAStaffMemberIsNotOverwrittenByAContextStamp(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _, _ := newAPITestServerOn(t, d)
	if _, err := d.wdb.Exec(`UPDATE member SET desired_state = 'online' WHERE id = 'kip'`); err != nil {
		t.Fatalf("prepare kip: %v", err)
	}
	session, err := api.hub.Connect("kip", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
	api.gauge.Set("kip", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
	behind := windowWriteBehind(t, hook, path, "FROM member ORDER BY name",
		`UPDATE member SET desired_state = 'offline', stopping_since = 1800000000 WHERE id = 'kip'`)

	windowWithin(t, "runReconcileTick", func() { api.runReconcileTick(20000) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the owner's stop did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "kip")
	if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 ||
		got.RefocusOp != "" || got.RefocusSince != 0 {
		t.Fatalf("after the tick: desired_state %q stopping_since %v refocus_op %q refocus_since %v; "+
			"want offline, 1800000000, no stamp", got.DesiredState, got.StoppingSince, got.RefocusOp, got.RefocusSince)
	}
}

// The token-expiry pass, like the context pass, decides from the tick's read:
// an owner stop that lands after that read stands and no 停止 is stamped over it.
func TestAnOwnerStopAfterTheTickReadAWorkerIsNotOverwrittenByATokenExpiryStamp(t *testing.T) {
	const now = 1700000000.0
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d, `desired_state = 'online', desired_machine_id = 'm-server-self',
		session_boot_ts = 1699397000`)
	session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE kind = 'outsource'",
		`UPDATE member SET desired_state = 'offline', stopping_since = 1800000000 WHERE id = 'ow-abc123'`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(now) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the owner's stop did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 ||
		got.RefocusOp != "" || got.RefocusSince != 0 {
		t.Fatalf("after the tick: desired_state %q stopping_since %v refocus_op %q refocus_since %v; "+
			"want offline, 1800000000, no stamp", got.DesiredState, got.StoppingSince, got.RefocusOp, got.RefocusSince)
	}
}

// CONTROL for the test above: with nothing in the gap, the same tick stamps the
// token-expiry 停止 an hour before the session's credential runs out.
func TestATickStampsATokenExpiryWindDownOnALiveWorker(t *testing.T) {
	d, _, _ := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d, `desired_state = 'online', desired_machine_id = 'm-server-self',
		session_boot_ts = 1699397000`)
	session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000000) })

	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.RefocusOp != refocusOpTokenExpiry || got.RefocusSince != 1700000000 {
		t.Fatalf("after the tick: refocus_op %q refocus_since %v; want %s at 1700000000",
			got.RefocusOp, got.RefocusSince, refocusOpTokenExpiry)
	}
}

// A connection is recorded as where a member landed only while it matches the
// member's pin. When the owner re-pins the member after the connection was
// judged, it is no longer the member's session on its machine, and
// last_machine_id keeps what it was.
func TestAConnectionJudgedAgainstAPinTheOwnerMovedIsNotRecordedAsTheLanding(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d,
		`desired_state = 'online', desired_machine_id = 'm-server-self', last_machine_id = 'm-before'`)
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?",
		`UPDATE member SET desired_machine_id = 'm-other' WHERE id = 'ow-abc123'`)

	windowWithin(t, "stampLandedMachine", func() { api.stampLandedMachine("ow-abc123", ServerSelfHost) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the re-pin did not land inside the gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if got.DesiredMachineID != "m-other" || got.LastMachineID != "m-before" {
		t.Fatalf("desired_machine_id %q last_machine_id %q; want m-other, m-before", got.DesiredMachineID, got.LastMachineID)
	}
}

// The queued-restart spend belongs to the converged-offline edge. A worker that
// is no longer desired offline when the spend is judged is not spent: no
// "the stop has landed — starting again" receipt, anchors and flag as the row
// has them.
func TestAQueuedRestartIsNotSpentOnAWorkerNoLongerDesiredOffline(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _ := windowTickWorker(t, d,
		`desired_state = 'offline', restart_after_stop = 1, stopping_since = 1700000000,
		 stopped_since = 1700000100`)
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE kind = 'outsource'",
		`UPDATE member SET desired_state = 'online' WHERE id = 'ow-abc123'`)

	windowWithin(t, "runOutsourceTick", func() { api.runOutsourceTick(1700000500) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the write did not land inside the tick's gap")
	}
	got := apiTestMemberRow(t, d, "ow-abc123")
	if !got.RestartAfterStop || strings.HasPrefix(got.LastOpReason, spawnReasonHeldDown+":") ||
		got.StoppingSince != 1700000000 || got.StoppedSince != 1700000100 {
		t.Fatalf("restart_after_stop %v last_op_reason %q stopping_since %v stopped_since %v; "+
			"want still queued, no held_down spend receipt, 1700000000, 1700000100",
			got.RestartAfterStop, got.LastOpReason, got.StoppingSince, got.StoppedSince)
	}
}

// windowStaff is the plain staff member Kip in the state a test acts on, over
// the windowDAL, with nothing else about him moving.
func windowStaff(t *testing.T, d *DAL, set string) *apiServer {
	t.Helper()
	api, _, _, _ := newAPITestServerOn(t, d)
	if _, err := d.wdb.Exec(`UPDATE member SET ` + set + ` WHERE id = '` + apiTestPlainAgentID + `'`); err != nil {
		t.Fatalf("prepare the member: %v", err)
	}
	return api
}

func windowConnect(t *testing.T, api *apiServer, memberID, machineID string) {
	t.Helper()
	session, err := api.hub.Connect(memberID, machineID)
	if err != nil {
		t.Fatalf("hub.Connect(%q): %v", memberID, err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
}

// An instant reconcile of a staff member decides from its read of the row and
// then stamps it. Whatever another writer lands after that read stands: each
// stamp is judged on, and written onto, the row as it is when the stamp lands.
func TestAStaffReconcileStampDoesNotUndoAWriteThatLandedAfterItsRead(t *testing.T) {
	const ownerStop = `UPDATE member SET desired_state = 'offline', stopping_since = 1800000000 WHERE id = 'kip'`
	for _, tc := range []struct {
		name    string
		prepare string
		setup   func(t *testing.T, api *apiServer)
		behind  string
		want    func(t *testing.T, got Member)
	}{
		{
			name:    "a queued restart is not spent on a member dismissed since the read",
			prepare: `desired_state = 'offline', restart_after_stop = 1, stopping_since = 1700000000, stopped_since = 1700000100`,
			behind:  `UPDATE member SET roster_status = 'removed', released_ts = 1800000000 WHERE id = 'kip'`,
			want: func(t *testing.T, got Member) {
				if got.RosterStatus != RosterStatusRemoved || got.ReleasedTS != 1800000000 ||
					got.DesiredState != DesiredStateOffline || !got.RestartAfterStop || got.LastOp != "" {
					t.Fatalf("roster_status %q released_ts %v desired_state %q restart_after_stop %v last_op %q; "+
						"want removed, 1800000000, offline, still queued, no receipt", got.RosterStatus,
						got.ReleasedTS, got.DesiredState, got.RestartAfterStop, got.LastOp)
				}
			},
		},
		{
			name:    "a relocation wind-down is not opened on a member the owner stopped since the read",
			prepare: `desired_state = 'online', desired_machine_id = 'm-box'`,
			setup: func(t *testing.T, api *apiServer) {
				reconcileTestPut(t, api.dal, Member{ID: "m-box", Name: "Box", Kind: KindWarden})
				windowConnect(t, api, "kip", ServerSelfHost)
			},
			behind: ownerStop,
			want: func(t *testing.T, got Member) {
				if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 ||
					got.RefocusSince != 0 || got.RefocusOp != "" {
					t.Fatalf("desired_state %q stopping_since %v refocus_since %v refocus_op %q; "+
						"want offline, 1800000000, no wind-down", got.DesiredState, got.StoppingSince,
						got.RefocusSince, got.RefocusOp)
				}
			},
		},
		{
			name:    "a back-off stamp yields to a wake timeout recorded since the read",
			prepare: `desired_state = 'online'`,
			setup: func(t *testing.T, api *apiServer) {
				api.setReconcileState("kip", reconcileState{BackoffUntil: 4000000000})
			},
			behind: `UPDATE member SET last_op = 'start', last_op_ok = 0, last_op_log = '',
				last_op_reason = 'wake_timeout: a newer attempt', last_op_at = 1800000000 WHERE id = 'kip'`,
			want: func(t *testing.T, got Member) {
				if got.LastOpReason != "wake_timeout: a newer attempt" || got.LastOpAt != 1800000000 {
					t.Fatalf("receipt: reason %q at %v; want the wake timeout at 1800000000",
						got.LastOpReason, got.LastOpAt)
				}
			},
		},
		{
			name:    "a placement stamp names the machine the owner chose since the read",
			prepare: `desired_state = 'online', desired_machine_id = 'm-gone'`,
			behind:  `UPDATE member SET desired_machine_id = 'm-other' WHERE id = 'kip'`,
			want: func(t *testing.T, got Member) {
				const want = "machine_unavailable: machine 'm-other' is not an active machine — " +
					"choose another one (改機器); no other machine is substituted"
				if got.LastOp != "start" || got.LastOpReason != want {
					t.Fatalf("receipt: last_op %q reason %q; want start, %q", got.LastOp, got.LastOpReason, want)
				}
			},
		},
		{
			name:    "a wake timeout is stamped without undoing an owner stop that landed since the read",
			prepare: `desired_state = 'online', waking_since = 1700000000`,
			setup: func(t *testing.T, api *apiServer) {
				api.setReconcileState("kip", reconcileState{LastCommand: reconcileCmdStart, LastCommandAt: 1700000000})
			},
			behind: ownerStop,
			want: func(t *testing.T, got Member) {
				const reason = "wake_timeout: the START was dispatched but the agent never came online within " +
					"the start window — check that claude runs and is logged in on the target machine " +
					"(warden log: ocwarden.out.log)"
				if got.DesiredState != DesiredStateOffline || got.StoppingSince != 1800000000 ||
					got.WakingSince != 0 || got.LastOp != "start" || got.LastOpReason != reason {
					t.Fatalf("desired_state %q stopping_since %v waking_since %v last_op %q reason %q; "+
						"want offline, 1800000000, 0, start, %q", got.DesiredState, got.StoppingSince,
						got.WakingSince, got.LastOp, got.LastOpReason, reason)
				}
			},
		},
		{
			name:    "a converged member's failed receipt is not cleared over a success recorded since the read",
			prepare: `desired_state = 'online', last_op = 'start', last_op_ok = 0, last_op_reason = 'wake_timeout: old', last_op_at = 1700000000`,
			setup: func(t *testing.T, api *apiServer) {
				windowConnect(t, api, "kip", "")
			},
			behind: `UPDATE member SET last_op = 'start', last_op_ok = 1, last_op_log = '', last_op_reason = '',
				last_op_at = 1800000000 WHERE id = 'kip'`,
			want: func(t *testing.T, got Member) {
				if got.LastOp != "start" || got.LastOpOK == nil || !*got.LastOpOK || got.LastOpAt != 1800000000 {
					t.Fatalf("receipt: last_op %q ok %v at %v; want the success at 1800000000",
						got.LastOp, got.LastOpOK, got.LastOpAt)
				}
			},
		},
		{
			name:    "an unset runtime is not resolved over the runtime the owner chose since the read",
			prepare: `desired_state = 'online', desired_machine_id = 'm-box', runtime = ''`,
			setup: func(t *testing.T, api *apiServer) {
				reconcileTestPut(t, api.dal, Member{ID: "m-box", Name: "Box", Kind: KindWarden})
				api.telemetry.Set("m-box", map[string]any{"runtimes": map[string]any{
					"claude": map[string]any{"installed": true, "logged_in": true},
				}})
				windowConnect(t, api, "m-box", "")
			},
			behind: `UPDATE member SET runtime = 'codex' WHERE id = 'kip'`,
			want: func(t *testing.T, got Member) {
				if got.Runtime != RuntimeCodex {
					t.Fatalf("runtime %q, want codex (the owner's choice)", got.Runtime)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, hook, path := windowDAL(t, "split pools")
			api := windowStaff(t, d, tc.prepare)
			if tc.setup != nil {
				tc.setup(t, api)
			}
			behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?", tc.behind)

			windowWithin(t, "reconcileMemberNow", func() { api.reconcileMemberNow("kip") })

			hook.wantFiredOnce(t)
			if !behind.landed(t) {
				t.Fatalf("premise: the other write did not land between the reconcile's read and its stamp")
			}
			tc.want(t, apiTestMemberRow(t, d, "kip"))
		})
	}
}

// First-run onboarding wakes the seeded assistant from its read of her row. A
// dismissal that lands after that read stands.
func TestOnboardingDoesNotReviveAnAssistantDismissedAfterItsRead(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api, _, _, _ := newAPITestServerOn(t, d)
	api.noReconcile = true
	run := onboardingRunner{
		wardenInstalled: func() bool { return true },
		wardenOnline:    func(string) bool { return true },
		sleep:           func(time.Duration) {},
		now:             func() float64 { return 100 },
		waitBudget:      time.Second,
	}
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?",
		`UPDATE member SET roster_status = 'removed', released_ts = 1800000000 WHERE id = ?`, seedMiraID)

	windowWithin(t, "runFirstRunOnboarding", func() {
		api.runFirstRunOnboarding(run, onboardingReportDTO{StartedAt: 10})
	})

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the dismissal did not land between onboarding's read and its wake")
	}
	got := apiTestMemberRow(t, d, seedMiraID)
	if got.RosterStatus != RosterStatusRemoved || got.ReleasedTS != 1800000000 {
		t.Fatalf("assistant after onboarding: roster_status %q released_ts %v; want removed, 1800000000",
			got.RosterStatus, got.ReleasedTS)
	}
}

// A lapsed receipt is stamped on a staff member by the tick. The member delta
// that follows describes the row as it is once the stamp lands, including an
// owner 活化 written after the stamp's read.
func TestAReceiptMissingStampPublishesTheRowAsItIsAfterTheStamp(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api := windowStaff(t, d, `desired_state = 'offline'`)
	api.armReceiptWatch("kip", reconcileCmdStop, ServerSelfHost, 1700000000)
	dashboard := apiTestListen(t, api, "")
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?",
		`UPDATE member SET desired_state = 'online', stopping_since = 0 WHERE id = 'kip'`)

	windowWithin(t, "runReconcileTick", func() { api.runReconcileTick(1700000100) })

	hook.wantFiredOnce(t)
	if !behind.landed(t) {
		t.Fatalf("premise: the 活化 did not land between the stamp's read and its write")
	}
	got := apiTestMemberRow(t, d, "kip")
	const reason = "receipt_missing: the stop was handed to machine \"m-server-self\" but no receipt came " +
		"back within 90s — the op may or may not have run; this row's last state is UNKNOWN, not failed. " +
		"Suspect the machine's link to the server (the receipt POST) before suspecting the op itself"
	if got.DesiredState != DesiredStateOnline || got.LastOp != "stop" || got.LastOpReason != reason {
		t.Fatalf("row: desired_state %q last_op %q reason %q; want online, stop, %q",
			got.DesiredState, got.LastOp, got.LastOpReason, reason)
	}
	dashboard.wantFrames(apiTestMemberFrame(1, "patch", "kip",
		apiTestMemberPayload("kip", "Kip", "active", "online"), "server"))
}

// A reconnect restores the durable session anchor rather than minting one. An
// anchor stored after the connect's read (a refused START put it back) is not
// overwritten by a newly minted one.
func TestASessionAnchorStoredAfterTheConnectReadIsNotOverwritten(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api := windowStaff(t, d, `session_boot_ts = 0`)
	api.gauge.Set("kip", map[string]any{"boot_ts": 1750000000.0})
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?",
		`UPDATE member SET session_boot_ts = 1700000000 WHERE id = 'kip'`)

	windowWithin(t, "anchorSessionBoot", func() { api.anchorSessionBoot("kip") })

	hook.wantFiredOnce(t)
	behind.landed(t)
	if got := apiTestMemberRow(t, d, "kip").SessionBootTS; got != 1700000000 {
		t.Fatalf("session_boot_ts %v, want 1700000000 (the anchor stored after the read)", got)
	}
}

// A refused START puts the old session's anchor back and moves a notice claim
// taken on a newer anchor with it. A newer anchor and its claim written after
// the restore read the row are not split by it: the row keeps an anchor and a
// claim on that same anchor.
func TestARefusedStartRestoreDoesNotSplitAnAnchorFromItsNoticeClaim(t *testing.T) {
	d, hook, path := windowDAL(t, "split pools")
	api := windowStaff(t, d, `session_boot_ts = 0, handover_noticed_ts = 0`)
	api.startClearedAnchorsMu.Lock()
	api.startClearedAnchors = map[string]sessionAnchorSnapshot{"kip": {bootTS: 1700000000, gauge: map[string]any{}}}
	api.startClearedAnchorsMu.Unlock()
	behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?",
		`UPDATE member SET session_boot_ts = 1750000000, handover_noticed_ts = 1750000000 WHERE id = 'kip'`)
	refused := false

	windowWithin(t, "restoreRefusedStartAnchor", func() {
		api.restoreRefusedStartAnchor("kip", reconcileCmdStart, &refused, spawnClobberReasonPrefix+" a live session holds the slot")
	})

	hook.wantFiredOnce(t)
	inGap := behind.landed(t)
	got := apiTestMemberRow(t, d, "kip")
	if got.SessionBootTS != got.HandoverNoticedTS {
		t.Fatalf("session_boot_ts %v handover_noticed_ts %v (newer anchor landed inside the gap: %v); "+
			"want the claim on the anchor the row holds", got.SessionBootTS, got.HandoverNoticedTS, inGap)
	}
}

// windowWarden is a second machine, m-box, with the given desired state, over
// the windowDAL. The owner token comes back for the machine routes.
func windowWarden(t *testing.T, d *DAL, desired string) (*apiServer, http.Handler, string) {
	t.Helper()
	api, h, _, owner := newAPITestServerOn(t, d)
	reconcileTestPut(t, d, Member{ID: "m-box", Name: "Box", Kind: KindWarden, DesiredState: desired})
	return api, h, owner
}

// A warden's one-shot uninstall intent is folded back to offline once the
// warden is gone. A delete that lands after the fold's read stands: the machine
// is not put back on the roster.
func TestAMachineDeletedAfterTheUninstallFoldReadItStaysDeleted(t *testing.T) {
	const deleted = `UPDATE member SET roster_status = 'removed', desired_state = 'offline' WHERE id = 'm-box'`
	for _, tc := range []struct {
		name     string
		armAfter string
		call     func(api *apiServer)
	}{
		{"the tick's roster pass", "FROM member ORDER BY name", func(api *apiServer) { api.runReconcileTick(1700000000) }},
		{"the disconnect edge", "FROM member WHERE id = ?", func(api *apiServer) { api.consumeUninstallOnDisconnect("m-box") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, hook, path := windowDAL(t, "split pools")
			api, _, _ := windowWarden(t, d, DesiredStateUninstall)
			behind := windowWriteBehind(t, hook, path, tc.armAfter, deleted)

			windowWithin(t, tc.name, func() { tc.call(api) })

			hook.wantFiredOnce(t)
			behind.landed(t)
			got := apiTestMemberRow(t, d, "m-box")
			if got.RosterStatus != RosterStatusRemoved || got.DesiredState != DesiredStateOffline {
				t.Fatalf("roster_status %q desired_state %q; want removed, offline", got.RosterStatus, got.DesiredState)
			}
		})
	}
}

// The machine routes that clear a residual uninstall or delete a machine read
// it first. A session anchor its connect edge stored after that read stands.
func TestMachineRoutesDoNotUndoAConnectEdgeThatLandedAfterTheirRead(t *testing.T) {
	const connected = `UPDATE member SET session_boot_ts = 1800000000 WHERE id = 'm-box'`
	for _, tc := range []struct {
		name    string
		desired string
		method  string
		target  string
		status  int
		roster  string
	}{
		{"the boot command clears a residual uninstall", DesiredStateUninstall, "GET", "/api/machines/m-box/boot-command", 200, RosterStatusActive},
		{"a delete removes the machine", DesiredStateOffline, "DELETE", "/api/machines/m-box", 200, RosterStatusRemoved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, hook, path := windowDAL(t, "split pools")
			_, h, owner := windowWarden(t, d, tc.desired)
			behind := windowWriteBehind(t, hook, path, "FROM member WHERE id = ?", connected)

			rec := windowRequest(t, h, tc.method, tc.target, owner, "")

			if rec.Code != tc.status {
				t.Fatalf("%s %s: status %d, want %d (%s)", tc.method, tc.target, rec.Code, tc.status, rec.Body.String())
			}
			hook.wantFiredOnce(t)
			if !behind.landed(t) {
				t.Fatalf("premise: the connect edge did not land between the route's read and its write")
			}
			got := apiTestMemberRow(t, d, "m-box")
			if got.SessionBootTS != 1800000000 || got.DesiredState != DesiredStateOffline || got.RosterStatus != tc.roster {
				t.Fatalf("session_boot_ts %v desired_state %q roster_status %q; want 1800000000, offline, %s",
					got.SessionBootTS, got.DesiredState, got.RosterStatus, tc.roster)
			}
		})
	}
}
