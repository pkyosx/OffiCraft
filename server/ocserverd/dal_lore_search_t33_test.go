package main

// T-33 — hop ②. Every test here asks the same question from a different angle:
// does the answer contain exactly what was asked for, and does it SAY what it
// did? A retrieval bug does not raise; it hands back a plausible set.

import (
	"errors"
	"strings"
	"testing"
)

// t33SearchSeed builds a small store: two subjects, one shared action.
func t33SearchSeed(t *testing.T, d *DAL) {
	t.Helper()
	t33Entity(t, d, "e-repo", "repo", "repo:officraft")
	t33Entity(t, d, "e-kyle", "agent", "agent:Kyle")
}

func t33Filed(t *testing.T, d *DAL, id, subject string, actions []string, mutate func(*LoreEntry)) LoreEntry {
	t.Helper()
	e := t33Entry(id)
	if mutate != nil {
		mutate(&e)
	}
	t33Put(t, d, e)
	if err := d.PutLoreSubject(id, subject); err != nil {
		t.Fatalf("file %s under %s: %v", id, subject, err)
	}
	for _, a := range actions {
		if err := d.PutLoreAction(id, a); err != nil {
			t.Fatalf("action %s on %s: %v", a, id, err)
		}
	}
	return e
}

func t33Search(t *testing.T, d *DAL, s LoreSearch) LoreSearchResult {
	t.Helper()
	got, err := d.SearchLore(s)
	if err != nil {
		t.Fatalf("search %+v: %v", s, err)
	}
	return got
}

func t33IDs(r LoreSearchResult) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.Entry.ID)
	}
	return out
}

// Asking on one axis returns what is filed there and NOTHING else, and the
// results are T1 — they matched every axis the caller asked on.
func TestLoreSearchOneAxisIsAMatchNotAnAnalogy(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", []string{"build"}, nil)
	t33Filed(t, d, "lore-b", "e-kyle", []string{"build"}, nil)

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft"})
	if ids := t33IDs(got); len(ids) != 1 || ids[0] != "lore-a" {
		t.Fatalf("subject filter returned %v", ids)
	}
	// 🔴 THE TIER. Read literally the design would call this an analogy, which
	// would make every ordinary lookup say "this is a guess" — a label that is
	// always on is a label nobody reads.
	if got.Hits[0].Tier != LoreTierMatch {
		t.Fatalf("an exact hit on the only axis asked was tiered %q: %s",
			got.Hits[0].Tier, got.Hits[0].TierNote)
	}
	if got.Total != 1 || got.Truncated {
		t.Fatalf("total/truncated: %d/%v", got.Total, got.Truncated)
	}
}

// Asking on BOTH axes and matching only one is the analogy tier, and the note
// has to say so in words — an entry that crossed an axis silently reads exactly
// like a rule for your case.
func TestLoreSearchCrossingAnAxisYouDidNotAskOnAnnouncesItself(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-both", "e-repo", []string{"build"}, nil)
	t33Filed(t, d, "lore-other", "e-kyle", []string{"build"}, nil)

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft", Actions: []string{"build"}})
	if ids := t33IDs(got); len(ids) != 2 || ids[0] != "lore-both" {
		t.Fatalf("hits/order: %v", ids)
	}
	if got.Hits[0].Tier != LoreTierMatch {
		t.Fatalf("the two-axis hit is tier %q", got.Hits[0].Tier)
	}
	if got.Hits[1].Tier != LoreTierAnalogy {
		t.Fatalf("the one-axis hit is tier %q, want the analogy tier", got.Hits[1].Tier)
	}
	if !strings.Contains(got.Hits[1].TierNote, "類比") {
		t.Fatalf("the analogy does not announce itself: %q", got.Hits[1].TierNote)
	}
}

// 🔴 「這個對象沒有東西」 and 「這個對象不存在」 are different answers and the
// owner ruled they must stay different (rc-455a5d3c308c). Both halves are
// asserted, because either alone would pass a version that always said one.
func TestLoreSearchTellsAnEmptySubjectApartFromAMissingOne(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)

	empty := t33Search(t, d, LoreSearch{SubjectKey: "agent:Kyle"})
	if !empty.SubjectResolved || len(empty.Hits) != 0 || empty.UnresolvedSubject != "" {
		t.Fatalf("a real subject with no entries: %+v", empty)
	}
	missing := t33Search(t, d, LoreSearch{SubjectKey: "repo:no-such-thing"})
	if missing.SubjectResolved || missing.UnresolvedSubject != "repo:no-such-thing" {
		t.Fatalf("a subject that does not exist: %+v", missing)
	}
}

