package main

// T-186 — migration 00105_drop_legacy_memory.
//
// 🔴 WHAT THESE TESTS ARE FOR. Three of the four things 00105 removes fail
// SILENTLY when the migration is wrong, and the fourth fails on a driver the
// author cannot reach from a shell:
//
//   · the document_history rows. Deleting the table and the column while
//     leaving the retained revisions behind produces a database that boots,
//     serves, and passes every other test in this package. The only thing that
//     could ever notice is a test that counts those rows, so one does — and it
//     seeds a task_manual_sop row in the same fixture, because a predicate
//     widened to `LIKE 'task_manual%'` would take the SURVIVING series too and
//     nothing else in the tree would object.
//
//   · ALTER TABLE ... DROP COLUMN. The sqlite3 CLI accepts it; the server has
//     never once used the sqlite3 CLI. migrate.go runs goose over
//     modernc.org/sqlite, a separate pure-Go implementation of the engine, and
//     00062 exists precisely because an ALTER that "obviously works" did not.
//     These tests reach the column through openSQLite, which is the same
//     handle serve uses, so "the drop works" is asserted about the shipping
//     driver rather than about a binary on one laptop.
//
//   · the two setting rows. get_settings enumerates the key space code-side,
//     so a row for a retired key is invisible through the API and cannot be
//     deleted through it either. Nothing reads it, nothing reports it, and
//     nothing goes red.
//
//   · the Down. Its job is not to restore data (it cannot) but to let an OLDER
//     binary start, and an older binary's first read of either surface is a
//     SELECT naming the table and the column. A Down that left the schema
//     alone would be a rollback that does not roll anything back, and the only
//     way to find that out otherwise is to perform it on a station.

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// t186UpToJustBefore105 migrates as far as 104 — the world 00105 will actually
// meet, with `lessons` and `task_manual.learnings` still present. It never
// migrates to head: asserting against head would make this file fail on the
// next unrelated migration.
//
// 🔴 IT DOES NOT ASSERT WHICH VERSION IT LANDED ON, and that is not laziness.
// `UpTo(104)` means "apply every migration numbered 104 or below", so the
// version this returns is whatever the highest migration below 00105 happens to
// be — it moves on its own as neighbouring numbers land, with no edit here.
// Callers read the version back instead of naming it.
func t186UpToJustBefore105(t *testing.T, db *sql.DB) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 104); err != nil {
		t.Fatalf("goose up to 104: %v", err)
	}
}

func t186OpenJustBefore105(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t186UpToJustBefore105(t, db)
	return db
}

func t186UpTo105(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.UpTo(db, "migrations", 105); err != nil {
		t.Fatalf("goose up to 105: %v", err)
	}
}

// t186Version reads the version goose itself believes the database is at. It is
// the only honest answer to "did the upgrade happen": the database FILE does not
// have to change at all in WAL mode until a checkpoint, so mtime, size and a
// hash of the main file can every one of them be identical across a migration
// that really ran.
func t186Version(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	v, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("goose version: %v", err)
	}
	return v
}

func t186TableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name).Scan(&n); err != nil {
		t.Fatalf("sqlite_master for %s: %v", name, err)
	}
	return n > 0
}

func t186ColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var got string
		if err := rows.Scan(&got); err != nil {
			t.Fatalf("scan column of %s: %v", table, err)
		}
		if got == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns of %s: %v", table, err)
	}
	return false
}

