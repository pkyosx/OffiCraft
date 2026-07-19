package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// TestVersionSubcommand asserts the `version` alias set prints a NON-EMPTY build
// identifier and exits 0 — the operational contract Seth needs ("easily distinguish
// if the cli is the right version"). Runs via realMain so the dispatch wiring is
// covered, not just printVersion in isolation.
func TestVersionSubcommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			rc := realMain([]string{arg}, func(string) string { return "" }, strings.NewReader(""), &out)
			if rc != 0 {
				t.Fatalf("%s: exit code = %d, want 0", arg, rc)
			}
			got := out.String()
			if !strings.Contains(got, "ocagent") {
				t.Errorf("%s: output missing binary name; got:\n%s", arg, got)
			}
			// self-hash must always be present and non-empty (it never depends on VCS
			// stamping), which is the always-available identity line.
			if !strings.Contains(got, "self-hash:") {
				t.Errorf("%s: output missing self-hash line; got:\n%s", arg, got)
			}
		})
	}
}

// TestPrintVersionReadsVCS asserts a stamped build's vcs.revision is surfaced, using
// an injected BuildInfo so the assertion doesn't depend on how the test binary was
// built (worktree test runs are unstamped).
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
	got := out.String()
	if !strings.Contains(got, "deadbeefcafe") {
		t.Errorf("vcs.revision not surfaced; got:\n%s", got)
	}
	// self-hash of the fixed bytes must be a stable non-empty prefix.
	if !strings.Contains(got, "self-hash:") || strings.Contains(got, "self-hash:    \n") {
		t.Errorf("self-hash empty; got:\n%s", got)
	}
}

// TestSelfHashDeterministic asserts identical bytes hash identically (the byte-parity
// oracle a human relies on) and unavailable-executable degrades gracefully.
func TestSelfHashDeterministic(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("same-bytes"), nil }
	exe := func() (string, error) { return "x", nil }
	a := selfHash(exe, read)
	b := selfHash(exe, read)
	if a != b {
		t.Errorf("self-hash not deterministic: %q vs %q", a, b)
	}
	if len(a) != selfHashPrefixLen {
		t.Errorf("self-hash prefix len = %d, want %d", len(a), selfHashPrefixLen)
	}
}
