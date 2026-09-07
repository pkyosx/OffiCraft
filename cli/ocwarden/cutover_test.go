package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// runAnswer is one queued answer for a repeated command, consumed in order.
type runAnswer struct {
	out string
	err error
}

// spawnCall is one detached process the cutover path asked for.
type spawnCall struct {
	bin     string
	args    []string
	env     []string
	logPath string
}

// cutoverRec is a recording stand-in for every cutover effect. Zero value: no
// files, no processes, every probe failing the way an absent one would.
type cutoverRec struct {
	ppidExe    map[int]string
	ppidExeErr map[int]error

	runOut   map[string]string
	runErr   map[string]error
	runQueue map[string][]runAnswer
	runs     []string

	runExitCode map[string]int
	runExitErr  map[string]error
	runExits    []string

	installerOut    string
	installerErr    error
	installerCalls  []string
	installerWrites map[string]string

	files     map[string]string
	readErr   map[string]error
	writeErr  map[string]error
	writes    []string
	chmods    []string
	chmodErr  map[string]error
	links     [][2]string
	linkErr   error
	removes   []string
	removeErr map[string]error

	created      map[string]bool
	createExclOn map[string]bool
	createErr    map[string]error

	modTimes map[string]time.Time
	births   map[string]time.Time

	spawns    []spawnCall
	spawnErr  error
	sleeps    []time.Duration
	sleepStop int
}

func newCutoverRec() *cutoverRec {
	return &cutoverRec{
		ppidExe: map[int]string{}, ppidExeErr: map[int]error{},
		runOut: map[string]string{}, runErr: map[string]error{}, runQueue: map[string][]runAnswer{},
		runExitCode: map[string]int{}, runExitErr: map[string]error{},
		files: map[string]string{}, readErr: map[string]error{}, writeErr: map[string]error{},
		chmodErr:  map[string]error{},
		removeErr: map[string]error{}, created: map[string]bool{},
		createExclOn: map[string]bool{}, createErr: map[string]error{},
		modTimes: map[string]time.Time{}, births: map[string]time.Time{},
		installerWrites: map[string]string{},
	}
}

func argvKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func (r *cutoverRec) ops() cutoverOps {
	return cutoverOps{
		ppidExe: func(pid int) (string, error) {
			if err, ok := r.ppidExeErr[pid]; ok {
				return "", err
			}
			return r.ppidExe[pid], nil
		},
		run: func(name string, args ...string) (string, error) {
			key := argvKey(name, args...)
			r.runs = append(r.runs, key)
			if queued := r.runQueue[key]; len(queued) > 0 {
				r.runQueue[key] = queued[1:]
				return queued[0].out, queued[0].err
			}
			if err, ok := r.runErr[key]; ok {
				return "", err
			}
			if out, ok := r.runOut[key]; ok {
				return out, nil
			}
			return "", os.ErrNotExist
		},
		runExit: func(name string, args ...string) (int, error) {
			key := argvKey(name, args...)
			r.runExits = append(r.runExits, key)
			if err, ok := r.runExitErr[key]; ok {
				return -1, err
			}
			if code, ok := r.runExitCode[key]; ok {
				return code, nil
			}
			return -1, os.ErrNotExist
		},
		runInstaller: func(name string, args ...string) (string, error) {
			r.installerCalls = append(r.installerCalls, argvKey(name, args...))
			for path, data := range r.installerWrites {
				r.files[path] = data
			}
			return r.installerOut, r.installerErr
		},
		readFile: func(path string) ([]byte, error) {
			if err, ok := r.readErr[path]; ok {
				return nil, err
			}
			if data, ok := r.files[path]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		},
		writeFile: func(path string, data []byte, perm os.FileMode) error {
			if err, ok := r.writeErr[path]; ok {
				return err
			}
			r.writes = append(r.writes, fmt.Sprintf("%s %04o %d", path, perm, len(data)))
			r.files[path] = string(data)
			return nil
		},
		chmod: func(path string, perm os.FileMode) error {
			if err, ok := r.chmodErr[path]; ok {
				return err
			}
			r.chmods = append(r.chmods, fmt.Sprintf("%s %04o", path, perm))
			return nil
		},
		link: func(oldpath, newpath string) error {
			if r.linkErr != nil {
				return r.linkErr
			}
			r.links = append(r.links, [2]string{oldpath, newpath})
			r.files[newpath] = r.files[oldpath]
			r.modTimes[newpath] = time.Unix(0, 0)
			return nil
		},
		remove: func(path string) error {
			if err, ok := r.removeErr[path]; ok {
				return err
			}
			r.removes = append(r.removes, path)
			delete(r.files, path)
			delete(r.modTimes, path)
			delete(r.created, path)
			return nil
		},
		createExcl: func(path string) (bool, error) {
			if err, ok := r.createErr[path]; ok {
				return false, err
			}
			if r.created[path] {
				return false, nil
			}
			r.created[path] = true
			r.createExclOn[path] = true
			return true, nil
		},
		modTime: func(path string) (time.Time, error) {
			if mt, ok := r.modTimes[path]; ok {
				return mt, nil
			}
			return time.Time{}, os.ErrNotExist
		},
		birthTime: func(path string) (time.Time, error) {
			if b, ok := r.births[path]; ok {
				return b, nil
			}
			return time.Time{}, errNoBirthTime
		},
		spawnDetached: func(bin string, args, env []string, logPath string) error {
			if r.spawnErr != nil {
				return r.spawnErr
			}
			r.spawns = append(r.spawns, spawnCall{bin: bin, args: args, env: env, logPath: logPath})
			return nil
		},
		sleep: func(d time.Duration) { r.sleeps = append(r.sleeps, d) },
	}
}

