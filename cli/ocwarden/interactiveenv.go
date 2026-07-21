package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// T-426d follow-up: INHERIT THE OWNER'S INTERACTIVE SHELL ENVIRONMENT.
//
// WHY (root cause, confirmed at three layers — measured, not reasoned):
//  1. warden's launchd plist supplies only a hard-coded minimal
//     EnvironmentVariables dict.
//  2. launchd NEVER sources ~/.zshrc — nothing in the launchd path does.
//  3. the spawn path is launchd -> tmux -> `zsh -c`, i.e. a NON-INTERACTIVE
//     shell, and zsh reads ~/.zshrc for INTERACTIVE shells only.
//
// Net effect measured 2026-07-20: 20 variables present in the owner's
// interactive shell are ABSENT from every spawned agent (11 of them
// credentials), plus PATH is missing three entries (~/.local/bin,
// ~/.asdf/shims, /opt/homebrew/sbin — the last of which is the real cause of
// the e2e suite's long-standing PATH workaround).
//
// The fix: at spawn time, ASK an interactive shell what its environment is and
// hand that to the agent as the BASE layer. The owner's dedicated env file
// (~/.officraft/env, agentenv.go) stays and becomes the OVERRIDE layer on top,
// so a single variable can be pinned or a value given that the interactive
// shell does not have. Owner ruled the scope: ALL variables, credentials
// included, and IDENTICAL for staff and outsourced members (his reasoning: the
// boundary belongs at "who may hire an outsourcer", not at starving an AI of
// tools).
//
// HOW WE ASK — and why NOT `export -p` (this is the load-bearing detail).
//
// The feasibility probe used `zsh -i -c 'export -p'`. Re-measuring its ACTUAL
// output on this host shows a trap that a KEY=value parser walks straight into:
//
//	export -T PATH path=( /Users/x/.local/bin /Users/x/.asdf/shims ... )
//	export -T FPATH fpath=( ... )
//	export -i10 SHLVL=1
//
// zsh renders TIED array parameters (PATH/path, FPATH/fpath) in ARRAY syntax,
// and attributed scalars with flag prefixes. A `KEY=value` parser silently
// DROPS PATH — the single most important variable this ticket exists to fix —
// and would have shipped looking green. `export -p` also quotes values in a
// shell dialect we would then have to unquote, which is where multi-line and
// embedded-quote credentials go wrong.
//
// `env -0` sidesteps all of it: NUL-delimited `KEY=VALUE` records, no quoting
// dialect, no array syntax, values may contain `=`, newlines, quotes, spaces —
// every byte is literal and the record boundary is a byte that cannot appear
// inside a value. Measured on this host: rc 0, zero stderr, PATH present as a
// proper colon-joined scalar including all three missing entries, ~0.12s.
//
// FAIL-SAFE IS ABSOLUTE. warden starts EVERY agent. If this path can break a
// spawn, one bad shell rc file takes the whole studio offline. Every failure
// below — shell missing, non-zero exit, timeout, empty output, malformed
// records — returns nil and the spawn continues on the existing minimal
// environment, with a warning on warden stderr (ocwarden.err.log).
//
// NOTHING HERE EVER LOGS A VALUE. The capture is a credential firehose. Values
// are dropped from the pipeline at the earliest possible point: the parser
// keeps a value only to put it in the pair, and no warning format string in
// this file has a value argument. Malformed records are reported by ORDINAL
// POSITION, never by content, because a record that failed the KEY=value shape
// may be a fragment of a value rather than a name.
// ---------------------------------------------------------------------------

// interactiveEnvShell is the shell asked for its interactive environment.
// ABSOLUTE: warden runs under launchd with a minimal PATH, and SHELL is not
// reliably set there, so neither PATH resolution nor $SHELL can be trusted.
const interactiveEnvShell = "/bin/zsh"

// interactiveEnvDumper is the argv run INSIDE that interactive shell. Absolute
// for the same reason as the shell itself; the owner's rc files may also leave
// PATH in any state at all, and this runs after they have executed.
const interactiveEnvDumper = "/usr/bin/env -0"

// interactiveEnvTimeout bounds the capture. The measured cost is ~0.12s; ten
// seconds is two orders of magnitude of headroom, so hitting it means the rc
// files are genuinely hung (a prompt for input, a network call), which is
// exactly the case that must not hang a spawn.
//
// Enforced with a Go context.WithTimeout + exec.CommandContext, NOT with a
// `timeout` wrapper command. macOS ships NO `timeout` binary (it is `gtimeout`
// from coreutils), and during the feasibility probe exactly that mistake
// produced rc=127 and very nearly got this entire approach written off as
// impossible. The timeout belongs in the caller's language, not in the argv.
const interactiveEnvTimeout = 10 * time.Second

