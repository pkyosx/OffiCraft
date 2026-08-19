package main

// renewapply_tfc53_test.go — the acting half of self-renewal.
//
// This code path ends in a syscall.Exec on every machine in the fleet, so the
// tests are shaped around the ONE outcome that cannot be undone: a machine whose
// old credential is gone and whose new one was never written is a machine nobody
// can reach again. Accordingly the FAILURE arms are the load-bearing ones — a
// refused, unreachable or empty renewal must leave the credential alone and the
// warden running — and each is paired with a control that would catch a version
// of this code that simply never renewed at all.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// renewHarness is one wired updater plus everything the test needs to say what it
// did: which paths it wrote, whether it exec'd, how many renewal requests it sent.
type renewHarness struct {
	u            *updater
	renewCalls   int
	verified     []string
	verifyStatus int
	verifyErr    error
	written      map[string]string
	writeErr     error
	execs        int
	logs         []string
}

// thisMachine is the `sub` both the running credential and its replacement must
// carry. It is NOT a default value: the renewal refuses a replacement whose sub
// does not match the one it is running as, so a fixture that left sub out would
// be rejected before any of the assertions below were reached.
const thisMachine = "m-7c1e4f0a92bd"

// dueToken is a credential far enough into its life to be due; freshToken is one
// that is not. Both carry iat, so the fraction rule (not the bare-expiry
// fallback) is what decides, and both carry sub, which is what identifies them
// as credentials for THIS machine.
func dueToken(t *testing.T, now time.Time) string {
	t.Helper()
	return jwtWith(t, map[string]any{
		"sub": thisMachine,
		"iat": now.Unix() - 29*86400,
		"exp": now.Unix() + 86400,
	})
}

func freshToken(t *testing.T, now time.Time) string {
	t.Helper()
	return jwtWith(t, map[string]any{
		"sub": thisMachine,
		"iat": now.Unix() - 86400,
		"exp": now.Unix() + 29*86400,
	})
}

// newRenewHarness wires an updater whose renewal answer is programmable. status/
// body/err are what the server seam returns; tokfile is where a successful
// renewal is expected to land.
func newRenewHarness(t *testing.T, token, tokfile string, now time.Time,
	status int, body map[string]any, transportErr error) *renewHarness {
	t.Helper()
	h := &renewHarness{written: map[string]string{}, verifyStatus: http.StatusOK}
	h.u = &updater{
		interval:     time.Millisecond,
		backoffStart: time.Millisecond,
		backoffCap:   time.Millisecond,
		sleep:        sleepUntil,
		ops:          stubOps{},
		get:          recordingGetter(nil, nil),
		exit:         func(int) {},
		execSelf:     func() error { h.execs++; return nil },
		now:          func() time.Time { return now },
		logf: func(format string, args ...any) {
			h.logs = append(h.logs, strings.TrimSpace(fmt.Sprintf(format, args...)))
		},
		token:       token,
		tokfilePath: tokfile,
		renew: func() (int, map[string]any, error) {
			h.renewCalls++
			return status, body, transportErr
		},
		verify: func(candidate string) (int, error) {
			h.verified = append(h.verified, candidate)
			return h.verifyStatus, h.verifyErr
		},
		writeTok: func(path, tok string) error {
			if h.writeErr != nil {
				return h.writeErr
			}
			h.written[path] = tok
			return nil
		},
	}
	return h
}

func (h *renewHarness) logged(sub string) bool {
	for _, l := range h.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ① the failures. A renewal that does not complete must cost NOTHING.
// ---------------------------------------------------------------------------

// TestMaybeRenewCredential_FailedRenewalKeepsTheOldCredential is the most
// important test in this file. Whatever the server does — refuses, is
// unreachable, or answers 200 with nothing usable — the machine must come out
// the other side holding exactly the credential it started with and still
// running. The alternative is a host that has to be visited in person.
func TestMaybeRenewCredential_FailedRenewalKeepsTheOldCredential(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

	cases := []struct {
		name   string
		status int
		body   map[string]any
		err    error
		expect string
	}{
		{
			name: "server refuses", status: http.StatusForbidden,
			expect: "status 403",
		},
		{
			name: "server unreachable", err: errors.New("connection refused"),
			expect: "connection refused",
		},
		{
			// The transport failed PART WAY: a status and a token-shaped field are
			// both present, and neither means the answer arrived intact. This is the
			// arm the transport-error guard carries on its own.
			name: "transport failed after a partial read", status: http.StatusOK,
			body: map[string]any{"token": "half-a-credential"},
			err:  errors.New("unexpected EOF"), expect: "unexpected EOF",
		},
		{
			// A body that LOOKS like an answer behind a status that says no. This is
			// the arm the status check actually carries: without it, a token-shaped
			// field in a gateway/error response is applied as a credential.
			name:   "refused, but the body still carries a token-shaped field",
			status: http.StatusBadGateway,
			body:   map[string]any{"token": "not-a-credential-the-server-minted"},
			expect: "status 502",
		},
		{
			name: "200 with no token field", status: http.StatusOK,
			body: map[string]any{"machine_id": "m-box"}, expect: "no token",
		},
		{
			name: "200 with an empty token", status: http.StatusOK,
			body: map[string]any{"token": "   "}, expect: "no token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := dueToken(t, now)
			h := newRenewHarness(t, old, tokfile, now, tc.status, tc.body, tc.err)

			if h.u.maybeRenewCredential() {
				t.Fatal("a renewal that did not produce a usable credential reported " +
					"success — the caller would now exec, and this machine would come " +
					"back holding whatever was left on disk")
			}
			if len(h.written) != 0 {
				t.Errorf("the token file was written despite the renewal failing: %v", h.written)
			}
			if h.u.token != old {
				t.Errorf("the in-process credential changed on a failed renewal")
			}
			if !h.logged(tc.expect) {
				t.Errorf("nothing in the log explains the refusal (want %q); got %v",
					tc.expect, h.logs)
			}
			// The control: the SAME wiring, answering properly, must renew — otherwise
			// every arm above passes on a function that never renews anything.
			ok := newRenewHarness(t, old, tokfile, now, http.StatusOK,
				map[string]any{"token": freshToken(t, now)}, nil)
			if !ok.u.maybeRenewCredential() {
				t.Fatal("control: a well-formed 200 did not renew, so the refusal " +
					"assertions above prove nothing")
			}
		})
	}
}

