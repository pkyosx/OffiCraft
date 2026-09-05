package main

// The station-address gate (T-88).
//
// WHAT THIS DEFENDS. `loadConfig` used to answer an unset OC_BASE with the
// hard-coded defaultBase and hand the result on as if somebody had configured
// it. Nothing downstream could tell the two apart: warden's Config carried
// Base/Token/ID and no bit saying where Base came from, so a machine whose
// install never received a station address would come up, talk to whatever is
// listening on 127.0.0.1:7755, and spawn members pointed at it. Everything looks
// normal on screen. The damage is NOT "cannot connect" — a station host, or a
// trial station on the same box, IS listening there — the damage is CONNECTING
// TO THE WRONG STATION, and no layer ever says a word.
//
// WHY IT STOPS RATHER THAN RETRIES. The failure this guards is a missing piece
// of configuration. Configuration does not arrive on its own: unlike the ocagent
// path in T-81 — which a download genuinely does fill in a moment later, so
// "decide when you use it" was the right shape there — nobody is coming to write
// OC_BASE while this process waits. Retrying would be a loop that can never
// succeed, and its only effect would be noise.
//
// 🔴 WHY IT DOES NOT EXIT, AND WHY THAT REASON IS NOT THE ONE YOU WILL EXPECT.
// There are two accounts in this repo of what launchd does with a warden that
// exits, and they contradict each other:
//
//   - The plist this installer writes (install.go) sets `KeepAlive` to an
//     unconditional <true/> with ThrottleInterval 10. Read straight, an exiting
//     warden is relaunched every 10 seconds forever, with its message going to
//     ocwarden.err.log, which nobody reads — a SILENT CRASHLOOP, strictly worse
//     than the bug being fixed.
//   - selfupdate.go records the opposite as an OBSERVATION, twice and
//     reproducibly, on real macOS hosts: "launchd does NOT relaunch — the
//     gui-domain LaunchAgent job sits 'not running, last exit 0' until a manual
//     launchctl kickstart". Its whole exec-in-place design rests on that.
//
// Both cannot be right, and this ticket did not settle which is — settling it
// means killing a warden on a machine that is currently carrying live agents,
// which is not a price this change is worth. So the halt below is chosen
// BECAUSE IT IS CORRECT UNDER EITHER ONE:
//
//   - if launchd relaunches, staying alive means there is no crashloop to hide
//     the signal in;
//   - if launchd does not relaunch, staying alive costs nothing that exiting
//     would have saved — the machine is out of service either way.
//
// And under both, the signal that actually reaches a human is the same one: this
// machine never appears on the roster, because it has nowhere to report to.
// A halted warden and an exited one are indistinguishable from the server's
// side; the halted one additionally leaves a live process and a sentinel file
// on the box saying why.
//
// ⚠️ THE COST, NAMED. A halted warden does not heal. Setting OC_BASE afterwards
// does nothing until somebody restarts it. That is deliberate: this ticket asks
// for a loud stop, not for a retry that eventually works.

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

// noBaseSentinelName is the file a halted warden leaves behind. It is a RECORD,
// never an input: nothing reads it back to decide anything, so a missing or
// unwritable one can never keep a healthy warden from starting, and a stale one
// left by an earlier halt can never stop a correctly-configured warden from
// running. (The same fail-open direction contextreport.go states for its backoff
// record: an unreadable scratch file must not be able to silence a working
// process. Here the exposure is removed rather than mitigated — there is no read
// path at all.)
const noBaseSentinelName = "ocwarden.no-base"

// baseFromEnv resolves OC_BASE and says WHETHER IT WAS THERE — the distinction
// the old loadConfig threw away. `configured` is false exactly when the env gave
// nothing usable, which is the case this whole file exists for.
//
// It deliberately reports on the ENV, not on the resulting value: a station host
// legitimately sets OC_BASE to a loopback address, and judging by comparing the
// final string against defaultBase would refuse that machine. What is wrong is
// never the address, it is nobody having chosen one.
// ⚠️ EMPTINESS IS JUDGED HERE, NOT BY normalizeBase, and that is not a detail.
// normalizeBase hands back anything it cannot parse UNCHANGED — deliberately, so
// the caller's own validation sees what the operator actually typed — so a
// whitespace-only OC_BASE comes back as whitespace, not as "". Asking
// normalizeBase whether the variable was set therefore answers "yes" for
// `OC_BASE="   "`, which is how a blank line in a launchd plist or an unexpanded
// shell variable would arrive. This function's own test caught that.
func baseFromEnv(env func(string) string) (base string, configured bool) {
	raw := env("OC_BASE")
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	return normalizeBase(raw), true // T-78: keep the host, re-decide the scheme
}

