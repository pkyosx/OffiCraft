package main

// Station-address gate: an unset OC_BASE halts the warden instead of falling back
// to defaultBase. The danger is not "cannot connect" — a station host or a trial
// station on the same box may be listening there — it is silently CONNECTING TO
// THE WRONG STATION.
//
// WHY IT STOPS RATHER THAN RETRIES: nobody will write OC_BASE while this process
// waits, so a retry loop could never succeed. The cost is deliberate: a halted
// warden does not heal; setting OC_BASE afterwards needs a restart.
//
// 🔴 WHY IT DOES NOT EXIT. The repo holds THREE accounts of what launchd does
// with a warden that exits, and they do not agree:
//
//  1. The installed plist sets `KeepAlive` <true/> with ThrottleInterval 10: an
//     exiting warden is relaunched every 10s forever, its message going to
//     ocwarden.err.log, which nobody reads — a SILENT CRASHLOOP, strictly worse
//     than the bug being fixed.
//  2. install.go's verify() DEPENDS on relaunch: it requires the same pid across
//     a settle window because "a bad-token / unreachable-server warden is
//     respawned by KeepAlive under a DIFFERENT pid".
//  3. selfupdate.go records the opposite as an OBSERVATION, reproduced on real
//     macOS hosts: "launchd does NOT relaunch — the gui-domain LaunchAgent job
//     sits 'not running, last exit 0' until a manual launchctl kickstart".
//
// ⚠️ The obvious reconciliation ("they describe different job shapes") is wrong,
// checked with `git log -S`: (2) and (3) were written in the same commit, both
// before the `officraft` anchor became the job leader, and contradict each other
// outright. Unsettled; KeepAlive now governs the anchor and nobody has measured
// under today's shape.
//
// Parking means the anchor's Wait() never returns, so the launchd job never
// exits and KeepAlive is never consulted — under every one of the three accounts.
// Exiting is the only choice that would have needed the question answered.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// A RECORD, never an input: nothing reads it back, so a missing or stale one can
// never change whether a warden starts.
const noBaseSentinelName = "ocwarden.no-base"

// Judges the ENV, not the value: a station host legitimately sets OC_BASE to a
// loopback address, so comparing against defaultBase would refuse it.
// ⚠️ Emptiness is judged here, not by normalizeBase: normalizeBase returns
// unparseable input unchanged, so a whitespace-only OC_BASE (a blank plist line,
// an unexpanded shell variable) would read as "set".
func baseFromEnv(env func(string) string) (base string, configured bool) {
	raw := env("OC_BASE")
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	return normalizeBase(raw), true
}

func noBaseMessage(where string) string {
	return "[ocwarden] FATAL: OC_BASE is not set — this warden was never told which station to talk to.\n" +
		"[ocwarden]   It is NOT guessing an address, and it will NOT start any members on this machine.\n" +
		"[ocwarden]   Guessing would be worse than stopping: something else may well be listening on\n" +
		"[ocwarden]   " + defaultBase + " (a station host, a trial station), and members started against it\n" +
		"[ocwarden]   would quietly join the WRONG station while every screen looked normal.\n" +
		// ⚠️ An earlier draft said the machine never appears on the roster — wrong: the
		// row is created before install, so it appears but never comes online
		// (the server's onboarding reports this as wake_undispatched).
		"[ocwarden]   This machine WILL still be listed, and will simply never come online.\n" +
		"[ocwarden]   Do not read its presence in the list as evidence that this is not the problem.\n" +
		"[ocwarden]   To fix: re-run `ocwarden install` with OC_BASE set to the station URL.\n" +
		"[ocwarden]   Setting OC_BASE alone is not enough — this process must be restarted to pick it up.\n" +
		"[ocwarden]   Halting here (staying alive, doing nothing) rather than exiting; see " + where + "\n"
}

// Deliberately NOT via resolvePaths: it needs OC_BASE, which is what is missing.
func noBaseSentinelPath(env func(string) string) (string, error) {
	home := env("HOME")
	if home == "" {
		return "", fmt.Errorf("HOME is not set")
	}
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(officraftRootFor(home, ns), "warden", noBaseSentinelName), nil
}

func writeNoBaseSentinel(env func(string) string, body string,
	mkdirAll func(string, os.FileMode) error,
	writeFile func(string, []byte, os.FileMode) error) string {

	path, err := noBaseSentinelPath(env)
	if err != nil {
		return "no sentinel written (" + err.Error() + ")"
	}
	if err := mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "no sentinel written (" + err.Error() + ")"
	}
	if err := writeFile(path, []byte(body), 0o644); err != nil {
		return "no sentinel written (" + err.Error() + ")"
	}
	return path
}

// The ONE call site of this file, on purpose: with the --once branch in realMain
// the halting branch (which hangs by design) had no test able to reach it.
func stationAddressGate(env func(string) string, out io.Writer, once bool,
	now func() time.Time, block func()) (int, bool) {

	if _, configured := baseFromEnv(env); configured {
		return 0, false
	}
	// --once is a test hook, never a launchd job: it refuses without parking.
	if once {
		fmt.Fprint(out, noBaseMessage(noBaseSentinelName))
		fmt.Fprintln(out, "[ocwarden] --once: refusing and exiting non-zero (no sentinel written; the launchd path halts instead)")
		return 1, true
	}
	return haltNoBase(env, out, now, block), true
}

func haltNoBase(env func(string) string, out io.Writer, now func() time.Time, block func()) int {
	fmt.Fprint(out, noBaseMessage(noBaseSentinelName))
	body := now().UTC().Format(time.RFC3339) + "\n" + noBaseMessage(noBaseSentinelName)
	where := writeNoBaseSentinel(env, body, os.MkdirAll, os.WriteFile)
	fmt.Fprintf(out, "[ocwarden] halted: no station address; %s\n", where)
	block()
	return 1
}

// A package variable, not a parameter, so a test can drive realMain through the
// halting branch. Evidence: changing `*once` to `true` at the call site made an
// unconfigured warden exit and the package still passed.
var gateBlock = blockUntilSignal

// Seam so the body of blockUntilSignal is testable. Evidence: `func() {}` for
// gateBlock, or `_ = ctx` for `<-ctx.Done()`, each made the warden exit instead of
// halting while a closure-counting test stayed green.
var notifyContext = signal.NotifyContext

func blockUntilSignal() {
	ctx, stop := notifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
