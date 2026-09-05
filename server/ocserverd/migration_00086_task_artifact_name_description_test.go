package main

// migration_00086_task_artifact_name_description_test.go — T-92.
//
// WHY THIS FILE EXISTS. 00086 is a DROP TABLE + rebuild over the live artifact
// set (measured 2026-09-05: 3,240 task_artifact rows and 71 task_artifact_history
// rows) and it shipped with nothing asserting what the rebuild produces. Its own
// header names the trap — "🔴 THE INDEX IS THE TRAP … Miss one and nothing
// raises" — and until this file the only thing standing between that sentence and
// production was a reviewer's eye. Every item below is a claim 00086 makes in
// prose; each one is now a claim a mutant can turn red.
//
// The seven claims, in the order the assertions appear:
//
//	① file / image → `description` carries the OLD label WHOLE (313 live labels
//	  are longer than the new 256 write cap and must NOT be truncated), and
//	  `name` is left EMPTY — the read path derives a file's name from its blob's
//	  filename rather than copying it into a column that can go stale.
//	② link → `name` is `substr(label, 1, 48)`, which in SQLite counts CHARACTERS,
//	  not bytes, so the fixture below carries multi-byte labels whose 48-rune and
//	  48-byte prefixes differ. `description` still carries the label whole.
//	③ link `url` becomes a text/uri-list blob whose bytes are the url's bytes,
//	  and the mint is DEDUPED: 704+9 live rows point at 641 distinct urls, so two
//	  rows with the same url must end up on ONE blob. The fixture seeds two rows
//	  sharing a url plus a third with a different one, and asserts both halves —
//	  same url ⇒ same attachment_id, different url ⇒ different attachment_id.
//	  Dedupe also crosses the two tables (the mint UNIONs them), so a history row
//	  shares the live rows' url too.
//	④ `attachment_id` is non-empty in BOTH tables afterwards: `url` as a column is
//	  gone, so a blank attachment_id is an artifact that points at nothing.
//	⑤ task_artifact_history.id is copied through with its VALUE. The version list
//	  is `ORDER BY id DESC`; let the rebuild re-number and every artifact's
//	  versions silently reorder while every query keeps answering. The fixture
//	  uses sparse, deliberately non-sequential ids inserted out of order, so a
//	  re-numbering rebuild produces different values AND a different order.
//	⑥ BOTH indexes are rebuilt. `idx_task_artifact_task` had an assertion
//	  elsewhere; `idx_task_artifact_history_artifact` had none, which is exactly
//	  the half the migration's own comment warns about.
//	⑦ Down puts `url` and `label` back per row, and COLLECTS the blobs the Up
//	  minted — chat_attachment must return to its pre-Up population, no more and
//	  no less. A pre-existing text/uri-list blob that no link artifact references
//	  is seeded as a negative control: the collector must not sweep it up.

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	migration00086Version = 86
	// 00081-00085 do not exist in this tree (they are held by branches in
	// flight — see 00086's header), so the version immediately below 00086 is
	// 00080. Written as its own constant so that a future migration filling one
	// of those gaps is a one-line edit rather than a puzzle.
	migration00086PriorVersion = 80
)

// The two urls the live rows share, and one only a history row uses. Chosen to
// exercise the mint's UNION across both tables.
const (
	t92URLShared = "https://github.com/hardcoretech/officraft/pull/387"
	t92URLOther  = "https://example.com/artifact?q=a%20b&x=1"
	// A url only task_artifact_history points at. Its blob must still be minted
	// (the mint UNIONs both tables) and still be collected by Down.
	t92URLHistOnly = "https://example.com/only-in-history"
)

// t92LongLabel is longer than BOTH caps (48 for name, 256 for description) and
// is multi-byte, so `substr(label,1,48)` counting characters and counting bytes
// give visibly different answers. If SQLite ever changed to byte semantics the
// name assertion below would go red rather than silently cut a rune in half.
var t92LongLabel = "【產物】" + strings.Repeat("交付物件 deliverable ", 20) + "END"