// TestMaybeRenewCredential_WriteFailureNeverExecs pins the ORDER. The exec is
// only ever allowed to happen after the new credential is on disk; a version that
// exec'd first (or exec'd regardless) would restart a warden into a token file
// that still holds the old credential at best, and nothing at worst.
func TestMaybeRenewCredential_WriteFailureNeverExecs(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")
	h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)
	h.writeErr = errors.New("read-only file system")

	if h.u.maybeRenewCredential() {
		t.Fatal("the write failed and renewal still reported success")
	}

	// Drive the real loop, not just the predicate: the claim is about what run()
	// does, and run() is what holds the exec.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	h.u.run(ctx)
	if h.execs != 0 {
		t.Errorf("run() exec'd %d time(s) after the credential write failed — the "+
			"process would have been replaced while the old credential was still the "+
			"only one on disk", h.execs)
	}
	if !h.logged("previous credential is untouched") {
		t.Errorf("the log does not say the old credential survived; got %v", h.logs)
	}
}

// TestMaybeRenewCredential_NotDueDoesNothing is the control for the whole file:
// a credential with no expiry (what warden credentials carry today) must never
// send a request, so the machinery above is not simply "renew every poll".
func TestMaybeRenewCredential_NotDueDoesNothing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no exp at all", jwtWith(t, map[string]any{"sub": "m-box"})},
		{"not a jwt", "not-a-token"},
		{"empty", ""},
		{"plenty of life left", freshToken(t, now)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRenewHarness(t, tc.token, tokfile, now, http.StatusOK,
				map[string]any{"token": freshToken(t, now)}, nil)
			if h.u.maybeRenewCredential() {
				t.Fatal("renewed a credential that is not due")
			}
			if h.renewCalls != 0 {
				t.Errorf("sent %d renewal request(s) for a credential that is not due — "+
					"fleet-wide that is one request per machine per poll, forever", h.renewCalls)
			}
			if len(h.written) != 0 {
				t.Errorf("wrote the token file for a credential that is not due: %v", h.written)
			}
		})
	}
	// Paired control: the same harness with a due credential does renew.
	h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)
	if !h.u.maybeRenewCredential() {
		t.Fatal("control: a due credential did not renew, so the never-due arms above " +
			"are satisfied by a function that never renews")
	}
}

// TestMaybeRenewCredential_ExplicitOCTokenBlocksRenewal covers the loop that
// would otherwise exec once per poll forever. execSelf carries os.Environ()
// across, so an explicitly-set OC_TOKEN outlives the exec and keeps overriding
// the token file — the new process would find the same stale credential due and
// exec again. Doing nothing is the only outcome that terminates.
func TestMaybeRenewCredential_ExplicitOCTokenBlocksRenewal(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")
	h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)
	h.u.envToken = h.u.token

	if h.u.maybeRenewCredential() {
		t.Fatal("renewed while OC_TOKEN was set in the environment — the exec that " +
			"follows would carry the old token straight back in, and the next poll " +
			"would find it due again: one exec per poll cycle, forever")
	}
	if h.renewCalls != 0 {
		t.Errorf("sent %d renewal request(s) that could not have taken effect", h.renewCalls)
	}
	if len(h.written) != 0 {
		t.Errorf("wrote a credential that the surviving OC_TOKEN would have overridden: %v", h.written)
	}
	if !h.logged("OC_TOKEN is set explicitly") {
		t.Errorf("the skip is silent — an operator has no way to learn why this "+
			"machine never renews; got %v", h.logs)
	}

	// The plist sets OC_WARDEN_TOKFILE and never OC_TOKEN, so the production path
	// is the empty-envToken one: it must still renew.
	h.u.envToken = "   "
	if !h.u.maybeRenewCredential() {
		t.Fatal("a blank OC_TOKEN blocked renewal — under launchd (which sets " +
			"OC_WARDEN_TOKFILE only) no machine would ever renew")
	}
}

