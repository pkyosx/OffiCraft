package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── T-bb70: the close-out report answers a BOUNDED receipt ───────────────────
//
// Both exits of HandleReportTaskCloseout… used to answer with the whole task —
// the idempotent no-op AND the first successful report. On a real ticket (a fat
// description plus a long plan) that body measured ~51k characters for a write
// that carries one bit of news. These tests pin the receipt's TOP-LEVEL KEY SET
// on both exits, so putting the whole task back turns them red instead of
// silently passing (a whole-task body happens to contain the receipt's fields).

// closeoutReceiptKeys is the exact top-level key set of the close-out receipt.
var closeoutReceiptKeys = []string{
	"closeout_reported", "closeout_ts", "task_id", "task_status",
}

// assertTopLevelKeys pins the response's top-level JSON key set EXACTLY: an
// extra key (whole-task regression) or a missing one is a failure, and the
// message names the difference. This is the anti-tautology guard — asserting
// "field X is present" would stay green if the handler answered the whole task.
func assertTopLevelKeys(t *testing.T, rec *httptest.ResponseRecorder, want []string, what string) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("%s: decode body (%d): %v", what, rec.Code, err)
	}
	got := make([]string, 0, len(raw))
	for k := range raw {
		got = append(got, k)
	}
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if strings.Join(got, ",") == strings.Join(wantSorted, ",") {
		return
	}
	wantSet := map[string]bool{}
	for _, k := range wantSorted {
		wantSet[k] = true
	}
	gotSet := map[string]bool{}
	var extra, missing []string
	for _, k := range got {
		gotSet[k] = true
		if !wantSet[k] {
			extra = append(extra, k)
		}
	}
	for _, k := range wantSorted {
		if !gotSet[k] {
			missing = append(missing, k)
		}
	}
	t.Fatalf("%s: response is NOT the bounded receipt — top-level key set differs.\n"+
		"  want (%d): %v\n  got  (%d): %v\n  EXTRA keys (whole-task leak): %v\n  MISSING keys: %v\n"+
		"  body length: %d chars",
		what, len(wantSorted), wantSorted, len(got), got, extra, missing, rec.Body.Len())
}

// fatCloseoutTask builds ONE realistically heavy terminal task — the shape that
// made the old whole-task answer ~51k characters: a long description plus a
// multi-step plan whose steps carry real Definition-of-Done prose. Both the
// before/after size measurement and the receipt assertions use this same
// fixture, so the two numbers are comparable.
func fatCloseoutTask(t *testing.T, api *apiServer, executor string) string {
	t.Helper()
	para := strings.Repeat(
		"The executor must reconcile the upstream ledger with the local mirror, "+
			"record every divergence with its row id, and leave the reconciliation "+
			"note where the next session can find it without re-deriving anything. ", 12)
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{
			"title":              "T-bb70 size fixture — a realistically heavy ticket",
			"executor_member_id": executor,
			"description":        strings.Repeat(para, 3),
		}, executor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create fat task: %d %s", rec.Code, rec.Body.String())
	}
	task := createdTaskView(t, api, rec)

	steps := make([]map[string]any, 0, 15)
	for i := 0; i < 15; i++ {
		n := strconv.Itoa(i + 1)
		steps = append(steps, map[string]any{
			"name": "step " + n + " — reconcile the ledger slice and write the divergence note",
			"dod":  "Slice " + n + " reconciled. " + para,
		})
	}
	view := submitPlan(t, api, task.ID, executor, steps)
	for _, st := range view.Steps {
		for _, status := range []string{"in_progress", "done"} {
			if r := reportStepStatus(t, api, task.ID, st.ID, executor, status, ""); r.Code != http.StatusOK {
				t.Fatalf("drive step %s %s: %d %s", st.ID, status, r.Code, r.Body.String())
			}
		}
	}
	return task.ID
}

// TestReportTaskCloseoutBodySize MEASURES (never estimates) the close-out
// response for the fat fixture and logs the character count, plus the size of
// the whole-task body the route used to answer with (GET /api/tasks/{id} —
// byte-identical to the old s.writeTask exit). Run with -v to read both.
func TestReportTaskCloseoutBodySize(t *testing.T) {
	api := newTasksTestServer(t)
	taskID := fatCloseoutTask(t, api, "m-exec")

	whole := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(whole,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if whole.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", whole.Code, whole.Body.String())
	}

	first := reportCloseout(t, api, taskID, "m-exec", "agent")
	if first.Code != http.StatusOK {
		t.Fatalf("close-out: %d %s", first.Code, first.Body.String())
	}
	repeat := reportCloseout(t, api, taskID, "m-exec", "agent")
	if repeat.Code != http.StatusOK {
		t.Fatalf("repeat close-out: %d %s", repeat.Code, repeat.Body.String())
	}

	t.Logf("MEASURED whole-task body (the OLD close-out answer): %d chars",
		whole.Body.Len())
	t.Logf("MEASURED close-out response, first report: %d chars", first.Body.Len())
	t.Logf("MEASURED close-out response, idempotent repeat: %d chars",
		repeat.Body.Len())
}

// TestReportTaskCloseoutAnswersABoundedReceiptOnBothExits pins BOTH exits —
// the first (stamping) report and the idempotent no-op repeat.
func TestReportTaskCloseoutAnswersABoundedReceiptOnBothExits(t *testing.T) {
	api := newTasksTestServer(t)
	taskID := fatCloseoutTask(t, api, "m-exec")

	// Exit 1 — the first report, which stamps.
	first := reportCloseout(t, api, taskID, "m-exec", "agent")
	if first.Code != http.StatusOK {
		t.Fatalf("first close-out: %d %s", first.Code, first.Body.String())
	}
	assertTopLevelKeys(t, first, closeoutReceiptKeys, "first close-out report")
	got := decodeBody[taskCloseoutReceiptDTO](t, first)
	if got.TaskID != taskID || got.TaskStatus != TaskStatusDone ||
		!got.CloseoutReported || got.CloseoutTS <= 0 {
		t.Fatalf("first receipt wrong content: %+v", got)
	}

	// Exit 2 — the idempotent no-op. Same bounded shape, same stamp.
	repeat := reportCloseout(t, api, taskID, "m-exec", "agent")
	if repeat.Code != http.StatusOK {
		t.Fatalf("repeat close-out: %d %s", repeat.Code, repeat.Body.String())
	}
	assertTopLevelKeys(t, repeat, closeoutReceiptKeys, "idempotent repeat close-out report")
	again := decodeBody[taskCloseoutReceiptDTO](t, repeat)
	if again != got {
		t.Fatalf("idempotent repeat must serve the SAME receipt: %+v vs %+v", again, got)
	}
}
