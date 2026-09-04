package main

// api_chat_envelope_t48_test.go — GET /api/chat after T-48's second half: the
// {messages, next_cursor} envelope, the opaque continuation cursor, the unread
// backfill, the one-sided sender/recipient filters, and the refusal of query
// parameters this route does not declare.
//
// Each behaviour below was verified to go red under a mutant that breaks THAT
// behaviour specifically; the mutant is named in the test's own note. The one
// that matters most is TestChatUnreadJudgesEachMessageAgainstItsOwnSendersWatermark:
// its mutant is the single-watermark unread, which fails by returning a SHORT
// page — indistinguishable from having nothing unread, and therefore silent.

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// seedUnreadFixture lays down the fixture the unread tests share.
//
// 🔑 IT IS BUILT SO THE TWO WORLDS DISAGREE. Under the correct per-(reader,
// sender) rule the caller's unread is {b-1, b-2, a-3}; under a single global
// watermark (the reader's highest last_read_ts, 2.0) it would be {b-2, a-3} —
// b-1 disappears, because it is older than a watermark belonging to a DIFFERENT
// sender. A fixture where both rules agree would let the mutant pass, so the
// disagreement is the fixture's job, not a coincidence in it.
//
//	m-1 → owner   ts 1, 2, 3     watermark (owner, m-1) = 2.0   ⇒ a-3 unread
//	m-2 → owner   ts 1.5, 2.5    NO watermark row               ⇒ both unread
//	owner → owner ts 4           own message                    ⇒ never unread
//	owner → m-1   ts 5           not addressed to the caller    ⇒ not unread
func seedUnreadFixture(t *testing.T, s *apiServer) {
	t.Helper()
	msgs := []ChatMessage{
		{ID: "a-1", Sender: "m-1", Recipient: "owner", Body: "a1", TS: 1},
		{ID: "b-1", Sender: "m-2", Recipient: "owner", Body: "b1", TS: 1.5},
		{ID: "a-2", Sender: "m-1", Recipient: "owner", Body: "a2", TS: 2},
		{ID: "b-2", Sender: "m-2", Recipient: "owner", Body: "b2", TS: 2.5},
		{ID: "a-3", Sender: "m-1", Recipient: "owner", Body: "a3", TS: 3},
		{ID: "self", Sender: "owner", Recipient: "owner", Body: "note to self", TS: 4},
		{ID: "out", Sender: "owner", Recipient: "m-1", Body: "outbound", TS: 5},
	}
	for _, m := range msgs {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
	if _, _, err := s.dal.PutChatRead(ChatRead{
		ReaderID: "owner", PeerID: "m-1", LastReadTS: 2.0,
	}); err != nil {
		t.Fatalf("put chat_read: %v", err)
	}
}

func unreadTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedUnreadFixture(t, s)
	return s
}

// pageIDs decodes one envelope into (ids, next_cursor).
func pageIDs(t *testing.T, rec *httptest.ResponseRecorder) ([]string, string) {
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
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out, chatEnvelopeNextCursor(t, rec.Body.Bytes())
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── the envelope ────────────────────────────────────────────────────────────

// EVERY path answers the object, and the three that name their own set carry no
// cursor. MUTANT: make any one path `writeJSON(w, 200, out)` again and its case
// goes red on "must answer the T-48 envelope".
func TestChatEveryPathAnswersTheEnvelope(t *testing.T) {
	s := windowTestServer(t)
	cases := []struct {
		name       string
		query      string
		wantCursor bool
	}{
		{"newest page, everything fits", "with=m-1", false},
		{"newest page, more behind it", "with=m-1&limit=2", true},
		{"the deprecated keyset page", "with=m-1&before_ts=6&before_id=c-6&limit=2", true},
		{"by ids", "ids=c-1&ids=c-2", false},
		{"start_id window", "start_id=c-2&limit=2", false},
		{"end_id window", "end_id=c-5&limit=2", false},
		{"unread", "unread=true", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := chatQueryRec(s, "owner", tc.query)
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			// chatEnvelopeMessages fails the test unless the body is the object.
			ids, cursor := pageIDs(t, rec)
			if (cursor != "") != tc.wantCursor {
				t.Fatalf("next_cursor %q, wantPresent=%v (rows %v)",
					cursor, tc.wantCursor, ids)
			}
		})
	}
}

