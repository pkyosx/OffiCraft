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
	"strings"
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
	{"POST", "/api/machines/{machine_id}/bootstrap-here"}:               "install_warden_on_server_host",
	{"POST", "/api/machines/{machine_id}/teardown-here"}:                "uninstall_warden_on_server_host",
	{"POST", "/api/machines/{member_id}/upgrade"}:                       "upgrade_warden",
	{"POST", "/api/tasks/{task_id}/message"}:                            "post_task_message",
	{"GET", "/api/outsource-workers/{id}/boot-context"}:                 "get_outsource_worker_boot_context",
	{"POST", "/api/outsource-workers/{id}/refocus"}:                     "refocus_outsource_worker",
	{"POST", "/api/outsource-workers/{id}/stop"}:                        "stop_outsource_worker",
	{"POST", "/api/outsource-workers/{id}/restart"}:                     "restart_outsource_worker",
	{"DELETE", "/api/task-manuals/{type_key}"}:                          "delete_task_manual",
}

// t6020Revised holds the rows a LATER owner ruling moved off the admin floor.
// The 2026-07-26 ruling opened 19; this table is the difference between that
// history and today, so nothing the owner decided is ever deleted from the
// record — `len(t6020Opened) + len(t6020Revised)` must stay 19 (asserted below).
// ⚠️ That count locks the CARDINALITY, not the IDENTITY of the rows: DELETING a
// row is caught (18 != 19), but SWAPPING one — dropping row A and adding some
// other row B that already sits at the admin floor — keeps the count at 19 and
// every per-row assertion still holds for B, so A silently leaves the
// admin-floor assertion behind. NOT a guard of the authorization boundary — do
// not build on it as one. What actually stops a swap today is a human reading
// the diff. (The frozen manifest and the conformance auth matrix may or may not
// catch it as well; that has NOT been verified, so it is not claimed here.)
//
// 🔴 ADDING A ROW HERE REQUIRES ITS OWN OWNER RULING, AND YOU MUST EDIT
// THE `len(t6020Revised) == 2` GUARD BELOW IN THE SAME COMMIT. That guard is a
// hard-coded count on purpose, exactly like the release-exemption roster in root
// CLAUDE.md §13: without it this table is a back door — moving any row into it
// would exempt that row from the admin-floor assertion, and the diff would look
// like housekeeping. A count that can only be changed by editing itself forces
// the deliberate act.
var t6020Revised = map[[2]string]string{
	// owner 2026-08-07, card rc-3ff94b116970 (T-1b88): 「應該是owner(我)，或是開卡
	// 的人，都可以標為過期？」 — the same verb, two kinds of caller. The floor
	// dropped to agent and the author check moved in-handler
	// (callerMayExpireCard), because "is this MY card" is a per-card fact no
	// principal class can express. The two ANSWER rows were not revised: closing
	// someone else's ask with an answer is still governance.
	{"POST", "/api/reply-cards/{card_id}/expire"}: "expire_reply_card",
	// owner 2026-08-20, card rc-b896e3f641e7 (T-b56e), option 0:「開給執行者（可
	// 終止自己名下的票）」 — same verb, and again a per-task fact no principal
	// class can express ("is this MY task"). The floor dropped to agent and the
	// decision moved in-handler (callerMayTerminateTask), which also carries the
	// one subtraction the ladder cannot state: an OUTSOURCE worker is refused on
	// its own task, because a 正職 and a contractor both rank principalAgent.
	{"POST", "/api/tasks/{task_id}/terminate"}: "terminate_task",
	// owner 2026-08-21, card rc-376a41719e62 (T-ed79):「如果原本正職可以改 model
	// 外包就應該可以改，如果只有 mira 可以改，那就不變，正職跟外包一樣，mira 是特殊
	// 的意義，他代替 owner 執行高權限動作。」— the floor is decided by the STAFF
	// face of the same act (PATCH /api/members/{member_id}, machine floor since
	// T-5336), not by how the verb looks on its own. It dropped two rungs, to
	// principalMachine, and it is the ONLY one of the four worker lifecycle rows
	// the ruling moved.
	{"POST", "/api/outsource-workers/{id}/model"}: "set_outsource_worker_model",
}