// t92FileLabel is > 256 characters: 00086 promises migrated descriptions are NOT
// truncated, and 313 live rows depend on that promise.
var t92FileLabel = "file label 檔案標籤 — " + strings.Repeat("x", 400) + " 結束"

// t92ArtifactRow is one seeded pre-00086 task_artifact row.
type t92ArtifactRow struct {
	id           string
	taskID       string
	kind         string
	attachmentID string
	url          string
	label        string
}

// t92ArtifactFixture is the live-table world. Two link rows deliberately share
// t92URLShared — that pair is the entire dedupe assertion.
func t92ArtifactFixture() []t92ArtifactRow {
	return []t92ArtifactRow{
		{"ta-file0001", "t-alpha", "file", "att-000000000001", "", t92FileLabel},
		{"ta-file0002", "t-alpha", "file", "att-000000000002", "", ""}, // empty label is still a label
		{"ta-img00001", "t-beta", "image", "att-000000000003", "", "screenshot 截圖 🖼"},
		{"ta-link0001", "t-alpha", "link", "", t92URLShared, t92LongLabel},
		{"ta-link0002", "t-beta", "link", "", t92URLShared, "PR #387"}, // same url, DIFFERENT label
		{"ta-link0003", "t-beta", "link", "", t92URLOther, "外部連結"},
	}
}

// t92HistoryRow is one seeded pre-00086 task_artifact_history row. `id` is
// explicit and sparse on purpose — see claim ⑤.
type t92HistoryRow struct {
	id           int64
	artifactID   string
	kind         string
	attachmentID string
	url          string
	label        string
	createdTS    float64
}

// t92HistoryFixture inserts ids OUT OF ORDER and with GAPS. A rebuild that lets
// AUTOINCREMENT re-number would hand back 1,2,3,4 in insertion order, which
// differs from these both in value and in the `ORDER BY id DESC` sequence.
func t92HistoryFixture() []t92HistoryRow {
	return []t92HistoryRow{
		{42, "ta-file0001", "file", "att-000000000010", "", "v1 of the file 檔案第一版", 100.5},
		{7, "ta-file0001", "file", "att-000000000011", "", "v0 of the file", 90.25},
		{913, "ta-link0001", "link", "", t92URLShared, t92LongLabel, 110.0},
		{58, "ta-link0009", "link", "", t92URLHistOnly, "a version whose live row is gone", 95.0},
	}
}

