package main

// renew_age_tfc53_test.go — T-fc53 第一段: renewing on AGE rather than on time
// remaining, the adjustable lifetime that sets the threshold, and the stagger
// that keeps the fleet from acting in one instant.
//
// 🔴 WHY THESE ARMS AND NOT OTHERS. When 第一段 landed, the credential this
// decides about was permanent, so the only direction that cost anything was
// EAGERNESS: a predicate that says yes too readily puts every machine on the
// mint endpoint on every poll and execs the whole fleet at once. 第二段 gave the
// credentials an exp, so LAZINESS now costs a HOST as well — a machine that
// misses its retry window needs a hand re-install. So
// every "is due" arm below is paired with a "is not due" control on a token that
// differs in exactly one field — a version of this code that simply always said
// yes has to fail one of each pair.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const testDay = 24 * time.Hour

// fmtI64 keeps the JSON fixtures below readable without pulling a formatter into
// a file whose subject is arithmetic.
func fmtI64(n int64) string { return strconv.FormatInt(n, 10) }

// permanentToken is the pre-第二段 credential shape, still in the field on every
// machine that has not yet renewed: a `sub`, an `iat`, and NO `exp`. Every
// fixture in this file uses it, because a
// fixture carrying an exp would be answered by the expiry arm and would prove
// nothing about the arm this ticket added.
func permanentToken(t *testing.T, sub string, issued time.Time) string {
	t.Helper()
	return jwtWith(t, map[string]any{"sub": sub, "iat": issued.Unix()})
}

// ---------------------------------------------------------------------------
// ① the predicate
// ---------------------------------------------------------------------------

func TestCredentialDueForRenewal_AgeArmReadsPermanentCredentials(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	const renewAfter = 20 * testDay

	for _, tc := range []struct {
		name  string
		token string
		want  bool
		why   string
	}{
		{
			name:  "permanent credential older than the threshold",
			token: permanentToken(t, thisMachine, now.Add(-21*testDay)),
			want:  true,
			why: "THE HEADLINE OF THIS TICKET. Before it, a credential with no exp was " +
				"never due at any age, so the renewal path was unreachable on every " +
				"machine in the fleet and had never once been observed to run",
		},
		{
			name:  "permanent credential younger than the threshold",
			token: permanentToken(t, thisMachine, now.Add(-19*testDay)),
			want:  false,
			why: "the control for the arm above. Without it, a predicate that answered " +
				"'due' for every permanent credential would pass the whole of this file " +
				"— and put the fleet on the mint endpoint once per poll, forever",
		},
		{
			name:  "permanent credential exactly at the threshold",
			token: permanentToken(t, thisMachine, now.Add(-20*testDay)),
			want:  true,
			why: "the boundary is inclusive on the AGE side. It has to be decided " +
				"somewhere, and one poll early costs one request while one poll late " +
				"costs a poll interval off the retry window",
		},
		{
			name:  "permanent credential issued moments ago",
			token: permanentToken(t, thisMachine, now),
			want:  false,
			why: "a credential the station has JUST minted must never read as due — " +
				"that is the once-per-poll exec runaway the whole path is built to avoid",
		},
		{
			name:  "permanent credential with no iat at all",
			token: jwtWith(t, map[string]any{"sub": thisMachine}),
			want:  false,
			why: "with neither exp nor iat there is nothing to date the credential by. " +
				"Missing must not read as epoch zero, whose age is decades and which " +
				"would therefore be due forever",
		},
		{
			name:  "permanent credential with a zero iat",
			token: jwtWith(t, map[string]any{"sub": thisMachine, "iat": 0}),
			want:  false,
			why:   "same as above, reached through a truncated or hand-edited token file",
		},
		{
			name:  "permanent credential with a negative iat",
			token: jwtWith(t, map[string]any{"sub": thisMachine, "iat": -1}),
			want:  false,
			why:   "a negative timestamp is not a young credential, it is a broken one",
		},
		{
			name:  "permanent credential with a non-numeric iat",
			token: jwtWith(t, map[string]any{"sub": thisMachine, "iat": "yesterday"}),
			want:  false,
			why:   "a wrong TYPE must be declined, not coerced to zero and then to 'due'",
		},
		{
			name:  "an unreadable token is never due at any age",
			token: "not.a.jwt",
			want:  false,
			why: "a parse failure is not evidence about age; a machine whose credential " +
				"this code cannot read is one to leave alone",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := credentialDueForRenewal(tc.token, now, renewAfter); got != tc.want {
				t.Errorf("credentialDueForRenewal(age arm) = %v, want %v — %s",
					got, tc.want, tc.why)
			}
		})
	}
}

