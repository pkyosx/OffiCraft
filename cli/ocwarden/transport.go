// transport.go — the warden's SSE command reader. The server cannot dial INTO a
// warden behind NAT, so commands ride the warden's own outbound GET /api/events:
// the warden sends no query/header except its Bearer auth, the server addresses
// the connection by the authenticated token sub (kind=warden), and
// parseCommandFrame (command.go) ignores every other frame on the stream.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	eventsPath = "/api/events"

	sseBackoffStart = 1 * time.Second
	sseBackoffCap   = 60 * time.Second

	sseDialTimeout   = 10 * time.Second
	sseHeaderTimeout = 30 * time.Second
	// Must stay well above the server's 15 s `: heartbeat` period (~3×): if
	// nothing at all arrives within it, the connection is presumed half-open and
	// dropped into reconnect.
	sseIdleReadTimeout = 45 * time.Second

	maxSSELine = 8 << 20
)

func scanSSE(r io.Reader, onPayload func([]byte)) error {
	return scanSSEWithActivity(r, onPayload, nil)
}

// scanSSEWithActivity: onActivity fires on EVERY line read, `: heartbeat`
// comments included, so a healthy heartbeat-only stream never trips the
// idle-read watchdog.
func scanSSEWithActivity(r io.Reader, onPayload func([]byte), onActivity func()) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		onPayload([]byte(payload))
	}
	for sc.Scan() {
		if onActivity != nil {
			onActivity()
		}
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			data = append(data, value)
		}
	}
	return sc.Err()
}

type sseTransport struct {
	base   string
	token  string
	client *http.Client
	deps   CommandDeps

	sleep           func(time.Duration)
	backoffStart    time.Duration
	backoffCap      time.Duration
	idleReadTimeout time.Duration
	logf            func(format string, args ...any)
	// onConnect fires on every successful (re)connect. main.go wires it to the
	// self-updater's Kick: a redeploy drops every stream, so a reconnect is the
	// most reliable sign to check for a new binary now instead of waiting out the
	// 15m poll.
	onConnect func()
}

// newSSEClient bounds connection SETUP only (dial / TLS / response headers). Never
// give it an overall Timeout: that would guillotine the always-open stream.
func newSSEClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: sseDialTimeout}).DialContext,
			TLSHandshakeTimeout:   sseDialTimeout,
			ResponseHeaderTimeout: sseHeaderTimeout,
		},
	}
}

func (t *sseTransport) run(ctx context.Context) {
	backoff := t.backoffStart
	for {
		if ctx.Err() != nil {
			return
		}
		opened, err := t.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if opened {
			backoff = t.backoffStart
			t.logf("[ocwarden] command reader: stream ended (%v); reconnecting in %s", err, backoff)
		} else {
			t.logf("[ocwarden] command reader: connect failed (%v); retrying in %s", err, backoff)
		}
		if !sleepCtx(ctx, t.sleep, backoff) {
			return
		}
		backoff = nextSSEBackoff(backoff, t.backoffCap)
	}
}

// connectOnce reports opened=true once a 200 body is being read (the caller then
// resets backoff).
//
// The idle-read watchdog uses SetReadDeadline on the socket (captured via
// httptrace.GotConn) rather than cancelling the request context: on a genuine
// half-open TCP the context-cancel→Close path may not unblock the blocked Read.
// If no conn was captured it falls back to an AfterFunc(cancelConn) watchdog so
// the watchdog is never silently disabled.
func (t *sseTransport) connectOnce(ctx context.Context) (opened bool, err error) {
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	var (
		connMu   sync.Mutex
		httpConn net.Conn
	)
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connMu.Lock()
			httpConn = info.Conn
			connMu.Unlock()
		},
	}
	connCtx = httptrace.WithClientTrace(connCtx, trace)
	req, err := http.NewRequestWithContext(connCtx, http.MethodGet, t.base+eventsPath, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	t.logf("[ocwarden] command reader: connected — streaming %s%s", t.base, eventsPath)
	if t.onConnect != nil {
		t.onConnect()
	}
	var onActivity func()
	if t.idleReadTimeout > 0 {
		connMu.Lock()
		conn := httpConn
		connMu.Unlock()
		if conn != nil {
			_ = conn.SetReadDeadline(time.Now().Add(t.idleReadTimeout))
			onActivity = func() { _ = conn.SetReadDeadline(time.Now().Add(t.idleReadTimeout)) }
			defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
		} else {
			watchdog := time.AfterFunc(t.idleReadTimeout, cancelConn)
			defer watchdog.Stop()
			onActivity = func() { watchdog.Reset(t.idleReadTimeout) }
		}
	}
	return true, scanSSEWithActivity(resp.Body, t.handlePayload, onActivity)
}

