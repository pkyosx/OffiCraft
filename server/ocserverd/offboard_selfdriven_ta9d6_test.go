package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The sentence itself. The owner cut four differently-worded notices down to
// ONE (「不需要太多不同描述吧, 就請他按照步驟做好下線, 頂多告訴他剩下 120 秒」),
// so what tells the situations apart is the FIELDS, not the tone — and each of
// the two clauses below is load-bearing in a way that is invisible from the
// code:
//
//   - "then call restart_self yourself" blocks BOTH failure directions at once.
//     Without the second half an agent idles until the server cuts it off (dead
//     time the owner explicitly does not want); without the first, it stops
//     mid-work — a predecessor read the old wording as "you are done" and
//     announced its own end of life at 40%.
//   - "You have 120 seconds left." is the ONLY difference between a notice that
//     means "there is room" and one that means "you are out of time".
//
// 🔴 Both were measured to be UNGUARDED before this test existed: deleting
// either clause left the entire ocserverd suite green (228s and 186s, whole
// suite, no cache). A sentence nothing asserts is a sentence the next edit
// silently rewrites.
func TestOffboardNotice_TheApprovedSentence(t *testing.T) {
	const where = "context 62% (your limits: 60% / 75%)"
	doc := "1. 報開始收尾\n2. 給自己留交接"

	// 🔴 THE OPENER NOW DIFFERS BY ARM (owner 2026-08-20, card rc-e9b655cd8e1a,
	// option 0: 「軟性那則的第一行改成不催」). He narrowed his own 2026-08-16
	// one-sentence design after a soft close-out that said "offboard now" while
	// the document below it said a soft arm may let its sub-agents finish —
	// the first line being the one read first. Everything AFTER the opener is
	// still identical on both arms and still asserted verbatim below.
	// WHOLE STRING (owner ruling 2026-08-20, c-2502de439aaa: 「你如果要比對
	// context 就是比對一整份要一模一樣」). offboardNotice is deterministic and
	// every input here is a literal, so the complete expected value exists —
	// and comparing it pins ALL of what three separate keyword assertions used
	// to pin, plus everything they never looked at: the opener, the second
	// half, the absence of "offboard now", the absence of any deadline clause,
	// the newline, and the document carried verbatim.
	soft := offboardNotice(where, offboardCloserRestartSelf, false, 0, doc)
	wantSoft := where + " — start your close-out: work the sequence below, " +
		"then call restart_self yourself.\n" + doc
	if soft != wantSoft {
		t.Fatalf("the soft notice must open WITHOUT urging and carry the rest of "+
			"the approved sentence verbatim:\n got %q\nwant %q", soft, wantSoft)
	}
	// Kept as a SHAPE assertion on top of the equality above, because it is not
	// a keyword test: it refuses a time in any of the shapes it knows, which is
	// the property the two arms actually differ on. What it turns on is the
	// UNIT, never the digit (offboard_absolute_deadline_td6a7_test.go is where
	// that is argued at length) — the quantity may be a digit, a CJK numeral,
	// or the quantity words 半/幾/几, so "剩半分鐘" and "還有兩分鐘" are both
	// caught. The bound is narrower than "any spelling": an English quantity
	// spelled in words ("two minutes left", "half an hour") is NOT caught, and
	// td6a7 argues that is a deliberate limit rather than an oversight — units
	// are a closed list, English quantity phrasing is not. An equality assertion pins today's
	// string; this one still fires if a future edit adds a clock in a wording
	// nobody has written yet.
	assertQuotesNoTime(t, "the soft notice", soft)

	// T-d6a7: the final call now names WHEN the deadline is, not how long is
	// left. A duration went stale on every replay of the same epoch (and broke
	// the client's verbatim de-dupe); an absolute instant is constant.
	//
	// ⚠️ The expected instant is a LITERAL, deliberately. It used to be computed
	// with the same `time.Unix(...).Format(time.RFC3339)` the production code
	// ran, so it agreed with whatever zone that produced — including the
	// implicit LOCAL one it actually used. The rendering is UTC and is asserted
	// as such.
	const deadline = 1_787_000_000.0 // 2026-08-17T20:53:20Z
	final := offboardNotice(where, offboardCloserRestartSelf, true, deadline, doc)
	wantFinal := where + " — offboard now: work the sequence below, " +
		"then call restart_self yourself. Your deadline is 2026-08-17T20:53:20Z.\n" + doc
	if final != wantFinal {
		t.Fatalf("the final call must name the deadline, right after the same "+
			"sentence:\n got %q\nwant %q", final, wantFinal)
	}

	// A final call with NO clock is a contradiction (offboardKindOf only answers
	// "final" for a clocked arm), and if it ever happens the sentence says
	// nothing about time rather than formatting epoch 0 as 1970.
	if noClock := offboardNotice(where, offboardCloserRestartSelf, true, 0, doc); strings.Contains(noClock, "deadline") {
		t.Fatalf("a final call with no clock must quote no time at all:\n%s", noClock)
	}

	// An empty document degrades to the sentence alone: losing the checklist is
	// survivable, losing the notice is not.
	bare := offboardNotice(where, offboardCloserRestartSelf, false, 0, "")
	wantBare := where + " — start your close-out: work the sequence below, " +
		"then call restart_self yourself."
	if bare != wantBare {
		t.Fatalf("an empty document must leave the sentence intact and alone "+
			"(no trailing newline):\n got %q\nwant %q", bare, wantBare)
	}
}

