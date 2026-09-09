package main

// migration_lock_test.go — the two LIVE checks over migration.lock, and nothing
// else. The mechanism they drive (the shared enumeration, the render/parse pair
// and both judgements) is ordinary source in migration_lock.go; read its header
// for why the lock exists and what each half can and cannot say.
//
// 🔴 WHAT THIS FILE IS NOT, since it used to be. Until T-125 this file also
// carried the enumeration, the generator and the git helpers — code that was
// never a test and only lived here because package-internal code had nowhere
// else runnable to sit. That is now `ocserverd migration-lock --write/--check`.
// Everything below asserts something.
//
//	CLASS A — TestMigrationLockMatchesTheTree, the lock against the migrations
//	sitting next to it. No baseline, so it is as alive on main as on a PR. It is
//	the same judgement the drift-migration-lock CI gate runs through the
//	subcommand; the duplication is deliberate and cheap — one arm fails at
//	`go test`, the other in the gate, and neither depends on the other running.
//
//	CLASS B — TestMigrationLockGrowsOnlyAtItsTail, the prefix rule against
//	origin/main. ⚠️ ON MAIN IT IS A NO-OP: the two sides are the same commit.

import (
	"os"
	"strings"
	"testing"
)

// treeEntries is the shared enumeration with a test's error handling. The
// enumeration itself returns an error rather than taking a *testing.T, because
// the subcommand is its other caller.
func treeEntries(t *testing.T) []migrationLockEntry {
	t.Helper()
	tree, err := migrationLockTreeEntries()
	if err != nil {
		t.Fatalf("enumerate this tree's migrations: %v", err)
	}
	return tree
}

// TestMigrationLockMatchesTheTree is CLASS A: the lock against the migrations
// sitting next to it. No baseline, so this one is as alive on main as on a PR.
//
// Red when: a migration was added, renamed, deleted or edited without
// regenerating the lock; a line was hand-edited; the roll hash is stale; the
// version column stopped ascending.
func TestMigrationLockMatchesTheTree(t *testing.T) {
	tree := treeEntries(t)

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
// PR-path check. See the class A/B note at the top of migration_lock.go.
//
// Red when: a line already on main was changed (a shipped migration was edited,
// or a new one was inserted into the middle of the list) or removed.
//
// 🔴 bin/check-released-migrations answers a NEIGHBOURING question from the same
// baseline — "did this branch touch a migration numbered at or below main's
// maximum" — off a plain `git diff --name-only`. It is coarser and it does not
// read the lock at all, which is the point: it survives the lock being
// regenerated, which is exactly what makes class A go green after a shipped
// migration is edited.
func TestMigrationLockGrowsOnlyAtItsTail(t *testing.T) {
	sha, err := gitOut("rev-parse", "origin/main")
	if err != nil {
		if inCI() {
			t.Fatalf("running in CI but origin/main does not resolve (%v), so the append-only "+
				"half of the lock has no baseline and this run would have been green with the "+
				"check switched off. FIX: the go-checks job's actions/checkout needs "+
				"`fetch-depth: 0`.", err)
		}
		t.Fatalf("the append-only half of the migration.lock guard cannot run here: this checkout "+
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
		// main carry THE MECHANISM? Before the merge commit it carries neither
		// file, so the skip is honest. From the merge commit onward it carries
		// migration_lock.go, and the mechanism on main with no lock beside it can
		// only mean the lock was removed or moved out from under this hardcoded
		// path.
		if _, e := gitOut("cat-file", "-e", "origin/main:"+migrationLockGuardRepoPath); e == nil {
			t.Fatalf("origin/main (%s) carries %s but NOT %s (%v). The mechanism is on main, so "+
				"this is no longer the pre-landing state: the lock was deleted, or moved out from "+
				"under the path this check reads. Either way the append-only half has silently "+
				"had no baseline since that happened — restore the file at that path, or move "+
				"BOTH constants in migration_lock.go together.",
				sha, migrationLockGuardRepoPath, migrationLockRepoPath, err)
		}
		t.Fatalf("origin/main (%s) carries neither %s nor %s (%v), so there is no baseline "+
			"to be a prefix of and nothing has landed yet. It is NOT a pass. This branch is what "+
			"puts both there; from that merge commit on, this same branch fatals instead.",
			sha, migrationLockRepoPath, migrationLockGuardRepoPath, err)
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
	// The DATE, not just the sha. T-64 shipped this line without it, then shipped a
	// note saying a stale baseline "can only make this check too permissive, never
	// too strict", and be10ac8d had to correct BOTH: the skew goes both ways, and the
	// direction that produces OUTPUT is the strict one. A reader looking at a red run
	// cannot tell a real finding from a stale-ref artefact without knowing which world
	// the verdict was reached in, and a sha alone does not say when.
	when, _ := gitOut("show", "-s", "--format=%ci", "origin/main")
	t.Logf("baseline: origin/main %s (%s) carries %d lock entries; this tree carries %d.\n"+
		"If that date is older than main really is, `git fetch origin main` and re-run before "+
		"believing anything below — a stale baseline skews this check BOTH WAYS, not one.\n"+
		"  TOO PERMISSIVE, and silent: main's newer entries are simply absent from the "+
		"baseline, so a prefix match succeeds against a shorter list than it should.\n"+
		"  TOO STRICT, and loud: every entry main itself rewrote since that ref reads as THIS "+
		"TREE having changed a shipped migration. That report names the line and the sha and "+
		"is specific enough to be believed. Check the date FIRST.",
		sha, when, len(mainParsed.lines), len(here.lines))
	for _, f := range migrationLockPrefixFindings(mainParsed.lines, here.lines) {
		t.Error(f)
	}
}

func TestCmdMigrationLock(t *testing.T) {
	t.Run("missing mode returns usage and a nonzero command status", func(t *testing.T) {
		var out strings.Builder
		if got := cmdMigrationLock(nil, &out); got != 2 {
			t.Fatalf("cmdMigrationLock(nil) = %d, want 2", got)
		}
		if !strings.Contains(out.String(), "needs exactly one of --write or --check") {
			t.Fatalf("usage output = %q, want the missing-mode explanation", out.String())
		}
	})

	t.Run("an unknown argument is rejected before touching the lock", func(t *testing.T) {
		var out strings.Builder
		if got := cmdMigrationLock([]string{"--unknown"}, &out); got != 2 {
			t.Fatalf("cmdMigrationLock(--unknown) = %d, want 2", got)
		}
		want := `[migration-lock] unknown argument "--unknown" — want exactly one of --write or --check`
		if !strings.Contains(out.String(), want) {
			t.Fatalf("unknown-argument output = %q, want %q", out.String(), want)
		}
	})

	t.Run("check mode validates the committed lock and reports its entry count", func(t *testing.T) {
		var out strings.Builder
		if got := cmdMigrationLock([]string{"--check"}, &out); got != 0 {
			t.Fatalf("cmdMigrationLock(--check) = %d, output %q", got, out.String())
		}
		if !strings.Contains(out.String(), "OK — "+migrationLockFile+" matches this tree exactly") {
			t.Fatalf("check output = %q, want the successful lock verdict", out.String())
		}
	})
}
