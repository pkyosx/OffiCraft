package main

// backup.go — the database backup engine: ONE implementation, several triggers.
//
// WHY this exists at all (T-ada9, owner 2026-07-31): until this landed the
// studio had NO backup of any kind. Every task, chat message, reply card,
// lessons doc and task manual lives in a single SQLite file, so a corrupt or
// deleted file meant there was nothing to go back to. That was the one failure
// mode with no retreat.
//
// WHY it is IN the server (owner asked directly, 2026-07-31): producing a
// consistent snapshot has to be done BY the database, and the server process is
// the one that already knows which database file this instance uses (namespaced
// instances each have their own). An external scheduler would have to carry a
// second copy of that knowledge, and — the part that actually matters — it dies
// SILENTLY.
//
// WHY one implementation with several triggers: a manual backup taken by hand
// before a risky operation and the one the cadence takes at 4am must be the
// SAME act. Two code paths would mean the one that was verified is not the one
// that runs unattended.
//
//	trigger ①  `ocserverd backup` (cmdBackup, main.go)  — by hand, before
//	           anything risky. This is also how the whole engine gets exercised
//	           by a human rather than only by a test.
//	trigger ②  the serve cadence (startBackupCadence) — always mounted, same
//	           shape as the auto-update cadence.
//	trigger ③  before goose migrations run (cmdServe) — owner: "每次升級
//	           server 前我們可以先備份在升級". Swapping the BINARY cannot hurt
//	           the data; a schema migration can, so the hook belongs there.
//	trigger ④  (not in this file) the cockpit's manual-backup button, which
//	           calls the same runDatabaseBackup.
//
// The snapshot mechanism is `VACUUM INTO` — SQLite's own online backup. It does
// not stop the server, and it produces a SINGLE self-consistent file, so a
// backup is one path to copy rather than three (main + -wal + -shm). Verified
// on modernc.org/sqlite v1.53.0 against the real 340 MB production database
// before any of this was written: 429 ms, integrity_check ok, every table's row
// count equal to the source, and a known row read back OUT OF THE BACKUP FILE.
//
// 🔴 What this engine deliberately does NOT do, so nobody reads more safety
// into it than it has:
//   - **It never deletes anything.** Rotation MOVES the evicted file into
//     `trash/` (repo rule: agents and the server do not `rm`; reclamation is
//     the warden's job). A rotation that deletes is a rotation that can delete
//     the wrong thing.
//   - **Backups live on the SAME MACHINE as the database.** That covers a
//     corrupt file or a bad migration; it does NOT cover losing the machine.
//     Off-machine backup is a separate ticket, and the owner was told so
//     explicitly rather than being left to assume otherwise.
//   - **Failure is loud in the server log, not in the cockpit.** A backup that
//     fails silently is worse than no backup, because it makes someone believe
//     they have a retreat — so every outcome logs, and a backup directory whose
//     newest file has gone stale logs too ("never ran" has to be as loud as
//     "ran and failed"). Getting that in front of the owner needs the cockpit
//     layer and is scoped separately.

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	// backupCadence is how often the loop WAKES, not how often it backs up.
	// A short tick with an interval check means a server that was down over a
	// backup window takes one shortly after boot instead of waiting a full
	// interval.
	backupCadence = 15 * time.Minute

	// backupInterval is the minimum age of the newest backup before the
	// cadence takes another one. 6h × 5 retained ≈ 30h of coverage.
	backupInterval = 6 * time.Hour

	// backupRetain is how many backup files are kept (owner 2026-07-31: "我們
	// 可以保留 5 份備份檔好了" + "自動 rotate"). Evicted files are MOVED to
	// trash/, never deleted here.
	//
	// ⚠️ One pool, one quota: a pre-migration backup counts against the same 5,
	// so a burst of upgrades can evict the scheduled ones. Upgrades are rare
	// and the freshest file is always kept, so this is accepted rather than
	// hidden behind a second quota.
	backupRetain = 5

	// backupFreeSpaceFactor is how much room must be free relative to the
	// database size before a backup is attempted. The snapshot is roughly the
	// size of the database, and this machine has run out of swap before — a
	// backup must never be the thing that fills the disk.
	backupFreeSpaceFactor = 3

	// backupStaleFactor multiplies backupInterval to decide when the newest
	// backup is old enough to complain about. This is the "never ran" alarm:
	// without it, a cadence that silently stopped looks exactly like a healthy
	// one.
	backupStaleFactor = 2

	// backupFilePrefix / backupFileSuffix bound what rotation is willing to
	// consider one of ITS files. Rotation moves files, so it must never be able
	// to reach something it did not create.
	backupFilePrefix = "officraft-"
	backupFileSuffix = ".db"
)

