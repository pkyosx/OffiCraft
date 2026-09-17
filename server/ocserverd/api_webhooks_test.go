// Skeleton generated from server/ocserverd/api_webhooks.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogWebhookRequest(t *testing.T) {
	_, h, _, owner := newAPITestServer(t)
	token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
	req := httptest.NewRequest(http.MethodPost, "/in?t="+token, strings.NewReader(`{"event":"deployed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Source", "ci")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /in status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("POST /in body = %q, want %q", got, `{"status":"ok"}`)
	}
	apiWantWebhookRequests(t, h, owner, "alerts", map[string]any{
		"ts":        apiAnyNumber,
		"outcome":   "delivered",
		"headers":   `{"Content-Type":["application/json"],"X-Webhook-Source":["ci"]}`,
		"body":      `{"event":"deployed"}`,
		"truncated": false,
	})
}

func TestNewWebhookToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		token := newWebhookToken()
		if len(token) != 43 {
			t.Fatalf("newWebhookToken() length = %d, want 43", len(token))
		}
		if strings.ContainsAny(token, "+/=") {
			t.Fatalf("newWebhookToken() = %q, want raw URL-safe base64 without padding", token)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("newWebhookToken() = %q is not raw URL-safe base64: %v", token, err)
		}
		if len(raw) != 32 {
			t.Fatalf("newWebhookToken() decoded length = %d, want 32", len(raw))
		}
		if seen[token] {
			t.Fatalf("newWebhookToken() repeated token %q", token)
		}
		seen[token] = true
	}
}

func TestHandleListWebhooksApiMembersMemberIdWebhooksGet(t *testing.T) {
	t.Run("a member nobody has given an endpoint answers an empty list", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiWantWebhookList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a member's endpoints come back oldest first", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		alerts := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		beta := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"beta"}`)
		dashboard := apiTestListen(t, api, "")

		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(alerts), map[string]any{
			"endpoint_id":        "beta",
			"purpose":            "",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              beta,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
		dashboard.wantFrames()
	})

	t.Run("one member's endpoints never appear under another member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		apiWantWebhookList(t, h, owner, "mira")
	})

	t.Run("a member this server does not have answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/ghost/webhooks", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/members/kip/webhooks", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/kip/webhooks", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleCreateWebhookApiMembersMemberIdWebhooksPost(t *testing.T) {
	t.Run("a create answers the minted endpoint, lands exactly one row and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"alerts","purpose":"CI"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		minted, _ := data["token"].(string)
		apiWantBody(t, data, apiWebhookAlertsRow(apiAnyString))
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("a slack endpoint records that it holds a shared phrase without ever answering it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"slackin","platform":"slack","signing_secret":"s3cret"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		minted, _ := data["token"].(string)
		want := map[string]any{
			"endpoint_id":        "slackin",
			"purpose":            "",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              apiAnyString,
			"platform":           "slack",
			"has_signing_secret": true,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		}
		apiWantBody(t, data, want)
		want["token"] = minted
		apiWantWebhookList(t, h, owner, "kip", want)
	})

	t.Run("an endpoint id the member already carries answers 409 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"alerts","purpose":"other"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a webhook endpoint 'alerts' already exists for this member")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
	})

	t.Run("an endpoint id outside the closed charset answers 422 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"../../etc/passwd"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"endpoint id may contain only letters, digits, '_' and '-' (no spaces or special characters)")
		apiWantWebhookList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a blank endpoint id answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner, `{"endpoint_id":"   "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "endpoint id cannot be blank")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("an endpoint id past the length cap answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"`+strings.Repeat("a", 65)+`"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "endpoint id must be at most 64 characters")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a platform outside the closed set answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"tg","platform":"telegram","signing_secret":"s3cret"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"platform must be one of ['generic' 'slack' 'github']; got 'telegram'")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a slack endpoint with nothing to verify against answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"slackin","platform":"slack"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "signing_secret is required when platform is 'slack'")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a body naming no endpoint_id answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner, `{"purpose":"CI"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: endpoint_id")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a body carrying a key this route does not declare answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner,
			`{"endpoint_id":"alerts","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"note\"")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a body that is not JSON answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", owner, `not json`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character 'o' in literal null (expecting 'u')")
		apiWantWebhookList(t, h, owner, "kip")
	})

	t.Run("a member this server does not have answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/ghost/webhooks", owner, `{"endpoint_id":"alerts"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", agent, `{"endpoint_id":"alerts"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantWebhookList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/webhooks", "", `{"endpoint_id":"alerts"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantWebhookList(t, h, owner, "kip")
	})
}

func TestHandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(t *testing.T) {
	t.Run("flipping the status and editing the purpose answers the changed row, stores it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner,
			`{"status":"disabled","purpose":"CD"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CD",
			"status":             "disabled",
			"created_ts":         apiAnyNumber,
			"token":              minted,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		}
		apiWantBody(t, data, want)
		apiWantWebhookList(t, h, owner, "kip", want)
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("rotating the shared phrase flips has_signing_secret without answering the phrase", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner,
			`{"signing_secret":"s3cret"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              minted,
			"platform":           "generic",
			"has_signing_secret": true,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		}
		apiWantBody(t, data, want)
		apiWantWebhookList(t, h, owner, "kip", want)
	})

	t.Run("an empty patch answers the row exactly as it stands", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiWebhookAlertsRow(minted))
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("a status outside the closed set answers 422 and changes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner, `{"status":"paused"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "status must be one of ['enabled' 'disabled']; got 'paused'")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
	})

	t.Run("a body carrying a key this route does not declare answers 422 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner, `{"endpoint_id":"other"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"endpoint_id\"")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("an endpoint this member does not carry answers 404 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/nope", owner, `{"status":"disabled"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'nope' not found")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("another member's endpoint id answers 404 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/mira/webhooks/alerts", owner, `{"status":"disabled"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'alerts' not found")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", agent, `{"status":"disabled"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", "", `{"status":"disabled"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})
}

func TestHandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(t *testing.T) {
	t.Run("a delete answers the revoked endpoint, empties the listing and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/alerts", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiWebhookAlertsRow(minted))
		apiWantWebhookList(t, h, owner, "kip")
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("the revoked token can never deliver again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/alerts", owner, "")
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+minted, "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		apiWantNoChatWithKip(t, h, owner)
		dashboard.wantFrames()
		recipient.wantFrames()
	})

	t.Run("only the addressed endpoint is revoked", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		beta := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"beta"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/beta", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"endpoint_id":        "beta",
			"purpose":            "",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              beta,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("an endpoint this member does not carry answers 404 and removes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'nope' not found")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
	})

	t.Run("another member's endpoint id answers 404 and removes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/members/mira/webhooks/alerts", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'alerts' not found")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/alerts", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/webhooks/alerts", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantWebhookList(t, h, owner, "kip", apiWebhookAlertsRow(minted))
	})
}

