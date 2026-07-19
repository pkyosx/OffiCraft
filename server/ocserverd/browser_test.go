package main

// browser_test.go — the first-run browser pop (browser.go): per-GOOS opener
// argv, silent degradation to a printed URL on any open failure, and the
// auto-open gating (TTY + OC_NO_OPEN_BROWSER valve).

import (
	"errors"
	"strings"
	"testing"
)

func recordingRun(calls *[][]string, err error) func(string, ...string) error {
	return func(name string, arg ...string) error {
		*calls = append(*calls, append([]string{name}, arg...))
		return err
	}
}

func TestBrowserOpenerArgvPerGOOS(t *testing.T) {
	const url = "http://127.0.0.1:8765/?code=tok"
	cases := []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open", url}},
		{"linux", []string{"xdg-open", url}},
	}
	for _, c := range cases {
		var calls [][]string
		b := browserOpener{goos: c.goos, run: recordingRun(&calls, nil)}
		if err := b.open(url); err != nil {
			t.Fatalf("%s: unexpected error: %v", c.goos, err)
		}
		if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(c.want, " ") {
			t.Fatalf("%s: argv = %v, want %v", c.goos, calls, c.want)
		}
	}
}

func TestBrowserOpenerUnsupportedGOOSErrorsWithoutRunning(t *testing.T) {
	var calls [][]string
	b := browserOpener{goos: "windows", run: recordingRun(&calls, nil)}
	if err := b.open("http://x/"); err == nil {
		t.Fatal("want error for unsupported GOOS")
	}
	if len(calls) != 0 {
		t.Fatalf("run must not be called on unsupported GOOS, got %v", calls)
	}
}

func TestPopFirstRunBrowserFailureFallsBackToURL(t *testing.T) {
	const url = "http://127.0.0.1:8765/?code=tok"
	var out strings.Builder
	b := browserOpener{goos: "linux", run: recordingRun(new([][]string), errors.New("no display"))}
	popFirstRunBrowser(b, url, &out)
	if !strings.Contains(out.String(), url) {
		t.Fatalf("failed open must print the full setup URL, got %q", out.String())
	}
}

func TestPopFirstRunBrowserSuccessPrintsNoToken(t *testing.T) {
	const url = "http://127.0.0.1:8765/?code=tok"
	var out strings.Builder
	b := browserOpener{goos: "darwin", run: recordingRun(new([][]string), nil)}
	popFirstRunBrowser(b, url, &out)
	if strings.Contains(out.String(), "tok") {
		t.Fatalf("successful open must not print the claim code, got %q", out.String())
	}
	if !strings.Contains(out.String(), "opened the setup page") {
		t.Fatalf("successful open must confirm on the log, got %q", out.String())
	}
}

func TestShouldAutoOpenBrowser(t *testing.T) {
	envWith := func(v string) func(string) string {
		return func(k string) string {
			if k == "OC_NO_OPEN_BROWSER" {
				return v
			}
			return ""
		}
	}
	if !shouldAutoOpenBrowser(envWith(""), true) {
		t.Fatal("TTY + no opt-out must auto-open")
	}
	if shouldAutoOpenBrowser(envWith("1"), true) {
		t.Fatal("OC_NO_OPEN_BROWSER set must suppress the pop")
	}
	if shouldAutoOpenBrowser(envWith(""), false) {
		t.Fatal("non-TTY stdout must suppress the pop")
	}
}

func TestFirstRunSetupURL(t *testing.T) {
	got := firstRunSetupURL("127.0.0.1:8765", "abc_DEF-123")
	if got != "http://127.0.0.1:8765/?code=abc_DEF-123" {
		t.Fatalf("setup URL = %q", got)
	}
}
