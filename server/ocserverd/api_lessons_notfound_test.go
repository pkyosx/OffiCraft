package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-d483 regression: the lessons API answered not_found for an existing role,
// breaking the learning loop. Root cause — an MCP get_lessons / replace_lessons
// that blanks role_key substitutes an empty path
// segment, missing /api/lessons/{role_key}; the SPA fallback then
// answers not_found. The fix folds the boot context's own key derivation into the
// tool boundary: blank role_key → the caller's role. T-2 removed task_type
// entirely; a call that still sends it is refused by name (see
// TestLessonsMCPRefusesTheRetiredTaskTypeArgument).
//
// These drive the SAME wired stack (MCP loopback + REST) an agent uses.

// newLessonsTestServer mirrors newWiredTestServer but hands back the DAL so a
// test can seed a custom-role agent member directly.
// newLessonsTestServer is the four-value shape's wrapper, kept because most
// callers do not need the apiServer itself. T-33 added
// newLessonsTestServerAPI for the ones that must reach a settings field (the
// lore feature switch) — one assembly, two faces, so the two can never build
// subtly different stacks.
func newLessonsTestServer(t *testing.T) (*httptest.Server, *DAL, []byte) {
	t.Helper()
	srv, dal, secret, _ := newLessonsTestServerAPI(t)
	return srv, dal, secret
}

func newLessonsTestServerAPI(t *testing.T) (*httptest.Server, *DAL, []byte, *apiServer) {
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
	api := newAPIServer(dal, NewHub(), singleKeyring(secret), 3600, "../..")
	phc, err := hashPassword("test-password")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	api.passwordHash = phc
	h, err := buildHandler(specsFor(api), api.keys, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, dal, secret, api
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
	// T-2 follow-up: this fixture used to build the member WITHOUT ever creating
	// the role, which is a shape that cannot boot on a real station at all —
	// buildBootContext folds the ROLE first and returns nil, and both token
	// paths abort on that nil. The incident this test records happened to a role
	// that REALLY EXISTS (r-25debddcf5dd is "OffiCraft Developer"), so creating
	// it here makes the fixture match the incident instead of an impossible
	// station. No assertion below moved.
	if err := dal.PutRoleDef(RoleDef{
		RoleKey: customRole, Name: "OffiCraft Developer", DefinitionMD: "dev\n",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	if err := dal.PutMember(Member{
		ID: "joey", Kind: KindStaff, RoleKey: customRole,
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	joeyTok, _ := mintJWT("joey", "agent", 300, secret, now, "")

	// 1. Baseline happy path: the one remaining segment, explicit → serves.
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"assistant"}}}`); isErr {
		t.Fatalf("explicit assistant must serve, got code=%q", code)
	}

	// 2. Whitespace-only role_key uses the same blank predicate as the shared
	//    MCP path guard, so the identity default still applies before dispatch.
	if isErr, code, _ := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":32,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"   "}}}`); isErr {
		t.Fatalf("agent get_lessons with whitespace role_key must serve, got code=%q", code)
	}

	// 3. NO arguments as an AGENT ("無參數,identity 從 token") → role from the
	//    roster → serves the custom role's doc, never not_found.
	if isErr, code, _ := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`); isErr {
		t.Fatalf("agent get_lessons with no arguments must serve, got code=%q", code)
	}

	// 4. replace_lessons UPSERTS to the role; a subsequent read returns the
	//    same text — one source of truth.
	marker := "T-d483 upsert marker"
	if isErr, code, _ := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"role_key":"`+customRole+`","text":"`+marker+`"}}}`); isErr {
		t.Fatalf("replace_lessons must upsert, got code=%q", code)
	}
	isErr, code, text := lessonsCall(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_lessons","arguments":{"role_key":"`+customRole+`"}}}`)
	if isErr {
		t.Fatalf("readback after upsert must serve, got code=%q", code)
	}
	if !strings.Contains(text, marker) {
		t.Fatalf("readback must carry the upserted text; got: %s", text)
	}

	// 5. The agent reads back its OWN just-written lessons with NO arguments —
	//    the round-trip the learning loop depends on.
	if _, _, agentText := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`); !strings.Contains(agentText, marker) {
		t.Fatalf("agent no-arg readback must see its own lessons; got: %s", agentText)
	}
}

// TestLessonsMCPRefusesTheRetiredTaskTypeArgument is the BACKWARD-COMPATIBILITY
// decision of T-2 step B, written down as an executable rule rather than a
// paragraph.
//
// 🔴 THE THREE CHOICES, AND WHY THIS ONE. A caller that still sends `task_type`
// could be (a) refused, (b) served with the field silently dropped, or (c)
// served with the field honoured. (c) is impossible — there is nowhere left to
// put it. (b) is the WORST of the three and not by a small margin: this
// endpoint's original defect was that an unvalidated task_type sent a write to
// a bucket the caller did not mean, answered 200, and said nothing. Dropping
// the field silently reproduces that experience exactly — the caller believes
// it addressed a classification, the write lands elsewhere — while deleting the
// last evidence that anything happened. So: refuse, by name, with the
// replacement stated.
//
// This is the named assertion a mutant that turns the refusal back into a
// silent drop has to turn red.
func TestLessonsMCPRefusesTheRetiredTaskTypeArgument(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	const role = "assistant"
	const marker = "the doc nobody may overwrite by accident"
	if err := dal.PutLessons(Lessons{RoleKey: role, Text: marker}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}

	for _, tc := range []struct{ name, args string }{
		{"get_lessons", `{"name":"get_lessons","arguments":{"role_key":"assistant","task_type":"general"}}`},
		{"get_lessons_empty_value", `{"name":"get_lessons","arguments":{"role_key":"assistant","task_type":""}}`},
		{"replace_lessons", `{"name":"replace_lessons","arguments":{"role_key":"assistant","task_type":"review-pr-seth","text":"poison"}}`},
		{"patch_lessons", `{"name":"patch_lessons","arguments":{"role_key":"assistant","task_type":"review-pr-seth","edits":[{"old":"","new":"poison"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isErr, code, text := lessonsCall(t, srv.URL, ownerTok,
				`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+tc.args+`}`)
			if !isErr {
				t.Fatalf("a call carrying the RETIRED task_type argument was ACCEPTED "+
					"(code=%q, body=%s). Silently ignoring it is the one behaviour this "+
					"removal must not have: the caller is left believing it named a "+
					"classification while the write went somewhere else — which is the "+
					"exact failure T-2 removed the axis to end", code, text)
			}
			if code != errorCodeForStatus(http.StatusBadRequest) {
				t.Errorf("refusal code = %q, want the 400 code — a retired ARGUMENT is the "+
					"caller's input being wrong, not a transport fault", code)
			}
			if !strings.Contains(text, "task_type") {
				t.Errorf("the refusal does not name the field it refused: %s. A caller that "+
					"cannot see WHICH argument was rejected has to guess, and guessing is "+
					"how the field gets sent again", text)
			}
			if !strings.Contains(text, "role_key") {
				t.Errorf("the refusal does not name the replacement (role_key): %s", text)
			}
		})
	}

	// The refusal must be a refusal, not a partially-applied write: the doc is
	// untouched after all four attempts.
	overlay, err := dal.GetLessons(role)
	if err != nil {
		t.Fatalf("GetLessons: %v", err)
	}
	if overlay == nil || overlay.Text != marker {
		t.Errorf("the lessons doc changed under a refused call: %+v — a refusal that still "+
			"writes is worse than an acceptance, because nothing reports it", overlay)
	}
}
