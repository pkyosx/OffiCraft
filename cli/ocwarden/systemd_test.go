package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeUnit struct {
	file      string
	loadState string
	enabled   bool
	active    bool
	mainPID   int
}

// fakeUserManager stands in for systemd --user and logind behind sysOps.run. It
// reads unit files from the recorder's in-memory files on daemon-reload, and
// refuses a systemctl that does not carry its runtime dir, like a real manager
// that cannot find its bus.
type fakeUserManager struct {
	sys        *sysRecorder
	unitDir    string
	runtimeDir string
	uid        string
	units      map[string]*fakeUnit
	linger     bool

	lingerRefused bool
	lingerIgnored bool
	crashLoop     bool
	stopIgnored   bool
	nextPID       int
}

func newFakeUserManager(sys *sysRecorder) *fakeUserManager {
	m := &fakeUserManager{
		sys:        sys,
		unitDir:    "/home/eva/.config/systemd/user",
		runtimeDir: "/run/user/1000",
		uid:        "1000",
		units:      map[string]*fakeUnit{},
		nextPID:    4000,
	}
	sys.perms[systemdBootedMarker] = os.ModeDir | 0o755
	sys.run = func(argv string, _ int) (string, error) { return m.answer(strings.Fields(argv)) }
	return m
}

// installed puts the manager in the state a finished install leaves behind.
func (m *fakeUserManager) installed(unitPath, tokfile, body string) {
	m.sys.files[unitPath] = body
	m.sys.files[tokfile] = jwtWardenOne
	m.nextPID++
	m.units[filepath.Base(unitPath)] = &fakeUnit{file: body, loadState: "loaded", enabled: true, active: true, mainPID: m.nextPID}
}

func (m *fakeUserManager) state(unit string) string {
	u, ok := m.units[unit]
	if !ok {
		return "absent"
	}
	pid := 0
	if u.active {
		pid = u.mainPID
	}
	return fmt.Sprintf("load=%s enabled=%v active=%v pid=%d", u.loadState, u.enabled, u.active, pid)
}

func (m *fakeUserManager) answer(f []string) (string, error) {
	if len(f) > 0 && f[0] == "loginctl" {
		return m.loginctl(f[1:])
	}
	if len(f) < 4 || f[0] != "env" || f[2] != "systemctl" || f[3] != "--user" {
		return "", fmt.Errorf("fake user manager: unexpected command %q", strings.Join(f, " "))
	}
	if f[1] != "XDG_RUNTIME_DIR="+m.runtimeDir {
		return "", errors.New("exit status 1: Failed to connect to bus: No such file or directory")
	}
	return m.systemctl(f[4:])
}

func (m *fakeUserManager) loginctl(a []string) (string, error) {
	switch {
	case len(a) == 2 && a[0] == "enable-linger" && a[1] == m.uid:
		if m.lingerRefused {
			return "", errors.New("exit status 1: Could not enable linger: Access denied")
		}
		if !m.lingerIgnored {
			m.linger = true
		}
		return "", nil
	case len(a) == 5 && a[0] == "show-user" && a[1] == m.uid && a[2] == "-p" && a[3] == "Linger" && a[4] == "--value":
		if m.linger {
			return "yes\n", nil
		}
		return "no\n", nil
	}
	return "", fmt.Errorf("fake loginctl: unexpected %q", strings.Join(a, " "))
}

func (m *fakeUserManager) systemctl(a []string) (string, error) {
	if len(a) == 0 {
		return "", errors.New("fake systemctl: no verb")
	}
	unit := a[len(a)-1]
	u := m.units[unit]
	switch {
	case len(a) == 1 && a[0] == "daemon-reload":
		m.reload()
		return "", nil
	case len(a) == 5 && a[0] == "show" && a[1] == "-p" && a[3] == "--value":
		return m.show(u, a[2]) + "\n", nil
	case len(a) == 2 && a[0] == "enable":
		if u == nil || u.loadState != "loaded" {
			return "", fmt.Errorf("exit status 1: Failed to enable unit: Unit file %s does not exist.", unit)
		}
		if !strings.Contains(u.file, "\nWantedBy=default.target\n") {
			return "", errors.New("exit status 1: The unit files have no installation config")
		}
		u.enabled = true
		return "", nil
	case (len(a) == 2 || len(a) == 3 && a[1] == "--now") && a[0] == "disable":
		if _, onDisk := m.sys.files[filepath.Join(m.unitDir, unit)]; u == nil || !onDisk {
			return "", fmt.Errorf("exit status 1: Failed to disable unit: Unit file %s does not exist.", unit)
		}
		u.enabled = false
		if len(a) == 3 && !m.stopIgnored {
			u.active = false
		}
		return "", nil
	case len(a) == 2 && a[0] == "restart":
		if u == nil || u.loadState != "loaded" {
			return "", fmt.Errorf("exit status 5: Failed to restart %s: Unit %s not found.", unit, unit)
		}
		m.nextPID++
		u.active, u.mainPID = true, m.nextPID
		return "", nil
	case len(a) == 3 && a[0] == "stop" && a[1] == "--no-block":
		if u == nil {
			return "", fmt.Errorf("exit status 5: Failed to stop %s: Unit %s not loaded.", unit, unit)
		}
		u.active = false
		if u.loadState == "not-found" {
			delete(m.units, unit)
		}
		return "", nil
	}
	return "", fmt.Errorf("fake systemctl: unexpected %q", strings.Join(a, " "))
}

