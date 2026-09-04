package main

// migration_lock_t75_judgement_test.go — the lock's judgement driven with
// corpora that are actually WRONG.
//
// 🔴 WHY THIS FILE IS NOT OPTIONAL. TestMigrationLockMatchesTheTree runs against
// a tree that is expected to be correct forever, so it is expected to be green
// forever — and a detector only ever exercised on clean input is indistinguishable
// from a detector that returns nil. Everything below feeds migrationLockFindings
// and migrationLockPrefixFindings a corpus carrying exactly one defect and
// requires that the finding names THAT defect by its own tag. Asserting merely
// that "something went red" would let a neighbouring check cover for a broken
// one; the tag is what stops that.
//
// The defects covered, one arm each: a migration missing from the lock, one too
// many, a duplicate version, a middle insertion, an edited body, a deleted
// migration, and a stale roll hash. Plus, at the bottom, the arm that asks the
// uncomfortable question: WOULD ANYTHING GO RED IF THE CHECK WERE SIMPLY GONE?

import (
	"os"
	"strings"
	"testing"
)

// lockFixture is a small, valid corpus: three .sql and one Go migration, mixed
// in one flat list exactly as the real file mixes them.
func lockFixture() []migrationLockEntry {
	return []migrationLockEntry{
		{version: 1, path: "migrations/00001_schema.sql", sha: strings.Repeat("a", 64)},
		{version: 2, path: "migrations/00002_two.sql", sha: strings.Repeat("b", 64)},
		{version: 3, path: "migration_00003_go.go", sha: strings.Repeat("c", 64)},
		{version: 4, path: "migrations/00004_four.sql", sha: strings.Repeat("d", 64)},
	}
}

func copyEntries(in []migrationLockEntry) []migrationLockEntry {
	out := make([]migrationLockEntry, len(in))
	copy(out, in)
	return out
}

// findingsMentioning returns the findings carrying tag.
func findingsMentioning(findings []string, tag string) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f, tag) {
			out = append(out, f)
		}
	}
	return out
}

// TestMigrationLockJudgementIsSilentOnACleanCorpus is the arm without which
// every arm below would also pass a judgement that always reports.
func TestMigrationLockJudgementIsSilentOnACleanCorpus(t *testing.T) {
	tree := lockFixture()
	if got := migrationLockFindings(renderMigrationLock(tree), tree); len(got) != 0 {
		t.Fatalf("a lock that matches its tree reported %d findings, want none: %v", len(got), got)
	}
}

