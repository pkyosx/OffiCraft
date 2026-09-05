package main

// routes_t63bf_scheduled_message_mcp_test.go — T-63bf: the four 定期訊息 CRUD
// rows gained MCP tools, and the admin floor they sit on did NOT move.
//
// WHY THIS EXISTS. A scheduled message is the mechanism for waking an AI member
// on a wall-clock slot. All four rows shipped `MCPExclude: true` on the webhook
// precedent ("configuration CRUD belongs in the cockpit, not the tool
// catalogue"), which meant the only way to arm the alarm was a surface no AI
// could reach — an alarm clock for agents that only a human can set. The owner
// ruled otherwise on 2026-08-19: 「助理應該能夠代替我設定這些東西 在我的授權下
// 進行」.
//
// The ruling opened an ENTRANCE, not a gate, so this file has to pin a PAIR and
// both halves need teeth of their own:
//
//	(1) the four tools EXIST — on the route table, in the table-derived tool
//	    index, in the frozen catalog, and callable through the live loopback.
//	    "The tool is absent" is otherwise nobody's failure: every pre-existing
//	    catalog guard only fires the other way (a table tool the catalog forgot).
//	(2) the floor did NOT move. Requires stays principalAdminAgent on all four,
//	    and an ordinary agent is a flat 403 on every one of them — through REST
//	    and through the MCP loopback alike. That refusal is the correct answer,
//	    not a gap, so it is asserted on the STATUS CODE rather than on the shape
//	    of what came back.
//
// MUTANTS (run by hand, each compiling):
//   - drop `Requires` on the PATCH row to principalAgent ⇒ arm (2)'s floor test
//     and the plain-agent PATCH refusal go red.
//   - put `MCPExclude: true` back on the GET row ⇒ arm (1) goes red on the table
//     assertion, the tool index and the live loopback.
//   - change one tool name in routes.go without regenerating the catalog ⇒ the
//     catalog membership assertion here goes red. `make drift-mcp-catalog` does
//     NOT: it regenerates the catalog from spec/openapi.json and never reads
//     routes.go, so it stays green through this mutant. The Go assertion is the
//     only thing standing between routes.go and a stale catalog.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// t63bfScheduledTools is the ruling's four rows written out BY HAND, method +
// path → the tool name each must expose. Deriving this from the table under
// test would make it unable to disagree with the table.
var t63bfScheduledTools = map[[2]string]string{
	{"GET", "/api/members/{member_id}/scheduled-messages"}:                  "list_scheduled_messages",
	{"POST", "/api/members/{member_id}/scheduled-messages"}:                 "create_scheduled_message",
	{"PATCH", "/api/members/{member_id}/scheduled-messages/{schedule_id}"}:  "update_scheduled_message",
	{"DELETE", "/api/members/{member_id}/scheduled-messages/{schedule_id}"}: "delete_scheduled_message",
}

func t63bfRouteIndex(t *testing.T) map[[2]string]RouteSpec {
	t.Helper()
	specs := defaultRouteSpecs()
	if len(specs) == 0 {
		t.Fatalf("empty route table — every assertion below would be vacuous")
	}
	index := make(map[[2]string]RouteSpec, len(specs))
	for _, s := range specs {
		index[[2]string{s.Method, s.Path}] = s
	}
	return index
}

// ── arm (1) — the tools exist ───────────────────────────────────────────────

func TestT63bfScheduledMessageRowsCarryTheirMCPTools(t *testing.T) {
	if len(t63bfScheduledTools) != 4 {
		t.Fatalf("the ruling covers 4 rows, this table lists %d", len(t63bfScheduledTools))
	}
	index := t63bfRouteIndex(t)
	callable := mcpToolIndex(defaultRouteSpecs())
	if len(callable) == 0 {
		t.Fatalf("no MCP tools at all — the assertions below would be vacuous")
	}
	for key, wantTool := range t63bfScheduledTools {
		spec, ok := index[key]
		if !ok {
			t.Errorf("%s %s is not in the route table at all", key[0], key[1])
			continue
		}
		if spec.MCPExclude {
			t.Errorf("%s %s is MCPExclude again — an excluded row is not in "+
				"tools/list, so the assistant this ruling was for cannot even "+
				"learn the tool's name (owner 2026-08-19)", key[0], key[1])
		}
		if got := spec.toolName(); got != wantTool {
			t.Errorf("%s %s exposes tool %q, want %q", key[0], key[1], got, wantTool)
		}
		if _, resolves := callable[wantTool]; !resolves {
			t.Errorf("tool %q does not resolve through mcpToolIndex — tools/call "+
				"would answer unknown tool", wantTool)
		}
	}
}

