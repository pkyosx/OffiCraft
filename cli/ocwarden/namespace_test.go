package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// The zero-diff contract lives in the UNTOUCHED golden tests (install_test.go /
// spawn_test.go / command_test.go / teardown_test.go): every empty-namespace
// derivation must keep them green without a single edit. This file adds the
// namespaced counterparts: the derivation table (empty == the historical
// literals, pinned as literals so a refactor cannot silently move them),
// charset enforcement, and the ns-suffixed goldens for paths / plist / launch
// line / teardown.

func TestNamespaceFromEnv(t *testing.T) {
	valid := []string{"", "seth", "a", "a-1", "0", "sixteen-chars-xx"}
	for _, ns := range valid {
		got, err := namespaceFromEnv(envFn(map[string]string{"OC_NAMESPACE": ns}))
		if err != nil || got != ns {
			t.Errorf("namespaceFromEnv(%q) = (%q, %v), want (%q, nil)", ns, got, err, ns)
		}
	}
	invalid := []string{"Seth", "s.eth", "s eth", "s_eth", "s/eth", "seventeen-chars-xx", "開", "s\neth"}
	for _, ns := range invalid {
		if _, err := namespaceFromEnv(envFn(map[string]string{"OC_NAMESPACE": ns})); err == nil {
			t.Errorf("namespaceFromEnv(%q) must refuse (charset lock [a-z0-9-]{1,16})", ns)
		}
	}
}

func TestNamespaceDerivations(t *testing.T) {
	// Empty namespace == the historical constants, pinned as LITERALS.
	if got := wardenLabelFor(""); got != "com.officraft.ocwarden" {
		t.Errorf(`wardenLabelFor("") = %q, want the literal com.officraft.ocwarden`, got)
	}
	if got := officraftRootFor("/Users/seth", ""); got != "/Users/seth/.officraft" {
		t.Errorf(`officraftRootFor("") = %q, want the literal ~/.officraft`, got)
	}
	if got := tmuxSocketFor(""); got != "officraft" {
		t.Errorf(`tmuxSocketFor("") = %q, want the literal officraft`, got)
	}
	if got := tokfileFor("/Users/seth", ""); got != "/Users/seth/.officraft/warden/exec-warden.tok" {
		t.Errorf(`tokfileFor("") = %q, want the historical literal`, got)
	}
	// Non-empty namespace: dot for the label, dash for path/socket.
	if got := wardenLabelFor("seth"); got != "com.officraft.ocwarden.seth" {
		t.Errorf(`wardenLabelFor("seth") = %q`, got)
	}
	if got := officraftRootFor("/Users/seth", "seth"); got != "/Users/seth/.officraft-seth" {
		t.Errorf(`officraftRootFor("seth") = %q`, got)
	}
	if got := tmuxSocketFor("seth"); got != "officraft-seth" {
		t.Errorf(`tmuxSocketFor("seth") = %q`, got)
	}
	if got := tokfileFor("/Users/seth", "seth"); got != "/Users/seth/.officraft-seth/warden/exec-warden.tok" {
		t.Errorf(`tokfileFor("seth") = %q`, got)
	}
}

func TestResolvePaths_Namespaced(t *testing.T) {
	p, err := resolvePaths(envFn(map[string]string{
		"HOME":         "/Users/seth",
		"OC_TOKEN":     "tok-abc",
		"OC_BASE":      "https://station.example",
		"OC_NAMESPACE": "seth",
	}), "/repo/bin/ocwarden", 501)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.namespace != "seth" || p.label != "com.officraft.ocwarden.seth" {
		t.Errorf("namespace/label = %q/%q", p.namespace, p.label)
	}
	if p.root != "/Users/seth/.officraft-seth" {
		t.Errorf("root = %q", p.root)
	}
	if p.tokfile != "/Users/seth/.officraft-seth/warden/exec-warden.tok" {
		t.Errorf("tokfile = %q", p.tokfile)
	}
	if p.plistPath != "/Users/seth/Library/LaunchAgents/com.officraft.ocwarden.seth.plist" {
		t.Errorf("plistPath = %q", p.plistPath)
	}
	if p.binPath != "/Users/seth/.officraft-seth/warden/ocwarden" {
		t.Errorf("binPath = %q", p.binPath)
	}
	if p.logDir != "/Users/seth/.officraft-seth/warden/log" {
		t.Errorf("logDir = %q", p.logDir)
	}
	if p.ocAgentBin != "/Users/seth/.officraft-seth/warden/ocagent" {
		t.Errorf("ocAgentBin = %q", p.ocAgentBin)
	}
}

