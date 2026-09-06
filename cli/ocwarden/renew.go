package main

// renew.go — deciding WHEN this machine's own credential needs replacing.
//
// Only the decision lives here. Fetching a new credential, writing it, and
// re-executing to pick it up are separate and have side effects; this file is
// the part that can be reasoned about and tested on its own.
//
// WHY THE WARDEN HAS TO DECIDE THIS ITSELF. The server-side token-expiry
// notification band deliberately excludes wardens (server/ocserverd/sse_bands.go
// — credential lifetime is machine governance, and a warden cannot act on the
// restart the band asks for). So there is no signal arriving from outside; the
// only thing that knows this credential is running out is the process holding
// it.
//
// ── T-fc53 第一段: AGE, NOT REMAINING TIME ──────────────────────────────────
//
// 🔴 THE ORIGINAL RULE COULD NOT FIRE AT ALL. It asked "how much of the ORIGINAL
// lifetime is left", which is a question about `exp` — and mintWardenToken issues
// warden credentials with NO exp (server/ocserverd/jwt.go: mintJWTWithoutExpiry).
// jwtLifetime therefore answered ok=false, credentialDueForRenewal returned false,
// and the whole renewal path was dead code on every machine in the fleet. The one
// thing that ever reached it was the T-80 station demand, which is a different
// question (a retired signing key) and does not run on a clock.
//
// So the trigger now reads `iat` — HOW LONG AGO WAS THIS CREDENTIAL ISSUED —
// which every warden credential has carried since the initial tree (jwtClaims.Iat
// has no omitempty, so it is always serialised). Age needs no exp, which is what
// lets this land BEFORE credentials are given expiries again: while they are
// still permanent, the worst a bug here can do is renew too often or not at all,
// and neither takes a machine off the network. Putting the expiry back first
// would have meant shipping an untested renewal path against credentials that had
// started to die.
//
// THE THRESHOLD IS THE SAME THRESHOLD, expressed from the other end. "Under a
// third of the lifetime remaining" IS "past two thirds of the lifetime old", so
// a fleet moving from one rule to the other does not change when it renews. What
// changes is where the number comes from: the lifetime is no longer readable off
// the credential, so the station publishes it (GET /api/machines/credential-policy)
// and this process keeps the last answer. A machine that has never had an answer
// uses the shipped default, which is the owner-ruled 30 days — i.e. exactly the
// behaviour the expiry rule would have produced.

import (
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"strings"
	"time"
)

// renewAtRemainingFraction is the share of a credential's ORIGINAL lifetime
// that, once left, starts renewal attempts. At the owner-ruled 30-day lifetime
// this is ten days.
//
// WHY A FRACTION AND NOT A NUMBER OF DAYS. The lifetime is an owner-adjustable
// setting. A hard-coded threshold survives only the lifetime it was written
// for: shorten the lifetime past it and every credential is born already due,
// so the fleet renews on every poll forever. A fraction moves with it.
//
// WHY A THIRD. What this threshold actually buys is a RETRY WINDOW: ten days at
// the fifteen-minute poll is roughly a thousand attempts, so a machine has to
// be off for ten days straight before its credential really dies. A quarter
// would also do; there is no reason to make the window smaller. What it must
// not be is generous enough to have every machine renewing for most of its
// life — at a third, a healthy machine spends twenty days doing nothing and
// renews once per lifetime.
//
// 🔴 IT IS ALSO WHAT BOUNDS THE SETTING'S FLOOR, and that link is the reason the
// two numbers must be read together. The retry window is lifetime/3; the poll is
// fifteen minutes; so a one-day lifetime buys eight hours ≈ 32 attempts. The
// station refuses a lifetime shorter than that (settings.go) precisely so this
// fraction never degenerates into "one or two polls to get it right".
const renewAtRemainingFraction = 1.0 / 3.0

const (
	// credentialLifetimeDefaultSecs is what this warden assumes the station's
	// credential lifetime is until the station tells it otherwise. It is the
	// owner-ruled 30 days, i.e. the same value the station ships as the default
	// of auth.warden_credential_lifetime_secs.
	//
	// 🔴 THE DEFAULT IS NOT A FALLBACK NOBODY REACHES. It is what EVERY machine
	// uses on its first poll after an upgrade, and it is what a machine uses for
	// as long as the policy endpoint is unreachable. It therefore has to be the
	// SAFE end of the range, not the eager one: assuming a lifetime that is too
	// SHORT makes a fleet renew far more often than the owner asked for, while
	// assuming one that is too LONG only delays a renewal on credentials that
	// (in this package) cannot expire anyway.
	credentialLifetimeDefaultSecs = int64(30 * 24 * 60 * 60)

	// credentialLifetimeFloorSecs is the SHORTEST lifetime this process will act
	// on, whatever the station says. It mirrors minWardenCredLifetimeSecs on the
	// server (one day) and exists because the two ends of this wire are versioned
	// independently: a station that has been talked into an out-of-range value,
	// or a future one that widens its own floor, must not be able to talk every
	// machine in the fleet into renewing on every poll. A value below the floor
	// is not clamped UP to it — it is discarded, and the shipped default is used
	// instead, because a nonsense number is not evidence about what the owner
	// wants and the default is the one value known to be safe.
	credentialLifetimeFloorSecs = int64(24 * 60 * 60)

	// credentialLifetimeCeilingSecs mirrors maxAgentTTLSecs (400 days), the
	// ceiling every long-lived credential on this station already lives under.
	credentialLifetimeCeilingSecs = int64(400 * 24 * 60 * 60)
)

