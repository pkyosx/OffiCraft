package main

// api_outsource_test.go — pins for the 外包 panel read face (api_outsource.go):
// the list must carry the CALLER's unread chat count per worker (the office
// row's red badge — owner report 2026-07-14: 外包列也要有未讀紅點), computed
// with the SAME UnreadCounts watermark inverse the member roster serves.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// assignOneWorker drives the manual tick to bind exactly one worker to a fresh
// outsource task, returning the worker id. Shared by the T-f190 fold + relocate
// pins below.
func assignOneWorker(t *testing.T, api *apiServer) string {
	t.Helper()
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
	task := createOutsourceTask(t, api, "review-pr", "review 1")
	api.runOutsourceTick(1000.0)
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("task not assigned after tick: %+v (%v)", bound, err)
	}
	return bound.ExecutorID
}

// TestListOutsourceWorkers_RuntimeFold (T-f190 item 1 + 2): the DTO folds the
// worker's REAL runtime facts from the SAME per-actor maps the member roster
// reads — machine (last_spawn_target resolved through the machine_alias overlay,
// honest raw-id / "" when unresolved / never dispatched), Claude account + live
// cost (telemetry), context % (gauge) — and the REAL delegator (the bound task's
// creator resolved to a member name + the raw creator_id), NOT a hardcoded
// "System owner". A worker that never reported a fact serves the honest null.
func TestListOutsourceWorkers_RuntimeFold(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // manual tick — deterministic single worker

	// The task creator is the token sub createOutsourceTask posts as ("m-front").
	// Seed that member so delegated_by resolves to a REAL name.
	creator := fullMember("m-front")
	creator.Name = "小前"
	if err := api.dal.PutMember(creator); err != nil {
		t.Fatalf("seed creator: %v", err)
	}

	workerID := assignOneWorker(t, api)

	// No online warden was connected, so the tick found no eligible host: the
	// worker is assigned but NEVER dispatched → last_spawn_target "" → the panel
	// renders 「尚未分配」, never a fabricated machine name.
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Machine != "" {
		t.Fatalf("never-dispatched worker must serve empty machine, got %+v", rows)
	}
	// It also has no telemetry/gauge yet → account/context/cost stay null (honest
	// dash on the panel), never a fabricated 0.
	if rows[0].Account != nil || rows[0].ContextPct != nil || rows[0].Cost != nil {
		t.Fatalf("unreported runtime must be null, got %+v", rows[0])
	}
	// The delegator resolves to the creator's REAL name + raw id (not "System owner").
	if rows[0].DelegatedBy != "小前" || rows[0].CreatorID != "m-front" {
		t.Fatalf("delegated_by=%q creator_id=%q, want 小前 / m-front",
			rows[0].DelegatedBy, rows[0].CreatorID)
	}

	// Now stamp the ACTUAL dispatch target (the in-memory spawn observation
	// since P7d) + an alias overlay, and report runtime facts on the SAME
	// per-actor maps the member roster reads.
	api.workerSpawnTarget[workerID] = "mach-1"
	if err := api.dal.PutMachineAlias(MachineAlias{MachineID: "mach-1", DisplayName: "MBP 5"}); err != nil {
		t.Fatalf("put alias: %v", err)
	}
	api.telemetry.Set(workerID, map[string]any{"account": "5e163893-raw-key", "cost": 2.5})
	api.gauge.Set(workerID, map[string]any{"context_pct": 37.0})

	rows = listWorkersAs(t, api, wireOwnerID)
	got := rows[0]
	if got.Machine != "MBP 5" {
		t.Errorf("machine = %q, want the alias display name MBP 5", got.Machine)
	}
	// T-ba6b: a raw account key with NO readable name (no alias, no reported
	// label) serves null — the raw credential hash never reaches the wire.
	if got.Account != nil {
		t.Errorf("unresolvable account must serve null, got %q", *got.Account)
	}
	if got.Cost == nil || *got.Cost != 2.5 {
		t.Errorf("cost = %v, want 2.5", got.Cost)
	}
	if got.ContextPct == nil || *got.ContextPct != 37.0 {
		t.Errorf("context_pct = %v, want 37", got.ContextPct)
	}

	// An owner-set alias resolves it — the panel then shows the readable name.
	if err := api.dal.PutAccountAlias(AccountAlias{
		Account: "5e163893-raw-key", DisplayName: "shawn-claude"}); err != nil {
		t.Fatalf("put account alias: %v", err)
	}
	rows = listWorkersAs(t, api, wireOwnerID)
	if rows[0].Account == nil || *rows[0].Account != "shawn-claude" {
		t.Errorf("aliased account = %v, want shawn-claude", rows[0].Account)
	}
}

