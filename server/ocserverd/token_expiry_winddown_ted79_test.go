package main

import "testing"

// token_expiry_winddown_ted79_test.go — a session whose agent token is about to
// expire is asked to wind down BEFORE it expires (owner 2026-08-21, one hour of
// lead), and it is asked with a plain 停止.
//
// The harm this replaces is silent and total. Every step of the offboard
// sequence is an MCP call carrying the session's own bearer token —
// report_stopping, post_chat, the lesson write, report_stopped. So an expired
// token does not make the close-out worse, it makes the close-out IMPOSSIBLE:
// the session cannot file the hand-off, cannot say it is finished, and cannot
// ask for its own restart. Renewal used to rest on the agent remembering.
//
// 🔴 Measured before writing this, twice, because the two failures are
// different and the second one is the one a reader would assume the first
// covers. With stampTokenExpiryWinddown's body emptied, three tests here go red
// — OpensA停止InsideTheLastHour on its FIRST assertion (refocus_op="", nothing
// opened at all), DoesNotRestampAnEpochItAlreadyOpened on its setup, and
// TheCadenceActuallyRunsIt. With the body INTACT but its call removed from
// runReconcileTick, ONLY TheCadenceActuallyRunsIt goes red: every other test in
// this file drives the pass directly, so a correct pass that production never
// calls would look exactly like a working feature. That is why the wiring has a
// test of its own.

// tokenExpiryFixture puts an online staff member on the roster with a session
// anchor placed `remaining` seconds before its derived token expiry, then runs
// ONE reconcile pass and returns the row as it survives the tick.
func tokenExpiryFixture(t *testing.T, s *apiServer, id string, remaining, now float64) Member {
	t.Helper()
	m := testAgent(id)
	m.SessionBootTS = now + remaining - float64(s.agentTokenTTLValue())
	putTestMember(t, s, m)
	l := connectOnline(t, s, m.ID)
	t.Cleanup(func() { drainListener(l) })
	members := []Member{m}
	s.stampTokenExpiryWinddown(members, now)
	return members[0]
}

func TestTokenExpiry_OpensA停止InsideTheLastHour(t *testing.T) {
	const now = 1_769_904_000.0 // 2026-02-01T00:00:00Z
	s := newReconcileTestServer(t)

	got := tokenExpiryFixture(t, s, "m-ed79-token", tokenExpiryLeadSecs-1, now)
	if got.RefocusOp != refocusOpTokenExpiry || got.RefocusSince != now {
		t.Fatalf("inside the lead: refocus_op=%q refocus_since=%v, want %q at %v — "+
			"a session whose token is about to die must be asked to close out while "+
			"the calls that close it out still work",
			got.RefocusOp, got.RefocusSince, refocusOpTokenExpiry, now)
	}

	// …and it is a 停止, not an 加速停止: nothing collects it on a clock and the
	// sentence quotes no time. The owner's words were 「就是呼叫軟下線，然後等他
	// report_stopped 以後再呼叫上線」 — waiting for the report IS the collection.
	cfg := s.reconcileConfigLive()
	if grace, clocked := recycleGraceFor(got.RefocusOp, cfg); clocked {
		t.Fatalf("token expiry was put on a %v s clock — the owner asked for a soft "+
			"stop, and a countdown here would cut short the very hand-off the "+
			"trigger exists to make possible", grace)
	}
	obs := obsOf(got.ID, DesiredStateOnline, true)
	obs.RefocusSince = got.RefocusSince
	obs.RefocusOp = got.RefocusOp
	for _, elapsed := range []float64{1, 120, 3600, 365 * 24 * 3600} {
		if d := reconcileDecide(obs, newReconcileState(), cfg, now+elapsed); d.Command == reconcileCmdStop {
			t.Fatalf("+%.0fs: the token-expiry stop was collected on a clock (%s)",
				elapsed, d.Reason)
		}
	}
	notice, _ := s.offboardDeltaPayload(got)["offboard_notice"].(string)
	if notice == "" {
		t.Fatal("a token-expiry wind-down must carry a notice at all — a stamp the " +
			"agent is never told about is not a request, it is a silent marker")
	}
	assertQuotesNoTime(t, refocusOpTokenExpiry, notice)
}

// The CONTROL for the assertion above. Without it a trigger that fires for every
// live session at every moment would pass the test above unchanged.
func TestTokenExpiry_NothingOpensOutsideTheLead(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)

	for _, remaining := range []float64{
		tokenExpiryLeadSecs + 1,
		tokenExpiryLeadSecs * 2,
		30 * 86400,
	} {
		got := tokenExpiryFixture(t, s, "m-ed79-early", remaining, now)
		if got.RefocusSince != 0 || got.RefocusOp != "" {
			t.Fatalf("%.0fs of token life left: opened %q at %v — a wind-down this "+
				"early costs a whole session for nothing",
				remaining, got.RefocusOp, got.RefocusSince)
		}
	}
}