func (t *sseTransport) handlePayload(payload []byte) {
	defer func() {
		if r := recover(); r != nil {
			t.logf("[ocwarden] command reader: recovered from panic handling frame: %v", r)
		}
	}()
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.logf("[ocwarden] command reader: skip malformed frame: %v", err)
		return
	}
	if cmd == nil {
		return
	}
	t.logf("[ocwarden] command reader: received %s frame (%s)", cmd.RPC, commandTargetLabel(cmd))
	if err := dispatchCommand(cmd, t.deps); err != nil {
		// A receipt fault means the op DID run — do not log it as "dispatch refused".
		// Only the server's receipt deadline (receipt_watch.go) will surface the
		// divergence.
		if errors.Is(err, errReceiptUndelivered) {
			t.logf("[ocwarden] command reader: %s EXECUTED but its receipt did not reach "+
				"the server (%s): %v — the server does not know this outcome",
				cmd.RPC, commandTargetLabel(cmd), err)
			return
		}
		t.logf("[ocwarden] command reader: dispatch refused: %v", err)
		return
	}
	t.logf("[ocwarden] command reader: dispatched %s OK (%s)", cmd.RPC, commandTargetLabel(cmd))
}

func commandTargetLabel(cmd *Command) string {
	if id, ok := argString(cmd.Args, "member_id"); ok && id != "" {
		return "member_id=" + id
	}
	if id, ok := argString(cmd.Args, "worker_id"); ok && id != "" {
		return "worker_id=" + id
	}
	return "target=?"
}

func nextSSEBackoff(cur, capd time.Duration) time.Duration {
	if cur <= 0 {
		return capd
	}
	next := cur * 2
	if next > capd {
		return capd
	}
	return next
}

func sleepCtx(ctx context.Context, sleep func(time.Duration), d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	sleep(d)
	return ctx.Err() == nil
}

// resolveClaudeBin: a launchd daemon's minimal PATH typically lacks ~/.local/bin
// (where claude installs), so a bare exec.LookPath fails there and start()
// refuses every spawn. `ocwarden install` stamps OC_CLAUDE_BIN into the plist
// (resolveClaudeForInstall, install.go) because PATH and the fallback dirs both
// miss a version-manager (asdf/nvm/volta) claude.
func resolveClaudeBin(env func(string) string) string {
	if p := strings.TrimSpace(env("OC_CLAUDE_BIN")); p != "" && isExecutableFile(p) {
		return p
	}
	if p, err := exec.LookPath("claude"); err == nil && p != "" {
		return p
	}
	home := env("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	for _, cand := range []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if isExecutableFile(cand) {
			return cand
		}
	}
	return ""
}

func resolveCodexBin(env func(string) string) string {
	if p := strings.TrimSpace(env("OC_CODEX_BIN")); p != "" && isExecutableFile(p) {
		return p
	}
	if p, err := exec.LookPath("codex"); err == nil && p != "" {
		return p
	}
	home := env("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	for _, cand := range []string{
		filepath.Join(home, ".local", "bin", "codex"),
		filepath.Join(home, ".npm-global", "bin", "codex"),
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	} {
		if isExecutableFile(cand) {
			return cand
		}
	}
	return ""
}

func isExecutableFile(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// resolveRepoRoot assumes the in-tree layout <repoRoot>/cli/ocwarden/ocwarden.
func resolveRepoRoot(executable func() (string, error)) string {
	exe, err := executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(exe)))
}

