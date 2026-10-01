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
	defaultNudge = "開始。"
	// The Enter loop is UNCONDITIONAL: every spawn spends 30×1s here, out of the
	// 90s receiptDeadlineSecs in server/ocserverd/receipt_watch.go (the START receipt
	// is POSTed only after Spawn returns); that comment lists the rest of the spawn
	// path's budgets, and their worst case already runs past 90s. NEITHER NUMBER
	// HAS EVER BEEN MEASURED, and nothing mechanical links them: cli/ocwarden and
	// server/ocserverd are separate Go modules.
	nudgeMaxAttempts = 30
	nudgeSettle      = 1 * time.Second

	paneCols = 160
	paneRows = 50

	// Read by the member's ocagent (cli/ocagent/listen.go). Rename one side only
	// and both modules stay green;
	// bin/listen-notice-mirror-guard.py holds the two copies equal.
	baseEnv       = "OC_BASE"
	idEnv         = "OC_ID"
	tokenEnv      = "OC_TOKEN"
	sessionEnv    = "OC_SESSION"
	tmuxSocketEnv = "OC_TMUX_SOCKET"
	agentHomeEnv  = "OC_AGENT_HOME"
)

type StartParams struct {
	MemberID       string
	PersonaContext string
	MemberToken    string
	Role           string
	// TaskType is INERT: nothing in this binary reads it. Left in place because
	// removing it is a wire-surface change.
	TaskType string
	Runtime  string
	Model    string

	Effort      string
	SessionName string
}

type SpawnOutcome struct {
	OK        bool
	SessionID string
	PID       string
	// Reason is "<code>: <detail>", folded onto member.last_op_reason (shown in the
	// FE 最近操作 block). Empty on OK.
	Reason string
	Note   string
}

var shlexUnsafe = regexp.MustCompile(`[^\w@%+=:,./-]`)

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !shlexUnsafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func jsonStr(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

// The token rides an "Authorization: Bearer" HEADER, never a ?token= query: the
// loopback only forwards the header.
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

// The two guard hooks are a front-and-back pair against a confirmation prompt
// --dangerously-skip-permissions cannot waive, with nobody at the keyboard:
// PreToolUse→guard-bash keeps it from being raised, PermissionRequest→guard-permission
// answers it once raised (no matcher: every question reaching it is unanswerable).
// All three commands are named bare because the launch line puts the workdir holding
// the ocagent symlink first on PATH.
func buildStatuslineSettings() string {
	return "{\n" +
		"  \"statusLine\": {\n" +
		"    \"type\": \"command\",\n" +
		"    \"command\": \"ocagent context-report\"\n" +
		"  },\n" +
		"  \"hooks\": {\n" +
		"    \"PreToolUse\": [\n" +
		"      {\n" +
		"        \"matcher\": \"Bash\",\n" +
		"        \"hooks\": [\n" +
		"          {\n" +
		"            \"type\": \"command\",\n" +
		"            \"command\": \"ocagent guard-bash\"\n" +
		"          }\n" +
		"        ]\n" +
		"      }\n" +
		"    ],\n" +
		"    \"PermissionRequest\": [\n" +
		"      {\n" +
		"        \"hooks\": [\n" +
		"          {\n" +
		"            \"type\": \"command\",\n" +
		"            \"command\": \"ocagent guard-permission\"\n" +
		"          }\n" +
		"        ]\n" +
		"      }\n" +
		"    ]\n" +
		"  }\n" +
		"}\n"
}

func compactSettingsJSON(raw string) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err != nil {
		return "", err
	}
	return compact.String(), nil
}

// The persona rides a durable local file, NEVER the command line (leak /
// arg-length limits). The ordered boot procedure lives only in
// seeds/boot_sequence.md / boot_sequence_codex.md (pre-fetched into personaFile);
// do not re-spell it here.
func buildClaudeSystemPrompt(agentID, role, personaFile, persona string) string {
	return fmt.Sprintf("你是 %s(role=%s)。以下「---」之後是你的 OffiCraft 開機檔全文"+
		"(與 %s 內容相同),它已經在你的 system prompt 裡:不要再用任何工具讀那個檔。"+
		"開機時照開機檔最後的「啟動步驟」逐步執行,只在這個 session 開始時做一次;"+
		"對話被壓縮(compact)後不是重新開機:不要重跑啟動步驟,繼續手上的工作。\n---\n",
		agentID, role, personaFile) + persona
}