// TestCredentialDueForRenewal_AZeroThresholdDisablesTheAgeArm pins the one input
// the age arm must refuse to act on. renewAfter is a duration derived from a
// setting; a wiring mistake that hands it the zero value would otherwise make
// EVERY credential due (age >= 0 always) on EVERY machine on EVERY poll. Nothing
// in production produces zero — credentialRenewAfter never returns it — which is
// exactly why the guard needs a test rather than an argument.
func TestCredentialDueForRenewal_AZeroThresholdDisablesTheAgeArm(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ancient := permanentToken(t, thisMachine, now.Add(-9999*testDay))
	if credentialDueForRenewal(ancient, now, 0) {
		t.Error("a zero threshold made an old permanent credential due. Zero is not a " +
			"threshold, it is an unset field, and acting on it renews the entire fleet " +
			"on every poll")
	}
	// Control: the same token IS due once a real threshold is supplied, so the
	// refusal above is about the zero and not about the token.
	if !credentialDueForRenewal(ancient, now, 20*testDay) {
		t.Fatal("the control failed: this token is not due even at a real threshold, so " +
			"the assertion above proves nothing")
	}
}

// ---------------------------------------------------------------------------
// ② the threshold: turning the station's lifetime into an age
// ---------------------------------------------------------------------------

// TestCredentialLifetimeDefaultSecs_IsNinetyDays writes the warden's built-in
// default out as a plain number, because every other use of it in this package
// derives the expected value from the constant itself and therefore holds for any
// value it is given. The station ships the same number as the default of
// auth.warden_credential_lifetime_secs and nothing compiles the two together, so
// each end pins its own against the same literal.
//
// Measured: setting credentialLifetimeDefaultSecs to 30 days fails THIS test and
// nothing else in cli/ocwarden, and before it existed that edit kept the whole
// package green.
func TestCredentialLifetimeDefaultSecs_IsNinetyDays(t *testing.T) {
	const ninetyDaysInSeconds = 7776000
	if credentialLifetimeDefaultSecs != ninetyDaysInSeconds {
		t.Errorf("credentialLifetimeDefaultSecs = %d s, want %d s (90 days, owner "+
			"2026-09-06). A machine that cannot reach the policy endpoint renews on "+
			"this number, so it must equal the lifetime the station actually issues",
			credentialLifetimeDefaultSecs, ninetyDaysInSeconds)
	}
}

