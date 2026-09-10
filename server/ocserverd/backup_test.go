package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	osexec "os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func backupTestOpenDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "officraft.db")
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func backupTestSeedDB(t *testing.T, rows int) (*sql.DB, string) {
	t.Helper()
	db, path := backupTestOpenDB(t)
	if _, err := db.Exec(`CREATE TABLE backup_test_rows (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec(`INSERT INTO backup_test_rows (id, title) VALUES (?, ?)`, fmt.Sprintf("row-%d", i), fmt.Sprintf("row %d", i)); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO backup_test_rows (id, title) VALUES ('sentinel', '備份哨兵')`); err != nil {
		t.Fatalf("insert sentinel: %v", err)
	}
	return db, path
}

func backupTestReadSnapshot(t *testing.T, path string) (string, int) {
	t.Helper()
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer db.Close()

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity check = %q, want %q", integrity, "ok")
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM backup_test_rows WHERE id = 'sentinel'`).Scan(&title); err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM backup_test_rows`).Scan(&rows); err != nil {
		t.Fatalf("count snapshot rows: %v", err)
	}
	return title, rows
}

func backupTestWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("backup test"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func backupTestFindFile(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return found
}

func backupTestCaptureLog(t *testing.T, res backupResult, runErr error) string {
	t.Helper()
	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})
	logBackupOutcome(res, runErr)
	return buf.String()
}

