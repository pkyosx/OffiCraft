package main

// authz_surface_gate_test.go — T-5336's structural node: a gate over the SHAPE
// that keeps producing these tickets, not over the one rule that was wrong.
//
// THE SHAPE. T-6020 (owner 2026-07-26) re-graded 24 operational endpoints in one
// pass. It could only see what it could enumerate, and what it enumerated was
// the ROUTE TABLE's `Requires` column. Authorization that lives ANYWHERE ELSE —
// a hard test inside a handler, or a write verb parked at the machine floor and
// guarded by nothing at all — was structurally invisible to that pass. The
// webhook CRUD rows this ticket fixed were exactly that: four write/read rows at
// `principalMachine`, whose only apparent protection (`MCPExclude`) was not an
// authz mechanism at all. So the defect is not "one rule was set wrong", it is
// "authz outside the route table does not get re-graded when the table does".
//
// This file is the enumeration T-6020 did not have. Two gates:
//
//	(1) TestAuthzOutsideTheRouteTableIsEnumerated — every caller-classification
//	    predicate in request-visible code must be listed here WITH A REASON. A
//	    new one is a build-time conversation, not a silent addition.
//	(2) TestMachineFloorWriteRoutesAreEachARuling — every WRITE verb declared at
//	    the machine floor must carry a ruling reference. Parking a mutating route
//	    at the floor is now an act someone signed for.
//
// ── WHY IT IS NOT ONE OF THE THREE USELESS GATES ────────────────────────────
//
// NOT TAUTOLOGICAL. Both gates were mutation-tested by hand (2026-07-27):
//   - adding one in-handler hard authz check (`currentScope(r) == "agent"` +
//     403 in HandleUpdateMemberApiMembersMemberIdPatch) → gate (1) RED;
//   - demoting one write route (`DELETE /api/members/{member_id}`) from
//     admin_agent back to principalMachine → gate (2) RED.
//     Both restored from a scratchpad copy afterwards, with the transcript in
//     the T-5336 report.
//
// NOT "MATCHES ITS OWN COMMENT". The scan is an AST walk (go/parser), not a
// text grep: comments and string literals are not expression nodes, so no prose
// in this file — or any other — can be mistaken for a predicate. This very file
// is also skipped by construction (_test.go files are never scanned).
//
// NOT "ASSERT EMPTY, THEN RANGE OVER THE EMPTY SET". This repo has shipped that
// bug before, so each gate proves its own corpus is populated BEFORE judging it:
// gate (1) fails if fewer files/functions/predicates than a floor were seen, and
// fails on a STALE inventory entry; gate (2) fails if the route table has no
// write rows or no machine-floor rows at all. A scanner that silently stops
// matching reddens. (This is the antidote for THESE TWO GATES only — it says
// nothing about that bug class elsewhere in the repo.)
//
// ⚠️ WHAT THESE GATES DO NOT DO — read this before trusting an entry. They
// cannot judge whether a reason is HONEST. A correctly-keyed entry with a
// plausible-sounding sentence ("legacy behaviour retained for backwards
// compatibility, revisit later") passes both gates; the length checks stop a
// one-word shrug, not a fluent one. Verified by the T-5336 review, which did
// exactly that and got two green gates.
//
// 🔴 A NAMED DEBT — THE SCANNER MATCHES THE SELECTOR NAME, NOT THE TYPE.
// `mentionsIdentity` treats any `.Kind` / `.RoleKey` selector as identity, and
// it has no idea what it was selected FROM. That is deliberate reach (it is how
// the "直接比對 kind" shape gets caught at all), and it is also a false-positive
// generator: `art.Kind == ArtifactKindLink` — a task ARTIFACT's content kind,
// file/image/link, nothing to do with any caller — trips it just as readily as
// `m.Kind == KindWarden` does. Every non-member type that grows a `Kind` field
// mis-fires here, and the T-60 artifact line has now done it twice.
//
// 🔑 MIS-FIRES ARE REGISTERED, NOT HIDDEN. The first one was settled by reading
// the artifact's kind into a local so the scanner could not see it. That is the
// one direction this file must never take: hiding drives the mis-fire count to
// zero, so the gate keeps LOOKING healthy exactly as it goes blind, and nobody
// is left holding evidence that the debt is growing. So the local-variable
// workaround is GONE (T-60 put `art.Kind ==` back inline) and the mis-fires are
// listed in nonCallerKindPredicates below, each with the reason it is not an
// identity read. THE LENGTH OF THAT MAP IS THE MIS-FIRE COUNT, and its growth
// is the pressure to pay the debt off — no one has to remember to notice.
//
// The debt itself is unchanged and it has a ticket: teach the scan the TYPE
// (resolve the selector's receiver so a member row and an artifact row stop
// looking alike), which means giving this file type information it does not
// have today — go/types over a loaded package instead of a bare go/parser walk.
// That is an owner-approved cost, and the evidence that buys the approval is
// nonCallerKindPredicates getting longer.
//
// What a mis-fire must NOT become is an entry in authzOutsideRouteTable. That
// map self-describes as the list of CALLER-CLASSIFICATION decisions; putting a
// non-authz predicate in it makes the list start lying about what it contains,
// and the next governance re-grade reads it as decisions to audit. The two maps
// are separate for that one reason.
//
// So the value of these lists is NOT that they cannot be padded — they can.
// It is that PADDING MUST SHOW UP IN THE DIFF: adding a decision outside the
// route table now requires editing this file, in the same commit, where a
// reviewer sees it. The gate converts a silent addition into a visible one.
// The only thing that stops a bad entry is a human reading the diff, so read
// new entries here as claims to check, not as decisions already approved.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── the scanner ─────────────────────────────────────────────────────────────

// authzFuncs are the identity seams. A predicate touching one of these is
// reading WHO IS CALLING, whatever else it does.
var authzFuncs = map[string]bool{
	"principalAtLeast":   true,
	"isOutsourceMember":  true,
	"classifyMember":     true,
	"currentScope":       true,
	"currentActor":       true,
	"principalOfRequest": true,
}

// authzIdents are the privilege-bearing constants. Comparing against one of
// these is a classification decision even when no seam function is involved —
// this is the "直接比對 kind / role_key" shape, which a scan keyed only on
// `currentScope` would walk straight past (and which is how the last one hid).
var authzIdents = map[string]bool{
	"principalOwner": true, "principalAdminAgent": true,
	"principalAgent": true, "principalMachine": true,
	"adminRoleKey": true, "machineKind": true,
	"KindStaff": true, "KindOutsource": true,
	"KindWarden": true, "KindHuman": true,
	// comparing an id against the owner's wire id IS a privilege test.
	"wireOwnerID": true,
}