// t6020RevisedFloor is the floor each revised row must now declare. Pinned as a
// map rather than assumed, so a second revision cannot silently inherit this
// one's answer.
var t6020RevisedFloor = map[[2]string]string{
	{"POST", "/api/reply-cards/{card_id}/expire"}: principalAgent,
	{"POST", "/api/tasks/{task_id}/terminate"}:    principalAgent,
	{"POST", "/api/outsource-workers/{id}/model"}: principalMachine,
}

// t6020AllOpenedRows is every row the 2026-07-26 ruling opened — those still at
// the admin floor PLUS those a later ruling revised. Coverage that is about
// "this tool exists and is fully described" must iterate THIS, not t6020Opened:
// a revised row is still one of the 19 the owner opened, and moving it out of
// the floor table must not quietly drop it out of the catalog and parameter-set
// teeth as well. (Only the FLOOR assertion is per-table, because that is the one
// fact the revision changed.)
func t6020AllOpenedRows() map[[2]string]string {
	all := make(map[[2]string]string, len(t6020Opened)+len(t6020Revised))
	for key, tool := range t6020Opened {
		all[key] = tool
	}
	for key, tool := range t6020Revised {
		all[key] = tool
	}
	return all
}

// t6020Withheld is the exact set the owner declined to open. The reason lives
// on each row in routes.go; the short version: minting an identity is
// self-escalation, and the password / Web Push rows are the owner's own account
// and own browser, not an office capability. T-c826 later added the owner's
// explicit choice that personal member identity/presentation also stays here.
//
// The five /api/auth/mfa* rows join them on the SAME reasoning as
// change-password, one step stronger: they decide how the OWNER authenticates.
// An admin_agent that could reach them could weaken — or, via enroll+activate,
// seize — the credential that governs it, so opening them would hand the office
// a way to escalate past its own owner. Off the MCP surface entirely for the
// same reason the password is: arming or disarming the owner's second factor is
// never something an agent does on the owner's behalf.
var t6020Withheld = [][2]string{
	{"POST", "/api/mint"},
	{"POST", "/api/auth/change-password"},
	{"GET", "/api/auth/mfa"},
	{"POST", "/api/auth/mfa/offer"},
	{"POST", "/api/auth/mfa/enroll"},
	{"POST", "/api/auth/mfa/activate"},
	{"POST", "/api/auth/mfa/disable"},
	{"GET", "/api/push/public-key"},
	{"POST", "/api/push/subscription"},
	{"DELETE", "/api/push/subscription"},
	{"PUT", "/api/members/{member_id}/avatar"},
	{"DELETE", "/api/members/{member_id}/avatar"},
}

