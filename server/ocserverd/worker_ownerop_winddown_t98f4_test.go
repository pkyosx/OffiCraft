package main

// worker_ownerop_winddown_t98f4_test.go — T-98f4 rule 2:
// 「我建議所有換手都可以給他機會收尾」.
//
// relocate (改機器) / restart (重啟) / 換 model all funnel through
// respawnWorkerForOwnerOp, and all three used to kill the session on the spot:
// no refocus stamp, no SOP 預告, no grace. 換手 has had the full wind-down since
// T-ea82, and from the worker's side the four events are the same one — this
// session ends, a new one continues the task.
//
// The second half of the owner's ask is 「有東西要存才等,沒有就立刻走」, so this
// file also pins the FAST paths. Which cases are fast is a stated criterion, not
// a guess — see workerHasStateToFlush. (The companion predicate this line used
// to name, ownerOpDisplacesTheSession, was deleted in T-65 包④ — see the 🔴 note
// above TestOwnerOp_RestartNeverWindsDown.)
//
// ─────────────────────────────────────────────────────────────────────────────
// THE DECISION TABLE (written because BOTH HIGH defects in this票 were mis-drawn
// boundaries of the SAME predicate — the 收口-window finding missed a state, and
// the STALE-LATCH finding, whose cause was the first one's own fix, drew the
// range too wide). Anyone changing workerHasStateToFlush: find your change in
// this table first. A cell you cannot state an expectation for is where the
// third defect lives.
//
// 🔴 THE TWO FINDINGS ARE NAMED, NOT NUMBERED (T-170e stage 2 ①). They used to
// be "round 2" and "round 3" here and "round-1" and "round-2" in the staff twin
// (member_ownerop_winddown.go and its test) — one ordinal apart, for the same
// two defects, and the repo's own history does not settle it: the commit that
// added the epoch scoping calls the arm it repaired 「第一輪 HIGH」. An ordinal
// only the review transcript knows is not something a comment can keep true, so
// both files now name the finding by what it was.
//
// TWO UPSTREAM GATES run BEFORE the predicate is even consulted, and they
// dominate it (respawnWorkerForOwnerOp):
//
//	G1  desired_state == offline   → held_down receipt, NOTHING starts. An owner
//	                                 停止 outranks every other owner verb.
//	                                 (TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt)
//	G2  op == restart              → immediate. 重啟 is not a wind-down CAUSE at
//	                                 all — it is a kill+respawn: it does not ask
//	                                 the current session to flush and hand over,
//	                                 it DISPLACES it (respawnWorkerForOwnerOpNow
//	                                 → respawnWorkerNow kills the session on the
//	                                 resolved target BEFORE it re-dispatches), so
//	                                 there is nothing to 預告 to. NOT because the
//	                                 worker must already be stopped — its handler
//	                                 has NO desired-offline gate and answers 200
//	                                 on a live, mid-加速停止 worker
//	                                 (api_outsource.go). 🔴 T-65 包④: on a LIVE
//	                                 session that handler no longer calls this
//	                                 funnel at all — 喚醒 is a no-op there — so
//	                                 the ownerOpDisplacesTheSession deny-list
//	                                 this row used to name is deleted.
//	                                 (TestOwnerOp_RestartNeverWindsDown)
//
// Then the predicate itself, over its four inputs. `active` is Status ==
// WorkerStatusActive; `online` is hub.IsOnline; `refocus` / `stopped` are the
// row's refocus_since / stopped_since anchors:
//
//	#   active online refocus stopped │ verdict   │ why
//	──────────────────────────────────┼───────────┼──────────────────────────────
//	1     Y      Y       0       0    │ WIND DOWN │ the ordinary case: a running
//	                                  │           │ session, no handover in
//	                                  │           │ progress. Only the agent can
//	                                  │           │ know if it has unsaved work,
//	                                  │           │ so ask it (ceiling, not a
//	                                  │           │ fixed wait).
//	2     Y      Y       0      >0    │ WIND DOWN │ ⚠️ ROUND-3 DEFECT CELL. A
//	                                  │           │ stopped_since with NO epoch is
//	                                  │           │ a leftover from an ordinary
//	                                  │           │ 停止 (workerReportStopped's
//	                                  │           │ else arm) that nothing clears.
//	                                  │           │ It is not a collected
//	                                  │           │ handover. Reading it as one
//	                                  │           │ killed every later verb on
//	                                  │           │ that worker, forever.
//	                                  │           │ (_OrdinaryStopRestartStillWindsDownLater)
//	3     Y      Y      >0       0    │ WIND DOWN │ a grace window is OPEN and NO
//	                                  │           │ kill has gone out yet. Safe to
//	                                  │           │ re-stamp: the new pin/model is
//	                                  │           │ already on the row, so the
//	                                  │           │ pending collect carries it.
//	4     Y      Y      >0      >0    │ IMMEDIATE │ ⚠️ ROUND-2 DEFECT CELL. THIS
//	                                  │           │ epoch is collected: kill+start
//	                                  │           │ already went out with the OLD
//	                                  │           │ values, and the dying session
//	                                  │           │ only looks online until its
//	                                  │           │ warden reaps it. Opening a
//	                                  │           │ second wind-down here
//	                                  │           │ dispatches NOTHING and the
//	                                  │           │ in-flight boot then discards
//	                                  │           │ the epoch.
//	                                  │           │ (_VerbAfterTheCollectIsNotSwallowed)
//	5-8   Y      N     any     any    │ IMMEDIATE │ D6: nothing can hear the 預告
//	                                  │           │ and nothing exists to flush,
//	                                  │           │ so waiting burns the whole
//	                                  │           │ deadline for certain.
//	                                  │           │ (_NoLiveSessionTakesEffectImmediately)
//	9-16  N     any    any     any    │ SAME AS   │ 🔴 T-4595: `active` IS NO
//	                                  │ 1-8       │ LONGER AN INPUT. It used to
//	                                  │           │ short-circuit the whole
//	                                  │           │ predicate to IMMEDIATE,
//	                                  │           │ because assigned (activated_ts
//	                                  │           │ == 0) meant the get_my_task
//	                                  │           │ claim never happened ⇒
//	                                  │           │ provably no task state. That
//	                                  │           │ tool is retired and the flip
//	                                  │           │ moved to report_waking, the
//	                                  │           │ FIRST boot verb, so `active`
//	                                  │           │ now proves only that the
//	                                  │           │ session said hello. Reading it
//	                                  │           │ as proof of an empty session
//	                                  │           │ would be reading a stale
//	                                  │           │ proof, so rows 9-16 collapsed
//	                                  │           │ into 1-8: an un-claimed but
//	                                  │           │ ONLINE worker winds down.
//	                                  │           │ (_UnclaimedButOnlineStillWindsDown)
//
// stopping_since is deliberately NOT an input: a worker that has announced it is
// stopping still owes a report_stopped, so it is mid-flush, not done.
//
// Cells 5-8 are collapsed because the earlier conjunct short-circuits — that is a
// claim about the code, and it is why that row needs one test rather than four.
// Cells 1-4 are each pinned individually, because those four are exactly where
// the boundary has been drawn wrong twice.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── the wind-down applies ────────────────────────────────────────────────────

