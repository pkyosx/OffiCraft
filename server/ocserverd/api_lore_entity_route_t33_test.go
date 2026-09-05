package main

// api_lore_entity_route_t33_test.go — T-33, 對象審核 on the wire.
//
// 🔴 WHAT THESE ADD OVER dal_lore_entity_t33_test.go. That file calls the DAL
// directly, which is exactly why it cannot say anything about WHO may reach it:
// ApproveLoreEntity and MergeLoreEntity take no principal at all, because the
// owner's ruling (rc-139a5ab99a19: 「待審，我跟 mira 有 admin 權限的才行」) is a
// statement about principal CLASS and the route table is the only place that
// can be said. So the gate exists ONLY as a row in defaultRouteSpecs, and a
// test that does not fire a real request through the auth middleware and the
// RBAC choke is not testing it.
//
// 🔴 THE REFUSAL IS ASSERTED BY ITS WORDING, NOT ONLY BY ITS NUMBER. A 403 from
// the route floor says "principal not permitted"; a 403 from anywhere else says
// something different. Checking 403 alone would let the floor be replaced by an
// unrelated refusal with the status code unchanged.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// loreEntityStack wires the stack plus the three identities the ruling
// separates: an ordinary agent (a member row with no role_key), an ADMIN agent
// (role_key "assistant" — mira's class), and the owner.
func loreEntityStack(t *testing.T) (srvURL string, dal *DAL, agentTok, adminTok, ownerTok string) {
	t.Helper()
	srv, dal, secret, api := newLessonsTestServerAPI(t)
	// T-33: these tests are about the review queue's OWN behaviour, which only
	// exists downstream of the station-wide lore switch. See loreGovStack.
	enableLoreForTest(api)
	now := time.Now().Unix()

	for _, m := range []Member{
		{ID: "m-lore-agent", Name: "lore-agent", Kind: KindStaff, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive},
		{ID: "m-lore-mira", Name: "lore-mira", Kind: KindStaff, RoleKey: adminRoleKey,
			Effort: "medium", DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive},
	} {
		if err := dal.PutMember(m); err != nil {
			t.Fatalf("put member %s: %v", m.ID, err)
		}
	}
	var err error
	if agentTok, err = mintJWT("m-lore-agent", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint agent token: %v", err)
	}
	if adminTok, err = mintJWT("m-lore-mira", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	if ownerTok, err = mintJWT("owner", "owner", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	return srv.URL, dal, agentTok, adminTok, ownerTok
}

func loreEntityQueue(t *testing.T, url, token string) []LorePendingEntityRowDTO {
	t.Helper()
	st, body := rosterREST(t, url, token, "GET", "/api/lore/entities/pending", "")
	if st != 200 {
		t.Fatalf("list pending: want 200, got %d %s", st, body)
	}
	var rows []LorePendingEntityRowDTO
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode queue %q: %v", body, err)
	}
	return rows
}

func loreEntityIDFor(t *testing.T, rows []LorePendingEntityRowDTO, canonical string) string {
	t.Helper()
	for _, row := range rows {
		if row.Canonical == canonical {
			return row.EntityId
		}
	}
	t.Fatalf("no pending row carries %q: %+v", canonical, rows)
	return ""
}

// TestLoreEntityReviewRoutesRefuseAnOrdinaryAgent is the owner's ruling as a
// wire fact, and it is the most important assertion in this file: 「待審，我跟
// mira 有 admin 權限的才行」.
//
// The admin arm runs FIRST as the positive control. Without it a stack that
// refused every caller — a typo'd Requires, a handler that never wired — would
// pass the interesting half of this test while serving nobody.
func TestLoreEntityReviewRoutesRefuseAnOrdinaryAgent(t *testing.T) {
	url, dal, agentTok, adminTok, ownerTok := loreEntityStack(t)
	t33Entity(t, dal, "en-real", "repo", "repo:officraft")
	t33Mint(t, dal, "repo:offcraft", "agent:Kylo", "agent:Kyra")
	rows := loreEntityQueue(t, url, adminTok)

	approveMe := loreEntityIDFor(t, rows, "repo:offcraft")
	mergeMe := loreEntityIDFor(t, rows, "agent:Kylo")
	agentMustNotTouch := loreEntityIDFor(t, rows, "agent:Kyra")

	// ── the positive control: admin and owner both get through ──────────────
	st, body := rosterREST(t, url, adminTok, "POST",
		"/api/lore/entities/"+approveMe+"/approve", `{}`)
	if st != 200 {
		t.Fatalf("admin approve: want 200, got %d %s", st, body)
	}
	st, body = rosterREST(t, url, ownerTok, "POST",
		"/api/lore/entities/"+mergeMe+"/merge", `{"into":"en-real"}`)
	if st != 200 {
		t.Fatalf("owner merge: want 200, got %d %s", st, body)
	}

	// ── the gate ────────────────────────────────────────────────────────────
	for _, tc := range []struct{ name, method, path, body string }{
		{"list", "GET", "/api/lore/entities/pending", ""},
		{"approve", "POST", "/api/lore/entities/" + agentMustNotTouch + "/approve", `{}`},
		{"merge", "POST", "/api/lore/entities/" + agentMustNotTouch + "/merge", `{"into":"en-real"}`},
	} {
		st, body := rosterREST(t, url, agentTok, tc.method, tc.path, tc.body)
		if st != 403 {
			t.Fatalf("ordinary agent %s: want 403, got %d %s", tc.name, st, body)
		}
		if !strings.Contains(body, "principal not permitted") {
			t.Fatalf("ordinary agent %s was refused by something OTHER than the route "+
				"floor — the class gate is the only place the owner's ruling is "+
				"written down, so a refusal from anywhere else means it is gone: %s",
				tc.name, body)
		}
	}

	// The refused entity is still parked: a 403 must change nothing.
	if got := loreEntityIDFor(t, loreEntityQueue(t, url, adminTok), "agent:Kyra"); got != agentMustNotTouch {
		t.Fatalf("the refused entity moved: %q", got)
	}
}

// TestLoreEntityPendingRouteCountsAndThenForgetsAnApprovedEntity is the queue's
// two claims over one request pair: the count is real, and working the queue
// actually empties it.
func TestLoreEntityPendingRouteCountsAndThenForgetsAnApprovedEntity(t *testing.T) {
	url, dal, _, adminTok, _ := loreEntityStack(t)
	t33Entity(t, dal, "en-real", "repo", "repo:officraft")
	t33Mint(t, dal, "repo:offcraft", "repo:officraft")
	t33Mint(t, dal, "repo:offcraft")

	rows := loreEntityQueue(t, url, adminTok)
	if len(rows) != 1 {
		t.Fatalf("queue = %+v, want only the minted subject (the approved one must not be in it)", rows)
	}
	row := rows[0]
	if row.Canonical != "repo:offcraft" || row.Type != "repo" || row.Name != "offcraft" {
		t.Fatalf("queue row = %+v", row)
	}
	if row.Entries != 2 {
		t.Fatalf("entries = %d, want 2 counted from the two writes", row.Entries)
	}
	if row.CreatedTs == 0 {
		t.Fatal("created_ts = 0 — the queue cannot say how long this name has been parked")
	}

	st, body := rosterREST(t, url, adminTok, "POST",
		"/api/lore/entities/"+row.EntityId+"/approve", `{"reason":"real repo, one letter short"}`)
	if st != 200 {
		t.Fatalf("approve: want 200, got %d %s", st, body)
	}
	var receipt LoreEntityGovernanceDTO
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		t.Fatalf("decode receipt %q: %v", body, err)
	}
	if receipt.Pending || receipt.Kind != LoreGovEntityApprove || receipt.MergedInto != "" {
		t.Fatalf("receipt = %+v", receipt)
	}
	// The actor has to be the identity the TOKEN names; an echo of the body
	// would prove nothing about who the server believes is asking.
	if receipt.ActorId != "m-lore-mira" || receipt.Reason != "real repo, one letter short" {
		t.Fatalf("receipt actor/reason = %+v", receipt)
	}

	if after := loreEntityQueue(t, url, adminTok); len(after) != 0 {
		t.Fatalf("the approved entity is still in the queue: %+v", after)
	}
}

