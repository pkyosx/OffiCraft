package main

// dal_lore_proposal_t33_test.go — T-33. 提案這一層的問題只有一個：審核看到的
// 差異，是不是就是會落地的東西；而它會不會在沒有人發現的情況下不是.
//
// 🔴 THE DIGEST TESTS ARE THE POINT OF THIS FILE. Every other assertion here is
// about a refusal a reader would notice anyway (a blank field, an unknown enum).
// The base-digest comparison is the one whose failure is SILENT: drop it and
// every proposal is still filed, still listed, still reads correctly, and the
// day one of them is applied it quietly reverts somebody else's edit.

import (
	"errors"
	"strings"
	"testing"
)

// t33Propose is a well-formed `update` proposal with the base digest left blank
// for each test to fill in — blank is refused, so a test that forgets to set it
// cannot accidentally pass.
func t33Propose(entryID string) LoreProposal {
	return LoreProposal{
		EntryID:      entryID,
		Kind:         "update",
		Encountered:  "T-33 slot 4, wiring the proposal route",
		Fault:        "stale",
		Evidence:     "the entry names dal_lore.go, and the function moved to dal_lore_write.go in 8282fdef",
		Label:        "boot context assembly",
		Symptoms:     "two blocks disagree about the same fact",
		Short:        "the fold happens in one place, and that place is lore_fold.go",
		Falsify:      "a second assembler appears",
		Instance:     "T-33 slot 3",
		ResidualRisk: "says nothing about who may call the fold",
		ActorID:      "ow-e27260b9ed05",
	}
}

// t33SeedForProposal writes a real entry through the ordinary write path and
// hands back its id and the digest of the original that was preserved with it.
//
// It goes through CreateLoreEntry rather than PutLoreEntry on purpose: a
// proposal is bound to an L0 revision, and PutLoreEntry writes none — an entry
// seeded that way would exercise the ErrLoreEntryNoOriginal arm and nothing else.
func t33SeedForProposal(t *testing.T, d *DAL) (string, string) {
	t.Helper()
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	got := t33Create(t, d, t33Write())
	return got.EntryID, got.SHA256
}

// 🔴 THE TEST THIS WHOLE ROUND EXISTS FOR. A proposal written against a digest
// that is not the entry's current one is REFUSED, and the refusal names both
// digests so the proposer can see which version he was holding.
func TestLoreProposalRefusesADigestThatIsNotTheEntrysCurrentOne(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = strings.Repeat("0", 64)
	_, err := d.CreateLoreProposal(p, 2000)
	if !errors.Is(err, ErrLoreProposalStale) {
		t.Fatalf("a proposal against a version nobody is holding was accepted: %v", err)
	}
	// The message has to carry BOTH digests. A refusal that says only "stale"
	// leaves the proposer unable to tell whether he read an older version or
	// mistyped one character, and those want different next steps.
	if !strings.Contains(err.Error(), sha) || !strings.Contains(err.Error(), p.BaseSHA256) {
		t.Fatalf("the refusal names neither what he sent nor what is there: %v", err)
	}
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM lore_proposal`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("the refused proposal was stored anyway: %d rows", n)
	}

	// 🔴 THE POSITIVE CONTROL, and it is not optional: without it a
	// CreateLoreProposal that refused EVERYTHING would pass the assertion above.
	p.BaseSHA256 = sha
	got, err := d.CreateLoreProposal(p, 2000)
	if err != nil {
		t.Fatalf("the same proposal against the CURRENT digest was refused: %v", err)
	}
	if got.BaseSHA256 != sha || got.BaseRevisionID == 0 {
		t.Fatalf("the receipt does not bind to the revision it matched: %+v", got)
	}
	if got.SHA256 == "" || got.SHA256 == sha {
		t.Fatalf("the proposed version digests to nothing, or to the base: %+v", got)
	}
}

// 🔴 THE OTHER HALF OF THE SAME MECHANISM, and the one submit-time checking
// cannot see: the proposal was fine when it was filed, and the entry moved
// AFTERWARDS. This is the shape a stale pull request has, and applying it
// unnoticed is what silently reverts whoever edited in between.
//
// The second revision is written directly, because nothing in the tree rewrites
// an entry yet — see the file header of api_lore_proposal.go and the report on
// this branch. That is precisely why the guard is here BEFORE the accept path
// exists: the accept path is what will start producing second revisions.
func TestLoreProposalGoesStaleWhenTheEntryMovesAfterItWasFiled(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}

	before, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if len(before.Proposals) != 1 {
		t.Fatalf("want one proposal, got %d", len(before.Proposals))
	}
	// The control: it is NOT stale yet. Without this the assertion below would
	// also pass for a `Stale` that is hard-coded true.
	if before.Proposals[0].Stale {
		t.Fatalf("a proposal filed against the current version reads as stale: %+v", before.Proposals[0])
	}
	if before.CurrentSHA256 != sha {
		t.Fatalf("current digest = %q, want %q", before.CurrentSHA256, sha)
	}

	// Somebody rewrites the entry.
	moved := t33Entry(entryID)
	moved.Short = "the fold happens in lore_fold.go, and the assembler cannot see L2"
	body := loreRevisionBody(moved)
	if _, err := d.wdb.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, 'somebody-else', 3000, 0)`,
		entryID, body, loreSHA256(body)); err != nil {
		t.Fatalf("second revision: %v", err)
	}

	after, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if !after.Proposals[0].Stale {
		t.Fatalf("the entry moved under this proposal and the list still calls it "+
			"current — applying it would discard the newer text with nothing to "+
			"show for it: %+v", after.Proposals[0])
	}
	// The digest it was compared against travels with the answer, so a reader
	// can re-derive `stale` instead of trusting it.
	if after.CurrentSHA256 == sha || after.CurrentSHA256 != loreSHA256(body) {
		t.Fatalf("current digest did not move with the entry: %q", after.CurrentSHA256)
	}
	if after.Proposals[0].BaseSHA256 != sha {
		t.Fatalf("the proposal's base digest was rewritten under it: %q", after.Proposals[0].BaseSHA256)
	}
}

