// claudeprobe.go — the heartbeat's `claude` telemetry field: the version of the
// claude CLI a spawn would actually run (same plist-stamped env and
// resolveClaudeBin chain) plus the PRESENCE-ONLY shape of its credentials, so
// the cockpit's machine rows can diagnose it without an SSH session.
//
// The version exec is skipped while the resolved binary's (size, mtime) is
// unchanged: the native installer symlinks ~/.local/bin/claude → versions/<v>
// and os.Stat follows the symlink, so an upgrade changes the stat identity.
package main

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Telemetry cycles every 30s; two subprocesses per cycle would be too costly.
const claudeProbeTTL = 5 * time.Minute

// The claude CLI stores credentials either in this file or, when it does not
// write the file, in the macOS keychain item below.
const claudeCredFileRel = "/.claude/.credentials.json"

const claudeKeychainService = "Claude Code-credentials"

// Single-goroutine by contract: only the telemetry producer loop calls collect,
// so there is no lock.
type claudeProber struct {
	env        func(string) string
	resolveBin func() string
	stat       func(string) (os.FileInfo, error)
	readFile   func(string) ([]byte, error)
	runner     CmdRunner
	goos       string
	now        func() time.Time

	verPath  string
	verSize  int64
	verMtime time.Time
	verValue string

	cached   map[string]any
	cachedAt time.Time
}

func newClaudeProber(env func(string) string, runner CmdRunner, goos string) *claudeProber {
	return &claudeProber{
		env:        env,
		resolveBin: func() string { return resolveClaudeBin(env) },
		stat:       os.Stat,
		readFile:   os.ReadFile,
		runner:     runner,
		goos:       goos,
		now:        time.Now,
	}
}

// A failed probe omits its key: the server reads an absent key as unknown, so
// never report a guess.
func (p *claudeProber) collect() map[string]any {
	if p.cached != nil && p.now().Sub(p.cachedAt) < claudeProbeTTL {
		return p.cached
	}
	out := map[string]any{}
	if v := p.version(); v != "" {
		out["version"] = v
	}
	if home := p.env("HOME"); home != "" {
		credPath := home + claudeCredFileRel
		_, err := p.stat(credPath)
		credFile := err == nil
		out["cred_file"] = credFile
		subReadable := false
		if credFile {
			subReadable = claudeCredSubscriptionType(p.readFile, credPath) != ""
		}
		out["sub_readable"] = subReadable
	}
	if strings.HasPrefix(p.goos, "darwin") {
		// NO -w: metadata only, the secret payload is never requested (and a
		// metadata query does not trip the keychain ACL). exit 0 = item present.
		_, err := p.runner.Run("security", "find-generic-password", "-s", claudeKeychainService)
		out["keychain"] = err == nil
	}
	p.cached = out
	p.cachedAt = p.now()
	return out
}

func (p *claudeProber) version() string {
	bin := p.resolveBin()
	if bin == "" {
		p.verPath = ""
		return ""
	}
	info, err := p.stat(bin)
	if err != nil || info.IsDir() {
		p.verPath = ""
		return ""
	}
	if p.verPath == bin && p.verSize == info.Size() && p.verMtime.Equal(info.ModTime()) {
		return p.verValue
	}
	out, err := p.runner.Run(bin, "--version")
	fields := strings.Fields(out)
	if err != nil || len(fields) == 0 {
		p.verPath = ""
		return ""
	}
	p.verPath, p.verSize, p.verMtime, p.verValue = bin, info.Size(), info.ModTime(), fields[0]
	return p.verValue
}

// The file holds live OAuth tokens: decode ONLY into a typed struct binding the
// one non-secret field, so tokens are never held, printed, or put in an error.
func claudeCredSubscriptionType(readFile func(string) ([]byte, error), path string) string {
	raw, err := readFile(path)
	if err != nil {
		return ""
	}
	var cred struct {
		ClaudeAiOauth struct {
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &cred) != nil {
		return ""
	}
	return strings.TrimSpace(cred.ClaudeAiOauth.SubscriptionType)
}
