package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

const deskDefaultSchedule00120 = `[{"start":"06:00","theme":"modern-office-desk-day"},{"start":"16:30","theme":"modern-office-desk-dusk"},{"start":"18:30","theme":"modern-office-desk-night"}]`

func openAt119(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "desk-theme.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 119); err != nil {
		t.Fatalf("goose up to 119: %v", err)
	}
	return db
}

func putSetting00120(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 0)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

// setting00120 answers the stored value, or "<absent>".
func setting00120(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	var v string
	err := db.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "<absent>"
	}
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return v
}

func putCustomTheme00120(t *testing.T, db *sql.DB, id string, order int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO custom_theme (theme_id, bundle, order_idx) VALUES (?, ?, ?)`,
		id, `{"id":"`+id+`","name":"copy","colors":{}}`, order); err != nil {
		t.Fatalf("seed theme %s: %v", id, err)
	}
}

func customThemeIDs00120(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT theme_id FROM custom_theme ORDER BY order_idx`)
	if err != nil {
		t.Fatalf("read custom_theme: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestMigration00120(t *testing.T) {
	type want struct{ theme, schedule, seasonal string }
	moved := want{"modern-office-desk-day", deskDefaultSchedule00120, "true"}

	for _, tc := range []struct {
		name   string
		before map[string]string
		want   want
	}{
		{"a fresh install gets the desk schedule with the seasonal theme on", nil, moved},
		{"a station that never chose a theme moves to the default",
			map[string]string{"display.theme": ""}, moved},
		{"a station on office moves to the default, seasonal back on",
			map[string]string{"display.theme": "office", "display.seasonal_theme": "false"}, moved},
		{"a station on office-light replaces the schedule it had",
			map[string]string{"display.theme": "office-light", "display.theme_schedule": `[{"start":"08:00","theme":"office-light"},{"start":"20:00","theme":"office"}]`}, moved},
		{"a station on a custom theme keeps it, its schedule and its seasonal choice",
			map[string]string{"display.theme": "midnight", "display.seasonal_theme": "false"},
			want{"midnight", "<absent>", "false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openAt119(t)
			for k, v := range tc.before {
				putSetting00120(t, db, k, v)
			}
			if err := goose.UpTo(db, "migrations", 120); err != nil {
				t.Fatalf("goose up to 120: %v", err)
			}
			got := want{
				setting00120(t, db, "display.theme"),
				setting00120(t, db, "display.theme_schedule"),
				setting00120(t, db, "display.seasonal_theme"),
			}
			if got != tc.want {
				t.Fatalf("after 00120 = %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("the schedule it writes loads as a valid server schedule", func(t *testing.T) {
		list, err := decodeThemeSchedule(deskDefaultSchedule00120)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if refusal, err := themeScheduleRefusalOn(openAt119(t), list); err != nil || refusal != "" {
			t.Fatalf("refusal = %q / %v, want none", refusal, err)
		}
	})

	t.Run("custom copies under the desk ids go and every other custom theme stays", func(t *testing.T) {
		db := openAt119(t)
		putCustomTheme00120(t, db, "midnight", 0)
		putCustomTheme00120(t, db, "modern-office-desk-day", 1)
		putCustomTheme00120(t, db, "modern-office-desk-dusk", 2)
		putCustomTheme00120(t, db, "modern-office-desk-night", 3)
		putSetting00120(t, db, "display.theme", "modern-office-desk-night")
		if err := goose.UpTo(db, "migrations", 120); err != nil {
			t.Fatalf("goose up to 120: %v", err)
		}
		if ids := customThemeIDs00120(t, db); len(ids) != 1 || ids[0] != "midnight" {
			t.Fatalf("custom themes after 00120 = %v, want [midnight]", ids)
		}
		if v := setting00120(t, db, "display.theme"); v != "modern-office-desk-night" {
			t.Fatalf("display.theme = %q, want the same id, now the built-in", v)
		}
	})

	t.Run("runs once: office chosen after the upgrade stays office", func(t *testing.T) {
		db := openAt119(t)
		if err := goose.UpTo(db, "migrations", 120); err != nil {
			t.Fatalf("goose up to 120: %v", err)
		}
		putSetting00120(t, db, "display.theme", "office")
		putSetting00120(t, db, "display.theme_schedule", "[]")
		if err := goose.Up(db, "migrations"); err != nil {
			t.Fatalf("goose up: %v", err)
		}
		if got := setting00120(t, db, "display.theme") + " " + setting00120(t, db, "display.theme_schedule"); got != "office []" {
			t.Fatalf("after a second goose up = %q, want \"office []\"", got)
		}
	})

	t.Run("down points everything naming a desk id back at office", func(t *testing.T) {
		db := openAt119(t)
		if err := goose.UpTo(db, "migrations", 120); err != nil {
			t.Fatalf("goose up to 120: %v", err)
		}
		if err := goose.DownTo(db, "migrations", 119); err != nil {
			t.Fatalf("goose down to 119: %v", err)
		}
		if got := setting00120(t, db, "display.theme") + " " + setting00120(t, db, "display.theme_schedule"); got != "office []" {
			t.Fatalf("after the Down = %q, want \"office []\"", got)
		}
	})
}
