package main

// api_signing_keys_t62_test.go — the three signing-key routes, driven as real
// HTTP requests through the production handler (buildAPIHandler), because what
// is being tested is the ROUTE: its floor, its statuses, and what its body is
// allowed to contain. Calling the handler methods directly would skip the very
// gate that makes these owner-only.
//
// The conformance auth matrix degrades the removal row to a 404 probe on
// purpose (removing a live key would revoke the harness's own credentials mid
// run — conformance/test_auth_matrix.py DEGRADED). This file is where that
// row's real semantics are pinned instead, so the degradation stays honest.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func t62Do(t *testing.T, srv *httptest.Server, method, path, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// t62Keys decodes the ring out of a response body.
func t62Keys(t *testing.T, body string) []SigningKeyDTO {
	t.Helper()
	var dto SigningKeysDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode ring: %v (body %q)", err, body)
	}
	return dto.Keys
}

// TestT62Route_ListShowsTheRingAndNeverTheKeys is the settings page's read.
func TestT62Route_ListShowsTheRingAndNeverTheKeys(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	tok := mintOwnerAt(t, keys, time.Now().Unix())
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	st, body := t62Do(t, srv, "GET", "/api/auth/signing-keys", tok)
	if st != http.StatusOK {
		t.Fatalf("owner must be able to read the ring, got %d: %s", st, body)
	}
	ring := t62Keys(t, body)
	if len(ring) != 2 {
		t.Fatalf("after one rotation the ring must show 2 keys, got %d: %s", len(ring), body)
	}
	signing := 0
	for _, k := range ring {
		if k.IsSigning {
			signing++
		}
		if k.KeyId == "" {
			t.Fatalf("every key must carry an id: %s", body)
		}
	}
	if signing != 1 {
		t.Fatalf("exactly one key must be marked as signing, got %d: %s", signing, body)
	}

	// 🔴 The leak check, and it is done against the RAW BODY rather than the
	// decoded struct on purpose: decoding into SigningKeysDTO can only see the
	// fields that type declares, so a handler that wrote an extra field would
	// be invisible to it. The raw bytes see everything that went out.
	for _, secret := range keys.verifySecrets() {
		if strings.Contains(body, string(secret)) {
			t.Fatalf("the response carries raw key material: %s", body)
		}
		if strings.Contains(body, b64uEncode(secret)) {
			t.Fatalf("the response carries base64url key material: %s", body)
		}
	}
	var loose []map[string]any
	var envelope map[string][]map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode loosely: %v", err)
	}
	loose = envelope["keys"]
	for _, k := range loose {
		for name := range k {
			switch name {
			case "key_id", "created_ts", "is_signing":
			default:
				t.Fatalf("the wire carries an undeclared field %q — a fingerprint or hash prefix added here would be an offline attack on a password-derived key: %s", name, body)
			}
		}
	}
}

// TestT62Route_RotateThroughTheAPIMovesSigningAndKeepsOldTokens drives the
// owner's button and then re-uses a credential minted BEFORE it.
func TestT62Route_RotateThroughTheAPIMovesSigningAndKeepsOldTokens(t *testing.T) {
	srv, keys, _, _ := t62Stack(t, []byte(interopSecret))
	tok := mintOwnerAt(t, keys, time.Now().Unix())
	firstID := keys.snapshot()[0].ID

	st, body := t62Do(t, srv, "POST", "/api/auth/signing-keys/rotate", tok)
	if st != http.StatusOK {
		t.Fatalf("rotate must answer 200 for the owner, got %d: %s", st, body)
	}
	ring := t62Keys(t, body)
	if len(ring) != 2 {
		t.Fatalf("rotate must ADD a key, not replace the ring: %s", body)
	}
	for _, k := range ring {
		if k.KeyId == firstID && k.IsSigning {
			t.Fatalf("the outgoing key must not still be signing: %s", body)
		}
	}
	// The token minted before the rotation was signed by the outgoing key, and
	// this very request proves it still authenticates.
	if st, body := t62Do(t, srv, "GET", "/api/auth/signing-keys", tok); st != http.StatusOK {
		t.Fatalf("a credential minted before the rotation must keep working, got %d: %s", st, body)
	}
}

