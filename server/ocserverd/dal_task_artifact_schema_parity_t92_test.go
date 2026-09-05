package main

// dal_task_artifact_schema_parity_t92_test.go — THE THREE-WAY RECONCILIATION
// the artifact tables did not have.
//
// T-92 rebuilt task_artifact and task_artifact_history (migrations/00086) and
// reshaped TaskArtifact / TaskArtifactHistory and every SQL string in
// dal_task_artifacts.go in the same package. Three descriptions of ONE schema
// now exist in three files, and nothing in the tree compared them:
//
//   ① the DDL           — migrations/00086_task_artifact_name_description.sql
//   ② the Go structs    — TaskArtifact / TaskArtifactHistory
//   ③ the SQL strings   — taskArtifactColumns, taskArtifactHistoryColumns, and
//                         the INSERT / UPDATE column lists written out by hand
//
// 🔴 WHY A COUNT IS NOT ENOUGH, and why this file never asserts one. Any drift
// that RENAMES rather than adds or drops — `description` misspelled in a column
// list, `created_by` typed as `created_bys`, a column reordered into the wrong
// scan slot — leaves all three counts equal. So every assertion below compares
// the NAME SET, and reports the two directions of the difference (missing /
// extra) by name.
//
// What each source is allowed to differ by is stated per-case rather than
// waved at: an INSERT into an AUTOINCREMENT table legitimately omits `id`, and
// an UPDATE legitimately omits the primary key and the row's owning task.
//
// 🔴 CORPUS GUARD. A parser that quietly returns nothing turns every set
// comparison below into "empty == empty". Each parse is therefore asserted to
// have found a plausible number of columns BEFORE anything is compared with it.

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

const t92MigrationPath = "migrations/00086_task_artifact_name_description.sql"

// t92DALSourcePath is read as a FILE rather than reflected on, because what is
// being reconciled is the SQL TEXT: a column name inside a backquoted string is
// invisible to the compiler and to reflection alike, which is exactly the class
// of drift this file exists to catch.
const t92DALSourcePath = "dal_task_artifacts.go"

// t92MinColumns is the corpus floor. Both tables carry eight columns; anything
// under this means the parser lost the table, not that the schema shrank.
const t92MinColumns = 7

// ── parsing ─────────────────────────────────────────────────────────────────

// t92MigrationUp is the Up half of 00086. The Down half re-creates tables of
// the SAME names with the OLD columns (url / label), so a parse that does not
// cut here reconciles against the schema this migration exists to leave behind.
func t92MigrationUp(t *testing.T) string {
	t.Helper()
	b, err := embeddedMigrations.ReadFile(t92MigrationPath)
	if err != nil {
		t.Fatalf("read %s: %v", t92MigrationPath, err)
	}
	s := string(b)
	i := strings.Index(s, "-- +goose Down")
	if i < 0 {
		t.Fatalf("%s has no `-- +goose Down` marker — the Up/Down split this "+
			"parse depends on is gone", t92MigrationPath)
	}
	return s[:i]
}

var t92ConstraintLead = regexp.MustCompile(
	`^(?i)(primary|foreign|unique|check|constraint)\b`)

// t92DDLColumns returns the column names of one CREATE TABLE, in declaration
// order. It stops at the line that closes the statement rather than at the
// first ")" — `kind TEXT NOT NULL CHECK (kind IN (...))` has parentheses of its
// own, and a first-")" parse would silently truncate the table to three columns
// and then happily "reconcile" that against nothing.
func t92DDLColumns(t *testing.T, sqlText, table string) []string {
	t.Helper()
	head := "CREATE TABLE " + table + " ("
	i := strings.Index(sqlText, head)
	if i < 0 {
		t.Fatalf("%s: no `%s` in the Up half — the DDL this test reconciles "+
			"against has been renamed or removed", t92MigrationPath, head)
	}
	var out []string
	for _, line := range strings.Split(sqlText[i+len(head):], "\n") {
		line = strings.TrimSpace(line)
		if line == ");" || line == ")" {
			if len(out) < t92MinColumns {
				t.Fatalf("%s %s: parsed only %d columns (%v) — the parser lost "+
					"the table; every comparison against it would be vacuous",
					t92MigrationPath, table, len(out), out)
			}
			return out
		}
		if line == "" || strings.HasPrefix(line, "--") || t92ConstraintLead.MatchString(line) {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.Fields(line)[0], ","))
	}
	t.Fatalf("%s %s: CREATE TABLE never closed", t92MigrationPath, table)
	return nil
}