// backupReason labels WHY a snapshot was taken. It rides in the filename, so a
// directory listing answers "was this the schedule, or did something risky
// happen here?" without opening anything.
type backupReason string

const (
	backupReasonManual       backupReason = "manual"
	backupReasonScheduled    backupReason = "scheduled"
	backupReasonPreMigration backupReason = "premigration"
)

// backupDirFor / backupTrashFor sit BESIDE the database file rather than under
// a fixed path, so a namespaced instance backs itself up into its own root and
// two instances can never write over each other.
func backupDirFor(dbPath string) string   { return filepath.Join(filepath.Dir(dbPath), "backups") }
func backupTrashFor(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "trash") }

// backupResult is what one snapshot attempt produced. Path is empty when the
// attempt was deliberately skipped (see Skipped).
type backupResult struct {
	Path     string
	Bytes    int64
	Took     time.Duration
	Rotated  []string // files moved into trash/ by this run
	Skipped  string   // non-empty = nothing was written, and this is why
	Reason   backupReason
	Stale    bool   // the newest PRE-EXISTING backup was older than the alarm window
	StaleAge string // human-readable age of that newest pre-existing backup
}

// freeBytesAt reports the free space on the filesystem holding dir. A failure
// to measure is NOT treated as "no space": refusing to back up because statfs
// hiccuped would turn an observability problem into a missing retreat.
func freeBytesAt(dir string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}

// backupFilesIn lists this engine's own backup files, newest first. Anything
// that does not match the prefix/suffix is invisible to it — including the
// hand-made `officraft.db.bak-pre-*` snapshots that predate this engine, which
// it must neither rotate nor count.
func backupFilesIn(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var mine []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, backupFilePrefix) && strings.HasSuffix(name, backupFileSuffix) {
			mine = append(mine, e)
		}
	}
	// The timestamp is fixed-width and lexically ordered, so sorting names
	// descending IS newest-first — no stat call, and no dependence on mtime
	// (which a copy or a restore can rewrite).
	sort.Slice(mine, func(i, j int) bool { return mine[i].Name() > mine[j].Name() })
	return mine, nil
}

// newestBackupTime parses the timestamp back out of the newest filename. It
// reads the FILENAME rather than the mtime on purpose: mtime survives neither
// copying a backup directory around nor a restore, and this value is what the
// staleness alarm and the cadence's "is one due?" question both rest on.
func newestBackupTime(dir string) (time.Time, bool) {
	files, err := backupFilesIn(dir)
	if err != nil || len(files) == 0 {
		return time.Time{}, false
	}
	return parseBackupStamp(files[0].Name())
}

