package main

// T-33 — the write seam. Every test here asks the same question in a different
// place: after this call, is there an entry that can be REACHED and an original
// that can be READ, or only a row that looks finished?

import (
	"errors"
	"strings"
	"testing"
)

func t33Write() LoreWrite {
	return LoreWrite{
		Label:        "boot context assembly",
		Symptoms:     "two blocks disagree about the same fact",
		Short:        "the fold happens in one place",
		Falsify:      "a second assembler appears",
		Instance:     "T-33 slot 3",
		ResidualRisk: "says nothing about who may call the fold",
		Origin:       "agent:O-197",
		Subjects:     []string{"repo:officraft"},
		Actions:      []string{"read-code"},
		ActorID:      "ow-e27260b9ed05",
	}
}

func t33Create(t *testing.T, d *DAL, w LoreWrite) LoreWriteResult {
	t.Helper()
	got, err := d.CreateLoreEntry(w, 1000)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return got
}

func t33CountEntries(t *testing.T, d *DAL) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_entry`).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

// The happy path, checked on every table the write is supposed to touch — not
// on the receipt it returned. A receipt that agrees with itself proves nothing.
func TestLoreCreateWritesEntrySubjectsActionsAndOriginal(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	got := t33Create(t, d, t33Write())

	entry := t33Get(t, d, got.EntryID)
	if entry == nil {
		t.Fatal("the entry the receipt names is not in the table")
	}
	if entry.Short != "the fold happens in one place" || entry.Status != "active" {
		t.Fatalf("entry came back wrong: %+v", *entry)
	}
	subjects, err := d.ListLoreSubjects(got.EntryID)
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subjects) != 1 || subjects[0] != "e-repo" {
		t.Fatalf("subject rows: got %v, want [e-repo]", subjects)
	}
	actions, err := d.ListLoreActions(got.EntryID)
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	if len(actions) != 1 || actions[0] != "read-code" {
		t.Fatalf("action rows: got %v, want [read-code]", actions)
	}

	// 🔴 THE ORIGINAL. This is the assertion the whole file exists for: an entry
	// written with no revision behind it is invisible in every count and every
	// context, and only shows up when somebody goes looking for the original.
	rev, err := d.LatestLoreRevision(got.EntryID)
	if err != nil {
		t.Fatalf("latest revision: %v", err)
	}
	if rev == nil {
		t.Fatal("the entry was written with NO L0 original")
	}
	if rev.SHA256 != got.SHA256 || rev.SHA256 != loreSHA256(rev.Body) {
		t.Fatalf("the stored digest does not hash the stored body: %q", rev.SHA256)
	}
	if !strings.Contains(rev.Body, "the fold happens in one place") ||
		!strings.Contains(rev.Body, "says nothing about who may call the fold") {
		t.Fatalf("the original does not carry the body it was written from:\n%s", rev.Body)
	}
	if rev.ActorID != "ow-e27260b9ed05" {
		t.Fatalf("revision actor: got %q", rev.ActorID)
	}

	var metaActor string
	if err := d.rdb.QueryRow(
		`SELECT source_actor_id FROM lore_meta WHERE entry_id = ?`, got.EntryID).Scan(&metaActor); err != nil {
		t.Fatalf("lore_meta row: %v", err)
	}
	if metaActor != "ow-e27260b9ed05" {
		t.Fatalf("lore_meta actor: got %q", metaActor)
	}
}

// A blank field renders as a NAMED empty section rather than disappearing.
// Skipping blanks would hash the same bytes for "never written" and "deleted",
// which is the collapse this ticket exists to prevent.
func TestLoreRevisionBodyNamesEveryFieldEvenWhenBlank(t *testing.T) {
	body := loreRevisionBody(LoreEntry{Short: "only this one is set"})
	for _, name := range []string{"label", "symptoms", "short", "falsify", "instance", "residual_risk"} {
		if !strings.Contains(body, name+":\n") {
			t.Fatalf("the rendered original drops the %q section:\n%s", name, body)
		}
	}
	if loreSHA256(body) == loreSHA256(loreRevisionBody(LoreEntry{})) {
		t.Fatal("an entry with a body and an entirely empty one hash the same")
	}
}

// 四格空白都被拒，而且每一格是它自己的具名錯誤——沒有哪一格會被折進別格的錯誤裡，
// 不然寫的人只會知道「被擋了」而不知道要補哪一格。
func TestLoreCreateRefusesTheFourFieldsWithoutWhichNothingIsReachable(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	for _, tc := range []struct {
		name   string
		mangle func(*LoreWrite)
		want   error
	}{
		{"blank symptoms", func(w *LoreWrite) { w.Symptoms = "   " }, ErrLoreSymptomsBlank},
		{"blank short", func(w *LoreWrite) { w.Short = "" }, ErrLoreShortBlank},
		{"blank falsify", func(w *LoreWrite) { w.Falsify = "" }, ErrLoreFalsifyBlank},
		{"whitespace falsify", func(w *LoreWrite) { w.Falsify = "  \t " }, ErrLoreFalsifyBlank},
		{"blank instance", func(w *LoreWrite) { w.Instance = "" }, ErrLoreInstanceBlank},
		{"whitespace instance", func(w *LoreWrite) { w.Instance = "   " }, ErrLoreInstanceBlank},
	} {
		w := t33Write()
		tc.mangle(&w)
		_, err := d.CreateLoreEntry(w, 1000)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("a refused write left %d entries behind", n)
	}

	// 兩格都給的寫入照樣成功，而且不是 degraded。少了這一半，一個把每一筆寫入都
	// 拒掉的實作也會讓上面全綠。
	got := t33Create(t, d, t33Write())
	entry := t33Get(t, d, got.EntryID)
	if entry == nil || entry.IsDegraded() {
		t.Fatalf("a complete entry must land and must not be degraded: %+v", entry)
	}
}

// 🔴 新規則只擋新寫入，不能回頭把舊資料變成讀不到的。2026-09-02 之前寫下的條目
// 兩格可以都是空的；它們必須照樣讀得出來，而 `degraded` 必須照樣是 true——那個
// 旗標是唯一看得見它們的東西。
func TestLoreEntriesWrittenBeforeTheRulingStillReadBackAsDegraded(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	legacy := LoreEntry{
		ID: "lore-legacy-01", Label: "口號", Symptoms: "x", Short: "y",
		Origin: "agent:O-197", CreatedTS: 1000, UpdatedTS: 1000,
	}
	if err := d.PutLoreEntry(legacy); err != nil {
		t.Fatalf("seed a pre-ruling entry: %v", err)
	}
	got := t33Get(t, d, legacy.ID)
	if got == nil {
		t.Fatal("an entry written before the ruling stopped being readable")
	}
	if !got.IsDegraded() {
		t.Fatalf("the only thing that makes a pre-ruling entry visible stopped firing: %+v", got)
	}
}

// A subject key nobody has ever used mints a PENDING entity and says so.
// Pending is the review queue, so the new subject must NOT reach the boot
// directory until somebody approves it.
func TestLoreCreateMintsUnknownSubjectAsPendingAndReportsIt(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = []string{"repo:offcraft-typo"}

	got := t33Create(t, d, w)
	if len(got.Minted) != 1 || got.Minted[0].Canonical != "repo:offcraft-typo" {
		t.Fatalf("the mint was not reported back: %+v", got.Minted)
	}

	roster, err := d.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster) != 0 {
		t.Fatalf("an unapproved subject reached the boot directory: %+v", roster)
	}

	// Discrimination: approve it and the SAME query must now return it. Without
	// this half, a roster that is broken in some other way would pass above.
	if _, err := d.wdb.Exec(`UPDATE entity SET pending = 0 WHERE id = ?`, got.Minted[0].EntityID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	roster, err = d.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster after approval: %v", err)
	}
	if len(roster) != 1 || roster[0].Canonical != "repo:offcraft-typo" || roster[0].Entries != 1 {
		t.Fatalf("approved subject did not appear: %+v", roster)
	}
}

// An unapproved type prefix is refused BY NAME, and the whole write is rolled
// back — no orphan entry, no orphan revision.
func TestLoreCreateRefusesUnknownSubjectTypeAndWritesNothing(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = []string{"repo:officraft", "vendor:acme"}

	_, err := d.CreateLoreEntry(w, 1000)
	if !errors.Is(err, ErrLoreSubjectUnknownType) {
		t.Fatalf("unknown subject type: got %v", err)
	}
	if !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("the refusal does not name the prefix: %v", err)
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("the refused write left %d entries behind", n)
	}
	var revs int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_revision`).Scan(&revs); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revs != 0 {
		t.Fatalf("the refused write left %d originals behind", revs)
	}
}

