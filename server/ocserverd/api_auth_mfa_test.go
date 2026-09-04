package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// api_auth_mfa_test.go — the owner's second factor and the credential-attempt
// brake, asserted at the HANDLER seam (the wire shape callers actually see).
//
// 🔴 WHAT THESE TESTS EXIST TO CATCH. Before this change /api/login had no
// attempt limit at all and no second factor, so the two defects worth guarding
// against are both silent: a code that can be replayed inside its acceptance
// window (nothing errors — the same six digits simply work twice), and a
// throttle that counts refused attempts as failures (nothing errors — the owner
// is simply locked out by a stranger). Each has a named test below.

const mfaTestPassword = "correct-horse-battery"

// mfaAPI builds a real apiServer on a temp DB with the owner password set.
func mfaAPI(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "mfa-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	api := newAPIServer(dal, NewHub(), singleKeyring([]byte(interopSecret)), 3600, "../..")
	phc, err := hashPassword(mfaTestPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := dal.PutSetting(settingPasswordHash, phc); err != nil {
		t.Fatalf("store hash: %v", err)
	}
	api.passwordHash = phc
	// 🔴 Shrink the refusal floor. Production servers leave this zero and get
	// throttleFailureFloor (3s); a package whose tests each paid that per refusal
	// would take minutes. The tests that are ABOUT the floor set it back to 0
	// explicitly — see TestFailedLoginsAllCostTheSameWallClock.
	api.credentialFailureFloor = time.Millisecond
	return api
}

// callJSON invokes a handler directly with a JSON body.
func callJSON(h func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func loginBody(password, code string) string {
	if code == "" {
		return fmt.Sprintf(`{"password":%q}`, password)
	}
	return fmt.Sprintf(`{"password":%q,"code":%q}`, password, code)
}

// offerMFA turns the ship-dark feature flag on — the precondition for enrolling.
// Everything about VERIFICATION is deliberately independent of it, which is what
// TestMFAOfferedFlagNeverDisarmsALiveFactor exists to prove.
func offerMFA(t *testing.T, api *apiServer, on bool) {
	t.Helper()
	rec := callJSON(api.HandleMfaOfferApiAuthMfaOfferPost,
		fmt.Sprintf(`{"offered":%t}`, on))
	if rec.Code != http.StatusOK {
		t.Fatalf("offer(%t): %d %s", on, rec.Code, rec.Body.String())
	}
	if decodeBody[mfaStateDTO](t, rec).Offered != on {
		t.Fatalf("offer(%t) did not stick", on)
	}
}

// armMFA enrols and activates a factor, returning the active secret.
func armMFA(t *testing.T, api *apiServer) string {
	t.Helper()
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body.String())
	}
	state := decodeBody[mfaStateDTO](t, rec)
	if state.Enrolled {
		t.Fatal("enroll must NOT arm the factor by itself")
	}
	if state.Secret == nil || *state.Secret == "" {
		t.Fatal("enroll returned no secret")
	}
	secret := *state.Secret

	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decode enrolled secret: %v", err)
	}
	code := totpCodeAt(key, time.Now().Unix()/totpStepSecs)
	rec = callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, code))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	if !decodeBody[mfaStateDTO](t, rec).Enrolled {
		t.Fatal("activate did not report the factor armed")
	}
	return secret
}

// liveCode generates the code an authenticator would show right now.
func liveCode(t *testing.T, secret string) string {
	t.Helper()
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return totpCodeAt(key, time.Now().Unix()/totpStepSecs)
}

// nextCode is the code an authenticator will show on the NEXT 30-second tick.
//
// 🔴 LOGIN TESTS MUST USE THIS, NOT liveCode, and the reason is a real product
// behaviour rather than a test workaround: activation SPENDS the step it proved
// (the replay floor moves to it), precisely so the activation code cannot double
// as the first login. A real owner just waits for the next tick; a test cannot
// afford to sleep 30 seconds, and +1 is inside the accepted skew window, so this
// is the same credential the phone would show — one tick early.
func nextCode(t *testing.T, secret string) string {
	t.Helper()
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return totpCodeAt(key, time.Now().Unix()/totpStepSecs+1)
}

// ── the control: nothing changes for an install that never turns MFA on ──────

func TestLoginWithoutMFAStillWorksAndIgnoresACode(t *testing.T) {
	api := mfaAPI(t)

	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "")); rec.Code != http.StatusOK {
		t.Fatalf("plain login: %d %s", rec.Code, rec.Body.String())
	}
	// A client that sends a code to a server with no enrolment must not be
	// punished for it — the wire contract says the field is ignored.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "123456")); rec.Code != http.StatusOK {
		t.Fatalf("login with a stray code: %d %s", rec.Code, rec.Body.String())
	}
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody("wrong", "")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d, want 401", rec.Code)
	}
}

// ── the enrol ceremony ──────────────────────────────────────────────────────

func TestMFAEnrolThenActivateArmsTheFactor(t *testing.T) {
	api := mfaAPI(t)
	if api.authMFAEnrolled() {
		t.Fatal("a fresh install must not have a factor armed")
	}
	secret := armMFA(t, api)
	if !api.authMFAEnrolled() {
		t.Fatal("factor not armed after activate")
	}
	// The armed secret must survive a reload from the DB — otherwise MFA
	// silently switches itself off on the next restart.
	reloaded, err := loadAuthSettings(api.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if reloaded.totpSecret != secret {
		t.Errorf("reloaded secret = %q, want the activated one", reloaded.totpSecret)
	}
}

func TestMFAEnrollRefusedWhileAFactorIsActive(t *testing.T) {
	api := mfaAPI(t)
	armMFA(t, api)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	if rec.Code != http.StatusConflict {
		t.Errorf("enroll over an active factor = %d, want 409 (rotation must disarm first)", rec.Code)
	}
}

func TestMFAActivateWithoutPendingIsAConflict(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":"123456"}`, mfaTestPassword))
	if rec.Code != http.StatusConflict {
		t.Errorf("activate with nothing pending = %d, want 409", rec.Code)
	}
}

// TestMFAActivateWrongCodeKeepsThePendingSecret — a typo must not force a fresh
// QR scan, or owners learn to abandon the ceremony half-done.
func TestMFAActivateWrongCodeKeepsThePendingSecret(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	secret := *decodeBody[mfaStateDTO](t, rec).Secret

	if rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":"000000"}`, mfaTestPassword)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong activation code = %d, want 401", rec.Code)
	}
	if api.authMFAEnrolled() {
		t.Fatal("a wrong code armed the factor")
	}
	// The same pending secret must still activate.
	rec = callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, liveCode(t, secret)))
	if rec.Code != http.StatusOK {
		t.Fatalf("retry after a typo = %d %s", rec.Code, rec.Body.String())
	}
}