// Only for a claude too old to take --append-system-prompt-file: the member
// then has to read the boot file itself, and one Read call stops short of it.
func buildAppendSystemPrompt(agentID, role, personaFile string) string {
	return fmt.Sprintf("你是 %s(role=%s)。你的完整身分、操作準則與啟動步驟都由 "+
		"launcher 預抓在本地檔 %s。第一步:用 Read 工具從第一行讀到最後一行;"+
		"一次 Read 讀不完時,用 offset/limit 從上一次停下的那一行接著讀,直到讀到最後一行。"+
		"不要用 cat/head/tail/sed 或任何終端機指令讀它:終端機輸出只有開頭一小段會進到你的 context,"+
		"其餘會被靜默丟棄而且不會有任何錯誤訊息,而「啟動步驟」在整份檔案的最後面。"+
		"整份讀完後,照裡面「啟動步驟」段逐步執行。",
		agentID, role, personaFile)
}

// The claude CLI hides --append-system-prompt-file from --help and lets
// --version swallow unknown flags, so neither can answer. Its parser rejects
// the FIRST unknown flag, so a rejection that names the trailing sentinel proves
// the file flag parsed; any other answer means "not supported", which falls back
// to a launch that still boots.
const claudePromptFileProbeFlag = "--oc-probe-unsupported-flag"

// Spent before the START receipt is sent, so it is one line of the budget
// listed at receiptDeadlineSecs (server/ocserverd/receipt_watch.go). A timeout
// reads as "not supported" and still boots.
const claudePromptFileProbeBudget = 2 * time.Second

// The second result says why not, for the warden log; it never carries the
// subprocess output.
func claudeAcceptsPromptFile(r CmdRunner, claudeBin, promptFile string) (bool, string) {
	_, err := r.Run(claudeBin, "--append-system-prompt-file", promptFile, claudePromptFileProbeFlag)
	switch {
	case err == nil:
		return false, "the probe exited 0 on an unknown flag"
	case strings.Contains(err.Error(), "'"+claudePromptFileProbeFlag+"'"):
		return true, ""
	case strings.Contains(err.Error(), "'--append-system-prompt-file'"):
		return false, "claude rejected --append-system-prompt-file"
	case strings.HasPrefix(err.Error(), "timeout after"):
		return false, "the probe timed out"
	default:
		return false, "the probe failed"
	}
}

type timeoutRunner interface {
	withTimeout(time.Duration) CmdRunner
}

// A runner that cannot change its timeout is returned as is.
func withRunTimeout(r CmdRunner, timeout time.Duration) CmdRunner {
	if t, ok := r.(timeoutRunner); ok {
		return t.withTimeout(timeout)
	}
	return r
}

type claudeSystemPrompt struct {
	flag, value string
}

func claudeSystemPromptFile(path string) claudeSystemPrompt {
	return claudeSystemPrompt{flag: "--append-system-prompt-file", value: path}
}

func claudeSystemPromptInline(text string) claudeSystemPrompt {
	return claudeSystemPrompt{flag: "--append-system-prompt", value: text}
}

// The workdir `ocagent` SYMLINK makes the bare `ocagent` resolve here, shadowing any
// unrelated ocagent on the host; without it the agent boots DEAF.
// A symlink, not a hardlink: warden self-update swaps the binary by ATOMIC RENAME, and
// a hardlink would keep the stale inode. Once home-installed, the ocAgentBin sibling
// is LOAD-BEARING: resolveRepoRoot then lands on $HOME and the repoRoot fallback
// points at nothing.
func ocAgentSymlinkTarget(repoRoot, ocAgentBin string) string {
	if ocAgentBin != "" {
		return ocAgentBin
	}
	return filepath.Join(repoRoot, "cli", "ocagent", "ocagent")
}

