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
//   * task-close (§8): RETIRED AS A BAND (T-91) — the terminal-task nudge is a
//     DURABLE CHAT ROW now (closeTask → postTaskChat), not an SSE frame. The
//     DECISION still lives in this file (decideTaskCloseNudge) because that is
//     where its siblings live and where it is cheapest to test; nothing here
//     pushes it. See spec/sse.md §8 for why it moved.

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// ⚠️ `notice` is a closure so a tick that decides to stay QUIET never pays
// for it — and that is ALL the closure buys. It does NOT make it
// once-per-session: this function keeps no state, so once an agent is past its
// notice point it returns non-nil on EVERY tick and runs the closure on every
// one of them, until the session ends. Two comments here used to claim the
// opposite ("must not run on the idle path of every connection to serve a frame
// that fires once per session"); an independent review measured the tick and
// found the claim false. What actually bounds the cost is the CALLER refusing
// to call this once the session's one notice is spent — api_infra.go's
// handoverNoticeTick asks handoverNoticeSettled first, and
// TestHandoverNoticeTick_ClosureIsNotRunAfterTheClaim counts the calls.
func decideHandoverNotice(
	agentID, runtime string, record map[string]any,
	cfg SseContextHighConfig, codexNoticeRound, codexThreshold int,
	notice func() string,
) *contextHighSignal {
	pct := actionableContextPct(record, cfg.StaleGuard)
	// 🔴 THE TWO ARMS DECIDE WHETHER TO SPEAK — THEY NO LONGER COMPOSE ANYTHING
	// (T-6f44, owner's decision 4). Each used to build a position clause
	// (「compaction round 3 (your limits: round 3 / round 4)」,「context 55%
	// (your limits: 55% / 65%)」) and hand it to the notice closure to be pasted
	// into the sentence. That clause is gone from both documents, so composing
	// it here was work whose only remaining consumer discarded it.
	//
	// The GATES are untouched and are the whole reason this switch survives:
	// which axis a runtime is judged on — compaction rounds for codex, context
	// pct for everything else — is a real difference and still decides silence.
	switch {
	case NormalizeRuntime(runtime) == RuntimeCodex:
		if !codexNoticeDue(record, pct, codexNoticeRound, codexThreshold) {
			return nil
		}
	default:
		if cfg.HandoverPct <= 0 || cfg.NoticePct <= 0 ||
			pct == nil || *pct < float64(cfg.NoticePct) {
			return nil
		}
	}
	// Read only once THIS tick has decided to speak — a quiet tick (below the
	// notice point / wrong compaction round) pays nothing. It is NOT read once
	// per session: every tick from here to the end of the session reaches this
	// line, which is why the caller short-circuits before entering at all.
	//
	// 🔴 THE SOFT DOCUMENT, and this is the whole of the choice: the FIRST
	// context threshold is an advance warning with no clock behind it — nothing
	// collects this session at a named instant — so it reads 〈停止〉 rather
	// than 加速停止. The second threshold is the one that carries a deadline,
	// and it arrives through the member delta, not through this band.
	//
	// A notice that could not be rendered means this tick stays QUIET. Sending
	// the frame with an empty reason would spend the session's one notice on a
	// message that says nothing.
	var reason string
	if notice != nil {
		reason = notice()
	}
	if reason == "" {
		return nil
	}
	return &contextHighSignal{
		Topic:  contextHighTopic,
		To:     agentID,
		Level:  levelWarn,
		Pct:    jsonFloat(*pct),
		Reason: reason,
	}
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

// ── task-close nudge (§8): the terminal-task notice ─────────────────────────
//
// 🔴 NO LONGER A BAND. The decision stays here; the DELIVERY is a durable chat
// row written by closeTask. taskCloseTopic survives as the payload's self-label
// (and as the name conformance and spec §8 still use for this notice), not as a
// topic anything publishes.

const taskCloseTopic = "task-close"

// taskCloseSignal is the close-out nudge's decided payload: who it is owed to
// and about which task.
//
// 🔴 IT IS NO LONGER AN SSE FRAME (T-91). The nudge used to be pushed down the
// executor's own live connection and nowhere else; the previous version of the
// comment below said so plainly — "an offline executor simply misses the
// reminder" — which made the ONE notice about a task's death the only lifecycle
// notice in the system with no durable copy. It is now a durable chat row, so
// the executor reads it at its next wake whether or not it was connected when
// the task closed. Topic/To/TaskID/TaskNo/Type/Status are kept because they are
// the decision's facts; Reason carries the rendered document text.
type taskCloseSignal struct {
	Topic  string `json:"topic"`
	To     string `json:"to"`
	TaskID string `json:"task_id"`
	TaskNo string `json:"task_no"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	// ClosedBy is the verified trigger of the write that closed the task — the
	// owner, an admin agent, the executor itself, or "boot-reconcile" when the
	// reconciler closed an all-done task with no caller present. There was no
	// such field at all before T-91: the notice said a task ended and gave the
	// recipient no way to tell its own last step report from somebody else
	// terminating the work under it, which are opposite situations.
	ClosedBy string `json:"closed_by"`
}

// decideTaskCloseNudge is the pure band DECISION — whether a nudge is owed and
// how it is addressed. It no longer composes the sentence: the words live in
// the 〈任務收尾〉 document and are folded in at the send site (T-7870), the same
// road the other nine lifecycle documents take. Evaluated when a task lands in
// a terminal status (closeTask — done AND terminated both count: a terminated
// task's executor still has a close-out to walk). nil = stay quiet, and
// there is now exactly ONE reason left:
//   - an unassigned task has nobody to remind. That is a fact about ADDRESSING,
//     not a judgement about whether the news matters.
//
// 🔴 TWO GATES WERE REMOVED (T-91, owner ruling), AND THE REASONING THAT PUT
// THEM THERE WAS SOUND — about a different question. Both asked "does this task
// have anything worth folding into a manual?": a DUPLICATED task is a
// duplicate of another ticket (T-02c9 point 6), and an AD-HOC task has no
// manual to fold into. Both true, and both beside the point once you ask what
// the recipient actually loses by not being told: its ticket is CLOSED, so
// every write it makes from here on is a 409. Filtering by "is there a manual"
// silenced exactly the two shapes where the close is most likely to have been
// somebody ELSE's decision rather than the executor's own last step report.
//
// 🔴 WHY THE SENTENCE LEFT THIS FUNCTION. Being pure was the named reason this
// one document never got wired: with no *apiServer there is no overlay to fold.
// That was true of the FUNCTION and false of the PATH — closeTask is a method
// and already reads the manual two lines above the call. So the decision stays
// here (pure, cheaply testable) and the text is fetched by the caller, which is
// exactly the split winddownNoticeText already uses. The Reason field is left
// EMPTY here on purpose: a default sentence composed here would be a second
// source of truth for the same words, which is the drift T-7870 exists to end.
//
// 🔴 DELIVERY IS NO LONGER BEST-EFFORT SSE. This comment used to end "an
// offline executor simply misses the reminder", and that sentence was the whole
// defect: the reminder was pushed down the executor's own live connection with
// no durable copy and no replay, so whether an agent ever learned its task had
// been closed depended on whether it happened to be connected at that instant —
// and an agent that has just been stopped, or a worker minted afterwards, never
// is. closeTask now writes it as a DURABLE chat row (postTaskChat), which is
// the same persistence the dependency-release notice already uses and the same
// row a wake snapshot folds in. The delivery guarantee is therefore "readable
// at the recipient's next wake", not "delivered if connected".
//
// 🔴 WHY THIS ONE IS A MESSAGE WHILE THE BLOCKER NOTICE (T-91's other half) IS
// NOT. 開機盤點 lists tasks that have NOT ended. A closed task is absent from it
// by construction, so "write it on the ticket" — the answer the owner chose for
// the blocking side — cannot reach anybody here: there is no ticket in the list
// to read it off.
func decideTaskCloseNudge(t Task) *taskCloseSignal {
	if !TaskIsTerminal(t.Status) || t.ExecutorID == "" {
		return nil
	}
	return &taskCloseSignal{
		Topic:  taskCloseTopic,
		To:     t.ExecutorID,
		TaskID: t.ID,
		TaskNo: TaskNo(t.ID),
		Type:   t.TypeKey,
		Status: t.Status,
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
