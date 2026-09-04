package main

// api_chat_by_ids_ta828_test.go — `get_chat?ids=` (T-a828): reading back the
// messages the wake snapshot FOLDED.
//
// The defect this seam repairs is a promise, not a crash: every collapsed
// message in a wake snapshot carries `body_omitted_chars` > 0 and the words
// "re-read it with get_chat", while get_chat took only a peer plus a paging
// cursor — nothing that could NAME one message. So the four things below are
// each pinned by their own test, and each has a mutant that reddens THAT test:
//
//	① a caller's own ids come back IN FULL          — the positive control, so
//	                                                  ② cannot pass vacuously
//	② an id between two other members is SERVED     — and it is served exactly
//	                                                  as far as the ordinary
//	                                                  listing already serves it
//	                                                  (T-4e95 aligned the two)
//	③ more ids than the cap is REFUSED              — and the refusal states
//	                                                  the cap
//	④ an id no message carries refuses the WHOLE    — the documented behaviour,
//	   call                                           pinned against the doc

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// seedFoldableThreads lays down two conversations that do NOT overlap: the
// caller's own line, and one strictly between two other members. The second is
// the whole point — without a message the caller was never part of, the
// participation boundary has nothing to be tested against.
func seedFoldableThreads(t *testing.T, s *apiServer) {
	t.Helper()
	msgs := []ChatMessage{
		{ID: "c-mine-1", Sender: "m-1", Recipient: "owner", Body: "first half of the plan", TS: 1.0},
		{ID: "c-mine-2", Sender: "owner", Recipient: "m-1", Body: "second half of the plan", TS: 2.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "att-1", "mime": "image/png", "filename": "diagram.png"},
			}}},
		// Two OTHER members talking to each other. The caller ("owner") is
		// neither end of it.
		{ID: "c-theirs", Sender: "m-1", Recipient: "m-2", Body: "not for the owner", TS: 3.0},
	}
	for _, m := range msgs {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
}

// chatByIDs drives the handler with a by-id request.
func chatByIDs(s *apiServer, sub string, ids ...string) *httptest.ResponseRecorder {
	return chatGetRec(s, sub, HandleListChatApiChatGetParams{Ids: &ids})
}

// ① POSITIVE CONTROL. It is listed first on purpose: every other test here
// asserts a REFUSAL, and a seam that refuses everything would pass all three of
// them. This is the one that says the door opens.
//
// It checks the two things a re-read is FOR — the whole body and the attachment
// refs — because a by-id read that returned the same collapsed lead the
// snapshot already showed would satisfy "200 with the right ids" and still be
// useless.
func TestChatByIDs_YourOwnMessagesComeBackWhole(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)

	rec := chatByIDs(s, "owner", "c-mine-2", "c-mine-1")
	if rec.Code != 200 {
		t.Fatalf("own ids must be readable: %d %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID          string `json:"id"`
		Body        string `json:"body"`
		Attachments []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(chatEnvelopeMessages(t, rec.Body.Bytes()), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("both named messages must come back, got %d: %s", len(rows), rec.Body.String())
	}
	// Oldest→newest, like the rest of this surface — NOT the order they were
	// named in.
	if rows[0].ID != "c-mine-1" || rows[1].ID != "c-mine-2" {
		t.Fatalf("by-id read must be oldest→newest, got %s then %s", rows[0].ID, rows[1].ID)
	}
	if rows[0].Body != "first half of the plan" || rows[1].Body != "second half of the plan" {
		t.Fatalf("bodies must come back WHOLE — that is what the fold marker promises: %s",
			rec.Body.String())
	}
	if len(rows[1].Attachments) != 1 || rows[1].Attachments[0].ID != "att-1" {
		t.Fatalf("attachment refs must ride along, got %+v", rows[1].Attachments)
	}
	if rows[1].Attachments[0].URL != "/api/chat/attachment/att-1" {
		t.Fatalf("attachment ref must carry its serve URL, got %q", rows[1].Attachments[0].URL)
	}
}

// ① (cont.) A by-id read is a RE-read, so it must not consume unread state.
// Kept beside the positive control rather than in the peek file because the
// reasoning is this seam's own: the snapshot already showed the caller this
// message, shortened; unfolding it says nothing about the rest of the thread.
func TestChatByIDs_NeverAdvancesTheReadWatermark(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)
	if rec := chatByIDs(s, "owner", "c-mine-2"); rec.Code != 200 {
		t.Fatalf("precondition: %d %s", rec.Code, rec.Body.String())
	}
	if wm := ownerWatermark(t, s, "m-1"); wm != 0 {
		t.Fatalf("a by-id re-read must not advance the watermark, got %v", wm)
	}
}

