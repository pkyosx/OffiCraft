package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T-426d follow-up — interactive-shell env inheritance.
//
// The canary values below are the load-bearing part of this file. Every one is
// a value that MUST reach the agent and MUST NEVER reach a log line, so a
// single constant serves both the "it got injected" assertion and the "it did
// not leak" assertion. If a future change starts formatting values into
// warnings, TestInteractiveEnv_LogsNeverContainValues reddens.
//
// They are deliberately not secret-SHAPED (no sk-/ghp-/AKIA prefixes): the repo
// hygiene step runs gitleaks over the tree, and a realistic-looking fixture
// would redden CI for the wrong reason.
// ---------------------------------------------------------------------------

const (
	canaryJiraToken   = "CANARY-JIRA-VALUE-MUST-NOT-BE-LOGGED"
	canaryGeminiKey   = "CANARY-GEMINI-VALUE-MUST-NOT-BE-LOGGED"
	canaryFigmaToken  = "CANARY-FIGMA-VALUE-MUST-NOT-BE-LOGGED"
	canaryOverrideVal = "CANARY-OVERRIDE-VALUE-MUST-NOT-BE-LOGGED"
)

// nulEnv builds a NUL-delimited env dump, the exact shape `env -0` emits
// (trailing NUL included — the real tool terminates the last record too).
func nulEnv(kvs ...string) string {
	var b strings.Builder
	for _, kv := range kvs {
		b.WriteString(kv)
		b.WriteByte(0)
	}
	return b.String()
}

// fxInteractiveDump is a stand-in for a real capture: the credentials the
// ticket exists to deliver, the PATH that `export -p` would have silently
// dropped, and the session-local names that must be filtered out.
func fxInteractiveDump() string {
	return nulEnv(
		"PATH=/Users/o/.local/bin:/Users/o/.asdf/shims:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/bin:/bin",
		"JIRA_TOKEN="+canaryJiraToken,
		"GEMINI_API_KEY="+canaryGeminiKey,
		"FIGMA_TOKEN="+canaryFigmaToken,
		"HOMEBREW_PREFIX=/opt/homebrew",
		"PWD=/Users/o/somewhere-else",
		"OLDPWD=/Users/o/before-that",
		"SHLVL=1",
		"TMUX=/private/tmp/tmux-501/default,9999,0",
		"TERM=xterm-256color",
		"OC_TOKEN=CANARY-IDENTITY-HIJACK-ATTEMPT",
	)
}

// logSpy captures every diagnostic line start() emits, so a test can assert on
// what the owner would actually find in ocwarden.err.log.
type logSpy struct{ lines []string }

func (l *logSpy) logf(format string, a ...any) {
	l.lines = append(l.lines, sprintfLike(format, a...))
}

func (l *logSpy) all() string { return strings.Join(l.lines, "\n") }

// sprintfLike keeps the spy honest: it formats exactly as the production Logf
// seam does, so the assertion sees the FINAL rendered text, not the format
// string. A leak that happens via a %v argument would be invisible otherwise.
func sprintfLike(format string, a ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, a...))
}

// ---------------------------------------------------------------------------
// (1) parser
// ---------------------------------------------------------------------------

func TestParseNulEnv_KeepsCredentialsAndPath(t *testing.T) {
	spy := &logSpy{}
	pairs := parseNulEnv(fxInteractiveDump(), spy.logf)

	got := map[string]string{}
	for _, p := range pairs {
		got[p.Key] = p.Value
	}
	// The whole point of the ticket: credentials and the full PATH arrive.
	if got["JIRA_TOKEN"] != canaryJiraToken {
		t.Errorf("JIRA_TOKEN not inherited: got %d chars, want the canary", len(got["JIRA_TOKEN"]))
	}
	if got["GEMINI_API_KEY"] != canaryGeminiKey {
		t.Error("GEMINI_API_KEY not inherited")
	}
	if got["HOMEBREW_PREFIX"] != "/opt/homebrew" {
		t.Errorf("HOMEBREW_PREFIX = %q, want /opt/homebrew", got["HOMEBREW_PREFIX"])
	}
	// PATH is the regression guard for the `export -p` trap: zsh renders it as
	// `export -T PATH path=( ... )`, which a KEY=value parser drops on the floor.
	// `env -0` gives it as a scalar and it must survive intact.
	for _, want := range []string{"/Users/o/.local/bin", "/Users/o/.asdf/shims", "/opt/homebrew/sbin"} {
		if !strings.Contains(got["PATH"], want) {
			t.Errorf("PATH lost %q — got %q", want, got["PATH"])
		}
	}
}

