package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openAt124(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "chat-delete-notice.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 124); err != nil {
		t.Fatalf("goose up to 124: %v", err)
	}
	return db
}

func TestMigration00125(t *testing.T) {
	recalledBody := "Owner 收回了訊息 #c-gone，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=c-gone 讀回。"
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		for _, r := range []struct {
			id, sender, body, meta string
			deletedTS              float64
		}{
			{"c-gone", "owner", "打錯了", `{}`, 10},
			{"c-kept", "owner", "還在", `{}`, 0},
			{"c-n1", "system", chatDeleteNoticeBody("c-gone"), `{"reply_to":"c-gone"}`, 0},
			{"c-n2", "system", recalledBody, `{"reply_to":"c-gone"}`, 0},
			{"c-x1", "system", chatDeleteNoticeBody("c-kept"), `{"reply_to":"c-kept"}`, 0},
			{"c-x2", "mira", chatDeleteNoticeBody("c-gone"), `{"reply_to":"c-gone"}`, 0},
			{"c-x3", "system", chatDeleteNoticeBody("c-gone") + "!", `{"reply_to":"c-gone"}`, 0},
			{"c-x4", "system", chatDeleteNoticeBody("c-gone"), `{this is not json`, 0},
			{"c-x5", "system", chatDeleteNoticeBody("c-gone"), `null`, 0},
			{"c-x6", "system", chatDeleteNoticeBody("c-gone"), `{}`, 0},
			{"c-x7", "system", chatDeleteNoticeBody("c-gone"), `{"reply_to":7}`, 0},
			{"c-x8", "system", chatDeleteNoticeBody("c-gone"),
				`{"reply_to":"c-gone","delete_notice":{"message_id":"c-other"}}`, 0},
		} {
			if _, err := db.Exec(`INSERT INTO chat_message (id, sender, recipient, body, ts, meta, deleted_ts)
				VALUES (?, ?, 'kip', ?, 1, ?, ?)`, r.id, r.sender, r.body, r.meta, r.deletedTS); err != nil {
				t.Fatalf("seed %s at 124: %v", r.id, err)
			}
		}
	}
	metaOf := func(t *testing.T, db *sql.DB) map[string]string {
		t.Helper()
		rows, err := db.Query(`SELECT id, meta FROM chat_message ORDER BY id`)
		if err != nil {
			t.Fatalf("read chat_message: %v", err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var id, meta string
			if err := rows.Scan(&id, &meta); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out[id] = meta
		}
		return out
	}

	t.Run("only system notices of a deleted message, in either wording, get the marker", func(t *testing.T) {
		db := openAt124(t)
		seed(t, db)
		if err := goose.UpTo(db, "migrations", 125); err != nil {
			t.Fatalf("goose up to 125: %v", err)
		}
		marked := `{"reply_to":"c-gone","delete_notice":{"message_id":"c-gone"}}`
		want := map[string]string{
			"c-gone": `{}`,
			"c-kept": `{}`,
			"c-n1":   marked,
			"c-n2":   marked,
			"c-x1":   `{"reply_to":"c-kept"}`,
			"c-x2":   `{"reply_to":"c-gone"}`,
			"c-x3":   `{"reply_to":"c-gone"}`,
			"c-x4":   `{this is not json`,
			"c-x5":   `null`,
			"c-x6":   `{}`,
			"c-x7":   `{"reply_to":7}`,
			"c-x8":   `{"reply_to":"c-gone","delete_notice":{"message_id":"c-other"}}`,
		}
		got := metaOf(t, db)
		for id, w := range want {
			if got[id] != w {
				t.Errorf("%s meta = %s, want %s", id, got[id], w)
			}
		}
	})

	t.Run("down removes the marker and nothing else", func(t *testing.T) {
		db := openAt124(t)
		seed(t, db)
		if err := goose.UpTo(db, "migrations", 125); err != nil {
			t.Fatalf("goose up to 125: %v", err)
		}
		if err := goose.DownTo(db, "migrations", 124); err != nil {
			t.Fatalf("goose down to 124: %v", err)
		}
		got := metaOf(t, db)
		for _, id := range []string{"c-n1", "c-n2"} {
			if got[id] != `{"reply_to":"c-gone"}` {
				t.Errorf("%s meta after the Down = %s", id, got[id])
			}
		}
	})
}
