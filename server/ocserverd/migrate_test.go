// Skeleton generated from server/ocserverd/migrate.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestOpenSQLite(t *testing.T) {
	t.Skip("TODO: openSQLite opens (creating parent dirs — dal.engine's zero-setup first boot) the SQLite database at path via the modernc driver, as the WRITE pool.")
}

func TestOpenSQLiteReadPool(t *testing.T) {
	t.Skip("TODO: openSQLiteReadPool opens the SAME file as a several-connection, READ-ONLY pool.")
}

func TestAssertJournalMode(t *testing.T) {
	t.Skip("TODO: assertJournalMode asks the DATABASE which journal mode it is ACTUALLY in.")
}

func TestRunMigrations(t *testing.T) {
	t.Skip("TODO: runMigrations applies every goose migration (goose up) to db.")
}

func TestCmdBackup(t *testing.T) {
	t.Skip("TODO: cmdBackup is backup trigger ① (backup.go): take ONE snapshot by hand, right now.")
}

func TestCmdMigrate(t *testing.T) {
	t.Skip("TODO: cmdMigrate resolves the DSN (env → oc.toml → sqlite convention default) and runs goose up against it.")
}