// bindCutoverOps points the package's ops constructor at rec for this test only.
func bindCutoverOps(t *testing.T, rec *cutoverRec) {
	t.Helper()
	previous := newCutoverOps
	newCutoverOps = func() cutoverOps { return rec.ops() }
	t.Cleanup(func() { newCutoverOps = previous })
}

// cutoverPaths is a wardenPaths whose every path lives under root.
func cutoverPaths(root string) wardenPaths {
	return wardenPaths{
		root:       root,
		home:       root,
		label:      "com.officraft.ocwarden",
		ocBase:     "https://station.example",
		ocToken:    jwtWardenOne,
		logDir:     filepath.Join(root, "warden", "log"),
		binPath:    filepath.Join(root, "warden", "ocwarden"),
		anchorSrc:  filepath.Join(root, "src", "officraft"),
		anchorPath: filepath.Join(root, "warden", "officraft"),
		plistPath:  filepath.Join(root, "Library", "LaunchAgents", "com.officraft.ocwarden.plist"),
		guiDomain:  "gui/501",
	}
}

func TestRealCutoverOps(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		ops := realCutoverOps()
		fmt.Printf("realCutoverOps handed back a seam (run==nil: %v)\n", ops.run == nil)
		return
	}
	code, out := runRefusalChild(t, "TestRealCutoverOps")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("realCutoverOps")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("realCutoverOps"))
	}
	if strings.Contains(out, "realCutoverOps handed back a seam") {
		t.Error("a test binary was handed the real launchctl/filesystem wiring")
	}
}

func TestSpawnDetachedProcess(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		err := spawnDetachedProcess("/bin/echo", []string{"hello"}, nil, "")
		fmt.Printf("spawnDetachedProcess started something (err: %v)\n", err)
		return
	}
	code, out := runRefusalChild(t, "TestSpawnDetachedProcess")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("spawnDetachedProcess(/bin/echo)")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("spawnDetachedProcess(/bin/echo)"))
	}
	if strings.Contains(out, "spawnDetachedProcess started something") {
		t.Error("a test binary was allowed to detach a real process")
	}
}

