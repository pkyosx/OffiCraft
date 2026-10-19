package main

import (
	"database/sql"
	"errors"
)

// Secret carries the sealed value only; nothing in the DAL ever sees plaintext.
type Secret struct {
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

func scanSecret(row interface{ Scan(...any) error }) (Secret, error) {
	var s Secret
	err := row.Scan(&s.ID, &s.Name, &s.Note, &s.Nonce, &s.ValueCT, &s.KeyVersion, &s.CreatedTS, &s.UpdatedTS, &s.LastAccessedTS)
	return s, err
}

func (d *DAL) ListSecrets() ([]Secret, error) {
	rows, err := d.rdb.Query(`SELECT ` + secretColumns + ` FROM secret ORDER BY created_ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		s, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DAL) CountSecrets() (int, error) {
	var n int
	err := d.rdb.QueryRow(`SELECT COUNT(*) FROM secret`).Scan(&n)
	return n, err
}

func getSecretOn(q sqlRowQuerier, id string) (*Secret, error) {
	s, err := scanSecret(q.QueryRow(`SELECT `+secretColumns+` FROM secret WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func insertSecretOn(ex sqlExecer, s Secret) error {
	_, err := ex.Exec(`INSERT INTO secret (`+secretColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Note, s.Nonce, s.ValueCT, s.KeyVersion, s.CreatedTS, s.UpdatedTS, s.LastAccessedTS)
	return err
}

func updateSecretOn(ex sqlExecer, s Secret) error {
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
