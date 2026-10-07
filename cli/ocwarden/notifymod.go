package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A claude member hears OffiCraft events through the notification mod (mod/, a
// Claude Code plugin of function hooks): it submits the boot prompt, then runs
// `ocagent listen --deliver-socket` as its child, which writes each event into
// the session's own messaging socket. There is no other route: a Claude Code too
// old for mods, or one that did not load the mod, fails the start.

//go:embed mod/.claude-plugin/plugin.json mod/hooks/hooks.json mod/hooks/register.ts
var notifyModFS embed.FS

// The mod knows none of these names: it reads them from notifyModConfigFile,
// which installNotifyMod writes from here. Only that file's own name is spelled
// again, in mod/hooks/register.ts.
const (
	notifyModDirName        = ".officraft-mod"
	notifyModConfigFile     = "officraft.json"
	notifyModStartedMarker  = ".officraft-mod-started"
	notifyModLoadedMarker   = ".officraft-mod-loaded"
	notifyModDisabledMarker = ".officraft-mod-disabled"
	notifyModBootedMarker   = ".officraft-mod-booted"

	notifyModMinClaudeVersion = "2.1.287"
)

type notifyModListener struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

type notifyModConfig struct {
	// Submitted by the mod BEFORE it starts the listener, so the boot is the
	// member's first turn and a backlog the listener prints queues behind it.
	BootPrompt string `json:"boot_prompt"`
	// Written first thing in the mod's session.start, before any other check:
	// when the mod did not load, its presence and mtime tell a late session.start
	// from a mod that never ran (logged by logNotifyModNotLoaded).
	StartedMarker  string `json:"started_marker"`
	LoadedMarker   string `json:"loaded_marker"`
	DisabledMarker string `json:"disabled_marker"`
	// Written once the boot prompt went in, so a mod that booted the member is
	// not restarted.
	BootedMarker string `json:"booted_marker"`
	// The mod writes the load marker only once the listener printed a stderr line
	// starting with one of these: a listener that refused to start prints none,
	// and the start then fails.
	ReadyPrefixes []string          `json:"ready_prefixes"`
	Listener      notifyModListener `json:"listener"`
}

func buildNotifyModConfig(workdir, bootPrompt string) string {
	b, _ := json.Marshal(notifyModConfig{
		BootPrompt:     bootPrompt,
		StartedMarker:  filepath.Join(workdir, notifyModStartedMarker),
		LoadedMarker:   filepath.Join(workdir, notifyModLoadedMarker),
		DisabledMarker: filepath.Join(workdir, notifyModDisabledMarker),
		BootedMarker:   filepath.Join(workdir, notifyModBootedMarker),
		ReadyPrefixes:  []string{noticeConnectedPrefix, noticeDisconnectedPrefix},
		Listener: notifyModListener{
			Argv: []string{filepath.Join(workdir, "ocagent"), "listen", "--deliver-socket"},
			Cwd:  workdir,
		},
	})
	return string(b) + "\n"
}

var notifyModFiles = []string{".claude-plugin/plugin.json", "hooks/hooks.json", "hooks/register.ts"}

// Its run time is part of the spawn budget listed at startReceiptDeadlineSecs
// (server/ocserverd/receipt_watch.go).
const claudeVersionProbeBudget = 2 * time.Second

// Owner-facing refusals, folded into 最近操作 (command.go). The station keeps
// only the first commandResultReasonMax bytes (server/ocserverd/api_monitoring.go),
// so what the owner must act on comes first and the details stay in the warden log.
func notifyClaudeTooOldReason(found string) string {
	return "notify_claude_too_old: 這台機器的 Claude Code 是 " + found + "，低於 " + notifyModMinClaudeVersion +
		"，Claude 成員無法上線。請到監控頁的機器分頁升級這台機器的 Claude Code。"
}

// attempts holds the flag read before each launch; two means the restart ran.
func notifyModNotLoadedReason(attempts []hooksModulesFlag) string {
	s := "notify_mod_not_loaded: 通知模組沒有載入，成員收不到 OffiCraft 訊息，已停止上線。"
	if len(attempts) == notifyModAttempts {
		s += "已自動重啟 Claude Code 一次仍沒載入；啟動前快取的開關：第 1 次 " +
			noteFlagValue(attempts[0]) + "、第 2 次 " + noteFlagValue(attempts[1]) + "。"
	}
	return s + "常見原因：工作目錄未信任、disableAllHooks、--safe-mode、受管設定擋掉 --plugin-dir。"
}

// The flag is a boolean; anything longer than "unreadable" is left to the log.
func noteFlagValue(f hooksModulesFlag) string {
	if len(f.Value) > len("unreadable") {
		return "非布林值"
	}
	return f.Value
}

// An unreadable version is not "too old": the load marker catches a Claude Code
// that cannot run the mod after all.
func (d SpawnDeps) claudeTooOldForNotifyMod() (found string, tooOld bool) {
	out, err := withRunTimeout(d.Runner, claudeVersionProbeBudget).Run(d.ClaudeBin, "--version")
	fields := strings.Fields(out)
	if err != nil || len(fields) == 0 {
		return "", false
	}
	below, known := claudeBelowNotifyMinimum(fields[0])
	if !known {
		return "", false
	}
	return fields[0], below
}

// claudeBelowNotifyMinimum is the one comparison with notifyModMinClaudeVersion:
// the spawn's refusal and the heartbeat's below_notify_minimum both read it.
func claudeBelowNotifyMinimum(version string) (below, known bool) {
	have, ok := parseDottedVersion(version)
	if !ok {
		return false, false
	}
	want, _ := parseDottedVersion(notifyModMinClaudeVersion)
	return compareCodexModelVersions(have, want) < 0, true
}

func parseDottedVersion(v string) ([]int, bool) {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// Rewritten on every spawn. The markers are cleared first: a loaded marker left by
// the previous session would pass the check below for a mod that never loaded, a
// disabled one would keep a mod that does load from booting, a started or booted
// one would keep a mod that never ran from its restart, and a started one would
// also date the not-loaded diagnosis to the previous session.
func (d SpawnDeps) installNotifyMod(workdir, bootPrompt string) string {
	if refusal := d.clearNotifyModMarkers(workdir); refusal != "" {
		return refusal
	}
	dir := filepath.Join(workdir, notifyModDirName)
	for _, name := range notifyModFiles {
		body, err := notifyModFS.ReadFile(path.Join("mod", name))
		if err != nil {
			return fmt.Sprintf("write_file_failed: notification mod %s is not in this warden: %v", name, err)
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := d.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return fmt.Sprintf("mkdir_failed: %s: %v", filepath.Dir(dest), err)
		}
		if err := d.WriteFile(dest, string(body), 0o600); err != nil {
			return fmt.Sprintf("write_file_failed: %s/%s: %v", notifyModDirName, name, err)
		}
	}
	if err := d.WriteFile(filepath.Join(dir, notifyModConfigFile), buildNotifyModConfig(workdir, bootPrompt), 0o600); err != nil {
		return fmt.Sprintf("write_file_failed: %s/%s: %v", notifyModDirName, notifyModConfigFile, err)
	}
	return ""
}

func (d SpawnDeps) clearNotifyModMarkers(workdir string) string {
	for _, marker := range []string{notifyModStartedMarker, notifyModLoadedMarker, notifyModDisabledMarker, notifyModBootedMarker} {
		if err := d.Remove(filepath.Join(workdir, marker)); err != nil && !os.IsNotExist(err) {
			return fmt.Sprintf("write_file_failed: clearing stale %s: %v", marker, err)
		}
	}
	return ""
}

func (d SpawnDeps) notifyModLoaded(workdir string) bool {
	return d.Exists != nil && d.Exists(filepath.Join(workdir, notifyModLoadedMarker))
}

func (d SpawnDeps) notifyModBooted(workdir string) bool {
	return d.Exists != nil && d.Exists(filepath.Join(workdir, notifyModBootedMarker))
}

// ⚠️ A mod that loads after the warden gave up on its session would otherwise
// boot the member and start a listener, which marks messages read in a session
// about to be killed; the mod checks this file first.
func (d SpawnDeps) disableNotifyMod(workdir string) {
	marker := filepath.Join(workdir, notifyModDisabledMarker)
	if err := d.WriteFile(marker, "the warden gave up on this session\n", 0o600); err != nil {
		d.logf("could not write %s (%v); a late-loading mod could still boot the member", marker, err)
	}
}

// Without the mod there is no listener, so the member could neither hear nor
// come online: it is stopped and the start fails.
func (d SpawnDeps) giveUpNotifyMod(memberID, workdir, socket, session string, launchedAt time.Time,
	attempts []hooksModulesFlag, disabled bool) string {
	if !disabled {
		d.disableNotifyMod(workdir)
	}
	d.logf("%s: the notification mod did not load; stopping the member", memberID)
	d.logHooksModulesFlag(memberID, "at give-up")
	// Before the teardown, so the capture shows what the wait left on screen.
	d.logNotifyModNotLoaded(memberID, workdir, socket, session, launchedAt)
	if !d.stopNotifyModAttempt(socket, session, workdir) {
		d.logf("%s: notify-mod: the member could not be torn down after the mod did not load", memberID)
	}
	return notifyModNotLoadedReason(attempts)
}

func (d SpawnDeps) stopNotifyModAttempt(socket, session, workdir string) bool {
	if d.StopAttempt == nil {
		return killSession(d.Runner, socket, session)
	}
	return d.StopAttempt(socket, session, workdir)
}

// Claude Code's own banner (external text, not ours): when its startup sync of the
// org's plugins changes them, it holds EVERY plugin, this mod included, until
// /reload-plugins. If Claude Code rewords it, the match fails and the 30 s wait
// ends in the one restart, then a failed start.
const claudePluginsChangedBanner = "Run /reload-plugins to activate"

// One poll every notifyModPollTicks × nudgeSettle.
const notifyModPollTicks = 2

// Bounded by 30 s (startReceiptDeadlineSecs): at most nudgeMaxAttempts sleeps and no poll starts
// past the deadline. Every tmux call here runs under notifyModCaptureBudget, so
// each costs at most 2 s + the runner's subprocessWaitDelay 2 s; the most the
// last poll can run over is its nudgeSettle sleep, one capture and the one-time
// /reload-plugins send (3 calls): 1 + 4 + 12 = 17 s. It returns as soon as the mod
// is loaded. While the mod has not even started, each poll looks at the pane for
// claudePluginsChangedBanner and answers it with /reload-plugins, once.
func (d SpawnDeps) waitForNotifyMod(memberID, workdir, socket, session string) {
	sleep := nudgeClock(d.Sleep)
	deadline := d.now().Add(nudgeMaxAttempts * nudgeSettle)
	reloadSent := false
	for tick := 1; tick <= nudgeMaxAttempts && d.now().Before(deadline); tick++ {
		sleep(nudgeSettle)
		if tick%notifyModPollTicks != 0 {
			continue
		}
		if d.notifyModLoaded(workdir) {
			return
		}
		if reloadSent || d.notifyModStarted(workdir) {
			continue
		}
		// A failed capture just misses this poll; the give-up logs its own.
		out, err := withRunTimeout(d.Runner, notifyModCaptureBudget).Run("tmux", "-L", socket, "capture-pane", "-p", "-t", session)
		if err != nil || !strings.Contains(out, claudePluginsChangedBanner) {
			continue
		}
		// ASCII, so send-keys -l is safe here (the nudge's multibyte caveat does not
		// apply); copy-mode -q first, as the nudge does, or the keys are swallowed.
		// ⚠️ Text an attached person has typed into the prompt box is submitted
		// together with /reload-plugins, and a startup dialog drawn over the prompt
		// takes the Enter instead.
		r := withRunTimeout(d.Runner, notifyModCaptureBudget)
		_, _ = r.Run("tmux", "-L", socket, "copy-mode", "-q", "-t", session)
		_, _ = r.Run("tmux", "-L", socket, "send-keys", "-t", session, "-l", "/reload-plugins")
		_, _ = r.Run("tmux", "-L", socket, "send-keys", "-t", session, "Enter")
		reloadSent = true
		d.logf("%s: notify-mod: plugins changed during startup; sent /reload-plugins", memberID)
	}
}

func (d SpawnDeps) notifyModStarted(workdir string) bool {
	return d.Exists != nil && d.Exists(filepath.Join(workdir, notifyModStartedMarker))
}

// A mod's listener left by a session whose Claude Code died without taking its
// child along would hold this member's identity beside the new one. Best effort,
// like stop(): a failure is logged and the spawn goes on.
func (d SpawnDeps) reapWorkdirListeners(memberID, workdir string) {
	if d.ReapWorkdirListeners == nil {
		return
	}
	found, cleared := d.ReapWorkdirListeners(workdir)
	switch {
	case found > 0 && cleared:
		d.logf("%s: reaped %d leftover ocagent process(es) in %s", memberID, found, workdir)
	case found > 0:
		d.logf("%s: %d leftover ocagent process(es) in %s did not exit; spawning anyway", memberID, found, workdir)
	}
}

// Diagnostics for the warden log only, never the owner-facing reason: the pane
// is whatever the member's screen held, and the owner's 最近操作 is no place for it.
const (
	notifyModNotLoadedLogPrefix = "notify-mod-not-loaded"
	notifyModNotLoadedPaneLines = 40
	// The pane is arbitrary text logged as-is; the cap keeps one failed start from
	// flooding the warden log (a 160-column pane runs to ~8 KiB in 50 rows).
	notifyModNotLoadedPaneCap = 4096
	notifyModCaptureBudget    = 2 * time.Second
)

// Best effort: every failure is a log line, and nothing here changes the spawn.
// It tells "session.start fired late" (started marker, dated) from "the mod never
// ran" (no marker), and shows what the member's pane showed.
func (d SpawnDeps) logNotifyModNotLoaded(memberID, workdir, socket, session string, launchedAt time.Time) {
	marker := filepath.Join(workdir, notifyModStartedMarker)
	switch mtime, err := d.modTime(marker); {
	case err == nil:
		d.logf("%s: %s: %s written %s after launch", memberID, notifyModNotLoadedLogPrefix,
			notifyModStartedMarker, mtime.Sub(launchedAt).Round(100*time.Millisecond))
	case os.IsNotExist(err):
		d.logf("%s: %s: %s absent: the mod's session.start never ran with its config", memberID,
			notifyModNotLoadedLogPrefix, notifyModStartedMarker)
	default:
		d.logf("%s: %s: %s unreadable: %v", memberID, notifyModNotLoadedLogPrefix, notifyModStartedMarker, err)
	}

	out, err := withRunTimeout(d.Runner, notifyModCaptureBudget).Run("tmux", "-L", socket, "capture-pane", "-p", "-t", session)
	if err != nil {
		d.logf("%s: %s: capture-pane of %s failed: %v", memberID, notifyModNotLoadedLogPrefix, session, err)
		return
	}
	lines := strings.Split(strings.TrimRight(out, " \n"), "\n")
	if len(lines) > notifyModNotLoadedPaneLines {
		lines = lines[len(lines)-notifyModNotLoadedPaneLines:]
	}
	pane, cut := tailBytes(strings.Join(lines, "\n"), notifyModNotLoadedPaneCap)
	note := ""
	if cut {
		note = fmt.Sprintf(", cut to its last %d bytes", notifyModNotLoadedPaneCap)
	}
	d.logf("%s: %s: pane %s, last %d lines%s:", memberID, notifyModNotLoadedLogPrefix, session, notifyModNotLoadedPaneLines, note)
	for _, line := range strings.Split(pane, "\n") {
		d.logf("%s: %s pane| %s", memberID, notifyModNotLoadedLogPrefix, line)
	}
}

// tailBytes keeps at most max bytes from the end of s, starting on a rune.
func tailBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:], true
}

func (d SpawnDeps) modTime(path string) (time.Time, error) {
	if d.ModTime == nil {
		return time.Time{}, errors.New("no ModTime seam")
	}
	return d.ModTime(path)
}

func (d SpawnDeps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}
