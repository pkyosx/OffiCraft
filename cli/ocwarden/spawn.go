// Phase 2 "hands": the start/spawn EXECUTION mechanism of the stateless warden.
//
// v2 is a server→warden PUSH model: the SERVER decides who/where/whether to spawn
// (placement, over-spawn guard, kind gate, reconcile — all live server-side). This
// file is PURELY the executor: given the server-downpushed start parameters, it
// assembles the launch command + .mcp.json + persona (trusted file channel) and
// boots ONE fresh agent as a visible tmux session, reusing the shared CmdRunner
// seam, tmuxSocket and memberSessionName. The warden reports NO presence (the server
// projects it from its own SSE view), so the spawn emits no waking/online report.
//
// It is a faithful Go port of the pure builders + tmux boot steps of the Python
// origin (agent/spawn.py build_mcp_config / build_append_system_prompt /
// build_launch_command / build_statusline_settings / tmux_new_session, and
// reconcile.py TmuxSpawnPort.spawn). The launch command and .mcp.json are
// golden-file pinned (see spawn_test.go) — a single divergent flag/value would
// make the spawned claude silently lose its MCP surface or persona. One flagged
// deviation from the Python origin: OC_TOKEN rides a 0600 workdir token file
// read at exec time, never the argv (see buildLaunchCommand).
//
// DELIBERATELY NOT here (server owns these in v2, or later warden phases):
//   - roster poll / placement / over-spawn guard / is_assistant_kind spawn
//     decision / reconcile loop / cross-tick state  → server (T2.1)
//   - kill / force-kill                             → Phase 3
//   - token minting (/api/bootstrap)                → server (A4: mint归server)
//   - inbound RPC channel wiring                    → Phase 4
//   - pretrust_launch_cwd (Pretrust seam below)     → Phase 4 wiring
//     Marks the workdir trusted in ~/.claude.json so claude's "trust this folder?"
//     dialog can't intercept and eat the boot nudge (LOAD-BEARING).
//   - STAGE-B bounded land-then-clear settle/retry  → Phase 4
//     A TUI-state robustness layer needing pane-capture (a CmdRunner stdin/capture
//     capability this seam lacks); Phase 2 delivers a single-shot nudge.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultNudge is the NEUTRAL boot nudge (spawn.DEFAULT_NUDGE) delivered via a
	// tmux buffer paste to commit the fresh agent's first user-turn.
	defaultNudge = "開始。"
	// nudgeMaxAttempts / nudgeSettle bound the boot-nudge Enter-retry (STAGE-B). A
	// cold claude REPL can take several seconds to accept input (rendering the
	// welcome screen, dismissing startup notices), so we retry the Enter until the
	// context gauge confirms submission. ~30×1s ≈ 30s covers a slow cold start yet
	// stays COMFORTABLY under the reconcile start_timeout (WAKING_TTL_SECS=90s) so a
	// slow-but-successful boot is never miscounted as a start-failure → circuit trip.
	nudgeMaxAttempts = 30
	nudgeSettle      = 1 * time.Second
	// paneCols/paneRows: the FIXED wide pane geometry (AgentSpawner.PANE_COLS/ROWS)
	// so the spawned TUI's wrap width is deterministic.
	paneCols = 160
	paneRows = 50
)

// ---------------------------------------------------------------------------
// start RPC surface (server→warden PUSH). The server decides the placement and
// hands DOWN these parameters; the warden only executes them.
// ---------------------------------------------------------------------------

// StartParams is the server-downpushed start(...) payload. token/role/task_type/
// model/session_name are server-owned decisions; the warden never mints a token
// nor picks a placement (A4/T2.1). PersonaContext is the pre-composed persona the
// server hands in as plain text (the warden does NO server I/O to fetch it).
type StartParams struct {
	MemberID       string
	PersonaContext string
	MemberToken    string
	Role           string
	TaskType       string // colours the boot prompt upstream; carried for parity
	Model          string
	// Effort is the member's owner-set reasoning-effort launch intent
	// (low/medium/high, from member.effort server-side). Empty ⇒ the historic
	// "medium" default, keeping an old frame's launch line byte-identical.
	Effort      string
	SessionName string
}

// SpawnOutcome is the start(...) return: {ok, session_id, pid} (mirrors the
// Python SpawnOutcome the reconcile port returns). In the async reconcile model
// (wade-ruled): OK means the spawn was EXECUTED (tmux session launched), NOT that
// the agent confirmed boot. Boot success is judged SERVER-side by watching presence
// flip waking→online with a live pid — the warden never claims boot itself.
type SpawnOutcome struct {
	OK        bool
	SessionID string
	PID       string
	// Reason is the STRUCTURED cause when OK is false — surfaced by the
	// dispatcher so a refused spawn is VISIBLE in the warden log AND carried to
	// the server on the command_result receipt (folded onto member.last_op_reason,
	// shown in the FE 最近操作 block). Format: "<code>: <detail>" where <code> is
	// one of the CLOSED set start() emits — claude_bin_unresolved /
	// session_already_exists / mkdir_failed / write_file_failed / symlink_failed /
	// pretrust_failed / spawn_exec_failed. Every OK=false path sets it (the old
	// bare three-in-one "already-running / workdir / mkdir" ambiguity is gone —
	// the 2026-07-13 Mira incident showed the owner a reason-less ✗ start).
	// Empty on OK.
	Reason string
}

// ---------------------------------------------------------------------------
// shell quoting (byte-for-byte port of Python shlex.quote) — LOAD-BEARING: the
// launch command golden equivalence depends on this matching exactly.
// ---------------------------------------------------------------------------

// shlexUnsafe mirrors shlex._find_unsafe = re.compile(r'[^\w@%+=:,./-]', re.ASCII).
// Go's \w is ASCII [0-9A-Za-z_], matching Python's re.ASCII flag.
var shlexUnsafe = regexp.MustCompile(`[^\w@%+=:,./-]`)

