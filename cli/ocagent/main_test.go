package main

import (
	"bytes"
	"strings"
	"testing"
)

// wantUsage is the synopsis both zero-configuration surfaces print, byte for byte.
const wantUsage = `usage: ocagent <subcommand> [flags]
  officraft agent-runtime (Plane A) thin shell.

subcommands:
  listen          hold the SSE downlink: chat (refetch) + work wakes
  context-report  statusLine reporter: stdin statusLine JSON → POST /api/agent/context
  suicide         self-terminate: kill my own tmux session (OC_SESSION) → SSE drops → offline
  download        fetch a chat attachment blob to a local file (streaming; --out <dir>)
  upload          stream a local file into the attachment store (prints the att id; --mime <type>)
  diff            print a compare-screen URL for two attachment ids / document versions (--external mints a no-login link)
  clean           get rid of a file or folder I made: quarantines it under my workdir (never rm)
  version         print this build's identity: build.sha, VCS stamp when present, self-hash
`

func TestUsage(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	if out.String() != wantUsage {
		t.Errorf("usage printed\n%s\nwant\n%s", out.String(), wantUsage)
	}
}

func TestRealMain(t *testing.T) {
	t.Run("a launch that named nothing prints the synopsis and exits 2", func(t *testing.T) {
		var out bytes.Buffer
		if rc := realMain(nil, testEnv(nil), strings.NewReader(""), &out); rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		if out.String() != wantUsage {
			t.Errorf("printed\n%s\nwant\n%s", out.String(), wantUsage)
		}
	})

	for _, name := range []string{"help", "--help", "-h"} {
		t.Run("`"+name+"` prints the synopsis and exits 0", func(t *testing.T) {
			var out bytes.Buffer
			if rc := realMain([]string{name}, testEnv(nil), strings.NewReader(""), &out); rc != 0 {
				t.Errorf("rc = %d, want 0 — asking is not an error", rc)
			}
			if out.String() != wantUsage {
				t.Errorf("printed\n%s\nwant\n%s", out.String(), wantUsage)
			}
		})
	}

	t.Run("an unknown subcommand names itself before the synopsis", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"roster"}, testEnv(nil), strings.NewReader(""), &out)
		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		want := "[ocagent] unknown subcommand \"roster\"\n\n" + wantUsage
		if out.String() != want {
			t.Errorf("printed\n%s\nwant\n%s", out.String(), want)
		}
	})

	t.Run("download without an id refuses with its own one-line usage", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"download"}, testEnv(nil), strings.NewReader(""), &out)
		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		want := "[ocagent] download: exactly one <attachment-id> argument is required\n" +
			"usage: ocagent download <attachment-id> [--out <dir>]\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("upload without a path refuses with its own one-line usage", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"upload"}, testEnv(nil), strings.NewReader(""), &out)
		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		want := "[ocagent] upload: exactly one <path> argument is required\n" +
			"usage: ocagent upload <path> [--mime <type>]\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})

	t.Run("an unparseable flag exits 2 without running the subcommand", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"context-report", "--bogus"}, testEnv(nil),
			strings.NewReader(`{"context_window":{"used_percentage":40}}`), &out)
		if rc != 2 {
			t.Errorf("rc = %d, want 2", rc)
		}
		want := "flag provided but not defined: -bogus\nUsage of ocagent context-report:\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q — the status line must not be printed when the "+
				"launch itself was rejected", out.String(), want)
		}
	})

	t.Run("context-report renders the status line and exits 0 with no station wired", func(t *testing.T) {
		var out bytes.Buffer
		rc := realMain([]string{"context-report"}, testEnv(map[string]string{"OC_HOME": t.TempDir()}),
			strings.NewReader(`{"context_window":{"used_percentage":40},"cost":{"total_cost_usd":2}}`),
			&out)
		if rc != 0 {
			t.Errorf("rc = %d, want 0 — a mis-wired agent's status line must keep working", rc)
		}
		want := "\x1b[90m████░░░░░░\x1b[0m \x1b[32m40%\x1b[0m\x1b[90m | \x1b[0m\x1b[33m$2.00\x1b[0m\n"
		if out.String() != want {
			t.Errorf("printed %q, want %q", out.String(), want)
		}
	})
}
