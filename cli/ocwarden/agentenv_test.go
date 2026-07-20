package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T-426d fixture value. DELIBERATELY FAKE — this suite never reads, sources, or
// prints the owner's real ~/.zshrc or any real credential. Everything below runs
// against files this test itself creates in t.TempDir().
const fxEnvValue = "T426D_FIXTURE_VALUE"

// writeEnvFile drops an env file at mode and returns its path.
func writeEnvFile(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile is umask-filtered; force the exact bits we are testing.
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// capturingLogf records every diagnostic line so a test can assert the REASON a
// branch was taken — not merely that it returned nil. "Failed by accident" and
// "correctly declined" both produce nil pairs; only the reason separates them.
func capturingLogf(sink *[]string) func(string, ...any) {
	return func(format string, a ...any) { *sink = append(*sink, fmt.Sprintf(format, a...)) }
}

func joinLog(lines []string) string { return strings.Join(lines, "\n") }

// ── format contract ─────────────────────────────────────────────────────────

func TestParseAgentEnv_AcceptedForms(t *testing.T) {
	var log []string
	body := strings.Join([]string{
		"# a comment",
		"",
		"   ",
		"PLAIN=" + fxEnvValue,
		"  SPACED  =  " + fxEnvValue + "  ",
		"export EXPORTED=" + fxEnvValue,
		`DQUOTED="two words"`,
		"SQUOTED='two words'",
		"EMPTY=",
		"EQ_IN_VALUE=a=b=c",
		"WITH_CR=" + fxEnvValue + "\r",
	}, "\n")
	got := parseAgentEnv(body, "env", capturingLogf(&log))

	want := []agentEnvPair{
		{"PLAIN", fxEnvValue},
		{"SPACED", fxEnvValue},
		{"EXPORTED", fxEnvValue},
		{"DQUOTED", "two words"},
		{"SQUOTED", "two words"},
		{"EMPTY", ""},
		{"EQ_IN_VALUE", "a=b=c"},
		{"WITH_CR", fxEnvValue},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d pairs, want %d: %+v (log:\n%s)", len(got), len(want), got, joinLog(log))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pair %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAgentEnv_LastWins(t *testing.T) {
	var log []string
	got := parseAgentEnv("DUP=first\nDUP=second\n", "env", capturingLogf(&log))
	if len(got) != 1 || got[0].Key != "DUP" || got[0].Value != "second" {
		t.Fatalf("duplicate key must collapse last-wins; got %+v", got)
	}
	if !strings.Contains(joinLog(log), "redefined") {
		t.Errorf("a redefinition must be logged; log:\n%s", joinLog(log))
	}
}

// A malformed line must cost ONLY itself. One typo must not silently strip the
// other variables — that failure mode looks identical to "the file wasn't read".
func TestParseAgentEnv_MalformedLineSkippedOthersSurvive(t *testing.T) {
	var log []string
	body := strings.Join([]string{
		"GOOD_ONE=" + fxEnvValue,
		"this line has no equals sign",
		"=novalue",
		"1BAD_KEY=x",
		"has-dash=x",
		"GOOD_TWO=" + fxEnvValue,
	}, "\n")
	got := parseAgentEnv(body, "env", capturingLogf(&log))

	if len(got) != 2 || got[0].Key != "GOOD_ONE" || got[1].Key != "GOOD_TWO" {
		t.Fatalf("valid lines must survive a malformed neighbour; got %+v", got)
	}
	// Assert the REASON, per skipped line — a silent skip is indistinguishable
	// from a parser that never saw the line at all.
	for _, want := range []string{"not KEY=value", "not a valid variable name"} {
		if !strings.Contains(joinLog(log), want) {
			t.Errorf("missing skip reason %q; log:\n%s", want, joinLog(log))
		}
	}
}

// ── trailing '#' (reviewer F-4) ─────────────────────────────────────────────

// A QUOTED value states its own boundary, so a comment after the closing quote
// is unambiguously not part of the value: strip it, silently, because nothing
// was guessed.
func TestParseAgentEnv_QuotedValueDropsTrailingComment(t *testing.T) {
	var log []string
	body := strings.Join([]string{
		`DQ="abc" # comment`,
		"SQ='abc' # comment",
		`SPACED="a b"   #  lots of space`,
		`HASH_INSIDE="a#b" # real comment`,
	}, "\n")
	got := parseAgentEnv(body, "env", capturingLogf(&log))

	want := map[string]string{"DQ": "abc", "SQ": "abc", "SPACED": "a b", "HASH_INSIDE": "a#b"}
	if len(got) != len(want) {
		t.Fatalf("got %d pairs, want %d: %+v", len(got), len(want), got)
	}
	for _, p := range got {
		if w := want[p.Key]; p.Value != w {
			t.Errorf("%s = %q, want %q — a quoted value delimits itself", p.Key, p.Value, w)
		}
	}
	// Nothing was guessed, so nothing to warn about.
	if strings.Contains(joinLog(log), "kept LITERALLY") {
		t.Errorf("an explicitly quoted value must not warn; log:\n%s", joinLog(log))
	}
}

// An UNQUOTED value has no stated boundary. '#' is legal inside a password, so
// stripping would silently truncate a credential. Keep it literal — but the
// silence is the actual bug this ticket exists to kill, so it MUST warn.
func TestParseAgentEnv_UnquotedTrailingHashIsLiteralAndWarns(t *testing.T) {
	var log []string
	got := parseAgentEnv("BARE=abc # comment\n", "env", capturingLogf(&log))

	if len(got) != 1 || got[0].Value != "abc # comment" {
		t.Fatalf("an unquoted value must be kept literal (a '#' may be part of a credential); got %+v", got)
	}
	if !strings.Contains(joinLog(log), "kept LITERALLY") {
		t.Fatalf("SILENT WRONG VALUE: the ambiguous case must warn; log:\n%s", joinLog(log))
	}
	// The warning must tell the owner how to get the other reading.
	if !strings.Contains(joinLog(log), `KEY="value" # comment`) {
		t.Errorf("the warning must show the fix; log:\n%s", joinLog(log))
	}
}

// A '#' with no preceding whitespace is ordinary value text, not a comment
// shape — it must neither be stripped nor warned about.
func TestParseAgentEnv_HashWithoutSpaceIsPlainValue(t *testing.T) {
	var log []string
	got := parseAgentEnv("PASS=abc#def\n", "env", capturingLogf(&log))

	if len(got) != 1 || got[0].Value != "abc#def" {
		t.Fatalf("a '#' inside a value must be untouched; got %+v", got)
	}
	if strings.Contains(joinLog(log), "kept LITERALLY") {
		t.Errorf("no comment shape ⇒ no warning; log:\n%s", joinLog(log))
	}
}

// The regression this whole item is about: the pre-fix behaviour dragged the
// quotes AND the comment into the value, with no signal at all.
func TestParseAgentEnv_NeverYieldsQuotePlusCommentGarbage(t *testing.T) {
	var log []string
	got := parseAgentEnv(`K="abc" # comment`+"\n", "env", capturingLogf(&log))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got[0].Value, `"`) || strings.Contains(got[0].Value, "#") {
		t.Errorf("value = %q — quotes/comment must never survive into a quoted value", got[0].Value)
	}
}

// ── NUL (reviewer F-1) ──────────────────────────────────────────────────────

// A NUL cannot survive into the agent's environment, so delivering the line
// would hand over a truncated value that looks fine. Refuse it, loudly.
func TestParseAgentEnv_RefusesNULValue(t *testing.T) {
	var log []string
	got := parseAgentEnv("HAS_NUL=ab\x00cd\nGOOD="+fxEnvValue+"\n", "env", capturingLogf(&log))

	if len(got) != 1 || got[0].Key != "GOOD" {
		t.Fatalf("a NUL-bearing value must be refused, not truncated; got %+v", got)
	}
	if !strings.Contains(joinLog(log), "NUL byte") {
		t.Errorf("the refusal must state its reason; log:\n%s", joinLog(log))
	}
}

// ── NEGATIVE: the file is data, never code (failure mode ③) ────────────────

// The env file must NOT become a second .zshrc. Shell metacharacters are values,
// not instructions: nothing here may be expanded, substituted, or executed.
func TestParseAgentEnv_NoShellExecutionOrExpansion(t *testing.T) {
	var log []string
	body := strings.Join([]string{
		"SUBST=$(echo pwned)",
		"BACKTICK=`echo pwned`",
		"EXPAND=$HOME",
		"SEMI=a; echo pwned",
	}, "\n")
	got := parseAgentEnv(body, "env", capturingLogf(&log))

	want := map[string]string{
		"SUBST":    "$(echo pwned)",
		"BACKTICK": "`echo pwned`",
		"EXPAND":   "$HOME",
		"SEMI":     "a; echo pwned",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pairs, want %d: %+v", len(got), len(want), got)
	}
	for _, p := range got {
		if w, ok := want[p.Key]; !ok || p.Value != w {
			t.Errorf("%s = %q, want LITERAL %q — values must never be expanded", p.Key, p.Value, w)
		}
	}
	// And a bare command line (no '=') is not a statement to run — it is refused.
	if bare := parseAgentEnv("touch /tmp/t426d-pwned\n", "env", capturingLogf(&log)); len(bare) != 0 {
		t.Errorf("a bare command line must be refused, got %+v", bare)
	}
}

// The end-to-end proof of the above: render the pairs, source the rendered file
// in a REAL shell, and confirm the metacharacters arrived as literal text and
// nothing executed. A Go-only assertion would not catch a quoting bug in
// renderAgentEnvFile — which is the actual place execution could leak back in.
func TestRenderAgentEnvFile_SourcedByRealShellStaysLiteral(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(dir, "pwned")
	pairs := parseAgentEnv(
		"SUBST=$(touch "+canary+")\nSEMI=x; touch "+canary+"\nPLAIN="+fxEnvValue+"\n",
		"env", func(string, ...any) {})

	rendered := filepath.Join(dir, ".oc-env")
	if err := os.WriteFile(rendered, []byte(renderAgentEnvFile(pairs)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c",
		". "+rendered+`; printf '%s|%s|%s' "$SUBST" "$SEMI" "$PLAIN"`).CombinedOutput()
	if err != nil {
		t.Fatalf("sourcing the rendered file failed: %v (%s)", err, out)
	}
	want := "$(touch " + canary + ")|x; touch " + canary + "|" + fxEnvValue
	if string(out) != want {
		t.Errorf("sourced values = %q, want %q", out, want)
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatal("SECURITY: sourcing the rendered env file EXECUTED the embedded command")
	}
}

// ── NEGATIVE: OC_* is warden-reserved ───────────────────────────────────────

// The env file must not be able to repoint an agent at another server or
// impersonate another member by setting the warden's own identity vars.
func TestParseAgentEnv_RefusesReservedOCKeys(t *testing.T) {
	var log []string
	body := strings.Join([]string{
		"OC_TOKEN=forged",
		"OC_BASE=http://evil.example",
		"OC_SESSION=member-bob",
		"OC_AGENT_HOME=/tmp/evil",
		"OK_VAR=" + fxEnvValue,
	}, "\n")
	got := parseAgentEnv(body, "env", capturingLogf(&log))

	if len(got) != 1 || got[0].Key != "OK_VAR" {
		t.Fatalf("every OC_* key must be refused; got %+v", got)
	}
	for _, k := range []string{"OC_TOKEN", "OC_BASE", "OC_SESSION", "OC_AGENT_HOME"} {
		if !strings.Contains(joinLog(log), k+" is warden-reserved") {
			t.Errorf("refusal of %s must be logged with its reason; log:\n%s", k, joinLog(log))
		}
	}
}

// ── loadAgentEnv: the fail-open ladder (failure mode ②) ─────────────────────

// Each degraded case returns nil. nil alone proves nothing — "failed wrongly"
// and "declined correctly" share it — so every case asserts its REASON too.
func TestLoadAgentEnv_FailOpenBranchesWithReasons(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, agentEnvMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, path, wantReason string }{
		{"unconfigured", "", ""},
		{"absent", filepath.Join(dir, "nope"), "no env file at"},
		{"directory", dir, "is a directory"},
		{"oversized", big, "over the"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var log []string
			got := loadAgentEnv(c.path, capturingLogf(&log))
			if got != nil {
				t.Fatalf("must degrade to nil pairs, got %+v", got)
			}
			if c.wantReason == "" {
				if len(log) != 0 {
					t.Errorf("an unconfigured path must be silent; log:\n%s", joinLog(log))
				}
				return
			}
			if !strings.Contains(joinLog(log), c.wantReason) {
				t.Errorf("missing reason %q; log:\n%s", c.wantReason, joinLog(log))
			}
		})
	}
}

