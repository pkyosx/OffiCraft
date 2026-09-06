package main

// api_lore_activity_route_t33_test.go — T-33: GET /api/members/{id}/lore-activity.
//
// The route's job is to make FOUR things distinguishable that all render as
// "nothing to show" if anybody gets lazy:
//
//   · the member is not running at all         → session_active:false, rows:[]
//   · the member is running and read nothing   → session_active:true,  rows:[]
//   · the member read something last session   → excluded (see the DAL tests)
//   · the member read something now gone       → the row STAYS, heading_found:false
//
// 🔴 AND ONE THING IT MUST NOT DO: file a journal row of its own. The panel
// displays lore_recall_log; a reader that writes to the table it displays would
// make its own visits indistinguishable from the agent's, in the one table the
// governance side uses to answer 「這一條到底有沒有人用」.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type loreActivityBody struct {
	MemberID      string  `json:"member_id"`
	SessionActive bool    `json:"session_active"`
	SessionBootTS float64 `json:"session_boot_ts"`
	Rows          []struct {
		CreatedTS     float64 `json:"created_ts"`
		SinceBootSecs float64 `json:"since_boot_secs"`
		Door          string  `json:"door"`
		EntryID       string  `json:"entry_id"`
		Heading       string  `json:"heading"`
		HeadingFound  bool    `json:"heading_found"`
		Status        string  `json:"status"`
	} `json:"rows"`
}

func getLoreActivity(t *testing.T, srvURL, memberID, token string) (int, loreActivityBody) {
	t.Helper()
	req, err := http.NewRequest("GET", srvURL+"/api/members/"+memberID+"/lore-activity", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body loreActivityBody
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return resp.StatusCode, body
}

// TestLoreActivitySeparatesNoSessionFromNoReads — the pair the whole panel rests
// on. Both answers carry `rows: []`; only `session_active` tells them apart, and
// on screen they are two different sentences.
func TestLoreActivitySeparatesNoSessionFromNoReads(t *testing.T) {
	srvURL, dal, _, ownerTok, _ := loreGovStack(t)

	// The member as the stack leaves it: offline, no anchor.
	code, body := getLoreActivity(t, srvURL, "m-lore-agent", ownerTok)
	if code != http.StatusOK {
		t.Fatalf("no-anchor read: want 200, got %d", code)
	}
	if body.SessionActive {
		t.Error("沒有 session 錨的成員必須回 session_active:false")
	}
	if body.SessionBootTS != 0 {
		t.Errorf("session_active:false 時錨應該是 0，拿到 %v", body.SessionBootTS)
	}
	if body.Rows == nil {
		t.Error("rows 必須是 []，不能是 null —— null 會讓前端多一種要處理的空")
	}
	if len(body.Rows) != 0 {
		t.Errorf("沒有 session 就不可能有這一任的取用，拿到 %d 列", len(body.Rows))
	}

	// Now anchor it, and read nothing.
	m, err := dal.GetMember("m-lore-agent")
	if err != nil || m == nil {
		t.Fatalf("get member: %v", err)
	}
	m.SessionBootTS = 5000
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("anchor member: %v", err)
	}
	code, body = getLoreActivity(t, srvURL, "m-lore-agent", ownerTok)
	if code != http.StatusOK {
		t.Fatalf("anchored read: want 200, got %d", code)
	}
	// 🔴 THE POINT: same empty rows, DIFFERENT answer.
	if !body.SessionActive {
		t.Error("有錨的成員必須回 session_active:true —— 「還沒讀東西」跟「沒有這一任」不是同一句話")
	}
	if body.SessionBootTS != 5000 {
		t.Errorf("錨應該原樣回來，拿到 %v", body.SessionBootTS)
	}
	if len(body.Rows) != 0 {
		t.Errorf("這一任還沒讀任何東西，拿到 %d 列", len(body.Rows))
	}
}

