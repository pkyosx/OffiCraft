package main

// T-202 — migration 00106_retire_the_hire_born_outsource_shell.
//
// 🔴 WHAT THIS TEST IS FOR. The migration is one UPDATE with a four-clause
// WHERE, and every way it can be wrong is SILENT:
//
//   · drop `kind = 'outsource'` and it would collect a STAFF row that happened
//     to carry the id — the roster would simply be one member shorter and
//     nothing would error;
//   · drop the `linked_task_id` clause and it would collect a worker that is
//     mid-task — the task's executor vanishes from the roster while the task
//     keeps pointing at it, and the failure surfaces hours later as a task
//     nobody is working on;
//   · drop `roster_status = 'active'` and a re-run would re-stamp released_ts
//     on rows already collected, moving a timestamp that is supposed to record
//     when a worker actually stopped.
//
// None of those produce an error, a failed migration, or a red test anywhere
// else in this package. So the three inputs the DoD names are seeded side by
// side in ONE database and asserted after ONE run: the target moves, and the
// two neighbours do not.
//
// The neighbours are not decoration. A migration that moved all three rows and
// a migration that moved only the right one are indistinguishable when the
// fixture holds the target alone.

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// The row this ticket exists to collect. The id is the real one from the live
// station, spelled out rather than parameterised: the migration hard-codes it,
// so a test that computed it from the same place would assert nothing.
const t202TargetID = "m-51110698e801"

// t202UpToJustBefore106 migrates as far as 105 — the world 00106 meets. It
// deliberately does not assert which version that lands on: UpTo(105) means
// "everything numbered 105 or below", which moves on its own as neighbouring
// numbers land.
func t202OpenJustBefore106(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 105); err != nil {
		t.Fatalf("goose up to 105: %v", err)
	}
	return db
}

func t202SeedMember(t *testing.T, db *sql.DB, id, kind, linkedTask string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO member (id, name, kind, roster_status, linked_task_id, released_ts)
		 VALUES (?, ?, ?, 'active', ?, 0.0)`,
		id, id, kind, linkedTask); err != nil {
		t.Fatalf("seed %s (%s): %v", id, kind, err)
	}
}

func t202ReadMember(t *testing.T, db *sql.DB, id string) (status string, releasedTS float64) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT roster_status, released_ts FROM member WHERE id = ?`, id,
	).Scan(&status, &releasedTS); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return status, releasedTS
}

func TestMigration00106CollectsOnlyTheUnboundOutsourceShell(t *testing.T) {
	db := t202OpenJustBefore106(t, "t202-up.db")

	// ① the target: outsource, active, bound to nothing.
	t202SeedMember(t, db, t202TargetID, "outsource", "")
	// ② an outsource worker that IS doing a job — must not move.
	t202SeedMember(t, db, "ow-busy0000001", "outsource", "T-999")
	// ③ a staff member — must not move.
	t202SeedMember(t, db, "m-staff0000001", "staff", "")
	// ④ THE ONLY GUARD ON `id = '…'` ITSELF, and it is a different question
	// from ② and ③. Those two disqualify themselves on the OTHER clauses, so
	// with the id clause deleted the WHERE still passes over them and this test
	// still goes green — measured: with `id = 'm-51110698e801'` removed from the
	// migration, every test in this package PASSED.
	//
	// What the id clause is actually holding back is the owner's explicit NO:
	// 「直接處理掉這個worker ... 不用另外開可以移除的功能」(rc-3989498e0c8f) — collect
	// THIS row, do not build a general remover. Without the literal id this
	// migration IS that general remover: it would sweep up every idle outsource
	// worker on the station in one shot. So the neighbour that measures it has to
	// be a row that satisfies EVERY OTHER clause and differs only in its id —
	// alive, kind='outsource', bound to no task. It must come out untouched.
	t202SeedMember(t, db, "ow-idle0000001", "outsource", "")

	for _, id := range []string{
		t202TargetID, "ow-busy0000001", "m-staff0000001", "ow-idle0000001",
	} {
		if status, _ := t202ReadMember(t, db, id); status != "active" {
			t.Fatalf("precondition: %s seeded as %q, want active", id, status)
		}
	}

	if err := goose.UpTo(db, "migrations", 106); err != nil {
		t.Fatalf("goose up to 106: %v", err)
	}
	// goose's own version is the only honest "did it run": in WAL mode the
	// main database file can be byte-identical across a migration that ran.
	if v, err := goose.GetDBVersion(db); err != nil || v != 106 {
		t.Fatalf("after the upgrade goose reports version %d (err %v), want 106", v, err)
	}

	status, released := t202ReadMember(t, db, t202TargetID)
	if status != "removed" {
		t.Fatalf("the target is %q, want removed", status)
	}
	if released <= 0 {
		t.Fatalf("the target's released_ts is %v, want a real stamp — a removed "+
			"row with released_ts 0 reads as \"removed but never released\", a "+
			"state the release path never produces", released)
	}

	if status, released := t202ReadMember(t, db, "ow-busy0000001"); status != "active" || released != 0 {
		t.Fatalf("the task-bound worker moved: status %q released_ts %v — "+
			"collecting it would abandon its task mid-flight", status, released)
	}
	if status, released := t202ReadMember(t, db, "m-staff0000001"); status != "active" || released != 0 {
		t.Fatalf("the staff member moved: status %q released_ts %v", status, released)
	}
	if status, released := t202ReadMember(t, db, "ow-idle0000001"); status != "active" || released != 0 {
		t.Fatalf("an UNRELATED idle outsource worker was collected: status %q "+
			"released_ts %v. This row differs from the target ONLY in its id, so "+
			"this is the `id = '%s'` clause failing — and without that clause the "+
			"migration is the general \"remove an outsource worker\" feature the "+
			"owner refused (rc-3989498e0c8f), applied to the whole roster at once",
			status, released, t202TargetID)
	}
}

