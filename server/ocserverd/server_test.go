package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func okHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthDTO{Status: "ok"})
}

// ── probes: byte-level parity with the Python responses ─────────────────────

func TestHealthProbeBytesMatchPython(t *testing.T) {
	h, err := buildHandler(defaultRouteSpecs(), []byte(interopSecret), nil, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status/content-type: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// The exact bytes the retired Python original answered (compact JSON) —
	// frozen wire shape.
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("body diverges from the Python probe: %q", body)
	}
}

func TestVersionProbeShapeMatchesPython(t *testing.T) {
	h, err := buildHandler(defaultRouteSpecs(), []byte(interopSecret), nil, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	// Key ORDER is part of the byte-level contract (pydantic field order):
	// version, git_sha, git_time, catalog_hash, update_available, latest_version,
	// then the additive release_tag (T-e9d1) last.
	wantOrder := []string{`"version":`, `"git_sha":`, `"git_time":`, `"catalog_hash":`, `"update_available":`, `"latest_version":`, `"release_tag":`}
	pos := -1
	for _, key := range wantOrder {
		i := strings.Index(string(body), key)
		if i <= pos {
			t.Fatalf("key %s out of order (or missing) in %s", key, body)
		}
		pos = i
	}
	var dto struct {
		Version         string  `json:"version"`
		GitSHA          string  `json:"git_sha"`
		GitTime         *string `json:"git_time"`
		CatalogHash     string  `json:"catalog_hash"`
		UpdateAvailable bool    `json:"update_available"`
		LatestVersion   *string `json:"latest_version"`
		ReleaseTag      *string `json:"release_tag"`
	}
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if dto.Version != "0.0.0" {
		t.Fatalf("version must stay 0.0.0 (M1 §3.9): %q", dto.Version)
	}
	// In this repo checkout the runtime git capture must yield the 7-char short
	// sha + an ISO time (outside a checkout they honestly degrade to
	// unknown/null, mirroring handlers.git_sha/git_time).
	if len(dto.GitSHA) != 7 {
		t.Fatalf("git_sha must be the 7-char short sha in a checkout: %q", dto.GitSHA)
	}
	if dto.GitTime == nil || !strings.Contains(*dto.GitTime, "T") {
		t.Fatalf("git_time must be ISO-8601 in a checkout: %v", dto.GitTime)
	}
	// The derived catalog hash (handlers.current_catalog_hash): 16 lowercase
	// hex chars over the non-mcp_exclude route surface.
	if len(dto.CatalogHash) != 16 {
		t.Fatalf("catalog_hash must be the 16-hex derived hash: %q", dto.CatalogHash)
	}
	if dto.UpdateAvailable || dto.LatestVersion != nil {
		t.Fatalf("update_available/latest_version must be false/null: %s", body)
	}
	// No updater configured → the running build's r-N is unknown (honest null).
	if dto.ReleaseTag != nil {
		t.Fatalf("release_tag must be null with no updater configured: %v", dto.ReleaseTag)
	}
	if !strings.Contains(string(body), `"latest_version":null`) {
		t.Fatalf("latest_version must serialise as null (not omitted): %s", body)
	}
}

func TestGitSHAPrefersStampedBuildIdentity(t *testing.T) {
	origSHA, origTime := buildSHA, buildTime
	t.Cleanup(func() { buildSHA, buildTime = origSHA, origTime })

	buildSHA, buildTime = "abc1234", "2026-07-12T00:00:00+08:00"
	if got := gitSHA(); got != "abc1234" {
		t.Fatalf("a stamped buildSHA must win over the CWD probe: %q", got)
	}
	if got := gitTime(); got != "2026-07-12T00:00:00+08:00" {
		t.Fatalf("a stamped buildTime must win over the CWD probe: %q", got)
	}

	// Unstamped (plain `go build`) keeps the checkout probe alive.
	buildSHA, buildTime = "", ""
	if got := gitSHA(); len(got) != 7 {
		t.Fatalf("unstamped gitSHA must probe the checkout's 7-char sha: %q", got)
	}
}

// TestBindErrorMessageIsActionable pins the port-clash FATAL. The bare Go error
// ("bind: address already in use") states the fact but not the fix; the operator
// needs to know it is a port clash AND how to get out of it. Uses a REAL double
// bind so the errors.Is(…, syscall.EADDRINUSE) unwrap is exercised end to end
// (net.OpError → os.SyscallError → syscall.Errno), not a hand-made sentinel.
func TestBindErrorMessageIsActionable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	clash, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		clash.Close()
		t.Fatal("second bind on the same port must fail")
	}
	msg := bindErrorMessage(port, err)
	for _, want := range []string{
		fmt.Sprintf("port %d already in use", port),
		"officraft server",
		"OC_SERVE_PORT=<other>",
		"[server].port in oc.toml",
		"lsof",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("EADDRINUSE message must be actionable (missing %q): %s", want, msg)
		}
	}

	// A non-EADDRINUSE failure must NOT be dressed up as a port clash.
	other := bindErrorMessage(8770, errors.New("permission denied"))
	if strings.Contains(other, "already in use") {
		t.Fatalf("only EADDRINUSE may claim a port clash: %s", other)
	}
	if !strings.Contains(other, "cannot bind port 8770") {
		t.Fatalf("non-clash bind errors must still name the port: %s", other)
	}
}

