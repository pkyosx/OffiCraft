package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The cockpit's 成本歸零 button (T-53, owner ruling rc-7dea0deefa63 option 0
// 「最小、不可逆」). The owner-visible 估計$ is TWO numbers added on the client —
// the durable banked_cost column and the live in-memory telemetry figure — and
// the whole risk in this feature is clearing one of them.
//
// 🔴 MUTANT: delete the `s.dropLiveCost(...)` call from either branch of
// HandleResetCostApiMembersMemberIdCostResetPost and
// TestResetCost_ClearsTheLiveFigureNotJustTheDurableOne (both halves) goes RED.
// A test that only asserts banked_cost == 0 passes that mutant, and the mutant
// ships a button the owner presses to no visible effect.

func doResetCost(t *testing.T, s *apiServer, actorID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResetCostApiMembersMemberIdCostResetPost(rec,
		taskReq(t, "POST", "/api/members/"+actorID+"/cost/reset", map[string]any{},
			wireOwnerID, "owner"), actorID)
	return rec
}

func costResetBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, rec.Body.String())
	}
	return out
}

func costResetServer(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
}

// liveCostOf reads the live half straight out of the telemetry store — the
// value the cockpit would add to the durable half on its very next read. Going
// through the store rather than an HTTP projection is deliberate: it is the
// only way to tell "the figure is gone" apart from "the figure is not being
// rendered right now".
func liveCostOf(s *apiServer, actorID string) (float64, bool) {
	entry := s.telemetry.Get(actorID)
	if entry == nil {
		return 0, false
	}
	v, ok := entry["cost"].(float64)
	return v, ok
}

func TestResetCost_ClearsTheLiveFigureNotJustTheDurableOne(t *testing.T) {
	t.Run("staff member", func(t *testing.T) {
		s := costResetServer(t)
		m := fullMember("seth")
		m.BankedCost = 4.0
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
			t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
		}

		doResetCost(t, s, "seth")

		after, err := s.dal.GetMember("seth")
		if err != nil || after == nil {
			t.Fatalf("re-read member: %v", err)
		}
		if after.BankedCost != 0 {
			t.Errorf("banked_cost = %v, want 0", after.BankedCost)
		}
		// The half that makes this button real. Leave it behind and the number
		// is back on the owner's screen at the next refresh, which he cannot
		// tell apart from the button doing nothing.
		if v, present := liveCostOf(s, "seth"); present {
			t.Errorf("live cost still %v — the durable half alone is not a reset; "+
				"the cockpit adds this back in on its next read", v)
		}
	})

	t.Run("outsource worker", func(t *testing.T) {
		s := costResetServer(t)
		seedWorker(t, s, "ow-7", "S7", 0.25, WorkerStatusActive)
		if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":0.5}`); rec.Code != 200 {
			t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
		}

		doResetCost(t, s, "ow-7")

		after, err := s.dal.GetOutsourceWorker("ow-7")
		if err != nil || after == nil {
			t.Fatalf("re-read worker: %v", err)
		}
		if after.BankedCost != 0 {
			t.Errorf("banked_cost = %v, want 0", after.BankedCost)
		}
		if v, present := liveCostOf(s, "ow-7"); present {
			t.Errorf("live cost still %v — same failure as the member arm, and the "+
				"reason one route serves both kinds", v)
		}
	})
}

// The receipt is the ONLY record of what was destroyed: no snapshot is kept, no
// undo route exists, and spend has no per-charge ledger behind it. If it
// answered with the post-reset state it would say nothing at all.
func TestResetCost_ReceiptCarriesWhatWasDestroyedNotTheZeroesLeftBehind(t *testing.T) {
	s := costResetServer(t)
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
		t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
	}

	got := costResetBody(t, doResetCost(t, s, "seth"))

	if got["member_id"] != "seth" {
		t.Errorf("member_id = %v, want seth", got["member_id"])
	}
	if got["cleared_cost"] != 1.5 {
		t.Errorf("cleared_cost = %v, want the 1.5 that was destroyed", got["cleared_cost"])
	}
	if got["cleared_banked_cost"] != 4.0 {
		t.Errorf("cleared_banked_cost = %v, want the 4.0 that was destroyed",
			got["cleared_banked_cost"])
	}
}