// parseBackupStamp pulls the UTC stamp out of `officraft-<stamp>-<reason>.db`.
func parseBackupStamp(name string) (time.Time, bool) {
	rest := strings.TrimPrefix(name, backupFilePrefix)
	rest = strings.TrimSuffix(rest, backupFileSuffix)
	// stamp is the first two dash-separated fields: 20260731-224500
	parts := strings.Split(rest, "-")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	ts, err := time.Parse("20060102-150405", parts[0]+"-"+parts[1])
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

// backupFileName is the only place a backup filename is spelled, so the writer
// and every reader (rotation, staleness, the cadence) cannot drift apart.
func backupFileName(now time.Time, reason backupReason) string {
	return fmt.Sprintf("%s%s-%s%s", backupFilePrefix, now.UTC().Format("20060102-150405"), reason, backupFileSuffix)
}

// runDatabaseBackup takes ONE snapshot. Every trigger goes through here.
//
// It writes to a `.partial` name and renames on success, so a crash or a full
// disk can never leave a truncated file sitting in the directory looking like a
// backup. That matters more than it sounds: the whole value of this directory is
// that everything in it can be restored, and a half-written file that is named
// like a backup poisons that assumption.
func runDatabaseBackup(db *sql.DB, dbPath string, reason backupReason, now time.Time) (backupResult, error) {
	res := backupResult{Reason: reason}
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, fmt.Errorf("create backup dir: %w", err)
	}

	// Report on the state we are walking into BEFORE writing, so "the cadence
	// had stopped and nobody noticed" is visible in the run that resumes it.
	if newest, ok := newestBackupTime(dir); ok {
		if age := now.Sub(newest); age > backupStaleFactor*backupInterval {
			res.Stale, res.StaleAge = true, age.Round(time.Minute).String()
		}
	} else {
		res.Stale, res.StaleAge = true, "no previous backup"
	}

	if info, err := os.Stat(dbPath); err == nil {
		if free, ok := freeBytesAt(dir); ok {
			if need := info.Size() * backupFreeSpaceFactor; free < need {
				res.Skipped = fmt.Sprintf("only %d MB free, want %d MB (db is %d MB)",
					free>>20, need>>20, info.Size()>>20)
				return res, nil
			}
		}
	}

	final := filepath.Join(dir, backupFileName(now, reason))
	partial := final + ".partial"
	_ = os.Remove(partial)

	started := time.Now()
	// VACUUM INTO only READS the source database, so the running server keeps
	// serving. What T-dd7a's WAL changed is precisely one half of that:
	//
	//   - READERS no longer wait. That is WAL doing its job.
	//   - WRITERS STILL WAIT the whole duration. 🔴 Do NOT read this as "backups
	//     do not affect writes". The reason has nothing to do with journal mode:
	//     this Exec occupies the write pool's ONE connection (openSQLite caps it
	//     at 1), so Go's pool queues every other writer behind it before SQLite
	//     is even consulted. Measured on a 78 MB database AFTER WAL was on:
	//     vacuum 108ms, writer wait 86ms, reader wait 3ms. Earlier measurement
	//     for scale: ~0.43s for 340 MB.
	//
	// The stall is therefore real and roughly the length of the snapshot. Anyone
	// quoting a cost for "what does a backup cost the studio" must quote the
	// WRITE side.
	//
	// 🔴 VACUUM INTO is also the reason this file is NOT what the single-file-copy
	// guard hunts (db_singlefile_copy_guard_test.go): it is SQLite's own online
	// backup, so the engine reads its own pages INCLUDING the "-wal" sidecar and
	// writes one already-consistent file. A `cp` of officraft.db would not — under
	// WAL it can silently omit the most recent commits.
	if _, err := db.Exec(`VACUUM INTO ?`, partial); err != nil {
		_ = os.Remove(partial)
		return res, fmt.Errorf("vacuum into %s: %w", partial, err)
	}
	res.Took = time.Since(started)

	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return res, fmt.Errorf("publish backup: %w", err)
	}
	if err := os.Chmod(final, 0o600); err != nil {
		// The bytes are already safe; a permissive mode is worth saying out
		// loud but not worth discarding the backup over.
		log.Printf("[backup] could not tighten permissions on %s: %v", final, err)
	}
	if info, err := os.Stat(final); err == nil {
		res.Bytes = info.Size()
	}
	res.Path = final

	rotated, err := rotateBackups(dbPath, backupRetain)
	if err != nil {
		// The new backup EXISTS — that is the point of this function. A
		// rotation problem is a disk-growth problem, reported without
		// pretending the snapshot failed.
		log.Printf("[backup] rotation after %s failed: %v", final, err)
	}
	res.Rotated = rotated
	return res, nil
}

