package main

// renew.go — deciding WHEN this machine's own credential needs replacing.
//
// The warden has to decide this itself: the server-side token-expiry band
// deliberately excludes wardens (server/ocserverd/sse_bands.go), so no signal
// arrives from outside.
//
// The trigger reads `iat` (AGE), not `exp`. Reading exp looks like the obvious
// simplification and is wrong: every warden installed before credentials were
// given expiries holds a credential with NO exp, and an exp-based trigger answers
// "not due" on exactly those machines (it once made renewal dead code
// fleet-wide); and an exp records the lifetime at mint time, whereas lowering the
// setting must move the fleet's renewal moment. The live lifetime is the one the
// station publishes (GET /api/machines/credential-policy); a machine with no
// answer uses the owner-ruled 30-day default.
//
// A machine that does not renew inside the last third of its lifetime is refused
// by the station and needs a hand re-install, and nothing raises an alarm.

import (
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"strings"
	"time"
)

// A FRACTION, not a number of days: the lifetime is owner-adjustable, and a
// fixed threshold longer than a shortened lifetime makes every credential born
// due — the fleet renews every poll forever. A third buys the RETRY WINDOW (ten
// days at the fifteen-minute poll ≈ a thousand attempts) while a healthy machine
// still renews once per lifetime.
//
// It also bounds the setting's floor: a one-day lifetime buys eight hours ≈ 32
// attempts, and the station refuses anything shorter (settings.go) so this never
// degenerates into "one or two polls to get it right".
const renewAtRemainingFraction = 1.0 / 3.0

const (
	// credentialLifetimeDefaultSecs is the owner-ruled 30 days (rc-f2b96594c621),
	// the station's shipped default of auth.warden_credential_lifetime_secs. EVERY
	// machine uses it on its first poll after an upgrade and whenever the policy
	// endpoint is unreachable. Under-estimating is the safe direction (over-
	// estimating renews after the credential is dead), but it is kept EQUAL rather
	// than low: a lower value makes every machine that cannot reach the endpoint
	// renew early, forever.
	credentialLifetimeDefaultSecs = int64(30 * 24 * 60 * 60)

	// Mirrors minWardenCredLifetimeSecs on the server (one day); the two ends are
	// versioned independently, so a station talked into an out-of-range value
	// must not make the fleet renew every poll. Out of range is DISCARDED in
	// favour of the default, not clamped: a nonsense number is not evidence.
	credentialLifetimeFloorSecs = int64(24 * 60 * 60)

	// Mirrors maxAgentTTLSecs (400 days) on the station.
	credentialLifetimeCeilingSecs = int64(400 * 24 * 60 * 60)
)

// credentialRenewJitterWindow only spreads machines that AGE into the threshold.
// It does NOT prevent a fleet-wide simultaneous renewal when the threshold MOVES
// under the fleet (lifetime lowered, or this code shipping to machines already
// past two thirds): measured, 40 of 40 machines renew on the SAME poll. To really
// break up that herd the offset has to be applied AFTER the threshold is crossed —
// this constant is the wrong lever (see
// TestMaybeRenewCredential_TheHerdIsRealWhenTheThresholdMovesUnderTheFleet).
//
// An hour is four times the natural fifteen-minute poll-phase spread. Derived
// from the machine id rather than random: a per-poll random offset makes a
// machine near the boundary flicker due/not-due and makes "when will this machine
// renew" unanswerable.
const credentialRenewJitterWindow = time.Hour

func credentialRenewAfter(lifetimeSecs int64, machineID string) time.Duration {
	if lifetimeSecs < credentialLifetimeFloorSecs || lifetimeSecs > credentialLifetimeCeilingSecs {
		lifetimeSecs = credentialLifetimeDefaultSecs
	}
	lifetime := time.Duration(lifetimeSecs) * time.Second
	base := lifetime - time.Duration(float64(lifetime)*renewAtRemainingFraction)
	return base + credentialRenewJitter(machineID, base)
}

// Capped at an eighth of the threshold, not just by the window: the stagger eats
// into the retry window, which is what saves a machine that was switched off.
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
	h := fnv.New64a()
	_, _ = h.Write([]byte(machineID))
	return time.Duration(h.Sum64() % uint64(window))
}

// TWO ARMS on purpose (normally the shape this repo removes): the AGE arm needs
// the station's lifetime, the EXPIRY arm needs only the credential itself, so a
// machine whose policy fetch has failed for weeks still renews on time; both mean
// "past two thirds of the lifetime". An UNREADABLE token is never due: a parse
// failure is not evidence of expiry.
func credentialDueForRenewal(token string, now time.Time, renewAfter time.Duration) bool {
	if iat, ok := jwtIssuedAt(token); ok && renewAfter > 0 {
		if now.Unix()-iat >= int64(renewAfter/time.Second) {
			return true
		}
	}
	// No expiry must read as "this arm cannot say", never as "expired long ago" —
	// the latter makes the whole fleet renew every poll.
	exp, iat, ok := jwtLifetime(token)
	if !ok {
		return false
	}
	// A missing iat is zero: exp-0 would be a lifetime of the whole epoch and every
	// credential would read as due.
	lifetime := exp - iat
	if iat <= 0 || lifetime <= 0 {
		return now.Unix() >= exp
	}
	remaining := exp - now.Unix()
	return float64(remaining) < float64(lifetime)*renewAtRemainingFraction
}

// Unverified: verification is the server's job and needs a secret this process
// does not have. Zero and negative iat are rejected: an age computed against
// them is decades, which makes every credential permanently due.
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