// null means "there was nothing to clear on that half", NOT "zero was cleared".
// That distinction is what lets the cockpit keep its existing "both null → dash"
// rule: after a reset the 估計$ cell reads 未量到 rather than 花了 0 元, with no
// display-side special case and no 'was reset' flag.
func TestResetCost_NothingToClearAnswersNullRatherThanZero(t *testing.T) {
	s := costResetServer(t)
	quiet := fullMember("quiet")
	// fullMember is a fully-populated fixture and carries a banked figure; this
	// test is about an actor with NOTHING measured, so zero it explicitly.
	quiet.BankedCost = 0
	if err := s.dal.PutMember(quiet); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	got := costResetBody(t, doResetCost(t, s, "quiet"))

	if got["cleared_cost"] != nil {
		t.Errorf("cleared_cost = %v, want null — nothing was measured, and 0 would "+
			"read as 'zero was cleared'", got["cleared_cost"])
	}
	if got["cleared_banked_cost"] != nil {
		t.Errorf("cleared_banked_cost = %v, want null", got["cleared_banked_cost"])
	}
}

// Pressing it twice is not an error, and the second press must not invent a
// figure that the first one already destroyed.
func TestResetCost_IsIdempotent(t *testing.T) {
	s := costResetServer(t)
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	costResetBody(t, doResetCost(t, s, "seth"))
	second := costResetBody(t, doResetCost(t, s, "seth"))

	if second["cleared_banked_cost"] != nil || second["cleared_cost"] != nil {
		t.Errorf("second reset destroyed something: %v — the first one already took it", second)
	}
}

