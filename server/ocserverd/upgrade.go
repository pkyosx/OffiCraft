package main

// upgrade.go — the upgrade EXECUTION body behind POST /api/update/upgrade.
// Source of truth: GitHub Releases on pkyosx/OffiCraft (the retired
// ocupdaterd chain's replacement, t-dc68) — the same seven-step verified
// swap, with the download side re-pointed at release assets.
//
// Shape of one upgrade (everything fallible runs SYNCHRONOUSLY in the
// request, so the owner's click gets a real answer, not a fire-and-forget):
//
//	1. re-fetch GitHub's release list (bounded) and pin the newest release
//	   the followed channel admits AT TRIGGER TIME — never trusted from the
//	   5-min cache. The expected digest comes from the release's own
//	   checksums.txt asset (bin/release publishes it beside the tarball);
//	2. download the officraft-<tag>-darwin-arm64.tar.gz asset (bounded)
//	   into a temp file IN THE RUNNING BINARY'S OWN DIRECTORY (same
//	   filesystem → the later rename is atomic), hashing while streaming;
//	3. verify: the computed sha256 must equal the checksums.txt entry for
//	   that exact asset name — any mismatch (or a checksums.txt without the
//	   entry) aborts with an honest error and nothing on disk has changed;
//	4. extract the `ocserverd` member from the verified tarball and
//	   smoke-test it (`<tmp> --help` must exit 0) — a wrong-architecture or
//	   truncated-yet-published artifact is caught HERE, while the old
//	   binary is still untouched;
//	5. back up the running binary to <exe>.bak (exactly ONE backup is kept:
//	   each successful upgrade overwrites the previous .bak — that file IS
//	   the manual rollback: stop the server, `mv ocserverd.bak ocserverd`,
//	   start. No automatic GC: one stale ~25MB file beside the binary is a
//	   cheap permanent escape hatch, so success does NOT delete it);
//	6. atomically rename the verified binary over the exe path (a failure
//	   here restores the .bak before answering, so the disk never ends up
//	   binary-less);
//	7. answer 200 {status:"restarting", target_version} — only now, with
//	   the swap already landed — then, off the request path after a short
//	   flush delay, re-exec the swapped path (syscall.Exec keeps the PID).
//
// NOTE the deliberately narrow scope: this in-place path swaps ONLY the
// ocserverd binary (the single-file deploy artifact — SPA/seeds/warden/agent
// all ride inside it as embeds). The release tarball's install.sh is the
// full fresh-install path; it is not run here.
//
// WHY re-exec (and not exit-and-let-a-supervisor-restart): the two real
// deployment forms are (a) the launchd LaunchAgent com.officraft.serve —
// bin/serve `exec`s ocserverd, so this process IS the tracked job and exec
// keeps its PID (launchd does not care; KeepAlive additionally covers an
// exec that dies at boot), and (b) a manual `./ocserverd serve` foreground
// run with NO supervisor at all — exiting there would turn "upgrade" into
// "outage". Re-exec is the one mechanism that restarts correctly under both.
// Go marks fds close-on-exec, so the old listener closes at exec and the new
// image re-binds; SQLite recovers a mid-flight process image swap exactly
// like a crash (WAL/busy_timeout).
//
// If the exec itself fails (should be impossible after the smoke test), the
// OLD process keeps serving unharmed — the new binary is already verified
// on disk, so the next restart (manual / launchd) comes up on it; the
// failure is logged loudly instead of silently retried.

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// upgradeDownloadTimeout bounds each asset download (the tarball runs
	// ~25MB; two minutes is generous even over a slow connection).
	upgradeDownloadTimeout = 2 * time.Minute
	// upgradeMaxBytes caps every download/extraction so a misbehaving release
	// cannot fill the disk through this path.
	upgradeMaxBytes = 256 << 20
	// upgradeSmokeTimeout bounds the candidate's `--help` smoke run.
	upgradeSmokeTimeout = 15 * time.Second
	// upgradeRestartDelay is how long the post-response goroutine waits before
	// re-exec'ing — long enough for the 200 to flush to the owner's browser.
	upgradeRestartDelay = 750 * time.Millisecond
	// checksumsAssetName is the digest manifest bin/release publishes beside
	// the tarball (shasum -a 256 format).
	checksumsAssetName = "checksums.txt"
	// serverBinaryName is the tar member this path installs.
	serverBinaryName = "ocserverd"
)

// releaseAssetName is the platform tarball bin/release packages for a tag.
func releaseAssetName(tag string) string {
	return "officraft-" + tag + "-darwin-arm64.tar.gz"
}

