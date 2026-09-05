package main

// outsource_creator_snapshot_test.go — T-8a67: what a 發包 with NO explicit spec
// resolves to, and what it must never overwrite.
//
// The defect these pin: a typed task whose MANUAL routes it to outsource used to
// resolve NOTHING at create time (inheritance was gated on an explicit `target`),
// so if the manual's assignee named no machine, no source anywhere named one —
// and a worker with no placement is never booted. The task sat with a minted
// worker that could not start, for life.
//
// Three cases, per the acceptance criteria: a DEFAULTED spec snapshots the
// creator; an EXPLICIT spec (manual's or target's) is never overwritten by that
// snapshot; and with outsource capacity the spawn actually reaches the machine
// the snapshot named.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// snapshotCreator is the dispatching member every case below creates as: fully
// configured, and pinned to a machine of its own. Its fields differ from every
// manual fixture in this file, so no assertion can pass by coincidence.
func snapshotCreator(t *testing.T, api *apiServer, machine string) {
	t.Helper()
	if err := api.dal.PutMember(Member{
		ID: "m-disp", Name: "Dispatcher", Kind: KindStaff, RoleKey: "dev",
		Runtime: RuntimeClaude, Model: "opus", Effort: "low",
		DesiredMachineID: machine, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed dispatcher: %v", err)
	}
}

// seedOutsourceManual writes a type manual whose assignee routes to outsource,
// with exactly the assignee JSON given (so a test can state a spec or withhold it).
func seedOutsourceManual(t *testing.T, api *apiServer, typeKey, assignee string) {
	t.Helper()
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey: typeKey, Purpose: "p", Fields: "[]", Assignee: assignee,
	}); err != nil {
		t.Fatalf("seed manual %s: %v", typeKey, err)
	}
}

// createdTask runs a create and returns the STORED row (the durable record is
// what the scheduler and the spawn seam read — never the response DTO).
func createdTask(t *testing.T, api *apiServer, body map[string]any) *Task {
	t.Helper()
	rec := createTaskAs(t, api, body, "m-disp", "agent")
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.GetTask(createdTaskView(t, api, rec).ID)
	if err != nil || stored == nil {
		t.Fatalf("re-read task: %v", err)
	}
	return stored
}

// ── case 1: a DEFAULTED spec snapshots the creator ───────────────────────────

// TestCreateManualDrivenOutsourceSnapshotsTheCreatorsMachine: a typed 發包 whose
// manual assignee states only `kind` inherits the CREATOR's placement and model.
//
// The machine is the field that matters most: nothing else in the system invents
// one, so before T-8a67 this row stored "" and the worker minted from it could
// never boot. runtime and effort come from the manual here — its assignee schema
// DEFAULTS them (claude / medium), which is a statement, not silence — so the
// snapshot decides model and machine, the two the schema leaves genuinely unset.
func TestCreateManualDrivenOutsourceSnapshotsTheCreatorsMachine(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // admission is case 3's subject; this is the create-time row
	seedMachine(t, api, "m-creator-box")
	snapshotCreator(t, api, "m-creator-box")
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource"}`)

	got := createdTask(t, api, map[string]any{
		"title": "manual routes it, nobody named a machine", "type_key": "typed"})

	if got.ExecutorKind != TaskExecutorOutsource || got.ExecutorID != "" {
		t.Fatalf("a manual outsource assignee must land unassigned outsource: %+v", got)
	}
	if got.OutsourceMachine != "m-creator-box" {
		t.Fatalf("machine = %q, want the creator's own pin — a 發包 with no placement "+
			"named is unbootable forever", got.OutsourceMachine)
	}
	if got.OutsourceModel != "opus" {
		t.Fatalf("model = %q, want the creator's (發一個像我這樣的)", got.OutsourceModel)
	}
	// …and it is still NOT an explicit dispatch: the scheduler must keep gating it
	// by its creator and keep reading the manual live.
	if got.OutsourceDispatched {
		t.Fatalf("a manual-driven create must not read as an explicit 發包: %+v", got)
	}
}

