package main

import (
	"fmt"
)

// Single-step plan edits (insert / delete / reorder), unlike the wholesale
// replaceTaskStepsOn, keep every other row's id, status, note and bound card.
//
// 🔴 THEY WRITE `order_idx` AND NOTHING ELSE on existing rows: `note` has
// exactly one writer (SetTaskStepNote), and a whole-row rewrite here would be a
// second, stale writer that silently loses a note. Adding a column to these
// UPDATEs re-opens that hazard.
//
// ⚠️ NO OVERWRITE PROTECTION, by owner ruling rc-5160b97384c4: concurrent writes
// leave the later one standing, silently.
//
// `order_idx` has no unique index (migrations/00004_tasks.sql), so shifts and a
// reorder can write one row at a time without a temporary shuffle.

func insertTaskStepOn(tx *writeTx, taskID string, at int, st TaskStep) ([]TaskStep, error) {
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
	return listTaskStepsOn(tx, taskID)
}

func deleteTaskStepOn(tx *writeTx, taskID, stepID string) ([]TaskStep, error) {
	var at int
	if err := tx.QueryRow(
		`SELECT order_idx FROM task_step WHERE task_id = ? AND id = ?`,
		taskID, stepID).Scan(&at); err != nil {
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
	return listTaskStepsOn(tx, taskID)
}

// reorderTaskStepsOn: the caller validates that orderedIDs is exactly the task's
// step set and that no terminal row moves; this only writes.
func reorderTaskStepsOn(tx *writeTx, taskID string, orderedIDs []string) ([]TaskStep, error) {
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
	return listTaskStepsOn(tx, taskID)
}
