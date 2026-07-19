package main

// api_taskmanuals_test.go — the manual-authorship split's server-side pins
// (owner ruling 2026-07-13): manual CONTENT (create / purpose / fields /
// sop_md / learnings) is agent-writable, the ASSIGNEE face is owner-only
// governance — a non-owner supplying `assignee` on create or edit is a flat
// 403 from the in-handler gate (the route floor is agent; the gate is the
// extra choke). Handlers are invoked directly (route-table auth is pinned by
// the conformance matrix; the gate reads the injected claims).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateManualMintsSystemKeyFromDisplayName pins the T-fa76 owner ruling:
// the type id is the SYSTEM's ("tm-"+hex12, minted server-side and returned
// in the DTO), the user's text is ONLY the display face. The legacy explicit
// type_key path stays alive (deprecated) with the display_name backfilled to
// the key; both blank is a 400.
func TestCreateManualMintsSystemKeyFromDisplayName(t *testing.T) {
	api := newTasksTestServer(t)

	// New path: display_name only → the server mints the tm- id and echoes
	// both faces back — the caller addresses later calls by the returned key.
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals", map[string]any{"display_name": " 審查 PR "},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("display_name create must 200, got %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		TypeKey     string `json:"type_key"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("create response: %v", err)
	}
	if !strings.HasPrefix(dto.TypeKey, "tm-") || len(dto.TypeKey) != len("tm-")+12 {
		t.Fatalf("minted key must be tm-+hex12, got %q", dto.TypeKey)
	}
	if dto.DisplayName != "審查 PR" {
		t.Fatalf("display_name must be the trimmed input, got %q", dto.DisplayName)
	}
	if m, err := api.dal.GetTaskManual(dto.TypeKey); err != nil || m == nil ||
		m.DisplayName != "審查 PR" {
		t.Fatalf("manual readback by minted key: %+v %v", m, err)
	}

	// Legacy path: an explicit type_key is the id verbatim, and a blank
	// display_name backfills to it (old MCP callers keep a display face).
	rec = httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals", map[string]any{"type_key": "legacy-type"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy type_key create must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if m, err := api.dal.GetTaskManual("legacy-type"); err != nil || m == nil ||
		m.DisplayName != "legacy-type" {
		t.Fatalf("legacy backfill display_name=type_key: %+v %v", m, err)
	}

	// Legacy path with its OWN display_name keeps it (no backfill clobber).
	rec = httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals",
		map[string]any{"type_key": "legacy-named", "display_name": "有名字"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy named create must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if m, _ := api.dal.GetTaskManual("legacy-named"); m == nil ||
		m.DisplayName != "有名字" {
		t.Fatalf("explicit display_name must win over backfill: %+v", m)
	}

	// Duplicate legacy key stays the 409.
	rec = httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals", map[string]any{"type_key": "legacy-type"},
		"m-exec", "agent"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate legacy key must 409, got %d %s", rec.Code, rec.Body.String())
	}

	// Both faces blank/absent → 400 (nothing to name the type by).
	for _, body := range []map[string]any{
		{}, {"display_name": "  "}, {"type_key": "", "display_name": ""},
	} {
		rec = httptest.NewRecorder()
		api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
			"/api/task-manuals", body, "m-exec", "agent"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("blank create %v must 400, got %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

// TestManualDisplayLabel pins the prose face: distinct display names carry
// the addressing key in parentheses; a blank or key-equal display name is
// the bare key (no "x（x）" stutter).
func TestManualDisplayLabel(t *testing.T) {
	if got := manualDisplayLabel("重構後端", "tm-1a2b3c4d5e6f"); got != "重構後端（tm-1a2b3c4d5e6f）" {
		t.Fatalf("distinct display label: %q", got)
	}
	if got := manualDisplayLabel("", "review-pr"); got != "review-pr" {
		t.Fatalf("blank display must be the bare key: %q", got)
	}
	if got := manualDisplayLabel("review-pr", "review-pr"); got != "review-pr" {
		t.Fatalf("key-equal display must not stutter: %q", got)
	}
}

func TestAgentCreatesManualAndEditsContentFields(t *testing.T) {
	api := newTasksTestServer(t)

	// An agent creates a blank manual.
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals", map[string]any{"type_key": "review-pr"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent create must 200, got %d %s", rec.Code, rec.Body.String())
	}

	// The same agent edits the content fields.
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/x", map[string]any{
			"purpose": "review pull requests",
			"sop_md":  "1. read the diff",
			"fields":  []map[string]any{{"name": "pr", "required": true, "is_key": true}},
		}, "m-exec", "agent"), "review-pr")
	if rec.Code != http.StatusOK {
		t.Fatalf("agent content edit must 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := api.dal.GetTaskManual("review-pr")
	if err != nil || m == nil {
		t.Fatalf("manual readback: %v %v", m, err)
	}
	if m.Purpose != "review pull requests" || m.SopMD != "1. read the diff" {
		t.Fatalf("content edit not applied: %+v", m)
	}
	if m.Assignee != "{}" {
		t.Fatalf("assignee must stay unset, got %q", m.Assignee)
	}
}

func TestAgentSuppliedAssigneeIs403OnCreateAndEdit(t *testing.T) {
	api := newTasksTestServer(t)
	assignee := map[string]any{"kind": "member", "member_id": "m-exec"}

	// Create carrying assignee → 403, and NO manual is written.
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals",
		map[string]any{"type_key": "gov-type", "assignee": assignee},
		"m-exec", "agent"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent create+assignee must 403, got %d %s", rec.Code, rec.Body.String())
	}
	if m, _ := api.dal.GetTaskManual("gov-type"); m != nil {
		t.Fatalf("refused create must write nothing, got %+v", m)
	}

	// Edit carrying assignee → 403 (deny-first: even on a missing type the
	// governance refusal wins, mirroring the admin routes' 403-before-404).
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/x", map[string]any{"assignee": assignee}, "m-exec", "agent"),
		"gov-type")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent edit+assignee must 403, got %d %s", rec.Code, rec.Body.String())
	}

	// A JSON-null assignee is ABSENT, not a governance write — content-only
	// edits keep flowing for agents.
	seedManualWithKey(t, api, "gov-type")
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/x", map[string]any{"purpose": "p", "assignee": nil}, "m-exec", "agent"),
		"gov-type")
	if rec.Code != http.StatusOK {
		t.Fatalf("agent edit with null assignee must 200, got %d %s",
			rec.Code, rec.Body.String())
	}
}

func TestOwnerAssigneeOnCreateIsValidatedAndApplied(t *testing.T) {
	api := newTasksTestServer(t)

	// A malformed assignee is the shared 400 (validateManualAssignee).
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals",
		map[string]any{"type_key": "own-type",
			"assignee": map[string]any{"kind": "member"}},
		"owner", "owner"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("owner bad assignee must 400, got %d %s", rec.Code, rec.Body.String())
	}

	// A well-formed owner assignee lands on the created manual.
	rec = httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals",
		map[string]any{"type_key": "own-type",
			"assignee": map[string]any{"kind": "member", "member_id": "m-exec"}},
		"owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner create+assignee must 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := api.dal.GetTaskManual("own-type")
	if err != nil || m == nil {
		t.Fatalf("manual readback: %v %v", m, err)
	}
	if m.Assignee != `{"kind":"member","member_id":"m-exec"}` {
		t.Fatalf("owner assignee not applied: %q", m.Assignee)
	}
}
