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

import (
	"encoding/base64"
	"encoding/json"
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
const renewAtRemainingFraction = 1.0 / 3.0

// credentialDueForRenewal reports whether the credential should be replaced now.
//
// A token with no expiry is NEVER due: warden credentials are permanent today,
// and "no expiry" must read as "nothing to renew" rather than as "expired long
// ago". Getting that backwards would make every warden in the fleet renew on
// every poll, against a server that would happily mint each time.
//
// An UNREADABLE token is also never due, for the same reason a missing token
// file fail-safes to "no token": a parse failure is not evidence of expiry, and
// a machine whose credential this code cannot read is a machine that should be
// left exactly as it is rather than pushed through a credential swap.
func credentialDueForRenewal(token string, now time.Time) bool {
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

// jwtLifetime reads `exp` and `iat` out of a JWT payload WITHOUT verifying the
// signature. Verification is the server's job and needs a secret this process
// does not have; what is being read here is the expiry of the credential this
// process is already holding, to decide whether to ask for another one. ok is
// false when the token is malformed or carries no `exp` — never a guess.
func jwtLifetime(token string) (exp, iat int64, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, 0, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return 0, 0, false
	}
	expF, hasExp := claims["exp"].(float64)
	if !hasExp {
		return 0, 0, false
	}
	iatF, _ := claims["iat"].(float64)
	return int64(expF), int64(iatF), true
}
