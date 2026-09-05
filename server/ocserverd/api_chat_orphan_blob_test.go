package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newWiredTestServerWithDB is newWiredTestServer plus the raw *sql.DB — this
// guard has to break a write and then count rows, which is exactly the handle
// the shared helper does not hand back.
func newWiredTestServerWithDB(t *testing.T) (*httptest.Server, []byte, *sql.DB) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "orphan-guard.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, db
}

// replyCardIDFromJSON reads the card id out of a create response. Hand-rolled
// index arithmetic got this wrong once already and, because the surrounding
// assertion only checked "status >= 500", the test passed while never touching
// a real card — a green that proved nothing.
func replyCardIDFromJSON(t *testing.T, body string) string {
	t.Helper()
	var card struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &card); err != nil {
		t.Fatalf("decode card: %v %s", err, body)
	}
	if !strings.HasPrefix(card.ID, "rc-") {
		t.Fatalf("card id must look like rc-…, got %q from %s", card.ID, body)
	}
	return card.ID
}

// breakWrites makes every INSERT/UPDATE on one table abort, leaving SELECTs
// working. That distinction is the whole point (review T1): the previous
// version renamed the table instead, which also broke the route's OWN lookup —
// on the answer face the 500 then came from reading the card, no attachment was
// ever decoded, and the guard could not go red for ANY implementation. A
// trigger fails the write and only the write.
func breakWrites(t *testing.T, db *sql.DB, table string) func() {
	t.Helper()
	for _, when := range []string{"INSERT", "UPDATE"} {
		stmt := fmt.Sprintf(
			`CREATE TRIGGER oc_break_%s_%s BEFORE %s ON %s
			 BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`,
			strings.ToLower(when), table, when, table)
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("arm %s trigger on %s: %v", when, table, err)
		}
	}
	return func() {
		for _, when := range []string{"INSERT", "UPDATE"} {
			if _, err := db.Exec(fmt.Sprintf(`DROP TRIGGER oc_break_%s_%s`,
				strings.ToLower(when), table)); err != nil {
				t.Fatalf("disarm %s trigger on %s: %v", when, table, err)
			}
		}
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// idsInJSONColumn collects attachment ids out of a refs column
// ([{id, mime, filename}, …]).
func idsInJSONColumn(t *testing.T, db *sql.DB, query string) map[string]string {
	t.Helper()
	out := map[string]string{}
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner, blob string
		if err := rows.Scan(&owner, &blob); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var refs []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(blob), &refs) != nil {
			continue
		}
		for _, r := range refs {
			if r.ID != "" {
				out[r.ID] = owner
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// referencedAttachmentIDs maps every attachment id NAMED by a record to a
// human description of the namer. This is the closed world the invariants are
// judged against, so an omission here is a blind spot one level up — review V2
// caught exactly that: task_artifact.attachment_id was missing, which made
// invariant (1) FALSE-POSITIVE on a blob a task artifact legitimately names.
func referencedAttachmentIDs(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for owner, q := range map[string]string{
		"chat message": `SELECT id, json_extract(meta, '$.attachments') FROM chat_message
		                 WHERE json_extract(meta, '$.attachments') IS NOT NULL`,
		"reply card question side": `SELECT id, attachments FROM reply_card`,
		"reply card answer side":   `SELECT id, answer_attachments FROM reply_card`,
	} {
		for id, rec := range idsInJSONColumn(t, db, q) {
			out[id] = owner + " " + rec
		}
	}
	rows, err := db.Query(`SELECT task_id, attachment_id FROM task_artifact
	                       WHERE attachment_id IS NOT NULL AND attachment_id != ''`)
	if err != nil {
		t.Fatalf("list task artifacts: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, attID string
		if err := rows.Scan(&taskID, &attID); err != nil {
			t.Fatalf("scan artifact: %v", err)
		}
		out[attID] = "task artifact on " + taskID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("artifact rows: %v", err)
	}
	return out
}

// assertStoreIsConsistent is the observable, replacing row counting (review U1,
// U2). Counting rows could not see two of the real defects: the answer face
// UPSERTS its card, so the row count is constant in both a correct and a broken
// world, and the create face writes TWO records (companion message AND card)
// while a single recordTable watched one of them. Both mutants passed.
//
// So assert the STATE the defects violate, not the size of a table:
//
//  1. every stored blob is named by some record   (no orphan blob)
//  2. every record's attachment ref exists        (no dangling attachment ref)
//  3. every message's reply_card_id exists        (no dangling ask — the
//     companion-message half)
//
// Every one of these is a property a user or a consumer can actually observe,
// and none of them depends on how many rows a write happens to touch.
func assertStoreIsConsistent(t *testing.T, db *sql.DB, when string) {
	t.Helper()

	referenced := referencedAttachmentIDs(t, db)

	stored := map[string]bool{}
	rows, err := db.Query(`SELECT id FROM chat_attachment`)
	if err != nil {
		t.Fatalf("list blobs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan blob: %v", err)
		}
		stored[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for id := range stored {
		if _, ok := referenced[id]; !ok {
			t.Errorf("%s — ORPHAN BLOB %s: stored but named by no record; the "+
				"only reclaim cascade walks from record refs, so it is "+
				"unreachable forever", when, id)
		}
	}
	for id, owner := range referenced {
		if !stored[id] {
			t.Errorf("%s — DANGLING REF: %s names attachment %s, which was "+
				"never written; its reader 404s on the attachment",
				when, owner, id)
		}
	}

	// The third invariant: a companion message pointing at a card that is not
	// there is a permanently unanswerable ask in the owner's stream.
	cards := map[string]bool{}
	crows, err := db.Query(`SELECT id FROM reply_card`)
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	defer crows.Close()
	for crows.Next() {
		var id string
		if err := crows.Scan(&id); err != nil {
			t.Fatalf("scan card: %v", err)
		}
		cards[id] = true
	}
	if err := crows.Err(); err != nil {
		t.Fatalf("card rows: %v", err)
	}
	mrows, err := db.Query(`SELECT id, json_extract(meta, '$.reply_card_id')
	                        FROM chat_message
	                        WHERE json_extract(meta, '$.reply_card_id') IS NOT NULL`)
	if err != nil {
		t.Fatalf("list asks: %v", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var msgID, cardID string
		if err := mrows.Scan(&msgID, &cardID); err != nil {
			t.Fatalf("scan ask: %v", err)
		}
		if cardID != "" && !cards[cardID] {
			t.Errorf("%s — DANGLING ASK: message %s points at reply card %s, "+
				"which was never written; the owner sees a question that can "+
				"never be answered", when, msgID, cardID)
		}
	}
	if err := mrows.Err(); err != nil {
		t.Fatalf("ask rows: %v", err)
	}
}

// attachmentFace is one request that carries an attachment, plus the table
// holding the record that names it.
type attachmentFace struct {
	name string
	// recordTable is the table whose WRITES get broken to simulate "the record
	// failed"; it is NOT the observable — see assertStoreIsConsistent.
	recordTable string
	// setup runs BEFORE the row baseline is taken and before any write is
	// broken, and returns whatever id send needs. Rows it writes (the card a
	// answer answers, the task a message hangs off) must not be counted as
	// damage — getting this wrong made three sub-cases fail against correct
	// code, which is the same "the setup is part of the measurement" mistake
	// the positive control exists to catch.
	setup func(t *testing.T, srv *httptest.Server, secret []byte) string
	send  func(t *testing.T, srv *httptest.Server, secret []byte, id string) (int, string)
}

func attachmentFaces() []attachmentFace {
	// TWO attachments, distinguishable (review V1): with a single one, an
	// implementation that keeps only the FIRST attachment loses data
	// SYMMETRICALLY — no orphan, no dangling ref, every invariant clean — and
	// the sweep cannot see it. Consistency is not sufficiency; the positive
	// control below asserts the declared attachments all LANDED.
	const inline = `{"data_b64":"aGVsbG8=","filename":"a.txt"},` +
		`{"data_b64":"c2Vjb25k","filename":"b.txt"}`
	post := func(path, body string, asOwner bool) func(*testing.T, *httptest.Server, []byte, string) (int, string) {
		return func(t *testing.T, srv *httptest.Server, secret []byte, _ string) (int, string) {
			now := time.Now().Unix()
			sub, scope := "mira", "agent"
			if asOwner {
				sub, scope = wireOwnerID, "owner"
			}
			tok, _ := mintJWT(sub, scope, 300, secret, now, "")
			return doRaw(t, "POST", srv.URL+path, tok, "application/json", []byte(body))
		}
	}
	return []attachmentFace{
		{
			name: "post_chat", recordTable: "chat_message",
			send: post("/api/chat",
				`{"to":"owner","body":"see attached","attachments":[`+inline+`]}`, false),
		},
		{
			name: "reply card create", recordTable: "reply_card",
			send: post("/api/reply-cards",
				`{"kind":"decision","summary":"ship it?","options":[{"text":"yes"},{"text":"no"}],"linked_task":null,"attachments":[`+inline+`]}`, false),
		},
		{
			// Review T2: this face was named as fixed but had no orphan case,
			// and reverting it to blob-then-record kept the whole suite green.
			name: "post_task_message", recordTable: "chat_message",
			setup: func(t *testing.T, srv *httptest.Server, secret []byte) string {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				status, resp := doRaw(t, "POST", srv.URL+"/api/tasks", ownerTok,
					"application/json",
					[]byte(`{"title":"orphan guard","executor_member_id":"mira"}`))
				if status != 200 {
					t.Fatalf("create task: %d %s", status, resp)
				}
				// T-91: the create response is a RECEIPT — {"task_id":…,
				// "task_no":…,"deduped":…} — not the wrapped row it used to be
				// ({"task":{…},"deduped":…}). This test only ever wanted the id.
				var created struct {
					TaskID string `json:"task_id"`
				}
				if err := json.Unmarshal([]byte(resp), &created); err != nil ||
					created.TaskID == "" {
					t.Fatalf("decode task: %v %s", err, resp)
				}
				return created.TaskID
			},
			send: func(t *testing.T, srv *httptest.Server, secret []byte, taskID string) (int, string) {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				return doRaw(t, "POST", srv.URL+"/api/tasks/"+taskID+"/message",
					ownerTok, "application/json",
					[]byte(`{"body":"see attached","attachments":[`+inline+`]}`))
			},
		},
		{
			// Review T1: the answer face's record IS the card row, so it must
			// stay readable while its write fails.
			name: "reply card answer", recordTable: "reply_card",
			setup: func(t *testing.T, srv *httptest.Server, secret []byte) string {
				now := time.Now().Unix()
				agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
				status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
					"application/json",
					[]byte(`{"kind":"decision","summary":"ship it?","options":[{"text":"yes"},{"text":"no"}],"linked_task":null}`))
				if status != 200 {
					t.Fatalf("open card: %d %s", status, resp)
				}
				return replyCardIDFromJSON(t, resp)
			},
			send: func(t *testing.T, srv *httptest.Server, secret []byte, cardID string) (int, string) {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				return doRaw(t, "POST", srv.URL+"/api/reply-cards/"+cardID+"/answer",
					ownerTok, "application/json",
					[]byte(`{"option_idxs":[0],"text":"see attached","attachments":[`+inline+`]}`))
			},
		},
	}
}

// T-e2b2 / review F1 → T1, T2, T3. This is the guard that matters: it asserts
// the OUTCOME through real HTTP rather than any spelling of the write, so it
// cannot be talked around the way the AST scan was (three times).
//
// Every face is probed in BOTH directions, because "atomic" has two failure
// modes and pinning one leaves the other free — review T3 built exactly that
// mutant (record first, blobs after) and the one-directional guard stayed green:
//
//	record write fails → no blob may survive (a blob nothing names; the only
//	                     reclaim cascade walks from record refs, so it is
//	                     unreachable forever)
//	blob write fails   → no record may survive (a record naming a blob that was
//	                     never written; its reader 404s on the attachment)
//
// Each direction carries a POSITIVE CONTROL: with nothing broken, the same
// request writes exactly one blob. Without it, "an error happened AND nothing
// bad appeared" is satisfied by a request that never reached the attachment at
// all — which is exactly how the previous answer-face guard passed while being
// unable to fail (review T1).
// assertBlobsAreReferenced pins that the store names exactly `want` distinct
// attachments across every record column the sweep reads — the "declared
// attachments all landed" half that the consistency invariants cannot express.
func assertBlobsAreReferenced(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	refs := referencedAttachmentIDs(t, db)
	if len(refs) != want {
		t.Fatalf("records name %d attachments, want %d — an attachment was "+
			"accepted and then silently dropped (the ticket's founding defect)",
			len(refs), want)
	}
}

func faceSetup(t *testing.T, face attachmentFace, srv *httptest.Server, secret []byte) string {
	t.Helper()
	if face.setup == nil {
		return ""
	}
	return face.setup(t, srv, secret)
}

func TestAttachmentWritesAreAllOrNothing(t *testing.T) {
	for _, face := range attachmentFaces() {
		t.Run(face.name, func(t *testing.T) {
			t.Run("positive control", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				before := countRows(t, db, "chat_attachment")
				status, resp := face.send(t, srv, secret, id)
				if status != 200 {
					t.Fatalf("unbroken request must succeed, got %d %s", status, resp)
				}
				if got := countRows(t, db, "chat_attachment") - before; got != 2 {
					t.Fatalf("unbroken request declared TWO attachments and stored "+
						"%d — either the failure cases below prove nothing, or "+
						"attachments are being dropped silently (review V1)", got)
				}
				assertStoreIsConsistent(t, db, "after a successful request")
				// Both blobs must be NAMED by the record too: storing them and
				// referencing one is the same silent loss seen from the other side.
				assertBlobsAreReferenced(t, db, 2)
			})

			t.Run("record write fails", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				before := countRows(t, db, "chat_attachment")
				disarm := breakWrites(t, db, face.recordTable)
				status, resp := face.send(t, srv, secret, id)
				disarm()
				if status < 500 {
					t.Fatalf("a broken record write must surface as an error, got %d %s",
						status, resp)
				}
				if got := countRows(t, db, "chat_attachment"); got != before {
					t.Errorf("chat_attachment went %d -> %d across a failed request",
						before, got)
				}
				assertStoreIsConsistent(t, db, "after a failed record write")
			})

			t.Run("blob write fails", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				disarm := breakWrites(t, db, "chat_attachment")
				status, resp := face.send(t, srv, secret, id)
				disarm()
				if status < 500 {
					t.Fatalf("a broken blob write must surface as an error, got %d %s",
						status, resp)
				}
				assertStoreIsConsistent(t, db, "after a failed blob write")
			})
		})
	}
}
