package main

// migration_upgrade_path_t64_test.go — T-64: the check that a STATION ALREADY
// RUNNING can boot on this tree.
//
// 🔴 WHY THIS EXISTS, AND WHY ANOTHER ASSERTION WOULD NOT HAVE DONE.
// Three guards already stand on the migration path and all three are real:
// TestMigrationVersionNumbersAreClaimedByExactlyOneSource and
// TestDuplicateVersionFindingsNamesBothSourcesAndTheNumber ask whether one
// number is claimed twice; TestGooseUpOnFreshSQLite asks whether the set applies
// to a BRAND NEW database. On 2026-09-03 four packages were in flight each
// carrying a migration numbered BELOW main's released maximum, and all three
// guards were green on those trees (measured, not reasoned: a probe .sql at
// 00066 and a probe Go registration at 00067 were planted in turn and the three
// named tests were run — PASS every time), while the same binary run against a
// database already at version 69 died with
//
//	found 1 missing migrations before current version 69: version 66: …
//
// exit 1, the server never listening. The reason no assertion caught it is
// structural rather than an oversight: ON A BLANK DATABASE THE CONCEPT OF A
// MISSING MIGRATION DOES NOT EXIST. Twelve test files do build on a
// half-migrated database (goose.UpTo / goose.DownTo), but every one of them
// gets there by applying a CONTIGUOUS PREFIX from empty, and a prefix cannot
// have a hole. So what was absent was not a line of code — it was a POPULATION:
// no test anywhere owned a database that had been migrated by a DIFFERENT
// (earlier, released) migration set. This file builds that population.
//
// WHAT IT DOES.
//  1. Works out which versions main has already RELEASED — see releasedVersions
//     below for where that fact comes from and what it costs.
//  2. Replays exactly those onto a throwaway SQLite file: the result is a
//     database in the state of a station in the field.
//  3. Runs the PRODUCTION runMigrations against it. If a version below that
//     station's version is new in this tree, goose refuses, in its own words,
//     and this test hands that refusal back with the number to use instead.
//
// The error text is goose's, deliberately. A hand-written explanation of what
// goose does is a sentence that goes stale the next time goose changes; the
// tool's own refusal cannot.
//
// 🔴 WHAT THIS DOES NOT DO — the sibling scan file (t49e7) carries the same
// section, and for the same reason: a guard read as covering more than it does is
// worse than no guard.
//
//   - It does not FETCH. It reads the local origin/main ref, because network
//     inside a unit test is one more thing that fails quietly. A stale ref means a
//     stale baseline, and it skews the two halves OPPOSITE ways: version numbers
//     go TOO PERMISSIVE (smaller released max), while shipped bodies go TOO
//     STRICT — main's own edits since that ref are reported as this tree editing
//     a shipped migration. So a stale ref DOES raise false alarms, and they are
//     the specific, believable kind. The SHA and its date are logged for exactly
//     this reason; read them before believing a red.
//   - It does not stop two branches picking the SAME free number. "Greater than
//     main's maximum" is necessary, not sufficient; the collision only becomes
//     visible once both branches meet, where the duplicate-version guard catches
//     it. Scanning the remote branches before choosing is still the procedure.
//   - It cannot make a green run STAY true. main's branch protection has
//     required_status_checks.strict = false, so a run that went green before
//     somebody else's migration landed is still mergeable afterwards, and this
//     check is never asked again. Closing that is a repository setting, not code.
//   - Two of the assertions in the replay (the station's version, and the Go
//     migrations being present in it) cannot fail given the checks above them.
//     They are canaries on goose, labelled as such at their site — not gates.
//   - The byte-for-byte check on an already-shipped migration compares WHOLE
//     FILES, both kinds. For a Go migration that means the whole source file, not
//     just the up/down bodies: an AST-level comparison would be the sharper tool
//     and this is not it. The consequence is one of scope, not of silence — a
//     purely mechanical edit to a shipped Go migration (a rename reaching it, a
//     gofmt change) turns this red and needs a human to say what to do, exactly as
//     it already did for *.sql.