func TestRealRunExit(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		code, err := realRunExit("/usr/bin/true")
		fmt.Printf("realRunExit ran something (code %d, err %v)\n", code, err)
		return
	}
	code, out := runRefusalChild(t, "TestRealRunExit")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("realRunExit(/usr/bin/true)")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("realRunExit(/usr/bin/true)"))
	}
	if strings.Contains(out, "realRunExit ran something") {
		t.Error("a test binary was allowed to run a real command")
	}
}

func TestRunInstallerCombined(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		out, err := runInstallerCombined("/usr/bin/true", "install", "--force")
		fmt.Printf("runInstallerCombined ran an installer (out %q, err %v)\n", out, err)
		return
	}
	code, out := runRefusalChild(t, "TestRunInstallerCombined")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out, refusalText("runInstallerCombined(/usr/bin/true)")) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, refusalText("runInstallerCombined(/usr/bin/true)"))
	}
	if strings.Contains(out, "runInstallerCombined ran an installer") {
		t.Error("a test binary was allowed to run a real install")
	}
}

func TestPsExePath(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		"ps -p 4242 -o comm=": "/Users/eva/.officraft/warden/officraft\n",
		"ps -p 1 -o comm=":    "  /sbin/launchd  ",
	}}
	exe := psExePath(runner)

	for _, c := range []struct {
		pid  int
		want string
	}{
		{4242, "/Users/eva/.officraft/warden/officraft"},
		{1, "/sbin/launchd"},
	} {
		got, err := exe(c.pid)
		if err != nil || got != c.want {
			t.Errorf("psExePath(%d) = (%q, %v), want (%q, nil)", c.pid, got, err, c.want)
		}
	}

	got, err := exe(999999)
	if got != "" || err == nil {
		t.Errorf("psExePath for a pid ps cannot read = (%q, %v), want (\"\", an error)", got, err)
	}
}

