package main

// lore_select_t33_test.go — T-33: the selection rule, which is the one格 the
// spec calls 最容易做出假綠的一格.
//
// 🔴 EVERY EXPECTATION IN THIS FILE IS WRITTEN OUT BY HAND. No test here
// re-derives what it expects by sorting, filtering or measuring — because a
// test that recomputes the answer is testing the rule against itself and stays
// green while both halves move together. The expectations are literal id lists
// in the order they must come back.
//
// The seeded fixtures are also deliberately NOT in the answer's order on the
// way in: a selector that returned its input unchanged would pass a test whose
// fixture was already sorted, which is exactly the false green this file exists
// to avoid.

import (
	"errors"
	"strings"
	"testing"
)

// fakeLoreLister is the seam selectLoreForScope reads through. It returns
// whatever it is handed, IN THAT ORDER, so a test can hand the selector an
// order that the real query would produce and check the stop rule on its own.
//
// 🔴 It does NOT sort. If it sorted, this file would be testing the fake.
type fakeLoreLister struct {
	entries []LoreEntry
	err     error
	calls   int
}

func (f *fakeLoreLister) ListLoreEntriesLive(scopeKind, scopeKey string) ([]LoreEntry, error) {
	f.calls++
	return f.entries, f.err
}

// loreFixture builds one entry with a title+body that costs exactly `chars`
// characters, so a test can state a cap in the same unit its fixtures are
// measured in.
func loreFixture(id, state string, chars int) LoreEntry {
	if chars < 1 {
		chars = 1
	}
	return LoreEntry{
		ID:    id,
		Title: strings.Repeat("t", 1),
		Body:  strings.Repeat("b", chars-1),
		State: state,
	}
}

func loreIDsOf(entries []LoreEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func wantLoreIDs(t *testing.T, got []LoreEntry, want []string) {
	t.Helper()
	gotIDs := loreIDsOf(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("selected %v, want exactly %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("selected %v, want exactly %v (differs at position %d)",
				gotIDs, want, i)
		}
	}
}

// TestSelectLoreKeepsTheListersOrder pins that the selector takes the order it
// is GIVEN and does not impose one of its own.
//
// 🔴 WHY THIS IS THE ASSERTION AND NOT "pinned comes first". The pinned-first,
// newest-effective-first order is produced by ONE piece of SQL
// (ListLoreEntriesLive) and is pinned against a real database in
// dal_lore_t33_test.go. If the selector ALSO sorted, there would be two
// definitions of the order and a change to either would leave the other looking
// plausible. So the rule here is the negative one: whatever arrives, in that
// order, out again.
//
// Mutant that must redden this: adding any sort.Slice to selectLoreForScope.
func TestSelectLoreKeepsTheListersOrder(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		loreFixture("L-3", LoreStateActive, 10),
		loreFixture("L-1", LoreStatePinned, 10),
		loreFixture("L-2", LoreStateActive, 10),
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 1000)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	// Hand-written. The fixture order is L-3, L-1, L-2 and that is the answer:
	// a selector that "helpfully" floated the pinned L-1 to the front would be
	// a second sort, and this is what refuses one.
	wantLoreIDs(t, sel.Entries, []string{"L-3", "L-1", "L-2"})
	if sel.FirstDroppedID != "" {
		t.Fatalf("FirstDroppedID = %q, want \"\" — everything fitted under a cap of 1000",
			sel.FirstDroppedID)
	}
}

// TestSelectLoreStopsAtTheFirstEntryThatDoesNotFit is the spec's 「就此停住」
// rule, and it is written so that the two wrong behaviours produce DIFFERENT
// visible failures:
//
//   - TRUNCATING the over-cap entry → L-2 would appear in the answer;
//   - SKIPPING it and continuing → L-3 would appear in the answer.
//
// The fixture is built so that L-3 WOULD fit in the room L-2 leaves behind
// (L-3 costs 10 against 20 remaining). That is the whole point: without it, a
// skip-and-continue implementation would produce the identical output and this
// test would be green against the bug it exists to catch.
func TestSelectLoreStopsAtTheFirstEntryThatDoesNotFit(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		loreFixture("L-1", LoreStateActive, 80),
		loreFixture("L-2", LoreStateActive, 50), // 80+50 = 130 > 100 ⇒ stop here
		loreFixture("L-3", LoreStateActive, 10), // would fit in the leftover 20
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 100)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	wantLoreIDs(t, sel.Entries, []string{"L-1"})
	if sel.FirstDroppedID != "L-2" {
		t.Fatalf("FirstDroppedID = %q, want \"L-2\" — the line is drawn above the "+
			"first entry that did not fit, not above the last one tried",
			sel.FirstDroppedID)
	}
	if sel.UsedChars != 80 {
		t.Fatalf("UsedChars = %d, want 80", sel.UsedChars)
	}
}

