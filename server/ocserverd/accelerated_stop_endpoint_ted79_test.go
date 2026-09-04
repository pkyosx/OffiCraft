package main

// accelerated_stop_endpoint_ted79_test.go — 加速停止, the MIDDLE rung of the
// owner's escalation 停止 → 加速停止 → 強制停止 (owner 2026-08-21 「可以給我按鈕
// 嗎」).
//
// Two properties make it an escalation rather than a second stop button, and
// each fails silently on its own:
//
//   - it REFUSES a member nobody has asked to stop. A clock on a member that was
//     told nothing is a deadline it never heard about — the shape this whole
//     ticket exists to remove;
//   - the clock it starts and the sentence the member is handed are ONE number,
//     on BOTH wind-down arms. The 下線 arm anchors on stopping_since and the 換手
//     arm on refocus_since, so this is the first cause that could have them
//     disagree by construction rather than by a typo.
//
// 🔴 Measured before writing this, twice, and the two reds are deliberately
// different sizes:
//
//   - winddownKindFor's accelerated_stop arm removed (handler and route left in
//     place) → the three behaviour tests fail (下線Arm, 換手Arm,
//     ReadsTheSameGrace) while every 409 still passes: the button would answer
//     200 and do nothing at all;
//   - winddownDeadlineOf's desired-offline arm removed → only the two tests that
//     read a 下線 deadline fail (下線Arm, ReadsTheSameGrace); 換手Arm stays
//     green. That is exactly what a half-wired ladder looks like — the arm the
//     owner is least likely to be on keeps working — which is why the two arms
//     have separate tests instead of one parameterised over them.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doAcceleratedStop(api *apiServer, memberID string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/members/"+memberID+"/accelerated-stop", nil)
	claims := map[string]any{"sub": "owner", "scope": "owner"}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(rec, r, memberID)
	return rec
}

func doWorkerAcceleratedStop(api *apiServer, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/outsource-workers/"+id+"/accelerated-stop", nil)
	claims := map[string]any{"sub": "owner", "scope": "owner"}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(rec, r, id)
	return rec
}

func reloadMember(t *testing.T, dal *DAL, id string) Member {
	t.Helper()
	m, err := dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("reload %s: %v", id, err)
	}
	return *m
}

// 🔴 THE GATE. Without it 加速停止 is not the middle rung of anything — it is a
// second, harsher stop button that skips the step where the member is told.
func TestAcceleratedStop_RefusesAMemberNobodyHasAskedToStop(t *testing.T) {
	api, dal := newGateTestAPI(t)

	t.Run("online, nothing winding down", func(t *testing.T) {
		putGateMember(t, dal, Member{ID: "as-idle", Kind: KindStaff,
			DesiredState: DesiredStateOnline})
		defer online(t, api, "as-idle")()

		if rec := doAcceleratedStop(api, "as-idle"); rec.Code != http.StatusConflict {
			t.Fatalf("want 409, got %d %s — a member that was never asked to stop "+
				"would be put on a deadline it was never told about",
				rec.Code, rec.Body.String())
		}
		got := reloadMember(t, dal, "as-idle")
		if got.RefocusOp != "" || got.RefocusSince != 0 || got.StoppingSince != 0 {
			t.Errorf("the refused call still wrote anchors: %+v", got)
		}
	})

	t.Run("no live session", func(t *testing.T) {
		putGateMember(t, dal, Member{ID: "as-offline", Kind: KindStaff,
			DesiredState: DesiredStateOffline, StoppingSince: 1000})
		if rec := doAcceleratedStop(api, "as-offline"); rec.Code != http.StatusConflict {
			t.Fatalf("want 409, got %d — a countdown nobody is connected to hear is "+
				"a silent deadline", rec.Code)
		}
	})

	t.Run("already cut off by 強制停止", func(t *testing.T) {
		putGateMember(t, dal, Member{ID: "as-forced", Kind: KindStaff,
			DesiredState: DesiredStateOffline, StoppingSince: 1000, ForcedStopAt: 1000})
		defer online(t, api, "as-forced")()
		if rec := doAcceleratedStop(api, "as-forced"); rec.Code != http.StatusConflict {
			t.Fatalf("want 409, got %d — a force-stopped session is not working a "+
				"close-out, so a deadline addressed to it has no reader", rec.Code)
		}
	})
}