// Who gets which sentence. This is the judgement the whole ticket turns on —
// soft or final, and when one becomes the other — and BOTH directions of it
// survived the entire server suite before this test existed (independent
// review: forcing every answer to final, then every answer to soft, each left
// `ok ocserverd ~200s`). A judgement nothing asserts is a judgement the next
// edit is free to invert.
func TestOffboardKindOf_WhoGetsWhichSentence(t *testing.T) {
	const t0 = 1_000_000.0
	soft, final := offboardKindSoft, offboardKindFinal

	cases := []struct {
		name   string
		member Member
		now    float64
		want   string
		// carries=false means the member is not being wound down at all.
		carries bool
	}{
		{"下線 just pressed", Member{DesiredState: DesiredStateOffline, StoppingSince: t0}, t0 + 1, soft, true},
		// 🔴 …and it is STILL soft long after any window, because nothing
		// collects it on a clock. A final call here would promise 120 seconds
		// nobody keeps.
		{"下線 an hour later", Member{DesiredState: DesiredStateOffline, StoppingSince: t0}, t0 + 3600, soft, true},
		{"desired offline with no anchor", Member{DesiredState: DesiredStateOffline}, t0, "", false},

		{"重新聚焦 just pressed", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + 1, soft, true},
		// 🔴 …and STILL soft an hour later, for the same reason 下線 is: owner
		// 2026-08-19 took the clock off this arm too, so a final call here would
		// start 120 seconds in the agent's head that nothing on the server is
		// counting. It used to flip at exactly SoftOffboardGraceSecs, which is
		// why the two rows below straddle that boundary.
		{"重新聚焦 one second before the old flip", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + SoftOffboardGraceSecs - 1, soft, true},
		{"重新聚焦 at the old flip", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + SoftOffboardGraceSecs, soft, true},
		{"重新聚焦 an hour later", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + 3600, soft, true},

		// 🔴 T-ed79: the FINAL sentence is the accelerated stop, and the
		// accelerated stop is the SECOND context threshold — nothing else. The
		// three rows below used to read final by FALLTHROUGH, which is how an
		// owner verb the owner never put on a clock ended up quoting a deadline.
		{"context pressure, second threshold", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpContextHigh}, t0 + 1, final, true},
		{"改機器", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: memberOpRelocate}, t0 + 1, soft, true},
		{"換 model / runtime", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: memberOpModel}, t0 + 1, soft, true},
		{"the agent's own restart_self", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRestartSelf}, t0 + 1, soft, true},
		// An op no constant names is SOFT, not final: the default has to be the
		// arm that promises nothing, or a cause nobody ruled on arrives with a
		// deadline attached.
		{"an op no constant names", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: "an_op_no_constant_names"}, t0 + 1, soft, true},

		{"online and untouched", Member{DesiredState: DesiredStateOnline}, t0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, carries := offboardKindOf(c.member, c.now)
			if carries != c.carries || kind != c.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", kind, carries, c.want, c.carries)
			}
		})
	}
}

