package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func openAt127(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "language.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 127); err != nil {
		t.Fatalf("goose up to 127: %v", err)
	}
	return db
}

func TestMigration00128(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before map[string]string
		want   string
	}{
		{"a claimed station that never chose a language keeps Chinese",
			map[string]string{"auth.password_hash": "hash"}, "zh"},
		{"a claimed station whose language was cleared keeps Chinese",
			map[string]string{"auth.password_hash": "hash", "display.language": ""}, "zh"},
		{"a claimed station on English stays on English",
			map[string]string{"auth.password_hash": "hash", "display.language": "en"}, "en"},
		{"a fresh install stays unset", nil, "<absent>"},
		{"an unclaimed station stays unset",
			map[string]string{"auth.password_hash": ""}, "<absent>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openAt127(t)
			for k, v := range tc.before {
				putSetting00120(t, db, k, v)
			}
			if err := goose.UpTo(db, "migrations", 128); err != nil {
				t.Fatalf("goose up to 128: %v", err)
			}
			if got := setting00120(t, db, "display.language"); got != tc.want {
				t.Fatalf("display.language after 00128 = %q, want %q", got, tc.want)
			}
		})
	}
}
