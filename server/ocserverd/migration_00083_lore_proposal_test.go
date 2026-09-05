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
	written := m83SeedEntryAtPreviousStage(t, db)

	// ── UP: the table arrives, and an entry that predates it can be proposed
	// against. That is the old-data question for this change: lore_proposal is
	// new and empty, but the rows it POINTS AT are older than it is.
	m83UpTo(t, db, mine)
	if !m83HasTable(t, db, "lore_proposal") {
		t.Fatal("the lore_proposal table did not arrive")
	}
	// 🔴 這一列是用原始 INSERT 填的，不是 CreateLoreProposal，理由跟 seed 一樣而且
	// 更不明顯：CreateLoreProposal 第一件事是 GetLoreEntry，而那個 SELECT 名的是
	// HEAD 這一階的 lore_entry 欄位（00084 的 heading / impact / …），這裡的資料庫
	// 停在 00083。走 DAL 只會撞到「沒有這個欄位」，而那跟這支測的東西無關。
	// ⚠️ 因此這一段量到的是「00083 這張表收得下一列指向更老條目的提案」，**不是**
	// 「提案路徑在這一階能跑」。後者已經沒有辦法在這裡量了，這行字就是那個縮水。
	filedID := "lp-m83seed0001"
	base := m83LatestRevisionID(t, db, written.EntryID)
	if _, err := db.Exec(`
		INSERT INTO lore_proposal (id, entry_id, kind, base_revision_id, base_sha256,
			encountered, fault, evidence, trigger, content, retire_when, problem,
			body, sha256, actor_id, created_ts)
		VALUES (?, ?, 'update', ?, ?, ?, 'stale', ?, ?, ?, '', '', ?, ?, ?, 2000)`,
		filedID, written.EntryID, base, written.SHA256,
		"T-33 slot 4, wiring the proposal route",
		"the entry names dal_lore.go, and the function moved",
		"我要確認開機脈絡是在哪一個檔案組起來的",
		"the fold happens in one place, and that place is lore_fold.go",
		"body", "sha", "ow-e27260b9ed05"); err != nil {
		t.Fatalf("file a proposal against an entry written BEFORE this migration: %v", err)
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
	//
	// 🔴 用 COUNT 而不是 dal.GetLoreEntry：資料庫現在退到了 prev 這一階，而 DAL 的
	// SELECT 名的是 HEAD 那一階的欄位（v8 的 heading / impact / impact_stars /
	// reviewed 從 00084 才存在）。走 DAL 只會撞到「沒有這個欄位」——那是在量
	// 「DAL 比資料庫新」，不是在量「這一列有沒有活下來」。
	var alive int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM lore_entry WHERE id = ?`, written.EntryID).Scan(&alive); err != nil {
		t.Fatalf("the entry did not survive Down: %v", err)
	}
	if alive != 1 {
		t.Fatalf("the entry did not survive Down: %d rows", alive)
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
	if filedID == "" {
		t.Fatal("nothing was ever filed, so the loss above is not a loss")
	}
}

// m83SeedEntryAtPreviousStage writes one entry AND its L0 original with raw SQL,
// naming only the columns that exist at the stage before 00083.
//
// 🔴 它不能走 CreateLoreEntry，而理由不是圖方便：那個函式寫的是 HEAD 這一階的
// 欄位（00084 的 heading / impact / impact_stars / reviewed），而這裡的資料庫
// 停在 00082。走 DAL 的話這支測試會在「seed」就爆掉，而爆的原因跟它要測的
// 「00083 的 Down 退了幾階」毫無關係——一支因為別的理由紅掉的守衛，跟一支壞掉的
// 守衛一樣沒有用。
//
// ⚠️ 這是抄一份寫入路徑，代價照實說：CreateLoreEntry 之後多寫一欄，這裡不會知道。
// 但它要種的本來就不是「今天的條目」，而是「一列比 00083 還老的條目」——那一列
// 依定義就不該帶今天的欄位。
func m83SeedEntryAtPreviousStage(t *testing.T, db *sql.DB) LoreWriteResult {
	t.Helper()
	w := t33Write()
	// 第 4 格在 00082 那一階的欄名還是 `problem`（00084 才改成 `impact`）。
	// 摘要用的是 HEAD 的渲染器，而那正確：sha256 比的是那串位元組，不是欄名。
	entry := LoreEntry{
		ID: "lore-m83-seed", Trigger: w.Trigger, Content: w.Content,
		RetireWhen: w.RetireWhen, Impact: w.Impact, Origin: w.Origin,
	}
	body := loreRevisionBody(entry, nil)
	sum := loreSHA256(body)
	if _, err := db.Exec(`
		INSERT INTO lore_entry (id, trigger, content, retire_when, problem,
			status, supersedes, editable_by, origin, created_ts, updated_ts)
		VALUES (?, ?, ?, ?, ?, 'active', '', 'agent', ?, 1000, 1000)`,
		entry.ID, entry.Trigger, entry.Content, entry.RetireWhen, entry.Impact,
		entry.Origin); err != nil {
		t.Fatalf("seed entry at the previous stage: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, ?, 1000, 0)`, entry.ID, body, sum, w.ActorID); err != nil {
		t.Fatalf("seed the L0 original at the previous stage: %v", err)
	}
	return LoreWriteResult{EntryID: entry.ID, SHA256: sum}
}

// m83LatestRevisionID reads the L0 row id an entry's newest original carries.
// Raw SQL for the same reason everything else here is: the stage under test is
// older than the DAL.
func m83LatestRevisionID(t *testing.T, db *sql.DB, entryID string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`SELECT id FROM lore_revision WHERE entry_id = ? ORDER BY id DESC LIMIT 1`,
		entryID).Scan(&id); err != nil {
		t.Fatalf("latest revision for %s: %v", entryID, err)
	}
	return id
}
