package main

// worker_restart_live_session_noop_t65_test.go — T-65 包④'s main guard: 喚醒
// pressed on an outsource worker whose SESSION IS STILL RUNNING must leave that
// session, and everything describing it, exactly as it found them.
//
// owner 2026-09-06 (rc-1f591528a6d0 圈 [0]): 「收斂成『正在跑就不動它』；真的要
// 強制重來再另外給一個動作」. The two cockpit panels have said the same word since
// 2026-07-31 and meant opposite things: 正職 活化 on a live member reaches
// reconcile, gets `online: converged` and sends no frame; 外包 重啟 went to
// respawnWorkerNow, which killed the session before dispatching the next one.
// Pressed on a worker that had been writing for half an hour, the unwritten half
// was gone, and nothing on the screen said which of the two you were pressing.
//
// 🔴 FOUR SEPARATE FACTS, AND THE LAST TWO ARE THE ONES A NAIVE FIX LOSES.
// "Do not restart it" is easy to read as "clear the row and skip the dispatch",
// and that version passes any test that only counts frames:
//
//	(a) NO FRAME IS DISPATCHED — neither the stop nor the start. Counted as
//	    RPCs, not as a bare length, so a failure says which half leaked.
//	(b) THE SESSION IS STILL THERE afterwards. (a) alone cannot see a kill that
//	    went out by some other road than the warden FIFO this drains.
//	(c) refocus_since / refocus_op / stopped_since ARE BIT-FOR-BIT WHAT THEY
//	    WERE. These three date the epoch of the session that is STILL UP — a
//	    加速停止 or a 換手 in flight right now. The offline arm clears them
//	    because there a session really is being replaced; clearing them HERE
//	    cancels a live wind-down silently, on a 200, from the one verb the owner
//	    pressed in order to leave the worker alone. 正職 活化 does not touch
//	    these three either, and that parity is the whole point.
//	(d) activation_pending IS FALSE. ownerOpOutcome's zero value answers
//	    Pending()==true, so the arm that dispatches nothing on purpose would
//	    otherwise tell the owner his 喚醒 was decided but never delivered — the
//	    opposite of what happened, and a badge he would press again.
//
// The seed puts REAL non-zero values in all three anchors on purpose. With them
// at zero, "the handler cleared them" and "the handler left them" produce the
// same literal, and (c) would be blind to exactly the mutant it exists to kill.

import (
	"net/http"
	"testing"
)

func TestRestartALiveWorkerIsACompleteNoOp(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// A 換手 that is mid-flight on the session that is up right now: the exact
	// thing 包④ says a 喚醒 must not cancel.
	seed, _ := api.dal.GetOutsourceWorker(workerID)
	seed.RefocusSince = 1000.0
	seed.RefocusOp = refocusOpRefocus
	seed.StoppedSince = 1002.0
	seed.StoppingSince = 1001.0
	seed.WakingSince = 1004.0
	if err := api.dal.PutOutsourceWorker(*seed); err != nil {
		t.Fatalf("seed the live worker's in-flight epoch: %v", err)
	}
	// The four wind-down anchors have a sole writer since T-55 — a whole-row
	// PutOutsourceWorker silently drops them, and the assertions below would then
	// be made against a state that was never planted.
	seedWorkerAnchors(t, api, *seed)

	before, _ := api.dal.GetOutsourceWorker(workerID)
	if before.RefocusSince == 0 || before.RefocusOp == "" || before.StoppedSince == 0 {
		t.Fatalf("fixture: the three anchors must actually be planted, got "+
			"refocus_since=%v refocus_op=%q stopped_since=%v — with them at zero "+
			"'left alone' and 'cleared' are the same value and this test proves "+
			"nothing", before.RefocusSince, before.RefocusOp, before.StoppedSince)
	}
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: this test is ONLY about the live arm; without a session " +
			"the handler takes the other branch and every assertion below is vacuous")
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("喚醒 on a live worker: %d %s — it is never refused (T-ed79 #10)",
			rec.Code, rec.Body.String())
	}
	body := workerBody(t, rec)

	// (a) nothing was dispatched, and the failure names which half leaked.
	var rpcs []string
	for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
		rpc, _ := decodeWardenFrame(t, f.Frame)
		rpcs = append(rpcs, rpc)
	}
	if len(rpcs) != 0 {
		t.Errorf("喚醒 dispatched %v on a worker that was already running. It must "+
			"send NOTHING: a %s displaces the session the owner pressed the button "+
			"to keep, and a %s on top of a live one is the double-spawn the warden's "+
			"local clobber-guard then has to refuse.", rpcs, reconcileCmdStop,
			reconcileCmdStart)
	}

	// (b) the session itself survived — a kill that took some road other than the
	// FIFO drained above is invisible to (a).
	if !api.hub.IsOnline(workerID) {
		t.Error("the live session is GONE after a 喚醒. That is the whole bug: the " +
			"owner pressed the word that means 「keep running」 on 正職 and it ended " +
			"the session on 外包.")
	}

	// (c) the three epoch anchors are bit-for-bit unchanged. Deliberately NOT
	// asserted as "non-zero": the mutant that matters re-stamps them to now just
	// as much as the one that zeroes them, and either way the in-flight wind-down
	// the agent was given a deadline for stops being the one it was told about.
	after, _ := api.dal.GetOutsourceWorker(workerID)
	for _, a := range []struct {
		name       string
		got, want  any
		whyItStays string
	}{
		{"refocus_since", after.RefocusSince, before.RefocusSince,
			"it dates the 換手 epoch of the session that is STILL UP"},
		{"refocus_op", after.RefocusOp, before.RefocusOp,
			"the cause travels with its epoch, and that epoch has not ended"},
		{"stopped_since", after.StoppedSince, before.StoppedSince,
			"it is this live session's 收口 latch, not a leftover from a replaced one"},
	} {
		if a.got != a.want {
			t.Errorf("喚醒 changed %s: %v → %v. Nothing was replaced, so there was "+
				"nothing to reset — %s. Clearing it cancels a running 加速停止 or 換手 "+
				"silently, on a 200, from the verb the owner pressed in order to leave "+
				"this worker alone. 正職 活化 does not touch it either "+
				"(clearMemberHandoverMarker: it clears stopping_since and waking_since "+
				"and deliberately clears NEITHER refocus_since NOR stopped_since).",
				a.name, a.want, a.got, a.whyItStays)
		}
	}

	// (d) the owner is not told his press is still in flight.
	if body["activation_pending"] == true {
		t.Error("activation_pending=true on a 喚醒 that had nothing to deliver. " +
			"「scheduled, not yet landed」 is false of this arm — the session it " +
			"wanted is already up — and a pending badge here invites the owner to " +
			"press again, which is how he would eventually find the one press that " +
			"DOES kill it. ownerOpOutcome{AlreadyRunning:true} exists for this, and " +
			"Pending() must keep reading it (`!Dispatched && !AlreadyRunning`).")
	}
}