// A trust-class entry does not travel: "X could be relied on" is about X. It is
// withheld from the analogy tier, and the caller has to ask for it by name —
// at which point the note must say WHOSE situation it describes.
func TestLoreSearchWithholdsTrustClassAnalogiesUntilAskedByName(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-trust", "e-kyle", []string{"estimate"}, nil)

	quiet := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft", Actions: []string{"estimate"}})
	if len(quiet.Hits) != 0 {
		t.Fatalf("a trust-class analogy crossed subjects unasked: %+v", t33IDs(quiet))
	}
	forced := t33Search(t, d, LoreSearch{
		SubjectKey: "repo:officraft", Actions: []string{"estimate"}, ForceTrustAnalogy: true})
	if ids := t33IDs(forced); len(ids) != 1 || ids[0] != "lore-trust" {
		t.Fatalf("force_trust_analogy did not let it through: %v", ids)
	}
	if !strings.Contains(forced.Hits[0].TierNote, "agent:Kyle") {
		t.Fatalf("the forced analogy does not name whose situation it is: %q",
			forced.Hits[0].TierNote)
	}

	// A METHOD-class entry crosses without being asked — the positive control.
	// Without it, a version that withheld every analogy would pass above.
	t33Filed(t, d, "lore-method", "e-kyle", []string{"build"}, nil)
	open := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft", Actions: []string{"build"}})
	if ids := t33IDs(open); len(ids) != 1 || ids[0] != "lore-method" {
		t.Fatalf("a method-class analogy was withheld too: %v", ids)
	}
}

// 🔴 AN ACTION NAME THE TABLE DOES NOT KNOW FAILS CLOSED — and the caller is
// TOLD, per entry and in aggregate. The mapping table's own header says it is
// hand-written, provisional and "the implementer's reading, not a decision
// anybody made"; a classification derived from a guess must not be
// indistinguishable from one derived from the table.
func TestLoreSearchSaysWhenAClassificationWasAGuess(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-unknown", "e-repo", []string{"zzz-not-in-the-table"}, nil)
	t33Filed(t, d, "lore-known", "e-repo", []string{"build"}, nil)

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft"})
	byID := map[string]LoreSearchHit{}
	for _, h := range got.Hits {
		byID[h.Entry.ID] = h
	}
	unknown, known := byID["lore-unknown"], byID["lore-known"]
	if !unknown.TrustFellBack {
		t.Fatalf("an unrecognised action was classified WITHOUT saying it guessed: %+v", unknown)
	}
	if unknown.TrustScope != string(TrustScopeTrust) {
		t.Fatalf("the fail-closed class is %q, want the strictest", unknown.TrustScope)
	}
	// The positive control: a known action must NOT be flagged, or the flag is
	// always on and carries nothing.
	if known.TrustFellBack {
		t.Fatalf("a recognised action was reported as a guess: %+v", known)
	}
	if len(got.UnmappedActions) != 1 || got.UnmappedActions[0] != "zzz-not-in-the-table" {
		t.Fatalf("the unrecognised name is not named back: %v", got.UnmappedActions)
	}
}

// The `query` filter is a LITERAL substring, and this test pins BOTH halves —
// that it matches, and that it fails on text about the same thing written
// differently. The second half is the honest limitation, asserted so nobody
// later reads this filter as semantic search.
func TestLoreSearchQueryIsLiteralAndSaysNothingAboutMeaning(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-x", "e-repo", []string{"build"}, func(e *LoreEntry) {
		e.Trigger = "two blocks disagree about the same fact"
	})
	t33Filed(t, d, "lore-y", "e-repo", []string{"build"}, func(e *LoreEntry) {
		e.Trigger = "the assembler and the roster report different numbers"
	})

	hit := t33Search(t, d, LoreSearch{Query: "DISAGREE"})
	if ids := t33IDs(hit); len(ids) != 1 || ids[0] != "lore-x" {
		t.Fatalf("case-insensitive literal match: %v", ids)
	}
	// 🔴 The two 第 1 格 above describe the same situation. A literal filter
	// finds one and not the other, and that is the documented limit — if this
	// assertion ever starts failing, somebody made the filter semantic and the
	// wire's `query_match` value became a lie.
	miss := t33Search(t, d, LoreSearch{Query: "disagree"})
	if len(miss.Hits) != 1 {
		t.Fatalf("a literal filter matched something it cannot understand: %v", t33IDs(miss))
	}
}