// TestT62Route_RemoveRefusals: the two refusals are different statuses because
// they call for different actions.
func TestT62Route_RemoveRefusals(t *testing.T) {
	srv, keys, _, _ := t62Stack(t, []byte(interopSecret))
	tok := mintOwnerAt(t, keys, time.Now().Unix())
	activeID := keys.snapshot()[0].ID

	st, body := t62Do(t, srv, "POST", "/api/auth/signing-keys/"+activeID+"/remove", tok)
	if st != http.StatusConflict {
		t.Fatalf("removing the SIGNING key must be 409 (rotate first), got %d: %s", st, body)
	}
	if !strings.Contains(body, "rotate first") {
		t.Fatalf("the 409 must tell the caller what to do instead: %s", body)
	}
	for _, secret := range keys.verifySecrets() {
		if strings.Contains(body, b64uEncode(secret)) || strings.Contains(body, string(secret)) {
			t.Fatalf("the refusal leaks key material: %s", body)
		}
	}

	st, body = t62Do(t, srv, "POST", "/api/auth/signing-keys/k-no-such-key/remove", tok)
	if st != http.StatusNotFound {
		t.Fatalf("an unknown key id must be 404, got %d: %s", st, body)
	}
}

// TestT62Route_RemoveRetiredKeyRevokesItsTokens is the full semantics the
// conformance matrix degrades away from: the removal really happens, and the
// credential the removed key signed is refused afterwards.
func TestT62Route_RemoveRetiredKeyRevokesItsTokens(t *testing.T) {
	srv, keys, _, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()
	oldTok := mintOwnerAt(t, keys, now)
	oldID := keys.snapshot()[0].ID

	// Rotate through the API, then mint a token under the NEW key: that is the
	// credential the removal request itself must authenticate with, because the
	// old one is about to stop working.
	if st, body := t62Do(t, srv, "POST", "/api/auth/signing-keys/rotate", oldTok); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: rotate %d: %s", st, body)
	}
	newTok := mintOwnerAt(t, keys, now)

	st, body := t62Do(t, srv, "POST", "/api/auth/signing-keys/"+oldID+"/remove", newTok)
	if st != http.StatusOK {
		t.Fatalf("removing the retired key must succeed, got %d: %s", st, body)
	}
	if len(t62Keys(t, body)) != 1 {
		t.Fatalf("the answer must be the ring AFTER the removal: %s", body)
	}
	if st, _ := t62Do(t, srv, "GET", "/api/auth/signing-keys", oldTok); st != http.StatusUnauthorized {
		t.Fatalf("the credential signed by the REMOVED key must now be refused, got %d — removal is the revocation seam", st)
	}
	if st, _ := t62Do(t, srv, "GET", "/api/auth/signing-keys", newTok); st != http.StatusOK {
		t.Fatalf("the credential signed by the surviving key must be unaffected, got %d", st)
	}
}

// TestT62Route_AdminAgentIsRefused: these routes govern the key that
// authenticates the caller, so the ladder stops below owner. An admin_agent —
// the highest non-owner class, and the one that reaches most of the office's
// knobs — must be refused on all three.
func TestT62Route_AdminAgentIsRefused(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	mira := fullMember("mira-t62")
	if err := dal.PutMember(mira); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}
	adminTok, err := mintJWT(mira.ID, "agent", 3600, keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	// Positive control: this credential is genuinely accepted elsewhere, so a
	// 403 below is the ROUTE refusing it and not a broken token.
	if st, body := t62Do(t, srv, "GET", "/api/settings", adminTok); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: the admin_agent credential must work on an admin-floor route, got %d: %s", st, body)
	}

	for _, probe := range []struct{ method, path string }{
		{"GET", "/api/auth/signing-keys"},
		{"POST", "/api/auth/signing-keys/rotate"},
		{"POST", "/api/auth/signing-keys/k-anything/remove"},
	} {
		if st, body := t62Do(t, srv, probe.method, probe.path, adminTok); st != http.StatusForbidden {
			t.Fatalf("%s %s must refuse an admin_agent with 403, got %d: %s", probe.method, probe.path, st, body)
		}
	}
}
