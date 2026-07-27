package main

// routes_t6020_governance_test.go — the two teeth of the owner's 2026-07-26
// governance ruling, held at the ROUTE TABLE (the conformance suite holds the
// same two facts over the live wire; this is the cheap lane that reddens in
// seconds).
//
// The ruling has two halves and neither is self-enforcing:
//
//	(a) 19 operational routes moved from requires=owner + mcp_exclude down to
//	    requires=admin_agent + a real MCP tool. Left unpinned this rots into
//	    "the tool has a name but the choke still refuses", or into a row
//	    silently drifting back to owner during an unrelated edit — and every
//	    other test in the package would stay green through both.
//
//	(b) Owner-only routes were deliberately NOT opened. That is the half most likely to
//	    be undone by a well-meaning future edit ("these look like they were
//	    missed"), which is exactly why routes.go carries the reason per row and
//	    why this file refuses the change.

import (
	"encoding/json"
	"os"
	"testing"
)

// t6020Opened is the exact set the owner opened: route → the MCP tool name it
// must expose. Written out by hand (not derived from the table) on purpose — a
// list derived from the thing under test cannot disagree with it.
var t6020Opened = map[[2]string]string{
	{"GET", "/api/settings"}:                                            "get_settings",
	{"PATCH", "/api/settings"}:                                          "update_settings",
	{"GET", "/api/release/check"}:                                       "check_release",
	{"POST", "/api/update/upgrade"}:                                     "upgrade_station",
	{"GET", "/api/members/{member_id}/webhooks/{endpoint_id}/requests"}: "list_webhook_requests",
	{"POST", "/api/reply-cards/{card_id}/answer"}:                       "answer_reply_card",
	{"PUT", "/api/reply-cards/{card_id}/answer"}:                        "reanswer_reply_card",
	{"POST", "/api/reply-cards/{card_id}/expire"}:                       "expire_reply_card",
	{"POST", "/api/machines/{machine_id}/bootstrap-here"}:               "install_warden_on_server_host",
	{"POST", "/api/machines/{machine_id}/teardown-here"}:                "uninstall_warden_on_server_host",
	{"POST", "/api/machines/{member_id}/upgrade"}:                       "upgrade_warden",
	{"POST", "/api/tasks/{task_id}/terminate"}:                          "terminate_task",
	{"POST", "/api/tasks/{task_id}/message"}:                            "post_task_message",
	{"GET", "/api/outsource-workers/{id}/boot-context"}:                 "get_outsource_worker_boot_context",
	{"POST", "/api/outsource-workers/{id}/refocus"}:                     "refocus_outsource_worker",
	{"POST", "/api/outsource-workers/{id}/stop"}:                        "stop_outsource_worker",
	{"POST", "/api/outsource-workers/{id}/restart"}:                     "restart_outsource_worker",
	{"POST", "/api/outsource-workers/{id}/model"}:                       "set_outsource_worker_model",
	{"DELETE", "/api/task-manuals/{type_key}"}:                          "delete_task_manual",
}

// t6020Withheld is the exact set the owner declined to open. The reason lives
// on each row in routes.go; the short version: minting an identity is
// self-escalation, and the password / Web Push rows are the owner's own account
// and own browser, not an office capability. T-c826 later added the owner's
// explicit choice that personal member identity/presentation also stays here.
var t6020Withheld = [][2]string{
	{"POST", "/api/mint"},
	{"POST", "/api/auth/change-password"},
	{"GET", "/api/push/public-key"},
	{"POST", "/api/push/subscription"},
	{"DELETE", "/api/push/subscription"},
	{"PUT", "/api/members/{member_id}/avatar"},
	{"DELETE", "/api/members/{member_id}/avatar"},
}

