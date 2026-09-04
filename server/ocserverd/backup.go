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
//	trigger ③  before goose migrations run — owner: "每次升級 server 前我們可
//	           以先備份在升級". Swapping the BINARY cannot hurt the data; a
//	           schema migration can, so the hook belongs there. It is mounted at
//	           BOTH doors into goose: cmdServe (server.go) and `ocserverd
//	           migrate` (cmdMigrate, migrate.go). It was on serve only until
//	           T-74, which meant the upgrade path's own `migrate` invocation
//	           carried the identical schema risk with no retreat point.
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
// 🔴 Retention has an EXIT, and this is the part that was missing until T-8.
// Rotation used to MOVE the evicted file into `trash/` and the comment here said
// "reclamation is the warden's job". That sentence was FALSE the day it was
// written, and it is recorded rather than quietly deleted because the shape of
// the mistake is the lesson:
//
//   - Nothing ever read `trash/`. `backupTrashFor` had exactly one non-test
//     caller — the rotation that wrote INTO it. No walker, no reaper, no
//     scheduled sweep.
//   - The warden's reaper (cli/ocwarden/trash.go, purgeTrash) could not have
//     taken the job even if it had been asked: its first guard requires the
//     workdir to be a DIRECT CHILD of the agents root, and the server's data
//     directory is not under that root at all. It was never called with this
//     path and would have refused it.
//
// So the retirement path was write-only, and by 2026-08-27 the studio's
// `server/data/trash/` held 278 files / 141.6 GiB growing ~6.9 GB a day — a
// disk-full clock, produced by a RESPONSIBILITY GAP rather than a broken
// component: the backup engine handed reclamation to a party that had never
// accepted it, and no code anywhere asserted the hand-off.
//
// Rotation therefore DELETES now (owner 2026-08-27: "我覺得應該只保留最新的 N
// 版備份，N 可以設定，剩餘的應該直接移除"), and `reapBackupTrash` drains the
// backlog the old path left behind. Both are bounded to files this engine can
// prove it created — see backupFilesIn.
//
// 🔴 What this engine deliberately does NOT do, so nobody reads more safety
// into it than it has:
//   - **It deletes only its OWN files, and only the ones already past the
//     retention window.** `backupFilesIn` is the whole reach: prefix
//     `officraft-` AND suffix `.db`, directories skipped. A hand-made
//     `officraft.db.bak-pre-*`, a stray `.partial`, a subdirectory — none of
//     them are visible to retention, so none of them can be counted as one of
//     the N and none of them can be removed.
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
	"strconv"
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
	// cadence takes another one.
	//
	// ⚠️ "6h × 5 = 30h of coverage" is the arithmetic for a directory that gets
	// nothing but scheduled backups. Manual ones share the routine quota, so a
	// busy day covers less than 30h. Do not quote the product as a guarantee.
	backupInterval = 6 * time.Hour

	// backupRetainDefault is the SHIPPED value of N — how many backup files are
	// kept PER POOL — used when `backup.retain` was never written.
	//
	// 🔴 WHO CHOSE 5: the owner, on 2026-07-31, in the ticket that created this
	// engine (T-ada9; the commit that introduced the constant records it as
	// "owner 另外指定三件：保留 5 份並自動 rotate"). T-8 made N adjustable and
	// deliberately did NOT move the default — changing the shipped number and
	// making it adjustable in the same change would silently re-decide something
	// the owner already decided.
	//
	// 🔴 N COUNTS VERSIONS, NOT DAYS. Five is five FILES, and how much calendar
	// they span depends entirely on how busy the day was: measured on this
	// machine, 2026-08-19 produced 19 backups and 2026-08-24 produced 4. The
	// same N therefore covers less than three days on a busy day and over a week
	// on a quiet one. Anyone quoting a retention DEPTH in time is quoting a
	// number this constant does not carry.
	//
	// 🔴 N IS PER POOL, NOT PER DIRECTORY. There are two pools (backupPoolOf),
	// so the directory holds up to 2 × N files, not N. Setting 5 buys 10 files,
	// not 5.
	backupRetainDefault = 5

	// minBackupRetain / maxBackupRetain bound what `backup.retain` may be set
	// to. Both are the IMPLEMENTER's choice (T-8), not the owner's, and the
	// reasoning is on each:
	//
	//   - The floor is 1, not 0. Zero would mean "keep nothing", i.e. delete the
	//     snapshot that was just taken — a knob whose lowest setting destroys
	//     the thing the knob is about. One means the newest of each pool always
	//     survives, which is the least this engine can promise and still be a
	//     backup engine.
	//   - The ceiling is 20 because N is a DISK budget in disguise. At the
	//     snapshot size measured on this machine (~712 MB) and two pools, the
	//     steady-state cost is 2 × N × 712 MB: N=5 is ~7.4 GiB (which is exactly
	//     what backups/ measured at), N=20 is ~28 GiB. Past that the knob starts
	//     recreating the unbounded-growth failure this ticket exists to end, so
	//     the ceiling is where the cost stops being a setting and starts being
	//     an incident.
	minBackupRetain = 1
	maxBackupRetain = 20

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

