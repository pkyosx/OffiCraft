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
	HandoverNote       string
	HandoverNoteTS     float64
	HandoverNoteBy     string
	// OutsourceRuntime / OutsourceModel / OutsourceEffort / OutsourceMachine is
	// the resolved outsource spec of a task on the outsource track (T-35e0,
	// migrations/00029): what the worker minted for it is given. '' on every
	// non-outsource task. OutsourceDispatched (migrations/00036) says which of the
	// TWO meanings the columns carry — the columns alone no longer do, and the old
	// "non-empty ⇒ explicit dispatch" inference is retired:
	//
	//   - Dispatched (an explicit create/reassign `target.kind=outsource`): the
	//     AUTHORITATIVE target. The scheduler mints from it in preference to the
	//     type manual's assignee spec, and skips its own spawn gate (the dispatch
	//     was already authorized at the handler, by the true initiator).
	//   - NOT dispatched (a plain manual-driven outsource task): a create-time
	//     SNAPSHOT of the creator's own runtime/model/effort/machine (T-8a67),
	//     consulted only for the fields the LIVE type manual leaves unset — so the
	//     manual stays authoritative and editable, while a manual that names no
	//     machine no longer strands the worker with no placement at all.
	//
	// Either way the row is the durable record: a handover or a rebirth re-reads
	// it rather than re-deriving, so placement cannot drift between generations.
	OutsourceRuntime    string
	OutsourceModel      string
	OutsourceEffort     string
	OutsourceMachine    string
	OutsourceDispatched bool
	// Handoff / HandoffNote / HandoffTaskID is the DECLARED destination of the
	// ball (T-74f8, migrations/00031): '' = never declared, else one of
	// domain.go's HandoffReturnToCreator / HandoffFollowUp / HandoffNone. The
	// close gate (api_tasks.go handoffGateVerdict) refuses to let a
	// creator≠executor task close until this is set, so a finished task can no
	// longer end with nobody holding anything.
	Handoff       string
	HandoffNote   string
	HandoffTaskID string
	// FrozenBy is WHO put this task into the frozen priority (T-6020,
	// migrations/00037): the verified token sub of that write — the wireOwnerID
	// literal for owner scope, else the member / outsource-worker id. '' means
	// "not frozen" (and pre-column rows, honestly unattributed); the write that
	// moves the task off frozen clears it. Until T-6020 frozen was gated to the
	// owner alone so the freezer was inferable; now that owner, admin_agent and
	// the executor may all freeze, it has to be RECORDED or the owner cannot
	// tell their own 喊停 from an agent's.
	FrozenBy string
	// KickoffNotifiedTo is VESTIGIAL (T-51b0). It was the de-duplication ledger
	// of the outsource kickoff notice (T-e77f, migrations/00056); the notice was
	// withdrawn wholesale (owner 2026-08-15, card rc-a4f6a7f8cd71) and NOTHING
	// writes this any more — it round-trips as whatever the row already held.
	//
	// The field and its column stay on purpose. Dropping a column needs a
	// migration whose only benefit is tidiness, while the owner's word for the
	// withdrawal was 「先砍掉」— provisional — and a column that is still there
	// is the difference between restoring the seam and re-deriving it. Do not
	// read a non-empty value as "a notice is outstanding"; it is a fossil of one
	// that was sent before this change.
	//
	// ⚠️ RESTORING the seam means CLEARING this column first. The fossils sit at
	// stamp == executor, so a restored de-duplication check would swallow the
	// first kickoff of exactly the tasks that were notified before — the subset
	// nobody would think to look at.
	KickoffNotifiedTo string
}

const taskColumns = `id, type_key, title, dedupe_key, inputs, description,
	status, lock, priority, executor_kind, executor_id, creator_id, waiting_reason,
	created_ts, updated_ts, closed_ts, closeout_ts, duplicate_of,
	reassigned_from, reassigned_from_kind,
	handover_note, handover_note_ts, handover_note_by,
	outsource_runtime, outsource_model, outsource_effort, outsource_machine,
	outsource_dispatched,
	handoff, handoff_note, handoff_task_id, frozen_by, kickoff_notified_to`