func (m *fakeUserManager) reload() {
	for path, body := range m.sys.files {
		if filepath.Dir(path) != m.unitDir || !strings.HasSuffix(path, ".service") {
			continue
		}
		name := filepath.Base(path)
		u := m.units[name]
		if u == nil {
			u = &fakeUnit{}
			m.units[name] = u
		}
		u.file, u.loadState = body, "loaded"
	}
	for name, u := range m.units {
		if _, onDisk := m.sys.files[filepath.Join(m.unitDir, name)]; onDisk {
			continue
		}
		if u.active {
			u.loadState = "not-found"
		} else {
			delete(m.units, name)
		}
	}
}

func (m *fakeUserManager) show(u *fakeUnit, prop string) string {
	switch prop {
	case "LoadState":
		if u == nil {
			return "not-found"
		}
		return u.loadState
	case "ActiveState":
		if u == nil || !u.active {
			return "inactive"
		}
		return "active"
	case "MainPID":
		if u == nil || !u.active {
			return "0"
		}
		if m.crashLoop {
			m.nextPID++
			return strconv.Itoa(m.nextPID)
		}
		return strconv.Itoa(u.mainPID)
	}
	return ""
}

func linuxInstallFixture() wardenPaths {
	return wardenPaths{
		root:       "/home/eva/.officraft",
		home:       "/home/eva",
		label:      "com.officraft.ocwarden",
		srcExe:     "/tmp/clone/ocwarden",
		ocBase:     "https://oc.example.com",
		ocToken:    jwtWardenOne,
		tokfile:    "/home/eva/.officraft/warden/exec-warden.tok",
		logDir:     "/home/eva/.officraft/warden/log",
		binPath:    "/home/eva/.officraft/warden/ocwarden",
		anchorSrc:  "/tmp/clone/officraft",
		anchorPath: "/home/eva/.officraft/warden/officraft",
		ocAgentBin: "/home/eva/.officraft/warden/ocagent",

		minimalPATH: "/home/eva/.local/bin:/usr/local/bin:/usr/bin:/bin",
		goos:        "linux",
		uid:         1000,
		unitDir:     "/home/eva/.config/systemd/user",
		unitPath:    "/home/eva/.config/systemd/user/com.officraft.ocwarden.service",
		runtimeDir:  "/run/user/1000",
	}
}

func linuxTeardownFixture() teardownPaths {
	return teardownPaths{
		tokfile:    "/home/eva/.officraft/warden/exec-warden.tok",
		label:      "com.officraft.ocwarden",
		goos:       "linux",
		unitPath:   "/home/eva/.config/systemd/user/com.officraft.ocwarden.service",
		runtimeDir: "/run/user/1000",
	}
}

const minimalUnit = `# RENDERED by ocwarden install for ROOT=/home/eva/.officraft — do not edit by hand; re-run the installer.
[Unit]
Description=OffiCraft warden (com.officraft.ocwarden)

[Service]
Type=simple
ExecStart="/home/eva/.officraft/warden/officraft"
WorkingDirectory=/home/eva/.officraft
Environment="PATH=/home/eva/.local/bin:/usr/local/bin:/usr/bin:/bin"
Environment="OC_BASE=https://oc.example.com"
Environment="HOME=/home/eva"
Environment="OC_WARDEN_TOKFILE=/home/eva/.officraft/warden/exec-warden.tok"
Restart=always
RestartSec=10
KillMode=process
StandardOutput=append:/home/eva/.officraft/warden/log/ocwarden.out.log
StandardError=append:/home/eva/.officraft/warden/log/ocwarden.err.log

[Install]
WantedBy=default.target
`

