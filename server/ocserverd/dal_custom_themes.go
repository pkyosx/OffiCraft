package main

// dal_custom_themes.go — T-83ef, the access layer for the per-theme table that
// migration 00059 introduced.
//
// The whole reason this seam exists is that the thing it replaces had no
// per-item write at all: custom themes lived as ONE json array inside ONE
// settings value, so "save this theme" was spelled "rewrite every theme,
// including every embedded image". Here the unit is a row, and that is the
// entire point — everything below is deliberately narrow.
//
// 🔴 `Bundle` IS RAW JSON TEXT, AND THAT IS A CONTRACT, NOT AN IMPLEMENTATION
// DETAIL. The migration copies each array element's own bytes so the move can be
// proved byte-for-byte before anything old is retired (see the migration's
// header). This layer keeps that property: it stores and returns the text it is
// given and never decodes it. Decoding belongs to the API layer, where the DTO
// is the wire contract; doing it here would put a lossy round-trip underneath
// the one guarantee the ticket rests on.
//
// 🔴 READ THIS BEFORE WRITING THE ENDPOINTS: WHILE `display.custom_themes` STILL
// EXISTS, IT IS THE TRUTH AND THIS TABLE IS A COPY. Migration 00059 copies once
// and nothing keeps the two in step afterwards, so a write that lands only here
// makes `GET /api/settings` and this table disagree — silently, with the
// settings face still serving the pre-upgrade answer. The change that adds the
// per-theme endpoints has to pick one: retire the legacy row in that same
// package, or write BOTH until it does. It cannot ignore the question, and the
// migration's header carries the precondition for retiring: refuse while the
// settings key customThemeSkipRecordKey exists, because Up skips elements it
// cannot key and that key is the receipt saying something was left behind. (An
// earlier draft of this sentence said "row count must match the legacy array's
// length" — that test can never pass on an install that skipped something, which
// would have locked those installs out of retirement with no way forward.)
//
// 🔴 WHICH IS WHAT T-83ef CHOSE, AND THE CHOICE HAS A NAMED HOLE. It retires the
// legacy row FROM THE WIRE — settings neither serves nor accepts custom_themes
// any more, so there is one truth on the wire — while leaving the row itself in
// the database, unwritten by anything, as the rollback path the ticket requires.
// The hole: the receipt above is read by whoever DELETES the row, but a skipped
// theme becomes unreachable at the earlier moment the row stops being SERVED.
// The precondition therefore does NOT protect those themes through this change;
// it protects the bytes, later. The migration's header carries the full
// statement, including the one measured and the one mechanical fact that keep it
// off the live-hazard list. Read it before you rely on that rule for anything.
//
// ⚠️ THE BYTE-FOR-BYTE GUARANTEE HAS A TIME WINDOW, and it closes here. It holds
// for what the migration wrote; the first write through this layer replaces that
// theme's bytes with whatever the caller marshalled. That is correct and
// intended — but it means the comparison that authorises retiring the legacy row
// has to be run BEFORE the endpoints start writing, not after.

import (
	"database/sql"
	"errors"
	"fmt"
)

// CustomTheme is one saved theme as it is stored: identity, the bundle's JSON
// text, its position in the owner's list, and when it was last written.
//
// UpdatedAt is 0 for rows created by migration 00059 — nothing knows when a
// theme that lived inside a shared settings value was last edited, and stamping
// migrate time would invent an edit that never happened.
type CustomTheme struct {
	ID        string
	Bundle    string
	OrderIdx  int
	UpdatedAt float64
}

// ListCustomThemes returns every saved theme in the owner's list order.
//
// ⚠️ ORDER COMES FROM order_idx, AND THE HONEST STATE OF THAT IS: it is a
// DELIBERATE CHOICE, not a bug fix. Measured on this tree (2026-08-17), the
// column cannot currently disagree with rowid order at all — a new row takes
// MAX(order_idx)+1 while SQLite gives it max(rowid)+1, so append,
// delete-then-re-add (middle row and highest row alike), editing through the
// upsert's conflict path, and VACUUM all leave `ORDER BY order_idx` and
// `ORDER BY rowid` identical. An earlier version of this comment claimed a
// delete-and-re-add would silently reshuffle the owner's list; an independent
// review disproved it, and the claim is gone rather than softened.
//
// The column stays because the list order is a fact the MIGRATION knows and
// writes down, while rowid order is an accident that currently agrees with it —
// and because inserting at a position, or an import that reorders, needs a
// column that means position. TestCustomThemeListOrderComesFromOrderIdxNotRowid
// is what stops this query drifting to `ORDER BY rowid`: it seeds rows whose
// stored positions deliberately contradict their insertion order, which is the
// one state the product cannot reach on its own and the only one that tells the
// two orderings apart.
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

