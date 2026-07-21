package main

// api_tasks_handoff_test.go — T-74f8 的兩個方向:
//
//   ① 該被擋的擋住了 — a creator≠executor task cannot reach done without saying
//      where the ball goes, and the refusal happens EARLY ENOUGH to still be
//      answerable (submit_plan must still work after the 422 — the whole trap
//      T-8a1e fell into).
//   ② 不該被擋的沒被擋 — the sentinels. A plain self-created ticket, a
//      pre-column blank-creator row, a mid-plan step report, and (the 32
//      production tickets that use them) a PARALLEL lane finishing while its
//      siblings still run must all behave EXACTLY as before.
//
// Every refusal assertion checks the MESSAGE, not only the code: "failed for
// the wrong reason" and "correctly refused" share a status code.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// seedHandoffTask writes a task row directly (the create handler's 正職授權矩陣
// forbids a plain agent from naming a DIFFERENT executor, which is exactly the
// creator≠executor shape under test) and gives it one pending step per name.
func seedHandoffTask(t *testing.T, api *apiServer, id, creator, executor string,
	stepNames ...string) Task {
	t.Helper()
	task := Task{
		ID: id, Title: "handoff fixture", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: executor, CreatorID: creator, CreatedTS: 100, UpdatedTS: 100,
	}
	if err := api.dal.PutTask(task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	for i, name := range stepNames {
		if err := api.dal.PutTaskStep(TaskStep{
			ID: id + "-s" + string(rune('a'+i)), TaskID: id, OrderIdx: i,
			Name: name, DoD: "done when done", Status: StepStatusInProgress,
		}); err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}
	return task
}

func seedActiveMember(t *testing.T, api *apiServer, id string) {
	t.Helper()
	if err := api.dal.PutMember(Member{
		ID: id, Name: id, Kind: "assistant", RoleKey: "dev",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed member %s: %v", id, err)
	}
}

// closeReport posts the step-done report carrying an optional handoff
// declaration and returns the recorder.
func closeReport(t *testing.T, api *apiServer, taskID, stepID, executor string,
	extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"status": StepStatusDone}
	for k, v := range extra {
		body[k] = v
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/status",
			body, executor, "agent"),
		taskID, stepID)
	return rec
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		return rec.Body.String()
	}
	// The error envelope nests {"error":{"message":…}} or carries a flat
	// message; fall back to the raw body so an assertion never silently passes
	// on an unexpected shape.
	if e, ok := env["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	if m, ok := env["message"].(string); ok {
		return m
	}
	return rec.Body.String()
}

func mustTask(t *testing.T, api *apiServer, id string) Task {
	t.Helper()
	got, err := api.dal.GetTask(id)
	if err != nil || got == nil {
		t.Fatalf("re-read task %s: %v", id, err)
	}
	return *got
}

// ── ① 該被擋的擋住了 ──────────────────────────────────────────────────────────

// The T-8a1e case itself: a task somebody else asked for, finished, with no
// successor anywhere. The report that would close it is refused — and the
// refusal must arrive while the plan is STILL EDITABLE, which is the only
// reason the gate lives before the step write rather than inside closeTask.
func TestHandoffGateRefusesTheClosingReportAndLeavesTheTaskAnswerable(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-aaaa00000001", "m-creator", "m-exec", "design")

	rec := closeReport(t, api, "t-aaaa00000001", "t-aaaa00000001-sa", "m-exec", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("closing report must be refused: %d %s", rec.Code, rec.Body.String())
	}
	msg := errorMessage(t, rec)
	for _, want := range []string{
		HandoffReturnToCreator, HandoffFollowUp, HandoffNone,
		"handoff_task_id", "handoff_note", "m-creator", "m-exec",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal must name %q so the caller can act on it; got: %s", want, msg)
		}
	}

	// Nothing was written: the step is untouched and the task is still open.
	step, err := api.dal.GetTaskStep("t-aaaa00000001-sa")
	if err != nil || step == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if step.Status != StepStatusInProgress {
		t.Fatalf("a refused close must not write the step: %s", step.Status)
	}
	after := mustTask(t, api, "t-aaaa00000001")
	if TaskIsTerminal(after.Status) || after.ClosedTS != 0 {
		t.Fatalf("a refused close must not close the task: %+v", after)
	}

	// The load-bearing half: submit_plan STILL WORKS. Had the gate sat inside
	// closeTask (or after the step write), the task would already be closed and
	// this call would be a permanent 409 — the executor would have been asked a
	// question it could no longer answer.
	plan := submitPlan(t, api, "t-aaaa00000001", "m-exec", []map[string]any{
		{"name": "design", "dod": "spec written"},
		{"name": "implement", "dod": "code merged"},
	})
	if len(plan.Steps) < 2 {
		t.Fatalf("replan after a refused close must land: %+v", plan.Steps)
	}
}