func TestParseNulEnv_DropsSessionLocalAndReservedNames(t *testing.T) {
	pairs := parseNulEnv(fxInteractiveDump(), nil)
	for _, p := range pairs {
		switch p.Key {
		case "PWD", "OLDPWD", "SHLVL", "TMUX", "TERM":
			// PWD in particular: the launch line cds into the workdir and THEN
			// sources this, so an inherited PWD would make $PWD lie for the
			// agent's entire lifetime.
			t.Errorf("session-local %s must not be inherited (it describes the CAPTURING shell)", p.Key)
		case "OC_TOKEN":
			// The agent's identity comes from the warden, never from a stray
			// export in the owner's .zshrc.
			t.Error("OC_TOKEN must be refused — OC_* is the warden's identity namespace")
		}
	}
}

func TestParseNulEnv_ValuesMayContainAnyByte(t *testing.T) {
	// `env -0` records are NUL-delimited, so `=`, newlines, quotes and spaces
	// are all ordinary content. This is exactly what `export -p` could not
	// promise, and credentials in the wild contain all of them.
	raw := nulEnv(
		"WITH_EQUALS=a=b=c",
		"WITH_NEWLINE=line1\nline2\nline3",
		"WITH_QUOTES=he said \"hi\" and 'bye'",
		"WITH_SPACES=  padded value  ",
		"EMPTY=",
	)
	got := map[string]string{}
	for _, p := range parseNulEnv(raw, nil) {
		got[p.Key] = p.Value
	}
	for k, want := range map[string]string{
		"WITH_EQUALS":  "a=b=c",
		"WITH_NEWLINE": "line1\nline2\nline3",
		"WITH_QUOTES":  "he said \"hi\" and 'bye'",
		"WITH_SPACES":  "  padded value  ",
		"EMPTY":        "",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}

func TestParseNulEnv_MalformedRecordsSkippedByPositionNotContent(t *testing.T) {
	// A record that fails the KEY=value shape may be a FRAGMENT OF A VALUE, so
	// the warning must identify it by position and never echo it.
	raw := nulEnv(
		"GOOD_ONE=kept",
		"this-record-has-no-equals-"+canaryJiraToken,
		"9INVALID=name starts with a digit",
		"=leading-equals-no-name",
		"ALSO_GOOD=kept-too",
	)
	spy := &logSpy{}
	pairs := parseNulEnv(raw, spy.logf)

	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2 (the malformed records must be skipped, not fatal)", len(pairs))
	}
	log := spy.all()
	if !strings.Contains(log, "malformed") {
		t.Errorf("malformed records were skipped silently; log = %q", log)
	}
	if strings.Contains(log, canaryJiraToken) {
		t.Error("LEAK: the malformed record's content reached the log")
	}
	if strings.Contains(log, "leading-equals-no-name") {
		t.Error("LEAK: malformed record content reached the log")
	}
}

func TestParseNulEnv_DuplicateLastWins(t *testing.T) {
	pairs := parseNulEnv(nulEnv("K=first", "K=second"), nil)
	if len(pairs) != 1 || pairs[0].Value != "second" {
		t.Errorf("got %+v, want a single K=second", pairs)
	}
}

