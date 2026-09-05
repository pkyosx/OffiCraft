package main

// migration_00082_lore_recall_session_anchor_test.go — the round trip, and what
// the rows that were already in the table become.
//
// 🔴 THE OLD ROWS ARE THE POINT OF THIS FILE. Every lore_recall_log row a live
// station carries today was written by the boot fold — one row per wake for the
// whole subject directory — and NONE of them carries a session anchor, because
// nothing recorded one. After 00082 they have the two new cells at their
// defaults, and the question that matters is whether a reader can tell those
// rows apart from a row whose read genuinely happened outside any session. If it
// cannot, 「這一列沒有錨」 and 「那一次沒記到」 render identically, which is the
// exact silent shape this whole round exists to kill.

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// m82Bounds derives this migration's version and the one immediately before it
// from the embedded migration set — never from a literal.
//
// 🔴 THIS FUNCTION EXISTS BECAUSE THE LITERALS ACTUALLY BIT. This file used to
// say `m82UpTo(t, db, 77)` and `goose.DownTo(..., 77)`. When the lore stages
// were renumbered 77/78/79 → 81/82/83 to land after another package's 00080,
// every one of those literals silently pointed at a stage that no longer had
// anything to do with this migration: the test migrated to 76, found no
// lore_recall_log, and failed. It failed LOUDLY, which is the good case — but
// only because it seeds a row before the UP. A literal in a test that merely
// asserts a version number would have gone on passing while measuring the wrong
// retreat. Derive; do not write the number down.
func m82Bounds(t *testing.T) (mine, prev int64) {
	t.Helper()
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var versions []int64
	mine = -1
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Fatalf("migration %q does not start with a version", name)
		}
		v, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			t.Fatalf("migration %q: %v", name, err)
		}
		versions = append(versions, v)
		if strings.Contains(name, "lore_recall_session_anchor") {
			if mine >= 0 {
				t.Fatalf("two migrations claim the recall session anchor: %d and %d", mine, v)
			}
			mine = v
		}
	}
	if mine < 0 {
		t.Fatal("no embedded migration carries the recall session anchor — this test " +
			"would otherwise be verifying nothing at all")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for _, v := range versions {
		if v < mine {
			prev = v
		}
	}
	if prev == 0 || prev == mine {
		t.Fatalf("could not derive the stage before %d from %v", mine, versions)
	}
	return mine, prev
}

func m82Goose(t *testing.T) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
}

func m82UpTo(t *testing.T, db *sql.DB, v int64) {
	t.Helper()
	m82Goose(t)
	if err := goose.UpTo(db, "migrations", v); err != nil {
		t.Fatalf("goose up to %d: %v", v, err)
	}
}

func m82Columns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(lore_recall_log)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		out[name] = true
	}
	return out
}