// authzScanSkip are the files that ARE the central mechanism (or generated).
// Excluding them is the point of the gate, not a hole in it: authz.go +
// routes.go + server.go are precisely the place authorization is SUPPOSED to
// live, and this gate exists to find everything that is not there.
var authzScanSkip = map[string]bool{
	"authz.go": true, "routes.go": true, "server.go": true, "ocapi_gen.go": true,
}

// callerContextTypes are the parameter / receiver types through which a
// function can be handed the caller. ⚠️ A TYPO HERE IS SILENT: a name matching
// nothing simply shrinks the scan, and the corpus floors do not notice because
// the *http.Request functions alone clear them. That is not hypothetical — this
// list shipped with "outsourceSpawnRequest", a type that never existed, and the
// 發包 authorization choke sat outside the inventory from day one because of it.
// TestCallerContextTypesAllExist is the tooth that makes the typo loud.
// Written WITHOUT the leading `*`: baseTypeName strips pointer-ness from both
// sides before comparing, so a pointer variant can never become a fourth way to
// silently fall out of the scan. (The first run of the existence check below
// caught exactly that — "*taskCaller" and "*outsourceGateRequest" were listed
// and matched nothing.)
var callerContextTypes = []string{
	"http.Request",
	"taskCaller",
	"outsourceGateRequest",
}

// baseTypeName renders a type and strips one leading pointer star.
func baseTypeName(fset *token.FileSet, n ast.Node) string {
	return strings.TrimPrefix(exprText(fset, n), "*")
}

// authzSite is one found predicate, keyed the way the inventory keys it.
type authzSite struct{ file, fn, expr string }

func (s authzSite) key() string { return s.file + " :: " + s.fn + " :: " + s.expr }

// classifiesPrincipal reports whether a function DERIVES a principal class,
// whatever its signature. This is the second entry point into authz, and the
// one a signature-only scan misses entirely: resolveDispatchInitiator takes a
// bare `actorID string` and returns a principal class + member row — no caller
// context type anywhere, yet it is the classifier the 發包 choke runs on.
func classifiesPrincipal(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd.Body, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && authzFuncs[id.Name] &&
				(id.Name == "classifyMember" || id.Name == "principalAtLeast" ||
					id.Name == "isOutsourceMember") {
				found = true
			}
		case *ast.Ident:
			n := v.Name
			if strings.HasPrefix(n, "principal") && len(n) > 9 && n[9] >= 'A' && n[9] <= 'Z' {
				found = true
			}
		}
		return true
	})
	return found
}

// seesCaller reports whether a function can observe the caller at all. This is
// the scope boundary that makes the corpus meaningful rather than noisy: a
// `m.Kind == KindWarden` inside the reconcile loop is dispatch logic about some
// member, while the same expression in a function holding the *http.Request (or
// an already-resolved caller context) is a decision about the CALLER.
func seesCaller(fset *token.FileSet, fd *ast.FuncDecl) bool {
	isCallerType := func(n ast.Node) bool {
		got := baseTypeName(fset, n)
		for _, want := range callerContextTypes {
			if got == want {
				return true
			}
		}
		return false
	}
	// The RECEIVER counts too: `func (c taskCaller) isAdminCapable() bool` takes
	// no parameters at all, and a params-only scan walks straight past the
	// 正職授權矩陣 helpers — which is precisely the class of miss this gate is for.
	if fd.Recv != nil {
		for _, rc := range fd.Recv.List {
			if isCallerType(rc.Type) {
				return true
			}
		}
	}
	if fd.Type.Params != nil {
		for _, p := range fd.Type.Params.List {
			if isCallerType(p.Type) {
				return true
			}
		}
	}
	return classifiesPrincipal(fd)
}

func exprText(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// mentionsIdentity reports whether the subtree reads caller identity: a seam
// call, a privilege-bearing constant, or a `.Kind` / `.RoleKey` field read.
func mentionsIdentity(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.CallExpr:
			switch f := v.Fun.(type) {
			case *ast.Ident:
				if authzFuncs[f.Name] {
					found = true
				}
			case *ast.SelectorExpr:
				if authzFuncs[f.Sel.Name] {
					found = true
				}
			}
		case *ast.Ident:
			if authzIdents[v.Name] {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "Kind" || v.Sel.Name == "RoleKey" {
				found = true
			}
		}
		return true
	})
	return found
}

// scanAuthzSites walks the package's production sources and returns every
// caller-classification predicate in request-visible code, plus the corpus
// counters the anti-vacuity assertions run on.
func scanAuthzSites(t *testing.T) (sites []authzSite, files, funcs int) {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .go files found — the scan would be vacuous (cwd wrong?)")
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") || authzScanSkip[base] {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		files++
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !seesCaller(fset, fd) {
				continue
			}
			funcs++
			record := func(n ast.Node) {
				s := authzSite{file: base, fn: fd.Name.Name, expr: exprText(fset, n)}
				if seen[s.key()] {
					return
				}
				seen[s.key()] = true
				sites = append(sites, s)
			}
			// shape A — an equality/inequality test that reads identity.
			ast.Inspect(fd.Body, func(x ast.Node) bool {
				be, ok := x.(*ast.BinaryExpr)
				if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
					return true
				}
				if !mentionsIdentity(be) {
					return true
				}
				record(be)
				return false // the outermost comparison is the decision
			})
			// shape B — a bare boolean capability call (no comparison at all;
			// `if !principalAtLeast(...)` has no BinaryExpr to find).
			ast.Inspect(fd.Body, func(x ast.Node) bool {
				ce, ok := x.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch f := ce.Fun.(type) {
				case *ast.Ident:
					name = f.Name
				case *ast.SelectorExpr:
					name = f.Sel.Name
				}
				if name == "principalAtLeast" || name == "isOutsourceMember" ||
					name == "classifyMember" {
					record(ce)
				}
				return true
			})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].key() < sites[j].key() })
	return sites, files, funcs
}

// ── gate (1): the enumeration ───────────────────────────────────────────────

