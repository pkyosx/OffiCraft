package main

// api_chat_gallery_index_t51_test.go — GET /api/chat/attachments now answers
// from chat_attachment_ref (migration 00074) instead of scanning chat_message.
//
// The endpoint had exactly TWO assertions before this file, and both of them
// only ever asserted an EMPTY gallery ("a rejected message must leave no
// rows"). An empty list is identical under both implementations, so the whole
// positive behaviour — what comes back, in what order, from which side of the
// conversation — was unguarded: the handler could have been swapped for one
// that returns anything non-empty and nothing would have gone red.
//
// Rows are seeded by writing chat_message DIRECTLY, never through post_chat.
// That is deliberate twice over: it pins the exact (ts, id) tie-breaks the
// order claims, AND it means the index rows can only have come from the
// triggers — no Go write path is involved, so these tests would fail if the
// hook were quietly moved back into application code.

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func seedGalleryMessage(t *testing.T, db *sql.DB, id, sender, recipient string, ts float64, meta string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
		 VALUES (?, ?, ?, '', ?, ?)`, id, sender, recipient, ts, meta); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func galleryIDs(t *testing.T, srv, token, peer string) []string {
	t.Helper()
	status, body := doRaw(t, "GET", srv+"/api/chat/attachments?with="+peer, token, "", nil)
	if status != 200 {
		t.Fatalf("gallery: want 200, got %d %s", status, body)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("gallery body is not JSON: %v %s", err, body)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func equalIDs(a, b []string) bool {
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

func TestChatGalleryServesBothSidesInStreamOrder(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, err := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}

	// Newest first. c-b and c-c share a ts on purpose: the tie-break is the
	// message id ASCENDING, which is the order the pre-index handler produced
	// (whole table read in (ts, id) ASC, then a STABLE sort on ts DESC).
	seedGalleryMessage(t, db, "c-a", "mira", "owner", 300,
		`{"attachments":[{"id":"att-a1","mime":"image/png","filename":"a1.png"}]}`)
	seedGalleryMessage(t, db, "c-b", "owner", "mira", 200,
		`{"attachments":[{"id":"att-b1","mime":"text/plain","filename":"b1.txt"},
		                 {"id":"att-b2","mime":"text/plain","filename":"b2.txt"}]}`)
	seedGalleryMessage(t, db, "c-c", "mira", "owner", 200,
		`{"attachments":[{"id":"att-c1","mime":"image/png","filename":"c1.png"}]}`)
	// Not mira's conversation at all — the peer filter must exclude it.
	seedGalleryMessage(t, db, "c-d", "owner", "kyle", 400,
		`{"attachments":[{"id":"att-d1","mime":"image/png","filename":"d1.png"}]}`)

	want := []string{"att-a1", "att-b1", "att-b2", "att-c1"}
	if got := galleryIDs(t, srv.URL, tok, "mira"); !equalIDs(got, want) {
		t.Fatalf("gallery order/contents wrong:\n got %v\nwant %v", got, want)
	}
}

func TestChatGalleryDropsAMessageWhoseIndexRowIsGone(t *testing.T) {
	// The discriminator between "reads the index" and "reads chat_message".
	// chat_message is left untouched, so an implementation that scans it (the
	// one this ticket replaced) still returns the row and this test goes red.
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, err := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	seedGalleryMessage(t, db, "c-a", "mira", "owner", 300,
		`{"attachments":[{"id":"att-a1","mime":"image/png","filename":"a1.png"}]}`)
	seedGalleryMessage(t, db, "c-b", "mira", "owner", 200,
		`{"attachments":[{"id":"att-b1","mime":"image/png","filename":"b1.png"}]}`)

	if got := galleryIDs(t, srv.URL, tok, "mira"); !equalIDs(got, []string{"att-a1", "att-b1"}) {
		t.Fatalf("precondition: want both rows first, got %v", got)
	}
	if _, err := db.Exec(`DELETE FROM chat_attachment_ref WHERE message_id = 'c-a'`); err != nil {
		t.Fatal(err)
	}
	var stillThere int
	if err := db.QueryRow(
		`SELECT count(*) FROM chat_message WHERE id = 'c-a'`).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 1 {
		t.Fatalf("this test only discriminates while the MESSAGE survives, got %d", stillThere)
	}
	if got := galleryIDs(t, srv.URL, tok, "mira"); !equalIDs(got, []string{"att-b1"}) {
		t.Fatalf("gallery must answer from the index, not chat_message: got %v", got)
	}
}

func TestChatGalleryReturnsASelfMessageOnce(t *testing.T) {
	// A self-message matches BOTH single-sided queries. There are none in live
	// data today and nothing rejects one, so the de-duplication is guarded here
	// rather than assumed.
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, err := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	seedGalleryMessage(t, db, "c-self", "mira", "mira", 100,
		`{"attachments":[{"id":"att-s1","mime":"image/png","filename":"s1.png"}]}`)

	if got := galleryIDs(t, srv.URL, tok, "mira"); !equalIDs(got, []string{"att-s1"}) {
		t.Fatalf("a self-message must appear exactly once, got %v", got)
	}
}

func TestChatGalleryRowCarriesTheWholeSnapshotNotJustTheID(t *testing.T) {
	// Every field below is SNAPSHOTTED into chat_attachment_ref by the trigger
	// rather than read from chat_message at query time. The other tests in this
	// file compare id lists only, so a trigger that wrote sender into recipient
	// would leave all three of them green while the panel credited every image
	// to the wrong member. Assert the whole projection, from the one row.
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, err := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO member (id, name, kind) VALUES ('owner', 'Owner', 'assistant')`); err != nil {
		t.Fatal(err)
	}
	// Asymmetric on purpose: mira is the RECIPIENT here, so a swap is visible.
	seedGalleryMessage(t, db, "c-a", "owner", "mira", 300,
		`{"attachments":[{"id":"att-a1","mime":"image/png","filename":"a1.png"}]}`)

	status, body := doRaw(t, "GET", srv.URL+"/api/chat/attachments?with=mira", tok, "", nil)
	if status != 200 {
		t.Fatalf("gallery: want 200, got %d %s", status, body)
	}
	var rows []chatGalleryEntryDTO
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("gallery body is not JSON: %v %s", err, body)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly one row, got %d: %s", len(rows), body)
	}
	got, want := rows[0], chatGalleryEntryDTO{
		ID:        "att-a1",
		URL:       "/api/chat/attachment/att-a1",
		Filename:  "a1.png",
		Mime:      "image/png",
		IsImage:   true,
		MessageID: "c-a",
		From:      "owner",
		FromName:  "Owner",
		To:        "mira",
		TS:        300,
	}
	if got != want {
		t.Fatalf("gallery row projection wrong:\n got %+v\nwant %+v", got, want)
	}
}
