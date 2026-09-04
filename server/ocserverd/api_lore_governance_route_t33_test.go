package main

// api_lore_governance_route_t33_test.go — T-33.
//
// 🔴 WHAT THESE TESTS ARE FOR, AND WHY THE EXISTING DAL SUITE DOES NOT COVER
// IT. dal_lore_governance_t33_test.go already pins loreRetireNeedsOwner and the
// journal, and it does so by calling the DAL directly — which is exactly why it
// could not say whether ANYTHING ELSE could reach that gate. Until these routes
// landed, the only caller of RetireLoreEntry in the whole tree was that test
// file. So every assertion below goes through a REAL HTTP REQUEST against the
// wired stack (auth middleware → RBAC choke → generated wrapper → handler),
// because "the DAL refuses it" and "the office refuses it" are two different
// claims and only the second one is worth anything to a caller.
//
// 🔴 THE TWO GATES ARE PINNED SEPARATELY AND THE REFUSALS ARE TOLD APART BY
// THEIR WORDING. A 403 from the route floor says "principal not permitted"; a
// 403 from the DAL names the rule it enforced. Asserting only the number would
// let the floor be deleted while the DAL's own check kept the status code
// identical — a mutant that changes nothing observable is a mutant nothing
// catches, and that is the failure this file exists to avoid.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// loreGovStack builds the wired stack plus the two identities the split needs:
// an ordinary agent (a member row with no role_key — classifyMember puts it at
// principalAgent) and the owner.
func loreGovStack(t *testing.T) (srvURL string, dal *DAL, agentTok, ownerTok, wardenTok string) {
	t.Helper()
	srv, dal, secret, api := newLessonsTestServerAPI(t)
	// T-33: the lore feature ships OFF, so every /api/lore/* row refuses 403 on a
	// default station. All 34 uses of this stack test the routes' OWN behaviour
	// (RBAC floors, 404s, 409s, body validation), which only exists downstream of
	// the feature gate — leaving the switch off here would turn every one of them
	// into an assertion about a 403 they do not mention.
	//
	// 🔴 THE SWITCH IS SET BEFORE THE FIRST REQUEST AND NEVER AGAIN. It is read
	// live per request (loreFeatureGate), not captured when the table was built,
	// so this assignment is what those requests see. The OFF behaviour of these
	// same routes lives in lore_toggle_t33_test.go, with a control.
	enableLoreForTest(api)
	now := time.Now().Unix()

	if err := dal.PutMember(Member{
		ID: "m-lore-agent", Name: "lore-agent", Kind: KindStaff, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put agent member: %v", err)
	}
	// A warden: the machine class. It is an authenticated identity that is NOT
	// a governance principal, which is the whole job of the route floor.
	if err := dal.PutMember(Member{
		ID: "m-lore-box", Name: "lore-box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put warden member: %v", err)
	}
	var err error
	if agentTok, err = mintJWT("m-lore-agent", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint agent token: %v", err)
	}
	if ownerTok, err = mintJWT("owner", "owner", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	if wardenTok, err = mintJWT("m-lore-box", "agent", 3600, secret, now, ""); err != nil {
		t.Fatalf("mint warden token: %v", err)
	}
	return srv.URL, dal, agentTok, ownerTok, wardenTok
}

func loreGovSeed(t *testing.T, dal *DAL, id string) {
	t.Helper()
	if err := dal.PutLoreEntry(t33Entry(id)); err != nil {
		t.Fatalf("seed entry %s: %v", id, err)
	}
}

func loreGovStatus(t *testing.T, dal *DAL, id string) string {
	t.Helper()
	got, err := dal.GetLoreEntry(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if got == nil {
		t.Fatalf("entry %s vanished", id)
	}
	return got.Status
}

// loreGovReceipt decodes a 200 body into the wire DTO, failing loudly rather
// than letting a wrong-shaped success masquerade as a right one.
func loreGovReceipt(t *testing.T, body string) LoreGovernanceDTO {
	t.Helper()
	var dto LoreGovernanceDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode receipt %q: %v", body, err)
	}
	return dto
}

// TestLoreRetireRouteReasonDecidesWhoMayFileIt is D11 as a wire fact.
//
// The two permissive arms are the POSITIVE CONTROL and they come first on
// purpose: without them a handler that refused every retirement would pass the
// interesting half of this test.
func TestLoreRetireRouteReasonDecidesWhoMayFileIt(t *testing.T) {
	url, dal, agentTok, ownerTok, _ := loreGovStack(t)

	for _, reason := range []string{LoreRetireExpired, LoreRetireMerged} {
		id := "e-route-" + reason
		loreGovSeed(t, dal, id)
		st, body := rosterREST(t, url, agentTok, "POST",
			"/api/lore/entries/"+id+"/retire", `{"reason":"`+reason+`"}`)
		if st != 200 {
			t.Fatalf("agent retire as %s: want 200, got %d %s", reason, st, body)
		}
		if got := loreGovStatus(t, dal, id); got != "retired" {
			t.Fatalf("agent retire as %s: status = %q, want retired", reason, got)
		}
		// The receipt has to carry the reason that was filed and the identity
		// the TOKEN names — an echo of the body would prove nothing about who
		// the server believes is asking.
		dto := loreGovReceipt(t, body)
		if dto.Reason != reason || dto.Kind != LoreGovRetire || dto.Status != "retired" {
			t.Fatalf("receipt for %s: %+v", reason, dto)
		}
		if dto.ActorId != "m-lore-agent" {
			t.Fatalf("receipt actor = %q, want the verified token subject", dto.ActorId)
		}
	}

	// The gate itself: 'falsified' is a judgement about truth.
	loreGovSeed(t, dal, "e-route-false")
	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-route-false/retire", `{"reason":"falsified"}`)
	if st != 403 {
		t.Fatalf("agent retire as falsified: want 403, got %d %s", st, body)
	}
	// 🔴 WHICH gate refused matters. The route floor is principalAgent, so this
	// caller is ABOVE the floor and got in — the refusal has to be the DAL's
	// per-reason rule, not "principal not permitted". Without this the whole
	// reason split could be replaced by an admin floor and the number would not
	// move.
	if strings.Contains(body, "principal not permitted") {
		t.Fatalf("agent retire as falsified was refused by the ROUTE FLOOR, not by "+
			"the reason gate — an agent must be able to reach this route to file "+
			"expired/merged: %s", body)
	}
	if got := loreGovStatus(t, dal, "e-route-false"); got != "active" {
		t.Fatalf("refused retire still changed status to %q", got)
	}
	evs, err := dal.ListLoreGovernanceEvents("e-route-false")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("refused retire wrote %d journal rows, want 0", len(evs))
	}

	// Control for the refusal: the SAME request from the owner lands.
	st, body = rosterREST(t, url, ownerTok, "POST",
		"/api/lore/entries/e-route-false/retire", `{"reason":"falsified","replaced_by":"e-better"}`)
	if st != 200 {
		t.Fatalf("owner retire as falsified: want 200, got %d %s", st, body)
	}
	if got := loreGovStatus(t, dal, "e-route-false"); got != "retired" {
		t.Fatalf("owner retire as falsified: status = %q, want retired", got)
	}
	dto := loreGovReceipt(t, body)
	if dto.Reason != LoreRetireFalsified || dto.ReplacedBy != "e-better" || dto.ActorId != "owner" {
		t.Fatalf("owner receipt: %+v", dto)
	}
}

// TestLoreRetireRouteRefusesAMachine pins the ROUTE FLOOR, which is the one
// thing the DAL cannot express: RetireLoreEntry takes an actor id and an
// is-owner bool and has no idea a warden exists.
//
// The agent arm is the positive control, and it is what makes this test able to
// fail in both directions: raise the floor and the control goes red, lower it
// and the warden arm does.
func TestLoreRetireRouteRefusesAMachine(t *testing.T) {
	url, dal, agentTok, _, wardenTok := loreGovStack(t)
	loreGovSeed(t, dal, "e-floor")

	st, body := rosterREST(t, url, wardenTok, "POST",
		"/api/lore/entries/e-floor/retire", `{"reason":"expired"}`)
	if st != 403 {
		t.Fatalf("warden retire: want 403, got %d %s", st, body)
	}
	if !strings.Contains(body, "principal not permitted") {
		t.Fatalf("warden retire was refused, but NOT by the route floor — the "+
			"refusal must come from Requires, before any body is read: %s", body)
	}
	if got := loreGovStatus(t, dal, "e-floor"); got != "active" {
		t.Fatalf("a refused machine still retired the entry (status %q)", got)
	}

	// Positive control: the identical request from an ordinary agent lands.
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-floor/retire", `{"reason":"expired"}`); st != 200 {
		t.Fatalf("POSITIVE CONTROL FAILED — an ordinary agent must be able to "+
			"retire an expired entry: %d %s", st, body)
	}
}

