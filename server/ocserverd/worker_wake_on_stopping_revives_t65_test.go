package main

// worker_wake_on_stopping_revives_t65_test.go — T-65 包④'s RESCUE-PATH proof.
//
// 🔴 THE QUESTION THIS FILE EXISTS TO ANSWER, and it was an INFERENCE before it
// was a measurement. WorkerDetailPanel.tsx puts `stoppingNow` on the 喚醒 side
// of its dialog split, and says so verbatim: 「/restart is reachable there (its
// guard refuses only a worker that is BOTH not held down and online), so the
// confirm really does revive it」. That sentence was written while 喚醒 on a
// live worker meant kill+respawn — the revival WAS the kill. 包④ deleted the
// kill. If nothing else brings the worker back, the owner presses the one word
// that promises 「revive」 on a worker that is mid-close-out, gets a 200, and
// then NOTHING happens — a silent failure, which is strictly worse than the bug
// 包④ closes, because the old behaviour at least did something.
//
// So the whole timeline is pinned here, end to end, through the PRODUCTION
// entry points only (the HTTP handler, the agent's own report, runOutsourceTick):
//
//	1. an ACTIVE worker mid-停止: desired offline + stopping_since stamped +
//	   the session STILL UP. That triple is exactly what the wire calls
//	   presence="stopping" (wire.go), which is what the panel reads as
//	   `stoppingNow` — asserted below rather than assumed, so this test cannot
//	   drift onto some neighbouring state that merely looks like it.
//	2. 喚醒 → the 包④ live arm: desired flips to online, stopping_since is
//	   cleared, NOTHING is dispatched and the session is left running its
//	   close-out. The owner did not interrupt the work; that is the point.
//	3. the agent finishes and files report_stopped. It lands on
//	   workerReportStopped's BARE LATCH — neither collect arm matches, because
//	   step 2 removed both of their preconditions (the 停止 arm needs desired
//	   offline; the 換手 arm needs refocus_since > 0). Its own comment is the
//	   claim this test verifies: 「no offline intent to hold the worker down, so
//	   the FSM's next pass simply starts it again」.
//	4. the next outsource tick → a worker_start REALLY GOES OUT.
//
// Step 4 is the load-bearing one and the reason this is a whole-timeline test
// rather than four unit tests: every step above changes an input that step 4
// reads, and a mutant in ANY of them (a stopping_since left standing, a
// desired_state left offline, a report that latches an epoch it should not)
// shows up here as "the worker never comes back" — the one symptom the owner
// would actually see.

import (
	"net/http"
	"testing"
)

