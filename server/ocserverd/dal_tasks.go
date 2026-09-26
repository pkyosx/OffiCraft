package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type Task struct {
	ID                 string
	TypeKey            string
	Title              string
	DedupeKey          string
	Inputs             map[string]any
	Description        string
	Status             string
	Lock               string
	Priority           string
	ExecutorKind       string
	ExecutorID         string
	CreatorID          string
	WaitingReason      string
	CreatedTS          float64
	UpdatedTS          float64
	ClosedTS           float64
	DuplicateOf        string
	ReassignedFrom     string
	ReassignedFromKind string
	HandoverNote       string
	HandoverNoteTS     float64
	HandoverNoteBy     string
	// Outsource* carry one of two meanings, told apart ONLY by OutsourceDispatched:
	// dispatched (explicit target.kind=outsource on create/reassign) is the
	// authoritative spec, which the scheduler mints from ahead of the type manual's
	// assignee spec and without its own spawn gate; not dispatched is a create-time
	// snapshot of the creator's runtime/model/effort/machine, consulted only for
	// fields the live manual leaves unset. Non-empty columns alone do not imply a
	// dispatch.
	OutsourceRuntime    string
	OutsourceModel      string
	OutsourceEffort     string
	OutsourceMachine    string
	OutsourceDispatched bool
	// FrozenBy is the verified sub (wireOwnerID for the owner) of whoever set
	// Priority=frozen; '' when not frozen.
	FrozenBy string
	// KickoffNotifiedTo is VESTIGIAL: the kickoff notice it de-duplicated was
	// withdrawn provisionally (owner rc-a4f6a7f8cd71) and nothing writes it; the
	// column stays so the seam can be restored. A non-empty value is a fossil, not
	// an outstanding notice. Restoring the seam means CLEARING this column first:
	// fossils sit at stamp == executor, so a restored check would swallow the first
	// kickoff of exactly the previously notified tasks.
	KickoffNotifiedTo string
	// ForcedDoneBy is stamped on EVERY force_task_done close and is what marks a
	// close as forced; ForcedDoneReason is optional (owner rc-a92a6252c3bd), so an
	// empty reason does not mean "not forced". Both are '' on every other close,
	// mark_task_done included.
	ForcedDoneBy     string
	ForcedDoneReason string
	// ReadyForDoneVisits counts ARRIVALS in ready_for_done (a task leaves it when a
	// step is added). The 〈任務可結案〉 notice prints it so a second arrival does not
	// read as a duplicate delivery. 0 on pre-column rows.
	ReadyForDoneVisits int
}

const taskColumns = `id, type_key, title, dedupe_key, inputs, description,
	status, lock, priority, executor_kind, executor_id, creator_id, waiting_reason,
	created_ts, updated_ts, closed_ts, duplicate_of,
	reassigned_from, reassigned_from_kind,
	handover_note, handover_note_ts, handover_note_by,
	outsource_runtime, outsource_model, outsource_effort, outsource_machine,
	outsource_dispatched,
	frozen_by, kickoff_notified_to,
	forced_done_by, forced_done_reason, ready_for_done_visits`

