package main

// dal_tasks_test.go — the task system's data access layer over a fresh migrated
// database per test: what a write stores, what a read answers, what a failed
// write leaves behind, and how the outsource projection folds onto the member
// row it is stored as.

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestScanTask(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalPutTask(t, d, dalTestTask("T-1"))
	bare := dalPutTask(t, d, Task{ID: "T-2", Status: TaskStatusNotStarted, Priority: TaskPriorityMid, ExecutorKind: "staff"})

	query := `SELECT ` + taskColumns + ` FROM task WHERE id = ?`

	got, err := scanTask(d.rdb.QueryRow(query, "T-1"))
	if err != nil {
		t.Fatalf("scanTask(T-1): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanTask(T-1):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanTask(d.rdb.QueryRow(query, "T-2"))
	if err != nil {
		t.Fatalf("scanTask(T-2): %v", err)
	}
	bare.Inputs = map[string]any{}
	bare.OutsourceRuntime = NormalizeRuntime("")
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("scanTask(T-2):\n got %+v\nwant %+v", got, bare)
	}
	if got.OutsourceDispatched {
		t.Fatalf("scanTask(T-2): a 0 dispatch column must read back as false")
	}

	if _, err := scanTask(d.rdb.QueryRow(query, "T-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanTask(T-ghost): want sql.ErrNoRows, got %v", err)
	}

	t.Run("an inputs column that is not JSON names the task it could not read", func(t *testing.T) {
		if _, err := d.wdb.Exec(`UPDATE task SET inputs = ? WHERE id = ?`, `{not json`, "T-2"); err != nil {
			t.Fatalf("write a bad inputs column: %v", err)
		}
		_, err := scanTask(d.rdb.QueryRow(query, "T-2"))
		if err == nil {
			t.Fatalf("scanTask over a bad inputs column: want an error, got nil")
		}
		want := "task T-2: bad inputs JSON: invalid character 'n' looking for beginning of object key string"
		if err.Error() != want {
			t.Fatalf("scanTask over a bad inputs column:\n got %q\nwant %q", err.Error(), want)
		}
	})
}

func TestListTasks(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("an empty board reads back as nil", func(t *testing.T) {
		got, err := d.ListTasks()
		if err != nil {
			t.Fatalf("ListTasks on an empty board: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTasks on an empty board: want nil, got %+v", got)
		}
	})

	newest := dalTestTask("T-3")
	newest.CreatedTS = 300
	middle := dalTestTask("T-2")
	middle.CreatedTS = 200
	middle.Status = TaskStatusDone
	oldest := dalTestTask("T-1")
	oldest.CreatedTS = 100
	for _, task := range []Task{newest, middle, oldest} {
		dalPutTask(t, d, task)
	}

	t.Run("every task comes back oldest first, terminal ones included", func(t *testing.T) {
		got, err := d.ListTasks()
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		want := []Task{oldest, middle, newest}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTasks:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestGetTask(t *testing.T) {
	d := newAPITestDAL(t)
	dalWantTask(t, d, dalPutTask(t, d, dalTestTask("T-1")))

	got, err := d.GetTask("T-ghost")
	if err != nil {
		t.Fatalf("GetTask(T-ghost): %v", err)
	}
	if got != nil {
		t.Fatalf("GetTask(T-ghost): want nil, got %+v", *got)
	}
}

func TestFindOpenTaskByDedupe(t *testing.T) {
	d := newAPITestDAL(t)

	open := dalTestTask("T-2")
	open.TypeKey = "sync-jira"
	open.DedupeKey = "ACE-1"
	open.CreatedTS = 200
	open.Status = TaskStatusInProgress
	dalPutTask(t, d, open)

	older := dalTestTask("T-1")
	older.TypeKey = "sync-jira"
	older.DedupeKey = "ACE-1"
	older.CreatedTS = 100
	older.Status = TaskStatusDone
	dalPutTask(t, d, older)

	otherType := dalTestTask("T-3")
	otherType.TypeKey = "ship-order"
	otherType.DedupeKey = "ACE-1"
	otherType.Status = TaskStatusInProgress
	dalPutTask(t, d, otherType)

	t.Run("the non-terminal task under that pair is the one that blocks a create", func(t *testing.T) {
		got, err := d.FindOpenTaskByDedupe("sync-jira", "ACE-1")
		if err != nil {
			t.Fatalf("FindOpenTaskByDedupe: %v", err)
		}
		if got == nil || !reflect.DeepEqual(*got, open) {
			t.Fatalf("FindOpenTaskByDedupe:\n got %+v\nwant %+v", got, open)
		}
	})

	t.Run("the type key is part of the pair, so another type's task does not answer", func(t *testing.T) {
		got, err := d.FindOpenTaskByDedupe("never-a-type", "ACE-1")
		if err != nil {
			t.Fatalf("FindOpenTaskByDedupe: %v", err)
		}
		if got != nil {
			t.Fatalf("FindOpenTaskByDedupe on an unknown type: want nil, got %+v", *got)
		}
	})

	for _, terminal := range []string{TaskStatusDone, TaskStatusTerminated, TaskStatusDuplicated} {
		t.Run("a "+terminal+" task never blocks a reopen", func(t *testing.T) {
			d := newAPITestDAL(t)
			task := dalTestTask("T-1")
			task.TypeKey = "sync-jira"
			task.DedupeKey = "ACE-1"
			task.Status = terminal
			dalPutTask(t, d, task)

			got, err := d.FindOpenTaskByDedupe("sync-jira", "ACE-1")
			if err != nil {
				t.Fatalf("FindOpenTaskByDedupe: %v", err)
			}
			if got != nil {
				t.Fatalf("FindOpenTaskByDedupe over a %s task: want nil, got %+v", terminal, *got)
			}

			task.ID = "T-2"
			task.Status = TaskStatusNotStarted
			dalPutTask(t, d, task)
			got, err = d.FindOpenTaskByDedupe("sync-jira", "ACE-1")
			if err != nil {
				t.Fatalf("FindOpenTaskByDedupe: %v", err)
			}
			if got == nil || got.ID != "T-2" {
				t.Fatalf("FindOpenTaskByDedupe once a live task exists: want T-2, got %+v", got)
			}
		})
	}

	t.Run("two open tasks under one pair answer with the older", func(t *testing.T) {
		d := newAPITestDAL(t)
		first := dalTestTask("T-9")
		first.TypeKey = "sync-jira"
		first.DedupeKey = "ACE-1"
		first.CreatedTS = 100
		dalPutTask(t, d, first)
		second := first
		second.ID = "T-1"
		second.CreatedTS = 200
		dalPutTask(t, d, second)

		got, err := d.FindOpenTaskByDedupe("sync-jira", "ACE-1")
		if err != nil {
			t.Fatalf("FindOpenTaskByDedupe: %v", err)
		}
		if got == nil || !reflect.DeepEqual(*got, first) {
			t.Fatalf("FindOpenTaskByDedupe:\n got %+v\nwant %+v", got, first)
		}
	})
}

func TestListOpenTasksByExecutor(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("an executor with nothing in flight reads back as nil", func(t *testing.T) {
		got, err := d.ListOpenTasksByExecutor("ann", 10)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		if got != nil {
			t.Fatalf("ListOpenTasksByExecutor with nothing in flight: want nil, got %+v", got)
		}
	})

	stale := dalTestTask("T-1")
	stale.ExecutorID = "ann"
	stale.UpdatedTS = 100
	fresh := dalTestTask("T-2")
	fresh.ExecutorID = "ann"
	fresh.UpdatedTS = 300
	middle := dalTestTask("T-3")
	middle.ExecutorID = "ann"
	middle.UpdatedTS = 200
	finished := dalTestTask("T-4")
	finished.ExecutorID = "ann"
	finished.UpdatedTS = 400
	finished.Status = TaskStatusDone
	elsewhere := dalTestTask("T-5")
	elsewhere.ExecutorID = "bob"
	elsewhere.UpdatedTS = 500
	for _, task := range []Task{stale, fresh, middle, finished, elsewhere} {
		dalPutTask(t, d, task)
	}

	t.Run("the in-flight tasks come back most recently updated first, and nobody else's", func(t *testing.T) {
		got, err := d.ListOpenTasksByExecutor("ann", 10)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		want := []Task{fresh, middle, stale}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListOpenTasksByExecutor:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the limit caps the snapshot at the newest end", func(t *testing.T) {
		got, err := d.ListOpenTasksByExecutor("ann", 2)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		want := []Task{fresh, middle}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListOpenTasksByExecutor capped at 2:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a limit of zero reads nothing while a negative one lifts the cap", func(t *testing.T) {
		got, err := d.ListOpenTasksByExecutor("ann", 0)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		if got != nil {
			t.Fatalf("ListOpenTasksByExecutor with limit 0: want nil, got %+v", got)
		}
		got, err = d.ListOpenTasksByExecutor("ann", -1)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		want := []Task{fresh, middle, stale}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListOpenTasksByExecutor with limit -1:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an equal updated_ts is broken by the newer created_ts", func(t *testing.T) {
		d := newAPITestDAL(t)
		earlier := dalTestTask("T-1")
		earlier.ExecutorID = "ann"
		earlier.UpdatedTS = 500
		earlier.CreatedTS = 100
		later := dalTestTask("T-2")
		later.ExecutorID = "ann"
		later.UpdatedTS = 500
		later.CreatedTS = 200
		dalPutTask(t, d, earlier)
		dalPutTask(t, d, later)

		got, err := d.ListOpenTasksByExecutor("ann", 10)
		if err != nil {
			t.Fatalf("ListOpenTasksByExecutor: %v", err)
		}
		want := []Task{later, earlier}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListOpenTasksByExecutor:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestCountOpenTasksByExecutor(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.CountOpenTasksByExecutor("ann")
	if err != nil {
		t.Fatalf("CountOpenTasksByExecutor with nothing in flight: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountOpenTasksByExecutor with nothing in flight: want 0, got %d", got)
	}

	for i, status := range []string{
		TaskStatusNotStarted, TaskStatusInProgress, TaskStatusWaitingOwner,
		TaskStatusWaitingExternal, TaskStatusDone, TaskStatusTerminated, TaskStatusDuplicated,
	} {
		task := dalTestTask("T-" + string(rune('1'+i)))
		task.ExecutorID = "ann"
		task.Status = status
		dalPutTask(t, d, task)
	}
	elsewhere := dalTestTask("T-9")
	elsewhere.ExecutorID = "bob"
	dalPutTask(t, d, elsewhere)

	t.Run("the four non-terminal statuses count and the three terminal ones do not", func(t *testing.T) {
		got, err := d.CountOpenTasksByExecutor("ann")
		if err != nil {
			t.Fatalf("CountOpenTasksByExecutor: %v", err)
		}
		if got != 4 {
			t.Fatalf("CountOpenTasksByExecutor(ann): want 4, got %d", got)
		}
		if got, err = d.CountOpenTasksByExecutor("bob"); err != nil || got != 1 {
			t.Fatalf("CountOpenTasksByExecutor(bob): want 1, got %d (%v)", got, err)
		}
		if got, err = d.CountOpenTasksByExecutor("nobody"); err != nil || got != 0 {
			t.Fatalf("CountOpenTasksByExecutor(nobody): want 0, got %d (%v)", got, err)
		}
	})
}

func TestCountOpenTasksOfType(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.CountOpenTasksOfType("sync-jira")
	if err != nil {
		t.Fatalf("CountOpenTasksOfType before any task: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountOpenTasksOfType before any task: want 0, got %d", got)
	}

	for i, status := range []string{TaskStatusInProgress, TaskStatusWaitingOwner, TaskStatusDone, TaskStatusDuplicated} {
		task := dalTestTask("T-" + string(rune('1'+i)))
		task.TypeKey = "sync-jira"
		task.Status = status
		dalPutTask(t, d, task)
	}
	other := dalTestTask("T-9")
	other.TypeKey = "ship-order"
	dalPutTask(t, d, other)

	t.Run("only this type's non-terminal tasks hold the manual open", func(t *testing.T) {
		got, err := d.CountOpenTasksOfType("sync-jira")
		if err != nil {
			t.Fatalf("CountOpenTasksOfType: %v", err)
		}
		if got != 2 {
			t.Fatalf("CountOpenTasksOfType(sync-jira): want 2, got %d", got)
		}
		if got, err = d.CountOpenTasksOfType("ship-order"); err != nil || got != 1 {
			t.Fatalf("CountOpenTasksOfType(ship-order): want 1, got %d (%v)", got, err)
		}
		if got, err = d.CountOpenTasksOfType("never-a-type"); err != nil || got != 0 {
			t.Fatalf("CountOpenTasksOfType(never-a-type): want 0, got %d (%v)", got, err)
		}
	})
}

func TestCountTasksDuplicatingOriginal(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.CountTasksDuplicatingOriginal("T-1")
	if err != nil {
		t.Fatalf("CountTasksDuplicatingOriginal before any duplicate: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountTasksDuplicatingOriginal before any duplicate: want 0, got %d", got)
	}

	original := dalTestTask("T-1")
	original.DuplicateOf = ""
	dalPutTask(t, d, original)
	for _, id := range []string{"T-2", "T-3"} {
		dup := dalTestTask(id)
		dup.Status = TaskStatusDuplicated
		dup.DuplicateOf = "T-1"
		dalPutTask(t, d, dup)
	}
	elsewhere := dalTestTask("T-4")
	elsewhere.Status = TaskStatusDuplicated
	elsewhere.DuplicateOf = "T-9"
	dalPutTask(t, d, elsewhere)

	t.Run("a task that is already an original is named by its duplicates and by nobody else's", func(t *testing.T) {
		got, err := d.CountTasksDuplicatingOriginal("T-1")
		if err != nil {
			t.Fatalf("CountTasksDuplicatingOriginal: %v", err)
		}
		if got != 2 {
			t.Fatalf("CountTasksDuplicatingOriginal(T-1): want 2, got %d", got)
		}
		if got, err = d.CountTasksDuplicatingOriginal("T-9"); err != nil || got != 1 {
			t.Fatalf("CountTasksDuplicatingOriginal(T-9): want 1, got %d (%v)", got, err)
		}
		if got, err = d.CountTasksDuplicatingOriginal("T-2"); err != nil || got != 0 {
			t.Fatalf("CountTasksDuplicatingOriginal(T-2): want 0, got %d (%v)", got, err)
		}
	})
}

func TestPutTaskOn(t *testing.T) {
	t.Run("the upsert mode lands a new row whole", func(t *testing.T) {
		d := newAPITestDAL(t)
		task := dalTestTask("T-1")
		if err := putTaskOn(d.wdb, task, taskWriteUpsert); err != nil {
			t.Fatalf("putTaskOn: %v", err)
		}
		dalWantTask(t, d, task)
	})

	t.Run("nil inputs are stored as an empty object rather than as SQL text nobody can decode", func(t *testing.T) {
		d := newAPITestDAL(t)
		task := dalTestTask("T-1")
		task.Inputs = nil
		if err := putTaskOn(d.wdb, task, taskWriteUpsert); err != nil {
			t.Fatalf("putTaskOn: %v", err)
		}
		task.Inputs = map[string]any{}
		dalWantTask(t, d, task)
	})

	t.Run("the upsert carries every column onto an existing row EXCEPT the title and the description", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))
		bystander := dalPutTask(t, d, dalTestTask("T-2"))

		stale := stored
		stale.Title = "a title read a moment earlier"
		stale.Description = "a description read a moment earlier"
		stale.Status = TaskStatusDone
		stale.Priority = TaskPriorityLow
		stale.ExecutorID = "carl"
		stale.UpdatedTS = 9999
		if err := putTaskOn(d.wdb, stale, taskWriteUpsert); err != nil {
			t.Fatalf("putTaskOn: %v", err)
		}

		want := stale
		want.Title = stored.Title
		want.Description = stored.Description
		dalWantTask(t, d, want)
		dalWantTask(t, d, bystander)
	})

	t.Run("the insert-only mode refuses an id that is already taken and leaves it as it stands", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))

		clash := dalTestTask("T-1")
		clash.Status = TaskStatusTerminated
		clash.ExecutorID = "carl"
		err := putTaskOn(d.wdb, clash, taskWriteInsertOnly)
		if err == nil {
			t.Fatalf("putTaskOn insert-only onto an occupied id: want an error, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: task.id (1555)" {
			t.Fatalf("putTaskOn insert-only onto an occupied id: got %q", err.Error())
		}
		dalWantTask(t, d, stored)
	})

	t.Run("the runtime is normalized on the way in", func(t *testing.T) {
		d := newAPITestDAL(t)
		task := dalTestTask("T-1")
		task.OutsourceRuntime = ""
		if err := putTaskOn(d.wdb, task, taskWriteUpsert); err != nil {
			t.Fatalf("putTaskOn: %v", err)
		}
		task.OutsourceRuntime = NormalizeRuntime("")
		dalWantTask(t, d, task)
	})

	t.Run("a write inside a rolled-back transaction reaches the table not at all", func(t *testing.T) {
		d := newAPITestDAL(t)
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := putTaskOn(tx, dalTestTask("T-1"), taskWriteInsertOnly); err != nil {
			t.Fatalf("putTaskOn on a transaction: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if got := dalTaskIDs(t, d); !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("tasks after the rollback: want none, got %v", got)
		}
		if err := putTaskOn(d.wdb, dalTestTask("T-1"), taskWriteInsertOnly); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
		dalWantTask(t, d, dalTestTask("T-1"))
	})
}

func TestListTaskDeps(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a task blocked by nothing reads back as nil", func(t *testing.T) {
		got, err := d.ListTaskDeps("T-1")
		if err != nil {
			t.Fatalf("ListTaskDeps: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskDeps on an unblocked task: want nil, got %v", got)
		}
	})

	t.Run("the blockers come back in a deterministic order, and no other task's", func(t *testing.T) {
		for _, blocker := range []string{"T-9", "T-2", "T-5"} {
			dalAddDep(t, d, "T-1", blocker)
		}
		dalAddDep(t, d, "T-7", "T-3")

		got, err := d.ListTaskDeps("T-1")
		if err != nil {
			t.Fatalf("ListTaskDeps: %v", err)
		}
		if want := []string{"T-2", "T-5", "T-9"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTaskDeps(T-1): want %v, got %v", want, got)
		}
		got, err = d.ListTaskDeps("T-7")
		if err != nil {
			t.Fatalf("ListTaskDeps(T-7): %v", err)
		}
		if want := []string{"T-3"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTaskDeps(T-7): want %v, got %v", want, got)
		}
	})
}

func TestAllTaskDeps(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no edges at all is an empty map, not nil", func(t *testing.T) {
		got, err := d.AllTaskDeps()
		if err != nil {
			t.Fatalf("AllTaskDeps: %v", err)
		}
		if !reflect.DeepEqual(got, map[string][]string{}) {
			t.Fatalf("AllTaskDeps over an empty table: want an empty map, got %v", got)
		}
	})

	t.Run("every task's blockers are folded under its own id, each list ordered", func(t *testing.T) {
		dalAddDep(t, d, "T-1", "T-9")
		dalAddDep(t, d, "T-1", "T-2")
		dalAddDep(t, d, "T-7", "T-3")

		got, err := d.AllTaskDeps()
		if err != nil {
			t.Fatalf("AllTaskDeps: %v", err)
		}
		want := map[string][]string{"T-1": {"T-2", "T-9"}, "T-7": {"T-3"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskDeps: want %v, got %v", want, got)
		}
	})
}

func TestListTasksBlockedBy(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a blocker nothing waits on reads back as nil", func(t *testing.T) {
		got, err := d.ListTasksBlockedBy("T-9")
		if err != nil {
			t.Fatalf("ListTasksBlockedBy: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTasksBlockedBy with no dependents: want nil, got %+v", got)
		}
	})

	second := dalTestTask("T-2")
	second.CreatedTS = 500
	first := dalTestTask("T-1")
	first.CreatedTS = 600
	unrelated := dalTestTask("T-3")
	for _, task := range []Task{second, first, unrelated} {
		dalPutTask(t, d, task)
	}
	dalAddDep(t, d, "T-1", "T-9")
	dalAddDep(t, d, "T-2", "T-9")
	dalAddDep(t, d, "T-3", "T-8")

	t.Run("the dependents come back whole, ordered by id, and only this blocker's", func(t *testing.T) {
		got, err := d.ListTasksBlockedBy("T-9")
		if err != nil {
			t.Fatalf("ListTasksBlockedBy: %v", err)
		}
		want := []Task{first, second}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTasksBlockedBy(T-9):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an edge naming a task that does not exist yields no row", func(t *testing.T) {
		dalAddDep(t, d, "T-never-created", "T-9")
		got, err := d.ListTasksBlockedBy("T-9")
		if err != nil {
			t.Fatalf("ListTasksBlockedBy: %v", err)
		}
		want := []Task{first, second}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTasksBlockedBy(T-9):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestAddTaskDep(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("one edge is added without disturbing the ones already there", func(t *testing.T) {
		dalAddDep(t, d, "T-1", "T-2")
		dalAddDep(t, d, "T-1", "T-3")
		dalAddDep(t, d, "T-7", "T-4")

		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-2", "T-3"}) {
			t.Fatalf("ListTaskDeps(T-1): got %v", got)
		}
		if got, _ := d.ListTaskDeps("T-7"); !reflect.DeepEqual(got, []string{"T-4"}) {
			t.Fatalf("ListTaskDeps(T-7): got %v", got)
		}
	})

	t.Run("adding the same edge again changes nothing and does not error", func(t *testing.T) {
		if err := d.AddTaskDep("T-1", "T-2"); err != nil {
			t.Fatalf("AddTaskDep twice: %v", err)
		}
		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-2", "T-3"}) {
			t.Fatalf("ListTaskDeps(T-1) after the repeat: got %v", got)
		}
		if got := dalDepCount(t, d); got != 3 {
			t.Fatalf("task_dep rows after the repeat: want 3, got %d", got)
		}
	})

	t.Run("a task may be recorded as blocking itself — nothing here refuses it", func(t *testing.T) {
		if err := d.AddTaskDep("T-1", "T-1"); err != nil {
			t.Fatalf("AddTaskDep(T-1, T-1): %v", err)
		}
		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-1", "T-2", "T-3"}) {
			t.Fatalf("ListTaskDeps(T-1): got %v", got)
		}
	})
}

