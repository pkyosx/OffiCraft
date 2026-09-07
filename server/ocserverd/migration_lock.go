package main

// migration_lock.go — T-75: migration.lock, the file whose job is to TURN A
// CLEAN MERGE INTO A CONFLICT, plus the `migration-lock` subcommand that writes
// and checks it.
//
// 🔴 WHY THIS IS ORDINARY SOURCE AND NOT A _test.go FILE. Every function here
// used to live in a test file, and the reason was never that any of it is a
// test: the enumeration reads the go:embed FS the server hands goose and
// AST-walks this package for goose registrations, and NEITHER IS REACHABLE FROM
// OUTSIDE package main. A test file was the only place package-internal code
// could sit while still being runnable from a shell. A subcommand is the other
// place, it is the honest one, and it is where this lives now (T-125). Nothing
// below asserts anything; the assertions are in migration_lock_test.go and
// migration_lock_judgement_test.go, which are tests because they test THIS.
//
// 🔴 WHY THIS EXISTS AT ALL, AND WHY NO TEST COULD HAVE DONE IT.
// Two PRs each add a migration numbered 00072. They touch DIFFERENT FILES, so
// git merges both without a word — measured, rc=0, no conflict markers. Every
// migration guard in this package is a CHECK, and a check only speaks when
// something runs it: the collision therefore becomes visible on the FIRST CI run
// of the merged main, which in this repo is after the merge button, and merging
// main is how a release goes out. The window between "both PRs are green" and
// "the station will not boot" contains a deploy.
//
// So the missing piece was never another assertion. It was a place where the two
// branches WRITE THE SAME BYTES, because that is the only thing git will refuse
// to merge for us. migration.lock is that place, and its shape is chosen from
// measurements rather than taste (all done on real two-branch merges):
//
//	both branches append at the file's END .............. CONFLICT  ← what we want
//	branches write lines 50 / 3 / 1 apart .............. auto-merged, silently
//	header carries "count=65 max=00071" ................ auto-merged: a collision
//	                                                     makes BOTH sides write
//	                                                     the IDENTICAL summary, and
//	                                                     git never conflicts on an
//	                                                     identical change
//	header carries a HASH OF THE WHOLE LIST ............ CONFLICT  ← two different
//	                                                     lists cannot hash equal
//	.sql in one section, Go in another .................. auto-merged (the two
//	                                                     branches land in different
//	                                                     sections)
//
// Hence: ONE flat list, .sql and Go migrations mixed, appended to at the tail
// only, with a roll hash of the whole list on the header line. Both halves are
// load-bearing and neither is decoration.
//
// 🔴 WHAT EACH LINE CARRIES, AND WHY IT IS NOT JUST THE FILENAME.
// Every entry is `<version> <path> sha256:<content hash>`. The content hash is
// there for a failure the filename cannot see: EDITING AN ALREADY-SHIPPED
// migration. goose records one row per version and never looks at that version
// again, so an edit reaches only brand-new installs; every station that already
// upgraded keeps the old schema, and nothing anywhere errors. Two populations of
// stations, different schemas, complete silence. A filename-only lock is byte
// for byte identical before and after that edit.
//
// 🔴 THE TWO CLASSES OF JUDGEMENT HERE, AND THE HONEST DIFFERENCE BETWEEN THEM.
// Read this before trusting any green.
//
//	CLASS A — TREE-INTERNAL (migrationLockFindings). The lock is compared against
//	THIS tree's own migrations. It needs no baseline, so it is just as alive on
//	main as on a PR. It answers: "does the lock describe the tree it sits in?" —
//	i.e. added / renamed / deleted / edited a migration and did not regenerate the
//	lock; hand-edited a line; a middle insertion that the generator appended
//	(which shows up as a version column that stops ascending). This is what
//	`ocserverd migration-lock --check` runs, and it is what the drift-migration-lock
//	CI gate is.
//
//	CLASS B — AGAINST ORIGIN/MAIN (migrationLockPrefixFindings). The prefix rule:
//	main's entry lines must be an exact prefix of this tree's. It answers "was
//	anything already released changed or removed, or inserted below the released
//	maximum?" — and it can only answer that by comparing with a baseline.
//	⚠️ ON MAIN ITSELF THE TWO SIDES ARE THE SAME COMMIT, SO IT IS A NO-OP THERE.
//	It is a PR-path judgement and nothing more; it is stated here rather than left
//	for a reader to discover, because a guard believed to cover more than it does
//	is worse than no guard. bin/check-released-migrations is the OTHER, coarser
//	sayer of the same rule, reached from a plain git diff rather than from the
//	lock.
//
//	The consequence, spelled out: regenerate the lock after editing a shipped
//	migration and class A goes green — the lock now honestly describes the tree.
//	Only class B says that the edit was not allowed, and it will never say it on
//	main.
//
// 🔴 THE ENUMERATION IS SHARED ON PURPOSE — this is the failure this whole
// mechanism was most likely to ship. If the generator listed migrations one way
// and the checker listed them another, the checker would be validating a
// different corpus than the one that was written, and it would be GREEN while
// doing it. So there is exactly one enumerator, migrationLockTreeEntries, and
// BOTH `--write` and `--check` call it — sharing by CALL, not by convention.
// It carries its own anti-vacuity floor, because an enumeration that returned
// nothing would make every set-comparison below trivially true.
//
// 🔴 WHERE THE .sql BYTES COME FROM. The embedded FS (embeddedMigrations), not
// the working directory — the same corpus goose is actually handed in
// runMigrations. A .sql sitting on disk that the embed pattern does not match
// would otherwise be in the lock and invisible to goose, and the lock would
// certify a set the server never runs. The Go migrations are read from the
// package directory on disk instead, because that IS their authority: what the
// compiler links is what registers with goose.
//
// ⚠️ EVERYTHING HERE IS RELATIVE TO THIS PACKAGE'S DIRECTORY, which is where the
// lock, the .go migrations and the `migrations/` tree all sit. The subcommand is
// therefore run FROM server/ocserverd (bin/gen-migration-lock and
// bin/check-migration-lock both cd there). A wrong working directory does not
// pass quietly: registrarLocations' file-count floor refuses an implausibly
// small corpus rather than reporting zero registrations as a clean tree.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

