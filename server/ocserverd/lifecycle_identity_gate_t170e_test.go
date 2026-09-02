package main

// lifecycle_identity_gate_t170e_test.go — T-170e stage 5. The guard stage 3
// named and deliberately did not attempt: LIFECYCLE-LIST-IS-OPT-IN-T170E.
//
// ── WHAT WAS STILL OPEN ─────────────────────────────────────────────────────
//
// Migration 00025's constitution says 外包＝正職: the only difference between a
// staff member and an outsource worker is that the worker is minted and released
// alongside its task. Owner, 2026-08-26: 「任何正職外包的差異化處理都需要重新檢視」
// and 「剩餘部分的一樣，是程式碼階層的一樣，不是又複製了兩份程式碼寫一樣的行為」.
//
// Stage 3 folded the two tick producers onto ONE entry filter
// (lifecyclePolicyFor) and ONE ordered list of pre-decide formalities
// (lifecycleRosterPasses), and lifecycle_roster_parity_t170e_test.go guards that
// list. Grep LIFECYCLE-LIST-IS-OPT-IN-T170E for the gap it left, in the words of
// the person who measured it: the parity test guards formalities that are ON the
// list. It does not guard the next person NOT USING THE LIST AT ALL. Stage 3's
// own reviewer planted that mutant — a staff-only roster loop written the old
// way, inline in runReconcileTick, never entered into the list — and every test
// in the package stayed green.
//
// 🔴 AND THAT IS THE SHAPE BOTH REAL FAILURES HAD. Neither the missing
// token-expiry lead nor the missing survived-stop sweep was a listed pass that
// somebody narrowed. Nobody wrote `Kind != KindOutsource` anywhere. The staff
// pass simply ran inside runReconcileTick, whose roster read is ListMembers, and
// ListMembers is `WHERE kind != 'outsource'` BY CONSTRUCTION (dal.go) — so the
// staff-only-ness was a property of the DATA SOURCE and appeared in no
// expression a kind-scan could ever see. A guard that only hunts for `Kind ==`
// would have caught neither of them.
//
// So this file is TWO gates (plus their teeth), and only the pair covers the shape:
//
//	(1) TestIdentityGatesAreEachOnTheRecord — every 身分閘 in the package's
//	    production sources (a Kind comparison, a switch on kind, a kind-seam
//	    call, or a member-kind constant written into a struct field) must be in
//	    identityGateLedger WITH A REASON. This is the DoD's literal sentence.
//	(2) TestTickProducersHaveNoUndeclaredRosterLoop — every iteration inside
//	    the functions named in lifecycleTickProducers (since T-14 item 5 that is
//	    runLifecycleTick, runReconcileTick and runOutsourceTick — the merged
//	    tick's entry joined the list rather than being excused) must be in
//	    lifecycleProducerLoopRulings with a reason and an expected count. A new
//	    pre-decide roster loop written the old way has no Kind expression to
//	    find, so what gate (2) has to go on is the ITERATION — and it sees only
//	    the iterations WRITTEN DIRECTLY IN THOSE FUNCTION BODIES. That is the
//	    spelling both real failures and stage 3's mutant had. It is NOT every
//	    spelling: the scan does not follow calls, so the same loop lifted into a
//	    helper and called from a producer is invisible to both gates (measured —
//	    see the third stated limit of REACH below, which is an open hole).
//	    Gate (2) is the one that reddens on stage 3's mutant, and
//	    TestLifecycleTickProducerSetIsDerived keeps its two-name denominator
//	    honest by enumerating every `*Tick` in the package and demanding each be
//	    classified — because "there are only two producers" is a universal claim
//	    and this ticket has been wrong about five of those already.
//
// ── ⚠️ GO WILL NOT HELP YOU HERE ────────────────────────────────────────────
//
// A struct literal that omits a field COMPILES. `lifecycleRosterPass{Name: …,
// Run: …}` with no AppliesTo is legal Go and yields a nil AppliesTo; a
// `identityGateLedger` entry with an empty reason is legal Go; a new pass added
// with a zero-valued anything is legal Go. NOTHING about these gates is enforced
// by the compiler — the type system cannot express "and you must have thought
// about this". If these tests are deleted, or skipped, or the ledger is emptied,
// the package still builds and still passes `go vet`. Do not read a green build
// as evidence that any of the below held.
//
// ── WHY THIS IS NOT ONE OF THE THREE USELESS GATES ──────────────────────────
//
// The template for this file is authz_surface_gate_test.go, in this package, and
// so is this section.
//
// NOT TAUTOLOGICAL. Both gates were mutation-tested by hand at authoring time
// (2026-08-27), each mutant hashed before and after so that "it landed" is
// evidence and not a memory, and each restored with the hash verified back:
//   - stage 3's own mutant, re-planted in shape: a staff-only roster loop added
//     inline in runReconcileTick under the runLifecycleRosterPasses call, with
//     NO kind expression in it → gate (2) RED, naming the producer and the loop.
//     Measured in both spellings — `for i := range members` (an existing key,
//     caught by the count) and `for _, cand := range members` (a new key). The
//     stage 3 parity test stayed GREEN on both, which is the division of labour
//     working, and is why this file had to exist.
//   - adding a fresh kind branch inside an existing handler
//     (`m.Kind != KindAssistant` in sseStopGateRefusal) → gate (1) RED, quoting
//     the file, the function and the expression.
//   - narrowing a listed formality to staff via a new AppliesTo → the stage 3
//     parity test RED, by pass name and again on the 外包 fixture.
//
// 🔴 AND THAT LAST MUTANT IS ALSO WHERE THIS GATE WAS FOUND WRONG. The first
// version of gate (1) DEDUPED sites by key, so the new `m.Kind != KindOutsource`
// keyed identically to the one recycle_loop_break already owned and the existing
// ledger entry absorbed it silently: green. The gate's own message said every
// identity gate carries a written reason, and a stand-in existed that made that
// message false while the check still passed — which is the definition of a
// check pointed at the wrong thing. It counts occurrences now, and the count
// check found three REAL doubled keys on its first run (two of them fine, and
// one whose ledger reason turned out to describe the wrong handler — see
// identityGateExpectedCount). Re-measured after the fix: the mutant is RED.
//
// Each assertion in this file was additionally probed with a "wrong but
// plausible" stand-in, all of which tripped: a stale ledger key, a typo in
// identityKindIdents, a typo in identitySeamFuncs, an exclusion naming a
// non-existent file, a renamed tick producer, a stale producer-loop ruling, a
// one-phrase reason, a new member kind declared in domain.go, a third
// `*Tick` roster producer added quietly, and disabling the comparison shape
// outright (corpus floor). Transcripts with shasums are in the T-170e stage 5
// report.
//
// NOT "MATCHES ITS OWN COMMENT". The scan is a go/parser AST walk, not a text
// grep. Comments and string literals are not expression nodes, so no prose in
// this file or any other can be mistaken for a gate — measured: worker_spawn.go
// carries three `Kind ==` occurrences inside comment blocks that `grep` reports
// and this scanner does not. _test.go files are never scanned, so this file is
// outside its own corpus by construction.
//
// NOT "ASSERT EMPTY, THEN RANGE OVER THE EMPTY SET". Every loop below proves its
// corpus is populated BEFORE judging it: gate (1) fatals under floors on sites /
// files / declarations and on a STALE ledger key; gate (2) fatals if either
// producer function cannot be found BY NAME (the name is the thing most likely
// to go stale, and a producer that vanished is a scan that guards nothing) or if
// the loops found fall under a floor. The exclusion list has its own tooth
// (TestIdentityScanExclusionsEachNameALiveFile) because an exclusion entry
// naming a file that does not exist SHRINKS NOTHING and looks fine, while an
// exclusion entry naming a file that was renamed silently drops it from the
// corpus — that exact typo class already cost this package once
// (callerContextTypes / "outsourceSpawnRequest", see TestCallerContextTypesAllExist).
//
// ── ⚠️ WHAT THESE GATES DO NOT DO ───────────────────────────────────────────
//
// They cannot judge whether a reason is HONEST. A correctly-keyed entry with a
// fluent, false sentence passes; the length check stops a one-word shrug, not a
// paragraph. The value is not that the ledger cannot be padded — it can. It is
// that PADDING MUST SHOW UP IN THE DIFF, in the same commit, where a reviewer
// sees it. Read new entries as claims to check, not as decisions already taken.
//
// Three stated limits of REACH, so that nobody reads this guard as wider than
// it is (a guard whose claimed range exceeds its real range is the very disease
// this ticket is about):
//
//   - PACKAGE-LOCAL. The scan globs `*.go` in ocserverd's own directory. Kind
//     gates in server/ocwarden, in the agent side, or in the Python tree are
//     NOT covered and never were. That is a scope, not a hole, but it is a
//     scope somebody could misread.
//   - THE `Kind:` STRUCT FIELD IS OVERLOADED IN THIS PACKAGE. Documents,
//     artifacts, reply cards, chat rows and handoff plans all have a field
//     literally named Kind carrying an unrelated vocabulary. The struct-literal
//     shape therefore fires only when the VALUE is one of the member-kind
//     constants (or when the field is ExecutorKind / ReassignedFromKind, which
//     are not overloaded and are exactly the 正職／外包 axis on the task side).
//     The test Logf's how many `Kind:` literals it saw and how many it kept, so
//     this sentence is re-checkable rather than trusted. The COMPARISON shape
//     has no such restriction — it is deliberately noisy, and the ledger carries
//     several "this is not an identity gate at all" entries as a result, because
//     a scanner tuned to hide its own false positives stops finding true ones.
//   - 🔴 GATE (2) IS IN-BODY ONLY. ONE CALL FRAME DOWN IS A HOLE, AND THE HOLE
//     IS OPEN TODAY. The scan walks the bodies of runReconcileTick and
//     runOutsourceTick and rules on the range/for statements it finds THERE. It
//     does not descend into what those bodies call, and nothing else in the
//     package does either. Measured on this commit, 2026-08-27, hashed before
//     and after and restored with the hash verified back: take the same
//     staff-only roster loop stage 3's mutant used, lift it verbatim into a new
//     method `s.m1cStaffOnlySweep(members, now)`, and call that method from
//     runReconcileTick right after runLifecycleRosterPasses — every test in the
//     package stays GREEN, the stage 3 parity test included. Gate (1) is blind
//     to it because there is still no kind expression; gate (2) is blind to it
//     because what the producer body gained is a CALL, not a loop. Writing the
//     same loop into runLifecycleRosterPasses' own body is green for the same
//     reason. So the reach of these two gates over
//     LIFECYCLE-LIST-IS-OPT-IN-T170E is NARROWED, NOT CLOSED: the inline
//     spelling — the one both real failures and stage 3's mutant actually had —
//     is caught, and the helper spelling is caught by nothing. This is stated as
//     a hole, not as a roadmap: there is no test, no lint and no review step
//     standing on it as of this commit, and reading past this paragraph on the
//     assumption that "someone will add it" is exactly how the first version of
//     this claim came to be false.
//
// 🔴 NO LINE NUMBERS anywhere in this file. Keys are file + enclosing symbol +
// expression text. A pure-comment commit moves every line in a file and would
// otherwise invalidate the whole ledger at once; that has already happened on
// this ticket.

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