func TestReplaceTaskDeps(t *testing.T) {
	t.Run("the whole list is replaced and no other task's edges move", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalAddDep(t, d, "T-1", "T-2")
		dalAddDep(t, d, "T-1", "T-3")
		dalAddDep(t, d, "T-7", "T-4")

		if err := d.ReplaceTaskDeps("T-1", []string{"T-5", "T-3"}); err != nil {
			t.Fatalf("ReplaceTaskDeps: %v", err)
		}
		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-3", "T-5"}) {
			t.Fatalf("ListTaskDeps(T-1): got %v", got)
		}
		if got, _ := d.ListTaskDeps("T-7"); !reflect.DeepEqual(got, []string{"T-4"}) {
			t.Fatalf("ListTaskDeps(T-7): got %v", got)
		}
	})

	t.Run("an empty list clears the task's edges and leaves the table otherwise whole", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalAddDep(t, d, "T-1", "T-2")
		dalAddDep(t, d, "T-7", "T-4")

		if err := d.ReplaceTaskDeps("T-1", nil); err != nil {
			t.Fatalf("ReplaceTaskDeps with no blockers: %v", err)
		}
		got, err := d.ListTaskDeps("T-1")
		if err != nil {
			t.Fatalf("ListTaskDeps: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskDeps(T-1) after clearing: want nil, got %v", got)
		}
		if got, _ := d.ListTaskDeps("T-7"); !reflect.DeepEqual(got, []string{"T-4"}) {
			t.Fatalf("ListTaskDeps(T-7): got %v", got)
		}
	})

	t.Run("a repeated blocker in the list lands once", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.ReplaceTaskDeps("T-1", []string{"T-2", "T-2", "T-3"}); err != nil {
			t.Fatalf("ReplaceTaskDeps: %v", err)
		}
		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-2", "T-3"}) {
			t.Fatalf("ListTaskDeps(T-1): got %v", got)
		}
		if got := dalDepCount(t, d); got != 2 {
			t.Fatalf("task_dep rows: want 2, got %d", got)
		}
	})

	t.Run("replacing the edges of a task that has none simply adds them", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.ReplaceTaskDeps("T-1", []string{"T-2"}); err != nil {
			t.Fatalf("ReplaceTaskDeps: %v", err)
		}
		if got, _ := d.ListTaskDeps("T-1"); !reflect.DeepEqual(got, []string{"T-2"}) {
			t.Fatalf("ListTaskDeps(T-1): got %v", got)
		}
	})
}

