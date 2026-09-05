package main

// T-4e95: ONE chat message can carry BOTH a reply-card link and a reply link.
//
// The cockpit's ChatArea renders `{m.replyCardId && quoteLine}` — a quote line
// beside a reply-card row — and earlier review rounds judged that branch
// unreachable "in product", i.e. dead code kept alive only by a test. It is
// not dead: `post_chat` exposes `meta` and `reply_to` side by side, this
// handler copies caller `meta` through WHOLESALE and deletes exactly ONE key
// from it (`reply_to`), and the cockpit derives `m.replyCardId` straight from
// `meta.reply_card_id` (api/mappers.ts) with no card-existence check. So the
// server really does serve rows with both fields set, and the cockpit branch
// really is on the product path.
//
// This pins the server half of that claim: the wholesale meta copy keeps
// `reply_card_id`, and the handler's own validated `reply_to` write survives
// alongside it. The cockpit half is pinned in
// frontend/src/components/ChatArea.reply-card-quote.test.tsx.

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChatPostCarriesReplyCardIDAndReplyToOnTheSameMessage(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	target, _ := postedChatRead(t, srv.URL, tok, `{"to":"owner","body":"the question"}`)
	if target == "" {
		t.Fatalf("no seed id")
	}

	// T-91: post_chat answers {id, ts, attachments}, so the two fields this test
	// is about are read off the SERVED message. That is the honest surface for
	// the claim anyway — meta.reply_card_id is what the COCKPIT reads, and the
	// cockpit reads it from a served message, never from the write's answer.
	_, raw := postedChatRead(t, srv.URL, tok,
		`{"to":"owner","body":"an ask that also quotes","reply_to":"`+target+
			`","meta":{"reply_card_id":"rc-forged000000"}}`)
	// The chat DTO has NO top-level reply_card_id — `meta.reply_card_id` is
	// the field the cockpit reads, so that is the field asserted here.
	var msg struct {
		ID      string         `json:"id"`
		ReplyTo string         `json:"reply_to"`
		Meta    map[string]any `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("decode: %v — %s", err, raw)
	}
	cardID, _ := msg.Meta["reply_card_id"].(string)
	if msg.ReplyTo != target {
		t.Fatalf("reply_to lost when the message also carries a card id: %q (want %q) — %s",
			msg.ReplyTo, target, raw)
	}
	if cardID != "rc-forged000000" {
		t.Fatalf("meta.reply_card_id not carried alongside reply_to: %q — %s", cardID, raw)
	}
}