// A cursor is a POSITION, so a message landing mid-walk cannot displace a row
// the caller has not read yet, nor hide one.
//
// MUTANT: page by row COUNT instead — make the second page
// `ListChatLatest(limit)` skipping `limit` rows in Go — and the assertion below
// goes red with c-3 served twice and c-2 never served.
func TestChatCursorIsStableWhenNewMessagesLandMidWalk(t *testing.T) {
	s := windowTestServer(t) // c-1..c-6 on the owner↔m-1 line, ts 1..6
	seen := []string{}
	ids, cursor := pageIDs(t, chatQueryRec(s, "owner", "with=m-1&limit=2"))
	seen = append(ids, seen...)
	if !sameIDs(ids, []string{"c-5", "c-6"}) {
		t.Fatalf("first page = %v, want the newest two", ids)
	}
	if cursor == "" {
		t.Fatal("first page must carry a cursor — four older messages remain")
	}
	// A new message arrives between the two requests. This is the whole point.
	if err := s.dal.PutChat(ChatMessage{
		ID: "c-7", Sender: "m-1", Recipient: "owner", Body: "landed mid-walk", TS: 7,
	}); err != nil {
		t.Fatalf("put c-7: %v", err)
	}
	for cursor != "" {
		prev := cursor
		ids, cursor = pageIDs(t, chatQueryRec(s, "owner", "with=m-1&limit=2&cursor="+prev))
		if cursor == prev {
			t.Fatalf("the cursor did not advance (%q) — a drain loop would spin here", cursor)
		}
		seen = append(ids, seen...)
	}
	if !sameIDs(seen, []string{"c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}) {
		t.Fatalf("walking back with the cursor must serve every older message "+
			"exactly once and never re-serve one: %v", seen)
	}
}

// ── unread ──────────────────────────────────────────────────────────────────

// 🔴 THE MUTANT THIS FILE EXISTS FOR. Replace the per-sender join in
// DAL.listChatUnread with ONE watermark —
//
//	LEFT JOIN chat_read r ON r.reader_id = ?   (no `AND r.peer_id = m.sender`)
//
// or, equivalently, compare every message against
// `(SELECT MAX(last_read_ts) FROM chat_read WHERE reader_id = ?)` — and this
// test goes red: b-1 (ts 1.5, from a peer the caller has NEVER opened)
// disappears because it is older than the watermark for a DIFFERENT sender.
//
// It fails as a SHORT PAGE. There is no error, no count, nothing to notice —
// which is why the assertion is on the exact set and not on "some rows came
// back".
func TestChatUnreadJudgesEachMessageAgainstItsOwnSendersWatermark(t *testing.T) {
	s := unreadTestServer(t)
	ids, _ := pageIDs(t, chatQueryRec(s, "owner", "unread=true"))
	want := []string{"b-1", "b-2", "a-3", "self"} // oldest→newest: 1.5, 2.5, 3, 4
	if !sameIDs(ids, want) {
		t.Fatalf("unread = %v, want %v — a message is unread against ITS OWN "+
			"sender's watermark, never against one global one", ids, want)
	}
}

// THE REVERSE GUARD, and it is its own test on purpose: the dangerous unread
// failure is UNDER-returning (a message that is genuinely unread never comes
// back), because it is silent — a short page reads exactly like an empty inbox.
// This one names the row that must be there and says why it is the fragile one.
//
// MUTANT: make the watermark comparison `m.ts > COALESCE(r.last_read_ts, 0)`
// into `>=`, or default a missing receipt to the newest ts instead of 0 —
// either drops b-1 and this goes red naming it.
func TestChatUnreadNeverDropsAMessageFromAPeerYouHaveNeverOpened(t *testing.T) {
	s := unreadTestServer(t)
	ids, _ := pageIDs(t, chatQueryRec(s, "owner", "unread=true"))
	found := false
	for _, id := range ids {
		if id == "b-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("b-1 is unread — m-2 has no chat_read row at all, so its whole "+
			"history is unread — and it is MISSING from %v. Under-returning here "+
			"is invisible: a short page looks like an empty inbox", ids)
	}
	// POSITIVE CONTROL for that assertion: mark m-2 read and b-1 must go. Without
	// this, "b-1 is present" could equally mean the filter is dead.
	if _, _, err := s.dal.PutChatRead(ChatRead{
		ReaderID: "owner", PeerID: "m-2", LastReadTS: 2.5,
	}); err != nil {
		t.Fatalf("put chat_read: %v", err)
	}
	ids, _ = pageIDs(t, chatQueryRec(s, "owner", "unread=true"))
	if !sameIDs(ids, []string{"a-3", "self"}) {
		t.Fatalf("after marking m-2 read to 2.5, unread = %v, want [a-3 self]", ids)
	}
}