import (
	"bytes"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

// treeMigrationVersions is every version this working tree declares, from BOTH
// sources — the embedded *.sql files and the Go migrations registered with
// goose.AddNamedMigration*. It reuses the enumerators the duplicate-version
// guard already owns rather than growing a second, quietly divergent scanner;
// "migrations/ only" is exactly the incomplete denominator that guard was
// written to kill.
func treeMigrationVersions(t *testing.T) map[int64]migrationClaim {
	t.Helper()
	out := map[int64]migrationClaim{}
	for _, c := range append(sqlMigrationClaims(t), goMigrationClaims(t)...) {
		out[c.version] = c
	}
	return out
}

// releasedMigration is one migration AS ORIGIN/MAIN HAS IT: where it lives, which
// kind of source declared it, and the bytes main ships.
//
// The bytes are read ONCE, here, because two consumers need them (the replay FS
// and the byte-for-byte check) and reading them twice is 61 extra subprocesses on
// a job whose time budget is already warning. The KIND is carried as a field for
// a sharper reason: it used to be re-derived by string-matching the display
// label, and one of those matches (`HasSuffix(where, ".go")`) could never be true,
// because a Go registration's label ends in `:<line>`. A dead branch that reads
// as a second supported format is worse than no branch.
type releasedMigration struct {
	path  string // repo-relative path on origin/main — the thing to `git show`
	where string // for humans: the path, or "<file>.go:<line>" for a Go registration
	isSQL bool
	body  string // main's bytes for path
}

// releasedVersions answers "which migration versions has main already shipped",
// computed FROM origin/main at the moment of the check.
//
// 🔴 WHY IT IS COMPUTED AND NOT CHECKED IN. The obvious alternative is a file in
// the tree listing the released numbers. It was rejected on Kyle's ruling
// (2026-09-03) for one reason: such a file goes stale silently. Whoever lands a
// migration has to remember to bump it, and when they forget nothing turns red —
// the guard keeps passing while the thing it guards has quietly moved. That is
// the exact failure shape this whole line of work exists to remove. A value read
// out of origin/main is a fact at the time it is read: a rebase re-derives it
// for free, and nobody has to remember anything.
//
// 🔴 WHAT IT COSTS, STATED PLAINLY. It needs a git repository that can resolve
// origin/main. Where that is not available the check cannot run, and this test
// SKIPS rather than passing — see the skip sites. A skip is not a green: read
// the reason.
func releasedVersions(t *testing.T) (versions map[int64]releasedMigration, ref string) {
	t.Helper()
	sha, err := gitOut("rev-parse", "origin/main")
	if err != nil {
		// 🔴 THE SKIP AND THE FAILURE ARE THE SAME EVENT, JUDGED DIFFERENTLY, and
		// they are decided HERE, at the one site that can produce them, so a second
		// skip path can never quietly un-coupled itself from the check that a skip
		// did not happen. Locally a skip is legitimate — a tarball has no remote.
		// In CI it is not: `go test` without -v prints `ok` for a package whose
		// tests all skipped, so a shallow checkout would take this guard offline
		// while the run stayed green, which is the exact silent-decay shape the
		// guard exists to remove.
		if inCI() {
			t.Fatalf("running in CI but origin/main does not resolve (%v), so the "+
				"upgrade-path check has no baseline and this run would have been green with "+
				"the check switched off. FIX: give the go-checks job's actions/checkout "+
				"`fetch-depth: 0` — a default (shallow) checkout carries only "+
				"refs/remotes/pull/N/merge.", err)
		}
		t.Skipf("T-64 upgrade-path guard cannot run here: this checkout cannot resolve "+
			"origin/main (%v). It is NOT a pass — the guard simply had no baseline. It is "+
			"enforced in CI, where a missing baseline is a failure rather than this skip.", err)
	}
	versions = map[int64]releasedMigration{}
	// Source ① — the SQL files as main has them.
	files, err := gitOut("ls-tree", "--full-tree", "--name-only", "origin/main", "server/ocserverd/migrations/")
	if err != nil {
		t.Fatalf("list origin/main migrations: %v", err)
	}
	for _, f := range strings.Split(files, "\n") {
		f = strings.TrimSpace(f)
		if !strings.HasSuffix(f, ".sql") {
			continue
		}
		v, err := goose.NumericComponent(path.Base(f))
		if err != nil {
			t.Fatalf("%s on origin/main has no version prefix goose can read: %v", f, err)
		}
		body, err := gitOut("show", "origin/main:"+f)
		if err != nil {
			t.Fatalf("read released migration %d (%s) out of origin/main: %v", v, f, err)
		}
		versions[v] = releasedMigration{path: f, where: f, isSQL: true, body: body}
	}
	// Source ② — the Go migrations as main has them, read by parsing main's own
	// copies of this package's files. A grep would match this file's prose; an
	// AST walk cannot match a comment.
	goFiles, err := gitOut("ls-tree", "--full-tree", "--name-only", "origin/main", "server/ocserverd/")
	if err != nil {
		t.Fatalf("list origin/main package files: %v", err)
	}
	parsed := 0
	for _, f := range strings.Split(goFiles, "\n") {
		f = strings.TrimSpace(f)
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := gitOut("show", "origin/main:"+f)
		if err != nil {
			t.Fatalf("read %s from origin/main: %v", f, err)
		}
		parsed++
		for v, line := range registrationsIn(t, filepath.Base(f), src) {
			versions[v] = releasedMigration{path: f, where: line, body: src}
		}
	}
	// Anti-vacuity: a scan that read nothing finds nothing and is indistinguishable
	// from a clean answer.
	if parsed < 20 {
		t.Fatalf("read only %d non-test .go files from origin/main — that corpus is too small "+
			"to be the real package, so a finding of zero Go migrations would mean nothing", parsed)
	}
	if len(versions) == 0 {
		t.Fatal("origin/main declares zero migrations — impossible, so the scan is wrong, not main")
	}
	return versions, sha
}

// registrationsIn parses one file's source and returns version -> "file:line"
// for every literal goose registration in it. Same shape as the duplicate-version
// guard's registrarLocations, but over SOURCE TEXT so it can be pointed at a file
// that only exists inside a git object.
func registrationsIn(t *testing.T, name, src string) map[int64]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v — a file this scan cannot read is a file it cannot clear", name, err)
	}
	found := map[int64]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "AddNamedMigration") || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		arg, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		v, err := goose.NumericComponent(arg)
		if err != nil {
			return true
		}
		found[v] = fmt.Sprintf("%s:%d", name, fset.Position(call.Lparen).Line)
		return true
	})
	return found
}

