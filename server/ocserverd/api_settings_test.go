package main

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidatePushContactEmail(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address string
		want    string
	}{
		{name: "simple public address", address: "eva@hardcoretech.link"},
		{name: "plus tag and subdomain", address: "owner+push@alerts.hardcoretech.link"},
		{name: "missing at sign", address: "eva.hardcoretech.link", want: "push_contact_email must be an email address like name@example.com"},
		{name: "display name", address: "Eva <eva@hardcoretech.link>", want: "push_contact_email must be a single plain address, without a mailto: prefix or display name"},
		{name: "private domain", address: "eva@studio.local", want: `push_contact_email cannot use the reserved domain "studio.local" — push gateways reject it`},
		{name: "not a public domain", address: "eva@localhost", want: "push_contact_email must use a real public domain"},
		{name: "too long", address: strings.Repeat("a", maxPushContactEmailLen+1), want: "push_contact_email must be at most 254 characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePushContactEmail(tc.address)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validatePushContactEmail(%q): %v", tc.address, err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("validatePushContactEmail(%q): want %q, got %v", tc.address, tc.want, err)
			}
		})
	}
}

// apiTestShippedSettings is the WHOLE settings body a freshly claimed server
// serves — every field written out, so a scenario table only has to name what
// its patch moved and nothing can change unnoticed.
func apiTestShippedSettings() map[string]any {
	return map[string]any{
		"owner_token_ttl":                  86400,
		"agent_token_ttl":                  604800,
		"handover_pct":                     50,
		"notice_pct":                       40,
		"codex_compaction_threshold":       3,
		"codex_notice_round":               2,
		"monitoring_refresh_seconds":       5,
		"outsource_max_parallel":           3,
		"accelerated_grace_secs":           120,
		"warden_credential_lifetime_secs":  2592000,
		"doc_cap_chars_duty":               1000,
		"doc_cap_chars_insight":            15000,
		"doc_cap_chars_manual_sop":         15000,
		"doc_cap_chars_system_interaction": 60000,
		"doc_cap_chars_boot_sequence":      15000,
		"doc_cap_chars_offboard":           15000,
		"lore_cap_chars_role":              10000,
		"lore_cap_chars_manual":            10000,
		"lore_cap_chars_title":             80,
		"lore_cap_chars_body":              500,
		"chat_budget_chars":                6000,
		"step_note_cap_chars":              10000,
		"backup_retain":                    5,
		"updater_receive_beta":             false,
		"updater_auto_update":              false,
		"org_name":                         "",
		"owner_name":                       "",
		"push_contact_email":               "",
		"display_theme":                    "",
		"display_language":                 "",
		"display_wide":                     false,
		"suggested_replies_reply_card":     []any{},
		"suggested_replies_task_message":   []any{},
		"suggested_replies_lore_message":   []any{},
		"onboarding":                       nil,
	}
}

func TestAcceleratedGraceInRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		want  bool
	}{
		{name: "below minimum", value: minAcceleratedGraceSecs - 1},
		{name: "minimum", value: minAcceleratedGraceSecs, want: true},
		{name: "maximum", value: maxAcceleratedGraceSecs, want: true},
		{name: "above maximum", value: maxAcceleratedGraceSecs + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceleratedGraceInRange(tc.value); got != tc.want {
				t.Fatalf("acceleratedGraceInRange(%d) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestOutsourceParallelInRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		want  bool
	}{
		{name: "below unlimited sentinel", value: minOutsourceParallel - 1},
		{name: "unlimited sentinel", value: minOutsourceParallel, want: true},
		{name: "zero pauses assignment", value: 0, want: true},
		{name: "maximum", value: maxOutsourceParallel, want: true},
		{name: "above maximum", value: maxOutsourceParallel + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := outsourceParallelInRange(tc.value); got != tc.want {
				t.Fatalf("outsourceParallelInRange(%d) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestWardenCredLifetimeInRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		want  bool
	}{
		{name: "below one day", value: minWardenCredLifetimeSecs - 1},
		{name: "one day", value: minWardenCredLifetimeSecs, want: true},
		{name: "four hundred days", value: maxWardenCredLifetimeSecs, want: true},
		{name: "above four hundred days", value: maxWardenCredLifetimeSecs + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wardenCredLifetimeInRange(tc.value); got != tc.want {
				t.Fatalf("wardenCredLifetimeInRange(%d) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestHandleAuthStatusApiAuthStatusGet(t *testing.T) {
	t.Run("a first-run server answers 200 with no password set and no factor required", func(t *testing.T) {
		_, h, _, _ := newAPITestStack(t)

		status, data := apiJSON(t, h, "GET", "/api/auth/status", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"password_set": false, "mfa_required": false})
	})

	t.Run("a claimed server answers 200 with the password set", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/auth/status", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"password_set": true, "mfa_required": false})
	})

	t.Run("an armed factor answers 200 with mfa_required true", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		apiTestArmMFA(t, api, d)

		status, data := apiJSON(t, h, "GET", "/api/auth/status", "", "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"password_set": true, "mfa_required": true})
	})
}

func TestHandleSetPasswordApiAuthSetPasswordPost(t *testing.T) {
	t.Run("the claim token plus a long enough password answers 200 and a bearer owner token", func(t *testing.T) {
		_, h, _, claim := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"first-run-pass","claim_token":"`+claim+`"}`)
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

	t.Run("the new password answers 200 at the login door", func(t *testing.T) {
		_, h, _, claim := newAPITestStack(t)
		apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"first-run-pass","claim_token":"`+claim+`"}`)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"first-run-pass"}`)
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

	t.Run("a request missing `claim_token` answers 422", func(t *testing.T) {
		_, h, _, _ := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"first-run-pass"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: claim_token")
	})

	t.Run("a password under eight characters answers 422", func(t *testing.T) {
		_, h, _, claim := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"short","claim_token":"`+claim+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "password must be at least 8 characters")
	})

	t.Run("setting a password on a claimed server answers 409", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"second-attempt","claim_token":"whatever"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a password is already set")
	})

	t.Run("a claim token that does not match answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestStack(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/set-password", "",
			`{"password":"first-run-pass","claim_token":"not-the-claim-token"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid claim token")
	})

	t.Run("attempts beyond the in-flight cap answer 429 with a one-second Retry-After", func(t *testing.T) {
		api, h, _, _ := newAPITestStack(t)
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
				req := httptest.NewRequest("POST", "/api/auth/set-password",
					strings.NewReader(`{"password":"first-run-pass","claim_token":"wrong"}`))
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
				if got.body != `{"error":{"code":"unauthorized","message":"invalid claim token"}}` {
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

func TestHandleChangePasswordApiAuthChangePasswordPost(t *testing.T) {
	t.Run("the current password plus a new one answers 200 and a fresh bearer owner token", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`","new_password":"a-brand-new-pass"}`)
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

	t.Run("the new password answers 200 at the login door", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`","new_password":"a-brand-new-pass"}`)

		status, data := apiJSON(t, h, "POST", "/api/login", "", `{"password":"a-brand-new-pass"}`)
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

	t.Run("a request missing `new_password` answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: new_password")
	})

	t.Run("a new password under eight characters answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"`+apiTestOwnerPassword+`","new_password":"short"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "new_password must be at least 8 characters")
	})

	t.Run("a wrong current password answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", owner,
			`{"current_password":"not-the-password","new_password":"a-brand-new-pass"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "invalid password")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", "",
			`{"current_password":"x","new_password":"a-brand-new-pass"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated admin_agent identity answers 403 because this row requires owner", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/auth/change-password", admin,
			`{"current_password":"x","new_password":"a-brand-new-pass"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestWriteOwnerToken(t *testing.T) {
	t.Run("writes a verifiable owner token with the requested expiry", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		now, ttl := int64(1700000000), int64(3600)
		rec := httptest.NewRecorder()

		api.writeOwnerToken(rec, ttl, now)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode token response: %v", err)
		}
		apiWantBody(t, body, map[string]any{
			"token":      apiAnyString,
			"token_type": "bearer",
			"expires_in": float64(ttl),
			"owner_id":   wireOwnerID,
		})
		token, _ := body["token"].(string)
		claims, err := verifyJWT(token, api.keys.signingSecret(), now)
		if err != nil {
			t.Fatalf("verify owner token: %v", err)
		}
		apiWantValue(t, "claims", any(claims), any(map[string]any{
			"sub": wireOwnerID, "scope": "owner", "iat": float64(now), "exp": float64(now + ttl),
		}))
	})

	t.Run("refuses to mint when the signing key is absent", func(t *testing.T) {
		api, _, _, _ := newAPITestStackWithoutSigningSecret(t)
		rec := httptest.NewRecorder()

		api.writeOwnerToken(rec, 3600, 1700000000)
		if rec.Code != 401 {
			t.Fatalf("want 401, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		apiWantError(t, body, "unauthorized", "auth not configured")
	})
}

func TestHandleGetSettingsApiSettingsGet(t *testing.T) {
	t.Run("a claimed server answers 200 with the shipped defaults", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/settings", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestShippedSettings())
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/settings", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/settings", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an authenticated admin_agent identity answers 200", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "GET", "/api/settings", admin, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestShippedSettings())
	})
}

func TestHandleUpdateSettingsApiSettingsPatch(t *testing.T) {
	t.Run("a patch of every numeric knob answers 200 with the new values", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{
			"owner_token_ttl":43200,
			"agent_token_ttl":2592000,
			"notice_pct":45,
			"handover_pct":80,
			"codex_notice_round":4,
			"codex_compaction_threshold":6,
			"monitoring_refresh_seconds":30,
			"accelerated_grace_secs":90,
			"warden_credential_lifetime_secs":864000,
			"outsource_max_parallel":-1,
			"doc_cap_chars_duty":2000,
			"doc_cap_chars_insight":20000,
			"doc_cap_chars_manual_sop":20000,
			"doc_cap_chars_system_interaction":70000,
			"doc_cap_chars_boot_sequence":20000,
			"doc_cap_chars_offboard":20000,
			"chat_budget_chars":9000,
			"step_note_cap_chars":20000,
			"backup_retain":9
		}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestShippedSettings()
		want["owner_token_ttl"] = 43200
		want["agent_token_ttl"] = 2592000
		want["notice_pct"] = 45
		want["handover_pct"] = 80
		want["codex_notice_round"] = 4
		want["codex_compaction_threshold"] = 6
		want["monitoring_refresh_seconds"] = 30
		want["accelerated_grace_secs"] = 90
		want["warden_credential_lifetime_secs"] = 864000
		want["outsource_max_parallel"] = -1
		want["doc_cap_chars_duty"] = 2000
		want["doc_cap_chars_insight"] = 20000
		want["doc_cap_chars_manual_sop"] = 20000
		want["doc_cap_chars_system_interaction"] = 70000
		want["doc_cap_chars_boot_sequence"] = 20000
		want["doc_cap_chars_offboard"] = 20000
		want["chat_budget_chars"] = 9000
		want["step_note_cap_chars"] = 20000
		want["backup_retain"] = 9
		apiWantBody(t, data, want)
	})

	t.Run("a patch of every text and toggle knob answers 200 with the new values", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{
			"updater_receive_beta":true,
			"updater_auto_update":true,
			"org_name":"  Studio Nine  ",
			"owner_name":"  Eva  ",
			"push_contact_email":"  eva@example.com  ",
			"display_theme":"office",
			"display_language":"en",
			"display_wide":true,
			"suggested_replies_reply_card":["  yes  ","","no"],
			"suggested_replies_task_message":["on it"]
		}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestShippedSettings()
		want["updater_receive_beta"] = true
		want["updater_auto_update"] = true
		want["org_name"] = "Studio Nine"
		want["owner_name"] = "Eva"
		want["push_contact_email"] = "eva@example.com"
		want["display_theme"] = "office"
		want["display_language"] = "en"
		want["display_wide"] = true
		want["suggested_replies_reply_card"] = []any{"yes", "no"}
		want["suggested_replies_task_message"] = []any{"on it"}
		apiWantBody(t, data, want)
	})

	t.Run("an empty patch answers 200 leaving every value alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestShippedSettings())
	})

	t.Run("an explicitly empty suggested-reply list answers 200 and clears it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "PATCH", "/api/settings", owner, `{"suggested_replies_reply_card":["yes"]}`)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"suggested_replies_reply_card":[]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestShippedSettings())
	})

	t.Run("clearing the studio and owner names answers 200 with both empty", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "PATCH", "/api/settings", owner, `{"org_name":"Studio Nine","owner_name":"Eva"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"org_name":"","owner_name":"","push_contact_email":"","display_theme":"","display_language":""}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestShippedSettings())
	})

	t.Run("an owner_token_ttl outside the whitelist answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"owner_token_ttl":99}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "owner_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds")
	})

	t.Run("an agent_token_ttl outside the whitelist answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"agent_token_ttl":99}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "agent_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds")
	})

	t.Run("a handover_pct outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"handover_pct":39}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "handover_pct must be between 40 and 90")
	})

	t.Run("a notice_pct outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"notice_pct":90}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "notice_pct must be between 1 and 89")
	})

	t.Run("a codex_compaction_threshold outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"codex_compaction_threshold":11}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "codex_compaction_threshold must be between 1 and 10")
	})

	t.Run("a codex_notice_round outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"codex_notice_round":11}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "codex_notice_round must be between 1 and 10")
	})

	t.Run("a notice_pct that does not land before handover_pct answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"notice_pct":60,"handover_pct":60}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "notice_pct must be strictly below handover_pct")
	})

	t.Run("a codex_notice_round that does not land before the threshold answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"codex_notice_round":5,"codex_compaction_threshold":5}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "codex_notice_round must be strictly below codex_compaction_threshold")
	})

	t.Run("a monitoring_refresh_seconds outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"monitoring_refresh_seconds":61}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "monitoring_refresh_seconds must be between 1 and 60")
	})

	t.Run("an accelerated_grace_secs outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"accelerated_grace_secs":9}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "accelerated_grace_secs must be between 10 and 3600 seconds")
	})

	t.Run("an outsource_max_parallel outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"outsource_max_parallel":21}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "outsource_max_parallel must be between -1 and 20 (-1 = unlimited)")
	})

	t.Run("a warden_credential_lifetime_secs outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"warden_credential_lifetime_secs":3600}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "warden_credential_lifetime_secs must be between 86400 and 34560000 seconds "+
			"(one day through 400 days) — a warden renews at two thirds of the lifetime, so the "+
			"remaining third is the window an offline machine has to get a replacement, and below "+
			"a day that window stops surviving a working day of downtime")
	})

	t.Run("a document cap below the shared floor answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"doc_cap_chars_insight":99}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "doc_cap_chars_insight must be between 100 and 100000 characters — "+
			"a lowered cap binds the next write only; stored content over it is never truncated and still reads back")
	})

	t.Run("a chat_budget_chars outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"chat_budget_chars":13001}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "chat_budget_chars must be between 1000 and 13000 characters")
	})

	t.Run("a step_note_cap_chars outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"step_note_cap_chars":999}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step_note_cap_chars must be between 1000 and 100000 characters")
	})

	t.Run("a backup_retain outside its range answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"backup_retain":21}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "backup_retain must be between 1 and 20 backups per pool")
	})

	t.Run("an org_name over the character cap answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"org_name":"`+strings.Repeat("x", 81)+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "org_name must be at most 80 characters")
	})

	t.Run("an owner_name over the character cap answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"owner_name":"`+strings.Repeat("x", 81)+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "owner_name must be at most 80 characters")
	})

	t.Run("a push_contact_email that is not an address answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"push_contact_email":"not-an-address"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "push_contact_email must be an email address like name@example.com")
	})

	t.Run("a display_theme nothing defines answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_theme":"midnight"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `display_theme must be "", office, or an existing custom theme id`)
	})

	t.Run("a display_language outside the vocabulary answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_language":"fr"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "display_language must be one of zh, en")
	})

	t.Run("a suggested reply-card entry over the character cap answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"suggested_replies_reply_card":["`+strings.Repeat("x", 121)+`"]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "suggested_replies_reply_card must be at most 120 characters per entry")
	})

	t.Run("a suggested task-message list over the entry cap answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		entries := strings.TrimSuffix(strings.Repeat(`"ok",`, 21), ",")
		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"suggested_replies_task_message":[`+entries+`]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "suggested_replies_task_message must be at most 20 entries")
	})

	t.Run("dismissing an onboarding banner that is not there answers 409", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"onboarding_dismissed":true}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "no onboarding banner is up to dismiss — the first-run report is absent or not in a failed state")
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "PATCH", "/api/settings", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestSettingsView(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	got := api.settingsView()
	want := settingsDTO{
		OwnerTokenTTL:                86400,
		AgentTokenTTL:                604800,
		HandoverPct:                  50,
		NoticePct:                    40,
		CodexCompactionThreshold:     3,
		CodexNoticeRound:             2,
		MonitoringRefreshSeconds:     5,
		OutsourceMaxParallel:         3,
		AcceleratedGraceSecs:         120,
		WardenCredentialLifetimeSecs: 2592000,
		DocCapCharsDuty:              1000,
		DocCapCharsInsight:           15000,
		DocCapCharsManualSop:         15000,
		DocCapCharsSystemInteraction: 60000,
		DocCapCharsBootSequence:      15000,
		DocCapCharsOffboard:          15000,
		LoreCapCharsRole:             10000,
		LoreCapCharsManual:           10000,
		LoreCapCharsTitle:            80,
		LoreCapCharsBody:             500,
		ChatBudgetChars:              6000,
		StepNoteCapChars:             10000,
		BackupRetain:                 5,
		OrgName:                      "",
		OwnerName:                    "",
		PushContactEmail:             "",
		DisplayTheme:                 "",
		DisplayLanguage:              "",
		SuggestedRepliesReplyCard:    []string{},
		SuggestedRepliesTaskMessage:  []string{},
		SuggestedRepliesLoreMessage:  []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settingsView defaults:\n got %+v\nwant %+v", got, want)
	}

	asPatchSettings(t, h, owner, `{"org_name":"Studio Nine","suggested_replies_reply_card":["yes"]}`)
	got = api.settingsView()
	if got.OrgName != "Studio Nine" || !reflect.DeepEqual(got.SuggestedRepliesReplyCard, []string{"yes"}) {
		t.Fatalf("settingsView did not expose live settings: %+v", got)
	}
	got.SuggestedRepliesReplyCard[0] = "changed"
	if api.suggestedRepliesReplyCard[0] != "yes" {
		t.Fatalf("settingsView returned an aliased suggestion slice: %v", api.suggestedRepliesReplyCard)
	}
}
