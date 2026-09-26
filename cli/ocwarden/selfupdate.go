// Self-update: the warden keeps ITSELF (and its sibling ocagent) current by
// polling the server, swapping in a fresh binary, then exec'ing the new ocwarden
// IN PLACE (same PID, argv, env).
//
// WHY EXEC-IN-PLACE, NOT EXIT-AND-LET-LAUNCHD-RELAUNCH: observed on real macOS
// hosts (twice, reproducibly), launchd does NOT relaunch an exited warden — the
// gui-domain LaunchAgent job sits "not running, last exit 0" until a manual
// `launchctl kickstart`. With exec the PID never dies, so launchd sees no stop.
//
// WHEN THE EXEC FAILS, THE TWO CALLERS ARE NOT SYMMETRIC:
//   - SELF-UPDATE (execInPlace): the binary on disk is already replaced, so it
//     falls back to exit(0) rather than stay up on a retired image.
//   - RENEWAL (execAfterRenewal, renewapply.go): the new credential is already on
//     disk and any later restart picks it up; exiting would trade a recoverable
//     state for a dead warden launchd does not relaunch. It logs and keeps running.
//
// SWAP ORACLE — RAW BYTES, NOT A VERSION STRING: the hosted binaries carry no
// version stamp, and an embedded git_sha could never equal the server's
// post-commit sha → an infinite "update" loop. /api/version's git_sha is ONLY a
// cheap gate to skip the download.
//
// NO CODE-SIGNING IN THIS LOOP (owner 2026-07-31). A signature must not become a
// gate: a self-built certificate fails `codesign --verify` on any machine that
// never trusted it, so a hard gate would brick every fleet update. Do not conflate
// this with the TCC identity anchor (cli/officraft), which is recognised by its
// BYTES and never overwritten.
//
// After replacing ocagent the warden does NOTHING: ocagent is spawned fresh per
// session, so the next spawn execs the new sibling.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	versionPath      = "/api/version"
	wardenBinaryPath = "/api/warden/binary"
	agentBinaryPath  = "/api/agent/binary"

	// The same telemetry ingest the hardware loop posts to; the server merges the
	// `self_update` field onto the per-agent entry.
	selfUpdateReportPath    = "/api/monitoring/telemetry"
	selfUpdateReportBudget  = 10 * time.Second
	selfUpdateHashPrefixLen = 12

	selfUpdateInterval     = 15 * time.Minute
	selfUpdateBackoffStart = 1 * time.Minute
	selfUpdateBackoffCap   = 30 * time.Minute
	// Not for the binary downloads: a wall-clock ceiling cut a slow but
	// progressing ocagent download mid-body (seth-m5, 2026-08-03 and 08-04).
	selfUpdateRequestBudget = 60 * time.Second

	selfUpdateDialTimeout         = 10 * time.Second
	selfUpdateTLSHandshakeTimeout = 10 * time.Second
	selfUpdateHeaderTimeout       = 30 * time.Second
	selfUpdateStallTimeout        = 60 * time.Second
	// selfUpdateDownloadBudget is a BACKSTOP for a source that dribbles just fast
	// enough to keep the stall guard quiet: credential renewal runs on the same
	// loop goroutine. It is PER BINARY, so one cycle can hold the loop — and a due
	// renewal or RenewNow demand — for selfUpdateRequestBudget + 2 × this.
	selfUpdateDownloadBudget = 30 * time.Minute

	selfUpdateProbeBudget = 10 * time.Second
)

type getter func(path string) (int, []byte, error)

type selfUpdateEvent struct {
	Binary  string `json:"binary"`
	OldHash string `json:"old_hash"`
	NewHash string `json:"new_hash"`
	At      string `json:"at"`
}

func hashPrefix(data []byte) string {
	sum := sha256.Sum256(data)
	full := hex.EncodeToString(sum[:])
	if len(full) > selfUpdateHashPrefixLen {
		return full[:selfUpdateHashPrefixLen]
	}
	return full
}

func httpGetter(client *http.Client, base, token string) getter {
	return func(path string) (int, []byte, error) {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			return 0, nil, err
		}
		return sendStationGet(client, req, token, nil)
	}
}