// TestMigrationLockJudgementNamesEachDefect drives one defect at a time and
// requires the finding to carry that defect's own tag — not merely to exist.
func TestMigrationLockJudgementNamesEachDefect(t *testing.T) {
	cases := []struct {
		name string
		// mutate returns (lock text, tree) with exactly one defect planted.
		mutate  func() (string, []migrationLockEntry)
		wantTag string
		// wantIn must appear in the tagged finding, so the message names the
		// thing a reader has to go and look at.
		wantIn []string
		// wantOut must NOT appear in the tagged finding. It is for the arms where
		// the defect is the message SAYING TOO MUCH — an unreachable arm printed
		// beside a reachable one is not extra safety, it is a wrong instruction
		// the reader cannot rule out.
		wantOut []string
	}{
		{
			// ① A MIGRATION ADDED (or a file RENAMED) WITHOUT UPDATING THE LOCK.
			name: "tree has a migration the lock does not list",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				tree := append(copyEntries(lockFixture()), migrationLockEntry{
					version: 5, path: "migrations/00005_new.sql", sha: strings.Repeat("e", 64)})
				return lock, tree
			},
			wantTag: findMissing,
			wantIn:  []string{"migrations/00005_new.sql", "5"},
		},
		{
			// ② A MIGRATION DELETED FROM THE TREE, LOCK NOT REGENERATED.
			name: "lock lists a migration the tree does not have",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				tree := copyEntries(lockFixture())[:3] // 00004 gone
				return lock, tree
			},
			wantTag: findExtra,
			wantIn:  []string{"migrations/00004_four.sql"},
		},
		{
			// ③ THE CONTENTS OF AN ALREADY-LISTED MIGRATION CHANGED — the failure
			// a filename-only lock is blind to.
			name: "a migration's body was edited",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				tree := copyEntries(lockFixture())
				tree[1].sha = strings.Repeat("f", 64)
				return lock, tree
			},
			wantTag: findContent,
			wantIn:  []string{"migrations/00002_two.sql", strings.Repeat("b", 64), strings.Repeat("f", 64)},
		},
		{
			// ⑤ CROSS-SOURCE COLLISION — main ships a Go migration at NNNNN, a
			// branch that predates it adds a .sql at NNNNN, the merge is CLEAN
			// (a branch with no lock of its own takes the other side's as a new
			// file), and the tree ends up holding BOTH while the lock lists only
			// main's.
			//
			// 🔴 THIS ARM EXISTS BECAUSE THE FIRST VERSION OF THE [lock:path]
			// FINDING GOT IT BACKWARDS, and expensively. It printed a RENAME arm
			// alongside the COLLISION arm and told the reader to settle it with
			// `git log` on the lock's path — which here is main's Go migration and
			// of course HAS commits, so the reader is sent to "put the file back",
			// i.e. to overwrite a migration that has already shipped. Measured on
			// a real two-branch fixture, not reasoned. The discriminator is that
			// the lock's path is STILL IN THE TREE, so nothing was renamed.
			//
			// wantOut is therefore load-bearing: this arm is about what the
			// finding must NOT say.
			name: "cross-source collision: both files are in the tree",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				// The lock's own file (migration_00003_go.go) stays; a second
				// claimant on version 3 arrives from the other source.
				tree := append(copyEntries(lockFixture()), migrationLockEntry{
					version: 3, path: "migrations/00003_other_branch.sql", sha: strings.Repeat("e", 64)})
				return lock, tree
			},
			wantTag: findPathMoved,
			wantIn: []string{
				"THIS IS A COLLISION, not a rename",
				"migration_00003_go.go",
				"migrations/00003_other_branch.sql",
				"renumber THIS tree's newer file",
				"DO NOT touch migration_00003_go.go",
			},
			wantOut: []string{
				"RENAME —",
				"put the file back",
				"git log HEAD",
			},
		},
		{
			// ⑥ THE AMBIGUOUS SHAPE, which must still print BOTH arms: the lock's
			// path is GONE from the tree, so the listing alone genuinely cannot
			// tell a rename from a collision and the reader is handed the command
			// that can. Without this arm, a "fix" that simply deleted the RENAME
			// arm everywhere would pass ⑤ and lose the case ⑤ is not about.
			name: "lock's path is gone from the tree: both arms, plus the command",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				tree := copyEntries(lockFixture())
				tree[3].path = "migrations/00004_four_renamed.sql" // 00004 moved
				return lock, tree
			},
			wantTag: findPathMoved,
			wantIn: []string{
				"RENAME —",
				"COLLISION —",
				"put the file back",
				"git log HEAD -- server/ocserverd/migrations/00004_four.sql",
				"Commits ⇒ RENAME. Nothing ⇒ COLLISION.",
			},
		},
		{
			// ④ TWO MIGRATIONS ON ONE NUMBER — what a clean merge of two branches
			// produced before this file existed.
			name: "one version listed twice",
			mutate: func() (string, []migrationLockEntry) {
				dup := migrationLockEntry{version: 4, path: "migrations/00004_other.sql", sha: strings.Repeat("9", 64)}
				lockEntries := append(copyEntries(lockFixture()), dup)
				tree := append(copyEntries(lockFixture()), dup)
				return renderMigrationLock(lockEntries), tree
			},
			wantTag: findDup,
			wantIn:  []string{"version 4", "migrations/00004_other.sql"},
		},
		{
			// ⑤ A NUMBER FROM A GAP, APPENDED. This is the case the owner's
			// amendment is about: "sorted ascending" would have ACCEPTED it,
			// because a gap-filling number sorts perfectly into the middle. Only
			// "append-only" rejects it — and it rejects it with no baseline.
			name: "a below-maximum migration appended at the tail",
			mutate: func() (string, []migrationLockEntry) {
				gapFill := migrationLockEntry{version: 3, path: "migrations/00003_gap.sql", sha: strings.Repeat("7", 64)}
				lockEntries := append(copyEntries(lockFixture()), gapFill) // appended AFTER version 4
				tree := append(copyEntries(lockFixture()), gapFill)
				return renderMigrationLock(lockEntries), tree
			},
			wantTag: findOrder,
			wantIn:  []string{"migrations/00003_gap.sql", "version 3 after version 4"},
		},
		{
			// ⑥ A LINE EDITED BY HAND (or half a merge resolution kept).
			name: "an entry line hand-edited without regenerating the roll",
			mutate: func() (string, []migrationLockEntry) {
				tree := copyEntries(lockFixture())
				tree[3].sha = strings.Repeat("8", 64)
				lock := renderMigrationLock(lockFixture())
				// Patch the line in place, exactly as a hand-edit would: the roll
				// line is left saying what it said.
				lock = strings.Replace(lock, strings.Repeat("d", 64), strings.Repeat("8", 64), 1)
				return lock, tree
			},
			wantTag: findRoll,
			wantIn:  []string{"roll line says", "bin/gen-migration-lock"},
		},
		{
			// ⑦ THE FILE IS NOT A LOCK AT ALL — e.g. conflict markers left in.
			name: "unparseable lock",
			mutate: func() (string, []migrationLockEntry) {
				lock := renderMigrationLock(lockFixture())
				lock = strings.Replace(lock, migrationLockRollPrefix, "<<<<<<< HEAD\n"+migrationLockRollPrefix, 1)
				return lock, lockFixture()
			},
			wantTag: findParse,
			wantIn:  []string{"cannot be read"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, tree := tc.mutate()

			// The control that makes the arm mean something: the SAME judgement,
			// on the same corpus WITHOUT the defect, must be silent.
			clean := lockFixture()
			if got := migrationLockFindings(renderMigrationLock(clean), clean); len(got) != 0 {
				t.Fatalf("the undefected corpus already reports %v — this arm would pass on a "+
					"judgement that reports unconditionally", got)
			}

			findings := migrationLockFindings(lock, tree)
			if len(findings) == 0 {
				t.Fatalf("planted defect %q produced NO finding", tc.name)
			}
			tagged := findingsMentioning(findings, tc.wantTag)
			if len(tagged) == 0 {
				t.Fatalf("planted defect %q produced findings, but none tagged %s — so the check "+
					"aimed at this defect is not the one that fired, and it may be dead:\n%s",
					tc.name, tc.wantTag, strings.Join(findings, "\n"))
			}
			joined := strings.Join(tagged, "\n")
			for _, want := range tc.wantIn {
				if !strings.Contains(joined, want) {
					t.Fatalf("the %s finding does not mention %q, so a reader cannot tell what to "+
						"go and look at:\n%s", tc.wantTag, want, joined)
				}
			}
			for _, unwanted := range tc.wantOut {
				if strings.Contains(joined, unwanted) {
					t.Fatalf("the %s finding still says %q. For this shape that arm is not just "+
						"noise — it is the instruction that overwrites an already-shipped "+
						"migration, and a reader with two arms in front of them cannot rule it "+
						"out:\n%s", tc.wantTag, unwanted, joined)
				}
			}
		})
	}
}

