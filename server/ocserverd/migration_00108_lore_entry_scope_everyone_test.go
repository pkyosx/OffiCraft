package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openLoreAt107(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t236.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 107); err != nil {
		t.Fatalf("goose up to 107: %v", err)
	}
	return db
}

func insertLoreKind(db *sql.DB, id string, seq int, kind, key string) error {
	_, err := db.Exec(`INSERT INTO lore_entry
		(id, seq, scope_kind, scope_key, title, body, author_id, source_task_id,
		 state, retire_reason, effective_ts, created_ts, updated_ts)
		VALUES (?, ?, ?, ?, 'title '||?, 'body '||?, 'm-a', 't-1', 'retired', 'why', 1.5, 2.5, 3.5)`,
		id, seq, kind, key, id, id)
	return err
}

func TestMigration00108AdmitsEveryoneAndKeepsEveryRowAndTheIndex(t *testing.T) {
	db := openLoreAt107(t)
	if err := insertLoreKind(db, "L-9", 9, "everyone", ""); err == nil {
		t.Fatal("before 00108 the CHECK admitted 'everyone' — nothing below tests the widening")
	}
	for _, row := range []struct {
		id        string
		seq       int
		kind, key string
	}{{"L-1", 1, "role", "r-x"}, {"L-2", 2, "agent", "m-a"}, {"L-3", 3, "manual", "tm-a"}} {
		if err := insertLoreKind(db, row.id, row.seq, row.kind, row.key); err != nil {
			t.Fatalf("seed %s: %v", row.kind, err)
		}
	}

	if err := goose.UpTo(db, "migrations", 108); err != nil {
		t.Fatalf("goose up to 108: %v", err)
	}

	if err := insertLoreKind(db, "L-4", 4, "everyone", ""); err != nil {
		t.Fatalf("after 00108 'everyone' is still refused: %v", err)
	}
	if err := insertLoreKind(db, "L-5", 5, "roles", ""); err == nil {
		t.Fatal("after 00108 the CHECK admits an arbitrary kind")
	}

	rows, err := db.Query(`SELECT id, seq, scope_kind, scope_key, title, body, author_id,
		source_task_id, state, retire_reason, effective_ts, created_ts, updated_ts
		FROM lore_entry ORDER BY seq`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer rows.Close()
	got, err := collectLoreEntries(rows)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := []LoreEntry{
		{ID: "L-1", Seq: 1, ScopeKind: "role", ScopeKey: "r-x", Title: "title L-1", Body: "body L-1", AuthorID: "m-a", SourceTaskID: "t-1", State: "retired", RetireReason: "why", EffectiveTS: 1.5, CreatedTS: 2.5, UpdatedTS: 3.5},
		{ID: "L-2", Seq: 2, ScopeKind: "agent", ScopeKey: "m-a", Title: "title L-2", Body: "body L-2", AuthorID: "m-a", SourceTaskID: "t-1", State: "retired", RetireReason: "why", EffectiveTS: 1.5, CreatedTS: 2.5, UpdatedTS: 3.5},
		{ID: "L-3", Seq: 3, ScopeKind: "manual", ScopeKey: "tm-a", Title: "title L-3", Body: "body L-3", AuthorID: "m-a", SourceTaskID: "t-1", State: "retired", RetireReason: "why", EffectiveTS: 1.5, CreatedTS: 2.5, UpdatedTS: 3.5},
		{ID: "L-4", Seq: 4, ScopeKind: "everyone", ScopeKey: "", Title: "title L-4", Body: "body L-4", AuthorID: "m-a", SourceTaskID: "t-1", State: "retired", RetireReason: "why", EffectiveTS: 1.5, CreatedTS: 2.5, UpdatedTS: 3.5},
	}
	if len(got) != len(want) {
		t.Fatalf("rows after 00108 = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d after 00108 = %+v, want %+v", i, got[i], want[i])
		}
	}

	var indexSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'lore_entry' AND sql IS NOT NULL`).Scan(&indexSQL); err != nil {
		t.Fatalf("the selection index is gone after 00108: %v", err)
	}
	const wantIndex = "CREATE INDEX idx_lore_entry_scope_state_effective\n" +
		"    ON lore_entry (scope_kind, scope_key, state, effective_ts DESC)"
	if indexSQL != wantIndex {
		t.Fatalf("index after 00108 = %q, want %q", indexSQL, wantIndex)
	}
}

func loreMigrationState(t *testing.T, db *sql.DB) (version int64, rows int, rebuildLeft int, indexes int) {
	t.Helper()
	v, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	for q, dst := range map[string]*int{
		`SELECT COUNT(*) FROM lore_entry`:                                                                           &rows,
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'lore_entry_rebuild'`:                                      &rebuildLeft,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_lore_entry_scope_state_effective'`: &indexes,
	} {
		if err := db.QueryRow(q).Scan(dst); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return v, rows, rebuildLeft, indexes
}

func TestMigration00108DownRefusesWhileAnEveryoneRowExistsAndLeavesNothingBehind(t *testing.T) {
	db := openLoreAt107(t)
	if err := goose.UpTo(db, "migrations", 108); err != nil {
		t.Fatalf("goose up to 108: %v", err)
	}
	if err := insertLoreKind(db, "L-1", 1, "agent", "m-a"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := insertLoreKind(db, "L-2", 2, "everyone", ""); err != nil {
		t.Fatalf("seed everyone: %v", err)
	}

	if err := goose.DownTo(db, "migrations", 107); err == nil {
		t.Fatal("00108 Down succeeded with an everyone row present — that row was dropped or rewritten")
	}
	v, rows, rebuild, idx := loreMigrationState(t, db)
	if v != 108 || rows != 2 || rebuild != 0 || idx != 1 {
		t.Fatalf("after the refused Down: version=%d rows=%d rebuild tables=%d index=%d, "+
			"want 108/2/0/1 — the failed batch was not rolled back whole", v, rows, rebuild, idx)
	}

	if _, err := db.Exec(`UPDATE lore_entry SET scope_kind = 'agent', scope_key = 'm-a' WHERE id = 'L-2'`); err != nil {
		t.Fatalf("move off everyone: %v", err)
	}
	if err := goose.DownTo(db, "migrations", 107); err != nil {
		t.Fatalf("00108 Down with no everyone row: %v", err)
	}
	v, rows, rebuild, idx = loreMigrationState(t, db)
	if v != 107 || rows != 2 || rebuild != 0 || idx != 1 {
		t.Fatalf("after the Down: version=%d rows=%d rebuild tables=%d index=%d, want 107/2/0/1",
			v, rows, rebuild, idx)
	}
	if err := insertLoreKind(db, "L-3", 3, "everyone", ""); err == nil {
		t.Fatal("after the Down the CHECK still admits 'everyone'")
	}
}
