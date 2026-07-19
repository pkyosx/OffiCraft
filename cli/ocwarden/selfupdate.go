// Self-update: the warden keeps ITSELF (and its sibling ocagent) current by
// polling the authoritative server and atomically swapping in a fresh binary —
// no manual `ocwarden install --force` after every land. This closes the
// self-contained-supply-chain loop: step0 committed bin/ocagent as a prebuilt,
// step1 exposed GET /api/agent/binary (sibling of GET /api/warden/binary), and
// THIS step teaches the running warden to pull + verify + swap those binaries and
// then exec the new ocwarden IN PLACE (same PID, same argv/env).
//
// WHY EXEC-IN-PLACE, NOT EXIT-AND-LET-LAUNCHD-RELAUNCH (load-bearing): the original
// design exited 0 and bet on launchd KeepAlive relaunching us. Observed on real
// macOS hosts (twice, reproducibly): launchd does NOT relaunch — the gui-domain
// LaunchAgent job sits "not running, last exit 0" until a manual `launchctl
// kickstart`, so every self-update killed the machine's warden. The fix removes
// launchd from the loop entirely: after the on-disk swap, syscall.Exec replaces our
// own process image with the new binary. The PID never dies, so from launchd's view
// the job never stopped and no relaunch semantics are involved. If the exec itself
// fails (it only returns on failure) we fall back to the old exit(0) path — no worse
// than before — with the failure logged.
//
// SWAP ORACLE — WHY CONTENT-HASH, NOT A VERSION STRING (load-bearing):
// The server's /api/version reports git_sha of the RUNNING server commit, but the
// hosted binaries carry NO version stamp and no checksum header. We deliberately do
// NOT embed a git_sha into ocwarden via ldflags to compare against it, for two
// reasons: (1) a committed prebuilt is built BEFORE the commit that carries it, so
// its embedded sha would never equal the server's post-commit sha → an infinite
// "update" loop; (2) an ldflags stamp risks drifting the CI 7d committed-prebuilt
// parity dryrun (which smoke-compares `--help` of committed vs fresh). Instead the
// swap decision is made by comparing the RAW BYTES of the live binary against the
// bytes the server serves: identical ⇒ already current (never swap, never restart);
// different ⇒ a real new artifact ⇒ verify + swap. This is drift-proof and
// loop-free by construction. /api/version's git_sha is used ONLY as a cheap gate to
// avoid downloading when the server has not moved since our last reconcile.
//
// ANTI-SUICIDE GUARDRAILS (a half-downloaded / wrong-arch binary must never brick
// the warden):
//   - VERIFY-BEFORE-SWAP: the freshly downloaded binary must EXEC and exit 0
//     (`--help`, the same side-effect-free smoke invocation CI trusts). A truncated
//     / corrupt / wrong-arch download fails to exec here → we bail with the live
//     binary UNTOUCHED.
//   - RETREAT PATH: the live binary is backed up to `<path>.prev` before the swap,
//     so a bad new binary can be rolled back.
//   - ATOMIC SWAP: same-directory rename (atomic on the local fs). A running process
//     keeps its already-mapped text pages, so replacing our own on-disk binary is
//     safe; the new bytes take effect only on the next exec (our own in-place exec).
//   - BACKOFF: a failed cycle (server down / auth reject / verify fail) never swaps
//     and retreats onto an exponential backoff so a broken server is not hammered.
//
// SIGNATURE OBSERVABILITY, NEVER ENFORCEMENT (T-33d5): release machines sign the
// served binaries with a stable self-signed identity (bin/codesign-artifact) so
// macOS TCC grants survive swaps. After a swap the loop LOGS the new binary's
// code-signing identity (adhoc / the signing CN / unsigned) purely for the
// operator's eyeballs. It deliberately does NOT verify-or-refuse: a self-built
// certificate fails `codesign --verify` on any machine that never trusted it,
// so a hard gate would brick every fleet update. The anti-suicide gates remain
// the exec probe + content hash alone.
//
// ocwarden vs ocagent DIFFERENCE: after replacing ITSELF, ocwarden execs the new
// binary in place (see above). After replacing ocagent it does NOTHING — ocagent
// is spawned fresh per session, so the next spawn simply execs the new sibling; an
// already-running agent keeps its old binary until it restarts on its own, which is
// harmless.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// versionPath is the cheap "has the server moved?" gate (GET /api/version →
	// handle_version). We read only its git_sha field.
	versionPath = "/api/version"
	// wardenBinaryPath / agentBinaryPath stream the committed prebuilts (GATED behind
	// the warden's own Bearer token; sibling handlers handle_warden_binary /
	// handle_agent_binary added in step1).
	wardenBinaryPath = "/api/warden/binary"
	agentBinaryPath  = "/api/agent/binary"

	// selfUpdateReportPath is where a completed self-update is announced. It reuses
	// the SAME telemetry ingest endpoint the 30s hardware loop already posts to — the
	// server merges the extra `self_update` field onto the per-agent entry (see
	// service/handlers.py handle_ingest_telemetry). A self-update is otherwise SILENT;
	// this one best-effort POST makes "warden X swapped from <old> to <new> at <ts>"
	// observable on the server without standing up a second reporting channel.
	selfUpdateReportPath = "/api/monitoring/telemetry"
	// selfUpdateReportBudget bounds the best-effort announce POST. Short because it
	// runs on the swap→exit critical path: a hung server must never delay the launchd
	// relaunch onto the new binary.
	selfUpdateReportBudget = 10 * time.Second
	// selfUpdateHashPrefixLen is how many hex chars of the sha256 we surface. A short
	// prefix is enough to eyeball "which build" without shipping the full digest.
	selfUpdateHashPrefixLen = 12

	// selfUpdateInterval is the poll cadence. 15m is a deliberate KNOB (see the
	// report): frequent enough that a land propagates within minutes, infrequent
	// enough that the gate GET is negligible load. Most ticks do ONE cheap
	// /api/version GET and stop (no binary download) because the server sha is
	// unchanged.
	selfUpdateInterval = 15 * time.Minute
	// selfUpdateBackoffStart / selfUpdateBackoffCap bound the retry backoff after a
	// failed cycle so a hard-down / auth-rejecting server is not hammered.
	selfUpdateBackoffStart = 1 * time.Minute
	selfUpdateBackoffCap   = 30 * time.Minute
	// selfUpdateHTTPTimeout bounds a single request. Generous because the binary
	// downloads are multi-MB; the /api/version gate is tiny and well within it.
	selfUpdateHTTPTimeout = 60 * time.Second
	// selfUpdateProbeBudget bounds the verify-before-swap `--help` exec.
	selfUpdateProbeBudget = 10 * time.Second
)