// TestMaybeRenewCredential_UnresolvableTokfileDoesNotRenew: with no path to write
// to there is no safe way to apply a credential, so the request is not even sent.
// Falling back to a guessed path would put the credential somewhere readTokfile
// will never look.
func TestMaybeRenewCredential_UnresolvableTokfileDoesNotRenew(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h := newRenewHarness(t, dueToken(t, now), "", now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)
	if h.u.maybeRenewCredential() {
		t.Fatal("renewed with no resolvable token file path")
	}
	if h.renewCalls != 0 {
		t.Errorf("asked the server for a credential it had nowhere to put")
	}
	if !h.logged("token file path does not resolve") {
		t.Errorf("silent skip; got %v", h.logs)
	}
}

// ---------------------------------------------------------------------------
// ② the success path, and WHERE it runs from.
// ---------------------------------------------------------------------------

// TestRun_RenewsAndExecsInPlace is the happy path end to end through run():
// the fresh credential lands on the exact file the next boot reads, and only
// then is the process replaced.
func TestRun_RenewsAndExecsInPlace(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")
	h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now), "machine_id": "m-box"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(2 * time.Second); cancel() }()
	h.u.run(ctx)

	if h.written[tokfile] != freshToken(t, now) {
		t.Fatalf("the new credential did not land at %s; wrote %v", tokfile, h.written)
	}
	if h.execs != 1 {
		t.Errorf("run() exec'd %d times, want exactly 1", h.execs)
	}
}

// TestRun_RenewsEvenWhenTheStationHasNotShipped is the placement test. checkOnce
// opens with a server-sha gate that returns early when the station has not moved,
// so a credential check living inside it would only be reached on release days.
// Credentials expire on their own calendar; a fleet that renews only when someone
// lands a commit is the exact weakness this work exists to remove.
func TestRun_RenewsEvenWhenTheStationHasNotShipped(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")
	h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)

	// The station is standing still: /api/version answers the sha the updater has
	// already reconciled against, which is precisely what makes checkOnce bail.
	var fetched []string
	h.u.get = recordingGetter(map[string]getResult{
		versionPath: {status: 200, body: versionBody("sha-unchanged")},
	}, &fetched)
	h.u.lastSHA = "sha-unchanged"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(2 * time.Second); cancel() }()
	h.u.run(ctx)

	if h.written[tokfile] != freshToken(t, now) {
		t.Fatalf("the credential was never renewed while the station's sha was "+
			"unchanged — the check is sitting behind the self-update sha gate, so "+
			"these machines only renew when somebody ships a release; wrote %v", h.written)
	}
	for _, p := range fetched {
		if p == wardenBinaryPath || p == agentBinaryPath {
			t.Errorf("downloaded %s even though the sha had not moved", p)
		}
	}
}

// ---------------------------------------------------------------------------
// ③ the file it writes is the file the next process reads.
// ---------------------------------------------------------------------------

// TestTokfilePath_MatchesWhatReadTokfileReads: renewal writes through the same
// derivation readTokfile uses. If those two ever disagree the renewal is silent
// and total — a credential written where nothing looks for it, discovered when
// the old one expires.
func TestTokfilePath_MatchesWhatReadTokfileReads(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"canonical", map[string]string{"HOME": home},
			filepath.Join(home, ".officraft", "warden", "exec-warden.tok")},
		{"namespaced", map[string]string{"HOME": home, "OC_NAMESPACE": "beta"},
			tokfileFor(home, "beta")},
		{"explicit override wins", map[string]string{
			"HOME": home, "OC_WARDEN_TOKFILE": filepath.Join(home, "elsewhere.tok")},
			filepath.Join(home, "elsewhere.tok")},
		{"no home", map[string]string{}, ""},
		{"illegal namespace", map[string]string{"HOME": home, "OC_NAMESPACE": "Bad Name"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string { return tc.env[k] }
			got := tokfilePath(env)
			if got != tc.want {
				t.Fatalf("tokfilePath = %q, want %q", got, tc.want)
			}
			if got == "" {
				return
			}
			// And the reader really does read THAT file.
			if err := os.MkdirAll(filepath.Dir(got), 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(got, []byte("the.renewed.token"), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if tok := readTokfile(env, os.ReadFile); tok != "the.renewed.token" {
				t.Errorf("readTokfile read %q from a different file than renewal writes "+
					"(%s) — a renewal would be invisible to the next boot", tok, got)
			}
		})
	}
}

