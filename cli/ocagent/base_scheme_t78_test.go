package main

import "testing"

// T-78 — the stored scheme in OC_BASE is IGNORED; the host decides.
//
// 🔴 This is the half that reaches machines ALREADY INSTALLED. Their launchd
// plist says OC_BASE=http://… and nothing rewrites it, so correctness has to
// happen on every read instead. Measured 2026-09-04: 166 of 167 agent configs
// on one host carried http://, and an edge redirect to https took every one of
// their MCP clients down at once.
func TestNormalizeBase_HostDecidesTheScheme(t *testing.T) {
	cases := []struct{ in, want string }{
		// the case that matters: what is stored on every installed machine today
		{"http://officraft.hardcoretech.link", "https://officraft.hardcoretech.link"},
		{"http://officraft.hardcoretech.link/", "https://officraft.hardcoretech.link"},
		{"https://officraft.hardcoretech.link", "https://officraft.hardcoretech.link"},
		// local testing keeps plaintext, with or without a port
		{"http://127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"http://localhost:7755", "http://localhost:7755"},
		{"http://LOCALHOST:7755", "http://LOCALHOST:7755"},
		{"http://127.0.0.1", "http://127.0.0.1"},
		// a stored https on loopback is downgraded — the host decides, not the store
		{"https://127.0.0.1:7755", "http://127.0.0.1:7755"},
		// not loopback, however much it looks like it
		{"http://localhost.evil.com", "https://localhost.evil.com"},
		{"http://[::1]:7755", "https://[::1]:7755"},
		{"http://192.168.1.5:7755", "https://192.168.1.5:7755"},
		// a path the caller appended is not part of the base
		{"http://officraft.hardcoretech.link/api/mcp", "https://officraft.hardcoretech.link"},
		// nothing to work with ⇒ handed back untouched rather than invented
		{"", ""},
		{"://", "://"},
		{"http://", "http://"},
		{":9999", ":9999"},

		// 🔴 REGRESSION (caught by resolvePaths' own test, 2026-09-04): a scheme we
		// do not recognise, or no scheme at all, must come back UNTOUCHED. An
		// earlier draft turned these into https://x and https://notaurl, which then
		// passed ocBaseShape — the normaliser had repaired input a guard exists to
		// reject.
		{"ftp://x", "ftp://x"},
		{"notaurl", "notaurl"},

		// 🔴 THESE THREE EXIST BECAUSE OF A MUTANT THE MIRROR GUARD CANNOT SEE.
		// The independent reviewer changed IndexAny(host, "/?#") to "/?" in ALL
		// THREE copies at once: the guard passed (the copies still matched) and
		// every module's suite passed (not one input carried a fragment). The
		// guard defends against DRIFT, not against the same mistake made
		// everywhere — only a test input can do that.
		{"http://officraft.hardcoretech.link#frag", "https://officraft.hardcoretech.link"},
		{"http://127.0.0.1:7755#frag", "http://127.0.0.1:7755"},
		{"http://officraft.hardcoretech.link?a=b", "https://officraft.hardcoretech.link"},
		// userinfo is not the host: without stripping it, this reads as NOT
		// loopback and a working local base gets upgraded to https.
		{"http://user:pass@127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"HTTP://officraft.hardcoretech.link", "https://officraft.hardcoretech.link"},
	}
	for _, tc := range cases {
		if got := normalizeBase(tc.in); got != tc.want {
			t.Errorf("normalizeBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSchemeForHost_OnlyTheTwoLoopbackNamesStayPlaintext(t *testing.T) {
	for _, h := range []string{"localhost", "localhost:7755", "127.0.0.1", "127.0.0.1:59123", "LocalHost"} {
		if got := schemeForHost(h); got != "http" {
			t.Errorf("schemeForHost(%q) = %q, want http", h, got)
		}
	}
	for _, h := range []string{"officraft.hardcoretech.link", "10.0.0.7", "[::1]:7755", "127.0.0.53", "localhost.evil.com", "not a host"} {
		if got := schemeForHost(h); got != "https" {
			t.Errorf("schemeForHost(%q) = %q, want https", h, got)
		}
	}
}
