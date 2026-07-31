package main

// backup_test.go — T-ada9.
//
// 🔴 What these tests are FOR, stated up front because the value of a backup is
// exactly the thing that is easy to fake: every assertion here is about reading
// DATA BACK OUT of the produced file. "The command returned nil", "a file
// exists" and "the size looks about right" are all satisfied by a backup that
// cannot be restored, and that is the failure this ticket exists to prevent.
//
// 🔴 Nothing here touches the production database or the real server root. Every
// test runs entirely inside its own t.TempDir(), and the paths the engine writes
// to are DERIVED from the database path it is handed — so a guard failing open
// cannot reach outside the temp dir. (Repo rule: a test must not depend on the
// thing it is testing to keep itself safe.)
//
// 🔴 What these tests deliberately do NOT prove (the tester made this an explicit
// condition of the deliverable, 2026-07-31): they prove the file is readable and
// the rows are all there. They do NOT prove the SERVER can boot on a restored
// database, and they do not prove a real disaster is recoverable end to end.
// Anyone reading "restore verified" will default to the stronger claim, so the
// boundary is written here as well as in the ticket.

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// seedBackupFixture builds a small database with a sentinel row, using the same
// openSQLite the server uses (same driver, same DSN shape) so the engine is
// exercised against a real connection rather than a stand-in.
func seedBackupFixture(t *testing.T, rows int) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "server", "data", "officraft.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE task (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec(`INSERT INTO task (id, title) VALUES (?, ?)`, fmt.Sprintf("t-%04d", i), fmt.Sprintf("row %d", i)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO task (id, title) VALUES ('t-sentinel', '備份哨兵')`); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}
	return db, dbPath
}

