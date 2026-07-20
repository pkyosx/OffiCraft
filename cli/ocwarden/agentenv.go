package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// T-426d: the agent env file.
//
// WHY this exists. An agent is started by launchd → `tmux new-session` → a
// NON-INTERACTIVE, NON-LOGIN `zsh -c`. zsh reads ~/.zshrc only for INTERACTIVE
// shells, so everything the owner keeps there (measured 2026-07-20: 21 vars, 11
// of them credentials, plus three PATH entries — ~/.local/bin, ~/.asdf/shims,
// /opt/homebrew/sbin) is simply ABSENT from every spawned agent. The warden's
// plist contributes only its explicit EnvironmentVariables dict.
//
// Owner ruled option C: load a DEDICATED env file on the agent launch path
// only — not ~/.zshenv (machine-wide credential exposure) and not the plist
// (credentials in cleartext + a redeploy per new variable).
//
// FORMAT (deliberately NOT a shell). This file is data, never code:
//
//	# comment
//	KEY=value
//	export KEY=value          (the `export ` prefix is tolerated, not required)
//	KEY="value with spaces"   (one matched surrounding quote pair is stripped)
//
// There is NO expansion of $VAR, no `` ` `` command substitution, no
// conditionals, no sourcing of other files. Values are taken LITERALLY. This is
// the whole point of failure-mode ③ in the option-C writeup: the file must not
// become a second .zshrc that runs arbitrary init logic on every spawn.
//
// The parse happens HERE, in Go. The launch line never sources the owner's file
// directly — the shell would happily execute whatever is in it. Instead the
// warden renders the VALIDATED pairs into a 0600 file in the agent workdir and
// the launch line sources that. Same reason the tmux argv carries only the
// tokenFile path and never the JWT: the launch command line is machine-visible
// via `ps`, so credential VALUES must ride a 0600 file, never the argv.
//
// FAIL-OPEN, deliberately (failure mode ②). A missing env file is NOT an error
// — it means "no extra variables", which is precisely the state every agent is
// in today. Nothing here may ever prevent an agent from booting: every failure
// path below returns nil pairs and lets the spawn continue.
// ---------------------------------------------------------------------------

// agentEnvMaxBytes caps the read so a pathological file cannot balloon the
// launch path. Far above any plausible credential set.
const agentEnvMaxBytes = 256 * 1024

// agentEnvRenderedName is the workdir file the launch line actually sources:
// warden-rendered from validated pairs, 0600, rewritten on every spawn.
const agentEnvRenderedName = ".oc-env"

// agentEnvKeyRe is the accepted key shape — a POSIX-portable shell name.
// Anything else (spaces, dashes, leading digits, empty) is skipped with a
// warning rather than silently mangled.
var agentEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// agentEnvPair is one validated KEY=value. Value is LITERAL — never expanded.
type agentEnvPair struct {
	Key   string
	Value string
}

// loadAgentEnv reads and parses the agent env file at path.
//
// It returns the validated pairs in file order, last-wins on duplicate keys. It
// returns nil — never an error — for every degraded case: no path configured,
// file absent, path is a directory, unreadable, or oversized. Callers MUST be
// able to proceed with nil (fail-open).
//
// logf (nil-skipped) is the observability channel. It carries KEY NAMES and
// reasons ONLY — never a value, since this file is where credentials live.
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
			// The ORDINARY case on a fresh machine. Not a warning — an absent
			// file means "no extra variables", which is a valid configuration.
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
	// Failure mode ③ of the option-C writeup: this file holds credentials, so a
	// mode wider than 0600 must produce an OBSERVABLE signal. It is a warning,
	// not a refusal — refusing here would convert a permissions nit into an
	// agent that will not boot, which is exactly the fail-closed behaviour the
	// owner ruled against. The owner gets a loud line in ocwarden.err.log.
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

