package main

// api_webhooks_test.go — M4 回呼端點 server-side pins: the owner-facing CRUD +
// the PUBLIC /in inlet's silent-accept / synthetic-chat delivery contract,
// exercised end-to-end through the wired stack (real DB, real auth gate).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func postIn(t *testing.T, baseURL, token, body string) (int, string) {
	t.Helper()
	return postInWithHeaders(t, baseURL, token, body, nil)
}

// postInWithHeaders POSTs to /in carrying arbitrary headers (the X-Slack-* /
// X-Hub-* / X-GitHub-Event platform signals the verification gate reads).
func postInWithHeaders(t *testing.T, baseURL, token, body string, headers map[string]string) (int, string) {
	t.Helper()
	url := baseURL + "/in"
	if token != "" {
		url += "?t=" + token
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// miraChatBodies returns the bodies of every message involving mira (owner view).
func miraChatBodies(t *testing.T, baseURL, token string) []map[string]any {
	t.Helper()
	status, body := get(t, baseURL+"/api/chat?with=mira", token)
	if status != 200 {
		t.Fatalf("list chat: want 200, got %d %s", status, body)
	}
	var msgs []map[string]any
	if err := json.Unmarshal([]byte(body), &msgs); err != nil {
		t.Fatalf("chat list not JSON array: %v %s", err, body)
	}
	return msgs
}

func TestHandleReceiveWebhookInPost(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	// Create an enabled endpoint on the seeded member (mira).
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/webhooks", ownerTok,
		`{"endpoint_id":"pr-events","purpose":"report PR results"}`)
	if status != 200 {
		t.Fatalf("create webhook: want 200, got %d %v", status, created)
	}
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("create must return a non-empty token: %v", created)
	}

	// Enabled token → the payload lands as ONE synthetic chat to mira, carrying
	// meta.webhook{endpoint_id,purpose} from a hook:<endpoint_id> sender.
	if code, resp := postIn(t, srv.URL, token, "PR #42 merged"); code != 200 {
		t.Fatalf("valid /in: want silent 200, got %d %s", code, resp)
	}
	msgs := miraChatBodies(t, srv.URL, ownerTok)
	if len(msgs) != 1 {
		t.Fatalf("enabled webhook must produce exactly ONE chat, got %d: %v", len(msgs), msgs)
	}
	got := msgs[0]
	if got["from"] != "hook:pr-events" || got["to"] != "mira" || got["body"] != "PR #42 merged" {
		t.Fatalf("synthetic chat shape wrong: %v", got)
	}
	meta, _ := got["meta"].(map[string]any)
	wh, _ := meta["webhook"].(map[string]any)
	if wh == nil || wh["endpoint_id"] != "pr-events" || wh["purpose"] != "report PR results" {
		t.Fatalf("meta.webhook missing/wrong: %v", meta)
	}

	// An unknown token is silently accepted (same 200) and delivers NOTHING —
	// the response never reveals whether an endpoint exists.
	if code, _ := postIn(t, srv.URL, "totally-bogus-token", "x"); code != 200 {
		t.Fatalf("unknown token must still 200 silently, got %d", code)
	}
	// A missing token likewise: silent 200, no delivery.
	if code, _ := postIn(t, srv.URL, "", "x"); code != 200 {
		t.Fatalf("missing token must 200 silently, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("unknown/missing token must deliver nothing (still 1 chat), got %d", n)
	}

	// Disable the endpoint → a call to the SAME token is now ignored.
	if code, resp := doJSON(t, "PATCH", srv.URL+"/api/members/mira/webhooks/pr-events", ownerTok,
		`{"status":"disabled"}`); code != 200 {
		t.Fatalf("disable: want 200, got %d %v", code, resp)
	}
	if code, _ := postIn(t, srv.URL, token, "should be ignored"); code != 200 {
		t.Fatalf("disabled /in: want silent 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("disabled endpoint must not deliver (still 1 chat), got %d", n)
	}

	// Re-enable → delivery resumes.
	if code, _ := doJSON(t, "PATCH", srv.URL+"/api/members/mira/webhooks/pr-events", ownerTok,
		`{"status":"enabled"}`); code != 200 {
		t.Fatalf("re-enable: want 200, got %d", code)
	}
	if code, _ := postIn(t, srv.URL, token, "second event"); code != 200 {
		t.Fatalf("re-enabled /in: want 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 2 {
		t.Fatalf("re-enabled endpoint must deliver (2 chats), got %d", n)
	}

	// Delete → permanent revocation: the token is inert.
	if code, _ := doJSON(t, "DELETE", srv.URL+"/api/members/mira/webhooks/pr-events", ownerTok, ""); code != 200 {
		t.Fatalf("delete: want 200, got %d", code)
	}
	if code, _ := postIn(t, srv.URL, token, "after delete"); code != 200 {
		t.Fatalf("deleted /in: want silent 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 2 {
		t.Fatalf("deleted endpoint must not deliver (still 2 chats), got %d", n)
	}
}

func TestWebhookCRUDValidationAndAuth(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	base := srv.URL + "/api/members/mira/webhooks"

	// Empty list to start.
	if code, body := get(t, base, ownerTok); code != 200 || strings.TrimSpace(body) != "[]" {
		t.Fatalf("initial list: want 200 [], got %d %s", code, body)
	}

	// A blank endpoint_id is a 422; a bad-charset id is a 422.
	if code, _ := doJSON(t, "POST", base, ownerTok, `{"endpoint_id":""}`); code != 422 {
		t.Fatalf("blank endpoint_id: want 422, got %d", code)
	}
	if code, _ := doJSON(t, "POST", base, ownerTok, `{"endpoint_id":"has space"}`); code != 422 {
		t.Fatalf("bad-charset endpoint_id: want 422, got %d", code)
	}

	// Create, then a duplicate id is a 409.
	if code, _ := doJSON(t, "POST", base, ownerTok, `{"endpoint_id":"deploy"}`); code != 200 {
		t.Fatalf("create deploy: want 200, got %d", code)
	}
	if code, _ := doJSON(t, "POST", base, ownerTok, `{"endpoint_id":"deploy"}`); code != 409 {
		t.Fatalf("duplicate endpoint_id: want 409, got %d", code)
	}

	// A bad status on PATCH is a 422.
	if code, _ := doJSON(t, "PATCH", base+"/deploy", ownerTok, `{"status":"paused"}`); code != 422 {
		t.Fatalf("bad status: want 422, got %d", code)
	}

	// Purpose edits persist; endpoint_id stays the address key.
	if code, data := doJSON(t, "PATCH", base+"/deploy", ownerTok, `{"purpose":"deploy hooks"}`); code != 200 ||
		data["purpose"] != "deploy hooks" {
		t.Fatalf("purpose edit: want 200 with new purpose, got %d %v", code, data)
	}

	// Unknown member → 404; unknown endpoint → 404.
	if code, _ := get(t, srv.URL+"/api/members/m-nobody/webhooks", ownerTok); code != 404 {
		t.Fatalf("unknown member list: want 404, got %d", code)
	}
	if code, _ := doJSON(t, "PATCH", base+"/ghost", ownerTok, `{"status":"disabled"}`); code != 404 {
		t.Fatalf("unknown endpoint patch: want 404, got %d", code)
	}

	// The management surface is gated: no token → 401. The public inlet is NOT.
	if code, _ := get(t, base, ""); code != 401 {
		t.Fatalf("no-token list must 401, got %d", code)
	}
	if code, _ := postIn(t, srv.URL, "anything", "x"); code != 200 {
		t.Fatalf("public /in must never gate (silent 200), got %d", code)
	}
}

// createWebhook POSTs a create with a raw JSON body and returns the parsed row.
func createWebhook(t *testing.T, baseURL, ownerTok, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "POST", baseURL+"/api/members/mira/webhooks", ownerTok, body)
}

func TestWebhookCreateRequiresSecretForSignedPlatforms(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	// slack/github without a signing_secret → 422 (cannot ever verify a call).
	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"sl","platform":"slack"}`); code != 422 {
		t.Fatalf("slack without secret: want 422, got %d", code)
	}
	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"gh","platform":"github"}`); code != 422 {
		t.Fatalf("github without secret: want 422, got %d", code)
	}
	// An unknown platform value → 422.
	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"x","platform":"discord","signing_secret":"s"}`); code != 422 {
		t.Fatalf("unknown platform: want 422, got %d", code)
	}
	// generic (default) needs no secret.
	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"gen"}`); code != 200 {
		t.Fatalf("generic create: want 200, got %d", code)
	}
	// slack WITH a secret succeeds.
	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"ok","platform":"slack","signing_secret":"s3cr3t"}`); code != 200 {
		t.Fatalf("slack with secret: want 200, got %d", code)
	}
}

func TestWebhookResponseNeverLeaksSecret(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	code, created := createWebhook(t, srv.URL, ownerTok,
		`{"endpoint_id":"gh","platform":"github","signing_secret":"top-secret-value"}`)
	if code != 200 {
		t.Fatalf("create: want 200, got %d %v", code, created)
	}
	// The response echoes platform + has_signing_secret, but NEVER the secret.
	if created["platform"] != "github" {
		t.Fatalf("platform not echoed: %v", created)
	}
	if created["has_signing_secret"] != true {
		t.Fatalf("has_signing_secret must be true: %v", created)
	}
	if _, present := created["signing_secret"]; present {
		t.Fatalf("signing_secret must NEVER appear on the wire: %v", created)
	}

	// It also must not leak through the list projection.
	_, listBody := get(t, srv.URL+"/api/members/mira/webhooks", ownerTok)
	if strings.Contains(listBody, "top-secret-value") || strings.Contains(listBody, `"signing_secret"`) {
		t.Fatalf("secret/field leaked in list: %s", listBody)
	}

	// A generic endpoint reports has_signing_secret=false, platform=generic.
	_, gen := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"gen"}`)
	if gen["platform"] != "generic" || gen["has_signing_secret"] != false {
		t.Fatalf("generic echo wrong: %v", gen)
	}
}

