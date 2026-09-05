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
	"strings"
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

// getTaskArtifacts reads one task's artifacts IN FULL through the T-66 read
// face (GET /api/tasks/{task_id}/artifacts). The full row stopped riding
// get_task on owner c-cd063427fb2f, so a metadata assertion belongs here.
func getTaskArtifacts(t *testing.T, api *apiServer, taskID string) taskArtifactListDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListTaskArtifactsApiTasksTaskIdArtifactsGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID+"/artifacts", nil, "owner", "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("list task artifacts: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskArtifactListDTO](t, rec)
}

// getTaskView reads the full task view. It carries NO artifact rows at all
// since T-92 — only artifact_count — so an assertion about the SET reads
// view.ArtifactCount, and an assertion about a ROW belongs on getTaskArtifacts.
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
	var add, rm, replace, history *RouteSpec
	for i := range defaultRouteSpecs() {
		spec := defaultRouteSpecs()[i]
		switch spec.Path {
		case "/api/tasks/{task_id}/artifact":
			s := spec
			add = &s
		case "/api/tasks/{task_id}/artifact/{artifact_id}":
			s := spec
			rm = &s
		case "/api/tasks/{task_id}/artifact/{artifact_id}/replace":
			s := spec
			replace = &s
		case "/api/tasks/{task_id}/artifact/{artifact_id}/history":
			s := spec
			history = &s
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
	// T-60 replace — the third verb, same model, same MCP surface.
	if replace == nil || replace.Method != "POST" || replace.Requires != principalAgent ||
		replace.MCPExclude || replace.MCPTool != "replace_task_artifact" {
		t.Fatalf("replace row must be POST + agent + MCP replace_task_artifact: %+v", replace)
	}
	// The version list is cockpit-only by decision: same floor, off MCP.
	if history == nil || history.Method != "GET" || history.Requires != principalAgent ||
		!history.MCPExclude || history.MCPTool != "" {
		t.Fatalf("history row must be GET + agent + MCPExclude: %+v", history)
	}
}

func TestAddLinkArtifact(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://github.com/x/y/pull/123",
			"name": "PR #123"}, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("add link: %d %s", rec.Code, rec.Body.String())
	}
	// The write answers with a bounded receipt (T-a98d), not the whole task.
	receipt := decodeBody[taskArtifactReceiptDTO](t, rec)
	if receipt.TaskID != task.ID || receipt.ArtifactID == "" || receipt.ArtifactCount != 1 {
		t.Fatalf("add receipt wrong shape: %+v", receipt)
	}
	view := getTaskView(t, api, task.ID)
	if view.ArtifactCount != 1 {
		t.Fatalf("expected artifact_count 1, got %d", view.ArtifactCount)
	}
	// The ROWS — and since T-92 the ids too — live on the T-66 artifacts read,
	// not on the task view, so that is where the receipt is checked against the
	// artifact it claims to have pinned.
	full := getTaskArtifacts(t, api, task.ID)
	if len(full.Artifacts) != 1 {
		t.Fatalf("expected 1 full artifact, got %+v", full.Artifacts)
	}
	a := full.Artifacts[0]
	if a.ID != receipt.ArtifactID {
		t.Fatalf("receipt must name the artifact it pinned: %q vs %q",
			receipt.ArtifactID, a.ID)
	}
	// A link's url is read back out of the text/uri-list blob T-92 mints for it,
	// so the caller sees the address it sent and never the blob behind it.
	if a.Kind != "link" || a.URL != "https://github.com/x/y/pull/123" ||
		a.Name != "PR #123" || a.Description != "" {
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
		map[string]any{"kind": "image", "attachment_id": "att-img1", "name": "架構圖"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add image: %d %s", rec.Code, rec.Body.String())
	}
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "file", "attachment_id": "att-file1", "name": "design"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add file: %d %s", rec.Code, rec.Body.String())
	}
	full := getTaskArtifacts(t, api, task.ID)
	if len(full.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %+v", full.Artifacts)
	}
	img, file := full.Artifacts[0], full.Artifacts[1]
	// T-92 narrowed the row: is_image went because it is mime's prefix and
	// filename went because the NAME derives from it. What is left of the blob
	// resolve is the serve path and the mime — the one field that separates a
	// .md from a .pdf from a .zip, which kind cannot do — so those are what the
	// resolve is still asserted through.
	if img.Kind != "image" || img.Mime != "image/png" ||
		img.URL != "/api/chat/attachment/att-img1" || img.Name != "架構圖" {
		t.Fatalf("image artifact metadata wrong: %+v", img)
	}
	if file.Kind != "file" || file.Mime != "text/markdown" ||
		file.URL != "/api/chat/attachment/att-file1" || file.Name != "design" {
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
		{"bad kind", map[string]any{"kind": "video", "url": "x", "name": "n"}, http.StatusBadRequest},
		{"link no url", map[string]any{"kind": "link", "name": "n"}, http.StatusBadRequest},
		{"link blank url", map[string]any{"kind": "link", "url": "  ", "name": "n"}, http.StatusBadRequest},
		{"file no attachment", map[string]any{"kind": "file", "name": "n"}, http.StatusBadRequest},
		{"file dangling attachment", map[string]any{"kind": "file", "attachment_id": "att-nope", "name": "n"}, http.StatusBadRequest},
		// T-92, owner rc-85b07ab98651「現在開始任務產物都需要有個名字」: name is
		// REQUIRED on this door, and both caps REFUSE rather than truncate — a
		// silently shortened name is a deliverable that no longer says what its
		// author said it was. Each case is otherwise a perfectly good request,
		// so the 400 can only be about the text.
		{"no name", map[string]any{"kind": "link", "url": "https://x/pr/1"}, http.StatusBadRequest},
		{"blank name", map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "   "}, http.StatusBadRequest},
		{"name over the cap", map[string]any{"kind": "link", "url": "https://x/pr/1",
			"name": strings.Repeat("界", artifactNameMaxChars+1)}, http.StatusBadRequest},
		{"description over the cap", map[string]any{"kind": "link", "url": "https://x/pr/1",
			"name": "n", "description": strings.Repeat("界", artifactDescriptionMaxChars+1)}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := addArtifact(t, api, task.ID, tc.body, "m-exec", "agent"); rec.Code != tc.want {
				t.Fatalf("%s: want %d got %d (%s)", tc.name, tc.want, rec.Code, rec.Body.String())
			}
		})
	}
	// None of the rejected attempts persisted.
	if got := getTaskView(t, api, task.ID); got.ArtifactCount != 0 {
		t.Fatalf("rejected attempts must not persist, got artifact_count %d", got.ArtifactCount)
	}
}

