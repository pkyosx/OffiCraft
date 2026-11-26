// Skeleton generated from server/ocserverd/auto_update.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// autoUpdateTestRig is an armed station whose running binary sits in a temp
// dir, backed by a release source that counts the requests it answers, so a
// tick that skipped the attempt can be told apart from one that failed it.
type autoUpdateTestRig struct {
	api       *apiServer
	exe       string
	hits      *atomic.Int64
	restarted chan string
}

func newAutoUpdateTestRig(t *testing.T, tag string, missing ...string) autoUpdateTestRig {
	t.Helper()
	srv := upgradeTestReleaseServer(t, tag, "#!/bin/sh\nexit 0\n", missing...)
	hits := &atomic.Int64{}
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(counting.Close)
	exe := filepath.Join(t.TempDir(), "ocserverd")
	if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}
	restarted := make(chan string, 4)
	api := &apiServer{
		updaterAutoUpdate:        true,
		updaterCheckIntervalSecs: 300,
		releaseSiteBase:          counting.URL,
		releasePlatform:          upgradeTestReleasePlatform,
		upgradeExeOverride:       exe,
		upgradeRestart:           func(path string, _ []string) { restarted <- path },
	}
	upgradeTestKnownRelease(t, api, tag)
	return autoUpdateTestRig{api: api, exe: exe, hits: hits, restarted: restarted}
}

// tick answers whether this tick asked the release source anything.
func (r autoUpdateTestRig) tick(t *testing.T, now time.Time) (acted, attempted bool) {
	t.Helper()
	before := r.hits.Load()
	acted = r.api.autoUpdateTickAt(now)
	return acted, r.hits.Load() != before
}

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
			releasePlatform:          upgradeTestReleasePlatform,
			upgradeExeOverride:       exe,
			upgradeRestart:           func(path string, _ []string) { restarted <- path },
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

