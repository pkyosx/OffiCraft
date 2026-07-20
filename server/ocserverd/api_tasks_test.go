package main

// api_tasks_test.go — the M3 task system's server-side pins: the pure domain
// derivations (task_no / codename / dedupe key), the agent-reported state
// machine's FULL transition table (both directions of every cell), the gate
// arming (card + step binding + task waiting_owner in one handler), the
// create dedupe (non-terminal hit answers the old task; a terminal twin never
// blocks a reopen), submit_plan's done-step retention, and the terminal-state
// worker release. Handlers are invoked directly (auth lives on the route
// table; the executor guard reads the injected claims).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// ── pure domain derivations ──────────────────────────────────────────────────

func TestTaskNoDerivesFromTheIDPrefix(t *testing.T) {
	if got := TaskNo("t-7d40aabbccdd"); got != "T-7d40" {
		t.Fatalf("TaskNo: want T-7d40, got %q", got)
	}
	if got := TaskNo("t-ab"); got != "T-ab" {
		t.Fatalf("TaskNo short id: want T-ab, got %q", got)
	}
}

func TestCodenameDerivation(t *testing.T) {
	cases := []struct {
		model    string
		existing []string
		want     string
	}{
		{"claude-opus-4-6", nil, "O-1"},
		{"claude-sonnet-4-5", []string{"S-2", "S-11", "O-9"}, "S-12"},
		{"Haiku", []string{"H-3"}, "H-4"},
		{"mystery-model", nil, "X-1"},
		{"opus", []string{"O-7", "O-x", "S-9"}, "O-8"}, // malformed entries skipped
	}
	for _, tc := range cases {
		if got := DeriveCodename(tc.model, tc.existing); got != tc.want {
			t.Fatalf("DeriveCodename(%q, %v): want %q, got %q",
				tc.model, tc.existing, tc.want, got)
		}
	}
}

func TestDedupeKeyValue(t *testing.T) {
	fields := []ManualField{
		{Name: "pr", Required: true, IsKey: true},
		{Name: "note", Required: false, IsKey: false},
		{Name: "repo", Required: false, IsKey: true},
	}
	// Composite: key fields join in DECLARATION order, unit-separated.
	got := DedupeKeyValue(fields, map[string]any{
		"pr": "123", "repo": "officraft", "note": "ignored",
	})
	if got != "123\x1fofficraft" {
		t.Fatalf("composite key: got %q", got)
	}
	// A partially present key still yields a value (empty slot kept).
	if got := DedupeKeyValue(fields, map[string]any{"pr": " 123 "}); got != "123\x1f" {
		t.Fatalf("partial key: got %q", got)
	}
	// No key values at all → no dedupe basis.
	if got := DedupeKeyValue(fields, map[string]any{"note": "x"}); got != "" {
		t.Fatalf("empty key must be \"\", got %q", got)
	}
	// No key fields → never a basis.
	if got := DedupeKeyValue([]ManualField{{Name: "a"}}, map[string]any{"a": "v"}); got != "" {
		t.Fatalf("keyless manual must derive \"\", got %q", got)
	}
	// Non-string values render as JSON literals.
	if got := DedupeKeyValue([]ManualField{{Name: "n", IsKey: true}},
		map[string]any{"n": 7.0}); got != "7" {
		t.Fatalf("numeric key: got %q", got)
	}
	// Field↔input matching is case/space insensitive (the review-pr-seth bug:
	// manual "PR Link" vs input "pr link" used to miss → empty key → no dedupe).
	keyField := []ManualField{{Name: "PR Link", IsKey: true}}
	for _, in := range []map[string]any{
		{"pr link": "https://x/1"},
		{"PR LINK": "https://x/1"},
		{"  pr link  ": "https://x/1"},
	} {
		if got := DedupeKeyValue(keyField, in); got != "https://x/1" {
			t.Fatalf("normalized match %v: got %q", in, got)
		}
	}
	// The VALUE is trimmed but never case-folded — a URL is case-sensitive.
	if got := DedupeKeyValue(keyField,
		map[string]any{"pr link": "https://X/1"}); got != "https://X/1" {
		t.Fatalf("value must not be case-folded: got %q", got)
	}
}

func TestNormalizeFieldKey(t *testing.T) {
	cases := map[string]string{
		"PR Link":    "pr link",
		"  PR Link ": "pr link",
		"pr link":    "pr link",
		"PR  Link":   "pr  link", // inner double space is preserved (distinct)
	}
	for in, want := range cases {
		if got := normalizeFieldKey(in); got != want {
			t.Fatalf("normalizeFieldKey(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestNormalizeInputsFirstWinsDeterministically(t *testing.T) {
	// "PR Link" sorts before "pr link"; first-wins keeps it, the later collider's
	// ORIGINAL name is reported. Determinism must not depend on map iteration.
	for i := 0; i < 20; i++ {
		norm, collisions := NormalizeInputs(map[string]any{
			"PR Link": "kept", "pr link": "dropped", "note": "n",
		})
		if norm["pr link"] != "kept" {
			t.Fatalf("first-wins broke: %v", norm)
		}
		if len(collisions) != 1 || collisions[0] != "pr link" {
			t.Fatalf("collision report: %v", collisions)
		}
	}
	if norm, coll := NormalizeInputs(nil); len(norm) != 0 || coll != nil {
		t.Fatalf("nil inputs: norm=%v coll=%v", norm, coll)
	}
}

func TestInputValueMissing(t *testing.T) {
	cases := []struct {
		v    any
		ok   bool
		want bool
	}{
		{nil, false, true},   // absent
		{nil, true, true},    // present but null
		{"", true, true},     // empty string
		{"   ", true, true},  // whitespace-only
		{"x", true, false},   // real string
		{0.0, true, false},   // numeric zero is a value
		{false, true, false}, // bool false is a value
	}
	for _, c := range cases {
		if got := InputValueMissing(c.v, c.ok); got != c.want {
			t.Fatalf("InputValueMissing(%#v, %v): want %v, got %v",
				c.v, c.ok, c.want, got)
		}
	}
}

// ── the agent state machines: FULL transition tables ─────────────────────────

func TestCanAgentTaskTransitionFullTable(t *testing.T) {
	statuses := []string{
		TaskStatusNotStarted, TaskStatusInProgress, TaskStatusWaitingOwner,
		TaskStatusWaitingExternal, TaskStatusDone, TaskStatusTerminated,
		TaskStatusDuplicated,
	}
	// duplicated is a terminal status but NOT on the agent status-report table:
	// it is reached only through the dedicated mark_duplicate action, so every
	// pair touching it must be false here (the loop asserts it against `legal`).
	legal := map[[2]string]bool{
		{TaskStatusNotStarted, TaskStatusInProgress}:      true,
		{TaskStatusInProgress, TaskStatusWaitingExternal}: true,
		{TaskStatusWaitingExternal, TaskStatusInProgress}: true,
		{TaskStatusInProgress, TaskStatusDone}:            true,
	}
	// waiting_owner is off BOTH sides now (T-68b7): the card lifecycle owns the
	// entry (open_gate / create_reply_card) and the exit (the answer restores
	// in_progress). Neither {* → waiting_owner} nor {waiting_owner → *} is a
	// legal agent report — the loop below asserts every waiting_owner pair false.
	for _, from := range statuses {
		for _, to := range statuses {
			want := legal[[2]string{from, to}]
			if got := CanAgentTaskTransition(from, to); got != want {
				t.Fatalf("task %s -> %s: want %v, got %v", from, to, want, got)
			}
		}
	}
}

func TestCanAgentStepTransitionFullTable(t *testing.T) {
	statuses := []string{
		StepStatusPending, StepStatusInProgress, StepStatusWaitingOwner,
		StepStatusWaitingExternal, StepStatusDone, StepStatusSuperseded,
	}
	legal := map[[2]string]bool{
		{StepStatusPending, StepStatusInProgress}:         true,
		{StepStatusInProgress, StepStatusDone}:            true,
		{StepStatusInProgress, StepStatusWaitingExternal}: true, // T-9ca5: step blocks on the outside world
		{StepStatusWaitingExternal, StepStatusInProgress}: true, // T-9ca5: external condition landed
	}
	// Step twin of the task table: waiting_owner is off both sides (T-68b7) —
	// the card answer restores the step to in_progress, not an agent report.
	// waiting_external IS agent-reportable (T-9ca5, unlike waiting_owner).
	for _, from := range statuses {
		for _, to := range statuses {
			want := legal[[2]string{from, to}]
			if got := CanAgentStepTransition(from, to); got != want {
				t.Fatalf("step %s -> %s: want %v, got %v", from, to, want, got)
			}
		}
	}
}

// ── handler-level fixtures ───────────────────────────────────────────────────

func newTasksTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "tasks-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return newAPIServer(NewDAL(db), NewHub(), []byte("tasks-test-secret"), 3600,
		assetRoot(t.TempDir()))
}

// taskReq builds a claims-stamped request (sub/scope = the verified token the
// auth middleware would have stashed).
func taskReq(t *testing.T, method, path string, body any, sub, scope string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	claims := map[string]any{"sub": sub, "scope": scope}
	return req.WithContext(
		context.WithValue(req.Context(), claimsContextKey, claims))
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response (%d %s): %v", rec.Code, rec.Body.String(), err)
	}
	return out
}

// createAdHocTask creates one member-executed ad-hoc task via the handler.
func createAdHocTask(t *testing.T, api *apiServer, executor string) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "unit task", "executor_member_id": executor},
		executor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskCreateResultDTO](t, rec).Task
}

// submitPlan replaces the plan as the executor and returns the task view.
func submitPlan(t *testing.T, api *apiServer, taskID, executor string, steps []map[string]any) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/plan",
			map[string]any{"steps": steps}, executor, "agent"),
		taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit plan: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec)
}