// ── permission signal (failure mode / constraint ③) ─────────────────────────

func TestLoadAgentEnv_WarnsOnLoosePermsButStillLoads(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			var log []string
			p := writeEnvFile(t, "LOOSE="+fxEnvValue+"\n", mode)
			got := loadAgentEnv(p, capturingLogf(&log))

			// Fail-OPEN: a permissions nit must not cost the agent its boot.
			if len(got) != 1 || got[0].Key != "LOOSE" {
				t.Fatalf("loose perms must still load (fail-open); got %+v", got)
			}
			if !strings.Contains(joinLog(log), "wider than 0600") {
				t.Errorf("mode %04o must produce an observable warning; log:\n%s", mode, joinLog(log))
			}
			if !strings.Contains(joinLog(log), "chmod 600") {
				t.Errorf("the warning must tell the owner how to fix it; log:\n%s", joinLog(log))
			}
		})
	}
}

func TestLoadAgentEnv_NoWarningAt0600(t *testing.T) {
	var log []string
	p := writeEnvFile(t, "TIGHT="+fxEnvValue+"\n", 0o600)
	if got := loadAgentEnv(p, capturingLogf(&log)); len(got) != 1 {
		t.Fatalf("0600 file must load; got %+v", got)
	}
	if strings.Contains(joinLog(log), "wider than 0600") {
		t.Errorf("0600 must NOT warn (a warning that always fires is no signal); log:\n%s", joinLog(log))
	}
	// 0400 is TIGHTER than 0600 — also not a warning.
	var log2 []string
	p2 := writeEnvFile(t, "TIGHT="+fxEnvValue+"\n", 0o400)
	loadAgentEnv(p2, capturingLogf(&log2))
	if strings.Contains(joinLog(log2), "wider than 0600") {
		t.Errorf("0400 is tighter than 0600 and must NOT warn; log:\n%s", joinLog(log2))
	}
}

