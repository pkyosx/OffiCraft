package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-74 (second half): the SERVE door's ordering, pinned.
//
// 🔴 WHY THIS TEST EXISTS AT ALL. backupBeforeMigrations' doc comment says both
// of its callers MUST call it before their goose call. Until this file that
// sentence was a NORM with no mechanical guarantee behind it: a comment that
// predicts a harm while nothing anywhere goes red is an unpaid ticket, and the
// change that added the second caller is exactly what made it matter. With one
// caller the ordering was a property of a single line; with two it became a
// contract every caller has to keep, and contracts get broken by the next
// person, not by the one who wrote them down.
//
// The migrate door is guarded by TestMigrateTakesPreMigrationBackupBeforeGoose.
// This is the SAME criterion applied to serve, through the SAME helpers
// (hasTable, solePreMigrationBackup) so the two doors cannot drift apart into
// two different definitions of "before goose": the snapshot must NOT contain
// goose_db_version, and the live database MUST contain it afterwards so that
// the first clause cannot pass vacuously against a migration that never ran.
//
// 🔴 HOW IT RUNS cmdServe WITHOUT STARTING A SERVER. cmdServe does its whole
// boot — backup, goose, seed, settings, keyring, boot reconciles — BEFORE it
// binds, and it is the bind that blocks forever. So the test HOLDS the port
// first: cmdServe performs every pre-bind step for real, fails net.Listen, and
// RETURNS rc 1 instead of entering http.Serve. Nothing blocks, nothing waits,
// and the only listener is the one this test owns and closes. This is the
// established idiom in this package, not one invented here — see
// TestServeBootRetiresOrphanReplyCards (server_test.go), and the same trick in
// wal_pool_test.go and backup_health_tda06_test.go.
//
// ⚠️ The database is deliberately NOT pre-migrated (unlike those tests, which
// call runMigrations in their fixture). A DB that already carries
// goose_db_version when serve starts would make the snapshot contain it no
// matter where the backup call sits, which would turn this test red for a
// reason that has nothing to do with ordering.
func TestServeTakesPreMigrationBackupBeforeGoose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "boot.db")

	// A non-empty database with NO goose state. The marker table makes the file
	// worth protecting (backupBeforeMigrations skips a zero-length one) and is
	// the fingerprint proving the snapshot is of THIS file.
	seed, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE t74_marker (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create marker: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO t74_marker (id) VALUES (1)`); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}
	if hasTable(t, dbPath, "goose_db_version") {
		t.Fatalf("precondition broken: the seeded database already has goose_db_version")
	}

	// Hold the port cmdServe is about to want, so the boot runs and the bind is
	// what ends it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	cfgPath := filepath.Join(dir, "oc.toml")
	if err := os.WriteFile(cfgPath,
		[]byte(fmt.Sprintf("[server]\nport = %d\n", port)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out strings.Builder
	rc := cmdServe(envOf(map[string]string{
		"OC_CONFIG":       cfgPath,
		"OC_DATABASE_URL": "sqlite:///" + dbPath,
	}), true, true, &out)
	if rc != 1 {
		t.Fatalf("the held port must make serve exit 1 (boot ran, bind failed), got %d\n%s",
			rc, out.String())
	}
	// Prove we exited for the reason we designed for. Without this, an EARLIER
	// fatal — one that returns 1 before goose or even before the backup — would
	// look identical to a clean pre-bind boot, and every assertion below would
	// be measuring a boot that never happened.
	if !strings.Contains(out.String(), "already in use") {
		t.Fatalf("expected the bind failure to be the reason serve exited, not an earlier fatal:\n%s",
			out.String())
	}

	// Negative control for the ordering assertion: if goose had not run, "the
	// snapshot has no goose_db_version" would be true for the wrong reason.
	if !hasTable(t, dbPath, "goose_db_version") {
		t.Fatalf("goose did not run: live database has no goose_db_version after cmdServe")
	}

	backupPath := solePreMigrationBackup(t, backupDirFor(dbPath))

	if !hasTable(t, backupPath, "t74_marker") {
		t.Fatalf("backup %s does not contain t74_marker: it is not a snapshot of the migrated database", backupPath)
	}
	if hasTable(t, backupPath, "goose_db_version") {
		t.Fatalf("backup %s contains goose_db_version: serve's snapshot was taken AFTER goose ran, "+
			"so it is a copy of the migration's outcome rather than a retreat point from it", backupPath)
	}
}