// rotateBackups keeps the newest `keep` files and MOVES the rest into trash/.
//
// 🔴 It moves, never deletes (repo rule). The mechanical reason matters as much
// as the rule: a bug in a mover leaves the file findable, the same bug in a
// deleter destroys exactly the thing this whole file exists to preserve.
func rotateBackups(dbPath string, keep int) ([]string, error) {
	dir := backupDirFor(dbPath)
	files, err := backupFilesIn(dir)
	if err != nil {
		return nil, err
	}
	if keep < 1 || len(files) <= keep {
		return nil, nil
	}
	trash := backupTrashFor(dbPath)
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return nil, fmt.Errorf("create trash dir: %w", err)
	}
	var moved []string
	for _, e := range files[keep:] {
		from := filepath.Join(dir, e.Name())
		to := filepath.Join(trash, e.Name())
		if err := os.Rename(from, to); err != nil {
			return moved, fmt.Errorf("retire %s: %w", e.Name(), err)
		}
		moved = append(moved, e.Name())
	}
	return moved, nil
}

// logBackupOutcome is the single voice of this engine. Every trigger reports
// through it so a reader of the log never has to know which one fired.
func logBackupOutcome(res backupResult, err error) {
	if res.Stale && res.StaleAge != "" {
		// Deliberately its own line: "the schedule had stopped" and "this run
		// worked" are different facts and a reader must be able to see both.
		log.Printf("[backup] WARNING newest existing backup was stale (%s) — the schedule may have stopped running", res.StaleAge)
	}
	switch {
	case err != nil:
		log.Printf("[backup] FAILED (%s): %v — THERE IS NO NEW RETREAT POINT", res.Reason, err)
	case res.Skipped != "":
		log.Printf("[backup] SKIPPED (%s): %s — no new retreat point was created", res.Reason, res.Skipped)
	default:
		log.Printf("[backup] ok (%s): %s (%d MB in %s)", res.Reason, filepath.Base(res.Path), res.Bytes>>20, res.Took.Round(time.Millisecond))
		if len(res.Rotated) > 0 {
			log.Printf("[backup] rotated %d older backup(s) into trash/: %s", len(res.Rotated), strings.Join(res.Rotated, ", "))
		}
	}
}

// startBackupCadence mounts the background loop. ALWAYS mounted by cmdServe —
// same shape as startAutoUpdateCadence — because a backup that has to be armed
// is a backup nobody has.
// It is deliberately NOT a method on apiServer even though the auto-update
// cadence next to it is: this engine needs the database handle and the file
// path, and nothing at all from the server struct. Hanging it off apiServer
// would suggest it reads server state that it does not.
func startBackupCadence(db *sql.DB, dbPath string, tick time.Duration) {
	go func() {
		for {
			time.Sleep(tick)
			backupTick(db, dbPath, time.Now())
		}
	}()
}

// backupTick is ONE evaluation, split out so the decision can be tested without
// waiting on a clock. taken=false means a backup was not DUE — which is the
// normal answer and is deliberately silent, unlike a failure or a skip.
func backupTick(db *sql.DB, dbPath string, now time.Time) (taken bool) {
	dir := backupDirFor(dbPath)
	if newest, ok := newestBackupTime(dir); ok && now.Sub(newest) < backupInterval {
		return false
	}
	res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now)
	logBackupOutcome(res, err)
	return err == nil && res.Skipped == ""
}

// backupBeforeMigrations is trigger ③. It runs BEFORE goose, and only when
// there is something to protect against — a database file that does not exist
// yet has nothing to lose, and a first boot should not be slowed down by
// snapshotting an empty file.
//
// A failure here is reported and does NOT stop the server: refusing to boot
// because a backup failed would trade an unlikely data risk for a certain
// outage. The log line says plainly that the migration is proceeding without a
// fresh retreat point.
func backupBeforeMigrations(db *sql.DB, dbPath string, now time.Time) {
	info, err := os.Stat(dbPath)
	if err != nil || info.Size() == 0 {
		return
	}
	res, err := runDatabaseBackup(db, dbPath, backupReasonPreMigration, now)
	logBackupOutcome(res, err)
	if err != nil || res.Skipped != "" {
		log.Printf("[backup] proceeding with migrations WITHOUT a fresh pre-migration backup")
	}
}
