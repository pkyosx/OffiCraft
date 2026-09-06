package main

// member_ownerop_winddown_collect_t65_test.go — T-65 包⑤.
//
// The guard over collectWindDownRow's TWO promises, neither of which had one
// before independent review measured that they did not.
//
// The shared body is three lines and both of its promises fail SILENTLY:
//
//	M1  read `prior` AFTER the stamp instead of before — the ordinary way to get
//	    a named return wrong. `prior` becomes `now`, so the one caller that
//	    rolls the latch back "restores" the value it was supposed to undo.
//	M2  make the stamp unconditional. The once-only is gone: a repeat report
//	    keeps receipting `already_reported` — so the receipt still reads right —
//	    while quietly MOVING an anchor the handler's own doc comment promises is
//	    "anchored ONCE (never re-stamped)".
//
// 🔴 MEASURED BEFORE THIS FILE EXISTED (independent review, T-65 包⑤): both
// mutants left the whole 456-test scope GREEN. Not "no test named them" — no
// test anywhere disagreed. The positive control in the same session pins that
// this is absence of a guard rather than absence of reach: flipping the
// `latched` return instead (`return false, prior` → `return true, prior`) does
// go red, on TestStopEffect_StaffIsCollectedThenAlreadyReported_T102. So the
// funnels run the body; nobody was checking what it wrote.
//
// Both blocks below drive production drivers — the real /api/self/stopped
// handler and the real outsource tick — rather than calling collectWindDownRow.
// A test over the three-line body would kill M1 and M2 by construction and
// would leave every call site deletable, which is the split T-14 PR ① paid for.

import (
	"strings"
	"testing"
)