// migrationLockFile is the lock's path, relative to this package directory.
//
// WHY BESIDE migrations/ AND NOT INSIDE IT. The lock covers BOTH sources: the
// .sql files under migrations/ and the Go migrations, which live in this
// directory and never under migrations/. Filed inside migrations/ it would read
// as a manifest of that directory — the exact incomplete denominator the
// duplicate-version guard exists to kill — and it would also sit inside the
// directory goose collects from.
const migrationLockFile = "migration.lock"

// The two REPO-relative paths the append-only half hands to git. They are a
// PAIR: the second is the only thing that tells "main does not carry the lock
// because this has not landed yet" apart from "main carries the guard but the
// lock is gone". If either file ever moves, BOTH constants must move in the
// same commit — otherwise that half goes back to skipping forever while still
// printing that it is enforced in CI, which is the exact shape this whole
// mechanism exists to kill.
const (
	// migrationLockPkgRepoPath is this package's directory FROM THE REPO ROOT.
	// It is load-bearing in the [lock:path] diagnosis below: every path in the
	// lock is relative to this directory, so a `git log … -- <lock path>` handed
	// to a reader standing at the repo root silently finds NOTHING and turns
	// every rename into a "collision" — measured on T-64, which hit exactly this
	// and fixed it with a `:(top)` pathspec.
	migrationLockPkgRepoPath = "server/ocserverd/"
	migrationLockRepoPath    = migrationLockPkgRepoPath + migrationLockFile
	// migrationLockGuardRepoPath is THIS file. It is the anchor that arms the
	// "main has the mechanism but not the lock" diagnosis, so it must name
	// whatever file carries the mechanism — which since T-125 is ordinary
	// source, not a _test.go.
	migrationLockGuardRepoPath = migrationLockPkgRepoPath + "migration_lock.go"
)

// The header line's prefix, and the per-entry hash prefix. Both are constants
// because they appear in failure messages and in the generator's output.
const (
	migrationLockRollPrefix = "roll sha256:"
	migrationLockHashPrefix = "sha256:"
)

// Anti-vacuity floors for the shared enumerator. Today's tree has 63 .sql and 2
// Go migrations; these are set well below that because they exist to catch an
// enumeration that has gone BLIND (wrong directory, wrong glob, an embed pattern
// that stopped matching), not to track the count. A count-tracking floor would
// need bumping on every migration and would be a second, quietly-drifting
// statement of how many migrations there are.
const (
	migrationLockMinSQL = 40
	migrationLockMinGo  = 2
)

// migrationLockEntry is one line of the lock: the version, where the migration
// lives, and the hash of its bytes.
type migrationLockEntry struct {
	version int64
	path    string // repo path relative to server/ocserverd/
	sha     string // hex sha256 of the migration's content
}

func (e migrationLockEntry) line() string {
	return fmt.Sprintf("%05d %s %s%s", e.version, e.path, migrationLockHashPrefix, e.sha)
}

// ---------------------------------------------------------------------------
// GIT AND ENVIRONMENT HELPERS
// ---------------------------------------------------------------------------

// gitOut keeps stderr. `.Output()` discards it, which turns every git failure
// into the bare string "exit status 128" — and a baseline that could not be read
// is the one place a reader has to be told WHY.
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

// ---------------------------------------------------------------------------
// THE SHARED ENUMERATION — one function, used by both --write and --check.
// ---------------------------------------------------------------------------

// registrarLocations parses every non-test .go file in this package and returns
// version -> "file:line" for each goose registration it can read literally.
//
// ⚠️ WHAT IT CANNOT SEE, stated because it was MEASURED and not imagined: a
// registration whose name is COMPUTED — goose.AddNamedMigrationContext(
// fmt.Sprintf(...), …) — is invisible to this AST walk, so the lock would never
// list it. gooseGoVersionCount below is the second, differently-derived number
// that turns that silent omission into a red.
func registrarLocations() (map[int64]string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read package dir: %w", err)
	}
	fset := token.NewFileSet()
	found := map[int64]string{}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w — a file this scan cannot read is a file it "+
				"cannot clear, so this is a failure and not a skip", name, err)
		}
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
				return true // a computed name: the registry still sees it, this locator does not
			}
			nameArg, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			v, err := goose.NumericComponent(nameArg)
			if err != nil {
				return true
			}
			found[v] = fmt.Sprintf("%s:%d", name, fset.Position(call.Lparen).Line)
			return true
		})
	}
	// Anti-vacuity: a scan over an empty corpus finds nothing and looks exactly
	// like a clean tree. It is also the thing that catches a wrong working
	// directory, which is the likeliest way this subcommand gets misused.
	if files < 20 {
		return nil, fmt.Errorf("the AST scan read %d files in this package — that corpus is too "+
			"small to be the real one, so a finding of zero registrations would mean nothing. "+
			"Run this from %s", files, migrationLockPkgRepoPath)
	}
	return found, nil
}

