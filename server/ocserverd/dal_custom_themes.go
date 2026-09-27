package main

// dal_custom_themes.go — the per-theme table from migration 00059.
//
// 🔴 `Bundle` is raw JSON text and is never decoded here: the migration's move is
// proved byte-for-byte, and a decode here would put a lossy round-trip under that
// guarantee. It holds only until the first write through this layer replaces a
// theme's bytes.
//
// 🔴 Retire vs double-write: T-83ef retired `display.custom_themes` FROM THE WIRE
// (settings neither serves nor accepts it) and left the row in the database as
// the rollback path. Precondition for deleting that row: refuse while settings key
// customThemeSkipRecordKey exists (Up skips elements it cannot key). Named hole: a
// skipped theme became unreachable when the row stopped being served, so the
// receipt protects the bytes, not those themes — full statement in the
// migration's header.

import (
	"database/sql"
	"errors"
	"fmt"
)

// UpdatedAt is 0 for rows created by migration 00059: stamping migrate time would
// invent an edit that never happened.
type CustomTheme struct {
	ID        string
	Bundle    string
	OrderIdx  int
	UpdatedAt float64
}

// ⚠️ Do not let this drift to `ORDER BY rowid`: the two agree in every state
// reachable today, so the swap would look correct until a position is inserted or
// an import reorders — and the owner's list silently comes back in the wrong order.
func (d *DAL) ListCustomThemes() ([]CustomTheme, error) {
	rows, err := d.rdb.Query(
		`SELECT theme_id, bundle, order_idx, updated_at FROM custom_theme ORDER BY order_idx`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomTheme
	for rows.Next() {
		var t CustomTheme
		if err := rows.Scan(&t.ID, &t.Bundle, &t.OrderIdx, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DAL) GetCustomTheme(id string) (*CustomTheme, error) { return getCustomThemeOn(d.rdb, id) }

func getCustomThemeOn(q sqlRowQuerier, id string) (*CustomTheme, error) {
	t := CustomTheme{ID: id}
	err := q.QueryRow(
		`SELECT bundle, order_idx, updated_at FROM custom_theme WHERE theme_id = ?`, id).
		Scan(&t.Bundle, &t.OrderIdx, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// 🔴 order_idx is deliberately absent from DO UPDATE SET: a new theme is appended
// (MAX + 1), an edited theme keeps its position in the owner's list.
func (d *DAL) PutCustomTheme(id, bundle string) error {
	if err := d.checkCustomThemeIDMatchesBundle(id, bundle); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`
		INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at)
		VALUES (?, ?, COALESCE((SELECT MAX(order_idx) + 1 FROM custom_theme), 0), ?)
		ON CONFLICT (theme_id) DO UPDATE SET
			bundle = excluded.bundle, updated_at = excluded.updated_at`,
		id, bundle, nowSecs())
	return err
}

// One named error per named table constraint, so the handler maps each to a 400
// naming the right field via errors.Is — never a string match on a database message.
var (
	ErrCustomThemeIDBlank       = errors.New("custom theme: the id is blank")
	ErrCustomThemeBundleNotJSON = errors.New("custom theme: the bundle is not valid JSON")
	ErrCustomThemeIDMismatch    = errors.New("custom theme: the bundle's own id does not match the id it is being filed under")
)

// checkCustomThemeIDMatchesBundle turns what the table's constraint would reject at
// INSERT (a 500) into a 400 naming the field. It asks SQLite, not Go, because the
// two disagree (measured): `{"id":"a","id":"b"}` is "b" in Go and "a" in SQLite; a
// lone surrogate becomes U+FFFD in Go only. The COMPARISON must happen in SQLite
// too — comparing the extracted value in Go let `{"id":1.0}`, `{"id":1e100}`,
// `{"id":-0.0}`, `{"id":3.0e2}` through to the constraint; the CAST to TEXT
// reproduces the column's affinity conversion.
// may not come through that DTO.)
func (d *DAL) checkCustomThemeIDMatchesBundle(id, bundle string) error {
	if id == "" {
		return ErrCustomThemeIDBlank
	}
	// json_valid first, separately: json_extract hard-errors on malformed JSON, which
	// would make "not JSON" (a 400) indistinguishable from a dead read pool.
	var valid bool
	if err := d.rdb.QueryRow(`SELECT json_valid(?)`, bundle).Scan(&valid); err != nil {
		return fmt.Errorf("custom theme: checking the bundle: %w", err)
	}
	if !valid {
		return ErrCustomThemeBundleNotJSON
	}
	var matches, missing bool
	var extracted sql.NullString
	if err := d.rdb.QueryRow(
		`SELECT CAST(json_extract(?, '$.id') AS TEXT) IS ?,
		        json_extract(?, '$.id') IS NULL,
		        json_extract(?, '$.id')`,
		bundle, id, bundle, bundle).Scan(&matches, &missing, &extracted); err != nil {
		return fmt.Errorf("custom theme: checking the bundle id: %w", err)
	}
	if missing {
		return fmt.Errorf("%w: the bundle carries no id", ErrCustomThemeIDMismatch)
	}
	if !matches {
		return fmt.Errorf("%w: bundle says %q, filed under %q", ErrCustomThemeIDMismatch, extracted.String, id)
	}
	return nil
}

// DeleteCustomTheme deliberately does not renumber survivors: gaps in order_idx are
// harmless, and renumbering is the whole-set write this table removed.
func (d *DAL) DeleteCustomTheme(id string) (bool, error) { return deleteCustomThemeOn(d.wdb, id) }

func deleteCustomThemeOn(ex sqlExecer, id string) (bool, error) {
	res, err := ex.Exec(`DELETE FROM custom_theme WHERE theme_id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (d *DAL) CountCustomThemes() (int, error) {
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM custom_theme`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
