package main

import (
	"net/http/httptest"
	"testing"
)

// T-102 — report_stopped's four outcomes must be TELLABLE APART on the wire.
//
// 🔴 THE DEFECT THESE PIN. report_stopped has four internal outcomes and, before
// this ticket, all four answered 200 with byte-identical bytes. Two of them do
// nothing: the bare latch (stopped_since written, no collector watching it) and
// the repeat report (the whole body skipped). An agent that read either as "I
// have been stopped" was wrong — nothing killed its session and the reconcile
// machine started it again seconds later, still spending.
//
// So every test here asserts the RECEIPT, through the HTTP handler, not the
// internal helper: the receipt is the only thing the caller ever sees, and a
// helper that returns the right verdict into a handler that drops it is the
// exact shape of the bug. Where a value claims a collect ("collected",
// "latched_for_collect") the warden frames are counted too — otherwise the
// assertion is that a string is a string.

// reportStoppedReceipt drives POST /api/self/stopped as `id` itself and returns
// the decoded receipt. Identity comes from the token (sub), never the body,
// which is what this face is for.
func reportStoppedReceipt(
	t *testing.T, s *apiServer, id string,
) (selfReportReceiptDTO, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, id, "agent"))
	if rec.Code != 200 {
		t.Fatalf("report_stopped(%s): %d %s", id, rec.Code, rec.Body.String())
	}
	return decodeBody[selfReportReceiptDTO](t, rec), rec
}

// stopEffectWorker seeds a live, ACTIVE worker with an online SSE session and a
// reachable warden — the state every arm below starts from. The caller then
// moves ONLY the fields its own arm is about.
func stopEffectWorker(t *testing.T, s *apiServer, id string) OutsourceWorker {
	t.Helper()
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	s.outsourceMu.Lock()
	w := activeWorkerAtPct(t, s, id, 10, now)
	s.outsourceMu.Unlock()
	return w
}

// 🔴 THE SILENT CELL. A desired-ONLINE worker with NO refocus epoch reports
// stopped: neither collect arm matches, so stopped_since is written and NOTHING
// is dispatched or owed. The receipt must say `recorded_only` — the one value
// that tells the caller it has been noted rather than stopped.
//
// The frame count is half the assertion: a receipt that said recorded_only while
// a kill actually went out would be just as wrong, in the other direction.
func TestStopEffect_BareLatchAnswersRecordedOnly_T102(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stopEffectWorker(t, s, "ow-bare")
	if w.RefocusSince != 0 || w.DesiredState != DesiredStateOnline {
		t.Fatalf("fixture must be the BARE case (desired online, no epoch), got "+
			"desired=%q refocus_since=%v", w.DesiredState, w.RefocusSince)
	}
	s.hub.DrainWardenCommands(ServerSelfHost) // ignore anything the seed emitted

	got, rec := reportStoppedReceipt(t, s, "ow-bare")
	t.Logf("bare-latch receipt: %s", rec.Body.String())
	if got.StopEffect != stopEffectRecordedOnly {
		t.Fatalf("stop_effect = %q, want %q — this report anchored stopped_since "+
			"and NOBODY is collecting it; any other value tells the caller a "+
			"collect is under way when none is",
			got.StopEffect, stopEffectRecordedOnly)
	}
	if n := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); n != 0 {
		t.Fatalf("the bare latch must dispatch NO kill (that IS the defect being "+
			"described), got %d stop(s)", n)
	}
	// The other half of "recorded": the anchor really is on the row, so this is
	// recorded_only and not a report that vanished.
	after, err := s.dal.GetOutsourceWorker("ow-bare")
	if err != nil || after == nil {
		t.Fatalf("read back: %v", err)
	}
	if after.StoppedSince <= 0 {
		t.Fatalf("recorded_only claims the report was RECORDED, but stopped_since "+
			"is %v", after.StoppedSince)
	}
}