func TestParseNulEnv_EmptyInputIsNotAPanic(t *testing.T) {
	if got := parseNulEnv("", nil); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
	if got := parseNulEnv("\x00\x00", nil); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

// ---------------------------------------------------------------------------
// (2) merge / override precedence
// ---------------------------------------------------------------------------

func TestMergeAgentEnv_FileOverridesInteractive(t *testing.T) {
	base := []agentEnvPair{
		{Key: "JIRA_TOKEN", Value: canaryJiraToken},
		{Key: "PATH", Value: "/from/interactive"},
		{Key: "KEEP", Value: "untouched"},
	}
	override := []agentEnvPair{
		{Key: "JIRA_TOKEN", Value: canaryOverrideVal}, // pin a different value
		{Key: "ONLY_IN_FILE", Value: "file-only"},     // supply one the shell lacks
	}
	got := map[string]string{}
	var order []string
	for _, p := range mergeAgentEnv(base, override) {
		got[p.Key] = p.Value
		order = append(order, p.Key)
	}
	if got["JIRA_TOKEN"] != canaryOverrideVal {
		t.Error("the env file must WIN over the interactive shell — that is what makes it the override layer")
	}
	if got["PATH"] != "/from/interactive" || got["KEEP"] != "untouched" {
		t.Error("names the file did not mention must survive from the base layer untouched")
	}
	if got["ONLY_IN_FILE"] != "file-only" {
		t.Error("the file must be able to supply a name the interactive shell does not have")
	}
	// Deterministic order: an overridden name keeps its BASE position, file-only
	// names append. A churning order would make every spawn's render differ.
	want := []string{"JIRA_TOKEN", "PATH", "KEEP", "ONLY_IN_FILE"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestMergeAgentEnv_EmptyOverrideIsANoOp(t *testing.T) {
	// The state every machine is in until the owner writes an env file.
	base := []agentEnvPair{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}}
	got := mergeAgentEnv(base, nil)
	if len(got) != 2 || got[0] != base[0] || got[1] != base[1] {
		t.Errorf("got %+v, want the base layer verbatim", got)
	}
}

func TestOverriddenKeyNames(t *testing.T) {
	base := []agentEnvPair{{Key: "A"}, {Key: "B"}, {Key: "C"}}
	override := []agentEnvPair{{Key: "C"}, {Key: "A"}, {Key: "Z"}}
	got := overriddenKeyNames(base, override)
	if strings.Join(got, ",") != "A,C" {
		t.Errorf("got %v, want [A C] (sorted, intersection only)", got)
	}
}

// ---------------------------------------------------------------------------
// (3) the REAL exec path — failure modes driven through real stub shells.
//
// A Go closure returning an error would prove only that the wrapper handles an
// error it was handed. These drive captureInteractiveEnv itself — real
// exec.CommandContext, real process, real exit status — because a test double
// that is more forgiving than the real tool is how an entire error-handling
// path gets deleted from coverage while CI stays green.
// ---------------------------------------------------------------------------

// stubShell writes an executable script that stands in for /bin/zsh. It is
// invoked exactly as the real shell is (`<shell> -i -c '<dumper>'`) and ignores
// its arguments, so it can model any behaviour a broken rc file produces.
func stubShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub-shell")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub shell: %v", err)
	}
	return path
}

func TestCaptureInteractiveEnv_Success(t *testing.T) {
	sh := stubShell(t, `printf 'JIRA_TOKEN=`+canaryJiraToken+`\0PATH=/a:/b\0'`)
	raw, err := captureInteractiveEnv(sh, 5*time.Second)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	pairs := parseNulEnv(raw, nil)
	if len(pairs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(pairs))
	}
}