// sqlTaskTerminalStatuses is kept in one place so a new terminal state updates
// every open-task filter at once.
const sqlTaskTerminalStatuses = `'done', 'terminated', 'duplicated'`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var inputs string
	var dispatched int
	err := row.Scan(
		&t.ID, &t.TypeKey, &t.Title, &t.DedupeKey, &inputs, &t.Description,
		&t.Status, &t.Lock, &t.Priority, &t.ExecutorKind, &t.ExecutorID, &t.CreatorID,
		&t.WaitingReason,
		&t.CreatedTS, &t.UpdatedTS, &t.ClosedTS, &t.DuplicateOf,
		&t.ReassignedFrom, &t.ReassignedFromKind,
		&t.HandoverNote, &t.HandoverNoteTS, &t.HandoverNoteBy,
		&t.OutsourceRuntime, &t.OutsourceModel, &t.OutsourceEffort, &t.OutsourceMachine,
		&dispatched,
		&t.FrozenBy,
		&t.KickoffNotifiedTo,
		&t.ForcedDoneBy, &t.ForcedDoneReason, &t.ReadyForDoneVisits,
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

// FindOpenTaskByDedupe is the create_task dedupe probe: terminal tasks never
// block a reopen (kyle ruling H2).
func (d *DAL) FindOpenTaskByDedupe(typeKey, dedupeKey string) (*Task, error) {
	row := d.rdb.QueryRow(`
		SELECT `+taskColumns+` FROM task
		WHERE type_key = ? AND dedupe_key = ?
		  AND status NOT IN (`+sqlTaskTerminalStatuses+`)
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

func (d *DAL) ListOpenTasksByExecutor(executorID string, limit int) ([]Task, error) {
	rows, err := d.rdb.Query(`
		SELECT `+taskColumns+` FROM task
		WHERE executor_id = ? AND status NOT IN (`+sqlTaskTerminalStatuses+`)
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

func (d *DAL) CountOpenTasksByExecutor(executorID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM task
		WHERE executor_id = ? AND status NOT IN (`+sqlTaskTerminalStatuses+`)`,
		executorID).Scan(&n)
	return n, err
}

func (d *DAL) CountOpenTasksOfType(typeKey string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`
		SELECT COUNT(*) FROM task
		WHERE type_key = ? AND status NOT IN (`+sqlTaskTerminalStatuses+`)`,
		typeKey).Scan(&n)
	return n, err
}

// CountTasksDuplicatingOriginal backs mark_task_duplicated's chain guard: a task
// that is already an original cannot itself be marked duplicated. With the
// "target is not itself duplicated" check (api_tasks.go) this keeps duplicate_of
// depth-1, so the cockpit link resolves in one hop.
func (d *DAL) CountTasksDuplicatingOriginal(originalID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM task WHERE duplicate_of = ?`, originalID).Scan(&n)
	return n, err
}

func (d *DAL) PutTask(t Task) error { return putTaskOn(d.wdb, t, taskWriteUpsert) }

type taskWriteMode int

const (
	taskWriteUpsert taskWriteMode = iota

	// taskWriteInsertOnly is CreateTaskMintingID's mode: a freshly minted id that
	// already exists means task_id_seq handed a number out twice, and the INSERT
	// must fail on the primary key. With the conflict clause the mint silently
	// overwrote the earlier task and still answered 200 (measured: a row lost).
	taskWriteInsertOnly
)

func putTaskOn(ex sqlExecer, t Task, mode taskWriteMode) error {
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
	stmt := `
		INSERT INTO task (` + taskColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if mode == taskWriteUpsert {
		stmt += taskUpsertConflictClause
	}
	_, err = ex.Exec(stmt,
		t.ID, t.TypeKey, t.Title, t.DedupeKey, string(blob), t.Description,
		t.Status, t.Lock, t.Priority, t.ExecutorKind, t.ExecutorID, t.CreatorID,
		t.WaitingReason,
		t.CreatedTS, t.UpdatedTS, t.ClosedTS, t.DuplicateOf,
		t.ReassignedFrom, t.ReassignedFromKind,
		t.HandoverNote, t.HandoverNoteTS, t.HandoverNoteBy,
		NormalizeRuntime(t.OutsourceRuntime),
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine,
		dispatched,
		t.FrozenBy,
		t.KickoffNotifiedTo,
		t.ForcedDoneBy, t.ForcedDoneReason, t.ReadyForDoneVisits,
	)
	return err
}

// taskUpsertConflictClause deliberately omits `description` and `title`; do not
// add them back. Task writers are load-mutate-save with no optimistic lock, so
// every listed column is replayed as the handler read it: with description
// listed, a priority change silently undid a description edit already answered
// 200 (measured: a deterministic interleave lost it every time). Those two
// columns are written only by SetTaskDescriptionOn / SetTaskTitleOn and by the
// create INSERT. Every column still listed remains last-writer-wins.
const taskUpsertConflictClause = `
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
			closed_ts = excluded.closed_ts,
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
			frozen_by = excluded.frozen_by,
			kickoff_notified_to = excluded.kickoff_notified_to,
			forced_done_by = excluded.forced_done_by,
			forced_done_reason = excluded.forced_done_reason,
			ready_for_done_visits = excluded.ready_for_done_visits`

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

func (d *DAL) AddTaskDep(taskID, blockedBy string) error {
	_, err := d.wdb.Exec(
		`INSERT OR IGNORE INTO task_dep (task_id, blocked_by) VALUES (?, ?)`,
		taskID, blockedBy)
	return err
}

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

type TaskStep struct {
	ID            string
	TaskID        string
	OrderIdx      int
	Name          string
	DoD           string
	Status        string
	ParallelGroup string
	IsGate        bool
	ReplyCardID   string
	WaitingReason string
	Note          string
	StartedTS     float64
	FinishedTS    float64
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

type TaskStepProgress struct {
	Done  int
	Total int
}

// AllTaskStepProgress is the SQL twin of domain.go TaskProgress; keep the two
// agreeing.
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

type TaskCurrentStep struct {
	ID   string
	Name string
}

// AllTaskCurrentStep is the SQL twin of domain.go CurrentStep; keep the two
// agreeing.
func (d *DAL) AllTaskCurrentStep() (map[string]TaskCurrentStep, error) {
	rows, err := d.rdb.Query(`
		SELECT task_id, id, name FROM (
		  SELECT task_id, id, name,
		         ROW_NUMBER() OVER (
		           PARTITION BY task_id ORDER BY order_idx, id) AS rn
		    FROM task_step
		   WHERE status != ? AND status != ?
		) WHERE rn = 1`, StepStatusDone, StepStatusSuperseded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TaskCurrentStep{}
	for rows.Next() {
		var taskID string
		var cur TaskCurrentStep
		if err := rows.Scan(&taskID, &cur.ID, &cur.Name); err != nil {
			return nil, err
		}
		out[taskID] = cur
	}
	return out, rows.Err()
}

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

// taskDescriptionOn / taskTitleOn must read inside the writing transaction, not
// reuse the handler's earlier read: the retained history revision has to be the
// state this write replaced (see SaveWithDocumentHistory).
func taskDescriptionOn(q sqlRowQuerier, id string) (string, bool, error) {
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

func taskTitleOn(q sqlRowQuerier, id string) (string, bool, error) {
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

// TouchTaskUpdatedTS exists because an open cockpit task card re-reads its step
// detail only when updated_ts changes (the SSE task delta carries no steps), so
// a step-only write such as a note must bump it or stay invisible.
func (d *DAL) TouchTaskUpdatedTS(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE task SET updated_ts = ? WHERE id = ?`, ts, id)
	return err
}

func (d *DAL) PutTaskStep(st TaskStep) error { return putTaskStepOn(d.wdb, st) }

// putTaskStepOn's conflict clause deliberately omits `note`; do not add it back.
// Other step writers load-mutate-save through here, so a listed note replays a
// stale copy over a handover note already answered 200 (measured: a
// deterministic interleave lost it every time; the concurrent repro misses about
// one run in five, so one green run proves nothing). For existing rows the note
// is written only by SetTaskStepNote, whose single-column UPDATE also cannot
// resurrect a step deleted between read and write the way this upsert can.
// Every column still listed remains last-writer-wins.
func putTaskStepOn(ex sqlExecer, st TaskStep) error {
	isGate := 0
	if st.IsGate {
		isGate = 1
	}
	_, err := ex.Exec(`
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

// ReplaceTaskSteps: which rows to retain / supersede is the handler's call (it joins
// reply_card); the DAL never reads the card table. A frozen row keeps started_ts
// and reply_card_id so its question-and-answer history still renders.
func (d *DAL) ReplaceTaskSteps(taskID string, retain, supersede []string,
	supersededTS float64, newSteps []TaskStep) ([]TaskStep, error) {
	existing, err := d.ListTaskSteps(taskID)
	if err != nil {
		return nil, err
	}
	preserved := map[string]bool{}
	for _, id := range retain {
		preserved[id] = true
	}
	superseded := map[string]bool{}
	for _, id := range supersede {
		preserved[id] = true
		superseded[id] = true
	}
	tx, err := d.wdb.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var kept []TaskStep
	for _, st := range existing {
		if !StepIsTerminal(st.Status) && !preserved[st.ID] {
			if _, err := tx.Exec(
				`DELETE FROM task_step WHERE id = ?`, st.ID); err != nil {
				return nil, err
			}
			continue
		}
		if superseded[st.ID] {
			st.Status = StepStatusSuperseded
			st.FinishedTS = supersededTS
		}
		kept = append(kept, st)
	}
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

// OutsourceWorker is the worker-vocabulary view of a kind='outsource' member row
// (the outsource_worker table was folded into member, migrations/00025). The
// vocabulary stays because the frozen wire DTO and the scheduler speak it;
// Status is derived, never stored.
type OutsourceWorker Member

func workerStatusFrom(rosterStatus string, activatedTS float64) string {
	if rosterStatus == RosterStatusRemoved {
		return WorkerStatusReleased
	}
	if activatedTS > 0 {
		return WorkerStatusActive
	}
	return WorkerStatusAssigned
}

func workerFromMember(m Member) OutsourceWorker {
	taskID := ""
	if m.LinkedTaskID != nil {
		taskID = *m.LinkedTaskID
	}
	w := OutsourceWorker(m)
	w.Name = ""
	w.Kind = ""
	w.RoleKey = ""
	w.RosterStatus = ""
	w.LinkedTaskID = nil
	w.HandoverNoticedTS = 0
	w.AgentIatFloor = 0
	w.TokenKeyID = ""
	w.Runtime = NormalizeRuntime(m.Runtime)
	w.TaskID = taskID
	w.Status = workerStatusFrom(m.RosterStatus, m.ActivatedTS)
	return w
}

// memberFromWorker: role_key stays "" so an outsource member gets the plain
// agent authz floor. The first write with Status active stamps activated_ts —
// the report_waking claim, the only assigned→active edge.
func memberFromWorker(w OutsourceWorker) Member {
	m := Member(w)
	identity := Member{
		Name:    w.Codename,
		Kind:    KindOutsource,
		RoleKey: "",
		Runtime: NormalizeRuntime(w.Runtime),
	}
	m.Name = identity.Name
	m.Kind = identity.Kind
	m.RoleKey = identity.RoleKey
	m.Runtime = identity.Runtime
	m.RosterStatus = RosterStatusActive
	if w.Status == WorkerStatusReleased {
		m.RosterStatus = RosterStatusRemoved
	}
	switch w.Status {
	case WorkerStatusAssigned:
		m.ActivatedTS = 0
	case WorkerStatusActive:
		if m.ActivatedTS == 0 {
			m.ActivatedTS = nowSecs()
		}
	}
	taskID := w.TaskID
	m.LinkedTaskID = &taskID
	m.TaskID = ""
	m.Status = ""
	return m
}

// ListOutsourceWorkers includes removed rows on purpose: codename MAX+1
// folds over the full set so a codename is never reused.
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

func (d *DAL) PutOutsourceWorker(w OutsourceWorker) error {
	return d.PutMember(memberFromWorker(w))
}

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

// ReleaseWorkerByID releases by worker id, not task: the deferred handover
// dismiss must fire only the predecessor, and after an outsource→outsource
// takeover the successor is already bound to the same task_id.
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

type TaskManual struct {
	TypeKey     string
	DisplayName string
	Purpose     string
	Fields      string
	SopMD       string
	Assignee    string // JSON object; "{}" = unset
	UpdatedTS   float64
}

const taskManualColumns = `type_key, purpose, fields, sop_md,
	assignee, updated_ts, display_name`

func scanTaskManual(row interface{ Scan(...any) error }) (TaskManual, error) {
	var m TaskManual
	err := row.Scan(
		&m.TypeKey, &m.Purpose, &m.Fields, &m.SopMD,
		&m.Assignee, &m.UpdatedTS, &m.DisplayName,
	)
	return m, err
}

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

func (d *DAL) GetTaskManual(typeKey string) (*TaskManual, error) {
	return getTaskManualOn(d.rdb, typeKey)
}

func getTaskManualOn(q sqlRowQuerier, typeKey string) (*TaskManual, error) {
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

func (d *DAL) PutTaskManual(m TaskManual) error {
	return putTaskManualOn(d.wdb, m)
}

func putTaskManualOn(ex sqlExecer, m TaskManual) error {
	_, err := ex.Exec(`
		INSERT INTO task_manual (`+taskManualColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (type_key) DO UPDATE SET
			purpose = excluded.purpose, fields = excluded.fields,
			sop_md = excluded.sop_md,
			assignee = excluded.assignee, updated_ts = excluded.updated_ts,
			display_name = excluded.display_name`,
		m.TypeKey, m.Purpose, m.Fields, m.SopMD,
		m.Assignee, m.UpdatedTS, m.DisplayName,
	)
	return err
}

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
		// Only the SOP stream: migration 00045 removed the retired four-field
		// bundle's history rows, and nothing can write them any more.
		_, err = tx.Exec(`DELETE FROM document_history
			WHERE document_key = ? AND document_kind = ?`,
			typeKey, docKindTaskManualSop)
		return err
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}
