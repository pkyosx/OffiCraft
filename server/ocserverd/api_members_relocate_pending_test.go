package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-8655: the relocate RESPONSE must stop lying. Before this ticket the handler
// dispatched the recycle move best-effort and ALWAYS 200'd with a plain member
// body, so a relocation whose STOP could not be delivered (old-machine warden
// unreachable) looked identical to a landed one — a silent false-success in the
// owner cockpit. The pin always lands (persisted before dispatch) so the status
// stays 200, but an undelivered dispatch now surfaces relocation_pending=true.
//
// These two tests pin BOTH edges of that observable through the real HTTP
// handler (the decision-layer twin lives in reconcile_relocate_dispatch_test.go):
// unreachable → pending=true, reachable → pending absent. The pair is the
// red/green guard — a "always set pending" mutant reddens the landed case, a
// "never set pending" mutant reddens the unlanded case.
//
// ⚠️ T-b6d9 RETARGETED BOTH FIXTURES, deliberately. A live member's 改機器 now
// opens a graceful wind-down and dispatches NOTHING on the click, so the state
// these two are about — "the relocation STOP went out and did / did not land" —
// is no longer reachable from a plain online member. It is still reachable, and
// still exercised here, at the arm where the verb takes effect IMMEDIATELY:
// this epoch's wind-down is already collected (refocus_since > 0 ∧
// stopped_since > 0) while the old session has not been reaped yet, so
// decideUp's recycle arm dispatches the robust STOP on the spot. The
// discriminating pair is preserved; only the fixture that reaches it moved.
// The wind-down arm's own pending contract is pinned by
// TestRelocateMember_WindDownIsAlsoPending / _OfflineRelocateIsNotPending below.

// online member re-pinned to a new machine while the OLD machine's warden (which
// holds the session to STOP) is UNREACHABLE → the pin lands, status is 200, and
// the body carries relocation_pending=true ("move scheduled, not yet landed").
func TestRelocateMember_UnlandedSurfacesPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	mover := testAgent("m-stuck")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	mover.RefocusSince = 1000.0 // this epoch's wind-down is already collected, so
	mover.StoppedSince = 1001.0 // the verb takes effect immediately (no new window)
	putTestMember(t, s, mover)
	connectOnlineMachine(t, s, "m-stuck", "mach-old") // the mover runs on the OLD machine
	// The OLD machine's warden is deliberately NOT connected: the relocation STOP
	// fails closed and cannot be delivered this instant.

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-stuck/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-stuck")

	// The pin always lands, so the relocate never FAILS on dispatch — still 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("an unlanded relocation still 200s (the pin persisted): %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-stuck"); got == nil || got.DesiredMachineID != "mach-new" {
		t.Fatalf("the pin must land even when dispatch did not: %+v", got)
	}
	// The fix: the caller is told the move has not landed yet.
	var body struct {
		RelocationPending *bool `json:"relocation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending == nil || !*body.RelocationPending {
		t.Fatalf("an undeliverable relocation STOP must surface relocation_pending=true, got %v (%s)",
			body.RelocationPending, rec.Body.String())
	}
}

// same divergence but the OLD machine's warden IS reachable → the STOP lands, so
// the response must NOT carry relocation_pending (omitempty → the field is
// absent). Guards against a mutant that pins pending unconditionally.
func TestRelocateMember_LandedNoPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	mover := testAgent("m-ok")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	mover.RefocusSince = 1000.0 // already collected → the verb takes effect NOW
	mover.StoppedSince = 1001.0 // (see the file header on the T-b6d9 retarget)
	putTestMember(t, s, mover)
	connectOnline(t, s, "mach-old")                // old warden reachable → the STOP can land
	connectOnlineMachine(t, s, "m-ok", "mach-old") // the mover runs on the OLD machine

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-ok/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RelocationPending *bool `json:"relocation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending != nil {
		t.Fatalf("a landed relocation must NOT carry relocation_pending, got %v (%s)",
			*body.RelocationPending, rec.Body.String())
	}
}