// …and the classification has to reach the wire, because the sentence is
// composed from it. 下線 must never carry the countdown clause.
func TestOffboardDeltaPayload_下線NeverCarriesACountdown(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-quiet")
	m.DesiredState = DesiredStateOffline
	m.StoppingSince = nowSecs() - 10*SoftOffboardGraceSecs // long past any window
	putTestMember(t, s, m)

	payload := s.offboardDeltaPayload(m)
	notice, ok := payload["offboard_notice"].(string)
	if !ok || notice == "" {
		t.Fatalf("下線 must carry the offboard notice: %+v", payload)
	}
	assertQuotesNoTime(t, "下線", notice)
	// 🔴 …and it must name the tool that actually WORKS here. restart_self
	// refuses a member the owner has taken down (it is a RE-start), so naming it
	// on this arm would be an instruction that can only answer 409 — and with no
	// clock collecting 下線, the session would sit refused until someone pressed
	// force-stop. Its sequence ends at report_stopped, which is also step 6 of
	// the document it is being shown.
	// WHOLE SENTENCE, compared as one string (owner ruling 2026-08-20,
	// c-2502de439aaa). The document carried below it is the owner's and is not
	// what this test guards — it mentions restart_self in its own right,
	// because it covers every offboard path — so the comparison is on the FIRST
	// LINE, which is the whole of what this server composes here.
	//
	// It pins in one assertion what two keyword checks used to: the closer is
	// report_stopped, restart_self appears nowhere in the sentence, and the
	// rest of the approved wording is intact.
	sentence, _, _ := strings.Cut(notice, "\n")
	wantSentence := "close-out (your limits: 40% / 50%) — start your close-out: " +
		"work the sequence below, then call report_stopped yourself."
	if sentence != wantSentence {
		t.Fatalf("下線 must be named the tool that works on this arm, and must "+
			"not be told to re-start itself:\n got %q\nwant %q",
			sentence, wantSentence)
	}
}

// The sequence the notice tells the agent to work must actually be workable.
// Step 1 is report_stopping, and the notice ends 「then call restart_self
// yourself」 — so an agent that has declared its close-out must still be able
// to make that call. It could not: report_stopping makes PresenceState project
// `stopping`, the endpoint gated on `online`, and once close-out anchors
// stopped being swept every tick the refusal lasted the whole soft window.
func TestRestartSelf_WorksWhileTheAgentIsClosingOut(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-closing")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-closing", "mach-a")
	// A mature session: past the respawn-storm floor, which is a separate guard.
	s.gauge.Set("m-closing", map[string]any{"boot_ts": nowSecs() - 10*minSelfRestartSecs})

	rec := httptest.NewRecorder()
	s.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", map[string]any{}, "m-closing", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.HandleRestartSelfApiSelfRefocusPost(rec,
		taskReq(t, "POST", "/api/self/refocus", map[string]any{}, "m-closing", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("an agent doing exactly what the notice told it must not be "+
			"refused: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-closing")
	if after.RefocusSince <= 0 {
		t.Fatalf("the self-restart must open a handover epoch: %+v", after)
	}

	// The owner's 重新聚焦 has the same gate and the same reason to survive it.
	rec = httptest.NewRecorder()
	s.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
		taskReq(t, "POST", "/api/members/m-closing/refocus", map[string]any{},
			wireOwnerID, "owner"), "m-closing")
	if rec.Code != http.StatusOK {
		t.Fatalf("重新聚焦 must still reach an agent that is mid-hand-off: %d %s",
			rec.Code, rec.Body.String())
	}
}

