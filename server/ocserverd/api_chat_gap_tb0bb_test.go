package main

// api_chat_gap_tb0bb_test.go — the SERVER-SIDE facts the frontend's seam fix
// (T-b0bb, frontend/src/hooks/useChat.ts) is built on. Read-only: it drives the
// real HandleListChatApiChatGet through httptest against a temp DAL, using the
// helpers api_chat_peek_test.go already defines. It starts no server.
//
// Three of these are GUARDS for the fix; one is a CHARACTERIZATION of behaviour
// the fix works AROUND and cannot change from the client. They are labelled
// individually, because a reader who mistakes the fourth for a guard would
// think the read-watermark problem was solved. It is not.

import (
	"fmt"
	"testing"
)

func seedChatN(t *testing.T, s *apiServer, peer, prefix string, n int, tsFrom float64) {
	t.Helper()
	for i := 0; i < n; i++ {
		m := ChatMessage{
			ID:        fmt.Sprintf("%s%02d", prefix, i+1),
			Sender:    peer,
			Recipient: "owner",
			TS:        tsFrom + float64(i),
		}
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
}

// GUARD 1. THE PROPERTY THE BACKFILL RIDES ON.
//
// The client closes a forward seam by paging BACKWARDS with before_ts+before_id
// — it has no choice, there is no forward cursor. That is only safe because a
// cursor page returns from a branch that runs BEFORE the watermark write.
//
// 🔴 THE SCENARIO THAT MAKES THIS LOAD-BEARING, and the reason the obvious
// version of this test is WORTHLESS. The obvious version lists cursorlessly
// first (watermark → newest) and then asks for an OLDER page and checks the
// watermark did not move. It cannot fail: PutChatRead is MONOTONIC, so an older
// page's lower ts is a no-op regardless of whether the branch writes. Verified:
// a mutant that adds a full PutChatRead to the cursor branch left that version
// GREEN.
//
// The case with teeth is the BACKGROUNDED WINDOW. When the tab is not focused
// the client loads through ?peek=true precisely so the unread badge keeps
// counting; if that load finds a seam, the backfill still goes out on the
// CURSOR door. So the cursor door must not mark either — otherwise backfilling
// in the background silently eats the unread state that peek exists to
// preserve. Here the watermark starts at 0 and the page carries HIGH ts values,
// so monotonicity hides nothing.
func TestChatBeforeCursorPageDoesNotAdvanceWatermark(t *testing.T) {
	// Positive control on its own server: the marking door really does mark,
	// so a green result below cannot come from a dead endpoint or a bad helper.
	//
	// The control seeds FEWER than chatListDefaultLimit rows on purpose. Since
	// T-91 the marking door only advances across a page that CONTINUES the
	// reader, and a first read of 40 rows would be served as the newest 30 —
	// a page with a hole under it, which correctly does not mark. A short
	// conversation keeps the control testing what it is for (the door marks)
	// instead of the continuity rule.
	sCtl := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedChatN(t, sCtl, "m-1", "a", 25, 1)
	withCtl := "m-1"
	chatGetRec(sCtl, "owner", HandleListChatApiChatGetParams{With: &withCtl})
	if wm := ownerWatermark(t, sCtl, "m-1"); wm != 25 {
		t.Fatalf("control: cursorless list must advance the watermark to 25, got %v", wm)
	}

	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	peer := "m-1"
	seedChatN(t, s, peer, "a", 40, 1)
	with := peer
	if wm := ownerWatermark(t, s, peer); wm != 0 {
		t.Fatalf("precondition: watermark must start at 0, got %v", wm)
	}

	// A cursor PAST the newest row, so the page carries the newest messages and
	// their high ts. Monotonicity cannot mask a write here.
	bts, bid := 999.0, "zzz"
	lim := 100
	page := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, Limit: &lim, BeforeTs: &bts, BeforeId: &bid,
	}))
	if len(page) != 40 || page[len(page)-1] != "a40" {
		t.Fatalf("precondition: the page must carry the newest rows, got %d rows ending %s",
			len(page), page[len(page)-1])
	}
	if wm := ownerWatermark(t, s, peer); wm != 0 {
		t.Fatalf("a before-cursor page MUST NOT advance the read watermark "+
			"(the T-b0bb backfill pages backwards on this door, and it runs while "+
			"the window is backgrounded behind ?peek=true): 0 -> %v", wm)
	}
}

