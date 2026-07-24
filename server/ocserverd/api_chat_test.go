package main

// api_chat_test.go — the chat surface's server-side pins for the attachment
// SEND seam: POST /api/chat/attachments (streaming upload) and post_chat's
// ref-form attachments item ({id} referencing an already-stored blob). The
// black-box wire authority stays conformance/ (test_rest_happy.py); these
// tests pin the handler semantics the happy table cannot cheaply reach
// (caps, sniff/override matrix, ref validation faces).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var testPNGBytes = append([]byte("\x89PNG\r\n\x1a\n"), []byte("conformance-ish png body")...)

func doRaw(t *testing.T, method, url, token, contentType string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func uploadBlob(t *testing.T, baseURL, token, query string, body []byte) map[string]any {
	t.Helper()
	status, resp := doRaw(t, "POST", baseURL+"/api/chat/attachments"+query, token, "", body)
	if status != 200 {
		t.Fatalf("upload: want 200, got %d %s", status, resp)
	}
	var ref map[string]any
	if err := json.Unmarshal([]byte(resp), &ref); err != nil {
		t.Fatalf("upload response is not JSON: %v %s", err, resp)
	}
	return ref
}

func TestUploadChatAttachment(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	// Happy path: named non-image, declared mime, bytes round-trip via the
	// serve route under the stored name.
	payload := []byte("%PDF-1.4 not really a pdf")
	ref := uploadBlob(t, srv.URL, agentTok,
		"?filename=report.pdf&mime=application/pdf", payload)
	id, _ := ref["id"].(string)
	if id == "" || !strings.HasPrefix(id, "att-") {
		t.Fatalf("upload must mint an att- id, got %v", ref)
	}
	if ref["mime"] != "application/pdf" || ref["filename"] != "report.pdf" {
		t.Fatalf("declared mime/filename must echo: %v", ref)
	}
	status, served := doRaw(t, "GET", srv.URL+"/api/chat/attachment/"+id, agentTok, "", nil)
	if status != 200 || served != string(payload) {
		t.Fatalf("served blob must round-trip: %d %q", status, served)
	}

	// An unnamed image sniffs its mime and defaults the pasted-image filename;
	// the request Content-Type is ignored (only ?mime= declares).
	ref = uploadBlob(t, srv.URL, agentTok, "", testPNGBytes)
	if ref["mime"] != "image/png" || ref["filename"] != "pasted-image.png" {
		t.Fatalf("sniff + filename default: %v", ref)
	}

	// An unnamed non-image stays unnamed (filename "") under octet-stream.
	ref = uploadBlob(t, srv.URL, agentTok, "", []byte("plain bytes"))
	if ref["mime"] != attachmentOctetStream || ref["filename"] != "" {
		t.Fatalf("unnamed non-image: %v", ref)
	}

	// Faults are flat 400s: empty body, >100MB body, >20MB image.
	for name, tc := range map[string]struct {
		query string
		body  []byte
		want  string
	}{
		"empty":       {"", nil, "attachment is empty"},
		"over100mb":   {"", make([]byte, chatAttachmentMaxBytes+1), "100 MB"},
		"over20mbimg": {"?mime=image/png", make([]byte, chatAttachmentImageMaxBytes+1), "20 MB"},
	} {
		status, resp := doRaw(t, "POST", srv.URL+"/api/chat/attachments"+tc.query,
			agentTok, "", tc.body)
		if status != 400 || !strings.Contains(resp, tc.want) {
			t.Fatalf("%s: want 400 %q, got %d %s", name, tc.want, status, resp)
		}
	}

	// Gated like every chat route.
	if status, _ := doRaw(t, "POST", srv.URL+"/api/chat/attachments", "", "", []byte("x")); status != 401 {
		t.Fatalf("anonymous upload must 401, got %d", status)
	}
}

// listChatRec drives HandleListChatApiChatGet directly with claims-context
// identity (the auth middleware's job) so a test can control message (ts, id)
// through the DAL — the wired-server POST path stamps its own.
func listChatRec(s *apiServer, sub string, params HandleListChatApiChatGetParams) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/chat", nil)
	claims := map[string]any{"sub": sub, "scope": "agent"}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	s.HandleListChatApiChatGet(rec, req, params)
	return rec
}

