package main

// migrate.go — the goose migration base over modernc.org/sqlite (cgo-free).
// The migrations are EMBEDDED so the shipped binary carries its own schema
// history (no on-disk migrations dir needed at runtime).
//
// migrations/ carries the real schema (00001_schema.sql — the retired Python
// implementation's tables reshaped per the ontology blueprint; dal.go is the
// access layer). sqlite-only for now: a postgres DSN is an explicit error
// until a postgres driver story is decided.

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
	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// openSQLite opens (creating parent dirs — dal.engine's zero-setup first boot)
// the SQLite database at path via the modernc driver, as the WRITE pool. Pair it
// with openSQLiteReadPool (serve) or use it alone (CLI one-shots, tests).
//
// WHY THE POSTURE CHANGED (T-dd7a, owner 2026-07-31). The previous posture was
// ONE pooled connection in the DEFAULT journal mode, on the reasoning that "a
// second pooled conn only manufactures SQLITE_BUSY between our own requests".
// That reasoning was correct — and it is exactly why WAL is turned on in the same
// change, never a connection cap on its own. What the old comment did not say is
// the price it was paying: with one connection for the whole process EVERY request
// serialises at the database, so the server answered one request at a time. That
// was measurable from outside and the owner hit it: a 0.1 kB endpoint took 904 ms
// while a 407 kB one took 85 ms in the same session, and two endpoints returning
// 3 kB and 10 kB finished within 1 ms of each other — which only happens when both
// are waiting on the same gate.
//
// WHAT THIS HANDLE IS. The write side, and it stays narrow on purpose:
//
//   - journal_mode(WAL) — a PERSISTENT property of the FILE, not a per-connection
//     setting: set once, it stays set, and asking again on every open is a no-op.
//     WAL is what lets readers run while a writer is in flight at all.
//
//   - ONE connection — SQLite has a single writer. Handing out more write
//     connections would not buy parallel writes, it would only move the queue from
//     Go's pool (where waiting is free and ordered) into SQLite's lock manager
//     (where waiting is a busy-loop and losing is an error).
//
//   - _txlock=immediate — every non-read-only transaction on this handle begins
//     BEGIN IMMEDIATE instead of the driver default BEGIN DEFERRED. The mechanism:
//     our transactions read and then write inside one tx
//     (SaveWithDocumentHistories snapshots the live document inside the very
//     transaction that overwrites it), a DEFERRED tx therefore has to UPGRADE a
//     read lock into a write lock, and SQLite answers an upgrade conflict with an
//     instant SQLITE_BUSY that busy_timeout does NOT wait out (retrying would
//     deadlock two upgraders, so the engine does not try). IMMEDIATE takes the
//     write lock at BEGIN, which busy_timeout does cover.
//
//     🔴 BE PRECISE ABOUT WHAT THIS BUYS, because the obvious reading is wrong and
//     was measured wrong: it does NOT protect our own concurrent writers. The
//     one-connection cap already makes an in-process upgrade conflict structurally
//     impossible, and that is not a deduction — dropping `_txlock=immediate` from
//     this DSN leaves TestSaveWithDocumentHistoryUnderConcurrentWritersKeepsThe-
//     ChainContiguous GREEN (measured 2026-08-01). What it protects is a SECOND
//     HANDLE on the same file: `ocserverd backup` (cmdBackup opens its own), a
//     shell sqlite3, a future second pool. Against those, IMMEDIATE is what makes
//     busy_timeout apply at all — without it such a writer does not wait, it
//     fails. The discriminating test is therefore two independent handles
//     (TestWritePool_ReadThenWriteNeverHitsBusy), not two goroutines.
//
//     It is also the guard that keeps this design correct if anyone ever raises
//     the cap: `WAL + raise the cap` WITHOUT it is exactly the change that was
//     tried and disproved (branch t-dd7a-wal, commit 25bf66d — full CI red on
//     SQLITE_BUSY).
//
//   - busy_timeout(5000) — the belt for those same outside writers. Our own
//     writers never reach it: the cap queues them in Go, where waiting is free.
func openSQLite(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+path+
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode("+sqliteJournalMode+")&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// sqliteJournalMode is the mode the FILE must be in for any of this to be safe,
// and the value assertJournalMode holds the live database to at boot.
const sqliteJournalMode = "WAL"

// sqliteMaxReadConns bounds the READ pool. Small on purpose: the point is to stop
// reads queueing behind one another, not to open as many connections as possible
// — each carries its own page cache, and past a handful the writer, not the pool,
// is the limit.
const sqliteMaxReadConns = 8

// openSQLiteReadPool opens the SAME file as a several-connection, READ-ONLY pool.
//
// Call it AFTER the write pool has migrated the file: `mode=ro` never creates a
// database, and a read-only connection cannot recover a WAL either — the write
// pool opening first is what guarantees both.
//
// 🔴 `mode=ro` is a GUARD, not a micro-optimisation. The way a reader/writer split
// fails is not a crash, it is a write quietly issued on the reader — which in WAL
// would either take the write lock from an unexpected place or (in a read-then-write
// transaction) hit the very SQLITE_BUSY this design exists to avoid, intermittently
// and under load only. Opening the pool read-only at the SQLite level turns that
// class of mistake into an immediate, named error ("attempt to write a readonly
// database") on the very first attempt, in every environment, including tests.
//
// It deliberately does NOT ask for journal_mode: that pragma is a WRITE to the
// file's header, so requesting it here would fail on a read-only connection. The
// mode is the file's property and the write pool already set it.
func openSQLiteReadPool(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(sqliteMaxReadConns)
	return db, nil
}

// assertJournalMode asks the DATABASE which journal mode it is ACTUALLY in.
//
// It exists because the failure it guards is invisible: a malformed pragma is not
// an error, SQLite silently ignores it, and the only symptom is the request
// queueing this ticket removed — correct data, no error, no log line, nothing to
// notice. Asserting the DSN string would be worthless; a typo'd pragma satisfies a
// string check exactly as happily as a correct one. The only honest question is the
// one put to the database itself.
func assertJournalMode(db *sql.DB, want string) (got string, err error) {
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&got); err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return got, fmt.Errorf("journal_mode is %q, want %q", got, want)
	}
	return got, nil
}

