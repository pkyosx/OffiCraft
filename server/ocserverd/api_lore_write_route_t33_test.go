package main

// api_lore_write_route_t33_test.go — T-33, the write route over real HTTP.
//
// 🔴 WHY THIS FILE IS NOT COVERED BY dal_lore_write_t33_test.go. That suite
// calls CreateLoreEntry directly, which is exactly why it cannot say whether
// anything ELSE can reach it. Before this route landed, the only caller of the
// write seam in the whole tree was a test — and a store nothing can write to is
// a store that is empty, whose directory renders as nothing, which looks
// identical to a feature nobody has used. Every assertion below therefore goes
// through the wired stack: auth middleware → RBAC choke → generated wrapper →
// handler.

import (
	"encoding/json"
	"strings"
	"testing"
)

func loreWriteBody(t *testing.T, body string) LoreWriteReceiptDTO {
	t.Helper()
	var dto LoreWriteReceiptDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode receipt %q: %v", body, err)
	}
	return dto
}

// 每一格 over the wire, `events` included: a write body that carried only the
// columns would leave the event path untested by every test that seeds with it.
// ⚠️ 這個 body 以前還有一個 `"trigger"` key，跟 heading 刻意寫成兩句不同的話。
// `rc-9002654dd81c`（2026-09-06「合併成 heading 一格」）之後那個 key 是**未宣告
// 的**，送它會被 422 指名擋掉 —— 見下面 TestLoreWriteRouteRefusesAnEntryNobodyCouldRead
// 裡的 "trigger is no longer a field" 那一列。
const loreWriteJSON = `{
	"heading": "two blocks disagreed and nobody noticed for a week",
	"content": "the fold happens in one place",
	"events": [
		{"happened_ts": 1756000000, "what": "Kyle 讀到兩個區塊互相矛盾",
		 "actor": "agent:O-197", "place": "machine:seth-m5"}
	],
		"subjects": ["repo:officraft"]
}`

// An ordinary agent can write, and what comes back is READ BACK from the store.
// 🔑 The directory assertion is the one that matters: before this route, the
// roster was empty for every member on the station, and an empty roster is not
// rendered at all — so "the feature works" and "nobody can see anything" looked
// the same. This asserts the write actually reaches that surface.
func TestLoreWriteRouteLetsAnAgentPutAnEntryWhereTheDirectoryFindsIt(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	before, err := dal.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("the station did not start with an empty directory: %+v", before)
	}

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", loreWriteJSON)
	if st != 200 {
		t.Fatalf("agent write: want 200, got %d %s", st, body)
	}
	dto := loreWriteBody(t, body)
	if dto.EntryId == "" || dto.RevisionId == 0 || len(dto.Sha256) != 64 {
		t.Fatalf("receipt is missing the entry or its original: %+v", dto)
	}
	entry, err := dal.GetLoreEntry(dto.EntryId)
	if err != nil || entry == nil {
		t.Fatalf("the entry the receipt names is not in the table: %v", err)
	}
	rev, err := dal.LatestLoreRevision(dto.EntryId)
	if err != nil || rev == nil {
		t.Fatalf("the write over the wire preserved NO original: %v", err)
	}
	if rev.ActorID != "m-lore-agent" {
		t.Fatalf("the original records actor %q, not the verified token subject", rev.ActorID)
	}

	// The subject was minted (nothing seeded it), so it is pending and must NOT
	// be in the directory yet. Approving it is the discriminating half: without
	// it, a roster broken some other way would also read as zero.
	if len(dto.PendingEntities) != 1 || dto.PendingEntities[0].Canonical != "repo:officraft" {
		t.Fatalf("the minted subject was not reported: %+v", dto.PendingEntities)
	}
	roster, err := dal.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(roster) != 0 {
		t.Fatalf("an unapproved subject reached the directory: %+v", roster)
	}
	if _, err := dal.wdb.Exec(
		`UPDATE entity SET pending = 0 WHERE id = ?`, dto.PendingEntities[0].EntityId); err != nil {
		t.Fatalf("approve: %v", err)
	}
	roster, err = dal.ListLoreSubjectRoster("")
	if err != nil {
		t.Fatalf("roster after approval: %v", err)
	}
	if len(roster) != 1 || roster[0].Entries != 1 {
		t.Fatalf("the written entry never reached the directory: %+v", roster)
	}
}