func TestListChatScrollbackCursor(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	// owner↔mira thread (caller = owner, ?with=mira); c-a/c-b collide on
	// ts=2.0 (id tie-break), plus noise from another conversation the ?with=
	// filter must exclude.
	for _, m := range []ChatMessage{
		{ID: "c-1", Sender: "mira", Recipient: "owner", TS: 1.0},
		{ID: "c-a", Sender: "owner", Recipient: "mira", TS: 2.0},
		{ID: "c-b", Sender: "mira", Recipient: "owner", TS: 2.0},
		{ID: "c-9", Sender: "owner", Recipient: "m-else", TS: 3.0},
		{ID: "c-4", Sender: "mira", Recipient: "owner", TS: 4.0},
	} {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	with := "mira"
	ids := func(rec *httptest.ResponseRecorder) []string {
		var msgs []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
			t.Fatalf("body: %v %s", err, rec.Body.String())
		}
		out := make([]string, len(msgs))
		for i, m := range msgs {
			out[i] = m.ID
		}
		return out
	}
	watermark := func() float64 {
		reads, err := s.dal.ListChatReads("owner", "mira")
		if err != nil {
			t.Fatalf("reads: %v", err)
		}
		if len(reads) == 0 {
			return 0
		}
		return reads[0].LastReadTS
	}

	// History page: 2 messages strictly older than (4.0, c-4), oldest→newest —
	// and the OWNER's read watermark for mira MUST NOT move (reading old
	// context is not reading the conversation's newest messages).
	limit := 2
	bts, bid := 4.0, "c-4"
	rec := listChatRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, Limit: &limit, BeforeTs: &bts, BeforeId: &bid,
	})
	if rec.Code != 200 {
		t.Fatalf("history page: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if got := ids(rec); len(got) != 2 || got[0] != "c-a" || got[1] != "c-b" {
		t.Fatalf("history page: want [c-a c-b], got %v", got)
	}
	if wm := watermark(); wm != 0 {
		t.Fatalf("a history page must never advance the read watermark, got %v", wm)
	}

	// The id tie-break: paging back from (2.0, c-b) keeps the equal-ts,
	// smaller-id sibling c-a and never re-serves c-b.
	bts, bid = 2.0, "c-b"
	rec = listChatRec(s, "owner", HandleListChatApiChatGetParams{
		With: &with, BeforeTs: &bts, BeforeId: &bid,
	})
	if got := ids(rec); len(got) != 2 || got[0] != "c-1" || got[1] != "c-a" {
		t.Fatalf("tie-break page: want [c-1 c-a], got %v", got)
	}
	if wm := watermark(); wm != 0 {
		t.Fatalf("tie-break history page advanced the watermark: %v", wm)
	}

	// A partial cursor is a 422 — before_ts and before_id travel together.
	rec = listChatRec(s, "owner", HandleListChatApiChatGetParams{With: &with, BeforeTs: &bts})
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "together") {
		t.Fatalf("partial cursor: want 422 'together', got %d %s", rec.Code, rec.Body.String())
	}
	rec = listChatRec(s, "owner", HandleListChatApiChatGetParams{With: &with, BeforeId: &bid})
	if rec.Code != 422 {
		t.Fatalf("partial cursor (id only): want 422, got %d", rec.Code)
	}
	if wm := watermark(); wm != 0 {
		t.Fatalf("rejected cursor requests must not touch the watermark: %v", wm)
	}

	// The cursorless list is unchanged: latest window + the auto read-receipt
	// advances the owner's watermark to the newest returned ts.
	rec = listChatRec(s, "owner", HandleListChatApiChatGetParams{With: &with})
	if rec.Code != 200 {
		t.Fatalf("plain list: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if got := ids(rec); len(got) != 4 || got[len(got)-1] != "c-4" {
		t.Fatalf("plain list: want the full thread ending c-4, got %v", got)
	}
	if wm := watermark(); wm != 4.0 {
		t.Fatalf("cursorless list must auto-mark to the newest ts, got %v", wm)
	}
}