const stampedUnit = `# RENDERED by ocwarden install for ROOT=/home/eva/.officraft — do not edit by hand; re-run the installer.
[Unit]
Description=OffiCraft warden (com.officraft.ocwarden)

[Service]
Type=simple
ExecStart="/home/eva/.officraft/warden/officraft"
WorkingDirectory=/home/eva/.officraft
Environment="PATH=/opt/x&y:/bin"
Environment="OC_BASE=https://oc.example.com"
Environment="HOME=/home/eva"
Environment="OC_WARDEN_TOKFILE=/home/eva/.officraft/warden/exec-warden.tok"
Environment="OC_CLAUDE_BIN=/opt/claude"
Environment="OC_CODEX_BIN=/opt/codex"
Restart=always
RestartSec=10
KillMode=process
StandardOutput=append:/home/eva/.officraft/warden/log/ocwarden.out.log
StandardError=append:/home/eva/.officraft/warden/log/ocwarden.err.log

[Install]
WantedBy=default.target
`

func TestRenderUnit(t *testing.T) {
	t.Run("under the plain fixture, the unit carries the anchor, the env and the restart policy", func(t *testing.T) {
		got, err := renderUnit(linuxInstallFixture())
		if err != nil || got != minimalUnit {
			t.Errorf("renderUnit = (\n%s, %v)\nwant\n%s", got, err, minimalUnit)
		}
	})

	t.Run("under specifier and quote characters, every value is escaped for systemd", func(t *testing.T) {
		p := linuxInstallFixture()
		p.namespace = "lab"
		p.label = "com.officraft.ocwarden.lab"
		p.home = "/home/e%v$"
		p.root = "/home/e%v$/.officraft-lab"
		p.anchorPath = "/home/e%v$/.officraft-lab/warden/officraft"
		p.logDir = "/home/e%v$/.officraft-lab/warden/log"
		p.tokfile = "/home/e%v$/.officraft-lab/warden/exec-warden.tok"
		p.plistPATH = `/opt/100%/bin:/opt/my "tools"\bin`
		p.claudeBin = "/opt/claude%1"
		p.credCheck = "0"
		want := `# RENDERED by ocwarden install for ROOT=/home/e%v$/.officraft-lab — do not edit by hand; re-run the installer.
[Unit]
Description=OffiCraft warden (com.officraft.ocwarden.lab)

[Service]
Type=simple
ExecStart="/home/e%%v$$/.officraft-lab/warden/officraft"
WorkingDirectory=/home/e%%v$/.officraft-lab
Environment="PATH=/opt/100%%/bin:/opt/my \"tools\"\\bin"
Environment="OC_BASE=https://oc.example.com"
Environment="HOME=/home/e%%v$"
Environment="OC_WARDEN_TOKFILE=/home/e%%v$/.officraft-lab/warden/exec-warden.tok"
Environment="OC_NAMESPACE=lab"
Environment="OC_CLAUDE_BIN=/opt/claude%%1"
Environment="OC_CLAUDE_CRED_CHECK=0"
Restart=always
RestartSec=10
KillMode=process
StandardOutput=append:/home/e%%v$/.officraft-lab/warden/log/ocwarden.out.log
StandardError=append:/home/e%%v$/.officraft-lab/warden/log/ocwarden.err.log

[Install]
WantedBy=default.target
`
		got, err := renderUnit(p)
		if err != nil || got != want {
			t.Errorf("renderUnit = (\n%s, %v)\nwant\n%s", got, err, want)
		}
	})

	t.Run("under a PATH with a line break, the unit is refused", func(t *testing.T) {
		p := linuxInstallFixture()
		p.plistPATH = "/opt/bin\n[Service]"
		got, err := renderUnit(p)
		if got != "" || err == nil || err.Error() != "PATH contains a line break, which a systemd unit cannot carry" {
			t.Errorf("renderUnit = (%q, %v), want a line-break refusal", got, err)
		}
	})

	t.Run("under a HOME with a line break, the unit is refused", func(t *testing.T) {
		p := linuxInstallFixture()
		p.root = "/home/e\nva/.officraft"
		got, err := renderUnit(p)
		if got != "" || err == nil || err.Error() != "ROOT contains a line break, which a systemd unit cannot carry" {
			t.Errorf("renderUnit = (%q, %v), want a line-break refusal", got, err)
		}
	})
}

