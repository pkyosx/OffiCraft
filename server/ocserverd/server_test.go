// Skeleton generated from server/ocserverd/server.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"
)

type serverTestKeepAliveConn struct {
	net.Conn
	config net.KeepAliveConfig
	err    error
}

func (c *serverTestKeepAliveConn) SetKeepAliveConfig(config net.KeepAliveConfig) error {
	c.config = config
	return c.err
}

type serverTestListener struct {
	net.Listener
	conn  net.Conn
	err   error
	calls int
}

func (l *serverTestListener) Accept() (net.Conn, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return l.conn, nil
}

func TestGitSHA(t *testing.T) {
	old := buildSHA
	t.Cleanup(func() { buildSHA = old })

	t.Run("a stamped build sha is returned verbatim", func(t *testing.T) {
		buildSHA = "release-sha-125"
		if got := gitSHA(); got != "release-sha-125" {
			t.Fatalf("gitSHA() = %q, want %q", got, "release-sha-125")
		}
	})

	t.Run("an unstamped checkout returns its measured short sha", func(t *testing.T) {
		buildSHA = ""
		want, err := gitOutput("rev-parse", "--short", "HEAD")
		if err != nil {
			t.Fatalf("gitOutput: %v", err)
		}
		if got := gitSHA(); got != want {
			t.Fatalf("gitSHA() = %q, want the checkout's measured sha %q", got, want)
		}
	})

	t.Run("an unavailable checkout returns unknown", func(t *testing.T) {
		buildSHA = ""
		t.Setenv("PATH", t.TempDir())
		if got := gitSHA(); got != "unknown" {
			t.Fatalf("gitSHA() = %q, want %q", got, "unknown")
		}
	})
}

func TestGitTime(t *testing.T) {
	old := buildTime
	t.Cleanup(func() { buildTime = old })

	t.Run("a stamped build time is returned verbatim", func(t *testing.T) {
		buildTime = "2026-09-08T12:34:56+08:00"
		if got := gitTime(); got != "2026-09-08T12:34:56+08:00" {
			t.Fatalf("gitTime() = %q, want the stamped value", got)
		}
	})

	t.Run("an unstamped checkout returns its measured commit time", func(t *testing.T) {
		buildTime = ""
		want, err := gitOutput("show", "-s", "--format=%cI", "HEAD")
		if err != nil {
			t.Fatalf("gitOutput: %v", err)
		}
		if got := gitTime(); got != want {
			t.Fatalf("gitTime() = %q, want the checkout's measured time %q", got, want)
		}
	})

	t.Run("an unavailable checkout returns an empty time", func(t *testing.T) {
		buildTime = ""
		t.Setenv("PATH", t.TempDir())
		if got := gitTime(); got != "" {
			t.Fatalf("gitTime() = %q, want an empty value", got)
		}
	})
}

func TestGitOutput(t *testing.T) {
	t.Run("successful git output is trimmed", func(t *testing.T) {
		got, err := gitOutput("rev-parse", "--short", "HEAD")
		if err != nil {
			t.Fatalf("gitOutput: %v", err)
		}
		if got == "" || got != strings.TrimSpace(got) {
			t.Fatalf("gitOutput() = %q, want non-empty trimmed output", got)
		}
	})

	t.Run("a git failure returns its empty output and error", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		got, err := gitOutput("rev-parse", "--short", "HEAD")
		if got != "" {
			t.Fatalf("gitOutput output = %q, want empty output", got)
		}
		if err == nil {
			t.Fatal("gitOutput error = nil, want the git failure")
		}
	})
}

