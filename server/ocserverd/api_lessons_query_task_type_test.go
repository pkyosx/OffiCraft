package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// api_lessons_query_task_type_test.go — T-2 follow-up.
//
// 🔴 WHAT WAS STILL OPEN AFTER T-2 SHIPPED, AND WHY IT MATTERED.
// T-2 retired the lessons `task_type` axis and built two refusals:
//
//   - the MCP tool face, which refuses the argument by PRESENCE before
//     dispatch (fillLessonsIdentityArgs → errLessonsTaskTypeRetired), and
//   - the REST request BODY, where an unknown key is a 422.
//
// Both were verified. Neither covered the third way the field can arrive: a
// QUERY parameter. Nothing on the three lessons routes read the query string,
// so `?task_type=anything` was dropped on the floor and the request answered
// 200 — on the GET (which has no body at all, so the query is the ONLY way in)
// and equally on both POSTs.
//
// That is not a cosmetic gap. It is T-2's ORIGINAL DEFECT reproduced on a face
// T-2 did not reach: a caller that believes it named a classification is told
// nothing and handed an answer that looks like the one it asked for. The ticket
// exists to end silent-ignore, and the route family it was about was still
// doing it.
//
// These tests are the named assertions a mutant that removes
// refuseRetiredLessonsQuery has to turn red — one per face, so a partial revert
// cannot hide behind a sibling.
//
// 🔑 THE DISCRIMINATION TEST IS THE POINT OF THE LAST CASE. A gate that
// answered 400 to EVERY unrecognised query key would also pass every assertion
// above while making a station-wide posture change nobody asked for. The
// unknown-query-is-ignored behaviour is the router's, on every route here; this
// change narrows exactly ONE name. TestLessonsQueryRefusalIsScopedToTheRetiredName
// is what tells those two implementations apart, and a mutant that widens the
// gate to all unknown keys dies there rather than passing silently.

// lessonsREST issues an authenticated REST call and returns (status, body).
// Deliberately raw: the whole subject here is what the HTTP face answers, so
// nothing between the request and the status code may be smoothed over.
func lessonsREST(t *testing.T, srvURL, token, method, pathAndQuery, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srvURL+pathAndQuery, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, pathAndQuery, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(raw)
}

const lessonsQueryMarker = "T-2 follow-up: the doc no query parameter may quietly address"

// lessonsQueryFixture stands up the wired stack with a known lessons doc on
// `assistant`, and hands back the owner token. The owner is used on purpose:
// it clears lessonsWriteAuthz for any role, so a refusal observed here is the
// retired-field gate and cannot be an authz refusal wearing its coat.
func lessonsQueryFixture(t *testing.T) (string, *DAL, string) {
	t.Helper()
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if err := dal.PutLessons(Lessons{RoleKey: "assistant", Text: lessonsQueryMarker}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}
	return srv.URL, dal, ownerTok
}

// assertLessonsUntouched is the anti-vacuity guard shared by the write cases: a
// refusal that still writes is worse than an acceptance, because nothing
// reports it.
func assertLessonsUntouched(t *testing.T, dal *DAL, why string) {
	t.Helper()
	overlay, err := dal.GetLessons("assistant")
	if err != nil {
		t.Fatalf("GetLessons: %v", err)
	}
	if overlay == nil || overlay.Text != lessonsQueryMarker {
		t.Errorf("%s: the lessons doc changed under a REFUSED call: %+v", why, overlay)
	}
}

