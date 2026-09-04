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
	rows, err := d.rdb.Query(`
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
	return getTaskArtifactOn(d.rdb, id)
}

// getTaskArtifactOn is the same read against any querier, so the write paths
// can re-read the row from INSIDE their transaction (what a replace retains
// must be the state that write actually replaced).
func getTaskArtifactOn(q sqlQuerier, id string) (*TaskArtifact, error) {
	row := q.QueryRow(
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
	rows, err := d.rdb.Query(
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
// job). Registration mints an id per call, so this is an INSERT, not an upsert;
// the update path for an existing pin is ReplaceTaskArtifact, which keeps the id.
func (d *DAL) PutTaskArtifact(a TaskArtifact) error {
	_, err := d.wdb.Exec(`
		INSERT INTO task_artifact (`+taskArtifactColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TaskID, a.Kind, a.AttachmentID, a.URL, a.Label,
		a.CreatedTS, a.CreatedBy,
	)
	return err
}

// DeleteTaskArtifact hard-deletes one artifact by id (the owner's un-pin)
// TOGETHER WITH every retained version of it, in one transaction.
//
// The LIVE row's own chat_attachment blob is deliberately left intact — a blob
// may be shared with a chat message / reply card, and that decree predates
// T-60. Its RETAINED VERSIONS are the opposite case and are collected here: a
// version's blob was written by a replace, is addressable through nothing but
// the artifact id being deleted, and once this row is gone no reader can ever
// name it again. Leaving those behind would be an owner-less version plus a
// blob nothing will ever collect (the survivor scan only ever revisits blobs a
// delete already put on its candidate list).
//
// ⚠️ The old parenthetical here ("the chat-attachment store has no delete path
// either") is FALSE as of T-62a8 and was removed: `DAL.DeleteChatInvolving`
// deletes chat_attachment rows, and it now counts `task_artifact.attachment_id`
// as a live reference — so an artifact row is what KEEPS a blob alive there.
// The standing consequence for the LIVE blob: un-pinning here can leave a blob
// that nothing references and that nothing will ever collect. That bounded leak
// is accepted; changing it means changing this function's contract, which is an
// owner call, not a drive-by.
//
// Returns true iff a row was removed.
func (d *DAL) DeleteTaskArtifact(id string) (bool, error) {
	var removed bool
	err := d.inTx(func(tx *sql.Tx) error {
		live, err := getTaskArtifactOn(tx, id)
		if err != nil {
			return err
		}
		candidates, err := taskArtifactHistoryBlobs(tx,
			`SELECT attachment_id FROM task_artifact_history
			 WHERE artifact_id = ? AND COALESCE(attachment_id, '') <> ''`, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`DELETE FROM task_artifact_history WHERE artifact_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM task_artifact WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		removed = n > 0
		// The live blob keeps its standing exemption even when a retained
		// version happened to point at the same one.
		if live != nil {
			delete(candidates, live.AttachmentID)
		}
		_, err = collectOrphanBlobs(tx, candidates)
		return err
	})
	return removed, err
}

// TaskArtifactHistory is one immutable pre-write snapshot of a replaced
// artifact (T-60, migrations/00071) — the whole replaced version, since an
// artifact version has no prose to hold back: a listing IS the content.
type TaskArtifactHistory struct {
	ID           int64
	ArtifactID   string
	Kind         string
	AttachmentID string
	URL          string
	Label        string
	CreatedTS    float64
	CreatedBy    string
}

const taskArtifactHistoryColumns = `id, artifact_id, kind, attachment_id, url,
	label, created_ts, created_by`