func TestDetectShape(t *testing.T) {
	const anchor = "/Users/eva/.officraft/warden/officraft"
	cases := []struct {
		name   string
		ppid   int
		exe    string
		exeErr error
		want   wardenShape
	}{
		{"the parent IS the anchor", 4242, anchor, nil, shapeAnchor},
		{"pid 1 is the anchor itself on a converted machine", 1, anchor, nil, shapeAnchor},
		{"launchd is the direct parent", 1, "/sbin/launchd", nil, shapeLegacy},
		{"a launchd by any path is still launchd", 77, "/usr/libexec/launchd", nil, shapeLegacy},
		{"pid 1 whatever it reports as", 1, "/some/other/binary", nil, shapeLegacy},
		{"a shell parent is no verdict at all", 900, "/bin/zsh", nil, shapeUnknown},
		{"a test binary parent is no verdict", 900, "/tmp/go-build/ocwarden.test", nil, shapeUnknown},
		{"an unreadable parent is no verdict", 900, "", errors.New("ps: no such process"), shapeUnknown},
		{"an empty parent path is no verdict", 900, "", nil, shapeUnknown},
		{"an unreadable pid 1 is not silently legacy", 1, "", errors.New("ps failed"), shapeUnknown},
		{"a same-named anchor elsewhere on disk is not this anchor", 4242, "/tmp/officraft", nil, shapeUnknown},
	}
	for _, c := range cases {
		rec := newCutoverRec()
		if c.exeErr != nil {
			rec.ppidExeErr[c.ppid] = c.exeErr
		} else {
			rec.ppidExe[c.ppid] = c.exe
		}
		if got := detectShape(rec.ops(), c.ppid, anchor); got != c.want {
			t.Errorf("%s: detectShape = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNewShapeReporter(t *testing.T) {
	const anchor = "/Users/eva/.officraft/warden/officraft"
	rec := newCutoverRec()
	rec.ppidExe[4242] = "/sbin/launchd"
	bindCutoverOps(t, rec)

	report := newShapeReporter(anchor, 4242)
	if got := report(); got != "legacy" {
		t.Errorf("first sample = %q, want %q", got, "legacy")
	}

	rec.ppidExe[4242] = anchor
	if got := report(); got != "anchor" {
		t.Errorf("sample after the conversion = %q, want %q — the shape is re-read every cycle", got, "anchor")
	}

	blind := newShapeReporter("", 4242)
	if got := blind(); got != "unknown" {
		t.Errorf("a reporter with no anchor path = %q, want %q", got, "unknown")
	}
	if runs := len(rec.runs); runs != 0 {
		t.Errorf("the reporter ran %d commands of its own, want 0", runs)
	}
}

func TestEnsureAnchorPresent(t *testing.T) {
	root := "/Users/eva/.officraft"
	p := cutoverPaths(root)
	probe := p.anchorPath + ".probe"

	t.Run("an anchor already on disk is left exactly as it is", func(t *testing.T) {
		rec := newCutoverRec()
		rec.modTimes[p.anchorPath] = time.Unix(1700000000, 0)
		rec.files[p.anchorPath] = "the identity"
		var log []string

		if err := ensureAnchorPresent(rec.ops(), p, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatalf("ensureAnchorPresent = %v, want nil", err)
		}
		if len(rec.writes) != 0 || len(rec.links) != 0 || len(rec.removes) != 0 || len(rec.runExits) != 0 {
			t.Errorf("an existing anchor was touched: writes=%v links=%v removes=%v preflights=%v",
				rec.writes, rec.links, rec.removes, rec.runExits)
		}
		if rec.files[p.anchorPath] != "the identity" {
			t.Errorf("anchor bytes = %q, want them untouched", rec.files[p.anchorPath])
		}
		if len(log) != 0 {
			t.Errorf("log = %#v, want silence", log)
		}
	})

	t.Run("a machine with no anchor gets one staged, probed and promoted", func(t *testing.T) {
		rec := newCutoverRec()
		rec.files[p.anchorSrc] = "anchor-bytes"
		rec.runExitCode[argvKey(probe, "--preflight")] = 2
		var log []string

		if err := ensureAnchorPresent(rec.ops(), p, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatalf("ensureAnchorPresent = %v, want nil", err)
		}
		if want := []string{probe + " 0755 12"}; !reflect.DeepEqual(rec.writes, want) {
			t.Errorf("writes = %v, want %v", rec.writes, want)
		}
		if want := []string{probe + " 0755"}; !reflect.DeepEqual(rec.chmods, want) {
			t.Errorf("chmods = %v, want %v", rec.chmods, want)
		}
		if want := []string{argvKey(probe, "--preflight")}; !reflect.DeepEqual(rec.runExits, want) {
			t.Errorf("preflights = %v, want %v — the probe is what gets proven", rec.runExits, want)
		}
		if want := [][2]string{{probe, p.anchorPath}}; !reflect.DeepEqual(rec.links, want) {
			t.Errorf("links = %v, want %v", rec.links, want)
		}
		if want := []string{probe}; !reflect.DeepEqual(rec.removes, want) {
			t.Errorf("removes = %v, want %v — only the probe is cleaned up", rec.removes, want)
		}
		if rec.files[p.anchorPath] != "anchor-bytes" {
			t.Errorf("promoted anchor = %q, want %q", rec.files[p.anchorPath], "anchor-bytes")
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] anchor cutover: no anchor on this machine; staged, probed and promoted %s (12 bytes)", p.anchorPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("an unreadable source falls back to the embedded anchor", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runExitCode[argvKey(probe, "--preflight")] = 2
		previous := embeddedAnchor
		embeddedAnchor = func() []byte { return []byte("embedded-anchor") }
		t.Cleanup(func() { embeddedAnchor = previous })

		if err := ensureAnchorPresent(rec.ops(), p, func(string, ...any) {}); err != nil {
			t.Fatalf("ensureAnchorPresent = %v, want nil", err)
		}
		if rec.files[p.anchorPath] != "embedded-anchor" {
			t.Errorf("promoted anchor = %q, want the embedded bytes", rec.files[p.anchorPath])
		}
	})

	t.Run("an empty source file is not a usable anchor either", func(t *testing.T) {
		rec := newCutoverRec()
		rec.files[p.anchorSrc] = ""
		rec.runExitCode[argvKey(probe, "--preflight")] = 2
		previous := embeddedAnchor
		embeddedAnchor = func() []byte { return []byte("embedded-anchor") }
		t.Cleanup(func() { embeddedAnchor = previous })

		if err := ensureAnchorPresent(rec.ops(), p, func(string, ...any) {}); err != nil {
			t.Fatalf("ensureAnchorPresent = %v, want nil", err)
		}
		if rec.files[p.anchorPath] != "embedded-anchor" {
			t.Errorf("promoted anchor = %q, want the embedded bytes", rec.files[p.anchorPath])
		}
	})

	t.Run("a build with no anchor anywhere refuses without touching the disk", func(t *testing.T) {
		rec := newCutoverRec()
		previous := embeddedAnchor
		embeddedAnchor = func() []byte { return nil }
		t.Cleanup(func() { embeddedAnchor = previous })

		err := ensureAnchorPresent(rec.ops(), p, func(string, ...any) {})

		want := fmt.Sprintf("no anchor at %s, no usable source at %s, and this ocwarden carries no embedded anchor",
			p.anchorPath, p.anchorSrc)
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if len(rec.writes) != 0 || len(rec.links) != 0 {
			t.Errorf("writes=%v links=%v, want nothing staged", rec.writes, rec.links)
		}
	})

	t.Run("every staging failure leaves no anchor and cleans the probe up", func(t *testing.T) {
		cases := []struct {
			name string
			mut  func(*cutoverRec)
			want string
		}{
			{"the probe cannot be written",
				func(r *cutoverRec) { r.writeErr[probe] = errors.New("read-only file system") },
				fmt.Sprintf("stage anchor at %s: read-only file system", probe)},
			{"the probe cannot be made executable",
				func(r *cutoverRec) { r.chmodErr[probe] = errors.New("operation not permitted") },
				fmt.Sprintf("make the staged anchor %s executable: operation not permitted", probe)},
			{"the probe does not pass the preflight",
				func(r *cutoverRec) { r.runExitCode[argvKey(probe, "--preflight")] = 0 },
				fmt.Sprintf("the anchor this ocwarden would deploy does not satisfy the preflight: "+
					"anchor preflight: %s exited 0, want 2 (a build whose anchor does not reject arguments is not the anchor this expects)", probe)},
			{"the probe cannot be executed at all",
				func(r *cutoverRec) { r.runExitErr[argvKey(probe, "--preflight")] = errors.New("permission denied") },
				fmt.Sprintf("the anchor this ocwarden would deploy does not satisfy the preflight: "+
					"anchor preflight: cannot execute %s: permission denied", probe)},
			{"the probe cannot be promoted",
				func(r *cutoverRec) {
					r.runExitCode[argvKey(probe, "--preflight")] = 2
					r.linkErr = errors.New("cross-device link")
				},
				fmt.Sprintf("promote the staged anchor to %s: cross-device link", p.anchorPath)},
		}
		for _, c := range cases {
			rec := newCutoverRec()
			rec.files[p.anchorSrc] = "anchor-bytes"
			c.mut(rec)

			err := ensureAnchorPresent(rec.ops(), p, func(string, ...any) {})

			if err == nil || err.Error() != c.want {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
			}
			if _, promoted := rec.files[p.anchorPath]; promoted {
				t.Errorf("%s: a failed staging still left an anchor behind", c.name)
			}
			if want := []string{probe}; !reflect.DeepEqual(rec.removes, want) {
				t.Errorf("%s: removes = %v, want %v", c.name, rec.removes, want)
			}
		}
	})

	t.Run("an anchor that appeared mid-staging is kept, not replaced", func(t *testing.T) {
		rec := newCutoverRec()
		rec.files[p.anchorSrc] = "anchor-bytes"
		rec.runExitCode[argvKey(probe, "--preflight")] = 2
		rec.linkErr = os.ErrExist
		var log []string

		if err := ensureAnchorPresent(rec.ops(), p, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatalf("ensureAnchorPresent = %v, want nil", err)
		}
		if len(rec.links) != 0 {
			t.Errorf("links = %v, want none to have taken", rec.links)
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] anchor cutover: %s appeared while this run was staging one; keeping the existing anchor", p.anchorPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
		if want := []string{probe}; !reflect.DeepEqual(rec.removes, want) {
			t.Errorf("removes = %v, want %v", rec.removes, want)
		}
	})
}

func TestAnchorPreflight(t *testing.T) {
	const anchor = "/Users/eva/.officraft/warden/officraft"
	t.Run("exit 2 is the anchor answering, and nothing else happens", func(t *testing.T) {
		rec := newCutoverRec()
		rec.runExitCode[argvKey(anchor, "--preflight")] = 2

		if err := anchorPreflight(rec.ops(), anchor); err != nil {
			t.Fatalf("anchorPreflight = %v, want nil", err)
		}
		if want := []string{argvKey(anchor, "--preflight")}; !reflect.DeepEqual(rec.runExits, want) {
			t.Errorf("ran %v, want %v", rec.runExits, want)
		}
		if len(rec.runs) != 0 || len(rec.writes) != 0 || len(rec.spawns) != 0 {
			t.Errorf("the preflight had side effects: runs=%v writes=%v spawns=%v", rec.runs, rec.writes, rec.spawns)
		}
	})

	for _, code := range []int{0, 1, 3, 127} {
		rec := newCutoverRec()
		rec.runExitCode[argvKey(anchor, "--preflight")] = code
		want := fmt.Sprintf("anchor preflight: %s exited %d, want 2 "+
			"(a build whose anchor does not reject arguments is not the anchor this expects)", anchor, code)
		if err := anchorPreflight(rec.ops(), anchor); err == nil || err.Error() != want {
			t.Errorf("exit %d: err = %v, want %q", code, err, want)
		}
	}

	rec := newCutoverRec()
	rec.runExitErr[argvKey(anchor, "--preflight")] = errors.New("operation not permitted")
	want := fmt.Sprintf("anchor preflight: cannot execute %s: operation not permitted", anchor)
	if err := anchorPreflight(rec.ops(), anchor); err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestAcquireCutoverLock(t *testing.T) {
	const lock = "/Users/eva/.officraft/warden/cutover.lock"

	t.Run("a free lock is taken", func(t *testing.T) {
		rec := newCutoverRec()
		if !acquireCutoverLock(rec.ops(), lock) {
			t.Fatal("acquireCutoverLock = false, want true")
		}
		if !rec.created[lock] {
			t.Error("the lock file was not created")
		}
		if len(rec.removes) != 0 {
			t.Errorf("removes = %v, want none", rec.removes)
		}
	})

	t.Run("a lock held by a live conversion is refused", func(t *testing.T) {
		rec := newCutoverRec()
		rec.created[lock] = true
		rec.modTimes[lock] = time.Now().Add(-time.Minute)

		if acquireCutoverLock(rec.ops(), lock) {
			t.Error("acquireCutoverLock = true, want false while another conversion holds it")
		}
		if len(rec.removes) != 0 {
			t.Errorf("a live lock was removed: %v", rec.removes)
		}
	})

	t.Run("a corpse older than the stale age is aged out and retaken", func(t *testing.T) {
		rec := newCutoverRec()
		rec.created[lock] = true
		rec.modTimes[lock] = time.Now().Add(-staleLockAge - time.Second)

		if !acquireCutoverLock(rec.ops(), lock) {
			t.Fatal("acquireCutoverLock = false, want true for a stale lock")
		}
		if want := []string{lock}; !reflect.DeepEqual(rec.removes, want) {
			t.Errorf("removes = %v, want %v", rec.removes, want)
		}
		if !rec.created[lock] {
			t.Error("the lock was not retaken after the corpse was cleared")
		}
	})

	t.Run("a lock whose age cannot be read is treated as live", func(t *testing.T) {
		rec := newCutoverRec()
		rec.created[lock] = true

		if acquireCutoverLock(rec.ops(), lock) {
			t.Error("acquireCutoverLock = true, want false when the lock's age is unreadable")
		}
		if len(rec.removes) != 0 {
			t.Errorf("removes = %v, want none", rec.removes)
		}
	})

	t.Run("a stale lock that cannot be removed is still a refusal", func(t *testing.T) {
		rec := newCutoverRec()
		rec.created[lock] = true
		rec.modTimes[lock] = time.Now().Add(-staleLockAge - time.Second)
		rec.removeErr[lock] = errors.New("permission denied")

		if acquireCutoverLock(rec.ops(), lock) {
			t.Error("acquireCutoverLock = true, want false when the corpse could not be cleared")
		}
	})

	t.Run("a create that fails outright is a refusal", func(t *testing.T) {
		rec := newCutoverRec()
		rec.createErr[lock] = errors.New("read-only file system")

		if acquireCutoverLock(rec.ops(), lock) {
			t.Error("acquireCutoverLock = true, want false when the lock cannot be created")
		}
	})
}

func TestMaybeStartAnchorCutover(t *testing.T) {
	root := "/Users/eva/.officraft"
	p := cutoverPaths(root)
	const ppid = 4242
	lockPath := filepath.Join(root, "warden", "cutover.lock")
	failedPath := filepath.Join(root, "warden", "cutover.failed")
	logPath := filepath.Join(p.logDir, "cutover.log")

	// legacyRec is a machine launchd is running the OLD way, with an anchor
	// already on disk that passes its preflight.
	legacyRec := func() *cutoverRec {
		rec := newCutoverRec()
		rec.ppidExe[ppid] = "/sbin/launchd"
		rec.modTimes[p.anchorPath] = time.Unix(1700000000, 0)
		rec.runExitCode[argvKey(p.anchorPath, "--preflight")] = 2
		return rec
	}

	t.Run("a legacy machine starts a detached converter under the lock", func(t *testing.T) {
		rec := legacyRec()
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 1 {
			t.Fatalf("spawns = %#v, want exactly one converter", rec.spawns)
		}
		got := rec.spawns[0]
		if got.bin != p.binPath || !reflect.DeepEqual(got.args, []string{"cutover-anchor"}) || got.logPath != logPath {
			t.Errorf("spawn = (%q, %v, %q), want (%q, [cutover-anchor], %q)",
				got.bin, got.args, got.logPath, p.binPath, logPath)
		}
		for _, want := range []string{
			"OC_BASE=https://station.example",
			"OC_TOKEN=" + jwtWardenOne,
			"OC_CUTOVER_LOCK=" + lockPath,
		} {
			if !slicesContain(got.env, want) {
				t.Errorf("the converter's env is missing %q", want)
			}
		}
		for _, unwanted := range got.env {
			if strings.HasPrefix(unwanted, "OC_NAMESPACE=") {
				t.Errorf("the main instance passed %q, want no namespace at all", unwanted)
			}
		}
		if !rec.created[lockPath] {
			t.Error("the converter was started without the lock being taken")
		}
		if len(rec.removes) != 0 {
			t.Errorf("removes = %v, want the lock left for the converter to release", rec.removes)
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] anchor cutover: legacy shape detected; detached converter started (log: %s)", logPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("a namespaced instance carries its namespace to the converter", func(t *testing.T) {
		rec := legacyRec()
		bindCutoverOps(t, rec)
		namespaced := p
		namespaced.namespace = "beta"

		maybeStartAnchorCutover(namespaced, ppid, func(string, ...any) {})

		if len(rec.spawns) != 1 {
			t.Fatalf("spawns = %#v, want one", rec.spawns)
		}
		if !slicesContain(rec.spawns[0].env, "OC_NAMESPACE=beta") {
			t.Error("the converter was started without OC_NAMESPACE=beta")
		}
	})

	t.Run("a machine that is not on the legacy shape is left alone", func(t *testing.T) {
		for _, c := range []struct {
			name string
			exe  string
		}{
			{"already converted", p.anchorPath},
			{"an unclassifiable parent", "/bin/zsh"},
		} {
			rec := legacyRec()
			rec.ppidExe[ppid] = c.exe
			bindCutoverOps(t, rec)
			var log []string

			maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

			if len(rec.spawns) != 0 || len(rec.created) != 0 || len(log) != 0 {
				t.Errorf("%s: spawns=%v locks=%v log=%#v, want nothing at all", c.name, rec.spawns, rec.created, log)
			}
		}
	})

	t.Run("a machine that already rolled back once never tries again", func(t *testing.T) {
		rec := legacyRec()
		rec.files[failedPath] = "2026-07-31T00:00:00Z\ninstall failed\n"
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 0 || len(rec.created) != 0 {
			t.Errorf("spawns=%v locks=%v, want nothing", rec.spawns, rec.created)
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] anchor cutover: skipped — a previous attempt rolled back (%s)", failedPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("no deployable anchor means no conversion", func(t *testing.T) {
		rec := legacyRec()
		delete(rec.modTimes, p.anchorPath)
		previous := embeddedAnchor
		embeddedAnchor = func() []byte { return nil }
		t.Cleanup(func() { embeddedAnchor = previous })
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 0 || len(rec.created) != 0 {
			t.Errorf("spawns=%v locks=%v, want nothing", rec.spawns, rec.created)
		}
		wantLog := []string{fmt.Sprintf(
			"[ocwarden] anchor cutover: skipped — no anchor at %s, no usable source at %s, and this ocwarden carries no embedded anchor",
			p.anchorPath, p.anchorSrc)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("an anchor that fails its preflight stops the conversion before the lock", func(t *testing.T) {
		rec := legacyRec()
		rec.runExitCode[argvKey(p.anchorPath, "--preflight")] = 0
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 0 || len(rec.created) != 0 {
			t.Errorf("spawns=%v locks=%v, want nothing", rec.spawns, rec.created)
		}
		wantLog := []string{fmt.Sprintf("[ocwarden] anchor cutover: skipped — anchor preflight: %s exited 0, "+
			"want 2 (a build whose anchor does not reject arguments is not the anchor this expects)", p.anchorPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("a conversion already in flight is not joined", func(t *testing.T) {
		rec := legacyRec()
		rec.created[lockPath] = true
		rec.modTimes[lockPath] = time.Now().Add(-time.Minute)
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 0 {
			t.Errorf("spawns = %v, want none", rec.spawns)
		}
		wantLog := []string{fmt.Sprintf("[ocwarden] anchor cutover: skipped — another conversion holds %s", lockPath)}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})

	t.Run("a converter that could not be started releases the lock it took", func(t *testing.T) {
		rec := legacyRec()
		rec.spawnErr = errors.New("no such file or directory")
		bindCutoverOps(t, rec)
		var log []string

		maybeStartAnchorCutover(p, ppid, func(f string, a ...any) { log = append(log, fmt.Sprintf(f, a...)) })

		if len(rec.spawns) != 0 {
			t.Errorf("spawns = %v, want none", rec.spawns)
		}
		if want := []string{lockPath}; !reflect.DeepEqual(rec.removes, want) {
			t.Errorf("removes = %v, want %v — a lock nobody holds must not block the next start", rec.removes, want)
		}
		wantLog := []string{"[ocwarden] anchor cutover: could not start converter: no such file or directory"}
		if !reflect.DeepEqual(log, wantLog) {
			t.Errorf("log = %#v, want %#v", log, wantLog)
		}
	})
}

func slicesContain(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
