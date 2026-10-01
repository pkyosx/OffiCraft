package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The interval applies while a runtime's last verdict is logged in, the recheck
// interval while it is logged out or unknown. Both share the range.
const (
	defaultLoginCheckInterval   = 300 * time.Second
	defaultLoginRecheckInterval = 30 * time.Second
	minLoginCheckIntervalSecs   = 30
	maxLoginCheckIntervalSecs   = 3600
)

const loginCheckEnvName = ".oc-login-check-env"

// spawnCheckBudget bounds a spawn's login check, its wait for a running periodic
// check included: past it the verdict is unknown and the spawn launches. The
// periodic check can hold the prober for ~25 s (shell capture, auth status and
// keychain on claude, then codex), and the START receipt has to reach the
// server inside its receiptDeadlineSecs (server/ocserverd/receipt_watch.go),
// whose derivation counts this budget. 🔴 Raising it eats that slack; nothing
// links the two modules but spawn_test.go's literal.
const spawnCheckBudget = 15 * time.Second

func loginIntervalFromReceipt(body map[string]any, key string, fallback time.Duration) time.Duration {
	secs, ok := body[key].(float64)
	if !ok || secs != float64(int64(secs)) ||
		secs < minLoginCheckIntervalSecs || secs > maxLoginCheckIntervalSecs {
		return fallback
	}
	return time.Duration(secs) * time.Second
}

type stdoutRunner interface {
	RunKeepStdout(name string, args ...string) (string, error)
}

// launchEnvCache holds the interactive-shell layer the most recent spawn
// captured, so a login check sees the member's environment without starting an
// interactive shell of its own every interval.
type launchEnvCache struct {
	mu    sync.Mutex
	pairs []agentEnvPair
}

// A spawn whose capture failed keeps the last good layer: the login check
// captures on its own only once, so a cleared cache would never refill.
func (c *launchEnvCache) remember(pairs []agentEnvPair) {
	if c == nil || len(pairs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pairs = append([]agentEnvPair(nil), pairs...)
}

// A spawn that raced ahead of the login check's own capture holds the fresher
// layer, so it is not overwritten.
func (c *launchEnvCache) rememberIfEmpty(pairs []agentEnvPair) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pairs) == 0 {
		c.pairs = append([]agentEnvPair(nil), pairs...)
	}
}

func (c *launchEnvCache) interactive() []agentEnvPair {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agentEnvPair(nil), c.pairs...)
}

// nil = unknown, which placement must never read as logged out.
type loginState struct {
	Claude *bool
	Codex  *bool
}

// The telemetry loop applies the receipt's interval and asks for the state; a
// spawn asks for a fresh verdict from the command goroutine (checkNow). mu
// serializes the two.
type loginProber struct {
	mu          sync.Mutex
	kick        chan struct{}
	spawnBudget time.Duration

	env        func(string) string
	runner     CmdRunner
	keep       stdoutRunner
	goos       string
	now        func() time.Time
	claudeHome claudeHome
	agentHome  string
	envFile    string
	launchEnv  *launchEnvCache
	captureEnv func() (string, error)
	mkdirAll   func(path string, perm os.FileMode) error
	writeFile  func(path, content string, mode os.FileMode) error
	remove     func(name string) error
	logf       func(string, ...any)

	interval     time.Duration
	recheck      time.Duration
	captureTried bool
	claude       runtimeLogin
	codex        runtimeLogin
}

type runtimeLogin struct {
	checked     bool
	at          time.Time
	verdict     *bool
	notLoggedIn bool
}

