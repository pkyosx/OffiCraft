package main

import "testing"

// T-8655: relocation DISPATCH OUTCOME at the decision layer. The relocate
// handler exposes the pin immediately, but the recycle STOP that actually moves
// a LIVE member can fail to land when the old-machine warden is unreachable.
// reconcileMemberNow now RETURNS its decision so the handler can surface
// relocation_pending instead of a silent 200 success. These tests pin
// DispatchUnlanded on both edges through the real reconcileMemberNow path
// (hub-driven observation + dispatch); the HTTP-response twin lives in
// api_members_relocate_pending_test.go.

// online member running on mach-old, owner-pinned to mach-new, with the
// OLD-machine warden OFFLINE → the relocation STOP cannot be delivered → the
// decision is DispatchUnlanded and the command downgrades to none (retry next
// tick).
func TestReconcileMemberNowRelocationUnlanded(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")

	m := testAgent("m-a")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, "m-a", "mach-old") // running on the OLD machine
	// the old-machine warden is NOT connected → enqueueToWarden("mach-old") fails closed

	dec := s.reconcileMemberNow("m-a")
	if !dec.DispatchUnlanded {
		t.Fatalf("want DispatchUnlanded=true when the old-machine warden is offline, got false (cmd=%s)", dec.Command)
	}
	if dec.Command != reconcileCmdNone {
		t.Fatalf("an unlanded relocation STOP must downgrade to none, got %s", dec.Command)
	}
}

// same divergence but the OLD-machine warden IS online → the STOP lands, so the
// move is NOT pending and it must route to the RUNNING (old) machine's warden.
func TestReconcileMemberNowRelocationLands(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	m := testAgent("m-b")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-old")               // old-machine warden online → the STOP can be delivered
	connectOnlineMachine(t, s, "m-b", "mach-old") // the mover runs on the OLD machine

	dec := s.reconcileMemberNow("m-b")
	if dec.DispatchUnlanded {
		t.Fatalf("want DispatchUnlanded=false when the old-machine warden is reachable")
	}
	if dec.Command != reconcileCmdStop {
		t.Fatalf("want relocation STOP dispatched, got %s", dec.Command)
	}
	if dec.DispatchWarden != "mach-old" {
		t.Fatalf("relocation STOP must route to the running-machine warden mach-old, got %q", dec.DispatchWarden)
	}
}
