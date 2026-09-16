package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── shared helpers ───────────────────────────────────────────────────────────

// assertStepOrderContiguous is the ONE place the invariant every writer of the
// step timeline shares is written down: order_idx is the contiguous range
// 0..n-1, in the order the rows come back.
//
// 🔴 IT IS A FUNCTION RATHER THAN A LINE REPEATED IN FOUR TESTS ON PURPOSE. Four
// copies of an assertion are four things that can each be quietly widened, and
// widening one of them reddens nothing. Every writer of the timeline calls THIS
// — insert_step, delete_step, reorder_steps AND submit_plan, which is the one
// that was already shipping.
func assertStepOrderContiguous(t *testing.T, after string, steps []taskStepDTO) {
	t.Helper()
	for i, st := range steps {
		if st.OrderIdx != i {
			idxs := make([]int, len(steps))
			for j, s := range steps {
				idxs[j] = s.OrderIdx
			}
			t.Fatalf("after %s: order_idx must be the contiguous range 0..%d in "+
				"timeline order, got %v (step %q at position %d carries %d)",
				after, len(steps)-1, idxs, st.Name, i, st.OrderIdx)
		}
	}
}

func insertStep(t *testing.T, api *apiServer, taskID, actor string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleInsertTaskStepApiTasksTaskIdStepsPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps", body, actor, "agent"), taskID)
	return rec
}

func deleteStep(t *testing.T, api *apiServer, taskID, stepID, actor string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/delete", nil,
			actor, "agent"), taskID, stepID)
	return rec
}

func reorderSteps(t *testing.T, api *apiServer, taskID, actor string, ids []string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReorderTaskStepsApiTasksTaskIdStepsReorderPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/reorder",
			map[string]any{"step_ids": ids}, actor, "agent"), taskID)
	return rec
}

func writeStepNote(t *testing.T, api *apiServer, taskID, stepID, actor, note string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/x", map[string]any{"note": note}, actor, "agent"),
		taskID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("write note on %s: %d %s", stepID, rec.Code, rec.Body.String())
	}
}

func storedStepNote(t *testing.T, api *apiServer, stepID string) string {
	t.Helper()
	st, err := api.dal.GetTaskStep(stepID)
	if err != nil {
		t.Fatalf("read step %s: %v", stepID, err)
	}
	if st == nil {
		t.Fatalf("step %s is gone", stepID)
	}
	return st.Note
}

func stepNames(steps []taskStepDTO) []string {
	out := make([]string, len(steps))
	for i, st := range steps {
		out[i] = st.Name
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// createDelegatedTask opens a task that somebody OTHER than its executor
// created — the shape the 交棒閘 asks about, and the shape every outsource
// ticket has.
func createDelegatedTask(t *testing.T, api *apiServer, creator, executor string) taskDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	scope := "agent"
	if creator == "owner" {
		scope = "owner"
	}
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "delegated task", "executor_member_id": executor},
		creator, scope))
	if rec.Code != http.StatusOK {
		t.Fatalf("create delegated task: %d %s", rec.Code, rec.Body.String())
	}
	return createdTaskView(t, api, rec)
}

// ── insert_step ──────────────────────────────────────────────────────────────

