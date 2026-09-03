package main

// T-33 — 對象審核 at the DAL: the review queue, 核可, 合併.

import (
	"errors"
	"strings"
	"testing"
)

// t33Mint writes one entry through the REAL write seam so its subjects arrive
// as genuinely minted pending entities. Seeding `entity` rows by hand would
// test this file against a fixture rather than against the path that fills the
// queue in production.
func t33Mint(t *testing.T, d *DAL, subjects ...string) LoreWriteResult {
	t.Helper()
	got, err := d.CreateLoreEntry(LoreWrite{
		Trigger:  "the queue is full and nothing reads it",
		Content:  "the pending column had no exit",
		Origin:   "agent:O-197",
		Subjects: subjects,
		ActorID:  "m-writer",
	}, 100)
	if err != nil {
		t.Fatalf("write against %v: %v", subjects, err)
	}
	return got
}

func t33PendingByKey(t *testing.T, d *DAL, canonical string) LorePendingEntity {
	t.Helper()
	rows, err := d.ListPendingLoreEntities()
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, row := range rows {
		if row.Canonical == canonical {
			return row
		}
	}
	t.Fatalf("no pending entity carries %q; queue = %+v", canonical, rows)
	return LorePendingEntity{}
}

func t33QueueKeys(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.ListPendingLoreEntities()
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Canonical)
	}
	return out
}

func t33HasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// TestListPendingLoreEntitiesReturnsOnlyPendingWithCountedEntries pins the two
// halves that make the queue worth reading: WHICH rows it carries, and whether
// the number beside each one was counted or invented.
//
// The approved subject is the NEGATIVE CONTROL and it is not decoration: a
// query that dropped the `pending = 1` clause would still answer with rows, and
// every other assertion here would pass.
func TestListPendingLoreEntitiesReturnsOnlyPendingWithCountedEntries(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-approved", "repo", "repo:officraft")

	// Two entries under one minted subject, one under another, and one under
	// the already-approved subject — so a count that returned "1" for
	// everything, or the total row count, is visibly wrong.
	t33Mint(t, d, "repo:offcraft", "repo:officraft")
	t33Mint(t, d, "repo:offcraft")
	t33Mint(t, d, "agent:Kyle")

	keys := t33QueueKeys(t, d)
	if len(keys) != 2 || !t33HasKey(keys, "repo:offcraft") || !t33HasKey(keys, "agent:Kyle") {
		t.Fatalf("queue = %v, want exactly the two minted keys", keys)
	}
	if t33HasKey(keys, "repo:officraft") {
		t.Fatal("the queue carried an ALREADY-APPROVED subject — the pending filter is gone, " +
			"and a reviewer would be asked to approve names that are already in the ontology")
	}

	typo := t33PendingByKey(t, d, "repo:offcraft")
	if typo.Entries != 2 {
		t.Fatalf("repo:offcraft entries = %d, want 2 counted from lore_subject", typo.Entries)
	}
	if typo.Type != "repo" || typo.Name != "offcraft" {
		t.Fatalf("repo:offcraft split = %q/%q, want repo/offcraft", typo.Type, typo.Name)
	}
	if typo.CreatedTS != 100 {
		t.Fatalf("repo:offcraft created_ts = %v, want the mint time", typo.CreatedTS)
	}
	if kyle := t33PendingByKey(t, d, "agent:Kyle"); kyle.Entries != 1 {
		t.Fatalf("agent:Kyle entries = %d, want 1", kyle.Entries)
	}
}

// TestListPendingLoreEntitiesCountsWithTheRetiredPredicate holds the count to
// the SAME predicate the boot directory and search use. A count that included
// retired entries would tell a reviewer a name is carrying knowledge it will
// not serve once approved.
func TestListPendingLoreEntitiesCountsWithTheRetiredPredicate(t *testing.T) {
	d := newTestDAL(t)
	kept := t33Mint(t, d, "repo:offcraft")
	gone := t33Mint(t, d, "repo:offcraft")
	if err := d.RetireLoreEntry(gone.EntryID, LoreRetireExpired, "m-writer", "", false, 200); err != nil {
		t.Fatalf("retire: %v", err)
	}

	if got := t33PendingByKey(t, d, "repo:offcraft").Entries; got != 1 {
		t.Fatalf("entries = %d, want 1 — only %s is still retrievable", got, kept.EntryID)
	}
}

