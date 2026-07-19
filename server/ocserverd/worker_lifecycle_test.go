package main

// worker_lifecycle_test.go — the T-32e1/T-f190 outsource-worker lifecycle ops:
// refocus (換手) / stop (停止) / restart (重啟) / model (換 model) HTTP handlers, the
// spawn_state "stopped" projection, and the context-high AUTO-handover tick
// branch. Owner mental model: an outsource worker is just a member the system
// creates and deletes, so every op reuses a member mechanism.
//
// Each negative assertion below was hand-verified against a mutant (the mutant
// that would turn it green→red is named in the comment) and paired with a
// positive control in the same test, per the team's quality bar.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── presence under owner-explicit stop ───────────────────────────────────────

// TestWorkerPresence_StopIntent: an owner-stopped worker (desired_state
// 'offline') projects the member exit vocabulary regardless of its
// assigned/active lifecycle status — "stopping" while the session still winds
// down (SSE alive), "stopped" once it is gone — never a fake-green latch. A
// RELEASED row still projects "" (off-panel). Mutant: dropping the StopIntent
// fold in workerPresence turns the offline-intent rows back to online/waking
// → red.
func TestWorkerPresence_StopIntent(t *testing.T) {
	const now = 1_000_000.0
	cases := []struct {
		name    string
		status  string
		desired string
		online  bool
		want    string
	}{
		{"active+online but offline-intent is stopping", WorkerStatusActive, DesiredStateOffline, true, "stopping"},
		{"assigned but offline-intent is stopped", WorkerStatusAssigned, DesiredStateOffline, false, "stopped"},
		{"active+online with online-intent is online", WorkerStatusActive, DesiredStateOnline, true, "online"},
		{"released even if offline-intent is blank", WorkerStatusReleased, DesiredStateOffline, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := OutsourceWorker{ID: "ow-1", Status: c.status, TaskID: "t-1",
				CreatedTS: now - 5, DesiredState: c.desired}
			dto := newOutsourceWorkerDTO(w, nil, outsourceWorkerProjection{now: now, online: c.online})
			if dto.Presence != c.want {
				t.Fatalf("presence = %q, want %q", dto.Presence, c.want)
			}
		})
	}
}

// ── shared setup ─────────────────────────────────────────────────────────────

// seedLiveWorkerEnv seeds an eligible+online warden (ServerSelfHost) and the
// review-pr manual so a respawn actually picks a target + folds a boot context.
func seedLiveWorkerEnv(t *testing.T, api *apiServer) {
	t.Helper()
	seedMachine(t, api, ServerSelfHost)   // Kind=warden, active → an eligible target
	connectWarden(t, api, ServerSelfHost) // online on the hub
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
}

// newActiveWorker builds an ACTIVE worker bound to a live outsource task, with a
// known online last-spawn target (ServerSelfHost). online=true also holds a live
// worker SSE (spawn_state online); online=false leaves it disconnected (the
// claimed-then-died shape). The env warden is already seeded so a respawn from
// the worker dispatches worker_stop (old target) + worker_start.
func newActiveWorker(t *testing.T, api *apiServer, online bool) string {
	t.Helper()
	seedLiveWorkerEnv(t, api)
	task := createOutsourceTask(t, api, "review-pr", "review")
	workerID := "ow-" + newHexID(6)
	// Codename derives from the unique worker id — the member.codename UNIQUE
	// index (00025) rejects a second fixture reusing a literal like "S-1".
	w := OutsourceWorker{ID: workerID, Codename: "S-" + workerID, Model: "claude-sonnet-4-5",
		Effort: "medium", TaskID: task.ID, Status: WorkerStatusActive,
		DesiredState: DesiredStateOnline}
	if err := api.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("get task: %v", err)
	}
	bound.ExecutorID = workerID // bind so notifyWorkerSpawn sees a live task
	if err := api.dal.PutTask(*bound); err != nil {
		t.Fatalf("bind task: %v", err)
	}
	if online {
		if _, err := api.hub.Connect(workerID, ""); err != nil {
			t.Fatalf("connect worker SSE: %v", err)
		}
	}
	api.workerSpawnTarget[workerID] = ServerSelfHost // a known online old session
	return workerID
}

// newActiveOnlineWorker is the common active+online case.
func newActiveOnlineWorker(t *testing.T, api *apiServer) string {
	return newActiveWorker(t, api, true)
}

func postWorker(t *testing.T, api *apiServer, workerID, op string, body map[string]any,
	h func(http.ResponseWriter, *http.Request, string)) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/"+op, body,
		wireOwnerID, "owner"), workerID)
	return rec
}

// ── refocus (換手) ─────────────────────────────────────────────────────────────

