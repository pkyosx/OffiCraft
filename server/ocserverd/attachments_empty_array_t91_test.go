package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The two create receipts NAME the empty attachment list; they never drop the key.
//
// 🔴 WHY THIS FILE EXISTS AT ALL. T-91 gave chatPostReceiptDTO and
// replyCardCreateReceiptDTO an `attachments` field. Both were written with
// `,omitempty` first, which means a post that carried no files answers with no
// `attachments` KEY — and nothing anywhere reddened. Measured: putting
// `omitempty` back on those two receipts (and on the read face's answer DTO)
// left all ten attachment / card-create Go tests green, rc=0. So the honest
// empty array had ZERO guards on it. This file is that guard.
//
// The convention it defends is the station's, stated most plainly by
// conformance/test_scheduled_messages.py: a field that appears only sometimes
// forces every reader to distinguish "this post carried no files" from "this
// server does not report files", which are answers to two different questions.
// A cockpit reading the second as the first draws an attachment strip that is
// empty rather than absent, or vice versa, and neither it nor its tests can
// tell — the mock returns whole objects by construction.
//
// 🔴 IT ASSERTS ON THE RAW JSON, NOT ON A DECODED STRUCT, AND THAT IS THE
// WHOLE POINT. Decoding into the receipt struct maps BOTH "key absent" and
// "key present, value []" onto the same zero value — a nil slice — so a struct
// comparison cannot see the difference this test exists to see. Reading
// map[string]json.RawMessage keeps them apart: absence is a missing map entry,
// the empty array is the two bytes `[]`.

// assertAttachmentsIsAnEmptyArray reads the raw body and states both halves:
// the key is THERE, and its value is the empty array rather than null.
func assertAttachmentsIsAnEmptyArray(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", what, rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("%s: decode: %v (%s)", what, err, rec.Body.String())
	}
	value, present := raw["attachments"]
	if !present {
		t.Fatalf("%s: the receipt dropped the `attachments` key. An absent key and "+
			"an empty list are two different answers, and a reader cannot tell "+
			"\"no files on this one\" from \"this server does not report files\": %s",
			what, rec.Body.String())
	}
	if got := string(value); got != "[]" {
		t.Fatalf("%s: attachments = %s, want the empty array `[]` — null and `[]` "+
			"are as different to a reader as absence is: %s",
			what, got, rec.Body.String())
	}
}

func TestChatPostReceiptNamesTheEmptyAttachmentList(t *testing.T) {
	api := newTasksTestServer(t)
	putMemberRow(t, api, "m-exec", KindStaff, "")

	rec := httptest.NewRecorder()
	api.HandlePostChatApiChatPost(rec, taskReq(t, "POST", "/api/chat", map[string]any{
		"to":   "m-exec",
		"body": "no files on this one",
	}, "owner", "owner"))
	assertAttachmentsIsAnEmptyArray(t, rec, "POST /api/chat")

	// Anti-vacuity: the receipt really is the bounded one, so a build that
	// answered the whole message (which also carries attachments) could not
	// satisfy the assertion above by accident.
	assertReceiptKeys(t, rec, "id", "ts", "attachments")
}

func TestReplyCardCreateReceiptNamesTheEmptyAttachmentList(t *testing.T) {
	api := newTasksTestServer(t)

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind":        "decision",
		"summary":     "no files on this one either",
		"options":     []map[string]any{{"text": "go"}, {"text": "hold"}},
		"linked_task": nil,
	})
	assertAttachmentsIsAnEmptyArray(t, rec, "POST /api/reply-cards")

	assertReceiptKeys(t, rec, "id", "chat_message_id", "created_ts", "attachments")
}
