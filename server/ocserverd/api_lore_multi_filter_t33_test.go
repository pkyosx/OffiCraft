package main

// api_lore_multi_filter_t33_test.go — T-33 傳承 list face: the four filter axes
// each carry a REPEATABLE plural spelling beside the frozen singular one
// (owner rc-0376bf875757 [1], the cockpit's three filters became multi-select).
//
// 🔴 WHAT THESE FOUR TESTS ARE FOR, and why each one asserts what it does:
//
//  1. The plural set really reaches SQL. A filter that is parsed, validated and
//     then not applied returns MORE rows than asked for, and "more rows" is the
//     one wrong answer a caller cannot spot — it looks like a scope that simply
//     has more in it. So the assertion is on the EXACT set of ids, not on
//     "every returned row is one of the wanted states" (which a no-op filter
//     would fail only by accident).
//
//  2. Plural beats singular. The two spellings are sent together by any client
//     mid-migration, so the case is real, not theoretical, and the two possible
//     rules (plural wins / singular wins) return DIFFERENT non-empty pages —
//     which is why the fixture below deliberately makes the singular name a
//     state the plural set does not contain.
//
//  3. An out-of-vocabulary element is a 400, NOT a quietly narrowed 200.
//     🔴 THE ASSERTION IS ON THE STATUS CODE. "the bad value was ignored" and
//     "the request was refused" both produce a page with nothing surprising in
//     it, so a test that only checked the rows would pass for both. It also
//     pins the message naming the offending VALUE and the PLURAL parameter,
//     because a refusal that says `state` when the caller sent `states` points
//     them at a field they never filled in.
//
//  4. The 上限線 in BOTH directions, in one test. Asserting only that two scopes
//     answer 0/"" is passed by an implementation that never reports a cap at
//     all; asserting only that one scope answers is passed by one that reports
//     the role budget over any page whatsoever. The value on the converged side
//     is checked too — WHICH entry is first dropped, not merely that one is.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// strsp is the plural params' pointer-to-slice, the shape oapi-codegen gives a
// repeatable query parameter (`?states=a&states=b`) — the same shape /api/tasks
// has carried for ?statuses= since T-a3e4.
func strsp(v ...string) *[]string { return &v }

// listLoreRaw is listLore without the 200 gate: tests that assert a REFUSAL
// need the recorder, and a helper that fataled on a non-200 would make the
// 400 case unwritable.
func listLoreRaw(t *testing.T, s *apiServer, sub string,
	params HandleListLoreEntriesApiLoreGetParams) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListLoreEntriesApiLoreGet(rec,
		taskReq(t, "GET", "/api/lore", nil, sub, "agent"), params)
	return rec
}

// seedLoreStates writes three entries under the caller's own role and puts each
// one in a different state, returning their ids keyed by state. Three states,
// because a two-value plural filter can only be shown to NARROW if there is a
// third row it must leave out.
func seedLoreStates(t *testing.T, s *apiServer, me string) map[string]string {
	t.Helper()
	byState := map[string]string{}
	for _, tc := range []struct{ title, state string }{
		{"活的一筆", LoreStateActive},
		{"釘的一筆", LoreStatePinned},
		{"廢的一筆", LoreStateRetired},
	} {
		rec := postLore(t, s, me, map[string]any{"title": tc.title, "body": "內容"})
		if rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", tc.title, rec.Code, rec.Body.String())
		}
		var dto LoreEntryWriteReceiptDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode seed receipt: %v", err)
		}
		if tc.state != LoreStateActive {
			ok, err := s.dal.SetLoreEntryState(dto.Id, tc.state, "", nowSecs())
			if err != nil || !ok {
				t.Fatalf("SetLoreEntryState(%s, %s): %v / %v", dto.Id, tc.state, ok, err)
			}
		}
		byState[tc.state] = dto.Id
	}
	return byState
}

func loreIdsOf(page LoreEntryListDTO) map[string]bool {
	out := map[string]bool{}
	for _, e := range page.Entries {
		out[e.Id] = true
	}
	return out
}

