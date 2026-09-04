package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The owner's rule, verbatim (2026-08-24): 「下線 → 加速 → 強制。後者一旦發出我們
// 就不該發出前者」—— the three wind-down stages only ever move FORWARD.
//
// 🔴 WHY THESE TESTS ARE NOT OPTIONAL. Before T-6f44 there was no test anywhere
// in this package asking about the ORDER of two stages. TestWindDownKind above
// pins what ONE cause means (soft or final); nothing pinned what happens when a
// second cause lands on a member that is already somewhere. That gap is exactly
// where the defect lived: pressing 重新聚焦 on an agent in 加速停止 answered 200,
// pushed the stage back to 停止, and cleared the deadline it was counting to —
// silently, with the whole suite green.

// The rank must be DERIVED from winddownKindFor, never listed beside it. This
// is the test that makes that true rather than merely intended: it walks the
// same closed cause set the clock tests walk, so a cause added to the constants
// (and to everyWindDownCause, as that list's own comment requires) is ranked by
// this package or fails here. A second hand-maintained table would pass its own
// tests while disagreeing with the clock about the same member.
func TestWindDownLadder_RankIsDerivedFromTheOneJudgementNotASecondList(t *testing.T) {
	for _, op := range everyWindDownCause {
		kind, clocked := winddownKindFor(op)
		rank := winddownStageRankOf(op)
		want := winddownStageStop
		if kind == offboardKindFinal {
			want = winddownStageAccelerated
		}
		if rank != want {
			t.Errorf("%q: rank=%d but winddownKindFor says kind=%q clocked=%v "+
				"(want rank %d) — the ladder and the clock have come apart, which "+
				"is the second-truth-source this rank exists to avoid",
				op, rank, kind, clocked, want)
		}
	}
}

