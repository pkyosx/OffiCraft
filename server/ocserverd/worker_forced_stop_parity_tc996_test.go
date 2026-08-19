package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-c996 — 「正職員工跟外包應該都要是」(owner 2026-08-18): the judgement that
// separates "cut off deliberately" from "working its close-out" must cover
// BOTH kinds, because the owner's spec is about both.
//
// It did not. forcedEpochLive reads member.forced_stop_at, OutsourceWorker had
// no such field, and memberFromWorker rebuilds a Member from scratch — so the
// projection handed offboardKindOf a zero, and the predicate was FALSE for
// every worker that ever existed. Nothing failed; the silence just did not
// apply on that side.
//
// 🔴 And the asymmetry is not cosmetic, because a worker 停止 IS the forced
// shape: api_outsource.go's stop kills the session on the spot — no grace, no
// warning, no 預告 — the same thing 強制下線 does to staff. So the ONE arm that
// is supposed to say nothing was the one arm that could still speak.
func TestWorkerStopIsForcedShaped_SoItSaysNothing(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// The sequence that reaches it, and the reason this needed a test rather
	// than a read: the notice rides `offline ∧ stopping_since > 0`, and only
	// the worker's OWN report_stopping stamps that anchor. So it takes the
	// worker politely announcing its wind-down FIRST, the owner pressing 停止
	// SECOND, and the worker living long enough to file report_stopped THIRD —
	// which is what publishes the delta the notice would ride.
	rec := httptest.NewRecorder()
	api.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, http.MethodPost, "/api/self/stopping", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}
	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, http.MethodPost, "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}

	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back the worker: %v", err)
	}
	// The fixture check: without BOTH of these the assertion below passes for
	// the wrong reason (a member that is not being wound down carries no notice
	// either, so the test would be green on an empty state).
	if w.DesiredState != DesiredStateOffline || w.StoppingSince <= 0 {
		t.Fatalf("fixture: this test needs the state the notice rides on "+
			"(desired=%q stopping_since=%v)", w.DesiredState, w.StoppingSince)
	}

	m := memberFromWorker(*w)
	if !forcedEpochLive(m) {
		t.Fatalf("a worker 停止 kills on the spot — it is the FORCED shape, and the "+
			"projection must say so (forced_stop_at=%v stopping_since=%v)",
			m.ForcedStopAt, m.StoppingSince)
	}
	if notice, ok := api.offboardDeltaPayload(m)["offboard_notice"]; ok {
		t.Fatalf("a force-stopped worker must be told NOTHING — the same ruling "+
			"api_members.go enforces for staff — got:\n%v", notice)
	}
	// 🔴 THE OTHER ORDER, and it is the one that guards the second anchor.
	// forcedEpochLive requires forced_stop_at >= stopping_since, so a 停止 that
	// stamps only forced_stop_at is defeated by a report_stopping that lands
	// AFTER it: the later anchor wins the comparison and the worker reads as
	// "still working its close-out" — the arm that speaks. Measured: with the
	// stopping_since stamp removed, the sequence below fans the full soft
	// notice ("work the sequence below, then call report_stopped yourself") at
	// a worker the owner has already killed, and the case above stays GREEN,
	// because its ordering stamps stopping_since first and never exercises the
	// guard at all.
	t.Run("…and the report_stopping that arrives AFTER the kill cannot re-open its mouth", func(t *testing.T) {
		late := newActiveOnlineWorker(t, api)
		postWorker(t, api, late, "stop", nil,
			api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
		rec := httptest.NewRecorder()
		api.HandleReportStoppingApiSelfStoppingPost(rec,
			taskReq(t, http.MethodPost, "/api/self/stopping", nil, late, "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("report_stopping after the stop: %d %s", rec.Code, rec.Body.String())
		}
		w, err := api.dal.GetOutsourceWorker(late)
		if err != nil || w == nil {
			t.Fatalf("read back the worker: %v", err)
		}
		if w.DesiredState != DesiredStateOffline || w.StoppingSince <= 0 {
			t.Fatalf("fixture: this case needs the notice-bearing state too "+
				"(desired=%q stopping_since=%v)", w.DesiredState, w.StoppingSince)
		}
		lm := memberFromWorker(*w)
		if !forcedEpochLive(lm) {
			t.Fatalf("a stop that lands FIRST is still the forced epoch — a later "+
				"report_stopping must not out-rank it (forced_stop_at=%v stopping_since=%v)",
				lm.ForcedStopAt, lm.StoppingSince)
		}
		if notice, ok := api.offboardDeltaPayload(lm)["offboard_notice"]; ok {
			t.Fatalf("the worker was already killed — this frame must carry nothing:\n%v", notice)
		}
	})
}

// The negative control, and it is what stops the fix above from becoming
// "workers never hear anything": a worker being handed over (重新聚焦) is NOT
// forced, so it must still be shown the sequence — and, since T-c996, with no
// countdown in it.
func TestWorkerHandoverStillSpeaks_AndCarriesNoCountdown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	postWorker(t, api, workerID, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)

	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back the worker: %v", err)
	}
	if w.RefocusSince <= 0 || w.RefocusOp != refocusOpRefocus {
		t.Fatalf("fixture: refocus must open a wind-down (since=%v op=%q)",
			w.RefocusSince, w.RefocusOp)
	}
	m := memberFromWorker(*w)
	if forcedEpochLive(m) {
		t.Fatal("a handover is not a force-stop — it must still be allowed to speak")
	}
	notice, ok := api.offboardDeltaPayload(m)["offboard_notice"].(string)
	if !ok || notice == "" {
		t.Fatalf("a worker being handed over must be shown the sequence: %+v",
			api.offboardDeltaPayload(m))
	}
	// 🔴 This assertion was toothless until T-d6a7's post-land review. It read
	// `strings.Contains(notice, "120 seconds") || strings.Contains(notice, "120
	// 秒")`, and after T-d6a7 no notice this server composes contains either
	// string — on ANY arm — so the second half of this test passed by
	// construction, guarding nothing, on the one path an outsource worker's
	// 重新聚焦 takes. It now rejects a span of time in any wording and either
	// language, which is a property of the sentence rather than of one wording
	// of it (offboard_absolute_deadline_td6a7_test.go).
	assertQuotesNoTime(t, "a worker's 重新聚焦", notice)
}
