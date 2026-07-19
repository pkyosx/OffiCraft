package main

// dal_task_artifacts.go — the durable data-access layer of the task artifact
// set (migrations/00022): task_artifact, the deliverables an agent pins onto a
// task card (file / image / link). Same convention as dal_tasks.go — explicit
// per-table methods, no generic repository; SSE fan-out stays a handler
// concern. File/image rows reference the shared chat_attachment blob store by
// attachment_id (one blob mechanism, not two); link rows carry a bare url.

import (
	"database/sql"
	"errors"
)

// TaskArtifact mirrors the task_artifact table. Exactly one of AttachmentID
// (kind file/image) or URL (kind link) is meaningful — the other is "".
type TaskArtifact struct {
	ID           string
	TaskID       string
	Kind         string // closed set 'file' | 'image' | 'link'
	AttachmentID string // chat_attachment.id for file/image; '' for link
	URL          string // the link url; '' for file/image
	Label        string // display label / link title; blob filename is the fallback
	CreatedTS    float64
	CreatedBy    string // verified sub of the registrar (§14); '' on none
}

const taskArtifactColumns = `id, task_id, kind, attachment_id, url, label,
	created_ts, created_by`

func scanTaskArtifact(row interface{ Scan(...any) error }) (TaskArtifact, error) {
	var a TaskArtifact
	err := row.Scan(
		&a.ID, &a.TaskID, &a.Kind, &a.AttachmentID, &a.URL, &a.Label,
		&a.CreatedTS, &a.CreatedBy,
	)
	return a, err
}

// ListTaskArtifacts returns one task's artifacts, oldest→newest (the curated
// pin order — created_ts, id tiebreak for determinism). The full-task read
// face folds these; the light list only needs the count (see CountArtifacts...).
func (d *DAL) ListTaskArtifacts(taskID string) ([]TaskArtifact, error) {
	rows, err := d.db.Query(`
		SELECT `+taskArtifactColumns+` FROM task_artifact
		WHERE task_id = ? ORDER BY created_ts, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskArtifact
	for rows.Next() {
		a, err := scanTaskArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetTaskArtifact returns one artifact by id, or nil if absent (the remove
// guard: a 404 vs a wrong-task 403 needs the row first).
func (d *DAL) GetTaskArtifact(id string) (*TaskArtifact, error) {
	row := d.db.QueryRow(
		`SELECT `+taskArtifactColumns+` FROM task_artifact WHERE id = ?`, id)
	a, err := scanTaskArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AllTaskArtifactCounts returns every task's artifact count in one grouped
// COUNT query — the light-list badge source (GET /api/tasks), which never
// loads the artifact rows themselves. Tasks with none are simply absent from
// the map (0 — the caller's zero value), mirroring AllTaskStepProgress.
func (d *DAL) AllTaskArtifactCounts() (map[string]int, error) {
	rows, err := d.db.Query(
		`SELECT task_id, COUNT(*) FROM task_artifact GROUP BY task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var taskID string
		var n int
		if err := rows.Scan(&taskID, &n); err != nil {
			return nil, err
		}
		out[taskID] = n
	}
	return out, rows.Err()
}

// PutTaskArtifact inserts one artifact row (the SSE delta is the handler's
// job). Registration is append-only — an id is minted per call, so this is an
// INSERT, not an upsert (no natural update path for a pinned deliverable).
func (d *DAL) PutTaskArtifact(a TaskArtifact) error {
	_, err := d.db.Exec(`
		INSERT INTO task_artifact (`+taskArtifactColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TaskID, a.Kind, a.AttachmentID, a.URL, a.Label,
		a.CreatedTS, a.CreatedBy,
	)
	return err
}

// DeleteTaskArtifact hard-deletes one artifact by id (the owner's un-pin). The
// referenced chat_attachment blob is deliberately left intact — a blob may be
// shared with a chat message/reply card, and blob GC is not this system's
// concern (the chat-attachment store has no delete path either). Returns true
// iff a row was removed.
func (d *DAL) DeleteTaskArtifact(id string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM task_artifact WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
