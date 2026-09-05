package main

// api_lore_read_route_t33_test.go — T-33, hop ③ over real HTTP.
//
// 🔴 WHAT THESE PIN, AND WHY IT IS THE TICKET'S OWN SENTENCE. The owner opened
// this ticket asking that the original be kept so a judgement can be re-made
// later. Before these routes, the original WAS kept and nothing served it —
// a state in which the database satisfies the requirement, every count agrees,
// and no agent can act on any of it.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func loreDetail(t *testing.T, body string) LoreEntryDetailDTO {
	t.Helper()
	var dto LoreEntryDetailDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode entry detail %q: %v", body, err)
	}
	return dto
}

// The whole point, over the wire: 第 2 格 (`content`) is lossy, `original` is not.
func TestLoreReadRouteHandsBackWhatContentCompressedAway(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	id := loreSearchSeed(t, url, agentTok, "repo:officraft", "the fold happens in one place")

	st, body := rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+id, "")
	if st != 200 {
		t.Fatalf("read: %d %s", st, body)
	}
	got := loreDetail(t, body)
	if got.EntryId != id || got.Content != "the fold happens in one place" {
		t.Fatalf("entry: %+v", got)
	}
	if got.Original == "" {
		t.Fatal("the entry was served with NO original — 「原始資訊可以保留」 would be " +
			"true of the store and false of every reader")
	}
	// 🔴 EVERY SECTION IS NAMED, blank ones included: a renderer that skipped
	// blanks would make "never written" and "deleted" the same bytes, which is
	// the erosion this ticket exists to make visible.
	// 五格：heading + 三個欄位 + `events:` 區塊。`events:` 也在這一行裡，因為一條
	// 沒有事件的條目跟一條事件被改寫弄丟的條目在原文裡必須不一樣。
	for _, f := range []string{"heading:", "content:", "retire_when:", "impact:", "events:"} {
		if !strings.Contains(got.Original, f) {
			t.Fatalf("the original drops %q:\n%s", f, got.Original)
		}
	}
	if got.Sha256 != loreSHA256(got.Original) {
		t.Fatalf("the digest does not hash the served text")
	}
	// 🔴 v8 加的三格在線上讀得回來。
	// ⚠️ 這一段以前還靠「標題與第 1 格是兩句不同的話」來抓一個把兩格接反的
	// handler。`trigger` 被 `rc-9002654dd81c`（2026-09-06）併進 heading 之後只剩
	// 一格，那個對調的錯誤在構造上不存在了 —— 不是這支測試放鬆了。
	// ⚠️ 星等在這裡是 0，而那是 seed 沒有送 `impact_stars` 的結果——0 = 還沒判。
	// 它斷言的是「沒送不會被補成 1」，不是「星等接上了」；星等接上了那一半由
	// TestLoreEntryCellsRoundTripByName 與寫入路由那支的 422 撐著。
	if got.Heading != "something became visible that had not been" {
		t.Fatalf("heading = %q — 標題格沒有被接到讀取路徑上", got.Heading)
	}
	if got.Impact != "T-33 slot 3" || got.ImpactStars != 0 {
		t.Fatalf("impact = %q / stars = %d", got.Impact, got.ImpactStars)
	}
	// ⚠️ `reviewed` 一定是 false，而那不是這支測出來的性質，是這一版**沒有任何
	// 路由蓋得了章**的結果。它被斷言在這裡，是為了讓「有人把 reviewed 接上了
	// 寫入路徑」這件事在這裡紅掉，而不是等到 agent 開始自己蓋自己的章才被發現。
	if got.Reviewed {
		t.Fatal("一條剛寫進來的條目讀回來就是 reviewed —— 蓋章的那一欄被寫入路徑碰到了")
	}
	// 🔴 標題**在**原文裡，而這是線上看得見的那一面：讀的人拿到 original 之後，
	// 會以為那就是這條條目當初寫下的全部 —— 所以少一格，就是那份「全部」在說謊。
	// 這一段以前釘的是相反的事（標題不在原文裡，是一個已知的洞）；洞被填掉的
	// 方式是讓提案帶得動它（owner rc-bbccbeb3d9e6「任何修改都是提案的一環」）。
	if !strings.Contains(got.Original, "heading:\n"+got.Heading+"\n") {
		t.Fatalf("原文裡沒有標題格，或它記的不是條目上那一句（%q）:\n%s",
			got.Heading, got.Original)
	}
	if !strings.Contains(got.Original, "impact_stars:\n") {
		t.Fatalf("原文裡沒有星等這一格:\n%s", got.Original)
	}
	if got.WrittenBy != "m-lore-agent" {
		t.Fatalf("written_by = %q, want the verified token subject", got.WrittenBy)
	}
	if len(got.Revisions) != 1 || got.Revisions[0].ShrinkChars != 0 {
		t.Fatalf("revision catalogue: %+v", got.Revisions)
	}
	if len(got.Subjects) != 1 || got.Subjects[0] != "repo:officraft" {
		t.Fatalf("subjects: %v", got.Subjects)
	}
}

