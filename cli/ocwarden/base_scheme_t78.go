package main

// base_scheme_t78.go — the one rule that decides http vs https, in the warden.
//
// See the canonical block below. Edit ALL THREE copies together or the mirror
// guard fails.

import (
	"net"
	"strings"
)

// ── T-78 CANONICAL BLOCK — DO NOT EDIT ONE COPY ─────────────────────────────
// This block is duplicated VERBATIM in three modules and bin/tests/base-scheme-
// mirror-guard.sh fails the build if the copies ever diverge:
//
//	server/ocserverd/base_scheme_t78.go   — what the server HANDS OUT
//	cli/ocwarden/base_scheme_t78.go       — what the warden CALLS and what it
//	                                        writes into each agent's .mcp.json
//	cli/ocagent/base_scheme_t78.go        — what the agent CALLS
//
// It is copied rather than shared because these are four separate Go modules
// with no common one; a shared package is the right fix and is deliberately NOT
// being done under an incident clock.
//
// THE RULE (owner 2026-09-04): the scheme is decided by the HOST.
//
//	localhost / 127.0.0.1 (optional port, any case)  →  http
//	everything else                                   →  https
//
// It is NOT read off whether this hop was encrypted, and NOT read off whatever
// scheme happens to be stored in OC_BASE. Both of those were measured wrong:
// TLS terminates at the edge, so the server saw plaintext for 100% of callers
// and handed out http://; and that http:// was then baked into every installed
// machine's launchd plist, where nothing ever re-derives it.
//
// 🔴 WHY LOOPBACK IS EXACTLY TWO NAMES AND NOT "every net.IP.IsLoopback". The
// server binds 127.0.0.1 and only 127.0.0.1 — hardwired, with the comment "the
// security model is loopback-bind only". Nothing listens on ::1 or on
// 127.0.0.53, so naming them reaches nothing whichever scheme we pick; widening
// the plaintext allowance to cover them buys no reachability and costs a door.
//
// A host this cannot parse is NOT loopback: the safe default is the encrypted
// one.
func isLoopbackHost(host string) bool {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	switch strings.ToLower(h) {
	case "localhost", "127.0.0.1":
		return true
	}
	return false
}

// schemeForHost is the whole of the rule.
func schemeForHost(host string) string {
	if isLoopbackHost(host) {
		return "http"
	}
	return "https"
}

// normalizeBase keeps the HOST of a stored base URL and re-decides its scheme.
//
// This is the half that reaches machines already installed: their OC_BASE says
// http:// and nothing will ever rewrite it, so every reader normalises on the
// way in instead. A base it cannot make sense of is handed back UNCHANGED —
// refusing to guess beats inventing a URL that reaches nothing.
func normalizeBase(raw string) string {
	s := strings.TrimSpace(raw)
	// ONLY a base that already declares http or https is re-schemed. Everything
	// else is handed back UNTOUCHED so the caller's own validation still sees
	// exactly what the operator typed.
	//
	// 🔴 THIS GUARD WAS ADDED AFTER IT FAILED. An earlier draft normalised any
	// input, so "ftp://x" became "https://x" and a bare "notaurl" became
	// "https://notaurl" — and both then PASSED ocBaseShape, which exists to
	// reject exactly those. resolvePaths' own test caught it
	// (install_test.go: expected shape error for OC_BASE="ftp://x").
	// A normaliser that repairs malformed input does not help the operator; it
	// deletes the check that would have told them.
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return raw
	}
	host := s[strings.Index(s, "://")+3:]
	// Anything after the authority is a path, query or fragment the caller
	// appended, not the base. ⚠️ ALL THREE delimiters matter: the reviewer
	// showed that dropping "#" here is a change every module's own suite still
	// passes, because not one test input carried a fragment. The cases below
	// now do.
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	// userinfo is not part of the host. Without this, "user:pass@127.0.0.1:7755"
	// splits on the LAST colon, leaves "user:pass@127.0.0.1", and is judged NOT
	// loopback — so a local base would be upgraded to https and stop working.
	// (Reviewer finding; it fails toward the encrypted answer, which is why it
	// is a wrong answer rather than an unsafe one.)
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	// "http://" and "http://:9999" land here: no host to re-scheme, and inventing
	// one produces a URL that reaches nothing.
	if host == "" || strings.HasPrefix(host, ":") {
		return raw
	}
	return schemeForHost(host) + "://" + host
}

// ── END T-78 CANONICAL BLOCK ────────────────────────────────────────────────
