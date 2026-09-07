// Skeleton generated from cli/ocwarden/cutover_run.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestCutoverCmd(t *testing.T) {
	t.Skip("TODO: cutoverCmd is the `ocwarden cutover-anchor` entry point.")
}

func TestReleaseCutoverLock(t *testing.T) {
	t.Skip("TODO: releaseCutoverLock drops the lock the parent took.")
}

func TestRunCutover(t *testing.T) {
	t.Skip("TODO: runCutover is the testable core: every effect goes through ops, so no test can reach a real launchctl or a real ~/Library/LaunchAgents path.")
}

func TestRollback(t *testing.T) {
	t.Skip("TODO: rollback puts the old plist back and makes launchd actually read it.")
}

func TestCutoverWaitGone(t *testing.T) {
	t.Skip("TODO: cutoverWaitGone mirrors install's bootoutUntilGone: bootout is ASYNC, and a bootstrap issued while the dying registration lingers fails with \"Bootstrap failed: 5\".")
}

func TestCutoverWaitRegistered(t *testing.T) {
	t.Skip("TODO: cutoverWaitRegistered is a HAND COPY of install's registeredUntilFound, for the same reason cutoverWaitGone is a hand copy of bootoutUntilGone: registeredUntilFound is typed on sysOps and this path runs on cutoverOps (the Go types do not meet), and keeping it local means the ROLLBACK path cannot be broken by a future change to the install path's bounds.")
}

func TestCutoverVerifyAlive(t *testing.T) {
	t.Skip("TODO: cutoverVerifyAlive requires the restored job to hold ONE pid across a settle window.")
}

func TestCutoverPID(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestWriteCutoverSentinel(t *testing.T) {
	t.Skip("TODO: writeCutoverSentinel records that this machine tried the conversion and ended up back on the old shape.")
}