// 🔴 WHAT YOU SENT TO SOMEBODY ELSE IS NEVER YOUR UNREAD, AND WHAT YOU SENT TO
// YOURSELF IS (owner ruling rc-dccab860be32). Those two look like one rule and
// are not: `out` is kept out by `recipient = ?`, which is about who the message
// is FOR; `self` is addressed to the caller and so it is genuinely a message
// waiting in the caller's own inbox. Whether a member wants to be shown its own
// handover note is a question about PRINTING, and it is answered in cli/ocagent
// — this door only answers "newer than my watermark for whoever sent it".
//
// MUTANT: put `m.sender <> ?` back into the query and `self` disappears here.
func TestChatUnreadKeepsWhatYouSentYourselfAndDropsWhatYouSentOthers(t *testing.T) {
	s := unreadTestServer(t)
	ids, _ := pageIDs(t, chatQueryRec(s, "owner", "unread=true"))
	for _, id := range ids {
		if id == "out" {
			t.Fatalf("a message you SENT to someone else is never your unread; got %v", ids)
		}
	}
	var found bool
	for _, id := range ids {
		if id == "self" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a message you addressed to YOURSELF is in your own inbox and is "+
			"unread until a receipt says otherwise; got %v — if this went red because "+
			"the predicate came back, the printing rule belongs in cli/ocagent", ids)
	}
}

func TestChatUnreadServesTheOldestBatchFirstAndPagesForwards(t *testing.T) {
	s := unreadTestServer(t)
	ids, cursor := pageIDs(t, chatQueryRec(s, "owner", "unread=true&limit=2"))
	if !sameIDs(ids, []string{"b-1", "b-2"}) {
		t.Fatalf("first unread page = %v, want the OLDEST two [b-1 b-2]", ids)
	}
	if cursor == "" {
		t.Fatal("a-3 is still unread — the page must carry a cursor")
	}
	ids, next := pageIDs(t, chatQueryRec(s, "owner", "unread=true&limit=2&cursor="+cursor))
	if !sameIDs(ids, []string{"a-3", "self"}) {
		t.Fatalf("second unread page = %v, want [a-3 self]", ids)
	}
	if next != "" {
		t.Fatalf("the backlog is exhausted; next_cursor must be absent, got %q", next)
	}
}

// DRAIN-TO-EMPTY TERMINATES, and a cursor that fails to advance is caught
// rather than spun on. The iteration cap is the point: without it a
// non-advancing cursor hangs the suite instead of failing it.
//
// MUTANT: make DAL.listChatUnread ignore its `after` anchor and every round
// serves the same row with the same cursor — the loop stops on the repeated
// cursor, NAMING it, instead of running forever.
func TestChatUnreadDrainLoopTerminatesAndTheCursorAlwaysAdvances(t *testing.T) {
	s := unreadTestServer(t)
	const maxRounds = 20
	cursor, rounds := "", 0
	seen := []string{}
	for {
		q := "unread=true&limit=1"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		ids, next := pageIDs(t, chatQueryRec(s, "owner", q))
		seen = append(seen, ids...)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatalf("round %d: next_cursor did not advance (%q) — this is the "+
				"shape that makes 'drain until empty' spin forever", rounds, next)
		}
		cursor = next
		if rounds++; rounds > maxRounds {
			t.Fatalf("drain did not terminate within %d rounds (seen %v)", maxRounds, seen)
		}
	}
	if !sameIDs(seen, []string{"b-1", "b-2", "a-3", "self"}) {
		t.Fatalf("the drain must see every unread message exactly once: %v", seen)
	}
}

