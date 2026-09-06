package main

// warden_cred_expiry_tfc53_test.go — T-fc53 第二段: warden credentials carry an
// `exp` again, and the two consequences that have NO other guard.
//
// The mint itself is pinned in api_machines_test.go
// (TestWardenCredentialsCarryTheConfiguredLifetimeAcrossAllMachineMintPaths),
// which walks all five mint paths. What is pinned HERE is the two places the
// change lands that a mint test cannot see:
//
//	① tokenExpiryOf still exempts warden — for a REASON THAT CHANGED. The old
//	   reason ("warden credentials have no exp") is now false, and nothing in
//	   that function or its existing tests would notice, because both were
//	   written in terms of the KIND rather than the reason. The exemption is
//	   correct for new reasons; this file is where those reasons are measured.
//	② the failure-condition numbers written next to wardenCredLifetimeSecsDefault
//	   are DERIVED from it. They cannot be enforced — they are timing properties
//	   of a fleet and of a person's finger on the key-removal button — but they
//	   can be stopped from going stale when the default moves.

import (
	"testing"
	"time"
)

// TestTokenExpiryOf_WardenIsExemptForReasonsTheExpiryDidNotChange.
//
// 🔴 THIS TEST EXISTS BECAUSE THE MUTANT IS INVISIBLE. tokenExpiryOf reads
// `m.Kind == KindWarden` and the comment beside it used to justify that with
// "a warden's token is minted with NO exp claim at all". 第二段 made that
// sentence false. A reader who removes the kind check on those grounds breaks
// nothing that any existing test can see: every assertion about the exemption
// is written in terms of the kind too, so the code and its tests would move
// together and stay green while wardens started being handed 停止 wind-downs
// computed from the wrong setting and the wrong anchor.
//
// So this asserts the REASONS, not the outcome:
//
//   - the warden credential's real lifetime is auth.warden_credential_lifetime_secs,
//     which is NOT auth.agent_token_ttl — the value tokenExpiryOf is handed;
//   - the derivation tokenExpiryOf would produce for a warden is therefore a
//     number unrelated to that credential's actual exp, not an approximation of it;
//   - and the function returns 0 anyway.
//
// Mutant: delete `m.Kind == KindWarden` from tokenExpiryOf → the last arm is red.
func TestTokenExpiryOf_WardenIsExemptForReasonsTheExpiryDidNotChange(t *testing.T) {
	s := newMachinesTestServer(t)

	credLifetime := int64(s.wardenCredLifetimeValue())
	agentTTL := s.agentTokenTTLValue()

	// ① The two clocks are different numbers. If they were ever made equal the
	// arithmetic below would coincide by luck and this test would stop
	// discriminating, so say so out loud rather than let it rot into a tautology.
	if credLifetime == agentTTL {
		t.Fatalf("this test needs the two lifetimes to differ; both are %d s. "+
			"They are separate owner settings (auth.warden_credential_lifetime_secs "+
			"vs auth.agent_token_ttl) and the whole point of the exemption is that "+
			"substituting one for the other is not an approximation", credLifetime)
	}

	// ② A real machine credential, through the real mint.
	m := Member{ID: "m-expiry-box", Name: "box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive}
	putTestMember(t, s, m)
	tok, err := s.mintWardenToken(m)
	if err != nil {
		t.Fatalf("mint warden credential: %v", err)
	}
	claims, err := verifyJWT(tok, s.keys.signingSecret(), time.Now().Unix())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	realExp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("a warden credential must carry an exp since 第二段: %v", claims)
	}
	iat, _ := claims["iat"].(float64)
	if got := int64(realExp - iat); got != credLifetime {
		t.Fatalf("warden credential lifetime %d s, want %d s", got, credLifetime)
	}

	// ③ What tokenExpiryOf's arithmetic would say if the kind check were gone.
	// SessionBootTS is set to the mint instant, which is the MOST favourable case
	// for the derivation — a real warden's session anchor has nothing to do with
	// when its credential was minted at all.
	m.SessionBootTS = iat
	wouldDerive := m.SessionBootTS + float64(agentTTL)
	if wouldDerive == realExp {
		t.Errorf("the derived expiry %v equals the credential's real exp %v — "+
			"this test is no longer measuring anything", wouldDerive, realExp)
	}

	// ④ And the function refuses to answer, which is the behaviour that must hold.
	if got := tokenExpiryOf(m, agentTTL); got != 0 {
		t.Errorf("tokenExpiryOf gave a warden a derived expiry of %v (its credential "+
			"really expires at %v) — the wind-down would be stamped against a "+
			"deadline computed from the wrong setting AND the wrong anchor, and it "+
			"asks for an MCP close-out a warden files none of", got, realExp)
	}
}

