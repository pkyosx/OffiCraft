package main

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

var taskHandoffColumns = []string{"handoff", "handoff_note", "handoff_task_id"}

func openTaskBefore110(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t246.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 109); err != nil {
		t.Fatalf("goose up to 109: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO task (id, title, handoff, handoff_note, handoff_task_id)
		VALUES ('T-1', 'Ship it', 'follow_up', 'see T-2', 'T-2')`); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return db
}

func readTaskRow(t *testing.T, db *sql.DB, id string) map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM task WHERE id = ?`, id)
	if err != nil {
		t.Fatalf("read task %s: %v", id, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if !rows.Next() {
		t.Fatalf("task %s is gone", id)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatalf("scan task %s: %v", id, err)
	}
	row := make(map[string]any, len(cols))
	for i, c := range cols {
		row[c] = vals[i]
	}
	return row
}

func TestMigration00110DropTaskHandoff(t *testing.T) {
	t.Run("the up drops the three handoff columns and keeps the task row", func(t *testing.T) {
		db := openTaskBefore110(t)

		if err := goose.UpTo(db, "migrations", 110); err != nil {
			t.Fatalf("goose up to 110: %v", err)
		}

		for _, col := range taskHandoffColumns {
			if t186ColumnExists(t, db, "task", col) {
				t.Fatalf("task.%s survived the upgrade", col)
			}
		}
		want := map[string]any{
			"id":                    "T-1",
			"type_key":              "",
			"title":                 "Ship it",
			"dedupe_key":            "",
			"inputs":                "{}",
			"description":           "",
			"status":                "not_started",
			"priority":              "mid",
			"executor_kind":         "staff",
			"executor_id":           "",
			"waiting_reason":        "",
			"created_ts":            float64(0),
			"updated_ts":            float64(0),
			"closed_ts":             float64(0),
			"closeout_ts":           float64(0),
			"creator_id":            "",
			"duplicate_of":          "",
			"reassigned_from":       "",
			"reassigned_from_kind":  "",
			"lock":                  "",
			"outsource_model":       "",
			"outsource_effort":      "",
			"outsource_machine":     "",
			"outsource_runtime":     "claude",
			"outsource_dispatched":  int64(0),
			"frozen_by":             "",
			"handover_note":         "",
			"handover_note_ts":      float64(0),
			"handover_note_by":      "",
			"kickoff_notified_to":   "",
			"forced_done_by":        "",
			"forced_done_reason":    "",
			"ready_for_done_visits": int64(0),
		}
		if got := readTaskRow(t, db, "T-1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("surviving row:\n got %#v\nwant %#v", got, want)
		}
	})

	t.Run("the down restores the three columns empty", func(t *testing.T) {
		db := openTaskBefore110(t)
		var seededHandoff, seededNote, seededTaskID string
		if err := db.QueryRow(`SELECT handoff, handoff_note, handoff_task_id
			FROM task WHERE id = 'T-1'`).Scan(&seededHandoff, &seededNote, &seededTaskID); err != nil {
			t.Fatalf("read the seeded columns: %v", err)
		}
		if seededHandoff != "follow_up" || seededNote != "see T-2" || seededTaskID != "T-2" {
			t.Fatalf("seeded columns = (%q, %q, %q), want (%q, %q, %q)",
				seededHandoff, seededNote, seededTaskID, "follow_up", "see T-2", "T-2")
		}
		if err := goose.UpTo(db, "migrations", 110); err != nil {
			t.Fatalf("goose up to 110: %v", err)
		}

		if err := goose.DownTo(db, "migrations", 109); err != nil {
			t.Fatalf("goose down to 109: %v", err)
		}

		var handoff, note, taskID string
		if err := db.QueryRow(`SELECT handoff, handoff_note, handoff_task_id
			FROM task WHERE id = 'T-1'`).Scan(&handoff, &note, &taskID); err != nil {
			t.Fatalf("read the restored columns: %v", err)
		}
		if handoff != "" || note != "" || taskID != "" {
			t.Fatalf("restored columns = (%q, %q, %q), want all empty", handoff, note, taskID)
		}
	})
}
