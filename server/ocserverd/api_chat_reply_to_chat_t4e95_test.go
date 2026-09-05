package main

// api_chat_reply_to_chat_t4e95_test.go — `reply_to_chat` (T-4e95, owner ruling
// 2026-08-21): the quoted message, shipped WITH the reply, on every read.
//
// WHAT THIS REPLACED, AND WHY THE TESTS LOOK LIKE THIS
// The wire used to carry the quoted message's ID and nothing else, and the
// browser went and fetched the rest when it did not already have it. That fetch
// could fail; a failed fetch was rendered as a placeholder that was sometimes a
// lie; the lie was repaid on the next inbound event. Three behaviours, all of
// which draw the SAME PIXELS whether they are right or wrong — which is why
// twenty rounds of review kept finding new holes in them and no test could see
// any of it.
//
// The replacement has one behaviour, so these tests are about proving there is
// no second one:
//
//	① a reply into ANOTHER conversation carries its quote (the door the owner
//	   opened, and the quote crossing it)
//	② the quote is built on EVERY read door — listing, history page, by-ids,
//	   POST echo, wake snapshot — because a door that forgot would look exactly
//	   like a message whose original is gone
//	③ it is built even when the quoted message is RIGHT THERE in the same
//	   payload: the "already loaded, skip it" optimisation must not exist
//	④ a target that cannot be read leaves the quote ABSENT while `reply_to`
//	   stays — a settled state, not an error and not a retry
//	⑤ the SERVER decides how much of the body is a quote, and collapses it to
//	   one line
//	⑥ an attachment-only original quotes as "" — legal, not a failure

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// replyQuote is the decoded `reply_to_chat` of one message, plus the `reply_to`
// beside it — the two are only meaningful together and every assertion here
// reads both.
type replyQuoteView struct {
	ID          string             `json:"id"`
	ReplyTo     string             `json:"reply_to"`
	ReplyToChat *chatReplyQuoteDTO `json:"reply_to_chat"`
}

// decodeReplyQuote reads one message object out of a raw JSON body.
func decodeReplyQuote(t *testing.T, raw string) replyQuoteView {
	t.Helper()
	var v replyQuoteView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode message: %v — %s", err, raw)
	}
	return v
}

