package main

// migration_00088_task_executor_kind_test.go — T-101.
//
// 00088 renames the TASK executor kind 'member' to 'staff'. SQLite can alter kind-vocab-guard:legacy
// neither a CHECK nor a DEFAULT in place, so the migration rebuilds `task`:
// create task_rebuild with CHECK (executor_kind IN ('staff','outsource')) and
// DEFAULT 'staff', copy every row through a named 33-column INSERT…SELECT that
// maps 'member' -> 'staff' in BOTH executor_kind and reassigned_from_kind, kind-vocab-guard:legacy
// DROP TABLE task, RENAME, and re-create
//
//	CREATE INDEX idx_task_status ON task (status)
//	CREATE INDEX idx_task_dedupe ON task (type_key, dedupe_key)
//
// which the DROP TABLE destroyed. A separate `json_set` UPDATE moves the same
// vocabulary inside `task_manual.assignee`. The Down reverses all of it.
//
// 🔴 WHICH MUTATION EACH TEST IS THE GUARD FOR. Every test below was measured
// against a real edit to the migration SQL; a test no mutant can redden is
// decoration.
//
//	TestMigration00088UpRenamesBothKindColumns
//	  → either CASE is dropped, inverted or widened: 'member' left behind, kind-vocab-guard:legacy
//	    'outsource' swept up by a blanket rename, or the empty string (the
//	    "never reassigned" marker, which reassigned_from_kind admits and no
//	    CHECK guards) rewritten into a word.
//
//	TestMigration00088UpPreservesEveryColumn
//	  → a column dropped from the INSERT or the SELECT list (its value silently
//	    replaced by the rebuild's DEFAULT for every existing row), or two
//	    same-typed columns transposed between the two lists. The column list is
//	    READ BACK FROM THE LIVE SCHEMA rather than typed here, so a column added
//	    by any earlier migration is covered the day it lands.
//
//	TestMigration00088IndexSurvivesUp
//	  → either CREATE INDEX after the RENAME is missing. Nothing raises when
//	    that happens: every status filter and every dedupe probe silently
//	    becomes a table scan.
//
//	TestMigration00088PreservesTheShapeOfTaskWhateverItIs
//	  → the same two failures with NO list in the test at all: `task` is
//	    photographed (columns in order + every index with its DDL) immediately
//	    before and immediately after the Up, and the photographs must match.
//
//	TestMigration00088DownRestoresThePreUpState
//	TestMigration00088DownRenamesBothKindColumnsBack
//	TestMigration00088AssigneeJSONSurvivesBothDirections
//	  → the ~90-line Down block, which until this file existed was executed by
//	    NOTHING in the tree. Its own column lists can drift, its CASEs can be
//	    missing, and — the quiet one — its two trailing CREATE INDEXes can be
//	    absent, leaving a rolled-back database that looks fine and scans.
//
//	TestMigration00088UpDownUpIsStable
//	  → the round trip is not idempotent: residue left by the Down, or a
//	    mapping that is not 1:1 in both directions.
//
// 🔴 WHAT THIS FILE DELIBERATELY DOES NOT REPEAT.
// TestUpgradingADatabaseThatAlreadyHasTasks (dal_task_id_seq_t52917b_test.go)
// already asserts, on the Up only: that no task row keeps the pre-rename value
// in either kind column and that three seeded rows arrive on 'staff'; that the
// executor_kind DEFAULT lands on 'staff' for an INSERT omitting the column; and
// that the Up's json_set preserves member_id and an unknown key. Those are not
// re-asserted here. What this file adds is the DOWN direction of all of it, the
// empty-string case, the per-column preservation of all 33 columns, and both
// indexes in both directions.

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	migration00088Version = 88
	// The migration numbers are not contiguous (00086 is the one below 00088),
	// so this is a VERSION BOUNDARY rather than a file: goose.UpTo/DownTo take
	// "the highest version that may be applied", and 87 names the state with
	// everything below 00088 applied and 00088 not.
	migration00088PriorVersion = migration00088Version - 1

	migration00088OldKind = "member"
	migration00088NewKind = "staff"
)

