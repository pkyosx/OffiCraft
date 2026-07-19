package main

// portal_test.go — behaviour lock for the management portal (portal.go) and
// the /api/latest fleet-heartbeat recording (server.go handleLatest):
// first-run claim → set-password → session, login refusals, session gating on
// every portal write/read, mint-once invite codes, promote-via-session, and
// the invite row's last-check record. Same throwaway-store httptest posture
// as server_test.go.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// portalJSON POSTs a JSON body to a portal path (token "" = no Authorization).
func (rig *testRig) portalJSON(t *testing.T, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return rig.do(req)
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON (%v): %s", err, rec.Body.String())
	}
	return body
}

// claimPortal walks the first-run flow for a rig: seed a claim token, set the
// password, return the session token.
func (rig *testRig) claimPortal(t *testing.T, password string) string {
	t.Helper()
	if err := rig.store.PutSetting(settingPortalClaimToken, "claim-secret"); err != nil {
		t.Fatalf("seed claim token: %v", err)
	}
	rec := rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"`+password+`","claim_token":"claim-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set-password: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	token, _ := decodeBody(t, rec)["token"].(string)
	if token == "" {
		t.Fatalf("set-password answered no session token: %s", rec.Body.String())
	}
	return token
}

func TestPortalFirstRunSetPassword(t *testing.T) {
	rig := newTestRig(t)

	// The first-run bit reads false before the claim…
	rec := rig.get(t, "/portal/api/status", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"password_set":false`) {
		t.Fatalf("status pre-claim: got %d %s", rec.Code, rec.Body.String())
	}

	// …a short password is refused, a wrong claim is refused…
	if err := rig.store.PutSetting(settingPortalClaimToken, "claim-secret"); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	if rec := rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"short","claim_token":"claim-secret"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: want 422, got %d", rec.Code)
	}
	if rec := rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"password-ok","claim_token":"WRONG"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong claim: want 401, got %d", rec.Code)
	}

	// …the right claim sets the password and logs straight in…
	rec = rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"password-ok","claim_token":"claim-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set-password: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	token, _ := decodeBody(t, rec)["token"].(string)
	if !strings.HasPrefix(token, "ocu-session-") {
		t.Fatalf("session token shape: got %q", token)
	}

	// …the claim token is consumed and the DB holds a PHC hash, no plaintext…
	if claim, _ := rig.store.GetSetting(settingPortalClaimToken); claim != nil {
		t.Fatalf("claim token survived its consumption")
	}
	hash, _ := rig.store.GetSetting(settingPortalPasswordHash)
	if hash == nil || !strings.HasPrefix(*hash, "$argon2id$") {
		t.Fatalf("stored password is not an argon2id PHC hash")
	}
	if strings.Contains(*hash, "password-ok") {
		t.Fatalf("plaintext password leaked into the stored hash")
	}

	// …a second claim is a 409 (already set — even with a correct-shaped body).
	if rec := rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"another-pass","claim_token":"claim-secret"}`); rec.Code != http.StatusConflict {
		t.Fatalf("re-claim: want 409, got %d", rec.Code)
	}
	// The status bit flipped.
	if rec := rig.get(t, "/portal/api/status", ""); !strings.Contains(rec.Body.String(), `"password_set":true`) {
		t.Fatalf("status post-claim: %s", rec.Body.String())
	}
}