// ── the vocabulary ──────────────────────────────────────────────────────────

// identityKindIdents is the member-kind closed set (domain.go) plus the two
// authz literals that spell the same thing (authz.go machineKind) and the task
// side's executor-kind pair. Comparing against one of these is a statement about
// WHICH POPULATION a row belongs to.
//
// ⚠️ A missing entry here shrinks the scan silently.
// TestIdentityKindVocabularyIsComplete is the tooth: it re-derives the closed
// set from the package's own const declarations and fails if one is not listed,
// so a NEW member kind cannot be added without entering this scan.
var identityKindIdents = map[string]bool{
	"KindAssistant":         true,
	"KindWarden":            true,
	"KindOutsource":         true,
	"machineKind":           true,
	"TaskExecutorMember":    true,
	"TaskExecutorOutsource": true,
}

// identityKindFields are the struct fields that carry a population label.
var identityKindFields = map[string]bool{
	"Kind":               true,
	"ExecutorKind":       true,
	"ReassignedFromKind": true,
}

// identityKindFieldsNotOverloaded are the subset of the above whose name is used
// for NOTHING but the 正職／外包 axis, so a struct literal setting one is always
// a population statement whatever the value's spelling.
var identityKindFieldsNotOverloaded = map[string]bool{
	"ExecutorKind": true, "ReassignedFromKind": true,
}

// identitySeamFuncs are helpers that answer the population question on behalf of
// a caller. Calling one IS the gate, even with no comparison in sight.
// TestIdentityKindVocabularyIsComplete asserts each is really declared here — a
// name matching nothing would not fail loudly, it would just shrink the scan.
var identitySeamFuncs = map[string]bool{
	"isOutsourceMember": true,
}

// ── the exclusion list ──────────────────────────────────────────────────────
//
// 🔴 THIS IS AN EXCLUSION LIST OVER THE WHOLE TREE, AND THAT IS DELIBERATE.
// This repo has paid for the alternative: an earlier survey on this very ticket
// used an INCLUSION list ("scan these places") and missed the same site four
// rounds running, because nobody thought of the place. Inverting it — scan
// everything, name what you leave out and why — reported 72 sites on the first
// pass. If you find yourself wanting to add an entry here to quiet a noisy
// finding, put the finding in the ledger with the reason instead. An entry here
// removes a whole FILE from the corpus forever.
//
// _test.go files are skipped structurally rather than through this map: test
// fixtures construct rows of every kind by the hundred, and skipping them is
// also what keeps this file from scanning itself.
var identityScanSkip = map[string]string{
	"message_keys_gen.go": "" +
		"generated (`Code generated … DO NOT EDIT`) from frontend i18n message keys; " +
		"a map of string keys to bools, no control flow, and a kind gate cannot appear " +
		"here without someone editing the generator instead. Measured 2026-08-27: it " +
		"holds two `Kind`-spelled hits, both inside STRING KEYS " +
		"(settings.assigneeKindMember / …Outsource), which are not expression nodes " +
		"and would produce nothing even if scanned — so this exclusion hides zero " +
		"findings today rather than an unknown number.",
	"theme_colornames_gen.go": "" +
		"generated colour-name table, same argument: constants only, no branches. " +
		"Measured 2026-08-27: zero occurrences of any kind vocabulary at all.",
	"theme_fonts_gen.go": "" +
		"generated font table, same argument as the colour-name table above, and " +
		"likewise measured at zero occurrences.",
}

// ── the scanner ─────────────────────────────────────────────────────────────

// identityGateSite is one found gate, keyed the way the ledger keys it: FILE ::
// ENCLOSING SYMBOL :: EXPRESSION. Never a line number.
type identityGateSite struct{ file, encl, shape, expr string }

func (s identityGateSite) key() string { return s.file + " :: " + s.encl + " :: " + s.expr }

func identityExprText(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// identityMentioned reports whether a subtree names a population: a member-kind
// constant, or a read of a kind-bearing field.
func identityMentioned(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.Ident:
			if identityKindIdents[v.Name] {
				found = true
			}
		case *ast.SelectorExpr:
			if identityKindFields[v.Sel.Name] {
				found = true
			}
		}
		return true
	})
	return found
}

type identityScanCounts struct {
	files, decls, kindLiteralsSeen, kindLiteralsKept int
	// occurrences counts how many times each ledger key was matched. A key
	// matched twice is two decisions wearing one name — see the note on
	// `record` and identityGateExpectedCount.
	occurrences map[string]int
}

// scanIdentityGates walks every production source in the package directory and
// returns every identity gate, in four shapes, plus the corpus counters the
// anti-vacuity assertions run on.
//
// Note that it walks whole DECLARATIONS, not only function bodies: a gate can
// live in a package-level `var` initialiser (a struct literal holding a
// closure), which a FuncDecl-driven walk never reaches. Such a site is keyed
// with the enclosing symbol "<package-level>".
func scanIdentityGates(t *testing.T) ([]identityGateSite, identityScanCounts) {
	t.Helper()
	counts := identityScanCounts{occurrences: map[string]int{}}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .go files found — the scan would be vacuous (cwd wrong?)")
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var sites []identityGateSite
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if _, skip := identityScanSkip[base]; skip {
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
			record := func(shape, expr string) {
				s := identityGateSite{file: base, encl: encl, shape: shape, expr: expr}
				// 🔴 OCCURRENCES ARE COUNTED, NOT DEDUPED, and that is not a
				// detail. An earlier version of this scanner deduped by key, and
				// a hand-planted mutant walked straight through it: narrowing
				// stale_stopping_clear to staff introduces a SECOND
				// `m.Kind != KindOutsource` inside lifecycleRosterPasses, which
				// keys identically to the one recycle_loop_break already has, so
				// the ledger's existing entry silently absorbed a brand-new
				// divergence. The stage 3 parity test caught that mutant; this
				// gate did not, and "another test covers it" is not a reason for
				// a check to be wrong about its own subject.
				counts.occurrences[s.key()]++
				if seen[s.key()] {
					return
				}
				seen[s.key()] = true
				sites = append(sites, s)
			}
			ast.Inspect(decl, func(x ast.Node) bool {
				switch v := x.(type) {

				// shape A — a comparison that reads a population. The outermost
				// comparison is the decision, so the walk stops descending.
				case *ast.BinaryExpr:
					if (v.Op == token.EQL || v.Op == token.NEQ) && identityMentioned(v) {
						record("compare", identityExprText(fset, v))
						return false
					}

				// shape B — a `switch` whose tag reads a population. A switch has
				// no BinaryExpr at all, so shape A walks straight past it; each
				// case arm is its own gate and is recorded by name.
				case *ast.SwitchStmt:
					if v.Tag == nil || !identityMentioned(v.Tag) {
						return true
					}
					tag := identityExprText(fset, v.Tag)
					for _, st := range v.Body.List {
						cc, ok := st.(*ast.CaseClause)
						if !ok {
							continue
						}
						if cc.List == nil {
							record("switch", tag+" switch default")
							continue
						}
						for _, e := range cc.List {
							record("switch", tag+" switch case "+identityExprText(fset, e))
						}
					}

				// shape C — a struct literal stamping a population onto a row.
				// This is where a population is CREATED rather than tested, and
				// it is invisible to a comparison-only scan. See the reach note
				// in the file header for why the overloaded `Kind:` field is
				// narrowed by value and the two executor fields are not.
				case *ast.KeyValueExpr:
					k, ok := v.Key.(*ast.Ident)
					if !ok || !identityKindFields[k.Name] {
						return true
					}
					counts.kindLiteralsSeen++
					if !identityKindFieldsNotOverloaded[k.Name] && !identityMentioned(v.Value) {
						return true
					}
					counts.kindLiteralsKept++
					record("literal", identityExprText(fset, v))

				// shape D — a seam call that answers the population question for
				// the caller. `if isOutsourceMember(m)` has no comparison and no
				// constant in it; shapes A–C all miss it.
				case *ast.CallExpr:
					name := ""
					switch fn := v.Fun.(type) {
					case *ast.Ident:
						name = fn.Name
					case *ast.SelectorExpr:
						name = fn.Sel.Name
					}
					if identitySeamFuncs[name] {
						record("seam", identityExprText(fset, v))
					}
				}
				return true
			})
		}
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].key() < sites[j].key() })
	return sites, counts
}

// ── gate (1): every 身分閘 is on the record ──────────────────────────────────

// The corpus floors. Set well under the counts observed on 2026-08-27 (96 gates
// / 84 files / 2271 declarations — the test Logf's all three, so this comment is
// re-checkable rather than something to trust): high enough that a scanner which
// stops matching reddens, low enough that ordinary refactoring does not.
//
// ⚠️ These prove "the scanner is alive". They do NOT prove "the scanner reaches
// everything it claims to" — a mis-spelled kind constant or a renamed seam
// shrinks the scan while these stay satisfied. That second property is
// TestIdentityKindVocabularyIsComplete's job; do not read these floors as
// covering it.
const (
	identityGateFloor = 50
	identityFileFloor = 50
	identityDeclFloor = 800
)

