package main

// sse_bands.go — the DIRECTED SSE bands (spec/sse.md §6/§6.1/§7/§8), ported from
// the retired Python service/sse/{context_high,warden_command}.py:
//
//   * context-high (§6): the server watches each agent's context_pct gauge and
//     pushes ONE directed advance notice down the agent's own connection before
//     that agent's handover — the stream loop in api_infra.go calls
//     decideHandoverNotice each quiet tick. Only that notice ever emits on the
//     wire; the HANDOVER band itself belongs to the producer auto-recycle
//     (reconcile.go stampContextHighRecycle).
//
//     T-c382 rewrote WHEN it fires and HOW OFTEN, and both halves matter:
//       - WHEN: derived from the handover threshold the owner actually sets
//         (claude: threshold - handoverNoticeLeadPct; codex: the round before
//         its compaction ceiling). It used to be its own hard-wired 40 with no
//         UI, so an owner who moved handover to 65% did not move the notice.
//       - HOW OFTEN: exactly once per SESSION. It used to re-fire every
//         RemindStepPct of gauge climb, which is five nudges between 40 and 65
//         — and the agent that obeys the first one has stopped working with a
//         quarter of its context unspent.
//     Both knobs (warn_pct, remind_step_pct) are gone rather than retuned: a
//     threshold that does not track the one the owner can see is a second
//     source of truth, and it was already wrong in production.
//
//   * token-expiry (§6.1): the server watches the verified JWT exp carried by
//     the live SSE request and repeatedly asks a restartable agent to use
//     restart_self before its credential becomes unusable.
//
//   * warden-command (§7): the directed frame envelope + the wire arg shapes.
//     The producer that decides + dispatches these frames (cadence tick,
//     event-driven click seams, grace clocks, reconcile store) lives in
//     reconcile.go.
//
//   * task-close (§8): the terminal-task nudge that asks an executor to write
//     back its learnings and report closeout.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// ── context-high band (service/sse/context_high.py) ─────────────────────────

const (
	contextHighTopic = "context-high"
	// tokenExpiryTopic is the agent's own advance notice that its currently
	// authenticated session will shortly lose every API surface. It is a
	// directed band rather than a durable task/chat record: replacing the token
	// by restart_self is the acknowledgement, and a replacement session gets a
	// fresh expiry claim.
	tokenExpiryTopic = "token-expiry"

	levelNone = "none"
	levelWarn = "warn"
	// levelHandover is decided but NEVER emitted on the wire (spec §6): the
	// >= handover response is the server-side producer auto-recycle (step ⑥).
	levelHandover = "handover"
)

// The advance notice is DERIVED from the handover threshold the owner actually
// sets, never configured beside it (T-c382). That is the whole bug: warn_pct
// used to be its own hard-wired 40 with no UI, so setting handover to 65% left
// the notice 25 points early and re-firing every 5 — five nudges before the
// event they were warning about, and an agent that obeys the first one winds
// down with a quarter of its context unspent (measured: a member wrote three
// batons and announced its own end of life at 50%).
//
// One lead figure per runtime, because the two runtimes hand over on different
// axes and a percentage means nothing to one of them:
const (
	// handoverNoticeLeadPct is the claude lead: notify at HandoverPct - 10.
	// Owner 2026-08-16, verbatim: 「上限前的 10%…例如 65% 的話會從 55% 開始通知,
	// 但是只通知一次」.
	handoverNoticeLeadPct = 10
	// codexNoticeRoundPct is the codex lead: a codex session hands over on
	// COMPACTION COUNT, so "10% before" has no meaning on its axis. Owner
	// 2026-08-16, verbatim: 「codex 的話則是在前一輪的 60% 開始提醒」「例如我設定
	// 是 5 那就是在第四輪的 60% 提醒一次」 — i.e. one round before the last, at
	// 60% through that round's context.
	codexNoticeRoundPct = 60
)