// TestListPendingLoreEntitiesKeepsAnUnusedMintedName is the row a JOIN would
// silently drop, and it is the single most interesting line in the queue: a key
// that was minted once and never used again is a typo whose writer corrected
// itself, so it must be visible AND countable as zero.
func TestListPendingLoreEntitiesKeepsAnUnusedMintedName(t *testing.T) {
	d := newTestDAL(t)
	got := t33Mint(t, d, "repo:offcraft")
	if _, err := d.wdb.Exec(`DELETE FROM lore_subject WHERE entry_id = ?`, got.EntryID); err != nil {
		t.Fatalf("unfile: %v", err)
	}

	row := t33PendingByKey(t, d, "repo:offcraft")
	if row.Entries != 0 {
		t.Fatalf("entries = %d, want 0", row.Entries)
	}
}

// TestApproveLoreEntityClearsPendingAndJournalsIt is the act plus its record —
// they are one transaction, so a test that checked only the column would pass
// on a version that published a name nobody can be shown to have approved.
func TestApproveLoreEntityClearsPendingAndJournalsIt(t *testing.T) {
	d := newTestDAL(t)
	t33Mint(t, d, "repo:offcraft")
	id := t33PendingByKey(t, d, "repo:offcraft").ID

	if err := d.ApproveLoreEntity(id, "m-mira", "checked against the repo list", 300); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if keys := t33QueueKeys(t, d); t33HasKey(keys, "repo:offcraft") {
		t.Fatalf("the approved entity is still in the queue: %v", keys)
	}

	entity, err := d.GetLoreEntity(id)
	if err != nil || entity == nil {
		t.Fatalf("get entity: %v %v", entity, err)
	}
	if entity.Pending {
		t.Fatal("the entity is still pending after a successful approve")
	}
	event, err := d.LatestLoreGovernanceEvent(id)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	if event == nil || event.Kind != LoreGovEntityApprove ||
		event.ActorID != "m-mira" || event.Reason != "checked against the repo list" {
		t.Fatalf("journal row = %+v", event)
	}

	// A second approval is refused rather than treated as a no-op: the caller
	// believes the entity is in a state it is not.
	if err := d.ApproveLoreEntity(id, "m-mira", "", 400); !errors.Is(err, ErrLoreEntityNotPending) {
		t.Fatalf("re-approve error = %v, want ErrLoreEntityNotPending", err)
	}
	if err := d.ApproveLoreEntity("en-nothing", "m-mira", "", 400); !errors.Is(err, ErrLoreEntityUnknown) {
		t.Fatalf("approve unknown error = %v, want ErrLoreEntityUnknown", err)
	}
}

