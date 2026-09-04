package main

// api_chat_peek_test.go — GET /api/chat and the read watermark.
//
// The file keeps its historical name (several other test files reference it for
// the helpers it defines) but its subject changed with T-48. It used to guard
// ?peek=true (T-cf91), the READ-ONLY conversation view that opted OUT of the
// auto read-receipt a plain ?with= list fired. Owner ruling, 2026-09-02:
// 「get_chat不應該可以標示已讀未讀，這應該要另一隻API明確表示有這個意圖」. So the
// receipt is gone, and with it the parameter that existed only to dodge it.
//
// What is guarded here now is the STRONGER statement: this route NEVER writes a
// read watermark, on ANY path — not "there is a way to ask it not to". That is
// what the removal is worth, and it is what a future caller re-adding a
// PutChatRead to the listing would break. TestMarkReadWritesWatermark is the
// positive control: without it, every zero below could equally mean the
// watermark plumbing is dead.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func chatGetRec(s *apiServer, sub string, params HandleListChatApiChatGetParams) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/chat", nil)
	claims := map[string]any{"sub": sub, "scope": scopeFor(sub)}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	s.HandleListChatApiChatGet(rec, req, params)
	return rec
}

// markReadRec drives the ONE route that is still allowed to write a watermark.
// It is the positive control for every "the watermark stayed at 0" assertion.
func markReadRec(s *apiServer, sub, peer string, ts float64) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"peer":%q,"last_read_ts":%v}`, peer, ts)
	req := httptest.NewRequest("POST", "/api/chat/mark-read", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	claims := map[string]any{"sub": sub, "scope": scopeFor(sub)}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	s.HandleMarkChatReadApiChatMarkReadPost(rec, req)
	return rec
}

func scopeFor(sub string) string {
	if sub == "owner" {
		return "owner"
	}
	return "agent"
}

// chatEnvelopeMessages pulls the `messages` array out of a GET /api/chat body.
//
// EVERY read door on that route answers the T-48 envelope
// (`{messages, next_cursor}`), so a test that decodes the body straight into an
// array is asserting against a shape the route does not serve — and would go
// green again the moment someone re-introduced the bare array. Going through
// here means the envelope is re-proved by every chat test in the package, not
// by one.
func chatEnvelopeMessages(t *testing.T, raw []byte) []byte {
	t.Helper()
	var env struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Messages == nil {
		t.Fatalf("GET /api/chat must answer the T-48 envelope {messages,...}: %v (%s)",
			err, raw)
	}
	return env.Messages
}

// chatEnvelopeNextCursor reads the continuation token, "" when absent.
func chatEnvelopeNextCursor(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, raw)
	}
	return env.NextCursor
}

func chatIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("chat GET → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(chatEnvelopeMessages(t, rec.Body.Bytes()), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func ownerWatermark(t *testing.T, s *apiServer, peer string) float64 {
	t.Helper()
	reads, err := s.dal.ListChatReads("owner", peer)
	if err != nil {
		t.Fatalf("reads: %v", err)
	}
	if len(reads) == 0 {
		return 0
	}
	return reads[0].LastReadTS
}

func seedTwoConversations(t *testing.T, s *apiServer) {
	t.Helper()
	// owner↔m-1 (the target thread) + owner↔m-2 (noise the ?with= filter drops).
	msgs := []ChatMessage{
		{ID: "a-1", Sender: "m-1", Recipient: "owner", TS: 1.0},
		{ID: "a-2", Sender: "owner", Recipient: "m-1", TS: 2.0},
		{ID: "a-3", Sender: "m-1", Recipient: "owner", TS: 3.0},
		{ID: "z-1", Sender: "m-2", Recipient: "owner", TS: 2.5}, // other convo
	}
	for _, m := range msgs {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
}

// POSITIVE CONTROL. The watermark is writable and ownerWatermark can see it —
// so a zero in the tests below is a real negative, not a broken helper or a
// table nothing ever writes to.
func TestMarkReadWritesWatermark(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, s)
	if wm := ownerWatermark(t, s, "m-1"); wm != 0 {
		t.Fatalf("precondition: watermark should start at 0, got %v", wm)
	}
	if rec := markReadRec(s, "owner", "m-1", 3.0); rec.Code != 200 {
		t.Fatalf("mark-read → %d: %s", rec.Code, rec.Body.String())
	}
	if wm := ownerWatermark(t, s, "m-1"); wm != 3.0 {
		t.Fatalf("POST /api/chat/mark-read must advance the watermark to 3.0, got %v", wm)
	}
}

// LOAD-BEARING NEGATIVE (T-48). No shape of a GET /api/chat request writes a
// watermark. MUTANT: put the old `if with != "" && len(msgs) > 0 { PutChatRead }`
// block back on the cursorless path and the first case goes red (3.0).
func TestChatListNeverAdvancesWatermark(t *testing.T) {
	with := "m-1"
	other := "m-2"
	big, none := 100, -1
	bts, bid := 999.0, "zzz"
	ids := []string{"a-1", "a-3"}

	cases := []struct {
		name   string
		params HandleListChatApiChatGetParams
	}{
		// The one that used to mark: a cursorless ?with= list. This is the case
		// the owner ruling is about.
		{"cursorless ?with=", HandleListChatApiChatGetParams{With: &with}},
		{"cursorless ?with= with a limit", HandleListChatApiChatGetParams{With: &with, Limit: &big}},
		{"uncapped ?with= (limit=-1)", HandleListChatApiChatGetParams{With: &with, Limit: &none}},
		{"one-sided sender filter", HandleListChatApiChatGetParams{With: &with, Sender: &with}},
		{"history page", HandleListChatApiChatGetParams{With: &with, Limit: &big, BeforeTs: &bts, BeforeId: &bid}},
		{"by ids", HandleListChatApiChatGetParams{Ids: &ids}},
		// No ?with= at all: the whole-stream listing must not mark anything
		// either, for any peer it happened to return.
		{"no peer", HandleListChatApiChatGetParams{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
			seedTwoConversations(t, s)
			if rec := chatGetRec(s, "owner", tc.params); rec.Code != 200 {
				t.Fatalf("GET /api/chat → %d: %s", rec.Code, rec.Body.String())
			}
			if wm := ownerWatermark(t, s, with); wm != 0 {
				t.Fatalf("%s must NOT advance the read watermark for %s: 0 -> %v",
					tc.name, with, wm)
			}
			if wm := ownerWatermark(t, s, other); wm != 0 {
				t.Fatalf("%s must NOT advance the read watermark for %s: 0 -> %v",
					tc.name, other, wm)
			}
		})
	}
}

// A watermark ALREADY set is not touched either — neither advanced nor reset by
// a later listing. (The old receipt was a monotonic upsert, so "it did not move
// backwards" was never the interesting half; "it did not move forwards" is.)
func TestChatListLeavesAnExistingWatermarkAlone(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, s)
	if rec := markReadRec(s, "owner", "m-1", 1.0); rec.Code != 200 {
		t.Fatalf("mark-read → %d: %s", rec.Code, rec.Body.String())
	}
	with := "m-1"
	rec := chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	// …and the listing still serves the right thread, in order.
	got := chatIDs(t, rec)
	want := []string{"a-1", "a-2", "a-3"}
	if len(got) != len(want) {
		t.Fatalf("thread: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("thread order: want %v, got %v", want, got)
		}
	}
	if wm := ownerWatermark(t, s, "m-1"); wm != 1.0 {
		t.Fatalf("listing a-2/a-3 (ts 2.0/3.0) must leave the watermark at 1.0, got %v", wm)
	}
}