// t92World brings a temp database to the state just BELOW 00086 and seeds both
// old-schema tables plus the blobs they reference.
func t92World(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t92-artifact-name-desc.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00086PriorVersion); err != nil {
		t.Fatalf("down to %d: %v", migration00086PriorVersion, err)
	}

	// The old schema must actually be in place, or every "the rebuild produced
	// X" assertion below would be measuring the wrong table.
	for _, col := range []string{"url", "label"} {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('task_artifact') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info(task_artifact): %v", err)
		}
		if n != 1 {
			t.Fatalf("task_artifact has no %q column at version %d — the world is not pre-00086",
				col, migration00086PriorVersion)
		}
	}

	// The blobs the file/image rows point at. 00086 copies their ids through
	// untouched; they exist here so the Down's collector has real neighbours it
	// must NOT delete.
	for _, r := range t92ArtifactFixture() {
		if r.attachmentID == "" {
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO chat_attachment (id, mime, data, filename) VALUES (?, 'image/png', ?, 'shot.png')`,
			r.attachmentID, []byte("blob bytes for "+r.attachmentID)); err != nil {
			t.Fatalf("seed blob %s: %v", r.attachmentID, err)
		}
	}
	for _, h := range t92HistoryFixture() {
		if h.attachmentID == "" {
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO chat_attachment (id, mime, data, filename) VALUES (?, 'application/pdf', ?, 'old.pdf')`,
			h.attachmentID, []byte("blob bytes for "+h.attachmentID)); err != nil {
			t.Fatalf("seed history blob %s: %v", h.attachmentID, err)
		}
	}
	// NEGATIVE CONTROL for the Down collector: a text/uri-list blob that no link
	// artifact points at — a .uri file someone genuinely uploaded. Down deletes
	// by mime AND by referent, so this must survive.
	if _, err := db.Exec(
		`INSERT INTO chat_attachment (id, mime, data, filename) VALUES ('att-uploadeduri', 'text/uri-list', ?, 'bookmarks.uri')`,
		[]byte("https://someone-uploaded-this.example/\n")); err != nil {
		t.Fatalf("seed uploaded uri-list blob: %v", err)
	}

	for _, r := range t92ArtifactFixture() {
		if _, err := db.Exec(
			`INSERT INTO task_artifact (id, task_id, kind, attachment_id, url, label, created_ts, created_by)
			 VALUES (?, ?, ?, ?, ?, ?, 1234.5, 'owner')`,
			r.id, r.taskID, r.kind, r.attachmentID, r.url, r.label); err != nil {
			t.Fatalf("seed task_artifact %s: %v", r.id, err)
		}
	}
	for _, h := range t92HistoryFixture() {
		if _, err := db.Exec(
			`INSERT INTO task_artifact_history (id, artifact_id, kind, attachment_id, url, label, created_ts, created_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'owner')`,
			h.id, h.artifactID, h.kind, h.attachmentID, h.url, h.label, h.createdTS); err != nil {
			t.Fatalf("seed task_artifact_history %d: %v", h.id, err)
		}
	}

	// ANTI-VACUITY: a fixture that failed to land would let every assertion pass
	// over empty tables, which is indistinguishable from a working migration.
	var a, h int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_artifact`).Scan(&a); err != nil {
		t.Fatalf("count task_artifact: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_artifact_history`).Scan(&h); err != nil {
		t.Fatalf("count task_artifact_history: %v", err)
	}
	if a != len(t92ArtifactFixture()) || h != len(t92HistoryFixture()) {
		t.Fatalf("fixture did not land: task_artifact=%d (want %d), history=%d (want %d)",
			a, len(t92ArtifactFixture()), h, len(t92HistoryFixture()))
	}
	return db
}

func t92Up(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.UpTo(db, "migrations", migration00086Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00086Version, err)
	}
}

// t92Prefix48 is the expected `name` for a link: the first 48 CHARACTERS of the
// label, computed in Go from the fixture rather than read back out of SQLite, so
// the assertion does not borrow the migration's own expression.
func t92Prefix48(label string) string {
	r := []rune(label)
	if len(r) <= 48 {
		return label
	}
	return string(r[:48])
}

type t92Artifact struct {
	kind         string
	attachmentID string
	name         string
	description  string
}

