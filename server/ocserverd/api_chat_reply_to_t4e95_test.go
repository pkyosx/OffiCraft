package main

// api_chat_reply_to_t4e95_test.go — `reply_to` (T-4e95): a message that quotes
// another message.
//
// The owner's ruling of 2026-08-21 shapes every assertion here, so it is worth
// stating once. The link is stored as an ID; what it POINTS AT is rebuilt and
// shipped on every read (`reply_to_chat`, guarded in
// api_chat_reply_to_chat_t4e95_test.go). This file is about the LINK: what the
// server accepts, what it refuses, and what it will not let a caller forge.
//
//	① the link ROUND-TRIPS — post it, read it back, it is still there
//	② a link to a message that does not exist is REFUSED — an id naming nothing
//	   is a mistake in the request
//	③ a link OUT of this conversation is ACCEPTED. This is the reversal: the
//	   server used to refuse it, and the owner ruled the refusal out, because
//	   replying to a line two other people exchanged in order to step in and ask
//	   about it is the use case. Nothing is smuggled by allowing it — a by-ids
//	   read already reaches every conversation, so the quoted text was readable
//	   before anyone quoted it.
//
//	④ …plus the one that is not about the caller's honesty but about the
//	   handler's: the meta map is copied through WHOLESALE, so a caller can put
//	   a reply_to there directly. It must be discarded, or ② is decoration.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// postedChat drives POST /api/chat as `mira` and returns (status, raw body).
func postedChat(t *testing.T, url string, tok string, body string) (int, string) {
	t.Helper()
	return doRaw(t, "POST", url+"/api/chat", tok, "application/json", []byte(body))
}

// chatFields reads the two fields these tests assert on out of a message JSON.
func chatFields(t *testing.T, raw string) (id string, replyTo string) {
	t.Helper()
	var msg struct {
		ID      string `json:"id"`
		ReplyTo string `json:"reply_to"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("decode chat message: %v — %s", err, raw)
	}
	return msg.ID, msg.ReplyTo
}

// postedChatRead posts a chat message and returns the SERVED message — the row
// read straight back through GET /api/chat?ids=<the id the receipt minted>.
//
// 🔴 T-91 IS WHY THIS EXISTS, AND THE TESTS BELOW GOT STRONGER FOR IT. post_chat
// used to answer with the whole chatMessageDTO, so every assertion here could be
// made against the write's own echo — which is exactly the reading this file
// already warned about in its own words ("the POST response is built from the
// in-memory row the handler just made, so it would still look right if the link
// were never persisted"). The write now answers {id, ts, attachments}: the id it
// minted, the stamp only the server can make, and the attachment ids a caller
// that uploaded inline learns here or nowhere. Everything else is read back, so
// the round trip this file is named for is the only path left.
func postedChatRead(t *testing.T, srvURL, tok, body string) (string, string) {
	t.Helper()
	status, raw := postedChat(t, srvURL, tok, body)
	if status != 200 {
		t.Fatalf("post chat: %d %s", status, raw)
	}
	var receipt struct {
		ID string  `json:"id"`
		TS float64 `json:"ts"`
	}
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatalf("decode post receipt: %v — %s", err, raw)
	}
	if receipt.ID == "" || receipt.TS <= 0 {
		t.Fatalf("the post receipt must carry the minted id and the server stamp: %s", raw)
	}
	status, listed := doRaw(t, "GET", srvURL+"/api/chat?ids="+receipt.ID, tok, "", nil)
	if status != 200 {
		t.Fatalf("re-read %s: %d %s", receipt.ID, status, listed)
	}
	rows := chatEnvelopeMessages(t, []byte(listed))
	var served []json.RawMessage
	if err := json.Unmarshal(rows, &served); err != nil {
		t.Fatalf("decode message list: %v — %s", err, listed)
	}
	if len(served) != 1 {
		t.Fatalf("?ids=%s served %d rows, want 1: %s", receipt.ID, len(served), listed)
	}
	return receipt.ID, string(served[0])
}

// ① ROUND TRIP. Posted, stored, served — and served again on a SECOND read, so
// a handler that merely echoed back what it was handed cannot pass.
func TestChatReplyTo_RoundTripsThroughStorage(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	quotedID, quotedServed := postedChatRead(t, srv.URL, tok, `{"to":"owner","body":"the question"}`)
	if _, quotedReplyTo := chatFields(t, quotedServed); quotedReplyTo != "" {
		t.Fatalf("a plain post must carry no link, got %q", quotedReplyTo)
	}

	replyID, replyServed := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"the answer","reply_to":"`+quotedID+`"}`)
	if _, replyTo := chatFields(t, replyServed); replyTo != quotedID {
		t.Fatalf("the served message must carry the link, got %q want %q", replyTo, quotedID)
	}

	// The decisive half: read it back off the wire a SECOND time, through the
	// raw listing, so a serving path that resolved the link only on the first
	// read cannot pass either.
	status, listed := doRaw(t, "GET", srv.URL+"/api/chat?ids="+replyID, tok, "", nil)
	if status != 200 {
		t.Fatalf("re-read: %d %s", status, listed)
	}
	if !strings.Contains(listed, `"reply_to":"`+quotedID+`"`) {
		t.Fatalf("the stored link must survive a re-read: %s", listed)
	}
}

