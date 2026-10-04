package main

import (
	"net/http"
	"testing"
	"time"
)

// shutdownWarden seeds an ACTIVE machine row and puts a live warden connection
// on it, so the reachability gate will accept a frame aimed there.
func shutdownWarden(t *testing.T, api *apiServer, d *DAL, id string) {
	t.Helper()
	if err := d.putMemberWholeRowForTest(Member{
		ID: id, Name: id, Kind: KindWarden, RosterStatus: RosterStatusActive,
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed warden %s: %v", id, err)
	}
	apiTestListen(t, api, id)
}

// shutdownLastLanding writes the durable last-observed landing the kill chain
// reads, without going through a connection (the stamp's own gate is
// api_infra.go's business, not this file's).
func shutdownLastLanding(t *testing.T, d *DAL, id, machineID string) {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("GetMember(%q): %v (%v)", id, m, err)
	}
	m.LastMachineID = machineID
	if err := d.putMemberWholeRowForTest(*m); err != nil {
		t.Fatalf("PutMember(%q): %v", id, err)
	}
}

// shutdownBootable gives a member the placement and the persona a START needs
// to assemble: the pin the dispatch routes by, and a role whose boot context
// folds (the seeded `engineer` row has no document, which fails closed).
func shutdownBootable(t *testing.T, d *DAL, id, machineID string) {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("GetMember(%q): %v (%v)", id, m, err)
	}
	m.RoleKey = "assistant"
	if err := d.putMemberWholeRowForTest(*m); err != nil {
		t.Fatalf("PutMember(%q): %v", id, err)
	}
	// desired_machine_id is insertOnly, so the whole-row write above cannot move
	// it — the pin has its own single-column writer.
	if err := d.SetMemberDesiredMachineID(id, machineID); err != nil {
		t.Fatalf("SetMemberDesiredMachineID(%q): %v", id, err)
	}
}

// shutdownStaff is an ACTIVE staff member with a live agent credential, no
// live session of its own, and whatever placement the caller asks for. The
// activate's own residual-session kill is drained off wardens, so what a test
// reads afterwards is only what its own report_stopped sent.
func shutdownStaff(t *testing.T, api *apiServer, h http.Handler, d *DAL,
	owner, pin string, wardens ...string,
) string {
	t.Helper()
	if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
		t.Fatalf("activate: %d %v", status, data)
	}
	if err := d.SetMemberDesiredMachineID("kip", pin); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	for _, id := range append(wardens, ServerSelfHost) {
		api.hub.DrainWardenCommands(id)
	}
	return apiTestAgentToken(t, api, "kip", "")
}