// ② TWO DOORS, ONE RULE (T-4e95, owner ruling). This cell used to pin a 403 on
// an id between two other members. That refusal is gone, and what replaced it is
// not "no rule" — it is the rule the ORDINARY LISTING already enforced, now
// enforced identically by both doors.
//
// Why the old assertion had to move rather than be deleted: the 403 never
// withheld anything. `?with=` filters on a PARTICIPANT, not on the caller, so
// the very same row was already served to the very same caller through the
// listing. Two doors onto one row disagreeing about who may open them is a
// discrepancy, and the only party it actually cost was an honest caller trying
// to follow a message's reply_to.
//
// So this test asserts the AGREEMENT, in both directions:
//   - the by-ids door serves a bystander the row (re-adding the 403 reddens it);
//   - the listing door serves the same bystander the same row (narrowing the
//     listing instead of this door also reddens it).
//
// A future ticket may decide this reach is wrong. If so it is wrong for BOTH
// doors, and this test is what makes fixing only one of them impossible.
func TestChatByIDs_ReachesExactlyAsFarAsTheOrdinaryListing(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)

	// m-3 is a bystander: neither end of c-theirs (m-1 ↔ m-2).
	rec := chatByIDs(s, "m-3", "c-theirs")
	if rec.Code != 200 {
		t.Fatalf("a by-ids read must reach as far as the listing does, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not for the owner") {
		t.Fatalf("the named message must come back whole: %s", rec.Body.String())
	}

	// The other half of the agreement, measured rather than assumed: the SAME
	// bystander asking the ORDINARY listing for that conversation gets the same
	// row. Without this half, someone could satisfy the test above by opening
	// this door while quietly closing the other one.
	peer := "m-1"
	listed := chatGetRec(s, "m-3", HandleListChatApiChatGetParams{With: &peer})
	if listed.Code != 200 {
		t.Fatalf("precondition: the listing must answer, got %d: %s",
			listed.Code, listed.Body.String())
	}
	if !strings.Contains(listed.Body.String(), "not for the owner") {
		t.Fatalf("the listing is what sets the reach — if it no longer serves "+
			"this row, the two doors are being narrowed apart: %s",
			listed.Body.String())
	}

	// Mixed call: naming a bystander's id alongside your own is now an ordinary
	// read, not a probe — both come back.
	mixed := chatByIDs(s, "owner", "c-mine-1", "c-theirs")
	if mixed.Code != 200 {
		t.Fatalf("a mixed call must answer, got %d: %s", mixed.Code, mixed.Body.String())
	}
	if !strings.Contains(mixed.Body.String(), "first half of the plan") ||
		!strings.Contains(mixed.Body.String(), "not for the owner") {
		t.Fatalf("both named ids must be answered: %s", mixed.Body.String())
	}
}

