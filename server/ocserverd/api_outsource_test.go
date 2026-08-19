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
	api.telemetry.Set(workerID, map[string]any{"account": "5e163893-raw-key", accountRuntimeKey: RuntimeClaude, "cost": 2.5})
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
		"account":         "0cea9af2-raw-key",
		accountRuntimeKey: RuntimeClaude,
		"account_label":   "eva@corp(Corp)",
		"ts":              1500.0,
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
// re-runs the SAME buildWorkerBootContext fold the spawn path uses, and NEVER
// carries a token (parity with the member /api/bootstrap UI preview).
//
// 🔴 T-4595 rewrote the interesting half. This used to assert the preview
// carried the identity block, the bound task and the manual, and that editing
// the task description showed up in it. None of that is true any more, and the
// old assertions were not merely stale — they pinned the very thing the ruling
// removed: a worker's boot context is the staff fold minus the persona slot, so
// it does not vary with the task or its manual AT ALL.
//
// What is asserted instead is the property that survived and the one that
// replaced it: the response really is the production fold (not an empty string
// or some second assembly), and it is INVARIANT to an edited task row — the
// strongest available statement, and one that turns red if any per-task text
// comes back through this endpoint.
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
	// It is the real fold: the two shared slots a worker actually receives.
	// Without this, every absence/invariance assertion below is satisfied by an
	// empty string.
	for _, want := range []string{"# Global Context", "# 啟動程序（Boot Sequence"} {
		if !strings.Contains(got.Context, want) {
			t.Errorf("preview must contain the shared block %q", want)
		}
	}
	for _, gone := range []string{"# 你的身分", "# 你的任務", "# 任務手冊", w.Codename} {
		if strings.Contains(got.Context, gone) {
			t.Errorf("preview still carries %q — a worker's boot context is the staff "+
				"fold minus the persona slot (T-4595)", gone)
		}
	}
	// The preview must never mint or echo a credential.
	if strings.Contains(rec.Body.String(), "worker_token") ||
		strings.Contains(rec.Body.String(), "token\"") {
		t.Fatalf("preview must not carry any token: %s", rec.Body.String()[:200])
	}

	// The preview does not vary with the bound task. The edit goes through
	// writeTaskDescription — the production write path (T-e271) — and NOT
	// through PutTask, which no longer writes the description column at all
	// (see PutTask's own note). Using PutTask here would exercise a mechanism
	// no caller uses, and the assertion would stop meaning anything.
	task, err := api.dal.GetTask(w.TaskID)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	if ok, err := api.writeTaskDescription(task, wireOwnerID, "事後補充的描述"); err != nil || !ok {
		t.Fatalf("edit task description: ok=%v err=%v", ok, err)
	}
	rec = httptest.NewRecorder()
	api.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(rec,
		taskReq(t, "GET", "/api/outsource-workers/"+workerID+"/boot-context",
			nil, wireOwnerID, "owner"), workerID)
	after := decodeBody[WorkerBootContextDTO](t, rec)
	if strings.Contains(after.Context, "事後補充的描述") {
		t.Error("preview pastes the bound task's description — the worker reads the " +
			"live task itself; a boot-time copy can only be stale (T-4595)")
	}
	if after.Context != got.Context {
		t.Error("preview varies with the bound task row; it must not")
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
	if rpc, args := decodeWardenFrame(t, oldFrames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != workerID {
		t.Errorf("old-host frame = %s %v, want worker_stop for %s", rpc, args, workerID)
	}
	// …and the PINNED host got the re-spawn (worker_start), proving machinePref
	// now prefers the pin over the manual's placement.
	newFrames := api.hub.DrainWardenCommands("m-new")
	if len(newFrames) != 1 {
		t.Fatalf("want 1 worker_start to the pinned host, got %d", len(newFrames))
	}
	if rpc, args := decodeWardenFrame(t, newFrames[0].Frame); rpc != reconcileCmdStart ||
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

// TestRelocateOutsourceWorker_RejectsUnresolvableMachine: ANY non-"" machine_id
// must resolve to a real machine — "auto" is no longer waved through as a
// pseudo-machine. "" stays the legal clear.
func TestRelocateOutsourceWorker_RejectsUnresolvableMachine(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	seedMachine(t, api, "m-real")

	relocate := func(machineID string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
			taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
				map[string]any{"machine_id": machineID}, wireOwnerID, "owner"), workerID)
		return rec
	}

	// SENTINEL: a real concrete machine id still pins.
	if rec := relocate("m-real"); rec.Code != http.StatusOK {
		t.Fatalf("concrete machine relocate must 200, got %d %s", rec.Code, rec.Body.String())
	}
	for _, machineID := range []string{"auto", "m-ghost"} {
		if rec := relocate(machineID); rec.Code != http.StatusNotFound {
			t.Fatalf("machine_id %q must 404, got %d %s", machineID, rec.Code, rec.Body.String())
		}
		w, _ := api.dal.GetOutsourceWorker(workerID)
		if w == nil || w.DesiredMachineID != "m-real" {
			t.Fatalf("a rejected %q relocate must leave the pin alone: %+v", machineID, w)
		}
	}
	// Owner 2026-07-27: 搬遷一定要帶機器 — "" is no longer the unpin, it is a 400,
	// and the pin it used to destroy survives untouched.
	if rec := relocate(""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a blank machine_id must 400, got %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w == nil || w.DesiredMachineID != "m-real" {
		t.Fatalf("a refused blank relocate must leave the worker pin alone: %+v", w)
	}
	// Absent key ⇒ the missing-required-field 422 face.
	rec := httptest.NewRecorder()
	api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
		taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
			map[string]any{}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an absent machine_id must 422, got %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w == nil || w.DesiredMachineID != "m-real" {
		t.Fatalf("a refused absent relocate must leave the worker pin alone: %+v", w)
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
	return decodeWardenFrame(t, frames[0].Frame)
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

// x46Worker seeds the VERBATIM X-46 row read off the live cockpit: an already
// minted worker, status ASSIGNED (never claimed), presence offline, desired_state
// online, bound task not_started, pinned to a concrete machine that is online and
// advertises its (codex) runtime — and every last_op field blank. Returns the
// worker id.
func x46Worker(t *testing.T, api *apiServer, pin string) string {
	t.Helper()
	seedMachine(t, api, pin)
	connectWarden(t, api, pin)
	api.telemetry.Set(pin, map[string]any{"runtimes": map[string]any{
		RuntimeCodex: map[string]any{"installed": true, "logged_in": true},
	}})
	task := putTaskFixture(t, api, Task{
		ID: "t-0ac4a22f3821", TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-49c2c70c9448",
	})
	putWorkerFixture(t, api, OutsourceWorker{
		ID: "ow-49c2c70c9448", Codename: "X-46", Runtime: RuntimeCodex,
		Model: "gpt-5.6-terra", Effort: "medium", TaskID: task.ID,
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	})
	return "ow-49c2c70c9448"
}

// TestRelocateAssignedWorker_X46 (T-e0e3): the X-46 row, relocated onto its
// concrete machine. The immediate path DOES dispatch here — status is `assigned`,
// so respawnWorkerNow's active-only O-28 deferral never applies — which is exactly
// why the blank row was so misleading: the start went out and left no trace,
// because worker spawn observability is in-memory by contract and nothing durable
// was written until the boot was judged failed.
func TestRelocateAssignedWorker_X46(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := x46Worker(t, api, "m-11b2")

	relocateOK(t, api, workerID, "m-11b2")

	if rpc, args := oneFrame(t, api, "m-11b2"); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Errorf("the relocate must dispatch a start to the pin: %s %v", rpc, args)
	}
	if got := api.workerSpawnTarget[workerID]; got != "m-11b2" {
		t.Errorf("spawn target = %q, want m-11b2", got)
	}

	// NOTHING ever drains that machine's command FIFO — which is what the field
	// evidence on eva-m5 showed: the warden logs a receipt line for every frame it
	// reads (cli/ocwarden/transport.go, logged BEFORE dispatch precisely so
	// delivery is provable from the warden log alone), and X-46's id appears zero
	// times there while other workers on the same machine appear repeatedly. So the
	// honest verdict for X-46 is "never collected", NOT "the runtime failed to
	// boot" — the frames never reached anything that could try.
	base := nowSecs()
	for i := 0; i < 4; i++ {
		api.runOutsourceTick(base + float64(i)*(WakingTTLSecs+10))
	}
	if api.hub.PendingWardenCommands("m-11b2") == 0 {
		t.Fatal("precondition: this fixture must leave the frames uncollected")
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if !strings.HasPrefix(w.LastOpReason, spawnReasonNeverCollected+":") || w.LastOpAt == 0 {
		t.Fatalf("an uncollected start must say so, got last_op=%q reason=%q at=%v",
			w.LastOp, w.LastOpReason, w.LastOpAt)
	}
	// And it must survive a server re-exec, unlike the in-memory machine cell —
	// that asymmetry is what made X-46 look like "no attempt was ever made".
	api.workerSpawnTarget = map[string]string{}
	api.workerReconcileStates = map[string]reconcileState{}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if !strings.HasPrefix(after.LastOpReason, spawnReasonNeverCollected+":") {
		t.Fatalf("the receipt must be durable across a re-exec, got %q", after.LastOpReason)
	}
}

// TestRelocateStoppedWorker_SavesPinWithoutReviving (owner ruling: placement is not
// a start): a worker the owner explicitly STOPPED keeps its 停止 across a 改機器.
// The pin is saved, nothing is dispatched, desired_state is untouched, and the row
// says WHY nothing started — the tick has always honoured this
// (TestStoppedWorker_TickNeverRevives); relocate used to be the one verb that
// quietly overturned it.
func TestRelocateStoppedWorker_SavesPinWithoutReviving(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	w.DesiredState = DesiredStateOffline // owner pressed 停止
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stop worker: %v", err)
	}
	seedMachine(t, api, "m-new")
	connectWarden(t, api, "m-new")

	relocateOK(t, api, workerID, "m-new")

	if got := len(api.hub.DrainWardenCommands("m-new")); got != 0 {
		t.Errorf("a relocate must never revive a stopped worker, got %d frames", got)
	}
	got, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if got.DesiredMachineID != "m-new" {
		t.Errorf("the placement must still be saved: pin = %q", got.DesiredMachineID)
	}
	if got.DesiredState != DesiredStateOffline {
		t.Errorf("desired_state must stay offline, got %q", got.DesiredState)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonHeldDown+":") {
		t.Errorf("want a %s receipt explaining the no-op, got %q",
			spawnReasonHeldDown, got.LastOpReason)
	}
}

// TestRestartWorker_NoKillTarget_StillAttemptsStart (owner ruling: fix the whole
// class, one shared path): 重啟 is the verb whose entire intent is "be running",
// yet it discarded respawnWorkerNow's bool exactly like relocate did — so in the
// no-kill-target shape it wrote a receipt that LOOKED caught while dispatching
// nothing. It now rides the same shared path and genuinely attempts the start.
func TestRestartWorker_NoKillTarget_StillAttemptsStart(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, claimed once, SSE gone
	api.hub.DrainWardenCommands(ServerSelfHost)
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	w.DesiredState = DesiredStateOffline // restart 409s unless the worker is stopped
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stop worker: %v", err)
	}

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}

	if rpc, args := oneFrame(t, api, ServerSelfHost); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Errorf("restart must actually attempt the start: %s %v", rpc, args)
	}
	// Restart flips desired_state online FIRST, so it can never reach the shared
	// path's held-down arm — the intent lives in the state, not in a per-entry copy.
	got, _ := api.dal.GetOutsourceWorker(workerID)
	if got.DesiredState != DesiredStateOnline {
		t.Errorf("restart must set desired_state online, got %q", got.DesiredState)
	}
	if strings.HasPrefix(got.LastOpReason, spawnReasonHeldDown+":") {
		t.Errorf("restart must not report itself held down: %q", got.LastOpReason)
	}
}