// 🔴 THE SECOND SILENT CELL. stopped_since is anchor-semantics — never
// re-stamped — so a repeat report skips the whole handler body. It must not
// borrow the first report's verdict: `already_reported` is the only honest
// answer, because whatever the first call did or failed to do is what still
// stands.
func TestStopEffect_RepeatReportAnswersAlreadyReported_T102(t *testing.T) {
	s := newWorkerTestServer(t)
	stopEffectWorker(t, s, "ow-twice")
	s.hub.DrainWardenCommands(ServerSelfHost)

	first, _ := reportStoppedReceipt(t, s, "ow-twice")
	if first.StopEffect != stopEffectRecordedOnly {
		t.Fatalf("precondition: the FIRST report on this fixture is the bare "+
			"latch, want %q, got %q — if this moved, the second-call assertion "+
			"below is testing a different transition than it claims",
			stopEffectRecordedOnly, first.StopEffect)
	}
	before, err := s.dal.GetOutsourceWorker("ow-twice")
	if err != nil || before == nil {
		t.Fatalf("read back: %v", err)
	}

	second, rec := reportStoppedReceipt(t, s, "ow-twice")
	t.Logf("repeat receipt: %s", rec.Body.String())
	if second.StopEffect != stopEffectAlreadyReported {
		t.Fatalf("stop_effect = %q, want %q — the second call ran no body at all "+
			"and must not report the first call's effect as its own",
			second.StopEffect, stopEffectAlreadyReported)
	}
	after, err := s.dal.GetOutsourceWorker("ow-twice")
	if err != nil || after == nil {
		t.Fatalf("read back: %v", err)
	}
	if after.StoppedSince != before.StoppedSince {
		t.Fatalf("the anchor was RE-STAMPED (%v → %v) — already_reported is "+
			"asserting 'this call did nothing', so the anchor must not move",
			before.StoppedSince, after.StoppedSince)
	}
	if n := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); n != 0 {
		t.Fatalf("a repeat report must dispatch nothing, got %d stop(s)", n)
	}
}

// The 停止 arm: an owner-op has already written desired_state=offline and opened
// a graceful (non-forced) stop epoch, so this report IS the 收口 — collectWorkerStop
// kills on the spot and holds the worker down. `collected`, and the kill is
// counted rather than taken on the receipt's word.
func TestStopEffect_OpenStopEpochAnswersCollected_T102(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stopEffectWorker(t, s, "ow-stop")
	now := nowSecs()
	w.DesiredState = DesiredStateOffline
	w.StoppingSince = now - 5
	w.RefocusSince = 0
	w.RefocusOp = ""
	s.outsourceMu.Lock()
	putWorkerFixture(t, s, w)
	s.outsourceMu.Unlock()
	if !gracefulStopEpochOpen(memberFromWorker(w)) {
		t.Fatalf("fixture must have an OPEN, non-forced stop epoch — this arm is "+
			"gated on it (stopping_since=%v)", w.StoppingSince)
	}
	s.hub.DrainWardenCommands(ServerSelfHost)

	got, rec := reportStoppedReceipt(t, s, "ow-stop")
	t.Logf("stop-epoch receipt: %s", rec.Body.String())
	if got.StopEffect != stopEffectCollected {
		t.Fatalf("stop_effect = %q, want %q — an open 停止 epoch is collected by "+
			"THIS call", got.StopEffect, stopEffectCollected)
	}
	if n := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); n != 1 {
		t.Fatalf("`collected` claims a kill went out on this call — got %d "+
			"stop(s). A receipt that says collected while nothing was dispatched "+
			"is the same silent lie in a new place", n)
	}
}