func TestBackupPoolOf(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{name: "pre-migration backups use their own quota", file: "officraft-20260908-120000-premigration.db", want: "premigration"},
		{name: "scheduled backups use the routine quota", file: "officraft-20260908-120000-scheduled.db", want: "routine"},
		{name: "manual backups use the routine quota", file: "officraft-20260908-120000-manual.db", want: "routine"},
		{name: "unknown reasons use the routine quota", file: "officraft-20260908-120000-checkpoint.db", want: "routine"},
		{name: "a malformed name uses the routine quota", file: "not-a-backup.db", want: "routine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(backupPoolOf(tc.file)); got != tc.want {
				t.Errorf("backupPoolOf(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

func TestBackupReasonIn(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{name: "reads the pre-migration reason", file: "officraft-20260908-120000-premigration.db", want: "premigration"},
		{name: "reads the scheduled reason", file: "officraft-20260908-120000-scheduled.db", want: "scheduled"},
		{name: "reads the manual reason", file: "officraft-20260908-120000-manual.db", want: "manual"},
		{name: "preserves an unrecognised reason label", file: "officraft-20260908-120000-checkpoint.db", want: "checkpoint"},
		{name: "returns empty when no reason field exists", file: "officraft-20260908-120000.db", want: ""},
		{name: "returns empty for a short name", file: "officraft-20260908.db", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(backupReasonIn(tc.file)); got != tc.want {
				t.Errorf("backupReasonIn(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

func TestFreeBytesAt(t *testing.T) {
	dir := t.TempDir()
	free, ok := freeBytesAt(dir)
	if ok != true {
		t.Fatalf("freeBytesAt(%q) reported ok = %v, want true", dir, ok)
	}
	if free <= 0 {
		t.Fatalf("freeBytesAt(%q) = %d, want a positive byte count", dir, free)
	}

	missing := filepath.Join(dir, "does-not-exist")
	free, ok = freeBytesAt(missing)
	if ok != false {
		t.Errorf("freeBytesAt(%q) reported ok = %v, want false", missing, ok)
	}
	if free != 0 {
		t.Errorf("freeBytesAt(%q) = %d, want 0", missing, free)
	}
}

func TestBackupFilesIn(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"officraft-20260908-120000-scheduled.db",
		"officraft-20260908-110000-manual.db",
		"officraft-20260908-100000-premigration.db",
		"officraft-20260908-130000-scheduled.db.partial",
		"officraft.db.bak-pre-v0.5.39",
		"other.db",
	} {
		backupTestWriteFile(t, filepath.Join(dir, name))
	}
	if err := os.Mkdir(filepath.Join(dir, "officraft-20260908-130000-manual.db"), 0o700); err != nil {
		t.Fatalf("make backup-shaped directory: %v", err)
	}

	entries, err := backupFilesIn(dir)
	if err != nil {
		t.Fatalf("backupFilesIn: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{
		"officraft-20260908-120000-scheduled.db",
		"officraft-20260908-110000-manual.db",
		"officraft-20260908-100000-premigration.db",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("backupFilesIn names = %v, want %v", got, want)
	}

	_, err = backupFilesIn(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Error("backupFilesIn on a missing directory returned nil error")
	}
}

func TestNewestBackupTime(t *testing.T) {
	t.Run("uses the newest filename timestamp instead of file modification time", func(t *testing.T) {
		dir := t.TempDir()
		oldName := "officraft-20260908-090000-scheduled.db"
		newName := "officraft-20260908-120000-manual.db"
		backupTestWriteFile(t, filepath.Join(dir, oldName))
		backupTestWriteFile(t, filepath.Join(dir, newName))
		oldMtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := os.Chtimes(filepath.Join(dir, newName), oldMtime, oldMtime); err != nil {
			t.Fatalf("change mtime: %v", err)
		}

		got, ok := newestBackupTime(dir)
		if ok != true {
			t.Fatalf("newestBackupTime reported ok = %v, want true", ok)
		}
		want := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("newestBackupTime = %s, want %s", got, want)
		}
	})

	t.Run("returns no time for an empty directory", func(t *testing.T) {
		got, ok := newestBackupTime(t.TempDir())
		if ok != false {
			t.Errorf("newestBackupTime reported ok = %v, want false", ok)
		}
		if !got.IsZero() {
			t.Errorf("newestBackupTime returned %s, want the zero time", got)
		}
	})

	t.Run("returns no time when the newest matching name has no valid timestamp", func(t *testing.T) {
		dir := t.TempDir()
		backupTestWriteFile(t, filepath.Join(dir, "officraft-20260908-not-a-time-scheduled.db"))
		got, ok := newestBackupTime(dir)
		if ok != false {
			t.Errorf("newestBackupTime reported ok = %v, want false", ok)
		}
		if !got.IsZero() {
			t.Errorf("newestBackupTime returned %s, want the zero time", got)
		}
	})
}

func TestParseBackupStamp(t *testing.T) {
	cases := []struct {
		name string
		file string
		want time.Time
		ok   bool
	}{
		{name: "scheduled stamp", file: "officraft-20260908-123456-scheduled.db", want: time.Date(2026, 9, 8, 12, 34, 56, 0, time.UTC), ok: true},
		{name: "pre-migration stamp", file: "officraft-20260731-224500-premigration.db", want: time.Date(2026, 7, 31, 22, 45, 0, 0, time.UTC), ok: true},
		{name: "invalid calendar date", file: "officraft-20260230-120000-scheduled.db", want: time.Time{}, ok: false},
		{name: "missing time field", file: "officraft-20260908.db", want: time.Time{}, ok: false},
		{name: "foreign snapshot name", file: "officraft.db.bak-pre-v0.5.39", want: time.Time{}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseBackupStamp(tc.file)
			if ok != tc.ok {
				t.Errorf("parseBackupStamp(%q) ok = %v, want %v", tc.file, ok, tc.ok)
			}
			if tc.ok && !got.Equal(tc.want) {
				t.Errorf("parseBackupStamp(%q) = %s, want %s", tc.file, got, tc.want)
			}
			if !tc.ok && !got.IsZero() {
				t.Errorf("parseBackupStamp(%q) returned %s on invalid input", tc.file, got)
			}
		})
	}
}

func TestRunDatabaseBackup(t *testing.T) {
	t.Run("writes a readable snapshot and publishes only the completed file", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 3)
		now := time.Date(2026, 9, 8, 12, 34, 56, 0, time.UTC)

		res, err := runDatabaseBackup(db, dbPath, backupReasonManual, now, 2)
		if err != nil {
			t.Fatalf("runDatabaseBackup: %v", err)
		}
		if string(res.Reason) != "manual" {
			t.Errorf("result reason = %q, want %q", res.Reason, "manual")
		}
		if res.Skipped != "" {
			t.Fatalf("backup was skipped: %s", res.Skipped)
		}
		if res.Stale != true || res.StaleAge != "no previous backup" {
			t.Errorf("first backup staleness = (%v, %q), want (true, %q)", res.Stale, res.StaleAge, "no previous backup")
		}
		if filepath.Dir(res.Path) != filepath.Join(filepath.Dir(dbPath), "backups") {
			t.Errorf("result directory = %q, want a backups directory beside the database", filepath.Dir(res.Path))
		}
		if filepath.Base(res.Path) != "officraft-20260908-123456-manual.db" {
			t.Errorf("result filename = %q, want %q", filepath.Base(res.Path), "officraft-20260908-123456-manual.db")
		}
		if res.Bytes <= 0 {
			t.Errorf("reported bytes = %d, want a positive size", res.Bytes)
		}
		if res.Took < 0 {
			t.Errorf("reported duration = %s, want a non-negative duration", res.Took)
		}
		if len(res.Deleted) != 0 {
			t.Errorf("deleted files = %v, want none for the first backup", res.Deleted)
		}
		if res.Reaped != 0 {
			t.Errorf("reaped files = %d, want 0", res.Reaped)
		}

		title, rows := backupTestReadSnapshot(t, res.Path)
		if title != "備份哨兵" {
			t.Errorf("snapshot sentinel = %q, want %q", title, "備份哨兵")
		}
		if rows != 4 {
			t.Errorf("snapshot rows = %d, want 4", rows)
		}
		if _, err := os.Stat(res.Path + ".partial"); !os.IsNotExist(err) {
			t.Errorf("partial output still exists: %v", err)
		}
		info, err := os.Stat(res.Path)
		if err != nil {
			t.Fatalf("stat published backup: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("published backup mode = %o, want 600", info.Mode().Perm())
		}
		if res.Bytes != info.Size() {
			t.Errorf("reported bytes = %d, published size = %d", res.Bytes, info.Size())
		}
	})

	t.Run("returns the database error without publishing a backup", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 1)
		if err := db.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}

		res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC), 2)
		if err == nil {
			t.Fatal("runDatabaseBackup returned nil error for a closed database")
		}
		if res.Path != "" {
			t.Errorf("failed result path = %q, want empty", res.Path)
		}
		if res.Skipped != "" {
			t.Errorf("failed result skipped = %q, want empty", res.Skipped)
		}
		files, listErr := backupFilesIn(backupDirFor(dbPath))
		if listErr != nil {
			t.Fatalf("list backup directory: %v", listErr)
		}
		if len(files) != 0 {
			t.Errorf("published backups after failure = %d, want 0", len(files))
		}
	})
}