// ---------------------------------------------------------------------------
// http GET seam — one closure with base+Bearer baked in, returning (status, body).
// Mirrors main.go's Poster, but for the download/version half.
// ---------------------------------------------------------------------------

// getter GETs path and returns (status, body, transport-error). A non-nil error is
// a transport failure (DNS/refused/timeout); status<200||>=300 is a live-but-bad
// response the caller classifies.
type getter func(path string) (int, []byte, error)

// selfUpdateEvent is the observability record of ONE completed ocwarden self-swap:
// which binary, the old→new content-hash prefixes (the same raw-byte content that IS
// the swap oracle), and when it happened. It rides the existing telemetry POST as the
// `self_update` field so the server can log "warden self-updated" out of the silence.
type selfUpdateEvent struct {
	Binary  string `json:"binary"`   // "ocwarden" | "ocagent"
	OldHash string `json:"old_hash"` // sha256 prefix of the replaced binary ("" if none)
	NewHash string `json:"new_hash"` // sha256 prefix of the freshly-swapped binary
	At      string `json:"at"`       // RFC3339 UTC timestamp of the swap
}

// hashPrefix returns the first selfUpdateHashPrefixLen hex chars of sha256(data) — a
// short, human-eyeballable "which build" tag, not a security checksum.
func hashPrefix(data []byte) string {
	sum := sha256.Sum256(data)
	full := hex.EncodeToString(sum[:])
	if len(full) > selfUpdateHashPrefixLen {
		return full[:selfUpdateHashPrefixLen]
	}
	return full
}

