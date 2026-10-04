package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Claude Code decides whether to run a plugin's hook MODULES (our mod's
// hooks.json "modules") from its GrowthBook flag tengu_plugin_hooks_modules. At
// startup, before the remote value arrives, it reads the value cached in its
// global config, which every Claude Code of that user shares and which a
// long-running older session can rewrite to false. A member started on that
// stale false never runs the mod; the same start then fetches true and caches
// it, so a second start a moment later does run it. Hence ONE restart before
// the paste fallback (retryNotifyMod), and these names, logged read-only before
// each attempt and at the fallback. They are Claude Code's names, not ours: if
// it renames them the log reads "absent" and nothing else changes.
const (
	claudeHooksModulesFlag      = "tengu_plugin_hooks_modules"
	claudeGrowthBookFeaturesKey = "cachedGrowthBookFeatures"
	claudeGrowthBookCachedAtKey = "cachedGrowthBookFeaturesAt"

	notifyModAttempts = 2
	// A value is logged as-is, so it is capped like the pane capture is.
	hooksModulesFlagValueCap = 64
)

// hooksModulesFlag is what the shared config held for the flag: Value is the
// cached JSON value ("true", "false", …), "absent" or "unreadable"; Detail says
// why for the last two.
type hooksModulesFlag struct {
	Path     string
	Value    string
	CachedAt string
	Detail   string
}

func (f hooksModulesFlag) String() string {
	s := fmt.Sprintf("%s %s=%s", f.Path, claudeHooksModulesFlag, f.Value)
	if f.Detail != "" {
		s += " (" + f.Detail + ")"
	}
	if f.CachedAt != "" {
		s += fmt.Sprintf(" %s=%s", claudeGrowthBookCachedAtKey, f.CachedAt)
	}
	return s
}

// The config file Claude Code reads is the one the launch line pins: the purge
// in claudeChildEnvPrologue drops any CLAUDE_CONFIG_DIR the member's env carries
// and re-exports only ClaudeHome's, so ClaudeJSONPath is that file.
// READ-ONLY: this never writes the file.
func (d SpawnDeps) readHooksModulesFlag() hooksModulesFlag {
	f := hooksModulesFlag{Path: d.ClaudeHome.ClaudeJSONPath()}
	read := d.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	raw, err := read(f.Path)
	switch {
	case os.IsNotExist(err):
		f.Value, f.Detail = "absent", "no such file"
		return f
	case err != nil:
		f.Value, f.Detail = "unreadable", err.Error()
		return f
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		f.Value, f.Detail = "unreadable", "not a JSON object"
		return f
	}
	f.CachedAt = renderGrowthBookCachedAt(root[claudeGrowthBookCachedAtKey], d.now())
	features, ok := root[claudeGrowthBookFeaturesKey]
	if !ok {
		f.Value, f.Detail = "absent", "no "+claudeGrowthBookFeaturesKey
		return f
	}
	var flags map[string]json.RawMessage
	if err := json.Unmarshal(features, &flags); err != nil || flags == nil {
		f.Value, f.Detail = "unreadable", claudeGrowthBookFeaturesKey+" is not an object"
		return f
	}
	value, ok := flags[claudeHooksModulesFlag]
	if !ok {
		f.Value = "absent"
		return f
	}
	f.Value = compactJSONValue(value)
	return f
}

// Claude Code stores the stamp as epoch milliseconds; anything else is shown
// as it is.
func renderGrowthBookCachedAt(raw json.RawMessage, now time.Time) string {
	if raw == nil {
		return "absent"
	}
	var ms float64
	if json.Unmarshal(raw, &ms) == nil && ms > 0 {
		at := time.UnixMilli(int64(ms)).UTC()
		return fmt.Sprintf("%s (%s before this read)", at.Format(time.RFC3339), now.Sub(at).Round(time.Second))
	}
	return compactJSONValue(raw)
}

func compactJSONValue(raw json.RawMessage) string {
	var buf bytes.Buffer
	s := string(raw)
	if json.Compact(&buf, raw) == nil {
		s = buf.String()
	}
	if len(s) > hooksModulesFlagValueCap {
		cut, _ := tailBytes(s, hooksModulesFlagValueCap)
		s = "…" + cut
	}
	return s
}

func (d SpawnDeps) logHooksModulesFlag(memberID, when string) hooksModulesFlag {
	f := d.readHooksModulesFlag()
	d.logf("%s: notify-mod: %s: %s", memberID, when, f)
	return f
}

