package main

// sse_bands.go — the two DIRECTED SSE bands (spec/sse.md §6/§7), ported from
// the retired Python service/sse/{context_high,warden_command}.py:
//
//   * context-high (§6): the server watches each agent's context_pct gauge
//     and pushes a directed "start converging" WARN reminder down the agent's
//     OWN connection. Pure decision functions (band / per-bucket dedup /
//     stale-pct guard) — the stream loop in api_infra.go calls the composed
//     decideContextHighSignal each quiet tick. The reminder is deduped per
//     remind-bucket (T-7826): one WARN per stepPct-wide gauge slice, re-firing
//     only on a climb into a higher bucket — never the old per-cooldown-tick
//     re-remind that bombarded a gauge parked just over the threshold. Only
//     WARN ever emits on the wire; the HANDOVER band belongs to the producer
//     auto-recycle (reconcile.go stampContextHighRecycle).
//
//   * warden-command (§7): the directed frame envelope + the wire arg shapes.
//     The producer that decides + dispatches these frames (cadence tick,
//     event-driven click seams, grace clocks, reconcile store) lives in
//     reconcile.go.

import (
	"encoding/json"
	"fmt"
	"math"
)

// ── context-high band (service/sse/context_high.py) ─────────────────────────

const (
	contextHighTopic = "context-high"

	levelNone = "none"
	levelWarn = "warn"
	// levelHandover is decided but NEVER emitted on the wire (spec §6): the
	// >= handover response is the server-side producer auto-recycle (step ⑥).
	levelHandover = "handover"

	// bucketReset is the per-connection "no bucket reminded yet" marker — the
	// state a connection carries below WARN (and at boot). Any real bucket
	// (>= 0) is strictly greater, so the first climb into the band emits.
	bucketReset = -1
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
// <= 0 disables that band (kill-switch); handover wins when both match.
func bandFor(pct *float64, warnPct, handoverPct int) string {
	if pct == nil {
		return levelNone
	}
	if handoverPct > 0 && *pct >= float64(handoverPct) {
		return levelHandover
	}
	if warnPct > 0 && *pct >= float64(warnPct) {
		return levelWarn
	}
	return levelNone
}

// bucketFor maps an actionable gauge pct to its remind-bucket index — a
// stepPct-wide slice of the gauge (e.g. step 5 → 40–44 is one bucket, 45–49 the
// next). This is the dedup key: one reminder per bucket. A step <= 0 falls back
// to 1 (every 1% its own bucket) so we never divide by zero.
func bucketFor(pct float64, stepPct int) int {
	if stepPct <= 0 {
		stepPct = 1
	}
	return int(math.Floor(pct / float64(stepPct)))
}

// advanceContext advances the per-connection band state for ONE poll tick
// (pure). The reminder is DEDUPED per remind-bucket (T-7826): a WARN fires when
// the gauge first enters the band, and again ONLY when it climbs into a HIGHER
// bucket than the one last reminded. Sitting in the same bucket — the
// 40→41→42 quiet-tick drift that used to re-fire every cooldown and bombard the
// agent — stays silent. A drop to a LOWER bucket re-arms it (lower the marker,
// no emit) so a later re-climb reminds again; dropping below WARN fully resets.
// HANDOVER advances the marker but never emits on the wire (spec §6).
func advanceContext(
	pct *float64, lastBucket int,
	warnPct, handoverPct, stepPct int,
) (emit bool, level string, newBucket int) {
	band := bandFor(pct, warnPct, handoverPct)
	if band == levelNone {
		return false, levelNone, bucketReset
	}
	bucket := bucketFor(*pct, stepPct)
	switch {
	case bucket > lastBucket:
		// A new, higher bucket than anything reminded in this in-band run: the
		// one condition that (re)emits — WARN on the wire, HANDOVER stays quiet.
		return band == levelWarn, band, bucket
	case bucket < lastBucket:
		// 降檔 within the band: re-arm this lower bucket (no emit) so climbing
		// back up into a higher bucket reminds again.
		return false, band, bucket
	}
	// Same bucket: already reminded — stay silent, hold the marker.
	return false, band, lastBucket
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

// decideContextHighSignal composes the whole per-tick decision: gauge record →
// actionable pct → band advance → the WARN signal (or nil to stay quiet).
// Returns the carry-forward remind-bucket marker. Fail-safe by construction.
func decideContextHighSignal(
	agentID string, record map[string]any,
	lastBucket int,
	cfg SseContextHighConfig,
) (*contextHighSignal, int) {
	pct := actionableContextPct(record, cfg.StaleGuard)
	emit, _, newBucket := advanceContext(
		pct, lastBucket,
		cfg.WarnPct, cfg.HandoverPct, cfg.RemindStepPct,
	)
	if !emit {
		// Not a fresh WARN bucket (same/lower bucket, below band, or a HANDOVER
		// tick — recycle is server-driven, step ⑥, never an SSE emit): stay
		// quiet on the wire but keep the bucket marker advancing.
		return nil, newBucket
	}
	return &contextHighSignal{
		Topic: contextHighTopic,
		To:    agentID,
		Level: levelWarn,
		Pct:   jsonFloat(*pct),
		Reason: fmt.Sprintf(
			"context %v%% — start converging; flush in-flight state to durable "+
				"stores (task notes, worklist memory) before you fill up",
			formatPct(*pct)),
	}, newBucket
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
// reminder walks the WHOLE §6.3 close-out: learnings write-back, scratch
// cleanup, then report_task_closeout. nil = stay quiet:
//   - a DUPLICATED task carries no lessons (T-02c9 point 6): it is a duplicate
//     of another ticket, so there is nothing to fold back into the manual;
//   - an AD-HOC task (no type) has no manual to write learnings into;
//   - an unassigned task has nobody to remind.
//
// `manualLabel` is the type's human-facing label (manualDisplayLabel — the
// display name with the key in parentheses, or the bare key): the SENTENCE
// shows the human face, but the MCP ADDRESSING string stays the raw type_key
// (T-fa76 — the agent must call write_task_learnings/get_task_manual by key,
// never by display name).
//
// Delivery is best-effort at-most-once down the executor's own live SSE
// connection (hub.PushDirected) — an offline executor simply misses the
// reminder; the learnings write-back stays reachable through the seed SOP.
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
		Reason: "任務 " + no + " 已結束（" + t.Status + "）。請處理結束後續：" +
			"若這一趟有值得留下的經驗（踩坑、更好做法），用 write_task_learnings" +
			"（type_key=`" + t.TypeKey + "`）整併回「" + manualLabel +
			"」的任務手冊（先 get_task_manual 讀現況、同主題合併後整份寫回）；" +
			"清掉這個任務的暫存資料／程序；最後用 report_task_closeout 回報後續已處理完。",
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
	Model          string `json:"model"`
	Effort         string `json:"effort"`
	SessionName    string `json:"session_name"`
}

// wardenCommandFrame is the {rpc, args} command riding the topic envelope.
type wardenCommandFrame struct {
	RPC  string `json:"rpc"`
	Args any    `json:"args"`
}