// noBaseMessage is what a person finds when they go looking. It names the
// variable, says what the warden did about it, and says what will and will not
// happen next — including that setting the variable now is not enough on its
// own, because the most expensive wrong guess here is "I fixed it, it should
// come back".
func noBaseMessage(where string) string {
	return "[ocwarden] FATAL: OC_BASE is not set — this warden was never told which station to talk to.\n" +
		"[ocwarden]   It is NOT guessing an address, and it will NOT start any members on this machine.\n" +
		"[ocwarden]   Guessing would be worse than stopping: something else may well be listening on\n" +
		"[ocwarden]   " + defaultBase + " (a station host, a trial station), and members started against it\n" +
		"[ocwarden]   would quietly join the WRONG station while every screen looked normal.\n" +
		"[ocwarden]   This machine will therefore not appear on the roster at all — that absence is the signal.\n" +
		"[ocwarden]   To fix: re-run `ocwarden install` with OC_BASE set to the station URL.\n" +
		"[ocwarden]   Setting OC_BASE alone is not enough — this process must be restarted to pick it up.\n" +
		"[ocwarden]   Halting here (staying alive, doing nothing) rather than exiting; see " + where + "\n"
}

// noBaseSentinelPath is where the record goes: the per-instance warden dir,
// derived from HOME and OC_NAMESPACE ONLY.
//
// It deliberately does NOT go through resolvePaths, which is the obvious place
// to reach for and the wrong one: resolvePaths needs OC_BASE, which is precisely
// what is missing here, so routing through it would make the explanation of the
// failure depend on the thing that failed.
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

// writeNoBaseSentinel drops the record best-effort and returns where it went (or
// why it did not). Failing to write it is NOT a reason to change course: the
// halt has already been decided by the env, and the log line has already been
// printed. The returned string is for the log, so that a reader who cannot find
// the file learns that from the same place they learned everything else.
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

// stationAddressGate is the ONE call site of this whole file, and it is one on
// purpose. An earlier shape had realMain branch on `--once` itself and call two
// different functions; that gave the gate two call sites, and a test can only
// ever reach the branch it can run — the halting one hangs by design, so nothing
// would have watched it. Folding the branch in here means the single line in
// realMain is the single thing that can be deleted, and deleting it is what the
// reached-test notices.
//
// Returns (exit code, stop). stop=false means OC_BASE was configured and the
// caller should carry on; the exit code is meaningless then.
func stationAddressGate(env func(string) string, out io.Writer, once bool,
	now func() time.Time, block func()) (int, bool) {

	if _, configured := baseFromEnv(env); configured {
		return 0, false
	}
	// --once is a TEST HOOK, never a launchd job, and a hook that parks forever is
	// a hook nobody can use. It still refuses — loudly, non-zero — it just returns
	// instead of halting.
	if once {
		fmt.Fprint(out, noBaseMessage(noBaseSentinelName))
		return 1, true
	}
	return haltNoBase(env, out, now, block), true
}

// haltNoBase is the whole refusal: say it, record it, then stop doing anything
// at all until the process is signalled.
//
// `block` is the seam that makes this testable — production passes
// blockUntilSignal, tests pass a function that returns at once. There is no
// timeout and no wake-up condition on purpose: see the file header for why this
// does not retry.
func haltNoBase(env func(string) string, out io.Writer, now func() time.Time, block func()) int {
	fmt.Fprint(out, noBaseMessage(noBaseSentinelName))
	body := now().UTC().Format(time.RFC3339) + "\n" + noBaseMessage(noBaseSentinelName)
	where := writeNoBaseSentinel(env, body, os.MkdirAll, os.WriteFile)
	fmt.Fprintf(out, "[ocwarden] halted: no station address; %s\n", where)
	block()
	// Reached only when the process was signalled, i.e. somebody or something
	// asked this warden to go away. Non-zero because nothing this warden was
	// installed to do ever happened.
	return 1
}

// blockUntilSignal parks until SIGINT/SIGTERM. This is what "stays alive, does
// nothing" means concretely: launchd sees a healthy long-lived job, no member is
// spawned, no HTTP request is made, and `ocwarden teardown` / a reboot still ends
// it the ordinary way.
func blockUntilSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
