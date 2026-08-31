package main

// api_chat_watermark_gap_t91_test.go — the SERVER half of the T-b0bb chat gap.
//
// The client half (frontend/src/hooks/useChat.ts) stops the thread from LOSING
// messages. It could not stop the server from MARKING THEM READ on the way
// past: a cursorless ?with= list returns only the newest chatListDefaultLimit
// rows but slid the reader's watermark to that page's newest ts — over any
// messages that fell between the reader's old position and the page's oldest
// row. Those messages then read as "already read": unread 0, no unread
// divider, no error. "Missed" and "read" became indistinguishable.
//
// THE RULE PINNED HERE: the auto read-receipt may only advance across a page
// that is CONTIGUOUS with where the reader already was. If any message
// addressed to the reader by this peer sits strictly between the old watermark
// and this page's oldest row, the page is discontiguous for that reader and the
// watermark must NOT cross the hole.
//
// Two directions, deliberately, because pinning only one of them is worse than
// pinning neither:
//   - GapDoesNotAdvance: the watermark must not cross a hole.
//   - ContiguousStillAdvances: with no hole, the auto-receipt must still fire
//     exactly as before. Without this, "never advance at all" would pass.

import "testing"

// A page that skips over messages the reader never saw must NOT carry the
// watermark past them, and those messages must stay unread.
func TestChatListWatermarkStopsAtTheGapT91(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	peer := "m-1"
	with := peer

	// The reader is caught up at ts 30.
	seedChatN(t, s, peer, "a", 30, 1)
	chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	if wm := ownerWatermark(t, s, peer); wm != 30 {
		t.Fatalf("precondition: reader must start caught up at 30, got %v", wm)
	}

	// A burst of 40 arrives while the reader is away. The next cursorless page
	// carries only the newest 30 (ts 41..70) — ts 31..40 fall in the hole.
	seedChatN(t, s, peer, "x", 40, 31)
	page := chatIDs(t, chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with}))
	if len(page) != chatListDefaultLimit || page[0] != "x11" {
		t.Fatalf("precondition: the page must be the newest %d rows starting x11, got %d rows starting %s",
			chatListDefaultLimit, len(page), page[0])
	}

	if wm := ownerWatermark(t, s, peer); wm != 30 {
		t.Fatalf("watermark crossed a gap: the page skipped ts 31..40 (never sent to "+
			"this reader) yet the watermark moved 30 -> %v. Messages the reader was "+
			"never shown are now marked read.", wm)
	}

	all, err := s.dal.ListChat()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	reads, err := s.dal.ListChatReads("owner", peer)
	if err != nil {
		t.Fatalf("reads: %v", err)
	}
	if n := UnreadCounts(all, reads, "owner")[peer]; n != 40 {
		t.Fatalf("unread after a gapped page: want 40 (the whole burst still unread), got %d", n)
	}
}

// The other direction: with nothing missing, the auto read-receipt must still
// advance to the newest row it served. This is what makes the guard above a
// CONTINUITY rule rather than "stop advancing".
func TestChatListWatermarkStillAdvancesWhenContiguousT91(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	peer := "m-2"
	with := peer

	// (a) First read ever, fewer rows than a page: nothing can be missing.
	seedChatN(t, s, peer, "a", 5, 1)
	chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	if wm := ownerWatermark(t, s, peer); wm != 5 {
		t.Fatalf("contiguous first read must advance the watermark to 5, got %v", wm)
	}

	// (b) A follow-up burst that still fits inside one page: the page starts at
	// ts 6, which is exactly where the reader was. No hole ⇒ must advance.
	seedChatN(t, s, peer, "b", 20, 6)
	chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	if wm := ownerWatermark(t, s, peer); wm != 25 {
		t.Fatalf("contiguous follow-up read must advance the watermark to 25, got %v", wm)
	}

	all, err := s.dal.ListChat()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	reads, err := s.dal.ListChatReads("owner", peer)
	if err != nil {
		t.Fatalf("reads: %v", err)
	}
	if n := UnreadCounts(all, reads, "owner")[peer]; n != 0 {
		t.Fatalf("a fully contiguous read must clear unread, got %d", n)
	}
}
