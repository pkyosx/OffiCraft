package main

// api_lore_proposal_accept_route_t33_test.go — T-33. 核可一份提案，在線上.
//
// 🔴 WHY THIS FILE EXISTS BESIDE dal_lore_proposal_t33_test.go. That suite calls
// ApplyLoreProposal directly, and ApplyLoreProposal takes an actorID as an
// ARGUMENT — it cannot say who is allowed to supply one, and it never could.
// 「誰有資格核可」 is a statement about principal CLASS, and the route table is
// the only place in this codebase where that can be written down. The owner made
// it on rc-a896af93d4f9 — 「你 ＋ 銀月（沿用現有前例）」 — so the gate exists ONLY
// as `Requires: principalAdminAgent` on one row of defaultRouteSpecs, and a test
// that does not fire a real request through the auth middleware and the RBAC
// choke is not testing it at all.
//
// 🔴 EVERY REFUSAL BELOW CARRIES A POSITIVE CONTROL, and the control runs FIRST
// wherever the ordering allows. This is the discipline dal_lore_governance_t33
// _test.go states at its head: a test that only asserts 「這個被擋下來」 passes
// unchanged against an implementation that refuses EVERYBODY — a typo'd
// Requires, a handler that was never wired, a route that 404s. Such a test has
// no power to discriminate, and it is worse than no test because it looks like
// coverage.
//
// 🔴 THE 403 IS ASSERTED BY ITS WORDING TOO. A 403 from the route floor says
// "principal not permitted"; a 403 from anywhere else says something different.
// Checking the number alone would let the class gate be replaced by an unrelated
// refusal with nothing turning red.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// loreAcceptStack wires the stack plus the FOUR identities this route separates:
// an ordinary agent (a member row with no role_key — the proposer's class), an
// ADMIN agent (role_key "assistant" — 銀月's class), the owner, and a warden (the
// machine class, an authenticated identity that is not a governance principal).
func loreAcceptStack(t *testing.T) (srvURL string, dal *DAL, agentTok, adminTok, ownerTok, wardenTok string) {
	t.Helper()
	srv, dal, secret, api := newLessonsTestServerAPI(t)
	// T-33: the lore feature ships OFF; these tests are about this route's OWN
	// behaviour, which only exists downstream of the station-wide switch. The
	// OFF behaviour lives in lore_toggle_t33_test.go. See loreGovStack.
	enableLoreForTest(api)
	now := time.Now().Unix()

	for _, m := range []Member{
		{ID: "m-accept-agent", Name: "accept-agent", Kind: KindStaff, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive},
		{ID: "m-accept-mira", Name: "accept-mira", Kind: KindStaff, RoleKey: adminRoleKey,
			Effort: "medium", DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive},
		{ID: "m-accept-box", Name: "accept-box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive},
	} {
		if err := dal.PutMember(m); err != nil {
			t.Fatalf("put member %s: %v", m.ID, err)
		}
	}
	var err error
	if agentTok, err = mintJWT("m-accept-agent", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint agent token: %v", err)
	}
	if adminTok, err = mintJWT("m-accept-mira", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	if ownerTok, err = mintJWT("owner", "owner", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	if wardenTok, err = mintJWT("m-accept-box", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint warden token: %v", err)
	}
	return srv.URL, dal, agentTok, adminTok, ownerTok, wardenTok
}

// loreAcceptUpdateBody is a WHOLE new version, as an `update` proposal must be:
// 四格 plus the entire 第 5 格. `marker` distinguishes one seed from another so a
// test can say WHICH version landed rather than that something did.
func loreAcceptUpdateBody(base, marker string) string {
	return `{
		"kind":"update",
		"base_sha256":"` + base + `",
		"encountered":"T-33 slot 5, wiring the accept route",
		"fault":"misled",
		"evidence":"the entry is retrieved for a situation it does not describe",
		"trigger":"two blocks disagree about the same fact",
		"content":"` + marker + `",
		"retire_when":"等只剩一個組裝器",
		"problem":"T-33 slot 3",
		"events":[{"happened_ts":1788440000,"what":"` + marker + ` was proposed"}]}`
}

// loreAcceptSeed writes an entry as the AGENT and files one `update` proposal
// against it, also as the agent. Proposer and accepter are deliberately
// different identities in every test below: a revision signed by the proposer is
// the one way this route could be wrong while every status code looked right.
func loreAcceptSeed(t *testing.T, url, agentTok, marker string) (entryID, proposalID, proposedSHA string) {
	t.Helper()
	entryID, sha := loreProposalSeed(t, url, agentTok)
	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreAcceptUpdateBody(sha, marker))
	if st != 200 {
		t.Fatalf("seed proposal %q: %d %s", marker, st, body)
	}
	var receipt LoreProposalReceiptDTO
	if err := json.Unmarshal([]byte(body), &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	return entryID, receipt.ProposalId, receipt.Sha256
}

func loreAcceptPath(entryID, proposalID string) string {
	return "/api/lore/entries/" + entryID + "/proposals/" + proposalID + "/accept"
}

func loreAcceptEntry(t *testing.T, url, tok, entryID string) LoreEntryDetailDTO {
	t.Helper()
	st, body := rosterREST(t, url, tok, "GET", "/api/lore/entries/"+entryID, "")
	if st != 200 {
		t.Fatalf("read entry %s: %d %s", entryID, st, body)
	}
	var got LoreEntryDetailDTO
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode entry detail: %v", err)
	}
	return got
}

// TestLoreProposalAcceptRouteFloorIsTheOwnersRuling is rc-a896af93d4f9 as a wire
// fact: 「你 ＋ 銀月（沿用現有前例）」 — the owner and an admin agent may accept, an
// ordinary agent may not, and the machine class does not get in the door.
//
// 🔴 THE TWO PASSING ARMS RUN FIRST, AND THEY ARE NOT DECORATION. Without them a
// stack that refused every caller would pass the interesting half of this test
// while serving nobody — which is precisely the state this ticket found the
// feature in: a mechanism nobody could reach.
func TestLoreProposalAcceptRouteFloorIsTheOwnersRuling(t *testing.T) {
	url, _, agentTok, adminTok, ownerTok, wardenTok := loreAcceptStack(t)

	byAdmin, adminProposal, _ := loreAcceptSeed(t, url, agentTok, "the admin arm landed")
	byOwner, ownerProposal, _ := loreAcceptSeed(t, url, agentTok, "the owner arm landed")
	refused, refusedProposal, refusedSHA := loreAcceptSeed(t, url, agentTok, "nobody below the floor landed this")

	// ── the positive control: admin agent and owner both get through ────────
	if st, body := rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(byAdmin, adminProposal), ""); st != 200 {
		t.Fatalf("admin agent accept: want 200, got %d %s", st, body)
	}
	if st, body := rosterREST(t, url, ownerTok, "POST",
		loreAcceptPath(byOwner, ownerProposal), ""); st != 200 {
		t.Fatalf("owner accept: want 200, got %d %s", st, body)
	}

	// ── the gate ────────────────────────────────────────────────────────────
	for _, tc := range []struct{ name, tok string }{
		{"ordinary agent", agentTok},
		{"warden (the machine class)", wardenTok},
	} {
		st, body := rosterREST(t, url, tc.tok, "POST",
			loreAcceptPath(refused, refusedProposal), "")
		if st != 403 {
			t.Fatalf("%s accept: want 403, got %d %s", tc.name, st, body)
		}
		if !strings.Contains(body, "principal not permitted") {
			t.Fatalf("%s was refused by something OTHER than the route floor — the "+
				"class gate is the only place rc-a896af93d4f9 is written down, so a "+
				"refusal from anywhere else means the ruling is gone: %s", tc.name, body)
		}
	}

	// 🔴 A 403 MUST CHANGE NOTHING. The refused entry still stands at the
	// version it was written with, and the proposal it refused is still there to
	// be accepted by somebody who may — which is also the second positive
	// control for the two arms above.
	before := loreAcceptEntry(t, url, agentTok, refused)
	if before.Content == "nobody below the floor landed this" {
		t.Fatalf("the refused proposal was applied anyway: %+v", before)
	}
	if len(before.Revisions) != 1 {
		t.Fatalf("a refused acceptance appended a revision: %+v", before.Revisions)
	}
	st, body := rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(refused, refusedProposal), "")
	if st != 200 {
		t.Fatalf("the SAME proposal accepted by an admin: want 200, got %d %s", st, body)
	}
	var applied LoreProposalAppliedDTO
	if err := json.Unmarshal([]byte(body), &applied); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if applied.Sha256 != refusedSHA {
		t.Fatalf("the accepted digest is not the proposal's own: %q vs %q",
			applied.Sha256, refusedSHA)
	}
}