// The diagnostics land in a log file on a shared machine, so they must name
// variables without ever disclosing what they hold.
func TestLoadAgentEnv_LogsNeverContainValues(t *testing.T) {
	var log []string
	secret := "SUPER_SECRET_" + fxEnvValue
	p := writeEnvFile(t, "A_KEY="+secret+"\nDUP="+secret+"\nDUP="+secret+"\nbad line\n", 0o644)
	pairs := loadAgentEnv(p, capturingLogf(&log))
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %+v", pairs)
	}
	if strings.Contains(joinLog(log), secret) {
		t.Fatalf("SECURITY: a value leaked into the log:\n%s", joinLog(log))
	}
	if names := strings.Join(agentEnvKeyNames(pairs), " "); names != "A_KEY DUP" {
		t.Errorf("key names = %q, want %q", names, "A_KEY DUP")
	}
}

// ── start() integration ─────────────────────────────────────────────────────

func startWithEnvFile(t *testing.T, envFile string) (SpawnOutcome, map[string]string, map[string]os.FileMode, *recRunner) {
	t.Helper()
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "4242\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	files := map[string]string{}
	modes := map[string]os.FileMode{}
	deps := newStartDeps(t, run, files)
	deps.EnvFile = envFile
	deps.WriteFile = func(path, content string, mode os.FileMode) error {
		files[path] = content
		modes[path] = mode
		return nil
	}
	out := deps.start(StartParams{
		MemberID:       "alice",
		PersonaContext: "PERSONA-BODY-HERE",
		MemberToken:    fxToken,
		Role:           "assistant",
		Model:          fxModel,
		SessionName:    "member-alice",
	})
	return out, files, modes, run
}

