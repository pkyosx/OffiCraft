package main

// T-33 — migration 00100_lore_role_scope_to_member.
//
// 🔴 WHAT THESE TESTS ARE FOR, AND IT IS NOT "the migration ran". The migration
// makes a decision per role — move, or deliberately do not move — and BOTH
// outcomes are silent successes at the SQL level. A version that moved
// everything onto the first member it found would pass any test that only
// checked "no rows are left at 'role'", and a version that moved nothing would
// pass any test that only checked "nothing was deleted". So every test below
// pins a row's DESTINATION, not merely its absence from where it started.
//
// The three cases are the three the rule names:
//   · exactly one active member under the role  ⇒ moved onto that member.
//   · none                                      ⇒ left, untouched, at 'role'.
//   · two or more                               ⇒ left, untouched, at 'role'.
// plus the one that makes the middle case real rather than theoretical: a
// dismissed member still HAS a row (dismissal is a soft delete), so "the role
// still has a member row" and "the role still has a member" are different
// questions and the migration must ask the second.

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// t33UpTo93 migrates to exactly 93 — lore_entry exists, 00100 has not run — so
// a fixture can be seeded into the world 00100 will actually meet. It never
// migrates to head: asserting against head would make this file fail on the
// next unrelated migration.
func t33UpTo93(t *testing.T, db *sql.DB) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 93); err != nil {
		t.Fatalf("goose up to 93: %v", err)
	}
}

func t33UpTo100(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.UpTo(db, "migrations", 100); err != nil {
		t.Fatalf("goose up to 100: %v", err)
	}
}

// t33SeedMember writes one roster row. rosterStatus is spelled out at every
// call site rather than defaulted: whether a member counts is the entire
// question this migration turns on, so a fixture that left it implicit would
// hide the axis under test.
func t33SeedMember(t *testing.T, db *sql.DB, id, roleKey, rosterStatus string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO member (id, name, kind, role_key, roster_status)
		 VALUES (?, ?, 'staff', ?, ?)`,
		id, "Name of "+id, roleKey, rosterStatus); err != nil {
		t.Fatalf("seed member %s: %v", id, err)
	}
}

// t33SeedLore writes one lore_entry row directly, so a fixture can carry the
// retired 'role' scope_kind that no Go constant names any more.
func t33SeedLore(t *testing.T, db *sql.DB, id string, seq int, kind, key, title string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO lore_entry
		   (id, seq, scope_kind, scope_key, title, body, author_id,
		    source_task_id, state, retire_reason, effective_ts, created_ts, updated_ts)
		 VALUES (?, ?, ?, ?, ?, 'body', 'author', '', 'active', '', 100, 100, 100)`,
		id, seq, kind, key, title); err != nil {
		t.Fatalf("seed lore %s: %v", id, err)
	}
}

// t33ScopeOf reads back one entry's (kind, key) pair. Both halves together,
// always: a check on the kind alone cannot see an implementation that changed
// 'role' to 'agent' and left the role_key sitting in scope_key, which is the
// single most likely way to get this migration wrong.
func t33ScopeOf(t *testing.T, db *sql.DB, id string) (kind, key string) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT scope_kind, scope_key FROM lore_entry WHERE id = ?`, id,
	).Scan(&kind, &key); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return kind, key
}

func t33CountLore(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lore_entry`).Scan(&n); err != nil {
		t.Fatalf("count lore: %v", err)
	}
	return n
}

func t33OpenAt93(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t33UpTo93(t, db)
	return db
}