// TestMigrationLockPrefixJudgement drives the class-B rule. It exists in this
// file, on synthetic input, precisely BECAUSE the live class-B test is a no-op on
// main and would otherwise be a check nobody had ever seen fire.
func TestMigrationLockPrefixJudgement(t *testing.T) {
	main := []string{
		"00001 migrations/00001_schema.sql sha256:aaa",
		"00002 migrations/00002_two.sql sha256:bbb",
		"00003 migration_00003_go.go sha256:ccc",
	}

	t.Run("appending is allowed", func(t *testing.T) {
		tree := append(append([]string{}, main...), "00004 migrations/00004_four.sql sha256:ddd")
		if got := migrationLockPrefixFindings(main, tree); len(got) != 0 {
			t.Fatalf("appending a new migration reported %v, want nothing — a rule that rejects "+
				"the normal case would simply be turned off", got)
		}
	})

	t.Run("an identical lock is allowed", func(t *testing.T) {
		// This is the shape the check has ON MAIN, where both sides are the same
		// commit. Asserted so the no-op is a measured fact, not a claim.
		if got := migrationLockPrefixFindings(main, append([]string{}, main...)); len(got) != 0 {
			t.Fatalf("identical locks reported %v, want nothing", got)
		}
	})

	t.Run("editing a released line is caught", func(t *testing.T) {
		tree := append([]string{}, main...)
		tree[1] = "00002 migrations/00002_two.sql sha256:EDITED"
		got := migrationLockPrefixFindings(main, tree)
		if len(findingsMentioning(got, findOrder)) == 0 {
			t.Fatalf("an edit to a released migration was not reported: %v", got)
		}
		if !strings.Contains(strings.Join(got, "\n"), "line 2") {
			t.Fatalf("the finding does not say which line diverged: %v", got)
		}
	})

	t.Run("inserting into the middle is caught", func(t *testing.T) {
		tree := []string{main[0], "00002 migrations/00002_inserted.sql sha256:xxx", main[1], main[2]}
		if len(findingsMentioning(migrationLockPrefixFindings(main, tree), findOrder)) == 0 {
			t.Fatal("a migration inserted below the released maximum was not reported")
		}
	})

	t.Run("removing a released line is caught", func(t *testing.T) {
		if len(findingsMentioning(migrationLockPrefixFindings(main, main[:2]), findExtra)) == 0 {
			t.Fatal("deleting a released migration was not reported")
		}
	})
}

