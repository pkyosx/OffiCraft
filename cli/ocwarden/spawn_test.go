package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// ── golden files: the ACTUAL Python origin outputs (agent/spawn.py builders),
// generated once and committed. The Go builders must reproduce them byte-for-byte.
// Canonical fixture (matches the generator):
//
//	claude_bin      = /Users/x/.local/bin/claude
//	workdir         = /home/oc/.officraft/agents/alice
//	token           = tok-abc.def.ghi   agent_id = alice   role = assistant
//	base            = http://127.0.0.1:7755
//	session         = member-alice       socket = officraft
//	model           = claude-sonnet-4
var (
	//go:embed testdata/golden_mcp_tok.json
	goldenMCPTok string
	//go:embed testdata/golden_mcp_notok.json
	goldenMCPNoTok string
	//go:embed testdata/golden_statusline.json
	goldenStatusline string
	//go:embed testdata/golden_append.txt
	goldenAppend string
	//go:embed testdata/golden_launch.txt
	goldenLaunch string
	//go:embed testdata/golden_launch_min.txt
	goldenLaunchMin string
)

const (
	fxClaudeBin = "/Users/x/.local/bin/claude"
	fxWorkdir   = "/home/oc/.officraft/agents/alice"
	fxPersona   = fxWorkdir + "/persona.md"
	fxMCPPath   = fxWorkdir + "/.mcp.json"
	fxSettings  = fxWorkdir + "/settings.json"
	fxToken     = "tok-abc.def.ghi"
	fxTokenFile = fxWorkdir + "/.oc-token"
	fxID        = "alice"
	fxBase      = "http://127.0.0.1:7755"
	fxRole      = "assistant"
	fxSession   = "member-alice"
	fxSocket    = "officraft"
	fxModel     = "claude-sonnet-4"
	fxRepoRoot  = "/home/oc/officraft"
	fxOcAgent   = fxWorkdir + "/ocagent"
)

// ── golden-file对賬: byte-for-byte equivalence with the Python origin ─────────

func TestGolden_BuildMCPConfig_BearerHeader(t *testing.T) {
	got := buildMCPConfig(fxBase, fxToken)
	if got != goldenMCPTok {
		t.Fatalf("mcp config diverged from python golden:\n--- got ---\n%s\n--- want ---\n%s", got, goldenMCPTok)
	}
	// Explicit invariants the golden encodes: Bearer HEADER auth (never ?token=).
	if !strings.Contains(got, `"Authorization": "Bearer tok-abc.def.ghi"`) {
		t.Error("token MUST be an Authorization: Bearer header")
	}
	if strings.Contains(got, "?token=") || strings.Contains(got, "token=") {
		t.Error("token MUST NOT appear as a url query")
	}
	if !strings.Contains(got, `"type": "http"`) || !strings.Contains(got, `"url": "http://127.0.0.1:7755/api/mcp"`) {
		t.Error("must wire ONE officraft http MCP server at {base}/api/mcp")
	}
}

func TestGolden_BuildMCPConfig_NoToken(t *testing.T) {
	got := buildMCPConfig(fxBase, "")
	if got != goldenMCPNoTok {
		t.Fatalf("no-token mcp config diverged:\n--- got ---\n%s\n--- want ---\n%s", got, goldenMCPNoTok)
	}
	if strings.Contains(got, "headers") || strings.Contains(got, "Authorization") {
		t.Error("token-less config must omit the headers block")
	}
}

func TestGolden_BuildStatuslineSettings(t *testing.T) {
	if got := buildStatuslineSettings(); got != goldenStatusline {
		t.Fatalf("statusline settings diverged:\n%q\nwant\n%q", got, goldenStatusline)
	}
}

func TestGolden_BuildAppendSystemPrompt(t *testing.T) {
	if got := buildAppendSystemPrompt(fxID, fxRole, fxPersona); got != goldenAppend {
		t.Fatalf("append-system-prompt diverged:\n--- got ---\n%s\n--- want ---\n%s", got, goldenAppend)
	}
}

func TestGolden_BuildLaunchCommand(t *testing.T) {
	appendSys := buildAppendSystemPrompt(fxID, fxRole, fxPersona)
	got := buildLaunchCommand(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, fxSocket, fxModel, "", fxSettings)
	if got != goldenLaunch {
		t.Fatalf("launch command diverged from golden:\n--- got ---\n%s\n--- want ---\n%s", got, goldenLaunch)
	}
	// Frozen-flag invariants the golden encodes.
	for _, must := range []string{
		"--dangerously-skip-permissions",
		// AskUserQuestion (built-in interactive menu) must be denied at the
		// harness: a headless tmux agent that pops the menu blocks forever
		// (2026-07-13 Mira incident) — skip-permissions does not gate it.
		"--disallowedTools AskUserQuestion",
		"--mcp-config " + fxMCPPath,
		"--effort medium",
		"--append-system-prompt",
		"--model claude-sonnet-4",
		"--settings " + fxSettings,
		// ABSOLUTE /bin/cat (T-426d G-1): the owner's env file is sourced earlier
		// in this line and can leave PATH without /bin, which would make a bare
		// `cat` fail and silently empty OC_TOKEN.
		`export OC_TOKEN="$(/bin/cat ` + fxTokenFile + `)"`,
		`export PATH=/home/oc/.officraft/agents/alice:"$PATH";`,
		"exec /Users/x/.local/bin/claude",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("launch command missing frozen fragment: %q", must)
		}
	}
	// The token VALUE must never ride the launch line (tmux argv is `ps`-visible
	// machine-wide) — only the 0600 token-file path does.
	if strings.Contains(got, fxToken) {
		t.Error("launch command must carry the token-file path, NEVER the token value")
	}
	// --effort must immediately follow the --mcp-config <path> pair (no
	// --strict-mcp-config between them, so worker MCP includes account connectors).
	if !strings.Contains(got, "--mcp-config "+fxMCPPath+" --effort") {
		t.Error("--effort must follow --mcp-config <path>")
	}
}

func TestGolden_BuildLaunchCommand_NoModelNoSettings(t *testing.T) {
	appendSys := buildAppendSystemPrompt(fxID, fxRole, fxPersona)
	got := buildLaunchCommand(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, fxSocket, "", "", "")
	if got != goldenLaunchMin {
		t.Fatalf("no-model/no-settings launch diverged:\n--- got ---\n%s\n--- want ---\n%s", got, goldenLaunchMin)
	}
	if strings.Contains(got, "--model") || strings.Contains(got, "--settings") {
		t.Error("unset model/settings must not emit their flags")
	}
	if strings.Contains(got, fxToken) {
		t.Error("launch command must carry the token-file path, NEVER the token value")
	}
}

// M2-2: the server-downpushed effort (member.effort) is baked into the launch
// command; empty keeps the historic pinned "--effort medium" (byte-identical
// line — asserted by the goldens above, which pass effort "").
func TestBuildLaunchCommand_EffortFromServer(t *testing.T) {
	appendSys := buildAppendSystemPrompt(fxID, fxRole, fxPersona)
	got := buildLaunchCommand(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, fxID, fxBase, fxSession, fxSocket, fxModel, "high", fxSettings)
	if !strings.Contains(got, "--effort high") {
		t.Errorf("explicit effort must emit --effort high; got:\n%s", got)
	}
	if strings.Contains(got, "--effort medium") {
		t.Error("explicit effort must replace the medium default")
	}
	// The flag keeps its frozen position: after --mcp-config <path>, before
	// --append-system-prompt.
	if !strings.Contains(got, "--mcp-config "+fxMCPPath+" --effort high --append-system-prompt") {
		t.Error("--effort must stay between --mcp-config <path> and --append-system-prompt")
	}
}

