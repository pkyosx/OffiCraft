package main

import (
	"database/sql"
	"errors"
)

const (
	shareLinkKindAttachment = "attachment"
	shareLinkKindDiff       = "diff"
)

type ShareLink struct {
	ID           string
	Kind         string
	TokenHash    string
	AttachmentID *string
	DiffBefore   *string
	DiffAfter    *string
	LabelBefore  *string
	LabelAfter   *string
	CreatedBy    string
	CreatedTS    float64
	ExpiresTS    *float64
	// Read through a join; not a column of share_link.
	AttachmentFilename *string
}

type shareLinkFilter struct {
	AttachmentID string
	Before       string
	After        string
}

const shareLinkSelect = `SELECT sl.id, sl.kind, sl.token_hash, sl.attachment_id, sl.diff_before,
	sl.diff_after, sl.label_before, sl.label_after, sl.created_by, sl.created_ts, sl.expires_ts,
	ca.filename
	FROM share_link sl LEFT JOIN chat_attachment ca ON ca.id = sl.attachment_id`

// A link whose expires_ts the clock has reached, or whose attachment the blob
// collector has deleted, is treated as gone everywhere, whether or not the
// prune has deleted its row yet. Blobs are deleted on several paths; this
// predicate and the prune are what keep share_link in step with all of them.
const shareLinkLive = `(sl.expires_ts IS NULL OR sl.expires_ts > ?)
	AND (sl.kind <> 'attachment' OR ca.id IS NOT NULL)`

func scanShareLink(row interface{ Scan(...any) error }) (ShareLink, error) {
	var l ShareLink
	err := row.Scan(&l.ID, &l.Kind, &l.TokenHash, &l.AttachmentID, &l.DiffBefore, &l.DiffAfter,
		&l.LabelBefore, &l.LabelAfter, &l.CreatedBy, &l.CreatedTS, &l.ExpiresTS, &l.AttachmentFilename)
	return l, err
}

func oneShareLink(row *sql.Row) (*ShareLink, error) {
	l, err := scanShareLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (d *DAL) InsertShareLink(l ShareLink) error {
	_, err := d.wdb.Exec(`INSERT INTO share_link (id, kind, token_hash, attachment_id, diff_before,
		diff_after, label_before, label_after, created_by, created_ts, expires_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.Kind, l.TokenHash, l.AttachmentID, l.DiffBefore, l.DiffAfter,
		l.LabelBefore, l.LabelAfter, l.CreatedBy, l.CreatedTS, l.ExpiresTS)
	return err
}

func (d *DAL) GetLiveShareLinkByTokenHash(tokenHash string, now float64) (*ShareLink, error) {
	return oneShareLink(d.rdb.QueryRow(shareLinkSelect+` WHERE sl.token_hash = ? AND `+shareLinkLive,
		tokenHash, now))
}

func getLiveShareLinkOn(q sqlRowQuerier, id string, now float64) (*ShareLink, error) {
	return oneShareLink(q.QueryRow(shareLinkSelect+` WHERE sl.id = ? AND `+shareLinkLive, id, now))
}

func (d *DAL) ListLiveShareLinks(f shareLinkFilter, now float64) ([]ShareLink, error) {
	where, args := ` WHERE `+shareLinkLive, []any{now}
	if f.AttachmentID != "" {
		where += ` AND sl.kind = 'attachment' AND sl.attachment_id = ?`
		args = append(args, f.AttachmentID)
	}
	if f.Before != "" {
		where += ` AND sl.kind = 'diff' AND sl.diff_before = ? AND sl.diff_after = ?`
		args = append(args, f.Before, f.After)
	}
	rows, err := d.rdb.Query(shareLinkSelect+where+` ORDER BY sl.created_ts, sl.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShareLink{}
	for rows.Next() {
		l, err := scanShareLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func deleteShareLinkOn(ex sqlExecer, id string) error {
	_, err := ex.Exec(`DELETE FROM share_link WHERE id = ?`, id)
	return err
}

func (d *DAL) PruneDeadShareLinks(now float64) (int64, error) {
	res, err := d.wdb.Exec(`DELETE FROM share_link
		WHERE (expires_ts IS NOT NULL AND expires_ts <= ?)
		OR (kind = 'attachment' AND NOT EXISTS
			(SELECT 1 FROM chat_attachment ca WHERE ca.id = share_link.attachment_id))`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
