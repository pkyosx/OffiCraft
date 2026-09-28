package main

// backup_health.go — the COCKPIT-VISIBLE half of the backup engine: backup.go
// only logs, into a file nothing reads, so a dead schedule and a healthy one
// looked identical to the owner.
//
// 🔴 The watchdog is its OWN goroutine, not called from the backup path: a
// cadence that never started would otherwise never be checked. Health state is
// DURABLE so a restart cannot turn "broken for three days" back into green.
//
// 🔴 Only SCHEDULED backups count as evidence the schedule is alive. Manual and
// pre-migration snapshots land in the same directory; counting them would let a
// hand-taken backup or an upgrade paper over a dead cadence.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// settingBackupWatchdogBaseline is write-once and durable: "never ran" is
	// measured against it, and process start time would reset on every restart,
	// so a never-working install would never accumulate enough uptime to alarm.
	settingBackupWatchdogBaseline = "backup.watchdog_baseline_ts"

	settingBackupHealth = "backup.health"

	backupWatchdogCadence = 5 * time.Minute
)

// Health vocabulary: closed sets shared by the DTO and the cockpit.
const (
	backupHealthHealthy   = "healthy"
	backupHealthUnhealthy = "unhealthy"
	backupHealthUnknown   = "unknown"

	backupHealthCodeNeverRan = "never_ran"
	backupHealthCodeStale    = "stale"
	backupHealthCodeFailed   = "failed"
)

func backupStaleAfter() time.Duration { return backupStaleFactor * backupInterval }

type backupHealthState struct {
	Code           string  `json:"code"`
	Detail         string  `json:"detail"`
	SinceTS        float64 `json:"since_ts"`
	CheckedTS      float64 `json:"checked_ts"`
	NewestBackupTS float64 `json:"newest_backup_ts"`
}

// Inside inTx, GetSetting and PutSetting run on that transaction (the DAL's
// reads and writes join the calling goroutine's transaction).
type backupHealthStore interface {
	GetSetting(key string) (*string, error)
	PutSetting(key, value string) error
	inTx(fn func(tx *writeTx) error) error
}

// The watchdog and the backup cadence both read the stored verdict and write a
// new one; each does so in one transaction, so a `failed` the cadence records
// cannot be overwritten by a watchdog pass that read the verdict before it.
type backupHealthMonitor struct {
	store  backupHealthStore
	dbPath string
}

func newBackupHealthMonitor(store backupHealthStore, dbPath string) *backupHealthMonitor {
	return &backupHealthMonitor{store: store, dbPath: dbPath}
}