func TestResetCost_UnknownActorIs404(t *testing.T) {
	s := costResetServer(t)
	if rec := doResetCost(t, s, "nobody"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown actor: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// A released worker CAN be reset, and this test exists because the endpoint
// used to refuse it. Owner ruling rc-1344cc76a24a (2026-09-02) 「連已經退場的也
// 要能清（帳號卡才會真的歸零）」 overrode that: released is the steady state for
// a worker (ReleaseWorkersForTask fires on every task close) and its spend
// deliberately stays in the account total, so refusing it here left a residue
// the owner could never clear however many buttons he pressed.
//
// 🔴 MUTANT: restore `|| wk.Status == WorkerStatusReleased` to the worker
// branch's 404 guard → RED. That guard is what this ruling removed, and it is
// the one place a well-meaning "make it consistent with the other outsource
// doors" edit would put it back.
func TestResetCost_ReleasedWorkerCanBeReset(t *testing.T) {
	s := costResetServer(t)
	seedWorker(t, s, "ow-gone", "S9", 3.0, WorkerStatusReleased)
	if rec := doIngestTelemetry(s, "ow-gone", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":0.75}`); rec.Code != 200 {
		t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
	}

	got := costResetBody(t, doResetCost(t, s, "ow-gone"))

	if got["cleared_banked_cost"] != 3.0 {
		t.Errorf("cleared_banked_cost = %v, want 3.0", got["cleared_banked_cost"])
	}
	if got["cleared_cost"] != 0.75 {
		t.Errorf("cleared_cost = %v, want 0.75", got["cleared_cost"])
	}
	after, err := s.dal.GetOutsourceWorker("ow-gone")
	if err != nil || after == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if after.BankedCost != 0 {
		t.Errorf("banked_cost = %v, want 0", after.BankedCost)
	}
	if v, present := liveCostOf(s, "ow-gone"); present {
		t.Errorf("live cost still %v — both halves must go for a released worker too", v)
	}
}

// 🔴 THE SEPARATION, asserted from the actor side. Owner ruling rc-5c5d7c7c6dcd
// (2026-09-02) split the two figures: the account card is its own accumulator,
// so clearing actors one by one does NOT bring it down.
//
// This test used to assert the OPPOSITE — that clearing every actor drove the
// account card to absent — under the earlier ruling rc-1344cc76a24a. The owner
// then asked for「帳號上面的清除可以跟 agent 上面的清除分開」, which makes the old
// assertion false by decision rather than by accident. It is kept, inverted,
// because the pair of resets touching each other is exactly the regression the
// ruling forbids, and nothing else would notice.
func TestResetCost_ClearingActorsLeavesTheAccountCardAlone(t *testing.T) {
	s := costResetServer(t)
	seedRegisteredMachine(t, s, "m-seth-m5")
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	seedWorker(t, s, "ow-live", "S7", 0.25, WorkerStatusActive)
	seedWorker(t, s, "ow-gone", "S9", 3.0, WorkerStatusReleased)
	for _, id := range []string{"seth", "ow-live", "ow-gone"} {
		if rec := doIngestTelemetry(s, id, "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	before := accountRow(t, monitoringOf(t, doGetMonitoring(s,
		map[string]any{"sub": "owner", "scope": "owner"})), "seth-m5-claude")
	if before["cost"] != 4.5 {
		t.Fatalf("fixture premise: account = %v, want 4.5 (three actors reporting "+
			"1.5 each)", before["cost"])
	}

	for _, id := range []string{"seth", "ow-live", "ow-gone"} {
		costResetBody(t, doResetCost(t, s, id))
	}

	after := accountRow(t, monitoringOf(t, doGetMonitoring(s,
		map[string]any{"sub": "owner", "scope": "owner"})), "seth-m5-claude")
	if after["cost"] != 4.5 {
		t.Errorf("account cost = %v, want 4.5 untouched — clearing members must "+
			"leave the account card alone (rc-5c5d7c7c6dcd). A drop here means the "+
			"two buttons the owner asked to separate are joined again", after["cost"])
	}
	// And the actors really were cleared, so the assertion above cannot pass
	// because the resets did nothing at all.
	for _, id := range []string{"seth", "ow-live", "ow-gone"} {
		if v, present := liveCostOf(s, id); present {
			t.Errorf("%s live cost still %v — the per-actor reset did not run", id, v)
		}
	}
}

// A failed durable write must destroy NOTHING — found by independent review
// (T-54), which probed a PutMember failure and caught the live figure already
// gone while banked_cost still stood, on a request that answered 500.
//
// This is the worst shape this endpoint can take. The live figure exists in one
// process's memory and nowhere else; the receipt is the only record of what a
// reset destroyed, and a 500 carries no receipt. So dropping it before the
// durable write means a failed request silently annihilates half the number
// with nothing anywhere able to reconstruct it — and the owner, seeing an
// error, would reasonably assume nothing happened.
//
// The injection closes the WRITE pool while leaving the READ pool open, which
// is why it needs NewDALPools rather than the shared-handle test DAL: closing a
// shared handle fails the handler's own lookup too, so it never reaches the
// drop and the bug hides. That is exactly what happened on the first attempt at
// this test — it passed against the buggy ordering.
//
// 🔴 MUTANT: move the `s.dropLiveCost(...)` call back above its arm's durable
// write (either arm) → the matching sub-test goes RED.
func TestResetCost_AFailedDurableWriteDestroysNothing(t *testing.T) {
	// writeDeadServer returns a server whose reads work and whose every write
	// fails — the production two-pool shape with the write half shut.
	writeDeadServer := func(t *testing.T, seed func(*apiServer)) *apiServer {
		t.Helper()
		path := filepath.Join(t.TempDir(), "cost-reset.db")
		w, err := openSQLite(path)
		if err != nil {
			t.Fatalf("open write pool: %v", err)
		}
		if err := runMigrations(w); err != nil {
			t.Fatalf("goose up: %v", err)
		}
		r, err := openSQLite(path)
		if err != nil {
			t.Fatalf("open read pool: %v", err)
		}
		t.Cleanup(func() { r.Close() })
		s := &apiServer{dal: NewDALPools(w, r), hub: NewHub(),
			telemetry: newMemStore(), gauge: newMemStore()}
		seed(s)
		if err := w.Close(); err != nil {
			t.Fatalf("close write pool: %v", err)
		}
		return s
	}

	t.Run("staff member", func(t *testing.T) {
		s := writeDeadServer(t, func(s *apiServer) {
			m := fullMember("seth")
			m.BankedCost = 4.0
			if err := s.dal.PutMember(m); err != nil {
				t.Fatalf("seed member: %v", err)
			}
			if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
				`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
				t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
			}
		})
		// Premise: the handler can still SEE the actor, so it really does reach
		// the write. Without this the test could pass for the wrong reason.
		if m, err := s.dal.GetMember("seth"); err != nil || m == nil {
			t.Fatalf("read pool must still serve: m=%v err=%v", m != nil, err)
		}

		if rec := doResetCost(t, s, "seth"); rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on a failed durable write, got %d %s",
				rec.Code, rec.Body.String())
		}

		// The half that cannot be reconstructed from anywhere.
		v, present := liveCostOf(s, "seth")
		if !present {
			t.Fatal("live cost was destroyed by a request that then failed — the " +
				"figure exists only in memory and the 500 carried no receipt, so " +
				"nothing can put it back")
		}
		if v != 1.5 {
			t.Errorf("live cost = %v, want 1.5 untouched", v)
		}
	})

	t.Run("outsource worker", func(t *testing.T) {
		s := writeDeadServer(t, func(s *apiServer) {
			seedWorker(t, s, "ow-7", "S7", 0.25, WorkerStatusActive)
			if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
				`{"runtime":"claude","account":"seth-m5-claude","cost":0.5}`); rec.Code != 200 {
				t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
			}
		})
		if wk, err := s.dal.GetOutsourceWorker("ow-7"); err != nil || wk == nil {
			t.Fatalf("read pool must still serve: wk=%v err=%v", wk != nil, err)
		}

		if rec := doResetCost(t, s, "ow-7"); rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on a failed durable write, got %d %s",
				rec.Code, rec.Body.String())
		}

		v, present := liveCostOf(s, "ow-7")
		if !present {
			t.Fatal("live cost destroyed on a failed worker reset — both arms must " +
				"fail the same way")
		}
		if v != 0.5 {
			t.Errorf("live cost = %v, want 0.5 untouched", v)
		}
	})
}

// THE WIRE HALF, which the durable half quietly took with it. banked_cost is
// no longer written through PutMember (T-14 項目 6), so the reset writes ONE
// column — and the member delta putMember used to fan for free had to be
// published by hand instead. Losing it costs nothing at write time and nothing
// in any other test here: the column is 0, the receipt is right, and the
// cockpit simply never hears about it.
//
// The two arms differ ON PURPOSE, and this pins BOTH sides of that asymmetry —
// the same split bankLiveCost's own parity test makes (worker_lifecycle_test):
// a staff member gets a member patch, and an outsource worker gets NONE, because
// a worker's changes ride the outsource_worker projection and a member patch
// naming an ow- id is the thing that split exists to prevent.
//
// 🔴 MUTANTS: drop the publishMemberPatch call → the staff arm goes red; fan a
// member patch from the worker arm "for symmetry" → the worker arm goes red.
func TestResetCost_FansAMemberDeltaForStaffAndNoneForAWorker(t *testing.T) {
	memberFrames := func(t *testing.T, actorID string, seed func(*apiServer)) []string {
		t.Helper()
		s := costResetServer(t)
		seed(s)
		l, err := s.hub.Connect(actorID, "")
		if err != nil {
			t.Fatalf("connect SSE: %v", err)
		}
		t.Cleanup(func() { s.hub.Disconnect(l) })
		if rec := doResetCost(t, s, actorID); rec.Code != http.StatusOK {
			t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
		}
		var out []string
		for frame := l.pop(); frame != nil; frame = l.pop() {
			if strings.Contains(string(frame), `"topic":"member"`) {
				out = append(out, string(frame))
			}
		}
		return out
	}

	t.Run("staff member", func(t *testing.T) {
		frames := memberFrames(t, "seth", func(s *apiServer) {
			m := fullMember("seth")
			m.BankedCost = 4.0
			if err := s.dal.PutMember(m); err != nil {
				t.Fatalf("seed member: %v", err)
			}
		})
		if len(frames) == 0 {
			t.Error("no member delta went out — the cockpit is told nothing, so the " +
				"panel keeps showing the figure that was just destroyed until " +
				"something unrelated happens to refresh it")
		}
	})

	t.Run("outsource worker", func(t *testing.T) {
		frames := memberFrames(t, "ow-7", func(s *apiServer) {
			seedWorker(t, s, "ow-7", "S7", 0.25, WorkerStatusActive)
		})
		if len(frames) > 0 {
			t.Errorf("a worker reset must never fan a member patch naming an ow- id "+
				"(its changes ride the outsource_worker projection): %s", frames[0])
		}
	})
}
