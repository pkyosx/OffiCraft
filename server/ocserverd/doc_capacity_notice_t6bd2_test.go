package main

// doc_capacity_notice_t6bd2_test.go — the three properties of the T-6bd2 SOFT
// notice that the ticket's own suite left unguarded, each written after a
// mutant survived a fully green run. (The peek's uncounted block, the ticket's
// other blocker, is guarded next door in doc_capacity_peek_t6bd2_test.go.)
//
//  1. COST. The notice fires once per session; composing it did NOT. Guarded by
//     counting how many times the two text closures actually run.
//  2. WHO CAN HANDLE IT. A row the reader cannot write has to name someone who
//     can. Deleting that half left every test green.
//  3. THE ADDRESS, NOT THE NAME. 銀月 is an editable display name; `mira` is
//     what a message can be sent to. Dropping the id left every test green.
//
// As in doc_capacity_t6bd2_test.go, every expected string is a LITERAL: an
// assertion that reads its expectation off the code under test moves with the
// mutant and can never turn red.

import (
	"strings"
	"testing"
)

// t6bd2NoticeReader adds a SECOND member sharing the seed assistant's role and
// runtime, and returns its id. The referral tests need a reader who is not the
// compactor herself — "去找銀月" said to 銀月 would be a green that proves
// nothing about whether the sentence names anybody.
func t6bd2NoticeReader(t *testing.T, s *apiServer) string {
	t.Helper()
	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("seed assistant: %v %v", mira, err)
	}
	reader := Member{
		ID: "m-reader", Name: "reader", Kind: KindAssistant,
		RoleKey: mira.RoleKey, Runtime: mira.Runtime,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(reader); err != nil {
		t.Fatalf("seed reader: %v", err)
	}
	return reader.ID
}

// t6bd2NoticeWire composes the soft notice the way the SSE loop does and
// returns the bytes that go on the wire.
func t6bd2NoticeWire(t *testing.T, s *apiServer, actor string) string {
	t.Helper()
	sig := decideHandoverNotice(actor, RuntimeClaude,
		map[string]any{"context_pct": 56.0, "boot_ts": 1000.0},
		SseContextHighConfig{HandoverPct: 65, NoticePct: 55}, 5, 6,
		s.offboardText,
		func() string { return docCapacityLines(s.docCapacityFor(actor, s.stepNoteCapacityFor(actor))) })
	if sig == nil {
		t.Fatal("the soft notice must fire at the notice pct")
	}
	frame, err := directedFrameText(contextHighTopic, sig)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	return string(frame)
}

