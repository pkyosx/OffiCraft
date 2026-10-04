package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// escapeeLine is a shell line that starts a process in a session of its own,
// out of reach of a process-group kill, which holds the shell's stdout and
// stderr open for a minute and records its pid in pidFile; the shell goes on
// only once it has, so a kill can never land before the escape.
func escapeeLine(pidFile string) string {
	return `{ /usr/bin/perl -MPOSIX -e 'POSIX::setsid(); open(my $f, ">", $ARGV[0]) or die; print $f $$; close $f; sleep 60' '` +
		pidFile + `' & while [ ! -s '` + pidFile + `' ]; do /bin/sleep 0.05; done; }`
}

// chattyEscapeeLine is escapeeLine for an escapee that writes a line to the
// shell's stdout every 20ms until the pipe is closed on it.
func chattyEscapeeLine(pidFile string) string {
	return `{ /usr/bin/perl -MPOSIX -e 'POSIX::setsid(); $| = 1; open(my $f, ">", $ARGV[0]) or die; print $f $$; close $f; ` +
		`while (1) { print "still here\n"; select(undef, undef, undef, 0.02) }' '` +
		pidFile + `' & while [ ! -s '` + pidFile + `' ]; do /bin/sleep 0.05; done; }`
}

// lateEscapeeLine is escapeeLine for an escapee that prints one line on the
// shell's stderr 300ms later and exits.
func lateEscapeeLine(pidFile string) string {
	return `{ /usr/bin/perl -MPOSIX -e 'POSIX::setsid(); open(my $f, ">", $ARGV[0]) or die; print $f $$; close $f; ` +
		`select(undef, undef, undef, 0.3); print STDERR "Error: written after the exit\n"' '` +
		pidFile + `' & while [ ! -s '` + pidFile + `' ]; do /bin/sleep 0.05; done; }`
}

// escapeeAlive reports whether the escapee recorded in pidFile is running, and
// kills it when the test ends.
func escapeeAlive(t *testing.T, pidFile string) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if n, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && convErr == nil && n > 0 {
			pid = n
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the escapee never recorded its pid")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return syscall.Kill(pid, 0) == nil
}

// fakeUpdater is a stand-in for the claude CLI's version and update commands:
// `--version` prints the version file (exit 1 when the noversion file exists);
// `update` records its pid and the FROM_FILE it was started with, prints a
// line, sleeps when the hang file exists, copies the next-version file over
// the version file when there is one, prints the out file on stderr and exits
// with the code in the rc file.
type fakeUpdater struct {
	root, bin, version, next, noVersion, pid, sawEnv, ran, hang, out, rc, escape, escapee, chatter, late string
}

func newFakeUpdater(t *testing.T) *fakeUpdater {
	t.Helper()
	root := t.TempDir()
	f := &fakeUpdater{
		root:      root,
		version:   filepath.Join(root, "version"),
		next:      filepath.Join(root, "next"),
		noVersion: filepath.Join(root, "noversion"),
		pid:       filepath.Join(root, "pid"),
		sawEnv:    filepath.Join(root, "saw-env"),
		ran:       filepath.Join(root, "ran"),
		hang:      filepath.Join(root, "hang"),
		out:       filepath.Join(root, "out"),
		rc:        filepath.Join(root, "rc"),
		escape:    filepath.Join(root, "escape"),
		escapee:   filepath.Join(root, "escapee-pid"),
		chatter:   filepath.Join(root, "chatter"),
		late:      filepath.Join(root, "late"),
	}
	f.bin = stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n"+
		`case "$1" in`+"\n"+
		`--version)`+"\n"+
		`  [ -f '`+f.noVersion+`' ] && { echo 'claude: broken install' >&2; exit 1; }`+"\n"+
		`  printf '%s (Claude Code)\n' "$(/bin/cat '`+f.version+`')"; exit 0;;`+"\n"+
		`update)`+"\n"+
		`  echo $$ > '`+f.pid+`'`+"\n"+
		`  printf '%s' "${FROM_FILE-unset}" > '`+f.sawEnv+`'`+"\n"+
		`  : > '`+f.ran+`'`+"\n"+
		`  [ -f '`+f.escape+`' ] && `+escapeeLine(f.escapee)+"\n"+
		`  [ -f '`+f.chatter+`' ] && `+chattyEscapeeLine(f.escapee)+"\n"+
		`  [ -f '`+f.late+`' ] && `+lateEscapeeLine(f.escapee)+"\n"+
		`  echo 'Checking for updates...'`+"\n"+
		`  [ -f '`+f.hang+`' ] && /bin/sleep 30`+"\n"+
		`  [ -f '`+f.next+`' ] && /bin/cp '`+f.next+`' '`+f.version+`'`+"\n"+
		`  [ -f '`+f.out+`' ] && /bin/cat '`+f.out+`' >&2`+"\n"+
		`  exit "$(/bin/cat '`+f.rc+`')";;`+"\n"+
		`esac`+"\n"+
		`exit 2`+"\n")
	f.write(t, f.version, "2.1.200")
	f.write(t, f.rc, "0")
	return f
}

