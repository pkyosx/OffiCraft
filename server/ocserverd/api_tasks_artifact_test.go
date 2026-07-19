package main

// api_tasks_artifact_test.go — the T-3dc5 task artifact set: registration of
// the three kinds (link / file / image), the file/image blob-metadata resolve,
// the input guards, the executor guard on add, the owner/admin un-pin, and the
// light-list count. The empty-artifact assertion (0 → count 0, badge hidden)
// carries a positive control (add one → count 1) so a mutant that hard-codes
// the count in either direction reddens.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// addArtifact posts add_task_artifact as (sub, scope).
func addArtifact(t *testing.T, api *apiServer, taskID string, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleAddTaskArtifactApiTasksTaskIdArtifactPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/artifact", body, sub, scope),
		taskID)
	return rec
}

// removeArtifact deletes one artifact as (sub, scope).
func removeArtifact(t *testing.T, api *apiServer, taskID, artID, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(rec,
		taskReq(t, "DELETE", "/api/tasks/"+taskID+"/artifact/"+artID, nil, sub, scope),
		taskID, artID)
	return rec
}

// getTaskView reads the full task view (folds the artifact set).
func getTaskView(t *testing.T, api *apiServer, taskID string) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "owner", "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec)
}

// listItemFor reads the light list and returns the item for taskID.
func listItemFor(t *testing.T, api *apiServer, taskID string) taskListItemDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListTasksApiTasksGet(rec,
		taskReq(t, "GET", "/api/tasks", nil, "owner", "owner"),
		HandleListTasksApiTasksGetParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d %s", rec.Code, rec.Body.String())
	}
	for _, it := range decodeBody[[]taskListItemDTO](t, rec) {
		if it.ID == taskID {
			return it
		}
	}
	t.Fatalf("task %s not in list", taskID)
	return taskListItemDTO{}
}

func TestArtifactRouteRowsGatingAndMCP(t *testing.T) {
	var add, rm *RouteSpec
	for i := range defaultRouteSpecs() {
		spec := defaultRouteSpecs()[i]
		switch spec.Path {
		case "/api/tasks/{task_id}/artifact":
			s := spec
			add = &s
		case "/api/tasks/{task_id}/artifact/{artifact_id}":
			s := spec
			rm = &s
		}
	}
	if add == nil || add.Method != "POST" || add.Requires != principalAgent ||
		add.MCPExclude || add.MCPTool != "add_task_artifact" {
		t.Fatalf("add row must be POST + agent + MCP add_task_artifact: %+v", add)
	}
	// Owner ruling 2026-07-18: remove shares add's model — agent + MCP tool.
	if rm == nil || rm.Method != "DELETE" || rm.Requires != principalAgent ||
		rm.MCPExclude || rm.MCPTool != "remove_task_artifact" {
		t.Fatalf("remove row must be DELETE + agent + MCP remove_task_artifact: %+v", rm)
	}
}

func TestAddLinkArtifact(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://github.com/x/y/pull/123",
			"label": "PR #123"}, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("add link: %d %s", rec.Code, rec.Body.String())
	}
	view := decodeBody[taskDTO](t, rec)
	if len(view.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %+v", view.Artifacts)
	}
	a := view.Artifacts[0]
	if a.Kind != "link" || a.URL != "https://github.com/x/y/pull/123" ||
		a.Label != "PR #123" || a.AttachmentID != "" || a.IsImage {
		t.Fatalf("link artifact wrong shape: %+v", a)
	}
	if a.CreatedBy != "m-exec" {
		t.Fatalf("created_by must be the verified sub, got %q", a.CreatedBy)
	}
}

func TestAddFileAndImageArtifactResolveBlobMetadata(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	pngName := "diagram.png"
	if err := api.dal.PutChatAttachment(ChatAttachment{
		ID: "att-img1", Mime: "image/png", Data: []byte("PNG"), Filename: &pngName,
	}); err != nil {
		t.Fatalf("seed image blob: %v", err)
	}
	mdName := "design.md"
	if err := api.dal.PutChatAttachment(ChatAttachment{
		ID: "att-file1", Mime: "text/markdown", Data: []byte("# hi"), Filename: &mdName,
	}); err != nil {
		t.Fatalf("seed file blob: %v", err)
	}
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "image", "attachment_id": "att-img1"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add image: %d %s", rec.Code, rec.Body.String())
	}
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "file", "attachment_id": "att-file1"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add file: %d %s", rec.Code, rec.Body.String())
	}
	view := getTaskView(t, api, task.ID)
	if len(view.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %+v", view.Artifacts)
	}
	img, file := view.Artifacts[0], view.Artifacts[1]
	if img.Kind != "image" || !img.IsImage || img.Mime != "image/png" ||
		img.Filename != "diagram.png" || img.URL != "/api/chat/attachment/att-img1" {
		t.Fatalf("image artifact metadata wrong: %+v", img)
	}
	if file.Kind != "file" || file.IsImage || file.Mime != "text/markdown" ||
		file.Filename != "design.md" || file.URL != "/api/chat/attachment/att-file1" {
		t.Fatalf("file artifact metadata wrong: %+v", file)
	}
}

