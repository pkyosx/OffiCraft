package main

// T-33 — the L1 write seam and its two join tables.

import (
	"errors"
	"strings"
	"testing"
)

func t33Entity(t *testing.T, d *DAL, id, typ, canonical string) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`INSERT INTO entity (id, type, canonical) VALUES (?, ?, ?)`, id, typ, canonical); err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func t33Entry(id string) WorldStateMemoryEntry {
	return WorldStateMemoryEntry{
		ID:           id,
		Label:        "boot context assembly",
		Origin:       "agent:O-197",
		Short:        "the fold happens in one place",
		Symptoms:     "two blocks disagree about the same fact",
		Falsify:      "a second assembler appears",
		Instance:     "T-33 slot 3",
		ResidualRisk: "it says nothing about who is allowed to call the fold",
		CreatedTS:    100,
		UpdatedTS:    100,
	}
}

func t33Put(t *testing.T, d *DAL, e WorldStateMemoryEntry) {
	t.Helper()
	if err := d.PutWorldStateMemoryEntry(e); err != nil {
		t.Fatalf("put %s: %v", e.ID, err)
	}
}

func t33Get(t *testing.T, d *DAL, id string) *WorldStateMemoryEntry {
	t.Helper()
	got, err := d.GetWorldStateMemoryEntry(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return got
}

func TestWorldStateMemoryEntryRoundTrips(t *testing.T) {
	d := newTestDAL(t)
	want := t33Entry("me-aaa")
	want.Origin = "human:Seth"
	want.Status = "underspecified"
	want.Visibility = "private"
	want.OwnerScope = "assistant"
	want.EditableBy = "owner-gated"
	want.Supersedes = "me-old"
	t33Put(t, d, want)

	got := t33Get(t, d, "me-aaa")
	if got == nil {
		t.Fatal("get returned nil for an entry that was just written")
	}
	if *got != want {
		t.Fatalf("round trip lost something:\n got %+v\nwant %+v", *got, want)
	}
}

// A missing id is not an error — the callers ask "does this exist" on purpose.
func TestWorldStateMemoryGetMissingEntryIsNilNotError(t *testing.T) {
	d := newTestDAL(t)
	got, err := d.GetWorldStateMemoryEntry("me-nope")
	if err != nil {
		t.Fatalf("get missing: unexpected error %v", err)
	}
	if got != nil {
		t.Fatalf("get missing: want nil, got %+v", got)
	}
}

// 🔴 origin is a SUBJECT KEY, and an unapproved type prefix is REFUSED BY NAME.
// This is the fail-closed half: the value set is open (any agent or human can be
// an origin), so nothing but the type vocabulary can be checked — and a prefix
// that quietly passed would let an entry claim an author from a category nobody
// approved, with the ranking rules still honouring it.
func TestWorldStateMemoryOriginMustBeAKnownTypePrefix(t *testing.T) {
	d := newTestDAL(t)
	for _, ok := range []string{"agent:O-197", "human:Seth", "role:assistant"} {
		e := t33Entry("me-" + ok)
		e.Origin = ok
		if err := d.PutWorldStateMemoryEntry(e); err != nil {
			t.Fatalf("origin %q must be accepted: %v", ok, err)
		}
	}
	e := t33Entry("me-bad-prefix")
	e.Origin = "wizard:Merlin"
	err := d.PutWorldStateMemoryEntry(e)
	if !errors.Is(err, ErrWorldStateMemoryOriginUnknownType) {
		t.Fatalf("err = %v, want ErrWorldStateMemoryOriginUnknownType", err)
	}
	// 🔴 IT MUST SAY WHICH PREFIX. "rejected" without the name sends the reader
	// hunting through a whole entry for the one word that was wrong.
	if !strings.Contains(err.Error(), "wizard") {
		t.Fatalf("the refusal must name the offending prefix, got %q", err)
	}
}

// 🔴 `member:` IS GONE — the type is `agent:`. The pair that has to work is
// agent/human, and `member` does not make that cut: the owner is a member too,
// so it fails to exclude the very thing `human` names.
func TestWorldStateMemoryOriginMemberPrefixIsNoLongerAType(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-member")
	e.Origin = "member:Kyle"
	if err := d.PutWorldStateMemoryEntry(e); !errors.Is(err, ErrWorldStateMemoryOriginUnknownType) {
		t.Fatalf("err = %v, want the `member` prefix to be refused", err)
	}
}

// A blank or shapeless origin is refused rather than defaulted. There is no
// default author, and an "unspecified" written as though it were a person would
// be a claim nobody made — while still counting as a ranking axis.
func TestWorldStateMemoryOriginBlankOrMalformedIsRefused(t *testing.T) {
	d := newTestDAL(t)
	for _, tc := range []struct {
		origin string
		want   error
	}{
		{"", ErrWorldStateMemoryOriginBlank},
		{"   ", ErrWorldStateMemoryOriginBlank},
		{"Seth", ErrWorldStateMemoryOriginMalformed},
		{"human:", ErrWorldStateMemoryOriginMalformed},
		{":Seth", ErrWorldStateMemoryOriginMalformed},
	} {
		e := t33Entry("me-origin")
		e.Origin = tc.origin
		if err := d.PutWorldStateMemoryEntry(e); !errors.Is(err, tc.want) {
			t.Fatalf("origin %q: err = %v, want %v", tc.origin, err, tc.want)
		}
	}
}

// 🔴 ONE COPY OF THE TYPE VOCABULARY. The prefixes origin accepts are exactly the
// rows of entity_type — the same list subjects are checked against. This test is
// what would catch a second, hard-coded list being introduced in Go: approve a
// type in the table alone, and the write must start passing.
func TestWorldStateMemoryOriginTypesComeFromTheEntityTypeTable(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-newtype")
	e.Origin = "vendor:Acme"
	if err := d.PutWorldStateMemoryEntry(e); !errors.Is(err, ErrWorldStateMemoryOriginUnknownType) {
		t.Fatalf("err = %v, want the unapproved prefix to be refused first", err)
	}
	if _, err := d.wdb.Exec(`INSERT INTO entity_type (type) VALUES ('vendor')`); err != nil {
		t.Fatalf("approve type: %v", err)
	}
	if err := d.PutWorldStateMemoryEntry(e); err != nil {
		t.Fatalf("after approving the type the same write must pass, got %v", err)
	}
}

// 🔴 The label is a NAME, and an over-long one is REFUSED — not truncated, not
// warned about. Silently shortening a name is the system editing an identifier
// that merges and supersedes point at.
func TestWorldStateMemoryLabelOverTheCapIsRefusedNotTruncated(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-longlabel")
	e.Label = strings.Repeat("x", worldStateMemoryLabelMaxRunes+1)
	err := d.PutWorldStateMemoryEntry(e)
	if !errors.Is(err, ErrWorldStateMemoryLabelTooLong) {
		t.Fatalf("err = %v, want ErrWorldStateMemoryLabelTooLong", err)
	}
	if got := t33Get(t, d, "me-longlabel"); got != nil {
		t.Fatalf("a refused write must not land, got %+v", got)
	}

	// Exactly at the cap is fine, and the length is counted in RUNES — a
	// 40-character Chinese name is 40, not 120.
	e.Label = strings.Repeat("界", worldStateMemoryLabelMaxRunes)
	if err := d.PutWorldStateMemoryEntry(e); err != nil {
		t.Fatalf("a label exactly at the cap must be accepted: %v", err)
	}
	if got := t33Get(t, d, "me-longlabel"); got == nil || got.Label != e.Label {
		t.Fatalf("the accepted label must land unchanged, got %+v", got)
	}
}

// origin is L1, which means it must be readable through the ordinary entry read
// — the one the assembler uses. A field that could only be reached through a
// governance query could not participate in ordering or truncation, which is the
// whole reason it was moved out of the meta table.
func TestWorldStateMemoryOriginSurvivesOnTheEntryRead(t *testing.T) {
	d := newTestDAL(t)
	e := t33Entry("me-ccc")
	e.Origin = "human:Seth"
	t33Put(t, d, e)
	t33Entity(t, d, "e-1", "repo", "repo:officraft")
	if err := d.PutWorldStateMemorySubject("me-ccc", "e-1"); err != nil {
		t.Fatalf("file subject: %v", err)
	}
	list, err := d.ListWorldStateMemoryEntriesBySubject("e-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Origin != "human:Seth" {
		t.Fatalf("origin did not survive the by-subject read: %+v", list)
	}
}

// 🔴 An edit keeps the birth timestamp. created_ts is what the staleness
// judgement reads; letting an edit reset it would make any entry look freshly
// minted the moment somebody tightened its wording.
func TestWorldStateMemoryPutIsAnUpsertThatKeepsCreatedTS(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-ddd"))

	edited := t33Entry("me-ddd")
	edited.Short = "tightened"
	edited.CreatedTS = 999
	edited.UpdatedTS = 500
	t33Put(t, d, edited)

	got := t33Get(t, d, "me-ddd")
	if got.Short != "tightened" || got.UpdatedTS != 500 {
		t.Fatalf("the edit did not land: %+v", got)
	}
	if got.CreatedTS != 100 {
		t.Fatalf("CreatedTS = %v, want the original 100 — an edit is not a birth", got.CreatedTS)
	}
}

func TestWorldStateMemoryPutRefusesABlankID(t *testing.T) {
	d := newTestDAL(t)
	err := d.PutWorldStateMemoryEntry(WorldStateMemoryEntry{})
	if !errors.Is(err, ErrWorldStateMemoryEntryIDBlank) {
		t.Fatalf("err = %v, want ErrWorldStateMemoryEntryIDBlank", err)
	}
}

// 🔴 ONE ENTRY, MANY SUBJECTS — the reason subjects are a join table.
func TestWorldStateMemoryEntryCanCarryManySubjects(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-eee"))
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	t33Entity(t, d, "e-mem", "member", "member:Kyle")
	for _, ent := range []string{"e-repo", "e-mem"} {
		if err := d.PutWorldStateMemorySubject("me-eee", ent); err != nil {
			t.Fatalf("file %s: %v", ent, err)
		}
	}
	// Re-filing an existing pair is the state the caller asked for, not an error.
	if err := d.PutWorldStateMemorySubject("me-eee", "e-repo"); err != nil {
		t.Fatalf("re-file: %v", err)
	}
	subs, err := d.ListWorldStateMemorySubjects("me-eee")
	if err != nil {
		t.Fatalf("list subjects: %v", err)
	}
	if len(subs) != 2 || subs[0] != "e-mem" || subs[1] != "e-repo" {
		t.Fatalf("subjects = %v, want the two filed entities, sorted", subs)
	}
	for _, ent := range []string{"e-repo", "e-mem"} {
		list, err := d.ListWorldStateMemoryEntriesBySubject(ent)
		if err != nil {
			t.Fatalf("list by %s: %v", ent, err)
		}
		if len(list) != 1 || list[0].ID != "me-eee" {
			t.Fatalf("list by %s = %+v, want the one entry", ent, list)
		}
	}
}

// 🔴 retired means "no longer retrieved", and the row still exists. Both halves
// are asserted: a test that only checked the list would also pass against a hard
// delete, which is the path this ticket deliberately does not build.
func TestWorldStateMemoryRetiredIsUnretrievedButNotGone(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, id := range []string{"me-live", "me-dead"} {
		t33Put(t, d, t33Entry(id))
		if err := d.PutWorldStateMemorySubject(id, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", id, err)
		}
	}
	dead := t33Entry("me-dead")
	dead.Status = "retired"
	t33Put(t, d, dead)

	list, err := d.ListWorldStateMemoryEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "me-live" {
		t.Fatalf("list = %+v, want only the active entry", list)
	}
	n, err := d.CountWorldStateMemoryEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1 — the count must agree with the list", n)
	}
	if got := t33Get(t, d, "me-dead"); got == nil || got.Status != "retired" {
		t.Fatalf("the retired entry must still be readable by id, got %+v", got)
	}
}

