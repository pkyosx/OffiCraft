package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentHistoryRestoreKeepsCurrentDocumentAndRestoresSnapshot(t *testing.T) {
	api := newTasksTestServer(t)
	for _, text := range []string{"one", "two", "three", "four"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
			"/api/global-context", map[string]any{"text": text}, "owner", "owner"))
		if rec.Code != http.StatusOK {
			t.Fatalf("write %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "owner", "owner"), "global_context", "global")
	if rec.Code != http.StatusOK {
		t.Fatalf("list history: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	history := hydrateHistory(t, api, "global_context", "global", "owner", "owner", rows)
	if len(history) != 3 || history[0].Content["text"] != "three" || history[2].Content["text"] != "one" {
		t.Fatalf("retained history = %+v, want three versions from three through one", history)
	}

	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
		"/api/document-history/global_context/global/restore", nil, "owner", "owner"), "global_context", "global", history[1].Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: status=%d body=%s", rec.Code, rec.Body.String())
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("restored text = %q, want two", current.Text)
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("history after restore = %d versions, want 3", len(stored))
	}
	var restoredCurrent map[string]string
	if err := json.Unmarshal([]byte(stored[0].ContentJSON), &restoredCurrent); err != nil {
		t.Fatal(err)
	}
	if restoredCurrent["text"] != "four" {
		t.Fatalf("restore did not retain the replaced current document: %+v", restoredCurrent)
	}
}

func TestDocumentHistoryRestorePreservesOverlayTombstones(t *testing.T) {
	api := newTasksTestServer(t)
	ownerReq := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	list := func(kind, key string) []historyRow {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
			ownerReq(http.MethodGet, "/api/document-history/"+kind+"/"+key, nil), kind, key)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
		}
		var rows []DocumentHistoryDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return hydrateHistory(t, api, kind, key, "owner", "owner", rows)
	}
	restore := func(kind, key string, id int64) {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			ownerReq(http.MethodPost, "/api/document-history/"+kind+"/"+key+"/restore", nil), kind, key, id)
		if rec.Code != http.StatusOK {
			t.Fatalf("restore %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
		}
	}

	// A subsequent write records the reset's persisted tombstone, then restore
	// must preserve it rather than materializing a non-default overlay.
	for _, text := range []string{"custom", "later"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
			ownerReq(http.MethodPost, "/api/global-context", map[string]any{"text": text}))
		if rec.Code != http.StatusOK {
			t.Fatalf("global write %q: %d %s", text, rec.Code, rec.Body.String())
		}
		if text == "custom" {
			rec = httptest.NewRecorder()
			api.HandleResetGlobalContextApiGlobalContextResetPost(rec, ownerReq(http.MethodPost, "/api/global-context/reset", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("global reset: %d %s", rec.Code, rec.Body.String())
			}
		}
	}
	globalHistory := list("global_context", "global")
	if globalHistory[0].Content["tombstoned"] != "true" {
		t.Fatalf("global reset snapshot = %+v, want tombstoned=true", globalHistory[0].Content)
	}
	restore("global_context", "global", globalHistory[0].Id)
	global, err := api.dal.GetUserContext()
	if err != nil || global == nil || !global.Tombstoned {
		t.Fatalf("restored global overlay = %+v, %v; want tombstone", global, err)
	}

	// A custom role can also carry a tombstone in historical data (for example,
	// a later version of the product may make its deletion restorable). Restore
	// must not turn that state into a live overlay.
	role := "r-history"
	if err := api.dal.PutRoleDef(RoleDef{RoleKey: role, Name: "later", DefinitionMD: "later"}); err != nil {
		t.Fatal(err)
	}
	tombstonedRole, err := roleDefHistorySnapshot(&RoleDef{RoleKey: role, Tombstoned: true})
	if err != nil {
		t.Fatal(err)
	}
	seedTombstoned := func(sqlQuerier) (string, error) { return tombstonedRole, nil }
	if err := api.dal.SaveWithDocumentHistory("role_definition", role, "owner", seedTombstoned, func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{RoleKey: role, Name: "later", DefinitionMD: "later"})
	}); err != nil {
		t.Fatal(err)
	}
	roleHistory := list("role_definition", role)
	if roleHistory[0].Content["tombstoned"] != "true" {
		t.Fatalf("role reset snapshot = %+v, want tombstoned=true", roleHistory[0].Content)
	}
	restore("role_definition", role, roleHistory[0].Id)
	roleOverlay, err := api.dal.GetRoleDef(role)
	if err != nil || roleOverlay == nil || !roleOverlay.Tombstoned {
		t.Fatalf("restored role overlay = %+v, %v; want tombstone", roleOverlay, err)
	}

	if err := api.dal.PutLessons(Lessons{RoleKey: role, TaskType: seedLessonsTaskType, Tombstoned: true}); err != nil {
		t.Fatal(err)
	}
	lessonsSnapshot, err := lessonsHistorySnapshot(&Lessons{RoleKey: role, TaskType: seedLessonsTaskType, Tombstoned: true})
	if err != nil {
		t.Fatal(err)
	}
	var lessonsContent map[string]string
	if err := json.Unmarshal([]byte(lessonsSnapshot), &lessonsContent); err != nil || !historyTombstoned(lessonsContent) {
		t.Fatalf("lessons tombstone snapshot = %s, %v; want preserved tombstone", lessonsSnapshot, err)
	}
}

