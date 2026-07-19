package main

// api_taskquality_test.go — T-f3ae task quality gate: submit_plan's DoD /
// non-empty-plan refusals, create_task's identity-key normalization + K1
// mandatory-key check + undefined-field warnings, the manual-side K1 rule
// (is_key ⟹ required), and the one-shot backfill migration (00010).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planRec submits a plan and returns the raw recorder (no 200 assertion, unlike
// the submitPlan helper) so refusals can be asserted.
func planRec(t *testing.T, api *apiServer, taskID, executor string, steps []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/plan",
			map[string]any{"steps": steps}, executor, "agent"),
		taskID)
	return rec
}

// ── submit_plan quality gate ─────────────────────────────────────────────────

func TestSubmitPlanRejectsEmptyDoD(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	for _, steps := range [][]map[string]any{
		{{"name": "a", "dod": "d"}, {"name": "b"}},             // missing dod key
		{{"name": "a", "dod": "d"}, {"name": "b", "dod": ""}},  // blank dod
		{{"name": "a", "dod": "d"}, {"name": "b", "dod": " "}}, // whitespace dod
	} {
		rec := planRec(t, api, task.ID, "m-exec", steps)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("empty DoD %v: want 400, got %d %s", steps, rec.Code, rec.Body.String())
		}
	}
	// A plan where every step has a DoD passes.
	if rec := planRec(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "a", "dod": "d1"}, {"name": "b", "dod": "d2"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("valid plan: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitPlanRejectsEmptyTimeline(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	// Zero steps on a task with no done prefix → the 空殼 case → 400.
	if rec := planRec(t, api, task.ID, "m-exec", []map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty plan: want 400, got %d %s", rec.Code, rec.Body.String())
	}
}

// A replan that keeps a done prefix but adds no fresh steps is allowed — the
// combined timeline is non-empty (the §2 边界 the design asked to pin).
func TestSubmitPlanEmptyFreshOverDonePrefixIsAllowed(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	// Two steps: driving the FIRST to done leaves the task open (the second is
	// still pending) so it does not auto-close (all-steps-done, T-9ca5) before
	// the replan can exercise the done-prefix boundary.
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "only", "dod": "d"},
		{"name": "trailing", "dod": "d"},
	})
	stepID := view.Steps[0].ID
	for _, st := range []string{"in_progress", "done"} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": st}, "m-exec", "agent"),
			task.ID, stepID)
		if rec.Code != http.StatusOK {
			t.Fatalf("step→%s: %d %s", st, rec.Code, rec.Body.String())
		}
	}
	// fresh=[] but a done step remains (the pending "trailing" is dropped) →
	// combined timeline non-empty → 200.
	if rec := planRec(t, api, task.ID, "m-exec", []map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("replan over done prefix: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── create_task: identity-key normalization + K1 + warnings ──────────────────

// createTypedRec creates a typed task with arbitrary inputs and returns the raw
// recorder (createTypedTask hard-codes the "pr" input).
func createTypedRec(t *testing.T, api *apiServer, typeKey string, inputs map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "t", "type_key": typeKey, "inputs": inputs},
		"m-exec", "agent"))
	return rec
}

func TestCreateTaskDedupesAcrossFieldNameCase(t *testing.T) {
	api := newTasksTestServer(t)
	// Manual field "PR Link"; callers send differently-cased/spaced keys.
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey:  "review-pr",
		Fields:   `[{"name":"PR Link","required":true,"is_key":true}]`,
		Assignee: `{"kind":"member","member_id":"m-exec"}`,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	first := createTypedRec(t, api, "review-pr", map[string]any{"PR Link": "https://x/1"})
	if first.Code != http.StatusOK {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	firstID := decodeBody[taskCreateResultDTO](t, first).Task.ID

	// Same PR, lower-cased + padded key → must dedupe onto the same task.
	again := createTypedRec(t, api, "review-pr", map[string]any{"  pr link ": "https://x/1"})
	res := decodeBody[taskCreateResultDTO](t, again)
	if again.Code != http.StatusOK || !res.Deduped || res.Task.ID != firstID {
		t.Fatalf("case-insensitive dedupe failed: code=%d deduped=%v id=%s want=%s",
			again.Code, res.Deduped, res.Task.ID, firstID)
	}
}

func TestCreateTaskK1RejectsEmptyIdentityKey(t *testing.T) {
	api := newTasksTestServer(t)
	// An is_key field that is NOT required (the legacy review-pr-seth shape),
	// seeded straight into the dal to bypass the manual-side K1 write guard.
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey:  "review-pr",
		Fields:   `[{"name":"PR Link","required":false,"is_key":true}]`,
		Assignee: `{"kind":"member","member_id":"m-exec"}`,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// No value for the key field → K1 refuses even though required:false.
	for _, inputs := range []map[string]any{
		{},                         // absent
		{"PR Link": ""},            // blank
		{"pr link": "   "},         // whitespace, normalized name
		{"slack": "https://s/thr"}, // only a non-key field present
	} {
		rec := createTypedRec(t, api, "review-pr", inputs)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("K1 empty key %v: want 400, got %d %s", inputs, rec.Code, rec.Body.String())
		}
	}
	// A real value passes.
	if rec := createTypedRec(t, api, "review-pr", map[string]any{"PR Link": "https://x/9"}); rec.Code != http.StatusOK {
		t.Fatalf("valued key: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTaskWarnsOnUndefinedFields(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey:  "review-pr",
		Fields:   `[{"name":"PR Link","required":true,"is_key":true}]`,
		Assignee: `{"kind":"member","member_id":"m-exec"}`,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// "slack thread" is undefined; "PR Link" is defined (normalized).
	rec := createTypedRec(t, api, "review-pr", map[string]any{
		"pr link": "https://x/1", "slack thread": "https://s/1",
	})
	res := decodeBody[taskCreateResultDTO](t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "slack thread") {
		t.Fatalf("want one warning naming 'slack thread', got %v", res.Warnings)
	}
	// All-defined inputs → no warnings.
	clean := createTypedRec(t, api, "review-pr", map[string]any{"PR Link": "https://x/2"})
	if w := decodeBody[taskCreateResultDTO](t, clean).Warnings; len(w) != 0 {
		t.Fatalf("clean create must have no warnings, got %v", w)
	}
}

func TestCreateAdHocNeverWarns(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "adhoc", "executor_member_id": "m-exec",
			"inputs": map[string]any{"anything": "goes"}}, "m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("adhoc create: %d %s", rec.Code, rec.Body.String())
	}
	if w := decodeBody[taskCreateResultDTO](t, rec).Warnings; len(w) != 0 {
		t.Fatalf("ad-hoc has no manual → no warnings, got %v", w)
	}
}