// TestCreateAdHocOutsourceWithNoSpecSnapshotsTheCreatorWhole: the free 發包 twin —
// no manual at all, `target.kind=outsource` with no fields, so all four come from
// the creator. Unlike the typed case there is no assignee schema to default
// runtime/effort, which is why "low" (the creator's) survives here.
func TestCreateAdHocOutsourceWithNoSpecSnapshotsTheCreatorWhole(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedMachine(t, api, "m-creator-box")
	snapshotCreator(t, api, "m-creator-box")

	got := createdTask(t, api, map[string]any{
		"title":  "ad-hoc 發包, spec unstated",
		"target": map[string]any{"kind": "outsource"}})

	if got.OutsourceRuntime != RuntimeClaude || got.OutsourceModel != "opus" ||
		got.OutsourceEffort != "low" || got.OutsourceMachine != "m-creator-box" {
		t.Fatalf("an ad-hoc 發包 with no spec must inherit the creator whole: %+v", got)
	}
	if !got.OutsourceDispatched {
		t.Fatalf("an explicit target IS a dispatch: %+v", got)
	}
}

// ── case 2: an EXPLICIT spec is never overwritten ────────────────────────────

// TestCreateManualDrivenOutsourceKeepsWhatTheManualStates: every field the 手冊
// names stands — the creator snapshot fills gaps and nothing else. The 手冊 is the
// owner's configuration for the whole task type; whoever happened to create the
// task must not silently override it.
func TestCreateManualDrivenOutsourceKeepsWhatTheManualStates(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedMachine(t, api, "m-creator-box")
	seedMachine(t, api, "m-manual-box")
	snapshotCreator(t, api, "m-creator-box")
	seedOutsourceManual(t, api, "typed",
		`{"kind":"outsource","runtime":"codex","model":"gpt-5-codex","effort":"high","machine":"m-manual-box"}`)

	got := createdTask(t, api, map[string]any{
		"title": "the manual states everything", "type_key": "typed"})

	if got.OutsourceRuntime != RuntimeCodex || got.OutsourceModel != "gpt-5-codex" ||
		got.OutsourceEffort != "high" || got.OutsourceMachine != "m-manual-box" {
		t.Fatalf("the manual's own spec must survive the creator snapshot: %+v", got)
	}
}

// TestCreateDispatchTargetSurvivesTheCreatorSnapshot: an EXPLICIT target beats
// both the manual and the creator, field by field. Pinned end-to-end (not only on
// inheritDispatchSpec) because the create handler is where the target is parsed,
// stored, and now also where the snapshot pass runs.
func TestCreateDispatchTargetSurvivesTheCreatorSnapshot(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	for _, m := range []string{"m-creator-box", "m-manual-box", "m-target-box"} {
		seedMachine(t, api, m)
	}
	snapshotCreator(t, api, "m-creator-box")
	seedOutsourceManual(t, api, "typed",
		`{"kind":"outsource","runtime":"claude","model":"sonnet","effort":"high","machine":"m-manual-box"}`)

	got := createdTask(t, api, map[string]any{
		"title": "the dispatcher named it all", "type_key": "typed",
		"target": map[string]any{"kind": "outsource", "runtime": "claude",
			"model": "haiku", "effort": "medium", "machine": "m-target-box"}})

	if got.OutsourceModel != "haiku" || got.OutsourceEffort != "medium" ||
		got.OutsourceMachine != "m-target-box" {
		t.Fatalf("an explicit target must not be overwritten by manual or creator: %+v", got)
	}
	if !got.OutsourceDispatched {
		t.Fatalf("an explicit target IS a dispatch: %+v", got)
	}
}

