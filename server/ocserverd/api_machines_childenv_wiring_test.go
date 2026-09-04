// api_machines_childenv_wiring_test.go — pins that bootstrap-here and
// teardown-here ACTUALLY CALL the allowlist, as opposed to merely having one
// available in the package.
//
// THE GAP THIS CLOSES
// -------------------
// api_machines_childenv_test.go tests ocwardenChildEnv as a pure function, and it
// is a good test OF THAT FUNCTION. But the defect T-5047 is about did not live in
// a function — it lived in two assignment statements:
//
//	env := os.Environ()                       // teardown-here, pre-T-5047
//	env := <os.Environ() minus OC_ID>         // bootstrap-here, pre-T-5047
//
// Reverting both call sites to exactly that — the original defect, byte for byte —
// left the ENTIRE server suite green: every childenv test still passed, because
// every one of them called the projection directly instead of going through the
// verb. A projection nobody is proven to call is not a defence, so the tests below
// drive the two verbs and read the env off the SEAM (runOcwarden), never by
// calling ocwardenChildEnv themselves.
//
// The seam is the recorder, not a fake of the thing under test: the env asserted
// on here is the literal []string that would have been handed to exec.Cmd.Env.
package main

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// recordedOcwardenRun is one captured child invocation.
type recordedOcwardenRun struct {
	bin  string
	args []string
	env  []string
}

// withRecordedOcwarden rebinds the runOcwarden seam for one test and returns the
// slice the runs land in. exit is what the fake child reports.
func withRecordedOcwarden(t *testing.T, exit int) *[]recordedOcwardenRun {
	t.Helper()
	runs := []recordedOcwardenRun{}
	prev := runOcwarden
	runOcwarden = func(bin string, args []string, env []string) (int, string, bool) {
		runs = append(runs, recordedOcwardenRun{bin: bin, args: args, env: env})
		return exit, "fake-ocwarden", false
	}
	t.Cleanup(func() { runOcwarden = prev })
	return &runs
}

// pollutedParentEnv sets, on the REAL test process, every variable that used to be
// inherited wholesale — including the two that can retarget or fake an install.
// t.Setenv restores them at the end of the test.
func pollutedParentEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OC_ID", "m-someone-else")
	t.Setenv("OC_NAMESPACE", "stray-instance")
	t.Setenv("WARDEN_INSTALL_DRYRUN", "1")
	t.Setenv("OC_AGENT_BIN", "/tmp/evil-ocagent")
	t.Setenv("OC_WARDEN_TOKFILE", "/tmp/somebody-elses.tok")
	t.Setenv("OC_AGENT_HOME", "/tmp/elsewhere")
	t.Setenv("OC_BASE", "http://attacker.example")
	t.Setenv("OC_TOKEN", "stale-token")
	t.Setenv("HOME", "/Users/fixture-home")
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
}

// assertChildEnvIsProjected is the shared verdict: whatever the parent carried,
// the child env may only contain allowlisted keys plus the keys the SERVER
// deliberately appends after the projection.
func assertChildEnvIsProjected(t *testing.T, verb string, env []string, serverAppended map[string]bool) {
	t.Helper()
	allowed := map[string]bool{}
	for _, k := range ocwardenChildEnvAllowlist {
		allowed[k] = true
	}
	seen := map[string]string{}
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			t.Errorf("%s: malformed child env entry %q", verb, kv)
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if !allowed[k] && !serverAppended[k] {
			// KEY ONLY, NEVER THE VALUE. This function scans the env that would
			// really be handed to the child, and under the very mutant it exists to
			// catch (the handler passing os.Environ() straight through) that slice is
			// the SERVER PROCESS's real environment. An independent review ran that
			// mutant and this message printed live ES_API_KEY_PROD / ES_API_KEY_STAGE
			// / SSH_AUTH_SOCK values into the CI log. A guard against leaking secrets
			// to a child must not leak them to stdout in the process of complaining.
			// The length is enough to tell "present and non-empty" from "empty".
			t.Errorf("%s: %s (value withheld, %d byte(s)) reached the ocwarden child — the verb is not going through ocwardenChildEnv", verb, k, len(v))
		}
		seen[k] = v
	}
	// The deliberate relays must SURVIVE (an allowlist that drops PATH/HOME turns
	// every one-click install into a warden that cannot resolve anything).
	if seen["HOME"] != "/Users/fixture-home" {
		t.Errorf("%s: HOME must be relayed verbatim, got %q", verb, seen["HOME"])
	}
	if seen["PATH"] != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Errorf("%s: PATH must be relayed verbatim, got %q", verb, seen["PATH"])
	}
}

