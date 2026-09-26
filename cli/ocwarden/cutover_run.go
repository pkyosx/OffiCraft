// `ocwarden cutover-anchor`: the grandchild spawned by maybeStartAnchorCutover. It
// runs in its own session so `launchctl bootout` of the warden job it came from
// does not take it along.
//
// 🔴 The backup is taken HERE, before install, and ANY non-zero install exit
// triggers rollback: install's writePlist overwrites unconditionally and lints
// AFTER writing, so a backup inside the installer is too late, and rolling back
// only on verify failure leaves a machine whose next reboot adopts an unverified
// plist.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// A SUCCESSFUL rollback returns 0 (the contracted outcome); non-zero means the
// rollback itself did not restore a live warden.
func cutoverCmd(env func(string) string, out io.Writer) int {
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, "%s [cutover] "+format+"\n",
			append([]any{time.Now().UTC().Format(time.RFC3339)}, a...)...)
	}
	exe, err := os.Executable()
	if err != nil {
		logf("FATAL: cannot resolve own binary: %v", err)
		return 1
	}
	p, err := resolvePaths(env, exe, os.Getuid())
	if err != nil {
		logf("FATAL: %v", err)
		return 1
	}
	ops := newCutoverOps()
	defer releaseCutoverLock(ops, env)

	return runCutover(ops, p, exe, logf)
}

// Best-effort: a leaked lock ages out via staleLockAge.
func releaseCutoverLock(ops cutoverOps, env func(string) string) {
	if lock := env("OC_CUTOVER_LOCK"); lock != "" {
		_ = ops.remove(lock)
	}
}

func runCutover(ops cutoverOps, p wardenPaths, exe string, logf func(string, ...any)) int {
	prevPath := p.plistPath + plistPrevSuffix
	target := p.guiDomain + "/" + p.labelOrDefault()

	old, err := ops.readFile(p.plistPath)
	if err != nil {
		logf("ABORT: cannot read current plist %s: %v (nothing to roll back to)", p.plistPath, err)
		return 1
	}
	if err := ops.writeFile(prevPath, old, 0o644); err != nil {
		logf("ABORT: cannot write plist backup %s: %v", prevPath, err)
		return 1
	}
	logf("backed up current plist -> %s (%d bytes)", prevPath, len(old))

	logf("running: %s install --force", exe)
	installOut, installErr := ops.runInstaller(exe, "install", "--force")
	if installOut != "" {
		logf("install output:\n%s", installOut)
	}
	if installErr == nil {
		logf("SUCCESS: converted to anchor shape; plist backup kept at %s", prevPath)
		return 0
	}

	// No sentinel when the installer changed nothing: that failure may be transient
	// (offline, so the ocagent download failed), and a sentinel would exclude the
	// machine from the migration for good. 🔴 Judge by the FACT (on-disk plist vs
	// backup), not by which step failed — a step index silently picks the wrong
	// side once someone inserts a step.
	if current, readErr := ops.readFile(p.plistPath); readErr == nil && string(current) == string(old) {
		logf("install FAILED (%v) but nothing was modified — machine is untouched, leaving no sentinel so the next start retries", installErr)
		return 0
	}

	logf("install FAILED (%v) — rolling back to the pre-conversion shape", installErr)
	if rbErr := rollback(ops, p, old, target, logf); rbErr != nil {
		logf("🔴 ROLLBACK FAILED: %v", rbErr)
		writeCutoverSentinel(ops, p, fmt.Sprintf("install failed: %v\nrollback FAILED: %v", installErr, rbErr), logf)
		return 1
	}
	logf("rollback complete: launchd is running the pre-conversion shape again")
	writeCutoverSentinel(ops, p, fmt.Sprintf("install failed: %v\nrolled back successfully", installErr), logf)
	return 0
}

