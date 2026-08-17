package main

// member_refocus_stamp_visibility_tccc7_test.go — ONE invariant, pinned by
// RESULT rather than by implementation site:
//
//	a member on its way offline — desired offline WITH a stop anchor, the shape
//	every cockpit 停止 produces — is never left carrying a refocus epoch, from
//	any entry point.
//
// The shape is named on purpose. Only the auto-stamp site is guarded on intent
// itself; the other two are covered here for the state that is actually
// reachable today (see below), which is narrower than "any desired-offline
// member". Do not read this file as proving the wider claim.
//
// WHY it is an invariant and not a preference (the cross-layer half): the agent
// prints the 下線程序 wake from cli/ocagent/listen_hooks.go
// maybeRecycle, and that function's FIRST condition is
// `desired_state == online`. A stamp on a desired-offline member is therefore
// not a weaker signal, it is NO signal — no SOP, no partial credit — while the
// server never reads it back either (RefocusSince is only consulted on the
// decideUp arm, which a desired-offline member does not take). The marker then
// outlives the stop: activate clears StoppingSince/WakingSince but NOT
// RefocusSince, so the next wake can be robust-stopped on an epoch that expired
// while nobody was listening, and the cockpit reads 換手中 the whole time.
//
// WHY the three cases are in ONE test instead of a guard at each site: measured
// at T-ccc7, the three entry points are NOT equally protected —
//
//	context-high auto-stamp   read hub.IsOnline (a live socket fact) and
//	                          NOTHING about intent → reachable, deterministic,
//	                          fixed in this pack by the named predicate
//	POST /members/{id}/refocus, POST /self/refocus
//	                          already refuse 409 before the stamp — but on the
//	                          STOP ANCHOR, not on intent: PresenceState reads
//	                          StoppingSince > 0 (StopIntent) and never reads
//	                          DesiredState on its online arm. Every path that
//	                          sets desired offline happens to set the anchor in
//	                          the same write, so today the two coincide. That is
//	                          a correlation, not the invariant.
//
// — so adding the same guard to the latter two would be dead code (proved: with
// the guard removed, an assertion on those two paths stays green). What is NOT
// dead is the risk: their safety rests on a correlation held up by the PRESENCE
// derivation in another file, which mentions neither intent nor these handlers.
// Break either half — drop StopIntent, or introduce a desired-offline
// transition that leaves no anchor — and both paths reopen silently with their
// own code untouched. A comment cannot catch that. This test can, for the shape
// it covers: it asserts the outcome, so it goes red from whichever direction
// that shape is broken.
//
// The fourth stamp site (armMemberOwnerOpHandover, the owner-verb funnel)
// guards on DesiredState directly and is pinned by
// TestMemberOwnerOp_StoppedMemberIsNotRevived; not repeated here.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// deactivatedButStillConnected is the state the cockpit's 停止 button leaves
// behind for the length of the stop grace: desired offline, stop anchor set,
// SSE still live. That live socket is the whole point — it is what makes
// "looks online" and "should be online" disagree.
func deactivatedButStillConnected(id string) Member {
	return Member{
		ID: id, Name: id, Kind: KindAssistant, Effort: "medium",
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
		StoppingSince:    9990,
	}
}

func TestRefocusStampVisibility_NoEntryPointStampsAMemberOnItsWayOffline(t *testing.T) {
	t.Run("context-high auto-stamp", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, deactivatedButStillConnected("m-ccc7-auto"))
		got, _ := s.dal.GetMember("m-ccc7-auto")
		members := []Member{*got}
		connectOnline(t, s, "m-ccc7-auto")

		now := 10000.0
		s.gauge.Set("m-ccc7-auto", map[string]any{
			"context_pct": 99.0, "context_pct_ts": now - 10, "boot_ts": now - 500,
		})

		s.stampContextHighRecycle(members, now)

		if members[0].RefocusSince != 0 {
			t.Fatalf("auto-stamped refocus_since=%v on a member on its way offline: "+
				"the agent's maybeRecycle gate ignores it, so this opens a wind-down "+
				"nobody performs and nobody collects", members[0].RefocusSince)
		}
		// The in-slice member is what the SAME tick observes; the row is what
		// survives it. Both must be clean.
		after, _ := s.dal.GetMember("m-ccc7-auto")
		if after.RefocusSince != 0 {
			t.Fatalf("row was stamped anyway: refocus_since=%v", after.RefocusSince)
		}
	})

	t.Run("owner presses 重新聚焦", func(t *testing.T) {
		api, dal := newGateTestAPI(t)
		putGateMember(t, dal, deactivatedButStillConnected("m-ccc7-refocus"))
		defer online(t, api, "m-ccc7-refocus")()

		r := httptest.NewRequest("POST", "/api/members/m-ccc7-refocus/refocus", nil)
		r = r.WithContext(context.WithValue(r.Context(), claimsContextKey,
			map[string]any{"sub": "owner", "scope": "owner"}))
		rec := httptest.NewRecorder()
		api.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec, r, "m-ccc7-refocus")

		if rec.Code != http.StatusConflict {
			t.Fatalf("refocus on a member on its way offline: want 409, got %d %s",
				rec.Code, rec.Body.String())
		}
		m, _ := dal.GetMember("m-ccc7-refocus")
		if m.RefocusSince != 0 {
			t.Fatalf("refocus stamped refocus_since=%v anyway", m.RefocusSince)
		}
	})

	t.Run("agent asks for its own handover", func(t *testing.T) {
		api, dal := newGateTestAPI(t)
		putGateMember(t, dal, deactivatedButStillConnected("m-ccc7-self"))
		defer online(t, api, "m-ccc7-self")()
		// Past the minimum-liveness floor, so a 429 cannot stand in for the
		// refusal this case is actually about.
		api.gauge.Set("m-ccc7-self", map[string]any{
			"boot_ts": nowSecs() - (minSelfRestartSecs + 100),
		})

		rec := doRestartSelf(api, "m-ccc7-self", "")

		if rec.Code != http.StatusConflict {
			t.Fatalf("restart_self while on its way offline: want 409, got %d %s",
				rec.Code, rec.Body.String())
		}
		m, _ := dal.GetMember("m-ccc7-self")
		if m.RefocusSince != 0 {
			t.Fatalf("restart_self stamped refocus_since=%v anyway", m.RefocusSince)
		}
	})
}
