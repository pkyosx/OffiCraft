package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stop_epoch_single_source_t170e_test.go — T-170e stage 2 ②.
//
// The rule that was written twice is NOT forcedEpochLive (that has always had
// one definition, and the worker side calls it through memberFromWorker). It is
// the two COMPOUNDS wrapped around it:
//
//	(a) stopEpochAnchor        — a 停止 re-stamps stopping_since UNLESS the epoch
//	                             under way is a live forced one.
//	(b) gracefulStopEpochOpen  — 加速停止 escalates only an OPEN, NON-forced stop.
//
// Each had a staff copy and a worker copy. These tests are deliberately ONE PER
// SIDE and pinned to absolute values, for the same reason the receipt pair is:
// a test that only asserted "staff and worker agree" would stay green for every
// change to a shared core, which is the one thing that has to be observable.

func doWorkerStop(api *apiServer, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/outsource-workers/"+id+"/stop", nil)
	claims := map[string]any{"sub": "owner", "scope": "owner"}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost(rec, r, id)
	return rec
}

func doDeactivateMember(api *apiServer, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/members/"+id+"/deactivate", nil)
	claims := map[string]any{"sub": "owner", "scope": "owner"}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec, r, id)
	return rec
}

// ── (a) stopEpochAnchor ───────────────────────────────────────────────────────
//
// Re-stamping stopping_since on a live forced epoch moves the row from "cut off
// deliberately" to "working its close-out" — forcedEpochLive is scoped by
// forced_stop_at >= stopping_since — and the arm that must say NOTHING becomes
// the arm that speaks.

func TestStopEpochAnchor_AStaffDeactivateDoesNotRestampALiveForcedEpoch(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const opened = 1_000.0
	putGateMember(t, dal, Member{ID: "m-170e-forced", Kind: KindStaff,
		DesiredState: DesiredStateOffline, StoppingSince: opened, ForcedStopAt: opened})

	if rec := doDeactivateMember(api, "m-170e-forced"); rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}

	got := reloadMember(t, dal, "m-170e-forced")
	if got.StoppingSince != opened {
		t.Fatalf("stopping_since %v → %v: a 下線 pressed on a session that was already "+
			"CUT OFF moved the epoch to the graceful side of forcedEpochLive, and the "+
			"member starts being spoken to as though it were working a close-out",
			opened, got.StoppingSince)
	}
	if !forcedEpochLive(got) {
		t.Fatalf("the epoch stopped reading as forced: %+v", got)
	}

	// POSITIVE CONTROL: an ORDINARY 下線 must still stamp, or this guard has
	// simply broken the stop epoch instead of protecting one.
	t.Run("an ordinary stop still opens its epoch", func(t *testing.T) {
		putGateMember(t, dal, Member{ID: "m-170e-plain", Kind: KindStaff,
			DesiredState: DesiredStateOnline})
		if rec := doDeactivateMember(api, "m-170e-plain"); rec.Code != http.StatusOK {
			t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
		}
		if got := reloadMember(t, dal, "m-170e-plain"); got.StoppingSince <= 0 {
			t.Fatalf("stopping_since=%v — a plain 下線 must anchor its own epoch",
				got.StoppingSince)
		}
	})
}

func TestStopEpochAnchor_AWorkerStopDoesNotRestampALiveForcedEpoch(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const opened = 1_000.0
	w := OutsourceWorker{ID: "ow-170e-forced", Status: WorkerStatusActive,
		DesiredState: DesiredStateOffline, StoppingSince: opened, ForcedStopAt: opened}
	if err := dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}

	if rec := doWorkerStop(api, w.ID); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}

	got, err := dal.GetOutsourceWorker(w.ID)
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	if got.StoppingSince != opened {
		t.Fatalf("stopping_since %v → %v: 停止 pressed on a worker that was already "+
			"CUT OFF moved the epoch to the graceful side of forcedEpochLive — the "+
			"staff twin of this is TestStopEpochAnchor_AStaffDeactivateDoesNot"+
			"RestampALiveForcedEpoch, and both read ONE function",
			opened, got.StoppingSince)
	}
	if !forcedEpochLive(memberFromWorker(*got)) {
		t.Fatalf("the epoch stopped reading as forced: %+v", got)
	}

	t.Run("an ordinary stop still opens its epoch", func(t *testing.T) {
		plain := OutsourceWorker{ID: "ow-170e-plain", Status: WorkerStatusActive,
			DesiredState: DesiredStateOnline}
		if err := dal.PutOutsourceWorker(plain); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		if rec := doWorkerStop(api, plain.ID); rec.Code != http.StatusOK {
			t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
		}
		got, _ := dal.GetOutsourceWorker(plain.ID)
		if got.StoppingSince <= 0 {
			t.Fatalf("stopping_since=%v — a plain 停止 must anchor its own epoch",
				got.StoppingSince)
		}
	})
}

// ── (b) gracefulStopEpochOpen ─────────────────────────────────────────────────
//
// The staff half of this pair already had its sentinel: the "already cut off by
// 強制停止" case of TestAcceleratedStop_RefusesAMemberNobodyHasAskedToStop. The
// worker half had NONE — TestAcceleratedStop_OutsourceTwin exercises the 換手
// arm (refocus_since) and the no-handover refusal, never the 下線 arm's forced
// case — so the worker copy of this compound was decoration.

func TestGracefulStopEpochOpen_加速停止RefusesAForceStoppedWorker(t *testing.T) {
	api, dal := newGateTestAPI(t)
	const opened = 1_000.0
	w := OutsourceWorker{ID: "ow-170e-as-forced", Status: WorkerStatusActive,
		DesiredState: DesiredStateOffline, StoppingSince: opened, ForcedStopAt: opened}
	if err := dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	defer online(t, api, w.ID)()

	if rec := doWorkerAcceleratedStop(api, w.ID); rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d %s — a force-stopped worker is not working a "+
			"close-out, so a deadline addressed to it has no reader. The staff twin "+
			"is the \"already cut off by 強制停止\" case of "+
			"TestAcceleratedStop_RefusesAMemberNobodyHasAskedToStop",
			rec.Code, rec.Body.String())
	}
	got, _ := dal.GetOutsourceWorker(w.ID)
	if got.RefocusOp == refocusOpAcceleratedStop {
		t.Fatalf("the refused call still promoted the cause: %+v", got)
	}

	// POSITIVE CONTROL: an OPEN, non-forced stop epoch on the same 下線 arm is
	// still escalatable — otherwise this guard has removed the button rather
	// than scoping it.
	t.Run("a graceful stop epoch is still escalatable", func(t *testing.T) {
		soft := OutsourceWorker{ID: "ow-170e-as-soft", Status: WorkerStatusActive,
			DesiredState: DesiredStateOffline, StoppingSince: nowSecs() - 10_000}
		if err := dal.PutOutsourceWorker(soft); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		defer online(t, api, soft.ID)()
		if rec := doWorkerAcceleratedStop(api, soft.ID); rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
		}
		got, _ := dal.GetOutsourceWorker(soft.ID)
		if got.RefocusOp != refocusOpAcceleratedStop {
			t.Fatalf("refocus_op=%q, want %q", got.RefocusOp, refocusOpAcceleratedStop)
		}
	})
}