func TestPortalLogin(t *testing.T) {
	rig := newTestRig(t)

	// No password set yet → flat 401 (no first-run hint on this path).
	if rec := rig.portalJSON(t, "/portal/api/login", "", `{"password":"whatever-123"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login pre-claim: want 401, got %d", rec.Code)
	}

	rig.claimPortal(t, "password-ok")

	if rec := rig.portalJSON(t, "/portal/api/login", "", `{"password":"WRONG-pass"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", rec.Code)
	}
	rec := rig.portalJSON(t, "/portal/api/login", "", `{"password":"password-ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	token, _ := decodeBody(t, rec)["token"].(string)
	if token == "" {
		t.Fatalf("login answered no token")
	}
	// The minted session actually opens the gated overview.
	if rec := rig.get(t, "/portal/api/overview", token); rec.Code != http.StatusOK {
		t.Fatalf("overview with fresh session: want 200, got %d", rec.Code)
	}
}

func TestPortalSessionGate(t *testing.T) {
	rig := newTestRig(t)
	rig.claimPortal(t, "password-ok") // claimed portal: the gate is live

	// Every gated route refuses no-token and garbage-token alike; the invite
	// code and publish token are NOT sessions and must not pass either.
	gated := []struct{ method, path, body string }{
		{"GET", "/portal/api/overview", ""},
		{"POST", "/portal/api/promote", `{"version":"v260101-0001"}`},
		{"POST", "/portal/api/invites", `{"name":"bob"}`},
		{"POST", "/portal/api/invites/1/revoke", `{}`},
	}
	for _, tok := range []string{"", "garbage", rig.inviteTok, rig.publishTok} {
		for _, g := range gated {
			var rec *httptest.ResponseRecorder
			if g.method == "GET" {
				rec = rig.get(t, g.path, tok)
			} else {
				rec = rig.portalJSON(t, g.path, tok, g.body)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s with token %q: want 401, got %d", g.method, g.path, tok, rec.Code)
			}
		}
	}
}

func TestPortalInviteMintAndRevoke(t *testing.T) {
	rig := newTestRig(t)
	session := rig.claimPortal(t, "password-ok")

	// Mint requires a name.
	if rec := rig.portalJSON(t, "/portal/api/invites", session, `{"name":"  "}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("nameless mint: want 422, got %d", rec.Code)
	}

	rec := rig.portalJSON(t, "/portal/api/invites", session, `{"name":"bob"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	code, _ := body["code"].(string)
	if !strings.HasPrefix(code, "ocu-inv-") {
		t.Fatalf("minted code shape: got %q", code)
	}
	id := int64(body["id"].(float64))

	// The minted code WORKS as an update-check credential immediately.
	if status, _ := rig.latestBody(t, "", code); status != http.StatusNotFound {
		t.Fatalf("fresh invite on empty catalog: want 404 (auth ok), got %d", status)
	}

	// The overview lists it WITHOUT any code material.
	rec = rig.get(t, "/portal/api/overview", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview: want 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), code) {
		t.Fatalf("overview leaked a plaintext invite code")
	}
	if !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Fatalf("overview misses the minted invite: %s", rec.Body.String())
	}

	// Revoke kills it for the API immediately; a second revoke is a 404.
	if rec := rig.portalJSON(t, "/portal/api/invites/"+jsonNum(id)+"/revoke", session, `{}`); rec.Code != http.StatusOK {
		t.Fatalf("revoke: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if status, _ := rig.latestBody(t, "", code); status != http.StatusUnauthorized {
		t.Fatalf("revoked invite: want 401, got %d", status)
	}
	if rec := rig.portalJSON(t, "/portal/api/invites/"+jsonNum(id)+"/revoke", session, `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("double revoke: want 404, got %d", rec.Code)
	}
}

func jsonNum(id int64) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}