func (d SpawnDeps) ocAgentTarget() (string, bool) {
	if d.ResolveOcAgentBin == nil {
		return "", false
	}
	return d.ResolveOcAgentBin()
}

// Flags/order are FROZEN: a divergence makes the spawned claude silently lose MCP
// (--mcp-config) or persona (--append-system-prompt-file / --append-system-prompt).
func buildLaunchCommand(claudeBin, workdir, mcpConfigPath string, sys claudeSystemPrompt, tokenFile, agentID, base, session, socket, model, effort, settingsJSON string, ch claudeHome) string {
	return buildLaunchCommandWithEnv(claudeBin, workdir, mcpConfigPath, sys,
		tokenFile, agentID, base, session, socket, model, effort, settingsJSON, nil, "", ch)
}

// ⚠️ ORDER IS THE WHOLE GUARANTEE: the CLAUDE_* purge and the HOME /
// CLAUDE_CONFIG_DIR exports come AFTER the render is sourced, so they overwrite what
// the owner's shell or env file carried. Move either above the source line and the
// child is back to inheriting a config home nobody chose.
func claudeChildEnvPrologue(workdir, envRendered string, ch claudeHome) string {
	s := "cd " + shellQuote(workdir) + "; "
	if envRendered != "" {
		s += "[ -f " + shellQuote(envRendered) + " ] && . " + shellQuote(envRendered) + "; "
	}
	s += claudeEnvPurgeFragment()
	// FALLBACK: the purge depends on /usr/bin/env resolving and degrades to a silent
	// no-op without it; CLAUDE_CONFIG_DIR relocates the credentials file too.
	if ch.ConfigDir == "" {
		s += "unset CLAUDE_CONFIG_DIR; "
	}
	return s
}

func claudeHomeExportPairs(ch claudeHome) [][2]string {
	var pairs [][2]string
	if ch.Home != "" {
		pairs = append(pairs, [2]string{"HOME", ch.Home})
	}
	if ch.ConfigDir != "" {
		pairs = append(pairs, [2]string{"CLAUDE_CONFIG_DIR", ch.ConfigDir})
	}
	return pairs
}

// envRendered is sourced FIRST so (a) the OC_* names this line exports override
// the file's same names — a positional override only, NOT enforcement of the OC_*
// rule (that lives solely in the parser) — and (b) `export PATH=<workdir>:"$PATH"`
// composes ON TOP of an env-file PATH.
func buildLaunchCommandWithEnv(claudeBin, workdir, mcpConfigPath string, sys claudeSystemPrompt, tokenFile, agentID, base, session, socket, model, effort, settingsJSON string, extraEnv [][2]string, envRendered string, ch claudeHome) string {
	cd := claudeChildEnvPrologue(workdir, envRendered, ch)
	pairs := [][2]string{
		{baseEnv, base},
		{sessionEnv, session},
		{tmuxSocketEnv, socket},
	}
	pairs = append(pairs, extraEnv...)
	// LAST in the export list, so a same-named pair from extraEnv cannot win.
	pairs = append(pairs, claudeHomeExportPairs(ch)...)
	kvs := make([]string, 0, len(pairs)+1)
	// OC_TOKEN reads the 0600 token file at exec time: the tmux command line is
	// visible machine-wide via `ps`. ABSOLUTE /bin/cat (measured): the env file sourced
	// earlier may leave PATH without /bin, and a bare `cat` then makes OC_TOKEN silently
	// EMPTY.
	kvs = append(kvs, tokenEnv+`="$(/bin/cat `+shellQuote(tokenFile)+`)"`)
	for _, p := range pairs {
		kvs = append(kvs, p[0]+"="+shellQuote(p[1]))
	}
	exports := "export " + strings.Join(kvs, " ") + "; "
	exports += "export PATH=" + shellQuote(workdir) + `:"$PATH"; `
	// Pinned "medium": the CLI default is high, the main token-cost driver.
	if effort == "" {
		effort = "medium"
	}
	parts := []string{
		shellQuote(claudeBin),
		"--dangerously-skip-permissions",
		// --dangerously-skip-permissions does NOT gate AskUserQuestion, and in a headless
		// tmux session nobody watches its menu blocks forever.
		"--disallowedTools",
		"AskUserQuestion",
		"--mcp-config",
		shellQuote(mcpConfigPath),
		"--effort",
		shellQuote(effort),
		sys.flag,
		shellQuote(sys.value),
	}
	if model != "" {
		parts = append(parts, "--model", shellQuote(model))
	}
	if settingsJSON != "" {
		parts = append(parts, "--settings", shellQuote(settingsJSON))
	}
	return cd + exports + "exec " + strings.Join(parts, " ")
}