// Writing the file is not enough: launchd caches a job's configuration at
// bootstrap, and both KeepAlive respawn and `launchctl kickstart -k` were measured
// (macOS 26.5, 15.7.7) serving the stale one. Only bootout→bootstrap re-reads it.
func rollback(ops cutoverOps, p wardenPaths, old []byte, target string, logf func(string, ...any)) error {
	if err := ops.writeFile(p.plistPath, old, 0o644); err != nil {
		return fmt.Errorf("restore plist %s: %w", p.plistPath, err)
	}
	logf("restored %s from backup", p.plistPath)

	// A bootout error is tolerated: "not currently loaded" is expected when the
	// failure happened before or during bootstrap.
	_, _ = ops.run("launchctl", "bootout", target)
	if !cutoverWaitGone(ops, target) {
		logf("WARN: %s still registered after bootout; bootstrapping anyway", target)
	}
	if _, err := ops.run("launchctl", "bootstrap", p.guiDomain, p.plistPath); err != nil {
		return fmt.Errorf("re-bootstrap old shape: %w", err)
	}
	// `launchctl bootstrap` can exit 0 and register NOTHING; kickstart then fails
	// with 113 "Could not find service", misnaming the broken step in the sentinel
	// — the only diagnosis anybody reads. Registration can also merely lag, so a
	// timeout alone must NOT fail the rollback; only kickstart also failing does.
	unregistered := cutoverWaitRegistered(ops, target)
	if unregistered != nil {
		logf("WARN: launchd still does not know %s after re-bootstrap exited 0; kickstarting anyway", target)
	}
	if _, err := ops.run("launchctl", "kickstart", "-k", target); err != nil {
		if unregistered != nil {
			return fmt.Errorf("re-bootstrap of the old shape exited 0 but registered nothing — launchd does not know %s, so the old job was never loaded; the plist to look at is %s: %w (the next verb then reported: %v)", target, p.plistPath, unregistered, err)
		}
		return fmt.Errorf("kickstart old shape: %w", err)
	}
	if err := cutoverVerifyAlive(ops, target); err != nil {
		return fmt.Errorf("old shape did not come back: %w", err)
	}
	return nil
}

// Bootout is ASYNC: bootstrapping while the dying registration lingers fails with
// "Bootstrap failed: 5". This and cutoverWaitRegistered are hand copies of
// install's bootoutUntilGone / registeredUntilFound (different ops types), kept
// local so a change to install's bounds cannot break the rollback path.
func cutoverWaitGone(ops cutoverOps, target string) bool {
	for k := 0; k < bootoutPollAttempts; k++ {
		if _, err := ops.run("launchctl", "print", target); err != nil {
			return true
		}
		ops.sleep(bootoutPollInterval)
	}
	return false
}

func cutoverWaitRegistered(ops cutoverOps, target string) error {
	var err error
	for k := 0; k < registerPollAttempts; k++ {
		if _, err = ops.run("launchctl", "print", target); err == nil {
			return nil
		}
		ops.sleep(registerPollInterval)
	}
	return err
}

// One pid must hold across a settle window: a warden that cannot start is
// respawned by KeepAlive under a different pid and looks alive on one sample.
func cutoverVerifyAlive(ops cutoverOps, target string) error {
	var pid string
	for k := 0; k < 30; k++ {
		if pid = cutoverPID(ops, target); pid != "" {
			break
		}
		ops.sleep(time.Second)
	}
	if pid == "" {
		return fmt.Errorf("%s reported no live pid within 30s", target)
	}
	for k := 0; k < 6; k++ {
		ops.sleep(time.Second)
		if curPID := cutoverPID(ops, target); curPID != pid {
			return fmt.Errorf("%s is crash-looping (pid %s -> %s)", target, pid, orNone(curPID))
		}
	}
	return nil
}

func cutoverPID(ops cutoverOps, target string) string {
	out, err := ops.run("launchctl", "print", target)
	if err != nil {
		return ""
	}
	if m := launchctlPIDRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// maybeStartAnchorCutover refuses to start while this sentinel exists; without it
// a rolled-back machine detects "legacy" on its next start and converts again,
// forever.
func writeCutoverSentinel(ops cutoverOps, p wardenPaths, reason string, logf func(string, ...any)) {
	path := filepath.Join(p.root, "warden", cutoverFailedName)
	body := fmt.Sprintf("%s\n%s\n\nRemove this file to allow another attempt.\n",
		time.Now().UTC().Format(time.RFC3339), reason)
	if err := ops.writeFile(path, []byte(body), 0o644); err != nil {
		logf("WARN: could not write %s: %v (the next start will retry the conversion)", path, err)
		return
	}
	logf("wrote %s — further conversion attempts are blocked until it is removed", path)
}