// pathStatable is a bare stat (a directory passes too). That suffices because
// install writes to a temp path and renames into place. A named function so it
// is tested on its own: a `return true` here makes the caller symlink to a
// binary that was never downloaded.
func pathStatable(p string) bool { _, err := os.Stat(p); return err == nil }

func newOcAgentResolver(executable func() (string, error), exists func(string) bool) func() (string, bool) {
	return func() (string, bool) {
		return resolveOcAgentBin(executable, exists, resolveRepoRoot(executable))
	}
}

// resolveOcAgentBin prefers the SIBLING of the running ocwarden: a home-installed
// warden carries ocagent in the same dir (install's installOcAgent), so no
// OC_AGENT_BIN env or plist stamp is needed. Fallback is the in-tree dev layout
// <repoRoot>/cli/ocagent/ocagent. The bool reports whether the chosen path
// exists, so the spawn path can tell "found" from "guessed".
func resolveOcAgentBin(executable func() (string, error), exists func(string) bool, repoRoot string) (string, bool) {
	if exe, err := executable(); err == nil {
		if sibling := filepath.Join(filepath.Dir(exe), "ocagent"); exists(sibling) {
			return sibling, true
		}
	}
	fallback := filepath.Join(repoRoot, "cli", "ocagent", "ocagent")
	return fallback, exists(fallback)
}

// buildLoginGate is the spawn-time login check: the heartbeat's own check, run
// now. OC_CLAUDE_CRED_CHECK=0 still switches it off for claude — the escape
// hatch for a host whose `claude auth status` reads logged out while claude in
// fact runs.
func buildLoginGate(env func(string) string, login *loginProber) func(runtime string) *bool {
	if login == nil {
		return nil
	}
	claudeOff := strings.TrimSpace(env("OC_CLAUDE_CRED_CHECK")) == "0"
	return func(runtime string) *bool {
		if runtime == "claude" && claudeOff {
			return nil
		}
		return login.checkForSpawn(runtime)
	}
}

// buildSpawnDeps is separate so tests can inspect the production literal:
// setting ResolveOcAgentBin to nil once reinstated the dangling-ocagent defect
// with the package green.
func buildSpawnDeps(cfg Config, env func(string) string, runner CmdRunner, socket, ns string) SpawnDeps {
	claudeBin := resolveClaudeBin(env)
	codexBin := resolveCodexBin(env)
	wardenBin, _ := os.Executable()
	return SpawnDeps{
		Runner:    runner,
		Base:      cfg.Base,
		Socket:    socket,
		Home:      defaultAgentHome(env),
		Namespace: ns,
		EnvFile:   defaultAgentEnvFile(env),
		// The base env layer (the owner's interactive shell, captured per spawn);
		// EnvFile layers on top as the override.
		CaptureEnv: defaultCaptureEnv(env),
		Logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "[ocwarden spawn] "+format+"\n", a...)
		},
		ClaudeBin: claudeBin,
		ClaudeTakesPromptFile: func(promptFile string) bool {
			return claudeAcceptsPromptFile(newCmdRunner(claudePromptFileProbeBudget), claudeBin, promptFile)
		},
		CodexBin:    codexBin,
		ClaudeHome:  resolvedClaudeHome(env, stderrLogf),
		WardenBin:   wardenBin,
		CodexModels: listCodexModels,
		RepoRoot:    resolveRepoRoot(os.Executable),
		// 🔴 A FUNCTION, resolved at spawn time: on a fresh machine ocagent is
		// downloaded after the warden boots, and a value baked in here left every
		// spawn with a dangling symlink, silently offline. And no inline closure: a
		// one-word `return true` edit here once reinstated that defect with the
		// package still green.
		ResolveOcAgentBin: newOcAgentResolver(os.Executable, pathStatable),
		WriteFile:         osWriteFile,
		MkdirAll:          os.MkdirAll,
		Symlink:           os.Symlink,
		Remove:            os.Remove,
		Sleep:             time.Sleep,
		Pretrust:          nil,
	}
}

