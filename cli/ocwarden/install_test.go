package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// fakeSys — the install/teardown side-effect seam mock. It records every call so a
// test asserts the EXACT launchctl sequence + file ops with zero subprocess, zero
// launchctl, zero filesystem mutation on the real machine.
// ---------------------------------------------------------------------------

type recordedRun struct {
	name string
	args []string
}

type fakeSys struct {
	runs     []recordedRun
	runFn    func(name string, args ...string) (string, error)
	writes   map[string][]byte
	modes    map[string]os.FileMode
	existing map[string][]byte // pre-seeded readable files (guard tokfile, copy source)
	renames  [][2]string
	removed  []string
	mkdirs   []string
	slept    int
	// injection hooks
	renameErr map[string]error
	removeErr map[string]error
}

func newFakeSys() *fakeSys {
	return &fakeSys{
		writes:    map[string][]byte{},
		modes:     map[string]os.FileMode{},
		existing:  map[string][]byte{},
		renameErr: map[string]error{},
		removeErr: map[string]error{},
	}
}

func (f *fakeSys) ops() sysOps {
	return sysOps{
		run: func(name string, args ...string) (string, error) {
			f.runs = append(f.runs, recordedRun{name, args})
			if f.runFn != nil {
				return f.runFn(name, args...)
			}
			return "", nil
		},
		mkdirAll: func(path string, _ os.FileMode) error {
			f.mkdirs = append(f.mkdirs, path)
			return nil
		},
		writeFile: func(path string, data []byte, perm os.FileMode) error {
			f.writes[path] = data
			f.modes[path] = perm
			return nil
		},
		readFile: func(path string) ([]byte, error) {
			if d, ok := f.existing[path]; ok {
				return d, nil
			}
			if d, ok := f.writes[path]; ok {
				return d, nil
			}
			return nil, os.ErrNotExist
		},
		rename: func(oldpath, newpath string) error {
			if err := f.renameErr[newpath]; err != nil {
				return err
			}
			f.renames = append(f.renames, [2]string{oldpath, newpath})
			if d, ok := f.writes[oldpath]; ok {
				f.writes[newpath] = d
				delete(f.writes, oldpath)
			}
			if m, ok := f.modes[oldpath]; ok {
				f.modes[newpath] = m
				delete(f.modes, oldpath)
			}
			return nil
		},
		remove: func(path string) error {
			if err := f.removeErr[path]; err != nil {
				return err
			}
			f.removed = append(f.removed, path)
			return nil
		},
		chmod: func(path string, mode os.FileMode) error {
			f.modes[path] = mode
			return nil
		},
		statMode: func(path string) (os.FileMode, error) {
			if m, ok := f.modes[path]; ok {
				return m, nil
			}
			return 0, os.ErrNotExist
		},
		sleep: func(_ time.Duration) { f.slept++ }, // counted, instant (no real wait)
	}
}

// ranNames returns the ordered list of subprocess names invoked — used to assert we
// NEVER shell to pkill/killall (only launchctl + plutil are ever legitimate).
func (f *fakeSys) ranNames() []string {
	out := make([]string, len(f.runs))
	for i, r := range f.runs {
		out[i] = r.name
	}
	return out
}

// assertNoForbiddenProcessKill fails if any recorded subprocess is a pattern-kill.
func assertNoForbiddenProcessKill(t *testing.T, f *fakeSys) {
	t.Helper()
	for _, r := range f.runs {
		switch r.name {
		case "pkill", "killall", "kill":
			t.Fatalf("FORBIDDEN process-kill invoked: %s %v — install/teardown must only launchctl by exact label", r.name, r.args)
		case "launchctl", "plutil":
			// allowed
		default:
			t.Fatalf("unexpected subprocess %q %v — only launchctl/plutil are allowed", r.name, r.args)
		}
	}
}

// ---------------------------------------------------------------------------
// resolvePaths
// ---------------------------------------------------------------------------

