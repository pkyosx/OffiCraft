package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// collectWarnings returns a warn sink and the slice it appends formatted lines to.
func collectWarnings() (func(string, ...any), *[]string) {
	var lines []string
	return func(format string, a ...any) { lines = append(lines, fmt.Sprintf(format, a...)) }, &lines
}

const quotesDoNotWrapWarning = "value kept LITERALLY including the quote characters — the quotes do not wrap the whole value " +
	"(there is trailing text after the closing quote); quote the WHOLE value, e.g. KEY=\"a x\""

const unquotedCommentWarning = "value kept LITERALLY including the trailing '#...' — this file has no end-of-line comments on unquoted values; " +
	"write KEY=\"value\" # comment if you meant a comment, or ignore this if '#' is part of the value"

func TestParseAgentEnv(t *testing.T) {
	t.Run("the whole documented format in one file", func(t *testing.T) {
		raw := strings.Join([]string{
			"# a comment",
			"",
			"   ",
			"EDITOR=vim",
			"export LANG=en_US.UTF-8",
			`GREETING="hello world"`,
			"GOFLAGS='-mod=readonly'",
			"HOMEBREW_PREFIX=/opt/homebrew",
			"LITERAL=$HOME/bin",
			"\tINDENTED=yes\t",
			"WITH_CR=crlf\r",
			"EDITOR=emacs",
		}, "\n")
		warn, lines := collectWarnings()

		got := parseAgentEnv(raw, "/Users/eva/.officraft/env", warn)

		want := []agentEnvPair{
			{"EDITOR", "emacs"},
			{"LANG", "en_US.UTF-8"},
			{"GREETING", "hello world"},
			{"GOFLAGS", "-mod=readonly"},
			{"HOMEBREW_PREFIX", "/opt/homebrew"},
			{"LITERAL", "$HOME/bin"},
			{"INDENTED", "yes"},
			{"WITH_CR", "crlf"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{
			"agent env: /Users/eva/.officraft/env:12 EDITOR redefined — later value wins",
		}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("every skipped line says why and costs the file nothing else", func(t *testing.T) {
		raw := strings.Join([]string{
			"KEEP_ME=1",
			"no equals sign here",
			"=novalue",
			"2BAD=x",
			"has space=x",
			"OC_TOKEN=stolen",
			"OC_ANYTHING=x",
			"WITH_NUL=a\x00b",
			"ALSO_KEPT=2",
		}, "\n")
		warn, lines := collectWarnings()

		got := parseAgentEnv(raw, "/env", warn)

		want := []agentEnvPair{{"KEEP_ME", "1"}, {"ALSO_KEPT", "2"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{
			"agent env: /env:2 skipped — not KEY=value (this file holds data only, never shell code)",
			"agent env: /env:3 skipped — not KEY=value (this file holds data only, never shell code)",
			`agent env: /env:4 skipped — "2BAD" is not a valid variable name`,
			`agent env: /env:5 skipped — "has space" is not a valid variable name`,
			"agent env: /env:6 skipped — OC_TOKEN is warden-reserved (OC_* is the agent's own identity)",
			"agent env: /env:7 skipped — OC_ANYTHING is warden-reserved (OC_* is the agent's own identity)",
			"agent env: /env:8 skipped — WITH_NUL contains a NUL byte, which cannot survive into the agent's environment",
		}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("a multi-assignment export line yields the first key and keeps the rest as its value", func(t *testing.T) {
		warn, lines := collectWarnings()
		got := parseAgentEnv("export A=1 B=2", "/env", warn)

		if want := []agentEnvPair{{"A", "1 B=2"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		if len(*lines) != 0 {
			t.Errorf("warnings = %#v, want none", *lines)
		}
	})

	t.Run("a PATH assignment is accepted and announced as a replacement", func(t *testing.T) {
		warn, lines := collectWarnings()
		got := parseAgentEnv("PATH=/opt/homebrew/sbin", "/env", warn)

		want := []agentEnvPair{{"PATH", "/opt/homebrew/sbin"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{"agent env: /env:1 PATH REPLACES the whole search path, it does not append to it — " +
			"this value drops /bin, /usr/bin and everything else from the agent's PATH. " +
			"$PATH is NOT expanded in this file, so `PATH=/x:$PATH` does not work either. " +
			"Write every directory you need explicitly, e.g. PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("an ambiguous value warns under its own key and line", func(t *testing.T) {
		warn, lines := collectWarnings()
		got := parseAgentEnv("EDITOR=vim # my editor", "/env", warn)

		want := []agentEnvPair{{"EDITOR", "vim # my editor"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		wantWarn := []string{"agent env: /env:1 EDITOR — " + unquotedCommentWarning}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("an empty file yields no pairs and no warnings", func(t *testing.T) {
		warn, lines := collectWarnings()
		if got := parseAgentEnv("", "/env", warn); got != nil {
			t.Errorf("pairs = %#v, want none", got)
		}
		if len(*lines) != 0 {
			t.Errorf("warnings = %#v, want none", *lines)
		}
	})
}

func TestParseAgentEnvValue(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		want     string
		wantWarn string
	}{
		{"a bare value is trimmed", "  vim  ", "vim", ""},
		{"an empty right-hand side is the empty value", "", "", ""},
		{"whitespace only is the empty value", "   ", "", ""},
		{"a double-quoted value loses one quote pair", `"hello world"`, "hello world", ""},
		{"a single-quoted value loses one quote pair", `'hello world'`, "hello world", ""},
		{"quotes are literal, never shell-interpreted", `"$HOME and 'x'"`, "$HOME and 'x'", ""},
		{"a quoted value followed by a comment states its own boundary", `"abc" # note`, "abc", ""},
		{"a single-quoted value followed by a comment does too", `'abc'  # note`, "abc", ""},
		{"quotes that do not wrap the whole value are kept literally", `"abc" def`, `"abc" def`, quotesDoNotWrapWarning},
		{"an unquoted trailing comment is kept literally and announced", "abc # note", "abc # note", unquotedCommentWarning},
		{"a value that is only a comment after a space is kept and announced", " # note", "# note", unquotedCommentWarning},
		{"a '#' with no preceding space is an ordinary character", "#note", "#note", ""},
		{"a '#' inside a password is not a comment", "p@ss#word", "p@ss#word", ""},
		{"a quoted value containing ' #' is not a comment", `"p@ss #word"`, "p@ss #word", ""},
		{"a lone quote character is kept", `"`, `"`, ""},
		{"an unterminated quote is kept", `"abc`, `"abc`, ""},
		{"an empty quoted value is the empty value", `""`, "", ""},
	}
	for _, c := range cases {
		got, gotWarn := parseAgentEnvValue(c.raw)
		if got != c.want || gotWarn != c.wantWarn {
			t.Errorf("%s: parseAgentEnvValue(%q) = (%q, %q), want (%q, %q)",
				c.name, c.raw, got, gotWarn, c.want, c.wantWarn)
		}
	}
}

func TestLoadAgentEnv(t *testing.T) {
	t.Run("a 0600 file is read, parsed and reported by name only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "env")
		if err := os.WriteFile(path, []byte("EDITOR=vim\nLANG=en_US.UTF-8\n"), 0o600); err != nil {
			t.Fatalf("stage: %v", err)
		}
		warn, lines := collectWarnings()

		got := loadAgentEnv(path, warn)

		want := []agentEnvPair{{"EDITOR", "vim"}, {"LANG", "en_US.UTF-8"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
		if len(*lines) != 0 {
			t.Errorf("warnings = %#v, want none for a well-formed 0600 file", *lines)
		}
	})

	t.Run("a mode wider than 0600 warns but still loads", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "env")
		if err := os.WriteFile(path, []byte("EDITOR=vim\n"), 0o644); err != nil {
			t.Fatalf("stage: %v", err)
		}
		warn, lines := collectWarnings()

		got := loadAgentEnv(path, warn)

		if want := []agentEnvPair{{"EDITOR", "vim"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v — a permissions nit must not cost the agent its variables", got, want)
		}
		wantWarn := []string{fmt.Sprintf(
			"agent env: WARNING %s mode is 0644, wider than 0600 — it holds credentials; run: chmod 600 %s", path, path)}
		if !reflect.DeepEqual(*lines, wantWarn) {
			t.Errorf("warnings = %#v, want %#v", *lines, wantWarn)
		}
	})

	t.Run("every degraded input yields nil pairs and one explanation", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "absent")
		asDir := filepath.Join(dir, "a-directory")
		if err := os.Mkdir(asDir, 0o755); err != nil {
			t.Fatalf("stage: %v", err)
		}
		oversized := filepath.Join(dir, "oversized")
		big := append([]byte("EDITOR=vim\n"), make([]byte, agentEnvMaxBytes)...)
		if err := os.WriteFile(oversized, big, 0o600); err != nil {
			t.Fatalf("stage: %v", err)
		}

		cases := []struct {
			name string
			path string
			want []string
		}{
			{"no path configured", "", nil},
			{"an absent file", missing, []string{
				fmt.Sprintf("agent env: no env file at %s; spawning without extra env", missing)}},
			{"a directory", asDir, []string{
				fmt.Sprintf("agent env: %s is a directory, not a file; spawning without extra env", asDir)}},
			{"an oversized file", oversized, []string{
				fmt.Sprintf("agent env: %s is %d bytes, over the %d cap; spawning without extra env",
					oversized, len(big), agentEnvMaxBytes)}},
		}
		for _, c := range cases {
			warn, lines := collectWarnings()
			if got := loadAgentEnv(c.path, warn); got != nil {
				t.Errorf("%s: pairs = %#v, want nil", c.name, got)
			}
			if !reflect.DeepEqual(*lines, c.want) {
				t.Errorf("%s: warnings = %#v, want %#v", c.name, *lines, c.want)
			}
		}
	})

	t.Run("a nil log sink is not a crash", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "env")
		if err := os.WriteFile(path, []byte("EDITOR=vim\nOC_BASE=x\n"), 0o644); err != nil {
			t.Fatalf("stage: %v", err)
		}
		if got, want := loadAgentEnv(path, nil), []agentEnvPair{{"EDITOR", "vim"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %#v, want %#v", got, want)
		}
	})
}

func TestRenderAgentEnvFile(t *testing.T) {
	pairs := []agentEnvPair{
		{"EDITOR", "vim"},
		{"GREETING", "hello world"},
		{"QUOTED", "it's here"},
		{"EMPTY", ""},
		{"PATH", "/opt/homebrew/bin:/usr/bin:/bin"},
		{"SMUGGLED", "x; rm -rf /"},
	}
	want := "# rendered by ocwarden from the agent env file — do not edit\n" +
		"export EDITOR=vim\n" +
		"export GREETING='hello world'\n" +
		"export QUOTED='it'\"'\"'s here'\n" +
		"export EMPTY=''\n" +
		"export PATH=/opt/homebrew/bin:/usr/bin:/bin\n" +
		"export SMUGGLED='x; rm -rf /'\n"
	if got := renderAgentEnvFile(pairs); got != want {
		t.Errorf("render =\n%q\nwant\n%q", got, want)
	}
	if got := renderAgentEnvFile(nil); got != "" {
		t.Errorf("render(nil) = %q, want \"\" so the caller writes no file at all", got)
	}
	if got := renderAgentEnvFile([]agentEnvPair{}); got != "" {
		t.Errorf("render(empty) = %q, want \"\"", got)
	}
}

func TestAgentEnvKeyNames(t *testing.T) {
	pairs := []agentEnvPair{{"EDITOR", "vim"}, {"GREETING", "hello world"}, {"LANG", "en_US.UTF-8"}}
	want := []string{"EDITOR", "GREETING", "LANG"}
	if got := agentEnvKeyNames(pairs); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %#v, want %#v in file order", got, want)
	}
	if got := agentEnvKeyNames(nil); got != nil {
		t.Errorf("names(nil) = %#v, want nil", got)
	}
}
