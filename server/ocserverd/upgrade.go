package main

// upgrade.go — the upgrade body behind POST /api/update/upgrade and the armed
// auto-update cadence (auto_update.go). The release is pinned the way
// update_check.go finds it; its assets are fetched from GitHub's fixed
// /releases/download/<tag>/<name> URLs, and the expected sha256 comes from the
// release's checksums.txt (bin/release publishes it beside the tarball).
// Everything fallible runs synchronously in the request, and the old binary is
// untouched until the candidate is verified and smoke-tested. Staging files sit in the running binary's own
// directory so the final rename is atomic (same filesystem).
//
// Exactly ONE <exe>.bak is kept, overwritten by each successful upgrade and
// never auto-deleted: it IS the manual rollback (stop the server, `mv
// ocserverd.bak ocserverd`, start). Only the ocserverd binary is swapped
// (SPA/seeds/warden/agent ride inside it as embeds); install.sh is not run.
//
// WHY re-exec, not exit-and-let-a-supervisor-restart: under launchd bin/serve
// execs ocserverd, so exec keeps the tracked PID; a manual `./ocserverd
// serve` has no supervisor, where exiting would turn "upgrade" into "outage".
// If the exec fails, the OLD process keeps serving and the verified new
// binary takes over on the next restart.

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	upgradeDialTimeout = 10 * time.Second

	upgradeTLSHandshakeTimeout = 10 * time.Second

	upgradeHeaderTimeout = 30 * time.Second

	upgradeMaxBytes = 256 << 20

	upgradeSmokeTimeout = 15 * time.Second
	// upgradeRestartDelay: long enough for the 200 to flush to the owner's
	// browser before the re-exec.
	upgradeRestartDelay = 750 * time.Millisecond

	checksumsAssetName = "checksums.txt"

	serverBinaryName = "ocserverd"
)

// releaseAssetName must match the tarball name bin/release packages.
func releaseAssetName(tag string) string {
	return "officraft-" + tag + "-darwin-arm64.tar.gz"
}

type upgradeFailure struct {
	status  int
	message string
}

func (e *upgradeFailure) Error() string { return e.message }

func upgradeFail(status int, format string, args ...any) *upgradeFailure {
	return &upgradeFailure{status: status, message: fmt.Sprintf(format, args...)}
}

// Pinned by a fresh read at trigger time: the cached check (update_check.go)
// is only the precondition gate.
func (s *apiServer) pinUpgradeRelease() (string, *upgradeFailure) {
	tag, none, err := fetchLatestOffiCraftRelease(s.releaseSiteBaseURL(), s.receiveBetaEnabled())
	if err != nil {
		return "", upgradeFail(http.StatusBadGateway,
			"cannot reach GitHub to pin the release — nothing was changed: %v", err)
	}
	if none {
		return "", upgradeFail(http.StatusConflict,
			"no release is published on GitHub — nothing to install")
	}
	return tag, nil
}

// releaseAsset is one file of the pinned release, at its download URL.
type releaseAsset struct {
	tag  string
	name string
	url  string
}

func pinnedReleaseAsset(base, tag, name string) releaseAsset {
	return releaseAsset{tag: tag, name: name, url: releaseAssetURL(base, tag, name)}
}

// Redirects must be followed: a release download URL redirects to GitHub's CDN.
func httpGetAsset(asset releaseAsset, budget time.Duration) (*http.Response, *upgradeFailure) {
	url := asset.url
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, upgradeFail(http.StatusBadGateway, "cannot build the download request: %v", err)
	}
	resp, err := upgradeAssetClient().Do(req)
	if err != nil {
		cancel()
		return nil, upgradeFail(http.StatusBadGateway,
			"downloading %s failed — nothing was changed: %v", url, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		cancel()
		return nil, upgradeFail(http.StatusBadGateway,
			"release %s carries no %q asset — refusing an unverifiable install; nothing was changed",
			asset.tag, asset.name)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, upgradeFail(http.StatusBadGateway,
			"the asset download answered %d for %s — nothing was changed", resp.StatusCode, url)
	}
	resp.Body = newStallGuard(resp.Body, cancel, upgradeStallTimeout)
	return resp, nil
}

// upgradeStallTimeout bounds SILENCE, not elapsed time: a wall-clock ceiling
// cut slow-but-progressing downloads (2026-09-14: the 20MB asset at ~200KB/s
// needed ~108s against a 120s ceiling).
const upgradeStallTimeout = 60 * time.Second

// upgradeBodyBudget is the tarball's BACKSTOP for a source that dribbles just
// fast enough to beat the stall guard. Unlike the stall bound it tightens as
// the release grows: TestUpgradeShippedBounds keeps upgradeMaxBytes over this
// budget under the 2026-09-14 link's ~198KB/s.
const (
	upgradeMetaBudget = 2 * time.Minute
	upgradeBodyBudget = 30 * time.Minute
)

var upgradeDialer = &net.Dialer{Timeout: upgradeDialTimeout}

