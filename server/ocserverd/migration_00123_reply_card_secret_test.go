package main

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

func openAt121(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "reply-card-secret.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 121); err != nil {
		t.Fatalf("goose up to 121: %v", err)
	}
	return db
}

const migration00123CardRow = `SELECT from_member, kind, summary, body, options, select_mode, status,
	created_ts, answered_ts, expired_ts, chat_message_id, COALESCE(answer_option_idxs, 'NULL'), answer_text,
	answer_attachments, attachments, task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts
	FROM reply_card WHERE id = ?`

func migration00123ReadRow(t *testing.T, db *sql.DB, id string) [19]any {
	t.Helper()
	var row [19]any
	ptrs := make([]any, len(row))
	for i := range row {
		ptrs[i] = &row[i]
	}
	if err := db.QueryRow(migration00123CardRow, id).Scan(ptrs...); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row
}

func TestMigration00123(t *testing.T) {
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		for _, stmt := range []string{
			`INSERT INTO reply_card (id, from_member, kind, summary, body, options, select_mode, status, created_ts,
				answered_ts, expired_ts, chat_message_id, answer_option_idxs, answer_text, answer_attachments, attachments,
				task_id, task_step_id, answer_reply_excerpt, snoozed_until_ts)
				VALUES ('rc-decision', 'kip', 'decision', 's1', 'b1', '[{"text":"a","ai_pick":true}]', 'multi', 'answered', 10,
				11, 0, 'c-1', '[0]', 'typed', '[{"id":"x"}]', '[{"id":"y"}]', 'T-1', 'ts-1', 'quoted', 0)`,
			`INSERT INTO reply_card (id, from_member, kind, summary, status, created_ts, snoozed_until_ts)
				VALUES ('rc-action', 'mira', 'action', 's2', 'waiting', 20, 99)`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
	}

	t.Run("up keeps every row, column and the index, and admits the secret kind", func(t *testing.T) {
		db := openAt121(t)
		seed(t, db)
		before := []any{migration00123ReadRow(t, db, "rc-decision"), migration00123ReadRow(t, db, "rc-action")}
		if err := goose.UpTo(db, "migrations", 123); err != nil {
			t.Fatalf("up: %v", err)
		}
		after := []any{migration00123ReadRow(t, db, "rc-decision"), migration00123ReadRow(t, db, "rc-action")}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("rows changed:\n got %#v\nwant %#v", after, before)
		}
		var provided, retrieved, destroy float64
		if err := db.QueryRow(`SELECT secret_provided_ts, secret_retrieved_ts, secret_destroy_after_ts FROM reply_card WHERE id='rc-action'`).
			Scan(&provided, &retrieved, &destroy); err != nil {
			t.Fatal(err)
		}
		if provided != 0 || retrieved != 0 || destroy != 0 {
			t.Fatalf("new columns = %v %v %v", provided, retrieved, destroy)
		}
		var index string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='reply_card' AND name='idx_reply_card_status'`).Scan(&index); err != nil {
			t.Fatalf("index gone: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO reply_card (id, kind, summary) VALUES ('rc-secret','secret','s')`); err != nil {
			t.Fatalf("secret kind refused: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO reply_card (id, kind, summary) VALUES ('rc-poll','poll','s')`); err == nil {
			t.Fatal("the CHECK no longer refuses an unknown kind")
		}
	})

	t.Run("down turns secret cards into action cards, drops the values, and up again works", func(t *testing.T) {
		db := openAt121(t)
		seed(t, db)
		if err := goose.UpTo(db, "migrations", 123); err != nil {
			t.Fatalf("up: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO reply_card (id, from_member, kind, summary, status, created_ts, answered_ts, secret_provided_ts, secret_destroy_after_ts)
			VALUES ('rc-secret', 'kip', 'secret', 's3', 'answered', 30, 31, 31, 99)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO reply_card_secret (card_id, nonce, value_ct, key_version) VALUES ('rc-secret', x'00', x'00', 1)`); err != nil {
			t.Fatal(err)
		}
		before := []any{migration00123ReadRow(t, db, "rc-decision"), migration00123ReadRow(t, db, "rc-action")}
		if err := goose.DownTo(db, "migrations", 121); err != nil {
			t.Fatalf("down: %v", err)
		}
		var kind, status string
		if err := db.QueryRow(`SELECT kind, status FROM reply_card WHERE id='rc-secret'`).Scan(&kind, &status); err != nil {
			t.Fatal(err)
		}
		if kind != "action" || status != "answered" {
			t.Fatalf("secret row after down: %s %s", kind, status)
		}
		var tables int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='reply_card_secret'`).Scan(&tables); err != nil || tables != 0 {
			t.Fatalf("reply_card_secret survived: %d %v", tables, err)
		}
		if _, err := db.Exec(`SELECT secret_provided_ts FROM reply_card`); err == nil {
			t.Fatal("secret_provided_ts survived the down")
		}
		if _, err := db.Exec(`INSERT INTO reply_card (id, kind, summary) VALUES ('rc-x','secret','s')`); err == nil {
			t.Fatal("the CHECK still admits secret after the down")
		}
		after := []any{migration00123ReadRow(t, db, "rc-decision"), migration00123ReadRow(t, db, "rc-action")}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("rows changed:\n got %#v\nwant %#v", after, before)
		}
		if err := goose.UpTo(db, "migrations", 123); err != nil {
			t.Fatalf("second up: %v", err)
		}
		if err := db.QueryRow(`SELECT kind FROM reply_card WHERE id='rc-secret'`).Scan(&kind); err != nil || kind != "action" {
			t.Fatalf("second up: %s %v", kind, err)
		}
		if _, err := db.Exec(`INSERT INTO reply_card_secret (card_id, nonce, value_ct, key_version) VALUES ('rc-action', x'00', x'00', 1)`); err != nil {
			t.Fatalf("table back: %v", err)
		}
	})
}
