package main

// backup_premigration_pool_test.go — T-ada9 step 6.
//
// 🔴 WHAT FAILURE THIS FILE EXISTS FOR, stated precisely because the imprecise
// version has already been written down once and had to be corrected:
//
// Rotation used to keep ONE pool of 5 across every reason, so the pre-migration
// backup competed for slots with the routine ones. The trigger is therefore NOT
// the passage of time — it is **five backups from any source**. Somebody taking
// five manual snapshots while investigating a problem evicts the pre-migration
// backup within MINUTES, and that is exactly the situation in which it is the
// only retreat there is (the schema change already went wrong). Writing this up
// as "about 30 hours (6h × 5)" counts only the scheduled trigger and makes the
// window sound survivable.
//
// 🔴 Why every test here asserts BOTH halves in the same fixture: "the
// pre-migration backup is still there" is satisfied by a pool that never
// overflowed, so on its own it proves nothing. The second assertion — the OLDEST
// routine backup is gone — is the evidence that the quota really did fill and
// rotation really did run. Both, in one directory, or neither is worth anything.
//
// 🔴 And the comparison is name-by-name in both directions: what should survive
// survived, what should not survive did not, and nothing extra was retired.
// Counting files hides the two cases that matter (the right number of the wrong
// files, and one eviction too many).
//
// Nothing here touches the production database or the real server root: every
// fixture lives in its own t.TempDir() and every path is derived from the
// database path handed to the engine.

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// takeBackup writes one backup and returns its bare filename.
func takeBackup(t *testing.T, db *sql.DB, dbPath string, reason backupReason, at time.Time) string {
	t.Helper()
	res, err := runDatabaseBackup(db, dbPath, reason, at)
	if err != nil {
		t.Fatalf("backup (%s, %s): %v", reason, at.Format(time.RFC3339), err)
	}
	if res.Skipped != "" {
		t.Fatalf("backup (%s) skipped: %s", reason, res.Skipped)
	}
	return filepath.Base(res.Path)
}

// namesIn lists a directory's plain files, sorted, so two sets can be compared
// element by element rather than by size.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// assertSameSet names every difference in both directions. Callers pass the
// COMPLETE expected set, so "should have been kept but was not", "should have
// been retired but is still here" and "was retired although nothing said to"
// each produce their own line.
func assertSameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	for _, n := range want {
		if !gotSet[n] {
			t.Errorf("%s: %s is MISSING (it should be there)", what, n)
		}
	}
	for _, n := range got {
		if !wantSet[n] {
			t.Errorf("%s: %s is present but should NOT be (over-eviction or wrong survivor)", what, n)
		}
	}
}

// TestRotate_PreMigrationSurvivesAFloodOfScheduledBackups is the ticket in one
// fixture: enough scheduled backups to overflow the routine quota, plus the one
// pre-migration backup that must not be paying for them.
func TestRotate_PreMigrationSurvivesAFloodOfScheduledBackups(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	base := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	// Oldest file in the directory, so a single shared pool retires it first —
	// which is precisely the defect.
	premigration := takeBackup(t, db, dbPath, backupReasonPreMigration, base)

	// One more than the quota, so the routine pool provably overflows.
	var scheduled []string
	for i := 1; i <= backupRetain+1; i++ {
		scheduled = append(scheduled, takeBackup(t, db, dbPath, backupReasonScheduled, base.Add(time.Duration(i)*time.Hour)))
	}
	oldestScheduled, newestScheduled := scheduled[0], scheduled[1:]

	kept := namesIn(t, backupDirFor(dbPath))
	trashed := namesIn(t, backupTrashFor(dbPath))

	// (1) The two halves of the positive/negative pair, spelled out one at a
	//     time so a failure says which half broke.
	if !contains(kept, premigration) {
		t.Errorf("the pre-migration backup %s was evicted by routine backups — it is the only retreat from a bad migration", premigration)
	}
	if contains(kept, oldestScheduled) {
		t.Errorf("the oldest scheduled backup %s is still here — the routine quota never filled, so this fixture proves nothing about eviction", oldestScheduled)
	}

	// (2) And the whole picture, both directions, by name.
	assertSameSet(t, "backups/", kept, append([]string{premigration}, newestScheduled...))
	assertSameSet(t, "trash/", trashed, []string{oldestScheduled})

	// Rotation retires by MOVING (repo rule), and a safety net only helps if
	// what lands in it is a real backup.
	if _, rows := readBackSentinel(t, filepath.Join(backupTrashFor(dbPath), oldestScheduled)); rows == 0 {
		t.Errorf("retired backup %s is unreadable", oldestScheduled)
	}
	// The kept pre-migration file has to still be restorable, not just present.
	if _, rows := readBackSentinel(t, filepath.Join(backupDirFor(dbPath), premigration)); rows == 0 {
		t.Errorf("surviving pre-migration backup %s carries no rows", premigration)
	}
}