// ── shellQuote: port of shlex.quote ─────────────────────────────────────────

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"safe.path/ok-1", "safe.path/ok-1"}, // fully safe → verbatim
		{"http://127.0.0.1:7755", "http://127.0.0.1:7755"}, // :,/,. all safe
		{"has space", "'has space'"},
		{"open(paren)", "'open(paren)'"},
		{"it's", `'it'"'"'s'`}, // embedded single quote
		{"開始。", "'開始。'"},       // multibyte → unsafe → quoted
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── the spawn mechanism: tmux argv capture + file writes + outcome ──

// recRunner records every argv and answers from out/err maps (default: success,
// empty stdout).
type recRunner struct {
	calls [][]string
	out   map[string]string
	err   map[string]error
}

func (r *recRunner) Run(name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	key := strings.Join(call, " ")
	if e, ok := r.err[key]; ok {
		return "", e
	}
	if s, ok := r.out[key]; ok {
		return s, nil
	}
	return "", nil
}

func (r *recRunner) sawArgv(want ...string) bool {
	key := strings.Join(want, " ")
	for _, c := range r.calls {
		if strings.Join(c, " ") == key {
			return true
		}
	}
	return false
}

// fxOcAgentResolver is the T-81 seam every start() test needs now that it is required.
// It answers the same path the pre-T-81 construction used and reports it present, so a
// test that is not ABOUT ocagent resolution reads exactly as it did before.
func fxOcAgentResolver() (string, bool) { return ocAgentSymlinkTarget(fxRepoRoot, ""), true }

func newStartDeps(t *testing.T, run *recRunner, files map[string]string) SpawnDeps {
	t.Helper()
	return newStartDepsLinks(t, run, files, map[string]string{})
}

// newStartDepsLinks additionally records the workdir `ocagent` SYMLINK: links maps the
// link path (newname) → its target (oldname), so a test can assert the symlink target.
func newStartDepsLinks(t *testing.T, run *recRunner, files, links map[string]string) SpawnDeps {
	t.Helper()
	return SpawnDeps{
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/home/oc/.officraft/agents",
		ClaudeBin: fxClaudeBin,
		RepoRoot:  fxRepoRoot,
		WriteFile: func(path, content string, mode os.FileMode) error { files[path] = content; return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
		Symlink:   func(oldname, newname string) error { links[newname] = oldname; return nil },
		Remove:    func(string) error { return nil },

		// 🔴 EXPLICIT NO-OP CLOCK, and it has to be explicit as of T-82. A nil Sleep
		// used to mean "no wait"; it now falls back to time.Sleep (fail-safe, so a
		// mis-wired production caller paces instead of firing 30 Enters in
		// microseconds). Leaving this nil would therefore make EVERY test built on
		// this helper spend a real nudgeMaxAttempts x nudgeSettle = 30 seconds in
		// the boot nudge. Tests that want to observe the pacing replace this with
		// their own recording clock.
		Sleep: func(time.Duration) {},

		ResolveOcAgentBin: fxOcAgentResolver,
	}
}

func TestStart_HappyPath(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "4242\n"},
		err: map[string]error{hasKey: errAbsent()}, // session absent → clobber-guard passes
	}
	files := map[string]string{}
	links := map[string]string{}
	deps := newStartDepsLinks(t, run, files, links)

	out := deps.start(StartParams{
		MemberID:       "alice",
		PersonaContext: "PERSONA-BODY-HERE",
		MemberToken:    fxToken,
		Role:           "assistant",
		Model:          fxModel,
		SessionName:    "member-alice",
	})

	if !out.OK || out.SessionID != "member-alice" || out.PID != "4242" {
		t.Fatalf("outcome = %+v, want {ok true member-alice 4242}", out)
	}

	// (1) tmux new-session at pinned 160x50 geometry with the launch command.
	appendSys := buildAppendSystemPrompt("alice", "assistant", fxPersona)
	// start() plumbs OC_EFFORT (empty member.effort ⇒ the "medium" default) as an
	// extra env pair, so the expected command must carry it too.
	wantCmd := buildLaunchCommandWithEnv(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, "alice", fxBase, "member-alice", fxSocket, fxModel, "", fxSettings,
		[][2]string{{"OC_EFFORT", "medium"}}, "")
	if !run.sawArgv("tmux", "-L", fxSocket, "new-session", "-d", "-s", "member-alice", "-x", "160", "-y", "50", wantCmd) {
		t.Errorf("expected tmux new-session -x 160 -y 50 with the golden launch command; calls:\n%v", run.calls)
	}
	// The token value must never surface in ANY tmux argv (`ps`-visible); it
	// travels only via the 0600 .oc-token file the launch line cats.
	for _, c := range run.calls {
		if strings.Contains(strings.Join(c, " "), fxToken) {
			t.Errorf("token value leaked into a tmux argv: %v", c)
		}
	}
	if files[fxTokenFile] != fxToken {
		t.Errorf(".oc-token = %q, want the member token", files[fxTokenFile])
	}
	// (2) window-size pinned manual.
	if !run.sawArgv("tmux", "-L", fxSocket, "set-option", "-t", "member-alice", "window-size", "manual") {
		t.Error("expected window-size manual pin")
	}
	// (3) boot nudge delivered via buffer + Enter.
	if !run.sawArgv("tmux", "-L", fxSocket, "set-buffer", "-b", "oc-spawn-nudge", defaultNudge) {
		t.Error("expected nudge loaded into a tmux buffer")
	}
	if !run.sawArgv("tmux", "-L", fxSocket, "paste-buffer", "-t", "member-alice", "-b", "oc-spawn-nudge", "-d", "-p") {
		t.Error("expected paste-buffer -d -p")
	}
	if !run.sawArgv("tmux", "-L", fxSocket, "send-keys", "-t", "member-alice", "Enter") {
		t.Error("expected Enter to commit first user-turn")
	}

	// (4) persona via TRUSTED FILE channel; .mcp.json golden; settings.json golden.
	if files[fxPersona] != "PERSONA-BODY-HERE" {
		t.Errorf("persona.md = %q, want the injected persona_context", files[fxPersona])
	}
	if files[fxMCPPath] != goldenMCPTok {
		t.Errorf(".mcp.json diverged from golden:\n%s", files[fxMCPPath])
	}
	if files[fxSettings] != goldenStatusline {
		t.Errorf("settings.json diverged from golden:\n%s", files[fxSettings])
	}
	// (5) ocagent published into the workdir as a SYMLINK to the resolved ocagent
	// binary (the case-B fix: without it the bare `ocagent listen` never resolves →
	// deaf boot). Not a data file — nothing is WriteFile'd at that path.
	if _, wrote := files[fxOcAgent]; wrote {
		t.Errorf("ocagent must be a symlink, not a written file; got file content:\n%s", files[fxOcAgent])
	}
	if got, want := links[fxOcAgent], ocAgentSymlinkTarget(fxRepoRoot, ""); got != want {
		t.Errorf("ocagent symlink target = %q, want %q", got, want)
	}
}

