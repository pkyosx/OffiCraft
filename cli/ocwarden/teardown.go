// `ocwarden teardown` — inverse of `ocwarden install`. The process is stopped only
// by launchd bootout, never pkill. It removes ONLY the two files install wrote
// (tokfile + plist), never ~/.officraft wholesale: that dir also holds agents/ state.
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

type teardownPaths struct {
	tokfile   string
	plistPath string
	guiDomain string
	label     string
}

func (p teardownPaths) resolvedLabel() string {
	if p.label != "" {
		return p.label
	}
	return wardenLabel
}

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
		tokfile:   tokfileFor(home, ns),
		plistPath: filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		guiDomain: fmt.Sprintf("gui/%d", uid),
		label:     label,
	}, nil
}

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

func (i *installer) removeFile(path string) {
	_ = removeFileTo(i.logf, i.sys, i.dryRun, path)
}

// doTeardown is shared by the `ocwarden teardown` CLI and the server-directed
// uninstall RPC, which folds log into the command_result the server records.
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
		// bootout is ASYNC: without the poll, the server's CONFIRM-THEN-REMOVE would
		// soft-delete on an unconfirmed teardown.
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

func (i *installer) runTeardown(p teardownPaths) (ok bool) {
	ok, log := doTeardown(i.sys, i.dryRun, p)
	for _, line := range strings.Split(strings.TrimRight(log, "\n"), "\n") {
		fmt.Fprintln(i.out, line)
	}
	return ok
}

// validateTeardownTarget fails closed: in automation a lost OC_NAMESPACE would
// otherwise silently tear down the live canonical warden.
func validateTeardownTarget(env func(string) string, canonicalExplicit bool) error {
	ns, err := namespaceFromEnv(env)
	if err != nil {
		return err
	}
	if ns != "" && canonicalExplicit {
		return fmt.Errorf("refusing: --canonical conflicts with OC_NAMESPACE=%q", ns)
	}
	if ns == "" && !canonicalExplicit {
		// Message order is deliberate: a wrapper that scrapes stderr for a fix-up must
		// read what this destroys and the namespaced escape before it finds --canonical.
		return fmt.Errorf("refusing: this would tear down the CANONICAL warden on this host " +
			"(launchd com.officraft.ocwarden, its exec token and plist) and stop every agent it " +
			"supervises. For an isolated instance set OC_NAMESPACE=<ns>; if destroying the " +
			"canonical warden is genuinely intended, authorize it explicitly with --canonical")
	}
	return nil
}

// teardownCmd returns 0 ONLY when the teardown is CONFIRMED: the server's
// handle_teardown_here soft-deletes the warden member only on exit 0.
func teardownCmd(env func(string) string, out io.Writer, canonicalExplicit bool) int {
	i := &installer{out: out, tag: "teardown", dryRun: env(dryRunEnv) == "1", sys: newHostSeam().sys}
	if err := validateTeardownTarget(env, canonicalExplicit); err != nil {
		i.errf("%v", err)
		return 1
	}
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