// The proposal stores a WHOLE version, and the bytes stored are the bytes the
// shared renderer produces — which is the only reason a proposal's digest and a
// revision's digest can be compared at all.
func TestLoreProposalStoresTheWholeVersionUnderTheSharedRenderer(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	p := t33Propose(entryID)
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); err != nil {
		t.Fatalf("file: %v", err)
	}
	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	row := list.Proposals[0]

	// 🔴 THE EXPECTATION IS BUILT WITHOUT loreProposalEntry, ON PURPOSE. Writing
	// `want := loreRevisionBody(loreProposalEntry(p))` would run the value under
	// test through the code under test on both sides of the comparison: a mapping
	// that dropped a field would drop it in `want` too and the assertion would
	// hold forever. Measured, not feared — that is exactly what the first version
	// of this test did, and a mutant that blanked `residual_risk` in the mapping
	// walked straight past it.
	want := loreRevisionBody(LoreEntry{
		Label: p.Label, Symptoms: p.Symptoms, Short: p.Short,
		Falsify: p.Falsify, Instance: p.Instance, ResidualRisk: p.ResidualRisk,
	})
	if row.Body != want {
		t.Fatalf("stored body is not what the shared renderer produces:\n got %q\nwant %q", row.Body, want)
	}
	if row.SHA256 != loreSHA256(want) {
		t.Fatalf("stored digest does not digest the stored body: %q", row.SHA256)
	}
	// Every field present with its NAME AND ITS VALUE. The name alone would be
	// satisfied by a renderer that printed six headings over six blanks — which
	// is the shape a dropped field actually has.
	for _, f := range []struct{ section, value string }{
		{"label:", p.Label}, {"symptoms:", p.Symptoms}, {"short:", p.Short},
		{"falsify:", p.Falsify}, {"instance:", p.Instance},
		{"residual_risk:", p.ResidualRisk},
	} {
		if !strings.Contains(row.Body, f.section+"\n"+f.value+"\n") {
			t.Fatalf("the stored version drops the %q section or its value: %q", f.section, row.Body)
		}
	}
	if row.Short != p.Short || row.Fault != "stale" || row.Encountered != p.Encountered ||
		row.Evidence != p.Evidence || row.ActorID != p.ActorID {
		t.Fatalf("the row is not what was proposed: %+v", row)
	}
}

