package main

// dal_lore.go — T-33 傳承（lore）: the durable access layer over
// migrations/00093 (`lore_entry` + `lore_entry_seq`).
//
// The surface is deliberately SMALLER than CRUD, and the missing letter is the
// U. There is no UpdateLoreEntry and no method anywhere in this file that can
// write `title` or `body` on a row that already exists: an entry is written once
// and is thereafter a fixed piece of text. What moves is `state`,
// `retire_reason` and `effective_ts`, and each of those has its own narrow
// method saying so in its name. A generic updater would make the no-edit rule a
// convention that every future call site has to remember, instead of an absence
// that will not compile.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// LoreEntry mirrors one lore_entry row. Field-for-column; nothing derived.
type LoreEntry struct {
	ID  string // "L-" + Seq
	Seq int
	// ⚠️ TWO KINDS ARE WRITABLE; A THIRD IS STILL READABLE. The Go vocabulary is
	// LoreScopeAgent | LoreScopeManual — owner collapsed the old trio on
	// 2026-09-07 (card rc-a43100fd0486 [0]) and there is no longer a
	// LoreScopeRole constant to name. But this field is a plain string scanned
	// straight off the row, and migrations/00100 deliberately LEFT some rows at
	// the literal 'role' (the ones whose member could not be determined), so a
	// value outside the two constants can and does come back out of the DB. Do
	// not "narrow" this to a validated enum on the read path: that would turn
	// the orphans 00100 chose to preserve into rows that fail to load.
	ScopeKind string // LoreScopeAgent | LoreScopeManual (+ legacy 'role' orphans)
	// The MEMBER's own id for 'agent' — every member-scoped entry, staff and
	// outsource alike, since the collapse. Task-manual type_key for 'manual'.
	// A legacy 'role' orphan still carries a role_key here.
	ScopeKey string
	Title    string
	Body     string
	// AuthorID is the writer's member id as it was at the moment of the write,
	// pinned. It is NOT re-resolved against the roster on read: a writer who has
	// since left did still write this.
	AuthorID string
	// SourceTaskID is the task the write happened inside, or "". Provenance
	// only — no code branches on it.
	SourceTaskID string
	State        string // LoreStateActive | LoreStatePinned | LoreStateRetired
	RetireReason string
	// EffectiveTS is the ordering key the selector reads, and the ONLY timestamp
	// 「提到最新」 moves.
	EffectiveTS float64
	// CreatedTS is when the entry was written and never changes — which is what
	// makes a bump of EffectiveTS reversible rather than destructive.
	CreatedTS float64
	UpdatedTS float64
}

// loreEntryColumns is the one column list every read in this file selects, in
// the order scanLoreEntry expects. One list and one scanner so a column added
// later cannot be picked up by three of four reads.
const loreEntryColumns = `id, seq, scope_kind, scope_key, title, body,
	author_id, source_task_id, state, retire_reason,
	effective_ts, created_ts, updated_ts`

func scanLoreEntry(row interface{ Scan(...any) error }) (LoreEntry, error) {
	var e LoreEntry
	err := row.Scan(&e.ID, &e.Seq, &e.ScopeKind, &e.ScopeKey, &e.Title, &e.Body,
		&e.AuthorID, &e.SourceTaskID, &e.State, &e.RetireReason,
		&e.EffectiveTS, &e.CreatedTS, &e.UpdatedTS)
	return e, err
}

// loreIDPrefix is the ONE place the display id's shape is written down.
const loreIDPrefix = "L-"

// loreMintRetryLimit bounds the compare-and-set loop in mintLoreNumber, for the
// same reasons and with the same caveats as mintRetryLimit in
// dal_task_id_seq.go. Read that comment before concluding this loop is what
// makes the mint safe: under today's single-connection IMMEDIATE write pool the
// TRANSACTION is what carries uniqueness, and the CAS is insurance against the
// mint being moved out of one.
const loreMintRetryLimit = 64

