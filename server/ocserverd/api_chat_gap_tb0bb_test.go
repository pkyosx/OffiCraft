package main

// api_chat_gap_tb0bb_test.go — the SERVER-SIDE facts the frontend's seam fix
// (T-b0bb, frontend/src/hooks/useChat.ts) is built on. Read-only: it drives the
// real HandleListChatApiChatGet through httptest against a temp DAL, using the
// helpers api_chat_peek_test.go already defines. It starts no server.
//
// All four are GUARDS. The fourth was a CHARACTERIZATION of behaviour T-b0bb
// worked AROUND and could not change from the client — the listing marking
// messages read that no response had carried. T-48 removed that write, so it
// is now a guard like the rest, asserting the opposite of what it recorded.

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
// the client still loads the thread so the unread badge keeps counting; if that
// load finds a seam, the backfill goes out on the CURSOR door. So the cursor
// door must not mark — otherwise backfilling in the background silently eats
// the unread state. Here the watermark starts at 0 and the page carries HIGH ts
// values, so monotonicity hides nothing.
//
// T-48 made this true of EVERY door on GET /api/chat, not just this one (the
// cursorless list used to mark, and ?peek=true was how the background window
// opted out; both are gone). This test stays because it pins the cursor branch
// specifically, at the numbers the frontend backfill uses.
func TestChatBeforeCursorPageDoesNotAdvanceWatermark(t *testing.T) {
	// Positive control on its own server: the ONE door that still marks really
	// does mark (POST /api/chat/mark-read), so a green result below cannot come
	// from a dead watermark table or a bad helper.
	sCtl := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedChatN(t, sCtl, "m-1", "a", 40, 1)
	if rec := markReadRec(sCtl, "owner", "m-1", 40); rec.Code != 200 {
		t.Fatalf("control: mark-read → %d: %s", rec.Code, rec.Body.String())
	}
	if wm := ownerWatermark(t, sCtl, "m-1"); wm != 40 {
		t.Fatalf("control: mark-read must advance the watermark to 40, got %v", wm)
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
			"the window is backgrounded): 0 -> %v", wm)
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

// ── GUARD 4 (was a CHARACTERIZATION until T-48) ──────────────────────────────
//
// This used to document a problem that WAS still there: the cursorless list
// advanced the watermark to the newest ts of the page it served, regardless of
// what the caller had actually been shown, so a caller that missed a window had
// those messages counted as read. It was the reason the client could not rely
// on unread state to notice a hole (the justification for gapSuspected), and it
// said in so many words that if someone fixed it later this test should be
// REWRITTEN, not deleted.
//
// T-48 fixed it, by removing the write rather than narrowing it: no path on
// GET /api/chat marks anything read any more (owner ruling 2026-09-02 —
// marking read is POST /api/chat/mark-read's job, stated explicitly). So the
// same scenario is now asserted in the other direction: the 10 messages no
// response ever carried are STILL UNREAD, and so is everything else the
// listing merely walked past.
func TestChatListNeverMarksMessagesTheCallerNeverReceived(t *testing.T) {
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
	if n := UnreadCounts(all, reads, "owner")[peer]; n != len(all) {
		t.Fatalf("listing must leave every message unread (T-48): unread is %d, "+
			"want %d — including the %d rows no response ever carried. A listing "+
			"that marks again puts the T-b0bb hole back: the client could not see "+
			"a gap it had already been told it had read.", n, len(all), len(never))
	}
}