// TestListOutsourceWorkers_AccountLabelOwnerGate (T-ba6b): the reporter-supplied
// account_label resolves the worker's account for the OWNER only (PII gate —
// the same monitoring overlay rule); a non-owner caller gets null, never the
// raw key and never the label.
func TestListOutsourceWorkers_AccountLabelOwnerGate(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	api.telemetry.Set(workerID, map[string]any{
		"account":       "0cea9af2-raw-key",
		"account_label": "eva@corp(Corp)",
		"ts":            1500.0,
	})

	rows := listWorkersAs(t, api, wireOwnerID)
	if rows[0].Account == nil || *rows[0].Account != "eva@corp(Corp)" {
		t.Fatalf("owner must see the reported label, got %v", rows[0].Account)
	}

	// A non-owner (agent-scope) caller: the label overlay stays empty and the
	// fold must NOT degrade to the raw key — honest null.
	rec := httptest.NewRecorder()
	api.HandleListOutsourceWorkersApiOutsourceWorkersGet(rec,
		taskReq(t, "GET", "/api/outsource-workers", nil, "mira", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("agent list workers: %d %s", rec.Code, rec.Body.String())
	}
	agentRows := decodeBody[[]outsourceWorkerDTO](t, rec)
	if agentRows[0].Account != nil {
		t.Fatalf("non-owner must not see label or raw key, got %q", *agentRows[0].Account)
	}
	if strings.Contains(rec.Body.String(), "0cea9af2-raw-key") ||
		strings.Contains(rec.Body.String(), "eva@corp") {
		t.Fatalf("non-owner body leaks the account identity: %s", rec.Body.String())
	}
}

// TestListOutsourceWorkers_MachineRawFallback (T-f190): a dispatch target with no
// alias overlay resolves to the RAW machine id — honest, never fabricated.
func TestListOutsourceWorkers_MachineRawFallback(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	api.workerSpawnTarget[workerID] = "mach-unaliased"
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Machine != "mach-unaliased" {
		t.Fatalf("unaliased target must fall back to the raw id, got %+v", rows)
	}
}

// TestListOutsourceWorkers_OwnerCreatorNotFabricated (T-f190 item 2): the owner's
// own ticket carries creator_id "owner" and an EMPTY delegated_by — the client
// renders the owner label, not a fabricated member name.
func TestListOutsourceWorkers_OwnerCreatorNotFabricated(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	// Flip the bound task's creator to the owner literal.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	task, err := api.dal.GetTask(w.TaskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	task.CreatorID = wireOwnerID
	if err := api.dal.PutTask(*task); err != nil {
		t.Fatalf("put task: %v", err)
	}
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].CreatorID != wireOwnerID || rows[0].DelegatedBy != "" {
		t.Fatalf("owner creator must carry no resolved name, got %+v", rows)
	}
}

// TestListOutsourceWorkers_WorkerCreatorResolvesToCodename pins the DECLARED
// P7d behavior change: a task created BY a worker (agent-scoped create_task)
// now resolves delegated_by to the creating worker's codename — pre-fold the
// GetMember(ow-) miss degraded to "" and the client fell back to the raw
// creator_id. The fold makes the lookup hit, which is the constitution's point
// (外包＝正職): the delegator is named like any member, never fabricated.
func TestListOutsourceWorkers_WorkerCreatorResolvesToCodename(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	task, err := api.dal.GetTask(w.TaskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	task.CreatorID = workerID // a worker delegating to itself is shape enough
	if err := api.dal.PutTask(*task); err != nil {
		t.Fatalf("put task: %v", err)
	}
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].CreatorID != workerID || rows[0].DelegatedBy != w.Codename {
		t.Fatalf("worker creator must resolve to its codename %q, got %+v", w.Codename, rows)
	}
}

