package main

// T-66 — the ARTIFACT split, the twin of the step-note split in
// api_tasks_step_read_t66_test.go: the shared task projection now carries an
// INDEX of the pinned deliverables (id + label), and list_task_artifacts is the
// one read that serves the rows in full.
//
// Owner c-cd063427fb2f, verbatim:「我覺得任務產物，只需要預設給標題跟ID, 有需要
// 再透過另一隻去拿就好了」. And on the SHAPE of 「另一隻」, c-f2d0fecb1168,
// verbatim:「應該是指名任務？」— one call per TASK, not per artifact.
//
// WHAT THESE PIN, and why each needs its own case:
//
//   - The fat fields are GONE FROM THE WIRE, not blanked. A decoded struct
//     cannot tell those apart (a Go type that no longer declares URL decodes a
//     url-bearing payload just as happily), so the removal is asserted against
//     the RAW JSON of a task that really does have an artifact, key by key.
//   - The response SAYS its artifacts are abridged. The AC is verbatim「成功的
//     回應不得看起來像完整的 task」, and「省略要自己說出來」— a 200 whose
//     artifact rows look complete is the defect, whether or not the caller
//     knows which fields a full row used to carry.
//   - It rides EVERY exit of the shared builder. The slimming is done in
//     newTaskDTO, so a write face that has no opinion about artifacts must be
//     just as thin — that is the whole reason for doing it there.
//   - The full rows are REACHABLE, in one call, for the whole ticket, and that
//     call carries the task read's floor and nothing stricter.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// t66ArtifactURL is distinctive enough that a substring search over a whole
// response body cannot match it by accident — so "did the fat field leak"
// is answerable without knowing which key it might have leaked under.
const t66ArtifactURL = "https://example.com/t66/only-on-the-full-row"

// t66ArtifactLabel is multi-byte on purpose: the label is one of the two fields
// the index DOES carry, so it has to survive the projection intact.
const t66ArtifactLabel = "設計稿 v3"

// t66ArtifactFixture pins one link artifact on a fresh task and returns the
// task id and the artifact id.
func t66ArtifactFixture(t *testing.T, api *apiServer, executor string) (string, string) {
	t.Helper()
	task := createAdHocTask(t, api, executor)
	rec := addArtifact(t, api, task.ID, map[string]any{
		"kind": "link", "url": t66ArtifactURL, "label": t66ArtifactLabel,
	}, executor, "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("seed artifact: %d %s", rec.Code, rec.Body.String())
	}
	return task.ID, decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID
}

// listArtifactsRaw calls the T-66 read and returns the recorder, so each case
// asserts its own status and can look at the raw bytes.
func listArtifactsRaw(t *testing.T, api *apiServer, taskID, caller, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListTaskArtifactsApiTasksTaskIdArtifactsGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID+"/artifacts", nil, caller, scope), taskID)
	return rec
}

// t66IndexOnlyKeys is what an index row is allowed to carry. It is spelled out
// rather than derived from the DTO so that ADDING a field back to
// taskArtifactRefDTO reddens here instead of silently widening the contract.
var t66IndexOnlyKeys = map[string]bool{"id": true, "label": true}

