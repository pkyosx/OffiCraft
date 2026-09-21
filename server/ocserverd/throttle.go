package main

// throttle.go — the brute-force brake on the credential-guessing surface:
// EVERY seam where a caller submits a secret and the server says yes or no.
//
// The authoritative list is the call sites, not this comment: grep for
// `loginThrottle.begin`. An earlier version of this line enumerated "the three
// PUBLIC/owner seams" and was already wrong when written (it omitted
// mfa/disable) and wronger later (change-password, mfa/activate) — a hardcoded
// count in prose about a set that grows, which is exactly what the root
// CLAUDE.md 〈文件鐵律〉 forbids. If you need the set, ask the compiler.
//
// WHY THIS EXISTS: before it, /api/login had no attempt limit of any kind. The
// only brake was argon2id's own cost (~16-18 ms measured on one Darwin box;
// the figure is hardware-specific and this comment used to quote ~50 ms with no
// machine attached), which is a CPU brake, not a policy one. Under the shipped security model (loopback bind, exposure via a tunnel
// the owner opens themselves — docs/guide/mobile.md) that was defensible; the
// moment an owner follows our OWN mobile instructions and puts a tunnel in
// front of this, it becomes an unlimited online password-guessing oracle that
// leaves no trace. This is the policy brake.
//
// ── THE SHAPE, AND WHY IT IS THIS ONE (owner's ruling, T-19 §0) ─────────────
//
// The brake is exactly TWO mechanisms, and neither of them counts anything:
//
//	1. A FLOOR ON THE WALL-CLOCK OF A REFUSAL. On the way in, a front-door
//	   handler stamps the instant it started; a refusal then waits until that
//	   instant plus throttleFailureFloor before it answers. A SUCCESS returns
//	   the moment it is proven — the owner never waits.
//	2. A CONCURRENCY CAP (throttleMaxInFlight). This is the half that turns
//	   the delay into a rate: without it, N simultaneous attempts each wait
//	   the same 3s in parallel and the floor limits nothing at all.
//
// Together they bound the front door at throttleMaxInFlight guesses per
// throttleFailureFloor, and the slot is held FOR the wait (the floor is spent
// inside the handler, before `release` runs) — that coupling is the whole
// mechanism, not an implementation detail.
//
// 🔴 THE FLOOR IS A DEADLINE, NOT AN ADDED SLEEP, and the difference is the
// security property. "Sleep 3s after you decide" makes the response time
// `work + 3s`, so every difference in `work` still shows through — the very
// thing the single indistinguishable refusal message exists to hide. "Wait
// until start+3s" makes every refusal cost the SAME, whatever it did: a wrong
// password (one argon2id) and a right password with a wrong code (one argon2id
// plus a TOTP verification) are the same number of milliseconds on the wire.
// That equal cost is the floor of this whole design: 「密碼錯」 and 「碼錯」
// must be indistinguishable by MESSAGE and by TIME, because either one telling
// them apart hands a guesser an oracle for which half was already right. Any
// change that makes either distinguishable is a security regression, not a UX
// tweak.
//
// 🔴 WHAT WAS DELETED, AND WHY — a counter, a doubling backoff, a cap, a decay
// window and one process-wide bucket used to sit here. They are gone by the
// owner's decision, and the reason is one sentence: NOBODY MAY BE ABLE TO LOCK
// THE OWNER OUT. That design refused the owner BEFORE verifying their
// password, so noteSuccess was unreachable while blocked; a stranger who could
// reach the login page could therefore hold the owner out with a trickle of a
// dozen failures an hour, and the only escape was a host shell. A flat floor
// has no state for an attacker to drive: nothing anyone does LEAVES ANYTHING
// BEHIND, so the moment they stop, the owner's next correct password is
// answered immediately.
//
// ⚠️ SAY THAT PRECISELY, NOT GENEROUSLY. "The owner always gets in, no matter
// what anyone else is doing" is the sentence this design invites, and it is
// FALSE. While the in-flight pool is full a correct password is refused with
// a 429 like anyone else's, and that is deliberate: letting the right password
// through a full pool would make the gate an oracle that announces the moment
// someone guesses correctly. What is actually true is narrower and is the whole
// improvement: the refusal is TRANSIENT and leaves no residue. Nobody can bank
// failures against the owner, and nobody can keep the door shut by walking away
// from it.
// The cost accepted in exchange is that guessing is slowed rather than
// stopped — at 4 guesses per 3s the front door is ~1.3 attempts a second,
// which against a real password is still hopeless and against a 6-digit code
// alone would not be. That is why the code is never the ONLY secret: every
// seam that takes a code takes the password too.
//
// ⚠️ THERE IS NO PER-CLIENT DIMENSION HERE, and that is a conclusion about the
// deployment rather than laziness. The server binds loopback only; every
// request that is not from this machine arrives through a tunnel or reverse
// proxy, so r.RemoteAddr is 127.0.0.1 for the owner's phone, the owner's
// laptop and an attacker alike, and X-Forwarded-For is attacker-controlled
// text. Bucketing on any of those would produce a per-attacker bucket the
// attacker chooses. (If per-client is ever revisited behind a trusted proxy,
// the key for IPv6 is the /64 prefix, not the address.)
//
// 🔴 ONLY TWO SEAMS ARE BRAKED AT ALL, AND THE LINE IS NOT WHERE PEOPLE GUESS.
// The owner's ruling is 「只有登入需要 throttling」, and the two `begin` call
// sites left are /api/login and /api/auth/set-password. change-password,
// mfa/activate and mfa/disable call nothing here — no floor, no cap, not one
// line.
//
// 🔑 THE DIVIDING LINE IS "CAN AN UNAUTHENTICATED CALLER REACH IT", NOT
// "IS THE CALLER LOGGED IN". Those sound like the same sentence and are not,
// and set-password is exactly where they come apart: it is not a login, nobody
// is "logged in" when they call it, and it is still braked — because it is
// PUBLIC (authPublic in routes.go), it compares a caller-supplied 32-byte
// secret, and it runs the same argon2id as login does (m=19MiB, t=2, p=1 —
// password.go). A stranger who can reach the port can reach it. That is the
// whole test. If a future seam is public and verifies a secret, it belongs
// behind this brake whether or not anyone would call it a login.
//
// The three that were dropped fail that test in the other direction: reaching
// them requires an owner token, so every cost a brake imposes there is paid by
// the honest owner against an attacker who is already inside. And the cap they
// used to take was SHARED with /api/login, so a token holder could fill the
// pool and make the OWNER's login answer 429 — an authenticated caller
// degrading the front door. ⚠️ THE COST IS REAL AND THE OWNER ACCEPTED IT
// KNOWING: whoever holds a live owner token can guess the CURRENT password at
// change-password without a brake. The judgement behind that is that a stolen
// owner token is already the disaster this system defends against —
// 「被進來本身嚴重程度跟密碼外流是一樣的」.
//
// 🔴 BUT "WITHOUT A BRAKE" IS NOT "UNBOUNDED", AND AN EARLIER VERSION OF THIS
// COMMENT SAID IT WAS. That was this file's own author writing a scarier claim
// than the code supports, in a ticket about exactly that failure — so the
// measurement is here rather than an adjective. change-password holds
// settingsMu's WRITE lock across its verifyPassword, so its argon2id calls are
// FULLY SERIALISED: measured 8 concurrent calls at 7.1–7.9x the cost of one
// (ratio ≈ N ⇒ concurrency 1), against /api/login's 4 concurrent at 1.15–1.31x
// (ratio ≈ 1 ⇒ genuinely parallel) as the positive control.
//
// So what a token holder actually gets is ~1 guess per verification, serialised:
// about 60 a second on the machine that was measured (one call 16–18 ms). And
// the whole-process ceiling on concurrent argon2id is login's 4 plus that 1 =
// 5, i.e. ~95 MiB at ~19 MiB each — that number is ARITHMETIC on the measured
// concurrency, not itself measured. Removing the gate from this route did NOT
// change either figure: the old code was begin() → settingsMu →
// verifyPassword, so settingsMu was always the binding constraint and the cap
// never was. What grew is the QUEUE behind the lock, and a queued request costs
// kilobytes, not 19 MiB.
//
// 🔴 THE OTHER HALF OF THAT TRADE IS THE ALERT, NOT A LOCKOUT. When
// /api/login accepts the password and refuses the second factor, the password
// is the thing that has leaked, and no amount of throttling fixes a leaked
// password. So that case tells the assistant, who tells the owner to change it
// (api_auth.go, noteFactorRefusedAfterCorrectPassword). ⚠️ It is dispatched on
// a goroutine ON PURPOSE: doing the DB write inline would make that ONE refusal
// slower than the others and hand back, in milliseconds, exactly the bit the
// identical message and the floor are spent hiding.

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// throttleFailureFloor is the wall-clock a REFUSED credential attempt
	// costs, measured from the moment the handler started rather than from the
	// moment it decided. 3 seconds.
	//
	// The number is a trade with exactly two sides. Upwards: an owner who
	// mistypes their password waits this long before the login wall says so,
	// every time, and that is the ONLY price the honest owner ever pays here
	// (a correct password is answered immediately). Downwards: with
	// throttleMaxInFlight it sets the front door's ceiling, and 4 per 3s is
	// ~1.3 guesses a second — against argon2id-hashed real passwords that is
	// hopeless for an attacker, and it is deliberately NOT strong enough to
	// protect a 6-digit code on its own, which is why no seam ever accepts a
	// code without the password.
	//
	// 🔴 IT IS SPENT WHILE HOLDING AN IN-FLIGHT SLOT. Moving the wait outside
	// the slot (or after `release`) leaves the floor a pure latency cost to the
	// owner that limits nothing.
	//
	// ⚠️ Both braked seams are PUBLIC, so the pool they share is only ever
	// contended by unauthenticated callers. Nothing an authenticated caller
	// does can take a slot away from the login page any more.
	throttleFailureFloor = 3 * time.Second
	// throttleMaxInFlight bounds how many credential verifications may be in
	// progress at once, and it is what makes the floor above mean anything.
	//
	// 🔴 WITHOUT IT THE BRAKE IS BYPASSABLE AND THE STATED DoS DEFENCE IS
	// FICTION. N simultaneous requests would each serve the same 3s floor
	// concurrently — N guesses per window instead of one — and would run N
	// concurrent argon2id verifications at ~19 MiB each (password.go): a few
	// thousand is tens of GB and the process is OOM-killed by one
	// unauthenticated burst. Measured shape: 500 concurrent wrong-password
	// POSTs used to yield ~500 verifications, not ~6.
	//
	// 4 rather than 1: a single slot would 429 the loser of a genuine
	// two-device race, and 4 still bounds memory at ~76 MiB.
	throttleMaxInFlight = 4
	// throttleBurstWait is the Retry-After handed to a caller refused for
	// concurrency. It is the only refusal this file still issues, and there is
	// no deadline to report — the slots free in floor time, so it says "a
	// moment" rather than pretending to know.
	throttleBurstWait = 1 * time.Second
)

