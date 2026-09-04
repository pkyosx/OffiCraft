package main

// roster_widening_ledger_t14i6_test.go — T-14 項目 6 PR ②, the mechanical half
// of the caller audit `DAL.ListMembers`' own doc comment asks readers to do.
//
// ── WHAT WAS OPEN ───────────────────────────────────────────────────────────
//
// PR ② deleted `WHERE kind != 'outsource'` from DAL.ListMembers (dal.go) and
// deleted the twin ListMembersIncludingOutsource. The function's doc now tells
// the next engineer, in its own words, that "EVERY OTHER CALLER NOW SEES ROWS IT
// NEVER SAW BEFORE" and then enumerates some of today's call sites in prose.
//
// 🔴 THAT ENUMERATION HAD NOTHING HOLDING IT UP. Reviewed 2026-09-03: no test in
// this package enumerated ListMembers call sites, so a fourteenth caller could
// be added — seeing contractor rows nobody decided it should see — and every
// test in the repo stayed green. The prose would go on saying "three of today's
// call sites", now false, sitting INSIDE the very function that instructs
// readers to audit their call site. This ticket's stated reason is 「兩邊行為該
// 一樣，而每一次歪掉都是靜悄悄的」; a silently stale caller audit is that failure
// mode wearing the ticket's own clothes.
//
// It had already happened once before anyone wrote this file. `reconcile.go`'s
// dispatchIdentitySweepNow is a widened call site — it read the narrow query
// before PR ② and reads the whole member table now — and the doc's enumeration
// did not mention it. It is SAFE (its loop opens with `m.Kind != KindWarden`, so
// contractor rows fall out on the first line of the body), so this was never a
// defect; it was the first concrete instance of the list going quietly stale.
//
// ── WHAT THIS FILE DOES ─────────────────────────────────────────────────────
//
// One gate plus its teeth. The template is TestIdentityGatesAreEachOnTheRecord
// in lifecycle_identity_gate_t170e_test.go — same shape, same rules, and the
// section headings below are that file's, deliberately.
//
//	TestListMembersCallSitesAreEachOnTheRecord — every `.ListMembers()` call in
//	the package's production sources must appear in listMembersCallSiteLedger
//	with (a) a ruling on whether PR ② WIDENED it and (b) a written reason. It
//	runs BOTH directions: a call site with no entry is UNDOCUMENTED and fails by
//	name; an entry matching no call site is an ORPHAN and fails by name.
//
// 🔴 THERE IS NO HARD-CODED CALL-SITE COUNT ANYWHERE IN THIS FILE, and that is
// the design decision, not an omission. A rule that says "there are 13 call
// sites" neither reddens nor warns when the fourteenth appears if somebody
// bumps the 13 in the same commit — and bumping a number reads like arithmetic,
// while adding a ledger row with a reason reads like a decision. What is
// mechanical here is the QUERY (an AST walk for the call) and the BIDIRECTIONAL
// join against a NAMED table. The corpus floors below exist for the opposite
// job — proving the scanner still matches anything at all — and they are set
// far under today's counts precisely so they cannot double as a census.
//
// ── WHAT "WIDENED" MEANS, AND WHY THE SCANNER CANNOT DERIVE IT ──────────────
//
// Widened = the call site read `DAL.ListMembers` BACK WHEN that query carried
// `WHERE kind != 'outsource'`, and therefore sees a strictly larger population
// today than it saw before PR ②. The complement is the seven sites that used to
// name ListMembersIncludingOutsource: for those the merge renamed a symbol and
// changed nothing about the rows.
//
// ⚠️ THAT IS A FACT ABOUT HISTORY, NOT ABOUT THIS TREE. No AST walk over the
// current sources can recover it — both groups now spell the identical call.
// So `Widened` is a HUMAN RULING, re-derived by hand against origin/main
// (66e0eaf9) when this file was written and reported as six sites. The gate
// does NOT verify it.
//
// 🔴 AND IT DOES NOT NEED TO, BECAUSE `Widened` IS FROZEN HISTORY. It is a
// claim about two commits that have both already landed — 66e0eaf9 and PR ② —
// and no future edit to this tree can change whether a site was widened by a
// commit that is already in the graph. IT CANNOT ROT. That is the difference
// between this column and the prose enumeration it replaced: the prose made a
// claim about the PRESENT ("today's call sites"), which the next commit could
// falsify while nobody looked; this column makes a claim about the PAST, which
// nothing can. Do not come back to "make it verifiable" — an AST walk over
// today's sources could not confirm it even in principle, and there is no
// decay for a checker to catch.
//
// The one failure mode that WOULD be live is a NEW call site landing with
// `Widened: true` — a false statement about history, written today. That one
// is already closed by the shape of the join: a new call site defaults to
// UNLISTED, which is red and prints its name, never to a ruling. Its author
// must add a row in the same diff, where a reviewer sees the word `Widened`
// next to a call that obviously postdates PR ②.
//
// ── WHY THIS IS NOT ONE OF THE USELESS GATES ────────────────────────────────
//
// NOT TAUTOLOGICAL. Mutation-tested by hand at authoring time (2026-09-04),
// each mutant hashed before and after so that "it landed" is evidence rather
// than a memory, and each restored by copying a snapshot back with the hash and
// a byte-for-byte `cmp` verified (not with git — a restore that silently did
// nothing looks exactly like a restore that worked):
//   - UNDOCUMENTED, new caller. A fourteenth call site planted in an unrelated
//     handler (api_docs.go's HandleListDocsApiDocsGet, which has nothing to do
//     with the roster) → RED, naming `api_docs.go :: HandleListDocsApiDocsGet`,
//     scan 13→14. 🔴 Negative control on the same mutant: every pre-existing
//     ledger gate in this package — TestIdentityGatesAreEachOnTheRecord,
//     TestIdentityKindVocabularyIsComplete,
//     TestTickProducersHaveNoUndeclaredRosterLoop,
//     TestLifecycleTickProducerSetIsDerived and the driver parity tests —
//     stayed GREEN. That is the measurement that says this file adds reach
//     rather than restating reach something else already had.
//   - UNDOCUMENTED, ledger row removed. The dispatchIdentitySweepNow row
//     deleted while its call site stood → RED, naming the same key, ledger
//     13→12 rows.
//   - ORPHAN, caller removed. dispatchIdentitySweepNow's roster read replaced
//     by `var members []Member` (compiles, `go build ./...` clean) → RED on the
//     OTHER side of the join, naming `reconcile.go ::
//     dispatchIdentitySweepNow` as a row whose call site no longer exists, scan
//     13→12. This is the direction the undocumented check cannot see: nothing
//     is missing from the ledger, the ledger is describing something that is.
//
// NOT "ASSERT EMPTY, THEN RANGE OVER THE EMPTY SET". The scan proves its corpus
// before judging it: floors on call sites, files and declarations, plus a tooth
// (TestListMembersIsStillTheOnlyRosterQuery) that fails if the METHOD NAME the
// scanner keys on stops existing — a rename would otherwise take the whole scan
// to zero while every ledger row turned into an orphan, which reads as a
// bookkeeping problem rather than as a dead guard.
//
// NOT "MATCHES ITS OWN COMMENT". go/parser AST walk, not a text grep. Comments
// and string literals are not expression nodes: measured 2026-09-04, this
// package's production sources hold FOURTEEN prose mentions of ListMembers — in
// api_chat.go, api_helpers.go (3), api_monitoring.go, dal.go (2),
// lifecycle_roster.go (2), lifecycle_tick.go (2), outsource_sched.go and
// reconcile.go (2) — every one of them a comment line that `git grep` reports
// and this scanner does not. _test.go files are never scanned, which is also
// what keeps this file out of its own corpus.
//
// ── ⚠️ WHAT THIS GATE DOES NOT DO ───────────────────────────────────────────
//
//   - IT CANNOT JUDGE WHETHER A REASON IS TRUE. A fluent, false sentence passes;
//     the length check stops a shrug, not a lie. The value is that padding must
//     appear IN THE DIFF, in the same commit, where a reviewer sees it.
//   - IT CANNOT VERIFY `Widened` — and that column is frozen history, so there
//     is nothing for it to catch. See above.
//   - PACKAGE-LOCAL. It globs `*.go` in ocserverd's own directory. Measured
//     2026-09-04: `git grep -l ListMembers -- '*.go'` names 26 files, all 26
//     inside this directory and none outside it — so the scope happens to be
//     complete today, it is not guaranteed complete. (The zero outside was run
//     with a positive control: the identical pathspec-and-filter form asking for
//     `package main` instead returns 94 files outside this directory, so the
//     zero is a real absence and not a broken command.) A caller added in
//     another Go package is invisible here.
//   - IT IS A CALL-SITE CENSUS, NOT A BEHAVIOUR CHECK. It says every caller was
//     ruled on. It does not say the ruling was right, and it does not notice a
//     kind filter being DELETED from inside a listed call site — that is what
//     the behaviour tests around each site are for
//     (TestGetMonitoring_LiveContractorCountsAsOneAgentNotTwo,
//     role_delete_contractor_t14i6_test.go, the lifecycle driver parity tests).
//
// 🔴 NO LINE NUMBERS anywhere in this file. Keys are file + enclosing symbol. A
// pure-comment commit moves every line in a file and would otherwise invalidate
// the whole ledger at once; that has already happened on this ticket.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// rosterQueryMethod is the single name this scan keys on. It is a string
// compared against AST selectors, so a rename empties the scan silently —
// TestListMembersIsStillTheOnlyRosterQuery is the tooth on that.
const rosterQueryMethod = "ListMembers"

