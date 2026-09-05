package main

// The 初始版本 read face (T-40f0, owner ruling rc-28885813e065 option ①).
//
// Before this route the version list's bottom row — the document's shipped
// default — went STRAIGHT to a restore confirmation, because the seed text was
// something the server only ever handed back AFTER a reset had already
// overwritten the document. So the one entry whose restore is least reversible
// was the only one the owner could not look at first.
//
// What is pinned here:
//   1. LOOKING NEVER WRITES. This is the whole safety claim of the change, so it
//      is measured as "the live document is byte-identical afterwards, and no
//      revision was retained", not as "the handler has no write call in it".
//   2. The seed comes back under the SAME field names a retained revision uses,
//      so the cockpit's one reader/diff serves both.
//   3. 404 lands in exactly the places a RESET 404s — a custom role, a task
//      manual, per-role lessons. A row whose compare works but whose restore
//      404s (or the reverse) is worse than no row.
//   4. The read floor is the read floor: the same machine floor as listing the
//      retained versions, NOT the admin floor the restore keeps.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getDocumentSeed(t *testing.T, api *apiServer, kind, key, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/"+kind+"/"+key+"/seed", nil, sub, scope), kind, key)
	return rec
}

func decodeDocumentSeed(t *testing.T, rec *httptest.ResponseRecorder) DocumentSeedDTO {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("seed read: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dto DocumentSeedDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	return dto
}

// The global block's default is the EMPTY document, and the field name must be
// present: to a diff, an absent key and an empty string are different documents
// ("nothing to compare" vs "everything you wrote would go away").
func TestGetDocumentSeed_GlobalContextIsTheEmptyDocument(t *testing.T) {
	api := newTasksTestServer(t)
	dto := decodeDocumentSeed(t, getDocumentSeed(t, api, "global_context", "global", "owner", "owner"))
	if dto.Kind != "global_context" || dto.Key != "global" {
		t.Fatalf("seed echo = %q/%q, want global_context/global", dto.Kind, dto.Key)
	}
	text, ok := dto.Content["text"]
	if !ok || text != "" {
		t.Fatalf("seed content = %+v, want an explicit empty \"text\"", dto.Content)
	}
	if dto.Content["tombstoned"] != "true" {
		t.Fatalf("seed content = %+v, want tombstoned=true (a reset writes the tombstone)", dto.Content)
	}
}

// The seed role's default is the FILE seed, under the same `definition_md` name
// a retained revision of that document carries.
func TestGetDocumentSeed_SeedRoleReturnsTheFileSeedUnderTheRevisionFieldName(t *testing.T) {
	api := newTasksTestServer(t)
	// Overwrite the role first: the seed must come from the FILE, never from
	// whatever the document happens to say now.
	rec := httptest.NewRecorder()
	api.HandleUpdateRoleApiRolesRolePost(rec, taskReq(t, http.MethodPost, "/api/roles/assistant",
		map[string]any{"definition_md": "owner's rewrite"}, "owner", "owner"), "assistant")
	if rec.Code != http.StatusOK {
		t.Fatalf("role write: status=%d body=%s", rec.Code, rec.Body.String())
	}

	dto := decodeDocumentSeed(t, getDocumentSeed(t, api, "role_definition", "assistant", "owner", "owner"))
	wantMD, hasSeed, err := api.root.seedRoleDefinitionMD("assistant")
	if err != nil || !hasSeed {
		t.Fatalf("fixture: seed role definition unreadable (hasSeed=%v err=%v)", hasSeed, err)
	}
	if wantMD == "" {
		t.Fatal("fixture: the seed role definition is empty — this test would be vacuous")
	}
	if dto.Content["definition_md"] != wantMD {
		t.Fatalf("seed definition_md = %q, want the file seed %q", dto.Content["definition_md"], wantMD)
	}
	if dto.Content["definition_md"] == "owner's rewrite" {
		t.Fatal("the seed read handed back the LIVE document, not the shipped default")
	}
	if dto.Content["tombstoned"] != "true" {
		t.Fatalf("seed content = %+v, want tombstoned=true", dto.Content)
	}
}

// RED LINE of this change: reading 初始版本 must not be able to restore it.
// Restoring replaces everything the owner has written, so the compare path is
// asserted to leave BOTH the document and its retained history untouched.
func TestGetDocumentSeed_ReadingNeverWritesTheDocument(t *testing.T) {
	api := newTasksTestServer(t)
	for _, text := range []string{"first", "second"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
			"/api/global-context", map[string]any{"text": text}, "owner", "owner"))
		if rec.Code != http.StatusOK {
			t.Fatalf("write %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}
	before, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	historyBefore, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if before.Text != "second" || len(historyBefore) == 0 {
		t.Fatalf("fixture: want a written document with history, got %q / %d versions",
			before.Text, len(historyBefore))
	}

	// Read the seed twice — an idempotence claim a single call cannot make.
	for i := 0; i < 2; i++ {
		decodeDocumentSeed(t, getDocumentSeed(t, api, "global_context", "global", "owner", "owner"))
	}

	after, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || after.IsDefault != before.IsDefault {
		t.Fatalf("reading the seed changed the live document: %q/%v → %q/%v",
			before.Text, before.IsDefault, after.Text, after.IsDefault)
	}
	historyAfter, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("reading the seed retained a revision: %d → %d versions",
			len(historyBefore), len(historyAfter))
	}
	for i := range historyAfter {
		if historyAfter[i].ContentJSON != historyBefore[i].ContentJSON {
			t.Fatalf("reading the seed rewrote retained revision %d", i)
		}
	}
}

// The 404 set must equal the set of documents whose RESET 404s, so the cockpit
// can keep deriving the 初始版本 row from one fact.
func TestGetDocumentSeed_404sExactlyWhereAResetDoes(t *testing.T) {
	api := newTasksTestServer(t)

	// A custom role: no file seed, and POST /api/roles/{role}/reset 404s.
	rec := httptest.NewRecorder()
	api.HandleCreateRoleApiRolesPost(rec, taskReq(t, http.MethodPost, "/api/roles",
		map[string]any{"name": "Seedless Role"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create role: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created roleCreateResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// A task manual, so the manual kinds are probed against a document that
	// really exists — otherwise a 404 would prove nothing about the seed.
	rec = httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, http.MethodPost, "/api/task-manuals",
		map[string]any{"type_key": "weekly-report"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create manual: status=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, probe := range []struct{ kind, key string }{
		{"role_definition", created.RoleKey},
		{"task_manual_sop", "weekly-report"},
		{"task_manual_learnings", "weekly-report"},
		{"lessons", created.RoleKey},
	} {
		got := getDocumentSeed(t, api, probe.kind, probe.key, "owner", "owner")
		if got.Code != http.StatusNotFound {
			t.Fatalf("%s/%s: status=%d body=%s, want 404 (no shipped default)",
				probe.kind, probe.key, got.Code, got.Body.String())
		}
	}

	// Positive control in the same test: the equivalence is an equivalence, not
	// a blanket 404.
	if got := getDocumentSeed(t, api, "role_definition", "assistant", "owner", "owner"); got.Code != http.StatusOK {
		t.Fatalf("seed role: status=%d body=%s, want 200", got.Code, got.Body.String())
	}
}

// The route reuses the READ authorization, not the restore's. A plain agent may
// read it (machine floor) — the restore of the same document is admin-gated and
// stays that way.
func TestGetDocumentSeed_KeepsTheReadFloorNotTheRestoreFloor(t *testing.T) {
	api := newTasksTestServer(t)
	if got := getDocumentSeed(t, api, "global_context", "global", "some-agent", "agent"); got.Code != http.StatusOK {
		t.Fatalf("plain agent reading the seed: status=%d body=%s, want 200 (read floor)",
			got.Code, got.Body.String())
	}
	// The counterfactual that makes the line above mean something: the same
	// document's RESTORE refuses the same caller.
	rec := httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		taskReq(t, http.MethodPost, "/api/document-history/global_context/global/1/restore",
			nil, "some-agent", "agent"), "global_context", "global", 1)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("plain agent restoring: status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

// The retired four-field manual kind is refused by NAME on the read face too —
// an empty/404 answer would read as "this manual has no default".
func TestGetDocumentSeed_RefusesTheRetiredManualKindByName(t *testing.T) {
	api := newTasksTestServer(t)
	got := getDocumentSeed(t, api, docKindTaskManual, "weekly-report", "owner", "owner")
	if got.Code != http.StatusBadRequest {
		t.Fatalf("retired kind: status=%d body=%s, want 400", got.Code, got.Body.String())
	}
	if !strings.Contains(got.Body.String(), "task_manual_sop") {
		t.Fatalf("retired-kind refusal must name the replacements: %s", got.Body.String())
	}
}

// The seeded role's INSIGHT default is the per-role file seed, under the same
// `text` field name a retained insight revision carries
// (insightHistorySnapshot).
//
// 🔴 THIS IS THE THIRD RESET ROUTE, and it was missing from documentSeedContent
// while `InsightDTO.has_seed` already said the default exists. That pair is the
// bug: the cockpit gates the 初始版本 row on has_seed=true, so the row rendered
// and opened, and then the modal could not read what the row was for. The 404
// set must equal the RESET-404 set, and `POST /api/insight/{role_key}/reset`
// does not 404 here.
func TestGetDocumentSeed_SeedRoleInsightReturnsTheFileSeedUnderTheRevisionFieldName(t *testing.T) {
	api := newTasksTestServer(t)
	// Overwrite the doc first: the seed must come from the FILE, never from
	// whatever the live document happens to say now.
	rec := httptest.NewRecorder()
	api.HandleReplaceInsightApiInsightRoleKeyPost(rec, taskReq(t, http.MethodPost,
		"/api/insight/assistant", map[string]any{"text": "owner's rewrite"}, "owner", "owner"), "assistant")
	if rec.Code != http.StatusOK {
		t.Fatalf("insight write: status=%d body=%s", rec.Code, rec.Body.String())
	}

	dto := decodeDocumentSeed(t, getDocumentSeed(t, api, "insight", "assistant", "owner", "owner"))
	if dto.Kind != "insight" || dto.Key != "assistant" {
		t.Fatalf("seed echo = %q/%q, want insight/assistant", dto.Kind, dto.Key)
	}
	wantMD, hasSeed, err := api.root.seedInsightMD("assistant")
	if err != nil || !hasSeed {
		t.Fatalf("fixture: seed insight unreadable (hasSeed=%v err=%v)", hasSeed, err)
	}
	if wantMD == "" {
		t.Fatal("fixture: the seed insight is empty — this test would be vacuous")
	}
	// 🔴 `text`, not `definition_md`: the diff in DocumentHistoryModal compares
	// this map against a retained revision's map key-by-key, so the wrong field
	// name shows up as "no differences" rather than as an error.
	if dto.Content["text"] != wantMD {
		t.Fatalf("seed text = %q, want the file seed %q", dto.Content["text"], wantMD)
	}
	if dto.Content["text"] == "owner's rewrite" {
		t.Fatal("the seed read handed back the LIVE document, not the shipped default")
	}
	if dto.Content["tombstoned"] != "true" {
		t.Fatalf("seed content = %+v, want tombstoned=true (a reset writes the tombstone)", dto.Content)
	}
}

// The 200/404 split for insight is per-ROLE, because the insight seed roster is
// the SET OF FILES (seeds/insight_<role>.md) — not the role roster. A custom
// role has no insight seed, its reset 404s, and so must this read.
func TestGetDocumentSeed_InsightIsPerRoleNotBlanket(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleCreateRoleApiRolesPost(rec, taskReq(t, http.MethodPost, "/api/roles",
		map[string]any{"name": "Seedless Role"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create role: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created roleCreateResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if got := getDocumentSeed(t, api, "insight", created.RoleKey, "owner", "owner"); got.Code != http.StatusNotFound {
		t.Fatalf("custom role insight: status=%d body=%s, want 404", got.Code, got.Body.String())
	}
	// Positive control: the seeded role in the same test answers 200.
	if got := getDocumentSeed(t, api, "insight", "assistant", "owner", "owner"); got.Code != http.StatusOK {
		t.Fatalf("seed role insight: status=%d body=%s, want 200", got.Code, got.Body.String())
	}
}
