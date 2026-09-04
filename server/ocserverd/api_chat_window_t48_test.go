package main

// api_chat_window_t48_test.go — GET /api/chat?start_id= / ?end_id= (T-48): the
// window anchored on a MESSAGE ID, both ends inclusive, walkable in BOTH
// directions.
//
// before_ts/before_id can only walk backwards, so a caller told to jump to one
// specific message could reach it and then not load what came AFTER it. The
// window is the repair; the guardrails are what stop the repair from quietly
// changing the route everyone else already uses.
//
// Each guardrail below has its own test, and each was verified to go red under
// a mutant that breaks THAT rule specifically — the mutants are named in the
// test's own note. The first one is the one that matters most: a request that
// sends NEITHER anchor must behave exactly as it does today, and it is pinned
// against the legacy limit semantics (negative = uncapped, 0 = empty) that the
// window path deliberately does not share.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatQueryRec drives GET /api/chat through the GENERATED wrapper with a real
// query string, so the legacy parameters are bound exactly as they are in
// production and the window anchors are read off the same URL the wrapper
// parses. The params-struct helper (chatGetRec) cannot express these two
// parameters until the spec branch lands them in ocapi_gen.go.
func chatQueryRec(s *apiServer, sub, rawQuery string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/chat?"+rawQuery, nil)
	claims := map[string]any{"sub": sub, "scope": scopeFor(sub)}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	wrapper := &ServerInterfaceWrapper{
		Handler: s,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		},
	}
	rec := httptest.NewRecorder()
	wrapper.HandleListChatApiChatGet(rec, req)
	return rec
}

// seedWindowStream lays down one owner↔m-1 line of six messages at ts 1..6,
// plus one message on a line the owner is not part of, so the participant
// filter has something to exclude.
func seedWindowStream(t *testing.T, s *apiServer) {
	t.Helper()
	msgs := []ChatMessage{
		{ID: "c-1", Sender: "m-1", Recipient: "owner", Body: "one", TS: 1},
		{ID: "c-2", Sender: "owner", Recipient: "m-1", Body: "two", TS: 2},
		{ID: "c-3", Sender: "m-1", Recipient: "owner", Body: "three", TS: 3},
		{ID: "c-4", Sender: "owner", Recipient: "m-1", Body: "four", TS: 4},
		{ID: "c-5", Sender: "m-1", Recipient: "owner", Body: "five", TS: 5},
		{ID: "c-6", Sender: "owner", Recipient: "m-1", Body: "six", TS: 6},
		{ID: "c-other", Sender: "m-2", Recipient: "m-3", Body: "not their line", TS: 7},
	}
	for _, m := range msgs {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
}

func windowTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedWindowStream(t, s)
	return s
}

func wantIDs(t *testing.T, rec *httptest.ResponseRecorder, want ...string) {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(chatEnvelopeMessages(t, rec.Body.Bytes()), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.ID
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ── ① NEITHER ANCHOR ⇒ TODAY'S BEHAVIOUR, BYTE FOR BYTE ─────────────────────
//
// The single most important guarantee in T-48, and the one a window
// implementation is most likely to break by accident: every rule the window
// path adds (a 1..200 limit, a 404 on an unknown id, a 422 on a bad pair) must
// be invisible to a request that sends neither anchor.
//
// It is pinned on the two legacy limit values that the window path REFUSES —
// limit=-1 (uncapped) and limit=0 (empty) — because those are exactly where a
// cap applied one level too high would show up, and limit=-1 is a spec-verbatim
// promise with committed callers.
//
// MUTANT (verified red): in HandleListChatApiChatGet, change the window guard
// from `hasStart || hasEnd` to `true`. Redden: the limit=-1 and limit=0 rows
// fail on the `status = 422, want 200` line in wantIDs.
func TestChatListWithoutAnchorsKeepsTodaysBehaviour(t *testing.T) {
	s := windowTestServer(t)
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"negative limit is still uncapped", "with=m-1&limit=-1",
			[]string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}},
		{"zero limit is still an empty page", "with=m-1&limit=0", nil},
		{"limit far above the window cap is still honoured", "with=m-1&limit=1000",
			[]string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}},
		{"default limit still caps to the newest", "with=m-1",
			[]string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}},
		{"explicit limit still cuts from the oldest end", "with=m-1&limit=2",
			[]string{"c-5", "c-6"}},
		{"the deprecated keyset cursor still pages back", "with=m-1&before_ts=4&before_id=c-4&limit=2",
			[]string{"c-2", "c-3"}},
		{"an unfiltered listing still spans every line", "limit=-1",
			[]string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6", "c-other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantIDs(t, chatQueryRec(s, "owner", tc.query), tc.want...)
		})
	}
}

