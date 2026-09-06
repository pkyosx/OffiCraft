package main

// T-4595 — get_my_task is RETIRED, and the assigned → active claim moved with
// it onto report_waking's outsource arm.
//
// WHAT THESE PIN, AND WHY EACH ONE NEEDS ITS OWN ASSERTION:
//
//  1. THE FLIP STILL HAPPENS, ON ITS NEW WRITE POINT. Deleting the two lines in
//     workerReportWaking turns every worker permanently `assigned`, and NOTHING
//     ELSE IN THE SUITE NOTICES: the old sentinel lived in get_my_task's own
//     handler test and went to the grave with it, the outsource scheduler mints
//     workers `assigned` and never re-reads the status, and the reconcile FSM
//     keys on hub presence rather than on the row. Verified by mutant, see the
//     header of TestWorkerClaimsItsTaskOnReportWaking.
//  2. WHAT THE WORKER LOST, IT GETS FROM get_task. The retirement's whole
//     premise is that get_task is a SUPERSET of what a worker needs, so the
//     four things the SOP tells a worker to look for — the step list, each
//     step's DoD, is_gate, parallel_group — are asserted through the face a
//     worker actually calls, under a worker's own ow- token. A projection that
//     quietly blanked any of them would leave the worker unable to do what the
//     shared boot sequence instructs.
//  3. THE ROUTE AND THE TOOL ARE GONE FROM THE TABLE. Removing a handler is not
//     the same as removing the surface; a stale row would boot-assert or, worse,
//     advertise a tool that 404s.
//
// ⚠️ NOT pinned here (deliberate, listed so nobody reads more into these than
// they say): the SSE fan-out ordering, and anything about the worker's own
// prompt text (that is package one's file set).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// seedAssignedWorker mints a worker row bound to taskID in the `assigned`
// state — the shape the outsource scheduler produces.
func seedAssignedWorker(t *testing.T, api *apiServer, id, taskID string) {
	t.Helper()
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: id, Codename: "S-1", Model: "sonnet", TaskID: taskID,
		Status: WorkerStatusAssigned, CreatedTS: 1,
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seeded worker unreadable: %v %+v", err, w)
	}
	// Positive control: the fixture really starts on the far side of the flip,
	// otherwise the assertion below would pass on a server that never flips.
	if w.Status != WorkerStatusAssigned {
		t.Fatalf("fixture must start assigned, got %q", w.Status)
	}
}

// TestWorkerClaimsItsTaskOnReportWaking is the sentinel for the moved write
// point. MUTANT VERIFIED: dropping the `if claimed { w.Status = ... }` lines
// from workerReportWaking fails HERE, on this test's own assertion
// ("report_waking must flip assigned → active").
//
// ⚠️ IT IS NOT THE ONLY ONE, AND AN EARLIER VERSION OF THIS COMMENT CLAIMED IT
// WAS. That mutant reddens TWO tests: this one, and
// api_tasks_reassign_test.go's TestReassignOutsourceSuccessorClaimsOnWakingThenTakesOver
// (which asserts the same flip as the precondition of a handover scenario).
// Both land on their own assertion about the flip, so the coverage is honest —
// the false half was the word "only". Recorded because an over-claiming comment
// is worse than no comment: the next reader skips their own re-verification on
// the strength of it.
func TestWorkerClaimsItsTaskOnReportWaking(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task := createAdHocTask(t, api, "m-exec")
	seedAssignedWorker(t, api, "ow-claimer", task.ID)

	if rec := reportWaking(t, api, "ow-claimer", "sonnet"); rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker("ow-claimer")
	if w == nil || w.Status != WorkerStatusActive {
		t.Fatalf("report_waking must flip assigned → active (it is now the ONLY "+
			"write point for that transition): %+v", w)
	}
	if w.ActivatedTS <= 0 {
		t.Fatalf("the flip must be durable — Status is a projection of "+
			"activated_ts, so a zero stamp reads back as `assigned`: %+v", w)
	}

	// The boot-reported model still lands, and a REPEAT report is a no-op on the
	// status: an already-active worker must not be re-claimed or re-stamped.
	before := w.ActivatedTS
	if rec := reportWaking(t, api, "ow-claimer", "opus"); rec.Code != http.StatusOK {
		t.Fatalf("second report_waking: %d %s", rec.Code, rec.Body.String())
	}
	again, _ := api.dal.GetOutsourceWorker("ow-claimer")
	if again.Status != WorkerStatusActive || again.ActivatedTS != before {
		t.Fatalf("a repeat report must not re-stamp the claim: %+v", again)
	}
	if again.ActualModel != "opus" {
		t.Fatalf("the boot-reported model must still land: %+v", again)
	}
}