func TestCaptureInteractiveEnv_FailureModes(t *testing.T) {
	cases := []struct {
		name  string
		shell string
		body  string
	}{
		{name: "nonzero exit", body: `exit 3`},
		{name: "nonzero exit after partial output", body: `printf 'A=1\0'; exit 1`},
		{name: "shell binary does not exist", shell: filepath.Join(t.TempDir(), "no-such-shell")},
		{name: "not executable", shell: func() string {
			p := filepath.Join(t.TempDir(), "not-exec")
			if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shell := c.shell
			if shell == "" {
				shell = stubShell(t, c.body)
			}
			raw, err := captureInteractiveEnv(shell, 5*time.Second)
			if err == nil {
				t.Fatalf("want an error, got raw output of %d bytes", len(raw))
			}
		})
	}
}

// TestCaptureInteractiveEnv_TimeoutIsEnforcedInGo is the guard on the specific
// trap that nearly killed this approach during the feasibility probe: the
// timeout must come from Go's context, NOT from a `timeout` command. macOS
// ships no `timeout` binary (it is `gtimeout`), so an argv-level timeout would
// fail with rc=127 on this very host and be misread as "the shell cannot be
// captured".
func TestCaptureInteractiveEnv_TimeoutIsEnforcedInGo(t *testing.T) {
	// Each body is a different way for an rc file to hang. The ORPHAN cases are
	// the ones that matter: they leave a GRANDCHILD holding the stdout pipe
	// after the shell itself is killed. Killing only the direct child leaves
	// cmd.Wait blocked on EOF for the grandchild's whole lifetime — the deadline
	// fires, the error is correct, and the spawn stalls anyway for 30s.
	//
	// That is why this test asserts ELAPSED TIME and not merely "an error came
	// back". Asserting only the error passes against the broken implementation:
	// it was the wall-clock assertion that caught it.
	bodies := map[string]string{
		"shell itself blocks":              "sleep 30",
		"grandchild holds the pipe":        "sh -c 'sleep 30' ; sleep 30",
		"backgrounded grandchild survives": "sleep 30 & sleep 30",
		"subshell keeps the pipe open":     "( sleep 30 ) ; sleep 30",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			sh := stubShell(t, body)
			start := time.Now()
			_, err := captureInteractiveEnv(sh, 300*time.Millisecond)
			elapsed := time.Since(start)
			if err == nil {
				t.Fatal("a hung shell must produce an error, not a hung spawn")
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Errorf("error = %v, want it to name the timeout", err)
			}
			// Generous but decisive: the deadline is 300ms and WaitDelay is 2s,
			// so anything near the 30s sleep means the tree outlived the timeout.
			if elapsed > 8*time.Second {
				t.Errorf("took %s — the deadline did not bound the whole process TREE, "+
					"only the direct child; a grandchild holding the stdout pipe stalls the spawn", elapsed)
			}
		})
	}
}

