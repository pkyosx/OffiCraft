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

const loreWriteJSON = `{
	"label": "boot context assembly",
	"symptoms": "two blocks disagree about the same fact",
	"short": "the fold happens in one place",
	"falsify": "a second assembler appears",
	"instance": "T-33 slot 3",
	"residual_risk": "says nothing about who may call the fold",
	"origin": "agent:O-197",
	"subjects": ["repo:officraft"],
	"actions": ["read-code"]
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
	if dto.Degraded {
		t.Fatalf("an entry with both a falsifier and an instance came back degraded: %+v", dto)
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

// 🔴 THE OWNER'S 2026-09-01 RULING AS A WIRE FACT: a thin entry LANDS and is
// reported, it is not refused. A hard gate here is the version of this feature
// that produces invented falsifiers, and an invented one cannot be counted.
func TestLoreWriteRouteAcceptsAThinEntryAndSaysSo(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"symptoms": "a rule exists and no tool implements it",
		"short": "a capability nobody can reach reads like a capability",
		"origin": "agent:O-197",
		"subjects": ["repo:officraft"]
	}`)
	if st != 200 {
		t.Fatalf("thin write: want 200, got %d %s", st, body)
	}
	if dto := loreWriteBody(t, body); !dto.Degraded {
		t.Fatalf("an entry with neither falsifier nor instance was not reported degraded: %+v", dto)
	}
}

// The two fields without which the row is unreachable are refused, and the
// refusal names which one. 422 rather than 400 keeps it the same answer every
// other body-validation refusal on this station gives.
func TestLoreWriteRouteRefusesAnEntryNobodyCouldRead(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	for _, tc := range []struct{ name, body, want string }{
		{"blank symptoms", `{"symptoms":"  ","short":"x","origin":"agent:O-197","subjects":["repo:officraft"]}`, "symptoms"},
		{"blank short", `{"symptoms":"x","short":"","origin":"agent:O-197","subjects":["repo:officraft"]}`, "short"},
		{"no subject", `{"symptoms":"x","short":"y","origin":"agent:O-197","subjects":[]}`, "subject"},
		{"unknown origin type", `{"symptoms":"x","short":"y","origin":"vendor:acme","subjects":["repo:officraft"]}`, "vendor"},
		{"unknown subject type", `{"symptoms":"x","short":"y","origin":"agent:O-197","subjects":["vendor:acme"]}`, "vendor"},
		{"malformed subject", `{"symptoms":"x","short":"y","origin":"agent:O-197","subjects":["officraft"]}`, "officraft"},
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
// misspelled `short` from writing an entry with an empty body.
func TestLoreWriteRouteRefusesAnUnknownFieldRatherThanDroppingIt(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/entries", `{
		"symptoms": "x", "shortt": "the typo that empties the body",
		"short": "y", "origin": "agent:O-197", "subjects": ["repo:officraft"]
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
		"symptoms": "x", "short": "y", "origin": "agent:O-197",
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
		"symptoms": "x", "short": "y", "origin": "agent:O-197",
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