// gitOut keeps stderr. `.Output()` discards it, which turns every git failure
// into the bare string "exit status 128" — and this file's skip message is the
// one place a reader has to be told WHY the baseline could not be read.
func gitOut(args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// inCI reports whether this run is the one that gates a merge. GITHUB_ACTIONS is
// checked first because it is what this repo's workflow actually sets and cannot
// be set by accident; CI is honoured too but is a crowded name (GitLab, Circle,
// Netlify, and plenty of dotfiles export it), which is why it is not alone.
func inCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != "" || os.Getenv("CI") != ""
}

// releasedOnlyFS is the migrations directory AS THE RELEASED STATION HAD IT: only
// the versions main ships, and — this is the part that is easy to get subtly
// wrong — with MAIN'S BYTES, not the working tree's.
//
// 🔴 WHY THE BYTES MATTER. Taking the tree's copy of an already-released file
// would make an in-place EDIT of a shipped migration invisible: the replay would
// apply the edited body, the station would come out matching the tree, and the
// upgrade would pass. But a station in the field ran the version that shipped,
// and goose will never run that file again — the edit reaches exactly nobody who
// already upgraded, while looking applied on every fresh install. Reading the
// blob out of origin/main is what makes the population the real one; the
// machinery to do it is already here for the Go scan.
func releasedOnlyFS(t *testing.T, released map[int64]releasedMigration) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	kept := 0
	for _, rm := range released {
		if !rm.isSQL {
			continue // a Go migration: it comes from goose's registry, not the FS
		}
		// goose reads this FS at "migrations/<name>"; origin/main paths are
		// repo-relative.
		m["migrations/"+path.Base(rm.path)] = &fstest.MapFile{Data: []byte(rm.body + "\n")}
		kept++
	}
	if kept == 0 {
		// Unreachable while main ships any .sql migration at all; kept as the
		// anti-vacuity net, because a replay over an empty FS proves nothing and
		// would otherwise look exactly like a clean pass.
		t.Fatal("the released-only FS came out empty — the replay would prove nothing")
	}
	return m
}