// identityGateExpectedCount pins the keys that legitimately match more than one
// site. It is deliberately EMPTY today: every gate in this package is textually
// unique within its function, and keeping it that way is the healthier state —
// two identical expressions in one function almost always want to be one named
// helper. An entry here is a promise that the ledger's single reason covers
// every occurrence, so add one only after reading them all.
var identityGateExpectedCount = map[string]int{
	// The list-filter expression, once for `executor=outsource` and once for
	// `executor=unassigned` — same test, two facets of one query parameter.
	"api_tasks.go :: HandleListTasksApiTasksGet :: t.ExecutorKind != TaskExecutorOutsource": 2,
	// Two distinct uses of the same already-normalised local: the argument to
	// authorizeTaskCreate, and the dispatch-spec inheritance.
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: executorKind == TaskExecutorOutsource": 2,
	// 🔴 THIS PIN IS WHY THE COUNT CHECK EXISTS. The two sites are NOT the same
	// decision, and the ledger's first draft described neither of them correctly
	// — it called this a "target-kind normalisation", which is what the CREATE
	// handler does, not this one. The check found the second site and the second
	// site is what showed the reason was wrong. See the entry itself.
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: kind == TaskExecutorOutsource": 2,
}

// identityGateLedger is THE LIST. Every identity gate in the package's
// production sources, with the reason it is legitimate.
//
// 🔴 BEFORE YOU ADD AN ENTRY, answer the owner's question
// (2026-08-26): 「任何正職外包的差異化處理都需要重新檢視」. Could this difference
// be deleted instead? The constitution (migration 00025) says 外包＝正職 and the
// ONE slot the difference is allowed to live in is lifecyclePolicyFor.ShouldExist
// — 「正職會不會有 instance 存活取決於人物設定有沒有這個角色，外包則是取決於 task
// 還是不是未完成狀態」. Anything else is either a different axis (machine vs
// person, task-executor vs member), a genuine wire/storage projection, or a
// divergence that needs a ruling. Say which, in the reason.
var identityGateLedger = map[string]string{

	// ── the ONE slot the 正職／外包 difference is allowed to live in ──────────
	"lifecycle_roster.go :: lifecyclePolicyFor :: m.Kind == KindOutsource": "" +
		"THE entry filter, and the only place the owner's 2026-08-26 ruling permits a " +
		"正職／外包 branch: a worker is alive while its task is unfinished, a member " +
		"while the roster carries it. Every other pre-decide difference is supposed to " +
		"be spelled as an AppliesTo on the shared list instead of here.",
	"lifecycle_roster.go :: lifecyclePolicyFor :: m.Kind == KindWarden": "" +
		"the staff arm's warden carve-out: a warden is never an agent-lifecycle " +
		"spawn/stop candidate, it is the thing that EXECUTES them — unless it is being " +
		"uninstalled, which is the one case the reconcile tick must still drive. A " +
		"machine-vs-person axis, not the 正職／外包 one.",
	"lifecycle_roster.go :: lifecycleRosterPasses :: m.Kind != KindOutsource": "" +
		"recycle_loop_break's AppliesTo — THE one declared staff-only formality, and " +
		"the mechanism working as designed: a worker already has a loop-break in " +
		"autoHandoverWorker arm (1) asking a different question, and two collectors on " +
		"one latch is the double-kill T-72dd removed. Read back by name in " +
		"lifecyclePassContractedReach; converging the two rules needs its own " +
		"owner-gated step.",
	"lifecycle_roster.go :: lifecycleRosterPasses :: m.Kind == KindWarden": "" +
		"uninstall_intent_consume's AppliesTo. Warden-only, and it always was — the " +
		"pass's own loop opened with the same test; hoisting it into the list changed " +
		"nothing about what runs and made the restriction readable from the list, " +
		"which is the whole mechanism. Machine-vs-person axis.",

	// ── the population primitives (authz.go / domain.go) ────────────────────
	"authz.go :: isOutsourceMember :: m.Kind == KindOutsource": "" +
		"THE seam itself. classifyMember cannot tell 正職 from 外包 (both rank " +
		"principalAgent), so the durable Member.Kind is the discriminator, in one " +
		"function, which is what every other caller is supposed to ask instead of " +
		"re-typing the comparison.",
	"authz.go :: classifyMember :: m.Kind == machineKind": "" +
		"the principal ladder's machine rung: a warden row classifies as " +
		"principalMachine. Machine-vs-person, not 正職／外包 — an outsource row and an " +
		"assistant row both fall through to the same agent rung here, which is exactly " +
		"the code-level sameness the constitution asks for.",
	"authz.go :: agentIatFloorRefusal :: m.Kind == machineKind": "" +
		"the T-14 項目 4B credential floor SKIPS machine rows. Machine-vs-person, not " +
		"正職／外包 — an outsource row and an assistant row are both subject to the floor " +
		"here, on the same line, which is the code-level sameness the constitution asks " +
		"for. The exemption exists because a warden credential is scope=\"agent\" with " +
		"NO exp (mintWardenToken), so a floor raised above one could never expire out " +
		"of the way: the machine would be off the fleet permanently and only a hand " +
		"re-install would bring it back. Warden does not call report_waking today, but " +
		"that is a property of the client today, not a contract — pinned by " +
		"TestAgentIatFloor_WardenPermanentTokenIsExempt.",
	"authz.go :: isRemovedMachine :: m.Kind == machineKind": "" +
		"the T-9cf8 revocation check: a DELETED machine's still-valid token must stop " +
		"being honoured. Only machine rows have a permanent credential, so only they " +
		"can be in this state — the kind test is what scopes the check to them.",
	"authz.go :: permanentCredentialRefusal :: m.Kind != machineKind": "" +
		"refuse to mint a never-expiring credential for anything that is not a machine. " +
		"An agent that obtained one would hold a permanent token; this is the guard " +
		"that keeps the exemption attached to the credential and not to a caller.",
	"api_auth.go :: mintWardenToken :: m.Kind != machineKind": "" +
		"the same refusal at the mint site — a warden token has no exp claim at all, " +
		"so handing one to a non-machine row would be a permanent credential for an " +
		"agent. Machine-vs-person axis.",
	"domain.go :: ValidateMember :: m.Kind != KindAssistant": "" +
		"the closed-set validation itself (schema CHECK mirror): kind must be one of " +
		"the three. Not a behavioural gate — this is the definition of the vocabulary " +
		"the rest of this ledger is written in.",
	"domain.go :: ValidateMember :: m.Kind != KindOutsource": "" +
		"second arm of the same closed-set validation; see the KindAssistant arm.",
	"domain.go :: ValidateMember :: m.Kind != KindWarden": "" +
		"third arm of the same closed-set validation; see the KindAssistant arm.",

	// ── roster resolution: which face may see which population ─────────────
	"api_helpers.go :: resolveMember :: m.Kind == KindOutsource": "" +
		"the staffOnly arm of the member lookup. This refusal used to be " +
		"UNCONDITIONAL and was therefore inherited by all 16 of its callers, including the READ " +
		"door — which made GET /api/members/{id} answer 404 for a row that " +
		"GET /api/members had just listed, and cost the cockpit one guaranteed " +
		"failed request plus a whole-roster refetch per contractor chat line. " +
		"Owner ruling 2026-08-28: 「其他真的要過濾要明確指定」 — so reads default to " +
		"the whole population and the verbs that must refuse a contractor ask for " +
		"THIS resolver by name (mint / bootstrap, which would hand it the WRONG " +
		"boot document; the stop family, whose contractor equivalents drive a " +
		"different kill funnel; dismissal, which is not how a contractor leaves; " +
		"webhook write + the unauthenticated inlet; and relocate, which needs the " +
		"refusal as CONTROL FLOW to fall through to the worker core). " +
		"A wire-surface split, not a lifecycle difference.",
	"api_helpers.go :: resolveMachine :: m.Kind != machineKind": "" +
		"the mirror of the above on the machine face: a machine IS a member row, so " +
		"resolveMachine is GetMember plus this kind test. Machine-vs-person axis.",
	"api_helpers.go :: observedHost :: m.Kind == machineKind": "" +
		"a warden row IS the machine, so its observed host is its own id rather than " +
		"something the hub reports about it. A projection fact, not a policy.",
	"api_machines.go :: HandleListMachinesApiMachinesGet :: m.Kind != machineKind": "" +
		"the machines collection is the roster FILTERED to kind==warden — this is the " +
		"filter, not a permission test. Already enumerated in authzOutsideRouteTable " +
		"for the authz re-grade; listed here too because the two gates count different " +
		"things and neither should be tuned to hide overlaps.",
	"api_machines.go :: HandleDeleteMachineApiMachinesMemberIdDelete :: m.Kind != machineKind": "" +
		"refuse to delete a non-machine through the machines face: a typo'd member id " +
		"must 404 as a machine rather than soft-delete a colleague. Also in " +
		"authzOutsideRouteTable.",

	// ── self-ops: the outsource fold, one handler face at a time ────────────
	//
	// These five are the SAME decision five times, and that is on purpose rather
	// than a copy-paste smell: each handler folds an ow- caller onto the worker
	// funnel (which takes outsourceMu) instead of the member putMember path,
	// because a member-topic fan-out would leak a worker row onto the staff
	// roster wire. Converging them would mean one funnel that both locks and
	// does not lock, which is a behaviour change.
	"api_members.go :: HandleReportWakingApiSelfWakingPost :: m.Kind == KindOutsource": "" +
		"self-report waking, outsource fold (T-ea82): clear the recycle markers through " +
		"the worker funnel under outsourceMu — a member-path putMember here would race " +
		"the outsource tick's read-modify-write and lose the fold.",
	"api_members.go :: HandleReportStoppingApiSelfStoppingPost :: m.Kind == KindOutsource": "" +
		"same self-report fold, stopping face: workerReportStopping rather than the " +
		"member path, for the same lock and the same fan-out topic reason.",
	"api_members.go :: HandleReportStoppedApiSelfStoppedPost :: m.Kind == KindOutsource": "" +
		"same self-report fold, stopped face — and this one also runs the worker 收口 " +
		"(kill+respawn on the first stopped-report of a refocus-marked worker), which " +
		"is the member recycle-kill shape riding the worker's own kill funnel.",
	"api_members.go :: HandleRestartSelfApiSelfRefocusPost :: m.Kind == KindOutsource": "" +
		"same fold on the self-refocus face: stamp the epoch and open the graceful " +
		"window through the worker funnel, the same shape the owner's refocus button " +
		"takes.",
	"api_members.go :: publishMemberAvatarChanged :: m.Kind == KindOutsource": "" +
		"publish the change on the outsource_worker topic instead of the member topic. " +
		"A WIRE-topic split (workers are owner-only on the wire), not a difference in " +
		"what happens to the row.",

	// ── personal-avatar target scoping (T-c826) ─────────────────────────────
	"api_members.go :: HandlePutMemberAvatarApiMembersMemberIdAvatarPut :: m.Kind == KindWarden": "" +
		"T-c826 owner ruling: wardens are infrastructure, not people with personal " +
		"avatars, so the avatar face 422s for that kind. Classifies the TARGET, not the " +
		"caller — the route table already makes the caller owner-only, which is why it " +
		"cannot be a Requires floor.",
	"api_members.go :: HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete :: m.Kind == KindWarden": "" +
		"the delete half of the same T-c826 target rule, same reasoning.",

	// ── offboard / wind-down ────────────────────────────────────────────────
	"api_members.go :: offboardManualWriteBackFor :: m.Kind != KindOutsource": "" +
		"the offboard 預告's task-manual clause is derived from the worker's LINKED " +
		"TASK, which only an outsource row has (LinkedTaskID). A staff member has no " +
		"such link, so there is nothing to write back — an absence of data, not a " +
		"withheld formality.",
	"member_ownerop_winddown.go :: memberHasStateToFlush :: m.Kind != KindAssistant": "" +
		"staff-only by construction, and the function's own comment says why for both " +
		"excluded kinds: a warden runs no ocagent and would never read the marker, and " +
		"an outsource row has its own funnel (respawnWorkerForOwnerOp) and does not reach " +
		"these handlers because the owner-op verbs leading here each pass staffOnly — a " +
		"per-call-site choice since 2026-08-28, not a property of the resolver. " +
		"🔴 Refused here RATHER THAN relied upon upstream — the " +
		"belt-and-braces is the point, but note the reach: this is an allow-list of ONE " +
		"kind, so a fourth member kind would inherit the exclusion without anyone " +
		"deciding to give it. Compare tokenExpiryOf, which was rewritten away from " +
		"exactly this shape.",
	"receipt_watch.go :: stampReceiptMissing :: m.Kind != KindOutsource": "" +
		"the receipt-missing stamp lands on a member row through putMember; a worker " +
		"row is stamped through the worker funnel below instead. Same formality, two " +
		"storage funnels — the divergence is the WRITE path, not the decision.",
	"reconcile.go :: tokenExpiryOf :: m.Kind == KindWarden": "" +
		"🔴 THE FIXED BUG, kept in the ledger as the worked example. This gate used to " +
		"read `Kind != KindAssistant`, which swept outsource in with warden while the " +
		"comment justified only the warden half — an exemption wider than its own " +
		"reason, and one of the two historical 外包-missing-a-formality failures. It now " +
		"names the ONE exempt kind (a warden's token is minted without an exp claim, so " +
		"asking about its expiry would invent a deadline), which is what stops the next " +
		"kind inheriting an exemption nobody decided to give it.",
	"sse_bands.go :: decideTokenExpirySignal :: member.Kind == KindWarden": "" +
		"the SSE-side twin of tokenExpiryOf's exemption, and it must stay in step with " +
		"it: a warden's token has no exp, so there is no expiry to signal. Named as the " +
		"one exempt kind for the same reason, NOT as an assistant allow-list.",

	// ── reconcile / dispatch: machine-vs-person, not 正職／外包 ──────────────
	"reconcile.go :: reconcileOne :: m.Kind != KindWarden": "" +
		"runtime-capability fail-closed before building a START frame: a warden is not " +
		"spawned with a runtime, so the question does not apply to it. Applies " +
		"identically to staff and outsource — which is the sameness the constitution " +
		"asks for.",
	"reconcile.go :: resolveEmptyRuntimeForPlacement :: m.Kind == KindWarden": "" +
		"same machine-vs-person axis at the runtime-resolution step (T-b3d0): a warden " +
		"has no agent runtime to resolve.",
	"reconcile.go :: wardenTargetOf :: target.Kind == KindWarden": "" +
		"resolving 'which machine executes this' — the addressed row must actually BE a " +
		"machine. Returning \"\" makes 'nowhere to send this' a nameable answer instead " +
		"of an ordinary-looking unreachable warden.",
	"reconcile.go :: wardenTargetOf :: cand.Kind == KindWarden": "" +
		"the fallback arm of the same resolution: the observed host id must resolve to " +
		"an ACTIVE machine row before it is used as a dispatch target.",
	"reconcile.go :: consumeUninstallIntentOnOffline :: m.Kind != KindWarden": "" +
		"the uninstall-intent sweep is about machines being uninstalled; only a warden " +
		"row can carry that desired_state. This is the loop whose in-body kind test " +
		"stage 3 hoisted into the shared list as an AppliesTo — the guard here is what " +
		"that AppliesTo mirrors, and the two must agree.",
	"reconcile.go :: consumeUninstallOnDisconnect :: m.Kind != KindWarden": "" +
		"the event-driven edge of the same uninstall sweep, same machine-only reason.",
	"reconcile.go :: dispatchIdentitySweepNow :: m.Kind != KindWarden": "" +
		"the identity sweep is dispatched TO machines (they are the executors that hold " +
		"the identity), so the loop selects active warden rows. Machine-vs-person.",
	"reconcile.go :: identitySweepOnConnect :: m.Kind == KindWarden": "" +
		"the other side of the same sweep: a warden connecting is not itself a subject " +
		"of the identity check it performs on others.",
	"reconcile.go :: connectionIsTheGenuineArticle :: m.Kind == KindOutsource": "" +
		"🔴 A REAL 正職／外包 DIVERGENCE, and a narrow one: the expected machine for a " +
		"staff member is its durable DesiredMachineID, while a worker with no pin falls " +
		"back to the observed spawn target (workerSpawnObs) because a worker is placed " +
		"by the scheduler rather than pinned by the owner. Same question, two places the " +
		"answer is stored. Converging it means giving workers a durable pin, which is a " +
		"behaviour change with an owner-visible face.",

	// ── worker spawn placement ──────────────────────────────────────────────
	"worker_spawn.go :: resolveWorkerPlacement :: m.Kind != KindWarden": "" +
		"the requested placement target must be an ACTIVE machine — refusing a " +
		"non-machine here is what turns 'you asked for a box that is not a box' into a " +
		"named unavailability instead of a silent fallback.",
	"worker_spawn.go :: reclaimWorkerSession :: m.Kind == KindWarden": "" +
		"fan the reclaim to every online machine when no specific spawn target is " +
		"remembered. Selecting machines to talk to, not classifying the worker.",

	// ── SSE / presence ──────────────────────────────────────────────────────
	"api_infra.go :: HandleEventsApiEventsGet :: m.Kind == KindWarden": "" +
		"warden-command eligibility (spec/sse.md §7): a connection drains the command " +
		"FIFO iff its token sub resolves to a machine row — the unforgeable addressing " +
		"key. Also enumerated in authzOutsideRouteTable.",
	"api_infra.go :: sseStopGateRefusal :: m.Kind == KindOutsource": "" +
		"🔴 A REAL DIVERGENCE, declared: a RELEASED worker's session deliberately lives " +
		"on for its §6.3 close-out duties (the reclaim grace), so its SSE must stay " +
		"admitted even though the row is roster-removed — the member stop gate below " +
		"would refuse it. The asymmetry is a consequence of workers being released with " +
		"their task while members are dismissed by hand; it is the retirement half the " +
		"stage 3 comment records as NOT yet wired into LifecyclePolicy.",
	"api_infra.go :: sseStopGateRefusal :: m.Kind != KindWarden": "" +
		"the T-a9d6 offboard-in-progress exemption: a member working its offboard " +
		"sequence keeps its SSE. Excluded for wardens because a warden has no offboard " +
		"sequence to work — machine-vs-person.",
	"api_infra.go :: bankLiveCost :: m.Kind != KindOutsource": "" +
		"cost banking fans on the member topic for staff and on the outsource_worker " +
		"topic for workers (pre-fold wire parity, owner-only visibility). The AMOUNT " +
		"banked is identical; only the topic differs.",

	// ── chat / roster projection ────────────────────────────────────────────
	"api_chat.go :: resolveChatRecipient :: m.Kind != KindAssistant": "" +
		"a chat recipient must be a person — staff or worker, explicitly BOTH. Written " +
		"as a two-arm test rather than `!= KindWarden` so that a fourth kind does not " +
		"silently become addressable; the pairing is what makes 外包＝正職 true here.",
	"api_chat.go :: resolveChatRecipient :: m.Kind != KindOutsource": "" +
		"the other arm of that same both-kinds-allowed test; see above.",
	"api_chat.go :: resumeFloorParts :: m.Kind == machineKind": "" +
		"the resume/floor projection puts warden rows in the MACHINE block, never in " +
		"the roster — a warden row IS a machine, not a colleague.",
	"api_chat.go :: resumeFloorParts :: m.Kind == KindOutsource": "" +
		"the same projection renders workers as a separate contractors block carrying " +
		"their current task and progress. Presentation of a fact only workers have (a " +
		"linked task), not a difference in treatment.",

	// ── monitoring ──────────────────────────────────────────────────────────
	"api_monitoring.go :: HandleGetMonitoringApiMonitoringGet :: m.Kind == machineKind": "" +
		"monitoring splits member rows from machine rows for rendering; the host list " +
		"is the active machine rows. Also enumerated in authzOutsideRouteTable.",
	"api_monitoring.go :: foldCommandResult :: m.Kind == KindOutsource": "" +
		"P5b convergence: a worker start/stop now rides the MEMBER verbs, so its receipt " +
		"arrives keyed by the ow- id and has to be routed to the worker fold. The " +
		"convergence is the point — this line is the seam where one receipt path serves " +
		"two storage funnels, not two receipt paths.",
	"api_monitoring.go :: stampReportedLaunchFacts :: m.Kind == KindOutsource": "" +
		"same routing, launch-facts face: re-read and write under outsourceMu so the " +
		"tick's concurrent write is not clobbered. Lock discipline, not policy.",

	// ── task executor kind: the 正職／外包 axis on the TASK side ────────────
	//
	// TaskExecutorMember / TaskExecutorOutsource are a different field from
	// Member.Kind and answer a different question ("who is this task FOR"),
	// but they are the same axis and the same ticket's subject, so they are
	// scanned and listed rather than declared out of scope.
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: executorKind == TaskExecutorOutsource": "" +
		"the create matrix's target half (T-23cf): whether this task is 發包. TWO sites " +
		"(pinned at 2) and the reason covers both — it is the `isOutsource` argument " +
		"to authorizeTaskCreate, and the guard on inheriting the dispatch spec " +
		"(runtime/model/effort/machine), which only a 發包 task has.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: executorKind == TaskExecutorMember": "" +
		"the other arm: a member-executed task needs an explicit executor id, an " +
		"outsource one is minted by the scheduler. A required-fields difference that " +
		"follows from workers not existing until they are minted.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: kind == TaskExecutorOutsource": "" +
		"the normalisation feeding the two arms above — the TASK MANUAL's stored " +
		"assignee (manualAssignee) folded into the canonical executorKind. Not the " +
		"request's target.kind, which is the separate " +
		"`trimString(body.Target.Kind)` entry below.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: kind == TaskExecutorMember": "" +
		"the other half of that same normalisation.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: trimString(body.Target.Kind) == TaskExecutorOutsource": "" +
		"T-23cf create matrix: the TARGET's executor kind as a request field, paired " +
		"with the caller class — the rule is about the PAIR, which no single route " +
		"Requires can state. Also in authzOutsideRouteTable.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: t.ExecutorKind == TaskExecutorOutsource": "" +
		"the event-driven scheduler seam (contract §B.4): an unassigned outsource task " +
		"just landed, so tick the scheduler NOW rather than up to a cadence period " +
		"later. A latency optimisation on the 發包 path, which is the only path with a " +
		"scheduler behind it.",
	"api_tasks.go :: HandleListTasksApiTasksGet :: t.ExecutorKind != TaskExecutorOutsource": "" +
		"the `executor=outsource` and `executor=unassigned` list filters — TWO sites " +
		"(pinned at 2), the second adding `ExecutorID == \"\"` on top. A query facet " +
		"the caller asked for, not a policy.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: kind == TaskExecutorOutsource": "" +
		"TWO sites, pinned at 2 in identityGateExpectedCount. (a) the 發包 authz " +
		"choke: a reassign toward outsource lands the task UNASSIGNED and the " +
		"scheduler mints the successor under the global parallel cap (T-35e0, no " +
		"inline mint), so an unauthorised initiator must be refused BEFORE the " +
		"handover side effects run; (b) the event-driven scheduler tick after the " +
		"write, so the successor is minted now rather than up to a cadence period " +
		"later — the twin of create_task's seam. Both exist only on the 發包 arm " +
		"because a member reassign has its executor in hand already.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: kind == TaskExecutorMember": "" +
		"the executor RE-POINT write, member arm: bind ExecutorID to the member just " +
		"resolved and RESET the row's outsource dispatch columns to their unset shape " +
		"(runtime back to the default, model/effort/machine emptied, dispatched false — " +
		"the runtime column is SET, not cleared). Its `else` is the " +
		"發包 arm, which lands the task UNASSIGNED for the scheduler to mint under the " +
		"parallel cap (T-35e0). The kinds differ here because a member executor is " +
		"already in hand at write time and an outsource one does not exist yet — not " +
		"because staff and 外包 get different treatment. (This entry has been wrong " +
		"once: it used to say 'the other arm of that same normalisation', copied from " +
		"the neighbouring entry, and there is no kind normalisation in this handler " +
		"at all — `kind` is a plain trimString of the request field.)",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: m.Kind == KindOutsource": "" +
		"P7d fold parity: an outsource ROW is never a 'member'-kind reassign target — " +
		"outsource executors are minted fresh by the outsource arm. Refusing here is " +
		"what keeps the two arms from both claiming the same worker. Also in " +
		"authzOutsideRouteTable.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: m.Kind == KindWarden": "" +
		"a warden is never a task executor — machine-vs-person. Also in " +
		"authzOutsideRouteTable.",
	"api_tasks.go :: HandleReassignTaskApiTasksTaskIdReassignPost :: t.ExecutorKind == TaskExecutorMember": "" +
		"the already-the-executor conflict check, scoped to the member arm because the " +
		"outsource arm has no stable executor id to compare against before the mint.",
	"api_tasks.go :: HandleClaimTaskApiTasksTaskIdClaimPost :: t.ReassignedFromKind == TaskExecutorOutsource": "" +
		"on takeover, the predecessor to release is an outsource worker only — a MEMBER " +
		"predecessor is never released here because it lives on its own member " +
		"lifecycle. This IS the 「外包跟著 task 生滅」half of the constitution, at the " +
		"one place it is actually spelled.",
	"api_tasks.go :: isOutsource :: isOutsourceMember(c.member)": "" +
		"T-23cf 正職授權矩陣 helper over the already-resolved caller — it asks the seam " +
		"rather than re-typing the comparison, which is the shape every other caller " +
		"should copy. Also in authzOutsideRouteTable.",
	"api_tasks_handoff.go :: releaseDependentsOnClose :: d.ExecutorKind == TaskExecutorOutsource": "" +
		"a dependent task unblocked by this close needs the scheduler ticked only if it " +
		"is an unassigned 發包 task; a member-executed dependent has an executor already.",
	"api_taskmanuals.go :: resolveManualAssigneeMachine :: kind != TaskExecutorOutsource": "" +
		"a manual's machine pin only means anything for an outsource assignee — a member " +
		"assignee carries its own DesiredMachineID.",
	"outsource_sched.go :: outsourceAwaitingAssignment :: t.ExecutorKind != TaskExecutorOutsource": "" +
		"the scheduler's candidate filter: only a 發包 task with no executor yet is " +
		"awaiting a mint. This is the definition of the queue, not a difference in " +
		"treatment.",
	"outsource_sched.go :: outsourceSpecOf :: kind != TaskExecutorOutsource": "" +
		"a task manual's assignee spec (runtime/model/effort/copies) is only read for an " +
		"outsource assignee; the fields have no member equivalent.",
	"outsource_sched.go :: runOutsourceTick :: t.ReassignedFromKind != TaskExecutorOutsource": "" +
		"the T-ba04 handover-timeout reaper: only an outsource predecessor leaks a " +
		"session when a takeover never lands. The same 「外包跟著 task 生滅」rule as the " +
		"claim handler above.",

	// ── minting a population (struct-literal shape) ─────────────────────────
	"dal_tasks.go :: memberFromWorker :: Kind: KindOutsource": "" +
		"THE projection that makes 外包＝正職 mechanically true: a worker row rendered as " +
		"a member row so the shared passes can run on it. If this stamp were wrong, " +
		"every gate above would classify workers as staff — it is the single point the " +
		"whole fold rests on.",
	"api_machines.go :: HandleOnboardMachineApiMachinesPost :: Kind: machineKind": "" +
		"onboarding a machine mints its warden member row. The one place a warden row " +
		"is created outside the seed.",
	"api_roles.go :: HandleCreateRoleApiRolesPost :: Kind: KindAssistant": "" +
		"role creation names the kind its members will be hired as; roles are a staff " +
		"concept (a worker's shape comes from its task manual instead).",
	"dbseed.go :: seedOutOfBox :: Kind: KindAssistant": "" +
		"the out-of-box seed's first colleague. Fixture data, listed because the scan " +
		"must not be tuned to skip the places rows are born.",
	"dbseed.go :: seedOutOfBox :: Kind: KindWarden": "" +
		"the out-of-box seed's local machine row; same reasoning as above.",
	"api_tasks.go :: HandleCreateTaskApiTasksPost :: ExecutorKind: executorKind": "" +
		"stamping the normalised executor kind onto the new task — the value the four " +
		"create-matrix comparisons above decided.",
	"wire.go :: newTaskDTO :: ExecutorKind: t.ExecutorKind": "" +
		"wire projection, straight copy: the DTO tells the client which population " +
		"executes the task. No decision here.",
	"wire.go :: newTaskDTO :: ReassignedFromKind: t.ReassignedFromKind": "" +
		"same wire projection for the predecessor's kind; no decision here.",
	"wire.go :: newTaskListItemDTO :: ExecutorKind: t.ExecutorKind": "" +
		"the list-item twin of newTaskDTO's copy; no decision here.",
	"wire.go :: newTaskListItemDTO :: ReassignedFromKind: t.ReassignedFromKind": "" +
		"the list-item twin of the predecessor-kind copy; no decision here.",
	"api_tasks.go :: resumeTasksFor :: ReassignedFromKind: t.ReassignedFromKind": "" +
		"T-91: the THIRD twin of the same straight copy — the wake snapshot's task " +
		"row, which was the one projection that carried neither the lock nor the " +
		"predecessor. No decision here either: it says how to RESOLVE the id in " +
		"reassigned_from (roster row vs outsource worker), which is a lookup " +
		"instruction, not a difference in treatment. 🔴 The reverse is what would " +
		"be the gate: omitting the kind would leave an agent unable to look its " +
		"predecessor up, and 「轉交給外包跟轉交給一個目前離線的正職應該要有同樣一套 " +
		"方法」 (owner) is exactly what this field makes possible with one code path.",
	"api_helpers.go :: newMemberDTO :: Kind: m.Kind": "" +
		"the member DTO carries the row's kind to the client verbatim. The cockpit " +
		"renders 正職 and 外包 differently BECAUSE of this field, which is the one place " +
		"the population is supposed to become visible — a presentation split, " +
		"downstream of every decision.",
	"api_helpers.go :: newMemberLightDTO :: Kind: m.Kind": "" +
		"the light DTO's copy of the same field; same reasoning as newMemberDTO.",
	"api_chat.go :: resumeFloorParts :: Kind: m.Kind": "" +
		"the resume/floor projection carries the kind so the client can place the row " +
		"in the right block. Pure copy, downstream of the two comparisons in the same " +
		"function that are listed above.",

	// ── NOT identity gates at all: the overloaded `Kind:` field on the
	// struct-literal shape. These reached the ledger because the value is a
	// `.Kind` read whose spelling this scanner cannot distinguish from a member
	// kind without type information. Listed rather than filtered, for the same
	// reason as the comparison-shape false positives below.
	"api_bootdocs.go :: bootDocSpecFor :: Kind: reg.Kind": "" +
		"NOT an identity gate — DOCUMENT kind (boot sequence / offboard / task " +
		"closeout…), an unrelated vocabulary reusing the field name.",
	"api_bootdocs.go :: foldBootDocDTO :: Kind: spec.Kind": "" +
		"NOT an identity gate — the same document-kind vocabulary, at the DTO fold.",
	"api_bootdocs.go :: replaceBootDoc :: Kind: spec.Kind": "" +
		"NOT an identity gate — the same document-kind vocabulary, at the write path.",
	"api_bootdocs.go :: resetBootDoc :: Kind: spec.Kind": "" +
		"NOT an identity gate — the same document-kind vocabulary, at the reset path.",
	"api_replycards.go :: replyCardListItemOf :: Kind: c.Kind": "" +
		"NOT an identity gate — REPLY-CARD kind (the question's shape), another " +
		"vocabulary sharing the field name.",
	"wire.go :: newReplyCardDTO :: Kind: c.Kind": "" +
		"NOT an identity gate — the wire twin of the reply-card kind copy above.",
	"wire.go :: newTaskArtifactDTO :: Kind: a.Kind": "" +
		"NOT an identity gate — ARTIFACT kind (file / image / link), copied to the DTO.",
	"api_lore_governance.go :: writeLoreGovernanceReceipt :: Kind: event.Kind": "" +
		"NOT an identity gate — LORE GOVERNANCE kind (`retire` / `revive`), the journal " +
		"row's own vocabulary, copied to the receipt. Nothing here reads a member.",
	"api_lore_entity.go :: writeLoreEntityReceipt :: Kind: event.Kind": "" +
		"NOT an identity gate — the SAME lore governance vocabulary one file over " +
		"(`entity-approve` / `entity-merge`), copied to the subject-review receipt.",

	// ── NOT identity gates at all ──────────────────────────────────────────
	//
	// The comparison shape keys on the FIELD NAME `Kind`, which several unrelated
	// vocabularies in this package also use. These are false positives and they
	// are kept on the ledger rather than special-cased in the scanner, because a
	// scanner tuned to hide its own near-misses stops finding the real ones —
	// the same ruling api_roles.go :: HandleDeleteRole got in authzOutsideRouteTable.
	"api_bootdocs.go :: bootDocRegFor :: reg.Kind == kind": "" +
		"NOT an identity gate — `Kind` here is a DOCUMENT kind (boot sequence, " +
		"offboard, task closeout…), an unrelated vocabulary that happens to reuse the " +
		"field name. Kept listed rather than filtered out of the scan.",
	"api_tasks.go :: taskArtifactDTOs :: a.Kind != ArtifactKindLink": "" +
		"NOT an identity gate — ARTIFACT kind (file / image / link). Same overloaded " +
		"field name, same reason for keeping it visible.",
	"wire.go :: newTaskArtifactDTO :: a.Kind != ArtifactKindLink": "" +
		"NOT an identity gate — the wire twin of the artifact-kind test above.",
	"api_tasks_handoff.go :: applyHandoffPlan :: plan.Kind switch case HandoffFollowUp": "" +
		"NOT an identity gate — HANDOFF-PLAN kind (none / follow-up / return to " +
		"creator). Caught by the switch shape, which exists for the member-kind switch " +
		"nobody has written yet; kept listed so the shape's reach stays demonstrable.",
	"api_tasks_handoff.go :: applyHandoffPlan :: plan.Kind switch case HandoffReturnToCreator": "" +
		"NOT an identity gate — the other arm of the same handoff-plan switch.",
	"api_members.go :: HandleHireMemberApiMembersPost :: trimmedOrEmpty(body.Kind) != \"\"": "" +
		"the `privileged` predicate of the §4 hire 閉環: hiring WITH a kind is " +
		"privilege-bearing (otherwise an agent hires itself an 'assistant' and walks up " +
		"the ladder), so it is admin-gated inside the handler. It tests whether a kind " +
		"was SUPPLIED, not which one. Also in authzOutsideRouteTable.",
}