// rosterQueryDeletedTwin is the function PR ② deleted. It must STAY deleted:
// its return is what its name promised, so a caller that wants contractor rows
// today and finds this name back would reasonably assume the OTHER one is still
// narrow. Re-introducing it re-creates the two-query split this whole ticket
// exists to end, and it would do so without touching a single ledger row.
const rosterQueryDeletedTwin = "ListMembersIncludingOutsource"

// listMembersCallSiteRuling is one row of the caller audit dal.go's doc used to
// carry in prose.
type listMembersCallSiteRuling struct {
	// Widened is a human ruling about two landed commits, not a measurement of
	// this tree — frozen history that cannot rot; see the file header. True when
	// the site read ListMembers back when it was `WHERE kind != 'outsource'`,
	// and so sees contractor rows today that it never saw before PR ②.
	Widened bool
	// Reason must answer, for a widened site, WHY seeing contractor rows here is
	// safe — in terms of what this call site does with them, not in terms of
	// what the query returns.
	Reason string
}

// listMembersCallSiteLedger is the audit list, and it is now the ONE copy.
// dal.go's doc points here rather than re-enumerating, because two copies of a
// list is two things to keep true and the prose copy is the one nothing can
// check.
//
// Key: FILE :: ENCLOSING SYMBOL. Never a line number.
var listMembersCallSiteLedger = map[string]listMembersCallSiteRuling{

	// ── the six WIDENED sites ───────────────────────────────────────────────
	//
	// Re-derived by hand on 2026-09-04 against origin/main (66e0eaf9): these six
	// spelled `s.dal.ListMembers()` while that query still carried
	// `WHERE kind != 'outsource'`.

	"api_chat.go :: HandleChatUnreadCountApiChatUnreadCountGet": {
		Widened: true,
		Reason: "" +
			"the owner's unread dot. Contractor rows arriving is not merely safe here, " +
			"it is the BEHAVIOUR THE OWNER ASKED FOR — 2026-07-14: 外包要算、已移除的" +
			"不算. The fold's own predicate is `RosterStatus != RosterStatusRemoved`, " +
			"which is a liveness question and asks nothing about kind, and live " +
			"contractors also arrive independently off ListOutsourceWorkers into the " +
			"same `live` map, keyed by member id — so a contractor appearing on both " +
			"paths sets one map key twice rather than double-counting. Widening this " +
			"site changes no number the owner sees.",
	},

	"api_monitoring.go :: HandleGetMonitoringApiMonitoringGet": {
		Widened: true,
		Reason: "" +
			"the monitoring snapshot, and the ONLY one of the six where widening was " +
			"load-bearing rather than inert. Live contractors also arrive off " +
			"ListOutsourceWorkers further down the same handler, so without a filter " +
			"each would enter `actors` and `sources` twice on one host key — one agent " +
			"too many on the machine card. The handler asks " +
			"`lifecycleTickDriverFor(m) != driverReconcile`, the SAME named predicate " +
			"the two lifecycle halves ask, rather than a hand-written kind test, so the " +
			"split stays one definition. Behaviour pinned by " +
			"TestGetMonitoring_LiveContractorCountsAsOneAgentNotTwo — that test, not " +
			"this ledger row, is what fails if the filter is deleted.",
	},

	"api_roles.go :: HandleCreateRoleApiRolesPost": {
		Widened: true,
		Reason: "" +
			"the codename-uniqueness scan when a new member is minted without a name. " +
			"It WANTS the union of every name ever taken — staff, warden, contractor, " +
			"and soft-removed rows too — because a name collision is a collision " +
			"whoever holds it. 🔴 BUT WIDENING IT IS A STRICT NO-OP TODAY, NOT A " +
			"LATENT BUGFIX — re-derived 2026-09-04, and this row said the opposite " +
			"until then. The two name spaces cannot meet: a contractor's member Name " +
			"IS its codename (memberFromWorker, dal_tasks.go), the sole writer of a " +
			"codename is DeriveCodename (outsource_sched.go is its only non-test " +
			"caller), and DeriveCodename emits exactly `<O|S|H|X>-<n>` — a ONE-LETTER " +
			"stem. PickMemberName returns only a MemberNamePool entry or, once the " +
			"pool is exhausted, `<PoolName>-<n>`; every pool entry is a multi-letter " +
			"given name, so no name it can return and no name it can fold into " +
			"`taken` ever equals a codename, case-folding included. The contractor " +
			"rows now arriving change neither the available set nor the fallback. " +
			"Filtering nothing is still the decision here, made on purpose: the " +
			"question this fold asks is 'has ANYONE ever answered to this name', " +
			"which asks nothing about kind — so it is already right for the day a " +
			"contractor is named from the pool, and a kind test added here would be " +
			"the bug rather than the fix.",
	},

	"api_roles.go :: HandleDeleteRoleApiRolesRoleDelete": {
		Widened: true,
		Reason: "" +
			"the role-delete cascade, and this row is a NAMED DEBT rather than a " +
			"safety argument. The fold is narrowed by `m.RoleKey != role`, and an " +
			"outsource row's RoleKey is always \"\" (dal_tasks.go), so no contractor " +
			"can match a real role key today. That holds only while contractors have " +
			"no role: the day they get one, this handler hard-deletes a live " +
			"contractor together with its whole chat, unconfirmed. The dependency is " +
			"pinned so it fails loudly instead of silently — " +
			"role_delete_contractor_t14i6_test.go.",
	},

	"reconcile.go :: runReconcileTick": {
		Widened: true,
		Reason: "" +
			"the reconcile half of the merged lifecycle tick. Contractor rows now " +
			"genuinely arrive in `all`, and `lifecycleTickDriverFor(m) != " +
			"driverReconcile` — asked BEFORE the entry filter, because 'is this mine " +
			"to decide' comes before 'should it exist' — is the ONLY thing keeping " +
			"them out of the member FSM. It is the named predicate that REPLACED the " +
			"WHERE clause, so the split is falsifiable by a parity test instead of " +
			"being a property of a SQL string. Deleting the line re-creates the " +
			"measured double-drive: one row taking a `start` from both halves in the " +
			"same tick.",
	},

	"reconcile.go :: dispatchIdentitySweepNow": {
		Widened: true,
		Reason: "" +
			"🔴 THE SITE dal.go's PROSE AUDIT MISSED — this row is why the file " +
			"exists. It broadcasts a stop for a residual session to every OTHER " +
			"warden, and its loop opens with `m.Kind != KindWarden || m.RosterStatus " +
			"!= RosterStatusActive`, so a contractor row falls out on the body's first " +
			"line: a contractor is never a warden, so it can never be a sweep target. " +
			"Safe, and safe by a test the widening did not weaken. What was wrong was " +
			"only the enumeration in dal.go, which listed neither this site nor the " +
			"fact that it had been widened.",
	},

	// ── the seven sites PR ② DID NOT WIDEN ──────────────────────────────────
	//
	// These spelled ListMembersIncludingOutsource on origin/main (66e0eaf9).
	// They always saw the whole member table; the merge renamed the symbol they
	// call and changed nothing about the rows they fold. They are in the ledger
	// so that the join is over EVERY call site — a table covering only the
	// interesting half cannot tell "a new caller" from "a caller I chose not to
	// list", which is the same silent staleness one level up.

	"api_chat.go :: HandleListChatAttachmentsApiChatAttachmentsGet": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. " +
			"Chat attachments belong to conversations with contractors as much as with " +
			"staff, which is why it named the wide twin in the first place.",
	},
	"api_chat.go :: resumeFloorParts": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. The " +
			"resume floor is deliberately ONE roster read over the whole member table; " +
			"the contractors on it are the point.",
	},
	"api_machines.go :: HandleListMachinesApiMachinesGet": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. A " +
			"machine's occupancy counts every agent running on it, contractors " +
			"included, or the box looks emptier than it is.",
	},
	"api_members.go :: HandleListMembersApiMembersGet": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. This " +
			"is the roster endpoint itself; whatever narrowing the caller wants is a " +
			"query parameter applied above, not a property of the SELECT.",
	},
	"api_roles.go :: requireLessonsAddressableRole": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. It " +
			"asks whether any row at all still answers to a role key before letting " +
			"lessons be addressed to it, and a contractor holding it would count.",
	},
	"worker_spawn.go :: resolveWorkerPlacement": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. " +
			"Placement reasons about what already occupies a machine, so it must see " +
			"the contractors that are the very rows it is placing alongside.",
	},
	"worker_spawn.go :: reclaimWorkerSession": {
		Reason: "not widened — called ListMembersIncludingOutsource before PR ②. " +
			"Reclaiming a worker's session looks across every row that could be " +
			"holding it, which on the 外包 path is mostly contractor rows.",
	},
}