func t186Count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// t186SeedLegacyMemory fills a pre-105 database with one of everything the
// migration is supposed to remove, plus one of everything adjacent that it must
// leave alone. Every row is written through the same handle the migration will
// use, so the fixture is a database an upgrade could really arrive at.
func t186SeedLegacyMemory(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO lessons (role_key, text, tombstoned) VALUES
		 ('r-alpha', 'what the alpha role learned', 0),
		 ('r-beta',  'what the beta role learned',  0)`); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO task_manual
		   (type_key, purpose, fields, sop_md, learnings, assignee, updated_ts, display_name)
		 VALUES ('tm-alpha', 'p', '[]', 'the sop', 'the accumulated learnings', '{}', 1.0, 'Alpha')`,
	); err != nil {
		t.Fatalf("seed task_manual: %v", err)
	}
	for _, r := range []struct{ kind, key string }{
		{"lessons", "r-alpha"},
		{"lessons", "r-beta"},
		{"task_manual_learnings", "tm-alpha"},
		{"task_manual_sop", "tm-alpha"},
		{"insight", "r-alpha"},
	} {
		if _, err := db.Exec(
			`INSERT INTO document_history
			   (document_kind, document_key, content_json, created_ts, actor_id)
			 VALUES (?, ?, '{}', 1.0, 'owner')`, r.kind, r.key); err != nil {
			t.Fatalf("seed document_history %s/%s: %v", r.kind, r.key, err)
		}
	}
	for _, k := range []string{
		"doc.cap_chars.learning",
		"doc.cap_chars.manual_learnings",
		"doc.cap_chars.manual_sop",
	} {
		if _, err := db.Exec(
			`INSERT INTO setting (key, value, updated_at) VALUES (?, '12000', 1.0)`,
			k); err != nil {
			t.Fatalf("seed setting %s: %v", k, err)
		}
	}
}