// T-b6d9, the wind-down arm's own pending contract. A LIVE member's 改機器 now
// dispatches nothing on the click — the move happens at the 收口. That is
// literally "scheduled, not yet landed", so the response must say pending.
// Answering a clean landed 200 here would be the same silent false-success
// T-8655 removed for the unreachable-warden case, just with a different cause.
func TestRelocateMember_WindDownIsAlsoPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	mover := testAgent("m-grace")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	putTestMember(t, s, mover)
	connectOnline(t, s, "mach-old") // fully reachable: nothing here is "unlanded"
	connectOnline(t, s, "mach-new")
	connectOnlineMachine(t, s, "m-grace", "mach-old")

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-grace/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-grace")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	// Positive control that this really IS the wind-down arm and not an unlanded
	// dispatch wearing its clothes: the epoch is stamped and no kill went out.
	if got, _ := s.dal.GetMember("m-grace"); got == nil || got.RefocusSince <= 0.0 {
		t.Fatalf("expected an open wind-down (refocus epoch stamped): %+v", got)
	}
	if f := drainFrames(t, s, "mach-old"); len(f) != 0 {
		t.Fatalf("the wind-down dispatches nothing on the click: %+v", f)
	}
	var body struct {
		RelocationPending *bool `json:"relocation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending == nil || !*body.RelocationPending {
		t.Fatalf("a 改機器 still winding down must surface relocation_pending=true, got %v (%s)",
			body.RelocationPending, rec.Body.String())
	}
}

// …and the誤擋 half: an OFFLINE member's re-pin moves nothing and waits for
// nothing, so it must NOT claim to be pending. Without this, "pending" could be
// hard-wired true and the pair above would still pass.
func TestRelocateMember_OfflineRelocateIsNotPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	m := testAgent("m-parked")
	// Deliberately desired-OFFLINE: nothing is decided, nothing is dispatched, so
	// neither pending cause can fire. (A desired-ONLINE member with no session is
	// a different story — reconcile tries to START it, and an unreachable target
	// warden there is a genuine unlanded dispatch, pending since T-8655.)
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-parked/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-parked")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RelocationPending *bool `json:"relocation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending != nil {
		t.Fatalf("re-pinning a member with no session is not a pending move, got %v (%s)",
			*body.RelocationPending, rec.Body.String())
	}
	// The flag alone cannot tell this arm apart from one that moved the member
	// or queued a start: assert the row it left behind. testAgent carries no
	// stop anchor, so this is the never-活化'd new hire — the half that is still
	// waiting on the owner.
	got, err := s.dal.GetMember("m-parked")
	if err != nil || got == nil {
		t.Fatalf("re-read m-parked: %v", err)
	}
	if got.DesiredMachineID != "mach-new" {
		t.Errorf("the pin is the only thing this verb does and it was not stored: "+
			"desired_machine_id = %q", got.DesiredMachineID)
	}
	if got.DesiredState != DesiredStateOffline || got.RestartAfterStop {
		t.Errorf("re-pinning a member nobody ever asked to stop must not start it: "+
			"desired=%q restart_after_stop=%v", got.DesiredState, got.RestartAfterStop)
	}
	if want := memberHeldDownReceipt(memberOpRelocate); got.LastOpReason != want {
		t.Errorf("last_op_reason = %q, want %q — the pin was stored and nothing was "+
			"moved, and this member is the one the owner still has to 活化",
			got.LastOpReason, want)
	}
}

// T-927a: `relocation_pending` is true for TWO unrelated situations, and the
// cockpit alerts on only one of them. So the wire has to say WHICH — otherwise
// the perfectly ordinary wind-down case raises a "nothing was dispatched" alarm,
// and an alarm that fires on normal operation is one everybody learns to ignore.
//
// The pair below is the discriminating one: same field pair, both causes, and
// each direction is asserted, because "deferred is always true" and "deferred is
// never true" are both mutants that a single test would let through.
func TestRelocateMember_WindDownIsDeferred(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	mover := testAgent("m-grace2")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	putTestMember(t, s, mover)
	connectOnline(t, s, "mach-old")
	connectOnline(t, s, "mach-new")
	connectOnlineMachine(t, s, "m-grace2", "mach-old")

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-grace2/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-grace2")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	// Positive control that this is really the wind-down arm: the epoch is
	// stamped and nothing was sent to the old machine.
	if got, _ := s.dal.GetMember("m-grace2"); got == nil || got.RefocusSince <= 0.0 {
		t.Fatalf("expected an open wind-down (refocus epoch stamped): %+v", got)
	}
	if f := drainFrames(t, s, "mach-old"); len(f) != 0 {
		t.Fatalf("the wind-down dispatches nothing on the click: %+v", f)
	}
	var body struct {
		RelocationPending  *bool `json:"relocation_pending"`
		RelocationDeferred *bool `json:"relocation_deferred"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending == nil || !*body.RelocationPending {
		t.Fatalf("the wind-down is still 'not landed', so pending stays true: %v (%s)",
			body.RelocationPending, rec.Body.String())
	}
	if body.RelocationDeferred == nil || !*body.RelocationDeferred {
		t.Fatalf("a deliberately deferred move must say so: relocation_deferred=%v (%s)",
			body.RelocationDeferred, rec.Body.String())
	}
}

// …and the other direction: a move that genuinely could not be delivered is NOT
// deferred, so the field must be ABSENT (omitempty) and the cockpit must still
// alert. Without this half, hard-wiring deferred=true would silence the alert
// everywhere and the test above would stay green.
func TestRelocateMember_UndeliverableIsNotDeferred(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	mover := testAgent("m-stuck2")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
	mover.RefocusSince = 1000.0 // this epoch's wind-down is already collected, so
	mover.StoppedSince = 1001.0 // the verb takes effect immediately (no new window)
	putTestMember(t, s, mover)
	connectOnlineMachine(t, s, "m-stuck2", "mach-old")
	// The OLD machine's warden is deliberately NOT connected.

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-stuck2/relocate",
			map[string]any{"machine_id": "mach-new"}, wireOwnerID, "owner"), "m-stuck2")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RelocationPending  *bool `json:"relocation_pending"`
		RelocationDeferred *bool `json:"relocation_deferred"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode relocate response: %v", err)
	}
	if body.RelocationPending == nil || !*body.RelocationPending {
		t.Fatalf("an undeliverable relocation STOP still surfaces pending: %v (%s)",
			body.RelocationPending, rec.Body.String())
	}
	if body.RelocationDeferred != nil {
		t.Fatalf("an undeliverable move is a failure, not a deferral: relocation_deferred=%v (%s)",
			*body.RelocationDeferred, rec.Body.String())
	}
}