// listMembersCallSiteExpectedCount pins the sites where ONE key covers more than
// one call. A key is FILE :: FUNCTION, so two calls in one function collapse
// onto a single row and the reason already written there would silently claim to
// cover a decision nobody read. Empty today (measured 2026-09-04: every key
// matches exactly once); the check is here because the day it stops being empty
// is the day it matters, and adding it later means noticing first.
var listMembersCallSiteExpectedCount = map[string]int{}

// ── the scanner ─────────────────────────────────────────────────────────────

// listMembersCallSite is one found call, keyed the way the ledger keys it.
type listMembersCallSite struct{ file, encl string }

func (s listMembersCallSite) key() string { return s.file + " :: " + s.encl }

type rosterScanCounts struct {
	files, decls int
	occurrences  map[string]int
}

// scanListMembersCallSites walks every production source in the package
// directory and returns every `<expr>.ListMembers()` call, plus the corpus
// counters the anti-vacuity assertions run on.
//
// It walks whole DECLARATIONS rather than only function bodies, so a call from a
// package-level var initialiser is keyed "<package-level>" and shows up rather
// than being missed. The method DECLARATION in dal.go is a FuncDecl, not a
// CallExpr, so it is not a site.
func scanListMembersCallSites(t *testing.T) ([]listMembersCallSite, rosterScanCounts) {
	t.Helper()
	counts := rosterScanCounts{occurrences: map[string]int{}}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .go files found — the scan would be vacuous (cwd wrong?)")
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var sites []listMembersCallSite
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
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
		counts.files++
		for _, decl := range f.Decls {
			counts.decls++
			encl := "<package-level>"
			if fd, ok := decl.(*ast.FuncDecl); ok {
				encl = fd.Name.Name
			}
			ast.Inspect(decl, func(x ast.Node) bool {
				call, ok := x.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != rosterQueryMethod {
					return true
				}
				s := listMembersCallSite{file: base, encl: encl}
				// Occurrences are COUNTED, not deduped: two calls in one
				// function are two decisions wearing one name, and the ledger
				// row for the first would absorb the second in silence. Same
				// lesson the identity gate learned the hard way.
				counts.occurrences[s.key()]++
				if seen[s.key()] {
					return true
				}
				seen[s.key()] = true
				sites = append(sites, s)
				return true
			})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].key() < sites[j].key() })
	return sites, counts
}

