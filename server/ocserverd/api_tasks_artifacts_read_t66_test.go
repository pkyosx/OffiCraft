package main

// T-66 — the ARTIFACT split, the twin of the step-note split in
// api_tasks_step_read_t66_test.go: the shared task projection stopped carrying
// the pinned deliverables, and list_task_artifacts is the one read that serves
// the rows in full.
//
// Owner c-cd063427fb2f, verbatim:「我覺得任務產物，只需要預設給標題跟ID, 有需要
// 再透過另一隻去拿就好了」. And on the SHAPE of 「另一隻」, c-f2d0fecb1168,
// verbatim:「應該是指名任務？」— one call per TASK, not per artifact.
//
// ⚠️ T-92 TOOK THE REMAINING HALF. The id+label index this file used to pin is
// GONE: a task response now carries `artifact_count` and no rows at all, and
// there is no `artifacts` key and no `artifacts_detail_level` on it either. The
// reason is in the count's own doc — the id list is a SET that grows with the
// age of the ticket and never shrinks, so leaving it there would have
// reintroduced, one migration later, the unbounded per-read cost the split
// exists to remove; and a caller holding an id is a caller about to act on that
// artifact, which needs the whole row anyway. The cases below therefore assert
// the COUNT and the ABSENCE of the rows, which is what the split now means.
//
// WHAT THESE PIN, and why each needs its own case:
//
//   - The dropped fields are GONE FROM THE WIRE, not blanked. A decoded struct
//     cannot tell those apart (a Go type that no longer declares URL decodes a
//     url-bearing payload just as happily), so the removal is asserted against
//     the RAW JSON of a task that really does have an artifact, key by key.
//   - The response SAYS how much of the set it is giving. The AC is verbatim
//    「成功的回應不得看起來像完整的 task」, and「省略要自己說出來」— since T-92
//     the count IS that statement: an EXACT, un-capped number that tells the
//     caller how many rows list_task_artifacts is holding for it.
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

// t66ArtifactName is multi-byte on purpose: the name is what the full read has
// to carry back intact, and a rune-counted cap makes a byte-counted mistake here
// easy to make.
const t66ArtifactName = "設計稿 v3"

// t66ArtifactFixture pins one link artifact on a fresh task and returns the
// task id and the artifact id.
func t66ArtifactFixture(t *testing.T, api *apiServer, executor string) (string, string) {
	t.Helper()
	task := createAdHocTask(t, api, executor)
	rec := addArtifact(t, api, task.ID, map[string]any{
		"kind": "link", "url": t66ArtifactURL, "name": t66ArtifactName,
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

// TestGetTaskArtifactsAreACountAndTheResponseSaysSo is the AC for the artifact
// half:「成功的回應不得看起來像完整的 task」. It reads the RAW body, because the
// point is what is on the WIRE.
func TestGetTaskArtifactsAreACountAndTheResponseSaysSo(t *testing.T) {
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

	// The artifact id is not on the wire either — it is what list_task_artifacts
	// is FOR (T-92), and a client that could scrape ids off the task view would
	// keep paying for a list that only ever grows.
	if strings.Contains(body, artID) {
		t.Fatalf("get_task carried the artifact id; since T-92 the ids come from "+
			"list_task_artifacts and the task view answers a count: %s", body)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 2. Declared-but-empty is NOT the ask, exactly as with the step note: an
	//    always-[] artifacts array reads to every existing client as "this task
	//    has no deliverables". The keys have to be gone from the schema — and
	//    that goes for the detail level too, which described rows that are no
	//    longer there.
	for _, gone := range []string{"artifacts", "artifacts_detail_level"} {
		if _, ok := raw[gone]; ok {
			t.Fatalf("get_task still declares %q on the wire — since T-92 the task view "+
				"carries a COUNT and no rows; a declared always-empty field is read by "+
				"every existing client as 'this task has none': %s", gone, body)
		}
	}
	// 3. The count is what the response says INSTEAD, and it is the real one:
	//    省略要自己說出來, and a number that is always 0 would say the opposite of
	//    the truth about a task that really does have a deliverable pinned.
	count, ok := raw["artifact_count"]
	if !ok {
		t.Fatalf("get_task must carry artifact_count — 省略要自己說出來: %s", body)
	}
	if string(count) != "1" {
		t.Fatalf("artifact_count = %s, want 1 — the count is EXACT and un-capped", count)
	}
}

// TestTaskArtifactCountRidesEveryExitOfTheSharedBuilder: the slimming is done in
// newTaskDTO so that nine responses get thinner at once (EXECUTOR JUDGEMENT —
// the owner ruled the payload, not the layer). A per-handler fix would leave
// the other eight serving the fat rows, so one of the eight is checked here.
func TestTaskArtifactCountRidesEveryExitOfTheSharedBuilder(t *testing.T) {
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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, gone := range []string{"artifacts", "artifacts_detail_level"} {
		if _, ok := raw[gone]; ok {
			t.Fatalf("set_task_deps's task payload still declares %q — the slimming lives in "+
				"newTaskDTO precisely so all nine exits say the same thing: %s",
				gone, rec.Body.String())
		}
	}
	if got := string(raw["artifact_count"]); got != "1" {
		t.Fatalf("set_task_deps's task payload must carry artifact_count 1 too, got %q", got)
	}
}

// TestListTaskArtifactsServesTheWholeRowAndSaysSo is the other half: everything
// the task view dropped has to be reachable, in full, through the read that
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
		t.Fatalf("artifacts_detail_level = %q, want full — since T-92 it has no opposite left "+
			"to contrast with, and is kept so a reader holding this payload does not have to "+
			"know which server version produced it to know the rows are whole",
			got.ArtifactsDetailLevel)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %+v", got.Artifacts)
	}
	a := got.Artifacts[0]
	if a.ID != artID || a.Kind != "link" || a.URL != t66ArtifactURL ||
		a.Name != t66ArtifactName || a.CreatedBy != "m-exec" || a.CreatedTS <= 0 {
		t.Fatalf("full artifact row is not full: %+v", a)
	}

	// 🔴 attachment_id, restored by the owner on rc-91e29b576ad8 after T-92
	// dropped it as duplication of url. THIS ROW IS A LINK, which is what makes
	// the assertion worth making: a link's url is its external target, so there
	// is no blob path to slice an id out of, and nothing else in the tool
	// surface lists an artifact's blob. Without this field a member cannot obey
	// system_interaction §2.1 —「直接用它的 id」for `ocagent diff` — on a link
	// at all, and can only obey it on a file by hand-building an address the
	// same document forbids.
	if a.AttachmentID == "" {
		t.Fatalf("attachment_id is empty on a link row: %+v", a)
	}
	var stored TaskArtifact
	arts, err := api.dal.ListTaskArtifacts(taskID)
	if err != nil {
		t.Fatalf("read stored artifacts: %v", err)
	}
	for _, r := range arts {
		if r.ID == artID {
			stored = r
		}
	}
	if a.AttachmentID != stored.AttachmentID {
		t.Fatalf("attachment_id on the wire is %q, the stored row holds %q — the field is "+
			"the row's own blob id served as stored, not a value derived at read time",
			a.AttachmentID, stored.AttachmentID)
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
			"kind": "link", "url": t66ArtifactURL + "/" + n, "name": n,
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
