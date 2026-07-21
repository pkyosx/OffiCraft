// Phase 4a-②b "ears (network half)": the SSE COMMAND TRANSPORT of the stateless
// warden — the outbound long-lived GET /api/events connection that carries the
// server's DIRECTED command frames down to the already-built command dispatch core
// (Phase 4a-②a, command.go). This file is the NETWORK half of the "SSE nudge
// reader"; command.go is the pure-logic half. Together: transport reads one SSE
// `data:` payload → parseCommandFrame → (if non-nil) dispatchCommand(deps).
//
// WHY OUTBOUND SSE (the NAT transport seam, warden half): the server is in the
// cloud, the warden is on the owner's Mac behind NAT, so the server CANNOT dial
// INTO the warden. The four→three RPC commands (start / robust-stop) instead ride
// the warden's OWN outbound SSE long-connection: the warden GETs /api/events with
// its warden-member agent token, the server writes a command frame onto THAT
// connection's downstream (keyed by the authenticated token `sub`), the warden
// executes, and the actual result returns ASYNC via presence. Correlation is
// ZERO-FIELD (no command_id) — the server reconciles desired_state against observed
// presence.
//
// ADDRESSING (be closed-loop — do NOT drift): the warden sends NO query/header
// except its Bearer auth. The server identifies the warden connection from the
// authenticated token `sub` (scope=agent, sub=warden member id, kind=warden) and
// pushes commands down that connection. Every other frame on the stream
// (context-high / ordinary deltas / `: heartbeat` comments) is ignored by
// parseCommandFrame (topic demux).
//
// GUARDRAILS (hard):
//   - A malformed frame, a dispatch error, or an adversarial payload NEVER crashes
//     the warden: handlePayload logs+skips (and recovers a panic defensively).
//   - A dropped connection (EOF / network error / server close) NEVER crashes: the
//     reconnect loop re-dials with exponential backoff (capped, no busy loop) — this
//     is the warden's "always online" liveness line.
//   - main.go starts the reader whenever a real token/id is present (not --once);
//     server-orchestrated STOP is the single, unconditional path. In tests the
//     transport is exercised against an httptest mock SSE server.
//   - Pure stdlib (net/http + bufio). No third-party.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// eventsPath is the officraft SSE downlink (GET /api/events).
	eventsPath = "/api/events"
	// sseBackoffStart / sseBackoffCap bound the reconnect exponential backoff. The
	// cap avoids a busy reconnect loop against a hard-down server; the start keeps a
	// healthy-connection reconnect near-immediate.
	sseBackoffStart = 1 * time.Second
	sseBackoffCap   = 60 * time.Second
	// sseDialTimeout / sseHeaderTimeout bound connection SETUP only — never the
	// long-lived body stream (a bounded body deadline would guillotine the
	// always-open SSE connection every N seconds).
	sseDialTimeout   = 10 * time.Second
	sseHeaderTimeout = 30 * time.Second
	// sseIdleReadTimeout is the idle-read watchdog threshold. Timeout:0 gives the
	// body stream NO deadline, so a silently-dead / half-open TCP connection (the
	// server-side socket died but no FIN/RST reached us) would wedge the reader in
	// a forever-blocking Read — "deaf but not reconnecting", the persona's cardinal
	// sin. officraft emits a `: heartbeat` keepalive every ~15s; if NOTHING (not
	// even a heartbeat) arrives within this window the connection is presumed dead
	// and force-dropped into the EXISTING reconnect/backoff path. ~3× the heartbeat
	// interval leaves ample slack for a healthy link while still catching a truly
	// deaf connection inside a minute. Tests inject a small value to fire fast.
	sseIdleReadTimeout = 45 * time.Second
	// maxSSELine caps a single SSE line so an adversarial unbounded line ends the
	// stream (→ reconnect) rather than growing memory without limit.
	maxSSELine = 8 << 20 // 8 MiB
)

// ---------------------------------------------------------------------------
// SSE line framing — a `data:` payload extractor over the wire byte stream.
// ---------------------------------------------------------------------------

