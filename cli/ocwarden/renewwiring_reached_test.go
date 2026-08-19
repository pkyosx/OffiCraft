package main

// renewwiring_reached_test.go — what the constructor production uses actually
// hands the renewal path, measured by USING it.
//
// 🔴 THIS FILE USED TO BE A go/ast CHECK, AND THAT WAS THE WRONG INSTRUMENT.
// It asserted that `u.apply(newRenewalWiring(cfg, env))` appeared in selfupdate.go
// and that main.go passed an identifier spelled `env`. Review walked through it
// four times without renaming anything:
//
//	newRenewalWiring(cfg, tokfileEnv(env, os.ReadFile))  // folded at the call site
//	if cfg.Token == "" { u.apply(…) }                    // a branch production never takes
//	u.apply(…); u.verify = nil                           // one line after it
//	env := tokfileEnv(env, os.ReadFile)                  // shadowed in main.go's block
//
// Each ends in a fleet that never renews, or one that writes an unconfirmed
// credential and execs into it — and a syntax check cannot see any of them,
// because none of them changes a name.
//
// 🔴 AND THE FIRST REPLACEMENT WAS STILL TOO WEAK: it called buildSelfUpdater and
// checked the fields were `!= nil`. Review then measured three edits that leave
// every one of them non-nil and the whole package green — verify replaced by a
// function that answers 200 to anything (confirm-before-write becomes a rubber
// stamp, the fleet writes credentials the station does not accept and execs into
// them), renew nil'd through a second statement, the exec seam replaced by a
// no-op. `!= nil` cannot tell "wired" from "wired to a lie". So the checks below
// CALL what the constructor handed over and look at what happens: a request
// arrives at a literal path with a literal credential, or a byte lands on disk.
//
// The endpoints are written out as literals on purpose. Asserting against
// credentialProbePath would move with the code under test, so a probe redirected
// to a public endpoint — which answers 200 to anybody and proves nothing about a
// candidate credential — would stay green.

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seenRequest is what the fake station recorded about one call.
type seenRequest struct {
	method string
	path   string
	auth   string
}