// webhookRow fetches one endpoint row from the owner-facing list projection —
// counter assertions read the SAME wire the member panel renders.
func webhookRow(t *testing.T, baseURL, ownerTok, endpointID string) map[string]any {
	t.Helper()
	status, body := get(t, baseURL+"/api/members/mira/webhooks", ownerTok)
	if status != 200 {
		t.Fatalf("list webhooks: want 200, got %d %s", status, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("webhook list not JSON array: %v %s", err, body)
	}
	for _, r := range rows {
		if r["endpoint_id"] == endpointID {
			return r
		}
	}
	t.Fatalf("endpoint %q not in list: %v", endpointID, rows)
	return nil
}

func rowNum(t *testing.T, row map[string]any, key string) float64 {
	t.Helper()
	f, ok := row[key].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number: %v", key, row)
	}
	return f
}

func TestWebhookCountersTrackDeliveredAndDroppedPerReason(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	signingSecret := "slack-signing-secret"
	_, created := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"cnt","platform":"slack","signing_secret":%q}`, signingSecret))
	token, _ := created["token"].(string)

	// A fresh endpoint reads all-zero: never received, nothing counted.
	row := webhookRow(t, srv.URL, ownerTok, "cnt")
	if rowNum(t, row, "last_received_ts") != 0 || rowNum(t, row, "delivered_count") != 0 ||
		rowNum(t, row, "dropped_count") != 0 || row["last_drop_reason"] != "" {
		t.Fatalf("fresh endpoint must read all-zero counters: %v", row)
	}

	// A failed signature → dropped_count+1, last_drop_reason=sig_failed, and
	// last_received_ts stamped (the drop still proves the caller reached us).
	before := float64(time.Now().Unix()) - 1
	eventBody := `{"type":"event_callback"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())
	if code, _ := postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         "v0=deadbeef",
		"X-Slack-Request-Timestamp": ts,
	}); code != 200 {
		t.Fatalf("bad sig: want silent 200, got %d", code)
	}
	row = webhookRow(t, srv.URL, ownerTok, "cnt")
	if rowNum(t, row, "dropped_count") != 1 || row["last_drop_reason"] != "sig_failed" {
		t.Fatalf("failed signature must count dropped=1 reason=sig_failed: %v", row)
	}
	if rowNum(t, row, "delivered_count") != 0 {
		t.Fatalf("failed signature must NOT count delivered: %v", row)
	}
	if rowNum(t, row, "last_received_ts") < before {
		t.Fatalf("a dropped call must still stamp last_received_ts: %v", row)
	}

	// A verified event → delivered_count+1 (dropped stays 1).
	sig := slackSign(signingSecret, ts, eventBody)
	if code, _ := postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         sig,
		"X-Slack-Request-Timestamp": ts,
	}); code != 200 {
		t.Fatalf("signed event: want 200, got %d", code)
	}
	row = webhookRow(t, srv.URL, ownerTok, "cnt")
	if rowNum(t, row, "delivered_count") != 1 || rowNum(t, row, "dropped_count") != 1 {
		t.Fatalf("verified delivery must count delivered=1 (dropped still 1): %v", row)
	}

	// A disabled endpoint → dropped_count+1, last_drop_reason=disabled.
	if code, _ := doJSON(t, "PATCH", srv.URL+"/api/members/mira/webhooks/cnt", ownerTok,
		`{"status":"disabled"}`); code != 200 {
		t.Fatalf("disable: want 200, got %d", code)
	}
	if code, _ := postIn(t, srv.URL, token, "ignored"); code != 200 {
		t.Fatalf("disabled /in: want silent 200, got %d", code)
	}
	row = webhookRow(t, srv.URL, ownerTok, "cnt")
	if rowNum(t, row, "dropped_count") != 2 || row["last_drop_reason"] != "disabled" {
		t.Fatalf("disabled drop must count dropped=2 reason=disabled: %v", row)
	}
	if rowNum(t, row, "delivered_count") != 1 {
		t.Fatalf("disabled drop must NOT touch delivered: %v", row)
	}
}