// TestLoreEntityMergeRouteAliasesTheSourceOntoTheSurvivor pins the merge over
// the wire, including the refusals the design says must never be silent.
func TestLoreEntityMergeRouteAliasesTheSourceOntoTheSurvivor(t *testing.T) {
	url, dal, _, adminTok, _ := loreEntityStack(t)
	t33Entity(t, dal, "en-real", "repo", "repo:officraft")
	t33Mint(t, dal, "repo:offcraft", "agent:Kylo")
	rows := loreEntityQueue(t, url, adminTok)
	src := loreEntityIDFor(t, rows, "repo:offcraft")
	stillPending := loreEntityIDFor(t, rows, "agent:Kylo")

	for _, tc := range []struct {
		name string
		into string
		want int
	}{
		{"a target that does not exist", "en-nothing", 404},
		{"a target that is itself pending", stillPending, 422},
		{"the source itself", src, 422},
	} {
		st, body := rosterREST(t, url, adminTok, "POST",
			"/api/lore/entities/"+src+"/merge", `{"into":"`+tc.into+`"}`)
		if st != tc.want {
			t.Fatalf("merge into %s: want %d, got %d %s", tc.name, tc.want, st, body)
		}
	}
	// Every refusal above left the source exactly where it was — a merge that
	// half-happened would drop it out of the queue while belonging to nobody.
	if got := loreEntityIDFor(t, loreEntityQueue(t, url, adminTok), "repo:offcraft"); got != src {
		t.Fatalf("the source moved after the refusals: %q", got)
	}

	st, body := rosterREST(t, url, adminTok, "POST",
		"/api/lore/entities/"+src+"/merge", `{"into":"en-real"}`)
	if st != 200 {
		t.Fatalf("merge: want 200, got %d %s", st, body)
	}
	var receipt LoreEntityGovernanceDTO
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		t.Fatalf("decode receipt %q: %v", body, err)
	}
	if receipt.MergedInto != "en-real" || receipt.Pending || receipt.Kind != LoreGovEntityMerge {
		t.Fatalf("receipt = %+v", receipt)
	}

	// The survivor is what the old key now resolves to — asserted through the
	// write seam, because `merged_into` and the alias row are only worth
	// anything if the resolver honours them.
	after := t33Mint(t, dal, "repo:offcraft")
	if len(after.Minted) != 0 || len(after.SubjectIDs) != 1 || after.SubjectIDs[0] != "en-real" {
		t.Fatalf("after the merge the old key resolved to %+v (minted %+v)",
			after.SubjectIDs, after.Minted)
	}

	// Merging a source that is no longer pending is a 409, never a silent 200.
	st, body = rosterREST(t, url, adminTok, "POST",
		"/api/lore/entities/"+src+"/merge", `{"into":"en-real"}`)
	if st != 409 {
		t.Fatalf("re-merge: want 409, got %d %s", st, body)
	}
}

