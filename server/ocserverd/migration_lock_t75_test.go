package main

// migration_lock_t75_test.go — T-75: migration.lock, the file whose job is to
// TURN A CLEAN MERGE INTO A CONFLICT.
//
// 🔴 WHY THIS EXISTS, AND WHY NO TEST COULD HAVE DONE IT.
// Two PRs each add a migration numbered 00072. They touch DIFFERENT FILES, so
// git merges both without a word — measured, rc=0, no conflict markers. Every
// migration guard in this package is a TEST, and a test only speaks when
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
// 🔴 THE TWO CLASSES OF CHECK IN THIS FILE, AND THE HONEST DIFFERENCE BETWEEN
// THEM. Read this before trusting any green here.
//
//	CLASS A — TREE-INTERNAL (TestMigrationLockMatchesTheTree). The lock is
//	compared against THIS tree's own migrations. It needs no baseline, so it is
//	just as alive on main as on a PR. It answers: "does the lock describe the
//	tree it sits in?" — i.e. added / renamed / deleted / edited a migration and
//	did not regenerate the lock; hand-edited a line; a middle insertion that the
//	generator appended (which shows up as a version column that stops ascending).
//
//	CLASS B — AGAINST ORIGIN/MAIN (TestMigrationLockGrowsOnlyAtItsTail). The
//	prefix rule: main's entry lines must be an exact prefix of this tree's. It
//	answers "was anything already released changed or removed, or inserted below
//	the released maximum?" — and it can only answer that by comparing with a
//	baseline. ⚠️ ON MAIN ITSELF THE TWO SIDES ARE THE SAME COMMIT, SO IT IS A
//	NO-OP THERE. It is a PR-path check and nothing more; it is stated here rather
//	than left for a reader to discover, because a guard believed to cover more
//	than it does is worse than no guard.
//
//	The consequence, spelled out: regenerate the lock after editing a shipped
//	migration and class A goes green — the lock now honestly describes the tree.
//	Only class B says that the edit was not allowed, and it will never say it on
//	main. (Until T-75 there was a second sayer, the T-64 upgrade-path guard that
//	replayed main's bytes into a station; it was removed once every mutation it
//	caught was measured to be caught here too.)
//
// 🔴 THE ENUMERATION IS SHARED ON PURPOSE — this is the failure this file was
// most likely to ship. If the generator listed migrations one way and the
// checker listed them another, the checker would be validating a different
// corpus than the one that was written, and it would be GREEN while doing it.
// So there is exactly one enumerator, migrationLockTreeEntries, and both the
// writer (TestWriteMigrationLock, driven by bin/gen-migration-lock) and the
// reader call it. It carries its own anti-vacuity floor, because an enumeration
// that returned nothing would make every set-comparison below trivially true.
//
// 🔴 WHERE THE .sql BYTES COME FROM. The embedded FS (embeddedMigrations), not
// the working directory — the same corpus goose is actually handed in
// runMigrations. A .sql sitting on disk that the embed pattern does not match
// would otherwise be in the lock and invisible to goose, and the lock would
// certify a set the server never runs. The Go migrations are read from the
// package directory on disk instead, because that IS their authority: what the
// compiler links is what registers with goose.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// migrationLockFile is the lock's path, relative to this package directory —
// which is also `go test`'s working directory for this package.
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
// change exists to kill.
const (
	// migrationLockPkgRepoPath is this package's directory FROM THE REPO ROOT.
	// It is load-bearing in the [lock:path] diagnosis below: every path in the
	// lock is relative to this directory, so a `git log … -- <lock path>` handed
	// to a reader standing at the repo root silently finds NOTHING and turns
	// every rename into a "collision" — measured on T-64, which hit exactly this
	// and fixed it with a `:(top)` pathspec.
	migrationLockPkgRepoPath   = "server/ocserverd/"
	migrationLockRepoPath      = migrationLockPkgRepoPath + migrationLockFile
	migrationLockGuardRepoPath = migrationLockPkgRepoPath + "migration_lock_t75_test.go"
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
// THE SHARED ENUMERATION — one function, used by both the writer and the reader.
// ---------------------------------------------------------------------------

// migrationLockTreeEntries lists every migration this tree ships, from both
// sources, sorted by version. This is the ONLY enumeration in this file: the
// generator writes what it returns and the checker compares against what it
// returns, so the two can never be describing different corpora.
func migrationLockTreeEntries(t *testing.T) []migrationLockEntry {
	t.Helper()
	var entries []migrationLockEntry

	// Source ① — the .sql files, out of the EMBEDDED FS (see the header note).
	sqlFiles, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	for _, f := range sqlFiles {
		v, err := goose.NumericComponent(f)
		if err != nil {
			t.Fatalf("%s has no version prefix goose can read: %v", f, err)
		}
		body, err := fs.ReadFile(embeddedMigrations, f)
		if err != nil {
			t.Fatalf("read embedded %s: %v", f, err)
		}
		entries = append(entries, migrationLockEntry{version: v, path: f, sha: sha256Hex(body)})
	}
	if len(sqlFiles) < migrationLockMinSQL {
		t.Fatalf("the embedded FS yielded %d .sql migrations (floor %d) — a corpus that small is "+
			"not this repo's, so every set-comparison against it below would be trivially true",
			len(sqlFiles), migrationLockMinSQL)
	}

	// Source ② — the Go migrations. registrarLocations is the AST walk the
	// duplicate-version guard already owns (a grep would match this file's own
	// prose); it returns version -> "file.go:line", and the file is what we hash.
	located := registrarLocations(t)
	if len(located) < migrationLockMinGo {
		t.Fatalf("the AST scan found %d Go migration registrations (floor %d) — this repo has had "+
			"Go migrations since 00054, so a smaller answer means the scan went blind, not that "+
			"they were removed", len(located), migrationLockMinGo)
	}
	for v, where := range located {
		file := where
		if i := strings.LastIndex(where, ":"); i >= 0 {
			file = where[:i]
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read Go migration %s (version %d): %v", file, v, err)
		}
		entries = append(entries, migrationLockEntry{version: v, path: file, sha: sha256Hex(body)})
	}

	for _, e := range entries {
		if strings.ContainsAny(e.path, " \t") {
			t.Fatalf("migration path %q contains whitespace — the lock is space-separated and "+
				"would silently mis-parse it", e.path)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].version != entries[j].version {
			return entries[i].version < entries[j].version
		}
		return entries[i].path < entries[j].path
	})
	return entries
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// RENDER / PARSE
// ---------------------------------------------------------------------------

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
			"Tell them apart by hand, from the REPO ROOT (not this package dir, or every lookup "+
			"silently finds nothing and every case reads as a collision): `git log HEAD -- %s`. "+
			"Commits ⇒ RENAME. Nothing ⇒ COLLISION.",
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
// THE LIVE CHECKS
// ---------------------------------------------------------------------------

// TestMigrationLockMatchesTheTree is CLASS A: the lock against the migrations
// sitting next to it. No baseline, so this one is as alive on main as on a PR.
//
// Red when: a migration was added, renamed, deleted or edited without
// regenerating the lock; a line was hand-edited; the roll hash is stale; the
// version column stopped ascending.
func TestMigrationLockMatchesTheTree(t *testing.T) {
	tree := migrationLockTreeEntries(t)

	// 🔴 POSITIVE CONTROL on the shared enumeration. Both sources must hit
	// something that is in this tree today, or the set-comparisons in the
	// judgement would be statements about an empty (or half-empty) set. 00001 is
	// the schema itself and 00054 is this repo's first Go migration; a landed
	// migration never moves, which is the whole premise of the lock.
	var sawSQL, sawGo bool
	for _, e := range tree {
		if e.path == "migrations/00001_schema.sql" {
			sawSQL = true
		}
		if e.version == 54 && strings.HasSuffix(e.path, ".go") {
			sawGo = true
		}
	}
	if !sawSQL {
		t.Fatalf("the shared enumeration did not return migrations/00001_schema.sql, so it is not "+
			"reading the migrations that ship. It returned %d entries.", len(tree))
	}
	if !sawGo {
		t.Fatal("the shared enumeration returned no Go migration for version 54 — it is blind to " +
			"the source that does NOT live under migrations/, which is exactly the half a " +
			"directory listing misses.")
	}

	raw, err := os.ReadFile(migrationLockFile)
	if err != nil {
		t.Fatalf("read %s: %v — the lock is what makes two branches adding a migration collide at "+
			"merge time instead of on main's CI after the release went out. Its absence is a "+
			"failure, never a skip. Create it with bin/gen-migration-lock.", migrationLockFile, err)
	}
	for _, f := range migrationLockFindings(string(raw), tree) {
		t.Error(f)
	}
}

// TestMigrationLockGrowsOnlyAtItsTail is CLASS B: the prefix rule, against
// origin/main.
//
// ⚠️ ON MAIN THIS IS A NO-OP — the two sides are the same commit. It is a
// PR-path check. See the class A/B note at the top of this file.
//
// Red when: a line already on main was changed (a shipped migration was edited,
// or a new one was inserted into the middle of the list) or removed.
func TestMigrationLockGrowsOnlyAtItsTail(t *testing.T) {
	// gitOut / inCI are the T-64 upgrade-path guard's, deliberately reused: a
	// second way of resolving the same baseline is a second thing to drift.
	sha, err := gitOut("rev-parse", "origin/main")
	if err != nil {
		if inCI() {
			t.Fatalf("running in CI but origin/main does not resolve (%v), so the append-only "+
				"half of the lock has no baseline and this run would have been green with the "+
				"check switched off. FIX: the go-checks job's actions/checkout needs "+
				"`fetch-depth: 0`.", err)
		}
		t.Skipf("the append-only half of the migration.lock guard cannot run here: this checkout "+
			"cannot resolve origin/main (%v). It is NOT a pass — there was no baseline. It is "+
			"enforced in CI.", err)
	}
	mainText, err := gitOut("show", "origin/main:"+migrationLockRepoPath)
	if err != nil {
		// Before this change lands, main has no lock. Once it does, a missing one
		// means somebody deleted or moved it — and that is a finding, not a skip.
		//
		// The distinction has to ARM ITSELF. An earlier draft of this branch said
		// it was "made by asking git, not by a flag someone has to flip" and then
		// skipped on any error forever, which is a flag nobody ever flips wearing
		// the words of a check. What arms it is asking git a second question: does
		// main carry THIS GUARD? Before the merge commit it carries neither file,
		// so the skip is honest. From the merge commit onward it carries the
		// guard, and a guard on main with no lock beside it can only mean the lock
		// was removed or moved out from under this hardcoded path.
		if _, e := gitOut("cat-file", "-e", "origin/main:"+migrationLockGuardRepoPath); e == nil {
			t.Fatalf("origin/main (%s) carries %s but NOT %s (%v). The guard is on main, so this "+
				"is no longer the pre-landing state: the lock was deleted, or moved out from "+
				"under the path this check reads. Either way the append-only half has silently "+
				"had no baseline since that happened — restore the file at that path, or move "+
				"BOTH constants in this file together.",
				sha, migrationLockGuardRepoPath, migrationLockRepoPath, err)
		}
		t.Skipf("origin/main (%s) carries neither %s nor this guard (%v), so there is no baseline "+
			"to be a prefix of and nothing has landed yet. It is NOT a pass. This branch is what "+
			"puts both there; from that merge commit on, this same branch fatals instead.",
			sha, migrationLockRepoPath, err)
	}
	mainParsed, err := parseMigrationLock(mainText)
	if err != nil {
		t.Fatalf("origin/main's %s does not parse: %v — main is the baseline, so this is a defect "+
			"in main, not in this branch", migrationLockFile, err)
	}
	// The floor is on main's TOTAL entry count, so it is the sum of both source
	// floors — not migrationLockMinSQL alone, which counts only the .sql half.
	if len(mainParsed.lines) < migrationLockMinSQL+migrationLockMinGo {
		t.Fatalf("origin/main's %s holds %d entries (floor %d) — too few to be the real lock, so "+
			"a prefix match against it would mean nothing", migrationLockFile,
			len(mainParsed.lines), migrationLockMinSQL+migrationLockMinGo)
	}
	raw, err := os.ReadFile(migrationLockFile)
	if err != nil {
		t.Fatalf("read %s: %v", migrationLockFile, err)
	}
	here, err := parseMigrationLock(string(raw))
	if err != nil {
		t.Fatalf("this tree's %s does not parse: %v", migrationLockFile, err)
	}
	t.Logf("baseline: origin/main %s carries %d lock entries; this tree carries %d",
		sha, len(mainParsed.lines), len(here.lines))
	for _, f := range migrationLockPrefixFindings(mainParsed.lines, here.lines) {
		t.Error(f)
	}
}
