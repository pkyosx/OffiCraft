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

// A claude member hears OffiCraft events through one of two routes, picked per
// spawn:
//   - the notification mod (mod/, a Claude Code plugin of function hooks) runs
//     `ocagent listen --deliver-mod` as its child and submits each event as a
//     prompt of the member's MAIN conversation, whatever view the pane shows;
//   - the paste route: a `listen-<id>` tmux session runs `ocagent listen
//     --deliver-tmux`, which pastes into the member's pane. Text pasted while the
//     pane shows a sub-agent goes to that sub-agent, so this is only the fallback
//     for a Claude Code too old for mods, or one that did not load it.

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
	notifyModAckFile        = ".officraft-listen-ack"

	// Read by the listener from its environment (cli/ocagent/listen.go's
	// listenAckFileEnv); bin/listen-notice-mirror-guard.py holds the two equal.
	listenAckFileEnv = "OC_LISTEN_ACK_FILE"

	notifyModMinClaudeVersion = "2.1.287"
)

type notifyModListener struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
}

type notifyModConfig struct {
	// Submitted by the mod BEFORE it starts the listener, so the boot is the
	// member's first turn and a backlog the listener prints queues behind it.
	BootPrompt string `json:"boot_prompt"`
	// Written first thing in the mod's session.start, before any other check:
	// after a fallback, its presence and mtime tell a late session.start from a
	// mod that never ran (logged by logNotifyModFallback).
	StartedMarker  string `json:"started_marker"`
	LoadedMarker   string `json:"loaded_marker"`
	DisabledMarker string `json:"disabled_marker"`
	// Written once the boot prompt went in, so a fallback does not paste a second one.
	BootedMarker string `json:"booted_marker"`
	AckFile      string `json:"ack_file"`
	// The mod writes the load marker only once the listener printed a frame or a
	// line starting with one of these: a listener that refused to start prints
	// neither, and the warden then falls back to pasting.
	ReadyPrefixes []string          `json:"ready_prefixes"`
	Listener      notifyModListener `json:"listener"`
}

func buildNotifyModConfig(workdir, bootPrompt string) string {
	ackFile := filepath.Join(workdir, notifyModAckFile)
	b, _ := json.Marshal(notifyModConfig{
		BootPrompt:     bootPrompt,
		StartedMarker:  filepath.Join(workdir, notifyModStartedMarker),
		LoadedMarker:   filepath.Join(workdir, notifyModLoadedMarker),
		DisabledMarker: filepath.Join(workdir, notifyModDisabledMarker),
		BootedMarker:   filepath.Join(workdir, notifyModBootedMarker),
		AckFile:        ackFile,
		ReadyPrefixes:  []string{noticeConnectedPrefix, noticeDisconnectedPrefix},
		Listener: notifyModListener{
			Argv: []string{filepath.Join(workdir, "ocagent"), "listen", "--deliver-mod"},
			Cwd:  workdir,
			Env:  map[string]string{listenAckEnv: "1", listenAckFileEnv: ackFile},
		},
	})
	return string(b) + "\n"
}

var notifyModFiles = []string{".claude-plugin/plugin.json", "hooks/hooks.json", "hooks/register.ts"}

// Its run time is part of the spawn budget listed at receiptDeadlineSecs
// (server/ocserverd/receipt_watch.go).
const claudeVersionProbeBudget = 2 * time.Second

// Owner-facing advisories on an OK spawn, folded into 最近操作 (command.go).
func notifyLegacyPasteNote(found string) string {
	return "notify_legacy_paste: 這台機器的 Claude Code 是 " + found + "，比通知模組需要的 " +
		notifyModMinClaudeVersion + " 舊，這位成員的通知改用貼進 tmux 視窗的舊方式送達；" +
		"有人把視窗切到子代理（sub-agent）畫面時，貼進去的通知會送錯地方而漏掉。" +
		"請到調度台升級這台機器的 Claude Code。"
}

