package main

// upgrade_test.go — the upgrade execution body (upgrade.go): the success path
// (download → verify → smoke → backup → swap → restart) and the failure paths
// that MUST leave the old binary untouched and serving.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// smokePassingBinary is a tiny artifact whose `--help` exits 0 on any POSIX
// machine — a stand-in "new ocserverd" the smoke test accepts.
const smokePassingBinary = "#!/bin/sh\nexit 0\n"

// smokeFailingBinary exits non-zero: hash-valid but must die at the smoke gate.
const smokeFailingBinary = "#!/bin/sh\nexit 7\n"

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fakeUpdaterFull is an httptest ocupdaterd speaking BOTH /api/latest and
// /api/binary. declaredSHA lets a test lie about the digest (corruption);
// "" means "declare the payload's true sha".
func fakeUpdaterFull(t *testing.T, invite, version, gitSHA string, payload []byte, declaredSHA string) *httptest.Server {
	t.Helper()
	if declaredSHA == "" {
		declaredSHA = sha256hex(payload)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+invite {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": version, "git_sha": gitSHA, "sha256": declaredSHA,
				"size": len(payload), "notes": "", "published_at": 1.0,
			})
		case "/api/binary":
			if r.URL.Query().Get("version") != version {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("X-Checksum-Sha256", declaredSHA)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newUpgradeTestServer stands up the full handler stack plus the upgrade test
// seams: a scratch "running binary" file and a captured restart.
func newUpgradeTestServer(t *testing.T) (api *apiServer, srvURL, token, exePath string, restarted chan string) {
	t.Helper()
	apiSrv, srv, _, _ := newSettingsTestServer(t, "up-pass")
	token = ownerLogin(t, srv.URL, "up-pass")
	exePath = filepath.Join(t.TempDir(), "ocserverd")
	if err := os.WriteFile(exePath, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	restarted = make(chan string, 1)
	apiSrv.upgradeExeOverride = exePath
	apiSrv.upgradeRestart = func(p string) { restarted <- p }
	return apiSrv, srv.URL, token, exePath, restarted
}

// configureUpdater PATCHes the updater settings and waits for the background
// check to settle so the precondition gate sees the fake's latest.
func configureUpdater(t *testing.T, api *apiServer, srvURL, token, updaterURL, invite string) {
	t.Helper()
	status, _ := doJSON(t, "PATCH", srvURL+"/api/settings", token, fmt.Sprintf(
		`{"updater_url":%q,"updater_invite_code":%q}`, updaterURL, invite))
	if status != 200 {
		t.Fatalf("settings patch: %d", status)
	}
	waitUpdateSettled(t, api)
}

func TestUpgradeSuccessSwapsBackupsAndRestarts(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	payload := []byte(smokePassingBinary)
	up := fakeUpdaterFull(t, "inv-1", "v260713-0002", "new-sha", payload, "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 200 {
		t.Fatalf("valid upgrade must 200: %d %v", status, data)
	}
	if data["status"] != "restarting" || data["target_version"] != "v260713-0002" {
		t.Fatalf("UpgradeResultDTO face wrong: %v", data)
	}
	// The swap landed BEFORE the 200: new bytes at the exe path...
	got, err := os.ReadFile(exePath)
	if err != nil || string(got) != smokePassingBinary {
		t.Fatalf("exe not swapped: %q %v", got, err)
	}
	if info, err := os.Stat(exePath); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("swapped binary not executable: %v %v", info, err)
	}
	// ...the old binary preserved as the .bak escape hatch...
	bak, err := os.ReadFile(exePath + ".bak")
	if err != nil || string(bak) != "OLD-BINARY" {
		t.Fatalf(".bak must hold the OLD binary: %q %v", bak, err)
	}
	// ...and the restart fired (through the seam) at the swapped path.
	select {
	case p := <-restarted:
		if p != exePath {
			t.Fatalf("restart pointed at %q, want %q", p, exePath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart never fired after a successful swap")
	}
	// No staging litter left beside the binary.
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exePath), ".ocserverd-upgrade-*"))
	if len(leftovers) != 0 {
		t.Fatalf("staging temp files left behind: %v", leftovers)
	}
}

func TestUpgradeChecksumMismatchLeavesOldBinary(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	payload := []byte(smokePassingBinary)
	// The updater DECLARES a digest that is not the payload's: corrupt/tampered.
	up := fakeUpdaterFull(t, "inv-1", "v260713-0003", "new-sha", payload,
		sha256hex([]byte("something else entirely")))
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("checksum mismatch must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeUnreachableUpdaterLeavesOldBinary(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	up := fakeUpdaterFull(t, "inv-1", "v260713-0004", "new-sha", []byte(smokePassingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
	// The updater dies AFTER the check cached "newer available" (the button is
	// showing) but BEFORE the click: the trigger-time pin must fail honestly.
	up.Close()

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("dead updater at trigger time must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeSmokeTestFailureLeavesOldBinary(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// Digest-valid artifact that cannot actually run: dies at the smoke gate.
	up := fakeUpdaterFull(t, "inv-1", "v260713-0005", "new-sha", []byte(smokeFailingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("smoke-test failure must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeRefusesWhenPinnedLatestIsRunningBuild(t *testing.T) {
	// The 5-min cache says "newer" (button showing) but the authoritative
	// trigger-time pin reveals the latest IS the running build (e.g. the
	// updater rolled back): honest 409, nothing touched.
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	up := fakeUpdaterFull(t, "inv-1", "v260713-0006", "new-sha", []byte(smokePassingBinary), "")
	configureUpdater(t, api, srvURL, token, up.URL, "inv-1")
	api.updateMu.Lock()
	api.updateCheck.latestGitSHA = "new-sha" // cache keeps claiming newer
	api.updateMu.Unlock()
	api.processSHA = "new-sha" // ...but the pin will match the running build

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 409 {
		t.Fatalf("pin-time same-build must 409: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

// assertUpgradeUntouched is every failure path's postcondition: old bytes
// still at the exe path, no .bak, no staging litter, no restart.
func assertUpgradeUntouched(t *testing.T, exePath string, restarted chan string) {
	t.Helper()
	got, err := os.ReadFile(exePath)
	if err != nil || string(got) != "OLD-BINARY" {
		t.Fatalf("old binary must be untouched: %q %v", got, err)
	}
	if _, err := os.Stat(exePath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no .bak may exist after a failed upgrade: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exePath), ".ocserverd-upgrade-*"))
	if len(leftovers) != 0 {
		t.Fatalf("staging temp files left behind: %v", leftovers)
	}
	select {
	case p := <-restarted:
		t.Fatalf("restart must NOT fire on failure (got %q)", p)
	default:
	}
}
