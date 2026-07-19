package main

// reconcile_identity_sweep_test.go — the cross-machine single-session sweep
// (T-bb29 §1-2, owner-approved rc-2230cb0158e8): the connection-edge 正身 sweep
// that reaps a same-id residual session on non-desired machines the instant the
// real 正身 is confirmed on its desired machine — never touching the just-
// confirmed session (the never-zero-live invariant).

import "testing"

// wardenOnline registers a warden member + brings it online, returns a cleanup.
func wardenOnline(t *testing.T, api *apiServer, dal *DAL, id string) func() {
	t.Helper()
	putGateMember(t, dal, Member{ID: id, Kind: KindWarden, DesiredState: DesiredStateOffline})
	l, err := api.hub.Connect(id, "")
	if err != nil {
		t.Fatalf("warden %s connect: %v", id, err)
	}
	return func() { api.hub.Disconnect(l) }
}

func TestIdentitySweepReapsResidualOnOtherMachinesOnly(t *testing.T) {
	api, dal := newGateTestAPI(t)
	// The 正身 is desired online on w-desired; two other wardens are online.
	putGateMember(t, dal, Member{ID: "m-1", Kind: KindAssistant,
		DesiredState: DesiredStateOnline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other-a")()
	defer wardenOnline(t, api, dal, "w-other-b")()

	// 正身 connects on its desired machine → sweep the OTHER machines.
	api.identitySweepOnConnect("m-1", "w-desired")

	// keepWarden (the desired machine) must NEVER be swept — never-zero invariant.
	if got := api.hub.DrainWardenCommands("w-desired"); len(got) != 0 {
		t.Fatalf("the 正身's own machine must never be swept, got %d frames", len(got))
	}
	// Every OTHER online warden gets exactly one robust stop for member-<id>.
	for _, w := range []string{"w-other-a", "w-other-b"} {
		got := api.hub.DrainWardenCommands(w)
		if len(got) != 1 {
			t.Fatalf("warden %s must get exactly one residual-stop frame, got %d", w, len(got))
		}
	}
}

func TestIdentitySweepDoesNotFireForWanderer(t *testing.T) {
	api, dal := newGateTestAPI(t)
	// Desired machine is w-desired, but this connection's claim is w-other (an
	// old instance from before a relocate) — it is a sweep TARGET, not initiator.
	putGateMember(t, dal, Member{ID: "m-2", Kind: KindAssistant,
		DesiredState: DesiredStateOnline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other")()

	api.identitySweepOnConnect("m-2", "w-other") // claim != desired

	for _, w := range []string{"w-desired", "w-other"} {
		if got := api.hub.DrainWardenCommands(w); len(got) != 0 {
			t.Fatalf("a wanderer connect must NOT initiate a sweep; warden %s got %d", w, len(got))
		}
	}
}

func TestIdentitySweepSkipsWhenDesiredOffline(t *testing.T) {
	api, dal := newGateTestAPI(t)
	// desired_state offline → no 正身 to establish, no sweep.
	putGateMember(t, dal, Member{ID: "m-3", Kind: KindAssistant,
		DesiredState: DesiredStateOffline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other")()

	api.identitySweepOnConnect("m-3", "w-desired")

	if got := api.hub.DrainWardenCommands("w-other"); len(got) != 0 {
		t.Fatalf("desired-offline member must not sweep, got %d", len(got))
	}
}

func TestIdentitySweepDedupesWithinWindow(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "m-4", Kind: KindAssistant,
		DesiredState: DesiredStateOnline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other")()

	api.identitySweepOnConnect("m-4", "w-desired")
	if got := api.hub.DrainWardenCommands("w-other"); len(got) != 1 {
		t.Fatalf("first sweep must broadcast once, got %d", len(got))
	}
	// A reconnect flap within the dedupe window must NOT re-broadcast.
	api.identitySweepOnConnect("m-4", "w-desired")
	if got := api.hub.DrainWardenCommands("w-other"); len(got) != 0 {
		t.Fatalf("a reconnect within the dedupe window must not re-sweep, got %d", len(got))
	}
}

// A案 P6 (the seth-m1 doppelganger fix, 2026-07-19): an OUTSOURCE member now
// rides the identity sweep too — the P5b naming convergence made its sessions
// member-<ow-id>, so the member-verb stop can target the residuals a spawn
// retry left behind on other machines. The pinned-target case mirrors the
// staff sweep verbatim.
func TestIdentitySweepReapsOutsourceResidual_PinnedTarget(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "ow-sweep", Kind: KindOutsource,
		DesiredState: DesiredStateOnline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other")()

	api.identitySweepOnConnect("ow-sweep", "w-desired")

	// The just-confirmed 正身's own machine is never swept (never-zero invariant).
	if got := api.hub.DrainWardenCommands("w-desired"); len(got) != 0 {
		t.Fatalf("the worker 正身's own machine must never be swept, got %d frames", len(got))
	}
	// Every OTHER online warden gets the residual stop — the active-period
	// cross-machine clone (seth-m1: a live doppelganger squatting 1h53m with no
	// reclamation) is reaped the moment the 正身 connects.
	if got := api.hub.DrainWardenCommands("w-other"); len(got) != 1 {
		t.Fatalf("the other machine must get exactly one residual-stop frame, got %d", len(got))
	}
}

// An UNPINNED worker ("auto"/"" desired machine) verifies its 正身 against the
// machine the server ACTUALLY dispatched the last start to (workerSpawnTarget).
// A matching claim sweeps; a stale-claim wanderer, and a worker with NO known
// dispatch target (restart amnesia), never initiate one — fail-safe.
func TestIdentitySweepReapsOutsourceResidual_AutoPlacement(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "ow-auto", Kind: KindOutsource,
		DesiredState: DesiredStateOnline, DesiredMachineID: ""})
	defer wardenOnline(t, api, dal, "w-dispatched")()
	defer wardenOnline(t, api, dal, "w-stale")()

	// No dispatch observation (restart amnesia) → an unverifiable 正身 → no sweep.
	api.identitySweepOnConnect("ow-auto", "w-dispatched")
	if got := api.hub.DrainWardenCommands("w-stale"); len(got) != 0 {
		t.Fatalf("no known dispatch target must mean no sweep, got %d", len(got))
	}

	api.outsourceMu.Lock()
	api.workerSpawnTarget["ow-auto"] = "w-dispatched"
	api.outsourceMu.Unlock()

	// A wanderer claim (an old clone on w-stale reconnecting) must not sweep.
	api.identitySweepOnConnect("ow-auto", "w-stale")
	if got := api.hub.DrainWardenCommands("w-dispatched"); len(got) != 0 {
		t.Fatalf("a wanderer clone must not initiate a sweep, got %d", len(got))
	}

	// The 正身 on the dispatched machine sweeps the stale host only.
	api.identitySweepOnConnect("ow-auto", "w-dispatched")
	if got := api.hub.DrainWardenCommands("w-dispatched"); len(got) != 0 {
		t.Fatalf("the dispatched machine must never be swept, got %d", len(got))
	}
	if got := api.hub.DrainWardenCommands("w-stale"); len(got) != 1 {
		t.Fatalf("the stale host must get exactly one residual stop, got %d", len(got))
	}
}

func TestIdentitySweepGatedByNoReconcile(t *testing.T) {
	api, dal := newGateTestAPI(t)
	api.noReconcile = true
	putGateMember(t, dal, Member{ID: "m-5", Kind: KindAssistant,
		DesiredState: DesiredStateOnline, DesiredMachineID: "w-desired"})
	defer wardenOnline(t, api, dal, "w-desired")()
	defer wardenOnline(t, api, dal, "w-other")()

	api.identitySweepOnConnect("m-5", "w-desired")

	if got := api.hub.DrainWardenCommands("w-other"); len(got) != 0 {
		t.Fatalf("--no-reconcile must gate the sweep off, got %d", len(got))
	}
}