// window-size manual: a later read-only attach must not shrink the fixed pane
// (early wrap).
func tmuxNewSession(r CmdRunner, socket, session, command string) error {
	cols, rows := strconv.Itoa(paneCols), strconv.Itoa(paneRows)
	if _, err := r.Run("tmux", "-L", socket, "new-session", "-d", "-s", session, "-x", cols, "-y", rows, command); err != nil {
		return err
	}
	_, _ = r.Run("tmux", "-L", socket, "set-option", "-t", session, "window-size", "manual")
	_, _ = r.Run("tmux", "-L", socket, "resize-window", "-t", session, "-x", cols, "-y", rows)
	return nil
}

// OC_SESSION names the MEMBER's session, never the listener's own: it is what
// `--deliver-tmux` pastes into and what the listener's self-exit probe watches — the
// tie that stops an orphaned listener projecting a dead member as online.
func buildListenerLaunchCommand(workdir, tokenFile, base, session, socket string,
	extraEnv [][2]string, envRendered string) string {
	s := "cd " + shellQuote(workdir) + "; "
	if envRendered != "" {
		s += "[ -f " + shellQuote(envRendered) + " ] && . " + shellQuote(envRendered) + "; "
	}
	kvs := []string{tokenEnv + `="$(/bin/cat ` + shellQuote(tokenFile) + `)"`}
	pairs := [][2]string{
		{baseEnv, base},
		{sessionEnv, session},
		{tmuxSocketEnv, socket},
	}
	pairs = append(pairs, extraEnv...)
	for _, p := range pairs {
		kvs = append(kvs, p[0]+"="+shellQuote(p[1]))
	}
	s += "export " + strings.Join(kvs, " ") + "; "
	s += "export PATH=" + shellQuote(workdir) + `:"$PATH"; `
	return s + "exec ocagent listen --deliver-tmux"
}

// A failed listener start is LOGGED, never fatal: the member is already up and
// nudged. 🔴 The stale kill is NOT tidiness: session names are reused across
// respawns, so a leftover listener would not self-exit, and the station kicking one
// of two listeners ends in `ocagent suicide` killing the just-spawned member.
func startListenerSession(d SpawnDeps, socket, session, memberID, command string) {
	listenSession := listenerSessionName(memberID)
	_, _ = d.Runner.Run("tmux", "-L", socket, "kill-session", "-t", listenSession)
	if err := tmuxNewSession(d.Runner, socket, listenSession, command); err != nil {
		d.logf("listener: could not start %s for %s (%v); the member boots deaf and "+
			"the station will recycle it", listenSession, session, err)
	}
}

// 🔴 nil FALLS BACK TO time.Sleep, NOT A NO-OP: a no-op here silently turned 30 paced
// Enters into 30 in microseconds with the whole package green. It only catches nil —
// a non-nil no-op clock at the per-spawn seam is indistinguishable by type.
func nudgeClock(sleep func(time.Duration)) func(time.Duration) {
	if sleep == nil {
		return time.Sleep
	}
	return sleep
}

