package main

// outsource_sched_test.go — the M3 Phase 2 scheduler pins. The pure admission
// core (outsourceDecide) carries the contract: queue order (priority then
// created_ts), the per-type copies cap, the global cap (0 = paused), frozen
// skipped, and same-call idempotence. The tick-level tests pin the IO shell:
// mint/bind/fan against a real store, cross-tick idempotence (the worker row
// IS the ledger), the create_task event seam, and the --no-outsource gate.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// ── pure decision core ────────────────────────────────────────────────────────

func specsOneType(copies int) map[string]outsourceTypeSpec {
	return map[string]outsourceTypeSpec{
		"review-pr": {Copies: copies, Model: "claude-sonnet-4-5", Effort: "medium"},
	}
}

func TestOutsourceDecideQueueOrder(t *testing.T) {
	// Deliberately shuffled input: order must come from priority then
	// created_ts (id is only the deterministic tie-break).
	cands := []outsourceCandidate{
		{TaskID: "t-low", TypeKey: "review-pr", Priority: TaskPriorityLow, CreatedTS: 1.0},
		{TaskID: "t-mid-late", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 9.0},
		{TaskID: "t-b", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 3.0},
		{TaskID: "t-high", TypeKey: "review-pr", Priority: TaskPriorityHigh, CreatedTS: 5.0},
		{TaskID: "t-mid-early", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
		{TaskID: "t-a", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 3.0},
	}
	got := outsourceDecide(cands, specsOneType(10), map[string]int{}, 0, 10)
	want := []string{"t-high", "t-mid-early", "t-a", "t-b", "t-mid-late", "t-low"}
	if len(got) != len(want) {
		t.Fatalf("want %d assignments, got %+v", len(want), got)
	}
	for i, id := range want {
		if got[i].TaskID != id {
			t.Fatalf("queue order: want %v, got %+v", want, got)
		}
	}
}

func TestOutsourceDecidePerTypeCopiesCap(t *testing.T) {
	cands := []outsourceCandidate{
		{TaskID: "t-1", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 1.0},
		{TaskID: "t-2", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
		{TaskID: "t-3", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 3.0},
	}
	// copies=2 with one already live → exactly ONE more admits.
	got := outsourceDecide(cands, specsOneType(2),
		map[string]int{"review-pr": 1}, 1, 10)
	if len(got) != 1 || got[0].TaskID != "t-1" {
		t.Fatalf("copies cap: want [t-1], got %+v", got)
	}
	// The type cap never blocks ANOTHER type.
	specs := specsOneType(1)
	specs["sync-jira"] = outsourceTypeSpec{Copies: 1, Model: "claude-haiku-4", Effort: "low"}
	got = outsourceDecide(append(cands,
		outsourceCandidate{TaskID: "t-4", TypeKey: "sync-jira",
			Priority: TaskPriorityMid, CreatedTS: 4.0}),
		specs, map[string]int{"review-pr": 1}, 1, 10)
	if len(got) != 1 || got[0].TaskID != "t-4" {
		t.Fatalf("per-type isolation: want [t-4], got %+v", got)
	}
}

func TestOutsourceDecideGlobalCap(t *testing.T) {
	cands := []outsourceCandidate{
		{TaskID: "t-1", TypeKey: "review-pr", Priority: TaskPriorityHigh, CreatedTS: 1.0},
		{TaskID: "t-2", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
		{TaskID: "t-3", TypeKey: "review-pr", Priority: TaskPriorityLow, CreatedTS: 3.0},
	}
	// cap 3, one live → two slots, best-priority first.
	got := outsourceDecide(cands, specsOneType(10), map[string]int{}, 1, 3)
	if len(got) != 2 || got[0].TaskID != "t-1" || got[1].TaskID != "t-2" {
		t.Fatalf("global cap: want [t-1 t-2], got %+v", got)
	}
	// Already at cap → nothing.
	if got := outsourceDecide(cands, specsOneType(10), map[string]int{}, 3, 3); len(got) != 0 {
		t.Fatalf("at cap: want none, got %+v", got)
	}
}

func TestOutsourceDecideZeroCapPausesAssignment(t *testing.T) {
	cands := []outsourceCandidate{
		{TaskID: "t-1", TypeKey: "review-pr", Priority: TaskPriorityHigh, CreatedTS: 1.0},
	}
	if got := outsourceDecide(cands, specsOneType(5), map[string]int{}, 0, 0); len(got) != 0 {
		t.Fatalf("cap 0 must pause assignment, got %+v", got)
	}
}

func TestOutsourceDecideUnlimitedGlobalCap(t *testing.T) {
	// globalCap < 0 = 無限 (spec SettingsDTO: -1) — every eligible candidate
	// admits regardless of the live total.
	cands := []outsourceCandidate{
		{TaskID: "t-1", TypeKey: "review-pr", Priority: TaskPriorityHigh, CreatedTS: 1.0},
		{TaskID: "t-2", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
		{TaskID: "t-3", TypeKey: "review-pr", Priority: TaskPriorityLow, CreatedTS: 3.0},
	}
	got := outsourceDecide(cands, specsOneType(10), map[string]int{}, 99, -1)
	if len(got) != 3 {
		t.Fatalf("unlimited global cap: want all 3, got %+v", got)
	}
}

func TestOutsourceDecideUnlimitedPerTypeCopies(t *testing.T) {
	// Copies == 0 = 無限 (spec TaskManualDTO) — the per-type cap never gates;
	// only the global cap can stop the fold.
	cands := []outsourceCandidate{
		{TaskID: "t-1", TypeKey: "review-pr", Priority: TaskPriorityHigh, CreatedTS: 1.0},
		{TaskID: "t-2", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
		{TaskID: "t-3", TypeKey: "review-pr", Priority: TaskPriorityLow, CreatedTS: 3.0},
	}
	got := outsourceDecide(cands, specsOneType(0),
		map[string]int{"review-pr": 7}, 7, 20)
	if len(got) != 3 {
		t.Fatalf("copies=0 unlimited: want all 3, got %+v", got)
	}
	// …and the GLOBAL cap still applies over an unlimited type.
	got = outsourceDecide(cands, specsOneType(0), map[string]int{}, 1, 3)
	if len(got) != 2 {
		t.Fatalf("copies=0 under global cap 3 with 1 live: want 2, got %+v", got)
	}
}

func TestOutsourceDecideSkipsFrozenAndSpeclessTypes(t *testing.T) {
	cands := []outsourceCandidate{
		{TaskID: "t-frozen", TypeKey: "review-pr", Priority: TaskPriorityFrozen, CreatedTS: 1.0},
		{TaskID: "t-orphan", TypeKey: "no-such-type", Priority: TaskPriorityHigh, CreatedTS: 2.0},
		{TaskID: "t-ok", TypeKey: "review-pr", Priority: TaskPriorityLow, CreatedTS: 3.0},
	}
	got := outsourceDecide(cands, specsOneType(5), map[string]int{}, 0, 10)
	if len(got) != 1 || got[0].TaskID != "t-ok" {
		t.Fatalf("frozen/spec-less skip: want [t-ok], got %+v", got)
	}
}

func TestOutsourceDecideIsIdempotentWithinOneCall(t *testing.T) {
	// The same task id queued twice admits ONCE, and each admission folds into
	// the running counts — a copies=1 type never double-assigns in one call.
	cands := []outsourceCandidate{
		{TaskID: "t-dup", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 1.0},
		{TaskID: "t-dup", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 1.0},
		{TaskID: "t-2", TypeKey: "review-pr", Priority: TaskPriorityMid, CreatedTS: 2.0},
	}
	got := outsourceDecide(cands, specsOneType(1), map[string]int{}, 0, 10)
	if len(got) != 1 || got[0].TaskID != "t-dup" {
		t.Fatalf("same-call idempotence: want [t-dup], got %+v", got)
	}
	// The fold never mutates the caller's live counts.
	live := map[string]int{"review-pr": 0}
	outsourceDecide(cands, specsOneType(5), live, 0, 10)
	if live["review-pr"] != 0 {
		t.Fatalf("liveByType mutated by decide: %v", live)
	}
}

func TestOutsourceSpecOfParsesTheManualAssignee(t *testing.T) {
	// Full spec.
	spec := outsourceSpecOf(TaskManual{Assignee: `{"kind":"outsource",` +
		`"model":"claude-opus-4-6","effort":"high","copies":3}`})
	if spec == nil || spec.Model != "claude-opus-4-6" || spec.Effort != "high" ||
		spec.Copies != 3 {
		t.Fatalf("full spec: got %+v", spec)
	}
	// copies absent → 1; effort absent → medium; machine absent → "auto".
	spec = outsourceSpecOf(TaskManual{Assignee: `{"kind":"outsource","model":"m"}`})
	if spec == nil || spec.Copies != 1 || spec.Effort != "medium" ||
		spec.Machine != "auto" {
		t.Fatalf("defaults: got %+v", spec)
	}
	// copies 0 = 無限 and an explicit machine id both ride through verbatim.
	spec = outsourceSpecOf(TaskManual{Assignee: `{"kind":"outsource",` +
		`"model":"m","copies":0,"machine":"warden-mbp5"}`})
	if spec == nil || spec.Copies != 0 || spec.Machine != "warden-mbp5" {
		t.Fatalf("unlimited copies + machine: got %+v", spec)
	}
	// Member assignee / unset / junk → nil (never an outsource spec).
	for _, blob := range []string{
		`{"kind":"member","member_id":"m-1"}`, `{}`, ``, `not json`,
	} {
		if got := outsourceSpecOf(TaskManual{Assignee: blob}); got != nil {
			t.Fatalf("assignee %q must yield nil, got %+v", blob, got)
		}
	}
}

// ── tick-level (IO shell against a real store) ───────────────────────────────

// putOutsourceManual stores one outsource-assignee manual straight through
// the DAL (governance is not under test here).
func putOutsourceManual(t *testing.T, api *apiServer, typeKey, model string, copies int) {
	t.Helper()
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey: typeKey,
		Fields:  "[]",
		Assignee: `{"kind":"outsource","model":"` + model + `",` +
			`"effort":"high","copies":` + strconv.Itoa(copies) + `}`,
	}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
}

// createOutsourceTask creates one typed task via the handler (the manual's
// assignee makes it outsource-tracked) and returns the created view.
func createOutsourceTask(t *testing.T, api *apiServer, typeKey, title string) taskDTO {
	t.Helper()
	// The typed-outsource fixture's creator (m-front) is a standing APPROVER so
	// the single spawn gate admits and the scheduler mint/bind/cap path is
	// exercised; the gate's own deny/pending verdicts are pinned separately with
	// non-approver creators. Seed only if absent so a test that pre-seeds m-front
	// (for its delegated_by name) keeps its own row.
	if m, _ := api.dal.GetMember("m-front"); m == nil {
		if err := api.dal.PutMember(Member{
			ID: "m-front", Name: "小前", Kind: "assistant", RoleKey: adminRoleKey,
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed approver creator: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": title, "type_key": typeKey}, "m-front", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create outsource task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskCreateResultDTO](t, rec).Task
}

func TestOutsourceTickAssignsMintsAndBinds(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // manual ticks only — the event seam is pinned below
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 2)
	task := createOutsourceTask(t, api, "review-pr", "review 1")
	if task.ExecutorKind != TaskExecutorOutsource || task.ExecutorID != "" {
		t.Fatalf("created task must await assignment: %+v", task)
	}

	api.runOutsourceTick(1000.0)

	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if bound.ExecutorID == "" {
		t.Fatalf("task not assigned: %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil {
		t.Fatalf("worker row missing for %q: %v", bound.ExecutorID, err)
	}
	if worker.Status != WorkerStatusAssigned || worker.TaskID != task.ID {
		t.Fatalf("worker must be assigned to the task: %+v", worker)
	}
	if worker.Codename != "S-1" || worker.Model != "claude-sonnet-4-5" ||
		worker.Effort != "high" {
		t.Fatalf("worker template must come from the manual assignee: %+v", worker)
	}

	// Cross-tick idempotence: the worker row IS the ledger — a second tick
	// finds no unassigned candidate and mints nothing.
	api.runOutsourceTick(1001.0)
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("second tick must not double-assign: %d workers", len(workers))
	}
}

func TestOutsourceTickHonoursBothCaps(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1) // copies=1
	putOutsourceManual(t, api, "sync-jira", "claude-haiku-4", 5)
	r1 := createOutsourceTask(t, api, "review-pr", "review 1")
	r2 := createOutsourceTask(t, api, "review-pr", "review 2")
	j1 := createOutsourceTask(t, api, "sync-jira", "sync 1")
	j2 := createOutsourceTask(t, api, "sync-jira", "sync 2")
	api.outsourceMaxParallel = 2 // global cap under the summed type caps

	api.runOutsourceTick(1000.0)

	assigned := func(id string) bool {
		task, err := api.dal.GetTask(id)
		if err != nil || task == nil {
			t.Fatalf("re-read %s: %v", id, err)
		}
		return task.ExecutorID != ""
	}
	// FIFO: r1 takes review-pr's single copy, j1 the second global slot;
	// r2 (type-capped) and j2 (global-capped) stay queued.
	if !assigned(r1.ID) || !assigned(j1.ID) {
		t.Fatalf("r1/j1 must be assigned")
	}
	if assigned(r2.ID) || assigned(j2.ID) {
		t.Fatalf("r2/j2 must remain queued (type/global caps)")
	}

	// Cap 0 pauses assignment outright.
	api.outsourceMaxParallel = 0
	api.runOutsourceTick(1001.0)
	if assigned(r2.ID) || assigned(j2.ID) {
		t.Fatalf("cap 0 must pause assignment")
	}

	// Raising the cap admits the backlog on the next tick — but review-pr's
	// copies=1 still holds while r1's worker is live.
	api.outsourceMaxParallel = 10
	api.runOutsourceTick(1002.0)
	if assigned(r2.ID) {
		t.Fatalf("r2 must stay queued behind the copies=1 live worker")
	}
	if !assigned(j2.ID) {
		t.Fatalf("j2 must be assigned once the global cap lifts")
	}
}

func TestOutsourceTickSkipsFrozenTasks(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 5)
	task := createOutsourceTask(t, api, "review-pr", "review 1")
	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"priority": "frozen"},
			wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}

	api.runOutsourceTick(1000.0)
	frozen, err := api.dal.GetTask(task.ID)
	if err != nil || frozen == nil {
		t.Fatalf("re-read: %v", err)
	}
	if frozen.ExecutorID != "" {
		t.Fatalf("frozen task must never be assigned: %+v", frozen)
	}

	// Unfreeze → the next tick assigns.
	rec = httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"priority": "mid"},
			wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("unfreeze: %d %s", rec.Code, rec.Body.String())
	}
	api.runOutsourceTick(1001.0)
	thawed, err := api.dal.GetTask(task.ID)
	if err != nil || thawed == nil {
		t.Fatalf("re-read: %v", err)
	}
	if thawed.ExecutorID == "" {
		t.Fatalf("unfrozen task must be assigned on the next tick")
	}
}

