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

// shutdownLockGuardWatchdog fails the test loudly if its body has not finished
// within a window far shorter than the package timeout. The returned func stops
// it and is meant to be deferred.
func shutdownLockGuardWatchdog(t *testing.T) func() {
	t.Helper()
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			panic(shutdownLockOrderRuling + " — the guard itself never finished")
		}
	}()
	return func() { close(done) }
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

	// ── can a staff broadcast kill hit the REPLACEMENT? (T-253 乙, staff half) ─
	//
	// The outsource half of this question is measured in
	// TestStopWorkerSessionForHandover. The two populations now share the kill
	// chain and the send, but NOT their retry arms — the worker's
	// retryUnlandedWorkerStop compares machines, while the member's
	// RobustStopPendingAt arm keys on plain presence (reconcileDecide) — so
	// "the same reasoning applies" is an inference, and this is the measurement.

	t.Run("a staff broadcast kill cannot reach the replacement: the retry marker is cleared by the very offline tick that decides the start", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-one")
		shutdownWarden(t, api, d, "m-two")
		// No pin, no live claim, no last landing: nothing can name a machine.
		agent := shutdownStaff(t, api, h, d, owner, "", "m-one", "m-two")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("stopped report: %d (%v)", status, data)
		}
		wsWantWardenFrames(t, api, "m-one", wsStopFrame("kip"))
		wsWantWardenFrames(t, api, "m-two", wsStopFrame("kip"))

		// The owner now places the replacement on m-one, and the tick that reads
		// this member offline decides its START.
		shutdownBootable(t, d, "kip", "m-one")
		row, err := d.GetMember("kip")
		if err != nil || row == nil {
			t.Fatalf("GetMember: %v (%v)", row, err)
		}
		started := api.reconcileTickMemberLocked(*row, nowSecs()+10000)
		apiWantValue(t, "the tick that saw it offline", any(map[string]any{
			"command": started.Command, "robust_stop_pending": started.State.RobustStopPendingAt,
		}), any(map[string]any{"command": "start", "robust_stop_pending": 0}))
		apiWantValue(t, "what the replacement's machine was told",
			any(wsVerbs(t, api, "m-one")), any([]any{"start"}))

		// The replacement is now live on m-one. Every later tick, arbitrarily far
		// past stop_retry, must leave it alone.
		if _, err := api.hub.Connect("kip", "m-one"); err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		fresh, err := d.GetMember("kip")
		if err != nil || fresh == nil {
			t.Fatalf("GetMember: %v (%v)", fresh, err)
		}
		late := api.reconcileTickMemberLocked(*fresh, nowSecs()+20000)

		apiWantValue(t, "the late tick", any(late.Command), any("none"))
		apiWantValue(t, "late kills at the replacement's machine",
			any(wsVerbs(t, api, "m-one")), any([]any{}))
		apiWantValue(t, "late kills elsewhere", any(wsVerbs(t, api, "m-two")), any([]any{}))
	})

	t.Run("POSITIVE CONTROL: with no offline tick in between, the same retry does re-dispatch a kill at the machine the session claims", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		shutdownWarden(t, api, d, "m-one")
		shutdownWarden(t, api, d, "m-two")
		agent := shutdownStaff(t, api, h, d, owner, "", "m-one", "m-two")

		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", agent, `{}`); status != 200 {
			t.Fatalf("stopped report: %d (%v)", status, data)
		}
		wsVerbs(t, api, "m-one")
		wsVerbs(t, api, "m-two")

		// The session the broadcast aimed at is STILL there — no tick ever read
		// this member offline, which is the whole difference from the case above.
		if _, err := api.hub.Connect("kip", "m-one"); err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		row, err := d.GetMember("kip")
		if err != nil || row == nil {
			t.Fatalf("GetMember: %v (%v)", row, err)
		}
		late := api.reconcileTickMemberLocked(*row, nowSecs()+10000)

		apiWantValue(t, "the late tick", any(late.Command), any("stop"))
		apiWantValue(t, "the re-fired kill", any(wsVerbs(t, api, "m-one")), any([]any{"stop"}))
		apiWantValue(t, "and nowhere else", any(wsVerbs(t, api, "m-two")), any([]any{}))
	})

	// ── the two locks are taken in sequence, never nested (T-253 C) ─────────

	t.Run("the outsource lock is released before the reconcile lock is taken", func(t *testing.T) {
		// 🔴 THE OWNER RULING THIS PINS (T-14, lifecycle_tick.go verbatim): lock A →
		// run A → drop → lock B → run B → drop. The worker stopped-report decides
		// and latches under outsourceMu, then hands off to a shutdown that takes
		// reconcileMu. Holding both would make this the package's only
		// double-holder.
		//
		// HOW IT TELLS THE TWO APART. reconcileMu is held here for the whole
		// experiment, so the handler is guaranteed to block inside the shutdown. If
		// it still held outsourceMu at that moment, no one else could ever take
		// outsourceMu — so acquiring it is exactly the observation that the lock
		// was dropped first.
		//
		// 🔴 IT CARRIES ITS OWN DEADLINE, WELL INSIDE THE PACKAGE TIMEOUT. A test
		// that proves a lock ordering by waiting on locks can only fail by not
		// finishing, and "did not finish" reaches a reader as a 10-minute package
		// timeout panic with a goroutine dump — indistinguishable from a flaky
		// hang, and it takes the whole package's result with it. The watchdog
		// below turns that into a labelled failure that names the ruling.
		defer shutdownLockGuardWatchdog(t)()
		api, h, d, _, session, contractor := apiTestLiveWorker(t)
		api.hub.Disconnect(session)

		api.reconcileMu.Lock()
		answered := make(chan int, 1)
		go func() {
			answered <- apiRequest(t, h, "POST", "/api/self/stopped", contractor, `{}`).Code
		}()

		// The durable latch is written inside the outsource-locked region, so its
		// arrival means the handler has reached the hand-off.
		deadline := time.Now().Add(5 * time.Second)
		latched := false
		for time.Now().Before(deadline) {
			if w, err := d.GetOutsourceWorker("ow-abc123"); err == nil && w != nil && w.StoppedSince > 0 {
				latched = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !latched {
			api.reconcileMu.Unlock()
			t.Fatal("the stopped latch never landed — the handler did not reach the shutdown")
		}

		free := make(chan struct{})
		go func() {
			api.outsourceMu.Lock()
			api.outsourceMu.Unlock()
			close(free)
		}()
		select {
		case <-free:
		case <-time.After(5 * time.Second):
			api.reconcileMu.Unlock()
			t.Fatal(shutdownLockOrderRuling)
		}

		// POSITIVE CONTROL: the sequence really is a sequence — release the second
		// lock and the same request completes, so the wait above was a genuine
		// block and not a request that had already finished.
		api.reconcileMu.Unlock()
		select {
		case code := <-answered:
			apiWantValue(t, "status", any(float64(code)), any(200))
		case <-time.After(5 * time.Second):
			t.Fatal("the stopped-report never completed once the reconcile lock was free")
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
	})
}