// reportStepStatus posts one step status report as the executor and returns the
// recorder (the caller asserts the code). waitingReason rides along only when
// non-empty (required when entering waiting_external, T-9ca5).
func reportStepStatus(t *testing.T, api *apiServer, taskID, stepID, executor, status, waitingReason string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"status": status}
	if waitingReason != "" {
		body["waiting_reason"] = waitingReason
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/status",
			body, executor, "agent"),
		taskID, stepID)
	return rec
}

// startFirstStep reports the task's first step in_progress, deriving the task to
// in_progress (T-9ca5: task status is derived from its steps). Call it AFTER
// submitPlan — the steps must exist first.
func startFirstStep(t *testing.T, api *apiServer, taskID, executor string) {
	t.Helper()
	task, err := api.dal.GetTask(taskID)
	if err != nil || task == nil {
		t.Fatalf("startFirstStep: load task: %v %v", task, err)
	}
	steps, err := api.dal.ListTaskSteps(taskID)
	if err != nil {
		t.Fatalf("startFirstStep: list steps: %v", err)
	}
	if len(steps) == 0 {
		t.Fatalf("startFirstStep: task %s has no steps (call after submitPlan)", taskID)
	}
	if rec := reportStepStatus(t, api, taskID, steps[0].ID, executor, "in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("startFirstStep: %d %s", rec.Code, rec.Body.String())
	}
}

// driveTaskDone plans a single step and reports it in_progress→done, deriving
// the task to done (auto-close, T-9ca5) — the standard closer for tests that
// just need a closed task (task status is derived, never reported).
func driveTaskDone(t *testing.T, api *apiServer, taskID, executor string) {
	t.Helper()
	view := submitPlan(t, api, taskID, executor, []map[string]any{
		{"name": "work", "dod": "done"},
	})
	stepID := view.Steps[0].ID
	for _, status := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, taskID, stepID, executor, status, ""); rec.Code != http.StatusOK {
			t.Fatalf("driveTaskDone %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
}

// claimTask drives the takeover of a reassigning task via the claim route
// (T-9ca5: POST /api/tasks/{id}/claim — replaces the retired reassigning→in_progress
// status report).
func claimTask(t *testing.T, api *apiServer, taskID, executor string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleClaimTaskApiTasksTaskIdClaimPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/claim", nil, executor, "agent"),
		taskID)
	return rec
}

// ── gate arming ──────────────────────────────────────────────────────────────

func TestOpenGateArmsTheCardBindsTheStepAndFlipsTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "prep", "dod": "ready"},
		{"name": "approve release", "dod": "owner said go", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	gateStep := view.Steps[1]
	if !gateStep.IsGate || gateStep.ReplyCardID != "" {
		t.Fatalf("announced gate must be dashed (no card): %+v", gateStep)
	}

	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "ship it?",
			"options": []string{"ship", "hold"},
		}, "m-exec", "agent"),
		task.ID, gateStep.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if card.Status != replyCardStatusWaiting {
		t.Fatalf("armed card must wait: %+v", card)
	}
	if card.Task == nil || card.Task.ID != task.ID {
		t.Fatalf("card must carry the task ref: %+v", card.Task)
	}

	// The stored card carries the birth marks; the step points back at it.
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored card: %v %v", stored, err)
	}
	if stored.TaskID != task.ID || stored.TaskStepID != gateStep.ID {
		t.Fatalf("card birth marks wrong: %+v", stored)
	}
	step, err := api.dal.GetTaskStep(gateStep.ID)
	if err != nil || step == nil {
		t.Fatalf("step: %v %v", step, err)
	}
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("step must be waiting_owner + bound: %+v", step)
	}
	stored2, err := api.dal.GetTask(task.ID)
	if err != nil || stored2 == nil {
		t.Fatalf("task: %v %v", stored2, err)
	}
	if stored2.Status != TaskStatusWaitingOwner {
		t.Fatalf("task must be waiting_owner, got %s", stored2.Status)
	}

}

// openGateCard arms a gate/plain step via open_gate and returns the served card.
func openGateCard(t *testing.T, api *apiServer, taskID, actor, stepID, summary string) replyCardDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": summary, "options": []string{"a", "b"},
		}, actor, "agent"), taskID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate %s: %d %s", stepID, rec.Code, rec.Body.String())
	}
	return decodeBody[replyCardDTO](t, rec)
}

// answerCard drives the owner's POST answer on a card.
func answerCard(t *testing.T, api *apiServer, cardID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleAnswerReplyCardApiReplyCardsCardIdAnswerPost(rec,
		taskReq(t, "POST", "/x", body, "owner", "owner"), cardID)
	return rec
}

// TestManualWaitingOwnerReportIsRejected pins T-68b7 ②: waiting_owner is NOT an
// agent-reportable status. A report of it on the step is a 400 (not the
// state-machine 409) — waiting_owner is reachable only by opening a card
// (open_gate / create_reply_card), and a rejected report moves nothing. (The
// task-level status report route is gone — task status is derived, T-8449.)
func TestManualWaitingOwnerReportIsRejected(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "build", "dod": "done"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "waiting_owner"},
			"m-exec", "agent"), task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("manual step waiting_owner report must 400, got %d %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "open_gate") ||
		!strings.Contains(rec.Body.String(), "create_reply_card") {
		t.Fatalf("step 400 must name open_gate + create_reply_card: %s",
			rec.Body.String())
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("a rejected report must not move the task, got %s", got.Status)
	}
}

// TestAnsweringACardResumesTheTaskAndStep pins T-68b7 ③⑤ "答卡→回前態": the SERVER
// restores the held step and task from waiting_owner back to in_progress when
// the owner answers the bound card (releaseCardHold); after it the agent
// finishes the work itself (in_progress → done). This supersedes ruling H4.
func TestAnsweringACardResumesTheTaskAndStep(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "go", "is_gate": true},
	})
	gateStep := view.Steps[0]
	startFirstStep(t, api, task.ID, "m-exec")
	card := openGateCard(t, api, task.ID, "m-exec", gateStep.ID, "go?")

	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	step, _ := api.dal.GetTaskStep(gateStep.ID)
	if step.Status != StepStatusInProgress {
		t.Fatalf("answered card must restore the step to in_progress, got %s", step.Status)
	}
	if step.ReplyCardID != card.ID {
		t.Fatalf("the step keeps the card linkage after resume: %+v", step)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("answered card must restore the task to in_progress, got %s", got.Status)
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "done"}, "m-exec", "agent"),
		task.ID, gateStep.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("the agent advances the resumed step to done: %d %s",
			rec.Code, rec.Body.String())
	}
}

// TestAnsweringACardOnATerminatedOrDoneTaskIsRejected pins the T-f571 fix
// (T-68b7 補審): closeTask (terminate() AND the agent's in_progress→done
// report) closes the task WITHOUT touching a still-bound waiting card, so the
// card is orphaned on a task that is already done/terminated. The answer
// route must reject it (409) rather than flip it to answered and have
// releaseCardHold bump the closed task's UpdatedTS back to
// the cockpit's "recently updated" top — and it must leave the card, step,
// and task exactly as they were.
func TestAnsweringACardOnATerminatedOrDoneTaskIsRejected(t *testing.T) {
	for _, status := range []string{TaskStatusTerminated, TaskStatusDone} {
		t.Run(status, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "m-exec")
			view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
				{"name": "approve", "dod": "go", "is_gate": true},
			})
			gateStep := view.Steps[0]
			startFirstStep(t, api, task.ID, "m-exec")
			card := openGateCard(t, api, task.ID, "m-exec", gateStep.ID, "go?")

			// closeTask is the shared terminal side-effect helper behind both
			// the owner's terminate() and an eventual in_progress→done agent
			// report; call it directly (same package) to reach BOTH terminal
			// branches of TaskIsTerminal without fighting the SPEC §3.2 guard
			// that already blocks an agent report out of waiting_owner.
			stored, err := api.dal.GetTask(task.ID)
			if err != nil || stored == nil {
				t.Fatalf("task: %v %v", stored, err)
			}
			if err := api.closeTask(stored, status, nowSecs(), "test"); err != nil {
				t.Fatalf("closeTask: %v", err)
			}
			beforeUpdatedTS := stored.UpdatedTS

			if rec := answerCard(t, api, card.ID,
				map[string]any{"option_idx": 0}); rec.Code != http.StatusConflict {
				t.Fatalf("answering an orphaned card on a %s task must 409, got %d %s",
					status, rec.Code, rec.Body.String())
			}

			storedCard, err := api.dal.GetReplyCard(card.ID)
			if err != nil || storedCard == nil {
				t.Fatalf("card: %v %v", storedCard, err)
			}
			if storedCard.Status != replyCardStatusWaiting {
				t.Fatalf("rejected answer must not flip the card, got %s", storedCard.Status)
			}
			step, err := api.dal.GetTaskStep(gateStep.ID)
			if err != nil || step == nil {
				t.Fatalf("step: %v %v", step, err)
			}
			if step.Status != StepStatusWaitingOwner {
				t.Fatalf("rejected answer must not move the orphaned step, got %s", step.Status)
			}
			got, err := api.dal.GetTask(task.ID)
			if err != nil || got == nil {
				t.Fatalf("task: %v %v", got, err)
			}
			if got.Status != status {
				t.Fatalf("rejected answer must not move the task off %s, got %s", status, got.Status)
			}
			if got.UpdatedTS != beforeUpdatedTS {
				t.Fatalf("rejected answer must not bump the closed task's UpdatedTS (T-68b7 float-back), before=%v after=%v",
					beforeUpdatedTS, got.UpdatedTS)
			}
		})
	}
}