// TestOutsourceDecideSnapshotFillsOnlyWhatTheManualOmits: the admission core's own
// half of case 2. A manual-driven candidate carries a creator snapshot; the LIVE
// manual outranks it field by field, and copies is the manual's alone (no creator
// has any say in how many of a type may run).
func TestOutsourceDecideSnapshotFillsOnlyWhatTheManualOmits(t *testing.T) {
	snap := outsourceCandidate{TaskID: "t-1", TypeKey: "typed",
		Priority: TaskPriorityMid, CreatedTS: 1.0,
		TargetRuntime: RuntimeClaude, TargetModel: "opus", TargetEffort: "low",
		TargetMachine: "m-creator-box"}

	// The manual states only what outsourceSpecOf's schema defaults it to.
	bare := map[string]outsourceTypeSpec{"typed": {
		Copies: 1, Runtime: RuntimeClaude, Effort: "medium"}}
	got := outsourceDecide([]outsourceCandidate{snap}, bare, map[string]int{}, 0, 10)
	if len(got) != 1 {
		t.Fatalf("want one admission, got %+v", got)
	}
	if got[0].Machine != "m-creator-box" || got[0].Model != "opus" {
		t.Fatalf("the snapshot must fill what the manual omits: %+v", got[0])
	}
	if got[0].Effort != "medium" || got[0].FromTarget {
		t.Fatalf("a snapshot is neither an effort nor a dispatch: %+v", got[0])
	}

	// A manual that states them wins, on the very same candidate.
	stated := map[string]outsourceTypeSpec{"typed": {Copies: 1,
		Runtime: RuntimeClaude, Model: "sonnet", Effort: "high", Machine: "m-manual-box"}}
	got = outsourceDecide([]outsourceCandidate{snap}, stated, map[string]int{}, 0, 10)
	if len(got) != 1 || got[0].Machine != "m-manual-box" || got[0].Model != "sonnet" ||
		got[0].Effort != "high" {
		t.Fatalf("the live manual must outrank the snapshot: %+v", got)
	}
}

// ── case 3: with capacity, the spawn reaches the snapshot's machine ──────────

