package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// rawErrorMessage reads the unified error envelope's message off a raw response
// body, so a refusal can be compared IN FULL. A substring probe would pass on
// any refusal that merely contains the phrase — including one raised for a
// different reason by a different gate.
func rawErrorMessage(t *testing.T, resp string) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp), &body); err != nil {
		t.Fatalf("response is not an error envelope: %v (%s)", err, resp)
	}
	return body.Error.Message
}

// T-e2b2 (owner rc-3a589dfec503, 2026-07-27): an attachment item that carries
// NEITHER id NOR data_b64 is refused — on every face that takes attachments.
//
// This lives in its own test, not in the fault table of
// TestHandlePostChatApiChatPost, because the decisive case is the one WITH body
// text: without text the message is empty anyway and the pre-T-e2b2 code
// already answered 400 (a different message, for a different reason), so a
// table row alone cannot tell the two worlds apart — and a sibling row failing
// first would abort the run before this one is even reached.
//
// What each mutant proves (measured, review finding F6 corrected the earlier
// overclaim): removing ONLY the refusal in resolveChatAttachmentInputs turns
// these red with a 400 carrying the WRONG reason ("attachment is empty");
// restoring the full pre-change shape — the refusal gone AND the chat handler's
// pre-filter back — turns the chat case red with a 200 and a posted message
// whose named file is absent, which is the defect itself. The task-message face
// has its own test (api_tasks_incomplete_attachment_test.go); it is not covered
// here.
func TestIncompleteAttachmentIsRefusedOnEveryFace(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	const ghost = `{"filename":"ghost.pdf","mime":"application/pdf"}`

	for _, tc := range []struct{ name, path, body string }{
		{"chat", "/api/chat",
			`{"to":"owner","body":"see attached","attachments":[` + ghost + `]}`},
		{"reply card", "/api/reply-cards",
			`{"kind":"decision","summary":"ship it?","options":[{"text":"yes"},{"text":"no"}],"linked_task":null,"attachments":[` + ghost + `]}`},
	} {
		// The answer face is covered by TestReplyCardAnswerRefusesIncompleteAttachment
		// below — it takes a live card id, so it cannot ride this table.
		status, resp := doRaw(t, "POST", srv.URL+tc.path, agentTok,
			"application/json", []byte(tc.body))
		if status != 400 || rawErrorMessage(t, resp) != "attachment carries neither id nor data_b64" {
			t.Errorf("%s: want 400 naming the missing id/data_b64, got %d %s",
				tc.name, status, resp)
		}
	}

	// Nothing was posted: the refusal is not a message that quietly lost its
	// attachment.
	status, resp := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", agentTok, "", nil)
	if status != 200 {
		t.Fatalf("read back the stream: %d %s", status, resp)
	}
	var stream []map[string]any
	if err := json.Unmarshal(chatEnvelopeMessages(t, []byte(resp)), &stream); err != nil {
		t.Fatalf("chat stream is not a list: %v (%s)", err, resp)
	}
	if len(stream) != 0 {
		t.Fatalf("a refused post must leave no message, got %v", stream)
	}
}

// T-e2b2 / review R2: the reply-card ANSWER face kept the pre-filter this
// ticket exists to delete — measured before the fix, the SAME input answered
// 400 on the question side and 200 on the answer side, with the named file
// simply gone from the answer. That is the ticket's founding complaint (one
// mechanism, two opposite answers) surviving on a fourth face, inside a
// function this change had already edited.
//
// The id-only case is refused with its own message rather than dropped: this
// face decodes inline bytes and has never resolved {id} refs. Whether it should
// GAIN ref support is a separate owner question, not something to smuggle in
// under a bug fix.
func TestReplyCardAnswerRefusesIncompleteAttachment(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")

	open := func() string {
		status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
			"application/json",
			[]byte(`{"kind":"decision","summary":"ship it?","options":[{"text":"yes"},{"text":"no"}],"linked_task":null}`))
		if status != 200 {
			t.Fatalf("open card: %d %s", status, resp)
		}
		return replyCardIDFromJSON(t, resp)
	}

	for _, tc := range []struct{ name, item, want string }{
		{"neither id nor bytes", `{"filename":"ghost.pdf"}`,
			"attachment carries neither id nor data_b64"},
		{"id-only ref", `{"id":"att-whatever"}`,
			"an answer attachment must carry data_b64; a stored-blob id " +
				"reference is not accepted on this face"},
		// Review U3: this face used to take the bytes, drop the id, and answer
		// 200 — while the shared schema promised a 400 for an item carrying
		// both. The last silent discard on the attachment surface.
		{"both id and bytes", `{"id":"att-whatever","data_b64":"aGVsbG8="}`,
			"attachment carries both id and data_b64"},
	} {
		card := open()
		status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards/"+card+"/answer",
			ownerTok, "application/json",
			[]byte(`{"option_idxs":[0],"text":"see attached","attachments":[`+tc.item+`]}`))
		if status != 400 || rawErrorMessage(t, resp) != tc.want {
			t.Errorf("%s: want 400 %q, got %d %s", tc.name, tc.want, status, resp)
			continue
		}
		// The card must still be answerable — a refused answer is not a
		// half-answered card.
		status, resp = doRaw(t, "GET", srv.URL+"/api/reply-cards/"+card, ownerTok, "", nil)
		var got replyCardDTO
		if status != 200 {
			t.Errorf("%s: reread card: %d %s", tc.name, status, resp)
			continue
		}
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Errorf("%s: card is not a ReplyCardDTO: %v (%s)", tc.name, err, resp)
			continue
		}
		if got.Status != replyCardStatusWaiting || got.Answer != nil {
			t.Errorf("%s: card must stay waiting after a refused answer, got %+v",
				tc.name, got)
		}
	}
}

// T-e2b2 / review W2: the 10-attachment cap on post_chat was unguarded —
// removing it left the whole suite green (conformance pins only the reply-card
// cap). This branch deliberately changed WHAT that cap counts (incomplete items
// are no longer filtered out before it), so it should not leave the cap itself
// resting on nothing.
func TestPostChatAttachmentCapIsEnforced(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	items := make([]string, 0, chatAttachmentsMaxCount+1)
	for i := 0; i <= chatAttachmentsMaxCount; i++ {
		items = append(items, `{"data_b64":"aGVsbG8="}`)
	}
	status, resp := doRaw(t, "POST", srv.URL+"/api/chat", agentTok, "application/json",
		[]byte(`{"to":"owner","body":"many","attachments":[`+strings.Join(items, ",")+`]}`))
	if status != 400 || rawErrorMessage(t, resp) != "a message may carry at most 10 attachments" {
		t.Fatalf("over-cap post must be refused, got %d %s", status, resp)
	}
	// The sentinel half: exactly at the cap is legal, so the guard is not just
	// "refuse everything with attachments".
	status, resp = doRaw(t, "POST", srv.URL+"/api/chat", agentTok, "application/json",
		[]byte(`{"to":"owner","body":"many","attachments":[`+
			strings.Join(items[:chatAttachmentsMaxCount], ",")+`]}`))
	if status != 200 {
		t.Fatalf("a post exactly at the cap must succeed, got %d %s", status, resp)
	}
}