func TestStart_CodexUsesSidecarWithoutTUINudge(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "5252\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	deps.CodexBin = "/opt/homebrew/bin/codex"
	deps.WardenBin = "/opt/officraft/ocwarden"

	out := deps.start(StartParams{
		MemberID:       "alice",
		PersonaContext: "PERSONA-BODY-HERE",
		MemberToken:    fxToken,
		Role:           "assistant",
		Runtime:        "codex",
		Model:          "gpt-5.6",
		Effort:         "high",
		SessionName:    "member-alice",
	})
	if !out.OK || out.PID != "5252" {
		t.Fatalf("outcome = %+v, want successful Codex sidecar", out)
	}
	if !run.sawArgv(deps.CodexBin, "login", "status") {
		t.Fatal("Codex spawn must fail-fast through `codex login status`")
	}
	wantCmd := buildCodexLaunchCommand(
		deps.WardenBin, deps.CodexBin, fxWorkdir, fxPersona, fxTokenFile,
		"alice", fxBase, "member-alice", fxSocket, "gpt-5.6", "high",
		[][2]string{{"OC_EFFORT", "high"}}, "",
	)
	if !run.sawArgv("tmux", "-L", fxSocket, "new-session", "-d", "-s", "member-alice", "-x", "160", "-y", "50", wantCmd) {
		t.Errorf("expected Codex sidecar tmux launch; calls:\n%v", run.calls)
	}
	if run.sawArgv("tmux", "-L", fxSocket, "send-keys", "-t", "member-alice", "Enter") {
		t.Fatal("Codex App Server boot must not receive Claude TUI keystrokes")
	}
}

func TestStart_CodexLoggedOutFailsBeforeLaunch(t *testing.T) {
	run := &recRunner{err: map[string]error{
		"/opt/homebrew/bin/codex login status": errors.New("not logged in"),
	}}
	deps := newStartDeps(t, run, map[string]string{})
	deps.CodexBin = "/opt/homebrew/bin/codex"
	deps.WardenBin = "/opt/officraft/ocwarden"
	out := deps.start(StartParams{
		MemberID: "alice", MemberToken: fxToken, Runtime: "codex",
	})
	if out.OK || !strings.HasPrefix(out.Reason, "codex_not_logged_in:") {
		t.Fatalf("outcome = %+v, want codex_not_logged_in", out)
	}
	for _, call := range run.calls {
		if len(call) > 0 && call[0] == "tmux" {
			t.Fatalf("logged-out Codex must fail before tmux launch: %v", run.calls)
		}
	}
}

// ── ocagent symlink target: the case-B workdir publish (golang ocagent, not python) ──

// TestOcAgentSymlinkTarget: with no resolved OcAgentBin (dev / in-tree), the workdir
// `ocagent` symlink points at the repoRoot-relative <repoRoot>/cli/ocagent/ocagent.
func TestOcAgentSymlinkTarget(t *testing.T) {
	got := ocAgentSymlinkTarget(fxRepoRoot, "")
	if got != fxRepoRoot+"/cli/ocagent/ocagent" {
		t.Errorf("symlink target = %q, want the repoRoot-relative golang ocagent", got)
	}
	// No python anywhere — the target is the compiled golang binary.
	if strings.Contains(got, "python") || strings.Contains(got, "agent.oc_agent") {
		t.Errorf("symlink target must NOT reference python, got: %s", got)
	}
}

// TestOcAgentSymlinkTarget_OcAgentBinOverride: a resolved OcAgentBin (the home sibling
// $HOME/.officraft/warden/ocagent a home-installed warden carries, download-guaranteed)
// becomes the symlink target verbatim, REPLACING the repoRoot-relative dev fallback.
func TestOcAgentSymlinkTarget_OcAgentBinOverride(t *testing.T) {
	const ocAgentBin = "/Users/seth_wang/.officraft/warden/ocagent"
	got := ocAgentSymlinkTarget(fxRepoRoot, ocAgentBin)
	if got != ocAgentBin {
		t.Errorf("symlink target = %q, want the explicit home sibling %q", got, ocAgentBin)
	}
	// the repoRoot-relative fallback path must NOT appear when the sibling is resolved.
	if strings.Contains(got, "/cli/ocagent/ocagent") {
		t.Errorf("home sibling must replace the repoRoot-relative fallback, got: %s", got)
	}
}

