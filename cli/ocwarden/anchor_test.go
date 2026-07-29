package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// installAnchor — the install-once rule
// ---------------------------------------------------------------------------

// TestInstallAnchor_WritesWhenAbsent: the first install materialises the anchor
// 0755 from the same source copyBinary uses, via temp+rename.
func TestInstallAnchor_WritesWhenAbsent(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("warden-bytes")
	var log bytes.Buffer
	i := &installer{out: &log, sys: f.ops()}

	if err := i.installAnchor(p); err != nil {
		t.Fatalf("installAnchor: %v", err)
	}
	if got := string(f.writes[p.anchorPath]); got != "warden-bytes" {
		t.Errorf("anchor content = %q, want the running binary's bytes", got)
	}
	if f.modes[p.anchorPath] != 0o755 {
		t.Errorf("anchor mode = %o, want 755", f.modes[p.anchorPath])
	}
	// The operator cannot discover the one-time grant on their own: an ungranted
	// anchor does not fail loudly, it hangs. So the install has to say it.
	if !strings.Contains(log.String(), "Full Disk Access") {
		t.Errorf("a freshly written anchor must tell the operator to grant it:\n%s", log.String())
	}
}

// TestInstallAnchor_NeverOverwritesExisting is THE load-bearing test of this
// change. The anchor's whole value is that its bytes — and therefore the cdhash
// macOS bound this machine's privacy grant to — never move. An install that
// refreshes it, even with identical bytes from the same build, revokes the grant,
// and the next TCC request from any agent blocks forever on a consent prompt no
// GUI-less LaunchAgent can display. If this test is ever "fixed" by letting the
// write through, the entire change is undone and nothing else in the suite notices.
func TestInstallAnchor_NeverOverwritesExisting(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("new-warden-bytes")
	f.modes[p.anchorPath] = 0o755 // an anchor from an earlier install, already granted
	var log bytes.Buffer
	i := &installer{out: &log, sys: f.ops()}

	if err := i.installAnchor(p); err != nil {
		t.Fatalf("installAnchor over an existing anchor: %v", err)
	}
	if _, wrote := f.writes[p.anchorPath]; wrote {
		t.Fatal("installAnchor REWROTE an existing anchor — that voids the machine's TCC grant and hangs every agent on an unanswerable consent prompt")
	}
	for path := range f.writes {
		if strings.Contains(path, ".ocanchor.") {
			t.Fatalf("installAnchor staged a temp anchor at %s — it must not even prepare a replacement", path)
		}
	}
	if len(f.renames) != 0 {
		t.Fatalf("installAnchor renamed something: %v", f.renames)
	}
	if !strings.Contains(log.String(), "leaving its bytes untouched") {
		t.Errorf("skipping must be visible in the install log:\n%s", log.String())
	}
}

// TestInstallAnchor_RefusesAnEmptyPath: a wardenPaths with no anchor is a mis-wire.
// Failing the install is right — the alternative renders a plist that boots, looks
// healthy, and silently pins TCC to the self-updating binary again.
func TestInstallAnchor_RefusesAnEmptyPath(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, sys: f.ops()}
	p := fixedPaths()
	p.anchorPath = ""
	if err := i.installAnchor(p); err == nil {
		t.Fatal("an empty anchorPath must fail the install, not fall back to the live binary")
	}
	if len(f.writes) != 0 {
		t.Errorf("refusal must mutate nothing, wrote: %v", f.writes)
	}
}

// TestInstallAnchor_StatErrorIsFatal: an unreadable anchor path must abort rather
// than be read as absent, because "absent" means "write one" and writing over a
// live anchor is the single thing this must never do.
func TestInstallAnchor_StatErrorIsFatal(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	// Seed the copy source: without it the write path fails for its OWN reason and
	// this test would pass even with the presence check deleted — which it did,
	// until a mutation run caught it.
	f.existing[p.srcExe] = []byte("warden-bytes")
	f.statErr[p.anchorPath] = errors.New("permission denied")
	i := &installer{out: io.Discard, sys: f.ops()}

	err := i.installAnchor(p)
	if err == nil {
		t.Fatal("an ambiguous stat must fail the install, not be treated as absent")
	}
	if !strings.Contains(err.Error(), "stat anchor") {
		t.Errorf("the failure must name the stat, got: %v", err)
	}
	if len(f.writes) != 0 {
		t.Errorf("must mutate nothing on stat failure, wrote: %v", f.writes)
	}
}

// TestInstallAnchor_DryRunMutatesNothing keeps the anchor inside the existing
// WARDEN_INSTALL_DRYRUN contract.
func TestInstallAnchor_DryRunMutatesNothing(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	f.existing[p.srcExe] = []byte("warden-bytes")
	var log bytes.Buffer
	i := &installer{out: &log, sys: f.ops(), dryRun: true}
	if err := i.installAnchor(p); err != nil {
		t.Fatalf("dry-run installAnchor: %v", err)
	}
	if len(f.writes) != 0 || len(f.renames) != 0 {
		t.Errorf("dry run mutated: writes=%v renames=%v", f.writes, f.renames)
	}
	if !strings.Contains(log.String(), "DRYRUN would") {
		t.Errorf("dry run must report intent:\n%s", log.String())
	}
}

// TestRunInstall_ReinstallKeepsTheAnchorButRefreshesTheWarden pins the pairing
// end-to-end, which is the field scenario: every release reinstalls, and each
// reinstall used to move the very identity TCC had granted. The live warden must
// still be refreshed; only the anchor is frozen.
func TestRunInstall_ReinstallKeepsTheAnchorButRefreshesTheWarden(t *testing.T) {
	f := newFakeSys()
	p := fixedPaths()
	p.ocToken = fakeJWT("m-aaa")
	f.existing[p.srcExe] = []byte("v2-warden")
	p.ocAgentSrc = "/src/ocagent"
	f.existing[p.ocAgentSrc] = []byte("OCAGENT-BYTES")
	f.modes[p.anchorPath] = 0o755 // installed by v1; operator already granted it

	bootstrapped := false
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
			return "", fmt.Errorf("Could not find service")
		}
		return "", nil
	}
	i := &installer{out: io.Discard, sys: f.ops()}

	if err := i.runInstall(p); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, wrote := f.writes[p.anchorPath]; wrote {
		t.Error("a reinstall rewrote the anchor — the operator's grant is now void and every agent will hang")
	}
	if got := string(f.writes[p.binPath]); got != "v2-warden" {
		t.Errorf("the live warden must still be refreshed by a reinstall, got %q", got)
	}
	plist := string(f.writes[p.plistPath])
	if !strings.Contains(plist, "<string>"+p.anchorPath+"</string><string>anchor</string>") {
		t.Errorf("plist must start the anchor, not the live warden:\n%s", plist)
	}
}

// TestRenderPlist_NeverStartsTheSelfUpdatingBinaryDirectly is the property behind
// the golden: whatever else the render does, ProgramArguments[0] must not be the
// path self-update rewrites.
func TestRenderPlist_NeverStartsTheSelfUpdatingBinaryDirectly(t *testing.T) {
	out := renderPlist(fixedPaths())
	if strings.Contains(out, "<array><string>/h/.officraft/warden/ocwarden</string>") {
		t.Errorf("launchd must not start the self-updating binary directly:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// the `anchor` verb
// ---------------------------------------------------------------------------

type fakeChild struct {
	signals chan os.Signal
	release chan struct{}
	state   *os.ProcessState
	waitErr error
}

func newFakeChild() *fakeChild {
	return &fakeChild{signals: make(chan os.Signal, 4), release: make(chan struct{})}
}

func (c *fakeChild) Signal(s os.Signal) error { c.signals <- s; return nil }

func (c *fakeChild) Wait() (*os.ProcessState, error) {
	<-c.release
	return c.state, c.waitErr
}

// withFakeChild rebinds the spawn seam for one test and reports the argv it saw.
func withFakeChild(t *testing.T, c anchorChild, err error) *[]string {
	t.Helper()
	var gotArgv []string
	prev := startAnchorChild
	startAnchorChild = func(argv []string) (anchorChild, error) {
		gotArgv = argv
		return c, err
	}
	t.Cleanup(func() { startAnchorChild = prev })
	return &gotArgv
}

// TestAnchorCmd_ForksTheProgramItWasGiven: the verb must spawn a SEPARATE process.
// Exec-ing in place would keep the pid but adopt the live warden's code identity —
// precisely the identity the anchor exists in order not to have.
func TestAnchorCmd_ForksTheProgramItWasGiven(t *testing.T) {
	child := newFakeChild()
	close(child.release) // returns (nil, nil) → exit 0 via the nil-state path below
	child.waitErr = errors.New("stop here")
	argv := withFakeChild(t, child, nil)

	anchorCmd([]string{"/h/.officraft/warden/ocwarden", "run"}, io.Discard)

	if len(*argv) != 2 || (*argv)[0] != "/h/.officraft/warden/ocwarden" || (*argv)[1] != "run" {
		t.Errorf("spawned argv = %v, want the warden and its verb verbatim", *argv)
	}
}

// TestAnchorCmd_RequiresAProgram: no child means nothing to anchor.
func TestAnchorCmd_RequiresAProgram(t *testing.T) {
	var out bytes.Buffer
	if rc := anchorCmd(nil, &out); rc != 2 {
		t.Errorf("exit = %d, want 2", rc)
	}
	if !strings.Contains(out.String(), "usage: ocwarden anchor") {
		t.Errorf("missing usage line: %s", out.String())
	}
}

// TestAnchorCmd_SpawnFailureIsLoud: launchd would otherwise see a bare non-zero and
// the operator would have no idea which path failed.
func TestAnchorCmd_SpawnFailureIsLoud(t *testing.T) {
	withFakeChild(t, nil, errors.New("no such file"))
	var out bytes.Buffer
	if rc := anchorCmd([]string{"/nope"}, &out); rc != 1 {
		t.Errorf("exit = %d, want 1", rc)
	}
	if !strings.Contains(out.String(), "/nope") || !strings.Contains(out.String(), "no such file") {
		t.Errorf("failure must name the path and the cause: %s", out.String())
	}
}

// TestAnchorCmd_ForwardsStopSignals: launchd stops the job by signalling the anchor,
// but the process that must react is the warden underneath. Without the relay,
// `launchctl bootout` reaps the anchor and orphans a live warden onto pid 1.
func TestAnchorCmd_ForwardsStopSignals(t *testing.T) {
	child := newFakeChild()
	child.waitErr = errors.New("done")
	withFakeChild(t, child, nil)

	registered := make(chan chan<- os.Signal, 1)
	prevNotify, prevStop := anchorNotify, anchorStop
	anchorNotify = func(c chan<- os.Signal, sig ...os.Signal) { registered <- c }
	anchorStop = func(chan<- os.Signal) {}
	t.Cleanup(func() { anchorNotify, anchorStop = prevNotify, prevStop })

	done := make(chan int, 1)
	go func() { done <- anchorCmd([]string{"/h/warden"}, io.Discard) }()

	sigs := <-registered
	sigs <- syscall.SIGTERM
	got := <-child.signals // blocks until the relay goroutine forwards it
	close(child.release)
	<-done

	if got != syscall.SIGTERM {
		t.Errorf("child got %v, want SIGTERM relayed", got)
	}
}

// TestAnchorExitStatus_ReportsTheChildsFate: launchd must see exactly what it would
// have seen had it started the warden directly — including the clean exit 0 that
// selfupdate.go's exec-in-place path depends on.
func TestAnchorExitStatus_ReportsTheChildsFate(t *testing.T) {
	// A signalled child reports 128+signum (shell convention) so `launchctl print`
	// shows a cause rather than a bare failure.
	if got := anchorExitStatusFromWait(syscall.WaitStatus(int(syscall.SIGKILL))); got != 128+9 {
		t.Errorf("signalled child status = %d, want %d", got, 128+9)
	}
	if got := anchorExitStatusFromWait(syscall.WaitStatus(0)); got != 0 {
		t.Errorf("clean exit status = %d, want 0", got)
	}
	if got := anchorExitStatusFromWait(syscall.WaitStatus(3 << 8)); got != 3 {
		t.Errorf("exit-3 child status = %d, want 3", got)
	}
}