// 'superseded' and 'underspecified' are NOT filtered — no ruling says they are,
// and dropping them here would decide that by accident, invisibly.
func TestWorldStateMemoryOnlyRetiredIsFilteredFromRetrieval(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, st := range []string{"active", "superseded", "underspecified", "retired"} {
		e := t33Entry("me-" + st)
		e.Status = st
		t33Put(t, d, e)
		if err := d.PutWorldStateMemorySubject(e.ID, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", e.ID, err)
		}
	}
	n, err := d.CountWorldStateMemoryEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3 — only 'retired' is excluded", n)
	}
}

// The action axis is OPEN: an unrecognised name must be storable, because
// refusing it would block exactly the writes this mechanism exists to capture.
// The safety is at read time (memoryTrustScope), and it is checked here end to
// end so the two halves cannot drift apart.
func TestWorldStateMemoryActionsAreOpenAndClassifiedOnRead(t *testing.T) {
	d := newTestDAL(t)
	t33Put(t, d, t33Entry("me-fff"))
	for _, a := range []string{"deploy", "an-action-nobody-mapped"} {
		if err := d.PutWorldStateMemoryAction("me-fff", a); err != nil {
			t.Fatalf("file action %q: %v", a, err)
		}
	}
	actions, err := d.ListWorldStateMemoryActions("me-fff")
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %v, want both stored", actions)
	}
	v := memoryTrustScope(actions)
	if v.Scope != TrustScopeTrust || !v.FellBack() {
		t.Fatalf("a stored unmapped action must fail closed and say so: %+v", v)
	}
}