// TestWorkerReadsItsWholePlanThroughGetTask is the other half of the
// retirement's premise: everything get_my_task used to hand a worker is
// reachable through get_task, under the worker's OWN token, unslimmed.
func TestWorkerReadsItsWholePlanThroughGetTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedManualWithKey(t, api, "review-pr")
	created, code := createTypedTask(t, api, "review-pr", "77")
	if code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	seedAssignedWorker(t, api, "ow-reader", created.TaskID)
	if rec := reportWaking(t, api, "ow-reader", "sonnet"); rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	submitPlan(t, api, created.TaskID, "m-exec", []map[string]any{
		{"name": "gather", "dod": "DODONE", "parallel_group": "lane"},
		{"name": "sift", "dod": "DODTHREE", "parallel_group": "lane"},
		{"name": "build", "dod": "DODTWO", "is_gate": true},
	})

	// The worker's own token, through the face it is now told to use.
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+created.TaskID, nil, "ow-reader", "agent"),
		created.TaskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("a worker must be able to read its own task: %d %s",
			rec.Code, rec.Body.String())
	}
	got := decodeBody[taskDTO](t, rec)
	if got.ID != created.TaskID {
		t.Fatalf("read the wrong task: %+v", got)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("get_task must list EVERY step, unslimmed: %+v", got.Steps)
	}
	// Each of the four things the shared SOP tells a worker to look for, named
	// separately — a projection that blanked any one of them would still list
	// three steps. Note the SECOND and THIRD steps: the retired projection kept
	// only the CURRENT step's prose, so asserting on step 0 alone would pass
	// against exactly the shape this ticket removed.
	if got.Steps[0].DoD != "DODONE" || got.Steps[1].DoD != "DODTHREE" ||
		got.Steps[2].DoD != "DODTWO" {
		t.Fatalf("every step must carry its DoD in full: %+v", got.Steps)
	}
	if got.Steps[0].ParallelGroup != "lane" || got.Steps[1].ParallelGroup != "lane" {
		t.Fatalf("parallel_group must survive — the SOP says fan out a sub-agent "+
			"per lane, which is unreadable without it: %+v", got.Steps)
	}
	if !got.Steps[2].IsGate {
		t.Fatalf("is_gate must survive — the SOP says SEE an approval gate "+
			"coming, not discover it: %+v", got.Steps[2])
	}
}

// TestGetMyTaskIsGoneFromEverySurface pins the retirement itself. The route
// table is the authority the MCP catalog, the spec-surface test and the auth
// matrix all derive from, so one stale row re-advertises a handler that no
// longer exists.
func TestGetMyTaskIsGoneFromEverySurface(t *testing.T) {
	specs := routeSpecs(&ServerInterfaceWrapper{})
	if len(specs) == 0 {
		t.Fatal("route table came back empty — this check would pass vacuously")
	}
	// Positive control: the verb that INHERITED the claim is still on the table
	// with its tool name, so a zero-hit search below means absence, not a broken
	// search.
	sawWaking := false
	for _, rs := range specs {
		if rs.Path == "/api/self/task" {
			t.Fatalf("GET /api/self/task is retired but still on the route table: %+v", rs)
		}
		if rs.MCPTool == "get_my_task" {
			t.Fatalf("get_my_task is retired but still advertised as a tool: %+v", rs)
		}
		if rs.Path == "/api/self/waking" && rs.MCPTool == "report_waking" {
			sawWaking = true
		}
	}
	if !sawWaking {
		t.Fatal("report_waking is missing from the route table — the search above " +
			"proves nothing without this control")
	}
}
