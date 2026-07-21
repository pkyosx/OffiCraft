package main

// dal_tasks.go — the durable data-access layer of the M3 task system
// (migrations/00004): task / task_dep / task_step / task_manual, plus the
// outsource-worker PROJECTION over the member table (migrations/00025 folded
// the outsource_worker table into member — A案 P7d), each with exactly the
// CRUD surface its handlers serve (the dal.go convention — explicit per-table
// methods, no generic repository). SSE fan-out stays a handler concern and is
// NOT here.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ── task ─────────────────────────────────────────────────────────────────────

// Task mirrors the task table. Inputs is the free-form JSON object of the
// manual's input-field values (schema lives in the manual, like chat meta).
type Task struct {
	ID            string
	TypeKey       string
	Title         string
	DedupeKey     string
	Inputs        map[string]any
	Description   string
	Status        string // DERIVED from steps (domain.go DeriveTaskStatus); closed set
	Lock          string // '' | 'reassigning' — orthogonal system hold (domain.go TaskLock*)
	Priority      string // closed set high|mid|low|frozen
	ExecutorKind  string // "member" | "outsource"
	ExecutorID    string // '' = outsource task awaiting assignment
	CreatorID     string // verified sub of the creator; '' on pre-column rows
	WaitingReason string // non-empty only while waiting_external
	CreatedTS     float64
	UpdatedTS     float64
	ClosedTS      float64 // 0.0 = still open
	CloseoutTS    float64 // 0.0 = close-out follow-ups not reported yet (§6.3)
	// DuplicateOf is the ORIGINAL task's id this one duplicates — non-empty
	// ONLY while Status=='duplicated' (set by mark_duplicate). Depth-1 by
	// construction (see api_tasks.go HandleMarkTaskDuplicate...): the target is
	// never itself duplicated and this task is never itself an original.
	DuplicateOf string
	// ReassignedFrom / ReassignedFromKind is the PREDECESSOR the task was last
	// handed over from (T-ba04): on every reassign the server stamps the OLD
	// executor's id + kind ('member' | 'outsource') here so the new executor and
	// the cockpit can name who to hand over WITH. '' / '' on a task never
	// reassigned (or pre-column rows).
	ReassignedFrom     string
	ReassignedFromKind string
	// OutsourceModel / OutsourceEffort / OutsourceMachine is the explicit 發包
	// target (T-35e0, migrations/00029), set ONLY on a create/reassign dispatch to
	// an outsource worker: the task lands unassigned (executor_kind='outsource',
	// executor_id='') carrying its target here, and the scheduler mints from it
	// (preferred over the type manual's assignee spec). '' / '' / '' on a plain
	// manual-driven outsource task (the scheduler falls back to the type spec) and
	// on every non-outsource task.
	OutsourceModel   string
	OutsourceEffort  string
	OutsourceMachine string
	// Handoff / HandoffNote / HandoffTaskID is the DECLARED destination of the
	// ball (T-74f8, migrations/00031): '' = never declared, else one of
	// domain.go's HandoffReturnToCreator / HandoffFollowUp / HandoffNone. The
	// close gate (api_tasks.go handoffGateVerdict) refuses to let a
	// creator≠executor task close until this is set, so a finished task can no
	// longer end with nobody holding anything.
	Handoff       string
	HandoffNote   string
	HandoffTaskID string
}

const taskColumns = `id, type_key, title, dedupe_key, inputs, description,
	status, lock, priority, executor_kind, executor_id, creator_id, waiting_reason,
	created_ts, updated_ts, closed_ts, closeout_ts, duplicate_of,
	reassigned_from, reassigned_from_kind,
	outsource_model, outsource_effort, outsource_machine,
	handoff, handoff_note, handoff_task_id`