// TestMigration00105DropsTheLegacyMemoryStorage — the whole upgrade path, on a
// database that carries real legacy content rather than an empty schema.
//
// 🔴 THE PRECONDITION BLOCK IS NOT CEREMONY. Every assertion after the upgrade
// is an ABSENCE, and an absence proves nothing unless the thing was present a
// moment earlier: a fixture that silently failed to seed would let this test
// pass against a migration that did nothing at all. So the table, the column
// and both row families are read back BEFORE goose is asked to move.
func TestMigration00105DropsTheLegacyMemoryStorage(t *testing.T) {
	db := t186OpenJustBefore105(t, "t186-up.db")
	t186SeedLegacyMemory(t, db)

	before := t186Version(t, db)
	if before == 0 || before >= 105 {
		t.Fatalf("precondition: goose reports version %d, want a real version "+
			"below 105 for the fixture to be a pre-upgrade database at all", before)
	}
	if !t186TableExists(t, db, "lessons") {
		t.Fatal("precondition: the lessons table is already gone at 104")
	}
	if !t186ColumnExists(t, db, "task_manual", "learnings") {
		t.Fatal("precondition: task_manual.learnings is already gone at 104")
	}
	if n := t186Count(t, db, `SELECT COUNT(*) FROM lessons`); n != 2 {
		t.Fatalf("precondition: lessons holds %d rows, want the 2 seeded", n)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM document_history
		  WHERE document_kind IN ('lessons', 'task_manual_learnings')`); n != 3 {
		t.Fatalf("precondition: %d legacy history rows, want the 3 seeded", n)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM setting
		  WHERE key IN ('doc.cap_chars.learning', 'doc.cap_chars.manual_learnings')`); n != 2 {
		t.Fatalf("precondition: %d legacy cap rows, want the 2 seeded", n)
	}

	t186UpTo105(t, db)

	if v := t186Version(t, db); v != 105 {
		t.Fatalf("after the upgrade goose reports version %d, want 105 (it was %d "+
			"before). This is the only indicator that answers honestly: in WAL "+
			"mode the main database file can be byte-identical across a "+
			"migration that ran.", v, before)
	}
	if t186TableExists(t, db, "lessons") {
		t.Fatal("the lessons table survived the upgrade")
	}
	if t186ColumnExists(t, db, "task_manual", "learnings") {
		t.Fatal("task_manual.learnings survived the upgrade — ALTER TABLE ... " +
			"DROP COLUMN did not take effect on modernc.org/sqlite, so 00105 " +
			"needs 00062's create/copy/drop/rename rebuild instead")
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM document_history
		  WHERE document_kind IN ('lessons', 'task_manual_learnings')`); n != 0 {
		t.Fatalf("%d retained revisions of the removed documents are still in the "+
			"journal. Nothing goes red on this: they are rows naming a restore "+
			"target that no longer exists.", n)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM setting
		  WHERE key IN ('doc.cap_chars.learning', 'doc.cap_chars.manual_learnings')`); n != 0 {
		t.Fatalf("%d cap rows for retired keys remain. settings.go no longer "+
			"declares them, so they are neither visible nor deletable through "+
			"get_settings / update_settings.", n)
	}

	// The neighbours. Each one shares a prefix, a table or a key space with
	// something removed above, and each is the row a widened predicate takes.
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM document_history WHERE document_kind = 'task_manual_sop'`); n != 1 {
		t.Fatalf("the surviving manual series holds %d rows, want 1 — a predicate "+
			"widened to LIKE 'task_manual%%' takes the live SOP history too", n)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM document_history WHERE document_kind = 'insight'`); n != 1 {
		t.Fatalf("the insight history holds %d rows, want 1 untouched", n)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM setting WHERE key = 'doc.cap_chars.manual_sop'`); n != 1 {
		t.Fatalf("the surviving cap row count is %d, want 1 — the IN-list must not "+
			"reach doc.cap_chars.manual_sop", n)
	}
	var sop string
	if err := db.QueryRow(
		`SELECT sop_md FROM task_manual WHERE type_key = 'tm-alpha'`).Scan(&sop); err != nil {
		t.Fatalf("read the manual back after the column drop: %v", err)
	}
	if sop != "the sop" {
		t.Fatalf("the manual's sop_md reads %q after the column drop, want %q — "+
			"the surviving columns must come through the drop unchanged", sop, "the sop")
	}
}

// TestMigration00105DownRestoresTheShapeAnOlderBinaryReads — the rollback's only
// job, stated as the query an older binary actually issues.
//
// 🔴 IT ASSERTS THE SHAPE AND THE EMPTINESS, IN THAT ORDER. A Down that
// recreated the table would pass a test that only looked for the table; what
// makes the rollback honest is that it hands the old binary an EMPTY document
// rather than a synthesized one, and that half is asserted too so nobody later
// "improves" the Down into inventing content.
func TestMigration00105DownRestoresTheShapeAnOlderBinaryReads(t *testing.T) {
	db := t186OpenJustBefore105(t, "t186-down.db")
	t186SeedLegacyMemory(t, db)
	before := t186Version(t, db)
	t186UpTo105(t, db)

	if t186TableExists(t, db, "lessons") {
		t.Fatal("precondition: the Up left the lessons table in place")
	}

	if err := goose.DownTo(db, "migrations", 104); err != nil {
		t.Fatalf("goose down to 104: %v", err)
	}

	if v := t186Version(t, db); v != before {
		t.Fatalf("after the rollback goose reports version %d, want the %d it was "+
			"at before the Up", v, before)
	}
	// The two reads an older binary performs on its first touch of either
	// surface. Against a no-op Down these are "no such table" / "no such
	// column" and the process does not serve.
	var text string
	err := db.QueryRow(`SELECT text FROM lessons WHERE role_key = 'r-alpha'`).Scan(&text)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("an older binary's read of lessons fails after the rollback: %v", err)
	}
	if err == nil {
		t.Fatalf("lessons still holds a row for r-alpha (%q). The Up kept no copy "+
			"of the text, so any content here was synthesized by the Down.", text)
	}
	var learnings string
	if err := db.QueryRow(
		`SELECT learnings FROM task_manual WHERE type_key = 'tm-alpha'`).Scan(&learnings); err != nil {
		t.Fatalf("an older binary's read of task_manual.learnings fails after the "+
			"rollback: %v", err)
	}
	if learnings != "" {
		t.Fatalf("the restored learnings column reads %q, want the empty string — "+
			"the Up deleted the content and kept no copy, so a rollback that "+
			"produced text would be inventing it", learnings)
	}
	if n := t186Count(t, db,
		`SELECT COUNT(*) FROM document_history
		  WHERE document_kind IN ('lessons', 'task_manual_learnings')`); n != 0 {
		t.Fatalf("the rollback resurrected %d retained revisions. The Up deleted "+
			"them irreversibly; a Down that produced any is producing rows that "+
			"were never there.", n)
	}
}
