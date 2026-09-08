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
	s.telemetry.Set(m.ID, map[string]any{"cost": 3.25})

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
	if got, _ := s.dal.GetMember(m.ID); got == nil || got.BankedCost != 3.25 {
		t.Fatalf("offline activate must bank the replaced generation's cost: %+v", got)
	}
	if _, ok := s.telemetry.Get(m.ID)["cost"]; ok {
		t.Fatal("offline activate left the replaced generation's live cost behind")
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
	// Sanity: the replacement START did not land. Offline activate may still
	// dispatch its stop-before-start handoff before the frame build fails.
	for _, frame := range drainFrames(t, s, "mach-live") {
		if frame.RPC == reconcileCmdStart {
			t.Fatalf("fixture is wrong — a START did land: %+v", frame)
		}
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

// TestActivateMember_RejectsUnresolvableMachine: activate is a placement write
// face like relocate, and the one that most needs the rule — it flips
// desired_state=online in the SAME call, so an unresolvable pin accepted here
// manufactures a member that wants to be online and can never be dispatched.
// Any non-blank machine_id must name a real machine; "" still clears the pin.
func TestActivateMember_RejectsUnresolvableMachine(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-real")
	connectOnline(t, s, "mach-real")

	m := testAgent("m-act")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-real"
	putTestMember(t, s, m)

	activate := func(body map[string]any) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
			taskReq(t, "POST", "/api/members/m-act/activate", body, wireOwnerID, "owner"),
			"m-act")
		return rec
	}

	for _, machineID := range []string{"auto", "mach-ghost"} {
		if rec := activate(map[string]any{"machine_id": machineID}); rec.Code != http.StatusNotFound {
			t.Fatalf("machine_id %q must 404, got %d %s", machineID, rec.Code, rec.Body.String())
		}
		got, _ := s.dal.GetMember("m-act")
		if got == nil || got.DesiredMachineID != "mach-real" {
			t.Fatalf("a refused %q activate must leave the pin alone: %+v", machineID, got)
		}
		if got.DesiredState != DesiredStateOffline {
			t.Fatalf("a refused %q activate must not flip desired_state: %+v", machineID, got)
		}
	}

	// SENTINEL: a real machine id still activates and stores the pin, so the
	// refusals above are the validation, not a broken fixture.
	if rec := activate(map[string]any{"machine_id": "mach-real"}); rec.Code != http.StatusOK {
		t.Fatalf("concrete machine activate must 200, got %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-act")
	if got == nil || got.DesiredMachineID != "mach-real" || got.DesiredState != DesiredStateOnline {
		t.Fatalf("a resolvable pin must activate and store: %+v", got)
	}

	// "" is the legal clear — the member is woken with no placement at all.
	if rec := activate(map[string]any{"machine_id": ""}); rec.Code != http.StatusOK {
		t.Fatalf("clearing the pin must 200, got %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-act"); got == nil || got.DesiredMachineID != "" {
		t.Fatalf("\"\" must clear the pin: %+v", got)
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
	m.StoppingSince = 1001
	m.StoppedSince = 1002
	m.RefocusSince = 1000
	m.RefocusOp = refocusOpRefocus
	m.WakingSince = 1003
	m.RestartAfterStop = true
	putTestMember(t, s, m)
	memberSession := connectOnline(t, s, "m-awake") // she holds her own SSE = online
	drainHubFrames(memberSession)
	drainFrames(t, s, "mach-live")

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
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-awake")
	if after == nil {
		t.Fatal("activate removed the already-online member")
	}
	if after.StoppingSince != 0 || after.WakingSince != 0 {
		t.Fatalf("online activate must clear stop/wake anchors: %+v", after)
	}
	if after.StoppedSince != 1002 || after.RefocusSince != 1000 || after.RefocusOp != refocusOpRefocus {
		t.Fatalf("online activate must preserve the active wind-down epoch: %+v", after)
	}
	if after.RestartAfterStop {
		t.Fatalf("online activate must consume restart_after_stop: %+v", after)
	}
	assertNoFrame(t, memberSession, "online activate")
	if frames := drainFrames(t, s, "mach-live"); len(frames) != 0 {
		t.Fatalf("online activate must not dispatch a replacement: %+v", frames)
	}
}
