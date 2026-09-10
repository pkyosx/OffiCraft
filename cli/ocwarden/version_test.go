package main

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

func TestSelfHash(t *testing.T) {
	path := stageBinary(t, t.TempDir()+"/ocwarden", "warden-bytes-v1")

	if got := selfHash(func() (string, error) { return path, nil }, os.ReadFile); got != "9c8d3cb7cabb" {
		t.Errorf("selfHash = %q, want %q", got, "9c8d3cb7cabb")
	}

	unnameable := func() (string, error) { return "", errors.New("executable path unknown") }
	if got := selfHash(unnameable, os.ReadFile); got != "unavailable: executable path unknown" {
		t.Errorf("selfHash = %q, want %q", got, "unavailable: executable path unknown")
	}

	unreadable := func(string) ([]byte, error) { return nil, errors.New("permission denied") }
	if got := selfHash(func() (string, error) { return path, nil }, unreadable); got != "unavailable: permission denied" {
		t.Errorf("selfHash = %q, want %q", got, "unavailable: permission denied")
	}
}

func TestPrintVersion(t *testing.T) {
	path := stageBinary(t, t.TempDir()+"/ocwarden", "warden-bytes-v1")
	exe := func() (string, error) { return path, nil }

	stamped := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "-ldflags", Value: "-s -w"},
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "80eec5fe0f1c4a2b8d3e5f60718293a4b5c6d7e8"},
			{Key: "vcs.time", Value: "2026-09-07T03:38:00Z"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}

	cases := []struct {
		name      string
		buildInfo func() (*debug.BuildInfo, bool)
		read      func(string) ([]byte, error)
		want      string
	}{
		{
			name:      "a git-directory build prints its stamp beside the content hash",
			buildInfo: stamped,
			read:      os.ReadFile,
			want: "ocwarden\n" +
				"  vcs.revision: 80eec5fe0f1c4a2b8d3e5f60718293a4b5c6d7e8\n" +
				"  vcs.time:     2026-09-07T03:38:00Z\n" +
				"  vcs.modified: false\n" +
				"  self-hash:    9c8d3cb7cabb\n",
		},
		{
			name:      "no build info at all still identifies the build by its bytes",
			buildInfo: func() (*debug.BuildInfo, bool) { return nil, false },
			read:      os.ReadFile,
			want: "ocwarden\n" +
				"  vcs.revision: unknown\n" +
				"  vcs.time:     unknown\n" +
				"  vcs.modified: unknown\n" +
				"  self-hash:    9c8d3cb7cabb\n",
		},
		{
			name: "a worktree build carries build info with no VCS settings",
			buildInfo: func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "GOARCH", Value: "arm64"}}}, true
			},
			read: os.ReadFile,
			want: "ocwarden\n" +
				"  vcs.revision: unknown\n" +
				"  vcs.time:     unknown\n" +
				"  vcs.modified: unknown\n" +
				"  self-hash:    9c8d3cb7cabb\n",
		},
		{
			name:      "an unreadable executable never suppresses the VCS half",
			buildInfo: stamped,
			read:      func(string) ([]byte, error) { return nil, errors.New("permission denied") },
			want: "ocwarden\n" +
				"  vcs.revision: 80eec5fe0f1c4a2b8d3e5f60718293a4b5c6d7e8\n" +
				"  vcs.time:     2026-09-07T03:38:00Z\n" +
				"  vcs.modified: false\n" +
				"  self-hash:    unavailable: permission denied\n",
		},
	}

	for _, c := range cases {
		var out bytes.Buffer
		printVersion(&out, c.buildInfo, exe, c.read)
		if out.String() != c.want {
			t.Errorf("%s: printVersion wrote\n%s\nwant\n%s", c.name, out.String(), c.want)
		}
	}
}

func TestCmdVersion(t *testing.T) {
	var out bytes.Buffer
	if code := cmdVersion(&out); code != 0 {
		t.Errorf("cmdVersion = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 5 || lines[0] != "ocwarden" {
		t.Fatalf("cmdVersion wrote\n%s", out.String())
	}
	if !regexp.MustCompile(`^  self-hash:    [0-9a-f]{12}$`).MatchString(lines[4]) {
		t.Errorf("the real providers must hash the running binary: %q", lines[4])
	}
}