// A malformed subject key is refused rather than minted. Minting `officraft`
// with no type would put a key into the ontology that no reader can parse.
func TestLoreCreateRefusesMalformedSubjectKey(t *testing.T) {
	d := newTestDAL(t)
	for _, bad := range []string{"officraft", "repo:", ":officraft", "   "} {
		w := t33Write()
		w.Subjects = []string{bad}
		if _, err := d.CreateLoreEntry(w, 1000); err == nil {
			t.Fatalf("subject %q was accepted", bad)
		}
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("refused writes left %d entries behind", n)
	}
}

// Filing against a merged-away subject follows the merge to the survivor.
// Filing against the merged-away row itself would produce an entry the boot
// directory can never mention, because that predicate hides merged rows.
func TestLoreCreateFilesAgainstTheSurvivorOfAMerge(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-old", "repo", "repo:oldname")
	t33Entity(t, d, "e-new", "repo", "repo:officraft")
	if _, err := d.wdb.Exec(`UPDATE entity SET merged_into = 'e-new' WHERE id = 'e-old'`); err != nil {
		t.Fatalf("merge: %v", err)
	}

	w := t33Write()
	w.Subjects = []string{"repo:oldname"}
	got := t33Create(t, d, w)
	if len(got.SubjectIDs) != 1 || got.SubjectIDs[0] != "e-new" {
		t.Fatalf("subject did not follow the merge: %v", got.SubjectIDs)
	}
	if len(got.Minted) != 0 {
		t.Fatalf("a known subject was minted again: %+v", got.Minted)
	}
}