func TestHandoffReturnToCreatorMintsADurableTaskOnTheCreator(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-aaaa00000002", "m-creator", "m-exec", "design")

	rec := closeReport(t, api, "t-aaaa00000002", "t-aaaa00000002-sa", "m-exec",
		map[string]any{"handoff": HandoffReturnToCreator,
			"handoff_note": "後續實作要不要做由你決定"})
	if rec.Code != http.StatusOK {
		t.Fatalf("declared close must pass: %d %s", rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, "t-aaaa00000002")
	if closed.Status != TaskStatusDone || closed.Handoff != HandoffReturnToCreator {
		t.Fatalf("close + declaration must both land: %+v", closed)
	}
	if closed.HandoffTaskID == "" {
		t.Fatalf("return_to_creator must point at the minted follow-up: %+v", closed)
	}

	// The DURABLE half: an open task on the creator's own list — the thing an
	// SSE delta could never be.
	followUp := mustTask(t, api, closed.HandoffTaskID)
	if followUp.ExecutorID != "m-creator" || TaskIsTerminal(followUp.Status) {
		t.Fatalf("follow-up must be an OPEN task on the creator: %+v", followUp)
	}
	if !strings.Contains(followUp.Description, "後續") ||
		!strings.Contains(followUp.Description, "後續實作要不要做由你決定") {
		t.Fatalf("follow-up must carry the handover note: %q", followUp.Description)
	}
	open, err := api.dal.ListOpenTasksByExecutor("m-creator", 10)
	if err != nil || len(open) != 1 || open[0].ID != followUp.ID {
		t.Fatalf("follow-up must show on the creator's open list: %v %+v", err, open)
	}
	deps, err := api.dal.ListTaskDeps(followUp.ID)
	if err != nil || len(deps) != 1 || deps[0] != closed.ID {
		t.Fatalf("follow-up must record what it came from: %v %v", err, deps)
	}
	// …and half B fires on it immediately: the creator is TOLD, durably.
	assertHandoverChat(t, api, "m-creator", TaskNo(closed.ID))
}

func TestHandoffFollowUpAttachesTheDepToTheSuccessor(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-aaaa00000003", "m-creator", "m-exec", "design")
	successor := createAdHocTask(t, api, "m-exec")

	rec := closeReport(t, api, "t-aaaa00000003", "t-aaaa00000003-sa", "m-exec",
		map[string]any{"handoff": HandoffFollowUp, "handoff_task_id": successor.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("declared close must pass: %d %s", rec.Code, rec.Body.String())
	}
	deps, err := api.dal.ListTaskDeps(successor.ID)
	if err != nil || len(deps) != 1 || deps[0] != "t-aaaa00000003" {
		t.Fatalf("successor must now be blocked by the finished task: %v %v", err, deps)
	}
	closed := mustTask(t, api, "t-aaaa00000003")
	if closed.Handoff != HandoffFollowUp || closed.HandoffTaskID != successor.ID {
		t.Fatalf("declaration must be recorded: %+v", closed)
	}
}

func TestHandoffFollowUpRefusesAnUnusableSuccessor(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")

	cases := []struct {
		name    string
		taskID  string
		extra   map[string]any
		wantIn  string
		makeSuc func(*apiServer) string
	}{
		{
			name: "missing id", taskID: "t-aaaa00000004",
			extra: map[string]any{"handoff": HandoffFollowUp}, wantIn: "requires handoff_task_id",
		},
		{
			name: "unknown id", taskID: "t-aaaa00000005",
			extra:  map[string]any{"handoff": HandoffFollowUp, "handoff_task_id": "t-nope"},
			wantIn: "unknown successor task",
		},
		{
			name: "self reference", taskID: "t-aaaa00000006",
			extra: map[string]any{"handoff": HandoffFollowUp,
				"handoff_task_id": "t-aaaa00000006"},
			wantIn: "must not be this task itself",
		},
		{
			name: "already closed successor", taskID: "t-aaaa00000007",
			extra:  map[string]any{"handoff": HandoffFollowUp},
			wantIn: "is already closed",
			makeSuc: func(api *apiServer) string {
				id := "t-dead00000001"
				_ = api.dal.PutTask(Task{ID: id, Status: TaskStatusDone,
					Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
					ExecutorID: "m-x", ClosedTS: 1})
				return id
			},
		},
		{
			name: "junk value", taskID: "t-aaaa00000008",
			extra: map[string]any{"handoff": "sideways"}, wantIn: "handoff must be one of",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedHandoffTask(t, api, tc.taskID, "m-creator", "m-exec", "design")
			extra := map[string]any{}
			for k, v := range tc.extra {
				extra[k] = v
			}
			if tc.makeSuc != nil {
				extra["handoff_task_id"] = tc.makeSuc(api)
			}
			rec := closeReport(t, api, tc.taskID, tc.taskID+"-sa", "m-exec", extra)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("want 422, got %d %s", rec.Code, rec.Body.String())
			}
			if msg := errorMessage(t, rec); !strings.Contains(msg, tc.wantIn) {
				t.Fatalf("refusal must say %q; got: %s", tc.wantIn, msg)
			}
			if TaskIsTerminal(mustTask(t, api, tc.taskID).Status) {
				t.Fatalf("a refused close must leave the task open")
			}
		})
	}
}

func TestHandoffNoneNeedsAReasonAndIsRecorded(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-aaaa00000009", "m-creator", "m-exec", "design")

	rec := closeReport(t, api, "t-aaaa00000009", "t-aaaa00000009-sa", "m-exec",
		map[string]any{"handoff": HandoffNone})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a bare 'none' must be refused: %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessage(t, rec); !strings.Contains(msg, "requires handoff_note") {
		t.Fatalf("refusal must name the missing field; got: %s", msg)
	}

	rec = closeReport(t, api, "t-aaaa00000009", "t-aaaa00000009-sa", "m-exec",
		map[string]any{"handoff": HandoffNone, "handoff_note": "純調查,結論已寫在卡上"})
	if rec.Code != http.StatusOK {
		t.Fatalf("a reasoned 'none' must close: %d %s", rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, "t-aaaa00000009")
	if closed.Status != TaskStatusDone || closed.Handoff != HandoffNone ||
		closed.HandoffNote != "純調查,結論已寫在卡上" {
		t.Fatalf("the declaration must be the audit trail: %+v", closed)
	}
}

// The agent that did exactly what the owner prescribed — "開個 task 掛上這個
// design task 作為 dependency" — must never meet the gate at all.
func TestHandoffGateStandsAsideWhenASuccessorAlreadyDependsOnTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-aaaa0000000a", "m-creator", "m-exec", "design")
	successor := createAdHocTask(t, api, "m-exec")
	if err := api.dal.AddTaskDep(successor.ID, "t-aaaa0000000a"); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	rec := closeReport(t, api, "t-aaaa0000000a", "t-aaaa0000000a-sa", "m-exec", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("an already-real handover must not be re-asked: %d %s",
			rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, "t-aaaa0000000a")
	if closed.Handoff != HandoffFollowUp || closed.HandoffTaskID != successor.ID {
		t.Fatalf("the auto-satisfied handover must still be recorded: %+v", closed)
	}
}

func TestHandoffReturnToCreatorRefusesAnOffRosterCreator(t *testing.T) {
	api := newTasksTestServer(t)
	// No member row for m-ghost at all (a dismissed member / released ow- worker).
	seedHandoffTask(t, api, "t-aaaa0000000b", "m-ghost", "m-exec", "design")

	rec := closeReport(t, api, "t-aaaa0000000b", "t-aaaa0000000b-sa", "m-exec",
		map[string]any{"handoff": HandoffReturnToCreator})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", rec.Code, rec.Body.String())
	}
	msg := errorMessage(t, rec)
	if !strings.Contains(msg, "no active roster member") ||
		!strings.Contains(msg, HandoffNone) {
		t.Fatalf("refusal must explain AND offer the other doors; got: %s", msg)
	}
}

// ── ② 不該被擋的沒被擋 — the sentinels ────────────────────────────────────────

