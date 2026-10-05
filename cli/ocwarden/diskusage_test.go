package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
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

// writeSized writes a file of exactly size bytes and returns the allocated
// bytes the filesystem gave it, the quantity the probe adds up.
func writeSized(t *testing.T, path string, size int) int64 {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Blocks * 512
}

type diskUsageFixture struct {
	home, root string
	runner     *duRunner
	// oldBinaries is what the old binaries in bin/ and warden/ take on disk.
	oldBinaries int64
}

// newDiskUsageFixture lays out a server machine's station: workspaces m-1,
// m-12 and ow plus a stray file in agents/; warden/log and server/log;
// release-backups/; in bin/ the live ocserverd, its .bak, a hand-kept
// .bak-v… and an unrelated file; in warden/ the live binaries, two .prev
// copies and the cutover's officraft.probe.
func newDiskUsageFixture(t *testing.T) *diskUsageFixture {
	t.Helper()
	home := t.TempDir()
	f := &diskUsageFixture{home: home, root: filepath.Join(home, ".officraft")}
	agents := filepath.Join(f.root, "agents")
	mkdirs(t, filepath.Join(agents, "m-1"), filepath.Join(agents, "m-12"), filepath.Join(agents, "ow"),
		filepath.Join(f.root, "warden", "log"), filepath.Join(f.root, "server", "log"),
		filepath.Join(f.root, "release-backups"))
	writeSized(t, filepath.Join(agents, "notes.txt"), 1)
	bin := filepath.Join(f.root, "bin")
	warden := filepath.Join(f.root, "warden")
	writeSized(t, filepath.Join(bin, "ocserverd"), 16384)
	writeSized(t, filepath.Join(bin, "notes.txt"), 4096)
	writeSized(t, filepath.Join(warden, "ocwarden"), 8192)
	writeSized(t, filepath.Join(warden, "officraft"), 4096)
	writeSized(t, filepath.Join(warden, "officraft.probe"), 4096)
	f.oldBinaries = writeSized(t, filepath.Join(bin, "ocserverd.bak"), 16384) +
		writeSized(t, filepath.Join(bin, "ocserverd.bak-v0.5.27-9722925"), 12288) +
		writeSized(t, filepath.Join(warden, "ocwarden.prev"), 8192) +
		writeSized(t, filepath.Join(warden, "ocagent.prev"), 4096)
	f.runner = &duRunner{reply: f.duReply(nil)}
	return f
}

// duLines is the whole-root du the fixture answers with, in KiB.
func (f *diskUsageFixture) duLines() []string {
	agents := filepath.Join(f.root, "agents")
	return []string{
		"5000\t" + filepath.Join(agents, "m-1"),
		"1\t" + filepath.Join(agents, "m-12"),
		"300\t" + filepath.Join(agents, "ow"),
		"5305\t" + agents,
		"30\t" + filepath.Join(f.root, "warden", "log"),
		"60\t" + filepath.Join(f.root, "warden"),
		"320\t" + filepath.Join(f.root, "server", "log"),
		"900\t" + filepath.Join(f.root, "server"),
		"2000\t" + filepath.Join(f.root, "release-backups"),
		"60\t" + filepath.Join(f.root, "bin"),
		"35000\t" + f.root,
	}
}

