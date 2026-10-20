package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

const (
	passkeyTestHost         = "localhost:8787"
	passkeyTestOrigin       = "http://localhost:8787"
	passkeyTestDomain       = "office.example.com"
	passkeyTestDomainOrigin = "https://office.example.com"
	passkeyTestDeviceName   = "Eva 的 MacBook"
)

var passkeyTestIDShape = regexp.MustCompile(`^pk-[0-9a-f]{12}$`)

const (
	softFlagUP = 0x01
	softFlagUV = 0x04
	softFlagAT = 0x40
)

// softPasskey is a software authenticator: an ES256 key answering create() and
// get() the way a platform authenticator does, with "none" attestation.
type softPasskey struct {
	key     *ecdsa.PrivateKey
	credID  []byte
	handle  []byte
	counter uint32
}

func newSoftPasskey(t *testing.T, label string) *softPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return &softPasskey{key: key, credID: []byte("soft-credential-" + label)}
}

func b64url(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func softJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return raw
}

func (a *softPasskey) authData(t *testing.T, rpID string, counter uint32, attested bool) []byte {
	t.Helper()
	rpHash := sha256.Sum256([]byte(rpID))
	flags := byte(softFlagUP | softFlagUV)
	if attested {
		flags |= softFlagAT
	}
	out := append([]byte{}, rpHash[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, counter)
	if !attested {
		return out
	}
	pub, err := a.key.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}
	point := pub.Bytes()
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:65]})
	if err != nil {
		t.Fatalf("cbor cose key: %v", err)
	}
	out = append(out, make([]byte, 16)...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(a.credID)))
	out = append(out, a.credID...)
	return append(out, cose...)
}

func softClientData(t *testing.T, kind, challenge, origin string) []byte {
	t.Helper()
	return softJSON(t, map[string]any{
		"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false,
	})
}

// create answers navigator.credentials.create() for options, at origin.
func (a *softPasskey) create(t *testing.T, options map[string]any, origin string) json.RawMessage {
	t.Helper()
	rpID, _ := options["rp"].(map[string]any)["id"].(string)
	userID, _ := options["user"].(map[string]any)["id"].(string)
	handle, err := base64.RawURLEncoding.DecodeString(userID)
	if err != nil {
		t.Fatalf("user.id is not base64url: %q", userID)
	}
	a.handle = handle
	challenge, _ := options["challenge"].(string)
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(t, rpID, a.counter, true),
	})
	if err != nil {
		t.Fatalf("cbor attestation: %v", err)
	}
	return softJSON(t, map[string]any{
		"id":                      b64url(a.credID),
		"rawId":                   b64url(a.credID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"clientExtensionResults":  map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64url(softClientData(t, "webauthn.create", challenge, origin)),
			"attestationObject": b64url(attestation),
			"transports":        []string{"hybrid", "internal"},
		},
	})
}

// get answers navigator.credentials.get() for options, at origin, with the
// signature counter advanced by one.
func (a *softPasskey) get(t *testing.T, options map[string]any, origin string) json.RawMessage {
	t.Helper()
	a.counter++
	return a.getWithCounter(t, options, origin, a.counter)
}

func (a *softPasskey) getWithCounter(t *testing.T, options map[string]any, origin string, counter uint32) json.RawMessage {
	t.Helper()
	rpID, _ := options["rpId"].(string)
	challenge, _ := options["challenge"].(string)
	authData := a.authData(t, rpID, counter, false)
	clientData := softClientData(t, "webauthn.get", challenge, origin)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}
	return softJSON(t, map[string]any{
		"id":                      b64url(a.credID),
		"rawId":                   b64url(a.credID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"clientExtensionResults":  map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64url(clientData),
			"authenticatorData": b64url(authData),
			"signature":         b64url(sig),
			"userHandle":        b64url(a.handle),
		},
	})
}

type passkeyTestEnv struct {
	api   *apiServer
	h     http.Handler
	d     *DAL
	owner string
}

func newPasskeyTestEnv(t *testing.T) passkeyTestEnv {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	return passkeyTestEnv{api: api, h: h, d: d, owner: owner}
}

func passkeyTestBeginRegistration(t *testing.T, env passkeyTestEnv, host string) (string, map[string]any) {
	t.Helper()
	status, begun := apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/begin", host, env.owner,
		`{"current_password":"`+apiTestOwnerPassword+`"}`)
	if status != 200 {
		t.Fatalf("register/begin: want 200, got %d (%v)", status, begun)
	}
	id, _ := begun["ceremony_id"].(string)
	options, _ := begun["options"].(map[string]any)
	return id, options
}