// agentList reads a document's history as a plain agent: the routes sit on the
// machine floor, so "the document was deleted" has to mean the versions are
// gone, not merely that the cockpit stopped linking to them.
func agentList(t *testing.T, api *apiServer, kind, key string) []historyRow {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/"+kind+"/"+key, nil, "m-agent", "agent"), kind, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	return hydrateHistory(t, api, kind, key, "m-agent", "agent", rows)
}

func replaceLessonsThrough(t *testing.T, api *apiServer, roleKey, taskType, text string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec, taskReq(t, http.MethodPost,
		"/api/lessons/"+roleKey+"/"+taskType, map[string]any{"text": text}, "owner", "owner"),
		roleKey, taskType)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace lessons %s/%s: status=%d body=%s", roleKey, taskType, rec.Code, rec.Body.String())
	}
}

func updateRoleThrough(t *testing.T, api *apiServer, role, definition string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateRoleApiRolesRolePost(rec, taskReq(t, http.MethodPost,
		"/api/roles/"+role, map[string]any{"definition_md": definition}, "owner", "owner"), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("update role %s: status=%d body=%s", role, rec.Code, rec.Body.String())
	}
}

// Deleting a role must take its retained versions with it — its own definition
// history and the lessons history of every task type it owned. Left behind,
// those versions stay readable by any authenticated caller, which is exactly
// the "permanently removed" promise the guide makes.
func TestDeletingARoleRemovesItsRetainedDocumentHistory(t *testing.T) {
	api := newTasksTestServer(t)
	const role, neighbour = "r-cascade", "r-cascadex"
	for _, key := range []string{role, neighbour} {
		if err := api.dal.PutRoleDef(RoleDef{RoleKey: key, Name: key, DefinitionMD: "v0"}); err != nil {
			t.Fatal(err)
		}
		updateRoleThrough(t, api, key, "v1")
		updateRoleThrough(t, api, key, "v2")
		for _, taskType := range []string{seedLessonsTaskType, "tm-second"} {
			replaceLessonsThrough(t, api, key, taskType, "lesson one")
			replaceLessonsThrough(t, api, key, taskType, "lesson two")
		}
	}
	if len(agentList(t, api, "role_definition", role)) == 0 ||
		len(agentList(t, api, "lessons", role+"::"+seedLessonsTaskType)) == 0 {
		t.Fatal("the fixture retained nothing — the deletion assertions below would prove nothing")
	}

	rec := httptest.NewRecorder()
	api.HandleDeleteRoleApiRolesRoleDelete(rec, taskReq(t, http.MethodDelete,
		"/api/roles/"+role, nil, "owner", "owner"), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete role: status=%d body=%s", rec.Code, rec.Body.String())
	}
	result := decodeBody[map[string]any](t, rec)
	if got := result["deleted_lessons"]; got != float64(2) {
		t.Errorf("deleted_lessons = %v, want 2 — the cascade changed the handler's answer", got)
	}

	if history := agentList(t, api, "role_definition", role); len(history) != 0 {
		t.Errorf("the deleted role still has %d readable definition versions: %+v", len(history), history)
	}
	for _, taskType := range []string{seedLessonsTaskType, "tm-second"} {
		if history := agentList(t, api, "lessons", role+"::"+taskType); len(history) != 0 {
			t.Errorf("the deleted role still has %d readable lessons versions under %q: %+v",
				len(history), taskType, history)
		}
	}
	// Only that role's documents: the prefix match includes the "::" separator,
	// so a role whose key merely starts with the same characters keeps its own.
	if len(agentList(t, api, "role_definition", neighbour)) == 0 ||
		len(agentList(t, api, "lessons", neighbour+"::"+seedLessonsTaskType)) == 0 {
		t.Error("deleting a role also erased the history of a role with a similar key")
	}
}