// ③ THE HARD CAP, and the refusal has to state it. A limit a caller can only
// discover by bisecting is a limit that gets bisected — by every agent, forever.
func TestChatByIDs_OverTheCapIsRefusedAndTheRefusalStatesIt(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)

	tooMany := make([]string, chatByIDsMax+1)
	for i := range tooMany {
		tooMany[i] = "c-ask-" + strconv.Itoa(i)
	}
	rec := chatByIDs(s, "owner", tooMany...)
	if rec.Code != 400 {
		t.Fatalf("%d ids must be refused (cap is %d), got %d: %s",
			len(tooMany), chatByIDsMax, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), strconv.Itoa(chatByIDsMax)) {
		t.Fatalf("the refusal must state the cap (%d): %s", chatByIDsMax, rec.Body.String())
	}
	// The cap counts DISTINCT ids: a caller that repeats one id is not asking
	// for a bigger response, and refusing it would be a limit on typing rather
	// than on payload. Exactly at the cap still answers — the boundary is
	// inclusive, and a cap that refused its own stated number would make the
	// message above false.
	atCap := make([]string, 0, chatByIDsMax)
	for i := 0; i < chatByIDsMax; i++ {
		atCap = append(atCap, "c-mine-1")
	}
	if rec := chatByIDs(s, "owner", atCap...); rec.Code != 200 {
		t.Fatalf("duplicates collapse, so %d repeats of one id is one id: %d %s",
			chatByIDsMax, rec.Code, rec.Body.String())
	}
}

// ④ AN UNKNOWN ID REFUSES THE WHOLE CALL. This is the documented choice
// (spec/openapi.json's ids description: "ALL OR NOTHING ON AN UNKNOWN ID"), and
// this test is what stops the documentation and the behaviour from drifting
// apart — including the second half, which is the one that would silently rot:
// the readable ids named in the SAME call must not come back.
func TestChatByIDs_AnUnknownIDRefusesTheWholeCall(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)

	rec := chatByIDs(s, "owner", "c-mine-1", "c-nosuch")
	if rec.Code != 404 {
		t.Fatalf("an unknown id must refuse the whole call, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "c-nosuch") {
		t.Fatalf("the refusal must name the id that was not found: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "first half of the plan") {
		t.Fatalf("all-or-nothing: the readable id in the same call must NOT be "+
			"answered, or a short array becomes indistinguishable from the fold "+
			"this seam exists to undo: %s", rec.Body.String())
	}
}

// ④ (cont.) The documented behaviour and the shipped behaviour are checked
// against the SAME sentence: the words the agent reads in tools/list are the
// words this test requires the spec to carry. Without this, "unknown ids are
// rejected" could remain true in code while the schema said something else.
func TestChatByIDs_TheDocumentedUnknownIDRuleIsTheShippedOne(t *testing.T) {
	desc := idsPropertyDescription(t)
	for _, promise := range []string{
		"ALL OR NOTHING ON AN UNKNOWN ID",
		"refuses the WHOLE call with 404",
		"AT MOST 20 DISTINCT IDS",
		// T-4e95: the sentence that replaced "REFUSED with 403". The doc must
		// state the reach the handler actually has — a schema still promising a
		// permission refusal the code no longer performs is the same false
		// promise this file exists to prevent, pointing the other way.
		"SAME REACH AS THE ORDINARY LISTING",
	} {
		if !strings.Contains(desc, promise) {
			t.Fatalf("the ids schema must document %q — the behaviour is pinned by "+
				"the tests above, and a schema that says something else is the "+
				"same false promise this ticket exists to remove", promise)
		}
	}
	// The cap in the prose is the cap in the code. Two numbers is one number
	// too many.
	if !strings.Contains(desc, "AT MOST "+strconv.Itoa(chatByIDsMax)+" DISTINCT IDS") {
		t.Fatalf("the documented cap disagrees with chatByIDsMax=%d", chatByIDsMax)
	}
}

// The same sentence is carried by TWO surfaces — the OpenAPI parameter (which
// the generators put in front of Go and TypeScript callers) and the MCP
// inputSchema (which is what tools/list shows an agent). They are kept BYTE
// IDENTICAL, so this is a mirror rather than a second copy that can drift: the
// failure this ticket is about is a promise that stopped being true in one place
// while other copies of it kept being read.
func TestChatByIDs_SchemaAndToolCatalogCarryTheSameSentence(t *testing.T) {
	if fromCatalog, fromSpec := idsPropertyDescription(t), idsParameterDescription(t); fromCatalog != fromSpec {
		t.Fatalf("the ids description has drifted between spec/openapi.json's "+
			"parameter and the MCP inputSchema agents read:\n  openapi: %.120s…\n  catalog: %.120s…",
			fromSpec, fromCatalog)
	}
}

// Blank ids are NOT a by-id read: `?ids=` on its own must leave the ordinary
// listing path byte-for-byte alone. Guards the branch, not the feature — a
// by-id branch that swallowed every request would break every existing caller.
func TestChatByIDs_BlankIDsFallThroughToTheOrdinaryList(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedFoldableThreads(t, s)
	with := "m-1"
	blank := []string{"", "   "}
	// The control is the SAME request without the parameter, so this cannot
	// pass by agreeing with a hand-written expectation that has itself drifted.
	want := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with}))
	if len(want) == 0 {
		t.Fatalf("precondition: the ordinary listing path must return something")
	}
	got := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with, Ids: &blank}))
	if len(got) != len(want) {
		t.Fatalf("blank ids must behave as if the parameter was absent: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blank ids must behave as if the parameter was absent: want %v, got %v", want, got)
		}
	}
}

