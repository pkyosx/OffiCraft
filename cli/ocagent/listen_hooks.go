package main

import (
	"fmt"
	"io"
	"strings"
)

// ---------------------------------------------------------------------------
// graceful lifecycle hooks — WindDownHook (desired_state=offline) + RecycleHook
// (desired_state=online ∧ refocus marker).
// ---------------------------------------------------------------------------
//
// Both hooks trigger the same way: the server fans a `member` delta naming THIS
// agent down its own /api/events; the hook REFETCHES the authoritative member row
// (R7 — the delta payload is a NUDGE, never trusted) and acts only on a POSITIVE
// read of the matching intent. From there the two DIVERGE:
//
// WIND-DOWN (desired_state=offline — the member is being taken DOWN, no respawn):
// WAKE the session with the server's offboard notice and leave the closing-out and
// the stop reports to it, exactly like RECYCLE below. It does NOT report phases and
// does NOT self-kill.
//
// 🔴 It used to do both — declare phase=stopping → stopped and `suicide` — with the
// line 「durable state already server-side — nothing extra to flush」. That was
// false of any session still holding an unwritten hand-off, an unposted step note
// or an unfolded lesson, and the sequence took the session down before it could
// write them; 下線 was the ONE offboard path that never showed the agent the
// checklist. The collection is now the server's, on the session's own
// report_stopped, which collects it immediately. There is no timer behind that:
// the owner ruled the only other way out is his force-stop button, so a session
// that never reports simply stays up, visible to him as 停止中, until he presses
// it (the warden killpg ladder is unchanged underneath).
//
// RECYCLE (desired_state=online ∧ refocus_since>0 — handover: a NEW me respawns):
// ocagent does NOT report phases and does NOT self-kill. It WAKES the interactive
// Claude session by printing the server's 下線程序 document on stdout (the session's
// Monitor tool holds this listener, so the wake lands in its transcript) and the
// SESSION walks that checklist itself over MCP. The text is NOT this binary's:
// the SERVER composes it and pushes it IN the member delta (owner 2026-08-16:
// 「改回真的推播」), so an owner can change what a collected session is told
// without a release, and this binary has no fetch of its own to get wrong.
// The kill is then SERVER-orchestrated end to end: the stopped report of a
// refocus-marked, still-desired-online member fires an immediate event-driven
// robust STOP (server api_members.go HandleReportStopped… → dispatchRobustStopNow
// → warden killpg kills the tmux session, taking this listener with it) → the SSE
// drop makes ¬online → the next tick's plain START respawns. A dead/unresponsive
// session that never reports is covered by the server's recycle grace (120 s for
// every cause except an owner-pressed 重新聚焦, which opens SOFT and gets the
// soft window first — recycleGraceFor): the reconcile tick dispatches the same
// robust STOP once the grace elapses (spec/lifecycle.md §4.5) — so ocagent needs
// NO local timeout and NO
// self-kill; a client-side kill on a frozen-wire observable is impossible anyway
// (the member DTO exposes no stopped_since, and `presence` still projects
// "stopping" while this SSE is held).
//
// All IO seams are injectable so tests drive the sequences with NO network and
// NO tmux.

// Nothing in this binary reports a presence phase any more. Both stop phases
// are the SESSION's to report over MCP (report_stopping / report_stopped are
// steps 1 and 6 of the 下線程序), which is the point of the change: a hook that
// declared them on the session's behalf was declaring a close-out that had not
// happened. The route and its wire body are unchanged and still served — this
// binary simply is not one of its callers.

// fetchMemberRow refetches the authoritative member row for THIS agent (R7 — the
// truth is GET /api/members/<self>). ok=false on any fault (⇒ do NOT act; a stop/
// recycle fires only on a POSITIVE read). Shared default for both hooks.
func fetchMemberRow(client httpClient, cfg Config) (map[string]any, bool) {
	status, body := getJSON(client, cfg, membersPath+cfg.ID, true)
	if status != 200 {
		return nil, false
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, false
	}
	return m, true
}

// ---------------------------------------------------------------------------
// WindDownHook — desired_state=offline graceful self-stop (intent-only).
// ---------------------------------------------------------------------------

