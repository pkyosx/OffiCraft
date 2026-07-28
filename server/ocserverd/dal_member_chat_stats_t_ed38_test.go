package main

// T-ed38 — ListMemberChatStats: the ONE aggregate that replaced the roster
// handler's ListChat()+ListChatReads()+UnreadCounts trio.
//
// Two properties are locked here, because a wrong answer on either is invisible
// on screen (a roster row simply sorts to the wrong place):
//
//  1. unread stays BYTE-FOR-BYTE what UnreadCounts computed. It drives the red
//     badge that has shipped for months; the aggregate is a performance change,
//     not a semantic one. The parity check below runs BOTH over the same
//     fixture and demands they agree — a divergence is a regression in the
//     shipped badge, not merely a new field being wrong.
//  2. last_activity is CALLER-RELATIVE and DIRECTION-BLIND: the newest message
//     between the caller and that peer either way. Not the peer's global
//     activity, and not the read watermark.

import "testing"

// chatStatsFixture is the ONE conversation set both assertions below read, so
// the parity check compares the two implementations over identical input.
//
//	owner ↔ m-1 : owner sent last (ts 40); two of m-1's are past the watermark
//	owner ↔ m-2 : m-2 sent last (ts 55), no watermark at all
//	m-1   ↔ m-2 : agent↔agent — must not touch the owner's numbers
//	owner ↔ m-3 : nothing at all — must be ABSENT, not a zero row
func chatStatsFixture(t *testing.T) *DAL {
	t.Helper()
	d := newTestDAL(t)
	for _, m := range []ChatMessage{
		{ID: "c-10", Sender: "owner", Recipient: "m-1", TS: 10},
		{ID: "c-20", Sender: "m-1", Recipient: "owner", TS: 20}, // read (<= watermark)
		{ID: "c-30", Sender: "m-1", Recipient: "owner", TS: 30}, // unread
		{ID: "c-35", Sender: "m-1", Recipient: "owner", TS: 35}, // unread
		{ID: "c-40", Sender: "owner", Recipient: "m-1", TS: 40}, // newest of this thread
		{ID: "c-55", Sender: "m-2", Recipient: "owner", TS: 55},
		{ID: "c-99", Sender: "m-1", Recipient: "m-2", TS: 99}, // agent↔agent
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
	if _, _, err := d.PutChatRead(ChatRead{ReaderID: "owner", PeerID: "m-1", LastReadTS: 25}); err != nil {
		t.Fatalf("put read: %v", err)
	}
	return d
}

func TestListMemberChatStats_UnreadMatchesUnreadCounts(t *testing.T) {
	d := chatStatsFixture(t)

	stats, err := d.ListMemberChatStats("owner")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// The OLD path, over the same rows.
	msgs, err := d.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	receipts, err := d.ListChatReads("owner", "")
	if err != nil {
		t.Fatalf("list reads: %v", err)
	}
	want := UnreadCounts(msgs, receipts, "owner")

	// Every peer the old path counted must match…
	for peer, n := range want {
		if got := stats[peer].UnreadCount; got != n {
			t.Fatalf("unread for %s: aggregate says %d, UnreadCounts says %d", peer, got, n)
		}
	}
	// …and the aggregate must not invent unread the old path never saw (e.g.
	// counting the owner's OWN sends, which share the same peer key).
	for peer, s := range stats {
		if s.UnreadCount != want[peer] {
			t.Fatalf("unread for %s: aggregate says %d, UnreadCounts says %d",
				peer, s.UnreadCount, want[peer])
		}
	}
	// Pin the expected shape too, so a fixture that stopped exercising the
	// watermark cannot make the parity check vacuously true.
	if stats["m-1"].UnreadCount != 2 {
		t.Fatalf("m-1 must have exactly the 2 messages past the watermark, got %d",
			stats["m-1"].UnreadCount)
	}
	if stats["m-2"].UnreadCount != 1 {
		t.Fatalf("m-2 has no watermark at all → its 1 message counts, got %d",
			stats["m-2"].UnreadCount)
	}
}

func TestListMemberChatStats_LastActivityIsCallerRelativeAndDirectionBlind(t *testing.T) {
	d := chatStatsFixture(t)

	stats, err := d.ListMemberChatStats("owner")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// The OWNER's own send is the newest in the m-1 thread — direction-blind, so
	// it is the activity stamp. (An "inbound only" reading would answer 35.)
	if got := stats["m-1"].LastActivityAt; got != 40 {
		t.Fatalf("m-1 activity must be the newest message EITHER way (40), got %v", got)
	}
	// The read watermark (25) must not enter this at all.
	if got := stats["m-2"].LastActivityAt; got != 55 {
		t.Fatalf("m-2 activity must be 55, got %v", got)
	}
	// A peer the caller never exchanged anything with is ABSENT — the zero value
	// is the honest "never talked", not a fabricated row.
	if _, ok := stats["m-3"]; ok {
		t.Fatalf("a never-contacted peer must not appear at all: %+v", stats)
	}
	// The agent↔agent message (ts 99, the NEWEST row in the table) belongs to
	// neither owner thread — a caller-blind MAX(ts) would leak it into both.
	for peer, s := range stats {
		if s.LastActivityAt == 99 {
			t.Fatalf("agent↔agent traffic leaked into %s's caller-relative activity: %+v",
				peer, stats)
		}
	}

	// Same table, DIFFERENT caller: m-2 sees its own two conversations, and the
	// agent↔agent line IS part of one of them.
	fromM2, err := d.ListMemberChatStats("m-2")
	if err != nil {
		t.Fatalf("stats m-2: %v", err)
	}
	if got := fromM2["m-1"].LastActivityAt; got != 99 {
		t.Fatalf("m-2↔m-1 activity must be 99, got %v", got)
	}
	if got := fromM2["owner"].LastActivityAt; got != 55 {
		t.Fatalf("m-2↔owner activity must be 55, got %v", got)
	}
}

func TestListMemberChatStats_EmptyStreamIsAnEmptyMap(t *testing.T) {
	d := newTestDAL(t)
	stats, err := d.ListMemberChatStats("owner")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("no chat at all must fold to an empty map, got %+v", stats)
	}
}