// TestGetWorkerBootContext (T-ba6b): the detail panel's initial-prompt preview
// re-runs the SAME buildWorkerBootContext fold the spawn path uses, over the
// CURRENT DB rows — the response carries the seed + identity + task + manual
// text and NEVER a token (parity with the member /api/bootstrap UI preview).
func TestGetWorkerBootContext(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}

	rec := httptest.NewRecorder()
	api.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/"+workerID+"/boot-context",
			nil, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("boot-context: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[WorkerBootContextDTO](t, rec)
	for _, want := range []string{w.Codename, "review 1", "# 你的身分", "# 你的任務"} {
		if !strings.Contains(got.Context, want) {
			t.Errorf("preview must contain %q", want)
		}
	}
	// The preview must never mint or echo a credential.
	if strings.Contains(rec.Body.String(), "worker_token") ||
		strings.Contains(rec.Body.String(), "token\"") {
		t.Fatalf("preview must not carry any token: %s", rec.Body.String()[:200])
	}

	// The preview reads the CURRENT rows: an edited task description shows up
	// (honest 「目前版本」 semantics — NOT a verbatim spawn-time record).
	task, err := api.dal.GetTask(w.TaskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	task.Description = "事後補充的描述"
	if err := api.dal.PutTask(*task); err != nil {
		t.Fatalf("put task: %v", err)
	}
	rec = httptest.NewRecorder()
	api.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/"+workerID+"/boot-context",
			nil, wireOwnerID, "owner"), workerID)
	if got := decodeBody[WorkerBootContextDTO](t, rec); !strings.Contains(got.Context, "事後補充的描述") {
		t.Errorf("preview must re-assemble from the current task row")
	}
}

// TestGetWorkerBootContext_UnknownWorker404 (T-ba6b): a stale route answers an
// honest 404, never an empty preview.
func TestGetWorkerBootContext_UnknownWorker404(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	rec := httptest.NewRecorder()
	api.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/ow-nope/boot-context",
			nil, wireOwnerID, "owner"), "ow-nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown worker = %d, want 404", rec.Code)
	}
}

// TestRelocateOutsourceWorker (T-f190 item 3): the owner 改機器 writes the
// owner-pinned desired_machine_id, clears the OLD session on the old target
// (worker_stop), and re-dispatches onto the PINNED machine (worker_start with
// machinePref = the pin) — all WITHOUT touching lifecycle (status stays assigned).
func TestRelocateOutsourceWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	// A real, active machine member so resolveMachine accepts the pin, online on
	// the hub so the re-dispatch lands on it.
	newMachine := fullMember("m-new")
	newMachine.Kind = machineKind
	if err := api.dal.PutMember(newMachine); err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	connectWarden(t, api, "m-new")
	// Pretend the worker currently has a live session on an old (online) host, so
	// the relocate must clear it there.
	connectWarden(t, api, "m-old")
	api.workerSpawnTarget[workerID] = "m-old"

	rec := httptest.NewRecorder()
	api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
		taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
			map[string]any{"machine_id": "m-new"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}

	// The pin is durable and lifecycle is untouched.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if w.DesiredMachineID != "m-new" {
		t.Errorf("desired_machine_id = %q, want m-new", w.DesiredMachineID)
	}
	if w.Status != WorkerStatusAssigned {
		t.Errorf("relocate must not change lifecycle, status = %q", w.Status)
	}

	// The OLD host got a worker_stop (session cleared there)…
	oldFrames := api.hub.DrainWardenCommands("m-old")
	if len(oldFrames) != 1 {
		t.Fatalf("want 1 worker_stop to the old host, got %d", len(oldFrames))
	}
	if rpc, args := decodeWardenFrame(t, oldFrames[0]); rpc != reconcileCmdStop ||
		args["member_id"] != workerID {
		t.Errorf("old-host frame = %s %v, want worker_stop for %s", rpc, args, workerID)
	}
	// …and the PINNED host got the re-spawn (worker_start), proving machinePref
	// now prefers the pin over the manual's placement.
	newFrames := api.hub.DrainWardenCommands("m-new")
	if len(newFrames) != 1 {
		t.Fatalf("want 1 worker_start to the pinned host, got %d", len(newFrames))
	}
	if rpc, args := decodeWardenFrame(t, newFrames[0]); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Errorf("pinned-host frame = %s %v, want worker_start for %s", rpc, args, workerID)
	}
}