// TestMigration00100MovesARoleWithExactlyOneActiveMemberOntoThatMember — the
// case the owner's instruction names, and the only one where 「那個角色底下的
// 那一位成員」 has a referent.
//
// 🔴 THE MANUAL ENTRY IN THE FIXTURE IS LOAD-BEARING. Without it, an
// implementation that rekeyed EVERY row onto the one member it found — ignoring
// scope_kind entirely — would be indistinguishable from a correct one. It is
// here so that "only role-scoped rows were touched" is something this test can
// actually fail on.
func TestMigration00100MovesARoleWithExactlyOneActiveMemberOntoThatMember(t *testing.T) {
	db := t33OpenAt93(t, "t33-move.db")
	t33SeedMember(t, db, "m-only", "r-alpha", "active")
	t33SeedLore(t, db, "L-1", 1, "role", "r-alpha", "first")
	t33SeedLore(t, db, "L-2", 2, "role", "r-alpha", "second")
	t33SeedLore(t, db, "L-3", 3, "manual", "tm-review", "a manual entry")

	t33UpTo100(t, db)

	for _, id := range []string{"L-1", "L-2"} {
		kind, key := t33ScopeOf(t, db, id)
		if kind != "agent" || key != "m-only" {
			t.Fatalf("%s is at %s/%s, want agent/m-only — a role with exactly one "+
				"active member has an unambiguous owner and its 傳承 must follow "+
				"the reader, which is that member", id, kind, key)
		}
	}
	// The manual row is untouched, key and kind both.
	if kind, key := t33ScopeOf(t, db, "L-3"); kind != "manual" || key != "tm-review" {
		t.Fatalf("the manual entry moved to %s/%s — this migration must touch "+
			"role-scoped rows and nothing else", kind, key)
	}
	if n := t33CountLore(t, db); n != 3 {
		t.Fatalf("lore_entry holds %d rows after the migration, want 3 — a rekey "+
			"neither creates nor destroys entries", n)
	}
}

// TestMigration00100LeavesARoleWithNoActiveMemberAlone — the zero arm, and the
// fixture is the shape that actually occurs: the role's member was DISMISSED.
//
// 🔴 THE DISMISSED ROW IS STILL THERE, which is the whole point. Dismissal sets
// roster_status='removed' and keeps the row forever, so an implementation that
// asked "does any member carry this role_key?" instead of "is exactly one ACTIVE
// member carrying it?" would find m-gone, decide the answer was unambiguous, and
// file this 傳承 under somebody who has left the company. That write cannot be
// undone through any face this station exposes, because a 傳承 entry has no edit
// path.
func TestMigration00100LeavesARoleWithNoActiveMemberAlone(t *testing.T) {
	db := t33OpenAt93(t, "t33-none.db")
	t33SeedMember(t, db, "m-gone", "r-empty", "removed")
	t33SeedLore(t, db, "L-1", 1, "role", "r-empty", "an orphan")

	t33UpTo100(t, db)

	kind, key := t33ScopeOf(t, db, "L-1")
	if kind != "role" || key != "r-empty" {
		t.Fatalf("L-1 is at %s/%s, want it LEFT at role/r-empty. The only member "+
			"carrying that role is dismissed, so there is nobody the entry can "+
			"honestly follow; a migration that moved it anyway filed one member's "+
			"傳承 under a departed one, irreversibly and with nothing reporting it.",
			kind, key)
	}
	if n := t33CountLore(t, db); n != 1 {
		t.Fatalf("lore_entry holds %d rows, want 1 — an entry the migration could "+
			"not place must be LEFT, never deleted", n)
	}
}

// TestMigration00100LeavesARoleWithTwoActiveMembersAlone — the ambiguous arm.
//
// 🔴 THIS IS THE TEST THAT SAYS THE ONE-TO-ONE IS NOT ENFORCED ANYWHERE. Two
// active members under one role is not a corrupt fixture: member.role_key has no
// UNIQUE index and the hire face never checks, so the schema admits it and this
// migration has to survive meeting it. Nothing here asserts that the station
// SHOULD have such a role — only that the migration refuses to guess when it
// does, rather than picking the alphabetically-first id and calling it an
// answer.
func TestMigration00100LeavesARoleWithTwoActiveMembersAlone(t *testing.T) {
	db := t33OpenAt93(t, "t33-two.db")
	t33SeedMember(t, db, "m-aaa", "r-shared", "active")
	t33SeedMember(t, db, "m-bbb", "r-shared", "active")
	t33SeedLore(t, db, "L-1", 1, "role", "r-shared", "ambiguous")

	t33UpTo100(t, db)

	kind, key := t33ScopeOf(t, db, "L-1")
	if kind == "agent" && key == "m-aaa" {
		t.Fatalf("L-1 was filed under m-aaa — the alphabetically first of TWO " +
			"active members. That is a guess with a deterministic tie-break, not " +
			"an answer: the other member has an equal claim and there is no edit " +
			"path to correct it afterwards.")
	}
	if kind != "role" || key != "r-shared" {
		t.Fatalf("L-1 is at %s/%s, want it LEFT at role/r-shared", kind, key)
	}
}