// backupPool is the set of files that compete for one quota of N (backup.retain).
type backupPool string

const (
	// backupPoolRoutine is ongoing coverage: the cadence's snapshots and the
	// ones a human takes by hand. They are interchangeable — either one is
	// "the state of the studio around then" — so they share one quota, and the
	// oldest of them is genuinely the least valuable file in the directory.
	backupPoolRoutine backupPool = "routine"

	// backupPoolPreMigration is its own pool, and this is the whole point of
	// having pools at all.
	//
	// 🔴 A pre-migration backup is the ONLY retreat from a schema migration that
	// went wrong, and it pays off at the moment you reach for it — which is
	// AFTER something already broke. In one shared pool its eviction trigger was
	// not time, it was FIVE BACKUPS FROM ANY SOURCE: somebody taking five manual
	// snapshots while investigating the breakage destroyed it within MINUTES, in
	// the exact situation it existed for. (The tempting arithmetic — "6h × 5 ≈ 30
	// hours of cover" — counts only the scheduled trigger and reads as safe.)
	//
	// A retreat that its own rotation can evict is a retreat that is absent on
	// the one day it is needed, so it does not share a quota with routine files.
	// It still HAS a quota (see rotateBackups): a directory that only grows is
	// how this machine ran out of disk before.
	backupPoolPreMigration backupPool = "premigration"
)

// backupPoolOf decides which quota a file counts against, reading the reason
// back out of the filename. An unrecognised or unparseable label lands in the
// routine pool: the alternative — inventing a pool per unknown label — would let
// a future typo create an unbounded directory that nothing ever rotates.
func backupPoolOf(name string) backupPool {
	if backupReasonIn(name) == backupReasonPreMigration {
		return backupPoolPreMigration
	}
	return backupPoolRoutine
}

// backupReasonIn pulls the reason field out of
// `officraft-<date>-<time>-<reason>.db`. It is the same parse as
// parseBackupStamp, reading the third field instead of the first two, and it is
// spelled once so the writer (backupFileName) cannot drift from the readers.
func backupReasonIn(name string) backupReason {
	rest := strings.TrimSuffix(strings.TrimPrefix(name, backupFilePrefix), backupFileSuffix)
	parts := strings.Split(rest, "-")
	if len(parts) < 3 {
		return ""
	}
	return backupReason(strings.Join(parts[2:], "-"))
}

// backupDirFor / backupTrashFor sit BESIDE the database file rather than under
// a fixed path, so a namespaced instance backs itself up into its own root and
// two instances can never write over each other.
//
// ⚠️ backupTrashFor no longer names a destination. Rotation stopped writing into
// `trash/` in T-8; the only thing that still names the directory is
// reapBackupTrash, which DRAINS the backlog the old move-based rotation left
// there. Do not re-point an eviction at it — see the file header.
func backupDirFor(dbPath string) string   { return filepath.Join(filepath.Dir(dbPath), "backups") }
func backupTrashFor(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "trash") }