// t6bd2NoticeLine returns the ONE line of the notice that names substr. Working
// per line matters: "the frame mentions mira somewhere" would be satisfied by
// any other sentence in the offboard document, so the assertion has to be
// anchored to the row it is about.
func t6bd2NoticeLine(t *testing.T, wire, substr string) string {
	t.Helper()
	// The frame is JSON, so the block's newlines arrive escaped.
	for _, line := range strings.Split(strings.ReplaceAll(wire, `\n`, "\n"), "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("the notice carries no line naming %q: %s", substr, wire)
	return ""
}

// t6bd2NoticeAction pulls the ACTION half off a rendered capacity line. The
// renderer's shape is `- {doc}: {size}/{cap} chars, {left} left → {action}`
// (docCapacityLines), so the action is everything after the arrow — which is
// what lets this file compare it whole against the constant instead of hunting
// for keywords inside it.
func t6bd2NoticeAction(t *testing.T, line string) string {
	t.Helper()
	_, action, found := strings.Cut(line, " → ")
	if !found {
		t.Fatalf("a capacity line must carry its action after an arrow: %q", line)
	}
	return action
}

// TestHandoverNoticeTick_ClosuresAreNotRunAfterTheClaim — BLOCKER 1.
//
// 🔴 WHAT WAS ACTUALLY WRONG. decideHandoverNotice has no memory: once an agent
// is past its notice point it returns a signal on EVERY quiet tick, and the
// once-per-session gate (claimHandoverNotice) is asked AFTER that signal has
// been composed. So both text closures — a fold over the offboard document and
// a fold over nine capped documents — ran every 250ms for the rest of the
// session, and every run after the first was thrown away. Measured on this
// branch before the fix: 21.3µs → 574.2µs per tick, 26.9× on an EMPTY station.
// Two comments in sse_bands.go asserted the exact opposite; a comment cannot be
// run, so this counts instead.
//
// It fails if handoverNoticeTick stops asking handoverNoticeSettled first —
// verified by deleting that guard and watching the counters reach 201.
func TestHandoverNoticeTick_ClosuresAreNotRunAfterTheClaim(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, seedMiraID)
	s.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})

	offboardRuns, docRuns := 0, 0
	offboard := func() string { offboardRuns++; return s.offboardText() }
	docCapacity := func() string {
		docRuns++
		return docCapacityLines(s.docCapacityFor(seedMiraID, s.stepNoteCapacityFor(seedMiraID)))
	}

	frame, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, offboard, docCapacity)
	if !ok || len(frame) == 0 {
		t.Fatal("the first tick past the notice point must send the session's one notice")
	}
	if offboardRuns != 1 || docRuns != 1 {
		t.Fatalf("the sending tick must compose the text exactly once: offboard=%d doc=%d",
			offboardRuns, docRuns)
	}

	// The rest of the session. Every one of these ticks is above the notice
	// point, so decideHandoverNotice would still say "send" — the claim is what
	// makes them silent, and the point of this test is that they must be silent
	// WITHOUT paying for the text first.
	for i := 0; i < 200; i++ {
		if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, offboard, docCapacity); ok {
			t.Fatalf("tick %d re-sent the once-per-session notice", i)
		}
	}
	if offboardRuns != 1 || docRuns != 1 {
		t.Fatalf("200 silent ticks composed text they threw away: offboard=%d doc=%d "+
			"(want 1 and 1) — the notice fires once per session, so its cost must too",
			offboardRuns, docRuns)
	}
}

// TestHandoverNoticeTick_ANewSessionStillPaysAndStillSends is the OTHER
// direction of the same guard, and it is not optional: "never runs the closures
// again" and "went permanently mute for this agent" are the same green
// otherwise. A new boot_ts is a new session and is entitled to its own notice.
func TestHandoverNoticeTick_ANewSessionStillPaysAndStillSends(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, seedMiraID)
	s.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})

	runs := 0
	doc := func() string { runs++; return "" }
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, s.offboardText, doc); !ok {
		t.Fatal("first session must be told")
	}
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, s.offboardText, doc); ok {
		t.Fatal("second tick of the SAME session must stay quiet")
	}

	// New session: the agent restarted, its gauge carries a new anchor.
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 2000.0})
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, s.offboardText, doc); !ok {
		t.Fatal("a NEW session must still get its own notice — a cost guard that " +
			"silences the feature has removed the feature")
	}
	if runs != 2 {
		t.Fatalf("the closure must run exactly once per SENDING tick: got %d, want 2", runs)
	}
}