func TestCredentialRenewAfter_TranslatesTheLifetimeAndRefusesNonsense(t *testing.T) {
	// No machine id ⇒ no stagger, so these assertions are about the arithmetic
	// alone. The stagger has its own test below.
	const noStagger = ""
	twoThirds := func(secs int64) time.Duration {
		lifetime := time.Duration(secs) * time.Second
		return lifetime - time.Duration(float64(lifetime)*renewAtRemainingFraction)
	}

	for _, tc := range []struct {
		name string
		secs int64
		want time.Duration
		why  string
	}{
		{
			name: "the station has never answered",
			secs: 0,
			want: twoThirds(credentialLifetimeDefaultSecs),
			why: "zero means 'no answer yet', not 'zero seconds'. Every machine is in " +
				"this state on its first poll after an upgrade, and stays in it for as " +
				"long as the policy endpoint is unreachable",
		},
		{
			name: "the owner's three-day test value",
			secs: 3 * 86400,
			want: 2 * testDay,
			why: "the value the owner said he would set to watch a renewal happen " +
				"without waiting a month: anything installed more than two days ago " +
				"renews on its next poll",
		},
		{
			name: "thirty days, what this used to ship",
			secs: 30 * 86400,
			want: 20 * testDay,
			why:  "unchanged from the expiry rule — two thirds of thirty days is twenty",
		},
		{
			name: "exactly the floor",
			secs: credentialLifetimeFloorSecs,
			want: twoThirds(credentialLifetimeFloorSecs),
			why:  "the floor itself is legal; the refusal starts below it",
		},
		{
			name: "one second under the floor",
			secs: credentialLifetimeFloorSecs - 1,
			want: twoThirds(credentialLifetimeDefaultSecs),
			why: "an out-of-range answer is DISCARDED, not clamped. It is not evidence " +
				"about what the owner wants, and the default is the value known to be safe",
		},
		{
			name: "one second over the ceiling",
			secs: credentialLifetimeCeilingSecs + 1,
			want: twoThirds(credentialLifetimeDefaultSecs),
			why:  "same rule at the other end",
		},
		{
			name: "a negative lifetime",
			secs: -1,
			want: twoThirds(credentialLifetimeDefaultSecs),
			why: "the shape a mangled response or a hand-edited setting produces. It " +
				"must never become a negative threshold, which every credential clears",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := credentialRenewAfter(tc.secs, noStagger); got != tc.want {
				t.Errorf("credentialRenewAfter(%d) = %s, want %s — %s",
					tc.secs, got, tc.want, tc.why)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ③ the stagger
// ---------------------------------------------------------------------------

// TestCredentialRenewJitter_SpreadsTheFleetAndNeverMoves is the owner-facing
// promise: he was told that lowering the lifetime would make every machine renew
// at once and that a stagger would be added. This is that stagger, measured.
func TestCredentialRenewJitter_SpreadsTheFleetAndNeverMoves(t *testing.T) {
	const base = 20 * testDay // the shipped threshold; base/8 is far above the window

	// A fleet of plausible machine ids. Real ones are `m-` + 12 hex chars.
	ids := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		ids = append(ids, string(rune('a'+i%26))+"m-machine-"+time.Duration(i).String())
	}

	buckets := map[int]int{}
	for _, id := range ids {
		j := credentialRenewJitter(id, base)
		if j < 0 || j >= credentialRenewJitterWindow {
			t.Fatalf("jitter for %q was %s, outside [0, %s) — a stagger that can exceed "+
				"its window eats into the retry period that keeps a switched-off machine "+
				"from losing its credential", id, j, credentialRenewJitterWindow)
		}
		// The poll is 15 minutes, so what matters is how many machines land in one
		// poll bucket, not how many distinct values there are.
		buckets[int(j/(15*time.Minute))]++
	}
	if len(buckets) < 4 {
		t.Errorf("64 machines landed in only %d of the 4 poll-length buckets across the "+
			"jitter window — a stagger that concentrates the fleet is not a stagger. "+
			"buckets=%v", len(buckets), buckets)
	}
	for b, n := range buckets {
		if n > len(ids)/2 {
			t.Errorf("bucket %d holds %d of %d machines — more than half the fleet would "+
				"still renew inside one poll interval", b, n, len(ids))
		}
	}

	// STABILITY. A fresh value per call does not stagger anything: it re-rolls the
	// threshold on every poll, so a machine near the boundary flickers and the
	// spread is luck rather than design.
	for _, id := range ids[:8] {
		first := credentialRenewJitter(id, base)
		for i := 0; i < 5; i++ {
			if again := credentialRenewJitter(id, base); again != first {
				t.Fatalf("jitter for %q moved between calls (%s then %s) — the offset must "+
					"be a property of the machine, not of the moment it was asked",
					id, first, again)
			}
		}
	}

	// An unconfigured warden has no fleet to spread and must not be pushed back.
	if j := credentialRenewJitter("", base); j != 0 {
		t.Errorf("an empty machine id got a %s stagger; there is no fleet to spread", j)
	}
}

// TestCredentialRenewJitter_IsBoundedByTheRetryWindowItEatsInto pins the cap that
// keeps the stagger from becoming the problem it solves. The stagger delays a
// renewal, and what it delays into is the retry window — the part of the lifetime
// that lets a machine that was switched off still get a new credential.
func TestCredentialRenewJitter_IsBoundedByTheRetryWindowItEatsInto(t *testing.T) {
	// A threshold so short that the flat window would be most of it.
	const tiny = 2 * time.Hour
	j := credentialRenewJitter(thisMachine, tiny)
	if j > tiny/8 {
		t.Errorf("jitter %s exceeds an eighth of a %s threshold — at short lifetimes the "+
			"stagger would consume the retry window it is supposed to sit inside", j, tiny)
	}
	// Control: at a 20-day threshold the cap is 2.5 days and does NOT bite, so the
	// assertion above is measuring the cap rather than a jitter that is always
	// tiny. The shipped threshold is longer still — two thirds of 90 days is 60,
	// whose cap is 7.5 days — so 20 days is the harsher control of the two.
	if credentialRenewJitter(thisMachine, 20*testDay) == 0 {
		t.Fatal("the control failed: this id gets no stagger even at the shipped " +
			"threshold, so the cap assertion above proves nothing")
	}
}

// ---------------------------------------------------------------------------
// ④ learning the lifetime from the station
// ---------------------------------------------------------------------------

func TestRefreshCredentialPolicy_AdoptsAnAnswerAndSurvivesEveryNonAnswer(t *testing.T) {
	const previous = int64(7 * 86400)

	for _, tc := range []struct {
		name   string
		status int
		body   []byte
		err    error
		want   int64
		why    string
	}{
		{
			name: "the station answers", status: http.StatusOK,
			body: []byte(`{"lifetime_secs":259200}`), want: 3 * 86400,
			why: "the owner lowering the setting has to reach the machine, and this is " +
				"the only path by which it does",
		},
		{
			name: "the station has not been upgraded yet", status: http.StatusNotFound,
			want: previous,
			why: "a 404 is the ORDINARY case during a rollout — every machine sees it " +
				"until the station lands. It must not disturb the number in hand",
		},
		{
			name: "a 404 that still carries a decodable body", status: http.StatusNotFound,
			body: []byte(`{"lifetime_secs":12345}`), want: previous,
			why: "the status is checked in its own right. The other non-200 arms all send " +
				"an empty body, so the unmarshal failure answers them first and a build " +
				"with no status check at all passes every one of them",
		},
		{
			name: "the station errors", status: http.StatusInternalServerError,
			want: previous,
			why:  "a station that cannot answer has said nothing about the lifetime",
		},
		{
			name: "the network is down", err: errors.New("dial tcp: no route to host"),
			want: previous,
			why:  "same — and this is the case that must not log, or it logs every poll",
		},
		{
			name: "200 with a renamed field", status: http.StatusOK,
			body: []byte(`{"credential_lifetime_secs":259200}`), want: previous,
			why: "🔴 THE FIELD IS A POINTER FOR THIS CASE. Decoded into a plain int64 a " +
				"renamed field lands as 0, which credentialRenewAfter reads as 'never " +
				"answered' — so a rename would silently revert the whole fleet to the " +
				"default while the endpoint kept answering 200",
		},
		{
			name: "200 with a zero lifetime", status: http.StatusOK,
			body: []byte(`{"lifetime_secs":0}`), want: previous,
			why: "zero is not a lifetime; adopting it would mean 'never answered' again",
		},
		{
			name: "200 with a negative lifetime", status: http.StatusOK,
			body: []byte(`{"lifetime_secs":-60}`), want: previous,
			why: "declined at the door rather than left for the threshold to sanitise",
		},
		{
			name: "200 that is not json", status: http.StatusOK,
			body: []byte(`<html>proxy error</html>`), want: previous,
			why: "a captive portal or a proxy answering 200 must not zero the policy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &updater{
				credLifetimeSecs: previous,
				get: func(path string) (int, []byte, error) {
					if path != credentialPolicyPath {
						t.Fatalf("asked %q, not the policy path", path)
					}
					return tc.status, tc.body, tc.err
				},
			}
			u.refreshCredentialPolicy()
			if u.credLifetimeSecs != tc.want {
				t.Errorf("credLifetimeSecs = %d, want %d — %s",
					u.credLifetimeSecs, tc.want, tc.why)
			}
		})
	}
}

// TestRefreshCredentialPolicy_SaysNothingAtAll. Every OTHER failure on this path
// is logged loudly because it means a renewal did not happen. This one means the
// threshold is merely unknown, which is not an incident — and a line per poll per
// machine for the length of a rollout is how a log file stops being read.
func TestRefreshCredentialPolicy_SaysNothingAtAll(t *testing.T) {
	var logs []string
	u := &updater{
		logf: func(format string, args ...any) { logs = append(logs, format) },
		get:  func(string) (int, []byte, error) { return http.StatusNotFound, nil, nil },
	}
	u.refreshCredentialPolicy()
	if len(logs) != 0 {
		t.Errorf("an unreachable policy endpoint logged %d line(s): %v", len(logs), logs)
	}
}

// ---------------------------------------------------------------------------
// ⑤ end to end through the acting half
// ---------------------------------------------------------------------------

// agedPermanentHarness wires a renewal whose RUNNING credential is the real
// field shape — permanent, no exp — issued `age` ago, against a station that
// publishes `lifetimeSecs`.
func agedPermanentHarness(t *testing.T, now time.Time, age time.Duration,
	lifetimeSecs int64, machineID string) *renewHarness {
	t.Helper()
	tokfile := filepath.Join(t.TempDir(), "warden.tok")
	fresh := permanentToken(t, thisMachine, now)
	h := newRenewHarness(t, permanentToken(t, thisMachine, now.Add(-age)), tokfile, now,
		http.StatusOK, map[string]any{"token": fresh}, nil)
	h.u.agentID = machineID
	h.u.get = func(path string) (int, []byte, error) {
		if path != credentialPolicyPath {
			return http.StatusNotFound, nil, nil
		}
		return http.StatusOK, []byte(`{"lifetime_secs":` + fmtI64(lifetimeSecs) + `}`), nil
	}
	return h
}

// TestMaybeRenewCredential_APermanentCredentialPastItsLifetimeIsFinallyRenewed is
// the whole ticket in one assertion. The SAME call, on the SAME credential shape,
// answered "not due" before this change — on every machine in the fleet, forever.
func TestMaybeRenewCredential_APermanentCredentialPastItsLifetimeIsFinallyRenewed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	// The owner's three-day test setting, on a machine installed a week ago.
	h := agedPermanentHarness(t, now, 7*testDay, 3*86400, thisMachine)

	if !h.u.maybeRenewCredential() {
		t.Fatalf("a permanent credential seven days old, against a three-day lifetime, "+
			"was not renewed. This is the state every machine in the fleet is in, and "+
			"the reason the renewal path had never once been observed to run. logs=%v",
			h.logs)
	}
	if h.renewCalls != 1 {
		t.Errorf("renew was called %d times, want exactly 1", h.renewCalls)
	}
	if len(h.written) != 1 {
		t.Errorf("wrote %d files, want exactly the token file: %v", len(h.written), h.written)
	}
}

