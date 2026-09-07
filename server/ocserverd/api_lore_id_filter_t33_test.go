package main

// api_lore_id_filter_t33_test.go — the 傳承編號 axis of the list face.
//
// WHY THE AXIS EXISTS AT ALL. Owner 2026-09-08: 「還有傳承編號的搜尋功能」「跟
// task 一樣」. 任務頁 has had a 任務編號 field since T-118; the 傳承 page's own
// design (LORE_SPEC.md §6) puts one in the first cell of its filter row, and the
// sentence that once explained its absence was written to justify the code, not
// to decide the design — that is recorded in the spec as a mistake, not a rule.
//
// 🔴 WHY THE AXIS IS ON THE SERVER, which is what these tests actually pin.
// This list is scroll-to-load (30 rows a batch). An id narrowed in the client
// narrows only the batch already downloaded, so 「這個編號不存在」 and 「它在下一
// 批，你還沒捲到」 become the same empty screen, and the 上限線 — drawn from the
// server's own fold — lands beside rows the client has since removed. Every test
// below therefore drives the HANDLER and asserts on the page it answers; none of
// them filters a slice in Go.
//
// 🔴 WHAT EACH TEST IS FOR, and why a weaker assertion would not do:
//
//  1. Found: the id reaches SQL. Asserted as the EXACT id set of length 1, not
//     as 「the wanted entry is somewhere in the page」 — an axis that is parsed
//     and then never applied passes the weak form on every run, because the
//     wanted entry IS in the unfiltered page.
//
//  2. Not found: an id nobody carries answers an EMPTY page. This is the test
//     the whole axis is judged by. The failure it guards is not an error — it is
//     the FULL LIST coming back, which reads as 「你要的那筆不在這裡，但這裡有
//     一堆」 rather than 「查無此編號」.
//
//  3. Exact, never substring. `L-1` must not drag in `L-10`. 「跟 task 一樣」 is
//     literal: 任務頁 resolves a committed id with `api.getTask(anchorId)`
//     (useTasks.ts:201) and pairs it by equality (`x.id === appliedId`,
//     TasksPage.tsx:458). A LIKE-based axis would also break paging — the extra
//     rows are cut by the same limit/offset, so the row asked for can fall off
//     the end of the batch.
//
//  4. AND, not OR. The id axis meets the other four the way they meet each
//     other. The fixture makes the two constraints disagree ON PURPOSE (an id
//     whose entry is `active`, asked for alongside `states=pinned`), because
//     that is the only shape where AND and OR give different answers; a fixture
//     where both agree is passed by either.
//
//  5. An EMPTY id set is 「do not narrow」, never 「match nothing」. It has to be
//     byte-identical to not sending the parameter, because that is what the
//     cockpit sends when the search box is cleared — an implementation that read
//     the empty set as an impossible constraint would answer a blank page to
//     someone who had just asked to see everything, and there is no error on the
//     wire to explain it. Both spellings of empty are covered: absent, and
//     present-but-blank.

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"testing"
)

// seedLoreEntries writes n entries under the caller's own scope and returns
// their ids in write order. It returns ids rather than titles because the id is
// the thing under test — the number the cockpit shows and a reader copies out.
func seedLoreEntries(t *testing.T, s *apiServer, me string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rec := postLore(t, s, me, map[string]any{
			"title": "標題", "body": "內容"})
		if rec.Code != http.StatusOK {
			t.Fatalf("seed #%d: %d %s", i, rec.Code, rec.Body.String())
		}
		var dto LoreEntryWriteReceiptDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode seed receipt #%d: %v", i, err)
		}
		ids = append(ids, dto.Id)
	}
	return ids
}

// sortedLoreIds is the page's id set as a comparable, ORDER-INDEPENDENT value.
// The list's own order is the fixed three-group one and is not what this file
// tests; comparing sorted sets keeps a failure here about the FILTER.
func sortedLoreIds(page LoreEntryListDTO) []string {
	out := make([]string, 0, len(page.Entries))
	for _, e := range page.Entries {
		out = append(out, e.Id)
	}
	sort.Strings(out)
	return out
}

// The ordered comparison itself is `sameIDs` in api_chat_envelope_t48_test.go —
// same package, one copy. A second one here would be a second thing to keep
// right for no gain.