const (
	// tokenExpiryWarningWindow is owner-set product policy: give an agent thirty
	// minutes to checkpoint its current turn and request restart_self before the
	// token becomes unusable.
	tokenExpiryWarningWindow = 30 * 60 // seconds
	// tokenExpiryReminderInterval keeps the warning pending instead of treating
	// one SSE frame as an acknowledgement. The server has no proof an agent read
	// a frame; only a replacement session (or expiry) settles it.
	tokenExpiryReminderInterval = 30 // seconds
)

// asNumber narrows a gauge value to float64 (a bool is NOT a number here).
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// bandFor is the pure band decision. FAIL-SAFE: no pct → none. A threshold
// <= 0 disables the band (kill-switch).
//
// Since T-c382 there is only one band left to decide: the ADVANCE NOTICE is no
// longer a band with its own threshold (it is derived — see
// decideHandoverNotice), so warnPct is gone from the signature and levelWarn is
// never returned here. Keeping a warn arm that nothing could reach would be a
// dead branch that reads like a live one.
func bandFor(pct *float64, handoverPct int) string {
	if pct != nil && handoverPct > 0 && *pct >= float64(handoverPct) {
		return levelHandover
	}
	return levelNone
}

// claudeNoticePct DERIVES the old single-threshold notice point (handover minus
// the lead). Since T-a9d6 the notice point is a SETTING, not a derivation — the
// owner sets the pair — so this is no longer part of the live decision. Its one
// remaining caller is the UPGRADE PATH (settings.go): an install that predates
// the pair has no stored notice_pct, and filling it from here is what makes the
// upgrade change no behaviour at all. Do not wire it back into the band.
func claudeNoticePct(handoverPct int) (int, bool) {
	if handoverPct <= 0 {
		return 0, false
	}
	at := handoverPct - handoverNoticeLeadPct
	if at <= 0 {
		return 0, false
	}
	return at, true
}

// codexNoticeDue reports whether a CODEX session is in its notice window: the
// round BEFORE the one that hands over (compaction_count == threshold-1), and
// at least codexNoticeRoundPct through that round's context.
//
// It reads compaction_count, NOT percent-against-the-handover-threshold, and
// that asymmetry is the second half of the T-c382 bug: the old band was
// runtime-blind, so a codex worker — whose handover is decided purely by
// compaction count — was being warned on a percentage with no relation to its
// own lifecycle (坐實: worker ow-638847c9d5f6 carrying context_pct 45.6 and
// compaction_count 3 while the band's threshold was 40).
// Since T-a9d6 the notice ROUND is the owner's own setting (the first of his
// pair, e.g. 「codex 則是 5 / 6 表示第五輪開始通知，第六輪 120 秒」) rather than
// threshold-1; noticeRound <= 0 falls back to that old derivation so a config
// that predates the pair behaves exactly as it did.
func codexNoticeDue(record map[string]any, pct *float64, noticeRound, codexThreshold int) bool {
	if codexThreshold < 1 {
		codexThreshold = defaultCodexCompactionThreshold
	}
	if noticeRound < 1 {
		noticeRound = codexThreshold - 1
	}
	count, ok := record["compaction_count"].(int)
	if !ok {
		return false
	}
	return count == noticeRound && pct != nil && *pct >= codexNoticeRoundPct
}

// gaugeBootTS narrows a gauge record's boot_ts to a float64 (false when absent /
// non-numeric / nil record) — the SSE-connect boot anchor the stale-pct guard,
// the boot-storm loop-guard, and the worker refocus loop-break all read.
func gaugeBootTS(record map[string]any) (float64, bool) {
	if record == nil {
		return 0, false
	}
	return asNumber(record["boot_ts"])
}

