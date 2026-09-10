package main

// forcestop_bank_lastdisconnect_t65_test.go — T-65 包⑤.
//
// ONE question, and the whitelist has been answering it in prose: when the owner
// force-stops a 正職 member, does the dying session's live cost ever reach
// banked_cost?
//
// The parity matrix (verb_population_parity_t65_test.go) compares both
// populations after the handler returns. Both force-stop paths now bank before
// dispatching the kill; this test also closes the subject's SSE stream to prove
// the later disconnect edge does not bank the same figure again.
//
// 🔴 IT DRIVES THE REAL HANDLER, NOT onLastDisconnect. Calling
// api.onLastDisconnect(id) directly would be a test of bankLiveCost with the
// interesting half — 「does that edge actually get reached when a force-stopped
// session's stream ends」 — assumed rather than exercised. That is the same split
// the parity file's rule 1 pays for: a guard over the pure helper leaves the
// wiring deletable with the suite still green. So the session here is a real GET
// /api/events stream in a goroutine, and it ends the way a killed session's
// stream ends: its request context goes away.
//
// ⚠️ WHAT THIS FILE DOES NOT CLAIM. It does not prove the warden kills the
// session or bound the warden's timing; its subject is the server's pre-kill
// banking and the idempotence of the later disconnect edge.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// forceStopBankLiveCost is the live figure planted on the subject. Exactly
// representable in float64, and non-zero for the reason parityLiveCost spells
// out at length: with a zero seed, "the pop happened and the money did not
// arrive" is indistinguishable from a correct bank.
const forceStopBankLiveCost = 7.5

// TestForceStoppedStaffCostIsBankedBeforeTheDisconnectEdge pins the pre-kill
// fold and the idempotent disconnect edge in one run.
func TestForceStoppedStaffCostIsBankedBeforeTheDisconnectEdge(t *testing.T) {
	api := newReconcileTestServer(t)
	const id = "m-forcebank"
	putTestMember(t, api, testAgent(id))

	sink := newFailAfterWrites(1 << 30)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(context.WithValue(ctx, claimsContextKey,
		map[string]any{"sub": id, "scope": "agent"}))
	streamDone := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(sink, req)
		close(streamDone)
	}()
	t.Cleanup(func() {
		cancel()
		<-streamDone
	})

	// The stream must be the live online projection before the cost is planted:
	// seeding into a connection that has not registered yet would leave the
	// last-disconnect edge with nothing to find, and the test would pass for
	// having measured nothing.
	waitFor(t, "the subject's SSE stream to project it online",
		func() bool { return api.hub.IsOnline(id) })

	entry := api.telemetry.Get(id)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["cost"] = forceStopBankLiveCost
	api.telemetry.Set(id, entry)

	code := postMember(t, api, id, "force-stop", nil,
		api.HandleForceStopMemberApiMembersMemberIdForceStopPost)
	if code != http.StatusOK {
		t.Fatalf("force-stop: want 200, got %d", code)
	}

	// The handler banks before dispatching the kill, so the disconnect edge has
	// no live figure left to fold.
	if banked := bankedCostOf(t, api, id); banked != forceStopBankLiveCost {
		t.Fatalf("force-stop must bank before dispatch: want %v, got %v",
			forceStopBankLiveCost, banked)
	}
	if _, live := api.telemetry.Get(id)["cost"]; live {
		t.Fatal("force-stop left the live figure after pre-kill banking")
	}

	// Ending the request context exercises the same disconnect edge that a killed
	// session produces.
	cancel()
	<-streamDone

	if api.hub.IsOnline(id) {
		t.Fatal("the stream returned but the member is still projected online — " +
			"the defer's hub.Disconnect did not run, so no last-disconnect edge " +
			"could have fired and half 2 below would be measuring the wrong thing")
	}
	if banked := bankedCostOf(t, api, id); banked != forceStopBankLiveCost {
		t.Fatalf("the disconnect edge changed the already-banked cost: "+
			"want %v, got %v",
			forceStopBankLiveCost, banked)
	}
	if _, live := api.telemetry.Get(id)["cost"]; live {
		t.Fatal("banked, but the live figure was not popped — the next disconnect " +
			"edge would bank it a second time (pop-before-write is what makes " +
			"onLastDisconnect idempotent)")
	}
}

func bankedCostOf(t *testing.T, api *apiServer, id string) float64 {
	t.Helper()
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back member %s: %v", id, err)
	}
	return m.BankedCost
}

// waitFor polls because the stream runs in its own goroutine and there is no
// signal to join on before it registers. It FAILS on the timeout rather than
// returning: a silent give-up here would let both halves below assert against a
// member that was never online, which is green for the wrong reason.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after 5s waiting for %s", what)
}