// ── auth middleware + RBAC choke (over a synthetic gated table) ──────────────

func gatedSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Path: "/api/floor", Handler: okHandler, Auth: authGated,
			Requires: principalMachine, Summary: "floor: any authenticated principal"},
		{Method: "GET", Path: "/api/admin", Handler: okHandler, Auth: authGated,
			Requires: principalAdminAgent, Summary: "admin choke"},
		{Method: "GET", Path: "/api/owner-only", Handler: okHandler, Auth: authGated,
			Requires: principalOwner, Summary: "owner choke"},
	}
}

func get(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(body)
}

func TestGatedRoutesFailClosed(t *testing.T) {
	secret := []byte(interopSecret)
	h, err := buildHandler(gatedSpecs(), secret, nil, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	expiredTok, _ := mintJWT("kyle", "agent", 1, secret, now-300, "")
	forgedTok, _ := mintJWT("owner", "owner", 300, []byte("wrong-secret"), now, "")

	// 401 deny-by-default: no / malformed / expired / forged credentials.
	for _, tok := range []string{"", "garbage", expiredTok, forgedTok} {
		status, body := get(t, srv.URL+"/api/floor", tok)
		if status != 401 || !strings.Contains(body, `"code":"unauthorized"`) {
			t.Fatalf("token %q: want 401 unauthorized envelope, got %d %s", tok, status, body)
		}
	}

	// The machine FLOOR admits any authenticated principal.
	if status, _ := get(t, srv.URL+"/api/floor", agentTok); status != 200 {
		t.Fatalf("floor must admit an agent: %d", status)
	}

	// The ?token= query fallback (extract_token): EventSource / <img src>
	// cannot set a header, so the identical verified JWT rides the query.
	if status, _ := get(t, srv.URL+"/api/floor?token="+ownerTok, ""); status != 200 {
		t.Fatalf("?token= query fallback must authorize: %d", status)
	}
	if status, _ := get(t, srv.URL+"/api/floor?token=garbage", ""); status != 401 {
		t.Fatalf("a garbage ?token= must stay 401: %d", status)
	}
	// A PRESENT-but-invalid Authorization header never falls through to the
	// query param (Python parity: header wins when set).
	if status, _ := get(t, srv.URL+"/api/floor?token="+ownerTok, "garbage"); status != 401 {
		t.Fatalf("an invalid header must not fall back to ?token=: %d", status)
	}
	if status, _ := get(t, srv.URL+"/api/floor", ownerTok); status != 200 {
		t.Fatalf("floor must admit the owner: %d", status)
	}

	// The admin/owner chokes: an agent is a flat 403 (envelope), the owner passes.
	for _, path := range []string{"/api/admin", "/api/owner-only"} {
		status, body := get(t, srv.URL+path, agentTok)
		if status != 403 || !strings.Contains(body, `"code":"forbidden"`) {
			t.Fatalf("%s with agent token: want 403 forbidden envelope, got %d %s", path, status, body)
		}
		if status, _ := get(t, srv.URL+path, ownerTok); status != 200 {
			t.Fatalf("%s with owner token: want 200, got %d", path, status)
		}
	}
}

// ── boot assertions: fail-closed app assembly (app.py spirit) ───────────────

func TestBootRefusesUndeclaredRequires(t *testing.T) {
	specs := []RouteSpec{{Method: "GET", Path: "/api/naked", Handler: okHandler, Auth: authGated}}
	if _, err := buildHandler(specs, []byte(interopSecret), nil, nil); err == nil {
		t.Fatal("a gated route with no requires declaration must refuse to boot")
	}
}

func TestBootRefusesUnknownRequires(t *testing.T) {
	specs := []RouteSpec{{Method: "GET", Path: "/api/x", Handler: okHandler, Auth: authGated, Requires: "superuser"}}
	if _, err := buildHandler(specs, []byte(interopSecret), nil, nil); err == nil {
		t.Fatal("an unknown requires class must refuse to boot")
	}
}

func TestBootRefusesAuthRequiresDisagreement(t *testing.T) {
	// public auth ⟺ requires="public" — either direction of disagreement fails.
	bad := [][]RouteSpec{
		{{Method: "GET", Path: "/api/a", Handler: okHandler, Auth: authPublic, Requires: principalOwner}},
		{{Method: "GET", Path: "/api/b", Handler: okHandler, Auth: authGated, Requires: requiresPublic}},
	}
	for i, specs := range bad {
		if _, err := buildHandler(specs, []byte(interopSecret), nil, nil); err == nil {
			t.Fatalf("case %d: auth/requires disagreement must refuse to boot", i)
		}
	}
}

func TestBootRefusesUnlabelledRoute(t *testing.T) {
	specs := []RouteSpec{{Method: "GET", Path: "/api/x", Handler: okHandler, Auth: "internal", Requires: principalOwner}}
	if _, err := buildHandler(specs, []byte(interopSecret), nil, nil); err == nil {
		t.Fatal("an unknown auth label must refuse to boot")
	}
}

// ── the full REST surface (M3 sub-batch A: wired stubs over the spec) ────────

// TestRouteTableCoversSpecSurface pins the table to the frozen wire SSOT: the
// set of (method, path) rows must equal the operations of spec/openapi.json
// exactly — a spec change without a table row (or a stray row) fails here.
func TestRouteTableCoversSpecSurface(t *testing.T) {
	raw, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	want := map[string]bool{}
	for path, item := range spec.Paths {
		for method := range item {
			want[strings.ToUpper(method)+" "+path] = true
		}
	}
	got := map[string]bool{}
	for _, row := range defaultRouteSpecs() {
		key := row.Method + " " + row.Path
		if got[key] {
			t.Fatalf("duplicate route row: %s", key)
		}
		got[key] = true
	}
	for key := range want {
		if !got[key] {
			t.Fatalf("spec operation missing from the route table: %s", key)
		}
	}
	for key := range got {
		if !want[key] {
			t.Fatalf("route table row not in the spec (wire freeze): %s", key)
		}
	}
}

// newWiredTestServer assembles the FULL stack (temp sqlite + migrations +
// seed + hub + repo-file assets via the checkout root) — the sub-batch-B
// integration face. The hub comes back too so a test can attach a listener
// and assert what a handler fans (or refuses to fan).
func newWiredTestServer(t *testing.T) (*httptest.Server, []byte, *Hub) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "server-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	hub := NewHub()
	api := newAPIServer(dal, hub, secret, 3600, "../..")
	phc, err := hashPassword("test-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	api.passwordHash = phc
	h, err := buildHandler(specsFor(api), secret, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h // the MCP tools/call loopback re-enters this mux (cmdServe wiring)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, hub
}

func TestBusinessRoutesServeThroughTheWiredStack(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)

	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")

	// The seeded roster serves through the machine-floor row: Mira is there.
	status, body := get(t, srv.URL+"/api/members", ownerTok)
	if status != 200 || !strings.Contains(body, `"id":"mira"`) {
		t.Fatalf("/api/members: want 200 with the seeded mira, got %d %s", status, body)
	}
	// The seed role folds from the file seed.
	if status, body := get(t, srv.URL+"/api/roles/assistant", ownerTok); status != 200 ||
		!strings.Contains(body, `"is_seed":true`) {
		t.Fatalf("/api/roles/assistant: want the folded seed, got %d %s", status, body)
	}

	// The table's auth/requires wiring still guards everything: 401 with no
	// token, 403 for a plain agent on an admin_agent row (deny BEFORE resolve).
	if status, body := get(t, srv.URL+"/api/members", ""); status != 401 ||
		!strings.Contains(body, `"code":"unauthorized"`) {
		t.Fatalf("no token: want 401 envelope, got %d %s", status, body)
	}
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/members/missing", nil)
	req.Header.Set("Authorization", "Bearer "+agentTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || !strings.Contains(string(respBody), `"code":"forbidden"`) {
		t.Fatalf("agent on admin row: want 403 envelope, got %d %s", resp.StatusCode, respBody)
	}

	// Login: the one public business entry (wrong password → flat 401;
	// missing field → 422 through the envelope).
	login := func(payload string) (int, string) {
		resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}
	if status, _ := login(`{"password":"test-password"}`); status != 200 {
		t.Fatalf("login right password: want 200, got %d", status)
	}
	if status, _ := login(`{"password":"wrong"}`); status != 401 {
		t.Fatalf("login wrong password: want 401, got %d", status)
	}
	if status, body := login(`{}`); status != 422 ||
		!strings.Contains(body, `"code":"validation_error"`) {
		t.Fatalf("login missing field: want 422 envelope, got %d %s", status, body)
	}

	// A query param the wrapper cannot bind stays the validation 422.
	if status, body := get(t, srv.URL+"/api/chat?limit=notanumber", ownerTok); status != 422 ||
		!strings.Contains(body, `"code":"validation_error"`) {
		t.Fatalf("bad query param: want 422 validation envelope, got %d %s", status, body)
	}
}

func TestMarkChatReadFansDeltaOnlyWhenWatermarkAdvances(t *testing.T) {
	srv, secret, hub := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	l, err := hub.Connect("", "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	markRead := func(ts float64) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/chat/mark-read",
			strings.NewReader(fmt.Sprintf(`{"peer":"mira","last_read_ts":%v}`, ts)))
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(body)
	}
	drainChatReadFrames := func() []map[string]any {
		t.Helper()
		var frames []map[string]any
		for {
			raw := l.pop()
			if raw == nil {
				return frames
			}
			_, envelope := parseSSEFrame(t, raw)
			if envelope["topic"] == "chat_read" {
				frames = append(frames, envelope)
			}
		}
	}

	// An advancing report fans EXACTLY one chat_read delta.
	if status, body := markRead(100); status != 200 {
		t.Fatalf("advance: want 200, got %d %s", status, body)
	}
	frames := drainChatReadFrames()
	if len(frames) != 1 {
		t.Fatalf("advance must fan exactly one chat_read frame, got %d: %v", len(frames), frames)
	}
	payload := frames[0]["data"].(map[string]any)["payload"].(map[string]any)
	if payload["reader"] != "owner" || payload["peer"] != "mira" || payload["last_read_ts"] != float64(100) {
		t.Fatalf("frame payload: %v", payload)
	}

	// A stale (lower) and an equal report are no-ops: the effective watermark
	// answers, but NOTHING fans (repository.put_chat_read: no write, no fan).
	for _, ts := range []float64{50, 100} {
		status, body := markRead(ts)
		if status != 200 || !strings.Contains(body, `"last_read_ts":100`) {
			t.Fatalf("stale/equal report must answer the effective watermark, got %d %s", status, body)
		}
		if frames := drainChatReadFrames(); len(frames) != 0 {
			t.Fatalf("stale/equal report must fan nothing, got %v", frames)
		}
	}
}

func TestBareVersionProbeShapeMatchesPython(t *testing.T) {
	h, err := buildHandler(defaultRouteSpecs(), []byte(interopSecret), nil, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/version")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var dto struct {
		Version     string `json:"version"`
		SHA         string `json:"sha"`
		CatalogHash string `json:"catalog_hash"`
	}
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if dto.Version != "0.0.0" || len(dto.SHA) != 7 || len(dto.CatalogHash) != 16 {
		t.Fatalf("probe shape diverges from ProbeVersionDTO: %s", body)
	}
	// Field ORDER is part of the parity contract (version, sha, catalog_hash).
	if !(strings.Index(string(body), `"version":`) < strings.Index(string(body), `"sha":`) &&
		strings.Index(string(body), `"sha":`) < strings.Index(string(body), `"catalog_hash":`)) {
		t.Fatalf("probe field order diverges: %s", body)
	}
}

// ── principal ladder constants ───────────────────────────────────────────────

func TestPrincipalLadderMatchesPython(t *testing.T) {
	// service.authz.PRINCIPAL_RANK: machine(0) < agent(1) < admin_agent(2) < owner(3).
	want := map[string]int{"machine": 0, "agent": 1, "admin_agent": 2, "owner": 3}
	for k, v := range want {
		if principalRank[k] != v {
			t.Fatalf("rank[%s] = %d, want %d", k, principalRank[k], v)
		}
	}
	if len(principalRank) != len(want) {
		t.Fatalf("ladder must be exactly the four classes: %v", principalRank)
	}
	if adminRoleKey != "assistant" || machineKind != "warden" {
		t.Fatalf("classification literals drifted: %q %q", adminRoleKey, machineKind)
	}
}