// TestLoreProposalAcceptRouteRefusesAProposalFiledAgainstAnotherEntry pins the
// half of the address that is easiest to drop.
//
// 🔴 PROPOSAL IDS ARE GLOBAL. A handler that read only `proposal_id` would work
// perfectly for every honest caller, and would let
// `/entries/<the wrong entry>/proposals/<a real id>/accept` REWRITE the entry the
// proposal actually belongs to while the path named a different one. That is the
// revision route's entry-scoping rule (a revision of another entry is a 404),
// except this route WRITES — so the same silence costs somebody else's memory.
func TestLoreProposalAcceptRouteRefusesAProposalFiledAgainstAnotherEntry(t *testing.T) {
	url, _, agentTok, adminTok, _, _ := loreAcceptStack(t)

	mine, proposal, _ := loreAcceptSeed(t, url, agentTok, "this belongs to its own entry")
	other, _, _ := loreAcceptSeed(t, url, agentTok, "this entry proposed nothing that follows")

	st, body := rosterREST(t, url, adminTok, "POST", loreAcceptPath(other, proposal), "")
	if st != 404 {
		t.Fatalf("a proposal reached through ANOTHER entry's address: want 404, got %d %s", st, body)
	}
	// 🔴 THE REFUSAL HAS TO SAY WHY. A bare 404 reads as 「no such proposal」 and
	// sends a reviewer off re-typing an id he already had right; this one names
	// the entry the proposal is really filed against.
	for _, want := range []string{proposal, mine, other, "constraint"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Fatalf("the 404 does not name %q — it is indistinguishable from "+
				"「no such proposal」: %s", want, body)
		}
	}
	// Nothing moved on either side.
	if got := loreAcceptEntry(t, url, agentTok, other); len(got.Revisions) != 1 {
		t.Fatalf("the mismatched acceptance wrote onto the entry in the path: %+v", got.Revisions)
	}
	if got := loreAcceptEntry(t, url, agentTok, mine); len(got.Revisions) != 1 {
		t.Fatalf("the mismatched acceptance wrote onto the proposal's OWN entry: %+v", got.Revisions)
	}

	// 🔴 THE POSITIVE CONTROL. Without it, a handler that 404'd every acceptance
	// — or one that rejected this pairing for some unrelated reason — would pass
	// everything above.
	if st, body := rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(mine, proposal), ""); st != 200 {
		t.Fatalf("the SAME proposal through its OWN entry's address: want 200, got %d %s", st, body)
	}
	if got := loreAcceptEntry(t, url, agentTok, mine); got.Content != "this belongs to its own entry" {
		t.Fatalf("the accepted version did not land: %+v", got)
	}
}