// upgradeFailure carries an HTTP status alongside the message so
// executeUpgrade's callers can answer the honest envelope directly.
type upgradeFailure struct {
	status  int
	message string
}

func (e *upgradeFailure) Error() string { return e.message }

func upgradeFail(status int, format string, args ...any) *upgradeFailure {
	return &upgradeFailure{status: status, message: fmt.Sprintf(format, args...)}
}

// pinUpgradeRelease pins the release to install AT TRIGGER TIME on the
// followed channel: the cached check (update_check.go) is only the
// precondition gate — the release the swap verifies against must come from a
// fresh authoritative read.
func (s *apiServer) pinUpgradeRelease() (githubRelease, *upgradeFailure) {
	rel, none, err := fetchLatestOffiCraftRelease(s.releaseAPIBaseURL(), s.receiveBetaEnabled())
	if err != nil {
		return rel, upgradeFail(http.StatusBadGateway,
			"cannot reach GitHub to pin the release — nothing was changed: %v", err)
	}
	if none {
		return rel, upgradeFail(http.StatusConflict,
			"no release is published on GitHub — nothing to install")
	}
	return rel, nil
}

// findReleaseAsset resolves one named asset on the pinned release.
func findReleaseAsset(rel githubRelease, name string) (githubReleaseAsset, *upgradeFailure) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	return githubReleaseAsset{}, upgradeFail(http.StatusBadGateway,
		"release %s carries no %q asset — refusing an unverifiable install; nothing was changed",
		rel.TagName, name)
}

// httpGetAsset performs one bounded anonymous GET (redirect-following — the
// browser_download_url redirects to GitHub's CDN).
func httpGetAsset(url string) (*http.Response, *upgradeFailure) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, upgradeFail(http.StatusBadGateway, "cannot build the download request: %v", err)
	}
	client := &http.Client{Timeout: upgradeDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, upgradeFail(http.StatusBadGateway,
			"downloading %s failed — nothing was changed: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, upgradeFail(http.StatusBadGateway,
			"the asset download answered %d for %s — nothing was changed", resp.StatusCode, url)
	}
	return resp, nil
}

// fetchExpectedSHA downloads the release's checksums.txt and extracts the
// sha256 recorded for assetName (shasum format: "<hex64>  <name>"; a leading
// "*" on the name — binary mode — is tolerated).
func fetchExpectedSHA(rel githubRelease, assetName string) (string, *upgradeFailure) {
	sums, fail := findReleaseAsset(rel, checksumsAssetName)
	if fail != nil {
		return "", fail
	}
	resp, fail := httpGetAsset(sums.BrowserDownloadURL)
	if fail != nil {
		return "", fail
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		digest := strings.ToLower(fields[0])
		if name == assetName && len(digest) == 64 && isLowerHex64(digest) {
			return digest, nil
		}
	}
	return "", upgradeFail(http.StatusBadGateway,
		"release %s's checksums.txt carries no sha256 for %s — refusing an unverifiable download; nothing was changed",
		rel.TagName, assetName)
}

