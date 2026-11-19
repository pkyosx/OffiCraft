package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

// sharedDocPreviousVersionsKept counts PREVIOUS versions only: a document holds
// at most this many plus its current one.
const sharedDocPreviousVersionsKept = 3

type SharedDoc struct {
	ID           string
	Kind         string
	AttachmentID string
	Name         string
	Description  string
	CreatedTS    float64
	CreatedBy    string
	UpdatedTS    float64
	UpdatedBy    string
	// ContentUpdatedTS/By move only with the content, never on a rename: they
	// stamp the version this content becomes once it is replaced.
	ContentUpdatedTS float64
	ContentUpdatedBy string
}

type SharedDocVersion struct {
	ID           int64
	DocID        string
	Kind         string
	AttachmentID string
	Name         string
	Description  string
	CreatedTS    float64
	CreatedBy    string
}

const sharedDocColumns = `id, kind, attachment_id, name, description,
	created_ts, created_by, updated_ts, updated_by, content_updated_ts, content_updated_by`

const sharedDocVersionColumns = `id, doc_id, kind, attachment_id, name,
	description, created_ts, created_by`

func scanSharedDoc(row interface{ Scan(...any) error }) (SharedDoc, error) {
	var d SharedDoc
	err := row.Scan(&d.ID, &d.Kind, &d.AttachmentID, &d.Name, &d.Description,
		&d.CreatedTS, &d.CreatedBy, &d.UpdatedTS, &d.UpdatedBy,
		&d.ContentUpdatedTS, &d.ContentUpdatedBy)
	return d, err
}

func scanSharedDocVersion(row interface{ Scan(...any) error }) (SharedDocVersion, error) {
	var v SharedDocVersion
	err := row.Scan(&v.ID, &v.DocID, &v.Kind, &v.AttachmentID, &v.Name,
		&v.Description, &v.CreatedTS, &v.CreatedBy)
	return v, err
}