func passkeyTestFinishRegistration(t *testing.T, env passkeyTestEnv, host, ceremonyID string, credential json.RawMessage, name string) (int, map[string]any) {
	t.Helper()
	return apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/finish", host, env.owner,
		string(softJSON(t, map[string]any{"ceremony_id": ceremonyID, "credential": credential, "name": name})))
}

// passkeyTestRegister walks register/begin → the authenticator →
// register/finish and answers the stored passkey.
func passkeyTestRegister(t *testing.T, env passkeyTestEnv, host, origin string, a *softPasskey) map[string]any {
	t.Helper()
	id, options := passkeyTestBeginRegistration(t, env, host)
	status, data := passkeyTestFinishRegistration(t, env, host, id, a.create(t, options, origin), passkeyTestDeviceName)
	if status != 200 {
		t.Fatalf("register/finish: want 200, got %d (%v)", status, data)
	}
	return data
}

func passkeyTestBeginLogin(t *testing.T, env passkeyTestEnv, host string) (string, map[string]any) {
	t.Helper()
	status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkey-login/begin", host, "", "")
	if status != 200 {
		t.Fatalf("passkey-login/begin: want 200, got %d (%v)", status, data)
	}
	id, _ := data["ceremony_id"].(string)
	options, _ := data["options"].(map[string]any)
	return id, options
}

func passkeyTestFinishLogin(t *testing.T, env passkeyTestEnv, host, ceremonyID string, credential json.RawMessage) (int, map[string]any) {
	t.Helper()
	return apiJSONAt(t, env.h, "POST", "/api/auth/passkey-login/finish", host, "",
		string(softJSON(t, map[string]any{"ceremony_id": ceremonyID, "credential": credential})))
}

func passkeyTestSignIn(t *testing.T, env passkeyTestEnv, host, origin string, a *softPasskey) (int, map[string]any) {
	t.Helper()
	id, options := passkeyTestBeginLogin(t, env, host)
	return passkeyTestFinishLogin(t, env, host, id, a.get(t, options, origin))
}

func passkeyTestWantRefused(t *testing.T, status int, data map[string]any) {
	t.Helper()
	if status != 401 {
		t.Fatalf("want 401, got %d (%v)", status, data)
	}
	apiWantError(t, data, "unauthorized", "passkey sign-in failed")
}

func passkeyTestWantID(t *testing.T, data map[string]any) string {
	t.Helper()
	id, _ := data["id"].(string)
	if !passkeyTestIDShape.MatchString(id) {
		t.Fatalf("id: want pk- plus 12 hex, got %q", id)
	}
	return id
}

var passkeyTestOwnerToken = map[string]any{
	"token":      apiAnyString,
	"token_type": "bearer",
	"expires_in": 86400,
	"owner_id":   "owner",
}

func TestHandleListPasskeysApiAuthPasskeysGet(t *testing.T) {
	t.Run("a registered passkey is listed with its name, binding and times and no key material", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, newSoftPasskey(t, "list"))

		status, data := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"passkeys": []any{map[string]any{
			"id":               apiAnyString,
			"name":             "Eva 的 MacBook",
			"rp_id":            "localhost",
			"origin":           "http://localhost:8787",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     nil,
			"usable_here":      true,
			"clone_warning_ts": nil,
		}}})
		passkeyTestWantID(t, data["passkeys"].([]any)[0].(map[string]any))
	})

	t.Run("a sign-in stamps last_used_ts", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "used")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		if status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a); status != 200 {
			t.Fatalf("sign-in: want 200, got %d (%v)", status, data)
		}

		_, data := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		apiWantBody(t, data, map[string]any{"passkeys": []any{map[string]any{
			"id":               apiAnyString,
			"name":             "Eva 的 MacBook",
			"rp_id":            "localhost",
			"origin":           "http://localhost:8787",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     apiAnyNumber,
			"usable_here":      true,
			"clone_warning_ts": nil,
		}}})
	})

	t.Run("a passkey registered on another host is listed as not usable here", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		passkeyTestRegister(t, env, passkeyTestDomain, passkeyTestDomainOrigin, newSoftPasskey(t, "elsewhere"))

		status, data := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"passkeys": []any{map[string]any{
			"id":               apiAnyString,
			"name":             "Eva 的 MacBook",
			"rp_id":            "office.example.com",
			"origin":           "https://office.example.com",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     nil,
			"usable_here":      false,
			"clone_warning_ts": nil,
		}}})
	})
}