// TestOwnerOp_RelocateWindsDownInsteadOfKilling: 改機器 on a live worker opens
// the window rather than shooting the session. The pin is persisted FIRST, so
// the collect's respawn lands on the new machine — the move still happens, it
// just no longer discards whatever the session had not written down.
func TestOwnerOp_RelocateWindsDownInsteadOfKilling(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	l, err := api.hub.Connect(workerID, "") // takeover: read the 預告 off this one
	if err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")

	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.DesiredMachineID != "m-elsewhere" {
		t.Fatalf("relocate must persist the pin immediately, got %q", w.DesiredMachineID)
	}
	if w.RefocusSince <= 0 {
		t.Fatal("relocate on a live worker must open a wind-down (refocus_since stamped)")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the wind-down must not kill the old session yet, got %d frames", got)
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 0 {
		t.Fatalf("nothing may start on the new machine while the old one winds down, got %d", got)
	}
	nudged := false
	for frame := l.pop(); frame != nil; frame = l.pop() {
		if strings.Contains(string(frame), `"topic":"member"`) &&
			strings.Contains(string(frame), workerID) {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("relocate must fan the SOP 預告 at the worker's own session")
	}

	// It finishes and says so → the move completes NOW, onto the pinned machine.
	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the shared FSM does the collect on the
	// next tick. The move itself is unchanged — respawnWorkerNow still starts on
	// whatever machine the row is pinned to when the collect runs.
	workerTickPass(t, api, workerID, nowSecs())
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
		t.Fatalf("the collect must kill the OLD session (1 stop), got %d frames", got)
	}
	starts := api.hub.DrainWardenCommands("m-elsewhere")
	if len(starts) != 1 {
		t.Fatalf("the collect must start on the NEW machine, got %d frames", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
}

// TestOwnerOp_WindDownRunsNoClock_AndEndsOnTheWorkersOwnReport: an owner verb
// is a 停止 (T-ed79), so NOTHING collects it on a clock — not at
// StoppingTimeoutSecs, not later. This test used to assert the opposite ("the
// deadline is the ceiling"), which was true of the FALLTHROUGH that put every
// unnamed cause on the clock, never of a ruling.
//
// The other half is here in the same test on purpose: "no clock" must not be
// readable as "never ends". The 收口 is the worker's own stopped report (and,
// on the arm this fixture cannot reach without tearing the socket down, its
// session dying — the grace-offline branch of autoHandoverWorker).
func TestOwnerOp_WindDownRunsNoClock_AndEndsOnTheWorkersOwnReport(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	w, _ := api.dal.GetOutsourceWorker(workerID)

	// The old ceiling, and far past it: the worker is still working its SOP and
	// nothing may be dispatched at it.
	for _, elapsed := range []float64{
		StoppingTimeoutSecs - 1, StoppingTimeoutSecs + 1, 100 * StoppingTimeoutSecs,
	} {
		workerTickPass(t, api, w.ID, w.RefocusSince+elapsed)
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("+%.0fs after 換 model: nothing may be dispatched (this op runs "+
				"no clock), got %d frames", elapsed, got)
		}
	}

	// …and the worker's own report IS the 收口: stop + start, at once.
	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the shared FSM does the collect on the
	// next tick. The move itself is unchanged — respawnWorkerNow still starts on
	// whatever machine the row is pinned to when the collect runs.
	workerTickPass(t, api, workerID, nowSecs())
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
		t.Fatalf("the worker's own stopped report must collect it (stop+start), "+
			"got %d frames", got)
	}
}

// ── the fast paths (有東西要存才等,沒有就立刻走) ────────────────────────────

// TestOwnerOp_NoLiveSessionTakesEffectImmediately: nothing can hear the 預告 and
// nothing exists to flush, so waiting would burn the whole deadline for certain.
func TestOwnerOp_NoLiveSessionTakesEffectImmediately(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, session already gone
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
		t.Fatal("a worker with no live session must not be made to wait out a wind-down")
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 1 {
		t.Fatalf("the move must take effect immediately, got %d frames on the new machine", got)
	}
}

// TestOwnerOp_UnclaimedButOnlineStillWindsDown is the INVERSE of the test that
// used to stand here, and the inversion is the point (T-4595).
//
// Before: `assigned` (activated_ts == 0) proved the worker had never been handed
// its task content, because the flip WAS the get_my_task claim — a tool call the
// agent makes after its runtime is up and it has started working. So an owner
// verb on such a worker took effect immediately.
//
// Now: get_my_task is retired and that flip lives on report_waking, the FIRST
// verb of the boot sequence. `assigned` therefore means "has not said hello yet",
// which says nothing about unsaved work, and `active` no longer proves the
// opposite either. Keeping the arm would mean shooting a working session on the
// strength of a proof that has stopped proving anything.
//
// 🔴 THIS IS THE SENTINEL FOR THAT REMOVAL: restoring the
// `w.Status == WorkerStatusActive &&` conjunct in workerHasStateToFlush fails
// HERE, on this test's own two assertions (no epoch stamped / a frame dispatched
// on the spot). The direction is the one member_ownerop_winddown.go argues for
// staff: err toward the grace, and the grace is a CEILING — this session ends the
// instant it answers report_stopped.
func TestOwnerOp_UnclaimedButOnlineStillWindsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedLiveWorkerEnv(t, api)
	workerID := assignOneWorker(t, api) // stays 'assigned'
	if _, err := api.hub.Connect(workerID, ""); err != nil {
		t.Fatalf("connect worker SSE: %v", err) // ONLINE, but never claimed
	}
	api.workerSpawnTarget[workerID] = ServerSelfHost
	api.hub.DrainWardenCommands(ServerSelfHost)

	// Pin it somewhere real so the relocate has a destination to dispatch to.
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.RefocusSince <= 0 {
		t.Fatal("an ONLINE worker must wind down even when it has not claimed its " +
			"task: since T-4595 the claim rides report_waking (the first boot verb), " +
			"so `assigned` no longer proves the session has nothing to save")
	}
	// The pin is persisted up front, so the pending collect lands on the new
	// machine — the move still happens, it is only no longer instantaneous.
	if w.DesiredMachineID != "m-elsewhere" {
		t.Fatalf("the owner's destination must be on the row before the collect: %+v", w)
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 0 {
		t.Fatalf("a wind-down dispatches NOTHING yet — the 收口 belongs to the "+
			"worker's report_stopped or the grace deadline; got %d frames", got)
	}
}

// TestOwnerOp_RestartNeverWindsDown: 重啟 skips the wind-down because it is not a
// wind-down CAUSE — it is a kill+respawn. It does not ask the current session to
// flush and hand over; it DISPLACES it (respawnWorkerForOwnerOp →
// respawnWorkerForOwnerOpNow → respawnWorkerNow kills the session on the resolved
// target BEFORE it re-dispatches), so an SOP 預告 would be fanned at a session
// that is going away regardless. 改機器 / 換 model are the opposite verb — "the
// same session's work must survive this change" — which is what the 預告 + window
// buys them. This is the ONE deny-listed verb; a new verb added later gets the
// wind-down by default.
//
// 🔴 NOT because 重啟 can only arrive at a worker the owner has already stopped.
// It can arrive at ANY live worker: its handler has exactly two preconditions —
// the row exists and it is not released — and NO desired-offline gate, so pressed
// on a worker with desired_state="online" that is mid-加速停止 it answers 200.
//
// ⚠️ T-65 包④ SPLIT THE HANDLER AND THE SENTENCE ABOVE NOW ONLY DESCRIBES ONE
// ARM OF IT. 喚醒 no longer displaces a live session at all: it reaches
// respawnWorkerForOwnerOp only when `!s.hub.IsOnline(id)` (api_outsource.go, the
// `if !sessionAliveReceipt` block). So "restart never winds down" has to be
// asserted on BOTH arms, and this test does that in two subtests:
//
//   - live: the funnel is not entered at all, so nothing may be stamped and
//     nothing may be dispatched — a 喚醒 that opened a 預告+grace on a session
//     the owner pressed the button in order to LEAVE ALONE is the same bug as
//     one that killed it, wearing a politer hat.
//   - not live: the funnel IS entered, with ownerOpRestart, and must come out
//     on the immediate arm.
//
// 🔴 THE DENY-LIST THIS TEST USED TO STAND BESIDE IS GONE, and how it went is
// worth the paragraph. `ownerOpDisplacesTheSession` became unreachable the
// moment 包④ landed: its only caller was the `!displaces && workerHasStateToFlush(w)`
// line, workerHasStateToFlush is `online && …`, and the only arm that still calls
// the funnel with ownerOpRestart is the one where online is false — so the second
// operand was false whatever the first said. It was MEASURED before it was
// removed (neutering it to `return false` left this whole set green), and it was
// removed rather than annotated, because a guard whose removal cannot change an
// answer is not a guard — it is a sentence the next reader believes.
//
// So the two subtests below assert the BEHAVIOUR (no wind-down, either arm),
// which is what the deny-list existed to produce and what now actually holds it:
// the handler gate in api_outsource.go, pinned by
// TestRestartALiveWorkerIsACompleteNoOp.
func TestOwnerOp_RestartNeverWindsDown(t *testing.T) {
	// The fixture presses 停止 first to manufacture the race where the session is
	// dying but has NOT been reaped yet, so a state-only criterion would still
	// read it as online. The prior 停止 is the test's setup, NOT the precondition
	// the rule rests on.
	t.Run("live session — nothing stamped, nothing sent", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)

		postWorker(t, api, workerID, "stop", nil,
			api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
		api.hub.DrainWardenCommands(ServerSelfHost)
		if !api.hub.IsOnline(workerID) {
			t.Fatal("fixture: the session must still look online for this test to bite")
		}
		// The predicate a wind-down would turn on says YES about this worker, so a
		// 喚醒 that fell into the wind-down arm really would open one. Without this
		// control, "no wind-down" and "nothing could have wound down anyway" are
		// the same green.
		pre, _ := api.dal.GetOutsourceWorker(workerID)
		if !hasUncollectedOnlineOwnerOpState(pre.RefocusSince, pre.StoppedSince, true) {
			t.Fatal("control: this fixture must have something to flush, otherwise " +
				"the assertions below cannot tell a skipped wind-down from an " +
				"impossible one")
		}

		rec := postWorker(t, api, workerID, "restart", nil,
			api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
		if rec.Code != http.StatusOK {
			t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
		}
		if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
			t.Error("喚醒 opened a wind-down on a live worker. It is not a wind-down " +
				"CAUSE on either arm — and on this one the owner pressed it precisely " +
				"in order to leave the session alone, so handing that session a 預告 " +
				"and a grace window is the bug 包④ closes, not a softer version of it.")
		}
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Errorf("喚醒 on a live worker dispatched %d frames, want 0", got)
		}
	})

	t.Run("no session — immediate re-dispatch, still no wind-down", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveWorker(t, api, false)
		api.hub.DrainWardenCommands(ServerSelfHost)

		rec := postWorker(t, api, workerID, "restart", nil,
			api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
		if rec.Code != http.StatusOK {
			t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
		}
		if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
			t.Error("restart must not open a wind-down")
		}
		sawStart := false
		for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
			if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStart {
				sawStart = true
			}
		}
		if !sawStart {
			t.Fatal("restart must re-dispatch immediately when there is no session " +
				"to leave alone — 包④ made 喚醒 a no-op on a LIVE worker, not a no-op")
		}
	})
}

// TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt: the pre-existing
// desired_state=offline branch point outranks everything, wind-down included —
// an owner 停止 must never be overturned by another owner verb.
//
// 🔴 IT IS ALSO THE COMPENSATION SENTINEL for the one asymmetry between this
// funnel's predicate and the staff twin's (T-170e stage 2 ①). memberHasStateToFlush
// carries an explicit desired-online guard (aRefocusStampWouldReachTheAgent);
// workerHasStateToFlush carries none, and what stands in for it is the gate this
// test drives — respawnWorkerForOwnerOp returning held_down BEFORE the predicate
// is consulted. The setup below is deliberately a worker that is desired-offline
// AND STILL ONLINE, which is exactly the state the shared core answers YES for:
// if the gate ever stops being first, this test is what says so.
func TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("hold down: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	// The state that makes this a compensation test rather than an ordinary
	// held-down test: the shared core says there IS something to flush, and the
	// only thing stopping a wind-down is the caller's gate.
	held, _ := api.dal.GetOutsourceWorker(workerID)
	if !api.hub.IsOnline(workerID) ||
		!hasUncollectedOnlineOwnerOpState(held.RefocusSince, held.StoppedSince, true) {
		t.Fatalf("setup: the shared predicate must answer YES here, or the gate "+
			"under test is not the thing doing the work: %+v", held)
	}

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince != 0 {
		t.Fatal("a stopped worker must not be woken into a wind-down")
	}
	if !strings.HasPrefix(after.LastOpReason, spawnReasonHeldDown) {
		t.Fatalf("receipt = %q, want %s", after.LastOpReason, spawnReasonHeldDown)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a stopped worker must not be started, got %d frames", got)
	}
}