func TestCreateTaskEventTickAssignsImmediately(t *testing.T) {
	// The create_task seam: an outsource create triggers the immediate tick —
	// no cadence wait. (noOutsource stays false: the seam is live.)
	api := newTasksTestServer(t)
	putOutsourceManual(t, api, "review-pr", "claude-opus-4-6", 2)
	task := createOutsourceTask(t, api, "review-pr", "review now")

	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read: %v", err)
	}
	if bound.ExecutorID == "" {
		t.Fatalf("event-driven tick must assign on create: %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil || worker.Codename != "O-1" {
		t.Fatalf("event-minted worker: %+v (err %v)", worker, err)
	}
	// A member-executed create must NOT tick — pinned implicitly by the ad-hoc
	// fixtures everywhere else (no worker rows appear).
}

func TestNoOutsourceGatesTheEventTick(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 2)
	task := createOutsourceTask(t, api, "review-pr", "review 1")

	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read: %v", err)
	}
	if bound.ExecutorID != "" {
		t.Fatalf("--no-outsource must gate the event tick: %+v", bound)
	}
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("--no-outsource must mint nothing: %d workers", len(workers))
	}
}

func TestOutsourceTickCodenamesNeverReuse(t *testing.T) {
	// A released worker's codename stays burned: the next mint of the same
	// family is MAX+1 over EVERY row ever issued (contract §A.4).
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
	first := createOutsourceTask(t, api, "review-pr", "review 1")
	api.runOutsourceTick(1000.0)

	// Owner terminates → the worker releases (Phase 1 side effect).
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, wireOwnerID, "owner"), first.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}

	second := createOutsourceTask(t, api, "review-pr", "review 2")
	api.runOutsourceTick(1001.0)
	bound, err := api.dal.GetTask(second.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("second task must assign after the release: %+v (err %v)", bound, err)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil {
		t.Fatalf("worker: %v", err)
	}
	if worker.Codename != "S-2" {
		t.Fatalf("codename must never reuse S-1: got %q", worker.Codename)
	}

	// P7d merged-storage shape: both workers are kind='outsource' MEMBER rows —
	// the released one soft-removed (roster_status) with released_ts stamped,
	// still feeding the codename fold above — while the STAFF surfaces stay
	// blind to them (ListMembers excludes kind='outsource').
	var releasedRoster string
	var releasedTS float64
	if err := api.dal.db.QueryRow(`SELECT roster_status, released_ts FROM member
		WHERE kind='outsource' AND codename='S-1'`).
		Scan(&releasedRoster, &releasedTS); err != nil {
		t.Fatalf("released worker must live in member: %v", err)
	}
	if releasedRoster != RosterStatusRemoved || releasedTS <= 0 {
		t.Fatalf("released worker member row = (%q, %v), want removed + released_ts",
			releasedRoster, releasedTS)
	}
	staff, err := api.dal.ListMembers()
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range staff {
		if m.Kind == KindOutsource {
			t.Fatalf("ListMembers must exclude outsource members, got %q", m.ID)
		}
	}
}