// TestLoreActivityFlattensAndKeepsTheVanishedEntry — one search row that
// returned THREE entries becomes three lines; the entry that no longer resolves
// keeps its line with heading_found:false and an EMPTY status.
func TestLoreActivityFlattensAndKeepsTheVanishedEntry(t *testing.T) {
	srvURL, dal, _, ownerTok, _ := loreGovStack(t)

	m, err := dal.GetMember("m-lore-agent")
	if err != nil || m == nil {
		t.Fatalf("get member: %v", err)
	}
	m.SessionBootTS = 5000
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("anchor: %v", err)
	}

	live := t33Entry("le-alive")
	live.Heading = "還在的那一條"
	t33Put(t, dal, live)
	gone := t33Entry("le-gone-retired")
	gone.Heading = "退役但讀過的那一條"
	gone.Status = "retired"
	t33Put(t, dal, gone)

	// ONE journal row, THREE entries — including one id that resolves to nothing.
	if err := dal.InsertLoreRecall(LoreRecall{
		ActorID:   "m-lore-agent",
		Query:     loreRecallQuerySearch,
		CreatedTS: 5120, // 上線後 120 秒
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries: []string{"le-alive", "le-gone-retired", "le-vanished"},
		}),
		SessionBootTS: 5000,
		SessionState:  loreRecallSessionAnchored,
	}); err != nil {
		t.Fatalf("insert recall: %v", err)
	}

	code, body := getLoreActivity(t, srvURL, "m-lore-agent", ownerTok)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if len(body.Rows) != 3 {
		t.Fatalf("一列取用回三條 ⇒ 攤平成 3 行，拿到 %d：%+v", len(body.Rows), body.Rows)
	}
	byID := map[string]int{}
	for i, r := range body.Rows {
		byID[r.EntryID] = i
		if r.Door != loreRecallQuerySearch {
			t.Errorf("%s 的門是 %q，應該是 search", r.EntryID, r.Door)
		}
		if r.CreatedTS != 5120 {
			t.Errorf("%s 的絕對時間是 %v，應該是 5120", r.EntryID, r.CreatedTS)
		}
		if r.SinceBootSecs != 120 {
			t.Errorf("%s 上線後秒數是 %v，應該是 120", r.EntryID, r.SinceBootSecs)
		}
		// 🔴 the wire invariant, asserted on EVERY row rather than on the one
		// the test happens to care about: status == "" iff !heading_found.
		if (r.Status == "") != !r.HeadingFound {
			t.Errorf("%s 破壞不變式：heading_found=%v status=%q",
				r.EntryID, r.HeadingFound, r.Status)
		}
	}

	alive := body.Rows[byID["le-alive"]]
	if !alive.HeadingFound || alive.Heading != "還在的那一條" || alive.Status != "active" {
		t.Errorf("活著的條目回 %+v", alive)
	}
	// 🔴 RETIRED IS NOT MISSING. It resolves, keeps its heading, and says retired
	// — the panel marks it, the deep link still reaches it.
	retired := body.Rows[byID["le-gone-retired"]]
	if !retired.HeadingFound || retired.Status != "retired" ||
		retired.Heading != "退役但讀過的那一條" {
		t.Errorf("退役條目回 %+v —— 退役是「不再被檢索」，不是查不到", retired)
	}
	// 🔴 THE VANISHED ONE KEEPS ITS LINE. Dropping it would render
	// 「讀過但現在查不到」 as 「沒讀過」.
	vanished := body.Rows[byID["le-vanished"]]
	if vanished.HeadingFound {
		t.Error("le-vanished 不存在，heading_found 應該是 false")
	}
	if vanished.Heading != "" {
		t.Errorf("查不到的條目不可以編一個標題，拿到 %q", vanished.Heading)
	}
	if vanished.Status != "" {
		t.Errorf("查不到就沒有 status，拿到 %q", vanished.Status)
	}
}

// TestLoreActivityWritesNoJournalRow — the panel reads the recall journal and
// must not appear in it.
//
// 🔴 THE POSITIVE CONTROL IS THE SEEDED ROW: the count is 1 before and 1 after,
// so "no new row" is measured against a table that demonstrably accepts rows,
// not against one where every insert silently failed.
func TestLoreActivityWritesNoJournalRow(t *testing.T) {
	srvURL, dal, _, ownerTok, _ := loreGovStack(t)

	m, err := dal.GetMember("m-lore-agent")
	if err != nil || m == nil {
		t.Fatalf("get member: %v", err)
	}
	m.SessionBootTS = 5000
	if err := dal.PutMember(*m); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	t33Put(t, dal, t33Entry("le-probe"))
	if err := dal.InsertLoreRecall(LoreRecall{
		ActorID: "m-lore-agent", Query: loreRecallQueryEntryRead, CreatedTS: 5010,
		Returned: encodeLoreRecallReturned(loreRecallReturned{
			Entries: []string{"le-probe"},
		}),
		SessionBootTS: 5000, SessionState: loreRecallSessionAnchored,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before := len(readLoreRecallsOf(t, dal))
	if before != 1 {
		t.Fatalf("陽性對照：種一列應該有 1 列，實際 %d", before)
	}
	if code, _ := getLoreActivity(t, srvURL, "m-lore-agent", ownerTok); code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	if after := len(readLoreRecallsOf(t, dal)); after != before {
		t.Errorf("這一支讀 journal，不可以寫 journal：讀之前 %d 列，讀之後 %d 列 —— "+
			"一個把自己記成取用的面板，會讓「有人用過這一條」變成它自己造的",
			before, after)
	}
}

// TestLoreActivityFloorIsAdminAgent — the same floor as the resume-summary read
// it sits under, asserted rather than assumed.
func TestLoreActivityFloorIsAdminAgent(t *testing.T) {
	srvURL, _, agentTok, ownerTok, _ := loreGovStack(t)

	if code, _ := getLoreActivity(t, srvURL, "m-lore-agent", agentTok); code != http.StatusForbidden {
		t.Errorf("一般 agent 讀別人的取用歷史應該 403，拿到 %d", code)
	}
	if code, _ := getLoreActivity(t, srvURL, "m-lore-agent", ownerTok); code != http.StatusOK {
		t.Errorf("owner 應該讀得到，拿到 %d", code)
	}
	if code, _ := getLoreActivity(t, srvURL, "m-nobody", ownerTok); code != http.StatusNotFound {
		t.Errorf("不存在的成員應該 404，拿到 %d", code)
	}
}

// TestLoreActivityAnswersWhileLoreIsSwitchedOff — the panel is NOT lore-gated,
// and that is deliberate: the journal keeps saying what happened while the
// feature was on. A 403 here would erase the history rather than the feature.
func TestLoreActivityAnswersWhileLoreIsSwitchedOff(t *testing.T) {
	srv, dal, secret, api := newLessonsTestServerAPI(t)
	// NOTE: enableLoreForTest is deliberately NOT called — this is a station
	// with the lore switch in its shipped OFF position.
	if err := dal.PutMember(Member{
		ID: "m-off", Name: "off", Kind: KindStaff, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
		SessionBootTS: 7000,
	}); err != nil {
		t.Fatalf("put member: %v", err)
	}
	_ = api
	ownerTok, err := mintJWT("owner", "owner", 3600, secret, int64(nowSecs()), "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	code, body := getLoreActivity(t, srv.URL, "m-off", ownerTok)
	if code != http.StatusOK {
		t.Fatalf("開關關著時這一支仍然要答，拿到 %d", code)
	}
	if !body.SessionActive {
		t.Error("開關關著不影響 session 錨的判讀")
	}
}