func TestHandleInsertTaskStepApiTasksTaskIdStepsPost(t *testing.T) {
	t.Run("inserting in front of a step leaves every other step exactly as it was", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "build", "dod": "d1"},
			{"name": "verify", "dod": "d2"},
			{"name": "accept", "dod": "d3", "is_gate": true},
		})
		build, verify, accept := v1.Steps[0], v1.Steps[1], v1.Steps[2]
		// A card binds only to a task that is actually under way.
		startFirstStep(t, api, task.ID, "m-exec")
		writeStepNote(t, api, task.ID, verify.ID, "m-exec", "half of the cases run")
		// The acceptance step is holding a card the owner has NOT answered — the
		// exact row a replan would have destroyed, card and note together.
		card := openCardOnStep(t, api, task.ID, "m-exec", accept.ID, "ship it?")
		writeStepNote(t, api, task.ID, accept.ID, "m-exec", "waiting on the owner")

		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "extra work the owner asked for", "dod": "the new piece is done",
			"before_step_id": accept.ID,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("insert: %d %s", rec.Code, rec.Body.String())
		}
		assertReceiptKeys(t, rec, "task_id", "step_id", "steps_total",
			"progress_done", "progress_total")
		receipt := decodeBody[taskStepInsertReceiptDTO](t, rec)
		if receipt.TaskID != task.ID || receipt.StepsTotal != 4 ||
			receipt.ProgressDone != 0 || receipt.ProgressTotal != 4 {
			t.Fatalf("insert receipt wrong shape: %+v", receipt)
		}

		v2 := getTaskView(t, api, task.ID)
		want := []string{"build", "verify", "extra work the owner asked for", "accept"}
		if got := stepNames(v2.Steps); !sameStrings(got, want) {
			t.Fatalf("timeline: want %v, got %v", want, got)
		}
		assertStepOrderContiguous(t, "insert_step", v2.Steps)
		if v2.Steps[2].ID != receipt.StepID {
			t.Fatalf("receipt step_id %q is not the new row %q",
				receipt.StepID, v2.Steps[2].ID)
		}
		if v2.Steps[2].Status != StepStatusPending {
			t.Fatalf("a new step opens pending, got %q", v2.Steps[2].Status)
		}
		// Nothing else moved: same ids, same statuses, same notes, same card.
		if v2.Steps[0].ID != build.ID || v2.Steps[1].ID != verify.ID ||
			v2.Steps[3].ID != accept.ID {
			t.Fatalf("existing steps must keep their ids: %+v", stepNames(v2.Steps))
		}
		if v2.Steps[3].Status != StepStatusWaitingOwner ||
			v2.Steps[3].ReplyCardID != card.ID {
			t.Fatalf("the waiting gate step must be untouched: %+v", v2.Steps[3])
		}
		if got := storedStepNote(t, api, verify.ID); got != "half of the cases run" {
			t.Fatalf("verify note must survive, got %q", got)
		}
		if got := storedStepNote(t, api, accept.ID); got != "waiting on the owner" {
			t.Fatalf("accept note must survive, got %q", got)
		}
		// And the owner's answer still lands on the step that asked.
		if rec := answerCard(t, api, card.ID,
			map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
			t.Fatalf("answer the untouched card: %d %s", rec.Code, rec.Body.String())
		}
		v3 := getTaskView(t, api, task.ID)
		if v3.Steps[3].ID != accept.ID || v3.Steps[3].Status != StepStatusInProgress {
			t.Fatalf("the answer must restore the SAME step: %+v", v3.Steps[3])
		}
	})

	t.Run("omitting before_step_id appends at the end of the timeline", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "one", "dod": "d1"},
			{"name": "two", "dod": "d2"},
		})
		if rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "three", "dod": "d3",
		}); rec.Code != http.StatusOK {
			t.Fatalf("append: %d %s", rec.Code, rec.Body.String())
		}
		v := getTaskView(t, api, task.ID)
		if got := stepNames(v.Steps); !sameStrings(got, []string{"one", "two", "three"}) {
			t.Fatalf("append must land last, got %v", got)
		}
		assertStepOrderContiguous(t, "insert_step (append)", v.Steps)
	})

	t.Run("a new step cannot be inserted ahead of a finished step", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "one", "dod": "d1"},
			{"name": "two", "dod": "d2"},
		})
		done := v1.Steps[0]
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, done.ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "sneak", "dod": "d", "before_step_id": done.ID,
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("inserting ahead of finished work must 409, got %d %s",
				rec.Code, rec.Body.String())
		}
		want := "step '" + done.ID + "' is already done — a new step cannot be " +
			"inserted ahead of finished work; name an unfinished step, or omit " +
			"before_step_id to append at the end of the plan"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 2 {
			t.Fatalf("a refused insert must write nothing, got %v", stepNames(v.Steps))
		}
	})

	t.Run("an unknown before_step_id is a 404 and writes nothing", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d"}})
		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "x", "dod": "d", "before_step_id": "ts-nosuchstep",
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown anchor must 404, got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message,
			"step 'ts-nosuchstep' not found"; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 1 {
			t.Fatalf("a refused insert must write nothing, got %v", stepNames(v.Steps))
		}
	})

	t.Run("a blank name or definition of done is refused", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d"}})
		rec := insertStep(t, api, task.ID, "m-exec",
			map[string]any{"name": "   ", "dod": "d"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("blank name must 400, got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message,
			"step name must not be blank"; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		rec = insertStep(t, api, task.ID, "m-exec",
			map[string]any{"name": "needs a dod", "dod": "  "})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("blank dod must 400, got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message,
			"step 'needs a dod' must have a non-empty definition of done"; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("a new step wedged between two lanes of a parallel group is refused", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "lane a", "dod": "d", "parallel_group": "g1"},
			{"name": "lane b", "dod": "d", "parallel_group": "g1"},
			{"name": "join", "dod": "d"},
		})
		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "wedge", "dod": "d", "before_step_id": v1.Steps[1].ID,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("splitting a group must 400, got %d %s", rec.Code, rec.Body.String())
		}
		want := "steps sharing parallel_group 'g1' must sit next to each other — " +
			"move them together, or give the later run a different group key"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 3 {
			t.Fatalf("a refused insert must write nothing, got %v", stepNames(v.Steps))
		}
	})

	// The wedge case above only exercises rule 2 (contiguity), which reads the
	// whole timeline. Rules 1 and 3 read the FRESH row alone, so dropping the
	// second argument to ValidatePlanParallelShape leaves contiguity working and
	// silently admits both of these — measured: 400 becomes 200 for each.
	t.Run("a new step may not be a gate inside a parallel group", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "lane a", "dod": "d", "parallel_group": "g1"},
			{"name": "lane b", "dod": "d", "parallel_group": "g1"},
		})
		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "ask the owner", "dod": "d",
			"parallel_group": "g1", "is_gate": true,
			"before_step_id": v1.Steps[0].ID,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("a gate inside a group must 400, got %d %s", rec.Code, rec.Body.String())
		}
		want := "step 'ask the owner': a gate step cannot sit inside a parallel group — " +
			"put the gate on its own step after the group's join step"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 2 {
			t.Fatalf("a refused insert must write nothing, got %v", stepNames(v.Steps))
		}
	})

	t.Run("a new step may not open a parallel group of its own", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d"}})
		rec := insertStep(t, api, task.ID, "m-exec", map[string]any{
			"name": "solo lane", "dod": "d", "parallel_group": "gnew",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("a one-lane group must 400, got %d %s", rec.Code, rec.Body.String())
		}
		want := "parallel_group 'gnew' holds only one step — running in parallel takes " +
			"at least two; drop the parallel_group to keep the step sequential"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 1 {
			t.Fatalf("a refused insert must write nothing, got %v", stepNames(v.Steps))
		}
	})

	t.Run("only the executor may insert, and never into a closed task", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d"}})
		rec := insertStep(t, api, task.ID, "m-stranger",
			map[string]any{"name": "x", "dod": "d"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("a stranger must 403, got %d %s", rec.Code, rec.Body.String())
		}
		driveTaskDone(t, api, task.ID, "m-exec")
		rec = insertStep(t, api, task.ID, "m-exec",
			map[string]any{"name": "x", "dod": "d"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("a closed task must 409, got %d %s", rec.Code, rec.Body.String())
		}
	})
}