func buildCommandDeps(cfg Config, env func(string) string, runner CmdRunner, launchEnv *launchEnvCache,
	login *loginProber) CommandDeps {
	// Error ignored: realMain refuses an invalid OC_NAMESPACE before any transport
	// is built.
	ns, _ := namespaceFromEnv(env)
	socket := tmuxSocketFor(ns)
	spawnDeps := buildSpawnDeps(cfg, env, runner, socket, ns)
	spawnDeps.LaunchEnv = launchEnv
	spawnDeps.LoginCheck = buildLoginGate(env, login)
	claudeJSONPath := spawnDeps.ClaudeHome.ClaudeJSONPath()
	return CommandDeps{
		Spawn: func(p StartParams) SpawnOutcome {
			workdir := agentWorkdir(spawnDeps.Home, p.MemberID)
			return spawnDeps.withPerSpawn(
				func() error { return pretrustWorkdir(claudeJSONPath, workdir) },
				func() { purgeTrash(spawnDeps.Home, workdir, stderrLogf) },
			).start(p)
		},
		Stop: func(session string) (bool, bool) {
			// The detached `ocagent listen` never receives the session's SIGHUP, so the
			// sweep finds it by workdir (lsof) and reaps it by pid. A legacy
			// worker-<ow-id> session resolves the retired workers/ root; an unresolvable
			// session keeps root "", which makes purgeTrash refuse.
			root := defaultAgentHome(env)
			workdir := memberWorkdirForSession(root, session)
			if workdir == "" {
				root = defaultWorkerHome(env)
				workdir = workerWorkdirForSession(root, session)
			}
			if workdir == "" {
				root = ""
			}
			return stop(runner, socket, session, realKill, realGetpgid, sweepSeams{
				listenPIDs: func(wd string) []int { return ocagentPIDsByCwd(runner, wd) },
				workdir:    workdir,
				sleep:      time.Sleep,
				purgeTrash: func() { purgeTrash(root, workdir, stderrLogf) },
			})
		},
		Teardown: func() (bool, string) {
			p, err := resolveTeardownPaths(env, os.Getuid())
			if err != nil {
				return false, fmt.Sprintf("[ocwarden teardown] cannot resolve paths: %v\n", err)
			}
			return doTeardown(newHostSeam().sys, env(dryRunEnv) == "1", p)
		},
		Exit:   os.Exit,
		Report: newCommandReporter(cfg),
	}
}

// newCommandReporter returns nil only when the server accepted the receipt.
// SYNCHRONOUS because uninstall must get its receipt to the server before the
// warden os.Exit()s; start/stop ignore the error.
func newCommandReporter(cfg Config) func(CommandResult) error {
	post := httpPoster(&http.Client{Timeout: commandReportTimeout}, cfg.Base, cfg.Token)
	return func(cr CommandResult) error {
		if cfg.Token == "" || cfg.ID == "" {
			return nil
		}
		if (strings.TrimSpace(cr.MemberID) == "" && strings.TrimSpace(cr.WorkerID) == "") ||
			strings.TrimSpace(cr.RPC) == "" {
			return nil
		}
		// No agent_id key: the frozen ingest schema refuses undeclared fields and would
		// 422 the whole receipt (the reporter is the verified JWT sub).
		payload := map[string]any{
			"command_result": map[string]any{
				"member_id": cr.MemberID,
				"worker_id": cr.WorkerID,
				"rpc":       cr.RPC,
				"ok":        cr.OK,
				"reason":    cr.Reason,
				"log":       cr.Log,
				"at":        cr.At,
			},
		}
		status, _ := post(commandResultPath, payload)
		if status < 200 || status >= 300 {
			return fmt.Errorf("command_result POST returned status %d", status)
		}
		return nil
	}
}

func newCommandTransport(cfg Config, deps CommandDeps, logf func(string, ...any)) *sseTransport {
	return &sseTransport{
		base:            cfg.Base,
		token:           cfg.Token,
		client:          newSSEClient(),
		deps:            deps,
		sleep:           time.Sleep,
		backoffStart:    sseBackoffStart,
		backoffCap:      sseBackoffCap,
		idleReadTimeout: sseIdleReadTimeout,
		logf:            logf,
	}
}