// TestLoreReviveRouteBringsAnEntryBackIntoRetrieval is the claim "retirement is
// not a delete" measured where it is actually made: the retrieval query.
//
// 🔑 The judge is ListLoreEntriesBySubject — the seam that excludes retired
// rows — and not the status column, because "the column says active" and "the
// thing that fetches memories fetches it again" are different facts and only
// the second one is what an agent experiences.
func TestLoreReviveRouteBringsAnEntryBackIntoRetrieval(t *testing.T) {
	url, dal, agentTok, ownerTok, _ := loreGovStack(t)
	loreGovSeed(t, dal, "e-revive")
	if err := dal.PutLoreSubject("e-revive", "e-repo"); err != nil {
		t.Fatalf("put subject: %v", err)
	}

	retrieved := func(what string) bool {
		t.Helper()
		list, err := dal.ListLoreEntriesBySubject("e-repo")
		if err != nil {
			t.Fatalf("%s: list by subject: %v", what, err)
		}
		for _, e := range list {
			if e.ID == "e-revive" {
				return true
			}
		}
		return false
	}

	if !retrieved("before") {
		t.Fatalf("POSITIVE CONTROL FAILED — the entry is not retrievable before " +
			"anything happened to it, so nothing below can mean anything")
	}
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-revive/retire", `{"reason":"expired"}`); st != 200 {
		t.Fatalf("agent retire: %d %s", st, body)
	}
	if retrieved("after retire") {
		t.Fatalf("a retired entry is still being retrieved")
	}

	// An ordinary agent may park an entry; it may NOT decide the entry holds
	// after all. The floor is what refuses, before a body is parsed.
	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-revive/revive", `{"reason":"I changed my mind"}`)
	if st != 403 {
		t.Fatalf("agent revive: want 403, got %d %s", st, body)
	}
	if !strings.Contains(body, "principal not permitted") {
		t.Fatalf("agent revive was refused, but NOT by the route floor — revive "+
			"declares Requires=owner and that is what must turn this away: %s", body)
	}
	if retrieved("after refused revive") {
		t.Fatalf("a refused revive still brought the entry back")
	}

	st, body = rosterREST(t, url, ownerTok, "POST",
		"/api/lore/entries/e-revive/revive", `{"reason":"the situation came back"}`)
	if st != 200 {
		t.Fatalf("owner revive: want 200, got %d %s", st, body)
	}
	if !retrieved("after revive") {
		t.Fatalf("REVIVE DID NOT BRING THE ENTRY BACK — retirement is only " +
			"reversible if this is true, and every claim that it is not a delete " +
			"rests on it")
	}
	dto := loreGovReceipt(t, body)
	if dto.Kind != LoreGovRevive || dto.Status != "active" || dto.ActorId != "owner" {
		t.Fatalf("revive receipt: %+v", dto)
	}

	// Reviving something that is not retired is refused rather than answered
	// "done": a 200 here would confirm a belief about the entry's state that is
	// wrong.
	if st, body := rosterREST(t, url, ownerTok, "POST",
		"/api/lore/entries/e-revive/revive", `{}`); st != 409 {
		t.Fatalf("revive of an active entry: want 409, got %d %s", st, body)
	}
}