// t92ColumnList splits a comma-separated SQL column list — the body of
// taskArtifactColumns, an INSERT's parenthesised list — into names, dropping
// the newlines and tabs a Go raw string keeps.
func t92ColumnList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// t92ConstColumns reads one `const <name> = ` + "`...`" column list out of the
// DAL source TEXT. Reading the compiled constant instead would still catch a
// missing column, but the point of going to the source is that this test must
// fail for a REASON a reader can act on — it can then name the file and the
// constant, not just the mismatch.
func t92ConstColumns(t *testing.T, src, name string) []string {
	t.Helper()
	re := regexp.MustCompile("(?s)const " + regexp.QuoteMeta(name) + " = `([^`]*)`")
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s: const %s not found — this test's subject moved",
			t92DALSourcePath, name)
	}
	cols := t92ColumnList(m[1])
	if len(cols) < t92MinColumns {
		t.Fatalf("%s: const %s parsed to %d columns (%v) — vacuous",
			t92DALSourcePath, name, len(cols), cols)
	}
	return cols
}

// t92SnakeCase turns a Go field name into the column name it stands for, with
// acronyms kept whole: ID → id, TaskID → task_id, CreatedTS → created_ts.
func t92SnakeCase(name string) string {
	var b strings.Builder
	r := []rune(name)
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prevLower := unicode.IsLower(r[i-1])
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || nextLower {
				b.WriteRune('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// ── comparison ──────────────────────────────────────────────────────────────

// t92AssertSameSet compares two column NAME SETS and reports both directions by
// name. It deliberately does not compare lengths first: two sets of equal size
// that disagree on a name is the whole failure mode this file exists for.
func t92AssertSameSet(t *testing.T, what string, want, got []string, wantSrc, gotSrc string) {
	t.Helper()
	missing, extra := t92SetDiff(want, got)
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	t.Errorf("%s: the schema is described in two places and they disagree.\n"+
		"  %s has, %s lacks: %v\n"+
		"  %s has, %s lacks: %v\n"+
		"  (%s = %v)\n  (%s = %v)",
		what, wantSrc, gotSrc, missing, gotSrc, wantSrc, extra,
		wantSrc, t92Sorted(want), gotSrc, t92Sorted(got))
}

func t92SetDiff(want, got []string) (missing, extra []string) {
	w, g := map[string]bool{}, map[string]bool{}
	for _, s := range want {
		w[s] = true
	}
	for _, s := range got {
		g[s] = true
	}
	for s := range w {
		if !g[s] {
			missing = append(missing, s)
		}
	}
	for s := range g {
		if !w[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func t92Sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// t92Without drops the named columns from a set — the stated, per-case reason a
// hand-written statement may legitimately not mention one.
func t92Without(cols []string, drop ...string) []string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	var out []string
	for _, c := range cols {
		if !skip[c] {
			out = append(out, c)
		}
	}
	return out
}

// ── the cases ───────────────────────────────────────────────────────────────

// TestTaskArtifactSchemaAgreesAcrossDDLStructAndSQL reconciles the LIVE table
// three ways. A column added to 00086 and not to the struct, a column the SELECT
// list names and the table does not, a name misspelt in either — all three land
// here with the offending NAME printed.
func TestTaskArtifactSchemaAgreesAcrossDDLStructAndSQL(t *testing.T) {
	up := t92MigrationUp(t)
	src := t92ReadDALSource(t)

	// The Up half creates the table under its _rebuild name and RENAMEs it, so
	// the DDL to read is the rebuild's.
	ddl := t92DDLColumns(t, up, "task_artifact_rebuild")

	var structCols []string
	for _, f := range t92StructFields(TaskArtifact{}) {
		structCols = append(structCols, t92SnakeCase(f))
	}
	t92AssertSameSet(t, "task_artifact", ddl, structCols,
		t92MigrationPath, "TaskArtifact struct")
	t92AssertSameSet(t, "task_artifact", ddl, t92ConstColumns(t, src, "taskArtifactColumns"),
		t92MigrationPath, t92DALSourcePath+" :: taskArtifactColumns")
}

// TestTaskArtifactHistorySchemaAgreesAcrossDDLStructAndSQL is the same
// reconciliation for the retained-version table. It gets its own case because
// the two tables drift INDEPENDENTLY: the history side is written by exactly
// one function, so a column added to the live table alone raises nothing at
// compile time and nothing at run time until a version is read back.
func TestTaskArtifactHistorySchemaAgreesAcrossDDLStructAndSQL(t *testing.T) {
	up := t92MigrationUp(t)
	src := t92ReadDALSource(t)

	ddl := t92DDLColumns(t, up, "task_artifact_history_rebuild")

	var structCols []string
	for _, f := range t92StructFields(TaskArtifactHistory{}) {
		structCols = append(structCols, t92SnakeCase(f))
	}
	t92AssertSameSet(t, "task_artifact_history", ddl, structCols,
		t92MigrationPath, "TaskArtifactHistory struct")
	t92AssertSameSet(t, "task_artifact_history", ddl,
		t92ConstColumns(t, src, "taskArtifactHistoryColumns"),
		t92MigrationPath, t92DALSourcePath+" :: taskArtifactHistoryColumns")
}

// TestTaskArtifactHandWrittenStatementsNameOnlyRealColumns covers the SQL that
// is NOT built from the shared constants: the history INSERT and the live-row
// UPDATE inside replaceTaskArtifactOn, both of which spell their columns out by
// hand. Those two are the ones a rename forgets.
func TestTaskArtifactHandWrittenStatementsNameOnlyRealColumns(t *testing.T) {
	up := t92MigrationUp(t)
	src := t92ReadDALSource(t)

	liveDDL := t92DDLColumns(t, up, "task_artifact_rebuild")
	histDDL := t92DDLColumns(t, up, "task_artifact_history_rebuild")

	// The history INSERT names every column but `id`, which is INTEGER PRIMARY
	// KEY AUTOINCREMENT and is the row's assigned version order.
	insertRe := regexp.MustCompile(`(?s)INSERT INTO task_artifact_history\s*\n?\s*\(([^)]*)\)`)
	m := insertRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s: no hand-written `INSERT INTO task_artifact_history (...)` "+
			"found — this case's subject moved", t92DALSourcePath)
	}
	t92AssertSameSet(t, "task_artifact_history INSERT (id excluded: AUTOINCREMENT)",
		t92Without(histDDL, "id"), t92ColumnList(m[1]),
		t92MigrationPath, t92DALSourcePath+" :: replaceTaskArtifactOn INSERT")

	// The UPDATE assigns every column except the primary key and the owning
	// task: a replace keeps the id and never moves the artifact between tasks.
	updRe := regexp.MustCompile(`(?s)UPDATE task_artifact\s*\n?\s*SET (.*?)\s*WHERE`)
	um := updRe.FindStringSubmatch(src)
	if um == nil {
		t.Fatalf("%s: no hand-written `UPDATE task_artifact SET ... WHERE` found "+
			"— this case's subject moved", t92DALSourcePath)
	}
	var assigned []string
	for _, part := range t92ColumnList(um[1]) {
		assigned = append(assigned, strings.TrimSpace(strings.SplitN(part, "=", 2)[0]))
	}
	t92AssertSameSet(t, "task_artifact UPDATE (id/task_id excluded: a replace keeps both)",
		t92Without(liveDDL, "id", "task_id"), assigned,
		t92MigrationPath, t92DALSourcePath+" :: replaceTaskArtifactOn UPDATE")
}

// ── small helpers ───────────────────────────────────────────────────────────

func t92ReadDALSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(t92DALSourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", t92DALSourcePath, err)
	}
	return string(b)
}

// t92StructFields lists a struct's exported field names in declaration order.
func t92StructFields(v any) []string {
	rt := reflect.TypeOf(v)
	out := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath == "" { // exported
			out = append(out, f.Name)
		}
	}
	if len(out) < t92MinColumns {
		panic(fmt.Sprintf("t92StructFields: %s has %d exported fields — vacuous",
			rt.Name(), len(out)))
	}
	return out
}