// TestOsTokfileWriter_WritesTheCredentialAtomicallyAt0600 exercises the write
// renewal actually uses — the SAME one `ocwarden install` uses — against a real
// filesystem: the credential must never be observable at loose perms, and a
// pre-existing wide-open file must not survive as one.
func TestOsTokfileWriter_WritesTheCredentialAtomicallyAt0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "warden")
	path := filepath.Join(dir, "exec-warden.tok")

	if err := osTokfileWriter().write(path, "fresh.token.value"); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != "fresh.token.value" {
		t.Errorf("wrote %q", body)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("credential is on disk at %o, not 0600", fi.Mode().Perm())
	}

	// Replacing a pre-existing 0644 file must leave 0600, not inherit the old perms.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := osTokfileWriter().write(path, "second.token.value"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	fi, _ = os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("a renewal over a 0644 tokfile left it at %o", fi.Mode().Perm())
	}
	// No temp file left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".exec-warden.tok.") {
			t.Errorf("left a temp credential behind: %s", e.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// ③ what the server sends back is not trusted just because it is non-empty.
// ---------------------------------------------------------------------------

// TestMaybeRenewCredential_RefusesACredentialThatIsNotForThisMachine covers the
// one failure in this file that CANNOT be retried out of: anything renamed over
// the token file replaces the only copy the host has, and the process then execs
// into it. If what landed is not a JWT, credentialDueForRenewal is false forever
// (no exp, not due), so the machine never tries again; nothing in the warden acts
// on a 401; and the previous credential is gone.
//
// The first arm is the shape that makes this a real risk rather than a
// hypothetical: the renewal DTO carries `token` beside two other strings, so a
// server-side transposition answers 200 with a machine id in the token field, compiles,
// and every machine in the fleet applies it within one poll of each other.
func TestMaybeRenewCredential_RefusesACredentialThatIsNotForThisMachine(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := dueToken(t, now)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

	otherMachine := jwtWith(t, map[string]any{
		"sub": "m-somebody-else",
		"iat": now.Unix() - 86400,
		"exp": now.Unix() + 29*86400,
	})
	noSub := jwtWith(t, map[string]any{
		"iat": now.Unix() - 86400,
		"exp": now.Unix() + 29*86400,
	})

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"a machine id where the token should be (field transposition)", "m-7c1e4f0a92bd"},
		{"a plain sentence — non-empty, and nothing else", "renewed ok"},
		{"a well-formed JWT belonging to another machine", otherMachine},
		{"a well-formed JWT carrying no subject at all", noSub},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRenewHarness(t, old, tokfile, now, http.StatusOK,
				map[string]any{"token": tc.token}, nil)
			if h.u.maybeRenewCredential() {
				t.Fatal("reported success on a replacement that is not this machine's " +
					"credential — the caller would exec, and this host would come back " +
					"unable to authenticate and unable to renew again")
			}
			if len(h.written) != 0 {
				t.Errorf("the token file was overwritten with %v", h.written)
			}
			if h.u.token != old {
				t.Errorf("the in-process credential changed")
			}
			if !h.logged("not a credential for this machine") {
				t.Errorf("nothing in the log names the reason; got %v", h.logs)
			}
		})
	}

	// The control. The same wiring with a credential that IS this machine's must
	// renew — otherwise every arm above is satisfied by a function that never
	// renews, which is exactly the shape this file keeps guarding against.
	ok := newRenewHarness(t, old, tokfile, now, http.StatusOK,
		map[string]any{"token": freshToken(t, now)}, nil)
	if !ok.u.maybeRenewCredential() {
		t.Fatal("control: a credential carrying this machine's own subject did not " +
			"renew, so the refusals above prove nothing")
	}
}

// TestMaybeRenewCredential_ReplacementAlreadyDueIsWrittenButNotExecd pins the
// runaway guard. A replacement that is due the moment it arrives would otherwise
// have this process exec, come back, find it due, renew, exec — once per poll
// forever. The credential is still written (it is valid, and the next real
// restart should pick it up); what is withheld is the exec.
func TestMaybeRenewCredential_ReplacementAlreadyDueIsWrittenButNotExecd(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := dueToken(t, now)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

	h := newRenewHarness(t, old, tokfile, now, http.StatusOK,
		map[string]any{"token": dueToken(t, now)}, nil)
	if h.u.maybeRenewCredential() {
		t.Fatal("asked the caller to exec into a credential that is ALREADY due — " +
			"that is one process replacement per poll cycle, indefinitely")
	}
	if h.written[tokfile] == "" {
		t.Error("the replacement was not written; it is valid and the next restart " +
			"should be able to pick it up")
	}
	if !h.logged("ALREADY due") {
		t.Errorf("nothing in the log explains why the exec was withheld; got %v", h.logs)
	}
}

// ---------------------------------------------------------------------------
// ④ the write either happens or it does not — nothing in between.
// ---------------------------------------------------------------------------