func TestStart_EnvFileRenderedAndSourced(t *testing.T) {
	p := writeEnvFile(t, "FIXTURE_A="+fxEnvValue+"\nFIXTURE_B=two words\n", 0o600)
	out, files, modes, run := startWithEnvFile(t, p)

	if !out.OK {
		t.Fatalf("spawn must succeed: %+v", out)
	}
	render := fxWorkdir + "/" + agentEnvRenderedName
	body, wrote := files[render]
	if !wrote {
		t.Fatalf("%s must be written", render)
	}
	if modes[render] != 0o600 {
		t.Errorf("%s mode = %04o, want 0600 (it holds credentials)", render, modes[render])
	}
	for _, want := range []string{"export FIXTURE_A=" + fxEnvValue, "export FIXTURE_B='two words'"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered file missing %q; got:\n%s", want, body)
		}
	}
	cmd := lastNewSessionCommand(t, run)
	// Sourced, guarded by [ -f ] so a file deleted after render degrades to
	// "no extra env" instead of erroring on the agent's first line.
	if !strings.Contains(cmd, "[ -f "+render+" ] && . "+render+";") {
		t.Errorf("launch line must guard-and-source the render; got:\n%s", cmd)
	}
	// Ordering is load-bearing: the file is sourced BEFORE the OC_* exports, so
	// warden identity wins, and before the PATH export, so an env-file PATH
	// composes with the workdir prepend instead of erasing it.
	if strings.Index(cmd, render) > strings.Index(cmd, "export OC_TOKEN") {
		t.Error("the env file must be sourced BEFORE the OC_* exports")
	}
	if strings.Index(cmd, render) > strings.Index(cmd, "export PATH=") {
		t.Error("the env file must be sourced BEFORE the PATH export")
	}
}