// TestAStationAtTheReleasedVersionCanUpgradeToThisTree is the check.
//
// RED WHEN: this tree declares a migration whose version is BELOW the highest
// version main has released and which main does not have — the shape that makes
// every station in the field fail to boot after the merge.
//
// It must not run in parallel: it drives goose's package-level base FS, which
// runMigrations also sets.
func TestAStationAtTheReleasedVersionCanUpgradeToThisTree(t *testing.T) {
	tree := treeMigrationVersions(t)
	released, ref := releasedVersions(t)

	var releasedMax int64
	for v := range released {
		if v > releasedMax {
			releasedMax = v
		}
	}
	var treeMax int64
	for v := range tree {
		if v > treeMax {
			treeMax = v
		}
	}
	// The number to tell somebody to use. It has to clear BOTH sides: this tree's
	// own highest and everything main has released. They differ exactly when the
	// branch is behind — and since the numbering checks now run on a behind branch
	// too (they do not need a rebase to be true), taking the tree's max alone would
	// hand out a number main has ALREADY shipped, i.e. advise the collision this
	// file exists to prevent.
	nextFree := treeMax + 1
	if releasedMax >= nextFree {
		nextFree = releasedMax + 1
	}

	// The baseline is only as fresh as origin/main is: this reads a REF, it never
	// fetches (network in a unit test is another thing that fails quietly). A stale
	// ref yields a stale baseline, and it skews the two halves of this check in
	// OPPOSITE directions — which is why the SHA and its date are logged, and not
	// merely the fact that some baseline was used.
	//
	//   Version numbers go TOO PERMISSIVE: an older ref has a smaller released
	//   max, so a version this check should have rejected slips through.
	//
	//   Shipped bodies go TOO STRICT: every edit main itself made between that
	//   older ref and today reads as THIS TREE editing a shipped migration, and
	//   the report names files and version numbers, so it is specific enough to
	//   be believed. Measured 2026-09-04 by pointing the ref one month back:
	//   five shipped migrations went red, all of them main's own history.
	//
	// So a reader of a failing run needs the date to know which world this verdict
	// was reached in — a red here is not evidence on its own.
	when, _ := gitOut("show", "-s", "--format=%ci", ref)
	t.Logf("baseline: origin/main %s (%s). If that is older than main really is, "+
		"`git fetch origin main` and re-run before believing anything below — a stale "+
		"baseline skews this check BOTH ways, not one. The version-number half goes too "+
		"PERMISSIVE (smaller released max, so a too-low version slips through). The "+
		"shipped-body half goes too STRICT: every edit main itself made since that ref "+
		"is reported as this tree editing a shipped migration, named file by file and "+
		"version by version, which reads exactly like a real finding.", ref, when)

	// A released version this tree does not declare has TWO causes and they are not
	// the same event, so they are not reported the same way.
	//
	// 🔴 WHY THE MERGE-BASE AND NOT "higher than my highest". The cheap test —
	// v > treeMax means main moved ahead — gets one case exactly backwards, and it
	// is the dangerous one: DELETE the highest released migration and treeMax falls
	// below it, so removing a shipped migration reads as an out-of-date branch and
	// skips locally. The merge-base answers the question actually being asked —
	// "did this branch ever have this file?" — because anything main added after
	// the fork is absent there, while everything the branch inherited is present.
	// (`contract-guards` already leans on `git merge-base HEAD origin/main` in CI,
	// so the ref depth this needs is one the workflow is known to provide.)
	mergeBase, mbErr := gitOut("merge-base", "HEAD", ref)
	var behind, deleted []int64
	heuristic := false
	for v, rm := range released {
		if _, ok := tree[v]; ok {
			continue
		}
		inherited := false
		if mbErr == nil {
			_, err := gitOut("cat-file", "-e", mergeBase+":"+rm.path)
			inherited = err == nil
		} else {
			// No merge-base to ask (a tarball, a grafted clone). Fall back to the
			// old heuristic and SAY SO at the report site, because it is the one
			// that misfiles a deleted highest migration.
			heuristic = true
			inherited = v <= treeMax
		}
		if inherited {
			deleted = append(deleted, v) // a shipped migration is gone from the tree
		} else {
			behind = append(behind, v) // main moved ahead; this branch has not caught up
		}
	}
	sort.Slice(deleted, func(i, j int) bool { return deleted[i] < deleted[j] })
	sort.Slice(behind, func(i, j int) bool { return behind[i] < behind[j] })
	if len(deleted) > 0 {
		how := fmt.Sprintf("this branch inherited them at its merge-base with main (%s)", mergeBase)
		if heuristic {
			how = fmt.Sprintf("no merge-base was available (%v), so this was decided by the "+
				"weaker test 'below this tree's own highest version %d'", mbErr, treeMax)
		}
		t.Fatalf("origin/main (%s) ships migration version(s) %v that this tree does not "+
			"declare — %s, so this is not a branch that is merely behind, it is a shipped "+
			"migration that has been REMOVED. A released migration cannot be withdrawn: every "+
			"station has already applied it, and goose will refuse the next boot.",
			ref, deleted, how)
	}
	// ── Finding ⓪: an already-shipped migration must still be byte-for-byte what
	// shipped. ───────────────────────────────────────────────────────────────────
	// The SAME asymmetry as a low version number, reached from the other side: a
	// station already past version V will never run V again, so an edit to it lands
	// on fresh installs ONLY. Nothing errors; the two populations just quietly stop
	// having the same schema, and every test in this package (all of which install
	// from empty) sees the edited version and agrees with itself.
	//
	// This is BEYOND the ticket's acceptance criteria, which are about version
	// numbering. It is here because it is the same defect family and the bytes were
	// already in hand for the replay — say so, and strip it if that is the wrong
	// call.
	seen := map[string]bool{}
	for v, rm := range released {
		if _, ok := tree[v]; !ok {
			continue // absent from this tree — already classified as deleted/behind above
		}
		if seen[rm.path] {
			continue // one Go file can register several versions; compare it once
		}
		seen[rm.path] = true

		var local []byte
		var err error
		if rm.isSQL {
			local, err = fs.ReadFile(embeddedMigrations, "migrations/"+path.Base(rm.path))
		} else {
			// The Go migrations live beside this test; the package dir is the cwd.
			local, err = os.ReadFile(filepath.Base(rm.path))
		}
		if err != nil {
			// TWO DIFFERENT EVENTS LAND HERE AND THEY NEED OPPOSITE FIXES, so this
			// must not name one of them and stop (T-64, measured 2026-09-04):
			//
			//   RENAME — this tree once had main's file and moved it. goose
			//   identifies a Go migration by the string passed to AddNamedMigration*
			//   rather than by the filename, so a rename leaves the version declared
			//   while the path main knows is gone. FIX: put the file back.
			//
			//   COLLISION — two branches independently took the same free number.
			//   main's file was never in this tree at all; the other branch landed
			//   first. FIX: renumber THIS tree's file upward.
			//
			// On the file listing alone the two are IDENTICAL — main has one path at
			// version v, this tree has another — so telling them apart needs history,
			// not the working tree. The measured cost of guessing is not symmetric:
			// "put the file back where main has it" told at a COLLISION is an
			// instruction to overwrite a migration somebody else has already shipped.
			// So when history cannot answer, print BOTH rather than pick one.
			// ":(top)" anchors the pathspec at the REPO ROOT. Without it git resolves
			// rm.path relative to the CWD — which for this package is server/ocserverd
			// — so every lookup silently finds nothing and EVERY case reads as a
			// collision, including the renames. Measured: the rename arm came back
			// "NEVER existed in this tree's history" for a file with two commits on it.
			seenHere, histErr := gitOut("log", "--oneline", "-1", "HEAD", "--", ":(top)"+rm.path)

			var diagnosis string
			switch {
			case histErr != nil:
				diagnosis = fmt.Sprintf("WHICH OF THE TWO THIS IS could not be determined "+
					"— reading this tree's history for that path failed (%v) — so both are "+
					"below. Tell them apart by hand, from the repo root: "+
					"`git log HEAD -- %s`. Commits ⇒ RENAME. "+
					"Nothing ⇒ COLLISION.", histErr, rm.path)
			case seenHere == "":
				diagnosis = fmt.Sprintf("THIS IS A COLLISION, not a rename: %s has NEVER "+
					"existed in this tree's history, so this tree did not move it — another "+
					"branch took version %d and landed first. FIX: renumber THIS tree's file "+
					"(%s) to %d, the lowest number free of both this tree and main. DO NOT "+
					"put main's file 'back': it was never here, and writing your version of "+
					"%d over a migration that has already shipped is the failure this whole "+
					"check exists to prevent.", rm.path, v, tree[v].where, nextFree, v)
			default:
				diagnosis = fmt.Sprintf("THIS IS A RENAME, not a collision: %s DOES appear "+
					"in this tree's history (%s), so this tree had it and moved it. A "+
					"released migration's path is part of what shipped. FIX: put the file "+
					"back where main has it. If the path genuinely has to change, that is a "+
					"decision for a person, not something to route around here.",
					rm.path, seenHere)
			}

			t.Fatalf("main ships released migration %d as %s, and this tree declares "+
				"version %d at %s, but main's path cannot be read here: %v.\n%s",
				v, rm.path, v, tree[v].where, err, diagnosis)
		}
		if strings.TrimSpace(rm.body) != strings.TrimSpace(string(local)) {
			kind := "migration file"
			mechanical := ""
			if !rm.isSQL {
				kind = "Go migration's source file"
				// A Go migration shares a package with everything else in it, so a
				// repo-wide rename reaches it whether or not anyone meant to touch a
				// shipped migration. That case is real, and this check cannot tell it
				// apart from a behaviour change — so it must not pretend to.
				mechanical = "\nThis is a Go file, so a repo-wide rename or a formatting pass " +
					"can reach it WITHOUT changing what the migration does. This check cannot " +
					"tell that apart from a real edit, and does not try to. If that is what " +
					"happened, do NOT open a new migration — an empty one fixes nothing. The " +
					"two cases part ways here, and only one of them has an out.\n" +
					"  RENAME: keep the shipped file byte-for-byte as it shipped (alias the " +
					"identifier, or leave the old name standing in this file) so that what " +
					"stations ran and what this tree says they ran remain the same text.\n" +
					"  FORMATTING: there is no such out. lint-go-fmt is a required check and " +
					"it demands the formatted text; this check demands the shipped text. Both " +
					"cannot hold at once, so no edit you make here clears them both — it is a " +
					"real deadlock and a human has to break it. Decide deliberately whether " +
					"to exempt this file from the formatter or to accept the reformat and " +
					"re-baseline this check, and say which in the PR. Do not pick one " +
					"silently, and do not reach for a new migration to escape it."
			}
			t.Errorf("%s for version %d (%s) has ALREADY SHIPPED and this tree changes its "+
				"body. goose records one row per version and never revisits it, so whatever "+
				"this edit does reaches nobody who has already upgraded past %d — it takes "+
				"effect on fresh installs only. IF THE EDIT CHANGES WHAT THE MIGRATION DOES, "+
				"that is two populations with different schemas and not a single error "+
				"anywhere; every test in this package installs from empty and will agree with "+
				"the edited version, so nothing here would notice. FIX: leave the shipped file "+
				"alone and put the CHANGE IN BEHAVIOUR in a new migration numbered %d.%s "+
				"(baseline origin/main %s)",
				kind, v, rm.where, v, nextFree, mechanical, ref)
		}
	}

	// ── Finding ①: the arithmetic, over BOTH sources. ────────────────────────────
	// Reported with Errorf, not Fatalf, so the end-to-end replay below still runs
	// and goose gets to state the same fact in its own words.
	var offenders []int64
	for v := range tree {
		if _, ok := released[v]; !ok && v <= releasedMax {
			offenders = append(offenders, v)
		}
	}
	sort.Slice(offenders, func(i, j int) bool { return offenders[i] < offenders[j] })
	for _, v := range offenders {
		t.Errorf("migration version %d (%s) is NEW in this branch but sits at or below %d, the "+
			"highest version origin/main (%s) has released. Every station that has already "+
			"migrated past %d will refuse to boot after this lands: goose stops at a version it "+
			"skipped, before applying anything, and the server exits without listening. "+
			"FIX: renumber it to %d (one above the highest version anyone has), and never reuse "+
			"a skipped number — a hole is deliberate here, it is what makes the refusal loud.",
			v, tree[v].where, releasedMax, ref, releasedMax, nextFree)
	}

	// Merely behind main. Reached only AFTER Findings ⓪ and ① have run: neither of
	// them needs the tree to be up to date (an already-shipped file's bytes, and
	// "is this number at or below main's highest", are both answerable from a stale
	// branch), and skipping them here threw away a real offender's red — a branch
	// that was behind AND carrying an illegal low number went green locally.
	// Only the REPLAY below needs a comparable pair, so only the replay is skipped.
	if len(behind) > 0 {
		// In CI this cannot happen — the pull_request checkout is the MERGE ref,
		// which already carries main — so if it does, something about the baseline
		// is wrong and silence would be the worse answer. Locally it is the ordinary
		// state of a branch someone has not rebased yet, and failing there would
		// teach people to ignore this test.
		if inCI() {
			t.Fatalf("origin/main (%s) ships migration version(s) %v that this tree lacks, "+
				"while running in CI — where the checkout is the merge ref and therefore "+
				"already contains main. The baseline and the tree are not the pair this test "+
				"assumes; do not read the result below as a pass.", ref, behind)
		}
		t.Skipf("the numbering and content checks above have RUN and are reflected in this "+
			"result; only the end-to-end replay is skipped, because this branch is behind "+
			"origin/main (%s), which ships version(s) %v it does not have — the released set "+
			"and this tree are not yet a comparable pair. Rebase and re-run to get the replay "+
			"too. CI checks the merged result anyway.", ref, behind)
	}

	// ── Finding ②: the population itself. ────────────────────────────────────────
	// Build a database in the state of a station in the field, then run the real
	// production migration path against it.
	db, err := openSQLite(filepath.Join(t.TempDir(), "released-station.db"))
	if err != nil {
		t.Fatalf("open throwaway db: %v", err)
	}
	defer db.Close()

	// goose's base FS is a PACKAGE GLOBAL. runMigrations sets it back on the happy
	// path, but every t.Fatalf between here and there would leave the narrowed FS
	// in place for whatever test runs next. Nothing enforces that the other goose
	// callers set it themselves, so this does not rely on them.
	defer goose.SetBaseFS(embeddedMigrations)
	goose.SetBaseFS(releasedOnlyFS(t, released))
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	// UpTo(releasedMax) rather than Up(): the version window is the ONLY lever that
	// keeps a Go migration this branch adds out of the replay. Narrowing the FS does
	// not — goose merges its global Go registry in wholesale when the FS carries no
	// .go files (measured in goose v3.27.2, collectGoMigrations' else branch), and
	// Provider's WithExcludeVersions filters filesystem sources only.
	if err := goose.UpTo(db, "migrations", releasedMax); err != nil {
		t.Fatalf("replaying the released migration set failed: %v", err)
	}
	// A Go migration this branch adds BELOW releasedMax is inside that window, so it
	// was applied anyway. Take its bookkeeping row back out: the station never ran
	// it, and goose refuses on the bookkeeping before it touches any schema.
	for _, v := range offenders {
		if tree[v].source != migrationSourceGo {
			continue
		}
		if _, err := db.Exec(`DELETE FROM goose_db_version WHERE version_id = ?`, v); err != nil {
			t.Fatalf("un-apply Go migration %d from the replayed station: %v", v, err)
		}
	}

	// The next two are RE-CONFIRMATIONS, not second gates, and they are labelled as
	// such rather than left to look load-bearing: given the checks above (every
	// released version is in the tree, and no offender is in the released set) they
	// cannot fail today. They are kept as canaries on goose itself — if a future
	// goose changes what UpTo applies, they are what says so instead of the replay
	// silently becoming a different population.
	if got := stationVersion(t, db); got != releasedMax {
		t.Fatalf("the replayed station sits at version %d, want %d (origin/main %s) — "+
			"the population is not what this test claims it is, so nothing below it counts",
			got, releasedMax, ref)
	}
	// The population must contain the Go migrations too, or the denominator has a
	// hole exactly where this repo's two Go migrations live.
	assertGoMigrationsAreInTheStation(t, db, released)

	// The upgrade itself: the SAME function main.go calls at boot, no test-only
	// re-implementation of it.
	if err := runMigrations(db); err != nil {
		t.Fatalf("a station already at version %d CANNOT boot on this tree.\n"+
			"goose's own refusal: %v\n"+
			"This is the boot path: cmdServe calls runMigrations before it listens, so the "+
			"station exits 1 and never comes up. FIX: renumber the offending migration(s) to "+
			"%d or higher (baseline read from origin/main %s).",
			releasedMax, err, nextFree, ref)
	}
}