// TestLoreProposalAcceptRouteWritesTheProposedVersionSignedByTheAccepter is what
// 「核可」 actually means, on the wire: the entry becomes the proposal, 第 5 格 is
// replaced WHOLESALE rather than merged, and the one record of the verdict — the
// new revision's actor_id — names the ACCEPTER.
func TestLoreProposalAcceptRouteWritesTheProposedVersionSignedByTheAccepter(t *testing.T) {
	url, _, agentTok, adminTok, _, _ := loreAcceptStack(t)
	entryID, proposal, proposedSHA := loreAcceptSeed(t, url, agentTok, "the fold happens in lore_fold.go")

	// The state BEFORE, so that every assertion after the accept is a change
	// rather than a coincidence.
	before := loreAcceptEntry(t, url, agentTok, entryID)
	if before.Content == "the fold happens in lore_fold.go" {
		t.Fatalf("the seed already reads like the proposal — this test could not discriminate: %+v", before)
	}
	if len(before.Revisions) != 1 || before.Revisions[0].ActorId != "m-accept-agent" {
		t.Fatalf("the seeded entry is not the writer's own single revision: %+v", before.Revisions)
	}

	st, body := rosterREST(t, url, adminTok, "POST", loreAcceptPath(entryID, proposal), "")
	if st != 200 {
		t.Fatalf("accept: want 200, got %d %s", st, body)
	}
	var applied LoreProposalAppliedDTO
	if err := json.Unmarshal([]byte(body), &applied); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if applied.ProposalId != proposal || applied.EntryId != entryID {
		t.Fatalf("the receipt does not name what it applied: %+v", applied)
	}
	if applied.RevisionId == 0 || applied.EventsAfter != 1 {
		t.Fatalf("the receipt does not describe what landed: %+v", applied)
	}
	// 🔴 THE BYTES ARE THE PROPOSAL'S OWN, NOT A RE-RENDERING. The reviewer
	// approved a specific string; a digest computed here instead of read off the
	// stored proposal would quietly diverge the day the renderer changed, and
	// both sides would still look entirely normal.
	if applied.Sha256 != proposedSHA {
		t.Fatalf("the digest that landed is not the one the proposal was filed with: %q vs %q",
			applied.Sha256, proposedSHA)
	}

	after := loreAcceptEntry(t, url, agentTok, entryID)
	if after.Content != "the fold happens in lore_fold.go" {
		t.Fatalf("the entry did not become the proposed version: %+v", after)
	}
	if after.Sha256 != applied.Sha256 {
		t.Fatalf("the read route and the acceptance disagree about the digest: %q vs %q",
			after.Sha256, applied.Sha256)
	}
	// 第 5 格 整批換掉：the entry's own seeded event is GONE, not merged with.
	if len(after.Events) != 1 ||
		after.Events[0].What != "the fold happens in lore_fold.go was proposed" {
		t.Fatalf("第 5 格 was merged rather than replaced wholesale: %+v", after.Events)
	}
	// 🔴 THE ACCEPTER SIGNS IT. A revision carrying the PROPOSER's id would mean
	// the only record this station keeps of a verdict names the wrong person —
	// and since no arbitration journal has been ruled on, there is no second
	// place to look.
	if len(after.Revisions) != 2 {
		t.Fatalf("accepting did not append exactly one revision: %+v", after.Revisions)
	}
	var newest LoreRevisionRowDTO
	for _, rev := range after.Revisions {
		if rev.RevisionId > newest.RevisionId {
			newest = rev
		}
	}
	if newest.RevisionId != applied.RevisionId {
		t.Fatalf("the receipt names a revision the catalogue does not: %d vs %+v",
			applied.RevisionId, after.Revisions)
	}
	if newest.ActorId != "m-accept-mira" {
		t.Fatalf("the acceptance was signed by %q, not by the accepter: %+v",
			newest.ActorId, after.Revisions)
	}
}

