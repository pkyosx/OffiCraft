package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestUpCustomThemeTable(t *testing.T) {
	t.Run("creates the table schema when the legacy setting is absent", func(t *testing.T) {
		d := newCustomThemeMigrationDAL(t)
		runCustomThemeMigration(t, d, upCustomThemeTable)

		rows, err := d.wdb.Query(`PRAGMA table_info(custom_theme)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info(custom_theme): %v", err)
		}
		defer rows.Close()
		type column struct {
			name         string
			typeName     string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		}
		var got []column
		for rows.Next() {
			var cid int
			var col column
			if err := rows.Scan(&cid, &col.name, &col.typeName, &col.notNull, &col.defaultValue, &col.primaryKey); err != nil {
				t.Fatalf("scan custom_theme schema: %v", err)
			}
			got = append(got, col)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read custom_theme schema: %v", err)
		}
		want := []column{
			{name: "theme_id", typeName: "TEXT", notNull: 1, primaryKey: 1},
			{name: "bundle", typeName: "TEXT", notNull: 1},
			{name: "order_idx", typeName: "INTEGER", notNull: 1},
			{name: "updated_at", typeName: "REAL", notNull: 1, defaultValue: sql.NullString{String: "0", Valid: true}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("custom_theme schema:\n got %#v\nwant %#v", got, want)
		}

		var count int
		if err := d.wdb.QueryRow(`SELECT COUNT(*) FROM custom_theme`).Scan(&count); err != nil {
			t.Fatalf("count custom_theme rows: %v", err)
		}
		if count != 0 {
			t.Fatalf("custom_theme row count = %d, want 0", count)
		}
		receipt, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		if receipt != nil {
			t.Fatalf("skip receipt = %q, want absent", *receipt)
		}
	})

	t.Run("copies each legacy element byte-for-byte and keeps the legacy setting", func(t *testing.T) {
		d := newCustomThemeMigrationDAL(t)
		legacy := `[{"id":"dusk","name":"Dusk","colors":{"bg":"#101010"}},{"id":"dawn","name":"Dawn","custom":"  keep this spacing  "}]`
		if err := d.PutSetting(legacyCustomThemesKey, legacy); err != nil {
			t.Fatalf("PutSetting(legacy themes): %v", err)
		}

		runCustomThemeMigration(t, d, upCustomThemeTable)

		rows, err := d.wdb.Query(`SELECT theme_id, bundle, order_idx, updated_at FROM custom_theme ORDER BY order_idx`)
		if err != nil {
			t.Fatalf("query migrated themes: %v", err)
		}
		defer rows.Close()
		type themeRow struct {
			id        string
			bundle    string
			orderIdx  int
			updatedAt float64
		}
		var got []themeRow
		for rows.Next() {
			var row themeRow
			if err := rows.Scan(&row.id, &row.bundle, &row.orderIdx, &row.updatedAt); err != nil {
				t.Fatalf("scan migrated theme: %v", err)
			}
			got = append(got, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read migrated themes: %v", err)
		}
		want := []themeRow{
			{id: "dusk", bundle: `{"id":"dusk","name":"Dusk","colors":{"bg":"#101010"}}`, orderIdx: 0, updatedAt: 0},
			{id: "dawn", bundle: `{"id":"dawn","name":"Dawn","custom":"  keep this spacing  "}`, orderIdx: 1, updatedAt: 0},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("migrated themes:\n got %#v\nwant %#v", got, want)
		}

		stored, err := d.GetSetting(legacyCustomThemesKey)
		if err != nil {
			t.Fatalf("GetSetting(legacy themes): %v", err)
		}
		if stored == nil || *stored != legacy {
			t.Fatalf("legacy themes = %q, want %q", valueOrNil(stored), legacy)
		}
		receipt, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		if receipt != nil {
			t.Fatalf("skip receipt = %q, want absent", *receipt)
		}
	})

	t.Run("skips unkeyable elements and records their indexes and reasons", func(t *testing.T) {
		d := newCustomThemeMigrationDAL(t)
		legacy := `[{"id":"dusk","name":"Dusk"},{"name":"No id"},{"id":"dusk","name":"Duplicate"},{"id":"dawn","name":"Dawn"}]`
		if err := d.PutSetting(legacyCustomThemesKey, legacy); err != nil {
			t.Fatalf("PutSetting(legacy themes): %v", err)
		}

		runCustomThemeMigration(t, d, upCustomThemeTable)

		var gotIDs []string
		rows, err := d.wdb.Query(`SELECT theme_id FROM custom_theme ORDER BY order_idx`)
		if err != nil {
			t.Fatalf("query migrated theme ids: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan migrated theme id: %v", err)
			}
			gotIDs = append(gotIDs, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read migrated theme ids: %v", err)
		}
		if want := []string{"dusk", "dawn"}; !reflect.DeepEqual(gotIDs, want) {
			t.Fatalf("migrated theme ids = %v, want %v", gotIDs, want)
		}

		receipt, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		want := `[{"index":1,"reason":"no usable id"},{"index":2,"reason":"id dusk already used by an earlier element"}]`
		if receipt == nil || *receipt != want {
			t.Fatalf("skip receipt = %q, want %q", valueOrNil(receipt), want)
		}
	})

	t.Run("rejects rows whose stored key and bundle do not agree", func(t *testing.T) {
		d := newCustomThemeMigrationDAL(t)
		runCustomThemeMigration(t, d, upCustomThemeTable)

		for _, tc := range []struct {
			name   string
			id     string
			bundle string
		}{
			{name: "blank key", id: "", bundle: `{"id":"dusk"}`},
			{name: "invalid JSON bundle", id: "dusk", bundle: `{not json`},
			{name: "mismatched key", id: "dusk", bundle: `{"id":"dawn"}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := d.wdb.Exec(
					`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, ?)`,
					tc.id, tc.bundle, 0, 0); err == nil {
					t.Fatalf("invalid custom_theme row was accepted: id=%q bundle=%q", tc.id, tc.bundle)
				}
			})
		}
	})
}

