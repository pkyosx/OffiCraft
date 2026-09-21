package main

// backup_health.go — the COCKPIT-VISIBLE half of the backup engine (T-da06).
//
// WHY this exists: backup.go already reports every outcome — but only through
// `log.Printf`, into a file that nothing on this machine aggregates and the
// cockpit cannot read. So "the schedule died three days ago" and "everything is
// fine" looked IDENTICAL to the owner. T-ada9's own filing said it: a backup
// that fails silently is worse than no backup, because it makes someone believe
// they have a retreat. Adding another log line would have been free and useless
// — that file has zero readers.
//
// The three facts this module makes visible, and why each one is here:
//
//   ① this run FAILED / was skipped        — no new retreat point was created.
//   ② the newest backup is STALE           — the schedule may have stopped.
//   ③ it NEVER RAN                         — a cadence that never armed and one
//                                            that silently died are the SAME
//                                            fact to whoever reaches for a
//                                            backup. ③ is the one a "did the
//                                            last run fail?" design misses
//                                            entirely, so it is a first-class
//                                            state here, not an afterthought.
//
// 🔴 Two structural decisions worth keeping:
//
//   - The watchdog is its OWN goroutine, armed by cmdServe, NOT something the
//     backup routine calls on its way past. If it hung off the backup path, a
//     cadence that never started would never be checked — which is precisely
//     failure ③, the one nothing else can see.
//   - Health state is DURABLE (settings rows), not in-memory. A server restart
//     must not turn "broken for three days" back into green, and the owner
//     dismissing anything must not either. Only a scheduled backup actually
//     landing clears it.
//
// 🔴 Only SCHEDULED backups count as evidence that the schedule is alive. A
// manual snapshot or a pre-migration one lands in the same directory, and if
// they counted, someone taking a backup by hand — or an upgrade — would paper
// over a dead cadence for another full window.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// settingBackupWatchdogBaseline is when the watchdog FIRST armed on this
	// installation (write-once, durable). It is the clock that "never ran"
	// measures against: without it, a fresh install would either alarm
	// immediately (before the first cadence tick could possibly have run) or —
	// far worse — need a "have we been up long enough?" judgement made against
	// process start time, which every restart would reset, so an installation
	// whose backup never worked would never accumulate enough uptime to say so.
	settingBackupWatchdogBaseline = "backup.watchdog_baseline_ts"

	// settingBackupHealth is the durable health verdict (JSON, see
	// backupHealthState). Absent = never evaluated = unknown.
	settingBackupHealth = "backup.health"

	// backupWatchdogCadence is how often the watchdog re-evaluates. It is
	// unrelated to how often backups are taken: it must keep ticking precisely
	// when the backup loop is NOT.
	backupWatchdogCadence = 5 * time.Minute
)

// Health vocabulary. Closed sets, spelled once, shared by the DTO and the
// cockpit.
const (
	backupHealthHealthy   = "healthy"
	backupHealthUnhealthy = "unhealthy"
	backupHealthUnknown   = "unknown"

	backupHealthCodeNeverRan = "never_ran"
	backupHealthCodeStale    = "stale"
	backupHealthCodeFailed   = "failed"
)

// backupStaleAfter is the one place the alarm window is derived. Spelled as a
// function so no reader ever hand-writes "12h" beside it: the window IS
// backupStaleFactor × backupInterval, and it must follow those constants if
// either ever changes.
func backupStaleAfter() time.Duration { return backupStaleFactor * backupInterval }

// backupHealthState is the durable verdict. Code == "" means healthy; the
// absence of the row entirely means unknown (never evaluated), which is a
// DIFFERENT thing and must never be folded into healthy.
type backupHealthState struct {
	Code           string  `json:"code"`
	Detail         string  `json:"detail"`
	SinceTS        float64 `json:"since_ts"`         // when the CURRENT incident started; 0 while healthy
	CheckedTS      float64 `json:"checked_ts"`       // when the watchdog last evaluated
	NewestBackupTS float64 `json:"newest_backup_ts"` // newest SCHEDULED backup seen; 0 = none
}

// backupHealthStore is the durable seam. It is an interface for exactly one
// reason: the tests for this file must be able to manufacture a FAILING backup
// without any possibility of touching a real database or the live server root
// (repo rule: a test must not depend on the thing it is testing to stay safe).
type backupHealthStore interface {
	GetSetting(key string) (*string, error)
	PutSetting(key, value string) error
}

// backupHealthMonitor owns the durable verdict. One per server.
type backupHealthMonitor struct {
	store  backupHealthStore
	dbPath string

	mu sync.Mutex
}