// scanSSE reads Server-Sent-Events from r and calls onPayload once per completed
// event with the event's concatenated `data` payload (the raw bytes command.go
// consumes). It implements the parts of the SSE line protocol officraft emits:
//
//   - lines are `\n`-separated (CRLF tolerated: a trailing `\r` is trimmed);
//   - a BLANK line is the event boundary → the accumulated data is dispatched;
//   - a line beginning with `:` is a comment (`: connected` / `: heartbeat`
//     keepalive) → ignored;
//   - `field: value` → one leading space after the colon is stripped; multiple
//     `data:` lines within one event are joined with `\n`; `id:` / `event:` /
//     `retry:` (and any other field) are ignored — the warden only wants data;
//   - per the SSE spec an incomplete final event (EOF before the blank line) is
//     DISCARDED, not dispatched (officraft always terminates a frame with the
//     `\n\n` boundary, so a real command is never lost this way).
//
// It returns when r hits EOF or a read error (that error), never panicking.
func scanSSE(r io.Reader, onPayload func([]byte)) error {
	return scanSSEWithActivity(r, onPayload, nil)
}

// scanSSEWithActivity is scanSSE plus an idle-read watchdog hook: onActivity (when
// non-nil) fires ONCE per successfully-read line — a `data:`/`event:`/`id:` field,
// a blank event boundary, AND a `: heartbeat` keepalive comment all count. That is
// exactly the "any frame arrived → the link is alive" signal the watchdog resets
// its deadline on, so a healthy heartbeat-only stream is never falsely reconnected.
// scanSSE delegates here with a nil hook (watchdog disabled) to keep its callers
// and existing tests untouched.
func scanSSEWithActivity(r io.Reader, onPayload func([]byte), onActivity func()) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		onPayload([]byte(payload))
	}
	for sc.Scan() {
		if onActivity != nil {
			onActivity() // a line arrived → the link is alive → reset the watchdog
		}
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" { // event boundary
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") { // comment / keepalive
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			// A field name with no colon (whole line is the field, value empty).
			// None of the warden's fields are valueless → ignore.
			continue
		}
		value = strings.TrimPrefix(value, " ") // strip ONE leading space (SSE spec)
		if field == "data" {
			data = append(data, value)
		}
		// id / event / retry / anything else → ignored.
	}
	return sc.Err()
}

// ---------------------------------------------------------------------------
// the transport — long-lived GET /api/events + reconnect/backoff + dispatch.
// ---------------------------------------------------------------------------

// sseTransport holds one warden's outbound command-reader connection. All I/O and
// timing runs through injectable seams (client / sleep / logf) so tests drive the
// whole reconnect + dispatch path against an httptest server with NO real sleep.
type sseTransport struct {
	base   string       // OC_BASE (trailing slash already stripped by loadConfig)
	token  string       // warden-member agent token → Authorization: Bearer
	client *http.Client // long-lived (no body deadline); shared with nothing
	deps   CommandDeps  // the REAL spawn/stop closures (or test fakes)

	sleep        func(time.Duration) // injectable; real is time.Sleep
	backoffStart time.Duration
	backoffCap   time.Duration
	// idleReadTimeout arms the idle-read watchdog on an OPEN stream: no frame within
	// this window → the connection is presumed silently-dead and force-dropped into
	// reconnect. Zero DISABLES the watchdog (unbounded blocking read, the pre-existing
	// behaviour) — so watchdog-unaware tests that leave it at 0 are unaffected; tests
	// inject a small value to fire fast, production sets sseIdleReadTimeout.
	idleReadTimeout time.Duration
	logf            func(format string, args ...any)
	// onConnect fires ONCE each time a 200 SSE stream successfully opens (每次成功
	// (re)連線). It is the 方案A hook (T-c93d): main.go wires it to the self-updater's
	// Kick so a reconnect — which for the committed-prebuilt model coincides with a
	// server redeploy serving a fresh binary — triggers an immediate self-update
	// check instead of waiting out the 15m poll. nil (default / tests) = no-op, so
	// the transport is unchanged when the hook is not wired.
	onConnect func()
}