// idsPropertyDescription reads the ids description out of the FROZEN catalog —
// the file tools/list is actually served from (assets.go, embed-only), not the
// generator's input. Reading the input would make this test agree with itself.
func idsPropertyDescription(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name != "get_chat" {
			continue
		}
		desc := tool.InputSchema.Properties["ids"].Description
		if desc == "" {
			t.Fatalf("get_chat advertises no ids parameter — an agent cannot use a " +
				"lever tools/list does not show it")
		}
		return desc
	}
	t.Fatalf("get_chat is missing from spec/mcp-catalog.json")
	return ""
}

// idsParameterDescription reads the same sentence from the frozen OpenAPI
// contract.
func idsParameterDescription(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	for _, p := range spec.Paths["/api/chat"]["get"].Parameters {
		if p.Name == "ids" {
			return p.Description
		}
	}
	t.Fatalf("GET /api/chat declares no ids parameter")
	return ""
}

// ── the wiring half ─────────────────────────────────────────────────────────
//
// Every test above hands the handler a params struct BY HAND, so all of them
// stay green if `?ids=` never reaches it — the repeated query parameter is
// bound by the GENERATED wrapper (ocapi_gen.go), and a lever the wrapper does
// not bind is a lever no caller has. This drives real HTTP through the real
// mux instead, which is also the only place the route's own floor is
// exercised: an ordinary agent has to get through it.
func TestChatByIDs_RepeatedQueryParameterReachesTheHandler(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	seedFoldableThreads(t, &apiServer{dal: dal, hub: NewHub()})

	ask := func(sub, query string) (int, string) {
		t.Helper()
		tok, err := mintJWT(sub, "agent", 3600, secret, time.Now().Unix(), "")
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		req, err := http.NewRequest("GET", srv.URL+"/api/chat?"+query, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return resp.StatusCode, string(body)
	}

	code, body := ask("m-1", "ids=c-mine-1&ids=c-mine-2")
	if code != 200 {
		t.Fatalf("a repeated ?ids= must bind and answer: %d %s", code, body)
	}
	if !strings.Contains(body, "c-mine-1") || !strings.Contains(body, "c-mine-2") {
		t.Fatalf("both repetitions must reach the handler, got %s", body)
	}
	// …and it really is the by-id path, not the ordinary listing answering with
	// everything this member can see: c-theirs involves m-1 too, so a DROPPED
	// ?ids= would put it in the answer.
	if strings.Contains(body, "c-theirs") {
		t.Fatalf("?ids= was ignored — this is the ordinary stream, not a by-id read: %s", body)
	}
	// The T-4e95 reach holds over real HTTP too, not just through the handler:
	// m-1 is one end of c-theirs, so the bystander has to be a third member.
	if code, body := ask("m-3", "ids=c-theirs"); code != 200 {
		t.Fatalf("a bystander must be served over the wire too: %d %s", code, body)
	}
}