func TestSystemdPreflight(t *testing.T) {
	notBooted := "systemd is not running as PID 1 on this host (/run/systemd/system is missing), " +
		"so there is no systemd user manager to run the warden under. " +
		"On WSL2: put `[boot]` / `systemd=true` in /etc/wsl.conf, run `wsl.exe --shutdown` from Windows, " +
		"reopen the distro and re-run the installer"

	t.Run("under a booted host without linger, linger ends up on", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		if err := i.systemdPreflight(linuxInstallFixture()); err != nil {
			t.Fatalf("systemdPreflight: %v", err)
		}
		if !m.linger {
			t.Error("linger = off, want on")
		}
		if want := "[ocwarden install] enabled linger for uid 1000\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("under linger already on, it passes and says so", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.linger = true
		m.lingerRefused = true
		if err := i.systemdPreflight(linuxInstallFixture()); err != nil {
			t.Fatalf("systemdPreflight: %v", err)
		}
		if want := "[ocwarden install] linger already on for uid 1000\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("under a host where systemd is not PID 1, the install is refused with the WSL hint", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		delete(sys.perms, systemdBootedMarker)
		err := i.systemdPreflight(linuxInstallFixture())
		if err == nil || err.Error() != notBooted {
			t.Errorf("err = %v, want %q", err, notBooted)
		}
		if m.linger {
			t.Error("linger was turned on for a host with no user manager")
		}
	})

	t.Run("under polkit refusing linger, the error names the admin command", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.lingerRefused = true
		err := i.systemdPreflight(linuxInstallFixture())
		want := "loginctl enable-linger 1000 failed: exit status 1: Could not enable linger: Access denied — " +
			"without linger the warden stops at logout and does not start at boot; " +
			"ask an administrator to run `sudo loginctl enable-linger 1000`, then re-run the installer"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("under an enable-linger that exits 0 but changes nothing, the install is refused", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.lingerIgnored = true
		err := i.systemdPreflight(linuxInstallFixture())
		want := "loginctl enable-linger 1000 exited 0 but Linger=no — " +
			"without linger the warden stops at logout and does not start at boot; " +
			"ask an administrator to run `sudo loginctl enable-linger 1000`, then re-run the installer"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("under a dry run, linger is narrated and left alone", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		m := newFakeUserManager(sys)
		if err := i.systemdPreflight(linuxInstallFixture()); err != nil {
			t.Fatalf("systemdPreflight: %v", err)
		}
		if m.linger {
			t.Error("a dry run turned linger on")
		}
		want := "[ocwarden install] DRYRUN would: require /run/systemd/system (systemd as PID 1), then `loginctl enable-linger 1000` unless Linger=yes\n"
		if out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})
}

func TestWriteUnit(t *testing.T) {
	p := linuxInstallFixture()

	t.Run("under a live run, the rendered unit is written 0644", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		if err := i.writeUnit(p); err != nil {
			t.Fatalf("writeUnit: %v", err)
		}
		if sys.files[p.unitPath] != minimalUnit || sys.perms[p.unitPath] != 0o644 {
			t.Errorf("unit = %o\n%s\nwant 644\n%s", sys.perms[p.unitPath], sys.files[p.unitPath], minimalUnit)
		}
		if want := "[ocwarden install] unit rendered: /home/eva/.config/systemd/user/com.officraft.ocwarden.service\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("under a dry run, the unit is narrated and not written", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		if err := i.writeUnit(p); err != nil {
			t.Fatalf("writeUnit: %v", err)
		}
		if len(sys.files) != 0 {
			t.Errorf("a dry run wrote %v", sys.files)
		}
		want := "[ocwarden install] DRYRUN would: mkdir -p /home/eva/.config/systemd/user; render unit -> /home/eva/.config/systemd/user/com.officraft.ocwarden.service\n"
		if out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("under an unrenderable value, nothing is written", func(t *testing.T) {
		bad := p
		bad.plistPATH = "/bin\n"
		i, sys, _ := newRecordingInstaller(false)
		err := i.writeUnit(bad)
		if err == nil || err.Error() != "cannot render the systemd unit: PATH contains a line break, which a systemd unit cannot carry" {
			t.Errorf("err = %v, want the render refusal", err)
		}
		if len(sys.files) != 0 {
			t.Errorf("a refused render wrote %v", sys.files)
		}
	})

	t.Run("under an unwritable config dir, the mkdir failure is returned", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		sys.fail["mkdir /home/eva/.config/systemd/user 755"] = errors.New("permission denied")
		err := i.writeUnit(p)
		if err == nil || err.Error() != "mkdir systemd user unit dir /home/eva/.config/systemd/user: permission denied" {
			t.Errorf("err = %v, want the mkdir failure", err)
		}
	})
}

