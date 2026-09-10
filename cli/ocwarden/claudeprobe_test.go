package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// probeFileInfo is the stat seam's answer: only size, mtime and dir-ness are
// ever read by the prober.
type probeFileInfo struct {
	size  int64
	mtime time.Time
	dir   bool
}

func (f probeFileInfo) Name() string       { return "claude" }
func (f probeFileInfo) Size() int64        { return f.size }
func (f probeFileInfo) Mode() fs.FileMode  { return 0o755 }
func (f probeFileInfo) ModTime() time.Time { return f.mtime }
func (f probeFileInfo) IsDir() bool        { return f.dir }
func (f probeFileInfo) Sys() any           { return nil }

// probeSeams records everything the prober asked its seams for.
type probeSeams struct {
	statPaths []string
	readPaths []string
	runCalls  [][]string
}

func TestNewClaudeProber(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("stage claude binary: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("stage cred dir: %v", err)
	}
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"claudeAiOauth":{"subscriptionType":"max"}}`), 0o600); err != nil {
		t.Fatalf("stage credentials: %v", err)
	}
	env := credEnvFunc(map[string]string{"HOME": home, "OC_CLAUDE_BIN": bin})
	runner := fakeRunner{out: map[string]string{bin + " --version": "2.1.211 (Claude Code)\n"}}

	prober := newClaudeProber(env, runner, "linux")

	want := map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true}
	if got := prober.collect(); !reflect.DeepEqual(got, want) {
		t.Errorf("collect() = %v, want %v", got, want)
	}

	if err := os.Remove(credPath); err != nil {
		t.Fatalf("remove credentials: %v", err)
	}
	prober.cached = nil
	wantAfter := map[string]any{"version": "2.1.211", "cred_file": false, "sub_readable": false}
	if got := prober.collect(); !reflect.DeepEqual(got, wantAfter) {
		t.Errorf("collect() after logout = %v, want %v", got, wantAfter)
	}
}

func TestClaudeProberCollect(t *testing.T) {
	const credJSON = `{"claudeAiOauth":{"subscriptionType":"max"}}`
	fixed := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	newProber := func(seams *probeSeams, home, goos string, files map[string]string, keychain error) *claudeProber {
		return &claudeProber{
			env:        credEnvFunc(map[string]string{"HOME": home}),
			resolveBin: func() string { return "/usr/local/bin/claude" },
			stat: func(path string) (os.FileInfo, error) {
				seams.statPaths = append(seams.statPaths, path)
				body, ok := files[path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return probeFileInfo{size: int64(len(body)), mtime: fixed}, nil
			},
			readFile: func(path string) ([]byte, error) {
				seams.readPaths = append(seams.readPaths, path)
				body, ok := files[path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return []byte(body), nil
			},
			runner: runnerFunc(func(name string, args ...string) (string, error) {
				seams.runCalls = append(seams.runCalls, append([]string{name}, args...))
				if name == "security" {
					return "", keychain
				}
				return "2.1.211 (Claude Code)", nil
			}),
			goos: goos,
			now:  func() time.Time { return fixed },
		}
	}

	cases := []struct {
		name      string
		home      string
		goos      string
		files     map[string]string
		keychain  error
		want      map[string]any
		wantStats []string
		wantReads []string
		wantRuns  [][]string
	}{
		{
			name: "darwin, logged in through the credentials file",
			home: "/Users/seth",
			goos: "darwin",
			files: map[string]string{
				"/usr/local/bin/claude":                 "binary",
				"/Users/seth/.claude/.credentials.json": credJSON,
			},
			want: map[string]any{
				"version": "2.1.211", "cred_file": true, "sub_readable": true, "keychain": true,
			},
			wantStats: []string{"/usr/local/bin/claude", "/Users/seth/.claude/.credentials.json"},
			wantReads: []string{"/Users/seth/.claude/.credentials.json"},
			wantRuns: [][]string{
				{"/usr/local/bin/claude", "--version"},
				{"security", "find-generic-password", "-s", "Claude Code-credentials"},
			},
		},
		{
			name:     "darwin, credentials live in the keychain instead of the file",
			home:     "/Users/seth",
			goos:     "darwin",
			files:    map[string]string{"/usr/local/bin/claude": "binary"},
			keychain: nil,
			want: map[string]any{
				"version": "2.1.211", "cred_file": false, "sub_readable": false, "keychain": true,
			},
			wantStats: []string{"/usr/local/bin/claude", "/Users/seth/.claude/.credentials.json"},
			wantRuns: [][]string{
				{"/usr/local/bin/claude", "--version"},
				{"security", "find-generic-password", "-s", "Claude Code-credentials"},
			},
		},
		{
			name:     "darwin with no keychain item",
			home:     "/Users/seth",
			goos:     "darwin",
			files:    map[string]string{"/usr/local/bin/claude": "binary"},
			keychain: errors.New("exit status 44"),
			want: map[string]any{
				"version": "2.1.211", "cred_file": false, "sub_readable": false, "keychain": false,
			},
			wantStats: []string{"/usr/local/bin/claude", "/Users/seth/.claude/.credentials.json"},
			wantRuns: [][]string{
				{"/usr/local/bin/claude", "--version"},
				{"security", "find-generic-password", "-s", "Claude Code-credentials"},
			},
		},
		{
			name: "linux never reports a keychain key at all",
			home: "/home/seth",
			goos: "linux",
			files: map[string]string{
				"/usr/local/bin/claude":                "binary",
				"/home/seth/.claude/.credentials.json": credJSON,
			},
			want:      map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true},
			wantStats: []string{"/usr/local/bin/claude", "/home/seth/.claude/.credentials.json"},
			wantReads: []string{"/home/seth/.claude/.credentials.json"},
			wantRuns:  [][]string{{"/usr/local/bin/claude", "--version"}},
		},
		{
			name: "an unreadable subscriptionType leaves the file key true",
			home: "/home/seth",
			goos: "linux",
			files: map[string]string{
				"/usr/local/bin/claude":                "binary",
				"/home/seth/.claude/.credentials.json": `{"claudeAiOauth":{}}`,
			},
			want:      map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": false},
			wantStats: []string{"/usr/local/bin/claude", "/home/seth/.claude/.credentials.json"},
			wantReads: []string{"/home/seth/.claude/.credentials.json"},
			wantRuns:  [][]string{{"/usr/local/bin/claude", "--version"}},
		},
		{
			name:      "no HOME reports the version alone",
			home:      "",
			goos:      "linux",
			files:     map[string]string{"/usr/local/bin/claude": "binary"},
			want:      map[string]any{"version": "2.1.211"},
			wantStats: []string{"/usr/local/bin/claude"},
			wantRuns:  [][]string{{"/usr/local/bin/claude", "--version"}},
		},
		{
			name:      "an unstattable binary omits the version rather than guessing",
			home:      "/home/seth",
			goos:      "linux",
			files:     map[string]string{},
			want:      map[string]any{"cred_file": false, "sub_readable": false},
			wantStats: []string{"/usr/local/bin/claude", "/home/seth/.claude/.credentials.json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seams := &probeSeams{}
			prober := newProber(seams, tc.home, tc.goos, tc.files, tc.keychain)

			got := prober.collect()

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("collect() = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(seams.statPaths, tc.wantStats) {
				t.Errorf("stat probed %v, want %v", seams.statPaths, tc.wantStats)
			}
			if !reflect.DeepEqual(seams.readPaths, tc.wantReads) {
				t.Errorf("readFile opened %v, want %v", seams.readPaths, tc.wantReads)
			}
			if !reflect.DeepEqual(seams.runCalls, tc.wantRuns) {
				t.Errorf("runner ran %v, want %v", seams.runCalls, tc.wantRuns)
			}
		})
	}

	t.Run("the whole group is served from cache inside the TTL", func(t *testing.T) {
		seams := &probeSeams{}
		files := map[string]string{
			"/usr/local/bin/claude":                "binary",
			"/home/seth/.claude/.credentials.json": credJSON,
		}
		prober := newProber(seams, "/home/seth", "linux", files, nil)
		clock := fixed
		prober.now = func() time.Time { return clock }

		first := prober.collect()
		delete(files, "/home/seth/.claude/.credentials.json")
		clock = fixed.Add(claudeProbeTTL - time.Nanosecond)
		second := prober.collect()

		want := map[string]any{"version": "2.1.211", "cred_file": true, "sub_readable": true}
		if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
			t.Errorf("collect() = %v then %v, want %v both times", first, second, want)
		}
		wantStats := []string{"/usr/local/bin/claude", "/home/seth/.claude/.credentials.json"}
		if !reflect.DeepEqual(seams.statPaths, wantStats) {
			t.Errorf("the cached call re-probed: stat saw %v, want %v", seams.statPaths, wantStats)
		}

		clock = fixed.Add(claudeProbeTTL)
		third := prober.collect()
		wantThird := map[string]any{"version": "2.1.211", "cred_file": false, "sub_readable": false}
		if !reflect.DeepEqual(third, wantThird) {
			t.Errorf("collect() after the TTL = %v, want %v", third, wantThird)
		}
	})
}

func TestClaudeProberVersion(t *testing.T) {
	const bin = "/usr/local/bin/claude"
	fixed := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		resolved string
		info     os.FileInfo
		statErr  error
		out      string
		runErr   error
		want     string
		wantRuns [][]string
	}{
		{
			name: "an unresolved binary is not stat'ed or executed",
		},
		{
			name:     "the first --version token wins",
			resolved: bin,
			info:     probeFileInfo{size: 10, mtime: fixed},
			out:      "2.1.211 (Claude Code)\n",
			want:     "2.1.211",
			wantRuns: [][]string{{bin, "--version"}},
		},
		{
			name:     "a stat fault omits the version",
			resolved: bin,
			statErr:  os.ErrNotExist,
		},
		{
			name:     "a resolved directory omits the version",
			resolved: bin,
			info:     probeFileInfo{size: 10, mtime: fixed, dir: true},
		},
		{
			name:     "an exec fault omits the version",
			resolved: bin,
			info:     probeFileInfo{size: 10, mtime: fixed},
			out:      "2.1.211",
			runErr:   errors.New("exec format error"),
			wantRuns: [][]string{{bin, "--version"}},
		},
		{
			name:     "empty output omits the version",
			resolved: bin,
			info:     probeFileInfo{size: 10, mtime: fixed},
			out:      "   \n",
			wantRuns: [][]string{{bin, "--version"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seams := &probeSeams{}
			prober := &claudeProber{
				resolveBin: func() string { return tc.resolved },
				stat: func(path string) (os.FileInfo, error) {
					seams.statPaths = append(seams.statPaths, path)
					return tc.info, tc.statErr
				},
				runner: runnerFunc(func(name string, args ...string) (string, error) {
					seams.runCalls = append(seams.runCalls, append([]string{name}, args...))
					return tc.out, tc.runErr
				}),
			}

			if got := prober.version(); got != tc.want {
				t.Errorf("version() = %q, want %q", got, tc.want)
			}
			if !reflect.DeepEqual(seams.runCalls, tc.wantRuns) {
				t.Errorf("runner ran %v, want %v", seams.runCalls, tc.wantRuns)
			}
		})
	}

	t.Run("the exec is skipped while the binary's stat identity is unchanged", func(t *testing.T) {
		seams := &probeSeams{}
		info := probeFileInfo{size: 10, mtime: fixed}
		version := "2.1.211 (Claude Code)"
		prober := &claudeProber{
			resolveBin: func() string { return bin },
			stat:       func(string) (os.FileInfo, error) { return info, nil },
			runner: runnerFunc(func(name string, args ...string) (string, error) {
				seams.runCalls = append(seams.runCalls, append([]string{name}, args...))
				return version, nil
			}),
		}

		first, second := prober.version(), prober.version()
		if first != "2.1.211" || second != "2.1.211" {
			t.Errorf("version() = %q then %q, want 2.1.211 both times", first, second)
		}
		if want := [][]string{{bin, "--version"}}; !reflect.DeepEqual(seams.runCalls, want) {
			t.Errorf("runner ran %v, want %v", seams.runCalls, want)
		}

		info = probeFileInfo{size: 11, mtime: fixed}
		version = "2.2.0 (Claude Code)"
		if got := prober.version(); got != "2.2.0" {
			t.Errorf("version() after an upgrade = %q, want 2.2.0", got)
		}
		wantRuns := [][]string{{bin, "--version"}, {bin, "--version"}}
		if !reflect.DeepEqual(seams.runCalls, wantRuns) {
			t.Errorf("runner ran %v, want %v", seams.runCalls, wantRuns)
		}

		info = probeFileInfo{size: 11, mtime: fixed.Add(time.Minute)}
		version = "2.3.0 (Claude Code)"
		if got := prober.version(); got != "2.3.0" {
			t.Errorf("version() after an mtime change = %q, want 2.3.0", got)
		}
	})

	t.Run("a failed probe is retried rather than cached", func(t *testing.T) {
		seams := &probeSeams{}
		out := ""
		prober := &claudeProber{
			resolveBin: func() string { return bin },
			stat:       func(string) (os.FileInfo, error) { return probeFileInfo{size: 10, mtime: fixed}, nil },
			runner: runnerFunc(func(name string, args ...string) (string, error) {
				seams.runCalls = append(seams.runCalls, append([]string{name}, args...))
				return out, nil
			}),
		}

		if got := prober.version(); got != "" {
			t.Errorf("version() = %q, want empty", got)
		}
		out = "2.1.211 (Claude Code)"
		if got := prober.version(); got != "2.1.211" {
			t.Errorf("version() on retry = %q, want 2.1.211", got)
		}
		wantRuns := [][]string{{bin, "--version"}, {bin, "--version"}}
		if !reflect.DeepEqual(seams.runCalls, wantRuns) {
			t.Errorf("runner ran %v, want %v", seams.runCalls, wantRuns)
		}
	})
}

func TestClaudeCredSubscriptionType(t *testing.T) {
	cases := []struct {
		name string
		body string
		err  error
		want string
	}{
		{name: "no file", err: os.ErrNotExist},
		{name: "not json", body: "not json at all"},
		{name: "json array", body: `[]`},
		{name: "no claudeAiOauth object", body: `{"other":{"subscriptionType":"max"}}`},
		{name: "blank subscriptionType", body: `{"claudeAiOauth":{"subscriptionType":"   "}}`},
		{
			name: "max plan",
			body: `{"claudeAiOauth":{"accessToken":"sk-secret","subscriptionType":" max "}}`,
			want: "max",
		},
		{
			name: "pro plan",
			body: `{"claudeAiOauth":{"subscriptionType":"pro"}}`,
			want: "pro",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked []string
			read := func(path string) ([]byte, error) {
				asked = append(asked, path)
				return []byte(tc.body), tc.err
			}

			got := claudeCredSubscriptionType(read, "/home/seth/.claude/.credentials.json")

			if got != tc.want {
				t.Errorf("claudeCredSubscriptionType = %q, want %q", got, tc.want)
			}
			if want := []string{"/home/seth/.claude/.credentials.json"}; !reflect.DeepEqual(asked, want) {
				t.Errorf("readFile opened %v, want %v", asked, want)
			}
		})
	}
}

// runnerFunc adapts a function to CmdRunner.
type runnerFunc func(name string, args ...string) (string, error)

func (f runnerFunc) Run(name string, args ...string) (string, error) { return f(name, args...) }