// The corpus floors. Set well under the counts observed on 2026-09-04 (13 call
// sites / 95 files / 2474 declarations — the test Logf's all three, so this
// comment is re-checkable rather than something to trust): high enough that a
// scanner which stops matching reddens, low enough that ordinary refactoring
// does not.
//
// ⚠️ THESE ARE NOT A CENSUS. They prove the scanner is alive. They deliberately
// do NOT pin today's 13, because a floor that equals the count is a hard-coded
// number wearing a floor's name, and the whole point of this file is that the
// count is not the mechanism — the bidirectional join is.
const (
	rosterCallSiteFloor = 8
	rosterFileFloor     = 40
	rosterDeclFloor     = 800
)

func TestListMembersCallSitesAreEachOnTheRecord(t *testing.T) {
	sites, counts := scanListMembersCallSites(t)

	// ── anti-vacuity: prove the corpus BEFORE judging it. Every loop below
	// ranges over `sites`; if the scanner stops matching (a renamed method, a
	// changed AST shape, a bad cwd) each loop would pass over nothing and this
	// gate would go green while guarding exactly zero.
	if len(sites) < rosterCallSiteFloor {
		t.Fatalf("the scan found only %d ListMembers call sites (floor %d) — the "+
			"SCANNER is broken, not the code. A green run here would mean nothing. "+
			"Fix the scan, or lower the floor deliberately in a commit that says why.",
			len(sites), rosterCallSiteFloor)
	}
	if counts.files < rosterFileFloor || counts.decls < rosterDeclFloor {
		t.Fatalf("scan corpus too small: %d files / %d declarations (floors %d / %d) — "+
			"same reasoning as above", counts.files, counts.decls,
			rosterFileFloor, rosterDeclFloor)
	}
	if len(listMembersCallSiteLedger) == 0 {
		t.Fatalf("the ledger is empty — it is the artifact this gate exists to keep")
	}

	widened := 0
	for _, r := range listMembersCallSiteLedger {
		if r.Widened {
			widened++
		}
	}
	// Logged rather than asserted. Six is the number re-derived against
	// origin/main when this file was written; it is history, and a test that
	// asserted it would be asserting a fact about a commit rather than about
	// this tree. Print it so the claim stays re-checkable.
	t.Logf("ListMembers call-site scan: %d sites / %d files / %d declarations "+
		"(ledger holds %d rows, %d of them ruled WIDENED by PR ②)",
		len(sites), counts.files, counts.decls, len(listMembersCallSiteLedger), widened)

	// ── direction 1: UNDOCUMENTED. A call site with no ledger row. ──────────
	found := make(map[string]bool, len(sites))
	var unlisted []string
	for _, s := range sites {
		found[s.key()] = true
		if _, listed := listMembersCallSiteLedger[s.key()]; !listed {
			unlisted = append(unlisted, s.key())
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Errorf("ListMembers call site(s) with no stated ruling:\n  %s\n\n"+
			"DAL.ListMembers does NOT hand you 'the staff'. Since T-14 項目 6 it is "+
			"`SELECT … FROM member` with no WHERE at all: every kind, every "+
			"roster_status. Decide, at YOUR call site, whether a contractor row "+
			"belongs in your fold, then record the decision here.\n"+
			"Do ONE of:\n"+
			"  (a) if the fold is a lifecycle fold, ask "+
			"`lifecycleTickDriverFor(m) != driverReconcile` (lifecycle_roster.go) — "+
			"the named predicate that REPLACED the WHERE clause — rather than writing "+
			"a fresh kind test, so the split stays one definition and the parity "+
			"tests reach your handler;\n"+
			"  (b) if you genuinely want staff only and it is not a lifecycle fold, "+
			"write the kind test you mean, next to the reason you mean it;\n"+
			"  (c) if you want the whole roster, say so — filtering NOTHING is a "+
			"decision too, and three of the rows below are exactly that on purpose.\n"+
			"Then add a row to listMembersCallSiteLedger with Widened:false (a NEW "+
			"call site was never narrow, so it was not widened by anything) and the "+
			"reason. Do NOT delete the site from the scan.",
			strings.Join(unlisted, "\n  "))
	}

	// ── direction 2: ORPHAN. A ledger row matching no call site. ────────────
	//
	// Without this the ledger could be padded with guesses until nothing is ever
	// unlisted, and the table would SILENCE the gate instead of documenting it.
	// It is also the only thing that notices a call site being DELETED, which
	// matters because a stale row hides that the live site it used to name has
	// moved and is now unlisted.
	var orphan []string
	for key := range listMembersCallSiteLedger {
		if !found[key] {
			orphan = append(orphan, key)
		}
	}
	sort.Strings(orphan)
	if len(orphan) > 0 {
		t.Errorf("listMembersCallSiteLedger lists call site(s) that no longer "+
			"exist:\n  %s\n"+
			"Either the call moved (re-key the row — keys are FILE :: ENCLOSING "+
			"SYMBOL, and renaming a handler re-keys it) or the caller was deleted "+
			"(drop the row, and say so in the commit: one fewer place reasoning about "+
			"正職／外包 is the good outcome). A stale key also hides the fact that the "+
			"live site it used to name may now be unlisted.", strings.Join(orphan, "\n  "))
	}

	// ── the SECOND call wearing the first one's name ────────────────────────
	var doubled []string
	for key, n := range counts.occurrences {
		want, pinned := listMembersCallSiteExpectedCount[key]
		if !pinned {
			want = 1
		}
		if n != want {
			doubled = append(doubled, key+" — matched "+itoa(n)+"x, accounted "+
				itoa(want)+"x")
		}
	}
	sort.Strings(doubled)
	if len(doubled) > 0 {
		t.Errorf("a ListMembers call-site key matches a different number of calls "+
			"than the ledger accounts for:\n  %s\n"+
			"Each row carries ONE ruling and ONE reason. A second call in the same "+
			"function is a second fold nobody wrote a reason for, hiding behind the "+
			"first one's row. Either give it its own function (usually the right "+
			"answer — two roster reads in one handler is also two reads of a table "+
			"that can change between them), or pin the count in "+
			"listMembersCallSiteExpectedCount and extend the reason to cover BOTH.",
			strings.Join(doubled, "\n  "))
	}
	for key := range listMembersCallSiteExpectedCount {
		if _, ok := counts.occurrences[key]; !ok {
			t.Errorf("listMembersCallSiteExpectedCount pins %q, which the scan does "+
				"not match at all — a pin on nothing is dead weight that reads as "+
				"considered.", key)
		}
	}
}

func TestListMembersCallSiteReasonsAreRealReasons(t *testing.T) {
	if len(listMembersCallSiteLedger) == 0 {
		t.Fatalf("empty ledger — nothing to judge")
	}
	for key, r := range listMembersCallSiteLedger {
		// A widened site is the one a reader is being asked to trust: it sees
		// rows it did not see before, and the row has to say what happens to
		// them. The complement only has to say it is the complement.
		min := 60
		if r.Widened {
			min = 160
		}
		if n := len(strings.TrimSpace(r.Reason)); n < min {
			t.Errorf("%s (Widened=%v): the reason is %d chars, under %d. It exists to "+
				"tell the next person auditing 正職／外包 differences what this fold "+
				"does with a contractor row; 'safe' or 'unchanged' does not do that. "+
				"⚠️ This checks LENGTH, not honesty — a fluent false sentence passes, "+
				"which is why the diff is the real review.", key, r.Widened, n, min)
		}
	}
}

// TestListMembersIsStillTheOnlyRosterQuery is the tooth on the scanner's own
// vocabulary.
//
// rosterQueryMethod is a NAME compared as a string. A name that matches nothing
// does not fail loudly — it takes the scan to zero, at which point every ledger
// row becomes an orphan and the failure reads as thirteen bookkeeping problems
// rather than as "the guard is dead". The call-site floor would catch it, but it
// would catch it wearing the wrong message; this test says the true thing.
//
// It runs both directions, because the ticket has one deletion to keep as well
// as one name to keep:
//   - DAL.ListMembers must still be declared, in dal.go, as a method;
//   - DAL.ListMembersIncludingOutsource must still be GONE. Re-introducing the
//     twin re-splits the query PR ② unified, and it would do so without adding
//     or removing a single row from the ledger above — the whole table would
//     still join perfectly while the thing it documents had come apart.
func TestListMembersIsStillTheOnlyRosterQuery(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — this check would be vacuous", err, len(paths))
	}
	fset := token.NewFileSet()
	declared := map[string]string{} // method name → file it is declared in
	scanned := 0
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
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
		scanned++
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			declared[fd.Name.Name] = base
		}
	}
	if scanned < rosterFileFloor {
		t.Fatalf("only %d production files parsed (floor %d) — this check would be "+
			"judging a corpus too small to mean anything", scanned, rosterFileFloor)
	}
	if file, ok := declared[rosterQueryMethod]; !ok {
		t.Errorf("no method named %q is declared in this package any more. "+
			"scanListMembersCallSites keys on that exact string, so the roster "+
			"call-site gate is now scanning for something that does not exist and "+
			"guards NOTHING. If the roster query was renamed, rename "+
			"rosterQueryMethod with it and re-key nothing else; the ledger keys are "+
			"call sites, not the callee.", rosterQueryMethod)
	} else if file != "dal.go" {
		t.Errorf("%s is declared in %s, not dal.go. That is not wrong by itself, but "+
			"the doc comment the ledger in this file is the mechanical half of lives "+
			"in dal.go — move it with the function or the two halves drift apart.",
			rosterQueryMethod, file)
	}
	if file, ok := declared[rosterQueryDeletedTwin]; ok {
		t.Errorf("%s is back, declared in %s. T-14 項目 6 deleted it because with "+
			"`WHERE kind != 'outsource'` lifted the two queries were literally the "+
			"same SELECT, and 剩餘部分的一樣是程式碼階層的一樣, not two copies "+
			"behaving the same. Its mere existence re-creates the split: a reader "+
			"seeing a WIDE name will infer the other one is NARROW, which is exactly "+
			"the inference this ticket exists to make impossible. Note that the "+
			"call-site ledger cannot notice this on its own — every row still joins.",
			rosterQueryDeletedTwin, file)
	}
}