func TestT63bfScheduledMessageToolsAreInTheFrozenCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatalf("frozen catalog is empty — the loop below would prove nothing")
	}
	listed := make(map[string]string, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		listed[tool.Name] = tool.Description
	}
	for key, tool := range t63bfScheduledTools {
		description, present := listed[tool]
		if !present {
			t.Errorf("%s %s is a live tool named %q but the frozen catalog does "+
				"not carry it — tools/list serves the catalog verbatim, so the "+
				"tool does not exist for any caller", key[0], key[1], tool)
			continue
		}
		// The ruling is about the ADMIN assistant. A description that does not
		// say who may call it sends every ordinary agent into a 403 it could
		// have read its way out of.
		if !strings.Contains(description, "admin_agent") {
			t.Errorf("tool %q's catalog description never names the admin_agent "+
				"floor, so a caller cannot tell from tools/list who may use it: %q",
				tool, description)
		}
	}
}

// ── arm (2) — the floor did not move ────────────────────────────────────────

func TestT63bfScheduledMessageRowsStayAtTheAdminAgentFloor(t *testing.T) {
	index := t63bfRouteIndex(t)
	for key := range t63bfScheduledTools {
		spec, ok := index[key]
		if !ok {
			t.Errorf("%s %s is not in the route table at all", key[0], key[1])
			continue
		}
		if spec.Requires != principalAdminAgent {
			t.Errorf("%s %s declares Requires=%q; T-63bf opened the MCP ENTRANCE "+
				"and left the gate exactly where it was, at %q. Anything lower "+
				"hands the office's alarm clock to every agent; %q would re-lock "+
				"out the admin 助理 the ruling was for",
				key[0], key[1], spec.Requires, principalAdminAgent, principalOwner)
		}
	}
}

