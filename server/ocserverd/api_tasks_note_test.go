package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── T-cc3e 步驟備註欄 guards ─────────────────────────────────────────────────
//
// These assert the VALUE that came back out, never merely that a field is
// declared: the ticket exists because a documented note field turned out not to
// exist, and a test that only reads the schema would have passed just as
// happily against that nothing. Every case below writes a distinctive string
// and reads it back through the real read path (get_task's step view).

// writeStepNote posts one note write as the given caller and returns the
// recorder, so each case asserts its own status code.
func writeStepNote(t *testing.T, api *apiServer, taskID, stepID, caller, note string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/note",
			map[string]any{"note": note}, caller, "agent"),
		taskID, stepID)
	return rec
}

// readStepNote re-reads one step through the task view — the path a successor
// session actually uses (get_task), not the DAL.
func readStepNote(t *testing.T, api *apiServer, taskID, stepID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	for _, st := range decodeBody[taskDTO](t, rec).Steps {
		if st.ID == stepID {
			return st.Note
		}
	}
	t.Fatalf("step %s missing from the task view", stepID)
	return ""
}

// TestStepNoteRoundTripsThroughTheTaskView is the核心 assertion: what a
// handover writes is what the next session reads. Dropping `note` from the
// persisted column list, from the DTO projection, or from the handler's assign
// reddens this — each of those is a way for the write to look successful and
// still leave the reader with nothing, which is precisely the failure this
// ticket was opened about.
func TestStepNoteRoundTripsThroughTheTaskView(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	stepID := view.Steps[0].ID
	const note = "跑完 conformance，紅在 auth matrix 第 3 case；下一步接前端 i18n"

	rec := writeStepNote(t, api, task.ID, stepID, "m-exec", note)
	if rec.Code != http.StatusOK {
		t.Fatalf("write note: %d %s", rec.Code, rec.Body.String())
	}
	// The receipt must echo what was STORED — the caller has to be able to
	// confirm the landing without a second round trip.
	if got := decodeBody[taskStepNoteReceiptDTO](t, rec).Note; got != note {
		t.Fatalf("receipt note = %q, want %q", got, note)
	}
	if got := readStepNote(t, api, task.ID, stepID); got != note {
		t.Fatalf("note read back = %q, want %q", got, note)
	}
	// A note belongs to ONE step: the sibling must not have picked it up.
	if got := readStepNote(t, api, task.ID, view.Steps[1].ID); got != "" {
		t.Fatalf("sibling step note = %q, want empty", got)
	}
}

// TestStepNoteWritableInEveryStepStatus pins the ticket's whole reason to
// exist. The two note-shaped fields that already existed are each locked to one
// moment — waiting_reason to waiting_external, the handoff fields to the
// closing report — so a handover landing at any other moment had nowhere to
// write. If someone later "tidies up" by gating this route on a step status,
// this reddens.
func TestStepNoteWritableInEveryStepStatus(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "pending lane", "dod": "d1"},
		{"name": "working lane", "dod": "d2"},
		{"name": "parked lane", "dod": "d3"},
		{"name": "finished lane", "dod": "d4"},
	})
	pending, working := view.Steps[0], view.Steps[1]
	parked, finished := view.Steps[2], view.Steps[3]

	// working → in_progress; parked → waiting_external; finished → done.
	if rec := reportStepStatus(t, api, task.ID, working.ID, "m-exec", "in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("start working lane: %d %s", rec.Code, rec.Body.String())
	}
	for _, s := range []string{"in_progress", "waiting_external"} {
		reason := ""
		if s == "waiting_external" {
			reason = "waiting on a third party"
		}
		if rec := reportStepStatus(t, api, task.ID, parked.ID, "m-exec", s, reason); rec.Code != http.StatusOK {
			t.Fatalf("park lane %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}
	for _, s := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, task.ID, finished.ID, "m-exec", s, ""); rec.Code != http.StatusOK {
			t.Fatalf("finish lane %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}

	for _, tc := range []struct{ name, stepID, note string }{
		{"pending", pending.ID, "not started; picks up after the merge lane"},
		{"in_progress", working.ID, "half way — schema regenerated, handler next"},
		{"waiting_external", parked.ID, "note and waiting_reason are different fields"},
		{"done", finished.ID, "finished; recording what it actually produced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := writeStepNote(t, api, task.ID, tc.stepID, "m-exec", tc.note); rec.Code != http.StatusOK {
				t.Fatalf("write in %s: %d %s", tc.name, rec.Code, rec.Body.String())
			}
			if got := readStepNote(t, api, task.ID, tc.stepID); got != tc.note {
				t.Fatalf("%s note = %q, want %q", tc.name, got, tc.note)
			}
		})
	}
	// waiting_reason is a SEPARATE field: writing a note must not have clobbered
	// the parked lane's reason, and the note must not be the reason.
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+task.ID, nil, "m-exec", "agent"), task.ID)
	for _, st := range decodeBody[taskDTO](t, rec).Steps {
		if st.ID == parked.ID && st.WaitingReason != "waiting on a third party" {
			t.Fatalf("waiting_reason = %q, want it untouched by the note write",
				st.WaitingReason)
		}
	}
}