// 🔴 PAGING THE BACKLOG CLEARS NOTHING. The unread path is the loudest place
// T-48's "this route never writes a watermark" rule could be broken — a
// backfill reads exactly like "I have now seen these".
//
// MUTANT: add a PutChatRead to serveChatUnread and this goes red. The positive
// control that the watermark is writable at all is
// TestMarkReadWritesWatermark in api_chat_peek_test.go.
func TestChatUnreadPagingNeverAdvancesAWatermark(t *testing.T) {
	s := unreadTestServer(t)
	before1 := ownerWatermark(t, s, "m-1")
	cursor := ""
	for i := 0; i < 5; i++ {
		q := "unread=true&limit=1"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		_, next := pageIDs(t, chatQueryRec(s, "owner", q))
		if next == "" {
			break
		}
		cursor = next
	}
	if wm := ownerWatermark(t, s, "m-1"); wm != before1 {
		t.Fatalf("paging unread moved the (owner, m-1) watermark %v -> %v", before1, wm)
	}
	if wm := ownerWatermark(t, s, "m-2"); wm != 0 {
		t.Fatalf("paging unread wrote a (owner, m-2) watermark: %v", wm)
	}
	// And the backlog is still there afterwards — the observable half of the
	// same statement, stated in the caller's own terms.
	ids, _ := pageIDs(t, chatQueryRec(s, "owner", "unread=true"))
	if !sameIDs(ids, []string{"b-1", "b-2", "a-3", "self"}) {
		t.Fatalf("after a full drain the backlog must be unchanged, got %v", ids)
	}
}

// ── refusals ────────────────────────────────────────────────────────────────

// MUTANT: drop any one refusal and its row goes red. Each says WHAT is wrong,
// so the message is asserted too — a bare status code would let a refusal for
// the wrong reason pass.
func TestChatRefusesContradictoryCursorRequests(t *testing.T) {
	s := unreadTestServer(t)
	// A real cursor of each direction, minted by the route itself.
	_, older := pageIDs(t, chatQueryRec(s, "owner", "limit=1"))
	_, newer := pageIDs(t, chatQueryRec(s, "owner", "unread=true&limit=1"))
	if older == "" || newer == "" {
		t.Fatalf("precondition: need one cursor of each direction, got %q / %q",
			older, newer)
	}
	cases := []struct {
		name   string
		query  string
		status int
		says   string
	}{
		{"cursor with the deprecated pair", "cursor=" + older + "&before_ts=1&before_id=a-1",
			422, "one keyset walk per request"},
		{"cursor with a window anchor", "cursor=" + older + "&start_id=a-1",
			422, "one keyset walk per request"},
		{"unread with the deprecated pair", "unread=true&before_ts=9&before_id=z",
			422, "unread cannot be combined"},
		{"unread with a window anchor", "unread=true&end_id=a-1",
			422, "unread cannot be combined"},
		{"an unread cursor on the plain listing", "cursor=" + newer,
			422, "towards newer messages"},
		{"a plain-listing cursor on unread", "unread=true&cursor=" + older,
			422, "towards older messages"},
		{"a token this API never minted", "cursor=not-a-cursor",
			422, "copy the previous response's next_cursor back verbatim"},
		{"a blank cursor is SENT, not absent", "cursor=",
			422, "copy the previous response's next_cursor back verbatim"},
		{"an unknown parameter", "wiht=m-1",
			400, "wiht"},
		{"the removed caller_only", "with=m-1&caller_only=true",
			400, "caller_only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := chatQueryRec(s, "owner", tc.query)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.says) {
				t.Fatalf("refusal must say %q, got %s", tc.says, body)
			}
		})
	}
}