// Appended to notifyModNotLoadedNote when the restart did not help either.
func notifyModRetriedNote(first, second hooksModulesFlag) string {
	return "warden 已自動重啟 Claude Code 再試一次，仍沒有載入（啟動前 Claude Code 快取的 " +
		claudeHooksModulesFlag + "：第 1 次 " + first.Value + "，第 2 次 " + second.Value + "）。"
}

type notifyModRetry struct {
	relaunched bool
	launchedAt time.Time
	flag       hooksModulesFlag
	// The disabled marker is in place, so the fallback need not write it again.
	disabled   bool
	failReason string
}

// retryNotifyMod runs when the first wait ended without the load marker. It
// restarts Claude Code ONCE, and only when the first one provably never ran the
// mod: then nothing was submitted (the mod submits the boot prompt, the warden
// pasted none), no listener ever connected, and nothing was marked read, so
// killing it loses nothing and the restart's boot is the member's only one. A
// mod that started or booted is left to the paste fallback as before: a restart
// there could boot the member twice.
//
// The launch line is reused as is: a normal stop + wake starts a fresh session
// with the same line too (no --resume), and this first one never had a turn.
// Its budget, one teardown and one more 30 s wait, is a line of
// startReceiptDeadlineSecs (server/ocserverd/receipt_watch.go).
// ⚠️ Its worst case also passes WakingTTLSecs (120 s, server/ocserverd/domain.go):
// the server may stamp wake_timeout and resend START, which meets
// session_already_exists and takes over the "zombie", stopping the second attempt.
// ⚠️ A self-update's exec-in-place does not wait for an in-flight start, so it
// can cut this one short anywhere in its ~80 s: left with no member and no
// receipt, or a restarted member with no listener. Neither boots twice; both
// recover through wake_timeout / zombie takeover.
func (d SpawnDeps) retryNotifyMod(memberID, workdir, socket, session, command string, launchedAt time.Time) notifyModRetry {
	// 🔴 BEFORE the started check: the mod writes its started marker before it
	// reads this one, so a session.start racing this check either shows below or
	// stands down instead of booting a member about to be killed.
	d.disableNotifyMod(workdir)
	out := notifyModRetry{disabled: true}
	if d.notifyModStarted(workdir) || d.notifyModBooted(workdir) {
		d.logf("%s: notify-mod: the mod ran in attempt 1 but did not load; no restart (it may have booted the member)", memberID)
		return out
	}
	if d.StopAttempt == nil {
		d.logf("%s: notify-mod: no teardown is wired; no restart", memberID)
		return out
	}
	d.logf("%s: notify-mod: attempt 1 did not run the mod; restarting Claude Code once", memberID)
	// The first attempt's screen, before it is gone.
	d.logNotifyModFallback(memberID, workdir, socket, session, launchedAt)
	if !d.StopAttempt(socket, session, workdir) {
		// stop() is killed && swept: a session already gone with a sweep survivor
		// is false too, and pasting into that missing pane would report a member
		// that is not there as started. An unknown probe (nil) counts as alive.
		if has := tmuxHasSession(d.Runner, socket, session); has != nil && !*has {
			out.failReason = "spawn_exec_failed: restarting after the notification mod did not load: " +
				"attempt 1's tmux session is gone but its teardown did not finish (a process survived the sweep); no restart"
			return out
		}
		d.logf("%s: notify-mod: attempt 1 could not be torn down; no restart", memberID)
		return out
	}
	// The first attempt is gone, so its disabled marker goes too: left in place
	// it would keep the second one's mod from booting.
	if why := d.clearNotifyModMarkers(workdir); why != "" {
		d.logf("%s: notify-mod: %s; restarting anyway", memberID, why)
	}
	out.disabled = false
	out.flag = d.logHooksModulesFlag(memberID, "attempt 2/2")
	if err := tmuxNewSession(d.Runner, socket, session, command); err != nil {
		out.failReason = fmt.Sprintf(
			"spawn_exec_failed: tmux new-session (restarting after the notification mod did not load): %v", err)
		return out
	}
	out.relaunched, out.launchedAt = true, d.now()
	// Its own wait, so a "Plugins changed" banner in this attempt gets its own
	// /reload-plugins.
	d.waitForNotifyMod(memberID, workdir, socket, session)
	return out
}
