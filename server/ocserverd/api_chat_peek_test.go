package main

// api_chat_peek_test.go — GET /api/chat?peek=true (T-cf91): the READ-ONLY
// conversation view. It must serve the SAME filtered+capped window as a plain
// ?with= list, but WITHOUT advancing the caller's read watermark — so a
// backgrounded window can refresh a thread without consuming its unread state.
// The old client did this by pulling the whole company stream (limit=-1) and
// filtering in the browser; ?peek=true lets the server filter+cap instead.

import (
	"context"
	"encoding/json"
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

func scopeFor(sub string) string {
	if sub == "owner" {
		return "owner"
	}
	return "agent"
}

func chatIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("chat GET → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
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

func TestChatWithoutPeekAdvancesWatermark(t *testing.T) {
	// POSITIVE CONTROL: a plain ?with= list DOES advance the watermark — proof
	// the peek test below is exercising a real difference, not a dead endpoint.
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, s)
	if wm := ownerWatermark(t, s, "m-1"); wm != 0 {
		t.Fatalf("precondition: watermark should start at 0, got %v", wm)
	}
	with := "m-1"
	chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	// newest returned m-1 message is a-3 @ ts=3.0.
	if wm := ownerWatermark(t, s, "m-1"); wm != 3.0 {
		t.Fatalf("plain ?with= must advance watermark to 3.0, got %v", wm)
	}
}

func TestChatPeekDoesNotAdvanceWatermark(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, s)
	with := "m-1"
	peek := "true"
	rec := chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with, Peek: &peek})
	// LOAD-BEARING negative. MUTANT: drop the `&& !peek` from the watermark
	// guard and this goes red (peek would advance it to 3.0).
	if wm := ownerWatermark(t, s, "m-1"); wm != 0 {
		t.Fatalf("peek=true must NOT advance the watermark, got %v", wm)
	}
	// …and it still returns the right thread (the ?with= filter + order held).
	got := chatIDs(t, rec)
	want := []string{"a-1", "a-2", "a-3"}
	if len(got) != len(want) {
		t.Fatalf("peek thread: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("peek thread order: want %v, got %v", want, got)
		}
	}
}

func TestChatPeekMatchesPlainListWindow(t *testing.T) {
	// CONTENT PARITY: peek returns the SAME messages as a plain ?with= list of
	// the same window — the only difference is the (absent) watermark side
	// effect. Two servers so the plain list's watermark write can't perturb peek.
	sPlain := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, sPlain)
	sPeek := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedTwoConversations(t, sPeek)

	with := "m-1"
	peek := "true"
	plain := chatIDs(t, chatGetRec(sPlain, "owner", HandleListChatApiChatGetParams{With: &with}))
	peeked := chatIDs(t, chatGetRec(sPeek, "owner", HandleListChatApiChatGetParams{With: &with, Peek: &peek}))
	if len(plain) != len(peeked) {
		t.Fatalf("peek/plain window differ: plain %v, peek %v", plain, peeked)
	}
	for i := range plain {
		if plain[i] != peeked[i] {
			t.Fatalf("peek/plain content differ at %d: plain %v, peek %v", i, plain, peeked)
		}
	}
}