func (f *fakeUpdater) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeUpdater) updateRan() bool {
	_, err := os.Stat(f.ran)
	return err == nil
}

func (f *fakeUpdater) alive(t *testing.T) bool {
	t.Helper()
	raw, err := os.ReadFile(f.pid)
	if err != nil {
		t.Fatalf("the fake update never recorded its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return syscall.Kill(pid, 0) == nil
}

type upgradeHarness struct {
	relay   *upgradeRelay
	fake    *fakeUpdater
	reports chan upgradeReport
	answer  func(upgradeReport) string

	mu     sync.Mutex
	events []string
}

func newUpgradeHarness(t *testing.T) *upgradeHarness {
	t.Helper()
	h := &upgradeHarness{fake: newFakeUpdater(t), reports: make(chan upgradeReport, 16)}
	t.Setenv("PATH", filepath.Join(h.fake.root, "no-binaries-here"))
	home := filepath.Join(h.fake.root, "home")
	env := envMap(map[string]string{"HOME": home, "OC_CLAUDE_BIN": h.fake.bin})
	prober := newLoginProber(env, &wardenRunner{}, shellKeep{}, "linux", &launchEnvCache{}, nil)
	prober.claudeHome = claudeHome{Home: home}
	prober.agentHome = filepath.Join(h.fake.root, "agents")
	prober.envFile = filepath.Join(h.fake.root, "env")
	prober.captureEnv = nil
	h.fake.write(t, prober.envFile, "FROM_FILE=file-value\n")
	h.relay = newUpgradeRelay(prober, func(rep upgradeReport) string {
		h.note("report " + rep.State)
		h.reports <- rep
		if h.answer != nil {
			return h.answer(rep)
		}
		return rep.State
	}, func() { h.note("after") }, nil)
	t.Cleanup(func() {
		h.relay.mu.Lock()
		run := h.relay.current
		h.relay.mu.Unlock()
		if run != nil {
			h.relay.mu.Lock()
			proc := run.proc
			h.relay.mu.Unlock()
			if proc != nil {
				proc.kill()
			}
			select {
			case <-run.done:
			case <-time.After(10 * time.Second):
				t.Error("the upgrade run never ended")
			}
		}
	})
	return h
}

func (h *upgradeHarness) note(event string) {
	h.mu.Lock()
	h.events = append(h.events, event)
	h.mu.Unlock()
}

func (h *upgradeHarness) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

func (h *upgradeHarness) next(t *testing.T) upgradeReport {
	t.Helper()
	select {
	case rep := <-h.reports:
		return rep
	case <-time.After(10 * time.Second):
		t.Fatal("no upgrade report arrived within 10s")
		return upgradeReport{}
	}
}

func (h *upgradeHarness) wantNoReport(t *testing.T) {
	t.Helper()
	select {
	case rep := <-h.reports:
		t.Fatalf("an unexpected report arrived: %+v", rep)
	case <-time.After(200 * time.Millisecond):
	}
}

func (h *upgradeHarness) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		h.relay.mu.Lock()
		idle := h.relay.current == nil
		h.relay.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the upgrade is still running after 10s")
}

func (h *upgradeHarness) wantRendersGone(t *testing.T) {
	t.Helper()
	renders, _ := filepath.Glob(filepath.Join(h.relay.prober.agentHome, loginRenderPrefix+"*"))
	if len(renders) != 0 {
		t.Errorf("env renders left behind: %v", renders)
	}
}

const upgradeUnchangedReason = "版本沒有變（仍是 2.1.200）：可能已經是最新版，或 claude update 升級的不是成員使用的那一份 Claude Code"

func TestUpgradeRelay(t *testing.T) {
	t.Run("under an update that raises the version, the relay reports running with the old version, then succeeded with both, after letting the heartbeat re-read the version", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.next, "2.1.290")
		h.relay.Start("ru-1", "claude")
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-1", State: "running", FromVersion: "2.1.200"}); got != want {
			t.Fatalf("first report = %+v, want %+v", got, want)
		}
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-1", State: "succeeded",
			FromVersion: "2.1.200", ToVersion: "2.1.290"}); got != want {
			t.Fatalf("second report = %+v, want %+v", got, want)
		}
		h.waitIdle(t)
		if want := []string{"report running", "after", "report succeeded"}; !reflect.DeepEqual(h.seen(), want) {
			t.Errorf("events = %v, want %v", h.seen(), want)
		}
		if raw, _ := os.ReadFile(h.fake.sawEnv); string(raw) != "file-value" {
			t.Errorf("claude update saw FROM_FILE=%q, want the member env file's file-value", raw)
		}
		h.wantRendersGone(t)
		h.wantNoReport(t)
	})

	t.Run("under an update that leaves the version unchanged, the relay reports failed with the unchanged reason and the version read back", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.relay.Start("ru-2", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-2", State: "failed",
			FromVersion: "2.1.200", ToVersion: "2.1.200", Reason: upgradeUnchangedReason}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if !h.fake.updateRan() {
			t.Error("control: claude update never ran")
		}
	})

	t.Run("under an update that leaves an older version, the relay reports failed saying so", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.next, "2.1.100")
		h.relay.Start("ru-o", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-o", State: "failed",
			FromVersion: "2.1.200", ToVersion: "2.1.100", Reason: "升級後的版本 2.1.100 比升級前的 2.1.200 舊"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under an update that exits non-zero, the relay reports failed with the exit and the last line it printed, and the version read back", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.rc, "1")
		h.fake.write(t, h.fake.out, "\x1b[31mError: EACCES: permission denied\x1b[0m\n")
		h.relay.Start("ru-3", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-3", State: "failed", FromVersion: "2.1.200",
			ToVersion: "2.1.200", Reason: "claude update 執行失敗（exit status 1）：Error: EACCES: permission denied"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if want := []string{"report running", "after", "report failed"}; !reflect.DeepEqual(h.seen(), want) {
			t.Errorf("events = %v, want %v", h.seen(), want)
		}
	})

	t.Run("under an update still running at the cap, its process is killed and the relay reports failed with the cap", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.relay.cap = 300 * time.Millisecond
		h.fake.write(t, h.fake.hang, "")
		h.relay.Start("ru-4", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-4", State: "failed", FromVersion: "2.1.200",
			ToVersion: "2.1.200", Reason: "claude update 超過 300ms 沒有結束，已中止"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if h.fake.alive(t) {
			t.Error("the update process outlived its cap")
		}
	})

	t.Run("under an update that leaves a descendant in its own session holding the output open, the relay still reports succeeded within seconds and takes the next upgrade", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.escape, "")
		h.fake.write(t, h.fake.next, "2.1.290")
		h.relay.Start("ru-e1", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-e1", State: "succeeded",
			FromVersion: "2.1.200", ToVersion: "2.1.290"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.waitIdle(t)
		if !escapeeAlive(t, h.fake.escapee) {
			t.Fatal("control: the escapee was not running, so nothing held the output open")
		}
		h.relay.Start("ru-e2", "claude")
		if got := h.next(t); got.UpgradeID != "ru-e2" || got.State != "running" {
			t.Fatalf("the next upgrade's first report = %+v, want ru-e2 running", got)
		}
	})

	t.Run("under an update killed at its cap whose escaped descendant holds the output open, the relay still reports failed within seconds", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.relay.cap = 300 * time.Millisecond
		h.fake.write(t, h.fake.escape, "")
		h.fake.write(t, h.fake.hang, "")
		h.relay.Start("ru-e3", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-e3", State: "failed", FromVersion: "2.1.200",
			ToVersion: "2.1.200", Reason: "claude update 超過 300ms 沒有結束，已中止"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.waitIdle(t)
		if !escapeeAlive(t, h.fake.escapee) {
			t.Fatal("control: the escapee was not running, so nothing held the output open")
		}
	})

	t.Run("under a descendant that prints a line shortly after the update exited, that line is still the failure's last line", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.late, "")
		h.fake.write(t, h.fake.rc, "1")
		h.relay.Start("ru-l", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-l", State: "failed", FromVersion: "2.1.200",
			ToVersion: "2.1.200", Reason: "claude update 執行失敗（exit status 1）：Error: written after the exit"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		escapeeAlive(t, h.fake.escapee)
	})

	t.Run("under an escaped descendant that never stops writing, the output is cut off at the drain cap and the relay still reports succeeded", func(t *testing.T) {
		grace, limit := pipeDrainGrace, pipeDrainCap
		pipeDrainCap = time.Second
		t.Cleanup(func() { pipeDrainGrace, pipeDrainCap = grace, limit })
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.chatter, "")
		h.fake.write(t, h.fake.next, "2.1.290")
		h.relay.Start("ru-c", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-c", State: "succeeded",
			FromVersion: "2.1.200", ToVersion: "2.1.290"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.waitIdle(t)
		escapeeAlive(t, h.fake.escapee)
	})

	t.Run("under a version that cannot be read before the update, the relay reports failed and never runs claude update", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.noVersion, "")
		h.relay.Start("ru-5", "claude")
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-5", State: "failed",
			Reason: "讀不到升級前的 Claude Code 版本（claude --version：exit status 1），沒有執行升級"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.waitIdle(t)
		if h.fake.updateRan() {
			t.Error("claude update ran without a version to compare against")
		}
	})

	t.Run("under a server that answers running with expired, claude update is not run", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.answer = func(upgradeReport) string { return "expired" }
		h.relay.Start("ru-6", "claude")
		h.next(t)
		h.waitIdle(t)
		h.wantNoReport(t)
		if h.fake.updateRan() {
			t.Error("claude update ran for an upgrade the server had already ended")
		}
	})

	t.Run("under a start for another upgrade while one runs, the second is reported failed and a repeat of the first is ignored", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.hang, "")
		h.relay.Start("ru-a", "claude")
		h.next(t)
		h.relay.Start("ru-a", "claude")
		h.relay.Start("ru-b", "claude")
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-b", State: "failed",
			Reason: "這台機器已經有另一個 Claude Code 升級在進行，請等它結束再試"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.wantNoReport(t)
	})

	t.Run("under a runtime other than claude, the relay reports failed and runs nothing", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.relay.Start("ru-7", "codex")
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-7", State: "failed",
			Reason: `這個 warden 只能升級 Claude Code，不能升級 "codex"`}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		if h.fake.updateRan() {
			t.Error("claude update ran for a codex upgrade")
		}
	})

	t.Run("under no claude on the machine, the relay reports failed", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.relay.resolveBin = func() string { return "" }
		h.relay.Start("ru-8", "claude")
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-8", State: "failed",
			Reason: "這台機器沒有安裝 Claude Code"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
	})
}

