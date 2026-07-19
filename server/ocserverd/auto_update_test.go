package main

// auto_update_test.go — the opt-in background self-upgrade cadence
// (auto_update.go): OFF means the tick never acts (the shipped default), ON
// runs the same verified swap body as the manual endpoint, and the TryLock
// makes auto and manual triggers mutually exclusive.

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
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// A newer version IS available — but the toggle is off (default).
	up := fakeUpdaterFull(t, "inv-1", "v260714-0002", "new-sha", []byte(smokePassingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("an unarmed tick must never act")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickUpgradesWhenArmed(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	up := fakeUpdaterFull(t, "inv-1", "v260714-0003", "new-sha", []byte(smokePassingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); !acted {
		t.Fatal("an armed tick with a newer version must act")
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
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// The updater's latest IS the running build (same git sha): armed or not,
	// there is nothing to do.
	up := fakeUpdaterFull(t, "inv-1", "v260714-0004", "run-sha", []byte(smokePassingBinary), "")
	api.processSHA = "run-sha"
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("an up-to-date armed tick must not act")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickFailureLeavesOldBinary(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// Digest-valid artifact that dies at the smoke gate: the armed tick tries,
	// fails honestly, and the old binary keeps serving (retry rides the
	// natural cadence, not a tight loop).
	up := fakeUpdaterFull(t, "inv-1", "v260714-0005", "new-sha", []byte(smokeFailingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
	armAutoUpdate(api, true)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("a failed swap must report acted=false")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestAutoUpdateTickLosesToInFlightManualUpgrade(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	up := fakeUpdaterFull(t, "inv-1", "v260714-0006", "new-sha", []byte(smokePassingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
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
