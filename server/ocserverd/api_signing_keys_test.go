// Skeleton generated from server/ocserverd/api_signing_keys.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http"
	"testing"
)

func TestSigningKeysDTOReturnsTheWholeRingOldestFirst(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	api.keys = newKeyring([]signingKey{
		{ID: "k-old", Key: []byte("old secret"), CreatedTS: 0},
		{ID: "k-new", Key: []byte("new secret"), CreatedTS: 42},
	}, "k-new")

	got := api.signingKeysDTO()
	apiWantBody(t, apiHelpersWire(t, got), map[string]any{
		"keys": []any{
			map[string]any{"key_id": "k-old", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": "k-new", "created_ts": 42, "is_signing": true},
		},
	})
}

// apiRing reads the ring back through the route that serves it.
func apiRing(t *testing.T, h http.Handler, credential string) []any {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", credential, "")
	if status != 200 {
		t.Fatalf("read the ring: %d %v", status, data)
	}
	keys, ok := data["keys"].([]any)
	if !ok {
		t.Fatalf("the ring answer carries no keys array: %v", data)
	}
	return keys
}

func apiWantRing(t *testing.T, h http.Handler, credential string, want ...map[string]any) {
	t.Helper()
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(t, "ring", any(apiRing(t, h, credential)), any(expected))
}

// apiRotateSigningKey rotates through the real route and answers the id of the
// key that now signs.
func apiRotateSigningKey(t *testing.T, h http.Handler, credential string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", credential, "")
	if status != 200 {
		t.Fatalf("rotate: %d %v", status, data)
	}
	keys, _ := data["keys"].([]any)
	for _, entry := range keys {
		key, _ := entry.(map[string]any)
		if signing, _ := key["is_signing"].(bool); signing {
			id, _ := key["key_id"].(string)
			return id
		}
	}
	t.Fatalf("the rotated ring names no signing key: %v", data)
	return ""
}

// apiOwnerCredential exchanges the owner password at the login route for a
// credential signed by whichever key signs NOW.
func apiOwnerCredential(t *testing.T, h http.Handler) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"`+apiTestOwnerPassword+`"}`)
	if status != 200 {
		t.Fatalf("login: %d %v", status, data)
	}
	credential, _ := data["token"].(string)
	if credential == "" {
		t.Fatalf("login minted nothing: %v", data)
	}
	return credential
}

// apiCredentialLives asks a gated row this credential is entitled to and
// reports whether the gate still accepts it.
func apiCredentialLives(t *testing.T, h http.Handler, credential string) bool {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/global-context", credential, "")
	switch status {
	case 200:
		return true
	case 401:
		apiWantError(t, data, "unauthorized", "invalid token")
		return false
	}
	t.Fatalf("want 200 or 401, got %d (%v)", status, data)
	return false
}

func TestHandleSigningKeysApiAuthSigningKeysGet(t *testing.T) {
	t.Run("an install that has never rotated lists exactly one key, the one that signs, with no creation time on record", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": true},
		}})
		dashboard.wantFrames()
	})

	t.Run("after a rotation both keys list oldest first with only the newest signing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		}})
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because how the owner authenticates is never an agent's business", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
	})

	t.Run("an ignored request body does not change the signing key ring", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/auth/signing-keys", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": true},
		}})
	})
}

func TestHandleSigningKeyRotateApiAuthSigningKeysRotatePost(t *testing.T) {
	t.Run("rotating adds a key, hands signing to it, and answers the whole ring the list route then serves", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": apiAnyString, "created_ts": apiAnyNumber, "is_signing": true},
		}})
		dashboard.wantFrames()

		minted, _ := data["keys"].([]any)[1].(map[string]any)
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": minted["key_id"], "created_ts": minted["created_ts"], "is_signing": true},
		)
	})

	t.Run("a credential signed by the outgoing key keeps working after the rotation, and so does one minted after it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		beforehand := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		apiRotateSigningKey(t, h, owner)

		if !apiCredentialLives(t, h, beforehand) {
			t.Fatal("rotation revokes nothing: a credential signed by the outgoing key must still verify")
		}
		if !apiCredentialLives(t, h, apiTestAgentToken(t, api, apiTestPlainAgentID, "")) {
			t.Fatal("a credential minted after the rotation must verify")
		}
		if !apiCredentialLives(t, h, apiOwnerCredential(t, h)) {
			t.Fatal("the owner must be able to sign in again after a rotation")
		}
	})

	t.Run("rotating twice leaves all three keys in the ring with only the newest signing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		first := apiRotateSigningKey(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": first, "created_ts": apiAnyNumber, "is_signing": false},
			map[string]any{"key_id": apiAnyString, "created_ts": apiAnyNumber, "is_signing": true},
		}})
		dashboard.wantFrames()
	})

	t.Run("a request carrying no credentials answers 401 and the ring is untouched", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": true})
	})

	t.Run("an authenticated agent identity answers 403 and the ring is untouched", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": true})
	})

	t.Run("an ignored request body does not change key rotation", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/rotate", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": apiAnyString, "created_ts": apiAnyNumber, "is_signing": true},
		}})
	})
}

func TestHandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost(t *testing.T) {
	t.Run("removing a retired key drops it from the ring and instantly voids every credential it signed, while the rest keep working", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		signedByTheOldKey := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		rotated := apiRotateSigningKey(t, h, owner)
		signedByTheNewKey := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		operator := apiOwnerCredential(t, h)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", operator, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		}})
		dashboard.wantFrames()

		if apiCredentialLives(t, h, signedByTheOldKey) {
			t.Fatal("removing a key must void the credentials it signed, with no grace period")
		}
		if !apiCredentialLives(t, h, signedByTheNewKey) {
			t.Fatal("a credential signed by the surviving key must keep working")
		}
		apiWantRing(t, h, operator,
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true})
	})

	t.Run("a share link minted under the removed key resolves right up to the removal and answers 401 after it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		before, after := apiDiffSeededSides(t, h, owner)
		minted := apiDiffShareQuery(t, h, owner, before, after, "", "")

		apiRotateSigningKey(t, h, owner)
		if status, data := apiJSON(t, h, "GET", "/api/diff?"+minted, "", ""); status != 200 {
			t.Fatalf("a rotation must not void a minted link: %d %v", status, data)
		}

		operator := apiOwnerCredential(t, h)
		if status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", operator, ""); status != 200 {
			t.Fatalf("remove: %d %v", status, data)
		}

		status, data := apiJSON(t, h, "GET", "/api/diff?"+minted, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid signature")
	})

	t.Run("the credential that removes the key it was itself signed under is answered, and is dead on the next request", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		}})
		dashboard.wantFrames()

		status, data = apiJSON(t, h, "GET", "/api/auth/signing-keys", owner, "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid token")
		apiWantRing(t, h, apiOwnerCredential(t, h),
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true})
	})

	t.Run("removing the key that currently signs is refused 409 telling the owner to rotate first, and the ring is untouched", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"key 'k-legacy' is the one currently signing and cannot be removed — rotate first, then remove it")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": true})
		if !apiCredentialLives(t, h, apiTestAgentToken(t, api, apiTestPlainAgentID, "")) {
			t.Fatal("a refused removal must leave the ring minting and verifying")
		}
	})

	t.Run("an id the ring does not hold answers 404 naming the id from the path, and the ring is untouched", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-nosuchkey/remove", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "no signing key 'k-nosuchkey'")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		)
	})

	t.Run("a request carrying no credentials answers 401 and the key it named is still in the ring", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)
		signedByTheOldKey := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		)
		if !apiCredentialLives(t, h, signedByTheOldKey) {
			t.Fatal("a refused removal must revoke nothing")
		}
	})

	t.Run("an authenticated agent identity answers 403 and cannot remove the key its own credential is signed under", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/"+rotated+"/remove", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		apiWantRing(t, h, owner,
			map[string]any{"key_id": "k-legacy", "created_ts": 0, "is_signing": false},
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		)
		if !apiCredentialLives(t, h, housekeeper) {
			t.Fatal("a refused removal must revoke nothing")
		}
	})

	t.Run("an ignored request body does not change removal of a retired key", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		rotated := apiRotateSigningKey(t, h, owner)

		status, data := apiJSON(t, h, "POST", "/api/auth/signing-keys/k-legacy/remove", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"keys": []any{
			map[string]any{"key_id": rotated, "created_ts": apiAnyNumber, "is_signing": true},
		}})
	})
}