func TestRecordCustomThemeSkips(t *testing.T) {
	t.Run("stores a non-empty receipt as the JSON skip list", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting(customThemeSkipRecordKey, `[{"index":9,"reason":"old"}]`); err != nil {
			t.Fatalf("seed old skip receipt: %v", err)
		}

		skipped := []customThemeSkip{
			{Index: 1, Reason: "no usable id"},
			{Index: 4, Reason: "id dusk already used by an earlier element"},
		}
		runCustomThemeMigration(t, d, func(ctx context.Context, tx *sql.Tx) error {
			return recordCustomThemeSkips(ctx, tx, skipped)
		})

		got, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		want := `[{"index":1,"reason":"no usable id"},{"index":4,"reason":"id dusk already used by an earlier element"}]`
		if got == nil || *got != want {
			t.Fatalf("skip receipt = %q, want %q", valueOrNil(got), want)
		}
	})

	t.Run("removes a stale receipt when the current run has no skips", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting(customThemeSkipRecordKey, `[{"index":1,"reason":"old"}]`); err != nil {
			t.Fatalf("seed old skip receipt: %v", err)
		}

		runCustomThemeMigration(t, d, func(ctx context.Context, tx *sql.Tx) error {
			return recordCustomThemeSkips(ctx, tx, nil)
		})

		got, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		if got != nil {
			t.Fatalf("skip receipt = %q, want absent", *got)
		}
	})
}

func TestAnnounceCustomThemeSkip(t *testing.T) {
	t.Run("writes the legacy location, index, reason, and outcome to stderr", func(t *testing.T) {
		got := captureMigrationStderr(t, func() {
			announceCustomThemeSkip(3, "no usable id")
		})
		want := "[migration 00059] SKIPPED display.custom_themes[3]: no usable id — it stays in the legacy row\n"
		if got != want {
			t.Fatalf("stderr = %q, want %q", got, want)
		}
	})
}

func TestDownCustomThemeTable(t *testing.T) {
	t.Run("drops the copied table, removes its receipt, and leaves the legacy value", func(t *testing.T) {
		d := newAPITestDAL(t)
		legacy := `[{"id":"dusk","name":"Dusk"}]`
		if err := d.PutSetting(legacyCustomThemesKey, legacy); err != nil {
			t.Fatalf("PutSetting(legacy themes): %v", err)
		}
		if err := d.PutSetting(customThemeSkipRecordKey, `[{"index":0,"reason":"no usable id"}]`); err != nil {
			t.Fatalf("PutSetting(skip receipt): %v", err)
		}
		if _, err := d.wdb.Exec(
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, ?)`,
			"dusk", `{"id":"dusk","name":"Dusk"}`, 0, 0); err != nil {
			t.Fatalf("seed custom_theme row: %v", err)
		}

		runCustomThemeMigration(t, d, downCustomThemeTable)

		var tableCount int
		if err := d.wdb.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'custom_theme'`,
		).Scan(&tableCount); err != nil {
			t.Fatalf("check custom_theme table: %v", err)
		}
		if tableCount != 0 {
			t.Fatalf("custom_theme table count = %d, want 0", tableCount)
		}
		receipt, err := d.GetSetting(customThemeSkipRecordKey)
		if err != nil {
			t.Fatalf("GetSetting(skip receipt): %v", err)
		}
		if receipt != nil {
			t.Fatalf("skip receipt = %q, want absent", *receipt)
		}
		stored, err := d.GetSetting(legacyCustomThemesKey)
		if err != nil {
			t.Fatalf("GetSetting(legacy themes): %v", err)
		}
		if stored == nil || *stored != legacy {
			t.Fatalf("legacy themes = %q, want %q", valueOrNil(stored), legacy)
		}
	})
}

func newCustomThemeMigrationDAL(t *testing.T) *DAL {
	t.Helper()
	d := newAPITestDAL(t)
	if _, err := d.wdb.Exec(`DROP TABLE custom_theme`); err != nil {
		t.Fatalf("drop migrated custom_theme table: %v", err)
	}
	if _, err := d.wdb.Exec(`DELETE FROM setting WHERE key IN (?, ?)`, legacyCustomThemesKey, customThemeSkipRecordKey); err != nil {
		t.Fatalf("clear migration settings: %v", err)
	}
	return d
}

func runCustomThemeMigration(t *testing.T, d *DAL, migration func(context.Context, *sql.Tx) error) {
	t.Helper()
	tx, err := d.wdb.Begin()
	if err != nil {
		t.Fatalf("begin migration transaction: %v", err)
	}
	if err := migration(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migration: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration transaction: %v", err)
	}
}

func captureMigrationStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = previous }()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(data)
}

func valueOrNil(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}
