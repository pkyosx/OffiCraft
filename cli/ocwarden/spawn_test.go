package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
//	base            = http://127.0.0.1:8770
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
	fxBase      = "http://127.0.0.1:8770"
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
	if !strings.Contains(got, `"type": "http"`) || !strings.Contains(got, `"url": "http://127.0.0.1:8770/api/mcp"`) {
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
		`export OC_TOKEN="$(cat ` + fxTokenFile + `)"`,
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
		{"http://127.0.0.1:8770", "http://127.0.0.1:8770"}, // :,/,. all safe
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
		[][2]string{{"OC_EFFORT", "medium"}})
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
	// stale link cleared first (idempotent re-spawn).
	if len(removed) != 1 || removed[0] != fxOcAgent {
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

	// Sibling exists → use it (the self-contained home-install layout).
	sibling := "/Users/seth/.officraft/warden/ocagent"
	if got := resolveOcAgentBin(homeExe, func(p string) bool { return p == sibling }, repoRoot); got != sibling {
		t.Errorf("home-install must exec the sibling ocagent, got %q want %q", got, sibling)
	}
	// No sibling on disk → fall back to the repoRoot-relative dev path.
	wantFallback := repoRoot + "/cli/ocagent/ocagent"
	if got := resolveOcAgentBin(devExe, func(string) bool { return false }, repoRoot); got != wantFallback {
		t.Errorf("dev run must fall back to repoRoot-relative ocagent, got %q want %q", got, wantFallback)
	}
	// Unresolvable executable → still yields the repoRoot fallback (no panic).
	if got := resolveOcAgentBin(func() (string, error) { return "", errString("no exe") }, func(string) bool { return true }, repoRoot); got != wantFallback {
		t.Errorf("unresolvable exe must yield the repoRoot fallback, got %q", got)
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
		[][2]string{{"OC_EFFORT", "medium"}})
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
	if !strings.HasPrefix(out.Reason, "claude_bin_unresolved:") {
		t.Errorf("unresolved claude bin must carry a claude_bin_unresolved reason, got %q", out.Reason)
	}
	if len(run.calls) != 0 {
		t.Errorf("must not touch tmux when claude bin is unresolved, got %v", run.calls)
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

// seqCaptureRunner drives the boot-nudge settle/retry: capture-pane returns a
// programmed sequence (the last value repeats), and Enter/paste presses are counted.
type seqCaptureRunner struct {
	captures   []string
	capIdx     int
	enterCount int
	pasteCount int
}

func (r *seqCaptureRunner) Run(name string, args ...string) (string, error) {
	argv := strings.Join(append([]string{name}, args...), " ")
	switch {
	case strings.Contains(argv, "capture-pane"):
		v := ""
		if n := len(r.captures); n > 0 {
			if r.capIdx < n {
				v = r.captures[r.capIdx]
			} else {
				v = r.captures[n-1]
			}
		}
		r.capIdx++
		return v, nil
	case strings.Contains(argv, "send-keys") && strings.Contains(argv, "Enter"):
		r.enterCount++
	case strings.Contains(argv, "paste-buffer"):
		r.pasteCount++
	}
	return "", nil
}

// TestTmuxDeliverNudge_RetriesUntilCommitted: the context gauge shows "?%" (not yet
// submitted) for the first two capture reads, then flips numeric — expect exactly 3
// Enter presses (2 retries + the committing one) and a SINGLE paste, then stop.
func TestTmuxDeliverNudge_RetriesUntilCommitted(t *testing.T) {
	r := &seqCaptureRunner{captures: []string{"🧠 ?% context", "🧠 ?% context", "🧠 5% context"}}
	tmuxDeliverNudge(r, func(time.Duration) {}, "sock", "member-x", defaultNudge)
	if r.enterCount != 3 {
		t.Fatalf("expected 3 Enter presses (2 unsubmitted + 1 committed), got %d", r.enterCount)
	}
	if r.pasteCount != 1 {
		t.Fatalf("expected a SINGLE paste (paste-once + Enter-retry), got %d", r.pasteCount)
	}
}

// TestTmuxDeliverNudge_StopsAtMaxAttempts: a nudge that never commits (gauge stuck at
// "?%") is bounded at nudgeMaxAttempts — it must never spin forever on a wedged TUI.
func TestTmuxDeliverNudge_StopsAtMaxAttempts(t *testing.T) {
	r := &seqCaptureRunner{captures: []string{"🧠 ?% context"}}
	tmuxDeliverNudge(r, func(time.Duration) {}, "sock", "member-x", defaultNudge)
	if r.enterCount != nudgeMaxAttempts {
		t.Fatalf("expected bounded %d attempts, got %d", nudgeMaxAttempts, r.enterCount)
	}
}

// TestNudgeSubmitted_ContextGauge: a numeric context gauge reads as submitted; the
// "?%" gauge (or no gauge yet, during cold init) does not — the positive signal that
// avoids the cold-init false-positive.
func TestNudgeSubmitted_ContextGauge(t *testing.T) {
	if !nudgeSubmitted("🧠 5% context · /rc active") {
		t.Fatal("a numeric context gauge must read as submitted")
	}
	if nudgeSubmitted("🧠 ?% context · /rc active") {
		t.Fatal("the ?% gauge must read as NOT submitted")
	}
	if nudgeSubmitted("welcome screen, no status bar yet") {
		t.Fatal("no gauge rendered yet must read as NOT submitted")
	}
}

// TestTmuxDeliverNudge_NilSleepSafe: a nil Sleep seam must not panic (test default
// is a no-op wait); commits on the first numeric read → 1 Enter.
func TestTmuxDeliverNudge_NilSleepSafe(t *testing.T) {
	r := &seqCaptureRunner{captures: []string{"🧠 7% context"}}
	tmuxDeliverNudge(r, nil, "sock", "member-x", defaultNudge)
	if r.enterCount != 1 {
		t.Fatalf("committed on first read → 1 Enter, got %d", r.enterCount)
	}
}
