package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The runtime-upgrade relay: the owner starts a Claude Code upgrade from the
// cockpit, the server sends runtime_upgrade, and this file runs `claude update`
// with the claude binary and environment members launch with, reading
// `claude --version` before and after.

const (
	rpcRuntimeUpgrade = "runtime_upgrade"

	runtimeUpgradePath = "/api/monitoring/runtime-upgrade"

	// The server's idle cap (20 minutes, server/ocserverd/runtime_upgrade.go)
	// must stay above this, so a stuck update is reported failed by the warden
	// before the server expires it.
	upgradeProcessCap = 10 * time.Minute

	upgradeReasonMaxRunes = 300

	// upgradeTailLines bounds what is kept of `claude update`'s output: enough
	// for Homebrew's whole notice, whatever else the update printed before it.
	upgradeTailLines     = 64
	upgradeTailLineRunes = 512
)

func refuseRealUpgradeInTest(bin string) {
	refuseRealPathInTest("upgrade", bin)
}

func startUpgradeProcess(shell, script, bin string) (*loginProc, error) {
	refuseRealUpgradeInTest(bin)
	return startGroupProcess(shell, script)
}

type UpgradeSeam interface {
	Start(upgradeID, runtime string)
	Abandon()
}

type upgradeReport struct {
	UpgradeID   string
	State       string
	FromVersion string
	ToVersion   string
	Reason      string
}

// upgradeReporter answers the state the server now holds ("" when the POST did
// not land).
type upgradeReporter func(upgradeReport) string

type upgradeRun struct {
	id        string
	proc      *loginProc
	done      chan struct{}
	abandoned bool
	timedOut  bool
}

type upgradeRelay struct {
	prober     *loginProber
	progress   upgradeReporter
	start      loginStarter
	resolveBin func() string
	remove     func(string) error
	cap        time.Duration
	// afterUpgrade runs once `claude update` has ended, however it ended, so
	// the next heartbeat reads the version again instead of the cached one.
	afterUpgrade func()
	logf         func(string, ...any)

	mu      sync.Mutex
	current *upgradeRun
}

func newUpgradeRelay(prober *loginProber, progress upgradeReporter, afterUpgrade func(),
	logf func(string, ...any)) *upgradeRelay {
	return &upgradeRelay{
		prober:       prober,
		progress:     progress,
		start:        startUpgradeProcess,
		resolveBin:   func() string { return resolveClaudeBin(prober.env) },
		remove:       prober.remove,
		cap:          upgradeProcessCap,
		afterUpgrade: afterUpgrade,
		logf:         logf,
	}
}

func (r *upgradeRelay) log(format string, args ...any) {
	if r.logf != nil {
		r.logf("[ocwarden upgrade] "+format, args...)
	}
}

func (r *upgradeRelay) fail(rep upgradeReport, reason string) {
	rep.State, rep.Reason = "failed", reason
	r.log("%s: failed", rep.UpgradeID)
	r.progress(rep)
}

// Start returns at once: the update runs on its own goroutine, never on the
// command reader's.
func (r *upgradeRelay) Start(upgradeID, runtime string) {
	if runtime != "claude" {
		r.fail(upgradeReport{UpgradeID: upgradeID}, fmt.Sprintf("這個 warden 只能升級 Claude Code，不能升級 %q", runtime))
		return
	}
	r.mu.Lock()
	if cur := r.current; cur != nil {
		r.mu.Unlock()
		if cur.id != upgradeID {
			r.fail(upgradeReport{UpgradeID: upgradeID}, "這台機器已經有另一個 Claude Code 升級在進行，請等它結束再試")
		}
		return
	}
	run := &upgradeRun{id: upgradeID, done: make(chan struct{})}
	r.current = run
	r.mu.Unlock()
	go r.run(run)
}