func TestWriteJSON(t *testing.T) {
	t.Run("a serialisable body is written as the JSON answer", func(t *testing.T) {
		rec := httptest.NewRecorder()

		writeJSON(rec, 201, map[string]string{"status": "restarting"})

		if rec.Code != 201 {
			t.Fatalf("want 201, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != `{"status":"restarting"}` {
			t.Fatalf("body: %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type: %q", got)
		}
	})

	t.Run("a body JSON cannot carry answers 500 in plain text", func(t *testing.T) {
		rec := httptest.NewRecorder()

		writeJSON(rec, 200, make(chan int))

		if rec.Code != 500 {
			t.Fatalf("want 500, got %d", rec.Code)
		}
		if got := rec.Body.String(); got != "internal server error\n" {
			t.Fatalf("body: %q", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Fatalf("content-type: %q", got)
		}
	})
}

func TestErrorCodeForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{status: 400, want: "validation_error"},
		{status: 422, want: "validation_error"},
		{status: 401, want: "unauthorized"},
		{status: 403, want: "forbidden"},
		{status: 404, want: "not_found"},
		{status: 405, want: "method_not_allowed"},
		{status: 409, want: "conflict"},
		{status: 503, want: "service_unavailable"},
		{status: 500, want: "internal_error"},
		{status: 418, want: "client_error"},
		{status: 600, want: "internal_error"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			if got := errorCodeForStatus(tc.status); got != tc.want {
				t.Fatalf("errorCodeForStatus(%d) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()

	writeError(rec, http.StatusUnprocessableEntity, "the title is required")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := rec.Body.String(); got != `{"error":{"code":"validation_error","message":"the title is required"}}` {
		t.Fatalf("body = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
}

func TestClaimsFromContext(t *testing.T) {
	want := map[string]any{"sub": "kip", "scope": "agent"}
	ctx := context.WithValue(context.Background(), claimsContextKey, want)
	got := claimsFromContext(ctx)
	if got == nil || len(got) != len(want) || got["sub"] != "kip" || got["scope"] != "agent" {
		t.Fatalf("claimsFromContext() = %#v, want %#v", got, want)
	}

	t.Run("an absent claim value is nil", func(t *testing.T) {
		if got := claimsFromContext(context.Background()); got != nil {
			t.Fatalf("claimsFromContext() = %#v, want nil", got)
		}
	})

	t.Run("a non-map claim value is nil", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), claimsContextKey, "not-claims")
		if got := claimsFromContext(ctx); got != nil {
			t.Fatalf("claimsFromContext() = %#v, want nil", got)
		}
	})
}

func TestVerifyingKeyFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), verifyingKeyContextKey, "k-legacy")
	if got := verifyingKeyFromContext(ctx); got != "k-legacy" {
		t.Fatalf("verifyingKeyFromContext() = %q, want %q", got, "k-legacy")
	}

	t.Run("an absent key is empty", func(t *testing.T) {
		if got := verifyingKeyFromContext(context.Background()); got != "" {
			t.Fatalf("verifyingKeyFromContext() = %q, want empty", got)
		}
	})

	t.Run("a non-string key is empty", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), verifyingKeyContextKey, 17)
		if got := verifyingKeyFromContext(ctx); got != "" {
			t.Fatalf("verifyingKeyFromContext() = %q, want empty", got)
		}
	})
}