// TestGetTaskArtifactsAreAnIndexAndTheResponseSaysSo is the AC for the artifact
// half:「成功的回應不得看起來像完整的 task」. It reads the RAW body, because the
// point is what is on the WIRE.
func TestGetTaskArtifactsAreAnIndexAndTheResponseSaysSo(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, artID := t66ArtifactFixture(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// 1. The url is not on the wire at all — not under `url`, not under any
	//    other key someone might smuggle it through.
	if strings.Contains(body, t66ArtifactURL) {
		t.Fatalf("get_task carried the artifact URL; owner c-cd063427fb2f says the default "+
			"payload is 「只需要預設給標題跟ID」: %s", body)
	}

	var raw struct {
		ArtifactsDetailLevel *string          `json:"artifacts_detail_level"`
		Artifacts            []map[string]any `json:"artifacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 2. The LIST is complete — the abridgement is per-row, never a cut set.
	if len(raw.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact row, got %d (%v)", len(raw.Artifacts), raw.Artifacts)
	}
	row := raw.Artifacts[0]
	// 3. Declared-but-empty is NOT the ask, exactly as with the step note: an
	//    always-"" url reads to every existing client as "this artifact has no
	//    url". The keys have to be gone from the schema.
	for k := range row {
		if !t66IndexOnlyKeys[k] {
			t.Fatalf("artifact index row still declares %q on the wire — the index is id + label "+
				"and nothing else (owner c-cd063427fb2f); a declared always-empty field is read "+
				"by every existing client as 'this artifact has none': %v", k, row)
		}
	}
	// 4. And it really is the INDEX of the artifact that exists, not an empty
	//    husk: both surviving fields carry their true values.
	if row["id"] != artID || row["label"] != t66ArtifactLabel {
		t.Fatalf("index row must carry the REAL id and label, got %v (want id=%q label=%q)",
			row, artID, t66ArtifactLabel)
	}
	// 5. The response says what its artifact rows are, rather than leaving the
	//    caller to infer it from which keys happen to be missing.
	if raw.ArtifactsDetailLevel == nil {
		t.Fatalf("get_task must declare artifacts_detail_level — 省略要自己說出來")
	}
	if *raw.ArtifactsDetailLevel != "index" {
		t.Fatalf("get_task must declare artifacts_detail_level=index, got %q — 省略要自己說出來",
			*raw.ArtifactsDetailLevel)
	}
}

// TestTaskArtifactIndexRidesEveryExitOfTheSharedBuilder: the slimming is done in
// newTaskDTO so that nine responses get thinner at once (EXECUTOR JUDGEMENT —
// the owner ruled the payload, not the layer). A per-handler fix would leave
// the other eight serving the fat rows, so one of the eight is checked here.
func TestTaskArtifactIndexRidesEveryExitOfTheSharedBuilder(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, _ := t66ArtifactFixture(t, api, "m-exec")

	// set_task_deps stands for the other eight: a WRITE face that answers with
	// the whole task and has no reason of its own to know anything about
	// artifacts — which is exactly why it would still be carrying them if the
	// slimming had been done in the get_task handler.
	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/deps",
			map[string]any{"blocked_by": []string{}}, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("set_task_deps: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), t66ArtifactURL) {
		t.Fatalf("set_task_deps still carries the artifact URL: %s", rec.Body.String())
	}
	var raw struct {
		ArtifactsDetailLevel string           `json:"artifacts_detail_level"`
		Artifacts            []map[string]any `json:"artifacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.ArtifactsDetailLevel != "index" {
		t.Fatalf("set_task_deps's task payload must declare artifacts_detail_level=index too, "+
			"got %q — the slimming lives in newTaskDTO precisely so all nine exits say the "+
			"same thing", raw.ArtifactsDetailLevel)
	}
	if len(raw.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact row, got %v", raw.Artifacts)
	}
	for k := range raw.Artifacts[0] {
		if !t66IndexOnlyKeys[k] {
			t.Fatalf("set_task_deps still serves the FULL artifact row (key %q): %v",
				k, raw.Artifacts[0])
		}
	}
}

// TestListTaskArtifactsServesTheWholeRowAndSaysSo is the other half: everything
// the index dropped has to be reachable, in full, through the read that
// replaces it — and that read declares itself.
func TestListTaskArtifactsServesTheWholeRowAndSaysSo(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, artID := t66ArtifactFixture(t, api, "m-exec")

	rec := listArtifactsRaw(t, api, taskID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("list_task_artifacts: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[taskArtifactListDTO](t, rec)
	if got.TaskID != taskID {
		t.Fatalf("answered task %q, asked for %q", got.TaskID, taskID)
	}
	if got.ArtifactsDetailLevel != "full" {
		t.Fatalf("artifacts_detail_level = %q, want full — it is what lets a caller tell this "+
			"response apart from the task view's index", got.ArtifactsDetailLevel)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %+v", got.Artifacts)
	}
	a := got.Artifacts[0]
	if a.ID != artID || a.Kind != "link" || a.URL != t66ArtifactURL ||
		a.Label != t66ArtifactLabel || a.CreatedBy != "m-exec" || a.CreatedTS <= 0 {
		t.Fatalf("full artifact row is not full: %+v", a)
	}
}

// TestListTaskArtifactsAnswersTheWholeTicketInOneCall pins the SHAPE the owner
// chose (c-f2d0fecb1168:「應該是指名任務？」) rather than the per-artifact door
// that would have mirrored get_task_step: three pinned deliverables come back
// from ONE call, in pin order.
func TestListTaskArtifactsAnswersTheWholeTicketInOneCall(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	want := []string{}
	for _, n := range []string{"one", "two", "three"} {
		rec := addArtifact(t, api, task.ID, map[string]any{
			"kind": "link", "url": t66ArtifactURL + "/" + n, "label": n,
		}, "m-exec", "agent")
		if rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", n, rec.Code, rec.Body.String())
		}
		want = append(want, decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID)
	}
	got := decodeBody[taskArtifactListDTO](t, listArtifactsRaw(t, api, task.ID, "m-exec", "agent"))
	if len(got.Artifacts) != len(want) {
		t.Fatalf("one call must answer the WHOLE ticket (owner c-f2d0fecb1168), got %d of %d",
			len(got.Artifacts), len(want))
	}
	for i, id := range want {
		if got.Artifacts[i].ID != id {
			t.Fatalf("artifact %d = %q, want %q (oldest→newest)", i, got.Artifacts[i].ID, id)
		}
	}
}

// TestListTaskArtifactsEmptyAndUnknown: nothing pinned is an EMPTY SET, never a
// 404 — the two mean different things and a client that cannot tell them apart
// cannot tell "no deliverables" from "no such ticket". An unknown task IS a 404,
// the same one the task view answers.
func TestListTaskArtifactsEmptyAndUnknown(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	rec := listArtifactsRaw(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty set must be 200: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"artifacts":[]`) {
		t.Fatalf("empty set must serialise as [], not null: %s", rec.Body.String())
	}

	if rec := listArtifactsRaw(t, api, "t-nope", "m-exec", "agent"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task = %d, want 404", rec.Code)
	}
}

// TestListTaskArtifactsCarriesTheTaskReadFloor: this endpoint moved NO field to
// a stricter or looser door. Every field it serves rode get_task until this
// ticket, and get_task has no executor gate — so a second agent, who executes
// nothing, reads it exactly as it already could through the task view.
func TestListTaskArtifactsCarriesTheTaskReadFloor(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, _ := t66ArtifactFixture(t, api, "m-exec")

	rec := listArtifactsRaw(t, api, taskID, "m-other", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("a non-executor agent must read artifacts exactly as it already could through "+
			"get_task (no executor gate on a READ): %d %s", rec.Code, rec.Body.String())
	}

	var row *RouteSpec
	for i := range defaultRouteSpecs() {
		if spec := defaultRouteSpecs()[i]; spec.Path == "/api/tasks/{task_id}/artifacts" {
			s := spec
			row = &s
		}
	}
	if row == nil {
		t.Fatalf("no route row for /api/tasks/{task_id}/artifacts")
	}
	if row.Method != "GET" || row.Requires != principalMachine || row.MCPExclude ||
		row.MCPTool != "list_task_artifacts" {
		t.Fatalf("the artifacts read must be GET + machine floor + MCP list_task_artifacts "+
			"(the same floor GET /api/tasks/{task_id} carries): %+v", row)
	}
}
