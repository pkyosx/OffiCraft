// Skeleton generated from server/ocserverd/migrate.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data", "officraft.db")
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent directory was not created: %v", err)
	}
	if got, err := assertJournalMode(db, sqliteJournalMode); err != nil {
		t.Fatalf("assertJournalMode: %v (got %q)", err, got)
	}
}

func TestOpenSQLiteReadPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "officraft.db")
	writeDB, err := openSQLite(path)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	if _, err := writeDB.Exec(`CREATE TABLE sample (value TEXT)`); err != nil {
		writeDB.Close()
		t.Fatalf("create sample table: %v", err)
	}
	if _, err := writeDB.Exec(`INSERT INTO sample(value) VALUES ('readable')`); err != nil {
		writeDB.Close()
		t.Fatalf("insert sample row: %v", err)
	}
	if err := writeDB.Close(); err != nil {
		t.Fatalf("close write db: %v", err)
	}

	readDB, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("openSQLiteReadPool: %v", err)
	}
	defer readDB.Close()
	if err := readDB.Ping(); err != nil {
		t.Fatalf("read pool Ping: %v", err)
	}
	if got := readDB.Stats().MaxOpenConnections; got != sqliteMaxReadConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, sqliteMaxReadConns)
	}
	var value string
	if err := readDB.QueryRow(`SELECT value FROM sample`).Scan(&value); err != nil {
		t.Fatalf("read sample row: %v", err)
	}
	if value != "readable" {
		t.Fatalf("sample value = %q, want readable", value)
	}
	if _, err := readDB.Exec(`CREATE TABLE forbidden (value TEXT)`); err == nil {
		t.Fatal("read pool allowed a write")
	}
}

func TestAssertJournalMode(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "officraft.db"))
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer db.Close()
	if got, err := assertJournalMode(db, sqliteJournalMode); err != nil || !strings.EqualFold(got, sqliteJournalMode) {
		t.Fatalf("assertJournalMode(WAL) = (%q, %v), want WAL and nil error", got, err)
	}
	if got, err := assertJournalMode(db, "delete"); err == nil || !strings.EqualFold(got, sqliteJournalMode) {
		t.Fatalf("assertJournalMode(delete) = (%q, %v), want actual WAL and an error", got, err)
	}
}

func TestRunMigrations(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "officraft.db"))
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations(second call): %v", err)
	}
	var table string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'member'`).Scan(&table); err != nil {
		t.Fatalf("member table: %v", err)
	}
	if table != "member" {
		t.Fatalf("table = %q, want member", table)
	}
	var migrationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version`).Scan(&migrationCount); err != nil {
		t.Fatalf("goose version rows: %v", err)
	}
	if migrationCount == 0 {
		t.Fatal("goose_db_version has no applied migrations")
	}
}

func TestCmdBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "officraft.db")
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE sample (value TEXT)`); err != nil {
		db.Close()
		t.Fatalf("create sample table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	dsn := "sqlite:///" + path
	env := func(name string) string {
		if name == envDatabaseURL {
			return dsn
		}
		return ""
	}
	var out strings.Builder
	if rc := cmdBackup(env, &out); rc != 0 {
		t.Fatalf("cmdBackup rc = %d, output=%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "backup ok:") || !strings.Contains(out.String(), path) {
		t.Fatalf("cmdBackup output = %q, want success and database path", out.String())
	}
	files, err := os.ReadDir(backupDirFor(path))
	if err != nil {
		t.Fatalf("ReadDir(backups): %v", err)
	}
	if len(files) != 1 || !strings.Contains(files[0].Name(), "-manual.db") {
		t.Fatalf("backup files = %#v, want one manual backup", files)
	}

	var unsupported strings.Builder
	if rc := cmdBackup(func(name string) string {
		if name == envDatabaseURL {
			return "postgres://example/db"
		}
		return ""
	}, &unsupported); rc != 1 || !strings.Contains(unsupported.String(), "supports sqlite DSNs only") {
		t.Fatalf("unsupported cmdBackup = (%d, %q), want rc 1 and sqlite-only error", rc, unsupported.String())
	}
}

func TestCmdMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "officraft.db")
	dsn := "sqlite:///" + path
	env := func(name string) string {
		if name == envDatabaseURL {
			return dsn
		}
		return ""
	}
	var out strings.Builder
	if rc := cmdMigrate(env, &out); rc != 0 {
		t.Fatalf("cmdMigrate rc = %d, output=%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "migrations applied + seed ensured") {
		t.Fatalf("cmdMigrate output = %q, want migration success", out.String())
	}
	db, err := openSQLiteReadPool(path)
	if err != nil {
		t.Fatalf("open migrated read pool: %v", err)
	}
	defer db.Close()
	var members int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member`).Scan(&members); err != nil {
		t.Fatalf("seeded members: %v", err)
	}
	if members < 2 {
		t.Fatalf("seeded member count = %d, want at least the default pair", members)
	}

	var unsupported strings.Builder
	if rc := cmdMigrate(func(name string) string {
		if name == envDatabaseURL {
			return "postgres://example/db"
		}
		return ""
	}, &unsupported); rc != 1 || !strings.Contains(unsupported.String(), "supports sqlite DSNs only") {
		t.Fatalf("unsupported cmdMigrate = (%d, %q), want rc 1 and sqlite-only error", rc, unsupported.String())
	}
}