// GUARD 2. THE PAIRING RULE THE CLIENT MUST OBEY.
// The backfill always sends both halves. If it ever sent one, this is what it
// would get, and the frontend simulator mirrors it so the mistake fails there
// too rather than only in production.
func TestChatLoneCursorHalfIs422(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedChatN(t, s, "m-1", "a", 5, 1)
	with := "m-1"
	bts := 3.0
	bid := "a03"

	if rec := chatGetRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, BeforeTs: &bts,
	}); rec.Code != 422 {
		t.Fatalf("before_ts alone must be 422, got %d", rec.Code)
	}
	if rec := chatGetRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, BeforeId: &bid,
	}); rec.Code != 422 {
		t.Fatalf("before_id alone must be 422, got %d", rec.Code)
	}
}

// GUARD 3. THE TWO WINDOW SHAPES THE FRONTEND SIMULATOR CLAIMS TO REPRODUCE.
//
// frontend/src/hooks/useChat.gap.test.ts mocks the wire with a hand-written
// server simulator. A simulator that has drifted from the real handler would
// let the frontend guards go green against a server that does not exist, which
// is exactly the failure mode those guards were written to avoid. This pins the
// two facts they depend on, at the same numbers.
func TestChatWindowShapesTheClientSimulatorAssumes(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	peer := "m-1"
	seedChatN(t, s, peer, "a", 70, 1)
	with := peer

	// (a) cursorless ⇒ the NEWEST chatListDefaultLimit rows, oldest→newest.
	got := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with}))
	if len(got) != chatListDefaultLimit {
		t.Fatalf("cursorless page size: want %d, got %d", chatListDefaultLimit, len(got))
	}
	if got[0] != "a41" || got[len(got)-1] != "a70" {
		t.Fatalf("cursorless window: want a41..a70, got %s..%s", got[0], got[len(got)-1])
	}

	// (b) before-cursor ⇒ the `limit` rows STRICTLY older than (ts, id),
	// oldest→newest. limit=100 is what the client's backfill sends.
	bts, bid := 41.0, "a41"
	lim := 100
	page := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, Limit: &lim, BeforeTs: &bts, BeforeId: &bid,
	}))
	if len(page) != 40 || page[0] != "a01" || page[len(page)-1] != "a40" {
		t.Fatalf("before-cursor page: want a01..a40 (40 rows), got %d rows %v", len(page), page)
	}
	// STRICTLY older: the cursor row itself must not come back.
	for _, id := range page {
		if id == "a41" {
			t.Fatalf("before-cursor page must exclude the cursor row itself")
		}
	}
}

// GUARD 4. THE SERVER HALF, WAS A CHARACTERIZATION (T-91).
//
// This used to record the problem rather than forbid it: the watermark advanced
// to the newest ts of the page it served, regardless of what the caller had
// actually been shown, so a caller that missed a window had those messages
// counted as read — unread 0, and no way for the client to notice the hole from
// unread state. That is why gapSuspected exists on the client at all.
//
// The owner ruled the server half in with the client half, so the same scenario
// is now asserted from the other side: the messages that were never delivered
// stay UNREAD. gapSuspected on the client is still the mechanism that closes
// the hole; this only stops the server lying about it in the meantime.
func TestChatWatermarkDoesNotCoverMessagesTheCallerNeverReceived(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	peer := "m-1"
	with := peer

	seedChatN(t, s, peer, "a", 30, 1)
	first := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with}))

	seedChatN(t, s, peer, "x", 40, 31) // a burst while the caller was away
	second := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with}))

	delivered := map[string]bool{}
	for _, id := range append(append([]string{}, first...), second...) {
		delivered[id] = true
	}
	all, err := s.dal.ListChat()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var never []string
	for _, m := range all {
		if !delivered[m.ID] {
			never = append(never, m.ID)
		}
	}
	if len(never) != 10 {
		t.Fatalf("expected 10 rows never delivered in any response, got %d: %v", len(never), never)
	}

	reads, err := s.dal.ListChatReads("owner", peer)
	if err != nil {
		t.Fatalf("reads: %v", err)
	}
	// 10 rows were never delivered; the 40-row burst that contains them is what
	// must still be reported unread. Zero here means the watermark swept the
	// undelivered rows — the exact lie T-91 removed.
	if n := UnreadCounts(all, reads, "owner")[peer]; n != 40 {
		t.Fatalf("unread must still cover the %d messages that were never delivered "+
			"in any response: want 40, got %d", len(never), n)
	}
}