// duReply answers the whole-root du with duLines, or with rootErr when given.
func (f *diskUsageFixture) duReply(rootErr error) func(string, []string) (string, error) {
	return func(name string, args []string) (string, error) {
		if name == "taskpolicy" {
			args = args[2:]
		}
		if len(args) == 4 && args[0] == "-k" && args[1] == "-d" && args[2] == "2" && args[3] == f.root {
			if rootErr != nil {
				return "", rootErr
			}
			return strings.Join(f.duLines(), "\n") + "\n", nil
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

func categoriesOf(logs, old any) []any {
	return []any{
		map[string]any{"key": "logs", "bytes": logs, "in_root": true},
		map[string]any{"key": "old_version_backups", "bytes": old, "in_root": true},
	}
}

func TestDiskUsageProbeMeasure(t *testing.T) {
	t.Run("under every probe succeeding on macOS, the report sizes the root, each workspace, the logs and the old versions, in one du", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"root_bytes":  int64(35840000),
			"members": []any{
				map[string]any{"member_id": "m-1", "workspace_bytes": int64(5120000)},
				map[string]any{"member_id": "m-12", "workspace_bytes": int64(1024)},
				map[string]any{"member_id": "ow", "workspace_bytes": int64(307200)},
			},
			"categories":       categoriesOf(int64(350*1024), int64(2000*1024)+f.oldBinaries),
			"disk_free_bytes":  int64(250_000_000_000),
			"disk_total_bytes": int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
		if want := []string{"taskpolicy -b du -k -d 2 " + f.root}; !reflect.DeepEqual(f.runner.calls, want) {
			t.Errorf("du calls = %q, want %q", f.runner.calls, want)
		}
		if f.runner.timeout != 15*time.Minute {
			t.Errorf("du timeout = %v, want 15m", f.runner.timeout)
		}
	})

	t.Run("off macOS, du runs without taskpolicy", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		got := f.probe(t, "linux").measure()
		if got["root_bytes"] != int64(35840000) {
			t.Errorf("root_bytes = %v, want 35840000", got["root_bytes"])
		}
		if want := []string{"du -k -d 2 " + f.root}; !reflect.DeepEqual(f.runner.calls, want) {
			t.Errorf("du calls = %q, want %q", f.runner.calls, want)
		}
	})

	t.Run("on a machine without the server, the absent server/log and release-backups count as 0 and only the warden's binaries are old versions", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		for _, rel := range []string{"server", "release-backups", "bin"} {
			if err := os.RemoveAll(filepath.Join(f.root, rel)); err != nil {
				t.Fatal(err)
			}
		}
		lines := f.duLines()
		f.runner.reply = func(string, []string) (string, error) {
			var keep []string
			for _, l := range lines {
				if !strings.Contains(l, "/server") && !strings.Contains(l, "/release-backups") && !strings.Contains(l, "/bin") {
					keep = append(keep, l)
				}
			}
			return strings.Join(keep, "\n") + "\n", nil
		}
		warden := filepath.Join(f.root, "warden")
		var prev int64
		for _, name := range []string{"ocwarden.prev", "ocagent.prev"} {
			info, _ := os.Lstat(filepath.Join(warden, name))
			prev += info.Sys().(*syscall.Stat_t).Blocks * 512
		}
		got := f.probe(t, "darwin").measure()
		if want := categoriesOf(int64(30*1024), prev); !reflect.DeepEqual(got["categories"], want) {
			t.Errorf("categories = %#v, want %#v", got["categories"], want)
		}
	})

	t.Run("under a whole-root du that fails with no output, root and workspace sizes are omitted and both categories are null", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		f.runner.reply = f.duReply(errors.New("timeout after 15m0s"))
		got := f.probe(t, "darwin").measure()
		want := map[string]any{
			"measured_at": float64(1790000107),
			"took_secs":   107.3,
			"members": []any{
				map[string]any{"member_id": "m-1"},
				map[string]any{"member_id": "m-12"},
				map[string]any{"member_id": "ow"},
			},
			"categories":       categoriesOf(nil, nil),
			"disk_free_bytes":  int64(250_000_000_000),
			"disk_total_bytes": int64(994_662_584_320),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("measure() =\n  %#v\nwant\n  %#v", got, want)
		}
	})

	t.Run("under a du that exits 1 but printed the root line, what it sized is used and a log directory it skipped makes logs null", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		agents := filepath.Join(f.root, "agents")
		f.runner.reply = func(name string, args []string) (string, error) {
			return "du: " + f.root + "/server/log: Permission denied\n" +
				"4999\t" + filepath.Join(agents, "m-1") + "\n" +
				"30\t" + filepath.Join(f.root, "warden", "log") + "\n" +
				"2000\t" + filepath.Join(f.root, "release-backups") + "\n" +
				"35001\t" + f.root + "\n", errors.New("exit status 1")
		}
		got := f.probe(t, "darwin").measure()
		members := got["members"].([]any)
		wantM1 := map[string]any{"member_id": "m-1", "workspace_bytes": int64(5118976)}
		wantOw := map[string]any{"member_id": "ow"}
		if got["root_bytes"] != int64(35841024) || !reflect.DeepEqual(members[0], wantM1) || !reflect.DeepEqual(members[2], wantOw) {
			t.Errorf("root_bytes = %v, members = %#v", got["root_bytes"], members)
		}
		if want := categoriesOf(nil, int64(2000*1024)+f.oldBinaries); !reflect.DeepEqual(got["categories"], want) {
			t.Errorf("categories = %#v, want %#v", got["categories"], want)
		}
	})

	t.Run("under an agents directory that cannot be listed, members are left out rather than sent empty", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		p := f.probe(t, "darwin")
		agents := filepath.Join(f.root, "agents")
		p.readDir = func(path string) ([]fs.DirEntry, error) {
			if path == agents {
				return nil, fs.ErrPermission
			}
			return os.ReadDir(path)
		}
		got := p.measure()
		if _, has := got["members"]; has || got["root_bytes"] != int64(35840000) {
			t.Errorf("members = %v, root_bytes = %v; want no members and the root kept", got["members"], got["root_bytes"])
		}
	})

	t.Run("under a bin directory that cannot be listed, old_version_backups is null and logs are kept", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		p := f.probe(t, "darwin")
		bin := filepath.Join(f.root, "bin")
		p.readDir = func(path string) ([]fs.DirEntry, error) {
			if path == bin {
				return nil, fs.ErrPermission
			}
			return os.ReadDir(path)
		}
		if want := categoriesOf(int64(350*1024), nil); !reflect.DeepEqual(p.measure()["categories"], want) {
			t.Errorf("categories = %#v, want %#v", p.measure()["categories"], want)
		}
	})

	t.Run("under a failed statfs, only the two disk fields are omitted", func(t *testing.T) {
		f := newDiskUsageFixture(t)
		p := f.probe(t, "darwin")
		p.statfs = func(string) (int64, int64, error) { return 0, 0, errors.New("no such volume") }
		got := p.measure()
		_, hasFree := got["disk_free_bytes"]
		_, hasTotal := got["disk_total_bytes"]
		if hasFree || hasTotal || got["root_bytes"] != int64(35840000) || got["categories"] == nil {
			t.Errorf("measure() = %#v, want no disk_free/total_bytes and the other fields kept", got)
		}
	})
}

