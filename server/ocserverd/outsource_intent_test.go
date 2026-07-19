package main

import "testing"

func TestValidTaskStatusAcceptsPendingOutsourceApproval(t *testing.T) {
	if !ValidTaskStatus(TaskStatusPendingOutsourceApproval) {
		t.Fatalf("ValidTaskStatus rejected the new pending_outsource_approval state")
	}
	if ValidTaskStatus("pending_outsource") {
		t.Fatalf("ValidTaskStatus accepted an unknown status")
	}
}

func TestCanOutsourceApprovalTransition(t *testing.T) {
	statuses := []string{
		TaskStatusNotStarted, TaskStatusInProgress, TaskStatusWaitingOwner,
		TaskStatusWaitingExternal, TaskStatusReassigning, TaskStatusDone,
		TaskStatusTerminated, TaskStatusDuplicated, TaskStatusPendingOutsourceApproval,
	}
	legal := map[[2]string]bool{
		{TaskStatusNotStarted, TaskStatusPendingOutsourceApproval}:  true,
		{TaskStatusInProgress, TaskStatusPendingOutsourceApproval}:  true,
		{TaskStatusPendingOutsourceApproval, TaskStatusReassigning}: true,
		{TaskStatusPendingOutsourceApproval, TaskStatusTerminated}:  true,
		{TaskStatusPendingOutsourceApproval, TaskStatusNotStarted}:  true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			want := legal[[2]string{from, to}]
			if got := CanOutsourceApprovalTransition(from, to); got != want {
				t.Fatalf("outsource %s -> %s: want %v, got %v", from, to, want, got)
			}
		}
	}
	// The no-re-dispatch invariant, called out explicitly: a task already at
	// pending may not re-enter pending.
	if CanOutsourceApprovalTransition(TaskStatusPendingOutsourceApproval, TaskStatusPendingOutsourceApproval) {
		t.Fatalf("pending -> pending must be illegal (no double dispatch)")
	}
}

func TestCanApproveOutsourceIntent(t *testing.T) {
	cases := []struct {
		name            string
		status          string
		current, expect int64
		want            bool
	}{
		{"pending and version matches approves", TaskStatusPendingOutsourceApproval, 3, 3, true},
		{"pending but stale version no-op", TaskStatusPendingOutsourceApproval, 4, 3, false},
		{"not pending no-op even if version matches", TaskStatusInProgress, 3, 3, false},
		{"terminal no-op", TaskStatusTerminated, 1, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanApproveOutsourceIntent(c.status, c.current, c.expect); got != c.want {
				t.Fatalf("CanApproveOutsourceIntent(%q, %d, %d) = %v, want %v",
					c.status, c.current, c.expect, got, c.want)
			}
		})
	}
}

func TestOutsourceIntentIdempotencyKey(t *testing.T) {
	if got := OutsourceIntentIdempotencyKey("task-7", 2); got != "task-7:2" {
		t.Fatalf("idempotency key = %q, want task-7:2", got)
	}
	// Same (task, version) is stable; a re-dispatch (new version) is distinct.
	if OutsourceIntentIdempotencyKey("task-7", 2) != OutsourceIntentIdempotencyKey("task-7", 2) {
		t.Fatalf("idempotency key is not stable for the same (task, version)")
	}
	if OutsourceIntentIdempotencyKey("task-7", 2) == OutsourceIntentIdempotencyKey("task-7", 3) {
		t.Fatalf("a re-dispatch (new version) must yield a distinct key")
	}
}

func TestCanDispatchOutsource(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{TaskStatusNotStarted, true},
		{TaskStatusInProgress, true},
		{TaskStatusPendingOutsourceApproval, false}, // already dispatched: no re-dispatch
		{TaskStatusReassigning, false},
		{TaskStatusDone, false},
		{TaskStatusTerminated, false},
	}
	for _, c := range cases {
		if got := CanDispatchOutsource(c.status); got != c.want {
			t.Fatalf("CanDispatchOutsource(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}