func TestIdentityGatesAreEachOnTheRecord(t *testing.T) {
	sites, counts := scanIdentityGates(t)

	// ── anti-vacuity: prove the corpus BEFORE judging it. Every loop below
	// ranges over `sites`; if the scanner stops matching (a renamed constant, a
	// changed AST shape, a bad cwd) each loop would pass over nothing and this
	// gate would go green while guarding exactly zero. That failure has shipped
	// in this repo before.
	if len(sites) < identityGateFloor {
		t.Fatalf("the scan found only %d identity gates (floor %d) — the SCANNER is "+
			"broken, not the code. A green run here would mean nothing. Fix the scan, "+
			"or lower the floor deliberately in a commit that says why.",
			len(sites), identityGateFloor)
	}
	if counts.files < identityFileFloor || counts.decls < identityDeclFloor {
		t.Fatalf("scan corpus too small: %d files / %d declarations (floors %d / %d) — "+
			"same reasoning as above", counts.files, counts.decls,
			identityFileFloor, identityDeclFloor)
	}
	if len(identityGateLedger) == 0 {
		t.Fatalf("the ledger is empty — it is the artifact this gate exists to keep")
	}
	// Logged rather than hard-coded in prose, so every count quoted in a comment
	// can be re-checked with `go test -v -run TestIdentityGatesAreEachOnTheRecord`
	// instead of trusted.
	t.Logf("identity scan: %d gates / %d files / %d declarations (ledger holds %d); "+
		"`Kind:` struct literals seen %d, kept %d (the rest carry a non-member "+
		"vocabulary — see the reach note in the file header)",
		len(sites), counts.files, counts.decls, len(identityGateLedger),
		counts.kindLiteralsSeen, counts.kindLiteralsKept)

	found := make(map[string]bool, len(sites))
	var unlisted []string
	for _, s := range sites {
		found[s.key()] = true
		if _, listed := identityGateLedger[s.key()]; !listed {
			unlisted = append(unlisted, "["+s.shape+"] "+s.key())
		}
	}
	if len(unlisted) > 0 {
		t.Errorf("identity gate(s) with no stated reason:\n  %s\n\n"+
			"Migration 00025's constitution is 外包＝正職, and the owner ruled on "+
			"2026-08-26 that 任何正職外包的差異化處理都需要重新檢視. A new gate is a "+
			"conversation, not a silent addition.\n"+
			"Do ONE of:\n"+
			"  (a) delete the difference — preferred. Ask whether the two arms could "+
			"be one arm; 剩餘部分的一樣是程式碼階層的一樣, not two copies behaving "+
			"the same;\n"+
			"  (b) if it is a pre-decide lifecycle formality, express it as an "+
			"AppliesTo on lifecycleRosterPasses (lifecycle_roster.go) so BOTH "+
			"producers get it by construction and the parity test reads the "+
			"restriction back by name;\n"+
			"  (c) if it genuinely belongs where it is, add it to identityGateLedger "+
			"with the reason and the ruling it came from.\n"+
			"Do NOT delete it from the scan.", strings.Join(unlisted, "\n  "))
	}

	// ── the SECOND decision wearing the first one's name ────────────────────
	//
	// A ledger key is FILE :: FUNCTION :: EXPRESSION, so two textually identical
	// gates in one function collapse onto one entry — and the reason already
	// written there would then be silently claiming to cover a decision nobody
	// read. Measured, not imagined: narrowing stale_stopping_clear to staff adds
	// a second `m.Kind != KindOutsource` to lifecycleRosterPasses, whose key is
	// the one recycle_loop_break already owns, and the first version of this gate
	// went green on it.
	var doubled []string
	for key, n := range counts.occurrences {
		want, pinned := identityGateExpectedCount[key]
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
		t.Errorf("an identity gate key matches a different number of sites than the "+
			"ledger accounts for:\n  %s\n"+
			"Each entry in identityGateLedger carries ONE reason. If the same "+
			"expression now appears twice in the same function, the second one is a "+
			"decision nobody wrote a reason for, and it is hiding behind the first "+
			"one's entry. Either give the new one a distinguishable form (which is "+
			"usually a sign it should be a named helper or an AppliesTo on "+
			"lifecycleRosterPasses), or pin the count in identityGateExpectedCount "+
			"and extend the reason to cover BOTH.", strings.Join(doubled, "\n  "))
	}
	for key := range identityGateExpectedCount {
		if _, ok := counts.occurrences[key]; !ok {
			t.Errorf("identityGateExpectedCount pins %q, which the scan does not match "+
				"at all — a pin on nothing is dead weight that reads as considered.", key)
		}
	}

	// A STALE entry is a finding too: without this the ledger could be padded
	// with guesses until nothing is ever unlisted, and the list would SILENCE
	// the gate instead of documenting it.
	var stale []string
	for key := range identityGateLedger {
		if !found[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("identityGateLedger lists gate(s) that no longer exist:\n  %s\n"+
			"Either the code moved (re-key the entry) or the difference was removed "+
			"(drop it — and say so, deleting a 正職／外包 divergence is the good "+
			"outcome). A stale key also hides the fact that the live gate it used to "+
			"name is now unlisted.", strings.Join(stale, "\n  "))
	}
}

func TestIdentityGateReasonsAreRealReasons(t *testing.T) {
	if len(identityGateLedger) == 0 {
		t.Fatalf("empty ledger — nothing to judge")
	}
	for key, reason := range identityGateLedger {
		if len(strings.TrimSpace(reason)) < 40 {
			t.Errorf("%s: the reason is %d chars. The entry exists to tell the next "+
				"person re-reviewing 正職／外包 differences why this one is legitimate; "+
				"'legacy' or 'ok' does not do that. ⚠️ This checks LENGTH, not honesty "+
				"— a fluent false sentence passes, which is why the diff is the real "+
				"review.", key, len(strings.TrimSpace(reason)))
		}
	}
}

// ── the teeth on the vocabulary ─────────────────────────────────────────────

// TestIdentityKindVocabularyIsComplete is the tooth that makes a silently
// shrinking scan loud.
//
// identityKindIdents and identitySeamFuncs are lists of NAMES compared as
// strings. A name that matches nothing does not fail — it just narrows the scan,
// and gate (1)'s corpus floors cannot notice, because the remaining names alone
// clear them. That is not hypothetical: the sibling gate in
// authz_surface_gate_test.go shipped with "outsourceSpawnRequest", a type that
// never existed, and the 發包 authorization choke sat outside that inventory from
// day one because of it.
//
// This runs BOTH directions:
//   - every name listed must exist in the package (the typo direction);
//   - every member-kind constant the package DECLARES must be listed (the
//     new-kind direction — adding a fourth member kind must not be able to slip
//     past this scan).
func TestIdentityKindVocabularyIsComplete(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — this check would be vacuous", err, len(paths))
	}
	fset := token.NewFileSet()

	declaredConsts := map[string]string{} // name -> literal value
	declaredFuncs := map[string]bool{}
	identUses := map[string]int{}
	callUses := map[string]int{}
	parsed := 0

	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if _, skip := identityScanSkip[base]; skip {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		parsed++
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declaredFuncs[d.Name.Name] = true
				}
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				for _, sp := range d.Specs {
					vs, ok := sp.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						lit := ""
						if i < len(vs.Values) {
							if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
								lit = strings.Trim(bl.Value, `"`)
							}
						}
						declaredConsts[name.Name] = lit
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				identUses[v.Name]++
			case *ast.CallExpr:
				if id, ok := v.Fun.(*ast.Ident); ok {
					callUses[id.Name]++
				}
			}
			return true
		})
	}
	if parsed == 0 || len(declaredConsts) == 0 {
		t.Fatalf("parsed %d files / %d consts — this check would be vacuous",
			parsed, len(declaredConsts))
	}

	// direction 1 — a listed name that matches nothing.
	for name := range identityKindIdents {
		if _, ok := declaredConsts[name]; !ok {
			t.Errorf("identityKindIdents lists %q, which this package declares nowhere. "+
				"A name matching nothing does not fail loudly — it silently shrinks the "+
				"identity scan while the corpus floors stay green. Fix the spelling, or "+
				"drop it and then go check what stopped being scanned.", name)
		}
	}
	for name := range identitySeamFuncs {
		if !declaredFuncs[name] {
			t.Errorf("identitySeamFuncs lists %q, which is not a function declared in "+
				"this package. Same silent-narrowing problem as above.", name)
		}
	}
	for name := range identityKindFields {
		if identUses[name] == 0 {
			t.Errorf("identityKindFields lists the field name %q, which appears nowhere "+
				"in the package's production sources. Same silent-narrowing problem.", name)
		}
	}

	// direction 2 — a member kind the package declares and this scan does not
	// know about. The member kind closed set is domain.go's `Kind*` constants;
	// the task-executor pair is `TaskExecutor*`. Both are derived here rather
	// than re-typed, so a NEW one enters the scan by being declared.
	//
	// ⚠️ `Kind*` is a prefix several unrelated vocabularies do NOT use (they
	// spell theirs docKind* / ArtifactKind* / Handoff*), which is what makes the
	// prefix usable as a derivation. If that ever stops being true, this check
	// starts reporting unrelated constants — which is the loud failure, not the
	// silent one, and is the right way round.
	var missing []string
	for name := range declaredConsts {
		isMemberKind := strings.HasPrefix(name, "Kind")
		isExecutorKind := strings.HasPrefix(name, "TaskExecutor")
		if !isMemberKind && !isExecutorKind {
			continue
		}
		if !identityKindIdents[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the package declares population constant(s) the identity scan does "+
			"not know about:\n  %s\n"+
			"Add them to identityKindIdents. A member kind (or task-executor kind) "+
			"outside that map is a whole population whose gates this file cannot see, "+
			"and the corpus floors will not notice — which is precisely how the two "+
			"historical 外包-missing-a-formality failures stayed invisible.",
			strings.Join(missing, "\n  "))
	}

	t.Logf("vocabulary check over %d production files: %d consts declared, "+
		"%d kind idents / %d seam funcs / %d kind fields listed",
		parsed, len(declaredConsts), len(identityKindIdents),
		len(identitySeamFuncs), len(identityKindFields))
}