// A human origin sorts ahead within its tier AND survives the count cap — what
// a person said is not competing with what an agent worked out.
func TestLoreSearchKeepsWhatAPersonSaidAheadAndUncapped(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	for _, id := range []string{"lore-m1", "lore-m2", "lore-m3"} {
		t33Filed(t, d, id, "e-repo", []string{"build"}, nil)
	}
	t33Filed(t, d, "lore-human", "e-repo", []string{"build"}, func(e *LoreEntry) {
		e.Origin = "human:Seth"
		e.CreatedTS = 9999 // newest, so only the origin rule can put it first
	})

	got := t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft", Limit: 1})
	ids := t33IDs(got)
	if len(ids) == 0 || ids[0] != "lore-human" {
		t.Fatalf("a human origin did not sort first: %v", ids)
	}
	if len(ids) != 2 {
		t.Fatalf("limit 1 kept %d hits; want the human one plus one agent one", len(ids))
	}
	if got.Total != 4 || !got.Truncated {
		t.Fatalf("total/truncated: %d/%v", got.Total, got.Truncated)
	}
}

// Retired entries are never retrieved — the same predicate the directory and
// the per-subject list use.
func TestLoreSearchNeverReturnsARetiredEntry(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-live", "e-repo", []string{"build"}, nil)
	t33Filed(t, d, "lore-gone", "e-repo", []string{"build"}, nil)
	if err := d.RetireLoreEntry("lore-gone", LoreRetireExpired, "m-x", "", false, 500); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if ids := t33IDs(t33Search(t, d, LoreSearch{SubjectKey: "repo:officraft"})); len(ids) != 1 || ids[0] != "lore-live" {
		t.Fatalf("retired entry reachable through search: %v", ids)
	}
}

// No axis at all is a legitimate question — "what is in here" — and it is NOT
// an analogy, because nothing crossed an axis the caller cared about.
func TestLoreSearchWithNoAxisIsNotAnAnalogy(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", []string{"build"}, nil)

	got := t33Search(t, d, LoreSearch{})
	if len(got.Hits) != 1 || got.Hits[0].Tier != LoreTierMatch {
		t.Fatalf("no-axis search: %+v", got.Hits)
	}
	if !strings.Contains(got.Hits[0].TierNote, "no selection axis") {
		t.Fatalf("the note does not say why it is not an analogy: %q", got.Hits[0].TierNote)
	}
}

// An out-of-range limit is refused rather than clamped: a caller that asked for
// 500 and silently got 100 believes it has seen everything.
func TestLoreSearchRefusesAnOutOfRangeLimitRatherThanClampingIt(t *testing.T) {
	d := newTestDAL(t)
	for _, n := range []int{-1, loreSearchLimitMax + 1} {
		if _, err := d.SearchLore(LoreSearch{Limit: n}); !errors.Is(err, ErrLoreSearchLimitRange) {
			t.Fatalf("limit %d: got %v", n, err)
		}
	}
}

// A subject key that resolves through an alias, and one whose subject was
// merged away, both reach the surviving subject — the same rule the write path
// applies, asserted here so the two cannot drift apart.
func TestLoreSearchResolvesAliasesAndFollowsMergesLikeTheWritePathDoes(t *testing.T) {
	d := newTestDAL(t)
	t33SearchSeed(t, d)
	t33Filed(t, d, "lore-a", "e-repo", []string{"build"}, nil)
	if _, err := d.wdb.Exec(
		`INSERT INTO entity_alias (alias, entity_id) VALUES ('repo:oc', 'e-repo')`); err != nil {
		t.Fatalf("alias: %v", err)
	}
	t33Entity(t, d, "e-old", "repo", "repo:oldname")
	if _, err := d.wdb.Exec(`UPDATE entity SET merged_into = 'e-repo' WHERE id = 'e-old'`); err != nil {
		t.Fatalf("merge: %v", err)
	}
	for _, key := range []string{"repo:oc", "repo:oldname"} {
		if ids := t33IDs(t33Search(t, d, LoreSearch{SubjectKey: key})); len(ids) != 1 || ids[0] != "lore-a" {
			t.Fatalf("key %q resolved to %v", key, ids)
		}
	}
}
