package main

import "fmt"

// Single-step plan edits (T-228). submit_plan / ReplaceTaskSteps stays exactly as
// it was: a WHOLESALE replace that deletes every unfinished row and mints new
// ids for whatever the fresh plan re-lists. These three writes are the opposite
// shape — they move ONE thing and leave every other row's id, status, note and
// bound reply card untouched — which is the whole reason they exist: a mid-task
// adjustment used to cost the executor every unfinished step's working note and
// any gate step still waiting for the owner's answer.
//
// 🔴 THEY WRITE `order_idx` AND NOTHING ELSE on rows that already exist. That is
// not tidiness, it is the ownership boundary PutTaskStep's godoc describes: the
// `note` column has exactly one writer (SetTaskStepNote), and a whole-row
// rewrite here would put a second, stale writer on it — a note written between
// some caller's read and this write would simply vanish. Adding a column to
// these UPDATEs re-opens that hazard.
//
// ⚠️ NO OVERWRITE PROTECTION, by owner ruling (rc-5160b97384c4, 2026-09-16):
// nothing here carries a version, compares a prior value or retries. Two writes
// landing together leave the later one standing and the earlier one gone,
// silently. That is the decision of record, not an oversight — the tool
// descriptions say so in as many words.
//
// 🔴 NONE OF THE THREE RE-IMPLEMENTS ReplaceTaskSteps'S ORDERING RULES, and that
// is deliberate: what a plan PRESERVES (done rows, already-superseded history,
// answered-card rows) and where the preserved prefix lands is a partition, and a
// second copy of a partition is a rule that drifts. These writes partition
// nothing. Insert and delete SHIFT the rows on one side of a position by one, so
// the stored contiguity they inherit is the contiguity they leave; only reorder
// assigns indexes outright, which is the entire thing it is for. The one
// invariant all four writers share — order_idx is the contiguous range 0..n-1 in
// timeline order — is asserted by test rather than shared as code, because the
// assertion is the thing that must not drift, and a shared 4-line loop would put
// the guarantee in the caller anyway.
//
// `order_idx` has no unique index (migrations/00004_tasks.sql), so a reorder can
// assign the new indexes one row at a time without a temporary shuffle.

// InsertTaskStep inserts st at timeline position `at` — 0 puts it first, and
// len(steps) appends — shifting every row at or after that position down by one.
// The caller mints the id and validates the position; the DAL only writes.
// Transactional; returns the resulting full step list in timeline order.
func (d *DAL) InsertTaskStep(taskID string, at int, st TaskStep) ([]TaskStep, error) {
	tx, err := d.wdb.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.Exec(`
		UPDATE task_step SET order_idx = order_idx + 1
		 WHERE task_id = ? AND order_idx >= ?`, taskID, at); err != nil {
		return nil, err
	}
	st.TaskID = taskID
	st.OrderIdx = at
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
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ListTaskSteps(taskID)
}

// DeleteTaskStep removes one step and closes the gap it leaves by shifting every
// later row up by one — the exact inverse of the insert shift, so the stored
// order_idx range stays contiguous without renumbering the whole timeline. The
// caller decides whether the step MAY go (terminal rows may not, and a plan may
// not empty); the DAL only writes. Transactional; returns the full step list.
func (d *DAL) DeleteTaskStep(taskID, stepID string) ([]TaskStep, error) {
	tx, err := d.wdb.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var at int
	if err := tx.QueryRow(
		`SELECT order_idx FROM task_step WHERE task_id = ? AND id = ?`,
		taskID, stepID).Scan(&at); err != nil {
		// Includes sql.ErrNoRows: the step went away between the handler's read
		// and this write. Fail rather than commit a shift over a timeline nobody
		// asked to touch — the same call this makes as a note written to a step
		// a concurrent replan deleted, which 404s instead of resurrecting it.
		return nil, fmt.Errorf("task %s: step %s: %w", taskID, stepID, err)
	}
	if _, err := tx.Exec(
		`DELETE FROM task_step WHERE task_id = ? AND id = ?`,
		taskID, stepID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		UPDATE task_step SET order_idx = order_idx - 1
		 WHERE task_id = ? AND order_idx > ?`, taskID, at); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ListTaskSteps(taskID)
}

// ReorderTaskSteps writes orderedIDs as the task's timeline order: element i
// becomes order_idx i. The caller validates that orderedIDs is exactly the
// task's step set and that no terminal row changes position; the DAL only
// writes, and it writes order_idx alone — no row is deleted, re-inserted or
// otherwise rebuilt, so every id, status, note and bound card survives.
// Transactional; returns the resulting full step list in timeline order.
func (d *DAL) ReorderTaskSteps(taskID string, orderedIDs []string) ([]TaskStep, error) {
	tx, err := d.wdb.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for i, id := range orderedIDs {
		res, err := tx.Exec(
			`UPDATE task_step SET order_idx = ? WHERE task_id = ? AND id = ?`,
			i, taskID, id)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, fmt.Errorf("task %s: step %s no longer exists", taskID, id)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ListTaskSteps(taskID)
}