// TestIdentityScanExclusionsEachNameALiveFile is the tooth on the EXCLUSION
// list, and it is the thing that makes "scan everything, name the exceptions"
// safe to rely on.
//
// An exclusion entry naming a file that does not exist is invisible: nothing
// fails, the corpus is unchanged, and the list merely reads as if more thought
// went into it than did. Worse, when a file is RENAMED the entry keeps quietly
// excluding the old name while the new file joins the corpus — or, if the rename
// went the other way, a real file drops out of the scan and nothing says so.
func TestIdentityScanExclusionsEachNameALiveFile(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — this check would be vacuous", err, len(paths))
	}
	live := map[string]bool{}
	for _, p := range paths {
		live[filepath.Base(p)] = true
	}
	if len(identityScanSkip) == 0 {
		t.Logf("no files are excluded from the identity scan — nothing to check here, " +
			"and that is the ideal state for an exclusion list")
		return
	}
	for name, why := range identityScanSkip {
		if !live[name] {
			t.Errorf("identityScanSkip excludes %q, which does not exist in this "+
				"package. An exclusion that matches nothing excludes nothing and just "+
				"makes the list look considered; if the file was RENAMED, the entry is "+
				"now excluding a ghost while the real file either rejoined the corpus "+
				"silently or fell out of it silently. Re-key it or drop it.", name)
		}
		if len(strings.TrimSpace(why)) < 40 {
			t.Errorf("identityScanSkip[%q]: the reason is %d chars. Excluding a file "+
				"removes it from this guard's corpus permanently — that is the one "+
				"decision here nobody can see in a later diff, so it has to be argued "+
				"at the point it is made.", name, len(strings.TrimSpace(why)))
		}
		if strings.HasSuffix(name, "_test.go") {
			t.Errorf("identityScanSkip[%q]: _test.go files are already skipped "+
				"structurally; an entry here is dead and suggests somebody expected "+
				"tests to be in the corpus.", name)
		}
	}
}