// The plain case the whole office runs on: an agent's own ticket, finished,
// nothing to hand anywhere. 270 of the 392 live tasks are this shape and NONE
// of them may gain a single step of friction.
func TestSentinelSelfCreatedTaskClosesWithNoHandoffAtAll(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-solo") // creator == executor
	planned := submitPlan(t, api, task.ID, "m-solo", []map[string]any{
		{"name": "just do it", "dod": "done"},
	})
	step := planned.Steps[0].ID
	if rec := reportStepStatus(t, api, task.ID, step, "m-solo",
		StepStatusInProgress, ""); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	rec := closeReport(t, api, task.ID, step, "m-solo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a self-created ticket must still close untouched: %d %s",
			rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, task.ID)
	if closed.Status != TaskStatusDone {
		t.Fatalf("want done, got %s", closed.Status)
	}
	if closed.Handoff != HandoffUndeclared {
		t.Fatalf("the gate must not invent a declaration it never asked for: %q",
			closed.Handoff)
	}
}

// Pre-creator_id rows (53 live) name only one side — the gate must not invent
// an obligation it cannot even address.
func TestSentinelBlankCreatorClosesUntouched(t *testing.T) {
	api := newTasksTestServer(t)
	seedHandoffTask(t, api, "t-bbbb00000001", "", "m-exec", "legacy step")
	if rec := closeReport(t, api, "t-bbbb00000001", "t-bbbb00000001-sa",
		"m-exec", nil); rec.Code != http.StatusOK {
		t.Fatalf("a blank-creator row must close as before: %d %s",
			rec.Code, rec.Body.String())
	}
	if mustTask(t, api, "t-bbbb00000001").Status != TaskStatusDone {
		t.Fatalf("want done")
	}
}

// Only the report that would CLOSE the task is gated. A mid-plan node finishing
// is the most common write in the system.
func TestSentinelMidPlanStepReportIsNeverGated(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-bbbb00000002", "m-creator", "m-exec",
		"design", "build", "ship")
	for _, s := range []string{"-sa", "-sb"} {
		rec := closeReport(t, api, "t-bbbb00000002", "t-bbbb00000002"+s, "m-exec", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("mid-plan step %s must not be gated: %d %s", s,
				rec.Code, rec.Body.String())
		}
	}
	if TaskIsTerminal(mustTask(t, api, "t-bbbb00000002").Status) {
		t.Fatalf("task must still be open with one step left")
	}
	// …and the LAST one is gated, proving the two branches are the same code
	// path discriminated only by "would this close the task".
	if rec := closeReport(t, api, "t-bbbb00000002", "t-bbbb00000002-sc",
		"m-exec", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the closing step MUST be gated: %d %s", rec.Code, rec.Body.String())
	}
}

// 32 production tickets use parallel_group. A lane finishing while its siblings
// still run must derive to in_progress, never trip the gate. This is the
// specific failure mode a hand-rolled "is this the last step?" check would have
// shipped — the gate asks DeriveTaskStatus instead.
func TestSentinelParallelLaneFinishingIsNeverGated(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-bbbb00000003", "m-creator", "m-exec", "lane A", "lane B")
	for _, id := range []string{"t-bbbb00000003-sa", "t-bbbb00000003-sb"} {
		st, _ := api.dal.GetTaskStep(id)
		st.ParallelGroup = "g1"
		if err := api.dal.PutTaskStep(*st); err != nil {
			t.Fatalf("group step: %v", err)
		}
	}
	if rec := closeReport(t, api, "t-bbbb00000003", "t-bbbb00000003-sa",
		"m-exec", nil); rec.Code != http.StatusOK {
		t.Fatalf("a parallel lane must close freely while siblings run: %d %s",
			rec.Code, rec.Body.String())
	}
	if got := mustTask(t, api, "t-bbbb00000003").Status; got != TaskStatusInProgress {
		t.Fatalf("task must stay in_progress: %s", got)
	}
}

// Non-done reports are outside the gate entirely.
func TestSentinelNonDoneReportsAreOutsideTheGate(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-bbbb00000004", "m-creator", "m-exec", "only step")
	rec := reportStepStatus(t, api, "t-bbbb00000004", "t-bbbb00000004-sa",
		"m-exec", StepStatusWaitingExternal, "waiting on vendor")
	if rec.Code != http.StatusOK {
		t.Fatalf("waiting_external must be unaffected: %d %s", rec.Code, rec.Body.String())
	}
}

// The owner's terminate is NOT the executor's close: it is a deliberate
// decision by the person the ball would go back to. Gating it would be exactly
// the "擴大打擊面" failure — and it also proves the gate is not sitting inside
// closeTask (every terminal path would be caught there).
func TestSentinelOwnerTerminateIsNotGated(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	seedHandoffTask(t, api, "t-bbbb00000005", "m-creator", "m-exec", "only step")
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/t-bbbb00000005/terminate", nil,
			"owner", "owner"), "t-bbbb00000005")
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate must not be gated: %d %s", rec.Code, rec.Body.String())
	}
	if mustTask(t, api, "t-bbbb00000005").Status != TaskStatusTerminated {
		t.Fatalf("want terminated")
	}
}

// ── half B: a dep actually hands over ────────────────────────────────────────

func assertHandoverChat(t *testing.T, api *apiServer, recipient, mentions string) {
	t.Helper()
	msgs, err := api.dal.ListChatInvolving(recipient, 50)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Recipient == recipient && strings.Contains(m.Body, mentions) {
			return
		}
	}
	t.Fatalf("no durable chat row to %s mentioning %s (found %d rows)",
		recipient, mentions, len(msgs))
}

func TestBlockerCloseReleasesAndTellsTheDependentExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	blocker := seedHandoffTask(t, api, "t-cccc00000001", "m-creator", "m-exec", "design")
	dependent := seedHandoffTask(t, api, "t-cccc00000002", "m-creator", "m-next")
	if err := api.dal.AddTaskDep(dependent.ID, blocker.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	if rec := closeReport(t, api, blocker.ID, blocker.ID+"-sa", "m-exec", nil); rec.Code != http.StatusOK {
		// The dep itself auto-satisfies the gate, so this must pass.
		t.Fatalf("close: %d %s", rec.Code, rec.Body.String())
	}
	// The DURABLE half — an SSE frame alone is what failed in T-8a1e.
	assertHandoverChat(t, api, "m-next", TaskNo(blocker.ID))
}