// gaugeSecsSinceBoot is the seconds-since-boot loop-guard input, computed
// identically for EVERY caller that feeds bootStormTripped — the member
// context-high auto-stamp (reconcile.stampContextHighRecycle), the member
// self-restart min-liveness gate (HandleRestartSelf), and the worker context
// auto-handover (autoHandoverWorker). nil when there is no usable boot_ts
// (missing gauge / server-restart amnesia) so the guard FAILS OPEN, never a
// false trip. Shared so the three lifecycle paths can never drift apart.
func gaugeSecsSinceBoot(record map[string]any, now float64) *float64 {
	bootTS, ok := gaugeBootTS(record)
	if !ok {
		return nil
	}
	secs := now - bootTS
	return &secs
}

// actionableContextPct returns the pct that may DRIVE the band decision, or
// nil when it must not: with the stale guard on, a pct counts only when its
// report ts is strictly newer than the connection's boot_ts — a predecessor
// session's leftover pct never triggers (spec §6).
func actionableContextPct(record map[string]any, staleGuard bool) *float64 {
	if record == nil {
		return nil
	}
	pct, ok := asNumber(record["context_pct"])
	if !ok {
		return nil
	}
	if !staleGuard {
		return &pct
	}
	pctTS, okPct := asNumber(record["context_pct_ts"])
	bootTS, okBoot := asNumber(record["boot_ts"])
	if !okPct || !okBoot || pctTS <= bootTS {
		return nil
	}
	return &pct
}

// contextHighSignal is the inner directed payload {topic,to,level,pct,reason}
// (spec §6 — the envelope duplicates topic).
type contextHighSignal struct {
	Topic  string    `json:"topic"`
	To     string    `json:"to"`
	Level  string    `json:"level"`
	Pct    jsonFloat `json:"pct"`
	Reason string    `json:"reason"`
}

// decideHandoverNotice composes the whole per-tick decision: gauge record →
// actionable pct → this runtime's notice rule → the ONE advance notice (or nil
// to stay quiet). Fail-safe by construction: no usable gauge → nil.
//
// It does NOT dedupe. Firing exactly once is a SESSION-scoped fact and this
// function is pure, so the caller owns it (api_infra.go, keyed on the gauge's
// boot_ts) — see the once-per-session note there for why per-connection state
// is not good enough.
//
// The reason line is not decoration. It carries the ceiling (an agent cannot
// read its own context %, which is why the server pushes at all) and the three
// things the owner requires done before the handover — verbatim, 2026-08-16:
// 「1. 把交接事項放進 chat 留給自己 2. 更新 task 把狀態或備注寫進去 3. 把
// learning / lesson 寫回去」. A notice that only says "you are running out"
// tells the agent nothing it can act on.
//
// T-6bd2 adds ONE more thing to the same frame: the caller's long-lived
// documents that are close to their character cap. It rides HERE, and nowhere
// on the write path, because writing memory back is step 4 of the sequence this
// notice carries — an agent told at step 4 that its task manual has 167
// characters left has no time left to do anything but delete words until the
// write fits. Told at the SOFT notice, it still has the whole close-out ahead
// of it.
//
// ⚠️ `offboard` and `docCapacity` are closures so a tick that decides to stay
// QUIET never pays for them — and that is ALL the closure buys. It does NOT
// make them once-per-session: this function keeps no state, so once an agent is
// past its notice point it returns non-nil on EVERY tick and runs both closures
// on every one of them, until the session ends. Two comments here used to claim
// the opposite ("must not run on the idle path of every connection to serve a
// frame that fires once per session"); the independent review measured the tick
// at 21.3µs → 574.2µs (26.9×, empty station) precisely because the claim was
// false. What actually bounds the cost is the CALLER refusing to
// call this once the session's one notice is spent — api_infra.go's
// handoverNoticeTick asks handoverNoticeSettled first, and
// TestHandoverNoticeTick_ClosuresAreNotRunAfterTheClaim counts the calls.
func decideHandoverNotice(
	agentID, runtime string, record map[string]any,
	cfg SseContextHighConfig, codexNoticeRound, codexThreshold int,
	offboard func() string, docCapacity func() string,
) *contextHighSignal {
	pct := actionableContextPct(record, cfg.StaleGuard)
	var where string
	switch {
	case NormalizeRuntime(runtime) == RuntimeCodex:
		if !codexNoticeDue(record, pct, codexNoticeRound, codexThreshold) {
			return nil
		}
		if codexThreshold < 1 {
			codexThreshold = defaultCodexCompactionThreshold
		}
		if codexNoticeRound < 1 {
			codexNoticeRound = codexThreshold - 1
		}
		where = fmt.Sprintf("compaction round %d (your limits: round %d / round %d)",
			codexNoticeRound, codexNoticeRound, codexThreshold)
	default:
		if cfg.HandoverPct <= 0 || cfg.NoticePct <= 0 ||
			pct == nil || *pct < float64(cfg.NoticePct) {
			return nil
		}
		where = fmt.Sprintf("context %v%% (your limits: %d%% / %d%%)",
			formatPct(*pct), cfg.NoticePct, cfg.HandoverPct)
	}
	// Read only once THIS tick has decided to speak — a quiet tick (below the
	// notice point / wrong compaction round) pays nothing. It is NOT read once
	// per session: every tick from here to the end of the session reaches this
	// line, which is why the caller short-circuits before entering at all.
	var text string
	if offboard != nil {
		text = offboard()
	}
	// Appended AFTER the document, never woven into the owner's sentence or the
	// document's steps — both are carried verbatim by ruling (see
	// offboardNotice). "" when nothing is near its cap, which is the ordinary
	// case and leaves the notice byte-identical to what it was before T-6bd2.
	if docCapacity != nil {
		text += docCapacity()
	}
	return &contextHighSignal{
		Topic: contextHighTopic,
		To:    agentID,
		Level: levelWarn,
		Pct:   jsonFloat(*pct),
		// A context-pressure notice always goes to a member that is still wanted
		// online, so its sequence ends in a re-start.
		// A context-pressure notice is never the final call, so it quotes no
		// deadline (T-d6a7); 0 is passed rather than a value nobody reads.
		Reason: offboardNotice(where, offboardCloserRestartSelf, false, 0, text),
	}
}