func TestExtractToken(t *testing.T) {
	cases := []struct {
		name   string
		auth   string
		target string
		want   string
	}{
		{name: "bearer header", auth: "Bearer jwt-token", target: "/?token=query-token", want: "jwt-token"},
		{name: "case-insensitive bearer with padding", auth: "bEaReR   padded-token", target: "/", want: "padded-token"},
		{name: "bare authorization value", auth: "raw-token", target: "/", want: "raw-token"},
		{name: "non-bearer authorization value", auth: "Basic abc", target: "/?token=query-token", want: "Basic abc"},
		{name: "query fallback", target: "/?token=query-token", want: "query-token"},
		{name: "header wins over query", auth: "Bearer header-token", target: "/?token=query-token", want: "header-token"},
		{name: "empty bearer does not fall back", auth: "Bearer ", target: "/?token=query-token", want: "Bearer"},
		{name: "no credentials", target: "/", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			if got := extractToken(r); got != tc.want {
				t.Fatalf("extractToken() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequireAuth(t *testing.T) {
	api, _, d, owner := newAPITestServer(t)
	agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

	t.Run("a missing key ring is a configured-auth refusal", func(t *testing.T) {
		called := 0
		h := requireAuth(nil, func() int64 { return 0 }, d.GetMember, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":{"code":"unauthorized","message":"auth not configured"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if called != 0 {
			t.Fatalf("next called %d times, want 0", called)
		}
	})

	t.Run("missing credentials do not reach the handler", func(t *testing.T) {
		called := 0
		h := requireAuth(api.keys, func() int64 { return 0 }, d.GetMember, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":{"code":"unauthorized","message":"missing credentials"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if called != 0 {
			t.Fatalf("next called %d times, want 0", called)
		}
	})

	t.Run("an invalid token is rejected without reaching the handler", func(t *testing.T) {
		called := 0
		h := requireAuth(api.keys, func() int64 { return 0 }, d.GetMember, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer not-a-jwt")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":{"code":"unauthorized","message":"invalid token"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if called != 0 {
			t.Fatalf("next called %d times, want 0", called)
		}
	})

	t.Run("a query token reaches the handler with claims and verifying key", func(t *testing.T) {
		called := 0
		var gotClaims map[string]any
		var gotKey string
		h := requireAuth(api.keys, func() int64 { return 0 }, d.GetMember, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			gotClaims = claimsFromContext(r.Context())
			gotKey = verifyingKeyFromContext(r.Context())
			writeJSON(w, http.StatusOK, map[string]string{"result": "accepted"})
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?token="+agent, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != `{"result":"accepted"}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if called != 1 {
			t.Fatalf("next called %d times, want 1", called)
		}
		if gotClaims == nil || len(gotClaims) != 4 || gotClaims["sub"] != "kip" || gotClaims["scope"] != "agent" {
			t.Fatalf("claims = %#v, want the four-field agent claims for %q", gotClaims, "kip")
		}
		if _, ok := gotClaims["iat"].(float64); !ok {
			t.Fatalf("claims iat = %#v, want a JSON number", gotClaims["iat"])
		}
		if _, ok := gotClaims["exp"].(float64); !ok {
			t.Fatalf("claims exp = %#v, want a JSON number", gotClaims["exp"])
		}
		if gotKey != "k-legacy" {
			t.Fatalf("verifying key = %q, want %q", gotKey, "k-legacy")
		}
	})

	t.Run("a token below the owner floor is rejected", func(t *testing.T) {
		called := 0
		h := requireAuth(api.keys, func() int64 { return time.Now().Unix() + 1 }, d.GetMember, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+owner)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":{"code":"unauthorized","message":"invalid token"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if called != 0 {
			t.Fatalf("next called %d times, want 0", called)
		}
	})
}

func TestShareSigGate(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		auth       string
		keys       *keyring
		wantStatus int
		wantBody   string
		wantRaw    int
		wantAuthed int
		wantVerify int
	}{
		{name: "valid signature serves raw", target: "/?sig=ok", keys: singleKeyring([]byte("secret")), wantStatus: http.StatusOK, wantBody: `{"route":"raw"}`, wantRaw: 1, wantVerify: 1},
		{name: "invalid signature is unauthorized", target: "/?sig=bad", keys: singleKeyring([]byte("secret")), wantStatus: http.StatusUnauthorized, wantBody: `{"error":{"code":"unauthorized","message":"invalid signature"}}`, wantVerify: 1},
		{name: "no signature follows authed chain", target: "/", keys: singleKeyring([]byte("secret")), wantStatus: http.StatusUnauthorized, wantBody: `{"error":{"code":"unauthorized","message":"authed path"}}`, wantAuthed: 1},
		{name: "bearer takes precedence over valid signature", target: "/?sig=ok", auth: "Bearer any-token", keys: singleKeyring([]byte("secret")), wantStatus: http.StatusUnauthorized, wantBody: `{"error":{"code":"unauthorized","message":"authed path"}}`, wantAuthed: 1},
		{name: "query token takes precedence over valid signature", target: "/?token=any-token&sig=ok", keys: singleKeyring([]byte("secret")), wantStatus: http.StatusUnauthorized, wantBody: `{"error":{"code":"unauthorized","message":"authed path"}}`, wantAuthed: 1},
		{name: "a nil key ring cannot verify a signature", target: "/?sig=ok", keys: nil, wantStatus: http.StatusUnauthorized, wantBody: `{"error":{"code":"unauthorized","message":"invalid signature"}}`, wantVerify: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawCalls, authedCalls, verifyCalls := 0, 0, 0
			raw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rawCalls++
				writeJSON(w, http.StatusOK, map[string]string{"route": "raw"})
			})
			authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authedCalls++
				writeError(w, http.StatusUnauthorized, "authed path")
			})
			verify := func(keys *keyring, r *http.Request, sig string) bool {
				verifyCalls++
				return sig == "ok"
			}
			h := shareSigGate(tc.keys, verify, raw, authed)
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus || rec.Body.String() != tc.wantBody {
				t.Fatalf("response = %d %q, want %d %q", rec.Code, rec.Body.String(), tc.wantStatus, tc.wantBody)
			}
			if rawCalls != tc.wantRaw || authedCalls != tc.wantAuthed || verifyCalls != tc.wantVerify {
				t.Fatalf("calls raw=%d authed=%d verify=%d, want %d/%d/%d", rawCalls, authedCalls, verifyCalls, tc.wantRaw, tc.wantAuthed, tc.wantVerify)
			}
		})
	}
}