// 停止 landing on a member already in 加速停止. This is the measured defect.
func TestWindDownLadder_RefocusIsRefusedOnAMemberAlreadyAccelerating(t *testing.T) {
	const now = 1_769_904_000.0

	s := newReconcileTestServer(t)
	m := testAgent("m-6f44-ladder")
	l := connectOnline(t, s, m.ID)
	defer drainListener(l)
	m.RefocusSince = now
	m.RefocusOp = refocusOpAcceleratedStop
	putTestMember(t, s, m)

	deadlineBefore := refocusDeadlineOf(m.RefocusSince, s.reconcileConfigLive(), m.RefocusOp)
	if deadlineBefore <= 0 {
		t.Fatalf("setup: 加速停止 must be counting to something, got %v — this test "+
			"cannot show a deadline being cleared if there was never one",
			deadlineBefore)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/members/"+m.ID+"/refocus", nil)
	s.HandleRefocusMemberApiMembersMemberIdRefocusPost(w, r, m.ID)

	if w.Code != http.StatusConflict {
		t.Errorf("refocus on an accelerating member answered %d, want %d — a 200 "+
			"here is the whole defect: the owner gets a success for an action that "+
			"walked the agent backwards", w.Code, http.StatusConflict)
	}

	after, err := s.dal.GetMember(m.ID)
	if err != nil || after == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if after.RefocusOp != refocusOpAcceleratedStop {
		t.Errorf("refocus_op=%q after the refused call, want %q — the row moved "+
			"even though the call was refused", after.RefocusOp, refocusOpAcceleratedStop)
	}
	// The stage is the visible half; the deadline is the half that actually hurt.
	// Assert it separately so a future change that keeps the op but drops the
	// clock cannot pass by agreeing with the assertion above alone.
	deadlineAfter := refocusDeadlineOf(after.RefocusSince, s.reconcileConfigLive(), after.RefocusOp)
	if deadlineAfter != deadlineBefore {
		t.Errorf("deadline moved from %v to %v — an agent that was counting down "+
			"stopped counting, which is the harm the owner's rule names",
			deadlineBefore, deadlineAfter)
	}
}

// The other direction must still work, or the guard is a regression rather than
// a protection: 加速停止 is allowed to land on a member in 停止.
func TestWindDownLadder_ForwardStillMovesAndTheSameStageMayReArm(t *testing.T) {
	const now = 1_769_904_000.0

	softThenFinal := testAgent("m-6f44-forward")
	softThenFinal.RefocusSince = now
	softThenFinal.RefocusOp = refocusOpRefocus
	if !armRefocusEpoch(&softThenFinal, refocusOpAcceleratedStop, now+1) {
		t.Fatal("加速停止 was refused on a member in 停止 — the ladder must go " +
			"FORWARD; a guard that blocks this has inverted the owner's rule")
	}
	if softThenFinal.RefocusOp != refocusOpAcceleratedStop {
		t.Errorf("refocus_op=%q, want %q", softThenFinal.RefocusOp, refocusOpAcceleratedStop)
	}

	// Equal rank is a re-arm, not a step backwards. Deliberately allowed — the
	// owner's sentence is about an EARLIER stage arriving after a later one, and
	// several callers re-stamp the same stage on purpose.
	sameStage := testAgent("m-6f44-rearm")
	sameStage.RefocusSince = now
	sameStage.RefocusOp = refocusOpRefocus
	if !armRefocusEpoch(&sameStage, refocusOpRestartSelf, now+1) {
		t.Error("a same-stage re-arm was refused — the guard is stricter than the " +
			"rule it implements, which makes it a behaviour change nobody asked for")
	}

	// A member with no wind-down open must always accept the first stamp.
	fresh := testAgent("m-6f44-fresh")
	if !armRefocusEpoch(&fresh, refocusOpRefocus, now) {
		t.Error("the FIRST stamp on a member with no epoch was refused — nothing " +
			"below 停止 exists to be walked back from")
	}
}

// 強制停止 is stage 3 and it is a property of the member, not a refocus cause.
// Nothing may re-open a slower wind-down underneath it.
func TestWindDownLadder_ForcedOutranksEveryCause(t *testing.T) {
	const now = 1_769_904_000.0

	forced := testAgent("m-6f44-forced")
	forced.StoppingSince = now
	forced.ForcedStopAt = now + 1
	if got := winddownStageOf(forced); got != winddownStageForced {
		t.Fatalf("setup: stage=%d, want %d (forcedEpochLive is the only way to "+
			"reach stage 3)", got, winddownStageForced)
	}
	for _, op := range everyWindDownCause {
		probe := forced
		if armRefocusEpoch(&probe, op, now+2) {
			t.Errorf("%q re-opened a wind-down on a force-stopped member — the "+
				"last stage must not be replaced by an earlier one", op)
		}
	}
}

// 🔴 THE SECOND GUARD, AND IT IS A SEPARATE ONE. The ladder above governs who
// may overwrite refocus_op. This band never touches that field: it decides from
// the gauge alone, which is why it was the one wind-down path with no guard at
// all. An agent the owner had already put into 加速停止 would, the moment its
// usage crossed the FIRST threshold, be handed a 停止 notice saying there is no
// hurry — while it was counting down to a deadline.
//
// The positive control is the load-bearing half: without it, "stayed quiet"
// and "this test never reached the band at all" are the same observation.
func TestHandoverNoticeBand_SaysNothingToAMemberAlreadyWindingDown(t *testing.T) {
	notice := func() string { return "NOTICE" }

	// POSITIVE CONTROL — same fixture, no wind-down open. This MUST fire, or
	// the silence asserted below proves nothing.
	control, dalC := newGateTestAPI(t)
	if err := seedOutOfBox(dalC); err != nil {
		t.Fatalf("seed: %v", err)
	}
	control.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	control.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})
	if _, ok := control.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); !ok {
		t.Fatal("positive control did not fire: a member with NO wind-down open, " +
			"over the notice point, must be sent the notice. Until this fires, a " +
			"quiet tick below says nothing about the guard")
	}

	// The real case: same everything, member parked in 加速停止.
	s, dal := newGateTestAPI(t)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s.ctxhigh = SseContextHighConfig{HandoverPct: 65, NoticePct: 55}
	s.gauge.Set(seedMiraID, map[string]any{"context_pct": 56.0, "boot_ts": 1000.0})

	m, err := dal.GetMember(seedMiraID)
	if err != nil || m == nil {
		t.Fatalf("read seeded member: %v", err)
	}
	m.RefocusSince = nowSecs()
	m.RefocusOp = refocusOpAcceleratedStop
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("park member in 加速停止: %v", err)
	}
	if err := dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince,
		m.StoppedSince, m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors: %v", err)
	}

	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); ok {
		t.Error("the band sent a 停止 notice to a member already in 加速停止 — " +
			"the agent is counting down to a deadline and was just told there is " +
			"no hurry")
	}

	// 🔴 AND IT MUST NOT HAVE SPENT THE SESSION'S ONE NOTICE. Going quiet by
	// CLAIMING would look identical above while destroying the notice: an agent
	// whose wind-down is later cleared would then never be warned at all.
	m.RefocusSince = 0
	m.RefocusOp = ""
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("clear the wind-down: %v", err)
	}
	if err := dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince,
		m.StoppedSince, m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors: %v", err)
	}
	if _, ok := s.handoverNoticeTick(seedMiraID, RuntimeClaude, notice); !ok {
		t.Error("after the wind-down cleared, the notice never came — the quiet " +
			"tick had claimed it, so silence cost the agent its only warning")
	}
}
