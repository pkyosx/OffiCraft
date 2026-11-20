// The Linux counterpart of the launchd job: a systemd --user unit named after
// the same label. Every systemctl call goes through sysOps.run, like launchctl.
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
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

// Under WSL2 a systemd user service does not keep the distro running: once the
// last Windows-side client (an Ubuntu window) exits, Windows stops the whole
// distro and the warden with it. A Linux process started from Windows through
// wsl.exe counts as a client, so a hidden `sleep infinity` launched by a script
// in the Windows Startup folder keeps the distro, and every member, online.
const (
	wslOSReleasePath = "/proc/sys/kernel/osrelease"
	wslCmdExe        = "/mnt/c/Windows/System32/cmd.exe"
	wslWscriptExe    = "/mnt/c/Windows/System32/wscript.exe"
	wslPathBin       = "/usr/bin/wslpath"
	// Windows programs started from a Linux cwd warn that UNC paths are unsupported.
	wslWindowsCwd = "/mnt/c"
)

var wslDistroShape = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func onWSL(sys sysOps) bool {
	release, err := sys.readFile(wslOSReleasePath)
	return err == nil && strings.Contains(strings.ToLower(string(release)), "microsoft")
}

func runWindows(sys sysOps, exe string, args ...string) (string, error) {
	return sys.run("env", append([]string{"-C", wslWindowsCwd, exe}, args...)...)
}

// `wslpath -w /` names the distro this process runs in without relying on
// WSL_DISTRO_NAME, which the server's child env does not carry.
func wslDistroName(sys sysOps, envDistro string) (string, error) {
	name := ""
	if out, err := sys.run(wslPathBin, "-w", "/"); err == nil {
		root := strings.TrimRight(strings.TrimSpace(out), `\`)
		for _, prefix := range []string{`\\wsl.localhost\`, `\\wsl$\`} {
			if strings.HasPrefix(root, prefix) {
				name = strings.TrimPrefix(root, prefix)
			}
		}
	}
	if name == "" {
		name = envDistro
	}
	if !wslDistroShape.MatchString(name) {
		return "", errors.New("the WSL distro name could not be determined")
	}
	return name, nil
}

type wslKeepAlive struct {
	distro  string
	path    string
	winPath string
}

func (k wslKeepAlive) script() string {
	return `CreateObject("WScript.Shell").Run "wsl.exe -d ` + k.distro + ` --exec sleep infinity", 0, False` + "\r\n"
}

func wslKeepAliveName(distro, namespace string) string {
	if namespace == "" {
		return "OffiCraft keep WSL running (" + distro + ").vbs"
	}
	return "OffiCraft keep WSL running (" + distro + ", " + namespace + ").vbs"
}

// The returned distro is filled in whenever it is known, even with an error,
// so the manual fallback can name it.
func locateWSLKeepAlive(sys sysOps, envDistro, namespace string) (wslKeepAlive, error) {
	distro, err := wslDistroName(sys, envDistro)
	if err != nil {
		return wslKeepAlive{}, err
	}
	k := wslKeepAlive{distro: distro}
	out, err := runWindows(sys, wslCmdExe, "/c", "echo %APPDATA%")
	if err != nil {
		return k, fmt.Errorf("cmd.exe could not be run: %w", err)
	}
	appData := strings.TrimSpace(out)
	if appData == "" || strings.Contains(appData, "%") {
		return k, errors.New("cmd.exe did not report the Windows APPDATA folder")
	}
	linuxAppData, err := sys.run(wslPathBin, "-u", appData)
	if err != nil {
		return k, fmt.Errorf("wslpath could not convert the APPDATA folder: %w", err)
	}
	name := wslKeepAliveName(distro, namespace)
	k.path = filepath.Join(strings.TrimSpace(linuxAppData), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", name)
	k.winPath = appData + `\Microsoft\Windows\Start Menu\Programs\Startup\` + name
	return k, nil
}

// Never fails the install: without the keep-alive the warden still works while
// an Ubuntu window is open.
func (i *installer) ensureWSLKeepAlive(p wardenPaths) {
	if !onWSL(i.sys) {
		return
	}
	if i.dryRun {
		i.logf("DRYRUN would: write the WSL keep-alive script into the Windows Startup folder and start it once")
		return
	}
	k, err := locateWSLKeepAlive(i.sys, p.wslDistro, p.namespace)
	if err == nil {
		if current, rerr := i.sys.readFile(k.path); rerr != nil || string(current) != k.script() {
			if werr := i.sys.writeFile(k.path, []byte(k.script()), 0o644); werr != nil {
				err = fmt.Errorf("could not write %s: %w", k.winPath, werr)
			}
		}
	}
	if err == nil {
		if _, rerr := runWindows(i.sys, wslWscriptExe, k.winPath); rerr != nil {
			err = fmt.Errorf("wscript.exe could not start %s: %w", k.winPath, rerr)
		}
	}
	if err != nil {
		distro := k.distro
		if distro == "" {
			distro = "<name from wsl -l -v>"
		}
		i.logf("WARN: WSL keep-alive not set up (%v) — members on this machine go offline when every Ubuntu window is closed. "+
			"Manual fallback, in Windows PowerShell: Start-Process wsl.exe -ArgumentList '-d','%s','--exec','sleep','infinity' -WindowStyle Hidden "+
			"(docs/guide/troubleshooting.md explains the Startup-folder file that repeats this at every Windows sign-in)", err, distro)
		return
	}
	i.logf("WSL keep-alive: wrote %s and started it; members stay online after every Ubuntu window is closed", k.winPath)
}

// Leaves the already-running keep-alive alone and never fails the teardown:
// the file is not a warden artifact the server waits on.
func removeWSLKeepAlive(sys sysOps, dryRun bool, p teardownPaths, logf func(string, ...any)) {
	if !onWSL(sys) {
		return
	}
	if dryRun {
		logf("DRYRUN would remove: the WSL keep-alive script from the Windows Startup folder")
		return
	}
	k, err := locateWSLKeepAlive(sys, p.wslDistro, p.namespace)
	if err != nil {
		logf("warning: could not locate the WSL keep-alive script (%v); if one is left, delete it from the Windows Startup folder (Win+R, shell:startup)", err)
		return
	}
	removeFileTo(logf, sys, false, k.path)
	logf("a WSL keep-alive already running stays until Windows signs out or restarts")
}