func t92ReadArtifacts(t *testing.T, db *sql.DB) map[string]t92Artifact {
	t.Helper()
	rows, err := db.Query(`SELECT id, kind, attachment_id, name, description FROM task_artifact`)
	if err != nil {
		t.Fatalf("read task_artifact: %v", err)
	}
	defer rows.Close()
	out := map[string]t92Artifact{}
	for rows.Next() {
		var id string
		var a t92Artifact
		if err := rows.Scan(&id, &a.kind, &a.attachmentID, &a.name, &a.description); err != nil {
			t.Fatalf("scan task_artifact: %v", err)
		}
		out[id] = a
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// t92BlobBytes returns the raw bytes of a chat_attachment row, and whether it
// exists. Read as []byte, never as string, because claim ③ is byte-for-byte.
func t92BlobBytes(t *testing.T, db *sql.DB, id string) ([]byte, string, bool) {
	t.Helper()
	var data []byte
	var mime string
	err := db.QueryRow(`SELECT data, mime FROM chat_attachment WHERE id = ?`, id).Scan(&data, &mime)
	if err == sql.ErrNoRows {
		return nil, "", false
	}
	if err != nil {
		t.Fatalf("read blob %s: %v", id, err)
	}
	return data, mime, true
}

// ── ① + ② the label split ────────────────────────────────────────────────────

// TestMigration00086SplitsLabelIntoNameAndDescription is claims ① and ②: what
// each kind's `name` and `description` hold after the rebuild.
func TestMigration00086SplitsLabelIntoNameAndDescription(t *testing.T) {
	db := t92World(t)
	t92Up(t, db)

	got := t92ReadArtifacts(t, db)
	if len(got) != len(t92ArtifactFixture()) {
		t.Fatalf("the rebuild produced %d rows, want %d — a copy that drops rows is the "+
			"one failure a per-row check below could not see", len(got), len(t92ArtifactFixture()))
	}

	for _, r := range t92ArtifactFixture() {
		a, ok := got[r.id]
		if !ok {
			t.Errorf("artifact %s vanished in the rebuild", r.id)
			continue
		}
		if a.kind != r.kind {
			t.Errorf("%s: kind %q, want %q", r.id, a.kind, r.kind)
		}

		// ① and ② share this half: description is the OLD LABEL, WHOLE. 313 live
		// rows carry a label longer than the new 256 cap and 00086 promises in
		// prose that none of them is cut.
		if a.description != r.label {
			t.Errorf("%s (%s): description is not the old label whole.\n got (%d chars): %q\n"+
				"want (%d chars): %q\n\n00086 caps NEW writes at 256 and explicitly does NOT "+
				"truncate migrated values — 313 live rows are longer than that cap and a "+
				"truncating copy would silently shorten every one of them.",
				r.id, r.kind, len([]rune(a.description)), a.description, len([]rune(r.label)), r.label)
		}

		switch r.kind {
		case "file", "image":
			// ① file/image names stay EMPTY. The read path derives a name from
			// the blob's filename; copying the label in would create a second
			// column that goes stale the moment the content is replaced.
			if a.name != "" {
				t.Errorf("%s (%s): name is %q, want EMPTY. 00086 leaves file/image names blank "+
					"on purpose — the name is derived from the blob's filename at read time, "+
					"and a copied one goes stale when the content is swapped.", r.id, r.kind, a.name)
			}
		case "link":
			// ② a link's name is the first 48 CHARACTERS of the label. Computed
			// here in Go over runes; a byte-wise cut would both differ from this
			// and split a multi-byte rune.
			want := t92Prefix48(r.label)
			if a.name != want {
				t.Errorf("%s (link): name is %q (%d chars), want %q (%d chars).\n\n"+
					"A link's cockpit title is substr(label,1,48), and SQLite's substr over "+
					"TEXT counts CHARACTERS — the same unit the write cap uses. A byte-wise "+
					"cut gives a different answer here and can leave a broken rune behind.",
					r.id, a.name, len([]rune(a.name)), want, len([]rune(want)))
			}
		}
	}
}

// ── ③ the url mint, and its dedupe ───────────────────────────────────────────

// TestMigration00086MintsOneDedupedBlobPerDistinctURL is claim ③, and the
// dedupe half is the part with the most leverage: 704+9 live link rows point at
// 641 distinct urls, so a mint that forgot to dedupe would create ~72 redundant
// blobs AND — worse — leave the collector's shared-blob accounting describing a
// world that no longer exists.
func TestMigration00086MintsOneDedupedBlobPerDistinctURL(t *testing.T) {
	db := t92World(t)
	t92Up(t, db)
	got := t92ReadArtifacts(t, db)

	link1 := got["ta-link0001"] // t92URLShared
	link2 := got["ta-link0002"] // t92URLShared, different label
	link3 := got["ta-link0003"] // t92URLOther

	// 🔴 DEDUPE, both directions, and asserted FIRST: the byte-equality loop
	// below Fatal()s when a blob is missing, which would mask this.
	// Same url ⇒ ONE blob shared…
	if link1.attachmentID != link2.attachmentID {
		t.Errorf("TWO LINK ROWS WITH THE SAME URL GOT DIFFERENT BLOBS: %s vs %s.\n\n"+
			"00086 mints one blob per DISTINCT url (measured: 704+9 rows over 641 urls) and "+
			"relies on that: a blob two artifacts share is collected only when BOTH stop "+
			"pointing at it, which is what dal.go collectSurvivingBlobRefs already computes. "+
			"A per-row mint stores the same bytes many times and quietly changes what "+
			"'shared' means for the collector.",
			link1.attachmentID, link2.attachmentID)
	}
	// …and different urls must NOT collapse onto one, which is the failure a
	// dedupe assertion alone would happily accept.
	if link3.attachmentID == link1.attachmentID {
		t.Errorf("two DIFFERENT urls landed on the same blob %s — the dedupe key must be the "+
			"url, not a constant", link3.attachmentID)
	}

	// The blob's bytes ARE the url's bytes. RFC 2483 says a uri-list is one URI
	// per line and these carry exactly the url — no trailing newline, no
	// re-encoding — so the blob can say what it is without a second field
	// somewhere else saying it.
	for _, c := range []struct {
		id, url, why string
	}{
		{link1.attachmentID, t92URLShared, "the shared url"},
		{link3.attachmentID, t92URLOther, "a url carrying % escapes and an &"},
	} {
		data, mime, ok := t92BlobBytes(t, db, c.id)
		if !ok {
			t.Fatalf("%s: no chat_attachment row %q — the mint did not run for this url", c.why, c.id)
		}
		if mime != "text/uri-list" {
			t.Errorf("%s: minted blob %s has mime %q, want \"text/uri-list\" — the mime is the "+
				"ONLY thing that identifies a minted url blob, and the Down collector deletes by it",
				c.why, c.id, mime)
		}
		if !bytes.Equal(data, []byte(c.url)) {
			t.Errorf("%s: minted blob %s holds %q, want the url's bytes exactly: %q",
				c.why, c.id, string(data), c.url)
		}
	}

	// Dedupe crosses the two tables: the mint UNIONs task_artifact and
	// task_artifact_history, so a history row on the shared url reuses the live
	// rows' blob rather than minting a second copy of the same bytes.
	var histShared string
	if err := db.QueryRow(
		`SELECT attachment_id FROM task_artifact_history WHERE id = 913`).Scan(&histShared); err != nil {
		t.Fatalf("read history row 913: %v", err)
	}
	if histShared != link1.attachmentID {
		t.Errorf("the history row on the shared url got blob %s, want the live rows' %s — the "+
			"mint UNIONs both tables so one url is one blob across the whole database",
			histShared, link1.attachmentID)
	}

	// Exactly one minted blob per distinct url across both tables: 3 here
	// (shared, other, history-only). Counting catches the redundant copies a
	// pairwise identity check cannot.
	var minted int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment WHERE mime = 'text/uri-list' AND id <> 'att-uploadeduri'`,
	).Scan(&minted); err != nil {
		t.Fatalf("count minted blobs: %v", err)
	}
	if minted != 3 {
		t.Errorf("the mint produced %d text/uri-list blobs, want 3 — the fixture holds 6 link "+
			"references (3 live rows + 1 history row, over 3 distinct urls); any other count "+
			"means the mint is not one-blob-per-distinct-url", minted)
	}

	// The history-only url is minted too — it is reachable from no live row, so
	// a mint that read only task_artifact would leave that version dangling.
	var histOnly string
	if err := db.QueryRow(
		`SELECT attachment_id FROM task_artifact_history WHERE id = 58`).Scan(&histOnly); err != nil {
		t.Fatalf("read history row 58: %v", err)
	}
	data, _, ok := t92BlobBytes(t, db, histOnly)
	if !ok || !bytes.Equal(data, []byte(t92URLHistOnly)) {
		t.Errorf("the history-ONLY url did not get its own blob: id=%q bytes=%q, want %q — "+
			"the mint's UNION exists so a version nobody's live row points at still resolves",
			histOnly, string(data), t92URLHistOnly)
	}
}

// ── ④ attachment_id is never blank ───────────────────────────────────────────

// TestMigration00086LeavesNoBlankAttachmentID is claim ④. After 00086 `url` as a
// column is GONE, so a row with a blank attachment_id points at nothing at all —
// and the new schema deliberately ships NO `CHECK (attachment_id <> '')`, which
// makes this test the only thing that would notice.
func TestMigration00086LeavesNoBlankAttachmentID(t *testing.T) {
	db := t92World(t)
	t92Up(t, db)

	for _, table := range []string{"task_artifact", "task_artifact_history"} {
		var blank, total int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM ` + table + ` WHERE attachment_id = '' OR attachment_id IS NULL`).Scan(&blank); err != nil {
			t.Fatalf("count blank attachment_id in %s: %v", table, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&total); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if total == 0 {
			t.Fatalf("%s is empty after the rebuild — this assertion would be vacuous", table)
		}
		if blank != 0 {
			t.Errorf("%s has %d rows (of %d) with a blank attachment_id. After 00086 the `url` "+
				"COLUMN is gone, so such a row points at nothing and no CHECK catches it — the "+
				"approved schema deliberately did not ask for one.", table, blank, total)
		}
	}
}

// ── ⑤ history ids are preserved, by value and by order ───────────────────────

// TestMigration00086PreservesHistoryIDs is claim ⑤. task_artifact_history.id is
// INTEGER PRIMARY KEY AUTOINCREMENT and the version list is `ORDER BY id DESC`;
// a rebuild that lets SQLite re-number produces a table that answers every query
// without error while every artifact's versions are in a different order.
func TestMigration00086PreservesHistoryIDs(t *testing.T) {
	db := t92World(t)
	t92Up(t, db)

	// Values: the exact multiset of ids the fixture seeded.
	want := []int64{}
	for _, h := range t92HistoryFixture() {
		want = append(want, h.id)
	}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })

	rows, err := db.Query(`SELECT id FROM task_artifact_history ORDER BY id`)
	if err != nil {
		t.Fatalf("read history ids: %v", err)
	}
	got := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		got = append(got, id)
	}
	rows.Close()

	if len(got) != len(want) {
		t.Fatalf("history has %d rows after the rebuild, want %d (ids got=%v want=%v)",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HISTORY IDS WERE NOT PRESERVED: got %v, want %v.\n\n"+
				"task_artifact_history.id is AUTOINCREMENT, so a rebuild that INSERTs without "+
				"naming the id column re-numbers every row from 1. The version list is "+
				"ORDER BY id DESC and nothing raises: the rows are all still there, in a "+
				"different order, under ids no other row's reference matches.", got, want)
		}
	}

	// Order: the id→row binding, not just the set of ids. A rebuild that kept
	// the id VALUES but attached them to the wrong rows passes the check above
	// and fails this one.
	for _, h := range t92HistoryFixture() {
		var artifactID, label string
		if err := db.QueryRow(
			`SELECT artifact_id, description FROM task_artifact_history WHERE id = ?`, h.id,
		).Scan(&artifactID, &label); err != nil {
			t.Fatalf("history id %d is missing after the rebuild: %v", h.id, err)
		}
		if artifactID != h.artifactID || label != h.label {
			t.Errorf("history id %d now holds artifact_id=%q description=%q, want %q / %q — "+
				"the ids survived but were re-attached to different rows, which reorders "+
				"versions just as thoroughly as re-numbering them",
				h.id, artifactID, label, h.artifactID, h.label)
		}
	}

	// The version list a reader actually issues, for the artifact that has two
	// versions: newest first, by the preserved ids.
	var newest int64
	if err := db.QueryRow(
		`SELECT id FROM task_artifact_history WHERE artifact_id = 'ta-file0001' ORDER BY id DESC LIMIT 1`,
	).Scan(&newest); err != nil {
		t.Fatalf("read newest version of ta-file0001: %v", err)
	}
	if newest != 42 {
		t.Errorf("the newest version of ta-file0001 is id %d, want 42 — the fixture inserted "+
			"42 BEFORE 7, so a re-numbering rebuild hands back the older row as the newest", newest)
	}
}