// ReplaceTaskArtifact swaps ONE artifact's content while its id stays put, and
// is the only writer of task_artifact_history. Three steps, one transaction:
// retain the row as it stands right now, overwrite it, then trim the retained
// versions to the newest documentHistoryKeepDefault — the SAME depth the
// document series keeps, read from that one constant rather than a second three
// written down here.
//
// The snapshot is re-read from inside the transaction (retainDocumentVersion's
// rule): the version retained is the state this write actually replaced, not
// whatever the handler read a moment earlier.
//
// The trim is where a blob would otherwise leak: the version that falls off the
// end is the last thing that could ever name its blob, so the blobs of the
// trimmed rows are collected in the same transaction, subject to the same
// whole-schema survivor scan every other collection uses.
//
// next carries the id of the artifact to replace plus the replacement's kind,
// content, CreatedTS and CreatedBy. ⚠️ created_ts/created_by are the CURRENT
// VERSION's facts, not the original registration's — each version is stamped
// with when it was written and by whom, which is what makes the version list
// readable. Consequence worth knowing: ListTaskArtifacts orders by created_ts,
// so a replaced deliverable moves to the end of the card's pin order.
//
// Returns false when the id names no artifact (the handler's 404 is decided
// before this, so that is a lost race rather than the normal path).
func (d *DAL) ReplaceTaskArtifact(next TaskArtifact) (bool, error) {
	var replaced bool
	err := d.inTx(func(tx *sql.Tx) error {
		current, err := getTaskArtifactOn(tx, next.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		replaced = true
		if _, err := tx.Exec(`INSERT INTO task_artifact_history
			(artifact_id, kind, attachment_id, url, label, created_ts, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			current.ID, current.Kind, current.AttachmentID, current.URL,
			current.Label, current.CreatedTS, current.CreatedBy); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE task_artifact
			SET kind = ?, attachment_id = ?, url = ?, label = ?,
			    created_ts = ?, created_by = ?
			WHERE id = ?`,
			next.Kind, next.AttachmentID, next.URL, next.Label,
			next.CreatedTS, next.CreatedBy, next.ID); err != nil {
			return err
		}
		return trimTaskArtifactHistory(tx, next.ID)
	})
	return replaced, err
}

// trimTaskArtifactHistory keeps the newest documentHistoryKeepDefault retained
// versions of one artifact and collects the blobs the dropped ones were the
// last referrer of.
func trimTaskArtifactHistory(tx *sql.Tx, artifactID string) error {
	const doomed = `FROM task_artifact_history
		WHERE artifact_id = ? AND id NOT IN (
			SELECT id FROM task_artifact_history
			WHERE artifact_id = ? ORDER BY id DESC LIMIT ?
		)`
	candidates, err := taskArtifactHistoryBlobs(tx,
		`SELECT attachment_id `+doomed+` AND COALESCE(attachment_id, '') <> ''`,
		artifactID, artifactID, documentHistoryKeepDefault)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE `+doomed,
		artifactID, artifactID, documentHistoryKeepDefault); err != nil {
		return err
	}
	_, err = collectOrphanBlobs(tx, candidates)
	return err
}

// taskArtifactHistoryBlobs runs a one-column attachment_id query and returns
// the non-empty ids as a candidate set.
func taskArtifactHistoryBlobs(tx *sql.Tx, query string, args ...any) (map[string]bool, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// ListTaskArtifactHistory returns one artifact's retained versions, NEWEST
// FIRST (the document-history convention — the reader is choosing how far to
// look back, not replaying the card's pin order).
func (d *DAL) ListTaskArtifactHistory(artifactID string) ([]TaskArtifactHistory, error) {
	rows, err := d.rdb.Query(`
		SELECT `+taskArtifactHistoryColumns+` FROM task_artifact_history
		WHERE artifact_id = ? ORDER BY id DESC`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskArtifactHistory
	for rows.Next() {
		var h TaskArtifactHistory
		if err := rows.Scan(&h.ID, &h.ArtifactID, &h.Kind, &h.AttachmentID,
			&h.URL, &h.Label, &h.CreatedTS, &h.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// TaskArtifactHistoryCounts returns, for ONE task, each of its artifacts'
// retained-version count in a single grouped query — the read face folds a
// version_count per artifact and must not pay a lookup each. Artifacts that
// have never been replaced are simply absent (0, the caller's zero value).
func (d *DAL) TaskArtifactHistoryCounts(taskID string) (map[string]int, error) {
	rows, err := d.rdb.Query(`
		SELECT h.artifact_id, COUNT(*) FROM task_artifact_history h
		JOIN task_artifact a ON a.id = h.artifact_id
		WHERE a.task_id = ? GROUP BY h.artifact_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