func TestHandleBeginPasskeyRegistrationApiAuthPasskeysRegisterBeginPost(t *testing.T) {
	t.Run("the current password on localhost answers creation options bound to localhost", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/begin", passkeyTestHost, env.owner,
			`{"current_password":"`+apiTestOwnerPassword+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"ceremony_id": apiAnyString,
			"expires_in":  300,
			"options": map[string]any{
				"challenge": apiAnyString,
				"rp":        map[string]any{"id": "localhost", "name": "OffiCraft"},
				"user":      map[string]any{"id": apiAnyString, "name": "owner", "displayName": "owner"},
				"pubKeyCredParams": []any{
					map[string]any{"type": "public-key", "alg": -7},
					map[string]any{"type": "public-key", "alg": -35},
					map[string]any{"type": "public-key", "alg": -36},
					map[string]any{"type": "public-key", "alg": -257},
					map[string]any{"type": "public-key", "alg": -258},
					map[string]any{"type": "public-key", "alg": -259},
					map[string]any{"type": "public-key", "alg": -37},
					map[string]any{"type": "public-key", "alg": -38},
					map[string]any{"type": "public-key", "alg": -39},
					map[string]any{"type": "public-key", "alg": -8},
				},
				"timeout": 300000,
				"authenticatorSelection": map[string]any{
					"requireResidentKey": true,
					"residentKey":        "required",
					"userVerification":   "required",
				},
				"attestation": "none",
			},
		})
	})

	t.Run("a wrong current password answers 401", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/begin", passkeyTestHost, env.owner,
			`{"current_password":"not-the-password"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password")
	})

	t.Run("an IP-address host answers 400 naming localhost", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/begin", "127.0.0.1:8787", env.owner,
			`{"current_password":"`+apiTestOwnerPassword+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"passkeys cannot be used on an IP address; open this site at http://localhost:8787 instead")
	})

	t.Run("a passkey already registered on this host is excluded from the options", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "excluded")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		passkeyTestRegister(t, env, passkeyTestDomain, passkeyTestDomainOrigin, newSoftPasskey(t, "other-site"))

		_, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)
		apiWantValue(t, "excludeCredentials", options["excludeCredentials"], []any{map[string]any{
			"type":       "public-key",
			"id":         b64url([]byte("soft-credential-excluded")),
			"transports": []any{"hybrid", "internal"},
		}})
	})

	t.Run("an owner holding 20 passkeys answers 409", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		for i := 0; i < 20; i++ {
			passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, newSoftPasskey(t, "limit-"+string(rune('a'+i))))
		}

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkeys/register/begin", passkeyTestHost, env.owner,
			`{"current_password":"`+apiTestOwnerPassword+`"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "the owner already has 20 passkeys, the limit; delete one first")
	})
}

func TestHandleFinishPasskeyRegistrationApiAuthPasskeysRegisterFinishPost(t *testing.T) {
	t.Run("a valid credential answers the stored passkey", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "finish")
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestDomain)

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestDomain, id,
			a.create(t, options, passkeyTestDomainOrigin), "  Eva 的 MacBook  ")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               apiAnyString,
			"name":             "Eva 的 MacBook",
			"rp_id":            "office.example.com",
			"origin":           "https://office.example.com",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     nil,
			"usable_here":      true,
			"clone_warning_ts": nil,
		})
		passkeyTestWantID(t, data)
	})

	t.Run("a ceremony finished once answers 400 the second time", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)
		if status, data := passkeyTestFinishRegistration(t, env, passkeyTestHost, id,
			newSoftPasskey(t, "first").create(t, options, passkeyTestOrigin), "first"); status != 200 {
			t.Fatalf("first finish: want 200, got %d (%v)", status, data)
		}

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestHost, id,
			newSoftPasskey(t, "second").create(t, options, passkeyTestOrigin), "second")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "ceremony_id is unknown, expired, or already used")
	})

	t.Run("a ceremony begun on another host answers 400 and stores nothing", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestDomain, id,
			newSoftPasskey(t, "moved").create(t, options, passkeyTestOrigin), "moved")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "this registration was begun at http://localhost:8787; finish it there")
		_, list := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		apiWantBody(t, list, map[string]any{"passkeys": []any{}})
	})

	t.Run("a blank name answers 400", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestHost, id,
			newSoftPasskey(t, "blank").create(t, options, passkeyTestOrigin), "   ")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "name must not be blank")
	})

	t.Run("a credential signed for another origin answers 400", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestHost, id,
			newSoftPasskey(t, "phished").create(t, options, "http://localhost:9999"), "phished")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "credential failed verification")
	})

	t.Run("the same authenticator registered twice answers 409", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "twice")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		id, options := passkeyTestBeginRegistration(t, env, passkeyTestHost)

		status, data := passkeyTestFinishRegistration(t, env, passkeyTestHost, id,
			a.create(t, options, passkeyTestOrigin), "again")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "this authenticator already holds a passkey for this server")
	})
}

func TestHandleDeletePasskeyApiAuthPasskeysPasskeyIdDelete(t *testing.T) {
	t.Run("a deleted passkey is answered back and can no longer sign in", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "deleted")
		id := passkeyTestWantID(t, passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a))

		status, data := apiJSONAt(t, env.h, "DELETE", "/api/auth/passkeys/"+id, passkeyTestHost, env.owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":               id,
			"name":             "Eva 的 MacBook",
			"rp_id":            "localhost",
			"origin":           "http://localhost:8787",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     nil,
			"usable_here":      true,
			"clone_warning_ts": nil,
		})
		status, data = passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a)
		passkeyTestWantRefused(t, status, data)
		_, list := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		apiWantBody(t, list, map[string]any{"passkeys": []any{}})
	})

	t.Run("an unknown id answers 404", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "DELETE", "/api/auth/passkeys/pk-000000000000", passkeyTestHost, env.owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "passkey 'pk-000000000000' not found")
	})
}

func TestHandleBeginPasskeyLoginApiAuthPasskeyLoginBeginPost(t *testing.T) {
	t.Run("a request answers discoverable request options requiring user verification", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkey-login/begin", passkeyTestDomain, "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"ceremony_id": apiAnyString,
			"expires_in":  300,
			"options": map[string]any{
				"challenge":        apiAnyString,
				"timeout":          300000,
				"rpId":             "office.example.com",
				"userVerification": "required",
			},
		})
	})

	t.Run("an IP-address host answers 400 naming localhost", func(t *testing.T) {
		env := newPasskeyTestEnv(t)

		status, data := apiJSONAt(t, env.h, "POST", "/api/auth/passkey-login/begin", "127.0.0.1:8787", "", "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"passkeys cannot be used on an IP address; open this site at http://localhost:8787 instead")
	})

	t.Run("a server with no signing secret answers 401 naming the configuration", func(t *testing.T) {
		_, h, _, _ := newAPITestStackWithoutSigningSecret(t)

		status, data := apiJSONAt(t, h, "POST", "/api/auth/passkey-login/begin", passkeyTestHost, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "auth not configured")
	})

	t.Run("the 65th ceremony in flight drops the oldest one", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "crowded")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		oldestID, oldest := passkeyTestBeginLogin(t, env, passkeyTestHost)
		secondID, second := passkeyTestBeginLogin(t, env, passkeyTestHost)
		for i := 0; i < 63; i++ {
			passkeyTestBeginLogin(t, env, passkeyTestHost)
		}

		status, data := passkeyTestFinishLogin(t, env, passkeyTestHost, oldestID, a.get(t, oldest, passkeyTestOrigin))
		passkeyTestWantRefused(t, status, data)
		status, data = passkeyTestFinishLogin(t, env, passkeyTestHost, secondID, a.get(t, second, passkeyTestOrigin))
		if status != 200 {
			t.Fatalf("second-oldest ceremony: want 200, got %d (%v)", status, data)
		}
	})
}

func TestHandleFinishPasskeyLoginApiAuthPasskeyLoginFinishPost(t *testing.T) {
	t.Run("a registered passkey answers an owner token that opens an owner route", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "login")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)

		status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, passkeyTestOwnerToken)
		token, _ := data["token"].(string)
		status, mfa := apiJSONAt(t, env.h, "GET", "/api/auth/mfa", passkeyTestHost, token, "")
		if status != 200 {
			t.Fatalf("owner route with the passkey token: want 200, got %d (%v)", status, mfa)
		}
		apiWantBody(t, mfa, map[string]any{"offered": false, "enrolled": false, "secret": nil, "otpauth_uri": nil})
	})

	t.Run("an armed TOTP factor is not asked for", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "totp")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		apiTestArmMFA(t, env.api, env.d)

		status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, passkeyTestOwnerToken)
	})

	t.Run("a ceremony begun on another host answers 401", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "hop")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		id, options := passkeyTestBeginLogin(t, env, passkeyTestHost)

		status, data := passkeyTestFinishLogin(t, env, passkeyTestDomain, id, a.get(t, options, passkeyTestOrigin))
		passkeyTestWantRefused(t, status, data)
	})

	t.Run("a ceremony used once answers 401 the second time", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "replay")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		id, options := passkeyTestBeginLogin(t, env, passkeyTestHost)
		if status, data := passkeyTestFinishLogin(t, env, passkeyTestHost, id, a.get(t, options, passkeyTestOrigin)); status != 200 {
			t.Fatalf("first finish: want 200, got %d (%v)", status, data)
		}

		status, data := passkeyTestFinishLogin(t, env, passkeyTestHost, id, a.get(t, options, passkeyTestOrigin))
		passkeyTestWantRefused(t, status, data)
	})

	t.Run("a sign counter that went backwards answers 401 and marks the passkey", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "cloned")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		a.counter = 4
		if status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a); status != 200 {
			t.Fatalf("sign-in at counter 5: want 200, got %d (%v)", status, data)
		}

		id, options := passkeyTestBeginLogin(t, env, passkeyTestHost)
		status, data := passkeyTestFinishLogin(t, env, passkeyTestHost, id, a.getWithCounter(t, options, passkeyTestOrigin, 3))
		passkeyTestWantRefused(t, status, data)
		rows, err := env.d.ListPasskeys()
		if err != nil || len(rows) != 1 {
			t.Fatalf("ListPasskeys: %v %v", rows, err)
		}
		if rows[0].CloneWarningTS == nil || rows[0].SignCount != 5 {
			t.Fatalf("want a clone warning and the counter kept at 5, got warning=%v count=%d",
				rows[0].CloneWarningTS, rows[0].SignCount)
		}
		_, list := apiJSONAt(t, env.h, "GET", "/api/auth/passkeys", passkeyTestHost, env.owner, "")
		apiWantBody(t, list, map[string]any{"passkeys": []any{map[string]any{
			"id":               apiAnyString,
			"name":             "Eva 的 MacBook",
			"rp_id":            "localhost",
			"origin":           "http://localhost:8787",
			"created_ts":       apiAnyNumber,
			"last_used_ts":     apiAnyNumber,
			"usable_here":      true,
			"clone_warning_ts": apiAnyNumber,
		}}})
	})

	t.Run("a passkey registered on another host answers 401 even when its signature is good", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "bound")
		passkeyTestRegister(t, env, passkeyTestDomain, passkeyTestDomainOrigin, a)

		status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a)
		passkeyTestWantRefused(t, status, data)
	})

	t.Run("an unknown ceremony answers 401", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		a := newSoftPasskey(t, "lost")
		passkeyTestRegister(t, env, passkeyTestHost, passkeyTestOrigin, a)
		_, options := passkeyTestBeginLogin(t, env, passkeyTestHost)

		status, data := passkeyTestFinishLogin(t, env, passkeyTestHost, "not-a-ceremony", a.get(t, options, passkeyTestOrigin))
		passkeyTestWantRefused(t, status, data)
	})

	t.Run("a refusal raises no password-exposed alert", func(t *testing.T) {
		env := newPasskeyTestEnv(t)
		deliveries := make(chan int, 4)
		env.api.authAlertDeliver = func(count int) { deliveries <- count }
		a := newSoftPasskey(t, "quiet")
		passkeyTestRegister(t, env, passkeyTestDomain, passkeyTestDomainOrigin, a)

		for i := 0; i < 3; i++ {
			status, data := passkeyTestSignIn(t, env, passkeyTestHost, passkeyTestOrigin, a)
			passkeyTestWantRefused(t, status, data)
		}
		select {
		case got := <-deliveries:
			t.Fatalf("a passkey refusal raised the alert (count %d)", got)
		case <-time.After(200 * time.Millisecond):
		}

		// Positive control on the same server: the first password-ok/code-wrong
		// refusal alerts with a count of exactly 1, so nothing above was counted.
		apiTestArmMFA(t, env.api, env.d)
		status, data := apiJSON(t, env.h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("control login: want 401, got %d (%v)", status, data)
		}
		select {
		case got := <-deliveries:
			if got != 1 {
				t.Fatalf("control alert count: want 1, got %d", got)
			}
		case <-time.After(time.Second):
			t.Fatal("control: the password-exposed alert did not fire at all, so the silence above proves nothing")
		}
	})
}