// readBackSentinel opens a produced file as a database and reads the known row.
// This is the ONLY thing that counts as proof in this file.
func readBackSentinel(t *testing.T, path string) (title string, rows int) {
	t.Helper()
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open produced file %s: %v", path, err)
	}
	defer db.Close()
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil {
		t.Fatalf("integrity_check on %s: %v", path, err)
	}
	if check != "ok" {
		t.Fatalf("integrity_check on %s = %q, want ok", path, check)
	}
	if err := db.QueryRow(`SELECT title FROM task WHERE id = 't-sentinel'`).Scan(&title); err != nil {
		t.Fatalf("read sentinel from %s: %v", path, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM task`).Scan(&rows); err != nil {
		t.Fatalf("count rows in %s: %v", path, err)
	}
	return title, rows
}

// TestRunDatabaseBackup_ProducesARestorableFile is the acceptance test for the
// whole ticket: the produced file must carry the DATA, not merely exist.
func TestRunDatabaseBackup_ProducesARestorableFile(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 200)
	var sourceRows int
	if err := db.QueryRow(`SELECT count(*) FROM task`).Scan(&sourceRows); err != nil {
		t.Fatalf("count source: %v", err)
	}

	res, err := runDatabaseBackup(db, dbPath, backupReasonManual, time.Date(2026, 7, 31, 23, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if res.Skipped != "" {
		t.Fatalf("backup skipped unexpectedly: %s", res.Skipped)
	}
	if res.Bytes <= 0 {
		t.Fatalf("backup reported %d bytes", res.Bytes)
	}

	title, rows := readBackSentinel(t, res.Path)
	if title != "備份哨兵" {
		t.Errorf("sentinel read back as %q", title)
	}
	if rows != sourceRows {
		t.Errorf("backup has %d rows, source has %d", rows, sourceRows)
	}

	// The whole point of the .partial rename dance: no half-written file may be
	// left behind wearing a backup's name.
	if _, err := os.Stat(res.Path + ".partial"); !os.IsNotExist(err) {
		t.Errorf("a .partial file survived a successful backup")
	}
	// 0600: this file is a complete copy of every chat, card and task in the
	// studio. A world-readable one is a leak with a friendly filename.
	if info, err := os.Stat(res.Path); err != nil {
		t.Fatalf("stat backup: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode is %v, want 0600", info.Mode().Perm())
	}
}

// TestRunDatabaseBackup_IsOnline pins the property the owner actually asked for:
// no downtime. A writer hammering the same database throughout the backup must
// not lose a single write, and the source must still be writable afterwards.
func TestRunDatabaseBackup_IsOnline(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 500)

	writer, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer writer.Close()

	var ok, failed int64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := writer.Exec(`INSERT INTO task (id, title) VALUES (?, 'live')`, fmt.Sprintf("live-%d", i)); err != nil {
				atomic.AddInt64(&failed, 1)
			} else {
				atomic.AddInt64(&ok, 1)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	time.Sleep(50 * time.Millisecond) // let the writer actually get going

	res, backupErr := runDatabaseBackup(db, dbPath, backupReasonScheduled, time.Now())
	close(stop)
	<-done

	if backupErr != nil {
		t.Fatalf("backup during concurrent writes: %v", backupErr)
	}
	if atomic.LoadInt64(&ok) == 0 {
		t.Fatal("the concurrent writer never landed a write — this test proved nothing")
	}
	if n := atomic.LoadInt64(&failed); n != 0 {
		t.Errorf("%d concurrent writes failed during the backup (backups must not cost writes)", n)
	}
	if _, err := db.Exec(`INSERT INTO task (id, title) VALUES ('after', 'after')`); err != nil {
		t.Errorf("source not writable after backup: %v", err)
	}
	if _, rows := readBackSentinel(t, res.Path); rows == 0 {
		t.Error("snapshot taken under load is empty")
	}
}

// TestRotateBackups_MovesToTrashAndNeverDeletes is the rule that protects the
// only copy of anything: rotation retires files by MOVING them. A rotation bug
// that deletes destroys precisely what this engine exists to keep.
func TestRotateBackups_MovesToTrashAndNeverDeletes(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var made []string
	for i := 0; i < backupRetain+3; i++ {
		res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, base.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		if res.Skipped != "" {
			t.Fatalf("backup %d skipped: %s", i, res.Skipped)
		}
		made = append(made, filepath.Base(res.Path))
	}

	kept, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(kept) != backupRetain {
		t.Fatalf("kept %d backups, want %d", len(kept), backupRetain)
	}
	// Newest-first: the survivors must be the LAST ones written. Keeping the
	// wrong five would pass a count check and lose the freshest retreat point.
	for i, e := range kept {
		want := made[len(made)-1-i]
		if e.Name() != want {
			t.Errorf("kept[%d] = %s, want %s", i, e.Name(), want)
		}
	}

	trashed, err := os.ReadDir(backupTrashFor(dbPath))
	if err != nil {
		t.Fatalf("read trash: %v", err)
	}
	if len(trashed) != 3 {
		t.Fatalf("trash holds %d files, want 3 (rotation must move, not delete)", len(trashed))
	}
	// And the evicted files must still be REAL backups, not truncated husks —
	// "moved to trash" is only a safety net if what lands there is intact.
	if _, rows := readBackSentinel(t, filepath.Join(backupTrashFor(dbPath), trashed[0].Name())); rows == 0 {
		t.Error("a retired backup is unreadable")
	}
}

// TestRotateBackups_IgnoresFilesItDidNotCreate. The production data directory
// already holds hand-made snapshots (`officraft.db.bak-pre-v0.5.39`) that
// predate this engine. Rotation MOVES files, so anything it can see it can take
// away — it must therefore only ever see its own.
func TestRotateBackups_IgnoresFilesItDidNotCreate(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	foreign := filepath.Join(dir, "officraft.db.bak-pre-v0.5.39")
	if err := os.WriteFile(foreign, []byte("not ours"), 0o600); err != nil {
		t.Fatalf("write foreign file: %v", err)
	}

	base := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < backupRetain+2; i++ {
		if _, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}

	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("rotation touched a file it did not create: %v", err)
	}
	if trashed, err := os.ReadDir(backupTrashFor(dbPath)); err != nil {
		t.Fatalf("read trash: %v", err)
	} else {
		for _, e := range trashed {
			if strings.Contains(e.Name(), "bak-pre") {
				t.Fatalf("rotation retired a foreign file: %s", e.Name())
			}
		}
	}
}

// TestBackupTick_OnlyActsWhenOneIsDue. The cadence wakes far more often than it
// backs up; a tick that snapshots every time would fill the disk and rotate the
// useful history out within one interval.
func TestBackupTick_OnlyActsWhenOneIsDue(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	if !backupTick(db, dbPath, now) {
		t.Fatal("first tick took no backup — an empty directory always means one is due")
	}
	if backupTick(db, dbPath, now.Add(backupInterval-time.Minute)) {
		t.Error("tick backed up again before the interval elapsed")
	}
	if !backupTick(db, dbPath, now.Add(backupInterval+time.Minute)) {
		t.Error("tick did not back up after the interval elapsed")
	}
	files, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("directory holds %d backups, want 2", len(files))
	}
}

// TestRunDatabaseBackup_ReportsStaleness is the "never ran" alarm. A cadence
// that silently stopped looks exactly like a healthy one, so the run that
// resumes it has to say what it walked into.
func TestRunDatabaseBackup_ReportsStaleness(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	first, err := runDatabaseBackup(db, dbPath, backupReasonManual, now)
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}
	if !first.Stale || first.StaleAge != "no previous backup" {
		t.Errorf("first ever backup reported Stale=%v (%q); an empty directory IS the alarm case", first.Stale, first.StaleAge)
	}

	fresh, err := runDatabaseBackup(db, dbPath, backupReasonManual, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if fresh.Stale {
		t.Errorf("a backup taken one minute after another reported stale (%q) — a false alarm teaches people to ignore it", fresh.StaleAge)
	}

	late, err := runDatabaseBackup(db, dbPath, backupReasonManual, now.Add(backupStaleFactor*backupInterval+time.Hour))
	if err != nil {
		t.Fatalf("late backup: %v", err)
	}
	if !late.Stale {
		t.Error("a gap longer than the alarm window did not report stale")
	}
}

// TestBackupBeforeMigrations_SkipsWhenThereIsNothingToProtect. A first boot has
// no data to lose, and snapshotting a zero-byte file would produce a "backup"
// that restores nothing while reporting success.
func TestBackupBeforeMigrations_SkipsWhenThereIsNothingToProtect(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "server", "data", "officraft.db")
	// Nothing at that path at all.
	backupBeforeMigrations(nil, missing, time.Now()) // must not panic, must not create anything
	if _, err := os.Stat(backupDirFor(missing)); !os.IsNotExist(err) {
		t.Error("a missing database produced a backup directory")
	}

	// An existing but empty file is the fresh-install shape.
	empty := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	backupBeforeMigrations(nil, empty, time.Now())
	if _, err := os.Stat(backupDirFor(empty)); !os.IsNotExist(err) {
		t.Error("an empty database produced a backup directory")
	}
}

// TestBackupBeforeMigrations_SnapshotsRealData is the other half: when there IS
// something to lose, the pre-migration hook must actually take a snapshot, and
// it must be labelled so a directory listing shows a risky moment happened.
func TestBackupBeforeMigrations_SnapshotsRealData(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 20)
	backupBeforeMigrations(db, dbPath, time.Date(2026, 7, 31, 23, 45, 0, 0, time.UTC))

	files, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil || len(files) != 1 {
		t.Fatalf("pre-migration hook produced %d backups (err=%v), want 1", len(files), err)
	}
	if !strings.Contains(files[0].Name(), string(backupReasonPreMigration)) {
		t.Errorf("backup %s is not labelled with its reason", files[0].Name())
	}
	if _, rows := readBackSentinel(t, filepath.Join(backupDirFor(dbPath), files[0].Name())); rows == 0 {
		t.Error("pre-migration backup carries no rows")
	}
}

// TestParseBackupStamp_RoundTrips guards the one thing every reader depends on:
// the filename is the timestamp. Staleness and "is one due?" both read it, so a
// format change that silently stopped parsing would disable BOTH — and the
// symptom would be a cadence that quietly backs up on every tick.
func TestParseBackupStamp_RoundTrips(t *testing.T) {
	want := time.Date(2026, 7, 31, 23, 45, 6, 0, time.UTC)
	name := backupFileName(want, backupReasonScheduled)
	got, ok := parseBackupStamp(name)
	if !ok {
		t.Fatalf("could not parse the name this package just wrote: %s", name)
	}
	if !got.Equal(want) {
		t.Errorf("round-trip gave %s, want %s", got, want)
	}
	if _, ok := parseBackupStamp("officraft.db.bak-pre-v0.5.39"); ok {
		t.Error("a foreign filename parsed as one of ours")
	}
}