func (r *upgradeRelay) run(run *upgradeRun) {
	defer func() {
		r.mu.Lock()
		r.current = nil
		r.mu.Unlock()
		close(run.done)
	}()
	rep := upgradeReport{UpgradeID: run.id}
	bin := r.resolveBin()
	if bin == "" {
		r.fail(rep, "這台機器沒有安裝 Claude Code")
		return
	}
	r.prober.prepareLaunchEnv()
	from, err := r.claudeVersion(bin, run.id)
	if err != nil {
		r.fail(rep, "讀不到升級前的 Claude Code 版本（claude --version："+err.Error()+"），沒有執行升級")
		return
	}
	if r.wasAbandoned(run) {
		return
	}
	rep.FromVersion = from
	rep.State = "running"
	r.log("%s: running claude update from %s", run.id, from)
	switch r.progress(rep) {
	case "failed", "expired", "succeeded":
		r.log("%s: the server already ended this upgrade; not running it", run.id)
		return
	}

	procReason, output := r.runUpdate(run, bin)
	if r.wasAbandoned(run) {
		return
	}
	if r.afterUpgrade != nil {
		r.afterUpgrade()
	}
	to, verr := r.claudeVersion(bin, run.id)
	if verr == nil {
		rep.ToVersion = to
	}
	switch {
	case procReason != "":
		r.fail(rep, procReason)
	case verr != nil:
		r.fail(rep, "claude update 結束了，但讀不到升級後的 Claude Code 版本（claude --version："+verr.Error()+"）")
	default:
		r.conclude(rep, output)
	}
}

func (r *upgradeRelay) conclude(rep upgradeReport, output []string) {
	have, okTo := parseDottedVersion(rep.ToVersion)
	had, okFrom := parseDottedVersion(rep.FromVersion)
	switch {
	case !okTo || !okFrom:
		r.fail(rep, fmt.Sprintf("看不懂 Claude Code 回報的版本（升級前 %s、升級後 %s），無法判斷有沒有升級", rep.FromVersion, rep.ToVersion))
	case compareCodexModelVersions(have, had) > 0:
		rep.State = "succeeded"
		r.log("%s: succeeded %s -> %s", rep.UpgradeID, rep.FromVersion, rep.ToVersion)
		r.progress(rep)
	case compareCodexModelVersions(have, had) == 0:
		if reason, ok := homebrewUnchangedReason(output, rep.ToVersion); ok {
			r.fail(rep, reason)
			return
		}
		r.fail(rep, "版本沒有變（仍是 "+rep.ToVersion+"）：可能已經是最新版，"+
			"或 claude update 升級的不是成員使用的那一份 Claude Code")
	default:
		r.fail(rep, "升級後的版本 "+rep.ToVersion+" 比升級前的 "+rep.FromVersion+" 舊")
	}
}