// TestBootstrapHere_ChildEnvGoesThroughTheAllowlist drives runWardenInstallHere —
// the CORE both the cockpit button and first-run onboarding run — with a parent
// process env carrying every historically-inherited landmine.
func TestBootstrapHere_ChildEnvGoesThroughTheAllowlist(t *testing.T) {
	pollutedParentEnv(t)
	runs := withRecordedOcwarden(t, 0)

	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), keys: singleKeyring([]byte("test-secret"))}
	m := Member{ID: "m-here", Kind: KindWarden}
	res, err := s.runWardenInstallHere(m, "/fake/ocwarden", "http://127.0.0.1:7755")
	if err != nil {
		t.Fatalf("runWardenInstallHere: %v", err)
	}
	if !res.OK {
		t.Fatalf("fake child exited 0 but result is not ok: %+v", res)
	}
	if len(*runs) != 1 {
		t.Fatalf("expected exactly one ocwarden invocation, got %d — the verb did not go through the seam", len(*runs))
	}
	run := (*runs)[0]
	if len(run.args) < 1 || run.args[0] != "install" {
		t.Fatalf("expected the install verb, got %v", run.args)
	}
	assertChildEnvIsProjected(t, "bootstrap-here", run.env, map[string]bool{
		"OC_BASE": true, "OC_TOKEN": true, "OC_NAMESPACE": true,
	})

	// The server-computed wiring must WIN over the inherited values of the same
	// name — appended after the projection, so it cannot be shadowed.
	got := childEnvMap(run.env)
	if got["OC_BASE"] != "http://127.0.0.1:7755" {
		t.Errorf("OC_BASE must be the server-computed base, got %q (the inherited attacker value leaked)", got["OC_BASE"])
	}
	if got["OC_TOKEN"] == "stale-token" || got["OC_TOKEN"] == "" {
		t.Errorf("OC_TOKEN must be the freshly minted member token, got %q", got["OC_TOKEN"])
	}
	// A MAIN-instance server (namespace "") must send NO namespace at all, even
	// though the parent process is carrying OC_NAMESPACE=stray-instance. This is
	// the live-warden case: inheriting it installs into another instance.
	if v, ok := got["OC_NAMESPACE"]; ok {
		t.Errorf("a main-instance server must not send OC_NAMESPACE, got %q", v)
	}
	if v, ok := got["OC_ID"]; ok {
		t.Errorf("OC_ID must never reach the child (identity rides in the token sub), got %q", v)
	}
	if v, ok := got["WARDEN_INSTALL_DRYRUN"]; ok {
		t.Errorf("WARDEN_INSTALL_DRYRUN=%q reached the child — the server would report a successful install of a warden that does not exist", v)
	}
}

// TestBootstrapHere_NamespacedServerSendsItsOwnNamespace is the SENTINEL twin: the
// allowlist must not break the legitimate namespace propagation, and the value must
// be the SERVER's, never the inherited one.
func TestBootstrapHere_NamespacedServerSendsItsOwnNamespace(t *testing.T) {
	pollutedParentEnv(t)
	runs := withRecordedOcwarden(t, 0)

	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), keys: singleKeyring([]byte("test-secret")), namespace: "lab"}
	if _, err := s.runWardenInstallHere(Member{ID: "m-here", Kind: KindWarden}, "/fake/ocwarden", "http://127.0.0.1:7756"); err != nil {
		t.Fatalf("runWardenInstallHere: %v", err)
	}
	got := childEnvMap((*runs)[0].env)
	if got["OC_NAMESPACE"] != "lab" {
		t.Errorf("namespaced server must send its OWN namespace, got %q (inherited was 'stray-instance')", got["OC_NAMESPACE"])
	}
	// Exactly one OC_NAMESPACE entry: a second, inherited one earlier in the slice
	// is invisible to a map but IS visible to some env parsers.
	n := 0
	for _, kv := range (*runs)[0].env {
		if strings.HasPrefix(kv, "OC_NAMESPACE=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("child env carries %d OC_NAMESPACE entries, want exactly 1 (a shadowed duplicate is a retarget waiting to happen)", n)
	}
}

// TestTeardownHere_ChildEnvGoesThroughTheAllowlist is the same proof for the verb
// that BOOTS OUT a launchd job — the one that cost this fleet three live-warden
// unloads. A main-instance server carrying a stray OC_NAMESPACE must tear down ITS
// OWN warden and nothing else.
func TestTeardownHere_ChildEnvGoesThroughTheAllowlist(t *testing.T) {
	pollutedParentEnv(t)
	runs := withRecordedOcwarden(t, 0)

	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), keys: singleKeyring([]byte("test-secret"))}
	exit, _, timedOut := s.runWardenTeardownHere("/fake/ocwarden")
	if exit != 0 || timedOut {
		t.Fatalf("fake child: exit=%d timedOut=%v", exit, timedOut)
	}
	if len(*runs) != 1 {
		t.Fatalf("expected exactly one ocwarden invocation, got %d", len(*runs))
	}
	run := (*runs)[0]
	if len(run.args) < 1 || run.args[0] != "teardown" {
		t.Fatalf("expected the teardown verb, got %v", run.args)
	}
	assertChildEnvIsProjected(t, "teardown-here", run.env, map[string]bool{"OC_NAMESPACE": true})

	got := childEnvMap(run.env)
	if v, ok := got["OC_NAMESPACE"]; ok {
		t.Errorf("a main-instance server tore down with OC_NAMESPACE=%q — that is a DIFFERENT instance's live warden", v)
	}
	if v, ok := got["OC_WARDEN_TOKFILE"]; ok {
		t.Errorf("OC_WARDEN_TOKFILE=%q reached the teardown child — it decides which instance's token is revoked", v)
	}
}

