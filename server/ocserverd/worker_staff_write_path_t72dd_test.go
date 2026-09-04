package main

import "testing"

// T-72dd FOUNDATION: does an outsource row actually accept a
// write through the STAFF path (putMember(memberFromWorker(w))) and read back
// through the WORKER path (dal.GetOutsourceWorker)?
func TestWorkerRowTakesTheStaffWritePath_T72dd(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// ── NEGATIVE CONTROL (a): do not write — the read must show the OLD value.
	pre, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || pre == nil {
		t.Fatalf("read back before the write: %v", err)
	}
	t.Logf("CONTROL-a (no write yet): refocus_since=%v refocus_op=%q stopped_since=%v desired=%q",
		pre.RefocusSince, pre.RefocusOp, pre.StoppedSince, pre.DesiredState)
	if pre.RefocusSince != 0 || pre.RefocusOp != "" {
		t.Fatalf("control-a broken: the fixture already carries an epoch "+
			"(since=%v op=%q) — a later PASS would prove nothing",
			pre.RefocusSince, pre.RefocusOp)
	}

	// ── THE WRITE, through the staff path.
	//
	// ⚠️ THE PROBE COLUMN MOVED (T-55 batch C). This used to prove the staff path
	// reaches the worker row by writing the wind-down anchors through it — and
	// those four have since left PutMember's DO UPDATE SET, so that write is now
	// a no-op for them and the old form would have asserted the opposite of the
	// truth. last_machine_id is the current choice because the whole-row write
	// still carries it; the assertion below is what catches the day it stops
	// being carried too. If you are here because that fired, pick another CARRIED
	// column — never delete the check.
	m := memberFromWorker(*pre)
	m.RefocusSince = 1234.5
	m.RefocusOp = refocusOpRefocus
	m.StoppedSince = 777.25
	m.LastMachineID = "m-t72dd-probe"
	if err := api.putMember(m, "t72dd-probe"); err != nil {
		t.Fatalf("putMember(memberFromWorker(w)): %v", err)
	}

	// ── THE READ, through the worker path (NOT the member path).
	mid, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || mid == nil {
		t.Fatalf("GetOutsourceWorker after the staff write: %v", err)
	}
	t.Logf("AFTER staff write, read via dal.GetOutsourceWorker: last_machine_id=%q "+
		"refocus_since=%v refocus_op=%q stopped_since=%v desired=%q",
		mid.LastMachineID, mid.RefocusSince, mid.RefocusOp, mid.StoppedSince, mid.DesiredState)
	if mid.LastMachineID != "m-t72dd-probe" {
		t.Fatalf("the staff write did NOT land on the worker row: last_machine_id=%q",
			mid.LastMachineID)
	}
	// 🔴 AND THE ANCHORS DID NOT RIDE IT. This half is new and it is the T-55
	// invariant: a whole-row writer holding a stale snapshot must not be able to
	// move these four, which is exactly what this staff-path write is.
	if mid.RefocusSince != 0 || mid.RefocusOp != "" || mid.StoppedSince != 0 {
		t.Fatalf("the wind-down anchors rode the whole-row write — since T-55 only "+
			"SetMemberWindDownAnchors may move them: since=%v op=%q stopped=%v",
			mid.RefocusSince, mid.RefocusOp, mid.StoppedSince)
	}

	// The epoch the rest of this test needs, planted through its sole writer.
	if err := api.dal.SetMemberWindDownAnchors(workerID, m.StoppingSince,
		m.StoppedSince, m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("plant the epoch through its sole writer: %v", err)
	}
	got, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || got == nil {
		t.Fatalf("GetOutsourceWorker after the anchor write: %v", err)
	}
	if got.RefocusSince != 1234.5 || got.RefocusOp != refocusOpRefocus || got.StoppedSince != 777.25 {
		t.Fatalf("the anchor write did NOT land on the worker row: "+
			"since=%v op=%q stopped=%v", got.RefocusSince, got.RefocusOp, got.StoppedSince)
	}

	// ── NEGATIVE CONTROL (b): a row that does not exist must read as nil.
	ghost, err := api.dal.GetOutsourceWorker("ow-no-such-worker")
	t.Logf("CONTROL-b (unknown id): row=%v err=%v", ghost, err)
	if err == nil && ghost != nil {
		t.Fatalf("control-b broken: GetOutsourceWorker invented a row for an unknown id")
	}

	// ── The 預告 half: can a refocus stamp reach an outsource projection at all?
	if !aRefocusStampWouldReachTheAgent(m) {
		t.Fatalf("aRefocusStampWouldReachTheAgent said NO for desired=%q", m.DesiredState)
	}
	proj := memberFromWorker(*got)
	kind, carries := offboardKindOf(proj, nowSecs())
	payload := api.offboardDeltaPayload(proj)
	notice, hasNotice := payload["offboard_notice"].(string)
	t.Logf("PROJECTION: kind=%q carries=%v hasNotice=%v desired=%q forced_stop_at=%v",
		kind, carries, hasNotice, proj.DesiredState, proj.ForcedStopAt)
	t.Logf("NOTICE(first 160): %.160s", notice)
	if !hasNotice {
		t.Fatalf("a refocus-stamped OUTSOURCE projection carried NO 預告 — payload=%v", payload)
	}

	// ── NEGATIVE CONTROL (c) for the notice: a worker with no epoch at all
	// must NOT carry one, or the assertion above passes for everything.
	clean := memberFromWorker(*pre)
	if _, ok := api.offboardDeltaPayload(clean)["offboard_notice"]; ok {
		t.Fatalf("control-c broken: an un-stamped worker also carries a notice")
	}
	t.Logf("CONTROL-c (no epoch): no offboard_notice — the notice check discriminates")
}

// T-72dd: is an EMPTY desired_state a state a worker row can actually hold?
// It matters because un-blinding routes "" to decideDown (the switch's default
// arm), so a "" row would stop being spawned.
func TestWorkerEmptyDesiredStateRoundTrips_T72dd(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	w := OutsourceWorker{ID: "ow-empty", Codename: "E-1", Model: "claude-sonnet-4-5",
		Effort: "medium", TaskID: "", Status: WorkerStatusAssigned}
	if err := api.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := api.dal.GetOutsourceWorker("ow-empty")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("EMPTY desired_state written -> read back as %q (column DEFAULT did NOT rescue it: %v)",
		got.DesiredState, got.DesiredState == "")
	t.Logf("reconcileDecide routing for %q: goes to decideDown (switch default arm)",
		got.DesiredState)
}