// TestRelocateOutsourceWorker_Rejects (T-f190 item 3): an unknown worker and an
// unknown machine both 404 — the pin never lands on a placement that can't boot,
// and a stale route never silently succeeds.
func TestRelocateOutsourceWorker_Rejects(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	// Unknown worker id → 404.
	rec := httptest.NewRecorder()
	api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
		taskReq(t, "POST", "/api/outsource-workers/ow-nope/relocate",
			map[string]any{"machine_id": "auto"}, wireOwnerID, "owner"), "ow-nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown worker: want 404, got %d %s", rec.Code, rec.Body.String())
	}

	// A concrete pin naming a machine that does not exist → 404 (never pinned).
	rec = httptest.NewRecorder()
	api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
		taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
			map[string]any{"machine_id": "m-ghost"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown machine: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	// The rejected pin never touched the row.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil || w.DesiredMachineID != "" {
		t.Fatalf("rejected relocate must leave desired_machine_id empty, got %+v", w)
	}
}

// relocateOK drives the owner 改機器 handler and asserts a 200. Shared by the
// input-shape fixtures below.
func relocateOK(t *testing.T, api *apiServer, workerID, machineID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
		taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
			map[string]any{"machine_id": machineID}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate → %s: %d %s", machineID, rec.Code, rec.Body.String())
	}
}

// seedMachine registers an active machine member so resolveMachine accepts a
// concrete pin, and (when online) makes it an eligible spawn target on the hub.
func seedMachine(t *testing.T, api *apiServer, id string) {
	t.Helper()
	m := fullMember(id)
	m.Kind = machineKind
	if err := api.dal.PutMember(m); err != nil {
		t.Fatalf("seed machine %s: %v", id, err)
	}
}

// oneFrame drains exactly one warden command off target and returns its rpc +
// args, failing if the count is not 1.
func oneFrame(t *testing.T, api *apiServer, target string) (string, map[string]any) {
	t.Helper()
	frames := api.hub.DrainWardenCommands(target)
	if len(frames) != 1 {
		t.Fatalf("want exactly 1 frame on %s, got %d", target, len(frames))
	}
	return decodeWardenFrame(t, frames[0])
}

// TestRelocateActiveWorker_MovesImmediately (T-f190 item 3, review gap): an
// ACTIVE worker (already claimed its task) must move THE MOMENT the owner
// relocates — NOT wait for a scheduler tick. The tick only re-spawns 'assigned'
// workers (outsource_sched), so a tick-deferred relocate would strand an active
// worker on the old machine forever. relocateWorkerNow dispatches immediately;
// this pins that behaviour (drained WITHOUT running a tick) and that lifecycle
// is untouched (a relocate is a placement change, not a state change).
func TestRelocateActiveWorker_MovesImmediately(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	// Flip the worker ACTIVE (claimed) with a live session on an old online host.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	w.Status = WorkerStatusActive
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("flip active: %v", err)
	}
	seedMachine(t, api, "m-new")
	connectWarden(t, api, "m-new")
	connectWarden(t, api, "m-old")
	api.workerSpawnTarget[workerID] = "m-old"

	relocateOK(t, api, workerID, "m-new")

	// Immediate (no tick ran): old host cleared, new host re-spawned.
	if rpc, args := oneFrame(t, api, "m-old"); rpc != reconcileCmdStop || args["member_id"] != workerID {
		t.Errorf("old host frame = %s %v, want worker_stop for %s", rpc, args, workerID)
	}
	if rpc, args := oneFrame(t, api, "m-new"); rpc != reconcileCmdStart || args["member_id"] != workerID {
		t.Errorf("new host frame = %s %v, want worker_start for %s", rpc, args, workerID)
	}
	// Lifecycle stayed active — a relocate never demotes a claimed worker.
	w, err = api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil || w.Status != WorkerStatusActive {
		t.Fatalf("relocate must keep active lifecycle, got %+v", w)
	}
}

// TestRelocateNeverDispatchedWorker (T-f190 item 3, review gap): relocating a
// worker that was NEVER dispatched (no live session anywhere — the 未分配 shape)
// must NOT fire a worker_stop (there is no old session to clear), only the
// worker_start onto the newly-pinned machine.
func TestRelocateNeverDispatchedWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	// No online warden at assign time → the worker was never dispatched: its
	// in-memory spawn target is empty (the 尚未分配 shape).
	if api.workerSpawnTarget[workerID] != "" {
		t.Fatalf("precondition: worker must be undispatched, target=%q", api.workerSpawnTarget[workerID])
	}
	seedMachine(t, api, "m-new")
	connectWarden(t, api, "m-new")

	relocateOK(t, api, workerID, "m-new")

	// The pinned host got the spawn, and it is the ONLY frame — no phantom
	// worker_stop to a machine the worker never ran on.
	if rpc, args := oneFrame(t, api, "m-new"); rpc != reconcileCmdStart || args["member_id"] != workerID {
		t.Errorf("new host frame = %s %v, want worker_start for %s", rpc, args, workerID)
	}
}