// TestMaybeRenewCredential_APermanentCredentialInsideItsLifetimeIsLeftAlone is
// the control the test above is worthless without: a version of this path that
// renewed every permanent credential would pass that one and put every machine
// on the mint endpoint once per poll for the rest of its life.
func TestMaybeRenewCredential_APermanentCredentialInsideItsLifetimeIsLeftAlone(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	// Installed one day ago against the shipped thirty-day lifetime: nowhere near.
	h := agedPermanentHarness(t, now, testDay, 30*86400, thisMachine)

	if h.u.maybeRenewCredential() {
		t.Fatalf("a one-day-old credential was renewed against a thirty-day lifetime. "+
			"logs=%v", h.logs)
	}
	if h.renewCalls != 0 {
		t.Errorf("renew was called %d times on a credential that was not due — that is "+
			"one mint per machine per poll, fleet-wide", h.renewCalls)
	}
	if len(h.written) != 0 {
		t.Errorf("something was written for a credential that was not due: %v", h.written)
	}
	if h.execs != 0 {
		t.Errorf("the process exec'd itself %d times for a credential that was not due", h.execs)
	}
}

// TestMaybeRenewCredential_EveryFailureOnTheAgeArmIsHarmless re-runs the file's
// central guarantee on the NEW trigger. The failure arms are already covered for
// the expiry arm (renewapply_tfc53_test.go), and covered for one arm is not
// covered: the age arm is what reaches this code in production, and a guard is
// only proven where a path actually reaches it.
func TestMaybeRenewCredential_EveryFailureOnTheAgeArmIsHarmless(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	original := permanentToken(t, thisMachine, now.Add(-7*testDay))

	for _, tc := range []struct {
		name   string
		break_ func(h *renewHarness)
		why    string
	}{
		{
			name: "the network is down",
			break_: func(h *renewHarness) {
				h.u.renew = func() (int, map[string]any, error) {
					h.renewCalls++
					return 0, nil, errors.New("dial tcp: no route to host")
				}
			},
			why: "an unreachable station must leave the machine holding a credential " +
				"that still works",
		},
		{
			name: "the station refuses",
			break_: func(h *renewHarness) {
				h.u.renew = func() (int, map[string]any, error) {
					h.renewCalls++
					return http.StatusForbidden, nil, nil
				}
			},
			why: "a 403 is an answer about this request, never a reason to discard a " +
				"working credential",
		},
		{
			name: "the station answers 200 with nothing usable",
			break_: func(h *renewHarness) {
				h.u.renew = func() (int, map[string]any, error) {
					h.renewCalls++
					return http.StatusOK, map[string]any{"token": ""}, nil
				}
			},
			why: "an empty token is the shape a server-side field transposition takes, " +
				"and it would arrive at every machine within one poll of the others",
		},
		{
			name:   "the station refuses the credential it just issued",
			break_: func(h *renewHarness) { h.verifyStatus = http.StatusUnauthorized },
			why: "the probe runs BEFORE the write precisely so this costs one read-only " +
				"GET and nothing else",
		},
		{
			name:   "writing the token file fails",
			break_: func(h *renewHarness) { h.writeErr = errors.New("no space left on device") },
			why: "the write is atomic, so a failure means the OLD credential is still " +
				"the one on disk and the running process still holds a working token",
		},
		{
			name:   "the token file is not writable",
			break_: func(h *renewHarness) { h.writeErr = os.ErrPermission },
			why: "a permissions mistake on the token file must not be able to strand a " +
				"host — it is the same class of accident as a full disk",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := agedPermanentHarness(t, now, 7*testDay, 3*86400, thisMachine)
			h.u.token = original
			tc.break_(h)

			if h.u.maybeRenewCredential() {
				t.Fatalf("a failed renewal asked for an exec — %s. logs=%v", tc.why, h.logs)
			}
			if h.u.token != original {
				t.Errorf("the in-memory credential changed under a failed renewal — %s", tc.why)
			}
			if len(h.written) != 0 {
				t.Errorf("a failed renewal wrote %v — %s", h.written, tc.why)
			}
			if h.execs != 0 {
				t.Errorf("a failed renewal exec'd the process — %s", tc.why)
			}
			if len(h.logs) == 0 {
				t.Errorf("a failed renewal said nothing. Renewal is unattended; a silent " +
					"refusal is indistinguishable from never having been due")
			}

			// CONTROL, per sub-case: the very same harness WITHOUT this breakage does
			// renew. Without it every arm above would pass on a maybeRenewCredential
			// that had simply stopped renewing anything.
			ok := agedPermanentHarness(t, now, 7*testDay, 3*86400, thisMachine)
			if !ok.u.maybeRenewCredential() {
				t.Fatalf("the control failed: an unbroken renewal did not renew either, so "+
					"this sub-case proves nothing. logs=%v", ok.logs)
			}
		})
	}
}