// newSSEClient builds the long-lived HTTP client for the SSE downlink. Crucially
// Timeout is 0 (NO overall deadline) — an SSE connection is meant to stay open
// indefinitely; only connection SETUP (dial / TLS / response headers) is bounded,
// never the streaming body.
func newSSEClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: sseDialTimeout}).DialContext,
			TLSHandshakeTimeout:   sseDialTimeout,
			ResponseHeaderTimeout: sseHeaderTimeout,
		},
	}
}

// run is the always-online reconnect loop. It blocks until ctx is cancelled,
// re-dialing whenever the stream drops. Backoff is exponential and capped; a
// connection that OPENED successfully (HTTP 200) resets the backoff so a normal
// long-connection drop reconnects fast, while a hard-down server (never opens)
// escalates toward the cap instead of hot-looping.
func (t *sseTransport) run(ctx context.Context) {
	backoff := t.backoffStart
	for {
		if ctx.Err() != nil {
			return
		}
		opened, err := t.connectOnce(ctx)
		if ctx.Err() != nil { // cancelled while connected/dialing → clean exit
			return
		}
		if opened {
			backoff = t.backoffStart // healthy connection dropped → reconnect fast
			t.logf("[ocwarden] command reader: stream ended (%v); reconnecting in %s", err, backoff)
		} else {
			t.logf("[ocwarden] command reader: connect failed (%v); retrying in %s", err, backoff)
		}
		if !sleepCtx(ctx, t.sleep, backoff) {
			return
		}
		backoff = nextSSEBackoff(backoff, t.backoffCap)
	}
}