func TestDispatchShutdown(t *testing.T) {
	// ── the kill chain's two new sources (T-253 B) ──────────────────────────

	t.Run("a staff kill follows the last landing rather than the pin it was moved off", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-old")
		apiTestListen(t, api, ServerSelfHost)
		agent := shutdownStaff(t, api, h, d, owner, ServerSelfHost, "m-old")
		shutdownLastLanding(t, d, "kip", "m-old")

		status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               "kip",
			"desired_state":    "online",
			"refocus_op":       "",
			"refocus_deadline": 0,
			"stop_effect":      "collected",
		})
		// The session being collected is on the machine it last landed on; the pin
		// already names where the NEXT one goes.
		wsWantWardenFrames(t, api, "m-old", wsStopFrame("kip"))
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("with no last landing the staff kill still falls through to the pin", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-old")
		apiTestListen(t, api, ServerSelfHost)
		agent := shutdownStaff(t, api, h, d, owner, ServerSelfHost, "m-old")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wsWantWardenFrames(t, api, "m-old")
	})

	t.Run("a live machine claim still outranks both, so a moved member is killed where it runs", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-old")
		shutdownWarden(t, api, d, "m-running")
		apiTestListen(t, api, ServerSelfHost)
		agent := shutdownStaff(t, api, h, d, owner, ServerSelfHost, "m-old", "m-running")
		shutdownLastLanding(t, d, "kip", "m-old")
		if _, err := api.hub.Connect("kip", "m-running"); err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-running", wsStopFrame("kip"))
		wsWantWardenFrames(t, api, "m-old")
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("with nothing naming a machine the kill is broadcast to every online warden", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-one")
		shutdownWarden(t, api, d, "m-two")
		agent := shutdownStaff(t, api, h, d, owner, "", "m-one", "m-two")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-one", wsStopFrame("kip"))
		wsWantWardenFrames(t, api, "m-two", wsStopFrame("kip"))
		// The stop that is never a no-op: an OFFLINE machine is not in the fan-out,
		// because the reachability gate refuses a frame nobody would ever drain.
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("with no warden online at all the broadcast has nowhere to go and nothing is sent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.putMemberWholeRowForTest(Member{
			ID: "m-dark", Name: "m-dark", Kind: KindWarden, RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed dark warden: %v", err)
		}
		agent := shutdownStaff(t, api, h, d, owner, "", "m-dark")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-dark")
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("a worker's in-memory spawn target outranks the landing the two populations share", func(t *testing.T) {
		api, h, d, _, session, contractor := apiTestLiveWorker(t)
		shutdownWarden(t, api, d, "m-spawned")
		api.hub.Disconnect(session)
		shutdownLastLanding(t, d, "ow-abc123", ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = "m-spawned"
		api.outsourceMu.Unlock()

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-spawned", wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("a worker with ONLY a last landing is killed there, and the rest of the fleet is left alone", func(t *testing.T) {
		// 🔴 THE ORIGINAL MOTIVATION FOR THE WHOLE TICKET, and until now the one
		// arm with no test: a server re-exec empties the spawn ledger, the worker
		// is offline so there is no live claim, and the durable last landing is
		// the ONLY thing that still knows where its session is. A SECOND warden is
		// online on purpose — with one, the broadcast tail sends to that same
		// machine and the two implementations are indistinguishable.
		api, h, d, _, session, contractor := apiTestLiveWorker(t)
		shutdownWarden(t, api, d, "m-landed")
		shutdownWarden(t, api, d, "m-bystander")
		api.hub.Disconnect(session)
		api.outsourceMu.Lock()
		delete(api.workerSpawnTarget, "ow-abc123") // the re-exec forgot the dispatch
		api.outsourceMu.Unlock()
		shutdownLastLanding(t, d, "ow-abc123", "m-landed")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-landed", wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, "m-bystander")
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("CONTROL: with the last landing wiped the same worker falls through to the broadcast", func(t *testing.T) {
		api, h, d, _, session, contractor := apiTestLiveWorker(t)
		shutdownWarden(t, api, d, "m-landed")
		shutdownWarden(t, api, d, "m-bystander")
		api.hub.Disconnect(session)
		api.outsourceMu.Lock()
		delete(api.workerSpawnTarget, "ow-abc123")
		api.outsourceMu.Unlock()
		shutdownLastLanding(t, d, "ow-abc123", "")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-landed", wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, "m-bystander", wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
	})

	t.Run("an id the roster cannot answer for is still killed: the fan-out carries it", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		apiTestListen(t, api, ServerSelfHost)

		api.dispatchShutdown("ow-no-such-row", "test")

		// An unreadable row must not swallow the stop, and with no source able to
		// name a machine it is the chain's fan-out that carries it.
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-no-such-row"))
	})

	t.Run("a staff shutdown is re-sent by the cadence while the session lingers past stop_retry", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		if err := d.SetMemberDesiredMachineID("kip", ServerSelfHost); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		apiTestListen(t, api, ServerSelfHost)
		api.hub.DrainWardenCommands(ServerSelfHost)

		api.dispatchShutdown("kip", "test")
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))

		apiTestListen(t, api, "kip")
		api.runReconcileTick(nowSecs() + api.reconcileConfigLive().StopRetry + 1)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
	})

	// 🔴 The shutdown takes outsourceMu (the spawn observation) and nothing else
	// of the scheduler's: the stop is recorded in the robust-stop ledger, whose
	// lock is a leaf. So the reconcile tick holding reconcileMu can never stall a
	// handler's kill, and no caller can end up holding both scheduler locks.
	t.Run("a staff stopped-report's kill goes out while the reconcile tick holds its lock", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		if err := d.SetMemberDesiredMachineID("kip", ServerSelfHost); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		apiTestListen(t, api, ServerSelfHost)
		api.hub.DrainWardenCommands(ServerSelfHost)
		agent := apiTestAgentToken(t, api, "kip", "")

		api.reconcileMu.Lock()
		defer api.reconcileMu.Unlock()
		answered := make(chan int, 1)
		go func() {
			answered <- apiRequest(t, h, "POST", "/api/self/stopped", agent, `{}`).Code
		}()
		select {
		case code := <-answered:
			apiWantValue(t, "status", any(float64(code)), any(200))
		case <-time.After(5 * time.Second):
			t.Fatal("the stopped-report blocked on the reconcile lock")
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
	})
}

// shutdownWaitFor polls until ready answers true, bounded well inside the
// package timeout. false means it never did.
func shutdownWaitFor(t *testing.T, ready func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func TestKillTargetChain(t *testing.T) {
	t.Run("a staff kill is addressed to the machine the session actually claims, not the machine it is pinned to", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "pinned", "m-old")
		targets, broadcast := api.killTargetChain("pinned", killTargetSources{})
		apiWantValue(t, "targets", any(map[string]any{"targets": targets, "broadcast": broadcast}),
			any(map[string]any{"targets": []string{"m-old"}, "broadcast": false}))
	})

	t.Run("with no live claim a staff kill falls back to the pin", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		targets, broadcast := api.killTargetChain("pinned", killTargetSources{})
		apiWantValue(t, "targets", any(map[string]any{"targets": targets, "broadcast": broadcast}),
			any(map[string]any{"targets": []string{"m-box"}, "broadcast": false}))
	})

	t.Run("a claim-less connection does not shadow the pin — the blank claim is not an address", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "pinned", "")
		targets, broadcast := api.killTargetChain("pinned", killTargetSources{})
		apiWantValue(t, "targets", any(map[string]any{"targets": targets, "broadcast": broadcast}),
			any(map[string]any{"targets": []string{"m-box"}, "broadcast": false}))
	})

	t.Run("with nothing to name, the kill is broadcast to every online warden", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "unpinned", Name: "U", Kind: KindStaff, RoleKey: "engineer"})
		reconcileTestPut(t, d, Member{ID: "m-dark", Name: "Dark", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-box", "")
		targets, broadcast := api.killTargetChain("unpinned", killTargetSources{})
		apiWantValue(t, "targets", any(map[string]any{"targets": targets, "broadcast": broadcast}),
			any(map[string]any{"targets": []string{"m-box"}, "broadcast": true}))
	})
}
