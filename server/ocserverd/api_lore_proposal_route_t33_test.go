package main

// api_lore_proposal_route_t33_test.go — T-33. 同一個問題，但問的是「辦公室擋不擋」
// 而不是「DAL 擋不擋」.
//
// 🔴 WHY THIS FILE EXISTS BESIDE THE DAL SUITE. dal_lore_proposal_t33_test.go
// calls CreateLoreProposal directly, which is exactly why it cannot say whether
// anything else can reach it, whether the staleness refusal comes out as a
// status a client can act on, or whether a warden is stopped at the door. Every
// assertion below goes through a REAL HTTP request against the wired stack.

import (
	"encoding/json"
	"strings"
	"testing"
)

// loreProposalSeed writes an entry through the REAL write route as the agent and
// returns its id and the digest the read route serves for it.
//
// 🔴 THE DIGEST IS READ BACK OFF THE WIRE, not taken from the DAL. That is the
// path a real proposer has: GET the entry, copy `sha256`, propose against it. A
// test that fetched the digest out of the database would pass even if the read
// route served a different one — which is the one way this feature could be
// wrong while every screen looked right.
func loreProposalSeed(t *testing.T, url, tok string) (string, string) {
	t.Helper()
	st, body := rosterREST(t, url, tok, "POST", "/api/lore/entries", `{
		"heading":"兩個區塊對同一件事給了不同答案","impact_stars":2,
		"content":"the fold happens in one place",
		"retire_when":"等只剩一個組裝器",
		"impact":"T-33 slot 3",
		"origin":"agent:O-197",
		"subjects":["agent:O-197"]}`)
	if st != 200 {
		t.Fatalf("seed entry: %d %s", st, body)
	}
	var receipt LoreWriteReceiptDTO
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		t.Fatalf("decode write receipt: %v", err)
	}
	st, body = rosterREST(t, url, tok, "GET", "/api/lore/entries/"+receipt.EntryId, "")
	if st != 200 {
		t.Fatalf("read back entry: %d %s", st, body)
	}
	var detail LoreEntryDetailDTO
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decode entry detail: %v", err)
	}
	if detail.Sha256 == "" || detail.Sha256 != receipt.Sha256 {
		t.Fatalf("the read route and the write receipt disagree about the digest: %q vs %q",
			detail.Sha256, receipt.Sha256)
	}
	return receipt.EntryId, detail.Sha256
}

func loreProposalBody(base string) string {
	return `{
		"kind":"update",
		"base_sha256":"` + base + `",
		"encountered":"T-33 slot 4, wiring the proposal route",
		"fault":"stale",
		"evidence":"the entry names a file that moved in 8282fdef",
		"heading":"兩個區塊對同一件事給了不同答案","impact_stars":2,
		"content":"the fold happens in lore_fold.go and nowhere else",
		"retire_when":"等只剩一個組裝器",
		"impact":"T-33 slot 3",
		"events":[]}`
}

// 🔴 THE WIRE FACE OF THE DIGEST CHECK. A proposal against a version that is not
// the entry's current one comes back 409 — not 200, and not 422 — with a message
// that says what happened in words a proposer can act on.
func TestLoreProposalRouteRefusesAStaleBaseDigestWith409(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	entryID, sha := loreProposalSeed(t, url, agentTok)

	stale := strings.Repeat("0", 64)
	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreProposalBody(stale))
	if st != 409 {
		t.Fatalf("a proposal against a version nobody holds: want 409, got %d %s", st, body)
	}
	// The words matter as much as the number here: 409 alone does not tell a
	// proposer whether to re-read the entry or to fix his own body.
	for _, want := range []string{"changed while you were reviewing it", sha, stale} {
		if !strings.Contains(body, want) {
			t.Fatalf("the 409 does not say %q: %s", want, body)
		}
	}

	// 🔴 POSITIVE CONTROL. Without it, a handler that answered 409 to every
	// proposal would pass everything above.
	st, body = rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreProposalBody(sha))
	if st != 200 {
		t.Fatalf("the same proposal against the CURRENT digest: want 200, got %d %s", st, body)
	}
	var receipt LoreProposalReceiptDTO
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if receipt.ProposalId == "" || receipt.BaseSha256 != sha || receipt.BaseRevisionId == 0 {
		t.Fatalf("the receipt does not bind the proposal to the version it matched: %+v", receipt)
	}
	if receipt.Sha256 == "" || receipt.Sha256 == sha {
		t.Fatalf("the receipt digests nothing, or digests the base: %+v", receipt)
	}
}

