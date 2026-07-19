package main

import (
	"fmt"
	"io"
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
// declare the stop intent ONCE over the presence wire (phase=stopping → stopped),
// then self-terminate via `ocagent suicide` (kill my OWN tmux session). Killing the
// session drops THIS SSE, so the server derives stopped from the connection fact —
// BEFORE the grace clock elapses, with no second actor required. The warden's killpg
// ladder stays as the UNTOUCHED force fallback for a crashed/wedged agent that never
// reaches here. The phase reports are 錦上添花 — convergence rides the SSE drop
// either way, so a failed report is logged HONESTLY (never masked) but never aborts
// the self-kill.
//
// RECYCLE (desired_state=online ∧ refocus_since>0 — handover: a NEW me respawns):
// ocagent does NOT report phases and does NOT self-kill. It WAKES the interactive
// Claude session by printing an explicit handover SOP on stdout (the session's
// Monitor tool holds this listener, so the wake lands in its transcript) and the
// SESSION walks the SOP itself over MCP: report_stopping first (honest "winding
// down" signal) → persist in-flight work → consolidate lessons → post a baton
// chat to its own id → report_stopped.
// The kill is then SERVER-orchestrated end to end: the stopped report of a
// refocus-marked, still-desired-online member fires an immediate event-driven
// robust STOP (server api_members.go HandleReportStopped… → dispatchRobustStopNow
// → warden killpg kills the tmux session, taking this listener with it) → the SSE
// drop makes ¬online → the next tick's plain START respawns. A dead/unresponsive
// session that never reports is covered by the server's recycle_grace (120 s)
// fallback: the reconcile tick dispatches the same robust STOP once the grace
// elapses (spec/lifecycle.md §4.5) — so ocagent needs NO local timeout and NO
// self-kill; a client-side kill on a frozen-wire observable is impossible anyway
// (the member DTO exposes no stopped_since, and `presence` still projects
// "stopping" while this SSE is held).
//
// The presence PHASE wire body is {"sessions": [], "phase": <phase>} POSTed to
// /api/members/<self>/presence — the SAME route + body the MCP `set_member_presence`
// tool hits for the boot-step phase=waking report; here the self-stop hook reuses it
// for stopping/stopped. All IO seams are injectable so tests drive the sequences
// with NO network / NO tmux.

// presenceBody is the FROZEN presence wire body. A struct (not a map) pins the
// field set + guarantees `sessions` marshals as `[]` (never `null`) — a phase
// edge carries an EMPTY sessions list, matching HttpPresencePort.report_phase.
type presenceBody struct {
	Sessions []any  `json:"sessions"`
	Phase    string `json:"phase"`
}

// phaseReporter POSTs the frozen presence phase body and returns the observed HTTP
// status (0 on a transport fault — postJSON surfaces a fault as a falsy status). This
// is the default reportPhase seam shared by both hooks and mirrors Python's wrapper
// around HttpPresencePort.report_phase.
func phaseReporter(client httpClient, cfg Config) func(string) int {
	return func(phase string) int {
		status, _ := postJSON(client, cfg, membersPath+cfg.ID+"/presence",
			presenceBody{Sessions: []any{}, Phase: phase})
		return status
	}
}

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
	started bool // one-shot: a repeated member delta does NOT re-report

	// seams (injectable for tests)
	fetchDesired  func() (string, bool) // authoritative desired_state refetch
	reportPhase   func(string) int      // presence phase POST → HTTP status
	selfTerminate func()                // graceful self-kill (default: `ocagent suicide`)
}

func newWindDownHook(client httpClient, cfg Config, env func(string) string, out io.Writer) *windDownHook {
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
		reportPhase:   phaseReporter(client, cfg),
		selfTerminate: func() { cmdSuicide(cfg, env, out) },
	}
}

func (h *windDownHook) say(msg string) { fmt.Fprintf(h.out, "[ocagent] %s\n", msg) }

// reportChecked reports a phase best-effort with an HONEST status check: a non-2xx
// (including a transport-fault 0) is logged FAILED and NOT masked, but never aborts the
// sequence (the warden tmux kill is the real drop, not this report).
func (h *windDownHook) reportChecked(phase string) {
	status := h.reportPhase(phase)
	if status >= 200 && status < 300 {
		h.say(fmt.Sprintf("self-stop: reported phase=%s (HTTP %d).", phase, status))
		return
	}
	h.say(fmt.Sprintf("self-stop: report phase=%s FAILED (HTTP %d) — NOT masked; "+
		"continuing (the warden tmux kill is the real drop, not this report).", phase, status))
}