// Past the derived expiry the token is certainly dead, so the sequence the stamp
// asks for cannot be filed. Printing it anyway would hand a session a list of
// instructions every one of which answers 401.
func TestTokenExpiry_NothingOpensAfterTheTokenIsAlreadyGone(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)

	for _, remaining := range []float64{0, -1, -86400} {
		got := tokenExpiryFixture(t, s, "m-ed79-late", remaining, now)
		if got.RefocusSince != 0 || got.RefocusOp != "" {
			t.Fatalf("%.0fs of token life left: opened %q — the close-out it asks "+
				"for is a series of calls on a token that no longer verifies",
				remaining, got.RefocusOp)
		}
	}
}

// The de-dup. The cadence runs every 30 s and the lead is an hour, so without a
// cooldown this trigger would re-stamp ~120 times — and each re-stamp calls
// armRefocusEpoch, which zeroes the wind-down anchors BY DESIGN. The close-out
// would be destroyed twice a minute for the whole final hour.
func TestTokenExpiry_DoesNotRestampAnEpochItAlreadyOpened(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)

	got := tokenExpiryFixture(t, s, "m-ed79-dedup", tokenExpiryLeadSecs-1, now)
	if got.RefocusSince != now {
		t.Fatalf("first pass did not open an epoch: %+v", got)
	}
	// A later tick, with the agent's own stopping report in flight.
	got.StoppingSince = now + 5
	putTestMember(t, s, got)
	members := []Member{got}
	s.stampTokenExpiryWinddown(members, now+30)
	if members[0].RefocusSince != now {
		t.Errorf("refocus_since moved to %v on the next tick — re-stamping restarts "+
			"the epoch and armRefocusEpoch zeroes the anchors, so the close-out in "+
			"flight is thrown away", members[0].RefocusSince)
	}
	if members[0].StoppingSince != now+5 {
		t.Errorf("stopping_since=%v — the agent's own close-out anchor was erased",
			members[0].StoppingSince)
	}
}

// An agent that has already said 「我收完了」 is not asked again — the same
// ruling the context pair carries, and for the same reason: armRefocusEpoch
// cannot tell a fresh report from a stale latch, so a stamp here DESTROYS a
// finished close-out and asks a session that is done to do it all over.
func TestTokenExpiry_DoesNotReopenAFinishedCloseOut(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)

	m := testAgent("m-ed79-finished")
	m.SessionBootTS = now + tokenExpiryLeadSecs - 1 - float64(s.agentTokenTTLValue())
	m.StoppedSince = now - 10
	putTestMember(t, s, m)
	l := connectOnline(t, s, m.ID)
	defer drainListener(l)
	// The gauge boot_ts is what tells THIS session's report apart from a
	// predecessor's latch (下線 → 活化 leaves one behind).
	s.gauge.Set(m.ID, map[string]any{"boot_ts": m.SessionBootTS})

	members := []Member{m}
	s.stampTokenExpiryWinddown(members, now)
	if members[0].RefocusSince != 0 {
		t.Errorf("opened a wind-down on a session that already reported stopped — "+
			"the stamp zeroes stopped_since, so the agent's finished close-out is "+
			"destroyed and it is asked to run the sequence again (refocus_since=%v)",
			members[0].RefocusSince)
	}
}

// A stamp that cannot reach the agent is not a weaker signal, it is NO signal:
// the agent's own gate (cli/ocagent maybeRecycle) requires desired_state=online,
// and an offline member is not reading anything at all. Both are stranded
// markers that activate does not clear.
func TestTokenExpiry_OnlyStampsWhatTheAgentWillActuallySee(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)
	anchor := now + tokenExpiryLeadSecs - 1 - float64(s.agentTokenTTLValue())

	t.Run("no live session", func(t *testing.T) {
		m := testAgent("m-ed79-offline")
		m.SessionBootTS = anchor
		putTestMember(t, s, m)
		members := []Member{m}
		s.stampTokenExpiryWinddown(members, now)
		if members[0].RefocusSince != 0 {
			t.Errorf("stamped a member with no live session (%v)", members[0].RefocusSince)
		}
	})

	t.Run("the server no longer wants it online", func(t *testing.T) {
		m := testAgent("m-ed79-going-down")
		m.SessionBootTS = anchor
		m.DesiredState = DesiredStateOffline
		putTestMember(t, s, m)
		l := connectOnline(t, s, m.ID)
		defer drainListener(l)
		members := []Member{m}
		s.stampTokenExpiryWinddown(members, now)
		if members[0].RefocusSince != 0 {
			t.Errorf("stamped a member already on its way out (%v)", members[0].RefocusSince)
		}
	})
}

