package main

// upgrade_test.go — the upgrade execution body (upgrade.go): the success path
// (pin → checksums verify → tarball download → extract → smoke → backup →
// swap → restart) and the failure paths that MUST leave the old binary
// untouched and serving.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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

// makeTarGz builds a gzip tarball whose members are name → content (the
// success shape carries an "ocserverd" member).
func makeTarGz(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range members {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// githubWithRelease stands up a fake GitHub carrying ONE release <tag> whose
// tarball asset holds binaryContent as the ocserverd member. declaredSHA lets
// a test lie in checksums.txt (corruption); "" declares the tarball's true
// sha. withChecksums=false omits the checksums.txt asset entirely.
func githubWithRelease(t *testing.T, tag, binaryContent, declaredSHA string, withChecksums bool) *fakeGitHub {
	t.Helper()
	tarball := makeTarGz(t, map[string]string{"ocserverd": binaryContent})
	if declaredSHA == "" {
		declaredSHA = sha256hex(tarball)
	}
	assetName := releaseAssetName(tag)
	assets := map[string][]byte{assetName: tarball}
	if withChecksums {
		assets[checksumsAssetName] = []byte(declaredSHA + "  " + assetName + "\n")
	}
	return newFakeGitHub(t, fakeRelease{tag: tag, assets: assets})
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

// pointAtGitHub wires the fake GitHub in and lets the background check settle
// so the precondition gate (updateStatus) sees its latest.
func pointAtGitHub(t *testing.T, api *apiServer, gh *fakeGitHub) {
	t.Helper()
	api.releaseAPIBase = gh.srv.URL
	api.kickUpdateCheck()
	waitUpdateSettled(t, api)
}

// assertUpgradeUntouched pins the failure-path invariant: old bytes at the
// exe path, no .bak, no staging litter, no restart.
func assertUpgradeUntouched(t *testing.T, exePath string, restarted chan string) {
	t.Helper()
	got, err := os.ReadFile(exePath)
	if err != nil || string(got) != "OLD-BINARY" {
		t.Fatalf("old binary must be untouched: %q %v", got, err)
	}
	if _, err := os.Stat(exePath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no .bak may exist after a refused upgrade: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exePath), ".oc*-upgrade-*"))
	more, _ := filepath.Glob(filepath.Join(filepath.Dir(exePath), ".officraft-upgrade-*"))
	if len(leftovers)+len(more) != 0 {
		t.Fatalf("staging litter left behind: %v %v", leftovers, more)
	}
	select {
	case p := <-restarted:
		t.Fatalf("restart must not fire: %q", p)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUpgradeSuccessSwapsBackupsAndRestarts(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.9.0", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 200 {
		t.Fatalf("valid upgrade must 200: %d %v", status, data)
	}
	if data["status"] != "restarting" || data["target_version"] != "v0.9.0" {
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
	// ...and the restart scheduled through the seam.
	select {
	case p := <-restarted:
		if p != exePath {
			t.Fatalf("restart pointed at %q, want %q", p, exePath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart never fired")
	}
}

func TestUpgradeNoNewerReleaseAnswers409(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// No GitHub check has ever succeeded (default unroutable base) — the
	// precondition gate refuses before anything reaches out.
	status, _ := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 409 {
		t.Fatalf("no known newer release must 409: %d", status)
	}
	_ = api
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeCorruptDownloadRefusesSwap(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// checksums.txt promises a digest the tarball does not hash to.
	gh := githubWithRelease(t, "v0.9.1", smokePassingBinary,
		"1111111111111111111111111111111111111111111111111111111111111111", true)
	pointAtGitHub(t, api, gh)

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("digest mismatch must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeMissingChecksumsRefusesUnverifiable(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.9.2", smokePassingBinary, "", false)
	pointAtGitHub(t, api, gh)

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("a release without checksums.txt must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeSmokeFailureRefusesSwap(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	// Digest-valid artifact that dies at the smoke gate (the health check):
	// verified bytes are NOT enough — a binary that cannot run never swaps in.
	gh := githubWithRelease(t, "v0.9.3", smokeFailingBinary, "", true)
	pointAtGitHub(t, api, gh)

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("smoke failure must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradeTarballWithoutServerBinaryRefused(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	tag := "v0.9.4"
	tarball := makeTarGz(t, map[string]string{"README.md": "not a binary"})
	assetName := releaseAssetName(tag)
	gh := newFakeGitHub(t, fakeRelease{tag: tag, assets: map[string][]byte{
		assetName:          tarball,
		checksumsAssetName: []byte(sha256hex(tarball) + "  " + assetName + "\n"),
	}})
	pointAtGitHub(t, api, gh)

	status, data := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("a tarball without ocserverd must 502: %d %v", status, data)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

func TestUpgradePinnedLatestEqualsRunningAnswers409(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	gh := githubWithRelease(t, "v0.9.5", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)
	// The cache says "newer", but the trigger-time re-pin sees the running
	// build's own tag (e.g. the stale cache raced a version bump).
	setAppVersion(t, "v0.9.5")
	// Rebuild the cache under the old belief: force available by faking the
	// cached tag as different from appVersion is impossible now — instead pin
	// the honesty gate directly: make the cache think an update exists.
	api.updateMu.Lock()
	api.updateCheck.rel.TagName = "v0.9.6-phantom"
	api.updateMu.Unlock()

	status, _ := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != 409 {
		t.Fatalf("pinned-latest == running must 409: %d", status)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}