// The corpus floors. Deliberately well under the counts observed on 2026-07-27
// (36 predicates / 58 files / 150 caller-visible functions — the test Logf's
// them, so this comment can be re-checked instead of trusted): high enough that
// a scanner which stops matching reddens, low enough that ordinary refactoring
// does not.
//
// ⚠️ What these floors DO prove is "the scanner is alive". What they do NOT
// prove is "the scanner reaches every entry point it claims to" — a mis-spelled
// entry in callerContextTypes shrinks the scan while these stay satisfied, which
// is how the 發包 choke went unlisted. TestCallerContextTypesAllExist is the
// check for that second property; do not read these floors as covering it.
const (
	authzSiteFloor = 20
	authzFileFloor = 15
	authzFuncFloor = 30
)

// authzOutsideRouteTable is THE LIST — every caller-classification predicate
// that lives outside authz.go / routes.go, with the reason it is legitimate
// there. This is the artifact T-6020 did not have: the next governance re-grade
// reads this file and can see, in one place, what the `Requires` column does
// NOT cover.
//
// Adding an entry is not a formality. Before you do, answer: could this be a
// `Requires` value on the route instead? If yes, put it there — the table is
// enumerable, a handler body is not.
var authzOutsideRouteTable = map[string]string{
	// ── owner-only presentation folds (not access control, but they DO branch
	// on the principal, so a re-grade must see them) ──────────────────────────
	"account_display.go :: accountDisplayFold :: s.principalOfRequest(r) == principalOwner": "" +
		"account LABELS (the human-readable billing alias) are folded in for the owner " +
		"only; every other principal gets the raw key. Presentation, not access — but it " +
		"is a principal branch, so it belongs on this list.",
	"api_monitoring.go :: HandleGetMonitoringApiMonitoringGet :: s.principalOfRequest(r) == principalOwner": "" +
		"same owner-only account-label overlay, at the monitoring handler's call site.",

	// ── self-ops: identity from the token, never a parameter (CLAUDE.md §14) ──
	"api_members.go :: HandleGetMemberApiMembersMemberIdGet :: memberId == currentActor(r)": "" +
		"self-read fold: an outsource worker's recycle/wind-down hook refetches ITS OWN " +
		"row and must see desired_state/refocus_since. Strictly self-scoped — any OTHER " +
		"ow- target keeps the pre-fold 404, so this widens nothing for a third party.",
	"api_members.go :: HandleReportWakingApiSelfWakingPost :: m.Kind == KindOutsource": "" +
		"outsource workers do not drive the member presence machine; the ow- self-report " +
		"faces refuse rather than write presence for a非-roster identity.",
	"api_members.go :: HandleReportStoppingApiSelfStoppingPost :: m.Kind == KindOutsource": "" +
		"same outsource self-report refusal, stopping face.",
	"api_members.go :: HandleReportStoppedApiSelfStoppedPost :: m.Kind == KindOutsource": "" +
		"same outsource self-report refusal, stopped face.",
	"api_members.go :: HandleRestartSelfApiSelfRefocusPost :: m.Kind == KindOutsource": "" +
		"same outsource refusal on the self-refocus face.",

	// ── the hire self-promotion seam (root CLAUDE.md §4, owner ruling) ────────
	"api_members.go :: HandleHireMemberApiMembersPost :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"§4 閉環: hiring is at the machine floor, but hiring WITH kind/role_key is " +
		"privilege-bearing (an agent could otherwise hire itself a 'staff' colleague " +
		"and walk up the ladder). The floor cannot express 'this FIELD needs admin', so the " +
		"choke is necessarily in the handler. See the machine-floor ruling for " +
		"POST /api/members below — the two halves are one decision.",
	"api_members.go :: HandleHireMemberApiMembersPost :: trimmedOrEmpty(body.Kind) != \"\"": "" +
		"the `privileged` predicate feeding the admin choke above: which REQUEST FIELDS " +
		"make this hire privilege-bearing. Listed because the choke is only as correct " +
		"as this field set — a new privilege-bearing field added here without extending " +
		"it is the silent regression.",
	"api_members.go :: HandleHireMemberApiMembersPost :: trimmedOrEmpty(body.RoleKey) != \"\"": "" +
		"second half of the same `privileged` predicate (role_key is the admin_agent " +
		"discriminator itself).",

	// ── SSE zombie gate (T-9cf8 / presence, §3) ──────────────────────────────
	"api_infra.go :: HandleEventsApiEventsGet :: currentScope(r) == \"agent\"": "" +
		"the SSE downlink resolves the subscribing member from the TOKEN (never a query " +
		"param) — an identity read, not a permission test.",
	"api_infra.go :: HandleEventsApiEventsGet :: m.Kind == KindWarden": "" +
		"wardens and members share one online projection (§3) but get different stream " +
		"gating; the row's kind picks which.",

	// ── machine-roster scoping (kind is the machine discriminator) ────────────
	"api_machines.go :: HandleListMachinesApiMachinesGet :: m.Kind != machineKind": "" +
		"the machines collection is the roster FILTERED to kind==warden; this is the " +
		"filter, not a permission test.",
	"api_machines.go :: HandleDeleteMachineApiMachinesMemberIdDelete :: m.Kind != machineKind": "" +
		"refuse to delete a non-machine through the machines face (a typo'd member id " +
		"must 404 as a machine, not soft-delete a colleague).",
	"api_monitoring.go :: HandleGetMonitoringApiMonitoringGet :: m.Kind == machineKind": "" +
		"monitoring splits member rows from machine rows by kind for rendering.",

	"api_infra.go :: HandleResetCostApiMembersMemberIdCostResetPost :: m.Kind != KindOutsource": "" +
		"T-53 成本歸零. The route table already makes the CALLER owner-only; this " +
		"predicate picks which of the two accumulators the actor keeps its spend in, " +
		"and it is deliberately the same test bankLiveCost makes for the same id — a " +
		"staff member banks (and resets) on member.banked_cost, an outsource member " +
		"through the worker row. It cannot be a Requires floor: the caller is owner on " +
		"both arms, while the branch depends on the kind of the member named in the path.",

	// ── personal-avatar target scoping (T-c826, owner 2026-07-27) ────────────
	"api_members.go :: HandlePutMemberAvatarApiMembersMemberIdAvatarPut :: m.Kind == KindWarden": "" +
		"T-c826 owner ruling: the route table already makes the caller owner-only; this " +
		"separate predicate classifies the requested TARGET. Wardens are infrastructure, " +
		"not people with personal avatars, so the avatar face returns 422 for that kind.",
	"api_members.go :: HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete :: m.Kind == KindWarden": "" +
		"the delete half of the same T-c826 target rule. It cannot be a Requires floor: " +
		"the caller remains owner in both cases, while eligibility depends on the member " +
		"identified by the path parameter.",

	// ── chat ─────────────────────────────────────────────────────────────────
	"api_chat.go :: HandlePostChatApiChatPost :: currentActor(r) != wireOwnerID": "" +
		"the sender is taken from the verified token, never from the body (§14); this " +
		"compares the resolved actor to the owner's wire id to pick the sender label.",

	// ── lessons write authz — T-5336's OTHER half (owner rc-46599297a1c4) ─────
	"api_roles.go :: lessonsWriteAuthz :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"owner 2026-07-27 rc-46599297a1c4: lessons are TIERED, not governance-locked. " +
		"Admin+ writes any role; a plain agent writes only its OWN role (roster row's " +
		"role_key vs path role_key). A per-caller/per-target comparison the route table " +
		"has no column for. Pinned by api_lessons_admin_authz_t5336_test.go.",
	"api_insight.go :: insightWriteAuthz :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"T-3809: the insight doc's WRITE authz, same tier as lessons — admin+ writes any " +
		"role, a plain agent writes only its OWN role (roster row's role_key vs path " +
		"role_key), and READ is not gated at all (owner rc-dc171587220c: this release " +
		"closes nothing on the read face). A separate function rather than a call into " +
		"lessonsWriteAuthz ON PURPOSE: that one hard-codes the word \"lessons\" into its " +
		"403 body, so sharing it would refuse an insight write with advice pointing at " +
		"the wrong document. Pinned by api_insight_isolation_test.go.",
	"api_document_history.go :: documentHistoryAllowed :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"history reads use the machine floor, while restoring global context and role definitions must " +
		"retain their admin-only write boundary. Lessons restoration delegates to the existing per-role " +
		"write check; this caller-plus-document-kind decision cannot be expressed by one route floor.",
	"api_roles.go :: fillLessonsIdentityArgs :: currentScope(r) == \"agent\"": "" +
		"MCP-side default: an agent omitting role_key means ITS OWN role, resolved from " +
		"the verified sub — the §14 'identity from auth, never a parameter' rule.",
	"api_roles.go :: HandleDeleteRoleApiRolesRoleDelete :: m.RoleKey != role": "" +
		"NOT authz — an in-use scan (which members still reference this role). It reads " +
		"RoleKey so the scanner catches it; kept on the list rather than special-cased, " +
		"because a scanner tuned to hide its own false positives stops finding the true " +
		"ones too.",

	// ── task 正職授權矩陣 (T-23cf phase 2) ────────────────────────────────────
	"api_tasks.go :: taskCallerOf :: currentScope(r) == \"owner\"": "" +
		"resolvePrincipal's twin that also returns the roster row: owner scope has no " +
		"roster row, so it short-circuits before the lookup.",
	"api_tasks.go :: isAdminCapable :: principalAtLeast(c.principal, principalAdminAgent)": "" +
		"T-23cf 正職授權矩陣 helper over the already-resolved caller.",
	"api_tasks.go :: isOutsource :: isOutsourceMember(c.member)": "" +
		"T-23cf: classifyMember alone cannot tell 正職 from 外包 (both rank " +
		"principalAgent), so the durable Member.Kind is the discriminator.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: principal != principalOwner": "" +
		"T-23cf create matrix: who may create for whom.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: trimString(body.Target.Kind) == TaskExecutorOutsource": "" +
		"T-23cf: the TARGET's executor kind (a request field), paired with the caller " +
		"class above — the rule is about the pair, which no single Requires can state.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: principal != principalOwner": "" +
		"T-23cf reassign matrix, caller half: the owner may reassign anything; below owner " +
		"the rule depends on the TARGET, so it cannot be a single route floor.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: m.Kind == KindOutsource": "" +
		"T-23cf reassign matrix: an 外包 worker may not be reassigned like 正職.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: m.Kind == KindWarden": "" +
		"T-23cf reassign matrix: a warden is never a task executor.",
	"api_tasks.go :: callerMayDriveTask :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"admin+ may drive ANY task; below that only the task's own executor may — a " +
		"caller-vs-resource comparison, not expressible as a route floor.",
	"api_tasks.go :: callerMayDriveTask :: currentActor(r) == t.ExecutorID": "" +
		"the self half of the same rule: the executor drives its own task.",
	"api_tasks.go :: callerMayEditTaskText :: currentActor(r) == t.CreatorID": "" +
		"T-52, owner 2026-09-02 card rc-1bb6e01c4bf7. While a task has NO executor at " +
		"all (executor_id == ''), its CREATOR counts as the executor — at the " +
		"text-only doors ONLY: update_task (title/description), the description and " +
		"title routes, add/remove artifact, the two step-note write faces, and the " +
		"task_description / task_title restores. A 發包票 is born unbound and stays " +
		"unbound until the scheduler binds a worker, and for that whole window nobody " +
		"awake can fix a typo in the brief the contractor will read on arrival. It " +
		"cannot be a route floor: 'does this task have an executor yet' is a " +
		"per-task, per-moment fact, not a principal class. 🔴 THE CONDITION IS " +
		"executor_id == '' AND NOTHING ELSE, so the door SHUTS the instant a worker " +
		"is bound — the creator is then a flat 403 again, exactly as before. Owner " +
		"scoped this to 改文字類 and named the doors that stay shut (freeze/priority, " +
		"terminate, reassign, claim, mark_duplicate, plan, step status, closeout, " +
		"deps, linked reply cards); those keep callerMayDriveTask verbatim. Calling " +
		"this predicate from another handler reverses that ruling rather than " +
		"extending it.",
	"api_tasks.go :: callerMayWriteHandover :: currentActor(r) == t.ReassignedFrom": "" +
		"T-91, owner ruling: while a task sits under the `reassigning` lock, the " +
		"stamped PREDECESSOR may still write the handover record (the step note the " +
		"reassign notice orders it to write) — and nothing else. It cannot be a route " +
		"floor: the subject is a per-task, per-moment fact (task.reassigned_from " +
		"AND task.lock), not a principal class. 🔴 The owner was offered the WIDE " +
		"version — both sides fully authorised during the handover — and refused it, " +
		"so this predicate guards ONE door (the step-note write faces) and " +
		"callerMayDriveTask still guards every other task write. Widening its call " +
		"sites reverses that ruling; 全域脈絡 §3.4 is unchanged.",
	"api_replycards.go :: callerMayExpireCard :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"T-1b88 (owner 2026-08-07, card rc-3ff94b116970) revised T-6020 for the expire " +
		"row: admin+ may retire ANY reply card, and below that only the card's own " +
		"author may. The admin half is the part the route floor used to carry alone — " +
		"it stays here because the floor is now principalAgent, so this function is the " +
		"only thing distinguishing the two callers.",
	"api_replycards.go :: callerMayExpireCard :: currentActor(r) == card.FromMember": "" +
		"the author half of the same rule: an agent retires the ask IT opened. " +
		"Caller-vs-resource — 'is this MY card' is a per-card fact (FromMember, stamped " +
		"at create time), so no principal class can express it and the route table " +
		"cannot hold this guard. Answering is untouched and still governance.",
	"api_taskmanuals.go :: callerMaySetAssignee :: principalAtLeast(s.principalOfRequest(r), principalAdminAgent)": "" +
		"assigning a manual to someone else is an admin act; assigning to yourself is " +
		"not. Caller-vs-target again.",

	// ── 發包 (outsource dispatch) choke — T-23cf ⑦ ───────────────────────────
	// ⚠️ These two were MISSING from this inventory until the T-5336 review:
	// callerContextTypes named a type that does not exist, so outsource_gate.go
	// was never scanned. They are the reason TestCallerContextTypesAllExist now
	// exists. Enumerated here only — the gate's ladder is NOT this ticket's to
	// change (it looks self-consistent: deny only an initiator with no member
	// row and below admin; T-23cf deliberately has no whitelist above that).
	"outsource_gate.go :: outsourceSpawnGate :: principalAtLeast(req.PrincipalClass, principalAdminAgent)": "" +
		"THE 發包 choke (④): admin+ is an 'approver' and clears authn outright; below " +
		"that an initiator with no member row is denied (deny-by-default, mirroring " +
		"classifyMember). Cannot be a route floor — the scheduler tick dispatches with " +
		"no *http.Request in hand, so the same choke must serve both entry points.",
	"outsource_gate.go :: resolveDispatchInitiator :: classifyMember(m)": "" +
		"the classifier the choke above runs on, reached from a bare actorID (the " +
		"verified token sub, or a task's creator on the scheduler path). Route floors " +
		"cannot reach here at all: there is no request.",
	"outsource_gate.go :: resolveDispatchInitiator :: actorID == wireOwnerID": "" +
		"the owner short-circuit of that classifier — the owner has no roster row, so " +
		"it must be recognised by its wire id before any member lookup.",

	// ── owner's wire id as a routing/notification discriminator ──────────────
	"api_chat.go :: HandlePostChatApiChatPost :: msg.Recipient == wireOwnerID": "" +
		"NOT access control — it decides whether to enqueue a Web Push (only the owner " +
		"has a browser subscription). Listed because it compares against the owner " +
		"identity, and a scanner tuned to hide its own near-misses stops finding real ones.",
	"api_chat.go :: HandlePostChatApiChatPost :: msg.Sender != wireOwnerID": "" +
		"the other half of the same push condition: do not push the owner their own " +
		"message back.",
	// ── the by-id chat re-read's participation boundary (T-a828) is GONE ─────
	//
	// T-4e95 (owner ruling) removed it: it refused with 403 a message between two
	// other members, while the ordinary listing already served that same message
	// to the same caller via `with`. Two doors onto the same rows disagreeing
	// about who may open them is not a boundary, it is a discrepancy — and the
	// honest caller paid for it. Nothing is enumerated here now BECAUSE the
	// predicate no longer exists; if a per-row chat read check ever returns, it
	// belongs back in this map with its reason.
	"api_tasks.go :: taskCallerOf :: classifyMember(m)": "" +
		"the classification step of the task-caller resolver (the owner branch above " +
		"returns before this line). Same primitive as the 發包 path, different entry.",
}

