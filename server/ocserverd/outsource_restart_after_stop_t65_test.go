package main

import (
	"net/http"
	"testing"
)

// ── T-65 包② — the queued 「起來」 on the outsource face ────────────────────────
//
// 🔴 READ THE FIRST TEST BEFORE THE OTHERS. Everything else in this file writes
// restart_after_stop through a handler and reads it back through
// GetOutsourceWorker; the first test is the one that says WHY that round-trip is
// not free, and it is the only one that would still fail if the projection
// silently dropped the column while every handler was written correctly.

// TestOutsourceProjectionCarriesRestartAfterStop pins the write half AND the
// read half of the OutsourceWorker ↔ Member projection for restart_after_stop.
//
// The hazard is asymmetric with every other field in that projection, and the
// asymmetry is the whole reason this test exists. A column memberFromWorker
// forgets is normally merely NOT REFRESHED. restart_after_stop is not one of
// those: mfRestartAfterStop is deliberately NOT insertOnly (dal_member_patch.go
// spells out why — it must land in the same write as desired_state), so
// memberWholeRow carries it into PutMember's UPDATE SET list and every one of
// the 13 non-test PutOutsourceWorker call sites WRITES it. With the projection
// blind, each of those writes stored `false` over whatever a handler had just
// stamped — no error, no red, and the owner's 重新聚焦 on a stopped worker would
// answer 200 and then never come up.
//
// Mutant (T-65 包② DoD): delete `RestartAfterStop: w.RestartAfterStop` from
// memberFromWorker and this test goes red on the "erased by a later worker
// write" assertion below.
func TestOutsourceProjectionCarriesRestartAfterStop(t *testing.T) {
	api := newTasksTestServer(t)
	id := newActiveOnlineWorker(t, api)

	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed read: %v", err)
	}
	// ── write half: the flag reaches the row through PutOutsourceWorker ───────
	w.RestartAfterStop = true
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read member: %v", err)
	}
	if !m.RestartAfterStop {
		t.Fatal("memberFromWorker dropped restart_after_stop: the member row the " +
			"worker projects reads false right after a worker write that set it")
	}
	// ── read half: it comes back out on the worker vocabulary ────────────────
	back, err := api.dal.GetOutsourceWorker(id)
	if err != nil || back == nil {
		t.Fatalf("read back: %v", err)
	}
	if !back.RestartAfterStop {
		t.Fatal("workerFromMember dropped restart_after_stop: the stored intent is " +
			"invisible to every worker-side reader, so nothing can ever spend it")
	}
	// ── the silent-erase shape, stated as its own assertion ──────────────────
	// A SECOND worker write that says nothing about the flag must not clear it.
	// This is the assertion the mutant kills, and it is separate on purpose: the
	// two above can both pass on a projection that carries the field only one
	// way.
	untouched, err := api.dal.GetOutsourceWorker(id)
	if err != nil || untouched == nil {
		t.Fatalf("read for re-put: %v", err)
	}
	untouched.LastOpLog = "an unrelated worker write"
	if err := api.dal.PutOutsourceWorker(*untouched); err != nil {
		t.Fatalf("re-put worker: %v", err)
	}
	after, err := api.dal.GetOutsourceWorker(id)
	if err != nil || after == nil {
		t.Fatalf("read after re-put: %v", err)
	}
	if !after.RestartAfterStop {
		t.Fatal("restart_after_stop was ERASED by an unrelated worker write — the " +
			"whole-row upsert carries this column, so a projection that does not " +
			"round-trip it does not merely forget the intent, it deletes it")
	}
}

// ── the queued 起來 on the six owner verbs ───────────────────────────────────