// connectOnce dials GET /api/events, and — on a 200 — streams its body through
// scanSSE until the stream ends. It returns (opened, err): opened is true once the
// 200 body is being read (so the caller resets backoff), false on a dial error or
// non-200 (so the caller keeps escalating backoff). It NEVER returns until the
// stream ends or ctx is cancelled — that is the long-lived connection.
//
// IDLE-READ WATCHDOG (SetReadDeadline): on an open 200 body it arms a per-connection
// READ DEADLINE on the underlying net.Conn that resets on every arriving line (see
// scanSSEWithActivity). If nothing arrives for idleReadTimeout the netpoller makes the
// blocked resp.Body.Read return a timeout error → scanSSE returns it → the caller
// (run) takes the EXISTING reconnect/backoff path. We use the netpoller's native
// read-deadline instead of cancelling the request context because a genuine half-open
// TCP (server-side socket gone, no FIN/RST reached us) can leave the context-cancel→
// net/http-Close path unable to unblock the blocked Read — SetReadDeadline unblocks it
// reliably regardless of TCP state. The conn is captured via httptrace.GotConn.
//
// The context-cancel path is KEPT INTACT for genuine ctx cancellation / clean shutdown
// (the child context is derived from ctx, so a real ctx cancellation still stops both
// this connection and the outer run loop). SetReadDeadline is ADDITIVE — the idle
// watchdog, not a replacement for ctx-driven shutdown. If GotConn did NOT capture a
// conn (defensive), we fall back to the previous AfterFunc(cancelConn) watchdog so the
// watchdog is never silently disabled.
func (t *sseTransport) connectOnce(ctx context.Context) (opened bool, err error) {
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	// Capture the underlying net.Conn for THIS request via httptrace so the idle
	// watchdog can arm a read deadline directly on the socket. GotConn fires during
	// client.Do (from the transport's goroutine), so guard the shared pointer.
	var (
		connMu   sync.Mutex
		httpConn net.Conn
	)
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connMu.Lock()
			httpConn = info.Conn
			connMu.Unlock()
		},
	}
	connCtx = httptrace.WithClientTrace(connCtx, trace)
	req, err := http.NewRequestWithContext(connCtx, http.MethodGet, t.base+eventsPath, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	// The warden sends NO query/header except its Bearer auth (be addressing
	// contract): the server identifies the warden from the authenticated token sub.
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return false, err // dial / TLS / header timeout / ctx cancel
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	// Positive connect beacon (Phase 4 flip observability): we now hold an OPEN 200
	// SSE stream. Logged once per (re)connect so the warden log shows exactly WHEN
	// the command downlink is live — lets us correlate a connect against the
	// server's START-emit time (a START enqueued while we were mid-reconnect could
	// otherwise be missed with no trace on either side).
	t.logf("[ocwarden] command reader: connected — streaming %s%s", t.base, eventsPath)
	// 方案A (T-c93d): a fresh SSE (re)connect is the event that most reliably marks
	// "the server process may have moved" (a redeploy drops every stream). Fire the
	// hook so the self-updater checks /api/version NOW rather than up to 15m later.
	// nil = unwired (tests / --once); the sha-gate makes a spurious kick near-free.
	if t.onConnect != nil {
		t.onConnect()
	}
	// Arm the idle-read watchdog only when configured (idleReadTimeout>0). Each line
	// read pushes the deadline forward; a lapse makes the blocked Read return → scanSSE
	// returns an error → run() reconnects. Zero leaves the pre-existing unbounded-read
	// behaviour intact (and the reset hook nil).
	var onActivity func() // nil → watchdog disabled (unbounded read, unchanged)
	if t.idleReadTimeout > 0 {
		connMu.Lock()
		conn := httpConn
		connMu.Unlock()
		if conn != nil {
			// Preferred path: arm the netpoller's read deadline directly on the socket.
			// This reliably unblocks a blocked Read even on a genuine half-open TCP,
			// unlike the context-cancel→Close path. Each arriving line pushes it forward.
			_ = conn.SetReadDeadline(time.Now().Add(t.idleReadTimeout))
			onActivity = func() { _ = conn.SetReadDeadline(time.Now().Add(t.idleReadTimeout)) }
			// Clear the deadline on exit so a pooled/keep-alive conn is not left with a
			// stale deadline (defensive; SSE conns are not reused, but be tidy).
			defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
		} else {
			// Defensive fallback: GotConn never captured a conn → keep the previous
			// AfterFunc(cancelConn) behaviour so the watchdog is never silently disabled.
			watchdog := time.AfterFunc(t.idleReadTimeout, cancelConn)
			defer watchdog.Stop()
			onActivity = func() { watchdog.Reset(t.idleReadTimeout) }
		}
	}
	return true, scanSSEWithActivity(resp.Body, t.handlePayload, onActivity)
}

// handlePayload is the bridge from ONE SSE data payload to the command dispatch
// core. It is the sole place the two halves meet: parse → (skip | log | dispatch).
// It is bulletproofed against a bad frame taking down the reader loop — a parse
// error or dispatch error is logged and skipped, and a defensive recover catches
// any panic from the injected side-effecting deps so a single frame can never
// crash the warden.
func (t *sseTransport) handlePayload(payload []byte) {
	defer func() {
		if r := recover(); r != nil {
			t.logf("[ocwarden] command reader: recovered from panic handling frame: %v", r)
		}
	}()
	cmd, err := parseCommandFrame(payload)
	if err != nil {
		t.logf("[ocwarden] command reader: skip malformed frame: %v", err)
		return
	}
	if cmd == nil {
		return // valid envelope, non-warden-command topic → skip (the common case)
	}
	// Observability (Phase 4 flip): a warden-command frame ACTUALLY reached us. Log
	// the RECEIPT before dispatch so "the push transport delivered a START/STOP" is
	// provable from the warden log ALONE — the decisive end-to-end evidence that
	// server enqueue → SSE downlink → warden drain is wired. Non-command frames
	// (heartbeat / member deltas) stay silent, so this line is signal not noise.
	t.logf("[ocwarden] command reader: received %s frame (%s)", cmd.RPC, commandTargetLabel(cmd))
	if err := dispatchCommand(cmd, t.deps); err != nil {
		t.logf("[ocwarden] command reader: dispatch refused: %v", err)
		return
	}
	t.logf("[ocwarden] command reader: dispatched %s OK (%s)", cmd.RPC, commandTargetLabel(cmd))
}