func TestHandlePostChatApiChatPost(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	postChat := func(body string) (int, string) {
		return doRaw(t, "POST", srv.URL+"/api/chat", agentTok, "application/json", []byte(body))
	}

	// Ref form: upload first, post the light {id} ref — the message stamps the
	// STORED blob's mime/filename (a filename/mime sent alongside is ignored),
	// and a ref-only message satisfies the non-empty rule.
	ref := uploadBlob(t, srv.URL, agentTok, "?filename=data.zip&mime=application/zip",
		[]byte("zipzipzip"))
	id := ref["id"].(string)
	status, resp := postChat(fmt.Sprintf(
		`{"to":"owner","attachments":[{"id":%q,"filename":"spoofed.txt","mime":"text/plain"}]}`, id))
	if status != 200 {
		t.Fatalf("ref post: want 200, got %d %s", status, resp)
	}
	var msg struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(resp), &msg); err != nil || len(msg.Attachments) != 1 {
		t.Fatalf("ref post echo: %v %s", err, resp)
	}
	got := msg.Attachments[0]
	if got["id"] != id || got["mime"] != "application/zip" || got["filename"] != "data.zip" {
		t.Fatalf("stored blob must be authoritative over the ref's fields: %v", got)
	}

	// The same blob may ride a second message (multi-reference).
	if status, resp := postChat(fmt.Sprintf(
		`{"to":"owner","attachments":[{"id":%q}]}`, id)); status != 200 {
		t.Fatalf("re-reference: want 200, got %d %s", status, resp)
	}

	// Refs and inline items mix in one message.
	inline := `{"data_b64":"aGVsbG8=","filename":"hi.txt","mime":"text/plain"}`
	status, resp = postChat(fmt.Sprintf(
		`{"to":"owner","attachments":[{"id":%q},%s]}`, id, inline))
	if status != 200 || strings.Count(resp, `"id":"att-`) < 2 {
		t.Fatalf("mixed ref+inline: want 200 with two refs, got %d %s", status, resp)
	}

	// Fault faces: unknown ref 400; id together with data_b64 400; an item
	// with NEITHER id nor data_b64 is silently dropped (legacy tolerance), so
	// alone it leaves the message empty → 400.
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"unknown ref":  {`{"to":"owner","attachments":[{"id":"att-nope"}]}`, "'att-nope' not found"},
		"id plus data": {fmt.Sprintf(`{"to":"owner","attachments":[{"id":%q,"data_b64":"aGk="}]}`, id), "both id and data_b64"},
		"empty item":   {`{"to":"owner","attachments":[{"filename":"ghost.txt"}]}`, "text or an attachment"},
	} {
		status, resp := postChat(tc.body)
		if status != 400 || !strings.Contains(resp, tc.want) {
			t.Fatalf("%s: want 400 %q, got %d %s", name, tc.want, status, resp)
		}
	}

	// All-or-nothing: a bad ref rejects the whole message — the inline sibling
	// must NOT be stored as an orphan blob (the message was never posted, so
	// no ref can name it; pin via the gallery staying empty for this peer).
	status, resp = postChat(
		`{"to":"m-nobody","attachments":[{"data_b64":"aGVsbG8="},{"id":"att-nope"}]}`)
	if status != 400 {
		t.Fatalf("bad ref must reject the message: %d %s", status, resp)
	}
	status, resp = doRaw(t, "GET", srv.URL+"/api/chat/attachments?with=m-nobody", agentTok, "", nil)
	if status != 200 || strings.TrimSpace(resp) != "[]" {
		t.Fatalf("rejected message must leave no gallery rows: %d %s", status, resp)
	}
}