// Delivered via a tmux buffer, never send-keys -l (drops multibyte under a busy
// TUI); set-buffer takes it as argv because CmdRunner has no stdin channel.
func tmuxDeliverNudge(r CmdRunner, sleep func(time.Duration), socket, session, nudge string) {
	sleep = nudgeClock(sleep)
	const buf = "oc-spawn-nudge"
	_, _ = r.Run("tmux", "-L", socket, "set-buffer", "-b", buf, nudge)
	// Paste ONCE (it lands even in a not-ready REPL); only the Enter races, so it
	// is retried. This loop deliberately does NOT judge success — a statusline-scraping
	// check was permanently false. The authority is the server's PRESENCE (a live SSE
	// listener for this member id), NOT a report_waking receipt and NOT waking_since
	// (stamped at dispatch).
	if _, err := r.Run("tmux", "-L", socket, "paste-buffer", "-t", session, "-b", buf, "-d", "-p"); err != nil {
		_, _ = r.Run("tmux", "-L", socket, "paste-buffer", "-t", session, "-b", buf)
	}
	for attempt := 0; attempt < nudgeMaxAttempts; attempt++ {
		// Under emacs mode-keys a pane left in copy-mode swallows every Enter while the
		// paste still lands; -q leaves any mode. Per attempt: a viewer can re-enter it.
		_, _ = r.Run("tmux", "-L", socket, "copy-mode", "-q", "-t", session)
		_, _ = r.Run("tmux", "-L", socket, "send-keys", "-t", session, "Enter")
		sleep(nudgeSettle)
	}
}

// DURABLE, not mkdtemp: a reaped tmpdir would take the token-bearing .mcp.json.
func agentWorkdir(home, id string) string {
	return filepath.Join(home, strings.ToLower(id))
}

// The namespace is validated upstream (realMain), so an error here degrades to
// the main-instance default.
func defaultAgentHome(env func(string) string) string {
	if h := env("OC_AGENT_HOME"); h != "" {
		return h
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "agents")
}

func defaultAgentEnvFile(env func(string) string) string {
	if p := env("OC_AGENT_ENV_FILE"); p != "" {
		return p
	}
	ns, _ := namespaceFromEnv(env)
	home, _ := os.UserHomeDir()
	return filepath.Join(officraftRootFor(home, ns), "env")
}

// OC_AGENT_ENV_INHERIT is the owner's kill switch back to the old behaviour
// without a rebuild: this feature runs on EVERY spawn.
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

func (d SpawnDeps) logf(format string, a ...any) {
	if d.Logf != nil {
		d.Logf(format, a...)
	}
}

// loggedOut is true only on an explicit logged-out verdict: an unknown one (an
// unreadable keychain, a timeout, output that does not parse) launches.
func (d SpawnDeps) loggedOut(runtime string) bool {
	if d.LoginCheck == nil {
		return false
	}
	verdict := d.LoginCheck(runtime)
	return verdict != nil && !*verdict
}

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
		d.logf("interactive env: capture produced no usable variables; spawning with the minimal environment")
		return nil
	}
	d.logf("interactive env: inherited %d var(s): %s",
		len(pairs), strings.Join(agentEnvKeyNames(pairs), " "))
	return pairs
}

func osWriteFile(path, content string, mode os.FileMode) error {
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return err
	}
	_ = os.Chmod(path, mode)
	return nil
}

// LOAD-BEARING: without it the "trust this folder?" dialog eats the boot nudge →
// dead-on-boot.
func pretrustWorkdir(claudeJSONPath, workdir string) error {
	return editClaudeProjectEntry(claudeJSONPath, workdir, func(entry map[string]any) {
		entry["hasTrustDialogAccepted"] = true
	})
}

// claude keys a project by the SYMLINK-RESOLVED cwd (measured A/B): a literal key
// behind a symlinked parent is never read.
func claudeProjectKey(workdir string) string {
	if resolved, err := filepath.EvalSymlinks(workdir); err == nil {
		return resolved
	}
	return workdir
}

