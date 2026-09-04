package main

// migration_lock_t75_gen_test.go — the WRITER half of migration.lock.
//
// 🔴 WHY THE GENERATOR LIVES IN A TEST FILE, WHICH IS NOT WHERE A GENERATOR
// USUALLY GOES. Every other generated artifact in this repo has a bin/ script
// that produces it and a drift check that regenerates and byte-compares. That
// shape cannot be used here, and the reason is the single most likely way this
// whole change could have shipped broken:
//
//	IF THE WRITER ENUMERATES MIGRATIONS ONE WAY AND THE CHECKER ANOTHER, THE
//	CHECKER IS VALIDATING A DIFFERENT CORPUS THAN THE ONE THAT WAS WRITTEN — AND
//	IT IS GREEN WHILE DOING IT.
//
// The checker's enumeration is not portable: it reads the .sql bytes out of
// `embeddedMigrations` (the go:embed FS the server actually hands goose) and
// finds the Go migrations by AST-walking this package. Both are only reachable
// from INSIDE package main. A separate bin/ program would have to re-implement
// them — a second enumeration, quietly diverging, which is the failure above.
//
// So the writer is a test in this package calling migrationLockTreeEntries, the
// very same function the checker calls. bin/gen-migration-lock is the executable
// wrapper a human runs; it is a thin shell around this, not a second
// implementation. Sharing is by CALL, not by convention.
//
// It is inert unless OC_WRITE_MIGRATION_LOCK=1 — an env var rather than a
// `go test` flag because an unregistered flag makes the whole package's binary
// fail to start, which reads like the package is broken.
//
// 🔴 WHY IT APPENDS INSTEAD OF SORTING, which is the part that looks like a bug
// until you see what it catches. Rewriting the file in version order every time
// would be simpler AND would destroy the append-only signal: a migration
// numbered below the current maximum would sort quietly into the middle and the
// version column would still ascend. By keeping the existing lines where they
// are and putting new ones at the END, a below-maximum migration lands at the
// tail with a smaller number — and the ascending-column check ([lock:order])
// sees it WITH NO BASELINE, i.e. on main too, not only on a PR.

import (
	"fmt"
	"os"
	"testing"
)

// TestWriteMigrationLock regenerates migration.lock. Run it through
// bin/gen-migration-lock; the env guard keeps it from writing during an ordinary
// `go test`.
func TestWriteMigrationLock(t *testing.T) {
	if os.Getenv("OC_WRITE_MIGRATION_LOCK") != "1" {
		t.Skip("set OC_WRITE_MIGRATION_LOCK=1 (or run bin/gen-migration-lock) to regenerate " +
			migrationLockFile)
	}
	tree := migrationLockTreeEntries(t) // ← the SAME enumeration the checker uses
	ordered := migrationLockNextContents(t, tree)
	text := renderMigrationLock(ordered)
	if err := os.WriteFile(migrationLockFile, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", migrationLockFile, err)
	}
	// The marker bin/gen-migration-lock greps for. `go test -run` with a pattern
	// that matches NOTHING prints `ok` and exits 0, byte-identical to a real pass;
	// the wrapper refuses to believe rc alone.
	fmt.Printf("[gen-migration-lock] wrote %s: %d entries\n", migrationLockFile, len(ordered))
}

// migrationLockNextContents produces the lock's lines IN FILE ORDER: every line
// the current lock already has, in the position it already has (with the tree's
// current hash), followed by everything new, appended.
func migrationLockNextContents(t *testing.T, tree []migrationLockEntry) []migrationLockEntry {
	t.Helper()
	byPath := map[string]migrationLockEntry{}
	for _, e := range tree {
		byPath[e.path] = e
	}
	var ordered []migrationLockEntry
	kept := map[string]bool{}

	if raw, err := os.ReadFile(migrationLockFile); err == nil {
		existing, perr := parseMigrationLock(string(raw))
		if perr != nil {
			t.Fatalf("the existing %s does not parse (%v). Refusing to regenerate over a file "+
				"this tool cannot read: it would silently discard whatever ordering the file "+
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
		t.Fatalf("read %s: %v", migrationLockFile, err)
	}

	// tree is sorted by version, so first-time generation comes out ascending and
	// later additions append in version order.
	for _, e := range tree {
		if !kept[e.path] {
			ordered = append(ordered, e)
		}
	}
	return ordered
}