// TestRelocateToSameMachine (T-f190 item 3, review gap): the code path is NOT a
// no-op — relocating to the machine the worker already runs on kills the current
// session and re-spawns it on that SAME machine (a deliberate "restart here", the
// same 殺舊+重生 primitive). This pins the DEFINED behaviour so a future "skip
// when same" optimisation is a conscious change, not an accident.
func TestRelocateToSameMachine(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	seedMachine(t, api, "m-x")
	connectWarden(t, api, "m-x")
	api.workerSpawnTarget[workerID] = "m-x"

	relocateOK(t, api, workerID, "m-x")

	// Both frames land on m-x, in order: kill the old session, then re-spawn.
	frames := api.hub.DrainWardenCommands("m-x")
	if len(frames) != 2 {
		t.Fatalf("same-machine relocate must kill+respawn (2 frames), got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0]); rpc != reconcileCmdStop || args["member_id"] != workerID {
		t.Errorf("frame[0] = %s %v, want worker_stop for %s", rpc, args, workerID)
	}
	if rpc, args := decodeWardenFrame(t, frames[1]); rpc != reconcileCmdStart || args["member_id"] != workerID {
		t.Errorf("frame[1] = %s %v, want worker_start for %s", rpc, args, workerID)
	}
	// The pin is durably the same machine.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil || w.DesiredMachineID != "m-x" {
		t.Fatalf("desired_machine_id = %+v, want m-x", w)
	}
}

// TestNewOutsourceWorkerDTO_Presence (A案 P6 — the ONE member liveness
// vocabulary, replacing the retired spawn_state): the DTO projects presence
// distinct from lifecycle status so the cockpit can tell apart
//   - "online"  : truly alive — holding a live SSE connection (the SAME
//     hub.IsOnline presence authority the member roster reads);
//   - "waking"  : not online with a fresh wake in flight (last start dispatch /
//     row birth within WakingTTLSecs) — grey, not a false green;
//   - "offline" : the wake window lapsed with no session, or the session died
//     after the claim — the O-19 "綠燈但沒人" made honest in BOTH forms (the
//     states the retired projection called "stuck").
//
// A released row projects "" (it is filtered off the panel anyway).
func TestNewOutsourceWorkerDTO_Presence(t *testing.T) {
	const now = 1_000_000.0
	cases := []struct {
		name      string
		status    string
		createdTS float64
		spawnAt   float64
		online    bool
		want      string
	}{
		{"active and online is online", WorkerStatusActive, now - 5, 0, true, "online"},
		// The anti-latch pin (DoD③): an 'active' worker whose SSE session died
		// must NOT stay green. A mutant that latches on status==active turns
		// this case red.
		{"active but offline is offline", WorkerStatusActive, now - 500, 0, false, "offline"},
		{"assigned fresh is waking", WorkerStatusAssigned, now - 10, 0, false, "waking"},
		{"assigned online but unclaimed is online", WorkerStatusAssigned, now - 10, 0, true, "online"},
		{"assigned just inside the wake window is waking", WorkerStatusAssigned, now - (WakingTTLSecs - 1), 0, false, "waking"},
		{"assigned past the wake window is offline", WorkerStatusAssigned, now - (WakingTTLSecs + 1), 0, false, "offline"},
		// A fresh re-dispatch (FSM respawn) re-arms the wake window off spawnAt
		// even when the row itself is old.
		{"stale row with a fresh dispatch is waking", WorkerStatusAssigned, now - 10_000, now - 5, false, "waking"},
		{"released is blank", WorkerStatusReleased, now - 10000, 0, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := OutsourceWorker{ID: "ow-1", Codename: "O-7", Status: c.status,
				TaskID: "t-1", CreatedTS: c.createdTS}
			dto := newOutsourceWorkerDTO(w, nil,
				outsourceWorkerProjection{now: now, online: c.online, spawnAt: c.spawnAt})
			if dto.Presence != c.want {
				t.Fatalf("presence = %q, want %q", dto.Presence, c.want)
			}
		})
	}
}