// TestTokfileWriter_AVerificationFailureLeavesTheDestinationUntouched is the
// assertion the renewal caller's log line depends on. It says "the previous
// credential is untouched and still in use" whenever write returns an error, so
// EVERY error path must leave the destination alone. The version that verified
// perms AFTER the rename broke that: those two steps returned an error with the
// destination already replaced, so the machine held a new credential on disk, an
// old one in memory, refused to exec, and renewed again every poll — the runaway
// this design exists to avoid, entering by a door nobody was watching.
func TestTokfileWriter_AVerificationFailureLeavesTheDestinationUntouched(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "exec-warden.tok")
	if err := os.WriteFile(dest, []byte("the-credential-this-host-is-running-on"), 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	renames := 0
	removed := []string{}
	w := osTokfileWriter()
	w.statMode = func(string) (os.FileMode, error) {
		return 0, fmt.Errorf("simulated stat failure")
	}
	realRename := w.rename
	w.rename = func(o, n string) error { renames++; return realRename(o, n) }
	realRemove := w.remove
	w.remove = func(p string) error { removed = append(removed, p); return realRemove(p) }

	err := w.write(dest, "the-replacement")
	if err == nil {
		t.Fatal("a failed verification reported success")
	}
	if renames != 0 {
		t.Errorf("the destination was renamed over despite the verification failing "+
			"(%d renames) — the caller's 'previous credential is untouched' is then a lie", renames)
	}
	body, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if string(body) != "the-credential-this-host-is-running-on" {
		t.Errorf("the destination changed on a failed write: %q", body)
	}
	if len(removed) != 1 {
		t.Errorf("the temp holding a live credential at 0600 was not cleaned up: %v", removed)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the directory should hold only the destination; got %d entries", len(entries))
	}
}

// ---------------------------------------------------------------------------
// ⑤ production hands the renewal the right values.
// ---------------------------------------------------------------------------

// TestNewRenewalWiring_ReadsTheRawEnvironmentAndTheRunningCredential exists
// because newSelfUpdater opens with refuseInTestBinary, so nothing in this
// package can construct it and every field it fills is otherwise unobserved.
// Measured before this split: `envToken: ""` and `renew: nil` both compiled and
// the whole suite stayed green — the OC_TOKEN guard and the entire feature could
// be switched off in production without one red test.
//
// ⚠️ This covers the VALUES only. The copy onto the updater is covered separately
// by TestApply_CarriesEveryFieldOntoTheUpdater below — an earlier version of this
// note said that copy lived on an untestable line inside newSelfUpdater, which
// had already stopped being true in the commit that wrote it.
func TestNewRenewalWiring_ReadsTheRawEnvironmentAndTheRunningCredential(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Base: "https://station.example", Token: "the-running-credential", ID: "m-box"}

	raw := map[string]string{"HOME": home}
	w := newRenewalWiring(cfg, func(k string) string { return raw[k] })

	if w.token != cfg.Token {
		t.Errorf("token = %q, want the credential this process runs on (%q)", w.token, cfg.Token)
	}
	if want := tokfileFor(home, ""); w.tokfilePath != want {
		t.Errorf("tokfilePath = %q, want the file readTokfile reads (%q)", w.tokfilePath, want)
	}
	if w.renew == nil {
		t.Error("renew is nil — renewal is wired off in production and no other test would notice")
	}
	if w.verify == nil {
		t.Error("verify is nil — the credential probe is skipped in production, and an " +
			"unusable credential would be written over the working one and exec'd into")
	}

	if w.writeTok == nil {
		t.Error("writeTok is nil — a due credential could never be written")
	}

	// The one that matters most: envToken must come from the RAW environment. Given
	// the tokfile-folded view instead, it reports the token FILE's contents as if
	// somebody had exported OC_TOKEN, which disables the infinite-exec guard on
	// precisely the machines it protects — every launchd warden, none of which sets
	// OC_TOKEN.
	if w.envToken != "" {
		t.Errorf("envToken = %q, want empty: nothing set OC_TOKEN in this environment", w.envToken)
	}
	tokfile := tokfileFor(home, "")
	if err := os.MkdirAll(filepath.Dir(tokfile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tokfile, []byte("what-the-token-file-holds"), 0o600); err != nil {
		t.Fatalf("seed tokfile: %v", err)
	}
	folded := tokfileEnv(func(k string) string { return raw[k] }, os.ReadFile)
	if got := folded("OC_TOKEN"); got != "what-the-token-file-holds" {
		t.Fatalf("control: the folded view should report the token file (%q) — without "+
			"that the assertion below cannot tell the two views apart", got)
	}
	if w2 := newRenewalWiring(cfg, func(k string) string { return raw[k] }); w2.envToken != "" {
		t.Errorf("envToken = %q with a token file present: the wiring is reading the "+
			"folded view, not the raw environment", w2.envToken)
	}

	explicit := map[string]string{"HOME": home, "OC_TOKEN": "exported-by-hand"}
	if w3 := newRenewalWiring(cfg, func(k string) string { return explicit[k] }); w3.envToken != "exported-by-hand" {
		t.Errorf("envToken = %q, want the value actually exported — the guard cannot "+
			"fire if this does not reach it", w3.envToken)
	}
}

