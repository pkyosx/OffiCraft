package main

// dal_chat_pushdown_t48_test.go — T-48 EQUIVALENCE HARNESS.
//
// Two whole-table reads moved into SQL: the cursorless chat page
// (ListChat + filter + slice in Go → ListChatLatest) and the unread fold
// (ListChat + ListChatReads + UnreadCounts in Go → UnreadCountsFor).
//
// ⚠️ THE TWO SOURCE-GREP RULES THAT USED TO LIVE HERE ARE GONE (T-48, R13-6).
// `TestChatListingDoesNotReadTheWholeTable` and
// `TestUnreadCountsHaveExactlyOneEntryPoint` read production `.go` text and
// matched strings, so extracting a handler body into a helper — no behaviour
// change at all — flipped them. They were lint rules in the wrong house; they
// are `bin/chat-pushdown-guard.py` now, wired into `bin/ci.sh` as
// `lint-chat-pushdown`, with its own positive/negative controls. What is left in
// this file asserts OUTPUTS against the pre-pushdown implementation, which is a
// different and non-substitutable job: the lint says the fast path is the only
// path, these say the fast path gives the same answers.
//
// 🔴 EQUIVALENCE IS THE REQUIREMENT, not "looks right". Each test keeps the OLD
// implementation alive as a reference function in this file and drives BOTH
// over the same fixtures, comparing the full result (order, content, count) —
// a hand-written expectation would only pin what the author already believed.

import (
	"net/http/httptest"
	"testing"
)

