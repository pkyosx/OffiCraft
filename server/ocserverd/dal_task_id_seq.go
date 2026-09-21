package main

// dal_task_id_seq.go — T-52917b 遞增票號: where a NEW task's id comes from.
//
// A task id used to be "t-" + 12 random hex. It is now "T-" + the next integer
// off a one-row counter table (migrations/00060). Old ids are NOT migrated and
// nothing here reads them: the two formats coexist because task.id is a plain
// TEXT PRIMARY KEY with no CHECK, no COLLATE and no length limit, GetTask is a
// byte-exact `WHERE id = ?`, and no table carries a foreign key into task.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// taskIDPrefix is the ONE place the new id's shape is written down.
const taskIDPrefix = "T-"

// mintRetryLimit bounds the compare-and-set loop below.
//
// 🔴 THIS COMMENT USED TO SAY THE RETRIES ARE SPENT AGAINST AN "EXTERNAL WRITER"
// — a `sqlite3` shell, a stray `ocserverd migrate`. THAT WAS FALSE, and the
// T-52917b review killed it by measurement. An external writer never reaches the
// counter at all: while this transaction is open it is stopped one layer lower,
// at BEGIN. Measured on this tree, a second handle on the same file:
//
//	EXTERNAL Begin() after 5.099858166s: err=database is locked (5) (SQLITE_BUSY)
//
// 5s is the busy_timeout (openSQLite). It never gets to run a statement, so it
// can never make our CAS report 0, so it can never spend a single retry.
//
// WHAT THE LOOP ACTUALLY GUARDS is the two ways this code could stop being
// written the way it is written today. Both were measured:
//
//  1. THE MINT MOVED OUT OF THE TRANSACTION. Read and CAS then run as two
//     separate autocommit statements with the connection back in the pool in
//     between. A competing advance lands between them and our CAS reports
//     `RowsAffected=0` — the loop engages, re-reads, and hands out a DIFFERENT
//     number instead of a duplicate. This is the case the loop exists for.
//  2. A CONFIGURATION REGRESSION: `_txlock=immediate` dropped from the DSN, so
//     Begin becomes DEFERRED and the write lock is no longer taken up front.
//     ⚠️ Measured, and it does NOT come out through this loop: an outside writer
//     does move the counter under a DEFERRED reader (777µs, 1 row), but our
//     following CAS then fails outright with `database is locked (517)`
//     (SQLITE_BUSY_SNAPSHOT) rather than reporting 0 rows. mintTaskNumber
//     returns that error and the create 500s. Loud, correct, no duplicate — but
//     it exits through the error return above, not through a retry. Nobody
//     should expect this loop to absorb that regression.
//
// 🔴 AND THE CONCLUSION TO KEEP: THE TRANSACTION IS WHAT IS CARRYING THIS, NOT
// THE CAS. Under today's settings the CAS cannot be measured to be stopping
// anything — the write pool is ONE connection (SetMaxOpenConns(1)) and Begin is
// IMMEDIATE, so among our own writers the CAS can never lose. Confirmed by
// mutation on the previous head: DELETING the `AND next = ?` condition alone
// changed NOTHING observable, every test stayed green across three runs. That is
// not an argument for deleting it — it is insurance against (1) above, and
// against a future in which the write pool is widened. It is simply not the
// mechanism doing today's work. Take the transaction away as well and the same
// 32-request probe lost 19 rows while every request still answered 200.
//
// A bound rather than a bare `for` so that if the loop ever DOES become
// reachable, the request fails loudly instead of hanging a connection forever.
const mintRetryLimit = 64