// ownerFloorAfterT6020 is the SECOND list, and it exists because t6020Withheld
// cannot grow.
//
// 🔴 WHY NOT JUST APPEND TO t6020Withheld. That table is a HISTORICAL RULING
// SET — twelve rows, a pinned literal, and a written arithmetic ("7 from the
// owner rulings plus the 5 /api/auth/mfa* rows"). Appending a row the owner
// never ruled on in 2026-07-26 would make that sentence false while leaving it
// on the page, which is the exact disease the count was pinned to prevent. It
// would also carry a second claim by association: every row on that list is
// MCPExclude, and this one is only owner-floor.
//
// The completeness check below reads BOTH lists, so a new owner-floor row still
// has to be declared with a reason before it can exist. What changed is only
// which table it is declared in.
var ownerFloorAfterT6020 = map[[2]string]string{
	{"POST", "/api/lore/entries/{entry_id}/revive"}: "" +
		"T-33, owner ruling ta-c568dfd29844 D11. Retiring an entry splits three ways by " +
		"REASON — expired/merged are tidying and stay with the agent, falsified is a " +
		"judgement about truth and is the owner's — and REVIVING asserts the entry " +
		"holds after all, which sits on the same side of that line. ⚠️ The owner ruled " +
		"on the falsified half; putting revive beside it is a DERIVATION, recorded as " +
		"such in dal_lore_governance.go, and its cost is one-directional: a revive that " +
		"should have been an agent's is a message to the owner, while an agent quietly " +
		"reviving something the owner retired as false is not. NOT in t6020Withheld: " +
		"that list is the 2026-07-26 ruling's own rows and its count is pinned to keep " +
		"the ruling legible. Off the MCP surface for the ordinary reason every " +
		"owner-floor row is — the owner acts through the cockpit, not through tools.",
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
	// 16 still at the admin floor + 3 later revised = the 19 the ruling opened.
	// Split this way so a revision has to MOVE a row (visible in the diff) rather
	// than delete one; the sum keeps the historical count honest.
	if len(t6020Opened)+len(t6020Revised) != 19 {
		t.Fatalf("the 2026-07-26 ruling opened 19 routes; these tables account for %d "+
			"(%d still at the admin floor + %d revised by a later ruling) — a row was "+
			"dropped rather than moved",
			len(t6020Opened)+len(t6020Revised), len(t6020Opened), len(t6020Revised))
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

func TestT6020RevisedRoutesSitAtTheirRevisedFloor(t *testing.T) {
	// 🔴 THE COUNT LOCK. Three rows have been revised (expire_reply_card, owner
	// 2026-08-07; terminate_task, owner 2026-08-20; set_outsource_worker_model,
	// owner 2026-08-21). A fourth one needs its own
	// owner ruling, and whoever adds it must edit this line in the same commit —
	// that is the point: this table exempts a row from the admin-floor assertion
	// above, so growing it must be a deliberate, visible act, never a side effect.
	if len(t6020Revised) != 3 {
		t.Fatalf("t6020Revised lists %d rows, expected 3 — a further revision needs its "+
			"OWN owner ruling, and this guard must be edited in the same commit",
			len(t6020Revised))
	}
	index := t6020RouteIndex(t)
	tools := mcpToolIndex(defaultRouteSpecs())
	for key, wantTool := range t6020Revised {
		wantFloor, pinned := t6020RevisedFloor[key]
		if !pinned {
			t.Errorf("%s %s is in t6020Revised with no floor pinned in t6020RevisedFloor — "+
				"a revision without a stated floor asserts nothing", key[0], key[1])
			continue
		}
		spec, ok := index[key]
		if !ok {
			t.Errorf("%s %s is not in the route table at all", key[0], key[1])
			continue
		}
		// This is the ONLY route-layer assertion that the floor actually moved.
		// The handler tests in api_replycards_test.go drive the handler function
		// directly and never pass through requirePrincipalClass, so their green
		// says nothing about this. Put the floor back to admin_agent and THIS is
		// what must redden.
		if spec.Requires != wantFloor {
			t.Errorf("%s %s declares Requires=%q; the revising owner ruling put it at %q "+
				"(a higher floor would re-lock out the very caller the revision was for; "+
				"a lower one would hand it to warden/machine tokens)",
				key[0], key[1], spec.Requires, wantFloor)
		}
		if spec.MCPExclude {
			t.Errorf("%s %s is MCPExclude — a revised row is still an agent tool", key[0], key[1])
		}
		if got := spec.toolName(); got != wantTool {
			t.Errorf("%s %s exposes tool %q, want %q", key[0], key[1], got, wantTool)
		}
		if _, callable := tools[wantTool]; !callable {
			t.Errorf("tool %q does not resolve through mcpToolIndex", wantTool)
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
	for key, tool := range t6020AllOpenedRows() {
		if !listed[tool] {
			t.Errorf("%s %s is a live tool named %q but the frozen catalog does "+
				"not carry it — tools/list serves the catalog verbatim, so the "+
				"tool does not exist for any caller", key[0], key[1], tool)
		}
	}
}

func TestT6020WithheldRoutesStayOwnerOnlyAndOffTheMCPSurface(t *testing.T) {
	// 7 from the original rulings + the 5 second-factor rows (see the table's
	// note) = 12. The arithmetic is spelled out because it was WRONG here —
	// "7 + the 3 second-factor rows" against a literal of 12, in the one file
	// whose entire job is to make this count deliberate. The rows are
	// GET /api/auth/mfa and POST offer/enroll/activate/disable: five, not three.
	// The literal is kept so ADDING an owner-only row stays a deliberate act
	// with a reason attached.
	if len(t6020Withheld) != 12 {
		t.Fatalf("this table must list 12 owner-only routes and lists %d — 7 from the "+
			"owner rulings plus the 5 /api/auth/mfa* rows added by the MFA change "+
			"(see the note on the table). Do not read the 12 as an owner ruling: the "+
			"owner ruled on 7.", len(t6020Withheld))
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
	// The UNION, not just t6020Opened: a withheld row laundered into t6020Revised
	// would otherwise pass this check. (The sum guard would still catch it, but a
	// check whose stated job is disjointness must not have a hole in it — that is
	// how a second reader concludes the pair was verified when it was not.)
	opened := t6020AllOpenedRows()
	for _, key := range t6020Withheld {
		if tool, both := opened[key]; both {
			t.Fatalf("%s %s is listed as BOTH opened/revised (%s) and withheld", key[0], key[1], tool)
		}
	}
	withheld := map[[2]string]bool{}
	for _, key := range t6020Withheld {
		withheld[key] = true
	}
	// The second table (see its note): rows that reached the owner floor AFTER
	// the ruling, each carrying its own reason. Read here so the completeness
	// clause keeps its teeth without the historical count having to absorb them.
	for key, reason := range ownerFloorAfterT6020 {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("%s %s sits at the owner floor with an EMPTY reason — the "+
				"declaration is the whole mechanism", key[0], key[1])
		}
		if withheld[key] {
			t.Fatalf("%s %s is on BOTH owner-floor lists", key[0], key[1])
		}
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
// the 19 new descriptors. spec/mcp-catalog.json is generated (bin/gen-mcp-catalog
// renders it from the x-mcp metadata on spec/openapi.json's operations, and
// spec/mcp.md §5 describes that flow), but each descriptor still comes from a
// hand-written x-mcp.legacy.descriptor fragment that the generator emits
// VERBATIM: it cross-checks that fragment's name and description against x-mcp
// and that inputSchema is a JSON object, never what is inside inputSchema. So a
// descriptor missing a parameter is still the DEFAULT outcome of adding one by
// hand, not an exotic failure: the tool would list, resolve, and answer — and Mira
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
// makes a red run green. It existed for six PRE-EXISTING gaps (T-c362) — those
// were REPAID in T-1ba2 and the map is now empty, so today the only populated
// hatch is openapiOverweight. None of the 19 is in either, and none may ever be:
// these descriptors were written for T-6020, so a "known drift" on one of them
// could only ever mean "we shipped it wrong and wrote that down instead of
// fixing it".
func TestT6020OpenedToolsCarryTheirWholeParameterSet(t *testing.T) {
	// Anti-vacuity, in the two places this test can go dead. The premise used to
	// be `len(knownCatalogDrift) == 0` and it Fatal'd on exactly the state that
	// means the debt was PAID — its own message said to delete it in that case,
	// which T-1ba2 is doing. What actually has to be non-empty is (a) the corpus
	// the loop walks, and (b) at least ONE escape-hatch map, so the lookups below
	// are exercised against real data rather than two empty maps.
	rows := t6020AllOpenedRows()
	if len(rows) == 0 {
		t.Fatalf("t6020AllOpenedRows() is empty — this test would pass vacuously")
	}
	// ⚠️ INVERTED, NOT SILENCED (T-18). This used to Fatal when BOTH escape
	// hatches were empty, on the premise that at least one is always populated so
	// the lookups below run against real data. That premise expired:
	// knownCatalogDrift was emptied by T-1ba2 (debt repaid) and openapiOverweight's
	// only ever entry was open_gate.bind, which existed solely because
	// ReplyCardCreateDTO was shared by two operations — T-18 removed open_gate, so
	// the DTO has one operation and no field on it can be read by one face and
	// ignored by another. Both maps empty is now the CORRECT state.
	//
	// 🔑 SO THE ASSERTION IS TURNED AROUND, and this is strictly stronger than
	// either the old tripwire or a comment asking the next person to remember.
	// The first draft of this fix relaxed the check and left a 🔴 note saying
	// "restore a non-vacuity assertion if a hatch is ever repopulated" — but a
	// rule someone has to remember and a check that cannot be forgotten are not
	// the same strength of guarantee, which is the whole argument T-18 is built
	// on. Applying it here: the correct state (both empty) is what gets PINNED,
	// today, by something that is evaluated on every run.
	//
	// It also catches more than the loop below does. The loop only fires for the
	// 19 T-6020 tools; this fires for ANY tool re-entering either hatch, and it
	// makes "we re-derived why the maps are empty" a required step rather than an
	// optional one.
	if n := len(knownCatalogDrift) + len(openapiOverweight); n != 0 {
		t.Fatalf("an escape hatch is populated again (%d entries) — the T-18 reasoning "+
			"that both maps are correctly empty no longer holds; re-derive it or "+
			"restore a per-tool non-vacuity check", n)
	}
	for key, tool := range t6020AllOpenedRows() {
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