// commandTargetLabel renders the addressed identity for the receipt/dispatch
// log lines: member_id for the member verbs, worker_id for the worker verbs
// (a worker frame carries no member_id — logging "<nil>" there was noise).
func commandTargetLabel(cmd *Command) string {
	if id, ok := argString(cmd.Args, "member_id"); ok && id != "" {
		return "member_id=" + id
	}
	if id, ok := argString(cmd.Args, "worker_id"); ok && id != "" {
		return "worker_id=" + id
	}
	return "target=?"
}

// nextSSEBackoff doubles cur, clamped to capd. A degenerate (<=0) seed can't wedge
// the loop hot-spinning at 0 → it jumps straight to the cap (the safest back-off).
// The run loop always seeds with a positive backoffStart and only grows via this,
// so the <=0 branch is purely defensive.
func nextSSEBackoff(cur, capd time.Duration) time.Duration {
	if cur <= 0 {
		return capd
	}
	next := cur * 2
	if next > capd {
		return capd
	}
	return next
}

// sleepCtx sleeps d via the injectable seam, but treats a cancelled ctx as an
// immediate stop signal (checked before AND after the sleep). Returns false when
// ctx is cancelled → the run loop exits. Tests inject an instant fake sleep and
// drive termination through ctx, so no real wall-clock backoff is ever waited.
func sleepCtx(ctx context.Context, sleep func(time.Duration), d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	sleep(d)
	return ctx.Err() == nil
}

// ---------------------------------------------------------------------------
// production wiring — bind the REAL CommandDeps + build the transport.
// ---------------------------------------------------------------------------

// resolveClaudeBin finds the claude CLI ROBUSTLY under a launchd daemon's minimal
// PATH (which typically LACKS ~/.local/bin, where claude installs). A bare
// exec.LookPath("claude") returns "" there → start()'s "claude must be resolvable"
// guard refuses every spawn with no visible reason (the Phase-4 boot-death cause).
// Resolution order:
//  1. OC_CLAUDE_BIN env override — the fleet-portable explicit path. `ocwarden
//     install` STAMPS this into the launchd plist (resolveClaudeForInstall,
//     install.go), because ② and ③ both miss under launchd for a version-manager
//     (asdf/nvm/volta) claude — this slot is the deterministic production path.
//     A foreground `ocwarden run` with OC_CLAUDE_BIN exported wins here too.
//  2. exec.LookPath("claude") — honours an enriched PATH when one is present.
//  3. common install locations (~/.local/bin, homebrew, /usr/local/bin).
//
// Returns "" ONLY when claude is truly absent everywhere — start() then refuses with
// a clear Reason instead of a silent no-op.
func resolveClaudeBin(env func(string) string) string {
	if p := strings.TrimSpace(env("OC_CLAUDE_BIN")); p != "" && isExecutableFile(p) {
		return p
	}
	if p, err := exec.LookPath("claude"); err == nil && p != "" {
		return p
	}
	home := env("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	for _, cand := range []string{
		filepath.Join(home, ".local", "bin", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if isExecutableFile(cand) {
			return cand
		}
	}
	return ""
}

// isExecutableFile reports whether p is an existing, non-directory file with at least
// one executable bit — a cheap "could we exec this" probe for resolveClaudeBin.
func isExecutableFile(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// buildCommandDeps binds the two dispatch side-effects to their real Phase 2/3
// mechanisms, exactly as command.go's CommandDeps doc prescribes:
//
//	Spawn → SpawnDeps.start (Phase 2): the full workdir + .mcp.json + persona +
//	        tmux-boot spawn.
//	Stop  → a closure over stop(runner, socket, session, realKill, realGetpgid)
//	        (Phase 3): the robust SIGHUP→escalate→re-assert stop ladder, targeting the
//	        command's own member session.
//
// The warden reports NO presence: the server projects each host's presence from its
// own SSE-connection view, so a spawn/kill fires no warden-side presence reconcile.
// resolveRepoRoot derives the officraft checkout root from the running
// executable: ocwarden is installed at <repoRoot>/cli/ocwarden/ocwarden, so
// three filepath.Dir hops up land on <repoRoot>. (The python origin sat one
// directory shallower and walked two parents; grouping this binary under cli/
// adds one level, so it is three hops here.)
// The os.Executable seam is injected so a test pins a deterministic path; an
// unresolvable executable yields "" (the shim would then exec a bad path, but
// that degenerate case only arises if the OS can't name our own binary).
func resolveRepoRoot(executable func() (string, error)) string {
	exe, err := executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(exe)))
}