// codex is judged in ROUNDS, and the notice has to say WHERE IT IS, not just
// where the limits are. The final call substitutes the threshold round because
// that is where the session has arrived; the soft notice must report the round
// the gauge actually shows.
//
// 🔴 Measured: deleting that substitution left the whole ocserverd suite green
// (259s) — the codex arm of the composer had no test at all.
func TestOffboardNoticeFor_CodexReportsWhereItActuallyIs(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-codex")
	m.Runtime = RuntimeCodex
	m.RefocusSince = nowSecs()
	m.RefocusOp = refocusOpRefocus
	putTestMember(t, s, m)
	s.gauge.Set("m-codex", map[string]any{"compaction_count": 3.0})

	final := s.codexCompactionThresholdSetting()
	soft := s.offboardNoticeFor(m, offboardKindSoft)
	if !strings.Contains(soft, "compaction round 3 ") {
		t.Fatalf("the soft notice must report the round the session is ON:\n%s", soft)
	}
	hard := s.offboardNoticeFor(m, offboardKindFinal)
	if !strings.Contains(hard, fmt.Sprintf("compaction round %d ", final)) {
		t.Fatalf("the final call must report the round it has ARRIVED at (%d):\n%s",
			final, hard)
	}
	// Both must still carry the limits, which is how the agent tells the two apart.
	for _, n := range []string{soft, hard} {
		if !strings.Contains(n, fmt.Sprintf("round %d)", final)) {
			t.Fatalf("every codex notice must carry its limits:\n%s", n)
		}
	}
}

// The deadline the cockpit renders, and the clock that actually collects the
// session, must be the SAME number — including when that number is "there is no
// clock". They were not: refocus_deadline was computed from RecycleGrace while
// an owner-pressed 重新聚焦 was collected at the soft window PLUS that, and the
// owner watched a time pass with nothing happening. Since 2026-08-19 that arm
// has no clock at all, so the cockpit must render NO deadline (0 → null) rather
// than any time whatsoever.
//
// 🔴 Measured before writing this: collapsing recycleGraceFor to
// `return cfg.RecycleGrace, true` — i.e. putting a silent 120s deadline back on
// the owner's button — left the whole ocserverd suite green (339s).
func TestRecycleGraceFor_MatchesTheClockThatActuallyCollects(t *testing.T) {
	cfg := defaultReconcileConfig()

	type want struct {
		grace   float64
		clocked bool
	}
	cases := map[string]want{
		// 停止 — the agent is shown the sequence and collected by its own
		// stopped report or by force-stop. No countdown, so no clock: not now,
		// and not after any window. T-ed79 moved the four rows after the first
		// one here; they were clocked by FALLTHROUGH, never by ruling.
		refocusOpRefocus:          {0, false},
		refocusOpRestartSelf:      {0, false},
		memberOpRelocate:          {0, false},
		memberOpModel:             {0, false},
		"":                        {0, false},
		"an_op_no_constant_names": {0, false},
		// 加速停止 — the ONE clocked cause, and it arrives already saying so.
		refocusOpContextHigh: {cfg.RecycleGrace, true},
	}
	for op, w := range cases {
		grace, clocked := recycleGraceFor(op, cfg)
		if grace != w.grace || clocked != w.clocked {
			t.Errorf("recycleGraceFor(%q) = (%v, %v), want (%v, %v)",
				op, grace, clocked, w.grace, w.clocked)
		}
	}

	// …and the wire field the cockpit reads must agree with it, or the owner is
	// shown a ceiling the server does not intend to honour. 0 is what the
	// cockpit maps to "no deadline"; any positive number renders a countdown.
	s := newReconcileTestServer(t)
	m := testAgent("m-deadline")
	m.RefocusSince = 1000
	m.RefocusOp = refocusOpRefocus
	putTestMember(t, s, m)
	if dto := s.newMemberDTO(m, "", "", 0); dto.RefocusDeadline != 0 {
		t.Fatalf("refocus_deadline = %v, want 0 — nothing collects an owner-pressed "+
			"重新聚焦 on a clock, so the cockpit must show no deadline", dto.RefocusDeadline)
	}

	// The positive control: an arm that IS clocked still reports its deadline,
	// so the assertion above is reading the refocus_op and not a field that
	// simply stopped being populated.
	m.RefocusOp = refocusOpContextHigh
	putTestMember(t, s, m)
	if dto := s.newMemberDTO(m, "", "", 0); dto.RefocusDeadline != 1000+cfg.RecycleGrace {
		t.Fatalf("refocus_deadline = %v, want %v for a clocked arm",
			dto.RefocusDeadline, 1000+cfg.RecycleGrace)
	}
}