// ② A link to nothing. 400 rather than 404 on purpose: what was not found is a
// FIELD OF THIS REQUEST, not the resource the request addresses.
//
// The second half is the load-bearing one — the message must not be stored
// anyway. A refusal that still writes the row is a refusal in name only.
func TestChatReplyTo_UnknownTargetIsRefusedAndNothingIsStored(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"orphan","reply_to":"c-nosuchmessage"}`)
	if status != 400 {
		t.Fatalf("an unknown reply_to must be refused, got %d %s", status, raw)
	}
	if !strings.Contains(raw, "c-nosuchmessage") {
		t.Fatalf("the refusal must name the id it is about: %s", raw)
	}

	_, listed := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	if strings.Contains(listed, "orphan") {
		t.Fatalf("a refused post must not be stored: %s", listed)
	}
}

// ③ A link OUT of this conversation is ACCEPTED — and this test exists because
// until 2026-08-21 it was a 400.
//
// 🔴 THIS IS THE ONE THAT PROVES THE DOOR IS OPEN. Both shapes the old check
// refused are here: a message the caller really is one end of but in a different
// thread, and a message strictly between two other members. The second is the
// owner's actual use case, stated as such — 「引用另外兩個人對話裡的一句話來介入
// 詢問」 — and the FIRST one is the one a half-hearted revert would leave broken,
// because "am I a participant" is the plausible-looking rule someone reaches for
// when deleting "is it the same conversation".
//
// The quote itself is asserted too, not just the 200: a server that accepted the
// post and then quietly declined to build reply_to_chat for a foreign target
// would pass a status-only test while shipping a reply with nothing visible
// attached, which is the failure this feature is supposed to make impossible.
func TestChatReplyTo_QuotingAnotherConversationIsAccepted(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	dal := NewDAL(db)

	// (a) mira's OWN message, but to a different peer.
	if err := dal.PutChat(ChatMessage{
		ID: "c-otherthread", Sender: "mira", Recipient: "kye",
		Body: "a line in another thread", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed other thread: %v", err)
	}
	// (b) a message strictly between two other members — the owner's case.
	if err := dal.PutChat(ChatMessage{
		ID: "c-bystanders", Sender: "m-1", Recipient: "m-2",
		Body: "the sentence worth stepping in about", TS: 2.0,
	}); err != nil {
		t.Fatalf("seed bystanders: %v", err)
	}

	for _, tc := range []struct{ id, body string }{
		{"c-otherthread", "a line in another thread"},
		{"c-bystanders", "the sentence worth stepping in about"},
	} {
		_, raw := postedChatRead(t, srv.URL, tok,
			`{"to":"owner","body":"stepping in about `+tc.id+`","reply_to":"`+tc.id+`"}`)
		if _, replyTo := chatFields(t, raw); replyTo != tc.id {
			t.Fatalf("the cross-conversation link must be stored, got %q want %q",
				replyTo, tc.id)
		}
		// …and the quote really came along, so the reply is readable as a reply.
		if !strings.Contains(raw, tc.body) {
			t.Fatalf("reply_to_chat must carry the quoted body across the "+
				"conversation boundary too: %s", raw)
		}
	}
}

// ③ (cont.) THE MAIN USE CASE: replying to a message the other party sent YOU.
//
// ⚠️ WHAT THIS TEST USED TO GUARD IS GONE. It was written to pin that
// sameChatConversation compared the two {sender, recipient} pairs as SETS
// rather than positionally — a reply travels the opposite way to the message it
// quotes, and a positional comparison refused every honest reply. That function
// was deleted with the same-conversation rule on 2026-08-21, so the property is
// not merely untested now, it does not exist.
//
// It is kept, smaller in claim, because the SHAPE it drives is still the
// commonest one in the product and nothing else in this file drives it: a reply
// posted in the opposite direction to the message it quotes must round-trip its
// link. Read it as coverage of the ordinary case, not as a guard on a rule.
func TestChatReplyTo_ReplyingToWhatTheOtherPartySentYou(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	// The owner's line to mira — the opposite direction to the reply below.
	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-fromowner", Sender: "owner", Recipient: "mira",
		Body: "這個你先做", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, raw := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"好，我接","reply_to":"c-fromowner"}`)
	if _, replyTo := chatFields(t, raw); replyTo != "c-fromowner" {
		t.Fatalf("the link must be stored, got %q", replyTo)
	}
}

