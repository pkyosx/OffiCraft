package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultLoginCheckInterval = 300 * time.Second
	minLoginCheckIntervalSecs = 30
	maxLoginCheckIntervalSecs = 3600
)

const loginCheckEnvName = ".oc-login-check-env"

func loginCheckIntervalFromReceipt(body map[string]any) time.Duration {
	secs, ok := body["login_check_interval_secs"].(float64)
	if !ok || secs != float64(int64(secs)) ||
		secs < minLoginCheckIntervalSecs || secs > maxLoginCheckIntervalSecs {
		return defaultLoginCheckInterval
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

func (c *launchEnvCache) remember(pairs []agentEnvPair) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pairs = append([]agentEnvPair(nil), pairs...)
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

// Single-goroutine by contract: the telemetry loop both applies the receipt's
// interval and asks for the state.
type loginProber struct {
	env        func(string) string
	runner     CmdRunner
	keep       stdoutRunner
	goos       string
	now        func() time.Time
	claudeHome claudeHome
	agentHome  string
	envFile    string
	launchEnv  *launchEnvCache
	mkdirAll   func(path string, perm os.FileMode) error
	writeFile  func(path, content string, mode os.FileMode) error
	remove     func(name string) error
	logf       func(string, ...any)

	interval time.Duration
	checked  bool
	lastAt   time.Time
	last     loginState
}

func newLoginProber(env func(string) string, runner CmdRunner, keep stdoutRunner, goos string,
	launchEnv *launchEnvCache, logf func(string, ...any)) *loginProber {
	return &loginProber{
		env:        env,
		runner:     runner,
		keep:       keep,
		goos:       goos,
		now:        time.Now,
		claudeHome: resolvedClaudeHome(env, nil),
		agentHome:  defaultAgentHome(env),
		envFile:    defaultAgentEnvFile(env),
		launchEnv:  launchEnv,
		mkdirAll:   os.MkdirAll,
		writeFile:  osWriteFile,
		remove:     os.Remove,
		logf:       logf,
		interval:   defaultLoginCheckInterval,
	}
}

func (p *loginProber) setInterval(d time.Duration) { p.interval = d }

func (p *loginProber) state() loginState {
	if p.checked && p.now().Sub(p.lastAt) < p.interval {
		return p.last
	}
	p.last = loginState{Claude: p.claudeLoggedIn(), Codex: p.codexLoggedIn()}
	p.checked = true
	p.lastAt = p.now()
	return p.last
}

func (p *loginProber) log(format string, args ...any) {
	if p.logf != nil {
		p.logf(format, args...)
	}
}

func (p *loginProber) codexLoggedIn() *bool {
	bin := resolveCodexBin(p.env)
	if bin == "" {
		return nil
	}
	_, err := p.runner.Run(bin, "login", "status")
	// A false here conflates signed out, probe timeout, crash and wrong binary, and
	// placement fail-closes every codex member on the host on it. The error goes to
	// the local log only: it is subprocess stderr we cannot promise is
	// credential-free.
	if err != nil {
		p.log("[ocwarden runtimeprobe] codex login status failed (bin=%s): %v", bin, err)
	}
	ok := err == nil
	return &ok
}

// The stdout of `claude auth status` carries the account's email and
// organization: it is decoded into the one boolean and never logged or sent.
func (p *loginProber) claudeLoggedIn() *bool {
	bin := resolveClaudeBin(p.env)
	if bin == "" || p.claudeHome.Home == "" || p.keep == nil {
		return nil
	}
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
		return nil
	}
	if *status.LoggedIn {
		return status.LoggedIn
	}
	// A locked or unreadable login keychain makes a signed-in claude report
	// loggedIn:false — that is not a logout, so it stays unknown.
	if strings.HasPrefix(p.goos, "darwin") {
		keychain := filepath.Join(p.claudeHome.Home, "Library", "Keychains", "login.keychain-db")
		if _, kerr := p.runner.Run("security", "show-keychain-info", keychain); kerr != nil {
			p.log("[ocwarden runtimeprobe] claude reports logged out but the login keychain is unreadable; reporting unknown: %v", kerr)
			return nil
		}
	}
	return status.LoggedIn
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