// gooseGoVersionCount asks goose itself how many GO-sourced migrations it holds,
// so the AST walk above has a second, differently-derived number to be checked
// against. registrarLocations reads the SOURCE and can only see a registration
// whose name is a literal; goose reads its own REGISTRY and sees every one. Two
// agreeing numbers derived the same way are one number; this is the other way.
//
// The recover is load-bearing. goose answers a duplicate version by PANICKING
// out of a sort comparator (`goose: duplicate version N detected`). Caught here
// it becomes a string the caller can report. sqlSources are the embedded .sql
// paths already enumerated as source ①; goose reports them under the same names,
// so subtracting them leaves the Go set.
func gooseGoVersionCount(sqlSources []string) (count int, refusal string, err error) {
	goose.SetBaseFS(embeddedMigrations) // the same base runMigrations sets
	isSQL := map[string]bool{}
	for _, p := range sqlSources {
		isSQL[p] = true
	}
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	collected, cerr := goose.CollectMigrations("migrations", 0, math.MaxInt64)
	if cerr != nil {
		return 0, "", fmt.Errorf("collect migrations: %w", cerr)
	}
	seen := map[int64]bool{}
	for _, m := range collected {
		if isSQL[m.Source] {
			continue
		}
		seen[m.Version] = true
	}
	return len(seen), "", nil
}

// migrationLockTreeEntries lists every migration this tree ships, from both
// sources, sorted by version. This is the ONLY enumeration: `--write` writes what
// it returns and `--check` compares against what it returns, so the two can never
// be describing different corpora.
func migrationLockTreeEntries() ([]migrationLockEntry, error) {
	var entries []migrationLockEntry

	// Source ① — the .sql files, out of the EMBEDDED FS (see the header note).
	sqlFiles, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("glob embedded migrations: %w", err)
	}
	for _, f := range sqlFiles {
		v, err := goose.NumericComponent(f)
		if err != nil {
			return nil, fmt.Errorf("%s has no version prefix goose can read: %w", f, err)
		}
		body, err := fs.ReadFile(embeddedMigrations, f)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", f, err)
		}
		entries = append(entries, migrationLockEntry{version: v, path: f, sha: sha256Hex(body)})
	}
	if len(sqlFiles) < migrationLockMinSQL {
		return nil, fmt.Errorf("the embedded FS yielded %d .sql migrations (floor %d) — a corpus "+
			"that small is not this repo's, so every set-comparison against it would be trivially "+
			"true", len(sqlFiles), migrationLockMinSQL)
	}

	// Source ② — the Go migrations. registrarLocations is the AST walk the
	// duplicate-version guard already owns (a grep would match this file's own
	// prose); it returns version -> "file.go:line", and the file is what we hash.
	located, err := registrarLocations()
	if err != nil {
		return nil, err
	}
	if len(located) < migrationLockMinGo {
		return nil, fmt.Errorf("the AST scan found %d Go migration registrations (floor %d) — this "+
			"repo has had Go migrations since 00054, so a smaller answer means the scan went blind, "+
			"not that they were removed", len(located), migrationLockMinGo)
	}
	// CORROBORATION, and it is not decoration. See gooseGoVersionCount's note:
	// the gap between the parse and the registry is a MEASURED escape hatch, and
	// because migrationLockMinGo equals today's count the floor above waves it
	// through. Asking a SECOND, differently-derived source for the same count is
	// what turns that silent omission into a red.
	goCount, refusal, err := gooseGoVersionCount(sqlFiles)
	switch {
	case err != nil:
		return nil, err
	case refusal != "":
		return nil, fmt.Errorf("goose refuses to enumerate this tree's migrations: %s\n"+
			"That is goose's own duplicate-version panic, caught here instead of at goose.Up "+
			"on a station. Two migrations share a version; find them and renumber the one that "+
			"has NOT shipped, then run bin/gen-migration-lock.", refusal)
	case goCount != len(located):
		return nil, fmt.Errorf("goose holds %d Go migration(s), the AST scan located %d — they must "+
			"agree.\nThe likely cause is a registration whose name is COMPUTED rather than a literal "+
			"(anything but a plain string as the first argument to AddNamedMigration*): goose sees "+
			"it, this scan cannot, and so %s would never list it — the version would exist for goose "+
			"and not for this check.\nFIX: pass a literal name. If a computed name is genuinely "+
			"required, this check has to learn to read it; do not silence this by raising a floor.",
			goCount, len(located), migrationLockFile)
	}
	for v, where := range located {
		file := where
		if i := strings.LastIndex(where, ":"); i >= 0 {
			file = where[:i]
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read Go migration %s (version %d): %w", file, v, err)
		}
		entries = append(entries, migrationLockEntry{version: v, path: file, sha: sha256Hex(body)})
	}

	for _, e := range entries {
		if strings.ContainsAny(e.path, " \t") {
			return nil, fmt.Errorf("migration path %q contains whitespace — the lock is "+
				"space-separated and would silently mis-parse it", e.path)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].version != entries[j].version {
			return entries[i].version < entries[j].version
		}
		return entries[i].path < entries[j].path
	})
	return entries, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// RENDER / PARSE
// ---------------------------------------------------------------------------

