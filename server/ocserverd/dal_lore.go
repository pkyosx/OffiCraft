package main

// dal_lore.go — T-33 傳承 (lore) over `lore_entry` + `lore_entry_seq`.
//
// 🔴 There is deliberately NO method that writes `title` or `body` on an existing
// row (no UpdateLoreEntry): an entry is written once. A generic updater would
// turn the no-edit rule into a convention instead of an absence.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type LoreEntry struct {
	ID  string
	Seq int
	// ScopeKind: migrations/00100 deliberately LEFT some rows at the legacy
	// literal 'role', so values outside the Lore* constants come back out of the
	// DB. Do not narrow this to a validated enum on the read path — the orphans
	// 00100 preserved would fail to load.
	ScopeKind string
	// ScopeKey: the member's own id for 'agent' (staff and outsource alike),
	// task-manual type_key for 'manual', "" for 'everyone', a role_key for a
	// legacy 'role' orphan.
	ScopeKey string
	Title    string
	Body     string
	// AuthorID is pinned at write time, NOT re-resolved against the roster.
	AuthorID string

	SourceTaskID string
	State        string
	RetireReason string

	EffectiveTS float64

	CreatedTS float64
	UpdatedTS float64

	// LoreType is one of the five Valid ones; the DB CHECK refuses "".
	LoreType string
}

const loreEntryColumns = `id, seq, scope_kind, scope_key, title, body,
	author_id, source_task_id, state, retire_reason,
	effective_ts, created_ts, updated_ts, lore_type`

func scanLoreEntry(row interface{ Scan(...any) error }) (LoreEntry, error) {
	var e LoreEntry
	err := row.Scan(&e.ID, &e.Seq, &e.ScopeKind, &e.ScopeKey, &e.Title, &e.Body,
		&e.AuthorID, &e.SourceTaskID, &e.State, &e.RetireReason,
		&e.EffectiveTS, &e.CreatedTS, &e.UpdatedTS, &e.LoreType)
	return e, err
}

const loreIDPrefix = "L-"

// loreMintRetryLimit: under today's single-connection IMMEDIATE write pool the
// TRANSACTION carries uniqueness; the CAS loop is insurance against the mint
// being moved out of one (same caveats as mintRetryLimit, dal_task_id_seq.go).
const loreMintRetryLimit = 64

// CreateLoreEntryMintingID mints and INSERTS in ONE transaction: a mint on one
// connection and an insert on another would hand the same number out twice under
// a widened pool, and a second write of an existing id is not an error the API
// would notice.
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Seq, e.ScopeKind, e.ScopeKey, e.Title, e.Body,
		e.AuthorID, e.SourceTaskID, e.State, e.RetireReason,
		e.EffectiveTS, e.CreatedTS, e.UpdatedTS, e.LoreType); err != nil {
		return e, err
	}
	return e, tx.Commit()
}

func mintLoreNumber(tx *writeTx) (int, error) {
	for attempt := 0; attempt < loreMintRetryLimit; attempt++ {
		var next int
		if err := tx.QueryRow(
			`SELECT next FROM lore_entry_seq WHERE id = 1`).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
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

// 🔴 THE LIVE ORDER IS PRODUCED HERE, IN SQL, AND NOWHERE ELSE: selectLoreEntries
// (lore_select.go) consumes this sequence and only decides where to stop.
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

// loreListFilter: every axis is a set because the cockpit filters are
// multi-select (owner rc-0376bf875757 [1]).
//
// 🔴 EntryIDs matches EXACTLY, never as a substring (owner: 「跟 task 一樣」): a
// substring `L-1` would drag in L-10…L-19 and, on a limit/offset page, push the
// asked-for rows off the end of the batch.
type loreListFilter struct {
	ScopeKinds []string
	ScopeKeys  []string
	States     []string
	AuthorIDs  []string
	EntryIDs   []string
	LoreTypes  []string
}

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
		{"id", f.EntryIDs},
		{"lore_type", f.LoreTypes},
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

// SetLoreEntryState writes retire_reason on EVERY transition: moving back to
// active must CLEAR it (the caller passes ""), or the cockpit keeps showing a
// stale reason beside a live entry.
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

// BumpLoreEntryEffective is 「提到最新」. 🔴 created_ts is deliberately NOT in
// this statement: it is the only record of when the entry was really written.
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

func (d *DAL) SetLoreEntryScope(id, scopeKind, scopeKey string, updatedTS float64) (bool, error) {
	res, err := d.wdb.Exec(
		`UPDATE lore_entry SET scope_kind = ?, scope_key = ?, updated_ts = ?
		 WHERE id = ? AND NOT (scope_kind = ? AND scope_key = ?)`,
		scopeKind, scopeKey, updatedTS, id, scopeKind, scopeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type loreScopeFacts struct {
	TaskTypeKey string
	// AuthorOnRoster: the owner and legacy '' authors have no member row, so an
	// 'agent' scope keyed to them would ride no boot document.
	AuthorOnRoster bool
}

// LoreScopeFacts: task-type rule (owner, T-236) — an entry WITH a source task
// takes that task's type only; an entry with NO source task written by an
// outsource member falls back to the task that member is bound to. The member
// row is read whatever its roster_status: a released worker keeps its
// linked_task_id and is still the author.
func (d *DAL) LoreScopeFacts(ids []string) (map[string]loreScopeFacts, error) {
	out := make(map[string]loreScopeFacts, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	clause, args := loreInClause("l.id", ids)
	rows, err := d.rdb.Query(`
		SELECT l.id,
			CASE
				WHEN l.source_task_id != '' THEN COALESCE(TRIM(src.type_key), '')
				WHEN m.kind = ? THEN COALESCE(TRIM(bound.type_key), '')
				ELSE ''
			END,
			m.id IS NOT NULL
		FROM lore_entry l
		LEFT JOIN task src ON src.id = l.source_task_id
		LEFT JOIN member m ON m.id = l.author_id
		LEFT JOIN task bound ON bound.id = m.linked_task_id
		WHERE 1=1`+clause, append([]any{KindOutsource}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var id string
		var f loreScopeFacts
		if err := rows.Scan(&id, &f.TaskTypeKey, &f.AuthorOnRoster); err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rows.Err()
}