// TestOwnerOp_VerbAfterTheCollectIsNotSwallowed (T-98f4, the 收口-window HIGH):
// the window between 「收口已latch」 and 「新 session 還沒連上」 must not eat an
// owner verb.
//
// The shape: the collect has ALREADY dispatched kill + start for this epoch
// (carrying the OLD pin), but the dying session is still hub.IsOnline because
// its warden has not reaped it yet. Without the StoppedSince arm in
// workerHasStateToFlush, the second verb takes the wind-down branch: it zeroes
// the collected latch, re-stamps refocus_since — and dispatches NOTHING. The
// in-flight OLD-config start then boots, boot_ts beats the new refocus_since,
// autoHandoverWorker's loop-break clears the epoch, and the owner's second
// 改機器 reaches no session at all: the cockpit shows m-third while the worker
// lives on m-elsewhere forever (the FSM rescue is gated on !IsOnline, so
// nothing heals it). That is the owner's original complaint coming back in
// through a new door, so the collected window takes the IMMEDIATE path.
func TestOwnerOp_VerbAfterTheCollectIsNotSwallowed(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	seedMachine(t, api, "m-third")
	connectWarden(t, api, "m-third")

	// 1) 改機器 → wind-down opens (no kill yet).
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #1: %d %s", rec.Code, rec.Body.String())
	}
	// 2) the worker answers → the collect latches stopped_since and dispatches
	//    kill + start with the pin as it stood THEN (m-elsewhere).
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the shared FSM does the collect on the
	// next tick. The move itself is unchanged — respawnWorkerNow still starts on
	// whatever machine the row is pinned to when the collect runs.
	workerTickPass(t, api, workerID, nowSecs())
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("fixture: the collect must have latched stopped_since")
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	api.hub.DrainWardenCommands("m-elsewhere")
	// The in-flight session has not booted yet, and the dying one still looks
	// online — this is exactly the window the bug lived in.
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the old session must still look online for this test to bite")
	}

	// 3) the owner changes his mind mid-flight. It must GO OUT, not be swallowed.
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-third"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #2: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.DesiredMachineID != "m-third" {
		t.Fatalf("pin = %q, want m-third", w.DesiredMachineID)
	}
	starts := api.hub.DrainWardenCommands("m-third")
	if len(starts) != 1 {
		t.Fatalf("a verb landing after the collect must dispatch NOW, got %d frames on "+
			"the new machine (a second wind-down would dispatch nothing and the "+
			"in-flight old-config start would then discard the epoch)", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
	// The collected latch must survive: zeroing it is what re-opened the window.
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("the already-collected latch must not be zeroed by a later owner verb")
	}
}