func TestIsOldBinary(t *testing.T) {
	for name, want := range map[string]bool{
		"ocserverd.bak": true, "ocserverd.bak-v0.5.27-9722925": true, "ocserverd.rollback-v055": true,
		"ocserverd.v040.bak": true, "ocserverd.bak.v010": true, "ocwarden.prev": true, "ocagent.prev": true,
		"ocwarden.bak-v0.5.27-9722925": true, "officraft.old": true,
		"ocserverd": false, "ocwarden": false, "officraft": false, "officraft.probe": false,
		"ocserverd.": false, "exec-warden.tok": false, ".ocserverd-upgrade-1": false, "ocagentx.prev": false,
	} {
		if got := isOldBinary(name); got != want {
			t.Errorf("isOldBinary(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestNewDiskUsageProbe(t *testing.T) {
	envOf := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	t.Run("under a canonical station, it measures ~/.officraft with the 15-minute du timeout", func(t *testing.T) {
		runner := &duRunner{}
		p, ok := newDiskUsageProbe(envOf(map[string]string{"HOME": "/Users/a"}), runner, "darwin")
		if !ok || p.root != "/Users/a/.officraft" || p.goos != "darwin" || p.run != runner || runner.timeout != 15*time.Minute {
			t.Errorf("ok = %v, root = %q, goos = %q, run is the runner = %v, timeout = %v", ok, p.root, p.goos, p.run == runner, runner.timeout)
		}
	})

	t.Run("under a namespace, it measures that station's root", func(t *testing.T) {
		p, ok := newDiskUsageProbe(envOf(map[string]string{"HOME": "/Users/a", "OC_NAMESPACE": "dev"}), &duRunner{}, "darwin")
		if !ok || p.root != "/Users/a/.officraft-dev" {
			t.Errorf("ok = %v, root = %q, want true and /Users/a/.officraft-dev", ok, p.root)
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
