package main

// server.go — app assembly (the Go twin of the retired Python service/app.py create_app +
// serve): run the fail-closed boot assertions FIRST, then register every
// RouteSpec row onto the mux with its auth + RBAC dependencies attached, then
// bind loopback:[server].port (port from oc.toml; the host is hardwired).
//
// Two dependencies per gated row, in order (register_routes contract): the JWT
// gate (401 deny-by-default), then — when the row's Requires names a class
// ABOVE the "machine" floor — the single principal choke (403 below the
// declared minimum). Public rows attach neither.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// appVersion is the single app version string. A `var` (not const) so bin/build
// can stamp it at link time (-X main.appVersion=…), same mechanism as buildSHA;
// an unstamped build (plain `go build`, self-build, dev) keeps the honest
// "0.0.0" placeholder. An OFFICIAL package (bin/release) stamps this to the
// GitHub Release tag, so /api/version's `version` self-identifies the release
// — the single human-facing version identity (release_check.go compares it
// against the newest GitHub Release tag).
var appVersion = "0.0.0"

// ── build identity (handlers.git_sha / git_time; captured once at boot) ─────

// buildSHA / buildTime are the LINK-TIME build identity, stamped by bin/build
// (-ldflags "-X main.buildSHA=<short sha> -X main.buildTime=<%cI>") onto the
// single-file deploy artifact. When stamped they WIN over the CWD git probe:
// a repo-less standalone binary has no checkout to probe, and even inside a
// checkout the running code's identity is the binary's own build, not
// whatever HEAD the CWD happens to sit on. Empty (a plain `go build`, e.g.
// the committed prebuilt) falls back to the probe.
var (
	buildSHA  string
	buildTime string
)

// gitSHA returns the stamped build sha, else the current short (7-char) git
// sha of the CWD checkout, else "unknown". Best-effort; never fails the boot.
func gitSHA() string {
	if buildSHA != "" {
		return buildSHA
	}
	out, err := gitOutput("rev-parse", "--short", "HEAD")
	if err != nil || out == "" {
		return "unknown"
	}
	return out
}

// gitTime returns the stamped build commit time, else the committer date of
// HEAD (strict ISO-8601), or "" when unavailable — the caller serialises ""
// as null, never a fabricated time.
func gitTime() string {
	if buildTime != "" {
		return buildTime
	}
	out, err := gitOutput("show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

func gitOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// ── DTOs (field ORDER mirrors service/dto.py so the JSON bytes align) ────────
//
// These three are HAND-WRITTEN besides the generated ocapi_gen.go types on
// purpose: the generated structs marshal alphabetically-sorted optional fields
// with omitempty, while the probe contract is byte-level field ORDER parity
// with the Python DTOs (null serialised, never omitted). The generated types
// are the sub-batch B request/response vocabulary; these lock the probes.

type healthDTO struct {
	Status string `json:"status"`
}

type versionDTO struct {
	Version         string  `json:"version"`
	GitSHA          string  `json:"git_sha"`
	GitTime         *string `json:"git_time"` // null when unavailable
	CatalogHash     string  `json:"catalog_hash"`
	UpdateAvailable bool    `json:"update_available"`
	LatestVersion   *string `json:"latest_version"` // only meaningful when UpdateAvailable
}

// probeVersionDTO is the bare `/version` deploy-probe shape (autodeploy reads
// `sha` to compare) — service/dto.py ProbeVersionDTO.
type probeVersionDTO struct {
	Version     string `json:"version"`
	SHA         string `json:"sha"`
	CatalogHash string `json:"catalog_hash"`
}

// ── response writers (unified error envelope; docs/design/api-error-envelope.md)

func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// errorCodeForStatus is the status → machine-readable code map
// (service.errors.CODE_BY_STATUS + the honest fallback buckets).
func errorCodeForStatus(status int) string {
	switch status {
	case 400, 422:
		return "validation_error"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 405:
		return "method_not_allowed"
	case 409:
		return "conflict"
	case 503:
		return "service_unavailable"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "client_error"
}

// writeError answers the ONE non-2xx wire shape every Python route already
// speaks: {"error":{"code":"...","message":"..."}}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": errorCodeForStatus(status), "message": message},
	})
}

