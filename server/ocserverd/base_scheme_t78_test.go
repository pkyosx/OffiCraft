// Skeleton generated from server/ocserverd/base_scheme_t78.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "LocalHost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "127.0.0.1:7755", want: true},
		{host: "LOCALHOST:80", want: true},
		{host: "127.0.0.1:", want: true},
		{host: "::1", want: false},
		{host: "[::1]:7755", want: false},
		{host: "127.0.0.53", want: false},
		{host: "127.0.0.2:7755", want: false},
		{host: "localhost.evil.com", want: false},
		{host: "station.example.com", want: false},
		{host: "", want: false},
		{host: " localhost ", want: false},
	} {
		t.Run(tc.host, func(t *testing.T) {
			if got := isLoopbackHost(tc.host); got != tc.want {
				t.Fatalf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestSchemeForHost(t *testing.T) {
	for _, tc := range []struct {
		host string
		want string
	}{
		{host: "localhost", want: "http"},
		{host: "127.0.0.1:7755", want: "http"},
		{host: "LOCALHOST:80", want: "http"},
		{host: "station.example.com", want: "https"},
		{host: "::1", want: "https"},
		{host: "127.0.0.53", want: "https"},
		{host: "", want: "https"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			if got := schemeForHost(tc.host); got != tc.want {
				t.Fatalf("schemeForHost(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

func TestNormalizeBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "a loopback base keeps http", raw: "http://127.0.0.1:7755", want: "http://127.0.0.1:7755"},
		{name: "a loopback base stored as https is downgraded", raw: "https://localhost:7755", want: "http://localhost:7755"},
		{name: "a remote base stored as http is upgraded", raw: "http://station.example.com", want: "https://station.example.com"},
		{name: "a remote base already https is unchanged", raw: "https://station.example.com", want: "https://station.example.com"},
		{name: "the scheme is matched case-insensitively and rewritten lowercase", raw: "HTTP://LocalHost:7755", want: "http://LocalHost:7755"},
		{name: "a path is dropped", raw: "http://station.example.com/api/events", want: "https://station.example.com"},
		{name: "a query is dropped", raw: "http://station.example.com?x=1", want: "https://station.example.com"},
		{name: "a fragment is dropped", raw: "http://station.example.com#frag", want: "https://station.example.com"},
		{name: "userinfo is not part of the host", raw: "https://user:pass@127.0.0.1:7755", want: "http://127.0.0.1:7755"},
		{name: "surrounding whitespace is trimmed before normalization", raw: "  http://station.example.com  ", want: "https://station.example.com"},
		{name: "an IPv6 loopback name remains on the encrypted scheme", raw: "http://[::1]:7755", want: "https://[::1]:7755"},
		{name: "a non-http scheme is handed back untouched", raw: "ftp://x", want: "ftp://x"},
		{name: "a bare word is handed back untouched", raw: "notaurl", want: "notaurl"},
		{name: "an empty base is handed back untouched", raw: "", want: ""},
		{name: "whitespace-only input is handed back byte-for-byte", raw: "   ", want: "   "},
		{name: "a scheme with no host is handed back untouched", raw: "http://", want: "http://"},
		{name: "a port with no host is handed back untouched", raw: "http://:9999", want: "http://:9999"},
		{name: "a scheme with only a path is handed back untouched", raw: "http:///api", want: "http:///api"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBase(tc.raw); got != tc.want {
				t.Fatalf("normalizeBase(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
