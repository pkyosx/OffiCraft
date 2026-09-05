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

// 六格 over the wire, 第 5 格 included: a write body that carried only the
// columns would leave the event path untested by every test that seeds with it.
// 🔴 標題格與第 1 格在這裡刻意是兩句不同的話 —— 寫成同一句，一個把兩格接反的
// handler 會讓每一支拿這個 body 當 fixture 的測試都全綠。
const loreWriteJSON = `{
	"heading": "two blocks disagreed and nobody noticed for a week",
	"trigger": "two blocks disagree about the same fact",
	"content": "the fold happens in one place",
	"retire_when": "等只剩一個組裝器",
	"impact": "T-33 slot 3",
	"impact_stars": 2,
	"events": [
		{"happened_ts": 1756000000, "what": "Kyle 讀到兩個區塊互相矛盾",
		 "actor": "agent:O-197", "place": "machine:seth-m5"}
	],
	"origin": "agent:O-197",
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
	if entry.Origin != "agent:O-197" {
		t.Fatalf("origin: got %q — it is the caller's to state", entry.Origin)
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

// 🔴 第 5 格在線上的拒絕。時（`happened_ts`）與事（`what`）必填；人／地／物
// 空著合法，但**寫錯**要被指名。這一支取代了舊的 falsify / instance 必填測試：
// 那道裁定（rc-714eea33c6ed）在五格裡沒有欄位可以套，不是被推翻，是沒有落點。
//
// 🔴 一筆壞事件拒絕的是**整筆寫入**。少了最後那個 count，一個「事件寫不進去但
// 條目本體照寫」的實作會讓上面每一條斷言都是綠的。
func TestLoreWriteRouteRefusesABadEventAndNamesTheCell(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	const head = `{"heading": "h", "trigger": "x", "content": "y", "origin": "agent:O-197",
		"subjects": ["repo:officraft"], "events": [`
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
		{"heading absent", `{"trigger":"x","content":"y","origin":"agent:O-197","subjects":["repo:officraft"]}`, "heading"},
		{"blank heading", `{"heading":"  ","trigger":"x","content":"y","origin":"agent:O-197","subjects":["repo:officraft"]}`, "heading"},
		{"blank trigger", `{"heading":"h","trigger":"  ","content":"x","origin":"agent:O-197","subjects":["repo:officraft"]}`, "trigger"},
		{"blank content", `{"heading":"h","trigger":"x","content":"","origin":"agent:O-197","subjects":["repo:officraft"]}`, "content"},
		{"no subject", `{"heading":"h","trigger":"x","content":"y","origin":"agent:O-197","subjects":[]}`, "subject"},
		{"unknown origin type", `{"heading":"h","trigger":"x","content":"y","origin":"vendor:acme","subjects":["repo:officraft"]}`, "vendor"},
		{"unknown subject type", `{"heading":"h","trigger":"x","content":"y","origin":"agent:O-197","subjects":["vendor:acme"]}`, "vendor"},
		{"malformed subject", `{"heading":"h","trigger":"x","content":"y","origin":"agent:O-197","subjects":["officraft"]}`, "officraft"},
		// 🔴 星等的值域也在這裡：它是**寫入者可以自己修好**的東西，所以它是 422
		// 而不是資料庫 CHECK 撞出來的 500。少了這兩行，把 loreImpactStarsError
		// 整個拿掉會全綠，而症狀是一個送錯星等的人收到「伺服器出事了」。
		{"impact_stars above the scale", `{"heading":"h","trigger":"x","content":"y","impact_stars":4,"origin":"agent:O-197","subjects":["repo:officraft"]}`, "impact_stars"},
		{"impact_stars below the scale", `{"heading":"h","trigger":"x","content":"y","impact_stars":-1,"origin":"agent:O-197","subjects":["repo:officraft"]}`, "impact_stars"},
	} {
		st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", tc.body)
		if st != 422 {
			t.Fatalf("%s: want 422, got %d %s", tc.name, st, body)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s: the refusal does not name %q: %s", tc.name, tc.want, body)
		}
	}
}

// A key the DTO does not declare is a 422, never a silent drop. This is the
// house rule, asserted HERE because the field set being closed is what stops a
// misspelled `content` from writing an entry with an empty body.
func TestLoreWriteRouteRefusesAnUnknownFieldRatherThanDroppingIt(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"heading": "h", "trigger": "x", "contentt": "the typo that empties the body",
		"content": "y",
		"origin": "agent:O-197", "subjects": ["repo:officraft"]
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

// Superseding over the wire re-statuses the predecessor and journals the act
// against the VERIFIED caller; a predecessor that does not exist refuses the
// whole write with a 404, leaving nothing behind.
func TestLoreWriteRouteSupersedesOnlyAnEntryThatExists(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", loreWriteJSON)
	if st != 200 {
		t.Fatalf("seed write: %d %s", st, body)
	}
	first := loreWriteBody(t, body).EntryId

	st, body = rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"heading": "h", "trigger": "x", "content": "y", "origin": "agent:O-197",
		"subjects": ["repo:officraft"], "supersedes": "`+first+`"
	}`)
	if st != 200 {
		t.Fatalf("supersede: want 200, got %d %s", st, body)
	}
	second := loreWriteBody(t, body)
	if second.Superseded != first {
		t.Fatalf("receipt does not name what was replaced: %+v", second)
	}
	if got := loreGovStatus(t, dal, first); got != "superseded" {
		t.Fatalf("the replaced entry is still %q", got)
	}
	events, err := dal.ListLoreGovernanceEvents(first)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != LoreGovSupersede ||
		events[0].ActorID != "m-lore-agent" || events[0].ReplacedBy != second.EntryId {
		t.Fatalf("the supersede left no usable journal row: %+v", events)
	}

	st, body = rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"heading": "h", "trigger": "x", "content": "y", "origin": "agent:O-197",
		"subjects": ["repo:officraft"], "supersedes": "lore-nope"
	}`)
	if st != 404 {
		t.Fatalf("supersede a ghost: want 404, got %d %s", st, body)
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