// The pair the owner sets is 「第一次通知 / 最後通牒」, and a pair whose first
// number is not below its second is not a pair — the notice would fire at or
// after the handover it is supposed to precede, i.e. never. The handler refuses
// it rather than silently reordering, because a silently corrected setting is
// one the owner cannot see he got wrong.
//
// 🔴 The claude half of that refusal was UNGUARDED: disabling it left the whole
// ocserverd suite green (240s, no cache). Measured, not assumed — the codex half
// below is asserted for the same reason, since the two are separate checks and a
// test for one says nothing about the other.
func TestSettingsPair_NoticeMustBeStrictlyBelowTheFinalCall(t *testing.T) {
	patch := func(t *testing.T, api *apiServer, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleUpdateSettingsApiSettingsPatch(rec,
			taskReq(t, http.MethodPatch, "/api/settings", body, "owner", "owner"))
		return rec
	}

	t.Run("claude: equal is refused, and so is inverted", func(t *testing.T) {
		s := newReconcileTestServer(t)
		for _, body := range []map[string]any{
			{"notice_pct": 65, "handover_pct": 65},
			{"notice_pct": 75, "handover_pct": 65},
		} {
			rec := patch(t, s, body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%v must be refused, got %d %s", body, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "notice_pct") {
				t.Fatalf("the refusal must name the field: %s", rec.Body.String())
			}
		}
		// …and a real pair still lands, so the check is a guard and not a wall.
		if rec := patch(t, s, map[string]any{"notice_pct": 60, "handover_pct": 75}); rec.Code != http.StatusOK {
			t.Fatalf("a valid pair must be accepted: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("codex: rounds obey the same rule", func(t *testing.T) {
		s := newReconcileTestServer(t)
		rec := patch(t, s, map[string]any{"codex_notice_round": 6, "codex_compaction_threshold": 6})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("equal rounds must be refused, got %d %s", rec.Code, rec.Body.String())
		}
		if rec := patch(t, s, map[string]any{"codex_notice_round": 5, "codex_compaction_threshold": 6}); rec.Code != http.StatusOK {
			t.Fatalf("a valid round pair must be accepted: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// The self-driven offboard: an agent that was told to close out and stop
// itself, does so, and reaches the end of the sequence.
//
// This path had no receiver. Collection was armed only by a refocus epoch —
// something ELSE deciding to take the session — which held while the offboard
// sequence was shown only to a session already being collected. Once the notice
// began telling agents to close out on their own (T-c382) and the sequence
// became a document any session could work (T-c9c0), an agent could finish its
// close-out, report stopped, and have nothing happen: it stayed alive holding a
// session it had already declared finished.
//
// owner 2026-08-16 (card rc-b08d49dc3b03, option ①): 「收掉並重生」.
func TestSelfDrivenOffboard_StoppedReportCollectsAndRespawns(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-self")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-self", "mach-a")

	// Nobody is collecting it: no refocus epoch, desired_state=online. This is
	// the whole point — the agent decided this by itself.
	rec := httptest.NewRecorder()
	s.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", map[string]any{}, "m-self", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("declaring the close-out must not kill anything: %+v", f)
	}

	// …and the cockpit must still show it, which is what the owner watched fail
	// (T-2123): the stale-stopping sweep used to erase a close-out in flight.
	s.runReconcileTick(nowSecs())
	after, _ := s.dal.GetMember("m-self")
	if after == nil || after.StoppingSince <= 0 {
		t.Fatalf("an in-flight close-out must keep its anchor: %+v", after)
	}

	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-self", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("a self-driven close-out must be collected on its stopped report: %+v", stops)
	}

	// …and a new generation takes its place, which is what the document has
	// been promising all along (「server 原地重生新的你」).
	s.hub.Disconnect(session)
	s.reconcileMemberNow("m-self")
	starts := drainFrames(t, s, "mach-a")
	if len(starts) != 1 || starts[0].RPC != "start" {
		t.Fatalf("the respawn must follow the collect: %+v", starts)
	}
}

// The same report on a member the owner has taken DOWN collects it just as
// promptly — and does NOT bring it back. desired_state is the only thing that
// decides which of the two happens.
func TestSelfDrivenOffboard_StoppedReportOnADesiredOfflineMemberDoesNotRespawn(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-down")
	m.DesiredMachineID = "mach-a"
	m.DesiredState = DesiredStateOffline
	m.StoppingSince = nowSecs()
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-down", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-down", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("a finished close-out must be collected immediately: %+v", stops)
	}

	s.hub.Disconnect(session)
	s.reconcileMemberNow("m-down")
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("a member the owner took down must stay down: %+v", f)
	}
}

// 強制下線 leaves a mark. It is the one offboard path that sends no notice, so
// what it kills leaves exactly what a session with nothing to say leaves —
// no hand-off, no fresh step note. This column is the difference, and the
// reader who needs it is the generation that comes after, so the next boot
// must NOT clear it.
func TestForceStop_RecordsThatTheSessionWasCutOff(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-cut")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-cut", "mach-a")

	before, _ := s.dal.GetMember("m-cut")
	if before.ForcedStopAt != 0 {
		t.Fatalf("a member that was never force-stopped must carry 0: %+v", before)
	}

	rec := httptest.NewRecorder()
	s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
		taskReq(t, "POST", "/api/members/m-cut/force-stop", map[string]any{},
			wireOwnerID, "owner"), "m-cut")
	if rec.Code != http.StatusOK {
		t.Fatalf("force-stop: %d %s", rec.Code, rec.Body.String())
	}
	cut, _ := s.dal.GetMember("m-cut")
	if cut.ForcedStopAt <= 0 {
		t.Fatalf("the force-stop must be recorded: %+v", cut)
	}

	// The next generation boots — and must still be able to see that its
	// predecessor was cut off rather than allowed to finish. report_waking
	// clears every OTHER lifecycle anchor on this row.
	rec = httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{}, "m-cut", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	woke, _ := s.dal.GetMember("m-cut")
	if woke.ForcedStopAt != cut.ForcedStopAt {
		t.Fatalf("the next boot must not erase the record: %v → %v",
			cut.ForcedStopAt, woke.ForcedStopAt)
	}

	// 🔴 …and the assertion above passes for a reason that is NOT the one that
	// protects this column: report_waking rewrites a row it just read, so it
	// carries the right value either way. Both halves of that were measured —
	// zeroing it in the handler AND letting the upsert carry it each left the
	// check green. What actually protects it is PutMember declining to write
	// the column at all, and the shape that finds out is a STALE snapshot:
	// any writer holding a member value from before the force-stop. That is
	// how the avatar pointer and the session anchor lost data before they got
	// their own seams.
	stale := *before
	stale.Name = "renamed by a writer holding an old snapshot"
	if err := s.dal.PutMember(stale); err != nil {
		t.Fatalf("stale put: %v", err)
	}
	survived, _ := s.dal.GetMember("m-cut")
	if survived.ForcedStopAt != cut.ForcedStopAt {
		t.Fatalf("a stale snapshot must not erase the force-stop record: %v → %v",
			cut.ForcedStopAt, survived.ForcedStopAt)
	}
	if survived.Name != stale.Name {
		t.Fatalf("…while the rest of that write must land normally: %+v", survived)
	}
}