// runUpdate answers "" when `claude update` exited 0, else why it did not,
// and the last upgradeTailLines non-blank lines it printed on either stream.
func (r *upgradeRelay) runUpdate(run *upgradeRun, bin string) (string, []string) {
	script, rendered := r.prober.claudeCommand(bin, loginRenderPrefix+run.id, "update",
		claudeCommandOpts{selfDeleteRender: true})
	proc, err := r.start(r.prober.shell(), script, bin)
	if err != nil {
		if rendered != "" {
			_ = r.remove(rendered)
		}
		return "claude update 無法啟動：" + err.Error(), nil
	}
	// Nobody can answer a prompt; a closed stdin makes one end instead of wait.
	_ = proc.stdin.Close()
	r.mu.Lock()
	run.proc = proc
	abandoned := run.abandoned
	r.mu.Unlock()
	if abandoned {
		proc.kill()
	}

	var (
		tailMu  sync.Mutex
		tail    []string
		readers sync.WaitGroup
	)
	read := func(stream io.Reader) {
		defer readers.Done()
		eachBoundedLine(stream, func(raw string) {
			if line := strings.TrimSpace(stripTerminalEscapes(raw)); line != "" {
				tailMu.Lock()
				if len(tail) == upgradeTailLines {
					tail = append(tail[:0], tail[1:]...)
				}
				tail = append(tail, clipRunes(line, upgradeTailLineRunes))
				tailMu.Unlock()
			}
		})
	}
	readers.Add(2)
	go read(proc.stdout)
	go read(proc.stderr)
	timer := time.AfterFunc(r.cap, func() {
		r.mu.Lock()
		run.timedOut = true
		r.mu.Unlock()
		proc.kill()
	})
	readers.Wait()
	waitErr := proc.wait()
	timer.Stop()
	if rendered != "" {
		_ = r.remove(rendered)
	}
	r.mu.Lock()
	timedOut := run.timedOut
	r.mu.Unlock()
	tailMu.Lock()
	output := append([]string(nil), tail...)
	tailMu.Unlock()
	switch {
	case timedOut:
		r.log("%s: claude update killed at its %s cap", run.id, r.cap)
		return fmt.Sprintf("claude update 超過 %s 沒有結束，已中止", r.cap), output
	case waitErr != nil:
		r.log("%s: claude update exited (%v)", run.id, waitErr)
		reason := "claude update 執行失敗（" + waitErr.Error() + "）"
		if len(output) > 0 {
			reason += "：" + clipRunes(output[len(output)-1], upgradeReasonMaxRunes)
		}
		return reason, output
	}
	r.log("%s: claude update exited 0", run.id)
	return "", output
}

// eachBoundedLine calls fn with every line of stream, keeping only the first
// 4 KiB of a longer one: unlike bufio.Scanner it never stops at a long line,
// which would leave the pipe undrained and the update blocked writing to it.
func eachBoundedLine(stream io.Reader, fn func(string)) {
	br := bufio.NewReaderSize(stream, 4096)
	for {
		chunk, err := br.ReadSlice('\n')
		line := string(chunk)
		for err == bufio.ErrBufferFull {
			_, err = br.ReadSlice('\n')
		}
		if line != "" {
			fn(strings.ToValidUTF8(strings.TrimSuffix(line, "\n"), ""))
		}
		if err != nil {
			return
		}
	}
}

var (
	brewManagedLine   = regexp.MustCompile(`(?i)managed by homebrew`)
	brewUpToDateLine  = regexp.MustCompile(`(?i)\bup to date\b`)
	brewAvailableLine = regexp.MustCompile(`(?i)update available:\s*v?[0-9][0-9.]*\s*(?:→|->|=>)\s*v?([0-9]+(?:\.[0-9]+){0,5})\b`)
	brewUpgradeCmd    = regexp.MustCompile(`^brew upgrade(?: --cask)? claude-code(?:@[a-z]+)?$`)
	brewLatestCmd     = regexp.MustCompile(`^brew uninstall --cask claude-code(?:@[a-z]+)? && brew install --cask claude-code@latest$`)
)

const (
	brewDefaultUpgradeCmd = "brew upgrade claude-code"
	brewDefaultLatestCmd  = "brew uninstall --cask claude-code && brew install --cask claude-code@latest"
)