// runMigrations applies every embedded goose migration (goose up) to db.
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

// cmdBackup is backup trigger ① (backup.go): take ONE snapshot by hand, right
// now. This is the seam a human uses before doing something risky — and it runs
// the SAME runDatabaseBackup the cadence runs, so what a person verified by hand
// is exactly what happens unattended at 4am.
//
// It resolves the DSN the same way serve does, so it always backs up THIS
// instance's database (namespaced instances included) rather than a path someone
// typed.
func cmdBackup(env func(string) string, out io.Writer) int {
	cfg, warnings, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	path, ok := sqliteFilePath(resolveDSN(env, cfg))
	if !ok {
		fmt.Fprintln(out, "[ocserverd] FATAL: backup supports sqlite DSNs only")
		return 1
	}
	if _, err := os.Stat(path); err != nil {
		// Creating the file here would produce a "backup" of an empty database
		// and report success — the exact shape of a retreat point that is not
		// one.
		fmt.Fprintf(out, "[ocserverd] FATAL: no database at %s (nothing to back up)\n", path)
		return 1
	}
	db, err := openSQLite(path)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", path, err)
		return 1
	}
	defer db.Close()
	res, err := runDatabaseBackup(db, path, backupReasonManual, time.Now())
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

// cmdMigrate resolves the DSN (env → oc.toml → sqlite convention default) and
// runs goose up against it.
func cmdMigrate(env func(string) string, out io.Writer) int {
	cfg, warnings, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn := resolveDSN(env, cfg)
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
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	// Out-of-box seed (idempotent): Mira + the server-self warden — the same
	// seed serve start ensures, so a bare `migrate` yields a bootable roster.
	if err := seedOutOfBox(NewDAL(db)); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: seed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "[ocserverd] migrations applied + seed ensured (%s)\n", path)
	return 0
}
