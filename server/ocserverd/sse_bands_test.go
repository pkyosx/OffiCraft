package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func fptr(v float64) *float64 { return &v }

func TestBandFor(t *testing.T) {
	cases := []struct {
		name     string
		pct      *float64
		handover int
		want     string
	}{
		{"nil pct fails safe to none", nil, 50, levelNone},
		{"below handover", fptr(49), 50, levelNone},
		{"at handover", fptr(50), 50, levelHandover},
		{"above handover", fptr(99), 50, levelHandover},
		{"threshold <= 0 disables the band", fptr(99), 0, levelNone},
	}
	for _, c := range cases {
		if got := bandFor(c.pct, c.handover); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestClaudeNoticePct_IsTheUpgradeFallback pins what this derivation is FOR
// since T-a9d6: the notice point is now the owner's own setting, and this
// function survives only to fill it in for an install that predates the pair.
// Getting it wrong would silently move every upgraded deployment's notice.
// Every assertion is an ABSOLUTE number on purpose: one written against the
// constant would stay green through exactly the drift being guarded.
func TestClaudeNoticePct_IsTheUpgradeFallback(t *testing.T) {
	// The owner's own worked example, verbatim: 「例如 65% 的話會從 55% 開始通知」.
	if got, ok := claudeNoticePct(65); !ok || got != 55 {
		t.Fatalf("handover 65 must notify at 55, got %d (ok=%v)", got, ok)
	}
	// Move the threshold: the notice MUST move with it. This is the pair that
	// dies if anyone re-hardwires the notice point.
	if got, ok := claudeNoticePct(90); !ok || got != 80 {
		t.Fatalf("handover 90 must notify at 80, got %d (ok=%v)", got, ok)
	}
	if got, ok := claudeNoticePct(40); !ok || got != 30 {
		t.Fatalf("handover 40 (the UI minimum) must notify at 30, got %d (ok=%v)", got, ok)
	}
	// Kill-switch and degenerate leads produce NO notice rather than one that
	// fires on a barely-used gauge.
	if _, ok := claudeNoticePct(0); ok {
		t.Fatal("a disabled handover band must produce no advance notice")
	}
	if _, ok := claudeNoticePct(handoverNoticeLeadPct); ok {
		t.Fatal("a lead that lands the notice at 0% must produce no notice")
	}
}

// TestCodexNoticeDue pins the OTHER runtime's axis. A codex session hands over
// on compaction count, so a percentage threshold means nothing to it — that was
// the second half of the bug (a codex worker was being warned at 40% of a gauge
// that does not decide anything for it).
func TestCodexNoticeDue(t *testing.T) {
	rec := func(count int) map[string]any { return map[string]any{"compaction_count": count} }
	// Owner's worked example, verbatim: 「例如我設定是 5 那就是在第四輪的 60% 提醒
	// 一次」 — threshold 5 ⇒ round 4 (count == 4) at >= 60%.
	// The owner's pair, verbatim: 「codex 則是 5 / 6 表示第五輪開始通知，第六輪
	// 120 秒」 — notice on round 5, final on round 6.
	if !codexNoticeDue(rec(5), fptr(60), 5, 6) {
		t.Fatal("pair 5/6: round 5 at 60% must be due")
	}
	if codexNoticeDue(rec(5), fptr(59), 5, 6) {
		t.Fatal("pair 5/6: round 5 below 60% is not due yet")
	}
	// One notice, on ONE round: neither earlier nor on the final round itself,
	// which would arrive after the decision it was warning about.
	if codexNoticeDue(rec(4), fptr(99), 5, 6) {
		t.Fatal("a round before the notice round must stay quiet")
	}
	if codexNoticeDue(rec(6), fptr(99), 5, 6) {
		t.Fatal("the final round itself must not carry the ADVANCE notice")
	}
	// The notice round is the OWNER'S first number, independent of the final
	// one: move only it and the notice moves, and only it.
	if !codexNoticeDue(rec(2), fptr(80), 2, 6) {
		t.Fatal("pair 2/6 must notify on round 2")
	}
	if codexNoticeDue(rec(5), fptr(80), 2, 6) {
		t.Fatal("pair 2/6 must not notify on round 5")
	}
	// A config that predates the pair (no notice round) falls back to the old
	// derivation, so an upgrade notifies exactly where it used to.
	if !codexNoticeDue(rec(4), fptr(60), 0, 5) {
		t.Fatal("no notice round: must fall back to threshold-1")
	}
	// Fail-safe inputs.
	if codexNoticeDue(map[string]any{}, fptr(99), 5, 6) {
		t.Fatal("a gauge with no compaction_count must fail safe to quiet")
	}
	if codexNoticeDue(rec(5), nil, 5, 6) {
		t.Fatal("no actionable pct must fail safe to quiet")
	}
}

func TestActionableContextPct(t *testing.T) {
	fresh := map[string]any{"context_pct": 45.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	if got := actionableContextPct(fresh, true); got == nil || *got != 45.0 {
		t.Fatalf("fresh pct must be actionable: %v", got)
	}
	stale := map[string]any{"context_pct": 45.0, "context_pct_ts": 5.0, "boot_ts": 10.0}
	if got := actionableContextPct(stale, true); got != nil {
		t.Fatalf("a pct reported at/before boot_ts is stale: %v", *got)
	}
	if got := actionableContextPct(stale, false); got == nil || *got != 45.0 {
		t.Fatal("stale_guard=false reverts to always-use-pct")
	}
	noAnchor := map[string]any{"context_pct": 45.0}
	if actionableContextPct(noAnchor, true) != nil {
		t.Fatal("missing freshness anchors must fail safe to nil")
	}
	if actionableContextPct(nil, true) != nil {
		t.Fatal("missing record must fail safe to nil")
	}
}

func TestDecideHandoverNotice(t *testing.T) {
	cfg := defaultSseContextHigh()
	// The owner's pair, verbatim: 「65% / 75% 表示第一次通知會是 65% 第二次通知
	// 會是 75%」.
	cfg.NoticePct, cfg.HandoverPct = 65, 75
	// The REAL producer, so which document this band reads is under test rather
	// than assumed. A closure returning a made-up sentence would go on passing
	// on a server that sent the final call's document to this soft arm.
	srv := newReconcileTestServer(t)
	_, steps, _ := DocSplitHeadBody(mustFoldText(t, srv, srv.offboardSpec()))
	doc := func() string {
		return srv.winddownNoticeText(offboardKindSoft, 0)
	}
	rec := func(pct float64, extra map[string]any) map[string]any {
		r := map[string]any{"context_pct": pct, "context_pct_ts": 20.0, "boot_ts": 10.0}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	notice := func(runtime string, record map[string]any, c SseContextHighConfig) *contextHighSignal {
		return decideHandoverNotice("m-1", runtime, record, c, 5, 6, doc)
	}

	t.Run("claude fires at the owner's first number, not before", func(t *testing.T) {
		if sig := notice(RuntimeClaude, rec(64, nil), cfg); sig != nil {
			t.Fatalf("64%% is below the 65%% notice point: %+v", sig)
		}
		sig := notice(RuntimeClaude, rec(65, nil), cfg)
		if sig == nil {
			t.Fatal("65% with the notice point at 65% must notify")
		}
		if sig.Topic != "context-high" || sig.To != "m-1" || sig.Level != "warn" ||
			float64(sig.Pct) != 65.0 {
			t.Fatalf("signal envelope: %+v", sig)
		}
	})

	// The two numbers must move INDEPENDENTLY. A single derived point would pass
	// the test above and fail this one, which is the whole shape of the change.
	t.Run("the two numbers are independent", func(t *testing.T) {
		earlier := cfg
		earlier.NoticePct = 50
		if sig := notice(RuntimeClaude, rec(50, nil), earlier); sig == nil {
			t.Fatal("moving only the notice point must move where the notice fires")
		}
		if sig := notice(RuntimeClaude, rec(50, nil), cfg); sig != nil {
			t.Fatalf("the unmoved pair must still be quiet at 50%%: %+v", sig)
		}
		laterFinal := cfg
		laterFinal.HandoverPct = 90
		if sig := notice(RuntimeClaude, rec(64, nil), laterFinal); sig != nil {
			t.Fatalf("moving only the FINAL number must not move the notice: %+v", sig)
		}
	})

	t.Run("the notice is the one approved sentence, carrying the document", func(t *testing.T) {
		sig := notice(RuntimeClaude, rec(65, nil), cfg)
		if sig == nil {
			t.Fatal("expected a notice")
		}
		// ⚠️ THE PCT AND THE LIMITS ARE GONE FROM THE NOTICE (T-6f44, decision 4:
		// 「{where} 不中文化，直接砍掉」). This block used to require them, on the
		// argument that an agent cannot read its own context %. The owner's
		// ruling is that knowing 「你在 59%」 has nothing to do with how to close
		// out — the notice's job is to say the close-out has started, and the
		// band it fired in is not an instruction. So the assertion is inverted:
		// nothing the tick composes may reach the agent.
		for _, leak := range []string{"context 65%", "your limits:", "compaction round"} {
			if strings.Contains(sig.Reason, leak) {
				t.Fatalf("the notice carries %q — the position clause was deleted: %q",
					leak, sig.Reason)
			}
		}
		// The instruction, asserted on BOTH halves: dropping either one is a
		// different bug (idle until cut off / stop mid-work) and both are red.
		// They are the DOCUMENT's own words now — the English opener that used
		// to carry them was the read-only head, and it went with {where}.
		if !strings.Contains(sig.Reason, "## 2. 開始下線") {
			t.Fatalf("the notice must point at the steps: %q", sig.Reason)
		}
		if !strings.Contains(sig.Reason, "report_stopped") {
			t.Fatalf("the notice must tell the agent to leave under its own power: %q", sig.Reason)
		}
		// The steps are the DOCUMENT's, verbatim — not a summary written in Go.
		if !strings.Contains(sig.Reason, steps) {
			t.Fatalf("the notice must carry the offboard document: %q", sig.Reason)
		}
		// A SOFT notice has no countdown, and no deadline either. That clause
		// belongs to the final call alone; carrying it here would read as "you
		// are out of time" a full band early. Asserted by SHAPE — a digit
		// attached to a unit of time, or a clock-shaped span — because a
		// whitelist of literals stops guarding at the next rewording.
		assertQuotesNoTime(t, "a context-pressure notice", sig.Reason)
	})

	// 🔴 THIS DIRECTION REVERSED IN T-3201, and the reversal is the point. The
	// sentence used to be built in Go beside the document, so an unreadable
	// document cost the checklist and kept the notice. The sentence IS the
	// document's read-only head now, so there is nothing left to send: the tick
	// stays QUIET rather than spending the session's one notice on an empty
	// reason, and the agent's client falls back on the key being absent.
	t.Run("a document that cannot be rendered keeps the tick quiet", func(t *testing.T) {
		if sig := decideHandoverNotice("m-1", RuntimeClaude, rec(65, nil), cfg, 5, 6,
			func() string { return "" }); sig != nil {
			t.Fatalf("an empty notice must not be sent at all: %+v", sig)
		}
		// …and the same for no closure at all, which is the other way the
		// reason can come back empty.
		if sig := decideHandoverNotice("m-1", RuntimeClaude, rec(65, nil), cfg, 5, 6,
			nil); sig != nil {
			t.Fatalf("no notice source must not send a blank frame: %+v", sig)
		}
	})

	t.Run("codex is judged on rounds, never on the claude percentage", func(t *testing.T) {
		// 65% would fire for claude. For codex on round 2 it must not: its
		// lifecycle has nothing to do with that number.
		quiet := rec(65, map[string]any{"compaction_count": 2})
		if sig := notice(RuntimeCodex, quiet, cfg); sig != nil {
			t.Fatalf("codex must not inherit the claude percentage rule: %+v", sig)
		}
		due := rec(60, map[string]any{"compaction_count": 5})
		sig := notice(RuntimeCodex, due, cfg)
		if sig == nil {
			t.Fatal("codex round 5 of the 5/6 pair at 60% must notify")
		}
		// ⚠️ THE AXIS IS NO LONGER IN THE SENTENCE (T-6f44, decision 4). What this
		// sub-test actually guards — that codex is JUDGED on rounds and never on
		// the claude percentage — is asserted above by the two decideHandoverNotice
		// calls: 65% stays quiet, round 5 of 5/6 fires. That is the whole rule.
		// What is gone is only the notice REPEATING the axis back, and with it the
		// ability to tell a codex notice from a claude one by reading it: both
		// arms send the same document now, which is correct — 〈停止〉 is the same
		// procedure whatever put the session over its line.
		for _, leak := range []string{"compaction round", "your limits:", "%"} {
			if strings.Contains(sig.Reason, leak) {
				t.Fatalf("a codex notice carries %q — the position clause was deleted: %q",
					leak, sig.Reason)
			}
		}
	})

	t.Run("fails safe when the gauge cannot be trusted", func(t *testing.T) {
		stale := map[string]any{"context_pct": 99.0, "context_pct_ts": 5.0, "boot_ts": 10.0}
		if sig := notice(RuntimeClaude, stale, cfg); sig != nil {
			t.Fatalf("a predecessor session's pct must not notify: %+v", sig)
		}
		if sig := notice(RuntimeClaude, nil, cfg); sig != nil {
			t.Fatalf("no gauge must not notify: %+v", sig)
		}
		off := cfg
		off.HandoverPct = 0
		if sig := notice(RuntimeClaude, rec(99, nil), off); sig != nil {
			t.Fatalf("the kill-switch must silence the notice too: %+v", sig)
		}
		noNotice := cfg
		noNotice.NoticePct = 0
		if sig := notice(RuntimeClaude, rec(99, nil), noNotice); sig != nil {
			t.Fatalf("an unset notice point must stay quiet, not fire at once: %+v", sig)
		}
	})
}

func TestDecideTokenExpirySignalRepeatsUntilRestart(t *testing.T) {
	const now int64 = 20_000
	claims := map[string]any{"exp": float64(now + tokenExpiryWarningWindow)}
	oldSession := map[string]any{"boot_ts": float64(now - int64(minSelfRestartSecs) - 1)}
	member := &Member{ID: "m-expiry", Kind: KindStaff}

	signal, last := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0)
	if signal == nil {
		t.Fatal("an eligible agent at the 30-minute boundary must be reminded")
	}
	if signal.Topic != tokenExpiryTopic || signal.To != "m-expiry" ||
		signal.ExpiresIn != tokenExpiryWarningWindow || !strings.Contains(signal.Reason, "restart_self") {
		t.Fatalf("signal = %+v", signal)
	}
	if last != now {
		t.Fatalf("first reminder timestamp = %d, want %d", last, now)
	}
	if signal, repeatedLast := decideTokenExpirySignal(
		"m-expiry", claims, member, oldSession, now+tokenExpiryReminderInterval-1, last); signal != nil || repeatedLast != last {
		t.Fatalf("reminder must stay quiet before cadence: signal=%+v last=%d", signal, repeatedLast)
	}
	if signal, repeatedLast := decideTokenExpirySignal(
		"m-expiry", claims, member, oldSession, now+tokenExpiryReminderInterval, last); signal == nil || repeatedLast != now+tokenExpiryReminderInterval {
		t.Fatalf("unhandled expiry must repeat on cadence: signal=%+v last=%d", signal, repeatedLast)
	}

	far := map[string]any{"exp": float64(now + tokenExpiryWarningWindow + 1)}
	if signal, _ := decideTokenExpirySignal("m-expiry", far, member, oldSession, now, 0); signal != nil {
		t.Fatalf("far-from-expiry token must stay quiet: %+v", signal)
	}
	if got, want := tokenExpiryNextCheck(far, now), now+1; got != want {
		t.Fatalf("far token must recheck at the exact 30-minute boundary: got %d want %d", got, want)
	}
	if got, want := tokenExpiryNextCheck(claims, now), now+tokenExpiryReminderInterval; got != want {
		t.Fatalf("pending token must use reminder cadence: got %d want %d", got, want)
	}
	freshSession := map[string]any{"boot_ts": float64(now - 1)}
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, freshSession, now, 0); signal != nil {
		t.Fatalf("notification must wait until restart_self is usable: %+v", signal)
	}
	member.RefocusSince = float64(now - 1)
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0); signal != nil {
		t.Fatalf("agent already in handover must not be reminded: %+v", signal)
	}
	member.RefocusSince = 0
	member.Kind = KindWarden
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0); signal != nil {
		t.Fatalf("warden must not receive restart_self reminder: %+v", signal)
	}
}