// TestLoreProposalAcceptRouteMapsTheSeamsRefusalsOntoStatuses pins the three
// refusals the seam already names onto codes a client can act on. None of them
// is invented here — they are ApplyLoreProposal's own error values, and this
// layer only chooses the number.
func TestLoreProposalAcceptRouteMapsTheSeamsRefusalsOntoStatuses(t *testing.T) {
	url, _, agentTok, adminTok, _, _ := loreAcceptStack(t)

	// ── an id nothing carries: 404 ──────────────────────────────────────────
	entryID, _, _ := loreAcceptSeed(t, url, agentTok, "this one is real")
	if st, body := rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(entryID, "lp-nothing-carries-this"), ""); st != 404 {
		t.Fatalf("unknown proposal: want 404, got %d %s", st, body)
	}

	// ── a `remove` proposal: it proposes no version to write ────────────────
	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", `{
			"kind":"remove",
			"base_sha256":"`+loreAcceptEntry(t, url, agentTok, entryID).Sha256+`",
			"encountered":"T-33 slot 5, the removal arm",
			"fault":"never-true",
			"evidence":"the claim never held"}`)
	if st != 200 {
		t.Fatalf("seed remove proposal: %d %s", st, body)
	}
	var removal LoreProposalReceiptDTO
	if err := json.Unmarshal([]byte(body), &removal); err != nil {
		t.Fatalf("decode removal receipt: %v", err)
	}
	st, body = rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(entryID, removal.ProposalId), "")
	if st != 409 {
		t.Fatalf("accepting a `remove` proposal: want 409, got %d %s", st, body)
	}
	if !strings.Contains(body, "retire_lore_entry") {
		t.Fatalf("the refusal does not name the act a removal actually asks for: %s", body)
	}

	// ── a proposal the entry moved out from under: 409 with BOTH digests ────
	// The second proposal is filed against the SAME base as the first, so
	// accepting the first is what makes the second stale — which is the case
	// submit-time checking cannot see and read-time `stale` cannot enforce.
	baseSHA := loreAcceptEntry(t, url, agentTok, entryID).Sha256
	st, body = rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreAcceptUpdateBody(baseSHA, "the first accepted version"))
	if st != 200 {
		t.Fatalf("seed first update proposal: %d %s", st, body)
	}
	var first LoreProposalReceiptDTO
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatalf("decode first receipt: %v", err)
	}
	st, body = rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/"+entryID+"/proposals", loreAcceptUpdateBody(baseSHA, "the second, now arguing with text that is gone"))
	if st != 200 {
		t.Fatalf("seed second update proposal: %d %s", st, body)
	}
	var second LoreProposalReceiptDTO
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatalf("decode second receipt: %v", err)
	}
	// 🔴 THE POSITIVE CONTROL FOR THE 409 BELOW: the FIRST one lands, so the
	// second's refusal is about staleness rather than about this route refusing
	// every acceptance in this test.
	if st, body := rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(entryID, first.ProposalId), ""); st != 200 {
		t.Fatalf("the first of two proposals: want 200, got %d %s", st, body)
	}
	nowSHA := loreAcceptEntry(t, url, agentTok, entryID).Sha256
	st, body = rosterREST(t, url, adminTok, "POST",
		loreAcceptPath(entryID, second.ProposalId), "")
	if st != 409 {
		t.Fatalf("accepting a proposal the entry moved out from under: want 409, got %d %s", st, body)
	}
	// Both digests travel, so the reviewer can see WHAT it was written against
	// and what it now stands at rather than being told to go and look.
	for _, want := range []string{"changed while you were reviewing it", baseSHA, nowSHA} {
		if !strings.Contains(body, want) {
			t.Fatalf("the staleness 409 does not carry %q: %s", want, body)
		}
	}
	// Nothing was applied: the entry still reads as the FIRST proposal left it.
	if got := loreAcceptEntry(t, url, agentTok, entryID); got.Content != "the first accepted version" {
		t.Fatalf("the stale acceptance was applied anyway: %+v", got)
	}
}