func httpDownloader(base, token string) getter {
	return func(path string) (int, []byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), selfUpdateDownloadBudget)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return 0, nil, err
		}
		return sendStationGet(selfUpdateDownloadClient, req, token, func(body io.ReadCloser) io.ReadCloser {
			return newStallGuard(body, cancel, selfUpdateStallTimeout)
		})
	}
}

func sendStationGet(client *http.Client, req *http.Request, token string, wrap func(io.ReadCloser) io.ReadCloser) (int, []byte, error) {
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	body := resp.Body
	if wrap != nil {
		body = wrap(body)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

var selfUpdateDialer = &net.Dialer{Timeout: selfUpdateDialTimeout}

var selfUpdateTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           selfUpdateDialer.DialContext,
	TLSHandshakeTimeout:   selfUpdateTLSHandshakeTimeout,
	ResponseHeaderTimeout: selfUpdateHeaderTimeout,
	// A Transport that supplies its own DialContext does not negotiate HTTP/2
	// unless asked; http.DefaultTransport, which these requests used before, does.
	ForceAttemptHTTP2: true,
	IdleConnTimeout:   90 * time.Second,
}

var (
	selfUpdateRequestClient  = &http.Client{Timeout: selfUpdateRequestBudget, Transport: selfUpdateTransport}
	selfUpdateDownloadClient = &http.Client{Timeout: 0, Transport: selfUpdateTransport}
)

// Same shape as server/ocserverd/upgrade.go's; copied because the two live in
// separate Go modules.
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

func (g *stallGuard) Close() error {
	g.timer.Stop()
	g.stop()
	return g.inner.Close()
}

type updaterOps interface {
	readFile(path string) ([]byte, error)
	writeFile(path string, data []byte, perm os.FileMode) error
	chmod(path string, perm os.FileMode) error
	rename(oldpath, newpath string) error
	remove(path string) error
	probe(bin string) error
}

// `<bin> --help` is the side-effect-free smoke invocation CI also trusts.
type osUpdaterOps struct{ runner CmdRunner }

func (osUpdaterOps) readFile(p string) ([]byte, error) { return os.ReadFile(p) }
func (osUpdaterOps) writeFile(p string, d []byte, m os.FileMode) error {
	return os.WriteFile(p, d, m)
}
func (osUpdaterOps) chmod(p string, m os.FileMode) error { return os.Chmod(p, m) }
func (osUpdaterOps) rename(a, b string) error            { return os.Rename(a, b) }
func (osUpdaterOps) remove(p string) error               { return os.Remove(p) }
func (o osUpdaterOps) probe(bin string) error {
	out, err := o.runner.Run(bin, "--help")
	if err != nil {
		return fmt.Errorf("exec %s --help: %w", bin, err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("health probe: %s --help produced no output", bin)
	}
	return nil
}

type updater struct {
	get       getter
	download  getter
	ops       updaterOps
	selfPath  string
	agentPath string

	interval     time.Duration
	backoffStart time.Duration
	backoffCap   time.Duration

	sleep    func(context.Context, time.Duration) bool
	kick     chan struct{}
	execSelf func() error
	exit     func(int)
	logf     func(string, ...any)

	post    Poster
	agentID string
	now     func() time.Time

	// writeTok is install's tokfileWriter, not a second implementation.
	// envToken is OC_TOKEN from the RAW environment, never the tokfile-folded
	// view: non-empty means the token survives the exec (renewapply.go).
	renew       credentialRenewer
	verify      credentialVerifier
	writeTok    func(path, token string) error
	tokfilePath string
	token       string
	envToken    string

	// renewedAwaitingRestart: a fresh credential is on disk but this process still
	// runs on the old one. Process-local on purpose. Without it
	// maybeRenewCredential would judge the OLD in-memory token still due and renew
	// every poll, forever, fleet-wide.
	renewedAwaitingRestart bool
	// renewDemanded (the `renew` warden-command, RenewNow) is ATOMIC: the SSE
	// reader raises it, the poll loop consumes it. It is cleared only once a
	// replacement has been written — a demand consumed on entry is lost on any
	// failure and the machine sits on the retired key.
	renewDemanded atomic.Bool
	// execPendingAfterRenewal must NOT be merged with renewedAwaitingRestart: the
	// "just-minted credential is already due" path latches WITHOUT wanting an exec
	// (exec'ing there is the once-per-poll runaway). Never cleared — a successful
	// exec does not return.
	execPendingAfterRenewal bool
	execAttempts            int

	lastSwap *selfUpdateEvent

	lastSHA string

	// credLifetimeSecs is the station's auth.warden_credential_lifetime_secs (GET
	// /api/machines/credential-policy); ZERO means never answered, and
	// credentialRenewAfter turns that into the default. In memory ONLY on purpose:
	// a restart can only fall back to the conservative default, never stick on a
	// stale short value; a persisted file's corruption would make the fleet renew
	// every poll. Poll-loop goroutine only, hence no atomic.
	credLifetimeSecs int64
}

func (u *updater) renewAfter() time.Duration {
	return credentialRenewAfter(u.credLifetimeSecs, u.agentID)
}

// run waits BEFORE the first check so a freshly-installed warden does not
// stampede the server the instant it boots.
func (u *updater) run(ctx context.Context) {
	backoff := u.backoffStart
	wait := u.interval
	for {
		if !u.waitNext(ctx, wait) {
			return
		}
		// Renewal runs EVERY turn and BEFORE checkOnce: checkOnce returns early
		// whenever the server sha is unchanged, and credentials expire on their own
		// calendar.
		if u.maybeRenewCredential() {
			u.execPendingAfterRenewal = true
		}
		// Retried every turn: an exec can fail transiently (ETXTBSY mid-swap,
		// EAGAIN). Not folded into the branch above — maybeRenewCredential answers
		// false forever once the credential is on disk (renewedAwaitingRestart).
		if u.execPendingAfterRenewal {
			u.execAfterRenewal()
		}
		wardenSwapped, err := u.checkOnce()
		if err != nil {
			u.logf("[ocwarden] self-update: cycle failed (%v); retrying in %s", err, backoff)
			wait = backoff
			backoff = nextSelfUpdateBackoff(backoff, u.backoffCap)
			continue
		}
		backoff = u.backoffStart
		wait = u.interval
		// Announced BEFORE the in-place exec: the exec'd process has no memory of
		// the swap.
		if u.lastSwap != nil {
			u.announceSelfUpdate()
			u.lastSwap = nil
		}
		if wardenSwapped {
			u.execInPlace("self-update: ocwarden replaced")
			return
		}
	}
}

// EVERY CALLER MUST HAVE ALREADY COMMITTED ITS CHANGE TO DISK: past this point
// the process that would have retried is gone.
func (u *updater) execInPlace(reason string) {
	u.logf("[ocwarden] %s — exec'ing the new binary in place (same PID)", reason)
	if u.execSelf != nil {
		err := u.execSelf()
		u.logf("[ocwarden] in-place exec failed (%v) — falling back to exit(0)", err)
	}
	u.exit(0)
}

func (u *updater) waitNext(ctx context.Context, wait time.Duration) bool {
	sleepCtx, cancelSleep := context.WithCancel(ctx)
	defer cancelSleep()
	done := make(chan bool, 1)
	go func() { done <- u.sleep(sleepCtx, wait) }()
	select {
	case <-ctx.Done():
		return false
	case alive := <-done:
		return alive
	case <-u.kick:
		return true
	}
}

// Kick is called by the SSE transport on every successful (re)connect (the
// server may have moved, i.e. a fresh binary may be served) and by the `update`
// warden-command. Coalesced (cap 1); each woken cycle is sha-gated cheap.
func (u *updater) Kick() {
	if u.kick == nil {
		return
	}
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// RenewNow raises a flag and kicks; it does NOT renew. The work stays in
// maybeRenewCredential on the loop's own goroutine, where its guards live —
// never on the SSE reader's goroutine, concurrently with a poll.
//
// Flag BEFORE kick: kicking first admits a cycle that finds no demand, and the
// demand then waits out a whole poll interval.
func (u *updater) RenewNow() {
	u.renewDemanded.Store(true)
	u.Kick()
}

func (u *updater) checkOnce() (bool, error) {
	sha, err := u.serverSHA()
	if err != nil {
		return false, err
	}
	if sha != "" && sha == u.lastSHA {
		return false, nil
	}

	// ocagent FIRST, so the fresh ocagent is in place before an ocwarden swap
	// exec's us.
	if u.agentPath != "" {
		if _, err := u.reconcileBinary(agentBinaryPath, u.agentPath, "ocagent"); err != nil {
			return false, err
		}
	}

	wardenSwapped := false
	if u.selfPath != "" {
		wardenSwapped, err = u.reconcileBinary(wardenBinaryPath, u.selfPath, "ocwarden")
		if err != nil {
			return false, err
		}
	}

	u.lastSHA = sha
	return wardenSwapped, nil
}

func (u *updater) serverSHA() (string, error) {
	status, body, err := u.get(versionPath)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", versionPath, err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", versionPath, status)
	}
	var v struct {
		GitSHA string `json:"git_sha"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("decode %s: %w", versionPath, err)
	}
	return v.GitSHA, nil
}

func (u *updater) reconcileBinary(path, livePath, name string) (bool, error) {
	status, body, err := u.download(path)
	if err != nil {
		return false, fmt.Errorf("download %s: %w", name, err)
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("download %s: status %d", name, status)
	}
	if len(body) == 0 {
		return false, fmt.Errorf("download %s: empty body", name)
	}

	var live []byte
	if data, rerr := u.ops.readFile(livePath); rerr == nil {
		live = data
		if bytes.Equal(live, body) {
			return false, nil
		}
	}

	dir := filepath.Dir(livePath)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.selfupdate.%d", name, os.Getpid()))
	cleanup := true
	defer func() {
		if cleanup {
			_ = u.ops.remove(tmp)
		}
	}()

	if err := u.ops.writeFile(tmp, body, 0o755); err != nil {
		return false, fmt.Errorf("write temp %s: %w", name, err)
	}
	// WriteFile's mode is umask-masked; re-assert 0755.
	if err := u.ops.chmod(tmp, 0o755); err != nil {
		return false, fmt.Errorf("chmod temp %s: %w", name, err)
	}

	if err := u.ops.probe(tmp); err != nil {
		return false, fmt.Errorf("verify %s failed — keeping current binary: %w", name, err)
	}

	// `.prev`, not `.bak`: stays clear of the CI hygiene `.bak$` denylist.
	if live != nil {
		prev := livePath + ".prev"
		if err := u.ops.writeFile(prev, live, 0o755); err != nil {
			return false, fmt.Errorf("backup current %s -> %s: %w", name, prev, err)
		}
	}

	if err := u.ops.rename(tmp, livePath); err != nil {
		return false, fmt.Errorf("atomic swap %s -> %s: %w", name, livePath, err)
	}
	cleanup = false
	u.logf("[ocwarden] self-update: replaced %s at %s (backup: %s.prev)", name, livePath, livePath)
	oldHash := ""
	if live != nil {
		oldHash = hashPrefix(live)
	}
	u.lastSwap = &selfUpdateEvent{
		Binary:  name,
		OldHash: oldHash,
		NewHash: hashPrefix(body),
		At:      u.clock().UTC().Format(time.RFC3339),
	}
	return true, nil
}

func (u *updater) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

func (u *updater) announceSelfUpdate() {
	ev := u.lastSwap
	if ev == nil || u.post == nil || strings.TrimSpace(u.agentID) == "" {
		return
	}
	// No agent_id key: identity is the verified JWT sub and the frozen ingest
	// schema refuses undeclared fields (one would 422 the announcement).
	payload := map[string]any{
		"self_update": map[string]any{
			"binary":   ev.Binary,
			"old_hash": ev.OldHash,
			"new_hash": ev.NewHash,
			"at":       ev.At,
		},
	}
	status, _ := u.post(selfUpdateReportPath, payload)
	if status < 200 || status >= 300 {
		u.logf("[ocwarden] self-update: announce POST returned status %d (ignored; swap already applied)", status)
		return
	}
	u.logf("[ocwarden] self-update: announced %s swap %s->%s", ev.Binary, ev.OldHash, ev.NewHash)
}

func nextSelfUpdateBackoff(cur, ceiling time.Duration) time.Duration {
	if cur < selfUpdateBackoffStart {
		cur = selfUpdateBackoffStart
	}
	next := cur * 2
	if next > ceiling {
		return ceiling
	}
	return next
}

func resolveSelfExe(executable func() (string, error)) string {
	exe, err := executable()
	if err != nil {
		return ""
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		return resolved
	}
	return exe
}

// selfUpdateAgentPath is the ocagent path the SELF-UPDATE loop keeps current.
// Unlike resolveOcAgentBin (the spawn shim's resolver, which falls back to the
// in-tree <repoRoot>/cli/ocagent/ocagent when no sibling exists), this
// UNCONDITIONALLY targets the home sibling <dir(exe)>/ocagent with NO
// exists-check: on a remote / manual install (no OC_AGENT_BIN, sibling not
// copied) an exists-fallback would point self-update at a dead path and every
// tick would fail; the sibling lets the FIRST tick download and populate it.
func selfUpdateAgentPath(executable func() (string, error)) string {
	exe := resolveSelfExe(executable)
	if exe == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "ocagent")
}

// A named function, not a closure, so buildSelfUpdater carries no syscall of its
// own.
func syscallExecImage(path string, argv, envv []string) error {
	refuseInTestBinary("syscallExecImage")
	_ = os.Stdout.Sync()
	_ = os.Stderr.Sync()
	return syscall.Exec(path, argv, envv)
}

// rawEnv carries the UNFOLDED environment, and is a struct so that handing over
// the wrong view is a compile error. realMain holds `env` (raw) and `renv`
// (OC_TOKEN folded in from the tokfile). Given the folded view, envToken reports
// the tokfile as if exported, the infinite-exec guard trips on every launchd
// warden, and the fleet silently stops renewing — `newSelfUpdater(cfg, renv,
// logf)` once compiled and passed the suite. A named func type would not help:
// an unnamed func value converts to it implicitly.
type rawEnv struct{ lookup func(string) string }

// Deliberately no refuseInTestBinary here: this body starts no process, and the
// refusal only made the production entry unobservable to tests.
// syscallExecImage keeps it.
func newSelfUpdater(cfg Config, env rawEnv, logf func(string, ...any)) *updater {
	return buildSelfUpdater(cfg, env, logf, os.Executable, syscallExecImage)
}

// buildSelfUpdater exists so what the constructor WIRES can be tested (a syntax
// check on the renewal lines was bypassed by edits that rename nothing).
// execImage is injected so a test can only build an updater whose execSelf goes
// nowhere.
func buildSelfUpdater(cfg Config, env rawEnv, logf func(string, ...any), executable func() (string, error), execImage func(string, []string, []string) error) *updater {
	selfPath := resolveSelfExe(executable)
	agentPath := selfUpdateAgentPath(executable)

	// Short-timeout client: the announce runs on the swap→exec path, where a hung
	// station must not hold up the exec.
	reportClient := &http.Client{Timeout: selfUpdateReportBudget}
	u := &updater{
		get:          httpGetter(selfUpdateRequestClient, cfg.Base, cfg.Token),
		download:     httpDownloader(cfg.Base, cfg.Token),
		ops:          osUpdaterOps{runner: newCmdRunner(selfUpdateProbeBudget)},
		selfPath:     selfPath,
		agentPath:    agentPath,
		interval:     selfUpdateInterval,
		backoffStart: selfUpdateBackoffStart,
		backoffCap:   selfUpdateBackoffCap,
		sleep:        sleepUntil,

		kick: make(chan struct{}, 1),

		execSelf: func() error { return execImage(selfPath, os.Args, os.Environ()) },
		exit:     os.Exit,
		logf:     logf,
		post:     httpPoster(reportClient, cfg.Base, cfg.Token),
		agentID:  cfg.ID,
		now:      time.Now,
	}
	// newRenewalWiring builds its own HTTP client: the short announce budget
	// beside the station request client is one character away.
	u.apply(newRenewalWiring(cfg, env.lookup))
	return u
}
