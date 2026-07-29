// `ocwarden anchor` — the TCC identity anchor. It runs ONE child (the live
// ocwarden), forwards the launchd stop signals to it, and exits with the child's
// status. That is the whole verb; it deliberately does nothing else, ever.
//
// WHY THIS EXISTS
// ---------------
// macOS attributes every privacy (TCC) decision not to the process making the
// syscall but to its RESPONSIBLE PROCESS, and launchd makes a job's main process
// responsible for itself. Every descendant inherits that attribution no matter
// which binary it later execs — measured on a live install, where the tmux server
// and every agent pane under it all reported the LaunchAgent's own executable as
// their responsible process.
//
// So before this verb, the TCC identity of the entire execution plane WAS the
// ocwarden binary named in ProgramArguments. ocwarden is adhoc-signed (no stable
// signing identity — see bin/codesign-artifact), which means TCC identifies it by
// cdhash: the hash of those exact bytes. Self-update swaps that binary whenever the
// server ships a new one, so the cdhash changed under TCC's feet on every release,
// and every grant the operator had given the fleet silently died with it.
//
// The failure mode this produced is far worse than "permission denied". A TCC
// request from an unrecognised client is answered by PROMPTING the user — and a
// LaunchAgent has no GUI to show a prompt in, so the request never returns. The
// open(2) blocks in the kernel forever. Field symptom: `claude` started in a fresh
// pane rendered not one byte and hung; a bare `ls ~/Documents` in the same pane hung
// identically, while the same command on a tmux server started from a normal login
// shell returned instantly. Nothing in any log; the operator sees a wedged agent.
//
// The fix is to stop pointing the launchd job at a binary that changes. This verb
// runs from a FROZEN COPY of ocwarden (installed once at $ROOT/warden/ocanchor and
// never overwritten — see installAnchor), so the responsible process the whole tree
// inherits is a file whose bytes, and therefore whose cdhash, never move. The live
// ocwarden becomes its child and may be swapped as often as the fleet likes.
//
// FORK, NOT EXEC. The child must be a separate process. Exec-ing in place would
// keep the pid but replace the code identity with the very binary we are trying not
// to be identified by, which is the entire bug. Anything here that turns the fork
// into an exec silently reverts the fix while leaving every test green — the damage
// is invisible until an operator's grants expire months later.
//
// THIS FILE IS A FROZEN CONTRACT. The copy at $ROOT/warden/ocanchor is whatever
// ocwarden version first installed it and is never updated, so a machine in the
// field is running THIS code as it was on its install day. Changing the semantics
// here does not change those machines; it only means new installs behave
// differently from old ones. Treat a behaviour change as needing a new anchor
// filename plus a documented re-authorization, not an edit in place.
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// anchorForwardedSignals are the stop signals launchd (and an operator) send to the
// job. The anchor is not the thing being asked to stop — the warden underneath it
// is — so each one is relayed to the child and the child's own exit decides ours.
// Without the relay, `launchctl bootout` would reap the anchor and leave a live
// ocwarden orphaned onto pid 1.
var anchorForwardedSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}

// anchorChild is the slice of *os.Process the verb needs, named as an interface so
// tests can drive the wait/signal logic without spawning anything.
type anchorChild interface {
	Signal(os.Signal) error
	Wait() (*os.ProcessState, error)
}

// startAnchorChild is the process-spawning seam, rebindable by tests in the same
// spirit as newHostSeam/newCmdRunner. The production implementation refuses to run
// under `go test`: this verb's whole job is to spawn a long-lived real process and
// hand it the terminal, which a test binary must never do.
//
// It uses os.StartProcess rather than os/exec on purpose. exec.Command would put a
// second `execRunner{`-shaped construction of real process wiring into the package
// and blunt hostseam_test's structural guard, for no benefit — this verb needs
// exactly one spawn with inherited stdio and nothing exec.Cmd adds.
var startAnchorChild = func(argv []string) (anchorChild, error) {
	refuseInTestBinary("startAnchorChild")
	return os.StartProcess(argv[0], argv, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
	})
}

// anchorNotify / anchorStop wrap signal registration so tests can inject signals
// without racing the real process-wide handler table.
var anchorNotify = signal.Notify
var anchorStop = signal.Stop

// anchorCmd implements `ocwarden anchor <program> [args...]`.
//
// The exit status is the child's, so launchd sees exactly what it would have seen
// had it started the warden directly — including the KeepAlive consequences of a
// clean exit 0, which selfupdate.go's exec-in-place path depends on staying
// unchanged. A child killed by a signal is reported as 128+signum, the shell
// convention, so an operator reading `launchctl print` sees a cause and not a bare
// failure.
func anchorCmd(args []string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: ocwarden anchor <program> [args...]")
		return 2
	}
	child, err := startAnchorChild(args)
	if err != nil {
		fmt.Fprintf(out, "[ocwarden anchor] FATAL: cannot start %s: %v\n", args[0], err)
		return 1
	}

	sigs := make(chan os.Signal, len(anchorForwardedSignals))
	anchorNotify(sigs, anchorForwardedSignals...)
	defer anchorStop(sigs)

	// The relay outlives the wait below only until the child is reaped; a send on a
	// dead pid is a harmless ESRCH, so no coordination is needed beyond the buffer.
	go func() {
		for s := range sigs {
			_ = child.Signal(s)
		}
	}()

	state, err := child.Wait()
	if err != nil {
		fmt.Fprintf(out, "[ocwarden anchor] FATAL: wait failed: %v\n", err)
		return 1
	}
	return anchorExitStatus(state)
}

// anchorExitStatus maps a finished child to the status the anchor exits with.
func anchorExitStatus(state *os.ProcessState) int {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok {
		return anchorExitStatusFromWait(ws)
	}
	return state.ExitCode()
}

// anchorExitStatusFromWait holds the actual convention, split out because
// os.ProcessState cannot be constructed by hand while the WaitStatus it carries
// can — so the mapping is testable without spawning anything.
func anchorExitStatusFromWait(ws syscall.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ws.ExitStatus()
}