func TestSystemdReinstall(t *testing.T) {
	p := linuxInstallFixture()
	unit := "com.officraft.ocwarden.service"

	t.Run("under a fresh unit file, the unit ends loaded, enabled and running", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		sys.files[p.unitPath] = minimalUnit
		if err := i.systemdReinstall(p); err != nil {
			t.Fatalf("systemdReinstall: %v", err)
		}
		if got, want := m.state(unit), "load=loaded enabled=true active=true pid=4001"; got != want {
			t.Errorf("unit state = %s, want %s", got, want)
		}
		if m.units[unit].file != minimalUnit {
			t.Errorf("the manager loaded\n%s\nwant\n%s", m.units[unit].file, minimalUnit)
		}
		if want := "[ocwarden install] enabled + restarted com.officraft.ocwarden.service (exact unit)\n"; out.String() != want {
			t.Errorf("log = %q, want %q", out.String(), want)
		}
	})

	t.Run("under a running warden, a reinstall loads the new file and restarts under a new pid", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		sys.files[p.unitPath] = stampedUnit
		if err := i.systemdReinstall(p); err != nil {
			t.Fatalf("systemdReinstall: %v", err)
		}
		if got, want := m.state(unit), "load=loaded enabled=true active=true pid=4002"; got != want {
			t.Errorf("unit state = %s, want %s", got, want)
		}
		if m.units[unit].file != stampedUnit {
			t.Errorf("the manager still runs\n%s\nwant\n%s", m.units[unit].file, stampedUnit)
		}
	})

	t.Run("under an unreachable user manager, the install fails and nothing is loaded", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.runtimeDir = "/run/user/2000"
		sys.files[p.unitPath] = minimalUnit
		err := i.systemdReinstall(p)
		want := "systemctl --user daemon-reload failed: exit status 1: Failed to connect to bus: No such file or directory — " +
			"the systemd user manager for uid 1000 is not reachable"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if got := m.state(unit); got != "absent" {
			t.Errorf("unit state = %s, want absent", got)
		}
	})

	t.Run("under a unit file the manager cannot see, the install names the file", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.unitDir = "/etc/systemd/user"
		sys.files[p.unitPath] = minimalUnit
		err := i.systemdReinstall(p)
		want := "the systemd user manager did not load com.officraft.ocwarden.service after daemon-reload (LoadState=not-found); " +
			"the unit file to look at is /home/eva/.config/systemd/user/com.officraft.ocwarden.service"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})

	t.Run("under a unit file with no [Install] section, enable fails and nothing runs", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		sys.files[p.unitPath] = strings.TrimSuffix(minimalUnit, "[Install]\nWantedBy=default.target\n")
		err := i.systemdReinstall(p)
		want := "systemctl --user enable com.officraft.ocwarden.service failed: exit status 1: The unit files have no installation config"
		if err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
		if got := m.state(unit); got != "load=loaded enabled=false active=false pid=0" {
			t.Errorf("unit state = %s, want loaded but neither enabled nor running", got)
		}
	})

	t.Run("under a dry run, the verbs are narrated and the manager is untouched", func(t *testing.T) {
		i, sys, out := newRecordingInstaller(true)
		m := newFakeUserManager(sys)
		sys.files[p.unitPath] = minimalUnit
		if err := i.systemdReinstall(p); err != nil {
			t.Fatalf("systemdReinstall: %v", err)
		}
		if got := m.state(unit); got != "absent" {
			t.Errorf("unit state = %s, want absent", got)
		}
		want := "[ocwarden install] DRYRUN would run: systemctl --user daemon-reload, then require LoadState=loaded for com.officraft.ocwarden.service\n" +
			"[ocwarden install] DRYRUN would run: systemctl --user enable com.officraft.ocwarden.service\n" +
			"[ocwarden install] DRYRUN would run: systemctl --user restart com.officraft.ocwarden.service\n"
		if out.String() != want {
			t.Errorf("log =\n%s\nwant\n%s", out.String(), want)
		}
	})
}