// ── manual-side K1: is_key ⟹ required ────────────────────────────────────────

func updateManualFields(t *testing.T, api *apiServer, typeKey string, fields []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
		taskReq(t, "POST", "/api/task-manuals/"+typeKey,
			map[string]any{"fields": fields}, "owner", "owner"),
		typeKey)
	return rec
}

func TestUpdateManualEnforcesIsKeyRequired(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutTaskManual(TaskManual{TypeKey: "m", Fields: "[]"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// is_key without required → 400.
	if rec := updateManualFields(t, api, "m", []map[string]any{
		{"name": "PR Link", "is_key": true, "required": false},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("is_key not required: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// is_key omitting required (defaults false) → 400.
	if rec := updateManualFields(t, api, "m", []map[string]any{
		{"name": "PR Link", "is_key": true},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("is_key default-required: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// is_key AND required → 200.
	if rec := updateManualFields(t, api, "m", []map[string]any{
		{"name": "PR Link", "is_key": true, "required": true},
		{"name": "note", "is_key": false, "required": false},
	}); rec.Code != http.StatusOK {
		t.Fatalf("valid manual: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── migration 00010: one-shot backfill ───────────────────────────────────────

// gooseUpSQL extracts the executable statements of a goose migration's Up block
// (everything between the +goose Up / +goose Down markers, comment lines
// stripped) so the test exercises the REAL migration SQL, not a copy.
func gooseUpSQL(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up := ""
	inUp := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(trimmed, "-- +goose Down") {
			break
		}
		if !inUp || strings.HasPrefix(trimmed, "--") {
			continue
		}
		up += line + "\n"
	}
	if strings.TrimSpace(up) == "" {
		t.Fatalf("no Up SQL extracted from %s", path)
	}
	return up
}

func TestMigration00010BackfillsIsKeyRequired(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "mig.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(
		`CREATE TABLE task_manual (type_key TEXT PRIMARY KEY, fields TEXT NOT NULL DEFAULT '[]')`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	seed := map[string]string{
		"legacy":    `[{"name":"PR Link","required":false,"is_key":true}]`,
		"already":   `[{"name":"pr","required":true,"is_key":true}]`,
		"nonkey":    `[{"name":"note","required":false,"is_key":false}]`,
		"composite": `[{"name":"pr","required":false,"is_key":true},{"name":"repo","required":false,"is_key":true},{"name":"note","required":false,"is_key":false}]`,
		"malformed": `{not json`,
	}
	for k, v := range seed {
		if _, err := db.Exec(
			`INSERT INTO task_manual (type_key, fields) VALUES (?, ?)`, k, v); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	up := gooseUpSQL(t, "migrations/00010_task_manual_iskey_required.sql")
	// Run twice: the second run must be a no-op (idempotent).
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(up); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	read := func(typeKey string) []ManualField {
		var blob string
		if err := db.QueryRow(
			`SELECT fields FROM task_manual WHERE type_key = ?`, typeKey).Scan(&blob); err != nil {
			t.Fatalf("read %s: %v", typeKey, err)
		}
		f, err := ParseManualFields(blob)
		if err != nil {
			t.Fatalf("parse %s (%q): %v", typeKey, blob, err)
		}
		return f
	}

	// legacy: the sole is_key field flipped to required, name preserved.
	if f := read("legacy"); len(f) != 1 || !f[0].Required || !f[0].IsKey || f[0].Name != "PR Link" {
		t.Fatalf("legacy: %+v", f)
	}
	// already-required: unchanged.
	if f := read("already"); len(f) != 1 || !f[0].Required {
		t.Fatalf("already: %+v", f)
	}
	// non-key: required stays false (never touched).
	if f := read("nonkey"); len(f) != 1 || f[0].Required || f[0].IsKey {
		t.Fatalf("nonkey: %+v", f)
	}
	// composite: both is_key fields required, the non-key one still optional.
	f := read("composite")
	if len(f) != 3 || !f[0].Required || !f[1].Required || f[2].Required {
		t.Fatalf("composite: %+v", f)
	}
	if f[0].Name != "pr" || f[1].Name != "repo" || f[2].Name != "note" {
		t.Fatalf("composite order not preserved: %+v", f)
	}
	// malformed JSON: skipped, left byte-for-byte intact.
	var raw string
	if err := db.QueryRow(
		`SELECT fields FROM task_manual WHERE type_key = 'malformed'`).Scan(&raw); err != nil {
		t.Fatalf("read malformed: %v", err)
	}
	if raw != seed["malformed"] {
		t.Fatalf("malformed row mutated: %q", raw)
	}
}
