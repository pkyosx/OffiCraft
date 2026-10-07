// Skeleton generated from server/ocserverd/auto_update.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutoUpdateEnabled(t *testing.T) {
	api := &apiServer{}
	if api.autoUpdateEnabled() {
		t.Fatal("auto-update is enabled before the toggle is armed")
	}

	api.updaterAutoUpdate = true
	if !api.autoUpdateEnabled() {
		t.Fatal("auto-update is disabled after the toggle is armed")
	}
}

func TestStartAutoUpdateCadence(t *testing.T) {
	api := &apiServer{}
	api.startAutoUpdateCadence(time.Hour)
}

func TestAutoUpdateTick(t *testing.T) {
	t.Run("disabled auto-update does not evaluate a release", func(t *testing.T) {
		api := &apiServer{}
		if api.autoUpdateTick() {
			t.Fatal("disabled auto-update acted")
		}
	})

	t.Run("armed auto-update with no newer cached release does nothing", func(t *testing.T) {
		api := &apiServer{updaterAutoUpdate: true, updaterCheckIntervalSecs: 300}
		api.updateCheck = updateCheckState{checkedAt: time.Now()}
		if api.autoUpdateTick() {
			t.Fatal("auto-update acted without a newer cached release")
		}
	})

	t.Run("armed auto-update with a newer verifiable release installs it and restarts", func(t *testing.T) {
		srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		restarted := make(chan string, 1)
		api := &apiServer{
			updaterAutoUpdate:        true,
			updaterCheckIntervalSecs: 300,
			releaseSiteBase:          srv.URL,
			upgradeExeOverride:       exe,
			upgradeRestart:           func(path string) { restarted <- path },
		}
		upgradeTestKnownRelease(t, api, "v1.2.3")

		if !api.autoUpdateTick() {
			t.Fatal("armed auto-update did not act on a newer release")
		}
		select {
		case got := <-restarted:
			if got != exe {
				t.Fatalf("restart path: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("restart seam was not called")
		}
		installed, err := os.ReadFile(exe)
		if err != nil || string(installed) != "#!/bin/sh\nexit 0\n" {
			t.Fatalf("installed binary: %q, %v", installed, err)
		}
	})
}

func TestDerefOr(t *testing.T) {
	var absent *string
	if got := derefOr(absent, "unknown"); got != "unknown" {
		t.Fatalf("nil value = %q, want %q", got, "unknown")
	}

	value := "v1.2.3"
	if got := derefOr(&value, "unknown"); got != "v1.2.3" {
		t.Fatalf("present value = %q, want %q", got, "v1.2.3")
	}
}
