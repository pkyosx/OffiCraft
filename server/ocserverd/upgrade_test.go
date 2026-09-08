// Skeleton generated from server/ocserverd/upgrade.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

func upgradeTestReleaseServer(t *testing.T, tag, binary string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	tarPath := upgradeTestTarball(t, dir, map[string]string{
		"release/" + serverBinaryName: binary,
	})
	tarball, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	digest := sha256.Sum256(tarball)
	sha := hex.EncodeToString(digest[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + releaseRepo + "/releases":
			_ = json.NewEncoder(w).Encode([]githubRelease{{
				TagName: tag,
				HTMLURL: "https://example.invalid/releases/" + tag,
				Assets: []githubReleaseAsset{
					{Name: checksumsAssetName, BrowserDownloadURL: srv.URL + "/checksums.txt"},
					{Name: releaseAssetName(tag), BrowserDownloadURL: srv.URL + "/release.tar.gz"},
				},
			}})
		case "/checksums.txt":
			_, _ = io.WriteString(w, sha+"  "+releaseAssetName(tag)+"\n")
		case "/release.tar.gz":
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPinUpgradeRelease(t *testing.T) {
	t.Run("pins the semver greatest release from a fresh authoritative read", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.RequestURI()
			_ = json.NewEncoder(w).Encode([]githubRelease{
				{TagName: "v0.9.9"},
				{TagName: "v1.2.3"},
			})
		}))
		defer srv.Close()

		api := &apiServer{releaseAPIBase: srv.URL}
		rel, fail := api.pinUpgradeRelease()

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if rel.TagName != "v1.2.3" {
			t.Fatalf("pinned tag: %q", rel.TagName)
		}
		if gotPath != "/repos/pkyosx/OffiCraft/releases?per_page=20" {
			t.Fatalf("request path: %q", gotPath)
		}
	})

	t.Run("a GitHub 404 is reported as no published release", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		api := &apiServer{releaseAPIBase: srv.URL}
		rel, fail := api.pinUpgradeRelease()

		if rel.TagName != "" || rel.HTMLURL != "" || rel.Draft || rel.Prerelease || len(rel.Assets) != 0 {
			t.Fatalf("release: %#v", rel)
		}
		if fail == nil || fail.status != http.StatusConflict || fail.Error() != "no release is published on GitHub — nothing to install" {
			t.Fatalf("failure: %#v", fail)
		}
	})
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
	t.Run("follows a redirect and returns the successful response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/asset", http.StatusFound)
				return
			}
			_, _ = io.WriteString(w, "release bytes")
		}))
		defer srv.Close()

		resp, fail := httpGetAsset(srv.URL + "/redirect")
		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "release bytes" {
			t.Fatalf("body: %q", body)
		}
	})

	t.Run("a non-200 answer is refused and the body is closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		resp, fail := httpGetAsset(srv.URL + "/missing")
		if resp != nil {
			t.Fatalf("response: %#v", resp)
		}
		if fail == nil || fail.status != http.StatusBadGateway || fail.Error() != "the asset download answered 404 for "+srv.URL+"/missing — nothing was changed" {
			t.Fatalf("failure: %#v", fail)
		}
	})
}

func TestFetchExpectedSHA(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	t.Run("extracts the matching digest and tolerates binary mode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "not a checksum line\n"+digest+"  *target.tar.gz\n")
		}))
		defer srv.Close()

		rel := githubRelease{
			TagName: "v1.2.3",
			Assets:  []githubReleaseAsset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}},
		}
		got, fail := fetchExpectedSHA(rel, "target.tar.gz")

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if got != digest {
			t.Fatalf("digest: %q", got)
		}
	})

	t.Run("a checksum entry for another asset is refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, digest+"  other.tar.gz\n")
		}))
		defer srv.Close()

		rel := githubRelease{
			TagName: "v1.2.3",
			Assets:  []githubReleaseAsset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}},
		}
		got, fail := fetchExpectedSHA(rel, "target.tar.gz")

		if got != "" {
			t.Fatalf("digest: %q", got)
		}
		if fail == nil || fail.status != http.StatusBadGateway || fail.Error() != "release v1.2.3's checksums.txt carries no sha256 for target.tar.gz — refusing an unverifiable download; nothing was changed" {
			t.Fatalf("failure: %#v", fail)
		}
	})
}

