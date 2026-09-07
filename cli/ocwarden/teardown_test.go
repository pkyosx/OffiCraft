package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// teardownFixture is the resolved main-instance teardown target.
func teardownFixture() teardownPaths {
	return teardownPaths{
		tokfile:   "/Users/eva/.officraft/warden/exec-warden.tok",
		plistPath: "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.plist",
		guiDomain: "gui/501",
		label:     "com.officraft.ocwarden",
	}
}

func TestResolvedLabel(t *testing.T) {
	t.Skip("a two-line fallback; both branches are asserted through the launchctl argv doTeardown issues in TestDoTeardown")
}

func TestResolveTeardownPaths(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want teardownPaths
	}{
		{"main instance", map[string]string{"HOME": "/Users/eva"}, teardownFixture()},
		{"namespaced instance", map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "lab"}, teardownPaths{
			tokfile:   "/Users/eva/.officraft-lab/warden/exec-warden.tok",
			plistPath: "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.lab.plist",
			guiDomain: "gui/501",
			label:     "com.officraft.ocwarden.lab",
		}},
		{"identity is irrelevant to removal", map[string]string{
			"HOME": "/Users/eva", "OC_BASE": "https://oc.example.com", "OC_TOKEN": jwtWardenOne, "OC_ID": "machine-7",
		}, teardownFixture()},
	}
	for _, c := range cases {
		got, err := resolveTeardownPaths(envMap(c.env), 501)
		if err != nil {
			t.Errorf("%s: err = %v, want nil", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: paths = %+v, want %+v", c.name, got, c.want)
		}
	}

	refused := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"HOME unset", map[string]string{}, "HOME must be set"},
		{"invalid namespace", map[string]string{"HOME": "/Users/eva", "OC_NAMESPACE": "Lab"},
			`OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
	}
	for _, c := range refused {
		got, err := resolveTeardownPaths(envMap(c.env), 501)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if !reflect.DeepEqual(got, teardownPaths{}) {
			t.Errorf("%s: a refusal still derived %+v", c.name, got)
		}
	}
}

func TestRemoveFileTo(t *testing.T) {
	path := "/Users/eva/.officraft/warden/exec-warden.tok"

	cases := []struct {
		name        string
		dryRun      bool
		present     bool
		removeErr   error
		wantRemoved bool
		wantLog     string
		wantCalls   []string
	}{
		{name: "a present file is removed", present: true, wantRemoved: true,
			wantLog: "removed: " + path + "\n", wantCalls: []string{"remove " + path}},
		{name: "an absent file is already done", wantRemoved: true,
			wantLog: "already absent: " + path + "\n", wantCalls: []string{"remove " + path}},
		{name: "a stubborn file is reported and survived", present: true,
			removeErr: errors.New("operation not permitted"), wantRemoved: false,
			wantLog:   "warning: could not remove " + path + ": operation not permitted (continuing)\n",
			wantCalls: []string{"remove " + path}},
		{name: "a dry run removes nothing", dryRun: true, present: true, wantRemoved: true,
			wantLog: "DRYRUN would remove: " + path + "\n"},
	}
	for _, c := range cases {
		sys := newSysRecorder()
		if c.present {
			sys.files[path] = "content"
		}
		if c.removeErr != nil {
			sys.fail["remove "+path] = c.removeErr
		}
		var log bytes.Buffer
		logf := func(format string, a ...any) { fmt.Fprintf(&log, format+"\n", a...) }

		if got := removeFileTo(logf, sys.ops(), c.dryRun, path); got != c.wantRemoved {
			t.Errorf("%s: removeFileTo = %v, want %v", c.name, got, c.wantRemoved)
		}
		if log.String() != c.wantLog {
			t.Errorf("%s: log = %q, want %q", c.name, log.String(), c.wantLog)
		}
		if !reflect.DeepEqual(sys.calls, c.wantCalls) {
			t.Errorf("%s: calls = %v, want %v", c.name, sys.calls, c.wantCalls)
		}
		if _, left := sys.files[path]; c.dryRun && !left {
			t.Errorf("%s: a dry run deleted the file", c.name)
		}
	}
}

func TestRemoveFile(t *testing.T) {
	t.Skip("a one-line shim onto removeFileTo (i.logf, i.sys, i.dryRun); its transcript is asserted through TestRunTeardown")
}

func TestDoTeardown(t *testing.T) {
	p := teardownFixture()
	target := "gui/501/com.officraft.ocwarden"
	notLoaded := errors.New("Could not find service in domain")

	t.Run("a live install is booted out and its artifacts removed", func(t *testing.T) {
		sys := newSysRecorder()
		sys.files[p.tokfile] = jwtWardenOne
		sys.files[p.plistPath] = minimalPlist
		sys.run = func(argv string, _ int) (string, error) {
			if argv == "launchctl print "+target {
				return "", notLoaded
			}
			return "", nil
		}
		ok, log := doTeardown(sys.ops(), false, p)
		if !ok {
			t.Error("ok = false, want true")
		}
		wantCalls := []string{
			"run launchctl bootout " + target,
			"run launchctl print " + target,
			"remove " + p.tokfile,
			"remove " + p.plistPath,
		}
		if !reflect.DeepEqual(sys.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
		}
		if len(sys.files) != 0 {
			t.Errorf("teardown left %v behind", sys.files)
		}
		want := "[ocwarden teardown] booted out " + target + " — CONFIRMED gone from launchd (exact label; tolerated if not loaded; never pkill)\n" +
			"[ocwarden teardown] removed: " + p.tokfile + "\n" +
			"[ocwarden teardown] removed: " + p.plistPath + "\n" +
			"[ocwarden teardown] teardown complete for com.officraft.ocwarden\n"
		if log != want {
			t.Errorf("log =\n%s\nwant\n%s", log, want)
		}
	})

	t.Run("a namespaced instance acts on its own label only", func(t *testing.T) {
		ns := teardownPaths{
			tokfile:   "/Users/eva/.officraft-lab/warden/exec-warden.tok",
			plistPath: "/Users/eva/Library/LaunchAgents/com.officraft.ocwarden.lab.plist",
			guiDomain: "gui/501",
			label:     "com.officraft.ocwarden.lab",
		}
		sys := newSysRecorder()
		sys.run = func(string, int) (string, error) { return "", notLoaded }
		ok, log := doTeardown(sys.ops(), false, ns)
		if !ok {
			t.Error("ok = false, want true")
		}
		wantCalls := []string{
			"run launchctl bootout gui/501/com.officraft.ocwarden.lab",
			"run launchctl print gui/501/com.officraft.ocwarden.lab",
			"remove " + ns.tokfile,
			"remove " + ns.plistPath,
		}
		if !reflect.DeepEqual(sys.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", sys.calls, wantCalls)
		}
		want := "[ocwarden teardown] booted out gui/501/com.officraft.ocwarden.lab — CONFIRMED gone from launchd (exact label; tolerated if not loaded; never pkill)\n" +
			"[ocwarden teardown] already absent: " + ns.tokfile + "\n" +
			"[ocwarden teardown] already absent: " + ns.plistPath + "\n" +
			"[ocwarden teardown] teardown complete for com.officraft.ocwarden.lab\n"
		if log != want {
			t.Errorf("log =\n%s\nwant\n%s", log, want)
		}
	})

	t.Run("a zero-value label falls back to the canonical one", func(t *testing.T) {
		zero := teardownFixture()
		zero.label = ""
		sys := newSysRecorder()
		sys.run = func(string, int) (string, error) { return "", notLoaded }
		ok, log := doTeardown(sys.ops(), false, zero)
		if !ok {
			t.Error("ok = false, want true")
		}
		if want := "run launchctl bootout " + target; sys.calls[0] != want {
			t.Errorf("first call = %q, want %q", sys.calls[0], want)
		}
		if !bytes.Contains([]byte(log), []byte("teardown complete for com.officraft.ocwarden\n")) {
			t.Errorf("log =\n%s\nwant it to name the canonical label", log)
		}
	})

	t.Run("a label that lingers is not a confirmed teardown", func(t *testing.T) {
		sys := newSysRecorder()
		sys.run = func(string, int) (string, error) { return "state = running", nil }
		ok, log := doTeardown(sys.ops(), false, p)
		if ok {
			t.Error("ok = true, want false — the bootout was never confirmed")
		}
		if n := sys.count("run launchctl print " + target); n != 25 {
			t.Errorf("%d print probes, want 25", n)
		}
		if n := sys.count("sleep 200ms"); n != 25 {
			t.Errorf("%d sleeps, want 25", n)
		}
		want := "[ocwarden teardown] bootout of " + target + " NOT confirmed: label still registered after ~5s bounded poll\n" +
			"[ocwarden teardown] already absent: " + p.tokfile + "\n" +
			"[ocwarden teardown] already absent: " + p.plistPath + "\n" +
			"[ocwarden teardown] teardown INCOMPLETE for com.officraft.ocwarden — the launchd bootout was not confirmed or a required artifact could not be removed\n"
		if log != want {
			t.Errorf("log =\n%s\nwant\n%s", log, want)
		}
	})

	t.Run("a stubborn artifact is not a confirmed teardown", func(t *testing.T) {
		sys := newSysRecorder()
		sys.files[p.tokfile] = jwtWardenOne
		sys.files[p.plistPath] = minimalPlist
		sys.fail["remove "+p.tokfile] = errors.New("operation not permitted")
		sys.run = func(argv string, _ int) (string, error) {
			if argv == "launchctl print "+target {
				return "", notLoaded
			}
			return "", nil
		}
		ok, log := doTeardown(sys.ops(), false, p)
		if ok {
			t.Error("ok = true, want false — an artifact survived")
		}
		if _, left := sys.files[p.tokfile]; !left {
			t.Error("the stubborn file was reported as surviving but is gone")
		}
		if _, left := sys.files[p.plistPath]; left {
			t.Error("teardown stopped at the stubborn file instead of continuing")
		}
		want := "[ocwarden teardown] booted out " + target + " — CONFIRMED gone from launchd (exact label; tolerated if not loaded; never pkill)\n" +
			"[ocwarden teardown] warning: could not remove " + p.tokfile + ": operation not permitted (continuing)\n" +
			"[ocwarden teardown] removed: " + p.plistPath + "\n" +
			"[ocwarden teardown] teardown INCOMPLETE for com.officraft.ocwarden — the launchd bootout was not confirmed or a required artifact could not be removed\n"
		if log != want {
			t.Errorf("log =\n%s\nwant\n%s", log, want)
		}
	})

	t.Run("a dry run touches nothing", func(t *testing.T) {
		sys := newSysRecorder()
		sys.files[p.tokfile] = jwtWardenOne
		sys.files[p.plistPath] = minimalPlist
		ok, log := doTeardown(sys.ops(), true, p)
		if !ok {
			t.Error("ok = false, want true")
		}
		if len(sys.calls) != 0 {
			t.Errorf("a dry run did %v", sys.calls)
		}
		if len(sys.files) != 2 {
			t.Errorf("a dry run removed files: %v", sys.files)
		}
		want := "[ocwarden teardown] DRYRUN would run: launchctl bootout " + target + "  (tolerate not-loaded; stops the process via launchd, never pkill)\n" +
			"[ocwarden teardown] DRYRUN would: poll `launchctl print " + target + "` until the label is gone (bootout is async; bounded ~5s)\n" +
			"[ocwarden teardown] DRYRUN would remove: " + p.tokfile + "\n" +
			"[ocwarden teardown] DRYRUN would remove: " + p.plistPath + "\n" +
			"[ocwarden teardown] DRYRUN complete — no machine state changed.\n"
		if log != want {
			t.Errorf("log =\n%s\nwant\n%s", log, want)
		}
	})
}

func TestRunTeardown(t *testing.T) {
	p := teardownFixture()
	target := "gui/501/com.officraft.ocwarden"

	sys := newSysRecorder()
	sys.run = func(argv string, _ int) (string, error) {
		if argv == "launchctl print "+target {
			return "", errors.New("Could not find service in domain")
		}
		return "", nil
	}
	out := &bytes.Buffer{}
	i := &installer{out: out, tag: "teardown", sys: sys.ops()}

	if ok := i.runTeardown(p); !ok {
		t.Error("runTeardown = false, want true — a fully-absent install is an idempotent success")
	}
	want := "[ocwarden teardown] booted out " + target + " — CONFIRMED gone from launchd (exact label; tolerated if not loaded; never pkill)\n" +
		"[ocwarden teardown] already absent: " + p.tokfile + "\n" +
		"[ocwarden teardown] already absent: " + p.plistPath + "\n" +
		"[ocwarden teardown] teardown complete for com.officraft.ocwarden\n"
	if out.String() != want {
		t.Errorf("streamed transcript =\n%s\nwant\n%s", out.String(), want)
	}

	stubborn := newSysRecorder()
	stubborn.files[p.tokfile] = jwtWardenOne
	stubborn.fail["remove "+p.tokfile] = os.ErrPermission
	stubborn.run = func(argv string, _ int) (string, error) {
		if argv == "launchctl print "+target {
			return "", errors.New("Could not find service in domain")
		}
		return "", nil
	}
	unconfirmed := &installer{out: &bytes.Buffer{}, tag: "teardown", sys: stubborn.ops()}
	if ok := unconfirmed.runTeardown(p); ok {
		t.Error("runTeardown = true, want false — an artifact survived")
	}
}

func TestValidateTeardownTarget(t *testing.T) {
	canonicalRefusal := "refusing: this would tear down the CANONICAL warden on this host " +
		"(launchd com.officraft.ocwarden, its exec token and plist) and stop every agent it " +
		"supervises. For an isolated instance set OC_NAMESPACE=<ns>; if destroying the " +
		"canonical warden is genuinely intended, authorize it explicitly with --canonical"

	cases := []struct {
		name      string
		env       map[string]string
		canonical bool
		want      string
	}{
		{"an implicit canonical target is refused", map[string]string{}, false, canonicalRefusal},
		{"the canonical target is spelled out", map[string]string{}, true, ""},
		{"a namespaced target is unambiguous from its env", map[string]string{"OC_NAMESPACE": "lab"}, false, ""},
		{"--canonical conflicts with a namespace", map[string]string{"OC_NAMESPACE": "lab"}, true,
			`refusing: --canonical conflicts with OC_NAMESPACE="lab"`},
		{"an invalid namespace is refused", map[string]string{"OC_NAMESPACE": "Lab"}, false,
			`OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
		{"an invalid namespace is refused even with --canonical", map[string]string{"OC_NAMESPACE": "Lab"}, true,
			`OC_NAMESPACE must match [a-z0-9-]{1,16}, got: "Lab"`},
	}
	for _, c := range cases {
		err := validateTeardownTarget(envMap(c.env), c.canonical)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", c.name, err)
		case c.want != "" && (err == nil || err.Error() != c.want):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestTeardownCmd(t *testing.T) {
	t.Skip("the entry point builds its effects from newHostSeam(), which this package binds to realHostSeam — calling it from a test binary is a deliberate os.Exit(1) (TestRealHostSeam) that would take the whole test process with it")
}