// ── login with the factor armed ─────────────────────────────────────────────

func TestLoginWithMFARequiresACorrectCode(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	for _, tc := range []struct{ name, password, code string }{
		{"no code", mfaTestPassword, ""},
		{"wrong code", mfaTestPassword, "000000"},
		{"right code, wrong password", "wrong", nextCode(t, secret)},
	} {
		rec := callJSON(api.HandleLoginApiLoginPost, loginBody(tc.password, tc.code))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", tc.name, rec.Code)
		}
	}

	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, nextCode(t, secret)))
	if rec.Code != http.StatusOK {
		t.Fatalf("password + live code = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if decodeBody[tokenDTO](t, rec).Token == "" {
		t.Error("no token minted on a successful two-factor login")
	}
}

// TestLoginRefusalDoesNotDiscloseWhichFactorFailed is a non-disclosure property,
// not a cosmetic one: a distinguishable refusal confirms a correct password to
// someone who has guessed only that half.
func TestLoginRefusalDoesNotDiscloseWhichFactorFailed(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	wrongPassword := callJSON(api.HandleLoginApiLoginPost, loginBody("nope", nextCode(t, secret)))
	wrongCode := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "000000"))

	if wrongPassword.Code != wrongCode.Code {
		t.Errorf("statuses differ: password %d vs code %d", wrongPassword.Code, wrongCode.Code)
	}
	if wrongPassword.Body.String() != wrongCode.Body.String() {
		t.Errorf("bodies differ — the refusal names which factor failed:\n password: %s\n code:     %s",
			wrongPassword.Body.String(), wrongCode.Body.String())
	}
}

// TestLoginRefusesAReplayedCode is THE replay guard. A TOTP code stays
// cryptographically valid for the whole acceptance window, so nothing but the
// persisted floor makes it single-use — and a regression here is completely
// silent.
func TestLoginRefusesAReplayedCode(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)
	code := nextCode(t, secret)

	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, code)); rec.Code != http.StatusOK {
		t.Fatalf("first use = %d %s", rec.Code, rec.Body.String())
	}
	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, code))
	if rec.Code == http.StatusOK {
		t.Fatal("the SAME code logged in twice — replay is open")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("replay = %d, want 401", rec.Code)
	}
}

// TestActivationCodeCannotBeReusedAsTheFirstLogin pins a deliberate
// consequence of activate spending the step it proved: the six digits that
// armed the factor are already burnt, so they cannot also open the first
// session. An owner never notices (activate does not log them out, and the next
// tick is 30 seconds away) — but a regression here would mean the activation
// code stays live in anyone's scrollback for the rest of its window.
func TestActivationCodeCannotBeReusedAsTheFirstLogin(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	secret := *decodeBody[mfaStateDTO](t, rec).Secret

	activationCode := liveCode(t, secret)
	if rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, activationCode)); rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, activationCode)); rec.Code == http.StatusOK {
		t.Fatal("the activation code also logged in — it was not spent")
	}
	// The next tick's code still works, so this is a spent STEP, not a broken secret.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, nextCode(t, secret))); rec.Code != http.StatusOK {
		t.Errorf("the next code was refused too = %d; the floor over-collected", rec.Code)
	}
}

// TestReplayFloorSurvivesAReload — the floor is durable, so a restart must not
// reopen the window on a code already spent.
func TestReplayFloorSurvivesAReload(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)
	code := nextCode(t, secret)
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, code)); rec.Code != http.StatusOK {
		t.Fatalf("first use: %d", rec.Code)
	}

	reloaded, err := loadAuthSettings(api.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := totpVerify(reloaded.totpSecret, code, time.Now().Unix(), reloaded.totpLastStep); ok {
		t.Fatal("a spent code verifies again after a reload — the floor is not durable")
	}
}

// ── disarming ───────────────────────────────────────────────────────────────

func TestMFADisableRequiresBothFactors(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	for _, tc := range []struct{ name, body string }{
		{"wrong password", fmt.Sprintf(`{"password":"nope","code":%q}`, nextCode(t, secret))},
		{"wrong code", fmt.Sprintf(`{"password":%q,"code":"000000"}`, mfaTestPassword)},
	} {
		rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost, tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", tc.name, rec.Code)
		}
		if !api.authMFAEnrolled() {
			t.Fatalf("%s: the factor was disarmed anyway", tc.name)
		}
	}

	rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid disable = %d %s", rec.Code, rec.Body.String())
	}
	if api.authMFAEnrolled() {
		t.Fatal("factor still armed after a valid disable")
	}
	// And login goes back to password-only.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "")); rec.Code != http.StatusOK {
		t.Errorf("password-only login after disable = %d", rec.Code)
	}
}

func TestMFADisableWithoutAnActiveFactorIsAConflict(t *testing.T) {
	api := mfaAPI(t)
	rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
		fmt.Sprintf(`{"password":%q,"code":"123456"}`, mfaTestPassword))
	if rec.Code != http.StatusConflict {
		t.Errorf("disable with nothing armed = %d, want 409", rec.Code)
	}
}

// TestMFADisableClearsEveryStoredKey — a leftover secret or floor would let a
// re-enrolment inherit state from the old one.
func TestMFADisableClearsEveryStoredKey(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)
	rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{settingTOTPSecret, settingTOTPPendingSecret, settingTOTPLastStep} {
		got, err := api.dal.GetSetting(key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if got != nil {
			t.Errorf("%s survived the disable: %q", key, *got)
		}
	}
}

// ── the credential-attempt brake, through the handlers ──────────────────────
//
// 🔴 THE TWO TESTS THAT OPEN THIS SECTION ARE THE FLOOR OF THE WHOLE DESIGN.
// /api/login refuses several materially different things and an attacker must
// not be able to tell which one they just hit. That takes two independent
// properties, and losing either one is a security regression rather than a
// cosmetic one:
//
//	MESSAGE  TestFailedLoginRefusalsAreByteIdentical
//	TIME     TestFailedLoginsAllCostTheSameWallClock
//
// The message half is the one that gets broken by good intentions: someone
// improving the login wall's copy adds 「驗證碼錯誤」 and hands away the fact
// that the password was right. The timing half is the one that gets broken by
// optimisation. Read both before touching either handler.