// A merge cycle is refused, not walked. Nothing in the schema forbids A→B→A.
func TestLoreCreateRefusesAMergeChainThatDoesNotEnd(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-a", "repo", "repo:a")
	t33Entity(t, d, "e-b", "repo", "repo:b")
	if _, err := d.wdb.Exec(
		`UPDATE entity SET merged_into = CASE id WHEN 'e-a' THEN 'e-b' ELSE 'e-a' END`); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	w := t33Write()
	w.Subjects = []string{"repo:a"}
	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreEntityMergeCycle) {
		t.Fatalf("merge cycle: got %v", err)
	}
}

// An alias resolves onto the same subject, and the duplicate files ONE row.
func TestLoreCreateResolvesAliasesAndDeduplicatesSubjects(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	if _, err := d.wdb.Exec(
		`INSERT INTO entity_alias (alias, entity_id) VALUES ('repo:oc', 'e-repo')`); err != nil {
		t.Fatalf("alias: %v", err)
	}
	w := t33Write()
	w.Subjects = []string{"repo:officraft", "repo:oc", "repo:officraft"}

	got := t33Create(t, d, w)
	if len(got.SubjectIDs) != 1 || got.SubjectIDs[0] != "e-repo" {
		t.Fatalf("subjects: got %v, want one e-repo", got.SubjectIDs)
	}
	subjects, err := d.ListLoreSubjects(got.EntryID)
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subjects) != 1 {
		t.Fatalf("filed %d subject rows for one subject", len(subjects))
	}
}

// Superseding re-statuses the old entry AND leaves a journal row. The pointer
// alone would say what replaced it and never who decided that, or when.
func TestLoreCreateSupersedesTheEntryItReplacesAndJournalsIt(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	old := t33Create(t, d, t33Write())

	w := t33Write()
	w.Supersedes = old.EntryID
	got := t33Create(t, d, w)

	prev := t33Get(t, d, old.EntryID)
	if prev == nil || prev.Status != "superseded" {
		t.Fatalf("the replaced entry kept status %+v", prev)
	}
	events, err := d.ListLoreGovernanceEvents(old.EntryID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != LoreGovSupersede ||
		events[0].ReplacedBy != got.EntryID || events[0].ActorID != "ow-e27260b9ed05" {
		t.Fatalf("the supersede left no usable journal row: %+v", events)
	}
}

// Superseding an id that names nothing rolls the WHOLE write back. A pointer
// into empty space is a dead end that looks like a trail.
func TestLoreCreateRefusesToSupersedeAnEntryThatDoesNotExist(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	w := t33Write()
	w.Supersedes = "lore-nope"

	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("supersedes a ghost: got %v", err)
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("the refused write left %d entries behind", n)
	}
}