func TestScanTaskStep(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalPutStep(t, d, dalTestStep("st-1", "T-1"))
	bare := dalPutStep(t, d, TaskStep{ID: "st-2", TaskID: "T-1", Status: StepStatusPending})

	query := `SELECT ` + taskStepColumns + ` FROM task_step WHERE id = ?`

	got, err := scanTaskStep(d.rdb.QueryRow(query, "st-1"))
	if err != nil {
		t.Fatalf("scanTaskStep(st-1): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanTaskStep(st-1):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanTaskStep(d.rdb.QueryRow(query, "st-2"))
	if err != nil {
		t.Fatalf("scanTaskStep(st-2): %v", err)
	}
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("scanTaskStep(st-2):\n got %+v\nwant %+v", got, bare)
	}
	if got.IsGate {
		t.Fatalf("scanTaskStep(st-2): a 0 gate column must read back as false")
	}

	if _, err := scanTaskStep(d.rdb.QueryRow(query, "st-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanTaskStep(st-ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestListTaskSteps(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a task with no plan reads back as nil", func(t *testing.T) {
		got, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskSteps on a planless task: want nil, got %+v", got)
		}
	})

	third := dalTestStep("st-3", "T-1")
	third.OrderIdx = 2
	tieB := dalTestStep("st-b", "T-1")
	tieB.OrderIdx = 0
	tieA := dalTestStep("st-a", "T-1")
	tieA.OrderIdx = 0
	elsewhere := dalTestStep("st-9", "T-2")
	for _, st := range []TaskStep{third, tieB, tieA, elsewhere} {
		dalPutStep(t, d, st)
	}

	t.Run("the steps come back in timeline order, ties broken by id, and no other task's", func(t *testing.T) {
		got, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		want := []TaskStep{tieA, tieB, third}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTaskSteps(T-1):\n got %+v\nwant %+v", got, want)
		}
		got, err = d.ListTaskSteps("T-2")
		if err != nil {
			t.Fatalf("ListTaskSteps(T-2): %v", err)
		}
		if !reflect.DeepEqual(got, []TaskStep{elsewhere}) {
			t.Fatalf("ListTaskSteps(T-2):\n got %+v\nwant %+v", got, []TaskStep{elsewhere})
		}
	})
}