// ── auth middleware (service/auth.py require_auth: the single verify path) ──

type contextKey string

const claimsContextKey contextKey = "ocserverd.claims"

func claimsFromContext(ctx context.Context) map[string]any {
	claims, _ := ctx.Value(claimsContextKey).(map[string]any)
	return claims
}

// extractToken pulls the bearer token from the request — the byte-faithful
// twin of service/auth.py extract_token. `Authorization: Bearer <jwt>` is the
// canonical form (scheme case-insensitive; a bare scheme-less value is
// tolerated too); when NO Authorization header is present the identical token
// is also accepted as a `?token=` query param, because the SPA's SSE downlink
// (EventSource) and inline <img>/<a href> blob loads cannot set a header. A
// present-but-invalid header never falls through to the query param.
func extractToken(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		scheme, rest, found := strings.Cut(header, " ")
		if found && strings.EqualFold(scheme, "bearer") {
			if token := strings.TrimSpace(rest); token != "" {
				return token
			}
		}
		return header
	}
	return r.URL.Query().Get("token")
}

// requireAuth wraps a GATED handler with the JWT gate: the extracted token
// (header first, then the `?token=` query fallback — see extractToken)
// verified against the single HS256 secret, claims stashed on the request
// context, 401 deny-by-default on anything else.
//
// ownerIatFloor (nil = no cut) is the change-password revocation seam
// (lifecycle.md §1.3): an owner-scope token whose iat is EARLIER than the
// floor was minted before the last password change and is refused — the one
// stateful exception to stateless verification. Agent/warden tokens never
// consult it.
func requireAuth(secret []byte, ownerIatFloor func() int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(secret) == 0 {
			writeError(w, http.StatusUnauthorized, "auth not configured")
			return
		}
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing credentials")
			return
		}
		claims, err := verifyJWT(token, secret, time.Now().Unix())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if ownerIatFloor != nil {
			if scope, _ := claims["scope"].(string); scope == "owner" {
				iat, ok := claims["iat"].(float64) // encoding/json numbers land as float64
				if !ok || int64(iat) < ownerIatFloor() {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims)))
	})
}

// shareSigGate is the third auth path on the ONE ShareSig-flagged route (the
// attachment blob GET): a bearer credential of any kind (header or ?token=)
// always takes the normal authed chain — a present-but-invalid token stays a
// 401 and NEVER falls through to the sig. Only a credential-less request may
// present ?sig=; a valid sig (HMAC over exactly the path's attachment_id —
// sharesig.go) serves the RAW handler, which by construction reads only that
// one blob; a bad sig is 401; no sig at all falls to the authed chain's
// "missing credentials" 401.
func shareSigGate(secret []byte, raw, authed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extractToken(r) != "" {
			authed.ServeHTTP(w, r)
			return
		}
		sig := r.URL.Query().Get("sig")
		if sig == "" {
			authed.ServeHTTP(w, r)
			return
		}
		if len(secret) == 0 || !verifyShareSig(secret, r.PathValue("attachment_id"), sig) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
		raw.ServeHTTP(w, r)
	})
}

// ── app assembly ─────────────────────────────────────────────────────────────

// buildHandler assembles the mux from the route table: boot assertions FIRST
// (fail closed — a bad table is an error, never a served app), then each row
// registered with its auth + RBAC chokes. Mirrors create_app + register_routes.
// lookup is the roster read the principal resolver classifies agent-scoped
// callers through (nil = token-only classification, the plumbing-test face).
func buildHandler(specs []RouteSpec, secret []byte, lookup func(id string) (*Member, error), ownerIatFloor func() int64) (http.Handler, error) {
	if err := assertAllRoutesLabelled(specs); err != nil {
		return nil, err
	}
	// RBAC twin of the auth-label assertion: every row must also declare the
	// MINIMUM principal class it admits and agree with its auth label.
	if err := assertAllRoutesDeclareRequires(specs); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	for _, spec := range specs {
		var h http.Handler = spec.Handler
		if spec.Auth == authGated {
			if spec.Requires != principalMachine {
				h = requirePrincipalClass(spec.Requires, lookup, h)
			}
			h = requireAuth(secret, ownerIatFloor, h)
			if spec.ShareSig {
				h = shareSigGate(secret, spec.Handler, h)
			}
		}
		mux.Handle(spec.Method+" "+spec.Path, h)
	}
	// The SPA / error fallback holds the bare "/" pattern — every table row
	// above is a more specific mux pattern and always wins (static.py
	// precedence, structural). See spa.go for the decision ladder.
	mux.Handle("/", newFallbackHandler(specs, webdistFS()))
	return mux, nil
}