// t63bfFixture stands up the wired stack with the seeded Mira (role_key
// "assistant" ⇒ admin_agent) and a plain agent whose sub is not on the roster
// (deny-by-default ⇒ principalAgent), plus the premise control that the two
// really do classify differently.
func t63bfFixture(t *testing.T) (string, string, string) {
	t.Helper()
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	adminTok, err := mintJWT("mira", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	plainTok, err := mintJWT("kyle-t63bf", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint plain-agent token: %v", err)
	}
	if got := classifyMember(&Member{ID: "mira", RoleKey: adminRoleKey}); got != principalAdminAgent {
		t.Fatalf("fixture premise: seeded mira must classify as %q, got %q", principalAdminAgent, got)
	}
	if got := classifyMember(nil); got != principalAgent {
		t.Fatalf("fixture premise: an unknown sub must classify as %q, got %q", principalAgent, got)
	}
	return srv.URL, adminTok, plainTok
}

// t63bfCall drives one tools/call over the real loopback and returns the
// CallToolResult, failing on a JSON-RPC error (an unknown tool name arrives as
// -32602, which is exactly the failure a re-added MCPExclude would produce).
func t63bfCall(t *testing.T, url, token, tool, arguments string) map[string]any {
	t.Helper()
	payload := postMCP(t, url, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tool+
			`","arguments":`+arguments+`}}`)
	if err, present := payload["error"]; present {
		t.Fatalf("tools/call %s: JSON-RPC error %v — an unknown tool name is what "+
			"an MCPExclude'd row answers with", tool, err)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call %s returned no result object: %v", tool, payload)
	}
	return result
}

// ── arm (3) — the admin assistant really drives all four, over MCP ──────────

func TestT63bfAdminAssistantDrivesAllFourScheduledMessageToolsOverMCP(t *testing.T) {
	url, adminTok, _ := t63bfFixture(t)

	created := t63bfCall(t, url, adminTok, "create_scheduled_message",
		`{"member_id":"mira","label":"stand-up","body":"晨會時間",`+
			`"cadence":"daily","hour":9,"minute":30,"timezone":"Asia/Taipei"}`)
	if created["isError"] != false {
		t.Fatalf("admin_agent create over MCP: %v", created)
	}
	structured, ok := created["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("a JSON object body must carry structuredContent: %v", created)
	}
	scheduleID, _ := structured["id"].(string)
	if scheduleID == "" {
		t.Fatalf("create returned no schedule id: %v", structured)
	}
	// T-91: the create receipt keeps what the SERVER decided and drops what the
	// caller sent — so timezone/hour/minute are no longer on it (they are stored
	// exactly as posted), while `cadence` stays because on a PATCH it is the
	// ASSEMBLED value and it decides which of the other fields mean anything.
	// The "stored as sent" claim moves to the list read below, which is the only
	// read this API has for a schedule.
	if structured["cadence"] != "daily" {
		t.Fatalf("create did not report the assembled cadence: %v", structured)
	}
	for _, echoed := range []string{"timezone", "hour", "minute", "body"} {
		if _, present := structured[echoed]; present {
			t.Fatalf("the create receipt must not echo %q back — the caller sent it: %v",
				echoed, structured)
		}
	}

	listed := t63bfCall(t, url, adminTok, "list_scheduled_messages", `{"member_id":"mira"}`)
	if listed["isError"] != false {
		t.Fatalf("admin_agent list over MCP: %v", listed)
	}
	// A top-level array carries no structuredContent (spec/mcp.md §3.3), so the
	// list is read out of the text item.
	// This read is now also where "stored as sent" is asserted (see the create
	// note above): the row has to come back carrying the timezone and the clock
	// the create posted, not merely the id.
	if body := t63bfResultText(t, listed); !strings.Contains(body, scheduleID) ||
		!strings.Contains(body, "stand-up") || !strings.Contains(body, "Asia/Taipei") ||
		!strings.Contains(body, `"hour":9`) || !strings.Contains(body, `"minute":30`) {
		t.Fatalf("list must serve the schedule just created, as sent, got %s", body)
	}

	patched := t63bfCall(t, url, adminTok, "update_scheduled_message",
		`{"member_id":"mira","schedule_id":"`+scheduleID+`","status":"disabled","hour":8}`)
	if patched["isError"] != false {
		t.Fatalf("admin_agent update over MCP: %v", patched)
	}
	patchedRow, ok := patched["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("update returned no structuredContent: %v", patched)
	}
	// `status` stays on the receipt because on the UPDATE path it is a real
	// answer — it says whether the row the caller just edited is actually live.
	// `hour` does not: the caller sent it. So the hour change is verified through
	// the list read, the only read this API has for a schedule.
	if patchedRow["status"] != "disabled" {
		t.Fatalf("update did not apply the status: %v", patchedRow)
	}
	if body := t63bfResultText(t, t63bfCall(t, url, adminTok,
		"list_scheduled_messages", `{"member_id":"mira"}`)); !strings.Contains(body, `"hour":8`) {
		t.Fatalf("update did not apply the hour: %s", body)
	}

	deleted := t63bfCall(t, url, adminTok, "delete_scheduled_message",
		`{"member_id":"mira","schedule_id":"`+scheduleID+`"}`)
	if deleted["isError"] != false {
		t.Fatalf("admin_agent delete over MCP: %v", deleted)
	}
	after := t63bfCall(t, url, adminTok, "list_scheduled_messages", `{"member_id":"mira"}`)
	if body := t63bfResultText(t, after); strings.Contains(body, scheduleID) {
		t.Fatalf("delete must really remove the schedule, list still carries it: %s", body)
	}
}

func t63bfResultText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("a CallToolResult carries exactly one text item: %v", result)
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content item is not an object: %v", content[0])
	}
	text, ok := item["text"].(string)
	if !ok {
		t.Fatalf("content item carries no text: %v", item)
	}
	return text
}