// ── ② THE WINDOW ITSELF ─────────────────────────────────────────────────────
//
// The positive control. Listed before the refusals for the same reason the
// by-ids suite does it: an implementation that refused every windowed request
// would satisfy every guardrail test below and serve nobody.
//
// MUTANT (verified red): in listChatWindow, make the start clause exclusive
// (`id > ?` instead of `id >= ?`). Redden: the start_id rows fail on the
// `ids = ...` line in wantIDs — c-3 disappears from the head of the window.
func TestChatWindowWalksBothDirectionsInclusively(t *testing.T) {
	s := windowTestServer(t)
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"start_id walks towards the newest, inclusive", "with=m-1&start_id=c-3&limit=3",
			[]string{"c-3", "c-4", "c-5"}},
		{"end_id walks towards the oldest, inclusive", "with=m-1&end_id=c-4&limit=3",
			[]string{"c-2", "c-3", "c-4"}},
		{"a pair bounds one window", "with=m-1&start_id=c-2&end_id=c-4&limit=30",
			[]string{"c-2", "c-3", "c-4"}},
		{"the same id at both ends is a window of one", "with=m-1&start_id=c-3&end_id=c-3&limit=30",
			[]string{"c-3"}},
		// Spec's ONLY sentence on the both-anchors case: "a window wider than 200
		// rows is truncated from the `start_id` end" ⇒ the END anchor survives and
		// the OLDER rows are the ones dropped. Reading the start_id paragraph as a
		// rule for this case is the mistake this row exists to prevent.
		{"a window wider than limit is truncated at the start_id end", "with=m-1&start_id=c-2&end_id=c-6&limit=2",
			[]string{"c-5", "c-6"}},
		{"a window running off the newest end is short, not an error", "with=m-1&start_id=c-5&limit=30",
			[]string{"c-5", "c-6"}},
		{"the participant filter still applies inside the window", "with=m-1&start_id=c-1&limit=30",
			[]string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantIDs(t, chatQueryRec(s, "owner", tc.query), tc.want...)
		})
	}
}

// ── ③ A CONTRADICTORY PAIR IS 422, NOT AN EMPTY ARRAY ───────────────────────
//
// The empty array is what a REAL but empty window answers, so answering a
// contradiction the same way would make the two indistinguishable — the caller
// could not tell "I sent the anchors backwards" from "there is nothing there".
//
// MUTANT (verified red): delete the `start.newerThan(*end)` refusal in
// serveChatWindow. Redden: `status = 200, want 422` — the call falls through
// to listChatWindow, which answers 200 with an empty array.
func TestChatWindowRefusesAContradictoryPair(t *testing.T) {
	s := windowTestServer(t)
	rec := chatQueryRec(s, "owner", "with=m-1&start_id=c-5&end_id=c-2&limit=30")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "[]" {
		t.Fatalf("a contradiction must not be answered as an empty window")
	}

	// The control: the same pair the RIGHT way round is a real window, so the
	// 422 above is the contradiction being caught and not the pair path being
	// broken outright.
	wantIDs(t, chatQueryRec(s, "owner", "with=m-1&start_id=c-2&end_id=c-5&limit=30"),
		"c-2", "c-3", "c-4", "c-5")
}

// ── ④ AN ANCHOR NAMING NO MESSAGE IS 404 ────────────────────────────────────
//
// Same reasoning as the by-ids refusal: an empty page is what a real window at
// the edge of the stream returns.
//
// MUTANT (verified red): in serveChatWindow, replace the two `writeError(...
// StatusNotFound ...)` refusals with `start = nil` / `end = nil`. Redden:
// `status = 200, want 404` on both subtests.
func TestChatWindowRefusesAnAnchorNoMessageCarries(t *testing.T) {
	s := windowTestServer(t)
	for _, tc := range []struct{ name, query, named string }{
		{"unknown start_id", "with=m-1&start_id=c-nope&limit=30", "c-nope"},
		{"unknown end_id", "with=m-1&end_id=c-nope&limit=30", "c-nope"},
		{"unknown id in a pair", "with=m-1&start_id=c-2&end_id=c-nope&limit=30", "c-nope"},
		{"a blank anchor is SENT, not absent", "with=m-1&start_id=&limit=30", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := chatQueryRec(s, "owner", tc.query)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if tc.named != "" && !strings.Contains(rec.Body.String(), tc.named) {
				t.Fatalf("refusal must name the id it could not find: %s", rec.Body.String())
			}
		})
	}
}