func TestASecondLiveBlockerHoldsTheReleaseBack(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	first := seedHandoffTask(t, api, "t-cccc00000003", "m-creator", "m-exec", "design")
	second := seedHandoffTask(t, api, "t-cccc00000004", "m-creator", "m-exec2", "research")
	dependent := seedHandoffTask(t, api, "t-cccc00000005", "m-creator", "m-next")
	for _, b := range []string{first.ID, second.ID} {
		if err := api.dal.AddTaskDep(dependent.ID, b); err != nil {
			t.Fatalf("add dep: %v", err)
		}
	}
	if rec := closeReport(t, api, first.ID, first.ID+"-sa", "m-exec", nil); rec.Code != http.StatusOK {
		t.Fatalf("close first: %d %s", rec.Code, rec.Body.String())
	}
	msgs, err := api.dal.ListChatInvolving("m-next", 50)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Recipient == "m-next" && strings.Contains(m.Body, "不再擋著你") {
			t.Fatalf("a task still blocked by %s must NOT be announced released", second.ID)
		}
	}
	// Closing the second one releases it.
	if rec := closeReport(t, api, second.ID, second.ID+"-sa", "m-exec2", nil); rec.Code != http.StatusOK {
		t.Fatalf("close second: %d %s", rec.Code, rec.Body.String())
	}
	assertHandoverChat(t, api, "m-next", TaskNo(second.ID))
}

func TestTaskHasLiveBlocker(t *testing.T) {
	statuses := map[string]string{
		"t-open": TaskStatusInProgress,
		"t-done": TaskStatusDone,
		"t-term": TaskStatusTerminated,
		"t-dup":  TaskStatusDuplicated,
	}
	cases := []struct {
		name string
		ids  []string
		want bool
	}{
		{"no deps at all", nil, false},
		{"every blocker terminal", []string{"t-done", "t-term", "t-dup"}, false},
		{"one live blocker", []string{"t-done", "t-open"}, true},
		{"dangling id never wedges", []string{"t-vanished"}, false},
	}
	for _, tc := range cases {
		if got := taskHasLiveBlocker(tc.ids, statuses); got != tc.want {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, got)
		}
	}
}

// The 發包 queue holds a blocked task and mints it the moment the blocker
// closes — "設計完成以後自動轉開發" made real rather than decorative.
func TestOutsourceSchedulerHoldsABlockedTaskThenMintsOnRelease(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "build-it", "claude-sonnet-4-5", 2)
	dev := createOutsourceTask(t, api, "build-it", "implement the design")
	// The CONTROL, in the SAME tick: an identical 發包 task with no blocker at
	// all. Without it this test cannot tell "the dep guard held dev back" from
	// "the tick died before minting anything" — runOutsourceTick recovers its
	// own panics into a log line (outsource_sched.go), so a FAULTED tick mints
	// nothing and looks exactly like a correct hold. It is also the 不該被擋的
	// sentinel: a guard that over-blocks kills this task too.
	free := createOutsourceTask(t, api, "build-it", "unblocked sibling")
	design := seedHandoffTask(t, api, "t-dddd00000001", "m-front", "m-exec", "design")
	if err := api.dal.AddTaskDep(dev.ID, design.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	api.runOutsourceTick(1000.0)
	if held := mustTask(t, api, dev.ID); held.ExecutorID != "" {
		t.Fatalf("a task with a live blocker must NOT be minted: %+v", held)
	}
	if ran := mustTask(t, api, free.ID); ran.ExecutorID == "" {
		t.Fatalf("the tick must still MINT the unblocked sibling — otherwise the "+
			"hold above proves nothing (a faulted tick mints nothing either): %+v", ran)
	}

	// Closing the blocker releases it (the gate auto-satisfies off the dep).
	if rec := closeReport(t, api, design.ID, design.ID+"-sa", "m-exec", nil); rec.Code != http.StatusOK {
		t.Fatalf("close blocker: %d %s", rec.Code, rec.Body.String())
	}
	api.runOutsourceTick(1001.0)
	if freed := mustTask(t, api, dev.ID); freed.ExecutorID == "" {
		t.Fatalf("the released task must be minted for: %+v", freed)
	}
}

// SENTINEL for half B: every dep row in production points at an ALREADY
// terminal blocker. None of them may start holding anything back.
func TestSentinelADepOnAnAlreadyClosedBlockerNeverHoldsTheQueue(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "build-it", "claude-sonnet-4-5", 2)
	dev := createOutsourceTask(t, api, "build-it", "implement")
	if err := api.dal.PutTask(Task{ID: "t-dddd00000002", Status: TaskStatusDone,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-exec", ClosedTS: 5}); err != nil {
		t.Fatalf("seed closed blocker: %v", err)
	}
	if err := api.dal.AddTaskDep(dev.ID, "t-dddd00000002"); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	api.runOutsourceTick(1000.0)
	if got := mustTask(t, api, dev.ID); got.ExecutorID == "" {
		t.Fatalf("a dep on a finished blocker must not hold the queue: %+v", got)
	}
}

// ── the discoverability layer ────────────────────────────────────────────────