// TestLoreRetireRouteRefusalsAreTheRightRefusals pins the error mapping. A
// typo'd reason answering 500 (or, worse, 200) is how a mis-filed retirement
// becomes indistinguishable from a real one afterwards.
func TestLoreRetireRouteRefusalsAreTheRightRefusals(t *testing.T) {
	url, dal, agentTok, ownerTok, _ := loreGovStack(t)
	loreGovSeed(t, dal, "e-typo")

	if st, body := rosterREST(t, url, ownerTok, "POST",
		"/api/lore/entries/e-typo/retire", `{"reason":"falsifed"}`); st != 422 {
		t.Fatalf("typo'd reason: want 422, got %d %s", st, body)
	}
	if got := loreGovStatus(t, dal, "e-typo"); got != "active" {
		t.Fatalf("a refused typo still retired the entry (status %q)", got)
	}
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-no-such-entry/retire", `{"reason":"expired"}`); st != 404 {
		t.Fatalf("unknown entry: want 404, got %d %s", st, body)
	}
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-typo/retire", `{}`); st != 422 {
		t.Fatalf("missing reason: want 422, got %d %s", st, body)
	}
	// Positive control for the three refusals above: the same route, same
	// caller, a reason it recognises.
	if st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/entries/e-typo/retire", `{"reason":"expired"}`); st != 200 {
		t.Fatalf("POSITIVE CONTROL FAILED — a well-formed retire must land: %d %s", st, body)
	}
}

// TestLoreRetireReachesAnAgentThroughTheMCPToolFace: agents do not send HTTP,
// they call MCP tools. A route an agent cannot name is a route an agent does
// not have, so the tool face is pinned through the same wired stack.
func TestLoreRetireReachesAnAgentThroughTheMCPToolFace(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)
	loreGovSeed(t, dal, "e-mcp")

	isErr, code, text := lessonsCall(t, url, agentTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"retire_lore_entry",`+
			`"arguments":{"entry_id":"e-mcp","reason":"merged","replaced_by":"e-mcp-2"}}}`)
	if isErr {
		t.Fatalf("retire_lore_entry over MCP: isError (code %q) %s", code, text)
	}
	if got := loreGovStatus(t, dal, "e-mcp"); got != "retired" {
		t.Fatalf("MCP retire: status = %q, want retired", got)
	}

	// And the gate holds on this face too: the tool is not a way around the
	// owner-only reason.
	loreGovSeed(t, dal, "e-mcp-false")
	isErr, _, text = lessonsCall(t, url, agentTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"retire_lore_entry",`+
			`"arguments":{"entry_id":"e-mcp-false","reason":"falsified"}}}`)
	if !isErr {
		t.Fatalf("retire_lore_entry as falsified over MCP must be refused: %s", text)
	}
	if got := loreGovStatus(t, dal, "e-mcp-false"); got != "active" {
		t.Fatalf("a refused MCP retire still changed status to %q", got)
	}
}