func TestResolveWebhook(t *testing.T) {
	t.Run("an existing endpoint is resolved by its member and endpoint id through the debug route", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		apiWantWebhookRequests(t, h, owner, "alerts")
	})

	t.Run("an endpoint absent for an existing member folds to the endpoint not-found response", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, http.MethodGet,
			"/api/members/kip/webhooks/missing/requests", owner, "")
		if status != http.StatusNotFound {
			t.Fatalf("GET missing webhook requests status = %d, want 404 (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'missing' not found")
	})

	t.Run("a member absent from the roster folds to the endpoint-labelled not-found response", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, http.MethodGet,
			"/api/members/ghost/webhooks/alerts/requests", owner, "")
		if status != http.StatusNotFound {
			t.Fatalf("GET absent-member webhook requests status = %d, want 404 (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'alerts' not found")
	})
}

func TestHandleReceiveWebhookInPost(t *testing.T) {
	t.Run("a storage fault returns a generic 500 and keeps the fault in the server log", func(t *testing.T) {
		_, h, d, _ := newAPITestServer(t)
		if err := d.rdb.Close(); err != nil {
			t.Fatalf("close read pool: %v", err)
		}
		var probe any
		probeErr := d.rdb.QueryRow("SELECT 1").Scan(&probe)
		if probeErr == nil {
			t.Fatal("closed read pool unexpectedly accepted a query")
		}
		logs := apiCaptureStandardLog(t)

		rec := apiRequest(t, h, http.MethodPost, "/in?t=storage-fault", "", `{"text":"build broke"}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/json")
		}
		wantBody := `{"error":{"code":"internal_error","message":"internal server error"}}`
		if got := rec.Body.String(); got != wantBody {
			t.Fatalf("body = %q, want %q", got, wantBody)
		}
		if !strings.Contains(logs.String(), probeErr.Error()) {
			t.Fatalf("server log = %q, want storage error %q", logs.String(), probeErr)
		}
	})

	t.Run("a chat write storage fault returns a generic 500 and records no delivery", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")
		if err := d.wdb.Close(); err != nil {
			t.Fatalf("close write pool: %v", err)
		}
		var probe any
		probeErr := d.wdb.QueryRow("SELECT 1").Scan(&probe)
		if probeErr == nil {
			t.Fatal("closed write pool unexpectedly accepted a query")
		}
		logs := apiCaptureStandardLog(t)

		rec := apiRequest(t, h, http.MethodPost, "/in?t="+token, "", `{"text":"build broke"}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/json")
		}
		wantBody := `{"error":{"code":"internal_error","message":"internal server error"}}`
		if got := rec.Body.String(); got != wantBody {
			t.Fatalf("body = %q, want %q", got, wantBody)
		}
		if !strings.Contains(logs.String(), probeErr.Error()) {
			t.Fatalf("server log = %q, want storage error %q", logs.String(), probeErr)
		}
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantNoChatWithKip(t, h, owner)
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
		apiWantWebhookRequests(t, h, owner, "alerts")
	})

	t.Run("a recipient lookup storage fault returns a generic 500 and keeps the fault in the server log", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		if _, err := d.wdb.Exec(`UPDATE member SET session_boot_ts = ? WHERE id = ?`, "not-a-number", "kip"); err != nil {
			t.Fatalf("corrupt member read value: %v", err)
		}
		_, probeErr := d.GetMember("kip")
		if probeErr == nil {
			t.Fatal("malformed member read value unexpectedly scanned successfully")
		}
		logs := apiCaptureStandardLog(t)

		rec := apiRequest(t, h, http.MethodPost, "/in?t="+token, "", `{"text":"build broke"}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/json")
		}
		wantBody := `{"error":{"code":"internal_error","message":"internal server error"}}`
		if got := rec.Body.String(); got != wantBody {
			t.Fatalf("body = %q, want %q", got, wantBody)
		}
		if !strings.Contains(logs.String(), probeErr.Error()) {
			t.Fatalf("server log = %q, want storage error %q", logs.String(), probeErr)
		}
	})

	t.Run("an accepted call synthesises one chat delta to the member and the owner and reaches nobody else", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":   apiAnyString,
					"from": "hook:alerts",
					"to":   "kip",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "server",
		}
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)
		bystander.wantFrames()

		read := apiRequest(t, h, "GET", "/api/chat?with=kip&limit=5", owner, "")
		if read.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", read.Code, read.Body.String())
		}
		var chat any
		if err := json.Unmarshal(read.Body.Bytes(), &chat); err != nil {
			t.Fatalf("non-JSON body: %s", read.Body.String())
		}
		apiWantValue(t, "chat", chat, map[string]any{
			"messages": []any{map[string]any{
				"id":                 apiAnyString,
				"from":               "hook:alerts",
				"from_name":          "",
				"to":                 "kip",
				"to_name":            "",
				"body":               `{"text":"build broke"}`,
				"body_omitted_chars": 0,
				"ts":                 apiAnyNumber,
				"ts_display":         "",
				"meta": map[string]any{
					"webhook": map[string]any{"endpoint_id": "alerts", "purpose": "CI"},
				},
				"reply_card_status": "",
				"attachments":       []any{},
				"reply_to":          "",
			}},
		})
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    1,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
	})

	t.Run("a token no endpoint carries answers the same silent acknowledgement and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t=nobody-minted-this", "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
	})

	t.Run("a call with no token at all answers the same silent acknowledgement and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in", "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
		recipient.wantFrames()
	})

	t.Run("a disabled endpoint answers the same silent acknowledgement, fans nothing and records the drop", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner, `{"status":"disabled"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "disabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    0,
			"dropped_count":      1,
			"last_drop_reason":   "disabled",
		})
	})

	t.Run("a slack endpoint whose signature does not verify answers the same silent acknowledgement and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip",
			`{"endpoint_id":"slackin","platform":"slack","signing_secret":"s3cret"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "", `{"text":"build broke"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "slackin",
			"purpose":            "",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "slack",
			"has_signing_secret": true,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    0,
			"dropped_count":      1,
			"last_drop_reason":   "sig_failed",
		})
	})

	t.Run("a body one byte over the cap is refused to the sender's face, reaches the member not at all, and is recorded as oversize", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "",
			strings.Repeat("a", 1048577))
		if status != 413 {
			t.Fatalf("want 413, got %d (%v)", status, data)
		}
		apiWantError(t, data, "client_error",
			"webhook payload is too large (max 1048576 bytes)")
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantNoChatWithKip(t, h, owner)
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    0,
			"dropped_count":      1,
			"last_drop_reason":   "oversize",
		})
		apiWantWebhookRequests(t, h, owner, "alerts", map[string]any{
			"ts":        apiAnyNumber,
			"outcome":   "dropped:oversize",
			"headers":   `{"Content-Type":["application/json"]}`,
			"body":      strings.Repeat("a", 16384),
			"truncated": true,
		})
	})

	t.Run("a repeated token parameter is refused 422, reaches the member not at all, and is never recorded", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token+"&t="+token, "", `{"text":"build broke"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"Invalid format for parameter t: multiple values for single value parameter 't'")
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantNoChatWithKip(t, h, owner)
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   0,
			"delivered_count":    0,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
		apiWantWebhookRequests(t, h, owner, "alerts")
	})

	t.Run("a body exactly at the cap still delivers, unchanged", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "",
			strings.Repeat("a", 1048576))
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"status": "ok"})
		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":   apiAnyString,
					"from": "hook:alerts",
					"to":   "kip",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "server",
		}
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    1,
			"dropped_count":      0,
			"last_drop_reason":   "",
		})
	})

	t.Run("an over-cap call to a disabled endpoint is refused for its size, and size is what gets recorded", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		apiJSON(t, h, "PATCH", "/api/members/kip/webhooks/alerts", owner, `{"status":"disabled"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "", strings.Repeat("a", 1048577))
		if status != 413 {
			t.Fatalf("want 413, got %d (%v)", status, data)
		}
		apiWantError(t, data, "client_error",
			"webhook payload is too large (max 1048576 bytes)")
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantNoChatWithKip(t, h, owner)
		// Size outranks status deliberately: the body was refused before anyone
		// asked whether this endpoint was accepting, so `disabled` would be a
		// reason nobody actually reached. The silent face is not lost either —
		// an over-cap call to an UNKNOWN token answers this same 413.
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "alerts",
			"purpose":            "CI",
			"status":             "disabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "generic",
			"has_signing_secret": false,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    0,
			"dropped_count":      1,
			"last_drop_reason":   "oversize",
		})
	})

	t.Run("an over-cap refusal is byte-identical for a live token and a token nobody minted", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		oversize := strings.Repeat("a", 1048577)

		// 🔴 THE POINT IS THAT THESE TWO ANSWERS CANNOT BE TOLD APART. Asserting
		// only that they are EQUAL would stay green if both collapsed to the
		// silent 200, so each is pinned to the written-out 413 as well.
		live := apiRequest(t, h, "POST", "/in?t="+token, "", oversize)
		unknown := apiRequest(t, h, "POST", "/in?t=nobody-minted-this", "", oversize)

		for _, probe := range []struct {
			name string
			rec  *httptest.ResponseRecorder
		}{{"live token", live}, {"unknown token", unknown}} {
			if probe.rec.Code != 413 {
				t.Fatalf("%s: status = %d, want 413", probe.name, probe.rec.Code)
			}
			wantBody := `{"error":{"code":"client_error","message":"webhook payload is too large (max 1048576 bytes)"}}`
			if got := probe.rec.Body.String(); got != wantBody {
				t.Fatalf("%s: body = %q, want %q", probe.name, got, wantBody)
			}
			if got := probe.rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("%s: Content-Type = %q, want %q", probe.name, got, "application/json")
			}
		}
		if live.Code != unknown.Code ||
			live.Body.String() != unknown.Body.String() ||
			live.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
			t.Fatalf("the two refusals differ: live %d %q %q, unknown %d %q %q",
				live.Code, live.Body.String(), live.Header().Get("Content-Type"),
				unknown.Code, unknown.Body.String(), unknown.Header().Get("Content-Type"))
		}
	})

	t.Run("an over-cap call to a signed endpoint is refused for its size, never misclassified as a bad signature", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		token := apiTestWebhookToken(t, h, owner, "kip",
			`{"endpoint_id":"slackin","platform":"slack","signing_secret":"s3cret"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/in?t="+token, "",
			strings.Repeat("a", 1048577))
		if status != 413 {
			t.Fatalf("want 413, got %d (%v)", status, data)
		}
		apiWantError(t, data, "client_error",
			"webhook payload is too large (max 1048576 bytes)")
		dashboard.wantFrames()
		recipient.wantFrames()
		apiWantNoChatWithKip(t, h, owner)
		apiWantWebhookRow(t, h, owner, map[string]any{
			"endpoint_id":        "slackin",
			"purpose":            "",
			"status":             "enabled",
			"created_ts":         apiAnyNumber,
			"token":              token,
			"platform":           "slack",
			"has_signing_secret": true,
			"last_received_ts":   apiAnyNumber,
			"delivered_count":    0,
			"dropped_count":      1,
			"last_drop_reason":   "oversize",
		})
	})
}

func apiCaptureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})
	return &logs
}

func apiTestWebhookToken(t *testing.T, h http.Handler, owner, memberID, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/members/"+memberID+"/webhooks", owner, body)
	if status != 200 {
		t.Fatalf("create webhook: %d %v", status, data)
	}
	token, _ := data["token"].(string)
	if token == "" {
		t.Fatalf("create webhook must mint a token: %v", data)
	}
	return token
}

// apiWebhookAlertsRow is the row the `alerts` endpoint carries while nothing
// has touched it — the shape every "and changes nothing" case below compares
// the listing against. minted is the token that create answered with, or
// apiAnyString when the answer being judged is the mint itself.
func apiWebhookAlertsRow(minted string) map[string]any {
	return map[string]any{
		"endpoint_id":        "alerts",
		"purpose":            "CI",
		"status":             "enabled",
		"created_ts":         apiAnyNumber,
		"token":              minted,
		"platform":           "generic",
		"has_signing_secret": false,
		"last_received_ts":   0,
		"delivered_count":    0,
		"dropped_count":      0,
		"last_drop_reason":   "",
	}
}

// apiWantWebhookList asserts GET /api/members/{member_id}/webhooks answers
// EXACTLY these endpoints in this order. Passing none asserts the empty list,
// which is a different answer from a null.
func apiWantWebhookList(t *testing.T, h http.Handler, caller, memberID string, want ...map[string]any) {
	t.Helper()
	apiWantJSONArray(t, h, "/api/members/"+memberID+"/webhooks", caller, "webhooks", want...)
}

func apiWantWebhookRow(t *testing.T, h http.Handler, owner string, want map[string]any) {
	t.Helper()
	apiWantWebhookList(t, h, owner, "kip", want)
}

// apiWantWebhookRequests asserts the endpoint's debug ring buffer holds EXACTLY
// these rows, newest first.
func apiWantWebhookRequests(t *testing.T, h http.Handler, caller, endpointID string, want ...map[string]any) {
	t.Helper()
	apiWantJSONArray(t, h, "/api/members/kip/webhooks/"+endpointID+"/requests", caller, "requests", want...)
}

// apiWantNoChatWithKip asserts nothing was ever written to kip's chat — the
// "and reaches nobody" half of a refused or dropped call.
func apiWantNoChatWithKip(t *testing.T, h http.Handler, caller string) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/chat?with=kip&limit=5", caller, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	apiWantValue(t, "chat", got, map[string]any{"messages": []any{}})
}

func apiWantJSONArray(t *testing.T, h http.Handler, target, caller, path string, want ...map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", target, caller, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(t, path, got, expected)
}

func TestHandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet(t *testing.T) {
	t.Run("an endpoint nothing has called answers an empty list", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		dashboard := apiTestListen(t, api, "")

		apiWantWebhookRequests(t, h, owner, "alerts")
		dashboard.wantFrames()
	})

	t.Run("a delivered call is recorded with the headers and the body it arrived with", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		apiJSON(t, h, "POST", "/in?t="+minted, "", `{"text":"build broke"}`)

		apiWantWebhookRequests(t, h, owner, "alerts", map[string]any{
			"ts":        apiAnyNumber,
			"outcome":   "delivered",
			"headers":   `{"Content-Type":["application/json"]}`,
			"body":      `{"text":"build broke"}`,
			"truncated": false,
		})
	})

	t.Run("a payload past the log cap is recorded cut short and marked truncated", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		apiJSON(t, h, "POST", "/in?t="+minted, "", strings.Repeat("x", 20000))

		apiWantWebhookRequests(t, h, owner, "alerts", map[string]any{
			"ts":        apiAnyNumber,
			"outcome":   "delivered",
			"headers":   `{"Content-Type":["application/json"]}`,
			"body":      strings.Repeat("x", 16384),
			"truncated": true,
		})
	})

	t.Run("a payload whose signature does not verify is recorded as a drop and reaches nobody", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip",
			`{"endpoint_id":"slackin","platform":"slack","signing_secret":"s3cret"}`)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")

		apiJSON(t, h, "POST", "/in?t="+minted, "", `{"text":"build broke"}`)

		apiWantWebhookRequests(t, h, owner, "slackin", map[string]any{
			"ts":        apiAnyNumber,
			"outcome":   "dropped:sig_failed",
			"headers":   `{"Content-Type":["application/json"]}`,
			"body":      `{"text":"build broke"}`,
			"truncated": false,
		})
		apiWantNoChatWithKip(t, h, owner)
		dashboard.wantFrames()
		recipient.wantFrames()
	})

	t.Run("the buffer keeps the newest five calls and drops the older ones", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		minted := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		for _, n := range []string{"0", "1", "2", "3", "4", "5", "6"} {
			apiJSON(t, h, "POST", "/in?t="+minted, "", `{"n":`+n+`}`)
		}

		apiWantWebhookRequests(t, h, owner, "alerts",
			map[string]any{"ts": apiAnyNumber, "outcome": "delivered", "headers": `{"Content-Type":["application/json"]}`, "body": `{"n":6}`, "truncated": false},
			map[string]any{"ts": apiAnyNumber, "outcome": "delivered", "headers": `{"Content-Type":["application/json"]}`, "body": `{"n":5}`, "truncated": false},
			map[string]any{"ts": apiAnyNumber, "outcome": "delivered", "headers": `{"Content-Type":["application/json"]}`, "body": `{"n":4}`, "truncated": false},
			map[string]any{"ts": apiAnyNumber, "outcome": "delivered", "headers": `{"Content-Type":["application/json"]}`, "body": `{"n":3}`, "truncated": false},
			map[string]any{"ts": apiAnyNumber, "outcome": "delivered", "headers": `{"Content-Type":["application/json"]}`, "body": `{"n":2}`, "truncated": false})
	})

	t.Run("an endpoint this member does not carry answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "GET", "/api/members/kip/webhooks/nope/requests", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'nope' not found")
	})

	t.Run("another member's endpoint id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "GET", "/api/members/mira/webhooks/alerts/requests", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "webhook endpoint 'alerts' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/members/kip/webhooks/alerts/requests", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

		status, data := apiJSON(t, h, "GET", "/api/members/kip/webhooks/alerts/requests", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestWriteWebhookAccepted(t *testing.T) {
	_, h, _, owner := newAPITestServer(t)
	token := apiTestWebhookToken(t, h, owner, "kip", `{"endpoint_id":"alerts","purpose":"CI"}`)

	ignored := apiRequest(t, h, http.MethodPost, "/in?t=unknown-token", "", `{"event":"ignored"}`)
	if ignored.Code != http.StatusOK || ignored.Body.String() != `{"status":"ok"}` {
		t.Fatalf("unknown token response = (%d, %q), want (200, %q)", ignored.Code, ignored.Body.String(), `{"status":"ok"}`)
	}
	if got := ignored.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unknown token content type = %q, want application/json", got)
	}
	apiWantNoChatWithKip(t, h, owner)

	accepted := apiRequest(t, h, http.MethodPost, "/in?t="+token, "", `{"event":"delivered"}`)
	if accepted.Code != ignored.Code || accepted.Body.String() != ignored.Body.String() {
		t.Fatalf("accepted response = (%d, %q), ignored response = (%d, %q); acknowledgements must be byte-identical",
			accepted.Code, accepted.Body.String(), ignored.Code, ignored.Body.String())
	}
	if got := accepted.Header().Get("Content-Type"); got != ignored.Header().Get("Content-Type") {
		t.Fatalf("accepted content type = %q, ignored content type = %q", got, ignored.Header().Get("Content-Type"))
	}
}
