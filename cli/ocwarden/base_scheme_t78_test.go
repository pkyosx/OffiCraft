package main

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"LOCALHOST", true},
		{"localhost:7755", true},
		{"127.0.0.1:80", true},
		{"127.0.0.1:", true},
		{"::1", false},
		{"[::1]:7755", false},
		{"127.0.0.53", false},
		{"127.0.0.2", false},
		{"localhost.evil.com", false},
		{"oc.example.com", false},
		{"", false},
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
		{"oc.example.com", "https"},
		{"::1", "https"},
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
		{"loopback stays plaintext", "http://127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"localhost stays plaintext", "http://localhost:7755", "http://localhost:7755"},
		{"a stored https loopback is downgraded", "https://127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"the stored http of a real host is upgraded", "http://oc.example.com", "https://oc.example.com"},
		{"path is dropped", "http://oc.example.com/api/agent", "https://oc.example.com"},
		{"query is dropped", "http://oc.example.com?x=1", "https://oc.example.com"},
		{"fragment is dropped", "http://oc.example.com#frag", "https://oc.example.com"},
		{"loopback with a path", "https://127.0.0.1:7755/api", "http://127.0.0.1:7755"},
		{"userinfo is not the host", "http://user:pw@127.0.0.1:7755", "http://127.0.0.1:7755"},
		{"userinfo before a real host", "https://user@oc.example.com", "https://oc.example.com"},
		{"scheme case is ignored, host case is kept", "HTTP://OC.Example.com/x", "https://OC.Example.com"},
		{"surrounding whitespace is trimmed", "  http://localhost  ", "http://localhost"},
		{"an ipv6 host is not loopback", "http://[::1]:7755", "https://[::1]:7755"},
		{"a foreign scheme is handed back untouched", "ftp://x", "ftp://x"},
		{"a bare word is handed back untouched", "notaurl", "notaurl"},
		{"empty is handed back untouched", "", ""},
		{"whitespace only is handed back untouched", "   ", "   "},
		{"no host to re-scheme", "http://", "http://"},
		{"port with no host", "http://:9999", "http://:9999"},
	}
	for _, c := range cases {
		if got := normalizeBase(c.raw); got != c.want {
			t.Errorf("%s: normalizeBase(%q) = %q, want %q", c.name, c.raw, got, c.want)
		}
	}
}
