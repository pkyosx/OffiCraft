package main

// backup.go — the database backup engine: one implementation, several triggers,
// so the path verified by hand is the one that runs unattended:
//
//	①  `ocserverd backup` (cmdBackup, migrate.go) — by hand.
//	②  the serve cadence (startBackupCadence) — always mounted.
//	③  before goose migrations (backupBeforeMigrations).
//	④  (not in this file) the cockpit's manual-backup button, which calls the
//	    same runDatabaseBackup.
//
// Backups live on the SAME MACHINE as the database: they cover a corrupt file or
// a bad migration, not losing the machine (off-machine backup is a separate
// ticket).

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
	// backupCadence is how often the loop WAKES, not how often it backs up: a
	// short tick lets a server that was down over a backup window take one
	// shortly after boot.
	backupCadence = 15 * time.Minute

	backupInterval = 6 * time.Hour

	// backupRetainDefault (5) is the owner's ruling (T-ada9); making N adjustable
	// did not move it.
	backupRetainDefault = 5

	// Floor 1: zero would delete the snapshot just taken. Ceiling 20: N is a disk
	// budget (~712 MB snapshot × 2 pools × N); past it the knob recreates the
	// unbounded growth retention exists to end.
	minBackupRetain = 1
	maxBackupRetain = 20

	backupFreeSpaceFactor = 3

	backupStaleFactor = 2

	backupFilePrefix = "officraft-"
	backupFileSuffix = ".db"
)

type backupReason string

const (
	backupReasonManual       backupReason = "manual"
	backupReasonScheduled    backupReason = "scheduled"
	backupReasonPreMigration backupReason = "premigration"
)

type backupPool string

const (
	backupPoolRoutine backupPool = "routine"

	// backupPoolPreMigration has its own quota because it is the only retreat
	// from a bad migration: in one shared pool, five manual snapshots taken while
	// investigating the breakage evicted it within minutes.
	backupPoolPreMigration backupPool = "premigration"
)

// backupPoolOf sends an unrecognised label to the routine pool: a pool per
// unknown label would let a typo create a directory that nothing rotates.
func backupPoolOf(name string) backupPool {
	if backupReasonIn(name) == backupReasonPreMigration {
		return backupPoolPreMigration
	}
	return backupPoolRoutine
}

func backupReasonIn(name string) backupReason {
	rest := strings.TrimSuffix(strings.TrimPrefix(name, backupFilePrefix), backupFileSuffix)
	parts := strings.Split(rest, "-")
	if len(parts) < 3 {
		return ""
	}
	return backupReason(strings.Join(parts[2:], "-"))
}

func backupDirFor(dbPath string) string   { return filepath.Join(filepath.Dir(dbPath), "backups") }
func backupTrashFor(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "trash") }

type backupResult struct {
	Path     string
	Bytes    int64
	Took     time.Duration
	Deleted  []string
	Reaped   int
	Skipped  string
	Reason   backupReason
	Stale    bool
	StaleAge string
}

func freeBytesAt(dir string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}

// backupFilesIn is the whole reach of rotation and the trash reaper. The
// hand-made `officraft.db.bak-pre-*` snapshots that predate this engine must
// stay invisible to it: never counted, never deleted.
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
	// Sort by NAME (fixed-width stamp), not mtime: a copy or restore rewrites
	// mtime, and a deleter sorting by a rewritable key can delete the newest file.
	sort.Slice(mine, func(i, j int) bool { return mine[i].Name() > mine[j].Name() })
	return mine, nil
}

func newestBackupTime(dir string) (time.Time, bool) {
	files, err := backupFilesIn(dir)
	if err != nil || len(files) == 0 {
		return time.Time{}, false
	}
	return parseBackupStamp(files[0].Name())
}