// TestMaybeRenewCredential_AStationThatRefusesTheNewCredentialCostsNothing is
// the second half of "confirmed usable before the old one is retired". The
// subject check reads the replacement; this asks the only party that can settle
// it whether the replacement WORKS. A credential minted with the wrong secret, or
// with a scope the station refuses, parses fine and carries the right subject —
// and bricks the host exactly like a non-JWT would, because the station answers
// 401 to a warden with no code path for 401 and no way back to a credential it
// has already overwritten.
//
// The assertion that matters is the ORDER: nothing is written when the probe says
// no. Confirming after the write would satisfy the words and not the intent.
func TestMaybeRenewCredential_AStationThatRefusesTheNewCredentialCostsNothing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := dueToken(t, now)
	tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

	for _, tc := range []struct {
		name   string
		status int
		err    error
		expect string
	}{
		{"the station refuses it", http.StatusUnauthorized, nil, "REFUSED the credential it just issued"},
		{"the station cannot be reached to ask", 0, fmt.Errorf("dial tcp: connection refused"),
			"could not present the new credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRenewHarness(t, old, tokfile, now, http.StatusOK,
				map[string]any{"token": freshToken(t, now)}, nil)
			h.verifyStatus, h.verifyErr = tc.status, tc.err

			if h.u.maybeRenewCredential() {
				t.Fatal("reported success on a credential the station would not take")
			}
			if len(h.written) != 0 {
				t.Errorf("the token file was replaced before the credential was confirmed "+
					"usable: %v", h.written)
			}
			if !h.logged(tc.expect) {
				t.Errorf("nothing in the log names the reason (want %q); got %v", tc.expect, h.logs)
			}
		})
	}

	// Control + order. A station that accepts it renews, and what was presented is
	// the CANDIDATE — presenting the credential already in hand would pass against
	// any station and prove nothing.
	fresh := freshToken(t, now)
	ok := newRenewHarness(t, old, tokfile, now, http.StatusOK, map[string]any{"token": fresh}, nil)
	if !ok.u.maybeRenewCredential() {
		t.Fatal("control: a station that accepts the new credential did not renew")
	}
	if len(ok.verified) != 1 || ok.verified[0] != fresh {
		t.Errorf("the probe presented %v; it must present the CANDIDATE (%q), not the "+
			"credential this process already runs on", ok.verified, fresh[:12]+"…")
	}
}

// TestApply_CarriesEveryFieldOntoTheUpdater closes the gap the previous round
// left open and mis-described. That round said the uncovered line lived inside
// newSelfUpdater and would need a static check to reach — but the same commit had
// already moved it OUT, into this plain method, which any test can call. The
// excuse outlived the thing it excused. Measured: dropping envToken from apply
// left the whole suite green, i.e. the infinite-exec guard could be switched off
// in production without one red test.
func TestApply_CarriesEveryFieldOntoTheUpdater(t *testing.T) {
	w := renewalWiring{
		renew:       func() (int, map[string]any, error) { return 0, nil, nil },
		verify:      func(string) (int, error) { return 0, nil },
		writeTok:    func(string, string) error { return nil },
		tokfilePath: "/somewhere/exec-warden.tok",
		token:       "the-running-credential",
		envToken:    "exported-by-hand",
	}
	u := &updater{}
	u.apply(w)

	if u.renew == nil {
		t.Error("renew was not carried over — renewal is off and nothing else would notice")
	}
	if u.verify == nil {
		t.Error("verify was not carried over — the credential probe is skipped, and an " +
			"unusable credential would be written and exec'd into")
	}
	if u.writeTok == nil {
		t.Error("writeTok was not carried over — a due credential could never be written")
	}
	if u.tokfilePath != w.tokfilePath {
		t.Errorf("tokfilePath = %q, want %q", u.tokfilePath, w.tokfilePath)
	}
	if u.token != w.token {
		t.Errorf("token = %q, want %q", u.token, w.token)
	}
	if u.envToken != w.envToken {
		t.Errorf("envToken = %q, want %q — this is the infinite-exec guard's only input",
			u.envToken, w.envToken)
	}
}

// TestTokfileWriter_ARenameFailureAlsoCleansUpTheTemp covers the failure that is
// likeliest to actually happen — a cross-device rename, EPERM — and the one the
// previous round left uncovered: its cleanup could be deleted and the suite
// stayed green, while the branch's own comment explained why it mattered.
func TestTokfileWriter_ARenameFailureAlsoCleansUpTheTemp(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "exec-warden.tok")

	removed := []string{}
	w := osTokfileWriter()
	w.rename = func(string, string) error { return fmt.Errorf("simulated cross-device rename") }
	realRemove := w.remove
	w.remove = func(p string) error { removed = append(removed, p); return realRemove(p) }

	if err := w.write(dest, "the-replacement"); err == nil {
		t.Fatal("a failed rename reported success")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("the destination exists after a failed rename: %v", err)
	}
	if len(removed) != 1 {
		t.Errorf("the temp was not cleaned up: %v", removed)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a live credential at 0600 was left behind: %v — one per failed "+
			"attempt, in a directory nobody sweeps", names)
	}
}