// offboardNotice composes EVERY offboard notice this server sends, in the one
// sentence the owner approved (2026-08-16, card rc-ec5859a4c384):
//
//	<where> — offboard now: work the sequence below, then call <closer> yourself.[ Your deadline is <RFC3339 UTC>.]
//	<the 下線程序 document, verbatim>
//
// Three things about it are deliberate and must survive edits:
//
//   - ONE sentence for every situation. The owner cut four differently-worded
//     notices down to this: 「不需要太多不同描述吧, 就請他按照步驟做好下線, 頂多
//     告訴他剩下 120 秒」. What tells the situations apart is the FIELDS — the
//     numbers in `where`, and whether the deadline clause is there — not tone.
//     ⚠️ That clause names an INSTANT, never a span. It was "You have 120
//     seconds left." until T-d6a7; the deadline runs from the first stamp and
//     this notice is REPLAYED on every write to the row, so a duration told a
//     later replay it had the full window when most of it was gone — and a
//     LIVE duration would be worse still, since the client de-dupes on the
//     whole sentence verbatim.
//   - "work the sequence below, THEN call <closer> yourself" blocks both
//     failure directions at once. Without the second half an agent idles until
//     the server cuts it off (dead time the owner explicitly does not want);
//     without the first, it stops mid-work — a predecessor read the old wording
//     as "you are done" and announced its own end of life at 40%.
//   - 🔴 <closer> is the tool that ACTUALLY works for this member, and the two
//     arms differ. A handover (desired online) ends in restart_self. A 下線
//     does NOT: restart_self refuses a member that is no longer wanted online,
//     by design — it is a RE-start. Naming it there told the agent to do
//     something that could only answer 409, and on that arm nothing collects it
//     on a clock, so it would sit refused until the owner pressed force-stop.
//     Its sequence ends at report_stopped, which is also step 6 of the document
//     it is being shown.
//   - The steps are the DOCUMENT's, carried verbatim, never a summary written
//     here. A summary in code is a second source of truth that the owner cannot
//     edit and nothing keeps in step (T-c382 shipped exactly that mistake).
//
// An empty document degrades to the sentence alone: losing the checklist is
// survivable, losing the notice is not.
// The two tools a session can end its own sequence with. Which one is TRUE for
// a given member is decided by offboardCloserFor — naming the wrong one asks it
// to make a call that can only be refused.
const (
	offboardCloserRestartSelf   = "restart_self"
	offboardCloserReportStopped = "report_stopped"
)