// sqlTerminalStatuses is the SQL IN-list of the terminal statuses — every
// "open task" filter (dedupe probe, resume block, open counts) excludes these.
// Kept in ONE place so a new terminal state (duplicated joined done/terminated
// in T-02c9) updates every filter at once rather than drifting per query.
const sqlTerminalStatuses = `'done', 'terminated', 'duplicated'`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var inputs string
	var dispatched int
	err := row.Scan(
		&t.ID, &t.TypeKey, &t.Title, &t.DedupeKey, &inputs, &t.Description,
		&t.Status, &t.Lock, &t.Priority, &t.ExecutorKind, &t.ExecutorID, &t.CreatorID,
		&t.WaitingReason,
		&t.CreatedTS, &t.UpdatedTS, &t.ClosedTS, &t.CloseoutTS, &t.DuplicateOf,
		&t.ReassignedFrom, &t.ReassignedFromKind,
		&t.HandoverNote, &t.HandoverNoteTS, &t.HandoverNoteBy,
		&t.OutsourceRuntime, &t.OutsourceModel, &t.OutsourceEffort, &t.OutsourceMachine,
		&dispatched,
		&t.Handoff, &t.HandoffNote, &t.HandoffTaskID, &t.FrozenBy,
		&t.KickoffNotifiedTo,
	)
	if err != nil {
		return Task{}, err
	}
	t.OutsourceDispatched = dispatched != 0
	if err := json.Unmarshal([]byte(inputs), &t.Inputs); err != nil {
		return Task{}, fmt.Errorf("task %s: bad inputs JSON: %w", t.ID, err)
	}
	return t, nil
}