// TestPluralStateFilterReturnsOnlyTheStatesAskedFor — axis 1 of the ruling:
// ?states= narrows, and it narrows in SQL. The scope holds one entry in each of
// the three states; two are asked for; the third must not come back.
func TestPluralStateFilterReturnsOnlyTheStatesAskedFor(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-multi-1", defaultBootRole)
	ids := seedLoreStates(t, s, me)

	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		States: strsp(LoreStatePinned, LoreStateRetired),
	})
	got := loreIdsOf(page)
	if len(page.Entries) != 2 {
		t.Fatalf("?states=pinned&states=retired returned %d entries, want exactly 2 "+
			"— the third row is active and was not asked for. %d means the IN "+
			"clause never reached the query and the page is unfiltered.",
			len(page.Entries), len(page.Entries))
	}
	if !got[ids[LoreStatePinned]] || !got[ids[LoreStateRetired]] {
		t.Fatalf("the two states asked for are %q and %q; the page carries %v",
			ids[LoreStatePinned], ids[LoreStateRetired], got)
	}
	if got[ids[LoreStateActive]] {
		t.Fatalf("the active entry %q came back from a page that asked only for "+
			"pinned and retired", ids[LoreStateActive])
	}
	for _, e := range page.Entries {
		if e.State != LoreStatePinned && e.State != LoreStateRetired {
			t.Fatalf("entry %q is in state %q, which was not in the set", e.Id, e.State)
		}
	}
}

// TestPluralFilterBeatsTheSingularWhenBothAreSent — the precedence rule, pinned
// where it is decidable. `state=active` names the ONE row the plural set leaves
// out, so "plural wins" and "singular wins" return disjoint, non-empty pages
// and no implementation can satisfy both.
func TestPluralFilterBeatsTheSingularWhenBothAreSent(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-multi-2", defaultBootRole)
	ids := seedLoreStates(t, s, me)

	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		State:  strp(LoreStateActive),
		States: strsp(LoreStatePinned, LoreStateRetired),
	})
	got := loreIdsOf(page)
	if got[ids[LoreStateActive]] {
		t.Fatalf("the page carries the ACTIVE entry %q — the singular ?state= won "+
			"over the plural ?states=, or the two were ANDed/unioned. When both "+
			"are sent the plural IS the filter and the singular is ignored.",
			ids[LoreStateActive])
	}
	if len(page.Entries) != 2 ||
		!got[ids[LoreStatePinned]] || !got[ids[LoreStateRetired]] {
		t.Fatalf("want exactly the pinned (%q) and retired (%q) entries, got %d: %v",
			ids[LoreStatePinned], ids[LoreStateRetired], len(page.Entries), got)
	}
}

// TestBadPluralFilterValueIsRefusedNotIgnored — the 400, asserted on the STATUS
// CODE. 「查無資料」 and 「你打錯字」 are the same picture on the wire, and an
// element silently dropped out of a SET is worse than a dropped scalar: some
// rows still arrive, so the answer looks whole.
func TestBadPluralFilterValueIsRefusedNotIgnored(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-multi-3", defaultBootRole)
	seedLoreStates(t, s, me)

	for _, tc := range []struct {
		name   string
		params HandleListLoreEntriesApiLoreGetParams
		bad    string
		param  string
	}{
		{"a bad state beside a good one",
			HandleListLoreEntriesApiLoreGetParams{
				States: strsp(LoreStateActive, "retried")},
			"retried", "states"},
		{"a bad scope_kind beside a good one",
			HandleListLoreEntriesApiLoreGetParams{
				ScopeKinds: strsp(LoreScopeAgent, "roles")},
			"roles", "scope_kinds"},
		// 🔴 `role` IS THE RETIRED VALUE AND IS NOW ITSELF OUT OF VOCABULARY.
		// It gets its own row because it is the one bad value a real client is
		// likely to send — every cockpit and every script written before the
		// 2026-09-07 collapse (card rc-a43100fd0486 [0]) knows it. Answering
		// 200-with-no-rows would tell those callers their 傳承 was deleted; the
		// 400 tells them their vocabulary is old, which is the true thing.
		{"the retired `role` kind, alone",
			HandleListLoreEntriesApiLoreGetParams{
				ScopeKinds: strsp("role")},
			"role", "scope_kinds"},
		{"the retired `role` kind beside a live one",
			HandleListLoreEntriesApiLoreGetParams{
				ScopeKinds: strsp(LoreScopeManual, "role")},
			"role", "scope_kinds"},
	} {
		rec := listLoreRaw(t, s, me, tc.params)
		// 🔴 THE STATUS CODE IS THE ASSERTION. An implementation that skipped the
		// unknown element would answer 200 with a perfectly plausible page, and a
		// test asserting only on the rows could not tell the two apart.
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: got %d %s — an out-of-vocabulary element must be REFUSED, "+
				"never dropped. A 200 here means the caller's typo was read as an "+
				"answer.", tc.name, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, tc.bad) {
			t.Fatalf("%s: the refusal does not name the offending value %q: %s",
				tc.name, tc.bad, body)
		}
		if !strings.Contains(body, tc.param) {
			t.Fatalf("%s: the refusal does not name %q, the parameter the caller "+
				"actually sent: %s", tc.name, tc.param, body)
		}
	}
}

