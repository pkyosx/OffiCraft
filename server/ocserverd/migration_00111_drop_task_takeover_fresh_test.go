package main

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

func openBootDocsBefore111(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t239.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 110); err != nil {
		t.Fatalf("goose up to 110: %v", err)
	}
	for _, kind := range []string{"task_takeover_fresh", "task_takeover_with_predecessor", "task_closeout"} {
		if _, err := db.Exec(`INSERT INTO boot_document (doc_kind, doc_key, text, tombstoned)
			VALUES (?, 'global', 'text of '||?, 0)`, kind, kind); err != nil {
			t.Fatalf("seed boot_document %s: %v", kind, err)
		}
		if _, err := db.Exec(`INSERT INTO document_history
			(document_kind, document_key, content_json, created_ts, actor_id)
			VALUES (?, 'global', '{"text":"old"}', 1.5, 'owner')`, kind); err != nil {
			t.Fatalf("seed document_history %s: %v", kind, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO document_history
		(document_kind, document_key, content_json, created_ts, actor_id)
		VALUES ('task_takeover_fresh_x', 'global', '{"text":"old"}', 1.5, 'owner')`); err != nil {
		t.Fatalf("seed a neighbouring kind: %v", err)
	}
	return db
}

func bootDocRowsLeft(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return migrationRowsAsText(t, db,
		`SELECT doc_kind || '|' || doc_key || '|' || text || '|' || tombstoned
		   FROM boot_document ORDER BY doc_kind, doc_key`)
}

func documentHistoryRowsLeft(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return migrationRowsAsText(t, db,
		`SELECT id || '|' || document_kind || '|' || document_key || '|' || content_json || '|' || created_ts || '|' || actor_id
		   FROM document_history ORDER BY id`)
}

func migrationRowsAsText(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, row)
	}
	return got
}

var (
	bootDocRowsAfter00111 = []string{
		"task_closeout|global|text of task_closeout|0",
		"task_takeover_with_predecessor|global|text of task_takeover_with_predecessor|0",
	}
	documentHistoryRowsAfter00111 = []string{
		`2|task_takeover_with_predecessor|global|{"text":"old"}|1.5|owner`,
		`3|task_closeout|global|{"text":"old"}|1.5|owner`,
		`4|task_takeover_fresh_x|global|{"text":"old"}|1.5|owner`,
	}
)

func TestMigration00111(t *testing.T) {
	t.Run("up deletes the retired kind's overlay and history rows and nothing else", func(t *testing.T) {
		db := openBootDocsBefore111(t)

		if err := goose.UpTo(db, "migrations", 111); err != nil {
			t.Fatalf("goose up to 111: %v", err)
		}

		if got := bootDocRowsLeft(t, db); !reflect.DeepEqual(got, bootDocRowsAfter00111) {
			t.Fatalf("boot_document after 00111 = %q, want %q", got, bootDocRowsAfter00111)
		}
		if got := documentHistoryRowsLeft(t, db); !reflect.DeepEqual(got, documentHistoryRowsAfter00111) {
			t.Fatalf("document_history after 00111 = %q, want %q", got, documentHistoryRowsAfter00111)
		}
	})

	t.Run("down is a no-op that leaves the surviving rows and brings nothing back", func(t *testing.T) {
		db := openBootDocsBefore111(t)
		if err := goose.UpTo(db, "migrations", 111); err != nil {
			t.Fatalf("goose up to 111: %v", err)
		}

		if err := goose.DownTo(db, "migrations", 110); err != nil {
			t.Fatalf("goose down to 110: %v", err)
		}

		if got := bootDocRowsLeft(t, db); !reflect.DeepEqual(got, bootDocRowsAfter00111) {
			t.Fatalf("boot_document after the Down = %q, want %q", got, bootDocRowsAfter00111)
		}
		if got := documentHistoryRowsLeft(t, db); !reflect.DeepEqual(got, documentHistoryRowsAfter00111) {
			t.Fatalf("document_history after the Down = %q, want %q", got, documentHistoryRowsAfter00111)
		}
	})
}