// parseAgentEnv is the PURE parser — split out from the I/O so the format
// contract is testable without touching a filesystem.
//
// A malformed line is SKIPPED with a warning; it never aborts the parse. One
// typo must not cost the agent the other ten variables.
func parseAgentEnv(raw, path string, warn func(string, ...any)) []agentEnvPair {
	var pairs []agentEnvPair
	index := map[string]int{} // key → position in pairs, for last-wins
	for n, line := range strings.Split(raw, "\n") {
		lineno := n + 1
		s := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		// A bare `export ` prefix is tolerated: it is what an owner's fingers
		// type out of .zshrc habit, and stripping it costs nothing. It is a
		// PREFIX STRIP, not shell parsing — `export A=1 B=2` still fails the
		// key check below and is skipped, rather than half-working.
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
		// OC_* is the warden's OWN identity namespace (OC_TOKEN, OC_BASE,
		// OC_SESSION, OC_TMUX_SOCKET, OC_AGENT_HOME, OC_EFFORT). Letting the env
		// file set those would let it repoint an agent at another server or
		// impersonate another member. Refused outright — not merely overridden.
		//
		// THIS IS THE ONLY ENFORCEMENT of the OC_* rule. The launch line's export
		// ordering happens to override the ~6 OC_* names it actually exports, but
		// that is a side effect of ordering, not a backstop: it does nothing for
		// any other OC_* name. Do not weaken this check on the belief that
		// something downstream re-checks it — nothing does.
		if strings.HasPrefix(key, "OC_") {
			warn("agent env: %s:%d skipped — %s is warden-reserved (OC_* is the agent's own identity)", path, lineno, key)
			continue
		}
		val, valWarn := parseAgentEnvValue(strings.TrimSpace(s[eq+1:]))
		if valWarn != "" {
			warn("agent env: %s:%d %s — %s", path, lineno, key, valWarn)
		}
		// F-1: a NUL cannot survive the trip through the rendered file into the
		// shell (C strings end at the first NUL), so the agent would silently
		// receive a TRUNCATED or empty value. Same failure shape as the trailing
		// `#` above — a wrong value that looks like a working one — so it gets
		// the same treatment: refuse the line and say why, never half-deliver it.
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

// trailingCommentRe matches the "whitespace then #" shape of an intended
// end-of-line comment on an UNQUOTED value.
var trailingCommentRe = regexp.MustCompile(`\s#`)

// parseAgentEnvValue turns the raw right-hand side into the final value, plus a
// warning string ("" when there is nothing to say).
//
// It strips ONE matched surrounding quote pair, so `K="a b"` and `K='a b'` both
// mean the two-word value. Everything inside is literal either way — there is no
// difference between the quote styles here, unlike in a shell.
//
// TRAILING `#` (reviewer F-4). `K=value # comment` is genuinely ambiguous: `#`
// is a perfectly legal character inside a password, so there is no reading of a
// bare line that is right in every case. The resolution turns on whether the
// owner told us where the value ENDS:
//
//   - QUOTED, comment outside the quotes — `K="abc" # note` — the closing quote
//     states the boundary explicitly. Intent is unambiguous, so the comment is
//     stripped and the value is `abc`. No warning: nothing was guessed.
//   - UNQUOTED — `K=abc # note` — the boundary is unknown. The value is kept
//     LITERAL (`abc # note`) and a warning fires. Guessing here would silently
//     truncate a credential that legitimately contains " #", which is a worse
//     bug than the one being fixed and is invisible when it happens.
//
// Neither branch ever silently produces a value the owner did not intend: the
// explicit case is honoured, the ambiguous case is preserved AND announced. That
// matters because this whole ticket exists to kill a class of silent-wrong-env
// failure that looks like "the tool is broken" rather than "I mistyped".
func parseAgentEnvValue(v string) (string, string) {
	// (1) Fully quoted, nothing after the closing quote — the common case.
	// Handled first so every previously-accepted form stays byte-identical.
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1], ""
		}
	}
	// (2) Quoted value followed by a trailing comment: the quote delimits the
	// value, so the comment is unambiguously not part of it.
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if j := strings.IndexByte(v[1:], v[0]); j >= 0 {
			closing := j + 1
			if rest := strings.TrimSpace(v[closing+1:]); strings.HasPrefix(rest, "#") {
				return v[1:closing], ""
			}
		}
	}
	// (3) Unquoted with a comment-shaped tail: keep it literal, but say so —
	// the owner must not discover this by watching the agent misbehave.
	if trailingCommentRe.MatchString(v) {
		return v, "value kept LITERALLY including the trailing '#...' — this file has no end-of-line comments on unquoted values; " +
			"write KEY=\"value\" # comment if you meant a comment, or ignore this if '#' is part of the value"
	}
	return v, ""
}

// renderAgentEnvFile renders validated pairs into the content of the workdir
// 0600 file the launch line sources. Every value is shellQuote'd, so the
// rendered file is exports and nothing else — sourcing it CANNOT execute
// anything the owner's file smuggled in, because the owner's bytes never reach
// the shell as code, only as a quoted literal.
//
// Empty pairs render "" and the caller writes no file at all.
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

// agentEnvKeyNames returns just the KEY NAMES, for a log line that proves what
// was loaded WITHOUT printing a single value.
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