// TestSetWorkerModel_StoppedWorkerNotRevived is the third entry on the shared
// path. The model handler used to re-ask "is this worker stopped?" itself and
// skip in silence; the shared path owns that one branch point now, so the answer
// is identical but the row explains it.
func TestSetWorkerModel_StoppedWorkerNotRevived(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, true) // active AND holding a live SSE
	api.hub.DrainWardenCommands(ServerSelfHost)
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	w.DesiredState = DesiredStateOffline // 停止 in flight, session still connected
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stop worker: %v", err)
	}

	rec := postWorker(t, api, workerID, "model", map[string]any{"model": "opus"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}

	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("a model change must not revive a stopped worker, got %d frames", got)
	}
	got, _ := api.dal.GetOutsourceWorker(workerID)
	if got.Model != "opus" {
		t.Errorf("the model change must still be saved, got %q", got.Model)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonHeldDown+":") {
		t.Errorf("want a %s receipt explaining the no-op, got %q",
			spawnReasonHeldDown, got.LastOpReason)
	}
}

// TestRelocateMintedOfflineWorker (T-e0e3 regression — the X-46 report): a worker
// already MINTED and CLAIMED (status active) whose session then died, with the
// server's spawn memory gone too (re-exec), relocated onto a concrete online
// machine. respawnWorkerNow's O-28 deferral used to swallow this shape whole: the
// pin landed, NOTHING was dispatched, and last_op/last_op_reason stayed BLANK —
// the cockpit showed 尚未分配機器 with nothing to diagnose. An owner relocate onto
// a concrete machine must actually ATTEMPT the start.
func TestRelocateMintedOfflineWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // no cadence tick may heal this — the handler must
	workerID := newActiveWorker(t, api, false)
	api.hub.DrainWardenCommands(ServerSelfHost)
	delete(api.workerSpawnTarget, workerID) // server re-exec forgot the dispatch

	seedMachine(t, api, "m-new")
	connectWarden(t, api, "m-new")

	relocateOK(t, api, workerID, "m-new")

	// The start went out, onto the machine the owner named and nowhere else.
	if rpc, args := oneFrame(t, api, "m-new"); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Errorf("pinned host frame = %s %v, want start for %s", rpc, args, workerID)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("nothing may be dispatched to the machine the owner did not pick, got %d", got)
	}
	// The dispatch is observable, so the cockpit's machine cell fills in.
	if got := api.workerSpawnTarget[workerID]; got != "m-new" {
		t.Errorf("spawn target = %q, want m-new (a blank cell is the X-46 symptom)", got)
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil || w.DesiredMachineID != "m-new" {
		t.Fatalf("pin must be durable: %+v (%v)", w, err)
	}
	if w.Status != WorkerStatusActive {
		t.Errorf("a relocate is a placement change, not a state change: status = %q", w.Status)
	}
}