// TestListOutsourceWorkers_PresenceUsesLivePresence (T-9ccf item 4, DoD③):
// end-to-end through the handler — an 'active' worker with NO live SSE
// connection must serve presence "offline" (session died after the claim), and
// only once it holds a hub connection does it read "online". This pins the
// WIRING (the handler passing hub.IsOnline), not just the pure projection.
func TestListOutsourceWorkers_PresenceUsesLivePresence(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
	task := createOutsourceTask(t, api, "review-pr", "review 1")
	api.runOutsourceTick(1000.0)
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("task not assigned after tick: %+v (%v)", bound, err)
	}
	workerID := bound.ExecutorID

	// Flip the worker 'active' (it claimed its task) but leave it with NO SSE
	// connection — the O-19 "claimed then the session died" shape.
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker %s: %+v (%v)", workerID, w, err)
	}
	w.Status = WorkerStatusActive
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("flip active: %v", err)
	}
	// Age the spawn observation past the wake window (the assignment tick just
	// stamped a fresh dispatch, which would honestly read "waking") — the shape
	// under test is "claimed long ago, session since died".
	api.outsourceMu.Lock()
	api.workerSpawnAt[workerID] = nowSecs() - (WakingTTLSecs + 10)
	api.outsourceMu.Unlock()
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Presence != "offline" {
		t.Fatalf("active-but-disconnected worker must read offline, got %+v", rows)
	}

	// Now give it a live SSE listener — presence flips it to a true green.
	if _, err := api.hub.Connect(workerID, ""); err != nil {
		t.Fatalf("connect worker listener: %v", err)
	}
	rows = listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Presence != "online" {
		t.Fatalf("active-and-connected worker must read online, got %+v", rows)
	}
}

// listWorkersAs GETs /api/outsource-workers through the handler as `sub`.
func listWorkersAs(t *testing.T, api *apiServer, sub string) []outsourceWorkerDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListOutsourceWorkersApiOutsourceWorkersGet(rec,
		taskReq(t, "GET", "/api/outsource-workers", nil, sub, "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list workers: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[[]outsourceWorkerDTO](t, rec)
}

func TestListOutsourceWorkersCarriesUnreadCount(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // manual tick — deterministic single worker
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
	task := createOutsourceTask(t, api, "review-pr", "review 1")
	api.runOutsourceTick(1000.0)

	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("task not assigned after tick: %+v (%v)", bound, err)
	}
	workerID := bound.ExecutorID

	// No chat yet → the row serves an explicit unread_count of 0.
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].UnreadCount != 0 {
		t.Fatalf("want one row with unread_count 0, got %+v", rows)
	}

	// Two worker→owner messages past the (absent) watermark count; an
	// owner→worker send and a worker→other message never do.
	for i, m := range []ChatMessage{
		{ID: "m-1", Sender: workerID, Recipient: wireOwnerID, Body: "回報 1", TS: 2000},
		{ID: "m-2", Sender: workerID, Recipient: wireOwnerID, Body: "回報 2", TS: 2001},
		{ID: "m-3", Sender: wireOwnerID, Recipient: workerID, Body: "收到", TS: 2002},
		{ID: "m-4", Sender: workerID, Recipient: "mira", Body: "同步", TS: 2003},
	} {
		if err := api.dal.PutChat(m); err != nil {
			t.Fatalf("put chat %d: %v", i, err)
		}
	}
	rows = listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].UnreadCount != 2 {
		t.Fatalf("want unread_count 2 for %s, got %+v", workerID, rows)
	}

	// The unread is PER-CALLER: another reader's watermark never clears the
	// owner's, and moving the owner's watermark past both messages clears it.
	if _, _, err := api.dal.PutChatRead(ChatRead{
		ReaderID: wireOwnerID, PeerID: workerID, LastReadTS: 2001,
	}); err != nil {
		t.Fatalf("put chat read: %v", err)
	}
	rows = listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].UnreadCount != 0 {
		t.Fatalf("watermark must clear the badge, got %+v", rows)
	}
}

// getWorkerAs drives the SINGLE-worker detail GET (GET /api/outsource-workers/{id})
// — the exact endpoint the 外包 detail panel fetches — and decodes the one DTO the
// panel binds its Claude Account cell from. Sibling of listWorkersAs for the
// detail path.
func getWorkerAs(t *testing.T, api *apiServer, sub, id string) outsourceWorkerDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetOutsourceWorkerApiOutsourceWorkersIdGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/"+id, nil, sub, "owner"), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("get worker %s: %d %s", id, rec.Code, rec.Body.String())
	}
	return decodeBody[outsourceWorkerDTO](t, rec)
}

