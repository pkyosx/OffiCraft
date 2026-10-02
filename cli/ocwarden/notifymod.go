package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	notifyModLoadedMarker   = ".officraft-mod-loaded"
	notifyModDisabledMarker = ".officraft-mod-disabled"
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
	BootPrompt     string `json:"boot_prompt"`
	LoadedMarker   string `json:"loaded_marker"`
	DisabledMarker string `json:"disabled_marker"`
	AckFile        string `json:"ack_file"`
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
		LoadedMarker:   filepath.Join(workdir, notifyModLoadedMarker),
		DisabledMarker: filepath.Join(workdir, notifyModDisabledMarker),
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
	have, ok := parseDottedVersion(fields[0])
	if !ok {
		return "", false
	}
	want, _ := parseDottedVersion(notifyModMinClaudeVersion)
	return fields[0], compareCodexModelVersions(have, want) < 0
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

// Rewritten on every spawn. Both markers are cleared first: a loaded marker left
// by the previous session would pass the check below for a mod that never loaded,
// and a disabled one would keep a mod that does load from starting its listener.
func (d SpawnDeps) installNotifyMod(workdir, bootPrompt string) string {
	for _, marker := range []string{notifyModLoadedMarker, notifyModDisabledMarker} {
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

// ⚠️ A mod that loads AFTER this fallback would otherwise start a second
// listener beside the paste one; the mod checks this file first.
func (d SpawnDeps) disableNotifyMod(workdir string) {
	marker := filepath.Join(workdir, notifyModDisabledMarker)
	if err := d.WriteFile(marker, "the warden fell back to the tmux paste listener\n", 0o600); err != nil {
		d.logf("could not write %s (%v); a late-loading mod would start a second listener", marker, err)
	}
}

// Paced like the nudge loop it replaces on the mod route, so the spawn budget
// (receiptDeadlineSecs) is unchanged.
func waitForNotifyMod(sleep func(time.Duration)) {
	sleep = nudgeClock(sleep)
	for attempt := 0; attempt < nudgeMaxAttempts; attempt++ {
		sleep(nudgeSettle)
	}
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
