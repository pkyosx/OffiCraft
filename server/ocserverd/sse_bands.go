package main

// sse_bands.go — decisions for the DIRECTED SSE frames (spec/sse.md §6–§8):
//
//   * context-high (§6): one advance notice before an agent's handover;
//     api_infra.go's stream loop calls decideHandoverNotice each quiet tick.
//     The handover itself is the producer auto-recycle (reconcile.go
//     stampContextHighRecycle) and never goes on the wire.
//   * token-expiry (§6.1): repeatedly asks a restartable agent to call
//     restart_self before its JWT exp.
//   * warden-command (§7): frame envelope + arg shapes; the producer lives in
//     reconcile.go.
//   * task-close (§8): no longer a frame — closeTask delivers it as a durable
//     chat row (postTaskChat); only the decision lives here.

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	contextHighTopic = "context-high"
	tokenExpiryTopic = "token-expiry"

	levelNone = "none"
	levelWarn = "warn"

	levelHandover = "handover"
)

const (
	// handoverNoticeLeadPct: when the DB has no ctx.notice_pct row (fresh installs
	// included), the notice point is recomputed as handover minus this lead on every
	// load and never written back; once notice_pct is stored, that value wins.
	handoverNoticeLeadPct = 10
	// codexNoticeRoundPct: a codex session hands over on COMPACTION COUNT, so
	// its notice is one round before the last, at 60% through that round
	// (owner 2026-08-16).
	codexNoticeRoundPct = 60
)

const (
	// tokenExpiryWarningWindow is owner-set policy: thirty minutes to
	// checkpoint and request restart_self.
	tokenExpiryWarningWindow = 30 * 60 // seconds
	// The warning repeats: one SSE frame is no proof it was read; only a
	// replacement session (or expiry) settles it.
	tokenExpiryReminderInterval = 30 // seconds
)

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

func bandFor(pct *float64, handoverPct int) string {
	if pct != nil && handoverPct > 0 && *pct >= float64(handoverPct) {
		return levelHandover
	}
	return levelNone
}

// claudeNoticePct is NOT part of the live decision (the notice point is an
// owner setting now). Its one caller is the upgrade path (settings.go), which
// fills notice_pct for installs that predate the pair. Do not wire it back
// into the band.
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

// noticeRound is the owner's setting; <= 0 falls back to threshold-1 so a
// config that predates it behaves as before.
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

// boot_ts is the SSE-connect boot anchor that the stale-pct guard, the
// boot-storm guard and the worker refocus loop-break all read.
func gaugeBootTS(record map[string]any) (float64, bool) {
	if record == nil {
		return 0, false
	}
	return asNumber(record["boot_ts"])
}

// gaugeSecsSinceBoot is shared by every bootStormTripped caller
// (reconcile.stampContextHighRecycle, HandleRestartSelf)
// so they cannot drift. nil when there is no usable boot_ts (e.g.
// server-restart amnesia), so the guard FAILS OPEN, never a false trip.
func gaugeSecsSinceBoot(record map[string]any, now float64) *float64 {
	bootTS, ok := gaugeBootTS(record)
	if !ok {
		return nil
	}
	secs := now - bootTS
	return &secs
}

// With the stale guard on, a pct counts only when reported strictly after
// this connection's boot_ts, so a predecessor session's leftover pct never
// triggers (spec §6).
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

type contextHighSignal struct {
	Topic  string    `json:"topic"`
	To     string    `json:"to"`
	Level  string    `json:"level"`
	Pct    jsonFloat `json:"pct"`
	Reason string    `json:"reason"`
}

// decideHandoverNotice is pure and keeps no state: past the notice point it
// returns non-nil — and runs `notice` — on EVERY tick (measured). Firing once
// per SESSION is the caller's job: api_infra.go's handoverNoticeTick asks
// handoverNoticeSettled first.
func decideHandoverNotice(
	agentID, runtime string, record map[string]any,
	cfg SseContextHighConfig, codexNoticeRound, codexThreshold int,
	notice func() string,
) *contextHighSignal {
	pct := actionableContextPct(record, cfg.StaleGuard)
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
	// A notice that could not be rendered keeps this tick QUIET: an empty
	// reason would spend the session's one notice on a message that says
	// nothing.
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

func formatPct(pct float64) any {
	if pct == float64(int64(pct)) {
		return int64(pct)
	}
	return pct
}

type tokenExpirySignal struct {
	Topic     string `json:"topic"`
	To        string `json:"to"`
	ExpiresIn int64  `json:"expires_in"`
	Reason    string `json:"reason"`
}

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

func tokenExpiryClaims(claims map[string]any, now int64) (int64, bool) {
	remaining, ok := tokenExpiryRemaining(claims, now)
	if !ok || remaining > tokenExpiryWarningWindow {
		return 0, false
	}
	return remaining, true
}

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

// Skips wardens (they cannot restart_self), a member already in handover (its
// replacement is being minted), and a session younger than restart_self's own
// min-liveness guard (the call could only 429).
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

// No id: line — directed frames are not part of the replayable delta stream
// (spec §6/§7).
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

// task-close: taskCloseTopic survives only as the payload's self-label (and
// the name conformance and spec §8 still use); nothing publishes it.

const taskCloseTopic = "task-close"

type taskCloseSignal struct {
	Topic  string `json:"topic"`
	To     string `json:"to"`
	TaskID string `json:"task_id"`
	TaskNo string `json:"task_no"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	// ClosedBy is the verified trigger of the closing write: the owner, an
	// admin agent, or the executor itself.
	ClosedBy string `json:"closed_by"`
}

// decideTaskCloseNudge decides only whether a nudge is owed and to whom.
// Reason stays EMPTY on purpose: the words live in the 〈任務收尾〉 document and
// are folded in at the send site (closeTask); a default sentence here would
// be a second source of truth. Duplicated and ad-hoc tasks are deliberately
// NOT filtered (owner ruling, T-91).
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

const wardenCommandTopic = "warden-command"

// Blank effort/model/session_name mean warden defaults.
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

type wardenCommandFrame struct {
	RPC  string `json:"rpc"`
	Args any    `json:"args"`
}

// wardenCommandDigest names ONLY the two non-secret fields, so accounting
// that reads a frame back can never touch, log or leak a START's
// member_token.
type wardenCommandDigest struct {
	Verb     string
	MemberID string
}

// false must be treated as "a loss the caller cannot attribute", never as "no
// loss".
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
