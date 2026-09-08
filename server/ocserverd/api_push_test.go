// Skeleton generated from server/ocserverd/api_push.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"crypto/ecdh"
	"encoding/base64"
	"net/http"
	"testing"
)

func TestPushVAPIDSubscriber(t *testing.T) {
	t.Skip("TODO: pushVAPIDSubscriber is the contact address handed to the push gateways as the VAPID subject.")
}

func TestValidatePushEndpoint(t *testing.T) {
	t.Skip("TODO: validatePushEndpoint rejects values which could turn a saved browser subscription into an arbitrary server-side request.")
}

func TestIsPublicPushIP(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestSafePushHTTPClient(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestWebPushClient(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPushPublicKey(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPushVAPIDKeys(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestEnqueueWebPush(t *testing.T) {
	t.Skip("TODO: enqueueWebPush starts best-effort delivery after the durable event was committed.")
}

func TestPushDeliveryStatusClass(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPushDeliveryErrorClass(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

// apiPushTargets reads the delivery targets back. Web Push subscriptions have
// no read route of their own — the three routes under test are the whole API
// face — so the store itself is where "did it land" is observed.
func apiPushTargets(t *testing.T, d *DAL) []any {
	t.Helper()
	rows, err := d.ListPushSubscriptions()
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		target := map[string]any{
			"endpoint":        row.Endpoint,
			"p256dh":          row.P256dh,
			"expiration_time": nil,
		}
		target["pair"] = row.Auth
		if row.ExpirationTime != nil {
			target["expiration_time"] = *row.ExpirationTime
		}
		out = append(out, target)
	}
	return out
}

func apiWantPushTargets(t *testing.T, d *DAL, want ...map[string]any) {
	t.Helper()
	expected := make([]any, len(want))
	for i := range want {
		expected[i] = want[i]
	}
	apiWantValue(t, "push-targets", any(apiPushTargets(t, d)), any(expected))
}

// apiSeedPushTargets files the two subscriptions the delete cases act on,
// through the real write route.
func apiSeedPushTargets(t *testing.T, h http.Handler, credential string) {
	t.Helper()
	for _, body := range []string{
		`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`,
		`{"endpoint":"https://push.example.com/ep-2","keys":{"p256dh":"fern","auth":"bark"}}`,
	} {
		if status, data := apiJSON(t, h, "POST", "/api/push/subscription", credential, body); status != 204 {
			t.Fatalf("seed subscription: %d %v", status, data)
		}
	}
}

func apiWantSeededPushTargets(t *testing.T, d *DAL) {
	t.Helper()
	apiWantPushTargets(t, d,
		map[string]any{
			"endpoint": "https://push.example.com/ep-1", "p256dh": "vine",
			"pair": "moss", "expiration_time": nil,
		},
		map[string]any{
			"endpoint": "https://push.example.com/ep-2", "p256dh": "fern",
			"pair": "bark", "expiration_time": nil,
		},
	)
}

func apiPushVAPIDStored(t *testing.T, d *DAL) bool {
	t.Helper()
	stored, err := d.GetSetting(settingPushVAPIDPrivateKey)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	return stored != nil
}

func apiWantNoBody(t *testing.T, status int, data map[string]any, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("want %d, got %d (%v)", want, status, data)
	}
	if len(data) != 0 {
		t.Fatalf("want an empty body, got %v", data)
	}
}

func TestHandleGetPushPublicKeyApiPushPublicKeyGet(t *testing.T) {
	t.Run("the first read mints the browser-usable VAPID public key and every later read answers the same one", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if apiPushVAPIDStored(t, d) {
			t.Fatal("a fresh station should hold no VAPID key until one is asked for")
		}
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/push/public-key", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"public_key": apiAnyString})
		dashboard.wantFrames()
		pushed()

		minted, _ := data["public_key"].(string)
		raw, err := base64.RawURLEncoding.DecodeString(minted)
		if err != nil {
			t.Fatalf("public_key is not base64url: %v", err)
		}
		if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
			t.Fatalf("public_key is not the uncompressed P-256 point PushManager.subscribe takes: %v", err)
		}

		_, again := apiJSON(t, h, "GET", "/api/push/public-key", owner, "")
		apiWantBody(t, again, map[string]any{"public_key": minted})
		if !apiPushVAPIDStored(t, d) {
			t.Fatal("the key pair should have been retained")
		}
	})

	t.Run("a request carrying no credentials answers 401 and mints no key pair", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/push/public-key", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		pushed()
		if apiPushVAPIDStored(t, d) {
			t.Fatal("a refused request must not have minted a key pair")
		}
	})

	t.Run("an authenticated agent identity answers 403 because the owner's browser is not an office capability, and mints no key pair", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/push/public-key", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		pushed()
		if apiPushVAPIDStored(t, d) {
			t.Fatal("a refused request must not have minted a key pair")
		}
	})

	t.Run("a GET /api/push/public-key request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) {
		t.Skip("structurally unproducible: this GET takes no query parameter and decodes " +
			"no request body, and the stack carries no content-type or size middleware, so " +
			"no wire-layer 4xx exists to observe — measured: a `{{{` body on this route " +
			"still answers 200 with the minted public_key, i.e. it reached the domain.")
	})
}

