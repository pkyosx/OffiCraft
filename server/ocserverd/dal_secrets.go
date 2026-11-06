package main

import (
	"database/sql"
	"errors"
)

// StoredSecret carries the sealed value only; nothing in the DAL ever sees plaintext.
type StoredSecret struct {
	ID         string
	Name       string
	Note       string
	Nonce      []byte
	ValueCT    []byte
	KeyVersion int
	CreatedTS  float64
	UpdatedTS  float64
	// nil until the first successful reveal.
	LastAccessedTS *float64
}

const secretColumns = `id, name, note, nonce, value_ct, key_version, created_ts, updated_ts, last_accessed_ts`

func scanSecret(row interface{ Scan(...any) error }) (StoredSecret, error) {
	var s StoredSecret
	err := row.Scan(&s.ID, &s.Name, &s.Note, &s.Nonce, &s.ValueCT, &s.KeyVersion, &s.CreatedTS, &s.UpdatedTS, &s.LastAccessedTS)
	return s, err
}

func (d *DAL) ListSecrets() ([]StoredSecret, error) {
	rows, err := d.rdb.Query(`SELECT ` + secretColumns + ` FROM secret ORDER BY created_ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredSecret
	for rows.Next() {
		s, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountSealedValues counts every row the vault key seals, in both tables: a key
// minted while either holds a row would orphan that row.
func (d *DAL) CountSealedValues() (int, error) {
	var n int
	err := d.rdb.QueryRow(`SELECT (SELECT COUNT(*) FROM secret) + (SELECT COUNT(*) FROM reply_card_secret)`).Scan(&n)
	return n, err
}

func getSecretOn(q sqlRowQuerier, id string) (*StoredSecret, error) {
	s, err := scanSecret(q.QueryRow(`SELECT `+secretColumns+` FROM secret WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func insertSecretOn(ex sqlExecer, s StoredSecret) error {
	_, err := ex.Exec(`INSERT INTO secret (`+secretColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Note, s.Nonce, s.ValueCT, s.KeyVersion, s.CreatedTS, s.UpdatedTS, s.LastAccessedTS)
	return err
}

func updateSecretOn(ex sqlExecer, s StoredSecret) error {
	_, err := ex.Exec(`UPDATE secret SET name = ?, note = ?, nonce = ?, value_ct = ?,
		key_version = ?, updated_ts = ? WHERE id = ?`,
		s.Name, s.Note, s.Nonce, s.ValueCT, s.KeyVersion, s.UpdatedTS, s.ID)
	return err
}

func deleteSecretOn(ex sqlExecer, id string) error {
	_, err := ex.Exec(`DELETE FROM secret WHERE id = ?`, id)
	return err
}

func markSecretAccessedOn(ex sqlExecer, id string, ts float64) error {
	_, err := ex.Exec(`UPDATE secret SET last_accessed_ts = ? WHERE id = ?`, ts, id)
	return err
}

// ReplyCardSecret is a secret card's sealed value. Only the provide, reveal and
// destroy paths touch it; it is never joined onto a ReplyCard.
type ReplyCardSecret struct {
	CardID     string
	Nonce      []byte
	ValueCT    []byte
	KeyVersion int
}

func getReplyCardSecretOn(q sqlRowQuerier, cardID string) (*ReplyCardSecret, error) {
	var c ReplyCardSecret
	err := q.QueryRow(`SELECT card_id, nonce, value_ct, key_version FROM reply_card_secret WHERE card_id = ?`, cardID).
		Scan(&c.CardID, &c.Nonce, &c.ValueCT, &c.KeyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func insertReplyCardSecretOn(ex sqlExecer, c ReplyCardSecret) error {
	_, err := ex.Exec(`INSERT INTO reply_card_secret (card_id, nonce, value_ct, key_version) VALUES (?, ?, ?, ?)`,
		c.CardID, c.Nonce, c.ValueCT, c.KeyVersion)
	return err
}

func deleteReplyCardSecretOn(ex sqlExecer, cardID string) error {
	_, err := ex.Exec(`DELETE FROM reply_card_secret WHERE card_id = ?`, cardID)
	return err
}

// ListReplyCardsHoldingSecret answers the cards with live ciphertext, without
// the ciphertext.
func (d *DAL) ListReplyCardsHoldingSecret() ([]ReplyCard, error) {
	rows, err := d.rdb.Query(`SELECT ` + replyCardColumns + ` FROM reply_card
		WHERE id IN (SELECT card_id FROM reply_card_secret) ORDER BY created_ts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReplyCard
	for rows.Next() {
		c, err := scanReplyCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
