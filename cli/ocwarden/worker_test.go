package main

import "testing"

func TestWorkerWorkdirForSession(t *testing.T) {
	cases := []struct {
		home    string
		session string
		want    string
	}{
		{"/Users/eva/.officraft/workers", "worker-ow-78173e", "/Users/eva/.officraft/workers/ow-78173e"},
		{"/Users/eva/.officraft/workers", "worker-OW-78173E", "/Users/eva/.officraft/workers/ow-78173e"},
		{"/Users/eva/.officraft/workers", "member-m1", ""},
		{"/Users/eva/.officraft/workers", "worker-", ""},
		{"/Users/eva/.officraft/workers", "", ""},
		{"", "worker-ow-78173e", ""},
	}
	for _, c := range cases {
		if got := workerWorkdirForSession(c.home, c.session); got != c.want {
			t.Errorf("workerWorkdirForSession(%q, %q) = %q, want %q", c.home, c.session, got, c.want)
		}
	}
}

func TestDefaultWorkerHome(t *testing.T) {
	t.Setenv("HOME", "/Users/eva")

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no namespace", map[string]string{}, "/Users/eva/.officraft/workers"},
		{"namespaced", map[string]string{"OC_NAMESPACE": "lab"}, "/Users/eva/.officraft-lab/workers"},
		{"invalid namespace falls back to the main root", map[string]string{"OC_NAMESPACE": "Lab!"}, "/Users/eva/.officraft/workers"},
		{"OC_AGENT_HOME wins as the agents sibling", map[string]string{
			"OC_AGENT_HOME": "/srv/oc/agents", "OC_NAMESPACE": "lab",
		}, "/srv/oc/workers"},
	}
	for _, c := range cases {
		env := func(k string) string { return c.env[k] }
		if got := defaultWorkerHome(env); got != c.want {
			t.Errorf("%s: defaultWorkerHome = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWorkerStopSessionFromArgs(t *testing.T) {
	session, err := workerStopSessionFromArgs(map[string]any{"worker_id": "OW-78173E"})
	if err != nil || session != "worker-ow-78173e" {
		t.Errorf("got (%q, %v), want (\"worker-ow-78173e\", nil)", session, err)
	}

	refused := []struct {
		name string
		args map[string]any
	}{
		{"no worker_id", map[string]any{"member_id": "ow-78173e"}},
		{"blank worker_id", map[string]any{"worker_id": "   "}},
		{"non-string worker_id", map[string]any{"worker_id": 42}},
		{"a raw session name is never accepted", map[string]any{"session_name": "worker-ow-78173e"}},
	}
	for _, c := range refused {
		session, err := workerStopSessionFromArgs(c.args)
		if session != "" {
			t.Errorf("%s: session = %q, want \"\"", c.name, session)
		}
		if err == nil || err.Error() != "command: worker_stop missing worker_id" {
			t.Errorf("%s: err = %v, want \"command: worker_stop missing worker_id\"", c.name, err)
		}
	}
}

func TestLegacyWorkerSessionFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"outsource id yields the retired session", map[string]any{"member_id": "ow-78173e"}, "worker-ow-78173e"},
		{"uppercase outsource id is lowercased", map[string]any{"member_id": "OW-78173E"}, "worker-ow-78173e"},
		{"padded outsource id still matches", map[string]any{"member_id": " ow-78173e "}, "worker- ow-78173e "},
		{"staff member id sweeps nothing", map[string]any{"member_id": "m-seth"}, ""},
		{"session-addressed stop sweeps nothing", map[string]any{"session_name": "member-ow-78173e"}, ""},
		{"blank member_id", map[string]any{"member_id": "  "}, ""},
		{"non-string member_id", map[string]any{"member_id": 42}, ""},
	}
	for _, c := range cases {
		if got := legacyWorkerSessionFromArgs(c.args); got != c.want {
			t.Errorf("%s: legacyWorkerSessionFromArgs = %q, want %q", c.name, got, c.want)
		}
	}
}
