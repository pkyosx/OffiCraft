package main

// dal_lore_t33_test.go — T-33: the ORDER, against a real SQLite file.
//
// The order is produced by one ORDER BY in ListLoreEntriesLive, so it is pinned
// here rather than in lore_select_t33_test.go — and pinned with hand-written id
// lists, never by re-sorting the fixtures in the test.

import (
	"testing"
)

// seedLore writes one entry directly through the DAL and returns it. The
// timestamps are passed in rather than taken from the clock so the expected
// order is a fact about the fixture, not about how fast the test ran.
func seedLore(t *testing.T, d *DAL, scopeKind, scopeKey, title, state string, effectiveTS float64) LoreEntry {
	t.Helper()
	e, err := d.CreateLoreEntryMintingID(LoreEntry{
		ScopeKind:   scopeKind,
		ScopeKey:    scopeKey,
		Title:       title,
		Body:        "body of " + title,
		AuthorID:    "m-author",
		State:       state,
		EffectiveTS: effectiveTS,
		CreatedTS:   effectiveTS,
		UpdatedTS:   effectiveTS,
	})
	if err != nil {
		t.Fatalf("CreateLoreEntryMintingID(%s): %v", title, err)
	}
	return e
}

// TestLoreLiveOrderIsPinnedFirstThenNewestEffective pins the whole order in one
// hand-written list, and the fixture is seeded in an order that is NOT the
// answer so that "returns its input" cannot pass.
//
// The expected list is written out literally:
//
//	L-4 (pinned,  effective 10)   ← pinned beats every active regardless of time
//	L-2 (pinned,  effective 5)
//	L-3 (active,  effective 30)   ← then active, newest effective first
//	L-1 (active,  effective 20)
//
// L-5 is retired and must be ABSENT — and it carries the newest effective_ts of
// all, so an implementation that dropped the state filter would put it FIRST
// among the actives and this list would fail loudly rather than subtly.
func TestLoreLiveOrderIsPinnedFirstThenNewestEffective(t *testing.T) {
	d := newTestDAL(t)
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "one", LoreStateActive, 20)   // L-1
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "two", LoreStatePinned, 5)    // L-2
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "three", LoreStateActive, 30) // L-3
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "four", LoreStatePinned, 10)  // L-4
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "five", LoreStateRetired, 99) // L-5

	got, err := d.ListLoreEntriesLive(LoreScopeAgent, "m-staff-1")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	wantLoreIDs(t, got, []string{"L-4", "L-2", "L-3", "L-1"})
}

// TestLoreLiveIsScopedToOneScope — a member's lore and a task type's lore share
// a table and must never leak into one another's fold. Both fixtures use the
// same scope_key string on purpose: a query that filtered on scope_key alone, or
// on scope_kind alone, would return both and this fails. That collision is not
// hypothetical since the scopes collapsed to two — an agent key is a member id
// and a manual key is a free-form type_key, and nothing stops one station
// choosing a type_key that reads like an id.
func TestLoreLiveIsScopedToOneScope(t *testing.T) {
	d := newTestDAL(t)
	seedLore(t, d, LoreScopeAgent, "shared-key", "member one", LoreStateActive, 10)  // L-1
	seedLore(t, d, LoreScopeManual, "shared-key", "manual one", LoreStateActive, 20) // L-2

	agent, err := d.ListLoreEntriesLive(LoreScopeAgent, "shared-key")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive(agent): %v", err)
	}
	wantLoreIDs(t, agent, []string{"L-1"})

	manual, err := d.ListLoreEntriesLive(LoreScopeManual, "shared-key")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive(manual): %v", err)
	}
	wantLoreIDs(t, manual, []string{"L-2"})
}

// TestLoreIDsAscendAndAreMintedOnce pins the display number: L-1, L-2, L-3 in
// write order, with seq agreeing with the id.
func TestLoreIDsAscendAndAreMintedOnce(t *testing.T) {
	d := newTestDAL(t)
	first := seedLore(t, d, LoreScopeAgent, "m-staff-1", "one", LoreStateActive, 1)
	second := seedLore(t, d, LoreScopeAgent, "m-staff-1", "two", LoreStateActive, 2)
	third := seedLore(t, d, LoreScopeManual, "some-type", "three", LoreStateActive, 3)

	if first.ID != "L-1" || second.ID != "L-2" || third.ID != "L-3" {
		t.Fatalf("minted ids %q/%q/%q, want L-1/L-2/L-3 — the counter is GLOBAL, "+
			"not per scope", first.ID, second.ID, third.ID)
	}
	if first.Seq != 1 || second.Seq != 2 || third.Seq != 3 {
		t.Fatalf("seq %d/%d/%d, want 1/2/3", first.Seq, second.Seq, third.Seq)
	}
}

