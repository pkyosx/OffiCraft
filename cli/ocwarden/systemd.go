// The Linux counterpart of the launchd job: a systemd --user unit named after
// the same label. Every systemctl call goes through sysOps.run, like launchctl.
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdBootedMarker = "/run/systemd/system"

func unitNameFor(label string) string {
	return label + ".service"
}

func systemdUnitDir(env func(string) string, home string) string {
	if x := env("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "systemd", "user")
	}
	return filepath.Join(home, ".config", "systemd", "user")
}

// The server starts `ocwarden install` with an env allowlist that drops
// XDG_RUNTIME_DIR, and without it `systemctl --user` cannot find the user bus.
func userRuntimeDir(env func(string) string, uid int) string {
	if x := env("XDG_RUNTIME_DIR"); filepath.IsAbs(x) {
		return x
	}
	return fmt.Sprintf("/run/user/%d", uid)
}

func systemctlUser(sys sysOps, runtimeDir string, args ...string) (string, error) {
	argv := append([]string{"XDG_RUNTIME_DIR=" + runtimeDir, "systemctl", "--user"}, args...)
	return sys.run("env", argv...)
}

func systemctlShow(sys sysOps, runtimeDir, unit, prop string) (string, error) {
	out, err := systemctlUser(sys, runtimeDir, "show", "-p", prop, "--value", unit)
	return strings.TrimSpace(out), err
}

// KillMode=process: the default control-group mode would kill every member's
// tmux server (it lives in this unit's cgroup) on each restart or stop.
const unitTemplate = `# RENDERED by ocwarden install for ROOT=%[1]s — do not edit by hand; re-run the installer.
[Unit]
Description=OffiCraft warden (%[2]s)

[Service]
Type=simple
ExecStart=%[3]s
WorkingDirectory=%[4]s
%[5]sRestart=always
RestartSec=10
KillMode=process
StandardOutput=append:%[6]s/ocwarden.out.log
StandardError=append:%[6]s/ocwarden.err.log

[Install]
WantedBy=default.target
`

func unitSpecifierSafe(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

func unitQuoted(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
}

func unitExecQuoted(s string) string {
	return unitQuoted(strings.ReplaceAll(s, "$", "$$"))
}

func renderUnit(p wardenPaths) (string, error) {
	pathVal := p.plistPATH
	if pathVal == "" {
		pathVal = p.minimalPATH
	}
	env := [][2]string{
		{"PATH", pathVal},
		{"OC_BASE", p.ocBase},
		{"HOME", p.home},
		{"OC_WARDEN_TOKFILE", p.tokfile},
	}
	if p.namespace != "" {
		env = append(env, [2]string{"OC_NAMESPACE", p.namespace})
	}
	if p.claudeBin != "" {
		env = append(env, [2]string{"OC_CLAUDE_BIN", p.claudeBin})
	}
	if p.codexBin != "" {
		env = append(env, [2]string{"OC_CODEX_BIN", p.codexBin})
	}
	if p.credCheck != "" {
		env = append(env, [2]string{"OC_CLAUDE_CRED_CHECK", p.credCheck})
	}
	var lines strings.Builder
	for _, kv := range env {
		if strings.ContainsAny(kv[1], "\n\r") {
			return "", fmt.Errorf("%s contains a line break, which a systemd unit cannot carry", kv[0])
		}
		lines.WriteString("Environment=" + unitQuoted(kv[0]+"="+kv[1]) + "\n")
	}
	for _, v := range [][2]string{{"ROOT", p.root}, {"the anchor path", p.anchorPath}, {"the log dir", p.logDir}} {
		if strings.ContainsAny(v[1], "\n\r") {
			return "", fmt.Errorf("%s contains a line break, which a systemd unit cannot carry", v[0])
		}
	}
	return fmt.Sprintf(unitTemplate, p.root, p.labelOrDefault(), unitExecQuoted(p.anchorPath),
		unitSpecifierSafe(p.root), lines.String(), unitSpecifierSafe(p.logDir)), nil
}

func notBootedWithSystemd() error {
	return errors.New("systemd is not running as PID 1 on this host (" + systemdBootedMarker + " is missing), " +
		"so there is no systemd user manager to run the warden under. " +
		"On WSL2: put `[boot]` / `systemd=true` in /etc/wsl.conf, run `wsl.exe --shutdown` from Windows, " +
		"reopen the distro and re-run the installer")
}

// Without linger the user manager, and the warden with it, stops at logout and
// does not start at boot.
func (i *installer) systemdPreflight(p wardenPaths) error {
	uid := strconv.Itoa(p.uid)
	if i.dryRun {
		i.logf("DRYRUN would: require %s (systemd as PID 1), then `loginctl enable-linger %s` unless Linger=yes", systemdBootedMarker, uid)
		return nil
	}
	if _, err := i.sys.statMode(systemdBootedMarker); err != nil {
		return notBootedWithSystemd()
	}
	if linger, _ := i.sys.run("loginctl", "show-user", uid, "-p", "Linger", "--value"); strings.TrimSpace(linger) == "yes" {
		i.logf("linger already on for uid %s", uid)
		return nil
	}
	if _, err := i.sys.run("loginctl", "enable-linger", uid); err != nil {
		return fmt.Errorf("loginctl enable-linger %s failed: %w — without linger the warden stops at logout and does not start at boot; "+
			"ask an administrator to run `sudo loginctl enable-linger %s`, then re-run the installer", uid, err, uid)
	}
	linger, _ := i.sys.run("loginctl", "show-user", uid, "-p", "Linger", "--value")
	if got := strings.TrimSpace(linger); got != "yes" {
		return fmt.Errorf("loginctl enable-linger %s exited 0 but Linger=%s — without linger the warden stops at logout and does not start at boot; "+
			"ask an administrator to run `sudo loginctl enable-linger %s`, then re-run the installer", uid, orNone(got), uid)
	}
	i.logf("enabled linger for uid %s", uid)
	return nil
}

func (i *installer) writeUnit(p wardenPaths) error {
	rendered, err := renderUnit(p)
	if err != nil {
		return fmt.Errorf("cannot render the systemd unit: %w", err)
	}
	if i.dryRun {
		i.logf("DRYRUN would: mkdir -p %s; render unit -> %s", p.unitDir, p.unitPath)
		return nil
	}
	if err := i.sys.mkdirAll(p.unitDir, 0o755); err != nil {
		return fmt.Errorf("mkdir systemd user unit dir %s: %w", p.unitDir, err)
	}
	if err := i.sys.writeFile(p.unitPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write unit %s: %w", p.unitPath, err)
	}
	i.logf("unit rendered: %s", p.unitPath)
	return nil
}

// restart also starts a unit that was not running; systemctl jobs are
// synchronous, so no poll is needed the way launchd's bootout needs one.
func (i *installer) systemdReinstall(p wardenPaths) error {
	unit := unitNameFor(p.labelOrDefault())
	if i.dryRun {
		i.logf("DRYRUN would run: systemctl --user daemon-reload, then require LoadState=loaded for %s", unit)
		i.logf("DRYRUN would run: systemctl --user enable %s", unit)
		i.logf("DRYRUN would run: systemctl --user restart %s", unit)
		return nil
	}
	if _, err := systemctlUser(i.sys, p.runtimeDir, "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload failed: %w — the systemd user manager for uid %d is not reachable", err, p.uid)
	}
	if state, err := systemctlShow(i.sys, p.runtimeDir, unit, "LoadState"); err != nil || state != "loaded" {
		return fmt.Errorf("the systemd user manager did not load %s after daemon-reload (LoadState=%s); the unit file to look at is %s", unit, orNone(state), p.unitPath)
	}
	if _, err := systemctlUser(i.sys, p.runtimeDir, "enable", unit); err != nil {
		return fmt.Errorf("systemctl --user enable %s failed: %w", unit, err)
	}
	if _, err := systemctlUser(i.sys, p.runtimeDir, "restart", unit); err != nil {
		return fmt.Errorf("systemctl --user restart %s failed: %w", unit, err)
	}
	i.logf("enabled + restarted %s (exact unit)", unit)
	return nil
}

