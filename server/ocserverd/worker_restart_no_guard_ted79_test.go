package main

// worker_restart_no_guard_ted79_test.go — T-ed79 parity #10: 重啟 on a worker
// that is still alive is no longer refused.
//
// owner 2026-08-21 ruled 「往正職靠：外包也不擋」. The staff side has no such
// guard: 活化 on a live member is simply honoured. The worker side answered a
// flat 409 「worker is still online — nothing to restart」.
//
// 🔴 THE SENTENCE HAD TO SURVIVE THE REFUSAL. Dropping the guard on its own
// would trade one clear line for a warden-level bounce
// ("session_already_exists"), which is the opposite of what the SAME owner ruled
// on #4, #12 and #14 in the same pass. So the fact is now a RECEIPT in the
// reason-code family instead of a refusal: it says the same thing, and it says
// it on the row the cockpit already renders.
//
// ⚠️ T-65 包④ CHANGED WHAT "NOT REFUSED" BUYS, and only that. The 200 is
// unchanged and so is the receipt; what follows the 200 is not. This file used
// to assert that the 200 was followed by a DISPLACEMENT (stop+start), on the
// argument that "a restart that kills the old session before starting the new
// one is not a double-spawn". The owner ruled on 2026-09-06
// (rc-1f591528a6d0 圈 [0]) 「收斂成『正在跑就不動它』」, so the same press now
// answers 200 and dispatches NOTHING. Both facts are still asserted below —
// the refusal is still gone, and now the displacement is too. The full
// four-part guard over the live arm (no frames, session survives, the three
// epoch anchors untouched, activation_pending false) is
// worker_restart_live_session_noop_t65_test.go.

import (
	"net/http"
	"strings"
	"testing"
)

func TestRestartALiveWorkerIsNotRefused(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code == http.StatusConflict {
		t.Fatalf("重啟 on a live worker answered 409 %s — owner 2026-08-21 「往正職靠："+
			"外包也不擋」. 活化 on a live STAFF member is honoured, and a restart that "+
			"kills the old session before starting the new one is not a double-spawn.",
			rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	// NOT REFUSED, AND NOT DISPLACED EITHER (T-65 包④). The 200 above is the
	// half T-ed79 #10 bought; this half is the owner's 2026-09-06 ruling. A
	// 喚醒 pressed on a worker that is still running must leave it running —
	// no kill, no second start.
	var rpcs []string
	for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
		rpc, _ := decodeWardenFrame(t, f.Frame)
		rpcs = append(rpcs, rpc)
	}
	if len(rpcs) != 0 {
		t.Fatalf("喚醒 on a live worker dispatched %v — it must dispatch NOTHING. "+
			"owner 2026-09-06 rc-1f591528a6d0 圈 [0] 「收斂成『正在跑就不動它』」: the "+
			"press that used to send stop+start took half an hour of unwritten work "+
			"with it, and 正職 活化 on a live member has never sent a frame.", rpcs)
	}
	if !api.hub.IsOnline(workerID) {
		t.Error("喚醒 ended the live session it was pressed on — the one thing the " +
			"ruling says it must not do")
	}
}

// The sentence the 409 used to carry must still be reachable, in the family, on
// the row — not only in a warden's refusal text.
func TestRestartALiveWorkerLeavesTheStillOnlineReceipt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	api.hub.DrainWardenCommands(ServerSelfHost)

	if rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost); rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}

	after, _ := api.dal.GetOutsourceWorker(workerID)
	if !strings.HasPrefix(after.LastOpReason, spawnReasonSessionAlive+":") {
		t.Errorf("last_op_reason = %q, want a %q receipt. The 409 this replaced told "+
			"the owner something true — the worker was still running — and losing that "+
			"sentence is the one cost PARITY-14 flagged on this ruling.",
			after.LastOpReason, spawnReasonSessionAlive)
	}
}

// A worker whose session is genuinely gone is NOT stamped: the receipt names a
// fact, and inventing it for every restart would make it worthless.
func TestRestartADeadWorkerLeavesNoSessionAliveReceipt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // no SSE — the session died
	api.hub.DrainWardenCommands(ServerSelfHost)

	if rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost); rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if strings.HasPrefix(after.LastOpReason, spawnReasonSessionAlive+":") {
		t.Errorf("a worker with no live session was stamped %q — that is not true of it",
			after.LastOpReason)
	}
}