// TestSysOpsTokfileWriter_CarriesTheRemoveSeam pins the projection. Without it
// the install path silently loses its cleanup: the seam is optional by design, so
// dropping it compiles, and the only symptom is temps accumulating on a machine
// nobody is looking at.
func TestSysOpsTokfileWriter_CarriesTheRemoveSeam(t *testing.T) {
	called := ""
	s := sysOps{remove: func(p string) error { called = p; return nil }}
	s.tokfileWriter().cleanup("/tmp/some-temp")
	if called != "/tmp/some-temp" {
		t.Errorf("the install seam's remove was not projected onto the writer (got %q)", called)
	}
}

// TestHttpCredentialVerifier_PresentsTheCandidateAtTheProbePath tests the
// PRODUCTION verifier, which until now nothing did: every other test in this file
// stubs `verify` out, so the closure that actually talks to the station was
// unobserved. Measured — replacing its body with `return http.StatusOK, nil`
// switched the whole probe off in production and the suite stayed green. The
// probe is this commit's entire point, so its own wiring being untested is the
// same shape the round before this one was about.
//
// Three things are asserted because all three are ways to have a probe that
// proves nothing: the wrong path (a public route answers 200 to anybody), the
// wrong token (presenting the credential already in hand passes against any
// station), and a status that does not come back verbatim.
func TestHttpCredentialVerifier_PresentsTheCandidateAtTheProbePath(t *testing.T) {
	var gotPath, gotAuth string
	answer := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(answer)
	}))
	defer srv.Close()

	verify := httpCredentialVerifier(&http.Client{}, srv.URL)

	status, err := verify("the-candidate-credential")
	if err != nil {
		t.Fatalf("probe returned a transport error against a live server: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want the station's own answer (200)", status)
	}
	// The path is written out LITERALLY rather than read from credentialProbePath.
	// Referencing the constant makes this assertion move with it, so pointing the
	// probe at a PUBLIC route — which answers 200 to anybody and proves nothing
	// about the credential — passes: measured, redirecting it to /api/version with
	// the constant edited stayed green. A literal cannot follow the code, so that
	// change has to appear in two places and one of them is a test.
	//
	// What matters about this path is that it is authenticated, read-only, and the
	// lowest rank a warden is entitled to. If it must change, change it here too and
	// say in the commit why the new one has those three properties.
	if gotPath != "/api/machines" {
		t.Errorf("probe hit %q, want /api/machines — an unauthenticated route would "+
			"answer 200 to anybody and prove nothing about the credential", gotPath)
	}
	if gotAuth != "Bearer the-candidate-credential" {
		t.Errorf("probe presented %q; it must present the CANDIDATE, not the credential "+
			"this process already runs on — the latter passes against any station", gotAuth)
	}

	// A refusal must come back as the station's own status, not be swallowed.
	answer = http.StatusUnauthorized
	if status, err := verify("a-credential-the-station-rejects"); err != nil || status != http.StatusUnauthorized {
		t.Errorf("a refused credential answered (%d, %v), want (401, nil) — a probe that "+
			"cannot report a refusal is decoration", status, err)
	}

	// An unreachable station is a transport error, NOT a status the caller could
	// mistake for a refusal.
	srv.Close()
	if status, err := verify("anything"); err == nil {
		t.Errorf("an unreachable station answered (%d, nil), want a transport error — "+
			"the caller treats those two differently on purpose", status)
	}
}

// TestMaybeRenewCredential_AServerErrorIsNotARefusal keeps the log honest: a
// station that failed to ANSWER is not a station saying no, and calling a 5xx a
// refusal sends whoever reads the log hunting a minting bug that does not exist.
// The ACTION is identical either way — keep the old credential — so only the log
// is at stake.
//
// 🔴 WHAT THIS SPLIT DOES *NOT* CATCH, corrected after review. An earlier version
// of this comment justified the split with the fail-closed roster read (authz.go:
// permanentCredentialRefusal returns true when the lookup errors, the opposite of
// the revocation gate beside it) and said one database blink therefore looks
// exactly like a refusal. The first half is true and the conclusion does not
// follow: server.go answers that refusal with writeError(w,
// http.StatusUnauthorized, "invalid token") — a DB blink arrives here as 401, not
// 5xx, so it lands in the REFUSED arm below and still reads as "the station
// refused the credential it just issued". The mis-attribution this split was
// argued from is the one case it cannot see. It is kept because a genuine 5xx is
// still worth telling apart; it is NOT evidence that the blink case is handled.
//
// The subtests cover the whole 5xx range, not just 500. 502 and 503 are what a
// proxy or load balancer in front of the station emits, i.e. the most likely 5xx
// in production, and `status == 500` compiles, passes a 500-only test, and puts
// them back in the refusal arm.
func TestMaybeRenewCredential_AServerErrorIsNotARefusal(t *testing.T) {
	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	} {
		t.Run(fmt.Sprintf("station answers %d", status), func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			tokfile := filepath.Join(t.TempDir(), "exec-warden.tok")

			h := newRenewHarness(t, dueToken(t, now), tokfile, now, http.StatusOK,
				map[string]any{"token": freshToken(t, now)}, nil)
			h.verifyStatus = status

			if h.u.maybeRenewCredential() {
				t.Fatal("renewed on a probe that never got an answer")
			}
			if len(h.written) != 0 {
				t.Errorf("the token file was replaced: %v", h.written)
			}
			if !h.logged("could not get an answer") {
				t.Errorf("the log does not say the answer was missing; got %v", h.logs)
			}
			if h.logged("REFUSED") {
				t.Errorf("the log calls a %d a refusal — that sends the reader hunting a "+
					"minting bug that does not exist; got %v", status, h.logs)
			}
		})
	}
}

