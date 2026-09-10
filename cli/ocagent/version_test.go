package main

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

// buildInfoWith returns a provider handing back exactly these settings.
func buildInfoWith(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: settings}, true
	}
}

// noBuildInfo is the provider a binary with no embedded build info gives.
func noBuildInfo() (*debug.BuildInfo, bool) { return nil, false }

// readsBytes returns a reader that hands back these bytes for any path, and
// records which path it was asked for.
func readsBytes(data []byte, asked *string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		*asked = path
		return data, nil
	}
}

func exeIs(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

func TestSelfHash(t *testing.T) {
	t.Run("the running binary's bytes hash to a 12-hex-char prefix", func(t *testing.T) {
		var asked string
		got := selfHash(exeIs("/opt/ocagent"), readsBytes([]byte("hello"), &asked))
		if got != "2cf24dba5fb0" {
			t.Fatalf("selfHash = %q, want %q", got, "2cf24dba5fb0")
		}
		if asked != "/opt/ocagent" {
			t.Fatalf("read %q, want the path os.Executable named", asked)
		}
	})

	t.Run("an empty binary still hashes", func(t *testing.T) {
		var asked string
		if got := selfHash(exeIs("/opt/ocagent"), readsBytes(nil, &asked)); got != "e3b0c44298fc" {
			t.Fatalf("selfHash = %q, want %q", got, "e3b0c44298fc")
		}
	})

	t.Run("an unresolvable executable path degrades to a named reason", func(t *testing.T) {
		read := func(string) ([]byte, error) { t.Fatal("read must not run"); return nil, nil }
		got := selfHash(func() (string, error) { return "", errors.New("no executable") }, read)
		if got != "unavailable: no executable" {
			t.Fatalf("selfHash = %q, want %q", got, "unavailable: no executable")
		}
	})

	t.Run("an unreadable binary degrades to a named reason", func(t *testing.T) {
		got := selfHash(exeIs("/opt/ocagent"), func(string) ([]byte, error) {
			return nil, errors.New("permission denied")
		})
		if got != "unavailable: permission denied" {
			t.Fatalf("selfHash = %q, want %q", got, "unavailable: permission denied")
		}
	})
}

func TestPrintVersion(t *testing.T) {
	var asked string
	cases := []struct {
		name      string
		buildInfo func() (*debug.BuildInfo, bool)
		exe       func() (string, error)
		read      func(string) ([]byte, error)
		want      string
	}{
		{
			name:      "a build with no VCS stamp prints two lines, not three unknowns",
			buildInfo: noBuildInfo,
			exe:       exeIs("/opt/ocagent"),
			read:      readsBytes([]byte("hello"), &asked),
			want: "ocagent\n" +
				"  build.sha:    unstamped (not built by bin/build-bindist)\n" +
				"  self-hash:    2cf24dba5fb0\n",
		},
		{
			name: "a fully stamped build prints all three VCS lines between the two",
			buildInfo: buildInfoWith(
				debug.BuildSetting{Key: "vcs.revision", Value: "d45b94bc0011"},
				debug.BuildSetting{Key: "vcs.time", Value: "2026-09-01T10:00:00Z"},
				debug.BuildSetting{Key: "vcs.modified", Value: "false"},
			),
			exe:  exeIs("/opt/ocagent"),
			read: readsBytes([]byte("hello"), &asked),
			want: "ocagent\n" +
				"  build.sha:    unstamped (not built by bin/build-bindist)\n" +
				"  vcs.revision: d45b94bc0011\n" +
				"  vcs.time:     2026-09-01T10:00:00Z\n" +
				"  vcs.modified: false\n" +
				"  self-hash:    2cf24dba5fb0\n",
		},
		{
			name:      "an unreadable binary still prints the rest of the block",
			buildInfo: noBuildInfo,
			exe:       exeIs("/opt/ocagent"),
			read:      func(string) ([]byte, error) { return nil, errors.New("permission denied") },
			want: "ocagent\n" +
				"  build.sha:    unstamped (not built by bin/build-bindist)\n" +
				"  self-hash:    unavailable: permission denied\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			printVersion(&out, tc.buildInfo, tc.exe, tc.read)
			if out.String() != tc.want {
				t.Fatalf("printVersion wrote\n%q\nwant\n%q", out.String(), tc.want)
			}
		})
	}
}

