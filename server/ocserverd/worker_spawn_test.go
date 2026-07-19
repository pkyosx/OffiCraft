package main

// worker_spawn_test.go — the Phase 6 wake/reclaim lifecycle: boot-context
// assembly, warden targeting, fail-closed dispatch, pacing, and the reclaim
// hook + backstop. Everything runs against the real DAL/hub with zero network.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newWorkerTestServer builds an apiServer with the out-of-box roster seeded
// (Mira + the server-self warden). The worker_context.md seed is read
// embed-only (from the staged seedsdist baked into the test binary), so no
// on-disk seeds/ is staged — a disk copy would be ignored anyway (T-e731).
func newWorkerTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "worker-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), []byte("worker-test-secret"), 3600,
		assetRoot(t.TempDir()))
}

func putWorkerFixture(t *testing.T, s *apiServer, w OutsourceWorker) OutsourceWorker {
	t.Helper()
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	return w
}

func putTaskFixture(t *testing.T, s *apiServer, task Task) Task {
	t.Helper()
	if task.Inputs == nil {
		task.Inputs = map[string]any{}
	}
	if err := s.dal.PutTask(task); err != nil {
		t.Fatalf("put task: %v", err)
	}
	return task
}

// connectWarden puts wardenID online on the hub (a live SSE downstream).
func connectWarden(t *testing.T, s *apiServer, wardenID string) {
	t.Helper()
	l, err := s.hub.Connect(wardenID, "")
	if err != nil {
		t.Fatalf("hub connect %s: %v", wardenID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
}

// decodeWardenFrame unwraps one FIFO frame ("data: {...}\n\n") into rpc + args.
func decodeWardenFrame(t *testing.T, frame []byte) (string, map[string]any) {
	t.Helper()
	raw := strings.TrimSuffix(strings.TrimPrefix(string(frame), "data: "), "\n\n")
	var env struct {
		Topic string `json:"topic"`
		Data  struct {
			RPC  string         `json:"rpc"`
			Args map[string]any `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("frame decode: %v (%s)", err, raw)
	}
	if env.Topic != wardenCommandTopic {
		t.Fatalf("frame topic = %q, want %q", env.Topic, wardenCommandTopic)
	}
	return env.Data.RPC, env.Data.Args
}

// ── boot context ─────────────────────────────────────────────────────────────

func TestBuildWorkerBootContext_FullAssembly(t *testing.T) {
	s := newWorkerTestServer(t)
	w := OutsourceWorker{ID: "ow-abc", Codename: "O-7", Model: "opus", Effort: "high"}
	task := Task{
		ID: "t-1234567890ab", TypeKey: "review-pr", Title: "Review PR 42",
		DedupeKey: "https://pr/42", Description: "把 42 號 PR 看完",
		Priority: TaskPriorityHigh,
		Inputs:   map[string]any{"pr_url": "https://pr/42", "repo": "x/y"},
	}
	manual := &TaskManual{
		TypeKey: "review-pr", DisplayName: "審查 PR",
		Purpose:   "review 一個 PR",
		Fields:    `[{"name":"pr_url","required":true,"is_key":true}]`,
		SopMD:     "先看 diff 再留結論",
		Learnings: "大 PR 先分檔看",
	}
	got, err := s.buildWorkerBootContext(w, task, manual)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if strings.Contains(got, ownerPlaceholder) {
		t.Errorf("the worker_context.md seed must have its %s placeholder substituted", ownerPlaceholder)
	}
	for _, want := range []string{
		"外包工作者 boot context",            // the embedded seed leads
		"ow-abc", "O-7", "opus", "high", // identity block
		TaskNo(task.ID), "Review PR 42", "review-pr", "https://pr/42",
		"把 42 號 PR 看完", "pr_url", // task block
		"review 一個 PR", "先看 diff 再留結論", "大 PR 先分檔看", // manual Q1/Q3/learnings
		"必填、識別鍵", // Q2 field annotations
		// T-fa76: the display face leads, the ADDRESSING type_key stays in
		// parentheses (the worker calls write_task_learnings by key).
		"類型：審查 PR（review-pr）",
		"# 任務手冊：審查 PR（review-pr）",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("boot context missing %q", want)
		}
	}
}

// T-ba04: a worker minted onto a task that is in `reassigning` gets a TAKEOVER
// section in its boot context — who its predecessor is (id) + the "hand over
// first, THEN flip the status yourself" protocol. A non-reassigning task must
// NOT carry that section (a fresh assignment has no predecessor). RED/GREEN pin
// for the boot-context handover fold.
func TestBuildWorkerBootContext_ReassigningTakeoverSection(t *testing.T) {
	s := newWorkerTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "m-pred", Name: "Ken", Kind: KindAssistant, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put predecessor: %v", err)
	}
	w := OutsourceWorker{ID: "ow-new", Codename: "O-2", Model: "opus", Effort: "high"}
	base := Task{ID: "t-aabbccddeeff", TypeKey: "x", Title: "接手任務", Priority: TaskPriorityMid}

	// Fresh (not reassigning) → no takeover section.
	fresh, err := s.buildWorkerBootContext(w, base, nil)
	if err != nil {
		t.Fatalf("fold fresh: %v", err)
	}
	// "接手序列" is dynamic-only (the seed cross-references the header phrase but
	// never emits this line) — a clean marker for the takeover block's presence.
	if strings.Contains(fresh, "接手序列") {
		t.Error("a non-reassigning task must NOT carry the takeover section")
	}

	// Reassigning with a stamped member predecessor → the section names it.
	takeover := base
	takeover.Lock = TaskLockReassigning
	takeover.ReassignedFrom = "m-pred"
	takeover.ReassignedFromKind = TaskExecutorMember
	got, err := s.buildWorkerBootContext(w, takeover, nil)
	if err != nil {
		t.Fatalf("fold takeover: %v", err)
	}
	for _, want := range []string{
		"接手序列",       // the takeover block (dynamic-only)
		"前任：Ken",     // predecessor resolved to its member name
		"m-pred",     // predecessor chat id
		"claim_task", // the takeover is the claim action now (T-9ca5)
	} {
		if !strings.Contains(got, want) {
			t.Errorf("takeover boot context missing %q", want)
		}
	}
}

func TestBuildWorkerBootContext_MissingManualIsHonest(t *testing.T) {
	s := newWorkerTestServer(t)
	got, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-1", Codename: "S-1", Model: "sonnet", Effort: "medium"},
		Task{ID: "t-aabbccddeeff", TypeKey: "gone-type", Title: "x", Priority: TaskPriorityMid},
		nil)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !strings.Contains(got, "手冊目前不存在") {
		t.Error("nil manual must be stated honestly, not silently omitted")
	}
}

// ── warden targeting ─────────────────────────────────────────────────────────

// putWardenFixture registers one more active warden (= machine) on the roster.
func putWardenFixture(t *testing.T, s *apiServer, id string) {
	t.Helper()
	if err := s.dal.PutMember(Member{
		ID: id, Name: id + " box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put warden %s: %v", id, err)
	}
}

// connectAgentOn projects an agent session onto a machine (a live SSE whose
// token carries machineID as its claim) — the agentLoadOn input.
func connectAgentOn(t *testing.T, s *apiServer, agentID, machineID string) {
	t.Helper()
	l, err := s.hub.Connect(agentID, machineID)
	if err != nil {
		t.Fatalf("hub connect %s@%s: %v", agentID, machineID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
}

// pickWarden calls the placement picker with a throwaway worker + wall clock —
// the existing placement tests below predate the T-9ccf cooldown/health params
// and only exercise the online/idlest/preference logic.
func pickWarden(s *apiServer, pref string) string {
	return s.pickWorkerWarden(OutsourceWorker{ID: "ow-pick"}, pref, nowSecs())
}

func TestPickWorkerWarden_AutoTieKeepsPriorPrecedence(t *testing.T) {
	s := newWorkerTestServer(t)
	// Nothing online → "" (fail-closed).
	if got := pickWarden(s, "auto"); got != "" {
		t.Fatalf("no online warden: got %q, want \"\"", got)
	}
	// Another active warden online → picked.
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, "m-other")
	if got := pickWarden(s, "auto"); got != "m-other" {
		t.Fatalf("other warden online: got %q, want m-other", got)
	}
	// Server-self online too, both idle (load tie) → server-self keeps the
	// pre-machine-preference precedence.
	connectWarden(t, s, ServerSelfHost)
	if got := pickWarden(s, "auto"); got != ServerSelfHost {
		t.Fatalf("server-self online: got %q, want %s", got, ServerSelfHost)
	}
	// An absent preference means "auto" (spec: absent = "auto").
	if got := pickWarden(s, ""); got != ServerSelfHost {
		t.Fatalf("empty preference: got %q, want %s", got, ServerSelfHost)
	}
}

func TestPickWorkerWarden_AutoPicksIdlestMachine(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	// Server-self hosts one live agent session; m-other hosts none → "auto"
	// must leave the precedence order and pick the IDLEST machine.
	connectAgentOn(t, s, "mira", ServerSelfHost)
	if got := pickWarden(s, "auto"); got != "m-other" {
		t.Fatalf("auto with loaded server-self: got %q, want m-other (idlest)", got)
	}
	// Load up m-other past server-self → the pick follows the counts back.
	connectAgentOn(t, s, "m-agent-b", "m-other")
	connectAgentOn(t, s, "m-agent-c", "m-other")
	if got := pickWarden(s, "auto"); got != ServerSelfHost {
		t.Fatalf("auto with loaded m-other: got %q, want %s", got, ServerSelfHost)
	}
}

func TestPickWorkerWarden_SpecifiedMachineOnline_Honoured(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	// m-other is BUSIER than server-self — an explicit machine id must still
	// win over the idlest-machine logic while it is online.
	connectAgentOn(t, s, "mira", "m-other")
	if got := pickWarden(s, "m-other"); got != "m-other" {
		t.Fatalf("specified online machine: got %q, want m-other", got)
	}
}

func TestPickWorkerWarden_SpecifiedMachineOffline_FallsBackToAuto(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	putWardenFixture(t, s, "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-third")
	// m-other is on the roster but OFFLINE → the spec-promised fallback to
	// "auto": idlest online machine (m-third idle beats loaded server-self).
	connectAgentOn(t, s, "mira", ServerSelfHost)
	if got := pickWarden(s, "m-other"); got != "m-third" {
		t.Fatalf("offline specified machine: got %q, want m-third (auto fallback)", got)
	}
	// And with NOTHING online at all it stays fail-closed.
	s2 := newWorkerTestServer(t)
	putWardenFixture(t, s2, "m-other")
	if got := pickWarden(s2, "m-other"); got != "" {
		t.Fatalf("offline specified machine, none online: got %q, want \"\"", got)
	}
}

// TestPickWorkerWarden_CooledMachineSkipped (T-9ccf DoD② 換機): a machine benched
// for a worker (a boot failure within the cooldown window) is skipped so the
// pick rotates to a healthy alternative; when EVERY online warden is benched the
// pick honestly returns "" (wait, do not hammer a known-bad host).
func TestPickWorkerWarden_CooledMachineSkipped(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	now := 1_000_000.0
	w := OutsourceWorker{ID: "ow-cool"}

	// server-self is idlest → normally picked. Bench it for this worker → the
	// pick must rotate to m-other.
	s.outsourceMu.Lock()
	s.benchWorkerMachine(w.ID, ServerSelfHost, now)
	got := s.pickWorkerWarden(w, "auto", now)
	s.outsourceMu.Unlock()
	if got != "m-other" {
		t.Fatalf("benched server-self must rotate to m-other, got %q", got)
	}

	// Bench m-other too → nothing eligible → "" (honest wait, no re-pick).
	s.outsourceMu.Lock()
	s.benchWorkerMachine(w.ID, "m-other", now)
	got = s.pickWorkerWarden(w, "auto", now)
	s.outsourceMu.Unlock()
	if got != "" {
		t.Fatalf("all online wardens benched must yield \"\" (wait), got %q", got)
	}

	// After the cooldown window elapses, the machines are eligible again.
	s.outsourceMu.Lock()
	got = s.pickWorkerWarden(w, "auto", now+workerSpawnCooldownSecs+1)
	s.outsourceMu.Unlock()
	if got == "" {
		t.Fatalf("expired cooldown must re-admit a machine, got \"\"")
	}
}

// TestPickWorkerWarden_DeprioritizesUnhealthyHost (T-9ccf DoD②, RAM+帳號判準): a
// warden whose claude sub is unreadable (creds broken) or whose ram_pct is over
// the ceiling is chosen only when nothing healthier is online. Here the idlest
// host has broken creds, so the busier-but-healthy host wins; an UNKNOWN probe
// (no telemetry) never deprioritizes.
func TestPickWorkerWarden_DeprioritizesUnhealthyHost(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-bad")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-bad")
	now := 1_000_000.0
	w := OutsourceWorker{ID: "ow-health"}

	// m-bad is idlest (no agents) but its claude sub is UNREADABLE; server-self
	// hosts a live agent (busier) but has no adverse probe → healthy wins.
	connectAgentOn(t, s, "mira", ServerSelfHost)
	s.telemetry.Set("m-bad", map[string]any{
		"claude": map[string]any{"sub_readable": false},
		"ts":     now,
	})
	s.outsourceMu.Lock()
	got := s.pickWorkerWarden(w, "auto", now)
	s.outsourceMu.Unlock()
	if got != ServerSelfHost {
		t.Fatalf("creds-broken idlest host must lose to a healthy busier host, got %q", got)
	}

	// A RAM-exhausted host is likewise deprioritized.
	s2 := newWorkerTestServer(t)
	putWardenFixture(t, s2, "m-ram")
	connectWarden(t, s2, ServerSelfHost)
	connectWarden(t, s2, "m-ram")
	connectAgentOn(t, s2, "mira", ServerSelfHost)
	s2.telemetry.Set("m-ram", map[string]any{
		"hardware": map[string]any{"ram_pct": workerPlacementRamCeiling + 5},
		"ts":       now,
	})
	s2.outsourceMu.Lock()
	got = s2.pickWorkerWarden(w, "auto", now)
	s2.outsourceMu.Unlock()
	if got != ServerSelfHost {
		t.Fatalf("ram-exhausted idlest host must lose to a healthy busier host, got %q", got)
	}
}

// TestFoldWorkerCommandResult_RefusedStartBenchesTarget (T-9ccf DoD② 換機): a
// REFUSED start receipt (the converged member verb — P5b) benches the worker's
// last spawn target so the next pick rotates off it. A legacy worker_start
// receipt from an old warden benches too; a SUCCESSFUL receipt benches nothing.
func TestFoldWorkerCommandResult_RefusedStartBenchesTarget(t *testing.T) {
	s := newWorkerTestServer(t)
	now := nowSecs()
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-rf", Codename: "O-1", TaskID: "t-rf", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-rf"] = "m-bad" // in-memory spawn observation (P7d)
	s.foldWorkerCommandResult("ow-rf", map[string]any{
		"rpc": reconcileCmdStart, "ok": false, "reason": "session_already_exists",
	}, triggerServer)

	s.outsourceMu.Lock()
	cooling := s.workerMachineCoolingOn("ow-rf", "m-bad", nowSecs())
	s.outsourceMu.Unlock()
	if !cooling {
		t.Fatal("a refused start must bench its last spawn target")
	}

	// A refused LEGACY worker_start receipt (old warden, transition window)
	// still benches.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-lg", Codename: "O-3", TaskID: "t-lg", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-lg"] = "m-old"
	s.foldWorkerCommandResult("ow-lg", map[string]any{
		"rpc": legacyWardenCmdWorkerStart, "ok": false, "reason": "session_already_exists",
	}, triggerServer)
	s.outsourceMu.Lock()
	cooling = s.workerMachineCoolingOn("ow-lg", "m-old", nowSecs())
	s.outsourceMu.Unlock()
	if !cooling {
		t.Fatal("a refused legacy worker_start must bench its last spawn target")
	}

	// A successful start receipt benches nothing.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-ok", Codename: "O-2", TaskID: "t-ok", Status: WorkerStatusAssigned,
		CreatedTS: now,
	})
	s.workerSpawnTarget["ow-ok"] = "m-good"
	s.foldWorkerCommandResult("ow-ok", map[string]any{
		"rpc": reconcileCmdStart, "ok": true,
	}, triggerServer)
	s.outsourceMu.Lock()
	cooling = s.workerMachineCoolingOn("ow-ok", "m-good", nowSecs())
	s.outsourceMu.Unlock()
	if cooling {
		t.Fatal("a successful start must NOT bench its target")
	}
}

// ── wake dispatch ────────────────────────────────────────────────────────────

func TestNotifyWorkerSpawn_NoOnlineWarden_FailClosed(t *testing.T) {
	s := newWorkerTestServer(t)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000001", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-1",
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-1", Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	_, stamped := s.workerSpawnAt[w.ID]
	s.outsourceMu.Unlock()
	if stamped {
		t.Error("fail-closed dispatch must NOT stamp pacing (next tick must retry)")
	}
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("nothing may be enqueued with no online warden, got %d frames", len(got))
	}
}

func TestNotifyWorkerSpawn_DispatchesMemberStart_AndPaces(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000002", TypeKey: "review-pr", Title: "Review PR 42",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-2",
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-2", Codename: "O-2", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.notifyWorkerSpawn(w, nowSecs()) // paced: within the retry window → NOT re-enqueued
	s.outsourceMu.Unlock()

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want exactly 1 start (pacing), got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0])
	if rpc != reconcileCmdStart {
		t.Fatalf("rpc = %q, want start (P5b: the member verb)", rpc)
	}
	if args["member_id"] != "ow-2" || args["model"] != "opus" || args["effort"] != "high" {
		t.Errorf("args = %v", args)
	}
	if args["role"] != workerBootRoleLabel {
		t.Errorf("role = %v, want %q", args["role"], workerBootRoleLabel)
	}
	if args["session_name"] != "" {
		t.Errorf("session_name = %v, want \"\" (warden derives member-<ow-id>)", args["session_name"])
	}
	token, _ := args["member_token"].(string)
	if token == "" {
		t.Fatal("member_token missing from the start frame")
	}
	// The minted token's sub must be the worker id (server-mint, agent floor).
	if sub := jwtSubOf(t, token); sub != "ow-2" {
		t.Errorf("token sub = %q, want ow-2", sub)
	}
	// A案 P1: the token now burns the ACTUAL dispatch host as machine_id — here
	// server-self was the "auto" pick, so the resolved warden id (never a literal
	// "auto"/"") rides the claim, mirroring the member token.
	if mid := jwtMachineOf(t, token); mid != ServerSelfHost {
		t.Errorf("token machine_id = %q, want %s (the resolved auto pick)", mid, ServerSelfHost)
	}
	persona, _ := args["persona_context"].(string)
	if !strings.Contains(persona, "O-2") || !strings.Contains(persona, "Review PR 42") {
		t.Error("persona_context must carry the identity + task")
	}
	// The token must never leak into the persona text (file/env only).
	if strings.Contains(persona, token) {
		t.Error("worker token leaked into the persona context")
	}
	if s.workerSpawnTarget["ow-2"] != ServerSelfHost {
		t.Errorf("spawn target = %q, want %s", s.workerSpawnTarget["ow-2"], ServerSelfHost)
	}
}

// TestNotifyWorkerSpawn_StampsSpawnObservation: each dispatch must stamp the
// in-memory spawn observation (workerSpawnAttempts++/workerSpawnAt/
// workerSpawnTarget) the cockpit projection folds from. In-memory by design
// since the P7d fold (the former durable spawn columns were retired with the
// outsource_worker table) — a restart forgetting them is the accepted trade.
func TestNotifyWorkerSpawn_StampsSpawnObservation(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000009", TypeKey: "review-pr", Title: "Review",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-9",
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-9", Codename: "O-9", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if got := s.workerSpawnAttempts["ow-9"]; got != 1 {
		t.Fatalf("workerSpawnAttempts = %d, want 1", got)
	}
	if got, _ := s.workerSpawnObs("ow-9"); got != ServerSelfHost {
		t.Fatalf("workerSpawnTarget = %q, want %s", got, ServerSelfHost)
	}
	if s.workerSpawnAt["ow-9"] == 0 {
		t.Fatalf("workerSpawnAt must be stamped, got 0")
	}
	// A案 P6: a successful dispatch stamps the shared-FSM in-flight state so
	// reconcileWorkerLiveness never doubles (and never zombie-misreads) a start
	// it did not decide itself.
	st := s.workerReconcileStates["ow-9"]
	if st.LastCommand != reconcileCmdStart || st.Phase != reconcilePhaseStarting {
		t.Fatalf("FSM state after dispatch = %+v, want start/starting", st)
	}
}

func TestNotifyWorkerSpawn_HonoursManualMachinePreference(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000e", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-e",
	})
	// The manual pins the spawn to m-other — the frame must land on ITS FIFO
	// even though the tie-break order would otherwise pick server-self.
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"opus","machine":"m-other"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-e", Codename: "O-14", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})

	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()

	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("server-self must receive nothing, got %d frames", got)
	}
	frames := s.hub.DrainWardenCommands("m-other")
	if len(frames) != 1 {
		t.Fatalf("want 1 start on the preferred machine, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0])
	if rpc != reconcileCmdStart || args["member_id"] != "ow-e" {
		t.Errorf("frame = %s %v", rpc, args)
	}
	if s.workerSpawnTarget["ow-e"] != "m-other" {
		t.Errorf("spawn target = %q, want m-other", s.workerSpawnTarget["ow-e"])
	}
	// A案 P1: the machine_id claim must equal the pinned host it actually landed
	// on, not the literal preference string.
	token, _ := args["member_token"].(string)
	if mid := jwtMachineOf(t, token); mid != "m-other" {
		t.Errorf("token machine_id = %q, want m-other (the pinned dispatch host)", mid)
	}
}

// A案 P5a: worker_stop rides the same fail-closed reachability gate as member
// dispatch — an offline target warden gets nothing in its FIFO.
func TestEnqueueWorkerStop_OfflineTarget_FailClosed(t *testing.T) {
	s := newWorkerTestServer(t)
	s.outsourceMu.Lock()
	accepted := s.enqueueWorkerStop(ServerSelfHost, "ow-1")
	s.outsourceMu.Unlock()
	if accepted {
		t.Error("worker_stop toward an offline warden must report not enqueued")
	}
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("nothing may land in a dead warden's FIFO, got %d frames", len(got))
	}

	connectWarden(t, s, ServerSelfHost)
	s.outsourceMu.Lock()
	accepted = s.enqueueWorkerStop(ServerSelfHost, "ow-1")
	s.outsourceMu.Unlock()
	if !accepted {
		t.Fatal("worker_stop toward an online warden must enqueue")
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop frame, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-1" {
		t.Errorf("rpc = %q args = %v", rpc, args)
	}
}

// A案 P5a rework: an owner stop whose kill the gate refused (target warden
// unreachable) is PARKED and re-fired by the tick once the target reconnects —
// never silently lost (殘活 session 零容忍).
func TestStopWorkerNow_OfflineTarget_ParksKillAndTickRefires(t *testing.T) {
	s := newWorkerTestServer(t)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-s", Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: "t-s", Status: WorkerStatusAssigned,
		DesiredState: DesiredStateOffline, // owner-explicit stop intent
	})
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = ServerSelfHost // last session host, now offline
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("offline target must get nothing yet, got %d frames", len(got))
	}

	s.runOutsourceTick(nowSecs()) // target still offline — parked kill held
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("tick with the target still offline must enqueue nothing, got %d", len(got))
	}

	connectWarden(t, s, ServerSelfHost)
	s.runOutsourceTick(nowSecs())
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want the parked worker_stop re-fired on reconnect, got %d frames", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-s" {
		t.Errorf("rpc = %q args = %v", rpc, args)
	}
	s.outsourceMu.Lock()
	pending := s.workerStopPending[w.ID]
	s.outsourceMu.Unlock()
	if pending != "" {
		t.Errorf("parking must clear once the kill went out, still %q", pending)
	}
	// Once drained, later ticks owe nothing (one kill, no stop-spam).
	s.runOutsourceTick(nowSecs())
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("no further stops owed after the parked kill drained, got %d", len(got))
	}
}

// captureStderr runs fn with os.Stderr swapped onto a pipe and returns what it
// wrote — the outsourceLog sentinel assertions read it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// TestRespawnWorkerNow_SpawnMemoryEmpty_KillsViaSseMachineClaim: server-restart
// amnesia (workerSpawnTarget empty) must NOT skip the kill when the worker's
// live SSE machine claim still names the host — the stop dispatches there (the
// member relocation-STOP ground truth). Mutant: dropping the hub.MachineOf
// fallback in resolveWorkerKillTarget → no stop frame ever leaves and the old
// session survives the respawn (the O-28 double-active).
func TestRespawnWorkerNow_SpawnMemoryEmpty_KillsViaSseMachineClaim(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	// The worker's live SSE carries the machine claim of the host it runs on.
	if _, err := api.hub.Connect(workerID, ServerSelfHost); err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	api.outsourceMu.Lock()
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch
	w, _ := api.dal.GetOutsourceWorker(workerID)
	done := api.respawnWorkerNow(*w, "auto-handover")
	api.outsourceMu.Unlock()
	if !done {
		t.Fatal("a resolvable SSE machine claim must let the respawn proceed")
	}
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 2 {
		t.Fatalf("want stop+start on the claimed machine, got %d frames", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != workerID {
		t.Fatalf("first frame = %s %v, want stop %s", rpc, args, workerID)
	}
	if rpc, _ := decodeWardenFrame(t, frames[1]); rpc != reconcileCmdStart {
		t.Fatalf("second frame = %s, want the respawn start", rpc)
	}
}

// TestRespawnWorkerNow_NoKillTarget_DefersWholeCycle: spawn memory empty AND no
// live SSE claim ⇒ the respawn must defer wholesale — no stop, no respawn, a
// greppable sentinel — and report false so the caller rolls its stamp back.
// Mutant: restoring the old `if old != "" { kill }` skip-and-respawn shape →
// a start frame appears with no preceding stop (red on the frame count).
func TestRespawnWorkerNow_NoKillTarget_DefersWholeCycle(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // no worker SSE at all
	api.hub.DrainWardenCommands(ServerSelfHost)
	var done bool
	logged := captureStderr(t, func() {
		api.outsourceMu.Lock()
		delete(api.workerSpawnTarget, workerID)
		w, _ := api.dal.GetOutsourceWorker(workerID)
		done = api.respawnWorkerNow(*w, "auto-handover")
		api.outsourceMu.Unlock()
	})
	if done {
		t.Fatal("no kill target must report the respawn as deferred")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a deferred handover must dispatch nothing, got %d frames", got)
	}
	if !strings.Contains(logged, "auto-handover deferred "+workerID) ||
		!strings.Contains(logged, "no kill target (spawn memory empty, sse offline)") {
		t.Fatalf("sentinel log missing, got: %q", logged)
	}
}

// ── the shared-FSM spawn/rescue path (A案 P6 — retired recoverStuckWorker) ──

// fsmWorkerFixture seeds a worker + a live bound task + a manual so the FSM's
// START decisions can actually dispatch (notifyWorkerSpawn refuses a worker
// with no live task).
func fsmWorkerFixture(t *testing.T, s *apiServer, id string, status string, createdTS float64) OutsourceWorker {
	t.Helper()
	putTaskFixture(t, s, Task{
		ID: "t-" + id, TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: id,
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	return putWorkerFixture(t, s, OutsourceWorker{
		ID: id, Codename: "O-1", Model: "opus", Effort: "high",
		TaskID: "t-" + id, Status: status, CreatedTS: createdTS,
	})
}

// TestReconcileWorkerLiveness_ClobberedStartZombieTakeover: a START that
// bounced off the warden clobber-guard (receipt reason session_already_exists —
// the O-19 ghost wedge) makes the FSM dispatch a robust member `stop` to the
// last spawn target (reaping the ghost), bench that host (換機), clear the
// pacing, and NOT mark the worker reclaimed. A second tick inside stop_retry
// must not stop-spam.
func TestReconcileWorkerLiveness_ClobberedStartZombieTakeover(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-g", WorkerStatusAssigned, now-500)
	w.LastOp = reconcileCmdStart
	w.LastOpReason = "session_already_exists: live session refused clobber"
	putWorkerFixture(t, s, w)

	s.outsourceMu.Lock()
	s.workerSpawnAt["ow-g"] = now - 5 // recently paced (must not block the reap path)
	s.workerSpawnTarget["ow-g"] = ServerSelfHost
	s.workerReconcileStates["ow-g"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 10,
	}
	s.reconcileWorkerLiveness(w, now)
	_, stillPaced := s.workerSpawnAt["ow-g"]
	cooling := s.workerMachineCoolingOn("ow-g", ServerSelfHost, now)
	s.outsourceMu.Unlock()

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("zombie takeover must enqueue exactly 1 stop, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0])
	if rpc != reconcileCmdStop || args["member_id"] != "ow-g" {
		t.Fatalf("takeover frame = %s %v, want stop ow-g", rpc, args)
	}
	if stillPaced {
		t.Fatal("the takeover must CLEAR the pacing stamp so the respawn is not throttled")
	}
	if !cooling {
		t.Fatal("the ghost's host must be benched for this worker (換機)")
	}
	if s.workerReclaimed["ow-g"] {
		t.Fatal("a rescue must NOT set workerReclaimed (that flag is for released-worker reclaim only)")
	}

	// The next tick moves to the RESPAWN leg (never a second stop toward the
	// same ghost), and with the only online warden benched the respawn honestly
	// waits — nothing is dispatched at the wedged host.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, now+1)
	s.outsourceMu.Unlock()
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the benched host must receive nothing after the takeover, got %d", got)
	}
}

// TestReconcileWorkerLiveness_InFlightStartAwaitsPresence (positive control):
// a start dispatched within start_timeout is a boot in flight — the FSM must
// leave it strictly alone (no re-start, no stop). Killing a healthy slow-booter
// would be the exact failure the old one-shot guard avoided; the FSM must too.
func TestReconcileWorkerLiveness_InFlightStartAwaitsPresence(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-f", WorkerStatusAssigned, now-10)
	s.outsourceMu.Lock()
	s.workerSpawnAt["ow-f"] = now - 5
	s.workerSpawnTarget["ow-f"] = ServerSelfHost
	s.workerReconcileStates["ow-f"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 5,
	}
	s.reconcileWorkerLiveness(w, now)
	_, stillPaced := s.workerSpawnAt["ow-f"]
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("an in-flight boot must be left alone, got %d frames", len(got))
	}
	if !stillPaced {
		t.Fatal("an in-flight boot's pacing must be left intact")
	}
}

// TestReconcileWorkerLiveness_SilentTimeoutBacksOffThenRespawns: a start that
// times out silently (lost frame / dead boot, no receipt) folds into the FSM's
// backoff — no immediate re-dispatch — and once the backoff window lapses the
// next tick re-spawns fresh.
func TestReconcileWorkerLiveness_SilentTimeoutBacksOffThenRespawns(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-b", WorkerStatusAssigned, now-500)
	s.outsourceMu.Lock()
	s.workerReconcileStates["ow-b"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
		LastCommandAt: now - (s.reconcileCfg.StartTimeout + 1),
	}
	s.reconcileWorkerLiveness(w, now) // timeout folds → backoff armed, nothing sent
	st := s.workerReconcileStates["ow-b"]
	s.outsourceMu.Unlock()
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a just-timed-out start must back off, not instantly re-dispatch (got %d)", got)
	}
	if st.Phase != reconcilePhaseBackoff || st.Attempts != 1 {
		t.Fatalf("state after silent timeout = %+v, want backoff/attempts=1", st)
	}

	// Past the backoff window the FSM re-spawns.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, st.BackoffUntil+1)
	s.outsourceMu.Unlock()
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 start after the backoff lapsed, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-b" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}

// TestReconcileWorkerLiveness_LegacyWorkerStartReceiptStillDetectsZombie: a
// clobber receipt folded by an OLD warden build still carries the retired
// worker_start verb — the transition fold (canonicalWorkerLastOp) must keep the
// zombie takeover working across the version skew.
func TestReconcileWorkerLiveness_LegacyWorkerStartReceiptStillDetectsZombie(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-l", WorkerStatusAssigned, now-500)
	w.LastOp = legacyWardenCmdWorkerStart
	w.LastOpReason = "session_already_exists: live session refused clobber"
	putWorkerFixture(t, s, w)
	s.outsourceMu.Lock()
	s.workerSpawnTarget["ow-l"] = ServerSelfHost
	s.workerReconcileStates["ow-l"] = reconcileState{
		Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 10,
	}
	s.reconcileWorkerLiveness(w, now)
	s.outsourceMu.Unlock()
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("legacy-receipt zombie takeover must enqueue 1 stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-l" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}

func TestNotifyWorkerSpawn_TerminalTask_NoDispatch(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000003", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusTerminated, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-3", ClosedTS: 1,
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-3", Codename: "O-3", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
	s.outsourceMu.Lock()
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Errorf("a terminal task must never boot a worker, got %d frames", len(got))
	}
}

// jwtSubOf verifies the minted token against the test secret and returns sub.
func jwtSubOf(t *testing.T, token string) string {
	t.Helper()
	claims, err := verifyJWT(token, []byte("worker-test-secret"), nowSecsInt())
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	sub, _ := claims["sub"].(string)
	return sub
}

// jwtMachineOf verifies the minted token and returns its machine_id claim (""
// when the claim is absent) — the A案 P1 assertion that a worker token now
// carries its dispatch host, mirroring the member token.
func jwtMachineOf(t *testing.T, token string) string {
	t.Helper()
	claims, err := verifyJWT(token, []byte("worker-test-secret"), nowSecsInt())
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	machineID, _ := claims["machine_id"].(string)
	return machineID
}

func nowSecsInt() int64 { return int64(nowSecs()) }

// ── reclaim ──────────────────────────────────────────────────────────────────

func TestReclaimWorkerSession_RecordedTarget(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-4", Codename: "O-4", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.workerSpawnTarget["ow-4"] = ServerSelfHost
	s.reclaimWorkerSession(w)
	reclaimed := s.workerReclaimed["ow-4"]
	s.outsourceMu.Unlock()
	if !reclaimed {
		t.Error("reclaim must mark the worker once a frame went out")
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop, got %d", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0])
	if rpc != reconcileCmdStop || args["member_id"] != "ow-4" {
		t.Errorf("frame = %s %v, want worker_stop ow-4", rpc, args)
	}
}

func TestReclaimWorkerSession_NoTargetBroadcastsToOnlineWardens(t *testing.T) {
	s := newWorkerTestServer(t)
	if err := s.dal.PutMember(Member{
		ID: "m-other", Name: "other box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put member: %v", err)
	}
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-5", Codename: "O-5", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.reclaimWorkerSession(w) // no recorded target (restart amnesia)
	s.outsourceMu.Unlock()
	for _, warden := range []string{ServerSelfHost, "m-other"} {
		if got := len(s.hub.DrainWardenCommands(warden)); got != 1 {
			t.Errorf("warden %s: want 1 broadcast worker_stop, got %d", warden, got)
		}
	}
}

func TestReclaimWorkerSession_NoOnlineWarden_RetriesLater(t *testing.T) {
	s := newWorkerTestServer(t)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-6", Codename: "O-6", Model: "opus", Effort: "high",
		TaskID: "t-x", Status: WorkerStatusReleased, ReleasedTS: 1,
	})
	s.outsourceMu.Lock()
	s.reclaimWorkerSession(w)
	reclaimed := s.workerReclaimed["ow-6"]
	s.outsourceMu.Unlock()
	if reclaimed {
		t.Error("an undeliverable reclaim must NOT be marked done (backstop retries)")
	}
}

func TestDismissOutsourceWorkersForTask_ReleasesAndReclaims(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000007", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-7", ClosedTS: 1,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-7", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusActive,
	})

	s.dismissOutsourceWorkersForTask(task.ID, 42.0, triggerServer)

	after, err := s.dal.GetOutsourceWorker("ow-7")
	if err != nil || after == nil {
		t.Fatalf("read back worker: %v", err)
	}
	if after.Status != WorkerStatusReleased || after.ReleasedTS != 42.0 {
		t.Errorf("worker after dismiss = %+v, want released@42", after)
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-7" {
		t.Errorf("frame = %s %v", rpc, args)
	}

	// Idempotent: a second dismissal (double报) enqueues nothing further.
	s.dismissOutsourceWorkersForTask(task.ID, 43.0, triggerServer)
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("second dismissal must be a no-op, got %d frames", got)
	}
}

// ── close-out report → immediate dismissal (the wired §6.3 step-2 hook) ─────

// postCloseout drives the real close-out handler as caller sub (agent scope).
func postCloseout(t *testing.T, s *apiServer, taskID, sub string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := taskReq(t, http.MethodPost, "/api/tasks/"+taskID+"/closeout", nil,
		sub, "agent")
	s.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(rec, req, taskID)
	return rec
}

func TestCloseoutReport_DismissesWorkerImmediately(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000b", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-b", ClosedTS: 1,
	})
	// The worker is still ACTIVE here (closeTask normally releases it, but the
	// hook must be robust to a lingering row) — the close-out must both
	// release it AND reclaim its session at once, NOT after the grace.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-b", Codename: "O-11", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusActive,
	})

	if rec := postCloseout(t, s, task.ID, "ow-b"); rec.Code != http.StatusOK {
		t.Fatalf("closeout report: %d %s", rec.Code, rec.Body.String())
	}

	after, err := s.dal.GetOutsourceWorker("ow-b")
	if err != nil || after == nil {
		t.Fatalf("read back worker: %v", err)
	}
	if after.Status != WorkerStatusReleased {
		t.Errorf("worker after close-out = %q, want released", after.Status)
	}
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 immediate worker_stop, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-b" {
		t.Errorf("frame = %s %v, want worker_stop ow-b", rpc, args)
	}
}

func TestCloseoutReport_RepeatIsNoOp_NoSecondDispatch(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000c", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-c", ClosedTS: 1,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-c", Codename: "O-12", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusReleased, ReleasedTS: 1,
	})

	if rec := postCloseout(t, s, task.ID, "ow-c"); rec.Code != http.StatusOK {
		t.Fatalf("first closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
		t.Fatalf("first closeout: want 1 worker_stop, got %d", got)
	}
	if rec := postCloseout(t, s, task.ID, "ow-c"); rec.Code != http.StatusOK {
		t.Fatalf("repeat closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("repeat closeout must dispatch nothing, got %d frames", got)
	}
}

func TestCloseoutReport_MemberTask_NoDismissal(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000d", Title: "ad-hoc thing",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorMember, ExecutorID: "mira", ClosedTS: 1,
	})
	// An UNRELATED live worker on another task must be untouched by this
	// member close-out (the dismissal is task-scoped).
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-d", Codename: "O-13", Model: "opus", Effort: "high",
		TaskID: "t-something-else", Status: WorkerStatusActive,
	})

	if rec := postCloseout(t, s, task.ID, "mira"); rec.Code != http.StatusOK {
		t.Fatalf("member closeout: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("member-task closeout must dispatch no worker_stop, got %d", got)
	}
	after, err := s.dal.GetOutsourceWorker("ow-d")
	if err != nil || after == nil || after.Status != WorkerStatusActive {
		t.Errorf("unrelated worker must stay active, got %+v (err %v)", after, err)
	}
}

// ── the scheduler tick's lifecycle passes ────────────────────────────────────

func TestTick_ReclaimBackstop_GraceRespected(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	putTaskFixture(t, s, Task{
		ID: "t-000000000008", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusDone, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-8", ClosedTS: now,
	})
	// Released WITHIN the grace → left alone; released BEYOND it → reclaimed.
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-8", Codename: "O-8", Model: "opus", Effort: "high",
		TaskID: "t-000000000008", Status: WorkerStatusReleased, ReleasedTS: now - 5,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-9", Codename: "O-9", Model: "opus", Effort: "high",
		TaskID: "t-000000000008", Status: WorkerStatusReleased,
		ReleasedTS: now - workerReclaimGraceSecs - 5,
	})

	s.runOutsourceTick(now)

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want exactly 1 backstop worker_stop, got %d", len(frames))
	}
	if _, args := decodeWardenFrame(t, frames[0]); args["member_id"] != "ow-9" {
		t.Errorf("backstop reclaimed %v, want ow-9", args["member_id"])
	}
}

func TestTick_AssignedWorker_RedispatchesSpawn(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	task := putTaskFixture(t, s, Task{
		ID: "t-00000000000a", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-a",
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-a", Codename: "O-10", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})

	s.runOutsourceTick(nowSecs())

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("want 1 worker_start from the tick pass, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-a" {
		t.Errorf("frame = %s %v", rpc, args)
	}
}