// 🔴 `events`在線上的拒絕。時（`happened_ts`）與事（`what`）必填；人／地／物
// 空著合法，但**寫錯**要被指名。這一支取代了舊的 falsify / instance 必填測試：
// 那道裁定（rc-714eea33c6ed）在五格裡沒有欄位可以套，不是被推翻，是沒有落點。
//
// 🔴 一筆壞事件拒絕的是**整筆寫入**。少了最後那個 count，一個「事件寫不進去但
// 條目本體照寫」的實作會讓上面每一條斷言都是綠的。
func TestLoreWriteRouteRefusesABadEventAndNamesTheCell(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	const head = `{"heading": "h", "content": "y", "subjects": ["repo:officraft"], "events": [`
	for _, tc := range []struct{ name, events, want string }{
		{"no happened_ts", `{"what": "有人踩到了"}`, "happened_ts"},
		{"happened_ts is zero", `{"happened_ts": 0, "what": "有人踩到了"}`, "happened_ts"},
		{"blank what", `{"happened_ts": 1756000000, "what": "   "}`, "what"},
		{"actor is not type:name", `{"happened_ts": 1756000000, "what": "有人踩到了", "actor": "Seth"}`, "actor"},
		{"actor names an unapproved type", `{"happened_ts": 1756000000, "what": "有人踩到了", "actor": "vendor:acme"}`, "vendor"},
		{"place is not type:name", `{"happened_ts": 1756000000, "what": "有人踩到了", "place": "seth-m5"}`, "place"},
	} {
		st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", head+tc.events+`]}`)
		if st != 422 {
			t.Fatalf("%s: want 422, got %d %s", tc.name, st, body)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s: the refusal does not name %q: %s", tc.name, tc.want, body)
		}
	}

	// 🔴 POSITIVE CONTROL, and it is the half that says the refusals above
	// discriminate: 人／地／物 ALL absent is legal, and it lands.
	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries",
		head+`{"happened_ts": 1756000000, "what": "有人踩到了"}]}`)
	if st != 200 {
		t.Fatalf("an event with 時 and 事 and nothing else must be legal: %d %s", st, body)
	}

	var n int
	if err := dal.rdb.QueryRow(`SELECT COUNT(*) FROM lore_entry`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("six refused writes and one accepted one left %d entries", n)
	}
	var evs int
	if err := dal.rdb.QueryRow(`SELECT COUNT(*) FROM lore_event`).Scan(&evs); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if evs != 1 {
		t.Fatalf("a refused write left %d events behind — a bad event must refuse the WHOLE write", evs)
	}
}