func TestDirectedFrameText(t *testing.T) {
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC:  "start",
		Args: wardenStartArgs{MemberID: "m-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(frame)
	if strings.Contains(text, "id: ") || !strings.HasPrefix(text, "data: ") ||
		!strings.HasSuffix(text, "\n\n") {
		t.Fatalf("directed frames are bare data: events with no id line: %q", text)
	}
	var envelope struct {
		Topic string `json:"topic"`
		Data  struct {
			RPC  string         `json:"rpc"`
			Args map[string]any `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data: "))), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Topic != "warden-command" || envelope.Data.RPC != "start" {
		t.Fatalf("envelope: %+v", envelope)
	}
	// The length check below makes this an EXACT set, not a subset: an extra key
	// is a field the warden was never told about. `task_type` was in this list
	// until T-2 — sourced from the lessons bucket, carried for parity only — and
	// its removal is what this now pins. Its twin lives in
	// conformance/test_sse.py (§7); both have to move together.
	want := []string{"member_id", "persona_context", "member_token", "role",
		"runtime", "model", "effort", "session_name"}
	if len(envelope.Data.Args) != len(want) {
		t.Fatalf("start args keys: %v", envelope.Data.Args)
	}
	for _, k := range want {
		if _, ok := envelope.Data.Args[k]; !ok {
			t.Fatalf("start args missing %q: %v", k, envelope.Data.Args)
		}
	}
}

// ── task-close nudge band (§8) ───────────────────────────────────────────────

func TestDecideTaskCloseNudge(t *testing.T) {
	base := Task{ID: "t-7d40aabbccdd", TypeKey: "review-pr", ExecutorID: "m-exec"}

	// Both terminal statuses nudge (a terminated run's lessons count too).
	for _, status := range []string{TaskStatusDone, TaskStatusTerminated} {
		task := base
		task.Status = status
		sig := decideTaskCloseNudge(task)
		if sig == nil {
			t.Fatalf("%s must nudge", status)
		}
		if sig.Topic != taskCloseTopic || sig.To != "m-exec" ||
			sig.TaskID != task.ID || sig.TaskNo != "t-7d40aabbccdd" ||
			sig.Type != "review-pr" || sig.Status != status {
			t.Fatalf("%s signal fields: %+v", status, sig)
		}
		// 🔴 THE DECISION MUST NOT CARRY WORDS (T-7870). The sentence lives in
		// the 〈任務收尾〉 document and is folded in at the send site; a default
		// composed here would be a second source of truth for the same words —
		// which is precisely how this document spent T-3201 editable, versioned,
		// and unread. An empty Reason is the assertion, not an oversight.
		if sig.Reason != "" {
			t.Fatalf("%s: the decision composed a sentence (%q) — the words belong to "+
				"the document, not to this function", status, sig.Reason)
		}
	}

	// 🔴 REVERSED BY OWNER RULING (T-91). This asserted that an ad-hoc task
	// (no type) never nudges, because there is no manual to write learnings
	// into. The premise is still true and is no longer the question: the
	// notice's job is telling an executor its ticket is CLOSED, which is
	// equally true of a typeless one. Same for a DUPLICATED close, whose gate
	// was removed in the same change.
	for _, silenced := range []Task{
		{ID: "t-adhoc", Status: TaskStatusDone, TypeKey: "", ExecutorID: "m-exec"},
		{ID: "t-dup", Status: TaskStatusDuplicated, TypeKey: "review-pr", ExecutorID: "m-exec"},
	} {
		if decideTaskCloseNudge(silenced) == nil {
			t.Fatalf("%s must nudge: the two gates that used to silence it asked "+
				"whether there were LESSONS, not whether the executor needed to "+
				"know its ticket was closed", silenced.ID)
		}
	}

	// A non-terminal status never nudges.
	open := base
	open.Status = TaskStatusInProgress
	if decideTaskCloseNudge(open) != nil {
		t.Fatal("non-terminal status must stay quiet")
	}

	// An unassigned task has nobody to remind.
	unassigned := base
	unassigned.Status = TaskStatusTerminated
	unassigned.ExecutorID = ""
	if decideTaskCloseNudge(unassigned) != nil {
		t.Fatal("unassigned close must stay quiet")
	}
}

func TestTaskCloseFrameIsABareDirectedEvent(t *testing.T) {
	task := Task{ID: "t-7d40aabbccdd", TypeKey: "review-pr",
		ExecutorID: "m-exec", Status: TaskStatusDone}
	frame, err := directedFrameText(taskCloseTopic, decideTaskCloseNudge(task))
	if err != nil {
		t.Fatal(err)
	}
	text := string(frame)
	if strings.Contains(text, "id: ") || !strings.HasPrefix(text, "data: ") {
		t.Fatalf("directed frames are bare data: events with no id line: %q", text)
	}
	var envelope struct {
		Topic string          `json:"topic"`
		Data  taskCloseSignal `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data: "))), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Topic != "task-close" || envelope.Data.To != "m-exec" ||
		envelope.Data.TaskID != task.ID || envelope.Data.Status != "done" {
		t.Fatalf("envelope: %+v", envelope)
	}
}