// TestAnsweringOneOfTwoCardsKeepsTheTaskWaiting pins the SPEC §3.2 exit guard
// (one task, many cards): answering one bound card restores its own step but
// leaves the TASK in waiting_owner until the LAST bound card is answered.
func TestAnsweringOneOfTwoCardsKeepsTheTaskWaiting(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "q1", "dod": "d1"},
		{"name": "q2", "dod": "d2"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	card1 := openGateCard(t, api, task.ID, "m-exec", view.Steps[0].ID, "q1?")
	card2 := openGateCard(t, api, task.ID, "m-exec", view.Steps[1].ID, "q2?")
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusWaitingOwner {
		t.Fatalf("two armed cards: task must be waiting_owner, got %s", got.Status)
	}

	// Answer the first: its step resumes, but the task stays waiting (card2).
	if rec := answerCard(t, api, card1.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer 1: %d %s", rec.Code, rec.Body.String())
	}
	if s0, _ := api.dal.GetTaskStep(view.Steps[0].ID); s0.Status != StepStatusInProgress {
		t.Fatalf("answered step 1 must resume, got %s", s0.Status)
	}
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusWaitingOwner {
		t.Fatalf("task must stay waiting_owner while card2 waits, got %s", got.Status)
	}

	// Answer the last: now the task resumes too.
	if rec := answerCard(t, api, card2.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer 2: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusInProgress {
		t.Fatalf("last answer must restore the task, got %s", got.Status)
	}
}

// TestOpenGateArmsANonGatePlainStep pins the fix for the "open_gate cannot
// raise a plain node" report: open_gate on an is_gate=false, not-done step of a
// live task is a legitimate ad-hoc 請示. It arms the step exactly as
// create_reply_card's auto-bind would (waiting_owner + bound card + task
// follows) — the two card-open paths agree (armStepWithCard). is_gate is a
// plan-declared property and is NOT flipped: the step becomes a card-carrying
// plain step, the state resumeGateState already folds.
func TestOpenGateArmsANonGatePlainStep(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "build", "dod": "compiles"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	plainStep := view.Steps[0]
	if plainStep.IsGate {
		t.Fatalf("precondition: step must be a plain (non-gate) node: %+v", plainStep)
	}

	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "which cloud?", "options": []string{"aws", "gcp"},
		}, "m-exec", "agent"),
		task.ID, plainStep.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate on a plain step must 200, got %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if card.Status != replyCardStatusWaiting {
		t.Fatalf("armed card must wait: %+v", card)
	}

	step, err := api.dal.GetTaskStep(plainStep.ID)
	if err != nil || step == nil {
		t.Fatalf("step: %v %v", step, err)
	}
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("plain step must be waiting_owner + bound: %+v", step)
	}
	// is_gate stays false — open_gate arms, it does not rewrite the plan shape.
	if step.IsGate {
		t.Fatalf("open_gate must not flip is_gate on a plain step: %+v", step)
	}
	stored, err := api.dal.GetTask(task.ID)
	if err != nil || stored == nil {
		t.Fatalf("task: %v %v", stored, err)
	}
	if stored.Status != TaskStatusWaitingOwner {
		t.Fatalf("task must follow into waiting_owner, got %s", stored.Status)
	}
}

func TestExecutorGuardDeniesAForeignAgentButAdmitsAdminCapability(t *testing.T) {
	// Hosted on the step-status report route (update_step_status) — the former
	// host, the retired task-status report route, is gone (T-8449). The guard
	// contract is unchanged: a foreign plain agent is turned away at 403, while
	// owner scope (admin capability) passes the executor guard and reaches the
	// handler proper.
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "work", "dod": "done"},
	})
	stepID := view.Steps[0].ID
	// A foreign plain agent is 403.
	rec := reportStepStatus(t, api, task.ID, stepID, "m-intruder", "in_progress", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign agent must 403, got %d %s", rec.Code, rec.Body.String())
	}
	// Owner scope (admin capability) passes the guard and the report lands.
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "in_progress"},
			"owner", "owner"),
		task.ID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner must pass the guard and land the report (200), got %d %s",
			rec.Code, rec.Body.String())
	}
}

// ── create dedupe (H1/H2) ────────────────────────────────────────────────────

func seedManualWithKey(t *testing.T, api *apiServer, typeKey string) {
	t.Helper()
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey:  typeKey,
		Fields:   `[{"name":"pr","required":true,"is_key":true}]`,
		Assignee: `{"kind":"member","member_id":"m-exec"}`,
	}); err != nil {
		t.Fatalf("seed manual: %v", err)
	}
}

func createTypedTask(t *testing.T, api *apiServer, typeKey, pr string) (taskCreateResultDTO, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{
			"title": "review " + pr, "type_key": typeKey,
			"inputs": map[string]any{"pr": pr},
		}, "m-exec", "agent"))
	if rec.Code != http.StatusOK {
		return taskCreateResultDTO{}, rec.Code
	}
	return decodeBody[taskCreateResultDTO](t, rec), rec.Code
}

func TestCreateTaskDedupesOnNonTerminalAndReopensPastTerminal(t *testing.T) {
	api := newTasksTestServer(t)
	seedManualWithKey(t, api, "review-pr")

	first, code := createTypedTask(t, api, "review-pr", "123")
	if code != http.StatusOK || first.Deduped {
		t.Fatalf("first create: code=%d deduped=%v", code, first.Deduped)
	}
	// Same key while the task is open → the EXISTING task, deduped:true.
	again, code := createTypedTask(t, api, "review-pr", "123")
	if code != http.StatusOK || !again.Deduped || again.Task.ID != first.Task.ID {
		t.Fatalf("dedupe hit: code=%d deduped=%v id=%s (want %s)",
			code, again.Deduped, again.Task.ID, first.Task.ID)
	}
	// A DIFFERENT key never collides.
	other, code := createTypedTask(t, api, "review-pr", "456")
	if code != http.StatusOK || other.Deduped || other.Task.ID == first.Task.ID {
		t.Fatalf("different key must mint fresh: %+v", other)
	}
	// Close the first task; the same key then mints a FRESH task (H2).
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), first.Task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	reopened, code := createTypedTask(t, api, "review-pr", "123")
	if code != http.StatusOK || reopened.Deduped || reopened.Task.ID == first.Task.ID {
		t.Fatalf("terminal twin must not block a reopen: %+v", reopened)
	}
}

// ── submit_plan keeps done steps ─────────────────────────────────────────────

func TestSubmitPlanReplacesOnlyTheNotDoneSteps(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	// Drive step "one" to done (this also derives the task to in_progress).
	stepOne := v1.Steps[0]
	for _, status := range []string{"in_progress", "done"} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": status},
				"m-exec", "agent"),
			task.ID, stepOne.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("step %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	// Re-plan: the done step survives (ahead), "two" is gone, fresh steps follow.
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "three", "dod": "d3"},
		{"name": "four", "dod": "d4", "parallel_group": "g1"},
		{"name": "five", "dod": "d5", "parallel_group": "g1"},
	})
	if len(v2.Steps) != 4 {
		t.Fatalf("want 4 steps (1 kept + 3 fresh), got %d", len(v2.Steps))
	}
	if v2.Steps[0].ID != stepOne.ID || v2.Steps[0].Status != StepStatusDone {
		t.Fatalf("done step must be kept in front: %+v", v2.Steps[0])
	}
	if v2.Steps[1].Name != "three" || v2.Steps[2].Name != "four" {
		t.Fatalf("fresh plan wrong: %+v", v2.Steps)
	}
	if v2.Steps[2].ParallelGroup != "g1" || v2.Steps[3].ParallelGroup != "g1" {
		t.Fatalf("parallel_group lost: %+v", v2.Steps)
	}
	if v2.ProgressDone != 1 || v2.ProgressTotal != 4 {
		t.Fatalf("progress: want 1/4, got %d/%d", v2.ProgressDone, v2.ProgressTotal)
	}
}

// TestSubmitPlanRelistingDoneStepsDoesNotDuplicate pins the whole-replace-but-
// keep-done contract when the executor RE-LISTS an already-done node in the
// fresh plan (the natural "here is the whole plan again" replan). The done node
// is preserved from the kept prefix — it must NOT also be re-inserted as a
// fresh pending twin (the 5→9 duplication bug). Match is by step name (the plan
// wire carries no id).
func TestSubmitPlanRelistingDoneStepsDoesNotDuplicate(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	// Drive step "one" to done (this also derives the task to in_progress).
	stepOne := v1.Steps[0]
	for _, status := range []string{"in_progress", "done"} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": status},
				"m-exec", "agent"),
			task.ID, stepOne.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("step %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	// Re-plan listing the WHOLE plan back — including the done "one" — plus one
	// genuinely new step. The done node must survive exactly once (the kept
	// prefix), not be duplicated as a fresh pending copy.
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
		{"name": "three", "dod": "d3"},
	})
	if len(v2.Steps) != 3 {
		t.Fatalf("want 3 steps (done 'one' kept once + fresh 'two','three'), got %d: %+v",
			len(v2.Steps), v2.Steps)
	}
	nameCount := map[string]int{}
	for _, s := range v2.Steps {
		nameCount[s.Name]++
	}
	if nameCount["one"] != 1 {
		t.Fatalf("done node 'one' must appear exactly once, got %d: %+v",
			nameCount["one"], v2.Steps)
	}
	// The kept done node keeps its identity (same id + done status), ahead of
	// the fresh plan.
	if v2.Steps[0].ID != stepOne.ID || v2.Steps[0].Status != StepStatusDone {
		t.Fatalf("done step must be kept (same id) in front: %+v", v2.Steps[0])
	}
	if v2.Steps[1].Name != "two" || v2.Steps[1].Status != StepStatusPending {
		t.Fatalf("re-listed 'two' must be a fresh pending, got %+v", v2.Steps[1])
	}
	if v2.Steps[2].Name != "three" {
		t.Fatalf("fresh 'three' must follow, got %+v", v2.Steps[2])
	}
	if v2.ProgressDone != 1 || v2.ProgressTotal != 3 {
		t.Fatalf("progress: want 1/3, got %d/%d", v2.ProgressDone, v2.ProgressTotal)
	}
}