// A `remove` proposes no version, and one that carries body fields is refused
// rather than stored with them ignored — a version on the reviewer's screen that
// no accept would ever write is the description/result gap this shape exists to
// close.
func TestLoreProposalRemoveCarriesNoVersion(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	bad := t33Propose(entryID)
	bad.Kind = "remove"
	bad.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(bad, 2000); !errors.Is(err, ErrLoreProposalRemoveBody) {
		t.Fatalf("a removal carrying a whole new version was accepted: %v", err)
	}

	good := LoreProposal{
		EntryID: entryID, Kind: "remove", BaseSHA256: sha,
		Encountered: "T-33 slot 4", Fault: "never-true",
		Evidence: "the function it names has never existed in this tree",
		ActorID:  "ow-e27260b9ed05",
	}
	got, err := d.CreateLoreProposal(good, 2000)
	if err != nil {
		t.Fatalf("a bare removal was refused: %v", err)
	}
	if got.SHA256 != "" {
		t.Fatalf("a removal proposed a version: %q", got.SHA256)
	}
	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if row := list.Proposals[0]; row.Kind != "remove" || row.Body != "" || row.Short != "" {
		t.Fatalf("the stored removal carries a version: %+v", row)
	}
}

// An `update` is held to the SAME field rules a write is, because accepting one
// means writing it through the ordinary write path — a proposal that path would
// refuse can never be accepted, and it would sit in the queue looking exactly
// like one that could.
func TestLoreProposalUpdateIsHeldToTheWritePathsFieldRules(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	for _, tc := range []struct {
		name string
		edit func(*LoreProposal)
		want error
	}{
		{"blank symptoms", func(p *LoreProposal) { p.Symptoms = " " }, ErrLoreSymptomsBlank},
		{"blank short", func(p *LoreProposal) { p.Short = "" }, ErrLoreShortBlank},
		{"blank falsify", func(p *LoreProposal) { p.Falsify = "" }, ErrLoreFalsifyBlank},
		{"blank instance", func(p *LoreProposal) { p.Instance = "" }, ErrLoreInstanceBlank},
		{"over-long label", func(p *LoreProposal) { p.Label = strings.Repeat("名", 41) }, ErrLoreLabelTooLong},
		{"unknown kind", func(p *LoreProposal) { p.Kind = "patch" }, ErrLoreProposalKindUnknown},
		{"unknown fault", func(p *LoreProposal) { p.Fault = "bad" }, ErrLoreProposalFaultUnknown},
		{"blank encountered", func(p *LoreProposal) { p.Encountered = "" }, ErrLoreProposalEncountered},
		{"blank evidence", func(p *LoreProposal) { p.Evidence = "\t" }, ErrLoreProposalEvidence},
		{"blank base digest", func(p *LoreProposal) { p.BaseSHA256 = "" }, ErrLoreProposalBaseBlank},
		{"no actor", func(p *LoreProposal) { p.ActorID = "" }, ErrLoreActorBlank},
	} {
		p := t33Propose(entryID)
		p.BaseSHA256 = sha
		tc.edit(&p)
		if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}

	// The positive control for the whole table: the unedited proposal lands.
	ok := t33Propose(entryID)
	ok.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(ok, 2000); err != nil {
		t.Fatalf("the unedited proposal was refused, so the table above proves nothing: %v", err)
	}
}

// A proposal identical to what is already there is refused: it costs a reviewer
// a read and can end in no change.
func TestLoreProposalRefusesAVersionIdenticalToTheBase(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	entry := t33Get(t, d, entryID)
	same := t33Propose(entryID)
	same.BaseSHA256 = sha
	same.Label, same.Symptoms, same.Short = entry.Label, entry.Symptoms, entry.Short
	same.Falsify, same.Instance, same.ResidualRisk = entry.Falsify, entry.Instance, entry.ResidualRisk
	if _, err := d.CreateLoreProposal(same, 2000); !errors.Is(err, ErrLoreProposalNoChange) {
		t.Fatalf("a proposal that changes nothing was filed: %v", err)
	}
}