// The task-manual twin. Its history is keyed by type_key alone, so a surviving
// revision is a complete copy of a manual the owner deleted.
func TestDeletingATaskManualRemovesItsRetainedDocumentHistory(t *testing.T) {
	f := newHistoryFixture(t)
	create := func(name string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleCreateTaskManualApiTaskManualsPost(rec,
			f.req(http.MethodPost, "/api/task-manuals", map[string]any{"display_name": name}))
		if rec.Code != http.StatusOK {
			t.Fatalf("create manual: status=%d body=%s", rec.Code, rec.Body.String())
		}
		return decodeBody[taskManualDTO](t, rec).TypeKey
	}
	update := func(typeKey, learnings string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
			f.req(http.MethodPost, "/api/task-manuals/"+typeKey, map[string]any{"learnings": learnings}), typeKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("update manual: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	kinds := []string{docKindTaskManualSop, docKindTaskManualLearnings}
	doomed, neighbour := create("Doomed"), create("Neighbour")
	for _, typeKey := range []string{doomed, neighbour} {
		update(typeKey, "learnings v1")
		update(typeKey, "learnings v2")
		f.api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(httptest.NewRecorder(),
			f.req(http.MethodPost, "/api/task-manuals/"+typeKey, map[string]any{"sop_md": "sop v1"}), typeKey)
		update(typeKey, "learnings v3")
		f.api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(httptest.NewRecorder(),
			f.req(http.MethodPost, "/api/task-manuals/"+typeKey, map[string]any{"sop_md": "sop v2"}), typeKey)
	}
	for _, kind := range kinds {
		if len(agentList(t, f.api, kind, doomed)) == 0 {
			t.Fatalf("the fixture retained no %s version — the deletion assertion below would prove nothing", kind)
		}
	}

	rec := httptest.NewRecorder()
	f.api.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete(rec,
		f.req(http.MethodDelete, "/api/task-manuals/"+doomed, nil), doomed)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete manual: status=%d body=%s", rec.Code, rec.Body.String())
	}
	result := decodeBody[map[string]any](t, rec)
	if result["deleted"] != true || result["type_key"] != doomed {
		t.Errorf("delete result = %+v, want the manual reported deleted — the cascade changed the answer", result)
	}

	for _, kind := range kinds {
		if history := agentList(t, f.api, kind, doomed); len(history) != 0 {
			t.Errorf("the deleted manual still has %d readable %s versions: %+v", len(history), kind, history)
		}
		if len(agentList(t, f.api, kind, neighbour)) == 0 {
			t.Errorf("deleting one manual also erased another manual's %s history", kind)
		}
	}
}

// Every seam that overwrites a versioned document must retain what it replaced.
// A face wired back to the historyless Put* still answers 200 with the same
// body — the only difference is that the replaced version is unrecoverable, so
// nothing but this assertion distinguishes the two. The subtest names the face
// so one broken seam points at itself rather than at "document history".
func TestEveryDocumentWriteFaceRetainsTheVersionItReplaced(t *testing.T) {
	ownerReq := func(t *testing.T, method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	call := func(t *testing.T, name string, do func(*httptest.ResponseRecorder)) {
		t.Helper()
		rec := httptest.NewRecorder()
		do(rec)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	writeGlobal := func(t *testing.T, api *apiServer, text string) {
		t.Helper()
		call(t, "replace_global_context", func(rec *httptest.ResponseRecorder) {
			api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
				ownerReq(t, http.MethodPost, "/api/global-context", map[string]any{"text": text}))
		})
	}
	writeLessons := func(t *testing.T, api *apiServer, role, taskType, text string) {
		t.Helper()
		replaceLessonsThrough(t, api, role, taskType, text)
	}
	writeInsight := func(t *testing.T, api *apiServer, roleKey, text string) {
		t.Helper()
		call(t, "replace_insight", func(rec *httptest.ResponseRecorder) {
			api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
				ownerReq(t, http.MethodPost, "/api/insight/"+roleKey,
					map[string]any{"text": text}), roleKey)
		})
	}
	newManual := func(t *testing.T, api *apiServer) string {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleCreateTaskManualApiTaskManualsPost(rec,
			ownerReq(t, http.MethodPost, "/api/task-manuals", map[string]any{"display_name": "Faces"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("create manual: status=%d body=%s", rec.Code, rec.Body.String())
		}
		return decodeBody[taskManualDTO](t, rec).TypeKey
	}
	updateManual := func(t *testing.T, api *apiServer, typeKey string, body map[string]any) {
		t.Helper()
		call(t, "update_task_manual", func(rec *httptest.ResponseRecorder) {
			api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
				ownerReq(t, http.MethodPost, "/api/task-manuals/"+typeKey, body), typeKey)
		})
	}
	writeLearnings := func(t *testing.T, api *apiServer, typeKey, text string) {
		t.Helper()
		call(t, "write_task_learnings", func(rec *httptest.ResponseRecorder) {
			api.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
				ownerReq(t, http.MethodPost, "/api/task-manuals/"+typeKey+"/learnings",
					map[string]any{"text": text}), typeKey)
		})
	}

	const role, taskType = seedRoleAssistant, "tm-faces"

	// Each face seeds its document to a known state, performs the write under
	// test, and names the address plus the field/value the retained revision
	// must carry.
	for _, face := range []struct {
		name string
		run  func(*testing.T, *apiServer) (kind, key, field, want string)
	}{
		{
			name: "replace_global_context",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeGlobal(t, api, "one")
				writeGlobal(t, api, "two")
				return "global_context", "global", "text", "one"
			},
		},
		{
			name: "reset_global_context",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeGlobal(t, api, "one")
				writeGlobal(t, api, "two")
				call(t, "reset_global_context", func(rec *httptest.ResponseRecorder) {
					api.HandleResetGlobalContextApiGlobalContextResetPost(rec,
						ownerReq(t, http.MethodPost, "/api/global-context/reset", nil))
				})
				return "global_context", "global", "text", "two"
			},
		},
		{
			name: "update_role",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				updateRoleThrough(t, api, role, "first")
				updateRoleThrough(t, api, role, "second")
				return "role_definition", role, "definition_md", "first"
			},
		},
		{
			name: "reset_role",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				updateRoleThrough(t, api, role, "first")
				updateRoleThrough(t, api, role, "second")
				call(t, "reset_role", func(rec *httptest.ResponseRecorder) {
					api.HandleResetRoleApiRolesRoleResetPost(rec,
						ownerReq(t, http.MethodPost, "/api/roles/"+role+"/reset", nil), role)
				})
				return "role_definition", role, "definition_md", "second"
			},
		},
		// T-6501 added the three insight faces. They were MISSING from this
		// table, not deliberately left out: replace_insight and patch_insight
		// have retained versions since T-3809 and nothing here confronted them,
		// so a seam wired back to the historyless putInsightOn would have
		// answered the same 200 with the same body and gone unnoticed.
		{
			name: "replace_insight",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeInsight(t, api, role, "insight one")
				writeInsight(t, api, role, "insight two")
				return "insight", role, "text", "insight one"
			},
		},
		{
			name: "patch_insight",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeInsight(t, api, role, "insight one")
				writeInsight(t, api, role, "insight two")
				call(t, "patch_insight", func(rec *httptest.ResponseRecorder) {
					api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
						ownerReq(t, http.MethodPost, "/api/insight/"+role+"/patch",
							map[string]any{"edits": []map[string]any{{"old": "insight two", "new": "insight three"}}}),
						role)
				})
				return "insight", role, "text", "insight two"
			},
		},
		{
			// The counterpart of the reset_role face above. `want` is the
			// PRE-RESET overlay — deliberately NOT the seed the reset restores,
			// which is what a retain-the-wrong-thing bug would leave here.
			name: "reset_insight",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeInsight(t, api, role, "insight one")
				writeInsight(t, api, role, "insight two")
				call(t, "reset_insight", func(rec *httptest.ResponseRecorder) {
					api.HandleResetInsightApiInsightRoleKeyResetPost(rec,
						ownerReq(t, http.MethodPost, "/api/insight/"+role+"/reset", nil), role)
				})
				return "insight", role, "text", "insight two"
			},
		},
		{
			name: "replace_lessons",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeLessons(t, api, role, taskType, "lesson one")
				writeLessons(t, api, role, taskType, "lesson two")
				return "lessons", role + "::" + taskType, "text", "lesson one"
			},
		},
		{
			name: "patch_lessons",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				writeLessons(t, api, role, taskType, "lesson one")
				writeLessons(t, api, role, taskType, "lesson two")
				call(t, "patch_lessons", func(rec *httptest.ResponseRecorder) {
					api.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost(rec,
						ownerReq(t, http.MethodPost, "/api/lessons/"+role+"/"+taskType+"/patch",
							map[string]any{"edits": []map[string]any{{"old": "lesson two", "new": "lesson three"}}}),
						role, taskType)
				})
				return "lessons", role + "::" + taskType, "text", "lesson two"
			},
		},
		{
			name: "update_task_manual",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				typeKey := newManual(t, api)
				updateManual(t, api, typeKey, map[string]any{"sop_md": "sop v1"})
				updateManual(t, api, typeKey, map[string]any{"sop_md": "sop v2"})
				return docKindTaskManualSop, typeKey, "sop_md", "sop v1"
			},
		},
		{
			name: "write_task_learnings",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				typeKey := newManual(t, api)
				writeLearnings(t, api, typeKey, "learnings one")
				writeLearnings(t, api, typeKey, "learnings two")
				return docKindTaskManualLearnings, typeKey, "learnings", "learnings one"
			},
		},
		{
			name: "patch_task_learnings",
			run: func(t *testing.T, api *apiServer) (string, string, string, string) {
				typeKey := newManual(t, api)
				writeLearnings(t, api, typeKey, "learnings one")
				call(t, "patch_task_learnings", func(rec *httptest.ResponseRecorder) {
					api.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(rec,
						ownerReq(t, http.MethodPost, "/api/task-manuals/"+typeKey+"/learnings/patch",
							map[string]any{"edits": []map[string]any{{"old": "learnings one", "new": "learnings two"}}}),
						typeKey)
				})
				return docKindTaskManualLearnings, typeKey, "learnings", "learnings one"
			},
		},
	} {
		t.Run(face.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			kind, key, field, want := face.run(t, api)
			history := agentList(t, api, kind, key)
			if len(history) == 0 {
				t.Fatalf("%s retained nothing — the version it replaced (%s = %q) is unrecoverable",
					face.name, field, want)
			}
			if got := history[0].Content[field]; got != want {
				t.Fatalf("%s retained %s = %q, want %q — the newest revision is not the version this write replaced",
					face.name, field, got, want)
			}
		})
	}
}