// TestEntryIdFilterServesExactlyThatOneEntry — the axis narrows, in SQL.
func TestEntryIdFilterServesExactlyThatOneEntry(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-id-1", defaultBootRole)
	ids := seedLoreEntries(t, s, me, 3)
	want := ids[1]

	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		EntryIds: strsp(want),
	})
	if got := sortedLoreIds(page); !sameIDs(got, []string{want}) {
		t.Fatalf("?entry_ids=%s served %v, want exactly [%s]. More than one id "+
			"back means the clause never reached the query — an unfiltered page "+
			"contains the wanted row too, so 「it is in there」 proves nothing.",
			want, got, want)
	}
}

// TestUnknownEntryIdServesAnEmptyPageNotEveryEntry — 🔴 THE ONE THAT MATTERS.
// A number nobody carries must answer 「沒有」, not 「這裡有三筆，都不是你要的」.
func TestUnknownEntryIdServesAnEmptyPageNotEveryEntry(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-id-2", defaultBootRole)
	seedLoreEntries(t, s, me, 3)

	// Well-formed and unclaimed: it is what a typo actually looks like. A
	// deliberately malformed string would pass even against an implementation
	// that only rejects garbage, which is not the behaviour being pinned.
	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		EntryIds: strsp(loreIDPrefix + "999999"),
	})
	if len(page.Entries) != 0 {
		t.Fatalf("?entry_ids=%s999999 served %d entries, want 0. This is the whole "+
			"axis: an unknown number that comes back with the full list tells the "+
			"reader their entry is missing when what is missing is the filter.",
			loreIDPrefix, len(page.Entries))
	}
}

// TestEntryIdMatchesTheWholeIdNotAPrefix — 「跟 task 一樣」 read literally.
func TestEntryIdMatchesTheWholeIdNotAPrefix(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-id-3", defaultBootRole)
	// Twelve entries, so both L-1 and L-1x exist in the same table and a
	// prefix/substring axis has something to wrongly drag in.
	ids := seedLoreEntries(t, s, me, 12)
	first := ids[0]

	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		EntryIds: strsp(first),
	})
	if got := sortedLoreIds(page); !sameIDs(got, []string{first}) {
		t.Fatalf("?entry_ids=%s served %v, want exactly [%s]. The extra rows are "+
			"the ids this one is a PREFIX of — a substring/LIKE axis, which is not "+
			"what 任務頁 does (equality: TasksPage.tsx:458) and which loses rows to "+
			"limit/offset once the batch is full.", first, got, first)
	}
}

// TestEntryIdIsANDedWithTheOtherAxes — one page, two constraints, both binding.
func TestEntryIdIsANDedWithTheOtherAxes(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-id-4", defaultBootRole)
	byState := seedLoreStates(t, s, me)
	activeID := byState[LoreStateActive]

	// The two constraints DISAGREE: this id names the active entry, and the page
	// asks for pinned. AND ⇒ nothing. OR (or a dropped state axis) ⇒ the active
	// entry, the pinned entry, or both.
	page := listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		EntryIds: strsp(activeID),
		States:   strsp(LoreStatePinned),
	})
	if len(page.Entries) != 0 {
		t.Fatalf("?entry_ids=%s&states=%s served %v, want 0 — %s is active, so the "+
			"two constraints cannot both hold. Any row here means the axes were "+
			"unioned, or the other axis stopped being applied at all.",
			activeID, LoreStatePinned, sortedLoreIds(page), activeID)
	}

	// The positive control, in the same test: with the constraints AGREEING the
	// page is not empty. Without it, an axis that always returns nothing would
	// pass the half above — and an always-empty filter is a live failure mode,
	// not a hypothetical one (it is what `IN ()` would mean if it parsed).
	page = listLore(t, s, me, HandleListLoreEntriesApiLoreGetParams{
		EntryIds: strsp(activeID),
		States:   strsp(LoreStateActive),
	})
	if got := sortedLoreIds(page); !sameIDs(got, []string{activeID}) {
		t.Fatalf("?entry_ids=%s&states=%s served %v, want exactly [%s] — the two "+
			"constraints agree here, so an empty page means the AND above passed "+
			"only because this filter never matches anything.",
			activeID, LoreStateActive, got, activeID)
	}
}

