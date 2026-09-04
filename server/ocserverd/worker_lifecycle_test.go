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

// TestWorkerPresence_StopIntent: an owner-stopped worker projects the member
// exit vocabulary regardless of its assigned/active lifecycle status —
// "stopping" while the session still winds down (SSE alive), "stopped" once it
// is gone — never a fake-green latch. A RELEASED row still projects ""
// (off-panel). Mutant: dropping the StopIntent fold in PresenceState turns the
// stopped rows back to online/waking → red.
//
// T-14 moved which COLUMN carries that intent. It used to be desired_state, read
// by a projection only workers called; it is now stopping_since, read by the
// projection BOTH kinds call — a staff row can be desired-offline with no anchor
// (the never-woken seed) and must stay 「離線」, so the anchor is the only test
// that answers both kinds correctly. The rows below therefore carry the anchor
// the two owner stop verbs stamp: both 停止 and 強制停止 write stopping_since
// through stopEpochAnchor BEFORE they write the offline intent, so a stopped
// worker never has one without the other.
func TestWorkerPresence_StopIntent(t *testing.T) {
	const now = 1_000_000.0
	cases := []struct {
		name     string
		status   string
		desired  string
		stopping float64
		online   bool
		want     string
	}{
		{"active+online but stopped is stopping", WorkerStatusActive, DesiredStateOffline, now - 3, true, "stopping"},
		{"assigned but stopped is stopped", WorkerStatusAssigned, DesiredStateOffline, now - 3, false, "stopped"},
		// The self-driven arm (report_stopping stamps the anchor and touches no
		// intent) — staff parity, and it is what the desired_state test used to
		// miss on this side.
		{"self-reported stopping while online is stopping", WorkerStatusActive, DesiredStateOnline, now - 3, true, "stopping"},
		{"active+online with online-intent is online", WorkerStatusActive, DesiredStateOnline, 0, true, "online"},
		{"released even if stopped is blank", WorkerStatusReleased, DesiredStateOffline, now - 3, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := OutsourceWorker{ID: "ow-1", Status: c.status, TaskID: "t-1",
				CreatedTS: now - 5, DesiredState: c.desired, StoppingSince: c.stopping}
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
		DesiredState: DesiredStateOnline,
		// Placement is now an explicit machine id (owner ruling 2026-07-25) — pin
		// it to the warden this fixture already seeds+connects online.
		DesiredMachineID: ServerSelfHost}
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
	// T-72dd: the stopped-report LATCHES; the collect is the shared FSM's, one
	// tick later. One decider, one kill — see workerReportStopped's own note.
	workerTickPass(t, api, workerID, nowSecs())
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("the first stopped-report must collect (stop+start), got %d frames", len(frames))
	}
	rpc0, _ := decodeWardenFrame(t, frames[0].Frame)
	rpc1, _ := decodeWardenFrame(t, frames[1].Frame)
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
	// T-72dd: and once-only is asserted DIRECTLY too — the next tick, with the
	// epoch still open and the latch still set, must not collect again. That is
	// the property the respawn's clobbering of the FSM stop anchor used to break.
	workerTickPass(t, api, workerID, nowSecs())
	if got := countStops(t, api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the collect must be once-only, got %d further stop(s)", got)
	}
}