// TestLessonsGETRefusesTheRetiredTaskTypeQueryParameter is the assertion for
// the face that had NO door at all. A GET carries no body, so the query string
// is the only way `task_type` can arrive — and it arrived, and was ignored.
func TestLessonsGETRefusesTheRetiredTaskTypeQueryParameter(t *testing.T) {
	url, _, tok := lessonsQueryFixture(t)

	// POSITIVE CONTROL FIRST, and it is load-bearing: it proves the route
	// serves at all under this fixture and this token. Without it, a 400 from
	// a route that is simply broken end-to-end would read as "the door works".
	if status, body := lessonsREST(t, url, tok, "GET", "/api/lessons/assistant", ""); status != http.StatusOK {
		t.Fatalf("positive control: plain GET must serve, got %d: %s", status, body)
	} else if !strings.Contains(body, lessonsQueryMarker) {
		t.Fatalf("positive control: plain GET served something that is not the doc: %s", body)
	}

	for _, tc := range []struct{ name, query string }{
		{"value", "?task_type=general"},
		// PRESENCE, not blankness — identical to the MCP rule. An empty value
		// is still a caller that believes the axis exists, and telling it so
		// is the entire point.
		{"empty_value", "?task_type="},
		{"bare_key", "?task_type"},
		{"alongside_other_keys", "?zzz_bogus=1&task_type=review-pr-seth"},
		{"repeated", "?task_type=a&task_type=b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := lessonsREST(t, url, tok, "GET", "/api/lessons/assistant"+tc.query, "")
			if status == http.StatusOK {
				t.Fatalf("GET %s was ACCEPTED (200): %s\n"+
					"Silently ignoring the retired task_type is the one behaviour this route "+
					"must not have: the caller is left believing it addressed a classification "+
					"while being handed the only document there is. That is the exact failure "+
					"T-2 exists to end, and until this gate the GET face still did it.", tc.query, body)
			}
			if status != http.StatusBadRequest {
				t.Errorf("GET %s answered %d, want 400 — a retired parameter is the caller's "+
					"input being wrong, not a transport fault", tc.query, status)
			}
			if !strings.Contains(body, "task_type") {
				t.Errorf("the refusal does not name the parameter it refused: %s. A caller that "+
					"cannot see WHICH input was rejected has to guess, and guessing is how the "+
					"field gets sent again", body)
			}
			if !strings.Contains(body, "role_key") {
				t.Errorf("the refusal does not name the replacement (role_key): %s", body)
			}
		})
	}
}

