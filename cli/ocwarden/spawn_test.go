// Skeleton generated from cli/ocwarden/spawn.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestShellQuote(t *testing.T) {
	t.Skip("TODO: shellQuote is the exact analogue of shlex.quote: \"\" → ”; a fully-safe string is returned verbatim; otherwise it is single-quoted with embedded ' → '\"'\"'.")
}

func TestJsonStr(t *testing.T) {
	t.Skip("TODO: jsonStr JSON-encodes one string with HTML escaping OFF, matching Python's json.dumps(..., ensure_ascii=False) (non-ASCII preserved, <>& NOT escaped).")
}

func TestBuildMCPConfig(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- pure builders (no I/O) — golden-file对賬 against agent/spawn.py.")
}

func TestBuildStatuslineSettings(t *testing.T) {
	t.Skip("TODO: buildStatuslineSettings is the port of build_statusline_settings: the Claude Code settings.json wiring the statusLine to the context reporter (json.dumps indent=2 + a trailing newline).")
}

func TestBuildAppendSystemPrompt(t *testing.T) {
	t.Skip("TODO: buildAppendSystemPrompt is the port of build_append_system_prompt: the TRUSTED boot channel — a MINIMAL pointer, NOT the boot SOP itself.")
}

func TestOcAgentSymlinkTarget(t *testing.T) {
	t.Skip("TODO: ocAgentSymlinkTarget picks the ABSOLUTE binary the workdir `ocagent` symlink points at.")
}

func TestOcAgentTarget(t *testing.T) {
	t.Skip("TODO: ocAgentTarget is the per-spawn resolution seam (T-81).")
}

func TestBuildLaunchCommand(t *testing.T) {
	t.Skip("TODO: buildLaunchCommand is the port of build_launch_command: the one shell line tmux new-session runs.")
}

func TestBuildLaunchCommandWithEnv(t *testing.T) {
	t.Skip("TODO: buildLaunchCommandWithEnv is buildLaunchCommand plus optional EXTRA env pairs appended after the frozen OC_* four (namespaced instances export OC_AGENT_HOME here — R8: without it two instances' same-named agents share one sse-cursor/context_report.stamp dir and trample each other).")
}

func TestTmuxNewSession(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- tmux boot helpers (over the CmdRunner seam) — mirror AgentSpawner.tmux_*.")
}

func TestNudgeClock(t *testing.T) {
	t.Skip("TODO: tmuxDeliverNudge delivers the neutral boot nudge ATOMICALLY via a tmux buffer (never send-keys -l, which drops multibyte under a busy TUI), then presses Enter to commit the first user-turn.")
}

func TestTmuxDeliverNudge(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestDefaultAgentHome(t *testing.T) {
	t.Skip("TODO: defaultAgentHome resolves the per-agent state base: OC_AGENT_HOME overrides, else ~/.officraft[-<ns>]/agents (mirrors AgentSpawner.home; the OC_NAMESPACE instance key moves it under the namespaced root — byte-identical to the historical ~/.officraft/agents for the empty namespace).")
}

func TestDefaultAgentEnvFile(t *testing.T) {
	t.Skip("TODO: defaultAgentEnvFile resolves the owner's agent env file: <officraft root>/env, namespace-aware exactly like defaultAgentHome (a namespaced instance reads its OWN env file, so two instances cannot cross-contaminate credentials).")
}

func TestDefaultCaptureEnv(t *testing.T) {
	t.Skip("TODO: defaultCaptureEnv resolves the production CaptureEnv seam.")
}

func TestInteractiveEnvPairs(t *testing.T) {
	t.Skip("TODO: interactiveEnvPairs is the FAIL-SAFE wrapper around the CaptureEnv seam.")
}

func TestOsWriteFile(t *testing.T) {
	t.Skip("TODO: osWriteFile is the real file-write seam (write-then-chmod, mirroring write_file).")
}

func TestPretrustWorkdir(t *testing.T) {
	t.Skip("TODO: --------------------------------------------------------------------------- pretrust — mark the launch workdir trusted in ~/.claude.json before launch.")
}

func TestAtomicWriteFile(t *testing.T) {
	t.Skip("TODO: atomicWriteFile writes data to path via a same-dir temp file + rename, so a reader (or a crash) never sees a half-written file.")
}

func TestDefaultClaudeJSONPath(t *testing.T) {
	t.Skip("TODO: defaultClaudeJSONPath resolves the claude.json to pre-trust: OC_CLAUDE_JSON overrides (a PoC safety valve so a live run can be pointed at a throwaway file), else ~/.claude.json (mirrors pretrust_launch_cwd's os.path.expanduser default).")
}

func TestWithPerSpawn(t *testing.T) {
	t.Skip("TODO: withPerSpawn returns a copy of d with ONLY the two per-spawn seams rebound — the ones that cannot be built until StartParams names a member, because they need that member's launch workdir.")
}

func TestStart(t *testing.T) {
	t.Skip("TODO: start EXECUTES one server-downpushed spawn.")
}