// TestBumpMovesEffectiveAndLeavesCreated is the reversibility guarantee: 提到最新
// moves effective_ts and NOT created_ts, so the original date survives.
//
// Mutant that must redden this: writing created_ts in BumpLoreEntryEffective.
func TestBumpMovesEffectiveAndLeavesCreated(t *testing.T) {
	d := newTestDAL(t)
	e := seedLore(t, d, LoreScopeAgent, "m-staff-1", "one", LoreStateActive, 100)

	if ok, err := d.BumpLoreEntryEffective(e.ID, 900); err != nil || !ok {
		t.Fatalf("BumpLoreEntryEffective: ok=%v err=%v", ok, err)
	}
	after, err := d.GetLoreEntry(e.ID)
	if err != nil || after == nil {
		t.Fatalf("GetLoreEntry: %v / %v", after, err)
	}
	if after.EffectiveTS != 900 {
		t.Fatalf("effective_ts = %v, want 900", after.EffectiveTS)
	}
	if after.CreatedTS != 100 {
		t.Fatalf("created_ts = %v, want 100 — 提到最新 must leave the record of "+
			"when the entry was actually written, which is what makes it reversible",
			after.CreatedTS)
	}
}

// TestBumpReordersTheFold — the bump has to be visible in the ORDER, not only
// in the column. The expected list is written out for both moments.
func TestBumpReordersTheFold(t *testing.T) {
	d := newTestDAL(t)
	old := seedLore(t, d, LoreScopeAgent, "m-staff-1", "old", LoreStateActive, 10)
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "new", LoreStateActive, 20)

	before, err := d.ListLoreEntriesLive(LoreScopeAgent, "m-staff-1")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	wantLoreIDs(t, before, []string{"L-2", "L-1"})

	if _, err := d.BumpLoreEntryEffective(old.ID, 30); err != nil {
		t.Fatalf("BumpLoreEntryEffective: %v", err)
	}
	after, err := d.ListLoreEntriesLive(LoreScopeAgent, "m-staff-1")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	wantLoreIDs(t, after, []string{"L-1", "L-2"})
}

// TestRetireHidesFromTheFoldButKeepsTheRow — 失效 is not deletion. The entry
// leaves the fold and is still readable by id, with its reason.
func TestRetireHidesFromTheFoldButKeepsTheRow(t *testing.T) {
	d := newTestDAL(t)
	e := seedLore(t, d, LoreScopeAgent, "m-staff-1", "one", LoreStateActive, 10)

	if ok, err := d.SetLoreEntryState(e.ID, LoreStateRetired, "superseded", 50); err != nil || !ok {
		t.Fatalf("SetLoreEntryState: ok=%v err=%v", ok, err)
	}
	live, err := d.ListLoreEntriesLive(LoreScopeAgent, "m-staff-1")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	wantLoreIDs(t, live, []string{})

	row, err := d.GetLoreEntry(e.ID)
	if err != nil || row == nil {
		t.Fatalf("the row must still exist after 失效: %v / %v", row, err)
	}
	if row.State != LoreStateRetired || row.RetireReason != "superseded" {
		t.Fatalf("row = %+v, want retired with its reason", row)
	}
	// ...and the way back.
	if _, err := d.SetLoreEntryState(e.ID, LoreStateActive, "", 60); err != nil {
		t.Fatalf("un-retire: %v", err)
	}
	back, err := d.ListLoreEntriesLive(LoreScopeAgent, "m-staff-1")
	if err != nil {
		t.Fatalf("ListLoreEntriesLive: %v", err)
	}
	wantLoreIDs(t, back, []string{"L-1"})
}

// TestSetStateOnAnUnknownEntryReportsMiss — the DAL must say "nothing moved" so
// the handler can answer 404 instead of a silent 200.
func TestSetStateOnAnUnknownEntryReportsMiss(t *testing.T) {
	d := newTestDAL(t)
	ok, err := d.SetLoreEntryState("L-999", LoreStateRetired, "", 1)
	if err != nil {
		t.Fatalf("SetLoreEntryState: %v", err)
	}
	if ok {
		t.Fatal("SetLoreEntryState reported a hit on an entry that does not exist")
	}
	ok, err = d.BumpLoreEntryEffective("L-999", 1)
	if err != nil {
		t.Fatalf("BumpLoreEntryEffective: %v", err)
	}
	if ok {
		t.Fatal("BumpLoreEntryEffective reported a hit on an entry that does not exist")
	}
}

// TestLorePageOrderIsPinnedActiveRetired pins the LIST page's three groups, and
// pins that the filter narrows before the page is cut.
func TestLorePageOrderIsPinnedActiveRetired(t *testing.T) {
	d := newTestDAL(t)
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "a", LoreStateActive, 20)  // L-1
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "b", LoreStateRetired, 99) // L-2
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "c", LoreStatePinned, 1)   // L-3
	seedLore(t, d, LoreScopeAgent, "m-staff-1", "d", LoreStateActive, 30)  // L-4

	page, err := d.ListLoreEntriesPage(loreListFilter{}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage: %v", err)
	}
	// Hand-written: pinned (L-3) → active newest first (L-4, L-1) → retired (L-2).
	// L-2 has the newest effective_ts of all and still sorts LAST, which is what
	// separates "grouped by state" from "sorted by time".
	wantLoreIDs(t, page, []string{"L-3", "L-4", "L-1", "L-2"})

	only, err := d.ListLoreEntriesPage(loreListFilter{States: []string{LoreStateActive}}, 30, 0)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage(filtered): %v", err)
	}
	wantLoreIDs(t, only, []string{"L-4", "L-1"})

	// The window is applied to the FILTERED, ORDERED sequence — not to the table.
	second, err := d.ListLoreEntriesPage(loreListFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("ListLoreEntriesPage(paged): %v", err)
	}
	wantLoreIDs(t, second, []string{"L-1", "L-2"})
}
