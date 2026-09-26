package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Kills this agent's own tmux session: the one cli/ocwarden/spawn.go injects via
// OC_SESSION/OC_TMUX_SOCKET. Callers: the `ocagent suicide` subcommand and the
// listener's fail-closed selfTerminate (listen_run.go).

func suicideUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent suicide

Ends your own session: kills the tmux session named by OC_SESSION on the
OC_TMUX_SOCKET socket (default "officraft"). That takes down everything running
in it, this command and your ocagent listen included, so the station sees you
go offline. It takes no flags, contacts no station and reports nothing to it:
no phase is reported on your behalf.

stdout: one line naming the session it is about to kill, or saying why it
did nothing (no OC_SESSION, or tmux could not be found). If the kill itself
fails (e.g. the session is already gone), a second line says so.

Exit codes:
  0  every run that gets past flag parsing, whether or not anything was
     killed. A successful kill ends this process before it can exit.
  2  --help itself, or any other flag parse error (an unknown flag); nothing
     is killed
`)
}

type tmuxKiller func(bin, socket, session string) error

func realTmuxKill(bin, socket, session string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, "-L", socket, "kill-session", "-t", session).Run()
}

// Mirrors makeSessionProbe's env reading — same variables, same
// defaultTmuxSocket fallback.
func tmuxSessionFromEnv(env func(string) string) (socket, session string, ok bool) {
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

// OC_BASE CLASSIFICATION: EXEMPT — contacts no station (the server learns of the
// kill only from the SSE drop), so the warnMissingBase guard would refuse a run that
// was going to work.
func cmdSuicide(cfg Config, env func(string) string, out io.Writer) int {
	return runSuicide(env, out, resolveTmuxBin, realTmuxKill)
}

func runSuicide(env func(string) string, out io.Writer, resolveBin func() string, kill tmuxKiller) int {
	socket, session, ok := tmuxSessionFromEnv(env)
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
		fmt.Fprintf(out, "[ocagent] suicide: kill-session returned %v "+
			"(session likely already gone) — the warden killpg remains the fallback.\n", err)
	}
	return 0
}