// credentialRenewJitterWindow is how far a machine's own renewal moment is
// pushed back from the moment the arithmetic alone would pick.
//
// 🔴 WHAT IT ACTUALLY DOES, AND WHAT IT DOES NOT. It spreads the fleet when
// machines REACH the threshold by ageing into it: their ages are close together
// near the boundary, so a per-machine offset of up to an hour decides who acts on
// which poll.
//
// 🔴 IT DOES NOT PREVENT THE FLEET-WIDE SIMULTANEOUS RENEWAL, and an earlier
// version of this comment claimed that it did. When the threshold MOVES UNDER the
// whole fleet — the owner lowering the lifetime setting, or this code shipping to
// machines whose credentials are already older than two thirds of the default —
// every machine is past threshold+offset at the same instant, and an offset of at
// most an hour changes nothing: they all act on their very next poll. Measured,
// not reasoned: a 40-machine harness with realistic ages (5..83 days) and the
// lifetime lowered to 3 days renews 40 of 40 on the SAME poll.
//
// So the fleet-wide event the owner was told about and accepted is REAL and is NOT
// softened by this constant. What keeps it survivable is elsewhere: every failure
// on this path keeps the old credential, and a failed exec does not exit. If you
// are here because you need the herd actually broken up, this constant is the
// wrong lever — the offset has to be applied AFTER the threshold is crossed, not
// added to the threshold, and that is a different behaviour with its own failure
// modes. See TestMaybeRenewCredential_TheHerdIsRealWhenTheThresholdMovesUnderTheFleet.
//
// WHY AN HOUR (for the ageing-into-it case above). The poll is fifteen minutes, so
// poll PHASE alone already spreads the fleet over fifteen minutes — machines wake fifteen minutes after their own
// start, not on a shared wall clock. An hour is four times that, so it dominates
// the natural spread rather than disappearing into it, and it puts at most about
// a quarter of the fleet in any one poll bucket. It is also small enough to be
// irrelevant to the credential itself: an hour out of the SHORTEST legal lifetime
// (one day) is 4% of the lifetime and one eighth of the eight-hour retry window.
//
// WHY IT IS DETERMINISTIC PER MACHINE AND NOT RANDOM. A fresh random offset on
// every poll does not stagger anything — it re-rolls the threshold each time, so
// a machine near the boundary flickers due/not-due and the fleet is spread only
// by luck. Worse, it makes "when will this machine renew" unanswerable, and that
// question is exactly what somebody debugging a stuck host will ask. Derived from
// the machine id, the offset is stable for the life of the machine, identical
// across restarts, and reproducible by hand.
const credentialRenewJitterWindow = time.Hour

// credentialRenewAfter turns the station's LIFETIME into the age at which THIS
// machine renews: two thirds of the lifetime, plus this machine's own stagger.
//
// lifetimeSecs is the station's answer, or 0 when it has never given one. Any
// value outside the floor/ceiling is discarded in favour of the shipped default
// — see credentialLifetimeFloorSecs for why it is discarded rather than clamped.
//
// machineID is what the stagger is derived from; an empty id simply gets no
// stagger, which is right for the one case that produces it (an unconfigured or
// test warden, where there is no fleet to spread).
func credentialRenewAfter(lifetimeSecs int64, machineID string) time.Duration {
	if lifetimeSecs < credentialLifetimeFloorSecs || lifetimeSecs > credentialLifetimeCeilingSecs {
		lifetimeSecs = credentialLifetimeDefaultSecs
	}
	lifetime := time.Duration(lifetimeSecs) * time.Second
	// The age twin of "under a third remaining". Written as lifetime minus the
	// fraction rather than as a 2/3 literal so there is exactly ONE number in
	// this file to change if the fraction ever moves, and no second place that
	// can be updated out of step with it.
	base := lifetime - time.Duration(float64(lifetime)*renewAtRemainingFraction)
	return base + credentialRenewJitter(machineID, base)
}