const notifyModNotLoadedNote = "notify_mod_not_loaded: OffiCraft 的通知模組（Claude Code mod）這次沒有載入，" +
	"常見原因：工作目錄沒有被信任、設定了 disableAllHooks、以 --safe-mode 啟動，" +
	"或受管設定（managed settings）擋掉了 --plugin-dir。這位成員的通知改用貼進 tmux 視窗的舊方式送達；" +
	"有人把視窗切到子代理（sub-agent）畫面時，通知可能漏掉。"

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
// the spawn's route choice and the heartbeat's below_notify_minimum both read it.
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
// disabled one would keep a mod that does load from starting its listener, a
// booted one would keep a fallback from pasting the boot prompt nobody submitted,
// and a started one would date a fallback's diagnosis to the previous session.
func (d SpawnDeps) installNotifyMod(workdir, bootPrompt string) string {
	for _, marker := range []string{notifyModStartedMarker, notifyModLoadedMarker, notifyModDisabledMarker, notifyModBootedMarker} {
		if err := d.Remove(filepath.Join(workdir, marker)); err != nil && !os.IsNotExist(err) {
			return fmt.Sprintf("write_file_failed: clearing stale %s: %v", marker, err)
		}
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

func (d SpawnDeps) notifyModLoaded(workdir string) bool {
	return d.Exists != nil && d.Exists(filepath.Join(workdir, notifyModLoadedMarker))
}

func (d SpawnDeps) notifyModBooted(workdir string) bool {
	return d.Exists != nil && d.Exists(filepath.Join(workdir, notifyModBootedMarker))
}

// ⚠️ A mod that loads AFTER this fallback would otherwise start a second
// listener beside the paste one; the mod checks this file first.
func (d SpawnDeps) disableNotifyMod(workdir string) {
	marker := filepath.Join(workdir, notifyModDisabledMarker)
	if err := d.WriteFile(marker, "the warden fell back to the tmux paste listener\n", 0o600); err != nil {
		d.logf("could not write %s (%v); a late-loading mod would start a second listener", marker, err)
	}
}

// Claude Code's own banner (external text, not ours): when its startup sync of the
// org's plugins changes them, it holds EVERY plugin, this mod included, until
// /reload-plugins. If Claude Code rewords it, the match fails and the 30 s wait
// ends in the paste fallback, as before.
const claudePluginsChangedBanner = "Run /reload-plugins to activate"

// One poll every notifyModPollTicks × nudgeSettle.
const notifyModPollTicks = 2

// Paced like the nudge loop it replaces on the mod route and bounded by the same
// 30 s (receiptDeadlineSecs): at most nudgeMaxAttempts sleeps and no poll starts
// past the deadline, so one capture (notifyModCaptureBudget + the runner's
// subprocessWaitDelay) is the most it can run over. It returns as soon as the mod
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
		// A failed capture just misses this poll; the fallback logs its own.
		out, err := withRunTimeout(d.Runner, notifyModCaptureBudget).Run("tmux", "-L", socket, "capture-pane", "-p", "-t", session)
		if err != nil || !strings.Contains(out, claudePluginsChangedBanner) {
			continue
		}
		// ASCII, so send-keys -l is safe here (the nudge's multibyte caveat does not
		// apply); copy-mode -q first, as the nudge does, or the keys are swallowed.
		_, _ = d.Runner.Run("tmux", "-L", socket, "copy-mode", "-q", "-t", session)
		_, _ = d.Runner.Run("tmux", "-L", socket, "send-keys", "-t", session, "-l", "/reload-plugins")
		_, _ = d.Runner.Run("tmux", "-L", socket, "send-keys", "-t", session, "Enter")
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

// Diagnostics for the warden log only, never the owner-facing Note: the pane is
// whatever the member's screen held, and the owner's 最近操作 is no place for it.
const (
	notifyModFallbackLogPrefix = "notify-mod-fallback"
	notifyModFallbackPaneLines = 40
	// The pane is arbitrary text logged as-is; the cap keeps one fallback from
	// flooding the warden log (a 160-column pane runs to ~8 KiB in 50 rows).
	notifyModFallbackPaneCap = 4096
	notifyModCaptureBudget   = 2 * time.Second
)

// Best effort: every failure is a log line, and nothing here changes the spawn.
// It tells "session.start fired late" (started marker, dated) from "the mod never
// ran" (no marker), and shows what the member's pane showed.
func (d SpawnDeps) logNotifyModFallback(memberID, workdir, socket, session string, launchedAt time.Time) {
	marker := filepath.Join(workdir, notifyModStartedMarker)
	switch mtime, err := d.modTime(marker); {
	case err == nil:
		d.logf("%s: %s: %s written %s after launch", memberID, notifyModFallbackLogPrefix,
			notifyModStartedMarker, mtime.Sub(launchedAt).Round(100*time.Millisecond))
	case os.IsNotExist(err):
		d.logf("%s: %s: %s absent: the mod's session.start never ran with its config", memberID,
			notifyModFallbackLogPrefix, notifyModStartedMarker)
	default:
		d.logf("%s: %s: %s unreadable: %v", memberID, notifyModFallbackLogPrefix, notifyModStartedMarker, err)
	}

	out, err := withRunTimeout(d.Runner, notifyModCaptureBudget).Run("tmux", "-L", socket, "capture-pane", "-p", "-t", session)
	if err != nil {
		d.logf("%s: %s: capture-pane of %s failed: %v", memberID, notifyModFallbackLogPrefix, session, err)
		return
	}
	lines := strings.Split(strings.TrimRight(out, " \n"), "\n")
	if len(lines) > notifyModFallbackPaneLines {
		lines = lines[len(lines)-notifyModFallbackPaneLines:]
	}
	pane, cut := tailBytes(strings.Join(lines, "\n"), notifyModFallbackPaneCap)
	note := ""
	if cut {
		note = fmt.Sprintf(", cut to its last %d bytes", notifyModFallbackPaneCap)
	}
	d.logf("%s: %s: pane %s, last %d lines%s:", memberID, notifyModFallbackLogPrefix, session, notifyModFallbackPaneLines, note)
	for _, line := range strings.Split(pane, "\n") {
		d.logf("%s: %s pane| %s", memberID, notifyModFallbackLogPrefix, line)
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