func TestResolvePaths_EmptyNamespaceHasNoSuffixAnywhere(t *testing.T) {
	p, err := resolvePaths(envFn(map[string]string{
		"HOME":     "/Users/seth",
		"OC_TOKEN": "tok-abc",
		"OC_BASE":  "https://station.example",
	}), "/repo/bin/ocwarden", 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.namespace != "" || p.label != "com.officraft.ocwarden" {
		t.Errorf("empty namespace must resolve the canonical label, got %q/%q", p.namespace, p.label)
	}
}

func TestResolvePaths_InvalidNamespaceRefused(t *testing.T) {
	for _, bad := range []string{"Seth", "s.eth", "s eth", "seventeen-chars-xx"} {
		_, err := resolvePaths(envFn(map[string]string{
			"HOME": "/h", "OC_TOKEN": "t", "OC_NAMESPACE": bad,
		}), "/r/bin/ocwarden", 1)
		if err == nil {
			t.Errorf("resolvePaths must refuse OC_NAMESPACE=%q", bad)
		}
	}
}

func TestRenderPlist_NamespacedStampsLabelAndEnv(t *testing.T) {
	p := fixedPaths()
	p.namespace = "seth"
	p.label = "com.officraft.ocwarden.seth"
	out := renderPlist(p)
	if err := xmlWellFormed(out); err != nil {
		t.Fatalf("namespaced plist not well-formed XML: %v", err)
	}
	for _, must := range []string{
		"<key>Label</key><string>com.officraft.ocwarden.seth</string>",
		"<key>OC_NAMESPACE</key><string>seth</string>",
	} {
		if !strings.Contains(out, must) {
			t.Errorf("namespaced plist missing %q:\n%s", must, out)
		}
	}
	// The OC_NAMESPACE stamp is inside EnvironmentVariables, right after the
	// tokfile line (the single conditional insertion point).
	if !strings.Contains(out,
		"<key>OC_WARDEN_TOKFILE</key><string>"+p.tokfile+"</string>\n        <key>OC_NAMESPACE</key><string>seth</string>\n    </dict>") {
		t.Errorf("OC_NAMESPACE must be stamped after OC_WARDEN_TOKFILE inside EnvironmentVariables:\n%s", out)
	}
}

func TestRenderPlist_EmptyNamespaceOmitsStamp(t *testing.T) {
	// Byte-level pin: an empty namespace renders EXACTLY these bytes (label
	// const, no OC_NAMESPACE key). ProgramArguments names the TCC identity
	// anchor, which forks the sibling ocwarden — launchd must never exec
	// ocwarden directly, or the responsible identity moves with every
	// self-update.
	p := fixedPaths()
	got := renderPlist(p)
	if strings.Contains(got, "OC_NAMESPACE") {
		t.Errorf("empty-namespace plist must not carry OC_NAMESPACE:\n%s", got)
	}
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- RENDERED by ocwarden install for ROOT=` + p.root + ` — do not edit by hand; re-run the installer. -->
<plist version="1.0">
<dict>
    <key>Label</key><string>com.officraft.ocwarden</string>
    <key>ProgramArguments</key>
    <array><string>` + p.anchorPath + `</string></array>
    <key>WorkingDirectory</key><string>` + p.root + `</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        <key>OC_BASE</key><string>` + p.ocBase + `</string>
        <key>HOME</key><string>` + p.home + `</string>
        <key>OC_WARDEN_TOKFILE</key><string>` + p.tokfile + `</string>
    </dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>` + p.logDir + `/ocwarden.out.log</string>
    <key>StandardErrorPath</key><string>` + p.logDir + `/ocwarden.err.log</string>
</dict>
</plist>
`
	if got != want {
		t.Fatalf("empty-namespace plist diverged from the historical bytes:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestLaunchctlReinstall_NamespacedLabel(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn()
	p := fixedPaths()
	p.namespace = "seth"
	p.label = "com.officraft.ocwarden.seth"
	p.plistPath = "/h/Library/LaunchAgents/com.officraft.ocwarden.seth.plist"
	i := &installer{out: io.Discard, sys: f.ops()}
	if err := i.launchctlReinstall(p); err != nil {
		t.Fatalf("launchctlReinstall: %v", err)
	}
	want := []string{
		"bootout gui/501/com.officraft.ocwarden.seth",
		"print gui/501/com.officraft.ocwarden.seth",
		"bootstrap gui/501 " + p.plistPath,
		"print gui/501/com.officraft.ocwarden.seth",
		"kickstart -k gui/501/com.officraft.ocwarden.seth",
	}
	if len(f.runs) != len(want) {
		t.Fatalf("runs = %v, want %v", f.runs, want)
	}
	for k := range want {
		if got := strings.Join(f.runs[k].args, " "); got != want[k] {
			t.Errorf("run[%d] = %q, want %q", k, got, want[k])
		}
	}
	assertNoForbiddenProcessKill(t, f)
}

func TestResolveTeardownPaths_Namespaced(t *testing.T) {
	p, err := resolveTeardownPaths(envFn(map[string]string{
		"HOME": "/h", "OC_NAMESPACE": "seth",
	}), 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.label != "com.officraft.ocwarden.seth" {
		t.Errorf("label = %q", p.label)
	}
	if p.tokfile != "/h/.officraft-seth/warden/exec-warden.tok" {
		t.Errorf("tokfile = %q", p.tokfile)
	}
	if p.plistPath != "/h/Library/LaunchAgents/com.officraft.ocwarden.seth.plist" {
		t.Errorf("plistPath = %q", p.plistPath)
	}
	if _, err := resolveTeardownPaths(envFn(map[string]string{
		"HOME": "/h", "OC_NAMESPACE": "Bad.NS",
	}), 501); err == nil {
		t.Error("resolveTeardownPaths must refuse an invalid OC_NAMESPACE")
	}
}

// A lost OC_NAMESPACE used to make `ocwarden teardown` silently resolve the
// canonical label/token/root.  The CLI boundary must reject that shape before
// resolveTeardownPaths can derive or mutate anything; canonical is opt-in.
func TestValidateTeardownTarget_FailsClosedAgainstImplicitCanonical(t *testing.T) {
	canonical := envFn(map[string]string{"HOME": "/h"})
	if err := validateTeardownTarget(canonical, false); err == nil {
		t.Fatal("bare teardown with no OC_NAMESPACE must refuse implicit canonical target")
	} else if !strings.Contains(err.Error(), "--canonical") {
		t.Fatalf("refusal must tell caller how to make canonical explicit, got %v", err)
	}
	if err := validateTeardownTarget(canonical, true); err != nil {
		t.Fatalf("explicit canonical teardown must remain available, got %v", err)
	}

	namespaced := envFn(map[string]string{"HOME": "/h", "OC_NAMESPACE": "e2e-safe"})
	if err := validateTeardownTarget(namespaced, false); err != nil {
		t.Fatalf("namespaced teardown must be admitted without canonical flag, got %v", err)
	}
	if err := validateTeardownTarget(namespaced, true); err == nil {
		t.Fatal("--canonical must not override a namespaced target")
	}
}

// REBASE NOTE (independent review of T-2257, rebased onto T-5047 e169ed1):
// T-2257 shipped its own `teardownSysOps` package-var seam plus a
// withFakeTeardownSys helper. T-5047 landed a STRICTLY STRONGER version of the
// same idea — `newHostSeam`, rebound to a fake in TestMain for the WHOLE package,
// with a source scan pinning `sysOps{`/`execRunner{` to one site each and
// refuseInTestBinary on execRunner.Run. Keeping teardownSysOps would have been a
// SECOND non-test reference to realSysOps, which TestMain's scan rejects outright.
// So the seam half of T-2257 is dropped in favour of T-5047's, and the assertions
// below now read T-5047's hostSeamFakes. The GUARD half (validateTeardownTarget)
// is untouched — it is orthogonal and still load-bearing.
func TestRealMain_BareTeardownRefusesBeforeAnyHostOperation(t *testing.T) {
	hostSeamFakes = nil
	env := envFn(map[string]string{"HOME": "/h"})

	var sb strings.Builder
	rc := realMain([]string{"teardown"}, env, &sb)
	if rc != 1 {
		t.Fatalf("bare canonical teardown rc=%d, want 1; transcript:\n%s", rc, sb.String())
	}
	if !strings.Contains(sb.String(), "--canonical") {
		t.Fatalf("bare teardown refusal must name explicit canonical authorization:\n%s", sb.String())
	}
	// BEFORE ANY HOST OPERATION — asserted against the seam itself, not against
	// the printed transcript: zero subprocesses, zero removals. This is the
	// assertion the test name has always claimed and never actually made.
	f := lastHostSeam(t)
	if len(f.runs) != 0 {
		t.Fatalf("bare teardown reached the host: ran %v", f.runs)
	}
	if len(f.removed) != 0 {
		t.Fatalf("bare teardown removed files: %v", f.removed)
	}

	// Seam interlock. The assertions above are only meaningful if the fake is
	// really the sysOps teardownCmd uses; if the seam were bypassed, f would
	// stay empty for the WRONG reason and the case above would be vacuous AND
	// live-fire. So drive an ADMITTED target through the same seam and require
	// the fake to have recorded it. Namespaced (not --canonical) on purpose:
	// even if this ever escaped to the real sysOps, com.officraft.ocwarden.seamprobe
	// and /h/.officraft-seamprobe exist on no host.
	hostSeamSeed = func(fs *fakeSys) { fs.runFn = labelGoneRunFn() }
	defer func() { hostSeamSeed = nil }()
	probe := envFn(map[string]string{"HOME": "/h", "OC_NAMESPACE": "seamprobe"})
	var ns strings.Builder
	if rc := realMain([]string{"teardown"}, probe, &ns); rc != 0 {
		t.Fatalf("namespaced teardown rc=%d, want 0:\n%s", rc, ns.String())
	}
	pf := lastHostSeam(t)
	if pf == f {
		t.Fatal("seam interlock FAILED: the namespaced run reused the refused run's seam")
	}
	if len(pf.runs) == 0 {
		t.Fatal("seam interlock FAILED: teardownCmd did not use the injected sysOps — " +
			"the refusal assertions above prove nothing and this test can reach real launchd")
	}
	wantFirst := fmt.Sprintf("bootout gui/%d/com.officraft.ocwarden.seamprobe", os.Getuid())
	if got := strings.Join(pf.runs[0].args, " "); got != wantFirst {
		t.Fatalf("seam interlock recorded first call %q, want %q (uid/label derivation moved?)", got, wantFirst)
	}
	// And nothing canonical may ever appear on the recorded surface.
	for _, r := range pf.runs {
		if strings.HasSuffix(strings.Join(r.args, " "), "/com.officraft.ocwarden") {
			t.Fatalf("canonical launchd label reached the seam: %v", r)
		}
	}
	for _, rm := range pf.removed {
		if strings.Contains(rm, "/.officraft/") || strings.HasSuffix(rm, "com.officraft.ocwarden.plist") {
			t.Fatalf("canonical artifact reached the seam: %q", rm)
		}
	}
}

func TestDoTeardown_NamespacedActsOnOwnLabelOnly(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn()
	p := teardownPaths{
		tokfile:   "/h/.officraft-seth/warden/exec-warden.tok",
		plistPath: "/h/Library/LaunchAgents/com.officraft.ocwarden.seth.plist",
		guiDomain: "gui/501",
		label:     "com.officraft.ocwarden.seth",
	}
	ok, log := doTeardown(f.ops(), false, p)
	if !ok {
		t.Fatalf("doTeardown ok=false; log:\n%s", log)
	}
	if got := strings.Join(f.runs[0].args, " "); got != "bootout gui/501/com.officraft.ocwarden.seth" {
		t.Errorf("bootout target = %q", got)
	}
	if !strings.Contains(log, "teardown complete for com.officraft.ocwarden.seth") {
		t.Errorf("log must name the namespaced label:\n%s", log)
	}
	// Only the namespaced artifacts are removed — never the main instance's.
	for _, r := range f.removed {
		if !strings.Contains(r, "-seth") && !strings.Contains(r, ".seth") {
			t.Errorf("removed a non-namespaced path: %q", r)
		}
	}
}

func TestDefaultAgentHome_Namespace(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := defaultAgentHome(envFn(map[string]string{})); got != home+"/.officraft/agents" {
		t.Errorf("empty namespace agent home = %q, want the historical default", got)
	}
	if got := defaultAgentHome(envFn(map[string]string{"OC_NAMESPACE": "seth"})); got != home+"/.officraft-seth/agents" {
		t.Errorf("namespaced agent home = %q", got)
	}
	if got := defaultAgentHome(envFn(map[string]string{"OC_AGENT_HOME": "/x", "OC_NAMESPACE": "seth"})); got != "/x" {
		t.Errorf("OC_AGENT_HOME override must win, got %q", got)
	}
}

func TestBuildLaunchCommandWithEnv_AgentHomeExport(t *testing.T) {
	appendSys := buildAppendSystemPrompt(fxID, fxRole, fxPersona)
	base := buildLaunchCommand(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, "officraft-seth", fxModel, "", fxSettings)
	got := buildLaunchCommandWithEnv(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, "officraft-seth", fxModel, "", fxSettings,
		[][2]string{{"OC_AGENT_HOME", "/home/oc/.officraft-seth/agents"}}, "")
	// The extra export lands INSIDE the frozen export statement, right after
	// OC_TMUX_SOCKET and before the PATH export; everything else is unchanged.
	if !strings.Contains(got, "OC_TMUX_SOCKET=officraft-seth OC_AGENT_HOME=/home/oc/.officraft-seth/agents; export PATH=") {
		t.Errorf("OC_AGENT_HOME must follow OC_TMUX_SOCKET inside the export statement:\n%s", got)
	}
	// nil extra == the historical line (the delegation identity).
	if buildLaunchCommandWithEnv(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, "officraft-seth", fxModel, "", fxSettings, nil, "") != base {
		t.Error("nil extraEnv must reproduce buildLaunchCommand byte-for-byte")
	}
}

func TestStart_NamespaceExportsAgentHome(t *testing.T) {
	hasKey := "tmux -L officraft-seth has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	files := map[string]string{}
	links := map[string]string{}
	deps := newStartDepsLinks(t, run, files, links)
	deps.Socket = "officraft-seth"
	deps.Namespace = "seth"
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	var launch string
	for _, c := range run.calls {
		if len(c) >= 4 && c[3] == "new-session" {
			launch = c[len(c)-1]
		}
	}
	if launch == "" {
		t.Fatal("no new-session recorded")
	}
	if !strings.Contains(launch, "OC_AGENT_HOME="+deps.Home) {
		t.Errorf("namespaced spawn must export OC_AGENT_HOME=%s; launch:\n%s", deps.Home, launch)
	}
	if !strings.Contains(launch, "OC_TMUX_SOCKET=officraft-seth") {
		t.Errorf("namespaced spawn must export the namespaced socket; launch:\n%s", launch)
	}
}

func TestStart_EmptyNamespaceOmitsAgentHomeExport(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	for _, c := range run.calls {
		if len(c) >= 4 && c[3] == "new-session" && strings.Contains(c[len(c)-1], "OC_AGENT_HOME") {
			t.Errorf("empty namespace must NOT export OC_AGENT_HOME (zero-diff); launch:\n%s", c[len(c)-1])
		}
	}
}

func TestRealMain_RunRefusesInvalidNamespace(t *testing.T) {
	var sb strings.Builder
	rc := realMain([]string{"run", "--once"}, envFn(map[string]string{
		"OC_NAMESPACE": "Bad.NS",
	}), &sb)
	if rc != 1 {
		t.Fatalf("rc = %d, want 1 (invalid OC_NAMESPACE refused)", rc)
	}
	if !strings.Contains(sb.String(), "OC_NAMESPACE") {
		t.Errorf("refusal must name OC_NAMESPACE:\n%s", sb.String())
	}
}

// Operator guidance must be RUNNABLE. `ocwarden teardown` now fails closed on an
// implicit canonical target, so every message that tells an operator to run it
// has to spell the explicit form — otherwise the official recovery instruction
// is itself refused and the operator has no way forward. The install guard's
// refusal is the one such message in this binary; the hint has to match the
// instance being installed, because handing a namespaced install
// "--canonical" would point it at the LIVE canonical warden.
func TestGuardRefusal_TeachesARunnableTeardown(t *testing.T) {
	refuse := func(ns string) string {
		t.Helper()
		f := newFakeSys()
		p := fixedPaths()
		p.namespace = ns
		p.ocID = "m-aaa"
		p.ocToken = fakeJWT("m-aaa")
		f.existing[p.tokfile] = []byte(fakeJWT("m-bbb"))
		i := &installer{out: io.Discard, sys: f.ops()}
		err := i.guard(p)
		if err == nil {
			t.Fatal("guard must refuse a foreign warden")
		}
		return err.Error()
	}

	canonical := refuse("")
	if !strings.Contains(canonical, "ocwarden teardown --canonical") {
		t.Errorf("canonical install must teach the EXPLICIT teardown; got: %s", canonical)
	}
	if strings.Contains(canonical, "'ocwarden teardown'") {
		t.Errorf("the bare form is refused by the CLI — an operator following it is stuck: %s", canonical)
	}

	namespaced := refuse("seth")
	if !strings.Contains(namespaced, "OC_NAMESPACE=seth ocwarden teardown") {
		t.Errorf("namespaced install must teach its OWN teardown; got: %s", namespaced)
	}
	if strings.Contains(namespaced, "--canonical") {
		t.Errorf("a namespaced install must NEVER point the operator at the canonical warden: %s", namespaced)
	}
}

// The refusal is read by AUTOMATION as often as by humans (the incident vector
// was an automated cleanup). It must lead with WHAT IS DESTROYED and offer the
// namespaced escape BEFORE it names the flag that authorizes the destruction —
// a retry wrapper scraping stderr should meet the safe option first.
func TestTeardownRefusal_LeadsWithConsequenceNotWithTheBypass(t *testing.T) {
	err := validateTeardownTarget(envFn(map[string]string{"HOME": "/h"}), false)
	if err == nil {
		t.Fatal("bare teardown must refuse")
	}
	msg := err.Error()
	for _, must := range []string{"CANONICAL warden", "com.officraft.ocwarden", "OC_NAMESPACE=", "--canonical"} {
		if !strings.Contains(msg, must) {
			t.Fatalf("refusal must mention %q; got: %s", must, msg)
		}
	}
	iCanon := strings.Index(msg, "--canonical")
	if i := strings.Index(msg, "CANONICAL warden"); i > iCanon {
		t.Errorf("the consequence must precede the bypass flag:\n%s", msg)
	}
	if i := strings.Index(msg, "OC_NAMESPACE="); i > iCanon {
		t.Errorf("the safe namespaced alternative must precede the bypass flag:\n%s", msg)
	}
}