func TestAllTaskSteps(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no steps at all is an empty map, not nil", func(t *testing.T) {
		got, err := d.AllTaskSteps()
		if err != nil {
			t.Fatalf("AllTaskSteps: %v", err)
		}
		if !reflect.DeepEqual(got, map[string][]TaskStep{}) {
			t.Fatalf("AllTaskSteps over an empty table: want an empty map, got %v", got)
		}
	})

	t.Run("every task's steps are folded under its own id in timeline order", func(t *testing.T) {
		second := dalTestStep("st-2", "T-1")
		second.OrderIdx = 1
		first := dalTestStep("st-1", "T-1")
		first.OrderIdx = 0
		elsewhere := dalTestStep("st-9", "T-2")
		for _, st := range []TaskStep{second, first, elsewhere} {
			dalPutStep(t, d, st)
		}

		got, err := d.AllTaskSteps()
		if err != nil {
			t.Fatalf("AllTaskSteps: %v", err)
		}
		want := map[string][]TaskStep{"T-1": {first, second}, "T-2": {elsewhere}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskSteps:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestAllTaskStepProgress(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no steps at all is an empty map, not nil", func(t *testing.T) {
		got, err := d.AllTaskStepProgress()
		if err != nil {
			t.Fatalf("AllTaskStepProgress: %v", err)
		}
		if !reflect.DeepEqual(got, map[string]TaskStepProgress{}) {
			t.Fatalf("AllTaskStepProgress over an empty table: want an empty map, got %v", got)
		}
	})

	t.Run("done steps count toward both halves, superseded ones toward neither, and a planless task is absent", func(t *testing.T) {
		dalPutStepWithStatus(t, d, "st-1", "T-1", StepStatusDone)
		dalPutStepWithStatus(t, d, "st-2", "T-1", StepStatusDone)
		dalPutStepWithStatus(t, d, "st-3", "T-1", StepStatusInProgress)
		dalPutStepWithStatus(t, d, "st-4", "T-1", StepStatusSuperseded)
		dalPutStepWithStatus(t, d, "st-5", "T-2", StepStatusPending)
		dalPutStepWithStatus(t, d, "st-6", "T-3", StepStatusSuperseded)
		dalPutTask(t, d, dalTestTask("T-9"))

		got, err := d.AllTaskStepProgress()
		if err != nil {
			t.Fatalf("AllTaskStepProgress: %v", err)
		}
		want := map[string]TaskStepProgress{
			"T-1": {Done: 2, Total: 3},
			"T-2": {Done: 0, Total: 1},
			"T-3": {Done: 0, Total: 0},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskStepProgress:\n got %v\nwant %v", got, want)
		}
	})
}

func TestAllTaskCurrentStep(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no steps at all is an empty map, not nil", func(t *testing.T) {
		got, err := d.AllTaskCurrentStep()
		if err != nil {
			t.Fatalf("AllTaskCurrentStep: %v", err)
		}
		if !reflect.DeepEqual(got, map[string]TaskCurrentStep{}) {
			t.Fatalf("AllTaskCurrentStep over an empty table: want an empty map, got %v", got)
		}
	})

	t.Run("the first non-terminal step in timeline order is the pointer, and an all-terminal plan has none", func(t *testing.T) {
		done := dalTestStep("st-1", "T-1")
		done.OrderIdx = 0
		done.Status = StepStatusDone
		dalPutStep(t, d, done)
		superseded := dalTestStep("st-2", "T-1")
		superseded.OrderIdx = 1
		superseded.Status = StepStatusSuperseded
		dalPutStep(t, d, superseded)
		current := dalTestStep("st-3", "T-1")
		current.OrderIdx = 2
		current.Status = StepStatusInProgress
		current.Name = "reconcile the yard"
		dalPutStep(t, d, current)
		later := dalTestStep("st-4", "T-1")
		later.OrderIdx = 3
		later.Status = StepStatusPending
		dalPutStep(t, d, later)

		finished := dalTestStep("st-9", "T-2")
		finished.Status = StepStatusDone
		dalPutStep(t, d, finished)

		open := dalTestStep("st-8", "T-3")
		open.Status = StepStatusWaitingOwner
		open.Name = "wait for the owner"
		dalPutStep(t, d, open)

		got, err := d.AllTaskCurrentStep()
		if err != nil {
			t.Fatalf("AllTaskCurrentStep: %v", err)
		}
		want := map[string]TaskCurrentStep{
			"T-1": {ID: "st-3", Name: "reconcile the yard"},
			"T-3": {ID: "st-8", Name: "wait for the owner"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskCurrentStep:\n got %v\nwant %v", got, want)
		}
	})

	t.Run("an equal order_idx is broken by the step id", func(t *testing.T) {
		d := newAPITestDAL(t)
		for _, id := range []string{"st-b", "st-a"} {
			st := dalTestStep(id, "T-1")
			st.OrderIdx = 0
			st.Status = StepStatusPending
			st.Name = "step " + id
			dalPutStep(t, d, st)
		}
		got, err := d.AllTaskCurrentStep()
		if err != nil {
			t.Fatalf("AllTaskCurrentStep: %v", err)
		}
		want := map[string]TaskCurrentStep{"T-1": {ID: "st-a", Name: "step st-a"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskCurrentStep:\n got %v\nwant %v", got, want)
		}
	})
}

func TestGetTaskStep(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutStep(t, d, dalTestStep("st-1", "T-1"))
	got, err := d.GetTaskStep("st-1")
	if err != nil {
		t.Fatalf("GetTaskStep(st-1): %v", err)
	}
	if got == nil || !reflect.DeepEqual(*got, stored) {
		t.Fatalf("GetTaskStep(st-1):\n got %+v\nwant %+v", got, stored)
	}

	missing, err := d.GetTaskStep("st-ghost")
	if err != nil {
		t.Fatalf("GetTaskStep(st-ghost): %v", err)
	}
	if missing != nil {
		t.Fatalf("GetTaskStep(st-ghost): want nil, got %+v", *missing)
	}
}

func TestSetTaskStepNote(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutStep(t, d, dalTestStep("st-1", "T-1"))
	bystander := dalPutStep(t, d, dalTestStep("st-2", "T-1"))

	t.Run("the note column moves and nothing else on the row does", func(t *testing.T) {
		ok, err := d.SetTaskStepNote("st-1", "got as far as the yard reconciliation")
		if err != nil {
			t.Fatalf("SetTaskStepNote: %v", err)
		}
		if !ok {
			t.Fatalf("SetTaskStepNote: want true, got false")
		}
		want := stored
		want.Note = "got as far as the yard reconciliation"
		dalWantStep(t, d, want)
		dalWantStep(t, d, bystander)
	})

	t.Run("a blank note clears the column rather than being refused", func(t *testing.T) {
		ok, err := d.SetTaskStepNote("st-1", "")
		if err != nil {
			t.Fatalf("SetTaskStepNote: %v", err)
		}
		if !ok {
			t.Fatalf("SetTaskStepNote: want true, got false")
		}
		want := stored
		want.Note = ""
		dalWantStep(t, d, want)
	})

	t.Run("a step that is gone reports false and is not resurrected", func(t *testing.T) {
		ok, err := d.SetTaskStepNote("st-ghost", "a note for nobody")
		if err != nil {
			t.Fatalf("SetTaskStepNote(st-ghost): %v", err)
		}
		if ok {
			t.Fatalf("SetTaskStepNote(st-ghost): want false, got true")
		}
		if got := dalStepIDs(t, d); !reflect.DeepEqual(got, []string{"st-1", "st-2"}) {
			t.Fatalf("steps after writing a note to an unknown id: got %v", got)
		}
	})
}

func TestSetTaskDescriptionOn(t *testing.T) {
	t.Run("the description and the updated stamp move, and nothing else on the row does", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))
		bystander := dalPutTask(t, d, dalTestTask("T-2"))

		ok, err := SetTaskDescriptionOn(d.wdb, "T-1", "the corrected description", 1800000000)
		if err != nil {
			t.Fatalf("SetTaskDescriptionOn: %v", err)
		}
		if !ok {
			t.Fatalf("SetTaskDescriptionOn: want true, got false")
		}
		want := stored
		want.Description = "the corrected description"
		want.UpdatedTS = 1800000000
		dalWantTask(t, d, want)
		dalWantTask(t, d, bystander)
	})

	t.Run("a task that is gone reports false and is not resurrected", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutTask(t, d, dalTestTask("T-1"))
		ok, err := SetTaskDescriptionOn(d.wdb, "T-ghost", "a description for nobody", 1800000000)
		if err != nil {
			t.Fatalf("SetTaskDescriptionOn(T-ghost): %v", err)
		}
		if ok {
			t.Fatalf("SetTaskDescriptionOn(T-ghost): want false, got true")
		}
		if got := dalTaskIDs(t, d); !reflect.DeepEqual(got, []string{"T-1"}) {
			t.Fatalf("tasks after writing a description to an unknown id: got %v", got)
		}
	})

	t.Run("a write inside a rolled-back transaction reaches the row not at all", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if _, err := SetTaskDescriptionOn(tx, "T-1", "never committed", 1800000000); err != nil {
			t.Fatalf("SetTaskDescriptionOn on a transaction: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantTask(t, d, stored)
		if _, err := SetTaskDescriptionOn(d.wdb, "T-1", "committed", 1800000000); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
	})
}

func TestTaskDescriptionOn(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutTask(t, d, dalTestTask("T-1"))

	t.Run("a stored task answers its description and says the row was there", func(t *testing.T) {
		got, found, err := taskDescriptionOn(d.rdb, "T-1")
		if err != nil {
			t.Fatalf("taskDescriptionOn: %v", err)
		}
		if !found || got != stored.Description {
			t.Fatalf("taskDescriptionOn(T-1): want (%q, true), got (%q, %v)", stored.Description, got, found)
		}
	})

	t.Run("an id naming no task answers the empty string and says so", func(t *testing.T) {
		got, found, err := taskDescriptionOn(d.rdb, "T-ghost")
		if err != nil {
			t.Fatalf("taskDescriptionOn(T-ghost): %v", err)
		}
		if found || got != "" {
			t.Fatalf("taskDescriptionOn(T-ghost): want (\"\", false), got (%q, %v)", got, found)
		}
	})

	t.Run("inside a transaction it reads that transaction's own uncommitted write", func(t *testing.T) {
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if _, err := SetTaskDescriptionOn(tx, "T-1", "rewritten in flight", 1800000000); err != nil {
			t.Fatalf("SetTaskDescriptionOn: %v", err)
		}
		got, found, err := taskDescriptionOn(tx, "T-1")
		if err != nil {
			t.Fatalf("taskDescriptionOn(tx): %v", err)
		}
		if !found || got != "rewritten in flight" {
			t.Fatalf("taskDescriptionOn(tx): want (%q, true), got (%q, %v)", "rewritten in flight", got, found)
		}
		onPool, _, err := taskDescriptionOn(d.rdb, "T-1")
		if err != nil {
			t.Fatalf("taskDescriptionOn(pool): %v", err)
		}
		if onPool != stored.Description {
			t.Fatalf("taskDescriptionOn(pool) mid-transaction: want %q, got %q", stored.Description, onPool)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantTask(t, d, stored)
	})
}

func TestSetTaskTitleOn(t *testing.T) {
	t.Run("the title and the updated stamp move, and nothing else on the row does", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))
		bystander := dalPutTask(t, d, dalTestTask("T-2"))

		ok, err := SetTaskTitleOn(d.wdb, "T-1", "the corrected title", 1800000000)
		if err != nil {
			t.Fatalf("SetTaskTitleOn: %v", err)
		}
		if !ok {
			t.Fatalf("SetTaskTitleOn: want true, got false")
		}
		want := stored
		want.Title = "the corrected title"
		want.UpdatedTS = 1800000000
		dalWantTask(t, d, want)
		dalWantTask(t, d, bystander)
	})

	t.Run("it writes what it is given, untrimmed — the trim lives at the door", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutTask(t, d, dalTestTask("T-1"))
		if _, err := SetTaskTitleOn(d.wdb, "T-1", "  padded  ", 1800000000); err != nil {
			t.Fatalf("SetTaskTitleOn: %v", err)
		}
		want := stored
		want.Title = "  padded  "
		want.UpdatedTS = 1800000000
		dalWantTask(t, d, want)
	})

	t.Run("a task that is gone reports false and is not resurrected", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutTask(t, d, dalTestTask("T-1"))
		ok, err := SetTaskTitleOn(d.wdb, "T-ghost", "a title for nobody", 1800000000)
		if err != nil {
			t.Fatalf("SetTaskTitleOn(T-ghost): %v", err)
		}
		if ok {
			t.Fatalf("SetTaskTitleOn(T-ghost): want false, got true")
		}
		if got := dalTaskIDs(t, d); !reflect.DeepEqual(got, []string{"T-1"}) {
			t.Fatalf("tasks after writing a title to an unknown id: got %v", got)
		}
	})
}

