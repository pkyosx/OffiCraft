package main

import (
	"os"
	"path/filepath"
	"testing"
)

// runtimeprobe_claude_unknown_test.go — the claude arm may only report a login
// verdict it actually has evidence for (T-b3d0, owner ruling: 「Installed 不知道
// 不應該標記」).
//
// The asymmetry these tests pin down: codex's logged_in comes from running
// `codex login status`, so its false is measured. Claude's comes from two
// presence checks with no probe behind them, so "neither matched" is only an
// absence of evidence. Publishing that as false made the server's
// runtimeCapabilityReady (reconcile.go) reject a perfectly usable claude and
// PERSIST codex onto the roster row — an irreversible choice built on a guess.

// stagedClaudeEnv returns an env func whose OC_CLAUDE_BIN resolves, so
// collectRuntimeCapabilities takes the installed branch of the claude arm. No
// codex is staged: these tests are about the claude arm alone.
func stagedClaudeEnv(t *testing.T) func(string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("stage a resolvable claude: %v", err)
	}
	return func(key string) string {
		switch key {
		case "OC_CLAUDE_BIN":
			return bin
		case "HOME":
			return t.TempDir()
		}
		return ""
	}
}

// claudeCapability runs the real collector and returns its claude entry, after
// asserting the precondition that the binary resolved — without that, the arm
// under test never executes and every assertion below would pass vacuously.
func claudeCapability(t *testing.T, probe map[string]any) map[string]any {
	t.Helper()
	got := collectRuntimeCapabilities(stagedClaudeEnv(t), runtimeProbeRunner{}, probe, nil)
	capability, ok := got["claude"].(map[string]any)
	if !ok {
		t.Fatalf("collector emitted no claude entry: %#v", got)
	}
	if installed, _ := capability["installed"].(bool); !installed {
		t.Fatalf("precondition: the staged claude did not resolve, so the arm under "+
			"test never ran; capability = %#v", capability)
	}
	return capability
}

// TestClaudeLoginUnknownIsOmittedNotReportedFalse is the ruling itself. Both
// presence checks came back negative; the collector has no login probe, so it
// must say nothing rather than say "signed out".
func TestClaudeLoginUnknownIsOmittedNotReportedFalse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		probe map[string]any
	}{
		{"probe reported neither key", map[string]any{"version": "2.1.211"}},
		{"both presence checks negative", map[string]any{
			"cred_file": false, "keychain": false}},
		{"keys present but not booleans", map[string]any{
			"cred_file": "no", "keychain": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capability := claudeCapability(t, tc.probe)
			if value, present := capability["logged_in"]; present {
				t.Errorf("logged_in = %#v, want the key ABSENT. Finding no "+
					"credential is not the same claim as finding the host signed "+
					"out: claude has no login probe, only two presence checks. A "+
					"reported false is spent by the server as fact — "+
					"runtimeCapabilityReady rejects it and placement persists codex "+
					"onto the roster row forever. capability = %#v",
					value, capability)
			}
		})
	}
}

// TestClaudeLoginTrueStillReportedWhenEvidenceExists is the positive control
// for the test above: if the omission were unconditional, the ruling would have
// been implemented by deleting the signal rather than by telling the truth
// about it, and the test above could not tell the difference.
func TestClaudeLoginTrueStillReportedWhenEvidenceExists(t *testing.T) {
	for _, tc := range []struct {
		name  string
		probe map[string]any
	}{
		{"credential file found", map[string]any{"cred_file": true, "keychain": false}},
		{"keychain item found", map[string]any{"cred_file": false, "keychain": true}},
		{"both found", map[string]any{"cred_file": true, "keychain": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capability := claudeCapability(t, tc.probe)
			if capability["logged_in"] != true {
				t.Errorf("logged_in = %#v, want true — evidence WAS found and must "+
					"still be reported; capability = %#v",
					capability["logged_in"], capability)
			}
		})
	}
}