// ── gate (2): the tick producers hold no undeclared roster loop ─────────────
//
// 🔴 THIS IS THE GATE THAT CLOSES LIFECYCLE-LIST-IS-OPT-IN-T170E, and gate (1)
// cannot do its job.
//
// The historical failures — a worker with no token-expiry lead, a worker with no
// survived-stop sweep — contained NO kind expression at all. The staff-only-ness
// came from runReconcileTick's roster read being ListMembers, which is
// `WHERE kind != 'outsource'` by construction. So the next instance of that
// mistake is a plain `for … range members { … }` written inline in one of the
// two producers, with nothing in it for a kind-scanner to find.
//
// What such a mistake CANNOT avoid is being a loop inside one of those two
// functions. So this gate enumerates them and requires each to be accounted for.
// The count is part of the ruling: `for _, w := range workers` appears twice in
// runOutsourceTick for two different reasons, and a third one appearing is
// exactly the event this gate exists to announce.
type producerLoopRuling struct {
	Count int
	Why   string
}

// lifecycleTickProducers are the functions that turn a roster snapshot into
// decisions. They are named as strings, which is a stale-name risk — so the gate
// FATALS if any name resolves to no function declaration, rather than
// quietly scanning one producer or none.
//
// ⚠️ A NAME LIST IS A CLAIM ABOUT A SET, so the set is built rather than
// asserted. "these are the only producers" is exactly the shape of sentence this
// ticket has been burned by, so TestLifecycleTickProducerSetIsDerived enumerates
// every `*Tick` method on apiServer and requires each one to be either a
// producer here or an entry in lifecycleNonRosterTicks with a reason. A new tick
// cannot be added to this package without that check saying something.
//
// 🔴 T-14 item 5 added the THIRD name, and it is deliberately not the shape the
// warning below is about. runLifecycleTick is the merged cadence entry — it
// replaced two 30s goroutines with one ordered tick — and its whole body is two
// flag-gated calls to the halves already on this list. It reads no rows and
// iterates nothing, so it brings no loop rulings with it. It is listed as a
// producer rather than excused in lifecycleNonRosterTicks for one reason: being
// on this list is what makes TestTickProducersHaveNoUndeclaredRosterLoop watch
// it, so a roster loop written into the merged entry — the newest, most obvious
// place to put one — has to be declared like any other. Excusing it as a
// non-roster tick would be true today and unwatched tomorrow.
var lifecycleTickProducers = []string{
	"runLifecycleTick", "runReconcileTick", "runOutsourceTick",
}