func TestTaskTitleOn(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutTask(t, d, dalTestTask("T-1"))

	t.Run("a stored task answers its title and says the row was there", func(t *testing.T) {
		got, found, err := taskTitleOn(d.rdb, "T-1")
		if err != nil {
			t.Fatalf("taskTitleOn: %v", err)
		}
		if !found || got != stored.Title {
			t.Fatalf("taskTitleOn(T-1): want (%q, true), got (%q, %v)", stored.Title, got, found)
		}
	})

	t.Run("an id naming no task answers the empty string and says so", func(t *testing.T) {
		got, found, err := taskTitleOn(d.rdb, "T-ghost")
		if err != nil {
			t.Fatalf("taskTitleOn(T-ghost): %v", err)
		}
		if found || got != "" {
			t.Fatalf("taskTitleOn(T-ghost): want (\"\", false), got (%q, %v)", got, found)
		}
	})

	t.Run("inside a transaction it reads that transaction's own uncommitted write", func(t *testing.T) {
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if _, err := SetTaskTitleOn(tx, "T-1", "rewritten in flight", 1800000000); err != nil {
			t.Fatalf("SetTaskTitleOn: %v", err)
		}
		got, found, err := taskTitleOn(tx, "T-1")
		if err != nil {
			t.Fatalf("taskTitleOn(tx): %v", err)
		}
		if !found || got != "rewritten in flight" {
			t.Fatalf("taskTitleOn(tx): want (%q, true), got (%q, %v)", "rewritten in flight", got, found)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantTask(t, d, stored)
	})
}

func TestTouchTaskUpdatedTS(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutTask(t, d, dalTestTask("T-1"))
	bystander := dalPutTask(t, d, dalTestTask("T-2"))

	t.Run("the stamp moves and nothing else on the row does", func(t *testing.T) {
		if err := d.TouchTaskUpdatedTS("T-1", 1800000000); err != nil {
			t.Fatalf("TouchTaskUpdatedTS: %v", err)
		}
		want := stored
		want.UpdatedTS = 1800000000
		dalWantTask(t, d, want)
		dalWantTask(t, d, bystander)
	})

	t.Run("it takes a stamp older than the one stored — nothing here holds it forward", func(t *testing.T) {
		if err := d.TouchTaskUpdatedTS("T-1", 1); err != nil {
			t.Fatalf("TouchTaskUpdatedTS: %v", err)
		}
		want := stored
		want.UpdatedTS = 1
		dalWantTask(t, d, want)
	})

	t.Run("an id naming no task creates nothing and does not error", func(t *testing.T) {
		if err := d.TouchTaskUpdatedTS("T-ghost", 1800000000); err != nil {
			t.Fatalf("TouchTaskUpdatedTS(T-ghost): %v", err)
		}
		if got := dalTaskIDs(t, d); !reflect.DeepEqual(got, []string{"T-1", "T-2"}) {
			t.Fatalf("tasks after touching an unknown id: got %v", got)
		}
	})
}

func TestPutTaskStep(t *testing.T) {
	t.Run("a new step lands whole", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalWantStep(t, d, dalPutStep(t, d, dalTestStep("st-1", "T-1")))
	})

	t.Run("an upsert carries every column onto an existing row EXCEPT the note", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutStep(t, d, dalTestStep("st-1", "T-1"))
		bystander := dalPutStep(t, d, dalTestStep("st-2", "T-1"))

		stale := stored
		stale.Note = "a note read a moment earlier"
		stale.Status = StepStatusDone
		stale.Name = "renamed"
		stale.OrderIdx = 7
		stale.IsGate = false
		stale.ReplyCardID = ""
		stale.FinishedTS = 1800000000
		if err := d.PutTaskStep(stale); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		want := stale
		want.Note = stored.Note
		dalWantStep(t, d, want)
		dalWantStep(t, d, bystander)
	})

	t.Run("an id that is gone is inserted back, note and all", func(t *testing.T) {
		d := newAPITestDAL(t)
		revived := dalTestStep("st-1", "T-1")
		revived.Note = "the note the insert half carries"
		if err := d.PutTaskStep(revived); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		dalWantStep(t, d, revived)
	})

	t.Run("a status outside the closed set lands as it stands — this layer refuses nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		odd := dalTestStep("st-1", "T-1")
		odd.Status = "halfway"
		if err := d.PutTaskStep(odd); err != nil {
			t.Fatalf("PutTaskStep with an unknown status: %v", err)
		}
		dalWantStep(t, d, odd)
	})
}

func TestReplaceTaskPlan(t *testing.T) {
	t.Run("the fresh plan replaces the live steps while the terminal ones keep their place ahead of it", func(t *testing.T) {
		d := newAPITestDAL(t)
		done := dalStepAt(t, d, "st-done", "T-1", 0, StepStatusDone)
		history := dalStepAt(t, d, "st-old", "T-1", 1, StepStatusSuperseded)
		dalStepAt(t, d, "st-live", "T-1", 2, StepStatusInProgress)
		elsewhere := dalStepAt(t, d, "st-9", "T-2", 0, StepStatusPending)

		fresh := []TaskStep{
			{ID: "st-new-1", Name: "draft the plan", DoD: "a plan exists"},
			{ID: "st-new-2", Name: "run it", DoD: "it ran", Status: StepStatusInProgress},
		}
		got, err := d.ReplaceTaskPlan("T-1", nil, nil, 0, fresh)
		if err != nil {
			t.Fatalf("ReplaceTaskPlan: %v", err)
		}
		want := []TaskStep{
			done, history,
			{ID: "st-new-1", TaskID: "T-1", OrderIdx: 2, Name: "draft the plan", DoD: "a plan exists", Status: StepStatusPending},
			{ID: "st-new-2", TaskID: "T-1", OrderIdx: 3, Name: "run it", DoD: "it ran", Status: StepStatusInProgress},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReplaceTaskPlan:\n got %+v\nwant %+v", got, want)
		}
		stored, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if !reflect.DeepEqual(stored, want) {
			t.Fatalf("the stored plan:\n got %+v\nwant %+v", stored, want)
		}
		dalWantStep(t, d, elsewhere)
	})

	t.Run("a retained step stays alive exactly as it stands, only re-indexed", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalStepAt(t, d, "st-done", "T-1", 0, StepStatusDone)
		kept := dalStepAt(t, d, "st-keep", "T-1", 1, StepStatusWaitingOwner)
		dalStepAt(t, d, "st-drop", "T-1", 2, StepStatusPending)

		got, err := d.ReplaceTaskPlan("T-1", []string{"st-keep"}, nil, 1800000000, nil)
		if err != nil {
			t.Fatalf("ReplaceTaskPlan: %v", err)
		}
		keptAfter := kept
		keptAfter.OrderIdx = 1
		want := []TaskStep{dalStepFixture("st-done", "T-1", 0, StepStatusDone), keptAfter}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReplaceTaskPlan:\n got %+v\nwant %+v", got, want)
		}
		if ids := dalStepIDs(t, d); !reflect.DeepEqual(ids, []string{"st-done", "st-keep"}) {
			t.Fatalf("the stored steps: want [st-done st-keep], got %v", ids)
		}
	})

	t.Run("a frozen step becomes superseded stamped at the freeze moment, keeping its card and start", func(t *testing.T) {
		d := newAPITestDAL(t)
		frozen := dalStepAt(t, d, "st-frozen", "T-1", 0, StepStatusWaitingOwner)
		got, err := d.ReplaceTaskPlan("T-1", nil, []string{"st-frozen"}, 1800000000, nil)
		if err != nil {
			t.Fatalf("ReplaceTaskPlan: %v", err)
		}
		want := frozen
		want.Status = StepStatusSuperseded
		want.FinishedTS = 1800000000
		if !reflect.DeepEqual(got, []TaskStep{want}) {
			t.Fatalf("ReplaceTaskPlan:\n got %+v\nwant %+v", got, []TaskStep{want})
		}
		dalWantStep(t, d, want)
	})

	t.Run("a plan with nothing to keep and nothing fresh empties the task's steps", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalStepAt(t, d, "st-1", "T-1", 0, StepStatusInProgress)
		dalStepAt(t, d, "st-2", "T-1", 1, StepStatusPending)
		elsewhere := dalStepAt(t, d, "st-9", "T-2", 0, StepStatusPending)

		got, err := d.ReplaceTaskPlan("T-1", nil, nil, 0, nil)
		if err != nil {
			t.Fatalf("ReplaceTaskPlan: %v", err)
		}
		if got != nil {
			t.Fatalf("ReplaceTaskPlan over an emptied plan: want nil, got %+v", got)
		}
		if ids := dalStepIDs(t, d); !reflect.DeepEqual(ids, []string{"st-9"}) {
			t.Fatalf("the stored steps: want only the other task's, got %v", ids)
		}
		dalWantStep(t, d, elsewhere)
	})

	t.Run("planning a task that had no plan simply lands the fresh steps", func(t *testing.T) {
		d := newAPITestDAL(t)
		got, err := d.ReplaceTaskPlan("T-1", nil, nil, 0, []TaskStep{{ID: "st-1", Name: "the only step"}})
		if err != nil {
			t.Fatalf("ReplaceTaskPlan: %v", err)
		}
		want := []TaskStep{{ID: "st-1", TaskID: "T-1", OrderIdx: 0, Name: "the only step", Status: StepStatusPending}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ReplaceTaskPlan:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a fresh step whose id is already taken half-applies nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		live := dalStepAt(t, d, "st-live", "T-1", 0, StepStatusInProgress)
		clash := dalStepAt(t, d, "st-taken", "T-2", 0, StepStatusPending)

		_, err := d.ReplaceTaskPlan("T-1", nil, nil, 0, []TaskStep{
			{ID: "st-new", Name: "the one that would have landed"},
			{ID: "st-taken", Name: "the one that collides"},
		})
		if err == nil {
			t.Fatalf("ReplaceTaskPlan with a colliding step id: want an error, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: task_step.id (1555)" {
			t.Fatalf("ReplaceTaskPlan with a colliding step id: got %q", err.Error())
		}
		dalWantStep(t, d, live)
		dalWantStep(t, d, clash)
		if ids := dalStepIDs(t, d); !reflect.DeepEqual(ids, []string{"st-live", "st-taken"}) {
			t.Fatalf("the stored steps after the rollback: got %v", ids)
		}
		if _, err := d.ReplaceTaskPlan("T-1", nil, nil, 0, nil); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
	})
}

func TestWorkerStatusFromMember(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rosterStatus string
		activatedTS  float64
		want         string
	}{
		{"a removed row is released whatever its claim says", RosterStatusRemoved, 1700000000, WorkerStatusReleased},
		{"a removed row with no claim is released too", RosterStatusRemoved, 0, WorkerStatusReleased},
		{"an active row that claimed its task is active", RosterStatusActive, 1700000000, WorkerStatusActive},
		{"an active row that never claimed is merely assigned", RosterStatusActive, 0, WorkerStatusAssigned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := workerStatusFromMember(tc.rosterStatus, tc.activatedTS); got != tc.want {
				t.Fatalf("workerStatusFromMember(%q, %v): want %q, got %q",
					tc.rosterStatus, tc.activatedTS, tc.want, got)
			}
		})
	}
}

