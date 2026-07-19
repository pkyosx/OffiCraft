package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-d483 regression: the lessons API answered not_found for an existing role,
// breaking the learning loop. Root cause — an MCP get_lessons / replace_lessons
// that omits (or blanks) task_type and/or role_key substitutes an empty path
// segment, missing /api/lessons/{role_key}/{task_type}; the SPA fallback then
// answers not_found. The fix folds the boot context's own key derivation into the
// tool boundary: blank task_type → "general", blank role_key → the caller's role.
//
// These drive the SAME wired stack (MCP loopback + REST) an agent uses.

// newLessonsTestServer mirrors newWiredTestServer but hands back the DAL so a
// test can seed a custom-role agent member directly.
func newLessonsTestServer(t *testing.T) (*httptest.Server, *DAL, []byte) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "lessons-test.db"))
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
	api := newAPIServer(dal, NewHub(), secret, 3600, "../..")
	phc, err := hashPassword("test-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	api.passwordHash = phc
	h, err := buildHandler(specsFor(api), secret, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, dal, secret
}

// lessonsCall posts a tools/call and returns (isError, code, text).
func lessonsCall(t *testing.T, url, token, body string) (bool, string, string) {
	t.Helper()
	payload := postMCP(t, url, token, body)
	if e, present := payload["error"]; present {
		t.Fatalf("expected result envelope, got rpc error: %v", e)
	}
	result := payload["result"].(map[string]any)
	isErr, _ := result["isError"].(bool)
	code := ""
	if sc, ok := result["structuredContent"].(map[string]any); ok {
		if e, ok := sc["error"].(map[string]any); ok {
			code, _ = e["code"].(string)
		}
	}
	text := ""
	if content, ok := result["content"].([]any); ok && len(content) > 0 {
		text, _ = content[0].(map[string]any)["text"].(string)
	}
	return isErr, code, text
}

func TestLessonsMCPDefaultsCloseTheLearningLoop(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	// A custom-role agent (the reported shape: role r-25debddcf5dd).
	const customRole = "r-25debddcf5dd"
	if err := dal.PutMember(Member{
		ID: "joey", Kind: KindAssistant, RoleKey: customRole,
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	joeyTok, _ := mintJWT("joey", "agent", 300, secret, now, "")

	// 1. Baseline happy path is unchanged: both segments explicit → serves.
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"assistant","task_type":"general"}}}`); isErr {
		t.Fatalf("explicit assistant/general must serve, got code=%q", code)
	}

	// 2. task_type OMITTED → defaults to "general", no longer not_found.
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"assistant"}}}`); isErr {
		t.Fatalf("get_lessons with omitted task_type must serve, got code=%q", code)
	}

	// 3. task_type EMPTY string → same default.
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"assistant","task_type":""}}}`); isErr {
		t.Fatalf("get_lessons with empty task_type must serve, got code=%q", code)
	}

	// 4. NO arguments as an AGENT ("無參數,identity 從 token") → role from the
	//    roster + general → serves the custom role's doc, never not_found.
	if isErr, code, _ := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`); isErr {
		t.Fatalf("agent get_lessons with no arguments must serve, got code=%q", code)
	}

	// 5. replace_lessons omitting task_type UPSERTS to (role, general); a
	//    subsequent explicit read returns the same text — one source of truth.
	marker := "T-d483 upsert marker"
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"role_key":"`+customRole+`","text":"`+marker+`"}}}`); isErr {
		t.Fatalf("replace_lessons with omitted task_type must upsert, got code=%q", code)
	}
	isErr, code, text := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"`+customRole+`","task_type":"general"}}}`)
	if isErr {
		t.Fatalf("readback after upsert must serve, got code=%q", code)
	}
	if !strings.Contains(text, marker) {
		t.Fatalf("readback must carry the upserted text; got: %s", text)
	}

	// 6. The agent reads back its OWN just-written lessons with NO arguments —
	//    the round-trip the learning loop depends on.
	if _, _, agentText := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`); !strings.Contains(agentText, marker) {
		t.Fatalf("agent no-arg readback must see its own lessons; got: %s", agentText)
	}
}
