package main

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestMigration00129(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "gate.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 128); err != nil {
		t.Fatalf("goose up to 128: %v", err)
	}
	for _, row := range []struct {
		id, card string
		isGate   int
	}{
		{"ts-card", "rc-1", 0},
		{"ts-plain", "", 0},
		{"ts-gate", "", 1},
	} {
		if _, err := db.Exec(`INSERT INTO task_step (id, task_id, name, status, is_gate, reply_card_id)
			VALUES (?, 'T-1', ?, 'done', ?, ?)`, row.id, row.id, row.isGate, row.card); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}
	if err := goose.UpTo(db, "migrations", 129); err != nil {
		t.Fatalf("goose up to 129: %v", err)
	}
	for id, want := range map[string]int{"ts-card": 1, "ts-plain": 0, "ts-gate": 1} {
		var got int
		if err := db.QueryRow(`SELECT is_gate FROM task_step WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if got != want {
			t.Fatalf("%s is_gate after 00129 = %d, want %d", id, got, want)
		}
	}
}