// TestNewRenewalWiring_TheClosuresItBuildsReachTheRealEndpoints closes the hole
// the fourth review found: every assertion above this one checks the wiring's
// fields for `!= nil`, and `!= nil` is satisfied by a closure that does the wrong
// thing or nothing at all. Measured on the tree before this test existed, each of
// these compiled and left the whole package GREEN:
//
//	verify:   func(string) (int, error) { return http.StatusOK, nil }   // probe off
//	writeTok: func(string, string) error { return nil }                 // write faked
//	renew:    httpCredentialRenewer(client, cfg.Base, "")               // no credential
//	renew's path -> "/api/version"; renew's method -> GET                // renewal off
//
// The first two are the fleet-scale ones. A no-op `verify` deletes the entire
// confirm-before-write step, so an unusable credential replaces the working one
// and the machine is exec'd into it; a no-op `writeTok` reports success without
// touching disk, so the new process re-reads the SAME expired token, renews, execs
// — the infinite-exec trap this file names elsewhere, arriving on every machine at
// once because every warden runs this identical wiring on the same 15-minute beat.
//
// So this test does not inspect the fields: it CALLS them against an httptest
// station and asserts what actually went over the wire. Every path, method and
// header here is a LITERAL — asserting against renewCredentialPath or
// credentialProbePath would move with the code under test and pass while the
// probe was redirected to a public endpoint, which is exactly the shape this
// branch has already hit once.
func TestNewRenewalWiring_TheClosuresItBuildsReachTheRealEndpoints(t *testing.T) {
	type seen struct {
		method string
		path   string
		auth   string
	}
	var got []seen
	station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, seen{r.Method, r.URL.Path, r.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"a-fresh-credential"}`))
	}))
	defer station.Close()

	home := t.TempDir()
	cfg := Config{Base: station.URL, Token: "the-credential-this-process-runs-on", ID: "m-box"}
	w := newRenewalWiring(cfg, func(k string) string { return map[string]string{"HOME": home}[k] })

	status, body, err := w.renew()
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("renew status = %d, want 200", status)
	}
	if body["token"] != "a-fresh-credential" {
		t.Errorf("renew did not decode the station's body: %v", body)
	}

	if _, err := w.verify("the-candidate-not-the-running-one"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("want one renewal request and one probe, got %d: %+v", len(got), got)
	}

	if got[0].method != "POST" {
		t.Errorf("renew method = %s, want POST — a GET renews nothing and the station "+
			"answers it, so the fleet would go quiet without one red test", got[0].method)
	}
	if got[0].path != "/api/machines/renew-credential" {
		t.Errorf("renew path = %q, want the renewal endpoint literally", got[0].path)
	}
	if got[0].auth != "Bearer the-credential-this-process-runs-on" {
		t.Errorf("renew Authorization = %q — the renewal must present the credential "+
			"this process is running on; the endpoint names no target and acts on the "+
			"caller's own verified sub, so an empty or wrong one renews nothing", got[0].auth)
	}

	if got[1].method != "GET" {
		t.Errorf("probe method = %s, want GET — the probe must not be able to be "+
			"mistaken for something that writes", got[1].method)
	}
	if got[1].path != "/api/machines" {
		t.Errorf("probe path = %q, want /api/machines literally. A probe redirected to "+
			"a public endpoint answers 200 to anybody and proves nothing about the "+
			"candidate credential", got[1].path)
	}
	if got[1].auth != "Bearer the-candidate-not-the-running-one" {
		t.Errorf("probe Authorization = %q, want the CANDIDATE credential. Probing with "+
			"the credential this process already holds always succeeds and tells us "+
			"nothing about the one about to be written", got[1].auth)
	}

	dest := filepath.Join(home, "written-by-the-production-writer")
	if err := w.writeTok(dest, "a-fresh-credential"); err != nil {
		t.Fatalf("writeTok: %v", err)
	}
	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("the production writer reported success and wrote nothing: %v", err)
	}
	if string(written) != "a-fresh-credential" {
		t.Errorf("writeTok wrote %q", written)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credential written at %o, want 0600 — a writer that reports success "+
			"without the real one's mode is not the real one", perm)
	}
}