// 下線 must not downgrade a 強制下線. The stop gate tells "close-out in flight"
// (admit the reconnect, because 下線 runs no clock and a refused reconnect
// self-terminates the session mid-hand-off) from "cut off deliberately"
// (refuse it) by comparing the two anchors. deactivate re-stamps
// stopping_since UNCONDITIONALLY — so without the exception this test pins, a
// deactivate arriving after a force-stop moves a cut-off member onto the
// admitted side, and nothing collects it afterwards.
//
// Found by independent review, not by me: I compared the anchors without
// asking who else writes them.
func TestDeactivate_DoesNotDowngradeAForcedStop(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-forced")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-forced", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
		taskReq(t, "POST", "/api/members/m-forced/force-stop", map[string]any{},
			wireOwnerID, "owner"), "m-forced")
	if rec.Code != http.StatusOK {
		t.Fatalf("force-stop: %d %s", rec.Code, rec.Body.String())
	}
	forced, _ := s.dal.GetMember("m-forced")
	if s.sseStopGateRefusal("m-forced") == "" {
		t.Fatalf("a force-stopped member must be refused to begin with: %+v", forced)
	}

	rec = httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-forced/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-forced")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-forced")
	if after.StoppingSince != forced.StoppingSince {
		t.Fatalf("a forced epoch's anchor must not move: %v → %v",
			forced.StoppingSince, after.StoppingSince)
	}
	if s.sseStopGateRefusal("m-forced") == "" {
		t.Fatalf("a deactivate after a force-stop must not re-open the gate: %+v", after)
	}

	// …and the exception stays narrow: activate clears the stop anchors (it
	// keeps forced_stop_at, which is the durable record), so the NEXT 下線 is a
	// fresh soft epoch and its reconnect must be admitted again. Testing
	// forced_stop_at alone instead of the live epoch would fail right here — it
	// would strip the soft-offboard admission from every member ever forced.
	rec = httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-forced/activate", map[string]any{},
			wireOwnerID, "owner"), "m-forced")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-forced/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-forced")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate after activate: %d %s", rec.Code, rec.Body.String())
	}
	fresh, _ := s.dal.GetMember("m-forced")
	if fresh.StoppingSince <= forced.StoppingSince {
		t.Fatalf("a fresh soft epoch must stamp a NEW anchor: %+v", fresh)
	}
	if msg := s.sseStopGateRefusal("m-forced"); msg != "" {
		t.Fatalf("a fresh close-out must still be admitted: %s", msg)
	}
}