func TestAddArtifactInputGuards(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"bad kind", map[string]any{"kind": "video", "url": "x"}, http.StatusBadRequest},
		{"link no url", map[string]any{"kind": "link"}, http.StatusBadRequest},
		{"link blank url", map[string]any{"kind": "link", "url": "  "}, http.StatusBadRequest},
		{"file no attachment", map[string]any{"kind": "file"}, http.StatusBadRequest},
		{"file dangling attachment", map[string]any{"kind": "file", "attachment_id": "att-nope"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := addArtifact(t, api, task.ID, tc.body, "m-exec", "agent"); rec.Code != tc.want {
				t.Fatalf("%s: want %d got %d (%s)", tc.name, tc.want, rec.Code, rec.Body.String())
			}
		})
	}
	// None of the rejected attempts persisted.
	if got := getTaskView(t, api, task.ID); len(got.Artifacts) != 0 {
		t.Fatalf("rejected attempts must not persist, got %+v", got.Artifacts)
	}
}

func TestAddArtifactExecutorGuard(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	link := map[string]any{"kind": "link", "url": "https://x/pr/1"}
	// A different agent (not the executor, no admin capability) is a flat 403.
	if rec := addArtifact(t, api, task.ID, link, "m-other", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-executor agent must 403, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner (admin capability) may pin on any task.
	if rec := addArtifact(t, api, task.ID, link, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner must pin, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAddArtifactOnTerminalTaskIs409(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task add must 409, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveArtifact(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"}, "m-exec", "agent")
	artID := decodeBody[taskDTO](t, rec).Artifacts[0].ID

	// Unknown artifact → 404; wrong-task ownership → 400.
	if rec := removeArtifact(t, api, task.ID, "ta-nope", "owner", "owner"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown artifact must 404, got %d", rec.Code)
	}
	other := createAdHocTask(t, api, "m-exec")
	if rec := removeArtifact(t, api, other.ID, artID, "owner", "owner"); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong-task remove must 400, got %d %s", rec.Code, rec.Body.String())
	}
	// The real un-pin removes the row.
	if rec := removeArtifact(t, api, task.ID, artID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if got := getTaskView(t, api, task.ID); len(got.Artifacts) != 0 {
		t.Fatalf("artifact must be gone, got %+v", got.Artifacts)
	}
}

// TestRemoveArtifactExecutorGuard is the mutant-guarding twin of
// TestAddArtifactExecutorGuard for the owner ruling 2026-07-18: un-pin now
// shares add's model, so a non-executor agent is a flat 403 (before any
// artifact lookup — it cannot probe artifact existence), the executing agent
// removes its own deliverable, and the owner (admin capability) removes on any
// task. Dropping the handler's callerMayDriveTask guard reddens the 403 case.
func TestRemoveArtifactExecutorGuard(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	link := map[string]any{"kind": "link", "url": "https://x/pr/1"}

	// A different agent (not the executor, no admin capability) is a flat 403 —
	// and the artifact must survive the rejected attempt.
	artID := decodeBody[taskDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).Artifacts[0].ID
	if rec := removeArtifact(t, api, task.ID, artID, "m-other", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-executor agent must 403, got %d %s", rec.Code, rec.Body.String())
	}
	if got := getTaskView(t, api, task.ID); len(got.Artifacts) != 1 {
		t.Fatalf("rejected remove must not un-pin, got %+v", got.Artifacts)
	}
	// The executing agent removes its own deliverable.
	if rec := removeArtifact(t, api, task.ID, artID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("executor agent must remove, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner (admin capability) removes on any task.
	artID2 := decodeBody[taskDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).Artifacts[0].ID
	if rec := removeArtifact(t, api, task.ID, artID2, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner must remove, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestArtifactCountEmptyThenPopulated is the 「0 個產物 → 標籤不出現」 backing
// assertion. The empty task must report count 0 on the light list AND an empty
// artifacts array on the full view; the positive control (add one → count 1)
// proves the count is actually wired (guards a hard-coded-0 or hard-coded-N
// mutant).
func TestArtifactCountEmptyThenPopulated(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	if it := listItemFor(t, api, task.ID); it.ArtifactCount != 0 {
		t.Fatalf("empty task must report artifact_count 0, got %d", it.ArtifactCount)
	}
	if view := getTaskView(t, api, task.ID); len(view.Artifacts) != 0 {
		t.Fatalf("empty task full view must have [] artifacts, got %+v", view.Artifacts)
	}

	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	if it := listItemFor(t, api, task.ID); it.ArtifactCount != 1 {
		t.Fatalf("after one add, artifact_count must be 1, got %d", it.ArtifactCount)
	}
}