// loginSpreadTolerance is how far apart TestFailedLoginsAllCostTheSameWallClock
// lets the refusals land before it calls them distinguishable.
//
// 🔴 IT IS A MEASURED NUMBER, NOT A GUESSED ONE — and the measurement CONDITIONS
// are part of the number. Quoting the value without them is how a comment ends
// up claiming a range nobody measured.
//
//	WHAT WAS MEASURED, AND WHERE
//	  * this file's author: 12 runs on an idle multi-core Darwin box,
//	    `-count=6` then `-count=6 -race`. Spread 879µs – 2.35ms; every case
//	    landed between 3.0001s and 3.0026s against the 3s floor.
//	  * an independent reviewer: 44 runs across 5 environments. Agreed
//	    everywhere EXCEPT `GOMAXPROCS=1` + `-race`, which produced 1 false red
//	    in 14 runs at a spread of 119ms.
//
// 100ms is ~42x the worst spread seen on any configuration that this project
// actually runs. It is NOT 42x the worst seen anywhere: under GOMAXPROCS=1 with
// -race it is under the noise, and that pair would flake.
//
// ⚠️ THAT PAIR IS NOT ON CI, and this file's author re-checked it rather than
// taking it on report. Measured here, not quoted: every `runs-on` in
// `.github/workflows/ci.yml` is `macos-15`; a grep for `go test … -race` or a
// `-race` in GOFLAGS across `.github`, `Makefile`, `bin`, `conformance`,
// `server` and `cli` finds two hits and BOTH are prose (a comment in this file
// and cli/ocwarden/CUTOVER.md) — nothing that executes; `GOMAXPROCS` is set in
// zero places. Positive control for the same pattern shape: `go test … -count=`
// hits 17 lines.
//
// ⚠️ Do not repeat "the -race grep is a clean zero" — it is not, it is zero
// EXECUTING hits and two textual ones, and a bare `grep -- -race` returns a
// dozen substring matches on words like `freeze-race` and `sch-race`. Anyone
// who ADDS `-race` under a constrained GOMAXPROCS owns re-measuring this
// number.
//
// ⚠️ An earlier version of this comment said 100ms "keeps a loaded CI box from
// producing a false red". Nobody had measured a loaded CI box. The measurements
// above are what there is; the retry in the test is what covers the rest.
//
// ⚠️ IT WAS 1 SECOND, WHICH WAS ~425x THE OBSERVED NOISE — wide enough that any
// single-branch divergence under a second stayed silently green. An independent
// reviewer found that before this file's own author did.
//
// 🔑 WHAT THIS NUMBER GOVERNS — and the first draft of this comment got it
// WRONG, so read the correction rather than the intuition. It claimed "catches
// anything ≥100ms added to one branch". It does not, and it must not:
//
//   - Work added BELOW the floor is INVISIBLE, and that is the entire feature.
//     The floor is a deadline at start+3s, so an extra 120ms on one branch still
//     lands at 3.000s like every other. Measured: that mutant stays green at any
//     tolerance, and a test that reddened on it would be asserting against the
//     design.
//   - What the tolerance actually governs is divergence that pushes a branch
//     PAST the floor — work an attacker could still see because no deadline is
//     hiding it any more. Measured: one branch at floor+150ms is RED at 100ms
//     and GREEN at the 1s this used to be. That band is exactly what tightening
//     bought, and it is the whole of what it bought.
//
// Going tighter is not supported by the data above: at 50ms the margin over the
// worst observed spread drops to ~21x, which is where scheduler starvation on a
// busy runner starts being a plausible cause of a red.
const loginSpreadTolerance = 100 * time.Millisecond

// loginRefusal is one way POST /api/login can refuse on credential grounds.
// `build` puts a server into the state that refusal needs and returns it with
// the body that provokes it — some of these need DIFFERENT server state, not
// just a different body, which is why this is a constructor and not a string.
type loginRefusal struct {
	name  string
	build func(t *testing.T) (*apiServer, string)
}

// loginRefusalCases enumerates EVERY credential refusal /api/login can produce,
// in ONE place, so the two tests below cannot drift apart about what the set is.
//
// 🔴 THE LIST IS THE POINT, AND IT USED TO BE WRONG. An earlier version carried
// four entries whose prose claimed to cover "wrong password / wrong password
// with a right code / RIGHT password with a wrong code / right password with no
// code at all" — but the fourth was never in the table (it held three
// wrong-password variants instead), and the owner's own fourth case, a server
// with NO PASSWORD SET, was in neither the table nor the prose. Two of the
// shapes this whole ticket exists to make indistinguishable were going
// unchecked while a comment said they were checked, which is worse than an
// admitted gap.
//
// 🔑 THE NO-PASSWORD ROW IS THE SHARPEST OF THEM, and it is why this list is
// load-bearing rather than tidy. Every other row runs a real argon2id
// verification; that one SHORT-CIRCUITS on `hash == ""` and does no crypto at
// all, so without the floor it answers in microseconds and announces "this
// server has no owner password yet" to anyone who asks. It is the one case
// where the timing spread is falsifiable by deleting a single line.
func loginRefusalCases() []loginRefusal {
	// armed builds a server whose factor is live, then hands `body` the secret.
	armed := func(body func(t *testing.T, secret string) string) func(*testing.T) (*apiServer, string) {
		return func(t *testing.T) (*apiServer, string) {
			t.Helper()
			api := mfaAPI(t)
			secret := armMFA(t, api)
			return api, body(t, secret)
		}
	}
	return []loginRefusal{
		{"wrong password, no code", armed(func(*testing.T, string) string {
			return loginBody("not-the-password", "")
		})},
		{"wrong password, wrong code", armed(func(*testing.T, string) string {
			return loginBody("not-the-password", "000000")
		})},
		{"wrong password, LIVE code", armed(func(t *testing.T, secret string) string {
			return loginBody("not-the-password", liveCode(t, secret))
		})},
		{"RIGHT password, wrong code", armed(func(*testing.T, string) string {
			return loginBody(mfaTestPassword, "000000")
		})},
		{"RIGHT password, no code at all", armed(func(*testing.T, string) string {
			return loginBody(mfaTestPassword, "")
		})},
		{"no password is set on this server", func(t *testing.T) (*apiServer, string) {
			t.Helper()
			api := mfaAPI(t)
			// Back to the pre-claim state. NOT the same as `s.secret == nil`,
			// which is a server-config refusal with its own message and is
			// deliberately distinguishable (see
			// TestLoginUnconfiguredSecretIsNotACredentialFailure).
			if err := api.dal.DeleteSetting(settingPasswordHash); err != nil {
				t.Fatalf("clear hash: %v", err)
			}
			api.passwordHash = ""
			return api, loginBody(mfaTestPassword, "")
		}},
	}
}

// headerFingerprint renders every header a client can read, in a STABLE order.
//
// 🔴 SORTED, NOT map-ranged. Go randomises map iteration order deliberately, so
// concatenating `rec.Header()` as it comes out compares two shuffles of the same
// set and goes red at random. It happens to be stable today only because these
// responses carry exactly one header; the second one anybody adds would make
// this test flake intermittently, which is the failure mode that gets a real
// assertion deleted for being "flaky".
func headerFingerprint(h http.Header) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += k + "=" + strings.Join(h[k], ",") + ";"
	}
	return out
}