// TestOutsourceTickSpawnsManualDrivenWorkerOnTheCreatorsMachine: the end of the
// chain. A typed 發包 with no spec anywhere, outsource capacity free, and the
// creator's own machine online → the tick mints a worker carrying the snapshot's
// model and DISPATCHES a start frame to that machine.
//
// This is the case the ticket was filed for. Before T-8a67 the mint happened and
// the dispatch did not: the worker's placement resolved to "", so it stalled with
// a no_machine_selected receipt and never came online.
func TestOutsourceTickSpawnsManualDrivenWorkerOnTheCreatorsMachine(t *testing.T) {
	api := newWorkerTestServer(t)
	api.outsourceMaxParallel = 5
	putWardenFixture(t, api, "m-creator-box")
	connectWarden(t, api, "m-creator-box")
	snapshotCreator(t, api, "m-creator-box")
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource"}`)

	// The event-driven tick inside create_task does the admission.
	task := createdTask(t, api, map[string]any{
		"title": "typed 發包, nobody named a machine", "type_key": "typed"})

	if task.ExecutorID == "" {
		t.Fatalf("free capacity must mint a worker for the task: %+v", task)
	}
	w, err := api.dal.GetOutsourceWorker(task.ExecutorID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	if w.Model != "opus" {
		t.Fatalf("the worker must be minted from the same snapshot the row stores: %+v", w)
	}
	if w.LastOpReason != "" {
		t.Fatalf("a placed spawn must leave no refusal receipt, got %q", w.LastOpReason)
	}
	rpc, args := oneFrame(t, api, "m-creator-box")
	if rpc != reconcileCmdStart {
		t.Fatalf("frame rpc = %q, want %q", rpc, reconcileCmdStart)
	}
	if args["member_id"] != w.ID {
		t.Fatalf("the start frame must address the minted worker: %+v", args)
	}
}

// TestSpawnManualDrivenSnapshotLosesToTheLiveManual: the snapshot must NOT freeze
// the manual. An owner who fixes the type's assignee placement after the task was
// created wins on the next spawn attempt — the snapshot is the weaker claim of the
// two durable task-side sources and is consulted only in the manual's silence.
//
// Without this ordering the T-8a67 snapshot would quietly re-create the very
// failure mode the columns were kept empty to avoid: a manual frozen at create
// time, editable by nobody.
func TestSpawnManualDrivenSnapshotLosesToTheLiveManual(t *testing.T) {
	api := newWorkerTestServer(t)
	api.noOutsource = true
	for _, m := range []string{"m-creator-box", "m-manual-box"} {
		putWardenFixture(t, api, m)
		connectWarden(t, api, m)
	}
	snapshotCreator(t, api, "m-creator-box")
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource"}`)

	task := createdTask(t, api, map[string]any{
		"title": "typed 發包", "type_key": "typed"})
	if task.OutsourceMachine != "m-creator-box" || task.OutsourceDispatched {
		t.Fatalf("fixture: want an undispatched creator snapshot, got %+v", task)
	}
	w := putWorkerFixture(t, api, OutsourceWorker{
		ID: "ow-live", Codename: "O-9", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredState: DesiredStateOnline,
	})

	// The owner now names a machine on the TYPE.
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource","machine":"m-manual-box"}`)

	api.outsourceMu.Lock()
	dispatched := api.notifyWorkerSpawn(w, 1_000_000.0)
	api.outsourceMu.Unlock()
	if !dispatched {
		t.Fatal("the spawn must be dispatched")
	}
	if got := api.hub.DrainWardenCommands("m-creator-box"); len(got) != 0 {
		t.Fatalf("the stale snapshot must lose to the live manual, got %d frames on "+
			"the creator's box", len(got))
	}
	if got := api.hub.DrainWardenCommands("m-manual-box"); len(got) != 1 {
		t.Fatalf("the live manual's machine must take the worker, got %d frames", len(got))
	}
}

// TestSpawnDispatchTargetOutranksTheManual: the mirror invariant — for a row that
// IS a dispatch, the task's own placement beats the manual's. Both rows carry the
// same two populated sources; only outsource_dispatched differs, so this pins that
// the arm order tracks the flag and not the fixture.
func TestSpawnDispatchTargetOutranksTheManual(t *testing.T) {
	api := newWorkerTestServer(t)
	api.noOutsource = true
	for _, m := range []string{"m-target-box", "m-manual-box"} {
		putWardenFixture(t, api, m)
		connectWarden(t, api, m)
	}
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource","machine":"m-manual-box"}`)
	task := putTaskFixture(t, api, Task{
		ID: "t-dispatched01", TypeKey: "typed", Title: "explicit 發包",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-tgt",
		OutsourceRuntime: RuntimeClaude, OutsourceModel: "opus", OutsourceEffort: "medium",
		OutsourceMachine: "m-target-box", OutsourceDispatched: true,
		CreatorID: wireOwnerID, CreatedTS: 1000, UpdatedTS: 1000,
	})
	w := putWorkerFixture(t, api, OutsourceWorker{
		ID: "ow-tgt", Codename: "O-8", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredState: DesiredStateOnline,
	})

	api.outsourceMu.Lock()
	api.notifyWorkerSpawn(w, 1_000_000.0)
	api.outsourceMu.Unlock()

	if got := api.hub.DrainWardenCommands("m-manual-box"); len(got) != 0 {
		t.Fatalf("an explicit target must outrank the manual, got %d frames on the "+
			"manual's box", len(got))
	}
	if got := api.hub.DrainWardenCommands("m-target-box"); len(got) != 1 {
		t.Fatalf("the dispatch target must take the worker, got %d frames", len(got))
	}
}

// TestCreateTypedManualDrivenCodexCreatorStillFailsClosed pins a KNOWN
// LIMITATION, not a desired behaviour (T-cd21 holds the fix): T-8a67's snapshot
// repairs CLAUDE creators only.
//
// outsourceSpecOf fills Runtime=claude / Effort=medium BEFORE reading any assignee
// key, so a typed manual's SILENCE about runtime is indistinguishable from it
// saying claude — those two of the four fields never reach the creator at all. A
// codex creator therefore gets runtime=claude with its codex model correctly
// dropped by the coupling rule, while the MACHINE arm (which has no coupling)
// still snapshots its codex box. The placement is then refused for the runtime
// the machine does not provide.
//
// This test exists so the gap stays on the record: the ticket's symptom is not
// fixed for codex creators, only re-coded from no_machine_selected to
// machine_unavailable. If someone later fixes outsourceSpecOf, this test SHOULD
// fail — and its failure is the signal to delete it along with the limitation
// notes in api_tasks.go and server/CLAUDE.md, not to re-pin it.
func TestCreateTypedManualDrivenCodexCreatorStillFailsClosed(t *testing.T) {
	api := newWorkerTestServer(t)
	api.outsourceMaxParallel = 5
	putWardenFixture(t, api, "m-codex-box")
	connectWarden(t, api, "m-codex-box")
	// The creator's own box reports CODEX only. A reported map is the warden's
	// full answer, so claude is ABSENT there — not merely unprobed.
	if rec := doIngestTelemetry(api, "m-codex-box", "m-codex-box",
		`{"runtimes":{"codex":{"installed":true,"logged_in":true}}}`); rec.Code != 200 {
		t.Fatalf("ingest telemetry: %d %s", rec.Code, rec.Body.String())
	}
	if err := api.dal.PutMember(Member{
		ID: "m-disp", Name: "Codex dev", Kind: KindStaff, RoleKey: "dev",
		Runtime: RuntimeCodex, Model: "gpt-5-codex", Effort: "high",
		DesiredMachineID: "m-codex-box", RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed codex creator: %v", err)
	}
	seedOutsourceManual(t, api, "typed", `{"kind":"outsource"}`)

	task := createdTask(t, api, map[string]any{
		"title": "codex creator, typed manual-driven", "type_key": "typed"})

	// The snapshot DID take the creator's machine — that half works…
	if task.OutsourceMachine != "m-codex-box" {
		t.Fatalf("the machine arm has no runtime coupling, so it snapshots "+
			"regardless: %+v", task)
	}
	// …and the runtime half cannot: the manual's default already claimed it, so
	// the codex model is dropped as another runtime's.
	if task.OutsourceRuntime != RuntimeClaude || task.OutsourceModel != "" {
		t.Fatalf("a typed manual defaults runtime to claude before the creator is "+
			"consulted, which drops its codex model: %+v", task)
	}
	// The consequence, stated rather than hidden: no frame, and a receipt naming
	// the runtime the machine does not provide.
	w, err := api.dal.GetOutsourceWorker(task.ExecutorID)
	if err != nil || w == nil {
		t.Fatalf("get worker: %v", err)
	}
	if !strings.HasPrefix(w.LastOpReason, placementReasonUnavailable+":") {
		t.Fatalf("last_op_reason = %q, want a %s receipt", w.LastOpReason,
			placementReasonUnavailable)
	}
	if !strings.Contains(w.LastOpReason, "does not provide the 'claude' runtime") {
		t.Fatalf("the receipt must name the missing runtime: %q", w.LastOpReason)
	}
	if got := api.hub.DrainWardenCommands("m-codex-box"); len(got) != 0 {
		t.Fatalf("nothing may be dispatched, got %d frames", len(got))
	}
}

// TestMigration00036BackfillsTheOldInference: 00036 must classify every
// PRE-EXISTING row exactly as the retired inference did — the flag DECLARES what
// "spec columns non-empty" already meant. A row that silently changed sides would
// either skip the creator gate it used to pass or lose the target it used to mint
// from, and "既有任務不納入修正範圍" only holds if their behaviour is preserved.
//
// outsource_runtime is deliberately absent from the backfill: the DAL normalizes
// it on every write, so it is never blank, and including it would mark every
// outsource task ever created as a dispatch.
func TestMigration00036BackfillsTheOldInference(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "dispatched-backfill.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// The world as 00035 left it — before the column existed.
	if err := goose.DownTo(db, "migrations", 35); err != nil {
		t.Fatalf("down to 35: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task
		(id, title, executor_kind, outsource_runtime, outsource_model,
		 outsource_effort, outsource_machine) VALUES
		('t-full',    'old dispatch, all named', 'outsource', 'claude', 'opus', 'medium', 'm-box'),
		('t-machine', 'old dispatch, machine only', 'outsource', 'claude', '', '', 'm-box'),
		('t-effort',  'old dispatch, effort only', 'outsource', 'claude', '', 'medium', ''),
		('t-manual',  'old manual-driven', 'outsource', 'claude', '', '', ''),
		('t-member',  'a member task', 'member', 'claude', '', '', '')`); err != nil {
		t.Fatalf("seed pre-00036 tasks: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("00036 up: %v", err)
	}

	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"t-full", true}, {"t-machine", true}, {"t-effort", true},
		{"t-manual", false}, {"t-member", false},
	} {
		var got int
		if err := db.QueryRow(
			`SELECT outsource_dispatched FROM task WHERE id = ?`, tc.id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", tc.id, err)
		}
		if (got != 0) != tc.want {
			t.Errorf("%s: outsource_dispatched = %d, want %v", tc.id, got, tc.want)
		}
	}
}