func (d *DAL) ListSharedDocs() ([]SharedDoc, error) {
	rows, err := d.rdb.Query(`SELECT ` + sharedDocColumns + ` FROM shared_doc ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SharedDoc
	for rows.Next() {
		doc, err := scanSharedDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (d *DAL) GetSharedDoc(id string) (*SharedDoc, error) {
	return getSharedDocOn(d.rdb, id)
}

func getSharedDocOn(q sqlRowQuerier, id string) (*SharedDoc, error) {
	doc, err := scanSharedDoc(q.QueryRow(
		`SELECT `+sharedDocColumns+` FROM shared_doc WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (d *DAL) SharedDocVersionCounts() (map[string]int, error) {
	rows, err := d.rdb.Query(
		`SELECT doc_id, COUNT(*) FROM shared_doc_history GROUP BY doc_id`)
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

func (d *DAL) ListSharedDocVersions(docID string) ([]SharedDocVersion, error) {
	rows, err := d.rdb.Query(`SELECT `+sharedDocVersionColumns+`
		FROM shared_doc_history WHERE doc_id = ? ORDER BY id DESC`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SharedDocVersion
	for rows.Next() {
		v, err := scanSharedDocVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// 🔴 A minted link blob lands in the SAME transaction as the row that pins it:
// the collector only revisits blobs a delete or trim names, so a blob stored
// but never bound would never be found.
func (d *DAL) CreateSharedDoc(doc SharedDoc, minted *ChatAttachment) error {
	return d.inTx(func(tx *writeTx) error {
		if minted != nil {
			if err := putChatAttachmentOn(tx, *minted); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO shared_doc (`+sharedDocColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			doc.ID, doc.Kind, doc.AttachmentID, doc.Name, doc.Description,
			doc.CreatedTS, doc.CreatedBy, doc.UpdatedTS, doc.UpdatedBy,
			doc.ContentUpdatedTS, doc.ContentUpdatedBy)
		return err
	})
}

func sharedDocInTx(tx *writeTx, id string) (*SharedDoc, error) {
	doc, err := getSharedDocOn(tx, id)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, refuseInTx(http.StatusNotFound, sharedDocNotFound(id))
	}
	return doc, nil
}

func sharedDocNotFound(id string) string {
	return "shared doc '" + id + "' not found"
}

func retainSharedDocVersion(tx *writeTx, current SharedDoc) error {
	_, err := tx.Exec(`INSERT INTO shared_doc_history
		(doc_id, kind, attachment_id, name, description, created_ts, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		current.ID, current.Kind, current.AttachmentID, current.Name,
		current.Description, current.ContentUpdatedTS, current.ContentUpdatedBy)
	return err
}

func writeSharedDocCurrent(tx *writeTx, next SharedDoc) error {
	_, err := tx.Exec(`UPDATE shared_doc
		SET attachment_id = ?, name = ?, description = ?, updated_ts = ?, updated_by = ?,
			content_updated_ts = ?, content_updated_by = ?
		WHERE id = ?`,
		next.AttachmentID, next.Name, next.Description, next.UpdatedTS, next.UpdatedBy,
		next.ContentUpdatedTS, next.ContentUpdatedBy, next.ID)
	return err
}

// SharedDocReplacement is new content, a new name or description, or both. An
// empty AttachmentID, a nil Name or a nil Description keeps what the row holds
// INSIDE the replacing transaction: a value read earlier would quietly undo a
// change that landed in between.
type SharedDocReplacement struct {
	ID           string
	AttachmentID string
	Name         *string
	Description  *string
	UpdatedTS    float64
	UpdatedBy    string
}

// ReplaceSharedDoc makes new content a version: the displaced one is retained
// and the oldest beyond the retention depth goes with its blob. A name or
// description edit alone changes the current row in place and costs no version.
// Refusals come back as txRefusal.
func (d *DAL) ReplaceSharedDoc(r SharedDocReplacement, minted *ChatAttachment) error {
	return d.inTx(func(tx *writeTx) error {
		current, err := sharedDocInTx(tx, r.ID)
		if err != nil {
			return err
		}
		next := *current
		next.UpdatedTS, next.UpdatedBy = r.UpdatedTS, r.UpdatedBy
		if r.Name != nil {
			next.Name = *r.Name
		}
		if r.Description != nil {
			next.Description = *r.Description
		}
		newContent := r.AttachmentID != ""
		if !newContent {
			if next.Name == current.Name && next.Description == current.Description {
				return refuseInTx(http.StatusBadRequest, sharedDocNothingChanged)
			}
			return writeSharedDocCurrent(tx, next)
		}
		next.AttachmentID = r.AttachmentID
		next.ContentUpdatedTS, next.ContentUpdatedBy = r.UpdatedTS, r.UpdatedBy
		if minted != nil {
			if err := putChatAttachmentOn(tx, *minted); err != nil {
				return err
			}
		}
		if err := retainSharedDocVersion(tx, *current); err != nil {
			return err
		}
		if err := writeSharedDocCurrent(tx, next); err != nil {
			return err
		}
		return trimSharedDocHistory(tx, r.ID)
	})
}

const sharedDocNothingChanged = "nothing to replace: the name and description sent are the current ones"

// RestoreSharedDocVersion MOVES the version out of history and the displaced
// current version in as the newest history row, so the count never changes and
// nothing is trimmed. Only the content comes back: the current name and
// description stay.
func (d *DAL) RestoreSharedDocVersion(docID string, versionID int64, actor string, now float64) error {
	return d.inTx(func(tx *writeTx) error {
		current, err := sharedDocInTx(tx, docID)
		if err != nil {
			return err
		}
		v, err := scanSharedDocVersion(tx.QueryRow(`SELECT `+sharedDocVersionColumns+`
			FROM shared_doc_history WHERE id = ? AND doc_id = ?`, versionID, docID))
		if errors.Is(err, sql.ErrNoRows) {
			return refuseInTx(http.StatusNotFound, sharedDocVersionNotFound(docID, versionID))
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM shared_doc_history WHERE id = ?`, v.ID); err != nil {
			return err
		}
		if err := retainSharedDocVersion(tx, *current); err != nil {
			return err
		}
		next := *current
		next.AttachmentID = v.AttachmentID
		next.UpdatedTS, next.UpdatedBy = now, actor
		next.ContentUpdatedTS, next.ContentUpdatedBy = now, actor
		return writeSharedDocCurrent(tx, next)
	})
}

func sharedDocVersionNotFound(docID string, versionID int64) string {
	return "version " + strconv.FormatInt(versionID, 10) + " is not a retained version of shared doc '" + docID + "'"
}

func trimSharedDocHistory(tx *writeTx, docID string) error {
	const doomed = `FROM shared_doc_history
		WHERE doc_id = ? AND id NOT IN (
			SELECT id FROM shared_doc_history
			WHERE doc_id = ? ORDER BY id DESC LIMIT ?
		)`
	candidates, err := taskArtifactHistoryBlobs(tx,
		`SELECT attachment_id `+doomed, docID, docID, sharedDocPreviousVersionsKept)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE `+doomed, docID, docID, sharedDocPreviousVersionsKept); err != nil {
		return err
	}
	_, err = collectOrphanBlobs(tx, candidates)
	return err
}

// DeleteSharedDoc removes a document and every retained version; all their
// blobs go to the collector, which keeps any another row still cites.
func (d *DAL) DeleteSharedDoc(id string) error {
	return d.inTx(func(tx *writeTx) error {
		doc, err := sharedDocInTx(tx, id)
		if err != nil {
			return err
		}
		candidates, err := taskArtifactHistoryBlobs(tx,
			`SELECT attachment_id FROM shared_doc_history WHERE doc_id = ?`, id)
		if err != nil {
			return err
		}
		if doc.AttachmentID != "" {
			candidates[doc.AttachmentID] = true
		}
		if _, err := tx.Exec(`DELETE FROM shared_doc_history WHERE doc_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM shared_doc WHERE id = ?`, id); err != nil {
			return err
		}
		_, err = collectOrphanBlobs(tx, candidates)
		return err
	})
}
