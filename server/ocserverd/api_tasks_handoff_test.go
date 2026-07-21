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