// The launch command is the tmux argv, visible machine-wide via `ps`. The same
// rule the token already follows applies to every env-file value.
func TestStart_EnvValuesNeverRideTheLaunchArgv(t *testing.T) {
	secret := "SECRET_" + fxEnvValue
	p := writeEnvFile(t, "FIXTURE_CRED="+secret+"\n", 0o600)
	_, _, _, run := startWithEnvFile(t, p)

	for _, call := range run.calls {
		for _, arg := range call {
			if strings.Contains(arg, secret) {
				t.Fatalf("SECURITY: an env-file VALUE reached the argv: %v", call)
			}
		}
	}
	// The NAME on the launch line is only the render PATH.
	if cmd := lastNewSessionCommand(t, run); !strings.Contains(cmd, agentEnvRenderedName) {
		t.Errorf("launch line must carry the render path; got:\n%s", cmd)
	}
}

// Fail-open at the top level: no env file ⇒ the agent still boots AND the launch
// line is byte-identical to the pre-T-426d output (the zero-diff guarantee).
func TestStart_MissingEnvFileStillBootsWithByteIdenticalLine(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	out, files, _, run := startWithEnvFile(t, missing)

	if !out.OK || out.PID != "4242" {
		t.Fatalf("a missing env file must NEVER block the spawn; outcome=%+v", out)
	}
	if _, wrote := files[fxWorkdir+"/"+agentEnvRenderedName]; wrote {
		t.Error("no env file ⇒ no render should be written")
	}
	appendSys := buildAppendSystemPrompt("alice", "assistant", fxPersona)
	want := buildLaunchCommandWithEnv(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, "alice", fxBase, "member-alice", fxSocket, fxModel, "", fxSettings,
		[][2]string{{"OC_EFFORT", "medium"}}, "")
	if got := lastNewSessionCommand(t, run); got != want {
		t.Errorf("launch line must be byte-identical when the env file is absent:\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(want, agentEnvRenderedName) {
		t.Error("the absent-file line must carry no env-file fragment at all")
	}
}

// An env file that parses to ZERO usable pairs (all comments / all refused) is
// the same as no file: no render, no source fragment.
func TestStart_AllLinesRefusedProducesNoRender(t *testing.T) {
	p := writeEnvFile(t, "# just a comment\nOC_TOKEN=forged\n1BAD=x\n", 0o600)
	out, files, _, run := startWithEnvFile(t, p)

	if !out.OK {
		t.Fatalf("spawn must succeed: %+v", out)
	}
	if _, wrote := files[fxWorkdir+"/"+agentEnvRenderedName]; wrote {
		t.Error("zero usable pairs ⇒ no render should be written")
	}
	if cmd := lastNewSessionCommand(t, run); strings.Contains(cmd, agentEnvRenderedName) {
		t.Errorf("zero usable pairs ⇒ no source fragment; got:\n%s", cmd)
	}
}

// A stale render from a previous spawn must be cleared, so deleting a credential
// from the env file actually takes it away from the agent.
func TestStart_ClearsStaleRender(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	var removed []string
	deps := newStartDeps(t, run, map[string]string{})
	deps.EnvFile = filepath.Join(t.TempDir(), "does-not-exist")
	deps.Remove = func(p string) error { removed = append(removed, p); return nil }

	deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})

	want := fxWorkdir + "/" + agentEnvRenderedName
	found := false
	for _, r := range removed {
		if r == want {
			found = true
		}
	}
	if !found {
		t.Errorf("a stale %s must be removed even when no env file exists; removed=%v", want, removed)
	}
}

