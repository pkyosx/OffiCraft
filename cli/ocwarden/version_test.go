package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// TestVersionSubcommand asserts the `version` alias set prints a NON-EMPTY build
// identifier and exits 0 — the operational contract Seth needs ("easily distinguish
// if the cli is the right version"). Runs via realMain so dispatch wiring is covered.
func TestVersionSubcommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			rc := realMain([]string{arg}, func(string) string { return "" }, &out)
			if rc != 0 {
				t.Fatalf("%s: exit code = %d, want 0", arg, rc)
			}
			got := out.String()
			if !strings.Contains(got, "ocwarden") {
				t.Errorf("%s: output missing binary name; got:\n%s", arg, got)
			}
			if !strings.Contains(got, "self-hash:") {
				t.Errorf("%s: output missing self-hash line; got:\n%s", arg, got)
			}
		})
	}
}

// TestVersionNotInHelp guards the CI parity invariant: the version block must NOT
// leak into the usage banner (--help / bare invocation), or bin/ci.sh 7d would flap.
func TestVersionNotInHelp(t *testing.T) {
	var out bytes.Buffer
	// bare invocation prints the usage banner (exit 0).
	realMain(nil, func(string) string { return "" }, &out)
	if strings.Contains(out.String(), "self-hash") || strings.Contains(out.String(), "vcs.revision") {
		t.Errorf("version details leaked into usage banner:\n%s", out.String())
	}
}

// TestPrintVersionReadsVCS asserts a stamped build's vcs.revision is surfaced, using
// an injected BuildInfo so the assertion doesn't depend on the test binary's stamping.
func TestPrintVersionReadsVCS(t *testing.T) {
	var out bytes.Buffer
	bi := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeefcafe"},
			{Key: "vcs.time", Value: "2026-07-10T00:00:00Z"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}
	exe := func() (string, error) { return "/proc/self/fake", nil }
	read := func(string) ([]byte, error) { return []byte("binary-bytes"), nil }
	printVersion(&out, bi, exe, read)
	if !strings.Contains(out.String(), "deadbeefcafe") {
		t.Errorf("vcs.revision not surfaced; got:\n%s", out.String())
	}
}

// TestSelfHashDeterministic asserts identical bytes hash identically (the byte-parity
// oracle) and matches the self-updater's hashPrefix contract.
func TestSelfHashDeterministic(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("same-bytes"), nil }
	exe := func() (string, error) { return "x", nil }
	a := selfHash(exe, read)
	b := selfHash(exe, read)
	if a != b {
		t.Errorf("self-hash not deterministic: %q vs %q", a, b)
	}
	if a != hashPrefix([]byte("same-bytes")) {
		t.Errorf("self-hash %q != hashPrefix contract %q", a, hashPrefix([]byte("same-bytes")))
	}
}