// ④ THE FORGED LINK. `meta` is copied through wholesale — that is documented
// behaviour and other keys rely on it — so the ONE key the server owns has to be
// removed on the way in. Without this, ② and ③ are decoration: a caller that
// wants an unvalidated link just puts it in meta instead.
func TestChatReplyTo_ACallerSuppliedMetaLinkIsDiscarded(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"forged","meta":{"reply_to":"c-forged","keepme":"yes"}}`)
	if status != 200 {
		t.Fatalf("the post itself is legal: %d %s", status, raw)
	}
	id, replyTo := chatFields(t, raw)
	if replyTo != "" {
		t.Fatalf("a caller-supplied meta.reply_to must not become the link, got %q", replyTo)
	}
	if strings.Contains(raw, "c-forged") {
		t.Fatalf("the forged value must not survive anywhere on the message: %s", raw)
	}

	// Read it back too — and check the SIBLING key survived, so "delete the
	// forged link" cannot be satisfied by dropping the whole meta map.
	status, listed := doRaw(t, "GET", srv.URL+"/api/chat?ids="+id, tok, "", nil)
	if status != 200 {
		t.Fatalf("re-read: %d %s", status, listed)
	}
	if strings.Contains(listed, "c-forged") {
		t.Fatalf("the forged link must not be stored: %s", listed)
	}
	if !strings.Contains(listed, "keepme") {
		t.Fatalf("only the reply_to key is the server's — the rest of meta must "+
			"still ride through: %s", listed)
	}
}

// ⑤ THE AGENT DOOR IS THE SAME DOOR (owner ruling, rc-67f1f1263daf: 「能：agent
// 發訊也可以指定回覆對象」). Every test above already posts AS AN AGENT, which is
// the behavioural half. This is the DISCOVERABILITY half: an agent only ever
// learns a parameter exists by reading the tool schema, so a reply_to the agent
// cannot see is a reply_to the agent does not have.
func TestChatReplyTo_ThePostChatToolSchemaAdvertisesIt(t *testing.T) {
	desc := postChatInputSchemaDescriptionOfReplyTo(t)
	if desc == "" {
		t.Fatal("post_chat's inputSchema must carry reply_to — the owner ruled " +
			"that agents may specify a reply target, and a parameter absent " +
			"from the schema is a parameter no agent will ever send")
	}
	// The two things an agent cannot discover by trying: the ONE refusal that
	// exists (the target must exist), and the fact that the conversation
	// boundary is NOT one — an agent that believes the old rule simply never
	// attempts the owner's use case, and gets no error to learn from.
	for _, promise := range []string{
		"must EXIST",
		"DOES NOT HAVE TO BE IN THE CONVERSATION",
		"DISCARDED",
	} {
		if !strings.Contains(desc, promise) {
			t.Fatalf("the schema must state %q — an agent only ever learns this "+
				"parameter's rules by reading them: %s", promise, desc)
		}
	}
}

// ⑥ THE META DOOR IS DOCUMENTED AS SHUT. ④ pins that a caller-supplied
// meta.reply_to is discarded; this pins that the agent-facing description SAYS
// SO. They are different failures: without ④ the door is open, without this the
// door is shut and nobody was told — and what an agent reads is the meta
// description, which still said "a misspelled key is stored rather than refused"
// and "One key is different" after this feature made reply_to the second one.
// An agent that believes that sends a link through meta, gets a 200, and finds
// the key gone when it reads the message back. A promise no test holds is a
// promise that goes stale on the next change; this one already had.
func TestChatReplyTo_TheMetaDescriptionAdmitsTheDeletion(t *testing.T) {
	desc := postChatInputSchemaDescriptionOf(t, "meta")
	if desc == "" {
		t.Fatal("post_chat's inputSchema must carry meta")
	}
	for _, promise := range []string{"meta.reply_to", "DELETES"} {
		if !strings.Contains(desc, promise) {
			t.Fatalf("meta's description must state %q — the handler removes "+
				"that key before storing, silently, and this description is "+
				"the only place an agent could learn it", promise)
		}
	}
}

// postChatInputSchemaDescriptionOfReplyTo reads reply_to's description out of
// the FROZEN MCP catalog — the bytes tools/list actually serves an agent, not
// the Go struct. Same reasoning as idsPropertyDescription: the promise an agent
// reads and the behaviour the tests above pin have to be the same sentence, or
// one of them drifts silently.
func postChatInputSchemaDescriptionOfReplyTo(t *testing.T) string {
	t.Helper()
	return postChatInputSchemaDescriptionOf(t, "reply_to")
}

// postChatInputSchemaDescriptionOf is the same read for any one property.
func postChatInputSchemaDescriptionOf(t *testing.T, prop string) string {
	t.Helper()
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name == "post_chat" {
			return tool.InputSchema.Properties[prop].Description
		}
	}
	t.Fatalf("post_chat is missing from spec/mcp-catalog.json")
	return ""
}