func newBackupHealthMonitor(store backupHealthStore, dbPath string) *backupHealthMonitor {
	return &backupHealthMonitor{store: store, dbPath: dbPath}
}

// baselineAt returns the durable watchdog baseline, arming it at `now` the
// first time. Write-once: a restart must not push the "never ran" deadline out
// again, or an installation whose cadence never worked would never reach it.
func (m *backupHealthMonitor) baselineAt(now time.Time) (time.Time, error) {
	raw, err := m.store.GetSetting(settingBackupWatchdogBaseline)
	if err != nil {
		return time.Time{}, err
	}
	if raw != nil {
		// strconv, NOT fmt.Sscanf: Sscanf accepts a numeric PREFIX, so a
		// corrupted "1785600000junk" would parse and be trusted. A baseline is
		// what "never ran" is measured against — half-reading one is worse than
		// re-arming.
		if ts, err := strconv.ParseFloat(strings.TrimSpace(*raw), 64); err == nil && ts > 0 {
			baseline := time.Unix(0, int64(ts*float64(time.Second)))
			// 🔴 A baseline in the FUTURE is the same class of unusable value as
			// a corrupt one, and it fails in the silent direction: "never ran"
			// is `now.Sub(baseline) > backupStaleAfter()`, which is FALSE for
			// every negative value, so a clock that stepped backwards after this
			// row was written means the never-ran alarm can NEVER fire again on
			// this installation. Re-arm it (write-once is about restarts pushing
			// the deadline out, not about honouring a measuring stick that
			// points the wrong way): the cost is bounded at one more grace
			// window, and it HEALS the durable row instead of re-deciding every
			// pass. Clamping in decideBackupHealth would leave the bogus value
			// in the database forever.
			if !baseline.After(now) {
				return baseline, nil
			}
		}
		// A corrupt baseline is re-armed rather than trusted: an unparseable
		// value must not silently become "epoch", which would read as "this
		// installation has been failing since 1970" on every fresh install.
	}
	if err := m.store.PutSetting(settingBackupWatchdogBaseline, fmt.Sprintf("%f", float64(now.UnixNano())/float64(time.Second))); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// load reads the durable verdict. (nil, nil) = never evaluated.
func (m *backupHealthMonitor) load() (*backupHealthState, error) {
	raw, err := m.store.GetSetting(settingBackupHealth)
	if err != nil || raw == nil {
		return nil, err
	}
	var st backupHealthState
	if err := json.Unmarshal([]byte(*raw), &st); err != nil {
		// report() renders this as unknown — never healthy. evaluate() does
		// NOT preserve it: see the honest boundary noted there.
		return nil, err
	}
	return &st, nil
}

func (m *backupHealthMonitor) save(st backupHealthState) error {
	blob, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return m.store.PutSetting(settingBackupHealth, string(blob))
}

// newestScheduledBackup reports the newest SCHEDULED backup that had ALREADY
// HAPPENED as of `now`.
//
// Manual and pre-migration snapshots are deliberately invisible here — see the
// file header: counting them would let a human taking a snapshot, or an
// upgrade, hide a dead cadence.
//
// 🔴 A STAMP IN THE FUTURE IS NOT EVIDENCE OF ANYTHING, and skipping it here is
// the whole repair for a silent, total backup outage. Every caller of this
// function subtracts the answer from a clock, and `now.Sub(future) < interval`
// is TRUE for every negative value, so one future-stamped file made
// backupTick answer "just backed up" (⇒ never backs up again) and
// decideBackupHealth answer "very fresh" (⇒ green). Backups stop and the
// cockpit stays green — an under-report, strictly worse than the over-report
// this ticket was opened for, because nothing ever calls out. An NTP
// correction, a restored VM snapshot or a dead RTC all produce it.
//
// 🔴 WHY THE FILTER LIVES HERE AND NOWHERE ELSE. This ticket exists because
// "when did the schedule last run?" had TWO approximate implementations. Adding
// a second future-check inside decideBackupHealth (or in backupTick) would
// recreate exactly that. There are four production callers — backupTick and
// three in this file — and fixing the shared question fixes all four at once
// and keeps them unable to disagree. The two sides are held by SEPARATE tests
// driving SEPARATE entry points, so coverage is per-side even though the
// implementation is single.
//
// 🔴 WHY SKIP RATHER THAN "TREAT AS INFINITELY OLD". Calling a future stamp
// ancient sounds conservative and is a trap: the tick would back up, but the
// bogus file is STILL the newest one, so the next tick 15 minutes later would
// back up again, and again, for as long as the clock is behind — filling the
// disk and rotating the real history out of the routine pool within one
// interval. Skipping converges instead: the snapshot this tick writes is
// stamped `now`, is therefore not in the future, and immediately becomes the
// answer. Cost of the whole incident: ONE extra snapshot.
//
// There is deliberately no skew grace. A grace re-opens the starvation window
// by exactly its own width, and its width is a number nobody can calibrate;
// meanwhile the cost of having none is bounded at that same one extra snapshot,
// which is the direction this module always errs in.
func newestScheduledBackup(dbPath string, now time.Time) (time.Time, bool) {
	files, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		return time.Time{}, false
	}
	for _, f := range files { // already newest-first
		if backupReasonIn(f.Name()) != backupReasonScheduled {
			continue
		}
		ts, ok := parseBackupStamp(f.Name())
		if !ok || ts.After(now) {
			continue
		}
		return ts, true
	}
	return time.Time{}, false
}

