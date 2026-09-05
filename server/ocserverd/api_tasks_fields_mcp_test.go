package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ── T-646a:MCP 端到端 ───────────────────────────────────────────────────────
//
// Supersedes the two files this replaces (T-e271's api_tasks_description_mcp_test
// and T-2ebe's api_tasks_title_mcp_test). Their tools are off the MCP catalogue
// now, so those tests could only assert about a surface no agent can reach — and
// every assertion they carried is carried here, against update_task.
//
// Everything else about this route is exercised by calling its handler directly.
// That is NOT enough, and the gap is specific rather than philosophical: a
// handler test constructs the request itself, so it proves nothing about the four
// things standing between an agent and that handler —
//
//	① the tool NAME update_task resolving to this route at all (mcpToolIndex,
//	   derived from the route table). A tool missing from the index is "tool not
//	   found" for every agent while every handler test stays green.
//	② splitToolArguments putting `task_id` in the PATH and `title` / `description`
//	   in the BODY. Get that wrong and the handler is reached with an empty id.
//	③ the auth gate + RBAC choke the loopback re-enters carrying the CALLER's own
//	   token — which is what makes the 403 below a statement about MCP callers
//	   rather than about a hand-stamped claims context.
//	④ the REST→MCP result mapping: a 4xx rides as a successful JSON-RPC result
//	   carrying isError, never an RPC error.
//
// An agent's only route into this feature is this path, so this is the only test
// that says the capability is REACHABLE.

// callUpdateTask drives one real tools/call through the wired mux. Fields are
// omitted from the arguments when nil, which is the distinction the whole tool
// rests on: ABSENT and PRESENT-BUT-EMPTY must not collapse into each other on
// the way through MCP either.
func callUpdateTask(t *testing.T, url, token, taskID string, title, description *string) map[string]any {
	t.Helper()
	arguments := map[string]any{"task_id": taskID}
	if title != nil {
		arguments["title"] = *title
	}
	if description != nil {
		arguments["description"] = *description
	}
	args, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return postMCP(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call",`+
		`"params":{"name":"update_task","arguments":`+string(args)+`}}`)
}

// toolResult unwraps the JSON-RPC envelope, insisting there is no RPC-level
// error (a route 4xx must ride as a RESULT with isError, per spec/mcp.md §3).
func toolResult(t *testing.T, payload map[string]any) (map[string]any, bool, string) {
	t.Helper()
	if rpcErr, present := payload["error"]; present {
		t.Fatalf("a route refusal must NOT become a JSON-RPC error: %v", rpcErr)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in MCP payload: %v", payload)
	}
	isError, _ := result["isError"].(bool)
	text := ""
	if content, ok := result["content"].([]any); ok && len(content) == 1 {
		if entry, ok := content[0].(map[string]any); ok {
			text, _ = entry["text"].(string)
		}
	}
	return result, isError, text
}

// mcpTaskFixture creates one task through the REST surface and hands back its
// id. Its executor is the seeded Mira and its creator is the owner, so the
// creator is NOT the executor — which is what makes the 403 below meaningful.
func mcpTaskFixture(t *testing.T, srv, ownerTok string) (taskID string) {
	t.Helper()
	body := strings.NewReader(`{"title":"mcp fields task","executor_member_id":"mira"}`)
	req, err := http.NewRequest("POST", srv+"/api/tasks", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || out.Task.ID == "" {
		t.Fatalf("create task through REST: %d", resp.StatusCode)
	}
	return out.Task.ID
}

// readTaskTextOverREST reads both fields back through a SECOND, independent call
// so no assertion below rests on the write's own echo.
func readTaskTextOverREST(t *testing.T, srv, token, taskID string) (title, description string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv+"/api/tasks/"+taskID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Title, out.Description
}

// countDocumentRevisions reads how many revisions of one document are retained,
// through the real list_document_history route. It is how the trim test tells a
// no-op apart from a write that stored identical text: both leave the same bytes
// on the task, and only the version list can say whether one of the three
// retained slots was spent.
func countDocumentRevisions(t *testing.T, srv, token, kind, key string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", srv+"/api/document-history/"+kind+"/"+key, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list_document_history %s/%s: %d", kind, key, resp.StatusCode)
	}
	var versions []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		t.Fatal(err)
	}
	return len(versions)
}

