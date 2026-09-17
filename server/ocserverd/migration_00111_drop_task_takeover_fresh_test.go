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

func bootDocKindsLeft(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	kinds := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan: %v", err)
		}
		kinds = append(kinds, k)
	}
	return kinds
}

func TestMigration00111(t *testing.T) {
	t.Run("up deletes the retired kind's overlay and history rows and nothing else", func(t *testing.T) {
		db := openBootDocsBefore111(t)

		if err := goose.UpTo(db, "migrations", 111); err != nil {
			t.Fatalf("goose up to 111: %v", err)
		}

		if got, want := bootDocKindsLeft(t, db,
			`SELECT doc_kind FROM boot_document ORDER BY doc_kind`),
			[]string{"task_closeout", "task_takeover_with_predecessor"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("boot_document kinds after 00111 = %v, want %v", got, want)
		}
		if got, want := bootDocKindsLeft(t, db,
			`SELECT document_kind FROM document_history ORDER BY document_kind`),
			[]string{"task_closeout", "task_takeover_fresh_x", "task_takeover_with_predecessor"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("document_history kinds after 00111 = %v, want %v", got, want)
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

		if got, want := bootDocKindsLeft(t, db,
			`SELECT doc_kind FROM boot_document ORDER BY doc_kind`),
			[]string{"task_closeout", "task_takeover_with_predecessor"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("boot_document kinds after the Down = %v, want %v", got, want)
		}
		if got, want := bootDocKindsLeft(t, db,
			`SELECT document_kind FROM document_history ORDER BY document_kind`),
			[]string{"task_closeout", "task_takeover_fresh_x", "task_takeover_with_predecessor"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("document_history kinds after the Down = %v, want %v", got, want)
		}
	})
}