// TestRotate_PreMigrationSurvivesAManualInvestigationBurst is the failure the
// owner would actually hit: something went wrong, somebody takes manual backup
// after manual backup while digging, and the pre-migration snapshot is gone
// within minutes. It also pins the deliberate other half of the shape — manual
// and scheduled DO share one quota, because both are routine coverage.
func TestRotate_PreMigrationSurvivesAManualInvestigationBurst(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	base := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	premigration := takeBackup(t, db, dbPath, backupReasonPreMigration, base)

	// Interleaved on purpose: if manual and scheduled were ever split into two
	// quotas of their own, this fixture would stop overflowing and the negative
	// assertion below would catch it.
	var routine []string
	for i := 1; i <= backupRetain+1; i++ {
		reason := backupReasonManual
		if i%2 == 0 {
			reason = backupReasonScheduled
		}
		routine = append(routine, takeBackup(t, db, dbPath, reason, base.Add(time.Duration(i)*time.Minute)))
	}
	oldestRoutine, survivors := routine[0], routine[1:]

	kept := namesIn(t, backupDirFor(dbPath))

	if !contains(kept, premigration) {
		t.Errorf("a burst of %d routine backups evicted the pre-migration backup %s in %d minutes", len(routine), premigration, len(routine))
	}
	if contains(kept, oldestRoutine) {
		t.Errorf("the oldest routine backup %s survived — manual and scheduled must share one quota, and this fixture must overflow it", oldestRoutine)
	}
	assertSameSet(t, "backups/", kept, append([]string{premigration}, survivors...))
	assertSameSet(t, "trash/", namesIn(t, backupTrashFor(dbPath)), []string{oldestRoutine})
}

// TestRotate_PreMigrationPoolIsBoundedToo guards the over-correction. Giving
// pre-migration backups their own quota must not mean giving them NO quota:
// a directory that only ever grows is how this machine ran out of swap before.
//
// ⚠️ Honest note on this test's discriminating power: it passes both before and
// after the split (one shared pool of 5 produces the same answer when every file
// has the same reason). It is red only against the plausible WRONG fix —
// exempting pre-migration files from rotation altogether — which is exactly why
// it is here and not merely implied by the two tests above.
func TestRotate_PreMigrationPoolIsBoundedToo(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	base := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)

	var made []string
	for i := 0; i < backupRetain+2; i++ {
		made = append(made, takeBackup(t, db, dbPath, backupReasonPreMigration, base.Add(time.Duration(i)*time.Hour)))
	}
	retired, survivors := made[:2], made[2:]

	assertSameSet(t, "backups/", namesIn(t, backupDirFor(dbPath)), survivors)
	assertSameSet(t, "trash/", namesIn(t, backupTrashFor(dbPath)), retired)
	for _, name := range retired {
		if _, rows := readBackSentinel(t, filepath.Join(backupTrashFor(dbPath), name)); rows == 0 {
			t.Errorf("retired pre-migration backup %s is unreadable", name)
		}
	}
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