func TestAutoUpdateTickGate(t *testing.T) {
	t0 := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)

	t.Run("consecutive failures back off; a newer release or a manual click resumes", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.3", "officraft-v1.2.3-linux-riscv64.tar.gz")
		for _, step := range []struct {
			at      time.Duration
			attempt bool
		}{
			{0, true},
			{time.Minute, false},
			{4*time.Minute + 59*time.Second, false},
			{5 * time.Minute, true},
			{14 * time.Minute, false},
			{15 * time.Minute, true},
			{34 * time.Minute, false},
		} {
			acted, attempted := rig.tick(t, t0.Add(step.at))
			if acted || attempted != step.attempt {
				t.Fatalf("tick at +%v: acted=%v attempted=%v, want attempted=%v", step.at, acted, attempted, step.attempt)
			}
		}

		// +34m30s is inside the third backoff (+15m … +35m), so only the
		// newer tag can explain an attempt here.
		upgradeTestKnownRelease(t, rig.api, "v1.2.4")
		if _, attempted := rig.tick(t, t0.Add(34*time.Minute+30*time.Second)); !attempted {
			t.Fatal("a newer release did not resume attempts during the backoff")
		}
		if _, attempted := rig.tick(t, t0.Add(36*time.Minute)); attempted {
			t.Fatal("the failure against the newer release was not backed off")
		}

		rec := httptest.NewRecorder()
		rig.api.HandleUpgradeApiUpdateUpgradePost(rec, httptest.NewRequest(http.MethodPost, "/api/update/upgrade", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("manual upgrade: %d %s", rec.Code, rec.Body)
		}
		if _, attempted := rig.tick(t, t0.Add(36*time.Minute)); !attempted {
			t.Fatal("a manual click did not resume auto-update attempts")
		}
		if got := upgradeTestDirEntries(t, filepath.Dir(rig.exe)); !reflect.DeepEqual(got, []string{"ocserverd"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})

	t.Run("a restart that came back on the old version stops retrying that tag until a newer release", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.4")
		upgradeTestKnownRelease(t, rig.api, "v1.2.3")
		rig.api.adoptUpgradeHandoff(appVersion+" v1.2.3", appVersion)

		for _, at := range []time.Duration{0, time.Hour, 24 * time.Hour} {
			if acted, attempted := rig.tick(t, t0.Add(at)); acted || attempted {
				t.Fatalf("tick at +%v retried the tag that did not take: acted=%v attempted=%v", at, acted, attempted)
			}
		}

		upgradeTestKnownRelease(t, rig.api, "v1.2.4")
		if acted, _ := rig.tick(t, t0.Add(25*time.Hour)); !acted {
			t.Fatal("a newer release was not installed")
		}
		if got := <-rig.restarted; got != rig.exe {
			t.Fatalf("restart path: %q", got)
		}
	})

	t.Run("a restart that came back on the new version holds nothing", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.3")
		rig.api.adoptUpgradeHandoff("v1.2.2 v1.2.3", appVersion)
		if acted, _ := rig.tick(t, t0); !acted {
			t.Fatal("auto-update was held after an upgrade that took")
		}
	})

	t.Run("a manual click releases a held tag", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.3")
		rig.api.adoptUpgradeHandoff(appVersion+" v1.2.3", appVersion)
		rig.api.resetAutoUpdateGate()
		if acted, _ := rig.tick(t, t0); !acted {
			t.Fatal("auto-update stayed held after the gate was reset")
		}
	})

	t.Run("after a swap whose exec failed, neither auto-update nor a click re-downloads, even when the cached tag lags the fetched one", func(t *testing.T) {
		// GitHub already marks v1.2.4 Latest while the cached check still says
		// v1.2.3: the swap installs v1.2.4, and the restart seam returns the
		// way a failed syscall.Exec does on macOS.
		rig := newAutoUpdateTestRig(t, "v1.2.4")
		upgradeTestKnownRelease(t, rig.api, "v1.2.3")
		if acted, _ := rig.tick(t, t0); !acted {
			t.Fatal("first tick did not upgrade")
		}
		<-rig.restarted

		for _, at := range []time.Duration{time.Minute, time.Hour} {
			if acted, attempted := rig.tick(t, t0.Add(at)); acted || attempted {
				t.Fatalf("tick at +%v re-ran the upgrade: acted=%v attempted=%v", at, acted, attempted)
			}
		}
		// The tick itself must stand down, not reach runUpgrade's refusal and
		// log a "failure" with a backoff for a restart that is only pending.
		if n := rig.api.autoUpdateGate.failures; n != 0 {
			t.Fatalf("pending restart was counted as %d failed attempt(s)", n)
		}
		before := rig.hits.Load()
		rec := httptest.NewRecorder()
		rig.api.HandleUpgradeApiUpdateUpgradePost(rec, httptest.NewRequest(http.MethodPost, "/api/update/upgrade", nil))
		var body struct {
			Error struct{ Message string } `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusConflict ||
			body.Error.Message != "a new version (v1.2.4) is already installed and waits for the server to restart; nothing was changed" {
			t.Fatalf("manual upgrade: %d %s", rec.Code, rec.Body)
		}
		if rig.hits.Load() != before {
			t.Fatal("the manual click asked the release source again")
		}
		if got := upgradeTestDirEntries(t, filepath.Dir(rig.exe)); !reflect.DeepEqual(got, []string{"ocserverd", "ocserverd.bak"}) {
			t.Fatalf("binary directory: %v", got)
		}
		if data, err := os.ReadFile(rig.exe + ".bak"); err != nil || string(data) != "running binary" {
			t.Fatalf("rollback copy: %q, %v", data, err)
		}
	})

	t.Run("losing the lock to a running upgrade is a skip, not a failure", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.3")
		rig.api.upgradeMu.Lock()
		acted, attempted := rig.tick(t, t0)
		rig.api.upgradeMu.Unlock()
		if acted || attempted {
			t.Fatalf("tick under a held lock: acted=%v attempted=%v", acted, attempted)
		}
		if acted, _ := rig.tick(t, t0.Add(time.Minute)); !acted {
			t.Fatal("the busy tick backed off the next one")
		}
	})

	t.Run("a swap whose restart returned does not re-download the same tag", func(t *testing.T) {
		rig := newAutoUpdateTestRig(t, "v1.2.3")
		if acted, _ := rig.tick(t, t0); !acted {
			t.Fatal("first tick did not upgrade")
		}
		<-rig.restarted
		if acted, attempted := rig.tick(t, t0.Add(time.Minute)); acted || attempted {
			t.Fatalf("second tick re-ran the upgrade: acted=%v attempted=%v", acted, attempted)
		}
		if got := upgradeTestDirEntries(t, filepath.Dir(rig.exe)); !reflect.DeepEqual(got, []string{"ocserverd", "ocserverd.bak"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})
}

func TestAutoUpdateBackoff(t *testing.T) {
	for failures, want := range map[int]time.Duration{
		1:   5 * time.Minute,
		2:   10 * time.Minute,
		3:   20 * time.Minute,
		7:   320 * time.Minute,
		8:   6 * time.Hour,
		100: 6 * time.Hour,
	} {
		if got := autoUpdateBackoff(failures); got != want {
			t.Errorf("autoUpdateBackoff(%d) = %v, want %v", failures, got, want)
		}
	}
}

func TestUpgradeHandoffEnv(t *testing.T) {
	got := upgradeHandoffEnv([]string{"A=1", upgradeHandoffEnvKey + "=old v0", "B=2"}, "v1.0.0", "v1.1.0")
	want := []string{"A=1", "B=2", upgradeHandoffEnvKey + "=v1.0.0 v1.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env: %v, want %v", got, want)
	}
}

func TestAdoptInheritedUpgradeHandoff(t *testing.T) {
	t.Setenv(upgradeHandoffEnvKey, appVersion+" v9.9.9")
	api := &apiServer{}

	api.adoptInheritedUpgradeHandoff()

	if v, set := os.LookupEnv(upgradeHandoffEnvKey); set {
		t.Fatalf("handoff still in the environment: %q", v)
	}
	if api.autoUpdateAllowed("v9.9.9", time.Now()) {
		t.Fatal("the tag that did not take is not held")
	}
	if !api.autoUpdateAllowed("v9.9.10", time.Now()) {
		t.Fatal("a newer tag is held")
	}
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