// ── delete_step ──────────────────────────────────────────────────────────────

func TestHandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost(t *testing.T) {
	t.Run("removing an unfinished step leaves every other step exactly as it was", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "one", "dod": "d1"},
			{"name": "dropped", "dod": "d2"},
			{"name": "three", "dod": "d3"},
		})
		writeStepNote(t, api, task.ID, v1.Steps[2].ID, "m-exec", "kept note")

		rec := deleteStep(t, api, task.ID, v1.Steps[1].ID, "m-exec")
		if rec.Code != http.StatusOK {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
		}
		assertReceiptKeys(t, rec, "task_id", "steps_total", "progress_done",
			"progress_total")
		receipt := decodeBody[taskStepMutationReceiptDTO](t, rec)
		if receipt.TaskID != task.ID || receipt.StepsTotal != 2 ||
			receipt.ProgressTotal != 2 {
			t.Fatalf("delete receipt wrong shape: %+v", receipt)
		}
		v2 := getTaskView(t, api, task.ID)
		if got := stepNames(v2.Steps); !sameStrings(got, []string{"one", "three"}) {
			t.Fatalf("timeline: want [one three], got %v", got)
		}
		assertStepOrderContiguous(t, "delete_step", v2.Steps)
		if v2.Steps[0].ID != v1.Steps[0].ID || v2.Steps[1].ID != v1.Steps[2].ID {
			t.Fatalf("survivors must keep their ids: %+v", v2.Steps)
		}
		if got := storedStepNote(t, api, v1.Steps[2].ID); got != "kept note" {
			t.Fatalf("a survivor's note must survive, got %q", got)
		}
		if got, _ := api.dal.GetTaskStep(v1.Steps[1].ID); got != nil {
			t.Fatalf("the deleted step must be gone: %+v", got)
		}
	})

	t.Run("a finished step cannot be deleted", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "one", "dod": "d1"},
			{"name": "two", "dod": "d2"},
		})
		done := v1.Steps[0]
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, done.ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		rec := deleteStep(t, api, task.ID, done.ID, "m-exec")
		if rec.Code != http.StatusConflict {
			t.Fatalf("deleting finished work must 409, got %d %s",
				rec.Code, rec.Body.String())
		}
		want := "step '" + done.ID + "' is already done — finished steps are the " +
			"record of what was done and cannot be deleted"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		if v := getTaskView(t, api, task.ID); len(v.Steps) != 2 {
			t.Fatalf("a refused delete must write nothing, got %v", stepNames(v.Steps))
		}
	})

	t.Run("the last remaining step cannot be deleted", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "only", "dod": "d"}})
		rec := deleteStep(t, api, task.ID, v1.Steps[0].ID, "m-exec")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("emptying a plan must 400, got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message,
			"a plan must have at least one step"; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("an unknown step is a 404", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d"}})
		rec := deleteStep(t, api, task.ID, "ts-nosuchstep", "m-exec")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown step must 404, got %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("deleting the last unfinished step of a delegated task is refused by the 交棒閘", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createDelegatedTask(t, api, "owner", "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "done work", "dod": "d1"},
			{"name": "abandoned", "dod": "d2"},
		})
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		rec := deleteStep(t, api, task.ID, v1.Steps[1].ID, "m-exec")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("deleting the last unfinished step of a delegated task must "+
				"422, got %d %s", rec.Code, rec.Body.String())
		}
		msg := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message
		for _, fragment := range []string{
			"it is the LAST unfinished step",
			"delete_step carries no handoff declaration",
			"update_step_status report that finishes it",
			"set_task_deps",
		} {
			if !strings.Contains(msg, fragment) {
				t.Fatalf("the refusal must name the way out (%q missing):\n%s",
					fragment, msg)
			}
		}
		v2 := getTaskView(t, api, task.ID)
		if len(v2.Steps) != 2 || v2.Status == TaskStatusReadyForDone {
			t.Fatalf("a refused delete must write nothing: %d steps, status %q",
				len(v2.Steps), v2.Status)
		}
	})

	// The 422 case proves the gate REFUSES; this one proves the admitted path
	// still WRITES the handover. Dropping applyHandoffPlan leaves the delete
	// answering 200 with the three handoff fields empty, and no other test looks
	// at them — a side effect nobody asserts is a side effect nothing guards.
	t.Run("a live dependent satisfies the gate and the handover is recorded", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createDelegatedTask(t, api, "owner", "m-exec")
		successor := createAdHocTask(t, api, "m-exec")
		if err := api.dal.AddTaskDep(successor.ID, task.ID); err != nil {
			t.Fatalf("AddTaskDep: %v", err)
		}
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "done work", "dod": "d1"},
			{"name": "abandoned", "dod": "d2"},
		})
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		if rec := deleteStep(t, api, task.ID, v1.Steps[1].ID,
			"m-exec"); rec.Code != http.StatusOK {
			t.Fatalf("delete with a live dependent: %d %s", rec.Code, rec.Body.String())
		}
		stored, err := api.dal.GetTask(task.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetTask: %+v %v", stored, err)
		}
		if stored.Handoff != HandoffFollowUp || stored.HandoffTaskID != successor.ID {
			t.Fatalf("the handover must be recorded: handoff=%q task=%q note=%q",
				stored.Handoff, stored.HandoffTaskID, stored.HandoffNote)
		}
		if stored.HandoffNote == "" {
			t.Fatalf("an automatic handover must say why it stood aside, got an empty note")
		}
	})

	t.Run("the gate stands aside when the creator is the executor", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "done work", "dod": "d1"},
			{"name": "abandoned", "dod": "d2"},
		})
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		if rec := deleteStep(t, api, task.ID, v1.Steps[1].ID,
			"m-exec"); rec.Code != http.StatusOK {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
		}
		v2 := getTaskView(t, api, task.ID)
		if v2.Status != TaskStatusReadyForDone {
			t.Fatalf("an all-done step set lands ready_for_done, got %q", v2.Status)
		}
		assertStepOrderContiguous(t, "delete_step (last unfinished)", v2.Steps)
	})
}