func TestIsLowerHex64(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{name: "lowercase digits and letters", text: "0123456789abcdef", want: true},
		{name: "uppercase is not lower hex", text: "0123456789ABCDEF", want: false},
		{name: "punctuation is rejected", text: "0123456789abcdef-", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLowerHex64(tc.text); got != tc.want {
				t.Fatalf("isLowerHex64(%q): want %v, got %v", tc.text, tc.want, got)
			}
		})
	}
}

func TestUpgradeTargetPath(t *testing.T) {
	t.Run("uses the explicit test seam", func(t *testing.T) {
		api := &apiServer{upgradeExeOverride: filepath.Join(t.TempDir(), "ocserverd")}
		got, err := api.upgradeTargetPath()
		if err != nil {
			t.Fatalf("upgradeTargetPath: %v", err)
		}
		if got != api.upgradeExeOverride {
			t.Fatalf("target: %q", got)
		}
	})

	t.Run("resolves the running executable when no seam is set", func(t *testing.T) {
		api := &apiServer{}
		got, err := api.upgradeTargetPath()
		if err != nil {
			t.Fatalf("upgradeTargetPath: %v", err)
		}
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		want, err := filepath.EvalSymlinks(exe)
		if err != nil {
			want = exe
		}
		if got != want {
			t.Fatalf("target: want %q, got %q", want, got)
		}
	})
}

func TestDownloadUpgradeTarball(t *testing.T) {
	const body = "verified release bytes"
	checksum := sha256.Sum256([]byte(body))
	wantSHA := hex.EncodeToString(checksum[:])

	t.Run("streams the body into the requested directory and verifies its digest", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		dir := t.TempDir()

		path, fail := downloadUpgradeTarball(githubReleaseAsset{
			Name: "release.tar.gz", BrowserDownloadURL: srv.URL,
		}, wantSHA, dir)

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read staged body: %v", err)
		}
		if string(data) != body {
			t.Fatalf("staged body: %q", data)
		}
		if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), ".officraft-upgrade-") {
			t.Fatalf("staged path: %q", path)
		}
	})

	t.Run("a digest mismatch removes the partial staging file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		dir := t.TempDir()

		path, fail := downloadUpgradeTarball(githubReleaseAsset{
			Name: "release.tar.gz", BrowserDownloadURL: srv.URL,
		}, strings.Repeat("0", 64), dir)

		if path != "" {
			t.Fatalf("path: %q", path)
		}
		if fail == nil || fail.status != http.StatusBadGateway {
			t.Fatalf("failure: %#v", fail)
		}
		if got := upgradeTestDirEntries(t, dir); len(got) != 0 {
			t.Fatalf("staging directory: %v", got)
		}
	})
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
	t.Run("verifies, smoke-tests, backs up, and atomically swaps the running binary", func(t *testing.T) {
		srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}

		version, fail := api.executeUpgrade()

		if fail != nil {
			t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
		}
		if version != "v1.2.3" {
			t.Fatalf("version: %q", version)
		}
		newBinary, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read new binary: %v", err)
		}
		if string(newBinary) != "#!/bin/sh\nexit 0\n" {
			t.Fatalf("new binary: %q", newBinary)
		}
		backup, err := os.ReadFile(exe + ".bak")
		if err != nil {
			t.Fatalf("read backup: %v", err)
		}
		if string(backup) != "running binary" {
			t.Fatalf("backup: %q", backup)
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd", "ocserverd.bak"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})

	t.Run("a pinned release that is not newer leaves the running binary alone", func(t *testing.T) {
		srv := upgradeTestReleaseServer(t, "v0.0.0", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}

		version, fail := api.executeUpgrade()

		if version != "" {
			t.Fatalf("version: %q", version)
		}
		if fail == nil || fail.status != http.StatusConflict || fail.Error() != "GitHub's current latest (v0.0.0) is not newer than the running build (0.0.0) — nothing newer to install" {
			t.Fatalf("failure: %#v", fail)
		}
		if got := upgradeTestDirEntries(t, dir); !reflect.DeepEqual(got, []string{"ocserverd"}) {
			t.Fatalf("binary directory: %v", got)
		}
	})
}