// credentialRenewJitter is this machine's stable offset within the window.
//
// 🔴 IT IS CAPPED BY THE THRESHOLD IT IS ADDED TO, not just by the window. The
// stagger eats into the retry window, and the retry window is what stops a
// machine that was switched off for a while from losing its credential. Bounding
// it at an eighth of the threshold means it can never be more than a small slice
// of that window whatever lifetime the owner picks — at the shipped 30 days the
// cap is 2.5 days and the hour applies untouched; only an absurdly short
// lifetime could ever make the cap bite, and then it bites in the safe direction.
func credentialRenewJitter(machineID string, base time.Duration) time.Duration {
	if machineID == "" {
		return 0
	}
	window := credentialRenewJitterWindow
	if cap := base / 8; cap < window {
		window = cap
	}
	if window <= 0 {
		return 0
	}
	// FNV-1a over the id: a stable, dependency-free spread. This is NOT a
	// security decision — nothing is being hidden, and a machine that could
	// choose its own offset would gain nothing but its own renewal moment — so
	// the cheapest well-distributed hash is the right one.
	h := fnv.New64a()
	_, _ = h.Write([]byte(machineID))
	return time.Duration(h.Sum64() % uint64(window))
}

// credentialDueForRenewal reports whether the credential should be replaced now.
//
// renewAfter is the age at which this machine renews (credentialRenewAfter).
//
// TWO ARMS, AND THE SECOND IS NOT DEAD WEIGHT. The AGE arm is the one that fires
// today, because warden credentials carry no exp. The EXPIRY arm is kept because
// it answers the same question from a fact the credential carries ITSELF: once
// credentials are given expiries back (T-fc53 第二段), a machine whose policy
// fetch has been failing for weeks still renews on time. Two independent paths to
// one decision is normally the shape this repo removes; here they are deliberate,
// because they fail independently — one needs the station reachable, the other
// needs nothing at all — and they agree by construction (both are "past two
// thirds of the lifetime").
//
// An UNREADABLE token is never due, for the same reason a missing token file
// fail-safes to "no token": a parse failure is not evidence of expiry, and a
// machine whose credential this code cannot read is a machine that should be
// left exactly as it is rather than pushed through a credential swap.
func credentialDueForRenewal(token string, now time.Time, renewAfter time.Duration) bool {
	// AGE ARM. `iat` is present on every credential this station mints, with or
	// without an exp, so this is the arm that actually runs.
	if iat, ok := jwtIssuedAt(token); ok && renewAfter > 0 {
		if now.Unix()-iat >= int64(renewAfter/time.Second) {
			return true
		}
	}
	// EXPIRY ARM — the original rule, unchanged.
	//
	// A token with no expiry is NEVER due here: "no expiry" must read as
	// "nothing this arm can say" rather than as "expired long ago". Getting that
	// backwards would make every warden in the fleet renew on every poll,
	// against a server that would happily mint each time.
	exp, iat, ok := jwtLifetime(token)
	if !ok {
		return false
	}
	// A MISSING iat is zero, and zero is not a timestamp: exp-0 would compute a
	// "lifetime" of the whole epoch, whose third is decades, so every credential
	// would read as due. Treat an absent or non-preceding iat as an unknown
	// lifetime and fall back to the expiry itself — due once the moment has
	// actually passed — rather than inventing a window.
	lifetime := exp - iat
	if iat <= 0 || lifetime <= 0 {
		return now.Unix() >= exp
	}
	remaining := exp - now.Unix()
	return float64(remaining) < float64(lifetime)*renewAtRemainingFraction
}

// jwtIssuedAt reads `iat` out of a JWT payload WITHOUT verifying the signature,
// for the same reason jwtLifetime does not: verification is the server's job and
// needs a secret this process does not have.
//
// ok is false when the token is malformed or carries no POSITIVE `iat` — never a
// guess. Zero and negative are rejected together and deliberately: neither is a
// timestamp, and an age computed against them is `now` itself, i.e. decades,
// which would make every credential permanently due. That is the fleet-wide
// renew-on-every-poll failure, reached through a claim an attacker does not even
// need to forge — a truncated or hand-edited token file gets there on its own.
func jwtIssuedAt(token string) (iat int64, ok bool) {
	claims, ok := jwtClaimsOf(token)
	if !ok {
		return 0, false
	}
	iatF, hasIat := claims["iat"].(float64)
	if !hasIat || iatF <= 0 {
		return 0, false
	}
	return int64(iatF), true
}

// jwtLifetime reads `exp` and `iat` out of a JWT payload WITHOUT verifying the
// signature. Verification is the server's job and needs a secret this process
// does not have; what is being read here is the expiry of the credential this
// process is already holding, to decide whether to ask for another one. ok is
// false when the token is malformed or carries no `exp` — never a guess.
func jwtLifetime(token string) (exp, iat int64, ok bool) {
	claims, ok := jwtClaimsOf(token)
	if !ok {
		return 0, 0, false
	}
	expF, hasExp := claims["exp"].(float64)
	if !hasExp {
		return 0, 0, false
	}
	iatF, _ := claims["iat"].(float64)
	return int64(expF), int64(iatF), true
}

// jwtClaimsOf decodes the payload segment. Shared by the two readers above so
// "what counts as a malformed token" is decided in ONE place: two copies of this
// would be two chances for the readers to disagree about a token, and they are
// consulted about the same token within microseconds of each other.
func jwtClaimsOf(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}