// TestSelectLoreTakesEverythingThatExactlyFills is the boundary: a total EQUAL
// to the cap is admitted, not refused. `>` versus `>=` in the stop condition is
// a one-character mutation with no other observable effect, so it needs its own
// case.
func TestSelectLoreTakesEverythingThatExactlyFills(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		loreFixture("L-1", LoreStateActive, 60),
		loreFixture("L-2", LoreStateActive, 40), // 60+40 = 100 == the cap
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 100)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	wantLoreIDs(t, sel.Entries, []string{"L-1", "L-2"})
	if sel.FirstDroppedID != "" {
		t.Fatalf("FirstDroppedID = %q, want \"\" — a total equal to the cap fits",
			sel.FirstDroppedID)
	}
}

// TestSelectLoreRefusesAnEntryBiggerThanTheWholeCap: position zero is not a
// special case that gets a free pass. An entry that alone exceeds the cap ends
// the selection with nothing taken — it is NOT truncated to fit.
func TestSelectLoreRefusesAnEntryBiggerThanTheWholeCap(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		loreFixture("L-1", LoreStateActive, 500),
		loreFixture("L-2", LoreStateActive, 10),
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 100)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	wantLoreIDs(t, sel.Entries, []string{})
	if sel.FirstDroppedID != "L-1" {
		t.Fatalf("FirstDroppedID = %q, want \"L-1\"", sel.FirstDroppedID)
	}
	if sel.UsedChars != 0 {
		t.Fatalf("UsedChars = %d, want 0 — nothing was taken", sel.UsedChars)
	}
}

// TestSelectLoreCountsCharactersNotBytes. The fixture is CJK, where a byte
// count is three times the character count: under a cap of 10 the entry costs
// 4 characters (1 title + 3 body) and fits, but 10 bytes and would not.
//
// Mutant that must redden this: len() in place of utf8.RuneCountInString in
// loreEntryChars.
func TestSelectLoreCountsCharactersNotBytes(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		{ID: "L-1", Title: "標", Body: "題內容", State: LoreStateActive}, // 4 chars, 12 bytes
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 10)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	wantLoreIDs(t, sel.Entries, []string{"L-1"})
	if sel.UsedChars != 4 {
		t.Fatalf("UsedChars = %d, want 4 — the cap is in CHARACTERS, and this "+
			"entry is 4 characters but 12 bytes", sel.UsedChars)
	}
}

// TestSelectLoreReadsZeroCapAsNoRoom. Zero must not be read as unlimited: that
// is how a mis-loaded setting turns into an unbounded boot document.
func TestSelectLoreReadsZeroCapAsNoRoom(t *testing.T) {
	lister := &fakeLoreLister{entries: []LoreEntry{
		loreFixture("L-1", LoreStateActive, 1),
	}}
	sel, err := selectLoreForScope(lister, LoreScopeAgent, "m-staff-1", 0)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	wantLoreIDs(t, sel.Entries, []string{})
	if lister.calls != 0 {
		t.Fatalf("the lister was called %d times under a zero cap — with no room "+
			"there is nothing to read", lister.calls)
	}
}

// TestSelectLoreSurfacesTheListerError — a database failure must not be folded
// into "this scope has no lore", which is what an ignored error would look like
// at both exits.
func TestSelectLoreSurfacesTheListerError(t *testing.T) {
	boom := errors.New("database is closed")
	_, err := selectLoreForScope(&fakeLoreLister{err: boom},
		LoreScopeAgent, "m-staff-1", 100)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the lister's own error — an empty fold and a "+
			"broken database must not look the same", err)
	}
}

// TestRenderLoreBlockIsEmptyForAnEmptySelection. Both exits append this
// unconditionally, so a heading with nothing under it would be a claim ("this
// member has no traditions") that the empty selection does not support — the cap
// may be zero, or everything may be retired.
func TestRenderLoreBlockIsEmptyForAnEmptySelection(t *testing.T) {
	if got := renderLoreBlock(loreSelection{Entries: []LoreEntry{}}); got != "" {
		t.Fatalf("renderLoreBlock(empty) = %q, want the empty string", got)
	}
}

// TestRenderLoreBlockNamesEveryEntry — the id has to be in the rendered text,
// because it is the ONLY handle a reader is given for retiring or bumping the
// entry it just read.
func TestRenderLoreBlockNamesEveryEntry(t *testing.T) {
	block := renderLoreBlock(loreSelection{Entries: []LoreEntry{
		{ID: "L-7", Title: "一件事", Body: "內容", State: LoreStateActive},
		{ID: "L-9", Title: "另一件", Body: "內容二", State: LoreStatePinned},
	}})
	for _, want := range []string{loreBlockHeading, "L-7", "一件事", "內容", "L-9", "另一件"} {
		if !strings.Contains(block, want) {
			t.Fatalf("rendered block is missing %q:\n%s", want, block)
		}
	}
	if strings.Index(block, "L-7") > strings.Index(block, "L-9") {
		t.Fatalf("rendered block reordered the selection:\n%s", block)
	}
}
