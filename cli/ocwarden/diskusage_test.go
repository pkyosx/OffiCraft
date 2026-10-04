package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// duRunner stands in for the subprocess seam: reply answers each du call, and
// every call is recorded as one space-joined command line.
type duRunner struct {
	calls   []string
	timeout time.Duration
	reply   func(name string, args []string) (string, error)
}

func (r *duRunner) Run(name string, args ...string) (string, error) {
	return "", errors.New("Run is not the disk-usage path")
}

func (r *duRunner) RunKeepStdout(name string, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return r.reply(name, args)
}

func (r *duRunner) withTimeout(d time.Duration) CmdRunner {
	r.timeout = d
	return r
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeCodexRollout writes a rollout whose first line is a record of type typ
// naming cwd, padded to exactly size bytes so its allocated blocks are known.
func writeCodexRollout(t *testing.T, path, typ, cwd string, size int) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	line := `{"timestamp":"2026-09-26T20:32:14.980Z","ordinal":0,"type":"` + typ +
		`","payload":{"session_id":"s","cwd":"` + cwd + `","originator":"officraft"}}` + "\n"
	body := line + strings.Repeat(" ", size-len(line))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type diskUsageFixture struct {
	home, root, projects, sessions string
	runner                         *duRunner
	claudeDirs                     map[string]string
}

// newDiskUsageFixture lays out one station: workspaces m-1, m-12, ow and ow-3;
// Claude projects for m-1 (two), ow, ow-3, a departed member, a sibling
// station, a station namespaced "agents" and an unrelated directory; Codex rollouts for m-12, m-1, a departed
// member, a sibling station, a non-meta first line and the agents dir itself.
func newDiskUsageFixture(t *testing.T) *diskUsageFixture {
	t.Helper()
	home := t.TempDir()
	f := &diskUsageFixture{
		home:     home,
		root:     filepath.Join(home, ".officraft"),
		projects: filepath.Join(home, ".claude", "projects"),
		sessions: filepath.Join(home, ".codex", "sessions"),
	}
	agents := filepath.Join(f.root, "agents")
	mkdirs(t, filepath.Join(agents, "m-1"), filepath.Join(agents, "m-12"),
		filepath.Join(agents, "ow"), filepath.Join(agents, "ow-3"))
	if err := os.WriteFile(filepath.Join(agents, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	enc := func(p string) string { return claudeProjectName(p) }
	f.claudeDirs = map[string]string{
		enc(filepath.Join(agents, "m-1")):                                     "10",
		enc(filepath.Join(agents, "m-1")) + "-work-y":                         "20",
		enc(filepath.Join(agents, "ow-3")) + "-work-x":                        "40",
		enc(filepath.Join(agents, "ow")) + "-scratch":                         "2",
		enc(filepath.Join(agents, "gone")):                                    "80",
		enc(filepath.Join(home, ".officraft-dev", "agents", "m-1")):           "1000",
		enc(filepath.Join(home, ".officraft-agents", "agents", "m-1")) + "-w": "500",
		enc(home) + "-elsewhere":                                              "3000",
	}
	for name := range f.claudeDirs {
		mkdirs(t, filepath.Join(f.projects, name))
	}
	mkdirs(t, filepath.Join(home, ".officraft-agents", "agents", "m-1"))
	if err := os.WriteFile(filepath.Join(f.projects, enc(filepath.Join(agents, "m-1"))+"-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	day := filepath.Join(f.sessions, "2026", "09", "27")
	writeCodexRollout(t, filepath.Join(day, "a.jsonl"), "session_meta", filepath.Join(agents, "m-12", "work", "z"), 8192)
	writeCodexRollout(t, filepath.Join(day, "b.jsonl"), "session_meta", filepath.Join(agents, "m-1"), 4096)
	writeCodexRollout(t, filepath.Join(f.sessions, "c.jsonl"), "session_meta", filepath.Join(agents, "left-9"), 4096)
	writeCodexRollout(t, filepath.Join(day, "d.jsonl"), "session_meta", filepath.Join(home, ".officraft-dev", "agents", "m-1"), 4096)
	writeCodexRollout(t, filepath.Join(day, "e.jsonl"), "event_msg", filepath.Join(agents, "m-1"), 4096)
	writeCodexRollout(t, filepath.Join(day, "f.jsonl"), "session_meta", agents, 4096)

	f.runner = &duRunner{reply: f.duReply(nil, nil)}
	return f
}

// duReply answers the whole-root du with a full tree, or with rootErr when one is
// given, and the projects du with one line per directory asked about, or with
// sizesErr.
func (f *diskUsageFixture) duReply(rootErr error, sizesErr error) func(string, []string) (string, error) {
	agents := filepath.Join(f.root, "agents")
	return func(name string, args []string) (string, error) {
		if name == "taskpolicy" {
			args = args[2:]
		}
		if len(args) == 4 && args[0] == "-k" && args[1] == "-d" && args[2] == "2" && args[3] == f.root {
			if rootErr != nil {
				return "", rootErr
			}
			return strings.Join([]string{
				"5000\t" + filepath.Join(agents, "m-1"),
				"1\t" + filepath.Join(agents, "m-12"),
				"300\t" + filepath.Join(agents, "ow"),
				"7000\t" + filepath.Join(agents, "ow-3"),
				"12301\t" + agents,
				"900\t" + filepath.Join(f.root, "server"),
				"35000\t" + f.root,
			}, "\n") + "\n", nil
		}
		if len(args) > 1 && args[0] == "-sk" {
			if sizesErr != nil {
				return "", sizesErr
			}
			var lines []string
			for _, dir := range args[1:] {
				kb, ok := f.claudeDirs[filepath.Base(dir)]
				if !ok {
					return "", errors.New("du asked about an unknown directory " + dir)
				}
				lines = append(lines, kb+"\t"+dir)
			}
			return strings.Join(lines, "\n") + "\n", nil
		}
		return "", errors.New("unexpected du call")
	}
}

func (f *diskUsageFixture) probe(t *testing.T, goos string) diskUsageProbe {
	t.Helper()
	p, ok := newDiskUsageProbe(func(k string) string {
		if k == "HOME" {
			return f.home
		}
		return ""
	}, f.runner, goos)
	if !ok {
		t.Fatal("newDiskUsageProbe refused a valid HOME")
	}
	p.statfs = func(path string) (int64, int64, error) {
		if path != f.root {
			return 0, 0, errors.New("statfs on " + path + ", want the station root")
		}
		return 250_000_000_000, 994_662_584_320, nil
	}
	clock := []time.Time{time.Unix(1790000000, 0), time.Unix(1790000000, 0).Add(107260 * time.Millisecond)}
	p.now = func() time.Time {
		next := clock[0]
		if len(clock) > 1 {
			clock = clock[1:]
		}
		return next
	}
	return p
}

func (f *diskUsageFixture) projectsCall(prefix string) string {
	agents := claudeProjectName(filepath.Join(f.root, "agents"))
	dirs := []string{agents + "-gone", agents + "-m-1", agents + "-m-1-work-y", agents + "-ow-3-work-x", agents + "-ow-scratch"}
	for i, d := range dirs {
		dirs[i] = filepath.Join(f.projects, d)
	}
	return prefix + "-sk " + strings.Join(dirs, " ")
}

func TestDiskUsageProbeMeasure(t *testing.T) {
	t.Run("under every probe succeeding on macOS, the report sizes the root, each member (0 for logs with no workspace left) and both runtimes' logs, leaving out a station whose Claude prefix extends this one's", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"root_bytes":  int64(35840000),
			"members": []any{
				map[string]any{"member_id": "left-9", "workspace_bytes": int64(0), "conversation_bytes": int64(4096)},
				map[string]any{"member_id": "m-1", "workspace_bytes": int64(5120000), "conversation_bytes": int64(34816)},
				map[string]any{"member_id": "m-12", "workspace_bytes": int64(1024), "conversation_bytes": int64(8192)},
				map[string]any{"member_id": "ow", "workspace_bytes": int64(307200), "conversation_bytes": int64(2048)},
				map[string]any{"member_id": "ow-3", "workspace_bytes": int64(7168000), "conversation_bytes": int64(40960)},
			},
			"claude_conversation_bytes": int64(155648),
			"codex_conversation_bytes":  int64(16384),
			"disk_free_bytes":           int64(250_000_000_000),
			"disk_total_bytes":          int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
		wantCalls := []string{"taskpolicy -b du -k -d 2 " + f.root, f.projectsCall("taskpolicy -b du ")}
		if !reflect.DeepEqual(f.runner.calls, wantCalls) {
			t.Errorf("du calls =\n  %q\nwant\n  %q", f.runner.calls, wantCalls)
		}
		if f.runner.timeout != 15*time.Minute {
			t.Errorf("du timeout = %v, want 15m", f.runner.timeout)
		}
	})

	t.Run("off macOS, du runs without taskpolicy", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		got := f.probe(t, "linux").measure()
		if got["root_bytes"] != int64(35840000) || got["claude_conversation_bytes"] != int64(155648) {
			t.Errorf("root_bytes = %v, claude_conversation_bytes = %v, want 35840000 and 155648",
				got["root_bytes"], got["claude_conversation_bytes"])
		}
		wantCalls := []string{"du -k -d 2 " + f.root, f.projectsCall("du ")}
		if !reflect.DeepEqual(f.runner.calls, wantCalls) {
			t.Errorf("du calls =\n  %q\nwant\n  %q", f.runner.calls, wantCalls)
		}
	})

	t.Run("under a whole-root du that fails with no output, root and workspace sizes are omitted and the rest is sent", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		f.runner.reply = f.duReply(errors.New("timeout after 15m0s"), nil)
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"members": []any{
				map[string]any{"member_id": "left-9", "conversation_bytes": int64(4096)},
				map[string]any{"member_id": "m-1", "conversation_bytes": int64(34816)},
				map[string]any{"member_id": "m-12", "conversation_bytes": int64(8192)},
				map[string]any{"member_id": "ow", "conversation_bytes": int64(2048)},
				map[string]any{"member_id": "ow-3", "conversation_bytes": int64(40960)},
			},
			"claude_conversation_bytes": int64(155648),
			"codex_conversation_bytes":  int64(16384),
			"disk_free_bytes":           int64(250_000_000_000),
			"disk_total_bytes":          int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
	})

	t.Run("under a du that exits 1 for an unreadable subdirectory but printed the root line, its sizes are used", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		agents := filepath.Join(f.root, "agents")
		full := f.duReply(nil, nil)
		f.runner.reply = func(name string, args []string) (string, error) {
			if args[len(args)-1] == f.root {
				return "du: " + agents + "/m-1/locked: Permission denied\n" +
					"4999\t" + filepath.Join(agents, "m-1") + "\n" +
					"35001\t" + f.root + "\n", errors.New("exit status 1")
			}
			return full(name, args)
		}
		got := f.probe(t, "darwin").measure()
		members := got["members"].([]any)
		if got["root_bytes"] != int64(35841024) {
			t.Errorf("root_bytes = %v, want 35841024", got["root_bytes"])
		}
		wantM1 := map[string]any{"member_id": "m-1", "workspace_bytes": int64(5118976), "conversation_bytes": int64(34816)}
		wantOw := map[string]any{"member_id": "ow", "conversation_bytes": int64(2048)}
		if !reflect.DeepEqual(members[1], wantM1) || !reflect.DeepEqual(members[3], wantOw) {
			t.Errorf("members[1], members[3] = %#v, %#v, want %#v, %#v", members[1], members[3], wantM1, wantOw)
		}
	})

	t.Run("under a failed Claude du, the Claude total and every member's conversation size are omitted", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		f.runner.reply = f.duReply(nil, errors.New("signal: killed"))
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"root_bytes":  int64(35840000),
			"members": []any{
				map[string]any{"member_id": "left-9", "workspace_bytes": int64(0)},
				map[string]any{"member_id": "m-1", "workspace_bytes": int64(5120000)},
				map[string]any{"member_id": "m-12", "workspace_bytes": int64(1024)},
				map[string]any{"member_id": "ow", "workspace_bytes": int64(307200)},
				map[string]any{"member_id": "ow-3", "workspace_bytes": int64(7168000)},
			},
			"codex_conversation_bytes": int64(16384),
			"disk_free_bytes":          int64(250_000_000_000),
			"disk_total_bytes":         int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
	})

	t.Run("under a failed statfs, only the two disk fields are omitted", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		p := f.probe(t, "darwin")
		p.statfs = func(string) (int64, int64, error) { return 0, 0, errors.New("no such volume") }
		got := p.measure()
		_, hasFree := got["disk_free_bytes"]
		_, hasTotal := got["disk_total_bytes"]
		if hasFree || hasTotal || got["root_bytes"] != int64(35840000) || got["codex_conversation_bytes"] != int64(16384) {
			t.Errorf("measure() = %#v, want no disk_free/total_bytes and the other fields kept", got)
		}
	})

	t.Run("under no Claude projects and no Codex sessions directory, both totals are zero and du runs once", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		if err := os.RemoveAll(f.projects); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(f.sessions); err != nil {
			t.Fatal(err)
		}
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"root_bytes":  int64(35840000),
			"members": []any{
				map[string]any{"member_id": "m-1", "workspace_bytes": int64(5120000), "conversation_bytes": int64(0)},
				map[string]any{"member_id": "m-12", "workspace_bytes": int64(1024), "conversation_bytes": int64(0)},
				map[string]any{"member_id": "ow", "workspace_bytes": int64(307200), "conversation_bytes": int64(0)},
				map[string]any{"member_id": "ow-3", "workspace_bytes": int64(7168000), "conversation_bytes": int64(0)},
			},
			"claude_conversation_bytes": int64(0),
			"codex_conversation_bytes":  int64(0),
			"disk_free_bytes":           int64(250_000_000_000),
			"disk_total_bytes":          int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
		if want := []string{"taskpolicy -b du -k -d 2 " + f.root}; !reflect.DeepEqual(f.runner.calls, want) {
			t.Errorf("du calls = %q, want %q", f.runner.calls, want)
		}
	})
}