// ── submit_plan: parallel (fork-join) shape guards ───────────────────────────

// TestSubmitPlanParallelShapeGuards pins the three 400s of the parallel write
// gate (gate-in-group / split group / one-lane group) plus the legal
// fork-join round-trip. Plans WITHOUT any parallel_group are untouched by the
// gate — every other test in this file keeps pinning that.
func TestSubmitPlanParallelShapeGuards(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	post := func(steps []map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"steps": steps},
				"m-exec", "agent"),
			task.ID)
		return rec
	}

	// 1. A gate inside a parallel group is refused.
	if rec := post([]map[string]any{
		{"name": "lane a", "dod": "d", "parallel_group": "pg"},
		{"name": "approve", "dod": "d", "parallel_group": "pg", "is_gate": true},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("gate-in-group: want 400, got %d %s", rec.Code, rec.Body.String())
	}

	// 2. A split group (same key, not consecutive) is refused.
	if rec := post([]map[string]any{
		{"name": "lane a", "dod": "d", "parallel_group": "pg"},
		{"name": "solo", "dod": "d"},
		{"name": "lane b", "dod": "d", "parallel_group": "pg"},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("split group: want 400, got %d %s", rec.Code, rec.Body.String())
	}

	// 3. A one-lane group is refused (parallel means at least two).
	if rec := post([]map[string]any{
		{"name": "lonely", "dod": "d", "parallel_group": "pg"},
		{"name": "join", "dod": "d"},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("one-lane group: want 400, got %d %s", rec.Code, rec.Body.String())
	}

	// Nothing landed: the task still has zero steps (400s never half-write).
	if steps, err := api.dal.ListTaskSteps(task.ID); err != nil || len(steps) != 0 {
		t.Fatalf("refused plans must not write: %v %v", steps, err)
	}

	// A legal fork-join plan (two groups + join + trailing gate) lands whole.
	rec := post([]map[string]any{
		{"name": "spec", "dod": "d"},
		{"name": "lane a", "dod": "d", "parallel_group": "pg"},
		{"name": "lane b", "dod": "d", "parallel_group": "pg"},
		{"name": "join", "dod": "d"},
		{"name": "check a", "dod": "d", "parallel_group": "verify"},
		{"name": "check b", "dod": "d", "parallel_group": "verify"},
		{"name": "approve", "dod": "d", "is_gate": true},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("legal fork-join: %d %s", rec.Code, rec.Body.String())
	}
	view := decodeBody[taskDTO](t, rec)
	got := make([]string, 0, len(view.Steps))
	for _, st := range view.Steps {
		got = append(got, st.ParallelGroup)
	}
	want := []string{"", "pg", "pg", "", "verify", "verify", ""}
	if len(got) != len(want) {
		t.Fatalf("steps: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parallel_group round-trip: want %v, got %v", want, got)
		}
	}
}

// TestSubmitPlanParallelReplanValidatesTheCombinedTimeline pins that the
// contiguity check spans the kept done prefix: replanning the still-pending
// lanes of a group is legal (the fresh lanes butt against the kept done
// lane), while re-using the group key later in the plan is refused — the
// stored timeline would fold into two stages (a visual lie).
func TestSubmitPlanParallelReplanValidatesTheCombinedTimeline(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "lane a", "dod": "d", "parallel_group": "pg"},
		{"name": "lane b", "dod": "d", "parallel_group": "pg"},
	})
	// Drive lane a to done; lane b stays pending.
	for _, status := range []string{"in_progress", "done"} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": status},
				"m-exec", "agent"),
			task.ID, v1.Steps[0].ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("step %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	post := func(steps []map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"steps": steps},
				"m-exec", "agent"),
			task.ID)
		return rec
	}
	// Refused: the fresh "pg" lane is separated from the kept done "pg" lane.
	if rec := post([]map[string]any{
		{"name": "solo", "dod": "d"},
		{"name": "lane b2", "dod": "d", "parallel_group": "pg"},
		{"name": "lane b3", "dod": "d", "parallel_group": "pg"},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("split across kept prefix: want 400, got %d %s",
			rec.Code, rec.Body.String())
	}
	// Legal: the rewritten lane butts against the kept done lane (combined
	// count 2 — one kept + one fresh — satisfies the two-lane floor).
	rec := post([]map[string]any{
		{"name": "lane b2", "dod": "d", "parallel_group": "pg"},
		{"name": "join", "dod": "d"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("contiguous replan: %d %s", rec.Code, rec.Body.String())
	}
	view := decodeBody[taskDTO](t, rec)
	if len(view.Steps) != 3 || view.Steps[0].Status != StepStatusDone ||
		view.Steps[1].ParallelGroup != "pg" || view.Steps[2].ParallelGroup != "" {
		t.Fatalf("replanned timeline wrong: %+v", view.Steps)
	}
}

// ── submit_plan: answered-card steps survive a replan as superseded (T-1aea) ─

// driveStepStatus posts one step status report as the executor.
func driveStepStatus(t *testing.T, api *apiServer, taskID, stepID, executor, status string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": status},
			executor, "agent"),
		taskID, stepID)
	return rec
}

// TestSubmitPlanFreezesAnsweredCardStepsAsSuperseded pins the T-1aea core: a
// replan PRESERVES a step whose latest bound card was already answered —
// frozen into the superseded terminal state, in its original timeline slot
// ahead of the fresh plan, finished_ts stamped, card pointer kept — while a
// step whose card still WAITS is replaced as before (the ask lives on in
// chat; answering it afterwards is a safe no-op on the removed step). The
// superseded row counts toward neither progress side and is never the
// current-step candidate of the resume snapshot.
func TestSubmitPlanFreezesAnsweredCardStepsAsSuperseded(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "prep", "dod": "d"},
		{"name": "ask direction", "dod": "owner answered"},
		{"name": "pending ask", "dod": "owner answered"},
	})
	// prep → done.
	for _, status := range []string{"in_progress", "done"} {
		if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID,
			"m-exec", status); rec.Code != http.StatusOK {
			t.Fatalf("prep %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	// "ask direction" gets an ANSWERED card; "pending ask" a still-WAITING one.
	answered := openGateCard(t, api, task.ID, "m-exec", v1.Steps[1].ID, "which way?")
	if rec := answerCard(t, api, answered.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	waiting := openGateCard(t, api, task.ID, "m-exec", v1.Steps[2].ID, "later?")

	// Re-plan with entirely fresh names.
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "build", "dod": "d"},
		{"name": "ship", "dod": "d"},
	})
	names := make([]string, 0, len(v2.Steps))
	for _, st := range v2.Steps {
		names = append(names, st.Name)
	}
	want := []string{"prep", "ask direction", "build", "ship"}
	if len(names) != len(want) {
		t.Fatalf("want steps %v, got %v", want, names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("kept prefix must keep original order ahead of fresh: want %v, got %v",
				want, names)
		}
	}
	frozen := v2.Steps[1]
	if frozen.ID != v1.Steps[1].ID || frozen.Status != StepStatusSuperseded {
		t.Fatalf("answered-card step must be kept (same id) and frozen superseded: %+v", frozen)
	}
	if frozen.FinishedTS <= 0 {
		t.Fatalf("freezing must stamp finished_ts (the freeze moment): %+v", frozen)
	}
	if frozen.ReplyCardID != answered.ID {
		t.Fatalf("the frozen row keeps its card pointer (Q&A history): %+v", frozen)
	}
	if frozen.ReplyCardStatus != replyCardStatusAnswered {
		t.Fatalf("the read-time join must still resolve the answered card: %+v", frozen)
	}
	// The waiting-card step is gone (replaced wholesale, as before).
	if got, _ := api.dal.GetTaskStep(v1.Steps[2].ID); got != nil {
		t.Fatalf("waiting-card step must be removed by the replan: %+v", got)
	}
	// Progress: superseded counts toward NEITHER side — 1 done / 3 total
	// (prep + build + ship), on the DTO and on both count paths.
	if v2.ProgressDone != 1 || v2.ProgressTotal != 3 {
		t.Fatalf("progress: want 1/3 (superseded excluded), got %d/%d",
			v2.ProgressDone, v2.ProgressTotal)
	}
	if prog, err := api.dal.AllTaskStepProgress(); err != nil ||
		prog[task.ID].Done != 1 || prog[task.ID].Total != 3 {
		t.Fatalf("AllTaskStepProgress must exclude superseded: %+v %v",
			prog[task.ID], err)
	}
	// The resume snapshot's current step skips the frozen row: first
	// non-terminal = "build".
	rows, _, err := api.resumeTasksFor("m-exec")
	if err != nil || len(rows) != 1 {
		t.Fatalf("resume rows: %+v %v", rows, err)
	}
	if rows[0].CurrentStepName != "build" {
		t.Fatalf("current step must skip superseded history, got %q",
			rows[0].CurrentStepName)
	}
	// Answering the orphaned waiting card afterwards is a safe no-op on the
	// removed step (releaseCardHold's guards) — 200, and the task resumes
	// in_progress since no other card waits.
	if rec := answerCard(t, api, waiting.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answering the orphaned card must still 200: %d %s",
			rec.Code, rec.Body.String())
	}
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusInProgress {
		t.Fatalf("task must resume in_progress after the last waiting card, got %s",
			got.Status)
	}
}

