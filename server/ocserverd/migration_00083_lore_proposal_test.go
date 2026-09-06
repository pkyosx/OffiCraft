package main

// migration_00083_lore_proposal_test.go — the round trip, and the two things a
// migration test most easily stops proving.
//
// 🔴 NOT ONE VERSION NUMBER IS WRITTEN DOWN IN THIS FILE, AND THAT IS THE POINT.
// These three migrations were RENUMBERED on 2026-09-04 by O-197: 00066 / 00067 /
// 00069 became 00081 / 00082 / 00083, relative order unchanged. The old numbers
// had been taken or overtaken on main — main carries a DIFFERENT
// 00069_account_spend plus 00070 — and goose will not start on either shape: a
// duplicate version panics, and a version BELOW the database's current one is a
// 「missing migration」 that plain `goose.Up` (no allow-missing) returns an error
// for. The new numbers came from scanning EVERY remote branch through both
// sources a migration version can come from — `server/ocserverd/migrations/*.sql`
// and `AddNamedMigrationContext("NNNNN` — where the highest number in use was
// 00076.
//
// 🔴 THAT SCAN IS GOOD FOR HOURS, NOT DAYS. Unpushed and just-pushed branches are
// invisible to it, so 00076 is a FLOOR, not a fact: main cannot see the numbers
// still in flight. RESCAN BOTH SOURCES ACROSS ALL REMOTE BRANCHES IMMEDIATELY
// BEFORE THIS LANDS, and renumber again if anything at or above 00081 has since
// appeared. And the failure renumbering causes is not the obvious one —
//
//   改號時最容易漏的不是新號，是舊號. Change 「up to 79」 and forget 「down to
//   78」 and the test still passes; it has just stopped verifying 「退掉我這一支」
//   and started verifying 「退掉好幾支」. The green light is still there and the
//   proof is gone.
//
// — so both numbers are DERIVED from the embedded migration set instead:
// `mine` is the version of the file that carries this table, `prev` is the
// version immediately before it IN THIS TREE. Renumbering the file renumbers
// both, together, with nothing to forget.
//
// ⚠️ AND `prev` IS NOT `mine-1` — it merely happens to be, right now. This
// renumbering landed the three on 77, 78, 79, which are contiguous, but that is
// an accident of which numbers were free and not a property this file may lean
// on; the next renumbering can reopen a gap the way 68 (t-48 only, invisible
// from here) once did. So `prev` is READ OUT OF THE TREE, never computed.
// Anything that assumed 「前一號」 would try to descend to a version that may not
// exist. That is the same shape this file is guarding against: 號碼看起來連續 ≠
// 它真的是前一階.
//
// 🔴 AND A VERSION NUMBER ALONE IS NOT EVIDENCE. `MAX(version_id) == prev` is
// something goose sets; it would read the same for a Down that dropped half the
// schema. So the assertions that carry the weight are about NAMES: this
// migration's own table is gone, and the previous stage's own column is still
// there.

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

// m83Bounds derives this migration's version and the one immediately before it
// from the embedded migration set — never from a literal.
func m83Bounds(t *testing.T) (mine, prev int64) {
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
		if strings.Contains(name, "lore_proposal") {
			if mine >= 0 {
				t.Fatalf("two migrations claim the lore_proposal table: %d and %d", mine, v)
			}
			mine = v
		}
	}
	if mine < 0 {
		t.Fatal("no embedded migration carries the lore_proposal table — this test " +
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

func m83Goose(t *testing.T) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
}

func m83UpTo(t *testing.T, db *sql.DB, v int64) {
	t.Helper()
	m83Goose(t)
	if err := goose.UpTo(db, "migrations", v); err != nil {
		t.Fatalf("goose up to %d: %v", v, err)
	}
}

func m83HasTable(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&n); err != nil {
		t.Fatalf("sqlite_master %s: %v", name, err)
	}
	return n > 0
}

func m83HasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func m83Version(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("version: %v", err)
	}
	return v
}