func TestBuildHandler(t *testing.T) {
	api, _, d, owner := newAPITestServer(t)
	agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
	publicCalls, adminCalls := 0, 0
	specs := []RouteSpec{
		Public(routeDef{
			Method: http.MethodGet,
			Path:   "/server-test-public",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				publicCalls++
				writeJSON(w, http.StatusOK, map[string]string{"route": "public"})
			},
		}).RouteSpec,
		Gated(principalAdminAgent, routeDef{
			Method: http.MethodGet,
			Path:   "/server-test-admin",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				adminCalls++
				writeJSON(w, http.StatusOK, map[string]string{"route": "admin"})
			},
		}).RouteSpec,
	}
	h, err := buildHandler(specs, api.keys, d.GetMember, api.authPasswordChangedAt)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	t.Run("public route is served anonymously", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodGet, "/server-test-public", "", "")
		if rec.Code != http.StatusOK || rec.Body.String() != `{"route":"public"}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if publicCalls != 1 {
			t.Fatalf("public handler calls = %d, want 1", publicCalls)
		}
	})

	t.Run("gated route rejects an anonymous request", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodGet, "/server-test-admin", "", "")
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":{"code":"unauthorized","message":"missing credentials"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if adminCalls != 0 {
			t.Fatalf("admin handler calls = %d, want 0", adminCalls)
		}
	})

	t.Run("the principal choke rejects a plain agent", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodGet, "/server-test-admin", agent, "")
		if rec.Code != http.StatusForbidden || rec.Body.String() != `{"error":{"code":"forbidden","message":"principal not permitted"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if adminCalls != 0 {
			t.Fatalf("admin handler calls = %d, want 0", adminCalls)
		}
	})

	t.Run("the owner reaches the gated handler", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodGet, "/server-test-admin", owner, "")
		if rec.Code != http.StatusOK || rec.Body.String() != `{"route":"admin"}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
		if adminCalls != 1 {
			t.Fatalf("admin handler calls = %d, want 1", adminCalls)
		}
	})

	t.Run("an unknown api path is a JSON not-found", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodGet, "/api/server-test-missing", "", "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":{"code":"not_found","message":"not found"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("a wrong method on a path-shaped route is method-not-allowed", func(t *testing.T) {
		rec := apiRequest(t, h, http.MethodPost, "/server-test-public", "", "{}")
		if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != `{"error":{"code":"method_not_allowed","message":"method not allowed"}}` {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
	})
}

func TestSpecsFor(t *testing.T) {
	api, _, d, owner := newAPITestServer(t)
	specs := specsFor(api)
	if len(specs) != 188 {
		t.Fatalf("specsFor returned %d routes, want 188", len(specs))
	}
	if api.catalogHash != "040d782e97b3481b" {
		t.Fatalf("catalogHash = %q, want %q", api.catalogHash, "040d782e97b3481b")
	}
	if len(api.mcpTools) != 132 {
		t.Fatalf("MCP tool index has %d entries, want 132", len(api.mcpTools))
	}
	if got, ok := api.mcpTools["get_version"]; !ok || got.Method != http.MethodGet || got.Path != "/api/version" {
		t.Fatalf("get_version = %#v, present=%v", got, ok)
	}
	if _, ok := api.mcpTools["get_health"]; ok {
		t.Fatal("the excluded health probe appeared in the MCP tool index")
	}
	h, err := buildHandler(specs, api.keys, d.GetMember, api.authPasswordChangedAt)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	rec := apiRequest(t, h, http.MethodGet, "/api/document-history/global_context/global/not-an-id", owner, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if rec.Body.String() != `{"error":{"code":"validation_error","message":"Invalid format for parameter id: error binding string parameter: strconv.ParseInt: parsing \"not-an-id\": invalid syntax"}}` {
		t.Fatalf("invalid path parameter body = %q", rec.Body.String())
	}
}

func TestNewAPIServer(t *testing.T) {
	oldSHA, oldTime := buildSHA, buildTime
	t.Cleanup(func() {
		buildSHA = oldSHA
		buildTime = oldTime
	})
	buildSHA = "server-test-sha"
	buildTime = "2026-09-08T12:34:56+08:00"

	d := newAPITestDAL(t)
	frame := []byte("update-frame")
	if err := d.PutWardenCommand(WardenCommand{
		WardenID:   "warden-test",
		Verb:       "update",
		MemberID:   "member-test",
		Frame:      frame,
		EnqueuedTS: float64(time.Now().Unix()),
	}); err != nil {
		t.Fatalf("PutWardenCommand: %v", err)
	}
	hub := NewHub()
	keys := singleKeyring([]byte("server-test-secret"))
	api := newAPIServer(d, hub, keys, 1234, "/server-root")
	buildSHA = "changed-after-construction"
	buildTime = "changed-after-construction"

	if api.processSHA != "server-test-sha" || api.processTime != "2026-09-08T12:34:56+08:00" {
		t.Fatalf("process identity = %q / %q", api.processSHA, api.processTime)
	}
	if api.dal != d || api.hub != hub || api.keys != keys || api.ownerTokenTTL != 1234 || api.root != "/server-root" {
		t.Fatalf("carrier dependencies = dal:%v hub:%v keys:%v ttl:%d root:%q", api.dal == d, api.hub == hub, api.keys == keys, api.ownerTokenTTL, api.root)
	}
	if api.agentTokenTTL != 604800 || api.telemetry == nil || api.gauge == nil || api.machineClaims == nil {
		t.Fatalf("constructor defaults are incomplete: agent ttl=%d telemetry=%v gauge=%v claims=%v", api.agentTokenTTL, api.telemetry != nil, api.gauge != nil, api.machineClaims != nil)
	}
	if api.suggestedRepliesReplyCard == nil || len(api.suggestedRepliesReplyCard) != 0 || api.suggestedRepliesTaskMessage == nil || len(api.suggestedRepliesTaskMessage) != 0 {
		t.Fatalf("suggested reply defaults = %#v / %#v", api.suggestedRepliesReplyCard, api.suggestedRepliesTaskMessage)
	}
	got := hub.DrainWardenCommands("warden-test")
	if len(got) != 1 || got[0].Subject != "member-test" || string(got[0].Frame) != string(frame) {
		t.Fatalf("rehydrated commands = %#v, want one member-test update frame", got)
	}
}

func TestApplyKeepAlive(t *testing.T) {
	want := net.KeepAliveConfig{Enable: true, Idle: 15 * time.Second, Interval: 5 * time.Second, Count: 3}

	t.Run("a keep-alive connection receives the full config", func(t *testing.T) {
		conn := &serverTestKeepAliveConn{}
		applyKeepAlive(conn)
		if conn.config != want {
			t.Fatalf("keep-alive config = %#v, want %#v", conn.config, want)
		}
	})

	t.Run("a non-keep-alive connection is left usable", func(t *testing.T) {
		left, right := net.Pipe()
		t.Cleanup(func() {
			left.Close()
			right.Close()
		})
		applyKeepAlive(left)
	})

	t.Run("a socket option error does not escape", func(t *testing.T) {
		conn := &serverTestKeepAliveConn{err: errors.New("keep-alive unavailable")}
		applyKeepAlive(conn)
		if conn.config != want {
			t.Fatalf("keep-alive config = %#v, want %#v", conn.config, want)
		}
	})
}

func TestAccept(t *testing.T) {
	want := net.KeepAliveConfig{Enable: true, Idle: 15 * time.Second, Interval: 5 * time.Second, Count: 3}

	t.Run("an accepted connection is configured before return", func(t *testing.T) {
		conn := &serverTestKeepAliveConn{}
		base := &serverTestListener{conn: conn}
		wrapped := keepAliveListener{Listener: base}
		got, err := wrapped.Accept()
		if err != nil || got != conn {
			t.Fatalf("Accept() = %v, %v; want the accepted connection", got, err)
		}
		if base.calls != 1 {
			t.Fatalf("underlying Accept calls = %d, want 1", base.calls)
		}
		if conn.config != want {
			t.Fatalf("keep-alive config = %#v, want %#v", conn.config, want)
		}
	})

	t.Run("an underlying accept error is returned unchanged", func(t *testing.T) {
		wantErr := errors.New("listener closed")
		base := &serverTestListener{err: wantErr}
		wrapped := keepAliveListener{Listener: base}
		got, err := wrapped.Accept()
		if got != nil || !errors.Is(err, wantErr) {
			t.Fatalf("Accept() = %v, %v; want nil and %v", got, err, wantErr)
		}
		if base.calls != 1 {
			t.Fatalf("underlying Accept calls = %d, want 1", base.calls)
		}
	})
}

func TestCmdServe(t *testing.T) {
	w := newServeWorld(t)
	if got := w.entries(t); len(got) != 0 {
		t.Fatalf("fresh serve world is not empty: %v", got)
	}
	var out strings.Builder
	rc := cmdServe(w.env, true, true, &out)
	if rc != 1 {
		t.Fatalf("cmdServe returned %d, want the held-port refusal", rc)
	}
	if got := w.entries(t); len(got) == 0 {
		t.Fatalf("cmdServe did not touch the configured database; output:\n%s", out.String())
	}
	for _, want := range []string{
		"[ocserverd] --no-reconcile: reconcile producer disabled",
		"[ocserverd] --no-outsource: outsource-assignment scheduler disabled",
		"already in use",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("cmdServe output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "ocserverd serving on ") {
		t.Fatalf("cmdServe announced a server after bind failure:\n%s", out.String())
	}
}

func TestBindErrorMessage(t *testing.T) {
	t.Run("address in use names both the port and remedies", func(t *testing.T) {
		got := bindErrorMessage(8775, fmt.Errorf("listen: %w", syscall.EADDRINUSE))
		want := "port 8775 already in use — another process (very likely another officraft server) holds it. Free it, or move this instance: set [server].port in oc.toml, or OC_SERVE_PORT=<other>. Find the holder with: lsof -nP -iTCP:8775 -sTCP:LISTEN"
		if got != want {
			t.Fatalf("bindErrorMessage() = %q, want %q", got, want)
		}
	})

	t.Run("other errors retain their cause", func(t *testing.T) {
		got := bindErrorMessage(8776, errors.New("permission denied"))
		want := "cannot bind port 8776: permission denied"
		if got != want {
			t.Fatalf("bindErrorMessage() = %q, want %q", got, want)
		}
	})
}