// TestRelocateMintedOfflineWorker_UnreachablePinLeavesReceipt is the same shape
// with the pinned machine OFFLINE: placement is still an explicit decision, so
// nothing may boot — but the refusal must be DIAGNOSABLE on the row rather than
// leaving the owner with the blank X-46 hit. Nothing is substituted for the
// machine the owner chose.
func TestRelocateMintedOfflineWorker_UnreachablePinLeavesReceipt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	api.hub.DrainWardenCommands(ServerSelfHost)
	delete(api.workerSpawnTarget, workerID)

	seedMachine(t, api, "m-dark") // a real machine, never connected

	relocateOK(t, api, workerID, "m-dark")

	if got := len(api.hub.DrainWardenCommands("m-dark")); got != 0 {
		t.Errorf("an offline pin must not be dispatched to, got %d frames", got)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("no other machine may be substituted for the owner's pin, got %d frames", got)
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if w.LastOp != reconcileCmdStart || w.LastOpAt == 0 ||
		!strings.HasPrefix(w.LastOpReason, placementReasonUnavailable+":") {
		t.Fatalf("a refused relocate must name its cause, got last_op=%q at=%v reason=%q",
			w.LastOp, w.LastOpAt, w.LastOpReason)
	}
	if !strings.Contains(w.LastOpReason, "m-dark") {
		t.Errorf("the receipt must name the machine that refused: %q", w.LastOpReason)
	}
}

