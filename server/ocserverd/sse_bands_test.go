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
	const steps = "# 下線程序\n1. report_stopping()\n2. post_chat 交接"
	doc := func() string { return steps }
	rec := func(pct float64, extra map[string]any) map[string]any {
		r := map[string]any{"context_pct": pct, "context_pct_ts": 20.0, "boot_ts": 10.0}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}
	notice := func(runtime string, record map[string]any, c SseContextHighConfig) *contextHighSignal {
		return decideHandoverNotice("m-1", runtime, record, c, 5, 6, doc, nil)
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
		// Both of the owner's numbers, and where this session is right now: an
		// agent cannot read its own context %, so a notice without them leaves
		// it unable to pace itself.
		if !strings.Contains(sig.Reason, "context 65% (your limits: 65% / 75%)") {
			t.Fatalf("the notice must carry the pct and both limits: %q", sig.Reason)
		}
		// The instruction, asserted on BOTH halves: dropping either one is a
		// different bug (idle until cut off / stop mid-work) and both are red.
		if !strings.Contains(sig.Reason, "work the sequence below") {
			t.Fatalf("the notice must point at the steps: %q", sig.Reason)
		}
		if !strings.Contains(sig.Reason, "then call restart_self yourself") {
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

	t.Run("the notice survives a document that cannot be read", func(t *testing.T) {
		sig := decideHandoverNotice("m-1", RuntimeClaude, rec(65, nil), cfg, 5, 6,
			func() string { return "" }, nil)
		if sig == nil {
			t.Fatal("losing the checklist must not lose the notice")
		}
		if !strings.Contains(sig.Reason, "offboard now") {
			t.Fatalf("the sentence must still be there: %q", sig.Reason)
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
		if !strings.Contains(sig.Reason, "compaction round 5 (your limits: round 5 / round 6)") {
			t.Fatalf("a codex notice must name ITS axis (rounds), not a pct: %q", sig.Reason)
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
	member := &Member{ID: "m-expiry", Kind: KindAssistant}

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
	want := []string{"member_id", "persona_context", "member_token", "role",
		"task_type", "runtime", "model", "effort", "session_name"}
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
		sig := decideTaskCloseNudge(task, "審查 PR（review-pr）")
		if sig == nil {
			t.Fatalf("%s must nudge", status)
		}
		if sig.Topic != taskCloseTopic || sig.To != "m-exec" ||
			sig.TaskID != task.ID || sig.TaskNo != "T-7d40" ||
			sig.Type != "review-pr" || sig.Status != status {
			t.Fatalf("%s signal fields: %+v", status, sig)
		}
		wantReason := "任務 T-7d40 已結束（" + status + "）。請處理收尾事項：" +
			"若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，再用 patch_task_learnings" +
			"（type_key=`review-pr`）只把改動的那一段送回「審查 PR（review-pr）」的任務手冊：改既有段落就用它的唯一錨點，第一次寫或要新增就用空錨點追加。不要用 write_task_learnings 做整份取代 —— 讀取後到寫入之間別人新增的內容會被無聲蓋掉；" +
			"用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、" +
			"收掉臨時 branch/worktree 與跑著的臨時程序；" +
			"最後用 report_task_closeout 回報後續已處理完。"
		if sig.Reason != wantReason {
			t.Fatalf("reason changed: got %q, want %q", sig.Reason, wantReason)
		}
	}

	// A blank label (manual deleted / lookup failed) falls back to the key.
	fallback := base
	fallback.Status = TaskStatusDone
	wantFallbackReason := "任務 T-7d40 已結束（done）。請處理收尾事項：" +
		"若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，再用 patch_task_learnings" +
		"（type_key=`review-pr`）只把改動的那一段送回「review-pr」的任務手冊：改既有段落就用它的唯一錨點，第一次寫或要新增就用空錨點追加。不要用 write_task_learnings 做整份取代 —— 讀取後到寫入之間別人新增的內容會被無聲蓋掉；" +
		"用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、" +
		"收掉臨時 branch/worktree 與跑著的臨時程序；" +
		"最後用 report_task_closeout 回報後續已處理完。"
	if sig := decideTaskCloseNudge(fallback, ""); sig == nil || sig.Reason != wantFallbackReason {
		t.Fatalf("blank label reason changed: got %+v, want %q", sig, wantFallbackReason)
	}

	// An ad-hoc task (no type) has no manual to write into — never nudges.
	adhoc := base
	adhoc.Status = TaskStatusDone
	adhoc.TypeKey = ""
	if decideTaskCloseNudge(adhoc, "") != nil {
		t.Fatal("ad-hoc close must stay quiet")
	}

	// A non-terminal status never nudges.
	open := base
	open.Status = TaskStatusInProgress
	if decideTaskCloseNudge(open, "") != nil {
		t.Fatal("non-terminal status must stay quiet")
	}

	// An unassigned task has nobody to remind.
	unassigned := base
	unassigned.Status = TaskStatusTerminated
	unassigned.ExecutorID = ""
	if decideTaskCloseNudge(unassigned, "") != nil {
		t.Fatal("unassigned close must stay quiet")
	}
}

func TestTaskCloseFrameIsABareDirectedEvent(t *testing.T) {
	task := Task{ID: "t-7d40aabbccdd", TypeKey: "review-pr",
		ExecutorID: "m-exec", Status: TaskStatusDone}
	frame, err := directedFrameText(taskCloseTopic, decideTaskCloseNudge(task, ""))
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
