package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// suicide: ocagent suicide  (graceful SSE-driven self-kill)
// ---------------------------------------------------------------------------
//
// The GRACEFUL self-termination lever. After the winddown/recycle hooks report
// phase=stopped over the presence wire, the agent kills its OWN tmux session:
// claude + this listener + every child drop, so the SSE downlink drops and the
// server derives OFFLINE from the connection fact — BEFORE the grace clock
// elapses, with no second actor required. The warden's killpg ladder is the
// UNTOUCHED force fallback for a crashed/wedged agent that never reaches here.
//
// It kills the SAME session the listener probes for its self-exit lifecycle tie:
// OC_SESSION (`member-<id>`) on the OC_TMUX_SOCKET (`officraft`) socket, both
// injected by the spawn shim (cli/ocwarden/spawn.go). No OC_SESSION (a headless /
// test run) ⇒ NOTHING to kill ⇒ clean no-op — never guess a session to destroy.
// The lever is `tmux -L <socket> kill-session -t <session>` (SIGHUP-first, the
// same lightweight leg the warden's killSession uses) — it takes down the whole
// pane tree INCLUDING the process running `suicide`.

// tmuxKiller runs `tmux -L <socket> kill-session -t <session>` and returns any
// error. Injected so a test asserts the argv without spawning tmux (the real one
// SIGHUPs this very process, so a successful kill never returns).
type tmuxKiller func(bin, socket, session string) error

// realTmuxKill is the production killer: `tmux -L <socket> kill-session -t
// <session>`, bounded by a short context so a wedged tmux cannot hang the exit.
func realTmuxKill(bin, socket, session string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, "-L", socket, "kill-session", "-t", session).Run()
}

// suicideSession resolves (socket, session) for the self-kill from the launch env,
// or ok=false when OC_SESSION is unset — a headless run has no session to kill, so
// the caller no-ops rather than guess. Mirrors makeSessionProbe's env reading (same
// OC_SESSION / OC_TMUX_SOCKET, same defaultTmuxSocket fallback).
func suicideSession(env func(string) string) (socket, session string, ok bool) {
	session = strings.TrimSpace(env("OC_SESSION"))
	if session == "" {
		return "", "", false
	}
	socket = strings.TrimSpace(env("OC_TMUX_SOCKET"))
	if socket == "" {
		socket = defaultTmuxSocket
	}
	return socket, session, true
}

// cmdSuicide implements `ocagent suicide`: kill this agent's own tmux session so
// the SSE drops and the server derives offline. Always returns 0 — a self-kill is
// best-effort + fire-and-forget (a mis-wire / already-gone session must never be a
// non-zero, alarming exit; the warden killpg is the force fallback either way).
func cmdSuicide(cfg Config, env func(string) string, out io.Writer) int {
	return runSuicide(env, out, resolveTmuxBin, realTmuxKill)
}

// runSuicide is the testable core: resolve the session, then kill it via the
// injected bin resolver + killer. resolveBin==""/no-session/kill-error all degrade
// to a logged no-op (return 0) — the SSE drop is what matters, never this call's
// own status. Mirrors the probe-disabled convention (no OC_SESSION ⇒ do nothing).
func runSuicide(env func(string) string, out io.Writer, resolveBin func() string, kill tmuxKiller) int {
	socket, session, ok := suicideSession(env)
	if !ok {
		fmt.Fprint(out, "[ocagent] suicide: no OC_SESSION — nothing to kill; exiting.\n")
		return 0
	}
	bin := resolveBin()
	if bin == "" {
		fmt.Fprint(out, "[ocagent] suicide: tmux unresolvable — cannot self-kill; "+
			"leaving the warden killpg as the fallback.\n")
		return 0
	}
	fmt.Fprintf(out, "[ocagent] suicide: tmux -L %s kill-session -t %s — dropping my SSE "+
		"so the server derives offline before the grace deadline.\n", socket, session)
	if err := kill(bin, socket, session); err != nil {
		// A successful kill SIGHUPs this very process, so it never returns; a returned
		// error means the session was already gone / unkillable — either way the SSE
		// drops (or is already dropped). Log HONESTLY, never mask, never fail hard.
		fmt.Fprintf(out, "[ocagent] suicide: kill-session returned %v "+
			"(session likely already gone) — the warden killpg remains the fallback.\n", err)
	}
	return 0
}
