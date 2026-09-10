package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestWouldCloseTask(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []TaskStep
		id    string
		want  bool
	}{
		{
			name:  "the only active step becomes done",
			steps: []TaskStep{{ID: "s-1", Status: StepStatusInProgress}},
			id:    "s-1",
			want:  true,
		},
		{
			name: "a sibling still in progress keeps the task open",
			steps: []TaskStep{
				{ID: "s-1", Status: StepStatusInProgress},
				{ID: "s-2", Status: StepStatusInProgress},
			},
			id:   "s-1",
			want: false,
		},
		{
			name: "superseded history does not keep an otherwise finished task open",
			steps: []TaskStep{
				{ID: "s-1", Status: StepStatusInProgress},
				{ID: "s-old", Status: StepStatusSuperseded},
			},
			id:   "s-1",
			want: true,
		},
		{
			name:  "an unrelated step id leaves a pending task open",
			steps: []TaskStep{{ID: "s-1", Status: StepStatusPending}},
			id:    "s-missing",
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wouldCloseTask(tc.steps, tc.id); got != tc.want {
				t.Fatalf("wouldCloseTask() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandoffGateReason(t *testing.T) {
	task := Task{ID: "T-123", CreatorID: "kip", ExecutorID: "mira"}
	for _, tc := range []struct {
		name   string
		door   string
		pieces []string
	}{
		{
			name: "step report names every available declaration route",
			door: handoffDoorStepReport,
			pieces: []string{
				"task 'T-123' was created by 'kip' but executed by 'mira'",
				"this report would CLOSE it",
				"handoff='return_to_creator'",
				"handoff='follow_up'",
				"handoff='none'",
			},
		},
		{
			name: "replan names the only routes that can avoid a closed plan",
			door: handoffDoorReplan,
			pieces: []string{
				"this plan leaves EVERY step done",
				"update_step_status",
				"create_task",
				"set_task_deps",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := handoffGateReason(task, tc.door)
			if strings.Count(got, task.ID) != 1 {
				t.Fatalf("task id occurrence count = %d, want 1 in %q", strings.Count(got, task.ID), got)
			}
			for _, piece := range tc.pieces {
				if !strings.Contains(got, piece) {
					t.Fatalf("reason %q does not contain %q", got, piece)
				}
			}
		})
	}
}

func TestHandoffGateVerdict(t *testing.T) {
	t.Run("a self-created task may close without a declaration", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.CreatorID = task.ExecutorID
		task.Handoff = HandoffUndeclared

		plan, code, message := api.handoffGateVerdict(task, handoffDoorStepReport, "", "", "")
		if plan != nil || code != 0 || message != "" {
			t.Fatalf("verdict = %#v, %d, %q; want nil, 0, empty", plan, code, message)
		}
	})

	t.Run("a cross-actor task without a successor is refused with its complete reason", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.CreatorID = apiTestPlainAgentID
		task.ExecutorID = "mira"
		task.Handoff = HandoffUndeclared

		plan, code, message := api.handoffGateVerdict(task, handoffDoorStepReport, "", "", "")
		if plan != nil || code != http.StatusUnprocessableEntity {
			t.Fatalf("verdict = %#v, %d; want nil, 422", plan, code)
		}
		if message != handoffGateReason(task, handoffDoorStepReport) {
			t.Fatalf("message = %q, want the step-report reason", message)
		}
	})

	t.Run("a live dependent automatically satisfies the follow-up declaration", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		blocker := dalTestTask("T-1")
		blocker.CreatorID = apiTestPlainAgentID
		blocker.ExecutorID = "mira"
		blocker.Handoff = HandoffUndeclared
		dependent := dalTestTask("T-2")
		dependent.Status = TaskStatusInProgress
		if err := d.PutTask(blocker); err != nil {
			t.Fatalf("PutTask(blocker): %v", err)
		}
		if err := d.PutTask(dependent); err != nil {
			t.Fatalf("PutTask(dependent): %v", err)
		}
		if err := d.AddTaskDep(dependent.ID, blocker.ID); err != nil {
			t.Fatalf("AddTaskDep: %v", err)
		}

		plan, code, message := api.handoffGateVerdict(blocker, handoffDoorStepReport, "", "", "")
		if code != 0 || message != "" || plan == nil {
			t.Fatalf("verdict = %#v, %d, %q; want an admitted plan", plan, code, message)
		}
		if !plan.Auto || plan.Kind != HandoffFollowUp || plan.TaskID != dependent.ID {
			t.Fatalf("plan = %#v, want automatic follow-up for %s", plan, dependent.ID)
		}
	})

	t.Run("none requires a non-blank reason and stores the trimmed reason", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.CreatorID = apiTestPlainAgentID
		task.ExecutorID = "mira"

		plan, code, message := api.handoffGateVerdict(task, handoffDoorStepReport, HandoffNone, "  no follow-up  ", "")
		if code != 0 || message != "" || plan == nil {
			t.Fatalf("verdict = %#v, %d, %q; want an admitted plan", plan, code, message)
		}
		if plan.Kind != HandoffNone || plan.Note != "no follow-up" {
			t.Fatalf("plan = %#v, want none with a trimmed note", plan)
		}

		_, code, message = api.handoffGateVerdict(task, handoffDoorStepReport, HandoffNone, "  ", "")
		if code != http.StatusUnprocessableEntity || !strings.Contains(message, "requires handoff_note") {
			t.Fatalf("blank-note verdict = %d, %q; want 422 with handoff_note reason", code, message)
		}
	})

	t.Run("follow-up requires an open successor and preserves its id", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.CreatorID = apiTestPlainAgentID
		task.ExecutorID = "mira"
		successor := dalTestTask("T-2")
		successor.Status = TaskStatusInProgress
		if err := d.PutTask(successor); err != nil {
			t.Fatalf("PutTask(successor): %v", err)
		}

		plan, code, message := api.handoffGateVerdict(task, handoffDoorStepReport, HandoffFollowUp, " successor note ", successor.ID)
		if code != 0 || message != "" || plan == nil {
			t.Fatalf("verdict = %#v, %d, %q; want an admitted plan", plan, code, message)
		}
		if plan.Kind != HandoffFollowUp || plan.TaskID != successor.ID || plan.Note != "successor note" {
			t.Fatalf("plan = %#v, want follow-up successor", plan)
		}

		_, code, message = api.handoffGateVerdict(task, handoffDoorStepReport, HandoffFollowUp, "", "T-missing")
		if code != http.StatusUnprocessableEntity || !strings.Contains(message, "unknown successor task") {
			t.Fatalf("unknown successor verdict = %d, %q; want 422", code, message)
		}
	})

	t.Run("return to creator requires an active creator and trims the note", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.CreatorID = apiTestPlainAgentID
		task.ExecutorID = "mira"

		plan, code, message := api.handoffGateVerdict(task, handoffDoorStepReport, HandoffReturnToCreator, "  review me  ", "")
		if code != 0 || message != "" || plan == nil {
			t.Fatalf("verdict = %#v, %d, %q; want an admitted plan", plan, code, message)
		}
		if plan.Kind != HandoffReturnToCreator || plan.Note != "review me" {
			t.Fatalf("plan = %#v, want return_to_creator with a trimmed note", plan)
		}
	})
}

