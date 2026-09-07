package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// recordingTmux writes an executable that appends its own argv, one per line, to
// the file named by $OCAGENT_TEST_ARGV, then exits with exitCode.
func recordingTmux(t *testing.T, exitCode int) (bin, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "tmux")
	argvFile = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> \"$OCAGENT_TEST_ARGV\"; done\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCAGENT_TEST_ARGV", argvFile)
	return bin, argvFile
}

// killSpy records every kill it is asked to perform and answers with err.
type killSpy struct {
	calls [][]string
	err   error
}

func (k *killSpy) kill(bin, socket, session string) error {
	k.calls = append(k.calls, []string{bin, socket, session})
	return k.err
}

func TestRealTmuxKill(t *testing.T) {
	t.Run("it runs `<bin> -L <socket> kill-session -t <session>`", func(t *testing.T) {
		bin, argvFile := recordingTmux(t, 0)
		if err := realTmuxKill(bin, "officraft", "member-alice"); err != nil {
			t.Fatalf("realTmuxKill returned %v, want nil", err)
		}
		raw, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatal(err)
		}
		want := "-L\nofficraft\nkill-session\n-t\nmember-alice\n"
		if string(raw) != want {
			t.Fatalf("argv %q, want %q", string(raw), want)
		}
	})

	t.Run("a tmux that refuses the kill returns its failure", func(t *testing.T) {
		bin, _ := recordingTmux(t, 1)
		err := realTmuxKill(bin, "officraft", "member-alice")
		if err == nil || err.Error() != "exit status 1" {
			t.Fatalf("realTmuxKill returned %v, want exit status 1", err)
		}
	})

	t.Run("an unrunnable binary returns its failure rather than pretending", func(t *testing.T) {
		if err := realTmuxKill(filepath.Join(t.TempDir(), "absent"), "officraft", "member-alice"); err == nil {
			t.Fatal("realTmuxKill returned nil for a binary that does not exist")
		}
	})
}

func TestSuicideSession(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		wantSocket  string
		wantSession string
		wantOK      bool
	}{
		{"OC_SESSION alone takes the default socket",
			map[string]string{"OC_SESSION": "member-alice"}, "officraft", "member-alice", true},
		{"OC_TMUX_SOCKET overrides the default socket",
			map[string]string{"OC_SESSION": "member-alice", "OC_TMUX_SOCKET": "lab"},
			"lab", "member-alice", true},
		{"both values are trimmed",
			map[string]string{"OC_SESSION": "  member-alice \n", "OC_TMUX_SOCKET": " lab "},
			"lab", "member-alice", true},
		{"a blank OC_TMUX_SOCKET falls back to the default socket",
			map[string]string{"OC_SESSION": "member-alice", "OC_TMUX_SOCKET": "   "},
			"officraft", "member-alice", true},
		{"no OC_SESSION means there is nothing to kill",
			map[string]string{"OC_TMUX_SOCKET": "lab"}, "", "", false},
		{"a whitespace-only OC_SESSION is no session",
			map[string]string{"OC_SESSION": "  "}, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			socket, session, ok := suicideSession(testEnv(tc.env))
			if socket != tc.wantSocket || session != tc.wantSession || ok != tc.wantOK {
				t.Fatalf("suicideSession = (%q, %q, %v), want (%q, %q, %v)",
					socket, session, ok, tc.wantSocket, tc.wantSession, tc.wantOK)
			}
		})
	}
}

func TestRunSuicide(t *testing.T) {
	resolvesTo := func(bin string) func() string { return func() string { return bin } }

	t.Run("a launched agent kills its own session on the socket it was given", func(t *testing.T) {
		var out bytes.Buffer
		spy := &killSpy{}
		rc := runSuicide(testEnv(map[string]string{"OC_SESSION": "member-alice", "OC_TMUX_SOCKET": "lab"}),
			&out, resolvesTo("/opt/homebrew/bin/tmux"), spy.kill)
		if rc != 0 {
			t.Fatalf("runSuicide returned %d, want 0", rc)
		}
		want := "[ocagent] suicide: tmux -L lab kill-session -t member-alice — dropping my SSE " +
			"so the server derives offline before the grace deadline.\n"
		if out.String() != want {
			t.Fatalf("stdout %q, want %q", out.String(), want)
		}
		wantCalls := [][]string{{"/opt/homebrew/bin/tmux", "lab", "member-alice"}}
		if !reflect.DeepEqual(spy.calls, wantCalls) {
			t.Fatalf("kills %v, want %v", spy.calls, wantCalls)
		}
	})

	t.Run("a headless run with no OC_SESSION kills nothing and still exits 0", func(t *testing.T) {
		var out bytes.Buffer
		spy := &killSpy{}
		resolver := func() string { t.Fatal("tmux must not be resolved with no session"); return "" }
		rc := runSuicide(testEnv(nil), &out, resolver, spy.kill)
		want := "[ocagent] suicide: no OC_SESSION — nothing to kill; exiting.\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
		if len(spy.calls) != 0 {
			t.Fatalf("kills %v, want none", spy.calls)
		}
	})

	t.Run("an unresolvable tmux kills nothing and leaves the warden as the fallback", func(t *testing.T) {
		var out bytes.Buffer
		spy := &killSpy{}
		rc := runSuicide(testEnv(map[string]string{"OC_SESSION": "member-alice"}),
			&out, resolvesTo(""), spy.kill)
		want := "[ocagent] suicide: tmux unresolvable — cannot self-kill; " +
			"leaving the warden killpg as the fallback.\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
		if len(spy.calls) != 0 {
			t.Fatalf("kills %v, want none", spy.calls)
		}
	})

	t.Run("a kill that returns is reported honestly and still exits 0", func(t *testing.T) {
		var out bytes.Buffer
		spy := &killSpy{err: errors.New("exit status 1")}
		rc := runSuicide(testEnv(map[string]string{"OC_SESSION": "member-alice"}),
			&out, resolvesTo("/usr/bin/tmux"), spy.kill)
		want := "[ocagent] suicide: tmux -L officraft kill-session -t member-alice — dropping my SSE " +
			"so the server derives offline before the grace deadline.\n" +
			"[ocagent] suicide: kill-session returned exit status 1 " +
			"(session likely already gone) — the warden killpg remains the fallback.\n"
		if rc != 0 || out.String() != want {
			t.Fatalf("got (%d, %q), want (0, %q)", rc, out.String(), want)
		}
	})
}