// credentialThrottle is the in-flight gate behind the credential seams. Safe
// for concurrent use; the zero value is a ready, empty throttle.
//
// It holds no failure history by design — see the 🔴 WHAT WAS DELETED note at
// the top of this file. There is nothing here for an attacker to drive.
type credentialThrottle struct {
	mu sync.Mutex
	// inFlight counts credential verifications currently running. See
	// throttleMaxInFlight — this is the half that survives concurrency.
	inFlight int
}

// begin is THE gate every credential seam must call. It answers (release, wait,
// blocked): on a refusal `release` is nil and `wait` is what to put in
// Retry-After; on admission the caller MUST `defer release()`.
//
// release is idempotent, so a `defer` plus an early explicit call cannot
// double-free a slot and let the pool drift upward over time.
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

// failureFloor is the wall-clock this server spends on a refused front-door
// credential attempt. Production servers never set the field, so they get
// throttleFailureFloor; the package's own tests shrink it, because a test that
// is not ABOUT the floor should not pay 3 seconds to walk past it.
//
// 🔴 THE ZERO VALUE MUST MEAN "the production floor", not "no floor". A field
// that defaults to off is one forgotten line away from shipping a server with
// no brake and nothing red to say so.
func (s *apiServer) failureFloor() time.Duration {
	if s.credentialFailureFloor > 0 {
		return s.credentialFailureFloor
	}
	return throttleFailureFloor
}

