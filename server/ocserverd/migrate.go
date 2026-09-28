package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// openSQLite opens the WRITE pool.
//
//   - ONE connection: SQLite has a single writer; more write connections only
//     move the queue from Go's pool (free, ordered) into SQLite's lock manager
//     (busy-loop, errors).
//   - WAL and _txlock=immediate are ONE decision. Our transactions read then
//     write; under WAL a DEFERRED tx must upgrade its lock, and an upgrade
//     conflict is an instant SQLITE_BUSY that busy_timeout does NOT wait out
//     (measured: WAL + DEFERRED failed 2 of 8, WAL + IMMEDIATE 0 of 8).
//     IMMEDIATE protects a SECOND handle on the file (`ocserverd backup`, a shell
//     sqlite3), not our own writers, which the cap already serialises. Raising
//     the cap without it was tried and disproved (commit 25bf66d, CI red on
//     SQLITE_BUSY). Cost: a Begin against a stuck external writer waits the full
//     busy_timeout, then fails.
func openSQLite(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", sqliteWriteDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func sqliteWriteDSN(path string) string {
	return "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(" + sqliteJournalMode + ")&_txlock=immediate"
}

const sqliteJournalMode = "WAL"

// sqliteMaxReadConns is small on purpose: each connection carries its own page
// cache, and past a handful the writer, not the pool, is the limit.
const sqliteMaxReadConns = 8

// openSQLiteReadPool must be opened AFTER the write pool has migrated the file:
// `mode=ro` never creates a database and cannot recover a WAL. `mode=ro` is a
// guard — a write issued on the reader fails at once ("attempt to write a
// readonly database") instead of intermittently under load — and for the same
// reason this pool never asks for journal_mode (a header write).
//
// 🔴 This pool is what can grow "-wal" without bound: auto-checkpoint cannot run
// while a reader pins an old snapshot, and with journal_size_limit at -1 the WAL
// never shrinks back (measured 4 MB → 196 MB with one read tx held open).
// ENFORCED: no explicit transaction on this pool. NOT enforced, a discipline:
// every Query's Rows are consumed promptly — held-open Rows do the same damage.
func openSQLiteReadPool(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteReadDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(sqliteMaxReadConns)
	return db, nil
}

func sqliteReadDSN(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&mode=ro"
}

// assertJournalMode asks the DATABASE, because a malformed pragma is silently
// ignored: a DSN string check passes a typo, and the only symptom is request
// queueing.
func assertJournalMode(db *sql.DB, want string) (got string, err error) {
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&got); err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return got, fmt.Errorf("journal_mode is %q, want %q", got, want)
	}
	return got, nil
}

// runMigrations: goose also runs Go migrations registered via
// goose.AddNamedMigrationContext, which live in this package, not under
// migrations/ (that directory is embedded as *.sql).
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

func cmdBackup(env func(string) string, out io.Writer) int {
	_, dsn, rc := announceResolution("backup", env, out)
	if rc != 0 {
		return rc
	}
	path, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintln(out, "[ocserverd] FATAL: backup supports sqlite DSNs only")
		return 1
	}
	if _, err := os.Stat(path); err != nil {
		// Opening a missing file would create it, and the "backup" of an empty
		// database would report success.
		fmt.Fprintf(out, "[ocserverd] FATAL: no database at %s (nothing to back up)\n", path)
		return 1
	}
	db, err := openSQLite(path)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", path, err)
		return 1
	}
	defer db.Close()
	res, err := runDatabaseBackup(db, path, backupReasonManual, time.Now(), liveBackupRetain(db))
	logBackupOutcome(res, err)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] backup FAILED: %v\n", err)
		return 1
	}
	if res.Skipped != "" {
		fmt.Fprintf(out, "[ocserverd] backup skipped: %s\n", res.Skipped)
		return 1
	}
	fmt.Fprintf(out, "[ocserverd] backup ok: %s (%d MB in %s)\n", res.Path, res.Bytes>>20, res.Took.Round(time.Millisecond))
	return 0
}

func cmdMigrate(env func(string) string, out io.Writer) int {
	_, dsn, rc := announceResolution("migrate", env, out)
	if rc != 0 {
		return rc
	}
	path, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintf(out, "[ocserverd] FATAL: migrate supports sqlite DSNs only for now (got %q); postgres lands with the M3 dal step\n", dsn)
		return 1
	}
	db, err := openSQLite(path)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", path, err)
		return 1
	}
	defer db.Close()
	backupBeforeMigrations(db, path, time.Now())
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	// The same out-of-box seed serve start ensures, so a bare `migrate` yields
	// a bootable roster.
	if err := seedOutOfBox(NewDAL(db)); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: seed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "[ocserverd] migrations applied + seed ensured (%s)\n", path)
	return 0
}