// specsFor builds the route table over one apiServer through the generated
// ServerInterfaceWrapper (param binding; a param the wrapper cannot bind is
// the wire-frozen 422 through the unified envelope) and stamps the
// derived catalog hash back onto the server (the hash is over the table's own
// non-mcp_exclude rows).
func specsFor(s *apiServer) []RouteSpec {
	wrapper := &ServerInterfaceWrapper{
		Handler: s,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		},
	}
	specs := routeSpecs(wrapper)
	s.catalogHash = catalogHashOf(specs)
	s.mcpTools = mcpToolIndex(specs)
	return specs
}

// newAPIServer assembles the handler carrier: build identity captured ONCE
// (at process start) so the probes report the sha of the RUNNING code — an
// autodeploy that pulls a new sha but fails to restart keeps reporting the
// OLD sha (handlers._PROCESS_SHA contract).
func newAPIServer(dal *DAL, hub *Hub, secret []byte, tokenTTL int64, root assetRoot) *apiServer {
	return &apiServer{
		processSHA:            gitSHA(),
		processTime:           gitTime(),
		dal:                   dal,
		hub:                   hub,
		telemetry:             newMemStore(),
		gauge:                 newMemStore(),
		machineClaims:         newMachineClaimStore(),
		secret:                secret,
		tokenTTL:              tokenTTL,
		outsourceMaxParallel:  defaultOutsourceMaxParallel,
		ctxhigh:               defaultSseContextHigh(),
		root:                  root,
		binHashes:             bindistBinaryHashesFrom(bindistFS()),
		reconcileStates:       map[string]reconcileState{},
		reconcileCfg:          defaultReconcileConfig(),
		identitySweepAt:       map[string]float64{},
		workerSpawnAt:         map[string]float64{},
		workerSpawnTarget:     map[string]string{},
		workerSpawnAttempts:   map[string]int{},
		workerReclaimed:       map[string]bool{},
		workerStopPending:     map[string]string{},
		workerMachinePref:     map[string]string{},
		workerReconcileStates: map[string]reconcileState{},
		workerMachineCooldown: map[string]float64{},
	}
}

// defaultRouteSpecs is the dependency-free table view (route-shape tests +
// the SPA fallback's template list): the probes work, the business handlers
// would need the full newAPIServer wiring.
func defaultRouteSpecs() []RouteSpec {
	return specsFor(newAPIServer(nil, NewHub(), nil, defaultTokenTTL, "."))
}

// sseKeepAlive is the TCP keep-alive config applied to every accepted
// connection (T-7e07). WHY at the socket level: a long-lived SSE downlink whose
// peer silently vanishes (machine off, NAT/LB/CDN drops the flow — no FIN/RST)
// leaves the server writing 15-byte heartbeats that just land in the kernel
// send buffer and return success, so neither r.Context() nor a write deadline
// notices for a very long time (the old ~15 min OS-retransmit wedge that pinned
// a member permanently online / 409-on-reconnect). Keep-alive probes ride on
// TCP ACKs, independent of app reads/writes: a healthy peer's kernel ACKs the
// probe (never falsely reaped, even on an idle stream), while a vanished peer
// fails to ACK and the connection is closed after ~Idle+Interval*Count ≈ 30 s →
// r.Context() cancels → the handler returns → Disconnect → the stale listener's
// online projection drops → the member can reconnect. Cross-platform: Go 1.23+
// net.KeepAliveConfig maps to TCP_KEEPALIVE/KEEPINTVL/KEEPCNT on macOS (the
// prod fleet) and the matching options on Linux. Values are deliberately not
// aggressive — a shorter window risks reaping a healthy connection over
// transient jitter.
var sseKeepAlive = net.KeepAliveConfig{
	Enable:   true,
	Idle:     15 * time.Second,
	Interval: 5 * time.Second,
	Count:    3,
}