// holdFailureFloor blocks until `started` plus the floor has elapsed, then
// returns. Call it on a front-door refusal IMMEDIATELY BEFORE writing the
// response and while the in-flight slot is still held.
//
// 🔴 IT TAKES THE START INSTANT, NOT A DURATION. Sleeping a fixed amount here
// would leave the work already done visible in the total, which is the leak the
// floor exists to close; taking the deadline makes every refusal cost the same
// regardless of what it had to compute to get here.
func (s *apiServer) holdFailureFloor(started time.Time) {
	if remaining := s.failureFloor() - time.Since(started); remaining > 0 {
		time.Sleep(remaining)
	}
}

// writeThrottled answers a rate-limited credential attempt: 429 with a
// Retry-After header (HTTP's own vocabulary for this, so a client does not have
// to parse prose) through the SAME error envelope every other refusal uses.
//
// The message states the wait and stops there — no hint about whether the
// submitted secret was close, and no suggestion of another endpoint to try.
// 429 maps to `client_error` through the existing errorCodeForStatus fallback,
// so the closed envelope-code vocabulary
// (docs/design/api-error-envelope.codes.json) does not move for this.
func writeThrottled(w http.ResponseWriter, wait time.Duration) {
	secs := int64(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	writeError(w, http.StatusTooManyRequests,
		"too many failed credential attempts; retry in "+strconv.FormatInt(secs, 10)+"s")
}