// TestCapLineAnswersForOneScopeAndGoesQuietForSeveral — the 上限線 rule extended
// to the plural axes, BOTH DIRECTIONS IN ONE TEST.
//
// 🔴 Half of this test alone proves nothing. Checking only that two scopes give
// 0/"" is passed by an implementation that never answers a cap at all; checking
// only that one scope answers is passed by one that answers for any page. The
// converged half also pins WHICH entry is first dropped — a cap_chars with an
// arbitrary first_dropped_id beside it is a line in the wrong place, and a line
// in the wrong place looks exactly like a line in the right place.
func TestCapLineAnswersForOneScopeAndGoesQuietForSeveral(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-multi-4", defaultBootRole)
	// 20 characters of room; each entry costs 5 (3-character title + 2-character
	// body), so four fit exactly and the FIFTH in fold order is the first drop.
	s.loreCapCharsRole = 20
	s.loreCapCharsManual = 20

	for _, ti := range []string{"甲甲甲", "乙乙乙", "丙丙丙", "丁丁丁", "戊戊戊", "己己己"} {
		if rec := postLore(t, s, me, map[string]any{
			"title": ti, "body": "內容"}); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", ti, rec.Code, rec.Body.String())
		}
	}
	// A SECOND scope, so that a two-value scope_kinds set names two scopes that
	// really exist rather than one real one and one empty one.
	typed, err := s.dal.CreateTaskMintingID(Task{
		Title: "一張有類型的任務", TypeKey: "tm-multi", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}
	if rec := postLore(t, s, me, map[string]any{
		"title": "庚庚庚", "body": "內容", "task_id": typed.ID}); rec.Code != http.StatusOK {
		t.Fatalf("seed manual: %d %s", rec.Code, rec.Body.String())
	}

	// ── DIRECTION 1: exactly one kind and exactly one key ⇒ the line IS drawn,
	// and it names the right entry. ────────────────────────────────────────────
	one := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKinds: strsp(LoreScopeAgent), ScopeKeys: strsp(me),
		Limit: intp(30),
	})
	if one.CapChars != 20 {
		t.Fatalf("cap_chars = %d on a page that converged on ONE scope, want the "+
			"cap actually in force (20). 0 means the plural axes never count as "+
			"converging and the 上限線 is silently gone for every multi-select "+
			"client.", one.CapChars)
	}
	if len(one.Entries) != 6 {
		t.Fatalf("the writer's own scope holds 6 entries, the page returned %d",
			len(one.Entries))
	}
	if one.FirstDroppedId == "" {
		t.Fatalf("six entries costing 5 each against a cap of 20 must drop one, " +
			"but first_dropped_id is empty")
	}
	// WHICH one: four fit, so the first drop is at index 4 of the fold's order —
	// newest effective first, which is the order this page is already in.
	idx := -1
	for i, e := range one.Entries {
		if e.Id == one.FirstDroppedId {
			idx = i
			break
		}
	}
	if idx != 4 {
		t.Fatalf("first_dropped_id %q sits at position %d of the ordered scope; "+
			"four entries of 5 characters fit in a 20-character budget, so the "+
			"first one dropped is position 4. A line at %d greys the wrong rows.",
			one.FirstDroppedId, idx, idx)
	}

	// ── DIRECTION 2: two of either axis ⇒ no cap and no line. ─────────────────
	for _, tc := range []struct {
		name   string
		params HandleListLoreEntriesApiLoreGetParams
	}{
		{"two kinds, one key", HandleListLoreEntriesApiLoreGetParams{
			ScopeKinds: strsp(LoreScopeAgent, LoreScopeManual),
			ScopeKeys:  strsp(me)}},
		{"one kind, two keys", HandleListLoreEntriesApiLoreGetParams{
			ScopeKinds: strsp(LoreScopeAgent),
			ScopeKeys:  strsp(me, "m-somebody-else")}},
		{"two of each", HandleListLoreEntriesApiLoreGetParams{
			ScopeKinds: strsp(LoreScopeAgent, LoreScopeManual),
			ScopeKeys:  strsp(me, "tm-multi")}},
	} {
		page := listLoreRaw(t, s, me, tc.params)
		if page.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.name, page.Code, page.Body.String())
		}
		var out LoreEntryListDTO
		if err := json.Unmarshal(page.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if out.CapChars != 0 || out.FirstDroppedId != "" {
			t.Fatalf("%s: got cap_chars=%d first_dropped_id=%q — a budget belongs "+
				"to ONE scope, so a page spanning several has no single cap to "+
				"report and no honest place to draw a line", tc.name,
				out.CapChars, out.FirstDroppedId)
		}
	}
}

