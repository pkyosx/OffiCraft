package main

// dal_task_artifacts.go — the durable data-access layer of the task artifact
// set (migrations/00022): task_artifact, the deliverables an agent pins onto a
// task card (file / image / link). Same convention as dal_tasks.go — explicit
// per-table methods, no generic repository; SSE fan-out stays a handler
// concern. SINCE T-92 EVERY row references the shared chat_attachment blob
// store by attachment_id — a link's target is stored as a text/uri-list blob,
// so there is one content mechanism rather than a blob for two kinds and a
// column for the third, and the `url` column is gone.

import (
	"database/sql"
	"errors"
)

// TaskArtifact mirrors the task_artifact table. AttachmentID is meaningful for
// EVERY kind since T-92 — a link's target lives in a text/uri-list blob like any
// other content — so there is no "exactly one of these two is set" rule left.
//
// 🔴 Name and Description are the two halves of the single `label` this table
// used to carry, and neither is what a reader gets back unchanged. Name is
// EMPTY on most migrated rows and the READ path derives a display name (the
// blob's filename, the link target, then `#`+id); Description holds the whole
// old label, which for 313 rows is longer than the 256-rune write cap — the cap
// binds new writes only, so nothing here may assume it.
type TaskArtifact struct {
	ID           string
	TaskID       string
	Kind         string // closed set 'file' | 'image' | 'link'
	AttachmentID string // chat_attachment.id — every kind, never blank
	Name         string // stored display name; '' means "derive one at read time"
	Description  string // the prose half of the old label; may exceed 256 runes
	CreatedTS    float64
	CreatedBy    string // verified sub of the registrar (§14); '' on none
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

// CountTaskArtifacts counts ONE task's pinned deliverables — the only thing a
// task response says about them since T-92. A COUNT(*) rather than
// len(ListTaskArtifacts(...)) on purpose: the task read is on every接票 path,
// and loading rows in order to throw them away is the cost this change exists
// to remove.
func (d *DAL) CountTaskArtifacts(taskID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM task_artifact WHERE task_id = ?`, taskID).Scan(&n)
	return n, err
}

// GetTaskArtifactBlob resolves the blob half of an artifact projection: the
// mime and the filename always, and the DATA only when the blob is a link
// target (mime text/uri-list).
//
// 🔴 IT IS NOT GetChatAttachment, AND THE DIFFERENCE IS THE POINT. That one
// SELECTs `data` unconditionally, so listing a 120-artifact ticket read 120
// files' bytes into memory to report their names. T-92 makes every kind — links
// included — resolve a blob, which would have widened that from "every file" to
// "every artifact". A link's target genuinely IS its bytes and is tens of
// characters; a report's bytes are megabytes and nothing on this path reads
// them. So the CASE below is what keeps a listing from paging the blob store.
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

// linkTargetMime is the media type a link artifact's blob carries: RFC 2483's
// one-URI-per-line list. It is a real registered type rather than an invented
// marker so the blob can say what it is without a second field elsewhere saying
// it for it.
const linkTargetMime = "text/uri-list"

// PutTaskArtifact inserts one artifact row (the SSE delta is the handler's
// job). Registration mints an id per call, so this is an INSERT, not an upsert;
// the update path for an existing pin is ReplaceTaskArtifact, which keeps the id.
func (d *DAL) PutTaskArtifact(a TaskArtifact) error {
	_, err := d.wdb.Exec(`
		INSERT INTO task_artifact (`+taskArtifactColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TaskID, a.Kind, a.AttachmentID, a.Name, a.Description,
		a.CreatedTS, a.CreatedBy,
	)
	return err
}

// PutTaskArtifactMintingBlob pins one artifact and, when the content arrives as
// BYTES rather than as an id already in the store, mints the blob for it — both
// in ONE transaction.
//
// 🔴 THAT IS THE WHOLE REASON THIS METHOD EXISTS. Uploading and then binding is
// two writes with a gap in between, and a caller that takes the first and not
// the second leaves a blob nothing references — the collector only revisits
// blobs a delete already put on its candidate list, so nothing ever goes looking
// for it. Every pin whose content is new bytes goes through here: the link path
// (a text/uri-list blob minted from the url the caller gave) and the raw-body
// upload path. A pin that REUSES an existing blob passes blob = nil and is one
// insert, which has no gap to begin with.
func (d *DAL) PutTaskArtifactMintingBlob(a TaskArtifact, blob *ChatAttachment) error {
	return d.inTx(func(tx *sql.Tx) error {
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

// ReplaceTaskArtifactMintingBlob is the same guarantee on the replace side: the
// new blob and the swap land together, or neither does.
func (d *DAL) ReplaceTaskArtifactMintingBlob(next TaskArtifact, blob *ChatAttachment) (bool, error) {
	if blob == nil {
		return d.ReplaceTaskArtifact(next)
	}
	var replaced bool
	err := d.inTx(func(tx *sql.Tx) error {
		if err := putChatAttachmentOn(tx, *blob); err != nil {
			return err
		}
		ok, err := replaceTaskArtifactOn(tx, next)
		replaced = ok
		return err
	})
	return replaced, err
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
	Name         string
	Description  string
	CreatedTS    float64
	CreatedBy    string
}

const taskArtifactHistoryColumns = `id, artifact_id, kind, attachment_id, name,
	description, created_ts, created_by`

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
		ok, err := replaceTaskArtifactOn(tx, next)
		replaced = ok
		return err
	})
	return replaced, err
}

// replaceTaskArtifactOn is the three-step swap itself, on a transaction the
// caller already holds — so a replace that must ALSO mint a blob puts both
// inside one transaction rather than two.
func replaceTaskArtifactOn(tx *sql.Tx, next TaskArtifact) (bool, error) {
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
			&h.Name, &h.Description, &h.CreatedTS, &h.CreatedBy); err != nil {
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
