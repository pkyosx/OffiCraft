// Skeleton generated from server/ocserverd/api_auth_mfa.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestMfaIssuer(t *testing.T) {
	t.Skip("TODO: mfaIssuer / mfaAccount label the entry in the owner's authenticator app.")
}

func TestMfaAccount(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestVerifyAndSpendTOTP(t *testing.T) {
	t.Skip("TODO: verifyAndSpendTOTP is THE second-factor check, and the ONLY place the replay floor moves.")
}

func TestHandleMfaStateApiAuthMfaGet(t *testing.T) {
	t.Run("a fresh server answers 200 with the factor neither offered nor enrolled", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", owner, "")
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

	t.Run("an armed factor answers 200 with enrolled true and the secret still withheld", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     false,
			"enrolled":    true,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", admin, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleMfaOfferApiAuthMfaOfferPost(t *testing.T) {
	t.Run("offering the feature answers 200 with offered true", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     true,
			"enrolled":    false,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("withdrawing the feature answers 200 with offered false", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":false}`)
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

	t.Run("the withdrawn flag leaves an armed factor enrolled", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":false}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     false,
			"enrolled":    true,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("a request missing `offered` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: offered")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", "", `{"offered":true}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/offer", admin, `{"offered":true}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleMfaEnrollApiAuthMfaEnrollPost(t *testing.T) {
	t.Run("enrolling on an offered server answers 200 with a secret and its otpauth URI", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		secret, _ := data["secret"].(string)
		if secret == "" {
			t.Fatalf("enroll must disclose a secret, got %v", data["secret"])
		}
		apiWantBody(t, data, map[string]any{
			"offered":  true,
			"enrolled": false,
			"secret":   apiAnyString,
			"otpauth_uri": "otpauth://totp/OffiCraft:owner?algorithm=SHA1&digits=6&issuer=OffiCraft" +
				"&period=30&secret=" + secret,
		})
	})

	t.Run("enrolling twice answers 200 and replaces the unproven secret", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		_, first := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		secret, _ := data["secret"].(string)
		if secret == "" || secret == first["secret"] {
			t.Fatalf("a second enrolment must mint a new secret, got %v after %v", data["secret"], first["secret"])
		}
		apiWantBody(t, data, map[string]any{
			"offered":  true,
			"enrolled": false,
			"secret":   apiAnyString,
			"otpauth_uri": "otpauth://totp/OffiCraft:owner?algorithm=SHA1&digits=6&issuer=OffiCraft" +
				"&period=30&secret=" + secret,
		})
	})

	t.Run("the studio and owner names label the enrolment entry", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "PATCH", "/api/settings", owner, `{"org_name":"Studio Nine","owner_name":"Eva"}`)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		secret, _ := data["secret"].(string)
		apiWantBody(t, data, map[string]any{
			"offered":  true,
			"enrolled": false,
			"secret":   apiAnyString,
			"otpauth_uri": "otpauth://totp/Studio%20Nine:Eva?algorithm=SHA1&digits=6&issuer=Studio+Nine" +
				"&period=30&secret=" + secret,
		})
	})

	t.Run("enrolling while the feature is not offered answers 403", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "the second factor is not enabled on this server")
	})

	t.Run("enrolling against an armed factor answers 409", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a second factor is already active; disable it first")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", admin, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleMfaActivateApiAuthMfaActivatePost(t *testing.T) {
	t.Run("the password plus a code from the pending secret answers 200 with the factor armed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		_, enrolled := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		secret, _ := enrolled["secret"].(string)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     true,
			"enrolled":    true,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("an armed factor answers 200 on the state row and 401 to a login with no code", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		_, enrolled := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		secret, _ := enrolled["secret"].(string)
		apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)

		status, data := apiJSON(t, h, "GET", "/api/auth/mfa", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     true,
			"enrolled":    true,
			"secret":      nil,
			"otpauth_uri": nil,
		})

		status, data = apiJSON(t, h, "POST", "/api/login", "",
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a request missing `code` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: code")
	})

	t.Run("activating while the feature is not offered answers 403", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"123456"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "the second factor is not enabled on this server")
	})

	t.Run("activating against an armed factor answers 409", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"123456"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a second factor is already active")
	})

	t.Run("activating with no enrolment behind it answers 409", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"123456"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "no pending enrolment; call /api/auth/mfa/enroll first")
	})

	t.Run("a wrong password with a good code answers 401 naming neither factor", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		_, enrolled := apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)
		secret, _ := enrolled["secret"].(string)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"not-the-password","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("the right password with a wrong code answers 401 with the same refusal", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		apiJSON(t, h, "POST", "/api/auth/mfa/enroll", owner, `{}`)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", "",
			`{"password":"x","code":"123456"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/activate", admin,
			`{"password":"x","code":"123456"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleMfaDisableApiAuthMfaDisablePost(t *testing.T) {
	t.Run("the password plus a live code answers 200 with the factor disarmed", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
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

	t.Run("disabling while the feature is still offered answers 200 keeping the flag on", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/mfa/offer", owner, `{"offered":true}`)
		secret := apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"offered":     true,
			"enrolled":    false,
			"secret":      nil,
			"otpauth_uri": nil,
		})
	})

	t.Run("a disarmed factor answers 200 on the next login without a code", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)
		apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"`+apiTestTOTPCode(t, secret)+`"}`)

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

	t.Run("a request missing `password` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner, `{"code":"123456"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: password")
	})

	t.Run("disabling with no factor armed answers 409", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"123456"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "no second factor is active")
	})

	t.Run("a wrong password with a live code answers 401 naming neither factor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		secret := apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"not-the-password","code":"`+apiTestTOTPCode(t, secret)+`"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("the right password with a wrong code answers 401 with the same refusal", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", owner,
			`{"password":"`+apiTestOwnerPassword+`","code":"000000"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password or code")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", "",
			`{"password":"x","code":"123456"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/auth/mfa/disable", admin,
			`{"password":"x","code":"123456"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}