// 強制下線 sends NO message — the owner ruled it outright ("強制還需要發訊息嗎"
// → no), and both this file's sibling comment and HandleForceStopMember's own
// godoc say so in prose.
//
// 🔴 Prose was all that said it. force-stop sets desired_state=offline and
// stamps stopping_since BEFORE it publishes, and the offboard payload attaches
// a notice to exactly that state — so the member being killed received a full
// SOFT notice on its own stream, telling it to work the sequence and call
// restart_self. Independent e2e verification caught the frame on the wire; the
// server suite was green throughout, because nothing asserted the promise.
func TestForceStop_SendsNoNotice(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	// Positive control FIRST: the same payload builder on the graceful arm must
	// carry a notice, otherwise an assertion of "no notice" below proves only
	// that the notice machinery is off entirely.
	soft := testAgent("m-soft")
	soft.DesiredState = DesiredStateOffline
	soft.StoppingSince = nowSecs()
	if _, ok := s.offboardDeltaPayload(soft)["offboard_notice"]; !ok {
		t.Fatalf("a graceful 下線 must still carry the sequence: %+v", soft)
	}

	m := testAgent("m-killed")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-killed", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
		taskReq(t, "POST", "/api/members/m-killed/force-stop", map[string]any{},
			wireOwnerID, "owner"), "m-killed")
	if rec.Code != http.StatusOK {
		t.Fatalf("force-stop: %d %s", rec.Code, rec.Body.String())
	}

	killed, _ := s.dal.GetMember("m-killed")
	if _, carries := offboardKindOf(*killed, nowSecs()); carries {
		t.Fatalf("force-stop must send nothing: %+v", killed)
	}
	if got, ok := s.offboardDeltaPayload(*killed)["offboard_notice"]; ok {
		t.Fatalf("force-stop must send nothing, got %q", got)
	}
}

// The force-stop record must land in the SAME write that publishes the member,
// not only through its own targeted seam. Independent review found the coupling:
// the SSE stop gate now READS forced_stop_at to tell "cut off deliberately" from
// "working its close-out", while the only writer was SetMemberForcedStopAt,
// whose failure is deliberately non-fatal — so a failed UPDATE left the column
// at 0 and a force-stopped member reconnected as if it were closing out, on an
// arm that runs no clock to collect it. A safety verdict must not hang on a
// best-effort write.
//
// The upsert carries it under max(), which is what keeps BOTH properties: a
// fresh writer's stamp lands, and a stale snapshot (older value, or 0) cannot
// erase one that is already stored.
func TestPutMemberCarriesForcedStopAtForwardOnly(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-upsert")
	putTestMember(t, s, m)

	stamped := m
	stamped.ForcedStopAt = 5000.0
	if err := s.dal.PutMember(stamped); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, _ := s.dal.GetMember("m-upsert")
	if got.ForcedStopAt != 5000.0 {
		t.Fatalf("the publishing write must carry the record: %v", got.ForcedStopAt)
	}

	// A stale snapshot — the shape that cost the avatar pointer and the session
	// anchor their data — must not roll it back.
	stale := m
	stale.ForcedStopAt = 0
	stale.Name = "written by a holder of an older row"
	if err := s.dal.PutMember(stale); err != nil {
		t.Fatalf("stale put: %v", err)
	}
	got, _ = s.dal.GetMember("m-upsert")
	if got.ForcedStopAt != 5000.0 {
		t.Fatalf("a stale snapshot must not erase the record: %v", got.ForcedStopAt)
	}
	if got.Name != stale.Name {
		t.Fatalf("…while the rest of that write must land normally: %+v", got)
	}

	// …and an EARLIER force-stop cannot overwrite a later one either.
	older := m
	older.ForcedStopAt = 4000.0
	if err := s.dal.PutMember(older); err != nil {
		t.Fatalf("older put: %v", err)
	}
	got, _ = s.dal.GetMember("m-upsert")
	if got.ForcedStopAt != 5000.0 {
		t.Fatalf("forced_stop_at must only move forward: %v", got.ForcedStopAt)
	}
}