// TestMigration00083DownRetreatsExactlyOneStage is the renumbering guard.
//
// 🔴 THE THREE ASSERTIONS AFTER THE DOWN HAVE TO BE READ TOGETHER, because no one
// of them is worth much alone:
//
//	the version equals the DERIVED previous stage   — goose set it, weak on its own
//	lore_proposal is GONE                           — my stage really came off
//	lore_recall_log.session_state is STILL THERE     — and nothing else did
//
// The third is the one a forgotten old number breaks: descending two stages
// instead of one would take 00082's columns with it, and the first assertion
// would happily agree with whatever number it landed on if that number were
// written down instead of derived.
func TestMigration00083DownRetreatsExactlyOneStage(t *testing.T) {
	mine, prev := m83Bounds(t)
	if mine <= prev {
		t.Fatalf("derived a previous stage %d that is not before %d", prev, mine)
	}
	db, err := openSQLite(filepath.Join(t.TempDir(), "m83-down.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// ── the station as it stands at the stage BEFORE this one ────────────────
	m83UpTo(t, db, prev)
	if m83HasTable(t, db, "lore_proposal") {
		t.Fatalf("stage %d already has lore_proposal; this test would prove nothing", prev)
	}
	// The previous stage's OWN artefact, named rather than numbered. If a
	// renumbering ever makes something else the previous stage, this line fails
	// loudly instead of quietly measuring a different retreat.
	if !m83HasColumn(t, db, "lore_recall_log", "session_state") {
		t.Fatalf("stage %d is not the lore recall-anchor stage — the retreat this "+
			"test measures is no longer the one it describes", prev)
	}

	dal := &DAL{rdb: db, wdb: db}
	if _, err := db.Exec(
		`INSERT INTO entity (id, type, canonical) VALUES ('e-repo', 'repo', 'repo:officraft')`,
	); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	written, err := dal.CreateLoreEntry(t33Write(), 1000)
	if err != nil {
		t.Fatalf("seed entry at stage %d: %v", prev, err)
	}

	// ── UP: the table arrives, and an entry that predates it can be proposed
	// against. That is the old-data question for this change: lore_proposal is
	// new and empty, but the rows it POINTS AT are older than it is.
	m83UpTo(t, db, mine)
	if !m83HasTable(t, db, "lore_proposal") {
		t.Fatal("the lore_proposal table did not arrive")
	}
	p := t33Propose(written.EntryID)
	p.BaseSHA256 = written.SHA256
	filed, err := dal.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("propose against an entry written BEFORE this migration: %v", err)
	}
	list, err := dal.ListLoreProposals(written.EntryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Proposals) != 1 || list.Proposals[0].Stale {
		t.Fatalf("a pre-migration entry reads wrong under the new path: %+v", list)
	}

	// ── DOWN: exactly one stage ──────────────────────────────────────────────
	m83Goose(t)
	if err := goose.DownTo(db, "migrations", prev); err != nil {
		t.Fatalf("goose down to %d: %v", prev, err)
	}
	if got := m83Version(t, db); got != prev {
		t.Fatalf("version after down = %d, want the derived previous stage %d", got, prev)
	}
	if m83HasTable(t, db, "lore_proposal") {
		t.Fatalf("down left lore_proposal behind — the retreat did not undo this stage")
	}
	if !m83HasColumn(t, db, "lore_recall_log", "session_state") {
		t.Fatalf("down took the PREVIOUS stage's columns with it: it retreated further " +
			"than one stage, which is exactly what a forgotten old number looks like")
	}
	// And the rows this stage never owned are untouched: retreating the code
	// must not cost the lore.
	entry, err := dal.GetLoreEntry(written.EntryID)
	if err != nil || entry == nil {
		t.Fatalf("the entry did not survive Down: %v %v", entry, err)
	}
	rev, err := dal.LatestLoreRevision(written.EntryID)
	if err != nil || rev == nil || rev.SHA256 != written.SHA256 {
		t.Fatalf("the L0 original did not survive Down: %+v %v", rev, err)
	}

	// ── UP again: the table comes back EMPTY, and that loss is the stated cost
	// of the Down rather than a surprise. Asserting it is what keeps 「有損」 an
	// observed fact instead of a sentence in a comment.
	m83UpTo(t, db, mine)
	after, err := dal.ListLoreProposals(written.EntryID)
	if err != nil {
		t.Fatalf("list after re-up: %v", err)
	}
	if len(after.Proposals) != 0 {
		t.Fatalf("a proposal survived a retreat past the table that holds it: %+v", after)
	}
	if filed.ProposalID == "" {
		t.Fatal("nothing was ever filed, so the loss above is not a loss")
	}
}