// maybeWindDown is the listen-loop trigger (side-effect ONLY — it NEVER asks the
// listener to self-exit). Returns true iff it DECLARED the stop intent this call.
// Gated (in order) by the NUDGE match, a one-shot re-entry flag, then a POSITIVE
// authoritative desired_state=offline refetch. Mirrors maybe_wind_down.
func (h *windDownHook) maybeWindDown(frame map[string]any) bool {
	if !shouldWindDown(frame, h.cfg.ID) {
		return false
	}
	if h.started {
		return false // already declared — keep listening (the warden owns the kill)
	}
	desired_state, ok := h.fetchDesired()
	if !ok || desired_state != desiredOffline {
		return false // my row changed but NOT a stop — keep listening
	}
	h.started = true
	h.say("self-stop: desired_state=offline confirmed — winding down (report stopping → " +
		"stopped, then self-terminate via `suicide`; the warden killpg stays as the " +
		"force fallback).")
	h.reportChecked("stopping")
	h.say("self-stop: winding down: durable state already server-side (task step notes " +
		"/ learnings post via MCP) — nothing extra to flush.")
	h.reportChecked("stopped")
	h.say("self-stop: reported phase=stopped (intent gone) — self-terminating: killing " +
		"my own tmux session drops the SSE → server derives offline before the grace " +
		"deadline (warden killpg = force fallback for a crashed agent).")
	if h.selfTerminate != nil {
		h.selfTerminate()
	}
	return true
}

// ---------------------------------------------------------------------------
// RecycleHook — desired_state=online ∧ refocus_since>0: wake the session with the
// handover SOP (wake-only; the kill is server-orchestrated — see the file header).
// ---------------------------------------------------------------------------

type recycleHook struct {
	cfg Config
	out io.Writer
	// The refocus epoch already woken for (0 = none). A NEW, larger epoch re-arms the
	// one-shot (the owner refocused again after a respawn).
	handledRefocus float64

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

// handoverSOP is the wake message printed into the session's Monitor transcript when
// the server marks THIS agent for recycle. The five steps mirror the boot-context
// handover contract (seeds/system_interaction.md §8b): stopping is reported FIRST
// (the cockpit flips to "stopping" the moment wind-down begins — an honest
// transition, and harmless: the server only kills on the stopped report or the
// grace timeout), durable writes follow (2–4), and the stopped report comes LAST —
// it is what lets the server kill/respawn immediately instead of waiting out the
// 120 s grace.
func handoverSOP(selfID string) []string {
	return []string{
		"recycle: server 已標記回收（refocus）— 請立刻照換手 SOP 收尾（約 120 秒寬限，逾時 server 會強制回收，未落盤的 context 就沒了）：",
		"recycle:   1) MCP report_stopping() — 先告知世界你開始收尾（座艙即顯停止中；server 不會因此提前收你）",
		"recycle:   2) 把在飛的工作寫回 task step note（做到哪、下一步接什麼）",
		"recycle:   3) 用 MCP get_lessons / replace_lessons 整併這輪的耐久教訓（合併、更新、刪過時，不是往後貼）",
		"recycle:   4) 用 MCP post_chat 給自己（to=" + selfID + "）發一則交接 baton：現況 / 在途 / blocker",
		"recycle:   5) MCP report_stopped() — 報完就停手；runtime 會自動收攤，server 原地重生新的你",
	}
}

// maybeRecycle is wind-down's refocus twin, but WAKE-ONLY (it never reports a phase
// and never self-kills — the handover is the SESSION's job and the kill is the
// SERVER's, per the file header). Returns true iff it woke the session this call.
// Gated (in order) by the NUDGE match, then a POSITIVE authoritative refetch of
// desired_state=online ∧ refocus_since>0 ∧ a NEW refocus epoch (one wake per epoch —
// the follow-up member deltas fanned by the session's own stopping/stopped reports
// re-enter here and must NOT re-print the SOP). Mutually exclusive with wind-down
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
	if refocus == h.handledRefocus {
		return false // already woke THIS epoch — a NEW, larger epoch re-arms below
	}
	h.handledRefocus = refocus
	for _, line := range handoverSOP(h.cfg.ID) {
		h.say(line)
	}
	return true
}