// TestSubmitPlanRelistingAnsweredCardStepContinuesTheLiveRow pins the
// no-copy rule: when the fresh plan re-lists an answered-card node by name
// (the whole-plan-again replan), the SAME live row continues — same id, same
// status, card pointer intact — with no superseded twin and no pending twin.
func TestSubmitPlanRelistingAnsweredCardStepContinuesTheLiveRow(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "ask direction", "dod": "owner answered"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	card := openGateCard(t, api, task.ID, "m-exec", v1.Steps[0].ID, "which way?")
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "ask direction", "dod": "owner answered"},
		{"name": "execute", "dod": "d"},
	})
	if len(v2.Steps) != 2 {
		t.Fatalf("re-listed node must not duplicate: %+v", v2.Steps)
	}
	cont := v2.Steps[0]
	if cont.ID != v1.Steps[0].ID || cont.Status != StepStatusInProgress {
		t.Fatalf("the live row continues as-is (no freeze, no copy): %+v", cont)
	}
	if cont.ReplyCardID != card.ID {
		t.Fatalf("the continued row keeps its card pointer: %+v", cont)
	}
	if v2.ProgressDone != 0 || v2.ProgressTotal != 2 {
		t.Fatalf("progress: want 0/2, got %d/%d", v2.ProgressDone, v2.ProgressTotal)
	}
}

// TestSubmitPlanFreezesExpiredCardStepsToo: expired is the other settled card
// state (the owner let the ask lapse — terminal, T-1aa4); the step's history
// is preserved exactly like an answered one.
func TestSubmitPlanFreezesExpiredCardStepsToo(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "ask direction", "dod": "owner answered"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	card := openGateCard(t, api, task.ID, "m-exec", v1.Steps[0].ID, "which way?")
	rec := httptest.NewRecorder()
	api.HandleExpireReplyCardApiReplyCardsCardIdExpirePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), card.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "fresh", "dod": "d"},
	})
	if len(v2.Steps) != 2 || v2.Steps[0].ID != v1.Steps[0].ID ||
		v2.Steps[0].Status != StepStatusSuperseded {
		t.Fatalf("expired-card step must freeze superseded ahead of fresh: %+v",
			v2.Steps)
	}
	if v2.Steps[0].ReplyCardStatus != replyCardStatusExpired {
		t.Fatalf("the frozen row still joins its expired card: %+v", v2.Steps[0])
	}
}

// TestSupersededIsTerminalOnEveryWriteFace pins the walls around the frozen
// state: an agent report INTO superseded is a 400 (not its lever — the server
// freezes on submit_plan), any report OUT of it is a 409 (terminal),
// re-arming it via open_gate is a 409, a later replan neither deletes nor
// re-freezes it, and a later plan may honestly re-introduce the same name as
// a NEW pending row (superseded work was not completed — no done-style
// dedupe).
func TestSupersededIsTerminalOnEveryWriteFace(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "ask direction", "dod": "owner answered"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	// Reporting superseded on a LIVE step is a 400 with the server-lever hint.
	if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID, "m-exec",
		"superseded"); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "not agent-reportable") {
		t.Fatalf("report INTO superseded must 400: %d %s", rec.Code, rec.Body.String())
	}
	card := openGateCard(t, api, task.ID, "m-exec", v1.Steps[0].ID, "which way?")
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "fresh", "dod": "d"},
	})
	frozenID := v2.Steps[0].ID
	if v2.Steps[0].Status != StepStatusSuperseded {
		t.Fatalf("precondition: frozen row: %+v", v2.Steps[0])
	}
	// Start the fresh step so the task derives to in_progress — open_gate checks
	// the task status before the step's superseded terminal, so the task must be
	// armable for the superseded-specific 409 below to be the one that fires.
	if rec := reportStepStatus(t, api, task.ID, v2.Steps[1].ID, "m-exec",
		"in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("start fresh: %d %s", rec.Code, rec.Body.String())
	}
	// OUT of superseded: a state-machine 409.
	if rec := driveStepStatus(t, api, task.ID, frozenID, "m-exec",
		"in_progress"); rec.Code != http.StatusConflict {
		t.Fatalf("report OUT of superseded must 409: %d %s", rec.Code, rec.Body.String())
	}
	// Re-arming the frozen row is a 409 (the card pointer is audit trail).
	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "again?", "options": []string{"a", "b"},
		}, "m-exec", "agent"), task.ID, frozenID)
	if rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "superseded") {
		t.Fatalf("open_gate on superseded must 409: %d %s", rec.Code, rec.Body.String())
	}
	// A LATER replan re-listing the frozen NAME mints a fresh pending twin —
	// the frozen row stays beside it as history, frozen exactly once.
	v3 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "ask direction", "dod": "owner answered"},
	})
	if len(v3.Steps) != 2 {
		t.Fatalf("frozen history + fresh twin expected: %+v", v3.Steps)
	}
	if v3.Steps[0].ID != frozenID || v3.Steps[0].Status != StepStatusSuperseded {
		t.Fatalf("frozen row must survive later replans untouched: %+v", v3.Steps[0])
	}
	if v3.Steps[1].ID == frozenID || v3.Steps[1].Status != StepStatusPending ||
		v3.Steps[1].Name != "ask direction" {
		t.Fatalf("re-introduced node must be a NEW pending row: %+v", v3.Steps[1])
	}
}

// ── terminal side effects: worker release ────────────────────────────────────

func TestTerminalStatesReleaseTheBoundWorker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(t *testing.T, api *apiServer, taskID, executor string)
	}{
		{"terminate", func(t *testing.T, api *apiServer, taskID, _ string) {
			rec := httptest.NewRecorder()
			api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
				taskReq(t, "POST", "/x", nil, "owner", "owner"), taskID)
			if rec.Code != http.StatusOK {
				t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
			}
		}},
		{"done", func(t *testing.T, api *apiServer, taskID, executor string) {
			driveTaskDone(t, api, taskID, executor)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "ow-worker1")
			if err := api.dal.PutOutsourceWorker(OutsourceWorker{
				ID: "ow-worker1", Codename: "O-1", Model: "opus",
				Effort: "high", TaskID: task.ID, Status: WorkerStatusActive,
				CreatedTS: 1,
			}); err != nil {
				t.Fatalf("seed worker: %v", err)
			}
			tc.close(t, api, task.ID, "ow-worker1")
			w, err := api.dal.GetOutsourceWorker("ow-worker1")
			if err != nil || w == nil {
				t.Fatalf("worker: %v %v", w, err)
			}
			if w.Status != WorkerStatusReleased || w.ReleasedTS == 0 {
				t.Fatalf("worker must be released with a stamp: %+v", w)
			}
			task2, err := api.dal.GetTask(task.ID)
			if err != nil || task2 == nil || task2.ClosedTS == 0 {
				t.Fatalf("closed_ts must stamp: %+v %v", task2, err)
			}
			// A closed task refuses every later agent push (set_priority here —
			// the task-status report route is gone, T-8449).
			rec := setPriority(t, api, task.ID, "ow-worker1", "agent", "high")
			if rec.Code != http.StatusConflict {
				t.Fatalf("post-terminal push must 409, got %d", rec.Code)
			}
		})
	}
}

// ── the worker claim (identity-locked) ───────────────────────────────────────