// 🔴 THE THREE PRECONDITIONS CAN ONLY BE TESTED ON THE KEYED ROW ITSELF.
// The test above seeds a task-bound worker and a staff member as NEIGHBOURS,
// and that does prove no collateral damage — but it has ZERO power over the
// kind and linked_task_id clauses, because the WHERE is keyed by a literal id
// first and those neighbours carry different ids. Measured, not reasoned: with
// the `linked_task_id` clause deleted from the migration, the test above still
// PASSED. A neighbour can never be collected no matter what the other clauses
// say.
//
// So each disqualifying attribute is put on the TARGET ID itself, one per case.
// This is the only fixture shape in which dropping a clause goes red.
func TestMigration00106LeavesTheTargetIDAloneWhenAnyPreconditionFails(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		status     string
		linkedTask string
		why        string
	}{
		{
			name: "bound to a task", kind: "outsource", status: "active", linkedTask: "T-999",
			why: "a worker that IS bound to a task is doing its job; collecting " +
				"it abandons the task mid-flight, and the task keeps pointing at a " +
				"member the roster no longer lists",
		},
		{
			name: "staff, not outsource", kind: "staff", status: "active", linkedTask: "",
			why: "a staff row carrying this id would be somebody else's member; " +
				"the roster would simply be one member shorter and nothing would error",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := t202OpenJustBefore106(t, "t202-precondition.db")
			t202SeedMember(t, db, t202TargetID, c.kind, c.linkedTask)

			if err := goose.UpTo(db, "migrations", 106); err != nil {
				t.Fatalf("goose up to 106: %v", err)
			}
			status, released := t202ReadMember(t, db, t202TargetID)
			if status != "active" || released != 0 {
				t.Fatalf("the row moved (status %q, released_ts %v) although it is %s — %s",
					status, released, c.name, c.why)
			}
		})
	}
}

// Re-running the migration must not re-stamp released_ts on a row that is
// already collected: that timestamp records when a worker actually stopped, and
// moving it later would rewrite a fact. This is what the roster_status='active'
// clause is for, and like the two above it can only be tested on the keyed id.
func TestMigration00106DoesNotRestampAnAlreadyCollectedRow(t *testing.T) {
	db := t202OpenJustBefore106(t, "t202-restamp.db")
	t202SeedMember(t, db, t202TargetID, "outsource", "")
	if _, err := db.Exec(
		`UPDATE member SET roster_status = 'removed', released_ts = 1000.0 WHERE id = ?`,
		t202TargetID); err != nil {
		t.Fatalf("seed an already-collected row: %v", err)
	}

	if err := goose.UpTo(db, "migrations", 106); err != nil {
		t.Fatalf("goose up to 106: %v", err)
	}
	status, released := t202ReadMember(t, db, t202TargetID)
	if status != "removed" || released != 1000.0 {
		t.Fatalf("the already-collected row was touched: status %q released_ts %v, "+
			"want removed / 1000 unchanged", status, released)
	}
}

// A station that never had the row — every fresh install and every CI database
// — must run this migration and be unchanged. The migration relies on an UPDATE
// whose WHERE matches nothing being a successful no-op rather than an error, so
// that property is asserted rather than assumed.
func TestMigration00106IsANoOpWhereTheRowNeverExisted(t *testing.T) {
	db := t202OpenJustBefore106(t, "t202-absent.db")

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member`).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}

	if err := goose.UpTo(db, "migrations", 106); err != nil {
		t.Fatalf("goose up to 106 on a database without the row: %v", err)
	}

	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member`).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != before {
		t.Fatalf("the member count moved from %d to %d on a database that never "+
			"had the shell row", before, after)
	}
	if n := 0; true {
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM member WHERE roster_status = 'removed'`).Scan(&n); err != nil {
			t.Fatalf("count removed: %v", err)
		}
		if n != 0 {
			t.Fatalf("%d rows were collected on a database that never had the shell row", n)
		}
	}
}