// seedStoppedAnchoredWorker builds the fixture the 重啟 verbs actually need, and
// that TestRelocateNeverStoppedWorker_SavesPinWithoutReviving deliberately does NOT
// have: a worker that WENT THROUGH the /stop handler, so stopping_since is a
// real anchor on the row (aStopWasEverAskedFor is TRUE).
//
// 🔴 THE OLDER TEST IS BLIND TO THIS WHOLE CHANGE, and that is a property worth
// stating rather than a coincidence to rely on. Its fixture reaches
// desired_state=offline by writing the field directly through
// PutOutsourceWorker, which never touches stopping_since — so the gate reads
// "nobody ever asked to stop this worker" and it stays green in BOTH directions.
// It is therefore not a signal about this change at all. If it ever DOES go red,
// the reading is not "the spec flipped": it means the aStopWasEverAskedFor gate
// was dropped and never-started workers are being booted by an edit.
//
// keepSession=true is 收工中 (the stop is in flight, the session is still up) —
// keepSession=false is the converged stop, where the queued start is spendable
// on the next tick.
func seedStoppedAnchoredWorker(t *testing.T, api *apiServer, keepSession bool) string {
	t.Helper()
	id := newActiveWorker(t, api, keepSession)
	rec := postWorker(t, api, id, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("fixture stop: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("fixture read: %v", err)
	}
	if w.DesiredState != DesiredStateOffline {
		t.Fatalf("fixture: desired_state = %q, want offline", w.DesiredState)
	}
	if w.StoppingSince <= 0 {
		t.Fatal("fixture: stopping_since is 0 — this fixture's whole point is that a " +
			"stop was REALLY asked for; without the anchor every assertion below " +
			"would silently be testing the never-stopped branch instead")
	}
	if w.RestartAfterStop {
		t.Fatal("fixture: 停止 left restart_after_stop set — the 下線 verbs clear it")
	}
	return id
}

// TestRefocusStoppedWorker_QueuesTheStartInsteadOf409 is the ticket's headline:
// 重新聚焦 on a stopped worker used to answer 409 「refocus requires a live
// worker」. Owner 2026-08-30 (rc-bc1b029a3aa2): 「一個重啟的 intention 遇上一個更
// 強硬的下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」.
//
// The session is deliberately STILL UP here (the stop is in flight), so the
// eager tick the handler fires cannot spend the intent and the assertion is
// about the QUEUE rather than about the wake.
func TestRefocusStoppedWorker_QueuesTheStartInsteadOf409(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, true)

	rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("refocus on a stopped worker = %d %s, want 200 with a queued 起來",
			rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if !w.RestartAfterStop {
		t.Fatal("restart_after_stop is false — the owner's 起來 was not recorded, so " +
			"nothing will bring this worker up when the stop converges")
	}
	// 沿用強硬下線規則: the stop is NOT cancelled and its stage is NOT rolled back.
	if w.DesiredState != DesiredStateOffline {
		t.Errorf("desired_state = %q — the 重啟 intent must be QUEUED behind the stop, "+
			"not overturn it on the spot", w.DesiredState)
	}
	if w.StoppingSince <= 0 {
		t.Error("stopping_since was cleared — the stop in flight is honoured as-is")
	}
	if w.RefocusSince > 0 {
		t.Error("a refocus epoch was opened on a stopped worker — that stamp has no " +
			"reader; only 「起來」 is recorded here")
	}
}

// TestRefocusConvergedStoppedWorker_ComesUpOnThePress is the same verb one state
// later: the stop has already LANDED, so there is nothing left to wait for and
// the queued start is spent on this very press. Staff parity — the member face
// calls reconcileMemberNow in exactly this branch for exactly this reason.
func TestRefocusConvergedStoppedWorker_ComesUpOnThePress(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, false)

	rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.DesiredState != DesiredStateOnline {
		t.Fatalf("desired_state = %q — the stop had already converged, so the owner's "+
			"起來 had nothing left to wait for", w.DesiredState)
	}
	if w.RestartAfterStop {
		t.Error("the intent was not cleared as it was spent — it would fire again " +
			"after the next 下線")
	}
}

// TestRefocusOnlineWorkerStillNeedsALiveSession keeps the NEGATIVE control the
// ticket explicitly asked to preserve: only the 「已停止」 409 is replaced. A
// worker the owner still wants ONLINE but whose session is gone has nothing to
// hand over, and that refusal stands.
func TestRefocusOnlineWorkerStillNeedsALiveSession(t *testing.T) {
	api := newTasksTestServer(t)
	id := newActiveWorker(t, api, false)

	rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("refocus on a wanted-online worker with no session = %d %s, want 409",
			rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.RestartAfterStop {
		t.Fatal("a refused refocus queued a start anyway — the 起來 branch is gated " +
			"on the worker being STOPPED, and this one is not")
	}
}

// TestRefocusNeverStoppedWorkerDoesNotQueueAStart is the OTHER negative control,
// and the ticket named its failure shape explicitly: a worker nobody has ever
// asked to stop must not be woken by a 重啟 verb. Here the row is desired-offline
// with NO stop anchor — the never-started worker.
func TestRefocusNeverStoppedWorkerDoesNotQueueAStart(t *testing.T) {
	api := newTasksTestServer(t)
	id := newActiveWorker(t, api, false)
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed read: %v", err)
	}
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	if w.StoppingSince > 0 {
		t.Fatal("fixture: this worker must have NO stop anchor")
	}

	rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("refocus on a never-stopped offline worker = %d %s, want the 409 to "+
			"stand (aStopWasEverAskedFor is false)", rec.Code, rec.Body.String())
	}
	back, err := api.dal.GetOutsourceWorker(id)
	if err != nil || back == nil {
		t.Fatalf("read back: %v", err)
	}
	if back.RestartAfterStop {
		t.Fatal("a start was queued on a worker nobody ever asked to stop")
	}
}