// stubStation answers every request with a renewal body and records what it was
// asked. It stands in for the real station for BOTH the renewal POST and the
// credential probe, which is what lets one server settle where each closure goes.
func stubStation(t *testing.T, got *[]seenRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = append(*got, seenRequest{r.Method, r.URL.Path, r.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"a-fresh-credential"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// exerciseRenewalSeams calls the three closures the constructor wired and asserts
// each one reached the thing it is supposed to reach. Shared by the buildSelfUpdater
// and newSelfUpdater cases so the production constructor is held to exactly the
// same standard as the testable one, not a weaker one.
func exerciseRenewalSeams(t *testing.T, u *updater, got *[]seenRequest, home, runningToken string) {
	t.Helper()

	if u.renew == nil || u.verify == nil || u.writeTok == nil {
		t.Fatalf("the constructor left renew/verify/writeTok as %v/%v/%v — a nil renew or "+
			"writeTok makes maybeRenewCredential return on its first line (renewal is not "+
			"in play, fleet-wide, without one log line), and a nil verify skips "+
			"confirm-before-write", u.renew == nil, u.verify == nil, u.writeTok == nil)
	}

	status, body, err := u.renew()
	if err != nil || status != http.StatusOK {
		t.Fatalf("renew() = (%d, %v); it must reach the station", status, err)
	}
	if body["token"] != "a-fresh-credential" {
		t.Errorf("renew() did not decode the station's body: %v", body)
	}
	if _, err := u.verify("the-candidate-not-the-running-one"); err != nil {
		t.Fatalf("verify() never reached the station: %v", err)
	}

	if len(*got) != 2 {
		t.Fatalf("want one renewal request and one probe, got %d: %+v", len(*got), *got)
	}
	renewReq, probeReq := (*got)[0], (*got)[1]

	if renewReq.method != "POST" || renewReq.path != "/api/machines/renew-credential" {
		t.Errorf("renew() sent %s %s, want POST /api/machines/renew-credential literally",
			renewReq.method, renewReq.path)
	}
	if renewReq.auth != "Bearer "+runningToken {
		t.Errorf("renew() presented %q, want the credential this process runs on. The "+
			"endpoint names no target and acts on the caller's own verified sub, so a "+
			"wrong or empty one renews nothing", renewReq.auth)
	}
	if probeReq.method != "GET" || probeReq.path != "/api/machines" {
		t.Errorf("verify() sent %s %s, want GET /api/machines literally. A probe pointed "+
			"anywhere public answers 200 to anybody, which turns confirm-before-write "+
			"into a rubber stamp that would let an unusable credential be written and "+
			"exec'd into", probeReq.method, probeReq.path)
	}
	if probeReq.auth != "Bearer the-candidate-not-the-running-one" {
		t.Errorf("verify() presented %q, want the CANDIDATE. Probing with the credential "+
			"this process already holds always succeeds and settles nothing about the one "+
			"about to overwrite it", probeReq.auth)
	}

	dest := filepath.Join(home, "written-by-the-wired-writer")
	if err := u.writeTok(dest, "a-fresh-credential"); err != nil {
		t.Fatalf("writeTok: %v", err)
	}
	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("writeTok reported success and wrote nothing (%v) — the renewal would "+
			"exec into a credential that is not there", err)
	}
	if string(written) != "a-fresh-credential" {
		t.Errorf("writeTok wrote %q", written)
	}
	if info, err := os.Stat(dest); err != nil {
		t.Fatalf("stat: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credential written at %o, want 0600 — a writer that reports success "+
			"without the real one's mode is not the real one", perm)
	}
}

// seedHome gives the constructor a HOME to resolve the token file under, plus a
// binary to call its own.
func seedHome(t *testing.T) (home, exe string) {
	t.Helper()
	home = t.TempDir()
	exe = filepath.Join(home, "bin", "ocwarden")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	return home, exe
}

// builtUpdater constructs through buildSelfUpdater with both host effects injected.
func builtUpdater(t *testing.T, home, exe string, cfg Config) *updater {
	t.Helper()
	raw := map[string]string{"HOME": home}
	return buildSelfUpdater(cfg, rawEnv{lookup: func(k string) string { return raw[k] }},
		func(string, ...any) {}, func() (string, error) { return exe, nil },
		func(string, []string, []string) error { return nil })
}

func TestBuildSelfUpdater_HandsRenewalEverythingItNeeds(t *testing.T) {
	var got []seenRequest
	station := stubStation(t, &got)
	home, exe := seedHome(t)
	cfg := Config{Base: station.URL, Token: "the-running-credential", ID: "m-box"}

	u := builtUpdater(t, home, exe, cfg)

	exerciseRenewalSeams(t, u, &got, home, cfg.Token)
	if u.token != cfg.Token {
		t.Errorf("token = %q, want the credential this process runs on (%q)", u.token, cfg.Token)
	}
	if want := tokfileFor(home, ""); u.tokfilePath != want {
		t.Errorf("tokfilePath = %q, want the file readTokfile reads (%q)", u.tokfilePath, want)
	}
}

// TestBuildSelfUpdater_ExecSelfReplacesThisBinary pins what the injected exec
// effect is asked to do. execSelf is called with no arguments, so everything about
// the exec — which binary, which argv — is decided inside the constructor, and
// exec'ing the wrong path is a machine that comes back as something else.
func TestBuildSelfUpdater_ExecSelfReplacesThisBinary(t *testing.T) {
	home, exe := seedHome(t)
	var gotPath string
	var gotArgv []string
	raw := map[string]string{"HOME": home}
	u := buildSelfUpdater(Config{Base: "https://station.example", Token: "tok", ID: "m-box"},
		rawEnv{lookup: func(k string) string { return raw[k] }}, func(string, ...any) {},
		func() (string, error) { return exe, nil },
		func(path string, argv, _ []string) error {
			gotPath, gotArgv = path, argv
			return errors.New("exec refused by the test seam")
		})

	if err := u.execSelf(); err == nil {
		t.Fatal("execSelf swallowed the exec seam's error; the callers decide what to do " +
			"about a failed exec and both need to be told it failed")
	}
	want := exe
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		want = resolved // t.TempDir() lives under a symlinked /var on darwin
	}
	if gotPath != want {
		t.Errorf("execSelf exec'd %q, want this warden's own binary %q", gotPath, want)
	}
	if len(gotArgv) == 0 || gotArgv[0] != os.Args[0] {
		t.Errorf("execSelf passed argv %v, want this process's own argv — the re-exec is "+
			"meant to be the same warden with the same arguments", gotArgv)
	}
}

// TestNewSelfUpdater_IsTheSameWiringProductionRunsOn is the one that matters: every
// other test in this file builds through buildSelfUpdater, and newSelfUpdater is
// the function main.go actually calls. It used to open with refuseInTestBinary,
// which made everything it does unobservable — and three separate edits inside it
// (renew nil'd, verify replaced by a rubber stamp, the exec seam replaced by a
// no-op) were measured passing the whole suite.
func TestNewSelfUpdater_IsTheSameWiringProductionRunsOn(t *testing.T) {
	var got []seenRequest
	station := stubStation(t, &got)
	home := t.TempDir()
	cfg := Config{Base: station.URL, Token: "the-running-credential", ID: "m-box"}

	u := newSelfUpdater(cfg, rawEnv{lookup: func(k string) string {
		return map[string]string{"HOME": home}[k]
	}}, func(string, ...any) {})

	exerciseRenewalSeams(t, u, &got, home, cfg.Token)
	if u.token != cfg.Token {
		t.Errorf("token = %q, want the credential this process runs on (%q)", u.token, cfg.Token)
	}
	if want := tokfileFor(home, ""); u.tokfilePath != want {
		t.Errorf("tokfilePath = %q, want the file readTokfile reads (%q)", u.tokfilePath, want)
	}
	// The binary it would replace is THIS process's binary, resolved the same way
	// resolveSelfExe does it — computed here from os.Executable rather than taken
	// from the updater, so a constructor handed a stubbed `executable` fails.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
		self = resolved
	}
	if u.selfPath != self {
		t.Errorf("selfPath = %q, want the running binary %q — self-update swaps and execs "+
			"whatever this says", u.selfPath, self)
	}
}