// The 下線 arm — the rung the owner actually walks: press 停止, wait, press
// 加速停止. This arm had NO clock and NO deadline field before, so every number
// below is new and all of them have to be the same number.
func TestAcceleratedStop_下線Arm_ClockAndSentenceAreOneNumber(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "as-soft", Kind: KindStaff,
		DesiredState: DesiredStateOffline, StoppingSince: 1000})
	defer online(t, api, "as-soft")()

	// Before: soft, no deadline anywhere, and nothing collects it — ever.
	before := reloadMember(t, dal, "as-soft")
	if kind, carries := offboardKindOf(before, nowSecs()); !carries || kind != offboardKindSoft {
		t.Fatalf("before the press: kind=%q carries=%v, want soft", kind, carries)
	}
	if d := winddownDeadlineOf(before, api.reconcileConfigLive()); d != 0 {
		t.Fatalf("before the press the 下線 arm quoted a deadline of %v", d)
	}

	if rec := doAcceleratedStop(api, "as-soft"); rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	got := reloadMember(t, dal, "as-soft")
	if got.RefocusOp != refocusOpAcceleratedStop {
		t.Fatalf("refocus_op=%q, want %q", got.RefocusOp, refocusOpAcceleratedStop)
	}
	if got.StoppingSince <= 1000 {
		t.Errorf("stopping_since=%v was not re-stamped — the owner's grace has to run "+
			"from HIS press, not from a 停止 he pressed hours earlier", got.StoppingSince)
	}
	if got.DesiredState != DesiredStateOffline {
		t.Errorf("desired_state=%q — 加速停止 escalates a stop, it does not change "+
			"what the owner asked for", got.DesiredState)
	}

	cfg := api.reconcileConfigLive()
	grace := cfg.RecycleGrace
	deadline := got.StoppingSince + grace

	// The sentence.
	kind, carries := offboardKindOf(got, got.StoppingSince+1)
	if !carries || kind != offboardKindFinal {
		t.Fatalf("after the press: kind=%q carries=%v — a clock the member is not "+
			"told about cuts its hand-off off with no warning at all", kind, carries)
	}
	// The wire field the cockpit renders.
	if d := winddownDeadlineOf(got, cfg); d != deadline {
		t.Errorf("refocus_deadline=%v, want %v", d, deadline)
	}
	// The clock that actually collects.
	obs := obsOf(got.ID, DesiredStateOffline, true)
	obs.RefocusOp = got.RefocusOp
	obs.StoppingSince = got.StoppingSince
	if d := reconcileDecide(obs, newReconcileState(), cfg, deadline-1); d.Command == reconcileCmdStop {
		t.Errorf("collected BEFORE the deadline it announced (%s)", d.Reason)
	}
	if d := reconcileDecide(obs, newReconcileState(), cfg, deadline); d.Command != reconcileCmdStop {
		t.Errorf("not collected AT the announced deadline — the member was told a "+
			"time nothing honours (%s)", d.Reason)
	}
}

// The 換手 arm. Same button, different anchor, and the re-stamp is load-bearing
// for the reason the context promotion documents: promoting in place would put
// the deadline at the ORIGINAL stamp, already in the past, and collect the member
// on the very tick that announced it.
func TestAcceleratedStop_換手Arm_RestampsSoTheDeadlineIsNotAlreadyGone(t *testing.T) {
	api, dal := newGateTestAPI(t)
	long := nowSecs() - 10_000
	putGateMember(t, dal, Member{ID: "as-recycle", Kind: KindStaff,
		DesiredState: DesiredStateOnline, RefocusSince: long, RefocusOp: refocusOpRefocus})
	defer online(t, api, "as-recycle")()

	if rec := doAcceleratedStop(api, "as-recycle"); rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	got := reloadMember(t, dal, "as-recycle")
	if got.RefocusOp != refocusOpAcceleratedStop {
		t.Fatalf("refocus_op=%q", got.RefocusOp)
	}
	if got.RefocusSince <= long {
		t.Fatalf("refocus_since=%v was not re-stamped — the deadline would be "+
			"%.0fs in the past and the member collected on the tick that announced it",
			got.RefocusSince, nowSecs()-long)
	}
	cfg := api.reconcileConfigLive()
	want := got.RefocusSince + cfg.RecycleGrace
	if d := winddownDeadlineOf(got, cfg); d != want {
		t.Errorf("refocus_deadline=%v, want %v", d, want)
	}
	if d := winddownDeadlineOf(got, cfg); d <= nowSecs() {
		t.Errorf("the announced deadline %v is already in the past", d)
	}
}

