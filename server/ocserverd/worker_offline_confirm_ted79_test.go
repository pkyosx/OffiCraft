package main

// worker_offline_confirm_ted79_test.go — T-ed79 parity #13: a worker walking its
// close-out is no longer cut off by ONE offline sample.
//
// owner 2026-08-21 (rc-7df3deb21b3b) reversed the original card. The card said
// "give staff the worker's behaviour"; the owner ruled the other way, verbatim:
// 「反過來但是不要三分鐘這麼久 他重新連上線應該不需要這麼長」— so the WORKER side
// has to confirm first, and staff is left exactly as it is.
//
// The three faces below are the whole ruling. A window that only collects late
// (face 2) but never cancels (face 3) is a delay, not a confirmation.

import "testing"

// stopWorkerFixture puts an ACTIVE+ONLINE worker into an open 停止 epoch — the
// arm that collects with collectWorkerStop (kill, no respawn) — then takes its
// session away, which is the network blip under test.
func stopWorkerFixture(t *testing.T, api *apiServer, now float64) string {
	t.Helper()
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	w.StoppingSince = now
	w.StoppedSince = 0.0
	w.RefocusSince = 0.0
	w.RefocusOp = ""
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("open the stop epoch: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	api.hub.DrainWardenCommands(ServerSelfHost)
	return workerID
}

func tickHandover(t *testing.T, api *apiServer, workerID string, now float64) int {
	t.Helper()
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	_ = w
	workerTickPass(t, api, workerID, now)
	return len(api.hub.DrainWardenCommands(ServerSelfHost))
}

// workerTickPass runs ONE tick's worth of the ACTIVE-worker branch for ONE
// worker — which is now THREE things, and all of them matter: the shared
// pre-decide formalities (T-170e stage 3), autoHandoverWorker (the loop-break
// and the 停止 arm), and then the SHARED FSM, which is where every handover 收口
// decision moved (T-72dd). A test that drives only the middle one is driving a
// function that no longer decides anything about a handover, and will read
// "nothing happened" as a behaviour claim when it is really just the wrong call
// site.
//
// 🔴 THE FIRST STEP IS NO LONGER HAND-COPIED, and the copy is why. This helper
// used to spell out the projection and the entry filter itself and call exactly
// ONE pass (stampContextHighRecycle) — which was the real tick's whole list when
// it was written, and had not been true since T-170e stage 1 wired the
// token-expiry and survived-stop passes beside it. Every test driving this
// helper was therefore measuring a tick a stage behind production, silently. It
// calls the SAME entry point runOutsourceTick calls now, so that gap cannot
// re-open.
//
// The re-read between the steps mirrors outsource_sched.go exactly: an earlier
// step may have cleared the epoch, and the FSM must never act on the stale row.
func workerTickPass(t *testing.T, api *apiServer, workerID string, now float64) {
	t.Helper()
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	api.outsourceMu.Lock()
	defer api.outsourceMu.Unlock()
	api.runWorkerLifecyclePasses([]OutsourceWorker{*w}, now)
	if reread, rerr := api.dal.GetOutsourceWorker(workerID); rerr == nil && reread != nil {
		w = reread
	}
	api.autoHandoverWorker(*w, now)
	fresh, ferr := api.dal.GetOutsourceWorker(workerID)
	if ferr != nil || fresh == nil || fresh.Status == WorkerStatusReleased ||
		fresh.DesiredState == DesiredStateOffline {
		return
	}
	api.reconcileWorkerLiveness(*fresh, now)
}

// dropWorkerSession removes every listener the worker holds — the server-side
// view of a disconnect, blip or death alike. They are indistinguishable at this
// instant, and that is the whole reason the window exists.
func dropWorkerSession(t *testing.T, api *apiServer, workerID string) {
	t.Helper()
	l, err := api.hub.Connect(workerID, "") // takeover of whatever listener is held
	if err != nil {
		t.Fatalf("takeover before drop: %v", err)
	}
	api.hub.Disconnect(l)
	if api.hub.IsOnline(workerID) {
		t.Fatalf("fixture: %s still reads online after dropping its session", workerID)
	}
}

// ── the handover arm after T-72dd ───────────────────────────────────────────
//
// 🔴 THE CONTRACT ON THIS ARM CHANGED, and it is not a relaxation — the three
// faces below used to describe autoHandoverWorker's OWN offline collect, which
// no longer exists. The handover 收口 is the shared FSM's, and the FSM's recycle
// arm requires an ONLINE session (decideUp: `obs.Online && obs.RefocusSince>0`).
// So "offline mid-handover" is no longer a collect question at all: there is
// nothing alive to kill, and the FSM simply re-STARTs the worker.
//
// What the confirmation window protected — 「不要因為一次取樣就砍掉還活著的
// worker」 — is NOT lost. It moved, and to a LONGER window: the only kill an
// offline worker can now attract is the zombie takeover, gated by
// ZombieConfirmGrace (2 × WakingTTLSecs), which is kept strictly LONGER than
// this window. The exact ratio is deliberately not written here — the
// assertion at the bottom of this file pins the ORDERING instead, so moving
// either constant cannot turn this sentence into a lie.
// A blip therefore costs at most one
// refused START (the warden's clobber-guard answers session_already_exists) —
// never a kill. That is the property the tests below assert, because it is the
// property the owner's ruling was about.
//
// workerOfflineConfirmGraceSecs itself is NOT dead: it still governs the 停止
// arm (autoHandoverWorker arm 0), which this ticket did not touch and which is
// pinned unchanged by TestWorkerStop_OfflineIsConfirmedBeforeTheCloseOutIsCollected
// below.

func TestWorkerHandover_OfflineMidHandoverIsRespawnedNeverKilled_T72dd(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)
	dropWorkerSession(t, api, workerID)

	// Straight away, and again well past the confirmation window: the answer is
	// the same both times, because offline is no longer a collect trigger.
	for _, at := range []float64{now, now + 30, now + workerOfflineConfirmGraceSecs + 1} {
		api.hub.DrainWardenCommands(ServerSelfHost)
		tickHandover(t, api, workerID, at)
		frames := api.hub.DrainWardenCommands(ServerSelfHost)
		stops := countStops(t, frames)
		t.Logf("at %+.0fs offline: %d frame(s), %d stop(s)", at-now, len(frames), stops)
		if stops != 0 {
			t.Fatalf("an offline mid-handover worker must NEVER be killed — there is "+
				"no live session to collect; got %d stop(s) at %+.0fs", stops, at-now)
		}
	}
	// …and no close-out was collected, because none happened: the session
	// vanished, it did not report anything.
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != 0 {
		t.Fatalf("nothing was collected, so nothing may latch stopped_since (got %v)",
			fresh.StoppedSince)
	}
}

