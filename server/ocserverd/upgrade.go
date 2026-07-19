package main

// upgrade.go — the upgrade EXECUTION body behind POST /api/update/upgrade
// (exit-beta: the seam update_check.go deliberately left as an honest 501).
//
// Shape of one upgrade (everything fallible runs SYNCHRONOUSLY in the
// request, so the owner's click gets a real answer, not a fire-and-forget):
//
//	1. re-fetch the updater's /api/latest (bounded) — the version + sha256
//	   are pinned AT TRIGGER TIME, never trusted from the 5-min cache;
//	2. download GET /api/binary?version= (bounded) into a temp file IN THE
//	   RUNNING BINARY'S OWN DIRECTORY (same filesystem → the later rename is
//	   atomic), hashing while streaming;
//	3. verify: the computed sha256 must equal BOTH the /api/latest metadata
//	   digest and the download's X-Checksum-Sha256 header — any mismatch
//	   aborts with an honest error and nothing on disk has changed;
//	4. smoke-test the candidate (`<tmp> --help` must exit 0) — a
//	   wrong-architecture or truncated-yet-published artifact is caught
//	   HERE, while the old binary is still untouched;
//	5. back up the running binary to <exe>.bak (exactly ONE backup is kept:
//	   each successful upgrade overwrites the previous .bak — that file IS
//	   the manual rollback: stop the server, `mv ocserverd.bak ocserverd`,
//	   start. No automatic GC: one stale ~25MB file beside the binary is a
//	   cheap permanent escape hatch, so success does NOT delete it);
//	6. atomically rename the verified temp over the exe path (a failure
//	   here restores the .bak before answering, so the disk never ends up
//	   binary-less);
//	7. answer 200 {status:"restarting", target_version} — only now, with
//	   the swap already landed — then, off the request path after a short
//	   flush delay, re-exec the swapped path (syscall.Exec keeps the PID).
//
// WHY re-exec (and not exit-and-let-a-supervisor-restart): the two real
// deployment forms are (a) the launchd LaunchAgent com.officraft.serve —
// bin/serve `exec`s ocserverd, so this process IS the tracked job and exec
// keeps its PID (launchd does not care; KeepAlive additionally covers an
// exec that dies at boot), and (b) the invited-user manual `./ocserverd
// serve` foreground run with NO supervisor at all — exiting there would
// turn "upgrade" into "outage". Re-exec is the one mechanism that restarts
// correctly under both. Listener/DB handoff is the launchctl-kickstart
// posture autodeploy already uses: Go marks fds close-on-exec, so the old
// listener closes at exec and the new image re-binds; SQLite recovers a
// mid-flight process image swap exactly like a crash (WAL/busy_timeout).
//
// If the exec itself fails (should be impossible after the smoke test), the
// OLD process keeps serving unharmed — the new binary is already verified
// on disk, so the next restart (manual / launchd) comes up on it; the
// failure is logged loudly instead of silently retried.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// upgradeLatestTimeout bounds the trigger-time /api/latest re-fetch.
	upgradeLatestTimeout = 5 * time.Second
	// upgradeDownloadTimeout bounds the whole binary download (the artifact
	// runs ~25MB; two minutes is generous even over a slow tunnel).
	upgradeDownloadTimeout = 2 * time.Minute
	// upgradeMaxBytes caps the download (mirrors ocupdaterd's publish cap) so
	// a misbehaving updater cannot fill the disk through this path.
	upgradeMaxBytes = 256 << 20
	// upgradeSmokeTimeout bounds the candidate's `--help` smoke run.
	upgradeSmokeTimeout = 15 * time.Second
	// upgradeRestartDelay is how long the post-response goroutine waits before
	// re-exec'ing — long enough for the 200 to flush to the owner's browser.
	upgradeRestartDelay = 750 * time.Millisecond
)

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

