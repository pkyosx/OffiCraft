package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// killRecord captures a kill invocation's argv so a test asserts command
// construction without spawning tmux (which would SIGHUP the test process).
type killRecord struct {
	calls [][3]string // (bin, socket, session) per call
	err   error
}

func (k *killRecord) kill(bin, socket, session string) error {
	k.calls = append(k.calls, [3]string{bin, socket, session})
	return k.err
}

func TestRunSuicide_NoSessionIsNoOp(t *testing.T) {
	var out bytes.Buffer
	rec := &killRecord{}
	env := func(string) string { return "" } // no OC_SESSION
	if rc := runSuicide(env, &out, func() string { return "/bin/tmux" }, rec.kill); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("no session ⇒ must NOT kill anything, got %v", rec.calls)
	}
	if !strings.Contains(out.String(), "no OC_SESSION") {
		t.Fatalf("expected the no-session no-op line, got %q", out.String())
	}
}

func TestRunSuicide_KillsResolvedSession(t *testing.T) {
	var out bytes.Buffer
	rec := &killRecord{}
	env := func(k string) string {
		return map[string]string{"OC_SESSION": "member-kyle", "OC_TMUX_SOCKET": "sock"}[k]
	}
	if rc := runSuicide(env, &out, func() string { return "/opt/tmux" }, rec.kill); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	want := [][3]string{{"/opt/tmux", "sock", "member-kyle"}}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("kill argv = %v want %v", rec.calls, want)
	}
}

func TestRunSuicide_DefaultsSocketWhenUnset(t *testing.T) {
	rec := &killRecord{}
	env := func(k string) string {
		if k == "OC_SESSION" {
			return "member-kyle"
		}
		return "" // OC_TMUX_SOCKET unset → defaultTmuxSocket
	}
	runSuicide(env, &bytes.Buffer{}, func() string { return "tmux" }, rec.kill)
	if len(rec.calls) != 1 || rec.calls[0][1] != defaultTmuxSocket {
		t.Fatalf("unset socket must default to %q, got %v", defaultTmuxSocket, rec.calls)
	}
}

func TestRunSuicide_UnresolvableTmuxIsNoOp(t *testing.T) {
	var out bytes.Buffer
	rec := &killRecord{}
	env := func(k string) string {
		if k == "OC_SESSION" {
			return "member-kyle"
		}
		return ""
	}
	if rc := runSuicide(env, &out, func() string { return "" }, rec.kill); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("unresolvable tmux ⇒ must NOT attempt a kill, got %v", rec.calls)
	}
	if !strings.Contains(out.String(), "unresolvable") {
		t.Fatalf("expected the tmux-unresolvable line, got %q", out.String())
	}
}

func TestRunSuicide_KillErrorLoggedNotMasked(t *testing.T) {
	var out bytes.Buffer
	rec := &killRecord{err: errors.New("no server running")}
	env := func(k string) string {
		if k == "OC_SESSION" {
			return "member-kyle"
		}
		return ""
	}
	if rc := runSuicide(env, &out, func() string { return "tmux" }, rec.kill); rc != 0 {
		t.Fatalf("rc = %d want 0 (best-effort self-kill)", rc)
	}
	if !strings.Contains(out.String(), "no server running") {
		t.Fatalf("a kill error must be logged honestly, got %q", out.String())
	}
}