// TestLoreEntityRoutesRefuseAMachine keeps the door shut on the class that is
// authenticated and is not a governance principal at all.
func TestLoreEntityRoutesRefuseAMachine(t *testing.T) {
	url, dal, _, adminTok, _ := loreEntityStack(t)
	if err := dal.PutMember(Member{
		ID: "m-lore-box", Name: "lore-box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put warden: %v", err)
	}
	wardenTok, err := mintJWT("m-lore-box", "agent", 3600, []byte(interopSecret), time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint warden token: %v", err)
	}
	t33Mint(t, dal, "repo:offcraft")
	id := loreEntityIDFor(t, loreEntityQueue(t, url, adminTok), "repo:offcraft")

	for _, tc := range []struct{ name, method, path, body string }{
		{"list", "GET", "/api/lore/entities/pending", ""},
		{"approve", "POST", "/api/lore/entities/" + id + "/approve", `{}`},
		{"merge", "POST", "/api/lore/entities/" + id + "/merge", `{"into":"en-real"}`},
	} {
		if st, body := rosterREST(t, url, wardenTok, tc.method, tc.path, tc.body); st != 403 {
			t.Fatalf("warden %s: want 403, got %d %s", tc.name, st, body)
		}
	}
}

// TestLoreEntityPendingRouteCarriesTheReviewPacket is the surviving half of the
// owner's second ruling on the wire: 「我希望 agent 做完功課以後給建議並提出我一
// 眼就可以判斷的資訊，我還是做最後的裁決」. The EVIDENCE has to reach him, and
// reading it has to change nothing on its own.
//
// ⚠️ 「給建議」 was built as a computed `suggestion` / `merge_target` pair and the
// owner removed it on 2026-09-05 (「ai 會笨到產生大小寫不一樣的對象嗎」), so the
// assertions on those two fields are gone. 「我還是做最後的裁決」 is asserted
// BELOW and is untouched — it was never a property of the suggestion, it is a
// property of the LIST ROUTE, and that is exactly why removing the suggestion
// could not weaken it.
func TestLoreEntityPendingRouteCarriesTheReviewPacket(t *testing.T) {
	url, dal, _, adminTok, _ := loreEntityStack(t)
	t33Entity(t, dal, "en-real", "repo", "repo:officraft")
	if _, err := dal.CreateLoreEntry(LoreWrite{
		Heading: "h", Trigger: "t", Content: "the fold happens in exactly one place",
		Origin: "agent:O-197", Subjects: []string{"repo:OffiCraft"}, ActorID: "m-writer",
	}, 100); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows := loreEntityQueue(t, url, adminTok)
	if len(rows) != 1 {
		t.Fatalf("queue = %+v", rows)
	}
	row := rows[0]
	if len(row.Similar) != 1 || row.Similar[0].Reason != LoreSimilarSameNormalized ||
		row.Similar[0].EntityId != "en-real" {
		t.Fatalf("similar = %+v — the REASON is the payload; a candidate without one is a "+
			"score in disguise", row.Similar)
	}
	if row.SampleShort != "the fold happens in exactly one place" {
		t.Fatalf("sample_short = %q", row.SampleShort)
	}

	// 🔴 READING THE QUEUE ACTS ON NOTHING. The entity is still pending after a
	// list that showed an exact fold of an existing name, and the merge still has
	// to be called by a human on an admin token. A version that acted on its own
	// evidence would answer this same list with an empty queue.
	entity, err := dal.GetLoreEntity(row.EntityId)
	if err != nil || entity == nil {
		t.Fatalf("get entity: %v %v", entity, err)
	}
	if !entity.Pending || entity.MergedInto != "" {
		t.Fatalf("reading the queue CHANGED the entity: %+v — the verdict is the reviewer's", entity)
	}

	// A row nothing resembles reads the same way over the wire: 「nothing in the
	// ontology looks like this」 is a FINDING, so it must arrive as `[]` and never
	// as `null` — a reader that has to treat the two alike eventually treats one
	// of them wrongly.
	t33Mint(t, dal, "human:Seth")
	seen := false
	for _, r := range loreEntityQueue(t, url, adminTok) {
		if r.Canonical != "human:Seth" {
			continue
		}
		seen = true
		if r.Similar == nil || len(r.Similar) != 0 {
			t.Fatalf("human:Seth row = %+v, want an EMPTY, non-null candidate list", r)
		}
	}
	if !seen {
		t.Fatalf("human:Seth never appeared in the queue — the loop below asserted nothing")
	}
}