// TestRefocusWorker_OnlineOpensGraceWindow (T-ea82): refocus on an active+online
// worker stamps refocus_since and fans the member-topic SOP 預告 at the worker's
// OWN session — and dispatches NO kill: the 收口 belongs to the stopped-report /
// grace-timeout drivers. Lifecycle untouched. Mutants: (a) keeping the old
// synchronous respawn → 2 frames (red on the 0-frame assertion); (b) dropping
// the 預告 publish → no member delta on the worker's listener (red).
func TestRefocusWorker_OnlineOpensGraceWindow(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	l, err := api.hub.Connect(workerID, "") // takeover of the fixture listener
	if err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })

	rec := postWorker(t, api, workerID, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.RefocusSince == 0 {
		t.Fatal("refocus must stamp refocus_since")
	}
	if w.Status != WorkerStatusActive {
		t.Errorf("refocus must not change lifecycle, status = %q", w.Status)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("graceful refocus must dispatch NO kill/respawn (grace open), got %d frames", got)
	}
	nudged := false
	for frame := l.pop(); frame != nil; frame = l.pop() {
		if strings.Contains(string(frame), `"topic":"member"`) &&
			strings.Contains(string(frame), workerID) {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("refocus must fan the member-topic SOP 預告 at the worker's own session")
	}
}

// TestWorkerReportStopped_CollectsHandover (T-ea82 form ①, 預告→report_stopped→
// respawn): the worker walks its SOP — report_stopping stamps the wind-down
// anchor without any kill; the FIRST report_stopped of the refocus-marked
// worker runs the 收口 (worker_stop + worker_start, same host) and latches
// stopped_since. refocus_since stays put (the boot loop-break owns it).
// Mutant: dropping the KindOutsource collect branch in HandleReportStopped →
// 0 frames (red).
func TestWorkerReportStopped_CollectsHandover(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	rec := postWorker(t, api, workerID, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("grace open must dispatch nothing, got %d frames", got)
	}

	rec = httptest.NewRecorder()
	api.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("worker report_stopping: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.StoppingSince <= 0 {
		t.Fatal("report_stopping must stamp the worker's stopping_since")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("report_stopping must never kill, got %d frames", got)
	}

	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("worker report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("the first stopped-report must collect (stop+start), got %d frames", len(frames))
	}
	rpc0, _ := decodeWardenFrame(t, frames[0])
	rpc1, _ := decodeWardenFrame(t, frames[1])
	if rpc0 != reconcileCmdStop || rpc1 != reconcileCmdStart {
		t.Errorf("frames = %s,%s, want stop then start", rpc0, rpc1)
	}
	w, _ = api.dal.GetOutsourceWorker(workerID)
	if w.StoppedSince <= 0 {
		t.Fatal("the collect must latch stopped_since (the once-only marker)")
	}
	if w.RefocusSince <= 0 {
		t.Fatal("refocus_since must stay set until the fresh boot's loop-break")
	}
}

// TestRefocusWorker_Rejects: the online-only + stopped + unknown/released gates.
// Mutants: (a) dropping the online gate → the offline case 200s; (b) dropping the
// stopped gate → the stopped case 200s; each hand-verified red.
func TestRefocusWorker_Rejects(t *testing.T) {
	// (a) an ACTIVE worker with NO live SSE (offline) → 409 online-only, and it
	// must NOT stamp / dispatch anything (the positive control is the test above).
	t.Run("offline is 409, no stamp, no dispatch", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		offlineID := newActiveWorker(t, api, false) // active but no worker SSE
		rec := postWorker(t, api, offlineID, "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
		if rec.Code != http.StatusConflict {
			t.Fatalf("offline refocus: want 409, got %d %s", rec.Code, rec.Body.String())
		}
		if w, _ := api.dal.GetOutsourceWorker(offlineID); w.RefocusSince != 0 {
			t.Fatal("a rejected refocus must not stamp refocus_since")
		}
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("a rejected refocus must dispatch nothing, got %d frames", got)
		}
	})

	// (b) a stopped worker → 409 (restart first).
	t.Run("stopped is 409", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		id := newActiveOnlineWorker(t, api)
		w, _ := api.dal.GetOutsourceWorker(id)
		w.DesiredState = DesiredStateOffline
		_ = api.dal.PutOutsourceWorker(*w)
		rec := postWorker(t, api, id, "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
		if rec.Code != http.StatusConflict {
			t.Fatalf("stopped refocus: want 409, got %d %s", rec.Code, rec.Body.String())
		}
	})

	// (c) unknown worker → 404.
	t.Run("unknown is 404", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		rec := postWorker(t, api, "ow-nope", "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown refocus: want 404, got %d", rec.Code)
		}
	})
}

// ── cost banking (T-ba6b — 外包複用正職 bankLiveCost 那套) ──────────────────────