// TestChatBodySizeLimit pins the send-side body char cap (T-f8fe): the 4,000-
// CHARACTER limit an agent post_chat must respect, its actionable 400, and the
// two exemptions (owner by sender identity; the hook:* ingest path).
func TestChatBodySizeLimit(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	postBody := func(tok, body string) (int, string) {
		raw, _ := json.Marshal(map[string]any{"to": "owner", "body": body})
		if tok == ownerTok {
			raw, _ = json.Marshal(map[string]any{"to": "mira", "body": body})
		}
		return doRaw(t, "POST", srv.URL+"/api/chat", tok, "application/json", raw)
	}

	// Boundary: exactly 4,000 chars passes; 4,001 is rejected (positive control
	// pinned against the negative right beside it).
	if status, resp := postBody(agentTok, strings.Repeat("a", chatBodyMaxChars)); status != 200 {
		t.Fatalf("agent body at the 4,000 boundary must pass: %d %s", status, resp)
	}
	status, resp := postBody(agentTok, strings.Repeat("a", chatBodyMaxChars+1))
	if status != 400 {
		t.Fatalf("agent body at 4,001 must be rejected: %d %s", status, resp)
	}
	// The 400 is actionable: it states the over-limit count, the limit, and
	// points the agent at ocagent upload — no bare "too long" a retry loop hits.
	for _, want := range []string{"4001", "4000", "attachment", "ocagent upload"} {
		if !strings.Contains(resp, want) {
			t.Fatalf("over-limit 400 must contain %q, got: %s", want, resp)
		}
	}

	// Exemption ①: owner (sender == wireOwnerID) sends an over-limit body fine —
	// the system never blocks a human's message.
	if status, resp := postBody(ownerTok, strings.Repeat("a", chatBodyMaxChars+5000)); status != 200 {
		t.Fatalf("owner over-limit body must pass (exempt): %d %s", status, resp)
	}

	// Attachments are NOT counted: a short body carrying an attachment passes,
	// and the blob's own size never feeds the char cap (ref form here — the blob
	// is decoupled from the body string entirely).
	ref := uploadBlob(t, srv.URL, agentTok, "?filename=big.bin&mime=application/octet-stream",
		make([]byte, 50_000))
	refID := ref["id"].(string)
	attBody, _ := json.Marshal(map[string]any{
		"to": "owner", "body": "see attached", "attachments": []map[string]any{{"id": refID}},
	})
	if status, resp := doRaw(t, "POST", srv.URL+"/api/chat", agentTok, "application/json", attBody); status != 200 {
		t.Fatalf("short body + large attachment must pass: %d %s", status, resp)
	}

	// Multibyte is counted in CHARACTERS, not bytes: 2,000 CJK chars = 6,000
	// bytes passes (well under 4,000 chars), while 4,001 CJK chars is rejected —
	// proving utf8.RuneCountInString, not len().
	if status, resp := postBody(agentTok, strings.Repeat("中", 2000)); status != 200 {
		t.Fatalf("2,000 CJK chars (6,000 bytes) must pass — char count, not bytes: %d %s", status, resp)
	}
	if status, resp := postBody(agentTok, strings.Repeat("中", chatBodyMaxChars+1)); status != 400 {
		t.Fatalf("4,001 CJK chars must be rejected — char count enforced: %d %s", status, resp)
	}

	// Exemption ②: the hook:* ingest path is a separate handler that never
	// reaches the cap — an over-limit external payload delivers intact.
	if _, created := doJSON(t, "POST", srv.URL+"/api/members/mira/webhooks", ownerTok,
		`{"endpoint_id":"pr-events","purpose":"report PR results"}`); created["token"] == nil {
		t.Fatalf("webhook create must return a token: %v", created)
	} else {
		hookTok := created["token"].(string)
		bigPayload := strings.Repeat("x", chatBodyMaxChars+3000)
		if code, resp := postIn(t, srv.URL, hookTok, bigPayload); code != 200 {
			t.Fatalf("over-limit hook payload must be accepted (exempt): %d %s", code, resp)
		}
		// Confirm it actually landed at full length — not silently truncated or dropped.
		var delivered bool
		for _, m := range miraChatBodies(t, srv.URL, ownerTok) {
			if b, _ := m["body"].(string); utf8.RuneCountInString(b) == len(bigPayload) {
				delivered = true
			}
		}
		if !delivered {
			t.Fatalf("over-limit hook payload must deliver intact (%d chars)", len(bigPayload))
		}
	}
}

