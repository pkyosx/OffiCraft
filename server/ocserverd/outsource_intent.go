package main

import "fmt"

// outsource_intent.go — the pure state layer for "案1 pending 核可": an agent
// DISPATCHES an outsource task (發包) and the owner approves each dispatch at a
// per-task gate before any worker is spawned. Framework-free like domain.go:
// only the intent value type and the pure predicates the dispatch/approve
// handler nodes will call — no DAL, no spawn, no HTTP here.
//
// "Approve" is a compare-and-swap over the dispatch intent: the owner approves
// a SPECIFIC intent version, and the swap only fires if the task still sits at
// pending_outsource_approval AND that version is still the live one. A stale
// approval (superseded intent, or a task the agent already moved on) is a
// no-op — this is what keeps a fresh worker from spawning against an intent the
// owner never actually saw.

// OutsourceIntent is one dispatch (發包) proposal awaiting owner approval. A
// pure domain value: the first handler node decides persistence — this layer
// only reasons over it. Version is the intent epoch; re-dispatching the same
// task mints a higher version, so an approval carrying an older version loses
// the CAS (CanApproveOutsourceIntent) and the owner sees only the latest.
type OutsourceIntent struct {
	TaskID   string
	Version  int64  // intent epoch; bumped on each re-dispatch of the task
	Model    string // target worker model
	Effort   string // target reasoning effort
	Machine  string // target machine
	IssuedBy string // the actor that dispatched (發起者)
}

// CanApproveOutsourceIntent is the approve-time CAS predicate: an approval may
// land only while the task is still parked at pending_outsource_approval AND
// the version the owner approved still matches the live intent version. Off
// pending, or version mismatch → false (the caller no-ops; nothing spawns).
func CanApproveOutsourceIntent(taskStatus string, currentVersion, expectedVersion int64) bool {
	return taskStatus == TaskStatusPendingOutsourceApproval && currentVersion == expectedVersion
}

// OutsourceIntentIdempotencyKey derives the dedupe key for a dispatch intent:
// (task id + intent version). Stable per (task, version), so a retried approve
// of the same intent collapses to one effect and a re-dispatch (new version)
// gets a fresh key.
func OutsourceIntentIdempotencyKey(taskID string, version int64) string {
	return fmt.Sprintf("%s:%d", taskID, version)
}

// CanDispatchOutsource is the no-double-dispatch guard: a task already awaiting
// approval cannot be re-dispatched. Tied to the entry side of
// outsourceApprovalTransitions (single source of truth), so it is true only
// from the legal dispatch source states and — crucially — false while already
// at pending_outsource_approval.
func CanDispatchOutsource(taskStatus string) bool {
	return CanOutsourceApprovalTransition(taskStatus, TaskStatusPendingOutsourceApproval)
}
