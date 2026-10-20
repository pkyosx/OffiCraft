package main

import (
	"database/sql"
	"errors"
)

type Passkey struct {
	ID              string
	CredentialID    []byte
	RPID            string
	Origin          string
	PublicKey       []byte
	AttestationType string
	AAGUID          []byte
	SignCount       uint32
	CloneWarningTS  *float64
	Transports      string
	Attachment      string
	BackupEligible  bool
	BackupState     bool
	Name            string
	CreatedTS       float64
	LastUsedTS      *float64
}

const passkeyColumns = `id, credential_id, rp_id, origin, public_key, attestation_type, aaguid,
	sign_count, clone_warning_ts, transports, attachment, backup_eligible, backup_state,
	name, created_ts, last_used_ts`

func scanPasskey(row interface{ Scan(...any) error }) (Passkey, error) {
	var p Passkey
	err := row.Scan(&p.ID, &p.CredentialID, &p.RPID, &p.Origin, &p.PublicKey, &p.AttestationType,
		&p.AAGUID, &p.SignCount, &p.CloneWarningTS, &p.Transports, &p.Attachment,
		&p.BackupEligible, &p.BackupState, &p.Name, &p.CreatedTS, &p.LastUsedTS)
	return p, err
}

func (d *DAL) ListPasskeys() ([]Passkey, error) {
	rows, err := d.rdb.Query(`SELECT ` + passkeyColumns + ` FROM passkey ORDER BY created_ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func countPasskeysOn(q sqlRowQuerier) (int, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM passkey`).Scan(&n)
	return n, err
}

func getPasskeyOn(q sqlRowQuerier, id string) (*Passkey, error) {
	return onePasskey(q.QueryRow(`SELECT `+passkeyColumns+` FROM passkey WHERE id = ?`, id))
}

// The rp_id half is what keeps a passkey registered under one host from
// signing in on another.
func (d *DAL) GetPasskeyByCredential(credentialID []byte, rpID string) (*Passkey, error) {
	return onePasskey(d.rdb.QueryRow(`SELECT `+passkeyColumns+` FROM passkey
		WHERE credential_id = ? AND rp_id = ?`, credentialID, rpID))
}

func passkeyCredentialExistsOn(q sqlRowQuerier, credentialID []byte) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM passkey WHERE credential_id = ?`, credentialID).Scan(&n)
	return n > 0, err
}

func onePasskey(row *sql.Row) (*Passkey, error) {
	p, err := scanPasskey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func insertPasskeyOn(ex sqlExecer, p Passkey) error {
	_, err := ex.Exec(`INSERT INTO passkey (`+passkeyColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.CredentialID, p.RPID, p.Origin, p.PublicKey, p.AttestationType, p.AAGUID,
		p.SignCount, p.CloneWarningTS, p.Transports, p.Attachment, p.BackupEligible, p.BackupState,
		p.Name, p.CreatedTS, p.LastUsedTS)
	return err
}

func deletePasskeyOn(ex sqlExecer, id string) error {
	_, err := ex.Exec(`DELETE FROM passkey WHERE id = ?`, id)
	return err
}

func deleteAllPasskeysOn(ex sqlExecer) (int64, error) {
	res, err := ex.Exec(`DELETE FROM passkey`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Reports false when the row is gone (deleted between lookup and update).
func (d *DAL) MarkPasskeySignedIn(id string, signCount uint32, backupState bool, ts float64) (bool, error) {
	res, err := d.wdb.Exec(`UPDATE passkey SET sign_count = ?, backup_state = ?, last_used_ts = ?
		WHERE id = ?`, signCount, backupState, ts, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (d *DAL) MarkPasskeyCloneWarning(id string, ts float64) error {
	_, err := d.wdb.Exec(`UPDATE passkey SET clone_warning_ts = ? WHERE id = ?`, ts, id)
	return err
}
