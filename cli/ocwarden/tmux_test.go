package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// wardenRun is one scripted answer of wardenRunner.
type wardenRun struct {
	out string
	err error
}

// wardenRunner is the CmdRunner double: it records every argv it is handed
// (joined with spaces) and answers from a script keyed by that same argv, so a
// test can assert both WHAT was run and what the code did with the answer.
type wardenRunner struct {
	script   map[string]wardenRun
	fallback wardenRun
	calls    []string
	// shellPassthrough makes an UNSCRIPTED `<shell> -c <script>` argv really run,
	// combined output and all. Opt-in per test: a rendered shell fragment asserted
	// as text passes just as happily when it word-splits wrong, matches the wrong
	// pattern, or resolves no binary, so the fragments that decide the child's
	// config home are exercised instead of read.
	shellPassthrough bool
}

func (r *wardenRunner) Run(name string, args ...string) (string, error) {
	return r.answer(name, args, false)
}

// RunCombined is the double's half of the CombinedCmdRunner seam.
func (r *wardenRunner) RunCombined(name string, args ...string) (string, error) {
	return r.answer(name, args, true)
}

func (r *wardenRunner) answer(name string, args []string, combined bool) (string, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, key)
	if res, ok := r.script[key]; ok {
		return res.out, res.err
	}
	if r.shellPassthrough && len(args) == 2 && args[0] == "-c" {
		return r.passthrough(name, args, combined)
	}
	return r.fallback.out, r.fallback.err
}

// passthrough really runs the shell line AND LOSES WHAT THE PRODUCTION SEAM WOULD
// HAVE LOST. This used to return CombinedOutput for both halves, which made the
// double strictly more generous than execRunner: a command that exits non-zero
// and answers on stderr came back with its answer here and as "" in production,
// so the pre-trust probe's whole test suite was green while every real spawn was
// refused. Run therefore mirrors execRunner.Run (stdout dropped on a non-zero
// exit, stderr folded into the error) and only RunCombined keeps both.
func (r *wardenRunner) passthrough(name string, args []string, combined bool) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if combined {
		cmd.Stderr = &out
	}
	err := cmd.Run()
	if combined {
		return out.String(), err
	}
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return out.String(), nil
}

func TestTmuxClassifyAbsent(t *testing.T) {
	cases := []struct {
		errText string
		want    bool
	}{
		{"no server running on /private/tmp/tmux-501/officraft", true},
		{"can't find session: member-m1", true},
		{"session not found: member-m1", true},
		{"error connecting to /private/tmp/tmux-501/officraft (No such file or directory)", true},
		{"CAN'T FIND SESSION: MEMBER-M1", true},
		{"error connecting to /tmp/x (Connection refused)", false},
		{"exec: \"tmux\": executable file not found in $PATH", false},
		{"signal: killed", false},
		{"", false},
	}
	for _, c := range cases {
		if got := tmuxClassifyAbsent(c.errText); got != c.want {
			t.Errorf("tmuxClassifyAbsent(%q) = %v, want %v", c.errText, got, c.want)
		}
	}
}

func TestTmuxHasSession(t *testing.T) {
	probe := "tmux -L officraft has-session -t member-m1"

	present := &wardenRunner{}
	got := tmuxHasSession(present, "officraft", "member-m1")
	if got == nil || !*got {
		t.Errorf("a tmux that exits 0 must read present (true), got %v", got)
	}
	if !reflect.DeepEqual(present.calls, []string{probe}) {
		t.Errorf("calls = %v, want %v", present.calls, []string{probe})
	}

	absent := &wardenRunner{fallback: wardenRun{err: errors.New("can't find session: member-m1")}}
	got = tmuxHasSession(absent, "officraft", "member-m1")
	if got == nil || *got {
		t.Errorf("a benign absent error must read POSITIVELY absent (false), got %v", got)
	}

	noServer := &wardenRunner{fallback: wardenRun{err: errors.New("no server running on /tmp/tmux-501/officraft")}}
	if got = tmuxHasSession(noServer, "officraft", "member-m1"); got == nil || *got {
		t.Errorf("no-server must read POSITIVELY absent (false), got %v", got)
	}

	broken := &wardenRunner{fallback: wardenRun{err: errors.New("exec: \"tmux\": executable file not found in $PATH")}}
	if got = tmuxHasSession(broken, "officraft", "member-m1"); got != nil {
		t.Errorf("an unclassifiable probe fault must read UNKNOWN (nil), got %v", *got)
	}

	nsSocket := &wardenRunner{}
	tmuxHasSession(nsSocket, "officraft-eva", "member-m1")
	if want := []string{"tmux -L officraft-eva has-session -t member-m1"}; !reflect.DeepEqual(nsSocket.calls, want) {
		t.Errorf("calls = %v, want %v", nsSocket.calls, want)
	}
}

func TestTmuxPanePID(t *testing.T) {
	display := "tmux -L officraft display-message -p -t member-m1 #{pane_pid}"

	r := &wardenRunner{script: map[string]wardenRun{display: {out: "48213\n"}}}
	if got := tmuxPanePID(r, "officraft", "member-m1"); got != "48213" {
		t.Errorf("pane pid = %q, want %q", got, "48213")
	}
	if !reflect.DeepEqual(r.calls, []string{display}) {
		t.Errorf("calls = %v, want %v", r.calls, []string{display})
	}

	faults := []struct {
		name string
		res  wardenRun
	}{
		{"tmux failed", wardenRun{out: "48213", err: errors.New("can't find session: member-m1")}},
		{"empty output", wardenRun{out: "   \n"}},
		{"non-numeric output", wardenRun{out: "no such pane"}},
		{"negative number", wardenRun{out: "-1"}},
		{"decimal number", wardenRun{out: "48213.0"}},
	}
	for _, f := range faults {
		r := &wardenRunner{script: map[string]wardenRun{display: f.res}}
		if got := tmuxPanePID(r, "officraft", "member-m1"); got != "" {
			t.Errorf("%s: pane pid = %q, want \"\"", f.name, got)
		}
	}
}