func TestCaptureInteractiveEnv_EmptyOutputParsesToNothing(t *testing.T) {
	// Exit 0 having printed nothing usable. The capture itself succeeds; it is
	// the wrapper's job to notice the result is useless and fall back loudly.
	sh := stubShell(t, `exit 0`)
	raw, err := captureInteractiveEnv(sh, 5*time.Second)
	if err != nil {
		t.Fatalf("exit 0 must not be an error: %v", err)
	}
	if got := parseNulEnv(raw, nil); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestCaptureInteractiveEnv_GarbageOutputIsSurvivable(t *testing.T) {
	sh := stubShell(t, `printf 'this is not an env dump at all\nnor is this'`)
	raw, err := captureInteractiveEnv(sh, 5*time.Second)
	if err != nil {
		t.Fatalf("garbage on stdout with exit 0 is not a capture error: %v", err)
	}
	spy := &logSpy{}
	if got := parseNulEnv(raw, spy.logf); len(got) != 0 {
		t.Errorf("got %+v, want nothing usable", got)
	}
	if !strings.Contains(spy.all(), "malformed") {
		t.Error("garbage must be reported, not silently swallowed")
	}
}

// TestCaptureInteractiveEnv_RealZsh runs the ACTUAL production command against
// the ACTUAL /bin/zsh. Every other test here uses a stub, and a stub can only
// ever confirm assumptions about zsh rather than test them — this is the one
// that would catch `env -0` not existing, or `zsh -i` refusing to run without a
// tty. Skipped where /bin/zsh is absent so CI stays portable.
func TestCaptureInteractiveEnv_RealZsh(t *testing.T) {
	if _, err := os.Stat(interactiveEnvShell); err != nil {
		t.Skipf("no %s on this host", interactiveEnvShell)
	}
	raw, err := captureInteractiveEnv(interactiveEnvShell, interactiveEnvTimeout)
	if err != nil {
		t.Fatalf("real zsh capture failed: %v", err)
	}
	pairs := parseNulEnv(raw, nil)
	if len(pairs) == 0 {
		t.Fatal("real zsh produced no usable variables")
	}
	// PATH as a SCALAR is the whole reason this uses `env -0`: `export -p`
	// renders it as `export -T PATH path=( ... )` and a KEY=value parser loses
	// it. If PATH ever stops arriving here, the ticket's primary symptom is back.
	var sawPath bool
	for _, p := range pairs {
		if p.Key == "PATH" {
			sawPath = true
			if !strings.Contains(p.Value, ":") && !strings.Contains(p.Value, "/") {
				t.Errorf("PATH does not look like a path list (len %d)", len(p.Value))
			}
			if strings.HasPrefix(p.Value, "(") || strings.Contains(p.Value, "path=(") {
				t.Error("PATH arrived in zsh ARRAY syntax — the capture command regressed to export -p")
			}
		}
	}
	if !sawPath {
		t.Error("PATH missing from a real zsh capture — the ticket's primary symptom")
	}
}

// ---------------------------------------------------------------------------
// (4) the spawn path: injection, fail-safe, precedence, and the leak guard
// ---------------------------------------------------------------------------

// startWithEnv runs a full spawn with the given capture seam and env file, and
// returns the rendered env file's content, the launch command, the log, and the
// outcome.
func startWithEnv(t *testing.T, capture func() (string, error), envFileBody string) (rendered, launch, log string, out SpawnOutcome) {
	t.Helper()
	hasKey := "tmux -L " + fxSocket + " has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	deps.CaptureEnv = capture

	if envFileBody != "" {
		p := filepath.Join(t.TempDir(), "env")
		if err := os.WriteFile(p, []byte(envFileBody), 0o600); err != nil {
			t.Fatal(err)
		}
		deps.EnvFile = p
	}
	spy := &logSpy{}
	deps.Logf = spy.logf

	out = deps.start(StartParams{
		MemberID:       "alice",
		PersonaContext: "PERSONA",
		MemberToken:    fxToken,
		Role:           "assistant",
		Model:          fxModel,
		SessionName:    "member-alice",
	})
	for path, content := range files {
		if strings.HasSuffix(path, agentEnvRenderedName) {
			rendered = content
		}
	}
	for _, c := range run.calls {
		if len(c) > 1 && c[1] == "-L" && strings.Contains(strings.Join(c, " "), "new-session") {
			launch = strings.Join(c, " ")
		}
	}
	return rendered, launch, spy.all(), out
}

// ① injection succeeds → the target variables reach the agent.
func TestStart_InteractiveEnvIsInjected(t *testing.T) {
	rendered, launch, _, out := startWithEnv(t,
		func() (string, error) { return fxInteractiveDump(), nil }, "")

	if !out.OK {
		t.Fatalf("spawn failed: %s", out.Reason)
	}
	for _, want := range []string{"JIRA_TOKEN", "GEMINI_API_KEY", "FIGMA_TOKEN", "HOMEBREW_PREFIX", "PATH"} {
		if !strings.Contains(rendered, "export "+want+"=") {
			t.Errorf("%s did not reach the agent's rendered env file", want)
		}
	}
	if !strings.Contains(rendered, canaryJiraToken) {
		t.Error("the credential VALUE must be in the rendered 0600 file — that is the deliverable")
	}
	// Session-local and reserved names must not have made it through.
	for _, bad := range []string{"export PWD=", "export OLDPWD=", "export TMUX=", "export OC_TOKEN="} {
		if strings.Contains(rendered, bad) {
			t.Errorf("rendered file contains %q, which describes the WARDEN, not the agent", bad)
		}
	}
	// And the launch line must actually source it.
	if !strings.Contains(launch, agentEnvRenderedName) {
		t.Errorf("launch line does not source the rendered env file: %s", launch)
	}
}

// ② capture failure → fall back to the minimal environment, spawn still OK.
func TestStart_CaptureFailureFallsBackAndStillSpawns(t *testing.T) {
	cases := []struct {
		name    string
		capture func() (string, error)
	}{
		{"seam not wired (kill switch)", nil},
		{"nonzero exit", func() (string, error) {
			return captureInteractiveEnv(stubShell(t, "exit 7"), 5*time.Second)
		}},
		{"empty output", func() (string, error) {
			return captureInteractiveEnv(stubShell(t, "exit 0"), 5*time.Second)
		}},
		{"garbage output", func() (string, error) {
			return captureInteractiveEnv(stubShell(t, `printf 'not an env dump'`), 5*time.Second)
		}},
		{"hung shell (context timeout)", func() (string, error) {
			return captureInteractiveEnv(stubShell(t, "sleep 30"), 200*time.Millisecond)
		}},
		{"missing shell", func() (string, error) {
			return captureInteractiveEnv("/definitely/not/a/shell", 5*time.Second)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// No panic, no failed spawn — that is the entire contract. warden
			// starts EVERY agent; if this path can break a spawn, one bad rc file
			// takes the whole studio offline.
			rendered, launch, log, out := startWithEnv(t, c.capture, "")
			if !out.OK {
				t.Fatalf("FAIL-SAFE VIOLATED: spawn refused because the env capture failed: %s", out.Reason)
			}
			if rendered != "" {
				t.Errorf("nothing should have been rendered; got %q", rendered)
			}
			// Byte-identical to the pre-feature launch line: no source hop at all.
			if strings.Contains(launch, agentEnvRenderedName) {
				t.Errorf("launch line sources a file that was never written: %s", launch)
			}
			if c.capture != nil && !strings.Contains(log, "minimal environment") {
				t.Errorf("the fallback must be announced in ocwarden.err.log; log = %q", log)
			}
		})
	}
}

// ③ override precedence, end to end through a real env file.
func TestStart_EnvFileOverridesInteractiveShell(t *testing.T) {
	rendered, _, log, out := startWithEnv(t,
		func() (string, error) { return fxInteractiveDump(), nil },
		"JIRA_TOKEN="+canaryOverrideVal+"\nONLY_IN_FILE=file-only\n")

	if !out.OK {
		t.Fatalf("spawn failed: %s", out.Reason)
	}
	if !strings.Contains(rendered, canaryOverrideVal) {
		t.Error("the env file's value must win — it is the override layer")
	}
	if strings.Contains(rendered, canaryJiraToken) {
		t.Error("the interactive shell's value survived an explicit override")
	}
	if !strings.Contains(rendered, "ONLY_IN_FILE") {
		t.Error("a file-only variable must still be delivered")
	}
	// Untouched names keep coming from the interactive layer.
	if !strings.Contains(rendered, canaryGeminiKey) {
		t.Error("names the file did not mention must survive from the interactive layer")
	}
	// The override is announced by NAME so the owner can see the precedence
	// without either value appearing.
	if !strings.Contains(log, "overrides the interactive shell for: JIRA_TOKEN") {
		t.Errorf("override not reported by name; log = %q", log)
	}
}

// ④ THE LEAK GUARD. Every diagnostic warden writes is asserted, character by
// character, to contain no credential VALUE — only names.
//
// This runs the full spawn across the happy path, the override path, and every
// failure path, because a leak is most likely to appear in an error message
// (where a `%v` of some captured output is the natural thing to write).
func TestInteractiveEnv_LogsNeverContainValues(t *testing.T) {
	secrets := []string{canaryJiraToken, canaryGeminiKey, canaryFigmaToken, canaryOverrideVal}

	scenarios := []struct {
		name    string
		capture func() (string, error)
		envFile string
	}{
		{"happy path", func() (string, error) { return fxInteractiveDump(), nil }, ""},
		{"with override", func() (string, error) { return fxInteractiveDump(), nil },
			"JIRA_TOKEN=" + canaryOverrideVal + "\n"},
		{"malformed records carrying a secret", func() (string, error) {
			return nulEnv("GOOD=1", "no-equals-here-"+canaryJiraToken, "9BAD="+canaryGeminiKey), nil
		}, ""},
		{"capture error", func() (string, error) {
			return captureInteractiveEnv(stubShell(t, "exit 9"), 5*time.Second)
		}, ""},
		{"shell prints a secret to stderr then fails", func() (string, error) {
			return captureInteractiveEnv(
				stubShell(t, `echo "`+canaryFigmaToken+`" >&2; exit 4`), 5*time.Second)
		}, ""},
		{"env file with a wide mode warning", func() (string, error) { return fxInteractiveDump(), nil },
			"GEMINI_API_KEY=" + canaryGeminiKey + "\nmalformed line without equals\n"},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			rendered, launch, log, out := startWithEnv(t, s.capture, s.envFile)
			if !out.OK {
				t.Fatalf("spawn failed: %s", out.Reason)
			}
			for _, secret := range secrets {
				if strings.Contains(log, secret) {
					t.Errorf("CREDENTIAL LEAK into the warden log (%s):\n%s", s.name, redactForFailure(log, secrets))
				}
			}
			// The launch line is visible machine-wide via `ps`. Values ride the
			// 0600 rendered file and must never be on the argv.
			for _, secret := range secrets {
				if strings.Contains(launch, secret) {
					t.Errorf("CREDENTIAL LEAK into the tmux argv, which `ps` shows machine-wide (%s)", s.name)
				}
			}
			// Sanity: the test is only meaningful if a value was actually in play.
			// Without this, a bug that silently delivered NOTHING would make the
			// leak assertion pass vacuously.
			if s.name == "happy path" && !strings.Contains(rendered, canaryJiraToken) {
				t.Fatal("vacuous test: no credential was delivered, so 'no leak' proves nothing")
			}
		})
	}
}

