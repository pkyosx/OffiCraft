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
			workerTickPass(t, api, w.ID, w.RefocusSince+elapsed)
			if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
				t.Fatalf("+%.0fs after 重新聚焦: nothing may be dispatched, got %d frames",
					elapsed, got)
			}
		}
	})

	// T-ed79: 換 model / 改機器 are 停止 too, on the WORKER side as well. The
	// owner's parity ruling quoted above (rc-5c478001de8a) is what decides this:
	// workers and staff read ONE judgement (winddownKindFor), so an op that
	// stopped being clocked for staff cannot stay clocked here without
	// recreating exactly the split this pair of tickets removed.
	t.Run("換 model is not collected on a clock either", func(t *testing.T) {
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
			t.Fatalf("the arm under test must NOT be the refocus arm (op = %q)", w.RefocusOp)
		}

		for _, elapsed := range []float64{
			StoppingTimeoutSecs + 1, 10 * StoppingTimeoutSecs, 100 * StoppingTimeoutSecs,
		} {
			workerTickPass(t, api, w.ID, w.RefocusSince+elapsed)
			if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
				t.Fatalf("+%.0fs after 換 model: nothing may be dispatched, got %d frames",
					elapsed, got)
			}
		}
	})

	// POSITIVE CONTROL: the ONE clocked cause (加速停止 — the second context
	// threshold) is still collected on time. Without it, "never collected" and
	// "the arm stopped being reached" look identical.
	t.Run("加速停止 is still collected on the clock", func(t *testing.T) {
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
		// Re-stamped as the accelerated cause: this arm is opened by context
		// pressure in production (worker_spawn.go), which needs a gauge fixture
		// the handler above does not; the op is what the clock reads.
		w.RefocusOp = refocusOpContextHigh
		if err := api.dal.PutOutsourceWorker(*w); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		seedWorkerAnchors(t, api, *w)

		workerTickPass(t, api, w.ID, w.RefocusSince+StoppingTimeoutSecs-1)
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("inside the grace window nothing may be dispatched, got %d frames", got)
		}

		workerTickPass(t, api, w.ID, w.RefocusSince+StoppingTimeoutSecs+1)
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
			t.Fatalf("the deadline must still collect (stop+start), got %d frames", got)
		}
	})
}

// THE OTHER HALF: what the cockpit is shown must be the same clock that
// actually collects — 0 when nothing does.
//
// 🔴 BOTH WIND-DOWN AXES BELONG IN THIS TABLE, and for a long time only one was
// here (T-14 item 3). Every case set RefocusSince and left DesiredState empty,
// so all five landed on the 換手 arm — and the name of this test, "follows the
// COLLECTING clock", was the thing that went unchecked on the other arm: 加速
// 停止 pressed on a 下線 worker re-anchors stopping_since, not refocus_since,
// and the tick collects it at stopping_since+grace, while the DTO quoted 0. A
// table that can only reach one arm cannot catch an arm going missing, which is
// exactly what happened. The 下線 cases below are the guard.
func TestWorkerDTORefocusDeadlineFollowsTheCollectingClock(t *testing.T) {
	cfg := defaultReconcileConfig()
	const since = 1600.0

	cases := []struct {
		name string
		w    OutsourceWorker
		want float64
	}{
		// ── 換手 axis (desired_state stays online) — anchors refocus_since ──
		{"重新聚焦 quotes no deadline at all",
			OutsourceWorker{RefocusSince: since, RefocusOp: refocusOpRefocus}, 0},
		{"改機器 quotes no deadline either (T-ed79: it is a 停止)",
			OutsourceWorker{RefocusSince: since, RefocusOp: "relocate"}, 0},
		{"換 model quotes none",
			OutsourceWorker{RefocusSince: since, RefocusOp: memberOpModel}, 0},
		{"restart_self quotes none",
			OutsourceWorker{RefocusSince: since, RefocusOp: refocusOpRestartSelf}, 0},
		{"加速停止 still quotes the deadline it is collected on",
			OutsourceWorker{RefocusSince: since, RefocusOp: refocusOpContextHigh},
			since + StoppingTimeoutSecs},

		// ── 下線 axis (desired_state=offline) — anchors stopping_since, and
		// carries NO refocus_since at all. This is the half the table was blind
		// to. The wants here are the stop arm of runOutsourceTick verbatim:
		// gracefulStopEpochOpen && recycleGraceFor(op).clocked, fired at
		// stopping_since + grace.
		{"下線 + 加速停止 quotes the stopping_since deadline the tick collects on (T-14 item 3)",
			OutsourceWorker{
				DesiredState: DesiredStateOffline, StoppingSince: since,
				RefocusOp: refocusOpAcceleratedStop,
			}, since + StoppingTimeoutSecs},
		{"下線 + context_high — the other clocked cause, same anchor",
			OutsourceWorker{
				DesiredState: DesiredStateOffline, StoppingSince: since,
				RefocusOp: refocusOpContextHigh,
			}, since + StoppingTimeoutSecs},
		{"a plain 停止 is on no clock, so it quotes none",
			OutsourceWorker{DesiredState: DesiredStateOffline, StoppingSince: since}, 0},
		{"a live FORCED epoch quotes none — its kill already went out",
			OutsourceWorker{
				DesiredState: DesiredStateOffline, StoppingSince: since,
				ForcedStopAt: since, RefocusOp: refocusOpAcceleratedStop,
			}, 0},
		{"下線 with no stop epoch open yet quotes none",
			OutsourceWorker{
				DesiredState: DesiredStateOffline, RefocusOp: refocusOpAcceleratedStop,
			}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := c.w
			w.ID = "ow-1"
			dto := newOutsourceWorkerDTO(w, nil,
				outsourceWorkerProjection{now: since + 1, cfg: cfg})
			if dto.RefocusDeadline != c.want {
				t.Fatalf("refocus_deadline = %v, want %v", dto.RefocusDeadline, c.want)
			}
		})
	}
}