// The gate's 422 tells the caller to send `handoff` on update_step_status. An
// agent reaches that tool ONLY through tools/list, which ocserverd serves
// verbatim from the frozen spec/mcp-catalog.json (assets.go, embed-only) — so a
// catalog whose update_step_status inputSchema does not advertise the three
// levers leaves the gate ANSWERABLE ONLY BY GUESSING: the refusal names
// parameters the tool surface says do not exist.
//
// This is not hypothetical — it is the state the半成品 shipped in: openapi.json
// carried the fields, the catalog did not, and NOTHING in CI compares the two
// (mcp_test.go and conformance/test_rest_happy.py both key on the tool NAME set
// only). So pin it here: the levers the refusal advertises must exist on the
// tool the refusal names.
func TestCatalogAdvertisesTheHandoffLeversTheGateDemands(t *testing.T) {
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]any `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	found := false
	for _, tool := range catalog.Tools {
		if tool.Name != "update_step_status" {
			continue
		}
		found = true
		for _, lever := range []string{"handoff", "handoff_note", "handoff_task_id"} {
			if _, ok := tool.InputSchema.Properties[lever]; !ok {
				t.Errorf("update_step_status must advertise %q — the gate's 422 "+
					"instructs the caller to send it, and tools/list is the only "+
					"place the caller can learn the tool's shape", lever)
			}
		}
		// The description is the part an agent reads BEFORE it is refused; a
		// lever with no prose is a lever nobody reaches for.
		for _, want := range []string{HandoffReturnToCreator, HandoffFollowUp, HandoffNone} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("update_step_status description must name the %q way out", want)
			}
		}
	}
	if !found {
		t.Fatalf("update_step_status is not in the frozen catalog at all")
	}
}

// ── the outsource lane: the population that must NOT be trapped ──────────────

// An outsource task is created BY a 正職 and executed BY an anonymous ow- worker,
// so creator≠executor holds for EVERY 發包 ticket — the whole 發包 lane sits
// inside the gate's population, which is the sharpest edge in this change. The
// worker is dismissed shortly after it reports its close-out, so a gate it
// cannot answer would strand the ticket in_progress with a live worker burning
// cost and nobody holding the ball.
//
// Two halves, both load-bearing:
//   - it IS gated (the ball genuinely has to go back to the 正職 who 發包'd);
//   - it CAN get out, and the way out lands the ball DURABLY on that 正職.
func TestOutsourceWorkerIsGatedButCanAlwaysHandBackToItsDispatcher(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-front")
	task := Task{
		ID: "t-eeee00000001", Title: "發包出去的活", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource,
		ExecutorID: "ow-1", CreatorID: "m-front", CreatedTS: 100, UpdatedTS: 100,
	}
	if err := api.dal.PutTask(task); err != nil {
		t.Fatalf("seed outsource task: %v", err)
	}
	if err := api.dal.PutTaskStep(TaskStep{
		ID: task.ID + "-sa", TaskID: task.ID, OrderIdx: 0, Name: "build",
		DoD: "shipped", Status: StepStatusInProgress,
	}); err != nil {
		t.Fatalf("seed step: %v", err)
	}

	// Half 1 — gated, and the refusal is ACTIONABLE by a worker that has no
	// idea who its creator is (it must name them).
	rec := closeReport(t, api, task.ID, task.ID+"-sa", "ow-1", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a 發包 ticket must be gated too: %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessage(t, rec); !strings.Contains(msg, "m-front") {
		t.Fatalf("the refusal must name the dispatcher the ball goes back to: %s", msg)
	}

	// Half 2 — the escape hatch works, and it is DURABLE. This is the assertion
	// that keeps the 發包 lane from deadlocking: an outsource worker always has a
	// creator to hand back to, so return_to_creator can never be a dead end.
	rec = closeReport(t, api, task.ID, task.ID+"-sa", "ow-1",
		map[string]any{"handoff": HandoffReturnToCreator})
	if rec.Code != http.StatusOK {
		t.Fatalf("the worker must be able to close after handing back: %d %s",
			rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, task.ID)
	if closed.Status != TaskStatusDone || closed.Handoff != HandoffReturnToCreator {
		t.Fatalf("ticket must close with the declaration recorded: %+v", closed)
	}
	// The ball is a TASK on the dispatcher's open list, not a notification.
	open, err := api.dal.ListOpenTasksByExecutor("m-front", 50)
	if err != nil {
		t.Fatalf("list dispatcher's open tasks: %v", err)
	}
	for _, o := range open {
		if o.ID == closed.HandoffTaskID {
			return
		}
	}
	t.Fatalf("the handed-back ball must be an OPEN task on m-front: %+v", open)
}

// A dependent that is ALREADY terminal must be left completely alone when its
// blocker closes: no "你被解鎖了" chat to an executor who finished days ago, no
// UpdatedTS re-bump floating a closed task back up the cockpit. Without this
// the release fan would reach into the archive every time a late blocker lands.
func TestSentinelAnAlreadyClosedDependentIsNeverReleasedOrAnnounced(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	blocker := seedHandoffTask(t, api, "t-ffff00000001", "m-creator", "m-exec", "design")
	// The dependent finished BEFORE its blocker did (a perfectly ordinary
	// out-of-order close), so it is terminal with a settled UpdatedTS.
	done := Task{
		ID: "t-ffff00000002", Title: "already finished", Status: TaskStatusDone,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-next", CreatorID: "m-creator",
		CreatedTS: 10, UpdatedTS: 20, ClosedTS: 20,
	}
	if err := api.dal.PutTask(done); err != nil {
		t.Fatalf("seed terminal dependent: %v", err)
	}
	if err := api.dal.AddTaskDep(done.ID, blocker.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	// The dep itself auto-satisfies the gate only for a NON-terminal dependent,
	// so this close must declare where the ball goes — proof in itself that a
	// terminal dependent is not mistaken for a live handover target.
	rec := closeReport(t, api, blocker.ID, blocker.ID+"-sa", "m-exec",
		map[string]any{"handoff": HandoffNone, "handoff_note": "沒有後續"})
	if rec.Code != http.StatusOK {
		t.Fatalf("close blocker: %d %s", rec.Code, rec.Body.String())
	}

	after := mustTask(t, api, done.ID)
	if after.UpdatedTS != 20 {
		t.Fatalf("a terminal dependent must not be touched (updated_ts moved "+
			"%v → %v — it would float back up the cockpit)", 20.0, after.UpdatedTS)
	}
	msgs, err := api.dal.ListChatInvolving("m-next", 50)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Recipient == "m-next" && strings.Contains(m.Body, "不再擋著你") {
			t.Fatalf("a finished dependent's executor must not be told it was released")
		}
	}
}

// The gate stands aside when a successor already depends on this task — but
// "already depends" has to mean a LIVE successor. A dependent that is itself
// finished holds no ball, so it must NOT auto-satisfy the gate: otherwise any
// task that ever had a (now closed) follow-up would close silently forever,
// which is the T-8a1e hole re-opened through the side door.
func TestATerminalDependentDoesNotAutoSatisfyTheGate(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	blocker := seedHandoffTask(t, api, "t-gggg00000001", "m-creator", "m-exec", "design")
	if err := api.dal.PutTask(Task{
		ID: "t-gggg00000002", Title: "a follow-up that already finished",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorMember, ExecutorID: "m-next",
		CreatorID: "m-creator", CreatedTS: 10, UpdatedTS: 20, ClosedTS: 20,
	}); err != nil {
		t.Fatalf("seed terminal dependent: %v", err)
	}
	if err := api.dal.AddTaskDep("t-gggg00000002", blocker.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	rec := closeReport(t, api, blocker.ID, blocker.ID+"-sa", "m-exec", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a FINISHED dependent must not stand in for a handover: %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessage(t, rec); !strings.Contains(msg, HandoffReturnToCreator) {
		t.Fatalf("refusal must still name the ways out: %s", msg)
	}
}

// ── 旁門:任務走到終態的每一把 AGENT 鑰匙 ─────────────────────────────────────
//
// The step-status report is not the only way an agent can drive a task to a
// terminal status. A gate that locks one key and leaves the others open is
// WORSE than no gate: it makes everybody believe the ball is caught. These pin
// every remaining agent-reachable terminal door.
//
// (owner terminate is deliberately NOT here: routes.go marks it principalOwner
// + MCPExclude — it is the owner's escape hatch, not an agent's key, and
// TestSentinelOwnerTerminateIsNotGated pins that it stays unguarded.)

// 🔴 The worst of the two, because the gate's own refusal used to send the
// executor down it: submit_plan re-derives the task status from the new step
// set and auto-closes when it is all-done. Replan rules keep `done` steps and
// DROP an unfinished card-less step outright, so a refused executor could
// replan to "just the nodes I already finished" and the task closes with
// handoff="" — no 422, no log, no signal.
func TestSubmitPlanCannotBeUsedToCloseAroundTheGate(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-hhhh00000001", "m-creator", "m-exec", "a", "b")
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		if st.Name == "a" {
			st.Status = StepStatusDone
			if err := api.dal.PutTaskStep(st); err != nil {
				t.Fatalf("put step: %v", err)
			}
		}
	}
	// The front door is shut.
	if rec := closeReport(t, api, task.ID, task.ID+"-sb", "m-exec", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("front door must refuse: %d %s", rec.Code, rec.Body.String())
	}

	// The side door: replan down to only the already-finished node.
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{{"name": "a", "dod": "done when done"}}},
			"m-exec", "agent"), task.ID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a replan that would CLOSE the task must be gated exactly like the "+
			"closing step report: %d %s", rec.Code, rec.Body.String())
	}
	after := mustTask(t, api, task.ID)
	if TaskIsTerminal(after.Status) || after.Handoff != HandoffUndeclared {
		t.Fatalf("GATE BYPASSED via submit_plan: status=%q handoff=%q closed_ts=%v",
			after.Status, after.Handoff, after.ClosedTS)
	}
	// The refusal has to be usable, which is more than "well worded" — see
	// replanRefusalProblems and the negative sample pinning it below.
	if problems := replanRefusalProblems(errorMessage(t, rec)); len(problems) > 0 {
		t.Fatalf("the refusal is not usable: %s", strings.Join(problems, "; "))
	}
}

// ── 這道 422 的鑑別力本身要被證明 ────────────────────────────────────────────
//
// The previous version of this assertion checked that the message did NOT
// contain "submit_plan still works". That string never appeared in a 422 body
// at all — it lived in a file-header comment — so the check passed against a
// full revert: dead code inside a test, which nothing else can find (panic
// probes only ever look at production code).
//
// The FIRST attempt at fixing it was also zero-discrimination, and that is the
// more interesting failure: asserting the message contains create_task,
// update_step_status and the three handoff values looks like it pins the fix,
// but the PRE-rework message contained all five of those too, and also never
// said "submit_plan". A stricter-looking assertion that still passes on the
// negative sample is no better than the one it replaced.
//
// So the rule this file now follows: an assertion is only worth having if it
// FAILS when the thing it claims to prove is false. That is not something you
// can eyeball — it has to be executed against the false case. Hence
// historicalReplanRefusal below: the exact 422 text from before the rework, kept
// as a negative sample, with a test that the checker rejects it.
//
// What actually changed in the rework, and therefore what has to be pinned:
//   - route ORDER — the always-available route (update_step_status on a kept
//     unfinished step) now comes first, because the other one is conditional;
//   - the CONDITION is stated — set_task_deps is guarded by callerMayDriveTask,
//     so it 403s unless the successor is assigned to the caller, and a handover
//     is by definition assigned to somebody else.
//
// Naming a route without naming its precondition is how a fail-closed guard
// turns into an outage, which is the failure this whole ticket is about.
func replanRefusalProblems(msg string) []string {
	var problems []string
	if strings.Contains(msg, "submit_plan") {
		problems = append(problems, "mentions submit_plan — the caller is already "+
			"IN submit_plan and it is the door being refused, so that is a loop")
	}
	for _, want := range []string{"update_step_status", "create_task",
		HandoffReturnToCreator, HandoffFollowUp, HandoffNone} {
		if !strings.Contains(msg, want) {
			problems = append(problems, "does not name "+want)
		}
	}
	// Ordering: the route that always works must be offered first. Both markers
	// are present in the old text too — it is their ORDER that changed.
	stepReport := strings.Index(msg, "update_step_status")
	setDeps := strings.Index(msg, "set_task_deps")
	if stepReport >= 0 && setDeps >= 0 && setDeps < stepReport {
		problems = append(problems, "offers the set_task_deps route BEFORE the "+
			"update_step_status route, but set_task_deps is the conditional one")
	}
	// The precondition must be stated, not left for the caller to discover by
	// receiving a 403 they have no way to interpret.
	if strings.Contains(msg, "set_task_deps") && !strings.Contains(msg, "403") {
		problems = append(problems, "offers the set_task_deps route without saying "+
			"it answers 403 unless the successor is assigned to the caller")
	}
	return problems
}

// historicalReplanRefusal is the replan 422 EXACTLY as it read before the
// round-2 rework (commit 74faaca, api_tasks_handoff.go). It is the negative
// sample: everything replanRefusalProblems claims to catch must be caught here,
// or the checker is decoration.
const historicalReplanRefusal = "task 't-x' (T-x) was created by 'm-a' but " +
	"executed by 'm-b': this plan leaves EVERY step done, which CLOSES the task, " +
	"and a closed task can never be replanned. A plan carries no handoff " +
	"declaration, so hand the ball over first, one of two ways: " +
	"(1) create the successor task (create_task) and point its blocked_by " +
	"at this task (set_task_deps) — this gate then stands aside by itself, " +
	"and closing this task releases the successor; or " +
	"(2) keep ONE unfinished step in this plan, then declare the handover " +
	"on the update_step_status report that closes it (handoff='return_to_creator'" +
	" | 'follow_up' + handoff_task_id | 'none' + handoff_note)."

func TestReplanRefusalCheckerRejectsThePreReworkWording(t *testing.T) {
	// Sanity: the sample really does satisfy the WEAK checks, which is exactly
	// why the weak checks were worthless. If this ever stops holding, the
	// negative sample has drifted and the test below proves nothing.
	for _, weak := range []string{"update_step_status", "create_task",
		HandoffReturnToCreator, HandoffFollowUp, HandoffNone} {
		if !strings.Contains(historicalReplanRefusal, weak) {
			t.Fatalf("negative sample no longer contains %q — it has drifted and "+
				"stopped being the thing we are proving discrimination against", weak)
		}
	}
	if strings.Contains(historicalReplanRefusal, "submit_plan") {
		t.Fatalf("negative sample must not contain submit_plan — the point is that " +
			"the old text passed that check too")
	}

	problems := replanRefusalProblems(historicalReplanRefusal)
	if len(problems) == 0 {
		t.Fatalf("ZERO-DISCRIMINATION ASSERTION: replanRefusalProblems accepts the " +
			"PRE-rework 422 wording, so it cannot tell the fix from its absence. " +
			"An assertion that passes when its premise is false is dead code.")
	}
	// And specifically the two things the rework changed, so a checker that
	// happens to reject the sample for some unrelated reason is not enough.
	joined := strings.Join(problems, "; ")
	for _, want := range []string{"BEFORE the", "403"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the checker must reject the old wording for the reason the "+
				"rework exists (%q), got: %s", want, joined)
		}
	}
}

// The live message must satisfy the checker that the old one fails — the
// positive half of the same pin, on the real handler output rather than a
// constant, so a change to handoffGateReason reddens here.
func TestReplanRefusalOffersTheAlwaysAvailableRouteFirstWithItsCondition(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-kkkk00000001", "m-creator", "m-exec", "a", "b")
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		if st.Name == "a" {
			st.Status = StepStatusDone
			if err := api.dal.PutTaskStep(st); err != nil {
				t.Fatalf("put step: %v", err)
			}
		}
	}
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{{"name": "a", "dod": "d"}}},
			"m-exec", "agent"), task.ID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", rec.Code, rec.Body.String())
	}
	msg := errorMessage(t, rec)
	if problems := replanRefusalProblems(msg); len(problems) > 0 {
		t.Fatalf("live refusal is not usable: %s\nmessage was: %s",
			strings.Join(problems, "; "), msg)
	}
	// The 403 warning has to be attached to the route it applies to, not left
	// floating: the caller reads this top-to-bottom and acts on the first thing
	// that looks like an instruction.
	if strings.Index(msg, "set_task_deps") > strings.Index(msg, "403") {
		t.Fatalf("the 403 caveat must come AFTER the route it qualifies, or it "+
			"reads as a caveat on the route above it: %s", msg)
	}
}

// mark_duplicate is the agent's SECOND terminal key (routes.go: principalAgent
// + MCPTool "mark_duplicate") and it calls closeTask directly. The ball on a
// duplicate genuinely goes to the ORIGINAL, so the server declares that itself
// rather than refusing — zero friction, and the semantics stop being silent.
func TestMarkDuplicateDeclaresTheHandoffItselfInsteadOfClosingSilently(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	original := seedHandoffTask(t, api, "t-hhhh00000002", "m-creator", "m-other", "work")
	dup := seedHandoffTask(t, api, "t-hhhh00000003", "m-creator", "m-exec", "work")

	rec := httptest.NewRecorder()
	api.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+dup.ID+"/duplicate",
			map[string]any{"duplicate_of": original.ID}, "m-exec", "agent"), dup.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark_duplicate must stay frictionless: %d %s", rec.Code, rec.Body.String())
	}
	after := mustTask(t, api, dup.ID)
	if after.Status != TaskStatusDuplicated {
		t.Fatalf("want duplicated, got %q", after.Status)
	}
	if after.Handoff != HandoffFollowUp || after.HandoffTaskID != original.ID {
		t.Fatalf("a duplicate's ball is ON THE ORIGINAL and must be recorded as "+
			"such, not left blank: handoff=%q handoff_task_id=%q",
			after.Handoff, after.HandoffTaskID)
	}
}

// ── 新閘的哨兵:證明第二道門沒有把合法的 replan 擋死 ──────────────────────────
//
// The replan gate is the one most able to do damage by over-reach: submit_plan
// is the single most-used write in the system, and every task goes through it.
// Blocking a legal replan would be worse than the hole it closes. Each of these
// is a plan that MUST still land exactly as before.

// The overwhelmingly common case: replanning to a plan that still has work in
// it. It does not close the task, so the gate must never even look at it — on a
// cross-executor task, which is the population the gate DOES ask.
func TestSentinelAnOrdinaryReplanOnACrossTaskIsNeverGated(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-iiii00000001", "m-creator", "m-exec", "a")
	plan := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "a", "dod": "still doing it"},
		{"name": "b", "dod": "and this too"},
	})
	if len(plan.Steps) < 2 {
		t.Fatalf("an ordinary replan must land untouched: %+v", plan.Steps)
	}
	if TaskIsTerminal(mustTask(t, api, task.ID).Status) {
		t.Fatalf("an ordinary replan must not close the task")
	}
}

// A self-created task replanning down to all-done: the executor IS the asker,
// there is nobody to hand back to, so this must close silently exactly as it
// did before the gate existed. (This is the 270-of-392 population.)
func TestSentinelSelfCreatedTaskCanStillReplanItselfClosed(t *testing.T) {
	api := newTasksTestServer(t)
	task := seedHandoffTask(t, api, "t-iiii00000002", "m-exec", "m-exec", "a", "b")
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		if st.Name == "a" {
			st.Status = StepStatusDone
			if err := api.dal.PutTaskStep(st); err != nil {
				t.Fatalf("put step: %v", err)
			}
		}
	}
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{{"name": "a", "dod": "done when done"}}},
			"m-exec", "agent"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("a self-created task must still be able to replan itself shut: %d %s",
			rec.Code, rec.Body.String())
	}
	if mustTask(t, api, task.ID).Status != TaskStatusDone {
		t.Fatalf("want done")
	}
}

// The agent that already did the right thing must never meet the second door
// either: a live successor depending on this task auto-satisfies the gate, and
// the replan close records that fact rather than refusing. Same rule as the
// step-report door — proof the two doors share one verdict, not two copies.
func TestReplanClosesWhenASuccessorAlreadyDependsOnTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-iiii00000003", "m-creator", "m-exec", "a", "b")
	successor := seedHandoffTask(t, api, "t-iiii00000004", "m-exec", "m-next", "carry on")
	if err := api.dal.AddTaskDep(successor.ID, task.ID); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		if st.Name == "a" {
			st.Status = StepStatusDone
			if err := api.dal.PutTaskStep(st); err != nil {
				t.Fatalf("put step: %v", err)
			}
		}
	}
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{{"name": "a", "dod": "done when done"}}},
			"m-exec", "agent"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("a handover that is ALREADY real must not be re-demanded: %d %s",
			rec.Code, rec.Body.String())
	}
	closed := mustTask(t, api, task.ID)
	if closed.Status != TaskStatusDone || closed.Handoff != HandoffFollowUp ||
		closed.HandoffTaskID != successor.ID {
		t.Fatalf("the auto-satisfied handover must be RECORDED on the replan door "+
			"too: status=%q handoff=%q task_id=%q",
			closed.Status, closed.Handoff, closed.HandoffTaskID)
	}
	// half B must still fire from a replan-driven close.
	assertHandoverChat(t, api, "m-next", TaskNo(task.ID))
}

// mark_duplicate must stay a zero-friction call for the population the gate does
// NOT ask (self-created): no declaration is invented on a task nobody has to
// hand anything over on, and the call certainly must not start refusing.
func TestSentinelSelfCreatedMarkDuplicateStaysUntouched(t *testing.T) {
	api := newTasksTestServer(t)
	original := seedHandoffTask(t, api, "t-iiii00000005", "m-exec", "m-exec", "work")
	dup := seedHandoffTask(t, api, "t-iiii00000006", "m-exec", "m-exec", "work")
	rec := httptest.NewRecorder()
	api.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+dup.ID+"/duplicate",
			map[string]any{"duplicate_of": original.ID}, "m-exec", "agent"), dup.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark_duplicate must never refuse: %d %s", rec.Code, rec.Body.String())
	}
	after := mustTask(t, api, dup.ID)
	if after.Status != TaskStatusDuplicated {
		t.Fatalf("want duplicated, got %q", after.Status)
	}
	if after.Handoff != HandoffUndeclared {
		t.Fatalf("a self-created duplicate has nobody to hand back to; the server "+
			"must not invent a declaration: handoff=%q", after.Handoff)
	}
}

// The single most common shape on a cross-executor task: the executor has
// FINISHED some steps and replans to add the next ones. The kept prefix is
// entirely done, so anything that judges the plan on the kept rows alone reads
// it as "all done" and refuses — a 422 on the most ordinary act in the system.
// The projection must include the FRESH steps, which are pending.
func TestSentinelReplanThatKeepsDoneStepsAndAddsMoreIsNeverGated(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-jjjj00000001", "m-creator", "m-exec", "a", "b")
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		if st.Name == "a" {
			st.Status = StepStatusDone
			if err := api.dal.PutTaskStep(st); err != nil {
				t.Fatalf("put step: %v", err)
			}
		}
	}
	// kept = [a(done)] — an all-done prefix — plus two fresh pending steps.
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{
				{"name": "a", "dod": "done when done"},
				{"name": "c", "dod": "next up"},
				{"name": "d", "dod": "and then this"},
			}}, "m-exec", "agent"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("carrying a finished prefix forward while adding new work is the "+
			"most ordinary replan there is; it must never be gated: %d %s",
			rec.Code, rec.Body.String())
	}
	after := mustTask(t, api, task.ID)
	if TaskIsTerminal(after.Status) {
		t.Fatalf("a plan with pending work must not close the task: %+v", after)
	}
}

// 🔴 The FREEZE path of the replan side door — the one the drop path above does
// NOT cover, and the one the second door's projection got wrong.
//
// The replan split has TWO fates for an unfinished existing step: a card-less
// one is DROPPED (covered by TestSubmitPlanCannotBeUsedToCloseAroundTheGate),
// and one whose bound reply card has settled (answered / expired) is KEPT and
// FROZEN to superseded by ReplaceTaskPlan. DeriveTaskStatus skips superseded
// rows entirely, so freezing moves the step set STRICTLY CLOSER to all-done.
//
// A projection built from the pre-write rows therefore reads "still working"
// for a plan that lands "done" — the error is one-directional and it is
// fail-OPEN: the task closes with handoff="", no 422 and no log, which is the
// precise failure this ticket exists to kill. The premise builds itself, too:
// expireWaitingCards settles cards server-side without the owner acting.
func TestSubmitPlanCannotCloseAroundTheGateByFreezingASettledCardStep(t *testing.T) {
	api := newTasksTestServer(t)
	seedActiveMember(t, api, "m-creator")
	task := seedHandoffTask(t, api, "t-jjjj00000001", "m-creator", "m-exec", "a", "b")
	card := answeredCard("rc-jjjj0001", 100, 200)
	if err := api.dal.PutReplyCard(card); err != nil {
		t.Fatalf("put card: %v", err)
	}
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, st := range steps {
		switch st.Name {
		case "a":
			st.Status = StepStatusDone
		case "b":
			// Unfinished, but its card has been answered — the KEPT-and-FROZEN
			// fate, not the dropped one.
			st.Status = StepStatusInProgress
			st.ReplyCardID = card.ID
		}
		if err := api.dal.PutTaskStep(st); err != nil {
			t.Fatalf("put step: %v", err)
		}
	}
	// The front door is shut, exactly as in the card-less case.
	if rec := closeReport(t, api, task.ID, task.ID+"-sb", "m-exec", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("front door must refuse: %d %s", rec.Code, rec.Body.String())
	}

	// The side door: replan down to only the already-finished node. "b" is not
	// named, so it freezes — and the stored timeline derives DONE.
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/plan",
			map[string]any{"steps": []map[string]any{{"name": "a", "dod": "done when done"}}},
			"m-exec", "agent"), task.ID)
	after := mustTask(t, api, task.ID)
	if TaskIsTerminal(after.Status) || after.Handoff != HandoffUndeclared {
		t.Fatalf("GATE BYPASSED via submit_plan freeze path: code=%d status=%q "+
			"handoff=%q closed_ts=%v body=%s", rec.Code, after.Status,
			after.Handoff, after.ClosedTS, rec.Body.String())
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a replan that FREEZES its way to all-done must be gated exactly "+
			"like the closing step report: %d %s", rec.Code, rec.Body.String())
	}
}