// TestToolsCallUpdateTaskAcceptsTheExecutor is the acceptance half, and it uses
// the case the two predecessor tools could not express: BOTH fields corrected in
// one call.
func TestToolsCallUpdateTaskAcceptsTheExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	_, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("透過 MCP 更正的標題"), strptr("透過 MCP 更正的敘述")))
	if isError {
		t.Fatalf("the executor's own tool call must be accepted: %s", text)
	}
	// The receipt is the task itself — and it must carry BOTH new values, so this
	// cannot pass on a call that was merely routed and did nothing.
	if !strings.Contains(text, "透過 MCP 更正的標題") || !strings.Contains(text, "透過 MCP 更正的敘述") {
		t.Fatalf("tool result must echo both stored values: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != "透過 MCP 更正的標題" {
		t.Fatalf("title read back = %q", title)
	}
	if description != "透過 MCP 更正的敘述" {
		t.Fatalf("description read back = %q", description)
	}
}

// TestToolsCallUpdateTaskLeavesUnnamedFieldsAlone is the partial-update half: a
// call naming only one field must not disturb the other. Without it, a handler
// that wrote the zero value for whatever the body omitted would pass every test
// above.
func TestToolsCallUpdateTaskLeavesUnnamedFieldsAlone(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		nil, strptr("只給敘述"))); isError {
		t.Fatalf("description-only call must be accepted: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != "mcp fields task" {
		t.Fatalf("a description-only call disturbed the title: %q", title)
	}
	if description != "只給敘述" {
		t.Fatalf("description = %q", description)
	}

	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("只給標題"), nil)); isError {
		t.Fatalf("title-only call must be accepted: %s", text)
	}
	title, description = readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != "只給標題" {
		t.Fatalf("title = %q", title)
	}
	if description != "只給敘述" {
		t.Fatalf("a title-only call disturbed the description: %q", description)
	}
}

// TestToolsCallUpdateTaskRefusesANonExecutor is the refusal half, and it asserts
// the REASON: the unified 403 envelope reaches the agent intact through the
// loopback rather than collapsing into a bare failure — and nothing was written,
// which a status-only assertion would not notice.
func TestToolsCallUpdateTaskRefusesANonExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	strangerTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)
	beforeTitle, beforeDescription := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if beforeTitle == "" {
		t.Fatal("fixture: the created task must already have a title")
	}

	result, isError, text := toolResult(t, callUpdateTask(t, srv.URL, strangerTok, taskID,
		strptr("不該寫進去的標題"), strptr("不該寫進去的敘述")))
	if !isError {
		t.Fatalf("a non-executor's tool call must be refused: %s", text)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("a JSON refusal must carry structuredContent: %v", result)
	}
	envelope, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("refusal must carry the unified error envelope: %v", structured)
	}
	if envelope["code"] != "forbidden" {
		t.Fatalf("error code = %v, want forbidden", envelope["code"])
	}
	if msg, _ := envelope["message"].(string); !strings.Contains(msg, "not the task's executor") {
		t.Fatalf("error message = %q, want the executor-guard reason", msg)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != beforeTitle || description != beforeDescription {
		t.Fatalf("refused tool call still wrote: title=%q description=%q", title, description)
	}
}

// TestToolsCallUpdateTaskRefusesABlankTitle carries the owner's asymmetry ruling
// (rc-796541192519 ①) onto the surface an agent actually holds: the blank
// refusal must survive the loopback as a result carrying isError, and the stored
// title must be untouched. Its description sibling CLEARS on a blank, so an
// implementation that treated the two fields alike would answer 200 here and
// empty the task-list row.
func TestToolsCallUpdateTaskRefusesABlankTitle(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)
	before, _ := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)

	for _, blank := range []string{"", "   ", "\t\n", "　"} {
		_, isError, text := toolResult(t,
			callUpdateTask(t, srv.URL, miraTok, taskID, strptr(blank), nil))
		if !isError {
			t.Fatalf("a blank title %q must be refused through MCP too: %s", blank, text)
		}
		if !strings.Contains(text, "title") {
			t.Fatalf("the refusal must name the field: %s", text)
		}
		if got, _ := readTaskTextOverREST(t, srv.URL, ownerTok, taskID); got != before {
			t.Fatalf("a refused blank still wrote: title = %q", got)
		}
	}
}