// migration00088TaskColumns reads `task`'s columns, IN ORDER, out of the live
// database.
//
// 🔴 THE LIST IS READ, NEVER TYPED. 00076's companion file keeps a hand-written
// list and then needs two further tests to keep that list honest, because a
// column added by an earlier migration is invisible to a list nobody updated —
// the rebuild drops it for every existing row and every assertion compares the
// list against itself. Reading the list back from the schema the real earlier
// migrations built removes that whole failure mode: a 34th column enters the
// fixture, the seed and the comparison on the day it lands.
func migration00088TaskColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info('task')`)
	if err != nil {
		t.Fatalf("pragma_table_info(task): %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("pragma_table_info returned no columns for task — the reader is broken, not the schema")
	}
	return out
}

// migration00088ColumnTypes maps column name → declared type, used to mint a
// type-appropriate distinctive value for every column without naming any of
// them here.
func migration00088ColumnTypes(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT name, type FROM pragma_table_info('task')`)
	if err != nil {
		t.Fatalf("pragma_table_info(task): %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan column type: %v", err)
		}
		out[name] = strings.ToUpper(typ)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate column types: %v", err)
	}
	return out
}

// migration00088Spec is one fixture row's identity: the two kind values it
// carries before the Up, and whether its free text is plain or awkward.
type migration00088Spec struct {
	id             string
	executorKind   string
	reassignedKind string
	priority       string
	// awkward text — CJK, a newline, a tab, leading/trailing spaces — is what a
	// rebuild that "normalised" a value would visibly damage.
	awkward bool
}

// migration00088Fixture is the pre-00088 population. Between them the rows cover
// every value either kind column can hold at this version:
//
//	executor_kind         'member' (moves) and 'outsource' (must not) kind-vocab-guard:legacy
//	reassigned_from_kind  'member' (moves), 'outsource' (must not) and the kind-vocab-guard:legacy
//	                      EMPTY STRING (never reassigned — no CHECK guards this
//	                      column, so it is exactly the value a careless CASE
//	                      would rewrite)
//
// and the two axes are CROSSED, so a mutant that renamed one column by reading
// the other cannot hide behind rows where both happen to agree.
func migration00088Fixture() []migration00088Spec {
	return []migration00088Spec{
		{id: "t-88-member", executorKind: migration00088OldKind,
			reassignedKind: migration00088OldKind, priority: "high"},
		{id: "t-88-outsource", executorKind: "outsource", reassignedKind: "outsource", priority: "mid"},
		{id: "t-88-never-reassigned", executorKind: migration00088OldKind,
			reassignedKind: "", priority: "low"},
		{id: "t-88-crossed", executorKind: "outsource",
			reassignedKind: migration00088OldKind, priority: "frozen"},
		{id: "t-88-奇怪", executorKind: migration00088OldKind,
			reassignedKind: "outsource", priority: "mid", awkward: true},
	}
}

// migration00088Value mints the value a given fixture row carries in a given
// column. Every (row, column) pair gets a DIFFERENT value of the column's own
// declared type — that is what makes a transposed pair in the INSERT…SELECT
// visible rather than merely plausible, and what makes a column silently
// replaced by its DEFAULT stand out.
//
// The four columns the schema constrains (the primary key and the two CHECKed /
// renamed ones, plus priority's CHECK) are supplied from the spec; everything
// else is generated, so the fixture needs no column list of its own.
func migration00088Value(spec migration00088Spec, col, colType string, rowIdx, colIdx int) any {
	switch col {
	case "id":
		return spec.id
	case "executor_kind":
		return spec.executorKind
	case "reassigned_from_kind":
		return spec.reassignedKind
	case "priority":
		return spec.priority
	}
	seq := rowIdx*1000 + colIdx
	switch {
	case strings.Contains(colType, "INT"):
		return int64(700000 + seq)
	case strings.Contains(colType, "REAL"), strings.Contains(colType, "FLOA"), strings.Contains(colType, "DOUB"):
		// Powers of two so the value is exact in binary floating point and a
		// difference is a real difference rather than a rounding artefact.
		return float64(seq) + 0.5 + 0.0625
	default:
		if spec.awkward {
			return fmt.Sprintf("  %s／%s／%d\n第二行\t分隔  ", spec.id, col, seq)
		}
		return fmt.Sprintf("%s|%s|%d", spec.id, col, seq)
	}
}