// keepAliveConn is the minimal surface applyKeepAlive needs — *net.TCPConn
// satisfies it; a test fake captures the config without touching a real socket.
type keepAliveConn interface {
	SetKeepAliveConfig(net.KeepAliveConfig) error
}

// applyKeepAlive arms sseKeepAlive on one accepted connection. Best-effort: a
// non-TCP conn (or one predating the API) simply skips it, never fails Accept.
func applyKeepAlive(c net.Conn) {
	if kc, ok := c.(keepAliveConn); ok {
		_ = kc.SetKeepAliveConfig(sseKeepAlive)
	}
}

// keepAliveListener wraps a net.Listener so every accepted connection gets
// sseKeepAlive. http.Serve(ln, …) (unlike ListenAndServe) sets no keep-alive of
// its own, so without this wrap the accepted SSE sockets have none. Keep-alive
// is invisible to short-lived request/response connections (they close before
// any probe fires), so no other endpoint's observable behaviour changes.
type keepAliveListener struct {
	net.Listener
}

func (l keepAliveListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	applyKeepAlive(c)
	return c, nil
}

// cmdServe is the zero-argument canonical start (service.app.serve): read
// oc.toml, open + migrate + seed the store, load the DB settings snapshot
// (running the one-shot oc.toml → DB auth migration — settings.go), assemble
// the app (boot assertions fail closed), mount the reconcile producer cadence
// (unless --no-reconcile) and the outsource-assignment scheduler cadence
// (unless --no-outsource), bind host:port.
func cmdServe(env func(string) string, noReconcile, noOutsource bool, out io.Writer) int {
	cfg, warnings, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn := resolveDSN(env, cfg)
	dbPath, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintf(out, "[ocserverd] FATAL: serve supports sqlite DSNs only for now (got %q)\n", dsn)
		return 1
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", dbPath, err)
		return 1
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: seed: %v\n", err)
		return 1
	}
	auth, err := loadAuthSettings(dal, cfg, func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: load settings: %v\n", err)
		return 1
	}
	api := newAPIServer(dal, NewHub(), auth.secret, auth.tokenTTL, ".")
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.ctxhigh = auth.ctxhigh
	api.outsourceMaxParallel = auth.outsourceMaxParallel
	api.updaterReceiveBeta = auth.updaterReceiveBeta
	api.updaterAutoUpdate = auth.updaterAutoUpdate
	// $OC_RELEASE_API_BASE is a HARNESS seam (conformance/e2e): it re-points
	// the GitHub Releases API base so a black-box run never reaches the real
	// api.github.com (hermeticity + the anonymous rate limit). "" = the real
	// GitHub — normal deployments never set it.
	api.releaseAPIBase = env("OC_RELEASE_API_BASE")
	api.orgName = auth.orgName
	api.ownerName = auth.ownerName
	api.displayTheme = auth.displayTheme
	api.displayLanguage = auth.displayLanguage
	api.namespace = cfg.Server.Namespace
	// The embed-fallback binary cache rides beside the SQLite data file — a
	// stable per-instance location that follows the configured DSN (never the
	// CWD): bootstrap/teardown-here can exec the embedded ocwarden repo-less.
	api.binCacheDir = filepath.Join(filepath.Dir(dbPath), "bin")
	// T-9ca5 ⑤: one-shot alignment of any pre-derivation task whose stored status
	// drifts from what its steps derive to. Non-fatal — a hiccup logs, boot goes on.
	if n, err := api.reconcileTaskStatusesOnBoot(); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARN: task status boot reconcile: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(out, "[ocserverd] task status boot reconcile: aligned %d task(s) to derived status\n", n)
	}
	claimToken, err := ensureFirstRunClaimToken(dal, auth.passwordHash != "", func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: claim token: %v\n", err)
		return 1
	}
	handler, err := buildHandler(specsFor(api), auth.secret, dal.GetMember, api.authPasswordChangedAt)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	// The MCP tools/call loopback re-enters this very mux (auth gate + RBAC
	// choke + param binding included) — wire it back onto the server.
	api.loopback = handler
	// Reconcile producer (reconcile.go): the 30s cadence is ALWAYS ON unless
	// --no-reconcile (the shadow-deployment kill-switch — it also disables the
	// event-driven dispatch seams via api.noReconcile).
	api.noReconcile = noReconcile
	if noReconcile {
		fmt.Fprintln(out, "[ocserverd] --no-reconcile: reconcile producer disabled (no cadence, no warden-command dispatch)")
	} else {
		api.startReconcileCadence(time.Duration(reconcileCadenceSecs * float64(time.Second)))
	}
	// Outsource assignment scheduler (outsource_sched.go; M3 Phase 2): the 30s
	// cadence is ALWAYS ON unless --no-outsource (which also disables the
	// event-driven create_task tick via api.noOutsource).
	api.noOutsource = noOutsource
	if noOutsource {
		fmt.Fprintln(out, "[ocserverd] --no-outsource: outsource-assignment scheduler disabled (no cadence, no event-driven assignment)")
	} else {
		api.startOutsourceCadence(time.Duration(outsourceCadenceSecs * float64(time.Second)))
	}
	// Auto-update cadence (auto_update.go): ALWAYS mounted — the OFF-default
	// `updater.auto_update` setting gates action, so an owner arming it via
	// PATCH /api/settings needs no restart. An unarmed tick is two mutex reads.
	api.startAutoUpdateCadence(autoUpdateCadence)
	// The bind host is hardwired loopback (B2): expose via a tunnel, never a
	// direct non-loopback bind.
	addr := fmt.Sprintf("%s:%d", defaultHost, cfg.Server.Port)
	// Bind FIRST, announce second. The old order printed "serving on ..." before
	// ListenAndServe had bound anything, so a port clash produced a log that
	// claimed success and then immediately contradicted itself with a FATAL —
	// and any reader (human or installer) that trusted the first line was simply
	// lied to. Holding the listener is the only honest moment to say we serve.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %s\n", bindErrorMessage(cfg.Server.Port, err))
		return 1
	}
	fmt.Fprintf(out, "ocserverd serving on http://%s\n", addr)
	if claimToken != "" {
		setupURL := firstRunSetupURL(addr, claimToken)
		fmt.Fprintf(out, "[ocserverd] FIRST RUN: no owner password is set — finish setup in a browser by choosing a password (the link carries the one-shot claim code):\n")
		if shouldAutoOpenBrowser(env, stdoutIsTerminal()) {
			go func() {
				time.Sleep(firstRunBrowserDelay)
				popFirstRunBrowser(browserOpener{goos: runtime.GOOS, run: runBrowserCommand}, setupURL, out)
			}()
		} else {
			fmt.Fprintf(out, "[ocserverd]   %s\n", setupURL)
		}
	}
	// Wrap the listener so every accepted connection carries the keep-alive
	// half-open reaper (T-7e07; sseKeepAlive above).
	if err := http.Serve(keepAliveListener{ln}, handler); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	return 0
}

// bindErrorMessage turns a net.Listen failure into something the operator can
// ACT on. The bare Go error ("listen tcp 127.0.0.1:8770: bind: address already
// in use") states the fact but not the fix; the overwhelmingly common cause is
// a second officraft instance (a stale serve job, or a re-install racing the
// old one), and the fix is to free the port or move this instance off it.
//
// A loud failure here is the DESIGNED behaviour, not a gap waiting for a port
// self-heal. Do not make the server silently bind elsewhere when its preferred
// port is taken: the base URL is hardwired at both ends (launchd plists,
// install.sh's OC_BASE), so an instance that moved its own port would strand
// every warden already installed against the old one. A settings field
// advertising exactly that self-heal once existed on the wire with no writer
// behind it anywhere; it was removed rather than implemented.
func bindErrorMessage(port int, err error) string {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Sprintf(
			"port %d already in use — another process (very likely another officraft server) holds it. "+
				"Free it, or move this instance: set [server].port in oc.toml, or OC_SERVE_PORT=<other>. "+
				"Find the holder with: lsof -nP -iTCP:%d -sTCP:LISTEN",
			port, port)
	}
	return fmt.Sprintf("cannot bind port %d: %v", port, err)
}
