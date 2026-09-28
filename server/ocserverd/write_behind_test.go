package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
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