// TestRelocateStoppedAnchoredWorkerQueuesTheStart is the 改機器 face — the test
// the ticket asked for by name, with the anchored fixture the existing
// TestRelocateNeverStoppedWorker_SavesPinWithoutReviving does not have.
func TestRelocateStoppedAnchoredWorkerQueuesTheStart(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, true)
	seedMachine(t, api, "mach-new")

	rec := postWorker(t, api, id, "relocate", map[string]any{"machine_id": "mach-new"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.DesiredMachineID != "mach-new" {
		t.Errorf("desired_machine_id = %q, want mach-new", w.DesiredMachineID)
	}
	if !w.RestartAfterStop {
		t.Fatal("改機器 on a stopped worker saved the pin and queued nothing — owner " +
			"2026-08-30 「change model / machine 只是帶起來的方式不一樣而已」")
	}
	if w.DesiredState != DesiredStateOffline {
		t.Errorf("desired_state = %q — the stop in flight is honoured as-is", w.DesiredState)
	}
}

// TestSetModelOnStoppedAnchoredWorkerQueuesTheStart is the 換 model face, driven
// against a CONVERGED stop on purpose.
//
// 🔴 IT DOES NOT SHARE THE 改機器 CALL PATH HERE, and the obvious reading says it
// does. Both verbs funnel through respawnWorkerForOwnerOp — but the model
// handler only CALLS that funnel when
// `launchIntentChanged && Status == active && hub.IsOnline(id)`, and this
// worker's session is gone, so the funnel is UNREACHABLE. This test drives the
// model handler's OWN queue branch; delete that branch and only this test goes
// red.
func TestSetModelOnStoppedAnchoredWorkerQueuesTheStart(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, false)

	rec := postWorker(t, api, id, "model", map[string]any{"model": "claude-opus-4-1"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("set model: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.Model != "claude-opus-4-1" {
		t.Errorf("model = %q, want claude-opus-4-1", w.Model)
	}
	if !w.RestartAfterStop {
		t.Fatal("換 model on a stopped worker stored the value and queued nothing")
	}
}

// TestStopVerbsClearTheQueuedStart is 後蓋前 in the direction that makes the
// feature SAFE rather than the direction that makes it work: 重新聚焦 → 停止 /
// 加速停止 / 強制停止 must end with the worker DOWN and staying down. Without
// these clears the queued intent outlives the verb meant to cancel it and the
// tick brings the worker back up under the owner.
func TestStopVerbsClearTheQueuedStart(t *testing.T) {
	for _, tc := range []struct {
		op   string
		call func(api *apiServer) func(http.ResponseWriter, *http.Request, string)
	}{
		{"stop", func(api *apiServer) func(http.ResponseWriter, *http.Request, string) {
			return api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost
		}},
		{"accelerated-stop", func(api *apiServer) func(http.ResponseWriter, *http.Request, string) {
			return api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost
		}},
		{"force-stop", func(api *apiServer) func(http.ResponseWriter, *http.Request, string) {
			return api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost
		}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			api := newTasksTestServer(t)
			// 加速停止 needs an OPEN wind-down to escalate and a LIVE session, so the
			// fixture is a worker 收工中 — the state all three verbs accept.
			id := seedStoppedAnchoredWorker(t, api, true)
			// Arm the intent through the real 重新聚焦 verb rather than by hand: a
			// hand-written flag would leave a failure ambiguous between "the stamp
			// never happened" and "the clear never happened", and the stamp is
			// exactly what the other tests in this file already pin.
			if rec := postWorker(t, api, id, "refocus", nil,
				api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost); rec.Code != http.StatusOK {
				t.Fatalf("arm via refocus: %d %s", rec.Code, rec.Body.String())
			}
			if got, _ := api.dal.GetOutsourceWorker(id); got == nil || !got.RestartAfterStop {
				t.Fatal("fixture: the intent is not armed — this test cannot say " +
					"anything about the clear until it is")
			}

			rec := postWorker(t, api, id, tc.op, nil, tc.call(api))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", tc.op, rec.Code, rec.Body.String())
			}
			back, err := api.dal.GetOutsourceWorker(id)
			if err != nil || back == nil {
				t.Fatalf("read back: %v", err)
			}
			if back.RestartAfterStop {
				t.Fatalf("%s left the queued 起來 armed — the last thing the owner "+
					"pressed decides (後蓋前), and he pressed a 下線 verb", tc.op)
			}
		})
	}
}

