package main

// auto_update_test.go — the opt-in background self-upgrade cadence
// (auto_update.go): OFF means the tick never acts (the shipped default), ON
// runs the same verified swap body as the manual endpoint against the newest
// GitHub release, and the TryLock makes auto and manual triggers mutually
// exclusive.

import (
	"os"
	"testing"
	"time"
)

// armAutoUpdate flips the live toggle the way the PATCH handler does.
func armAutoUpdate(api *apiServer, on bool) {
	api.settingsMu.Lock()
	api.updaterAutoUpdate = on
	api.settingsMu.Unlock()
}

func TestAutoUpdateTickOffByDefaultNeverActs(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	// A newer release IS available — but the toggle is off (default).
	gh := githubWithRelease(t, "v0.8.0", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("an unarmed tick must never act")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickUpgradesWhenArmed(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.8.1", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); !acted {
		t.Fatal("an armed tick with a newer release must act")
	}
	// The exact same postconditions as a manual upgrade: verified swap landed,
	// .bak escape hatch present, restart scheduled through the seam.
	got, err := os.ReadFile(exePath)
	if err != nil || string(got) != smokePassingBinary {
		t.Fatalf("exe not swapped: %q %v", got, err)
	}
	bak, err := os.ReadFile(exePath + ".bak")
	if err != nil || string(bak) != "OLD-BINARY" {
		t.Fatalf(".bak must hold the OLD binary: %q %v", bak, err)
	}
	select {
	case p := <-restarted:
		if p != exePath {
			t.Fatalf("restart pointed at %q, want %q", p, exePath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart never fired after an armed auto-update")
	}
}

func TestAutoUpdateTickNoopWhenUpToDate(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	// GitHub's latest IS the running build's tag: armed or not, nothing to do.
	setAppVersion(t, "v0.8.2")
	gh := githubWithRelease(t, "v0.8.2", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("an up-to-date armed tick must not act")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickFailureLeavesOldBinary(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	// Digest-valid artifact that dies at the smoke gate: the armed tick tries,
	// fails honestly, and the old binary keeps serving (retry rides the
	// natural cadence, not a tight loop).
	gh := githubWithRelease(t, "v0.8.3", smokeFailingBinary, "", true)
	pointAtGitHub(t, api, gh)
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("a failed swap must report acted=false")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickLosesToInFlightManualUpgrade(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.8.4", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)
	armAutoUpdate(api, true)

	// A manual upgrade holds the lock: the armed tick must answer "already in
	// progress" (acted=false) instead of racing a second swap.
	if !api.upgradeMu.TryLock() {
		t.Fatal("test setup: lock must be free")
	}
	defer api.upgradeMu.Unlock()
	if acted := api.autoUpdateTick(); acted {
		t.Fatal("a tick during an in-flight upgrade must not act")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}