// migrationLockHeader is the lock file's comment block, and it is FROZEN TEXT,
// not prose to be tidied.
//
// 🔴 EDITING IT REWRITES migration.lock. `--check` byte-compares, so any change
// here — including fixing the stale file name two lines down, which since T-125
// points at a file that no longer exists — turns every checkout red until the
// lock is regenerated and committed. It is worth doing; it is not worth doing by
// accident, folded into an unrelated change, which is why this note is here
// instead of the fix.
const migrationLockHeader = `# migration.lock — every migration this tree ships, in the order they were added.
#
# GENERATED. Regenerate with:  bin/gen-migration-lock
#
# THE RULES, enforced by migration_lock_t75_test.go:
#   * new migrations are APPENDED AT THE END of this file, never inserted;
#   * an existing line is never edited, reordered or deleted;
#   * the version column therefore only ever ascends.
#
# WHY THE ROLL LINE. It is a sha256 over every entry line below it. Two branches
# that each add a migration write DIFFERENT roll values, so git refuses to merge
# them and someone has to look — which is the whole point of this file. A plain
# "count=N max=NNNNN" header does NOT do this: when two branches collide on a
# number they write the IDENTICAL summary, and git merges identical changes
# without a word. That was measured, not assumed.
#
# WHY EACH LINE CARRIES A CONTENT HASH. Editing an already-shipped migration is
# invisible to goose (one row per version, never revisited): new installs get the
# edit, upgraded stations never do, and nothing errors. Only the hash sees it.
`

// renderMigrationLock builds the file's text from entries IN FILE ORDER (which
// is insertion order, NOT necessarily sorted — that is what makes an
// out-of-order version column detectable).
func renderMigrationLock(entries []migrationLockEntry) string {
	var body strings.Builder
	for _, e := range entries {
		body.WriteString(e.line())
		body.WriteString("\n")
	}
	return migrationLockHeader + migrationLockRollPrefix + sha256Hex([]byte(body.String())) + "\n" + body.String()
}

// parsedMigrationLock is what a lock file says, before any judgement is passed
// on it.
type parsedMigrationLock struct {
	roll    string
	entries []migrationLockEntry // in FILE order
	lines   []string             // the raw entry lines, in file order
}