// Built ONCE: a client per call would leave an orphan Transport (idle
// connections and their read loops) behind on every upgrade.
var upgradeAssetSharedClient = &http.Client{
	// Deliberately 0: per-call budgets ride the request context, so the tiny
	// metadata fetch and the multi-megabyte body are bounded differently.
	Timeout: 0,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           upgradeDialer.DialContext,
		TLSHandshakeTimeout:   upgradeTLSHandshakeTimeout,
		ResponseHeaderTimeout: upgradeHeaderTimeout,
		// A Transport that supplies DialContext does NOT negotiate HTTP/2
		// unless asked (http.DefaultTransport does); kept on so the wire
		// behaviour matches.
		ForceAttemptHTTP2: true,
		IdleConnTimeout:   90 * time.Second,
	},
}

func upgradeAssetClient() *http.Client { return upgradeAssetSharedClient }

// stallGuard cancels the request once the body delivers no bytes for `every`.
// Without it, a download that answers its headers and then sends nothing
// would hold runUpgrade's TryLock forever, and the machine would silently
// never upgrade again.
type stallGuard struct {
	inner io.ReadCloser
	timer *time.Timer
	every time.Duration
	stop  context.CancelFunc
}

func newStallGuard(inner io.ReadCloser, stop context.CancelFunc, every time.Duration) io.ReadCloser {
	return &stallGuard{inner: inner, timer: time.AfterFunc(every, stop), every: every, stop: stop}
}

func (g *stallGuard) Read(p []byte) (int, error) {
	n, err := g.inner.Read(p)
	if n > 0 {
		g.timer.Reset(g.every)
	}
	return n, err
}

// Callers must Close: it is the only EARLY release of the request context,
// and go vet's lostcancel check is satisfied the moment the cancel is handed
// to another function.
func (g *stallGuard) Close() error {
	g.timer.Stop()
	g.stop()
	return g.inner.Close()
}

func fetchExpectedSHA(sums releaseAsset, assetName string) (string, *upgradeFailure) {
	resp, fail := httpGetAsset(sums, upgradeMetaBudget)
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
		sums.tag, assetName)
}

func isLowerHex64(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

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

func downloadUpgradeTarball(asset releaseAsset, expectedSHA, dir string) (string, *upgradeFailure) {
	resp, fail := httpGetAsset(asset, upgradeBodyBudget)
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
			"the download of %s broke mid-stream — nothing was changed", asset.name)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA {
		os.Remove(tmpPath)
		return "", upgradeFail(http.StatusBadGateway,
			"sha256 mismatch on %s: checksums.txt promises %s, got %s — the download is corrupt or tampered; nothing was changed",
			asset.name, expectedSHA, got)
	}
	return tmpPath, nil
}

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

func (s *apiServer) executeUpgrade() (string, *upgradeFailure) {
	tag, fail := s.pinUpgradeRelease()
	if fail != nil {
		return "", fail
	}
	// Re-check against the PINNED release (the cache may be stale): strictly
	// newer, so a lagging release list can never turn an upgrade into a
	// downgrade.
	if !releaseIsNewer(tag, appVersion) {
		return "", upgradeFail(http.StatusConflict,
			"GitHub's current latest (%s) is not newer than the running build (%s) — nothing newer to install",
			tag, appVersion)
	}

	base := s.releaseSiteBaseURL()
	asset := pinnedReleaseAsset(base, tag, releaseAssetName(tag))
	expectedSHA, fail := fetchExpectedSHA(pinnedReleaseAsset(base, tag, checksumsAssetName), asset.name)
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
	defer os.Remove(tmpPath)

	if fail := smokeTestBinary(tmpPath); fail != nil {
		return "", fail
	}

	bakPath := exePath + ".bak"
	if err := os.Rename(exePath, bakPath); err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot back up the running binary to %s — nothing was changed: %v", bakPath, err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		if restoreErr := os.Rename(bakPath, exePath); restoreErr != nil {
			log.Printf("[upgrade] CRITICAL: swap failed (%v) AND restoring the backup failed (%v) — %s is missing; restore it manually from %s",
				err, restoreErr, exePath, bakPath)
			return "", upgradeFail(http.StatusInternalServerError,
				"the binary swap failed AND the automatic backup restore failed — manual attention needed (backup at %s): %v", bakPath, err)
		}
		return "", upgradeFail(http.StatusInternalServerError,
			"the binary swap failed — the old binary was restored and keeps serving: %v", err)
	}
	return tag, nil
}

func restartIntoUpgradedBinary(exePath string) {
	time.Sleep(upgradeRestartDelay)
	log.Printf("[upgrade] restarting: exec %s (argv %v)", exePath, os.Args)
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		log.Printf("[upgrade] CRITICAL: exec of the upgraded binary failed (%v) — the OLD build keeps serving; the new binary is installed at %s and takes over on the next restart", err, exePath)
	}
}

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
	if err != nil {
		return "", "", upgradeFail(http.StatusInternalServerError, "%s", err.Error())
	}
	log.Printf("[upgrade] release %s verified and swapped into %s (backup: %s.bak); restarting in %v",
		version, exePath, exePath, upgradeRestartDelay)
	return version, exePath, nil
}

func (s *apiServer) scheduleUpgradeRestart(exePath string) {
	restart := s.upgradeRestart
	if restart == nil {
		restart = restartIntoUpgradedBinary
	}
	// Mark before the delay so live SSE handlers report station-shutdown
	// rather than peer disconnects. If the restart seam returns (e.g. a
	// failed syscall.Exec), clear it so the old process keeps serving
	// honestly.
	s.markStationShutdown()
	go func() {
		restart(exePath)
		s.clearStationShutdown()
	}()
}

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
