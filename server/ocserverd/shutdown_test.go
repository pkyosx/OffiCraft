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
	if err := d.PutMember(Member{
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
	if err := d.PutMember(*m); err != nil {
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
	if err := d.PutMember(*m); err != nil {
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

// shutdownLockOrderRuling is the one sentence every failure of the lock-order
// guard must print, whether it fails by assertion or by not finishing.
const shutdownLockOrderRuling = "T-253 lock order: the scheduler lock is still held while the " +
	"shutdown waits on the reconcile lock — that is the nested hold the T-14 ruling " +
	"forbids (lock A → run A → drop → lock B → run B → drop)"

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
		if err := d.PutMember(Member{
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

	t.Run("an id the roster cannot answer for is treated as a WORKER, so nothing is left behind to suppress its next start", func(t *testing.T) {
		// 🔴 FAIL-CLOSED. The kind decides whether the member producer's
		// at-least-once marker is armed, and arming it for a worker is the F1
		// defect: the worker tick's decider then holds the START that is due and,
		// past stop_retry, benches the machine for a STOP it reads as a zombie
		// takeover. A row this server cannot read must land on the cheap mistake,
		// not the ruling-level one.
		//
		// The CONSEQUENCE — the next tick starting the replacement instead of
		// waiting — is measured on a row that exists, in
		// TestHandleReportStoppedApiSelfStoppedPost. Here there is no row by
		// construction (that is the case under test), so no tick can run and the
		// marker is all there is to read.
		api, _, _, _ := newAPITestServer(t)
		apiTestListen(t, api, ServerSelfHost)

		api.dispatchShutdown("ow-no-such-row", "test")

		apiWantValue(t, "the member producer's marker",
			any(api.lifecycleState("ow-no-such-row").RobustStopPendingAt), any(float64(0)))
	})

	t.Run("CONTROL: an id the roster DOES answer for as staff keeps the cadence's re-send of its kill", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		if err := d.SetMemberDesiredMachineID("kip", ServerSelfHost); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		apiTestListen(t, api, ServerSelfHost)

		api.dispatchShutdown("kip", "test")

		if got := api.lifecycleState("kip").RobustStopPendingAt; got <= 0 {
			t.Fatalf("a staff shutdown must arm the marker, got %v", got)
		}
	})

	// ── the two locks are taken in sequence, never nested (T-253 C) ─────────
	//
	// 🔴 WHAT ACTUALLY HOLDS THIS UP DIFFERS BY POPULATION, AND SAYING SO IS THE
	// POINT — the earlier version of this test claimed a mechanism it did not
	// have, which is worse than having none.
	//
	//   * STAFF is the arm where the two locks really are a SEQUENCE:
	//     dispatchShutdown takes outsourceMu (the spawn observation), drops it,
	//     and then takes reconcileMu for the at-least-once marker. That is the
	//     ordering the subtest below measures, and holding reconcileMu really
	//     does block the handler inside the shutdown.
	//   * OUTSOURCE does not reach reconcileMu at all any more: the marker is
	//     staff-only since T-253 F1, so a worker's shutdown never touches the
	//     second lock and "never both at once" is true of it VACUOUSLY. What it
	//     still needs — dropping outsourceMu before the shared shutdown — is
	//     enforced STRUCTURALLY instead: dispatchShutdown re-takes outsourceMu,
	//     and Go mutexes do not re-enter, so a caller that kept it self-deadlocks
	//     on the spot. Measured: moving that Unlock past the shutdown hangs the
	//     request inside resolveShutdownTargets. The worker-side test for it is
	//     the receipt-lock barrier in TestHandleReportStoppedApiSelfStoppedPost,
	//     which cannot reach its assertions unless the handler got through that
	//     re-acquire.

	t.Run("a staff shutdown drops the scheduler lock before it takes the reconcile lock", func(t *testing.T) {
		// 🔴 THE OWNER RULING THIS PINS (T-14, lifecycle_tick.go verbatim): lock A →
		// run A → drop → lock B → run B → drop.
		//
		// HOW IT TELLS THE TWO APART. reconcileMu is held for the whole
		// experiment, so the handler is guaranteed to BLOCK inside the shutdown —
		// the staff arm cannot finish without the marker. If it still held
		// outsourceMu at that moment nobody else could ever take outsourceMu, so
		// acquiring it is exactly the observation that the lock was dropped first.
		//
		// Every wait below is bounded well inside the package timeout, so this can
		// only fail as a named assertion on the test's own goroutine — never as a
		// package-wide timeout panic that takes every other result with it.
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
		unlocked := false
		unlock := func() {
			if !unlocked {
				unlocked = true
				api.reconcileMu.Unlock()
			}
		}
		defer unlock()

		answered := make(chan int, 1)
		go func() {
			answered <- apiRequest(t, h, "POST", "/api/self/stopped", agent, `{}`).Code
		}()

		// The durable latch is written before the shutdown, so its arrival means
		// the handler has reached the hand-off.
		if !shutdownWaitFor(t, func() bool {
			m, err := d.GetMember("kip")
			return err == nil && m != nil && m.StoppedSince > 0
		}) {
			t.Fatal("the close-out latch never landed — the handler did not reach the shutdown")
		}

		// THE MEASUREMENT. The handler is inside the shutdown waiting on
		// reconcileMu; outsourceMu must therefore be free.
		free := make(chan struct{})
		go func() {
			api.outsourceMu.Lock()
			api.outsourceMu.Unlock()
			close(free)
		}()
		select {
		case <-free:
		case <-time.After(5 * time.Second):
			unlock()
			t.Fatal(shutdownLockOrderRuling)
		}

		// POSITIVE CONTROL, and it does discriminate here: while reconcileMu is
		// held the request MUST still be in flight. If it had already answered,
		// the acquisition above would have proved nothing about ordering.
		select {
		case code := <-answered:
			unlock()
			t.Fatalf("the request answered %d while the reconcile lock was held — it "+
				"never blocks on the second lock, so this test measures nothing", code)
		default:
		}

		unlock()
		select {
		case code := <-answered:
			apiWantValue(t, "status", any(float64(code)), any(200))
		case <-time.After(5 * time.Second):
			t.Fatal("the stopped-report never completed once the reconcile lock was free")
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