// lifecycleNonRosterTicks are the OTHER cadence/loop ticks in this package —
// the ones that are not pre-decide roster producers — each with the reason it
// is not. Read this before adding a name: if your new tick walks the member
// roster and does something to rows, it does NOT belong here, and it very
// likely does not belong as a third producer either. It belongs as an
// AppliesTo-carrying pass on lifecycleRosterPasses, which is the whole point of
// stages 1–3.
var lifecycleNonRosterTicks = map[string]string{
	"runScheduledMessageTick": "" +
		"iterates SCHEDULED MESSAGE rows (ListAllEnabledScheduledMessages), not the " +
		"member roster. It resolves a member only to deliver to it; no member row is " +
		"read, stamped or filtered by kind anywhere in the pass.",
	"handoverNoticeTick": "" +
		"ONE member, not a roster: it is a single quiet tick of the context-high SSE " +
		"band for the connection it is called on. It does read that member's row (the " +
		"wind-down-stage guard), so it IS a wind-down formality — but it has no " +
		"iteration to enumerate, and it reaches whoever is connected regardless of " +
		"kind, so the roster-loop gate has nothing to say about it. Its 外包 reach is " +
		"a question for the SSE path, not for this file.",
	"autoUpdateTick": "" +
		"the station's self-update cadence — binaries and versions, no member rows " +
		"involved at all.",
	"backupTick": "" +
		"the SQLite backup cadence; a free function over the *sql.DB, not a method on " +
		"apiServer, and it touches no domain rows.",
}