func (i *installer) unitMainPID(p wardenPaths) string {
	pid, err := systemctlShow(i.sys, p.runtimeDir, unitNameFor(p.labelOrDefault()), "MainPID")
	if err != nil || pid == "0" {
		return ""
	}
	if _, convErr := strconv.Atoi(pid); convErr != nil {
		return ""
	}
	return pid
}

// From inside the unit (the uninstall RPC) the process must outlive the
// teardown to send its receipt, so the unit is only disabled here and the stop
// is queued afterwards by stopUnitNoBlock. A synchronous stop from inside
// would SIGTERM this process before the receipt goes out.
func systemdTeardown(sys sysOps, dryRun bool, p teardownPaths, insideUnit bool, logf func(string, ...any)) (gone, okTok, okUnit bool) {
	unit := unitNameFor(p.resolvedLabel())
	disable := []string{"disable", "--now", unit}
	if insideUnit {
		disable = []string{"disable", unit}
	}
	if dryRun {
		logf("DRYRUN would run: systemctl --user %s  (tolerate not-loaded; stops the process via systemd, never pkill)", strings.Join(disable, " "))
		okTok = removeFileTo(logf, sys, dryRun, p.tokfile)
		okUnit = removeFileTo(logf, sys, dryRun, p.unitPath)
		logf("DRYRUN would run: systemctl --user daemon-reload, then require LoadState=not-found for %s", unit)
		return true, okTok, okUnit
	}
	if _, err := systemctlUser(sys, p.runtimeDir, disable...); err != nil {
		logf("systemctl --user %s reported: %v (continuing)", strings.Join(disable, " "), err)
	}
	okTok = removeFileTo(logf, sys, dryRun, p.tokfile)
	okUnit = removeFileTo(logf, sys, dryRun, p.unitPath)
	if _, err := systemctlUser(sys, p.runtimeDir, "daemon-reload"); err != nil {
		logf("systemctl --user daemon-reload failed: %v", err)
	}
	load, loadErr := systemctlShow(sys, p.runtimeDir, unit, "LoadState")
	active, activeErr := systemctlShow(sys, p.runtimeDir, unit, "ActiveState")
	switch {
	case loadErr != nil || activeErr != nil:
		logf("removal of %s NOT confirmed: the systemd user manager could not be asked", unit)
	case load != "not-found":
		logf("removal of %s NOT confirmed: LoadState=%s", unit, orNone(load))
	case insideUnit:
		gone = true
		logf("disabled %s — CONFIRMED unloaded from the systemd user manager; this warden stops itself after the receipt", unit)
	case active != "inactive" && active != "failed":
		logf("removal of %s NOT confirmed: the unit is unloaded but still %s", unit, orNone(active))
	default:
		gone = true
		logf("disabled + stopped %s — CONFIRMED gone from the systemd user manager (exact unit; never pkill)", unit)
	}
	return gone, okTok, okUnit
}

// --no-block queues the stop job and returns, so systemd (not Restart=always)
// decides what happens when this process exits.
func stopUnitNoBlock(sys sysOps, p teardownPaths) error {
	_, err := systemctlUser(sys, p.runtimeDir, "stop", "--no-block", unitNameFor(p.resolvedLabel()))
	return err
}