func TestApplyHandoffPlan(t *testing.T) {
	t.Run("a nil plan leaves the task unchanged", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		before := task
		if err := api.applyHandoffPlan(&task, nil); err != nil {
			t.Fatalf("applyHandoffPlan: %v", err)
		}
		if !reflect.DeepEqual(task, before) {
			t.Fatalf("task changed from %#v to %#v", before, task)
		}
	})

	t.Run("follow-up attaches the dependency and records every declaration field", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		successor := dalTestTask("T-2")
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask(task): %v", err)
		}
		if err := d.PutTask(successor); err != nil {
			t.Fatalf("PutTask(successor): %v", err)
		}
		plan := &handoffPlan{Kind: HandoffFollowUp, Note: "continue there", TaskID: successor.ID}
		if err := api.applyHandoffPlan(&task, plan); err != nil {
			t.Fatalf("applyHandoffPlan: %v", err)
		}
		if task.Handoff != HandoffFollowUp || task.HandoffNote != plan.Note || task.HandoffTaskID != successor.ID {
			t.Fatalf("task = %#v, want stamped follow-up declaration", task)
		}
		deps, err := d.ListTaskDeps(successor.ID)
		if err != nil {
			t.Fatalf("ListTaskDeps: %v", err)
		}
		if len(deps) != 1 || deps[0] != task.ID {
			t.Fatalf("successor deps = %#v, want [%s]", deps, task.ID)
		}
	})

	t.Run("return to creator records the declaration without creating a dependency", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.HandoffTaskID = ""
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask(task): %v", err)
		}
		if err := api.applyHandoffPlan(&task, &handoffPlan{Kind: HandoffReturnToCreator, Note: "review"}); err != nil {
			t.Fatalf("applyHandoffPlan: %v", err)
		}
		if task.Handoff != HandoffReturnToCreator || task.HandoffNote != "review" || task.HandoffTaskID != "" {
			t.Fatalf("task = %#v, want return declaration without successor", task)
		}
		deps, err := d.ListTaskDeps(task.ID)
		if err != nil {
			t.Fatalf("ListTaskDeps: %v", err)
		}
		if len(deps) != 0 {
			t.Fatalf("task deps = %#v, want none", deps)
		}
	})
}