// TestStepNoteIsWholesaleAndClearable — it is a current-state note, not an
// append-only log: the second write replaces the first, and "" empties it.
func TestStepNoteIsWholesaleAndClearable(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID
	for _, want := range []string{"first pass", "second pass replaces it", ""} {
		if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", want); rec.Code != http.StatusOK {
			t.Fatalf("write %q: %d %s", want, rec.Code, rec.Body.String())
		}
		if got := readStepNote(t, api, task.ID, stepID); got != want {
			t.Fatalf("note = %q, want %q", got, want)
		}
	}
}

// TestStepNoteSurvivesAReplan — a replan keeps done steps as history, and the
// note is the most valuable thing on a finished step (what it actually
// produced). Losing it on submit_plan would silently destroy exactly the
// handover record this field exists to carry.
func TestStepNoteSurvivesAReplan(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	doneStep := view.Steps[0].ID
	for _, s := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, task.ID, doneStep, "m-exec", s, ""); rec.Code != http.StatusOK {
			t.Fatalf("drive done %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}
	const note = "produced the spec diff; regenerated all three files"
	if rec := writeStepNote(t, api, task.ID, doneStep, "m-exec", note); rec.Code != http.StatusOK {
		t.Fatalf("write note: %d %s", rec.Code, rec.Body.String())
	}
	submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "rethought", "dod": "d3"},
	})
	if got := readStepNote(t, api, task.ID, doneStep); got != note {
		t.Fatalf("note after replan = %q, want it kept as %q", got, note)
	}
}

// TestStepNoteWriteMovesTaskUpdatedTS — the delivery guard.
//
// Storing the note is not the deliverable; the owner SEEING it is. A task card
// he already has open re-reads its step-bearing detail only when updated_ts
// changes: the SSE task delta carries id/status/priority and the list it
// refreshes carries no steps at all. An earlier draft of this handler skipped
// the bump on purpose and shipped a note that was invisible to exactly the
// person watching a live handover — the one case the ticket exists for. Drop
// the TouchTaskUpdatedTS call and this reddens.
func TestStepNoteWriteMovesTaskUpdatedTS(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	stepID := view.Steps[0].ID

	before, err := api.dal.GetTask(task.ID)
	if err != nil || before == nil {
		t.Fatalf("load task: %v %v", before, err)
	}
	if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", "第 4 步跑到 conformance"); rec.Code != http.StatusOK {
		t.Fatalf("write note: %d %s", rec.Code, rec.Body.String())
	}
	after, err := api.dal.GetTask(task.ID)
	if err != nil || after == nil {
		t.Fatalf("reload task: %v %v", after, err)
	}
	if !(after.UpdatedTS > before.UpdatedTS) {
		t.Fatalf("updated_ts did not move (%v → %v) — an already-open task card "+
			"will never re-hydrate, so the note is invisible to the owner",
			before.UpdatedTS, after.UpdatedTS)
	}
	// The bump must not have smeared anything else across the task row.
	if after.Status != before.Status || after.Priority != before.Priority ||
		after.ExecutorID != before.ExecutorID {
		t.Fatalf("note write changed more than updated_ts: %+v → %+v", *before, *after)
	}
}