// assertGoMigrationsAreInTheStation pins that the replayed population covers Go
// migrations and not just migrations/*.sql. Without it the whole construction
// could be blind to half the migration sources and still look correct.
func assertGoMigrationsAreInTheStation(t *testing.T, db *sql.DB, released map[int64]releasedMigration) {
	t.Helper()
	var want []int64
	for v, rm := range released {
		if !rm.isSQL {
			want = append(want, v)
		}
	}
	if len(want) == 0 {
		t.Fatal("origin/main declares no Go migrations at all — this repo has had them since " +
			"00054, so the baseline scan is reading only migrations/*.sql and the denominator " +
			"is incomplete")
	}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for _, v := range want {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM goose_db_version WHERE version_id = ? AND is_applied = 1`, v,
		).Scan(&n); err != nil {
			t.Fatalf("read goose bookkeeping for %d: %v", v, err)
		}
		if n == 0 {
			t.Fatalf("Go migration %d (%s) is NOT applied in the replayed station. The replay "+
				"covers migrations/*.sql only, which is the incomplete denominator this repo "+
				"has already been bitten by once", v, released[v].where)
		}
	}
	t.Logf("replayed station carries the Go migrations too: %v", want)
}

func stationVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read station version: %v", err)
	}
	if !v.Valid {
		return 0
	}
	return v.Int64
}