// TestNewSelfUpdater_ReadsTheRawEnvironmentNotTheTokfileView: given the folded
// view, envToken reports the token FILE's contents as if somebody had exported
// OC_TOKEN. That trips the infinite-exec guard on precisely the machines it
// protects — every launchd warden, none of which sets OC_TOKEN — and the whole
// fleet quietly stops renewing. Checked on the CONSTRUCTED updater because every
// way review defeated the old syntax check happened between the call site and
// newRenewalWiring: folded at the call site, shadowed in the caller's block, or
// reassigned before the call.
func TestNewSelfUpdater_ReadsTheRawEnvironmentNotTheTokfileView(t *testing.T) {
	home := t.TempDir()
	tokfile := tokfileFor(home, "")
	if err := os.MkdirAll(filepath.Dir(tokfile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tokfile, []byte("what-the-token-file-holds"), 0o600); err != nil {
		t.Fatalf("seed tokfile: %v", err)
	}
	raw := func(k string) string { return map[string]string{"HOME": home}[k] }
	// Control: the folded view really does report the file, so a green assertion
	// below means the raw view was used — not that both views happen to be empty.
	if got := tokfileEnv(raw, os.ReadFile)("OC_TOKEN"); got != "what-the-token-file-holds" {
		t.Fatalf("control: the folded view should report the token file, got %q", got)
	}

	u := newSelfUpdater(Config{Base: "https://station.example", Token: "tok", ID: "m-box"},
		rawEnv{lookup: raw}, func(string, ...any) {})
	if u.envToken != "" {
		t.Errorf("envToken = %q with a token file on disk and nothing exporting "+
			"OC_TOKEN. The constructor is reading the tokfile-folded view somewhere "+
			"between here and newRenewalWiring, which disables the infinite-exec "+
			"guard on every launchd warden and stops the fleet renewing", u.envToken)
	}
}

// execSeamEnv drives the three roles of the exec-seam test below: the parent
// (unset), the child that calls the seam, and the generation that only exists if
// the seam really replaced a process image.
const execSeamEnv = "OC_TFC53_EXEC_SEAM_ROLE"

// TestNewSelfUpdater_HandsOverTheExecSeamThatRefusesInATestBinary is the only
// assertion in this file that cannot be made in-process, because the thing being
// asserted is that calling it KILLS the process.
//
// What is at stake: newSelfUpdater's whole body is `buildSelfUpdater(cfg, env,
// logf, os.Executable, syscallExecImage)`. Replace that last argument with
// `func(string, []string, []string) error { return nil }` and every other test
// here still passes — the updater is fully wired, renewal still works — but the
// exec after a self-update swap silently does nothing, execInPlace reads the nil
// return as a failed exec and exits 0, and launchd does not relaunch an exited
// warden. Every machine in the fleet, on the next release.
//
// So the child calls u.execSelf() for real and the parent reads how it died:
//
//	exit 1 + the refusal on stderr → the real syscallExecImage was handed over
//	exit 4                        → execSelf returned, i.e. a stub, not the syscall
//	exit 3                        → the exec really happened (the refusal is gone)
//
// The exit-3 arm is why the child stamps its env before calling: syscall.Exec here
// would re-run this same test binary with this same argv, so without a generation
// marker a missing refusal would be an endless self-exec instead of one red test.
// If the marker itself is ever broken the worst case is bounded and local — one
// orphaned test process on a developer machine, not anything touching launchd —
// and a shell that already exports OC_TFC53_EXEC_SEAM_ROLE makes the parent noisy,
// never green.
func TestNewSelfUpdater_HandsOverTheExecSeamThatRefusesInATestBinary(t *testing.T) {
	switch os.Getenv(execSeamEnv) {
	case "replaced":
		// Only reachable as the re-exec'd image. One generation, then out.
		os.Exit(3)
	case "call":
		os.Setenv(execSeamEnv, "replaced")
		u := newSelfUpdater(Config{Base: "https://station.invalid", Token: "tok", ID: "m-box"},
			rawEnv{lookup: os.Getenv}, func(string, ...any) {})
		_ = u.execSelf()
		os.Exit(4)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), execSeamEnv+"=call")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	code := 0
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("could not run the child test binary: %v", err)
	}

	switch code {
	case 1:
		if !strings.Contains(stderr.String(), "syscallExecImage") {
			t.Errorf("the child exited 1 but not from the exec seam's refusal; stderr:\n%s",
				stderr.String())
		}
	case 4:
		t.Error("execSelf RETURNED. The exec effect production hands over is not the real " +
			"syscall — a stub that returns leaves the binary swap unapplied, execInPlace " +
			"reads it as a failed exec and exits 0, and launchd does not relaunch an " +
			"exited warden. Fleet-wide, on the next release.")
	case 3:
		t.Error("the exec ACTUALLY replaced the test binary's process image — the refusal " +
			"on syscallExecImage is gone. A test binary that can exec itself into ocwarden " +
			"is exactly what hostseam_test.go exists to prevent.")
	default:
		t.Errorf("child exited %d, want 1 (the refusal); stderr:\n%s", code, stderr.String())
	}
}