// migration00088Manuals is the task_manual side, where the SAME vocabulary lives
// inside a JSON blob with no CHECK behind it.
//
// The unknown key is the load-bearing one: the migration uses json_set on
// `$.kind` rather than writing a fresh object precisely so that every OTHER key
// — member_id, and any key the validator stored verbatim without reading —
// survives byte-for-byte. A blob rewrite would satisfy "kind is staff" while
// silently dropping them.
func migration00088Manuals() []struct{ typeKey, before, afterUp string } {
	return []struct{ typeKey, before, afterUp string }{
		{
			typeKey: "tm-88-member",
			before:  `{"kind":"member","member_id":"m-88","unknown_key":7,"深":"值"}`,
			afterUp: `{"kind":"staff","member_id":"m-88","unknown_key":7,"深":"值"}`,
		},
		{
			typeKey: "tm-88-outsource",
			before:  `{"kind":"outsource","model":"opus","effort":"high"}`,
			afterUp: `{"kind":"outsource","model":"opus","effort":"high"}`,
		},
		{typeKey: "tm-88-unset", before: `{}`, afterUp: `{}`},
		{
			// Not JSON at all. The migration's WHERE has a json_valid() guard,
			// so this row must be left exactly as it is in BOTH directions —
			// a guard dropped from the WHERE turns json_set into NULL here and
			// the NOT NULL column refuses it.
			typeKey: "tm-88-not-json", before: `member`, afterUp: `member`,
		},
	}
}

// migration00088Norm renders a scanned cell as a comparable string, carrying its
// type along so a REAL rounded into an INTEGER, or a value flattened to the
// empty string, is a difference rather than a match.
func migration00088Norm(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("TEXT(%q)", string(t))
	case string:
		return fmt.Sprintf("TEXT(%q)", t)
	case int64:
		return fmt.Sprintf("INT(%d)", t)
	case float64:
		return fmt.Sprintf("REAL(%v)", t)
	default:
		return fmt.Sprintf("%T(%v)", v, v)
	}
}

// migration00088World brings a temp database to the state just BEFORE 00088 and
// seeds the fixture there.
//
// 🔴 THE DIRECTION MATTERS. It goes UP and STOPS, never up-past-and-back-down:
// DownTo would run 00088's own Down, and the "pre-Up schema" would then be a
// table the thing under test had just rebuilt from its own column list —
// agreeing with itself no matter what an earlier migration added. Stopping at
// 87 leaves `task` exactly as the REAL earlier migrations built it.
func migration00088World(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "task-executor-kind.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", migration00088PriorVersion); err != nil {
		t.Fatalf("up to %d: %v", migration00088PriorVersion, err)
	}

	cols := migration00088TaskColumns(t, db)
	types := migration00088ColumnTypes(t, db)
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	stmt := fmt.Sprintf(`INSERT INTO task (%s) VALUES (%s)`,
		strings.Join(cols, ", "), placeholders)
	for rowIdx, spec := range migration00088Fixture() {
		args := make([]any, 0, len(cols))
		for colIdx, c := range cols {
			args = append(args, migration00088Value(spec, c, types[c], rowIdx, colIdx))
		}
		if _, err := db.Exec(stmt, args...); err != nil {
			t.Fatalf("seed task %s: %v", spec.id, err)
		}
	}
	for _, m := range migration00088Manuals() {
		if _, err := db.Exec(
			`INSERT INTO task_manual (type_key, assignee) VALUES (?, ?)`, m.typeKey, m.before,
		); err != nil {
			t.Fatalf("seed task_manual %s: %v", m.typeKey, err)
		}
	}

	// ANTI-VACUITY: a fixture that failed to land would let every assertion
	// below pass over an empty table, which is indistinguishable from a working
	// migration. Both tables are empty at this version, so the counts are exact.
	assertCount := func(table string, want int) {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
			t.Fatalf("count seeded %s rows: %v", table, err)
		}
		if got != want {
			t.Fatalf("seeded %d %s rows, wrote %d — the fixture did not land", got, table, want)
		}
	}
	assertCount("task", len(migration00088Fixture()))
	assertCount("task_manual", len(migration00088Manuals()))
	return db
}