func TestNewDiskUsageProbe(t *testing.T) {
	envOf := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	t.Run("under a canonical station with no redirects, it measures ~/.officraft, ~/.claude/projects and ~/.codex/sessions", func(t *testing.T) {
		runner := &duRunner{}
		p, ok := newDiskUsageProbe(envOf(map[string]string{"HOME": "/Users/a"}), runner, "darwin")
		got := []string{p.root, p.claudeProjects, p.codexSessions, p.goos}
		want := []string{"/Users/a/.officraft", "/Users/a/.claude/projects", "/Users/a/.codex/sessions", "darwin"}
		if !ok || !reflect.DeepEqual(got, want) || p.run != runner || runner.timeout != 15*time.Minute {
			t.Errorf("ok = %v, paths = %q, run is the runner = %v, timeout = %v; want true, %q, true, 15m",
				ok, got, p.run == runner, runner.timeout, want)
		}
	})

	t.Run("under a namespace, an OC_CLAUDE_JSON redirect and CODEX_HOME, it follows all three", func(t *testing.T) {
		p, ok := newDiskUsageProbe(envOf(map[string]string{
			"HOME": "/Users/a", "OC_NAMESPACE": "dev",
			"OC_CLAUDE_JSON": "/srv/claude/.claude.json", "CODEX_HOME": "/srv/codex/",
		}), &duRunner{}, "darwin")
		got := []string{p.root, p.claudeProjects, p.codexSessions}
		want := []string{"/Users/a/.officraft-dev", "/srv/claude/projects", "/srv/codex/sessions"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("ok = %v, paths = %q, want true and %q", ok, got, want)
		}
	})

	t.Run("under no HOME, a relative HOME or a runner that cannot keep stdout, nothing is measured", func(t *testing.T) {
		for name, c := range map[string]struct {
			env    map[string]string
			runner CmdRunner
		}{
			"no HOME":       {map[string]string{}, &duRunner{}},
			"relative HOME": {map[string]string{"HOME": "home/a"}, &duRunner{}},
			"bad namespace": {map[string]string{"HOME": "/Users/a", "OC_NAMESPACE": "Dev"}, &duRunner{}},
			"no keep":       {map[string]string{"HOME": "/Users/a"}, fakeRunner{}},
		} {
			if _, ok := newDiskUsageProbe(envOf(c.env), c.runner, "darwin"); ok {
				t.Errorf("%s: ok = true, want false", name)
			}
		}
	})
}