// TestRelocateMintedOfflineWorker_BlankMachineRefusedAndStartsNothing replaces
// the old _ClearedPinNeverStarts. Its whole premise — relocate with "" clears
// the pin, leaving the worker with no placement — was DELETED by the owner
// ruling of 2026-07-27 (搬遷一定要帶機器), so the state it asserted about is
// unreachable through this route and re-asserting it would pin a contract the
// server no longer has.
//
// What survives is the half that still means something, and it is the half the
// original was really guarding: an owner action that names no destination must
// not put the worker anywhere. That used to be "the pin is cleared and the
// scheduler refuses"; it is now "the request itself is refused". BOTH are
// checked, because a 400 that had already half-executed would be the worse bug:
// nothing dispatched, and the pin the request never named is still intact.
// (The 「no placement ⇒ no start, with a no_machine_selected receipt」 invariant
// itself is unaffected and still pinned at its own source —
// worker_spawn_test.go TestNotifyWorkerSpawn_StampsNoMachineSelectedReason and
// reconcile_test.go's member twin.)
func TestRelocateMintedOfflineWorker_BlankMachineRefusedAndStartsNothing(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	api.hub.DrainWardenCommands(ServerSelfHost)
	delete(api.workerSpawnTarget, workerID)
	pinned := readWorker(t, api, workerID).DesiredMachineID
	// An online, eligible machine exists — nothing may drift onto it unasked.
	seedMachine(t, api, "m-idle")
	connectWarden(t, api, "m-idle")

	rec := postWorker(t, api, workerID, "relocate", map[string]any{"machine_id": ""},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a relocate naming no machine must 400, got %d %s", rec.Code, rec.Body.String())
	}
	for _, target := range []string{"m-idle", ServerSelfHost} {
		if got := len(api.hub.DrainWardenCommands(target)); got != 0 {
			t.Errorf("a refused relocate must start nothing (%s got %d frames)", target, got)
		}
	}
	if got := readWorker(t, api, workerID).DesiredMachineID; got != pinned {
		t.Errorf("a refused relocate must not touch the pin: %q, want %q", got, pinned)
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
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop || args["member_id"] != workerID {
		t.Errorf("frame[0] = %s %v, want worker_stop for %s", rpc, args, workerID)
	}
	if rpc, args := decodeWardenFrame(t, frames[1].Frame); rpc != reconcileCmdStart || args["member_id"] != workerID {
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

// TestListOutsourceWorkers_MachineSurvivesReexec (T-c23a): the cockpit machine
// cell must survive a server re-exec. The spawn observation (workerSpawnTarget)
// is in-memory since the P7d fold: a restart forgets it, and a HEALTHY live
// worker is never re-dispatched, so the cell read 「尚未分配」 forever while the
// session kept working. The projection now falls back to the restart-proof
// observed host: live SSE machine claim first, then the worker's self-reported
// telemetry `machine` — the same precedence the member observedHost fold trusts.
func TestListOutsourceWorkers_MachineSurvivesReexec(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	if err := api.dal.PutMachineAlias(MachineAlias{
		MachineID: "mach-1", DisplayName: "MBP 5"}); err != nil {
		t.Fatalf("put alias: %v", err)
	}

	// Spawn 後: the in-memory dispatch observation drives the cell.
	api.outsourceMu.Lock()
	api.workerSpawnTarget[workerID] = "mach-1"
	api.outsourceMu.Unlock()
	if rows := listWorkersAs(t, api, wireOwnerID); rows[0].Machine != "MBP 5" {
		t.Fatalf("after spawn: machine = %q, want MBP 5", rows[0].Machine)
	}

	// Simulated re-exec: the in-memory spawn maps are reborn EMPTY (restart
	// amnesia — the exact P7d posture), while the worker's session lives on.
	api.outsourceMu.Lock()
	api.workerSpawnTarget = map[string]string{}
	api.workerSpawnAt = map[string]float64{}
	api.outsourceMu.Unlock()

	// The live worker reconnects its SSE carrying the machine_id token claim —
	// the restart-proof ground truth. The cell must keep its value.
	l, err := api.hub.Connect(workerID, "mach-1")
	if err != nil {
		t.Fatalf("connect worker listener: %v", err)
	}
	if rows := listWorkersAs(t, api, wireOwnerID); rows[0].Machine != "MBP 5" {
		t.Fatalf("after re-exec + SSE reconnect: machine = %q, want MBP 5 (尚未分配 regression)",
			rows[0].Machine)
	}

	// SSE gone but telemetry still remembers the host → second-rung fallback.
	api.hub.Disconnect(l)
	api.telemetry.Set(workerID, map[string]any{"machine": "mach-1"})
	if rows := listWorkersAs(t, api, wireOwnerID); rows[0].Machine != "MBP 5" {
		t.Fatalf("after re-exec, telemetry fallback: machine = %q, want MBP 5", rows[0].Machine)
	}

	// A REAL dispatch this server run outranks both fallbacks — the cell shows
	// where the server actually sent the last start.
	api.outsourceMu.Lock()
	api.workerSpawnTarget[workerID] = "mach-2"
	api.outsourceMu.Unlock()
	if rows := listWorkersAs(t, api, wireOwnerID); rows[0].Machine != "mach-2" {
		t.Fatalf("dispatch memory must outrank fallbacks: machine = %q, want mach-2",
			rows[0].Machine)
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

func TestOutsourceWorker_RuntimeAccountNeverBorrowsAnotherRuntime(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api) // default runtime is Claude
	if err := api.dal.PutAccountAlias(AccountAlias{
		Account: "codex:8906abc", DisplayName: "EvaChatGPT",
	}); err != nil {
		t.Fatalf("put alias: %v", err)
	}
	api.telemetry.Set(workerID, map[string]any{
		"account": "codex:8906abc", accountRuntimeKey: RuntimeCodex,
		"account_label": "ChatGPT", "ts": 1.0,
	})

	// Exercise BOTH HTTP projections bound by the external-worker UI.
	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Account != nil {
		t.Fatalf("list must not attribute Codex account to Claude worker: %+v", rows)
	}
	detail := getWorkerAs(t, api, wireOwnerID, workerID)
	if detail.Account != nil {
		t.Fatalf("detail must not attribute Codex account to Claude worker: %q", *detail.Account)
	}
}

// TestGetOutsourceWorker_AccountResolvedOnDetailPath (T-f190fix): the owner-
// reported bug lived on the DETAIL page, so the single-worker GET — not just the
// list — must route the Claude account through the shared resolveAccountDisplay
// fold. Its raw telemetry key is the real `<accountUuid>/<organizationUuid>` shape
// (readClaudeAccount, cli/ocagent/contextreport.go). This locks
// the detail path so a regression that stopped resolving ONLY the single GET
// (which every list-only account test would still pass) can never re-expose it.
func TestGetOutsourceWorker_AccountResolvedOnDetailPath(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // manual tick — deterministic single worker
	workerID := assignOneWorker(t, api)

	// The warden reports a session's account as readClaudeAccount's
	// `<accountUuid>/<organizationUuid>` composite. We use an OBVIOUSLY-synthetic
	// stand-in carrying the repo's `-raw-key` marker (matching the sibling
	// list tests) — a real 64-hex literal trips the CI gitleaks generic-api-key
	// gate, and the test only needs the composite raw SHAPE, not real entropy.
	// With NO alias and no reported label it is UNRESOLVABLE, so the detail DTO
	// must serve null → the panel's honest dash, NEVER this raw string.
	const rawKey = "5e163893-user-raw-key/0cea9af2-org-raw-key"
	api.telemetry.Set(workerID, map[string]any{"account": rawKey, accountRuntimeKey: RuntimeClaude, "cost": 1.0})

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
		RefocusOp:        memberOpRelocate,
		DesiredState:     "online",
		BankedCost:       3.25,
		// The reported twins are deliberately DIFFERENT from the configured
		// values above: a golden where they matched would still pass if the
		// builder served the configured field by mistake (T-7f28).
		ActualModel:   "claude-opus-5",
		ActualRuntime: RuntimeCodex,
		ActualEffort:  "medium",
		LastMachineID: "mac-1",
	}
	// TypeKey + CreatedTS are load-bearing for the T-a3e4 task-join fields
	// (task_no / task_created_ts / task_type_key / task_type_name): the panel's
	// sort key and row labels ride the DTO now, so a golden that left them zero
	// would still pass if the builder dropped the join entirely.
	fullTask := &Task{ID: "t-1", Title: "review 1", Status: "in_progress",
		CreatorID: "m-9", TypeKey: "tm-review", CreatedTS: 900.0}
	fullProjection := outsourceWorkerProjection{
		unread: 4,
		now:    2000.0,
		online: true,
		// The deadline on the wire now comes from the SAME grace the tick
		// collects on (T-fe5e), so the golden needs the real config — a zero
		// config would quietly report the stamp itself as the deadline.
		cfg:            defaultReconcileConfig(),
		tele:           map[string]any{"account": "raw-key-1", accountRuntimeKey: RuntimeClaude, "cost": 1.5},
		gaugeEntry:     map[string]any{"context_pct": 42.0},
		spawnTarget:    "mac-1",
		machineDisplay: func(id string) string { return "Mac Studio (" + id + ")" },
		accountDisplay: func(raw string) string { return "alice@example.com" },
		delegatedBy:    "Bob",
		typeDisplay:    func(key string) string { return "程式碼審查 (" + key + ")" },
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
			want: `{"id":"ow-1","avatar_url":"","codename":"O-7","runtime":"claude","model":"claude-sonnet-4-5","effort":"high","actual_model":"claude-opus-5","actual_runtime":"codex","actual_effort":"medium","status":"active","task_id":"t-1","task_title":"review 1","task_status":"in_progress","task_no":"T-1","task_created_ts":900,"task_type_key":"tm-review","task_type_name":"程式碼審查 (tm-review)","created_ts":1000,"unread_count":4,"presence":"online","machine":"Mac Studio (mac-1)","desired_machine_id":"mac-2","actual_machine":"mac-1","account":"alice@example.com","context_pct":42,"cost":1.5,"banked_cost":3.25,"last_op":"worker_start","last_op_ok":true,"last_op_log":"spawned ok","last_op_reason":"","last_op_at":1501,"creator_id":"m-9","delegated_by":"Bob","refocus_since":1600,"refocus_op":"relocate","refocus_deadline":1720,"desired_state":"online"}`,
		},
		{
			name: "bare row honest empties",
			w: OutsourceWorker{ID: "ow-2", Codename: "O-8",
				Model: "claude-haiku-4-5", TaskID: "t-2",
				Status: WorkerStatusAssigned, CreatedTS: 1999.0},
			task: nil, p: outsourceWorkerProjection{now: 2000.0},
			want: `{"id":"ow-2","avatar_url":"","codename":"O-8","runtime":"claude","model":"claude-haiku-4-5","effort":"","actual_model":"","actual_runtime":"","actual_effort":"","status":"assigned","task_id":"t-2","task_title":"","task_status":"","task_no":"","task_created_ts":0,"task_type_key":"","task_type_name":"","created_ts":1999,"unread_count":0,"presence":"waking","machine":"","desired_machine_id":"","actual_machine":"","account":null,"context_pct":null,"cost":null,"banked_cost":null,"last_op":"","last_op_ok":null,"last_op_log":"","last_op_reason":"","last_op_at":0,"creator_id":"","delegated_by":"","refocus_since":0,"refocus_op":"","refocus_deadline":0,"desired_state":""}`,
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
		f := foldActorRuntime(nil, nil, 0, RuntimeClaude)
		if f.account != "" || f.cost != nil || f.contextPct != nil || f.bankedCost != nil {
			t.Fatalf("empty fold = %+v, want all zero", f)
		}
	})
	t.Run("reported facts fold through", func(t *testing.T) {
		f := foldActorRuntime(
			map[string]any{"account": "raw-key-1", accountRuntimeKey: RuntimeClaude, "cost": 2.5},
			map[string]any{"context_pct": 37.0}, 1.25, RuntimeClaude)
		if f.account != "raw-key-1" {
			t.Errorf("legacy non-Codex account = %q, want raw-key-1", f.account)
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
			map[string]any{"context_pct": "high"}, 0, RuntimeClaude)
		if f.account != "" || f.cost != nil || f.contextPct != nil || f.bankedCost != nil {
			t.Fatalf("mistyped fold = %+v, want all zero", f)
		}
	})
	t.Run("account provenance blocks a foreign runtime but keeps its own", func(t *testing.T) {
		tele := map[string]any{"account": "codex:8906abc", accountRuntimeKey: RuntimeCodex}
		if got := foldActorRuntime(tele, nil, 0, RuntimeClaude).account; got != "" {
			t.Fatalf("claude account = %q, want empty for codex provenance", got)
		}
		if got := foldActorRuntime(tele, nil, 0, RuntimeCodex).account; got != "codex:8906abc" {
			t.Fatalf("codex account = %q, want its own key", got)
		}
	})
	t.Run("unproven legacy account is fail-closed for both runtimes", func(t *testing.T) {
		tele := map[string]any{"account": "legacy-key"}
		for _, runtime := range []string{RuntimeClaude, RuntimeCodex} {
			if got := foldActorRuntime(tele, nil, 0, runtime).account; got != "" {
				t.Fatalf("%s legacy account = %q, want empty", runtime, got)
			}
		}
	})
	t.Run("the entry's mutable runtime field is NOT provenance", func(t *testing.T) {
		// `runtime` is rewritten by every later heartbeat, so an unstamped
		// account must never be admitted by matching against it — that is the
		// silent degrade into "whichever runtime reported last".
		tele := map[string]any{"account": "unstamped-key", "runtime": RuntimeClaude}
		if got := foldActorRuntime(tele, nil, 0, RuntimeClaude).account; got != "" {
			t.Fatalf("unstamped account admitted via the mutable runtime field: %q", got)
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

// TestRelocateOutsourceWorker_MachineIsMandatory is the worker twin of
// TestRelocateMember_MachineIsMandatory (owner 2026-07-27: 搬遷一定要帶機器).
// Both faces are pinned because they are two handlers, and a ruling enforced on
// only one of them is the shape this repo has already been bitten by.
func TestRelocateOutsourceWorker_MachineIsMandatory(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	seedMachine(t, api, "m-far")
	connectWarden(t, api, "m-far")
	api.hub.DrainWardenCommands(ServerSelfHost)
	pinned := readWorker(t, api, workerID).DesiredMachineID
	if pinned == "" {
		t.Fatal("fixture: this test needs a pin that could be destroyed")
	}

	for _, tc := range []struct {
		name string
		body any
		want int
	}{
		{"blank", map[string]any{"machine_id": ""}, http.StatusBadRequest},
		{"null", map[string]any{"machine_id": nil}, http.StatusBadRequest},
		{"absent", map[string]any{}, http.StatusUnprocessableEntity},
	} {
		rec := httptest.NewRecorder()
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(rec,
			taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/relocate",
				tc.body, wireOwnerID, "owner"), workerID)
		if rec.Code != tc.want {
			t.Fatalf("%s machine_id: got %d %s, want %d",
				tc.name, rec.Code, rec.Body.String(), tc.want)
		}
		if got := readWorker(t, api, workerID).DesiredMachineID; got != pinned {
			t.Fatalf("%s machine_id must leave the pin alone: %q", tc.name, got)
		}
	}

	// SENTINEL: a named machine still moves the worker.
	rec := postWorker(t, api, workerID, "relocate", map[string]any{"machine_id": "m-far"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("a named machine must still relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got := readWorker(t, api, workerID).DesiredMachineID; got != "m-far" {
		t.Fatalf("the relocate must land: %q", got)
	}
}