// TestFailedLoginRefusalsAreByteIdentical — the non-disclosure half. Status,
// body and every header a client can read must be the same for every entry in
// loginRefusalCases, or the refusal itself says which half of the credential
// was right.
func TestFailedLoginRefusalsAreByteIdentical(t *testing.T) {
	type answer struct {
		name   string
		code   int
		body   string
		header string
	}
	cases := loginRefusalCases()
	if len(cases) < 2 {
		t.Fatalf("only %d refusal case(s) — there is nothing to compare", len(cases))
	}
	var got []answer
	for _, tc := range cases {
		// A server PER CASE: the no-password row needs different state, and a
		// shared server would also let one case's attempt leak into the next.
		api, body := tc.build(t)
		rec := callJSON(api.HandleLoginApiLoginPost, body)
		got = append(got, answer{tc.name, rec.Code, rec.Body.String(), headerFingerprint(rec.Header())})
	}
	for _, a := range got {
		if a.code != http.StatusUnauthorized {
			t.Errorf("%s → %d, want 401", a.name, a.code)
		}
	}
	first := got[0]
	for _, a := range got[1:] {
		if a.body != first.body {
			t.Errorf("login discloses WHICH half of the credential failed:\n  %s: %s\n  %s: %s",
				first.name, first.body, a.name, a.body)
		}
		if a.header != first.header {
			t.Errorf("login discloses which half failed through a HEADER:\n  %s: %s\n  %s: %s",
				first.name, first.header, a.name, a.header)
		}
	}
}

// TestFailedLoginsAllCostTheSameWallClock — the other half, and the one the
// refusal floor exists for.
//
// 🔴 HOW THIS AVOIDS BEING FLAKY, because a timing assertion usually is. Two
// assertions, both far from the scheduler's noise:
//
//   - EVERY case costs at least the floor. This bound can only be violated by a
//     code change, never by a slow machine — a slow machine makes elapsed
//     LONGER. It is what catches a refusal that answers as soon as it knows.
//   - the SPREAD between them is under loginSpreadTolerance, MEASURED TWICE. See
//     that constant for the measurements the number comes from, and the retry
//     below for why only this half of the assertion is re-measured.
//
// It uses the PRODUCTION floor rather than a shrunken one on purpose: the
// property is that the floor dominates the work, and a floor smaller than the
// work would make the whole assertion vacuous. Every case runs concurrently
// against its OWN server, so the run costs one floor rather than N and no case
// can take an in-flight slot from another.
//
// ⚠️ WHAT THIS TEST DOES NOT CATCH, stated because an independent reviewer had
// to rediscover it: turning the deadline into `time.Sleep(floor)` leaves this
// GREEN. Under that mutant every case still costs floor + its own work, and the
// works are close enough together that the spread stays inside tolerance. The
// test that catches it is TestHoldFailureFloorIsADeadlineNotASleep, which
// measures the wait against a start instant it controls.
func TestFailedLoginsAllCostTheSameWallClock(t *testing.T) {
	type result struct {
		name    string
		elapsed time.Duration
		code    int
	}
	// measure runs every case once, concurrently, each against its OWN server.
	// Concurrent so the run costs one floor rather than N, and separate servers
	// so no case can take an in-flight slot from another.
	measure := func() []result {
		cases := loginRefusalCases()
		results := make([]result, len(cases))
		var wg sync.WaitGroup
		for i, tc := range cases {
			api, body := tc.build(t)
			api.credentialFailureFloor = 0 // the PRODUCTION floor — this test is about it
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				start := time.Now()
				rec := callJSON(api.HandleLoginApiLoginPost, body)
				results[i] = result{name, time.Since(start), rec.Code}
			}(i, tc.name)
		}
		wg.Wait()
		return results
	}
	spreadOf := func(rs []result) (time.Duration, time.Duration, time.Duration) {
		lo, hi := rs[0].elapsed, rs[0].elapsed
		for _, r := range rs {
			if r.elapsed < lo {
				lo = r.elapsed
			}
			if r.elapsed > hi {
				hi = r.elapsed
			}
		}
		return hi - lo, lo, hi
	}

	results := measure()
	for _, r := range results {
		if r.code != http.StatusUnauthorized {
			t.Fatalf("%s → %d, want 401 (the timing assertion below would be measuring "+
				"the wrong thing)", r.name, r.code)
		}
		// 🔴 THE LOWER BOUND IS NOT RETRIED, deliberately. A slow machine makes
		// elapsed LONGER, so this bound can only be violated by a code change —
		// there is no flake to absorb, and retrying it would just double the
		// time it takes to report a real regression.
		if r.elapsed < throttleFailureFloor {
			t.Errorf("%s was refused after %v, less than the %v floor — the handler is "+
				"answering as soon as it knows, so an attacker can time the difference "+
				"between these refusals", r.name, r.elapsed, throttleFailureFloor)
		}
	}

	// 🔑 THE SPREAD IS MEASURED AGAIN BEFORE IT IS BELIEVED. Unlike the bound
	// above, this one CAN be tripped by the scheduler rather than by the code:
	// an independent reviewer reproduced exactly that at GOMAXPROCS=1 under
	// -race (1 red in 14 runs, spread 119ms against a 100ms tolerance). A real
	// regression is systematic and survives a second measurement; a descheduled
	// goroutine does not. The retry costs 3 seconds and it costs them ONLY on a
	// run that was about to go red, so an ordinary green run is unchanged.
	spread, _, _ := spreadOf(results)
	if spread <= loginSpreadTolerance {
		return
	}
	t.Logf("spread %v exceeded the %v tolerance on the first measurement — "+
		"re-measuring once before failing", spread, loginSpreadTolerance)
	for _, r := range results {
		t.Logf("  first   %-36s %v", r.name, r.elapsed)
	}
	second := measure()
	spread2, _, _ := spreadOf(second)
	for _, r := range second {
		t.Logf("  second  %-36s %v", r.name, r.elapsed)
	}
	if spread2 <= loginSpreadTolerance {
		t.Logf("second measurement spread %v is inside tolerance — treating the first "+
			"as scheduler noise, not a regression", spread2)
		return
	}
	t.Errorf("the %d refusals differ by %v and then by %v on a re-measurement "+
		"(tolerance %v) — twice is not the scheduler. They must be indistinguishable "+
		"by time as well as by message", len(second), spread, spread2, loginSpreadTolerance)
}