// The owner's knob reaches BOTH clocked causes — this is the 「統一在第二門檻跟
// 加速停止使用」 half of block 4, asserted from the endpoint side where the two
// causes can actually be told apart.
func TestAcceleratedStop_ReadsTheSameGraceTheSecondContextThresholdDoes(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const changed = 900
	api.settingsMu.Lock()
	api.acceleratedGraceSecs = changed
	api.settingsMu.Unlock()

	putGateMember(t, dal, Member{ID: "as-grace", Kind: KindStaff,
		DesiredState: DesiredStateOffline, StoppingSince: 1000})
	defer online(t, api, "as-grace")()
	if rec := doAcceleratedStop(api, "as-grace"); rec.Code != 200 {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	got := reloadMember(t, dal, "as-grace")
	cfg := api.reconcileConfigLive()

	manual, manualClocked := recycleGraceFor(refocusOpAcceleratedStop, cfg)
	auto, autoClocked := recycleGraceFor(refocusOpContextHigh, cfg)
	if !manualClocked || !autoClocked {
		t.Fatalf("one of the two 加速停止 causes lost its clock (manual=%v auto=%v)",
			manualClocked, autoClocked)
	}
	if manual != changed || auto != changed {
		t.Fatalf("the owner set %d and got manual=%v automatic=%v — two knobs where "+
			"the owner asked for one", changed, manual, auto)
	}
	if d := winddownDeadlineOf(got, cfg); d != got.StoppingSince+changed {
		t.Errorf("the endpoint quoted %v, want %v", d, got.StoppingSince+changed)
	}
}

// The outsource twin. The gate is the same, and the ONE thing this must not do
// is change what the worker's 停止 means — that is a separate piece of work.
func TestAcceleratedStop_OutsourceTwin(t *testing.T) {
	api, dal := newGateTestAPI(t)

	t.Run("refuses a worker with no handover open", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-as-idle", Status: WorkerStatusActive,
			DesiredState: DesiredStateOnline}
		if err := dal.PutOutsourceWorker(w); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		defer online(t, api, w.ID)()
		if rec := doWorkerAcceleratedStop(api, w.ID); rec.Code != http.StatusConflict {
			t.Fatalf("want 409, got %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("accelerates an open handover without touching desired_state", func(t *testing.T) {
		long := nowSecs() - 10_000
		w := OutsourceWorker{ID: "ow-as-open", Status: WorkerStatusActive,
			DesiredState: DesiredStateOnline, RefocusSince: long,
			RefocusOp: refocusOpRefocus}
		if err := dal.PutOutsourceWorker(w); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		defer online(t, api, w.ID)()

		if rec := doWorkerAcceleratedStop(api, w.ID); rec.Code != 200 {
			t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
		}
		got, err := dal.GetOutsourceWorker(w.ID)
		if err != nil || got == nil {
			t.Fatalf("reload: %v", err)
		}
		if got.RefocusOp != refocusOpAcceleratedStop || got.RefocusSince <= long {
			t.Fatalf("refocus_op=%q refocus_since=%v (was %v)",
				got.RefocusOp, got.RefocusSince, long)
		}
		// 🔴 The boundary with the OTHER step's work: 停止 for a worker still
		// means desired_state=offline + kill. This endpoint must not have moved
		// that intent, and must not have released or stopped the worker.
		if got.DesiredState != DesiredStateOnline {
			t.Errorf("desired_state=%q — 加速停止 changed what 停止 means, which is "+
				"another step's work and not this one's", got.DesiredState)
		}
		if got.Status != WorkerStatusActive {
			t.Errorf("status=%q — the worker was taken out of service by an "+
				"escalation that only puts a clock on a handover", got.Status)
		}
	})
}
