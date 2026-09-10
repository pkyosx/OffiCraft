// Skeleton generated from server/ocserverd/lifecycle_tick.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"testing"
	"time"
)

func TestRunLifecycleTick(t *testing.T) {
	t.Run("the reconcile kill switch leaves outsource assignment running", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		lifecycleTickTestQueue(t, d, "T-lifecycle-outsource")
		api.noReconcile = true

		api.runLifecycleTick(1700000100)

		gotTask, err := d.GetTask("T-lifecycle-outsource")
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if gotTask == nil || gotTask.ExecutorID == "" {
			t.Fatalf("task after outsource half = %+v, want an assigned executor", gotTask)
		}
		workers, err := d.ListOutsourceWorkers()
		if err != nil {
			t.Fatalf("ListOutsourceWorkers: %v", err)
		}
		if len(workers) != 1 {
			t.Fatalf("worker count = %d, want 1", len(workers))
		}
	})

	t.Run("the outsource kill switch leaves reconcile dispatching staff", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		mira, err := d.GetMember(seedMiraID)
		if err != nil || mira == nil {
			t.Fatalf("GetMember(mira): %v (%+v)", err, mira)
		}
		mira.DesiredState = DesiredStateOnline
		if err := d.PutMember(*mira); err != nil {
			t.Fatalf("PutMember(mira): %v", err)
		}
		warden, err := api.hub.Connect(ServerSelfHost, ServerSelfHost)
		if err != nil {
			t.Fatalf("hub.Connect(server-self): %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(warden) })
		api.noOutsource = true

		api.runLifecycleTick(1700000100)

		commands := api.hub.DrainWardenCommands(ServerSelfHost)
		if len(commands) != 1 {
			t.Fatalf("warden command count = %d, want 1", len(commands))
		}
		pending, ok := api.receiptPending[seedMiraID]
		if !ok || pending.RPC != reconcileCmdStart || pending.Deadline != 1700000190 {
			t.Fatalf("receipt watch = %+v, want start deadline 1700000190", pending)
		}
		workers, err := d.ListOutsourceWorkers()
		if err != nil {
			t.Fatalf("ListOutsourceWorkers: %v", err)
		}
		if len(workers) != 0 {
			t.Fatalf("workers after disabled outsource half = %d, want 0", len(workers))
		}
	})
}

func TestStartLifecycleCadence(t *testing.T) {
	api := &apiServer{}
	api.startLifecycleCadence(time.Hour)
}

func lifecycleTickTestQueue(t *testing.T, d *DAL, taskID string) {
	t.Helper()
	manual := TaskManual{
		TypeKey:  "tm-lifecycle-tick",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","runtime":"claude","model":"sonnet"}`,
	}
	if err := d.PutTaskManual(manual); err != nil {
		t.Fatalf("PutTaskManual: %v", err)
	}
	if err := d.PutTask(Task{
		ID: taskID, TypeKey: manual.TypeKey, Title: "Lifecycle tick task",
		Inputs: map[string]any{}, Status: TaskStatusNotStarted, Priority: TaskPriorityHigh,
		ExecutorKind: TaskExecutorOutsource, CreatorID: wireOwnerID,
		CreatedTS: 1700000000, UpdatedTS: 1700000000,
	}); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}