// decodeReplyQuotes reads a message ARRAY and returns it keyed by id.
func decodeReplyQuotes(t *testing.T, raw string) map[string]replyQuoteView {
	t.Helper()
	var rows []replyQuoteView
	if err := json.Unmarshal(chatEnvelopeMessages(t, []byte(raw)), &rows); err != nil {
		t.Fatalf("decode message list: %v — %s", err, raw)
	}
	out := map[string]replyQuoteView{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// ── ① the quote crosses the conversation boundary ────────────────────────────
//
// The companion of TestChatReplyTo_QuotingAnotherConversationIsAccepted, which
// pins that the POST is allowed. This pins the half that makes it USEFUL: the
// owner steps into two other members' thread by quoting a line out of it, and
// what comes back has to actually say what that line was. A server that stored
// the link and then declined to resolve a foreign target would leave the owner
// holding a reply that points at something they cannot see.
func TestReplyToChat_CrossesTheConversationBoundary(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-bystanders", Sender: "m-1", Recipient: "m-2",
		Body: "我覺得那個 leak 在 warden", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// T-91: post_chat answers a receipt, so the SERVED message is read back
	// through GET /api/chat?ids= — the read door the quote is actually built on.
	_, raw := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"這句可以展開講嗎","reply_to":"c-bystanders"}`)
	got := decodeReplyQuote(t, raw)
	if got.ReplyToChat == nil {
		t.Fatalf("the quote must ride along even when the original belongs to a "+
			"conversation the replier is in neither end of: %s", raw)
	}
	if got.ReplyToChat.ID != "c-bystanders" || got.ReplyToChat.From != "m-1" {
		t.Fatalf("the quote must name the original and its sender, got %+v",
			*got.ReplyToChat)
	}
	// 🔴 THE ADDRESSEE IS THE QUOTED MESSAGE'S OWN, AND THIS IS THE ONLY CELL
	// WHERE THE WRONG ANSWER IS DISTINGUISHABLE. The reply travels mira→owner
	// while the quoted line travelled m-1→m-2, so a projection that reached for
	// the ENCLOSING message's recipient — the plausible mistake, and the one a
	// same-conversation fixture cannot catch — would say "owner" here.
	//
	// MUTANT ①: drop To from chatReplyQuoteDTO → this is what says so.
	// MUTANT ②: project the replying message's recipient instead of the quoted
	// one's → red here, green in every other test in this file.
	if got.ReplyToChat.To != "m-2" {
		t.Fatalf("the quote must name the ORIGINAL's addressee (m-2), not the "+
			"recipient of the reply carrying it, got to=%q", got.ReplyToChat.To)
	}
	if got.ReplyToChat.Content != "我覺得那個 leak 在 warden" {
		t.Fatalf("the quote must carry what was said, got %q", got.ReplyToChat.Content)
	}
}

// ── ② every read door builds it ──────────────────────────────────────────────
//
// 🔴 THIS IS THE "一律組出" TEST and it is deliberately one test over four
// doors rather than four tests over one door each. The failure it exists to
// catch is a door someone forgot — and a per-door test file makes exactly that
// failure invisible, because the forgotten door has no file. Listing every door
// in one table means adding a door without adding a row here is the only way to
// escape it, and that is a visible omission in a diff.
//
// ⚠️ THERE IS A FIFTH CALL SITE AND IT IS DELIBERATELY NOT A ROW:
// `POST /api/tasks/{task_id}/message` (api_tasks.go) also serves through
// servedChatMessageDTO. It builds its own `meta` and that meta never contains
// `reply_to`, so `dto.ReplyTo` is "" on every message it can ever emit and
// chatReplyQuote returns nil by its first line — a row here could only assert
// "no quote", which is true of any function at all and would pin nothing. It is
// named here rather than left out silently, because "there are four doors" was
// already false when this table was written. If that handler ever learns to
// stamp `reply_to`, it becomes a real door and needs a real row.
//
// MUTANT: move the `dto.ReplyToChat = …` line out of servedChatMessageDTO into
// any single handler — this test goes red on the three it is no longer on.
func TestReplyToChat_IsBuiltOnEveryReadDoor(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-target", Sender: "owner", Recipient: "mira",
		Body: "這個你先做", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The POST echo is the first door, and it is answered by the same call that
	// creates the row every other door then reads.
	// 🔴 T-91 REMOVED THE "POST echo" DOOR FROM THIS LIST, and the reason is the
	// point of the list rather than an exception to it. This test enumerates the
	// doors that SERVE a message, because a door that skipped the quote join
	// would be indistinguishable on screen from an original that is really gone.
	// post_chat stopped being such a door: it answers {id, ts, attachments} — the
	// minted id, the server's stamp, and the attachment ids a caller that
	// uploaded inline learns there or nowhere — and it serves no message at all,
	// so there is no quote for it to skip. The four remaining doors are the four
	// that actually hand a reader a message, and they are all still here.
	replyID, _ := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"好，我接","reply_to":"c-target"}`)

	doors := []struct {
		name string
		read func() map[string]replyQuoteView
	}{
		{"GET /api/chat?with=", func() map[string]replyQuoteView {
			_, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
		{"GET /api/chat?ids=", func() map[string]replyQuoteView {
			_, raw := doRaw(t, "GET", srv.URL+"/api/chat?ids="+replyID, tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
		{"GET /api/chat?before_ts= (history page)", func() map[string]replyQuoteView {
			// A cursor far in the future, so the page is everything.
			_, raw := doRaw(t, "GET",
				srv.URL+"/api/chat?with=owner&before_ts=99999999999&before_id=zzzz",
				tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
	}

	for _, door := range doors {
		rows := door.read()
		row, ok := rows[replyID]
		if !ok {
			t.Fatalf("%s did not carry the reply at all (%d rows)", door.name, len(rows))
		}
		if row.ReplyTo != "c-target" {
			t.Fatalf("%s: reply_to must survive, got %q", door.name, row.ReplyTo)
		}
		if row.ReplyToChat == nil {
			t.Fatalf("%s: reply_to_chat must be built HERE too — a door that "+
				"skips it is indistinguishable on screen from an original that "+
				"is really gone", door.name)
		}
		if row.ReplyToChat.Content != "這個你先做" {
			t.Fatalf("%s: the quote must carry the original body, got %q",
				door.name, row.ReplyToChat.Content)
		}
	}
}

// ── ② (cont.) the wake snapshot is a read door too ───────────────────────────
//
// Separate test because it is a separate handler with its OWN projection
// (resumeChatMessageDTO), which is exactly the shape of bug the r18 review found
// for `reply_to` itself: the REST path was guarded, the wake path shared a
// helper with it and LOOKED guarded, and a mutant in the wake projection alone
// left the whole package green.
//
// It also pins the thing only this door has: the snapshot resolves DISPLAY
// NAMES, so the quote carries `from_name` here and "" everywhere else.
//
// MUTANT (run): delete the `d.ReplyToChat = s.chatReplyQuote(…)` line from
// resumeChatMessageDTO — TWO tests go red, this one and
// TestResumeSummary_EstimateCountsEverythingTheChatBlockCarries, which bills the
// quote's runes into chat_chars and therefore also notices when the quote stops
// being built. An earlier version of this note claimed "only this test"; that
// was never measured and it is wrong. The overlap is fine — this is still the
// only test that says WHAT the quote must contain on this door — but a mutant
// note that overstates its exclusivity invites the next reviewer to delete the
// other one as redundant.
func TestReplyToChat_IsBuiltInTheWakeSnapshot(t *testing.T) {
	api := resumeCtxServer(t)

	putChat(t, api, "c-asked", "m-peer", "m-exec", "要出還是等？", 100, nil)
	putChat(t, api, "c-answer", "m-exec", "m-peer", "等，我還在追一個 leak", 101,
		map[string]any{chatReplyToMetaKey: "c-asked"})

	snap := resumeSnapshot(t, api, "m-exec")
	answer := chatByID(t, snap, "c-answer")
	if answer.ReplyToChat == nil {
		t.Fatalf("a waking agent must read WHAT the reply answered, not just " +
			"that it answered something — reply_to_chat is absent")
	}
	if answer.ReplyToChat.Content != "要出還是等？" {
		t.Fatalf("the quote must carry the original body, got %q",
			answer.ReplyToChat.Content)
	}
	// The name half — this is the ONE read that resolves display names, and the
	// quote follows chatMessageDTO's own convention on the same payload.
	if answer.ReplyToChat.FromName != "小佩" {
		t.Fatalf("on the read that resolves names, the quote must carry the "+
			"sender's name beside the id, got from_name=%q from=%q",
			answer.ReplyToChat.FromName, answer.ReplyToChat.From)
	}
	// The addressee half of the same convention: both ids always, both names on
	// this door only. c-asked went m-peer→m-exec, so the pair is 小佩 → 阿執.
	if answer.ReplyToChat.To != "m-exec" || answer.ReplyToChat.ToName != "阿執" {
		t.Fatalf("the quote must carry the addressee's id AND, on this door, the "+
			"name beside it, got to=%q to_name=%q",
			answer.ReplyToChat.To, answer.ReplyToChat.ToName)
	}
	// A message that answers nothing claims no quote — without this half a
	// projection that stamped every row would pass.
	if asked := chatByID(t, snap, "c-asked"); asked.ReplyToChat != nil {
		t.Fatalf("a message that replies to nothing must carry no quote, got %+v",
			*asked.ReplyToChat)
	}

	// …and it is on the WIRE under its documented name. The struct assertions
	// above survive a field renamed in the JSON tag.
	raw := resumeSnapshotRaw(t, api, "m-exec")
	if !strings.Contains(raw, `"reply_to_chat"`) {
		t.Fatalf("reply_to_chat must be on the wire under that name: %s", raw)
	}
}

// ── ③ no "it is already in this payload" optimisation ────────────────────────
//
// 🔴 THE POINT OF THIS TEST IS THAT IT LOOKS REDUNDANT. Both messages are in
// the same listing, so a reader could resolve the quote itself, and skipping the
// build here would save a query and change nothing a user can see — today. It is
// forbidden anyway, and the owner ruled it so: an optimisation that fires
// SOMETIMES means the client needs a fallback for when it does not, and that
// fallback is the entire machine this redesign deleted.
//
// MUTANT: return nil from chatReplyQuote when the target is in the same batch —
// this test goes red and, by construction, nothing else in the package does.
func TestReplyToChat_IsBuiltEvenWhenTheOriginalIsInTheSamePayload(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-right-there", Sender: "owner", Recipient: "mira",
		Body: "one row above the reply", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	status, posted := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"answering it","reply_to":"c-right-there"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, posted)
	}
	replyID := decodeReplyQuote(t, posted).ID

	_, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	rows := decodeReplyQuotes(t, raw)
	if _, ok := rows["c-right-there"]; !ok {
		t.Fatalf("precondition: the original must be in the same payload — %s", raw)
	}
	row := rows[replyID]
	if row.ReplyToChat == nil {
		t.Fatalf("the quote must be built even though the original is right " +
			"there in the same response — a conditional build is a second code " +
			"path with no visible difference when it is wrong")
	}
	if row.ReplyToChat.Content != "one row above the reply" {
		t.Fatalf("got quote content %q", row.ReplyToChat.Content)
	}
}

// ── ④ a target that cannot be read: absent quote, surviving link ─────────────
//
// The state a real station reaches when the quoted message is cleared or the
// member that held it is gone. The link is stamped straight into meta here
// rather than posted, because the POST door refuses an unknown target on
// purpose (that refusal is guarded in api_chat_reply_to_t4e95_test.go ②) — this
// is about a link that WAS valid when it was made.
//
// Three assertions, and all three are load-bearing: the message is still served
// (a missing original must not take the conversation down), `reply_to` is still
// on it (so a reader can say "this was a reply and its original is gone" rather
// than silently drawing an ordinary message), and the quote is absent rather
// than fabricated or blank-but-present.
//
// MUTANT: make chatReplyQuote return an empty &chatReplyQuoteDTO{} instead of
// nil when the target misses — this test goes red on the absence assertion.
func TestReplyToChat_AbsentWhenTheOriginalIsGone(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-orphaned-reply", Sender: "mira", Recipient: "owner",
		Body: "answering something that is no longer here", TS: 5.0,
		Meta: map[string]any{chatReplyToMetaKey: "c-longgone"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	if status != 200 {
		t.Fatalf("a missing quote target must not fail the whole read: %d %s",
			status, raw)
	}
	row, ok := decodeReplyQuotes(t, raw)["c-orphaned-reply"]
	if !ok {
		t.Fatalf("the message itself must still be served: %s", raw)
	}
	if row.ReplyTo != "c-longgone" {
		t.Fatalf("reply_to must survive its target — it is what lets a reader "+
			"say 'this was a reply' at all, got %q", row.ReplyTo)
	}
	if row.ReplyToChat != nil {
		t.Fatalf("a target that cannot be read must leave the quote ABSENT, "+
			"not present-and-empty: %+v", *row.ReplyToChat)
	}
}

// ── ⑤ the SERVER decides how long a quote is, and flattens it ────────────────
//
// Both halves in one test because they are one decision: what a quote LINE is.
// The length used to live in the browser (ChatArea's QUOTE_EXCERPT_CHARS) and
// the wire carried the whole body, so every client shortened it itself — two
// copies of a display rule, neither of them wrong when they disagreed.
//
// The exact cut is asserted by RUNE COUNT rather than by a literal string, so
// this stays true for a body that is not ASCII — and the body here is not.
//
// 🔴 THE LENGTH IS WRITTEN OUT HERE AS A LITERAL, and the first version of this
// test did not do that — it compared against `chatReplyQuoteMaxChars + 1`. That
// assertion MOVES WITH THE THING IT CHECKS: a mutant changing the constant from
// 60 to 40 changed both sides of the comparison and the whole package stayed
// green (measured, not feared). A wire promise about a fixed length cannot be
// guarded by a test that reads the length off the wire's own definition.
//
// MUTANT: change chatReplyQuoteMaxChars, or replace the strings.Fields collapse
// with strings.TrimSpace — this test goes red on the count / on the newline
// respectively.
func TestReplyToChat_ContentIsShortenedAndFlattenedByTheServer(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	// 90 runes of CJK across three lines (95 runes in all, counting the two
	// newlines and the run of three spaces in the middle).
	//
	// 🔴 THE BLANK LINE IS EARLY ON PURPOSE. It has to survive INTO the 60 runes
	// that are kept, or the collapse assertion below is satisfied by the cut
	// rather than by the collapse — measured: with the newline at rune 90 a
	// mutant swapping the whitespace collapse for a plain TrimSpace left the
	// whole package green, because everything past rune 60 (newline included)
	// was thrown away either way.
	long := strings.Repeat("長", 10) + "\n\n" + strings.Repeat("話", 30) +
		"   " + strings.Repeat("短", 50)
	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-verbose", Sender: "owner", Recipient: "mira", Body: long, TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, raw := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"tl;dr?","reply_to":"c-verbose"}`)
	q := decodeReplyQuote(t, raw).ReplyToChat
	if q == nil {
		t.Fatalf("no quote: %s", raw)
	}
	// ① ONE LINE. A quote row is a pointer, not a rendering — a multi-line
	// excerpt would push the reader's layout around for nothing.
	if strings.ContainsAny(q.Content, "\n\r") {
		t.Fatalf("the quote must be collapsed to one line, got %q", q.Content)
	}
	// ② CUT BY THE SERVER, at 60 runes. The 60 is spelled out — see the note
	// above about why reading chatReplyQuoteMaxChars here guards nothing.
	const wantQuoteRunes = 60
	if chatReplyQuoteMaxChars != wantQuoteRunes {
		t.Fatalf("the quote length is a WIRE PROMISE (%d runes) and changing it "+
			"changes what every reader draws — if that is deliberate, change it "+
			"here too, on purpose. got chatReplyQuoteMaxChars=%d",
			wantQuoteRunes, chatReplyQuoteMaxChars)
	}
	runes := []rune(q.Content)
	if len(runes) != wantQuoteRunes+1 { // + the ellipsis standing in for the cut
		t.Fatalf("the server must cut the quote to %d runes + an ellipsis, got "+
			"%d runes (%q)", wantQuoteRunes, len(runes), q.Content)
	}
	if runes[len(runes)-1] != '…' {
		t.Fatalf("a cut quote must say it was cut, got %q", q.Content)
	}
	// ③ …and the whole body is NOT on the wire. Without this, a server that
	// shipped everything and let the client trim would still pass ① and ② if it
	// also happened to ship a trimmed copy. The tail run is what to look for:
	// it is past the cut, so its presence anywhere in the response means the
	// untrimmed body rode along.
	if strings.Contains(raw, strings.Repeat("短", 50)) {
		t.Fatalf("the untrimmed body must not ride along: %s", raw)
	}
}

// ── ⑥ an attachment-only original quotes as "" ───────────────────────────────
//
// "" is an ORDINARY value here, not a failure and not a placeholder for one —
// the way a missing original is said is the absence of the whole object (④).
// Pinned because the obvious "helpful" change is to substitute some 「（附件）」
// text server-side, which would make an empty quote and a missing quote look
// alike again.
func TestReplyToChat_AnAttachmentOnlyOriginalQuotesAsEmpty(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-photo", Sender: "owner", Recipient: "mira", Body: "", TS: 1.0,
		Meta: map[string]any{"attachments": []any{
			map[string]any{"id": "a-1", "mime": "image/png", "filename": "x.png"},
		}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, raw := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"這張是哪來的","reply_to":"c-photo"}`)
	q := decodeReplyQuote(t, raw).ReplyToChat
	if q == nil {
		t.Fatalf("a text-less original is still an original — the quote must be " +
			"PRESENT with an empty content, because absence means 'gone'")
	}
	if q.Content != "" {
		t.Fatalf("nothing may be invented for a body-less message, got %q", q.Content)
	}
	if q.ID != "c-photo" || q.From != "owner" {
		t.Fatalf("the quote must still name the original and its sender, got %+v", *q)
	}
}

// ── ④ (cont.) A READ FAILURE IS NOT A MISS ───────────────────────────────────
//
// 🔴 THE HOLE THIS CLOSES. chatReplyQuote used to say
// `if err != nil || len(quoted) == 0 { return nil }` — one nil for two facts
// that are not the same fact. The browser draws absence as ONE FIXED,
// ASSERTIVE sentence 「這則訊息已不存在」, so a database that merely failed to
// answer produced a confident claim that a message which is still sitting in
// that database is gone. The r21 review mutated `err != nil` into a panic and
// NOTHING went red: zero coverage on a line carrying a lie.
//
// FAULT INJECTION, WITHOUT A PRODUCTION SEAM. `s.dal` is a concrete *DAL over
// sqlite and there is no interface to substitute, so the fault is put in the
// DATA: a chat row whose `meta` column is not JSON makes scanChat — and
// therefore ListChatByIDs — return an error for that row and only that row.
// The corrupt row is parked in a conversation between two OTHER members, so the
// door under test never scans it directly; the only thing that reaches it is the
// quote join. Without that separation the handler's own listing read would fail
// first and the test would pass for the wrong reason (measured: on
// `GET /api/chat?with=`, which does a whole-table ListChat, it does exactly
// that — which is why the doors below are the two that read a bounded set).
//
// MUTANT: restore `if err != nil || len(quoted) == 0 { return nil, nil }` in
// chatReplyQuote — all FOUR doors below come back 200 with `reply_to_chat: null`
// and this test goes red on the first of them. Nothing else in the package
// moves.
func TestReplyToChat_AnUnreadableOriginalFailsLoudlyRatherThanClaimingItIsGone(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	// The quoted message: someone else's conversation (legal to quote since the
	// 2026-08-21 widening) and UNREADABLE — `meta` is not JSON.
	if _, err := db.Exec(`
		INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"c-unreadable", "m-a", "m-b", "我覺得那個 leak 在 warden", 1.0,
		"{this is not json"); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-reply", Sender: "mira", Recipient: "owner", Body: "我跟進", TS: 2.0,
		Meta: map[string]any{chatReplyToMetaKey: "c-unreadable"},
	}); err != nil {
		t.Fatalf("seed reply: %v", err)
	}

	// 🔴 THE WAKE PATH IS IN THIS TABLE, and it is the door that matters most.
	// The two reads below it are a browser refreshing a thread; these two run
	// while an agent is waking, so an agent that once replied to a row which
	// later goes bad cannot start at all. Measured on this very fixture: both
	// answer 200 before the reply exists and 500 after, with the bad row
	// unchanged — and the bad row is in a THIRD PARTY'S conversation, so
	// the snapshot's own bounded read (ListChatInvolving, capped at
	// resumeChatFetch) never scans it. Only the quote join reaches it.
	//
	// That cost is the accepted price of the 2026-08-21 ruling (bad data must be
	// noisy), not a defect — but it must be VISIBLE in the suite, because it is
	// the thing a future reader will want to weigh before changing this line.
	//
	// ⚠️ THE OTHER TWO SERVING DOORS ARE DELIBERATELY ABSENT — see chatReplyQuote.
	// `GET /api/chat?with=` (whole-table ListChat) and the POST echo (the
	// reply_to existence gate reads the same id before storing anything) each
	// 500 on their OWN read of the bad row, so their quote branch is unreachable
	// for a single-row fault. Verified by mutation: with the error swallowed in
	// chatReplyQuote, those two still 500 while all four rows here turn 200. A
	// row for either would be green against the mutant and would guard nothing.
	doors := []struct {
		name string
		url  string
	}{
		{"GET /api/resume-summary (wake)",
			srv.URL + "/api/resume-summary"},
		{"GET /api/resume-summary-size (wake)",
			srv.URL + "/api/resume-summary-size"},
		{"GET /api/chat?ids=", srv.URL + "/api/chat?ids=c-reply"},
		{"GET /api/chat?before_ts= (history page)",
			srv.URL + "/api/chat?with=owner&before_ts=99999999999&before_id=zzzz"},
	}
	for _, door := range doors {
		status, raw := doRaw(t, "GET", door.url, tok, "", nil)
		if status != 500 {
			t.Fatalf("%s: a quote that could not be READ must fail loudly, not "+
				"come back as the fixed 「這則訊息已不存在」 sentence — got %d %s",
				door.name, status, raw)
		}
	}

	// …and the SAME door answers 200 for a target that is genuinely absent.
	// Without this half the assertion above is satisfied by a server that 500s
	// on every miss, which would be the opposite defect (a cleared message would
	// take out the conversation that quotes it).
	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-reply-gone", Sender: "mira", Recipient: "owner", Body: "這則的目標真的沒了",
		TS: 3.0, Meta: map[string]any{chatReplyToMetaKey: "c-never-existed"},
	}); err != nil {
		t.Fatalf("seed gone reply: %v", err)
	}
	status, raw := doRaw(t, "GET", srv.URL+"/api/chat?ids=c-reply-gone", tok, "", nil)
	if status != 200 {
		t.Fatalf("a genuinely missing original is a settled state, not an "+
			"error: got %d %s", status, raw)
	}
	rows := decodeReplyQuotes(t, raw)
	row, ok := rows["c-reply-gone"]
	if !ok {
		t.Fatalf("the reply itself must still be served: %s", raw)
	}
	if row.ReplyTo != "c-never-existed" {
		t.Fatalf("reply_to must survive a missing target, got %q", row.ReplyTo)
	}
	if row.ReplyToChat != nil {
		t.Fatalf("a missing original must leave the quote ABSENT, got %+v",
			*row.ReplyToChat)
	}
}
