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

// online member re-pinned to a new machine while the OLD machine's warden (which
// holds the session to STOP) is UNREACHABLE → the pin lands, status is 200, and
// the body carries relocation_pending=true ("move scheduled, not yet landed").
func TestRelocateMember_UnlandedSurfacesPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	mover := testAgent("m-stuck")
	mover.DesiredState = DesiredStateOnline
	mover.DesiredMachineID = "mach-old"
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