// TestMigration00082CarriesTheOldBootRowsForwardAsUnrecorded is the whole
// question in one test: a row written by the OLD writer (the only writer there
// was), carried across the migration, must come out saying 'unrecorded' — and
// must be distinguishable from a row written by the NEW writer for an actor with
// no session, which says 'unanchored'.
func TestMigration00082CarriesTheOldBootRowsForwardAsUnrecorded(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "m82-legacy.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// ── the station as it stands TODAY: the stage BEFORE this one, one boot-fold row ──
	mine, prev := m82Bounds(t)
	m82UpTo(t, db, prev)
	if cols := m82Columns(t, db); cols["session_boot_ts"] || cols["session_state"] {
		t.Fatalf("stage %d already has the anchor columns; this test would prove nothing", prev)
	}
	// Written through the OLD column list on purpose. A row inserted through
	// today's DAL would carry the new cells and could not be the legacy shape.
	if _, err := db.Exec(`
		INSERT INTO lore_recall_log (actor_id, query, subject_id, hop, returned, created_ts)
		VALUES ('m-old', 'boot-fold', '', 0, '{"subjects":["agent:Kyle"],"omitted":3}', 1700000000)`,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	// ── UP: prev → this migration ───────────────────────────────────────────
	m82UpTo(t, db, mine)
	cols := m82Columns(t, db)
	if !cols["session_boot_ts"] || !cols["session_state"] {
		t.Fatalf("the anchor columns did not arrive: %v", cols)
	}

	var actor, query, returned, state string
	var created, boot float64
	if err := db.QueryRow(`
		SELECT actor_id, query, returned, created_ts, session_boot_ts, session_state
		FROM lore_recall_log`,
	).Scan(&actor, &query, &returned, &created, &boot, &state); err != nil {
		t.Fatalf("read the carried-forward row: %v", err)
	}
	// Nothing the old row said may have changed. An ADD COLUMN that rewrote a
	// journal would be worse than no column at all.
	if actor != "m-old" || query != loreRecallQueryBoot || created != 1700000000 ||
		returned != `{"subjects":["agent:Kyle"],"omitted":3}` {
		t.Fatalf("the legacy row changed across the migration: %q %q %v %q",
			actor, query, created, returned)
	}
	if boot != 0 {
		t.Errorf("session_boot_ts = %v, want 0 — there was no anchor to carry", boot)
	}
	// 🔴 THE ASSERTION THE FILE EXISTS FOR.
	if state != loreRecallSessionUnrecorded {
		t.Fatalf("session_state = %q, want %q — an old row must say NOBODY LOOKED, "+
			"not that somebody looked and found no session; those two are opposite "+
			"facts and on any screen they render the same unless this cell separates "+
			"them", state, loreRecallSessionUnrecorded)
	}

	// ── and the three states really are three ────────────────────────────────
	// Written through the DAL, i.e. the new path, exactly as the handlers do.
	dal := &DAL{rdb: db, wdb: db}
	for _, r := range []LoreRecall{
		{ActorID: "m-new", Query: loreRecallQuerySearch, Returned: "{}", CreatedTS: 2,
			SessionState: loreRecallSessionUnanchored},
		{ActorID: "m-new", Query: loreRecallQuerySearch, Returned: "{}", CreatedTS: 3,
			SessionBootTS: 1700000500, SessionState: loreRecallSessionAnchored},
	} {
		if err := dal.InsertLoreRecall(r); err != nil {
			t.Fatalf("insert %s: %v", r.SessionState, err)
		}
	}
	seen := map[string]bool{}
	rows, err := db.Query(`SELECT session_state FROM lore_recall_log ORDER BY id`)
	if err != nil {
		t.Fatalf("read states: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			t.Fatalf("scan state: %v", err)
		}
		seen[st] = true
	}
	for _, want := range []string{
		loreRecallSessionUnrecorded, loreRecallSessionUnanchored, loreRecallSessionAnchored,
	} {
		if !seen[want] {
			t.Errorf("state %q is not reachable in one table — the three cannot be "+
				"told apart by a reader that only sees rows", want)
		}
	}
}

// TestMigration00082DownIsReversibleAndKeepsTheRows exercises the Down block,
// which — as 00047's round-trip test says of its own — has no execution path in
// the product at all (`ocserverd` has no `migrate down` subcommand). This test is
// its only executor.
//
// 🔴 DOWN LOSES THE ANCHORS AND NOTHING ELSE, AND THAT IS ASSERTED RATHER THAN
// PROMISED. Dropping the two columns throws away every anchor recorded while
// they existed — an older binary sees exactly the world it left behind — but the
// ROWS themselves must survive: this journal is append-only ground truth, and a
// Down that took the history with it would make retreating the code cost the
// data.
func TestMigration00082DownIsReversibleAndKeepsTheRows(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "m82-down.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	mine, prev := m82Bounds(t)
	m82UpTo(t, db, mine)

	dal := &DAL{rdb: db, wdb: db}
	if err := dal.InsertLoreRecall(LoreRecall{
		ActorID: "m-a", Query: loreRecallQueryEntryRead, Returned: `{"entries":["le-1"]}`,
		CreatedTS: 1700000900, SessionBootTS: 1700000000,
		SessionState: loreRecallSessionAnchored,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m82Goose(t)
	if err := goose.DownTo(db, "migrations", prev); err != nil {
		t.Fatalf("goose down to %d: %v", prev, err)
	}
	var version int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != prev {
		t.Fatalf("version after down = %d, want %d", version, prev)
	}
	if cols := m82Columns(t, db); cols["session_boot_ts"] || cols["session_state"] {
		t.Fatalf("down left the columns behind: %v", cols)
	}
	var actor, returned string
	var created float64
	if err := db.QueryRow(
		`SELECT actor_id, returned, created_ts FROM lore_recall_log`,
	).Scan(&actor, &returned, &created); err != nil {
		t.Fatalf("the row did not survive Down: %v", err)
	}
	if actor != "m-a" || returned != `{"entries":["le-1"]}` || created != 1700000900 {
		t.Fatalf("Down disturbed the row: %q %q %v", actor, returned, created)
	}

	// UP again: the columns come back at their defaults, and the row that lived
	// through the retreat honestly reads as 'unrecorded' — its anchor is gone
	// and the cell says so instead of implying one was observed.
	m82UpTo(t, db, mine)
	var state string
	var boot float64
	if err := db.QueryRow(
		`SELECT session_boot_ts, session_state FROM lore_recall_log`,
	).Scan(&boot, &state); err != nil {
		t.Fatalf("re-up read: %v", err)
	}
	if boot != 0 || state != loreRecallSessionUnrecorded {
		t.Fatalf("after down/up the row says %v/%q, want 0/%q", boot, state,
			loreRecallSessionUnrecorded)
	}
}