func TestGetMyTaskClaimsAndActivates(t *testing.T) {
	api := newTasksTestServer(t)
	seedManualWithKey(t, api, "review-pr")
	created, code := createTypedTask(t, api, "review-pr", "77")
	if code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-claimer", Codename: "S-1", Model: "sonnet",
		TaskID: created.Task.ID, Status: WorkerStatusAssigned, CreatedTS: 1,
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	rec := httptest.NewRecorder()
	api.HandleGetMyTaskApiSelfTaskGet(rec,
		taskReq(t, "GET", "/api/self/task", nil, "ow-claimer", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[myTaskDTO](t, rec)
	if got.Task.ID != created.Task.ID {
		t.Fatalf("claimed wrong task: %+v", got.Task)
	}
	if got.Manual == nil || got.Manual.TypeKey != "review-pr" {
		t.Fatalf("claim must carry the manual snapshot: %+v", got.Manual)
	}
	w, _ := api.dal.GetOutsourceWorker("ow-claimer")
	if w.Status != WorkerStatusActive {
		t.Fatalf("first claim must flip assigned -> active: %+v", w)
	}
	// A caller with no worker row is a 404.
	rec = httptest.NewRecorder()
	api.HandleGetMyTaskApiSelfTaskGet(rec,
		taskReq(t, "GET", "/api/self/task", nil, "m-member", "agent"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("memberless claim must 404, got %d", rec.Code)
	}
}

// ── waiting_external requires a reason ───────────────────────────────────────

// TestWaitingExternalRequiresAReasonAndClearsOnExit pins the T-9ca5 down-push:
// waiting_external is now a STEP-level report requiring a non-blank
// waiting_reason (422 otherwise); the task DERIVES to waiting_external and its
// display waiting_reason mirrors that step's reason. Leaving the step clears it.
func TestWaitingExternalRequiresAReasonAndClearsOnExit(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "integrate", "dod": "vendor confirms"},
	})
	stepID := view.Steps[0].ID
	startFirstStep(t, api, task.ID, "m-exec")

	if rec := reportStepStatus(t, api, task.ID, stepID, "m-exec",
		"waiting_external", ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing waiting_reason must 422, got %d %s", rec.Code, rec.Body.String())
	}
	rec := reportStepStatus(t, api, task.ID, stepID, "m-exec",
		"waiting_external", "vendor sandbox pending")
	if rec.Code != http.StatusOK {
		t.Fatalf("enter waiting_external: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskDTO](t, rec); got.Status != TaskStatusWaitingExternal ||
		got.WaitingReason != "vendor sandbox pending" {
		t.Fatalf("task must derive waiting_external + mirror the step's reason: %+v", got)
	}
	rec = reportStepStatus(t, api, task.ID, stepID, "m-exec", "in_progress", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("exit waiting_external: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskDTO](t, rec); got.Status != TaskStatusInProgress ||
		got.WaitingReason != "" {
		t.Fatalf("reason must clear on exit and the task resume in_progress: %+v", got)
	}
}

// ── terminal side effects: the task-close nudge band (spec/sse.md §8) ────────

// popTaskCloseFrames drains every buffered frame off l and returns the
// task-close band frames (bare data: events — no id: line).
func popTaskCloseFrames(t *testing.T, l *hubListener) []string {
	t.Helper()
	var out []string
	for {
		frame := l.pop()
		if frame == nil {
			return out
		}
		text := string(frame)
		if !strings.Contains(text, `"topic":"task-close"`) {
			continue
		}
		if strings.Contains(text, "id: ") {
			t.Fatalf("a directed nudge must carry no id line: %q", text)
		}
		out = append(out, text)
	}
}

func TestTaskCloseNudgeRidesTheExecutorsConnectionOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(t *testing.T, api *apiServer, taskID string)
	}{
		{"done", func(t *testing.T, api *apiServer, taskID string) {
			driveTaskDone(t, api, taskID, "m-exec")
		}},
		{"terminated", func(t *testing.T, api *apiServer, taskID string) {
			rec := httptest.NewRecorder()
			api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
				taskReq(t, "POST", "/x", nil, "owner", "owner"), taskID)
			if rec.Code != http.StatusOK {
				t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			seedManualWithKey(t, api, "review-pr")
			created, code := createTypedTask(t, api, "review-pr", "123")
			if code != http.StatusOK {
				t.Fatalf("create: %d", code)
			}
			executor, err := api.hub.Connect("m-exec", "")
			if err != nil {
				t.Fatal(err)
			}
			owner, err := api.hub.Connect("", "")
			if err != nil {
				t.Fatal(err)
			}
			tc.close(t, api, created.Task.ID)
			frames := popTaskCloseFrames(t, executor)
			if len(frames) != 1 {
				t.Fatalf("executor must get exactly one nudge, got %d: %v",
					len(frames), frames)
			}
			if !strings.Contains(frames[0], created.Task.ID) ||
				!strings.Contains(frames[0], "write_task_learnings") ||
				!strings.Contains(frames[0], "report_task_closeout") {
				t.Fatalf("nudge must name the task and both close-out tools: %q",
					frames[0])
			}
			if got := popTaskCloseFrames(t, owner); len(got) != 0 {
				t.Fatalf("the owner fan-out must never carry the nudge: %v", got)
			}
		})
	}
}

func TestTaskCloseNudgeSkipsAdHocTasks(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	executor, err := api.hub.Connect("m-exec", "")
	if err != nil {
		t.Fatal(err)
	}
	driveTaskDone(t, api, task.ID, "m-exec")
	if got := popTaskCloseFrames(t, executor); len(got) != 0 {
		t.Fatalf("an ad-hoc close has no manual — no nudge, got %v", got)
	}
}

// ── §6.3 close-out report ────────────────────────────────────────────────────

// reportCloseout posts one close-out report with the given identity.
func reportCloseout(t *testing.T, api *apiServer, taskID, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/closeout", nil, sub, scope),
		taskID)
	return rec
}

func TestReportTaskCloseoutStampsOnceAndIsIdempotent(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	// An OPEN task has nothing to close out — flat 409, nothing stamps.
	if rec := reportCloseout(t, api, task.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("open task must 409, got %d %s", rec.Code, rec.Body.String())
	}

	driveTaskDone(t, api, task.ID, "m-exec")

	// First report stamps + serves closeout_reported:true.
	rec := reportCloseout(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("close-out: %d %s", rec.Code, rec.Body.String())
	}
	if view := decodeBody[taskDTO](t, rec); !view.CloseoutReported {
		t.Fatalf("first report must serve closeout_reported:true: %+v", view)
	}
	stored, err := api.dal.GetTask(task.ID)
	if err != nil || stored == nil || stored.CloseoutTS <= 0 {
		t.Fatalf("closeout_ts must stamp: %+v %v", stored, err)
	}
	stamp := stored.CloseoutTS

	// A repeat is a 200 no-op: same body shape, the stamp never moves.
	rec = reportCloseout(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat close-out must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if view := decodeBody[taskDTO](t, rec); !view.CloseoutReported {
		t.Fatalf("repeat must still serve closeout_reported:true: %+v", view)
	}
	stored, err = api.dal.GetTask(task.ID)
	if err != nil || stored == nil || stored.CloseoutTS != stamp {
		t.Fatalf("idempotent repeat must not restamp: %v vs %v (%v)",
			stored.CloseoutTS, stamp, err)
	}

	// Unknown task → 404.
	if rec := reportCloseout(t, api, "t-missing", "m-exec", "agent"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task must 404, got %d", rec.Code)
	}
}

func TestReportTaskCloseoutEnforcesTheExecutorGuard(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	driveTaskDone(t, api, task.ID, "m-exec")
	// A foreign agent may not report another's close-out.
	if rec := reportCloseout(t, api, task.ID, "m-other", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign agent must 403, got %d %s", rec.Code, rec.Body.String())
	}
	// Admin capability (owner scope) passes — the §14 convention.
	if rec := reportCloseout(t, api, task.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner-scope close-out must pass, got %d %s",
			rec.Code, rec.Body.String())
	}
}

// ── §6.2 resume-summary task block ───────────────────────────────────────────

func resumeSnapshot(t *testing.T, api *apiServer, sub string) resumeSummaryDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, sub, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[resumeSummaryDTO](t, rec)
}

// resumeSnapshotRaw returns the wake snapshot's RAW serialized body — the
// no-detail-leak assertions grep the actual wire bytes, not a decoded struct.
func resumeSnapshotRaw(t *testing.T, api *apiServer, sub string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, sub, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestResumeSummaryCarriesTheCallersOpenTasksAsLightRows(t *testing.T) {
	api := newTasksTestServer(t)

	// Someone ELSE's task and a CLOSED own task must both stay out.
	createAdHocTask(t, api, "m-other")
	closed := createAdHocTask(t, api, "m-exec")
	driveTaskDone(t, api, closed.ID, "m-exec")

	// The live task: plan with a done step, the current step, a LONG DoD and
	// an armed gate — none of that detail may ride the snapshot (T-3f31 owner
	// ruling: 任務不該包含細節); the row carries the current node NAME plus the
	// detail_chars size of the omitted plan text instead.
	task := createAdHocTask(t, api, "m-exec")
	longDoD := strings.Repeat("驗", 240)
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "prep", "dod": "ready"},
		{"name": "build", "dod": longDoD},
		{"name": "approve", "dod": "owner said go", "is_gate": true},
		{"name": "ship", "dod": "deployed", "is_gate": true},
	})
	// prep executed; build is where the boundary sits.
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "in_progress"},
			"m-exec", "agent"), task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "done"},
			"m-exec", "agent"), task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("step done: %d %s", rec.Code, rec.Body.String())
	}
	// Arm the "approve" gate — the task flips waiting_owner; the light row
	// still lists it (non-terminal) without any gate/step detail.
	rec = httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "go?",
			"options": []string{"go", "hold"},
		}, "m-exec", "agent"), task.ID, view.Steps[2].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)

	snapshot := resumeSnapshot(t, api, "m-exec")
	if len(snapshot.Tasks) != 1 {
		t.Fatalf("exactly the caller's open task must list, got %d: %+v",
			len(snapshot.Tasks), snapshot.Tasks)
	}
	got := snapshot.Tasks[0]
	if got.ID != task.ID || got.TaskNo != TaskNo(task.ID) ||
		got.Status != TaskStatusWaitingOwner {
		t.Fatalf("task identity wrong: %+v", got)
	}
	if got.ProgressDone != 1 || got.ProgressTotal != 4 {
		t.Fatalf("progress must count the executed boundary: %+v", got)
	}
	// The current node rides as id + NAME (the first non-done step).
	if got.CurrentStepID != view.Steps[1].ID || got.CurrentStepName != "build" {
		t.Fatalf("current step id+name must be the first non-done: %+v", got)
	}
	// detail_chars = Σ runes(step name + DoD) — the size of the omitted plan.
	wantChars := 0
	for _, st := range []struct{ name, dod string }{
		{"prep", "ready"}, {"build", longDoD},
		{"approve", "owner said go"}, {"ship", "deployed"},
	} {
		wantChars += len([]rune(st.name)) + len([]rune(st.dod))
	}
	if got.DetailChars != wantChars {
		t.Fatalf("detail_chars: want %d, got %d", wantChars, got.DetailChars)
	}
	// No plan detail leaks onto the wire: the serialized row has no steps key
	// and none of the DoD text.
	raw := resumeSnapshotRaw(t, api, "m-exec")
	if strings.Contains(raw, `"steps"`) || strings.Contains(raw, longDoD[:9]) {
		t.Fatalf("light rows must carry no steps/DoD text: %s", raw)
	}
	// The overview folds the sizes (peek-then-decide).
	ov := snapshot.Overview
	if ov.TasksReturned != 1 || ov.TasksOpenTotal != 1 ||
		ov.TasksDetailChars != wantChars {
		t.Fatalf("overview task sizes wrong: %+v", ov)
	}
	if ov.CardsWaiting != 1 || ov.CardsAnsweredRecent != 0 {
		t.Fatalf("overview must count the caller's waiting card: %+v", ov)
	}
	if ov.ChatCount != len(snapshot.Chat) {
		t.Fatalf("overview chat_count must match the snapshot: %+v", ov)
	}

	// The owner answers the gate card → the caller's card counts fold over.
	storedCard, err := api.dal.GetReplyCard(card.ID)
	if err != nil || storedCard == nil {
		t.Fatalf("card: %v %v", storedCard, err)
	}
	storedCard.Status = replyCardStatusAnswered
	storedCard.AnsweredTS = nowSecs()
	if err := api.dal.PutReplyCard(*storedCard); err != nil {
		t.Fatal(err)
	}
	ov = resumeSnapshot(t, api, "m-exec").Overview
	if ov.CardsWaiting != 0 || ov.CardsAnsweredRecent != 1 {
		t.Fatalf("answered card must move panes in the overview: %+v", ov)
	}
}

