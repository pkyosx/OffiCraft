package main

// api_lore_cutoff_t33_test.go — T-33 spec §6 上限線: GET /api/lore reports WHICH
// entry the fold drops, and it reports it from the fold's own selector.
//
// 🔴 THE EXPECTATION IS THE ASSEMBLED BOOT DOCUMENT, not a second run of
// selectLoreForScope. A test that re-runs the production function and compares
// the answer to itself passes for every possible implementation of it — that is
// the trap api_lore_t33_test.go's header names. Here the entry the list calls
// "first dropped" is looked for in the document the server really produced: it
// must be ABSENT, and the entry above it must be PRESENT. If the line and the
// fold ever disagree, one of those two literals fails.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func listLore(t *testing.T, s *apiServer, sub string,
	params HandleListLoreEntriesApiLoreGetParams) LoreEntryListDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListLoreEntriesApiLoreGet(rec,
		taskReq(t, "GET", "/api/lore", nil, sub, "agent"), params)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var out LoreEntryListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

// TestListNamesTheEntryTheFoldActuallyDropped is the whole point of the two new
// fields. The page asked for is ONE row long while the scope holds six, which is
// exactly the case a client-side computation gets wrong: everything it can see
// fits, so it would draw no line at all.
func TestListNamesTheEntryTheFoldActuallyDropped(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-cut-1", defaultBootRole)
	// 20 characters of room. Each entry below costs 5 (a 3-character title and a
	// 2-character body), so four fit exactly and the fifth is the first drop.
	s.loreCapCharsRole = 20

	titles := []string{"標題一", "標題二", "標題三", "標題四", "標題五", "標題六"}
	for _, ti := range titles {
		if rec := postLore(t, s, me, map[string]any{
			"title": ti, "body": "內容"}); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", ti, rec.Code, rec.Body.String())
		}
	}

	// Newest effective first, so the display order is the reverse of the write
	// order and the drop falls in the middle of the list, not at its tail.
	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKind: strp(LoreScopeRole), ScopeKey: strp(defaultBootRole),
		Limit: intp(1),
	})
	if page.CapChars != 20 {
		t.Fatalf("cap_chars = %d, want the cap actually in force (20)", page.CapChars)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("asked for one row, got %d — the page size is not being applied",
			len(page.Entries))
	}
	if page.FirstDroppedId == "" {
		t.Fatalf("six entries against a cap of 20 characters must drop one, but " +
			"first_dropped_id is empty — the line was computed over the PAGE " +
			"instead of over the scope")
	}

	// Which titles those ids carry, and which entry sits immediately above the
	// line, read off the full ordered list.
	full := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKind: strp(LoreScopeRole), ScopeKey: strp(defaultBootRole),
		Limit: intp(30),
	})
	if len(full.Entries) != len(titles) {
		t.Fatalf("full list has %d entries, want %d", len(full.Entries), len(titles))
	}
	if full.FirstDroppedId != page.FirstDroppedId {
		t.Fatalf("the line moved when the page size changed (%q with limit=30, %q "+
			"with limit=1) — it is being derived from the rows returned rather "+
			"than from the scope", full.FirstDroppedId, page.FirstDroppedId)
	}
	idx := -1
	for i, e := range full.Entries {
		if e.Id == full.FirstDroppedId {
			idx = i
			break
		}
	}
	if idx <= 0 {
		t.Fatalf("first_dropped_id %q is at position %d of the list — it must name "+
			"a real entry that is not the very first one here",
			full.FirstDroppedId, idx)
	}
	droppedTitle := full.Entries[idx].Title
	keptTitle := full.Entries[idx-1].Title

	// Ground truth: the document the fold really produced.
	m, err := s.dal.GetMember(me)
	if err != nil || m == nil {
		t.Fatalf("GetMember: %v / %v", m, err)
	}
	ctx, err := s.buildBootContext("", m)
	if err != nil || ctx == nil {
		t.Fatalf("buildBootContext: %v / %v", ctx, err)
	}
	doc := ctx.Context
	if !strings.Contains(doc, keptTitle) {
		t.Fatalf("%q sits immediately ABOVE the line the list drew, so the fold "+
			"must have carried it — but it is not in the boot document. The line "+
			"is above the wrong entry.", keptTitle)
	}
	if strings.Contains(doc, droppedTitle) {
		t.Fatalf("the list says %q is the first entry that does NOT fit, but the "+
			"boot document carries it. The line and the fold disagree.",
			droppedTitle)
	}
}