// The label cap is a REFUSAL, never a truncation: a name that changes silently
// breaks whatever was pointing at it.
func TestLoreCreateRefusesAnOverlongLabelRatherThanTrimmingIt(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	w := t33Write()
	w.Label = strings.Repeat("字", loreLabelMaxRunes+1)

	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreLabelTooLong) {
		t.Fatalf("overlong label: got %v", err)
	}
	if n := t33CountEntries(t, d); n != 0 {
		t.Fatalf("the refused write left %d entries behind", n)
	}
}

// An entry with no subject is refused. It would exist, be counted, and be
// reachable by nothing — the boot directory is indexed by subject.
func TestLoreCreateRefusesAnEntryNobodyCouldEverFind(t *testing.T) {
	d := newTestDAL(t)
	w := t33Write()
	w.Subjects = nil
	if _, err := d.CreateLoreEntry(w, 1000); !errors.Is(err, ErrLoreSubjectsEmpty) {
		t.Fatalf("no subject: got %v", err)
	}
}

// A blank origin and an unapproved origin prefix are both refused — the same
// rule PutLoreEntry enforces, asserted through THIS path so the composite
// cannot quietly stop applying it.
func TestLoreCreateEnforcesTheOriginRuleOnItsOwnPath(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	blank := t33Write()
	blank.Origin = ""
	if _, err := d.CreateLoreEntry(blank, 1000); !errors.Is(err, ErrLoreOriginBlank) {
		t.Fatalf("blank origin: got %v", err)
	}
	unknown := t33Write()
	unknown.Origin = "vendor:acme"
	if _, err := d.CreateLoreEntry(unknown, 1000); !errors.Is(err, ErrLoreOriginUnknownType) {
		t.Fatalf("unknown origin type: got %v", err)
	}
}

// 🔴 THIS TEST LOOKS LIKE A TAUTOLOGY TODAY AND THAT IS THE POINT.
//
// api_lore_read.go carries an early return that turns an unparsable revision id
// into a 404. Mutating that branch away leaves every test green — measured —
// because strconv.ParseInt answers 0 on failure and the scoped lookup then finds
// nothing, so the same 404 comes out of the other door. The guard is therefore
// not load-bearing, and it is only SAFE to say that because of ONE unstated
// premise: no revision ever carries id 0.
//
// That premise is what this test pins. Nothing else does: it is a property of
// `INTEGER PRIMARY KEY AUTOINCREMENT`, and a comment describing it would not
// raise its hand on the day somebody changes how revision ids are allocated.
//
// ⇒ The value of this test is not today, when it cannot fail. It is the day it
// DOES fail — because on that day the meaning of the failure is "the read
// route's parse guard has just started carrying load; go and test it properly".
func TestLoreRevisionIdsNeverStartAtZero(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")

	first := t33Create(t, d, t33Write())
	if first.RevisionID == 0 {
		t.Fatal("a revision was allocated id 0 — the read route's parse guard is now " +
			"load-bearing (a garbage id parses to 0 and would ADDRESS this row), and " +
			"api_lore_read.go says in as many words that it is not")
	}
	// Not merely non-zero: the ids come from AUTOINCREMENT, so the first one is
	// 1. Asserting the actual start makes a change of allocation scheme visible
	// rather than merely a change that happens to skip zero.
	if first.RevisionID != 1 {
		t.Fatalf("the first revision has id %d, want 1 — revision ids are no longer "+
			"plain AUTOINCREMENT, so anything reasoning about their values (see "+
			"api_lore_read.go) has to be re-read", first.RevisionID)
	}
	second := t33Create(t, d, t33Write())
	if second.RevisionID <= first.RevisionID {
		t.Fatalf("revision ids are not increasing: %d then %d", first.RevisionID, second.RevisionID)
	}
}
