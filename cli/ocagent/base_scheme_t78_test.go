package main

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LocalHost", true},
		{"127.0.0.1", true},
		{"127.0.0.1:7755", true},
		{"LOCALHOST:80", true},
		{"::1", false},
		{"[::1]:7755", false},
		{"127.0.0.53", false},
		{"127.0.0.2:7755", false},
		{"station.example.com", false},
		{"station.example.com:7755", false},
		{"", false},
		{" localhost ", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestSchemeForHost(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"localhost", "http"},
		{"127.0.0.1:7755", "http"},
		{"station.example.com", "https"},
		{"::1", "https"},
		{"127.0.0.53", "https"},
		{"", "https"},
	}
	for _, c := range cases {
		if got := schemeForHost(c.host); got != c.want {
			t.Errorf("schemeForHost(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestNormalizeBase(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a loopback base keeps http", "http://127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"a loopback base stored as https is downgraded", "https://localhost:7755", "http://localhost:7755"},
		{"a remote base stored as http is upgraded", "http://station.example.com", "https://station.example.com"},
		{"a remote base already https is unchanged", "https://station.example.com", "https://station.example.com"},
		{"the scheme is matched case-insensitively and rewritten lowercase", "HTTP://LocalHost:7755", "http://LocalHost:7755"},
		{"a path is dropped", "http://station.example.com/api/events", "https://station.example.com"},
		{"a query is dropped", "http://station.example.com?x=1", "https://station.example.com"},
		{"a fragment is dropped", "http://station.example.com#frag", "https://station.example.com"},
		{"userinfo is not part of the host", "https://user:pass@127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"surrounding whitespace is trimmed", "  http://station.example.com  ", "https://station.example.com"},
		{"a non-http scheme is handed back untouched", "ftp://x", "ftp://x"},
		{"a bare word is handed back untouched", "notaurl", "notaurl"},
		{"an empty base is handed back untouched", "", ""},
		{"a scheme with no host is handed back untouched", "http://", "http://"},
		{"a port with no host is handed back untouched", "http://:9999", "http://:9999"},
		{"a scheme with only a path is handed back untouched", "http:///api", "http:///api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeBase(c.raw); got != c.want {
				t.Errorf("normalizeBase(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}