// TestWebhookInResponseIndistinguishableAcrossOutcomes pins the 防探測 iron
// rule the counters must NOT weaken: every /in outcome except the Slack
// url_verification handshake answers the SAME status + byte-identical body —
// a probing caller learns nothing from delivered vs dropped vs unknown token.
func TestWebhookInResponseIndistinguishableAcrossOutcomes(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	signingSecret := "slack-signing-secret"
	_, created := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"probe","platform":"slack","signing_secret":%q}`, signingSecret))
	token, _ := created["token"].(string)

	eventBody := `{"type":"event_callback"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())
	goodSig := slackSign(signingSecret, ts, eventBody)

	type outcome struct {
		name string
		code int
		body string
	}
	var outcomes []outcome
	collect := func(name string, code int, body string) {
		outcomes = append(outcomes, outcome{name, code, body})
	}

	code, body := postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         goodSig,
		"X-Slack-Request-Timestamp": ts,
	})
	collect("verified+delivered", code, body)
	code, body = postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         "v0=deadbeef",
		"X-Slack-Request-Timestamp": ts,
	})
	collect("sig_failed", code, body)
	code, body = postIn(t, srv.URL, "totally-bogus-token", eventBody)
	collect("unknown token", code, body)
	code, body = postIn(t, srv.URL, "", eventBody)
	collect("missing token", code, body)
	if c, _ := doJSON(t, "PATCH", srv.URL+"/api/members/mira/webhooks/probe", ownerTok,
		`{"status":"disabled"}`); c != 200 {
		t.Fatalf("disable: want 200, got %d", c)
	}
	code, body = postIn(t, srv.URL, token, eventBody)
	collect("disabled", code, body)

	ref := outcomes[0]
	for _, o := range outcomes[1:] {
		if o.code != ref.code || o.body != ref.body {
			t.Fatalf("/in outcome %q must be indistinguishable from %q: got %d %q vs %d %q",
				o.name, ref.name, o.code, o.body, ref.code, ref.body)
		}
	}
	if ref.code != 200 {
		t.Fatalf("silent face must be 200, got %d", ref.code)
	}
}

