package main

import "testing"

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
