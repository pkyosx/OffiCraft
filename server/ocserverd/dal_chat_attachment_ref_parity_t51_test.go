package main

// dal_chat_attachment_ref_parity_t51_test.go — the triggers and the backfill in
// migration 00074 are TWO COPIES of the same projection of
// chat_message.meta.attachments. Nothing makes them agree: change one and the
// other keeps its own idea of what belongs in the index, silently.
//
// 🔴 THE BACKFILL'S SQL IS READ OUT OF THE MIGRATION FILE, NOT RETYPED HERE.
// A copy of the projection written in this file would be a THIRD copy, and it
// would be the one keeping the test green while the other two drifted apart —
// the same shape as a consistency check that rebuilds its own idea of the
// world instead of asking production for it.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// backfillProjectionSQL returns the SELECT half of the migration's backfill,
// rewritten to project the same tuple the table stores. The anchors are the
// statement's own first line and its terminating semicolon.
func backfillProjectionSQL(t *testing.T) string {
	t.Helper()
	const path = "migrations/00074_chat_attachment_ref.sql"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(raw)
	const anchor = "\nSELECT m.id, j.key,"
	i := strings.Index(body, anchor)
	if i < 0 {
		t.Fatalf("%s: the backfill SELECT no longer starts with %q — this test "+
			"reads the migration so the two copies cannot drift; re-anchor it "+
			"rather than pasting the SQL in here", path, strings.TrimSpace(anchor))
	}
	stmt := body[i+1:]
	j := strings.Index(stmt, ";")
	if j < 0 {
		t.Fatalf("%s: backfill statement is not terminated", path)
	}
	return stmt[:j]
}

// indexRowKeys reads the whole index table as comparable strings.
func indexRowKeys(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(
		`SELECT ` + chatAttachmentRefColumns + ` FROM chat_attachment_ref`)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r ChatAttachmentRef
		if err := rows.Scan(&r.MessageID, &r.Ord, &r.AttachmentID,
			&r.Sender, &r.Recipient, &r.TS, &r.Mime, &r.Filename); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		out = append(out, fmt.Sprintf("%s|%d|%s|%s|%s|%v|%s|%s",
			r.MessageID, r.Ord, r.AttachmentID, r.Sender, r.Recipient, r.TS,
			r.Mime, r.Filename))
	}
	sort.Strings(out)
	return out
}

// backfillRowKeys runs the migration's own projection over the current
// chat_message rows and returns the same comparable strings.
func backfillRowKeys(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(backfillProjectionSQL(t))
	if err != nil {
		t.Fatalf("run backfill projection: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r ChatAttachmentRef
		if err := rows.Scan(&r.MessageID, &r.Ord, &r.AttachmentID,
			&r.Sender, &r.Recipient, &r.TS, &r.Mime, &r.Filename); err != nil {
			t.Fatalf("scan projection: %v", err)
		}
		out = append(out, fmt.Sprintf("%s|%d|%s|%s|%s|%v|%s|%s",
			r.MessageID, r.Ord, r.AttachmentID, r.Sender, r.Recipient, r.TS,
			r.Mime, r.Filename))
	}
	sort.Strings(out)
	return out
}

func mustAgree(t *testing.T, d *DAL, when string) {
	t.Helper()
	got, want := indexRowKeys(t, d), backfillRowKeys(t, d)
	if len(got) != len(want) {
		t.Fatalf("%s: index has %d rows, the migration's own projection says %d\n"+
			" index: %v\n backfill: %v", when, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: row %d differs\n index: %s\n backfill: %s",
				when, i, got[i], want[i])
		}
	}
}

func TestTriggersAndBackfillProjectTheSameRows(t *testing.T) {
	d := newTestDAL(t)
	mustAgree(t, d, "on an empty database")

	put := func(m ChatMessage) {
		t.Helper()
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	att := func(ids ...string) map[string]any {
		refs := make([]any, 0, len(ids))
		for _, id := range ids {
			refs = append(refs, map[string]any{
				"id": id, "mime": "image/png", "filename": id + ".png"})
		}
		return map[string]any{"attachments": refs}
	}

	put(ChatMessage{ID: "c-two", Sender: "m-1", Recipient: "owner", TS: 1,
		Meta: att("a-1", "a-2")})
	mustAgree(t, d, "after a message with two attachments")

	put(ChatMessage{ID: "c-none", Sender: "m-1", Recipient: "owner", TS: 2,
		Meta: map[string]any{}})
	mustAgree(t, d, "after a message with no attachments")

	// The upsert branch: same id, a different attachment list.
	put(ChatMessage{ID: "c-two", Sender: "m-1", Recipient: "owner", TS: 3,
		Meta: att("a-9")})
	mustAgree(t, d, "after rewriting an existing message")

	// A ref with no id is dropped by the migration on both sides.
	put(ChatMessage{ID: "c-hole", Sender: "m-1", Recipient: "owner", TS: 4,
		Meta: map[string]any{"attachments": []any{
			map[string]any{"id": ""},
			map[string]any{"id": "a-after-hole", "mime": "text/plain"},
		}}})
	mustAgree(t, d, "after a message whose first ref has no id")

	// A self-message matches BOTH single-sided read queries. It is the one
	// shape where the index and the projection could agree while the endpoint
	// still double-counts, so it is pinned on both sides: here for parity, and
	// in TestChatGalleryReturnsASelfMessageOnce for the de-duplication.
	put(ChatMessage{ID: "c-self", Sender: "m-1", Recipient: "m-1", TS: 5,
		Meta: att("a-self")})
	mustAgree(t, d, "after a self-message")

	// The same blob id twice in one message. The migration's own header says
	// (attachment_id, message_id) is NOT a safe key — this is the shape that
	// makes that true, and `ord` is the only thing keeping the two rows apart.
	put(ChatMessage{ID: "c-dup", Sender: "m-1", Recipient: "owner", TS: 6,
		Meta: att("a-dup", "a-dup")})
	mustAgree(t, d, "after one message naming the same blob twice")

	// Shapes the element guard rejects. Each of these used to make the whole
	// INSERT fail with "malformed JSON" (a string element), or land a
	// fabricated serve url in the index (a non-string id). Both sides must
	// reject them identically, so parity is the place they stay pinned.
	put(ChatMessage{ID: "c-bad", Sender: "m-1", Recipient: "owner", TS: 7,
		Meta: map[string]any{"attachments": []any{
			"a-bare-string",
			[]any{1},
			map[string]any{"id": 123},
			map[string]any{"id": "a-good", "mime": "image/png", "filename": "a-good.png"},
		}}})
	mustAgree(t, d, "after a message mixing rejected element shapes with a good one")

	put(ChatMessage{ID: "c-notarray", Sender: "m-1", Recipient: "owner", TS: 8,
		Meta: map[string]any{"attachments": "nope"}})
	mustAgree(t, d, "after a message whose attachments is not an array")

	if _, _, err := d.DeleteChatInvolving("m-1"); err != nil {
		t.Fatalf("delete chat involving: %v", err)
	}
	mustAgree(t, d, "after deleting the conversation")
}