func offboardNotice(where, closer string, finalCall bool, deadline float64, offboardText string) string {
	reason := where + " — offboard now: work the sequence below, then call " +
		closer + " yourself."
	// T-d6a7 — the final call quotes an ABSOLUTE deadline, not a duration.
	//
	// It used to say a hardcoded "You have 120 seconds left." while the deadline
	// runs from the FIRST stamp, and this notice is REPLAYED whenever the row is
	// rewritten (the context pct is part of `where`, so a pct change re-sends
	// it). Measured on a live station: two notices 46 s apart, both claiming
	// 120 s — the second one telling an agent it had 120 s when it had ~74.
	//
	// 🔴 The intuitive fix — printing the seconds REMAINING — is the one thing
	// this must not do. The client de-dupes notices by comparing the whole
	// sentence verbatim (cli/ocagent listen_hooks), so a countdown makes every
	// replay a different string, the de-dupe never matches again, and an agent
	// working its close-out is woken and re-fed the whole document on every
	// write to its row. An absolute deadline is CONSTANT within the epoch, so
	// the sentence is stable and the number cannot go stale.
	//
	// The value comes from refocusDeadlineOf — the SAME expression that fills
	// the cockpit's refocus_deadline — so there is no second source of truth to
	// drift. deadline <= 0 means nothing collects this epoch on a clock, and
	// then no time is quoted at all: offboardKindOf only returns "final" for a
	// clocked arm (refocus_since > 0 and refocus_op != refocus), so a final call
	// with no deadline is a contradiction, and printing epoch 0 formatted as
	// 1970 would be worse than saying nothing.
	if finalCall && deadline > 0 {
		// .UTC() is not cosmetic. The reader is an AGENT, which need not be
		// running on this host, and the whole point of the sentence is that the
		// same epoch renders to the same characters on every replay — an
		// implicit local zone makes the literal depend on the server process's
		// TZ. Every other machine-read RFC3339 in this tree is explicit.
		reason += " Your deadline is " +
			time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339) + "."
	}
	if offboardText != "" {
		reason += "\n" + offboardText
	}
	return reason
}

// formatPct renders the pct for the human reason line (45 not 45.0 for whole
// numbers — the Python f-string prints the float, but the wording is not
// contract; keep it readable).
func formatPct(pct float64) any {
	if pct == float64(int64(pct)) {
		return int64(pct)
	}
	return pct
}

// tokenExpirySignal is the inner payload of the repeating token-expiry
// directed band. expires_in is deliberately an integer number of seconds: it
// gives the agent enough urgency to prioritise a safe checkpoint, without
// asking it to inspect or expose its credential.
type tokenExpirySignal struct {
	Topic     string `json:"topic"`
	To        string `json:"to"`
	ExpiresIn int64  `json:"expires_in"`
	Reason    string `json:"reason"`
}