type windDownHook struct {
	cfg     Config
	out     io.Writer
	started bool // a repeated member delta carrying the SAME notice is silent
	// lastNotice is the sentence already shown for this wind-down. The soft
	// notice and the final call differ by the 120-second clause, so keying on
	// the text is what lets the second one through without re-printing the
	// first on every follow-up delta.
	lastNotice string

	// seams (injectable for tests)
	fetchDesired func() (string, bool) // authoritative desired_state refetch
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

// maybeWindDown is the listen-loop trigger (side-effect ONLY — it NEVER asks the
// listener to self-exit). Returns true iff it WOKE the session this call.
// Gated (in order) by the NUDGE match, then a POSITIVE authoritative
// desired_state=offline refetch, then a notice this hook has not already shown.
//
// 🔴 IT NO LONGER STOPS THE SESSION ON ITS BEHALF. It used to declare
// phase=stopping, print 「durable state already server-side — nothing extra to
// flush」, declare phase=stopped and `suicide` — a sentence that was FALSE of
// any session holding an unwritten hand-off, an unposted step note or an
// unfolded lesson, and a sequence that gave the agent no chance to write them.
// 下線 now walks the SAME path as context pressure (owner 2026-08-16): the
// server pushes the offboard notice, the SESSION works it and reports stopped
// itself, and that report is what collects it.
//
// Not one-shot but notice-keyed: the soft notice and the final call are two
// different sentences on the same wind-down, and the second one — the one that
// says the 120 seconds have started — has to get through.
func (h *windDownHook) maybeWindDown(frame map[string]any) bool {
	if !shouldWindDown(frame, h.cfg.ID) {
		return false
	}
	desired_state, ok := h.fetchDesired()
	if !ok || desired_state != desiredOffline {
		return false // my row changed but NOT a stop — keep listening
	}
	notice := offboardNoticeIn(frame)
	if h.started && notice == h.lastNotice {
		return false // same sentence already delivered — do not repeat it
	}
	h.started = true
	h.lastNotice = notice
	h.wake(notice)
	return true
}

// wake prints the server-composed notice into the session's transcript, or the
// shared fallback when the frame carried none. Same contract as the recycle
// hook's: losing the checklist is survivable, being collected without knowing
// it is not.
func (h *windDownHook) wake(notice string) {
	if strings.TrimSpace(notice) == "" {
		h.say(offboardFallback)
		return
	}
	for _, line := range strings.Split(notice, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		h.say("offboard: " + line)
	}
}

// ---------------------------------------------------------------------------
// RecycleHook — desired_state=online ∧ refocus_since>0: wake the session with the
// server's 下線程序 text (wake-only; the kill is server-orchestrated — see the header).
// ---------------------------------------------------------------------------

type recycleHook struct {
	cfg Config
	out io.Writer
	// The refocus epoch already woken for (0 = none). A NEW, larger epoch re-arms the
	// one-shot (the owner refocused again after a respawn).
	handledRefocus float64
	// lastNotice is the sentence already shown for that epoch. An owner-pressed
	// 重新聚焦 opens SOFT and is promoted to the final call on the same epoch,
	// so the epoch alone would swallow the sentence that says the 120 seconds
	// have started.
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

// offboardFallback is the ONLY hard-coded wake text left in this binary. The wake
// message itself is the server's 下線程序 document (owner-editable, seed-backed) —
// it is fetched on the same edge that refetches the member row, so a fetch that
// faults or answers an EMPTY document must still leave the agent knowing it is
// being collected. Losing the checklist is survivable; losing the notice is not.
const offboardFallback = "recycle: server 要收你了，但這則通知沒有帶到下線程序 —— " +
	"請立刻用 MCP get_offboard 拿完整收尾清單並照做，別空手停下。"

// offboardNoticeIn digs the server-composed notice out of a member delta:
// frame → data → payload → offboard_notice. Every miss answers "", which is
// what arms the fallback — a frame from a server too old to push the notice
// looks exactly like a frame whose payload lost it, and both must still leave
// the agent knowing it is being collected.
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

// wakeForRecycle prints the wake message into the session's Monitor transcript:
// the notice the SERVER composed and pushed in this frame, line by line, or the
// fallback above when the frame carried none. Blank lines are dropped — the
// transcript is a line wire, not a rendered document.
//
// The text is no longer fetched back over HTTP (owner 2026-08-16: 「改回真的
// 推播」). What that costs is the one thing worth writing down: a frame that
// arrives without the notice cannot be repaired from this side, so the fallback
// has to name the tool that gets it — losing the checklist is survivable, being
// collected without knowing it is not.
func (h *recycleHook) wakeForRecycle(notice string) {
	if strings.TrimSpace(notice) == "" {
		h.say(offboardFallback)
		return
	}
	for _, line := range strings.Split(notice, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		h.say("recycle: " + line)
	}
}

// maybeRecycle is wind-down's refocus twin, but WAKE-ONLY (it never reports a phase
// and never self-kills — the handover is the SESSION's job and the kill is the
// SERVER's, per the file header). Returns true iff it woke the session this call.
// Gated (in order) by the NUDGE match, then a POSITIVE authoritative refetch of
// desired_state=online ∧ refocus_since>0 ∧ a NEW refocus epoch (one wake per epoch —
// the follow-up member deltas fanned by the session's own stopping/stopped reports
// re-enter here and must NOT re-print the wake). The epoch is claimed BEFORE the
// document is fetched, so a failed fetch spends the epoch on the fallback notice
// rather than re-waking on every later delta. Mutually exclusive with wind-down
// (offline vs online intent), so both are safe to call on every member delta.
func (h *recycleHook) maybeRecycle(frame map[string]any) bool {
	if !shouldWindDown(frame, h.cfg.ID) { // identical NUDGE gate
		return false
	}
	member, ok := h.fetchMember()
	if !ok {
		return false
	}
	if d, _ := member["desired_state"].(string); d != desiredOnline {
		return false // not an online-intent member → recycle does not apply
	}
	refocus, ok := member["refocus_since"].(float64)
	if !ok || refocus <= 0 {
		return false // no pending refocus marker → nothing to recycle
	}
	notice := offboardNoticeIn(frame)
	if refocus == h.handledRefocus && notice == h.lastNotice {
		return false // already woke THIS epoch with THIS sentence
	}
	h.handledRefocus = refocus
	h.lastNotice = notice
	h.wakeForRecycle(notice)
	return true
}