// TestSuccessfulLoginSpendsNoFloor — the other side of the same coin, and the
// reason the floor is on the REFUSAL rather than on the route. An owner who
// knows their password must never wait: a floor on success would be a permanent
// three-second tax on the only person entitled to be here, and it would buy
// nothing (there is nothing left to guess once the answer is right).
func TestSuccessfulLoginSpendsNoFloor(t *testing.T) {
	api := mfaAPI(t)
	api.credentialFailureFloor = 0 // the production floor

	start := time.Now()
	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, ""))
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	if elapsed >= throttleFailureFloor {
		t.Errorf("a CORRECT password took %v, at or past the %v refusal floor — the "+
			"owner is paying the attacker's tax", elapsed, throttleFailureFloor)
	}
}

// TestLoginRefusesTheCorrectPasswordWhenThePoolIsFull — the in-flight cap must
// gate on the ATTEMPT, not on the answer. A gate that lets a correct password
// through is an oracle: it tells an attacker exactly when they have guessed
// right, whatever the refusal for everyone else says.
func TestLoginRefusesTheCorrectPasswordWhenThePoolIsFull(t *testing.T) {
	api := mfaAPI(t)
	occupyThrottleSlots(t, api)
	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, ""))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("correct password with the pool full = %d, want 429 (no oracle)", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without a Retry-After header")
	}
}

// TestSetPasswordTakesTheFrontDoorBrake — the first-run claim token is a
// 32-byte secret submitted by an UNAUTHENTICATED caller, i.e. the same class of
// guessing target as the password, so this seam carries the full front-door
// brake: the in-flight cap AND the refusal floor.
func TestSetPasswordTakesTheFrontDoorBrake(t *testing.T) {
	api := mfaAPI(t)
	// Reset to the pre-password state and plant a claim token.
	if err := api.dal.DeleteSetting(settingPasswordHash); err != nil {
		t.Fatalf("clear hash: %v", err)
	}
	api.passwordHash = ""
	if err := api.dal.PutSetting(settingClaimToken, "the-real-claim-token"); err != nil {
		t.Fatalf("plant claim token: %v", err)
	}
	body := `{"password":"long-enough-pw","claim_token":"guess"}`

	// The floor, at a shrunken but measurable value.
	api.credentialFailureFloor = 300 * time.Millisecond
	start := time.Now()
	rec := callJSON(api.HandleSetPasswordApiAuthSetPasswordPost, body)
	elapsed := time.Since(start)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("claim-token guess = %d, want 401", rec.Code)
	}
	if elapsed < api.credentialFailureFloor {
		t.Errorf("a wrong claim token was refused after %v, less than the %v floor — "+
			"the first-run seam has no brake on guessing", elapsed, api.credentialFailureFloor)
	}

	// And the cap.
	api.credentialFailureFloor = time.Millisecond
	occupyThrottleSlots(t, api)
	if rec := callJSON(api.HandleSetPasswordApiAuthSetPasswordPost, body); rec.Code != http.StatusTooManyRequests {
		t.Errorf("claim-token guess with the pool full = %d, want 429", rec.Code)
	}
}

// TestSetPasswordFloorDoesNotHoldTheSettingsLock — the refusal waits ~3s, and
// it must do that with settingsMu RELEASED. Sleeping under it would let an
// unauthenticated caller stall every settings read and write on the server for
// three seconds a request: a brake on guessing turned into a denial of service
// against everything else.
//
// Asserted by taking the lock from another goroutine WHILE the refusal is
// waiting out its floor. If the handler still held it, this would block for the
// rest of the floor.
func TestSetPasswordFloorDoesNotHoldTheSettingsLock(t *testing.T) {
	api := mfaAPI(t)
	if err := api.dal.DeleteSetting(settingPasswordHash); err != nil {
		t.Fatalf("clear hash: %v", err)
	}
	api.passwordHash = ""
	if err := api.dal.PutSetting(settingClaimToken, "the-real-claim-token"); err != nil {
		t.Fatalf("plant claim token: %v", err)
	}
	api.credentialFailureFloor = 2 * time.Second

	done := make(chan struct{})
	go func() {
		defer close(done)
		callJSON(api.HandleSetPasswordApiAuthSetPasswordPost,
			`{"password":"long-enough-pw","claim_token":"guess"}`)
	}()
	// Let the handler get past the token comparison and into the wait.
	time.Sleep(300 * time.Millisecond)

	locked := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		api.settingsMu.Lock()
		api.settingsMu.Unlock()
		locked <- time.Since(start)
	}()
	select {
	case took := <-locked:
		if took > time.Second {
			t.Errorf("acquiring settingsMu took %v while a refusal served its floor — "+
				"the handler is sleeping under the lock", took)
		}
	case <-time.After(time.Second):
		t.Error("settingsMu was still held a second into the refusal floor — an " +
			"unauthenticated caller can stall every settings operation on the server")
	}
	<-done
}

