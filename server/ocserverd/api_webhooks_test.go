// Skeleton generated from server/ocserverd/api_webhooks.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestLogWebhookRequest(t *testing.T) {
	t.Skip("TODO: logWebhookRequest records one resolved /in request into the endpoint's debug ring buffer (newest 5 kept).")
}

func TestNewWebhookToken(t *testing.T) {
	t.Skip("TODO: newWebhookToken mints a high-entropy, unguessable, URL-safe opaque token (32 bytes of crypto/rand → base64url, ~43 chars).")
}

func TestHandleListWebhooksApiMembersMemberIdWebhooksGet(t *testing.T) {
	t.Run("a well-formed GET /api/members/{member_id}/webhooks answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/webhooks request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members/{member_id}/webhooks reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/webhooks request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleCreateWebhookApiMembersMemberIdWebhooksPost(t *testing.T) {
	t.Run("a well-formed POST /api/members/{member_id}/webhooks answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/webhooks request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/members/{member_id}/webhooks reaches this handler with member_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/members/{member_id}/webhooks request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch(t *testing.T) {
	t.Run("a well-formed PATCH /api/members/{member_id}/webhooks/{endpoint_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/members/{member_id}/webhooks/{endpoint_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to PATCH /api/members/{member_id}/webhooks/{endpoint_id} reaches this handler with member_id, endpoint_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PATCH /api/members/{member_id}/webhooks/{endpoint_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete(t *testing.T) {
	t.Run("a well-formed DELETE /api/members/{member_id}/webhooks/{endpoint_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/members/{member_id}/webhooks/{endpoint_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to DELETE /api/members/{member_id}/webhooks/{endpoint_id} reaches this handler with member_id, endpoint_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/members/{member_id}/webhooks/{endpoint_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestResolveWebhook(t *testing.T) {
	t.Skip("TODO: resolveWebhook returns the endpoint addressed by (member, endpoint_id), folding an absent member OR an absent endpoint to errNotFound (the 404 face).")
}

func TestHandleReceiveWebhookInPost(t *testing.T) {
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

func apiWantWebhookRow(t *testing.T, h http.Handler, owner string, want map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/members/kip/webhooks", owner, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	apiWantValue(t, "webhooks", got, []any{want})
}

func TestHandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet(t *testing.T) {
	t.Run("a well-formed GET /api/members/{member_id}/webhooks/{endpoint_id}/requests answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/webhooks/{endpoint_id}/requests request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/members/{member_id}/webhooks/{endpoint_id}/requests reaches this handler with member_id, endpoint_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/members/{member_id}/webhooks/{endpoint_id}/requests request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestWriteWebhookAccepted(t *testing.T) {
	t.Skip("TODO: writeWebhookAccepted is the single silent acknowledgement — byte-identical for an accepted and an ignored call so the response never leaks endpoint existence.")
}