// TestGetOutsourceWorker_AccountResolvedOnDetailPath (T-f190fix): the owner-
// reported bug lived on the DETAIL page, so the single-worker GET — not just the
// list — must route the Claude account through the shared resolveAccountDisplay
// fold. Its raw telemetry key is the real `<userID>/<organizationUuid>` shape
// (readClaudeAccount, cli/ocagent/contextreport.go): a 64-hex user id joined to
// an org uuid — exactly the credential string the panel used to leak. This locks
// the detail path so a regression that stopped resolving ONLY the single GET
// (which every list-only account test would still pass) can never re-expose it.
func TestGetOutsourceWorker_AccountResolvedOnDetailPath(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // manual tick — deterministic single worker
	workerID := assignOneWorker(t, api)

	// The warden reports a session's account as readClaudeAccount's
	// `<userID(64-hex)>/<organizationUuid>` composite (the exact two-segment
	// shape the owner's screenshot leaked). We use an OBVIOUSLY-synthetic
	// stand-in carrying the repo's `-raw-key` marker (matching the sibling
	// list tests) — a real 64-hex literal trips the CI gitleaks generic-api-key
	// gate, and the test only needs the composite raw SHAPE, not real entropy.
	// With NO alias and no reported label it is UNRESOLVABLE, so the detail DTO
	// must serve null → the panel's honest dash, NEVER this raw string.
	const rawKey = "5e163893-user-raw-key/0cea9af2-org-raw-key"
	api.telemetry.Set(workerID, map[string]any{"account": rawKey, "cost": 1.0})

	got := getWorkerAs(t, api, wireOwnerID, workerID)
	if got.Account != nil {
		t.Fatalf("detail GET: unresolvable account must serve null (honest dash), "+
			"got raw leak %q", *got.Account)
	}

	// An owner-set alias resolves it — the detail panel then shows the SAME
	// readable name the member panel does (parity), never the raw key.
	if err := api.dal.PutAccountAlias(AccountAlias{
		Account: rawKey, DisplayName: "shawn-claude"}); err != nil {
		t.Fatalf("put account alias: %v", err)
	}
	got = getWorkerAs(t, api, wireOwnerID, workerID)
	if got.Account == nil || *got.Account != "shawn-claude" {
		t.Fatalf("detail GET: aliased account = %v, want shawn-claude", got.Account)
	}
}

