package main

// api_infra_session_anchor_t4235_test.go — the DURABLE session anchor
// (member.session_boot_ts, migrations/00051).
//
// The defect this file exists for: the session anchor lived ONLY on the
// in-memory gauge, which a station re-exec empties by contract. The agents
// survive that re-exec — their SSE streams drop and they reconnect seconds
// later — and that reconnect, finding no anchor, minted a brand-new one. A
// session alive for hours therefore read as seconds old, and restart_self's
// 600s minimum-liveness floor refused it. Three field samples over two upgrades
// and three different agents.
//
// 🔴 WHAT MAKES THIS FILE DIFFERENT FROM api_infra_bootts_test.go: every test
// here drives a SERVER RESTART — s.gauge is replaced with a fresh empty
// memStore, exactly what a re-exec does — and then calls the REAL
// onFirstConnect. That is the shape the previous attempt at this bug could not
// reach, and it is the only shape that decides whether a fix helps the sessions
// that were already running when the station upgraded.
//
// Both directions are pinned, and they are pinned separately because they fail
// for opposite reasons:
//   (a) FALSE REFUSAL — a session that survived a re-exec must keep its anchor
//       and be let through the floor.
//   (b) THE REVERSE — making the anchor durable must NOT let a genuinely new
//       session inherit its predecessor's anchor. The respawn-storm guard is
//       the thing this whole gate exists to be, and a durable anchor is exactly
//       the kind of change that can weaken it silently.

import (
	"context"
	"net/http/httptest"
	"testing"
)

// reExec simulates the station upgrade: the process is replaced, so every
// in-memory store is empty again. The durable row is all that survives. This is
// the SAME thing newAPIServer does on a cold boot (newMemStore), not a
// test-only shortcut around some guard.
func reExec(api *apiServer) {
	api.gauge = newMemStore()
}

// ── (a) the sessions that were already running when the station upgraded ──────

// TestSessionAnchorSurvivesAServerReExec is the ticket in one assertion: the
// anchor is still the ORIGINAL boot moment after the gauge has been wiped and
// the agent has reconnected.
func TestSessionAnchorSurvivesAServerReExec(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-live", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	// A session that booted hours ago.
	api.onFirstConnect("sa-live")
	orig, ok := gaugeBootTS(api.gauge.Get("sa-live"))
	if !ok {
		t.Fatal("precondition: the first connect must anchor the session")
	}
	// Anti-tautology: prove the durable row really took the anchor, otherwise
	// every assertion below could pass on a server that persists nothing and
	// merely happens to keep its gauge.
	m, err := dal.GetMember("sa-live")
	if err != nil || m == nil {
		t.Fatalf("reload sa-live: %v", err)
	}
	if m.SessionBootTS != orig {
		t.Fatalf("the connect edge must persist the anchor: durable %v, gauge %v",
			m.SessionBootTS, orig)
	}

	// The station upgrades. The process is replaced; the agent is not.
	reExec(api)
	if _, ok := gaugeBootTS(api.gauge.Get("sa-live")); ok {
		t.Fatal("precondition: a re-exec must leave the gauge empty")
	}

	// The agent's SSE stream dropped and it reconnects.
	api.onFirstConnect("sa-live")

	got, ok := gaugeBootTS(api.gauge.Get("sa-live"))
	if !ok {
		t.Fatal("a reconnect after a re-exec must restore the session anchor, not drop it")
	}
	if got != orig {
		t.Fatalf("a reconnect after a re-exec re-anchored the session: want %v "+
			"(the real boot moment), got %v — this is the defect: a session alive "+
			"for hours reads as seconds old", orig, got)
	}
}

