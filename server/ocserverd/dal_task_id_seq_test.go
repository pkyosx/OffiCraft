package main

// dal_task_id_seq_test.go — where a new task's id comes from: what the counter
// hands out, what lands under it, and what a refused or failed create leaves
// behind.

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestCreateTaskMintingID(t *testing.T) {
	t.Run("a fresh database mints T-1 and the row lands under that id", func(t *testing.T) {
		d := newAPITestDAL(t)
		got, err := d.CreateTaskMintingID(dalTestTask(""), nil)
		if err != nil {
			t.Fatalf("CreateTaskMintingID: %v", err)
		}
		want := dalTestTask("T-1")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CreateTaskMintingID:\n got %+v\nwant %+v", got, want)
		}
		dalWantTask(t, d, want)
		if n := dalSeqNext(t, d); n != 2 {
			t.Fatalf("task_id_seq.next after one mint: want 2, got %d", n)
		}
	})

	t.Run("consecutive creates take consecutive numbers and both rows survive", func(t *testing.T) {
		d := newAPITestDAL(t)
		for _, want := range []string{"T-1", "T-2", "T-3"} {
			got, err := d.CreateTaskMintingID(dalTestTask(""), nil)
			if err != nil {
				t.Fatalf("CreateTaskMintingID: %v", err)
			}
			if got.ID != want {
				t.Fatalf("CreateTaskMintingID: want id %q, got %q", want, got.ID)
			}
		}
		if got := dalTaskIDs(t, d); !reflect.DeepEqual(got, []string{"T-1", "T-2", "T-3"}) {
			t.Fatalf("stored task ids: want [T-1 T-2 T-3], got %v", got)
		}
	})

	t.Run("the precheck is handed the minted id after it exists and before the row lands", func(t *testing.T) {
		d := newAPITestDAL(t)
		var seen []string
		var rowsDuringPrecheck []string
		_, err := d.CreateTaskMintingID(dalTestTask(""), func(id string) error {
			seen = append(seen, id)
			rowsDuringPrecheck = dalTaskIDs(t, d)
			return nil
		})
		if err != nil {
			t.Fatalf("CreateTaskMintingID: %v", err)
		}
		if !reflect.DeepEqual(seen, []string{"T-1"}) {
			t.Fatalf("precheck calls: want [T-1], got %v", seen)
		}
		if !reflect.DeepEqual(rowsDuringPrecheck, []string{}) {
			t.Fatalf("task rows visible while the precheck runs: want none, got %v", rowsDuringPrecheck)
		}
		dalWantTask(t, d, dalTestTask("T-1"))
	})

	t.Run("a refusing precheck returns its own error unchanged, writes no row and returns the number", func(t *testing.T) {
		d := newAPITestDAL(t)
		refusal := errors.New("outsource spawn gate denied")
		got, err := d.CreateTaskMintingID(dalTestTask(""), func(string) error { return refusal })
		if !errors.Is(err, refusal) {
			t.Fatalf("CreateTaskMintingID with a refusing precheck: want the caller's own error, got %v", err)
		}
		if got.ID != "T-1" {
			t.Fatalf("the refused task still carries the id it was refused under: want T-1, got %q", got.ID)
		}
		if rows := dalTaskIDs(t, d); !reflect.DeepEqual(rows, []string{}) {
			t.Fatalf("task rows after a refusal: want none, got %v", rows)
		}
		if n := dalSeqNext(t, d); n != 1 {
			t.Fatalf("task_id_seq.next after a refusal: want 1 (the number is returned), got %d", n)
		}
		next, err := d.CreateTaskMintingID(dalTestTask(""), nil)
		if err != nil {
			t.Fatalf("CreateTaskMintingID after a refusal: %v", err)
		}
		if next.ID != "T-1" {
			t.Fatalf("the returned number is handed out again: want T-1, got %q", next.ID)
		}
	})

	t.Run("a colliding id fails loudly instead of overwriting the task already stored there", func(t *testing.T) {
		d := newAPITestDAL(t)
		squatter := dalTestTask("T-1")
		squatter.Title = "the row already filed under T-1"
		dalPutTask(t, d, squatter)

		_, err := d.CreateTaskMintingID(dalTestTask(""), nil)
		if err == nil {
			t.Fatalf("CreateTaskMintingID onto an occupied id: want an error, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: task.id (1555)" {
			t.Fatalf("CreateTaskMintingID onto an occupied id: got %q", err.Error())
		}
		dalWantTask(t, d, squatter)
		if rows := dalTaskIDs(t, d); !reflect.DeepEqual(rows, []string{"T-1"}) {
			t.Fatalf("task rows after the collision: want [T-1] only, got %v", rows)
		}
		if n := dalSeqNext(t, d); n != 1 {
			t.Fatalf("task_id_seq.next after a failed insert: want 1 (rolled back), got %d", n)
		}
	})

	t.Run("a create over an already-advanced counter mints above it", func(t *testing.T) {
		d := newAPITestDAL(t)
		if _, err := d.wdb.Exec(`UPDATE task_id_seq SET next = 41 WHERE id = 1`); err != nil {
			t.Fatalf("advance the counter: %v", err)
		}
		got, err := d.CreateTaskMintingID(dalTestTask(""), nil)
		if err != nil {
			t.Fatalf("CreateTaskMintingID: %v", err)
		}
		if got.ID != "T-41" {
			t.Fatalf("CreateTaskMintingID: want T-41, got %q", got.ID)
		}
		if n := dalSeqNext(t, d); n != 42 {
			t.Fatalf("task_id_seq.next: want 42, got %d", n)
		}
	})

	t.Run("a rolled-back create leaves the one-connection write pool usable", func(t *testing.T) {
		d := newAPITestDAL(t)
		refusal := errors.New("refused")
		if _, err := d.CreateTaskMintingID(dalTestTask(""), func(string) error { return refusal }); !errors.Is(err, refusal) {
			t.Fatalf("CreateTaskMintingID: %v", err)
		}
		got, err := d.CreateTaskMintingID(dalTestTask(""), nil)
		if err != nil {
			t.Fatalf("the write pool is wedged after a rollback: %v", err)
		}
		dalWantTask(t, d, dalTestTask(got.ID))
	})
}

func TestMintTaskNumber(t *testing.T) {
	t.Run("successive claims on one open transaction hand out successive numbers", func(t *testing.T) {
		d := newAPITestDAL(t)
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		var got []int
		for i := 0; i < 3; i++ {
			n, err := mintTaskNumber(tx)
			if err != nil {
				t.Fatalf("mintTaskNumber: %v", err)
			}
			got = append(got, n)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Fatalf("three claims on one transaction: want [1 2 3], got %v", got)
		}
		if n := dalSeqNext(t, d); n != 4 {
			t.Fatalf("task_id_seq.next after three claims: want 4, got %d", n)
		}
	})

	t.Run("a rolled-back claim gives the number back", func(t *testing.T) {
		d := newAPITestDAL(t)
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		n, err := mintTaskNumber(tx)
		if err != nil {
			t.Fatalf("mintTaskNumber: %v", err)
		}
		if n != 1 {
			t.Fatalf("mintTaskNumber on a fresh database: want 1, got %d", n)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if got := dalSeqNext(t, d); got != 1 {
			t.Fatalf("task_id_seq.next after a rollback: want 1, got %d", got)
		}

		tx, err = d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin again — the write pool is wedged: %v", err)
		}
		again, err := mintTaskNumber(tx)
		if err != nil {
			t.Fatalf("mintTaskNumber after a rollback: %v", err)
		}
		if again != 1 {
			t.Fatalf("the returned number is claimed again: want 1, got %d", again)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	})

	t.Run("a database with no counter row says so instead of inventing a number", func(t *testing.T) {
		d := newAPITestDAL(t)
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer tx.Rollback() //nolint:errcheck // the test only reads afterwards
		if _, err := tx.Exec(`DELETE FROM task_id_seq`); err != nil {
			t.Fatalf("delete the counter row: %v", err)
		}
		_, err = mintTaskNumber(tx)
		if err == nil {
			t.Fatalf("mintTaskNumber with no counter row: want an error, got nil")
		}
		if err.Error() != "task_id_seq row is missing — database not migrated" {
			t.Fatalf("mintTaskNumber with no counter row: got %q", err.Error())
		}
		if errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("the sql.ErrNoRows is deliberately not passed through, got %v", err)
		}
	})
}

// dalSeqNext reads the number the next task will be called.

func dalSeqNext(t *testing.T, d *DAL) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("read task_id_seq: %v", err)
	}
	return n
}
