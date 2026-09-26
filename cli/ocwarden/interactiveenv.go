package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Inherit the owner's interactive shell environment: warden's launchd plist has a
// minimal env, launchd never sources ~/.zshrc, and the spawn's `zsh -c` is
// non-interactive. Owner ruled the scope: ALL variables, credentials included,
// IDENTICAL for staff and outsourced members.
//
// FAIL-SAFE IS ABSOLUTE: warden starts EVERY agent, so every capture failure returns
// nil and the spawn continues on the minimal environment — one bad rc file must not
// take the whole studio offline. NOTHING HERE EVER LOGS A VALUE: the capture is a
// credential firehose.
//
// Why `env -0` and NOT `zsh -i -c 'export -p'`: zsh renders tied arrays in array
// syntax (`export -T PATH path=( ... )`), so a KEY=value parser silently DROPS PATH
// and still looks green; export -p also quotes values in a shell dialect.

// Absolute: under launchd neither PATH nor $SHELL can be trusted.
const interactiveEnvShell = "/bin/zsh"

// Absolute too: the owner's rc files may leave PATH in any state.
const interactiveEnvDumper = "/usr/bin/env -0"

// Measured capture cost is ~0.12s. Enforced via context.WithTimeout, NOT a
// `timeout` wrapper: macOS ships no `timeout` binary (rc=127).
const interactiveEnvTimeout = 10 * time.Second

const interactiveEnvMaxBytes = 1024 * 1024

const interactiveEnvWaitDelay = 2 * time.Second

// interactiveEnvSessionLocal is the ONLY subtraction from the owner's "give it
// all" ruling: zero credentials, a correctness exclusion, not a security one.
//
//   - PWD, OLDPWD: the launch line runs `cd <workdir>` and THEN sources the
//     rendered env file, so an inherited PWD would lie for the agent's lifetime.
//   - SHLVL, _: bookkeeping about the capturing shell.
//   - TMUX, TMUX_PANE: set when ocwarden is started from a tmux pane; the agent's
//     tmux commands would then target the OWNER's server instead of warden's socket.
//   - TERM, COLUMNS, LINES: describe the capturing terminal; overriding the pane's
//     own values garbles the claude TUI.
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

func captureInteractiveEnv(shell string, timeout time.Duration) (string, error) {
	if shell == "" {
		shell = interactiveEnvShell
	}
	if timeout <= 0 {
		timeout = interactiveEnvTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-i", "-c", interactiveEnvDumper)
	// The deadline must bound the WHOLE TREE (measured): CommandContext kills only the
	// direct child, and Wait then blocks until every holder of the stdout pipe exits, so
	// an rc file's leftover grandchild stalls the spawn past the timeout.
	//   (1) Setsid + a Cancel that kills the whole process group. Setsid also matters
	//       when warden runs from a terminal: an rc file reading /dev/tty from a
	//       background group gets SIGTTIN and can stay stopped forever.
	//   (2) WaitDelay is what GUARANTEES a bounded return; (1) is best-effort, since a
	//       process can change its own group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = interactiveEnvWaitDelay
	cmd.Stdin = nil
	var out, errb strings.Builder
	cmd.Stdout = &out
	// stderr is NOT merged into stdout (rc-file chatter must never parse as records)
	// and is never logged: it can carry anything an rc file printed.
	cmd.Stderr = &errb
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return "", fmt.Errorf("%s -i -c %q failed: %w", shell, interactiveEnvDumper, err)
	}
	raw := out.String()
	if len(raw) > interactiveEnvMaxBytes {
		return "", fmt.Errorf("output is %d bytes, over the %d cap", len(raw), interactiveEnvMaxBytes)
	}
	return raw, nil
}

// The OC_* drop is the enforcement: a stray .zshrc export of OC_TOKEN / OC_BASE
// would repoint an agent at another server or identity. The launch line exporting
// its own OC_* after sourcing is an ordering side effect covering ~6 names, not a
// backstop.
func parseNulEnv(raw string, warn func(string, ...any)) []agentEnvPair {
	if warn == nil {
		warn = func(string, ...any) {}
	}
	var pairs []agentEnvPair
	index := map[string]int{}
	var skippedMalformed []int
	for n, rec := range strings.Split(raw, "\x00") {
		if rec == "" {
			continue
		}
		eq := strings.Index(rec, "=")
		if eq <= 0 {
			skippedMalformed = append(skippedMalformed, n+1)
			continue
		}
		key := rec[:eq]
		if !agentEnvKeyRe.MatchString(key) {
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

func joinInts(ns []int) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, fmt.Sprint(n))
	}
	return strings.Join(parts, ",")
}

// Order is deterministic because the rendered file's export order is observable;
// a churning order would make every spawn's render differ.
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