// ── arm (4) — an ordinary agent is still refused, on both channels ──────────

func TestT63bfPlainAgentIsRefusedOnAllFourScheduledMessageVerbs(t *testing.T) {
	url, adminTok, plainTok := t63bfFixture(t)

	// Seed a REAL schedule through the admin face so the update/delete probes
	// aim at something that exists: a 403 on a missing row would be
	// indistinguishable from a 404 the choke never reached.
	status, seeded := doJSON(t, "POST", url+"/api/members/mira/scheduled-messages", adminTok,
		`{"label":"deny-face target","body":"x","cadence":"daily","hour":7,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("seed schedule via admin: want 200, got %d %v", status, seeded)
	}
	scheduleID, _ := seeded["id"].(string)
	if scheduleID == "" {
		t.Fatalf("seed returned no id: %v", seeded)
	}

	base := url + "/api/members/mira/scheduled-messages"
	cases := []struct {
		name, method, path, restBody string
		tool, args                   string
	}{
		{
			"list", "GET", base, "",
			"list_scheduled_messages", `{"member_id":"mira"}`,
		},
		{
			"create", "POST", base,
			`{"label":"sneaky","body":"x","cadence":"daily","hour":1,"minute":0,"timezone":"UTC"}`,
			"create_scheduled_message",
			`{"member_id":"mira","label":"sneaky-mcp","body":"x","cadence":"daily","hour":1,"minute":0,"timezone":"UTC"}`,
		},
		{
			"update", "PATCH", base + "/" + scheduleID, `{"status":"disabled"}`,
			"update_scheduled_message",
			`{"member_id":"mira","schedule_id":"` + scheduleID + `","status":"disabled"}`,
		},
		{
			"delete", "DELETE", base + "/" + scheduleID, "",
			"delete_scheduled_message",
			`{"member_id":"mira","schedule_id":"` + scheduleID + `"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/rest", func(t *testing.T) {
			status, data := doJSON(t, tc.method, tc.path, plainTok, tc.restBody)
			if status != 403 {
				t.Fatalf("plain agent %s %s: want 403, got %d %v — opening the MCP "+
					"entrance must not have widened the gate",
					tc.method, tc.path, status, data)
			}
			env, _ := data["error"].(map[string]any)
			if env == nil || env["code"] != "forbidden" {
				t.Fatalf("plain agent %s %s: want the standard forbidden envelope, got %v",
					tc.method, tc.path, data)
			}
		})
		t.Run(tc.name+"/mcp", func(t *testing.T) {
			// The tool RESOLVES for a plain agent (it is in tools/list for
			// everyone) and the loopback then hits the same choke REST does —
			// so the refusal arrives as isError, never as "unknown tool".
			result := t63bfCall(t, url, plainTok, tc.tool, tc.args)
			if result["isError"] != true {
				t.Fatalf("plain agent tools/call %s: want isError, got %v", tc.tool, result)
			}
			if body := t63bfResultText(t, result); !strings.Contains(body, "forbidden") {
				t.Fatalf("plain agent tools/call %s must be refused by the RBAC "+
					"choke (forbidden), got %s", tc.tool, body)
			}
		})
	}

	// The refusals really were authz and not a broken fixture: the seeded row is
	// still there for the admin, and neither of the plain agent's writes landed.
	status, listBody := get(t, base, adminTok)
	if status != 200 {
		t.Fatalf("control: admin LIST after the deny faces: want 200, got %d %s", status, listBody)
	}
	if !strings.Contains(listBody, scheduleID) {
		t.Fatalf("control: the plain agent's DELETE must NOT have landed: %s", listBody)
	}
	if strings.Contains(listBody, "sneaky") {
		t.Fatalf("control: the plain agent's CREATE must NOT have landed: %s", listBody)
	}
}