// CreateLoreEntryMintingID mints the next display number and INSERTS the entry
// under it, both in ONE transaction — the shape CreateTaskMintingID established
// and for the identical reason: a mint on one connection followed by an insert
// on another hands the same number out twice under a widened pool, and the
// second write of an existing id is not an error the API would notice.
//
// The caller supplies everything but ID and Seq. It returns the stored entry.
func (d *DAL) CreateLoreEntryMintingID(e LoreEntry) (LoreEntry, error) {
	tx, err := d.wdb.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	n, err := mintLoreNumber(tx)
	if err != nil {
		return e, err
	}
	e.Seq = n
	e.ID = loreIDPrefix + strconv.Itoa(n)

	if _, err := tx.Exec(
		`INSERT INTO lore_entry (`+loreEntryColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Seq, e.ScopeKind, e.ScopeKey, e.Title, e.Body,
		e.AuthorID, e.SourceTaskID, e.State, e.RetireReason,
		e.EffectiveTS, e.CreatedTS, e.UpdatedTS); err != nil {
		return e, err
	}
	return e, tx.Commit()
}

// mintLoreNumber claims the next number on an OPEN transaction. It takes the
// tx rather than opening one so nothing can call it without a critical section
// in hand.
func mintLoreNumber(tx *sql.Tx) (int, error) {
	for attempt := 0; attempt < loreMintRetryLimit; attempt++ {
		var next int
		if err := tx.QueryRow(
			`SELECT next FROM lore_entry_seq WHERE id = 1`).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// The single row is a schema invariant (00093 seeds it, the CHECK
				// forbids a second). Absent ⇒ this file is not the schema we think
				// it is; say so rather than inventing a counter.
				return 0, errors.New("lore_entry_seq row is missing — database not migrated")
			}
			return 0, err
		}
		res, err := tx.Exec(
			`UPDATE lore_entry_seq SET next = next + 1 WHERE id = 1 AND next = ?`, next)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n == 1 {
			return next, nil
		}
	}
	return 0, fmt.Errorf(
		"could not claim a lore number in %d attempts — every compare-and-set on "+
			"lore_entry_seq reported 0 rows", loreMintRetryLimit)
}