// resolveOcAgentBin picks the ocagent binary the spawn shim execs, with NO external
// injection needed — a self-contained package finds its own sibling. Preference:
//  1. SIBLING of the running ocwarden — <dir(exe)>/ocagent. A home-installed warden
//     ($HOME/.officraft/warden/ocwarden) carries ocagent in the SAME warden/ (install's
//     installOcAgent puts it there — downloaded from the server by default, or copied from
//     an OC_AGENT_BIN local override), so the sibling exists and is authoritative. This is
//     why the warden needs no OC_AGENT_BIN env / no plist stamp: the two binaries ship
//     as one package and the warden looks right next to itself.
//  2. FALLBACK <repoRoot>/cli/ocagent/ocagent — the in-tree dev layout, where ocwarden
//     and ocagent live in separate cli/ subdirs (no sibling), reached via the repoRoot
//     three-parent walk.
//
// exists is injected (os.Stat in production) so a test pins the branch deterministically.
func resolveOcAgentBin(executable func() (string, error), exists func(string) bool, repoRoot string) string {
	if exe, err := executable(); err == nil {
		if sibling := filepath.Join(filepath.Dir(exe), "ocagent"); exists(sibling) {
			return sibling
		}
	}
	return filepath.Join(repoRoot, "cli", "ocagent", "ocagent")
}

