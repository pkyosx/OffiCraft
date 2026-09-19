package main

import (
	"errors"
	"strings"
	"testing"
)

// The listener a claude member does NOT own: it is started beside the member so
// the member's harness cannot drop it, and it carries the member's session name
// so it dies with the member.

func TestListenerSessionNameIsNotAMemberSession(t *testing.T) {
	got := listenerSessionName("M1")
	if got != "listen-m1" {
		t.Errorf("listenerSessionName = %q, want listen-m1", got)
	}
	// 🔴 kill.go maps anything under the member prefix to a workdir of the same
	// name, so a listener called "member-m1-listen" would be swept as a member
	// whose workdir does not exist.
	if isMemberSession(got) {
		t.Errorf("%q passes the member-session guard — the kill mechanism would treat the listener as a member", got)
	}
}

func TestStartListenerSessionClearsTheStaleOneFirst(t *testing.T) {
	h := newSpawnHarness()
	d := h.deps()

	startListenerSession(d, "officraft", "member-m1", "m1", "exec ocagent listen --deliver-tmux")

	if len(h.runner.calls) < 2 {
		t.Fatalf("calls = %v", h.runner.calls)
	}
	// ORDER, not mere presence. Member session names are reused across respawns,
	// so a listener left from the previous session is watching a name that exists
	// again and will not self-exit; two listeners on one identity make the station
	// evict one, and the loser's escape hatch kills OC_SESSION — the member that
	// was just spawned.
	if got := h.runner.calls[0]; got != "tmux -L officraft kill-session -t listen-m1" {
		t.Errorf("call 0 = %q, want the stale listener to be cleared first", got)
	}
	if got := h.runner.calls[1]; !strings.HasPrefix(got, "tmux -L officraft new-session -d -s listen-m1 ") {
		t.Errorf("call 1 = %q, want the new listener session", got)
	}
}

func TestStartListenerSessionLogsAFailureAndDoesNotAbortTheSpawn(t *testing.T) {
	h := newSpawnHarness()
	h.runner.fallback = wardenRun{err: errors.New("no server running")}
	d := h.deps()

	startListenerSession(d, "officraft", "member-m1", "m1", "exec ocagent listen --deliver-tmux")

	joined := strings.Join(h.logs, "\n")
	if !strings.Contains(joined, "listen-m1") {
		t.Errorf("a listener that could not start must say so, logs = %q", joined)
	}
}

func TestListenerLaunchCommandCarriesTheMemberSessionAndTheDeliveryFlag(t *testing.T) {
	got := buildListenerLaunchCommand("/w/m1", "/w/m1/.oc-token", "http://127.0.0.1:7755",
		"member-m1", "officraft", [][2]string{{"OC_AGENT_HOME", "/ns"}}, "/w/m1/.oc-env")

	for _, want := range []string{
		`cd /w/m1; `,
		`[ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; `,
		`OC_TOKEN="$(/bin/cat /w/m1/.oc-token)"`,
		`OC_SESSION=member-m1`,
		`OC_TMUX_SOCKET=officraft`,
		`OC_AGENT_HOME=/ns`,
		`exec ocagent listen --deliver-tmux`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("listener line is missing %q:\n%s", want, got)
		}
	}
	// Without the flag the listener prints to a stdout nobody reads and the
	// member never hears about a single event, while looking perfectly healthy
	// from the station (it still holds the connection).
	if !strings.HasSuffix(got, "--deliver-tmux") {
		t.Errorf("the delivery flag must be the last thing on the line:\n%s", got)
	}
	// The env file is sourced BEFORE the token is read: an owner env file that
	// replaced PATH would otherwise be read after /bin/cat had already resolved.
	if strings.Index(got, ".oc-env; ") > strings.Index(got, "OC_TOKEN=") {
		t.Errorf("the agent env must be sourced before the exports:\n%s", got)
	}
}