// TestCollectWindDownRowLatchesOnceAndRollsBack is ONE test with two blocks on
// purpose: the two promises are two halves of the same three lines, and a
// reader who breaks either should be sent to the same place.
func TestCollectWindDownRowLatchesOnceAndRollsBack(t *testing.T) {
	// ── M2: the once-only. A repeat report must not move the anchor ──────────
	//
	// TestStopEffect_StaffIsCollectedThenAlreadyReported_T102 already drives
	// this exact pair of calls and asserts the RECEIPT and the FIFO. It stays
	// green under M2, because both of those still read correctly — the anchor is
	// the only thing that moves, and nothing was reading it. That is the gap
	// this block closes, which is also why it is not folded into that test:
	// what it asserts is a different fact about the same two calls.
	t.Run("a repeat stopped-report leaves the anchor exactly where it was", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putWarden(t, s, "mach-a")
		const id = "m-latch-once"
		m := testAgent(id)
		m.DesiredMachineID = "mach-a"
		putTestMember(t, s, m)
		connectOnline(t, s, "mach-a")
		connectOnlineMachine(t, s, id, "mach-a")

		first, _ := reportStoppedReceipt(t, s, id)
		if first.StopEffect != stopEffectCollected {
			t.Fatalf("precondition: the first staff report must collect, got %q",
				first.StopEffect)
		}
		latched := memberStoppedSince(t, s, id)
		if latched <= 0.0 {
			t.Fatalf("precondition: the first report must latch stopped_since, got %v",
				latched)
		}

		second, _ := reportStoppedReceipt(t, s, id)
		if second.StopEffect != stopEffectAlreadyReported {
			t.Fatalf("precondition: the repeat must receipt %q, got %q",
				stopEffectAlreadyReported, second.StopEffect)
		}
		if again := memberStoppedSince(t, s, id); again != latched {
			t.Fatalf("the repeat report MOVED stopped_since: %v → %v. "+
				"HandleReportStoppedApiSelfStoppedPost opens with 「anchors "+
				"stopped_since ONCE (never re-stamped)」, and the once-only that "+
				"makes that true is collectWindDownRow's `<= 0` guard. The receipt "+
				"still said %q, so this is invisible from the wire — which is why "+
				"the assertion is on the anchor.", latched, again, second.StopEffect)
		}
	})

	// ── M1: `prior` is the value from BEFORE the stamp ───────────────────────
	//
	// Reaching the one reader of `prior` needs the collect's rollback arm:
	// respawnWorkerNow refuses (ACTIVE with no kill target) while the session is
	// still ONLINE. newActiveOnlineWorker connects with a BLANK machine claim,
	// so dropping the spawn-target entry leaves resolveWorkerKillTarget with
	// nothing on either of its two roads — the shape collectWorkerHandover's own
	// comment calls "a blank machine claim (no production shape, tokens carry
	// the host)".
	//
	// The driver is the real outsource tick: an ACTIVE + online worker inside a
	// CLOCKED refocus epoch past its grace is what decideUp's recycle arm
	// collects, and reconcileWorkerLiveness executes that through
	// collectWorkerHandover.
	t.Run("a collect that cannot respawn rolls the latch back to zero", func(t *testing.T) {
		api := newReconcileTestServer(t)
		api.noOutsource = true
		id := newActiveOnlineWorker(t, api)

		w, err := api.dal.GetOutsourceWorker(id)
		if err != nil || w == nil {
			t.Fatalf("read back worker: %v", err)
		}
		now := nowSecs()
		// context_high is one of the two CLOCKED causes (winddownKindFor), so
		// the recycle arm fires on the grace rather than waiting for a report —
		// which is what lets this block reach the collect without a stopped
		// report having already latched the anchor it is about to assert on.
		w.RefocusSince = now - 100000.0
		w.RefocusOp = refocusOpContextHigh
		w.StoppedSince = 0.0
		if err := api.dal.PutOutsourceWorker(*w); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		seedWorkerAnchors(t, api, *w)

		// The refusal: no spawn memory, and the SSE claim is blank, so
		// resolveWorkerKillTarget answers "" on an ACTIVE worker.
		delete(api.workerSpawnTarget, id)
		if !api.hub.IsOnline(id) {
			t.Fatal("precondition: the rollback arm this block is about is the " +
				"ONLINE one — the offline arm rolls the whole epoch back through " +
				"clearWorkerRefocus instead and never reads `prior`")
		}
		if got := api.resolveWorkerKillTarget(id); got != "" {
			t.Fatalf("precondition: respawnWorkerNow must REFUSE, but a kill "+
				"target resolved to %q", got)
		}

		api.runOutsourceTick(now)

		after, err := api.dal.GetOutsourceWorker(id)
		if err != nil || after == nil {
			t.Fatalf("read back worker after the tick: %v", err)
		}
		// 🔴 THE POSITIVE CONTROL, AND IT IS NOT OPTIONAL HERE. "stopped_since
		// == 0" is ALSO what a tick that never reached the collect leaves
		// behind, so without this the block would pass by doing nothing —
		// exactly the green-for-the-wrong-reason shape this package keeps
		// finding. respawnWorkerNow stamps this receipt on the refusal path and
		// nowhere else, and it is durable (SetMemberOpReceipt), so reading it
		// back proves the collect ran, latched, and then hit the refusal whose
		// rollback the assertion below is about.
		if !strings.Contains(after.LastOpReason, spawnReasonRespawnDeferred) {
			t.Fatalf("the tick never reached the deferred-respawn arm — "+
				"last_op_reason = %q, want it to contain %q. The stopped_since "+
				"assertion below would then be green for having measured nothing.",
				after.LastOpReason, spawnReasonRespawnDeferred)
		}
		if after.StoppedSince != 0.0 {
			t.Fatalf("the deferred collect did not roll the latch back: "+
				"stopped_since = %v, want 0. collectWorkerHandover restores "+
				"`prior` — the value collectWindDownRow read BEFORE it stamped — "+
				"so a `prior` read AFTER the stamp writes `now` back and the "+
				"rollback silently confirms the latch instead of undoing it. A "+
				"latched anchor here reads to workerHasStateToFlush as 「this "+
				"epoch is already collected」, and the grace arm never retries.",
				after.StoppedSince)
		}
		if after.RefocusSince <= 0.0 {
			t.Fatalf("precondition failed the OTHER way: the epoch was torn down "+
				"(refocus_since = %v), so this ran the session-gone arm and "+
				"proves nothing about `prior`", after.RefocusSince)
		}
	})
}

func memberStoppedSince(t *testing.T, s *apiServer, id string) float64 {
	t.Helper()
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back member %s: %v", id, err)
	}
	return m.StoppedSince
}