func buildCommandDeps(cfg Config, env func(string) string, runner CmdRunner) CommandDeps {
	// Real spawn mechanism, fully wired (only reached with the gate ON — a PoC
	// default-OFF build never constructs a live connection, so start() never runs).
	// Resolve claude ROBUSTLY: a launchd daemon inherits a MINIMAL PATH (no
	// ~/.local/bin), so a bare exec.LookPath("claude") returns "" and start()
	// silently refuses every spawn (the Phase-4-flip boot-death root cause). See
	// resolveClaudeBin.
	claudeBin := resolveClaudeBin(env)
	// The instance namespace keys the tmux socket + agent home (validated at
	// process entry — realMain refuses an invalid OC_NAMESPACE before any
	// transport is built, so the error case here is unreachable).
	ns, _ := namespaceFromEnv(env)
	socket := tmuxSocketFor(ns)
	spawnDeps := SpawnDeps{
		Runner:    runner,
		Base:      cfg.Base,
		Socket:    socket,
		Home:      defaultAgentHome(env),
		Namespace: ns,
		// T-426d: the owner's agent env file (<officraft root>/env, 0600). Absent
		// is the normal state and is NOT an error — the spawn proceeds without it.
		EnvFile: defaultAgentEnvFile(env),
		// T-426d follow-up: the BASE env layer — the owner's interactive shell,
		// captured per spawn (~0.12s measured). EnvFile above layers on top as the
		// override. nil (OC_AGENT_ENV_INHERIT=0) restores the previous behaviour.
		// A capture failure is never fatal: start() falls back to the minimal
		// environment and warns on stderr.
		CaptureEnv: defaultCaptureEnv(env),
		// Diagnostics land on warden stderr → <logDir>/ocwarden.err.log per the
		// plist's StandardErrorPath. Key names and reasons only, never values.
		Logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "[ocwarden spawn] "+format+"\n", a...)
		},
		ClaudeBin: claudeBin,
		// RepoRoot is the in-tree dev fallback base for the ocagent shim. OcAgentBin is
		// the resolved exec target: a home-installed warden finds ocagent as its OWN
		// SIBLING ($HOME/.officraft/warden/ocagent) with no env/plist injection; a dev
		// run falls back to <repoRoot>/cli/ocagent/ocagent. resolveOcAgentBin owns both.
		RepoRoot:   resolveRepoRoot(os.Executable),
		OcAgentBin: resolveOcAgentBin(os.Executable, func(p string) bool { _, err := os.Stat(p); return err == nil }, resolveRepoRoot(os.Executable)),
		WriteFile:  osWriteFile,
		MkdirAll:   os.MkdirAll,
		// Symlink / Remove publish the workdir `ocagent` as a symlink to OcAgentBin.
		Symlink: os.Symlink,
		Remove:  os.Remove,
		// Real wall-clock pacing for the boot-nudge settle/retry — a cold claude REPL
		// needs real time to become input-ready before the nudge Enter commits.
		Sleep: time.Sleep,
		// Pretrust is bound PER-SPAWN below (it needs the per-member launch workdir,
		// which only exists once StartParams arrives); the struct field stays nil so
		// the seam's nil-skip contract is unchanged.
		Pretrust: nil,
	}
	// The real ~/.claude.json pre-trust target (OC_CLAUDE_JSON can redirect it).
	claudeJSONPath := defaultClaudeJSONPath(env)
	return CommandDeps{
		// Rebuild the Pretrust seam per spawn so it targets THIS member's actual
		// launch workdir (same durable dir start() computes) — the Phase-4 real
		// ~/.claude.json write, mirroring pretrust_launch_cwd(self.workdir). A failing
		// pretrust aborts the spawn inside start() (don't spawn a nudge-eaten zombie).
		Spawn: func(p StartParams) SpawnOutcome {
			sd := spawnDeps
			workdir := agentWorkdir(sd.Home, p.MemberID)
			sd.Pretrust = func() error { return pretrustWorkdir(claudeJSONPath, workdir) }
			return sd.start(p)
		},
		Stop: func(session string) (bool, bool) {
			// The sweep seams complete the ladder's ⓪/⑤ legs: lsof-discover any
			// ocagent still anchored to the session's workdir (the detached
			// `ocagent listen` SIGHUP never reaches) and reap it exact-pid, pacing
			// the TERM→KILL grace with real wall-clock. The ONE closure serves
			// both namespaces (P5b): member-<id> resolves the agents/ workdir;
			// a LEGACY worker-<ow-id> residual resolves the retired workers/
			// sibling root, so its detached listener is still reaped.
			workdir := memberWorkdirForSession(defaultAgentHome(env), session)
			if workdir == "" {
				workdir = workerWorkdirForSession(defaultWorkerHome(env), session)
			}
			return stop(runner, socket, session, realKill, realGetpgid, sweepSeams{
				listenPIDs: func(wd string) []int { return ocagentPIDsByCwd(runner, wd) },
				workdir:    workdir,
				sleep:      time.Sleep,
			})
		},
		// Teardown removes THIS warden's own install (bootout + delete tokfile/plist),
		// bound over the SAME resolved paths + sysOps the `ocwarden teardown` CLI uses,
		// and honouring WARDEN_INSTALL_DRYRUN. It returns (ok, log) WITHOUT exiting — the
		// uninstall dispatch case reports + self-exits. A path-resolution failure (HOME
		// unset) yields (false, <reason>) so uninstall reports the fault and stays alive.
		Teardown: func() (bool, string) {
			p, err := resolveTeardownPaths(env, os.Getuid())
			if err != nil {
				return false, fmt.Sprintf("[ocwarden teardown] cannot resolve paths: %v\n", err)
			}
			return doTeardown(realSysOps(), env(dryRunEnv) == "1", p)
		},
		// Real process-exit seam for the uninstall self-teardown. The uninstall case
		// calls this ONLY after its receipt is proven delivered.
		Exit: os.Exit,
		// SYNCHRONOUS command_result reporter (fleet remote-ops stage 1). Uses the SAME
		// telemetry ingest endpoint + a DEDICATED short-timeout poster. start/stop ignore
		// its error (best-effort); uninstall gates its self-exit on a nil (delivered) return.
		Report: newCommandReporter(cfg),
	}
}