func TestLiveBackupRetain(t *testing.T) {
	if got := liveBackupRetain(nil); got != 5 {
		t.Errorf("liveBackupRetain(nil) = %d, want 5", got)
	}

	cases := []struct {
		name      string
		makeTable bool
		hasRow    bool
		value     string
		want      int
	}{
		{name: "missing setting table falls back", want: 5},
		{name: "missing row falls back", makeTable: true, want: 5},
		{name: "minimum accepted value", makeTable: true, hasRow: true, value: "1", want: 1},
		{name: "maximum accepted value", makeTable: true, hasRow: true, value: "20", want: 20},
		{name: "surrounding whitespace is accepted", makeTable: true, hasRow: true, value: " 7 ", want: 7},
		{name: "non-numeric value falls back", makeTable: true, hasRow: true, value: "seven", want: 5},
		{name: "zero falls back", makeTable: true, hasRow: true, value: "0", want: 5},
		{name: "value above the ceiling falls back", makeTable: true, hasRow: true, value: "21", want: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := backupTestOpenDB(t)
			if tc.makeTable {
				if _, err := db.Exec(`CREATE TABLE setting (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
					t.Fatalf("create setting table: %v", err)
				}
			}
			if tc.hasRow {
				if _, err := db.Exec(`INSERT INTO setting (key, value) VALUES (?, ?)`, settingBackupRetain, tc.value); err != nil {
					t.Fatalf("insert setting: %v", err)
				}
			}
			if got := liveBackupRetain(db); got != tc.want {
				t.Errorf("liveBackupRetain = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRotateBackups(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "officraft.db")
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make backup directory: %v", err)
	}
	allBackups := []string{
		"officraft-20260908-120000-scheduled.db",
		"officraft-20260908-110000-manual.db",
		"officraft-20260908-100000-scheduled.db",
		"officraft-20260908-090000-manual.db",
		"officraft-20260908-080000-manual.db",
		"officraft-20260908-120500-premigration.db",
		"officraft-20260908-110500-premigration.db",
		"officraft-20260908-100500-premigration.db",
	}
	for _, name := range allBackups {
		backupTestWriteFile(t, filepath.Join(dir, name))
	}
	foreign := filepath.Join(dir, "officraft.db.bak-pre-v0.5.39")
	partial := filepath.Join(dir, "officraft-20260908-140000-scheduled.db.partial")
	backupTestWriteFile(t, foreign)
	backupTestWriteFile(t, partial)
	directory := filepath.Join(dir, "officraft-20260908-130000-ignored.db")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("make backup-shaped directory: %v", err)
	}

	deleted, err := rotateBackups(dbPath, 2)
	if err != nil {
		t.Fatalf("rotateBackups: %v", err)
	}
	wantDeleted := []string{
		"officraft-20260908-100500-premigration.db",
		"officraft-20260908-100000-scheduled.db",
		"officraft-20260908-090000-manual.db",
		"officraft-20260908-080000-manual.db",
	}
	if !reflect.DeepEqual(deleted, wantDeleted) {
		t.Errorf("deleted = %v, want %v", deleted, wantDeleted)
	}
	wantSurvivors := []string{
		"officraft-20260908-120500-premigration.db",
		"officraft-20260908-120000-scheduled.db",
		"officraft-20260908-110500-premigration.db",
		"officraft-20260908-110000-manual.db",
	}
	entries, err := backupFilesIn(dir)
	if err != nil {
		t.Fatalf("list survivors: %v", err)
	}
	survivors := make([]string, 0, len(entries))
	for _, entry := range entries {
		survivors = append(survivors, entry.Name())
	}
	if !reflect.DeepEqual(survivors, wantSurvivors) {
		t.Errorf("survivors = %v, want %v", survivors, wantSurvivors)
	}
	for _, name := range wantDeleted {
		if found := backupTestFindFile(t, filepath.Dir(dbPath), name); found != "" {
			t.Errorf("deleted backup %q still exists at %q", name, found)
		}
	}
	for _, path := range []string{foreign, partial, directory} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("non-owned path %q was changed: %v", path, err)
		}
	}

	t.Run("a non-positive keep value leaves files in place", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "officraft.db")
		dir := backupDirFor(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make backup directory: %v", err)
		}
		name := "officraft-20260908-150000-scheduled.db"
		backupTestWriteFile(t, filepath.Join(dir, name))
		deleted, err := rotateBackups(path, 0)
		if err != nil {
			t.Fatalf("rotateBackups: %v", err)
		}
		if len(deleted) != 0 {
			t.Errorf("deleted = %v, want no files", deleted)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("backup was removed for keep=0: %v", err)
		}
	})

	t.Run("a missing directory returns its read error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "officraft.db")
		if _, err := rotateBackups(path, 2); err == nil {
			t.Error("rotateBackups on a missing directory returned nil error")
		}
	})
}

func TestReapBackupTrash(t *testing.T) {
	t.Run("removes only directly matching legacy files", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		trash := backupTrashFor(dbPath)
		if err := os.MkdirAll(trash, 0o700); err != nil {
			t.Fatalf("make trash directory: %v", err)
		}
		matching := []string{
			"officraft-20260908-100000-scheduled.db",
			"officraft-20260908-090000-premigration.db",
		}
		for _, name := range matching {
			backupTestWriteFile(t, filepath.Join(trash, name))
		}
		foreign := filepath.Join(trash, "officraft.db.bak-pre-v0.5.39")
		partial := filepath.Join(trash, "officraft-20260908-080000-manual.db.partial")
		backupTestWriteFile(t, foreign)
		backupTestWriteFile(t, partial)
		directory := filepath.Join(trash, "officraft-20260908-070000-manual.db")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("make trash subdirectory: %v", err)
		}

		reaped, err := reapBackupTrash(dbPath)
		if err != nil {
			t.Fatalf("reapBackupTrash: %v", err)
		}
		if reaped != 2 {
			t.Errorf("reaped = %d, want 2", reaped)
		}
		for _, name := range matching {
			if _, err := os.Stat(filepath.Join(trash, name)); !os.IsNotExist(err) {
				t.Errorf("legacy backup %q remains: %v", name, err)
			}
		}
		for _, path := range []string{foreign, partial, directory} {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("non-matching trash path %q was changed: %v", path, err)
			}
		}
	})

	t.Run("a missing trash directory is already clean", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		reaped, err := reapBackupTrash(dbPath)
		if err != nil {
			t.Fatalf("reapBackupTrash: %v", err)
		}
		if reaped != 0 {
			t.Errorf("reaped = %d, want 0", reaped)
		}
	})

	t.Run("a symlinked trash directory is refused without touching the target", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "officraft.db")
		backupDir := backupDirFor(dbPath)
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			t.Fatalf("make backup directory: %v", err)
		}
		name := "officraft-20260908-120000-scheduled.db"
		backupTestWriteFile(t, filepath.Join(backupDir, name))
		if err := os.Symlink(backupDir, backupTrashFor(dbPath)); err != nil {
			t.Fatalf("symlink trash: %v", err)
		}

		reaped, err := reapBackupTrash(dbPath)
		if err != nil {
			t.Fatalf("reapBackupTrash: %v", err)
		}
		if reaped != 0 {
			t.Errorf("reaped = %d, want 0", reaped)
		}
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Errorf("backup behind symlink was changed: %v", err)
		}
	})
}

func TestLogBackupOutcome(t *testing.T) {
	t.Run("reports stale state, success, rotation and reaping separately", func(t *testing.T) {
		got := backupTestCaptureLog(t, backupResult{
			Path:     "/tmp/officraft-20260908-120000-manual.db",
			Bytes:    3145728,
			Took:     1234 * time.Millisecond,
			Deleted:  []string{"old-a.db", "old-b.db"},
			Reaped:   2,
			Reason:   backupReasonManual,
			Stale:    true,
			StaleAge: "13h",
		}, nil)
		want := "[backup] WARNING newest existing backup was stale (13h) — this studio had no recent retreat point\n" +
			"[backup] ok (manual): officraft-20260908-120000-manual.db (3 MB in 1.234s)\n" +
			"[backup] DELETED 2 backup(s) past the retention limit: old-a.db, old-b.db\n" +
			"[backup] reclaimed 2 file(s) from the legacy trash/ backlog\n"
		if got != want {
			t.Errorf("log output = %q, want %q", got, want)
		}
	})

	t.Run("reports a plain successful backup without optional lines", func(t *testing.T) {
		got := backupTestCaptureLog(t, backupResult{
			Path:   "/tmp/officraft-20260908-130000-scheduled.db",
			Bytes:  1048576,
			Took:   time.Second,
			Reason: backupReasonScheduled,
		}, nil)
		want := "[backup] ok (scheduled): officraft-20260908-130000-scheduled.db (1 MB in 1s)\n"
		if got != want {
			t.Errorf("log output = %q, want %q", got, want)
		}
	})

	t.Run("reports an error instead of a new retreat point", func(t *testing.T) {
		got := backupTestCaptureLog(t, backupResult{Reason: backupReasonScheduled}, errors.New("VACUUM INTO failed"))
		want := "[backup] FAILED (scheduled): VACUUM INTO failed — THERE IS NO NEW RETREAT POINT\n"
		if got != want {
			t.Errorf("log output = %q, want %q", got, want)
		}
	})

	t.Run("reports a deliberate skip", func(t *testing.T) {
		got := backupTestCaptureLog(t, backupResult{
			Reason:  backupReasonPreMigration,
			Skipped: "only 1 MB free, want 2 MB (db is 0 MB)",
		}, nil)
		want := "[backup] SKIPPED (premigration): only 1 MB free, want 2 MB (db is 0 MB) — no new retreat point was created\n"
		if got != want {
			t.Errorf("log output = %q, want %q", got, want)
		}
	})
}

func TestStartBackupCadence(t *testing.T) {
	if os.Getenv("OFFICRAFT_BACKUP_CADENCE_CHILD") == "1" {
		db, dbPath := backupTestSeedDB(t, 1)
		startBackupCadence(db, dbPath, 10*time.Millisecond, nil)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			files, err := backupFilesIn(backupDirFor(dbPath))
			if err == nil && len(files) == 1 {
				if string(backupReasonIn(files[0].Name())) != "scheduled" {
					t.Fatalf("cadence reason = %q, want %q", backupReasonIn(files[0].Name()), "scheduled")
				}
				title, rows := backupTestReadSnapshot(t, filepath.Join(backupDirFor(dbPath), files[0].Name()))
				if title != "備份哨兵" || rows != 2 {
					t.Fatalf("cadence snapshot = (%q, %d), want (%q, 2)", title, rows, "備份哨兵")
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("cadence did not create a scheduled backup")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := osexec.CommandContext(ctx, os.Args[0], "-test.run", "^TestStartBackupCadence$")
	cmd.Env = append(os.Environ(), "OFFICRAFT_BACKUP_CADENCE_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cadence child failed: %v\n%s", err, output)
	}
}

func TestBackupTick(t *testing.T) {
	t.Run("takes a scheduled backup when none exists", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		if taken := backupTick(db, dbPath, now, nil); taken != true {
			t.Fatal("backupTick returned false for an empty backup directory")
		}
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "officraft-20260908-120000-scheduled.db" {
			t.Errorf("backups = %v, want [officraft-20260908-120000-scheduled.db]", entries)
		}
	})

	t.Run("does not take another backup before the interval", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		if _, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now, 5); err != nil {
			t.Fatalf("seed scheduled backup: %v", err)
		}
		if taken := backupTick(db, dbPath, now.Add(backupInterval-time.Minute), nil); taken != false {
			t.Fatal("backupTick took a backup before the interval elapsed")
		}
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("backup count = %d, want 1", len(entries))
		}
	})

	t.Run("does not treat a pre-migration backup as scheduled coverage", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		if _, err := runDatabaseBackup(db, dbPath, backupReasonPreMigration, now, 5); err != nil {
			t.Fatalf("seed pre-migration backup: %v", err)
		}
		if taken := backupTick(db, dbPath, now.Add(time.Hour), nil); taken != true {
			t.Fatal("backupTick did not take a scheduled backup after only a pre-migration backup")
		}
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		got := make([]string, 0, len(entries))
		for _, entry := range entries {
			got = append(got, entry.Name())
		}
		want := []string{
			"officraft-20260908-130000-scheduled.db",
			"officraft-20260908-120000-premigration.db",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("backups = %v, want %v", got, want)
		}
	})

	t.Run("does not treat a future scheduled stamp as coverage", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		if _, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now.Add(time.Hour), 5); err != nil {
			t.Fatalf("seed future backup: %v", err)
		}
		if taken := backupTick(db, dbPath, now, nil); taken != true {
			t.Fatal("backupTick did not take a backup with only a future scheduled stamp")
		}
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		got := make([]string, 0, len(entries))
		for _, entry := range entries {
			got = append(got, entry.Name())
		}
		want := []string{
			"officraft-20260908-130000-scheduled.db",
			"officraft-20260908-120000-scheduled.db",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("backups = %v, want %v", got, want)
		}
	})

	t.Run("returns false when the snapshot cannot run", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		if err := db.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		if taken := backupTick(db, dbPath, time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC), nil); taken != false {
			t.Fatal("backupTick returned true for a failed snapshot")
		}
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("backups after failed tick = %d, want 0", len(entries))
		}
	})
}

func TestBackupBeforeMigrations(t *testing.T) {
	t.Run("does nothing for a missing or empty database", func(t *testing.T) {
		root := t.TempDir()
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		missing := filepath.Join(root, "missing", "officraft.db")
		backupBeforeMigrations(nil, missing, now)
		if _, err := os.Stat(backupDirFor(missing)); !os.IsNotExist(err) {
			t.Errorf("missing database backup directory error = %v, want not exist", err)
		}

		empty := filepath.Join(root, "empty.db")
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatalf("write empty database: %v", err)
		}
		backupBeforeMigrations(nil, empty, now)
		if _, err := os.Stat(backupDirFor(empty)); !os.IsNotExist(err) {
			t.Errorf("empty database backup directory error = %v, want not exist", err)
		}
	})

	t.Run("writes a labelled readable snapshot for an existing database", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 2)
		now := time.Date(2026, 9, 8, 15, 16, 17, 0, time.UTC)
		backupBeforeMigrations(db, dbPath, now)

		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list pre-migration backups: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("pre-migration backup count = %d, want 1", len(entries))
		}
		if entries[0].Name() != "officraft-20260908-151617-premigration.db" {
			t.Errorf("pre-migration backup name = %q, want %q", entries[0].Name(), "officraft-20260908-151617-premigration.db")
		}
		title, rows := backupTestReadSnapshot(t, filepath.Join(backupDirFor(dbPath), entries[0].Name()))
		if title != "備份哨兵" {
			t.Errorf("snapshot sentinel = %q, want %q", title, "備份哨兵")
		}
		if rows != 3 {
			t.Errorf("snapshot rows = %d, want 3", rows)
		}
	})

	t.Run("continues without publishing when the snapshot fails", func(t *testing.T) {
		db, dbPath := backupTestSeedDB(t, 0)
		if err := db.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
		backupBeforeMigrations(db, dbPath, time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC))
		entries, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list failed pre-migration backups: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("published backups after failure = %d, want 0", len(entries))
		}
	})
}