// backupResult is what one snapshot attempt produced. Path is empty when the
// attempt was deliberately skipped (see Skipped).
type backupResult struct {
	Path     string
	Bytes    int64
	Took     time.Duration
	Deleted  []string // files DELETED from backups/ by this run's rotation
	Reaped   int      // legacy trash/ files deleted by this run (reapBackupTrash)
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
// `retain` is N — how many files survive PER POOL. Every trigger passes the
// value it read from `backup.retain` (liveBackupRetain); it is a PARAMETER
// rather than a read inside this function so that a test can drive retention
// without a settings table, and so that the number one run acted on is visible
// at the call site.
func runDatabaseBackup(db *sql.DB, dbPath string, reason backupReason, now time.Time, retain int) (backupResult, error) {
	res := backupResult{Reason: reason}
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, fmt.Errorf("create backup dir: %w", err)
	}

	// Report on the state we are walking into BEFORE writing, so "there was no
	// recent retreat point and nobody noticed" is visible in the run that
	// creates one.
	//
	// 🔴 This one deliberately keeps newestBackupTime — it is NOT the same
	// question backupTick and backup_health.go ask, and aligning it would be a
	// change of meaning dressed up as consistency. Those two ask "is the
	// SCHEDULE alive?", for which only a scheduled backup is evidence. This asks
	// "was there ANY restorable snapshot here when I arrived?", and a
	// pre-migration file from an hour ago genuinely IS one — it restores exactly
	// as well as a scheduled one. Narrowing it would make `ocserverd backup` on
	// a machine that has never run `serve` announce a dead schedule that does
	// not exist, and this field gates nothing: its only consumer is the log line
	// below. The schedule-liveness claim now has a durable, cockpit-visible
	// owner (backupHealthMonitor), so this line stops making it — see
	// logBackupOutcome.
	//
	// 🔴 The NEGATIVE branch is a guard, not a rounding detail. `age >
	// staleFactor*interval` is FALSE for every negative value, so a file stamped
	// in the future would make this report "there was a recent retreat point"
	// about a directory whose newest name is pure fiction. This field's entire
	// content IS recency, so it cannot stay quiet about that. It is reported as
	// stale with its OWN reason string: a future stamp is not "no previous
	// backup" (there IS a file, and it may well restore) and it is not an age
	// either — it is "I could not establish that a recent retreat point exists",
	// which is what stale means here.
	//
	// The population above is deliberately left alone (newestBackupTime, every
	// reason). Refusing to trust a nonsensical timestamp is not the same change
	// as narrowing WHICH backups count, and only the latter is forbidden here.
	if newest, ok := newestBackupTime(dir); ok {
		age := now.Sub(newest)
		switch {
		case age < 0:
			res.Stale, res.StaleAge = true, "newest backup is stamped in the future"
		case age > backupStaleFactor*backupInterval:
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

	deleted, err := rotateBackups(dbPath, retain)
	if err != nil {
		// The new backup EXISTS — that is the point of this function. A
		// rotation problem is a disk-growth problem, reported without
		// pretending the snapshot failed.
		log.Printf("[backup] rotation after %s failed: %v", final, err)
	}
	res.Deleted = deleted

	// Drain whatever the old move-based rotation left in trash/. It is bounded,
	// idempotent and normally a no-op (an empty or absent directory), so it
	// rides the same trigger rather than needing a cadence of its own — and a
	// studio that upgrades into this build reclaims its backlog on the very
	// first backup instead of on some later sweep nobody armed.
	reaped, err := reapBackupTrash(dbPath)
	if err != nil {
		log.Printf("[backup] draining trash/ after %s failed: %v", final, err)
	}
	res.Reaped = reaped
	return res, nil
}

// liveBackupRetain resolves N from the `backup.retain` settings row, at the
// moment a backup is taken.
//
// 🔴 It reads the DATABASE rather than the apiServer's settings snapshot, and
// that is deliberate: this engine is not a method on apiServer and holds nothing
// from it (see startBackupCadence for why), while three of its four triggers —
// `ocserverd backup`, the pre-migration hook, and a test harness — run with no
// apiServer in existence at all. One indexed read per BACKUP (not per request)
// is the cheapest way for every trigger to answer the same question from the
// same source, and it means a PATCH from the cockpit takes effect on the next
// snapshot with no restart.
//
// A missing row, an unreadable table (the pre-migration hook runs before goose)
// or a value outside the accepted range all fall back to backupRetainDefault.
// Falling back is safe in the direction that matters: the default is a bounded,
// owner-chosen number, so a corrupt row can never widen this engine's reach —
// and `serve` refuses to boot on an out-of-range row anyway (loadAuthSettings),
// so the fallback is reachable only for the CLI triggers.
func liveBackupRetain(db *sql.DB) int {
	if db == nil {
		return backupRetainDefault
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM setting WHERE key = ?`, settingBackupRetain).Scan(&raw); err != nil {
		return backupRetainDefault
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < minBackupRetain || n > maxBackupRetain {
		return backupRetainDefault
	}
	return n
}

// rotateBackups keeps the newest `keep` files OF EACH POOL and DELETES the rest.
//
// 🔴 IT DELETES. It used to move the evicted file into trash/ and nothing ever
// emptied trash/, so "retired" meant "kept forever under a different name" — the
// directory grew to 141.6 GiB (see the file header). The safety this function
// owes is NOT "the evicted file is still findable somewhere"; it is that it
// can only ever reach files it created ITSELF and only ever the ones already
// beyond N. Both come from backupFilesIn, which is the only listing this
// function has:
//
//	sorted by FILENAME, newest first — the stamp is fixed-width and lexically
//	ordered, so name order IS time order. Deliberately not mtime: mtime survives
//	neither copying a backup directory around nor a restore, and a deleter that
//	sorts by a rewritable key can delete the newest file.
//	filtered to prefix `officraft-` AND suffix `.db`, directories skipped — so
//	a `.partial` (it ends in `.partial`, not `.db`), a hand-made
//	`officraft.db.bak-pre-*` (the prefix is `officraft.`, not `officraft-`) and
//	any subdirectory are all INVISIBLE here: never counted toward N, never
//	deleted.
//	names are unique within a directory by construction, so there is no
//	same-name case to resolve — a file's name decides both its pool
//	(backupPoolOf) and its rank, and one name is one file.
//
// 🔴 `keep` is per pool, not per directory. It was per directory, and that meant
// five backups from ANY trigger could retire the pre-migration snapshot — see
// backupPoolPreMigration for why that was the wrong file to make cheap. Every
// pool is still bounded, so the directory as a whole stays bounded (at
// keep × number-of-pools) instead of growing forever.
func rotateBackups(dbPath string, keep int) ([]string, error) {
	dir := backupDirFor(dbPath)
	files, err := backupFilesIn(dir)
	if err != nil {
		return nil, err
	}
	if keep < 1 {
		return nil, nil
	}
	// backupFilesIn is already newest-first, and grouping preserves that order,
	// so within each pool the survivors are the newest `keep`.
	pools := map[backupPool][]os.DirEntry{}
	for _, e := range files {
		pool := backupPoolOf(e.Name())
		pools[pool] = append(pools[pool], e)
	}

	var overdue []os.DirEntry
	for _, inPool := range pools {
		if len(inPool) > keep {
			overdue = append(overdue, inPool[keep:]...)
		}
	}
	if len(overdue) == 0 {
		return nil, nil
	}
	// Map iteration order is random, so sort back into the directory's own
	// newest-first order: the log line then reads the same way every run.
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].Name() > overdue[j].Name() })

	var deleted []string
	for _, e := range overdue {
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			return deleted, fmt.Errorf("retire %s: %w", e.Name(), err)
		}
		deleted = append(deleted, e.Name())
	}
	return deleted, nil
}

// reapBackupTrash deletes the backlog that the OLD move-based rotation parked in
// `trash/` and never came back for. It is the one-way door's other half: without
// it, switching rotation to delete would stop the bleeding while leaving the
// 141.6 GiB already on disk there permanently, because nothing else on this
// machine will ever read that directory (see the file header).
//
// 🔴 ITS REACH IS EXACTLY ROTATION'S OWN REACH, and for the same reason: it uses
// backupFilesIn, so it can only see files matching `officraft-*.db` that sit
// DIRECTLY in trash/. Subdirectories are skipped (backupFilesIn skips dirs and
// this never recurses), and anything a human or another tool parked there under
// a different name is left exactly where it is. It never removes the trash
// DIRECTORY itself — an empty directory costs nothing and removing it would be
// reach this function has no reason to have.
//
// 🔴 THE SENTENCE ABOVE IS ONLY TRUE BECAUSE OF THE LSTAT BELOW. "Directly in
// trash/" is a claim about the FILESYSTEM, and backupFilesIn cannot make it:
// os.ReadDir FOLLOWS a symlinked `trash`, so `trash -> backups` (they are
// siblings, and relocating trash/ behind a symlink is the most natural thing an
// operator does when 141.6 GiB will not fit on this disk any more) would point
// this deleter at the LIVE backups directory and empty it, newest snapshot
// included. Measured, not reasoned: with this guard removed,
// TestRetention_RefusesToReapThroughASymlinkedTrash reports the reaper deleting
// all three planted backups through the link.
//
// So the trash path is LSTAT'd, NEVER STAT'd, and a symlink is REFUSED — the
// same guard, in the same shape, as G5 in this repo's sister reaper
// cli/ocwarden/trash.go (purgeTrash), which was written for exactly this attack
// and whose comment reads "lstat (never stat)". Two reapers that delete on the
// owner's behalf should not disagree about whether a symlinked trash is
// followable. Refusal is (0, nil) plus a loud log line, not an error: the
// backlog staying on disk is a few stale gigabytes, and failing the whole backup
// run over it would trade a disk-space problem for a missing retreat point.
//
// It stops at THIS layer only, deliberately. purgeTrash also carries G7/G8
// (EvalSymlinks containment) because its root and workdir are strings assembled
// from an agent id — attacker-shaped input. Here the only input is dbPath, and
// the trash path is Join(Dir(dbPath), "trash"): no setting can re-point it, and
// ReadDir's entry names are basenames, so ".." and absolute paths cannot appear.
// The symlinked leaf was the one real hole, and it is the one closed.
//
// 🔴 There is no "keep the newest few" here on purpose. Every file in trash/ was
// ALREADY judged beyond N by the rotation that put it there; re-applying a quota
// would resurrect a retention rule for files that have already been retired
// once, which is a second, invisible retention policy.
//
// A missing trash/ is the normal, healthy state and returns (0, nil).
func reapBackupTrash(dbPath string) (int, error) {
	trash := backupTrashFor(dbPath)
	// LSTAT, NEVER STAT: we must see the LINK, not what it points at. os.Stat
	// here would resolve the link and hand the loop below somebody else's
	// directory. Same guard as cli/ocwarden/trash.go G5.
	if info, err := os.Lstat(trash); err == nil && info.Mode()&os.ModeSymlink != 0 {
		log.Printf("[backup] REFUSED to reclaim the legacy trash backlog: %q is a symlink — refusing to follow it out of the data directory", trash)
		return 0, nil
	}
	files, err := backupFilesIn(trash)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	reaped := 0
	for _, e := range files {
		if err := os.Remove(filepath.Join(trash, e.Name())); err != nil {
			return reaped, fmt.Errorf("reap trash/%s: %w", e.Name(), err)
		}
		reaped++
	}
	return reaped, nil
}

// logBackupOutcome is the single voice of this engine. Every trigger reports
// through it so a reader of the log never has to know which one fired.
func logBackupOutcome(res backupResult, err error) {
	if res.Stale && res.StaleAge != "" {
		// Deliberately its own line: "there was no recent retreat point" and
		// "this run worked" are different facts and a reader must be able to see
		// both.
		//
		// 🔴 It no longer says "the schedule may have stopped running". Stale
		// here counts backups of EVERY reason (see runDatabaseBackup), so it
		// cannot support a claim about the schedule specifically — a directory
		// full of pre-migration snapshots satisfies it while the cadence is
		// dead. That claim belongs to backupHealthMonitor, which measures
		// scheduled backups only and is durable and visible in the cockpit.
		log.Printf("[backup] WARNING newest existing backup was stale (%s) — this studio had no recent retreat point", res.StaleAge)
	}
	switch {
	case err != nil:
		log.Printf("[backup] FAILED (%s): %v — THERE IS NO NEW RETREAT POINT", res.Reason, err)
	case res.Skipped != "":
		log.Printf("[backup] SKIPPED (%s): %s — no new retreat point was created", res.Reason, res.Skipped)
	default:
		log.Printf("[backup] ok (%s): %s (%d MB in %s)", res.Reason, filepath.Base(res.Path), res.Bytes>>20, res.Took.Round(time.Millisecond))
		if len(res.Deleted) > 0 {
			log.Printf("[backup] DELETED %d backup(s) past the retention limit: %s", len(res.Deleted), strings.Join(res.Deleted, ", "))
		}
		if res.Reaped > 0 {
			log.Printf("[backup] reclaimed %d file(s) from the legacy trash/ backlog", res.Reaped)
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
// health may be nil (tests, dependency-free assemblies): the cadence then
// behaves exactly as it did before the cockpit half existed.
func startBackupCadence(db *sql.DB, dbPath string, tick time.Duration, health *backupHealthMonitor) {
	go func() {
		for {
			time.Sleep(tick)
			backupTick(db, dbPath, time.Now(), health)
		}
	}()
}

// backupTick is ONE evaluation, split out so the decision can be tested without
// waiting on a clock. taken=false means a backup was not DUE — which is the
// normal answer and is deliberately silent, unlike a failure or a skip.
//
// 🔴 It asks newestScheduledBackup — the SAME question, through the SAME
// function, that the staleness alarm asks (backup_health.go). It used to ask
// newestBackupTime, which counts every file in the directory regardless of
// reason, and that one-word difference was a real defect on this machine:
//
//	officraft-20260804-123056-premigration.db  +6h = 183056
//	officraft-20260804-183057-scheduled.db     ← the next scheduled one, 1s later
//
// The scheduled backup before it was at 042534, so the SCHEDULE had a 14h05m
// hole while the directory looked busy. It happened three times in three days
// (0802-103012→0803-004908, 0803-075043→0803-222534, 0804-042534→0804-183057),
// each time one second after a pre-migration snapshot aged out of the interval.
// One `ocserverd` upgrade deferred the cadence by a full backupInterval; a
// stretch of upgrades deferred it past the 12h alarm window, so the alarm was
// telling the truth and the cadence was the thing that was wrong.
//
// 🔴 The contradiction was INSIDE this file. backupPoolOf gives pre-migration
// snapshots their own quota precisely because they are NOT interchangeable with
// routine coverage (see backupPoolPreMigration) — while this tick was treating
// one as a substitute for the routine backup it displaced. Widening the alarm
// instead would have been the wrong repair: with 18 pre-migration snapshots on
// this machine in five days, a cadence that had stopped completely would have
// stayed green.
//
// The cost is accepted and small: a snapshot taken by hand no longer defers the
// schedule either, so a manual backup may be followed by a scheduled one within
// the tick. They share the routine quota, rotation MOVES rather than deletes,
// and the pre-migration pool is untouchable by it.
func backupTick(db *sql.DB, dbPath string, now time.Time, health *backupHealthMonitor) (taken bool) {
	// newestScheduledBackup is asked AS OF `now`, so it can never hand back a
	// stamp from the future — which matters here more than anywhere: this
	// comparison is `< backupInterval`, and that is TRUE for every negative
	// value, so a single future-stamped file would make this tick answer "just
	// backed up" forever and stop backups outright. See newestScheduledBackup.
	if newest, ok := newestScheduledBackup(dbPath, now); ok && now.Sub(newest) < backupInterval {
		return false
	}
	res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now, liveBackupRetain(db))
	logBackupOutcome(res, err)
	// T-da06: the log line above has no reader on this machine. This is the
	// same outcome, told to something the cockpit can see. It reports the
	// failure IMMEDIATELY — the watchdog alone would only notice one stale
	// window (12h) later, by which time the owner has believed in a retreat
	// point for half a day.
	health.noteScheduledOutcome(res, err, now)
	return err == nil && res.Skipped == ""
}

// backupBeforeMigrations is trigger ③. Both of its callers — cmdServe
// (server.go) and cmdMigrate (migrate.go) — MUST call it before their goose
// call: a snapshot taken after `goose up` has committed is a copy of the
// outcome, not a retreat from it.
//
// 🔴 That MUST is not left as a norm. BOTH doors are pinned, by the same
// criterion and the same helpers, so reordering either one turns something red:
//
//	TestServeTakesPreMigrationBackupBeforeGoose   (serve_backup_order_t74_test.go)
//	TestMigrateTakesPreMigrationBackupBeforeGoose (migrate_backup_t74_test.go)
//
// Both read the SNAPSHOT'S OWN CONTENTS rather than merely checking that a file
// appeared — an existence-only assertion is satisfied identically by both
// orderings, so it has no power over the failure worth guarding. The snapshot
// must not contain goose_db_version (goose creates that table, so its absence
// is only possible if the copy predates goose), and the live database must
// contain it afterwards so the first clause cannot pass vacuously against a
// migration that never ran. A THIRD caller added later needs its own test:
// these two guard their own call sites, not this function's every future user.
//
// It runs BEFORE goose, and only when
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
	res, err := runDatabaseBackup(db, dbPath, backupReasonPreMigration, now, liveBackupRetain(db))
	logBackupOutcome(res, err)
	if err != nil || res.Skipped != "" {
		log.Printf("[backup] proceeding with migrations WITHOUT a fresh pre-migration backup")
	}
}