// An entry nobody has proposed anything for lists EMPTY rather than failing:
// 「沒有人提案」 and 「沒有這條」 are different facts.
func TestLoreProposalListIsEmptyRatherThanMissing(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Proposals == nil || len(list.Proposals) != 0 {
		t.Fatalf("want an empty list, got %+v", list.Proposals)
	}
	if list.CurrentSHA256 != sha || list.CurrentRevisionID == 0 {
		t.Fatalf("an empty list still has to say what the entry stands at: %+v", list)
	}

	// An entry that carries no L0 original at all is refused rather than
	// answered with an empty base — proposing against nothing is not a proposal.
	t33Put(t, d, t33Entry("lore-no-original"))
	if _, err := d.ListLoreProposals("lore-no-original"); !errors.Is(err, ErrLoreEntryNoOriginal) {
		t.Fatalf("an entry with no original listed anyway: %v", err)
	}
	p := t33Propose("lore-no-original")
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, ErrLoreEntryNoOriginal) {
		t.Fatalf("a proposal against an entry with no original was filed: %v", err)
	}
}

// An entry id nothing carries is a flat refusal, never a stored row pointing at
// nothing.
func TestLoreProposalRefusesAnUnknownEntry(t *testing.T) {
	d := newTestDAL(t)
	_, sha := t33SeedForProposal(t, d)
	p := t33Propose("lore-nobody-carries-this")
	p.BaseSHA256 = sha
	if _, err := d.CreateLoreProposal(p, 2000); !errors.Is(err, ErrLoreEntryUnknown) {
		t.Fatalf("a proposal against a non-existent entry: %v", err)
	}
}

// 🔴 THE ORDER IS ORDERED BY TIME, AND THE TEST MAKES TIME AND id DISAGREE.
//
// A proposal id is random hex, so on real data `ORDER BY id DESC` lands on the
// right answer roughly half the time and on the wrong one the other half — and
// both look identical to a reader, because every row is present and every field
// is right. Only the reading order is wrong, and the reviewer reads the first
// one. A test that files two proposals and trusts whatever ids came out cannot
// see this: it passes on the coin-flip. So the ids are forced into the order
// OPPOSITE to the filing order, which is the only arrangement in which
// id-ordering and time-ordering give different answers every single run.
func TestLoreProposalListsNewestFirstWhenIdOrderContradictsTime(t *testing.T) {
	d := newTestDAL(t)
	entryID, sha := t33SeedForProposal(t, d)

	older := t33Propose(entryID)
	older.BaseSHA256 = sha
	older.Short = "the fold happens in one place, and that place is lore_fold.go"
	gotOlder, err := d.CreateLoreProposal(older, 1000)
	if err != nil {
		t.Fatalf("filing the older proposal: %v", err)
	}
	newer := t33Propose(entryID)
	newer.BaseSHA256 = sha
	newer.Short = "the fold happens in lore_fold.go, and nothing else may assemble one"
	gotNewer, err := d.CreateLoreProposal(newer, 2000)
	if err != nil {
		t.Fatalf("filing the newer proposal: %v", err)
	}

	// Forced, not hoped for: the OLDER row gets the id that sorts LAST
	// descending, so an id-ordered read must lead with the replaced version.
	const olderID, newerID = "lp-ffffffffffff", "lp-000000000000"
	for _, r := range []struct{ from, to string }{
		{gotOlder.ProposalID, olderID}, {gotNewer.ProposalID, newerID},
	} {
		if _, err := d.wdb.Exec(`UPDATE lore_proposal SET id = ? WHERE id = ?`, r.to, r.from); err != nil {
			t.Fatalf("renaming %s: %v", r.from, err)
		}
	}

	list, err := d.ListLoreProposals(entryID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Proposals) != 2 {
		t.Fatalf("both proposals must be listed, got %d", len(list.Proposals))
	}
	if list.Proposals[0].ID != newerID {
		t.Fatalf("the list leads with the version its proposer already replaced: "+
			"got %s (created_ts %.0f), want %s (created_ts %.0f)",
			list.Proposals[0].ID, list.Proposals[0].CreatedTS,
			newerID, list.Proposals[1].CreatedTS)
	}
	if list.Proposals[0].CreatedTS <= list.Proposals[1].CreatedTS {
		t.Fatalf("the listing is not descending in time: %.0f then %.0f",
			list.Proposals[0].CreatedTS, list.Proposals[1].CreatedTS)
	}
}
