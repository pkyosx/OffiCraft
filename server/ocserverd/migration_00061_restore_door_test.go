package main

// migration_00061_restore_door_test.go — T-2 step A's SECOND door, re-aimed by
// step B.
//
// WHAT THIS FILE USED TO ASSERT, and why it cannot any more. 00061 deleted every
// non-general `lessons` row, and the reviewer's measured escape was the restore
// route: given a retained revision under the key "<role_key>::<task_type>", the
// lessons arm of restoreDocumentHistory split the key and handed the task_type
// it found to putLessonsOn VERBATIM, so a restore could put a non-general row
// straight back. The old test reproduced that end to end through the real
// handlers: write a non-general lessons doc, run the migration, restore, and
// require that no non-general row came back.
//
// 🔴 THAT REPRODUCTION IS NO LONGER WRITABLE, and that is the point of step B
// rather than a gap in it. There is no `task_type` — not on the write face
// (HandleReplaceLessonsApiLessonsRoleKeyPost takes one path parameter), not in
// the row (00062 dropped the column), and not in the history key. The first
// line of the old fixture — "write a lessons doc under task_type
// 'review-pr-seth'" — cannot be expressed, so a test that still tried would be
// asserting over a scenario it could not construct.
//
// What replaces it is the STRUCTURAL statement, which is stronger than the
// behavioural one it retires: the door is not guarded, it is gone. Both halves
// are checked here — the column is absent from the live schema, and a restore
// through the real handler lands under the BARE role_key rather than under any
// composite. TestMigration00062* holds the migration's own side.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// migration00061RestoreWorld returns a fully migrated API server together with
// the handle its DAL writes through, because these tests read the schema of the
// same database the handlers use.
func migration00061RestoreWorld(t *testing.T) (*apiServer, *sql.DB) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "restore-door.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return newAPIServer(NewDAL(db), NewHub(), singleKeyring([]byte("restore-door-secret")), 3600,
		assetRoot(t.TempDir())), db
}

// replaceLessonsThroughHandler drives the real write face, which is what
// retains a document_history revision under the role_key.
func replaceLessonsThroughHandler(t *testing.T, api *apiServer, role, text string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec, taskReq(t, http.MethodPost,
		"/api/lessons/"+role, map[string]any{"text": text}, "owner", "owner"),
		role)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace_lessons(%s): %d %s", role, rec.Code, rec.Body.String())
	}
}

// lessonsColumns reads the live column list of the `lessons` table.
func lessonsColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info('lessons')`)
	if err != nil {
		t.Fatalf("pragma_table_info(lessons): %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestLessonsHasNoTaskTypeColumnAfterMigrations is the structural half. It is
// the assertion a revert of 00062 has to turn red.
func TestLessonsHasNoTaskTypeColumnAfterMigrations(t *testing.T) {
	_, db := migration00061RestoreWorld(t)
	cols := lessonsColumns(t, db)
	// ANTI-VACUITY: a typo in the pragma, or a table that does not exist, would
	// return an empty list and let the absence check below pass over nothing.
	if len(cols) == 0 {
		t.Fatalf("pragma_table_info returned no columns for `lessons` — the probe is " +
			"broken, so the absence assertion below would prove nothing")
	}
	var haveRoleKey bool
	for _, c := range cols {
		if c == "task_type" {
			t.Fatalf("THE LESSONS CLASSIFICATION AXIS IS BACK: `lessons` still carries a "+
				"task_type column (%s). T-2 removed it so that no write face, and no "+
				"restore, can choose a bucket the caller did not mean; a column here is "+
				"the axis existing again no matter what the handlers accept",
				strings.Join(cols, ", "))
		}
		if c == "role_key" {
			haveRoleKey = true
		}
	}
	if !haveRoleKey {
		t.Fatalf("`lessons` has no role_key column (%s) — the table is not the one this "+
			"test believes it is reading", strings.Join(cols, ", "))
	}
}

// TestLessonsRestoreLandsUnderTheBareRoleKey is the behavioural half: whatever
// the restore writes, it is addressed by role_key alone. This is what used to
// be "the restore cannot choose a non-general task_type" — the same property,
// stated in the vocabulary that survives the removal.
func TestLessonsRestoreLandsUnderTheBareRoleKey(t *testing.T) {
	api, _ := migration00061RestoreWorld(t)
	const role = seedRoleAssistant

	replaceLessonsThroughHandler(t, api, role, "v1 — the revision that gets retained")
	replaceLessonsThroughHandler(t, api, role, "v2 — the write that retains v1")

	stored, err := api.dal.ListDocumentHistory("lessons", role)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(stored) == 0 {
		t.Fatalf("no retained revision under %q — the fixture did not land, so nothing "+
			"below would be measuring anything", role)
	}

	rec := httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		taskReq(t, http.MethodPost, "/api/document-history/lessons/"+role+"/"+
			strconv.FormatInt(stored[0].ID, 10)+"/restore", nil, "owner", "owner"),
		"lessons", role, stored[0].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}

	overlay, err := api.dal.GetLessons(role)
	if err != nil {
		t.Fatalf("get lessons: %v", err)
	}
	if overlay == nil {
		t.Fatal("the restore wrote no lessons overlay at all")
	}
	if overlay.RoleKey != role {
		t.Errorf("the restore landed under role_key %q, want %q — a restore that writes "+
			"anywhere but the key it was addressed by is the shape of the bug T-2 removed "+
			"the classification axis to end", overlay.RoleKey, role)
	}
	if !strings.Contains(overlay.Text, "v1") {
		t.Errorf("the restored text is %q, want the retained v1 — the restore put back "+
			"something other than the revision it was handed", overlay.Text)
	}
}