func newLoginProber(env func(string) string, runner CmdRunner, keep stdoutRunner, goos string,
	launchEnv *launchEnvCache, logf func(string, ...any)) *loginProber {
	return &loginProber{
		env:         env,
		runner:      runner,
		keep:        keep,
		goos:        goos,
		now:         time.Now,
		claudeHome:  resolvedClaudeHome(env, nil),
		agentHome:   defaultAgentHome(env),
		envFile:     defaultAgentEnvFile(env),
		launchEnv:   launchEnv,
		captureEnv:  defaultCaptureEnv(env),
		mkdirAll:    os.MkdirAll,
		writeFile:   osWriteFile,
		remove:      os.Remove,
		logf:        logf,
		interval:    defaultLoginCheckInterval,
		recheck:     defaultLoginRecheckInterval,
		kick:        make(chan struct{}, 1),
		spawnBudget: spawnCheckBudget,
	}
}

func (p *loginProber) setIntervals(check, recheck time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interval = check
	p.recheck = recheck
}

func (p *loginProber) state() loginState {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refresh(&p.claude, p.claudeLoggedIn)
	p.refresh(&p.codex, p.codexLoggedIn)
	return loginState{Claude: p.claude.verdict, Codex: p.codex.verdict}
}

// checkNow runs runtime's login check regardless of the interval, keeps the
// verdict as the one the next heartbeat reports, and asks the telemetry loop to
// send that heartbeat now. nil = unknown.
func (p *loginProber) checkNow(runtime string) *bool {
	p.mu.Lock()
	var verdict *bool
	switch runtime {
	case "claude":
		verdict = p.record(&p.claude, p.claudeLoggedIn)
	case "codex":
		verdict = p.record(&p.codex, p.codexLoggedIn)
	default:
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	select {
	case p.kick <- struct{}{}:
	default:
	}
	return verdict
}

// checkForSpawn is checkNow within spawnBudget. A check that overruns keeps
// running, so its verdict still reaches the report when it lands.
func (p *loginProber) checkForSpawn(runtime string) *bool {
	done := make(chan *bool, 1)
	go func() { done <- p.checkNow(runtime) }()
	timer := time.NewTimer(p.spawnBudget)
	defer timer.Stop()
	select {
	case verdict := <-done:
		return verdict
	case <-timer.C:
		p.log("[ocwarden runtimeprobe] %s login check passed its %s spawn budget; launching on an unknown verdict", runtime, p.spawnBudget)
		return nil
	}
}

// kicked fires once after any checkNow the telemetry loop has not yet reported.
func (p *loginProber) kicked() <-chan struct{} {
	return p.kick
}

// probe reports whether it ran a check at all; a runtime that is not installed
// stays on the interval rather than being re-resolved every heartbeat.
//
// A 30 s recheck means every heartbeat only because the loop sleeps 30 s AFTER
// each beat, so consecutive checks are never less than 30 s apart. A fixed-rate
// ticker would make that 30 s land a hair short and silently double the cadence.
func (p *loginProber) refresh(r *runtimeLogin, probe func() (verdict *bool, probed bool)) {
	wait := p.interval
	if r.notLoggedIn {
		wait = p.recheck
	}
	if r.checked && p.now().Sub(r.at) < wait {
		return
	}
	p.record(r, probe)
}

func (p *loginProber) record(r *runtimeLogin, probe func() (verdict *bool, probed bool)) *bool {
	verdict, probed := probe()
	r.verdict = verdict
	r.checked = true
	r.at = p.now()
	r.notLoggedIn = probed && (verdict == nil || !*verdict)
	return verdict
}

func (p *loginProber) log(format string, args ...any) {
	if p.logf != nil {
		p.logf(format, args...)
	}
}

func (p *loginProber) codexLoggedIn() (*bool, bool) {
	bin := resolveCodexBin(p.env)
	if bin == "" {
		return nil, false
	}
	_, err := p.runner.Run(bin, "login", "status")
	if err == nil {
		ok := true
		return &ok, true
	}
	// The error goes to the local log only: it is subprocess stderr we cannot
	// promise is credential-free.
	p.log("[ocwarden runtimeprobe] codex login status failed (bin=%s): %v", bin, err)
	// Only a non-zero exit is codex saying "not logged in"; a timeout or a binary
	// that would not start is unknown, and a spawn goes ahead on unknown.
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return nil, true
	}
	loggedOut := false
	return &loggedOut, true
}