// putUnassignedTargetTask lands an unassigned outsource task carrying an explicit
// 發包 target directly (the create/reassign dispatch shape) — the sched fixture
// for the target-aware admission path (T-35e0), self-made, no prod row.
func putUnassignedTargetTask(t *testing.T, api *apiServer, id, typeKey, model, effort string) {
	t.Helper()
	if err := api.dal.PutTask(Task{
		ID: id, TypeKey: typeKey, Title: id, Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource, ExecutorID: "",
		OutsourceModel: model, OutsourceEffort: effort, OutsourceMachine: "auto",
		CreatorID: wireOwnerID, CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatalf("put target task %s: %v", id, err)
	}
}

// ③ the scheduler mints from a task's explicit outsource_target in preference to
// the type manual's assignee spec — an owner reassign/create dispatch overrides
// whatever the type would otherwise mint.
func TestOutsourceTickPrefersOutsourceTargetOverTypeSpec(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	// The type manual would mint sonnet/high; the task's explicit target is opus/low.
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 2)
	putUnassignedTargetTask(t, api, "t-target", "review-pr", "opus", "low")

	api.runOutsourceTick(1000.0)

	bound, _ := api.dal.GetTask("t-target")
	if bound == nil || bound.ExecutorID == "" {
		t.Fatalf("target task must be assigned: %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil {
		t.Fatalf("worker missing: %v", err)
	}
	if worker.Model != "opus" || worker.Effort != "low" {
		t.Fatalf("mint must follow the explicit target, not the type spec: %+v", worker)
	}
}

// ③b a target task whose type has NO manual assignee is still minted — the
// target carries the whole spec, so the scheduler never skips it as spec-less
// (the old type-only decide would have left it queued forever).
func TestOutsourceTickMintsTargetTaskWithNoTypeManual(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putUnassignedTargetTask(t, api, "t-adhoc", "", "sonnet", "medium")

	api.runOutsourceTick(1000.0)

	bound, _ := api.dal.GetTask("t-adhoc")
	if bound == nil || bound.ExecutorID == "" {
		t.Fatalf("a type-less target task must still be minted: %+v", bound)
	}
	worker, _ := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if worker == nil || worker.Model != "sonnet" {
		t.Fatalf("worker must carry the target model: %+v", worker)
	}
}

// ④ every 發包 path funnels through the ONE global cap — explicit dispatch
// targets included. Two target tasks under cap=1 admit exactly one; the other
// queues (no immediate-spawn side door for explicit dispatches).
func TestOutsourceTickExplicitTargetsObeyTheGlobalCap(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	api.outsourceMaxParallel = 1
	putUnassignedTargetTask(t, api, "t-a", "", "sonnet", "medium")
	putUnassignedTargetTask(t, api, "t-b", "", "sonnet", "medium")

	api.runOutsourceTick(1000.0)

	assigned := func(id string) bool {
		task, _ := api.dal.GetTask(id)
		return task != nil && task.ExecutorID != ""
	}
	// FIFO by created_ts tie-break on id: exactly one admits under cap=1.
	n := 0
	for _, id := range []string{"t-a", "t-b"} {
		if assigned(id) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("cap=1 must admit exactly one target dispatch, got %d", n)
	}

	// Lifting the cap admits the backlog — still through the scheduler, never inline.
	api.outsourceMaxParallel = 3
	api.runOutsourceTick(1001.0)
	if !assigned("t-a") || !assigned("t-b") {
		t.Fatalf("raising the cap must admit the queued target dispatch")
	}
}

// ④b the sched gate is NOT re-run for an explicit target: an owner reassign of a
// subordinate-created task lands a target whose CREATOR is not whitelisted, yet
// the successor still mints (the dispatch was authorized at the handler — a
// creator-based re-gate here would wrongly orphan it).
func TestOutsourceTickDoesNotReGateExplicitTargetByCreator(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	// A non-whitelisted subordinate creator; the default policy names nobody.
	if err := api.dal.PutMember(Member{
		ID: "m-plain", Name: "Plain", Kind: KindAssistant, RoleKey: "dev",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed creator: %v", err)
	}
	if err := api.dal.PutTask(Task{
		ID: "t-owned", Title: "owner reassigned this", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource, ExecutorID: "",
		OutsourceModel: "sonnet", OutsourceEffort: "medium", OutsourceMachine: "auto",
		CreatorID: "m-plain", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatalf("put task: %v", err)
	}

	api.runOutsourceTick(1000.0)

	bound, _ := api.dal.GetTask("t-owned")
	if bound == nil || bound.ExecutorID == "" {
		t.Fatalf("an authorized target dispatch must not be re-gated by creator: %+v", bound)
	}
}