// redactForFailure keeps even the FAILURE message from printing the secret it
// caught — a leak test whose failure output leaks is not much of a leak test.
func redactForFailure(s string, secrets []string) string {
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec, "<REDACTED>")
	}
	return s
}

// The log must still be USEFUL: names present, values absent. A "log nothing"
// implementation would pass the leak guard and be useless in an incident.
func TestInteractiveEnv_LogNamesTheVariablesItDelivered(t *testing.T) {
	_, _, log, out := startWithEnv(t,
		func() (string, error) { return fxInteractiveDump(), nil }, "")
	if !out.OK {
		t.Fatalf("spawn failed: %s", out.Reason)
	}
	for _, name := range []string{"JIRA_TOKEN", "GEMINI_API_KEY", "FIGMA_TOKEN", "PATH"} {
		if !strings.Contains(log, name) {
			t.Errorf("%s is not named in the log — the owner cannot tell what the agent got", name)
		}
	}
	if !strings.Contains(log, "OC_TOKEN") {
		t.Error("the refused OC_* name should be reported, so a stray .zshrc export is visible")
	}
}

// ---------------------------------------------------------------------------
// (5) the kill switch
// ---------------------------------------------------------------------------

func TestDefaultCaptureEnv_KillSwitch(t *testing.T) {
	for _, off := range []string{"0", "false", "no", "off", "OFF", " 0 "} {
		env := func(k string) string {
			if k == "OC_AGENT_ENV_INHERIT" {
				return off
			}
			return ""
		}
		if defaultCaptureEnv(env) != nil {
			t.Errorf("OC_AGENT_ENV_INHERIT=%q must disable inheritance entirely", off)
		}
	}
	// Unset, empty, or anything else ⇒ ON (inheritance is the new default).
	for _, on := range []string{"", "1", "true", "yes", "anything"} {
		env := func(k string) string {
			if k == "OC_AGENT_ENV_INHERIT" {
				return on
			}
			return ""
		}
		if defaultCaptureEnv(env) == nil {
			t.Errorf("OC_AGENT_ENV_INHERIT=%q must leave inheritance ON", on)
		}
	}
}