// TestBankLiveCost: the ONE shared fold banks a live telemetry cost into the
// durable banked_cost of WHICHEVER kind the actor id resolves to, pops the
// live field exactly once, and — unlike the old member-only fold — leaves the
// live figure untouched for an id it cannot resolve (loss-free). Mutant:
// popping before resolving (the old shape) → the unknown-actor case loses the
// live cost → red.
func TestBankLiveCost(t *testing.T) {
	t.Run("worker cost banks and accumulates", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)
		api.telemetry.Set(workerID, map[string]any{"cost": 1.5})
		api.bankLiveCost(workerID)
		w, _ := api.dal.GetOutsourceWorker(workerID)
		if w.BankedCost != 1.5 {
			t.Fatalf("banked = %v, want 1.5", w.BankedCost)
		}
		if _, still := api.telemetry.Get(workerID)["cost"]; still {
			t.Fatal("bank must POP the live cost (exactly-once)")
		}
		// A second bank with no fresh cost is a no-op; a fresh session cost
		// ACCUMULATES (never resets).
		api.bankLiveCost(workerID)
		api.telemetry.Set(workerID, map[string]any{"cost": 2.0})
		api.bankLiveCost(workerID)
		if w, _ := api.dal.GetOutsourceWorker(workerID); w.BankedCost != 3.5 {
			t.Fatalf("banked after 2nd session = %v, want 3.5", w.BankedCost)
		}
	})

	t.Run("no live cost is a no-op", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)
		api.bankLiveCost(workerID)
		if w, _ := api.dal.GetOutsourceWorker(workerID); w.BankedCost != 0 {
			t.Fatalf("no-cost bank must stay 0, got %v", w.BankedCost)
		}
	})

	t.Run("unknown actor keeps the live figure", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.telemetry.Set("ghost-1", map[string]any{"cost": 4.0})
		api.bankLiveCost("ghost-1")
		if got, ok := api.telemetry.Get("ghost-1")["cost"].(float64); !ok || got != 4.0 {
			t.Fatalf("unresolvable actor must keep its live cost, got %v (%v)", got, ok)
		}
	})

	// P7d fold: a worker IS a member row now, so banked_cost lands on the same
	// row either way — the branch discriminator is the WIRE: the worker branch
	// writes through PutOutsourceWorker (delta rides the outsource_worker
	// projection), never putMember, so no member patch naming an ow- id fans.
	t.Run("outsource actor rides the worker branch, no member patch", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveWorker(t, api, false)
		l, err := api.hub.Connect(workerID, "")
		if err != nil {
			t.Fatalf("connect worker SSE: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(l) })
		api.telemetry.Set(workerID, map[string]any{"cost": 1.25})
		api.bankLiveCost(workerID)
		if w, _ := api.dal.GetOutsourceWorker(workerID); w == nil || w.BankedCost != 1.25 {
			t.Fatalf("worker banked = %+v, want 1.25", w)
		}
		for frame := l.pop(); frame != nil; frame = l.pop() {
			if strings.Contains(string(frame), `"topic":"member"`) {
				t.Fatalf("banking a worker's cost must never fan a member patch: %s", frame)
			}
		}
	})

	t.Run("member cost still banks through the same fold", func(t *testing.T) {
		api := newTasksTestServer(t)
		m := fullMember("m-bank")
		if err := api.dal.PutMember(m); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		prior := m.BankedCost // fullMember seeds a non-zero banked figure
		api.telemetry.Set("m-bank", map[string]any{"cost": 0.75})
		api.bankLiveCost("m-bank")
		if got, _ := api.dal.GetMember("m-bank"); got == nil || got.BankedCost != prior+0.75 {
			t.Fatalf("member bank = %+v, want banked %v", got, prior+0.75)
		}
	})
}