// TestEmptyEntryIdSetIsTheSameAsNotSendingIt — an empty set is 「不縮小」.
func TestEmptyEntryIdSetIsTheSameAsNotSendingIt(t *testing.T) {
	s := loreTestServer(t)
	me := hireLoreStaff(t, s, "m-lore-id-5", defaultBootRole)
	// 🔴 THE EXPECTATION COMES FROM THE SEED, NOT FROM A BASELINE CALL. An earlier
	// draft read the unfiltered page first and compared the empty-set pages to it,
	// and under the 「empty set ⇒ match nothing」 mutant that draft went red on the
	// BASELINE — a precondition — rather than on the comparison this test is
	// named for. Both are failures, but only one of them says what broke.
	want := append([]string(nil), seedLoreEntries(t, s, me, 3)...)
	sort.Strings(want)

	for _, tc := range []struct {
		name   string
		params HandleListLoreEntriesApiLoreGetParams
	}{
		// The reference: this axis absent altogether. Every case below it has to
		// be indistinguishable from this one.
		{"no entry-id parameter at all", HandleListLoreEntriesApiLoreGetParams{}},
		// The cockpit's cleared search box, in every shape it can arrive as.
		{"?entry_ids= with no values", HandleListLoreEntriesApiLoreGetParams{
			EntryIds: strsp()}},
		{"?entry_ids= carrying only blanks", HandleListLoreEntriesApiLoreGetParams{
			EntryIds: strsp("", "   ")}},
		{"?entry_id= empty singular", HandleListLoreEntriesApiLoreGetParams{
			EntryId: strp("  ")}},
	} {
		got := sortedLoreIds(listLore(t, s, me, tc.params))
		if !sameIDs(got, want) {
			t.Fatalf("%s served %v, want all three seeded entries %v. An empty set "+
				"means 「do not narrow on this axis」 — reading it as 「match nothing」 "+
				"hands a blank list to someone who just cleared the box, with nothing "+
				"on the wire to explain why.", tc.name, got, want)
		}
	}
}

// ── the axis has to reach an AGENT, not only a browser ───────────────────────
//
// 🔴 WHY THIS TEST IS NOT A DUPLICATE OF THE REST OF THE FILE. Everything above
// drives the Go handler, and a handler that reads a parameter no tool advertises
// is reachable by the cockpit and by nobody else. Agents do not call REST; they
// call the tools in spec/mcp-catalog.json, which ocserverd serves verbatim.
//
// WHAT WAS ACTUALLY MISSING. There is no `get_lore_entry` tool — the whole lore
// surface an agent has is write_lore_entry / list_lore_entries /
// set_lore_entry_state / bump_lore_entry — so before this axis existed an agent
// had NO WAY AT ALL to name one entry. It could page the list and hope the one
// it wanted was in the batch. That is the gap owner named on 2026-09-08 (「agent
// 沒有能力取得單一 lore 嗎 這樣要怎麼跟我互動討論某個 lore」), and it is why
// `list_lore_entries` + one id IS this repo's single-entry read.
//
// 🔴 THE CATALOG IS NOT DERIVED FROM THE PARAMETER LIST, which is the trap this
// test exists for. bin/gen-mcp-catalog copies `x-mcp.legacy.descriptor` — a
// hand-written JSON fragment — out of openapi.json verbatim; adding a query
// parameter beside it changes NOTHING in the catalog, and the generator still
// prints success. So 「the REST param is there」 is not evidence for 「the tool
// has it」, and only reading the served catalog is.
func TestListLoreEntriesToolAdvertisesTheEntryIdAxis(t *testing.T) {
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read spec/mcp-catalog.json: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode spec/mcp-catalog.json: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name != "list_lore_entries" {
			continue
		}
		for _, want := range []string{"entry_ids", "entry_id"} {
			if _, ok := tool.InputSchema.Properties[want]; !ok {
				t.Fatalf("the list_lore_entries TOOL has no %q parameter. An agent "+
					"cannot ask for one 傳承 by its number, and there is no "+
					"get_lore_entry tool to fall back to — so 「agent 起疑時自己讀原文」 "+
					"is not something it can do. The parameter existing on GET /api/lore "+
					"does not carry it here: gen-mcp-catalog copies x-mcp.legacy."+
					"descriptor verbatim and reports success either way. Tool "+
					"parameters present: %v", want, sortedKeys(tool.InputSchema.Properties))
			}
		}
		return
	}
	t.Fatal("spec/mcp-catalog.json has no list_lore_entries tool at all — the " +
		"lore list is not reachable by any agent, which is a larger failure than " +
		"the one this test was written for.")
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
