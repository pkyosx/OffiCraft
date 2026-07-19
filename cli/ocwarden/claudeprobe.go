// claudeprobe.go — the heartbeat's local claude CLI probe (T-97ee stage 1).
//
// Every telemetry cycle may ride an extra `claude` field: the version of the
// claude CLI this host's agents would actually run (the SAME resolveClaudeBin
// chain the spawn path uses) plus the PRESENCE-ONLY shape of its credentials
// (cred file exists / subscriptionType readable / macOS keychain item exists).
// The server folds it into the machine rows so this host's credential shape
// (which credential source exists, whether the cred file's subscriptionType
// is readable) is diagnosable from the cockpit at a glance instead of an SSH
// session. NOTE (T-f694): the account key no longer includes subscriptionType
// — ocagent does not read the credentials file at all — so an unreadable sub
// can no longer split an account into duplicate monitoring rows; this probe
// stays as a pure machine-observability face.
//
// Deliberately NEVER a secret: version comes from `--version`, the cred file
// is decoded into a typed struct binding ONLY subscriptionType, and the
// keychain check runs `security find-generic-password` WITHOUT -w
// (item metadata only, the secret payload is never requested).
//
// Cost control (the fingerprint.go pattern — injectable seams, single
// goroutine, fail-soft, cache): the whole probe group refreshes at most once
// per claudeProbeTTL (5m); within a refresh the version exec is skipped when
// the resolved binary's (size, mtime) stat identity is unchanged (the native
// installer symlinks ~/.local/bin/claude → versions/<v>, so an upgrade swaps
// the stat identity; os.Stat follows the symlink). Most 30s cycles cost
// nothing; a TTL refresh mostly costs a couple of stats.
package main

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// claudeProbeTTL is how long one probe result is served before re-probing.
// Credential-shape drift (login/logout/upgrade) being visible at minute
// granularity is plenty; two subprocesses every 30s would not be.
const claudeProbeTTL = 5 * time.Minute

// claudeCredFileRel is the credentials file location the claude CLI writes
// when it does not store credentials in the macOS Keychain. ocagent no longer
// reads this file (T-f694 dropped subscriptionType from the account key);
// the probe reports its shape purely as machine observability.
const claudeCredFileRel = "/.claude/.credentials.json"

// claudeKeychainService is the macOS login-keychain service name the claude
// CLI stores its credentials under when it does not write the file.
const claudeKeychainService = "Claude Code-credentials"

// claudeProber probes the local claude CLI (version + credential shape) for
// the telemetry heartbeat. Single-goroutine by contract: only the telemetry
// producer loop calls collect (no lock needed). Every IO edge is an injectable
// seam so tests drive it with fakes — no real exec, stat, read, or keychain.
type claudeProber struct {
	env        func(string) string
	resolveBin func() string // the spawn path's resolveClaudeBin chain
	stat       func(string) (os.FileInfo, error)
	readFile   func(string) ([]byte, error)
	runner     CmdRunner // `claude --version` + `security find-generic-password`
	goos       string
	now        func() time.Time

	// version identity cache: re-exec --version only when the resolved
	// binary's stat identity changed (fingerprint.go's cache shape).
	verPath  string
	verSize  int64
	verMtime time.Time
	verValue string

	// whole-group TTL cache.
	cached   map[string]any
	cachedAt time.Time
}

// newClaudeProber wires the real seams. The env func is the SAME plist-stamped
// env the transport resolves claude from, so the probe reports the exact
// binary a spawn would use (OC_CLAUDE_BIN → PATH → common dirs).
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

// collect returns the current claude probe map (empty = nothing probed →
// buildTelemetryPayload omits the field). Serves the cached group inside the
// TTL window; a refresh is fail-soft per item — a failed probe omits that key
// (server reads absent as unknown), never reports a guess.
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
		// sub_readable: true ONLY when the file exists AND subscriptionType is
		// non-blank. Pure observability of the credential file's shape — the
		// account key no longer depends on it (T-f694).
		subReadable := false
		if credFile {
			subReadable = claudeCredSubscriptionType(p.readFile, credPath) != ""
		}
		out["sub_readable"] = subReadable
	}
	// Keychain presence is darwin-only (login keychain); elsewhere the key is
	// simply absent → the server synthesizes as much as the remaining probes
	// allow (honest unknown, never a fabricated false).
	if strings.HasPrefix(p.goos, "darwin") {
		// NO -w: metadata-only lookup, the secret payload is never requested
		// (and a metadata query does not trip the keychain ACL). exit 0 =
		// item present, non-zero = absent.
		_, err := p.runner.Run("security", "find-generic-password", "-s", claudeKeychainService)
		out["keychain"] = err == nil
	}
	p.cached = out
	p.cachedAt = p.now()
	return out
}

// version resolves the claude binary (the spawn chain) and returns its
// `--version` first token ("2.1.211 (Claude Code)" → "2.1.211"), "" on any
// miss (unresolved / stat fault / exec fault / empty output) — fail-soft, the
// payload just omits the field. The exec is skipped while the resolved path's
// (size, mtime) stat identity matches the cache.
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

// claudeCredSubscriptionType extracts claudeAiOauth.subscriptionType from the
// credentials file at path, "" on any miss (no file / bad json / field blank).
// The file holds live OAuth tokens: decode ONLY into a typed struct that binds
// the one non-secret field, so accessToken/refreshToken are never held in a
// variable, printed, or surfaced in an error.
//
// Historically a LITERAL COPY of ocagent's claudeSubscriptionType; that
// function was removed by T-f694 (the account key no longer includes the
// plan), so this probe-side reader is now the only copy.
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