// TestNewOutsourceWorkerDTO_GoldenWireShape pins the EXACT serialised wire
// shape of the worker DTO (P7b read-path convergence): the goldens below were
// captured from the pre-convergence builder, so the shared-fold refactor must
// reproduce them byte-for-byte — field order, names, null-vs-value, everything.
func TestNewOutsourceWorkerDTO_GoldenWireShape(t *testing.T) {
	ok := true
	fullWorker := OutsourceWorker{
		ID:               "ow-1",
		Codename:         "O-7",
		Model:            "claude-sonnet-4-5",
		Effort:           "high",
		TaskID:           "t-1",
		Status:           WorkerStatusActive,
		CreatedTS:        1000.0,
		LastOp:           "worker_start",
		LastOpOK:         &ok,
		LastOpLog:        "spawned ok",
		LastOpAt:         1501.0,
		DesiredMachineID: "mac-2",
		RefocusSince:     1600.0,
		DesiredState:     "online",
		BankedCost:       3.25,
	}
	fullTask := &Task{ID: "t-1", Title: "review 1", Status: "in_progress", CreatorID: "m-9"}
	fullProjection := outsourceWorkerProjection{
		unread:         4,
		now:            2000.0,
		online:         true,
		tele:           map[string]any{"account": "raw-key-1", "cost": 1.5},
		gaugeEntry:     map[string]any{"context_pct": 42.0},
		spawnTarget:    "mac-1",
		machineDisplay: func(id string) string { return "Mac Studio (" + id + ")" },
		accountDisplay: func(raw string) string { return "alice@example.com" },
		delegatedBy:    "Bob",
	}
	cases := []struct {
		name string
		w    OutsourceWorker
		task *Task
		p    outsourceWorkerProjection
		want string
	}{
		{
			name: "every field populated",
			w:    fullWorker, task: fullTask, p: fullProjection,
			want: `{"id":"ow-1","codename":"O-7","model":"claude-sonnet-4-5","effort":"high","status":"active","task_id":"t-1","task_title":"review 1","task_status":"in_progress","created_ts":1000,"unread_count":4,"presence":"online","machine":"Mac Studio (mac-1)","desired_machine_id":"mac-2","account":"alice@example.com","context_pct":42,"cost":1.5,"banked_cost":3.25,"last_op":"worker_start","last_op_ok":true,"last_op_log":"spawned ok","last_op_reason":"","last_op_at":1501,"creator_id":"m-9","delegated_by":"Bob","refocus_since":1600,"desired_state":"online"}`,
		},
		{
			name: "bare row honest empties",
			w: OutsourceWorker{ID: "ow-2", Codename: "O-8",
				Model: "claude-haiku-4-5", TaskID: "t-2",
				Status: WorkerStatusAssigned, CreatedTS: 1999.0},
			task: nil, p: outsourceWorkerProjection{now: 2000.0},
			want: `{"id":"ow-2","codename":"O-8","model":"claude-haiku-4-5","effort":"","status":"assigned","task_id":"t-2","task_title":"","task_status":"","created_ts":1999,"unread_count":0,"presence":"waking","machine":"","desired_machine_id":"","account":null,"context_pct":null,"cost":null,"banked_cost":null,"last_op":"","last_op_ok":null,"last_op_log":"","last_op_reason":"","last_op_at":0,"creator_id":"","delegated_by":"","refocus_since":0,"desired_state":""}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(newOutsourceWorkerDTO(c.w, c.task, c.p))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != c.want {
				t.Fatalf("wire shape drifted:\n got %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestFoldActorRuntime(t *testing.T) {
	t.Run("nil maps and zero banked fold all-empty", func(t *testing.T) {
		f := foldActorRuntime(nil, nil, 0)
		if f.account != "" || f.cost != nil || f.contextPct != nil || f.bankedCost != nil {
			t.Fatalf("empty fold = %+v, want all zero", f)
		}
	})
	t.Run("reported facts fold through", func(t *testing.T) {
		f := foldActorRuntime(
			map[string]any{"account": "raw-key-1", "cost": 2.5},
			map[string]any{"context_pct": 37.0}, 1.25)
		if f.account != "raw-key-1" {
			t.Errorf("account = %q, want raw-key-1", f.account)
		}
		if f.cost == nil || *f.cost != 2.5 {
			t.Errorf("cost = %v, want 2.5", f.cost)
		}
		if f.contextPct == nil || *f.contextPct != 37.0 {
			t.Errorf("context_pct = %v, want 37", f.contextPct)
		}
		if f.bankedCost == nil || *f.bankedCost != 1.25 {
			t.Errorf("banked_cost = %v, want 1.25", f.bankedCost)
		}
	})
	t.Run("wrong-typed entries fold empty not fabricated", func(t *testing.T) {
		f := foldActorRuntime(
			map[string]any{"account": 7, "cost": "x"},
			map[string]any{"context_pct": "high"}, 0)
		if f.account != "" || f.cost != nil || f.contextPct != nil || f.bankedCost != nil {
			t.Fatalf("mistyped fold = %+v, want all zero", f)
		}
	})
}

// TestRelocateOutsourceWorker_AdminGated (P7c 外包對齊正職): the route's floor
// dropped from owner to admin_agent — the exact member relocate floor. Pinned
// through the FULL wired stack: a plain agent is a flat 403 envelope; the
// admin (seeded Mira, role assistant) and the owner both pass the gate and
// land the honest 404 on an unknown worker (no worker rows in this fixture).
func TestRelocateOutsourceWorker_AdminGated(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()

	relocate := func(token string) (int, string) {
		t.Helper()
		req, err := http.NewRequest("POST", srv.URL+"/api/outsource-workers/ow-nope/relocate",
			strings.NewReader(`{"machine_id":"auto"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	if status, body := relocate(agentTok); status != 403 || !strings.Contains(body, `"code":"forbidden"`) {
		t.Fatalf("plain agent: want 403 envelope, got %d %s", status, body)
	}
	adminTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	if status, body := relocate(adminTok); status != 404 {
		t.Fatalf("admin agent must pass the gate (honest 404 on ow-nope): got %d %s", status, body)
	}
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	if status, body := relocate(ownerTok); status != 404 {
		t.Fatalf("owner must pass the gate (honest 404 on ow-nope): got %d %s", status, body)
	}
}
