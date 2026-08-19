package main

// renewwiring_reached_test.go — what the constructor production uses actually
// hands the renewal path.
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
// because none of them changes a name. Reading names is all it ever did; the
// commit that added it claimed it said "production still reaches them", which was
// never what it measured.
//
// So the checks below call buildSelfUpdater and look at the updater that comes
// out. A field that is nil, or an envToken that came from the token FILE, is the
// same defect however it was written.

import (
	"os"
	"path/filepath"
	"testing"
)

// builtUpdater constructs through the same path production does, with only the
// executable seam injected — the one thing refuseInTestBinary is really about.
func builtUpdater(t *testing.T, home string, cfg Config) *updater {
	t.Helper()
	exe := filepath.Join(home, "bin", "ocwarden")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	raw := map[string]string{"HOME": home}
	return buildSelfUpdater(cfg, rawEnv{lookup: func(k string) string { return raw[k] }},
		func(string, ...any) {}, func() (string, error) { return exe, nil },
		func(string, []string, []string) error { return nil })
}

func TestBuildSelfUpdater_HandsRenewalEverythingItNeeds(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Base: "https://station.example", Token: "the-running-credential", ID: "m-box"}
	u := builtUpdater(t, home, cfg)

	// Each of these is a way the fleet stops renewing, or renews unsafely, with
	// nothing else in the package going red. maybeRenewCredential opens with
	// `u.renew == nil || u.writeTok == nil` and returns false — not an error path,
	// "renewal is not in play" — so a miss here is silent on every machine.
	if u.renew == nil {
		t.Error("renew is nil on the updater production builds — self-renewal is off " +
			"fleet-wide and not one log line says so")
	}
	if u.verify == nil {
		t.Error("verify is nil on the updater production builds — confirm-before-write " +
			"is skipped, so an unusable credential replaces the working one and this " +
			"process execs into it, and nothing reaches that host again")
	}
	if u.writeTok == nil {
		t.Error("writeTok is nil on the updater production builds — a due credential " +
			"could never be written")
	}
	if u.token != cfg.Token {
		t.Errorf("token = %q, want the credential this process runs on (%q)", u.token, cfg.Token)
	}
	if want := tokfileFor(home, ""); u.tokfilePath != want {
		t.Errorf("tokfilePath = %q, want the file readTokfile reads (%q)", u.tokfilePath, want)
	}
}

// TestBuildSelfUpdater_ReadsTheRawEnvironmentNotTheTokfileView is the one that
// matters most, and it is checked on the CONSTRUCTED updater rather than on
// newRenewalWiring in isolation, because every way review defeated the old check
// happened between those two points: folded at the call site, shadowed in the
// caller's block, or reassigned before the call.
//
// Given the folded view, envToken reports the token FILE's contents as if somebody
// had exported OC_TOKEN. That trips the infinite-exec guard on precisely the
// machines it protects — every launchd warden, none of which sets OC_TOKEN — and
// the whole fleet quietly stops renewing.
func TestBuildSelfUpdater_ReadsTheRawEnvironmentNotTheTokfileView(t *testing.T) {
	home := t.TempDir()
	tokfile := tokfileFor(home, "")
	if err := os.MkdirAll(filepath.Dir(tokfile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tokfile, []byte("what-the-token-file-holds"), 0o600); err != nil {
		t.Fatalf("seed tokfile: %v", err)
	}
	// Control: the folded view really does report the file, so a green assertion
	// below means the raw view was used — not that both views happen to be empty.
	folded := tokfileEnv(func(k string) string { return map[string]string{"HOME": home}[k] }, os.ReadFile)
	if got := folded("OC_TOKEN"); got != "what-the-token-file-holds" {
		t.Fatalf("control: the folded view should report the token file, got %q", got)
	}

	u := builtUpdater(t, home, Config{Base: "https://station.example", Token: "tok", ID: "m-box"})
	if u.envToken != "" {
		t.Errorf("envToken = %q with a token file on disk and nothing exporting "+
			"OC_TOKEN. The constructor is reading the tokfile-folded view somewhere "+
			"between here and newRenewalWiring, which disables the infinite-exec "+
			"guard on every launchd warden and stops the fleet renewing", u.envToken)
	}
}