// TestMigration00100IsIdempotent — a replay must not move anything a second
// time and must not error.
//
// 🔴 THE SECOND RUN IS FORCED, not simulated. goose records 00100 as applied, so
// simply calling UpTo again is a no-op that proves nothing about the SQL. This
// calls the migration's own Up on a fresh transaction against the ALREADY
// MIGRATED database, which is the state a replay actually meets.
func TestMigration00100IsIdempotent(t *testing.T) {
	db := t33OpenAt93(t, "t33-idem.db")
	t33SeedMember(t, db, "m-only", "r-alpha", "active")
	// A member whose id happens to equal another role's key. If a re-run matched
	// on scope_key without also constraining scope_kind, this row is what would
	// get picked up on the second pass.
	t33SeedMember(t, db, "r-alpha", "r-beta", "active")
	t33SeedLore(t, db, "L-1", 1, "role", "r-alpha", "moves once")

	t33UpTo100(t, db)
	kind1, key1 := t33ScopeOf(t, db, "L-1")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := upLoreRoleScopeToMember(t.Context(), tx); err != nil {
		tx.Rollback()
		t.Fatalf("second Up returned an error — a replay must be a no-op, not a "+
			"failure: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	kind2, key2 := t33ScopeOf(t, db, "L-1")
	if kind1 != kind2 || key1 != key2 {
		t.Fatalf("a replay moved the entry again: %s/%s became %s/%s. The first "+
			"run's destination is a MEMBER id; a second migration of it would key "+
			"the entry off whatever that id collides with.", kind1, key1, kind2, key2)
	}
	if kind2 != "agent" || key2 != "m-only" {
		t.Fatalf("after two runs L-1 is at %s/%s, want agent/m-only", kind2, key2)
	}
}

// TestMigration00100DownReturnsStaffEntriesAndLeavesOutsourceAlone — the
// retreat, in the two halves that differ.
//
// A member carrying a role_key goes back to that role; a member carrying none
// (an outsource worker, which is what the agent scope held before this
// migration existed) must NOT — 'role' has nothing to name for it, and a Down
// that swept it up would invent a role_key of "" and hide the entry from every
// reader on the older binary too.
func TestMigration00100DownReturnsStaffEntriesAndLeavesOutsourceAlone(t *testing.T) {
	db := t33OpenAt93(t, "t33-down.db")
	t33SeedMember(t, db, "m-staff", "r-alpha", "active")
	t33SeedMember(t, db, "ow-hand", "", "active")
	t33SeedLore(t, db, "L-1", 1, "role", "r-alpha", "staff entry")
	t33SeedLore(t, db, "L-2", 2, "agent", "ow-hand", "outsource entry")

	t33UpTo100(t, db)
	if kind, key := t33ScopeOf(t, db, "L-1"); kind != "agent" || key != "m-staff" {
		t.Fatalf("precondition: L-1 is at %s/%s, want agent/m-staff", kind, key)
	}

	if err := goose.DownTo(db, "migrations", 93); err != nil {
		t.Fatalf("goose down to 93: %v", err)
	}

	if kind, key := t33ScopeOf(t, db, "L-1"); kind != "role" || key != "r-alpha" {
		t.Fatalf("after Down, L-1 is at %s/%s, want role/r-alpha — the older "+
			"binary reads staff 傳承 out of the role scope, so a rollback that "+
			"left it at agent would hide it from the fold it is being restored for",
			kind, key)
	}
	if kind, key := t33ScopeOf(t, db, "L-2"); kind != "agent" || key != "ow-hand" {
		t.Fatalf("after Down, the OUTSOURCE entry is at %s/%s, want it untouched "+
			"at agent/ow-hand. It was never role-scoped and its member holds no "+
			"role_key, so there is no role for it to return to.", kind, key)
	}
}

// TestMigration00100ReportsTheOrphansItLeftBehind — the report is a REQUIREMENT,
// not logging.
//
// 🔴 WHY THIS IS A TEST AND NOT A COMMENT. Everything else in this file checks
// that the right rows moved and the wrong ones did not. None of it can fail if
// the migration silently stops SAYING which rows it left: the database would be
// in exactly the correct state and the operator would simply never learn that
// three entries are now unreachable by any 屬於 filter. The orphan lines are the
// only channel that carries that fact off the machine, so they are pinned here —
// the count, and the role_key, for each arm.
func TestMigration00100ReportsTheOrphansItLeftBehind(t *testing.T) {
	db := t33OpenAt93(t, "t33-report.db")
	// One role that moves, one with nobody active, one with two active.
	t33SeedMember(t, db, "m-only", "r-moves", "active")
	t33SeedMember(t, db, "m-left", "r-empty", "removed")
	t33SeedMember(t, db, "m-aaa", "r-shared", "active")
	t33SeedMember(t, db, "m-bbb", "r-shared", "active")
	t33SeedLore(t, db, "L-1", 1, "role", "r-moves", "moves")
	t33SeedLore(t, db, "L-2", 2, "role", "r-empty", "orphan A")
	t33SeedLore(t, db, "L-3", 3, "role", "r-shared", "orphan B")
	t33SeedLore(t, db, "L-4", 4, "role", "r-shared", "orphan C")

	var buf strings.Builder
	prev := loreScopeMigrationLog
	loreScopeMigrationLog = &buf
	t.Cleanup(func() { loreScopeMigrationLog = prev })

	t33UpTo100(t, db)
	out := buf.String()

	// Each orphaned group is named, with its own count and its own reason. A
	// summary line alone would say "3 entries were left" and send whoever reads
	// it hunting through the table for WHICH.
	for _, want := range []string{
		`"r-empty"`, `"r-shared"`,
		"no member is active under it",
		"2 members are active under it",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the migration's report does not contain %q.\nreport:\n%s",
				want, out)
		}
	}
	// The role that MOVED is not reported as an orphan — a report that named
	// every role would be as useless as one that named none.
	if strings.Contains(out, `"r-moves"`) {
		t.Fatalf("r-moves was reported as left-behind, and it was moved:\n%s", out)
	}
	// The summary's two numbers, which are what an operator actually reads.
	if !strings.Contains(out, "moved 1 entry") {
		t.Fatalf("the summary does not report 1 moved entry:\n%s", out)
	}
	if !strings.Contains(out, "left 3 entries") {
		t.Fatalf("the summary does not report 3 left entries — three rows across "+
			"two roles were declined:\n%s", out)
	}
}

