package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openAt123(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "chat-deleted-ts.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 123); err != nil {
		t.Fatalf("goose up to 123: %v", err)
	}
	return db
}

type chatDeletedRow struct {
	id, meta  string
	deletedTS float64
}

func readChatDeletedRows(t *testing.T, db *sql.DB) []chatDeletedRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, meta, deleted_ts FROM chat_message ORDER BY id`)
	if err != nil {
		t.Fatalf("read chat_message: %v", err)
	}
	defer rows.Close()
	var out []chatDeletedRow
	for rows.Next() {
		var r chatDeletedRow
		if err := rows.Scan(&r.id, &r.meta, &r.deletedTS); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func TestMigration00124(t *testing.T) {
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		for _, r := range [][2]string{
			{"c-1", `{"recalled_ts":50.5,"source":"cli"}`},
			{"c-2", `{"source":"cli"}`},
			{"c-3", `{this is not json`},
			{"c-4", `{"recalled_ts":0}`},
			{"c-5", `{"recalled_ts":"yes"}`},
		} {
			if _, err := db.Exec(`INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
				VALUES (?, 'kip', 'mira', 'hi', 1, ?)`, r[0], r[1]); err != nil {
				t.Fatalf("seed %s at 123: %v", r[0], err)
			}
		}
	}

	t.Run("a deletion held in meta.recalled_ts moves to the column and leaves meta; every other row is untouched", func(t *testing.T) {
		db := openAt123(t)
		seed(t, db)
		if err := goose.UpTo(db, "migrations", 124); err != nil {
			t.Fatalf("goose up to 124: %v", err)
		}
		got := readChatDeletedRows(t, db)
		want := []chatDeletedRow{
			{"c-1", `{"source":"cli"}`, 50.5},
			{"c-2", `{"source":"cli"}`, 0},
			{"c-3", `{this is not json`, 0},
			{"c-4", `{"recalled_ts":0}`, 0},
			{"c-5", `{"recalled_ts":"yes"}`, 0},
		}
		if len(got) != len(want) {
			t.Fatalf("rows = %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("down puts the time back into meta and drops the column and its index", func(t *testing.T) {
		db := openAt123(t)
		seed(t, db)
		if err := goose.UpTo(db, "migrations", 124); err != nil {
			t.Fatalf("goose up to 124: %v", err)
		}
		if err := goose.DownTo(db, "migrations", 123); err != nil {
			t.Fatalf("goose down to 123: %v", err)
		}
		var meta string
		if err := db.QueryRow(`SELECT meta FROM chat_message WHERE id = 'c-1'`).Scan(&meta); err != nil {
			t.Fatalf("read c-1: %v", err)
		}
		if meta != `{"source":"cli","recalled_ts":50.5}` {
			t.Fatalf("c-1 meta after the Down = %s", meta)
		}
		var cols, idx int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('chat_message') WHERE name = 'deleted_ts'`).Scan(&cols); err != nil {
			t.Fatalf("pragma_table_info: %v", err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'idx_chat_message_recipient_sender_deleted_ts'`).Scan(&idx); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		if cols != 0 || idx != 0 {
			t.Fatalf("after the Down: %d deleted_ts columns, %d indexes, want 0 and 0", cols, idx)
		}
	})
}