func TestUpdaterExecAbandonsARunningUpgrade(t *testing.T) {
	t.Run("under a running claude update, the self-update exec first kills it and reports it failed", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.hang, "")
		h.relay.Start("ru-x", "claude")
		h.next(t)
		deadline := time.Now().Add(10 * time.Second)
		for !h.fake.updateRan() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !h.fake.alive(t) {
			t.Fatal("control: the update process is not running")
		}
		var aliveAtExec []bool
		up := &updater{execSelf: func() error {
			aliveAtExec = append(aliveAtExec, h.fake.alive(t))
			return fmt.Errorf("exec refused in test")
		}}
		transport := &sseTransport{deps: CommandDeps{Upgrade: h.relay}}
		wireUpdaterSeams(transport, up)
		if err := up.execSelf(); err == nil || err.Error() != "exec refused in test" {
			t.Fatalf("execSelf = %v", err)
		}
		if !reflect.DeepEqual(aliveAtExec, []bool{false}) {
			t.Errorf("update process alive at exec = %v, want [false]", aliveAtExec)
		}
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-x", State: "failed",
			Reason: "warden 為了更新自己而重新啟動，升級被中止；請重新開始升級"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.wantNoReport(t)
	})
}

// recordingUpgrade is an UpgradeSeam that only records what reached it.
type recordingUpgrade struct{ calls []string }