// TestMergeLoreEntityFollowsTheSourceOntoTheSurvivor is the merge's whole
// claim, and it is asserted through the RESOLVER rather than by reading the two
// columns back: `merged_into` and the alias row are only worth anything if the
// paths that resolve subject keys honour them.
func TestMergeLoreEntityFollowsTheSourceOntoTheSurvivor(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-real", "repo", "repo:officraft")
	t33Mint(t, d, "repo:offcraft")
	src := t33PendingByKey(t, d, "repo:offcraft").ID

	if err := d.MergeLoreEntity(src, "en-real", "m-mira", "same repo, one letter short", 300); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if keys := t33QueueKeys(t, d); t33HasKey(keys, "repo:offcraft") {
		t.Fatalf("the merged entity is still in the queue: %v", keys)
	}

	entity, err := d.GetLoreEntity(src)
	if err != nil || entity == nil {
		t.Fatalf("get source: %v %v", entity, err)
	}
	if entity.MergedInto != "en-real" || entity.Pending {
		t.Fatalf("source after merge = %+v", entity)
	}

	// ① the old key is no longer MINTED a second time and ② it now files onto
	// the survivor — both through the write seam's own resolver, which is the
	// thing production runs.
	after := t33Mint(t, d, "repo:offcraft")
	if len(after.Minted) != 0 {
		t.Fatalf("the old key minted a NEW pending entity after the merge: %+v — the alias "+
			"row is missing and the review bought nothing", after.Minted)
	}
	if len(after.SubjectIDs) != 1 || after.SubjectIDs[0] != "en-real" {
		t.Fatalf("subject ids after merge = %v, want the survivor", after.SubjectIDs)
	}

	// ③ and RETRIEVAL follows the same way. The write seam and the search seam
	// resolve subject keys through two separate functions, so honouring the
	// merge on one side proves nothing about the other.
	got, err := d.SearchLore(LoreSearch{SubjectKey: "repo:offcraft"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !got.SubjectResolved {
		t.Fatalf("the merged-away key came back unresolved: %+v — a reader asking for the "+
			"old name is told it does not exist rather than being sent to the survivor", got)
	}
	if len(got.Hits) != 2 {
		t.Fatalf("search hits = %d, want the two entries now filed against the survivor", len(got.Hits))
	}

	event, err := d.LatestLoreGovernanceEvent(src)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	if event == nil || event.Kind != LoreGovEntityMerge || event.ReplacedBy != "en-real" {
		t.Fatalf("journal row = %+v", event)
	}
}

// TestMergeLoreEntityRefusesATargetNoReaderCouldFollow is the refusal set, and
// every arm here is a merge that would otherwise report success while parking
// the source behind a name the boot directory ALSO hides.
func TestMergeLoreEntityRefusesATargetNoReaderCouldFollow(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-real", "repo", "repo:officraft")
	t33Mint(t, d, "repo:offcraft", "agent:Kylo")
	src := t33PendingByKey(t, d, "repo:offcraft").ID
	stillPending := t33PendingByKey(t, d, "agent:Kylo").ID

	for _, tc := range []struct {
		name string
		into string
		want error
	}{
		{"a target that does not exist", "en-nothing", ErrLoreEntityUnknown},
		{"a blank target", "", ErrLoreEntityUnknown},
		{"a target that is itself pending", stillPending, ErrLoreEntityTargetPending},
		{"the source itself", src, ErrLoreEntityMergeSelf},
	} {
		if err := d.MergeLoreEntity(src, tc.into, "m-mira", "", 300); !errors.Is(err, tc.want) {
			t.Fatalf("merge into %s: error = %v, want %v", tc.name, err, tc.want)
		}
		// A refused merge must leave NOTHING behind: a source that was quietly
		// un-pended by a rolled-back attempt would vanish from the queue while
		// still belonging to nobody.
		if keys := t33QueueKeys(t, d); !t33HasKey(keys, "repo:offcraft") {
			t.Fatalf("merge into %s dropped the source out of the queue: %v", tc.name, keys)
		}
	}

	// A target that has itself been merged away: the caller named a subject to
	// keep, and quietly redirecting onto ITS survivor would keep a different one.
	if err := d.MergeLoreEntity(stillPending, "en-real", "m-mira", "", 300); err != nil {
		t.Fatalf("seed merge: %v", err)
	}
	if err := d.MergeLoreEntity(src, stillPending, "m-mira", "", 400); !errors.Is(err, ErrLoreEntityTargetMerged) {
		t.Fatalf("merge into a merged-away target: error = %v, want ErrLoreEntityTargetMerged", err)
	}

	// And an already-merged SOURCE cannot be merged again — it is not awaiting
	// review any more.
	if err := d.MergeLoreEntity(stillPending, "en-real", "m-mira", "", 400); !errors.Is(err, ErrLoreEntityNotPending) {
		t.Fatalf("re-merge error = %v, want ErrLoreEntityNotPending", err)
	}
}

// TestApprovedLoreEntityReachesTheBootSubjectDirectory is why approving is
// worth doing at all: until it happens the boot directory filters the subject
// out, so every entry filed against it is unreachable by subject at wake.
func TestApprovedLoreEntityReachesTheBootSubjectDirectory(t *testing.T) {
	d := newTestDAL(t)
	t33Mint(t, d, "repo:offcraft")
	id := t33PendingByKey(t, d, "repo:offcraft").ID

	before, err := d.ListLoreSubjectRoster("m-any")
	if err != nil {
		t.Fatalf("roster before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("a PENDING subject reached the boot directory: %+v", before)
	}

	if err := d.ApproveLoreEntity(id, "m-mira", "", 300); err != nil {
		t.Fatalf("approve: %v", err)
	}
	after, err := d.ListLoreSubjectRoster("m-any")
	if err != nil {
		t.Fatalf("roster after: %v", err)
	}
	if len(after) != 1 || after[0].Canonical != "repo:offcraft" || after[0].Entries != 1 {
		t.Fatalf("boot directory after approve = %+v", after)
	}
}

// ── the review packet (round 2) ──────────────────────────────────────────────

// TestPendingLoreEntitySuggestsMergeOnlyOnAnExactFold is the owner's rule as
// three cases, and the middle one — the blank — is the one that matters most:
// 「算不出明確結論時要回空字串」. A version that filled it with a plausible guess
// would pass the other two.
func TestPendingLoreEntitySuggestsMergeOnlyOnAnExactFold(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-real", "repo", "repo:officraft")
	t33Entity(t, d, "en-agent", "agent", "agent:ocwarden")

	// ① identical once case / width / `_`-`-` are folded ⇒ merge, named target.
	// ② one edit away from an existing name ⇒ evidence, NO suggestion.
	// ③ nothing resembles it ⇒ approve.
	t33Mint(t, d, "repo:OffiCraft", "agent:ocwardn", "human:Seth")

	exact := t33PendingByKey(t, d, "repo:OffiCraft")
	if exact.Suggestion != LoreSuggestMerge || exact.MergeTarget != "en-real" {
		t.Fatalf("repo:OffiCraft suggestion = %q → %q, want merge → en-real",
			exact.Suggestion, exact.MergeTarget)
	}
	if len(exact.Similar) != 1 || exact.Similar[0].Reason != LoreSimilarSameNormalized ||
		exact.Similar[0].Canonical != "repo:officraft" {
		t.Fatalf("repo:OffiCraft similar = %+v", exact.Similar)
	}

	fuzzy := t33PendingByKey(t, d, "agent:ocwardn")
	if fuzzy.Suggestion != "" || fuzzy.MergeTarget != "" {
		t.Fatalf("agent:ocwardn suggestion = %q → %q, want the EMPTY string — one edit apart "+
			"is how a typo looks AND how two different names look, so it is evidence for "+
			"the reviewer and never a verdict", fuzzy.Suggestion, fuzzy.MergeTarget)
	}
	if len(fuzzy.Similar) != 1 || fuzzy.Similar[0].Canonical != "agent:ocwarden" ||
		fuzzy.Similar[0].Reason != LoreSimilarEditDistance1 {
		t.Fatalf("agent:ocwardn similar = %+v, want the fuzzy candidate reported anyway — "+
			"withholding the verdict must not mean withholding the evidence", fuzzy.Similar)
	}

	alone := t33PendingByKey(t, d, "human:Seth")
	if alone.Suggestion != LoreSuggestApprove || alone.MergeTarget != "" {
		t.Fatalf("human:Seth suggestion = %q → %q, want approve", alone.Suggestion, alone.MergeTarget)
	}
	if len(alone.Similar) != 0 {
		t.Fatalf("human:Seth similar = %+v, want none", alone.Similar)
	}
}

// TestPendingLoreEntityWithholdsASuggestionOnTwoExactCandidates is the case
// `canonical`'s uniqueness does NOT rule out: two DIFFERENT existing keys can
// fold onto the same string, and picking the first would be a coin toss served
// as a recommendation.
func TestPendingLoreEntityWithholdsASuggestionOnTwoExactCandidates(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-a", "repo", "repo:offi-craft")
	t33Entity(t, d, "en-b", "repo", "repo:offi_craft")
	t33Mint(t, d, "repo:OFFI-CRAFT")

	row := t33PendingByKey(t, d, "repo:OFFI-CRAFT")
	if row.Suggestion != "" || row.MergeTarget != "" {
		t.Fatalf("suggestion = %q → %q, want empty: two existing subjects fold onto this "+
			"name and the rule cannot choose between them", row.Suggestion, row.MergeTarget)
	}
	if len(row.Similar) != 2 {
		t.Fatalf("similar = %+v, want both candidates shown", row.Similar)
	}
}

// TestPendingLoreEntityDoesNotCompareAcrossTypes holds the comparison to one
// type prefix, which is 00066's own ruling: 「Kyle being both the canonical of
// agent:Kyle and an alias of human:KyleHsia is CORRECT, not a data error」.
// Offering that as a merge candidate would push a reviewer to fold together two
// things the schema says are two things.
func TestPendingLoreEntityDoesNotCompareAcrossTypes(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-human", "human", "human:Kyle")
	t33Mint(t, d, "agent:Kyle")

	row := t33PendingByKey(t, d, "agent:Kyle")
	if len(row.Similar) != 0 {
		t.Fatalf("similar = %+v, want none across type prefixes", row.Similar)
	}
	if row.Suggestion != LoreSuggestApprove {
		t.Fatalf("suggestion = %q, want approve", row.Suggestion)
	}
}

// TestPendingLoreEntityIgnoresSubjectsAReviewerCouldNotMergeInto keeps the
// candidate set equal to what the merge route will actually accept. A pending
// or merged-away candidate would read as homework and refuse 422 when acted on.
func TestPendingLoreEntityIgnoresSubjectsAReviewerCouldNotMergeInto(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "en-live", "repo", "repo:live")
	if _, err := d.wdb.Exec(
		`INSERT INTO entity (id, type, canonical, pending) VALUES ('en-parked','repo','repo:OffiCraft',1)`,
	); err != nil {
		t.Fatalf("seed pending candidate: %v", err)
	}
	if _, err := d.wdb.Exec(
		`INSERT INTO entity (id, type, canonical, merged_into) VALUES ('en-dead','repo','repo:offi_craft','en-live')`,
	); err != nil {
		t.Fatalf("seed merged candidate: %v", err)
	}
	t33Mint(t, d, "repo:officraft")

	row := t33PendingByKey(t, d, "repo:officraft")
	if len(row.Similar) != 0 || row.Suggestion != LoreSuggestApprove {
		t.Fatalf("similar = %+v, suggestion = %q — neither a PENDING nor a MERGED-AWAY subject "+
			"is a legal merge target, so neither may be offered as one", row.Similar, row.Suggestion)
	}
}

// TestPendingLoreEntityCarriesTheFirstEntrysContentAsASample is the 「一眼就可以
// 判斷」 half: the reviewer sees what the name is about without opening it.
//
// ⚠️ The wire field is still called `sample_short` — see LorePendingEntity.
// What it samples is 第 2 格 (`content`); `short` no longer exists.
func TestPendingLoreEntityCarriesTheFirstEntrysContentAsASample(t *testing.T) {
	d := newTestDAL(t)
	first, err := d.CreateLoreEntry(LoreWrite{
		Trigger: "t", Content: "the fold happens in exactly one place",
		Origin: "agent:O-197", Subjects: []string{"repo:offcraft"}, ActorID: "m-writer",
	}, 100)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := d.CreateLoreEntry(LoreWrite{
		Trigger: "t", Content: "a later entry that must NOT be the sample",
		Origin: "agent:O-197", Subjects: []string{"repo:offcraft"}, ActorID: "m-writer",
	}, 200); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if row := t33PendingByKey(t, d, "repo:offcraft"); row.SampleShort != "the fold happens in exactly one place" {
		t.Fatalf("sample = %q, want the FIRST entry's content (%s)", row.SampleShort, first.EntryID)
	}

	// A long body is trimmed AND says so; a subject with no entry has no sample
	// rather than a sentence invented here.
	long := strings.Repeat("長", loreSampleShortRunes+40)
	if _, err := d.CreateLoreEntry(LoreWrite{
		Trigger: "t", Content: long, Origin: "agent:O-197",
		Subjects: []string{"repo:verbose"}, ActorID: "m-writer",
	}, 300); err != nil {
		t.Fatalf("long write: %v", err)
	}
	got := t33PendingByKey(t, d, "repo:verbose").SampleShort
	if len([]rune(got)) != loreSampleShortRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long sample = %d runes, suffix %q — a trimmed sample must announce the trim",
			len([]rune(got)), got[len(got)-3:])
	}

	empty := t33Mint(t, d, "repo:unused")
	if _, err := d.wdb.Exec(`DELETE FROM lore_subject WHERE entry_id = ?`, empty.EntryID); err != nil {
		t.Fatalf("unfile: %v", err)
	}
	if row := t33PendingByKey(t, d, "repo:unused"); row.SampleShort != "" {
		t.Fatalf("sample for an entry-less subject = %q, want empty", row.SampleShort)
	}
}