// ── ⑥ both indexes come back ─────────────────────────────────────────────────

// TestMigration00086RebuildsBothIndexes is claim ⑥ and the migration's own named
// trap: "DROP TABLE takes idx_task_artifact_task and
// idx_task_artifact_history_artifact with it and RENAME does not bring them
// back. Miss one and nothing raises: the queries keep answering, just by scan."
// idx_task_artifact_task was already asserted elsewhere in this package;
// idx_task_artifact_history_artifact was asserted NOWHERE.
func TestMigration00086RebuildsBothIndexes(t *testing.T) {
	db := t92World(t)
	t92Up(t, db)

	for _, idx := range []struct{ name, table, why string }{
		{"idx_task_artifact_task", "task_artifact",
			"every artifact list is a lookup by task_id"},
		{"idx_task_artifact_history_artifact", "task_artifact_history",
			"the version list is (artifact_id, id DESC) and this index is what makes it ordered without a sort"},
	} {
		var onTable string
		err := db.QueryRow(
			`SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = ?`, idx.name).Scan(&onTable)
		if err == sql.ErrNoRows {
			t.Errorf("INDEX %s IS GONE after 00086. %s. DROP TABLE took it and RENAME did not "+
				"bring it back — and nothing raises, because the queries keep answering by "+
				"full scan.", idx.name, idx.why)
			continue
		}
		if err != nil {
			t.Fatalf("sqlite_master lookup for %s: %v", idx.name, err)
		}
		if onTable != idx.table {
			t.Errorf("index %s is attached to %q, want %q — it was rebuilt onto the wrong table",
				idx.name, onTable, idx.table)
		}
		// pragma_index_list asks the same question of the TABLE rather than of
		// the schema text, so an index that exists under the right name but is
		// not registered on the table is still caught.
		var listed int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ?`, idx.table, idx.name).Scan(&listed); err != nil {
			t.Fatalf("pragma_index_list(%s): %v", idx.table, err)
		}
		if listed != 1 {
			t.Errorf("pragma_index_list(%s) reports %d indexes named %s, want 1",
				idx.table, listed, idx.name)
		}
	}
}

// ── ⑦ Down ───────────────────────────────────────────────────────────────────

// TestMigration00086DownRestoresURLAndLabelAndCollectsMintedBlobs is claim ⑦.
// The Down block had NO execution path anywhere in the product before this test
// — ocserverd ships no `migrate down` subcommand — and it contains a DELETE over
// chat_attachment, which is the single most dangerous statement in the file.
func TestMigration00086DownRestoresURLAndLabelAndCollectsMintedBlobs(t *testing.T) {
	db := t92World(t)

	// Baseline: the exact chat_attachment population before the mint. Compared
	// as a SET of ids, not a count, so a Down that deletes one blob and leaves
	// another behind cannot cancel out to the right number.
	blobIDs := func() []string {
		rows, err := db.Query(`SELECT id FROM chat_attachment ORDER BY id`)
		if err != nil {
			t.Fatalf("read chat_attachment ids: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan blob id: %v", err)
			}
			out = append(out, id)
		}
		return out
	}
	before := blobIDs()
	if len(before) == 0 {
		t.Fatal("no blobs before Up — the collection assertion below would be vacuous")
	}

	t92Up(t, db)

	// Non-vacuity: the mint really did add blobs, so "the population came back"
	// is a statement about a collector that ran, not about one that had nothing
	// to do.
	if mid := blobIDs(); len(mid) != len(before)+3 {
		t.Fatalf("after Up chat_attachment holds %d blobs, want %d (%d seeded + 3 minted) — "+
			"the Down assertions below would be measuring the wrong thing",
			len(mid), len(before)+3, len(before))
	}

	if err := goose.DownTo(db, "migrations", migration00086PriorVersion); err != nil {
		t.Fatalf("goose down to %d: %v", migration00086PriorVersion, err)
	}

	// ⑦a the minted blobs are collected — the population is byte-identical to
	// the pre-Up one, INCLUDING the uploaded text/uri-list blob no link points
	// at, which the collector must not sweep up.
	after := blobIDs()
	if strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("DOWN DID NOT RESTORE THE BLOB POPULATION.\n after (%d): %v\nbefore (%d): %v\n\n"+
			"Down's DELETE is scoped by mime = 'text/uri-list' AND by referent. Leaving a "+
			"minted blob behind orphans bytes nothing can reach; deleting one it did not mint "+
			"destroys an upload — note the fixture's 'att-uploadeduri', a .uri file a person "+
			"uploaded as kind='file', which a mime-only DELETE would take with it.",
			len(after), after, len(before), before)
	}

	// ⑦b `url` and `label` are back, per row, byte for byte.
	for _, r := range t92ArtifactFixture() {
		var url, label, attachmentID string
		if err := db.QueryRow(
			`SELECT url, label, attachment_id FROM task_artifact WHERE id = ?`, r.id,
		).Scan(&url, &label, &attachmentID); err != nil {
			t.Fatalf("row %s after Down: %v", r.id, err)
		}
		if url != r.url {
			t.Errorf("%s (%s): Down restored url %q, want %q — a link's url comes back by "+
				"reading its blob's bytes, so a mismatch means the round trip through "+
				"CAST(data AS TEXT) is not an identity", r.id, r.kind, url, r.url)
		}
		if label != r.label {
			t.Errorf("%s (%s): Down restored label %q, want %q — label is rebuilt from "+
				"`description`, which for every row this migration wrote IS the original label",
				r.id, r.kind, label, r.label)
		}
		if attachmentID != r.attachmentID {
			t.Errorf("%s (%s): Down restored attachment_id %q, want %q (a link's goes back to "+
				"blank; a file/image keeps its blob)", r.id, r.kind, attachmentID, r.attachmentID)
		}
	}

	// ⑦c the same for the history table, ids still intact after the round trip.
	for _, h := range t92HistoryFixture() {
		var artifactID, url, label, attachmentID string
		if err := db.QueryRow(
			`SELECT artifact_id, url, label, attachment_id FROM task_artifact_history WHERE id = ?`, h.id,
		).Scan(&artifactID, &url, &label, &attachmentID); err != nil {
			t.Fatalf("history id %d after Down: %v — the id did not survive the round trip", h.id, err)
		}
		if artifactID != h.artifactID || url != h.url || label != h.label || attachmentID != h.attachmentID {
			t.Errorf("history id %d after Down: artifact_id=%q url=%q label=%q attachment_id=%q, "+
				"want %q / %q / %q / %q",
				h.id, artifactID, url, label, attachmentID,
				h.artifactID, h.url, h.label, h.attachmentID)
		}
	}

	// ⑦d both indexes exist on the RESTORED tables too. Down does the same
	// DROP TABLE + RENAME dance, so it walks into the identical trap.
	for _, idx := range []string{"idx_task_artifact_task", "idx_task_artifact_history_artifact"} {
		var name string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&name); err != nil {
			t.Errorf("index %s is missing after Down: %v — the rollback rebuilds the tables the "+
				"same way Up does and loses the indexes the same way", idx, err)
		}
	}
}