// The fields without which the row is unreachable — or reachable but
// indistinguishable from a finished entry — are refused, and the refusal names
// which one. 422 rather than 400 keeps it the same answer every other
// body-validation refusal on this station gives.
//
// 🔴 標題格有**兩種**拒絕，而它們走的是兩條不同的路，所以兩種都列在這裡：整個
// key 沒送是 decodeJSONBodyStrict 的「field required」，送了但是空白是 DAL 的
// ErrLoreHeadingBlank。只測其中一種，另一條路可以整條消失而這支全綠。
func TestLoreWriteRouteRefusesAnEntryNobodyCouldRead(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	for _, tc := range []struct{ name, body, want string }{
		{"heading absent", `{"content":"y","subjects":["repo:officraft"]}`, "heading"},
		{"blank heading", `{"heading":"  ","content":"y","subjects":["repo:officraft"]}`, "heading"},
		// 🔴 第三種拒絕：標題超過 140 個字元。它在這裡而不是只在 DAL 那一層，
		// 是因為**這一段是它變成 422 的地方**：沒有被 writeLoreWriteError 列舉
		// 的錯誤會掉到 internalError 變成 500，而 500 的意思是「重試」，重試永遠
		// 會失敗。141 個中文字＝141 個 rune、423 個 byte —— 用中文送，是為了讓
		// 一個用 len() 數位元組的實作在錯誤訊息裡報出 423 而被下面那句抓到。
		{"over-long heading", `{"heading":"` + strings.Repeat("記", 141) +
			`","content":"y","subjects":["repo:officraft"]}`, "heading"},
		// 🔴 這一列以前是 {"blank trigger", …}，也就是「`heading`空白要被拒」。那一格
		// 被 `rc-9002654dd81c`（2026-09-06）併進 heading ⇒ 它守的東西已經在上面
		// 「heading absent／blank heading」兩列裡。留在這裡的是**新的**那件事：
		// `trigger` 現在是一個未宣告的 key，而未宣告的 body key 一律 422 指名拒絕。
		// 少了這一列，一個仍然收 `trigger`（然後靜默丟掉）的 handler 會全綠，而
		// 寫入者會以為他寫下了一句沒有人存下來的話。
		{"trigger is no longer a field", `{"heading":"h","trigger":"x","content":"y","subjects":["repo:officraft"]}`, "trigger"},
		{"blank content", `{"heading":"h","content":"","subjects":["repo:officraft"]}`, "content"},
		{"no subject", `{"heading":"h","content":"y","subjects":[]}`, "subject"},
		// 🔴 這一列以前是 {"unknown origin type", …}，守的是 origin 的型別前綴。
		// `origin` 整格被 `rc-9c9bf14a579f`（2026-09-06「一起拿掉」）移除 ⇒ 那件事
		// 沒有了。留在這裡的是**新的**那件事，跟上面 `trigger` 那一列同一個形狀：
		// `origin` 現在是一個未宣告的 key，而未宣告的 body key 一律 422 指名拒絕。
		// 🔴 少了這一列，一個仍然收 `origin`（然後靜默丟掉）的 handler 會全綠，而
		// 送出來的人會以為他標註了來源 —— 那正是這一格被拿掉之後最像沒事的失敗。
		{"origin is no longer a field", `{"heading":"h","origin":"human:Seth","content":"y","subjects":["repo:officraft"]}`, "origin"},
		{"unknown subject type", `{"heading":"h","content":"y","subjects":["vendor:acme"]}`, "vendor"},
		{"malformed subject", `{"heading":"h","content":"y","subjects":["officraft"]}`, "officraft"},
		// 🔴 `impact_stars` 現在跟 `trigger` / `origin` 一樣是**未宣告的 key**：
		// owner 2026-09-06 逐字「都改掉」把那一格連同欄位拿掉了。這一列守的是
		// 「送它會被指名擋掉」，不是「值域」—— 一個仍然收下它再靜默丟掉的 handler
		// 會讓寫入者以為他判過了這條傳承的下場。
		{"impact_stars is no longer a field", `{"heading":"h","content":"y","impact_stars":2,"subjects":["repo:officraft"]}`, "impact_stars"},
		{"impact is no longer a field", `{"heading":"h","content":"y","impact":"x","subjects":["repo:officraft"]}`, "impact"},
		{"revisit_when is no longer a field", `{"heading":"h","content":"y","revisit_when":"x","subjects":["repo:officraft"]}`, "revisit_when"},
		{"supersedes is no longer a field", `{"heading":"h","content":"y","supersedes":"lore-1","subjects":["repo:officraft"]}`, "supersedes"},
	} {
		st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", tc.body)
		if st != 422 {
			t.Fatalf("%s: want 422, got %d %s", tc.name, st, body)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s: the refusal does not name %q: %s", tc.name, tc.want, body)
		}
		// 🔴 超長那一列多要一件事：訊息裡的長度必須是 141（字元），不是 423
		// （位元組）。少了這一句，一個用 len() 的實作也會回 422 並提到 heading，
		// 而寫的人會看著「我明明只寫了 141 個字」的訊息去查一個不存在的問題。
		if tc.name == "over-long heading" {
			if !strings.Contains(body, "141") || !strings.Contains(body, "140") {
				t.Fatalf("%s: 訊息沒有同時說出上限 140 與送來的 141: %s", tc.name, body)
			}
			if strings.Contains(body, "423") {
				t.Fatalf("%s: 訊息報的是位元組數（423）而不是字元數: %s", tc.name, body)
			}
		}
	}
}

// A key the DTO does not declare is a 422, never a silent drop. This is the
// house rule, asserted HERE because the field set being closed is what stops a
// misspelled `content` from writing an entry with an empty body.
func TestLoreWriteRouteRefusesAnUnknownFieldRatherThanDroppingIt(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"heading": "h", "contentt": "the typo that empties the body",
		"content": "y",
		"subjects": ["repo:officraft"]
	}`)
	if st != 422 {
		t.Fatalf("unknown field: want 422, got %d %s", st, body)
	}
	var n int
	if err := dal.rdb.QueryRow(`SELECT COUNT(*) FROM lore_entry`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refused body wrote %d entries", n)
	}
}

// 🔴 THE FLOOR. A warden is an authenticated identity and is NOT a member with
// experience to record; it is refused at the door, before any body is read.
// The refusal has to be the ROUTE FLOOR's wording — if this ever starts being
// refused deeper down, the floor could be deleted without the number moving.
func TestLoreWriteRouteRefusesAMachineAtTheDoor(t *testing.T) {
	url, _, _, _, wardenTok := loreGovStack(t)

	st, body := rosterREST(t, url, wardenTok, "POST", "/api/lore/entries", loreWriteJSON)
	if st != 403 {
		t.Fatalf("warden write: want 403, got %d %s", st, body)
	}
	if !strings.Contains(body, "principal not permitted") {
		t.Fatalf("the warden was refused by something other than the route floor: %s", body)
	}
}

// The owner is above the floor and writes through the same door — the ladder is
// "at least", not "exactly". Without this, raising the floor to owner-only
// would not move a single number in this file.
func TestLoreWriteRouteAdmitsTheOwnerToo(t *testing.T) {
	url, _, _, ownerTok, _ := loreGovStack(t)

	st, body := rosterREST(t, url, ownerTok, "POST", "/api/lore/entries", loreWriteJSON)
	if st != 200 {
		t.Fatalf("owner write: want 200, got %d %s", st, body)
	}
}