// interactiveEnvMaxBytes caps the capture. A real environment is a few KB; the
// cap exists so a pathological rc file that prints forever cannot balloon the
// spawn path. Same intent as agentEnvMaxBytes.
const interactiveEnvMaxBytes = 1024 * 1024

// interactiveEnvSessionLocal is the ONLY subtraction from the owner's "give it
// all" ruling, and it contains ZERO credentials — every name here is shell or
// terminal BOOKKEEPING whose value describes the CAPTURING process and is
// actively WRONG when transplanted into the agent. This is a correctness
// exclusion, not a security one; narrowing what credentials an agent receives
// is the owner's call and is not being made here.
//
// Why each name is unsafe to inherit (the first two are demonstrated, not
// theoretical — both appear in a real capture on this host):
//
//   - PWD, OLDPWD: the launch line runs `cd <workdir>` and THEN sources the
//     rendered env file, so an inherited PWD overwrites the correct value with
//     the warden's working directory. The shell's actual directory stays the
//     workdir, so $PWD would LIE for the agent's whole lifetime, and every
//     tool that reads $PWD instead of calling getcwd() would resolve relative
//     paths against the wrong root. This is a silent-wrong-value bug of exactly
//     the class this ticket exists to eliminate.
//   - SHLVL, _: shell bookkeeping about the capturing shell. Stale and
//     meaningless in the agent; `_` in particular holds the last argv word.
//   - TMUX, TMUX_PANE: set whenever ocwarden is started from inside a tmux
//     pane (the ordinary DEV path — the launchd path does not have them).
//     Inheriting them makes every tmux command the AGENT runs believe it is
//     inside the OWNER's pane on the OWNER's server, targeting the owner's
//     session instead of the agent's. warden deliberately runs agents on its
//     own tmux socket; this would undo that.
//   - TERM, COLUMNS, LINES: describe the capturing terminal. The agent's real
//     terminal is its tmux pane, which sets these itself; overriding them
//     after the fact garbles the claude TUI's rendering.
//
// Everything else — all 11 credentials, the toolchain vars, PATH, the display
// vars — passes through untouched, per the ruling.
var interactiveEnvSessionLocal = map[string]bool{
	"PWD":       true,
	"OLDPWD":    true,
	"SHLVL":     true,
	"_":         true,
	"TMUX":      true,
	"TMUX_PANE": true,
	"TERM":      true,
	"COLUMNS":   true,
	"LINES":     true,
}

// captureInteractiveEnv is the real capture seam: it runs the interactive shell
// under a Go context timeout and returns its raw NUL-delimited stdout.
//
// It returns an error for every failure mode; the caller turns ALL of them into
// "spawn with the minimal environment" rather than a failed spawn.
func captureInteractiveEnv(shell string, timeout time.Duration) (string, error) {
	if shell == "" {
		shell = interactiveEnvShell
	}
	if timeout <= 0 {
		timeout = interactiveEnvTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// -i makes zsh read ~/.zshrc; -c takes the dumper as the command. The
	// interactive flag is the entire point — a non-interactive zsh is precisely
	// the environment the agent already has and this call exists to escape.
	cmd := exec.CommandContext(ctx, shell, "-i", "-c", interactiveEnvDumper)
	// No stdin. An interactive shell whose rc files prompt would otherwise block
	// on a terminal that does not exist; with stdin closed it gets EOF and the
	// timeout is the backstop rather than the primary defence.
	cmd.Stdin = nil
	var out, errb strings.Builder
	cmd.Stdout = &out
	// stderr is captured so a diagnosis lands in the warning, but it is NOT
	// merged into stdout: rc-file chatter on stderr must never be parsed as
	// environment records. Measured on this host: zero stderr lines.
	cmd.Stderr = &errb
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		// The shell's stderr can carry a value-free diagnosis ("no such file"),
		// but it can ALSO carry anything an rc file chose to print. It is not
		// forwarded to the log for that reason — only the exit status is.
		return "", fmt.Errorf("%s -i -c %q failed: %w", shell, interactiveEnvDumper, err)
	}
	raw := out.String()
	if len(raw) > interactiveEnvMaxBytes {
		return "", fmt.Errorf("output is %d bytes, over the %d cap", len(raw), interactiveEnvMaxBytes)
	}
	return raw, nil
}

