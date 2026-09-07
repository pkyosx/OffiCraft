// Skeleton generated from server/ocserverd/backup.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestBackupPoolOf(t *testing.T) {
	t.Skip("TODO: backupPoolOf decides which quota a file counts against, reading the reason back out of the filename.")
}

func TestBackupReasonIn(t *testing.T) {
	t.Skip("TODO: backupReasonIn pulls the reason field out of `officraft-<date>-<time>-<reason>.db`.")
}

func TestFreeBytesAt(t *testing.T) {
	t.Skip("TODO: freeBytesAt reports the free space on the filesystem holding dir.")
}

func TestBackupFilesIn(t *testing.T) {
	t.Skip("TODO: backupFilesIn lists this engine's own backup files, newest first.")
}

func TestNewestBackupTime(t *testing.T) {
	t.Skip("TODO: newestBackupTime parses the timestamp back out of the newest filename.")
}

func TestParseBackupStamp(t *testing.T) {
	t.Skip("TODO: parseBackupStamp pulls the UTC stamp out of `officraft-<stamp>-<reason>.db`.")
}

func TestRunDatabaseBackup(t *testing.T) {
	t.Skip("TODO: runDatabaseBackup takes ONE snapshot.")
}

func TestLiveBackupRetain(t *testing.T) {
	t.Skip("TODO: liveBackupRetain resolves N from the `backup.retain` settings row, at the moment a backup is taken.")
}

func TestRotateBackups(t *testing.T) {
	t.Skip("TODO: rotateBackups keeps the newest `keep` files OF EACH POOL and DELETES the rest.")
}

func TestReapBackupTrash(t *testing.T) {
	t.Skip("TODO: reapBackupTrash deletes the backlog that the OLD move-based rotation parked in `trash/` and never came back for.")
}

func TestLogBackupOutcome(t *testing.T) {
	t.Skip("TODO: logBackupOutcome is the single voice of this engine.")
}

func TestStartBackupCadence(t *testing.T) {
	t.Skip("TODO: startBackupCadence mounts the background loop.")
}

func TestBackupTick(t *testing.T) {
	t.Skip("TODO: backupTick is ONE evaluation, split out so the decision can be tested without waiting on a clock.")
}

func TestBackupBeforeMigrations(t *testing.T) {
	t.Skip("TODO: backupBeforeMigrations is trigger ③.")
}