// TestMigration00100ReportsEvenWhenItHadNothingToDo — the unconditional half.
//
// A migration that only spoke when something went wrong would make silence
// ambiguous: "no orphans" and "the report was removed / never ran" would look
// identical in a log, and the second is the one people assume when they see
// nothing.
func TestMigration00100ReportsEvenWhenItHadNothingToDo(t *testing.T) {
	db := t33OpenAt93(t, "t33-report-empty.db")

	var buf strings.Builder
	prev := loreScopeMigrationLog
	loreScopeMigrationLog = &buf
	t.Cleanup(func() { loreScopeMigrationLog = prev })

	t33UpTo100(t, db)

	out := buf.String()
	if !strings.Contains(out, "moved 0 entries") || !strings.Contains(out, "left 0 entries") {
		t.Fatalf("a run with nothing to do must still report both numbers, so that "+
			"silence means 'did not run' rather than 'nothing to do'.\nreport:\n%s", out)
	}
}

// TestMigration00100OnTheTrialStationShape — the SPECIFIC input the owner is
// about to run this against, pinned as a fixture rather than reasoned about.
//
// 🔴 WHY A SEPARATE TEST WHEN THE ARMS ARE ALREADY COVERED. The arms above are
// each a minimal fixture built to isolate one rule. This one reproduces the
// SHAPE of the real data — nine entries, three of them role-scoped under
// `assistant`, with exactly one active member (`mira`) under that role, the rest
// spread across other scopes — and asserts the whole outcome at once: three rows
// move to agent/mira and NOTHING is orphaned. A rule that is individually
// correct can still add up to the wrong total on a real mix (a stray manual row
// swept along, an entry double-counted), and that total is what the owner will
// see. The numbers here are the trial station's as reported on 2026-09-07.
//
// ⚠️ IT IS A SHAPE, NOT A COPY. The other six entries' exact scopes were not
// transcribed, so they are stand-ins chosen to exercise the neighbours a real
// mix has: other members, a manual, and an entry already at `agent`. If the real
// station turns out to hold a role-scoped entry under some OTHER role, this test
// says nothing about it — the arms above are what cover that, and the
// migration's own orphan report is what would announce it at run time.
func TestMigration00100OnTheTrialStationShape(t *testing.T) {
	db := t33OpenAt93(t, "t33-trial.db")
	// `assistant` has exactly one ACTIVE member. The dismissed row beside it is
	// what makes this a real fixture rather than a clean-room one: a station that
	// has been running has departed members, and counting them would break this.
	t33SeedMember(t, db, "mira", "assistant", "active")
	t33SeedMember(t, db, "m-departed", "assistant", "removed")
	t33SeedMember(t, db, "ow-7d8ad859dd9b", "", "active")

	// Nine entries. Three role-scoped under `assistant` — L-1, L-4, L-9, the ids
	// the owner reported — and six others that must not move.
	t33SeedLore(t, db, "L-1", 1, "role", "assistant", "first")
	t33SeedLore(t, db, "L-2", 2, "manual", "tm-review", "a manual entry")
	t33SeedLore(t, db, "L-3", 3, "agent", "ow-7d8ad859dd9b", "a contractor entry")
	t33SeedLore(t, db, "L-4", 4, "role", "assistant", "second")
	t33SeedLore(t, db, "L-5", 5, "manual", "tm-review", "another manual entry")
	t33SeedLore(t, db, "L-6", 6, "manual", "tm-ship", "a second manual")
	t33SeedLore(t, db, "L-7", 7, "agent", "ow-7d8ad859dd9b", "another contractor entry")
	t33SeedLore(t, db, "L-8", 8, "manual", "tm-review", "a third manual entry")
	t33SeedLore(t, db, "L-9", 9, "role", "assistant", "third")

	var buf strings.Builder
	prev := loreScopeMigrationLog
	loreScopeMigrationLog = &buf
	t.Cleanup(func() { loreScopeMigrationLog = prev })

	t33UpTo100(t, db)

	// ── the three that move ────────────────────────────────────────────────
	for _, id := range []string{"L-1", "L-4", "L-9"} {
		kind, key := t33ScopeOf(t, db, id)
		if kind != "agent" || key != "mira" {
			t.Fatalf("%s is at %s/%s, want agent/mira", id, kind, key)
		}
	}

	// ── and the six that must not ──────────────────────────────────────────
	for _, want := range []struct{ id, kind, key string }{
		{"L-2", "manual", "tm-review"},
		{"L-3", "agent", "ow-7d8ad859dd9b"},
		{"L-5", "manual", "tm-review"},
		{"L-6", "manual", "tm-ship"},
		{"L-7", "agent", "ow-7d8ad859dd9b"},
		{"L-8", "manual", "tm-review"},
	} {
		kind, key := t33ScopeOf(t, db, want.id)
		if kind != want.kind || key != want.key {
			t.Fatalf("%s moved to %s/%s, want it untouched at %s/%s",
				want.id, kind, key, want.kind, want.key)
		}
	}

	// ── ZERO ORPHANS, asserted on the data AND on the report ───────────────
	//
	// 🔴 BOTH, because they can disagree. A count over the table says what
	// happened; the report is what the operator will actually read. If the
	// migration ever moved rows without reporting the totals (or vice versa),
	// one of these two lines catches it and a single check would not.
	var leftAtRole int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM lore_entry WHERE scope_kind = 'role'`,
	).Scan(&leftAtRole); err != nil {
		t.Fatalf("count leftovers: %v", err)
	}
	if leftAtRole != 0 {
		t.Fatalf("%d entries were left at scope_kind='role'; on this input every "+
			"role-scoped entry has exactly one active member to follow, so the "+
			"expected orphan count is 0", leftAtRole)
	}
	if n := t33CountLore(t, db); n != 9 {
		t.Fatalf("lore_entry holds %d rows, want all 9 — a rekey neither creates "+
			"nor destroys entries", n)
	}
	out := buf.String()
	if !strings.Contains(out, "moved 3 entries") {
		t.Fatalf("the report does not say 3 entries moved:\n%s", out)
	}
	if !strings.Contains(out, "left 0 entries") {
		t.Fatalf("the report does not say 0 entries were left behind:\n%s", out)
	}
	if strings.Contains(out, "LEFT AS-IS") {
		t.Fatalf("the report named an orphan on an input that has none:\n%s", out)
	}
}