// parseNulEnv parses NUL-delimited `KEY=VALUE` records into pairs, in the order
// the shell emitted them.
//
// Session-local names (interactiveEnvSessionLocal) and the warden's own OC_*
// identity namespace are dropped. The OC_* rule mirrors the env file's parser:
// letting the captured environment set OC_TOKEN / OC_BASE / OC_SESSION would
// let a stray export in the owner's .zshrc repoint an agent at another server
// or hand it another member's identity. That the launch line happens to export
// its own OC_* AFTER sourcing is an ordering side effect covering ~6 names, not
// a backstop for the rest — this check is the enforcement.
//
// warn receives NAMES and REASONS only. A record that fails the KEY=value shape
// is reported by its ORDINAL POSITION and its content is never formatted, since
// an unparseable record may be a fragment of a credential value.
func parseNulEnv(raw string, warn func(string, ...any)) []agentEnvPair {
	if warn == nil {
		warn = func(string, ...any) {}
	}
	var pairs []agentEnvPair
	index := map[string]int{}
	var skippedMalformed []int
	for n, rec := range strings.Split(raw, "\x00") {
		if rec == "" {
			// Trailing NUL produces one empty final record. Ordinary, not a fault.
			continue
		}
		eq := strings.Index(rec, "=")
		if eq <= 0 {
			skippedMalformed = append(skippedMalformed, n+1)
			continue
		}
		key := rec[:eq]
		if !agentEnvKeyRe.MatchString(key) {
			// The key POSITION did not hold a valid variable name, so whatever is
			// there is not a name and must not be echoed. Position only.
			skippedMalformed = append(skippedMalformed, n+1)
			continue
		}
		if interactiveEnvSessionLocal[key] {
			continue
		}
		if strings.HasPrefix(key, "OC_") {
			warn("interactive env: skipped %s — OC_* is warden-reserved (the agent's own identity)", key)
			continue
		}
		val := rec[eq+1:]
		// A NUL cannot appear inside a record by construction (it is the
		// delimiter), so unlike the env file there is no NUL check to do here.
		if i, dup := index[key]; dup {
			pairs[i].Value = val
			continue
		}
		index[key] = len(pairs)
		pairs = append(pairs, agentEnvPair{Key: key, Value: val})
	}
	if len(skippedMalformed) > 0 {
		warn("interactive env: skipped %d malformed record(s) at position(s) %s — not KEY=value; "+
			"content withheld from this log because an unparseable record may be part of a credential value",
			len(skippedMalformed), joinInts(skippedMalformed))
	}
	return pairs
}

// joinInts renders positions for the malformed-record warning. Positions are
// derived from the record index, never from record content.
func joinInts(ns []int) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, fmt.Sprint(n))
	}
	return strings.Join(parts, ",")
}

// mergeAgentEnv layers override ON TOP of base and returns the result.
//
// base is the captured interactive environment; override is the owner's
// ~/.officraft/env file. The file WINS on a name collision — that is what makes
// it an override layer: the owner can pin one variable to a different value, or
// supply one the interactive shell does not have, without touching .zshrc. An
// empty file therefore has literally no effect, which is the state every
// machine is in until the owner writes one.
//
// Order is deterministic: base order first (an overridden name keeps its base
// POSITION but takes the override VALUE), then override-only names in file
// order. Deterministic order matters because the rendered file's export order
// is observable and a churning order would make every spawn's render differ.
func mergeAgentEnv(base, override []agentEnvPair) []agentEnvPair {
	merged := make([]agentEnvPair, len(base))
	copy(merged, base)
	index := make(map[string]int, len(base))
	for i, p := range merged {
		index[p.Key] = i
	}
	for _, p := range override {
		if i, ok := index[p.Key]; ok {
			merged[i].Value = p.Value
			continue
		}
		index[p.Key] = len(merged)
		merged = append(merged, p)
	}
	return merged
}

// overriddenKeyNames returns the sorted names present in BOTH layers — the
// names the env file actually overrode. Names only; used for a log line that
// proves the precedence without printing either value.
func overriddenKeyNames(base, override []agentEnvPair) []string {
	inBase := make(map[string]bool, len(base))
	for _, p := range base {
		inBase[p.Key] = true
	}
	var names []string
	for _, p := range override {
		if inBase[p.Key] {
			names = append(names, p.Key)
		}
	}
	sort.Strings(names)
	return names
}