// TestSetTaskStepNoteWritesOnlyTheNoteColumn — a DAL-level guard.
//
// 🔴 Read what this does and does NOT cover before trusting it.
//
// COVERS: the DAL write itself touches one column. Widen SetTaskStepNote to
// write any other column and this reddens.
//
// DOES NOT COVER: the handler reverting to the load-mutate-save shape every
// other step writer uses (GetTaskStep → mutate → PutTaskStep). That mutation
// was run and SURVIVED this test — the danger only appears when a CONCURRENT
// writer lands between some OTHER handler's read and its write, and there is no
// seam here to interleave at.
//
// ⚠️ That gap is no longer untested. api_tasks_note_race_test.go constructs it
// (T-e271 node 6): a deterministic interleave replaying update_step_status's
// own read-mutate-write order, and two goroutines driving the two real
// endpoints for 60 rounds. Both were measured RED before the fix. The fix is an
// ownership boundary — `note` is out of PutTaskStep's ON CONFLICT list, pinned
// by TestTaskStepNoteRaceGuardHasTeeth — so this DAL-level guard now sits
// alongside real behavioural coverage rather than standing in for it.
func TestSetTaskStepNoteWritesOnlyTheNoteColumn(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	stepID := view.Steps[0].ID
	if rec := reportStepStatus(t, api, task.ID, stepID, "m-exec", "in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("start step: %d %s", rec.Code, rec.Body.String())
	}
	before, err := api.dal.GetTaskStep(stepID)
	if err != nil || before == nil {
		t.Fatalf("load step: %v %v", before, err)
	}

	ok, err := api.dal.SetTaskStepNote(stepID, "written straight through the DAL")
	if err != nil || !ok {
		t.Fatalf("SetTaskStepNote: ok=%v err=%v", ok, err)
	}
	after, err := api.dal.GetTaskStep(stepID)
	if err != nil || after == nil {
		t.Fatalf("reload step: %v %v", after, err)
	}
	if after.Note != "written straight through the DAL" {
		t.Fatalf("note = %q, want it written", after.Note)
	}
	// Everything else must be byte-identical: compare the whole struct with the
	// note field normalised away, so a newly added column is covered too.
	a, b := *before, *after
	a.Note, b.Note = "", ""
	if a != b {
		t.Fatalf("SetTaskStepNote changed more than the note column:\n before=%+v\n after =%+v", a, b)
	}
	// A step that is gone reports false rather than resurrecting itself.
	if ok, err := api.dal.SetTaskStepNote("ts-does-not-exist", "x"); err != nil || ok {
		t.Fatalf("write to a missing step: ok=%v err=%v, want ok=false", ok, err)
	}
}

// TestStepNoteRefusedOnAClosedTask — the tool description promises writability
// in every STEP status, and this is the line that promise stops at: once every
// step is done the task auto-closes, and a closed task's record stops moving
// (the same rule the artifact set follows). Pinned so the copy and the code
// cannot drift apart again.
func TestStepNoteRefusedOnAClosedTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	driveTaskDone(t, api, task.ID, "m-exec")
	view := submitPlanFetch(t, api, task.ID)

	rec := writeStepNote(t, api, task.ID, view[0].ID, "m-exec", "too late")
	if rec.Code != http.StatusConflict {
		t.Fatalf("write on a closed task: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already closed") {
		t.Fatalf("409 body = %s, want it to name the closed task", rec.Body.String())
	}
}

// submitPlanFetch reads a task's steps straight from the DAL (the plan view is
// not available after the task closed).
func submitPlanFetch(t *testing.T, api *apiServer, taskID string) []TaskStep {
	t.Helper()
	steps, err := api.dal.ListTaskSteps(taskID)
	if err != nil || len(steps) == 0 {
		t.Fatalf("list steps: %v %v", steps, err)
	}
	return steps
}