// shellQuote is the exact analogue of shlex.quote: "" → ”; a fully-safe string is
// returned verbatim; otherwise it is single-quoted with embedded ' → '"'"'.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !shlexUnsafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// jsonStr JSON-encodes one string with HTML escaping OFF, matching Python's
// json.dumps(..., ensure_ascii=False) (non-ASCII preserved, <>& NOT escaped).
func jsonStr(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

// ---------------------------------------------------------------------------
// pure builders (no I/O) — golden-file对賬 against agent/spawn.py.
// ---------------------------------------------------------------------------

// buildMCPConfig is the byte-for-byte port of build_mcp_config: ONE officraft
// HTTP MCP server at {base}/api/mcp, token carried in an "Authorization: Bearer"
// HEADER (never a ?token= query — the loopback only forwards the header), emitted
// as json.dumps(ensure_ascii=False, indent=2). Ordered key emission is manual
// because Go's json.Marshal sorts map keys (which would break equivalence). A
// falsy token omits the headers block (still valid config, token-less case).
func buildMCPConfig(base, token string) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString("  \"mcpServers\": {\n")
	sb.WriteString("    \"officraft\": {\n")
	sb.WriteString("      \"type\": \"http\",\n")
	sb.WriteString("      \"url\": " + jsonStr(base+"/api/mcp"))
	if token != "" {
		sb.WriteString(",\n")
		sb.WriteString("      \"headers\": {\n")
		sb.WriteString("        \"Authorization\": " + jsonStr("Bearer "+token) + "\n")
		sb.WriteString("      }\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString("    }\n")
	sb.WriteString("  }\n")
	sb.WriteString("}")
	return sb.String()
}

// buildStatuslineSettings is the port of build_statusline_settings: the Claude
// Code settings.json wiring the statusLine to the context reporter (json.dumps
// indent=2 + a trailing newline).
func buildStatuslineSettings() string {
	return "{\n" +
		"  \"statusLine\": {\n" +
		"    \"type\": \"command\",\n" +
		"    \"command\": \"ocagent context-report\"\n" +
		"  }\n" +
		"}\n"
}

// buildAppendSystemPrompt is the port of build_append_system_prompt: the TRUSTED
// boot channel — a MINIMAL pointer, NOT the boot SOP itself. The persona rides this
// channel (a durable local file), NEVER the command line (avoids leak / arg-length
// limits).
//
// Single source of truth for the ordered boot procedure is now the office
// global_context.md §5.1 (pre-fetched by the launcher into personaFile alongside the
// persona / role doc). So this append-prompt no longer re-spells the boot steps
// (that second hardcode was the cross-language drift risk this step removes); it just
// tells the fresh agent WHO it is and to LOAD personaFile and follow its 開機程序
// section step by step. The full ordered SOP (report_waking → resume_summary →
// ocagent listen, SSE-connect⟺ready) lives in personaFile, not here — loading
// personaFile is THIS prompt's own instruction, not a boot-sequence step. base is
// no longer needed (the /api/events URL moved into the SOP text).
func buildAppendSystemPrompt(agentID, role, personaFile string) string {
	prompt := fmt.Sprintf("你是 %s(role=%s)。你的完整身分、操作準則與開機程序都由 "+
		"launcher 預抓在本地檔 %s;第一步用 Read 工具載入它,並照裡面"+
		"「開機程序」段逐步執行(做事/治理走 officraft MCP 工具,聽事件走 "+
		"ocagent listen)。用繁體中文回。",
		agentID, role, personaFile)
	return prompt
}

// ocAgentSymlinkTarget picks the ABSOLUTE binary the workdir `ocagent` symlink points
// at. The boot prompt tells the fresh agent to run a BARE `ocagent` (listen /
// context-report); buildLaunchCommand prepends the workdir to PATH, so a SYMLINK named
// `ocagent` in the workdir makes that bare name resolve to it — shadowing any UNRELATED
// ocagent on the host — and exec follows the link to the COMPILED golang ocagent (the
// agent host needs NO python at all, unlike the python origin's `python -m
// agent.oc_agent`). This is the case-B fix for the header's "ocagent binary publish
// into the workdir" gap: a clean spawn without it boots a DEAF agent (no listen →
// never online).
//
// The target is ocAgentBin when set (resolveOcAgentBin's answer: the ocwarden SIBLING
// $HOME/.officraft/warden/ocagent once home-installed, guaranteed present by install's
// download step), else it FALLS BACK to the repoRoot-relative <repoRoot>/cli/ocagent/ocagent
// (dev / in-tree, no home install). The sibling path is LOAD-BEARING once the warden is
// home-installed: the durable ocwarden runs from $HOME/.officraft/warden, so
// resolveRepoRoot's os.Executable walk lands on $HOME (not the real checkout) and the
// relative fallback would point at a nonexistent binary — a deaf-on-boot agent.
//
// Why a SYMLINK (not a hardlink or a cd-wrapper script): warden self-update swaps the
// binary via ATOMIC RENAME (temp → rename over $HOME/.officraft/warden/ocagent). A symlink
// resolves by PATH, so post-rename it still points at the live path → every agent's next
// exec picks up the new binary transparently. A hardlink pins an INODE, so the pre-rename
// hardlink keeps the STALE inode after the swap → the update never reaches the agent. The
// old cd-into-repoRoot wrapper is dropped: the golang ocagent is self-contained and needs
// no repo cwd, and the symlink is PATH-resolved + exec'd directly with no shell hop.
func ocAgentSymlinkTarget(repoRoot, ocAgentBin string) string {
	if ocAgentBin != "" {
		return ocAgentBin
	}
	return filepath.Join(repoRoot, "cli", "ocagent", "ocagent")
}

// buildLaunchCommand is the port of build_launch_command: the one shell line tmux
// new-session runs. Flags/order are FROZEN — a divergence makes the spawned claude
// silently lose MCP (--mcp-config) or persona
// (--append-system-prompt). OC_* ride the env export so the agent runs under its
// OWN identity; the workdir is prepended to PATH so a bare `ocagent` resolves.
// --model is emitted only when set (an unset model keeps the line byte-identical to
// the pre-model version); --settings only when settings_path is given.
//
// Deviation from the Python origin (flagged): the origin exported the member token
// LITERALLY (OC_TOKEN=<jwt>), leaking it machine-wide via the tmux argv (`ps` shows
// the full new-session command line). Here the line carries only tokenFile — the
// 0600 workdir token file start() writes — and defers the read to the spawned
// shell: OC_TOKEN="$(/bin/cat <tokenFile>)" (absolute — see the builder body,
// T-426d G-1). Same pattern as the warden's own
// exec-warden tokfile (main.go readTokfile).
//
// The workdir is prepended to PATH so a bare `ocagent` resolves — the ocagent
// binary itself is published into the workdir by Phase 4 wiring (the golang
// ocagent, agent-cli's T2.4 artifact), NOT by this pure builder. Until that
// wiring lands, a spawned agent on a clean host has no ocagent on PATH.
func buildLaunchCommand(claudeBin, workdir, mcpConfigPath, appendSys, tokenFile, agentID, base, session, socket, model, effort, settingsPath string) string {
	return buildLaunchCommandWithEnv(claudeBin, workdir, mcpConfigPath, appendSys,
		tokenFile, agentID, base, session, socket, model, effort, settingsPath, nil, "")
}

// buildLaunchCommandWithEnv is buildLaunchCommand plus optional EXTRA env pairs
// appended after the frozen OC_* four (namespaced instances export
// OC_AGENT_HOME here — R8: without it two instances' same-named agents share
// one sse-cursor/context_report.stamp dir and trample each other). nil/empty
// extra keeps the line byte-identical to the historical output, which is the
// entire zero-diff guarantee for the empty namespace.
//
// envRendered (T-426d) is the workdir 0600 file holding the owner's agent env
// vars, already PARSED AND VALIDATED by loadAgentEnv and re-rendered as pure
// `export K='v'` lines (see agentenv.go). Empty ⇒ nothing is emitted and the
// line stays byte-identical to the pre-T-426d output. Non-empty ⇒ it is sourced
// FIRST, before the OC_* exports, for two reasons: (a) the OC_* names this line
// actually exports then override anything the file set under those SAME names —
// note this is a positional override of those specific names only, NOT a second
// enforcement of the OC_* prefix rule (that rule lives solely in the parser),
// and (b) the launch line's own `export PATH=<workdir>:"$PATH"` then composes ON TOP of an
// env-file PATH rather than being erased by it. The source is guarded by a
// `[ -f ]` test so a file deleted between render and exec degrades to "no extra
// env" instead of a shell error on the agent's very first line.
func buildLaunchCommandWithEnv(claudeBin, workdir, mcpConfigPath, appendSys, tokenFile, agentID, base, session, socket, model, effort, settingsPath string, extraEnv [][2]string, envRendered string) string {
	cd := "cd " + shellQuote(workdir) + "; "
	if envRendered != "" {
		cd += "[ -f " + shellQuote(envRendered) + " ] && . " + shellQuote(envRendered) + "; "
	}
	pairs := [][2]string{
		{"OC_BASE", base},
		{"OC_SESSION", session},
		{"OC_TMUX_SOCKET", socket},
	}
	pairs = append(pairs, extraEnv...)
	kvs := make([]string, 0, len(pairs)+1)
	// OC_TOKEN reads the 0600 token file at exec time — only the PATH rides the
	// argv, never the token value (the tmux command line is visible machine-wide).
	//
	// ABSOLUTE /bin/cat, NOT a bare `cat` (T-426d G-1, measured — not reasoned).
	// The env file is sourced EARLIER in this same line, so by the time this
	// substitution runs, PATH is whatever the OWNER's env file left it as. An
	// owner who writes `PATH=/opt/homebrew/sbin` (expecting it to APPEND, which
	// it does not) removes /bin from PATH, a bare `cat` then fails to resolve,
	// and OC_TOKEN silently becomes EMPTY — the agent boots and can never
	// authenticate. Verified in real zsh and sh: bare `cat` yields TOKEN=[],
	// /bin/cat yields the token even with PATH set to the empty string.
	//
	// This is a DELIBERATE divergence from the Python origin's golden output
	// (goldens updated to match). It is the only layer of this defence that does
	// not depend on the owner filling the env file in correctly, which is
	// exactly why it has to exist: docs and warnings are advisory, this is not.
	kvs = append(kvs, `OC_TOKEN="$(/bin/cat `+shellQuote(tokenFile)+`)"`)
	for _, p := range pairs {
		kvs = append(kvs, p[0]+"="+shellQuote(p[1]))
	}
	exports := "export " + strings.Join(kvs, " ") + "; "
	exports += "export PATH=" + shellQuote(workdir) + `:"$PATH"; `
	// The server-downpushed effort (member.effort — the owner's launch intent,
	// M2-2). Empty defaults to the historic pinned "medium" (CLI default is
	// high, the main token-cost driver), so an effort-less frame keeps the line
	// byte-identical to the pre-effort version.
	if effort == "" {
		effort = "medium"
	}
	parts := []string{
		shellQuote(claudeBin),
		"--dangerously-skip-permissions",
		// AskUserQuestion is claude's BUILT-IN interactive-menu tool —
		// --dangerously-skip-permissions does NOT gate it. A spawned agent is a
		// HEADLESS tmux session nobody watches, so an AskUserQuestion menu blocks
		// the session forever (2026-07-13 Mira incident: a member popped the menu
		// to ask the owner, who only sees the cockpit — the question died in tmux).
		// Denied at the harness so the tool CANNOT be invoked; the seeds teach the
		// why (headless ⇒ open a gate / post_chat instead). Both member AND worker
		// spawns flow through this builder, so one deny covers both paths.
		"--disallowedTools",
		"AskUserQuestion",
		"--mcp-config",
		shellQuote(mcpConfigPath),
		"--effort",
		shellQuote(effort),
		"--append-system-prompt",
		shellQuote(appendSys),
	}
	if model != "" {
		parts = append(parts, "--model", shellQuote(model))
	}
	if settingsPath != "" {
		parts = append(parts, "--settings", shellQuote(settingsPath))
	}
	return cd + exports + "exec " + strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// tmux boot helpers (over the CmdRunner seam) — mirror AgentSpawner.tmux_*.
// ---------------------------------------------------------------------------

// tmuxNewSession starts a DETACHED session running command at the FIXED wide pane
// geometry, then PINs window-size to manual so a later read-only attach can't shrink
// it back (reintroducing early wrap). Mirrors AgentSpawner.tmux_new_session: the
// new-session return classifies success/failure; the set-option/resize are
// best-effort (their result is not gated on).
func tmuxNewSession(r CmdRunner, socket, session, command string) error {
	cols, rows := strconv.Itoa(paneCols), strconv.Itoa(paneRows)
	if _, err := r.Run("tmux", "-L", socket, "new-session", "-d", "-s", session, "-x", cols, "-y", rows, command); err != nil {
		return err
	}
	_, _ = r.Run("tmux", "-L", socket, "set-option", "-t", session, "window-size", "manual")
	_, _ = r.Run("tmux", "-L", socket, "resize-window", "-t", session, "-x", cols, "-y", rows)
	return nil
}

// tmuxDeliverNudge delivers the neutral boot nudge ATOMICALLY via a tmux buffer
// (never send-keys -l, which drops multibyte under a busy TUI), then presses Enter
// to commit the first user-turn. Mirrors AgentSpawner.tmux_paste + tmux_enter.
//
// Deviation from Python (flagged): the origin uses load-buffer over STDIN; the
// Phase 1 CmdRunner seam has no stdin channel, so the SHORT constant nudge is
// carried as an argv via set-buffer. paste-buffer keeps the -d -p flags and the
// bare-flag fallback exactly as the origin.
func tmuxDeliverNudge(r CmdRunner, sleep func(time.Duration), socket, session, nudge string) {
	if sleep == nil {
		sleep = func(time.Duration) {} // test default: no real wait
	}
	const buf = "oc-spawn-nudge"
	_, _ = r.Run("tmux", "-L", socket, "set-buffer", "-b", buf, nudge)
	// The paste lands reliably even into a not-yet-ready REPL (the text sits in the
	// input box), so paste ONCE. The fragile part is the Enter: a single shot loses
	// the race when claude's REPL is not input-ready (the Enter fires before the box
	// accepts it, or a startup notice eats it → the nudge sits UNSUBMITTED → the agent
	// never boots — the Phase-4 boot-death's last mile). So retry the Enter, settling
	// and CONFIRMING submission via a POSITIVE signal (nudgeSubmitted) each time.
	// Bounded well under the reconcile start_timeout so a wedged TUI can't spin here.
	if _, err := r.Run("tmux", "-L", socket, "paste-buffer", "-t", session, "-b", buf, "-d", "-p"); err != nil {
		_, _ = r.Run("tmux", "-L", socket, "paste-buffer", "-t", session, "-b", buf)
	}
	for attempt := 0; attempt < nudgeMaxAttempts; attempt++ {
		_, _ = r.Run("tmux", "-L", socket, "send-keys", "-t", session, "Enter")
		sleep(nudgeSettle)
		pane, err := r.Run("tmux", "-L", socket, "capture-pane", "-t", session, "-p")
		if err == nil && nudgeSubmitted(pane) {
			return
		}
	}
}

// nudgeSubmitted reports a POSITIVE "the boot nudge committed" signal: claude has
// processed at least one turn, so its context gauge flips from "?%" (nothing
// submitted yet) to a numeric percent. This is deliberately a positive signal — an
// absence check ("nudge no longer on the input line") FALSE-POSITIVES during cold
// init, before the input box has even rendered, and would stop the retry with the
// nudge still unsubmitted (the bug that shipped in the first STAGE-B cut).
func nudgeSubmitted(pane string) bool {
	return strings.Contains(pane, "% context") && !strings.Contains(pane, "?% context")
}

// ---------------------------------------------------------------------------
// workdir + file seams (mirror agent_workdir + AgentSpawner.write_file).
// ---------------------------------------------------------------------------

// agentWorkdir is the DURABLE per-agent work dir <home>/<id_lower> (id lowercased —
// the canonical per-agent key). Durable, NOT an ephemeral mkdtemp: a reaped tmpdir
// would take the token-bearing .mcp.json with it → the agent silently loses config.
func agentWorkdir(home, id string) string {
	return filepath.Join(home, strings.ToLower(id))
}

// defaultAgentHome resolves the per-agent state base: OC_AGENT_HOME overrides, else
// ~/.officraft[-<ns>]/agents (mirrors AgentSpawner.home; the OC_NAMESPACE
// instance key moves it under the namespaced root — byte-identical to the
// historical ~/.officraft/agents for the empty namespace). Used by the
// Phase 4 wiring; the namespace is validated upstream (realMain), so an error
// here degrades to the main-instance default.
func defaultAgentHome(env func(string) string) string {
	if h := env("OC_AGENT_HOME"); h != "" {
		return h
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "agents")
}

// defaultAgentEnvFile resolves the owner's agent env file: <officraft root>/env,
// namespace-aware exactly like defaultAgentHome (a namespaced instance reads its
// OWN env file, so two instances cannot cross-contaminate credentials). Note this
// sits at the officraft ROOT, a sibling of the agents/ dir — it is owner-authored
// configuration, not per-agent state.
//
// OC_AGENT_ENV_FILE overrides it outright, which is also how tests and the
// conformance suite point it at a fixture without touching the real ~/.officraft.
func defaultAgentEnvFile(env func(string) string) string {
	if p := env("OC_AGENT_ENV_FILE"); p != "" {
		return p
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "env")
}

// defaultCaptureEnv resolves the production CaptureEnv seam.
//
// Returns nil — inheritance OFF, launch line byte-identical to before — when
// OC_AGENT_ENV_INHERIT is set to a disabling value. That kill switch exists
// because this feature runs on EVERY spawn on the machine: if some rc file
// interaction turns out to break agents in a way nobody predicted, the owner
// needs a way back to the previous behaviour that does not require a rebuild.
//
// OC_AGENT_ENV_SHELL redirects the shell, which is how tests and the
// conformance suite point this at a stub without a real ~/.zshrc.
func defaultCaptureEnv(env func(string) string) func() (string, error) {
	switch strings.ToLower(strings.TrimSpace(env("OC_AGENT_ENV_INHERIT"))) {
	case "0", "false", "no", "off":
		return nil
	}
	shell := env("OC_AGENT_ENV_SHELL")
	if shell == "" {
		shell = interactiveEnvShell
	}
	return func() (string, error) { return captureInteractiveEnv(shell, interactiveEnvTimeout) }
}

// logf is the nil-skipped diagnostic channel (SpawnDeps.Logf).
func (d SpawnDeps) logf(format string, a ...any) {
	if d.Logf != nil {
		d.Logf(format, a...)
	}
}

// interactiveEnvPairs is the FAIL-SAFE wrapper around the CaptureEnv seam. It
// is the single place where "the interactive shell could not be read" is turned
// into "spawn on the minimal environment" instead of "do not spawn".
//
// EVERY degraded case returns nil and lets the spawn continue: seam not wired
// (inheritance off), shell missing, non-zero exit, timeout, oversized output,
// output that contains no parseable record at all. warden starts every agent on
// the machine — a spawn that can be killed by a bad rc file is a single point of
// failure for the entire studio, which is strictly worse than an agent that is
// merely missing some tools.
//
// The warning it emits names the FAILURE, never the output. d.logf goes to
// warden stderr, which launchd captures into ocwarden.err.log.
func (d SpawnDeps) interactiveEnvPairs() []agentEnvPair {
	if d.CaptureEnv == nil {
		return nil
	}
	raw, err := d.CaptureEnv()
	if err != nil {
		d.logf("interactive env: capture failed (%v); spawning with the minimal environment "+
			"— the agent will be missing whatever ~/.zshrc exports", err)
		return nil
	}
	pairs := parseNulEnv(raw, d.logf)
	if len(pairs) == 0 {
		// A shell that exits 0 having printed nothing usable is a real failure
		// mode (an rc file that exec'd something else, a stubbed shell), and it
		// is INVISIBLE unless it is called out — the spawn otherwise looks
		// completely normal while the agent silently has no credentials.
		d.logf("interactive env: capture produced no usable variables; spawning with the minimal environment")
		return nil
	}
	d.logf("interactive env: inherited %d var(s): %s",
		len(pairs), strings.Join(agentEnvKeyNames(pairs), " "))
	return pairs
}

// osWriteFile is the real file-write seam (write-then-chmod, mirroring write_file).
// Tests inject a capturing fake instead.
func osWriteFile(path, content string, mode os.FileMode) error {
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return err
	}
	_ = os.Chmod(path, mode)
	return nil
}

// ---------------------------------------------------------------------------
// pretrust — mark the launch workdir trusted in ~/.claude.json before launch.
// Semantic port of agent/spawn.py:pretrust_launch_cwd (LOAD-BEARING).
// ---------------------------------------------------------------------------

// pretrustWorkdir pre-marks workdir trusted in the claude.json at claudeJSONPath
// BEFORE launch, so the fresh TUI lands on the composer instead of blocking on the
// "trust this folder?" dialog (which would eat the boot nudge → dead-on-boot).
// LOAD-BEARING. Semantic port of pretrust_launch_cwd — read-modify-write SAFELY:
// preserve every existing top-level key, ensure projects["<abs workdir>"] exists,
// and set only hasTrustDialogAccepted=true. A missing or unparsable file starts from
// an empty config (only an absent/corrupt file is replaced — good data is NEVER
// clobbered); any other read error (permission etc.) is surfaced, not swallowed.
// Idempotent: re-trusting the same workdir is a no-op change. The write is ATOMIC
// (temp file in the same dir + rename) at mode 0600, so a crash mid-write can never
// truncate a live ~/.claude.json.
//
// The path is INJECTED (production passes the real ~/.claude.json; tests pass a temp
// file) so a test can NEVER touch the live ~/.claude.json.
func pretrustWorkdir(claudeJSONPath, workdir string) error {
	data := map[string]any{}
	raw, err := os.ReadFile(claudeJSONPath)
	switch {
	case err == nil:
		// Parse best-effort: an unparsable file or a non-object top level restarts
		// from {} (mirrors the python FileNotFoundError/ValueError → {} fallback).
		var loaded any
		if json.Unmarshal(raw, &loaded) == nil {
			if m, ok := loaded.(map[string]any); ok {
				data = m
			}
		}
	case errors.Is(err, os.ErrNotExist):
		// missing file → empty config (created below).
	default:
		return err
	}

	// projects["<abs workdir>"].hasTrustDialogAccepted = true, creating the nested
	// maps only when absent — every other existing key/entry is left untouched.
	projects, ok := data["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
		data["projects"] = projects
	}
	entry, ok := projects[workdir].(map[string]any)
	if !ok {
		entry = map[string]any{}
		projects[workdir] = entry
	}
	entry["hasTrustDialogAccepted"] = true

	// Encode with HTML escaping OFF (mirrors json.dump(ensure_ascii=False)) and a
	// 2-space indent (matches the python indent=2), then write atomically.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	return atomicWriteFile(claudeJSONPath, buf.Bytes(), 0o600)
}

// atomicWriteFile writes data to path via a same-dir temp file + rename, so a reader
// (or a crash) never sees a half-written file. The temp file is created 0600 and the
// mode re-asserted before rename; a leftover temp is cleaned on any error path.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude-json-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has consumed it
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// defaultClaudeJSONPath resolves the claude.json to pre-trust: OC_CLAUDE_JSON
// overrides (a PoC safety valve so a live run can be pointed at a throwaway file),
// else ~/.claude.json (mirrors pretrust_launch_cwd's os.path.expanduser default).
func defaultClaudeJSONPath(env func(string) string) string {
	if p := env("OC_CLAUDE_JSON"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

// ---------------------------------------------------------------------------
// the spawn mechanism — receives the server-downpushed start params, executes.
// ---------------------------------------------------------------------------

// SpawnDeps carries the injectable seams so start(...) drives the whole flow with
// fakes and NO real tmux / FS in tests. Phase 4 wires the real seams (execRunner,
// osWriteFile, os.MkdirAll, resolved claude bin) and the inbound RPC channel.
type SpawnDeps struct {
	Runner CmdRunner
	Base   string
	Socket string // defaults to tmuxSocket when ""
	Home   string // per-agent state base (agentWorkdir joins <home>/<id>)
	// Namespace is the validated OC_NAMESPACE instance key ("" = main instance).
	// Non-empty ⇒ the launch command additionally exports OC_AGENT_HOME=Home so
	// the agent's own cursor/stamp state lands under the namespaced root (R8);
	// empty keeps the launch line byte-identical to the historical output.
	Namespace string
	// EnvFile (T-426d) is the path to the owner's agent env file, default
	// <officraft root>/env. Empty ⇒ the feature is off and the launch line is
	// byte-identical to the pre-T-426d output. An ABSENT file at this path is
	// not an error — the spawn proceeds with no extra env (fail-open).
	EnvFile string
	// CaptureEnv (T-426d follow-up) returns the OWNER'S INTERACTIVE SHELL
	// environment as a NUL-delimited `KEY=VALUE` dump — see interactiveenv.go for
	// why an interactive shell has to be asked at all (launchd never sources
	// ~/.zshrc, and the spawn path is a non-interactive `zsh -c`), and why the
	// dump is `env -0` rather than `export -p`.
	//
	// This is the BASE env layer; EnvFile is layered on top of it as the
	// OVERRIDE. nil ⇒ inheritance is off and the launch line is byte-identical
	// to the pre-inheritance output (also what OC_AGENT_ENV_INHERIT=0 wires).
	//
	// FAIL-SAFE CONTRACT: an error from this seam is NEVER fatal. start() logs a
	// value-free warning and spawns on the minimal environment. warden starts
	// every agent on the machine; this path must not be able to take the studio
	// offline.
	CaptureEnv func() (string, error)
	// Logf (nil-skipped) is where env-file diagnostics go — warden stderr, which
	// launchd captures into <logDir>/ocwarden.err.log. It receives KEY NAMES and
	// reasons ONLY; a value from EnvFile or from CaptureEnv must never be
	// formatted into it.
	Logf      func(string, ...any)
	ClaudeBin string // pre-resolved claude executable (Phase 4 resolves it)
	// ClaudeCreds (T-ba62, nil-skipped) is the spawn-time "is claude logged in?"
	// existence probe. Resolvable-but-logged-out was the ONE prerequisite with no
	// gate anywhere: the TUI starts, the nudge is delivered into a login prompt,
	// and start() returns OK:true forever. The seam returns a value-free verdict
	// (claudecreds.go's SET/unset summary) — it must NEVER be able to hand a
	// credential value back into this file.
	ClaudeCreds func() claudeCredStatus
	// RepoRoot is the officraft checkout root, injected at construction (from
	// os.Executable — ocwarden lives at <repoRoot>/cli/ocwarden/ocwarden). It is the
	// base for the ocagent shim's exec target (<repoRoot>/cli/ocagent/ocagent);
	// injected (not derived inside start) so tests pin a deterministic root.
	RepoRoot string
	// OcAgentBin is the RESOLVED ocagent binary path the workdir `ocagent` symlink
	// points at (resolveOcAgentBin: the ocwarden sibling when home-installed, else the
	// repoRoot-relative dev path). Injected pre-resolved so start needs no FS probe.
	// "" ⇒ ocAgentSymlinkTarget falls back to <RepoRoot>/cli/ocagent/ocagent.
	OcAgentBin string
	WriteFile  func(path, content string, mode os.FileMode) error
	MkdirAll   func(path string, perm os.FileMode) error
	// Symlink / Remove publish the workdir `ocagent` as a SYMLINK to OcAgentBin (see
	// ocAgentSymlinkTarget for why a symlink, not a wrapper/hardlink). Remove clears a
	// stale link first so re-spawn into an existing workdir is idempotent (os.Symlink
	// errors on an existing name); a not-exist Remove is ignored.
	Symlink func(oldname, newname string) error
	Remove  func(name string) error
	Nudge   string // defaults to defaultNudge when ""
	// Pretrust marks the launch workdir trusted in ~/.claude.json BEFORE launch so
	// claude's "trust this folder?" dialog can't intercept and eat the boot nudge
	// (LOAD-BEARING, mirrors pretrust_launch_cwd). nil in Phase 2 (seam only — Phase
	// 4 wires the real ~/.claude.json write); a nil seam is skipped, a failing one
	// aborts the spawn (a live trust gate WOULD eat the nudge → dead-on-boot).
	Pretrust func() error
	// PurgeTrash (T-684c, nil-skipped) reaps <workdir>/trash at spawn time — the
	// scratch the PREVIOUS generation of this agent mv'd there instead of rm-ing it
	// (the harness's un-waivable dangerous-rm prompt hangs a headless agent; see
	// trash.go). Bound PER-SPAWN by the transport wiring because it needs this
	// member's workdir, exactly like Pretrust. Purely best-effort: it never fails
	// a spawn.
	PurgeTrash func()
	// Sleep paces the boot-nudge settle/retry between Enter presses. nil ⇒ no wait
	// (the test default — fakes drive readiness synchronously); production wires
	// time.Sleep so a cold claude REPL gets real time to become input-ready.
	Sleep func(time.Duration)
}

// start EXECUTES one server-downpushed spawn. It does NOT decide whether to spawn
// (that is the server's placement call); it only refuses to CLOBBER a live local
// session (a local safety guard, distinct from the server's over-spawn guard).
//
// Sequence (mirrors TmuxSpawnPort.spawn's new-session → nudge, plus the file writes
// AgentSpawner.run does before launch): guard → durable workdir → write persona.md
// (trusted channel) + .mcp.json + settings.json + .oc-token → build launch command → pretrust
// workdir (Phase-4 seam, nil-skipped) → tmux new-session → boot nudge → read pid →
// outcome. The warden emits NO presence report (server-projected). DEFERRED to Phase 4
// wiring (see file header): ocagent binary publish into workdir, real pretrust write,
// and the STAGE-B settle/retry loop — until those land a spawned agent can boot-fail
// silently (the server's presence projection never sees it come online, and retries).
func (d SpawnDeps) start(p StartParams) SpawnOutcome {
	base := strings.TrimRight(d.Base, "/")
	socket := d.Socket
	if socket == "" {
		socket = tmuxSocket
	}
	session := p.SessionName
	if session == "" {
		session = memberSessionName(p.MemberID)
	}
	role := p.Role
	if role == "" {
		role = "agent" // mirrors AgentSpawner: spec.role or "agent"
	}
	nudge := d.Nudge
	if nudge == "" {
		nudge = defaultNudge
	}

	// claude CLI must be resolvable (Python raises SpawnError otherwise).
	if d.ClaudeBin == "" {
		return SpawnOutcome{OK: false, Reason: "claude_bin_unresolved: set OC_CLAUDE_BIN or put claude on the daemon PATH (~/.local/bin absent from launchd PATH)"}
	}
	// ...and it must be LOGGED IN (T-ba62). A logged-out claude launches its TUI
	// fine, so every downstream step "succeeds" and the outcome is OK:true while
	// the agent can never boot — the silent failure this gate exists to end.
	// nil seam = gate off (test default / OC_CLAUDE_CRED_CHECK=0). The reason
	// carries the value-free SET/unset summary ONLY (see claudecreds.go).
	if d.ClaudeCreds != nil {
		if st := d.ClaudeCreds(); !st.Present {
			return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
				"claude_not_logged_in: no claude credential found on this host (%s) — "+
					"run `claude` once as this user and complete login, then retry. "+
					"To bypass this gate instead: re-run `ocwarden install` with "+
					"OC_CLAUDE_CRED_CHECK=0 in its environment (the warden is a launchd "+
					"job, so only the plist the installer stamps can change its env — "+
					"exporting the variable in a shell has no effect on it)", st.Summary)}
		}
	}
	// idempotent clobber-guard: REFUSE to stomp a live session. This is a LOCAL
	// "don't kill a running agent" safety, NOT the server's over-spawn guard
	// (presence-count placement decision, which lives server-side). A BROKEN probe
	// (nil) is not treated as present — only a positively-present session refuses.
	if has := tmuxHasSession(d.Runner, socket, session); has != nil && *has {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"session_already_exists: tmux session %q is already live (clobber-guard refused to stomp it)", session)}
	}

	workdir := agentWorkdir(d.Home, p.MemberID)
	if err := d.MkdirAll(workdir, 0o700); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"mkdir_failed: workdir %s: %v", workdir, err)}
	}
	// T-684c: reap whatever the PREVIOUS generation of this agent mv'd into
	// <workdir>/trash before the fresh session starts (the seeds tell agents to mv,
	// never rm — see trash.go for why the delete has to happen HERE, outside claude).
	// nil-skipped seam; a refusal/failure is logged inside purgeTrash and NEVER
	// aborts the spawn — a stale trash dir must not be able to take an agent offline.
	if d.PurgeTrash != nil {
		d.PurgeTrash()
	}
	personaFile := filepath.Join(workdir, "persona.md")
	mcpConfigPath := filepath.Join(workdir, ".mcp.json")
	settingsPath := filepath.Join(workdir, "settings.json")
	tokenFile := filepath.Join(workdir, ".oc-token")

	// persona → TRUSTED FILE channel (not the command line): the append-system-prompt
	// points the fresh agent at this file to load its identity.
	if err := d.WriteFile(personaFile, p.PersonaContext, 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: persona.md: %v", err)}
	}
	// .mcp.json → the ONE officraft MCP server, token in the Bearer header.
	// No --strict-mcp-config: the agent ALSO loads user-scope MCP (account
	// connectors, e.g. Slack) on top of this server.
	if err := d.WriteFile(mcpConfigPath, buildMCPConfig(base, p.MemberToken), 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: .mcp.json: %v", err)}
	}
	// settings.json → the statusLine context reporter (paired with --settings).
	if err := d.WriteFile(settingsPath, buildStatuslineSettings(), 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: settings.json: %v", err)}
	}
	// .oc-token → the member token at 0600, so the launch command's argv carries
	// only this PATH and the spawned shell reads the value itself
	// (OC_TOKEN="$(/bin/cat …)") — a literal export would leak the JWT machine-wide
	// via `ps` on the tmux command line. Overwritten on every START, so a
	// handover/recycle re-mint always lands the fresh token.
	if err := d.WriteFile(tokenFile, p.MemberToken, 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: .oc-token: %v", err)}
	}
	// ocagent → the LOCAL publish path (case-B fix for the header's "ocagent binary
	// publish into the workdir" gap): a workdir SYMLINK to the resolved ocagent binary
	// (home sibling once installed, else the repoRoot-relative dev path). The boot
	// prompt's bare `ocagent listen` / `context-report` resolves here via the
	// PATH-prepended workdir and execs the linked GOLANG ocagent (no python on the
	// agent host). A symlink (not a wrapper/hardlink) so warden self-update's atomic
	// rename of the target transparently reaches every agent — see ocAgentSymlinkTarget.
	// Remove clears any stale link first (os.Symlink errors on an existing name) →
	// re-spawn into an existing workdir stays idempotent.
	ocAgentLink := filepath.Join(workdir, "ocagent")
	if err := d.Remove(ocAgentLink); err != nil && !os.IsNotExist(err) {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"symlink_failed: clearing stale ocagent link: %v", err)}
	}
	if err := d.Symlink(ocAgentSymlinkTarget(d.RepoRoot, d.OcAgentBin), ocAgentLink); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"symlink_failed: publishing workdir ocagent link: %v", err)}
	}

	appendSys := buildAppendSystemPrompt(p.MemberID, role, personaFile)
	// Namespaced instances export OC_AGENT_HOME so the agent's sse-cursor /
	// context_report.stamp live under the instance root (R8); the empty
	// namespace exports nothing extra — the line stays byte-identical.
	var extraEnv [][2]string
	if d.Namespace != "" {
		extraEnv = append(extraEnv, [2]string{"OC_AGENT_HOME", d.Home})
	}
	// OC_EFFORT publishes the owner's launch intent (member.effort) into the
	// agent's env so the statusLine reporter (ocagent context-report) can render
	// the ⚡<effort> badge — the effort is a --effort FLAG to claude, never on
	// stdin, so the status line has no other way to see it. Mirror the same
	// empty→"medium" default the --effort flag resolves to (buildLaunchCommandWithEnv),
	// so the env value and the flag value never disagree.
	effortEnv := p.Effort
	if effortEnv == "" {
		effortEnv = "medium"
	}
	extraEnv = append(extraEnv, [2]string{"OC_EFFORT", effortEnv})

	// T-426d: the owner's agent env file. loadAgentEnv NEVER fails the spawn —
	// an absent/unreadable/oversized file yields nil pairs and the agent boots
	// exactly as it does today. Only a NON-EMPTY validated set produces the
	// workdir 0600 render and the source line.
	//
	// The stale render is removed FIRST, unconditionally: if the owner deletes a
	// credential from the env file, the next spawn must not keep handing the
	// agent yesterday's copy out of the workdir. Remove failures are non-fatal
	// (the write below overwrites anyway, and no-file is the common case).
	envRendered := ""
	renderPath := filepath.Join(workdir, agentEnvRenderedName)
	if err := d.Remove(renderPath); err != nil && !os.IsNotExist(err) {
		d.logf("agent env: could not clear stale %s (%v); continuing", renderPath, err)
	}
	// LAYER 1 (base): the owner's INTERACTIVE shell environment. Everything about
	// this call is fail-safe — see interactiveEnvPairs.
	interactive := d.interactiveEnvPairs()
	// LAYER 2 (override): the owner's env file, layered ON TOP so a single
	// variable can be pinned or supplied that the interactive shell lacks. An
	// unwritten file contributes nothing, which keeps the pre-existing
	// four-rounds-reviewed behaviour of agentenv.go exactly as it was.
	fileEnv := loadAgentEnv(d.EnvFile, d.logf)
	if names := overriddenKeyNames(interactive, fileEnv); len(names) > 0 {
		d.logf("agent env: %s overrides the interactive shell for: %s",
			d.EnvFile, strings.Join(names, " "))
	}
	if pairs := mergeAgentEnv(interactive, fileEnv); len(pairs) > 0 {
		// 0600: this file holds the credentials the whole feature exists to
		// deliver. A write failure is NON-FATAL — the agent boots without the
		// extra env rather than not booting at all.
		if err := d.WriteFile(renderPath, renderAgentEnvFile(pairs), 0o600); err != nil {
			d.logf("agent env: could not write %s (%v); spawning without extra env", renderPath, err)
		} else {
			envRendered = renderPath
			// Names only — proving WHAT was loaded without printing a value.
			d.logf("agent env: %d var(s) for the agent (%d inherited from the interactive shell, %d from %s): %s",
				len(pairs), len(interactive), len(fileEnv), d.EnvFile,
				strings.Join(agentEnvKeyNames(pairs), " "))
		}
	}

	command := buildLaunchCommandWithEnv(d.ClaudeBin, workdir, mcpConfigPath, appendSys,
		tokenFile, p.MemberID, base, session, socket, p.Model, p.Effort, settingsPath, extraEnv, envRendered)

	// pretrust the workdir BEFORE launch (LOAD-BEARING): without it claude's trust
	// dialog can intercept and eat the boot nudge → dead-on-boot. Phase 2 leaves the
	// seam nil (Phase 4 wires the real ~/.claude.json write); a nil seam is skipped,
	// a failing one aborts (better to not-spawn than to spawn a nudge-eaten zombie).
	if d.Pretrust != nil {
		if err := d.Pretrust(); err != nil {
			return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
				"pretrust_failed: marking workdir trusted in claude.json: %v", err)}
		}
	}

	// STAGE-A: detached claude TUI in tmux at the pinned geometry.
	if err := tmuxNewSession(d.Runner, socket, session, command); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"spawn_exec_failed: tmux new-session: %v", err)}
	}
	// STAGE-B: deliver the neutral boot nudge via the trusted tmux buffer, with the
	// bounded settle/confirm RETRY loop (pane-capture) so the nudge actually commits
	// even when claude's REPL is not input-ready the instant new-session returns.
	tmuxDeliverNudge(d.Runner, d.Sleep, socket, session, nudge)

	pid := tmuxPanePID(d.Runner, socket, session)
	return SpawnOutcome{OK: true, SessionID: session, PID: pid}
}