// The refusal NAMES the offending parameter — "bad parameters" would send a
// caller re-reading its whole query string. MUTANT: drop the `%s` for the
// unknown names (or refuse with a generic message) and this goes red.
func TestChatUnknownParameterRefusalNamesEveryOffender(t *testing.T) {
	s := unreadTestServer(t)
	rec := chatQueryRec(s, "owner", "with=m-1&peek=true&calller_only=1")
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, name := range []string{"calller_only", "peek"} {
		if !strings.Contains(body, name) {
			t.Fatalf("the refusal must name %q, got %s", name, body)
		}
	}
	// It also states what IS accepted, so a mistyped name is one round trip to
	// fix rather than a trip to the spec.
	if !strings.Contains(body, "recipient") || !strings.Contains(body, "unread") {
		t.Fatalf("the refusal must list the accepted parameters, got %s", body)
	}
}

// 🔴 THE ACCEPTED SET IS THE SPEC'S, NOT A SECOND LIST. This reads
// spec/openapi.json — the source the params struct is generated from — and
// requires the guard's set to be exactly that plus the `?token=` transport
// credential. MUTANT: hand-write a set in api_chat.go and add a parameter to
// the spec; this goes red naming the parameter the guard would have refused.
func TestChatQueryParamSetIsTheSpecsOwnParameterList(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.json"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	paths, _ := spec["paths"].(map[string]any)
	chat, _ := paths["/api/chat"].(map[string]any)
	get, _ := chat["get"].(map[string]any)
	declared, _ := get["parameters"].([]any)
	if len(declared) == 0 {
		t.Fatal("could not read the GET /api/chat parameters out of spec/openapi.json")
	}
	want := map[string]bool{authTokenQueryParam: true}
	for _, raw := range declared {
		p, _ := raw.(map[string]any)
		if p["in"] != "query" {
			continue
		}
		name, _ := p["name"].(string)
		want[name] = true
	}
	for name := range want {
		if !chatQueryParamSet[name] {
			t.Fatalf("the spec declares %q but the guard would refuse it with a 400", name)
		}
	}
	for name := range chatQueryParamSet {
		if !want[name] {
			t.Fatalf("the guard accepts %q, which the spec does not declare", name)
		}
	}
}

// The `?token=` credential rides the query on clients that cannot set a header
// (EventSource, <img src>), so the guard must let it through. MUTANT: drop
// authTokenQueryParam from chatQueryParamSet and this goes red — in production
// it would deny every such client, and only there.
func TestChatQueryGuardAdmitsTheTokenCredential(t *testing.T) {
	s := unreadTestServer(t)
	if rec := chatQueryRec(s, "owner", "with=m-1&token=whatever"); rec.Code != 200 {
		t.Fatalf("?token= must not be refused as unknown: %d %s",
			rec.Code, rec.Body.String())
	}
}

// ── one-sided filters ───────────────────────────────────────────────────────

// MUTANT: make `sender` match either side (`sender = ? OR recipient = ?`, the
// shape `with` uses) and the second case goes red — `out` comes back.
func TestChatSenderAndRecipientNarrowOneSideEach(t *testing.T) {
	s := unreadTestServer(t)
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"sender alone", "sender=m-2", []string{"b-1", "b-2"}},
		{"recipient alone", "recipient=m-1", []string{"out"}},
		{"both AND into one direction", "sender=owner&recipient=m-1", []string{"out"}},
		{"an id nothing matches is an empty page, not an error", "sender=nobody", []string{}},
		{"composed with with=", "with=m-1&sender=m-1", []string{"a-1", "a-2", "a-3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, _ := pageIDs(t, chatQueryRec(s, "owner", tc.query))
			if !sameIDs(ids, tc.want) {
				t.Fatalf("%s = %v, want %v", tc.query, ids, tc.want)
			}
		})
	}
}