// updaterRelease is /api/latest's metadata face (the fields this path needs;
// ocupdaterd is outside the frozen spec, so this is a plain JSON decode).
type updaterRelease struct {
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// fetchUpdaterRelease pins the release to install AT TRIGGER TIME on the
// followed channel: the cached check (update_check.go) is only the
// precondition gate — the digest the swap verifies against must come from a
// fresh authoritative read.
func fetchUpdaterRelease(cfgURL, cfgCode, channel string) (updaterRelease, *upgradeFailure) {
	var rel updaterRelease
	req, err := http.NewRequest(http.MethodGet,
		strings.TrimRight(cfgURL, "/")+"/api/latest?channel="+url.QueryEscape(channel), nil)
	if err != nil {
		return rel, upgradeFail(http.StatusBadGateway, "cannot build the updater request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfgCode)
	client := &http.Client{Timeout: upgradeLatestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return rel, upgradeFail(http.StatusBadGateway,
			"cannot reach the updater server to pin the release — nothing was changed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, upgradeFail(http.StatusBadGateway,
			"the updater's /api/latest answered %d — nothing was changed", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return rel, upgradeFail(http.StatusBadGateway,
			"cannot decode the updater's /api/latest — nothing was changed: %v", err)
	}
	if rel.Version == "" {
		return rel, upgradeFail(http.StatusBadGateway,
			"the updater's /api/latest carried no version — nothing was changed")
	}
	if len(rel.SHA256) != 64 || !isLowerHex64(rel.SHA256) {
		return rel, upgradeFail(http.StatusBadGateway,
			"the updater's /api/latest carried no usable sha256 digest — refusing an unverifiable download; nothing was changed")
	}
	return rel, nil
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

// downloadUpgradeBinary streams GET /api/binary?version= into a temp file in
// dir, hashing while copying, and verifies the digest against BOTH the pinned
// metadata sha and the response's X-Checksum-Sha256. Returns the temp path;
// every failure removes the temp file itself.
func downloadUpgradeBinary(cfgURL, cfgCode string, rel updaterRelease, dir string) (string, *upgradeFailure) {
	req, err := http.NewRequest(http.MethodGet,
		strings.TrimRight(cfgURL, "/")+"/api/binary?version="+url.QueryEscape(rel.Version), nil)
	if err != nil {
		return "", upgradeFail(http.StatusBadGateway, "cannot build the download request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfgCode)
	client := &http.Client{Timeout: upgradeDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", upgradeFail(http.StatusBadGateway,
			"downloading version %s failed — nothing was changed: %v", rel.Version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", upgradeFail(http.StatusBadGateway,
			"the updater's /api/binary answered %d for version %s — nothing was changed",
			resp.StatusCode, rel.Version)
	}
	if hdr := strings.ToLower(strings.TrimSpace(resp.Header.Get("X-Checksum-Sha256"))); hdr != "" && hdr != rel.SHA256 {
		return "", upgradeFail(http.StatusBadGateway,
			"the updater contradicts itself: /api/latest promises sha256 %s but the download declares %s — refusing; nothing was changed",
			rel.SHA256, hdr)
	}

	tmp, err := os.CreateTemp(dir, ".ocserverd-upgrade-*")
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
			"the download of version %s broke mid-stream — nothing was changed", rel.Version)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != rel.SHA256 {
		os.Remove(tmpPath)
		return "", upgradeFail(http.StatusBadGateway,
			"sha256 mismatch on the downloaded binary: expected %s, got %s — the download is corrupt or tampered; nothing was changed",
			rel.SHA256, got)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot mark the downloaded binary executable — nothing was changed: %v", err)
	}
	return tmpPath, nil
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

// executeUpgrade runs steps 1–6 (pin → download → verify → smoke → backup →
// swap). On success the NEW binary sits at exePath and the OLD one at
// exePath+".bak"; on any failure the old binary is untouched and still the
// one at exePath. Returns the pinned version installed.
func (s *apiServer) executeUpgrade(cfgURL, cfgCode, channel string) (string, *upgradeFailure) {
	rel, fail := fetchUpdaterRelease(cfgURL, cfgCode, channel)
	if fail != nil {
		return "", fail
	}
	// The honesty gate re-runs against the PINNED release: the cache that
	// enabled the button may be stale (e.g. the updater rolled back since).
	if rel.Version == appVersion || (rel.GitSHA != "" && rel.GitSHA == s.processSHA) {
		return "", upgradeFail(http.StatusConflict,
			"the updater's current latest (%s) IS the running build — nothing newer to install", rel.Version)
	}

	exePath, err := s.upgradeTargetPath()
	if err != nil {
		return "", upgradeFail(http.StatusInternalServerError,
			"cannot resolve the running binary's path — nothing was changed: %v", err)
	}

	tmpPath, fail := downloadUpgradeBinary(cfgURL, cfgCode, rel, filepath.Dir(exePath))
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
	return rel.Version, nil
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
// (auto_update.go): precondition gates (updater configured, a newer version
// known) as honest 409-shaped failures, ONE upgrade at a time (TryLock — a
// concurrent trigger answers 409, never a second swap), then the full
// verified execution body. On success the swap has LANDED on disk and the
// caller owns scheduling the re-exec (via scheduleUpgradeRestart).
func (s *apiServer) runUpgrade() (version, exePath string, fail *upgradeFailure) {
	cfgURL, cfgCode, cfgChannel := s.updaterConfig()
	if cfgURL == "" || cfgCode == "" {
		return "", "", upgradeFail(http.StatusConflict,
			"no updater server is configured — set updater_url and updater_invite_code in settings first")
	}
	available, _ := s.updateStatus()
	if !available {
		return "", "", upgradeFail(http.StatusConflict,
			"no newer version is known — the running build is the latest this updater has published")
	}
	if !s.upgradeMu.TryLock() {
		return "", "", upgradeFail(http.StatusConflict, "an upgrade is already in progress")
	}
	defer s.upgradeMu.Unlock()

	version, fail = s.executeUpgrade(cfgURL, cfgCode, cfgChannel)
	if fail != nil {
		return "", "", fail
	}
	exePath, err := s.upgradeTargetPath()
	if err != nil { // unreachable: executeUpgrade resolved the same path
		return "", "", upgradeFail(http.StatusInternalServerError, "%s", err.Error())
	}
	log.Printf("[upgrade] version %s verified and swapped into %s (backup: %s.bak); restarting in %v",
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
