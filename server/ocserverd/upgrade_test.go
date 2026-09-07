// Skeleton generated from server/ocserverd/upgrade.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// upgradeTestTarball writes a gzip tarball at <dir>/release.tar.gz carrying one
// regular member per entry, and answers its path.
func upgradeTestTarball(t *testing.T, dir string, members map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range members {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	path := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	return path
}

// upgradeTestDirEntries answers the names dir holds, so a failure that promises
// "nothing was changed" can be held to it.
func upgradeTestDirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// upgradeTestKnownRelease puts the server in the state a successful check
// leaves it in: GitHub answered, and the newest release it named is tag.
func upgradeTestKnownRelease(t *testing.T, api *apiServer, tag string) {
	t.Helper()
	api.updateMu.Lock()
	defer api.updateMu.Unlock()
	api.updateCheck = updateCheckState{
		checkedAt: time.Now(),
		lastOKAt:  time.Now(),
		ok:        true,
		rel:       githubRelease{TagName: tag},
	}
}

func TestPinUpgradeRelease(t *testing.T) {
	t.Skip("TODO: pinUpgradeRelease pins the release to install AT TRIGGER TIME on the followed channel: the cached check (update_check.go) is only the precondition gate — the release the swap verifies against must come from a fresh authoritative read.")
}

func TestFindReleaseAsset(t *testing.T) {
	rel := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubReleaseAsset{
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums.txt", Size: 130},
		},
	}

	t.Run("an asset the release carries resolves to its download entry", func(t *testing.T) {
		asset, fail := findReleaseAsset(rel, "checksums.txt")

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		want := githubReleaseAsset{
			Name:               "checksums.txt",
			BrowserDownloadURL: "https://example.invalid/checksums.txt",
			Size:               130,
		}
		if asset != want {
			t.Fatalf("asset: %#v", asset)
		}
	})

	t.Run("an asset the release does not carry refuses with 502", func(t *testing.T) {
		asset, fail := findReleaseAsset(rel, "officraft-v1.2.3-darwin-arm64.tar.gz")

		if asset != (githubReleaseAsset{}) {
			t.Fatalf("asset: %#v", asset)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != `release v1.2.3 carries no "officraft-v1.2.3-darwin-arm64.tar.gz" asset — refusing an unverifiable install; nothing was changed` {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestHttpGetAsset(t *testing.T) {
	t.Skip("TODO: httpGetAsset performs one bounded anonymous GET (redirect-following — the browser_download_url redirects to GitHub's CDN).")
}

func TestFetchExpectedSHA(t *testing.T) {
	t.Skip("TODO: fetchExpectedSHA downloads the release's checksums.txt and extracts the sha256 recorded for assetName (shasum format: \"<hex64> <name>\"; a leading \"*\" on the name — binary mode — is tolerated).")
}

func TestIsLowerHex64(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestUpgradeTargetPath(t *testing.T) {
	t.Skip("TODO: upgradeTargetPath resolves the file to replace: the test seam when set, else the running executable (symlinks resolved, so the REAL file is swapped, not a link hop).")
}

func TestDownloadUpgradeTarball(t *testing.T) {
	t.Skip("TODO: downloadUpgradeTarball streams the release tarball into a temp file in dir, hashing while copying, and verifies the digest against the checksums.txt entry.")
}

func TestExtractServerBinary(t *testing.T) {
	t.Run("the ocserverd member is extracted as an executable at any depth", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := upgradeTestTarball(t, dir, map[string]string{
			"officraft-v1.2.3/ocserverd": "candidate binary",
		})

		path, fail := extractServerBinary(tarPath, dir)

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if filepath.Dir(path) != dir {
			t.Fatalf("path: %q", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data) != "candidate binary" {
			t.Fatalf("extracted: %q", data)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode: %v", info.Mode().Perm())
		}
	})

	t.Run("a tarball carrying no ocserverd member refuses with 502 and stages nothing", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := upgradeTestTarball(t, dir, map[string]string{"README.md": "not a binary"})

		path, fail := extractServerBinary(tarPath, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != `the release tarball carries no "ocserverd" member — nothing was changed` {
			t.Fatalf("message: %q", fail.Error())
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"release.tar.gz"}) {
			t.Fatalf("staging directory: %v", got)
		}
	})

	t.Run("an asset that is not a gzip tarball refuses with 502 and stages nothing", func(t *testing.T) {
		dir := t.TempDir()
		tarPath := filepath.Join(dir, "release.tar.gz")
		if err := os.WriteFile(tarPath, []byte("this is not a tarball"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		path, fail := extractServerBinary(tarPath, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded asset is not a gzip tarball — nothing was changed: gzip: invalid header" {
			t.Fatalf("message: %q", fail.Error())
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"release.tar.gz"}) {
			t.Fatalf("staging directory: %v", got)
		}
	})

	t.Run("a tarball that is not on disk refuses with 500", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "release.tar.gz")

		path, fail := extractServerBinary(missing, dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 500 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "cannot reopen the downloaded tarball — nothing was changed: open "+missing+": no such file or directory" {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestSmokeTestBinary(t *testing.T) {
	t.Run("a candidate whose --help exits 0 passes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}

		if fail := smokeTestBinary(path); fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
	})

	t.Run("a candidate whose --help exits non-zero is refused with 502", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}

		fail := smokeTestBinary(path)

		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded binary failed its --help smoke test — refusing to install it; nothing was changed: exit status 3" {
			t.Fatalf("message: %q", fail.Error())
		}
	})

	t.Run("a candidate that cannot start on this machine is refused with 502", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocserverd")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		fail := smokeTestBinary(path)

		if fail == nil {
			t.Fatalf("want a failure")
		}
		if fail.status != 502 {
			t.Fatalf("status: %d", fail.status)
		}
		if fail.Error() != "the downloaded binary cannot start on this machine (wrong platform build?) — nothing was changed: fork/exec "+path+": permission denied" {
			t.Fatalf("message: %q", fail.Error())
		}
	})
}

func TestExecuteUpgrade(t *testing.T) {
	t.Skip("TODO: executeUpgrade runs steps 1–6 (pin → checksums → download → verify → extract → smoke → backup → swap).")
}

func TestRestartIntoUpgradedBinary(t *testing.T) {
	t.Skip("TODO: restartIntoUpgradedBinary re-execs the swapped binary after a short flush delay (step 7).")
}

func TestRunUpgrade(t *testing.T) {
	t.Skip("TODO: runUpgrade is the SHARED trigger body behind both the owner's explicit POST /api/update/upgrade and the armed auto-update cadence (auto_update.go): the precondition gate (a newer release known) as an honest 409-shaped failure, ONE upgrade at a time (TryLock — a concurrent trigger answers 409, never a second swap), then the full verified execution body.")
}

func TestScheduleUpgradeRestart(t *testing.T) {
	t.Skip("TODO: scheduleUpgradeRestart fires the post-swap re-exec off the caller's path (the test seam upgradeRestart captures it instead of exec'ing the test process away).")
}

func TestHandleUpgradeApiUpdateUpgradePost(t *testing.T) {
	t.Run("a trigger with no newer release known answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
		dashboard.wantFrames()
	})

	t.Run("a trigger while an upgrade is already running answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		upgradeTestKnownRelease(t, api, "v9.9.9")
		api.upgradeMu.Lock()
		defer api.upgradeMu.Unlock()
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "an upgrade is already in progress")
		dashboard.wantFrames()
	})

	t.Run("a trigger that cannot reach GitHub answers 502 and leaves the binary alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write: %v", err)
		}
		api.upgradeExeOverride = exe
		api.releaseAPIBase = "http://127.0.0.1:1"
		upgradeTestKnownRelease(t, api, "v9.9.9")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != 502 {
			t.Fatalf("want 502, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error",
			"cannot reach GitHub to pin the release — nothing was changed: "+
				`Get "http://127.0.0.1:1/repos/pkyosx/OffiCraft/releases?per_page=20": `+
				"dial tcp 127.0.0.1:1: connect: connection refused")
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd"}) {
			t.Fatalf("binary directory: %v", got)
		}
		data0, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data0) != "running binary" {
			t.Fatalf("binary: %q", data0)
		}
		dashboard.wantFrames()
	})

	t.Run("a well-formed POST /api/update/upgrade answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/update/upgrade request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/update/upgrade reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/update/upgrade request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
