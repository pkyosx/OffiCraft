package main

// forcestop_bank_lastdisconnect_t65_test.go — T-65 包⑤.
//
// ONE question, and the whitelist has been answering it in prose: when the owner
// force-stops a 正職 member, does the dying session's live cost ever reach
// banked_cost?
//
// The parity matrix (verb_population_parity_t65_test.go) reads 強制停止|banked_cost
// as costUntouched on the staff side and costBanked on the worker side, and for
// a while that cell was read as 「the staff money is LOST」. It is not. The two
// populations bank on two different EDGES:
//
//	外包  stopWorkerNow → bankLiveCost, INSIDE the kill funnel, synchronously.
//	正職  dispatchRobustStopNow banks nothing; the warden kills the session, the
//	      agent's SSE stream ends, and api_infra.go's stream defer fires
//	      onLastDisconnect → bankLiveCost on the real online→offline edge.
//
// 🔴 WHY THE MATRIX CANNOT SEE THIS, and therefore why this file exists. Its
// fixture keeps the subject's SSE connection UP for the whole run and only
// disconnects in t.Cleanup, which is after the terminal read. So its staff cell
// samples the world at the instant the handler returns — BEFORE the staff edge
// can exist. That cell measures IMMEDIACY; it cannot measure AMOUNT, and no
// amount of re-reading it will make it able to.
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
// session, and it does not bound the window between the kill and the disconnect
// edge — the warden is not in this module. Its subject is the SERVER half: given
// that the stream ends, the money lands. The residual risk is written on the
// 強制停止|banked_cost whitelist row (s.telemetry is a *memStore, so a station
// that re-execs inside that window loses the staff figure while 外包 is immune).

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

// TestForceStoppedStaffCostBanksOnTheDisconnectEdge pins BOTH halves of the
// staff banking story in one run, because either half alone is misleading:
// asserting only the second would not distinguish "banked late" from "banked in
// the handler", and asserting only the first is what the parity matrix already
// does.
func TestForceStoppedStaffCostBanksOnTheDisconnectEdge(t *testing.T) {
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

	// ── half 1: when the handler returns, nothing has been banked yet ────────
	//
	// This is the parity matrix's cell, re-asserted here so that the two halves
	// are pinned by ONE test. If a future edit gives the staff force-stop its own
	// synchronous bank, THIS is the assertion that says so — and the whitelist
	// row's "two edges, same total" sentence would then be describing a repo that
	// no longer exists.
	if banked := bankedCostOf(t, api, id); banked != 0.0 {
		t.Fatalf("force-stop must not bank in the handler (that is 外包's edge, "+
			"not 正職's): banked_cost = %v, want 0", banked)
	}
	if _, live := api.telemetry.Get(id)["cost"]; !live {
		t.Fatal("the live figure vanished without being banked — that is costLost, " +
			"the outcome bankLiveCost's own comment says already happened once")
	}

	// ── half 2: the money lands when the session's stream ends ───────────────
	//
	// Ending the request context is what a killed session looks like from this
	// side: the stream's loop sees ctx.Err() and returns through the defer that
	// carries the §5.2 edge hooks.
	cancel()
	<-streamDone

	if api.hub.IsOnline(id) {
		t.Fatal("the stream returned but the member is still projected online — " +
			"the defer's hub.Disconnect did not run, so no last-disconnect edge " +
			"could have fired and half 2 below would be measuring the wrong thing")
	}
	if banked := bankedCostOf(t, api, id); banked != forceStopBankLiveCost {
		t.Fatalf("the force-stopped session's live cost never reached banked_cost: "+
			"want %v, got %v. 強制停止|banked_cost is whitelisted as a PERMANENT "+
			"difference on the strength of this landing later, not never — if this "+
			"is red, that row is now describing a real loss of owner-visible money",
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