func TestAddArtifactExecutorGuard(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	link := map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"}
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
		map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"},
		"m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task add must 409, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestRemoveArtifactOnTerminalTaskIs409 is the symmetric twin of
// TestAddArtifactOnTerminalTaskIs409 (owner ruling 2026-07-25, T-2654): a closed
// task's deliverable set is frozen in EVERY direction. The add-only freeze made
// un-pin an unrecoverable loss — a deliverable could be taken off a closed card
// and never put back. The open-task remove below is the positive control, so a
// mutant that freezes un-pin unconditionally reddens too.
func TestRemoveArtifactOnTerminalTaskIs409(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	link := map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"}

	// Positive control: while the task is still open, un-pin works as before.
	openArt := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).ArtifactID
	if rec := removeArtifact(t, api, task.ID, openArt, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("open task remove must stay 200, got %d %s", rec.Code, rec.Body.String())
	}

	// Pin one more, then close the task.
	artID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).ArtifactID
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}

	// The executor is refused — same 409 shape as add.
	if rec := removeArtifact(t, api, task.ID, artID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task remove must 409, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner (admin capability) is not exempt either — the freeze is a task
	// state rule, not a permission rule.
	if rec := removeArtifact(t, api, task.ID, artID, "owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task remove by owner must 409, got %d %s", rec.Code, rec.Body.String())
	}
	// The rejected attempts must leave the deliverable pinned.
	if got := getTaskView(t, api, task.ID); got.ArtifactCount != 1 {
		t.Fatalf("frozen artifact must survive, got artifact_count %d", got.ArtifactCount)
	}
}

func TestRemoveArtifact(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"}, "m-exec", "agent")
	artID := decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID

	// Unknown artifact → 404; wrong-task ownership → 400.
	if rec := removeArtifact(t, api, task.ID, "ta-nope", "owner", "owner"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown artifact must 404, got %d", rec.Code)
	}
	other := createAdHocTask(t, api, "m-exec")
	if rec := removeArtifact(t, api, other.ID, artID, "owner", "owner"); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong-task remove must 400, got %d %s", rec.Code, rec.Body.String())
	}
	// The real un-pin removes the row and answers with a bounded receipt.
	rmRec := removeArtifact(t, api, task.ID, artID, "owner", "owner")
	if rmRec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rmRec.Code, rmRec.Body.String())
	}
	receipt := decodeBody[taskArtifactReceiptDTO](t, rmRec)
	if receipt.TaskID != task.ID || receipt.ArtifactID != artID || receipt.ArtifactCount != 0 {
		t.Fatalf("remove receipt wrong shape: %+v", receipt)
	}
	if got := getTaskView(t, api, task.ID); got.ArtifactCount != 0 {
		t.Fatalf("artifact must be gone, got artifact_count %d", got.ArtifactCount)
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
	link := map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"}

	// A different agent (not the executor, no admin capability) is a flat 403 —
	// and the artifact must survive the rejected attempt.
	artID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).ArtifactID
	if rec := removeArtifact(t, api, task.ID, artID, "m-other", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-executor agent must 403, got %d %s", rec.Code, rec.Body.String())
	}
	if got := getTaskView(t, api, task.ID); got.ArtifactCount != 1 {
		t.Fatalf("rejected remove must not un-pin, got artifact_count %d", got.ArtifactCount)
	}
	// The executing agent removes its own deliverable.
	if rec := removeArtifact(t, api, task.ID, artID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("executor agent must remove, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner (admin capability) removes on any task.
	artID2 := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID, link, "m-exec", "agent")).ArtifactID
	if rec := removeArtifact(t, api, task.ID, artID2, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner must remove, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestArtifactCountEmptyThenPopulated is the 「0 個產物 → 標籤不出現」 backing
// assertion. The empty task must report count 0 on the light list AND on the
// full view — which since T-92 is the ONLY thing either of them says about the
// artifact set; the positive control (add one → count 1) proves the count is
// actually wired (guards a hard-coded-0 or hard-coded-N mutant).
func TestArtifactCountEmptyThenPopulated(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	if it := listItemFor(t, api, task.ID); it.ArtifactCount != 0 {
		t.Fatalf("empty task must report artifact_count 0, got %d", it.ArtifactCount)
	}
	if view := getTaskView(t, api, task.ID); view.ArtifactCount != 0 {
		t.Fatalf("empty task full view must report artifact_count 0, got %d", view.ArtifactCount)
	}

	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1", "name": "PR #1"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	if it := listItemFor(t, api, task.ID); it.ArtifactCount != 1 {
		t.Fatalf("after one add, artifact_count must be 1, got %d", it.ArtifactCount)
	}
}
