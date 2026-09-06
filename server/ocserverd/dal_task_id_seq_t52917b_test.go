package main

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"path/filepath"
	"sync"
	"testing"
)

// ── T-52917b: the two facts the design rests on, MEASURED ────────────────────

// TestRowsAffectedIsExactOnThisDriver measures what the ticket says nobody has
// measured: whether `sql.Result.RowsAffected()` on modernc.org/sqlite reports
// the EXACT number of rows an UPDATE changed.
//
// It matters because the whole mint is a compare-and-set that reads "1" as "the
// number is mine" and "0" as "somebody beat me". If the driver over-reported —
// answered 1 for a WHERE that matched nothing — every claim would look like a
// win and two transactions would mint the same number. Six-plus places in this
// tree already decide on this value; this is the first one that measures it.
//
// The three cases are the three answers the CAS can get.
func TestRowsAffectedIsExactOnThisDriver(t *testing.T) {
	db := openTaskSeqTestDB(t)

	// ① a matching CAS ⇒ exactly 1.
	res, err := db.Exec(`UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = 1`)
	if err != nil {
		t.Fatalf("cas: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("matching CAS reported %d rows (err %v), want exactly 1 — the "+
			"mint reads this value as \"the number is mine\"", n, err)
	}

	// ② a NON-matching CAS ⇒ exactly 0, never 1. This is the direction that
	// would be fatal: a driver answering 1 here hands the same number twice.
	res, err = db.Exec(`UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = 1`)
	if err != nil {
		t.Fatalf("stale cas: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 0 {
		t.Fatalf("STALE CAS reported %d rows (err %v), want exactly 0 — a driver "+
			"that says 1 here would mint the same task number twice", n, err)
	}

	// ③ and it is a ROW count, not a "did anything run" flag: an UPDATE that
	// matches several rows must report several.
	if _, err := db.Exec(`CREATE TABLE ra_probe (v INTEGER)`); err != nil {
		t.Fatalf("probe table: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`INSERT INTO ra_probe (v) VALUES (0)`); err != nil {
			t.Fatalf("probe insert: %v", err)
		}
	}
	res, err = db.Exec(`UPDATE ra_probe SET v = 1`)
	if err != nil {
		t.Fatalf("probe update: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 5 {
		t.Fatalf("multi-row UPDATE reported %d (err %v), want 5 — RowsAffected "+
			"must be a row COUNT, not a boolean", n, err)
	}
}

// TestWritePoolIsCappedAtOneConnection measures the other read-from-the-config
// claim: that the WRITE handle really only ever holds one connection open. The
// comments in migrate.go/dal.go assert it; SetMaxOpenConns(1) is the line that
// is supposed to cause it; nothing until now watched it happen.
//
// 🔴 WHAT THIS TEST NO LONGER DOES, and why (T-52917b review, 建議 4). It used to
// run the contention probe below while tracking the highest `db.Stats().InUse`
// it ever saw, and assert `peak <= 1`. That assertion was NEAR-TAUTOLOGICAL:
// `SetMaxOpenConns(1)` is the very thing that makes database/sql refuse to open
// a second connection, so `InUse` is arithmetically incapable of exceeding 1
// while the cap is in place — and if somebody REMOVED the cap, ① below has
// already failed the test before the probe runs. It could not fail
// independently of ①, so it was reporting confidence it had not earned. It is
// gone; do not add it back.
//
// ① IS THE ASSERTION WITH TEETH ON THE CAP. ② is a different guard: sixteen
// concurrent read-then-write transactions must ALL commit (errors are now
// asserted, not swallowed as they were before) and must advance the counter by
// EXACTLY sixteen.
//
// 🔴 MEASURED MUTANT MATRIX, so the next reader does not have to guess which
// assertion covers what. Each row was run with ① temporarily disabled, so that
// ② was answering on its own:
//
//	mutation                                       ①      ②
//	SetMaxOpenConns(1) → (4), _txlock kept         RED    green
//	_txlock=immediate dropped AND pool → (4)       RED    RED
//
// Read that honestly. ① is the ONLY thing that catches a widened cap: with
// BEGIN IMMEDIATE still in the DSN, SQLite's own file lock serialises the four
// connections and busy_timeout absorbs the wait, so ② sails through. ② earns
// its place on the second row — it is the assertion that notices when the
// transactions stop being IMMEDIATE, failing with `database is locked (517)`
// (SQLITE_BUSY_SNAPSHOT) as a DEFERRED reader tries to upgrade. Neither
// assertion subsumes the other, and NEITHER is the old peak probe.
func TestWritePoolIsCappedAtOneConnection(t *testing.T) {
	db := openTaskSeqTestDB(t)

	// ① the toothed one.
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("write pool MaxOpenConnections = %d, want 1 — the mint's critical "+
			"section rests on database/sql serialising our own writers onto a "+
			"single connection (openSQLite's SetMaxOpenConns(1))", got)
	}

	var before int
	if err := db.QueryRow(`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&before); err != nil {
		t.Fatalf("read counter: %v", err)
	}

	// ② real contention: every transaction must succeed, and none may be lost.
	const workers = 16
	errs := make(chan error, workers*3)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := db.Begin()
			if err != nil {
				errs <- fmt.Errorf("Begin: %w", err)
				return
			}
			// 🔴 A READ *THEN* A WRITE, deliberately — the same shape as the mint
			// (mintTaskNumber reads `next`, then claims it). A bare
			// `SET next = next + 1` would be atomic INSIDE SQLite and could not
			// lose an update no matter how the pool is configured, which would
			// make ② tautological in its own way.
			var cur int
			if err := tx.QueryRow(
				`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&cur); err != nil {
				errs <- fmt.Errorf("Query: %w", err)
				_ = tx.Rollback()
				return
			}
			if _, err := tx.Exec(
				`UPDATE task_id_seq SET next = ? WHERE id = 1`, cur+1); err != nil {
				errs <- fmt.Errorf("Exec: %w", err)
				_ = tx.Rollback()
				return
			}
			if err := tx.Commit(); err != nil {
				errs <- fmt.Errorf("Commit: %w", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a write-pool transaction failed under contention: %v — with "+
			"MaxOpenConns(1) and BEGIN IMMEDIATE these must queue, not collide", err)
	}

	var after int
	if err := db.QueryRow(`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&after); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if after-before != workers {
		t.Fatalf("counter advanced by %d across %d concurrent transactions, want "+
			"%d — an increment was LOST, so the write pool is not serialising the "+
			"read-modify-write the mint depends on", after-before, workers, workers)
	}
}

// TestMintedNumberIsReturnedWhenTheCreateRollsBack pins 唯一 over 連續 from the
// other side: a refused create must NOT burn its number, because minting and
// inserting share one transaction.
func TestMintedNumberIsReturnedWhenTheCreateRollsBack(t *testing.T) {
	api := newTasksTestServer(t)
	_, err := api.dal.CreateTaskMintingID(Task{Title: "refused"},
		func(string) error { return errOutsourceGateDenied })
	if err == nil {
		t.Fatalf("a refusing precheck must fail the create")
	}
	got := createAdHocTask(t, api, "m-exec")
	if got.ID != "T-1" {
		t.Fatalf("first surviving task is %q, want T-1 — a rolled-back create must "+
			"return its number, not burn it", got.ID)
	}
	var rows int
	if err := api.dal.rdb.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("task rows = %d, want 1 — a refused create must leave no orphan", rows)
	}
}

func openTaskSeqTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "seq-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return db
}

// TestUpgradingADatabaseThatAlreadyHasTasks is the real upgrade path, run on a
// database populated BEFORE migration 00060 exists: legacy "t-"+hex rows and a
// hand-planted "T-5" are already there when the counter table is created.
//
// It exists because the seeding SELECT is the one piece of this change that can
// only be wrong on somebody ELSE's database — a fresh test db has no rows for it
// to read, so every other test here would stay green with the seed hard-coded
// to 1 and the first mint on a live upgrade would collide with an existing row.
func TestUpgradingADatabaseThatAlreadyHasTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := openSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// ① migrate to the version JUST BEFORE the counter lands.
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 59); err != nil {
		t.Fatalf("goose up-to 59: %v", err)
	}
	if _, err := db.Exec(`SELECT 1 FROM task_id_seq`); err == nil {
		t.Fatal("task_id_seq already exists at version 59 — this test is not " +
			"exercising the upgrade it claims to")
	}

	// ② populate it the way a live database is populated.
	//
	// 🔴 THE EXECUTOR KIND HERE IS THE PRE-RENAME SPELLING, WRITTEN AS A
	// LITERAL, AND IT HAS TO BE. This seeds at schema version 59, where the
	// CHECK still reads IN ('member','outsource') — 00088 renames it to 'staff'
	// only in step ③ below. Using TaskExecutorStaff here fails the CHECK at
	// INSERT time, which is a test bug, not a product one: a database being
	// upgraded from version 59 contains the OLD value by definition.
	const preRenameExecutorKind = "member" // kind-vocab-guard:legacy
	pre := NewDAL(db)
	now := nowSecs()
	for _, id := range []string{"t-72dd79b666d0", "t-ced055e27e9f", "T-5"} {
		if err := pre.PutTask(Task{ID: id, Title: "pre-upgrade " + id,
			Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
			ExecutorKind: preRenameExecutorKind, ExecutorID: "m-exec",
			ReassignedFrom: "m-old", ReassignedFromKind: preRenameExecutorKind,
			CreatedTS: now, UpdatedTS: now}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// The SAME vocabulary is also stored inside a JSON blob with no CHECK behind
	// it, and that is the highest-risk copy in the package: a manual whose
	// assignee still spells the retired kind stops binding an executor at all,
	// and create_task then refuses with a message that names executor_member_id
	// rather than the assignee. Five manuals on the live station carried the old
	// value when this was written, two of them on the shipping path.
	if _, err := db.Exec(
		`INSERT INTO task_manual (type_key, assignee) VALUES (?, ?), (?, ?), (?, ?)`,
		"tm-pre-staff", `{"kind":"`+preRenameExecutorKind+`","member_id":"m-exec","unknown_key":7}`,
		"tm-pre-outsource", `{"kind":"outsource","model":"opus"}`,
		"tm-pre-unset", `{}`,
	); err != nil {
		t.Fatalf("seed manuals: %v", err)
	}

	// ③ upgrade.
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// The rows seeded at version 59 carried the pre-rename kind in BOTH kind
	// columns; 00088 must have renamed them in place. Asserted HERE because this
	// is the only test that starts from a POPULATED OLD schema — a fresh
	// database has no pre-rename row for the migration to act on, so a rename
	// that silently skipped existing rows would pass everywhere else.
	//
	// 🔴 reassigned_from_kind IS THE HALF THAT NEEDS AN ASSERTION, and that was
	// measured rather than assumed. executor_kind carries the new CHECK, so a
	// migration that forgot to rename it cannot even complete — goose fails on
	// the INSERT and every test that runs migrations goes red. Asserting only
	// that column would therefore have been an assertion with no teeth, hidden
	// behind a louder failure. reassigned_from_kind has NO CHECK and admits '',
	// so dropping its CASE leaves a migration that succeeds, a database that
	// looks upgraded, and a column still speaking the retired word. Nothing
	// else in the tree notices.
	for _, col := range []string{"executor_kind", "reassigned_from_kind"} {
		var stale int
		if err := db.QueryRow(
			`SELECT count(*) FROM task WHERE `+col+` = ?`, preRenameExecutorKind,
		).Scan(&stale); err != nil {
			t.Fatalf("count pre-rename %s: %v", col, err)
		}
		if stale != 0 {
			t.Errorf("00088 left %d task row(s) with %s on the pre-rename value",
				stale, col)
		}
		var renamed int
		if err := db.QueryRow(
			`SELECT count(*) FROM task WHERE `+col+` = ?`, TaskExecutorStaff,
		).Scan(&renamed); err != nil {
			t.Fatalf("count renamed %s: %v", col, err)
		}
		if renamed != 3 {
			t.Errorf("00088 renamed %s on %d of the 3 seeded rows", col, renamed)
		}
	}

	// The assignee blob: the kind renamed, EVERY OTHER KEY byte-identical. The
	// second half is the point — json_set on `$.kind` is used instead of writing
	// a fresh object precisely so member_id and any key the validator stores
	// without reading (it accepts unknown sub-keys verbatim) survive. A blob
	// rewrite would pass a "kind is staff" assertion while silently dropping
	// them.
	for _, tc := range []struct{ typeKey, want string }{
		{"tm-pre-staff", `{"kind":"staff","member_id":"m-exec","unknown_key":7}`},
		{"tm-pre-outsource", `{"kind":"outsource","model":"opus"}`},
		{"tm-pre-unset", `{}`},
	} {
		var got string
		if err := db.QueryRow(
			`SELECT assignee FROM task_manual WHERE type_key = ?`, tc.typeKey,
		).Scan(&got); err != nil {
			t.Fatalf("read assignee %s: %v", tc.typeKey, err)
		}
		if got != tc.want {
			t.Errorf("assignee %s after 00088:\n got  %s\n want %s", tc.typeKey, got, tc.want)
		}
	}

	// the counter must clear the highest EXISTING T-<n>, not restart at 1
	var next int
	if err := db.QueryRow(`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&next); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if next != 6 {
		t.Fatalf("counter seeded at %d, want 6 — an upgrade must start ABOVE the "+
			"highest T-<n> already in the table, or the first mint overwrites it", next)
	}

	// every pre-upgrade row is untouched, by its exact original id
	for _, id := range []string{"t-72dd79b666d0", "t-ced055e27e9f", "T-5"} {
		got, err := pre.GetTask(id)
		if err != nil || got == nil {
			t.Fatalf("pre-upgrade task %q lost by the migration: %v %v", id, got, err)
		}
		if got.Title != "pre-upgrade "+id {
			t.Fatalf("pre-upgrade task %q rewritten: title = %q", id, got.Title)
		}
	}

	// and the next mint lands clear of them
	minted, err := pre.CreateTaskMintingID(Task{Title: "post-upgrade",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorStaff, ExecutorID: "m-exec",
		CreatedTS: now, UpdatedTS: now}, nil)
	if err != nil {
		t.Fatalf("mint after upgrade: %v", err)
	}
	if minted.ID != "T-6" {
		t.Fatalf("first mint after upgrade is %q, want T-6", minted.ID)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 4 {
		t.Fatalf("task rows = %d, want 4 — the new mint must not have overwritten "+
			"a pre-upgrade row", rows)
	}
}