// webhookRequests fetches one endpoint's /in debug ring buffer (owner wire).
func webhookRequests(t *testing.T, baseURL, tok, memberID, endpointID string) (int, []map[string]any) {
	t.Helper()
	status, body := get(t, baseURL+"/api/members/"+memberID+"/webhooks/"+endpointID+"/requests", tok)
	if status != 200 {
		return status, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("requests not a JSON array: %v %s", err, body)
	}
	return status, rows
}

func TestWebhookRequestLogRecordsEveryOutcome(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	// One slack endpoint walks challenge → sig_failed → delivered → disabled.
	signingSecret := "slack-signing-secret"
	_, created := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"log","platform":"slack","signing_secret":%q}`, signingSecret))
	token, _ := created["token"].(string)

	postIn(t, srv.URL, token, `{"type":"url_verification","challenge":"c1"}`)
	eventBody := `{"type":"event_callback"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())
	postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         "v0=deadbeef",
		"X-Slack-Request-Timestamp": ts,
	})
	postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         slackSign(signingSecret, ts, eventBody),
		"X-Slack-Request-Timestamp": ts,
	})
	doJSON(t, "PATCH", srv.URL+"/api/members/mira/webhooks/log", ownerTok, `{"status":"disabled"}`)
	postIn(t, srv.URL, token, "while disabled")

	_, rows := webhookRequests(t, srv.URL, ownerTok, "mira", "log")
	var outcomes []string
	for _, r := range rows {
		outcomes = append(outcomes, r["outcome"].(string))
	}
	want := []string{"dropped:disabled", "delivered", "dropped:sig_failed", "challenge"}
	if fmt.Sprint(outcomes) != fmt.Sprint(want) {
		t.Fatalf("outcomes newest→oldest: want %v, got %v", want, outcomes)
	}
	// Each row carries the raw body and the JSON-serialised headers.
	if rows[0]["body"] != "while disabled" {
		t.Fatalf("dropped:disabled row must carry the raw body: %v", rows[0])
	}
	var hdrs map[string][]string
	if err := json.Unmarshal([]byte(rows[1]["headers"].(string)), &hdrs); err != nil {
		t.Fatalf("headers must be a JSON header map: %v %v", err, rows[1]["headers"])
	}
	if got := hdrs["X-Slack-Request-Timestamp"]; len(got) != 1 || got[0] != ts {
		t.Fatalf("delivered row must record the request headers: %v", hdrs)
	}

	// A verified GitHub ping logs outcome=ping.
	ghSecret := "github-webhook-secret"
	_, ghCreated := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"logping","platform":"github","signing_secret":%q}`, ghSecret))
	ghToken, _ := ghCreated["token"].(string)
	pingBody := `{"zen":"z"}`
	postInWithHeaders(t, srv.URL, ghToken, pingBody, map[string]string{
		"X-Hub-Signature-256": githubSign(ghSecret, pingBody),
		"X-GitHub-Event":      "ping",
	})
	_, ghRows := webhookRequests(t, srv.URL, ownerTok, "mira", "logping")
	if len(ghRows) != 1 || ghRows[0]["outcome"] != "ping" {
		t.Fatalf("verified ping must log one outcome=ping row: %v", ghRows)
	}

	// member_gone: an endpoint whose member was dismissed still logs the drop
	// (read via the DAL — the owner route 404s once the member is gone).
	code, hired := doJSON(t, "POST", srv.URL+"/api/members", ownerTok, `{"name":"loggone"}`)
	if code != 200 {
		t.Fatalf("hire: want 200, got %d %v", code, hired)
	}
	goneID, _ := hired["id"].(string)
	_, goneCreated := doJSON(t, "POST", srv.URL+"/api/members/"+goneID+"/webhooks", ownerTok,
		`{"endpoint_id":"gone"}`)
	goneToken, _ := goneCreated["token"].(string)
	if code, _ := doJSON(t, "DELETE", srv.URL+"/api/members/"+goneID, ownerTok, ""); code != 200 {
		t.Fatalf("dismiss: want 200, got %d", code)
	}
	if code, _ := postIn(t, srv.URL, goneToken, "into the void"); code != 200 {
		t.Fatalf("member-gone /in: want silent 200, got %d", code)
	}
	status, _ := webhookRequests(t, srv.URL, ownerTok, goneID, "gone")
	if status != 404 {
		t.Fatalf("requests of a dismissed member must 404, got %d", status)
	}
}

func TestWebhookRequestLogKeepsOnlyNewestFive(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	_, created := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"ring"}`)
	token, _ := created["token"].(string)
	for i := 1; i <= 7; i++ {
		if code, _ := postIn(t, srv.URL, token, fmt.Sprintf("event %d", i)); code != 200 {
			t.Fatalf("/in %d: want 200, got %d", i, code)
		}
	}
	_, rows := webhookRequests(t, srv.URL, ownerTok, "mira", "ring")
	if len(rows) != 5 {
		t.Fatalf("ring buffer must keep EXACTLY 5 rows after 7 calls, got %d", len(rows))
	}
	for i, r := range rows { // newest first: event 7 … event 3
		want := fmt.Sprintf("event %d", 7-i)
		if r["body"] != want {
			t.Fatalf("row %d: want body %q (newest first), got %q", i, want, r["body"])
		}
	}
}