func editClaudeProjectEntry(claudeJSONPath, workdir string, fn func(entry map[string]any)) error {
	workdir = claudeProjectKey(workdir)
	data := map[string]any{}
	raw, err := os.ReadFile(claudeJSONPath)
	switch {
	case err == nil:
		var loaded any
		if json.Unmarshal(raw, &loaded) == nil {
			if m, ok := loaded.(map[string]any); ok {
				data = m
			}
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}

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
	fn(entry)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	return atomicWriteFile(claudeJSONPath, buf.Bytes(), 0o600)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude-json-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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

type SpawnDeps struct {
	Runner     CmdRunner
	Base       string
	Socket     string
	Home       string
	Namespace  string
	EnvFile    string
	CaptureEnv func() (string, error)
	// LaunchEnv keeps the last spawn's interactive layer for the login check.
	LaunchEnv *launchEnvCache
	// Logf receives KEY NAMES and reasons ONLY, never a value.
	Logf      func(string, ...any)
	ClaudeBin string
	// nil, or false, launches with the inline boot pointer that has the member
	// read the boot file itself; the string says why not.
	ClaudeTakesPromptFile func(promptFile string) (bool, string)
	CodexBin              string
	// ClaudeHome feeds both the launch line and Pretrust's file, which keeps the
	// write and the read on the same claude.json.
	ClaudeHome claudeHome
	WardenBin  string
	// LoginCheck runs the runtime's login check now (the same one the heartbeat
	// reports) and returns its verdict; nil verdict = unknown, nil func = no check.
	LoginCheck func(runtime string) *bool
	// nil refuses every spawn whose model is a Codex family word.
	CodexModels func(codexBin string) ([]codexModelEntry, error)
	RepoRoot    string
	// Resolved PER SPAWN: on a fresh machine ocagent is downloaded AFTER warden boot,
	// so a boot-time path left every member with a dangling symlink, never online, and
	// no error anywhere. 🔴 REQUIRED: a lenient nil restored exactly that bug with the
	// package green, so nil REFUSES the spawn.
	ResolveOcAgentBin func() (string, bool)
	WriteFile         func(path, content string, mode os.FileMode) error
	MkdirAll          func(path string, perm os.FileMode) error
	Symlink           func(oldname, newname string) error
	Remove            func(name string) error
	Nudge             string
	Pretrust          func() error
	PurgeTrash        func()
	// nil means REAL time.Sleep (see nudgeClock): tests wanting speed pass a no-op
	// explicitly.
	Sleep func(time.Duration)
}

// A named method, not `sd := base; sd.X = …` in the transport closure: widening
// what varies per spawn must change THIS signature. It does NOT make SpawnDeps
// immutable: a caller can still assign to the returned value.
func (d SpawnDeps) withPerSpawn(pretrust func() error, purgeTrash func()) SpawnDeps {
	d.Pretrust = pretrust
	d.PurgeTrash = purgeTrash
	return d
}

// Owner-facing last_op_reason, rendered with no truncation in --color-danger, so
// it is compressed to labels. The Codex exit comes FIRST: it is usually the
// cheapest fix, and owners were being sent to install a runtime they had declined.
const claudeBinUnresolvedReason = "claude_bin_unresolved: no Claude Code on this machine. " +
	"Fix any one: set this member's 執行環境 to Codex; " +
	"install Claude Code here; or re-install the warden with OC_CLAUDE_BIN=<path>."

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
		role = "agent"
	}
	nudge := d.Nudge
	if nudge == "" {
		nudge = defaultNudge
	}

	runtimeName := strings.TrimSpace(p.Runtime)
	if runtimeName == "" {
		runtimeName = "claude"
	}
	if runtimeName != "claude" && runtimeName != "codex" {
		return SpawnOutcome{OK: false, Reason: "runtime_unsupported: expected claude or codex"}
	}
	if runtimeName == "claude" && d.ClaudeBin == "" {
		return SpawnOutcome{OK: false, Reason: claudeBinUnresolvedReason}
	}
	if runtimeName == "claude" && d.ClaudeHome.Home == "" {
		return SpawnOutcome{OK: false, Reason: "claude_home_unresolved: the launch line cannot state the child's HOME, so the pre-trusted claude.json may not be the file it reads — set HOME in the warden's environment"}
	}
	if runtimeName == "codex" {
		if d.CodexBin == "" {
			return SpawnOutcome{OK: false, Reason: "codex_bin_unresolved: set OC_CODEX_BIN or put codex on the daemon PATH"}
		}
		if d.WardenBin == "" {
			return SpawnOutcome{OK: false, Reason: "warden_bin_unresolved: cannot launch codex-session sidecar"}
		}
		if d.loggedOut("codex") {
			return SpawnOutcome{OK: false, Reason: "codex_not_logged_in: `codex login status` failed on this host"}
		}
		resolved, refusal := d.resolveCodexLaunchModel(p.Model)
		if refusal != "" {
			return SpawnOutcome{OK: false, Reason: refusal}
		}
		p.Model = resolved
	}
	// A logged-out claude launches its TUI fine and the spawn would report OK:true
	// while the agent can never boot.
	if runtimeName == "claude" && d.loggedOut("claude") {
		return SpawnOutcome{OK: false, Reason: "claude_not_logged_in: `claude auth status` reports " +
			"logged out on this host. Fix any one: set this member's 執行環境 to Codex; " +
			"log in with `claude` as this user; or re-install the warden with " +
			"OC_CLAUDE_CRED_CHECK=0 (shell exports do not reach it)."}
	}
	// A BROKEN probe (nil) is not treated as present — only a positively-present session refuses.
	if has := tmuxHasSession(d.Runner, socket, session); has != nil && *has {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"session_already_exists: tmux session %q is already live (clobber-guard refused to stomp it)", session)}
	}

	workdir := agentWorkdir(d.Home, p.MemberID)
	if err := d.MkdirAll(workdir, 0o700); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"mkdir_failed: workdir %s: %v", workdir, err)}
	}
	if d.PurgeTrash != nil {
		d.PurgeTrash()
	}
	personaFile := filepath.Join(workdir, "persona.md")
	promptFile := filepath.Join(workdir, "system-prompt.md")
	mcpConfigPath := filepath.Join(workdir, ".mcp.json")
	settingsPath := filepath.Join(workdir, "settings.json")
	settingsJSON, err := compactSettingsJSON(buildStatuslineSettings())
	if err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf("settings_invalid: %v", err)}
	}
	tokenFile := filepath.Join(workdir, ".oc-token")

	if err := d.WriteFile(personaFile, p.PersonaContext, 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: persona.md: %v", err)}
	}
	// No --strict-mcp-config: the agent ALSO loads user-scope MCP (account
	// connectors, e.g. Slack) on top of this server.
	if err := d.WriteFile(mcpConfigPath, buildMCPConfig(base, p.MemberToken), 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: .mcp.json: %v", err)}
	}
	// settings.json is kept for inspection only; the launch passes settingsJSON inline
	// so a later file edit cannot redirect the child's config home.
	if err := d.WriteFile(settingsPath, buildStatuslineSettings(), 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: settings.json: %v", err)}
	}
	if err := d.WriteFile(tokenFile, p.MemberToken, 0o600); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"write_file_failed: .oc-token: %v", err)}
	}
	ocAgentLink := filepath.Join(workdir, "ocagent")
	if err := d.Remove(ocAgentLink); err != nil && !os.IsNotExist(err) {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"symlink_failed: clearing stale ocagent link: %v", err)}
	}
	ocAgentTarget, ocAgentPresent := d.ocAgentTarget()
	if !ocAgentPresent {
		where := ocAgentTarget
		if where == "" {
			where = "<no path: this warden was built without an ocagent resolver>"
		}
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"ocagent_not_found: no ocagent binary at %s. The agent would start "+
				"but could never connect. If this machine was just installed, the "+
				"download may still be running — the next spawn picks it up with no "+
				"warden restart. Otherwise re-install the warden on this machine.",
			where)}
	}
	if err := d.Symlink(ocAgentTarget, ocAgentLink); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"symlink_failed: publishing workdir ocagent link: %v", err)}
	}

	sys := claudeSystemPromptInline(buildAppendSystemPrompt(p.MemberID, role, personaFile))
	takesPromptFile := false
	if runtimeName == "claude" {
		whyNot := "no prompt-file probe is wired"
		if d.ClaudeTakesPromptFile != nil {
			takesPromptFile, whyNot = d.ClaudeTakesPromptFile(promptFile)
		}
		if !takesPromptFile {
			d.logf("%s boots by reading persona.md itself, not via --append-system-prompt-file: %s",
				p.MemberID, whyNot)
		}
	}
	if takesPromptFile {
		if err := d.WriteFile(promptFile, buildClaudeSystemPrompt(p.MemberID, role, personaFile, p.PersonaContext), 0o600); err != nil {
			return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
				"write_file_failed: system-prompt.md: %v", err)}
		}
		sys = claudeSystemPromptFile(promptFile)
	}
	// Namespaced instances export OC_AGENT_HOME: otherwise two instances' same-named
	// agents share one sse-cursor / context_report.stamp dir.
	var extraEnv [][2]string
	if d.Namespace != "" {
		extraEnv = append(extraEnv, [2]string{agentHomeEnv, d.Home})
	}

	// The stale render is removed FIRST: a credential the owner deleted from the env
	// file must not keep reaching the agent from the workdir.
	envRendered := ""
	renderPath := filepath.Join(workdir, agentEnvRenderedName)
	if err := d.Remove(renderPath); err != nil && !os.IsNotExist(err) {
		d.logf("agent env: could not clear stale %s (%v); continuing", renderPath, err)
	}
	interactive := d.interactiveEnvPairs()
	d.LaunchEnv.remember(interactive)
	fileEnv := loadAgentEnv(d.EnvFile, d.logf)
	if names := overriddenKeyNames(interactive, fileEnv); len(names) > 0 {
		d.logf("agent env: %s overrides the interactive shell for: %s",
			d.EnvFile, strings.Join(names, " "))
	}
	if pairs := mergeAgentEnv(interactive, fileEnv); len(pairs) > 0 {
		if err := d.WriteFile(renderPath, renderAgentEnvFile(pairs), 0o600); err != nil {
			d.logf("agent env: could not write %s (%v); spawning without extra env", renderPath, err)
		} else {
			envRendered = renderPath
			d.logf("agent env: %d var(s) for the agent (%d inherited from the interactive shell, %d from %s): %s",
				len(pairs), len(interactive), len(fileEnv), d.EnvFile,
				strings.Join(agentEnvKeyNames(pairs), " "))
		}
	}

	command := ""
	if runtimeName == "codex" {
		command = buildCodexLaunchCommand(d.WardenBin, d.CodexBin, workdir,
			personaFile, tokenFile, p.MemberID, base, session, socket, p.Model, p.Effort,
			extraEnv, envRendered, d.logf)
	} else {
		command = buildLaunchCommandWithEnv(d.ClaudeBin, workdir, mcpConfigPath, sys,
			tokenFile, p.MemberID, base, session, socket, p.Model, p.Effort, settingsJSON, extraEnv, envRendered,
			d.ClaudeHome)
	}

	if runtimeName == "claude" && d.Pretrust != nil {
		if err := d.Pretrust(); err != nil {
			return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
				"pretrust_failed: marking workdir trusted in claude.json: %v", err)}
		}
	}

	if err := tmuxNewSession(d.Runner, socket, session, command); err != nil {
		return SpawnOutcome{OK: false, Reason: fmt.Sprintf(
			"spawn_exec_failed: tmux new-session: %v", err)}
	}
	if runtimeName == "claude" {
		// Claude only: codex's sidecar starts the boot turn through App Server; keystrokes
		// would target a non-interactive pane.
		tmuxDeliverNudge(d.Runner, d.Sleep, socket, session, nudge)

		// The listener runs BESIDE the member, not inside its harness, which drops
		// background jobs every 30 minutes (presence IS that connection). Started AFTER
		// the nudge: a listener connecting first would paste into a still-starting TUI.
		startListenerSession(d, socket, session, p.MemberID,
			buildListenerLaunchCommand(workdir, tokenFile, base, session, socket,
				extraEnv, envRendered))
	}

	pid := tmuxPanePID(d.Runner, socket, session)
	return SpawnOutcome{OK: true, SessionID: session, PID: pid}
}