// GetLoreEntry reads one entry by id. nil, nil = no such entry (the caller maps
// that to 404; it is not an error).
func (d *DAL) GetLoreEntry(id string) (*LoreEntry, error) {
	e, err := scanLoreEntry(d.rdb.QueryRow(
		`SELECT `+loreEntryColumns+` FROM lore_entry WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListLoreEntriesLive returns the entries of ONE scope that are NOT retired,
// already in the selection order: pinned first, then everything else, and
// newest EFFECTIVE first inside each group, with seq descending as the
// tie-break so two entries stamped in the same millisecond still have a total
// order.
//
// 🔴 THE ORDER IS PRODUCED HERE, IN SQL, AND NOWHERE ELSE. selectLoreEntries
// (lore_select.go) consumes this sequence and only decides where to stop; it
// does not re-sort. Two sorts would be two places to change when the rule
// changes, and the one that got missed would still look plausible.
//
// `pinned` is put first by ordering on a computed 0/1 rather than by running two
// queries and concatenating: a second query is a second chance for the WHERE
// clauses to drift apart, and this way the retired filter is written once.
func (d *DAL) ListLoreEntriesLive(scopeKind, scopeKey string) ([]LoreEntry, error) {
	rows, err := d.rdb.Query(
		`SELECT `+loreEntryColumns+` FROM lore_entry
		 WHERE scope_kind = ? AND scope_key = ? AND state != ?
		 ORDER BY (state = ?) DESC, effective_ts DESC, seq DESC`,
		scopeKind, scopeKey, LoreStateRetired, LoreStatePinned)
	if err != nil {
		return nil, err
	}
	return collectLoreEntries(rows)
}

// loreListFilter is the server-side filter behind the cockpit's list page.
//
// 🔴 EVERY AXIS IS A SET, because the cockpit's three filters are multi-select
// (owner rc-0376bf875757 [1]). An EMPTY set means 「do not narrow on this axis」
// — it is NOT 「match nothing」, and it never reaches SQL: an empty set skips its
// clause entirely rather than emitting `IN ()`, which is a syntax error in
// SQLite and would be the wrong meaning even if it parsed.
//
// The wire still carries a singular spelling of each axis beside the plural one;
// folding the two into ONE set is the handler's job (loreFilterValues in
// api_lore.go), so nothing below has to know that two spellings exist.
//
// 🔴 The filter travels WITH the paging, never after it. Filtering a page that
// was already cut client-side makes 「捲到底沒有了」 and 「真的沒有了」 the same
// picture, and makes any count computed off the visible rows wrong.
type loreListFilter struct {
	ScopeKinds []string
	ScopeKeys  []string
	States     []string
	AuthorIDs  []string
}

// loreInClause renders one axis as ` AND <column> IN (?,?,…)` plus its args, or
// ("", nil) for an empty set.
//
// 🔴 THE EMPTY CASE IS THE WHOLE REASON THIS IS A FUNCTION. `IN ()` does not
// parse, so an empty set cannot be expressed as a clause at all — it has to be
// the ABSENCE of one. Written inline at four call sites, that is four chances
// for one of them to build the placeholder list before checking the length.
func loreInClause(column string, vals []string) (string, []any) {
	if len(vals) == 0 {
		return "", nil
	}
	args := make([]any, 0, len(vals))
	for _, v := range vals {
		args = append(args, v)
	}
	return " AND " + column + " IN (" +
		strings.TrimPrefix(strings.Repeat(",?", len(vals)), ",") + ")", args
}

// ListLoreEntriesPage serves the cockpit list: the filter applied in SQL, the
// fixed three-group order (pinned → active → retired, newest-effective first
// inside each), and a limit/offset window over THAT ordering.
//
// The group order is a CASE and not the ORDER BY the boot fold uses, because the
// two faces answer different questions: the fold shows only what is live and
// needs pinned-before-the-rest, while the page shows everything and needs the
// retired collected at the bottom rather than interleaved by timestamp.
func (d *DAL) ListLoreEntriesPage(f loreListFilter, limit, offset int) ([]LoreEntry, error) {
	query := `SELECT ` + loreEntryColumns + ` FROM lore_entry WHERE 1=1`
	var args []any
	for _, axis := range []struct {
		column string
		vals   []string
	}{
		{"scope_kind", f.ScopeKinds},
		{"scope_key", f.ScopeKeys},
		{"state", f.States},
		{"author_id", f.AuthorIDs},
	} {
		clause, clauseArgs := loreInClause(axis.column, axis.vals)
		query += clause
		args = append(args, clauseArgs...)
	}
	query += ` ORDER BY CASE state WHEN ? THEN 0 WHEN ? THEN 1 ELSE 2 END,
		effective_ts DESC, seq DESC LIMIT ? OFFSET ?`
	args = append(args, LoreStatePinned, LoreStateActive, limit, offset)

	rows, err := d.rdb.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return collectLoreEntries(rows)
}

func collectLoreEntries(rows *sql.Rows) ([]LoreEntry, error) {
	defer rows.Close() //nolint:errcheck
	out := []LoreEntry{}
	for rows.Next() {
		e, err := scanLoreEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetLoreEntryState moves one entry between active / pinned / retired and
// stamps updated_ts. It reports whether a row was there to move (false ⇒ the
// caller answers 404, never a silent 200).
//
// retire_reason is written on EVERY transition, not only into retired, and that
// is deliberate: moving an entry back to active has to CLEAR the reason it was
// retired for, or the cockpit would keep rendering a stale explanation beside a
// live entry. The caller passes "" for the non-retired states.
//
// 🔴 It cannot touch title or body. There is no edit path; see the file header.
func (d *DAL) SetLoreEntryState(id, state, retireReason string, updatedTS float64) (bool, error) {
	res, err := d.wdb.Exec(
		`UPDATE lore_entry SET state = ?, retire_reason = ?, updated_ts = ?
		 WHERE id = ?`, state, retireReason, updatedTS, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// BumpLoreEntryEffective is 「提到最新」: effective_ts ← now.
//
// 🔴 created_ts IS NOT IN THIS STATEMENT, and that is the whole design. The
// entry keeps the record of when it was really written, so a bump can be
// explained, undone, or simply understood later. A version of this that wrote
// both columns would look identical on the cockpit and would quietly destroy
// the only copy of the original date.
func (d *DAL) BumpLoreEntryEffective(id string, ts float64) (bool, error) {
	res, err := d.wdb.Exec(
		`UPDATE lore_entry SET effective_ts = ?, updated_ts = ? WHERE id = ?`,
		ts, ts, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
