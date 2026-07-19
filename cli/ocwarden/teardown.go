// `ocwarden teardown` — the symmetric inverse of `ocwarden install`. It removes the
// execution-plane warden from THIS machine: bootout the launchd job under the EXACT
// label com.officraft.ocwarden AND poll until launchd CONFIRMS the label is gone
// (bootoutUntilGone, shared with install — bootout is ASYNC, so the return alone
// proves nothing; the process stops because launchd stops it — NEVER pkill), then
// delete the exact files install created (the 0600 exec-warden token file and the
// launchd plist). It shares install.go's sysOps seam, so tests and CI never touch
// launchctl or the live machine, and WARDEN_INSTALL_DRYRUN=1 prints intent without
// mutating anything.
//
// IDEMPOTENT: every step tolerates "already gone" — bootout of a not-loaded label is
// ignored (the first poll probe already reports it gone), and os.Remove of a missing
// file is treated as success. Re-running never errors. But a teardown that CANNOT be
// confirmed (label lingering after the bounded poll / a stubborn file) reports
// ok=false and exits non-zero — the server's CONFIRM-THEN-REMOVE depends on it.
//
// PRECISE, NOT rm -rf: teardown removes ONLY the two artifacts install wrote
// (tokfile + plist). It deliberately does NOT wipe $HOME/.officraft/ wholesale —
// that dir also holds spawned-agent state (agents/), which is not an install artifact.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// teardownPaths is the minimal path set teardown needs: identity/OC_BASE are
// irrelevant to removal, so teardown requires only HOME (for the tokfile + plist
// paths) and the uid (for the gui domain).
type teardownPaths struct {
	tokfile   string
	plistPath string
	guiDomain string
	// label is the launchd label to boot out (namespace-derived); the zero value
	// falls back to the canonical wardenLabel (resolvedLabel).
	label string
}

// resolvedLabel is the launchd label teardown acts on: the namespace-derived
// p.label when resolved, else the canonical wardenLabel.
func (p teardownPaths) resolvedLabel() string {
	if p.label != "" {
		return p.label
	}
	return wardenLabel
}

// resolveTeardownPaths derives the removal targets from HOME + uid (+ the
// OC_NAMESPACE instance key). It intentionally does NOT read OC_TOKEN/OC_BASE —
// teardown is identity-agnostic.
func resolveTeardownPaths(env func(string) string, uid int) (teardownPaths, error) {
	home := env("HOME")
	if home == "" {
		return teardownPaths{}, errors.New("HOME must be set")
	}
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return teardownPaths{}, err
	}
	label := wardenLabelFor(ns)
	return teardownPaths{
		tokfile:   tokfileFor(home, ns), // $HOME/.officraft[-ns]/warden/exec-warden.tok
		plistPath: filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		guiDomain: fmt.Sprintf("gui/%d", uid),
		label:     label,
	}, nil
}

// removeFileTo deletes one path idempotently, emitting its outcome through the
// injected logf sink (so both the CLI streaming logger and doTeardown's capture
// buffer share ONE removal body): a missing file is success (the teardown
// idempotence contract), any other error is logged as a warning and teardown
// continues (best-effort cleanup — never abort a teardown on a single stubborn
// file). It returns false ONLY on a real (non-not-exist) removal failure, so
// doTeardown can fold per-file failures into its overall ok verdict.
func removeFileTo(logf func(string, ...any), sys sysOps, dryRun bool, path string) (removed bool) {
	if dryRun {
		logf("DRYRUN would remove: %s", path)
		return true
	}
	err := sys.remove(path)
	switch {
	case err == nil:
		logf("removed: %s", path)
		return true
	case errors.Is(err, os.ErrNotExist):
		logf("already absent: %s", path)
		return true
	default:
		logf("warning: could not remove %s: %v (continuing)", path, err)
		return false
	}
}

// removeFile is the installer-method shim kept for the existing CLI path: it
// streams through i.logf and discards the per-file verdict (the CLI teardown is
// best-effort and idempotent — it does not gate on a single stubborn file).
func (i *installer) removeFile(path string) {
	_ = removeFileTo(i.logf, i.sys, i.dryRun, path)
}