// The catalogue carries no text, and the revision route serves it in full.
func TestLoreReadRouteServesOneRevisionInFull(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	id := loreSearchSeed(t, url, agentTok, "repo:officraft", "the original outlives the summary")

	_, body := rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+id, "")
	detail := loreDetail(t, body)
	revID := detail.Revisions[0].RevisionId

	st, body := rosterREST(t, url, agentTok, "GET",
		"/api/lore/entries/"+id+"/revisions/"+strconv.Itoa(revID), "")
	if st != 200 {
		t.Fatalf("revision: %d %s", st, body)
	}
	var rev LoreRevisionDTO
	if err := json.Unmarshal([]byte(body), &rev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rev.Body != detail.Original || rev.Sha256 != detail.Sha256 {
		t.Fatalf("the revision disagrees with the entry's own original")
	}
	if rev.EntryId != id {
		t.Fatalf("entry_id: %q", rev.EntryId)
	}
}

// 🔴 THE ENTRY ID IN THE PATH IS A CONSTRAINT. Revision ids are global, so an
// unscoped lookup would serve one entry's original through another's address.
func TestLoreReadRouteRefusesARevisionBelongingToAnotherEntry(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	mine := loreSearchSeed(t, url, agentTok, "repo:officraft", "mine")
	theirs := loreSearchSeed(t, url, agentTok, "repo:officraft", "theirs")

	_, body := rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+theirs, "")
	otherRev := loreDetail(t, body).Revisions[0].RevisionId

	// Positive control first: that revision IS readable under its OWN entry.
	if st, _ := rosterREST(t, url, agentTok, "GET",
		"/api/lore/entries/"+theirs+"/revisions/"+strconv.Itoa(otherRev), ""); st != 200 {
		t.Fatalf("the revision is not readable under its own entry: %d", st)
	}
	st, body := rosterREST(t, url, agentTok, "GET",
		"/api/lore/entries/"+mine+"/revisions/"+strconv.Itoa(otherRev), "")
	if st != 404 {
		t.Fatalf("another entry's revision was served through this address: %d %s", st, body)
	}
}

// A retired entry is still readable BY ID: `retired` means "no longer
// retrieved", and the one path that can say what the retired thing said must
// not be the path that refuses.
func TestLoreReadRouteStillServesARetiredEntry(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)
	id := loreSearchSeed(t, url, agentTok, "repo:officraft", "still readable")
	if err := dal.RetireLoreEntry(id, LoreRetireExpired, "m-lore-agent", "", false, 500); err != nil {
		t.Fatalf("retire: %v", err)
	}

	// It is gone from RETRIEVAL — the discriminating half.
	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search",
		`{"subject":"repo:officraft"}`)
	if st != 200 {
		t.Fatalf("search: %d %s", st, body)
	}
	if got := loreSearchBody(t, body); len(got.Entries) != 0 {
		t.Fatalf("a retired entry is still retrievable: %+v", got.Entries)
	}
	// And still readable by id.
	st, body = rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+id, "")
	if st != 200 {
		t.Fatalf("a retired entry is unreadable by id: %d %s", st, body)
	}
	if got := loreDetail(t, body); got.Status != "retired" || got.Original == "" {
		t.Fatalf("retired entry detail: %+v", got)
	}
}

// An id nothing carries is a flat 404 on both routes, and a non-numeric
// revision is a 404 too — it is an ADDRESS, and an address naming nothing is
// "not found" whatever it is made of.
func TestLoreReadRouteAnswers404ForAddressesThatNameNothing(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	for _, path := range []string{
		"/api/lore/entries/lore-nope",
		"/api/lore/entries/lore-nope/revisions/1",
		"/api/lore/entries/lore-nope/revisions/not-a-number",
	} {
		if st, body := rosterREST(t, url, agentTok, "GET", path, ""); st != 404 {
			t.Fatalf("%s: want 404, got %d %s", path, st, body)
		}
	}
}

// The floor: a machine has nothing to re-judge.
func TestLoreReadRouteRefusesAMachineAtTheDoor(t *testing.T) {
	url, _, _, _, wardenTok := loreGovStack(t)
	st, body := rosterREST(t, url, wardenTok, "GET", "/api/lore/entries/lore-anything", "")
	if st != 403 {
		t.Fatalf("warden read: want 403, got %d %s", st, body)
	}
	if !strings.Contains(body, "principal not permitted") {
		t.Fatalf("refused by something other than the route floor: %s", body)
	}
}
