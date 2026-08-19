package main

import (
	"fmt"
	"strings"
	"testing"
)

// T-c996 — 重新聚焦 says there is no countdown, and now there is not one.
//
// 🔴 This file exists because the OLD behaviour was decided by a CONSTANT and
// asserted by nothing: 重新聚焦 opened SOFT, and at SoftOffboardGraceSecs the
// sentence flipped to "you have 120 seconds" while recycleGraceFor's 720 s clock
// really did collect the session. The two halves are separately reachable, so
// they are separately pinned here — a change that fixes the sentence and leaves
// the clock produces something WORSE than the split it removed (an agent told
// there is no deadline, cut off at 720 s with no warning), and that mistake has
// to turn a test red.
//
// Every case runs the arms side by side: the ones that DO carry a countdown are
// the positive controls, so a test that stops discriminating — a field that
// silently stopped being populated, an arm that stopped being reached — fails
// instead of passing vacuously.

// THE LOAD-BEARING HALF: the reconcile tick must not collect an owner-pressed
// 重新聚焦 on time, no matter how long it has been.
//
// 🔴 Mutant measured before this was written: restoring
// `return cfg.SoftOffboardGrace + cfg.RecycleGrace, true` for refocusOpRefocus
// — the exact pre-T-c996 clock — turns THIS test red and, without it, left the
// whole ocserverd suite green.
func TestRefocusIsCollectedByNoClock_ButEveryOtherCauseStillIs(t *testing.T) {
	cfg := defaultReconcileConfig()
	t0 := 1_000_000.0

	obsRefocused := func(op string) memberObservation {
		o := obsOf("m-refocused", DesiredStateOnline, true)
		o.RefocusSince = t0
		o.RefocusOp = op
		return o
	}

	// A year is not a number anyone would pick as a window; it is here to say
	// that no window exists, rather than that this one is generous.
	const aYear = 365 * 24 * 3600.0

	t.Run("重新聚焦 is never timed out, however long it takes", func(t *testing.T) {
		for _, elapsed := range []float64{
			1,
			cfg.RecycleGrace,                         // the old flat clock
			cfg.SoftOffboardGrace,                    // the old sentence flip
			cfg.SoftOffboardGrace + cfg.RecycleGrace, // the old collection, exactly
			aYear,
		} {
			d := reconcileDecide(obsRefocused(refocusOpRefocus), newReconcileState(), cfg, t0+elapsed)
			if d.Command == reconcileCmdStop {
				t.Fatalf("+%.0fs: 重新聚焦 was collected on a clock — the notice it was "+
					"sent says there is none, so this is a silent deadline (%s)",
					elapsed, d.Reason)
			}
		}
	})

	// The positive control, and it is the whole reason the case above means
	// anything: this arm reaches the SAME code path and IS collected. If both
	// went quiet the test above would pass for the wrong reason.
	t.Run("every other cause still gets exactly its 120 seconds", func(t *testing.T) {
		for _, op := range []string{refocusOpContextHigh, refocusOpRestartSelf, memberOpRelocate, memberOpModel} {
			before := reconcileDecide(obsRefocused(op), newReconcileState(), cfg, t0+cfg.RecycleGrace-1)
			if before.Command == reconcileCmdStop {
				t.Fatalf("%s: collected BEFORE its grace elapsed (%s)", op, before.Reason)
			}
			after := reconcileDecide(obsRefocused(op), newReconcileState(), cfg, t0+cfg.RecycleGrace)
			if after.Command != reconcileCmdStop {
				t.Fatalf("%s: must still be collected once its 120s elapses, got %q (%s)",
					op, after.Command, after.Reason)
			}
		}
	})

	// …and the one thing that DOES collect a 重新聚焦 still does: the agent
	// saying it is done. Without this, "never stopped" could be read as "this
	// arm no longer collects at all", which would leave the slot occupied
	// forever.
	t.Run("the agent's own stopped report still collects it, immediately", func(t *testing.T) {
		o := obsRefocused(refocusOpRefocus)
		o.AgentStopped = true
		d := reconcileDecide(o, newReconcileState(), cfg, t0+1)
		if d.Command != reconcileCmdStop {
			t.Fatalf("a refocus-marked member that reported stopped must be collected "+
				"at once, got %q (%s)", d.Command, d.Reason)
		}
	})
}

// THE SENTENCE HALF: 重新聚焦 must never be told a number of seconds, at any
// point in its epoch — and the arms that ARE on a clock must still say theirs.
//
// This asserts the composed sentence, not the classification, because the
// classification is an internal name: an agent acts on what it is told.
func TestRefocusNoticeNeverStartsACountdown_ButAContextLimitStillDoes(t *testing.T) {
	s := newReconcileTestServer(t)

	noticeAt := func(t *testing.T, op string, age float64) string {
		t.Helper()
		m := testAgent("m-notice")
		m.RefocusSince = nowSecs() - age
		m.RefocusOp = op
		putTestMember(t, s, m)
		payload := s.offboardDeltaPayload(m)
		notice, ok := payload["offboard_notice"].(string)
		if !ok || notice == "" {
			t.Fatalf("%s at +%.0fs must carry a notice at all: %+v", op, age, payload)
		}
		return notice
	}

	// Straddling the old flip point deliberately: before T-c996 the first of
	// these was silent about time and the rest promised 120 seconds.
	//
	// T-d6a7 changed the SHAPE of what a clocked arm says (an absolute deadline
	// instead of "120 seconds left"). This asserted a list of literal clauses in
	// response, which is the same mistake one wording later: an independent
	// review added "Time remaining: 74s." here and nothing in the tree went red.
	// assertQuotesNoTime rejects a digit attached to a unit of time (ASCII and
	// CJK) and a clock-shaped span like 00:01:14, read on the composed sentence
	// only — offboard_absolute_deadline_td6a7_test.go states the exact edge.
	for _, age := range []float64{1, SoftOffboardGraceSecs - 1, SoftOffboardGraceSecs, 10 * SoftOffboardGraceSecs} {
		notice := noticeAt(t, refocusOpRefocus, age)
		assertQuotesNoTime(t, fmt.Sprintf("重新聚焦 at +%.0fs", age), notice)
	}

	// The positive control: the causes that really are on the clock must still
	// say when it runs out, or an agent under context pressure would take its
	// time and be cut off.
	if notice := noticeAt(t, refocusOpContextHigh, 1); !strings.Contains(notice, "Your deadline is ") {
		t.Fatalf("a context-limit handover IS on the clock and must say when it "+
			"runs out:\n%s", notice)
	}
}