// TestEachAxisFiltersOnItsOwnColumn — the fifth property, and the one the four
// above cannot see.
//
// 🔴 THE FAILURE THIS EXISTS FOR IS A CROSSED WIRE, NOT A MISSING ONE. The DAL
// builds its WHERE from a literal table of (column, values) pairs:
//
//	{"scope_kind", f.ScopeKinds}, {"scope_key", f.ScopeKeys},
//	{"state", f.States},          {"author_id", f.AuthorIDs}
//
// Swap two rows of that table and everything still compiles, every axis still
// narrows, and the tests above still pass: they exercise `state`, which would
// keep working, and the shared IN-clause builder, which is not what broke. What
// changes is only WHICH column each set is compared against — so the page comes
// back the right SIZE with the wrong ROWS, and a caller reading it has no way to
// tell.
//
// So the fixture is built to be CROSSABLE on purpose: entry A's scope_key is
// entry B's author_id and vice versa. Filtering by "alpha" therefore has two
// different right-looking answers, and only the correctly wired axis gives the
// one this test demands. A fixture with distinct values everywhere would return
// an EMPTY page under a swap — which is also a failure, but a weaker one: an
// empty page is what a typo gives you too.
func TestEachAxisFiltersOnItsOwnColumn(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-multi-axes", defaultBootRole)

	mk := func(title, scopeKey, authorID string) string {
		t.Helper()
		e, err := s.dal.CreateLoreEntryMintingID(LoreEntry{
			ScopeKind:   LoreScopeAgent,
			ScopeKey:    scopeKey,
			Title:       title,
			Body:        "body of " + title,
			AuthorID:    authorID,
			State:       LoreStateActive,
			EffectiveTS: 100,
			CreatedTS:   100,
			UpdatedTS:   100,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
		return e.ID
	}
	// The cross: A is scoped to alpha and written by beta; B is the mirror.
	a := mk("A", "alpha", "beta")
	b := mk("B", "beta", "alpha")
	c := mk("C", "gamma", "gamma")

	for _, tc := range []struct {
		name   string
		params HandleListLoreEntriesApiLoreGetParams
		want   string
		wrong  string
	}{
		{
			name:   "scope_keys=alpha",
			params: HandleListLoreEntriesApiLoreGetParams{ScopeKeys: strsp("alpha")},
			want:   a,
			wrong:  b, // what a scope_key↔author_id swap would return instead
		},
		{
			name:   "author_ids=alpha",
			params: HandleListLoreEntriesApiLoreGetParams{AuthorIds: strsp("alpha")},
			want:   b,
			wrong:  a,
		},
	} {
		page := listLore(t, s, me, tc.params)
		got := loreIdsOf(page)
		if len(page.Entries) != 1 || !got[tc.want] {
			t.Fatalf("%s: want exactly [%s], got %v — the set was compared against "+
				"the wrong column, or against none", tc.name, tc.want, got)
		}
		if got[tc.wrong] {
			t.Fatalf("%s: %s came back. That is the row the OTHER axis holds "+
				"\"alpha\" on, so this set reached the wrong column", tc.name, tc.wrong)
		}
		if got[c] {
			t.Fatalf("%s: %s came back and it carries \"alpha\" on neither axis — "+
				"the filter did not narrow at all", tc.name, c)
		}
	}
}