// 🔴 THE DERIVATION ITSELF, checked against the thing it claims to describe.
// "session_boot_ts + agent_token_ttl is the expiry, no new column needed" is an
// inference across two files (buildStartFrame mints, anchorSessionBoot stamps),
// and an inference is exactly what nobody re-checks after one of the two moves.
// This mints a token the way the START path does and compares the exp claim the
// verifier will actually enforce against what tokenExpiryOf answers.
func TestTokenExpiry_DerivationMatchesTheTokenTheStartPathMints(t *testing.T) {
	s := newReconcileTestServer(t)
	ttl := s.agentTokenTTLValue()

	m := testAgent("m-ed79-derive")
	const mintedAt = 1_769_904_000
	// Minted at a KNOWN instant so the comparison is exact rather than
	// wall-clock-dependent. This is mintMemberToken's own body (api_auth.go:
	// mintMemberToken → mintAgentToken → mintJWT with the member's desired
	// machine), with time.Now() pinned — the one thing that cannot be injected.
	token, err := mintJWT(m.ID, "agent", ttl, s.keys.signingSecret(), mintedAt, m.DesiredMachineID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	claims, err := verifyJWT(token, s.keys.signingSecret(), mintedAt)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	exp, ok := asNumber(claims["exp"])
	if !ok {
		t.Fatal("a staff boot token must carry an exp — the whole trigger is " +
			"about that claim, and a token without one would never expire")
	}

	// The session anchor is stamped on the SSE first-connect edge, i.e. at or
	// after the mint. At zero boot latency the derivation is EXACT.
	m.SessionBootTS = mintedAt
	if got := tokenExpiryOf(m, ttl); got != exp {
		t.Fatalf("tokenExpiryOf=%v but the token's exp is %v — the trigger would "+
			"fire against a lifetime the verifier does not enforce", got, exp)
	}
	// With real boot latency the anchor is LATER, so the derivation is an UPPER
	// bound: the trigger fires late, never before the token exists. Pinned so a
	// future change that makes it an under-estimate (firing on a token that has
	// not been minted yet) is a red build.
	for _, latency := range []float64{1, 30, 300} {
		m.SessionBootTS = mintedAt + latency
		if got := tokenExpiryOf(m, ttl); got < exp {
			t.Errorf("boot latency %.0fs: derived expiry %v is EARLIER than the real "+
				"one %v — the derivation must never under-estimate", latency, got, exp)
		}
	}

	// Not derivable → 0, and every caller treats 0 as "do nothing". A warden's
	// credential is minted with NO exp at all (mintJWTWithoutExpiry), so asking
	// this question about one would invent an expiry that does not exist.
	warden := testAgent("mach-ed79")
	warden.Kind = KindWarden
	warden.SessionBootTS = mintedAt
	if got := tokenExpiryOf(warden, ttl); got != 0 {
		t.Errorf("a warden got a derived token expiry of %v — its credential has no "+
			"exp claim at all", got)
	}
	unanchored := testAgent("m-ed79-unanchored")
	if got := tokenExpiryOf(unanchored, ttl); got != 0 {
		t.Errorf("a member with no session anchor got a derived expiry of %v", got)
	}
}

// The trigger has to be WIRED, not merely correct. A pass that nothing calls is
// indistinguishable from one that does nothing, and every other test in this
// file drives stampTokenExpiryWinddown directly — so this is the only one that
// would notice the call disappearing from the cadence.
func TestTokenExpiry_TheCadenceActuallyRunsIt(t *testing.T) {
	const now = 1_769_904_000.0
	s := newReconcileTestServer(t)

	m := testAgent("m-ed79-cadence")
	m.SessionBootTS = now + tokenExpiryLeadSecs - 1 - float64(s.agentTokenTTLValue())
	putTestMember(t, s, m)
	l := connectOnline(t, s, m.ID)
	defer drainListener(l)

	s.runReconcileTick(now)

	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("reread: %v", err)
	}
	if got.RefocusOp != refocusOpTokenExpiry || got.RefocusSince != now {
		t.Fatalf("after one cadence tick: refocus_op=%q refocus_since=%v, want %q "+
			"at %v — the pass is not wired into runReconcileTick, so nothing in "+
			"production ever runs it", got.RefocusOp, got.RefocusSince,
			refocusOpTokenExpiry, now)
	}
}