// The stdout of `claude auth status` carries the account's email and
// organization: it is decoded into the one boolean and never logged or sent.
func (p *loginProber) claudeLoggedIn() (*bool, bool) {
	bin := resolveClaudeBin(p.env)
	if bin == "" || p.claudeHome.Home == "" || p.keep == nil {
		return nil, false
	}
	p.ensureLaunchEnv()
	cmd, rendered := p.claudeStatusCommand(bin)
	out, err := p.keep.RunKeepStdout(p.shell(), "-c", cmd)
	if rendered != "" {
		_ = p.remove(rendered)
	}
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &status) != nil || status.LoggedIn == nil {
		if err != nil {
			p.log("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s): %v", bin, err)
		} else {
			p.log("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s)", bin)
		}
		return nil, true
	}
	if *status.LoggedIn {
		return status.LoggedIn, true
	}
	// Without the owner's interactive shell the check misses a credential that
	// ~/.zshrc exports (API key, Bedrock, Vertex), so its false proves nothing —
	// unless capture is switched off, when members launch without that shell too.
	if p.captureEnv != nil && len(p.launchEnv.interactive()) == 0 {
		p.log("[ocwarden runtimeprobe] claude reports logged out but the interactive shell env is unavailable; reporting unknown")
		return nil, true
	}
	// A locked or unreadable login keychain makes a signed-in claude report
	// loggedIn:false — that is not a logout, so it stays unknown.
	if strings.HasPrefix(p.goos, "darwin") {
		keychain := filepath.Join(p.claudeHome.Home, "Library", "Keychains", "login.keychain-db")
		if _, kerr := p.runner.Run("security", "show-keychain-info", keychain); kerr != nil {
			p.log("[ocwarden runtimeprobe] claude reports logged out but the login keychain is unreadable; reporting unknown: %v", kerr)
			return nil, true
		}
	}
	return status.LoggedIn, true
}

// Before the first spawn the shared cache is empty; the check captures the shell
// once itself rather than run without it, and a later spawn refreshes the cache.
func (p *loginProber) ensureLaunchEnv() {
	if p.captureTried || p.captureEnv == nil || len(p.launchEnv.interactive()) > 0 {
		return
	}
	p.captureTried = true
	raw, err := p.captureEnv()
	if err != nil {
		p.log("[ocwarden runtimeprobe] interactive env: capture failed (%v)", err)
		return
	}
	p.launchEnv.rememberIfEmpty(parseNulEnv(raw, p.logf))
}

func (p *loginProber) shell() string {
	if strings.HasPrefix(p.goos, "darwin") {
		return interactiveEnvShell
	}
	return "/bin/sh"
}

// The render holds the env file's credentials, so the caller removes it as soon
// as the command has run.
func (p *loginProber) claudeStatusCommand(bin string) (string, string) {
	rendered := ""
	pairs := mergeAgentEnv(p.launchEnv.interactive(), loadAgentEnv(p.envFile, nil))
	if len(pairs) > 0 && p.agentHome != "" {
		path := filepath.Join(p.agentHome, loginCheckEnvName)
		if err := p.mkdirAll(p.agentHome, 0o700); err == nil {
			if err := p.writeFile(path, renderAgentEnvFile(pairs), 0o600); err == nil {
				rendered = path
			}
		}
	}
	dir := p.agentHome
	if dir == "" {
		dir = p.claudeHome.Home
	}
	cmd := claudeChildEnvPrologue(dir, rendered, p.claudeHome)
	if exports := claudeHomeExportPairs(p.claudeHome); len(exports) > 0 {
		kvs := make([]string, 0, len(exports))
		for _, kv := range exports {
			kvs = append(kvs, kv[0]+"="+shellQuote(kv[1]))
		}
		cmd += "export " + strings.Join(kvs, " ") + "; "
	}
	return cmd + "exec " + shellQuote(bin) + " auth status", rendered
}