// TestTeardownHere_NamespacedServerTearsDownItsOwn — sentinel twin for teardown.
func TestTeardownHere_NamespacedServerTearsDownItsOwn(t *testing.T) {
	pollutedParentEnv(t)
	runs := withRecordedOcwarden(t, 0)

	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), namespace: "lab"}
	s.runWardenTeardownHere("/fake/ocwarden")
	if got := childEnvMap((*runs)[0].env)["OC_NAMESPACE"]; got != "lab" {
		t.Errorf("namespaced server must tear down its OWN instance, got OC_NAMESPACE=%q", got)
	}
}

// TestOcwardenChildEnv_IsTheOnlyEnvSourceForBothVerbs is the DRIFT GUARD: the two
// verbs above are the only places in this package that build an ocwarden child env,
// and each must build it from the projection. This is a source scan, so it fails
// WITHOUT executing anything — the counterfactual (revert either site to
// `os.Environ()`) is caught before a child is ever spawned.
//
// ⚠️ IT SCANS CODE ONLY, AND THAT IS THE WHOLE DIFFERENCE
// The first version of this guard grepped the RAW file, comments included, and was
// therefore an always-true assertion for the mutant that matters most: independent
// review reverted the call site to `append([]string{}, os.Environ()...)` and left
// the string `ocwardenChildEnv(os.Environ())` in a nearby COMMENT — the positive
// check matched the comment, the negative check (`:= os.Environ()`) did not match
// `append(...)`, and this test PASSED while the server shipped its whole
// environment to a process that boots out a launchd job. The claim in the sentence
// above was simply false for that variant.
//
// So the scan runs over codeOnly(): the file re-printed from its AST with every
// comment dropped. bin/tests/namespace-mirror-guard.sh already learned this lesson
// (its code_only helper); the Go side had not caught up.
func TestOcwardenChildEnv_IsTheOnlyEnvSourceForBothVerbs(t *testing.T) {
	body, err := codeOnly("api_machines.go")
	if err != nil {
		t.Fatalf("read api_machines.go: %v", err)
	}
	for _, fn := range []string{"func (s *apiServer) runWardenInstallHere", "func (s *apiServer) runWardenTeardownHere"} {
		start := strings.Index(body, fn)
		if start < 0 {
			t.Fatalf("%s not found — the verb was renamed and this guard silently stopped guarding it", fn)
		}
		// The function body up to the next top-level func declaration.
		rest := body[start:]
		if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
			rest = rest[:end+1]
		}
		if !strings.Contains(rest, "ocwardenChildEnv(os.Environ())") {
			t.Errorf("%s does not build its child env with ocwardenChildEnv(os.Environ()) — it is shipping the server's whole environment to a process that installs or boots out a launchd job", fn)
		}
		if strings.Contains(rest, ":= os.Environ()") {
			t.Errorf("%s assigns os.Environ() directly — that is the pre-T-5047 defect", fn)
		}
		// Any OTHER route from os.Environ() into the child env is the same defect
		// wearing a different syntax (`append([]string{}, os.Environ()...)` is the
		// literal mutant that defeated the comment-blind version of this guard). The
		// projection call is the only legitimate mention, so subtract it and there
		// must be nothing left.
		if n := strings.Count(rest, "os.Environ()") - strings.Count(rest, "ocwardenChildEnv(os.Environ())"); n > 0 {
			t.Errorf("%s mentions os.Environ() %d time(s) outside ocwardenChildEnv(...) — every route from the server's own environment into the child must go through the allowlist projection, whatever the syntax", fn, n)
		}
	}
}

// codeOnly returns path's Go source with EVERY comment removed, by re-printing it
// from an AST parsed without comments. Source scans in this package assert on code,
// and a scan that also matched its own prose is not an assertion — see the warning
// on TestOcwardenChildEnv_IsTheOnlyEnvSourceForBothVerbs for the mutant that
// exploited exactly that. Mirrors code_only() in bin/tests/namespace-mirror-guard.sh.
func codeOnly(path string) (string, error) {
	fset := token.NewFileSet()
	// mode 0: comments are not attached, so the printer cannot emit them.
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, f); err != nil {
		return "", err
	}
	return buf.String(), nil
}