// A WriteFile failure on the render must NOT fail the spawn — the agent boots
// without the extra env rather than not booting at all.
func TestStart_RenderWriteFailureIsNonFatal(t *testing.T) {
	p := writeEnvFile(t, "FIXTURE_A="+fxEnvValue+"\n", 0o600)
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "4242\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	files := map[string]string{}
	var log []string
	deps := newStartDeps(t, run, files)
	deps.EnvFile = p
	deps.Logf = capturingLogf(&log)
	deps.WriteFile = func(path, content string, mode os.FileMode) error {
		if strings.HasSuffix(path, agentEnvRenderedName) {
			return fmt.Errorf("disk full")
		}
		files[path] = content
		return nil
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})

	if !out.OK || out.PID != "4242" {
		t.Fatalf("a render write failure must not block the spawn; outcome=%+v", out)
	}
	if !strings.Contains(joinLog(log), "spawning without extra env") {
		t.Errorf("the degradation must state its reason; log:\n%s", joinLog(log))
	}
	if cmd := lastNewSessionCommand(t, run); strings.Contains(cmd, agentEnvRenderedName) {
		t.Errorf("a failed render must not be sourced; got:\n%s", cmd)
	}
}

// ── default path resolution ─────────────────────────────────────────────────

func TestDefaultAgentEnvFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"main instance", nil, filepath.Join(home, ".officraft", "env")},
		{"namespaced", map[string]string{"OC_NAMESPACE": "seth"}, filepath.Join(home, ".officraft-seth", "env")},
		{"override", map[string]string{"OC_AGENT_ENV_FILE": "/tmp/custom-env"}, "/tmp/custom-env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defaultAgentEnvFile(func(k string) string { return c.env[k] }); got != c.want {
				t.Errorf("defaultAgentEnvFile = %q, want %q", got, c.want)
			}
		})
	}
}

// lastNewSessionCommand pulls the command argument out of the recorded
// `tmux new-session` call.
func lastNewSessionCommand(t *testing.T, run *recRunner) string {
	t.Helper()
	for i := len(run.calls) - 1; i >= 0; i-- {
		c := run.calls[i]
		if len(c) >= 2 && c[0] == "tmux" && strings.Contains(strings.Join(c, " "), "new-session") {
			return c[len(c)-1]
		}
	}
	t.Fatal("no tmux new-session call recorded")
	return ""
}