// newCommandReporter builds the SYNCHRONOUS command_result reporter closure. It POSTs
// {command_result:{member_id, rpc, ok, reason, log, at}} to the telemetry ingest
// endpoint on its OWN short-timeout client and RETURNS the delivery verdict as an
// error (nil == the server durably accepted the receipt):
//
//   - no token/id, or a blank member_id/rpc → nil (an unaddressed receipt is noise;
//     a mis-wired warden CANNOT report, so this is a benign no-op, not a failure).
//   - any transport error (DNS/refused/timeout) → poster returns status 0 → error.
//   - any non-2xx status → error (the server did NOT record the receipt).
//   - a 2xx status → nil (delivered).
//
// WHY SYNCHRONOUS (v2 uninstall — the self-teardown timing barrier): uninstall is the
// warden dismantling ITSELF, so its receipt MUST reach the server BEFORE the warden
// os.Exit()s — a fire-and-forget POST could be dropped when the process dies mid-flight,
// leaving the server unable to reconcile the member's final state. The start/stop cases
// keep calling report best-effort and IGNORE this error (behaviour unchanged, just a
// wider signature); only the uninstall case gates its os.Exit on a nil return.
func newCommandReporter(cfg Config) func(CommandResult) error {
	post := httpPoster(&http.Client{Timeout: commandReportTimeout}, cfg.Base, cfg.Token)
	return func(cr CommandResult) error {
		// Guard the same way runOnce guards telemetry: a mis-wired warden (no token/id)
		// or an unaddressed/verb-less receipt is a guaranteed-useless POST → skip (nil).
		if cfg.Token == "" || cfg.ID == "" {
			return nil
		}
		// A receipt must address SOMEONE: a member_id (member verbs) OR a worker_id
		// (T-9ccf worker verbs). Neither → unaddressed noise, skip. worker_id rides
		// the SAME free-shape command_result object (no wire-schema change); the
		// server routes on whichever id is present.
		if (strings.TrimSpace(cr.MemberID) == "" && strings.TrimSpace(cr.WorkerID) == "") ||
			strings.TrimSpace(cr.RPC) == "" {
			return nil
		}
		payload := map[string]any{
			"agent_id": cfg.ID,
			"command_result": map[string]any{
				"member_id": cr.MemberID,
				"worker_id": cr.WorkerID,
				"rpc":       cr.RPC,
				"ok":        cr.OK,
				"reason":    cr.Reason,
				"log":       cr.Log,
				"at":        cr.At,
			},
		}
		// Synchronous: block on the POST and translate its verdict to an error. A
		// transport fault surfaces as status 0; any non-2xx is a non-delivery. The
		// start/stop callers discard this; the uninstall caller REQUIRES a nil here
		// before it self-exits.
		status, _ := post(commandResultPath, payload)
		if status < 200 || status >= 300 {
			return fmt.Errorf("command_result POST returned status %d", status)
		}
		return nil
	}
}

// newCommandTransport assembles the production transport: the long-lived SSE
// client, the real dispatch deps, real time.Sleep backoff, and a stderr/out
// logger. Constructing it does NOT connect — run(ctx) does — so this is inert
// until main.go's gate explicitly starts it.
func newCommandTransport(cfg Config, env func(string) string, runner CmdRunner,
	logf func(string, ...any)) *sseTransport {
	return &sseTransport{
		base:            cfg.Base,
		token:           cfg.Token,
		client:          newSSEClient(),
		deps:            buildCommandDeps(cfg, env, runner),
		sleep:           time.Sleep,
		backoffStart:    sseBackoffStart,
		backoffCap:      sseBackoffCap,
		idleReadTimeout: sseIdleReadTimeout,
		logf:            logf,
	}
}