// migration00088ReadAll reads the whole task table as id → column → normalised
// value, BY NAME, so a rebuild that reordered the physical columns is not
// mistaken for one that moved the data. The column list comes from the database
// it is reading, so it follows the schema rather than a list in this file.
func migration00088ReadAll(t *testing.T, db *sql.DB) map[string]map[string]string {
	t.Helper()
	cols := migration00088TaskColumns(t, db)
	rows, err := db.Query(fmt.Sprintf(`SELECT %s FROM task`, strings.Join(cols, ", ")))
	if err != nil {
		t.Fatalf("read task: %v", err)
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan task: %v", err)
		}
		row := map[string]string{}
		var id string
		for i, c := range cols {
			row[c] = migration00088Norm(cells[i])
			if c == "id" {
				switch v := cells[i].(type) {
				case string:
					id = v
				case []byte:
					id = string(v)
				}
			}
		}
		if _, dup := out[id]; dup {
			t.Fatalf("task id %q appears twice — the rebuild duplicated a row", id)
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// migration00088ReadAssignees reads task_manual as type_key → assignee, verbatim.
func migration00088ReadAssignees(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT type_key, assignee FROM task_manual`)
	if err != nil {
		t.Fatalf("read task_manual: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("scan task_manual: %v", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// migration00088AssertIndexes is the shared index check, used after the Up,
// after the Down and after the second Up. `DROP TABLE task` destroys both
// indexes and `RENAME` does not bring them back, so every direction of this
// migration owes them back explicitly — and when one is missing NOTHING raises:
// the database looks fine and every status filter and dedupe probe silently
// becomes a table scan.
//
// Checked twice over, because neither half alone is enough. Existence by name
// in sqlite_master catches the index being gone; the FORCED PLAN (`INDEXED BY`,
// which SQLite refuses with "no query solution" when the named index cannot
// serve the query) catches an index that exists under the right name but stands
// on the wrong columns.
func migration00088AssertIndexes(t *testing.T, db *sql.DB, when string) {
	t.Helper()
	for _, idx := range []struct{ name, probe string }{
		{"idx_task_status",
			`SELECT id FROM task INDEXED BY idx_task_status WHERE status = 'not_started'`},
		{"idx_task_dedupe",
			`SELECT id FROM task INDEXED BY idx_task_dedupe WHERE type_key = 'k' AND dedupe_key = 'd'`},
	} {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx.name,
		).Scan(&n); err != nil {
			t.Fatalf("query sqlite_master for %s: %v", idx.name, err)
		}
		if n != 1 {
			t.Errorf("%s: %s is present %d time(s), want 1 — DROP TABLE task takes the index "+
				"with it and RENAME does not bring it back, so a rebuild that forgets to "+
				"re-create it leaves a database that looks fine and table-scans every "+
				"status filter / dedupe probe. Nothing raises when this breaks",
				when, idx.name, n)
			continue
		}
		rows, err := db.Query(idx.probe)
		if err != nil {
			t.Errorf("%s: a query forced through %s could not be planned (%v) — the index "+
				"exists by name but does not cover the columns it is supposed to",
				when, idx.name, err)
			continue
		}
		rows.Close()
	}
}

// migration00088TaskShape photographs what `task` ACTUALLY is right now: its
// columns in order, and every index standing on it with its DDL. Both come from
// the live database, never from a list in this file.
func migration00088TaskShape(t *testing.T, db *sql.DB) (cols []string, idx []string) {
	t.Helper()
	read := func(q string) []string {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate: %v", err)
		}
		return out
	}
	cols = read(`SELECT name FROM pragma_table_info('task')`)
	idx = read(`SELECT name || ' :: ' || COALESCE(sql, '<implicit>') FROM sqlite_master
	            WHERE type = 'index' AND tbl_name = 'task' ORDER BY name`)
	return cols, idx
}

func migration00088Up(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.UpTo(db, "migrations", migration00088Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00088Version, err)
	}
}

func migration00088Down(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.DownTo(db, "migrations", migration00088PriorVersion); err != nil {
		t.Fatalf("goose down to %d: %v", migration00088PriorVersion, err)
	}
}

// migration00088WantKind is what each pre-Up value must become. Worked out by
// hand from what the migration claims, never derived from its own CASE.
func migration00088WantKind(before string) string {
	if before == migration00088OldKind {
		return migration00088NewKind
	}
	return before
}

// ── 1. the two kind columns ──────────────────────────────────────────────────

// TestMigration00088UpRenamesBothKindColumns is the load-bearing assertion of
// the Up.
//
// 🔴 reassigned_from_kind IS THE HALF THAT NEEDS THIS. executor_kind carries the
// new CHECK, so a rebuild that forgot ITS case cannot even complete — goose
// fails on the INSERT and everything that runs migrations goes red, ahead of any
// assertion here. reassigned_from_kind has NO CHECK and admits the empty
// string, so dropping its CASE leaves a migration that succeeds, a database
// that looks upgraded, and a column still speaking the retired word.
//
// THE EMPTY STRING IS THE OTHER HALF. It means "never reassigned" and must come
// through unchanged — a CASE widened into `WHEN reassigned_from_kind <>
// 'outsource' THEN 'staff'`, or a blanket rename, invents a reassignment that
// never happened, and no constraint anywhere would object.
func TestMigration00088UpRenamesBothKindColumns(t *testing.T) {
	db := migration00088World(t)

	// Anti-vacuity: the fixture must actually contain every value whose fate is
	// asserted below, or those claims are claims about nothing.
	seen := map[string]int{}
	for _, s := range migration00088Fixture() {
		seen["executor_kind="+s.executorKind]++
		seen["reassigned_from_kind="+s.reassignedKind]++
	}
	for _, want := range []string{
		"executor_kind=member", "executor_kind=outsource",
		"reassigned_from_kind=member", "reassigned_from_kind=outsource",
		"reassigned_from_kind=",
	} {
		if seen[want] == 0 {
			t.Fatalf("the fixture seeds no row with %q — this test would prove nothing about it", want)
		}
	}

	migration00088Up(t, db)
	got := migration00088ReadAll(t, db)
	if len(got) != len(migration00088Fixture()) {
		t.Fatalf("task holds %d rows after 00088, want %d — the rebuild's INSERT…SELECT must "+
			"copy every row", len(got), len(migration00088Fixture()))
	}

	for _, spec := range migration00088Fixture() {
		t.Run(spec.id, func(t *testing.T) {
			row, ok := got[spec.id]
			if !ok {
				t.Fatalf("task %q did not survive 00088", spec.id)
			}
			for col, before := range map[string]string{
				"executor_kind":        spec.executorKind,
				"reassigned_from_kind": spec.reassignedKind,
			} {
				want := migration00088WantKind(before)
				if row[col] != migration00088Norm(want) {
					t.Errorf("task %q had %s=%q before 00088 and has %s after, want %s — the "+
						"migration maps 'member' -> 'staff' value by value in BOTH kind "+
						"columns and passes 'outsource' and '' through untouched",
						spec.id, col, before, row[col], migration00088Norm(want))
				}
			}
		})
	}

	// And the population as a whole, which a per-row loop cannot see.
	for _, col := range []string{"executor_kind", "reassigned_from_kind"} {
		var stale int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM task WHERE `+col+` = ?`, migration00088OldKind,
		).Scan(&stale); err != nil {
			t.Fatalf("count leftover %s: %v", col, err)
		}
		if stale != 0 {
			t.Errorf("%d task row(s) still carry %s=%q after 00088 — the point of the "+
				"migration is that this population is RENAMED, not merely joined by a "+
				"second name", stale, col, migration00088OldKind)
		}
		var stray int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM task WHERE ` + col + ` NOT IN ('staff', 'outsource', '')`,
		).Scan(&stray); err != nil {
			t.Fatalf("count stray %s: %v", col, err)
		}
		if stray != 0 {
			t.Errorf("%d task row(s) carry a %s outside {'staff','outsource',''} after 00088", stray, col)
		}
	}
	var invented int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM task WHERE id = 't-88-never-reassigned' AND reassigned_from_kind <> ''`,
	).Scan(&invented); err != nil {
		t.Fatalf("count invented reassignments: %v", err)
	}
	if invented != 0 {
		t.Errorf("the never-reassigned row came out of 00088 with a non-empty "+
			"reassigned_from_kind — '' must stay '' (%d row)", invented)
	}
}

// ── 2. everything that is NOT a kind column ──────────────────────────────────

// TestMigration00088UpPreservesEveryColumn is the guard 00088's own header calls
// "THE WHOLE RISK": the rebuild names 33 columns twice, by hand, and anything
// missing from either list is silently replaced by the new table's DEFAULT for
// every existing row while the migration reports success.
func TestMigration00088UpPreservesEveryColumn(t *testing.T) {
	db := migration00088World(t)
	cols := migration00088TaskColumns(t, db)
	before := migration00088ReadAll(t, db)
	if len(before) == 0 {
		t.Fatal("no rows seeded — this comparison would prove nothing")
	}

	migration00088Up(t, db)
	after := migration00088ReadAll(t, db)

	if got := migration00088TaskColumns(t, db); !reflect.DeepEqual(got, cols) {
		t.Fatalf("00088 changed task's column set.\n before: %v\n  after: %v", cols, got)
	}

	for _, spec := range migration00088Fixture() {
		t.Run(spec.id, func(t *testing.T) {
			pre, ok := before[spec.id]
			if !ok {
				t.Fatalf("task %q was not in the pre-Up readback", spec.id)
			}
			post, ok := after[spec.id]
			if !ok {
				t.Fatalf("task %q did not survive 00088", spec.id)
			}
			for _, c := range cols {
				if c == "executor_kind" || c == "reassigned_from_kind" {
					continue // deliberately changed; covered by their own test
				}
				if post[c] != pre[c] {
					t.Errorf("column %q of task %q changed across the rebuild: got %s, want %s\n\n"+
						"Every column other than the two kind columns is copied verbatim. A "+
						"value that moved to a NEIGHBOURING column means the INSERT list and "+
						"the SELECT list have drifted apart; a value that became ''/0/0.0 "+
						"means the column was dropped from one of them and took the rebuilt "+
						"table's DEFAULT.",
						c, spec.id, post[c], pre[c])
				}
			}
		})
	}
}

// ── 3. 🔴 the indexes ────────────────────────────────────────────────────────

// TestMigration00088IndexSurvivesUp guards the failure that raises nothing.
func TestMigration00088IndexSurvivesUp(t *testing.T) {
	db := migration00088World(t)

	// Negative control: the guarantee exists BEFORE the migration, so a green
	// result below is about the rebuild preserving it rather than about the
	// indexes never having been there.
	migration00088AssertIndexes(t, db, "before the Up")

	migration00088Up(t, db)
	migration00088AssertIndexes(t, db, "after the Up")
}

// TestMigration00088PreservesTheShapeOfTaskWhateverItIs is the list-free ruler:
// `task` is photographed immediately before the Up and immediately after, and
// the two photographs must match. Whatever the schema happens to be on the day,
// a rebuild is only allowed to change VALUES, never the SHAPE — so a column or
// an index added by ANY earlier migration is covered the day it lands, with
// nothing in this file to keep up to date.
func TestMigration00088PreservesTheShapeOfTaskWhateverItIs(t *testing.T) {
	db := migration00088World(t)

	beforeCols, beforeIdx := migration00088TaskShape(t, db)
	if len(beforeCols) == 0 || len(beforeIdx) == 0 {
		t.Fatalf("precondition: task must have columns (%d) and at least one index (%d) before "+
			"00088 — if this fires the reader is broken, not the schema", len(beforeCols), len(beforeIdx))
	}

	migration00088Up(t, db)
	afterCols, afterIdx := migration00088TaskShape(t, db)

	if !reflect.DeepEqual(beforeCols, afterCols) {
		t.Errorf("00088 changed task's COLUMNS.\n before: %v\n  after: %v\n"+
			"⇒ The rebuild's INSERT…SELECT names a fixed 33-column list. Anything the schema "+
			"has that the list does not is copied away and every existing row loses that "+
			"value silently. Add your column to BOTH directions of "+
			"migrations/%05d_task_executor_kind_member_to_staff.sql.",
			beforeCols, afterCols, migration00088Version)
	}
	if !reflect.DeepEqual(beforeIdx, afterIdx) {
		t.Errorf("00088 changed task's INDEXES.\n before: %v\n  after: %v\n"+
			"⇒ DROP TABLE takes every index with it and RENAME does not bring them back. A "+
			"lost index raises nothing and logs nothing — it just turns the query it served "+
			"into a table scan. Re-create it explicitly in BOTH directions of "+
			"migrations/%05d_task_executor_kind_member_to_staff.sql.",
			beforeIdx, afterIdx, migration00088Version)
	}
}

// ── 4. 🔴 the Down, which nothing in this tree executed before this file ──────

// TestMigration00088DownRestoresThePreUpState is exact rather than plausible:
// 'member' and 'staff' are the same population under two names on this axis, so
// the mapping is 1:1 in both directions and no row becomes unrepresentable. The
// Down also does DROP TABLE, so it owes both indexes back too.
func TestMigration00088DownRestoresThePreUpState(t *testing.T) {
	db := migration00088World(t)
	cols := migration00088TaskColumns(t, db)
	before := migration00088ReadAll(t, db)
	beforeManuals := migration00088ReadAssignees(t, db)
	beforeCols, beforeIdx := migration00088TaskShape(t, db)
	if len(before) == 0 {
		t.Fatal("no rows seeded — this round trip would prove nothing")
	}

	migration00088Up(t, db)
	migration00088Down(t, db)

	after := migration00088ReadAll(t, db)
	if len(after) != len(before) {
		t.Fatalf("Down produced %d task rows, want %d", len(after), len(before))
	}
	for _, spec := range migration00088Fixture() {
		t.Run(spec.id, func(t *testing.T) {
			post, ok := after[spec.id]
			if !ok {
				t.Fatalf("task %q did not survive the round trip", spec.id)
			}
			for _, c := range cols {
				if post[c] != before[spec.id][c] {
					t.Errorf("column %q of task %q is %s after Up→Down, want the pre-Up %s — "+
						"both kind columns must be mapped back to 'member' and every other "+
						"column is copied verbatim in both directions",
						c, spec.id, post[c], before[spec.id][c])
				}
			}
		})
	}

	afterManuals := migration00088ReadAssignees(t, db)
	if !reflect.DeepEqual(afterManuals, beforeManuals) {
		t.Errorf("task_manual.assignee did not come back to its pre-Up state.\n before: %v\n  after: %v",
			beforeManuals, afterManuals)
	}

	afterCols, afterIdx := migration00088TaskShape(t, db)
	if !reflect.DeepEqual(beforeCols, afterCols) {
		t.Errorf("the Down changed task's COLUMNS.\n before: %v\n  after: %v", beforeCols, afterCols)
	}
	if !reflect.DeepEqual(beforeIdx, afterIdx) {
		t.Errorf("the Down changed task's INDEXES.\n before: %v\n  after: %v\n"+
			"⇒ A Down that forgets its own CREATE INDEX leaves a rolled-back database that "+
			"looks fine and silently scans.", beforeIdx, afterIdx)
	}
	migration00088AssertIndexes(t, db, "after the Down")
}

// TestMigration00088DownRenamesBothKindColumnsBack states the Down's value
// contract on its own, so that "the round trip returned to where it started"
// cannot be satisfied by a Down that simply undid nothing (which would fail the
// Up's own CHECK anyway) or by an Up and a Down that are wrong in the same way.
//
// The empty string is asserted in this direction too: nothing at all guards
// reassigned_from_kind here, and a Down whose CASE was widened turns "never
// reassigned" into "reassigned from a member".
func TestMigration00088DownRenamesBothKindColumnsBack(t *testing.T) {
	db := migration00088World(t)
	migration00088Up(t, db)

	// Precondition, measured rather than assumed: the Up really did leave
	// 'staff' behind for the Down to act on.
	var staffRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM task WHERE executor_kind = ? OR reassigned_from_kind = ?`,
		migration00088NewKind, migration00088NewKind,
	).Scan(&staffRows); err != nil {
		t.Fatalf("count staff rows after the Up: %v", err)
	}
	if staffRows == 0 {
		t.Fatal("no row carries 'staff' after the Up — the Down would have nothing to reverse")
	}

	migration00088Down(t, db)

	for _, spec := range migration00088Fixture() {
		var execKind, reassignedKind string
		if err := db.QueryRow(
			`SELECT executor_kind, reassigned_from_kind FROM task WHERE id = ?`, spec.id,
		).Scan(&execKind, &reassignedKind); err != nil {
			t.Fatalf("read %s after the Down: %v", spec.id, err)
		}
		if execKind != spec.executorKind {
			t.Errorf("task %q has executor_kind %q after Up→Down, want the pre-Up %q",
				spec.id, execKind, spec.executorKind)
		}
		if reassignedKind != spec.reassignedKind {
			t.Errorf("task %q has reassigned_from_kind %q after Up→Down, want the pre-Up %q — "+
				"no CHECK guards this column in either direction, so its CASE is the only "+
				"thing that moves it back and '' must stay ''",
				spec.id, reassignedKind, spec.reassignedKind)
		}
	}

	var leftover int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM task WHERE executor_kind = ? OR reassigned_from_kind = ?`,
		migration00088NewKind, migration00088NewKind,
	).Scan(&leftover); err != nil {
		t.Fatalf("count leftover staff: %v", err)
	}
	if leftover != 0 {
		t.Errorf("%d task row(s) still carry 'staff' after the Down — at this schema version "+
			"executor_kind's CHECK lists {'member','outsource'} and 'staff' is not a value "+ // kind-vocab-guard:legacy
			"the rolled-back database can even represent", leftover)
	}

	// The rolled-back CHECK and DEFAULT are a pair, and they fail differently:
	// a Down that restores the CHECK but leaves DEFAULT 'staff' produces a table
	// whose every existing row is correct and whose next INSERT omitting the
	// column writes a value its own CHECK rejects.
	var defaultKind string
	if err := db.QueryRow(
		`INSERT INTO task (id, title) VALUES ('t-88-default-probe', 'probe') RETURNING executor_kind`,
	).Scan(&defaultKind); err != nil {
		t.Fatalf("the Down left the executor_kind DEFAULT on a value its own restored CHECK "+
			"rejects — an INSERT omitting the column cannot land: %v", err)
	}
	if defaultKind != migration00088OldKind {
		t.Errorf("the Down left the executor_kind DEFAULT on %q, want %q", defaultKind, migration00088OldKind)
	}
}

// TestMigration00088AssigneeJSONSurvivesBothDirections covers the third place
// the same vocabulary is stored — a JSON blob with no CHECK behind it.
//
// The unknown key is the point. json_set on `$.kind` is used instead of writing
// a fresh object precisely so that member_id, and any key the validator stored
// verbatim without reading, survive byte-for-byte; a blob rewrite would pass a
// "kind is staff" assertion while silently dropping them. Asserted on the exact
// string in BOTH directions, and the Down half is asserted nowhere else.
func TestMigration00088AssigneeJSONSurvivesBothDirections(t *testing.T) {
	db := migration00088World(t)

	migration00088Up(t, db)
	afterUp := migration00088ReadAssignees(t, db)
	for _, m := range migration00088Manuals() {
		if got := afterUp[m.typeKey]; got != m.afterUp {
			t.Errorf("assignee %s after the Up:\n got  %s\n want %s\n"+
				"⇒ Only `$.kind` moves. Every other key — member_id and any key stored "+
				"verbatim by the validator — must be byte-identical, and a row whose "+
				"assignee is not valid JSON must not be touched at all.",
				m.typeKey, got, m.afterUp)
		}
	}

	migration00088Down(t, db)
	afterDown := migration00088ReadAssignees(t, db)
	for _, m := range migration00088Manuals() {
		if got := afterDown[m.typeKey]; got != m.before {
			t.Errorf("assignee %s after Up→Down:\n got  %s\n want the pre-Up %s\n"+
				"⇒ The Down's json_set is the mirror of the Up's. Nothing else in this tree "+
				"executes it, so this assertion is the only thing standing between a broken "+
				"rollback and a manual whose assignee stops binding an executor.",
				m.typeKey, got, m.before)
		}
	}
}

// ── 5. stability ─────────────────────────────────────────────────────────────

// TestMigration00088UpDownUpIsStable runs the round trip twice: the state after
// the second Up must equal the state after the first, task_manual included. A
// mapping that is not 1:1 in both directions, or a Down that leaves residue,
// diverges here even when each single direction looks right on its own.
func TestMigration00088UpDownUpIsStable(t *testing.T) {
	db := migration00088World(t)

	migration00088Up(t, db)
	firstUp := migration00088ReadAll(t, db)
	firstUpManuals := migration00088ReadAssignees(t, db)
	if len(firstUp) == 0 {
		t.Fatal("no rows after the first Up — this round trip would prove nothing")
	}
	cols := migration00088TaskColumns(t, db)

	migration00088Down(t, db)
	migration00088Up(t, db)
	secondUp := migration00088ReadAll(t, db)
	secondUpManuals := migration00088ReadAssignees(t, db)

	// Flattened and sorted so the diff names the whole population, not the first
	// cell that happens to differ.
	flatten := func(m map[string]map[string]string) []string {
		out := make([]string, 0, len(m))
		for id, row := range m {
			cells := make([]string, 0, len(row))
			for _, c := range cols {
				cells = append(cells, c+"="+row[c])
			}
			out = append(out, id+"\x00"+strings.Join(cells, "\x00"))
		}
		sort.Strings(out)
		return out
	}
	got, want := flatten(secondUp), flatten(firstUp)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("UP→DOWN→UP DID NOT LAND WHERE THE FIRST UP DID.\n got (%d rows): %q\nwant (%d rows): %q",
			len(got), got, len(want), want)
	}
	if !reflect.DeepEqual(secondUpManuals, firstUpManuals) {
		t.Errorf("UP→DOWN→UP left task_manual.assignee elsewhere.\n got: %v\nwant: %v",
			secondUpManuals, firstUpManuals)
	}

	// Both indexes are re-created by every direction, so a rebuild that
	// remembers them only once shows up here.
	migration00088AssertIndexes(t, db, "after the second Up")
}