func TestHandleCreatePushSubscriptionApiPushSubscriptionPost(t *testing.T) {
	t.Run("a well-formed subscription answers 204 with no body and becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		apiWantNoBody(t, status, data, 204)
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d, map[string]any{
			"endpoint": "https://push.example.com/ep-1", "p256dh": "vine",
			"pair": "moss", "expiration_time": nil,
		})
	})

	t.Run("a second endpoint is stored beside the first, and re-sending one endpoint replaces its stored pair in place", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`); status != 204 {
			t.Fatalf("first subscribe: %d %v", status, data)
		}
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-2","keys":{"p256dh":"fern","auth":"bark"}}`)
		apiWantNoBody(t, status, data, 204)
		status, data = apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"reed","auth":"silt"},"expiration_time":1799999999}`)
		apiWantNoBody(t, status, data, 204)
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d,
			map[string]any{
				"endpoint": "https://push.example.com/ep-1", "p256dh": "reed",
				"pair": "silt", "expiration_time": float64(1799999999),
			},
			map[string]any{
				"endpoint": "https://push.example.com/ep-2", "p256dh": "fern",
				"pair": "bark", "expiration_time": nil,
			},
		)
	})

	t.Run("an endpoint that is not HTTPS is refused 400 and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"http://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "push endpoint must be an absolute HTTPS URL")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("an endpoint aimed back at this machine is refused 400 by name and by address, and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://localhost/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "push endpoint host is not public")

		status, data = apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://127.0.0.1/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "push endpoint host is not public")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("a subscription whose endpoint or pair is blank is refused 400 and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"   ","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "endpoint and subscription keys must not be blank")

		status, data = apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"  "}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "endpoint and subscription keys must not be blank")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("a body that omits the browser's key pair answers 422 naming the missing field and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: keys")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("a body carrying a field this route does not declare answers 422 and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"},"user_id":"owner"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "user_id"`)
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("a POST /api/push/subscription request the wire layer rejects (a malformed JSON body) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("a request carrying no credentials answers 401 and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", "",
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})

	t.Run("an authenticated agent identity answers 403 and nothing becomes a delivery target", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/push/subscription", housekeeper,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d)
	})
}

func TestHandleDeletePushSubscriptionApiPushSubscriptionDelete(t *testing.T) {
	t.Run("deleting one endpoint answers 204 and takes only that one out of the delivery targets", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1"}`)
		apiWantNoBody(t, status, data, 204)
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d, map[string]any{
			"endpoint": "https://push.example.com/ep-2", "p256dh": "fern",
			"pair": "bark", "expiration_time": nil,
		})
	})

	t.Run("an endpoint nothing is stored under still answers 204 and leaves every delivery target in place", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-9"}`)
		apiWantNoBody(t, status, data, 204)
		dashboard.wantFrames()
		pushed()
		apiWantSeededPushTargets(t, d)
	})

	t.Run("an endpoint padded with whitespace still names the stored target", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", owner,
			`{"endpoint":"  https://push.example.com/ep-1  "}`)
		apiWantNoBody(t, status, data, 204)
		dashboard.wantFrames()
		pushed()
		apiWantPushTargets(t, d, map[string]any{
			"endpoint": "https://push.example.com/ep-2", "p256dh": "fern",
			"pair": "bark", "expiration_time": nil,
		})
	})

	t.Run("a body that omits the endpoint answers 422 naming the missing field and removes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: endpoint")
		dashboard.wantFrames()
		pushed()
		apiWantSeededPushTargets(t, d)
	})

	t.Run("a DELETE /api/push/subscription request the wire layer rejects (a malformed JSON body) answers 422 without reaching the domain", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		dashboard.wantFrames()
		pushed()
		apiWantSeededPushTargets(t, d)
	})

	t.Run("a request carrying no credentials answers 401 and removes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", "",
			`{"endpoint":"https://push.example.com/ep-1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		pushed()
		apiWantSeededPushTargets(t, d)
	})

	t.Run("an authenticated agent identity answers 403 and removes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiSeedPushTargets(t, h, owner)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		pushed := apiTestWebPushSink(t, api)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/push/subscription", housekeeper,
			`{"endpoint":"https://push.example.com/ep-1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		pushed()
		apiWantSeededPushTargets(t, d)
	})
}