func parseBackupStamp(name string) (time.Time, bool) {
	rest := strings.TrimPrefix(name, backupFilePrefix)
	rest = strings.TrimSuffix(rest, backupFileSuffix)
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

func backupFileName(now time.Time, reason backupReason) string {
	return fmt.Sprintf("%s%s-%s%s", backupFilePrefix, now.UTC().Format("20060102-150405"), reason, backupFileSuffix)
}

func runDatabaseBackup(db *sql.DB, dbPath string, reason backupReason, now time.Time, retain int) (backupResult, error) {
	res := backupResult{Reason: reason}
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, fmt.Errorf("create backup dir: %w", err)
	}

	// Deliberately counts backups of EVERY reason (newestBackupTime), unlike
	// backupTick and backup_health.go, which count scheduled ones only: this asks
	// "was there any restorable snapshot?", not "is the schedule alive?".
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
	// VACUUM INTO is SQLite's online backup: it reads the "-wal" sidecar and
	// writes one consistent file, whereas a `cp` under WAL can silently omit the
	// latest commits. Readers keep going, but WRITERS wait the whole duration:
	// this Exec holds the write pool's only connection (openSQLite caps it at 1).
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
		log.Printf("[backup] could not tighten permissions on %s: %v", final, err)
	}
	if info, err := os.Stat(final); err == nil {
		res.Bytes = info.Size()
	}
	res.Path = final

	deleted, err := rotateBackups(dbPath, retain)
	if err != nil {
		log.Printf("[backup] rotation after %s failed: %v", final, err)
	}
	res.Deleted = deleted

	reaped, err := reapBackupTrash(dbPath)
	if err != nil {
		log.Printf("[backup] draining trash/ after %s failed: %v", final, err)
	}
	res.Reaped = reaped
	return res, nil
}

// liveBackupRetain reads the DATABASE, not the apiServer's settings: the CLI
// and pre-migration triggers run with no apiServer, and a cockpit PATCH takes
// effect on the next snapshot. The fallback is reachable only from the CLI
// triggers — `serve` refuses to boot on an out-of-range row (loadAuthSettings).
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

func rotateBackups(dbPath string, keep int) ([]string, error) {
	dir := backupDirFor(dbPath)
	files, err := backupFilesIn(dir)
	if err != nil {
		return nil, err
	}
	if keep < 1 {
		return nil, nil
	}
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

// reapBackupTrash drains `trash/`, the backlog left by the old move-based
// rotation. Nothing else ever reads trash/ (the warden's purgeTrash refuses this
// path), so never point an eviction at it again. No keep-newest quota here:
// every file in it was already judged beyond N.
//
// The trash path is LSTAT'd and a symlink refused: os.ReadDir follows a
// symlinked `trash -> backups`, and this loop would empty the live backups
// (measured). Same guard as G5 in cli/ocwarden/trash.go (purgeTrash).
func reapBackupTrash(dbPath string) (int, error) {
	trash := backupTrashFor(dbPath)
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

func logBackupOutcome(res backupResult, err error) {
	if res.Stale && res.StaleAge != "" {
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

func startBackupCadence(db *sql.DB, dbPath string, tick time.Duration, health *backupHealthMonitor) {
	go func() {
		for {
			time.Sleep(tick)
			backupTick(db, dbPath, time.Now(), health)
		}
	}()
}

// backupTick asks newestScheduledBackup, not newestBackupTime: counting every
// reason let each pre-migration snapshot defer the schedule by a full interval
// (a 14h hole, three times in three days).
func backupTick(db *sql.DB, dbPath string, now time.Time, health *backupHealthMonitor) (taken bool) {
	// newestScheduledBackup is asked as of `now`, so it never returns a future
	// stamp: `< backupInterval` is true for every negative age, and one
	// future-stamped file would stop backups forever.
	if newest, ok := newestScheduledBackup(dbPath, now); ok && now.Sub(newest) < backupInterval {
		return false
	}
	res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now, liveBackupRetain(db))
	logBackupOutcome(res, err)
	health.noteScheduledOutcome(res, err, now)
	return err == nil && res.Skipped == ""
}

// backupBeforeMigrations must run BEFORE goose at both callers, cmdServe
// (server.go) and cmdMigrate (migrate.go): a snapshot taken after `goose up` is
// a copy of the outcome, not a retreat from it. A check of either door must read
// the snapshot's contents (no goose_db_version), not just that a file appeared;
// a third caller needs its own check.
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
