package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openAt120(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "reply-card-snooze.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 120); err != nil {
		t.Fatalf("goose up to 120: %v", err)
	}
	return db
}

func TestMigration00121(t *testing.T) {
	t.Run("a waiting card stored before the column reads as not snoozed and counts as waiting", func(t *testing.T) {
		db := openAt120(t)
		if _, err := db.Exec(`INSERT INTO reply_card (id, from_member, kind, summary, options, status, created_ts)
			VALUES ('rc-old', 'kip', 'decision', '要不要漲價', '[{"text":"漲","ai_pick":false}]', 'waiting', 1700000000)`); err != nil {
			t.Fatalf("seed card at 120: %v", err)
		}
		if err := goose.Up(db, "migrations"); err != nil {
			t.Fatalf("goose up: %v", err)
		}
		var stored float64
		if err := db.QueryRow(`SELECT snoozed_until_ts FROM reply_card WHERE id = 'rc-old'`).Scan(&stored); err != nil {
			t.Fatalf("read snoozed_until_ts: %v", err)
		}
		if stored != 0 {
			t.Fatalf("snoozed_until_ts after 00121 = %v, want 0", stored)
		}

		_, h, _, owner := newAPITestServerOn(t, NewDAL(db))
		rows := apiTestReplyCardPane(t, h, owner, "")
		if len(rows) != 1 {
			t.Fatalf("waiting pane = %v, want the one old card", rows)
		}
		apiWantValue(t, "row.snoozed_until_ts", rows[0].(map[string]any)["snoozed_until_ts"], nil)
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/rc-old", owner, "")
		apiWantValue(t, "card.snoozed_until_ts", card["snoozed_until_ts"], nil)
		_, counts := apiJSON(t, h, "GET", "/api/reply-cards/count", owner, "")
		apiWantBody(t, counts, map[string]any{"waiting": 1, "answered": 0, "expired": 0, "waiting_next_change_ts": nil})
	})

	t.Run("down drops the column", func(t *testing.T) {
		db := openAt120(t)
		if err := goose.UpTo(db, "migrations", 121); err != nil {
			t.Fatalf("goose up to 121: %v", err)
		}
		if err := goose.DownTo(db, "migrations", 120); err != nil {
			t.Fatalf("goose down to 120: %v", err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('reply_card') WHERE name = 'snoozed_until_ts'`).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info: %v", err)
		}
		if n != 0 {
			t.Fatalf("snoozed_until_ts columns after the Down = %d, want 0", n)
		}
	})
}