// TestToolsCallUpdateTaskRejectsTheWholeBodyOnABlankTitle is the case neither
// predecessor could have: one call carrying an ILLEGAL title beside a PERFECTLY
// GOOD description. The 400 must leave BOTH fields untouched.
//
// This is the assertion that fails if validation is ever moved back inside the
// write loop — a per-field implementation stores the description, then refuses,
// and the caller is told nothing happened while half of it did.
func TestToolsCallUpdateTaskRejectsTheWholeBodyOnABlankTitle(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)
	beforeTitle, beforeDescription := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)

	_, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("   "), strptr("這一段完全合法，但它不該被寫進去")))
	if !isError {
		t.Fatalf("a blank title must refuse the WHOLE body: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != beforeTitle {
		t.Fatalf("title moved on a refused body: %q", title)
	}
	if description != beforeDescription {
		t.Fatalf("🔴 half-applied: the description was written behind a 400: %q", description)
	}
}

// TestToolsCallUpdateTaskTrimsBothFields is the owner's other ruling
// (rc-0fb94a25a8a8 ①). Two claims, and they are different claims: the STORED
// value is trimmed, and the unchanged-value COMPARISON is made on the trimmed
// value — the second is what stops a stray trailing space from reading as an
// edit and spending one of the three retained revisions saying nothing moved.
//
// The comparison half is asserted through the document history, because that is
// the only place the difference is visible: a trimming implementation that
// compared the RAW value would store exactly the same text and pass every
// read-back assertion while quietly retaining a revision.
func TestToolsCallUpdateTaskTrimsBothFields(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("  有空白的標題\t"), strptr("\n有空白的敘述  "))); isError {
		t.Fatalf("first write must be accepted: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != "有空白的標題" {
		t.Fatalf("stored title was not trimmed: %q", title)
	}
	if description != "有空白的敘述" {
		t.Fatalf("stored description was not trimmed: %q", description)
	}

	beforeTitleRevs := countDocumentRevisions(t, srv.URL, ownerTok, "task_title", taskID)
	beforeDescRevs := countDocumentRevisions(t, srv.URL, ownerTok, "task_description", taskID)

	// Re-send the SAME text differing only by surrounding whitespace. Nothing
	// changed, so nothing may be versioned.
	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("有空白的標題   "), strptr("   有空白的敘述"))); isError {
		t.Fatalf("a whitespace-only difference must not be an error: %s", text)
	}
	if got := countDocumentRevisions(t, srv.URL, ownerTok, "task_title", taskID); got != beforeTitleRevs {
		t.Fatalf("a whitespace-only title resend burned a revision: %d → %d", beforeTitleRevs, got)
	}
	if got := countDocumentRevisions(t, srv.URL, ownerTok, "task_description", taskID); got != beforeDescRevs {
		t.Fatalf("a whitespace-only description resend burned a revision: %d → %d", beforeDescRevs, got)
	}
}

// TestToolsCallUpdateTaskVersionsOnlyTheFieldThatChanged pins the one claim in
// this route that nothing else can see: a history stream is enrolled ONLY for a
// field that is actually changing.
//
// Enrolling both unconditionally reads back identically on the task and passes
// every other test in this file, while quietly retaining a revision for the
// untouched field — and the retained set is only three deep, so a run of
// title-only corrections would push the description's oldest recoverable wording
// off the end. The damage is invisible until someone needs the wording back.
func TestToolsCallUpdateTaskVersionsOnlyTheFieldThatChanged(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	// Give the description a value so it HAS something a stray revision could
	// retain — on an empty description the snapshot is "{}" and nothing is kept,
	// which would make this test pass for the wrong reason.
	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		nil, strptr("原本的敘述"))); isError {
		t.Fatalf("seed write must be accepted: %s", text)
	}
	beforeTitleRevs := countDocumentRevisions(t, srv.URL, ownerTok, "task_title", taskID)
	beforeDescRevs := countDocumentRevisions(t, srv.URL, ownerTok, "task_description", taskID)

	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("只改標題"), nil)); isError {
		t.Fatalf("title-only call must be accepted: %s", text)
	}

	if got := countDocumentRevisions(t, srv.URL, ownerTok, "task_title", taskID); got != beforeTitleRevs+1 {
		t.Fatalf("the CHANGED field must retain exactly one revision: %d → %d", beforeTitleRevs, got)
	}
	if got := countDocumentRevisions(t, srv.URL, ownerTok, "task_description", taskID); got != beforeDescRevs {
		t.Fatalf("an untouched description was versioned anyway: %d → %d", beforeDescRevs, got)
	}
}

