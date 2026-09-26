package main

import (
	"fmt"
	"io"
	"strings"
)

// Lifecycle hooks, fired by a `member` delta naming this agent. R7: the delta is
// only a nudge — desired_state / refocus_since come from a refetch of the member
// row; the one thing taken from the payload is offboard_notice, which the SERVER
// composes and pushes there (owner-editable 〈停止〉 text, no client fetch).
//
// Both hooks only WAKE the session — never report a phase or self-kill: the
// session itself must walk the 〈停止〉 checklist (hand-off, step notes, lessons)
// and call report_stopped over MCP; that report is what the server collects on
// (recycle: HandleReportStopped in api_members.go → dispatchRobustStopNow → warden
// killpg on the tmux session (this listener dies with it) → SSE drop → the next
// reconcile tick's START respawns).
//
// 🔴 No local timeout (owner ruling, T-ed79): a session that never reports stays
// up until the owner presses 加速停止 or 強制停止; the only clock is the server's
// recycle grace (recycleGraceFor, spec/lifecycle.md §4.5).

func fetchMemberRow(client httpClient, cfg Config) (map[string]any, bool) {
	status, body := getJSON(client, cfg, membersPath+cfg.MemberID, true)
	if status != 200 {
		return nil, false
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, false
	}
	return m, true
}

type windDownHook struct {
	cfg     Config
	out     io.Writer
	started bool
	// Keyed on the whole sentence: the server's soft notice and final call
	// differ only by the deadline clause. 🔴 That is why the server quotes an
	// ABSOLUTE deadline, never a countdown — a remaining-seconds number would
	// change on every replay and re-wake the session on every write to its row.
	lastNotice string

	fetchDesired func() (string, bool)
}

func newWindDownHook(client httpClient, cfg Config, out io.Writer) *windDownHook {
	return &windDownHook{
		cfg: cfg,
		out: out,
		fetchDesired: func() (string, bool) {
			m, ok := fetchMemberRow(client, cfg)
			if !ok {
				return "", false
			}
			d, ok := m["desired_state"].(string)
			return d, ok
		},
	}
}

func (h *windDownHook) say(msg string) { fmt.Fprintf(h.out, "[ocagent] %s\n", msg) }

func (h *windDownHook) maybeWindDown(frame map[string]any) bool {
	if !isMemberFrameForSelf(frame, h.cfg.MemberID) {
		return false
	}
	desired, ok := h.fetchDesired()
	if !ok || desired != desiredOffline {
		return false
	}
	notice := offboardNoticeIn(frame)
	if h.started && notice == h.lastNotice {
		return false
	}
	h.started = true
	h.lastNotice = notice
	h.wake(notice)
	return true
}

func (h *windDownHook) wake(notice string) {
	if strings.TrimSpace(notice) == "" {
		h.say("offboard: " + offboardFallback)
		return
	}
	for _, line := range strings.Split(notice, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		h.say("offboard: " + line)
	}
}

type recycleHook struct {
	cfg            Config
	out            io.Writer
	handledRefocus float64
	// The server may say something new on the same epoch; keying on the
	// sentence is the de-dup contract api_members.go names.
	lastNotice string

	fetchMember func() (map[string]any, bool)
}

func newRecycleHook(client httpClient, cfg Config, out io.Writer) *recycleHook {
	return &recycleHook{
		cfg:         cfg,
		out:         out,
		fetchMember: func() (map[string]any, bool) { return fetchMemberRow(client, cfg) },
	}
}

func (h *recycleHook) say(msg string) { fmt.Fprintf(h.out, "[ocagent] %s\n", msg) }

// Shipped text: the agent reads these bytes on stdout (like seeds/*.md), and
// nothing reports a bad edit. 〈停止〉 must match the document's on-screen name
// (ruling rc-e12733548e4b) — the agent goes looking for it by that name.
const offboardFallback = "server 要收你了，但這則通知沒有帶到〈停止〉 —— " +
	"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。"

func offboardNoticeIn(frame map[string]any) string {
	data, ok := frame["data"].(map[string]any)
	if !ok {
		return ""
	}
	payload, ok := data["payload"].(map[string]any)
	if !ok {
		return ""
	}
	notice, _ := payload["offboard_notice"].(string)
	return notice
}

func (h *recycleHook) wakeForRecycle(notice string) {
	if strings.TrimSpace(notice) == "" {
		h.say("recycle: " + offboardFallback)
		return
	}
	for _, line := range strings.Split(notice, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		h.say("recycle: " + line)
	}
}

// The session's own report_stopping/report_stopped make the server fan more
// member deltas, which re-enter here and must not re-wake it; and the notice
// rides EVERY write to the row. So the epoch is claimed even by a fallback wake,
// and only a new epoch OR a new sentence (soft→final, or the real notice after a
// fallback) wakes again.
func (h *recycleHook) maybeRecycle(frame map[string]any) bool {
	if !isMemberFrameForSelf(frame, h.cfg.MemberID) { // identical NUDGE gate
		return false
	}
	member, ok := h.fetchMember()
	if !ok {
		return false
	}
	if d, _ := member["desired_state"].(string); d != desiredOnline {
		return false
	}
	refocus, ok := member["refocus_since"].(float64)
	if !ok || refocus <= 0 {
		return false
	}
	notice := offboardNoticeIn(frame)
	if refocus == h.handledRefocus && notice == h.lastNotice {
		return false
	}
	h.handledRefocus = refocus
	h.lastNotice = notice
	h.wakeForRecycle(notice)
	return true
}