// TestRestartSelfIsAllowedForASessionThatSurvivedAReExec drives the consumer the
// three field samples actually hit. This is the acceptance criterion: it must
// take effect for a session that was ALREADY RUNNING when the station upgraded.
func TestRestartSelfIsAllowedForASessionThatSurvivedAReExec(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-rs", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	// The session booted well before the floor — the three reporters were
	// hours in.
	old := nowSecs() - (minSelfRestartSecs + 3600)
	m, err := dal.GetMember("sa-rs")
	if err != nil || m == nil {
		t.Fatalf("load sa-rs: %v", err)
	}
	m.SessionBootTS = old
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	// Station upgrade: gauge empty, agent still alive, agent reconnects.
	reExec(api)
	defer online(t, api, "sa-rs")()
	api.onFirstConnect("sa-rs")

	// Anti-tautology: the restored anchor must genuinely be the old one. Without
	// this the 200 below could just as well come from a gate that never ran.
	if got, ok := gaugeBootTS(api.gauge.Get("sa-rs")); !ok || got != old {
		t.Fatalf("precondition: reconnect must restore the pre-upgrade anchor; got %v ok=%t", got, ok)
	}

	rec := doRestartSelf(api, "sa-rs", `{"reason":"picking up the new tool directory"}`)
	if rec.Code != 200 {
		t.Fatalf("a session that survived the upgrade must be allowed to restart: "+
			"want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestContextHighAutoRecycleIsNotSuppressedAfterAReExec — consumer 2 of 3.
// stampContextHighRecycle uses the same boot-storm guard as a SUPPRESSOR: a
// fresh boot over the handover line is assumed to be its own boot context. With
// gauge-only anchors, every live agent looked freshly booted after an upgrade,
// so no auto-recycle could fire for MinBootSecs.
func TestContextHighAutoRecycleIsNotSuppressedAfterAReExec(t *testing.T) {
	api, dal := newGateTestAPI(t)
	now := nowSecs()
	ctxhigh := api.ctxHighConfig()
	if ctxhigh.MinBootSecs <= 0 {
		t.Skip("boot-storm guard disabled by config — nothing to suppress")
	}
	putGateMember(t, dal, Member{ID: "sa-ch", Kind: KindStaff,
		DesiredState: DesiredStateOnline, Runtime: RuntimeClaude})
	m, _ := dal.GetMember("sa-ch")
	m.SessionBootTS = now - (ctxhigh.MinBootSecs + 3600)
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	reExec(api)
	defer online(t, api, "sa-ch")()
	api.onFirstConnect("sa-ch")

	// A pct over the handover line, reported after the restored anchor so the
	// stale-pct guard admits it.
	entry := api.gauge.Get("sa-ch")
	entry["context_pct"] = ctxhigh.HandoverPct + 1
	entry["context_pct_ts"] = now
	api.gauge.Set("sa-ch", entry)

	members := []Member{*mustMember(t, dal, "sa-ch")}
	api.stampContextHighRecycle(members, now)

	back := mustMember(t, dal, "sa-ch")
	if back.RefocusSince <= 0 {
		t.Fatal("an hours-old session over the handover line must auto-recycle " +
			"after a re-exec; the boot-storm suppressor read the reconnect as a fresh boot")
	}
}

// TestWorkerAutoHandoverIsNotSuppressedAfterAReExec — consumer 3 of 3, the
// worker twin of the previous test.
func TestWorkerAutoHandoverIsNotSuppressedAfterAReExec(t *testing.T) {
	api, dal := newGateTestAPI(t)
	now := nowSecs()
	ctxhigh := api.ctxHighConfig()
	if ctxhigh.MinBootSecs <= 0 {
		t.Skip("boot-storm guard disabled by config — nothing to suppress")
	}
	putGateMember(t, dal, Member{ID: "ow-sa", Kind: KindOutsource,
		Codename: "O-4235", DesiredState: DesiredStateOnline,
		Runtime: RuntimeClaude, ActivatedTS: now - 7200})
	m, _ := dal.GetMember("ow-sa")
	m.SessionBootTS = now - (ctxhigh.MinBootSecs + 3600)
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	reExec(api)
	defer online(t, api, "ow-sa")()
	api.onFirstConnect("ow-sa")

	entry := api.gauge.Get("ow-sa")
	entry["context_pct"] = ctxhigh.HandoverPct + 1
	entry["context_pct_ts"] = now
	api.gauge.Set("ow-sa", entry)

	w, err := dal.GetOutsourceWorker("ow-sa")
	if err != nil || w == nil {
		t.Fatalf("load worker: %v", err)
	}
	if w.SessionBootTS <= 0 {
		t.Fatalf("precondition: the worker projection must carry the anchor; got %v — "+
			"memberFromWorker rebuilds a Member from scratch, so a forgotten column "+
			"is zeroed by the next outsource write", w.SessionBootTS)
	}
	workerTickPass(t, api, w.ID, now)

	back, err := dal.GetOutsourceWorker("ow-sa")
	if err != nil || back == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if back.RefocusSince <= 0 {
		t.Fatal("an hours-old worker over the handover line must auto-hand-over " +
			"after a re-exec; the boot-storm suppressor read the reconnect as a fresh boot")
	}
}

// ── (b) the respawn-storm guard must not be weakened, in either direction ─────

// TestGenuinelyFreshSessionIsStillRefused is the positive control: the guard
// this gate exists to be must still fire. Nothing about persisting the anchor
// may make a real respawn storm possible.
func TestGenuinelyFreshSessionIsStillRefused(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-fresh", Kind: KindStaff,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "sa-fresh")()

	// A real session birth: the boundary cleared the anchor, then it connected.
	api.clearSessionBootTS("sa-fresh")
	api.onFirstConnect("sa-fresh")

	rec := doRestartSelf(api, "sa-fresh", "")
	if rec.Code != 429 {
		t.Fatalf("a genuinely fresh session must still be refused (respawn storm "+
			"guard): want 429, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestSessionBoundaryClearsTheDurableAnchorSoARebirthIsNotWavedThrough is the
// REVERSE direction the durable anchor introduces, and the one that would be
// invisible without this test: if the boundary cleared only the gauge, the next
// session — a genuinely new one — would read its predecessor's hours-old anchor
// off the durable row and sail through the floor. Note the re-exec: that is what
// used to hide this by wiping the gauge, and it no longer does.
func TestSessionBoundaryClearsTheDurableAnchorSoARebirthIsNotWavedThrough(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-reborn", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	// An hours-old session.
	m, _ := dal.GetMember("sa-reborn")
	m.SessionBootTS = nowSecs() - (minSelfRestartSecs + 7200)
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	// A real session boundary — a START dispatch / STOP / kill.
	api.clearSessionBootTS("sa-reborn")

	back := mustMember(t, dal, "sa-reborn")
	if back.SessionBootTS != 0 {
		t.Fatalf("a session boundary must zero the DURABLE anchor too; got %v — "+
			"otherwise the replacement session inherits it and the storm guard "+
			"never fires again for this member", back.SessionBootTS)
	}

	// The replacement session boots. A re-exec in between must not change this.
	reExec(api)
	defer online(t, api, "sa-reborn")()
	api.onFirstConnect("sa-reborn")

	rec := doRestartSelf(api, "sa-reborn", "")
	if rec.Code != 429 {
		t.Fatalf("the replacement session is seconds old and must be refused: "+
			"want 429, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestMidSessionFlapStillDoesNotMoveTheAnchor — T-8fb2's invariant, re-pinned
// against the durable store: an ordinary drop/reconnect with no boundary
// crossed must leave the anchor exactly where it is, in BOTH stores.
func TestMidSessionFlapStillDoesNotMoveTheAnchor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-flap", Kind: KindStaff})

	api.onFirstConnect("sa-flap")
	orig := mustMember(t, dal, "sa-flap").SessionBootTS
	if orig <= 0 {
		t.Fatal("precondition: the first connect must anchor")
	}

	api.onFirstConnect("sa-flap")
	api.onFirstConnect("sa-flap")

	if got := mustMember(t, dal, "sa-flap").SessionBootTS; got != orig {
		t.Fatalf("a mid-session flap must not move the durable anchor: want %v, got %v", orig, got)
	}
	if got, _ := gaugeBootTS(api.gauge.Get("sa-flap")); got != orig {
		t.Fatalf("a mid-session flap must not move the gauge anchor: want %v, got %v", orig, got)
	}
}

// TestAPreExistingGaugeAnchorIsAdoptedNotOverwritten covers the migration
// transition and any failed durable write: an anchor already on the gauge with
// no durable twin must be ADOPTED. The anchor may move backwards on this edge,
// never forwards — forwards is the defect.
func TestAPreExistingGaugeAnchorIsAdoptedNotOverwritten(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-adopt", Kind: KindStaff})

	orig := nowSecs() - 5000
	api.gauge.Set("sa-adopt", map[string]any{"boot_ts": orig})

	api.onFirstConnect("sa-adopt")

	if got, _ := gaugeBootTS(api.gauge.Get("sa-adopt")); got != orig {
		t.Fatalf("an existing gauge anchor must not be overwritten: want %v, got %v", orig, got)
	}
	if got := mustMember(t, dal, "sa-adopt").SessionBootTS; got != orig {
		t.Fatalf("an existing gauge anchor must be adopted into the durable row: "+
			"want %v, got %v", orig, got)
	}
}

// TestAnchorRoundTripsThroughTheWorkerProjection is the trap memberFromWorker
// sets: it rebuilds a Member from scratch, so a column it forgets is ZEROED by
// the very next outsource write — and zeroing THIS one hands a live hours-old
// worker back to the boot-storm guard as "just booted".
func TestAnchorRoundTripsThroughTheWorkerProjection(t *testing.T) {
	m := fullMember("ow-proj")
	m.Kind = KindOutsource
	m.Codename = "O-8"
	m.SessionBootTS = nowSecs() - 4242

	back := memberFromWorker(workerFromMember(m))
	if back.SessionBootTS != m.SessionBootTS {
		t.Errorf("session_boot_ts = %v, want %v — memberFromWorker dropped it, so "+
			"the next outsource write erases a live session's anchor",
			back.SessionBootTS, m.SessionBootTS)
	}
}

// ── (c) the wiring: an anchor nobody stamps is not a defence ─────────────────

// TestSessionAnchorIsWiredIntoTheRealConnectEdge drives the REAL SSE handler
// rather than calling onFirstConnect by hand.
//
// 🔴 THIS TEST EXISTS BECAUSE THE GAP WAS MEASURED, not assumed. The reviewer's
// B1 finding on the previous attempt at this ticket was that the whole fix could
// be silently unwired while every gate stayed green. The same mutant was run
// here: deleting the `s.onFirstConnect(memberID)` call from
// HandleEventsApiEventsGet left `go test ./...` at rc=0 with zero failures,
// because every other test in this file (and in api_infra_bootts_test.go) calls
// onFirstConnect directly and therefore cannot tell whether production does.
// This test is the one thing standing between a refactor and the bug returning
// verbatim with a green board. It follows the shape T-98f4 already established
// for the landed-machine stamp on this same edge
// (TestSticky_ConnectStampsTheLandingFromTheTokenClaim), and for the same
// reason.
func TestSessionAnchorIsWiredIntoTheRealConnectEdge(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "sa-wire", Kind: KindStaff,
		DesiredState: DesiredStateOnline})

	// Drive GET /api/events with an already-cancelled context: the handler runs
	// its whole first-connect edge, then the stream loop returns at once.
	connect := func() {
		req := httptest.NewRequest("GET", "/api/events", nil)
		claims := map[string]any{"sub": "sa-wire", "scope": "agent"}
		ctx, cancel := context.WithCancel(
			context.WithValue(req.Context(), claimsContextKey, claims))
		cancel()
		api.HandleEventsApiEventsGet(httptest.NewRecorder(), req.WithContext(ctx))
	}

	connect()
	orig := mustMember(t, dal, "sa-wire").SessionBootTS
	if orig <= 0 {
		t.Fatal("a real SSE connect must persist the session anchor — nothing in " +
			"production calls the anchor edge, so the fix is inert")
	}

	// And the restore half is wired too: re-exec, reconnect for real, and the
	// pre-upgrade anchor must come back rather than a fresh one.
	reExec(api)
	connect()
	got, ok := gaugeBootTS(api.gauge.Get("sa-wire"))
	if !ok || got != orig {
		t.Fatalf("a real reconnect after a re-exec must restore the anchor: "+
			"want %v, got %v ok=%t", orig, got, ok)
	}
}

func mustMember(t *testing.T, dal *DAL, id string) *Member {
	t.Helper()
	m, err := dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("reload %s: %v", id, err)
	}
	return m
}