func TestPortalPromote(t *testing.T) {
	rig := newTestRig(t)
	session := rig.claimPortal(t, "password-ok")

	payload := []byte("beta build")
	if rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", sha256hex(payload), payload)); rec.Code != http.StatusCreated {
		t.Fatalf("publish: got %d", rec.Code)
	}

	// Unknown version → 404; blank → 400.
	if rec := rig.portalJSON(t, "/portal/api/promote", session, `{"version":"v269999-0001"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("promote unknown: want 404, got %d", rec.Code)
	}
	if rec := rig.portalJSON(t, "/portal/api/promote", session, `{"version":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("promote blank: want 400, got %d", rec.Code)
	}

	// The portal promote stamps GA exactly like the publish-token route.
	rec := rig.portalJSON(t, "/portal/api/promote", session, `{"version":"v260101-0001"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody(t, rec)["channel_ga"]; got != true {
		t.Fatalf("promoted release not GA: %v", got)
	}
	if status, body := rig.latestBody(t, "", rig.inviteTok); status != http.StatusOK || body["version"] != "v260101-0001" {
		t.Fatalf("GA latest after portal promote: %d %v", status, body)
	}
}

// TestLatestRecordsFleetHeartbeat locks the /api/latest side of fleet
// monitoring: the invite row records when it checked and what it
// self-reported — including an honest blank overwrite from a client that
// stopped reporting.
func TestLatestRecordsFleetHeartbeat(t *testing.T) {
	rig := newTestRig(t)

	// Before any check the record is empty.
	invites, err := rig.store.ListInvites()
	if err != nil || len(invites) != 1 {
		t.Fatalf("ListInvites: %v %d", err, len(invites))
	}
	if invites[0].LastCheckAt != nil {
		t.Fatalf("fresh invite already has a check record")
	}

	// A check with self-report stamps time + version + sha + channel (the 404
	// empty-catalog answer still counts — the CHECK happened).
	if status, _ := rig.latestBody(t, "?channel=beta&current_version=v260101-0009&current_sha=deadbeef1", rig.inviteTok); status != http.StatusNotFound {
		t.Fatalf("latest: want 404 on empty catalog, got %d", status)
	}
	invites, _ = rig.store.ListInvites()
	got := invites[0]
	if got.LastCheckAt == nil || got.LastVersion != "v260101-0009" ||
		got.LastSHA != "deadbeef1" || got.LastChannel != "beta" {
		t.Fatalf("fleet record after check: %+v", got)
	}

	// A later check WITHOUT self-report overwrites with blanks (honest data).
	if status, _ := rig.latestBody(t, "", rig.inviteTok); status != http.StatusNotFound {
		t.Fatalf("latest: want 404, got %d", status)
	}
	invites, _ = rig.store.ListInvites()
	got = invites[0]
	if got.LastVersion != "" || got.LastSHA != "" || got.LastChannel != channelGA {
		t.Fatalf("blank self-report must overwrite: %+v", got)
	}

	// A REFUSED check (bad code) records nothing new.
	before := *got.LastCheckAt
	if status, _ := rig.latestBody(t, "", "ocu-inv-bogus"); status != http.StatusUnauthorized {
		t.Fatalf("bogus code: want 401, got %d", status)
	}
	invites, _ = rig.store.ListInvites()
	if *invites[0].LastCheckAt != before {
		t.Fatalf("a refused check must not stamp the record")
	}

	// The overview surfaces the record fields.
	session := rig.claimPortal(t, "password-ok")
	rec := rig.get(t, "/portal/api/overview", session)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"last_check_at":`) {
		t.Fatalf("overview misses fleet fields: %d %s", rec.Code, rec.Body.String())
	}
}

// TestPortalSetupURL locks the serve-banner seam: unclaimed portal → a stable
// one-shot URL; claimed portal → "".
func TestPortalSetupURL(t *testing.T) {
	rig := newTestRig(t)

	url1, err := portalSetupURL(rig.store, "127.0.0.1:8790")
	if err != nil || !strings.HasPrefix(url1, "http://127.0.0.1:8790/portal/?claim=ocu-claim-") {
		t.Fatalf("setup URL: %q %v", url1, err)
	}
	// Stable across restarts while unclaimed.
	url2, _ := portalSetupURL(rig.store, "127.0.0.1:8790")
	if url1 != url2 {
		t.Fatalf("setup URL must be stable while unclaimed: %q vs %q", url1, url2)
	}
	// Claimed → no URL.
	claim := strings.TrimPrefix(url1, "http://127.0.0.1:8790/portal/?claim=")
	if rec := rig.portalJSON(t, "/portal/api/set-password", "",
		`{"password":"password-ok","claim_token":"`+claim+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("claim via printed URL token: got %d: %s", rec.Code, rec.Body.String())
	}
	url3, err := portalSetupURL(rig.store, "127.0.0.1:8790")
	if err != nil || url3 != "" {
		t.Fatalf("claimed portal must print no setup URL: %q %v", url3, err)
	}
}

// TestPortalIndexServed — the embedded UI answers at /portal/.
func TestPortalIndexServed(t *testing.T) {
	rig := newTestRig(t)
	rec := rig.get(t, "/portal/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ocupdaterd") ||
		!strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("portal index: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// TestPortalSessionExpiry — an expired session row is a flat 401.
func TestPortalSessionExpiry(t *testing.T) {
	rig := newTestRig(t)
	rig.claimPortal(t, "password-ok")

	plaintext, hash, err := mintSecret("session")
	if err != nil {
		t.Fatalf("mintSecret: %v", err)
	}
	if err := rig.store.InsertPortalSession(hash, -1); err != nil { // born expired
		t.Fatalf("InsertPortalSession: %v", err)
	}
	if rec := rig.get(t, "/portal/api/overview", plaintext); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired session: want 401, got %d", rec.Code)
	}
}