// httpGetter builds the real GET-{base}{path} closure with a Bearer token.
func httpGetter(client *http.Client, base, token string) getter {
	return func(path string) (int, []byte, error) {
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, nil, err
		}
		return resp.StatusCode, body, nil
	}
}

// ---------------------------------------------------------------------------
// updaterOps — the injectable side-effect seam (filesystem + exec probe), so the
// reconcile logic is unit-tested without touching a live host or downloading over
// the network. Mirrors install.go's sysOps idea, narrowed to what a swap needs.
// ---------------------------------------------------------------------------

type updaterOps interface {
	readFile(path string) ([]byte, error)
	writeFile(path string, data []byte, perm os.FileMode) error
	chmod(path string, perm os.FileMode) error
	rename(oldpath, newpath string) error
	remove(path string) error
	// probe is the VERIFY-BEFORE-SWAP health check: it must EXEC the freshly
	// downloaded binary and return nil ONLY if it runs and exits 0. A truncated /
	// wrong-arch / corrupt download must yield a non-nil error here.
	probe(bin string) error
}

// osUpdaterOps wires the seam to the real OS. The probe runs `<bin> --help` — the
// same side-effect-free smoke invocation CI's 7d parity dryrun uses (no network, no
// files, no launchctl) — and requires exit 0 with non-empty output.
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

// ---------------------------------------------------------------------------
// updater — the self-update engine.
// ---------------------------------------------------------------------------

type updater struct {
	get       getter
	ops       updaterOps
	selfPath  string // live ocwarden (our own executable, symlinks resolved)
	agentPath string // live ocagent (home sibling, selfUpdateAgentPath)

	interval     time.Duration
	backoffStart time.Duration
	backoffCap   time.Duration

	sleep func(context.Context, time.Duration) bool // ctx-aware wait seam (sleepUntil)
	// kick is the EVENT-DRIVEN seam (T-c93d 方案A): a buffered (cap 1) wake channel
	// that cuts the poll wait short so checkOnce runs within seconds of a triggering
	// event instead of waiting out `interval`. run()'s wait selects on it (waitNext);
	// Kick() feeds it. Producers: the SSE transport calls Kick() on every successful
	// (re)connect — a reconnect means the server process may have moved, which for the
	// committed-prebuilt model (see the SWAP ORACLE header) is precisely when a fresh
	// binary starts being served. REUSED BY T-5f01: the `update` warden-command
	// dispatch calls the SAME Kick() to force an owner-demanded self-update. nil (unwired
	// / most tests) disables the seam — a nil channel is simply never selected, so the
	// 15m timer stands alone as the backstop, exactly the pre-方案A behaviour.
	kick chan struct{}
	// execSelf replaces this process IN PLACE with the freshly-swapped binary
	// (syscall.Exec over selfPath with our own argv/env in production). It returns
	// ONLY on failure — a successful exec never comes back. nil (tests / unwired)
	// skips straight to the exit fallback.
	execSelf func() error
	exit     func(int) // process-exit seam (os.Exit) — the exec-failure FALLBACK
	logf     func(string, ...any)

	// signatureOf reports the code-signing identity of the binary at a path,
	// logged after a successful swap so the operator can see whether the fleet
	// runs release-signed or adhoc builds (see the SIGNATURE OBSERVABILITY
	// header). Purely informational — its answer never gates a swap. nil
	// (tests / unwired) skips the log line; production wires codesignIdentity.
	signatureOf func(path string) string

	// post + agentID are the OBSERVABILITY seam: after a successful ocwarden swap the
	// loop announces it on the existing telemetry endpoint (best-effort). post reuses
	// main.go's Poster; agentID keys the report to this warden. now is the clock seam
	// so the announce timestamp is deterministic under test. All three are optional —
	// a nil post (or empty agentID) simply skips the announce; it never blocks a swap.
	post    Poster
	agentID string
	now     func() time.Time

	// lastSwap holds the ocwarden self-update event captured during the most recent
	// swapping cycle, consumed by run() to announce it just before exit.
	lastSwap *selfUpdateEvent

	// lastSHA is the server git_sha we last reconciled against (in-memory only: a
	// launchd relaunch re-derives it with one extra cheap cycle). It is the download
	// GATE, never the swap oracle — recorded ONLY after a fully successful cycle so a
	// mid-cycle failure always retries instead of silently skipping.
	lastSHA string
}

