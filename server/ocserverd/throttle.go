package main

// throttle.go — the brute-force brake on the credential-guessing surface. The
// braked set is the `loginThrottle.begin` call sites, not this comment.
//
// WHY: once an owner follows our own mobile instructions (docs/guide/mobile.md)
// and puts a tunnel in front of this loopback server, /api/login is otherwise an
// unlimited online password-guessing oracle; argon2id's cost is a CPU brake, not
// a policy one.
//
// THE SHAPE (owner ruling T-19 §0) — exactly TWO mechanisms, neither counts:
//
//	1. A FLOOR ON THE WALL-CLOCK OF A REFUSAL: a DEADLINE from handler start,
//	   not an added sleep (a sleep would still leak the work done). A success
//	   returns immediately.
//	2. A CONCURRENCY CAP (throttleMaxInFlight), which turns the delay into a
//	   rate. The slot is held FOR the wait — that coupling is the mechanism.
//
// 🔴 「密碼錯」 and 「碼錯」 must be indistinguishable by MESSAGE and by TIME,
// or a guesser learns which half was right. Making either distinguishable is a
// security regression, not a UX tweak.
//
// 🔴 NO COUNTER, BACKOFF OR LOCKOUT (owner decision): NOBODY MAY BE ABLE TO LOCK
// THE OWNER OUT. A flat floor leaves nothing behind for an attacker to drive.
// Precisely: while the pool is full a correct password gets 429 too (letting it
// through would announce a correct guess) — the refusal is only TRANSIENT.
// ~1.3 guesses/s is hopeless against a password but not a 6-digit code alone,
// which is why no seam takes a code without the password.
//
// ⚠️ NO PER-CLIENT DIMENSION: behind the tunnel r.RemoteAddr is 127.0.0.1 for
// everyone and X-Forwarded-For is attacker-controlled text, so any bucket key
// would be one the attacker chooses.
//
// 🔴 WHICH SEAMS (owner ruling 「只有登入需要 throttling」): the test is "CAN AN
// UNAUTHENTICATED CALLER REACH IT and does it verify a secret", not "is it a
// login". So /api/auth/set-password (public, argon2id) is braked, and
// change-password / mfa/activate / mfa/disable (owner token) are not — a shared
// pool let a token holder make the owner's login 429. Accepted by the owner: a
// live owner token can guess the current password at change-password unbraked
// (「被進來本身嚴重程度跟密碼外流是一樣的」). Not unbounded: it holds settingsMu's
// write lock across verifyPassword, so those argon2id calls are serialised
// (measured: 8 concurrent ≈ 7.1–7.9x one call).
//
// The other half of that trade is the ALERT, not a lockout: password accepted +
// second factor refused means the password leaked, so the assistant is told
// (noteFactorRefusedAfterCorrectPassword), on a goroutine ON PURPOSE — an inline
// DB write would make that one refusal measurably slower.

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// throttleFailureFloor: the only price the honest owner pays (a mistyped
	// password waits this long); with throttleMaxInFlight it sets the front
	// door's ceiling at 4 per 3s.
	throttleFailureFloor = 3 * time.Second
	// throttleMaxInFlight: without it N simultaneous refusals serve the floor in
	// parallel and run N argon2id at ~19 MiB each — one unauthenticated burst
	// OOM-kills the process. 4 rather than 1 so a genuine two-device race does
	// not 429 the loser.
	throttleMaxInFlight = 4
	// throttleBurstWait: slots free in floor time, so there is no deadline to
	// report — it says "a moment".
	throttleBurstWait = 1 * time.Second
)

type credentialThrottle struct {
	mu sync.Mutex

	inFlight int
}

func (t *credentialThrottle) begin() (func(), time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight >= throttleMaxInFlight {
		return nil, throttleBurstWait, true
	}
	t.inFlight++
	released := false
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if released {
			return
		}
		released = true
		t.inFlight--
	}, 0, false
}

// failureFloor: the ZERO VALUE MUST MEAN the production floor, not "no floor" —
// only this package's tests set credentialFailureFloor.
func (s *apiServer) failureFloor() time.Duration {
	if s.credentialFailureFloor > 0 {
		return s.credentialFailureFloor
	}
	return throttleFailureFloor
}

func (s *apiServer) holdFailureFloor(started time.Time) {
	if remaining := s.failureFloor() - time.Since(started); remaining > 0 {
		time.Sleep(remaining)
	}
}

// writeThrottled: 429 maps to `client_error` through errorCodeForStatus, so the
// closed envelope-code vocabulary (docs/design/api-error-envelope.codes.json)
// does not move for this.
func writeThrottled(w http.ResponseWriter, wait time.Duration) {
	secs := int64(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	writeError(w, http.StatusTooManyRequests,
		"too many failed credential attempts; retry in "+strconv.FormatInt(secs, 10)+"s")
}