// TestRefocusWorker_BanksCostAcrossRespawn (owner DoD: 跨一次 respawn 後累計不歸零):
// the handover 收口 (stopped-report → collect → kill+respawn) banks the dying
// session's live cost, and a second full handover keeps accumulating. Since the
// graceful flush (T-ea82) the refocus POST itself banks nothing — the kill
// moved to the collect. Mutant: dropping the bankLiveCost call in
// respawnWorkerNow → banked stays 0 → red.
func TestRefocusWorker_BanksCostAcrossRespawn(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	api.telemetry.Set(workerID, map[string]any{"cost": 2.5})

	handover := func(label string) {
		rec := postWorker(t, api, workerID, "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s refocus: %d %s", label, rec.Code, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		api.HandleReportStoppedApiSelfStoppedPost(rec,
			taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s report_stopped: %d %s", label, rec.Code, rec.Body.String())
		}
	}

	handover("1st")
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.BankedCost != 2.5 {
		t.Fatalf("banked_cost = %v, want 2.5 (the pre-handover spend)", w.BankedCost)
	}
	if _, still := api.telemetry.Get(workerID)["cost"]; still {
		t.Fatal("live cost must be popped after banking")
	}

	// The fresh session reports its own cost; a second handover ACCUMULATES.
	api.telemetry.Set(workerID, map[string]any{"cost": 1.0})
	handover("2nd")
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.BankedCost != 3.5 {
		t.Fatalf("banked_cost after 2nd handover = %v, want 3.5 (never reset)", w.BankedCost)
	}

	// Honest-null: a worker that never banked serves null, not 0.
	freshID := newActiveOnlineWorker(t, api)
	rows := listWorkersAs(t, api, wireOwnerID)
	for _, row := range rows {
		if row.ID == freshID && row.BankedCost != nil {
			t.Fatalf("never-banked worker must serve null banked_cost, got %v", *row.BankedCost)
		}
	}
}

// ── stop (停止) / restart (重啟) ────────────────────────────────────────────────

// TestStopWorker_KillsAndHoldsDown: stop stamps stopped_since, clears any
// in-flight refocus, kills the session, and does NOT re-dispatch — the worker
// projects spawn_state "stopped". Mutant: if stop called respawnWorkerNow (a
// stray re-spawn) a worker_start frame would appear → the "no worker_start"
// assertion goes red.
func TestStopWorker_KillsAndHoldsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	// A prior in-flight refocus that the stop must supersede.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 900
	_ = api.dal.PutOutsourceWorker(*w)

	rec := postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}
	w, _ = api.dal.GetOutsourceWorker(workerID)
	if w.DesiredState != DesiredStateOffline {
		t.Fatal("stop must set desired_state offline")
	}
	if w.RefocusSince != 0 {
		t.Fatal("stop must clear any in-flight refocus")
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("stop must kill exactly the session (1 worker_stop), got %d", len(frames))
	}
	if rpc, _ := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop {
		t.Errorf("stop frame = %s, want worker_stop", rpc)
	}
	// presence through the DTO: offline-intent + still-online session reads "stopping".
	dto := newOutsourceWorkerDTO(*w, nil, outsourceWorkerProjection{now: nowSecs(), online: true})
	if dto.Presence != "stopping" {
		t.Errorf("stopped worker presence = %q, want stopping (still online, winding down)", dto.Presence)
	}
}

// TestStopWorker_Idempotent: a second stop is a clean no-op — desired_state stays
// offline and it still returns 200 (never a 409 / never toggles back online).
// Mutant: making stop toggle desired_state instead of setting offline → the
// second stop flips it back online (red).
func TestStopWorker_Idempotent(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	rec := postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-stop must be a clean 200, got %d %s", rec.Code, rec.Body.String())
	}
	if again, _ := api.dal.GetOutsourceWorker(workerID); again.DesiredState != DesiredStateOffline {
		t.Fatalf("re-stop must keep desired_state offline, got %q", again.DesiredState)
	}
}

// TestRestartWorker_ClearsAndRedispatches: restart on a stopped worker sets
// desired_state back online and re-dispatches (worker_start). A non-stopped
// worker → 409. Mutant: dropping the "not stopped → 409" guard → the non-stopped
// case 200s (a hidden double-spawn) → red.
func TestRestartWorker_ClearsAndRedispatches(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// Not stopped → 409.
	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("restart of a live worker: want 409, got %d %s", rec.Code, rec.Body.String())
	}

	// Stop it, then restart → 200, marker cleared, worker_start dispatched.
	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	api.hub.DrainWardenCommands(ServerSelfHost)
	rec = postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.DesiredState != DesiredStateOnline {
		t.Fatal("restart must set desired_state back online")
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	sawStart := false
	for _, f := range frames {
		if rpc, _ := decodeWardenFrame(t, f); rpc == reconcileCmdStart {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatalf("restart must re-dispatch a worker_start, got %d frames", len(frames))
	}
}

// TestStoppedWorker_TickNeverRevives (the team-lead warning): once stopped, the
// scheduler tick must NOT revive the worker — neither the assigned recover+respawn
// nor the active auto-handover branch. Mutant: dropping the `desired_state ==
// offline` guard in the tick's assigned branch → a worker_start reappears (red).
func TestStoppedWorker_TickNeverRevives(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true

	// An ASSIGNED, stopped worker, past the stuck threshold (would normally be
	// recovered+respawned). Seed an eligible+online warden so that WITHOUT the
	// stopped guard the tick WOULD dispatch — that is what makes the assertion bite.
	seedLiveWorkerEnv(t, api)
	assignedID := assignOneWorker(t, api)
	aw, _ := api.dal.GetOutsourceWorker(assignedID)
	aw.CreatedTS = nowSecs() - (WakingTTLSecs + 100)
	aw.DesiredState = DesiredStateOffline
	_ = api.dal.PutOutsourceWorker(*aw)
	api.workerSpawnTarget[assignedID] = ServerSelfHost
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.runOutsourceTick(nowSecs())
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a stopped assigned worker must not be revived by the tick, got %d frames", got)
	}

	// An ACTIVE, stopped worker whose session is gone (offline) must likewise
	// stay down — the A案 P6 active-offline FSM rescue must not override the
	// owner-explicit hold (mutant: dropping the desired-offline guard on the
	// tick's active rescue arm → a start reappears, red).
	activeID := newActiveWorker(t, api, false)
	aw2, _ := api.dal.GetOutsourceWorker(activeID)
	aw2.DesiredState = DesiredStateOffline
	_ = api.dal.PutOutsourceWorker(*aw2)
	api.hub.DrainWardenCommands(ServerSelfHost)
	api.runOutsourceTick(nowSecs())
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a stopped active worker must not be revived by the tick, got %d frames", got)
	}
}

