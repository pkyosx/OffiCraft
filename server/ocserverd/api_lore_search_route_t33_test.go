package main

// api_lore_search_route_t33_test.go — T-33, hop ② over real HTTP.

import (
	"encoding/json"
	"strings"
	"testing"
)

func loreSearchBody(t *testing.T, body string) LoreSearchResultDTO {
	t.Helper()
	var dto LoreSearchResultDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode search result %q: %v", body, err)
	}
	return dto
}

// loreSearchSeed writes one entry through the WRITE ROUTE (not the DAL) so the
// two halves of this feature are exercised against each other: if the writer
// files a subject the reader cannot resolve, this is where it shows.
func loreSearchSeed(t *testing.T, url, tok, subject, content string) string {
	t.Helper()
	st, body := rosterREST(t, url, tok, "POST", "/api/lore/entries", `{
		"heading": "something became visible that had not been",
		"trigger": "something is visible",
		"content": "`+content+`",
		"retire_when": "等只剩一個組裝器", "impact": "T-33 slot 3",
		"origin": "agent:O-197",
		"subjects": ["`+subject+`"]
	}`)
	if st != 200 {
		t.Fatalf("seed write: %d %s", st, body)
	}
	return loreWriteBody(t, body).EntryId
}

// The happy face, and the assertion that matters is `applied`: it is the only
// way a caller can tell 「this condition was applied」 from 「this condition was
// dropped on the floor」.
func TestLoreSearchRouteAnswersWithWhatItActuallyApplied(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)
	id := loreSearchSeed(t, url, agentTok, "repo:officraft", "the fold happens in one place")

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search",
		`{"subject":"repo:officraft","limit":5}`)
	if st != 200 {
		t.Fatalf("search: %d %s", st, body)
	}
	got := loreSearchBody(t, body)
	if len(got.Entries) != 1 || got.Entries[0].EntryId != id {
		t.Fatalf("entries: %+v", got.Entries)
	}
	a := got.Applied
	if a.Subject != "repo:officraft" || a.Limit != 5 || a.QueryMatch != loreQueryMatchLiteral {
		t.Fatalf("applied: %+v", a)
	}
	if !got.SubjectResolved || got.UnresolvedSubject != "" || got.Total != 1 || got.Truncated {
		t.Fatalf("result envelope: %+v", got)
	}
}

// 🔴 THE REFUSAL THIS ROUTE'S SHAPE EXISTS FOR. A selection condition the DTO
// does not declare is refused BY NAME. The design lists `context_labels` and
// never defines what it is compared against, so it is not implemented — and the
// failure direction of not implementing it has to be loud, or the caller gets a
// plausible answer to a question it did not ask.
func TestLoreSearchRouteRefusesAConditionItDoesNotImplement(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search",
		`{"context_labels":["anything"]}`)
	if st != 422 {
		t.Fatalf("undeclared condition: want 422, got %d %s", st, body)
	}
	if !strings.Contains(body, "context_labels") {
		t.Fatalf("the refusal does not name the condition: %s", body)
	}

	// 🔴 `actions` IS NOW ONE OF THOSE CONDITIONS. Owner removed the 活動 axis on
	// 2026-09-05; an agent (or a stale MCP descriptor) that keeps sending it must
	// be TOLD, because the failure mode of accepting-and-ignoring it is a caller
	// that believes it filtered and did not — and the wrong memories look exactly
	// like the right ones.
	st, body = rosterREST(t, url, agentTok, "POST", "/api/lore/search",
		`{"subject":"repo:officraft","actions":["build"]}`)
	if st != 422 {
		t.Fatalf("a removed condition: want 422, got %d %s", st, body)
	}
	if !strings.Contains(body, "actions") {
		t.Fatalf("the refusal does not name `actions`: %s", body)
	}
}

// The counterpart, and it is here to keep the design note honest rather than to
// bless the behaviour: the SAME word on the query string is accepted and
// ignored. That is why the conditions had to go in the body — the verb protects
// nothing.
func TestLoreSearchRouteShowsWhyTheConditionsAreNotInTheQueryString(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST",
		"/api/lore/search?subject=repo:officraft", `{}`)
	if st != 200 {
		t.Fatalf("query-string condition: want it IGNORED with 200, got %d %s", st, body)
	}
	got := loreSearchBody(t, body)
	if got.Applied.Subject != "" {
		t.Fatalf("a query-string condition was APPLIED: %+v", got.Applied)
	}
}

// 「this subject has nothing」 and 「this subject does not exist」 are different
// answers, over the wire as well as in the DAL.
func TestLoreSearchRouteTellsAnEmptySubjectApartFromAMissingOne(t *testing.T) {
	url, dal, agentTok, _, _ := loreGovStack(t)
	if _, err := dal.wdb.Exec(
		`INSERT INTO entity (id, type, canonical) VALUES ('e-quiet','repo','repo:quiet')`); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search", `{"subject":"repo:quiet"}`)
	if st != 200 {
		t.Fatalf("empty subject: %d %s", st, body)
	}
	if got := loreSearchBody(t, body); !got.SubjectResolved || len(got.Entries) != 0 {
		t.Fatalf("a real but empty subject: %+v", got)
	}

	st, body = rosterREST(t, url, agentTok, "POST", "/api/lore/search", `{"subject":"repo:ghost"}`)
	if st != 200 {
		t.Fatalf("missing subject: %d %s", st, body)
	}
	got := loreSearchBody(t, body)
	if got.SubjectResolved || got.UnresolvedSubject != "repo:ghost" {
		t.Fatalf("a subject that does not exist: %+v", got)
	}
}

// A bad limit is refused rather than clamped, over the wire.
func TestLoreSearchRouteRefusesAnOutOfRangeLimit(t *testing.T) {
	url, _, agentTok, _, _ := loreGovStack(t)

	st, body := rosterREST(t, url, agentTok, "POST", "/api/lore/search", `{"limit":500}`)
	if st != 422 {
		t.Fatalf("limit 500: want 422, got %d %s", st, body)
	}
	if !strings.Contains(body, "500") {
		t.Fatalf("the refusal does not say what was sent: %s", body)
	}
}

// The floor: a machine is refused at the door, and by the ROUTE FLOOR's wording.
func TestLoreSearchRouteRefusesAMachineAtTheDoor(t *testing.T) {
	url, _, _, _, wardenTok := loreGovStack(t)

	st, body := rosterREST(t, url, wardenTok, "POST", "/api/lore/search", `{}`)
	if st != 403 {
		t.Fatalf("warden search: want 403, got %d %s", st, body)
	}
	if !strings.Contains(body, "principal not permitted") {
		t.Fatalf("refused by something other than the route floor: %s", body)
	}
}
