// Skeleton generated from cli/ocwarden/cutover.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestRealCutoverOps(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPsExePath(t *testing.T) {
	t.Skip("TODO: psExePath reads a pid's executable path via `ps -p <pid> -o comm=`, which on macOS prints the full path.")
}

func TestSpawnDetachedProcess(t *testing.T) {
	t.Skip("TODO: spawnDetachedProcess starts bin in its OWN SESSION (Setsid) and releases it, so it is not in this process's process group and survives `launchctl bootout` of this job — measured on macOS 26.5, and the whole reason no one-shot launchd job or extra daemon is needed.")
}

func TestDetectShape(t *testing.T) {
	t.Skip("TODO: detectShape answers what launchd is ACTUALLY running, from the parent process's executable path — the one signal that cannot be wrong: - parent is the anchor → anchor (launchd → officraft → ocwarden) - parent is launchd (pid 1) → legacy (launchd → ocwarden run) - anything else (a shell, a test) → unknown (NOT a verdict; never convert) Deliberately NOT \"does ~/.officraft/warden/officraft exist\": a machine can have the anchor file on disk and still be booted from a legacy plist, which is the exact state this migration exists to fix.")
}

func TestNewShapeReporter(t *testing.T) {
	t.Skip("TODO: newShapeReporter builds the 30s heartbeat's warden-shape collector: the same detectShape verdict the startup hook computes for its own go/no-go decision, re-read once per cycle so the FLEET can tell a converted machine from an unconverted one.")
}

func TestEnsureAnchorPresent(t *testing.T) {
	t.Skip("TODO: ensureAnchorPresent materialises the anchor when this machine has none, so the preflight below has something real to probe.")
}

func TestAnchorPreflight(t *testing.T) {
	t.Skip("TODO: anchorPreflight proves the anchor is present, executable and not quarantined, with ZERO side effects: `officraft <any arg>` prints usage and exits 2 (see cli/officraft/main.go realMain) — it forks nothing.")
}

func TestRealRunExit(t *testing.T) {
	t.Skip("TODO: realRunExit runs a command purely to observe its exit code.")
}

func TestMaybeStartAnchorCutover(t *testing.T) {
	t.Skip("TODO: maybeStartAnchorCutover is the `ocwarden run` startup hook.")
}

func TestAcquireCutoverLock(t *testing.T) {
	t.Skip("TODO: acquireCutoverLock takes the O_EXCL lock, aging out a corpse older than staleLockAge so a machine killed mid-conversion is not locked out forever.")
}

func TestRunInstallerCombined(t *testing.T) {
	t.Skip("TODO: runInstallerCombined runs `ocwarden install --force` and returns its output WHETHER OR NOT IT FAILED.")
}