// TestActiveOfflineWorker_TickRescues (A案 P6): an ACTIVE worker whose session
// DIED (no live SSE, no in-flight handover, not owner-stopped) is rescued by
// the tick's shared-FSM arm — a fresh start is dispatched instead of the old
// spawn_state=stuck latch waiting for a manual restart.
func TestActiveOfflineWorker_TickRescues(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, session gone
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.runOutsourceTick(nowSecs())

	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 rescue start for the died active worker, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want start %s", rpc, args, workerID)
	}
}

// ── model (換 model) ───────────────────────────────────────────────────────────

// TestSetWorkerModel_ActiveRespawns: 換 model on an active+online worker persists
// the model+effort and kills+respawns so it takes effect NOW. Mutant: dropping
// the respawn on the active branch → 0 frames (red).
func TestSetWorkerModel_ActiveRespawns(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8", "effort": "high"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.Model != "claude-opus-4-8" || w.Effort != "high" {
		t.Fatalf("model/effort not persisted: %q/%q", w.Model, w.Effort)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
		t.Fatalf("active model change must kill+respawn (2 frames), got %d", got)
	}
}

// TestSetWorkerModel_AssignedPersistsOnly: 換 model on an ASSIGNED (not-yet-live)
// worker persists the model but does NOT respawn — it takes effect at the next
// spawn. Mutant: respawning on the assigned branch → a worker_start appears (red).
func TestSetWorkerModel_AssignedPersistsOnly(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedLiveWorkerEnv(t, api)           // eligible warden: a mutant that respawns WOULD dispatch
	workerID := assignOneWorker(t, api) // stays 'assigned'
	api.workerSpawnTarget[workerID] = ServerSelfHost
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.Model != "claude-opus-4-8" {
		t.Fatalf("assigned model not persisted: %q", w.Model)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("an assigned model change must not respawn, got %d frames", got)
	}
}

// ── context-high AUTO-handover (the ACTIVE-worker tick branch) ────────────────

// handoverGauge builds a gauge record at pct with a MATURE boot (no boot-storm)
// and a fresh pct report (passes the stale-guard).
func handoverGauge(now, pct float64) map[string]any {
	return map[string]any{
		"context_pct": pct, "context_pct_ts": now - 10, "boot_ts": now - 500,
	}
}

// TestAutoHandoverWorker_FixtureSpace: the trigger fixture space the team lead
// enumerated — pct at the HANDOVER line triggers; pct just below does not; a nil
// gauge does not; a boot-storm-fresh over-line boot does not; an offline worker
// does not. The positive control (the first case) proves the negatives are not
// vacuously green. Mutants hand-verified: (a) changing `!= levelHandover` to
// `== levelNone` makes the pct-49 case fire; (b) dropping the bootStormTripped
// guard makes the fresh-boot case fire; (c) dropping the IsOnline gate makes the
// offline case fire; (d) reading gauge existence instead of actionable pct makes
// the nil-gauge case fire.
func TestAutoHandoverWorker_FixtureSpace(t *testing.T) {
	const now = 100_000.0
	setup := func(t *testing.T) (*apiServer, string) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		return api, newActiveOnlineWorker(t, api)
	}
	handover := float64(newTasksTestServer(t).ctxHighConfig().HandoverPct) // 50

	triggered := func(t *testing.T, api *apiServer, workerID string) bool {
		w, _ := api.dal.GetOutsourceWorker(workerID)
		api.hub.DrainWardenCommands(ServerSelfHost)
		api.autoHandoverWorker(*w, now)
		fresh, _ := api.dal.GetOutsourceWorker(workerID)
		return fresh.RefocusSince == now
	}

	t.Run("pct at handover line triggers (positive control)", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, handoverGauge(now, handover))
		if !triggered(t, api, id) {
			t.Fatal("pct == HandoverPct must auto-refocus (stamp + grace open)")
		}
		// T-ea82 graceful flush: the auto-stamp opens the grace window — it
		// must NOT kill/respawn synchronously any more.
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("the auto-stamp must dispatch nothing (grace open), got %d frames", got)
		}
	})
	t.Run("pct just below handover does not", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, handoverGauge(now, handover-1))
		if triggered(t, api, id) {
			t.Fatal("pct below the handover line must not auto-refocus")
		}
	})
	t.Run("nil gauge does not", func(t *testing.T) {
		api, id := setup(t) // never Set a gauge entry
		if triggered(t, api, id) {
			t.Fatal("a worker with no gauge (nil pct) must never auto-refocus")
		}
	})
	t.Run("boot-storm fresh boot does not", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, map[string]any{
			"context_pct": handover + 40, "context_pct_ts": now - 1, "boot_ts": now - 10,
		})
		if triggered(t, api, id) {
			t.Fatal("a fresh over-line boot must be suppressed (boot-storm loop-guard)")
		}
	})
	t.Run("offline worker does not", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		id := newActiveWorker(t, api, false) // active but no worker SSE → offline
		api.gauge.Set(id, handoverGauge(now, handover))
		if triggered(t, api, id) {
			t.Fatal("an offline worker must never auto-refocus")
		}
	})
}