// homebrewUnchangedReason explains an unchanged version when `claude update`
// said Homebrew manages this install: it then only prints what to run and
// exits 0 without upgrading anything, so the generic reason would mislead.
func homebrewUnchangedReason(output []string, current string) (string, bool) {
	managed, upToDate := false, false
	target, upgradeCmd, latestCmd := "", brewDefaultUpgradeCmd, brewDefaultLatestCmd
	for _, line := range output {
		switch {
		case brewManagedLine.MatchString(line):
			managed = true
		case brewUpToDateLine.MatchString(line):
			upToDate = true
		case brewUpgradeCmd.MatchString(line):
			upgradeCmd = line
		case brewLatestCmd.MatchString(line):
			latestCmd = line
		}
		if m := brewAvailableLine.FindStringSubmatch(line); m != nil {
			target = m[1]
		}
	}
	if !managed {
		return "", false
	}
	lagging := func(v string) string {
		return "這台的 Claude Code 由 Homebrew 的一般版管理，目前最新只到 " + v +
			"，低於收通知需要的 " + notifyModMinClaudeVersion + "；請在這台機器改裝 latest 版：`" + latestCmd + "`"
	}
	if target != "" {
		if below, known := claudeBelowNotifyMinimum(target); known {
			if below {
				return lagging(target), true
			}
			return "這台的 Claude Code 由 Homebrew 管理，調度台無法直接升級；請在這台機器執行 `" +
				upgradeCmd + "`（可升到 " + target + "）", true
		}
	}
	if upToDate && target == "" {
		if below, known := claudeBelowNotifyMinimum(current); known {
			if below {
				return lagging(current), true
			}
			return "這台的 Claude Code 由 Homebrew 管理，已是 Homebrew 上的最新版（" + current + "）", true
		}
	}
	return "這台的 Claude Code 由 Homebrew 管理，調度台無法直接升級；請在這台機器用 Homebrew 升級", true
}

func (r *upgradeRelay) wasAbandoned(run *upgradeRun) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return run.abandoned
}

// claudeVersion reads `claude --version` through the same launch prologue a
// member's claude runs under.
func (r *upgradeRelay) claudeVersion(bin, upgradeID string) (string, error) {
	if r.prober.keep == nil {
		return "", errors.New("這個 warden 沒有可以執行版本查詢的執行器")
	}
	script, rendered := r.prober.claudeCommand(bin, loginRenderPrefix+upgradeID, "--version", claudeCommandOpts{})
	out, err := r.prober.keep.RunKeepStdout(r.prober.shell(), "-c", script)
	if rendered != "" {
		_ = r.remove(rendered)
	}
	if err != nil {
		return "", errors.New(clipRunes(strings.TrimSpace(err.Error()), upgradeReasonMaxRunes))
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", errors.New("沒有輸出")
	}
	return fields[0], nil
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// Abandon kills a running `claude update` before the warden execs itself in
// place: the exec would orphan it and nobody would report its end.
func (r *upgradeRelay) Abandon() {
	r.mu.Lock()
	run := r.current
	var proc *loginProc
	if run != nil {
		run.abandoned = true
		proc = run.proc
	}
	r.mu.Unlock()
	if run == nil {
		return
	}
	if proc != nil {
		proc.kill()
	}
	r.progress(upgradeReport{UpgradeID: run.id, State: "failed",
		Reason: "warden 為了更新自己而重新啟動，升級被中止；請重新開始升級"})
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
	}
}

func newUpgradeReporter(cfg Config) upgradeReporter {
	post := httpPoster(&http.Client{Timeout: commandReportTimeout}, cfg.Base, cfg.Token)
	return func(rep upgradeReport) string {
		if cfg.Token == "" {
			return ""
		}
		_, body := post(runtimeUpgradePath, upgradeReportPayload(rep))
		state, _ := body["state"].(string)
		return state
	}
}

func upgradeReportPayload(rep upgradeReport) map[string]any {
	payload := map[string]any{"upgrade_id": rep.UpgradeID, "state": rep.State}
	if rep.FromVersion != "" {
		payload["from_version"] = rep.FromVersion
	}
	if rep.ToVersion != "" {
		payload["to_version"] = rep.ToVersion
	}
	if rep.Reason != "" {
		payload["reason"] = rep.Reason
	}
	return payload
}

var errUpgradeArgs = errors.New("command: runtime_upgrade frame missing a valid upgrade_id")

func dispatchUpgrade(cmd *Command, upgrade UpgradeSeam) error {
	if upgrade == nil {
		return fmt.Errorf("command: %s refused: upgrade seam not wired", cmd.RPC)
	}
	id, _ := argString(cmd.Args, "upgrade_id")
	if !loginIDPattern.MatchString(id) {
		return errUpgradeArgs
	}
	runtime, _ := argString(cmd.Args, "runtime")
	upgrade.Start(id, runtime)
	return nil
}