// The count and the list answer the same question, so they must never disagree.
func TestWorldStateMemoryCountAgreesWithList(t *testing.T) {
	d := newTestDAL(t)
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	for _, id := range []string{"me-1", "me-2", "me-3"} {
		t33Put(t, d, t33Entry(id))
		if err := d.PutWorldStateMemorySubject(id, "e-repo"); err != nil {
			t.Fatalf("file %s: %v", id, err)
		}
	}
	list, err := d.ListWorldStateMemoryEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n, err := d.CountWorldStateMemoryEntriesBySubject("e-repo")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(list) {
		t.Fatalf("count = %d, list = %d", n, len(list))
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	// An entity nobody filed anything against answers zero, not an error.
	if n, err := d.CountWorldStateMemoryEntriesBySubject("e-unused"); err != nil || n != 0 {
		t.Fatalf("count for an unused entity = %d, %v; want 0, nil", n, err)
	}
}

// 🔴 IsDegraded — the positive and negative cases the owner asked for.
//
// The point of this pair is not the boolean; it is that the boolean CAN BE
// COMPUTED. With the body fields fixed, an entry eroded back into a slogan is a
// query. Under free form it is indistinguishable from an entry whose author
// simply never wrote that section — which is the silent loss this ticket exists
// to make visible.
func TestWorldStateMemoryIsDegraded(t *testing.T) {
	full := t33Entry("me-full")
	if full.IsDegraded() {
		t.Fatalf("an entry with both a falsifier and an instance is not degraded: %+v", full)
	}

	slogan := t33Entry("me-slogan")
	slogan.Falsify = ""
	slogan.Instance = ""
	if !slogan.IsDegraded() {
		t.Fatalf("an entry with neither a falsifier nor an instance IS degraded: %+v", slogan)
	}

	// Whitespace is not content — an entry padded back to "non-empty" with a
	// space is exactly the erosion this is meant to catch.
	blank := t33Entry("me-blank")
	blank.Falsify = "   "
	blank.Instance = "\n"
	if !blank.IsDegraded() {
		t.Fatalf("whitespace must not count as content: %+v", blank)
	}

	// Either field alone keeps it out: thin is not empty, and flagging thin
	// entries would flag most honest first drafts.
	for _, e := range []WorldStateMemoryEntry{
		func() WorldStateMemoryEntry { e := t33Entry("x"); e.Falsify = ""; return e }(),
		func() WorldStateMemoryEntry { e := t33Entry("x"); e.Instance = ""; return e }(),
	} {
		if e.IsDegraded() {
			t.Fatalf("one of the two fields is still present; not degraded: %+v", e)
		}
	}
}

// The six body fields survive a write and a read, by name. A column dropped from
// the INSERT list or transposed in the scan would otherwise show up much later
// as an entry that lost one section.
func TestWorldStateMemorySixBodyFieldsRoundTripByName(t *testing.T) {
	d := newTestDAL(t)
	e := WorldStateMemoryEntry{
		ID:           "me-six",
		Label:        "L",
		Symptoms:     "SY",
		Short:        "SH",
		Falsify:      "F",
		Instance:     "I",
		ResidualRisk: "R",
		Origin:       "agent:O-197",
	}
	t33Put(t, d, e)
	got := t33Get(t, d, "me-six")
	if got.Label != "L" || got.Symptoms != "SY" || got.Short != "SH" ||
		got.Falsify != "F" || got.Instance != "I" || got.ResidualRisk != "R" {
		t.Fatalf("a body field was lost or transposed: %+v", *got)
	}
}