// TestAutoHandoverWorker_StoppedNotInspected: a stopped ACTIVE worker over the
// handover line is never auto-refocused (autoHandoverWorker's row-reread guard).
// Mutant: dropping the `desired_state == offline` guard in autoHandoverWorker →
// it fires (red).
func TestAutoHandoverWorker_StoppedNotInspected(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	_ = api.dal.PutOutsourceWorker(*w)
	api.gauge.Set(workerID, handoverGauge(now, float64(api.ctxHighConfig().HandoverPct)))
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.runOutsourceTick(now)
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a stopped worker must not be auto-handed-over, got %d frames", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.RefocusSince != 0 {
		t.Fatal("a stopped worker must not be stamped for handover")
	}
}

// TestCollectWorkerHandover_NoKillTarget_RollsBackEpochForFSMRescue (T-ea82,
// review B1): a mid-grace worker whose session is GONE with an unresolvable
// kill target (spawn memory lost to a server restart, SSE never coming back)
// must roll the WHOLE handover epoch back: nothing dispatched, the
// stopped_since latch cleared (else the next tick spawns without a kill — the
// O-28 double-active), AND refocus_since cleared — a kept refocus would mask
// the tick's FSM rescue while the collect can never find a target, the B1
// livelock. Mutant: keeping refocus on the dead-session defer → the
// RefocusSince assertion goes red.
func TestCollectWorkerHandover_NoKillTarget_RollsBackEpochForFSMRescue(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // no SSE — died mid-grace
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = now - 10 // grace window open, deadline NOT yet passed
	_ = api.dal.PutOutsourceWorker(*w)
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.outsourceMu.Lock()
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch
	w, _ = api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now)
	api.outsourceMu.Unlock()

	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a deferred collect must dispatch nothing, got %d frames", got)
	}
	fresh, _ := api.dal.GetOutsourceWorker(workerID)
	if fresh.StoppedSince != 0 {
		t.Fatal("a deferred collect must roll the stopped_since latch back (else the " +
			"next tick spawns without a kill)")
	}
	if fresh.RefocusSince != 0 {
		t.Fatalf("a dead-session defer must clear refocus_since (B1: a kept stamp masks "+
			"the FSM rescue forever), got %v", fresh.RefocusSince)
	}
}

// TestMidGraceRestartAmnesia_Converges (T-ea82, review B1 probe): the exact
// wedge shape the reviewer ran — refocus stamped, session dead, spawn memory
// lost to a server re-exec — must CONVERGE through the real tick: the first
// tick's deferred collect rolls the epoch back, and a following tick's FSM
// rescue re-spawns the worker. Mutant: reverting the dead-session defer to a
// latch-only rollback → 20 ticks past the deadline dispatch nothing (red).
func TestMidGraceRestartAmnesia_Converges(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, session dead (no SSE)
	stampWorkerRefocus(t, api, workerID, now)
	api.outsourceMu.Lock()
	delete(api.workerSpawnTarget, workerID) // server re-exec amnesia
	api.outsourceMu.Unlock()
	api.hub.DrainWardenCommands(ServerSelfHost)

	dispatched := false
	for i := 0; i < 20 && !dispatched; i++ {
		api.runOutsourceTick(now + StoppingTimeoutSecs + float64(i))
		for _, frame := range api.hub.DrainWardenCommands(ServerSelfHost) {
			if rpc, _ := decodeWardenFrame(t, frame); rpc == reconcileCmdStart {
				dispatched = true
			}
		}
	}
	if !dispatched {
		fresh, _ := api.dal.GetOutsourceWorker(workerID)
		t.Fatalf("wedged: 20 ticks past the grace deadline dispatched no START "+
			"(refocus_since=%v stopped_since=%v) — the collect defers forever and the "+
			"FSM rescue stays masked", fresh.RefocusSince, fresh.StoppedSince)
	}
}