// 🔴 THE ARM T-3201 CHANGES, AND THE ONLY ONE THAT HAD NO TEST.
//
// The offboard document now names ONE close-out verb, report_stopped, on both
// arms — owner, verbatim (c-5b3d8f192a0b): 「我預期是 report_stopped，因為是
// server 控制他上下線，他只是要回報執行狀態。restart_self 真正的用途應該是我在
// 對話中跟他說你做完這件事情自己重啟」. Today's code still interpolates
// offboardCloserFor, which says restart_self whenever a member is still wanted
// online — so the arm the document changes is exactly this one: an agent that
// asked for its own restart, was given the wind-down, and then ends the
// sequence with report_stopped instead.
//
// Everything in the tree that says this is safe is READ from the code. The two
// arms above cover no-epoch-online (respawns) and no-epoch-offline (stays
// down); NEITHER has a refocus epoch in flight, and a refocus epoch is what
// restart_self stamps. This test is that missing third arm: with the stamp on,
// report_stopped must still collect AND respawn — not park the member down for
// good, which is what "the document tells them the wrong verb" would cost.
//
// End-to-end (a real agent, a real warden) is the shipping check; this is the
// handler-level half.
func TestSelfDrivenOffboard_StoppedReportAfterARestartSelfStampRespawns(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-restart")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-restart", "mach-a")
	// Past the respawn-storm floor: that guard is asserted elsewhere and must
	// not be what this test measures.
	s.gauge.Set("m-restart", map[string]any{"boot_ts": nowSecs() - 10*minSelfRestartSecs})

	// t1 — the agent was told 「做完某事自己重啟」 and asks for its own restart.
	rec := httptest.NewRecorder()
	s.HandleRestartSelfApiSelfRefocusPost(rec,
		taskReq(t, "POST", "/api/self/refocus", map[string]any{}, "m-restart", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("restart_self: %d %s", rec.Code, rec.Body.String())
	}
	stamped, _ := s.dal.GetMember("m-restart")
	if stamped == nil || stamped.RefocusSince <= 0 || stamped.RefocusOp != refocusOpRestartSelf {
		t.Fatalf("premise: restart_self must leave a refocus stamp, or this test is "+
			"the no-epoch arm again: %+v", stamped)
	}
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("restart_self must collect nobody by itself: %+v", f)
	}

	// t4 — the agent works the document and ends it the way the document says.
	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-restart", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("the close-out must be collected on the stopped report: %+v", stops)
	}

	// t5 — and a new generation takes its place. This is the assertion the
	// whole verb change rests on: RESPAWN, not a member parked down holding a
	// stamp nobody will act on.
	s.hub.Disconnect(session)
	s.reconcileMemberNow("m-restart")
	starts := drainFrames(t, s, "mach-a")
	if len(starts) != 1 || starts[0].RPC != "start" {
		t.Fatalf("report_stopped under a restart_self stamp must RESPAWN, not stop "+
			"for good: %+v", starts)
	}
	back, _ := s.dal.GetMember("m-restart")
	if back == nil || back.DesiredState != DesiredStateOnline {
		t.Fatalf("the member must still be wanted online after the recycle: %+v", back)
	}
}
