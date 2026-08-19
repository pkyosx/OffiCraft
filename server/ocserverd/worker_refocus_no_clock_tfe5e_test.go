package main

import (
	"net/http"
	"testing"
)

// T-fe5e — an outsource worker's 重新聚焦 runs no clock either.
//
// 🔴 Why this file exists: the notice and the clock were TWO readers of the
// same mark, and they disagreed. The wording came from the shared member
// composer (offboardKindOf → SOFT for refocusOpRefocus, no countdown), while
// the collect came from the worker's own in-flight arm, which read no op at all
// and gave every epoch a flat StoppingTimeoutSecs. So a worker was told there
// was no deadline and cut off at 120 s.
//
// T-c996 took the clock off 重新聚焦 for staff. The owner ruled the same for
// workers on 2026-08-19 (rc-5c478001de8a): 「外包跟正職在這一塊的行為我要一樣
// 沒有道理不一樣」— and rejected the cost argument that they differ, since a
// staff member holding a single task pays exactly what a worker pays.
//
// 🔴 Every case carries its positive control: the ops that ARE clocked must
// still be collected on time and must still quote the deadline. Without them,
// "never collected" and "the arm stopped being reached" look identical — and
// this whole change is reachable only through arms the existing worker tests
// never touched (stampWorkerRefocus leaves RefocusOp empty, so they all land on
// the clocked branch).

// THE LOAD-BEARING HALF: the in-flight arm must not collect an owner-pressed
// 重新聚焦, no matter how long it has been.
func TestWorkerRefocusIsCollectedByNoClock_ButEveryOtherCauseStillIs(t *testing.T) {
	// 重新聚焦: still there long after the old 120 s ceiling.
	t.Run("refocus is never collected on a clock", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)

		rec := postWorker(t, api, workerID, "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
		if rec.Code != http.StatusOK {
			t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
		}
		api.hub.DrainWardenCommands(ServerSelfHost)
		w, _ := api.dal.GetOutsourceWorker(workerID)
		if w.RefocusOp != refocusOpRefocus {
			t.Fatalf("refocus_op = %q, want %q — the arm under test is not reached",
				w.RefocusOp, refocusOpRefocus)
		}

		for _, elapsed := range []float64{
			StoppingTimeoutSecs + 1, 10 * StoppingTimeoutSecs, 100 * StoppingTimeoutSecs,
		} {
			api.outsourceMu.Lock()
			api.autoHandoverWorker(*w, w.RefocusSince+elapsed)
			api.outsourceMu.Unlock()
			if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
				t.Fatalf("+%.0fs after 重新聚焦: nothing may be dispatched, got %d frames",
					elapsed, got)
			}
		}
	})

	// POSITIVE CONTROL: a clocked cause (改機器) is still collected on time.
	t.Run("relocate is still collected on the clock", func(t *testing.T) {
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
		if w.RefocusOp == refocusOpRefocus {
			t.Fatalf("the control must NOT be the refocus arm (op = %q)", w.RefocusOp)
		}

		api.outsourceMu.Lock()
		api.autoHandoverWorker(*w, w.RefocusSince+StoppingTimeoutSecs-1)
		api.outsourceMu.Unlock()
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("inside the grace window nothing may be dispatched, got %d frames", got)
		}

		api.outsourceMu.Lock()
		api.autoHandoverWorker(*w, w.RefocusSince+StoppingTimeoutSecs+1)
		api.outsourceMu.Unlock()
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
			t.Fatalf("the deadline must still collect (stop+start), got %d frames", got)
		}
	})
}

// THE OTHER HALF: what the cockpit is shown must be the same clock that
// actually collects — 0 when nothing does.
func TestWorkerDTORefocusDeadlineFollowsTheCollectingClock(t *testing.T) {
	cfg := defaultReconcileConfig()
	const since = 1600.0

	cases := []struct {
		name string
		op   string
		want float64
	}{
		{"重新聚焦 quotes no deadline at all", refocusOpRefocus, 0},
		{"改機器 still quotes the deadline it is collected on", "relocate", since + StoppingTimeoutSecs},
		{"context_high still quotes it too", "context_high", since + StoppingTimeoutSecs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dto := newOutsourceWorkerDTO(
				OutsourceWorker{ID: "ow-1", RefocusSince: since, RefocusOp: c.op}, nil,
				outsourceWorkerProjection{now: since + 1, cfg: cfg})
			if dto.RefocusDeadline != c.want {
				t.Fatalf("refocus_deadline = %v, want %v", dto.RefocusDeadline, c.want)
			}
		})
	}
}