// TestLoreEntityPendingRouteCarriesTheEmptinessAndTheEntries is round 3 on the
// wire (owner 2026-09-04: 「為什麼核可的可見內容這麼少 我根本無從審核起」).
//
// 🔴 IT ASSERTS THE THREE NEW FACTS *THROUGH THE ROUTE*, not at the DAL, because
// the failure this round is repairing was a serving failure: the columns were in
// the table the whole time and the row on the screen did not carry them. A DAL
// test cannot tell a filled struct field from a served one.
func TestLoreEntityPendingRouteCarriesTheEmptinessAndTheEntries(t *testing.T) {
	url, dal, _, adminTok, _ := loreEntityStack(t)

	// One name that was USED — two entries, one of them retired — and one that
	// was minted and never used again. On the old wire both would have shown a
	// count and a sample and nothing to tell them apart.
	kept, err := dal.CreateLoreEntry(LoreWrite{
		Heading: "the queue showed one sample and hid the rest",
		Trigger: "I am about to review a name I cannot see behind",
		Content: "the queue showed one sample and no way to open the rest",
		Origin:  "agent:O-197", Subjects: []string{"repo:used"}, ActorID: "m-minter",
	}, 100)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	gone, err := dal.CreateLoreEntry(LoreWrite{
		Heading: "this one got retired", Trigger: "I am about to be retired", Content: "c",
		Origin: "agent:O-197", Subjects: []string{"repo:used"}, ActorID: "m-minter",
	}, 200)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if err := dal.RetireLoreEntry(gone.EntryID, LoreRetireExpired, "m-minter", "", false, 300); err != nil {
		t.Fatalf("retire: %v", err)
	}
	orphan := t33Mint(t, dal, "repo:orphan")
	if _, err := dal.wdb.Exec(`DELETE FROM lore_subject WHERE entry_id = ?`, orphan.EntryID); err != nil {
		t.Fatalf("unfile: %v", err)
	}

	var used, never LorePendingEntityRowDTO
	for _, r := range loreEntityQueue(t, url, adminTok) {
		switch r.Canonical {
		case "repo:used":
			used = r
		case "repo:orphan":
			never = r
		}
	}
	if used.EntityId == "" || never.EntityId == "" {
		t.Fatal("the queue did not carry both minted names")
	}

	if used.CreatedBy != "m-minter" {
		t.Fatalf("repo:used created_by = %q, want the minting actor over the wire — the "+
			"column has been written since 00081 and no response carried it", used.CreatedBy)
	}
	if used.Entries != 1 || used.EntriesEver != 2 {
		t.Fatalf("repo:used = %d now / %d ever, want 1/2", used.Entries, used.EntriesEver)
	}
	if len(used.EntryRefs) != 1 || used.EntryRefs[0].EntryId != kept.EntryID ||
		used.EntryRefs[0].Trigger != "I am about to review a name I cannot see behind" ||
		used.EntryRefs[0].Status != "active" {
		t.Fatalf("repo:used entry_refs = %+v — every retrievable entry, identified by id and "+
			"by 第 1 格, is what makes the row reviewable at all", used.EntryRefs)
	}

	if never.Entries != 0 || never.EntriesEver != 0 {
		t.Fatalf("repo:orphan = %d/%d, want 0/0", never.Entries, never.EntriesEver)
	}
	if never.EntryRefs == nil || len(never.EntryRefs) != 0 {
		t.Fatalf("repo:orphan entry_refs = %v, want `[]` on the wire: null would leave a "+
			"reader to treat 「nothing filed」 and 「not answered」 as the same thing",
			never.EntryRefs)
	}
	// 🔴 `never.Suggestion` WAS ASSERTED HERE — empty, because nothing resembled
	// the name AND nothing ever used it, and those two facts pointed opposite
	// ways. The field went with the rule (owner 2026-09-05). The two facts it
	// weighed are both still asserted immediately above, which is the half of
	// this test that was ever about serving 「可見內容」.
}
