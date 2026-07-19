package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMcpToolIndexMatchesFrozenCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	index := mcpToolIndex(defaultRouteSpecs())
	if len(index) != len(catalog.Tools) {
		t.Fatalf("tool index size %d != frozen catalog %d", len(index), len(catalog.Tools))
	}
	for _, tool := range catalog.Tools {
		if _, ok := index[tool.Name]; !ok {
			t.Errorf("frozen catalog tool %q missing from the table-derived index", tool.Name)
		}
	}
}

func TestSplitToolArgumentsPathQueryBody(t *testing.T) {
	getSpec := RouteSpec{Method: "GET", Path: "/api/members/{member_id}"}
	path, query, body := splitToolArguments(getSpec, map[string]any{
		"member_id": "m-1",
		"limit":     json.Number("5"),
		"with":      "peer",
		"unset":     nil,
	})
	if path != "/api/members/m-1" {
		t.Fatalf("path param not substituted: %q", path)
	}
	if body != nil {
		t.Fatalf("GET route must not carry a body: %q", body)
	}
	if !strings.Contains(query, "limit=5") || !strings.Contains(query, "with=peer") {
		t.Fatalf("remaining GET args must become query params: %q", query)
	}
	if strings.Contains(query, "unset") {
		t.Fatalf("nil optionals must be dropped from the query: %q", query)
	}

	// A missing path key substitutes the EMPTY string (spec §3.1 rule 1).
	path, _, _ = splitToolArguments(getSpec, map[string]any{})
	if path != "/api/members/" {
		t.Fatalf("missing path key must substitute empty string: %q", path)
	}

	// A GET list value expands doseq-style: one pair per element.
	_, query, _ = splitToolArguments(getSpec, map[string]any{
		"member_id": "m-1", "tag": []any{"a", "b"},
	})
	if !strings.Contains(query, "tag=a") || !strings.Contains(query, "tag=b") {
		t.Fatalf("list query values must expand per element: %q", query)
	}

	// Non-GET: remaining keys are the JSON body; empty remaining → {} (a body
	// is always sent for a write route).
	postSpec := RouteSpec{Method: "POST", Path: "/api/members/{member_id}/activate"}
	path, query, body = splitToolArguments(postSpec, map[string]any{
		"member_id": "m-1", "name": "n", "count": json.Number("3"),
	})
	if path != "/api/members/m-1/activate" || query != "" {
		t.Fatalf("POST split: path=%q query=%q", path, query)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil || parsed["name"] != "n" || parsed["count"] != float64(3) {
		t.Fatalf("POST body must carry the remaining args as JSON: %s (%v)", body, err)
	}
	_, _, body = splitToolArguments(postSpec, map[string]any{"member_id": "m-1"})
	if string(body) != "{}" {
		t.Fatalf("empty remaining args must send {}: %q", body)
	}
}

func TestCallToolResultMapping(t *testing.T) {
	// 2xx object body: isError false, structuredContent == the parsed object.
	result := callToolResult(200, []byte(`{"id":"m-1","cost":1.50}`))
	if result["isError"] != false {
		t.Fatalf("200 must map to isError:false: %v", result)
	}
	content := result["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != `{"id":"m-1","cost":1.50}` {
		t.Fatalf("content must be ONE text item with the raw body: %v", content)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["id"] != "m-1" || structured["cost"] != json.Number("1.50") {
		t.Fatalf("object body must carry literal-exact structuredContent: %v", result)
	}

	// Top-level array: structuredContent MUST be absent (spec §3.3).
	result = callToolResult(200, []byte(`[{"id":"m-1"}]`))
	if _, present := result["structuredContent"]; present {
		t.Fatalf("array body must omit structuredContent: %v", result)
	}

	// 4xx: a successful result with isError true; empty body → empty text.
	result = callToolResult(404, nil)
	if result["isError"] != true {
		t.Fatalf("404 must map to isError:true: %v", result)
	}
	if result["content"].([]any)[0].(map[string]any)["text"] != "" {
		t.Fatalf("empty body must map to empty text: %v", result)
	}

	// Non-JSON body: text carries it, structuredContent absent.
	result = callToolResult(200, []byte("plain"))
	if _, present := result["structuredContent"]; present {
		t.Fatalf("non-JSON body must omit structuredContent: %v", result)
	}
}

func postMCP(t *testing.T, url, token, body string) map[string]any {
	t.Helper()
	req, err := http.NewRequest("POST", url+"/api/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("MCP envelope must ride HTTP 200: %d %s", resp.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("bad MCP payload: %v %s", err, raw)
	}
	return payload
}

func TestToolsCallLoopbackThroughTheWiredStack(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")

	callResult := func(payload map[string]any) map[string]any {
		t.Helper()
		if err, present := payload["error"]; present {
			t.Fatalf("expected a result envelope, got error: %v", err)
		}
		return payload["result"].(map[string]any)
	}

	// GET tool, top-level array: isError false, seeded roster in text.
	result := callResult(postMCP(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_members","arguments":{}}}`))
	if result["isError"] != false {
		t.Fatalf("get_members: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"mira"`) {
		t.Fatalf("get_members text must carry the seeded roster: %s", text)
	}
	if _, present := result["structuredContent"]; present {
		t.Fatalf("array body must omit structuredContent: %v", result)
	}

	// Path-param split + route 404 → isError true with the REST envelope.
	result = callResult(postMCP(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_member","arguments":{"member_id":"missing"}}}`))
	if result["isError"] != true {
		t.Fatalf("get_member(missing): %v", result)
	}
	if sc := result["structuredContent"].(map[string]any); sc["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("route 404 must surface the REST envelope: %v", result)
	}

	// Authorization forwards verbatim: an agent on an admin-floor tool gets
	// the SAME RBAC 403 as REST, as an isError result — never an RPC error.
	result = callResult(postMCP(t, srv.URL, agentTok,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_role","arguments":{"name":"X"}}}`))
	if result["isError"] != true ||
		result["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "forbidden" {
		t.Fatalf("agent on admin tool must be the RBAC 403 envelope: %v", result)
	}

	// Absent arguments default to {} and a body IS sent for a write route.
	result = callResult(postMCP(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"reset_global_context"}}`))
	if result["isError"] != false {
		t.Fatalf("reset_global_context with no arguments: %v", result)
	}
}

func TestToolsCallWithoutLoopbackIsHonest32603(t *testing.T) {
	// The dependency-free table (no loopback wired) must answer an honest
	// internal error, never a fabricated result.
	h, err := buildHandler(defaultRouteSpecs(), []byte(interopSecret), nil, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	tok, _ := mintJWT("owner", "owner", 300, []byte(interopSecret), time.Now().Unix(), "")
	payload := postMCP(t, srv.URL, tok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_members"}}`)
	errObj, ok := payload["error"].(map[string]any)
	if !ok || errObj["code"] != float64(-32603) {
		t.Fatalf("unwired loopback must be -32603: %v", payload)
	}
}

// TestToolsCallRelocateMemberMovesWorker (P7c, gate rc-2786636f30e5 外包對齊正職):
// the MCP channel for moving a worker is the EXISTING relocate_member tool —
// its member_id argument also accepts a worker id, and the handler falls
// through to the worker relocate core. An admin agent succeeds through the
// full loopback (auth gate + RBAC choke + param binding); a plain agent gets
// the same RBAC 403 envelope as REST. No worker-specific tool exists, so the
// tool surface (and catalog_hash) is unchanged.
func TestToolsCallRelocateMemberMovesWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	// An admin-class caller (role assistant) on the roster.
	admin := fullMember("adm-mcp")
	if err := api.dal.PutMember(admin); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	secret := []byte("tasks-test-secret")
	h, err := buildHandler(specsFor(api), secret, api.dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	now := time.Now().Unix()
	adminTok, _ := mintJWT("adm-mcp", "agent", 300, secret, now, "")
	payload := postMCP(t, srv.URL, adminTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"relocate_member","arguments":{"member_id":"`+workerID+`","machine_id":"auto"}}}`)
	if errObj, present := payload["error"]; present {
		t.Fatalf("expected a result envelope, got error: %v", errObj)
	}
	result := payload["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("relocate_member(worker id) as admin: %v", result)
	}
	sc := result["structuredContent"].(map[string]any)
	if sc["id"] != workerID {
		t.Fatalf("structuredContent must be the worker projection: %v", sc)
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if w.DesiredMachineID != "auto" {
		t.Errorf("worker desired_machine_id = %q, want auto", w.DesiredMachineID)
	}

	// A plain agent (no roster capability) is the RBAC 403 — denied at the
	// route gate before any table is consulted.
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	payload = postMCP(t, srv.URL, agentTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"relocate_member","arguments":{"member_id":"`+workerID+`","machine_id":"auto"}}}`)
	result = payload["result"].(map[string]any)
	if result["isError"] != true ||
		result["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "forbidden" {
		t.Fatalf("plain agent must get the RBAC 403 envelope: %v", result)
	}
}