// TestListReportsTheManualCapForAManualScope — the two budgets are separate
// settings and are never summed, so the page has to report the one belonging to
// the scope it was asked about. Reporting the role budget for a manual page
// would draw the line in the wrong place with no error anywhere, and the two
// caps are equal at their shipped defaults, which is exactly the condition
// under which such a bug is invisible. They are set APART here so that they
// cannot both be right.
func TestListReportsTheManualCapForAManualScope(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-cut-4", defaultBootRole)
	s.loreCapCharsRole = 9000
	s.loreCapCharsManual = 20

	typed, err := s.dal.CreateTaskMintingID(Task{
		Title: "一張有類型的任務", TypeKey: "tm-review", ExecutorKind: TaskExecutorStaff,
		ExecutorID: me, CreatorID: me, Priority: TaskPriorityMid,
		CreatedTS: 1, UpdatedTS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("CreateTaskMintingID: %v", err)
	}
	for _, ti := range []string{"庚庚庚", "辛辛辛", "壬壬壬", "癸癸癸", "寅寅寅", "卯卯卯"} {
		if rec := postLore(t, s, me, map[string]any{
			"title": ti, "body": "內容", "task_id": typed.ID}); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", ti, rec.Code, rec.Body.String())
		}
	}

	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKind: strp(LoreScopeManual), ScopeKey: strp("tm-review"),
	})
	if page.CapChars != 20 {
		t.Fatalf("cap_chars = %d on a MANUAL page, want the manual budget (20). "+
			"9000 means the role budget is being reported for both scopes.",
			page.CapChars)
	}
	if page.FirstDroppedId == "" {
		t.Fatalf("six entries against the manual budget of 20 characters must " +
			"drop one, but first_dropped_id is empty")
	}

	// Ground truth for exit 2 is the manual read, not the boot document.
	full := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKind: strp(LoreScopeManual), ScopeKey: strp("tm-review"), Limit: intp(30),
	})
	idx := -1
	for i, e := range full.Entries {
		if e.Id == full.FirstDroppedId {
			idx = i
			break
		}
	}
	if idx <= 0 {
		t.Fatalf("first_dropped_id %q is at position %d — it must name a real "+
			"entry that is not the first one", full.FirstDroppedId, idx)
	}
	sel, err := selectLoreForScope(s.dal, LoreScopeManual, "tm-review", 20)
	if err != nil {
		t.Fatalf("selectLoreForScope: %v", err)
	}
	rendered := renderLoreBlock(sel)
	if !strings.Contains(rendered, full.Entries[idx-1].Title) {
		t.Fatalf("%q sits immediately above the line, so exit 2 must carry it — "+
			"it is not in the rendered 傳承 block", full.Entries[idx-1].Title)
	}
	if strings.Contains(rendered, full.Entries[idx].Title) {
		t.Fatalf("the list calls %q the first entry that does not fit, but exit 2 "+
			"carries it", full.Entries[idx].Title)
	}
}

// TestListDrawsNoLineWhenTheFilterNamesNoSingleScope — a budget belongs to a
// scope. Reporting one scope's cap over a page that spans several would put a
// line in a list where no single line means anything, and the reader has no way
// to see that it is meaningless.
func TestListDrawsNoLineWhenTheFilterNamesNoSingleScope(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-cut-2", defaultBootRole)
	s.loreCapCharsRole = 20
	for _, ti := range []string{"甲甲甲", "乙乙乙", "丙丙丙", "丁丁丁", "戊戊戊", "己己己"} {
		if rec := postLore(t, s, me, map[string]any{
			"title": ti, "body": "內容"}); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", ti, rec.Code, rec.Body.String())
		}
	}

	for _, tc := range []struct {
		name   string
		params HandleListLoreEntriesApiLoreGetParams
	}{
		{"no filter at all", HandleListLoreEntriesApiLoreGetParams{}},
		{"kind without key", HandleListLoreEntriesApiLoreGetParams{
			ScopeKind: strp(LoreScopeRole)}},
		{"key without kind", HandleListLoreEntriesApiLoreGetParams{
			ScopeKey: strp(defaultBootRole)}},
	} {
		page := listLore(t, s, me, tc.params)
		if page.CapChars != 0 || page.FirstDroppedId != "" {
			t.Fatalf("%s: got cap_chars=%d first_dropped_id=%q — a page that does "+
				"not converge on one scope has no cap to report and no honest "+
				"place to draw a line", tc.name, page.CapChars, page.FirstDroppedId)
		}
	}
}

// TestListReportsNoDropWhenEverythingFits — the other end of the same field.
// A non-empty first_dropped_id on a scope that fits would grey out entries the
// reader is in fact getting, and nothing would say so.
func TestListReportsNoDropWhenEverythingFits(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-cut-3", defaultBootRole)
	s.loreCapCharsRole = 10000
	for _, ti := range []string{"子子子", "丑丑丑"} {
		if rec := postLore(t, s, me, map[string]any{
			"title": ti, "body": "內容"}); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", ti, rec.Code, rec.Body.String())
		}
	}
	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		ScopeKind: strp(LoreScopeRole), ScopeKey: strp(defaultBootRole),
	})
	if page.CapChars != 10000 {
		t.Fatalf("cap_chars = %d, want 10000", page.CapChars)
	}
	if page.FirstDroppedId != "" {
		t.Fatalf("two short entries against a cap of 10000 drop nothing, but "+
			"first_dropped_id = %q", page.FirstDroppedId)
	}
}

// TestTheListFaceGoesThroughTheOneSelectorToo extends the guard in
// api_lore_t33_test.go to the third caller. The two folds were the two the spec
// named; the 上限線 is a THIRD place the same rule is needed, and restating it
// here rather than calling the selector is how the line starts pointing at a
// different entry from the one the fold drops — silently, since both look right.
func TestTheListFaceGoesThroughTheOneSelectorToo(t *testing.T) {
	src := readSourceForLoreGuard(t, "api_lore.go")
	if !strings.Contains(src, "selectLoreForScope(") {
		t.Fatalf("api_lore.go no longer calls selectLoreForScope — the 上限線 is " +
			"being computed from a second copy of the picking rule")
	}
}