// TestLifecycleTickProducerSetIsDerived builds the denominator instead of
// trusting it.
//
// 🔴 THE REASON THIS TEST EXISTS is that the sentence "the two tick producers"
// is a universal claim, and this ticket's record is that such claims have been
// wrong five times in a row when nobody built the set first. So: enumerate every
// function in this package whose name ends in "Tick", and require each to be
// classified. A third roster producer added later fails here by name, in the
// same commit that adds it.
func TestLifecycleTickProducerSetIsDerived(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — this check would be vacuous", err, len(paths))
	}
	fset := token.NewFileSet()
	ticks := map[string]bool{}
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if _, skip := identityScanSkip[base]; skip {
			continue
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", p, perr)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasSuffix(fd.Name.Name, "Tick") {
				continue
			}
			ticks[fd.Name.Name] = true
		}
	}
	// Anti-vacuity: the derivation has to find the producers it is checking, or
	// the classification below judges an empty set and passes.
	if len(ticks) < len(lifecycleTickProducers) {
		t.Fatalf("derived only %d `*Tick` function(s) — fewer than the %d producers "+
			"this file already names, so the derivation is broken and every "+
			"classification below would be vacuous", len(ticks), len(lifecycleTickProducers))
	}
	producer := map[string]bool{}
	for _, name := range lifecycleTickProducers {
		producer[name] = true
	}
	var unclassified []string
	for name := range ticks {
		if producer[name] {
			continue
		}
		why, ok := lifecycleNonRosterTicks[name]
		if !ok {
			unclassified = append(unclassified, name)
			continue
		}
		if len(strings.TrimSpace(why)) < 40 {
			t.Errorf("lifecycleNonRosterTicks[%q]: the reason is %d chars. Saying a tick "+
				"is not a roster producer is a claim about what it iterates; it has to "+
				"name what it iterates instead.", name, len(strings.TrimSpace(why)))
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("cadence tick(s) neither declared a lifecycle producer nor excused as "+
			"a non-roster tick:\n  %s\n"+
			"If it walks the member roster and does anything to rows, it is a "+
			"pre-decide formality: put it on lifecycleRosterPasses "+
			"(lifecycle_roster.go) with an AppliesTo so BOTH 正職 and 外包 get it by "+
			"construction. If it genuinely iterates something else, say what, in "+
			"lifecycleNonRosterTicks. 🔴 Do not add it to lifecycleTickProducers "+
			"without reading stages 1–3 first: a third producer is another place a "+
			"formality can be given to one population and not the other, which is the "+
			"defect this whole ticket exists to remove.", strings.Join(unclassified, "\n  "))
	}
	var ghosts []string
	for name := range lifecycleNonRosterTicks {
		if !ticks[name] {
			ghosts = append(ghosts, name)
		}
	}
	sort.Strings(ghosts)
	if len(ghosts) > 0 {
		t.Errorf("lifecycleNonRosterTicks excuses tick(s) that do not exist:\n  %s\n"+
			"An excuse matching nothing makes the list look considered while excusing "+
			"nothing. Re-key or drop it.", strings.Join(ghosts, "\n  "))
	}
	t.Logf("derived %d `*Tick` function(s): %d producer(s), %d classified non-roster",
		len(ticks), len(lifecycleTickProducers), len(lifecycleNonRosterTicks))
}

// lifecycleProducerLoopFloor guards against the walk silently matching nothing.
const lifecycleProducerLoopFloor = 4

// lifecycleProducerLoopRulings — every iteration inside the producers named in
// lifecycleTickProducers, what it is for, and how many times that exact `range`
// expression appears. (Three names since T-14 item 5; runLifecycleTick appears
// in none of the rulings below because it iterates nothing — it calls one half,
// then the other.)
//
// 🔴 ADDING A LOOP HERE IS THE THING TO THINK TWICE ABOUT. If it walks the
// roster and does something to rows BEFORE the decide pass, it is a
// pre-decide formality and it belongs in lifecycleRosterPasses
// (lifecycle_roster.go), not here — that is the entire lesson of stages 1–3.
// Put it on the list, give it an AppliesTo, and BOTH populations get it by
// construction. A loop written here reaches whichever population that producer
// happens to read, and the other side cannot tell the difference between "this
// formality was withheld from me" and "this formality does not exist".
var lifecycleProducerLoopRulings = map[string]producerLoopRuling{
	"runReconcileTick :: for _, m := range all": {
		Count: 1,
		Why: "THE entry filter, applied through lifecyclePolicyFor — the one question " +
			"that used to be hand-written in four places. It builds the candidate set " +
			"and does not touch rows; the formalities run afterwards, from the shared " +
			"list.",
	},
	"runReconcileTick :: for i := range members": {
		Count: 1,
		Why: "the decide→dispatch pass, one candidate at a time, AFTER the shared " +
			"pre-decide formalities and the receipt sweep. This is the tick's terminal " +
			"loop, not a formality — a new stamp added in here would be exactly the " +
			"regression this gate exists to announce.",
	},
	"runOutsourceTick :: for _, t := range tasks": {
		Count: 4,
		Why: "four passes over the task snapshot, none of them a roster formality: the " +
			"task-id→type index, the T-ba04 handover-timeout reaper, the " +
			"task-id→status index for the dependency hold (T-74f8), and the assignment " +
			"candidate filter that feeds outsourceDecide. The reaper is the only one " +
			"that writes, and what it releases is a task's orphaned PREDECESSOR — a " +
			"task-lifecycle act, downstream of runWorkerLifecyclePasses. 🔴 The count " +
			"was 3 when this entry was first written and the gate said so: the " +
			"candidate filter had been overlooked. That is the mechanism working, and " +
			"it is the reason the count is part of the ruling rather than a bare key.",
	},
	"runOutsourceTick :: for _, w := range workers": {
		Count: 2,
		Why: "two passes over the worker snapshot: the live-count/codename fold (read " +
			"only, feeds the parallel cap), and the per-worker FSM dispatch AFTER " +
			"runWorkerLifecyclePasses. The second is this producer's terminal loop, the " +
			"twin of runReconcileTick's decide pass. 🔴 If you are about to add a stamp " +
			"to either of them, it is a pre-decide formality and belongs in " +
			"lifecycleRosterPasses so 正職 gets it too.",
	},
	"runOutsourceTick :: for _, m := range manuals": {
		Count: 1,
		Why: "builds the type-key→spec map from the task manuals. Nothing to do with " +
			"the roster at all — manuals are templates, not rows.",
	},
	"runOutsourceTick :: for _, d := range decisions": {
		Count: 1,
		Why: "the mint/bind/fan pass over the scheduler's decisions, which is where a " +
			"worker row is CREATED. Downstream of every formality by definition: the " +
			"row does not exist yet when the passes run.",
	},
}

func TestTickProducersHaveNoUndeclaredRosterLoop(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("glob: %v (%d files) — this gate would be vacuous", err, len(paths))
	}
	fset := token.NewFileSet()

	seenProducer := map[string]bool{}
	live := map[string]int{}
	total := 0

	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if _, skip := identityScanSkip[base]; skip {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			want := false
			for _, name := range lifecycleTickProducers {
				if fd.Name.Name == name {
					want = true
				}
			}
			if !want {
				continue
			}
			if seenProducer[fd.Name.Name] {
				t.Fatalf("two functions named %q — the key space this gate uses is "+
					"ambiguous", fd.Name.Name)
			}
			seenProducer[fd.Name.Name] = true
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				var text string
				switch v := n.(type) {
				case *ast.RangeStmt:
					head := "for "
					if v.Key != nil {
						head += identityExprText(fset, v.Key)
						if v.Value != nil {
							head += ", " + identityExprText(fset, v.Value)
						}
						head += " := range "
					} else {
						head += "range "
					}
					text = head + identityExprText(fset, v.X)
				case *ast.ForStmt:
					text = "for " + identityExprText(fset, v.Cond)
				default:
					return true
				}
				live[fd.Name.Name+" :: "+text]++
				total++
				return true
			})
		}
	}

	// ── anti-vacuity, and it is the important half here. If a producer were
	// RENAMED, a gate keyed on its old name would find nothing, report nothing,
	// and go green — guarding a function that no longer exists while the real
	// one grows loops unwatched. Zero hits must be a FAILURE, never a pass.
	for _, name := range lifecycleTickProducers {
		if !seenProducer[name] {
			t.Fatalf("lifecycleTickProducers names %q, which is not a function "+
				"declared in this package's production sources. This gate is the only "+
				"thing standing between 「加一個 pre-decide roster loop 不進清單」and a "+
				"green suite (LIFECYCLE-LIST-IS-OPT-IN-T170E), and a stale producer "+
				"name turns it off silently. Re-key it to the producer's current name.",
				name)
		}
	}
	if total < lifecycleProducerLoopFloor {
		t.Fatalf("only %d loop(s) found across the producers (floor %d) — the walk is "+
			"broken, not the code; every check below would be vacuous",
			total, lifecycleProducerLoopFloor)
	}
	t.Logf("producer loop scan: %d loops across %d producers (%d distinct range "+
		"expressions, ledger holds %d)", total, len(seenProducer), len(live),
		len(lifecycleProducerLoopRulings))

	var problems []string
	for key, n := range live {
		ruling, ok := lifecycleProducerLoopRulings[key]
		if !ok {
			problems = append(problems, key+" — NOT ON THE LIST (appears "+
				itoa(n)+"x)")
			continue
		}
		if ruling.Count != n {
			problems = append(problems, key+" — appears "+itoa(n)+"x, the ruling "+
				"accounts for "+itoa(ruling.Count)+"x")
		}
		if len(strings.TrimSpace(ruling.Why)) < 40 {
			problems = append(problems, key+" — the Why is "+
				itoa(len(strings.TrimSpace(ruling.Why)))+" chars")
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Errorf("undeclared iteration inside a lifecycle tick producer:\n  %s\n\n"+
			"🔴 THIS IS THE SHAPE OF BOTH HISTORICAL FAILURES. A worker got no "+
			"token-expiry lead and no survived-stop sweep, and NEITHER of them "+
			"contained a `Kind ==` anywhere: the staff pass simply ran inline in "+
			"runReconcileTick, whose roster read is ListMembers — `WHERE kind != "+
			"'outsource'` by construction (dal.go). From a worker's side, a formality "+
			"that guards only 正職 and a formality that does not exist are "+
			"indistinguishable.\n"+
			"So, for the loop above:\n"+
			"  (a) if it does anything TO a row before the decide pass, it is a "+
			"pre-decide formality — put it in lifecycleRosterPasses "+
			"(lifecycle_roster.go) with an AppliesTo, and both populations get it by "+
			"construction while lifecycle_roster_parity_t170e_test.go reads the reach "+
			"back by name;\n"+
			"  (b) if it genuinely is not a roster formality (an index build, the "+
			"terminal decide pass), add it to lifecycleProducerLoopRulings with the "+
			"reason and the count.\n"+
			"⚠️ Go will not have warned you about any of this: a pass added with a "+
			"missing field still compiles.", strings.Join(problems, "\n  "))
	}

	var stale []string
	for key := range lifecycleProducerLoopRulings {
		if _, ok := live[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("lifecycleProducerLoopRulings accounts for loop(s) that no longer "+
			"exist:\n  %s\nRe-key the entry (the loop moved) or drop it (the loop went "+
			"— possibly into lifecycleRosterPasses, which is the good outcome). A stale "+
			"entry hides whether the loop it used to name is still accounted for.",
			strings.Join(stale, "\n  "))
	}
}

// itoa avoids pulling strconv in for three call sites in an error path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