// ── ⑤ THE TWO CURSOR FAMILIES CANNOT BE MIXED ───────────────────────────────
//
// They disagree about direction. Honouring one and dropping the other silently
// hands the caller the wrong end of the stream and never says so.
//
// MUTANT (verified red): delete the `req.beforeSent` refusal in
// serveChatWindow. Redden: `status = 200, want 422` on every subtest — the
// before_* cursor is silently ignored and the window is served.
func TestChatWindowRefusesTheDeprecatedCursorAlongsideIt(t *testing.T) {
	s := windowTestServer(t)
	for _, q := range []string{
		"with=m-1&start_id=c-2&before_ts=4&before_id=c-4&limit=30",
		"with=m-1&end_id=c-4&before_ts=4&before_id=c-4&limit=30",
		// One half of the pair is enough: the mistake is naming the old family
		// at all, and the "before_ts and before_id must be supplied together"
		// 422 must not be what answers it — that would send the caller off to
		// add the missing half of a cursor it should not be sending.
		"with=m-1&start_id=c-2&before_ts=4&limit=30",
		"with=m-1&start_id=c-2&before_id=c-4&limit=30",
	} {
		t.Run(q, func(t *testing.T) {
			rec := chatQueryRec(s, "owner", q)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "direction") {
				t.Fatalf("refusal must say WHY the families cannot mix: %s", rec.Body.String())
			}
		})
	}
}

// ── ⑥ limit IS BOUNDED TO 1..200 ON THIS PATH ONLY ──────────────────────────
//
// The bound and its EXEMPTION are one rule, so they are pinned together: the
// same limit values that are refused here are asserted unchanged on the legacy
// path by TestChatListWithoutAnchorsKeepsTodaysBehaviour.
//
// ⚠️ 200 BOUNDS ROWS, NOT BYTES. 200 rows has been measured at 687 KB and
// nothing on this route bounds the payload. There is no test below for a byte
// ceiling because there is no byte ceiling to test.
//
// MUTANT (verified red): change the guard to `req.limit > chatWindowMaxLimit`
// (dropping the lower bound). Redden: the limit=0 and limit=-1 subtests fail on
// `status = 200, want 422`.
func TestChatWindowBoundsTheLimitOnItsOwnPath(t *testing.T) {
	s := windowTestServer(t)
	for _, q := range []string{
		"with=m-1&start_id=c-1&limit=201",
		"with=m-1&start_id=c-1&limit=0",
		"with=m-1&start_id=c-1&limit=-1",
		"with=m-1&end_id=c-6&limit=201",
		"with=m-1&end_id=c-6&limit=-1",
	} {
		t.Run(q, func(t *testing.T) {
			rec := chatQueryRec(s, "owner", q)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
			}
		})
	}
	// REFUSAL ORDER: a request that is wrong about BOTH the limit and the anchor
	// is answered about the limit. Reporting the 404 first would send the caller
	// hunting for a message that is there, in a request that was never going to
	// be served.
	t.Run("the limit bound is answered before the anchor lookup", func(t *testing.T) {
		rec := chatQueryRec(s, "owner", "with=m-1&start_id=c-nope&limit=-1")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("the bounds themselves are accepted", func(t *testing.T) {
		wantIDs(t, chatQueryRec(s, "owner", "with=m-1&start_id=c-1&limit=1"), "c-1")
		wantIDs(t, chatQueryRec(s, "owner", "with=m-1&start_id=c-1&limit=200"),
			"c-1", "c-2", "c-3", "c-4", "c-5", "c-6")
	})
}

// ── ⑦ THE WINDOW WRITES NO READ WATERMARK ───────────────────────────────────
//
// The route lost that side effect earlier in T-48; a new path onto the same
// route is exactly where it could come back.
//
// MUTANT (verified red): add `_ = s.dal.PutChatRead(...)` at the end of
// serveChatWindow. Redden: `watermark = 6, want 0`.
func TestChatWindowNeverAdvancesTheReadWatermark(t *testing.T) {
	s := windowTestServer(t)
	wantIDs(t, chatQueryRec(s, "owner", "with=m-1&start_id=c-1&limit=200"),
		"c-1", "c-2", "c-3", "c-4", "c-5", "c-6")
	if got := ownerWatermark(t, s, "m-1"); got != 0 {
		t.Fatalf("watermark = %v, want 0 — this route marks nothing read", got)
	}
}

// ── ⑧ ?ids= STILL ANSWERS ON ITS OWN ────────────────────────────────────────
//
// The spec says ids is answered without consulting start_id/end_id. That is a
// claim about ORDER inside the handler, so it is worth one test: a windowed
// request that ALSO names ids must come back as the by-ids read, not as a
// window and not as a 422 about the window's parameters.
//
// MUTANT (verified red): move the window branch above the `ids` branch in
// HandleListChatApiChatGet. Redden: `status = 422, want 200` — the by-ids call
// carries no limit, so it is refused by the window's limit bound.
func TestChatByIDsIsNotConsultedByTheWindow(t *testing.T) {
	s := windowTestServer(t)
	wantIDs(t, chatQueryRec(s, "owner", "ids=c-5&ids=c-2&start_id=c-nope&limit=-1"),
		"c-2", "c-5")
}
