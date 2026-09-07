// Skeleton generated from server/ocserverd/api_auth.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMintWardenToken(t *testing.T) {
	t.Skip("TODO: mintWardenToken mints the permanent machine credential used only by warden installation paths.")
}

func TestHandleLoginApiLoginPost(t *testing.T) {
	t.Run("the owner password answers 200 and a bearer owner token", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 86400,
			"owner_id":   "owner",
		})
	})

	t.Run("the minted token opens an owner-gated row", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		_, minted := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		token, _ := minted["token"].(string)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", token, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     false,
			"enrolled":    false,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("the owner password plus a live code answers 200 while a factor is armed", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 86400,
			"owner_id":   "owner",
		})
	})

	t.Run("a request missing `password` answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: password")
	})

	t.Run("a request on a server with no signing secret answers 401 naming the configuration", func(t *testing.T) {
		_, h, _, _ := newAPITestStackWithoutSigningSecret(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "auth not configured")
	})

	t.Run("a wrong password answers 401 naming neither factor", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"not-the-password"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a password with no password ever set answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"anything-at-all"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("the right password with a wrong code answers 401 with the same refusal", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a storage fault while the code is being spent answers 500 rather than letting the login through", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)
		code := apiTestTOTPCode(t, secret)
		if err := d.wdb.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`","code":"`+code+`"}`)
		if status != 500 {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", "internal error: sql: database is closed")
	})

	t.Run("attempts beyond the in-flight cap answer 429 with a one-second Retry-After", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		api.credentialFailureFloor = 2 * time.Second

		type answer struct {
			code       int
			retryAfter string
			body       string
		}
		answers := make(chan answer, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"wrong"}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				answers <- answer{rec.Code, rec.Header().Get("Retry-After"), rec.Body.String()}
			}()
		}
		wg.Wait()
		close(answers)
		throttled := 0
		for got := range answers {
			switch got.code {
			case 429:
				throttled++
				if got.retryAfter != "1" {
					t.Fatalf("Retry-After: want \"1\", got %q", got.retryAfter)
				}
				if got.body != `{"error":{"code":"client_error","message":"too many failed credential attempts; retry in 1s"}}` {
					t.Fatalf("throttled body: %q", got.body)
				}
			case 401:
				if got.body != `{"error":{"code":"unauthorized","message":"invalid password or code"}}` {
					t.Fatalf("refused body: %q", got.body)
				}
			default:
				t.Fatalf("want 401 or 429, got %d (%s)", got.code, got.body)
			}
		}
		if throttled != 4 {
			t.Fatalf("want 4 of 8 concurrent attempts refused for concurrency, got %d", throttled)
		}
	})
}

func TestHandleMintApiMintPost(t *testing.T) {
	t.Run("an owner minting for a staff member answers 200 and a bearer token for that member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"mira","ttl_days":7}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 604800,
			"owner_id":   "mira",
		})
	})

	t.Run("a ttl over the ceiling answers 200 capped at 400 days", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"mira","ttl_days":9999}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": 34560000,
			"owner_id":   "mira",
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", "", `{"member_id":"mira","ttl_days":7}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/mint", admin, `{"member_id":"mira","ttl_days":7}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request missing `ttl_days` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner, `{"member_id":"mira"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: ttl_days")
	})

	t.Run("a member that is not on the roster answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/mint", owner,
			`{"member_id":"ghost","ttl_days":7}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})
}

func TestHandleBootstrapApiBootstrapPost(t *testing.T) {
	t.Run("a spawn naming a member answers 200 with that member's boot package and a token", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"member_id":"mira"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Mira",
			"context": apiAnyString,
			"token":   apiAnyString,
		})
	})

	t.Run("a preview with no member answers 200 and a null token", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"assistant"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Assistant",
			"context": apiAnyString,
			"token":   nil,
		})
	})

	t.Run("an empty body answers 200 on the default role", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"role":    "assistant",
			"name":    "Assistant",
			"context": apiAnyString,
			"token":   nil,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", "", `{"member_id":"mira"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", agent, `{"member_id":"mira"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a body that is not JSON answers 422 without reaching the roster", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
	})

	t.Run("a storage fault while the boot package is assembled answers 500", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.rdb.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"assistant"}`)
		if status != 500 {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", "internal error: sql: database is closed")
	})

	t.Run("a member that is not on the roster answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"member_id":"ghost"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("a role nobody defines answers 404 naming the role", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/bootstrap", owner, `{"role":"nosuchrole"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "role 'nosuchrole' not found")
	})
}

func TestNoteTokenKeyObservation(t *testing.T) {
	t.Skip("TODO: noteTokenKeyObservation records, against the identity that just authenticated, WHICH signing key verified its credential — and, when that key is no longer the one signing, asks that machine to go get a new credential.")
}

func TestAskMachineToRenewIfStale(t *testing.T) {
	t.Skip("TODO: askMachineToRenewIfStale is the SUMMONS half (T-80, owner ruling A): when the credential this machine keeps presenting is signed by a key that is no longer the signing one, push the `renew` verb down its warden-command downlink.")
}

func TestClaimRenewAsk(t *testing.T) {
	t.Skip("TODO: claimRenewAsk is the compare-and-stamp that makes the interval hold under concurrency: two requests arriving together must not both decide the interval has elapsed.")
}

func TestKeyRenewNow(t *testing.T) {
	t.Skip("TODO: keyRenewNow reads the injectable clock; nil means the real one.")
}

func TestRememberTokenKey(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}
