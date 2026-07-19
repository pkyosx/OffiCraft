package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func fptr(v float64) *float64 { return &v }

func TestBandFor(t *testing.T) {
	cases := []struct {
		name           string
		pct            *float64
		warn, handover int
		want           string
	}{
		{"nil pct fails safe to none", nil, 40, 50, levelNone},
		{"below warn", fptr(39), 40, 50, levelNone},
		{"warn band", fptr(45), 40, 50, levelWarn},
		{"handover wins over warn", fptr(50), 40, 50, levelHandover},
		{"warn threshold <= 0 disables the band", fptr(45), 0, 50, levelNone},
		{"handover threshold <= 0 disables the band", fptr(99), 40, 0, levelWarn},
	}
	for _, c := range cases {
		if got := bandFor(c.pct, c.warn, c.handover); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestAdvanceContext(t *testing.T) {
	// step 5: bucket = floor(pct/5). warn 40 → bucket 8; 45 → 9; handover 50 → 10.
	t.Run("first entry into the band emits and marks the bucket", func(t *testing.T) {
		emit, level, bucket := advanceContext(fptr(42), bucketReset, 40, 50, 5)
		if !emit || level != levelWarn || bucket != 8 {
			t.Fatalf("got %v %q %d", emit, level, bucket)
		}
	})
	t.Run("same bucket stays quiet no matter how many ticks", func(t *testing.T) {
		// 40→41→42→44 all live in bucket 8 — the drift that used to bombard.
		for _, pct := range []float64{40, 41, 42, 44} {
			emit, _, bucket := advanceContext(fptr(pct), 8, 40, 50, 5)
			if emit || bucket != 8 {
				t.Fatalf("pct %v: got emit=%v bucket=%d", pct, emit, bucket)
			}
		}
	})
	t.Run("climbing into a higher bucket re-reminds", func(t *testing.T) {
		emit, level, bucket := advanceContext(fptr(45), 8, 40, 50, 5)
		if !emit || level != levelWarn || bucket != 9 {
			t.Fatalf("got %v %q %d", emit, level, bucket)
		}
	})
	t.Run("dropping to a lower bucket re-arms without emitting", func(t *testing.T) {
		// 降檔 from bucket 9 back to 8: no emit, marker lowered so a re-climb fires.
		emit, _, bucket := advanceContext(fptr(42), 9, 40, 50, 5)
		if emit || bucket != 8 {
			t.Fatalf("re-arm: got emit=%v bucket=%d", emit, bucket)
		}
		emit, _, bucket = advanceContext(fptr(46), 8, 40, 50, 5)
		if !emit || bucket != 9 {
			t.Fatalf("re-climb after 降檔 must emit: got emit=%v bucket=%d", emit, bucket)
		}
	})
	t.Run("dropping below warn resets to the sentinel", func(t *testing.T) {
		emit, level, bucket := advanceContext(fptr(10), 9, 40, 50, 5)
		if emit || level != levelNone || bucket != bucketReset {
			t.Fatalf("got %v %q %d", emit, level, bucket)
		}
	})
	t.Run("handover advances the marker but never emits on the wire", func(t *testing.T) {
		emit, level, bucket := advanceContext(fptr(52), 8, 40, 50, 5)
		if emit || level != levelHandover || bucket != 10 {
			t.Fatalf("got %v %q %d", emit, level, bucket)
		}
	})
}

// TestContextHighDedupePerBucket is the T-7826 regression: a gauge parked in one
// 5% bucket across many quiet ticks reminds exactly ONCE (the reported symptom
// was 30+ reminders as the gauge drifted 40→41→42 over a few minutes). Climbing
// into the next bucket reminds again; recovery below warn then re-climb reminds.
func TestContextHighDedupePerBucket(t *testing.T) {
	cfg := defaultSseContextHigh()
	drive := func(pct float64) (emits, last int) {
		last = bucketReset
		rec := map[string]any{"context_pct": pct, "context_pct_ts": 20.0, "boot_ts": 10.0}
		for i := 0; i < 60; i++ {
			var sig *contextHighSignal
			sig, last = decideContextHighSignal("m-1", rec, last, cfg)
			if sig != nil {
				emits++
			}
		}
		return emits, last
	}
	emits, last := drive(42)
	if emits != 1 {
		t.Fatalf("a gauge parked in one bucket must remind once, got %d", emits)
	}
	// Now climb into the next bucket (45–49) from that carried marker: one more.
	rec := map[string]any{"context_pct": 46.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	sig, last := decideContextHighSignal("m-1", rec, last, cfg)
	if sig == nil || last != 9 {
		t.Fatalf("climbing into a new bucket must remind: sig=%v bucket=%d", sig, last)
	}
	// Recover below warn (reset), then re-enter: reminds again.
	recLow := map[string]any{"context_pct": 12.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	_, last = decideContextHighSignal("m-1", recLow, last, cfg)
	if last != bucketReset {
		t.Fatalf("recovery below warn must reset the marker, got %d", last)
	}
	sig, _ = decideContextHighSignal("m-1", rec, last, cfg)
	if sig == nil {
		t.Fatal("re-entering the band after recovery must remind again")
	}
}

// TestContextHighPerConnectionIsolation: two members' gauges drive independent
// markers — one member sitting quiet never suppresses another's first reminder.
func TestContextHighPerConnectionIsolation(t *testing.T) {
	cfg := defaultSseContextHigh()
	recA := map[string]any{"context_pct": 42.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	recB := map[string]any{"context_pct": 47.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	// Member A settles into bucket 8 (one reminder, then quiet).
	sigA, lastA := decideContextHighSignal("m-a", recA, bucketReset, cfg)
	if sigA == nil {
		t.Fatal("A first entry must remind")
	}
	sigA, lastA = decideContextHighSignal("m-a", recA, lastA, cfg)
	if sigA != nil {
		t.Fatal("A same bucket must stay quiet")
	}
	// Member B, with its OWN marker, still gets its first reminder.
	sigB, _ := decideContextHighSignal("m-b", recB, bucketReset, cfg)
	if sigB == nil || sigB.To != "m-b" {
		t.Fatalf("B must remind on its own connection: %+v", sigB)
	}
	// A's marker is untouched by B's tick.
	if lastA != 8 {
		t.Fatalf("A marker leaked: %d", lastA)
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

func TestDecideContextHighSignal(t *testing.T) {
	cfg := defaultSseContextHigh()
	record := map[string]any{"context_pct": 45.0, "context_pct_ts": 20.0, "boot_ts": 10.0}

	signal, bucket := decideContextHighSignal("m-1", record, bucketReset, cfg)
	if signal == nil {
		t.Fatal("warn band must emit")
	}
	if signal.Topic != "context-high" || signal.To != "m-1" || signal.Level != "warn" ||
		float64(signal.Pct) != 45.0 || signal.Reason == "" {
		t.Fatalf("signal: %+v", signal)
	}
	if bucket != 9 { // floor(45/5)
		t.Fatalf("carry-forward bucket: %d", bucket)
	}

	// HANDOVER is decided but NEVER emitted on the wire (producer auto-recycle
	// owns it) — the bucket bookkeeping still advances.
	record["context_pct"] = 60.0
	signal, bucket = decideContextHighSignal("m-1", record, 9, cfg)
	if signal != nil {
		t.Fatalf("handover must not emit over SSE: %+v", signal)
	}
	if bucket != 12 { // floor(60/5) — marker advances even while quiet on the wire
		t.Fatalf("handover bookkeeping must advance: %d", bucket)
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
		"task_type", "model", "effort", "session_name"}
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
		if !strings.Contains(sig.Reason, "T-7d40") ||
			!strings.Contains(sig.Reason, "write_task_learnings") {
			t.Fatalf("reason must name the task and the tool: %q", sig.Reason)
		}
		// T-fa76: the sentence shows the display label, but the MCP
		// ADDRESSING string stays the raw type_key.
		if !strings.Contains(sig.Reason, "「審查 PR（review-pr）」") ||
			!strings.Contains(sig.Reason, "type_key=`review-pr`") {
			t.Fatalf("reason must carry display label AND raw key: %q", sig.Reason)
		}
	}

	// A blank label (manual deleted / lookup failed) falls back to the key.
	fallback := base
	fallback.Status = TaskStatusDone
	if sig := decideTaskCloseNudge(fallback, ""); sig == nil ||
		!strings.Contains(sig.Reason, "「review-pr」") {
		t.Fatalf("blank label must fall back to the raw key: %+v", sig)
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
