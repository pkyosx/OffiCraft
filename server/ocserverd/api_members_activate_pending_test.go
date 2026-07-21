package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-ba62: the activate RESPONSE must stop lying, exactly as relocate stopped
// lying in T-8655. `activate` dropped reconcileMemberNow's return value on the
// floor, so a wake whose START could not be handed to the target warden (the
// machine's warden never installed, or its SSE down) answered a clean 200 with
// a plain member body — byte-indistinguishable from a wake that actually
// started. The intent always persists, so the status stays 200; an undelivered
// dispatch now surfaces activation_pending=true.
//
// Both edges are pinned. The pair is the red/green guard: an "always set
// pending" mutant reddens the landed case, a "never set pending" mutant reddens
// the unlanded case, and neither could be caught by either test alone.

func TestActivateMember_UnlandedSurfacesPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-dead") // exists in the roster, but never connects

	m := testAgent("m-sleepy")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-dead"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-sleepy/activate",
			map[string]any{}, wireOwnerID, "owner"), "m-sleepy")

	if rec.Code != http.StatusOK {
		t.Fatalf("an unlanded activate still 200s (the intent persisted): %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-sleepy"); got == nil || got.DesiredState != DesiredStateOnline {
		t.Fatalf("the wake intent must persist even when dispatch did not: %+v", got)
	}
	var body struct {
		ActivationPending *bool `json:"activation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if body.ActivationPending == nil || !*body.ActivationPending {
		t.Fatalf("an undeliverable START must surface activation_pending=true, got %v (%s)",
			body.ActivationPending, rec.Body.String())
	}
}

// The positive control: a REACHABLE warden takes the START, so the response must
// NOT carry activation_pending (omitempty → the key is absent).
func TestActivateMember_LandedNoPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live") // the warden holds its SSE downstream

	m := testAgent("m-sleepy")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-sleepy/activate",
			map[string]any{}, wireOwnerID, "owner"), "m-sleepy")

	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if _, present := raw["activation_pending"]; present {
		t.Fatalf("a LANDED activate must not report pending: %s", rec.Body.String())
	}
	// Sanity: the START really was dispatched (otherwise the assertion above
	// would pass for the wrong reason — nothing decided at all).
	if frames := drainFrames(t, s, "mach-live"); len(frames) == 0 {
		t.Fatalf("expected a START frame on the warden FIFO; got none")
	}
}

// R4: the failure mode the first cut missed. reconcileOne downgrades a START to
// `none` when buildStartFrame cannot assemble a payload — and does NOT set
// DispatchUnlanded on that path. So a REACHABLE warden plus an unbuildable
// frame answered a clean 200 with no pending flag while nothing was dispatched:
// a silent false success inside the code written to remove silent false
// successes. The fix asks positively whether a START went out.
func TestActivateMember_UnbuildableFrameSurfacesPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live") // the warden IS reachable

	m := testAgent("m-noroleprofile")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-live"
	m.RoleKey = "no-such-role-exists" // buildStartFrame fails closed on this
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-noroleprofile/activate",
			map[string]any{}, wireOwnerID, "owner"), "m-noroleprofile")

	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	// Sanity: nothing was dispatched (otherwise the assertion below would be
	// checking a case that never occurred).
	if frames := drainFrames(t, s, "mach-live"); len(frames) != 0 {
		t.Fatalf("fixture is wrong — a START did land: %+v", frames)
	}
	var body struct {
		ActivationPending *bool `json:"activation_pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if body.ActivationPending == nil || !*body.ActivationPending {
		t.Fatalf("an undispatched wake must surface activation_pending=true even when "+
			"the warden is reachable, got %v (%s)", body.ActivationPending, rec.Body.String())
	}
}

// Positive control for the "or already online" arm: activating a member who is
// ALREADY online decides `none` legitimately (nothing to start), and that must
// not be reported as pending.
func TestActivateMember_AlreadyOnlineIsNotPending(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-awake")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)
	connectOnline(t, s, "m-awake") // she holds her own SSE = online

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-awake/activate",
			map[string]any{}, wireOwnerID, "owner"), "m-awake")

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := raw["activation_pending"]; present {
		t.Fatalf("an already-online member must not report pending: %s", rec.Body.String())
	}
}