// TestWardenCredLifetime_TheFailureConditionNumbersAreDerivedNotTyped.
//
// ⚠️ THIS IS NOT A GUARD ON THE FAILURE CONDITIONS THEMSELVES, and it must not be
// read as one. The two conditions written next to wardenCredLifetimeSecsDefault
// (settings.go) and in spec/lifecycle.md §1.6 — a signing key must stay on the
// ring for two thirds of a lifetime after it stops signing, and a machine off the
// network for longer than the last third loses its credential — are properties of
// a fleet in time and of when a person presses a button. Nothing on this station
// enforces either, and nothing here does.
//
// What this DOES catch is the numbers going stale. Both figures are stated in
// days in prose ("60 days", "30 days"), and the only thing that makes them true
// is the default they are derived from. Move the default and the prose is silently
// wrong — a reader would then wait 60 days on a 30-day margin, which is the exact
// mistake the prose exists to prevent.
func TestWardenCredLifetime_TheFailureConditionNumbersAreDerivedNotTyped(t *testing.T) {
	const day = 86400

	// The prose next to wardenCredLifetimeSecsDefault and in spec/lifecycle.md §1.6.
	const proseKeyRetentionDays = 60 // "two thirds of the lifetime"
	const proseRetryWindowDays = 30  // "the last third of the lifetime"

	if got := wardenCredLifetimeSecsDefault * 2 / 3 / day; got != proseKeyRetentionDays {
		t.Errorf("two thirds of the shipped lifetime is %d days, but settings.go and "+
			"spec/lifecycle.md §1.6 both say a stepped-down signing key must stay on "+
			"the ring for %d days. Fix the prose in BOTH places — an operator who "+
			"waits the number written there and removes the key takes every machine "+
			"still holding a credential it signed off the fleet, at once, with no "+
			"grace", got, proseKeyRetentionDays)
	}
	if got := wardenCredLifetimeSecsDefault / 3 / day; got != proseRetryWindowDays {
		t.Errorf("the retry window is %d days, but the prose says %d. That number is "+
			"how long a machine may be off the network before its credential is gone "+
			"for good and it needs a hand re-install", got, proseRetryWindowDays)
	}

	// The default has to be a legal value of its own setting — a shipped default
	// the write face would refuse is a station that boots on a number nobody can
	// re-enter.
	if !wardenCredLifetimeInRange(wardenCredLifetimeSecsDefault) {
		t.Errorf("the shipped default %d is outside the accepted range %d..%d",
			wardenCredLifetimeSecsDefault, minWardenCredLifetimeSecs, maxWardenCredLifetimeSecs)
	}
	// And it must stay under the 400-day agent ceiling, which is now a REAL
	// ceiling on this credential rather than a documented one: the mint stamps
	// an exp from it.
	if int64(wardenCredLifetimeSecsDefault) > maxAgentTTLSecs {
		t.Errorf("the warden credential lifetime %d s exceeds the %d s ceiling every "+
			"other long-lived credential on this station lives under",
			wardenCredLifetimeSecsDefault, maxAgentTTLSecs)
	}
}