// TestAutoHandoverWorker_LoopBreak: a worker already handing over (refocus_since
// set) is skipped as the cooldown, then cleared once a fresh session boots after
// the stamp (gauge boot_ts > refocus_since). Mutant: clearing on ANY boot_ts
// (dropping the `> refocus_since` compare) would clear prematurely on the OLD
// session's boot_ts → the "still set on old boot_ts" assertion goes red.
func TestAutoHandoverWorker_LoopBreak(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = now - 100 // handover in flight
	_ = api.dal.PutOutsourceWorker(*w)

	// (a) still the OLD session (boot_ts before the stamp) → marker stays set.
	api.gauge.Set(workerID, map[string]any{"boot_ts": now - 200})
	w, _ = api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now)
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.RefocusSince == 0 {
		t.Fatal("marker must stay set while only the OLD session's boot_ts is present")
	}

	// (b) a FRESH session booted after the stamp → the loop-break clears the
	// refocus marker AND the wind-down anchors (T-ea82 — a stale stopped_since
	// latch bleeding into the next epoch would skip that epoch's grace).
	w, _ = api.dal.GetOutsourceWorker(workerID)
	w.StoppingSince = now - 80
	w.StoppedSince = now - 60
	_ = api.dal.PutOutsourceWorker(*w)
	api.gauge.Set(workerID, map[string]any{"boot_ts": now - 50})
	w, _ = api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now)
	fresh, _ := api.dal.GetOutsourceWorker(workerID)
	if fresh.RefocusSince != 0 {
		t.Fatal("a session booted after the stamp must clear refocus_since (loop-break)")
	}
	if fresh.StoppingSince != 0 || fresh.StoppedSince != 0 {
		t.Fatalf("the loop-break must clear the wind-down anchors too, got stopping=%v stopped=%v",
			fresh.StoppingSince, fresh.StoppedSince)
	}
}

// ── graceful flush 收口 (T-ea82 — grace timeout / offline fallback / race) ────

// stampWorkerRefocus opens a handover epoch directly on the row (the stamp the
// refocus handler / auto-stamp would have written) without any dispatch.
func stampWorkerRefocus(t *testing.T, api *apiServer, workerID string, since float64) {
	t.Helper()
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = since
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stamp refocus: %v", err)
	}
}

// TestAutoHandoverWorker_GraceTimeout_ForceCollects (T-ea82 form ②): a worker
// that never reports stopped is force-collected once StoppingTimeoutSecs pass
// — and NOT one tick earlier. Mutant: dropping the deadline compare in the
// in-flight arm → the at-deadline case dispatches nothing (red).
func TestAutoHandoverWorker_GraceTimeout_ForceCollects(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now)
	api.hub.DrainWardenCommands(ServerSelfHost)

	// Inside the window: wait, dispatch nothing, latch nothing.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now+StoppingTimeoutSecs-1)
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("inside the grace window the tick must wait, got %d frames", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != 0 {
		t.Fatal("inside the grace window nothing may latch stopped_since")
	}

	// At the deadline: force-collect (stop+start) + latch.
	w, _ = api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now+StoppingTimeoutSecs)
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("the grace deadline must force-collect (stop+start), got %d frames", len(frames))
	}
	rpc0, _ := decodeWardenFrame(t, frames[0])
	rpc1, _ := decodeWardenFrame(t, frames[1])
	if rpc0 != reconcileCmdStop || rpc1 != reconcileCmdStart {
		t.Errorf("frames = %s,%s, want stop then start", rpc0, rpc1)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince <= 0 {
		t.Fatal("the force-collect must latch stopped_since")
	}
}

// TestAutoHandoverWorker_GraceOffline_CollectsImmediately (T-ea82 form ③ / D6):
// a mid-grace worker whose session DIED (SSE gone) is collected on the next
// tick — nothing left can flush, so waiting out the deadline is pure waste.
// Mutant: dropping the offline check in the in-flight arm → 0 frames until the
// deadline (red).
func TestAutoHandoverWorker_GraceOffline_CollectsImmediately(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, no SSE
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)

	w, _ := api.dal.GetOutsourceWorker(workerID)
	api.autoHandoverWorker(*w, now) // deadline is ~110s away
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("an offline mid-grace worker must collect NOW (stop+start), got %d frames",
			len(frames))
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince <= 0 {
		t.Fatal("the offline collect must latch stopped_since")
	}
}