// ── reorder_steps ────────────────────────────────────────────────────────────

func TestHandleReorderTaskStepsApiTasksTaskIdStepsReorderPost(t *testing.T) {
	t.Run("reordering rebuilds nothing — ids, statuses, notes and cards all survive", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "a", "dod": "d1"},
			{"name": "b", "dod": "d2"},
			{"name": "c", "dod": "d3"},
		})
		a, b, c := v1.Steps[0], v1.Steps[1], v1.Steps[2]
		startFirstStep(t, api, task.ID, "m-exec")
		writeStepNote(t, api, task.ID, b.ID, "m-exec", "b's note")
		card := openCardOnStep(t, api, task.ID, "m-exec", c.ID, "which way?")

		rec := reorderSteps(t, api, task.ID, "m-exec", []string{c.ID, a.ID, b.ID})
		if rec.Code != http.StatusOK {
			t.Fatalf("reorder: %d %s", rec.Code, rec.Body.String())
		}
		assertReceiptKeys(t, rec, "task_id", "steps_total", "progress_done",
			"progress_total")
		v2 := getTaskView(t, api, task.ID)
		if got := stepNames(v2.Steps); !sameStrings(got, []string{"c", "a", "b"}) {
			t.Fatalf("timeline: want [c a b], got %v", got)
		}
		assertStepOrderContiguous(t, "reorder_steps", v2.Steps)
		if v2.Steps[0].ID != c.ID || v2.Steps[1].ID != a.ID || v2.Steps[2].ID != b.ID {
			t.Fatalf("no row may be rebuilt — ids must be the same rows: %+v", v2.Steps)
		}
		if got := storedStepNote(t, api, b.ID); got != "b's note" {
			t.Fatalf("the moved step's note must survive, got %q", got)
		}
		if v2.Steps[0].Status != StepStatusWaitingOwner ||
			v2.Steps[0].ReplyCardID != card.ID {
			t.Fatalf("the card-holding step must keep its card: %+v", v2.Steps[0])
		}
	})

	// 🔴 THE FINISHED STEP SITS IN THE MIDDLE, and that is the whole test. With it
	// at index 0, "keep the finished step where it is" and "push the finished
	// steps to the front" produce the SAME timeline, so the assertion cannot tell
	// the two apart — measured: an implementation that squeezes finished rows to
	// the front passes a version of this test that parks the done step first.
	t.Run("finished steps keep their positions and must not be listed", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "x", "dod": "d1"},
			{"name": "done", "dod": "d2"},
			{"name": "y", "dod": "d3"},
		})
		x, done, y := v1.Steps[0], v1.Steps[1], v1.Steps[2]
		for _, status := range []string{"in_progress", "done"} {
			if rec := driveStepStatus(t, api, task.ID, done.ID, "m-exec",
				status); rec.Code != http.StatusOK {
				t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
			}
		}
		if rec := reorderSteps(t, api, task.ID, "m-exec",
			[]string{y.ID, x.ID}); rec.Code != http.StatusOK {
			t.Fatalf("reorder: %d %s", rec.Code, rec.Body.String())
		}
		v2 := getTaskView(t, api, task.ID)
		if got := stepNames(v2.Steps); !sameStrings(got, []string{"y", "done", "x"}) {
			t.Fatalf("the finished step must stay at index 1: want [y done x], got %v", got)
		}
		assertStepOrderContiguous(t, "reorder_steps (with finished history)", v2.Steps)

		rec := reorderSteps(t, api, task.ID, "m-exec", []string{done.ID, y.ID, x.ID})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("listing a finished step must 400, got %d %s",
				rec.Code, rec.Body.String())
		}
		want := "step '" + done.ID + "' is already done — finished steps keep the " +
			"position they already hold and must not be listed in step_ids"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("a step_ids that is not exactly the unfinished set is refused", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "a", "dod": "d1"},
			{"name": "b", "dod": "d2"},
		})
		a, b := v1.Steps[0], v1.Steps[1]
		for _, tc := range []struct {
			name string
			ids  []string
			want string
		}{
			{
				name: "one missing",
				ids:  []string{b.ID},
				want: "step_ids must list every unfinished step of task '" + task.ID +
					"' exactly once; missing: '" + a.ID + "'",
			},
			{
				name: "one listed twice",
				ids:  []string{a.ID, b.ID, a.ID},
				want: "step '" + a.ID + "' is listed twice in step_ids",
			},
			{
				name: "a step of no task at all",
				ids:  []string{a.ID, b.ID, "ts-nosuchstep"},
				want: "step 'ts-nosuchstep' is not a step of task '" + task.ID + "'",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rec := reorderSteps(t, api, task.ID, "m-exec", tc.ids)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("must 400, got %d %s", rec.Code, rec.Body.String())
				}
				if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != tc.want {
					t.Fatalf("refusal:\n got %q\nwant %q", got, tc.want)
				}
				v := getTaskView(t, api, task.ID)
				if got := stepNames(v.Steps); !sameStrings(got, []string{"a", "b"}) {
					t.Fatalf("a refused reorder must write nothing, got %v", got)
				}
			})
		}
	})

	t.Run("a reorder that pulls a lane out of its parallel group is refused", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "lane a", "dod": "d", "parallel_group": "g1"},
			{"name": "lane b", "dod": "d", "parallel_group": "g1"},
			{"name": "join", "dod": "d"},
		})
		rec := reorderSteps(t, api, task.ID, "m-exec",
			[]string{v1.Steps[0].ID, v1.Steps[2].ID, v1.Steps[1].ID})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("splitting a group must 400, got %d %s", rec.Code, rec.Body.String())
		}
		want := "steps sharing parallel_group 'g1' must sit next to each other — " +
			"move them together, or give the later run a different group key"
		if got := decodeBody[ErrorEnvelopeDTO](t, rec).Error.Message; got != want {
			t.Fatalf("refusal:\n got %q\nwant %q", got, want)
		}
		v2 := getTaskView(t, api, task.ID)
		if got := stepNames(v2.Steps); !sameStrings(got, []string{"lane a", "lane b", "join"}) {
			t.Fatalf("a refused reorder must write nothing, got %v", got)
		}
	})

	t.Run("only the executor may reorder, and never on a closed task", func(t *testing.T) {
		api := newTasksTestServer(t)
		task := createAdHocTask(t, api, "m-exec")
		v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
			{"name": "a", "dod": "d1"},
			{"name": "b", "dod": "d2"},
		})
		ids := []string{v1.Steps[1].ID, v1.Steps[0].ID}
		if rec := reorderSteps(t, api, task.ID, "m-stranger",
			ids); rec.Code != http.StatusForbidden {
			t.Fatalf("a stranger must 403, got %d %s", rec.Code, rec.Body.String())
		}
		driveTaskDone(t, api, task.ID, "m-exec")
		if rec := reorderSteps(t, api, task.ID, "m-exec",
			ids); rec.Code != http.StatusConflict {
			t.Fatalf("a closed task must 409, got %d %s", rec.Code, rec.Body.String())
		}
	})
}

// TestSubmitPlanLeavesTheStepOrderContiguous holds submit_plan — the writer that
// was already shipping — to the SAME invariant the three single-step writes are
// held to, through the same assertion. Without it the rule would be guarded on
// the new half only, and the old half is the one in use today.
func TestSubmitPlanLeavesTheStepOrderContiguous(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	v1 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	assertStepOrderContiguous(t, "submit_plan (first plan)", v1.Steps)
	for _, status := range []string{"in_progress", "done"} {
		if rec := driveStepStatus(t, api, task.ID, v1.Steps[0].ID, "m-exec",
			status); rec.Code != http.StatusOK {
			t.Fatalf("drive %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	v2 := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "three", "dod": "d3"},
		{"name": "four", "dod": "d4"},
	})
	assertStepOrderContiguous(t, "submit_plan (replan over a kept prefix)", v2.Steps)
}