func TestWorkerFromMember(t *testing.T) {
	t.Run("the member row projects onto the worker vocabulary whole", func(t *testing.T) {
		m := dalTestOutsourceMember("ow-1")
		got := workerFromMember(m)
		want := dalTestWorker("ow-1")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("workerFromMember:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an unbound member projects the empty task id rather than dereferencing nothing", func(t *testing.T) {
		m := dalTestOutsourceMember("ow-1")
		m.LinkedTaskID = nil
		got := workerFromMember(m)
		want := dalTestWorker("ow-1")
		want.TaskID = ""
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("workerFromMember:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the status is derived from the roster and the claim, never carried", func(t *testing.T) {
		m := dalTestOutsourceMember("ow-1")
		m.ActivatedTS = 0
		if got := workerFromMember(m).Status; got != WorkerStatusAssigned {
			t.Fatalf("an unclaimed worker: want %q, got %q", WorkerStatusAssigned, got)
		}
		m.RosterStatus = RosterStatusRemoved
		if got := workerFromMember(m).Status; got != WorkerStatusReleased {
			t.Fatalf("a removed worker: want %q, got %q", WorkerStatusReleased, got)
		}
	})
}

func TestMemberFromWorker(t *testing.T) {
	t.Run("the worker maps back onto the member row it is stored as", func(t *testing.T) {
		got := memberFromWorker(dalTestWorker("ow-1"))
		want := dalTestOutsourceMember("ow-1")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("memberFromWorker:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the two halves round-trip, so a worker write cannot zero a column it does not know", func(t *testing.T) {
		w := dalTestWorker("ow-1")
		if got := workerFromMember(memberFromWorker(w)); !reflect.DeepEqual(got, w) {
			t.Fatalf("the round trip:\n got %+v\nwant %+v", got, w)
		}
	})

	t.Run("a released worker maps onto a removed roster row", func(t *testing.T) {
		w := dalTestWorker("ow-1")
		w.Status = WorkerStatusReleased
		got := memberFromWorker(w)
		if got.RosterStatus != RosterStatusRemoved {
			t.Fatalf("a released worker: want roster %q, got %q", RosterStatusRemoved, got.RosterStatus)
		}
		if got.ActivatedTS != w.ActivatedTS {
			t.Fatalf("a released worker keeps its claim anchor: want %v, got %v", w.ActivatedTS, got.ActivatedTS)
		}
	})

	t.Run("an assigned worker's claim anchor is cleared, an active one's is stamped when it has none", func(t *testing.T) {
		w := dalTestWorker("ow-1")
		w.Status = WorkerStatusAssigned
		if got := memberFromWorker(w).ActivatedTS; got != 0 {
			t.Fatalf("an assigned worker: want the anchor cleared, got %v", got)
		}

		w.Status = WorkerStatusActive
		w.ActivatedTS = 0
		before := nowSecs()
		got := memberFromWorker(w).ActivatedTS
		if got < before {
			t.Fatalf("an active worker with no anchor: want a stamp no older than %v, got %v", before, got)
		}

		w.ActivatedTS = 1700000011
		if got := memberFromWorker(w).ActivatedTS; got != 1700000011 {
			t.Fatalf("an active worker that already claimed: want its own anchor, got %v", got)
		}
	})
}

func TestListOutsourceWorkers(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a roster with no outsource rows reads back as nil", func(t *testing.T) {
		dalPutMember(t, d, dalTestMember("ann", "Ann"))
		got, err := d.ListOutsourceWorkers()
		if err != nil {
			t.Fatalf("ListOutsourceWorkers: %v", err)
		}
		if got != nil {
			t.Fatalf("ListOutsourceWorkers with no outsource rows: want nil, got %+v", got)
		}
	})

	t.Run("every outsource row projects, released ones included, oldest first", func(t *testing.T) {
		second := dalTestOutsourceMember("ow-2")
		second.CreatedTS = 200
		second.Codename = "O-2"
		dalPutMember(t, d, second)
		first := dalTestOutsourceMember("ow-1")
		first.CreatedTS = 100
		dalPutMember(t, d, first)
		released := dalTestOutsourceMember("ow-3")
		released.CreatedTS = 300
		released.Codename = "O-3"
		released.RosterStatus = RosterStatusRemoved
		dalPutMember(t, d, released)

		got, err := d.ListOutsourceWorkers()
		if err != nil {
			t.Fatalf("ListOutsourceWorkers: %v", err)
		}
		want := []OutsourceWorker{
			workerFromMember(first), workerFromMember(second), workerFromMember(released),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListOutsourceWorkers:\n got %+v\nwant %+v", got, want)
		}
		if got[2].Status != WorkerStatusReleased {
			t.Fatalf("the removed row projects as %q, want %q", got[2].Status, WorkerStatusReleased)
		}
	})
}

func TestGetOutsourceWorker(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalTestOutsourceMember("ow-1")
	dalPutMember(t, d, stored)
	dalPutMember(t, d, dalTestMember("ann", "Ann"))

	t.Run("an outsource id projects onto the worker vocabulary", func(t *testing.T) {
		got, err := d.GetOutsourceWorker("ow-1")
		if err != nil {
			t.Fatalf("GetOutsourceWorker(ow-1): %v", err)
		}
		want := workerFromMember(stored)
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetOutsourceWorker(ow-1):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a staff id on the same table is nil, and so is an id nobody carries", func(t *testing.T) {
		got, err := d.GetOutsourceWorker("ann")
		if err != nil {
			t.Fatalf("GetOutsourceWorker(ann): %v", err)
		}
		if got != nil {
			t.Fatalf("GetOutsourceWorker(ann): want nil, got %+v", *got)
		}
		got, err = d.GetOutsourceWorker("ow-ghost")
		if err != nil {
			t.Fatalf("GetOutsourceWorker(ow-ghost): %v", err)
		}
		if got != nil {
			t.Fatalf("GetOutsourceWorker(ow-ghost): want nil, got %+v", *got)
		}
	})
}

func TestReleaseWorkersForTask(t *testing.T) {
	t.Run("every worker bound to the task is flipped and returned, and nobody else's row moves", func(t *testing.T) {
		d := newAPITestDAL(t)
		second := dalWorkerOnTask(t, d, "ow-2", "O-2", "T-1", 200)
		first := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		elsewhere := dalWorkerOnTask(t, d, "ow-9", "O-9", "T-2", 300)

		flipped, err := d.ReleaseWorkersForTask("T-1", 1800000000)
		if err != nil {
			t.Fatalf("ReleaseWorkersForTask: %v", err)
		}
		want := []OutsourceWorker{
			dalReleasedWorker(first, 1800000000), dalReleasedWorker(second, 1800000000),
		}
		if !reflect.DeepEqual(flipped, want) {
			t.Fatalf("ReleaseWorkersForTask:\n got %+v\nwant %+v", flipped, want)
		}
		dalWantWorker(t, d, dalReleasedWorker(first, 1800000000))
		dalWantWorker(t, d, dalReleasedWorker(second, 1800000000))
		dalWantWorker(t, d, elsewhere)
	})

	t.Run("running it again flips nothing, because the rows are already released", func(t *testing.T) {
		d := newAPITestDAL(t)
		worker := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		if _, err := d.ReleaseWorkersForTask("T-1", 1800000000); err != nil {
			t.Fatalf("ReleaseWorkersForTask: %v", err)
		}
		flipped, err := d.ReleaseWorkersForTask("T-1", 1900000000)
		if err != nil {
			t.Fatalf("ReleaseWorkersForTask again: %v", err)
		}
		if flipped != nil {
			t.Fatalf("ReleaseWorkersForTask again: want nil, got %+v", flipped)
		}
		dalWantWorker(t, d, dalReleasedWorker(worker, 1800000000))
	})

	t.Run("a task nobody works on flips nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		worker := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		flipped, err := d.ReleaseWorkersForTask("T-9", 1800000000)
		if err != nil {
			t.Fatalf("ReleaseWorkersForTask(T-9): %v", err)
		}
		if flipped != nil {
			t.Fatalf("ReleaseWorkersForTask(T-9): want nil, got %+v", flipped)
		}
		dalWantWorker(t, d, worker)
	})
}

func TestReleaseWorkerByID(t *testing.T) {
	t.Run("one worker is flipped by its own id while the other on the same task stays", func(t *testing.T) {
		d := newAPITestDAL(t)
		predecessor := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		successor := dalWorkerOnTask(t, d, "ow-2", "O-2", "T-1", 200)

		got, err := d.ReleaseWorkerByID("ow-1", 1800000000)
		if err != nil {
			t.Fatalf("ReleaseWorkerByID: %v", err)
		}
		want := dalReleasedWorker(predecessor, 1800000000)
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("ReleaseWorkerByID:\n got %+v\nwant %+v", got, want)
		}
		dalWantWorker(t, d, want)
		dalWantWorker(t, d, successor)
	})

	t.Run("an already-released worker is a nil no-op that does not restamp it", func(t *testing.T) {
		d := newAPITestDAL(t)
		worker := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		if _, err := d.ReleaseWorkerByID("ow-1", 1800000000); err != nil {
			t.Fatalf("ReleaseWorkerByID: %v", err)
		}
		got, err := d.ReleaseWorkerByID("ow-1", 1900000000)
		if err != nil {
			t.Fatalf("ReleaseWorkerByID again: %v", err)
		}
		if got != nil {
			t.Fatalf("ReleaseWorkerByID again: want nil, got %+v", *got)
		}
		dalWantWorker(t, d, dalReleasedWorker(worker, 1800000000))
	})

	t.Run("an id no worker carries is a nil no-op", func(t *testing.T) {
		d := newAPITestDAL(t)
		worker := dalWorkerOnTask(t, d, "ow-1", "O-1", "T-1", 100)
		dalPutMember(t, d, dalTestMember("ann", "Ann"))

		got, err := d.ReleaseWorkerByID("ow-ghost", 1800000000)
		if err != nil {
			t.Fatalf("ReleaseWorkerByID(ow-ghost): %v", err)
		}
		if got != nil {
			t.Fatalf("ReleaseWorkerByID(ow-ghost): want nil, got %+v", *got)
		}
		got, err = d.ReleaseWorkerByID("ann", 1800000000)
		if err != nil {
			t.Fatalf("ReleaseWorkerByID(ann): %v", err)
		}
		if got != nil {
			t.Fatalf("ReleaseWorkerByID over a staff id: want nil, got %+v", *got)
		}
		dalWantWorker(t, d, worker)
		dalWantMember(t, d, dalTestMember("ann", "Ann"))
	})
}

func TestScanTaskManual(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalPutManual(t, d, dalTestManual("sync-jira"))
	bare := dalPutManual(t, d, TaskManual{TypeKey: "ship-order"})

	query := `SELECT ` + taskManualColumns + ` FROM task_manual WHERE type_key = ?`

	got, err := scanTaskManual(d.rdb.QueryRow(query, "sync-jira"))
	if err != nil {
		t.Fatalf("scanTaskManual(sync-jira): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanTaskManual(sync-jira):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanTaskManual(d.rdb.QueryRow(query, "ship-order"))
	if err != nil {
		t.Fatalf("scanTaskManual(ship-order): %v", err)
	}
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("scanTaskManual(ship-order):\n got %+v\nwant %+v", got, bare)
	}

	if _, err := scanTaskManual(d.rdb.QueryRow(query, "never-a-type")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanTaskManual(never-a-type): want sql.ErrNoRows, got %v", err)
	}
}

func TestListTaskManuals(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no manuals at all reads back as nil", func(t *testing.T) {
		got, err := d.ListTaskManuals()
		if err != nil {
			t.Fatalf("ListTaskManuals: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskManuals with no manuals: want nil, got %+v", got)
		}
	})

	t.Run("the order is the display name, case-insensitively, with the type key standing in when it is unset", func(t *testing.T) {
		zebra := dalTestManual("aaa-type")
		zebra.DisplayName = "Zebra"
		banana := dalTestManual("zzz-type")
		banana.DisplayName = "banana"
		nameless := dalTestManual("mmm-type")
		nameless.DisplayName = ""
		apple := dalTestManual("bbb-type")
		apple.DisplayName = "Apple"
		for _, m := range []TaskManual{zebra, banana, nameless, apple} {
			dalPutManual(t, d, m)
		}

		got, err := d.ListTaskManuals()
		if err != nil {
			t.Fatalf("ListTaskManuals: %v", err)
		}
		want := []TaskManual{apple, banana, nameless, zebra}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTaskManuals:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("two manuals sharing a display name fall back to the type key", func(t *testing.T) {
		d := newAPITestDAL(t)
		second := dalTestManual("zzz-type")
		second.DisplayName = "Shared"
		first := dalTestManual("aaa-type")
		first.DisplayName = "Shared"
		dalPutManual(t, d, second)
		dalPutManual(t, d, first)

		got, err := d.ListTaskManuals()
		if err != nil {
			t.Fatalf("ListTaskManuals: %v", err)
		}
		if !reflect.DeepEqual(got, []TaskManual{first, second}) {
			t.Fatalf("ListTaskManuals:\n got %+v\nwant %+v", got, []TaskManual{first, second})
		}
	})
}

func TestGetTaskManualOn(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutManual(t, d, dalTestManual("sync-jira"))

	t.Run("a stored manual reads back whole and an unknown type key is nil", func(t *testing.T) {
		got, err := getTaskManualOn(d.rdb, "sync-jira")
		if err != nil {
			t.Fatalf("getTaskManualOn: %v", err)
		}
		if got == nil || !reflect.DeepEqual(*got, stored) {
			t.Fatalf("getTaskManualOn(sync-jira):\n got %+v\nwant %+v", got, stored)
		}
		missing, err := getTaskManualOn(d.rdb, "never-a-type")
		if err != nil {
			t.Fatalf("getTaskManualOn(never-a-type): %v", err)
		}
		if missing != nil {
			t.Fatalf("getTaskManualOn(never-a-type): want nil, got %+v", *missing)
		}
	})

	t.Run("inside a transaction it reads that transaction's own uncommitted write", func(t *testing.T) {
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		edited := stored
		edited.SopMD = "rewritten in flight"
		if err := putTaskManualOn(tx, edited); err != nil {
			t.Fatalf("putTaskManualOn: %v", err)
		}
		got, err := getTaskManualOn(tx, "sync-jira")
		if err != nil {
			t.Fatalf("getTaskManualOn(tx): %v", err)
		}
		if got == nil || !reflect.DeepEqual(*got, edited) {
			t.Fatalf("getTaskManualOn(tx):\n got %+v\nwant %+v", got, edited)
		}
		onPool, err := getTaskManualOn(d.rdb, "sync-jira")
		if err != nil {
			t.Fatalf("getTaskManualOn(pool): %v", err)
		}
		if onPool == nil || !reflect.DeepEqual(*onPool, stored) {
			t.Fatalf("getTaskManualOn(pool) mid-transaction:\n got %+v\nwant %+v", onPool, stored)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantManual(t, d, stored)
	})
}

func TestPutTaskManualOn(t *testing.T) {
	t.Run("a new manual lands whole", func(t *testing.T) {
		d := newAPITestDAL(t)
		m := dalTestManual("sync-jira")
		if err := putTaskManualOn(d.wdb, m); err != nil {
			t.Fatalf("putTaskManualOn: %v", err)
		}
		dalWantManual(t, d, m)
	})

	t.Run("writing the same type key again carries every column onto the stored row", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutManual(t, d, dalTestManual("sync-jira"))
		bystander := dalPutManual(t, d, dalTestManual("ship-order"))

		edited := TaskManual{
			TypeKey: "sync-jira", DisplayName: "Sync Jira, edited", Purpose: "a new purpose",
			Fields: `[{"name":"ticket","required":true,"is_key":true}]`, SopMD: "# new sop",
			Learnings: "what we learned", Assignee: `{"kind":"outsource"}`, UpdatedTS: 1800000000,
		}
		if err := putTaskManualOn(d.wdb, edited); err != nil {
			t.Fatalf("putTaskManualOn: %v", err)
		}
		dalWantManual(t, d, edited)
		dalWantManual(t, d, bystander)
	})

	t.Run("a write inside a rolled-back transaction reaches the table not at all", func(t *testing.T) {
		d := newAPITestDAL(t)
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := putTaskManualOn(tx, dalTestManual("sync-jira")); err != nil {
			t.Fatalf("putTaskManualOn on a transaction: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		got, err := d.GetTaskManual("sync-jira")
		if err != nil {
			t.Fatalf("GetTaskManual: %v", err)
		}
		if got != nil {
			t.Fatalf("GetTaskManual after the rollback: want nil, got %+v", *got)
		}
		if err := putTaskManualOn(d.wdb, dalTestManual("sync-jira")); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
	})
}

func TestDeleteTaskManual(t *testing.T) {
	t.Run("the manual goes, its retained document history goes with it, and its neighbour stays", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutManual(t, d, dalTestManual("sync-jira"))
		bystander := dalPutManual(t, d, dalTestManual("ship-order"))
		dalSeedManualHistory(t, d, "sync-jira", docKindTaskManualSop)
		dalSeedManualHistory(t, d, "sync-jira", docKindTaskManualLearnings)
		dalSeedManualHistory(t, d, "ship-order", docKindTaskManualSop)

		deleted, err := d.DeleteTaskManual("sync-jira")
		if err != nil {
			t.Fatalf("DeleteTaskManual: %v", err)
		}
		if !deleted {
			t.Fatalf("DeleteTaskManual: want true, got false")
		}
		got, err := d.GetTaskManual("sync-jira")
		if err != nil || got != nil {
			t.Fatalf("GetTaskManual after the delete: want nil, got %+v (%v)", got, err)
		}
		dalWantManual(t, d, bystander)
		if n := dalManualHistoryCount(t, d, "sync-jira"); n != 0 {
			t.Fatalf("the deleted manual's retained revisions: want 0, got %d", n)
		}
		if n := dalManualHistoryCount(t, d, "ship-order"); n != 1 {
			t.Fatalf("the neighbour's retained revisions: want 1, got %d", n)
		}
	})

	t.Run("a type key naming no manual reports false and removes nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutManual(t, d, dalTestManual("sync-jira"))
		dalSeedManualHistory(t, d, "sync-jira", docKindTaskManualSop)

		deleted, err := d.DeleteTaskManual("never-a-type")
		if err != nil {
			t.Fatalf("DeleteTaskManual(never-a-type): %v", err)
		}
		if deleted {
			t.Fatalf("DeleteTaskManual(never-a-type): want false, got true")
		}
		dalWantManual(t, d, stored)
		if n := dalManualHistoryCount(t, d, "sync-jira"); n != 1 {
			t.Fatalf("the surviving manual's retained revisions: want 1, got %d", n)
		}
	})
}

// dalTestTask is a task row with every column carrying a distinct non-zero
// value, so a write that touches a column it should not shows up.

func dalTestTask(id string) Task {
	return Task{
		ID:                  id,
		TypeKey:             "type-alpha",
		Title:               "reconcile the yard ledger",
		DedupeKey:           "dedupe-alpha",
		Inputs:              map[string]any{"field": "value"},
		Description:         "what the ticket asks for",
		Status:              TaskStatusInProgress,
		Lock:                TaskLockReassigning,
		Priority:            TaskPriorityHigh,
		ExecutorKind:        "staff",
		ExecutorID:          "ann",
		CreatorID:           "bob",
		WaitingReason:       "waiting on the yard",
		CreatedTS:           1700000001,
		UpdatedTS:           1700000002,
		ClosedTS:            1700000003,
		CloseoutTS:          1700000004,
		DuplicateOf:         "T-900",
		ReassignedFrom:      "carl",
		ReassignedFromKind:  "staff",
		HandoverNote:        "handover note",
		HandoverNoteTS:      1700000005,
		HandoverNoteBy:      "dee",
		OutsourceRuntime:    "claude",
		OutsourceModel:      "sonnet",
		OutsourceEffort:     "high",
		OutsourceMachine:    "mac-1",
		OutsourceDispatched: true,
		Handoff:             HandoffFollowUp,
		HandoffNote:         "handoff note",
		HandoffTaskID:       "T-901",
		FrozenBy:            "eve",
		KickoffNotifiedTo:   "ow-1",
	}
}

// dalPutTask stores t and answers with it, so a test can name the row it
// seeded and the row it expects to read back in one literal.

func dalPutTask(t *testing.T, d *DAL, task Task) Task {
	t.Helper()
	if err := d.PutTask(task); err != nil {
		t.Fatalf("PutTask(%q): %v", task.ID, err)
	}
	return task
}

// dalWantTask asserts the stored task row reads back as want, whole.

func dalWantTask(t *testing.T, d *DAL, want Task) {
	t.Helper()
	got, err := d.GetTask(want.ID)
	if err != nil {
		t.Fatalf("GetTask(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetTask(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetTask(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

// dalTaskIDs names every stored task, by id, in a comparable order.

func dalTaskIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(`SELECT id FROM task ORDER BY id`)
	if err != nil {
		t.Fatalf("list task ids: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan task id: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// dalTestStep is one plan node with every column carrying a distinct non-zero
// value, so a write that touches a column it should not shows up.

func dalTestStep(id, taskID string) TaskStep {
	return TaskStep{
		ID:            id,
		TaskID:        taskID,
		OrderIdx:      3,
		Name:          "reconcile the yard",
		DoD:           "the yard agrees with the ledger",
		Status:        StepStatusInProgress,
		ParallelGroup: "group-a",
		IsGate:        true,
		ReplyCardID:   "rc-1",
		WaitingReason: "the yard has not answered",
		Note:          "got as far as the second container",
		StartedTS:     1700000001,
		FinishedTS:    1700000002,
	}
}

// dalStepFixture is dalTestStep placed at a position and a status.

func dalStepFixture(id, taskID string, orderIdx int, status string) TaskStep {
	st := dalTestStep(id, taskID)
	st.OrderIdx = orderIdx
	st.Status = status
	return st
}

func dalPutStep(t *testing.T, d *DAL, st TaskStep) TaskStep {
	t.Helper()
	if err := d.PutTaskStep(st); err != nil {
		t.Fatalf("PutTaskStep(%q): %v", st.ID, err)
	}
	return st
}

func dalPutStepWithStatus(t *testing.T, d *DAL, id, taskID, status string) TaskStep {
	t.Helper()
	return dalPutStep(t, d, dalStepFixture(id, taskID, 0, status))
}

func dalStepAt(t *testing.T, d *DAL, id, taskID string, orderIdx int, status string) TaskStep {
	t.Helper()
	return dalPutStep(t, d, dalStepFixture(id, taskID, orderIdx, status))
}

func dalWantStep(t *testing.T, d *DAL, want TaskStep) {
	t.Helper()
	got, err := d.GetTaskStep(want.ID)
	if err != nil {
		t.Fatalf("GetTaskStep(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetTaskStep(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetTaskStep(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

func dalStepIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	return dalScanStrings(t, d, `SELECT id FROM task_step ORDER BY id`)
}

func dalAddDep(t *testing.T, d *DAL, taskID, blockedBy string) {
	t.Helper()
	if err := d.AddTaskDep(taskID, blockedBy); err != nil {
		t.Fatalf("AddTaskDep(%q, %q): %v", taskID, blockedBy, err)
	}
}

func dalDepCount(t *testing.T, d *DAL) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM task_dep`).Scan(&n); err != nil {
		t.Fatalf("count task_dep: %v", err)
	}
	return n
}

// dalTestOutsourceMember is the member row a worker is stored as, with every
// column the projection carries holding a distinct value.

func dalTestOutsourceMember(id string) Member {
	m := dalTestMember(id, "O-7")
	m.Kind = KindOutsource
	m.RoleKey = ""
	m.Codename = "O-7"
	linked := "T-1"
	m.LinkedTaskID = &linked
	m.AvatarAttachmentID = "ava-1"
	m.HandoverNoticedTS = 0
	m.AgentIatFloor = 0
	m.TokenKeyID = ""
	return m
}

// dalTestWorker is the worker vocabulary dalTestOutsourceMember projects onto.

func dalTestWorker(id string) OutsourceWorker {
	ok := true
	return OutsourceWorker{
		ID:                 id,
		Codename:           "O-7",
		Runtime:            "claude",
		Model:              "sonnet",
		ActualModel:        "sonnet-4",
		ActualRuntime:      "codex",
		ActualEffort:       "high",
		Effort:             "medium",
		TaskID:             "T-1",
		Status:             WorkerStatusActive,
		ActivatedTS:        1700000011,
		CreatedTS:          1700000000,
		ReleasedTS:         1700000010,
		LastOp:             "stop",
		LastOpOK:           &ok,
		LastOpLog:          "log line",
		LastOpReason:       "code: detail",
		LastOpAt:           1700000009,
		DesiredMachineID:   "mac-1",
		LastMachineID:      "mac-0",
		SessionBootTS:      1700000001,
		RefocusSince:       1700000005,
		RefocusOp:          "refocus",
		StoppingSince:      1700000003,
		StoppedSince:       1700000004,
		WakingSince:        1700000002,
		ForcedStopAt:       1700000006,
		DesiredState:       "online",
		RestartAfterStop:   true,
		BankedCost:         12.5,
		AvatarAttachmentID: "ava-1",
	}
}

// dalWorkerOnTask stores one active worker bound to a task and answers with the
// projection it reads back as.

func dalWorkerOnTask(t *testing.T, d *DAL, id, codename, taskID string, createdTS float64) OutsourceWorker {
	t.Helper()
	m := dalTestOutsourceMember(id)
	m.Codename = codename
	m.Name = codename
	m.CreatedTS = createdTS
	m.ReleasedTS = 0
	m.LinkedTaskID = &taskID
	dalPutMember(t, d, m)
	return workerFromMember(m)
}

// dalReleasedWorker is the projection a release leaves behind.

func dalReleasedWorker(w OutsourceWorker, now float64) OutsourceWorker {
	w.Status = WorkerStatusReleased
	w.ReleasedTS = now
	return w
}

func dalWantWorker(t *testing.T, d *DAL, want OutsourceWorker) {
	t.Helper()
	got, err := d.GetOutsourceWorker(want.ID)
	if err != nil {
		t.Fatalf("GetOutsourceWorker(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetOutsourceWorker(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetOutsourceWorker(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

// dalTestManual is one task type's manual with every column carrying a distinct
// non-zero value.

func dalTestManual(typeKey string) TaskManual {
	return TaskManual{
		TypeKey:     typeKey,
		DisplayName: "Manual for " + typeKey,
		Purpose:     "what this task type is for",
		Fields:      `[{"name":"ticket","required":true,"is_key":true}]`,
		SopMD:       "# how to do it",
		Learnings:   "what went wrong last time",
		Assignee:    `{"kind":"staff","id":"ann"}`,
		UpdatedTS:   1700000001,
	}
}

func dalPutManual(t *testing.T, d *DAL, m TaskManual) TaskManual {
	t.Helper()
	if err := d.PutTaskManual(m); err != nil {
		t.Fatalf("PutTaskManual(%q): %v", m.TypeKey, err)
	}
	return m
}

func dalWantManual(t *testing.T, d *DAL, want TaskManual) {
	t.Helper()
	got, err := d.GetTaskManual(want.TypeKey)
	if err != nil {
		t.Fatalf("GetTaskManual(%q): %v", want.TypeKey, err)
	}
	if got == nil {
		t.Fatalf("GetTaskManual(%q): no row", want.TypeKey)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetTaskManual(%q):\n got %+v\nwant %+v", want.TypeKey, *got, want)
	}
}

// dalSeedManualHistory writes one retained revision of a manual document, so a
// delete's cascade has something to reach.

func dalSeedManualHistory(t *testing.T, d *DAL, documentKey, documentKind string) {
	t.Helper()
	if _, err := d.wdb.Exec(`INSERT INTO document_history
		(document_kind, document_key, content_json, created_ts, actor_id)
		VALUES (?, ?, ?, ?, ?)`,
		documentKind, documentKey, `{"body":"an earlier revision"}`, 1700000001.0, "ann"); err != nil {
		t.Fatalf("seed a revision of %q: %v", documentKey, err)
	}
}

func dalManualHistoryCount(t *testing.T, d *DAL, documentKey string) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM document_history WHERE document_key = ?`, documentKey).Scan(&n); err != nil {
		t.Fatalf("count revisions of %q: %v", documentKey, err)
	}
	return n
}
