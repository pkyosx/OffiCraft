package main

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	secretActionReveal = "reveal"
	secretActionDenied = "denied"

	secretAccessLogDefaultLimit = 200
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
}

type SecretAccessLogEntry struct {
	ID         int64
	Action     string
	SecretID   string
	SecretName string
	MemberID   string
	MachineID  string
	TS         float64
}

type SecretAccessLogFilter struct {
	SecretID string
	MemberID string
	Action   string
	Limit    int
}

const secretColumns = `id, name, note, nonce, value_ct, key_version, created_ts, updated_ts`

func scanSecret(row interface{ Scan(...any) error }) (Secret, error) {
	var s Secret
	err := row.Scan(&s.ID, &s.Name, &s.Note, &s.Nonce, &s.ValueCT, &s.KeyVersion, &s.CreatedTS, &s.UpdatedTS)
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
	_, err := ex.Exec(`INSERT INTO secret (`+secretColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Note, s.Nonce, s.ValueCT, s.KeyVersion, s.CreatedTS, s.UpdatedTS)
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

func insertSecretAccessLogOn(ex sqlExecer, action, secretID, memberID, machineID string, ts float64) error {
	_, err := ex.Exec(`INSERT INTO secret_access_log (action, secret_id, member_id, machine_id, ts)
		VALUES (?, ?, ?, ?, ?)`, action, secretID, memberID, machineID, ts)
	return err
}

func (d *DAL) ListSecretAccessLog(f SecretAccessLogFilter) ([]SecretAccessLogEntry, error) {
	var where []string
	var args []any
	if f.SecretID != "" {
		where = append(where, "l.secret_id = ?")
		args = append(args, f.SecretID)
	}
	if f.MemberID != "" {
		where = append(where, "l.member_id = ?")
		args = append(args, f.MemberID)
	}
	if f.Action != "" {
		where = append(where, "l.action = ?")
		args = append(args, f.Action)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = secretAccessLogDefaultLimit
	}
	query := `SELECT l.id, l.action, l.secret_id, COALESCE(s.name, ''), l.member_id, l.machine_id, l.ts
		FROM secret_access_log l LEFT JOIN secret s ON s.id = l.secret_id`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY l.ts DESC, l.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.rdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SecretAccessLogEntry
	for rows.Next() {
		var e SecretAccessLogEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.SecretID, &e.SecretName, &e.MemberID, &e.MachineID, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
