package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stageShell writes an executable stand-in for /bin/zsh whose body is `body`,
// so the capture can be driven without running the owner's real rc files.
func stageShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("stage shell: %v", err)
	}
	return path
}

func TestCaptureInteractiveEnv(t *testing.T) {
	t.Run("the shell's raw stdout comes back byte for byte", func(t *testing.T) {
		shell := stageShell(t, "printf 'EDITOR=vim\\0GREETING=hello world\\0'\n")

		got, err := captureInteractiveEnv(shell, 10*time.Second)

		if err != nil {
			t.Fatalf("captureInteractiveEnv = %v, want the shell's output", err)
		}
		if want := "EDITOR=vim\x00GREETING=hello world\x00"; got != want {
			t.Errorf("raw = %q, want %q", got, want)
		}
	})

	t.Run("the shell is asked for its INTERACTIVE environment", func(t *testing.T) {
		shell := stageShell(t, "printf 'ARGV=%s\\0' \"$*\"\n")

		got, err := captureInteractiveEnv(shell, 10*time.Second)

		if err != nil {
			t.Fatalf("captureInteractiveEnv = %v", err)
		}
		if want := "ARGV=-i -c /usr/bin/env -0\x00"; got != want {
			t.Errorf("argv = %q, want %q", got, want)
		}
	})

	t.Run("rc chatter on stderr is never parsed as environment", func(t *testing.T) {
		shell := stageShell(t, "echo 'nvm: loading' >&2\nprintf 'EDITOR=vim\\0'\n")

		got, err := captureInteractiveEnv(shell, 10*time.Second)

		if err != nil {
			t.Fatalf("captureInteractiveEnv = %v", err)
		}
		if want := "EDITOR=vim\x00"; got != want {
			t.Errorf("raw = %q, want %q — stderr must not be merged into stdout", got, want)
		}
	})

	t.Run("a shell that exits non-zero is an error carrying no captured bytes", func(t *testing.T) {
		shell := stageShell(t, "printf 'EDITOR=vim\\0'\nexit 3\n")

		got, err := captureInteractiveEnv(shell, 10*time.Second)

		if got != "" {
			t.Errorf("raw = %q, want \"\" on failure", got)
		}
		want := fmt.Sprintf("%s -i -c %q failed: exit status 3", shell, "/usr/bin/env -0")
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("a shell that does not exist is an error, never a spawn-killer", func(t *testing.T) {
		shell := filepath.Join(t.TempDir(), "no-such-shell")

		got, err := captureInteractiveEnv(shell, 10*time.Second)

		if got != "" {
			t.Errorf("raw = %q, want \"\"", got)
		}
		if err == nil || !strings.HasPrefix(err.Error(), shell+" -i -c ") {
			t.Errorf("err = %v, want one naming the shell that could not be run", err)
		}
	})

	t.Run("output over the cap is refused rather than carried", func(t *testing.T) {
		shell := stageShell(t, "dd if=/dev/zero bs=1024 count=1100 2>/dev/null | tr '\\0' 'a'\n")

		got, err := captureInteractiveEnv(shell, 30*time.Second)

		if got != "" {
			t.Errorf("raw is %d bytes, want \"\"", len(got))
		}
		want := fmt.Sprintf("output is %d bytes, over the %d cap", 1100*1024, interactiveEnvMaxBytes)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("a hung shell that leaves a grandchild running is bounded, and the grandchild dies with it", func(t *testing.T) {
		dir := t.TempDir()
		marker := filepath.Join(dir, "grandchild-survived")
		shell := stageShell(t, "(sleep 30; touch "+marker+") &\nsleep 30\n")

		started := time.Now()
		got, err := captureInteractiveEnv(shell, 300*time.Millisecond)
		elapsed := time.Since(started)

		if got != "" {
			t.Errorf("raw = %q, want \"\"", got)
		}
		if want := "timed out after 300ms"; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if bound := interactiveEnvWaitDelay + 5*time.Second; elapsed > bound {
			t.Errorf("the capture took %s, want it bounded well under %s — the grandchild held the pipe open", elapsed, bound)
		}
		time.Sleep(time.Second)
		if _, err := os.Stat(marker); err == nil {
			t.Error("the grandchild outlived the capture — the process group was not signalled")
		}
	})
}

func TestParseNulEnv(t *testing.T) {
	t.Run("records arrive in shell order, session-local and OC_* names dropped", func(t *testing.T) {
		raw := strings.Join([]string{
			"EDITOR=vim",
			"PWD=/Users/eva",
			"GREETING=hello world",
			"SHLVL=2",
			"_=/usr/bin/env",
			"TMUX=/private/tmp/tmux-501/default,123,0",
			"TMUX_PANE=%7",
			"TERM=xterm-256color",
			"COLUMNS=203",
			"LINES=51",
			"OLDPWD=/tmp",
			"OC_BASE=https://evil.example",
			"LANG=en_US.UTF-8",
			"EDITOR=emacs",
			"EMPTY=",
			"HAS_EQUALS=a=b=c",
		}, "\x00") + "\x00"
		warn, lines := collectWarnings()

		got := parseNulEnv(raw, warn)

		want := []agentEnvPair{
			{"EDITOR", "emacs"},
			{"GREETING", "hello world"},
			{"LANG", "en_US.UTF-8"},
			{"EMPTY", ""},
			{"HAS_EQUALS", "a=b=c"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{
			"interactive env: skipped OC_BASE — OC_* is warden-reserved (the agent's own identity)",
		}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("malformed records are reported by position and never by content", func(t *testing.T) {
		raw := strings.Join([]string{
			"EDITOR=vim",
			"a-fragment-of-some-credential",
			"=novalue",
			"2BAD=x",
			"LANG=en_US.UTF-8",
		}, "\x00") + "\x00"
		warn, lines := collectWarnings()

		got := parseNulEnv(raw, warn)

		want := []agentEnvPair{{"EDITOR", "vim"}, {"LANG", "en_US.UTF-8"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{
			"interactive env: skipped 3 malformed record(s) at position(s) 2,3,4 — not KEY=value; " +
				"content withheld from this log because an unparseable record may be part of a credential value",
		}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
		for _, line := range *lines {
			if strings.Contains(line, "a-fragment-of-some-credential") {
				t.Errorf("the warning echoed the record content: %q", line)
			}
		}
	})

	t.Run("an empty capture and a nil warn sink are both survivable", func(t *testing.T) {
		warn, lines := collectWarnings()
		if got := parseNulEnv("", warn); got != nil {
			t.Errorf("pairs = %#v, want none", got)
		}
		if len(*lines) != 0 {
			t.Errorf("warnings = %#v, want none", *lines)
		}
		if got := parseNulEnv("EDITOR=vim\x00bad\x00", nil); !reflect.DeepEqual(got, []agentEnvPair{{"EDITOR", "vim"}}) {
			t.Errorf("pairs = %#v, want the one good record", got)
		}
	})
}

func TestJoinInts(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{nil, ""},
		{[]int{}, ""},
		{[]int{7}, "7"},
		{[]int{2, 3, 41}, "2,3,41"},
	}
	for _, c := range cases {
		if got := joinInts(c.in); got != c.want {
			t.Errorf("joinInts(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMergeAgentEnv(t *testing.T) {
	base := []agentEnvPair{
		{"EDITOR", "vim"},
		{"LANG", "en_US.UTF-8"},
		{"GREETING", "from the shell"},
	}
	override := []agentEnvPair{
		{"GREETING", "from the file"},
		{"EDITOR", "emacs"},
		{"ADDED_BY_FILE", "1"},
	}

	got := mergeAgentEnv(base, override)

	want := []agentEnvPair{
		{"EDITOR", "emacs"},
		{"LANG", "en_US.UTF-8"},
		{"GREETING", "from the file"},
		{"ADDED_BY_FILE", "1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %#v, want %#v — overridden names keep their base position", got, want)
	}
	if !reflect.DeepEqual(base, []agentEnvPair{
		{"EDITOR", "vim"}, {"LANG", "en_US.UTF-8"}, {"GREETING", "from the shell"},
	}) {
		t.Errorf("the base layer was mutated: %#v", base)
	}

	if got := mergeAgentEnv(base, nil); !reflect.DeepEqual(got, base) {
		t.Errorf("merge with an empty override = %#v, want the base unchanged %#v", got, base)
	}
	if got := mergeAgentEnv(nil, override); !reflect.DeepEqual(got, override) {
		t.Errorf("merge onto an empty base = %#v, want the override in file order %#v", got, override)
	}
	if got := mergeAgentEnv(nil, nil); len(got) != 0 {
		t.Errorf("merge of two empty layers = %#v, want nothing", got)
	}
}

func TestOverriddenKeyNames(t *testing.T) {
	base := []agentEnvPair{{"EDITOR", "vim"}, {"LANG", "en_US.UTF-8"}, {"GREETING", "hi"}}
	override := []agentEnvPair{{"GREETING", "hello"}, {"EDITOR", "emacs"}, {"ADDED_BY_FILE", "1"}}

	if got, want := overriddenKeyNames(base, override), []string{"EDITOR", "GREETING"}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %#v, want %#v sorted", got, want)
	}
	if got := overriddenKeyNames(base, []agentEnvPair{{"ADDED_BY_FILE", "1"}}); got != nil {
		t.Errorf("names = %#v, want nil when the file overrode nothing", got)
	}
	if got := overriddenKeyNames(nil, override); got != nil {
		t.Errorf("names = %#v, want nil when there is no base layer", got)
	}
}