// run is the ctx-aware self-update loop: it waits interval (or a backoff after a
// failed cycle), reconciles, and — if it replaced ITSELF — execs the new binary in
// place (falling back to exit(0) only if the exec fails). A cancelled ctx
// (SIGINT/SIGTERM) ends it cleanly.
// It waits BEFORE the first check so a freshly-installed warden (binaries already
// current) does not stampede the server the instant it boots.
func (u *updater) run(ctx context.Context) {
	backoff := u.backoffStart
	wait := u.interval
	for {
		if !u.waitNext(ctx, wait) {
			return // ctx cancelled during the wait → clean exit
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
		// OBSERVABILITY: a self-update is otherwise silent. If this cycle swapped a
		// binary, announce it (best-effort) so the server can log "warden self-updated".
		// Done for the ocwarden case BEFORE the in-place exec — the exec'd process would
		// have no memory of the swap, so this POST is the reliable announce point — and
		// for an ocagent-only swap (no exec follows) on the same cycle. announceSelfUpdate
		// never blocks or fails the swap: a nil poster / unreachable server is swallowed.
		if u.lastSwap != nil {
			u.announceSelfUpdate()
			u.lastSwap = nil
		}
		if wardenSwapped {
			// EXEC-IN-PLACE (see the file header): replace this process image with the
			// new binary — same PID, so the launchd job never stops (launchd KeepAlive
			// demonstrably does NOT relaunch an exited warden). execSelf returns only on
			// failure; then, and only then, fall back to the old exit(0) — no worse than
			// the pre-fix behaviour, and the failure is on record in the log.
			u.logf("[ocwarden] self-update: ocwarden replaced — exec'ing the new binary in place (same PID)")
			if u.execSelf != nil {
				err := u.execSelf()
				u.logf("[ocwarden] self-update: in-place exec failed (%v) — falling back to exit(0)", err)
			}
			u.exit(0)
			return
		}
	}
}

// waitNext blocks until the poll interval elapses, a kick arrives, or ctx is
// cancelled. It returns true to run a reconcile cycle (timer elapsed OR kicked)
// and false ONLY when ctx was cancelled (→ run() exits cleanly). This is the
// event-driven wait of 方案A: the 15m timer is the backstop, a kick (SSE reconnect
// today; T-5f01's `update` rpc tomorrow) is the fast path. The delay still flows
// through the injectable sleep seam, run under a CHILD ctx so a kick cancels the
// in-flight sleep cleanly (the goroutine returns into the buffered `done` channel —
// no leak, no busy-loop). A nil kick channel is never selected, so an unwired
// updater behaves exactly as the old `u.sleep(ctx, wait)` did.
func (u *updater) waitNext(ctx context.Context, wait time.Duration) bool {
	sleepCtx, cancelSleep := context.WithCancel(ctx)
	defer cancelSleep()
	done := make(chan bool, 1)
	go func() { done <- u.sleep(sleepCtx, wait) }()
	select {
	case <-ctx.Done():
		return false // parent cancelled → stop the loop
	case alive := <-done:
		return alive // timer elapsed (true) or ctx cancelled mid-sleep (false)
	case <-u.kick:
		return true // kicked → run a cycle NOW (deferred cancelSleep unblocks the goroutine)
	}
}

// Kick requests an out-of-band reconcile on the NEXT wait, waking run() so
// checkOnce runs within seconds instead of waiting out the poll interval. It is
// the public face of the event-driven seam (see the `kick` field): the SSE
// transport calls it on every successful (re)connect (方案A), and — REUSED BY
// T-5f01 — the `update` warden-command dispatch calls it to force an
// owner-demanded self-update. Non-blocking and COALESCED (buffered cap 1): a
// reconnect storm collapses to a single pending wake rather than stacking N
// cycles, and each woken cycle is sha-gated cheap anyway (checkOnce's /api/version
// gate). Nil-safe: an updater with no kick channel silently ignores the call.
func (u *updater) Kick() {
	if u.kick == nil {
		return
	}
	select {
	case u.kick <- struct{}{}:
	default: // a wake is already pending → coalesce (de-bounce, never stack)
	}
}

// checkOnce runs ONE reconcile cycle. Returns (wardenSwapped, error) — note only an
// ocwarden swap flips the bool (ocagent swaps never trigger the self-exit).
func (u *updater) checkOnce() (bool, error) {
	// 1. Cheap gate: skip everything if the server has not moved since we last
	//    reconciled (avoids re-downloading multi-MB binaries every tick).
	sha, err := u.serverSHA()
	if err != nil {
		return false, err
	}
	if sha != "" && sha == u.lastSHA {
		return false, nil
	}

	// 2. Reconcile ocagent FIRST (a non-self swap that never restarts us), so that if
	//    the ocwarden swap below triggers our exit, the fresh ocagent is already in
	//    place for the next spawn.
	if u.agentPath != "" {
		if _, err := u.reconcileBinary(agentBinaryPath, u.agentPath, "ocagent"); err != nil {
			return false, err // do NOT record sha → retry next cycle
		}
	}

	// 3. Reconcile ocwarden (ourselves).
	wardenSwapped := false
	if u.selfPath != "" {
		wardenSwapped, err = u.reconcileBinary(wardenBinaryPath, u.selfPath, "ocwarden")
		if err != nil {
			return false, err
		}
	}

	// 4. Only after BOTH reconciled cleanly do we remember this server sha.
	u.lastSHA = sha
	return wardenSwapped, nil
}

// serverSHA reads git_sha from /api/version — the cheap download gate.
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

// reconcileBinary downloads the server's hosted binary for `path`, compares its
// CONTENT to the live file at `livePath`, and — only if they differ — verifies the
// download runs, backs the live one up to `<path>.prev`, then atomically swaps it
// in. Returns (swapped, error). Any download / verify / write failure NEVER swaps:
// the live binary is untouched and the error propagates so the caller retries.
func (u *updater) reconcileBinary(path, livePath, name string) (bool, error) {
	status, body, err := u.get(path)
	if err != nil {
		return false, fmt.Errorf("download %s: %w", name, err)
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("download %s: status %d", name, status)
	}
	if len(body) == 0 {
		return false, fmt.Errorf("download %s: empty body", name)
	}

	// Content hash is the swap ORACLE: identical bytes ⇒ the live binary already IS
	// the server's, so do nothing (this is what makes the loop idempotent and
	// drift-proof — see the file header).
	var live []byte
	if data, rerr := u.ops.readFile(livePath); rerr == nil {
		live = data
		if bytes.Equal(live, body) {
			return false, nil
		}
	}

	dir := filepath.Dir(livePath)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.selfupdate.%d", name, os.Getpid()))
	// Best-effort temp cleanup on any early return (verify fail, backup fail, …); the
	// successful-swap path clears this so the renamed temp is not deleted.
	cleanup := true
	defer func() {
		if cleanup {
			_ = u.ops.remove(tmp)
		}
	}()

	if err := u.ops.writeFile(tmp, body, 0o755); err != nil {
		return false, fmt.Errorf("write temp %s: %w", name, err)
	}
	// WriteFile's mode is umask-masked; re-assert 0755 so the probe (and the swapped
	// binary) is executable regardless of the caller's umask.
	if err := u.ops.chmod(tmp, 0o755); err != nil {
		return false, fmt.Errorf("chmod temp %s: %w", name, err)
	}

	// VERIFY-BEFORE-SWAP (anti-suicide): a corrupt/truncated/wrong-arch download must
	// fail to exec here and we bail with the live binary UNTOUCHED.
	if err := u.ops.probe(tmp); err != nil {
		return false, fmt.Errorf("verify %s failed — keeping current binary: %w", name, err)
	}

	// RETREAT PATH: back the live binary up to <path>.prev before the swap so a bad
	// new binary can be rolled back. (`.prev`, not `.bak`, to stay clear of the CI
	// hygiene `.bak$` denylist — these are runtime files, never committed.)
	if live != nil {
		prev := livePath + ".prev"
		if err := u.ops.writeFile(prev, live, 0o755); err != nil {
			return false, fmt.Errorf("backup current %s -> %s: %w", name, prev, err)
		}
	}

	// ATOMIC SWAP: same-dir rename is atomic on the local fs.
	if err := u.ops.rename(tmp, livePath); err != nil {
		return false, fmt.Errorf("atomic swap %s -> %s: %w", name, livePath, err)
	}
	cleanup = false
	u.logf("[ocwarden] self-update: replaced %s at %s (backup: %s.prev)", name, livePath, livePath)
	// Capture the observability event at the point the old/new bytes are in scope. run()
	// announces the ocwarden event just before it exits; ocagent swaps are recorded the
	// same way and announced on the same cycle (no self-exit follows an ocagent-only swap).
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
	// SIGNATURE OBSERVABILITY (see the file header): name who signed the binary
	// we just swapped in. Informational only — never gates the swap.
	if u.signatureOf != nil {
		if sig := u.signatureOf(livePath); sig != "" {
			u.logf("[ocwarden] self-update: %s signing identity: %s", name, sig)
		}
	}
	return true, nil
}

// codesignIdentity shells out to /usr/bin/codesign -dv and distills the signing
// identity of the binary at path: "adhoc", the leaf Authority CN (e.g.
// "OffiCraft Code Signing" on release-signed artifacts), "unsigned", or ""
// when codesign itself is unavailable/unreadable (non-darwin dev hosts — the
// caller then skips the log line). SOFT by design (T-33d5): a self-built
// certificate does not VERIFY on machines that never trusted it, so the
// identity is logged, never enforced.
func codesignIdentity(path string) string {
	// codesign prints the -dv details on stderr, so capture combined output.
	out, err := exec.Command("/usr/bin/codesign", "-dv", "--verbose=2", path).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "not signed") {
			return "unsigned"
		}
		return ""
	}
	return parseCodesignIdentity(string(out))
}