// TestSoftNoticeNamesWhoCanCompactWhatTheReaderCannot — BLOCKER-adjacent, G7.
//
// 🔴 THE MUTANT THIS EXISTS FOR: strip the referral half of the row's action —
// leave the numbers, drop "who can handle it" — and the entire suite stayed
// green, on the very timing this ticket argues is the important one. A reader
// staring at "insight: 12500/15000 chars, 2500 left" with no addressee has
// exactly two options, and one of them (write it itself) answers 403.
//
// The reader here is NOT the compactor, so a green cannot come from the
// sentence accidentally naming its own reader.
//
// 🔴 CORRECTED 2026-08-20. This test used to assert, in its own words, that
// "an insight answers 403 to an ordinary agent" — and it does not. Measured
// with a zero-damage probe (patch_insight with an anchor that cannot exist, so
// the permission gate answers before anything is written):
//
//	patch_insight role_key=<OWN role>     → 400 validation_error  ⇒ WRITABLE
//	patch_insight role_key=<ANOTHER role> → 403 (role-scoped refusal)
//
// The signal was telling readers "you cannot write this one (it answers 403 to
// you)", an agent falsified it in one call within seconds of receiving the
// notice, and this test was pinning the falsehood in place.
//
// ⇒ The property being guarded was always the RIGHT one — a row must hand the
// reader an addressee, not just numbers — but its stated reason was wrong. The
// addressee is there because compacting long-term memory under close-out
// pressure is the failure this whole feature answers, NOT because the reader is
// barred from the document.
func TestSoftNoticeNamesWhoCanCompactWhatTheReaderCannot(t *testing.T) {
	s := t6bd2Server(t)
	reader := t6bd2NoticeReader(t, s)
	t6bd2FillAll(t, s, reader)

	line := t6bd2NoticeLine(t, t6bd2NoticeWire(t, s, reader), "insight (")

	// WHOLE STRING against the CONSTANT (owner ruling 2026-08-20,
	// c-2502de439aaa). The three keyword checks this replaces asked "does it
	// name 銀月", "does it avoid 403/cannot write/not yours to write" and "does
	// it say yourself" — a rewrite could satisfy all three and still be the
	// wrong sentence. What this test is about is WHICH of the three sentences
	// the long-term-memory row got, and equality asks exactly that.
	if action := t6bd2NoticeAction(t, line); action != docCapacityActionSelfMemory {
		t.Fatalf("a long-term-memory row must carry the memory sentence — it "+
			"names who can compact it WITHOUT claiming a permission the reader "+
			"has:\n got %q\nwant %q", action, docCapacityActionSelfMemory)
	}

	// And the CONTRAST is half the property: a row the reader CAN write must
	// not be turned into a referral. If both classes said the same thing, the
	// assertion above would pass for a notice that says one thing to everyone —
	// which is the noise the block is designed not to be.
	own := t6bd2NoticeLine(t, t6bd2NoticeWire(t, s, reader), "task manual SOP (")
	if action := t6bd2NoticeAction(t, own); action != docCapacityActionSelf {
		t.Fatalf("a manual the reader CAN write must tell it to do it itself, "+
			"not send it to somebody else:\n got %q\nwant %q", action, docCapacityActionSelf)
	}
}

// TestSoftNoticeReferralCarriesTheAddressableID — G2.
//
// 🔴 THE MUTANT: drop the "(mira)" and keep 銀月. Everything still reads fine to
// a human and the whole suite stayed green — but 銀月 is a DISPLAY NAME, and
// display names are editable from the roster UI at any time. The id is what
// post_chat / create_reply_card actually take. A referral that names only the
// display name is a referral the reader cannot act on the moment somebody
// renames the member, and nothing anywhere would report that.
func TestSoftNoticeReferralCarriesTheAddressableID(t *testing.T) {
	s := t6bd2Server(t)
	reader := t6bd2NoticeReader(t, s)
	t6bd2FillAll(t, s, reader)

	line := t6bd2NoticeLine(t, t6bd2NoticeWire(t, s, reader), "insight (")
	if !strings.Contains(line, "(mira)") {
		t.Fatalf("the referral must carry the compactor's addressable id, not "+
			"only its editable display name: %s", line)
	}
}

// TestSoftNoticeReferralIDSurvivesARename is the same property proved the way
// it actually breaks: rename the member and the id must still be there. Without
// this, "(mira)" could be read as a second spelling of the display name.
func TestSoftNoticeReferralIDSurvivesARename(t *testing.T) {
	s := t6bd2Server(t)
	reader := t6bd2NoticeReader(t, s)
	t6bd2FillAll(t, s, reader)

	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("seed assistant: %v %v", mira, err)
	}
	mira.Name = "改過名字的銀月"
	if err := s.dal.PutMember(*mira); err != nil {
		t.Fatalf("rename: %v", err)
	}

	line := t6bd2NoticeLine(t, t6bd2NoticeWire(t, s, reader), "insight (")
	if !strings.Contains(line, "(mira)") {
		t.Fatalf("the addressable id must survive a display-name change: %s", line)
	}
}