func TestWakeOnAStoppingWorkerBringsItBackAfterTheCloseOut(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // this test drives runOutsourceTick itself
	// online=false so THIS test owns the session listener and can end it at the
	// exact moment the agent finishes — which is the whole middle of the
	// timeline. newActiveOnlineWorker keeps its listener to itself, and a second
	// listener would not take the worker offline when dropped.
	workerID := newActiveWorker(t, api, false)
	session, err := api.hub.Connect(workerID, "")
	if err != nil {
		t.Fatalf("connect the worker's own session: %v", err)
	}

	// ── 1. mid-停止, session still working ────────────────────────────────────
	const stoppingAt = 1_000.0
	seed, _ := api.dal.GetOutsourceWorker(workerID)
	seed.DesiredState = DesiredStateOffline // 停止 held it down
	seed.StoppingSince = stoppingAt         // the graceful epoch is OPEN
	seed.StoppedSince = 0.0                 // the agent has NOT finished yet
	seed.RefocusSince = 0.0                 // a 停止 stamps no refocus epoch
	seed.RefocusOp = ""
	putWorkerFixture(t, api, *seed)

	// FIXTURE PROOF, not an assumption: the state the panel calls `stoppingNow`
	// is a DERIVED presence, and if this fixture produced "stopped" or "offline"
	// instead, every sentence above would be about a different worker.
	dto := newOutsourceWorkerDTO(*seed, nil, outsourceWorkerProjection{
		now: stoppingAt + 1, online: api.hub.IsOnline(workerID)})
	if dto.Presence != "stopping" {
		t.Fatalf("fixture presence = %q, want \"stopping\" — this test is about the "+
			"state WorkerDetailPanel.tsx calls `stoppingNow` (presence === \"stopping\"), "+
			"which is the ONE state where 喚醒 is offered on a worker whose session is "+
			"still alive. Any other presence and the timeline below is about something "+
			"the owner cannot actually reach from that dialog.", dto.Presence)
	}
	api.hub.DrainWardenCommands(ServerSelfHost) // ignore fixture noise

	// ── 2. 喚醒 — the 包④ live arm ────────────────────────────────────────────
	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("喚醒 on a stopping worker: %d %s — it is never refused, and the "+
			"panel offers it precisely here", rec.Code, rec.Body.String())
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("喚醒 dispatched %d frame(s) on a worker whose session is still "+
			"winding down. 包④'s live arm sends nothing: the close-out the agent is "+
			"running right now is exactly the work the owner must not lose", got)
	}
	if !api.hub.IsOnline(workerID) {
		t.Fatal("the close-out session was killed by 喚醒 — the whole of 包④ is that " +
			"it is not")
	}
	afterWake, _ := api.dal.GetOutsourceWorker(workerID)
	// These two are the ONLY things that carry the wake forward into step 4:
	// nothing else about this press survives the request. If either regresses,
	// the START at the end never happens — desired offline is refused by the
	// tick's own gate (outsource_sched.go: `fresh.DesiredState != offline`), and
	// a surviving stopping_since keeps gracefulStopEpochOpen true so the agent's
	// report is COLLECTED (kill, never re-spawn) instead of merely latched.
	if afterWake.DesiredState != DesiredStateOnline {
		t.Fatalf("desired_state = %q after 喚醒, want %q. This flag IS the revival: "+
			"the worker comes back because the reconcile tick sees an online intent "+
			"with no session, not because anything was dispatched today",
			afterWake.DesiredState, DesiredStateOnline)
	}
	if afterWake.StoppingSince != 0 {
		t.Fatalf("stopping_since = %v after 喚醒, want 0. Left standing it keeps the "+
			"graceful 停止 epoch OPEN, and the agent's report below would then take "+
			"workerReportStopped's 停止 arm — collectWorkerStop, which kills and "+
			"deliberately never re-spawns. The worker would be gone for good, on a "+
			"200, from the button that says 喚醒", afterWake.StoppingSince)
	}

	// ── 3. the agent finishes its close-out and reports ──────────────────────
	_, effect, err := api.workerReportStopped(workerID, triggerServer)
	if err != nil {
		t.Fatalf("report_stopped: %v", err)
	}
	if effect != stopEffectRecordedOnly {
		t.Errorf("stop effect = %q, want %q. Step 2 removed the preconditions of "+
			"BOTH collect arms (the 停止 arm needs desired offline, the 換手 arm needs "+
			"refocus_since > 0), so this report is a RECORD and nothing is owed on "+
			"it. %q would mean a kill went out; %q would mean a tick is expected to "+
			"collect an epoch that no longer exists — and the receipt the owner reads "+
			"would be promising him a collection nobody is coming to do.",
			effect, stopEffectRecordedOnly, stopEffectCollected,
			stopEffectLatchedForCollect)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("the agent's own report dispatched %d frame(s); the bare latch "+
			"dispatches nothing", got)
	}

	// The session ends because the agent ended it — nobody killed it.
	api.hub.Disconnect(session)
	if api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the session must be gone before the tick, or the tick sees " +
			"a converged online worker and the START below is vacuous")
	}

	// ── 4. the next tick must REALLY re-dispatch a start ─────────────────────
	api.runOutsourceTick(stoppingAt + 500)
	var rpcs []string
	for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
		rpc, _ := decodeWardenFrame(t, f.Frame)
		rpcs = append(rpcs, rpc)
	}
	starts := 0
	for _, rpc := range rpcs {
		if rpc == reconcileCmdStart {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("the tick dispatched %v — want exactly one %q.\n\n"+
			"🔴 THIS IS THE CELL THE WHOLE FILE EXISTS FOR. Before T-65 包④ a 喚醒 "+
			"pressed on a stopping worker revived it by KILLING it and spawning the "+
			"replacement in the same breath. 包④ deleted that kill deliberately — and "+
			"if this assertion is red, it deleted the revival with it: the owner "+
			"presses 喚醒 on a mid-close-out worker, is answered 200, watches the "+
			"close-out finish, and then the worker simply never comes back, with "+
			"nothing red and nothing on the screen saying so. WorkerDetailPanel.tsx "+
			"promises the opposite in as many words (「the confirm really does revive "+
			"it」), so the panel would be lying too.", rpcs, reconcileCmdStart)
	}
}