// TestPostChatQueuesRegardlessOfRecipientPresence pins the invariant the T-9c3c
// compose-gate fix rests on: POST /api/chat NEVER consults the recipient's
// presence — a message to an offline / waking / stopping member is stored and
// readable by that member on its next boot, exactly as the "queue for offline"
// design promises. The frontend used to lock the composer while a member read
// `waking`/`stopping`; this test is the backend half proving nothing was ever
// at risk of being dropped, so a future presence gate on the send path reddens.
func TestPostChatQueuesRegardlessOfRecipientPresence(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	// Control: read mira's PROJECTED presence off the roster, so each leg proves
	// it actually exercised the state it claims (a post that "works while waking"
	// is worthless if mira was never waking).
	presenceOfMira := func() string {
		_, body := get(t, srv.URL+"/api/members", ownerTok)
		var roster []map[string]any
		if err := json.Unmarshal([]byte(body), &roster); err != nil {
			t.Fatalf("roster not JSON: %v %s", err, body)
		}
		for _, m := range roster {
			if m["id"] == "mira" {
				p, _ := m["presence"].(string)
				return p
			}
		}
		t.Fatalf("mira absent from roster: %s", body)
		return ""
	}
	ownerPostToMira := func(body string) {
		status, resp := doRaw(t, "POST", srv.URL+"/api/chat", ownerTok,
			"application/json",
			[]byte(fmt.Sprintf(`{"to":"mira","body":%q}`, body)))
		if status != 200 {
			t.Fatalf("post to mira: want 200, got %d %s", status, resp)
		}
	}

	// Leg 1 — offline (a freshly seeded member holds no SSE connection).
	if got := presenceOfMira(); got != "offline" {
		t.Fatalf("leg 1 control: want mira offline, got %q", got)
	}
	ownerPostToMira("hello-offline")

	// Leg 2 — waking: owner intent online (activate) + a fresh self-reported
	// waking_since is exactly the 90s TTL window the compose bug locked on.
	if status, resp := doRaw(t, "POST", srv.URL+"/api/members/mira/activate",
		ownerTok, "application/json", []byte(`{}`)); status != 200 {
		t.Fatalf("activate mira: want 200, got %d %s", status, resp)
	}
	if status, resp := doRaw(t, "POST", srv.URL+"/api/self/waking",
		miraTok, "application/json", []byte(`{}`)); status != 200 {
		t.Fatalf("mira self/waking: want 200, got %d %s", status, resp)
	}
	if got := presenceOfMira(); got != "waking" {
		t.Fatalf("leg 2 control: want mira waking, got %q", got)
	}
	ownerPostToMira("hello-waking")

	// Leg 3 — stopped: owner-explicit stop (deactivate stamps stopping_since; not
	// online ⇒ stopped). The wind-down states the composer also used to lock.
	if status, resp := doRaw(t, "POST", srv.URL+"/api/members/mira/deactivate",
		ownerTok, "application/json", []byte(`{}`)); status != 200 {
		t.Fatalf("deactivate mira: want 200, got %d %s", status, resp)
	}
	if got := presenceOfMira(); got != "stopped" {
		t.Fatalf("leg 3 control: want mira stopped, got %q", got)
	}
	ownerPostToMira("hello-stopped")

	// The recipient reads the whole queue on wake: mira, from its OWN token,
	// sees all three regardless of the presence each was sent into.
	status, body := get(t, srv.URL+"/api/chat?with=owner", miraTok)
	if status != 200 {
		t.Fatalf("mira reads its chat: want 200, got %d %s", status, body)
	}
	for _, want := range []string{"hello-offline", "hello-waking", "hello-stopped"} {
		if !strings.Contains(body, want) {
			t.Fatalf("queued message %q missing from mira's chat: %s", want, body)
		}
	}
}