// TestStepNoteAcceptsAdminCapability — the guard is executor-OR-admin, and only
// the executor half was covered. An admin助理 fixing a note on someone else's
// task must not be refused. Both faces onto the field are asserted here: the
// patch face calls callerMayDriveTask through the SAME helper, and the route
// table says so ("the handler shares it verbatim"), so the admin half has to be
// pinned on both or the claim rests on one face only.
func TestStepNoteAcceptsAdminCapability(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	stepID := view.Steps[0].ID

	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/steps/"+stepID+"/note",
			map[string]any{"note": "written by the owner"}, wireOwnerID, "owner"),
		task.ID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner write: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := readStepNote(t, api, task.ID, stepID); got != "written by the owner" {
		t.Fatalf("note = %q, want the admin write to have landed", got)
	}

	// Same caller, the anchor-patch face: an admin amending one segment of a note
	// on someone else's task must land too, and land splice-precise.
	status, data := patchStepNoteAs(t, api, task.ID, stepID, wireOwnerID, "owner",
		map[string]any{"edits": []any{
			map[string]any{"old": "the owner", "new": "the admin, by anchor"},
		}})
	if status != http.StatusOK {
		t.Fatalf("admin patch: %d %v, want 200", status, data)
	}
	if got := readStepNote(t, api, task.ID, stepID); got != "written by the admin, by anchor" {
		t.Fatalf("note = %q, want the admin patch to have landed", got)
	}
}

// TestStepNoteRefusesTheWrongCaller — the same executor-or-admin gate as every
// other task-driving write. Dropping callerMayDriveTask from the handler
// reddens this; the assertion names the REASON, not just the failure, so a
// 403 arriving for some unrelated cause cannot pass for the guard working.
func TestStepNoteRefusesTheWrongCaller(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID

	rec := writeStepNote(t, api, task.ID, stepID, "m-someone-else", "not my task")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger write: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not the task's executor") {
		t.Fatalf("403 body = %s, want it to name the executor guard", rec.Body.String())
	}
	// And the refusal must be real, not cosmetic: nothing landed.
	if got := readStepNote(t, api, task.ID, stepID); got != "" {
		t.Fatalf("note after refused write = %q, want empty", got)
	}
}

// TestStepNoteRefusesAnUnknownStep — a step id belonging to another task is a
// 404, not a silent write onto whatever row matched.
func TestStepNoteRefusesAnUnknownStep(t *testing.T) {
	api := newTasksTestServer(t)
	mine := createAdHocTask(t, api, "m-exec")
	submitPlan(t, api, mine.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	other := createAdHocTask(t, api, "m-exec")
	otherView := submitPlan(t, api, other.ID, "m-exec", []map[string]any{{"name": "x", "dod": "d"}})

	rec := writeStepNote(t, api, mine.ID, otherView.Steps[0].ID, "m-exec", "wrong task")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-task write: %d %s, want 404", rec.Code, rec.Body.String())
	}
	if got := readStepNote(t, api, other.ID, otherView.Steps[0].ID); got != "" {
		t.Fatalf("other task's step note = %q, want empty", got)
	}
}

// TestStepNoteRefusesOverTheCharLimit — the same ceiling as the task-level
// handover note, counted in RUNES: a 3,000-character Chinese note is well
// inside the limit and must be accepted, which a byte-based count would reject.
func TestStepNoteRefusesOverTheCharLimit(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	stepID := view.Steps[0].ID

	legal := strings.Repeat("備", 3000) // 9,000 bytes, 3,000 runes
	if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", legal); rec.Code != http.StatusOK {
		t.Fatalf("3,000-rune CJK note: %d %s, want 200", rec.Code, rec.Body.String())
	}
	over := strings.Repeat("備", chatBodyMaxChars+1)
	rec := writeStepNote(t, api, task.ID, stepID, "m-exec", over)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-cap note: %d %s, want 400", rec.Code, rec.Body.String())
	}
	// The refusal must leave the previous note intact, not half-apply.
	if got := readStepNote(t, api, task.ID, stepID); got != legal {
		t.Fatalf("note after refused over-cap write changed; want the legal one kept")
	}
}