func t6020RouteIndex(t *testing.T) map[[2]string]RouteSpec {
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

func TestT6020OpenedRoutesSitAtTheAdminAgentFloor(t *testing.T) {
	if len(t6020Opened) != 19 {
		t.Fatalf("the ruling opened 19 routes, this table lists %d", len(t6020Opened))
	}
	index := t6020RouteIndex(t)
	tools := mcpToolIndex(defaultRouteSpecs())
	for key, wantTool := range t6020Opened {
		spec, ok := index[key]
		if !ok {
			t.Errorf("%s %s is not in the route table at all", key[0], key[1])
			continue
		}
		if spec.Requires != principalAdminAgent {
			t.Errorf("%s %s declares Requires=%q; T-6020 put it at %q — %q would "+
				"re-lock the admin 助理 out, anything lower would hand an "+
				"operational verb to every agent",
				key[0], key[1], spec.Requires, principalAdminAgent, principalOwner)
		}
		if spec.MCPExclude {
			t.Errorf("%s %s is MCPExclude again — an excluded row is not in "+
				"tools/list, so the AI side cannot even learn the tool's name",
				key[0], key[1])
		}
		if got := spec.toolName(); got != wantTool {
			t.Errorf("%s %s exposes tool %q, want %q (the frozen catalog names it)",
				key[0], key[1], got, wantTool)
		}
		if _, callable := tools[wantTool]; !callable {
			t.Errorf("tool %q does not resolve through mcpToolIndex — tools/call "+
				"would answer unknown tool", wantTool)
		}
	}
}

func TestT6020OpenedToolsAreInTheFrozenCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct{ Name string } `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatalf("frozen catalog is empty — the loop below would prove nothing")
	}
	listed := make(map[string]bool, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		listed[tool.Name] = true
	}
	for key, tool := range t6020Opened {
		if !listed[tool] {
			t.Errorf("%s %s is a live tool named %q but the frozen catalog does "+
				"not carry it — tools/list serves the catalog verbatim, so the "+
				"tool does not exist for any caller", key[0], key[1], tool)
		}
	}
}

func TestT6020WithheldRoutesStayOwnerOnlyAndOffTheMCPSurface(t *testing.T) {
	if len(t6020Withheld) != 7 {
		t.Fatalf("the owner rulings withheld 7 routes, this table lists %d", len(t6020Withheld))
	}
	index := t6020RouteIndex(t)
	// Scan the tool index by ROUTE (not by name): a withheld row that grew a
	// tool would be a new name nobody thought to look for.
	byRoute := map[[2]string]string{}
	for name, spec := range mcpToolIndex(defaultRouteSpecs()) {
		byRoute[[2]string{spec.Method, spec.Path}] = name
	}
	if len(byRoute) == 0 {
		t.Fatalf("no MCP tools at all — the exclusion check below would be vacuous")
	}
	for _, key := range t6020Withheld {
		spec, ok := index[key]
		if !ok {
			t.Errorf("%s %s is not in the route table at all", key[0], key[1])
			continue
		}
		if spec.Requires != principalOwner {
			t.Errorf("%s %s declares Requires=%q — the owner explicitly kept "+
				"this route owner-only (T-6020: identity minting/account/browser; "+
				"T-c826: personal member avatar identity/presentation). Read the note on the row "+
				"in routes.go before changing this.", key[0], key[1], spec.Requires)
		}
		if !spec.MCPExclude {
			t.Errorf("%s %s lost MCPExclude — it must stay off the AI-callable "+
				"surface entirely, not merely be refused by the choke",
				key[0], key[1])
		}
		if name, leaked := byRoute[key]; leaked {
			t.Errorf("%s %s is reachable as MCP tool %q", key[0], key[1], name)
		}
	}
}

// TestT6020OpenedAndWithheldAreDisjointAndComplete keeps the two tables above
// honest against each other: the 24 rows the ruling ruled on are exactly the
// rows that USED to be requires=owner + mcp_exclude, so nothing may appear in
// both lists, and no row may be left at the owner floor without appearing in
// the withheld list. That last clause is the one that matters — it is how a
// future row quietly parked at requires=owner gets noticed instead of
// inheriting the ruling's silence.
func TestT6020OpenedAndWithheldAreDisjointAndComplete(t *testing.T) {
	for _, key := range t6020Withheld {
		if tool, both := t6020Opened[key]; both {
			t.Fatalf("%s %s is listed as BOTH opened (%s) and withheld", key[0], key[1], tool)
		}
	}
	withheld := map[[2]string]bool{}
	for _, key := range t6020Withheld {
		withheld[key] = true
	}
	var stragglers []string
	for _, spec := range defaultRouteSpecs() {
		if spec.Requires != principalOwner {
			continue
		}
		if !withheld[[2]string{spec.Method, spec.Path}] {
			stragglers = append(stragglers, spec.Method+" "+spec.Path)
		}
	}
	if len(stragglers) > 0 {
		t.Fatalf("route(s) still at the owner floor that the T-6020 withheld list "+
			"does not account for: %v — either the owner ruled on it (add it, with "+
			"its reason on the row) or it is new and needs its own ruling; an "+
			"unexplained owner-only row is how the last five stopped being "+
			"legible", stragglers)
	}
}

// TestT6020OpenedToolsCarryTheirWholeParameterSet is the field-level tooth for
// the 19 new descriptors. spec/mcp-catalog.json is HAND-MAINTAINED (spec/mcp.md
// §5 names a bin/dump-mcp-catalog that does not exist in this tree), so a
// descriptor missing a parameter is the DEFAULT outcome of adding one by hand,
// not an exotic failure: the tool would list, resolve, and answer — and Mira
// would simply have no way to send the argument.
//
// The comparison itself already exists and works: spec_catalog_conformance_test.go
// confronts routes ≡ openapi ≡ catalog on the parameter-NAME set, in both
// directions, for every non-excluded row — verified against three deliberate
// mutants on these very 19 (a dropped body field, a dropped path param, and an
// invented extra all redden it). Re-implementing that comparison here would be
// a second copy of one rule, which is the disease this repo keeps catching.
//
// What that test canNOT stop is the escape hatch: knownCatalogDrift silences a
// tool's missing parameters by name, and adding an entry is a one-line edit that
// makes a red run green. It exists for six PRE-EXISTING gaps (T-c362, frozen by
// the owner — not this ticket's to fix). None of the 19 is in it, and none may
// ever be: these descriptors were written today, so a "known drift" on one of
// them could only ever mean "we shipped it wrong and wrote that down instead of
// fixing it".
func TestT6020OpenedToolsCarryTheirWholeParameterSet(t *testing.T) {
	if len(knownCatalogDrift) == 0 {
		t.Fatalf("knownCatalogDrift is empty — the check below would be vacuous; " +
			"if the T-c362 debt really was paid, delete this guard's premise too")
	}
	for key, tool := range t6020Opened {
		if params, baselined := knownCatalogDrift[tool]; baselined {
			t.Errorf("tool %q (%s %s) has a knownCatalogDrift baseline %v — these 19 "+
				"descriptors were hand-written for T-6020, so a baseline on one of "+
				"them is not inherited debt, it is a parameter we got wrong and then "+
				"silenced. Fix spec/mcp-catalog.json instead: an agent whose only "+
				"view of a tool is tools/list cannot send an argument the "+
				"descriptor omits.", tool, key[0], key[1], params)
		}
		if params, overweight := openapiOverweight[tool]; overweight {
			t.Errorf("tool %q (%s %s) has an openapiOverweight entry %v — same "+
				"reasoning in the other direction: openapi would be advertising a "+
				"lever this operation ignores.", tool, key[0], key[1], params)
		}
	}
}