func TestUnitMainPID(t *testing.T) {
	p := linuxInstallFixture()

	t.Run("under a running unit, its main pid is returned", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		if got := i.unitMainPID(p); got != "4001" {
			t.Errorf("unitMainPID = %q, want %q", got, "4001")
		}
	})

	t.Run("under a stopped unit, there is no pid", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		m.units["com.officraft.ocwarden.service"].active = false
		if got := i.unitMainPID(p); got != "" {
			t.Errorf("unitMainPID = %q, want empty", got)
		}
	})

	t.Run("under an unreachable manager, there is no pid", func(t *testing.T) {
		i, sys, _ := newRecordingInstaller(false)
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		m.runtimeDir = "/run/user/2000"
		if got := i.unitMainPID(p); got != "" {
			t.Errorf("unitMainPID = %q, want empty", got)
		}
	})
}

func TestSystemdTeardown(t *testing.T) {
	t.Skip("its outcomes (unit state, removed files, transcript) are asserted through doTeardown in TestDoTeardown")
}

func TestStopUnitNoBlock(t *testing.T) {
	p := linuxTeardownFixture()
	unit := "com.officraft.ocwarden.service"

	t.Run("under a unit already disabled and unloaded from inside, the stop leaves nothing behind", func(t *testing.T) {
		sys := newSysRecorder()
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		if ok, _ := doTeardown(sys.ops(), false, p, true); !ok {
			t.Fatal("the inside teardown was not confirmed")
		}
		if err := stopUnitNoBlock(sys.ops(), p); err != nil {
			t.Fatalf("stopUnitNoBlock: %v", err)
		}
		if got := m.state(unit); got != "absent" {
			t.Errorf("unit state = %s, want absent", got)
		}
	})

	t.Run("under an unreachable manager, the stop is an error and the unit keeps running", func(t *testing.T) {
		sys := newSysRecorder()
		m := newFakeUserManager(sys)
		m.installed(p.unitPath, p.tokfile, minimalUnit)
		m.runtimeDir = "/run/user/2000"
		err := stopUnitNoBlock(sys.ops(), p)
		if err == nil || err.Error() != "exit status 1: Failed to connect to bus: No such file or directory" {
			t.Errorf("err = %v, want the bus failure", err)
		}
		if got := m.state(unit); got != "load=loaded enabled=true active=true pid=4001" {
			t.Errorf("unit state = %s, want it still running", got)
		}
	})
}

// fakeWindowsSide stands in for the Windows half of a WSL2 host behind
// sysOps.run: the kernel release that marks WSL2, wslpath, cmd.exe and
// wscript.exe, whose started scripts it records. Every other command goes on to
// the run it wraps.
type fakeWindowsSide struct {
	distroRoot string
	interopOff bool
	started    []string
}

func newFakeWindowsSide(sys *sysRecorder) *fakeWindowsSide {
	w := &fakeWindowsSide{distroRoot: `\\wsl.localhost\Ubuntu-24.04\`}
	sys.files["/proc/sys/kernel/osrelease"] = "5.15.167.4-microsoft-standard-WSL2\n"
	next := sys.run
	sys.run = func(argv string, nth int) (string, error) {
		windows := strings.HasPrefix(argv, "env -C /mnt/c /mnt/c/Windows/System32/")
		if windows && w.interopOff {
			return "", errors.New("exec format error")
		}
		switch {
		case argv == "/usr/bin/wslpath -w /":
			return w.distroRoot + "\n", nil
		case argv == "env -C /mnt/c /mnt/c/Windows/System32/cmd.exe /c echo %APPDATA%":
			return `C:\Users\eva\AppData\Roaming` + "\r\n", nil
		case argv == `/usr/bin/wslpath -u C:\Users\eva\AppData\Roaming`:
			return "/mnt/c/Users/eva/AppData/Roaming\n", nil
		case strings.HasPrefix(argv, "env -C /mnt/c /mnt/c/Windows/System32/wscript.exe "):
			w.started = append(w.started, strings.TrimPrefix(argv, "env -C /mnt/c /mnt/c/Windows/System32/wscript.exe "))
			return "", nil
		case windows:
			return "", fmt.Errorf("fake Windows side: unexpected %q", argv)
		}
		if next == nil {
			return "", nil
		}
		return next(argv, nth)
	}
	return w
}

const (
	keepAlivePath    = "/mnt/c/Users/eva/AppData/Roaming/Microsoft/Windows/Start Menu/Programs/Startup/OffiCraft keep WSL running (Ubuntu-24.04).vbs"
	keepAliveWinPath = `C:\Users\eva\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup\OffiCraft keep WSL running (Ubuntu-24.04).vbs`
	keepAliveScript  = `CreateObject("WScript.Shell").Run "wsl.exe -d Ubuntu-24.04 --exec sleep infinity", 0, False` + "\r\n"
)
