// Skeleton generated from server/ocserverd/outsource_sched.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestOutsourceAwaitingAssignment(t *testing.T) {
	t.Skip("TODO: outsourceAwaitingAssignment reports whether a task is an UNASSIGNED outsource slot the scheduler should mint for: an outsource track with no bound executor, not frozen, and either not_started (a fresh create / a fully-reset reassign) OR held under the reassigning lock (a handover successor slot — a partially-done task reassigned to outsource derives to in_progress, so status alone would miss it; the lock is the honest \"awaiting the successor mint\" signal).")
}

func TestTaskPriorityRank(t *testing.T) {
	t.Skip("TODO: taskPriorityRank orders the queue: high before mid before low; frozen (and any junk) sorts last AND is skipped by the decide loop — a frozen task is never assigned (SPEC §3.3: the scheduler skips frozen wholesale).")
}

func TestSortOutsourceQueue(t *testing.T) {
	t.Skip("TODO: sortOutsourceQueue orders candidates priority-then-created_ts (task id as the deterministic tie-break).")
}

func TestOutsourceDecide(t *testing.T) {
	t.Skip("TODO: outsourceDecide is the PURE admission function: walk the queue in order and admit every candidate that fits BOTH caps, folding each admission into the running counts so one call never over-assigns (same-tick idempotence).")
}

func TestFillTypeSpecFromSnapshot(t *testing.T) {
	t.Skip("TODO: fillTypeSpecFromSnapshot folds a manual-driven candidate's creator snapshot (task.outsource_*, written at create time — T-8a67) into the fields the LIVE type manual leaves unset.")
}

func TestOutsourceSpecOf(t *testing.T) {
	t.Skip("TODO: outsourceSpecOf extracts a manual's outsource assignee spec: nil for {} / a member assignee / undecodable JSON.")
}

func TestOutsourceLog(t *testing.T) {
	t.Skip("TODO: ── logging ──────────────────────────────────────────────────────────────────")
}

func TestRunOutsourceTick(t *testing.T) {
	t.Skip("TODO: ── the tick (snapshot → decide → mint/bind/fan) ───────────────────────────── runOutsourceTick runs ONE scheduler tick: snapshot the queue + the live worker counts from the DB, decide, then mint/bind/fan each assignment.")
}

func TestOutsourceTickNow(t *testing.T) {
	t.Skip("TODO: notifyWorkerSpawn (the former Phase 6 seam) now lives in worker_spawn.go: it assembles the worker boot context, server-mints the ow- token, and pushes a worker_start frame onto an online warden's command FIFO.")
}
