package main

import (
	"database/sql"
	"path/filepath"
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
		var title string
		if err := db.QueryRow(`SELECT title FROM task WHERE id = 'T-1'`).Scan(&title); err != nil {
			t.Fatalf("read the task back: %v", err)
		}
		if title != "Ship it" {
			t.Fatalf("title = %q, want %q", title, "Ship it")
		}
	})

	t.Run("the down restores the three columns empty", func(t *testing.T) {
		db := openTaskBefore110(t)
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