// decideBackupHealth is the whole verdict, as a pure function of the facts. It
// takes no clock, no filesystem and no database so the decision itself can be
// tested exhaustively, including the branch that has historically been
// impossible to reach by waiting: "a previous backup exists but is too old".
//
// lastFailureTS is when a scheduled attempt last reported failure/skip (0 =
// none). It only wins while it is NEWER than the newest scheduled backup —
// otherwise a failure from last week would keep a working cadence red.
func decideBackupHealth(now, baseline time.Time, newest time.Time, hasNewest bool, lastFailureTS float64, lastFailureDetail string) backupHealthState {
	st := backupHealthState{CheckedTS: epochOf(now)}
	if hasNewest {
		st.NewestBackupTS = epochOf(newest)
	}

	if lastFailureTS > 0 && (!hasNewest || lastFailureTS > epochOf(newest)) {
		st.Code, st.Detail = backupHealthCodeFailed, lastFailureDetail
		return st
	}
	if !hasNewest {
		// Before the grace window is up this is not yet evidence of anything —
		// but it is NOT healthy either: there is no retreat point. The caller
		// renders this as `unknown`, never green.
		if now.Sub(baseline) > backupStaleAfter() {
			st.Code = backupHealthCodeNeverRan
			st.Detail = fmt.Sprintf("no scheduled backup has ever landed (watching for %s)", now.Sub(baseline).Round(time.Minute))
		}
		return st
	}
	if age := now.Sub(newest); age > backupStaleAfter() {
		st.Code = backupHealthCodeStale
		st.Detail = fmt.Sprintf("newest scheduled backup is %s old (alarm after %s)", age.Round(time.Minute), backupStaleAfter())
		return st
	}
	return st
}

func epochOf(t time.Time) float64 { return float64(t.UnixNano()) / float64(time.Second) }

// evaluate runs one watchdog pass and persists the verdict. It preserves
// SinceTS across passes so "broken since Tuesday" stays anchored to Tuesday
// rather than to the most recent tick.
func (m *backupHealthMonitor) evaluate(now time.Time) (backupHealthState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	baseline, err := m.baselineAt(now)
	if err != nil {
		return backupHealthState{}, err
	}
	// 🔴 HONEST BOUNDARY: an unreadable prior verdict is REPLACED, not
	// preserved. The filesystem is the ground truth for "stale" and "never
	// ran", so those two re-derive correctly on the very next pass; the only
	// thing a corrupt row can lose is the immediate `failed` marker, and that
	// loss is bounded — a cadence that keeps failing goes stale within
	// backupStaleAfter() and alarms again on its own. Preserving the
	// unreadable row instead would be worse: nothing would ever clear it, so a
	// single bad write would freeze the light forever.
	prev, _ := m.load()

	var failTS float64
	var failDetail string
	if prev != nil && prev.Code == backupHealthCodeFailed {
		failTS, failDetail = prev.SinceTS, prev.Detail
	}
	newest, hasNewest := newestScheduledBackup(m.dbPath, now)
	next := decideBackupHealth(now, baseline, newest, hasNewest, failTS, failDetail)

	if next.Code != "" {
		next.SinceTS = epochOf(now)
		if prev != nil && prev.Code == next.Code && prev.SinceTS > 0 {
			next.SinceTS = prev.SinceTS // same incident, keep its start
		}
	}
	if err := m.save(next); err != nil {
		return next, err
	}
	return next, nil
}

