package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-b6d9 — the STAFF 換手收尾 funnel (member_ownerop_winddown.go), and in
// particular the 換模型 verb, which before this ticket did LITERALLY NOTHING to
// the running session: PATCH /api/members/{id} wrote the new model and answered
// 200 while the agent kept running the old one until something unrelated
// happened to respawn it.
//
// ── the predicate's decision table (memberHasStateToFlush) ───────────────────
// Copied from workerHasStateToFlush; the staff substitutions and the reason for
// each are in the file comment of member_ownerop_winddown.go. Inputs, in the
// order the function short-circuits them:
//
//	kind  | desired | online | refocus | stopped | wind down? | pinned by
//	------+---------+--------+---------+---------+------------+----------------
//	!staff| *       | *      | *       | *       | NO         | _WardenIsNeverWoundDown
//	staff | offline | *      | *       | *       | NO         | _StoppedMemberIsNotRevived
//	staff | online  | false  | *       | *       | NO         | _OfflineTakesEffectImmediately
//	staff | online  | true   | >0      | >0      | NO         | _VerbAfterTheCollectIsNotSwallowed
//	staff | online  | true   | >0      | 0       | YES        | _SecondVerbDuringAnOpenWindowReStamps
//	staff | online  | true   | 0       | >0      | YES        | _OrdinaryStopRestartStillWindsDownLater
//	staff | online  | true   | 0       | 0       | YES        | _ModelChangeWindsDownThenRespawns
//
// The last two rows are the two halves of the epoch-scoped 已收攏 conjunct, and
// each was a HIGH in the worker twin's review: reading stopped_since GLOBALLY
// makes row 6 answer NO forever after any ordinary 停止, and dropping
// stopped_since makes row 5 answer NO inside the collect window. If a change
// here has no row, the table is wrong — or the third hole is in it.

// row 7 — the headline: a live staff member's 換模型 winds down, and the respawn
// that follows the 收口 carries the NEW model. Both halves matter: stamping an
// epoch that never respawns on the new value would be theatre.
func TestMemberOwnerOp_ModelChangeWindsDownThenRespawns(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-model")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.Model = "old-model"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-model", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-model",
			map[string]any{"model": "new-model"}, wireOwnerID, "owner"), "m-model")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-model")
	if got == nil || got.Model != "new-model" {
		t.Fatalf("the new model must persist: %+v", got)
	}
	// (a) the agent was told — refocus_since>0 ∧ desired_state=online is the exact
	// condition cli/ocagent's recycleHook prints the 下線程序 wake on.
	if got.RefocusSince <= 0.0 {
		t.Fatalf("a live 換模型 must open a wind-down (refocus epoch): %+v", got)
	}
	// (b) and nothing was killed on the click.
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("the wind-down dispatches nothing on the click: %+v", f)
	}

	// 收口: the agent reports stopped → robust STOP now; the session drops; the
	// next tick's plain START must carry the NEW model.
	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-model", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("the 收口 must dispatch the robust STOP: %+v", stops)
	}

	s.hub.Disconnect(session) // the warden reaped it: the member is now offline
	s.reconcileMemberNow("m-model")
	starts := drainFrames(t, s, "mach-a")
	if len(starts) != 1 || starts[0].RPC != "start" {
		t.Fatalf("the respawn must be dispatched after the 收口: %+v", starts)
	}
	if starts[0].Args["model"] != "new-model" {
		t.Fatalf("the respawn must carry the NEW model, got %v — otherwise the owner "+
			"pressed 儲存 and nothing changed, which is the defect this ticket fixes",
			starts[0].Args["model"])
	}
}

// row 7, 誤擋 half — renaming a member is not a launch intent and must NEVER
// recycle it. Without this, "wind down on every PATCH" passes everything above.
func TestMemberOwnerOp_RenameNeverWindsDown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-rename")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.Model = "keep-me"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-rename", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-rename",
			map[string]any{"name": "New Name"}, wireOwnerID, "owner"), "m-rename")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-rename")
	if got == nil || got.Name != "New Name" {
		t.Fatalf("the rename must persist: %+v", got)
	}
	if got.RefocusSince != 0.0 {
		t.Fatalf("a rename must not recycle the session: refocus_since = %v", got.RefocusSince)
	}
	// Re-sending the SAME model is also not a change — an idempotent save must not
	// cost the owner a handover.
	rec = httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-rename",
			map[string]any{"model": "keep-me"}, wireOwnerID, "owner"), "m-rename")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-rename"); got == nil || got.RefocusSince != 0.0 {
		t.Fatalf("re-saving an unchanged model must not recycle: %+v", got)
	}
}

// row 3 — no live session: nothing can hear the 預告 and nothing can flush, so
// the change simply takes effect on the next wake. Waiting here would burn the
// whole grace for certain.
func TestMemberOwnerOp_OfflineTakesEffectImmediately(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-dark2")
	m.DesiredState = DesiredStateOnline // wants to run; holds no SSE connection
	m.Model = "old"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-dark2",
			map[string]any{"model": "new"}, wireOwnerID, "owner"), "m-dark2")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-dark2")
	if got == nil || got.Model != "new" {
		t.Fatalf("the new model must persist: %+v", got)
	}
	if got.RefocusSince != 0.0 {
		t.Fatalf("no live session ⇒ no wind-down: refocus_since = %v", got.RefocusSince)
	}
}