// sqlTerminalStatuses is the SQL IN-list of the terminal statuses — every
// "open task" filter (dedupe probe, resume block, open counts) excludes these.
// Kept in ONE place so a new terminal state (duplicated joined done/terminated
// in T-02c9) updates every filter at once rather than drifting per query.
const sqlTerminalStatuses = `'done', 'terminated', 'duplicated'`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var inputs string
	err := row.Scan(
		&t.ID, &t.TypeKey, &t.Title, &t.DedupeKey, &inputs, &t.Description,
		&t.Status, &t.Lock, &t.Priority, &t.ExecutorKind, &t.ExecutorID, &t.CreatorID,
		&t.WaitingReason,
		&t.CreatedTS, &t.UpdatedTS, &t.ClosedTS, &t.CloseoutTS, &t.DuplicateOf,
		&t.ReassignedFrom, &t.ReassignedFromKind,
		&t.OutsourceModel, &t.OutsourceEffort, &t.OutsourceMachine,
		&t.Handoff, &t.HandoffNote, &t.HandoffTaskID,
	)
	if err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal([]byte(inputs), &t.Inputs); err != nil {
		return Task{}, fmt.Errorf("task %s: bad inputs JSON: %w", t.ID, err)
	}
	return t, nil
}

// ListTasks returns every task, oldest→newest (filters/sorting are handler
// projections — the wire serves full DTOs, the FE partitions).
func (d *DAL) ListTasks() ([]Task, error) {
	rows, err := d.db.Query(
		`SELECT ` + taskColumns + ` FROM task ORDER BY created_ts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTask returns one task by id, or nil if absent.
func (d *DAL) GetTask(id string) (*Task, error) {
	row := d.db.QueryRow(`SELECT `+taskColumns+` FROM task WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FindOpenTaskByDedupe returns the NON-terminal task matching (typeKey,
// dedupeKey), or nil — the create_task dedupe probe (terminal tasks never
// block a reopen; kyle ruling H2). Oldest match wins for determinism.
func (d *DAL) FindOpenTaskByDedupe(typeKey, dedupeKey string) (*Task, error) {
	row := d.db.QueryRow(`
		SELECT `+taskColumns+` FROM task
		WHERE type_key = ? AND dedupe_key = ?
		  AND status NOT IN (`+sqlTerminalStatuses+`)
		ORDER BY created_ts LIMIT 1`, typeKey, dedupeKey)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListOpenTasksByExecutor returns the NON-terminal tasks a caller executes,
// most recently updated first, capped to limit — the resume-summary task
// block's query (SPEC §6.2: a handover resumes in-flight tasks; the bound
// keeps the wake snapshot small).
func (d *DAL) ListOpenTasksByExecutor(executorID string, limit int) ([]Task, error) {
	rows, err := d.db.Query(`
		SELECT `+taskColumns+` FROM task
		WHERE executor_id = ? AND status NOT IN (`+sqlTerminalStatuses+`)
		ORDER BY updated_ts DESC, created_ts DESC LIMIT ?`, executorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountOpenTasksByExecutor counts ALL the NON-terminal tasks a caller
// executes — the resume-summary overview's tasks_open_total (the light task
// rows are capped to resumeTasksN; this count tells the waking agent how many
// more list_tasks would page).
func (d *DAL) CountOpenTasksByExecutor(executorID string) (int, error) {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM task
		WHERE executor_id = ? AND status NOT IN (`+sqlTerminalStatuses+`)`,
		executorID).Scan(&n)
	return n, err
}

// CountOpenTasksOfType counts NON-terminal tasks of a type — the manual
// delete guard (SPEC §5.1: a type with open tasks cannot be deleted).
func (d *DAL) CountOpenTasksOfType(typeKey string) (int, error) {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM task
		WHERE type_key = ? AND status NOT IN (`+sqlTerminalStatuses+`)`,
		typeKey).Scan(&n)
	return n, err
}

// CountTasksDuplicatingOriginal counts the tasks that already point AT originalID
// as their duplicate_of original — the mark_duplicate chain guard (T-02c9
// point 3): a task that is already an original cannot itself be marked
// duplicated, which (together with the "target must not itself be duplicated"
// guard) keeps the graph depth-1 so the cockpit link always resolves in one hop.
func (d *DAL) CountTasksDuplicatingOriginal(originalID string) (int, error) {
	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM task WHERE duplicate_of = ?`, originalID).Scan(&n)
	return n, err
}

// PutTask upserts a task row (the SSE delta is the handler's job).
func (d *DAL) PutTask(t Task) error {
	inputs := t.Inputs
	if inputs == nil {
		inputs = map[string]any{}
	}
	blob, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		INSERT INTO task (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			type_key = excluded.type_key, title = excluded.title,
			dedupe_key = excluded.dedupe_key, inputs = excluded.inputs,
			description = excluded.description, status = excluded.status,
			lock = excluded.lock,
			priority = excluded.priority,
			executor_kind = excluded.executor_kind,
			executor_id = excluded.executor_id,
			creator_id = excluded.creator_id,
			waiting_reason = excluded.waiting_reason,
			created_ts = excluded.created_ts, updated_ts = excluded.updated_ts,
			closed_ts = excluded.closed_ts, closeout_ts = excluded.closeout_ts,
			duplicate_of = excluded.duplicate_of,
			reassigned_from = excluded.reassigned_from,
			reassigned_from_kind = excluded.reassigned_from_kind,
			outsource_model = excluded.outsource_model,
			outsource_effort = excluded.outsource_effort,
			outsource_machine = excluded.outsource_machine,
			handoff = excluded.handoff,
			handoff_note = excluded.handoff_note,
			handoff_task_id = excluded.handoff_task_id`,
		t.ID, t.TypeKey, t.Title, t.DedupeKey, string(blob), t.Description,
		t.Status, t.Lock, t.Priority, t.ExecutorKind, t.ExecutorID, t.CreatorID,
		t.WaitingReason,
		t.CreatedTS, t.UpdatedTS, t.ClosedTS, t.CloseoutTS, t.DuplicateOf,
		t.ReassignedFrom, t.ReassignedFromKind,
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine,
		t.Handoff, t.HandoffNote, t.HandoffTaskID,
	)
	return err
}

// ── task_dep ─────────────────────────────────────────────────────────────────

// ListTaskDeps returns the blocked_by ids of one task (deterministic order).
func (d *DAL) ListTaskDeps(taskID string) ([]string, error) {
	rows, err := d.db.Query(
		`SELECT blocked_by FROM task_dep WHERE task_id = ? ORDER BY blocked_by`,
		taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllTaskDeps maps task_id → blocked_by ids over the whole table (the
// list-endpoint fold input).
func (d *DAL) AllTaskDeps() (map[string][]string, error) {
	rows, err := d.db.Query(
		`SELECT task_id, blocked_by FROM task_dep ORDER BY task_id, blocked_by`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var t, b string
		if err := rows.Scan(&t, &b); err != nil {
			return nil, err
		}
		out[t] = append(out[t], b)
	}
	return out, rows.Err()
}

// ListTasksBlockedBy returns the tasks that name blockerID in their blocked_by
// list — the REVERSE of ListTaskDeps, and the query behind the T-74f8 handover
// half B: when a blocker reaches a terminal status, closeTask walks its
// dependents to release + wake them. Deterministic order (task id).
func (d *DAL) ListTasksBlockedBy(blockerID string) ([]Task, error) {
	rows, err := d.db.Query(`
		SELECT `+taskColumns+` FROM task
		WHERE id IN (SELECT task_id FROM task_dep WHERE blocked_by = ?)
		ORDER BY id`, blockerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddTaskDep adds ONE blocked_by edge without disturbing the rest of the list
// (set_task_deps' whole-list write would clobber deps the successor already
// carries). Idempotent — INSERT OR IGNORE on the composite key.
func (d *DAL) AddTaskDep(taskID, blockedBy string) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO task_dep (task_id, blocked_by) VALUES (?, ?)`,
		taskID, blockedBy)
	return err
}

// ReplaceTaskDeps replaces one task's deps wholesale (set_task_deps is a
// whole-list write) — transactional so a failed insert never half-applies.
func (d *DAL) ReplaceTaskDeps(taskID string, blockedBy []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.Exec(`DELETE FROM task_dep WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, b := range blockedBy {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO task_dep (task_id, blocked_by) VALUES (?, ?)`,
			taskID, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── task_step ────────────────────────────────────────────────────────────────

// TaskStep mirrors the task_step table (one row = one progress leaf).
type TaskStep struct {
	ID            string
	TaskID        string
	OrderIdx      int
	Name          string
	DoD           string
	Status        string // closed set (domain.go StepStatus*)
	ParallelGroup string // '' = plain sequential node
	IsGate        bool
	ReplyCardID   string // the CURRENTLY armed card; '' = none
	WaitingReason string // non-empty only while waiting_external (T-9ca5; task-level moved here)
	StartedTS     float64
	FinishedTS    float64
}

const taskStepColumns = `id, task_id, order_idx, name, dod, status,
	parallel_group, is_gate, reply_card_id, waiting_reason, started_ts, finished_ts`

func scanTaskStep(row interface{ Scan(...any) error }) (TaskStep, error) {
	var st TaskStep
	var isGate int
	err := row.Scan(
		&st.ID, &st.TaskID, &st.OrderIdx, &st.Name, &st.DoD, &st.Status,
		&st.ParallelGroup, &isGate, &st.ReplyCardID, &st.WaitingReason,
		&st.StartedTS, &st.FinishedTS,
	)
	if err != nil {
		return TaskStep{}, err
	}
	st.IsGate = isGate != 0
	return st, nil
}

// ListTaskSteps returns one task's steps in timeline order.
func (d *DAL) ListTaskSteps(taskID string) ([]TaskStep, error) {
	rows, err := d.db.Query(`
		SELECT `+taskStepColumns+` FROM task_step
		WHERE task_id = ? ORDER BY order_idx, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskStep
	for rows.Next() {
		st, err := scanTaskStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// AllTaskSteps maps task_id → steps (timeline order) over the whole table
// (the list-endpoint fold input).
func (d *DAL) AllTaskSteps() (map[string][]TaskStep, error) {
	rows, err := d.db.Query(
		`SELECT ` + taskStepColumns + ` FROM task_step ORDER BY task_id, order_idx, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]TaskStep{}
	for rows.Next() {
		st, err := scanTaskStep(rows)
		if err != nil {
			return nil, err
		}
		out[st.TaskID] = append(out[st.TaskID], st)
	}
	return out, rows.Err()
}

// TaskStepProgress is the leaf-count pair the light task list needs — the same
// (done, total) TaskProgress derives from full step rows, but counted in SQL so
// the list projection never loads the steps' dod/name text.
type TaskStepProgress struct {
	Done  int
	Total int
}

// AllTaskStepProgress returns every task's step (done, total) counts in one
// grouped COUNT query — the light-list progress source (GET /api/tasks), which
// skips the AllTaskSteps full-row scan. Tasks with no steps are simply absent
// from the map (0/0 — the caller's zero value), matching TaskProgress on [].
// superseded rows count toward neither side (pure replan history — T-1aea;
// domain.TaskProgress is the in-memory twin, keep them agreeing).
func (d *DAL) AllTaskStepProgress() (map[string]TaskStepProgress, error) {
	rows, err := d.db.Query(
		`SELECT task_id,
		        SUM(CASE WHEN status != ? THEN 1 ELSE 0 END) AS total,
		        SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS done
		   FROM task_step GROUP BY task_id`, StepStatusSuperseded, StepStatusDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TaskStepProgress{}
	for rows.Next() {
		var taskID string
		var total, done int
		if err := rows.Scan(&taskID, &total, &done); err != nil {
			return nil, err
		}
		out[taskID] = TaskStepProgress{Done: done, Total: total}
	}
	return out, rows.Err()
}

// GetTaskStep returns one step by id, or nil if absent.
func (d *DAL) GetTaskStep(id string) (*TaskStep, error) {
	row := d.db.QueryRow(
		`SELECT `+taskStepColumns+` FROM task_step WHERE id = ?`, id)
	st, err := scanTaskStep(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// PutTaskStep upserts one step row.
func (d *DAL) PutTaskStep(st TaskStep) error {
	isGate := 0
	if st.IsGate {
		isGate = 1
	}
	_, err := d.db.Exec(`
		INSERT INTO task_step (`+taskStepColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			task_id = excluded.task_id, order_idx = excluded.order_idx,
			name = excluded.name, dod = excluded.dod,
			status = excluded.status,
			parallel_group = excluded.parallel_group,
			is_gate = excluded.is_gate,
			reply_card_id = excluded.reply_card_id,
			waiting_reason = excluded.waiting_reason,
			started_ts = excluded.started_ts,
			finished_ts = excluded.finished_ts`,
		st.ID, st.TaskID, st.OrderIdx, st.Name, st.DoD, st.Status,
		st.ParallelGroup, isGate, st.ReplyCardID, st.WaitingReason,
		st.StartedTS, st.FinishedTS,
	)
	return err
}

// ReplaceTaskPlan replaces a task's non-preserved steps with newSteps
// (submit_plan semantics): terminal steps (done / already-superseded history)
// are ALWAYS kept, in their original order, ahead of the fresh plan; the
// handler additionally names the
// answered-card rows to preserve (T-1aea) — `retain` ids stay alive exactly
// as they are (the fresh plan re-listed them by name), `freeze` ids become
// the superseded terminal state with finished_ts stamped to frozenTS (the
// freeze moment — started_ts and reply_card_id stay, so the step's
// question-and-answer history keeps rendering). Every other non-done row is
// deleted. Which rows qualify is the HANDLER's call (it joins the reply_card
// side); the DAL never reads the card table here — the layering stays.
// Transactional; returns the resulting full step list in timeline order.
// newSteps arrive with ID/Status/OrderIdx unset — this method assigns order
// indexes after the kept prefix (ids are the caller's mint).
func (d *DAL) ReplaceTaskPlan(taskID string, retain, freeze []string,
	frozenTS float64, newSteps []TaskStep) ([]TaskStep, error) {
	existing, err := d.ListTaskSteps(taskID)
	if err != nil {
		return nil, err
	}
	preserved := map[string]bool{}
	for _, id := range retain {
		preserved[id] = true
	}
	frozen := map[string]bool{}
	for _, id := range freeze {
		preserved[id] = true
		frozen[id] = true
	}
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var kept []TaskStep
	for _, st := range existing {
		// Terminal rows (done AND already-superseded history) are always
		// kept; other rows survive only when the handler named them.
		if !StepIsTerminal(st.Status) && !preserved[st.ID] {
			if _, err := tx.Exec(
				`DELETE FROM task_step WHERE id = ?`, st.ID); err != nil {
				return nil, err
			}
			continue
		}
		if frozen[st.ID] {
			st.Status = StepStatusSuperseded
			st.FinishedTS = frozenTS
		}
		kept = append(kept, st)
	}
	// Re-index the kept prefix 0..n-1 (original relative order — done and
	// superseded rows keep their place on the timeline), then the fresh plan
	// after it. The one UPDATE also lands the freeze (status + finished_ts);
	// done/retained rows just rewrite their own unchanged values.
	for i := range kept {
		kept[i].OrderIdx = i
		if _, err := tx.Exec(
			`UPDATE task_step SET order_idx = ?, status = ?, finished_ts = ?
			  WHERE id = ?`,
			kept[i].OrderIdx, kept[i].Status, kept[i].FinishedTS,
			kept[i].ID); err != nil {
			return nil, err
		}
	}
	out := kept
	for i, st := range newSteps {
		st.TaskID = taskID
		st.OrderIdx = len(kept) + i
		if st.Status == "" {
			st.Status = StepStatusPending
		}
		isGate := 0
		if st.IsGate {
			isGate = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO task_step (`+taskStepColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			st.ID, st.TaskID, st.OrderIdx, st.Name, st.DoD, st.Status,
			st.ParallelGroup, isGate, st.ReplyCardID, st.WaitingReason,
			st.StartedTS, st.FinishedTS); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ── outsource_worker (member-table projection since A案 P7d) ─────────────────

// OutsourceWorker is the outsource projection over the MEMBER table
// (migrations/00025 folded the retired outsource_worker table in — 外包＝正職,
// the only difference is the task-coupled lifecycle). ID is the worker's JWT
// sub AND its member row id (ow- prefix, disjoint from every staff id). The
// struct keeps the historical worker vocabulary so the frozen wire DTO and the
// scheduler stay verbatim; Status is DERIVED from the member row
// (roster_status + activated_ts — workerStatusFromMember), never stored.
//
// The former spawn_attempts / last_spawn_ts / last_spawn_target columns were
// deliberately NOT carried over: outsource spawn observability now lives in
// the server's in-memory maps (worker_spawn.go workerSpawnAt/Target/Attempts),
// the member-reconcile posture — a restart forgets them (accepted trade-off,
// P7d spec 1f).
type OutsourceWorker struct {
	ID       string
	Codename string
	Model    string
	Effort   string
	TaskID   string
	Status   string // closed set assigned|active|released (derived projection)
	// ActivatedTS is the durable assigned→active anchor (member.activated_ts):
	// 0 = never claimed its task; >0 = the first GET /api/self/task claim time.
	// Writers normally leave it alone — the Put mapping stamps it when Status
	// flips to active with no anchor yet.
	ActivatedTS  float64
	CreatedTS    float64
	ReleasedTS   float64
	LastOp       string
	LastOpOK     *bool // nil = no worker receipt folded yet (three-valued)
	LastOpLog    string
	LastOpReason string
	LastOpAt     float64
	// DesiredMachineID is the OWNER-PINNED placement (T-f190, migrations/00018),
	// the worker twin of member.desired_machine_id: "" = unpinned (fall back to
	// the manual's "auto"|machine-id preference), "auto" = idlest-online, or a
	// concrete machine id. notifyWorkerSpawn prefers this over the manual pref;
	// the relocate handler writes it and re-spawns onto the chosen machine.
	DesiredMachineID string
	// RefocusSince is the in-flight context-handover marker (T-32e1,
	// migrations/00019), the worker twin of member.RefocusSince: >0 while a
	// refocus (owner 換手 button OR the context-high auto-handover) is mid-flight,
	// 0 otherwise. Set by both refocus paths, used as the auto-handover cooldown,
	// and cleared by the tick's loop-break once a fresh session boots after it.
	RefocusSince float64
	// StoppingSince / StoppedSince are the graceful-handover wind-down anchors
	// (T-ea82), DIRECT mirrors of the member columns (the row has carried them
	// since the P7d fold): stopping_since marks the SOP started; stopped_since
	// is the dump-done latch both 收口 drivers (stopped-report, grace timeout)
	// key their once-only check on. Carried through workerFromMember /
	// memberFromWorker so no in-between PutOutsourceWorker can zero a
	// mid-handover anchor.
	StoppingSince float64
	StoppedSince  float64
	// DesiredState is the run-intent, a DIRECT mirror of member.DesiredState
	// (T-f190, migrations/00020): "online" (system wants it running — the default),
	// "offline" (owner-explicit STOP — held down, every auto-revival path skips it).
	// Set "offline" by stop, back to "online" by restart. A worker whose intent is
	// offline projects spawn_state "stopped". Replaces the earlier bespoke
	// stopped_since marker with the member value domain (owner: 外包＝系統代管的正職員工).
	DesiredState string
	// BankedCost is the persistent historical cumulative cost (T-ba6b,
	// migrations/00021), the worker twin of member.BankedCost: the live
	// telemetry cost folds in here (bankLiveCost — the SAME helper the member
	// SSE-disconnect edge uses) whenever the session ends or is killed for a
	// respawn, so a refocus / 換 model / auto-handover no longer zeroes the
	// owner-visible spend. Kept separate from the live figure (never
	// overlapping); the panel sums live + banked.
	BankedCost float64
}

// workerStatusFromMember derives the frozen worker lifecycle vocabulary from
// the member row's anchors: roster removed ⇒ released; a claimed task
// (activated_ts > 0) ⇒ active; else assigned. The single derivation both scan
// and every projection share — keep it the exact inverse of memberFromWorker.
func workerStatusFromMember(rosterStatus string, activatedTS float64) string {
	if rosterStatus == RosterStatusRemoved {
		return WorkerStatusReleased
	}
	if activatedTS > 0 {
		return WorkerStatusActive
	}
	return WorkerStatusAssigned
}

// workerFromMember projects one kind='outsource' member row onto the worker
// vocabulary (the read half of the P7d fold).
func workerFromMember(m Member) OutsourceWorker {
	taskID := ""
	if m.LinkedTaskID != nil {
		taskID = *m.LinkedTaskID
	}
	return OutsourceWorker{
		ID:               m.ID,
		Codename:         m.Codename,
		Model:            m.Model,
		Effort:           m.Effort,
		TaskID:           taskID,
		Status:           workerStatusFromMember(m.RosterStatus, m.ActivatedTS),
		ActivatedTS:      m.ActivatedTS,
		CreatedTS:        m.CreatedTS,
		ReleasedTS:       m.ReleasedTS,
		LastOp:           m.LastOp,
		LastOpOK:         m.LastOpOK,
		LastOpLog:        m.LastOpLog,
		LastOpReason:     m.LastOpReason,
		LastOpAt:         m.LastOpAt,
		DesiredMachineID: m.DesiredMachineID,
		RefocusSince:     m.RefocusSince,
		StoppingSince:    m.StoppingSince,
		StoppedSince:     m.StoppedSince,
		DesiredState:     m.DesiredState,
		BankedCost:       m.BankedCost,
	}
}

// memberFromWorker maps the worker vocabulary back onto a member row (the
// write half). Name mirrors the codename (the outsource display name);
// role_key stays "" (an outsource member classifies as a plain agent — the
// same authz floor the roster-less worker had). Status → roster_status +
// activated_ts: the first write with Status active and no anchor yet stamps
// activated_ts = now (the GET /api/self/task claim edge — the only
// assigned→active transition).
func memberFromWorker(w OutsourceWorker) Member {
	roster := RosterStatusActive
	if w.Status == WorkerStatusReleased {
		roster = RosterStatusRemoved
	}
	activated := w.ActivatedTS
	switch w.Status {
	case WorkerStatusAssigned:
		activated = 0.0
	case WorkerStatusActive:
		if activated == 0.0 {
			activated = nowSecs()
		}
	}
	taskID := w.TaskID
	return Member{
		ID:               w.ID,
		Name:             w.Codename,
		Kind:             KindOutsource,
		RoleKey:          "",
		Model:            w.Model,
		Effort:           w.Effort,
		DesiredState:     w.DesiredState,
		DesiredMachineID: w.DesiredMachineID,
		RefocusSince:     w.RefocusSince,
		StoppingSince:    w.StoppingSince,
		StoppedSince:     w.StoppedSince,
		BankedCost:       w.BankedCost,
		LastOp:           w.LastOp,
		LastOpOK:         w.LastOpOK,
		LastOpLog:        w.LastOpLog,
		LastOpReason:     w.LastOpReason,
		LastOpAt:         w.LastOpAt,
		RosterStatus:     roster,
		LinkedTaskID:     &taskID,
		Codename:         w.Codename,
		CreatedTS:        w.CreatedTS,
		ReleasedTS:       w.ReleasedTS,
		ActivatedTS:      activated,
	}
}

// ListOutsourceWorkers returns every outsource member row projected onto the
// worker vocabulary (released/removed included — the panel filter is a handler
// projection; codename MAX+1 folds over the FULL set, removed rows included,
// so a codename is never reused).
func (d *DAL) ListOutsourceWorkers() ([]OutsourceWorker, error) {
	rows, err := d.db.Query(`SELECT ` + memberColumns +
		` FROM member WHERE kind = 'outsource' ORDER BY created_ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutsourceWorker
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workerFromMember(m))
	}
	return out, rows.Err()
}

// GetOutsourceWorker returns one worker by id (the JWT sub), or nil. Only
// kind='outsource' rows project — a staff/warden member id is nil here by
// construction (the two id namespaces are disjoint anyway).
func (d *DAL) GetOutsourceWorker(id string) (*OutsourceWorker, error) {
	row := d.db.QueryRow(`SELECT `+memberColumns+
		` FROM member WHERE id = ? AND kind = 'outsource'`, id)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w := workerFromMember(m)
	return &w, nil
}

// PutOutsourceWorker upserts one worker as its kind='outsource' member row
// (memberFromWorker mapping). Pure DAL — no member SSE delta: the outsource
// wire keeps its own owner-only outsource_worker topic (publishOutsourceWorker).
func (d *DAL) PutOutsourceWorker(w OutsourceWorker) error {
	return d.PutMember(memberFromWorker(w))
}

// ReleaseWorkersForTask flips every not-yet-released worker bound to taskID
// to released (the task-terminal side effect) and returns the flipped rows —
// the handler fans one outsource_worker delta per row. Row retention is the
// audit trail; idempotent (already-released rows are untouched).
func (d *DAL) ReleaseWorkersForTask(taskID string, now float64) ([]OutsourceWorker, error) {
	rows, err := d.db.Query(`SELECT `+memberColumns+` FROM member
		WHERE kind = 'outsource' AND linked_task_id = ? AND roster_status != ?
		ORDER BY created_ts, id`, taskID, RosterStatusRemoved)
	if err != nil {
		return nil, err
	}
	var flipped []OutsourceWorker
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		flipped = append(flipped, workerFromMember(m))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range flipped {
		flipped[i].Status = WorkerStatusReleased
		flipped[i].ReleasedTS = now
		if _, err := d.db.Exec(`
			UPDATE member SET roster_status = ?, released_ts = ?
			WHERE id = ? AND kind = 'outsource'`,
			RosterStatusRemoved, now, flipped[i].ID); err != nil {
			return nil, err
		}
	}
	return flipped, nil
}

// ReleaseWorkerByID flips ONE worker (by its own id) to released if it is not
// already, returning the flipped row (or nil when the id is unknown / already
// released). The by-WORKER-ID twin of ReleaseWorkersForTask (T-ba04): the
// deferred handover dismiss must fire the PREDECESSOR outsource worker alone —
// releasing by task_id would also catch the NEW worker that an outsource→
// outsource takeover has already bound to the SAME task_id, killing the very
// session that just took over. Idempotent (an already-released row is a nil
// no-op).
func (d *DAL) ReleaseWorkerByID(workerID string, now float64) (*OutsourceWorker, error) {
	w, err := d.GetOutsourceWorker(workerID)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Status == WorkerStatusReleased {
		return nil, nil
	}
	w.Status = WorkerStatusReleased
	w.ReleasedTS = now
	if _, err := d.db.Exec(`
		UPDATE member SET roster_status = ?, released_ts = ?
		WHERE id = ? AND kind = 'outsource'`,
		RosterStatusRemoved, now, workerID); err != nil {
		return nil, err
	}
	return w, nil
}

// ── task_manual ──────────────────────────────────────────────────────────────

// TaskManual mirrors the task_manual table. Fields/Assignee stay as their
// stored JSON TEXT here (the domain ring parses fields; assignee is a
// free-shape object the handlers validate on write).
type TaskManual struct {
	TypeKey     string
	DisplayName string
	Purpose     string
	Fields      string // JSON array [{name, required, is_key}]
	SopMD       string
	Learnings   string
	Assignee    string // JSON object; "{}" = unset
	UpdatedTS   float64
}

const taskManualColumns = `type_key, purpose, fields, sop_md, learnings,
	assignee, updated_ts, display_name`

func scanTaskManual(row interface{ Scan(...any) error }) (TaskManual, error) {
	var m TaskManual
	err := row.Scan(
		&m.TypeKey, &m.Purpose, &m.Fields, &m.SopMD, &m.Learnings,
		&m.Assignee, &m.UpdatedTS, &m.DisplayName,
	)
	return m, err
}

// ListTaskManuals returns every manual, ordered by display name
// (falling back to type_key when unset), then type_key.
func (d *DAL) ListTaskManuals() ([]TaskManual, error) {
	rows, err := d.db.Query(
		`SELECT ` + taskManualColumns + ` FROM task_manual
		ORDER BY (CASE WHEN display_name = '' THEN type_key ELSE display_name END)
		COLLATE NOCASE, type_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskManual
	for rows.Next() {
		m, err := scanTaskManual(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetTaskManual returns one manual by type key, or nil if absent.
func (d *DAL) GetTaskManual(typeKey string) (*TaskManual, error) {
	row := d.db.QueryRow(
		`SELECT `+taskManualColumns+` FROM task_manual WHERE type_key = ?`, typeKey)
	m, err := scanTaskManual(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PutTaskManual upserts one manual row.
func (d *DAL) PutTaskManual(m TaskManual) error {
	_, err := d.db.Exec(`
		INSERT INTO task_manual (`+taskManualColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (type_key) DO UPDATE SET
			purpose = excluded.purpose, fields = excluded.fields,
			sop_md = excluded.sop_md, learnings = excluded.learnings,
			assignee = excluded.assignee, updated_ts = excluded.updated_ts,
			display_name = excluded.display_name`,
		m.TypeKey, m.Purpose, m.Fields, m.SopMD, m.Learnings,
		m.Assignee, m.UpdatedTS, m.DisplayName,
	)
	return err
}

// DeleteTaskManual hard-deletes one manual (pure owner data — no seed, no
// tombstone). The open-task 409 guard is the handler's. Returns true iff a
// row was deleted.
func (d *DAL) DeleteTaskManual(typeKey string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM task_manual WHERE type_key = ?`, typeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