// TestOpenWorkerHandoverGrace_OfflineFallsBackToImmediateKill (T-ea82 / D6): a
// refocus stamped against a worker with NO live session skips the grace window
// entirely at the stamp site — the 預告 has no audience — and takes the legacy
// immediate kill+respawn. Mutant: dropping the IsOnline gate in
// openWorkerHandoverGrace → 0 frames (red).
func TestOpenWorkerHandoverGrace_OfflineFallsBackToImmediateKill(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, no SSE
	stampWorkerRefocus(t, api, workerID, now)
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.outsourceMu.Lock()
	w, _ := api.dal.GetOutsourceWorker(workerID)
	api.openWorkerHandoverGrace(*w, triggerServer)
	api.outsourceMu.Unlock()

	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("an offline grace open must fall back to kill+respawn, got %d frames",
			len(frames))
	}
}

// TestWorkerHandoverCollect_OnceOnly (T-ea82 form ④ / D4): the two 收口 drivers
// racing each other never double-collect — whichever latches stopped_since
// first wins, the loser is a no-op. Mutant: dropping the stopped_since<=0
// guard on either driver → a second stop+start pair fans (red).
func TestWorkerHandoverCollect_OnceOnly(t *testing.T) {
	t.Run("stopped-report first, then the timeout tick", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)
		since := nowSecs() - 10
		stampWorkerRefocus(t, api, workerID, since)
		api.hub.DrainWardenCommands(ServerSelfHost)

		rec := httptest.NewRecorder()
		api.HandleReportStoppedApiSelfStoppedPost(rec,
			taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
		}
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
			t.Fatalf("the stopped-report must collect once (stop+start), got %d frames", got)
		}

		// The grace deadline fires right after — it must see the latch and stand
		// down: no second KILL may fan (a paced re-dispatch heal of the START is
		// legitimate phase-B behavior, a stop is the double-collect signature).
		w, _ := api.dal.GetOutsourceWorker(workerID)
		api.autoHandoverWorker(*w, since+StoppingTimeoutSecs+1)
		for _, frame := range api.hub.DrainWardenCommands(ServerSelfHost) {
			if rpc, _ := decodeWardenFrame(t, frame); rpc == reconcileCmdStop {
				t.Fatalf("a collected handover must not re-collect on timeout (second stop): %s", frame)
			}
		}
	})

	t.Run("timeout first, then a late stopped-report", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		workerID := newActiveOnlineWorker(t, api)
		since := nowSecs() - StoppingTimeoutSecs - 1
		stampWorkerRefocus(t, api, workerID, since)
		api.hub.DrainWardenCommands(ServerSelfHost)

		w, _ := api.dal.GetOutsourceWorker(workerID)
		api.autoHandoverWorker(*w, nowSecs())
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
			t.Fatalf("the deadline must force-collect (stop+start), got %d frames", got)
		}
		latched, _ := api.dal.GetOutsourceWorker(workerID)
		if latched.StoppedSince <= 0 {
			t.Fatal("the force-collect must latch stopped_since")
		}

		rec := httptest.NewRecorder()
		api.HandleReportStoppedApiSelfStoppedPost(rec,
			taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("late report_stopped: %d %s", rec.Code, rec.Body.String())
		}
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("a late stopped-report must not re-collect, got %d frames", got)
		}
		if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != latched.StoppedSince {
			t.Fatal("a late stopped-report must never re-stamp the latch")
		}
	})
}

// TestPutOutsourceWorker_KeepsWindDownAnchors (T-ea82 form ⑤ / D5): the
// stopping/stopped anchors a self-report stamped survive ANY later
// PutOutsourceWorker round-trip — the workerFromMember/memberFromWorker
// mappings carry them symmetrically, so a tick's unrelated row write can never
// zero a mid-handover anchor. Mutant: dropping either field from either
// mapping → red.
func TestPutOutsourceWorker_KeepsWindDownAnchors(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, nowSecs())

	rec := httptest.NewRecorder()
	api.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}
	before, _ := api.dal.GetOutsourceWorker(workerID)
	if before.StoppingSince <= 0 {
		t.Fatal("report_stopping must stamp the worker's stopping_since")
	}

	// Any unrelated read-modify-write of the worker row (the tick shape).
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.Effort = "high"
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.StoppingSince != before.StoppingSince {
		t.Fatalf("PutOutsourceWorker clobbered stopping_since: %v → %v",
			before.StoppingSince, after.StoppingSince)
	}

	// And the member-row view agrees (the mapping is symmetric, not lossy).
	m, _ := api.dal.GetMember(workerID)
	if m == nil || m.StoppingSince != before.StoppingSince {
		t.Fatalf("member row lost the anchor: %+v", m)
	}
}