func TestDiskUsageReporter(t *testing.T) {
	t.Run("it measures at once, then again once the interval has passed since the last measurement finished, waking at most every minute", func(t *testing.T) {
		clock := newFakeClock(time.Unix(1790000000, 0))
		var measuredAt []time.Time
		r := newDiskUsageReporter(func() map[string]any {
			clock.advance(5 * time.Minute)
			measuredAt = append(measuredAt, clock.now())
			return map[string]any{"root_bytes": int64(len(measuredAt))}
		})
		r.now = clock.now
		if got := r.snapshot(); got != nil {
			t.Errorf("snapshot before any measurement = %v, want nil", got)
		}
		var waits []time.Duration
		var seen []map[string]any
		r.run(context.Background(), func(_ context.Context, d time.Duration) bool {
			waits = append(waits, d)
			seen = append(seen, r.snapshot())
			clock.advance(d)
			return len(measuredAt) < 2
		})
		wantWaits := make([]time.Duration, 61)
		for i := range wantWaits {
			wantWaits[i] = time.Minute
		}
		if !reflect.DeepEqual(waits, wantWaits) {
			t.Fatalf("waits = %v, want 61 waits of 1m", waits)
		}
		wantAt := []time.Time{time.Unix(1790000300, 0), time.Unix(1790004200, 0)}
		if !reflect.DeepEqual(measuredAt, wantAt) {
			t.Errorf("measured at %v, want %v", measuredAt, wantAt)
		}
		if want := map[string]any{"root_bytes": int64(1)}; !reflect.DeepEqual(seen[59], want) {
			t.Errorf("snapshot at the last wait = %v, want %v", seen[59], want)
		}
		if want := map[string]any{"root_bytes": int64(2)}; !reflect.DeepEqual(seen[60], want) {
			t.Errorf("snapshot at the wait after the second measurement = %v, want %v", seen[60], want)
		}
	})

	t.Run("under an interval lowered from a day to 600 s after 600 s have passed, the next wake measures; under one raised before it is due, nothing is measured early", func(t *testing.T) {
		start := time.Unix(1790000000, 0)
		clock := newFakeClock(start)
		var measuredAt []time.Duration
		r := newDiskUsageReporter(func() map[string]any {
			measuredAt = append(measuredAt, clock.now().Sub(start))
			return map[string]any{}
		})
		r.now = clock.now
		r.setInterval(86400 * time.Second)
		var waits []time.Duration
		r.run(context.Background(), func(_ context.Context, d time.Duration) bool {
			waits = append(waits, d)
			clock.advance(d)
			switch clock.now().Sub(start) {
			case 900 * time.Second:
				r.setInterval(600 * time.Second)
			case 1260 * time.Second:
				r.setInterval(1200 * time.Second)
			}
			return len(measuredAt) < 3
		})
		if want := []time.Duration{0, 900 * time.Second, 2100 * time.Second}; !reflect.DeepEqual(measuredAt, want) {
			t.Errorf("measured at +%v, want +%v", measuredAt, want)
		}
		for _, d := range waits {
			if d != time.Minute {
				t.Fatalf("waits = %v, want every wait 1m", waits)
			}
		}
		if len(waits) != 36 {
			t.Errorf("%d waits, want 36", len(waits))
		}
	})

	t.Run("while a measurement is running, a snapshot returns at once with the previous result", func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		r := newDiskUsageReporter(func() map[string]any {
			close(started)
			<-release
			return map[string]any{"root_bytes": int64(1)}
		})
		done := make(chan struct{})
		go func() {
			r.run(context.Background(), func(context.Context, time.Duration) bool { return false })
			close(done)
		}()
		<-started
		got := make(chan map[string]any, 1)
		go func() { got <- r.snapshot() }()
		select {
		case snap := <-got:
			if snap != nil {
				t.Errorf("snapshot = %v, want nil", snap)
			}
		case <-time.After(5 * time.Second):
			t.Error("snapshot blocked behind a running measurement")
		}
		close(release)
		<-done
		if want := map[string]any{"root_bytes": int64(1)}; !reflect.DeepEqual(r.snapshot(), want) {
			t.Errorf("snapshot after = %v, want %v", r.snapshot(), want)
		}
	})

	t.Run("under a cancelled context, it does not measure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := newDiskUsageReporter(func() map[string]any { t.Error("measured after cancel"); return nil })
		r.run(ctx, func(context.Context, time.Duration) bool { return true })
	})
}

type fakeClock struct{ at time.Time }

func newFakeClock(at time.Time) *fakeClock   { return &fakeClock{at: at} }
func (c *fakeClock) now() time.Time          { return c.at }
func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }
