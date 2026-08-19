// hostseam_test.go — the STRUCTURAL reason a `go test ./cli/ocwarden/...` run can
// never install, teardown, bootout or bootstrap anything on the machine it runs on.
//
// THE DEFECT THIS CLOSES (T-5047 gap 1)
// -------------------------------------
// `ocwarden install` / `ocwarden teardown` already funnelled every side effect
// through the injectable sysOps seam — but they WIRED that seam to the real OS
// themselves (realSysOps() inline, inside installCmd/teardownCmd). The seam was
// therefore only as protective as each individual test's discipline: a test that
// called `realMain([]string{"install"})` — and transport_test.go / namespace_test.go
// already call realMain for `run --once`, so nothing about that is exotic — would
// bootout and bootstrap the LIVE canonical com.officraft.ocwarden job of whoever
// ran the suite. The install tests were safe only because they happened to build
// `installer` by hand with a fake. That is safety by coincidence, and the identical
// shape on the teardown path unloaded this fleet's live warden three times.
//
// THE FIX IS A SEAM, NOT A CHECK
// ------------------------------
// install.go now has ONE production construction point for every real-host effect
// (realHostSeam) reachable only through the package-level var newHostSeam. The
// TestMain below rebinds that var BEFORE any test in this package runs. So:
//
//   - every test, including ones written years from now, gets the fake;
//   - a test does not have to remember to opt in;
//   - and — the claim this actually buys — deleting or breaking a guard IN THE CODE
//     UNDER TEST (installer.guard, the namespace validation, the label derivation)
//     cannot escalate into touching the host, because an entry point that takes its
//     effects from newHostSeam gets the fake no matter what its own logic does.
//
// WHAT THE SEAM DOES **NOT** BUY (learned the hard way, twice)
// -----------------------------------------------------------
// The seam protects entry points that GO THROUGH IT. It cannot protect against an
// entry point that assembles the wiring itself — `sysOps{run: execRunner{…}.Run,
// rename: os.Rename, …}` inline in teardownCmd names neither realSysOps nor
// realHostSeam, so the identifier-based scans stay green and the seam is simply not
// consulted. Independent review built exactly that mutant and the test binary
// issued a real `launchctl bootout gui/<uid>/com.officraft.ocwarden`.
//
// Two things close that, and BOTH are needed:
//   - scanHostSeamSource check (4) pins the STRUCTURE, not just the names: the
//     `sysOps{` and `execRunner{` composite literals may exist in exactly one place
//     each. A hand-assembled seam is now a TestMain refusal before m.Run().
//   - main.go's execRunner.Run opens with refuseInTestBinary. This is the only
//     guard a hand-assembled struct cannot route around, because however the struct
//     was built the subprocess still has to be started there. It fires BEFORE
//     exec.Command, i.e. before the host is touched — not afterwards.
//
// TestHostSeam_StructureIsReported gives the static half a name in `go test -v`
// output. It is a reporting wrapper only; TestMain is the gate.
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errHostSeamBlocked is what the test-binary seam returns for the effects that
// have no meaningful in-memory stand-in (exec a candidate binary, HTTP to a
// server). Recorded, never performed.
var errHostSeamBlocked = errors.New("host seam is blocked in the test binary")

// hostSeamFakes records, in order, every seam handed out during the test binary's
// life, so a test can assert on WHAT an entry point tried to do to the host.
var hostSeamFakes []*fakeSys

// hostSeamSeed lets a test pre-populate the in-memory filesystem of the seam an
// entry point is ABOUT to construct (an entry point resolves its own paths, so the
// test cannot reach the fake afterwards). Purely a fixture hook: it can only add
// readable bytes to the fake, never restore a route to the real OS.
var hostSeamSeed func(*fakeSys)

// fakeHostSeam is the test binary's binding of newHostSeam. The mutating half is
// the same in-memory fakeSys the install tests already use (so an entry point runs
// to completion and its intent is fully observable); the exec/network halves refuse.
func fakeHostSeam() hostSeam {
	f := newFakeSys()
	if hostSeamSeed != nil {
		hostSeamSeed(f)
	}
	hostSeamFakes = append(hostSeamFakes, f)
	return hostSeam{
		sys:         f.ops(),
		claudeProbe: func(_, _, _ string) error { return errHostSeamBlocked },
		agentGet: func(_, _ string) getter {
			return func(string) (int, []byte, error) { return 0, nil, errHostSeamBlocked }
		},
		agentProbe: func(string) error { return errHostSeamBlocked },
	}
}

// TestMain is the structural gate: it runs once, before every test in the package,
// and there is no way for an individual test to be reached without passing through
// it. Do NOT add an opt-out.
//
// WHY THE SOURCE SCANS RUN HERE AND NOT AS ORDINARY TESTS
// ------------------------------------------------------
// They used to be plain Test* functions whose own comments claimed they "fail before
// anything executes". That claim was FALSE, and it was falsified by experiment — then
// demonstrated the hard way. Go runs tests in declaration order, file by file in
// sorted filename order: hostseam_test.go sorts before install_test.go, and within
// this very file TestInstallCmd_CannotReachTheRealHost is declared ABOVE both static
// guards. So under the one-line mutant the guards exist to catch (installCmd's
// `newHostSeam()` → `realHostSeam()`, or teardownCmd's `newHostSeam().sys` →
// `realSysOps()`), the run reaches the CannotReachTheRealHost tests — which drive the
// FULL install/teardown at the CANONICAL label on purpose — and constructs the REAL
// seam there. The guards had not run yet and never got the chance.
//
// 🔴 That is not a theoretical ordering argument. During T-5047 verification a mutant
// run on a tree without these gates did exactly this and issued a real
// `launchctl bootout gui/<uid>/com.officraft.ocwarden` against the developer
// machine's LIVE warden: the job was unloaded and had to be re-bootstrapped by hand.
// A detection that speaks after the bootout is not a defence.
//
// TestMain is the one place in a Go package guaranteed to run before any test
// function. Failing here — before m.Run() — is what "before anything executes"
// actually means. Do not move these back out into Test* functions.
func TestMain(m *testing.M) {
	// Every structural gate runs here, together, before m.Run(). scanProcessStarters
	// is the one that does not depend on anybody having remembered to extend it:
	// it enumerates the process-starting call sites that actually exist and refuses
	// the ones nobody has sanctioned, so a NEW seam in a NEW file is caught by
	// construction rather than by the next reviewer's memory.
	violations := scanHostSeamSource()
	violations = append(violations, scanProcessStarters()...)
	violations = append(violations, scanTestsForRealHostCalls()...)
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "\nFATAL: cli/ocwarden host-seam structure is broken — REFUSING TO RUN ANY TEST.\n"+
			"These checks run in TestMain, before m.Run(), because a test binary in which an entry\n"+
			"point can construct the real seam would act on this machine's LIVE launchd domain the\n"+
			"first time any test drives install or teardown — which the very next tests do, at the\n"+
			"canonical label, on purpose.\n\n")
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
	newHostSeam = fakeHostSeam
	// The exec runner is rebound for the same reason and at the same moment: the
	// real one now REFUSES inside a test binary (main.go execRunner.Run →
	// refuseInTestBinary), so the telemetry/probe paths that legitimately shell out
	// in production must take their runner from this seam. A test that wants to
	// observe argv still injects its own recording runner locally, exactly as before.
	newCmdRunner = func(time.Duration) CmdRunner { return blockedRunner{} }
	// T-ff5d: the anchor-cutover seam is rebound here for the same reason and at
	// the same moment. Its production binding reads and WRITES real paths — the
	// live plist, its .prev backup, the lock and failure sentinels — and drives
	// launchctl bootout/bootstrap on the canonical label. Rebinding it in TestMain
	// means every test in this package gets the fake without opting in, including
	// tests written later and tests that neuter the guard under test.
	newCutoverOps = blockedCutoverOps
	os.Exit(m.Run())
}

// blockedRunner is the test binary's binding of newCmdRunner: it starts no process
// and returns errHostSeamBlocked for every argv. Callers on the telemetry path all
// treat a runner error as "this field is unavailable", so a `run --once` still
// completes end to end without a single subprocess.
type blockedRunner struct{}

func (blockedRunner) Run(name string, args ...string) (string, error) {
	return "", fmt.Errorf("%w: refusing to exec %s", errHostSeamBlocked, name)
}

// scanHostSeamSource is the whole static half of the guarantee, as a pure function
// over this package's non-test sources so TestMain can run it before m.Run(). It
// returns one human-readable violation per problem found, empty when the structure
// holds. Both halves are here because both are preconditions for the test binary
// being safe at all, and neither is safe to discover late:
//
//	(1) SINGLE CONSTRUCTION POINT — realSysOps() may appear exactly once in the
//	    non-test sources (realHostSeam's body). TestMain can only protect what it
//	    can rebind; an inline realSysOps() in an entry point (the pre-T-5047 shape)
//	    is unrebindable.
//	(2) NO DIRECT realHostSeam() CALL — realHostSeam may be MENTIONED as its own
//	    declaration and as newHostSeam's initialiser, and CALLED nowhere. This is
//	    the hole the first version of this file left open: with the mutant in place
//	    realSysOps() still appears exactly once and `var newHostSeam` is still
//	    there, so (1) alone stays green while the entry point is wired straight to
//	    the real OS again.
//	(3) THE RUNTIME BACKSTOP IS STILL WIRED — refuseInTestBinary must still open the
//	    body of the two real-seam constructors AND of execRunner.Run, because (1),
//	    (2) and (4) are source scans and a source scan is exactly what a bad edit
//	    can defeat.
//	(4) NO HAND-ASSEMBLED HOST WIRING — `sysOps{`, `execRunner{` and `cutoverOps{`
//	    composite literals may appear in exactly one place each (realSysOps's
//	    return, newCmdRunner's initialiser, and realCutoverOps's return). This is
//	    the check whose ABSENCE independent review exploited: (1) and (2) pin two
//	    IDENTIFIERS, so a mutant that writes
//	    `sysOps{run: execRunner{…}.Run, rename: os.Rename, …}` inline in teardownCmd
//	    mentions NEITHER name, keeps every scan above green, and reaches the live
//	    launchd domain. (1)/(2) guard the front door of a house with no walls; this
//	    is the wall, and refuseInTestBinary on execRunner.Run (3) is the runtime
//	    proof that even a wall with a hole in it cannot let a test binary exec.
//
//	    ⚠️ cutoverOps joined this list in T-ff5d, and it was added because the SAME
//	    mutant worked verbatim on it: cutover.go grew a second seam whose real
//	    binding drives `launchctl bootout` on the canonical label, overwrites the
//	    live plist, and spawns a DETACHED machine conversion — and none of the
//	    scans above had ever heard of it, so
//	    `cutoverOps{runExit: realRunExit, spawnDetached: spawnDetachedProcess}` in
//	    a non-test function passed every guard and really started a process from
//	    inside the test binary. The lesson is structural, not about this one seam:
//	    a NEW seam is invisible to a scan that enumerates the old ones by name.
//	    Anything that wires the real host must be added here and to (3).
func scanHostSeamSource() []string { return scanHostSeamSourceIn(".") }

// scanHostSeamSourceIn is scanHostSeamSource over an ARBITRARY directory of Go
// sources. The parameter exists so the guard can be pointed at a fixture holding
// a known-bad file and REQUIRED to reject it — see
// TestHostSeamGuard_RejectsTheKnownBadWiring. A guard that is only ever run
// against a tree it already passes cannot distinguish "the structure holds" from
// "the check does nothing", which is the failure mode this whole file exists to
// rule out.
func scanHostSeamSourceIn(dir string) []string {
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{fmt.Sprintf("cannot read package sources to verify the host seam: %v (fail closed)", err)}
	}
	total := 0
	// (4) counters for the two host-wiring composite literals, keyed by the file
	// they are allowed to live in.
	litTotals := map[string]int{"sysOps{": 0, "execRunner{": 0, "cutoverOps{": 0}
	litHome := map[string]string{
		"sysOps{": "install.go", "execRunner{": "main.go", "cutoverOps{": "cutover.go",
	}
	litWhere := map[string]string{
		"sysOps{":     "install.go's realSysOps (the ONE place the real OS is wired)",
		"execRunner{": "main.go's `var newCmdRunner` initialiser (the ONE place the real exec runner is built)",
		"cutoverOps{": "cutover.go's realCutoverOps (the ONE place the anchor-cutover seam is wired to launchctl, the live plist and a detached converter)",
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			out = append(out, fmt.Sprintf("cannot read %s to verify the host seam: %v (fail closed)", name, err))
			continue
		}
		src := string(raw)

		// (1) single construction point.
		if n := countRealSysOpsCalls(src); n > 0 {
			if name != "install.go" {
				out = append(out, fmt.Sprintf("%s calls realSysOps() directly (%d time(s)) — the real host must be wired ONLY in install.go's realHostSeam, reachable through newHostSeam, or the test binary cannot rebind it", name, n))
			}
			total += n
		}

		// (2) realHostSeam is never called.
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// The two legitimate mentions, neither of which is a call site.
			if strings.HasPrefix(trimmed, "func realHostSeam()") ||
				strings.HasPrefix(trimmed, "var newHostSeam = realHostSeam") {
				continue
			}
			if strings.Contains(trimmed, "realHostSeam()") {
				out = append(out, fmt.Sprintf("%s:%d calls realHostSeam() directly:\n\t\t%s\n\t  Every entry point must go through newHostSeam() — calling realHostSeam bypasses the var TestMain rebinds, so the test binary is wired to the LIVE launchd domain again and no assertion can prevent the damage, only report it afterwards.",
					name, i+1, trimmed))
			}
		}

		// (4) no hand-assembled host wiring anywhere else.
		for lit := range litTotals {
			n := countCodeOccurrences(src, lit)
			if n > 0 && name != litHome[lit] {
				out = append(out, fmt.Sprintf("%s builds a %s composite literal (%d time(s)) — real host wiring may only be assembled in %s. Assembling it by hand is how a mutant reaches launchctl without ever naming realSysOps or realHostSeam, which is exactly how this fleet's live warden was booted out during T-5047 verification.",
					name, lit, n, litWhere[lit]))
			}
			litTotals[lit] += n
		}
	}
	if total != 1 {
		out = append(out, fmt.Sprintf("realSysOps() appears %d time(s) across the non-test sources, want exactly 1 (realHostSeam)", total))
	}
	for lit, n := range litTotals {
		if n != 1 {
			out = append(out, fmt.Sprintf("%s composite literals appear %d time(s) across the non-test sources, want exactly 1 (%s)", lit, n, litWhere[lit]))
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "install.go"))
	if err != nil {
		return append(out, fmt.Sprintf("cannot read install.go to verify the host seam: %v (fail closed)", err))
	}
	if !strings.Contains(string(raw), "var newHostSeam = realHostSeam") {
		out = append(out, "install.go must keep `var newHostSeam = realHostSeam` — a func (not a var) cannot be rebound by TestMain and the whole structural guarantee evaporates")
	}
	// (3) the runtime backstop must stay wired at BOTH real-seam entry points.
	for _, fn := range []string{"func realSysOps() sysOps {\n\trefuseInTestBinary(", "func realHostSeam() hostSeam {\n\trefuseInTestBinary("} {
		if !strings.Contains(string(raw), fn) {
			out = append(out, fmt.Sprintf("install.go lost the runtime backstop: expected the body of %q to open with refuseInTestBinary(...) — that call is what makes constructing the real seam under `go test` impossible rather than merely detectable",
				strings.SplitN(fn, " {", 2)[0]))
		}
	}
	// (3, cont.) …and at the PROCESS choke point, which is the only guard a
	// hand-assembled sysOps cannot route around: whatever built the struct, the
	// subprocess still has to be started in execRunner.Run.
	mainRaw, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		return append(out, fmt.Sprintf("cannot read main.go to verify the exec choke point: %v (fail closed)", err))
	}
	if !strings.Contains(string(mainRaw), "func (r execRunner) Run(name string, args ...string) (string, error) {\n\trefuseInTestBinary(") {
		out = append(out, "main.go lost the exec choke point: execRunner.Run's body must OPEN with refuseInTestBinary(...). The two identifier scans above can be defeated by an inline `sysOps{run: execRunner{…}.Run, …}` literal (independent review did exactly that, and the test binary issued a real launchctl bootout against this machine's live warden). The refusal on the exec syscall is what turns that from after-the-fact detection into prevention")
	}
	if !strings.Contains(string(mainRaw), "var newCmdRunner = func(timeout time.Duration) CmdRunner { return execRunner{timeout: timeout} }") {
		out = append(out, "main.go must keep `var newCmdRunner` as the single rebindable construction point for the real exec runner — production code that needs a real subprocess takes it from there, and TestMain rebinds it so the refusal above is never hit by legitimate test traffic")
	}
	return out
}

// countCodeOccurrences counts occurrences of lit on CODE lines only. The comments
// in this package discuss `sysOps{` and `execRunner{` at length on purpose (this
// very function's doc comment does), so a scan that counted comment text would be
// impossible to keep green while documenting itself — the pattern-(a) always-true
// assertion this codebase has now been bitten by twice.
func countCodeOccurrences(src, lit string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") {
			continue
		}
		n += strings.Count(t, lit)
	}
	return n
}

// lastHostSeam returns the fake handed to the most recent entry-point call.
func lastHostSeam(t *testing.T) *fakeSys {
	t.Helper()
	if len(hostSeamFakes) == 0 {
		t.Fatal("no host seam was constructed — the entry point never asked for one, so this test proves nothing")
	}
	return hostSeamFakes[len(hostSeamFakes)-1]
}

// TestInstallCmd_CannotReachTheRealHost drives the FULL CLI dispatch
// (`realMain{"install","--force"}`) at the CANONICAL instance — no OC_NAMESPACE, so
// the launchd label is exactly com.officraft.ocwarden, the live production warden's
// label — and proves the run is entirely absorbed by the injected seam.
//
// --force is deliberate: it DISABLES installer.guard, i.e. this is the run with the
// guard already out of the way. The assertions below are therefore about the seam
// and nothing else, which is the point — "we added a check" would not survive here.
//
// HOME is a t.TempDir() so that the filesystem blast radius is zero even if this
// test itself is wrong; the LAUNCHD LABEL is NOT sandboxed, because a launchd label
// is a singleton in the uid's gui domain and does not follow HOME. The label under
// test is the live one, and the recorded bootout target proves it.
func TestInstallCmd_CannotReachTheRealHost(t *testing.T) {
	hostSeamFakes = nil
	home := t.TempDir()
	agentSrc := filepath.Join(home, "ocagent-src")
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(selfExe); rerr == nil {
		selfExe = resolved
	}
	// Seed the two files install READS (its own binary as the copy source, and the
	// OC_AGENT_BIN override) INSIDE the fake, so the run proceeds all the way to
	// launchctl. Note what this proves on its own: even reading the test binary
	// off disk goes through the seam.
	hostSeamSeed = func(f *fakeSys) {
		f.existing[selfExe] = []byte("ocwarden-bytes")
		f.existing[filepath.Join(filepath.Dir(selfExe), "officraft")] = []byte("officraft-anchor-bytes")
		f.existing[agentSrc] = []byte("ocagent-bytes")
	}
	defer func() { hostSeamSeed = nil }()
	// A real (harmless) executable inside the sandbox, so the install-time runtime
	// resolution (resolveClaudeBin → isExecutableFile, a read-only real-OS probe
	// that deliberately stays outside the mutating seam) finds a candidate and the
	// run reaches launchctl. The seam still refuses to EXEC it (claudeProbe).
	claudeStub := filepath.Join(home, "claude")
	if werr := os.WriteFile(claudeStub, []byte("#!/bin/sh\nexit 0\n"), 0o755); werr != nil {
		t.Fatalf("write claude stub: %v", werr)
	}
	var out strings.Builder

	rc := realMain([]string{"install", "--force"}, envFn(map[string]string{
		"HOME":          home,
		"OC_BASE":       "http://127.0.0.1:7755",
		"OC_TOKEN":      fakeJWT("m-canonical"),
		"OC_AGENT_BIN":  agentSrc,
		"OC_CLAUDE_BIN": claudeStub,
	}), &out)

	f := lastHostSeam(t)

	// 1. This really was the dangerous path: the run addressed the LIVE canonical
	//    label in the real gui domain. If this assertion ever fails the test has
	//    stopped covering the thing it exists for.
	wantTarget := "gui/" + itoa(os.Getuid()) + "/" + wardenLabel
	var bootedOut bool
	for _, r := range f.runs {
		if r.name == "launchctl" && len(r.args) >= 2 && r.args[0] == "bootout" && r.args[1] == wantTarget {
			bootedOut = true
		}
	}
	if !bootedOut {
		t.Fatalf("install did not even attempt `launchctl bootout %s` (rc=%d) — this test no longer exercises the canonical-instance path:\nruns=%v\nout=%s",
			wantTarget, rc, f.runs, out.String())
	}

	// 2. …and every one of those attempts landed in the fake. Only launchctl/plutil
	//    are legitimate subprocess names at all, and none of them ran for real.
	assertNoForbiddenProcessKill(t, f)

	// 3. Nothing was written outside the sandbox: every path the run wrote or
	//    renamed is under the temp HOME, and the real one is untouched.
	realHome := os.Getenv("HOME")
	for p := range f.writes {
		if realHome != "" && strings.HasPrefix(p, realHome+string(os.PathSeparator)) {
			t.Fatalf("install addressed a path under the REAL home: %s", p)
		}
	}
	if realHome != "" {
		if _, err := os.Stat(filepath.Join(realHome, ".officraft", "warden", "exec-warden.tok.tmp")); err == nil {
			t.Fatalf("install left a real artifact under %s/.officraft/warden — the seam leaked", realHome)
		}
	}

	// 4. POSITIVE marker (lesson: a sentinel that only speaks when it fails cannot
	//    be told apart from a broken one). The plist the run RENDERED is observable
	//    in the fake, which is only possible because the fake absorbed the write.
	plist := filepath.Join(home, "Library", "LaunchAgents", wardenLabel+".plist")
	if _, ok := f.writes[plist]; !ok {
		if _, ok := f.writes[plist+".tmp"]; !ok {
			t.Errorf("expected the rendered plist to be captured by the fake at %s; writes=%v", plist, keysOf(f.writes))
		}
	}
}

// TestTeardownCmd_CannotReachTheRealHost is the same proof for the inverse verb —
// the one that actually cost this fleet three live-warden unloads. `ocwarden
// teardown --canonical` takes NO identity: whatever HOME says, the label it boots
// out is the canonical singleton. Structurally absorbed, exactly like install.
//
// REBASE NOTE (independent review of T-2257): --canonical is now REQUIRED to reach
// this path at all. Without it validateTeardownTarget refuses and the run never
// gets as far as launchctl — which would turn this test green for the WRONG
// reason (nothing booted out because nothing ran). The flag keeps the seam proof
// pointed at the destructive path it exists to cover; T-2257's own tests cover the
// refusal.
func TestTeardownCmd_CannotReachTheRealHost(t *testing.T) {
	hostSeamFakes = nil
	home := t.TempDir()
	var out strings.Builder

	realMain([]string{"teardown", "--canonical"}, envFn(map[string]string{"HOME": home}), &out)

	f := lastHostSeam(t)
	wantTarget := "gui/" + itoa(os.Getuid()) + "/" + wardenLabel
	var bootedOut bool
	for _, r := range f.runs {
		if r.name == "launchctl" && len(r.args) >= 2 && r.args[0] == "bootout" && r.args[1] == wantTarget {
			bootedOut = true
		}
	}
	if !bootedOut {
		t.Fatalf("teardown did not attempt `launchctl bootout %s` — the test no longer covers the canonical path:\nruns=%v\nout=%s",
			wantTarget, f.runs, out.String())
	}
	assertNoForbiddenProcessKill(t, f)
}

// TestHostSeam_StructureIsReported is the single named REPORTING WRAPPER over
// scanHostSeamSource. It is NOT the enforcement point and it is NOT coverage: the
// gate is TestMain, which runs the same scan before m.Run() and os.Exit(1)s on any
// violation, so by the time this function is reachable the scan is known empty and
// the loop body below never executes. It exists only so the property has a NAME in
// `go test -v` output and in cli/CLAUDE.md, and so something still speaks if
// TestMain's gate is ever weakened or removed.
//
// ⚠️ DO NOT ADD A SECOND ONE. There used to be two functions here —
// TestHostSeam_SingleConstructionPoint and
// TestHostSeam_RealHostSeamIsNeverCalledDirectly — with BYTE-IDENTICAL bodies,
// both looping over the same already-empty slice. Independent review replaced both
// loop bodies with panic(), ran the whole package, and got `ok` with both tests
// reported as PASS: two green lines in the log, zero executed statements, and a
// reader could reasonably have counted them as two independent checks. Each
// property enforced by the scan is documented in scanHostSeamSource's own doc
// comment (1)–(4); that is where a new property goes, not into a new no-op test.
func TestHostSeam_StructureIsReported(t *testing.T) {
	for _, v := range scanHostSeamSource() {
		t.Errorf("host seam structure violated: %s", v)
	}
}

// countRealSysOpsCalls counts CALL SITES of realSysOps() in Go source: comment
// lines (which discuss the seam at length, on purpose) and the function's own
// declaration are not call sites and must not be counted, or the guard would be
// impossible to keep green while documenting itself.
func countRealSysOpsCalls(src string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "func realSysOps()") {
			continue
		}
		n += strings.Count(t, "realSysOps()")
	}
	return n
}

// itoa avoids pulling strconv into this file's import list for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ───────────────────────────────────────────────────────────────────────────
// (5) THE ZERO-ROWS QUERY — every process-starting call site must be sanctioned
// ───────────────────────────────────────────────────────────────────────────
//
// WHY THE CHECKS ABOVE WERE NOT ENOUGH, STATED AS A PROPERTY OF THEIR SHAPE
// ------------------------------------------------------------------------
// Checks (1)-(4) derive their SCOPE from an enumerated list of identifiers and
// filenames: realSysOps, realHostSeam, `sysOps{`, `execRunner{`, install.go,
// main.go. A list like that is only as wide as the last person's memory, and —
// this is the part that makes it dangerous rather than merely incomplete — when
// it goes out of date it does not fail. It SHRINKS ITS OWN COVERAGE AND STAYS
// GREEN.
//
// That is not a hypothetical. cutover.go arrived with a whole second seam
// (cutoverOps) whose real binding runs `launchctl bootout` on the canonical
// label, overwrites the live plist and spawns a DETACHED machine conversion.
// Every check above passed, unchanged, without ever having heard of it — and a
// hand-assembled `cutoverOps{runExit: realRunExit, spawnDetached:
// spawnDetachedProcess}` in a non-test function really started a process from
// inside the test binary. Adding "cutoverOps{" to check (4) fixes THAT seam and
// leaves the shape of the defect completely intact for the next one.
//
// So this check has a different shape on purpose: a QUERY THAT MUST RETURN ZERO
// ROWS. It enumerates every call site that can START A PROCESS anywhere in the
// package's non-test sources — exec.Command, exec.CommandContext, syscall.Exec,
// syscall.ForkExec/StartProcess, os.StartProcess, and a hand-built exec.Cmd
// literal — subtracts the sanctioned choke points below, and REQUIRES THE
// REMAINDER TO BE EMPTY. A new file, a new function, a new seam: all of them
// appear in the remainder automatically. "Is the coverage still right?" is
// answered by running it, not by remembering to update a table.
//
// AST, not grep, for the reason this repo has now recorded three times: comments
// and string literals are not expression nodes, so a scan built on go/parser
// cannot be satisfied by its own documentation. This very file writes
// `exec.Command` in prose a dozen times.
type processStarter struct {
	// why must be a real reason. Enforced at ≥40 chars, mirroring the server's
	// authz inventory gate: the check cannot stop someone writing a fluent
	// nothing, but it CAN force the entry to appear in the diff, where a human
	// reads it. Treat every entry here as a claim to be checked, not a decision
	// already approved.
	why string
	// mustRefuse marks the sites whose production effect lands on THIS machine's
	// warden — the launchd job, the live plist, or this very process image. Their
	// bodies must OPEN with refuseInTestBinary, because a source scan is exactly
	// what a bad edit defeats and these are the sites where being wrong costs a
	// developer their running warden.
	mustRefuse bool
}

var sanctionedProcessStarters = map[string]processStarter{
	"main.go:(execRunner).Run": {
		why:        "THE process choke point of the whole binary: every launchctl bootout/bootstrap/kickstart, plutil, tmux and probe that is not already behind a seam ends up here, so it is the last place a caller can be stopped before the host is touched.",
		mustRefuse: true,
	},
	"cutover.go:spawnDetachedProcess": {
		why:        "Starts the DETACHED anchor converter (setsid + Release), whose production argv runs `ocwarden cutover-anchor` -> `install --force` -> bootout of the live canonical job. Worse than any other site here: the child outlives `go test`, so an escape is not even bounded by the run that caused it.",
		mustRefuse: true,
	},
	"cutover.go:realRunExit": {
		why:        "Runs the anchor preflight for its EXIT CODE. It calls exec directly rather than through execRunner.Run, so it needs its own refusal — a cutoverOps struct assembled by hand still has to start its subprocess here.",
		mustRefuse: true,
	},
	"cutover.go:runInstallerCombined": {
		why:        "Runs `ocwarden install --force`, i.e. the machine conversion itself: deploys the anchor, rewrites the plist, boots the launchd job out and back in. The single most destructive argv in this package.",
		mustRefuse: true,
	},
	"selfupdate.go:syscallExecImage": {
		why:        "THE syscall.Exec that REPLACES THIS PROCESS IMAGE with the swapped ocwarden. Under `go test` that would replace the test binary itself. It used to be an inline closure inside newSelfUpdater, and the constructor refused as a whole; splitting a testable buildSelfUpdater out of that constructor moved the syscall to a site this inventory could not name, and TestMain refused the whole suite until it was named here — which is the check working. The refusal now sits on the syscall itself, so constructing an updater is safe and only handing over the real exec is not.",
		mustRefuse: true,
	},
	"install.go:realClaudeProbe": {
		why: "Runs `<claude> --version` to prove the resolved CLI is executable under a minimal PATH. Read-only, and it is one of the fields of hostSeam, so TestMain's rebinding of newHostSeam already keeps every test off it.",
	},
	"interactiveenv.go:captureInteractiveEnv": {
		why: "Runs the operator's login shell to dump an interactive environment. Read-only with respect to this machine's warden, and interactiveenv_test.go drives it DIRECTLY against stub shells it writes into t.TempDir(), which is the tested behaviour rather than an escape.",
	},
	"codex_session.go:runCodexSession": {
		why: "The `ocwarden codex-session` subcommand: starts the codex app-server and, on demand, an ocagent listener. A foreground operator verb that does not touch launchd, the plist or the anchor; its process tree dies with the command.",
	},
}

// processStartAPIs are the calls that can bring a new process into existence.
// Keyed as package.Symbol because that is what an AST selector expression gives
// us without type information; a local variable happening to be called `exec` is
// not a realistic way to hide a process start, whereas a new FILE is exactly how
// the last one hid.
var processStartAPIs = map[string]bool{
	"exec.Command":         true,
	"exec.CommandContext":  true,
	"syscall.Exec":         true,
	"syscall.ForkExec":     true,
	"syscall.StartProcess": true,
	"os.StartProcess":      true,
}

// funcKey names a function the way sanctionedProcessStarters keys it.
func funcKey(file string, fn *ast.FuncDecl) string {
	name := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0].Type
		if star, isStar := recv.(*ast.StarExpr); isStar {
			recv = star.X
		}
		if id, isIdent := recv.(*ast.Ident); isIdent {
			name = "(" + id.Name + ")." + name
		}
	}
	return file + ":" + name
}

// startsAProcess reports whether fn's body contains a call that creates a
// process, including a hand-built `exec.Cmd{…}` literal (the composite-literal
// route around the constructor, the same trick that defeated check (1)/(2)).
func startsAProcess(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok &&
					processStartAPIs[id.Name+"."+sel.Sel.Name] {
					found = true
				}
			}
		case *ast.CompositeLit:
			if sel, ok := node.Type.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok &&
					id.Name == "exec" && sel.Sel.Name == "Cmd" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// opensWithRefusal reports whether fn's FIRST statement is refuseInTestBinary(…).
// First statement, not "contains": a refusal that runs after the exec is the
// after-the-fact detection this whole file exists to stop settling for.
func opensWithRefusal(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return false
	}
	stmt, ok := fn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "refuseInTestBinary"
}

// parsePackageFuncs parses dir's Go files (test files iff wantTests) and calls
// visit for every function declaration with a body.
func parsePackageFuncs(dir string, wantTests bool, visit func(file string, fn *ast.FuncDecl)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") != wantTests {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		for _, decl := range parsed.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				visit(name, fn)
			}
		}
	}
	return nil
}

// scanProcessStarters is the zero-rows query described above.
func scanProcessStarters() []string { return scanProcessStartersIn(".") }

func scanProcessStartersIn(dir string) []string {
	var out []string
	seen := map[string]bool{}
	err := parsePackageFuncs(dir, false, func(file string, fn *ast.FuncDecl) {
		if !startsAProcess(fn) {
			return
		}
		key := funcKey(file, fn)
		seen[key] = true
		sanctioned, listed := sanctionedProcessStarters[key]
		if !listed {
			out = append(out, fmt.Sprintf("%s STARTS A PROCESS and is not a sanctioned choke point.\n"+
				"\t  Every process-starting call site in this package must be listed in sanctionedProcessStarters with a real reason, so that adding one is a visible decision in the diff rather than a silent widening of what a test binary can do to the machine it runs on. If this site can touch this machine's launchd job, plist or process image, it must also open with refuseInTestBinary and be marked mustRefuse.", key))
			return
		}
		if len(sanctioned.why) < 40 {
			out = append(out, fmt.Sprintf("%s is sanctioned with a %d-character reason; write a real one (>=40 chars) — an entry nobody can evaluate is not a decision", key, len(sanctioned.why)))
		}
		if sanctioned.mustRefuse && !opensWithRefusal(fn) {
			out = append(out, fmt.Sprintf("%s lost its runtime backstop: its body must OPEN with refuseInTestBinary(...).\n"+
				"\t  This site's production effect lands on this machine's own warden, and every other guard here is a SOURCE SCAN — which is precisely what a bad edit defeats. The refusal on the syscall is the layer a hand-assembled struct cannot route around, because however the struct was built the process still has to be started here.", key))
		}
	})
	if err != nil {
		return append(out, fmt.Sprintf("cannot enumerate process-starting call sites: %v (fail closed)", err))
	}
	// ANTI-VACUITY. A scanner that parsed nothing reports nothing, and "no rows"
	// would then read exactly like "no unsanctioned sites". The corpus proves
	// itself non-empty before its verdict means anything.
	if len(seen) == 0 {
		out = append(out, "the process-starter scan found NO process-starting call site at all — this package unquestionably has several, so the scanner is broken and its silence must not be read as a pass")
	}
	for key := range sanctionedProcessStarters {
		if !seen[key] {
			out = append(out, fmt.Sprintf("sanctionedProcessStarters lists %s, which no longer starts a process (or no longer exists). A stale entry is a finding, not housekeeping: it means the inventory and the code have drifted, and the next reader cannot tell which of the two is right", key))
		}
	}
	return out
}

// realHostFunctions are the functions a TEST must never call directly: the real
// seam constructors and the process-starting choke points. Production binds
// them; tests bind a substitute. The measurable criterion is that this list's
// call count from test files is ZERO — which is a property a scan can check,
// unlike "tests are careful".
//
// captureInteractiveEnv is deliberately NOT here even though it starts a
// process: interactiveenv_test.go drives it against stub shells it wrote into
// t.TempDir(), and that IS the behaviour under test. The line this list draws is
// "can it affect this machine's warden", not "does it fork".
var realHostFunctions = map[string]string{
	"realSysOps":           "wires the seam to the real OS",
	"realHostSeam":         "constructs the real host seam",
	"realCutoverOps":       "wires the anchor-cutover seam to launchctl, the live plist and a detached converter",
	"realRunExit":          "execs for an exit code outside execRunner",
	"spawnDetachedProcess": "starts a setsid'd child that outlives the test run",
	"runInstallerCombined": "runs the machine conversion",
}

// scanTestsForRealHostCalls returns one row per direct call from a TEST file to
// one of the functions above. It must return zero rows.
func scanTestsForRealHostCalls() []string { return scanTestsForRealHostCallsIn(".") }

func scanTestsForRealHostCallsIn(dir string) []string {
	var out []string
	err := parsePackageFuncs(dir, true, func(file string, fn *ast.FuncDecl) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || realHostFunctions[id.Name] == "" {
				return true
			}
			out = append(out, fmt.Sprintf("%s:%s calls %s() directly — that function %s, and a test must take its effects from a substitute instead. Bind the seam, do not call the real one",
				file, fn.Name.Name, id.Name, realHostFunctions[id.Name]))
			return true
		})
	})
	if err != nil {
		out = append(out, fmt.Sprintf("cannot scan test files for direct real-host calls: %v (fail closed)", err))
	}
	return out
}

// ───────────────────────────────────────────────────────────────────────────
// POSITIVE CONTROLS — the guards are run against KNOWN-BAD source and must
// reject it
// ───────────────────────────────────────────────────────────────────────────
//
// ⚠️ READ BEFORE DELETING THESE AS "a second copy of TestHostSeam_StructureIsReported".
// They are not. That one re-runs the scan over the SAME tree TestMain already
// passed, so its loop body is provably unreachable — cli/CLAUDE.md says so and
// says not to add another like it. These run the scan over a DIFFERENT INPUT: a
// staged copy of the package with a known-bad file added. Their assertions fail
// whenever the corresponding rule is removed, which is the entire property a
// guard has to have and the one nobody had checked.
//
// The known-bad shapes live HERE, as source the scan is executed against, rather
// than in prose. A mutant described in a comment protects nothing; independent
// review demonstrated exactly this one — `cutoverOps{runExit: realRunExit,
// spawnDetached: spawnDetachedProcess}` in a non-test function — passing every
// static guard and really starting a process from inside the test binary. Now it
// is a fixture, and the day someone drops the rule that catches it, this goes red.

// stagePackageSources copies the package's NON-TEST .go files into a fresh temp
// dir. The copy is the corpus every mutant below is added to, so each case
// differs from a passing tree by exactly one file.
func stagePackageSources(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package sources: %v", err)
	}
	copied := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
		copied++
	}
	if copied == 0 {
		t.Fatal("staged no sources at all — every case below would then be judging an empty directory")
	}
	return dir
}

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

// mentions reports whether any violation contains every one of want.
func mentions(violations []string, want ...string) bool {
	for _, v := range violations {
		hit := true
		for _, w := range want {
			if !strings.Contains(v, w) {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

// TestHostSeamGuards_StagedCopyOfThisPackageIsClean is the BASELINE every case
// below depends on. If the staged copy were rejected for some unrelated reason,
// every mutant case would "pass" without the mutant contributing anything — the
// vacuous-positive-control failure mode.
func TestHostSeamGuards_StagedCopyOfThisPackageIsClean(t *testing.T) {
	dir := stagePackageSources(t)
	if v := scanHostSeamSourceIn(dir); len(v) > 0 {
		t.Fatalf("a faithful copy of this package must pass the structure scan; got %v", v)
	}
	if v := scanProcessStartersIn(dir); len(v) > 0 {
		t.Fatalf("a faithful copy of this package must pass the process-starter query; got %v", v)
	}
}

// TestHostSeamGuards_RejectHandAssembledSeams replays, as executable fixtures,
// both hand-assembled seams that have actually defeated a guard in this repo:
// the T-5047 sysOps mutant, and the cutoverOps mutant independent review built
// for T-ff5d after cutover.go introduced a second seam the scans knew nothing
// about.
func TestHostSeamGuards_RejectHandAssembledSeams(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, wantFile, wantLiteral string
	}{
		{
			name:     "the T-5047 sysOps mutant",
			wantFile: "zz_mutant_sysops.go",
			fixture: `package main

import "os"

func hijackedTeardown() sysOps {
	return sysOps{run: newCmdRunner(0).Run, rename: os.Rename, remove: os.Remove}
}
`,
			wantLiteral: "sysOps{",
		},
		{
			name:     "the T-ff5d cutoverOps mutant, verbatim",
			wantFile: "zz_mutant_cutoverops.go",
			fixture: `package main

func hijackedCutover() cutoverOps {
	return cutoverOps{runExit: realRunExit, spawnDetached: spawnDetachedProcess}
}
`,
			wantLiteral: "cutoverOps{",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := stagePackageSources(t)
			writeFixture(t, dir, tc.wantFile, tc.fixture)
			violations := scanHostSeamSourceIn(dir)
			if !mentions(violations, tc.wantFile, tc.wantLiteral) {
				t.Fatalf("a hand-assembled %s in %s went UNREPORTED (scan said %v).\n"+
					"This exact shape has reached a live launchd domain from a test binary before. "+
					"Without this rule the mutant compiles, every identifier scan stays green, and "+
					"the seam is simply never consulted.", tc.wantLiteral, tc.wantFile, violations)
			}
		})
	}
}

// TestProcessStarterQuery_RejectsAnUnsanctionedNewSite is the control for the
// property the enumerated lists did NOT have: a process-starting site in a file
// the guard has never heard of must be caught WITHOUT anyone extending a table.
func TestProcessStarterQuery_RejectsAnUnsanctionedNewSite(t *testing.T) {
	for _, tc := range []struct{ name, file, fixture string }{
		{
			name: "exec.Command in a brand-new file",
			file: "zz_new_seam.go",
			fixture: `package main

import "os/exec"

func quietlyBootsOutTheLiveWarden() error {
	return exec.Command("launchctl", "bootout", "gui/501/com.officraft.ocwarden").Run()
}
`,
		},
		{
			name: "a hand-built exec.Cmd literal, routing around the constructor",
			file: "zz_new_literal.go",
			fixture: `package main

import "os/exec"

func stillStartsAProcess() error {
	cmd := exec.Cmd{Path: "/bin/launchctl", Args: []string{"launchctl", "bootout"}}
	return cmd.Run()
}
`,
		},
		{
			name: "syscall.Exec replacing the process image",
			file: "zz_new_execve.go",
			fixture: `package main

import "syscall"

func replacesTheTestBinary() error {
	return syscall.Exec("/bin/launchctl", []string{"launchctl"}, nil)
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := stagePackageSources(t)
			writeFixture(t, dir, tc.file, tc.fixture)
			violations := scanProcessStartersIn(dir)
			if !mentions(violations, tc.file, "STARTS A PROCESS") {
				t.Fatalf("an unsanctioned process start in %s went UNREPORTED (query said %v).\n"+
					"The whole point of this query is that a NEW file cannot widen what a test "+
					"binary may do to the machine without appearing in the diff.", tc.file, violations)
			}
		})
	}
}

// TestProcessStarterQuery_RejectsALostRuntimeBackstop covers the other half:
// the inventory can be perfectly up to date while the refusal that actually
// prevents the exec has been deleted. Source scans are what a bad edit defeats,
// so the layer that survives one is checked too.
func TestProcessStarterQuery_RejectsALostRuntimeBackstop(t *testing.T) {
	dir := stagePackageSources(t)
	path := filepath.Join(dir, "cutover.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read staged cutover.go: %v", err)
	}
	const refusal = "\trefuseInTestBinary(\"spawnDetachedProcess(\" + bin + \")\")\n"
	if !strings.Contains(string(raw), refusal) {
		t.Fatalf("precondition: spawnDetachedProcess no longer opens with the expected refusal, so this control is testing nothing")
	}
	writeFixture(t, dir, "cutover.go", strings.Replace(string(raw), refusal, "", 1))

	violations := scanProcessStartersIn(dir)
	if !mentions(violations, "cutover.go:spawnDetachedProcess", "runtime backstop") {
		t.Fatalf("deleting the refusal from spawnDetachedProcess went UNREPORTED (query said %v).\n"+
			"That function setsids and releases its child, so an escape outlives the test run "+
			"that caused it, and its production argv converts the machine for real.", violations)
	}
}

// TestRealHostFunctionsAreNeverCalledFromTests states the isolation criterion as
// a number: direct calls from test files to the real system-operation functions
// must be ZERO. TestMain enforces it over the real tree; this case proves the
// check can actually see one.
func TestRealHostFunctionsAreNeverCalledFromTests(t *testing.T) {
	if v := scanTestsForRealHostCalls(); len(v) > 0 {
		t.Fatalf("test files call real host functions directly: %v", v)
	}
	dir := t.TempDir()
	writeFixture(t, dir, "zz_bad_test.go", `package main

import "testing"

func TestSomethingCareless(t *testing.T) {
	ops := realCutoverOps()
	_ = ops
}
`)
	violations := scanTestsForRealHostCallsIn(dir)
	if !mentions(violations, "zz_bad_test.go", "realCutoverOps") {
		t.Fatalf("a test calling realCutoverOps() directly went UNREPORTED (scan said %v)", violations)
	}
}
