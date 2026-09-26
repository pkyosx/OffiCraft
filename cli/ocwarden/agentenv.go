package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The agent env file. Agents start from a NON-INTERACTIVE `zsh -c` (launchd →
// tmux), which never reads ~/.zshrc, so the owner's variables are absent unless
// supplied here. Owner ruled option C: a dedicated file on the agent launch path
// only — not ~/.zshenv (machine-wide credential exposure), not the plist.
//
// The file is data, never code: no $VAR expansion, no command substitution,
// values taken literally. It is parsed HERE; the launch line only sources a
// warden-rendered 0600 file of validated pairs, because the launch command line
// is visible via `ps` — credential values must never ride the argv.
//
// FAIL-OPEN by design: every failure path returns nil pairs and the spawn
// continues.

const agentEnvMaxBytes = 256 * 1024

const agentEnvRenderedName = ".oc-env"

var agentEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type agentEnvPair struct {
	Key   string
	Value string
}

// loadAgentEnv: logf carries KEY NAMES and reasons only — never a value; this
// file is where credentials live.
func loadAgentEnv(path string, logf func(string, ...any)) []agentEnvPair {
	warn := func(format string, a ...any) {
		if logf != nil {
			logf(format, a...)
		}
	}
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			warn("agent env: no env file at %s; spawning without extra env", path)
			return nil
		}
		warn("agent env: cannot stat %s (%v); spawning without extra env", path, err)
		return nil
	}
	if fi.IsDir() {
		warn("agent env: %s is a directory, not a file; spawning without extra env", path)
		return nil
	}
	if perm := fi.Mode().Perm(); perm&^0o600 != 0 {
		warn("agent env: WARNING %s mode is %04o, wider than 0600 — it holds credentials; run: chmod 600 %s",
			path, perm, path)
	}
	if fi.Size() > agentEnvMaxBytes {
		warn("agent env: %s is %d bytes, over the %d cap; spawning without extra env",
			path, fi.Size(), agentEnvMaxBytes)
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		warn("agent env: cannot read %s (%v); spawning without extra env", path, err)
		return nil
	}
	return parseAgentEnv(string(raw), path, warn)
}

func parseAgentEnv(raw, path string, warn func(string, ...any)) []agentEnvPair {
	var pairs []agentEnvPair
	index := map[string]int{}
	for n, line := range strings.Split(raw, "\n") {
		lineno := n + 1
		s := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		s = strings.TrimSpace(s)
		eq := strings.Index(s, "=")
		if eq <= 0 {
			warn("agent env: %s:%d skipped — not KEY=value (this file holds data only, never shell code)", path, lineno)
			continue
		}
		key := strings.TrimSpace(s[:eq])
		if !agentEnvKeyRe.MatchString(key) {
			warn("agent env: %s:%d skipped — %q is not a valid variable name", path, lineno, key)
			continue
		}
		// OC_* is the warden's own identity namespace (OC_TOKEN, OC_BASE, …); letting
		// this file set it would repoint an agent at another server or impersonate
		// another member.
		// 🔴 This is the ONLY enforcement: the launch line's export ordering happens to
		// override the few OC_* names it exports, and nothing downstream re-checks.
		if strings.HasPrefix(key, "OC_") {
			warn("agent env: %s:%d skipped — %s is warden-reserved (OC_* is the agent's own identity)", path, lineno, key)
			continue
		}
		// Pass the RAW right-hand side UNTRIMMED: the trailing-comment detector keys on
		// the whitespace before '#', and trimming would silently turn `K= # note` into
		// the value "# note".
		val, valWarn := parseAgentEnvValue(s[eq+1:])
		if valWarn != "" {
			warn("agent env: %s:%d %s — %s", path, lineno, key, valWarn)
		}
		if key == "PATH" {
			warn("agent env: %s:%d PATH REPLACES the whole search path, it does not append to it — "+
				"this value drops /bin, /usr/bin and everything else from the agent's PATH. "+
				"$PATH is NOT expanded in this file, so `PATH=/x:$PATH` does not work either. "+
				"Write every directory you need explicitly, e.g. PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
				path, lineno)
		}
		if strings.ContainsRune(val, 0) {
			warn("agent env: %s:%d skipped — %s contains a NUL byte, which cannot survive into the agent's environment", path, lineno, key)
			continue
		}
		if i, dup := index[key]; dup {
			warn("agent env: %s:%d %s redefined — later value wins", path, lineno, key)
			pairs[i].Value = val
			continue
		}
		index[key] = len(pairs)
		pairs = append(pairs, agentEnvPair{Key: key, Value: val})
	}
	return pairs
}

var trailingCommentRe = regexp.MustCompile(`\s#`)

// parseAgentEnvValue: '#' is legal inside a password, so an unquoted
// `K=abc # note` is kept LITERAL with a warning rather than guessed; only a
// closing quote (`K="abc" # note`) states where the value ends. Known silent
// case: `K=#note` (no whitespace before '#') is an ordinary literal value.
func parseAgentEnvValue(raw string) (string, string) {
	v := strings.TrimSpace(raw)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1], ""
		}
	}
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if j := strings.IndexByte(v[1:], v[0]); j >= 0 {
			closing := j + 1
			rest := strings.TrimSpace(v[closing+1:])
			if strings.HasPrefix(rest, "#") {
				return v[1:closing], ""
			}
			if rest != "" {
				return v, "value kept LITERALLY including the quote characters — the quotes do not wrap the whole value " +
					"(there is trailing text after the closing quote); quote the WHOLE value, e.g. KEY=\"a x\""
			}
		}
	}
	if trailingCommentRe.MatchString(raw) {
		return v, "value kept LITERALLY including the trailing '#...' — this file has no end-of-line comments on unquoted values; " +
			"write KEY=\"value\" # comment if you meant a comment, or ignore this if '#' is part of the value"
	}
	return v, ""
}

func renderAgentEnvFile(pairs []agentEnvPair) string {
	if len(pairs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# rendered by ocwarden from the agent env file — do not edit\n")
	for _, p := range pairs {
		fmt.Fprintf(&b, "export %s=%s\n", p.Key, shellQuote(p.Value))
	}
	return b.String()
}

func agentEnvKeyNames(pairs []agentEnvPair) []string {
	if len(pairs) == 0 {
		return nil
	}
	names := make([]string, 0, len(pairs))
	for _, p := range pairs {
		names = append(names, p.Key)
	}
	return names
}