// TestChangePasswordShortNewPasswordStaysA422 — a malformed request is not a
// credential guess and must be answered on its own terms, before any password
// is read. The 422 must not become a 401 by falling through to the verify.
//
// ⚠️ This used to fill the in-flight pool first, to prove the shape check beat
// the BRAKE. There is no brake on this route any more (see
// TestOwnerGatedSeamsNeverConsultTheThrottle), so that setup would assert
// nothing; the ordering it guards is now shape-check before credential-read.
// The same contract is still live on set-password, where the brake remains.
func TestChangePasswordShortNewPasswordStaysA422(t *testing.T) {
	api := mfaAPI(t)
	rec := callJSON(api.HandleChangePasswordApiAuthChangePasswordPost,
		`{"current_password":"whatever","new_password":"short"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("short new_password = %d, want 422", rec.Code)
	}
}

// ── the pre-auth probe and the cockpit's read-only view ─────────────────────

func TestAuthStatusPublishesMFARequired(t *testing.T) {
	api := mfaAPI(t)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	api.HandleAuthStatusApiAuthStatusGet(rec, req)
	before := decodeBody[authStatusDTO](t, rec)
	if !before.PasswordSet {
		t.Error("password_set should be true")
	}
	if before.MFARequired {
		t.Error("mfa_required should be false before enrolment")
	}

	armMFA(t, api)

	rec = httptest.NewRecorder()
	api.HandleAuthStatusApiAuthStatusGet(rec, httptest.NewRequest("GET", "/api/auth/status", nil))
	if !decodeBody[authStatusDTO](t, rec).MFARequired {
		t.Error("mfa_required should be true once the factor is armed")
	}
	// The probe is PUBLIC and must never leak the secret itself.
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("auth status body mentions a secret: %s", rec.Body.String())
	}
}

// TestActiveSecretIsNeverEchoedBack — enroll is the ONE moment a secret crosses
// the wire. If activate or disable echoed it, a stolen owner token could read
// out an existing enrolment and clone the factor.
func TestActiveSecretIsNeverEchoedBack(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	secret := *decodeBody[mfaStateDTO](t, rec).Secret

	rec = callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, liveCode(t, secret)))
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("activate echoed the secret: %s", rec.Body.String())
	}
	state := decodeBody[mfaStateDTO](t, rec)
	if state.Secret != nil || state.OtpauthURI != nil {
		t.Error("activate must answer null for secret/otpauth_uri")
	}

	rec = callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("disable echoed the secret: %s", rec.Body.String())
	}
}

// TestCorruptStoredSecretIsABootError — booting with MFA silently OFF because a
// row got mangled is the one outcome an owner would never notice until it
// mattered.
func TestCorruptStoredSecretIsABootError(t *testing.T) {
	api := mfaAPI(t)
	if err := api.dal.PutSetting(settingTOTPSecret, "not-valid-base32-!!!"); err != nil {
		t.Fatalf("plant: %v", err)
	}
	if _, err := loadAuthSettings(api.dal, Config{}, func(string) {}); err == nil {
		t.Fatal("a corrupt TOTP secret booted cleanly — MFA would be silently off")
	}
}

// ── H1/H2/H3 and the atomicity invariant: the policy layer, under concurrency ──

// TestVerifyAndSpendTOTPIsAtomicUnderConcurrency is the test the change's most
// emphasised invariant went without.
//
// 🔴 WHY IT HAD TO BE WRITTEN, and why -race is not a substitute. Splitting
// verifyAndSpendTOTP into "RLock read the secret+floor / verify unlocked / Lock
// write the floor" — exactly the shape its own comments forbid — left the whole
// 75-test suite GREEN, and passes `go test -race` too, because the defect is a
// logic race on the floor VALUE, not a data race on memory. Nothing but a
// concurrent test can see it.
//
// The property: N goroutines presenting the SAME code must yield exactly ONE
// success. A code is single-use only because the floor advances inside the same
// critical section that verified it.
func TestVerifyAndSpendTOTPIsAtomicUnderConcurrency(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)
	code := nextCode(t, secret)
	now := time.Now().Unix()

	const goroutines = 32
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make([]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		done.Add(1)
		go func(idx int) {
			defer done.Done()
			start.Wait() // release them all at once, to actually contend
			ok, err := api.verifyAndSpendTOTP(code, now)
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
			}
			results[idx] = ok
		}(i)
	}
	start.Done()
	done.Wait()

	accepted := 0
	for _, ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d of %d concurrent uses of ONE code were accepted, want exactly 1 — "+
			"the verify-and-spend critical section is not atomic, so a code is replayable",
			accepted, goroutines)
	}
}

// TestMFAActivateRequiresThePassword — H3. A stolen owner token alone must not
// be able to ARM a factor: the thief would enrol a secret they control, and the
// real owner's password would then answer 401 with no way to disarm it (disable
// needs a live code) — a durable lockout from a transient theft.
func TestMFAActivateRequiresThePassword(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	secret := *decodeBody[mfaStateDTO](t, rec).Secret

	// A correct code with the WRONG password must not arm anything.
	rec = callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":"not-the-password","code":%q}`, liveCode(t, secret)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("activate with a wrong password = %d, want 401", rec.Code)
	}
	if api.authMFAEnrolled() {
		t.Fatal("a factor was armed WITHOUT the password — a stolen token can lock the owner out")
	}
	// Omitting it entirely is a 422, not a silent pass.
	if rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"code":%q}`, liveCode(t, secret))); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("activate without a password field = %d, want 422", rec.Code)
	}
	if api.authMFAEnrolled() {
		t.Fatal("a factor was armed with no password field at all")
	}
}

// TestMFAActivateRefusalIsIndistinguishable — same non-disclosure property the
// login and disable seams hold: naming which half failed confirms a correct
// password to someone holding only a session.
func TestMFAActivateRefusalIsIndistinguishable(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)
	rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
	secret := *decodeBody[mfaStateDTO](t, rec).Secret

	wrongPwd := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":"nope","code":%q}`, liveCode(t, secret)))
	wrongCode := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":"000000"}`, mfaTestPassword))

	if wrongPwd.Code != wrongCode.Code || wrongPwd.Body.String() != wrongCode.Body.String() {
		t.Errorf("activate discloses WHICH factor failed:\n password: %d %s\n code:     %d %s",
			wrongPwd.Code, wrongPwd.Body.String(), wrongCode.Code, wrongCode.Body.String())
	}
}

// ── the owner-gated seams take NO brake at all ──────────────────────────────
//
// 🔴 THIS SECTION USED TO ASSERT THE OPPOSITE, and the whole of it turned over
// on one owner ruling: 「只有登入需要 throttling」. Three tests that lived here
// are gone because what they pinned no longer exists, and saying which is the
// point — a reader who finds thin coverage here should see a decision, not an
// oversight:
//
//	M11  TestMFADisableTakesTheBrakeBeforeVerifying   pinned disable calls begin()
//	M12  TestMFAActivateTakesTheBrakeBeforeVerifying  pinned activate calls begin()
//	M13  TestMFADisableRejectionSpendsFromTheBudget   pinned a failure counter
//
// M13 went in the first round (§0 deleted the counter). M11 and M12 go now, for
// the same reason one step further: those handlers no longer call the throttle
// at all, so a test asserting that they do would be asserting against the
// ruling. Two more went with them — TestMFAActivateConflictsAreNotThrottled and
// TestMFADisableConflictIsNotThrottled — which pinned that a 409 beats the
// brake on a route that now has no brake for it to beat.
//
// What replaces all five is ONE test of the new property, below. It is stronger
// than what it replaces: the four removed tests asserted an ordering, this one
// asserts that a full pool changes NOTHING about these three routes — including
// that they still SUCCEED, which no ordering test ever checked.