// TestRefocusWorker_Rejects: the online-only + never-stopped + unknown/released
// gates. Mutants: (a) dropping the online gate → the offline case 200s;
// (b) dropping the aStopWasEverAskedFor gate → case (b) 200s; each hand-verified
// red.
//
// ⚠️ CASE (b) NO LONGER MEANS WHAT ITS NAME SAYS (T-65 包②). 「stopped」 stopped
// being a refusal: a worker whose stop is in flight — or has landed — now gets a
// 200 and a QUEUED 起來 (restart_after_stop), by owner ruling 2026-08-30. What
// case (b) actually pins is the NARROWER survivor: its fixture reaches
// desired_state=offline by writing the field directly, so it never acquires a
// stopping_since anchor, and a worker nobody ever asked to stop still has no
// 下線 for an 上線 rule to be added to. Read it as 「never-stopped is 409」. The
// 200 side lives in outsource_restart_after_stop_t65_test.go.
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

	// (b) a worker that is desired-offline but was NEVER asked to stop (no
	// stopping_since anchor — see the ⚠️ on this function) → 409. The mutation
	// this kills is dropping aStopWasEverAskedFor from queueWorkerRestartAfterStop,
	// which would boot a worker that has never started.
	t.Run("never-stopped offline is 409", func(t *testing.T) {
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
	// row either way — both branches call AddMemberBankedCost. The branch
	// discriminator is the WIRE: the worker branch fans nothing (its changes
	// ride the outsource_worker projection), so no member patch naming an ow-
	// id ever goes out.
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
		l, err := api.hub.Connect("m-bank", "")
		if err != nil {
			t.Fatalf("connect member SSE: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(l) })
		api.telemetry.Set("m-bank", map[string]any{"cost": 0.75})
		api.bankLiveCost("m-bank")
		if got, _ := api.dal.GetMember("m-bank"); got == nil || got.BankedCost != prior+0.75 {
			t.Fatalf("member bank = %+v, want banked %v", got, prior+0.75)
		}
		// The push is the half a single-column migration silently drops: the
		// member branch stopped writing the whole row (T-14 項目 6), and the
		// delta the whole-row write used to fan must survive that — the
		// wind-down / recycle hooks key on a member delta naming self, and this
		// fold runs on the last-disconnect edge.
		fanned := false
		for frame := l.pop(); frame != nil; frame = l.pop() {
			if strings.Contains(string(frame), `"topic":"member"`) &&
				strings.Contains(string(frame), "m-bank") {
				fanned = true
			}
		}
		if !fanned {
			t.Fatal("banking a member's cost must still fan its member delta")
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
		// T-72dd: the report latches; the FSM collects (and banks) next tick.
		workerTickPass(t, api, workerID, nowSecs())
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

// TestForceStopWorker_KillsAndHoldsDown: 強制停止 stamps the forced anchors,
// clears any in-flight refocus, kills the session, and does NOT re-dispatch —
// the worker projects presence "stopping" while its SSE is still up. Mutant: if
// it called respawnWorkerNow (a stray re-spawn) a start frame would appear.
//
// 🔴 THIS USED TO BE TestStopWorker_KillsAndHoldsDown, verbatim, against
// /stop. It was re-aimed rather than deleted when 停止 became a graceful
// close-out (T-ed79, owner 「強制殺移到第三顆按鈕」): the behaviour it pins did not
// go away, it moved to a different button, and a deleted test would have left
// the third rung with no proof it kills at all.
func TestForceStopWorker_KillsAndHoldsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	// A prior in-flight refocus that the force-stop must supersede.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 900
	_ = api.dal.PutOutsourceWorker(*w)
	seedWorkerAnchors(t, api, *w)

	rec := postWorker(t, api, workerID, "force-stop", nil,
		api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("force-stop: %d %s", rec.Code, rec.Body.String())
	}
	w, _ = api.dal.GetOutsourceWorker(workerID)
	if w.DesiredState != DesiredStateOffline {
		t.Fatal("force-stop must set desired_state offline")
	}
	if w.RefocusSince != 0 {
		t.Fatal("force-stop must clear any in-flight refocus")
	}
	if w.ForcedStopAt <= 0 || !forcedEpochLive(memberFromWorker(*w)) {
		t.Fatalf("force-stop must leave a LIVE forced epoch — that record is what "+
			"keeps the notice silent: %+v", w)
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("force-stop must kill exactly the session (1 stop), got %d", len(frames))
	}
	if rpc, _ := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop {
		t.Errorf("kill frame = %s, want %s", rpc, reconcileCmdStop)
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
// desired_state back online and re-dispatches (worker_start).
//
// ⚠️ IT USED TO OPEN BY ASSERTING A 409 ON A LIVE WORKER. T-ed79 #10 removed
// that refusal (owner 2026-08-21 「往正職靠：外包也不擋」), so the assertion was
// deleted rather than inverted — the live case is now its own subject, with its
// three faces, in worker_restart_no_guard_ted79_test.go. What is left here is
// what this test was always really about: the STOPPED→restart path.
// (The neighbouring half — a worker whose session died on its own must not be
// refused — is TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn below.)
func TestRestartWorker_ClearsAndRedispatches(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// Stop it, then restart → 200, marker cleared, worker_start dispatched.
	// 強制停止 rather than 停止: this case wants the session GONE before the
	// restart, and 停止 now only asks for a close-out.
	postWorker(t, api, workerID, "force-stop", nil,
		api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost)
	api.hub.DrainWardenCommands(ServerSelfHost)
	rec := postWorker(t, api, workerID, "restart", nil,
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
		if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStart {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatalf("restart must re-dispatch a worker_start, got %d frames", len(frames))
	}
}

// TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn (T-7526): the guard
// used to read `desired_state != offline → 409`, i.e. it asked "did anyone press
// STOP?" (INTENT) when the question is "is it actually alive?" (LIVENESS). A
// worker whose session died on its own still carries desired_state=online, so
// the one endpoint that could bring it back answered 409 — and the panel, whose
// restart affordance keys off presence, offered 停止 instead of 重新啟動. The
// owner had no way to revive it at all.
func TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	// online=false: claimed then died — desired_state stays online, no live SSE.
	workerID := newActiveWorker(t, api, false)
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.DesiredState != DesiredStateOnline {
		t.Fatalf("fixture must keep desired_state online (nobody pressed stop), got %q",
			w.DesiredState)
	}
	if api.hub.IsOnline(workerID) {
		t.Fatal("fixture must have NO live session")
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restarting a dead-session worker: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	sawStart := false
	for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
		if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStart {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatal("reviving a dead-session worker must re-dispatch a worker_start")
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
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want start %s", rpc, args, workerID)
	}
}

// ── model (換 model) ───────────────────────────────────────────────────────────

// TestSetWorkerModel_ActiveWindsDownThenRespawns: 換 model on an active+online
// worker persists the model+effort and — since T-98f4 rule 2 — opens the SAME
// graceful wind-down 換手 has, instead of killing the session outright. The
// respawn (carrying the new model) is the stopped-report's job.
//
// CONTRACT CHANGE, recorded: this test used to assert 2 frames (stop+start) at
// the handler. Owner 2026-07-27:「我建議所有換手都可以給他機會收尾」 — the old
// shape threw away whatever the session had not written down yet.
func TestSetWorkerModel_ActiveWindsDownThenRespawns(t *testing.T) {
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
	if w.RefocusSince <= 0 {
		t.Fatal("an active model change must open a wind-down (refocus_since stamped)")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the wind-down must dispatch no kill yet, got %d frames", got)
	}

	// The worker finishes its SOP and says so — the respawn is immediate, and it
	// carries the NEW model. Since T-55 the row is written AFTER the window
	// opens (the launch-intent setters run past respawnWorkerForOwnerOp), which
	// this arm does not care about: the collect happens on a later request, long
	// past both writes.
	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	// T-72dd: the stopped-report LATCHES; the collect is the shared FSM's, one
	// tick later. One decider, one kill — see workerReportStopped's own note.
	workerTickPass(t, api, workerID, nowSecs())
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("the stopped-report must collect (stop+start), got %d frames", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[1].Frame)
	if rpc != reconcileCmdStart || args["model"] != "claude-opus-4-8" {
		t.Fatalf("respawn frame = %s %v, want a start carrying the new model", rpc, args)
	}
}

// TestSetWorkerModel_ImmediateRespawnCarriesTheNewModel covers the OTHER arm of
// respawnWorkerForOwnerOp — the one that dispatches a START synchronously, from
// inside the handler, INSTEAD of opening a wind-down. It is reached when the
// epoch has already been collected (stopped_since latched) while the dying
// session still reads as online, which is the window
// TestOwnerOp_VerbAfterTheCollectIsNotSwallowed opens.
//
// 🔴 WHY IT HAS TO EXIST SINCE T-55: on that arm the frame goes out BEFORE the
// setters store the values, so the START can only be right because
// respawnWorkerForOwnerOp takes the worker BY VALUE and nothing under it
// re-reads the row for the launch spec. That was an incidental property before;
// it is load-bearing now. Mutant: make notifyWorkerSpawn (or anything below it)
// build the frame from a fresh GetOutsourceWorker and this test goes red with
// the OLD values — which is exactly what the owner would get in production, on a
// 200, with no receipt.
//
// ALL THREE launch intents are sent and asserted, not just the model: the same
// window carries runtime and effort through the same by-value chain, and a
// single-field pin would leave the other two riding on nothing. runtime is the
// one that hurts most when it slips, because it has a SECOND consumer the frame
// column does not show — buildWorkerBootContext feeds w.Runtime to
// workerBootSequence, so a stale runtime boots the worker on the OTHER runtime's
// 啟動步驟 document. That failure is invisible from every direction: 200, no
// receipt, nothing red, and the row afterwards holds the NEW value.
//
// The sibling above covers the wind-down arm, where the row is written long
// before the collect dispatches. Neither one covers the other.
func TestSetWorkerModel_ImmediateRespawnCarriesTheNewModel(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	// The warden must be able to take a CODEX worker, or step 3's runtime change
	// would be refused at placement and dispatch nothing — a green that proves
	// the opposite of what this test is for.
	if rec := doIngestTelemetry(api, ServerSelfHost, ServerSelfHost, bothRuntimes); rec.Code != 200 {
		t.Fatalf("fixture: telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}

	// 1) The first 換 model opens a wind-down and dispatches nothing.
	setWorkerModelBody(t, api, workerID, map[string]any{"model": "claude-opus-4-8"})

	// 2) The worker answers; the shared FSM collects on the next tick. The latch
	//    lands while the old session is still online — the window this arm lives in.
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	workerTickPass(t, api, workerID, nowSecs())
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("fixture: the collect must have latched stopped_since, or this test " +
			"is walking the wind-down arm the sibling already covers")
	}
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the dying session must still look online for the immediate arm")
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	// 3) The owner changes all three launch intents at once. This one dispatches NOW.
	setWorkerModelBody(t, api, workerID, map[string]any{
		"model": "claude-opus-4-9", "runtime": RuntimeCodex, "effort": "high"})

	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	var starts int
	for _, f := range frames {
		rpc, args := decodeWardenFrame(t, f.Frame)
		if rpc != reconcileCmdStart {
			continue
		}
		starts++
		// Errorf, not Fatalf: the three are independent facts about one frame, and
		// a re-read stales all three at once. Failing fast would show only the
		// first and hide that the other two are equally unpinned.
		//
		// Three calls rather than a table: a {field, value} slice literal reads to
		// lint-effort-vocab as a hand-written effort vocabulary and it correctly
		// reports the list as incomplete. Silencing that with a SKIP_FILES line
		// would blind this whole file to the guard for the sake of one row.
		assertArg := func(field, want string) {
			t.Helper()
			if args[field] != want {
				t.Errorf("the synchronous respawn dispatched %s %v, want %q — "+
					"the frame is built from the VALUE the handler holds, and the launch-intent "+
					"setters run AFTER it (T-55). Something under notifyWorkerSpawn re-read the "+
					"row, which still carries the previous value at that instant.",
					field, args[field], want)
			}
		}
		assertArg("model", "claude-opus-4-9")
		assertArg("runtime", RuntimeCodex)
		assertArg("effort", "high")
	}
	if starts != 1 {
		t.Fatalf("a 換 model landing after the collect must dispatch exactly one START "+
			"now, got %d (0 means it opened a second wind-down instead and this test "+
			"is no longer covering the immediate arm)", starts)
	}
	// …and the row caught up on all three, so the two writes did not disagree.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.Model != "claude-opus-4-9" || w.Runtime != RuntimeCodex || w.Effort != "high" {
		t.Fatalf("row = model %q / runtime %q / effort %q, want claude-opus-4-9 / %s / high",
			w.Model, w.Runtime, w.Effort, RuntimeCodex)
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

	notice := float64(newTasksTestServer(t).ctxHighConfig().NoticePct)

	// openedOp reports WHICH wind-down this tick opened ("" = none). It used to
	// be a bool, because the worker path had exactly ONE threshold. It has two
	// now (T-72dd: workers go through the staff pass), and the distinction is the
	// whole point — the first opens a clockless 停止, the second the 加速停止.
	openedOp := func(t *testing.T, api *apiServer, workerID string) string {
		w, _ := api.dal.GetOutsourceWorker(workerID)
		api.hub.DrainWardenCommands(ServerSelfHost)
		workerTickPass(t, api, w.ID, now)
		fresh, _ := api.dal.GetOutsourceWorker(workerID)
		if fresh.RefocusSince != now {
			return ""
		}
		return fresh.RefocusOp
	}
	triggered := func(t *testing.T, api *apiServer, workerID string) bool {
		return openedOp(t, api, workerID) != ""
	}

	t.Run("pct at handover line triggers (positive control)", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, handoverGauge(now, handover))
		if op := openedOp(t, api, id); op != refocusOpContextHigh {
			t.Fatalf("pct == HandoverPct must open the SECOND threshold "+
				"(context_high — the 加速停止), got %q", op)
		}
		// T-ea82 graceful flush: the auto-stamp opens the grace window — it
		// must NOT kill/respawn synchronously any more.
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("the auto-stamp must dispatch nothing (grace open), got %d frames", got)
		}
	})
	// 🔴 THIS CASE INVERTED, and the inversion is the parity fix (T-72dd).
	// "Below the handover line ⇒ nothing happens" was true only while the worker
	// path had ONE threshold. Staff have had two since T-ed79, and a worker now
	// goes through the same pass: below handover_pct but at or above notice_pct
	// opens the FIRST threshold — a clockless 停止 (context_notice), which is the
	// half the worker copy never had. It is not a kill and not a countdown; it is
	// the agent being ASKED to wind down while there is still room.
	t.Run("pct just below handover opens the FIRST threshold, not nothing", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, handoverGauge(now, handover-1))
		op := openedOp(t, api, id)
		if op != refocusOpContextNotice {
			t.Fatalf("between the two thresholds must open context_notice, got %q", op)
		}
		if _, clocked := winddownKindFor(op); clocked {
			t.Fatal("the first threshold must run NO clock")
		}
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("the soft threshold must dispatch nothing, got %d frames", got)
		}
	})
	// …and BELOW the first threshold still nothing happens at all — without this
	// the case above would pass for a pass that stamps everything.
	t.Run("pct below the first threshold does nothing", func(t *testing.T) {
		api, id := setup(t)
		api.gauge.Set(id, handoverGauge(now, notice-1))
		if op := openedOp(t, api, id); op != "" {
			t.Fatalf("below notice_pct nothing may be opened, got %q", op)
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
	seedWorkerAnchors(t, api, *w)
	api.hub.DrainWardenCommands(ServerSelfHost)

	api.outsourceMu.Lock()
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch
	api.outsourceMu.Unlock()
	w, _ = api.dal.GetOutsourceWorker(workerID)
	// Two ticks: the first only ARMS the continuous-offline anchor (T-ed79 #13 —
	// one offline sample no longer collects anything), the second is past the
	// confirm window and is the one this test is about.
	workerTickPass(t, api, w.ID, now)
	w, _ = api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now+workerOfflineConfirmGraceSecs)

	// Drained ONCE — both assertions below read this same batch (draining twice
	// would let the first read swallow the evidence for the second).
	dispatched := api.hub.DrainWardenCommands(ServerSelfHost)
	if got := countStops(t, dispatched); got != 0 {
		t.Fatalf("there is no live session to kill — got %d stop(s)", got)
	}
	fresh, _ := api.dal.GetOutsourceWorker(workerID)
	if fresh.StoppedSince != 0 {
		t.Fatalf("nothing was collected, so no close-out may be latched (got %v)",
			fresh.StoppedSince)
	}
	// 🔴 THE B1 LIVELOCK CANNOT HAPPEN ANY MORE, so this test no longer demands
	// the epoch be torn down to avoid it (T-72dd).
	//
	// B1 was: the collect waits for a kill target, the target waits for a
	// respawn, and the respawn is masked by `RefocusSince == 0` — so a kept stamp
	// froze the worker forever, and rolling the epoch back was the escape. That
	// mask is GONE: the tick now runs the FSM for an ACTIVE worker whatever its
	// epoch says, so the rescue is reachable with the stamp still in place. The
	// epoch ends the ordinary way instead — the respawn boots and the loop-break
	// clears it.
	//
	// What must still be true is the thing B1 was really about: the worker is
	// NOT stuck. Assert that directly.
	sawStart := false
	for _, f := range dispatched {
		if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStart {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatal("the FSM rescue must be REACHABLE with the epoch still stamped — " +
			"that reachability is what retired the B1 rollback")
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
			if rpc, _ := decodeWardenFrame(t, frame.Frame); rpc == reconcileCmdStart {
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
	seedWorkerAnchors(t, api, *w)

	// (a) still the OLD session (boot_ts before the stamp) → marker stays set.
	api.gauge.Set(workerID, map[string]any{"boot_ts": now - 200})
	w, _ = api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now)
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
	seedWorkerAnchors(t, api, *w)
	api.gauge.Set(workerID, map[string]any{"boot_ts": now - 50})
	w, _ = api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now)
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
// It stamps the ACCELERATED cause (the second context threshold) because every
// caller of this helper means to exercise the CLOCKED arm of autoHandoverWorker.
// It used to leave RefocusOp EMPTY for that purpose — empty was clocked by
// fallthrough — which stopped being true when T-ed79 made FINAL the positive
// condition and left exactly one clocked cause. Naming it is also closer to
// production: no path stamps an epoch without an op. Do not "fix" this by
// stamping one of the 停止 causes here; it would move a dozen tests onto the
// no-clock branch silently, and that branch is pinned separately in
// worker_refocus_no_clock_tfe5e_test.go.
func stampWorkerRefocus(t *testing.T, api *apiServer, workerID string, since float64) {
	t.Helper()
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = since
	w.RefocusOp = refocusOpContextHigh
	w.StoppingSince = 0.0
	w.StoppedSince = 0.0
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stamp refocus: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
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
	workerTickPass(t, api, w.ID, now+StoppingTimeoutSecs-1)
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("inside the grace window the tick must wait, got %d frames", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != 0 {
		t.Fatal("inside the grace window nothing may latch stopped_since")
	}

	// At the deadline: force-collect (stop+start) + latch.
	w, _ = api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now+StoppingTimeoutSecs)
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("the grace deadline must force-collect (stop+start), got %d frames", len(frames))
	}
	rpc0, _ := decodeWardenFrame(t, frames[0].Frame)
	rpc1, _ := decodeWardenFrame(t, frames[1].Frame)
	if rpc0 != reconcileCmdStop || rpc1 != reconcileCmdStart {
		t.Errorf("frames = %s,%s, want stop then start", rpc0, rpc1)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince <= 0 {
		t.Fatal("the force-collect must latch stopped_since")
	}
	// T-72dd: once-only asserted directly — the next tick must not re-collect.
	workerTickPass(t, api, workerID, now+StoppingTimeoutSecs+1)
	if got := countStops(t, api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the collect must not repeat on the next tick, got %d stop(s)", got)
	}
}

// TestAutoHandoverWorker_GraceOffline_CollectsOnceConfirmed (T-ea82 form ③ / D6,
// re-aimed by T-ed79 #13): a mid-grace worker whose session is GONE is collected
// well before the grace deadline — nothing left can flush, so waiting out the
// deadline is pure waste. What changed is only WHEN "gone" becomes a fact: since
// owner 2026-08-21 (rc-7df3deb21b3b) it takes a full workerOfflineConfirmGraceSecs
// of continuous offline instead of one instantaneous sample. The confirmation
// boundary now coincides with the current 120s lifecycle window; at that point
// a gone session is restarted rather than force-collected, so this remains the
// D6 form and not the grace-timeout one.
// The three faces of the window itself (inside / lapsed / cancelled by a
// reconnect) are pinned in worker_offline_confirm_ted79_test.go.
// Mutant: dropping the offline check in the in-flight arm → 0 frames until the
// deadline (red).
func TestAutoHandoverWorker_GraceOffline_CollectsOnceConfirmed(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, no SSE
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)

	w, _ := api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now) // arms the continuous-offline anchor
	w, _ = api.dal.GetOutsourceWorker(workerID)
	workerTickPass(t, api, w.ID, now+workerOfflineConfirmGraceSecs)
	// 🔴 WHAT A GONE SESSION GETS IS A RESPAWN, NOT A KILL (T-72dd). The collect
	// is the shared FSM's now, and decideUp's recycle arm requires an ONLINE
	// session — correctly, because there is nothing to kill here: the session is
	// gone. So the FSM re-STARTs the worker and nothing is "collected" at all.
	// Waiting out the deadline is still avoided (the D6 point this test was
	// written for), and no close-out is fabricated for a session that never
	// reported one.
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if got := countStops(t, frames); got != 0 {
		t.Fatalf("there is no live session to kill — a gone worker must only be "+
			"respawned, got %d stop(s)", got)
	}
	if len(frames) != 1 {
		t.Fatalf("a confirmed-gone mid-grace worker must be re-STARTed, got %d frames",
			len(frames))
	}
	if rpc, _ := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart {
		t.Fatalf("frame = %s, want start", rpc)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince > 0 {
		t.Fatalf("nothing was collected, so no close-out may be latched (got %v)",
			fresh.StoppedSince)
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
		// T-72dd: the report latches; the FSM collects on the next tick.
		workerTickPass(t, api, workerID, nowSecs())
		if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
			t.Fatalf("the stopped-report must collect once (stop+start), got %d frames", got)
		}

		// The next tick, INSIDE stop_retry, must stand down: the collect already
		// went out and this is the same epoch, so a second kill here would be the
		// double-collect signature.
		w, _ := api.dal.GetOutsourceWorker(workerID)
		workerTickPass(t, api, w.ID, nowSecs())
		if got := countStops(t, api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
			t.Fatalf("a collected handover must not re-collect inside stop_retry, "+
				"got %d stop(s)", got)
		}

		// 🔴 PAST stop_retry, WITH THE SESSION STILL ONLINE, a second STOP is
		// CORRECT and this test no longer forbids it (T-72dd). The collect is the
		// shared FSM's now, and that arm is at-least-once by design: a worker that
		// is still online long past the retry window is one whose STOP did not
		// land, and re-sending it is the heal. The old assertion could forbid this
		// because the old collector never retried a lost kill at all — it switched
		// to re-dispatching the START and left a live session on a closed-out
		// worker forever, which is the wedge T-ed79 kept finding.
		//
		// (The fixture keeps the worker "online" because nothing here simulates
		// the warden actually killing it; in production the session drops and the
		// recycle arm is not reached at all.)
		w, _ = api.dal.GetOutsourceWorker(workerID)
		workerTickPass(t, api, w.ID, nowSecs()+StoppingTimeoutSecs+1)
		if got := countStops(t, api.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
			t.Fatalf("a STOP that never landed must be re-dispatched past stop_retry, "+
				"got %d stop(s)", got)
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
		workerTickPass(t, api, w.ID, nowSecs())
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

	// Any unrelated read-modify-write of the worker row (the tick shape). The
	// unrelated field has to be one the whole-row write still CARRIES, and the
	// answer keeps moving: effort left PutMember's DO UPDATE SET in T-55's first
	// batch and last_op followed it in the second, so each of them in turn would
	// have made this an upsert that changes nothing at all — passing while no
	// longer standing for the thing the test is named after. last_machine_id is
	// the current choice, and the re-read below is what will catch the day it
	// stops being carried too. If you are here because that assertion fired, the
	// fix is to pick another CARRIED column, never to delete the check.
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.LastMachineID = "m-tick"
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	if reread, _ := api.dal.GetOutsourceWorker(workerID); reread == nil ||
		reread.LastMachineID != "m-tick" {
		t.Fatalf("the unrelated write must actually land, else this test asserts "+
			"nothing: %+v", reread)
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