func isLowerHex64(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// upgradeTargetPath resolves the file to replace: the test seam when set,
// else the running executable (symlinks resolved, so the REAL file is
// swapped, not a link hop).
func (s *apiServer) upgradeTargetPath() (string, error) {
	if s.upgradeExeOverride != "" {
		return s.upgradeExeOverride, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// downloadUpgradeTarball streams the release tarball into a temp file in dir,
// hashing while copying, and verifies the digest against the checksums.txt
// entry. Returns the temp path; every failure removes the temp file itself.
func downloadUpgradeTarball(asset githubReleaseAsset, expectedSHA, dir string) (string, *upgradeFailure) {
	resp, fail := httpGetAsset(asset.BrowserDownloadURL)
	if fail != nil {
		return "", fail
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(dir, ".officraft-upgrade-*.tar.gz")
	if err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot create a staging file beside the running binary (%s) — is the directory writable? nothing was changed: %v", dir, err)
	}
	tmpPath := tmp.Name()
	hasher := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, upgradeMaxBytes))
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		os.Remove(tmpPath)
		return "", upgradeFail(http.StatusBadGateway,
			"the download of %s broke mid-stream — nothing was changed", asset.Name)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA {
		os.Remove(tmpPath)
		return "", upgradeFail(http.StatusBadGateway,
			"sha256 mismatch on %s: checksums.txt promises %s, got %s — the download is corrupt or tampered; nothing was changed",
			asset.Name, expectedSHA, got)
	}
	return tmpPath, nil
}

// extractServerBinary pulls the `ocserverd` member out of the verified
// tarball into a temp file in dir (0755). The member may sit at any depth
// (bin/release packages a flat tarball today; a wrapping directory tomorrow
// must not break upgrades) — the basename is what identifies it.
func extractServerBinary(tarPath, dir string) (string, *upgradeFailure) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot reopen the downloaded tarball — nothing was changed: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", upgradeFail(http.StatusBadGateway,
			"the downloaded asset is not a gzip tarball — nothing was changed: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", upgradeFail(http.StatusBadGateway,
				"the release tarball carries no %q member — nothing was changed", serverBinaryName)
		}
		if err != nil {
			return "", upgradeFail(http.StatusBadGateway,
				"the release tarball is unreadable — nothing was changed: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != serverBinaryName {
			continue
		}
		tmp, err := os.CreateTemp(dir, ".ocserverd-upgrade-*")
		if err != nil {
			return "", upgradeFail(http.StatusInternalServerError,
				"cannot create a staging file beside the running binary (%s) — nothing was changed: %v", dir, err)
		}
		tmpPath := tmp.Name()
		_, err = io.Copy(tmp, io.LimitReader(tr, upgradeMaxBytes))
		closeErr := tmp.Close()
		if err != nil || closeErr != nil {
			os.Remove(tmpPath)
			return "", upgradeFail(http.StatusBadGateway,
				"extracting %s from the tarball failed — nothing was changed", serverBinaryName)
		}
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			os.Remove(tmpPath)
			return "", upgradeFail(http.StatusInternalServerError,
				"cannot mark the extracted binary executable — nothing was changed: %v", err)
		}
		return tmpPath, nil
	}
}

// smokeTestBinary runs `<candidate> --help` with a bounded timeout and
// requires exit 0 — the cheapest possible "this artifact can at least start
// on THIS machine" gate (wrong architecture, mach-o/ELF mixups, truncation
// that survived a matching digest upload... all die here, pre-swap).
func smokeTestBinary(path string) *upgradeFailure {
	cmd := exec.Command(path, "--help")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return upgradeFail(http.StatusBadGateway,
			"the downloaded binary cannot start on this machine (wrong platform build?) — nothing was changed: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return upgradeFail(http.StatusBadGateway,
				"the downloaded binary failed its --help smoke test — refusing to install it; nothing was changed: %v", err)
		}
		return nil
	case <-time.After(upgradeSmokeTimeout):
		_ = cmd.Process.Kill()
		<-done
		return upgradeFail(http.StatusBadGateway,
			"the downloaded binary hung on its --help smoke test — refusing to install it; nothing was changed")
	}
}

// executeUpgrade runs steps 1–6 (pin → checksums → download → verify →
// extract → smoke → backup → swap). On success the NEW binary sits at exePath
// and the OLD one at exePath+".bak"; on any failure the old binary is
// untouched and still the one at exePath. Returns the pinned tag installed.
func (s *apiServer) executeUpgrade() (string, *upgradeFailure) {
	rel, fail := s.pinUpgradeRelease()
	if fail != nil {
		return "", fail
	}
	// The honesty gate re-runs against the PINNED release: the cache that
	// enabled the button may be stale (e.g. the release was deleted since).
	if rel.TagName == appVersion {
		return "", upgradeFail(http.StatusConflict,
			"GitHub's current latest (%s) IS the running build — nothing newer to install", rel.TagName)
	}

	asset, fail := findReleaseAsset(rel, releaseAssetName(rel.TagName))
	if fail != nil {
		return "", fail
	}
	expectedSHA, fail := fetchExpectedSHA(rel, asset.Name)
	if fail != nil {
		return "", fail
	}

	exePath, err := s.upgradeTargetPath()
	if err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot resolve the running binary's path — nothing was changed: %v", err)
	}
	dir := filepath.Dir(exePath)

	tarPath, fail := downloadUpgradeTarball(asset, expectedSHA, dir)
	if fail != nil {
		return "", fail
	}
	defer os.Remove(tarPath)

	tmpPath, fail := extractServerBinary(tarPath, dir)
	if fail != nil {
		return "", fail
	}
	defer os.Remove(tmpPath) // no-op after the success rename

	if fail := smokeTestBinary(tmpPath); fail != nil {
		return "", fail
	}

	// Backup (the manual rollback path — see the file header's step 5 for the
	// retention policy: exactly one .bak, overwritten per successful upgrade,
	// never auto-deleted).
	bakPath := exePath + ".bak"
	if err := os.Rename(exePath, bakPath); err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot back up the running binary to %s — nothing was changed: %v", bakPath, err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		// The disk must never end up binary-less: restore the backup before
		// answering. (Same directory, same filesystem — this restore is the
		// exact inverse of the rename that just succeeded.)
		if restoreErr := os.Rename(bakPath, exePath); restoreErr != nil {
			log.Printf("[upgrade] CRITICAL: swap failed (%v) AND restoring the backup failed (%v) — %s is missing; restore it manually from %s",
				err, restoreErr, exePath, bakPath)
			return "", upgradeFail(http.StatusInternalServerError,
				"the binary swap failed AND the automatic backup restore failed — manual attention needed (backup at %s): %v", bakPath, err)
		}
		return "", upgradeFail(http.StatusInternalServerError,
			"the binary swap failed — the old binary was restored and keeps serving: %v", err)
	}
	return rel.TagName, nil
}