func TestReleaseDependentsOnClose(t *testing.T) {
	t.Run("a dependent with no live blockers is touched and receives a durable notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"mira"}`)
		blocker, err := d.GetTask("T-1")
		if err != nil || blocker == nil {
			t.Fatalf("GetTask(blocker): %v %#v", err, blocker)
		}
		dependent, err := d.GetTask("T-2")
		if err != nil || dependent == nil {
			t.Fatalf("GetTask(dependent): %v %#v", err, dependent)
		}
		if err := d.AddTaskDep(dependent.ID, blocker.ID); err != nil {
			t.Fatalf("AddTaskDep: %v", err)
		}
		blocker.Status = TaskStatusDone
		if err := d.PutTask(*blocker); err != nil {
			t.Fatalf("PutTask(blocker): %v", err)
		}
		before := dependent.UpdatedTS

		api.releaseDependentsOnClose(*blocker, 1750000000, "test")

		got, err := d.GetTask(dependent.ID)
		if err != nil || got == nil {
			t.Fatalf("GetTask(dependent): %v %#v", err, got)
		}
		if got.Status != TaskStatusNotStarted || got.UpdatedTS != 1750000000 || got.UpdatedTS == before {
			t.Fatalf("dependent = %#v, want untouched status and updated timestamp", got)
		}
		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("chat rows = %d, want one release notice", len(rows))
		}
		wantBody := api.taskNoticeText(docKindTaskUnblocked, map[string]string{"blocked_task_no": dependent.ID})
		if rows[0].Sender != wireSystemSender || rows[0].Recipient != dependent.ExecutorID || rows[0].Body != wantBody || rows[0].Meta["task_id"] != dependent.ID {
			t.Fatalf("release notice = %#v, want the durable unblocked notice", rows[0])
		}
	})

	t.Run("a second live blocker prevents release", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		blocker := dalTestTask("T-1")
		blocker.Status = TaskStatusDone
		live := dalTestTask("T-2")
		live.Status = TaskStatusInProgress
		dependent := dalTestTask("T-3")
		dependent.Status = TaskStatusNotStarted
		dependent.ExecutorID = "mira"
		for _, task := range []Task{blocker, live, dependent} {
			if err := d.PutTask(task); err != nil {
				t.Fatalf("PutTask(%s): %v", task.ID, err)
			}
		}
		if err := d.AddTaskDep(dependent.ID, blocker.ID); err != nil {
			t.Fatalf("AddTaskDep(blocker): %v", err)
		}
		if err := d.AddTaskDep(dependent.ID, live.ID); err != nil {
			t.Fatalf("AddTaskDep(live): %v", err)
		}

		api.releaseDependentsOnClose(blocker, 1750000000, "test")

		got, err := d.GetTask(dependent.ID)
		if err != nil || got == nil {
			t.Fatalf("GetTask(dependent): %v %#v", err, got)
		}
		if got.UpdatedTS == 1750000000 {
			t.Fatalf("dependent was touched while a live blocker remained: %#v", got)
		}
		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("chat rows = %#v, want none", rows)
		}
	})

	t.Run("terminal dependents are ignored", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		blocker := dalTestTask("T-1")
		blocker.Status = TaskStatusDone
		dependent := dalTestTask("T-2")
		dependent.Status = TaskStatusTerminated
		for _, task := range []Task{blocker, dependent} {
			if err := d.PutTask(task); err != nil {
				t.Fatalf("PutTask(%s): %v", task.ID, err)
			}
		}
		if err := d.AddTaskDep(dependent.ID, blocker.ID); err != nil {
			t.Fatalf("AddTaskDep: %v", err)
		}

		api.releaseDependentsOnClose(blocker, 1750000000, "test")

		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("chat rows = %#v, want none", rows)
		}
	})
}

func TestHasLiveBlocker(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	tasks := []Task{dalTestTask("T-1"), dalTestTask("T-2"), dalTestTask("T-3"), dalTestTask("T-4")}
	statuses := []string{TaskStatusInProgress, TaskStatusDone, TaskStatusTerminated, TaskStatusDuplicated}
	for i := range tasks {
		tasks[i].Status = statuses[i]
	}
	for _, task := range tasks {
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask(%s): %v", task.ID, err)
		}
	}
	for _, tc := range []struct {
		name string
		ids  []string
		want bool
	}{
		{name: "an in-progress blocker is live", ids: []string{"T-1"}, want: true},
		{name: "terminal blockers are not live", ids: []string{"T-2", "T-3", "T-4"}, want: false},
		{name: "a deleted blocker does not permanently block", ids: []string{"T-missing"}, want: false},
		{name: "an empty blocker list is clear", ids: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := api.hasLiveBlocker(tc.ids); got != tc.want {
				t.Fatalf("hasLiveBlocker(%#v) = %v, want %v", tc.ids, got, tc.want)
			}
		})
	}
}

func TestTaskHasLiveBlocker(t *testing.T) {
	statusOf := map[string]string{
		"T-1": TaskStatusInProgress,
		"T-2": TaskStatusDone,
		"T-3": TaskStatusTerminated,
		"T-4": TaskStatusDuplicated,
	}
	for _, tc := range []struct {
		name string
		ids  []string
		want bool
	}{
		{name: "a live snapshot status blocks", ids: []string{"T-1"}, want: true},
		{name: "terminal snapshot statuses do not block", ids: []string{"T-2", "T-3", "T-4"}, want: false},
		{name: "a missing snapshot row is a dangling marker", ids: []string{"T-missing"}, want: false},
		{name: "an empty blocker list is clear", ids: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskHasLiveBlocker(tc.ids, statusOf); got != tc.want {
				t.Fatalf("taskHasLiveBlocker(%#v) = %v, want %v", tc.ids, got, tc.want)
			}
		})
	}
}
