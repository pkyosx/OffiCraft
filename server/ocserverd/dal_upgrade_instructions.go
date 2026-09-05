package main

// dal_upgrade_instructions.go — the durable data-access layer of the upgrade
// instruction set (migrations/00085): upgrade_instruction, the owner's standing
// instructions to the assistant, handed over at every station upgrade until she
// ticks them off. Same convention as dal_task_artifacts.go — explicit
// per-table methods, no generic repository; SSE fan-out stays a handler concern
// and so does every question about WHO may write or tick a row (that identity
// comes from the verified token, never from a column here).

import (
	"database/sql"
)

// UpgradeInstruction mirrors the upgrade_instruction table.
//
// Done is the whole point of the row rather than a convenience: an instruction
// that is not done is handed over again at the NEXT upgrade, and the one after
// that, which is what makes "which upgrade will pick this up" a question nobody
// has to answer. DoneBy/DoneTS are kept because "it was ticked" and "who ticked
// it, when" are different facts and only the second survives a disagreement.
type UpgradeInstruction struct {
	ID        string
	Body      string // what the owner wants done; the only field he authors
	CreatedTS float64
	CreatedBy string // verified sub of the author (§14); '' on none
	Done      bool
	DoneTS    float64 // 0 while open
	DoneBy    string  // verified sub of whoever ticked it; '' while open
}

const upgradeInstructionColumns = `id, body, created_ts, created_by,
	done, done_ts, done_by`

func scanUpgradeInstruction(row interface{ Scan(...any) error }) (UpgradeInstruction, error) {
	var u UpgradeInstruction
	err := row.Scan(
		&u.ID, &u.Body, &u.CreatedTS, &u.CreatedBy,
		&u.Done, &u.DoneTS, &u.DoneBy,
	)
	return u, err
}

// ListOpenUpgradeInstructions returns the instructions still waiting, oldest
// first — the hand-over order, so the assistant reads them in the order they
// were asked for rather than newest-first.
//
// This is the ONE read on the upgrade path and the same read behind the
// cockpit's waiting count, which is why the index leads with done: neither
// caller ever touches a finished row.
func (d *DAL) ListOpenUpgradeInstructions() ([]UpgradeInstruction, error) {
	return listUpgradeInstructionsOn(d.rdb, `WHERE done = 0`)
}

// ListUpgradeInstructions returns every instruction, open ones first and each
// group oldest→newest — the cockpit's view, where a finished instruction is
// still worth seeing (it is the only evidence the work was ever picked up).
func (d *DAL) ListUpgradeInstructions() ([]UpgradeInstruction, error) {
	return listUpgradeInstructionsOn(d.rdb, ``)
}

func listUpgradeInstructionsOn(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, where string) ([]UpgradeInstruction, error) {
	rows, err := q.Query(`
		SELECT ` + upgradeInstructionColumns + ` FROM upgrade_instruction
		` + where + ` ORDER BY done, created_ts, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UpgradeInstruction
	for rows.Next() {
		u, err := scanUpgradeInstruction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetUpgradeInstruction returns one instruction by id, or nil if absent — the
// tick and withdraw guards both need the row before they can tell a 404 apart
// from a refusal.
func (d *DAL) GetUpgradeInstruction(id string) (*UpgradeInstruction, error) {
	row := d.rdb.QueryRow(`
		SELECT `+upgradeInstructionColumns+` FROM upgrade_instruction
		WHERE id = ?`, id)
	u, err := scanUpgradeInstruction(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// PutUpgradeInstruction inserts one instruction row (the SSE delta is the
// handler's job). Writing an instruction mints an id per call, so this is an
// INSERT and never an upsert: an edit would silently change what the assistant
// was already handed at an earlier upgrade.
func (d *DAL) PutUpgradeInstruction(u UpgradeInstruction) error {
	_, err := d.wdb.Exec(`
		INSERT INTO upgrade_instruction (`+upgradeInstructionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Body, u.CreatedTS, u.CreatedBy,
		u.Done, u.DoneTS, u.DoneBy,
	)
	return err
}

// MarkUpgradeInstructionDone ticks one instruction off. Reports whether THIS
// call is the one that closed it.
//
// 🔴 THE FIRST TICK WINS, and the WHERE clause is what enforces it rather than
// a read-then-write: a second tick matches no row, so it can neither overwrite
// who did the work nor move the timestamp. Two sessions of the assistant
// racing on the same instruction is the ordinary case here, not an exotic one —
// she is handed the whole open set at every upgrade.
func (d *DAL) MarkUpgradeInstructionDone(id string, by string, ts float64) (bool, error) {
	res, err := d.wdb.Exec(`
		UPDATE upgrade_instruction SET done = 1, done_ts = ?, done_by = ?
		WHERE id = ? AND done = 0`, ts, by, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteUpgradeInstruction hard-deletes one instruction — the author
// withdrawing something he should not have written.
//
// WHY A WITHDRAW PATH EXISTS AT ALL. Ticking is the assistant's verb and means
// "I did this"; there is no honest way for the owner to use it to retract a
// mistake. Without a delete, an instruction written in error is handed over at
// every single upgrade forever, and the only way to stop it would be to ask the
// assistant to certify work that never happened.
//
// Returns true iff a row was removed.
func (d *DAL) DeleteUpgradeInstruction(id string) (bool, error) {
	res, err := d.wdb.Exec(`DELETE FROM upgrade_instruction WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