// doTeardown is the PURE teardown core, extracted so it can be driven BOTH by the
// `ocwarden teardown` CLI (teardownCmd) AND by the server-directed uninstall RPC
// (dispatchCommand). It executes the symmetric teardown — bootout (tolerated) →
// remove tokfile → remove plist — and RETURNS its result instead of exiting or
// writing to a fixed sink:
//
//	ok  → true when the label is CONFIRMED gone from launchd (bootoutUntilGone:
//	      bootout with its non-zero not-loaded exit tolerated, then poll
//	      `launchctl print` until launchd reports the label truly gone — bootout is
//	      ASYNC, so "bootout returned" is NOT "daemon stopped") and no file removal
//	      hit a real (non-not-exist) error. A fully-absent install is ok=true
//	      (idempotent: the first probe already reports the label gone). A label that
//	      still lingers after the bounded poll makes ok=false — the CONFIRM half of
//	      the server's CONFIRM-THEN-REMOVE must never report an unconfirmed bootout
//	      as done.
//	log → the human-readable transcript of every step, so the uninstall RPC can fold
//	      it into the command_result the server durably records.
//
// It NEVER calls os.Exit and NEVER calls i.errf — the caller decides what to do
// with (ok, log). The process stops via launchd bootout; NEVER pkill.
func doTeardown(sys sysOps, dryRun bool, p teardownPaths) (ok bool, log string) {
	var b strings.Builder
	logf := func(format string, a ...any) {
		fmt.Fprintf(&b, "[ocwarden teardown] "+format+"\n", a...)
	}
	label := p.resolvedLabel()
	target := p.guiDomain + "/" + label
	gone := true
	if dryRun {
		logf("DRYRUN would run: launchctl bootout %s  (tolerate not-loaded; stops the process via launchd, never pkill)", target)
		logf("DRYRUN would: poll `launchctl print %s` until the label is gone (bootout is async; bounded ~%ds)", target, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
	} else {
		// bootout + poll-until-gone (bootoutUntilGone, shared with install): tolerate a
		// non-zero exit (job not currently loaded — idempotent), then CONFIRM launchd
		// reports the label truly gone. bootout is ASYNC — without the poll, "bootout
		// returned" says nothing about the daemon actually being stopped, and the server's
		// CONFIRM-THEN-REMOVE would soft-delete on an unconfirmed teardown. This is the
		// ONLY process action; the warden stops because launchd stops it — never pkill.
		gone = bootoutUntilGone(sys, target)
		if gone {
			logf("booted out %s — CONFIRMED gone from launchd (exact label; tolerated if not loaded; never pkill)", target)
		} else {
			logf("bootout of %s NOT confirmed: label still registered after ~%ds bounded poll", target, bootoutPollAttempts*int(bootoutPollInterval/time.Millisecond)/1000)
		}
	}
	okTok := removeFileTo(logf, sys, dryRun, p.tokfile)
	okPlist := removeFileTo(logf, sys, dryRun, p.plistPath)
	ok = gone && okTok && okPlist
	if dryRun {
		logf("DRYRUN complete — no machine state changed.")
	} else if ok {
		logf("teardown complete for %s", label)
	} else {
		logf("teardown INCOMPLETE for %s — the launchd bootout was not confirmed or a required artifact could not be removed", label)
	}
	return ok, b.String()
}

// runTeardown executes the symmetric teardown for the CLI entry point by delegating
// to the pure doTeardown core and STREAMING its captured transcript through i.out
// (so the CLI's live output is unchanged). It returns doTeardown's ok verdict —
// teardown stays idempotent (a fully-absent install is ok=true), but an UNCONFIRMED
// teardown (label lingering after the bounded bootout poll, or a stubborn file)
// surfaces as ok=false so teardownCmd can exit non-zero: the server's teardown-here
// CONFIRM-THEN-REMOVE keys off that exit code, and a 0 for an unconfirmed teardown
// would soft-delete a machine whose daemon may still be running.
func (i *installer) runTeardown(p teardownPaths) (ok bool) {
	ok, log := doTeardown(i.sys, i.dryRun, p)
	// Re-emit the captured transcript line-by-line through the CLI logger so the
	// on-screen output matches the prior behaviour (minus the "install" tag → "teardown").
	for _, line := range strings.Split(strings.TrimRight(log, "\n"), "\n") {
		fmt.Fprintln(i.out, line)
	}
	return ok
}

// teardownCmd is the thin `ocwarden teardown` entry point. Returns 0 ONLY when the
// teardown is CONFIRMED (label gone from launchd + artifacts removed; idempotent —
// a fully-absent install still returns 0), 1 on a resolution failure (e.g. HOME
// unset) OR an unconfirmed/incomplete teardown. The non-zero-on-unconfirmed exit is
// LOAD-BEARING for the server's handle_teardown_here: it soft-deletes the warden
// member only on exit 0 (CONFIRM-THEN-REMOVE).
func teardownCmd(env func(string) string, out io.Writer) int {
	i := &installer{out: out, dryRun: env(dryRunEnv) == "1", sys: realSysOps()}
	p, err := resolveTeardownPaths(env, os.Getuid())
	if err != nil {
		i.errf("%v", err)
		return 1
	}
	if !i.runTeardown(p) {
		return 1
	}
	return 0
}