func envFn(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolvePaths_Defaults(t *testing.T) {
	// The running binary lives in a clone's bin/, but install is self-contained: every
	// durable path resolves under $HOME/.officraft, and the clone path is retained
	// only as srcExe (the copy source).
	p, err := resolvePaths(envFn(map[string]string{
		"HOME":     "/Users/seth",
		"OC_TOKEN": "tok-abc",
	}), "/repo/bin/ocwarden", 501)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.root != "/Users/seth/.officraft" {
		t.Errorf("root = %q, want /Users/seth/.officraft (home data root)", p.root)
	}
	if p.srcExe != "/repo/bin/ocwarden" {
		t.Errorf("srcExe = %q, want the running binary path", p.srcExe)
	}
	if p.ocBase != defaultBase {
		t.Errorf("ocBase = %q, want default %q", p.ocBase, defaultBase)
	}
	if p.tokfile != "/Users/seth/.officraft/warden/exec-warden.tok" {
		t.Errorf("tokfile = %q", p.tokfile)
	}
	if p.plistPath != "/Users/seth/Library/LaunchAgents/com.officraft.ocwarden.plist" {
		t.Errorf("plistPath = %q", p.plistPath)
	}
	if p.binPath != "/Users/seth/.officraft/warden/ocwarden" {
		t.Errorf("binPath = %q, want the stable home target", p.binPath)
	}
	if p.logDir != "/Users/seth/.officraft/warden/log" {
		t.Errorf("logDir = %q", p.logDir)
	}
	if p.guiDomain != "gui/501" {
		t.Errorf("guiDomain = %q, want gui/501", p.guiDomain)
	}
}

// TestResolvePaths_HomeRootedArbitraryBinary proves the durable paths do NOT depend on
// where the binary was curl'd to: /tmp/ocwarden and ./ocwarden both resolve to the
// same $HOME/.officraft home install, differing only in srcExe (the copy source).
func TestResolvePaths_HomeRootedArbitraryBinary(t *testing.T) {
	for _, exe := range []string{"/tmp/ocwarden", "./ocwarden", "/downloads/x/ocwarden"} {
		p, err := resolvePaths(envFn(map[string]string{
			"HOME":     "/Users/seth",
			"OC_TOKEN": "tok-abc",
		}), exe, 501)
		if err != nil {
			t.Fatalf("exe=%s: unexpected error: %v", exe, err)
		}
		if p.srcExe != exe {
			t.Errorf("exe=%s: srcExe = %q, want the running binary path", exe, p.srcExe)
		}
		if p.binPath != "/Users/seth/.officraft/warden/ocwarden" {
			t.Errorf("exe=%s: binPath = %q, want home target", exe, p.binPath)
		}
		if p.root != "/Users/seth/.officraft" {
			t.Errorf("exe=%s: root = %q, want home data root", exe, p.root)
		}
		if p.logDir != "/Users/seth/.officraft/warden/log" {
			t.Errorf("exe=%s: logDir = %q", exe, p.logDir)
		}
		if p.tokfile != "/Users/seth/.officraft/warden/exec-warden.tok" {
			t.Errorf("exe=%s: tokfile = %q", exe, p.tokfile)
		}
	}
}

func TestResolvePaths_TrailingSlashStrippedAndID(t *testing.T) {
	p, err := resolvePaths(envFn(map[string]string{
		"HOME":     "/h",
		"OC_TOKEN": "t",
		"OC_BASE":  "https://sandbox.example.com:8770/",
		"OC_ID":    "member-9",
	}), "/r/bin/ocwarden", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.ocBase != "https://sandbox.example.com:8770" {
		t.Errorf("ocBase = %q, want trailing slash stripped", p.ocBase)
	}
	if p.ocID != "member-9" {
		t.Errorf("ocID = %q, want member-9", p.ocID)
	}
}

func TestResolvePaths_MissingTokenAndHomeAndBadBase(t *testing.T) {
	if _, err := resolvePaths(envFn(map[string]string{"HOME": "/h"}), "/r/bin/ocwarden", 1); err == nil {
		t.Error("expected error when OC_TOKEN missing")
	}
	if _, err := resolvePaths(envFn(map[string]string{"OC_TOKEN": "t"}), "/r/bin/ocwarden", 1); err == nil {
		t.Error("expected error when HOME missing")
	}
	for _, bad := range []string{"ftp://x", "http://has space", `http://x"y`, "http://x<y", "notaurl"} {
		_, err := resolvePaths(envFn(map[string]string{"HOME": "/h", "OC_TOKEN": "t", "OC_BASE": bad}), "/r/bin/ocwarden", 1)
		if err == nil {
			t.Errorf("expected shape error for OC_BASE=%q", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// plist render + XML lint
// ---------------------------------------------------------------------------

func TestRenderPlist_SubstitutesAndIsWellFormed(t *testing.T) {
	p := wardenPaths{
		root: "/repo", home: "/Users/seth", ocBase: "http://127.0.0.1:8770",
		tokfile: "/Users/seth/.officraft/exec-warden.tok",
		logDir:  "/repo/var/log", binPath: "/repo/bin/ocwarden",
	}
	out := renderPlist(p)
	if err := xmlWellFormed(out); err != nil {
		t.Fatalf("rendered plist not well-formed XML: %v", err)
	}
	for _, ph := range []string{"__ROOT__", "__HOME__", "__OC_BASE__"} {
		if strings.Contains(out, ph) {
			t.Errorf("rendered plist still contains placeholder %s", ph)
		}
	}
	must := []string{
		"<string>com.officraft.ocwarden</string>",
		"<array><string>/repo/bin/ocwarden</string><string>run</string></array>",
		"<key>OC_BASE</key><string>http://127.0.0.1:8770</string>",
		"<key>HOME</key><string>/Users/seth</string>",
		"<key>OC_WARDEN_TOKFILE</key><string>/Users/seth/.officraft/exec-warden.tok</string>",
		"/repo/var/log/ocwarden.out.log",
		"/repo/var/log/ocwarden.err.log",
	}
	for _, s := range must {
		if !strings.Contains(out, s) {
			t.Errorf("rendered plist missing %q", s)
		}
	}
}

func TestXMLWellFormed_RejectsMalformed(t *testing.T) {
	if err := xmlWellFormed("<plist><dict></plist>"); err == nil {
		t.Error("expected malformed XML to be rejected")
	}
}

// ---------------------------------------------------------------------------
// writeTokfile — 0600 + atomic temp+rename
// ---------------------------------------------------------------------------

func fixedPaths() wardenPaths {
	return wardenPaths{
		root: "/h/.officraft", home: "/h", srcExe: "/tmp/ocwarden",
		ocBase: "http://127.0.0.1:8770", ocToken: "secret-jwt",
		tokfile:   "/h/.officraft/warden/exec-warden.tok",
		laDir:     "/h/Library/LaunchAgents",
		plistPath: "/h/Library/LaunchAgents/com.officraft.ocwarden.plist",
		logDir:    "/h/.officraft/warden/log", binPath: "/h/.officraft/warden/ocwarden",
		guiDomain: "gui/501",
	}
}

func TestWriteTokfile_AtomicAnd0600(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, sys: f.ops()}
	p := fixedPaths()
	if err := i.writeTokfile(p); err != nil {
		t.Fatalf("writeTokfile: %v", err)
	}
	// token landed at final path with 0600, via a temp + rename (never written
	// directly to the live path).
	if string(f.writes[p.tokfile]) != "secret-jwt" {
		t.Errorf("tokfile content = %q, want secret-jwt", f.writes[p.tokfile])
	}
	if f.modes[p.tokfile] != 0o600 {
		t.Errorf("tokfile mode = %o, want 600", f.modes[p.tokfile])
	}
	if len(f.renames) != 1 {
		t.Fatalf("expected exactly one atomic rename, got %d", len(f.renames))
	}
	if got := f.renames[0][1]; got != p.tokfile {
		t.Errorf("rename dest = %q, want %q", got, p.tokfile)
	}
	if !strings.HasPrefix(f.renames[0][0], "/h/.officraft/warden/.exec-warden.tok.") {
		t.Errorf("rename src = %q, want a temp under the tokfile dir", f.renames[0][0])
	}
}

func TestWriteTokfile_DryRunWritesNothing(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.writeTokfile(fixedPaths()); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 || len(f.renames) != 0 || len(f.mkdirs) != 0 {
		t.Errorf("dry-run must not write/rename/mkdir; got writes=%v renames=%v mkdirs=%v", f.writes, f.renames, f.mkdirs)
	}
}

// ---------------------------------------------------------------------------
// launchctl — EXACT label sequence, bootout tolerance, never pkill
// ---------------------------------------------------------------------------

// labelGoneRunFn makes `launchctl print` report the label unregistered (non-zero
// exit) so the post-bootout poll confirms "gone" on its first probe — the common
// clean-machine path. Other launchctl verbs succeed.
func labelGoneRunFn(name string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "print" {
		return "", fmt.Errorf("Could not find service %q in domain for user gui: 501", args[1])
	}
	return "", nil
}

func TestLaunchctlReinstall_ExactLabelSequence(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn
	i := &installer{out: io.Discard, sys: f.ops()}
	p := fixedPaths()
	if err := i.launchctlReinstall(p); err != nil {
		t.Fatalf("launchctlReinstall: %v", err)
	}
	want := []recordedRun{
		{"launchctl", []string{"bootout", "gui/501/com.officraft.ocwarden"}},
		{"launchctl", []string{"print", "gui/501/com.officraft.ocwarden"}}, // poll-until-gone probe
		{"launchctl", []string{"bootstrap", "gui/501", p.plistPath}},
		{"launchctl", []string{"kickstart", "-k", "gui/501/com.officraft.ocwarden"}},
	}
	if len(f.runs) != len(want) {
		t.Fatalf("runs = %v, want %v", f.runs, want)
	}
	for k := range want {
		if f.runs[k].name != want[k].name || strings.Join(f.runs[k].args, " ") != strings.Join(want[k].args, " ") {
			t.Errorf("run[%d] = %v, want %v", k, f.runs[k], want[k])
		}
	}
	if f.slept != 0 {
		t.Errorf("a label gone on the first probe must not sleep; slept %d times", f.slept)
	}
	assertNoForbiddenProcessKill(t, f)
}

func TestLaunchctlReinstall_BootoutErrorTolerated(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "bootout" {
			return "", fmt.Errorf("Boot-out failed: 3: No such process")
		}
		return labelGoneRunFn(name, args...)
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.launchctlReinstall(fixedPaths()); err != nil {
		t.Fatalf("bootout error must be tolerated (not-loaded), got: %v", err)
	}
}

// TestLaunchctlReinstall_WaitsOutAsyncBootout is THE idempotence regression test for
// the field failure "Bootstrap failed: 5: Input/output error": bootout is async, so
// on a re-install the old registration lingers for a few polls. The installer must
// keep probing `launchctl print` (sleeping between probes) and only bootstrap AFTER
// the label is confirmed gone.
func TestLaunchctlReinstall_WaitsOutAsyncBootout(t *testing.T) {
	f := newFakeSys()
	const lingerPolls = 3
	prints := 0
	f.runFn = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "print" {
			prints++
			if prints <= lingerPolls {
				return "com.officraft.ocwarden = {\n\tpid = 4242\n}", nil // still registered
			}
			return "", fmt.Errorf("Could not find service") // now gone
		}
		return "", nil
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.launchctlReinstall(fixedPaths()); err != nil {
		t.Fatalf("launchctlReinstall: %v", err)
	}
	if prints != lingerPolls+1 {
		t.Errorf("expected %d print probes (linger then gone), got %d", lingerPolls+1, prints)
	}
	if f.slept != lingerPolls {
		t.Errorf("expected %d inter-probe sleeps, got %d", lingerPolls, f.slept)
	}
	// bootstrap must come strictly AFTER the last (gone) probe.
	lastPrint, bootstrapAt := -1, -1
	for k, r := range f.runs {
		if len(r.args) > 0 && r.args[0] == "print" {
			lastPrint = k
		}
		if len(r.args) > 0 && r.args[0] == "bootstrap" {
			bootstrapAt = k
		}
	}
	if bootstrapAt < lastPrint {
		t.Errorf("bootstrap (run %d) raced the lingering label (last print probe at run %d)", bootstrapAt, lastPrint)
	}
	assertNoForbiddenProcessKill(t, f)
}

// TestLaunchctlReinstall_BootstrapsAnywayAfterPollBudget: a label that NEVER
// deregisters within the bounded wait must not brick the install — parity with
// bin/ocserver's drop_and_load, which warns and bootstraps anyway.
func TestLaunchctlReinstall_BootstrapsAnywayAfterPollBudget(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		return "com.officraft.ocwarden = {\n\tpid = 4242\n}", nil // print always succeeds
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.launchctlReinstall(fixedPaths()); err != nil {
		t.Fatalf("exhausted poll budget must warn + proceed, got: %v", err)
	}
	if f.slept != bootoutPollAttempts {
		t.Errorf("expected the full poll budget of %d sleeps, got %d", bootoutPollAttempts, f.slept)
	}
	sawBootstrap := false
	for _, r := range f.runs {
		if len(r.args) > 0 && r.args[0] == "bootstrap" {
			sawBootstrap = true
		}
	}
	if !sawBootstrap {
		t.Fatal("bootstrap must still be attempted after the poll budget is exhausted")
	}
}

func TestLaunchctlReinstall_BootstrapFailureAborts(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "bootstrap" {
			return "", fmt.Errorf("bootstrap failed 5: Input/output error")
		}
		return labelGoneRunFn(name, args...)
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.launchctlReinstall(fixedPaths()); err == nil {
		t.Fatal("bootstrap failure must abort install")
	}
	// kickstart must NOT be attempted after a bootstrap failure.
	for _, r := range f.runs {
		if len(r.args) > 0 && r.args[0] == "kickstart" {
			t.Fatal("kickstart ran despite bootstrap failure")
		}
	}
}

func TestLaunchctlReinstall_DryRunTouchesNothing(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.launchctlReinstall(fixedPaths()); err != nil {
		t.Fatal(err)
	}
	if len(f.runs) != 0 {
		t.Errorf("dry-run must not run launchctl; got %v", f.ranNames())
	}
}

// ---------------------------------------------------------------------------
// verify — pid stability
// ---------------------------------------------------------------------------

func TestVerify_StablePidSucceeds(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		return "com.officraft.ocwarden = {\n\tpid = 4242\n\tstate = running\n}", nil
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.verify(fixedPaths()); err != nil {
		t.Fatalf("stable pid should verify OK, got: %v", err)
	}
}

func TestVerify_CrashLoopDetected(t *testing.T) {
	f := newFakeSys()
	var n int
	f.runFn = func(name string, args ...string) (string, error) {
		n++
		return fmt.Sprintf("pid = %d\n", 1000+n), nil // different pid each poll
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.verify(fixedPaths()); err == nil {
		t.Fatal("changing pid across settle window must be reported as crash-loop")
	}
}

func TestVerify_NoPidFails(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("could not find service") // not loaded → non-zero
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.verify(fixedPaths()); err == nil {
		t.Fatal("no pid within 30s must fail")
	}
}

// ---------------------------------------------------------------------------
// full dry-run install — end-to-end, zero side effects
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// copyBinary — self-contained home install (atomic 0755 temp+rename)
// ---------------------------------------------------------------------------

func TestCopyBinary_AtomicHome0755(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths() // srcExe=/tmp/ocwarden, binPath=/h/.officraft/warden/ocwarden
	f.existing[p.srcExe] = []byte("BINARY-BYTES")
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.copyBinary(p); err != nil {
		t.Fatalf("copyBinary: %v", err)
	}
	if string(f.writes[p.binPath]) != "BINARY-BYTES" {
		t.Errorf("home binary content = %q, want the source bytes", f.writes[p.binPath])
	}
	if f.modes[p.binPath] != 0o755 {
		t.Errorf("home binary mode = %o, want 755", f.modes[p.binPath])
	}
	if len(f.renames) != 1 || f.renames[0][1] != p.binPath {
		t.Fatalf("expected exactly one atomic rename to %q, got %v", p.binPath, f.renames)
	}
	if !strings.HasPrefix(f.renames[0][0], "/h/.officraft/warden/.ocwarden.") {
		t.Errorf("rename src = %q, want a temp under the warden dir", f.renames[0][0])
	}
}

func TestCopyBinary_SkipsWhenAlreadyHomeBinary(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.srcExe = p.binPath // re-run from the installed location
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.copyBinary(p); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 || len(f.renames) != 0 {
		t.Errorf("self-copy must be skipped when srcExe==binPath; got writes=%v renames=%v", f.writes, f.renames)
	}
}

func TestCopyBinary_DryRunCopiesNothing(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("BINARY-BYTES")
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.copyBinary(p); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 || len(f.renames) != 0 || len(f.mkdirs) != 0 {
		t.Errorf("dry-run must not copy; got writes=%v renames=%v mkdirs=%v", f.writes, f.renames, f.mkdirs)
	}
}

// ---------------------------------------------------------------------------
// installOcAgent — self-contained: DEFAULT download from GET /api/agent/binary, or
// OC_AGENT_BIN local-override copy; either way → home sibling (never in the plist env)
// ---------------------------------------------------------------------------

func TestResolvePaths_OcAgentBinSourceAndHomeTarget(t *testing.T) {
	// With OC_AGENT_BIN set, ocAgentSrc carries the local-override source and ocAgentBin
	// is the home sibling of ocwarden (installOcAgent's dst; NEVER stamped into the plist).
	p, err := resolvePaths(envFn(map[string]string{
		"HOME":         "/Users/seth",
		"OC_TOKEN":     "tok-abc",
		"OC_AGENT_BIN": "/run/officraft/ocagent-go/ocagent",
	}), "/Users/seth/.officraft/warden/ocwarden", 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.ocAgentSrc != "/run/officraft/ocagent-go/ocagent" {
		t.Errorf("ocAgentSrc = %q, want the source path", p.ocAgentSrc)
	}
	if p.ocAgentBin != "/Users/seth/.officraft/warden/ocagent" {
		t.Errorf("ocAgentBin = %q, want home sibling of ocwarden", p.ocAgentBin)
	}
}

func TestResolvePaths_OcAgentBinOptionalAndValidated(t *testing.T) {
	// Absent OC_AGENT_BIN ⇒ empty source (plist omits the env; warden falls back).
	p, err := resolvePaths(envFn(map[string]string{"HOME": "/h", "OC_TOKEN": "t"}), "/r/bin/ocwarden", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.ocAgentSrc != "" {
		t.Errorf("ocAgentSrc = %q, want empty when OC_AGENT_BIN unset", p.ocAgentSrc)
	}
	// A relative or whitespace-laden OC_AGENT_BIN is rejected (copy-source hygiene).
	for _, bad := range []string{"relative/ocagent", "./ocagent", "/has space/ocagent", "/tab\tocagent"} {
		if _, err := resolvePaths(envFn(map[string]string{"HOME": "/h", "OC_TOKEN": "t", "OC_AGENT_BIN": bad}), "/r/bin/ocwarden", 1); err == nil {
			t.Errorf("expected error for OC_AGENT_BIN=%q", bad)
		}
	}
}

func TestRenderPlist_NeverEmitsOcAgentBin(t *testing.T) {
	// The warden discovers ocagent as its own runtime sibling (resolveOcAgentBin), so
	// the plist must carry NO ocagent path — even when an install did copy one in.
	p := wardenPaths{
		root: "/repo", home: "/Users/seth", ocBase: "http://127.0.0.1:8770",
		tokfile: "/Users/seth/.officraft/exec-warden.tok",
		logDir:  "/repo/var/log", binPath: "/repo/bin/ocwarden",
		ocAgentSrc: "/run/officraft/ocagent-go/ocagent",
		ocAgentBin: "/Users/seth/.officraft/bin/ocagent",
	}
	out := renderPlist(p)
	if err := xmlWellFormed(out); err != nil {
		t.Fatalf("plist not well-formed: %v", err)
	}
	if strings.Contains(out, "OC_AGENT_BIN") || strings.Contains(out, "ocagent") {
		t.Errorf("plist must never carry the ocagent path (runtime sibling discovery), got:\n%s", out)
	}
}

// okProbe is a verify-before-swap stub that always passes (well-formed download).
func okProbe(string) error { return nil }

// TestInstallOcAgent_LocalOverrideAtomicHome0755 covers the OC_AGENT_BIN override branch:
// a local source file is copied to the home sibling 0755 via temp + atomic rename.
func TestInstallOcAgent_LocalOverrideAtomicHome0755(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.ocAgentSrc = "/run/officraft/ocagent-go/ocagent"
	p.ocAgentBin = "/h/.officraft/warden/ocagent"
	f.existing[p.ocAgentSrc] = []byte("OCAGENT-BYTES")
	i := &installer{out: io.Discard, sys: f.ops(), agentProbe: okProbe}
	if err := i.installOcAgent(p); err != nil {
		t.Fatalf("installOcAgent: %v", err)
	}
	if string(f.writes[p.ocAgentBin]) != "OCAGENT-BYTES" {
		t.Errorf("home ocagent content = %q, want source bytes", f.writes[p.ocAgentBin])
	}
	if f.modes[p.ocAgentBin] != 0o755 {
		t.Errorf("home ocagent mode = %o, want 755", f.modes[p.ocAgentBin])
	}
	if len(f.renames) != 1 || f.renames[0][1] != p.ocAgentBin {
		t.Fatalf("expected one atomic rename to %q, got %v", p.ocAgentBin, f.renames)
	}
	if !strings.HasPrefix(f.renames[0][0], "/h/.officraft/warden/.ocagent.") {
		t.Errorf("rename src = %q, want a temp under the warden dir", f.renames[0][0])
	}
}

// TestInstallOcAgent_DownloadDefaultVerifiesAndWrites covers the DEFAULT (production)
// branch: OC_AGENT_BIN unset ⇒ download from GET /api/agent/binary, verify-before-swap,
// atomic write to the home sibling. This is the fix for the deaf-agent bug: a machine
// with NO repo/local path still gets a real ocagent.
func TestInstallOcAgent_DownloadDefaultVerifiesAndWrites(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths() // ocAgentSrc == "" ⇒ download branch
	p.ocAgentBin = "/h/.officraft/warden/ocagent"
	var gotPath string
	get := func(path string) (int, []byte, error) {
		gotPath = path
		return 200, []byte("DOWNLOADED-OCAGENT"), nil
	}
	var probed string
	probe := func(bin string) error { probed = bin; return nil }
	i := &installer{out: io.Discard, sys: f.ops(), agentGet: get, agentProbe: probe}
	if err := i.installOcAgent(p); err != nil {
		t.Fatalf("installOcAgent (download): %v", err)
	}
	if gotPath != agentBinaryPath {
		t.Errorf("downloaded from %q, want %q", gotPath, agentBinaryPath)
	}
	if string(f.writes[p.ocAgentBin]) != "DOWNLOADED-OCAGENT" {
		t.Errorf("home ocagent content = %q, want downloaded bytes", f.writes[p.ocAgentBin])
	}
	if f.modes[p.ocAgentBin] != 0o755 {
		t.Errorf("home ocagent mode = %o, want 755", f.modes[p.ocAgentBin])
	}
	if probed == "" || !strings.HasPrefix(probed, "/h/.officraft/warden/.ocagent.") {
		t.Errorf("verify-before-swap must probe the downloaded temp, probed=%q", probed)
	}
	if len(f.renames) != 1 || f.renames[0][1] != p.ocAgentBin {
		t.Fatalf("expected one atomic rename to %q, got %v", p.ocAgentBin, f.renames)
	}
}

// TestInstallOcAgent_DownloadVerifyFailInstallsNothing is the anti-suicide guard: a
// download that fails to exec (truncated / wrong-arch) must NOT be renamed into place —
// the temp is removed and NO ocagent is left at the sibling path (else every spawn would
// exit 127).
func TestInstallOcAgent_DownloadVerifyFailInstallsNothing(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	get := func(string) (int, []byte, error) { return 200, []byte("CORRUPT"), nil }
	probe := func(string) error { return fmt.Errorf("bad arch") }
	i := &installer{out: io.Discard, sys: f.ops(), agentGet: get, agentProbe: probe}
	if err := i.installOcAgent(p); err == nil {
		t.Fatal("expected installOcAgent to fail when the download fails verify")
	}
	if _, ok := f.writes[p.ocAgentBin]; ok {
		t.Errorf("bad download must NOT be installed at %q", p.ocAgentBin)
	}
	if len(f.renames) != 0 {
		t.Errorf("bad download must not be renamed into place, got %v", f.renames)
	}
	if len(f.removed) == 0 {
		t.Errorf("the failed temp must be removed")
	}
}

// TestInstallOcAgent_DownloadNon200Aborts: a non-200 download aborts the install loudly
// rather than silently leaving ocagent missing.
func TestInstallOcAgent_DownloadNon200Aborts(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	get := func(string) (int, []byte, error) { return 503, nil, nil }
	i := &installer{out: io.Discard, sys: f.ops(), agentGet: get, agentProbe: okProbe}
	if err := i.installOcAgent(p); err == nil {
		t.Fatal("expected installOcAgent to fail on a non-200 download")
	}
	if len(f.writes) != 0 || len(f.renames) != 0 {
		t.Errorf("a failed download must install nothing; got writes=%v renames=%v", f.writes, f.renames)
	}
}

func TestInstallOcAgent_DownloadDryRunWritesNothing(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths() // download branch
	called := false
	get := func(string) (int, []byte, error) { called = true; return 200, []byte("X"), nil }
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops(), agentGet: get, agentProbe: okProbe}
	if err := i.installOcAgent(p); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("dry-run must not download")
	}
	if len(f.writes) != 0 || len(f.renames) != 0 || len(f.mkdirs) != 0 {
		t.Errorf("dry-run must mutate nothing; got writes=%v renames=%v mkdirs=%v", f.writes, f.renames, f.mkdirs)
	}
}

func TestInstallOcAgent_LocalOverrideDryRunCopiesNothing(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.ocAgentSrc = "/run/ocagent-go/ocagent"
	p.ocAgentBin = "/h/.officraft/warden/ocagent"
	f.existing[p.ocAgentSrc] = []byte("X")
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.installOcAgent(p); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 0 || len(f.renames) != 0 || len(f.mkdirs) != 0 {
		t.Errorf("dry-run must not copy; got writes=%v renames=%v mkdirs=%v", f.writes, f.renames, f.mkdirs)
	}
}

// ---------------------------------------------------------------------------
// guard — one warden per machine (refuse / allow / --force matrix)
// ---------------------------------------------------------------------------

// fakeJWT builds an UNSIGNED 3-segment JWT whose middle segment carries the given
// `sub` — only that segment matters to jwtSub (which does not verify the signature).
func fakeJWT(sub string) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	pay := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q}`, sub)))
	return hdr + "." + pay + ".sig"
}

func TestGuard_OneWardenPerMachine(t *testing.T) {
	cases := []struct {
		name        string
		existingSub string // "" = no pre-existing tokfile on this box
		newOCID     string // OC_ID override for the new install ("" = derive from token)
		newTokenSub string // sub of the new OC_TOKEN
		force       bool
		wantRefuse  bool
	}{
		{"fresh: no existing tokfile", "", "", "m-aaa", false, false},
		{"same id via OC_ID", "m-aaa", "m-aaa", "m-aaa", false, false},
		{"same id via token sub", "m-aaa", "", "m-aaa", false, false},
		// Half-install re-run regression: the tokfile's sub matches the re-minted
		// token's sub, but a stray/display OC_ID differs. The machine re-installing
		// ITSELF must never be mistaken for a foreign warden (token-sub match wins).
		{"same token sub with stray OC_ID proceeds", "m-aaa", "eva-m5-display", "m-aaa", false, false},
		{"same OC_ID with different token sub proceeds", "m-aaa", "m-aaa", "m-bbb", false, false},
		{"different id refuses", "m-bbb", "m-aaa", "m-aaa", false, true},
		{"different id via token sub refuses", "m-bbb", "", "m-aaa", false, true},
		{"different token sub and OC_ID refuses", "m-bbb", "m-ccc", "m-aaa", false, true},
		{"different id with --force proceeds", "m-bbb", "m-aaa", "m-aaa", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSys()
			p := fixedPaths()
			p.ocID = tc.newOCID
			p.ocToken = fakeJWT(tc.newTokenSub)
			if tc.existingSub != "" {
				f.existing[p.tokfile] = []byte(fakeJWT(tc.existingSub))
			}
			i := &installer{out: io.Discard, force: tc.force, sys: f.ops()}
			err := i.guard(p)
			if tc.wantRefuse && err == nil {
				t.Fatalf("expected guard to REFUSE (existing=%s new=%s/%s)", tc.existingSub, tc.newOCID, tc.newTokenSub)
			}
			if !tc.wantRefuse && err != nil {
				t.Fatalf("expected guard to PROCEED, got refuse: %v", err)
			}
		})
	}
}

// TestGuard_RefusesUnderDryRun proves the guard runs and can refuse even in DRYRUN
// (the tokfile probe is a non-mutating read), so a dry-run preview surfaces the block.
func TestGuard_RefusesUnderDryRun(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.ocID = "m-aaa"
	p.ocToken = fakeJWT("m-aaa")
	f.existing[p.tokfile] = []byte(fakeJWT("m-bbb"))
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.runInstall(p); err == nil {
		t.Fatal("dry-run install must still refuse when a different-machine warden exists")
	}
	// A refusal mutates nothing.
	if len(f.writes) != 0 || len(f.renames) != 0 || len(f.runs) != 0 {
		t.Errorf("refusal must not mutate; got writes=%v renames=%v runs=%v", f.writes, f.renames, f.ranNames())
	}
}

// TestRunInstall_ReRunOverHalfInstallSucceeds is the end-to-end idempotence
// regression for the field failure: a previous install half-completed (tokfile
// already on disk) AND the old launchd registration is still draining when the
// re-run reaches bootstrap. Without --force the guard must recognize the machine
// re-installing itself (same token sub, despite a stray OC_ID), and the launchctl
// step must wait out the async bootout before bootstrapping — the whole re-run
// walks through to a verified install.
func TestRunInstall_ReRunOverHalfInstallSucceeds(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.ocID = "eva-m5-display" // stray display id ≠ token sub — must not trip the guard
	p.ocToken = fakeJWT("m-aaa")
	f.existing[p.tokfile] = []byte(fakeJWT("m-aaa")) // half-install leftover
	f.existing[p.srcExe] = []byte("OCWARDEN-BYTES")
	p.ocAgentSrc = "/src/ocagent"
	f.existing[p.ocAgentSrc] = []byte("OCAGENT-BYTES")

	// Stateful launchctl: before bootstrap, `print` reports the OLD registration
	// still draining for two polls (async bootout), then gone; after bootstrap,
	// `print` reports the NEW job's stable pid so verify() passes.
	bootstrapped := false
	lingering := 2
	f.runFn = func(name string, args ...string) (string, error) {
		if len(args) == 0 {
			return "", nil
		}
		switch args[0] {
		case "bootstrap":
			bootstrapped = true
		case "print":
			if bootstrapped {
				return "com.officraft.ocwarden = {\n\tpid = 4242\n}", nil
			}
			if lingering > 0 {
				lingering--
				return "com.officraft.ocwarden = {\n\tpid = 1111\n}", nil // old job draining
			}
			return "", fmt.Errorf("Could not find service")
		}
		return "", nil
	}
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.runInstall(p); err != nil {
		t.Fatalf("re-run over a half-install must walk through, got: %v", err)
	}
	if !bootstrapped {
		t.Fatal("install never reached launchctl bootstrap")
	}
	if lingering != 0 {
		t.Errorf("bootstrap did not wait out the draining registration (%d lingering polls unconsumed)", lingering)
	}
	assertNoForbiddenProcessKill(t, f)
}

func TestRunInstall_DryRunNoSideEffects(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if err := i.runInstall(fixedPaths()); err != nil {
		t.Fatalf("dry-run install: %v", err)
	}
	if len(f.runs) != 0 || len(f.writes) != 0 || len(f.renames) != 0 || len(f.mkdirs) != 0 || len(f.removed) != 0 || f.slept != 0 {
		t.Errorf("dry-run install mutated something: runs=%v writes=%v renames=%v mkdirs=%v removed=%v slept=%d",
			f.ranNames(), f.writes, f.renames, f.mkdirs, f.removed, f.slept)
	}
}

// ---------------------------------------------------------------------------
// install-time claude resolution + the OC_CLAUDE_BIN plist stamp
// (the fix for "launchd warden 永遠 claude_bin_unresolved": resolve claude in
// the install env, stamp it into the plist so runtime priority ① hits).
// ---------------------------------------------------------------------------

func discardLogf(string, ...any) {}

func TestRenderPlist_DefaultHasNoClaudeStampAndMinimalPATH(t *testing.T) {
	// Zero claudeBin/plistPATH must render byte-compatible with the historical
	// output: minimal PATH, no OC_CLAUDE_BIN key anywhere.
	out := renderPlist(fixedPaths())
	if strings.Contains(out, "OC_CLAUDE_BIN") {
		t.Errorf("no-claude plist must not carry OC_CLAUDE_BIN:\n%s", out)
	}
	if !strings.Contains(out, "<key>PATH</key><string>"+wardenPlistPATH+"</string>") {
		t.Errorf("no-override plist must keep the minimal launchd PATH %q:\n%s", wardenPlistPATH, out)
	}
}

func TestRenderPlist_StampsClaudeBin(t *testing.T) {
	p := fixedPaths()
	p.namespace = "seth"
	p.label = wardenLabelFor("seth")
	p.claudeBin = "/Users/seth/.asdf/shims/claude"
	out := renderPlist(p)
	if err := xmlWellFormed(out); err != nil {
		t.Fatalf("plist not well-formed: %v", err)
	}
	if !strings.Contains(out, "<key>OC_CLAUDE_BIN</key><string>/Users/seth/.asdf/shims/claude</string>") {
		t.Errorf("plist must stamp OC_CLAUDE_BIN:\n%s", out)
	}
	// The stamp lives inside EnvironmentVariables, after the OC_NAMESPACE line.
	if !strings.Contains(out,
		"<key>OC_NAMESPACE</key><string>seth</string>\n        <key>OC_CLAUDE_BIN</key><string>/Users/seth/.asdf/shims/claude</string>\n    </dict>") {
		t.Errorf("OC_CLAUDE_BIN must be stamped after OC_NAMESPACE inside EnvironmentVariables:\n%s", out)
	}
}

func TestRenderPlist_PlistPATHOverrideEscaped(t *testing.T) {
	p := fixedPaths()
	p.claudeBin = "/x/claude"
	p.plistPATH = "/a&b:/usr/bin" // XML-special char must be escaped, not break the doc
	out := renderPlist(p)
	if err := xmlWellFormed(out); err != nil {
		t.Fatalf("plist not well-formed with escaped PATH: %v", err)
	}
	if !strings.Contains(out, "<key>PATH</key><string>/a&amp;b:/usr/bin</string>") {
		t.Errorf("plist must carry the escaped PATH override:\n%s", out)
	}
	if strings.Contains(out, "<key>PATH</key><string>"+wardenPlistPATH+"</string>") {
		t.Errorf("override must replace the minimal PATH:\n%s", out)
	}
}

func TestResolveClaudeForInstall_NotFound(t *testing.T) {
	bin, pathEnv := resolveClaudeForInstall(envFn(nil),
		func() string { return "" },
		func(string, string, string) error { t.Fatal("probe must not run when lookup misses"); return nil },
		discardLogf)
	if bin != "" || pathEnv != "" {
		t.Fatalf("unresolved claude must return empty stamp, got (%q, %q)", bin, pathEnv)
	}
}

func TestResolveClaudeForInstall_UnstampablePathRefused(t *testing.T) {
	for _, bad := range []string{"relative/claude", "/has space/claude", "/x<y/claude", "/x&y/claude"} {
		bin, _ := resolveClaudeForInstall(envFn(nil),
			func() string { return bad }, nil, discardLogf)
		if bin != "" {
			t.Errorf("unstampable claude path %q must be refused, got %q", bad, bin)
		}
	}
}

func TestResolveClaudeForInstall_MinimalPATHSuffices(t *testing.T) {
	var probes [][2]string
	bin, pathEnv := resolveClaudeForInstall(
		envFn(map[string]string{"HOME": "/h", "PATH": "/rich/user/path:/usr/bin"}),
		func() string { return "/opt/homebrew/bin/claude" },
		func(bin, pathE, home string) error { probes = append(probes, [2]string{bin, pathE}); return nil },
		discardLogf)
	if bin != "/opt/homebrew/bin/claude" || pathEnv != "" {
		t.Fatalf("minimal-PATH-clean claude must stamp bin only, got (%q, %q)", bin, pathEnv)
	}
	if len(probes) != 1 || probes[0][1] != wardenPlistPATH {
		t.Fatalf("must probe exactly once under the minimal launchd PATH, got %v", probes)
	}
}

func TestResolveClaudeForInstall_ShimNeedsInstallerPATH(t *testing.T) {
	// asdf/nvm shim shape: dies under the minimal PATH, runs under the
	// installer's PATH → stamp OC_CLAUDE_BIN AND carry the installer PATH.
	userPATH := "/h/.asdf/shims:/h/.asdf/bin:/usr/bin"
	probe := func(bin, pathE, home string) error {
		if pathE == wardenPlistPATH {
			return fmt.Errorf("shim: asdf not on PATH")
		}
		return nil
	}
	bin, pathEnv := resolveClaudeForInstall(
		envFn(map[string]string{"HOME": "/h", "PATH": userPATH}),
		func() string { return "/h/.asdf/shims/claude" }, probe, discardLogf)
	if bin != "/h/.asdf/shims/claude" || pathEnv != userPATH {
		t.Fatalf("shim claude must stamp bin + installer PATH, got (%q, %q)", bin, pathEnv)
	}
}

func TestResolveClaudeForInstall_BothProbesFailStampsBestEffort(t *testing.T) {
	bin, pathEnv := resolveClaudeForInstall(
		envFn(map[string]string{"HOME": "/h", "PATH": "/some/path"}),
		func() string { return "/x/claude" },
		func(string, string, string) error { return fmt.Errorf("nope") },
		discardLogf)
	if bin != "/x/claude" || pathEnv != "" {
		t.Fatalf("double-probe-fail must still stamp the bin best-effort with default PATH, got (%q, %q)", bin, pathEnv)
	}
}

// stableLaunchctl returns a runFn whose `launchctl print` reports a stable pid
// once bootstrapped (so verify() passes) and not-found before.
func stableLaunchctl() func(string, ...string) (string, error) {
	bootstrapped := false
	return func(name string, args ...string) (string, error) {
		if len(args) == 0 {
			return "", nil
		}
		switch args[0] {
		case "bootstrap":
			bootstrapped = true
		case "print":
			if bootstrapped {
				return "com.officraft.ocwarden = {\n\tpid = 4242\n}", nil
			}
			return "", fmt.Errorf("Could not find service")
		}
		return "", nil
	}
}

func TestRunInstall_StampsClaudeIntoWrittenPlist(t *testing.T) {
	f := newFakeSys()
	f.runFn = stableLaunchctl()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("OCWARDEN-BYTES")
	p.ocAgentSrc = "/src/ocagent"
	f.existing[p.ocAgentSrc] = []byte("OCAGENT-BYTES")
	i := &installer{
		out: io.Discard, sys: f.ops(),
		resolveClaude: func() (string, string) { return "/h/.local/bin/claude", "" },
	}
	if err := i.runInstall(p); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	plist := string(f.writes[p.plistPath])
	if !strings.Contains(plist, "<key>OC_CLAUDE_BIN</key><string>/h/.local/bin/claude</string>") {
		t.Errorf("written plist must carry the OC_CLAUDE_BIN stamp:\n%s", plist)
	}
}

// T-ba62: an unresolvable claude is FAIL-CLOSED, not a warning. Asserts the
// REASON (not merely the non-nil error): "wrongly failed" and "correctly
// refused" share an exit code, so the refusal must be identified by its text.
// A REAL (non-dry-run) install is used so the "no residue" half is meaningful.
func TestRunInstall_MissingClaudeFailsClosedWithReason(t *testing.T) {
	f := newFakeSys()
	f.runFn = stableLaunchctl()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("OCWARDEN-BYTES")
	p.ocAgentSrc = "/src/ocagent"
	f.existing[p.ocAgentSrc] = []byte("OCAGENT-BYTES")
	var sb strings.Builder
	i := &installer{
		out: &sb, sys: f.ops(),
		resolveClaude: func() (string, string) { return "", "" },
	}
	err := i.runInstall(p)
	if err == nil {
		t.Fatalf("missing claude must FAIL the install; got nil error\n%s", sb.String())
	}
	if !strings.Contains(err.Error(), "claude_bin_unresolved") {
		t.Errorf("refusal must name its reason (claude_bin_unresolved); got %q", err)
	}
	out := sb.String()
	for _, want := range []string{"claude_bin_unresolved", "OC_CLAUDE_BIN", "FATAL", "NOTHING was installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("fail-closed install output must contain %q:\n%s", want, out)
		}
	}
	// NO RESIDUE: the refusal happens before every mutation — no plist, no
	// tokfile, no binary copy, and no launchctl call.
	if len(f.writes) != 0 {
		t.Errorf("fail-closed install must mutate nothing; wrote: %v", f.writes)
	}
	if len(f.runs) != 0 {
		t.Errorf("fail-closed install must run no launchctl; ran: %v", f.runs)
	}
}
