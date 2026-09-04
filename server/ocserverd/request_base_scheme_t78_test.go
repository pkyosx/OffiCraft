package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// T-78 — the scheme this server hands out is decided by the HOST, not by
// whether THIS hop was encrypted.
//
// 🔴 Why this file exists: TLS terminates at Cloudflare, so r.TLS is nil for
// every real request and requestBaseURL answered "http" 100% of the time —
// including for the browser that typed https. That answer is not read and
// discarded: it is baked into an installed machine (buildInstallScript →
// OC_BASE=… → the launchd plist), so it pins that machine to a plaintext base
// for the warden-binary download, the one-time-code→token exchange and every
// later call. Measured 2026-09-04: 166 of 167 .mcp.json files on one host
// carried http://.
//
// The rule (owner 2026-09-04): localhost / 127.0.0.1 → http (local testing),
// everything else → https, with NO configuration knob to get wrong.

func TestBaseURLForHost_LoopbackKeepsHTTP(t *testing.T) {
	// Every one of these is a shape local testing actually uses. A fix that
	// only special-cased the bare literals would send https to the rest and
	// break the very thing the ruling protects.
	for _, host := range []string{
		"localhost",
		"localhost:7755",
		"LocalHost:7755", // Host headers are not case-normalised for us
		"127.0.0.1",
		"127.0.0.1:7755",
		"127.0.0.1:59123",
	} {
		if got, want := baseURLForHost(host), "http://"+host; got != want {
			t.Errorf("baseURLForHost(%q) = %q, want %q — a loopback host must stay http", host, got, want)
		}
	}
}

func TestBaseURLForHost_EverythingElseGetsHTTPS(t *testing.T) {
	for _, host := range []string{
		"officraft.hardcoretech.link",
		"officraft.example.com:8443",
		"192.168.1.5:7755",   // NOT loopback: a LAN address gets https like anything else
		"10.0.0.7",           // ditto
		"not a valid host",   // unparseable ⇒ the SAFE default, which is the encrypted one
		"localhost.evil.com", // the prefix must not be enough
		"127.0.0.1.evil.com", // ditto

		// 🔴 These two ARE loopback literals, and they still get https ON
		// PURPOSE (owner 2026-09-04). Nothing listens on them: config.go:40-42
		// hardwires the bind to 127.0.0.1 ("loopback-bind only"), so a caller
		// naming them reaches nothing either way — and a plaintext door that
		// buys no reachability is a door for free.
		"[::1]:7755",
		"127.0.0.53:7755",
	} {
		if got, want := baseURLForHost(host), "https://"+host; got != want {
			t.Errorf("baseURLForHost(%q) = %q, want %q — only loopback stays http", host, got, want)
		}
	}
}

// 🔴 The scheme must NOT move when the hop's encryption changes. This is the
// assertion that pins the whole point of the ticket: the old code read r.TLS,
// and behind Cloudflare that field is nil for a caller who typed https.
func TestRequestBaseURL_IgnoresWhetherThisHopWasEncrypted(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"public host, plaintext hop (this is production)", "officraft.hardcoretech.link", "https://officraft.hardcoretech.link"},
		{"loopback, plaintext hop", "127.0.0.1:7755", "http://127.0.0.1:7755"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/install.sh", nil)
			req.Host = tc.host
			req.TLS = nil // exactly what Cloudflare hands this process
			if got := requestBaseURL(req); got != tc.want {
				t.Fatalf("requestBaseURL(host=%q, TLS=nil) = %q, want %q", tc.host, got, tc.want)
			}

			// Same host, TLS present. The answer MUST NOT CHANGE — if it does,
			// the scheme is still being read off the hop.
			req2 := httptest.NewRequest("GET", "/install.sh", nil)
			req2.Host = tc.host
			req2.TLS = &tls.ConnectionState{HandshakeComplete: true}
			if got := requestBaseURL(req2); got != tc.want {
				t.Fatalf("requestBaseURL(host=%q, TLS=set) = %q, want %q — the hop must not decide the scheme", tc.host, got, tc.want)
			}
		})
	}
}

// 🔴 The guard above protects a FUNCTION. This one protects the CALL SITE:
// it goes through the real handler, because a change that routed the installer
// through some other base would leave every assertion above green while
// handing a remote machine the wrong URL.
func TestInstallScriptCarriesTheHTTPSBase(t *testing.T) {
	const host = "officraft.example.com"
	s := ocwardenInstallBaseServer(t)
	token := "eyJhbGciOiJIUzI1NiJ9.test.token"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/install.sh?token="+token, nil)
	req.Host = host
	req.TLS = nil
	s.HandleInstallScriptInstallShGet(rec, req, HandleInstallScriptInstallShGetParams{Token: &token})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /install.sh = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if want := `OC_BASE="https://` + host + `"`; !strings.Contains(body, want) {
		t.Fatalf("the served installer must pin OC_BASE to the https base.\nwant substring: %s\nbody:\n%s", want, body)
	}
	if bad := "http://" + host; strings.Contains(body, bad) {
		t.Fatalf("the served installer still contains a plaintext base %q — every URL it bakes in "+
			"(warden binary download, the code→token exchange, OC_BASE itself) must be https.\nbody:\n%s", bad, body)
	}
}

// 🔴 bin/install.sh DEPENDS on this URL being plaintext, and nothing said so
// until now. Its closing report greps the fresh serve log for the one-time
// setup link with the literal pattern
//
//	grep -o "http://[^ ]*/?code=[A-Za-z0-9_-]*"     (bin/install.sh:1622)
//
// If this link ever came out https, that grep would quietly match nothing, the
// installer would fall back to the plain URL, and a fresh install would stop
// printing the claim link the owner needs to claim the server — with no error
// anywhere. The scheme is correct today because the bind is hardwired to
// loopback, but "correct by coincidence" is what this test converts into
// "correct and watched".
func TestFirstRunSetupURLStaysPlaintextForTheLoopbackBind(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7755", "127.0.0.1:59123", "localhost:7755"} {
		got := firstRunSetupURL(addr, "claim-token-abc")
		want := "http://" + addr + "/?code=claim-token-abc"
		if got != want {
			t.Fatalf("firstRunSetupURL(%q) = %q, want %q — bin/install.sh:1622 greps for "+
				"http://…/?code=… and would silently match nothing", addr, got, want)
		}
	}
	// And the rule still applies if the address ever stops being loopback: the
	// point is that the decision comes from schemeForHost, not from a literal.
	if got := firstRunSetupURL("officraft.example.com", "c"); got != "https://officraft.example.com/?code=c" {
		t.Fatalf("a non-loopback address must follow the same rule, got %q", got)
	}
}