// A reconnect mid-handover leaves the worker alone: it is online, its epoch is
// still open, and it has not reported done — so the recycle arm waits, exactly
// as it does for staff.
func TestWorkerHandover_ReconnectMidHandoverIsNotKilled_T72dd(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now-10)
	// 🔴 A CLOCKLESS cause on purpose. stampWorkerRefocus stamps context_high,
	// which IS on a clock — a worker on that cause is collected at its deadline
	// whether it reconnected or not, and that is the 加速停止 contract, not a bug.
	// The claim here is about the arm with NO clock: 重新聚焦 is collected by the
	// agent's own report and by nothing else, so a reconnect must leave it alone
	// no matter how long the tick waits.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusOp = refocusOpRefocus
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("re-stamp op: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	api.hub.DrainWardenCommands(ServerSelfHost)
	dropWorkerSession(t, api, workerID)
	tickHandover(t, api, workerID, now)
	if _, err := api.hub.Connect(workerID, ""); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	// Long past the confirmation window, and long past ZombieConfirmGrace too — the
	// reconnect is what makes both irrelevant.
	tickHandover(t, api, workerID, now+30)
	tickHandover(t, api, workerID, now+30+2*workerOfflineConfirmGraceSecs)
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if got := countStops(t, frames); got != 0 {
		t.Fatalf("a reconnected worker that has NOT reported done must not be "+
			"collected — the recycle arm waits for the dump; got %d stop(s)", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != 0 {
		t.Fatal("a live worker mid-handover must not have its close-out latched for it")
	}
}

// ── the same three faces on the 停止 arm, which collects WITHOUT a respawn ───

func TestWorkerStop_OfflineIsConfirmedBeforeTheCloseOutIsCollected(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := stopWorkerFixture(t, api, now)
	dropWorkerSession(t, api, workerID)

	tickHandover(t, api, workerID, now) // arms
	if got := tickHandover(t, api, workerID, now+30); got != 0 {
		t.Fatalf("a 停止 close-out must not be cut off by a 30s blip, got %d frames", got)
	}
	if got := tickHandover(t, api, workerID, now+workerOfflineConfirmGraceSecs); got != 1 {
		t.Fatalf("the confirmed-gone 停止 collect kills and does NOT respawn — want 1 "+
			"warden frame, got %d", got)
	}
}

// ── the number itself ───────────────────────────────────────────────────────

// The window is ONE constant so a later owner ruling is a one-line edit. The
// honest reconnect floor is about 90s (idle-read watchdog 45s + backoff cap 15s
// + one 30s cadence tick); owner 2026-08-27 (rc-dbee69264859) selected 120s.
// Keep this value independent of WakingTTLSecs even though both are 120s today.
func TestWorkerOfflineConfirmGraceMatchesOwnerRuling(t *testing.T) {
	if workerOfflineConfirmGraceSecs != 120.0 {
		t.Errorf("workerOfflineConfirmGraceSecs = %v, want 120 — owner 2026-08-27 "+
			"(rc-dbee69264859) selected this independent confirmation window; it "+
			"must not be re-derived from WakingTTLSecs or ZombieConfirmGrace.",
			workerOfflineConfirmGraceSecs)
	}
	if workerOfflineConfirmGraceSecs >= defaultReconcileConfig().ZombieConfirmGrace {
		t.Errorf("the worker confirm window (%v) must be SHORTER than ZombieConfirmGrace "+
			"(%v) — 「不要三分鐘這麼久」", workerOfflineConfirmGraceSecs,
			defaultReconcileConfig().ZombieConfirmGrace)
	}
}