// The handover arm: desired online + a live refocus epoch. Nothing is dispatched
// HERE by design (one decider, one kill — the FSM collects off this very latch
// on the next tick), so `collected` would be a lie and `recorded_only` would be
// alarming nonsense. `latched_for_collect` is the cell that says "owed, one tick
// away", and the tick is driven to prove the debt is real.
func TestStopEffect_OpenHandoverAnswersLatchedForCollect_T102(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stopEffectWorker(t, s, "ow-hand")
	now := nowSecs()
	w.RefocusSince = now - 5
	w.RefocusOp = refocusOpRestartSelf
	s.outsourceMu.Lock()
	putWorkerFixture(t, s, w)
	s.outsourceMu.Unlock()
	s.hub.DrainWardenCommands(ServerSelfHost)

	got, rec := reportStoppedReceipt(t, s, "ow-hand")
	t.Logf("handover receipt: %s", rec.Body.String())
	if got.StopEffect != stopEffectLatchedForCollect {
		t.Fatalf("stop_effect = %q, want %q", got.StopEffect, stopEffectLatchedForCollect)
	}
	if n := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); n != 0 {
		t.Fatalf("this arm LATCHES ONLY — the kill belongs to the next tick — "+
			"got %d stop(s) from the report itself", n)
	}
	// 🔴 The half that separates latched_for_collect from recorded_only: the
	// collect is genuinely OWED. Without this the two values would be
	// indistinguishable claims about the future.
	s.runOutsourceTick(now + 1)
	if n := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); n != 1 {
		t.Fatalf("latched_for_collect promises the NEXT TICK collects off this "+
			"latch; the tick produced %d stop(s)", n)
	}
}

// A staff report is ALWAYS collected (owner 2026-08-16, rc-b08d49dc3b03), so the
// staff side has only two of the four cells — and a repeat is still nothing.
// Pinned because the staff arm names its effects at a DIFFERENT call site from
// the worker fold, and nothing else would notice the two drifting apart.
func TestStopEffect_StaffIsCollectedThenAlreadyReported_T102(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-staff-t102")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-staff-t102", "mach-a")
	drainFrames(t, s, "mach-a")

	first, rec := reportStoppedReceipt(t, s, "m-staff-t102")
	t.Logf("staff first receipt: %s", rec.Body.String())
	if first.StopEffect != stopEffectCollected {
		t.Fatalf("stop_effect = %q, want %q — a staff stopped-report is always "+
			"collected; there is no recorded_only cell on this side",
			first.StopEffect, stopEffectCollected)
	}
	if f := drainFrames(t, s, "mach-a"); len(f) != 1 || f[0].RPC != "stop" {
		t.Fatalf("`collected` on the staff arm claims the robust STOP went out "+
			"on this call: %+v", f)
	}
	second, rec2 := reportStoppedReceipt(t, s, "m-staff-t102")
	t.Logf("staff repeat receipt: %s", rec2.Body.String())
	if second.StopEffect != stopEffectAlreadyReported {
		t.Fatalf("stop_effect = %q, want %q on the repeat",
			second.StopEffect, stopEffectAlreadyReported)
	}
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("a repeat staff report re-dispatched %d frame(s) — "+
			"already_reported asserts this call did nothing", len(f))
	}
}

// The wire shape itself: stop_effect rides on the STOPPED face and on no other.
//
// Both halves matter. Present-on-stopped is what makes the field readable at
// all; absent-on-waking is what keeps the other three faces byte-identical to
// what they answered before T-102 — they are not stop reports and have no
// effect to name, and a receipt that carried an empty stop_effect would invite a
// caller to switch on "".
func TestStopEffect_RidesOnlyTheStoppedFace_T102(t *testing.T) {
	s := newWorkerTestServer(t)
	stopEffectWorker(t, s, "ow-shape")

	rec := httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{}, "ow-shape", "agent"))
	if rec.Code != 200 {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	t.Logf("waking receipt: %s", rec.Body.String())
	assertReceiptKeys(t, rec, "id", "desired_state", "refocus_op", "refocus_deadline")

	_, stoppedRec := reportStoppedReceipt(t, s, "ow-shape")
	t.Logf("stopped receipt: %s", stoppedRec.Body.String())
	assertReceiptKeys(t, stoppedRec,
		"id", "desired_state", "refocus_op", "refocus_deadline", "stop_effect")
}