func TestResumeSummaryTaskBlockIsBounded(t *testing.T) {
	api := newTasksTestServer(t)
	var ids []string
	for range resumeTasksN + 2 {
		ids = append(ids, createAdHocTask(t, api, "m-exec").ID)
	}
	// Touch the OLDEST task last — recency is by updated_ts, not creation. A
	// priority change is a live write that bumps updated_ts (task status is
	// derived, never reported — T-9ca5).
	if rec := setPriority(t, api, ids[0], "m-exec", "agent", "high"); rec.Code != http.StatusOK {
		t.Fatalf("touch: %d %s", rec.Code, rec.Body.String())
	}
	snapshot := resumeSnapshot(t, api, "m-exec")
	if len(snapshot.Tasks) != resumeTasksN {
		t.Fatalf("the block must cap at %d, got %d", resumeTasksN,
			len(snapshot.Tasks))
	}
	if snapshot.Tasks[0].ID != ids[0] {
		t.Fatalf("most recently UPDATED must lead: want %s, got %s",
			ids[0], snapshot.Tasks[0].ID)
	}
	// The overview reports the TRUE open total past the cap.
	ov := snapshot.Overview
	if ov.TasksReturned != resumeTasksN || ov.TasksOpenTotal != resumeTasksN+2 {
		t.Fatalf("overview must count all open tasks past the cap: %+v", ov)
	}
}

// peekResumeSize calls the peek endpoint (T-7974) for a caller.
func peekResumeSize(t *testing.T, api *apiServer, sub string) resumeSummarySizeDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec,
		taskReq(t, "GET", "/api/resume-summary-size", nil, sub, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[resumeSummarySizeDTO](t, rec)
}

// TestPeekResumeSummarySizeIsLightAndConsistent pins the two-step boot's step
// one (T-7974): the peek carries the SAME overview counts a full resume_summary
// reports (assembled through the shared resumeSnapshotParts, so they cannot
// drift), a derived estimated_total_chars, and NO content of any kind — not the
// chat bodies, not the task rows, not step/DoD detail.
func TestPeekResumeSummarySizeIsLightAndConsistent(t *testing.T) {
	api := newTasksTestServer(t)

	// An open task with a plan (non-zero detail_chars) …
	task := createAdHocTask(t, api, "m-exec")
	longDoD := strings.Repeat("驗", 240)
	submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "build", "dod": longDoD},
		{"name": "ship", "dod": "deployed"},
	})
	// … and a chat message longer than the preview cap, so chat_chars is the
	// truncated size the snapshot would actually carry (not the raw body).
	longBody := strings.Repeat("交接內容", 300) // 1200 runes > resumeChatBodyPreview
	if err := api.dal.PutChat(ChatMessage{
		ID: "c-peek", Sender: "owner", Recipient: "m-exec", Body: longBody, TS: 1.0,
	}); err != nil {
		t.Fatal(err)
	}

	full := resumeSnapshot(t, api, "m-exec")
	peek := peekResumeSize(t, api, "m-exec")

	// The peek's identity + overview equal what the full snapshot reports.
	if peek.Identity == nil || *peek.Identity != "m-exec" {
		t.Fatalf("peek identity: %+v", peek.Identity)
	}
	if peek.Overview != full.Overview {
		t.Fatalf("peek overview must match resume_summary's exactly:\n peek=%+v\n full=%+v",
			peek.Overview, full.Overview)
	}
	// The chat body was truncated: chat_chars is the preview cap + the ellipsis
	// rune, and it matches the runes the full snapshot's body carries.
	if peek.Overview.ChatCount != 1 {
		t.Fatalf("expected the one seeded message, got %+v", peek.Overview)
	}
	wantChatChars := resumeChatBodyPreview + 1 // truncated body + "…"
	if peek.Overview.ChatChars != wantChatChars {
		t.Fatalf("chat_chars: want %d, got %d", wantChatChars, peek.Overview.ChatChars)
	}
	if got := len([]rune(full.Chat[0].Body)); got != wantChatChars {
		t.Fatalf("chat_chars must equal the truncated body runes: overview %d, body %d",
			peek.Overview.ChatChars, got)
	}
	// estimated_total_chars = chat bodies + the plan text the rows omit.
	wantEstimate := peek.Overview.ChatChars + peek.Overview.TasksDetailChars
	if peek.EstimatedTotalChars != wantEstimate {
		t.Fatalf("estimated_total_chars: want %d, got %d",
			wantEstimate, peek.EstimatedTotalChars)
	}
	if peek.Overview.TasksDetailChars == 0 {
		t.Fatalf("the seeded plan must contribute detail_chars: %+v", peek.Overview)
	}

	// The wire body carries the size block ONLY — no content keys, no leaked
	// chat body or plan text.
	rec := httptest.NewRecorder()
	api.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec,
		taskReq(t, "GET", "/api/resume-summary-size", nil, "m-exec", "agent"))
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"chat", "tasks"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("peek must not carry a %q key: %v", k, raw)
		}
	}
	body := rec.Body.String()
	if strings.Contains(body, longDoD[:9]) || strings.Contains(body, longBody[:9]) {
		t.Fatalf("peek must leak no chat/plan content: %s", body)
	}
}

// TestPeekResumeSummarySizeEmptyCallerIsZeroesNotError pins the degrade path:
// a caller with no chat and no tasks peeks zeroes, never an error.
func TestPeekResumeSummarySizeEmptyCallerIsZeroesNotError(t *testing.T) {
	api := newTasksTestServer(t)
	peek := peekResumeSize(t, api, "m-nobody")
	if peek.Overview != (resumeOverviewDTO{}) || peek.EstimatedTotalChars != 0 {
		t.Fatalf("empty caller must peek zeroes: %+v", peek)
	}
	if peek.Note == "" {
		t.Fatalf("peek must carry the guidance note")
	}
}

// TestTaskMessageBodyCarriesTaskNo pins the owner→executor message-box body:
// the visible text is prefixed with the task's display number so the executor
// sees which task the ruling is about (owner 2026-07-14). meta.task_id stays
// the machine linkage.
func TestTaskMessageBodyCarriesTaskNo(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandlePostTaskMessageApiTasksTaskIdMessagePost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/message",
			map[string]any{"body": "  先做 P0 的部分  "}, wireOwnerID, "owner"),
		task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("post message: %d %s", rec.Code, rec.Body.String())
	}
	msg := decodeBody[chatMessageDTO](t, rec)
	if want := "[" + TaskNo(task.ID) + "] 先做 P0 的部分"; msg.Body != want {
		t.Fatalf("body: want %q, got %q", want, msg.Body)
	}
	// The machine linkage is untouched — still in meta.
	if msg.Meta["task_id"] != task.ID {
		t.Fatalf("meta.task_id: want %q, got %v", task.ID, msg.Meta["task_id"])
	}
}

// ── mark_duplicate (T-02c9) ──────────────────────────────────────────────────

// markDuplicate posts one mark_duplicate action with the given identity.
func markDuplicate(t *testing.T, api *apiServer, taskID, duplicateOf, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/duplicate",
			map[string]any{"duplicate_of": duplicateOf}, sub, scope),
		taskID)
	return rec
}