func TestRestartIntoUpgradedBinary(t *testing.T) {
	if os.Getenv("UPGRADE_RESTART_HELPER") == "1" {
		restartIntoUpgradedBinary(os.Getenv("UPGRADE_RESTART_TARGET"))
		return
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "restart.txt")
	target := filepath.Join(dir, "replacement.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$UPGRADE_RESTART_MARKER\"\n"), 0o755); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestartIntoUpgradedBinary$", "-test.v")
	cmd.Env = append(os.Environ(),
		"UPGRADE_RESTART_HELPER=1",
		"UPGRADE_RESTART_TARGET="+target,
		"UPGRADE_RESTART_MARKER="+marker,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart helper: %v\n%s", err, output)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read restart marker: %v", err)
	}
	if !strings.Contains(string(data), "-test.run=^TestRestartIntoUpgradedBinary$") {
		t.Fatalf("re-exec arguments: %q", data)
	}
}

func TestRunUpgrade(t *testing.T) {
	srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
	dir := t.TempDir()
	exe := filepath.Join(dir, "ocserverd")
	if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}
	api := &apiServer{releaseAPIBase: srv.URL, upgradeExeOverride: exe}
	upgradeTestKnownRelease(t, api, "v1.2.3")

	version, path, fail := api.runUpgrade()

	if fail != nil {
		t.Fatalf("want no failure, got %d %q", fail.status, fail.message)
	}
	if version != "v1.2.3" || path != exe {
		t.Fatalf("result: version=%q path=%q", version, path)
	}
}

func TestScheduleUpgradeRestart(t *testing.T) {
	called := make(chan string, 1)
	api := &apiServer{}
	api.upgradeRestart = func(path string) {
		if !api.stationShuttingDown.Load() {
			t.Errorf("restart seam ran before shutdown marker was set")
		}
		called <- path
	}

	api.scheduleUpgradeRestart("/tmp/ocserverd")
	select {
	case got := <-called:
		if got != "/tmp/ocserverd" {
			t.Fatalf("restart path: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("restart seam was not called")
	}
	if api.stationShuttingDown.Load() {
		t.Fatal("shutdown marker was not cleared after restart seam returned")
	}
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

	t.Run("a well-formed POST /api/update/upgrade answers 200 after the swap lands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		srv := upgradeTestReleaseServer(t, "v1.2.3", "#!/bin/sh\nexit 0\n")
		dir := t.TempDir()
		exe := filepath.Join(dir, "ocserverd")
		if err := os.WriteFile(exe, []byte("running binary"), 0o755); err != nil {
			t.Fatalf("write old binary: %v", err)
		}
		api.releaseAPIBase = srv.URL
		api.upgradeExeOverride = exe
		upgradeTestKnownRelease(t, api, "v1.2.3")
		restarted := make(chan string, 1)
		api.upgradeRestart = func(path string) { restarted <- path }
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")

		if status != http.StatusOK {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"status":         "restarting",
			"target_version": "v1.2.3",
		})
		select {
		case got := <-restarted:
			if got != exe {
				t.Fatalf("restart path: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("restart seam was not called")
		}
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", "", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a plain authenticated agent answers 403", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", agent, "")
		if status != http.StatusForbidden {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("the POST path reaches the upgrade handler and returns its domain conflict", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/update/upgrade", owner, "")
		if status != http.StatusConflict {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
	})

	t.Run("malformed body and content type are not wire-layer rejections for this bodyless route", func(t *testing.T) {
		t.Skip("structurally unproducible: this POST declares no request body, and the stack has no content-type or size middleware; measured malformed and oversized bodies reach the domain instead of producing a wire-layer 4xx")
	})
}
