package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The listener a claude member does NOT own: it is started beside the member so
// the member's harness cannot drop it, and it carries the member's session name
// so it dies with the member.

func TestListenerSessionName(t *testing.T) {
	got := listenerSessionName("M1")
	if want := "listen-m1"; got != want {
		t.Errorf("listenerSessionName = %q, want %q", got, want)
	}
	// 🔴 kill.go maps anything under the member prefix to a workdir of the same
	// name, so a listener called "member-m1-listen" would be swept as a member
	// whose workdir does not exist.
	if isMemberSession(got) {
		t.Errorf("%q passes the member-session guard — the kill mechanism would treat the listener as a member", got)
	}
}

func TestStartListenerSession(t *testing.T) {
	t.Run("the stale listener is cleared before the new one is started", func(t *testing.T) {
		h := newSpawnHarness()
		startListenerSession(h.deps(), "officraft", "member-m1", "m1", "exec ocagent listen --deliver-tmux")

		// ORDER, not mere presence. Member session names are reused across
		// respawns, so a listener left from the previous session is watching a
		// name that exists again and will not self-exit; two listeners on one
		// identity make the station evict one, and the loser's escape hatch kills
		// OC_SESSION — the member that was just spawned.
		want := []string{
			"tmux -L officraft kill-session -t listen-m1",
			"tmux -L officraft new-session -d -s listen-m1 -x 160 -y 50 exec ocagent listen --deliver-tmux",
			"tmux -L officraft set-option -t listen-m1 window-size manual",
			"tmux -L officraft resize-window -t listen-m1 -x 160 -y 50",
		}
		if !reflect.DeepEqual(h.runner.calls, want) {
			t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, want)
		}
		if len(h.logs) != 0 {
			t.Errorf("a listener that started must say nothing, logs = %v", h.logs)
		}
	})

	t.Run("a listener that cannot start says so in the warden log", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.fallback = wardenRun{err: errors.New("no server running")}

		startListenerSession(h.deps(), "officraft", "member-m1", "m1", "exec ocagent listen --deliver-tmux")

		want := []string{"listener: could not start listen-m1 for member-m1 (no server running); " +
			"the member boots deaf and the station will recycle it"}
		if !reflect.DeepEqual(h.logs, want) {
			t.Errorf("logs =\n%v\nwant\n%v", h.logs, want)
		}
	})
}

func TestBuildListenerLaunchCommand(t *testing.T) {
	got := buildListenerLaunchCommand("/w/m1", "/w/m1/.oc-token", "http://127.0.0.1:7755",
		"member-m1", "officraft", [][2]string{{"OC_AGENT_HOME", "/ns"}}, "/w/m1/.oc-env")

	// 🔴 OC_SESSION IS THE MEMBER'S SESSION, NOT THE LISTENER'S OWN. It is what
	// the listener pastes into and what its self-exit probe watches; pointing it
	// at listen-m1 would make the listener outlive the member it speaks for, and
	// the station would read a dead member as online forever.
	//
	// The env file is sourced BEFORE the exports so an owner env file that
	// replaced PATH cannot make the absolute /bin/cat unnecessary and then break
	// it; --deliver-tmux is what the whole change is for (without it the listener
	// prints to a stdout nobody reads while still looking healthy to the station).
	want := `cd /w/m1; [ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; ` +
		`export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" OC_BASE=http://127.0.0.1:7755 ` +
		`OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft OC_AGENT_HOME=/ns; ` +
		`export PATH=/w/m1:"$PATH"; ` +
		`exec ocagent listen --deliver-tmux`
	if got != want {
		t.Errorf("listener line =\n%s\nwant\n%s", got, want)
	}

	// No env file ⇒ the source clause is absent entirely rather than sourcing "".
	bare := buildListenerLaunchCommand("/w/m1", "/w/m1/.oc-token", "http://127.0.0.1:7755",
		"member-m1", "officraft", nil, "")
	if strings.Contains(bare, "[ -f ") {
		t.Errorf("no env file was rendered, so nothing should be sourced:\n%s", bare)
	}
	if want := `cd /w/m1; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft; ` +
		`export PATH=/w/m1:"$PATH"; exec ocagent listen --deliver-tmux`; bare != want {
		t.Errorf("listener line =\n%s\nwant\n%s", bare, want)
	}
}