// TestMarkDuplicateClosesTaskPointsAtOriginalAndSkipsNudge pins the happy path:
// the task lands in the duplicated terminal status with duplicate_of set +
// closed_ts stamped, and — unlike done/terminated — NO learnings nudge fires
// down the executor's connection (T-02c9 point 6: a duplicate has no lessons).
func TestMarkDuplicateClosesTaskPointsAtOriginalAndSkipsNudge(t *testing.T) {
	api := newTasksTestServer(t)
	seedManualWithKey(t, api, "review-pr")
	original, code := createTypedTask(t, api, "review-pr", "100")
	if code != http.StatusOK {
		t.Fatalf("create original: %d", code)
	}
	dupCreated, code := createTypedTask(t, api, "review-pr", "101")
	if code != http.StatusOK {
		t.Fatalf("create dup: %d", code)
	}
	executor, err := api.hub.Connect(dupCreated.Task.ExecutorID, "")
	if err != nil {
		t.Fatal(err)
	}
	rec := markDuplicate(t, api, dupCreated.Task.ID, original.Task.ID,
		dupCreated.Task.ExecutorID, "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("mark_duplicate: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[taskDTO](t, rec)
	if got.Status != TaskStatusDuplicated {
		t.Fatalf("status: want duplicated, got %q", got.Status)
	}
	if got.DuplicateOf != original.Task.ID {
		t.Fatalf("duplicate_of: want %q, got %q", original.Task.ID, got.DuplicateOf)
	}
	if got.ClosedTS == nil || *got.ClosedTS <= 0 {
		t.Fatalf("closed_ts must stamp on the terminal transition, got %v", got.ClosedTS)
	}
	if frames := popTaskCloseFrames(t, executor); len(frames) != 0 {
		t.Fatalf("a duplicated close must NOT nudge learnings, got %v", frames)
	}
}

// TestMarkDuplicateGuards pins every rejection that keeps the graph depth-1 and
// the action honest.
func TestMarkDuplicateGuards(t *testing.T) {
	api := newTasksTestServer(t)
	original := createAdHocTask(t, api, "m-exec")
	subject := createAdHocTask(t, api, "m-exec")

	// blank duplicate_of → 422.
	if rec := markDuplicate(t, api, subject.ID, "  ", "m-exec", "agent"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank duplicate_of: want 422, got %d %s", rec.Code, rec.Body.String())
	}
	// self-reference → 409.
	if rec := markDuplicate(t, api, subject.ID, subject.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("self-reference: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// unknown original → 404.
	if rec := markDuplicate(t, api, subject.ID, "t-doesnotexist", "m-exec", "agent"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown original: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// a non-executor non-admin agent → 403 (executor guard).
	if rec := markDuplicate(t, api, subject.ID, original.ID, "m-other", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-executor: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestMarkDuplicateChainGuards pins the two depth-1 invariants: you cannot point
// at a task that is itself duplicated, and you cannot mark a task that is
// already someone else's original.
func TestMarkDuplicateChainGuards(t *testing.T) {
	api := newTasksTestServer(t)
	original := createAdHocTask(t, api, "m-exec")
	first := createAdHocTask(t, api, "m-exec")
	second := createAdHocTask(t, api, "m-exec")

	// first → duplicate of original (original is now an "original").
	if rec := markDuplicate(t, api, first.ID, original.ID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("first mark: %d %s", rec.Code, rec.Body.String())
	}
	// point AT a duplicate (second → first, but first is itself duplicated) → 409.
	if rec := markDuplicate(t, api, second.ID, first.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("pointing at a duplicate: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// mark a task that is already an original (original → second) → 409.
	if rec := markDuplicate(t, api, original.ID, second.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("marking an original: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// an already-terminal task cannot be re-marked → 409.
	if rec := markDuplicate(t, api, first.ID, original.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("re-marking a closed task: want 409, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── set_task_priority (T-0786) ───────────────────────────────────────────────

// setPriority posts one priority change through the handler under the given
// claims (sub/scope), returning the recorder.
func setPriority(t *testing.T, api *apiServer, taskID, sub, scope, priority string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/priority",
			map[string]any{"priority": priority}, sub, scope),
		taskID)
	return rec
}

// TestSetTaskPriorityOwnerSetsEveryValueAndFansTheDelta pins the owner's full
// vocabulary (high|mid|low|frozen — freeze/unfreeze ride the knob) and that
// each change fans the task patch carrying the NEW priority (spec/sse.md §4).
func TestSetTaskPriorityOwnerSetsEveryValueAndFansTheDelta(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	cockpit, err := api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"high", "mid", "low", "frozen"} {
		rec := setPriority(t, api, task.ID, "owner", "owner", p)
		if rec.Code != http.StatusOK {
			t.Fatalf("owner %s: %d %s", p, rec.Code, rec.Body.String())
		}
		if got := decodeBody[taskDTO](t, rec); got.Priority != p {
			t.Fatalf("priority: want %q, got %q", p, got.Priority)
		}
		found := false
		for {
			frame := cockpit.pop()
			if frame == nil {
				break
			}
			text := string(frame)
			if strings.Contains(text, `"topic":"task"`) &&
				strings.Contains(text, `"priority":"`+p+`"`) {
				found = true
			}
		}
		if !found {
			t.Fatalf("no task patch carrying priority %q reached the cockpit", p)
		}
	}
	// The owner unfreezes too (frozen → high).
	if rec := setPriority(t, api, task.ID, "owner", "owner", "high"); rec.Code != http.StatusOK {
		t.Fatalf("owner unfreeze: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetTaskPriorityExecutorMaySetHighMidLow: the executor retunes their OWN
// task within the working range (T-0786).
func TestSetTaskPriorityExecutorMaySetHighMidLow(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	for _, p := range []string{"high", "mid", "low"} {
		rec := setPriority(t, api, task.ID, "m-exec", "agent", p)
		if rec.Code != http.StatusOK {
			t.Fatalf("executor %s: %d %s", p, rec.Code, rec.Body.String())
		}
		if got := decodeBody[taskDTO](t, rec); got.Priority != p {
			t.Fatalf("priority: want %q, got %q", p, got.Priority)
		}
	}
}

// TestSetTaskPriorityFrozenStaysOwnerOnly: a non-owner may neither SET frozen
// nor move a frozen task (leaving frozen IS unfreezing) — both flat 403s.
func TestSetTaskPriorityFrozenStaysOwnerOnly(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	// The executor freezing their own task → 403.
	if rec := setPriority(t, api, task.ID, "m-exec", "agent", "frozen"); rec.Code != http.StatusForbidden {
		t.Fatalf("executor freeze: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner freezes; the executor may not unfreeze (any value → 403).
	if rec := setPriority(t, api, task.ID, "owner", "owner", "frozen"); rec.Code != http.StatusOK {
		t.Fatalf("owner freeze: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setPriority(t, api, task.ID, "m-exec", "agent", "high"); rec.Code != http.StatusForbidden {
		t.Fatalf("executor unfreeze: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner unfreezes fine.
	if rec := setPriority(t, api, task.ID, "owner", "owner", "high"); rec.Code != http.StatusOK {
		t.Fatalf("owner unfreeze: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetTaskPriorityForeignAgentIs403: a plain agent that is not the task's
// executor hits the executor guard.
func TestSetTaskPriorityForeignAgentIs403(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := setPriority(t, api, task.ID, "m-intruder", "agent", "high")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign agent: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "caller is not the task's executor") {
		t.Fatalf("wrong 403 face: %s", rec.Body.String())
	}
}

// TestSetTaskPriorityAdminAgentMayRetuneButNeverFreezes: admin capability
// (role_key=assistant) passes the executor guard on ANY task — but the frozen
// knob stays the owner's, on both sides.
func TestSetTaskPriorityAdminAgentMayRetuneButNeverFreezes(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutMember(Member{
		ID: "m-admin", Kind: KindAssistant, RoleKey: adminRoleKey,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	task := createAdHocTask(t, api, "m-exec")

	for _, p := range []string{"high", "mid", "low"} {
		if rec := setPriority(t, api, task.ID, "m-admin", "agent", p); rec.Code != http.StatusOK {
			t.Fatalf("admin %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := setPriority(t, api, task.ID, "m-admin", "agent", "frozen"); rec.Code != http.StatusForbidden {
		t.Fatalf("admin freeze: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := setPriority(t, api, task.ID, "owner", "owner", "frozen"); rec.Code != http.StatusOK {
		t.Fatalf("owner freeze: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setPriority(t, api, task.ID, "m-admin", "agent", "high"); rec.Code != http.StatusForbidden {
		t.Fatalf("admin unfreeze: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetTaskPriorityTerminalTaskIs409: a closed task's priority is dead
// weight — done, terminated and duplicated all answer 409 (existing wire
// behaviour, kept).
func TestSetTaskPriorityTerminalTaskIs409(t *testing.T) {
	api := newTasksTestServer(t)

	// done
	done := createAdHocTask(t, api, "m-exec")
	driveTaskDone(t, api, done.ID, "m-exec")
	// terminated
	terminated := createAdHocTask(t, api, "m-exec")
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+terminated.ID+"/terminate", nil,
			"owner", "owner"),
		terminated.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	// duplicated
	original := createAdHocTask(t, api, "m-exec")
	duplicated := createAdHocTask(t, api, "m-exec")
	if rec := markDuplicate(t, api, duplicated.ID, original.ID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("mark duplicate: %d %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct{ name, id string }{
		{"done", done.ID}, {"terminated", terminated.ID}, {"duplicated", duplicated.ID},
	} {
		// The owner AND the executor both hit the terminal wall.
		if rec := setPriority(t, api, tc.id, "owner", "owner", "high"); rec.Code != http.StatusConflict {
			t.Fatalf("%s owner: want 409, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
		if rec := setPriority(t, api, tc.id, "m-exec", "agent", "high"); rec.Code != http.StatusConflict {
			t.Fatalf("%s executor: want 409, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// TestSetTaskPriorityValidationFaces: closed-set 400, missing key 422,
// missing task 404 (post-choke identities resolve the target first — the
// guard sits after resolve, same as plan/status).
func TestSetTaskPriorityValidationFaces(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	if rec := setPriority(t, api, task.ID, "m-exec", "agent", "urgent"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad value: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/priority",
			map[string]any{}, "m-exec", "agent"),
		task.ID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing key: want 422, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := setPriority(t, api, "t-missing", "m-exec", "agent", "high"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing task: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestReconcileTaskStatusesOnBoot pins the boot reconcile seam: the one-shot
// alignment pass corrects a genuinely-drifted row to its derived status while
// leaving terminal tasks (not step-derivable) alone.
func TestReconcileTaskStatusesOnBoot(t *testing.T) {
	api := newTasksTestServer(t)
	// A genuinely-drifted row: status not_started but its step is in_progress →
	// reconcile SHOULD correct this to in_progress.
	if err := api.dal.PutTask(Task{
		ID: "t-drift", TypeKey: "tm-x", Title: "drift", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember, ExecutorID: "m-1",
		CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatalf("put drift task: %v", err)
	}
	if err := api.dal.PutTaskStep(TaskStep{
		ID: "t-drift-s0", TaskID: "t-drift", OrderIdx: 0, Name: "work", Status: StepStatusInProgress,
	}); err != nil {
		t.Fatalf("put drift step: %v", err)
	}
	// A terminal control: reconcile must not re-derive it.
	if err := api.dal.PutTask(Task{
		ID: "t-term", TypeKey: "tm-x", Title: "term", Status: TaskStatusTerminated,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember, ExecutorID: "m-1",
		CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatalf("put terminal task: %v", err)
	}
	if err := api.dal.PutTaskStep(TaskStep{
		ID: "t-term-s0", TaskID: "t-term", OrderIdx: 0, Name: "work", Status: StepStatusInProgress,
	}); err != nil {
		t.Fatalf("put terminal step: %v", err)
	}
	if _, err := api.reconcileTaskStatusesOnBoot(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	drift, _ := api.dal.GetTask("t-drift")
	if drift.Status != TaskStatusInProgress {
		t.Fatalf("boot reconcile must correct the drifted task to in_progress, got %q", drift.Status)
	}
	term, _ := api.dal.GetTask("t-term")
	if term.Status != TaskStatusTerminated {
		t.Fatalf("boot reconcile must leave a terminal task untouched, got %q", term.Status)
	}
}