// ListTasks returns every task, oldest→newest (filters/sorting are handler
// projections — the wire serves full DTOs, the FE partitions).
func (d *DAL) ListTasks() ([]Task, error) {
	rows, err := d.rdb.Query(
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
	row := d.rdb.QueryRow(`SELECT `+taskColumns+` FROM task WHERE id = ?`, id)
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
	row := d.rdb.QueryRow(`
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
	rows, err := d.rdb.Query(`
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
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM task
		WHERE executor_id = ? AND status NOT IN (`+sqlTerminalStatuses+`)`,
		executorID).Scan(&n)
	return n, err
}

// CountOpenTasksOfType counts NON-terminal tasks of a type — the manual
// delete guard (SPEC §5.1: a type with open tasks cannot be deleted).
func (d *DAL) CountOpenTasksOfType(typeKey string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
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
	err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM task WHERE duplicate_of = ?`, originalID).Scan(&n)
	return n, err
}

// PutTask upserts a task row (the SSE delta is the handler's job).
//
// 🔴 `description` AND `title` ARE DELIBERATELY ABSENT FROM THE ON CONFLICT
// UPDATE LIST (description: T-e271 node 3; title: T-2ebe). Do not "restore"
// either — those lines are the lost update, and for description it was
// measured, not theorised.
//
// The hazard is structural, not exotic: this is a whole-row upsert with no
// optimistic lock, and every task-writing handler is a load-mutate-save
// (resolveTask on the READ pool → mutate one field → PutTask on the write
// pool). Nothing links the read to the write, so the upsert asserts EVERY
// column as that handler read them. With the description in the conflict list,
// an admin changing a task's priority replays the description it happened to
// read a moment earlier — silently destroying a correction the description
// endpoint had already answered 200 to. Measured before the fix: a
// deterministic interleave lost it every time, and two goroutines driving the
// two real endpoints lost it by round 17 of 60. "Rare" was not true.
//
// The fix is an OWNERSHIP BOUNDARY rather than a lock or a retry: each column is
// written ONLY by its own single-field setter (SetTaskDescriptionOn /
// SetTaskTitleOn, each of which versions its column in the same transaction) and
// by the INSERT half of this very statement, which is how create_task sets them
// — it mints a fresh id, so it never reaches the conflict clause. (Until T-f265
// the handoff follow-up was a second such INSERT; it no longer exists, which
// removes a writer rather than adding one.) Single-writer columns cannot be clobbered by a stale
// whole-row copy, because no stale whole-row copy of them exists. Guarded by
// TestTaskDescriptionRaceGuardHasTeeth and TestTaskTitleRaceGuardHasTeeth.
//
// 🔴 `title` JOINED THIS CARVE-OUT WHEN IT BECAME EDITABLE (T-2ebe), and the
// ORDER of those two facts is the whole point. While a title could only be set
// at birth, listing it in the conflict clause was harmless: the value being
// replayed was always the value already stored, so a lost update had nothing to
// lose. Opening an edit door is what turns that same line into the description
// bug verbatim — an admin changing a task's priority replays the title it
// happened to read a moment earlier and silently destroys a correction the title
// endpoint has already answered 200 to. Verified at the time of the change that
// no production path mutates Title on an EXISTING row (the only writers are the
// create INSERT and the new setter; the handoff follow-up's INSERT was a third
// until T-f265 removed it), so
// dropping it from the conflict clause changes nothing for any existing caller —
// it only removes the clobber.
//
// ⚠️ SCOPE, stated so nobody reads more safety into this than is here: this
// removes the hazard for ONE column. Every OTHER column of this row remains a
// shared-write, last-writer-wins field, and two handlers racing on two
// different columns still lose one of them. That is pre-existing and untouched
// (T-e271 node 3 explicitly did not widen into it) — PutTaskStep carries the
// same carve-out for its `note` column one table over (T-e271 node 6), and its
// remaining columns are still shared-write for exactly this reason.
func (d *DAL) PutTask(t Task) error {
	inputs := t.Inputs
	if inputs == nil {
		inputs = map[string]any{}
	}
	blob, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	dispatched := 0
	if t.OutsourceDispatched {
		dispatched = 1
	}
	_, err = d.wdb.Exec(`
		INSERT INTO task (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			type_key = excluded.type_key,
			dedupe_key = excluded.dedupe_key, inputs = excluded.inputs,
			status = excluded.status,
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
			handover_note = excluded.handover_note,
			handover_note_ts = excluded.handover_note_ts,
			handover_note_by = excluded.handover_note_by,
			outsource_runtime = excluded.outsource_runtime,
			outsource_model = excluded.outsource_model,
			outsource_effort = excluded.outsource_effort,
			outsource_machine = excluded.outsource_machine,
			outsource_dispatched = excluded.outsource_dispatched,
			handoff = excluded.handoff,
			handoff_note = excluded.handoff_note,
			handoff_task_id = excluded.handoff_task_id,
			frozen_by = excluded.frozen_by,
			kickoff_notified_to = excluded.kickoff_notified_to`,
		t.ID, t.TypeKey, t.Title, t.DedupeKey, string(blob), t.Description,
		t.Status, t.Lock, t.Priority, t.ExecutorKind, t.ExecutorID, t.CreatorID,
		t.WaitingReason,
		t.CreatedTS, t.UpdatedTS, t.ClosedTS, t.CloseoutTS, t.DuplicateOf,
		t.ReassignedFrom, t.ReassignedFromKind,
		t.HandoverNote, t.HandoverNoteTS, t.HandoverNoteBy,
		NormalizeRuntime(t.OutsourceRuntime),
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine,
		dispatched,
		t.Handoff, t.HandoffNote, t.HandoffTaskID, t.FrozenBy,
		t.KickoffNotifiedTo,
	)
	return err
}

// ── task_dep ─────────────────────────────────────────────────────────────────

// ListTaskDeps returns the blocked_by ids of one task (deterministic order).
func (d *DAL) ListTaskDeps(taskID string) ([]string, error) {
	rows, err := d.rdb.Query(
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
	rows, err := d.rdb.Query(
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
	rows, err := d.rdb.Query(`
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
//
// (T-e77f's warning about this bypassing the kickoff seam retired with the seam
// itself in T-51b0 — nothing reads kickoff_notified_to any more.)
func (d *DAL) AddTaskDep(taskID, blockedBy string) error {
	_, err := d.wdb.Exec(
		`INSERT OR IGNORE INTO task_dep (task_id, blocked_by) VALUES (?, ?)`,
		taskID, blockedBy)
	return err
}

// ReplaceTaskDeps replaces one task's deps wholesale (set_task_deps is a
// whole-list write) — transactional so a failed insert never half-applies.
func (d *DAL) ReplaceTaskDeps(taskID string, blockedBy []string) error {
	tx, err := d.wdb.Begin()
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
	// Note is the step's free-text working note: what this step got to and what
	// comes next (T-cc3e). Unlike WaitingReason it is bound to NO status — it is
	// writable in every one of them, because a handover lands at an arbitrary
	// moment and the note is what the next session reads to pick the work back
	// up. Wholesale write, last one wins: current state, not an append-only log.
	Note       string
	StartedTS  float64
	FinishedTS float64
}

const taskStepColumns = `id, task_id, order_idx, name, dod, status,
	parallel_group, is_gate, reply_card_id, waiting_reason, note, started_ts, finished_ts`

func scanTaskStep(row interface{ Scan(...any) error }) (TaskStep, error) {
	var st TaskStep
	var isGate int
	err := row.Scan(
		&st.ID, &st.TaskID, &st.OrderIdx, &st.Name, &st.DoD, &st.Status,
		&st.ParallelGroup, &isGate, &st.ReplyCardID, &st.WaitingReason, &st.Note,
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
	rows, err := d.rdb.Query(`
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
	rows, err := d.rdb.Query(
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
	rows, err := d.rdb.Query(
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
	row := d.rdb.QueryRow(
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

// SetTaskStepNote writes ONE column of ONE step row (T-cc3e). It reports
// whether a row was actually there: false means the step is gone, and the
// caller turns that into a 404 rather than silently succeeding.
//
// Deliberately NOT PutTaskStep. Every other step writer does load-mutate-save
// through that whole-row upsert, which replays every column the caller read
// moments earlier — fine when the caller owns the transition, wrong here. A
// note write carries no opinion about status, reply_card_id or order_idx, but
// a whole-row upsert would assert stale values for all of them: answer a reply
// card in the window between the read and the write and the upsert drags the
// step back to waiting_owner pointing at a card that is already answered; let
// submit_plan delete the step in that window and the upsert RESURRECTS it,
// because an upsert on a deleted row inserts. A single-column UPDATE cannot do
// either — it touches nothing it was not asked to touch, and it affects zero
// rows when the step is gone.
func (d *DAL) SetTaskStepNote(id, note string) (bool, error) {
	res, err := d.wdb.Exec(`UPDATE task_step SET note = ? WHERE id = ?`, note, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetTaskDescriptionOn writes ONE task's description (plus the updated_ts that
// makes an already-open cockpit card re-read it) and nothing else, through the
// caller's executer — so the description edit and the document_history revision
// it replaces land in the SAME transaction (T-e271, api_tasks_description.go).
// Reports whether a row was actually updated; false means the task is gone,
// which the caller turns into a 404 rather than silently succeeding.
//
// Deliberately NOT PutTask, for the same reason SetTaskStepNote is not
// PutTaskStep: PutTask is a whole-row upsert with no optimistic lock, so
// writing a description through it replays every other column — status,
// priority, executor, the outsource spec — as the caller read them a moment
// earlier, and races whoever is changing one of those. Correcting the ticket's
// text carries no opinion about any of them. It would also RESURRECT a task
// deleted in that window, because an upsert on a missing row inserts.
func SetTaskDescriptionOn(ex sqlExecer, id, description string, updatedTS float64) (bool, error) {
	res, err := ex.Exec(
		`UPDATE task SET description = ?, updated_ts = ? WHERE id = ?`,
		description, updatedTS, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// taskDescriptionOn reads ONE task's description from inside the caller's
// transaction — the document-history snapshot reader (T-e271). It re-reads
// rather than trusting a value the handler folded earlier, for the reason
// SaveWithDocumentHistory documents: the retained revision must be the state
// this write actually replaced, otherwise two writers racing on one task both
// retain the same ancestor and the revision written between them becomes
// unrecoverable.
func taskDescriptionOn(q sqlQuerier, id string) (string, bool, error) {
	var description string
	err := q.QueryRow(`SELECT description FROM task WHERE id = ?`, id).Scan(&description)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return description, true, nil
}

// SetTaskTitleOn writes ONE task's title (plus the updated_ts that makes an
// already-open cockpit card re-read it) and nothing else, through the caller's
// executer — so the title edit and the document_history revision it replaces
// land in the SAME transaction (T-2ebe, api_tasks_title.go). Reports whether a
// row was actually updated; false means the task is gone, which the caller turns
// into a 404 rather than silently succeeding.
//
// Deliberately NOT PutTask, for the reason spelled out at PutTask's carve-out:
// a whole-row upsert replays every other column as the caller read it a moment
// earlier, and it RESURRECTS a task deleted in that window because an upsert on
// a missing row inserts. Correcting a ticket's title carries no opinion about
// its status, priority or executor.
//
// The caller trims; this writes what it is given. Keeping the trim at the door
// rather than here is deliberate — the door is also where a blank is refused
// (400), and splitting "what counts as blank" from "what gets stored" across two
// layers is how the two drift apart.
func SetTaskTitleOn(ex sqlExecer, id, title string, updatedTS float64) (bool, error) {
	res, err := ex.Exec(
		`UPDATE task SET title = ?, updated_ts = ? WHERE id = ?`,
		title, updatedTS, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// taskTitleOn reads ONE task's title from inside the caller's transaction — the
// document-history snapshot reader (T-2ebe), twin of taskDescriptionOn. It
// re-reads rather than trusting a value the handler folded earlier: the retained
// revision must be the state this write actually replaced, otherwise two writers
// racing on one task both retain the same ancestor and the revision written
// between them becomes unrecoverable.
func taskTitleOn(q sqlQuerier, id string) (string, bool, error) {
	var title string
	err := q.QueryRow(`SELECT title FROM task WHERE id = ?`, id).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return title, true, nil
}

// TouchTaskUpdatedTS bumps ONE task's updated_ts and nothing else (T-cc3e).
//
// The cockpit's task card re-reads its (heavy, step-carrying) detail when
// updated_ts changes — the SSE task delta itself only carries id/status/
// priority and the list it refreshes carries no steps at all. So a write that
// changes only a step, and leaves updated_ts alone, is invisible to a card the
// owner already has open: it renders the detail it hydrated on expand, forever.
// That is the whole deliverable of this ticket ("第 4 步做到哪"), so the note
// write has to move this field.
//
// Single-column UPDATE for the same reason as SetTaskStepNote above: PutTask is
// a whole-row upsert with no optimistic lock, so bumping a timestamp through it
// would replay every other task column — status, priority, executor — as the
// caller last read them, and race whoever is changing one of those.
func (d *DAL) TouchTaskUpdatedTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE task SET updated_ts = ? WHERE id = ?`, ts, id)
	return err
}

// PutTaskStep upserts one step row.
//
// 🔴 `note` IS DELIBERATELY ABSENT FROM THE ON CONFLICT UPDATE LIST (T-e271
// node 6). Do not "restore" it — that line is the lost update, and it was
// measured, not theorised.
//
// The hazard is structural, not exotic: this is a whole-row upsert with no
// optimistic lock, and every OTHER step writer is a load-mutate-save
// (dal.GetTaskStep → mutate one field → dal.PutTaskStep) — update_step_status,
// armStepWithCard (open_gate / create_reply_card auto-bind), the reply-card
// release path, and the reassign step reset. Nothing links those reads to those
// writes, so the upsert asserts EVERY column as that handler read them. With
// the note in the conflict list, an agent reporting a step's status replays the
// note it happened to read a moment earlier — silently destroying a handover
// note the note endpoint had already answered 200 to, which the successor
// session then never sees. "Rare" was not true: measured before the fix, a
// deterministic interleave lost it EVERY time, and two goroutines driving the
// two real endpoints lost it in 12 of 15 sixty-round runs, landing anywhere from
// round 0 to round 54. ⚠️ Those two numbers are not interchangeable — the
// deterministic one is the reliable signal; the concurrent one misses roughly
// one run in five, so a single green run of it proves nothing.
//
// The fix is an OWNERSHIP BOUNDARY rather than a lock or a retry: for a row that
// ALREADY EXISTS, the column is written by exactly one statement —
// SetTaskStepNote, a single-column UPDATE. Single-writer columns cannot be
// clobbered by a stale whole-row copy, because no stale whole-row copy of them
// exists. Guarded by TestTaskStepNoteRaceGuardHasTeeth.
//
// ⚠️ Do not read the surviving INSERT half as a second writer. NO production
// caller reaches it deliberately: all four load an existing row first
// (update_step_status, armStepWithCard, the reply-card release path, the
// reassign step reset), and submit_plan mints its rows through
// ReplaceTaskPlan's own bare INSERT — which is a different statement, not this
// one, and which never carries a conflict clause at all. The INSERT half here
// fires only when a step is deleted between some caller's read and its write,
// and that is the pre-existing RESURRECTION hazard SetTaskStepNote's own godoc
// names — untouched by this change, and the reason that endpoint answers 404
// instead of upserting the row back into existence.
//
// ⚠️ SCOPE, stated so nobody reads more safety into this than is here: this
// removes the hazard for ONE column. EVERY column still named in the ON CONFLICT
// clause below remains a shared-write, last-writer-wins field, and two handlers
// racing on two different ones still lose one of them. That is pre-existing and
// untouched (T-e271 node 6 explicitly did not widen into it) — the same shape
// PutTask documents one table over.
//
// Deliberately phrased as "every column still in that clause" rather than as a
// list of them: an enumeration here would be a second copy of the clause, and
// the copy is what goes stale — the first version of this very paragraph
// already listed six columns while the clause named eleven. The clause below is
// the one source that cannot drift from itself; read it, do not trust a prose
// echo of it.
func (d *DAL) PutTaskStep(st TaskStep) error {
	isGate := 0
	if st.IsGate {
		isGate = 1
	}
	_, err := d.wdb.Exec(`
		INSERT INTO task_step (`+taskStepColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		st.ParallelGroup, isGate, st.ReplyCardID, st.WaitingReason, st.Note,
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
	tx, err := d.wdb.Begin()
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
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			st.ID, st.TaskID, st.OrderIdx, st.Name, st.DoD, st.Status,
			st.ParallelGroup, isGate, st.ReplyCardID, st.WaitingReason, st.Note,
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
	Runtime  string
	Model    string
	// ActualModel / ActualRuntime / ActualEffort are the runtime-REPORTED twins
	// of the owner's configured launch Model / Runtime / Effort. Outsource
	// workers project the same member row, so all three MUST round-trip through
	// memberFromWorker: that function rebuilds a Member from scratch, and any
	// column it forgets is zeroed on the next worker write — which would silently
	// erase a reported value the telemetry path had just stamped.
	ActualModel   string
	ActualRuntime string
	ActualEffort  string
	Effort        string
	TaskID        string
	Status        string // closed set assigned|active|released (derived projection)
	// ActivatedTS is the durable assigned→active anchor (member.activated_ts):
	// 0 = never claimed its task; >0 = the first report_waking claim time (T-4595
	// moved that edge off the retired GET /api/self/task).
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
	// the task's 發包 target, then the type manual), else a concrete machine id.
	// notifyWorkerSpawn prefers this over the manual pref;
	// the relocate handler writes it and re-spawns onto the chosen machine.
	DesiredMachineID string
	// LastMachineID is the STICKY placement anchor (T-98f4, migrations/00039),
	// mirroring member.last_machine_id: the machine this worker's last CONFIRMED
	// session connected from ("" = it has never landed anywhere yet, i.e. the
	// next boot is its first). notifyWorkerSpawn prefers it over the configured
	// (task row / 手冊) arms but below the owner pin, and — unlike the pin —
	// treats it as a soft preference that falls through when that machine cannot
	// currently take the worker. Written only by the SSE first-connect edge; no
	// owner verb writes it directly.
	LastMachineID string
	// SessionBootTS mirrors member.session_boot_ts (T-4235, migrations/00051):
	// the durable anchor for when this worker's CURRENT session first connected,
	// 0 when no session is anchored. Carried through workerFromMember /
	// memberFromWorker for the SAME reason StoppingSince/StoppedSince are — the
	// projection rebuilds a Member from scratch, so a column it forgets is ZEROED
	// by the next outsource write, and zeroing this one hands a live hours-old
	// session back to the boot-storm guard as "just booted".
	SessionBootTS float64
	// RefocusSince is the in-flight context-handover marker (T-32e1,
	// migrations/00019), the worker twin of member.RefocusSince: >0 while a
	// refocus (owner 換手 button OR the context-high auto-handover) is mid-flight,
	// 0 otherwise. Set by both refocus paths, used as the auto-handover cooldown,
	// and cleared by the tick's loop-break once a fresh session boots after it.
	RefocusSince float64
	// RefocusOp names WHICH operation opened that window ("" when none is in
	// flight) — the worker twin of member.refocus_op. Stamped and cleared in
	// lockstep with RefocusSince.
	RefocusOp string
	// ForcedStopAt mirrors member.forced_stop_at (T-a9d6, migrations/00057) —
	// the durable record that this session was CUT OFF rather than collected,
	// and the one field forcedEpochLive reads to decide whether an offboard
	// delta says anything at all.
	//
	// 🔴 It has to be here, not derived, because the projection rebuilds a
	// Member from scratch: for as long as this field did not exist,
	// forcedEpochLive was FALSE for every worker that ever ran, and the silence
	// the owner ruled for a forced stop simply did not apply on this side
	// (T-c996). Deriving it from desired_state instead would be true only for as
	// long as 停止 stays the ONLY writer of offline on a worker — a condition
	// nothing enforces and nothing would report breaking.
	ForcedStopAt float64
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
	// Carry it through every worker projection so lifecycle writes never erase it.
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
		Runtime:          NormalizeRuntime(m.Runtime),
		Model:            m.Model,
		ActualModel:      m.ActualModel,
		ActualRuntime:    m.ActualRuntime,
		ActualEffort:     m.ActualEffort,
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
		LastMachineID:    m.LastMachineID,
		SessionBootTS:    m.SessionBootTS,
		RefocusSince:     m.RefocusSince,
		RefocusOp:        m.RefocusOp,
		StoppingSince:    m.StoppingSince,
		StoppedSince:     m.StoppedSince,
		ForcedStopAt:     m.ForcedStopAt,
		DesiredState:     m.DesiredState,
		BankedCost:       m.BankedCost,
	}
}

// memberFromWorker maps the worker vocabulary back onto a member row (the
// write half). Name mirrors the codename (the outsource display name);
// role_key stays "" (an outsource member classifies as a plain agent — the
// same authz floor the roster-less worker had). Status → roster_status +
// activated_ts: the first write with Status active and no anchor yet stamps
// activated_ts = now (the report_waking claim edge — the only assigned→active
// transition; T-4595 moved it off the retired GET /api/self/task).
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
		Runtime:          NormalizeRuntime(w.Runtime),
		Model:            w.Model,
		ActualModel:      w.ActualModel,
		ActualRuntime:    w.ActualRuntime,
		ActualEffort:     w.ActualEffort,
		Effort:           w.Effort,
		DesiredState:     w.DesiredState,
		DesiredMachineID: w.DesiredMachineID,
		LastMachineID:    w.LastMachineID,
		SessionBootTS:    w.SessionBootTS,
		RefocusSince:     w.RefocusSince,
		RefocusOp:        w.RefocusOp,
		StoppingSince:    w.StoppingSince,
		StoppedSince:     w.StoppedSince,
		ForcedStopAt:     w.ForcedStopAt,
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
	rows, err := d.rdb.Query(`SELECT ` + memberColumns +
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
	row := d.rdb.QueryRow(`SELECT `+memberColumns+
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
	rows, err := d.rdb.Query(`SELECT `+memberColumns+` FROM member
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
		if _, err := d.wdb.Exec(`
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
	if _, err := d.wdb.Exec(`
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
	rows, err := d.rdb.Query(
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
	return getTaskManualOn(d.rdb, typeKey)
}

func getTaskManualOn(q sqlQuerier, typeKey string) (*TaskManual, error) {
	row := q.QueryRow(
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
	return putTaskManualOn(d.wdb, m)
}

func putTaskManualOn(ex sqlExecer, m TaskManual) error {
	_, err := ex.Exec(`
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
//
// The manual's retained history goes in the SAME transaction — see
// DeleteRoleDef for why a half-applied delete is the state being avoided.
func (d *DAL) DeleteTaskManual(typeKey string) (bool, error) {
	var deleted bool
	err := d.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM task_manual WHERE type_key = ?`, typeKey)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = n > 0
		// Both manual streams (T-1f39). The retired four-field bundle is not
		// listed: migration 00045 removed every row of it and nothing can write
		// another, so there is nothing left for this cascade to reach.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_key = ? AND document_kind IN (?, ?)`,
			typeKey, docKindTaskManualSop, docKindTaskManualLearnings)
		return err
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}