func TestVcsLines(t *testing.T) {
	cases := []struct {
		name      string
		buildInfo func() (*debug.BuildInfo, bool)
		want      []string
	}{
		{"no build info at all prints nothing", noBuildInfo, nil},
		{"build info with no VCS settings prints nothing",
			buildInfoWith(debug.BuildSetting{Key: "GOARCH", Value: "arm64"}), nil},
		{"all three keys render in revision/time/modified order, whatever order they arrive in",
			buildInfoWith(
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
				debug.BuildSetting{Key: "vcs.time", Value: "2026-09-01T10:00:00Z"},
				debug.BuildSetting{Key: "vcs.revision", Value: "d45b94bc0011"},
			),
			[]string{
				"  vcs.revision: d45b94bc0011",
				"  vcs.time:     2026-09-01T10:00:00Z",
				"  vcs.modified: true",
			}},
		{"a partially stamped build shows only the keys it really has",
			buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: "d45b94bc0011"}),
			[]string{"  vcs.revision: d45b94bc0011"}},
		{"a key present but blank is absent, not an empty field",
			buildInfoWith(
				debug.BuildSetting{Key: "vcs.revision", Value: "   "},
				debug.BuildSetting{Key: "vcs.time", Value: "2026-09-01T10:00:00Z"},
			),
			[]string{"  vcs.time:     2026-09-01T10:00:00Z"}},
		{"a value with surrounding whitespace is trimmed",
			buildInfoWith(debug.BuildSetting{Key: "vcs.revision", Value: "  d45b94bc0011\n"}),
			[]string{"  vcs.revision: d45b94bc0011"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vcsLines(tc.buildInfo)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("vcsLines = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestBuildSHAOrUnstamped(t *testing.T) {
	t.Run("an unstamped build says so even when VCS metadata is sitting right there", func(t *testing.T) {
		got := buildSHAOrUnstamped(buildInfoWith(
			debug.BuildSetting{Key: "vcs.revision", Value: "d45b94bc0011"}))
		if got != "unstamped (not built by bin/build-bindist)" {
			t.Fatalf("buildSHAOrUnstamped = %q, want the unstamped sentence", got)
		}
	})

	t.Run("a link-time stamp is returned trimmed", func(t *testing.T) {
		original := buildSHA
		t.Cleanup(func() { buildSHA = original })
		buildSHA = "  d45b94bc\n"
		if got := buildSHAOrUnstamped(noBuildInfo); got != "d45b94bc" {
			t.Fatalf("buildSHAOrUnstamped = %q, want %q", got, "d45b94bc")
		}
	})

	t.Run("a whitespace-only stamp is no stamp", func(t *testing.T) {
		original := buildSHA
		t.Cleanup(func() { buildSHA = original })
		buildSHA = "   "
		if got := buildSHAOrUnstamped(noBuildInfo); got != "unstamped (not built by bin/build-bindist)" {
			t.Fatalf("buildSHAOrUnstamped = %q, want the unstamped sentence", got)
		}
	})
}

func TestCmdVersion(t *testing.T) {
	var out bytes.Buffer
	if rc := cmdVersion(&out); rc != 0 {
		t.Fatalf("cmdVersion returned %d, want 0", rc)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if lines[0] != "ocagent" {
		t.Fatalf("first line %q, want %q", lines[0], "ocagent")
	}
	if lines[1] != "  build.sha:    unstamped (not built by bin/build-bindist)" {
		t.Fatalf("second line %q, want the unstamped build.sha line", lines[1])
	}
	last := lines[len(lines)-1]
	if !regexp.MustCompile(`^  self-hash:    [0-9a-f]{12}$`).MatchString(last) {
		t.Fatalf("last line %q, want the self-hash of this real binary", last)
	}
}