// TestMaybeRenewCredential_TheHerdIsRealWhenTheThresholdMovesUnderTheFleet pins the
// behaviour the jitter does NOT prevent, so that nobody reads the stagger as a
// solution to it. Ages here are REALISTIC (5..83 days), not hugging the boundary:
// the threshold has moved under all of them, so every machine is past
// threshold+offset and acts on its very next poll.
//
// 🔴 If this test ever goes red, somebody has changed the renewal path so that the
// fleet no longer moves together. That is a GOOD change, but it is a behaviour
// change with its own failure modes — delete this test deliberately, do not
// "fix" it.
func TestMaybeRenewCredential_TheHerdIsRealWhenTheThresholdMovesUnderTheFleet(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	const fleet = 40

	renewed := 0
	for i := 0; i < fleet; i++ {
		id := "m-" + fmtI64(int64(0xa10000+i*7919))
		age := time.Duration(5+(i*2)%79) * testDay // 5..83 days, all far past 2 days
		h := agedPermanentHarness(t, now, age, 3*86400, id)
		if h.u.maybeRenewCredential() {
			renewed++
		}
	}
	if renewed != fleet {
		t.Errorf("only %d of %d machines renewed on the same poll; this test exists to "+
			"record that ALL of them do once the threshold moves under the fleet. A "+
			"smaller number means the renewal path now staggers the herd — which is a "+
			"real behaviour change, not a passing test", renewed, fleet)
	}
}