func TestDefaultCaptureEnv_ShellOverrideIsHonoured(t *testing.T) {
	sh := stubShell(t, `printf 'FROM_STUB=yes\0'`)
	env := func(k string) string {
		if k == "OC_AGENT_ENV_SHELL" {
			return sh
		}
		return ""
	}
	capture := defaultCaptureEnv(env)
	if capture == nil {
		t.Fatal("capture seam must be wired")
	}
	raw, err := capture()
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	pairs := parseNulEnv(raw, nil)
	if len(pairs) != 1 || pairs[0].Key != "FROM_STUB" {
		t.Errorf("got %+v, want the stub shell's output", pairs)
	}
}

// ---------------------------------------------------------------------------
// (6) full round trip through a REAL shell
// ---------------------------------------------------------------------------

// TestInteractiveEnv_RoundTripThroughRealShell closes the loop end to end:
// capture -> parse -> merge -> render -> SOURCE IT IN A REAL SHELL -> dump
// again, and assert the values come back byte-identical.
//
// Every other test in this file stops at the rendered text. That leaves the
// last hop — the shell actually parsing what renderAgentEnvFile wrote —
// untested, and it is the hop where quoting bugs live: a value containing a
// newline, a single quote, or a `$` is exactly where a naive quoter silently
// hands the agent something different from what the owner has.
func TestInteractiveEnv_RoundTripThroughRealShell(t *testing.T) {
	want := map[string]string{
		"PLAIN":        "simple",
		"WITH_SPACES":  "a b c",
		"WITH_SQUOTE":  "it's got one",
		"WITH_DQUOTE":  `he said "hi"`,
		"WITH_DOLLAR":  "$HOME and ${NOT_EXPANDED} and `date`",
		"WITH_NEWLINE": "line1\nline2",
		"WITH_EQUALS":  "a=b=c",
		"WITH_BACKSL":  `back\slash`,
		"EMPTY":        "",
		"PATH_LIKE":    "/a/b:/c/d:/e/f",
	}
	var dump []string
	for k, v := range want {
		dump = append(dump, k+"="+v)
	}
	pairs := parseNulEnv(nulEnv(dump...), nil)
	if len(pairs) != len(want) {
		t.Fatalf("parsed %d pairs, want %d", len(pairs), len(want))
	}

	rendered := filepath.Join(t.TempDir(), agentEnvRenderedName)
	if err := os.WriteFile(rendered, []byte(renderAgentEnvFile(pairs)), 0o600); err != nil {
		t.Fatal(err)
	}
	// /bin/sh, not zsh: the agent's launch line runs under whatever tmux gives
	// it, so the rendered file must be plain POSIX-sourceable.
	got, err := captureInteractiveEnv(
		stubShell(t, ". "+rendered+"\nexec /usr/bin/env -0"), 10*time.Second)
	if err != nil {
		t.Fatalf("sourcing the rendered file failed: %v", err)
	}
	back := map[string]string{}
	for _, p := range parseNulEnv(got, nil) {
		back[p.Key] = p.Value
	}
	for k, v := range want {
		if back[k] != v {
			t.Errorf("%s round-tripped as %q, want %q", k, back[k], v)
		}
	}
}