// TestOwnerOp_OrdinaryStopRestartStillWindsDownLater (T-98f4, the STALE-LATCH
// HIGH — the hole the 收口-window fix opened): the 「已收攏」 fast path must be
// EPOCH-SCOPED, not a global read of stopped_since.
//
// stopped_since is latched in TWO places, and only one of them is a handover:
// collectWorkerHandover latches it as the 收口 of a refocus epoch, but
// workerReportStopped's else arm ALSO latches it for a report that arrives
// outside any handover — an ordinary 停止 where the worker politely says it has
// finished. Nothing clears that one: clearWorkerRefocus is only reachable while
// refocus_since > 0, and the restart handler writes desired_state and nothing
// else. So the latch outlives the whole stop→restart cycle.
//
// Read globally, that latch says "already collected" forever: every later 改機器
// / 換 model on that worker is shot on the spot — no 預告, no grace, whatever
// the session had not written down is gone — and it repeats for the rest of the
// worker's life. That is the exact failure rule 2 exists to abolish, re-entering
// through a broader and far more ordinary door than the one the 收口-window fix
// closed.
func TestOwnerOp_OrdinaryStopRestartStillWindsDownLater(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// A stopped-report arriving OUTSIDE any handover: desired_state stays online
	// and refocus_since was never stamped, so workerReportStopped falls through
	// to the bare latch — stopped_since set, no kill, the session still alive.
	// That is the stale latch this test is about.
	//
	// ⚠️ IT USED TO REACH THAT STATE VIA 停止 → report → 重啟, and T-ed79 parity
	// #11 closed that route: the restart handler now clears the wind-down anchors
	// it used to leave behind. The state itself is still perfectly reachable —
	// this arm needs no owner verb at all — so the test keeps its subject and only
	// changes how it gets there. (The rest of that route is covered by
	// worker_restart_clears_markers_ted79_test.go.)
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the shared FSM does the collect on the
	// next tick. The move itself is unchanged — respawnWorkerNow still starts on
	// whatever machine the row is pinned to when the collect runs.
	workerTickPass(t, api, workerID, nowSecs())
	api.hub.DrainWardenCommands(ServerSelfHost)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.RefocusSince != 0 || w.StoppedSince <= 0 {
		t.Fatalf("fixture: this test needs a NON-handover latch "+
			"(refocus=%v stopped=%v)", w.RefocusSince, w.StoppedSince)
	}
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the worker must be online for the wind-down to be owed")
	}

	// Now the owner changes the model. This worker is running and has never been
	// through a handover, so it is owed the FULL wind-down.
	if rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost); rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince <= 0 {
		t.Fatal("a worker whose only stopped_since came from an ORDINARY stop must " +
			"still get its 收尾 — a stale latch is not a collected handover")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the wind-down must not kill the session yet, got %d frames", got)
	}
	// The fresh epoch also HEALS the stale latch, so the worker is not stuck
	// re-deciding this every time.
	if after.StoppedSince != 0 {
		t.Fatalf("a new epoch must clear the stale latch, got stopped_since=%v",
			after.StoppedSince)
	}
}