func (r *recordingUpgrade) Start(id, runtime string) {
	r.calls = append(r.calls, "start "+id+" "+runtime)
}
func (r *recordingUpgrade) Abandon() { r.calls = append(r.calls, "abandon") }

func TestDispatchUpgradeCommand(t *testing.T) {
	frame := func(args string) []byte {
		return []byte(`{"topic":"warden-command","data":{"rpc":"runtime_upgrade","args":` + args + `}}`)
	}
	t.Run("under a runtime_upgrade frame, it reaches the upgrade seam with its arguments and sends no receipt", func(t *testing.T) {
		seam := &recordingUpgrade{}
		var receipts []CommandResult
		cmd, err := parseCommandFrame(frame(`{"member_id":"m-box","upgrade_id":"ru-1","runtime":"claude"}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := dispatchCommand(cmd, CommandDeps{Upgrade: seam, Report: func(cr CommandResult) error {
			receipts = append(receipts, cr)
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
		if want := []string{"start ru-1 claude"}; !reflect.DeepEqual(seam.calls, want) {
			t.Errorf("seam calls = %v, want %v", seam.calls, want)
		}
		if len(receipts) != 0 {
			t.Errorf("a runtime_upgrade frame produced a command_result receipt: %+v", receipts)
		}
	})

	t.Run("under a malformed upgrade_id, dispatch refuses it and the seam is not reached", func(t *testing.T) {
		seam := &recordingUpgrade{}
		cmd, err := parseCommandFrame(frame(`{"member_id":"m-box","upgrade_id":"../x","runtime":"claude"}`))
		if err != nil {
			t.Fatal(err)
		}
		err = dispatchCommand(cmd, CommandDeps{Upgrade: seam})
		if err == nil || err.Error() != "command: runtime_upgrade frame missing a valid upgrade_id" {
			t.Fatalf("err = %v", err)
		}
		if len(seam.calls) != 0 {
			t.Errorf("the seam was reached: %v", seam.calls)
		}
	})

	t.Run("under no upgrade seam, dispatch refuses", func(t *testing.T) {
		cmd, err := parseCommandFrame(frame(`{"member_id":"m-box","upgrade_id":"ru-1","runtime":"claude"}`))
		if err != nil {
			t.Fatal(err)
		}
		err = dispatchCommand(cmd, CommandDeps{})
		if err == nil || err.Error() != "command: runtime_upgrade refused: upgrade seam not wired" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestWireLoginCheckUpgrade(t *testing.T) {
	t.Run("under the wired upgrade seam, a runtime_upgrade posts running then succeeded to the station, invalidates the claude probe and kicks the heartbeat", func(t *testing.T) {
		fake := newFakeUpdater(t)
		fake.write(t, fake.next, "2.1.290")
		t.Setenv("PATH", filepath.Join(fake.root, "no-binaries-here"))
		home := filepath.Join(fake.root, "home")
		env := envMap(map[string]string{
			"HOME": home, "OC_AGENT_HOME": filepath.Join(fake.root, "agents"),
			"OC_AGENT_ENV_FILE": filepath.Join(fake.root, "no-env-file"), "OC_AGENT_ENV_INHERIT": "0",
			"OC_CLAUDE_BIN": fake.bin,
		})
		runner := keepWardenRunner{
			wardenRunner:    &wardenRunner{fallback: wardenRun{err: errors.New("unscripted")}},
			keepShellRunner: &keepShellRunner{},
		}
		invalidated := make(chan struct{}, 4)
		var kicked bool
		bodies := wireBodies(t, func(base string) {
			deps, login, _ := wireLoginCheck(Config{Base: base, Token: "tok", ID: "m-box"}, env, runner, "linux", nil,
				func() { invalidated <- struct{}{} })
			login.captureEnv = nil
			cmd, err := parseCommandFrame([]byte(`{"topic":"warden-command","data":{"rpc":"runtime_upgrade",` +
				`"args":{"member_id":"m-box","upgrade_id":"ru-w","runtime":"claude"}}}`))
			if err != nil {
				t.Fatal(err)
			}
			if err := dispatchCommand(cmd, deps); err != nil {
				t.Fatal(err)
			}
			relay := deps.Upgrade.(*upgradeRelay)
			deadline := time.Now().Add(10 * time.Second)
			for {
				relay.mu.Lock()
				idle := relay.current == nil
				relay.mu.Unlock()
				if idle && len(invalidated) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the upgrade did not finish within 10s")
				}
				time.Sleep(10 * time.Millisecond)
			}
			select {
			case <-login.kicked():
				kicked = true
			default:
			}
		})
		want := []map[string]any{
			{"upgrade_id": "ru-w", "state": "running", "from_version": "2.1.200"},
			{"upgrade_id": "ru-w", "state": "succeeded", "from_version": "2.1.200", "to_version": "2.1.290"},
		}
		if !reflect.DeepEqual(bodies, want) {
			t.Errorf("posted bodies =\n  %#v\nwant\n  %#v", bodies, want)
		}
		if len(invalidated) != 1 {
			t.Errorf("the claude probe was invalidated %d time(s), want 1", len(invalidated))
		}
		if !kicked {
			t.Error("a finished upgrade did not kick the heartbeat")
		}
	})
}

func TestStartUpgradeProcessRefusesARealBinaryInATestBinary(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		_, _ = startUpgradeProcess("/bin/sh", "exit 0", "/usr/bin/true")
		fmt.Println("startUpgradeProcess ran a binary outside the temp dir")
		return
	}
	code, out := runRefusalChild(t, "TestStartUpgradeProcessRefusesARealBinaryInATestBinary")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "\nFATAL: refusing to run a real /usr/bin/true update inside a test binary.\n" +
		"Upgrade tests must stage a fake CLI in a temp dir and inject it.\n"
	if !strings.Contains(out, want) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, want)
	}
	if strings.Contains(out, "startUpgradeProcess ran a binary outside the temp dir") {
		t.Error("a test binary was allowed to start an update process from a real binary")
	}
	fake := stageBinary(t, filepath.Join(t.TempDir(), "claude"), "#!/bin/sh\nexit 0\n")
	proc, err := startUpgradeProcess("/bin/sh", "exec "+fake, fake)
	if err != nil {
		t.Fatalf("control: a temp-dir fake could not start: %v", err)
	}
	if err := proc.wait(); err != nil {
		t.Fatalf("control: the fake did not exit 0: %v", err)
	}
}

// The three notices below are `claude update`'s output under Homebrew as
// measured on a real machine (2.1.288 from claude-code@latest; 2.1.285 and
// 2.1.280 from claude-code); it exits 0 in all three without upgrading.
const (
	brewLatestUpToDate = "Current version: 2.1.288\nChecking for updates to latest version...\n\n" +
		"Claude is managed by Homebrew.\nClaude is up to date!\n"
	brewStableUpToDate = "Current version: 2.1.285\nChecking for updates to stable version...\n\n" +
		"Claude is managed by Homebrew.\nClaude is up to date!\n\n" +
		"Tip: For more frequent updates, use the claude-code@latest cask:\n" +
		"  brew uninstall --cask claude-code && brew install --cask claude-code@latest\n"
	brewStableBehind = "Current version: 2.1.280\nChecking for updates to stable version...\n\n" +
		"Claude is managed by Homebrew.\nUpdate available: 2.1.280 → 2.1.285\n\n" +
		"To update, run:\n  brew upgrade claude-code\n\n" +
		"Tip: For more frequent updates, use the claude-code@latest cask:\n" +
		"  brew uninstall --cask claude-code && brew install --cask claude-code@latest\n"

	brewLaggingReason = "這台的 Claude Code 由 Homebrew 的一般版管理，目前最新只到 2.1.285，低於收通知需要的 2.1.287；" +
		"請在這台機器改裝 latest 版：`brew uninstall --cask claude-code && brew install --cask claude-code@latest`"
	brewFallbackReason = "這台的 Claude Code 由 Homebrew 管理，調度台無法直接升級；請在這台機器用 Homebrew 升級"
)

func TestUpgradeRelayUnderHomebrew(t *testing.T) {
	cases := []struct {
		name, version, out, rc, want string
	}{
		{"an up-to-date latest cask at or above the notify minimum says it is Homebrew's newest",
			"2.1.288", brewLatestUpToDate, "0", "這台的 Claude Code 由 Homebrew 管理，已是 Homebrew 上的最新版（2.1.288）"},
		{"an up-to-date stable cask below the notify minimum says to switch to the latest cask, echoing the tip",
			"2.1.285", brewStableUpToDate, "0", brewLaggingReason},
		{"a stable cask whose available update is still below the notify minimum says to switch to the latest cask",
			"2.1.280", brewStableBehind, "0", brewLaggingReason},
		{"an available update at or above the notify minimum names the brew upgrade command and the target, under color codes, CRLF and ->",
			"2.1.280", "\x1b[1mClaude is managed by Homebrew.\x1b[0m\r\nUpdate available: 2.1.280 -> 2.1.290\r\n" +
				"To update, run:\r\n  \x1b[36mbrew upgrade claude-code@latest\x1b[0m\r\n", "0",
			"這台的 Claude Code 由 Homebrew 管理，調度台無法直接升級；請在這台機器執行 `brew upgrade claude-code@latest`（可升到 2.1.290）"},
		{"Homebrew output of no known shape falls back to the generic Homebrew reason",
			"2.1.280", "Claude is managed by Homebrew.\nSomething new happened.\n", "0", brewFallbackReason},
		{"output that does not mention Homebrew keeps the generic unchanged reason",
			"2.1.200", "Claude is up to date!\nUpdate available: 2.1.200 → 2.1.290\n", "0", upgradeUnchangedReason},
		{"a non-zero exit still reports the exit and the last line, even under Homebrew",
			"2.1.280", brewStableBehind, "1",
			"claude update 執行失敗（exit status 1）：brew uninstall --cask claude-code && brew install --cask claude-code@latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newUpgradeHarness(t)
			h.fake.write(t, h.fake.version, tc.version)
			h.fake.write(t, h.fake.out, tc.out)
			h.fake.write(t, h.fake.rc, tc.rc)
			h.relay.Start("ru-h", "claude")
			h.next(t)
			if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-h", State: "failed",
				FromVersion: tc.version, ToVersion: tc.version, Reason: tc.want}); got != want {
				t.Fatalf("final report = %+v, want %+v", got, want)
			}
		})
	}

	t.Run("under output far longer than the kept tail, with a line past the scanner's limit, the Homebrew notice at the end is still read and the update is not stalled", func(t *testing.T) {
		h := newUpgradeHarness(t)
		h.fake.write(t, h.fake.version, "2.1.285")
		noise := strings.Repeat("progress line\n", 5000) + strings.Repeat("x", 200000) + "\n"
		h.fake.write(t, h.fake.out, noise+brewStableUpToDate)
		h.relay.Start("ru-big", "claude")
		h.next(t)
		if got, want := h.next(t), (upgradeReport{UpgradeID: "ru-big", State: "failed",
			FromVersion: "2.1.285", ToVersion: "2.1.285", Reason: brewLaggingReason}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})
}

func TestHomebrewUnchangedReason(t *testing.T) {
	t.Run("without the Homebrew line it does not answer", func(t *testing.T) {
		if reason, ok := homebrewUnchangedReason([]string{"Claude is up to date!"}, "2.1.288"); ok {
			t.Fatalf("answered %q for output that never mentions Homebrew", reason)
		}
	})
	t.Run("an unparseable current version under an up-to-date notice falls back", func(t *testing.T) {
		if reason, _ := homebrewUnchangedReason([]string{"Claude is managed by Homebrew.", "Claude is up to date!"}, "weird"); reason != brewFallbackReason {
			t.Fatalf("reason = %q, want the fallback", reason)
		}
	})
	t.Run("without the tip line the lagging reason uses the default latest-cask command", func(t *testing.T) {
		got, _ := homebrewUnchangedReason([]string{"Claude is managed by Homebrew.", "Claude is up to date!"}, "2.1.285")
		if got != brewLaggingReason {
			t.Fatalf("reason = %q, want %q", got, brewLaggingReason)
		}
	})
	t.Run("the lagging reason echoes the tip's command when Homebrew gives one", func(t *testing.T) {
		tip := "brew uninstall --cask claude-code@beta && brew install --cask claude-code@latest"
		got, _ := homebrewUnchangedReason([]string{"Claude is managed by Homebrew.", "Claude is up to date!", tip}, "2.1.285")
		if want := strings.Replace(brewLaggingReason, brewDefaultLatestCmd, tip, 1); got != want {
			t.Fatalf("reason = %q, want %q", got, want)
		}
	})
	t.Run("every reason fits the reason cap", func(t *testing.T) {
		for _, out := range []string{brewLatestUpToDate, brewStableUpToDate, brewStableBehind} {
			got, _ := homebrewUnchangedReason(strings.Split(out, "\n"), "2.1.288")
			if n := len([]rune(got)); n > upgradeReasonMaxRunes {
				t.Errorf("reason is %d runes, over the %d cap: %q", n, upgradeReasonMaxRunes, got)
			}
		}
	})
}