// CreateTaskMintingID mints the next id and INSERTS the task under it, both in
// ONE transaction.
//
// 🔴 THE TWO HALVES ARE NOT SEPARABLE. Minting on its own connection and then
// inserting on another would be correct-but-lossy on a crash (a burned number,
// survivable) and, worse, it invites the next reader to "simplify" the CAS away
// because a single-connection write pool makes a bare `next = next + 1` look
// safe. It is not: the READ that decides which number this task gets happens on
// a different statement from the write, the connection goes back to the pool in
// between, and two handlers that both read 7 both mint T-7.
//
// Since the T-52917b review the second T-7 at least FAILS LOUDLY: the insert
// below runs as a plain INSERT (taskWriteInsertOnly), so it trips task.id's
// primary key, rolls this transaction back and 500s. It used to go through
// PutTask's `ON CONFLICT (id) DO UPDATE` upsert, which did not error — it
// OVERWROTE the first task while the API answered 200, and the damage was an
// invisible MISSING ROW. A loud 500 is the floor, not a licence to weaken the
// mint: the point is still to never hand the same number out twice.
//
// The claim is a compare-and-set on the value THIS transaction just read:
//
//	UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = <what I read>
//
// 1 row affected ⇒ the number is mine. 0 ⇒ somebody moved it; re-read and try
// again. The whole claim rests on this driver's RowsAffected being exact.
//
// The property being defended is UNIQUENESS, not contiguity. A precheck refusal
// or a failed insert rolls the counter back and the number is reused; an
// external writer advancing it leaves a gap. Neither hurts. Two tasks called
// T-7 does.
//
// precheck runs AFTER the id exists and BEFORE the row lands, so a gate that
// needs the id can see it and a refusal still leaves no orphan task. It may be
// nil. 🔴 It runs INSIDE the write transaction on the single write connection,
// so it must not touch the database — any write would deadlock against the
// transaction holding the lock, and any read would be served from a different
// pool at a different point in time. Return AbortCreate(err) to refuse.
//
// A precheck error rolls the transaction back and is returned UNCHANGED, so the
// caller can tell its own refusal from a database failure with errors.Is and
// answer 403 rather than 500.
//
// It returns the task with ID filled in.
func (d *DAL) CreateTaskMintingID(t Task, precheck func(id string) error) (Task, error) {
	// Begin on the write pool ⇒ BEGIN IMMEDIATE (openSQLite's `_txlock=immediate`,
	// migrate.go). The write lock is taken UP FRONT, which is what makes the
	// read-then-CAS below a genuine critical section rather than a hopeful one.
	tx, err := d.wdb.Begin()
	if err != nil {
		return t, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	n, err := mintTaskNumber(tx)
	if err != nil {
		return t, err
	}
	t.ID = taskIDPrefix + strconv.Itoa(n)

	if precheck != nil {
		if err := precheck(t.ID); err != nil {
			return t, err // rollback returns the number
		}
	}
	if err := putTaskOn(tx, t, taskWriteInsertOnly); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

// mintTaskNumber claims the next number on an OPEN transaction. Split out so
// the CAS is testable on its own and so nothing can call it without a tx in
// hand: it takes the tx, it does not open one.
func mintTaskNumber(tx *sql.Tx) (int, error) {
	for attempt := 0; attempt < mintRetryLimit; attempt++ {
		var next int
		if err := tx.QueryRow(
			`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// The single row is a schema invariant (migrations/00060 seeds it and
				// the CHECK forbids a second). Absent ⇒ the file is not the schema we
				// think it is; say so rather than inventing a counter.
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
	// Reaching here means the CAS reported 0 rows mintRetryLimit times running.
	// Under the settings described on mintRetryLimit that should be unreachable,
	// which is exactly why it must fail loudly rather than loop: something about
	// how the mint is wired has changed. The caller turns this into a 500, and
	// because the whole mint lives in the create transaction, the rollback leaves
	// NO orphan task row.
	return 0, fmt.Errorf(
		"could not claim a task number in %d attempts — every compare-and-set on "+
			"task_id_seq reported 0 rows, so something is advancing the counter "+
			"between this transaction's read and its claim",
		mintRetryLimit)
}

// errOutsourceGateDenied is the sentinel the create handler's spawn-gate
// precheck refuses with, so a 403 stays a 403 after travelling out through
// CreateTaskMintingID's error return instead of becoming a 500.
var errOutsourceGateDenied = errors.New("outsource spawn gate denied")
