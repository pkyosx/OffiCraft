package main

// migration.lock exists to TURN A CLEAN MERGE INTO A CONFLICT. Two PRs that each
// add migration 00072 touch different files, so git merges both silently
// (measured); a check only speaks when something runs it, so the collision first
// shows on the merged main's CI — after the merge button, and merging main is how
// a release goes out. The lock is a place where such branches write the same
// bytes. Its shape was measured on real two-branch merges:
//
//	both branches append at the file's END .............. CONFLICT  ← what we want
//	branches write lines 50 / 3 / 1 apart .............. auto-merged, silently
//	header carries "count=65 max=00071" ................ auto-merged: a collision
//	                                                     makes BOTH sides write
//	                                                     the IDENTICAL summary, and
//	                                                     git never conflicts on an
//	                                                     identical change
//	header carries a HASH OF THE WHOLE LIST ............ CONFLICT
//	.sql in one section, Go in another .................. auto-merged
//
// Hence ONE flat list, .sql and Go mixed, appended at the tail only, with a roll
// hash of the whole list on the header line.
//
// TWO CLASSES OF JUDGEMENT:
//
//	CLASS A — migrationLockFindings: the lock against THIS tree, no baseline, so
//	alive on main too. This is what `migration-lock --check` runs.
//	CLASS B — migrationLockPrefixFindings: main's entry lines must be an exact
//	prefix of this tree's. ⚠️ ON MAIN IT IS A NO-OP (both sides are the same
//	commit); it is a PR-path check only.
//	Regenerating the lock after editing a shipped migration turns class A green;
//	only class B says the edit was not allowed, and never on main.
//
// The .sql bytes come from the embedded FS goose is actually handed, not the
// working directory: a .sql on disk that the embed pattern misses would otherwise
// be certified by the lock yet never run by the server.

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

const migrationLockFile = "migration.lock"

const (
	migrationLockPkgRepoPath = "server/ocserverd/"
	migrationLockRepoPath    = migrationLockPkgRepoPath + migrationLockFile
	// migrationLockGuardRepoPath is THIS file: the anchor that arms the "main has
	// the mechanism but not the lock" diagnosis.
	migrationLockGuardRepoPath = migrationLockPkgRepoPath + "migration_lock.go"
)

const (
	migrationLockRollPrefix = "roll sha256:"
	migrationLockHashPrefix = "sha256:"
)

// Anti-vacuity floors: they catch an enumeration gone BLIND (wrong directory,
// glob or embed pattern), not the count — keep them well below the real tree and
// do not bump them per migration.
const (
	migrationLockMinSQL = 40
	migrationLockMinGo  = 2
)

type migrationLockEntry struct {
	version int64
	path    string
	sha     string
}

func (e migrationLockEntry) line() string {
	return fmt.Sprintf("%05d %s %s%s", e.version, e.path, migrationLockHashPrefix, e.sha)
}

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

func inCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != "" || os.Getenv("CI") != ""
}

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
				return true
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
	if files < 20 {
		return nil, fmt.Errorf("the AST scan read %d files in this package — that corpus is too "+
			"small to be the real one, so a finding of zero registrations would mean nothing. "+
			"Run this from %s", files, migrationLockPkgRepoPath)
	}
	return found, nil
}

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

func migrationLockTreeEntries() ([]migrationLockEntry, error) {
	var entries []migrationLockEntry

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

	located, err := registrarLocations()
	if err != nil {
		return nil, err
	}
	if len(located) < migrationLockMinGo {
		return nil, fmt.Errorf("the AST scan found %d Go migration registrations (floor %d) — this "+
			"repo has had Go migrations since 00054, so a smaller answer means the scan went blind, "+
			"not that they were removed", len(located), migrationLockMinGo)
	}
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

// migrationLockHeader is FROZEN TEXT: `--check` byte-compares, so any edit here —
// including fixing its stale test-file name — turns every checkout red until the
// lock is regenerated and committed. Do it deliberately, not folded into another
// change.
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

func renderMigrationLock(entries []migrationLockEntry) string {
	var body strings.Builder
	for _, e := range entries {
		body.WriteString(e.line())
		body.WriteString("\n")
	}
	return migrationLockHeader + migrationLockRollPrefix + sha256Hex([]byte(body.String())) + "\n" + body.String()
}

type parsedMigrationLock struct {
	roll    string
	entries []migrationLockEntry
	lines   []string
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

const (
	findParse   = "[lock:parse]"
	findRoll    = "[lock:roll]"
	findOrder   = "[lock:order]"
	findDup     = "[lock:dup]"
	findMissing = "[lock:missing]"
	findExtra   = "[lock:extra]"
	findContent = "[lock:content]"

	findPathMoved = "[lock:path]"
)

func migrationLockFindings(lockText string, tree []migrationLockEntry) []string {
	parsed, err := parseMigrationLock(lockText)
	if err != nil {
		return []string{fmt.Sprintf("%s %s cannot be read: %v. Regenerate it with "+
			"bin/gen-migration-lock rather than repairing it by hand.", findParse, migrationLockFile, err)}
	}
	var findings []string

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
	// The split on "is the lock's path still in the tree?" is deliberate: the
	// guessing cost is not symmetric. Measured: a clean cross-source merge (main's
	// Go migration and a branch's .sql at the same number) leaves both files in the
	// tree; printing both arms there sends the reader to `git log` main's Go file,
	// which reads as RENAME — i.e. "put main's shipped migration back", the
	// expensive wrong fix.
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
			continue
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
			break
		}
	}
	return findings
}

// migrationLockWriteMarker must be the LAST line `--write` prints:
// bin/gen-migration-lock requires this exact string before believing a write
// happened, because rc alone lies (a body replaced by an early return exits 0).
const migrationLockWriteMarker = "[gen-migration-lock] wrote"

// migrationLockNextContents keeps every existing line in place and APPENDS new
// ones instead of sorting. Sorting looks simpler but would destroy the signal: a
// below-maximum migration would sort quietly into the middle, whereas appended it
// lands at the tail with a smaller number and [lock:order] sees it with no
// baseline.
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
				// Gone from the tree: the lock follows the tree; whether a shipped
				// migration may disappear is class B's judgement, not the generator's.
				continue
			}
			ordered = append(ordered, cur)
			kept[e.path] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", migrationLockFile, err)
	}

	for _, e := range tree {
		if !kept[e.path] {
			ordered = append(ordered, e)
		}
	}
	return ordered, nil
}

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
