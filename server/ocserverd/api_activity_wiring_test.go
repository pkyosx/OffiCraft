package main

// api_activity_wiring_test.go — T-a1d7. The activity feature's central design
// decision is that the two SSE lifecycle edges carry it: a drop must record
// `offline_since` (so the reconnect can be judged) and a connect must decide
// whether a claim held from before the drop survives.
//
// 🔴 WHY THIS FILE EXISTS. api_activity_test.go drives activityOnConnect /
// activityOnDisconnect DIRECTLY, so it proves the helpers are correct and
// proves nothing about anyone calling them. Deleting both calls from
// onFirstConnect / onLastDisconnect left the entire ocserverd suite green —
// and that gap is exactly how the boot-turn defect
// (TestActivityOnConnect_FirstEverConnectKeepsTheBootTurnClaim) reached review.
//
// This is verbatim the defect class server/CLAUDE.md already records and built
// a dedicated test for: 「沒有人證明有人呼叫的投影不是防線」
// (api_machines_childenv_wiring_test.go).
//
// SCOPE, honestly stated: these tests drive the edge hooks that the SSE handler
// calls (api_infra.go:100 and :121) through the REAL constructor (newAPIServer),
// not a hand-assembled struct. They prove edge-hook → activity. They do NOT
// re-prove hub → edge-hook; that seam predates this feature and carries the
// existing presence projection.
//
// ⚠️ WHICH TEST GUARDS WHICH CALL — measured, not assumed. Do not assume the
// test whose NAME matches an edge is the one holding it up:
//
//	remove s.activityOnConnect     → only TestSSEEdgesTogetherDropAStaleClaimOnReconnect fails
//	remove s.activityOnDisconnect  → only TestSSEDisconnectEdgeRecordsTheDrop fails
//
// TestSSEConnectEdgeKeepsTheBootTurnClaim survives BOTH removals and is not a
// wiring guard at all: with the call gone nothing touches the claim, so "the
// claim survived" is exactly what an unwired build produces too. It guards the
// boot-turn REGRESSION (someone re-widening the judgement to the 0→1 edge), not
// the wiring. Deleting TestSSEEdgesTogether… would silently retire the only
// proof that the connect edge is called at all.

import "testing"

// The store must actually exist on a server built the production way —
// otherwise every helper below early-returns and this whole file is a
// green that means nothing.
func TestActivityStoreIsWiredByTheRealConstructor(t *testing.T) {
	api, _ := newGateTestAPI(t)
	if api.activityStore() == nil {
		t.Fatal("newAPIServer must construct the activity store; a nil store makes every activity path a silent no-op")
	}
}

// THE BOOT TURN, at the real seam. seeds/boot_sequence.md step 3 hangs
// `ocagent listen` LAST, so the first-ever connect lands mid-turn. The SSE
// connect edge must not discard that live claim.
func TestSSEConnectEdgeKeepsTheBootTurnClaim(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "w-boot", Kind: KindAssistant})

	activityReceipt(t, doReportActivity(api, "w-boot", `{"state":"active","turn_id":"t1"}`))
	api.onFirstConnect("w-boot") // the 0→1 edge — no drop was ever observed

	entry := api.activity.Get("w-boot")
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("the SSE connect edge destroyed a live boot-turn claim, got %q", state)
	}
}

// The disconnect edge must record the drop. Without this stamp the reconnect
// judgement has no input, and a real absence would look like a blip forever.
func TestSSEDisconnectEdgeRecordsTheDrop(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "w-drop", Kind: KindAssistant})

	activityReceipt(t, doReportActivity(api, "w-drop", `{"state":"active","turn_id":"t1"}`))
	api.onLastDisconnect("w-drop")

	entry := api.activity.Get("w-drop")
	if off, _ := entry[activityKeyOfflineSince].(float64); off <= 0 {
		t.Fatal("the SSE disconnect edge must record offline_since — the reconnect decision has no other input")
	}
	// And the claim itself survives the drop: a blip must not lose the turn.
	if state, _ := entry[activityKeyState].(string); state != ActivityActive {
		t.Fatalf("a drop must not destroy the claim, got %q", state)
	}
}

// Both edges together, in the order they actually fire, across a REAL absence:
// drop → (longer than the grace) → reconnect. This is the arm that proves the
// connect edge is still wired to the judgement after the boot-turn fix — a fix
// that simply never discarded anything would pass the two tests above.
func TestSSEEdgesTogetherDropAStaleClaimOnReconnect(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "w-gone", Kind: KindAssistant})

	activityReceipt(t, doReportActivity(api, "w-gone", `{"state":"active","turn_id":"t1"}`))
	api.onLastDisconnect("w-gone")

	// Backdate the drop past the grace: the session was really gone.
	entry := api.activity.Get("w-gone")
	entry[activityKeyOfflineSince] = nowSecs() - api.activityGraceSecs() - 1
	api.activity.Set("w-gone", entry)

	api.onFirstConnect("w-gone")

	entry = api.activity.Get("w-gone")
	if state, _ := entry[activityKeyState].(string); state != "" {
		t.Fatalf("a reconnect after a real absence must drop the stale claim, got %q", state)
	}
	if _, stamped := entry[activityKeyLastEnd]; stamped {
		t.Fatal("we never observed that turn end — no completion time may be invented")
	}
}