// TestMaybeRenewCredential_TheStaggerSpreadsMachinesThatAgeIntoTheThreshold covers
// the case the jitter DOES help with, and only that case: ages sitting on the
// boundary, where an offset of up to an hour decides who acts on which poll.
//
// ⚠️ Its old name was ...LoweringTheLifetimeDoesNotMoveTheWholeFleetAtOnce, which
// was WIDER than what it guards: the fixture pins every machine at exactly the
// threshold, so it never exercised the lowering case its name promised. The
// companion test above is the one that covers that.
func TestMaybeRenewCredential_TheStaggerSpreadsMachinesThatAgeIntoTheThreshold(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	const fleet = 40

	renewed := 0
	for i := 0; i < fleet; i++ {
		id := "m-" + fmtI64(int64(0xa10000+i*7919))
		h := agedPermanentHarness(t, now, 2*testDay, 3*86400, id)
		if h.u.maybeRenewCredential() {
			renewed++
		}
	}
	if renewed == fleet {
		t.Errorf("all %d machines renewed on the same poll even though their ages sit "+
			"exactly ON the threshold — this is the one case the per-machine offset is "+
			"supposed to spread, so the offset is not being applied at all. (It does NOT "+
			"spread a fleet whose ages are already far past the threshold; that case is "+
			"pinned by TestMaybeRenewCredential_TheHerdIsRealWhenTheThresholdMovesUnderTheFleet.)", fleet)
	}

	// CONTROL: the stagger DELAYS, it does not cancel. An hour later — past the
	// whole jitter window — every one of them must have renewed, or what looks
	// like a stagger is really a fleet that never renews.
	later := now.Add(credentialRenewJitterWindow + time.Minute)
	renewedLater := 0
	for i := 0; i < fleet; i++ {
		id := "m-" + fmtI64(int64(0xa10000+i*7919))
		h := agedPermanentHarness(t, later, 2*testDay+credentialRenewJitterWindow+time.Minute,
			3*86400, id)
		if h.u.maybeRenewCredential() {
			renewedLater++
		}
	}
	if renewedLater != fleet {
		t.Errorf("only %d of %d machines had renewed a full jitter window past the "+
			"threshold. The stagger must spread the fleet, never exempt part of it — a "+
			"machine that is permanently not-due is a machine nobody is watching",
			renewedLater, fleet)
	}
}