// tokenExpiryRemaining returns the remaining lifetime of the verified request
// token. Normal authenticated requests always carry numeric exp, but this
// deliberately fails closed for synthetic/malformed claim maps so an unknown
// credential shape can never make a listener noisy.
func tokenExpiryRemaining(claims map[string]any, now int64) (int64, bool) {
	if claims == nil {
		return 0, false
	}
	exp, ok := asNumber(claims["exp"])
	if !ok {
		return 0, false
	}
	remaining := int64(exp) - now
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// tokenExpiryClaims narrows tokenExpiryRemaining to the advance-warning window.
func tokenExpiryClaims(claims map[string]any, now int64) (int64, bool) {
	remaining, ok := tokenExpiryRemaining(claims, now)
	if !ok || remaining > tokenExpiryWarningWindow {
		return 0, false
	}
	return remaining, true
}

// tokenExpiryNextCheck schedules the SSE loop's next expiry inspection. A
// far-away token wakes the loop again at the WARNING BOUNDARY itself, rather
// than only at the repeating-reminder cadence; that is what keeps the first
// signal aligned to "30 minutes before expiry". Invalid claims stay quiet and
// use the ordinary cadence solely to avoid hot-looping synthetic requests.
func tokenExpiryNextCheck(claims map[string]any, now int64) int64 {
	remaining, ok := tokenExpiryRemaining(claims, now)
	if !ok {
		return now + tokenExpiryReminderInterval
	}
	if remaining > tokenExpiryWarningWindow {
		return now + (remaining - tokenExpiryWarningWindow)
	}
	return now + tokenExpiryReminderInterval
}

// decideTokenExpirySignal decides one scheduled token-expiry reminder. It is
// intentionally separate from context-high: unlike a gauge, a token's expiry
// cannot recover, so it repeats every interval until the old session goes away.
//
// Wardens are excluded: their credential lifecycle is a machine governance
// concern, and they cannot use restart_self. A row already in handover is also
// excluded; its replacement is already being minted, so repeating a request to
// restart would only distract it. The minimum-liveness guard is the exact same
// one restart_self enforces, preventing a warning that can only yield 429.
func decideTokenExpirySignal(
	agentID string, claims map[string]any, member *Member, gauge map[string]any,
	now int64, lastReminder int64,
) (*tokenExpirySignal, int64) {
	if member == nil || member.Kind == KindWarden || member.RefocusSince > 0 {
		return nil, lastReminder
	}
	if bootStormTripped(gaugeSecsSinceBoot(gauge, float64(now)), minSelfRestartSecs) {
		return nil, lastReminder
	}
	remaining, ok := tokenExpiryClaims(claims, now)
	if !ok {
		return nil, lastReminder
	}
	if lastReminder > 0 && now-lastReminder < tokenExpiryReminderInterval {
		return nil, lastReminder
	}
	return &tokenExpirySignal{
		Topic:     tokenExpiryTopic,
		To:        agentID,
		ExpiresIn: remaining,
		Reason:    fmt.Sprintf("agent token expires in %ds; checkpoint this turn, then call restart_self to receive a fresh token", remaining),
	}, now
}

// directedFrameText wraps a directed band payload in the shared
// {"topic": ..., "data": ...} envelope as a bare data: event — NO id: line
// (not part of the replayable delta stream; spec §6/§7).
func directedFrameText(topic string, data any) ([]byte, error) {
	inner, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
	}{Topic: topic, Data: inner})
	if err != nil {
		return nil, err
	}
	return []byte("data: " + string(raw) + "\n\n"), nil
}

// ── task-close nudge band (§8): learnings write-back reminder ────────────────

const taskCloseTopic = "task-close"