// listChatLatestReference is VERBATIM the pre-T-48 handler path: read the whole
// table oldest→newest, filter to the participant, filter again by one side,
// then keep the last `limit`. It is the oracle, so it must not be "improved".
//
// The second filter used to be `caller_only` (the caller, EITHER side); that
// parameter was removed with T-48 and `sender` (ONE side) took its job, so the
// oracle narrows the same way the wire now does.
func listChatLatestReference(t *testing.T, d *DAL, participant, sender string, limit int) []ChatMessage {
	t.Helper()
	msgs, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	if participant != "" {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == participant || m.Recipient == participant {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if sender != "" {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == sender {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if limit >= 0 {
		if limit == 0 {
			msgs = nil
		} else if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
	}
	return msgs
}

// seedChatPushdownFixture builds a stream with everything the two paths can
// disagree about: several conversation lines, both directions, third-party
// traffic the caller is not in, EQUAL timestamps whose only tie-break is id
// (both orders of insertion), and an id whose lexical order fights its ts.
func seedChatPushdownFixture(t *testing.T, d *DAL) {
	t.Helper()
	msgs := []ChatMessage{
		{ID: "m01", Sender: "m-1", Recipient: "owner", TS: 1},
		{ID: "m02", Sender: "owner", Recipient: "m-1", TS: 2},
		{ID: "m03", Sender: "m-2", Recipient: "owner", TS: 3},
		{ID: "m04", Sender: "m-1", Recipient: "m-2", TS: 4}, // owner not involved
		{ID: "m05", Sender: "m-1", Recipient: "owner", TS: 5},
		// equal ts, ids out of insertion order — the (ts, id) tie-break.
		{ID: "m07", Sender: "m-1", Recipient: "owner", TS: 6},
		{ID: "m06", Sender: "owner", Recipient: "m-1", TS: 6},
		{ID: "m08", Sender: "m-2", Recipient: "m-1", TS: 6},   // equal ts, third line
		{ID: "a99", Sender: "m-1", Recipient: "owner", TS: 7}, // id sorts before m*
		{ID: "m09", Sender: "m-3", Recipient: "owner", TS: 8},
		{ID: "m10", Sender: "owner", Recipient: "m-3", TS: 9},
		{ID: "m11", Sender: "m-1", Recipient: "owner", TS: 10},
	}
	for _, m := range msgs {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
}

func sameChatRows(a, b []ChatMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Sender != b[i].Sender ||
			a[i].Recipient != b[i].Recipient || a[i].Body != b[i].Body ||
			a[i].TS != b[i].TS {
			return false
		}
	}
	return true
}

func chatRowIDs(msgs []ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestListChatLatestMatchesTheGoFilterItReplaced(t *testing.T) {
	d := newTestDAL(t)
	seedChatPushdownFixture(t, d)

	participants := []string{"", "owner", "m-1", "m-2", "m-3", "nobody"}
	senders := []string{"", "owner", "m-1", "m-2", "nobody"}
	limits := []int{-1, 0, 1, 2, 3, 5, 12, 30, 100}

	cases := 0
	for _, p := range participants {
		for _, c := range senders {
			for _, lim := range limits {
				want := listChatLatestReference(t, d, p, c, lim)
				got, err := d.listChatLatest(chatListFilter{participant: p, sender: c}, lim)
				if err != nil {
					t.Fatalf("ListChatLatest(%q,%q,%d): %v", p, c, lim, err)
				}
				if !sameChatRows(want, got) {
					t.Fatalf("ListChatLatest(%q,%q,%d) diverged from the Go path:\n old %v\n new %v",
						p, c, lim, chatRowIDs(want), chatRowIDs(got))
				}
				cases++
			}
		}
	}
	if cases != len(participants)*len(senders)*len(limits) {
		t.Fatalf("case count: %d", cases)
	}
	// The oracle must actually SEE rows, or every comparison above is 0 == 0.
	if n := len(listChatLatestReference(t, d, "m-1", "", -1)); n != 9 {
		t.Fatalf("fixture is not exercising the filters: m-1 line has %d rows, want 9", n)
	}
}

func TestListChatLatestOnAnEmptyTable(t *testing.T) {
	d := newTestDAL(t)
	for _, lim := range []int{-1, 0, 1, 30} {
		got, err := d.ListChatLatest("m-1", lim)
		if err != nil {
			t.Fatalf("limit %d: %v", lim, err)
		}
		if len(got) != 0 {
			t.Fatalf("limit %d: want no rows, got %v", lim, chatRowIDs(got))
		}
	}
}

// ── unread counts ────────────────────────────────────────────────────────────

func TestUnreadCountsForMatchesTheGoFold(t *testing.T) {
	d := newTestDAL(t)
	seedChatPushdownFixture(t, d)
	// A watermark per shape: none at all (m-3), one BELOW the newest message
	// (m-1, ts 6 — a-99@7 and m11@10 stay unread), one exactly ON a message's
	// ts (m-2, ts 3 — strict > means it counts as read), and one for a peer
	// that never sent anything.
	for _, r := range []ChatRead{
		{ReaderID: "owner", PeerID: "m-1", LastReadTS: 6},
		{ReaderID: "owner", PeerID: "m-2", LastReadTS: 3},
		{ReaderID: "owner", PeerID: "ghost", LastReadTS: 99},
		{ReaderID: "m-1", PeerID: "owner", LastReadTS: 2}, // ANOTHER reader's receipt
	} {
		if _, _, err := d.PutChatRead(r); err != nil {
			t.Fatalf("put read: %v", err)
		}
	}

	for _, reader := range []string{"owner", "m-1", "m-2", "m-3", "nobody"} {
		messages, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		receipts, err := d.ListChatReads(reader, "")
		if err != nil {
			t.Fatalf("ListChatReads: %v", err)
		}
		want := UnreadCounts(messages, receipts, reader)
		got, err := d.UnreadCountsFor(reader)
		if err != nil {
			t.Fatalf("UnreadCountsFor(%q): %v", reader, err)
		}
		if len(want) != len(got) {
			t.Fatalf("UnreadCountsFor(%q): want %v, got %v", reader, want, got)
		}
		for peer, n := range want {
			if got[peer] != n {
				t.Fatalf("UnreadCountsFor(%q)[%q]: want %d, got %d (full: %v vs %v)",
					reader, peer, n, got[peer], want, got)
			}
		}
	}

	// The oracle must be non-trivial: owner really does have unread, and the
	// on-the-ts watermark really did clear m-2's only message.
	own, err := d.UnreadCountsFor("owner")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if own["m-1"] != 2 {
		t.Fatalf("fixture: owner should have 2 unread from m-1, got %d (%v)", own["m-1"], own)
	}
	if _, present := own["m-2"]; present {
		t.Fatalf("a watermark ON a message's ts must clear it (strict >), got %v", own)
	}
	if own["m-3"] != 1 {
		t.Fatalf("a peer with NO receipt must count every addressed message, got %v", own)
	}
}

// ── the outsource unread faces (T-48 follow-up) ──────────────────────────────
//
// api_outsource.go carried THREE MORE copies of the same whole-table unread
// fold — the contractor LIST (which the owner pays on every cockpit open), the
// single-worker GET, and writeWorkerProjectionWith (the shared response fold
// behind every owner lifecycle verb: relocate / refocus / stop / restart /
// model). All three now go through DAL.UnreadCountsFor.
//
// Same rule as everywhere else in this file: the oracle is the OLD code, run
// over the same fixture, per actor — not a hand-written number.

func outsourceUnreadOracle(t *testing.T, d *DAL, actor, workerID string) int {
	t.Helper()
	messages, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	receipts, err := d.ListChatReads(actor, "")
	if err != nil {
		t.Fatalf("ListChatReads: %v", err)
	}
	return UnreadCounts(messages, receipts, actor)[workerID]
}

func TestOutsourceUnreadFacesMatchTheGoFold(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	worker, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || worker == nil {
		t.Fatalf("worker %s: %+v (%v)", workerID, worker, err)
	}

	// Traffic in both directions across three readers, so each actor below sees
	// a DIFFERENT number and a wrong actor cannot pass by coincidence.
	for _, m := range []ChatMessage{
		{ID: "o-1", Sender: workerID, Recipient: wireOwnerID, TS: 2000},
		{ID: "o-2", Sender: workerID, Recipient: wireOwnerID, TS: 2001},
		{ID: "o-3", Sender: workerID, Recipient: wireOwnerID, TS: 2002},
		{ID: "o-4", Sender: wireOwnerID, Recipient: workerID, TS: 2003}, // owner's own send
		{ID: "o-5", Sender: workerID, Recipient: "mira", TS: 2004},      // a third party's line
		{ID: "o-6", Sender: "mira", Recipient: wireOwnerID, TS: 2005},   // not this worker
	} {
		if err := api.dal.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
	// Owner has read up to o-2; mira has read nothing; the worker itself has a
	// watermark on a peer that never wrote to it.
	if _, _, err := api.dal.PutChatRead(ChatRead{
		ReaderID: wireOwnerID, PeerID: workerID, LastReadTS: 2001,
	}); err != nil {
		t.Fatalf("put read: %v", err)
	}
	if _, _, err := api.dal.PutChatRead(ChatRead{
		ReaderID: workerID, PeerID: "nobody", LastReadTS: 9999,
	}); err != nil {
		t.Fatalf("put read: %v", err)
	}

	actors := []string{wireOwnerID, "mira", workerID, "m-front", "never-seen"}
	distinct := map[int]bool{}
	for _, actor := range actors {
		want := outsourceUnreadOracle(t, api.dal, actor, workerID)
		distinct[want] = true

		// FACE 1 — the contractor list (cockpit's 外包 panel).
		rows := listWorkersAs(t, api, actor)
		if len(rows) != 1 {
			t.Fatalf("actor %s: want one worker row, got %d", actor, len(rows))
		}
		if rows[0].UnreadCount != want {
			t.Fatalf("actor %s: LIST unread_count = %d, the Go fold says %d",
				actor, rows[0].UnreadCount, want)
		}

		// FACE 2 — the single-worker detail GET.
		rec := httptest.NewRecorder()
		api.HandleGetOutsourceWorkerApiOutsourceWorkersIdGet(rec,
			taskReq(t, "GET", "/api/outsource-workers/"+workerID, nil, actor, "owner"), workerID)
		if rec.Code != 200 {
			t.Fatalf("actor %s: single GET → %d %s", actor, rec.Code, rec.Body.String())
		}
		one := decodeBody[outsourceWorkerDTO](t, rec)
		if one.UnreadCount != want {
			t.Fatalf("actor %s: single-GET unread_count = %d, the Go fold says %d",
				actor, one.UnreadCount, want)
		}

		// FACE 3 — writeWorkerProjectionWith, the shared post-op fold behind
		// every owner lifecycle verb (relocate / refocus / stop / restart / model).
		rec = httptest.NewRecorder()
		api.writeWorkerProjection(rec,
			taskReq(t, "POST", "/api/outsource-workers/"+workerID+"/stop", nil, actor, "owner"), *worker)
		if rec.Code != 200 {
			t.Fatalf("actor %s: projection → %d %s", actor, rec.Code, rec.Body.String())
		}
		proj := decodeBody[outsourceWorkerDTO](t, rec)
		if proj.UnreadCount != want {
			t.Fatalf("actor %s: post-op projection unread_count = %d, the Go fold says %d",
				actor, proj.UnreadCount, want)
		}
	}
	// The fixture must actually SEPARATE the actors, or all fifteen comparisons
	// above are the same number compared to itself.
	if len(distinct) < 2 {
		t.Fatalf("fixture does not discriminate between actors: every one saw %v", distinct)
	}
	if n := outsourceUnreadOracle(t, api.dal, wireOwnerID, workerID); n != 1 {
		t.Fatalf("fixture: owner should have exactly 1 unread from the worker (watermark at o-2), got %d", n)
	}
}