func (m *backupHealthMonitor) baselineAt(now time.Time) (time.Time, error) {
	raw, err := m.store.GetSetting(settingBackupWatchdogBaseline)
	if err != nil {
		return time.Time{}, err
	}
	if raw != nil {
		// strconv, NOT fmt.Sscanf: Sscanf accepts a numeric PREFIX, so a corrupted
		// "1785600000junk" would parse and be trusted.
		if ts, err := strconv.ParseFloat(strings.TrimSpace(*raw), 64); err == nil && ts > 0 {
			baseline := time.Unix(0, int64(ts*float64(time.Second)))
			// 🔴 A FUTURE baseline is re-armed, not honoured: `now.Sub(baseline) >
			// backupStaleAfter()` is false for every negative value, so after a
			// backwards clock step the never-ran alarm could NEVER fire again.
			// Clamping in decideBackupHealth instead would leave the bogus row in
			// the database forever.
			if !baseline.After(now) {
				return baseline, nil
			}
		}
	}
	if err := m.store.PutSetting(settingBackupWatchdogBaseline, fmt.Sprintf("%f", float64(now.UnixNano())/float64(time.Second))); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func (m *backupHealthMonitor) load() (*backupHealthState, error) {
	raw, err := m.store.GetSetting(settingBackupHealth)
	if err != nil || raw == nil {
		return nil, err
	}
	var st backupHealthState
	if err := json.Unmarshal([]byte(*raw), &st); err != nil {
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

// newestScheduledBackup is the ONE answer to "when did the schedule last run?"
// for all four production callers (backupTick and three here) — do not add a
// second future-check in any of them.
//
// 🔴 A future-stamped file is SKIPPED: every caller subtracts it from a clock,
// and a negative age reads as "just backed up" — backupTick would never back up
// again while the cockpit stayed green. Treating it as "infinitely old" instead
// is a trap: it stays the newest file, so every tick would back up again, filling
// the disk and rotating the real history out. Skipping converges after ONE extra
// snapshot. No skew grace, deliberately: it would reopen the starvation window by
// its own, uncalibratable width.
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

// The backup directory is listed inside the transaction on purpose. Listed
// before it, a scheduled backup and its noteScheduledOutcome could both land in
// between, and this pass would then overwrite the fresh verdict with one
// computed from the older listing.
func (m *backupHealthMonitor) evaluate(now time.Time) (next backupHealthState, err error) {
	err = m.store.inTx(func(*writeTx) error {
		next, err = m.evaluateOnce(now)
		return err
	})
	return next, err
}

func (m *backupHealthMonitor) evaluateOnce(now time.Time) (backupHealthState, error) {
	baseline, err := m.baselineAt(now)
	if err != nil {
		return backupHealthState{}, err
	}
	// 🔴 An unreadable prior verdict is REPLACED, not preserved: stale/never-ran
	// re-derive from the filesystem next pass, and a lost `failed` marker returns
	// within backupStaleAfter(); preserving it would freeze the light forever.
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
			next.SinceTS = prev.SinceTS
		}
	}
	if err := m.save(next); err != nil {
		return next, err
	}
	return next, nil
}

func (m *backupHealthMonitor) noteScheduledOutcome(res backupResult, runErr error, now time.Time) {
	if m == nil {
		return
	}
	_ = m.store.inTx(func(*writeTx) error {
		return m.noteScheduledOutcomeOnce(res, runErr, now)
	})
}

func (m *backupHealthMonitor) noteScheduledOutcomeOnce(res backupResult, runErr error, now time.Time) error {
	if _, err := m.baselineAt(now); err != nil {
		return err
	}
	prev, _ := m.load()

	failed := runErr != nil || res.Skipped != ""
	if !failed {
		newest, hasNewest := newestScheduledBackup(m.dbPath, now)
		st := backupHealthState{CheckedTS: epochOf(now)}
		if hasNewest {
			st.NewestBackupTS = epochOf(newest)
		}
		return m.save(st)
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
	return m.save(st)
}

// report serves the DURABLE verdict only and never re-derives health from the
// filesystem, so the indicator, the monitor card and the watchdog cannot
// disagree.
func (m *backupHealthMonitor) report() BackupHealthDTO {
	dto := BackupHealthDTO{
		Status:         backupHealthUnknown,
		StaleAfterSecs: backupStaleAfter().Seconds(),
	}
	if m == nil {
		dto.Detail = "backup health is not being watched on this server"
		return dto
	}
	st, _ := m.load()
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
		dto.Status = backupHealthUnknown
		if dto.Detail == "" {
			dto.Detail = "no scheduled backup has landed yet"
		}
	}
	return dto
}

func armBackupHealth(store backupHealthStore, dbPath string, now time.Time) *backupHealthMonitor {
	m := newBackupHealthMonitor(store, dbPath)
	if _, err := m.evaluate(now); err != nil {
		fmt.Fprintf(os.Stderr, "[backup] WARNING could not record backup health: %v\n", err)
	}
	return m
}

func startBackupHealthWatchdog(m *backupHealthMonitor, tick time.Duration) {
	if m == nil {
		return
	}
	go func() {
		for {
			time.Sleep(tick)
			surviveLockInTx("backup health watchdog", func() {
				if _, err := m.evaluate(time.Now()); err != nil {
					fmt.Fprintf(os.Stderr, "[backup] WARNING backup health watchdog: %v\n", err)
				}
			})
		}
	}()
}
