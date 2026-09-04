package main

// worker_restart_clears_markers_ted79_test.go — T-ed79 parity #11: 重啟 starts a
// new session, so it must start from a clean sheet.
//
// The staff activate has always cleared the anchors that describe the session it
// is replacing, and deliberately KEPT forced_stop_at (the record that a PAST
// session was cut off — the reader who needs it most is the one that comes
// after). The worker restart cleared NOTHING: it wrote desired_state and
// returned. worker_spawn.go names one of the leftovers by name — "NOTHING clears
// the second one … so it outlives the whole stop→restart cycle".

import (
	"net/http"
	"testing"
)

// workerCarryingAStaleEpoch: an already-COLLECTED wind-down (refocus + stopped
// both latched) that the owner then stopped, which is the state a restart lands
// on. Both anchors describe a session that no longer exists.
func workerCarryingAStaleEpoch(t *testing.T, api *apiServer, workerID string) {
	t.Helper()
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 1000.0
	w.RefocusOp = refocusOpRefocus
	w.StoppingSince = 1001.0
	w.StoppedSince = 1002.0
	w.ForcedStopAt = 1003.0
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("seed the stale epoch: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
}

func TestRestartWorkerClearsThePreviousSessionsAnchors(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	workerCarryingAStaleEpoch(t, api, workerID)

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}

	after, _ := api.dal.GetOutsourceWorker(workerID)
	for _, anchor := range []struct {
		name string
		got  float64
	}{
		{"refocus_since", after.RefocusSince},
		{"stopping_since", after.StoppingSince},
		{"stopped_since", after.StoppedSince},
	} {
		if anchor.got != 0 {
			t.Errorf("重啟 left %s=%v behind. It describes the session the restart is "+
				"REPLACING; carried into the next one it is read as a fact about that "+
				"one.", anchor.name, anchor.got)
		}
	}
	if after.RefocusOp != "" {
		t.Errorf("重啟 left refocus_op=%q — the cause goes with the window", after.RefocusOp)
	}
	// …and the one that must SURVIVE, for the reason migrations/00057 states: it
	// does not describe this session, it describes the one BEFORE it, and the
	// reader who needs it most is the one that comes after.
	if after.ForcedStopAt != 1003.0 {
		t.Errorf("重啟 cleared forced_stop_at (%v, want 1003) — that is the durable "+
			"record that a past session was CUT OFF, not an anchor of the session "+
			"being replaced. Staff activate keeps it on purpose and says so in three "+
			"places (api_members.go, dal.go, migrations/00057).", after.ForcedStopAt)
	}
}

// The consequence a stale pair produces, which the epoch-scoped predicate does
// NOT heal: workerHasStateToFlush asks "is THIS epoch's wind-down collected?",
// and a leftover refocus+stopped pair answers YES about an epoch that ended
// before this session existed — so the next owner verb is shot on the spot with
// no close-out at all.
func TestOwnerVerbAfterARestartStillWindsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	workerCarryingAStaleEpoch(t, api, workerID)

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	setWorkerModelBody(t, api, workerID, map[string]any{"model": "claude-opus-4-9"})

	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince <= 0 {
		t.Error("a 換 model on the freshly restarted session was taken IMMEDIATELY " +
			"instead of opening a wind-down — the leftover refocus+stopped pair from " +
			"the previous session answered 'this epoch is already collected' for an " +
			"epoch that ended before this session booted")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("the wind-down must not kill anything yet, got %d warden frames", got)
	}
}
