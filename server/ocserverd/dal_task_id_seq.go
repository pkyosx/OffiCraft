package main

// dal_task_id_seq.go — a new task id is "T-" + the next integer off a one-row
// counter (migrations/00060). Old "t-" + 12-hex ids are NOT migrated; both
// formats coexist (task.id is a plain TEXT PRIMARY KEY, and no table carries a
// foreign key into task).

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

const taskIDPrefix = "T-"

// mintRetryLimit: the retries are NOT spent against an external writer — a
// second handle is stopped at BEGIN by busy_timeout and never reaches the
// counter. Uniqueness today is carried by the TRANSACTION (IMMEDIATE on the
// one-connection write pool), not the CAS: deleting `AND next = ?` alone changed
// nothing observable; removing the transaction too lost 19 of 32 rows while
// every request answered 200.
// The CAS is insurance for a mint moved out of the transaction or a widened
// pool. Dropping `_txlock=immediate` surfaces as SQLITE_BUSY_SNAPSHOT (a 500),
// not as retries. Bounded so a reachable loop fails loudly instead of hanging.
const mintRetryLimit = 64

// CreateTaskMintingID mints and INSERTS in ONE transaction; the halves are not
// separable — a mint read on one statement and written on another lets two
// handlers both mint T-7. The insert is taskWriteInsertOnly, so a duplicate
// trips the primary key and 500s (an upsert silently overwrote the first task).
// The property is UNIQUENESS, not contiguity: gaps and reused numbers are fine.
//
// precheck runs after the id exists and before the row lands; it runs INSIDE the
// write transaction on the single write connection, so it must not touch the
// database (a write deadlocks, a read comes from another pool at another time).
// Return AbortCreate(err) to refuse; the error is returned unchanged so the
// caller can answer 403 rather than 500.
func (d *DAL) CreateTaskMintingID(t Task, precheck func(id string) error) (Task, error) {
	err := d.inTx(func(tx *sql.Tx) error {
		var err error
		t, err = createTaskMintingIDOn(tx, t, precheck)
		return err
	})
	return t, err
}

func createTaskMintingIDOn(tx *sql.Tx, t Task, precheck func(id string) error) (Task, error) {
	n, err := mintTaskNumber(tx)
	if err != nil {
		return t, err
	}
	t.ID = taskIDPrefix + strconv.Itoa(n)

	if precheck != nil {
		if err := precheck(t.ID); err != nil {
			return t, err
		}
	}
	return t, putTaskOn(tx, t, taskWriteInsertOnly)
}

func mintTaskNumber(tx *sql.Tx) (int, error) {
	for attempt := 0; attempt < mintRetryLimit; attempt++ {
		var next int
		if err := tx.QueryRow(
			`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, errors.New("task_id_seq row is missing — database not migrated")
			}
			return 0, err
		}
		res, err := tx.Exec(
			`UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = ?`, next)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n == 1 {
			return next, nil
		}
	}
	return 0, fmt.Errorf(
		"could not claim a task number in %d attempts — every compare-and-set on "+
			"task_id_seq reported 0 rows, so something is advancing the counter "+
			"between this transaction's read and its claim",
		mintRetryLimit)
}

var errOutsourceGateDenied = errors.New("outsource spawn gate denied")