// taskCloseSignal is the inner directed payload {topic,to,task_id,task_no,
// type,status,reason} (the envelope duplicates topic, exactly like §6).
type taskCloseSignal struct {
	Topic  string `json:"topic"`
	To     string `json:"to"`
	TaskID string `json:"task_id"`
	TaskNo string `json:"task_no"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// decideTaskCloseNudge is the pure band decision, evaluated when a task lands
// in a terminal status (closeTask — done AND terminated both count: a
// terminated task's executor has lessons worth folding back too). The
// reminder walks the WHOLE §6.3 close-out: an anchor-addressed learnings patch, scratch
// cleanup, then report_task_closeout. nil = stay quiet:
//   - a DUPLICATED task carries no lessons (T-02c9 point 6): it is a duplicate
//     of another ticket, so there is nothing to fold back into the manual;
//   - an AD-HOC task (no type) has no manual to write learnings into;
//   - an unassigned task has nobody to remind.
//
// `manualLabel` is the type's human-facing label (manualDisplayLabel — the
// display name with the key in parentheses, or the bare key): the SENTENCE
// shows the human face, but the MCP ADDRESSING string stays the raw type_key
// (T-fa76 — the agent must call patch_task_learnings/get_task_manual by key,
// never by display name).
//
// Delivery is best-effort at-most-once down the executor's own live SSE
// connection (hub.PushDirected) — an offline executor simply misses the
// reminder; the learnings patch stays reachable through the seed SOP.
func decideTaskCloseNudge(t Task, manualLabel string) *taskCloseSignal {
	if !TaskIsTerminal(t.Status) || t.Status == TaskStatusDuplicated ||
		t.TypeKey == "" || t.ExecutorID == "" {
		return nil
	}
	if manualLabel == "" {
		manualLabel = t.TypeKey
	}
	no := TaskNo(t.ID)
	return &taskCloseSignal{
		Topic:  taskCloseTopic,
		To:     t.ExecutorID,
		TaskID: t.ID,
		TaskNo: no,
		Type:   t.TypeKey,
		Status: t.Status,
        Reason: "任務 " + no + " 已結束（" + t.Status + "）。請處理收尾事項：" +
            "若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，再用 patch_task_learnings" +
            "（type_key=`" + t.TypeKey + "`）以唯一錨點只修改「" + manualLabel +
            "」的任務手冊中這次新增或修正的學習經驗；不要用 write_task_learnings 做整份取代，因為讀取後到寫入間別人新增的內容會被無聲蓋掉，錨點若已移動則 patch 會回錯；" +
            "用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、" +
			"收掉臨時 branch/worktree 與跑著的臨時程序；" +
			"最後用 report_task_closeout 回報後續已處理完。",
	}
}

// ── warden-command band: frame + the event-driven START producer (§7) ───────

const wardenCommandTopic = "warden-command"

// wardenStartArgs is the START rpc args shape (spec §7): blank effort/model/
// session_name mean warden defaults; session_name is always "" today.
type wardenStartArgs struct {
	MemberID       string `json:"member_id"`
	PersonaContext string `json:"persona_context"`
	MemberToken    string `json:"member_token"`
	Role           string `json:"role"`
	TaskType       string `json:"task_type"`
	Runtime        string `json:"runtime"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
	SessionName    string `json:"session_name"`
}

// wardenCommandFrame is the {rpc, args} command riding the topic envelope.
type wardenCommandFrame struct {
	RPC  string `json:"rpc"`
	Args any    `json:"args"`
}

// wardenCommandDigest is the read-back SUBSET of a built command frame: which
// verb, and which member it addressed. Deliberately its own decode shape rather
// than a round-trip of wardenCommandFrame (whose Args is `any` for the encode
// side) — it names ONLY the two non-secret fields, so accounting that reads a
// frame back can never touch, log or leak the member_token riding a START.
type wardenCommandDigest struct {
	Verb     string
	MemberID string
}

// decodeWardenCommandFrame parses one warden-command wire frame back into its
// digest. Returns false for anything that is not a well-formed frame on this
// topic — the caller must treat that as "a loss it cannot attribute", never as
// "no loss".
func decodeWardenCommandFrame(frame []byte) (wardenCommandDigest, bool) {
	raw := bytes.TrimPrefix(bytes.TrimSpace(frame), []byte("data: "))
	var env struct {
		Topic string `json:"topic"`
		Data  struct {
			RPC  string `json:"rpc"`
			Args struct {
				MemberID string `json:"member_id"`
			} `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Topic != wardenCommandTopic {
		return wardenCommandDigest{}, false
	}
	return wardenCommandDigest{Verb: env.Data.RPC, MemberID: env.Data.Args.MemberID}, true
}
