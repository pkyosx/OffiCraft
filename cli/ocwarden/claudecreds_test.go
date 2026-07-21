package main

import (
	"encoding/xml"
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

// ── the escape hatch must actually be pressable ─────────────────────────────
//
// The spawn refusal ADVERTISES OC_CLAUDE_CRED_CHECK=0. The warden is a launchd
// job, so its environment is exactly what its plist says — an operator
// exporting that variable in a shell changes nothing. If the installer does not
// relay it into the plist, the documented way out silently does not exist,
// which at 3am with a dark fleet is worse than no way out at all.

func TestRelayedCredCheck_OnlyTheExplicitOptOutIsRelayed(t *testing.T) {
	cases := map[string]string{
		"0":     "0", // the opt-out
		"1":     "",  // explicitly on ⇒ nothing to stamp
		"":      "",  // unset ⇒ nothing to stamp
		"false": "",  // junk is not an opt-out
		" 0 ":   "0", // trimmed
	}
	for in, want := range cases {
		if got := relayedCredCheck(envOf(map[string]string{"OC_CLAUDE_CRED_CHECK": in})); got != want {
			t.Errorf("relayedCredCheck(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPlist_StampsTheCredCheckOptOut(t *testing.T) {
	p := fixedPaths()
	p.credCheck = "0"
	plist := renderPlist(p)
	if !strings.Contains(plist, "<key>OC_CLAUDE_CRED_CHECK</key><string>0</string>") {
		t.Errorf("the opt-out must reach the warden's plist (its only env source):\n%s", plist)
	}
	// Positive control: without it the render carries no such key at all, so the
	// assertion above cannot be passing for a trivial reason.
	if strings.Contains(renderPlist(fixedPaths()), "OC_CLAUDE_CRED_CHECK") {
		t.Errorf("the gate-on render must stay free of the key")
	}
}

// A root path containing "--" used to render a plist that is not well-formed XML
// ("--" is forbidden inside an XML comment), so plutil rejected the install with
// an opaque message. namespaceShape ([a-z0-9-]{1,16}) genuinely allows "a--b".
func TestRenderPlist_DoubleDashRootStaysWellFormedXML(t *testing.T) {
	p := fixedPaths()
	p.root = "/tmp/oc--scratch/.officraft"
	plist := renderPlist(p)
	// slice the comment BODY (the delimiters themselves contain "--")
	comment := plist[strings.Index(plist, "<!--")+4 : strings.Index(plist, "-->")]
	if strings.Contains(comment, "--") {
		t.Errorf("the header comment must carry no double dash (invalid XML):\n%s", comment)
	}
	if err := xml.Unmarshal([]byte(plist), new(struct {
		XMLName xml.Name `xml:"plist"`
	})); err != nil {
		t.Errorf("rendered plist must parse as XML: %v\n%s", err, plist)
	}
	// The WorkingDirectory (a <string>, not a comment) keeps the REAL path —
	// sanitising the comment must not corrupt the value the warden runs in.
	if !strings.Contains(plist, "<key>WorkingDirectory</key><string>/tmp/oc--scratch/.officraft</string>") {
		t.Errorf("the real root must survive verbatim outside the comment:\n%s", plist)
	}
}