// row 2 — an explicit 停止 dominates. desired_state=offline means the owner
// already said stop; stamping a refocus epoch would park a marker the agent's
// own gate (desired_state=online) will never read.
func TestMemberOwnerOp_StoppedMemberIsNotRevived(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-held")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-a"
	m.Model = "old"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-held", "mach-a") // session not reaped yet

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-held",
			map[string]any{"model": "new"}, wireOwnerID, "owner"), "m-held")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-held")
	if got == nil || got.Model != "new" {
		t.Fatalf("the new model is still recorded: %+v", got)
	}
	if got.RefocusSince != 0.0 {
		t.Fatalf("a stopped member must not be wound down: refocus_since = %v", got.RefocusSince)
	}
	if got.DesiredState != DesiredStateOffline {
		t.Fatalf("no owner verb may quietly overturn 停止: %q", got.DesiredState)
	}
}

// row 1 — a warden runs no ocagent, so a refocus epoch on one is a marker no
// process will ever read.
func TestMemberOwnerOp_WardenIsNeverWoundDown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-w")
	connectOnline(t, s, "mach-w")

	w, err := s.dal.GetMember("mach-w")
	if err != nil || w == nil {
		t.Fatalf("read warden row: %v", err)
	}
	w.DesiredState = DesiredStateOnline
	if err := s.putMember(*w, triggerServer); err != nil {
		t.Fatalf("arm warden: %v", err)
	}
	if s.memberHasStateToFlush(*w) {
		t.Fatal("a warden has no agent session to wind down")
	}
}

// row 4 — this epoch's wind-down is ALREADY collected (refocus>0 ∧ stopped>0):
// the kill+respawn went out carrying whatever the row held then, and the old
// session merely has not been reaped. Opening a SECOND window here would
// dispatch nothing while the in-flight respawn boots on the OLD value, and the
// owner's change would reach no session at all. (The worker twin's round-1 HIGH.)
func TestMemberOwnerOp_VerbAfterTheCollectIsNotSwallowed(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-collected")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.RefocusSince = 1000.0
	m.StoppedSince = 1001.0
	m.Model = "old"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-collected", "mach-a")

	before := m.RefocusSince
	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-collected",
			map[string]any{"model": "new"}, wireOwnerID, "owner"), "m-collected")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-collected")
	if got == nil || got.RefocusSince != before {
		t.Fatalf("a collected epoch must not be re-opened (the verb takes effect now): "+
			"refocus_since %v → %+v", before, got)
	}
}

// row 6 — the epoch guard's other half. stopped_since is latched by an ORDINARY
// report_stopped too (deactivate → the agent says it finished), and nothing
// clears it while the member sits offline. Read GLOBALLY it would claim "already
// collected" for the rest of that member's life, so every later 換模型 / 改機器
// would be shot on the spot with no warning — the worker twin's round-2 HIGH.
func TestMemberOwnerOp_OrdinaryStopRestartStillWindsDownLater(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-cycled")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.Model = "old"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-cycled", "mach-a")

	// An ORDINARY stop: deactivate, then the agent reports it finished. That
	// latches stopped_since OUTSIDE any handover (refocus_since stays 0).
	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-cycled/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-cycled")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-cycled", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := s.dal.GetMember("m-cycled"); got == nil || got.StoppedSince <= 0.0 ||
		got.RefocusSince != 0.0 {
		t.Fatalf("fixture: want a stopped_since latch with NO refocus epoch, got %+v", got)
	}

	// Restarted by hand (activate writes desired_state; it does not clear the
	// stale latch), and the member is live again.
	rec = httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-cycled/activate",
			map[string]any{"machine_id": "mach-a"}, wireOwnerID, "owner"), "m-cycled")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	drainFrames(t, s, "mach-a") // discard the activate's START

	// The next 換模型 must STILL wind down gracefully.
	rec = httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-cycled",
			map[string]any{"model": "new"}, wireOwnerID, "owner"), "m-cycled")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-cycled")
	if got == nil || got.RefocusSince <= 0.0 {
		t.Fatalf("a stale stopped_since latch from an ORDINARY stop must not make every "+
			"later verb an instant kill: %+v", got)
	}
	if got.StoppedSince != 0.0 {
		t.Fatalf("opening an epoch must zero the stale latch: %+v", got)
	}
}

// row 5 — a second verb DURING an open window re-stamps the epoch and the 收口
// still happens. Reading only refocus_since (treating any open window as
// "already collected") would swallow it.
func TestMemberOwnerOp_SecondVerbDuringAnOpenWindowReStamps(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")

	m := testAgent("m-twice")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.RefocusSince = 1000.0 // a window is OPEN and not yet collected
	m.Model = "old"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnline(t, s, "mach-b")
	connectOnlineMachine(t, s, "m-twice", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-twice/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-twice")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dal.GetMember("m-twice")
	if got == nil || got.RefocusSince <= 1000.0 {
		t.Fatalf("a verb inside an open window must RE-STAMP the epoch: %+v", got)
	}
	if got.DesiredMachineID != "mach-b" {
		t.Fatalf("the new pin must land: %+v", got)
	}
	// …and it is still collectable: the stopped-report kills the session on the
	// machine it actually runs on, not on the destination.
	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-twice", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("the 收口 must kill on the RUNNING machine mach-a: %+v", stops)
	}
	if f := drainFrames(t, s, "mach-b"); len(f) != 0 {
		t.Fatalf("the destination warden must not be asked to kill a session it "+
			"does not hold: %+v", f)
	}
}