// TestStart_PublishesOcAgentSymlink: start publishes the workdir `ocagent` as a
// SYMLINK (Remove-then-Symlink) to the resolved binary — NOT a WriteFile'd data file —
// so warden self-update's atomic rename of the target transparently reaches the agent.
// The Remove clears any stale link first (idempotent re-spawn).
func TestStart_PublishesOcAgentSymlink(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	modes := map[string]os.FileMode{}
	links := map[string]string{}
	removed := []string{}
	deps := SpawnDeps{
		// T-82: explicit no-op clock — a nil Sleep now falls back to time.Sleep
		// (fail-safe for production), so leaving it out costs this test a real 30s.
		Sleep:     func(time.Duration) {},
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/home/oc/.officraft/agents",
		ClaudeBin: fxClaudeBin,
		RepoRoot:  fxRepoRoot,
		WriteFile: func(path, content string, mode os.FileMode) error { modes[path] = mode; return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
		Symlink:   func(oldname, newname string) error { links[newname] = oldname; return nil },
		Remove:    func(name string) error { removed = append(removed, name); return nil },

		ResolveOcAgentBin: fxOcAgentResolver,
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	// ocagent is a symlink to the resolved target, and NOT a written data file.
	if got, want := links[fxOcAgent], ocAgentSymlinkTarget(fxRepoRoot, ""); got != want {
		t.Errorf("ocagent symlink target = %q, want %q", got, want)
	}
	if _, wrote := modes[fxOcAgent]; wrote {
		t.Errorf("ocagent must be a symlink, not a WriteFile'd path")
	}
	// stale link cleared first (idempotent re-spawn). T-426d adds a second,
	// unrelated Remove — the stale .oc-env render — so assert the ocagent link is
	// among the removals rather than that it is the only one.
	if !slices.Contains(removed, fxOcAgent) {
		t.Errorf("must Remove the stale ocagent link before symlinking; removed=%v", removed)
	}
	// the data files stay 0600 (unchanged), including the token file.
	if modes[fxPersona] != 0o600 || modes[fxMCPPath] != 0o600 || modes[fxSettings] != 0o600 || modes[fxTokenFile] != 0o600 {
		t.Errorf("data files must stay 0600: persona=%o mcp=%o settings=%o token=%o", modes[fxPersona], modes[fxMCPPath], modes[fxSettings], modes[fxTokenFile])
	}
}

// TestStart_OcAgentSymlinkFailureAborts: a failing symlink aborts the spawn (same
// abort contract as the data-file writes) — never new-session past it.
func TestStart_OcAgentSymlinkFailureAborts(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	deps := SpawnDeps{
		// T-82: explicit no-op clock — a nil Sleep now falls back to time.Sleep
		// (fail-safe for production), so leaving it out costs this test a real 30s.
		Sleep:     func(time.Duration) {},
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/home/oc/.officraft/agents",
		ClaudeBin: fxClaudeBin,
		RepoRoot:  fxRepoRoot,
		WriteFile: func(path, content string, mode os.FileMode) error { return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
		Symlink:   func(oldname, newname string) error { return errString("symlink failed") },
		Remove:    func(string) error { return nil },
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if out.OK {
		t.Fatal("a failing ocagent symlink MUST abort the spawn")
	}
	for _, c := range run.calls {
		if len(c) >= 4 && c[3] == "new-session" {
			t.Errorf("must not new-session after the symlink failed; calls: %v", run.calls)
		}
	}
}

// TestStart_OcAgentSymlinkRemoveNotExistOK: a not-exist Remove (fresh workdir, no
// prior link) is IGNORED — the spawn proceeds to symlink. Only a real Remove error
// (non not-exist) aborts.
func TestStart_OcAgentSymlinkRemoveNotExistOK(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{
		out: map[string]string{"tmux -L officraft display-message -p -t member-alice #{pane_pid}": "7\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	links := map[string]string{}
	deps := SpawnDeps{
		// T-82: explicit no-op clock — a nil Sleep now falls back to time.Sleep
		// (fail-safe for production), so leaving it out costs this test a real 30s.
		Sleep:     func(time.Duration) {},
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/home/oc/.officraft/agents",
		ClaudeBin: fxClaudeBin,
		RepoRoot:  fxRepoRoot,
		WriteFile: func(path, content string, mode os.FileMode) error { return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
		Symlink:   func(oldname, newname string) error { links[newname] = oldname; return nil },
		Remove:    func(string) error { return os.ErrNotExist },

		ResolveOcAgentBin: fxOcAgentResolver,
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("a not-exist Remove must NOT abort the spawn; outcome = %+v", out)
	}
	if links[fxOcAgent] == "" {
		t.Error("spawn must proceed to symlink after a not-exist Remove")
	}
}

// TestResolveRepoRoot: the repo root is three parents up from the ocwarden binary
// (<repoRoot>/cli/ocwarden/ocwarden) — one level deeper than the python origin's
// two-parent __file__ walk, because the binary is now grouped under cli/.
func TestResolveRepoRoot(t *testing.T) {
	got := resolveRepoRoot(func() (string, error) { return "/home/oc/officraft/cli/ocwarden/ocwarden", nil })
	if got != "/home/oc/officraft" {
		t.Errorf("resolveRepoRoot = %q, want /home/oc/officraft", got)
	}
	// an unresolvable executable yields "" (degenerate, but must not panic).
	if got := resolveRepoRoot(func() (string, error) { return "", errString("no exe") }); got != "" {
		t.Errorf("unresolvable executable must yield empty root, got %q", got)
	}
}

// TestResolveOcAgentBin: a home-installed warden finds ocagent as its OWN SIBLING
// (no env/plist), and only falls back to the repoRoot-relative dev path when the
// sibling does not exist.
func TestResolveOcAgentBin(t *testing.T) {
	const repoRoot = "/home/oc/officraft"
	homeExe := func() (string, error) { return "/Users/seth/.officraft/warden/ocwarden", nil }
	devExe := func() (string, error) { return repoRoot + "/cli/ocwarden/ocwarden", nil }

	// Sibling exists → use it (the self-contained home-install layout), and report it
	// as present.
	sibling := "/Users/seth/.officraft/warden/ocagent"
	if got, ok := resolveOcAgentBin(homeExe, func(p string) bool { return p == sibling }, repoRoot); got != sibling || !ok {
		t.Errorf("home-install must exec the sibling ocagent, got %q ok=%v want %q ok=true", got, ok, sibling)
	}
	// No sibling on disk → fall back to the repoRoot-relative dev path.
	wantFallback := repoRoot + "/cli/ocagent/ocagent"
	if got, _ := resolveOcAgentBin(devExe, func(string) bool { return false }, repoRoot); got != wantFallback {
		t.Errorf("dev run must fall back to repoRoot-relative ocagent, got %q want %q", got, wantFallback)
	}
	// Unresolvable executable → still yields the repoRoot fallback (no panic).
	if got, _ := resolveOcAgentBin(func() (string, error) { return "", errString("no exe") }, func(string) bool { return true }, repoRoot); got != wantFallback {
		t.Errorf("unresolvable exe must yield the repoRoot fallback, got %q", got)
	}

	// T-81 — the half that did not exist before: the fallback branch now REPORTS
	// whether the path it settled on is actually there. This is the whole difference
	// between "deaf forever, silently" and one visible refusal: nothing on a fresh
	// machine has $HOME/cli/ocagent/ocagent, and the old signature had no way to say so.
	if got, ok := resolveOcAgentBin(devExe, func(p string) bool { return p == wantFallback }, repoRoot); got != wantFallback || !ok {
		t.Errorf("fallback that EXISTS must report ok=true, got %q ok=%v", got, ok)
	}
	if got, ok := resolveOcAgentBin(devExe, func(string) bool { return false }, repoRoot); got != wantFallback || ok {
		t.Errorf("fallback that does NOT exist must report ok=false, got %q ok=%v", got, ok)
	}
}

func TestStart_PretrustSeam(t *testing.T) {
	// The Pretrust seam is invoked before launch; a FAILING Pretrust aborts the
	// spawn (a live trust gate would eat the boot nudge → refuse rather than spawn
	// a nudge-eaten zombie). A nil Pretrust is skipped (covered by HappyPath).
	t.Run("invoked on success", func(t *testing.T) {
		hasKey := "tmux -L officraft has-session -t member-alice"
		pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
		run := &recRunner{
			out: map[string]string{pidKey: "4242\n"},
			err: map[string]error{hasKey: errAbsent()},
		}
		files := map[string]string{}
		deps := newStartDeps(t, run, files)
		called := false
		deps.Pretrust = func() error { called = true; return nil }

		out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice", Model: fxModel})
		if !out.OK {
			t.Fatalf("outcome = %+v, want ok", out)
		}
		if !called {
			t.Error("Pretrust seam MUST be invoked before launch")
		}
	})

	t.Run("failure aborts spawn (no new-session)", func(t *testing.T) {
		hasKey := "tmux -L officraft has-session -t member-alice"
		run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
		files := map[string]string{}
		deps := newStartDeps(t, run, files)
		deps.Pretrust = func() error { return errString("trust write failed") }

		out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
		if out.OK {
			t.Fatal("a failing Pretrust MUST abort the spawn (don't spawn a nudge-eaten zombie)")
		}
		if !strings.HasPrefix(out.Reason, "pretrust_failed:") {
			t.Errorf("pretrust abort must carry a pretrust_failed reason, got %q", out.Reason)
		}
		for _, c := range run.calls {
			if len(c) >= 4 && c[3] == "new-session" {
				t.Errorf("must not new-session after Pretrust failed; calls: %v", run.calls)
			}
		}
	})
}

func TestStart_SessionNameDefaultsToMemberSessionName(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-bob"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}}
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{MemberID: "Bob", MemberToken: fxToken}) // empty SessionName
	if !out.OK || out.SessionID != "member-bob" {
		t.Fatalf("outcome = %+v, want session member-bob (memberSessionName default, lowercased)", out)
	}
}

func TestStart_RefusesToClobberLiveSession(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{out: map[string]string{hasKey: ""}} // has-session succeeds → PRESENT
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if out.OK {
		t.Fatal("start MUST refuse to clobber a live session")
	}
	// The refusal carries a STRUCTURED reason (not the old ambiguous bare
	// OK=false) so the server-folded last_op_reason tells the owner WHY.
	if !strings.HasPrefix(out.Reason, "session_already_exists:") {
		t.Errorf("clobber-guard refusal must carry a session_already_exists reason, got %q", out.Reason)
	}
	if run.sawArgv("tmux", "-L", fxSocket, "new-session", "-d", "-s", "member-alice", "-x", "160", "-y", "50", "") {
		t.Error("must not have attempted new-session on a live session")
	}
}

// TestStart_WorkerGhostSession_RealTmuxAbsentSpawns (T-9ccf, O-19): the
// clobber-guard reconciles against REAL tmux (tmuxHasSession), NOT any server-
// side "this worker was dispatched to warden X" registration. When the server
// believes a session is live (a stale workerSpawnTarget stamp) but real tmux
// has NO such session, the warden MUST spawn — the ghost registration never
// blocks the retry. This is the warden half of the O-19 fix; the finding is
// that the warden is STATELESS and already treats real tmux as the source of
// truth, so the guard needs no change — this test LOCKS that so a future
// refactor can't reintroduce an internal registry that outlives the session.
// Positive control lives in TestStart_RefusesToClobberLiveSession (a REAL live
// session is still refused).
func TestStart_WorkerGhostSession_RealTmuxAbsentSpawns(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-ow-1"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}} // real tmux: NO session
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{
		MemberID: "ow-1", PersonaContext: fxPersona, MemberToken: fxToken,
		Role: "outsource-worker",
	})
	if !out.OK || out.SessionID != "member-ow-1" {
		t.Fatalf("a ghost registration with real tmux absent MUST spawn, got %+v", out)
	}
}

// TestStart_WorkerBrokenProbe_SpawnsConservatively: a BROKEN has-session probe
// (nil three-way — binary missing / unclassifiable) is NOT treated as present,
// so start proceeds. A broken probe must never wedge a worker in a permanent
// clobber-refusal (the fail-open half of "reconcile against real tmux").
func TestStart_WorkerBrokenProbe_SpawnsConservatively(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-ow-1"
	// A non-classifiable error → tmuxHasSession returns nil (UNKNOWN), which the
	// guard reads as "not positively present" → spawn.
	run := &recRunner{err: map[string]error{hasKey: fmt.Errorf("tmux: some unclassifiable failure")}}
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{
		MemberID: "ow-1", PersonaContext: fxPersona, MemberToken: fxToken,
		Role: "outsource-worker",
	})
	if !out.OK {
		t.Fatalf("a broken probe must NOT refuse (conservative spawn), got %+v", out)
	}
}

func TestStart_NewSessionFailure(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{
		hasKey: errAbsent(),
	}}
	// Make new-session fail: match its exact argv key.
	appendSys := buildAppendSystemPrompt("alice", "agent", fxPersona)
	cmd := buildLaunchCommandWithEnv(fxClaudeBin, fxWorkdir, fxMCPPath, appendSys,
		fxTokenFile, "alice", fxBase, "member-alice", fxSocket, "", "", fxSettings,
		[][2]string{{"OC_EFFORT", "medium"}}, "")
	nsKey := strings.Join([]string{"tmux", "-L", fxSocket, "new-session", "-d", "-s", "member-alice", "-x", "160", "-y", "50", cmd}, " ")
	run.err[nsKey] = errAbsent() // any error
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if out.OK {
		t.Fatal("new-session failure must yield ok=false")
	}
	if !strings.HasPrefix(out.Reason, "spawn_exec_failed:") {
		t.Errorf("new-session failure must carry a spawn_exec_failed reason, got %q", out.Reason)
	}
}

func TestStart_NoClaudeBin(t *testing.T) {
	run := &recRunner{}
	deps := SpawnDeps{
		// T-82: explicit no-op clock — a nil Sleep now falls back to time.Sleep
		// (fail-safe for production), so leaving it out costs this test a real 30s.
		Sleep:     func(time.Duration) {},
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/tmp",
		WriteFile: func(string, string, os.FileMode) error { return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken})
	if out.OK {
		t.Fatal("unresolvable claude bin must yield ok=false")
	}
	// Full equality, not a prefix/contains probe: the ONLY thing that was ever
	// wrong with this refusal lived in the part a prefix assert does not read
	// — it offered two exits and both were "go get claude". The third exit
	// (change this member's runtime) has to be IN the sentence, so the whole
	// sentence is the assertion.
	want := "claude_bin_unresolved: no Claude Code on this machine. " +
		"Fix any one: set this member's 執行環境 to Codex; " +
		"install Claude Code here; or re-install the warden with OC_CLAUDE_BIN=<path>."
	// The Codex exit is the hard acceptance criterion of this ticket and is
	// asserted SEPARATELY from the equality above, so that a future edit which
	// rewords the sentence cannot quietly drop it: whoever updates `want` to
	// match their new wording has to defeat this line on purpose.
	if !strings.Contains(out.Reason, "Codex") {
		t.Errorf("the refusal must always offer the change-this-member's-runtime "+
			"exit — that is the whole reason it was rewritten; got %q", out.Reason)
	}
	// One line, not a paragraph. last_op_reason is contractually a "structured
	// one-line cause" (ocapi_gen.go) and the cockpit renders it un-truncated in
	// red (.mp-lastop__reason). The version this replaced ran 431 display
	// columns. The bound is deliberately loose — it is a wall-of-red guard, not
	// a style rule — but it does fail on a relapse into prose.
	if width := runewidth(out.Reason); width > 220 {
		t.Errorf("refusal is %d display columns; last_op_reason is rendered "+
			"un-truncated in the member panel and a paragraph there becomes a wall "+
			"of red: %q", width, out.Reason)
	}
	if out.Reason != want {
		t.Errorf("refusal reason\n got: %q\nwant: %q", out.Reason, want)
	}
	if len(run.calls) != 0 {
		t.Errorf("must not touch tmux when claude bin is unresolved, got %v", run.calls)
	}
}

// T-ba62: claude present but LOGGED OUT is refused BEFORE any side effect, and
// the refusal is identified by its REASON — "wrongly failed" and "correctly
// refused" share OK=false, so asserting the flag alone proves nothing.
func TestStart_ClaudeNotLoggedIn(t *testing.T) {
	run := &recRunner{}
	wrote := map[string]string{}
	deps := SpawnDeps{
		// T-82: explicit no-op clock — a nil Sleep now falls back to time.Sleep
		// (fail-safe for production), so leaving it out costs this test a real 30s.
		Sleep:     func(time.Duration) {},
		Runner:    run,
		Base:      fxBase,
		Socket:    fxSocket,
		Home:      "/tmp",
		ClaudeBin: fxClaudeBin,
		ClaudeCreds: func() claudeCredStatus {
			// THE SHAPE A REAL HOST PRODUCES, not a convenient short one.
			// probeClaudeCreds marks cred_file, keychain and all four
			// claudeCredEnvKeys, so a signed-out Mac renders SIX pairs. Stubbing
			// two made the width assertion below measure a string that is 110
			// columns shorter than anything an owner ever sees — the guard was
			// green on a message that does not exist.
			return claudeCredStatus{Present: false, Summary: sixSourceUnsetSummary}
		},
		WriteFile: func(p, c string, _ os.FileMode) error { wrote[p] = c; return nil },
		MkdirAll:  func(string, os.FileMode) error { return nil },
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken})
	if out.OK {
		t.Fatal("a logged-out claude must yield ok=false, not a silent OK")
	}
	// The WHOLE sentence, composed from this test's own input (the summary the
	// stub credential seam returned). The keyword-sampling version this
	// replaced could not see anything it did not happen to sample — including
	// whether the summary leaked a credential VALUE rather than the
	// value-free SET/unset shape, which is the one property this reason
	// exists to keep.
	want := fmt.Sprintf(
		"claude_not_logged_in: no claude credential here (%s). "+
			"Fix any one: set this member's 執行環境 to Codex; "+
			"run `claude` once as this user; or re-install the warden "+
			"with OC_CLAUDE_CRED_CHECK=0 (shell exports do not reach it).",
		sixSourceUnsetSummary)
	if out.Reason != want {
		t.Errorf("refusal reason\n got: %q\nwant: %q", out.Reason, want)
	}
	// THE EXIT THAT MUST NOT SILENTLY LEAVE. This arm is the one a
	// claude-installed-but-signed-out host lands on, so if it ever stops
	// naming the per-member runtime switch, the owner is back to being told
	// to go fix a runtime they may have deliberately declined — the exact
	// failure this ticket opened on. The whole-sentence compare above would
	// also catch it, but it would read as "someone reworded a string"; this
	// one names the property.
	// 🔴 PIN THE RUNTIME BY NAME, not by the Chinese label around it. An
	// independent reviewer broke the first version of this assertion by
	// rewriting the sentence to "set this member's 執行環境 elsewhere" and
	// updating `want` to match: the whole-sentence compare went green, this
	// line went green, and the message no longer told anyone about Codex at
	// all. The label is the container; "Codex" is the property. The sibling
	// test (TestStart_NoClaudeBin) already pins it this way.
	if !strings.Contains(out.Reason, "Codex") {
		t.Errorf("the signed-out refusal must still name the Codex exit, got %q", out.Reason)
	}
	if !strings.Contains(out.Reason, "執行環境") {
		t.Errorf("the Codex exit must name the per-member setting the owner edits, got %q", out.Reason)
	}
	// Wall-of-red ceiling, measured against the SIX-source summary above —
	// i.e. what an owner actually sees. 359 at the time of writing (the old
	// wording was 516). It cannot meet claudeBinUnresolvedReason's 220 because
	// that one is a constant with no summary interpolated at all; 140 of these
	// columns are the summary itself. Shrinking THAT is a separate change.
	if width := runewidth(out.Reason); width > 380 {
		t.Errorf("refusal reason is %d display columns; keep it short enough to read "+
			"on the member row (was 406 before T-b3d0)", width)
	}
	// NO RESIDUE: no tmux session, no workdir files.
	if len(run.calls) != 0 {
		t.Errorf("must not touch tmux when claude is logged out, got %v", run.calls)
	}
	if len(wrote) != 0 {
		t.Errorf("must not write any workdir file when claude is logged out, got %v", wrote)
	}
}

// The gate must NOT stand in the way of a credentialed host (the positive
// control for the test above: a green refusal test alone cannot tell a working
// gate from one that refuses everything).
func TestStart_ClaudeLoggedInProceeds(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	run := &recRunner{err: map[string]error{hasKey: errAbsent()}} // session absent
	files := map[string]string{}
	deps := newStartDeps(t, run, files)
	deps.ClaudeCreds = func() claudeCredStatus {
		return claudeCredStatus{Present: true, Summary: "keychain=SET"}
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("a credentialed host must spawn normally; got reason %q", out.Reason)
	}
}

func TestAgentWorkdir_Lowercased(t *testing.T) {
	if got := agentWorkdir("/home/oc/agents", "Alice"); got != "/home/oc/agents/alice" {
		t.Errorf("agentWorkdir = %q, want lowercased join", got)
	}
}

// ── pretrustWorkdir: the real ~/.claude.json trust-mark (temp files ONLY) ─────
//
// EVERY test here writes to a t.TempDir() path — NEVER the live ~/.claude.json.

const fxTrustWorkdir = "/home/oc/.officraft/agents/alice"

// readClaudeJSON parses a claude.json temp file as the python-origin structure so
// tests can assert on projects["<workdir>"].hasTrustDialogAccepted.
func readClaudeJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("claude.json is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

// trusted reports whether projects[workdir].hasTrustDialogAccepted == true — the
// exact key/structure the python origin writes and claude reads.
func trusted(m map[string]any, workdir string) bool {
	projects, ok := m["projects"].(map[string]any)
	if !ok {
		return false
	}
	entry, ok := projects[workdir].(map[string]any)
	if !ok {
		return false
	}
	v, _ := entry["hasTrustDialogAccepted"].(bool)
	return v
}

func TestPretrustWorkdir_CreatesFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatalf("pretrustWorkdir: %v", err)
	}
	m := readClaudeJSON(t, path) // (e) round-trips through json.Unmarshal cleanly
	if !trusted(m, fxTrustWorkdir) {
		t.Errorf("expected projects[%q].hasTrustDialogAccepted=true, got %+v", fxTrustWorkdir, m)
	}
}

func TestPretrustWorkdir_MergesPreservingOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	// Pre-existing config: an unrelated top-level key AND an unrelated project entry,
	// plus a sibling field inside the very entry we will trust.
	seed := `{
	  "numStartups": 7,
	  "oauthAccount": {"emailAddress": "x@y.z"},
	  "projects": {
	    "/some/other/dir": {"hasTrustDialogAccepted": true, "history": ["a"]},
	    "` + fxTrustWorkdir + `": {"exampleFiles": ["main.go"]}
	  }
	}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatalf("pretrustWorkdir: %v", err)
	}
	m := readClaudeJSON(t, path)

	// (b) unrelated top-level keys untouched.
	if m["numStartups"].(float64) != 7 {
		t.Errorf("numStartups clobbered: %v", m["numStartups"])
	}
	if oa, ok := m["oauthAccount"].(map[string]any); !ok || oa["emailAddress"] != "x@y.z" {
		t.Errorf("oauthAccount clobbered: %v", m["oauthAccount"])
	}
	projects := m["projects"].(map[string]any)
	// unrelated project entry untouched.
	other := projects["/some/other/dir"].(map[string]any)
	if other["hasTrustDialogAccepted"] != true || len(other["history"].([]any)) != 1 {
		t.Errorf("unrelated project entry clobbered: %v", other)
	}
	// our entry: trust added, its pre-existing sibling field preserved.
	entry := projects[fxTrustWorkdir].(map[string]any)
	if entry["hasTrustDialogAccepted"] != true {
		t.Errorf("trust mark not set: %v", entry)
	}
	if ef, ok := entry["exampleFiles"].([]any); !ok || len(ef) != 1 || ef[0] != "main.go" {
		t.Errorf("sibling field in the trusted entry clobbered: %v", entry["exampleFiles"])
	}
}

func TestPretrustWorkdir_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// (c) marking the SAME workdir twice is a harmless no-op change (byte-stable).
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second pretrust changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if !trusted(readClaudeJSON(t, path), fxTrustWorkdir) {
		t.Error("workdir must remain trusted after a repeat pretrust")
	}
}

func TestPretrustWorkdir_FileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatal(err)
	}
	// (d) the written file is 0600 (the token-adjacent config must not be world/group
	// readable). Applies to both the create-new and rewrite-existing paths.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("claude.json perm = %o, want 0600", perm)
	}
	// rewrite path: pre-seed at a looser mode, pretrust must land 0600.
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(path)
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("after rewrite, perm = %o, want 0600", perm)
	}
}

func TestPretrustWorkdir_UnparsableFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	// A corrupt/unparsable file is treated as empty (only absent/corrupt data is
	// replaced) — after pretrust it is valid JSON with the trust mark.
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pretrustWorkdir(path, fxTrustWorkdir); err != nil {
		t.Fatalf("pretrustWorkdir over a corrupt file: %v", err)
	}
	if !trusted(readClaudeJSON(t, path), fxTrustWorkdir) {
		t.Error("a corrupt file must be replaced with a valid trusted config")
	}
}

func TestDefaultClaudeJSONPath(t *testing.T) {
	// OC_CLAUDE_JSON override wins (the PoC safety valve keeping tests off live data).
	env := func(k string) string {
		if k == "OC_CLAUDE_JSON" {
			return "/tmp/throwaway.json"
		}
		return ""
	}
	if got := defaultClaudeJSONPath(env); got != "/tmp/throwaway.json" {
		t.Errorf("override = %q, want /tmp/throwaway.json", got)
	}
	// Absent override → ~/.claude.json.
	if got := defaultClaudeJSONPath(func(string) string { return "" }); !strings.HasSuffix(got, "/.claude.json") {
		t.Errorf("default = %q, want a ~/.claude.json path", got)
	}
}

// errAbsent is a benign "session missing" tmux error (classified absent by
// tmuxClassifyAbsent) so the clobber-guard reads the session as not present.
func errAbsent() error { return errString("can't find session: member-x") }

type errString string

func (e errString) Error() string { return string(e) }

// nudgeCountingRunner counts the boot nudge's Enter and paste presses.
//
// T-82 renamed it from seqCaptureRunner and DELETED its capture-pane arm, which had
// become dead: it fed a programmed pane transcript to the success verdict this
// ticket removed, and both remaining users construct it empty. Deleting the arm is
// not tidying — a fake that still ANSWERS capture-pane is a fake that would let a
// re-added screen read pass unnoticed. This one has nothing to answer with.
type nudgeCountingRunner struct {
	enterCount int
	pasteCount int
}

func (r *nudgeCountingRunner) Run(name string, args ...string) (string, error) {
	argv := strings.Join(append([]string{name}, args...), " ")
	switch {
	case strings.Contains(argv, "send-keys") && strings.Contains(argv, "Enter"):
		r.enterCount++
	case strings.Contains(argv, "paste-buffer"):
		r.pasteCount++
	}
	return "", nil
}

// TestTmuxDeliverNudge_AlwaysRunsBoundedAttempts (T-82): the Enter retry is bounded
// at nudgeMaxAttempts and a SINGLE paste, and it runs those attempts UNCONDITIONALLY
// — there is no early exit, because there is no local success verdict any more.
//
// This replaces a test that asserted the loop stopped early once the status line
// showed a numeric context gauge. That verdict has been permanently false, so the
// loop had already been running all attempts every time — and the old test kept
// passing throughout, because its fake fed the pane text the verdict wanted.
// (The status line in question is OUR OWN — see the comment on tmuxDeliverNudge.)
//
// 🔴 THE COUNTS BELOW ARE WRITTEN AS LITERALS ON PURPOSE. Asserting
// `enterCount != nudgeMaxAttempts` is an IDENTITY — a constant compared against
// itself — so it has zero discrimination against a change to the constant. An
// independent review made exactly that edit (30 → 1, i.e. back to the single-shot
// Enter this loop's own comment names as the Phase-4 boot-death) and the whole
// 517-test package stayed green. With literals, that edit is red here.
//
// ⚠️ If you are changing nudgeMaxAttempts, that is a BEHAVIOUR CHANGE, not a
// constant tidy-up: it is its own ticket with its own measurement, and it must
// also update the receiptDeadlineSecs derivation in
// server/ocserverd/receipt_watch.go, which spends this loop's wall time.
//
// DEFEATED BY: an early `return` in the loop; changing nudgeMaxAttempts; changing
// nudgeSettle. All three are red here.
func TestTmuxDeliverNudge_AlwaysRunsBoundedAttempts(t *testing.T) {
	const wantAttempts = 30                    // literal, NOT nudgeMaxAttempts — see above
	const wantSettle = 1000 * time.Millisecond // literal, NOT nudgeSettle — see above

	if nudgeMaxAttempts != wantAttempts {
		t.Fatalf("nudgeMaxAttempts is %d, this test pins %d. Changing it is a behaviour change: "+
			"update receipt_watch.go's deadline derivation in the same package of work.",
			nudgeMaxAttempts, wantAttempts)
	}
	if nudgeSettle != wantSettle {
		t.Fatalf("nudgeSettle is %v, this test pins %v. Same reason as nudgeMaxAttempts.",
			nudgeSettle, wantSettle)
	}

	// The rhythm, not just the count: a loop that fires 30 Enters in microseconds is
	// materially the single-shot bug. Record every sleep the loop actually asks for.
	var slept []time.Duration
	r := &nudgeCountingRunner{}
	tmuxDeliverNudge(r, func(d time.Duration) { slept = append(slept, d) }, "sock", "member-x", defaultNudge)

	if r.enterCount != wantAttempts {
		t.Fatalf("the loop must run exactly %d bounded attempts with no early exit, got %d", wantAttempts, r.enterCount)
	}
	if r.pasteCount != 1 {
		t.Fatalf("expected a SINGLE paste (paste-once + Enter-retry), got %d", r.pasteCount)
	}
	if len(slept) != wantAttempts {
		t.Fatalf("the loop must settle between attempts: expected %d sleeps, got %d. "+
			"Deleting the settle turns 30 attempts into one burst, which is the bug this loop exists to avoid.",
			wantAttempts, len(slept))
	}
	for i, d := range slept {
		if d != wantSettle {
			t.Fatalf("sleep #%d was %v, expected %v", i, d, wantSettle)
		}
	}
}

// TestBuildSpawnDeps_NudgeClockIsRealAndWaits (T-82, from the independent review's third mutant):
// the layer above. tmuxDeliverNudge takes its clock as a seam. ⚠️ THE SENTENCE THAT
// USED TO BE HERE IS NOW FALSE and is corrected rather than deleted, because it is
// the reasoning the rest of this test rests on: it said the nil fallback is a NO-OP
// that "exists for tests", so a production call site passing nil would silently turn
// the 30-second settle into microseconds. That WAS true when this test was written
// and is why the test exists. It is not true now — nudgeClock substitutes the REAL
// time.Sleep for a nil clock, so passing nil paces for real instead of skipping.
// This test still earns its place: it pins that buildSpawnDeps WIRES the clock, and
// a wired clock is what keeps the fallback from ever being the thing in charge.
//
// This is the fourth layer of a family this repo has been holed at repeatedly:
// function → call site → what the call site passes → the seam's identity. The
// review made this exact edit (d.Sleep → nil at the call site) and the package
// stayed green.
//
// It asks the PRODUCTION builder (buildSpawnDeps, transport.go) for its answer
// rather than a struct assembled here, because a test that builds its own SpawnDeps
// is guarding a shape, not a wiring.
//
// DEFEATED BY: dropping or nil-ing `Sleep:` in buildSpawnDeps, or wiring it to
// something that does not actually wait.
func TestBuildSpawnDeps_NudgeClockIsRealAndWaits(t *testing.T) {
	deps := buildSpawnDeps(Config{Base: "https://station.example", Token: "t", ID: "m-x"},
		func(string) string { return "" }, &recordingRunner{}, "sock", "")

	if deps.Sleep == nil {
		t.Fatal("buildSpawnDeps left Sleep nil. Production must WIRE the clock rather than lean " +
			"on tmuxDeliverNudge's nil fallback: the fallback paces for real (it is a fail-safe, " +
			"not a no-op), so a nil here is not a correctness bug any more — but it means the one " +
			"place that decides pacing is a fallback nobody chose, which is how the field went " +
			"unnoticed the first time.")
	}

	// Positive control: it is not enough that the field is non-nil — it must be a
	// clock. A stub that returns immediately would satisfy a nil check and defeat
	// the settle just as completely.
	start := time.Now()
	deps.Sleep(5 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 2*time.Millisecond {
		t.Fatalf("positive control: deps.Sleep(5ms) returned after %v — it is not a real clock, "+
			"so the settle between Enter presses does not happen in production", elapsed)
	}
}

// TestStart_PassesItsClockToTheNudge (T-82) is the layer BETWEEN the two tests
// above, and it is here because writing them first was not enough — the mutant that
// proves it was run:
//
//	buildSpawnDeps has Sleep: time.Sleep      → guarded by the test above
//	tmuxDeliverNudge settles between attempts → guarded by the test above that
//	start() HANDS ITS OWN Sleep TO THE NUDGE  → was guarded by NOTHING
//
// Editing the single call site to `tmuxDeliverNudge(d.Runner, nil, …)` left every
// one of those tests green, because at the time tmuxDeliverNudge's nil fallback WAS
// "no wait" and each of the other two tests supplies or inspects its own clock. A
// field can be correctly built and correctly consumed while the one line joining them
// drops it.
//
// ⚠️ The nil arm is no longer "no wait" (nudgeClock returns the real time.Sleep), so
// that exact mutant now costs 30 real seconds instead of corrupting behaviour. The
// LESSON above is unchanged and is why this test stays: the seam between a correctly
// built field and its correct consumer had no guard, and the fix for one layer is
// where the next layer's hole gets made.
//
// DEFEATED BY: passing anything other than d.Sleep at the tmuxDeliverNudge call
// site in start().
func TestStart_PassesItsClockToTheNudge(t *testing.T) {
	hasKey := "tmux -L officraft has-session -t member-alice"
	pidKey := "tmux -L officraft display-message -p -t member-alice #{pane_pid}"
	run := &recRunner{
		out: map[string]string{pidKey: "4242\n"},
		err: map[string]error{hasKey: errAbsent()},
	}
	deps := newStartDeps(t, run, map[string]string{})

	var slept []time.Duration
	deps.Sleep = func(d time.Duration) { slept = append(slept, d) }

	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice", Model: fxModel})
	if !out.OK {
		t.Fatalf("positive control: the spawn itself failed (%+v), so this guard proved nothing", out)
	}

	if len(slept) != nudgeMaxAttempts {
		t.Fatalf("start() must hand ITS OWN clock to the boot nudge: expected %d settles, saw %d.\n"+
			"Zero means the call site passed nil (or some other clock), and tmuxDeliverNudge's nil\n"+
			"fallback is a TEST default that does not wait — so production fires every Enter in\n"+
			"microseconds, which is materially the single-shot Enter this loop exists to avoid.",
			nudgeMaxAttempts, len(slept))
	}
	for i, d := range slept {
		if d != nudgeSettle {
			t.Fatalf("settle #%d was %v, expected nudgeSettle %v", i, d, nudgeSettle)
		}
	}
}

// TestTmuxDeliverNudge_NeverReadsTheAgentsScreen (T-82) is the guard the ticket asks
// for by name: if anyone puts a read of the agent's own screen back on this path,
// something must say so.
//
// It asserts on the ARGV the nudge actually issued, not on a string in the source,
// because the defect being guarded is not "the word capture-pane appears" — it is
// "this loop decides something by reading a rendered screen". A future rewrite that
// scrapes the pane through some other tmux verb would still have to issue a command
// that reads it, and the denominator here is every command issued.
//
// ⚠️ WHAT THIS DOES NOT COVER, stated because a blacklist always looks broader than
// it is: tmux has more ways to read a pane than the verbs listed below —
// `pipe-pane`, `save-buffer`, `list-panes -F`, `display -p` among them. The list is
// the ones a rewrite is likely to reach for, not a proof that no read is possible.
// The load-bearing guard against this class is the AUTHORITY, not this list: success
// is decided by the server's receipt, and this test is a tripwire on the way back.
//
// DEFEATED BY: any re-added pane read inside tmuxDeliverNudge that uses one of the
// listed verbs.
func TestTmuxDeliverNudge_NeverReadsTheAgentsScreen(t *testing.T) {
	r := &recordingRunner{}
	tmuxDeliverNudge(r, func(time.Duration) {}, "sock", "member-x", defaultNudge)
	if len(r.argv) == 0 {
		t.Fatal("positive control: the nudge issued no commands at all, so this guard proved nothing")
	}
	for _, argv := range r.argv {
		for _, forbidden := range []string{
			"capture-pane", "display-message", "show-buffer",
			"pipe-pane", "save-buffer", "list-panes", "display -p",
		} {
			if strings.Contains(argv, forbidden) {
				t.Fatalf("the boot nudge read the agent's own screen (%q in %q).\n"+
					"Success on this path is decided by ONE authority \u2014 whether the server received that\n"+
					"member's report_waking inside StartTimeout \u2014 and it is deliberately not decided here.\n"+
					"What T-82 removed was a verdict scraped off a RENDERED SCREEN. Note the mechanism,\n"+
					"because the obvious reading is wrong: that status line is OURS \u2014 buildStatuslineSettings\n"+
					"points the agent's statusLine at our own `ocagent context-report`, and T-51a8 (our ticket)\n"+
					"changed its layout so the check went permanently false. The lesson is not \"do not read\n"+
					"someone else's screen\"; it is that two of our own modules were coupled through an\n"+
					"uncontracted string with nothing guarding it.", forbidden, argv)
			}
		}
	}
}

// recordingRunner records every argv the caller issues and answers everything with
// an empty success. It is the fake for asserting on WHAT WAS ASKED; nudgeCountingRunner
// counts a few specific verbs and discards the rest, so it cannot answer "was this
// ever issued at all". (Before T-82 this comment said the other fake had to be
// avoided because it ANSWERS capture-pane — true then, false now: that arm was
// deleted in the same change, so neither fake can serve a pane read.)
type recordingRunner struct{ argv []string }

func (r *recordingRunner) Run(name string, args ...string) (string, error) {
	r.argv = append(r.argv, strings.Join(append([]string{name}, args...), " "))
	return "", nil
}

// TestTmuxDeliverNudge_NilSleepSafe: a nil Sleep seam must not panic, and must
// still run the whole bounded attempt count (since T-82 there is no early exit).
//
// ⚠️ IT NO LONGER PASSES nil, and that is not a workaround — it is the point. As of
// T-82 a nil clock falls back to time.Sleep (fail-safe, mirroring kill.go), so a
// test passing nil here would really wait 30 seconds. What the test is actually
// about is "an unwired clock does not panic and does not shorten the loop", which
// a caller-supplied no-op states without buying a 30-second test.
//
// The count is a LITERAL for the same reason as TestTmuxDeliverNudge_AlwaysRunsBoundedAttempts:
// `!= nudgeMaxAttempts` is a constant compared against itself and has no
// discrimination against a change to the constant.
func TestTmuxDeliverNudge_NilSleepSafe(t *testing.T) {
	const wantAttempts = 30 // literal, NOT nudgeMaxAttempts — see above
	r := &nudgeCountingRunner{}
	tmuxDeliverNudge(r, func(time.Duration) {}, "sock", "member-x", defaultNudge)
	if r.enterCount != wantAttempts {
		t.Fatalf("an unwired clock must still run the bounded %d attempts, got %d", wantAttempts, r.enterCount)
	}
}

// runewidth is a deliberately crude display-width count: CJK / fullwidth runes
// take two columns where everything else takes one. It exists only so the
// refusal-length guard in TestStart_NoClaudeBin measures the mixed zh/en string
// the way the member panel actually renders it, instead of counting a 4-rune
// 執行環境 as 4 columns.
func runewidth(s string) int {
	width := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
			r >= 0x2E80 && r <= 0xA4CF, // CJK radicals … Yi
			r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
			r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
			r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
			r >= 0xFFE0 && r <= 0xFFE6:
			width += 2
		default:
			width++
		}
	}
	return width
}

// sixSourceUnsetSummary is what probeClaudeCreds returns on a signed-out darwin
// host with HOME set: cred_file, keychain, then every claudeCredEnvKeys entry,
// each rendered "<name>=unset" and space-joined. Kept here (rather than a short
// stand-in) so the width assertions measure the string an owner actually reads.
const sixSourceUnsetSummary = "cred_file=unset keychain=unset " +
	"ANTHROPIC_API_KEY=unset ANTHROPIC_AUTH_TOKEN=unset " +
	"CLAUDE_CODE_USE_BEDROCK=unset CLAUDE_CODE_USE_VERTEX=unset"