// nonCallerKindPredicates — THE MIS-FIRE REGISTRY, and deliberately not part of
// authzOutsideRouteTable. Each key is a predicate the scanner flagged because it
// reads a selector called `Kind` / `RoleKey`, and which is NOT a caller-identity
// test at all: the scan has no type information, so it cannot tell a member
// row's kind from some other row's discriminator (see the NAMED DEBT in this
// file's header).
//
// Registering one is NOT an approval of the predicate — it is an admission that
// this gate got it wrong, kept where the next reader can count them. The
// alternative that was tried first, and rejected: reshape the production code
// (read the field into a local) until the scanner cannot see it. That keeps the
// count at zero while the gate goes blind, which is the worst of both.
//
// So: when this map grows, the scanner is getting further from the truth, and
// the fix is the type-aware scan the header names — not a longer map. The
// reason on each entry says WHAT the kind belongs to and why it is not identity.
var nonCallerKindPredicates = map[string]string{
	"api_tasks.go :: HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost :: kind != art.Kind": "" +
		"a task ARTIFACT's content kind (file/image/link) versus the kind the replace " +
		"body asked for — the immutability rule for a pinned deliverable. No principal " +
		"is on either side of this comparison.",
	"api_tasks.go :: HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost :: art.Kind == ArtifactKindLink": "" +
		"the same artifact content kind, picking which content rules apply (a link " +
		"carries a url, a file carries an attachment). Not a caller classification.",
	"api_tasks.go :: HandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet :: v.Kind != ArtifactKindLink": "" +
		"a retained artifact VERSION's content kind, deciding whether the version has a " +
		"blob to resolve a filename from. Same non-caller kind, read off the history row.",
	"api_tasks_artifact_upload.go :: HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost :: art.Kind == ArtifactKindLink": "" +
		"the pinned artifact's CONTENT kind on the raw-body replace door: a link's " +
		"content is a url rather than bytes, so that door is closed to one explicitly " +
		"instead of quietly turning it into a file. No principal is on either side.",
	"api_tasks_artifact_upload.go :: HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost :: k != art.Kind": "" +
		"the same artifact content kind: the kind SNIFFED from the uploaded bytes versus " +
		"the pinned one, which is the T-60 immutability rule read through the T-92 " +
		"upload transport. Not a caller classification.",
}

