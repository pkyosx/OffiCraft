package main

// api_taskmanuals_test.go — the manual-authorship split's server-side pins
// (owner ruling 2026-07-13, floor lowered by T-6020 owner ruling 2026-07-26):
// manual CONTENT (create / purpose / fields / sop_md / learnings) is
// agent-writable, the ASSIGNEE face is GOVERNANCE — admitted for owner AND
// admin_agent, while a PLAIN agent supplying `assignee` on create or edit is a
// flat 403 from the in-handler gate (the route floor is agent; the gate is the
// extra choke). Both sides are pinned here: the plain-agent refusal in
// TestAgentSuppliedAssigneeIs403OnCreateAndEdit, the admin_agent success in
// TestAdminAgentAssigneeIsAppliedOnCreateAndEdit — a boundary needs both or it
// is only a direction. Handlers are invoked directly (route-table auth is
// pinned by the conformance matrix; the gate reads the injected claims).

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
	// T-91: the create receipt carries the MINTED key and nothing the caller
	// sent — so display_name is no longer on it. That is exactly the split this
	// test was always about: the key is news on this face (the server minted
	// it), the display name is the caller's own trimmed string. The trimming is
	// still asserted, one line down, against the STORED row — which is where the
	// claim actually belongs and is a stronger place to make it.
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
	if dto.DisplayName != "" {
		t.Fatalf("the create receipt must not echo display_name back: %q", dto.DisplayName)
	}
	if m, err := api.dal.GetTaskManual(dto.TypeKey); err != nil || m == nil ||
		m.DisplayName != "審查 PR" {
		t.Fatalf("manual readback by minted key (display_name must be the trimmed input): %+v %v", m, err)
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

// TestManualOutsourceAssigneeMachineMustNameARealMachine pins the assignee's
// `machine` contract on BOTH handlers: "auto" is a flat 400 (it names no
// machine), a shaped-but-unknown id is the resolve 404, and a real machine id
// lands — so every worker of the type has somewhere to boot.
func TestManualOutsourceAssigneeMachineMustNameARealMachine(t *testing.T) {
	api := newTasksTestServer(t)
	seedMachine(t, api, "m-real")

	create := func(typeKey, machine string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
			"/api/task-manuals", map[string]any{"type_key": typeKey,
				"assignee": map[string]any{"kind": "outsource", "machine": machine}},
			"owner", "owner"))
		return rec
	}
	update := func(typeKey, machine string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
			"/x", map[string]any{
				"assignee": map[string]any{"kind": "outsource", "machine": machine}},
			"owner", "owner"), typeKey)
		return rec
	}

	// SENTINEL: a real machine id is accepted and stored, on both faces.
	if rec := create("ok-type", "m-real"); rec.Code != http.StatusOK {
		t.Fatalf("create with a real machine must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if m, _ := api.dal.GetTaskManual("ok-type"); m == nil ||
		!strings.Contains(m.Assignee, `"machine":"m-real"`) {
		t.Fatalf("the assignee machine must land: %+v", m)
	}
	if rec := update("ok-type", "m-real"); rec.Code != http.StatusOK {
		t.Fatalf("update with a real machine must 200, got %d %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		machine string
		want    int
	}{
		{"auto", http.StatusBadRequest},  // never a machine — refused on shape
		{"m-ghost", http.StatusNotFound}, // shaped fine, names nothing
	} {
		if rec := create("bad-"+tc.machine, tc.machine); rec.Code != tc.want {
			t.Fatalf("create machine %q: want %d, got %d %s",
				tc.machine, tc.want, rec.Code, rec.Body.String())
		}
		if m, _ := api.dal.GetTaskManual("bad-" + tc.machine); m != nil {
			t.Fatalf("a refused create must write nothing, got %+v", m)
		}
		if rec := update("ok-type", tc.machine); rec.Code != tc.want {
			t.Fatalf("update machine %q: want %d, got %d %s",
				tc.machine, tc.want, rec.Code, rec.Body.String())
		}
		if m, _ := api.dal.GetTaskManual("ok-type"); m == nil ||
			!strings.Contains(m.Assignee, `"machine":"m-real"`) {
			t.Fatalf("a refused update must leave the assignee alone: %+v", m)
		}
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

// TestAdminAgentAssigneeIsAppliedOnCreateAndEdit pins the T-6020 half of the
// assignee gate (owner ruling 2026-07-26): the floor moved from owner to
// admin_agent, so an admin 助理 sets who executes a task type on BOTH faces.
// The plain-agent refusal directly above is the other half — the two together
// are what makes "governance, not owner-only, and not agent-open" a real
// boundary rather than a direction.
func TestAdminAgentAssigneeIsAppliedOnCreateAndEdit(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutMember(Member{
		ID: "m-admin", Kind: KindStaff, RoleKey: adminRoleKey,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	assignee := map[string]any{"kind": "member", "member_id": "m-exec"}

	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals",
		map[string]any{"type_key": "adm-type", "assignee": assignee},
		"m-admin", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin create+assignee must 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := api.dal.GetTaskManual("adm-type")
	if err != nil || m == nil {
		t.Fatalf("manual readback: %v %v", m, err)
	}
	if m.Assignee != `{"kind":"member","member_id":"m-exec"}` {
		t.Fatalf("admin assignee not applied on create: %q", m.Assignee)
	}

	seedManualWithKey(t, api, "adm-edit")
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/x", map[string]any{"assignee": assignee}, "m-admin", "agent"),
		"adm-edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin edit+assignee must 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err = api.dal.GetTaskManual("adm-edit")
	if err != nil || m == nil {
		t.Fatalf("manual readback: %v %v", m, err)
	}
	if m.Assignee != `{"kind":"member","member_id":"m-exec"}` {
		t.Fatalf("admin assignee not applied on edit: %q", m.Assignee)
	}
}
