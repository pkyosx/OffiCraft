package main

import (
	"database/sql"
	"errors"
)

// Every kind references a chat_attachment blob by AttachmentID; a link's
// target is stored as a text/uri-list blob.
//
// 🔴 Name is EMPTY on most migrated rows (empty = the READ path derives a
// display name), and Description may exceed the 256-rune write cap (the cap
// binds new writes only), so nothing here may assume it.
type TaskArtifact struct {
	ID           string
	TaskID       string
	Kind         string
	AttachmentID string
	Name         string
	Description  string
	CreatedTS    float64
	CreatedBy    string
}

const taskArtifactColumns = `id, task_id, kind, attachment_id, name, description,
	created_ts, created_by`

func scanTaskArtifact(row interface{ Scan(...any) error }) (TaskArtifact, error) {
	var a TaskArtifact
	err := row.Scan(
		&a.ID, &a.TaskID, &a.Kind, &a.AttachmentID, &a.Name, &a.Description,
		&a.CreatedTS, &a.CreatedBy,
	)
	return a, err
}

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

func (d *DAL) GetTaskArtifact(id string) (*TaskArtifact, error) {
	return getTaskArtifactOn(d.rdb, id)
}

func getTaskArtifactOn(q sqlRowQuerier, id string) (*TaskArtifact, error) {
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

func (d *DAL) CountTaskArtifacts(taskID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM task_artifact WHERE task_id = ?`, taskID).Scan(&n)
	return n, err
}

// 🔴 Not GetChatAttachment on purpose: that one SELECTs `data` unconditionally,
// and the CASE below is what keeps an artifact listing from reading every
// file's bytes. Only a link target's (tiny) bytes are returned.
func (d *DAL) GetTaskArtifactBlob(id string) (*ChatAttachment, error) {
	var a ChatAttachment
	var filename sql.NullString
	var data []byte
	err := d.rdb.QueryRow(`
		SELECT id, mime, filename,
		       CASE WHEN mime = ? THEN data ELSE NULL END
		  FROM chat_attachment WHERE id = ?`, linkTargetMime, id,
	).Scan(&a.ID, &a.Mime, &filename, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Data = data
	if filename.Valid {
		a.Filename = &filename.String
	}
	return &a, nil
}

const linkTargetMime = "text/uri-list"

func (d *DAL) PutTaskArtifact(a TaskArtifact) error {
	_, err := d.wdb.Exec(`
		INSERT INTO task_artifact (`+taskArtifactColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TaskID, a.Kind, a.AttachmentID, a.Name, a.Description,
		a.CreatedTS, a.CreatedBy,
	)
	return err
}

// 🔴 Blob and pin land in ONE transaction: the collector only revisits blobs a
// delete already put on its candidate list, so a blob uploaded but never bound
// would never be found.
func (d *DAL) PutTaskArtifactMintingBlob(a TaskArtifact, blob *ChatAttachment) error {
	return d.inTx(func(tx *writeTx) error {
		if blob != nil {
			if err := putChatAttachmentOn(tx, *blob); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`
			INSERT INTO task_artifact (`+taskArtifactColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.TaskID, a.Kind, a.AttachmentID, a.Name, a.Description,
			a.CreatedTS, a.CreatedBy)
		return err
	})
}

func (d *DAL) ReplaceTaskArtifactMintingBlob(next TaskArtifact, blob *ChatAttachment) (bool, error) {
	if blob == nil {
		return d.ReplaceTaskArtifact(next)
	}
	var replaced bool
	err := d.inTx(func(tx *writeTx) error {
		if err := putChatAttachmentOn(tx, *blob); err != nil {
			return err
		}
		ok, err := replaceTaskArtifactOn(tx, next)
		replaced = ok
		return err
	})
	return replaced, err
}

// The LIVE row's own blob is deliberately left intact for file/image — it may
// be shared with a chat message / reply card — so un-pinning can leave a blob
// nothing will ever collect; that bounded leak is accepted and changing it is
// an owner call. A LINK's blob is exempt from that (owner rc-27107ca914a7): it
// joins the candidates and collectOrphanBlobs' survivor scan decides, since
// migration 00086 deduped identical targets and two artifacts CAN share one.
func (d *DAL) DeleteTaskArtifact(id string) (bool, error) {
	var removed bool
	err := d.inTx(func(tx *writeTx) error {
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
		if live != nil {
			if live.Kind == ArtifactKindLink {
				if live.AttachmentID != "" {
					candidates[live.AttachmentID] = true
				}
			} else {
				delete(candidates, live.AttachmentID)
			}
		}
		_, err = collectOrphanBlobs(tx, candidates)
		return err
	})
	return removed, err
}

type TaskArtifactHistory struct {
	ID           int64
	ArtifactID   string
	Kind         string
	AttachmentID string
	Name         string
	Description  string
	CreatedTS    float64
	CreatedBy    string
}

const taskArtifactHistoryColumns = `id, artifact_id, kind, attachment_id, name,
	description, created_ts, created_by`

// ⚠️ created_ts/created_by become the CURRENT version's facts, and
// ListTaskArtifacts orders by created_ts, so a replaced deliverable moves to
// the end of the card's pin order.
func (d *DAL) ReplaceTaskArtifact(next TaskArtifact) (bool, error) {
	var replaced bool
	err := d.inTx(func(tx *writeTx) error {
		ok, err := replaceTaskArtifactOn(tx, next)
		replaced = ok
		return err
	})
	return replaced, err
}

func replaceTaskArtifactOn(tx *writeTx, next TaskArtifact) (bool, error) {
	var replaced bool
	err := func() error {
		current, err := getTaskArtifactOn(tx, next.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		replaced = true
		if _, err := tx.Exec(`INSERT INTO task_artifact_history
			(artifact_id, kind, attachment_id, name, description, created_ts, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			current.ID, current.Kind, current.AttachmentID, current.Name,
			current.Description, current.CreatedTS, current.CreatedBy); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE task_artifact
			SET kind = ?, attachment_id = ?, name = ?, description = ?,
			    created_ts = ?, created_by = ?
			WHERE id = ?`,
			next.Kind, next.AttachmentID, next.Name, next.Description,
			next.CreatedTS, next.CreatedBy, next.ID); err != nil {
			return err
		}
		return trimTaskArtifactHistory(tx, next.ID)
	}()
	return replaced, err
}

func trimTaskArtifactHistory(tx *writeTx, artifactID string) error {
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

func taskArtifactHistoryBlobs(tx *writeTx, query string, args ...any) (map[string]bool, error) {
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
			&h.Name, &h.Description, &h.CreatedTS, &h.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

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