// TestMigrationLockCheckIsReachedAtAll is the "what if the check were simply
// gone" arm. It is aimed at ONE way this change could rot: someone points the
// live check at a corpus that cannot disagree with it.
//
// It does NOT cover the other way, and an earlier version of this comment
// claimed it did. Measured, not reasoned about: seed a mutant that makes
// migrationLockFindings return nil unconditionally and this test still PASSES —
// it never calls the judgement. What goes red on that mutant is
// TestMigrationLockJudgementNamesEachDefect. Both mutants are caught; they are
// caught by DIFFERENT tests, and a comment that credits this one with coverage
// it does not have is the exact species of reassurance this whole change exists
// to remove.
//
// It works by proving the live path is WIRED: the lock on disk, parsed, has the
// same number of entries the shared enumeration returns, and the roll hash on
// disk is the one those very lines produce. If the enumeration were replaced by
// a stub, or the file by a placeholder, these numbers stop matching. It is a
// deliberately different question from TestMigrationLockMatchesTheTree, which
// asks whether the CONTENTS agree; this one asks whether the two things being
// compared are the real two things.
func TestMigrationLockCheckIsReachedAtAll(t *testing.T) {
	tree := migrationLockTreeEntries(t)
	if len(tree) < migrationLockMinSQL+migrationLockMinGo {
		t.Fatalf("the shared enumeration returned %d migrations — below the anti-vacuity floor, "+
			"so every comparison built on it would be trivially true", len(tree))
	}
	raw, err := os.ReadFile(migrationLockFile)
	if err != nil {
		t.Fatalf("read %s: %v", migrationLockFile, err)
	}
	parsed, err := parseMigrationLock(string(raw))
	if err != nil {
		t.Fatalf("parse %s: %v", migrationLockFile, err)
	}
	if len(parsed.entries) != len(tree) {
		t.Fatalf("%s holds %d entries and the shared enumeration found %d migrations in this "+
			"tree — the two sides of every check in this round are not the same population",
			migrationLockFile, len(parsed.entries), len(tree))
	}
	var body strings.Builder
	for _, l := range parsed.lines {
		body.WriteString(l)
		body.WriteString("\n")
	}
	if got := sha256Hex([]byte(body.String())); got != parsed.roll {
		t.Fatalf("the roll hash on disk (%s) is not the hash of the lines on disk (%s) — the one "+
			"property that makes two branches conflict here is not holding", parsed.roll, got)
	}
}
