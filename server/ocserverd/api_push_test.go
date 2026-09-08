// Tests for the observable push API and delivery outcomes.

package main

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const (
	pushTestVAPIDPrivateKey  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE"
	pushTestVAPIDPublicKey   = "BGsX0fLhLEJH-Lzm5WOkQPJ3A32BLeszoPShOUXYmMKWT-NC4v4af5uO5-tKfA-eFivOM1drMV7Oy7ZAaDe_UfU"
	pushTestSubscriptionAuth = "AAAAAAAAAAAAAAAAAAAAAA"
)

type pushTestRoundTripper func(*http.Request) (*http.Response, error)

func (f pushTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type pushTestHTTPClient struct {
	do func(*http.Request) (*http.Response, error)
}

func (c *pushTestHTTPClient) Do(r *http.Request) (*http.Response, error) {
	return c.do(r)
}

type pushTestDeliveryRequest struct {
	endpoint        string
	method          string
	contentType     string
	contentEncoding string
	ttl             string
	urgency         string
	authorization   string
	bodyLen         int
	readErr         error
	responseStatus  int
}

func TestPushVAPIDSubscriber(t *testing.T) {
	t.Run("the address saved through the settings API is returned for delivery", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
			`{"push_contact_email":"  owner@gofreight.com  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "push_contact_email", data["push_contact_email"], "owner@gofreight.com")
		if got := api.pushVAPIDSubscriber(); got != "owner@gofreight.com" {
			t.Fatalf("pushVAPIDSubscriber() = %q, want %q", got, "owner@gofreight.com")
		}
	})

	t.Run("an unset contact address is returned as empty", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		if got := api.pushVAPIDSubscriber(); got != "" {
			t.Fatalf("pushVAPIDSubscriber() = %q, want an empty address", got)
		}
	})
}

func TestValidatePushEndpoint(t *testing.T) {
	t.Run("a public HTTPS endpoint is accepted by the subscription API and becomes a target", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, http.MethodPost, "/api/push/subscription", owner,
			`{"endpoint":"https://push.example.com/ep-1","keys":{"p256dh":"vine","auth":"moss"}}`)
		apiWantNoBody(t, status, data, http.StatusNoContent)
		apiWantPushTargets(t, d, map[string]any{
			"endpoint": "https://push.example.com/ep-1", "p256dh": "vine",
			"pair": "moss", "expiration_time": nil,
		})
	})

	for _, tc := range []struct {
		name     string
		endpoint string
		message  string
	}{
		{name: "a non-HTTPS scheme", endpoint: "http://push.example.com/ep-1", message: "push endpoint must be an absolute HTTPS URL"},
		{name: "userinfo in the URL", endpoint: "https://owner:secret@push.example.com/ep-1", message: "push endpoint must be an absolute HTTPS URL"},
		{name: "the localhost hostname", endpoint: "https://localhost/ep-1", message: "push endpoint host is not public"},
		{name: "a private IPv4 address", endpoint: "https://192.168.1.9/ep-1", message: "push endpoint host is not public"},
		{name: "a shared carrier address", endpoint: "https://100.64.0.1/ep-1", message: "push endpoint host is not public"},
	} {
		tc := tc
		t.Run(tc.name+" is refused without storing a target", func(t *testing.T) {
			_, h, d, owner := newAPITestServer(t)

			status, data := apiJSON(t, h, http.MethodPost, "/api/push/subscription", owner,
				`{"endpoint":"`+tc.endpoint+`","keys":{"p256dh":"vine","auth":"moss"}}`)
			if status != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", tc.message)
			apiWantPushTargets(t, d)
		})
	}
}

func TestIsPublicPushIP(t *testing.T) {
	for _, tc := range []struct {
		name string
		ip   string
		want bool
	}{
		{name: "a public IPv4 address is allowed", ip: "8.8.8.8", want: true},
		{name: "a public IPv6 address is allowed", ip: "2001:4860:4860::8888", want: true},
		{name: "an RFC 1918 10 slash 8 address is refused", ip: "10.0.0.1"},
		{name: "an RFC 1918 172 slash 12 address is refused", ip: "172.16.0.1"},
		{name: "an RFC 1918 192 slash 16 address is refused", ip: "192.168.0.1"},
		{name: "a shared carrier address is refused", ip: "100.64.0.1"},
		{name: "an IPv4 loopback address is refused", ip: "127.0.0.1"},
		{name: "an IPv6 loopback address is refused", ip: "::1"},
		{name: "a link-local address is refused", ip: "169.254.1.1"},
		{name: "a unique-local IPv6 address is refused", ip: "fc00::1"},
		{name: "a multicast address is refused", ip: "224.0.0.1"},
		{name: "an IPv4-mapped private address is refused", ip: "::ffff:192.168.0.1"},
		{name: "an unspecified address is refused", ip: "0.0.0.0"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ip, err := netip.ParseAddr(tc.ip)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", tc.ip, err)
			}
			if got := isPublicPushIP(ip); got != tc.want {
				t.Fatalf("isPublicPushIP(%q) = %t, want %t", tc.ip, got, tc.want)
			}
		})
	}

	if got := isPublicPushIP(netip.Addr{}); got {
		t.Fatal("isPublicPushIP(an invalid address) = true, want false")
	}
}

func TestSafePushHTTPClient(t *testing.T) {
	t.Run("a loopback destination is refused before a connection is attempted", func(t *testing.T) {
		client := safePushHTTPClient()
		response, err := client.Get("http://127.0.0.1:1/push")
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("a loopback destination unexpectedly produced an HTTP response")
		}
		if got := err.Error(); got != `Get "http://127.0.0.1:1/push": push endpoint resolved to a non-public address` {
			t.Fatalf("loopback refusal = %q", got)
		}
	})

	t.Run("a redirect response is refused instead of following the new location", func(t *testing.T) {
		client := safePushHTTPClient()
		client.Transport = pushTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://push.example.com/ep-2"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    r,
			}, nil
		})
		req, err := http.NewRequest(http.MethodPost, "https://push.example.com/ep-1", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		response, err := client.Do(req)
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("a redirect response unexpectedly succeeded")
		}
		if got := err.Error(); got != `Post "https://push.example.com/ep-2": push endpoint redirects are not allowed` {
			t.Fatalf("redirect refusal = %q", got)
		}
	})
}

func TestWebPushClient(t *testing.T) {
	t.Run("an injected client is the outbound delivery client", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		called := false
		stub := &pushTestHTTPClient{do: func(r *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}}
		api.pushHTTPClient = stub

		client := api.webPushClient()
		req, err := http.NewRequest(http.MethodPost, "https://push.example.com/ep-1", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent || !called {
			t.Fatalf("configured client response = %d, called = %t", response.StatusCode, called)
		}
	})

	t.Run("without an injected client delivery still refuses a loopback destination", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		client := api.webPushClient()
		req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/push", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		response, err := client.Do(req)
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("the default delivery client unexpectedly accepted a loopback destination")
		}
		if got := err.Error(); got != `Post "http://127.0.0.1:1/push": push endpoint resolved to a non-public address` {
			t.Fatalf("loopback refusal = %q", got)
		}
	})
}

func TestPushPublicKey(t *testing.T) {
	t.Run("the public-key API derives the fixed browser key from the stored private key", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutSetting(settingPushVAPIDPrivateKey, pushTestVAPIDPrivateKey); err != nil {
			t.Fatalf("PutSetting: %v", err)
		}

		status, data := apiJSON(t, h, http.MethodGet, "/api/push/public-key", owner, "")
		if status != http.StatusOK {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"public_key": pushTestVAPIDPublicKey})
		stored, err := d.GetSetting(settingPushVAPIDPrivateKey)
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if stored == nil || *stored != pushTestVAPIDPrivateKey {
			t.Fatalf("stored VAPID private key = %v, want %q", stored, pushTestVAPIDPrivateKey)
		}
	})

	t.Run("a malformed stored private key answers an internal error and is not replaced", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutSetting(settingPushVAPIDPrivateKey, "!"); err != nil {
			t.Fatalf("PutSetting: %v", err)
		}

		status, data := apiJSON(t, h, http.MethodGet, "/api/push/public-key", owner, "")
		if status != http.StatusInternalServerError {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", "internal error: illegal base64 data at input byte 0")
		stored, err := d.GetSetting(settingPushVAPIDPrivateKey)
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if stored == nil || *stored != "!" {
			t.Fatalf("stored malformed VAPID private key = %v, want %q", stored, "!")
		}
	})
}

func TestPushVAPIDKeys(t *testing.T) {
	t.Run("the delivery key pair is the stored private key and its matching public point", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutSetting(settingPushVAPIDPrivateKey, pushTestVAPIDPrivateKey); err != nil {
			t.Fatalf("PutSetting: %v", err)
		}

		publicKey, privateKey, err := api.pushVAPIDKeys()
		if err != nil {
			t.Fatalf("pushVAPIDKeys: %v", err)
		}
		if publicKey != pushTestVAPIDPublicKey {
			t.Fatalf("public key = %q, want %q", publicKey, pushTestVAPIDPublicKey)
		}
		if privateKey != pushTestVAPIDPrivateKey {
			t.Fatalf("private key = %q, want %q", privateKey, pushTestVAPIDPrivateKey)
		}

		publicKeyAgain, privateKeyAgain, err := api.pushVAPIDKeys()
		if err != nil {
			t.Fatalf("pushVAPIDKeys second read: %v", err)
		}
		if publicKeyAgain != pushTestVAPIDPublicKey || privateKeyAgain != pushTestVAPIDPrivateKey {
			t.Fatalf("second key read = %q / %q, want the same fixed pair", publicKeyAgain, privateKeyAgain)
		}
	})
}

func TestEnqueueWebPush(t *testing.T) {
	t.Run("the configured push sink receives the complete notification payload", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		pushed := apiTestWebPushSink(t, api)
		api.enqueueWebPush(webPushPayload{
			Kind: "reply_card", ChatID: "chat-1", ChatPeerID: "mira", ReplyCardID: "rc-1",
			Title: "Owner decision", Body: "A decision is waiting.", NeedsDecision: true,
		})
		pushed(map[string]any{
			"kind": "reply_card", "chat_id": "chat-1", "chat_peer_id": "mira", "reply_card_id": "rc-1",
			"title": "Owner decision", "body": "A decision is waiting.", "needs_decision": true,
		})
	})

	t.Run("expired delivery targets are pruned while an accepted target remains", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, http.MethodPatch, "/api/settings", owner,
			`{"push_contact_email":"owner@gofreight.com"}`)
		if status != http.StatusOK {
			t.Fatalf("configure push contact: %d (%v)", status, data)
		}
		if err := d.PutSetting(settingPushVAPIDPrivateKey, pushTestVAPIDPrivateKey); err != nil {
			t.Fatalf("PutSetting: %v", err)
		}

		endpoints := []string{
			"https://push.example.com/ep-1",
			"https://push.example.com/ep-2",
		}
		for _, endpoint := range endpoints {
			status, data := apiJSON(t, h, http.MethodPost, "/api/push/subscription", owner,
				`{"endpoint":"`+endpoint+`","keys":{"p256dh":"`+pushTestVAPIDPublicKey+`","auth":"`+pushTestSubscriptionAuth+`"}}`)
			apiWantNoBody(t, status, data, http.StatusNoContent)
		}

		responseStatuses := map[string]int{
			"/ep-1": http.StatusGone,
			"/ep-2": http.StatusCreated,
		}
		calls := make(chan pushTestDeliveryRequest, len(endpoints))
		api.pushHTTPClient = &pushTestHTTPClient{do: func(r *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(r.Body)
			status := responseStatuses[r.URL.Path]
			calls <- pushTestDeliveryRequest{
				endpoint:        r.URL.String(),
				method:          r.Method,
				contentType:     r.Header.Get("Content-Type"),
				contentEncoding: r.Header.Get("Content-Encoding"),
				ttl:             r.Header.Get("TTL"),
				urgency:         r.Header.Get("Urgency"),
				authorization:   r.Header.Get("Authorization"),
				bodyLen:         len(body),
				readErr:         readErr,
				responseStatus:  status,
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}}

		api.enqueueWebPush(webPushPayload{
			Kind: "chat", ChatID: "chat-2", ChatPeerID: "mira",
			Title: "New message", Body: "Please review this.",
		})

		seen := make(map[string]pushTestDeliveryRequest, len(endpoints))
		for range endpoints {
			select {
			case call := <-calls:
				if _, exists := seen[call.endpoint]; exists {
					t.Fatalf("delivery target %q was called twice", call.endpoint)
				}
				seen[call.endpoint] = call
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for the two delivery requests")
			}
		}

		for endpoint, wantStatus := range map[string]int{
			endpoints[0]: http.StatusGone,
			endpoints[1]: http.StatusCreated,
		} {
			call, ok := seen[endpoint]
			if !ok {
				t.Fatalf("no delivery request for %q; got %v", endpoint, seen)
			}
			if call.method != http.MethodPost || call.contentType != "application/octet-stream" ||
				call.contentEncoding != "aes128gcm" || call.ttl != "60" || call.urgency != "high" {
				t.Fatalf("delivery request for %q has unexpected HTTP fields: %#v", endpoint, call)
			}
			if call.bodyLen == 0 || call.readErr != nil {
				t.Fatalf("delivery request for %q body length/error = %d/%v", endpoint, call.bodyLen, call.readErr)
			}
			if !strings.HasPrefix(call.authorization, "vapid t=") {
				t.Fatalf("delivery request for %q has no VAPID authorization: %q", endpoint, call.authorization)
			}
			if call.responseStatus != wantStatus {
				t.Fatalf("delivery response for %q = %d, want %d", endpoint, call.responseStatus, wantStatus)
			}
		}

		deadline := time.Now().Add(2 * time.Second)
		for {
			rows, err := d.ListPushSubscriptions()
			if err != nil {
				t.Fatalf("ListPushSubscriptions: %v", err)
			}
			if len(rows) == 1 && rows[0].Endpoint == endpoints[1] {
				break
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("expired target was not pruned; targets = %#v", rows)
			}
			time.Sleep(time.Millisecond)
		}
		apiWantPushTargets(t, d, map[string]any{
			"endpoint": endpoints[1], "p256dh": pushTestVAPIDPublicKey,
			"pair": pushTestSubscriptionAuth, "expiration_time": nil,
		})
	})
}

func TestPushDeliveryStatusClass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{name: "a 200 response is accepted", status: http.StatusOK, want: "accepted"},
		{name: "a 299 response is accepted", status: 299, want: "accepted"},
		{name: "a 404 response is expired", status: http.StatusNotFound, want: "expired"},
		{name: "a 410 response is expired", status: http.StatusGone, want: "expired"},
		{name: "a 400 response is rejected", status: http.StatusBadRequest, want: "rejected"},
		{name: "a 499 response is rejected", status: 499, want: "rejected"},
		{name: "a 500 response is a gateway error", status: http.StatusInternalServerError, want: "gateway_error"},
		{name: "a 599 response is a gateway error", status: 599, want: "gateway_error"},
		{name: "a 300 response is unexpected", status: http.StatusMultipleChoices, want: "unexpected"},
		{name: "a 600 response is unexpected", status: 600, want: "unexpected"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := pushDeliveryStatusClass(tc.status); got != tc.want {
				t.Fatalf("pushDeliveryStatusClass(%d) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestPushDeliveryErrorClass(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "a wrapped deadline is a timeout", err: fmt.Errorf("push: %w", context.DeadlineExceeded), want: "timeout"},
		{name: "a DNS transport error is a network error", err: &net.DNSError{Err: "no such host", Name: "push.example.com"}, want: "network"},
		{name: "an application error is a send error", err: errors.New("invalid VAPID key"), want: "send_error"},
		{name: "a nil error still has the generic send classification", err: nil, want: "send_error"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := pushDeliveryErrorClass(tc.err); got != tc.want {
				t.Fatalf("pushDeliveryErrorClass(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
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