// parseCodesignIdentity extracts the identity from `codesign -dv --verbose=2`
// output: "Signature=adhoc" ⇒ "adhoc"; else the FIRST "Authority=" line (the
// leaf certificate's CN); else "".
func parseCodesignIdentity(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "Signature=adhoc" {
			return "adhoc"
		}
		if v, ok := strings.CutPrefix(line, "Authority="); ok {
			return v
		}
	}
	return ""
}

// clock returns the injected clock or time.Now when unset, so the announce timestamp
// is deterministic under test without every construction site wiring a clock.
func (u *updater) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

// announceSelfUpdate best-effort POSTs the captured self-update event onto the existing
// telemetry endpoint. It is deliberately toothless: a nil poster, empty agent id, absent
// event, or ANY transport/HTTP error is logged and swallowed — the swap has already
// succeeded and MUST NOT be held hostage to the server being reachable.
func (u *updater) announceSelfUpdate() {
	ev := u.lastSwap
	if ev == nil || u.post == nil || strings.TrimSpace(u.agentID) == "" {
		return
	}
	payload := map[string]any{
		"agent_id": u.agentID,
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

// nextSelfUpdateBackoff doubles cur, capped at cap.
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

// resolveSelfExe resolves the running executable's real path (symlinks followed).
// Empty if the OS cannot name our own binary; if only symlink resolution fails it
// falls back to the raw executable path.
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

// selfUpdateAgentPath is the ocagent path the SELF-UPDATE loop keeps current. Unlike
// resolveOcAgentBin (the spawn shim's resolver, which falls back to the in-tree
// <repoRoot>/cli/ocagent/ocagent source dir when no sibling exists), this
// UNCONDITIONALLY targets the home sibling <dir(exe)>/ocagent with NO exists-check:
// on a remote / manual install (no OC_AGENT_BIN, sibling not copied) the source dir
// does not exist on the host, so an exists-fallback would point self-update at a
// dead path and every tick would fail to write its temp. Pointing at the sibling
// unconditionally lets the FIRST tick download + populate it there. Empty if the OS
// cannot name our own binary — checkOnce skips an empty agentPath.
func selfUpdateAgentPath(executable func() (string, error)) string {
	exe := resolveSelfExe(executable)
	if exe == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "ocagent")
}

// newSelfUpdater wires the real self-update engine: it resolves the LIVE binary
// paths it will replace (ocwarden = our own executable; ocagent = the home sibling
// via selfUpdateAgentPath — unconditional, unlike the spawn path's resolveOcAgentBin),
// builds the Bearer-authed GET seam and the real filesystem/exec ops, and returns a
// ready updater. Mirrors newCommandTransport's construction shape.
func newSelfUpdater(cfg Config, logf func(string, ...any)) *updater {
	selfPath := resolveSelfExe(os.Executable)
	agentPath := selfUpdateAgentPath(os.Executable)

	client := &http.Client{Timeout: selfUpdateHTTPTimeout}
	// Separate, short-timeout client for the best-effort announce so a slow announce
	// can never eat into the generous multi-MB download budget on the swap→exit path.
	reportClient := &http.Client{Timeout: selfUpdateReportBudget}
	return &updater{
		get:          httpGetter(client, cfg.Base, cfg.Token),
		ops:          osUpdaterOps{runner: execRunner{timeout: selfUpdateProbeBudget}},
		selfPath:     selfPath,
		agentPath:    agentPath,
		interval:     selfUpdateInterval,
		backoffStart: selfUpdateBackoffStart,
		backoffCap:   selfUpdateBackoffCap,
		sleep:        sleepUntil,
		// Buffered cap 1: coalesces a reconnect storm into a single pending wake
		// (see Kick). The transport's onConnect is wired to Kick in main.go.
		kick: make(chan struct{}, 1),
		// The production exec-in-place: replace this process with the just-swapped
		// binary at the SAME path, argv, and env — a faithful restart with zero
		// launchd involvement. Unix-only (execve) is fine: the warden is launchd/
		// tmux-bound already, there is no non-unix build of this module. stdout/
		// stderr writes are unbuffered fd writes, but Sync best-effort anyway so
		// nothing the kernel has pending is lost across the image replacement.
		execSelf: func() error {
			_ = os.Stdout.Sync()
			_ = os.Stderr.Sync()
			return syscall.Exec(selfPath, os.Args, os.Environ())
		},
		exit:        os.Exit,
		logf:        logf,
		signatureOf: codesignIdentity,
		post:        httpPoster(reportClient, cfg.Base, cfg.Token),
		agentID:     cfg.ID,
		now:         time.Now,
	}
}