// TestToolsCallUpdateTaskClearsADescriptionOfOnlyWhitespace pins the consequence
// of trimming the description that a reader would otherwise have to derive:
// whitespace trims to "", and "" on this field is a real write that CLEARS. It
// is named here so the behaviour is a decision on the record rather than a
// surprise found in production.
func TestToolsCallUpdateTaskClearsADescriptionOfOnlyWhitespace(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		nil, strptr("先有內容"))); isError {
		t.Fatalf("seed write must be accepted: %s", text)
	}
	if _, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		nil, strptr("   \t "))); isError {
		t.Fatalf("a whitespace-only description must be accepted, not refused: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if description != "" {
		t.Fatalf("a whitespace-only description must CLEAR, got %q", description)
	}
	if title == "" {
		t.Fatal("clearing the description must not touch the title")
	}
}

// TestToolsCallUpdateTaskOnAClosedTask carries T-e271's ruling ② onto the
// agent-facing surface: the tool an agent actually holds must work after the
// task closes, not only the handler underneath it.
func TestToolsCallUpdateTaskOnAClosedTask(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	// Close it through the real terminate route (owner-gated).
	req, _ := http.NewRequest("POST", srv.URL+"/api/tasks/"+taskID+"/terminate", nil)
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("terminate: %d", resp.StatusCode)
	}

	_, isError, text := toolResult(t, callUpdateTask(t, srv.URL, miraTok, taskID,
		strptr("結案後更正的標題"), strptr("結案後透過 MCP 更正")))
	if isError {
		t.Fatalf("a closed task must still accept the text tool: %s", text)
	}
	title, description := readTaskTextOverREST(t, srv.URL, ownerTok, taskID)
	if title != "結案後更正的標題" || description != "結案後透過 MCP 更正" {
		t.Fatalf("closed-task text read back = %q / %q", title, description)
	}

	// The control, on the SAME closed task through the SAME channel: the artifact
	// tool is refused. Without it, "the tool worked" could equally mean the
	// terminal guard is missing everywhere.
	payload := postMCP(t, srv.URL, miraTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_task_artifact",`+
			`"arguments":{"task_id":"`+taskID+`","kind":"link","name":"pr","url":"https://example.invalid/1"}}}`)
	if _, artErr, artText := toolResult(t, payload); !artErr {
		t.Fatalf("a closed task's artifact set must stay frozen: %s", artText)
	}
}

// TestToolsListNoLongerCarriesTheFoldedTools is the other half of "one tool, not
// three": the two predecessors must be GONE from the catalogue an agent reads,
// while update_task is present. A merge that added the new tool and left the old
// ones on the surface would pass every behavioural test in this file and still
// fail the ticket.
//
// It asserts against BOTH sources rather than one, because they can disagree and
// each alone is satisfiable while an agent still sees the wrong thing: the
// route-derived index is what a tools/call resolves through, and the frozen
// catalogue is the bytes tools/list serves. (A wired test server does not stage
// the embedded catalogue, so driving a real tools/list here would fail on a
// missing file rather than on the claim.)
func TestToolsListNoLongerCarriesTheFoldedTools(t *testing.T) {
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
	served := map[string]bool{}
	for _, tool := range catalog.Tools {
		served[tool.Name] = true
	}
	index := mcpToolIndex(defaultRouteSpecs())

	if !served["update_task"] {
		t.Error("update_task is missing from the frozen catalog — the capability does not exist for agents")
	}
	if _, ok := index["update_task"]; !ok {
		t.Error("update_task is missing from the route-derived tool index — a tools/call would not resolve")
	}
	for _, folded := range []string{"update_task_title", "update_task_description"} {
		if served[folded] {
			t.Errorf("%s is still in the frozen catalog; the fold left three tools where one was wanted", folded)
		}
		if _, ok := index[folded]; ok {
			t.Errorf("%s is still on the route-derived tool index; MCPExclude was not applied", folded)
		}
	}
}