// noteScheduledOutcome records what a scheduled backup attempt actually did.
// This is what makes a failure visible IMMEDIATELY rather than one stale window
// later: the cadence knows it failed right now, and nothing else in the system
// does.
//
// Manual and pre-migration triggers deliberately do not report here — see the
// file header.
func (m *backupHealthMonitor) noteScheduledOutcome(res backupResult, runErr error, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.baselineAt(now); err != nil {
		return
	}
	prev, _ := m.load()

	failed := runErr != nil || res.Skipped != ""
	if !failed {
		// A scheduled backup landed. That — and only that — clears an incident.
		newest, hasNewest := newestScheduledBackup(m.dbPath, now)
		st := backupHealthState{CheckedTS: epochOf(now)}
		if hasNewest {
			st.NewestBackupTS = epochOf(newest)
		}
		_ = m.save(st)
		return
	}

	detail := "scheduled backup failed, no new retreat point was created"
	switch {
	case runErr != nil:
		detail = fmt.Sprintf("scheduled backup failed: %v — no new retreat point was created", runErr)
	case res.Skipped != "":
		detail = fmt.Sprintf("scheduled backup skipped: %s — no new retreat point was created", res.Skipped)
	}
	st := backupHealthState{
		Code:      backupHealthCodeFailed,
		Detail:    detail,
		SinceTS:   epochOf(now),
		CheckedTS: epochOf(now),
	}
	if newest, ok := newestScheduledBackup(m.dbPath, now); ok {
		st.NewestBackupTS = epochOf(newest)
	}
	if prev != nil && prev.Code == backupHealthCodeFailed && prev.SinceTS > 0 {
		st.SinceTS = prev.SinceTS
	}
	_ = m.save(st)
}

// report is what the endpoint serves. It reads the DURABLE verdict only — it
// never re-derives health from the filesystem, so the indicator, the monitor
// card and the watchdog cannot disagree with each other.
func (m *backupHealthMonitor) report() BackupHealthDTO {
	dto := BackupHealthDTO{
		Status:         backupHealthUnknown,
		StaleAfterSecs: backupStaleAfter().Seconds(),
	}
	if m == nil {
		dto.Detail = "backup health is not being watched on this server"
		return dto
	}
	m.mu.Lock()
	// load() returns a nil state on EVERY error path, so nil is the single
	// question worth asking here — an `err != nil ||` in front of it would be a
	// condition that can never independently decide anything.
	st, _ := m.load()
	m.mu.Unlock()
	if st == nil {
		dto.Detail = "the backup watchdog has not reported yet"
		return dto
	}

	if st.CheckedTS > 0 {
		checked := st.CheckedTS
		dto.CheckedTs = &checked
	}
	if st.NewestBackupTS > 0 {
		newest := st.NewestBackupTS
		dto.NewestBackupTs = &newest
		if st.CheckedTS > 0 {
			age := st.CheckedTS - st.NewestBackupTS
			dto.NewestBackupAgeSecs = &age
		}
	}
	dto.Code = st.Code
	dto.Detail = st.Detail
	if st.SinceTS > 0 {
		since := st.SinceTS
		dto.SinceTs = &since
	}

	switch {
	case st.Code != "":
		dto.Status = backupHealthUnhealthy
	case st.NewestBackupTS > 0:
		dto.Status = backupHealthHealthy
	default:
		// Evaluated, no incident, but no scheduled backup exists yet (the grace
		// window after a fresh install). There is no retreat point, so this is
		// NOT green — the whole point of this module is that a missing retreat
		// must never look like a present one.
		dto.Status = backupHealthUnknown
		if dto.Detail == "" {
			dto.Detail = "no scheduled backup has landed yet"
		}
	}
	return dto
}

// armBackupHealth is called SYNCHRONOUSLY by cmdServe before the watchdog
// goroutine starts. Doing the first pass inline is what makes the wiring
// observable: the baseline row exists in the database from the moment serve got
// this far, so a test can prove serve really armed it (a goroutine could be
// asserted to exist only by racing it).
func armBackupHealth(store backupHealthStore, dbPath string, now time.Time) *backupHealthMonitor {
	m := newBackupHealthMonitor(store, dbPath)
	if _, err := m.evaluate(now); err != nil {
		// Failing to write health state must never stop the server from
		// serving — same trade as the pre-migration backup hook. It is loud
		// instead.
		fmt.Fprintf(os.Stderr, "[backup] WARNING could not record backup health: %v\n", err)
	}
	return m
}

// startBackupHealthWatchdog mounts the loop. ALWAYS mounted by cmdServe, beside
// the backup cadence but INDEPENDENT of it — see the file header for why that
// independence is the whole point.
func startBackupHealthWatchdog(m *backupHealthMonitor, tick time.Duration) {
	if m == nil {
		return
	}
	go func() {
		for {
			time.Sleep(tick)
			if _, err := m.evaluate(time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "[backup] WARNING backup health watchdog: %v\n", err)
			}
		}
	}()
}