// TestOwnerGatedSeamsNeverConsultTheThrottle — the positive form of the owner's
// ruling, and the guard against someone re-adding a cap here because it looks
// prudent.
//
// 🔑 THE POOL IS SHARED WITH /api/login, AND THAT IS WHY THIS MATTERS BEYOND
// TIDINESS. While these handlers took a slot, a token holder hammering
// change-password could fill all four and make the OWNER's login answer 429 —
// an already-authenticated caller degrading the front door for everyone. With
// the pool held full for the whole test, every one of these three must behave
// exactly as it would on an idle server.
func TestOwnerGatedSeamsNeverConsultTheThrottle(t *testing.T) {
	t.Run("change-password succeeds with the pool full", func(t *testing.T) {
		api := mfaAPI(t)
		occupyThrottleSlots(t, api)
		rec := callJSON(api.HandleChangePasswordApiAuthChangePasswordPost,
			fmt.Sprintf(`{"current_password":%q,"new_password":"a-long-enough-new-one"}`, mfaTestPassword))
		if rec.Code != http.StatusOK {
			t.Fatalf("change-password with the pool full = %d %s, want 200 — this seam "+
				"must not consult loginThrottle at all", rec.Code, rec.Body.String())
		}
	})

	t.Run("change-password still refuses a wrong password with 401", func(t *testing.T) {
		api := mfaAPI(t)
		api.credentialFailureFloor = 3 * time.Second // production; must go unspent
		occupyThrottleSlots(t, api)
		start := time.Now()
		rec := callJSON(api.HandleChangePasswordApiAuthChangePasswordPost,
			`{"current_password":"guess","new_password":"a-long-enough-new-one"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong current password with the pool full = %d, want 401 (never 429)", rec.Code)
		}
		if elapsed := time.Since(start); elapsed >= api.credentialFailureFloor {
			t.Errorf("a refused change-password took %v — this seam must serve no "+
				"refusal floor either", elapsed)
		}
	})

	t.Run("mfa/activate arms the factor with the pool full", func(t *testing.T) {
		api := mfaAPI(t)
		offerMFA(t, api, true)
		rec := callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("enroll: %d %s", rec.Code, rec.Body.String())
		}
		secret := *decodeBody[mfaStateDTO](t, rec).Secret
		occupyThrottleSlots(t, api)
		rec = callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
			fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, liveCode(t, secret)))
		if rec.Code != http.StatusOK {
			t.Fatalf("activate with the pool full = %d %s, want 200", rec.Code, rec.Body.String())
		}
		if !api.authMFAEnrolled() {
			t.Error("activate answered 200 but armed nothing")
		}
	})

	t.Run("mfa/disable disarms the factor with the pool full", func(t *testing.T) {
		api := mfaAPI(t)
		secret := armMFA(t, api)
		occupyThrottleSlots(t, api)
		// nextCode, not liveCode: activation SPENT the current step, so a
		// liveCode would be refused by the replay floor for an unrelated reason.
		rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
			fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
		if rec.Code != http.StatusOK {
			t.Fatalf("disable with the pool full = %d %s, want 200", rec.Code, rec.Body.String())
		}
		if api.authMFAEnrolled() {
			t.Error("disable answered 200 but the factor is still armed")
		}
	})

	t.Run("the documented 409s survive a full pool", func(t *testing.T) {
		api := mfaAPI(t)
		offerMFA(t, api, true)
		occupyThrottleSlots(t, api)
		if rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
			fmt.Sprintf(`{"password":%q,"code":"123456"}`, mfaTestPassword)); rec.Code != http.StatusConflict {
			t.Errorf("activate with nothing pending = %d, want 409", rec.Code)
		}
		if rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
			fmt.Sprintf(`{"password":%q,"code":"123456"}`, mfaTestPassword)); rec.Code != http.StatusConflict {
			t.Errorf("disable with nothing armed = %d, want 409", rec.Code)
		}
	})
}

// occupyThrottleSlots fills the in-flight pool and keeps it full for the rest of
// the test. It is the ONLY way any handler can answer 429 — there is no failure
// ramp left to climb — and it needs no clock and no goroutine.
//
// 🔑 IT IS NOW USED IN BOTH DIRECTIONS. On the two PUBLIC seams it is the setup
// that provokes a 429; on the three owner-gated ones it is the setup that must
// change NOTHING. Read the call site before assuming which.
//
// The releases are dropped on purpose: nothing else shares this apiServer.
func occupyThrottleSlots(t *testing.T, api *apiServer) {
	t.Helper()
	for i := 0; i < throttleMaxInFlight; i++ {
		if _, _, blocked := api.loginThrottle.begin(); blocked {
			t.Fatalf("could not take in-flight slot %d of %d", i+1, throttleMaxInFlight)
		}
	}
	if _, _, blocked := api.loginThrottle.begin(); !blocked {
		t.Fatal("the pool admitted more than throttleMaxInFlight — the setup is not throttled")
	}
}

// TestLoginUnconfiguredSecretIsNotACredentialFailure — a missing signing secret
// is server CONFIG, not a credential fact. It must be settled before any
// credential work: it must not take an in-flight slot, must not burn a TOTP
// step, and must not pay the refusal floor (nobody guessed anything).
//
// ⚠️ It is therefore the one refusal on this route that IS distinguishable, by
// message and by speed — and that is correct rather than a leak, because it
// discloses a fact about the SERVER (auth is not configured) and none about the
// caller's password. GET /api/auth/status says as much to anyone who asks.
func TestLoginUnconfiguredSecretIsNotACredentialFailure(t *testing.T) {
	api := mfaAPI(t)
	api.credentialFailureFloor = 0 // the production floor — it must go unspent
	api.keys = singleKeyring(nil)

	start := time.Now()
	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, ""))
	elapsed := time.Since(start)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured login = %d, want 401", rec.Code)
	}
	if elapsed >= throttleFailureFloor {
		t.Errorf("an unconfigured-server refusal took %v, at or past the %v floor — "+
			"it is being treated as a credential failure", elapsed, throttleFailureFloor)
	}
	// It must also not have taken a slot on the way past.
	occupyThrottleSlots(t, api)
}

// TestMFAActivateFloorIsPersistedBeforeTheSecret pins the write ORDER, which is
// the substitute for a transaction this package does not have. Armed-with-no-floor
// is the one dangerous partial state: after a restart the floor loads as 0 and
// the activation code becomes replayable as a login.
func TestMFAActivateFloorIsPersistedBeforeTheSecret(t *testing.T) {
	api := mfaAPI(t)
	armMFA(t, api)

	floor, err := api.dal.GetSetting(settingTOTPLastStep)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor == nil || *floor == "" || *floor == "0" {
		t.Fatalf("activate left the replay floor at %v — the activation code is "+
			"replayable across a restart", floor)
	}
}

// TestMFAActivateRefusedWhenAFactorIsAlreadyActive — half the state machine the
// file header draws, and it was unpinned: removing the guard left the suite
// green. It is what stops an armed factor being replaced without proving the old
// one, which is the same property enroll's 409 protects from the other side.
func TestMFAActivateRefusedWhenAFactorIsAlreadyActive(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	// A perfectly valid code for the ACTIVE secret must still be refused: this
	// endpoint arms a PENDING enrolment, and there is none.
	rec := callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
	if rec.Code != http.StatusConflict {
		t.Errorf("activate while a factor is active = %d, want 409", rec.Code)
	}
	if !api.authMFAEnrolled() {
		t.Error("the existing factor was disturbed")
	}
}

// TestMFAActivateUpdatesMemoryOnlyAfterEveryDBWrite pins the DB-before-memory
// ordering. Inverting it (memory first) left the suite green, yet it is what
// keeps a partially-written activation from leaving the live snapshot claiming a
// factor the database does not have — MFA would appear ON until the next restart
// silently turned it OFF.
func TestMFAActivateUpdatesMemoryOnlyAfterEveryDBWrite(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	// Memory and DB must agree, in both directions.
	stored, err := api.dal.GetSetting(settingTOTPSecret)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if stored == nil || *stored != secret {
		t.Fatalf("DB secret = %v, memory = %q — they disagree", stored, secret)
	}
	inMemory, floor := func() (string, int64) {
		api.settingsMu.RLock()
		defer api.settingsMu.RUnlock()
		return api.totpSecret, api.totpLastStep
	}()
	if inMemory != *stored {
		t.Errorf("memory secret %q != DB secret %q", inMemory, *stored)
	}
	dbFloor, err := api.dal.GetSetting(settingTOTPLastStep)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if dbFloor == nil || *dbFloor != strconv.FormatInt(floor, 10) {
		t.Errorf("memory floor %d != DB floor %v", floor, dbFloor)
	}
	// The pending slot must be gone, so a re-enrolment cannot inherit it.
	pending, err := api.dal.GetSetting(settingTOTPPendingSecret)
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending != nil {
		t.Errorf("pending secret survived activation: %q", *pending)
	}
}

// ── the ship-dark feature flag ───────────────────────────────────────────────

// TestMFADefaultsToNotOffered — the whole point of the flag: an install that
// upgrades into this build must be completely unaffected until its owner opts
// in. Nothing about login changes, and the set-up path is closed.
func TestMFADefaultsToNotOffered(t *testing.T) {
	api := mfaAPI(t)

	if api.authMFAOffered() {
		t.Fatal("a fresh install must NOT offer the second factor")
	}
	reloaded, err := loadAuthSettings(api.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.mfaOffered {
		t.Error("an absent auth.mfa_offered row must load as false, not true")
	}
	// The set-up path is closed…
	for _, tc := range []struct {
		name string
		rec  *httptest.ResponseRecorder
	}{
		{"enroll", callJSON(api.HandleMfaEnrollApiAuthMfaEnrollPost, "")},
		{"activate", callJSON(api.HandleMfaActivateApiAuthMfaActivatePost,
			fmt.Sprintf(`{"password":%q,"code":"123456"}`, mfaTestPassword))},
	} {
		if tc.rec.Code != http.StatusForbidden {
			t.Errorf("%s while not offered = %d, want 403", tc.name, tc.rec.Code)
		}
	}
	// …and login is byte-for-byte what it was before this feature existed.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "")); rec.Code != http.StatusOK {
		t.Errorf("password-only login on a dark install = %d, want 200", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	api.HandleAuthStatusApiAuthStatusGet(rec, req)
	if decodeBody[authStatusDTO](t, rec).MFARequired {
		t.Error("a dark install must report mfa_required: false")
	}
}

// 🔴 TestMFAOfferedFlagNeverDisarmsALiveFactor is THE test for this flag.
//
// The flag is a rollout switch, not a security switch. If turning it off also
// turned verification off, it would BE the bypass: anyone holding a stolen owner
// token could withdraw the feature and walk straight past the second factor that
// exists to stop exactly that — undoing the both-factors rule on disable in one
// line. So: withdraw the feature from an ARMED install and assert that nothing
// about verification moves.
func TestMFAOfferedFlagNeverDisarmsALiveFactor(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)

	offerMFA(t, api, false) // withdraw the feature while a factor is armed

	if !api.authMFAEnrolled() {
		t.Fatal("withdrawing the feature disarmed the factor")
	}
	// The login wall must still be told to ask for a code — otherwise it hides
	// the field while the server still demands one, and the owner is locked out.
	rec := httptest.NewRecorder()
	api.HandleAuthStatusApiAuthStatusGet(rec, httptest.NewRequest("GET", "/api/auth/status", nil))
	if !decodeBody[authStatusDTO](t, rec).MFARequired {
		t.Error("mfa_required went false while a factor is still armed — the wall " +
			"would hide the code field and every login would fail with no way to see why")
	}
	// Password alone must still be refused.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("password-only login while the feature is withdrawn = %d, want 401 — "+
			"the flag became a bypass", rec.Code)
	}
	// …and the code still works.
	if rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, nextCode(t, secret))); rec.Code != http.StatusOK {
		t.Fatalf("two-factor login while the feature is withdrawn = %d, want 200", rec.Code)
	}
}

// TestMFADisableWorksWhileTheFeatureIsWithdrawn — the other half of the same
// rule. Taking the off-switch away alongside the on-switch would strand an owner
// with a factor they can no longer remove through the product.
func TestMFADisableWorksWhileTheFeatureIsWithdrawn(t *testing.T) {
	api := mfaAPI(t)
	secret := armMFA(t, api)
	offerMFA(t, api, false)

	rec := callJSON(api.HandleMfaDisableApiAuthMfaDisablePost,
		fmt.Sprintf(`{"password":%q,"code":%q}`, mfaTestPassword, nextCode(t, secret)))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable while the feature is withdrawn = %d %s", rec.Code, rec.Body.String())
	}
	if api.authMFAEnrolled() {
		t.Error("factor still armed after a valid disable")
	}
}

// TestMFAOfferSurvivesAReload — a rollout decision that forgets itself on
// restart would silently re-hide the feature (or re-expose it).
func TestMFAOfferSurvivesAReload(t *testing.T) {
	api := mfaAPI(t)
	offerMFA(t, api, true)

	reloaded, err := loadAuthSettings(api.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.mfaOffered {
		t.Fatal("the flag did not survive a reload")
	}
	offerMFA(t, api, false)
	reloaded, err = loadAuthSettings(api.dal, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.mfaOffered {
		t.Error("turning the flag back off did not survive a reload")
	}
}

// TestMFAStateReadsBothBits — the cockpit's one read, and it must never leak a
// secret (that happens exactly once, at enroll).
func TestMFAStateReadsBothBits(t *testing.T) {
	api := mfaAPI(t)
	get := func() mfaStateDTO {
		rec := httptest.NewRecorder()
		api.HandleMfaStateApiAuthMfaGet(rec, httptest.NewRequest("GET", "/api/auth/mfa", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/auth/mfa = %d", rec.Code)
		}
		return decodeBody[mfaStateDTO](t, rec)
	}
	if s := get(); s.Offered || s.Enrolled {
		t.Fatalf("fresh install = %+v, want both false", s)
	}
	secret := armMFA(t, api)
	s := get()
	if !s.Offered || !s.Enrolled {
		t.Errorf("after arming = %+v, want both true", s)
	}
	if s.Secret != nil || s.OtpauthURI != nil {
		t.Error("the state read echoed a secret — it is disclosed only by enroll")
	}
	if rec := httptest.NewRecorder(); true {
		api.HandleMfaStateApiAuthMfaGet(rec, httptest.NewRequest("GET", "/api/auth/mfa", nil))
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the ACTIVE secret leaked into the state read: %s", rec.Body.String())
		}
	}
}