func TestAuthzOutsideTheRouteTableIsEnumerated(t *testing.T) {
	sites, files, funcs := scanAuthzSites(t)

	// ── anti-vacuity: prove the corpus BEFORE judging it. If the scanner stops
	// matching (a renamed seam, a changed AST shape, a bad cwd), every loop
	// below would pass over nothing and this gate would go green while guarding
	// exactly zero. That failure mode has shipped in this repo before.
	if len(sites) < authzSiteFloor {
		t.Fatalf("the scan found only %d authz predicates (floor %d) — the SCANNER is "+
			"broken, not the code. Every check below ranges over this slice, so a green "+
			"run here would mean nothing. Fix the scan (or lower the floor deliberately, "+
			"in a commit that says why).", len(sites), authzSiteFloor)
	}
	if files < authzFileFloor || funcs < authzFuncFloor {
		t.Fatalf("scan corpus too small: %d files / %d request-visible functions "+
			"(floors %d / %d) — same reasoning as above", files, funcs, authzFileFloor, authzFuncFloor)
	}
	if len(authzOutsideRouteTable) == 0 {
		t.Fatalf("the inventory is empty — it is the artifact this gate exists to keep")
	}
	// Logged, not hard-coded in prose: any count quoted in a comment or in
	// server/CLAUDE.md can be re-checked with `go test -v -run Enumerated`
	// instead of being trusted. (The base commit of this ticket exists because
	// a comment that disagrees with the code is worse than no comment.)
	t.Logf("authz scan corpus: %d predicates / %d files / %d caller-visible functions "+
		"(inventory holds %d)", len(sites), files, funcs, len(authzOutsideRouteTable))

	found := make(map[string]bool, len(sites))
	var unlisted []string
	for _, s := range sites {
		found[s.key()] = true
		_, listed := authzOutsideRouteTable[s.key()]
		_, registered := nonCallerKindPredicates[s.key()]
		if !listed && !registered {
			unlisted = append(unlisted, s.key())
		}
	}
	if len(unlisted) > 0 {
		t.Errorf("authorization decided OUTSIDE the route table, and not on the list:\n  %s\n\n"+
			"This is the shape that made T-6020 miss the webhook rows: a governance "+
			"re-grade enumerates routes.go's Requires column, and anything deciding "+
			"elsewhere is invisible to it.\n"+
			"Do ONE of:\n"+
			"  (a) move the decision to the route table (a Requires value on the row) —\n"+
			"      preferred, because the table is enumerable and a handler body is not;\n"+
			"  (b) if it genuinely cannot be a route floor (caller-vs-target rules, "+
			"per-field privilege), add it to authzOutsideRouteTable with the reason and "+
			"the ruling it came from.\n"+
			"Do NOT delete it from the scan.\n\n"+
			"⚠️ If the predicate above is a `.Kind` that is NOT a caller's kind (an "+
			"artifact's file/image/link, some other row's discriminator), this gate has "+
			"MIS-FIRED: it matches the selector NAME and cannot see the type. Neither (a) "+
			"nor (b) applies, and do NOT add it to authzOutsideRouteTable — that map is "+
			"read as a list of authorization decisions.\n"+
			"  (c) register it in nonCallerKindPredicates with a one-line reason saying "+
			"whose kind it actually is. Do NOT instead reshape the code so the scan stops "+
			"seeing it (a local variable): that drives the mis-fire count to zero while "+
			"the gate goes blind. The registry's LENGTH is the evidence that buys the "+
			"type-aware scan — see the NAMED DEBT paragraph in this file's header.",
			strings.Join(unlisted, "\n  "))
	}

	// A STALE entry is a finding too: without this, the list can be padded with
	// guesses until nothing is ever unlisted — the inventory would silence the
	// gate instead of documenting it.
	var stale []string
	for key := range authzOutsideRouteTable {
		if !found[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("authzOutsideRouteTable lists predicate(s) that no longer exist:\n  %s\n"+
			"Either the code moved (re-key the entry) or the decision was removed (drop "+
			"it). An entry matching nothing is dead weight that makes the list "+
			"harder to read, and a stale key hides the fact that the live predicate "+
			"it used to name is now unlisted.", strings.Join(stale, "\n  "))
	}
}

func TestAuthzInventoryReasonsAreRealReasons(t *testing.T) {
	if len(authzOutsideRouteTable) == 0 || len(nonCallerKindPredicates) == 0 {
		t.Fatalf("empty inventory (%d listed / %d registered) — nothing to judge",
			len(authzOutsideRouteTable), len(nonCallerKindPredicates))
	}
	for key, reason := range authzOutsideRouteTable {
		if len(strings.TrimSpace(reason)) < 40 {
			t.Errorf("%s: the reason is %d chars. An entry exists to tell the NEXT "+
				"governance re-grade why this decision is not on the route table; "+
				"'legacy' or 'ok' does not do that.", key, len(strings.TrimSpace(reason)))
		}
	}
	for key, reason := range nonCallerKindPredicates {
		if len(strings.TrimSpace(reason)) < 40 {
			t.Errorf("%s: the mis-fire reason is %d chars. It has to say WHOSE kind this "+
				"is and why no principal is on either side; a shrug lets a real identity "+
				"test be waved through as a false positive.", key, len(strings.TrimSpace(reason)))
		}
	}
}

// TestNonCallerKindRegistryEntriesStillExist is the registry's tooth.
//
// A mis-fire registry rots in one specific way: the predicate is rewritten or
// deleted, the entry stays, and from then on the map's LENGTH — the whole point
// of keeping it, since it is the evidence that buys the type-aware scan — counts
// mis-fires that no longer happen. Nothing else in this file would notice: gate
// (1) only fails on predicates it CANNOT find in a map, never on a map holding
// keys it cannot find in the code.
//
// (authzOutsideRouteTable has the same check inline in gate (1); the registry
// gets its own test so a stale registration names itself rather than arriving as
// one more line under an authorization heading it does not belong to.)
func TestNonCallerKindRegistryEntriesStillExist(t *testing.T) {
	if len(nonCallerKindPredicates) == 0 {
		t.Fatalf("the registry is empty — nothing to judge. If the type-aware scan " +
			"finally landed, delete this test with it, in the same commit.")
	}
	sites, _, _ := scanAuthzSites(t)
	if len(sites) < authzSiteFloor {
		t.Fatalf("the scan found only %d predicates (floor %d) — the SCANNER is broken, "+
			"and every key below would look stale for the wrong reason",
			len(sites), authzSiteFloor)
	}
	found := make(map[string]bool, len(sites))
	for _, s := range sites {
		found[s.key()] = true
	}
	var stale, doubleListed []string
	for key := range nonCallerKindPredicates {
		if !found[key] {
			stale = append(stale, key)
		}
		if _, alsoAuthz := authzOutsideRouteTable[key]; alsoAuthz {
			doubleListed = append(doubleListed, key)
		}
	}
	sort.Strings(stale)
	sort.Strings(doubleListed)
	if len(stale) > 0 {
		t.Errorf("nonCallerKindPredicates registers mis-fire(s) that the scan no longer "+
			"reports:\n  %s\nThe predicate was rewritten or removed — drop the entry (or "+
			"re-key it). Left in place it inflates the mis-fire count, which is the exact "+
			"number the type-aware scan is being argued for on.",
			strings.Join(stale, "\n  "))
	}
	if len(doubleListed) > 0 {
		t.Errorf("predicate(s) in BOTH authzOutsideRouteTable and "+
			"nonCallerKindPredicates:\n  %s\nA predicate is either a caller "+
			"classification or a mis-fire, not both, and the two maps mean opposite "+
			"things to a governance re-grade.", strings.Join(doubleListed, "\n  "))
	}
}

// ── gate (2): write verbs at the machine floor ──────────────────────────────

// machineFloorRuling records WHO decided a mutating route could sit at the
// floor. `Ruling` must name a ticket or an owner date — a bare whitelist entry
// is exactly the thing nobody can re-audit later.
type machineFloorRuling struct{ Ruling, Why string }

var writeVerbs = map[string]bool{"POST": true, "PATCH": true, "PUT": true, "DELETE": true}

// machineFloorWriteRulings — every WRITE route declared at principalMachine,
// each traced to the decision that put it there. The floor attaches NO class
// choke (authz.go), so each of these is "any authenticated principal may do
// this", which is a claim, not a default.
var machineFloorWriteRulings = map[string]machineFloorRuling{
	"PATCH /api/members/{member_id}": {
		Ruling: "T-5336 · owner 2026-07-27",
		Why: "KEPT at the floor deliberately: members trust each other, so editing a " +
			"colleague's name/model/effort is housekeeping, not governance. The asymmetry " +
			"with DELETE on the same {member_id} (admin_agent) is known and accepted. " +
			"Full reasoning on the row in routes.go.",
	},
	"POST /api/outsource-workers/{id}/model": {
		Ruling: "T-ed79 · owner 2026-08-21 · rc-376a41719e62",
		Why: "brought DOWN two rungs to meet the staff face of the same act. The owner " +
			"ruled 「如果原本正職可以改 model 外包就應該可以改…mira 是特殊的意義，他代替 " +
			"owner 執行高權限動作」: the floor of 改 model is whatever PATCH " +
			"/api/members/{member_id} carries (machine, T-5336), because it is the same " +
			"housekeeping act on the other side of the roster, and admin_agent is " +
			"reserved for acts delegated on the owner's behalf. The other four worker " +
			"lifecycle rows were NOT moved. Full reasoning on the row in routes.go.",
	},
	"PATCH /api/accounts/{account_id}": {
		Ruling: "T-5336 裁定 3 · owner 2026-07-27",
		Why: "the account display-name overlay. The owner was asked in this very ticket " +
			"whether to raise it to admin (to close an asymmetry) and DECLINED — this " +
			"round changes no read-surface floor. Raising it needs a new ruling.",
	},
	"PATCH /api/machines/{machine_id}": {
		Ruling: "T-5336 裁定 3 · owner 2026-07-27",
		Why: "the machine display-name overlay, the twin of the account one above and " +
			"covered by the same declined proposal.",
	},
	"POST /api/members": {
		Ruling: "root CLAUDE.md §4 閉環",
		Why: "hiring is floor-level, but hiring WITH kind/role_key is admin-gated INSIDE " +
			"the handler (see the HandleHireMember entries in authzOutsideRouteTable) — " +
			"otherwise an agent hires itself a 'staff' colleague and walks up the ladder. The " +
			"floor here is half of a two-part decision, not an unguarded route.",
	},
	"POST /api/machines/renew-credential": {
		Ruling: "T-fc53 · owner 2026-08-19 提案「到期前自動換發」",
		Why: "the floor is FORCED here, not chosen: principalMachine ranks BELOW " +
			"principalAgent, so principalAgent would lock the WARDEN — the only " +
			"intended caller — out of its own renewal. The floor therefore admits " +
			"any authenticated principal, and the guard is inside the handler: " +
			"resolveMachine refuses every caller whose row is not an ACTIVE machine. " +
			"What an ordinary agent could reach if that handler check were dropped " +
			"is a PERMANENT credential for itself, which is exactly what " +
			"mintWardenToken's kind check exists to prevent — so the handler guard " +
			"carries a mutant of its own in api_machines_renew_tfc53_test.go. " +
			"The route also names NO target: the machine acted on is the caller's " +
			"verified sub, so the floor grants 'renew yourself', never 'renew anyone'.",
	},
	"POST /api/self/waking":   selfOpRuling,
	"POST /api/self/stopping": selfOpRuling,
	"POST /api/self/stopped":  selfOpRuling,
	"POST /api/self/refocus":  selfOpRuling,
	"POST /api/chat": {
		Ruling: "M1 wire freeze · root CLAUDE.md §14",
		Why: "talking is what every principal in the office does; the SENDER is taken " +
			"from the verified token and can never be forged via the body, so the floor " +
			"grants 'speak as yourself', not 'speak as anyone'.",
	},
	"POST /api/chat/mark-read": {
		Ruling: "M1 wire freeze · root CLAUDE.md §14",
		Why: "a read receipt for the CALLER's own reader id (from the token). Floor-level " +
			"by the same argument as POST /api/chat.",
	},
	"POST /api/chat/attachments": {
		Ruling: "M1 wire freeze",
		Why: "uploading a blob to attach to one's own message; the blob is inert until " +
			"referenced by a chat/reply-card the caller is entitled to post.",
	},
	"POST /api/reply-cards": {
		Ruling: "M1 wire freeze · root CLAUDE.md §14",
		Why: "agents OPEN cards, the owner answers them (the answer faces are " +
			"admin_agent since T-6020). Opening is the low-privilege half by design.",
	},
	"POST /api/agent/context": {
		Ruling: "M1 wire freeze",
		Why:    "an agent posting ITS OWN accumulated context back; identity from the token.",
	},
	"POST /api/monitoring/telemetry": {
		Ruling: "M1 wire freeze · T-9cf8",
		Why: "the machine heartbeat — wardens rank AT the machine floor, so this route " +
			"cannot be raised without cutting off the principals it exists for. Deleted " +
			"machines are cut off by the T-9cf8 revocation check in requireAuth instead.",
	},
	"POST /api/mcp": {
		Ruling: "M1 wire freeze · spec/mcp.md",
		Why: "the MCP transport envelope itself, not an operation. It re-enters the same " +
			"mux via the loopback, so every tools/call is re-authorized at the REAL " +
			"route's floor — raising this row would gate the transport, not the verbs.",
	},
}

var selfOpRuling = machineFloorRuling{
	Ruling: "root CLAUDE.md §14 · owner 2026-07-10",
	Why: "self-ops carry NO identity parameter — the server reads the caller from the " +
		"token, so the route can only ever affect the caller itself. Wardens rank at " +
		"the machine floor and must be able to report their own presence.",
}

func TestMachineFloorWriteRoutesAreEachARuling(t *testing.T) {
	specs := defaultRouteSpecs()
	if len(specs) == 0 {
		t.Fatalf("empty route table — every check below would be vacuous")
	}

	// ── anti-vacuity: the table must actually CONTAIN both halves of what this
	// gate judges, otherwise "no violations" means "nothing was looked at".
	var writes, machineFloor int
	for _, s := range specs {
		if writeVerbs[s.Method] {
			writes++
		}
		if s.Requires == principalMachine {
			machineFloor++
		}
	}
	if writes == 0 || machineFloor == 0 {
		t.Fatalf("route table has %d write rows and %d machine-floor rows — with either "+
			"at zero this gate guards nothing", writes, machineFloor)
	}

	live := map[string]bool{}
	for _, s := range specs {
		if !writeVerbs[s.Method] || s.Requires != principalMachine {
			continue
		}
		key := s.Method + " " + s.Path
		live[key] = true
		ruling, ok := machineFloorWriteRulings[key]
		if !ok {
			t.Errorf("%s is a MUTATING route declared at the %q floor, which attaches no "+
				"class choke at all — any authenticated principal may call it, wardens "+
				"included. That may well be right, but it is a decision someone has to "+
				"make: add it to machineFloorWriteRulings naming the ticket or the owner "+
				"date, or give the row a real Requires. (This is the exact shape of the "+
				"four webhook rows T-5336 fixed: floor-declared, guarded by nothing, and "+
				"invisible to the T-6020 re-grade.)", key, principalMachine)
			continue
		}
		if !strings.Contains(ruling.Ruling, "T-") && !strings.Contains(ruling.Ruling, "owner ") &&
			!strings.Contains(ruling.Ruling, "CLAUDE.md") && !strings.Contains(ruling.Ruling, "M1") {
			t.Errorf("%s: Ruling=%q names no ticket, owner date, or charter section — an "+
				"exemption nobody can trace is a whitelist, and a whitelist is how this "+
				"gate becomes decorative. This checks the FORM of the reference, not that the "+
				"ruling says what you claim — that is the reviewer's job", key, ruling.Ruling)
		}
		if len(strings.TrimSpace(ruling.Why)) < 40 {
			t.Errorf("%s: Why is %d chars; the next re-grade reads it to decide whether "+
				"the ruling still holds", key, len(strings.TrimSpace(ruling.Why)))
		}
	}

	// Stale exemptions are findings: a route that moved UP must lose its
	// exemption, or the list slowly becomes a place where anything can hide.
	var stale []string
	for key := range machineFloorWriteRulings {
		if !live[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("machineFloorWriteRulings exempts route(s) that are no longer write rows "+
			"at the machine floor:\n  %s\nDrop the entry (the route was raised — good) or "+
			"re-key it — an entry matching no live row is dead weight, and it hides "+
			"whether the row it used to name still needs an exemption.",
			strings.Join(stale, "\n  "))
	}
}

// TestCallerContextTypesAllExist is the tooth M1 of the T-5336 review bought.
//
// callerContextTypes is a list of TYPE NAMES compared as strings. A name that
// matches nothing does not fail — it silently narrows the scan, and the corpus
// floors in gate (1) cannot notice, because the *http.Request functions alone
// clear them. So the floors prove "the scanner is alive", NOT "the scanner can
// see every entry point it claims to". Those are different properties and only
// the first one was being checked.
//
// This is not a hypothetical: the list shipped naming "outsourceSpawnRequest",
// which has never existed in this tree (the real type is outsourceGateRequest),
// and so the 發包 authorization choke — an admit/deny decision plus a principal
// classifier — sat outside the inventory from the day the gate was written.
func TestCallerContextTypesAllExist(t *testing.T) {
	if len(callerContextTypes) == 0 {
		t.Fatalf("callerContextTypes is empty — the scan would fall back to the " +
			"principal-classifier criterion alone")
	}
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — the check below would be vacuous", err, len(paths))
	}
	fset := token.NewFileSet()
	hits := make(map[string]int, len(callerContextTypes))
	funcs := 0
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") || authzScanSkip[base] {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			funcs++
			var fields []*ast.Field
			if fd.Recv != nil {
				fields = append(fields, fd.Recv.List...)
			}
			if fd.Type.Params != nil {
				fields = append(fields, fd.Type.Params.List...)
			}
			for _, fld := range fields {
				text := baseTypeName(fset, fld.Type)
				for _, want := range callerContextTypes {
					if text == want {
						hits[want]++
					}
				}
			}
		}
	}
	if funcs == 0 {
		t.Fatalf("no function declarations parsed — this check would be vacuous")
	}
	for _, want := range callerContextTypes {
		if hits[want] == 0 {
			t.Errorf("callerContextTypes lists %q, which matches NO parameter or "+
				"receiver in this package. A name that matches nothing does not fail "+
				"loudly — it just shrinks the scan, and gate (1)'s corpus floors stay "+
				"green because the *http.Request functions alone clear them. Fix the "+
				"spelling, or drop the entry if the type is genuinely gone (and then "+
				"check what stopped being scanned).", want)
		}
	}
	t.Logf("caller-context type coverage over %d functions: %v", funcs, hits)
}
