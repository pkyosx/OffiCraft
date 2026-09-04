package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-74: `ocserverd migrate` must take a pre-migration backup, and it must take
// it BEFORE goose.
//
// 🔴 WHY THIS TEST DOES NOT ASSERT "a backup file exists".
//
// The bug this guards is not "no backup" in general — it is a backup taken at
// the WRONG MOMENT. A snapshot written after `goose up` has already committed
// is not a retreat point from the migration; it is a copy of the outcome the
// operator would be trying to escape. A test that only asserts a
// `-premigration.db` file appeared passes just as happily with the call moved
// below runMigrations, so it has zero discriminating power over exactly the
// failure worth guarding.
//
// So the assertion is made on the CONTENT of the snapshot, which carries its
// own timestamp in a form that cannot be faked by ordering luck:
//
//   - The database handed to `migrate` is seeded with a marker table and
//     deliberately has NO goose state (`goose_db_version` does not exist).
//   - After cmdMigrate returns, the LIVE database must have `goose_db_version`
//     — proof that goose actually ran in this process, so the next assertion is
//     not vacuously true against a migration that never happened.
//   - The backup file must contain the marker table (it is a snapshot of OUR
//     database, not of something else) and must NOT contain
//     `goose_db_version`.
//
// The last pair is the ordering discriminator: `goose_db_version` is created by
// goose itself. Its absence from the snapshot is only possible if VACUUM INTO
// read the file at a point in time when goose had not yet touched it. Move the
// backup call below runMigrations and the snapshot necessarily contains the
// table, and this test goes red — which is the mutant this file exists to
// catch.
func TestMigrateTakesPreMigrationBackupBeforeGoose(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(dataDir, "officraft.db")

	// A non-empty database with NO goose state. backupBeforeMigrations
	// deliberately skips a zero-length file, so the marker table is doing double
	// duty: it makes the file worth protecting, and it is the fingerprint that
	// proves the snapshot is of this file.
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

	env := func(k string) string {
		switch k {
		case envDatabaseURL:
			return "sqlite:///" + dbPath
		case envConfigPath:
			return filepath.Join(root, "no-such-oc.toml")
		}
		return ""
	}
	var out strings.Builder
	if rc := cmdMigrate(env, &out); rc != 0 {
		t.Fatalf("cmdMigrate rc=%d, want 0; output:\n%s", rc, out.String())
	}

	// Negative control for the assertion below: if goose had not run, "the
	// snapshot has no goose_db_version" would be true for the wrong reason.
	if !hasTable(t, dbPath, "goose_db_version") {
		t.Fatalf("goose did not run: live database has no goose_db_version after cmdMigrate")
	}

	backupPath := solePreMigrationBackup(t, backupDirFor(dbPath))

	if !hasTable(t, backupPath, "t74_marker") {
		t.Fatalf("backup %s does not contain t74_marker: it is not a snapshot of the migrated database", backupPath)
	}
	if hasTable(t, backupPath, "goose_db_version") {
		t.Fatalf("backup %s contains goose_db_version: the snapshot was taken AFTER goose ran, "+
			"so it is a copy of the migration's outcome rather than a retreat point from it", backupPath)
	}
}

// solePreMigrationBackup returns the one `-premigration.db` file in dir, failing
// with the full directory listing when there is not exactly one.
//
// Shared by BOTH door tests (migrate here, serve in
// serve_backup_order_t74_test.go) on purpose: one definition of "the
// pre-migration snapshot" means the two doors cannot drift into two different
// standards. Its messages therefore name no particular caller.
func solePreMigrationBackup(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read backup dir %s: %v (this door took no pre-migration backup at all)", dir, err)
	}
	var found []string
	var all []string
	for _, e := range entries {
		all = append(all, e.Name())
		if e.IsDir() {
			continue
		}
		if backupReasonIn(e.Name()) == backupReasonPreMigration {
			found = append(found, e.Name())
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly 1 %s backup in %s, got %d; directory holds: %v",
			backupReasonPreMigration, dir, len(found), all)
	}
	return filepath.Join(dir, found[0])
}

// hasTable opens the sqlite file at path and reports whether it has the named
// table. It opens the FILE (not a handle already in play), because the whole
// question is what is written on disk in that snapshot.
func hasTable(t *testing.T, path, table string) bool {
	t.Helper()
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query sqlite_master in %s: %v", path, err)
	}
	return true
}