// The list route serves the WHOLE proposed version and says, per row, whether it
// still stands — with the digest it was compared against in the same response.
func TestLoreProposalRouteListServesTheWholeVersionAndItsStaleness(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)
	entryID, sha := loreProposalSeed(t, url, agentTok)

	// An entry nobody has proposed anything for: 200 and an empty list, not 404.
	st, body := rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+entryID+"/proposals", "")
	if st != 200 {
		t.Fatalf("empty proposal list: want 200, got %d %s", st, body)
	}
	var empty LoreProposalListDTO
	if err := json.Unmarshal([]byte(body), &empty); err != nil {
		t.Fatalf("decode empty list: %v", err)
	}
	if empty.Proposals == nil || len(empty.Proposals) != 0 || empty.CurrentSha256 != sha {
		t.Fatalf("an entry with no proposals answered %+v", empty)
	}
	if !strings.Contains(body, `"proposals":[]`) {
		t.Fatalf("the empty list is served as null rather than []: %s", body)
	}

	st, body = rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreProposalBody(sha))
	if st != 200 {
		t.Fatalf("file: %d %s", st, body)
	}

	st, body = rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+entryID+"/proposals", "")
	if st != 200 {
		t.Fatalf("list: %d %s", st, body)
	}
	var list LoreProposalListDTO
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Proposals) != 1 {
		t.Fatalf("want one proposal, got %d", len(list.Proposals))
	}
	row := list.Proposals[0]
	if row.Stale {
		t.Fatalf("a proposal filed a moment ago reads as stale: %+v", row)
	}
	if row.Content != "the fold happens in lore_fold.go and nowhere else" || row.Body == "" {
		t.Fatalf("the proposed version did not reach the wire: %+v", row)
	}
	// The actor is the TOKEN's subject, never anything the body could assert.
	if row.ActorId != "m-lore-agent" {
		t.Fatalf("actor_id = %q, want the token subject", row.ActorId)
	}
	if row.BaseSha256 != sha || list.CurrentSha256 != sha {
		t.Fatalf("the two digests the reviewer compares are not both served: %+v %+v", row, list)
	}

	// Somebody rewrites the entry underneath the proposal. Nothing on the WIRE
	// can do this yet — ApplyLoreProposal is a DAL seam with no route, because
	// who may accept is arbitration policy nobody has ruled on — so the second
	// revision is written directly, which is the whole reason the guard is in
	// place before that path is reachable.
	moved := loreRevisionBody(t33Entry(entryID), nil)
	if _, err := dal.wdb.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, 'somebody-else', 3000, 0)`,
		entryID, moved, loreSHA256(moved)); err != nil {
		t.Fatalf("second revision: %v", err)
	}

	st, body = rosterREST(t, url, agentTok, "GET", "/api/lore/entries/"+entryID+"/proposals", "")
	if st != 200 {
		t.Fatalf("list after the entry moved: %d %s", st, body)
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !list.Proposals[0].Stale {
		t.Fatalf("the entry moved under this proposal and the wire still calls it "+
			"current: %s", body)
	}
	if list.CurrentSha256 != loreSHA256(moved) || list.Proposals[0].BaseSha256 != sha {
		t.Fatalf("the reviewer cannot re-derive `stale` from what was served: %s", body)
	}
}

// The floor: a warden is an authenticated identity that is not a member with
// experience to report, and it is refused at the door on BOTH rows — before any
// body is parsed and before any entry id is looked up.
func TestLoreProposalRoutesRefuseAMachineAtTheDoor(t *testing.T) {
	url, _, agentTok, ownerTok, wardenTok := loreGovStack(t)
	entryID, sha := loreProposalSeed(t, url, agentTok)

	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/lore/entries/" + entryID + "/proposals", loreProposalBody(sha)},
		{"GET", "/api/lore/entries/" + entryID + "/proposals", ""},
	} {
		if st, body := rosterREST(t, url, wardenTok, tc.method, tc.path, tc.body); st != 403 {
			t.Errorf("%s as a warden: want 403, got %d %s", tc.method, st, body)
		}
		if st, body := rosterREST(t, url, "", tc.method, tc.path, tc.body); st != 401 {
			t.Errorf("%s with no token: want 401, got %d %s", tc.method, st, body)
		}
	}
	// The owner is above the floor and gets the route's semantics, not a 403 —
	// otherwise the two assertions above would be satisfied by a route that
	// refused everybody.
	if st, body := rosterREST(t, url, ownerTok, "GET",
		"/api/lore/entries/"+entryID+"/proposals", ""); st != 200 {
		t.Fatalf("owner list: want 200, got %d %s", st, body)
	}
}

// An entry id nothing carries is a 404 on both rows — never a 200 that files a
// review request nobody will ever see.
func TestLoreProposalRoutesAnswer404ForAnUnknownEntry(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	_, sha := loreProposalSeed(t, url, agentTok)

	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/lore-nobody-carries-this/proposals", loreProposalBody(sha)); st != 404 {
		t.Errorf("propose against an unknown entry: want 404, got %d %s", st, body)
	}
	if st, body := rosterREST(t, url, agentTok, "GET",
		"/api/lore/entries/lore-nobody-carries-this/proposals", ""); st != 404 {
		t.Errorf("list an unknown entry: want 404, got %d %s", st, body)
	}
}

// The body's field set is CLOSED: an undeclared key is refused by name rather
// than dropped. A dropped key on this route would be a proposer's change that
// silently did not travel.
func TestLoreProposalRouteRefusesAnUndeclaredBodyKey(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	entryID, sha := loreProposalSeed(t, url, agentTok)

	body := strings.Replace(loreProposalBody(sha), `"kind":"update",`,
		`"kind":"update","base_revision_id":7,`, 1)
	st, got := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", body)
	if st != 422 || !strings.Contains(got, "base_revision_id") {
		t.Fatalf("an undeclared key was not refused BY NAME: %d %s", st, got)
	}
}

// 🔴 第 5 格在線上：提案帶得動它，而審核者從**同一個回應**就看得出它動了哪幾筆。
//
// 這一支是負責人裁定「提案改得動事件」之後必須配上的那一面。DAL 那一層已經有
// 對應的斷言，但這裡問的是不一樣的問題：這些事實有沒有真的走到線上，還是只活在
// 一個沒有人讀得到的 struct 欄位裡。
func TestLoreProposalRouteCarriesEventsAndSaysWhichOnesMoved(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)
	entryID, _ := loreProposalSeed(t, url, agentTok)

	// 條目本來有兩筆事件。它們透過 DAL 寫進去，因為這一批沒有「事後補一筆事件」
	// 的路由——重點是提案面，不是補記面。條目的摘要因此改變，所以底下重讀一次。
	if _, err := dal.wdb.Exec(`
		INSERT INTO lore_event (entry_id, happened_ts, what, actor, place, object)
		VALUES (?, 1700000000, '留著不動的那一筆', '', '', ''),
		       (?, 1700000100, '機器串錯的那一筆', '', '', '')`,
		entryID, entryID); err != nil {
		t.Fatalf("seed events: %v", err)
	}
	events, err := dal.ListLoreEvents(entryID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	entry, err := dal.GetLoreEntry(entryID)
	if err != nil || entry == nil {
		t.Fatalf("entry: %+v %v", entry, err)
	}
	moved := loreRevisionBody(*entry, events)
	if _, err := dal.wdb.Exec(`
		INSERT INTO lore_revision (entry_id, body, sha256, actor_id, created_ts, shrink_chars)
		VALUES (?, ?, ?, 'seed', 1500, 0)`, entryID, moved, loreSHA256(moved)); err != nil {
		t.Fatalf("revision: %v", err)
	}
	sha := loreSHA256(moved)

	// 🔴 一份沒說第 5 格的 `update` 是 422 —— 在線上也是。少了這一條，一次漏填
	// 會在審核者看不見的地方主張刪光所有事件，而核可是整批換掉的，那個主張會
	// 真的落地。
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", `{
			"kind":"update","base_sha256":"`+sha+`",
			"encountered":"讀到它的時候","fault":"stale","evidence":"第 5 格串錯了",
			"heading":"兩個區塊對同一件事給了不同答案","impact_stars":2,
			"content":"the fold happens in lore_fold.go and nowhere else"}`); st != 422 {
		t.Fatalf("一份沒帶 events 的 update：want 422, got %d %s", st, body)
	}

	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", `{
			"kind":"update","base_sha256":"`+sha+`",
			"encountered":"讀到它的時候","fault":"stale","evidence":"第 5 格串錯了",
			"heading":"兩個區塊對同一件事給了不同答案","impact_stars":2,
			"content":"the fold happens in lore_fold.go and nowhere else",
			"retire_when":"等只剩一個組裝器","impact":"T-33 slot 3",
			"events":[
				{"happened_ts":1700000000,"what":"留著不動的那一筆"},
				{"happened_ts":1700000100,"what":"人工修好的那一筆"}]}`)
	if st != 200 {
		t.Fatalf("file with events: %d %s", st, body)
	}

	st, body = rosterREST(t, url, agentTok, "GET",
		"/api/lore/entries/"+entryID+"/proposals", "")
	if st != 200 {
		t.Fatalf("list: %d %s", st, body)
	}
	var list LoreProposalListDTO
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	row := list.Proposals[0]
	if len(row.Events) != 2 {
		t.Fatalf("提案自己的第 5 格沒有走到線上: %s", body)
	}
	if len(row.EventsAdded) != 1 || row.EventsAdded[0].What != "人工修好的那一筆" {
		t.Fatalf("events_added 沒有說他加了哪一筆: %s", body)
	}
	if len(row.EventsRemoved) != 1 || row.EventsRemoved[0].What != "機器串錯的那一筆" {
		t.Fatalf("events_removed 沒有說他刪了哪一筆 —— 刪除只表現成一個「不在」，"+
			"審核者就是會漏掉這一半: %s", body)
	}
	// 🔴 被比較的另一邊也在同一個回應裡，所以審核者可以自己重算這個差異，而不是
	// 只能相信它 —— 跟 `stale` 附上 current_sha256 是同一條規則。
	if len(list.CurrentEvents) != 2 {
		t.Fatalf("現況的第 5 格沒有跟差異一起送出來: %s", body)
	}
	// 空陣列而不是 null：一個要把兩者當同一件事處理的讀者，遲早會有一邊處理錯。
	if !strings.Contains(body, `"events_removed":[`) || !strings.Contains(body, `"current_events":[`) {
		t.Fatalf("事件欄位在線上是 null 而不是 []: %s", body)
	}
}
