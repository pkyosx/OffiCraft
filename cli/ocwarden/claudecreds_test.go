package main

import (
	"errors"
	"strings"
	"testing"
)

// envOf builds an env lookup from a map (absent key → "").
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// credRunner is a CmdRunner whose keychain lookup outcome is pinned, and which
// RECORDS its argv so the "no -w" security invariant is assertable.
type credRunner struct {
	found bool
	calls [][]string
}

func (c *credRunner) Run(name string, args ...string) (string, error) {
	c.calls = append(c.calls, append([]string{name}, args...))
	if c.found {
		return "keychain: item metadata", nil
	}
	return "", errors.New("SecKeychainSearchCopyNext: The specified item could not be found")
}

func TestProbeClaudeCreds_AbsentEverywhere(t *testing.T) {
	r := &credRunner{found: false}
	st := probeClaudeCreds(envOf(map[string]string{"HOME": "/h"}),
		func(string) bool { return false }, r, "darwin")
	if st.Present {
		t.Fatalf("no source present must report Present=false; summary=%q", st.Summary)
	}
	for _, want := range []string{"cred_file=unset", "keychain=unset", "ANTHROPIC_API_KEY=unset"} {
		if !strings.Contains(st.Summary, want) {
			t.Errorf("summary must name %q; got %q", want, st.Summary)
		}
	}
}

func TestProbeClaudeCreds_EachSourceAloneIsEnough(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		exists bool
		key    bool // keychain
		want   string
	}{
		{"credfile", map[string]string{"HOME": "/h"}, true, false, "cred_file=SET"},
		{"keychain", map[string]string{"HOME": "/h"}, false, true, "keychain=SET"},
		{"apikey", map[string]string{"HOME": "/h", "ANTHROPIC_API_KEY": "sk-xxx"}, false, false, "ANTHROPIC_API_KEY=SET"},
		{"authtoken", map[string]string{"HOME": "/h", "ANTHROPIC_AUTH_TOKEN": "tok"}, false, false, "ANTHROPIC_AUTH_TOKEN=SET"},
		{"bedrock", map[string]string{"HOME": "/h", "CLAUDE_CODE_USE_BEDROCK": "1"}, false, false, "CLAUDE_CODE_USE_BEDROCK=SET"},
		{"vertex", map[string]string{"HOME": "/h", "CLAUDE_CODE_USE_VERTEX": "1"}, false, false, "CLAUDE_CODE_USE_VERTEX=SET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := probeClaudeCreds(envOf(tc.env), func(string) bool { return tc.exists },
				&credRunner{found: tc.key}, "darwin")
			if !st.Present {
				t.Fatalf("%s alone must satisfy the gate; summary=%q", tc.name, st.Summary)
			}
			if !strings.Contains(st.Summary, tc.want) {
				t.Errorf("summary must contain %q; got %q", tc.want, st.Summary)
			}
		})
	}
}

// 🔴 The security invariant, asserted mechanically: the summary is allowed to
// contain source NAMES and the words SET/unset — nothing else. A credential
// value, a token fragment, or the credentials-file PATH leaking in would fail
// here. The env below plants a distinctive value in EVERY value-bearing source
// so a leak has something to be caught by (a positive control: without the
// values planted, "no leak found" would be indistinguishable from "nothing to
// find").
func TestProbeClaudeCreds_SummaryNeverCarriesValuesOrPaths(t *testing.T) {
	secret := "sk-ant-SECRETVALUE-должен-не-появиться"
	home := "/Users/leaky-home"
	st := probeClaudeCreds(envOf(map[string]string{
		"HOME":                    home,
		"ANTHROPIC_API_KEY":       secret,
		"ANTHROPIC_AUTH_TOKEN":    secret,
		"CLAUDE_CODE_USE_BEDROCK": secret,
		"CLAUDE_CODE_USE_VERTEX":  secret,
	}), func(string) bool { return true }, &credRunner{found: true}, "darwin")
	if !st.Present {
		t.Fatalf("sanity: this fixture is fully credentialed")
	}
	if strings.Contains(st.Summary, secret) {
		t.Fatalf("SECURITY: summary leaked a credential value: %q", st.Summary)
	}
	if strings.Contains(st.Summary, home) || strings.Contains(st.Summary, ".credentials.json") {
		t.Fatalf("SECURITY: summary leaked a credential path: %q", st.Summary)
	}
	// Whitelist every token: "<known-source>=SET|unset" and nothing else.
	allowed := map[string]bool{"cred_file": true, "keychain": true}
	for _, k := range claudeCredEnvKeys {
		allowed[k] = true
	}
	for _, tok := range strings.Fields(st.Summary) {
		name, verdict, ok := strings.Cut(tok, "=")
		if !ok || !allowed[name] || (verdict != "SET" && verdict != "unset") {
			t.Errorf("summary token %q is not a whitelisted <source>=SET|unset pair", tok)
		}
	}
}

// The keychain lookup must never request the payload: `-w` returns the secret.
func TestProbeClaudeCreds_KeychainLookupIsMetadataOnly(t *testing.T) {
	r := &credRunner{found: true}
	probeClaudeCreds(envOf(map[string]string{"HOME": "/h"}),
		func(string) bool { return false }, r, "darwin")
	if len(r.calls) != 1 {
		t.Fatalf("expected exactly one keychain lookup; got %v", r.calls)
	}
	for _, a := range r.calls[0] {
		if a == "-w" {
			t.Fatalf("SECURITY: keychain lookup must NOT pass -w (that returns the secret): %v", r.calls[0])
		}
	}
}

// Non-darwin drops the keychain source honestly (absent, never a fake false).
func TestProbeClaudeCreds_NonDarwinOmitsKeychain(t *testing.T) {
	st := probeClaudeCreds(envOf(map[string]string{"HOME": "/h"}),
		func(string) bool { return false }, &credRunner{found: true}, "linux")
	if strings.Contains(st.Summary, "keychain") {
		t.Errorf("non-darwin must omit the keychain source entirely; got %q", st.Summary)
	}
}

func TestBuildClaudeCredProbe_OptOutDisablesGate(t *testing.T) {
	if p := buildClaudeCredProbe(envOf(map[string]string{"OC_CLAUDE_CRED_CHECK": "0"}), &credRunner{}); p != nil {
		t.Errorf("OC_CLAUDE_CRED_CHECK=0 must return a nil (off) seam")
	}
	if p := buildClaudeCredProbe(envOf(map[string]string{}), &credRunner{}); p == nil {
		t.Errorf("the gate must be ON by default")
	}
}