func parseMigrationLock(text string) (parsedMigrationLock, error) {
	var out parsedMigrationLock
	seenRoll := false
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !seenRoll {
			if !strings.HasPrefix(line, migrationLockRollPrefix) {
				return out, fmt.Errorf("line %d: the first non-comment line must be %q, got %q",
					n+1, migrationLockRollPrefix+"<hex>", line)
			}
			out.roll = strings.TrimPrefix(line, migrationLockRollPrefix)
			seenRoll = true
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return out, fmt.Errorf("line %d: want `<version> <path> %s<hex>`, got %q",
				n+1, migrationLockHashPrefix, line)
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return out, fmt.Errorf("line %d: %q is not a version number: %v", n+1, fields[0], err)
		}
		if !strings.HasPrefix(fields[2], migrationLockHashPrefix) {
			return out, fmt.Errorf("line %d: %q is not %s<hex>", n+1, fields[2], migrationLockHashPrefix)
		}
		out.entries = append(out.entries, migrationLockEntry{
			version: v,
			path:    fields[1],
			sha:     strings.TrimPrefix(fields[2], migrationLockHashPrefix),
		})
		out.lines = append(out.lines, line)
	}
	if !seenRoll {
		return out, fmt.Errorf("no %q line: the lock has no roll hash, which is the half that makes "+
			"two branches conflict instead of merging clean", migrationLockRollPrefix+"<hex>")
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// THE JUDGEMENT — pure, so it can be driven with corpora that are actually WRONG.
// A detector only ever run on correct input is indistinguishable from one that
// returns nil.
// ---------------------------------------------------------------------------

// Finding tags. Each judgement carries a stable tag so that a test proving "this
// mutation is caught" can assert WHICH check caught it, rather than settling for
// "something went red" — a mutant covered by a neighbouring assertion proves
// nothing about the assertion it was aimed at.
const (
	findParse   = "[lock:parse]"
	findRoll    = "[lock:roll]"
	findOrder   = "[lock:order]"
	findDup     = "[lock:dup]"
	findMissing = "[lock:missing]"
	findExtra   = "[lock:extra]"
	findContent = "[lock:content]"
	// findPathMoved is the T-64 diagnosis, carried over here when this guard's
	// teardown removed the file that used to make it: a version the lock and the
	// tree BOTH hold, at two different paths. On the listing alone a RENAME and a
	// COLLISION are identical, and the two need opposite fixes.
	findPathMoved = "[lock:path]"
)

// migrationLockFindings is the whole class-A judgement: does this lock text
// honestly describe this set of migrations?
func migrationLockFindings(lockText string, tree []migrationLockEntry) []string {
	parsed, err := parseMigrationLock(lockText)
	if err != nil {
		return []string{fmt.Sprintf("%s %s cannot be read: %v. Regenerate it with "+
			"bin/gen-migration-lock rather than repairing it by hand.", findParse, migrationLockFile, err)}
	}
	var findings []string

	// ── the roll hash ────────────────────────────────────────────────────────
	var body strings.Builder
	for _, l := range parsed.lines {
		body.WriteString(l)
		body.WriteString("\n")
	}
	if want := sha256Hex([]byte(body.String())); want != parsed.roll {
		findings = append(findings, fmt.Sprintf("%s the roll line says sha256:%s but the %d entry "+
			"lines below it hash to sha256:%s. Either a line was hand-edited without regenerating, "+
			"or a merge resolution kept one side's roll and the other side's lines. Run "+
			"bin/gen-migration-lock. (The roll line is not decoration: it is what makes two "+
			"branches adding a migration CONFLICT instead of merging clean.)",
			findRoll, parsed.roll, len(parsed.lines), want))
	}

	// ── append-only shape: the version column only ascends ───────────────────
	// This is the tree-internal half of "a new migration may only be numbered
	// above every released one". The generator APPENDS, so a migration numbered
	// below the current maximum lands at the tail with a smaller number and the
	// column stops ascending — visible here with no baseline at all.
	for i := 1; i < len(parsed.entries); i++ {
		prev, cur := parsed.entries[i-1], parsed.entries[i]
		if cur.version <= prev.version {
			findings = append(findings, fmt.Sprintf("%s %s is append-only, so its version column "+
				"must ascend, but line %d holds version %d after version %d (%s after %s). A "+
				"migration numbered at or below one already in the list is the failure this rule "+
				"exists for: goose records versions monotonically and refuses to run a version it "+
				"skipped past, so a station already upgraded past %d will REFUSE TO START on this "+
				"tree — `found 1 missing migrations before current version …`. Renumber it to "+
				"max+1 and regenerate.",
				findOrder, migrationLockFile, i+1, cur.version, prev.version, cur.path, prev.path, cur.version))
		}
	}

	// ── duplicates inside the lock itself ────────────────────────────────────
	seenVersion := map[int64]string{}
	seenPath := map[string]bool{}
	for _, e := range parsed.entries {
		if other, ok := seenVersion[e.version]; ok {
			findings = append(findings, fmt.Sprintf("%s version %d is listed twice in %s (%s and "+
				"%s). goose panics on a duplicate version from inside goose.Up — on a station, "+
				"during an upgrade, as a stack trace about two files nobody there wrote.",
				findDup, e.version, migrationLockFile, other, e.path))
		}
		seenVersion[e.version] = e.path
		if seenPath[e.path] {
			findings = append(findings, fmt.Sprintf("%s %s is listed twice in %s.",
				findDup, e.path, migrationLockFile))
		}
		seenPath[e.path] = true
	}

	// ── the lock versus the tree ─────────────────────────────────────────────
	lockByPath := map[string]migrationLockEntry{}
	for _, e := range parsed.entries {
		lockByPath[e.path] = e
	}
	treeByPath := map[string]migrationLockEntry{}
	for _, e := range tree {
		treeByPath[e.path] = e
	}
	var treePaths []string
	for p := range treeByPath {
		treePaths = append(treePaths, p)
	}
	sort.Strings(treePaths)
	for _, p := range treePaths {
		te := treeByPath[p]
		le, ok := lockByPath[p]
		if !ok {
			findings = append(findings, fmt.Sprintf("%s this tree ships migration %d (%s) and %s "+
				"does not list it. A migration added — or a file RENAMED — without regenerating the "+
				"lock leaves the lock describing a set that no longer exists, and the lock is the "+
				"only thing that makes a second branch adding the same number collide with you. "+
				"Run bin/gen-migration-lock.", findMissing, te.version, p, migrationLockFile))
			continue
		}
		if le.version != te.version {
			findings = append(findings, fmt.Sprintf("%s %s is version %d in the tree and version "+
				"%d in %s.", findContent, p, te.version, le.version, migrationLockFile))
		}
		if le.sha != te.sha {
			// 🔴 THE MECHANICAL-EDIT ARM, carried over verbatim in substance from
			// the T-64 upgrade-path guard that used to say this and has been torn
			// down. A Go migration shares a package with everything else in it, so
			// a repo-wide rename or a formatting pass reaches it whether or not
			// anyone meant to touch a shipped migration. This check cannot tell
			// that apart from a behaviour change — so it must not pretend to, and
			// it must say so, because ONE OF THE TWO CASES HAS NO OUT AT ALL.
			mechanical := ""
			if !strings.HasSuffix(p, ".sql") {
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
			findings = append(findings, fmt.Sprintf("%s the contents of %s changed: %s records "+
				"sha256:%s, the tree has sha256:%s. If this migration has already SHIPPED, this is "+
				"the silent-schema-fork case and the edit must be reverted: goose writes one row "+
				"per version and never runs it again, so the new bytes reach fresh installs only "+
				"— every station that already upgraded keeps the old schema and NOTHING ANYWHERE "+
				"ERRORS. Add a new migration instead. If it has not shipped, regenerate with "+
				"bin/gen-migration-lock.%s", findContent, p, migrationLockFile, le.sha, te.sha,
				mechanical))
		}
	}
	var lockPaths []string
	for p := range lockByPath {
		lockPaths = append(lockPaths, p)
	}
	sort.Strings(lockPaths)
	for _, p := range lockPaths {
		if _, ok := treeByPath[p]; !ok {
			findings = append(findings, fmt.Sprintf("%s %s lists migration %d (%s) and this tree "+
				"does not have it — it was deleted or renamed. A shipped migration cannot be "+
				"removed: stations that ran it are recorded at that version and goose refuses to "+
				"run past a version it can no longer find. Restore the file, or (if it never "+
				"shipped) regenerate with bin/gen-migration-lock.",
				findExtra, migrationLockFile, lockByPath[p].version, p))
		}
	}

	// ── RENAME versus COLLISION ──────────────────────────────────────────────
	// 🔴 Carried over from the T-64 upgrade-path guard (measured 2026-09-04),
	// which is the file that used to say this and has been torn down. The two
	// loops above have already reported the halves — the lock's path as
	// [lock:extra], the tree's path as [lock:missing] — and for ONE of the two
	// shapes below those two findings are the SAME LISTING for two events that
	// need OPPOSITE FIXES:
	//
	//   RENAME — this tree once had the lock's file and moved it. goose
	//   identifies a Go migration by the string passed to AddNamedMigration*
	//   rather than by the filename, so a rename leaves the version declared
	//   while the path the lock knows is gone. FIX: put the file back.
	//
	//   COLLISION — two branches independently took the same free number. FIX:
	//   renumber THIS tree's file upward.
	//
	// 🔴 THE COST OF GUESSING IS NOT SYMMETRIC. "Put the file back where the lock
	// has it" told at a COLLISION is an instruction to overwrite a migration
	// somebody else has already shipped — the exact failure this whole mechanism
	// exists to prevent. Guessing the other way only wastes a round trip.
	//
	// 🔴 SO THE FIRST JOB IS TO ASK WHETHER THE TWO ARE ACTUALLY AMBIGUOUS, AND
	// USUALLY THEY ARE NOT. The discriminator is IS THE LOCK'S PATH STILL IN THE
	// TREE?, and it is available right here without asking git:
	//
	//   lock's path IS in the tree  ⇒ nothing was moved, the file is sitting
	//   right there. Both paths exist and both claim the number: this is a
	//   COLLISION, stated flatly. No RENAME arm, because there is no rename.
	//
	//   lock's path is NOT in the tree ⇒ the file is gone from where the lock
	//   says it is. NOW the two are genuinely indistinguishable from the listing
	//   and both arms are printed, with the one command that separates them.
	//
	// ⚠️ MEASURED, and it is why the flat "print both" version of this was wrong:
	// the reachable shape is a CROSS-SOURCE collision — main ships a Go migration
	// at NNNNN, a branch that predates it adds a .sql at NNNNN, and the merge is
	// CLEAN (a branch with no lock of its own takes main's as a new file, no
	// conflict, nothing stops you). The tree then holds BOTH files and the lock
	// lists only main's. "Print both arms" hands that reader a `git log` on main's
	// Go file, which of course has commits, which reads as RENAME — i.e. as "put
	// main's shipped migration back", the expensive wrong half.
	//
	// ⚠️ A version the lock lists MORE THAN ONCE is skipped here: [lock:dup] above
	// already names both of its lines exactly, and a second finding built from a
	// map that can only hold one path per version would pair the wrong two.
	lockPathsByVersion := map[int64][]string{}
	for _, e := range parsed.entries {
		lockPathsByVersion[e.version] = append(lockPathsByVersion[e.version], e.path)
	}
	nextFree := int64(0)
	for _, e := range parsed.entries {
		if e.version >= nextFree {
			nextFree = e.version + 1
		}
	}
	for _, e := range tree {
		if e.version >= nextFree {
			nextFree = e.version + 1
		}
	}
	for _, p := range treePaths {
		te := treeByPath[p]
		lps := lockPathsByVersion[te.version]
		if len(lps) != 1 || lps[0] == te.path {
			continue // not claimed twice, or claimed by this very path
		}
		lp := lps[0]
		if _, lockPathStillHere := treeByPath[lp]; lockPathStillHere {
			findings = append(findings, fmt.Sprintf("%s version %d is claimed TWICE in this tree: "+
				"%s lists it at %s, that file is STILL HERE, and %s claims the same number. THIS "+
				"IS A COLLISION, not a rename — nothing was moved, both files exist. It is what "+
				"two branches taking the same free number looks like after they meet, and the "+
				"merge that produced it can be completely clean: a branch with no lock of its own "+
				"takes the other side's lock as a NEW FILE, so git never conflicts and nothing "+
				"makes you stop and look.\n"+
				"  FIX: renumber THIS tree's newer file (%s) to %d — above every number either "+
				"side declares — and then run bin/gen-migration-lock.\n"+
				"  DO NOT touch %s to resolve this. It is what the lock already records, which "+
				"means it is what has already shipped; goose writes one row per version and never "+
				"revisits it, so editing or moving it reaches nobody who has already upgraded and "+
				"silently forks the schema. Left alone, the pair is worse: goose PANICS on a "+
				"duplicate version from inside goose.Up — on a station, during an upgrade, as a "+
				"stack trace about two files nobody there wrote.",
				findPathMoved, te.version, migrationLockFile, lp, te.path, te.path, nextFree, lp))
			continue
		}
		findings = append(findings, fmt.Sprintf("%s version %d is at %s in %s and at %s in this "+
			"tree, AND %s is not in this tree at all. TWO DIFFERENT EVENTS LOOK LIKE THIS AND "+
			"THEY NEED OPPOSITE FIXES, so this does not name one of them and stop.\n"+
			"  RENAME — this tree once had %s and moved it. goose identifies a Go migration by "+
			"the string passed to AddNamedMigration* rather than by the filename, so a rename "+
			"leaves the version declared while the path the lock knows is gone. FIX: put the file "+
			"back at %s. A released migration's path is part of what shipped; if the path "+
			"genuinely has to change, that is a decision for a person, not something to route "+
			"around here.\n"+
			"  COLLISION — %s was NEVER in this tree: another branch took version %d and landed "+
			"first. FIX: renumber THIS tree's file (%s) to %d and regenerate with "+
			"bin/gen-migration-lock. DO NOT put %s 'back': it was never here, and writing your "+
			"version of %d over a migration that has already shipped is the failure this whole "+
			"check exists to prevent.\n"+
			"  WHICH ONE THIS IS cannot be read off the listing once the lock's file is gone. "+
			"Tell them apart by hand, from anywhere in the repo: `git log HEAD -- ':(top)%s'`. "+
			"Commits ⇒ RENAME. Nothing ⇒ COLLISION.\n"+
			"  The ':(top)' is LOAD-BEARING and the quotes are why it survives your shell. "+
			"Without it git resolves the path against your CWD — and you are standing in this "+
			"package's directory, because that is where this check runs — so the lookup "+
			"silently finds NOTHING and EVERY case reads as a COLLISION, renames included. That "+
			"is not hypothetical: T-64 shipped exactly this bug (4a6a537a) and its rename arm "+
			"reported that a file with two commits on it had NEVER existed in this tree.",
			findPathMoved, te.version, lp, migrationLockFile, te.path, lp, lp, lp, lp,
			te.version, te.path, nextFree, lp, te.version, migrationLockPkgRepoPath+lp))
	}
	return findings
}

// migrationLockPrefixFindings is the CLASS B judgement: main's entry lines must
// be an exact prefix of this tree's. Pure, for the same reason as above.
//
// ⚠️ Its input on main is two copies of the same lines, so it can only ever
// return nil there. That is not a defect to be fixed — it is what "compare with
// the baseline" means — but it is the reason this check must never be described
// as covering deletion or renumbering in general.
func migrationLockPrefixFindings(mainLines, treeLines []string) []string {
	var findings []string
	if len(treeLines) < len(mainLines) {
		findings = append(findings, fmt.Sprintf("%s origin/main's %s has %d entries and this tree "+
			"has %d — either lines were REMOVED (every line is a migration some station has "+
			"already run; the list may only grow), or this branch is simply BEHIND main and has "+
			"not merged it yet. Check that second one FIRST when you are running locally: it is "+
			"by far the likelier of the two and it looks identical from here. `git merge "+
			"origin/main`, then `bin/gen-migration-lock`.",
			findExtra, migrationLockFile, len(mainLines), len(treeLines)))
	}
	n := len(mainLines)
	if len(treeLines) < n {
		n = len(treeLines)
	}
	for i := 0; i < n; i++ {
		if mainLines[i] != treeLines[i] {
			findings = append(findings, fmt.Sprintf("%s %s line %d differs from origin/main's:\n"+
				"  main: %s\n  here: %s\n"+
				"Lines already on main describe migrations that have already been released. "+
				"Changing one means either a shipped migration's contents were edited (invisible "+
				"to every station that already upgraded — see the content note in this file) or a "+
				"new migration was INSERTED into the middle of the list, which puts it below the "+
				"released maximum and stops upgraded stations from booting. New migrations are "+
				"appended at the END, numbered above every line already here.",
				findOrder, migrationLockFile, i+1, mainLines[i], treeLines[i]))
			break // the first divergence is the finding; the rest is its shadow
		}
	}
	return findings
}

// ---------------------------------------------------------------------------
// THE SUBCOMMAND
// ---------------------------------------------------------------------------

// migrationLockWriteMarker is the line `--write` prints as its LAST act, and the
// line bin/gen-migration-lock requires before believing anything happened.
//
// 🔴 WHY A MARKER AND NOT rc. This was learned the expensive way when the writer
// was a `go test -run` invocation: a -run pattern matching NOTHING prints `ok`
// and exits 0, byte for byte what a real pass looks like. The subcommand form
// removes that particular trap, but the class survives it — a body replaced by
// an early return exits 0 just the same — so the wrapper still refuses to
// believe rc alone, and this is what it believes instead.
const migrationLockWriteMarker = "[gen-migration-lock] wrote"

// migrationLockNextContents produces the lock's lines IN FILE ORDER: every line
// the current lock already has, in the position it already has (with the tree's
// current hash), followed by everything new, appended.
//
// 🔴 WHY IT APPENDS INSTEAD OF SORTING, which is the part that looks like a bug
// until you see what it catches. Rewriting the file in version order every time
// would be simpler AND would destroy the append-only signal: a migration
// numbered below the current maximum would sort quietly into the middle and the
// version column would still ascend. By keeping the existing lines where they
// are and putting new ones at the END, a below-maximum migration lands at the
// tail with a smaller number — and the ascending-column check ([lock:order])
// sees it WITH NO BASELINE, i.e. on main too, not only on a PR.
func migrationLockNextContents(tree []migrationLockEntry) ([]migrationLockEntry, error) {
	byPath := map[string]migrationLockEntry{}
	for _, e := range tree {
		byPath[e.path] = e
	}
	var ordered []migrationLockEntry
	kept := map[string]bool{}

	if raw, err := os.ReadFile(migrationLockFile); err == nil {
		existing, perr := parseMigrationLock(string(raw))
		if perr != nil {
			return nil, fmt.Errorf("the existing %s does not parse (%v). Refusing to regenerate over "+
				"a file this tool cannot read: it would silently discard whatever ordering the file "+
				"still held, and that ordering IS the append-only record. Fix or delete it "+
				"deliberately.", migrationLockFile, perr)
		}
		for _, e := range existing.entries {
			cur, ok := byPath[e.path]
			if !ok {
				// The migration is gone from the tree. The lock follows the tree —
				// the judgement about whether a shipped migration may disappear
				// belongs to the checker (class B, against origin/main), not here.
				continue
			}
			ordered = append(ordered, cur) // same position, CURRENT hash
			kept[e.path] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", migrationLockFile, err)
	}

	// tree is sorted by version, so first-time generation comes out ascending and
	// later additions append in version order.
	for _, e := range tree {
		if !kept[e.path] {
			ordered = append(ordered, e)
		}
	}
	return ordered, nil
}

// cmdMigrationLock is the `migration-lock` subcommand: --write regenerates the
// lock, --check re-derives it and refuses anything that does not match.
//
// 🔴 ONE ENUMERATION, BOTH MODES. Both arms below start from the same
// migrationLockTreeEntries call and render through the same renderMigrationLock.
// A second implementation for the checking side is the failure this whole design
// is built to avoid: it would validate a different corpus than the one that was
// written, and it would be green while doing it.
func cmdMigrationLock(args []string, out io.Writer) int {
	mode := ""
	for _, a := range args {
		switch a {
		case "--write", "-write":
			mode = "write"
		case "--check", "-check":
			mode = "check"
		default:
			fmt.Fprintf(out, "[migration-lock] unknown argument %q — want exactly one of --write or --check\n", a)
			return 2
		}
	}
	if mode == "" {
		fmt.Fprintln(out, "[migration-lock] needs exactly one of --write or --check.")
		fmt.Fprintln(out, "  --write  regenerate "+migrationLockFile+" from this tree")
		fmt.Fprintln(out, "  --check  re-derive it and fail if the committed file does not match")
		fmt.Fprintln(out, "Run it from "+migrationLockPkgRepoPath+", or through bin/gen-migration-lock /")
		fmt.Fprintln(out, "bin/check-migration-lock, which do that for you.")
		return 2
	}

	tree, err := migrationLockTreeEntries()
	if err != nil {
		fmt.Fprintf(out, "[migration-lock] cannot enumerate this tree's migrations: %v\n", err)
		return 1
	}

	if mode == "write" {
		ordered, err := migrationLockNextContents(tree)
		if err != nil {
			fmt.Fprintf(out, "[migration-lock] %v\n", err)
			return 1
		}
		if err := os.WriteFile(migrationLockFile, []byte(renderMigrationLock(ordered)), 0o644); err != nil {
			fmt.Fprintf(out, "[migration-lock] write %s: %v\n", migrationLockFile, err)
			return 1
		}
		fmt.Fprintf(out, "%s %s: %d entries\n", migrationLockWriteMarker, migrationLockFile, len(ordered))
		return 0
	}

	raw, err := os.ReadFile(migrationLockFile)
	if err != nil {
		fmt.Fprintf(out, "[migration-lock] read %s: %v\n", migrationLockFile, err)
		fmt.Fprintln(out, "The lock is what makes two branches adding a migration collide at merge time")
		fmt.Fprintln(out, "instead of on main's CI after the release went out. Its absence is a FAILURE,")
		fmt.Fprintln(out, "never a pass. Create it with bin/gen-migration-lock.")
		return 1
	}
	findings := migrationLockFindings(string(raw), tree)
	if len(findings) > 0 {
		fmt.Fprintf(out, "FAIL — %s does not describe this tree (%d finding(s)):\n\n", migrationLockFile, len(findings))
		for _, f := range findings {
			fmt.Fprintf(out, "%s\n\n", f)
		}
		return 1
	}
	// The byte comparison, on top of the findings. The findings answer "is the
	// lock's CLAIM true"; this answers "would regenerating produce these exact
	// bytes" — the same question every other drift gate in this repo asks, and
	// the one that catches a difference the judgement has no arm for (a header
	// edit, a stray blank line, a changed line ending).
	ordered, err := migrationLockNextContents(tree)
	if err != nil {
		fmt.Fprintf(out, "[migration-lock] %v\n", err)
		return 1
	}
	if want := renderMigrationLock(ordered); want != string(raw) {
		fmt.Fprintf(out, "FAIL — %s is not byte-identical to what regenerating produces, and the\n", migrationLockFile)
		fmt.Fprintln(out, "per-entry judgement above found nothing — so the difference is OUTSIDE the entry")
		fmt.Fprintln(out, "lines (the header, a blank line, a line ending) or in their ORDER.")
		fmt.Fprint(out, migrationLockTextDiff(string(raw), want))
		fmt.Fprintln(out, "FIX: bin/gen-migration-lock, then review the diff and commit it.")
		return 1
	}
	fmt.Fprintf(out, "[migration-lock] OK — %s matches this tree exactly (%d entries).\n",
		migrationLockFile, len(ordered))
	return 0
}

// migrationLockTextDiff reports the first line where two texts part company,
// with a little context. Not a general diff: the caller has already established
// that the two are supposed to be identical, so the FIRST divergence is the
// finding and everything after it is that finding's shadow.
func migrationLockTextDiff(have, want string) string {
	haveLines, wantLines := strings.Split(have, "\n"), strings.Split(want, "\n")
	n := len(haveLines)
	if len(wantLines) < n {
		n = len(wantLines)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  committed: %d line(s)\n  regenerated: %d line(s)\n", len(haveLines), len(wantLines))
	for i := 0; i < n; i++ {
		if haveLines[i] != wantLines[i] {
			fmt.Fprintf(&b, "  first difference at line %d:\n    committed:   %q\n    regenerated: %q\n",
				i+1, haveLines[i], wantLines[i])
			return b.String()
		}
	}
	fmt.Fprintf(&b, "  the first %d line(s) are identical; the files differ only in length.\n", n)
	return b.String()
}