// GetCustomTheme returns one saved theme, or nil when no theme carries that id.
// A missing theme is not an error here: "does this id exist" is a question the
// callers ask on purpose (a PUT deciding create-vs-replace, a DELETE reporting
// 404), and folding it into an error would make them parse one.
func (d *DAL) GetCustomTheme(id string) (*CustomTheme, error) {
	t := CustomTheme{ID: id}
	err := d.rdb.QueryRow(
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

// PutCustomTheme creates or replaces ONE theme — the write this whole ticket
// exists to make expressible.
//
// 🔴 order_idx IS NOT IN THE UPDATE CLAUSE, AND THAT IS THE POINT. A new theme
// is appended (MAX + 1); an existing theme KEEPS the position it already had, so
// editing a theme's colours does not move it to the bottom of the owner's list.
// The VALUES expression still computes an append position on the conflict path —
// SQLite evaluates it before it discovers the conflict — and it is discarded
// there, which is exactly the behaviour wanted and the reason the column is
// absent from DO UPDATE SET rather than being set to itself.
//
// COALESCE covers the empty table: MAX over no rows is NULL, and NULL would fail
// the NOT NULL column rather than mean "first".
//
// 🔴 IT REFUSES A MISMATCHED PAIR ITSELF RATHER THAN LETTING THE TABLE DO IT, and
// that is not belt-and-braces — it is the difference between a 400 and a 500. See
// checkCustomThemeIDMatchesBundle.
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

// The three refusals this layer can produce, mirroring the table's three named
// constraints one for one. They are NAMED errors because the handler above has
// to turn each into a 400 that says what is wrong — `errors.Is` against these,
// never a string match on a database message, whose wording nobody has promised
// to keep stable.
//
// ⚠️ THREE, NOT ONE, FOR THE SAME REASON THE TABLE HAS THREE NAMED CONSTRAINTS
// RATHER THAN ONE ANONYMOUS CHECK: a caller told "the id does not match" when
// the real problem is a blank id, or a bundle that is not JSON at all, goes and
// looks at the wrong field. An earlier version of this file answered all three
// with the mismatch error and its test froze that in place — the same defect the
// table had, one layer up.
var (
	ErrCustomThemeIDBlank       = errors.New("custom theme: the id is blank")
	ErrCustomThemeBundleNotJSON = errors.New("custom theme: the bundle is not valid JSON")
	ErrCustomThemeIDMismatch    = errors.New("custom theme: the bundle's own id does not match the id it is being filed under")
)

// checkCustomThemeIDMatchesBundle asks THE DATABASE what the bundle's id is, and
// refuses the write when that disagrees with the key.
//
// 🔴 WHY THIS EXISTS EVEN THOUGH THE TABLE ALREADY HAS A CHECK FOR IT. The
// constraint is a table-level guard: it fires at INSERT time, from inside the
// driver, as a failed write. Every write path that reaches this layer for the
// rest of the product's life passes under it — including the per-theme endpoints
// this ticket is being split to make possible. Without this function, the first
// bundle whose id Go and SQLite read differently produces a failed statement at
// runtime, which is a 500 and a log line, when what the caller deserves is a 400
// naming the field.
//
// 🔴 AND THE DISAGREEMENT IS REAL, MEASURED, NOT DEFENSIVE PROGRAMMING. Go's
// decoder and SQLite's json_extract do not agree on every input both accept:
// `{"id":"a","id":"b"}` (duplicate key) reads as "b" in Go and "a" in SQLite;
// `{"id":"a\ud800b"}` (lone surrogate) reads as a U+FFFD substitution in Go and
// as the original bytes in SQLite. A handler that derives the key from the
// decoded DTO — the obvious way to write it — hands this layer a pair the table
// will reject. Migration 00059 hit exactly this and skips such elements; the
// endpoints cannot skip, so they need an answer, and this is it.
//
// 🔴 THE COMPARISON HAPPENS IN SQLITE TOO, NOT ONLY THE EXTRACTION, and that
// distinction is the whole point rather than a detail. An earlier version pulled
// the extracted id into Go and compared strings there — which reads like "asking
// SQLite" but is not: the VALUE came from SQLite and the JUDGEMENT stayed in Go,
// so the two could still disagree. Measured, five inputs passed that check and
// were then refused by the constraint — exactly the 500 this function exists to
// prevent, e.g. `{"id":1.0}` (Go renders the extracted number "1", SQLite's
// affinity conversion renders it "1.0"), `{"id":1e100}`, `{"id":-0.0}`,
// `{"id":3.0e2}`. The CAST to TEXT reproduces the affinity conversion the column
// applies, and with it all eight probed inputs agree with what the table
// actually accepts.
//
// (None of those shapes is reachable through the wire today — ThemeBundleDTO.Id
// is a string, so a numeric id fails DTO decoding first. This is about the
// function being true to its own description, and about the next caller, who
// may not come through that DTO.)
func (d *DAL) checkCustomThemeIDMatchesBundle(id, bundle string) error {
	if id == "" {
		return ErrCustomThemeIDBlank
	}
	// json_valid FIRST, and separately: json_extract raises a hard error on
	// malformed JSON, so folding the two together would leave "the bundle is not
	// JSON" (a 400) indistinguishable from "the read pool is gone" (not a 400).
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
		// `extracted` is used ONLY to say what was found. The judgement above is
		// SQLite's; this is the sentence a human reads.
		return fmt.Errorf("%w: bundle says %q, filed under %q", ErrCustomThemeIDMismatch, extracted.String, id)
	}
	return nil
}

// DeleteCustomTheme removes one theme and reports whether a row was actually
// removed, so a handler can tell 404 from 204 without a second query.
//
// It deliberately does NOT renumber the survivors. Gaps in order_idx are
// harmless — ORDER BY reads a sparse sequence exactly as well as a dense one —
// and renumbering would rewrite every remaining row on every delete, which is
// the whole-set write this ticket removed, reintroduced through a side door.
func (d *DAL) DeleteCustomTheme(id string) (bool, error) {
	res, err := d.wdb.Exec(`DELETE FROM custom_theme WHERE theme_id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountCustomThemes answers the cap check (maxCustomThemes) without loading
// every bundle — the rows being counted are the ones carrying the embedded
// images, so reading them to measure how many there are would defeat the split.
func (d *DAL) CountCustomThemes() (int, error) {
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM custom_theme`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
