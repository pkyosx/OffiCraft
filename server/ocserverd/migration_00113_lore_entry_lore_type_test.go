package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openLoreAt112(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "lore-type.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 112); err != nil {
		t.Fatalf("goose up to 112: %v", err)
	}
	return db
}

type loreTitleRow struct{ id, title, loreType string }

func readLoreTitles(t *testing.T, db *sql.DB, withType bool) []loreTitleRow {
	t.Helper()
	q := `SELECT id, title, '' FROM lore_entry ORDER BY seq`
	if withType {
		q = `SELECT id, title, lore_type FROM lore_entry ORDER BY seq`
	}
	rows, err := db.Query(q)
	if err != nil {
		t.Fatalf("read lore_entry: %v", err)
	}
	defer rows.Close()
	var out []loreTitleRow
	for rows.Next() {
		var r loreTitleRow
		if err := rows.Scan(&r.id, &r.title, &r.loreType); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func wantLoreTitles(t *testing.T, label string, got, want []loreTitleRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: rows = %+v, want %+v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: row %d = %+v, want %+v", label, i, got[i], want[i])
		}
	}
}

var lore00113Before = []loreTitleRow{
	{"L-1", "[指示衝突] restart_self 不會自行完成重啟", ""},
	{"L-2", "[指示補充] 缺少的條件", ""},
	{"L-3", "[Owner 決策] 先驗證真實情境", ""},
	{"L-4", "[Owner 偏好] 回覆用中文", ""},
	{"L-5", "[工作原則] 先讀規格再動手", ""},
	{"L-6", "沒有前綴的傳承", ""},
	{"L-7", "先讀 [Owner 決策] 再動手", ""},
	{"L-8", "[Owner 決策]沒有空格", ""},
	{"L-9", "[owner 決策] 小寫", ""},
	{"L-10", "[指示衝突] [工作原則] 疊兩層", ""},
	{"L-11", "[指示衝突] [Owner 決策] 兩個前綴", ""},
}

func seedLore00113(t *testing.T, db *sql.DB) {
	t.Helper()
	for i, r := range lore00113Before {
		if _, err := db.Exec(`INSERT INTO lore_entry
			(id, seq, scope_kind, scope_key, title, body, author_id, source_task_id,
			 state, retire_reason, effective_ts, created_ts, updated_ts)
			VALUES (?, ?, 'agent', 'm-a', ?, '內容', 'm-a', '', 'active', '', 1, 1, 1)`,
			r.id, i+1, r.title); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
}

func TestMigration00113(t *testing.T) {
	t.Run("up moves each title prefix onto lore_type and backs up every rewritten title", func(t *testing.T) {
		db := openLoreAt112(t)
		seedLore00113(t, db)
		if err := goose.UpTo(db, "migrations", 113); err != nil {
			t.Fatalf("goose up to 113: %v", err)
		}
		wantLoreTitles(t, "after 00113", readLoreTitles(t, db, true), []loreTitleRow{
			{"L-1", "restart_self 不會自行完成重啟", "instruction_conflict"},
			{"L-2", "缺少的條件", "instruction_supplement"},
			{"L-3", "先驗證真實情境", "owner_decision"},
			{"L-4", "回覆用中文", "owner_preference"},
			{"L-5", "[工作原則] 先讀規格再動手", "other"},
			{"L-6", "沒有前綴的傳承", ""},
			{"L-7", "先讀 [Owner 決策] 再動手", ""},
			{"L-8", "[Owner 決策]沒有空格", ""},
			{"L-9", "[owner 決策] 小寫", ""},
			{"L-10", "[工作原則] 疊兩層", "instruction_conflict"},
			{"L-11", "[Owner 決策] 兩個前綴", "instruction_conflict"},
		})

		rows, err := db.Query(`SELECT id, title, '' FROM lore_entry_title_backup_00113 ORDER BY id`)
		if err != nil {
			t.Fatalf("read backup: %v", err)
		}
		var backup []loreTitleRow
		for rows.Next() {
			var r loreTitleRow
			if err := rows.Scan(&r.id, &r.title, &r.loreType); err != nil {
				t.Fatalf("scan backup: %v", err)
			}
			backup = append(backup, r)
		}
		rows.Close()
		wantLoreTitles(t, "backup", backup, []loreTitleRow{
			{"L-1", "[指示衝突] restart_self 不會自行完成重啟", ""},
			{"L-10", "[指示衝突] [工作原則] 疊兩層", ""},
			{"L-11", "[指示衝突] [Owner 決策] 兩個前綴", ""},
			{"L-2", "[指示補充] 缺少的條件", ""},
			{"L-3", "[Owner 決策] 先驗證真實情境", ""},
			{"L-4", "[Owner 偏好] 回覆用中文", ""},
		})

		if _, err := db.Exec(`UPDATE lore_entry SET lore_type = 'bogus' WHERE id = 'L-6'`); err == nil {
			t.Fatal("after 00113 the CHECK admits an arbitrary lore_type")
		}
		if _, err := db.Exec(`INSERT INTO lore_entry
			(id, seq, scope_kind, scope_key, title, body, author_id, source_task_id,
			 state, retire_reason, effective_ts, created_ts, updated_ts)
			VALUES ('L-99', 99, 'agent', 'm-a', 't', 'b', 'm-a', '', 'active', '', 1, 1, 1)`); err != nil {
			t.Fatalf("an insert that names no lore_type is refused: %v", err)
		}
		var lt string
		if err := db.QueryRow(`SELECT lore_type FROM lore_entry WHERE id = 'L-99'`).Scan(&lt); err != nil || lt != "" {
			t.Fatalf("default lore_type = %q / %v, want \"\"", lt, err)
		}
	})

	t.Run("down restores every title and removes the column and the backup", func(t *testing.T) {
		db := openLoreAt112(t)
		seedLore00113(t, db)
		if err := goose.UpTo(db, "migrations", 113); err != nil {
			t.Fatalf("goose up to 113: %v", err)
		}
		if err := goose.DownTo(db, "migrations", 112); err != nil {
			t.Fatalf("goose down to 112: %v", err)
		}
		wantLoreTitles(t, "after the Down", readLoreTitles(t, db, false), lore00113Before)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lore_entry') WHERE name = 'lore_type'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("lore_type columns after the Down = %d / %v, want 0", n, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'lore_entry_title_backup_00113'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("backup tables after the Down = %d / %v, want 0", n, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_lore_entry_scope_state_effective'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("selection index after the Down = %d / %v, want 1", n, err)
		}
	})
}
