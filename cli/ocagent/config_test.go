package main

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestRequireBase(t *testing.T) {
	t.Run("a configured base lets the caller through in silence", func(t *testing.T) {
		var errOut bytes.Buffer
		if requireBase(Config{Base: "https://station.example.com", BaseConfigured: true}, "diff", &errOut) {
			t.Fatal("requireBase said stop for a configured base")
		}
		if errOut.String() != "" {
			t.Fatalf("stderr %q, want nothing", errOut.String())
		}
	})

	t.Run("an unconfigured base stops the caller and names the subcommand", func(t *testing.T) {
		var errOut bytes.Buffer
		if !requireBase(Config{Base: defaultBase}, "upload", &errOut) {
			t.Fatal("requireBase let an unconfigured base through")
		}
		want := "[ocagent] upload: no OC_BASE configured — nothing here knows which station " +
			"to talk to, and the built-in default is this machine's loopback address.\n"
		if errOut.String() != want {
			t.Fatalf("stderr %q, want %q", errOut.String(), want)
		}
	})

	t.Run("the message never echoes the base it fell back to", func(t *testing.T) {
		var errOut bytes.Buffer
		requireBase(Config{Base: defaultBase, Token: "tok-secret"}, "download", &errOut)
		if bytes.Contains(errOut.Bytes(), []byte(defaultBase)) ||
			bytes.Contains(errOut.Bytes(), []byte("tok-secret")) {
			t.Fatalf("stderr %q leaked an OC_* value", errOut.String())
		}
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("a fully wired launch resolves every field", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":       "http://station.example.com/",
			"OC_TOKEN":      "h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9.s",
			"OC_ID":         "member-bob",
			"OC_AGENT_HOME": "/srv/officraft/agents",
			"OC_ROLE":       "engineer",
			"OC_TASK_TYPE":  "build",
		}))
		want := Config{
			Base:           "https://station.example.com",
			BaseConfigured: true,
			Token:          "h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9.s",
			ID:             "member-bob",
			Home:           "/srv/officraft/agents",
			Role:           "engineer",
			TaskType:       "build",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("loadConfig = %+v, want %+v", got, want)
		}
	})

	t.Run("an unset OC_ID falls back to the token's sub claim", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":       "https://station.example.com",
			"OC_TOKEN":      "h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9.s",
			"OC_AGENT_HOME": "/srv/agents",
		}))
		if got.ID != "member-alice" {
			t.Fatalf("ID = %q, want %q", got.ID, "member-alice")
		}
	})

	t.Run("an unset OC_BASE takes the loopback default and records that it was invented", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{"OC_AGENT_HOME": "/srv/agents"}))
		want := Config{Base: "http://127.0.0.1:7755", BaseConfigured: false, Home: "/srv/agents"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("loadConfig = %+v, want %+v", got, want)
		}
	})

	t.Run("a loopback OC_BASE the operator chose counts as configured", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":       "http://127.0.0.1:7755",
			"OC_AGENT_HOME": "/srv/agents",
		}))
		if got.Base != "http://127.0.0.1:7755" || !got.BaseConfigured {
			t.Fatalf("got Base=%q BaseConfigured=%v, want the loopback address counted as configured",
				got.Base, got.BaseConfigured)
		}
	})

	t.Run("a base normalizeBase cannot re-scheme still counts as configured", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":       "http://",
			"OC_AGENT_HOME": "/srv/agents",
		}))
		if got.Base != "http:" || !got.BaseConfigured {
			t.Fatalf("got Base=%q BaseConfigured=%v, want (\"http:\", true)", got.Base, got.BaseConfigured)
		}
	})

	t.Run("no token and no id leaves both empty rather than guessing", func(t *testing.T) {
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":       "https://station.example.com",
			"OC_AGENT_HOME": "/srv/agents",
		}))
		if got.Token != "" || got.ID != "" {
			t.Fatalf("got Token=%q ID=%q, want both empty", got.Token, got.ID)
		}
	})

	t.Run("an unset OC_AGENT_HOME derives this instance's agents root", func(t *testing.T) {
		t.Setenv("HOME", "/home/tester")
		got := loadConfig(testEnv(map[string]string{
			"OC_BASE":      "https://station.example.com",
			"OC_NAMESPACE": "lab",
		}))
		if got.Home != "/home/tester/.officraft-lab/agents" {
			t.Fatalf("Home = %q, want %q", got.Home, "/home/tester/.officraft-lab/agents")
		}
	})
}

func TestFallbackAgentsHome(t *testing.T) {
	homeIs := func(path string) func() (string, error) {
		return func() (string, error) { return path, nil }
	}
	cases := []struct {
		name      string
		namespace string
		home      func() (string, error)
		want      string
	}{
		{"the main instance lives under .officraft", "", homeIs("/home/tester"),
			"/home/tester/.officraft/agents"},
		{"a namespaced instance lives under .officraft-<ns>", "lab", homeIs("/home/tester"),
			"/home/tester/.officraft-lab/agents"},
		{"digits and dashes are inside the locked charset", "lab-2", homeIs("/home/tester"),
			"/home/tester/.officraft-lab-2/agents"},
		{"a traversal namespace yields nothing rather than escaping the root", "../x",
			homeIs("/home/tester"), ""},
		{"an uppercase namespace is outside the charset and yields nothing", "Lab",
			homeIs("/home/tester"), ""},
		{"a 17-character namespace is over the cap and yields nothing", "abcdefghijklmnopq",
			homeIs("/home/tester"), ""},
		{"an unresolvable home yields nothing, namespace or not", "lab",
			func() (string, error) { return "", errors.New("no home") }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fallbackAgentsHome(testEnv(map[string]string{"OC_NAMESPACE": tc.namespace}), tc.home)
			if got != tc.want {
				t.Fatalf("fallbackAgentsHome = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJwtSub(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"a well-formed token yields its sub claim",
			"h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9.sig", "member-alice"},
		{"a padded/standard-alphabet payload is not raw-url base64 and yields nothing",
			"h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9=.sig", ""},
		{"a non-string sub yields nothing", "h.eyJzdWIiOjEyM30.sig", ""},
		{"a payload without sub yields nothing", "h.eyJyb2xlIjoieCJ9.sig", ""},
		{"a payload that is not JSON yields nothing", "h.bm90IGpzb24.sig", ""},
		{"a payload that is not base64 yields nothing", "h.!!!.sig", ""},
		{"a two-segment token yields nothing", "h.eyJzdWIiOiJtZW1iZXItYWxpY2UiLCJleHAiOjF9", ""},
		{"an empty token yields nothing", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jwtSub(tc.token); got != tc.want {
				t.Fatalf("jwtSub(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}