// TestRestartWorkerClearsTheQueuedStart: 重啟 spends the intent by DOING it —
// leaving the flag armed would fire a second start after the next 下線.
func TestRestartWorkerClearsTheQueuedStart(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, true)
	if rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost); rec.Code != http.StatusOK {
		t.Fatalf("arm via refocus: %d %s", rec.Code, rec.Body.String())
	}

	rec := postWorker(t, api, id, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.DesiredState != DesiredStateOnline {
		t.Errorf("desired_state = %q, want online", w.DesiredState)
	}
	if w.RestartAfterStop {
		t.Fatal("重啟 brought the worker up AND left the queued start armed — it " +
			"would fire again the next time the owner stops it")
	}
}

// TestOutsourceTickSpendsTheQueuedStart is the CONSUMPTION point, and the whole
// feature reduces to this assertion: a queued 起來 on a converged-offline worker
// becomes desired_state=online at the tick.
//
// It arms through 改機器 rather than 重新聚焦 deliberately — the refocus handler
// fires an eager tick of its own, which would make this test unable to tell
// "the tick spends it" from "the handler spent it".
//
// 🔴 WHERE THE CONSUME IS CALLED FROM IS THE WHOLE TRICK. runOutsourceTick's
// per-worker switch `continue`s on `DesiredState == offline` in the assigned
// branch and gates the active branch on `fresh.DesiredState != offline`, so a
// consumption point inside reconcileWorkerLiveness is UNREACHABLE for exactly
// the population that needs it.
//
// Mutant (T-65 包② DoD): remove the consumeWorkerRestartAfterStop call from
// runOutsourceTick and this goes red on desired_state.
func TestOutsourceTickSpendsTheQueuedStart(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, false)
	seedMachine(t, api, "mach-new")
	if rec := postWorker(t, api, id, "relocate", map[string]any{"machine_id": "mach-new"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("arm via relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := api.dal.GetOutsourceWorker(id); got == nil || !got.RestartAfterStop {
		t.Fatal("fixture: 改機器 did not queue the start — nothing below would mean " +
			"anything")
	}

	api.runOutsourceTick(nowSecs())

	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.DesiredState != DesiredStateOnline {
		t.Fatalf("desired_state = %q after the tick — the queued 起來 was never spent, "+
			"which is the failure shape where the owner presses the button, gets a "+
			"200, and the worker simply never comes up", w.DesiredState)
	}
	if w.RestartAfterStop {
		t.Error("the intent was not cleared as it was spent — it would fire again " +
			"after the next 下線")
	}
	if w.StoppingSince != 0 {
		t.Errorf("stopping_since=%v survived the wake — the next 重啟 verb would read "+
			"this worker as still being stopped", w.StoppingSince)
	}
}

// TestOutsourceTickDoesNotSpendWhileTheSessionIsStillUp: the intent waits for the
// stop to actually LAND. Restarting a worker underneath a kill still in flight is
// what the member twin's `!hub.IsOnline` gate exists to prevent, and it is
// inherited here rather than re-derived.
func TestOutsourceTickDoesNotSpendWhileTheSessionIsStillUp(t *testing.T) {
	api := newTasksTestServer(t)
	id := seedStoppedAnchoredWorker(t, api, true)
	if rec := postWorker(t, api, id, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost); rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}

	api.runOutsourceTick(nowSecs())

	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.DesiredState != DesiredStateOffline {
		t.Fatalf("desired_state = %q — the queued start fired while the session was "+
			"still up, restarting the worker underneath its own stop", w.DesiredState)
	}
	if !w.RestartAfterStop {
		t.Error("the intent was dropped without being spent — the owner's 起來 is lost")
	}
}