// TestLessonsWriteRoutesRefuseTheRetiredTaskTypeQueryParameter covers the two
// POSTs. Their BODY door has been shut since T-2 (unknown key → 422); their
// QUERY was wide open, and the ticket's defect is a WRITE landing somewhere the
// caller did not name — so this is the face where silent-ignore costs the most.
func TestLessonsWriteRoutesRefuseTheRetiredTaskTypeQueryParameter(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"replace_lessons", "/api/lessons/assistant", `{"text":"poison"}`},
		{"patch_lessons", "/api/lessons/assistant/patch", `{"edits":[{"old":"","new":"poison"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, dal, tok := lessonsQueryFixture(t)

			// Positive control: the SAME body, without the query, writes. This
			// is what makes the refusal below attributable to the query
			// parameter and to nothing else.
			if status, body := lessonsREST(t, url, tok, "POST", tc.path, tc.body); status != http.StatusOK {
				t.Fatalf("positive control: %s without a query must write, got %d: %s", tc.path, status, body)
			}
			// Put the marker back so the untouched-check below is meaningful.
			if err := dal.PutLessons(Lessons{RoleKey: "assistant", Text: lessonsQueryMarker}); err != nil {
				t.Fatalf("PutLessons: %v", err)
			}

			status, body := lessonsREST(t, url, tok, "POST", tc.path+"?task_type=review-pr-seth", tc.body)
			if status == http.StatusOK {
				t.Fatalf("POST %s?task_type=… was ACCEPTED (200): %s\n"+
					"This is the ticket's original defect on a different face: a write whose "+
					"caller named a classification, landing on the one document there is, "+
					"reported as success.", tc.path, body)
			}
			if status != http.StatusBadRequest {
				t.Errorf("POST %s?task_type=… answered %d, want 400", tc.path, status)
			}
			if !strings.Contains(body, "task_type") {
				t.Errorf("the refusal does not name the parameter it refused: %s", body)
			}
			assertLessonsUntouched(t, dal, tc.name)
		})
	}
}

// TestLessonsQueryRefusalIsScopedToTheRetiredName is the DISCRIMINATION test,
// and it is the one that keeps this change honest.
//
// Ignoring undeclared query parameters is the ROUTER's behaviour, on every
// route this station serves. Turning that off for the lessons family would be a
// posture change nobody asked for, and it would pass every other assertion in
// this file. So: one unrecognised name that is NOT task_type must still be
// ignored exactly as before, on all three routes.
//
// A mutant that widens the gate from "the retired name" to "any unknown key"
// dies HERE, and nowhere else in this file.
func TestLessonsQueryRefusalIsScopedToTheRetiredName(t *testing.T) {
	url, _, tok := lessonsQueryFixture(t)

	if status, body := lessonsREST(t, url, tok, "GET", "/api/lessons/assistant?zzz_bogus=1", ""); status != http.StatusOK {
		t.Errorf("GET ?zzz_bogus=1 answered %d, want 200: %s\n"+
			"This gate is scoped to the ONE name T-2 retired. Refusing every unknown query "+
			"key is a station-wide posture change, not this ticket.", status, body)
	}
	if status, body := lessonsREST(t, url, tok, "POST", "/api/lessons/assistant?zzz_bogus=1", `{"text":"still writable"}`); status != http.StatusOK {
		t.Errorf("POST ?zzz_bogus=1 answered %d, want 200: %s", status, body)
	}
	if status, body := lessonsREST(t, url, tok, "POST", "/api/lessons/assistant/patch?zzz_bogus=1",
		`{"edits":[{"old":"","new":"\nappended"}]}`); status != http.StatusOK {
		t.Errorf("POST /patch ?zzz_bogus=1 answered %d, want 200: %s", status, body)
	}
}

// TestLessonsMCPGetStillServesOverTheQueryPath guards the collateral this
// change could plausibly have caused and that no other test in the tree would
// notice.
//
// 🔑 THE MECHANISM: splitToolArguments turns every non-path argument of a GET
// tool into a QUERY parameter before the loopback dispatch. get_lessons is a
// GET tool. So the new query gate sits directly on the path every agent's
// get_lessons travels, and a gate that were even slightly too wide would break
// the learning loop for every role at once — which is the failure mode T-d483
// already paid for once.
//
// The MCP face refuses task_type EARLIER (fillLessonsIdentityArgs), so the two
// doors are belt and braces rather than duplicates; this asserts the braces did
// not strangle the wearer.
func TestLessonsMCPGetStillServesOverTheQueryPath(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	if err := dal.PutMember(Member{
		ID: "joey", Kind: KindStaff, RoleKey: "assistant",
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	joeyTok, _ := mintJWT("joey", "agent", 300, secret, now, "")
	if err := dal.PutLessons(Lessons{RoleKey: "assistant", Text: lessonsQueryMarker}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}

	for _, tc := range []struct{ name, token, args string }{
		{"explicit_role_key", ownerTok, `{"role_key":"assistant"}`},
		// The no-argument agent round-trip: role_key is folded from identity,
		// leaving an EMPTY query — the case a too-eager gate would still pass,
		// which is why the explicit one above is here too.
		{"agent_no_arguments", joeyTok, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isErr, code, text := lessonsCall(t, srv.URL, tc.token,
				`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lessons","arguments":`+tc.args+`}}`)
			if isErr {
				t.Fatalf("MCP get_lessons was refused (code=%q): %s\n"+
					"The retired-parameter gate sits on the loopback query path every agent's "+
					"get_lessons uses. If it refuses a legitimate call, the learning loop is "+
					"broken for every role at once.", code, text)
			}
			if !strings.Contains(text, lessonsQueryMarker) {
				t.Errorf("MCP get_lessons served something that is not the doc: %s", text)
			}
		})
	}
}