func TestWebhookRequestLogMarksTruncation(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	_, created := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"trunc"}`)
	token, _ := created["token"].(string)

	// A small body is stored verbatim, truncated=false.
	postIn(t, srv.URL, token, "small")
	_, rows := webhookRequests(t, srv.URL, ownerTok, "mira", "trunc")
	if rows[0]["body"] != "small" || rows[0]["truncated"] != false {
		t.Fatalf("small body must be verbatim + truncated=false: %v", rows[0])
	}

	// An oversized body (>16 KiB) is cut at the cap and marked truncated.
	big := strings.Repeat("x", 20<<10)
	postIn(t, srv.URL, token, big)
	_, rows = webhookRequests(t, srv.URL, ownerTok, "mira", "trunc")
	body, _ := rows[0]["body"].(string)
	if len(body) != 16<<10 || rows[0]["truncated"] != true {
		t.Fatalf("oversized body must cut at 16 KiB + truncated=true: len=%d trunc=%v",
			len(body), rows[0]["truncated"])
	}

	// Oversized HEADERS (>4 KiB serialised) likewise mark truncated.
	postInWithHeaders(t, srv.URL, token, "hdr probe", map[string]string{
		"X-Big-Header": strings.Repeat("h", 5<<10),
	})
	_, rows = webhookRequests(t, srv.URL, ownerTok, "mira", "trunc")
	headers, _ := rows[0]["headers"].(string)
	if len(headers) != 4<<10 || rows[0]["truncated"] != true {
		t.Fatalf("oversized headers must cut at 4 KiB + truncated=true: len=%d trunc=%v",
			len(headers), rows[0]["truncated"])
	}
}

func TestWebhookRequestsRouteRequiresOwner(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	if code, _ := createWebhook(t, srv.URL, ownerTok, `{"endpoint_id":"rbac"}`); code != 200 {
		t.Fatalf("create: want 200, got %d", code)
	}
	url := srv.URL + "/api/members/mira/webhooks/rbac/requests"

	if code, _ := get(t, url, ""); code != 401 {
		t.Fatalf("anonymous: want 401, got %d", code)
	}
	// mira's agent token is ADMIN_AGENT class (role assistant) — still below
	// the owner floor: raw payload debug data never reaches any agent.
	adminTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	if code, _ := get(t, url, adminTok); code != 403 {
		t.Fatalf("admin agent: want 403, got %d", code)
	}
	// A plain agent (fresh hire, no privileged role) is likewise a flat 403.
	_, hired := doJSON(t, "POST", srv.URL+"/api/members", ownerTok, `{"name":"rbacpeer"}`)
	peerID, _ := hired["id"].(string)
	peerTok, _ := mintJWT(peerID, "agent", 300, secret, now, "")
	if code, _ := get(t, url, peerTok); code != 403 {
		t.Fatalf("plain agent: want 403, got %d", code)
	}
	if code, rows := webhookRequests(t, srv.URL, ownerTok, "mira", "rbac"); code != 200 || len(rows) != 0 {
		t.Fatalf("owner: want 200 with an empty ring, got %d %v", code, rows)
	}
}

func TestWebhookSlackVerification(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	signingSecret := "slack-signing-secret"
	_, created := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"slk","platform":"slack","purpose":"slack events","signing_secret":%q}`, signingSecret))
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("create must mint a token: %v", created)
	}

	// url_verification handshake → synchronous challenge echo, NO chat delivery.
	code, body := postIn(t, srv.URL, token, `{"type":"url_verification","challenge":"chal-xyz"}`)
	if code != 200 {
		t.Fatalf("challenge: want 200, got %d", code)
	}
	var chal map[string]any
	if err := json.Unmarshal([]byte(body), &chal); err != nil || chal["challenge"] != "chal-xyz" {
		t.Fatalf("challenge must be echoed synchronously, got %s", body)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 0 {
		t.Fatalf("url_verification must NOT deliver a chat, got %d", n)
	}

	// A correctly signed event → delivered.
	eventBody := `{"type":"event_callback"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := slackSign(signingSecret, ts, eventBody)
	code, _ = postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         sig,
		"X-Slack-Request-Timestamp": ts,
	})
	if code != 200 {
		t.Fatalf("signed slack event: want 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("a valid signature must deliver exactly 1 chat, got %d", n)
	}

	// A bad signature → silent 200, NO new delivery (still 1).
	code, _ = postInWithHeaders(t, srv.URL, token, eventBody, map[string]string{
		"X-Slack-Signature":         "v0=deadbeef",
		"X-Slack-Request-Timestamp": ts,
	})
	if code != 200 {
		t.Fatalf("bad slack signature: want silent 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("a failed signature must NOT deliver (still 1 chat), got %d", n)
	}
}

func TestWebhookGithubVerification(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	signingSecret := "github-webhook-secret"
	_, created := createWebhook(t, srv.URL, ownerTok,
		fmt.Sprintf(`{"endpoint_id":"ghk","platform":"github","signing_secret":%q}`, signingSecret))
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("create must mint a token: %v", created)
	}

	body := `{"action":"opened","number":7}`
	sig := githubSign(signingSecret, body)

	// A correctly signed push/PR event → delivered.
	code, _ := postInWithHeaders(t, srv.URL, token, body, map[string]string{
		"X-Hub-Signature-256": sig,
		"X-GitHub-Event":      "pull_request",
	})
	if code != 200 {
		t.Fatalf("signed github event: want 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("a valid github signature must deliver 1 chat, got %d", n)
	}

	// A bad signature → silent 200, no delivery.
	code, _ = postInWithHeaders(t, srv.URL, token, body, map[string]string{
		"X-Hub-Signature-256": "sha256=deadbeef",
		"X-GitHub-Event":      "pull_request",
	})
	if code != 200 {
		t.Fatalf("bad github signature: want silent 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("a failed github signature must NOT deliver (still 1), got %d", n)
	}

	// A verified `ping` event is ack'd (200) but NOT forwarded as a chat.
	pingBody := `{"zen":"Keep it logically awesome."}`
	pingSig := githubSign(signingSecret, pingBody)
	code, _ = postInWithHeaders(t, srv.URL, token, pingBody, map[string]string{
		"X-Hub-Signature-256": pingSig,
		"X-GitHub-Event":      "ping",
	})
	if code != 200 {
		t.Fatalf("github ping: want 200, got %d", code)
	}
	if n := len(miraChatBodies(t, srv.URL, ownerTok)); n != 1 {
		t.Fatalf("a verified ping must NOT deliver a chat (still 1), got %d", n)
	}
}