// TestOwnerOp_SecondVerbDuringAnOpenWindowReStampsAndStillCollects pins cell 3
// of the decision table: refocus > 0 ∧ stopped == 0 — a grace window that is
// OPEN and not yet collected.
//
// It is the neighbour of the 收口-window defect cell and the reason that fix had to
// be an AND rather than "refocus > 0 ⇒ immediate": nothing has been dispatched
// yet here, so re-stamping is harmless and the owner still gets his 收尾. What
// makes it safe is that the new pin is persisted BEFORE the window is (re)opened,
// so whichever collect eventually fires carries the LATEST value — checked below
// by letting the worker answer and watching where it actually lands.
func TestOwnerOp_SecondVerbDuringAnOpenWindowReStampsAndStillCollects(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	seedMachine(t, api, "m-third")
	connectWarden(t, api, "m-third")

	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #1: %d %s", rec.Code, rec.Body.String())
	}
	first, _ := api.dal.GetOutsourceWorker(workerID)
	if first.RefocusSince <= 0 || first.StoppedSince != 0 {
		t.Fatalf("fixture: cell 3 needs an OPEN, uncollected window "+
			"(refocus=%v stopped=%v)", first.RefocusSince, first.StoppedSince)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	// The owner changes his mind while the window is still open. Still a
	// wind-down — and still no kill.
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-third"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #2: %d %s", rec.Code, rec.Body.String())
	}
	second, _ := api.dal.GetOutsourceWorker(workerID)
	if second.RefocusSince <= 0 {
		t.Fatal("a verb landing inside an OPEN window must keep the 收尾 open")
	}
	if second.DesiredMachineID != "m-third" {
		t.Fatalf("pin = %q, want m-third", second.DesiredMachineID)
	}
	for _, target := range []string{ServerSelfHost, "m-elsewhere", "m-third"} {
		if got := len(api.hub.DrainWardenCommands(target)); got != 0 {
			t.Fatalf("nothing may be dispatched while the window is open "+
				"(%s got %d frames)", target, got)
		}
	}

	// It answers → the collect fires ONCE and carries the LATEST pin.
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the shared FSM does the collect on the
	// next tick. The move itself is unchanged — respawnWorkerNow still starts on
	// whatever machine the row is pinned to when the collect runs.
	workerTickPass(t, api, workerID, nowSecs())
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 0 {
		t.Fatalf("the superseded destination must receive nothing, got %d frames", got)
	}
	starts := api.hub.DrainWardenCommands("m-third")
	if len(starts) != 1 {
		t.Fatalf("the collect must start on the LATEST pin, got %d frames", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
}