// restartIntoUpgradedBinary re-execs the swapped binary after a short flush
// delay (step 7). syscall.Exec replaces THIS process image in place — same
// PID (launchd keeps tracking it), same args, same env, same cwd — and Go's
// close-on-exec fds free the listener for the new image to re-bind.
func restartIntoUpgradedBinary(exePath string) {
	time.Sleep(upgradeRestartDelay)
	log.Printf("[upgrade] restarting: exec %s (argv %v)", exePath, os.Args)
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		// Post-smoke-test this should be unreachable; the OLD process keeps
		// serving unharmed and the verified new binary waits on disk for the
		// next (manual / launchd) restart. Loud, no silent retry loop.
		log.Printf("[upgrade] CRITICAL: exec of the upgraded binary failed (%v) — the OLD build keeps serving; the new binary is installed at %s and takes over on the next restart", err, exePath)
	}
}

// runUpgrade is the SHARED trigger body behind both the owner's explicit
// POST /api/update/upgrade and the armed auto-update cadence
// (auto_update.go): the precondition gate (a newer release known) as an
// honest 409-shaped failure, ONE upgrade at a time (TryLock — a concurrent
// trigger answers 409, never a second swap), then the full verified
// execution body. On success the swap has LANDED on disk and the caller owns
// scheduling the re-exec (via scheduleUpgradeRestart).
func (s *apiServer) runUpgrade() (version, exePath string, fail *upgradeFailure) {
	available, _ := s.updateStatus()
	if !available {
		return "", "", upgradeFail(http.StatusConflict,
			"no newer release is known — the running build is the latest published on GitHub (use 檢查更新 to re-check)")
	}
	if !s.upgradeMu.TryLock() {
		return "", "", upgradeFail(http.StatusConflict, "an upgrade is already in progress")
	}
	defer s.upgradeMu.Unlock()

	version, fail = s.executeUpgrade()
	if fail != nil {
		return "", "", fail
	}
	exePath, err := s.upgradeTargetPath()
	if err != nil { // unreachable: executeUpgrade resolved the same path
		return "", "", upgradeFail(http.StatusInternalServerError, "%s", err.Error())
	}
	log.Printf("[upgrade] release %s verified and swapped into %s (backup: %s.bak); restarting in %v",
		version, exePath, exePath, upgradeRestartDelay)
	return version, exePath, nil
}

// scheduleUpgradeRestart fires the post-swap re-exec off the caller's path
// (the test seam upgradeRestart captures it instead of exec'ing the test
// process away).
func (s *apiServer) scheduleUpgradeRestart(exePath string) {
	restart := s.upgradeRestart
	if restart == nil {
		restart = restartIntoUpgradedBinary
	}
	go restart(exePath)
}

// POST /api/update/upgrade — owner-gated EXPLICIT upgrade trigger (the
// software-update card's 升級 button; MCPExclude — agents can never call
// it). Preconditions answer honest 409s; a valid trigger runs the full
// execution body synchronously (see the file header) and only answers
// 200 {status:"restarting"} once the verified swap has landed. The armed
// auto-update cadence (auto_update.go) shares the same runUpgrade body.
func (s *apiServer) HandleUpgradeApiUpdateUpgradePost(w http.ResponseWriter, r *http.Request) {
	version, exePath, fail := s.runUpgrade()
	if fail != nil {
		writeError(w, fail.status, fail.message)
		return
	}
	writeJSON(w, http.StatusOK, UpgradeResultDTO{
		Status:        "restarting",
		TargetVersion: version,
	})
	s.scheduleUpgradeRestart(exePath)
}
