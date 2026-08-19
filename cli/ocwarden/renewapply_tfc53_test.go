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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// renewHarness is one wired updater plus everything the test needs to say what it
// did: which paths it wrote, whether it exec'd, how many renewal requests it sent.
type renewHarness struct {
	u          *updater
	renewCalls int
	written    map[string]string
	writeErr   error
	execs      int
	logs       []string
}

// dueToken is a credential far enough into its life to be due; freshToken is one
// that is not. Both carry iat, so the fraction rule (not the bare-expiry
// fallback) is what decides.
func dueToken(t *testing.T, now time.Time) string {
	t.Helper()
	return jwtWith(t, map[string]any{
		"iat": now.Unix() - 29*86400,
		"exp": now.Unix() + 86400,
	})
}

func freshToken(t *testing.T, now time.Time) string {
	t.Helper()
	return jwtWith(t, map[string]any{
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
	h := &renewHarness{written: map[string]string{}}
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
				map[string]any{"token": "fresh.token.value"}, nil)
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
		map[string]any{"token": "fresh.token.value"}, nil)
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
				map[string]any{"token": "fresh.token.value"}, nil)
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
		map[string]any{"token": "fresh.token.value"}, nil)
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
		map[string]any{"token": "fresh.token.value"}, nil)
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
		map[string]any{"token": "fresh.token.value"}, nil)
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
		map[string]any{"token": "fresh.token.value", "machine_id": "m-box"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(2 * time.Second); cancel() }()
	h.u.run(ctx)

	if h.written[tokfile] != "fresh.token.value" {
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
		map[string]any{"token": "fresh.token.value"}, nil)

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

	if h.written[tokfile] != "fresh.token.value" {
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
