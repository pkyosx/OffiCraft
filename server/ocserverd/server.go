package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// A var so bin/build can stamp it at link time (-X main.appVersion=…); an
// official package (bin/release) stamps the GitHub Release tag, which
// release_check.go compares against the newest release.
var appVersion = "0.0.0"

// Stamped at link time by bin/build (-ldflags -X); when set they win over the
// CWD git probe — the running code's identity is the binary's own build.
var (
	buildSHA  string
	buildTime string
)

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

// Hand-written beside the generated types on purpose: the probe contract pins
// field ORDER and serialises null (never omitted), while the generated structs
// sort optional fields and use omitempty.

type healthDTO struct {
	Status string `json:"status"`
}

type versionDTO struct {
	Version         string  `json:"version"`
	GitSHA          string  `json:"git_sha"`
	GitTime         *string `json:"git_time"`
	CatalogHash     string  `json:"catalog_hash"`
	UpdateAvailable bool    `json:"update_available"`
	LatestVersion   *string `json:"latest_version"`
	// OMITTED (not null) when no update check has ever succeeded, so such a station
	// serves the exact bytes it served before this field existed.
	UpdateCheckedOKAt *string `json:"update_checked_ok_at,omitempty"`
}

// The bare `/version` deploy probe; autodeploy reads `sha`.
type probeVersionDTO struct {
	Version     string `json:"version"`
	SHA         string `json:"sha"`
	CatalogHash string `json:"catalog_hash"`
}

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

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": errorCodeForStatus(status), "message": message},
	})
}

type contextKey string

const claimsContextKey contextKey = "ocserverd.claims"

// Carries WHICH ring key verified the request; requireAuth only hands it down
// and records nothing, because a request is not proof the machine RUNS on that
// credential: a renewing warden presents its CANDIDATE credential before writing
// it to disk (cli/ocwarden/renewapply.go), so recording it here once reported
// "moved to the new key" for a credential the machine never adopted.
const verifyingKeyContextKey contextKey = "ocserverd.verifying_key_id"

func claimsFromContext(ctx context.Context) map[string]any {
	claims, _ := ctx.Value(claimsContextKey).(map[string]any)
	return claims
}

func verifyingKeyFromContext(ctx context.Context) string {
	id, _ := ctx.Value(verifyingKeyContextKey).(string)
	return id
}

// The `?token=` fallback exists because the SPA's EventSource and <img>/<a href>
// blob loads cannot set a header. A present-but-invalid header never falls
// through to the query param.
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
	return r.URL.Query().Get(authTokenQueryParam)
}

// Named because GET /api/chat refuses undeclared query params
// (unknownChatQueryParams) and must know this one is a credential; a second
// spelling there would deny every EventSource and <img src> carrying its token —
// in production only.
const authTokenQueryParam = "token"

// keys is the ring itself, not a key copied out of it: that is what makes a
// rotation take effect on the next request, and removing a key revokes the
// tokens it signed.
// ownerIatFloor is the change-password revocation seam (lifecycle.md §1.3).
// lookup is checked AFTER signature verification, so a forged token never
// reaches a roster read; it also binds every exp-less credential to an active
// warden row — no other signed JWT may become permanent.
func requireAuth(keys *keyring, ownerIatFloor func() int64, lookup func(id string) (*Member, error), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if keys == nil || len(keys.verifySecrets()) == 0 {
			writeError(w, http.StatusUnauthorized, "auth not configured")
			return
		}
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing credentials")
			return
		}
		claims, keyID, err := verifyJWTAnyKey(keys, token, time.Now().Unix())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if ownerIatFloor != nil {
			if scope, _ := claims["scope"].(string); scope == "owner" {
				iat, ok := claims["iat"].(float64)
				if !ok || int64(iat) < ownerIatFloor() {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
			}
		}
		if agentIatFloorRefusal(claims, lookup) {
			// Named on a header so the process holding the superseded session stops
			// retrying instead of reconnecting forever; the body stays the ordinary one on
			// purpose (authRefusalHeader in authz.go).
			w.Header().Set(authRefusalHeader, refusalAgentSuperseded)
			// Logged because this refusal kills a live session (tmux + model); without the
			// line the owner sees a member's tmux vanish with nothing in the server log.
			sub, _ := claims["sub"].(string)
			iat, _ := claims["iat"].(float64)
			log.Printf("[auth] REFUSED %s: agent credential iat=%d is below its "+
				"member's agent_iat_floor — a newer session of this member has "+
				"reported waking, so this one is superseded. Marked %s: %s; the "+
				"process holding it should stop retrying and shut itself down.",
				sub, int64(iat), authRefusalHeader, refusalAgentSuperseded)
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if permanentCredentialRefusal(claims, lookup) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if refusal := memberRemovedRefusal(claims, lookup); refusal != "" {
			w.Header().Set(authRefusalHeader, refusalMemberRemoved)
			sub, _ := claims["sub"].(string)
			log.Printf("[auth] REFUSED %s: member has left the roster. Marked %s: %s; "+
				"the process holding it should stop retrying and shut itself down.",
				sub, authRefusalHeader, refusalMemberRemoved)
			writeError(w, http.StatusUnauthorized, refusal)
			return
		}
		if refusal := revocationRefusal(claims, lookup); refusal != "" {
			writeError(w, http.StatusUnauthorized, refusal)
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		ctx = context.WithValue(ctx, verifyingKeyContextKey, keyID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func shareSigGate(keys *keyring, verify shareSigVerifier, raw, authed http.Handler) http.Handler {
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
		if keys == nil || !verify(keys, r, sig) {
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
		raw.ServeHTTP(w, r)
	})
}

// Wraps the one production call so it is testable: hand buildHandler any ring
// other than api.keys and a rotation moves the minting half while the verifying
// half stays behind — tokens the server itself refuses, with nothing going red.
func buildAPIHandler(api *apiServer, lookup func(id string) (*Member, error)) (http.Handler, error) {
	return buildHandler(specsFor(api), api.keys, lookup, api.authPasswordChangedAt)
}

func buildHandler(specs []RouteSpec, keys *keyring, lookup func(id string) (*Member, error), ownerIatFloor func() int64) (http.Handler, error) {
	mux := http.NewServeMux()
	for _, spec := range specs {
		var h http.Handler = spec.Handler
		if spec.Auth == authGated {
			if spec.Requires != principalMachine {
				h = requirePrincipalClass(spec.Requires, lookup, h)
			}
			h = requireAuth(keys, ownerIatFloor, lookup, h)
			if spec.ShareSig != nil {
				h = shareSigGate(keys, spec.ShareSig, spec.Handler, h)
			}
		}
		mux.Handle(spec.Method+" "+spec.Path, h)
	}
	mux.Handle("/", newFallbackHandler(specs, webdistFS()))
	return answerLockInTx(mux), nil
}

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

// A field left out here compiles and keeps its constructor default, so the
// owner's stored value is silently lost on every restart.
func (s *apiServer) adoptSettings(auth authSettings) {
	s.agentTokenTTL = auth.agentTokenTTL
	s.passwordHash = auth.passwordHash
	s.passwordChangedAt = auth.passwordChangedAt
	s.mfaOffered = auth.mfaOffered
	s.totpSecret = auth.totpSecret
	s.totpLastStep = auth.totpLastStep
	s.ctxHigh = auth.ctxHigh
	s.codexCompactionThreshold = auth.codexCompactionThreshold
	s.codexNoticeRound = auth.codexNoticeRound
	s.monitoringRefreshSeconds = auth.monitoringRefreshSeconds
	s.acceleratedGraceSecs = auth.acceleratedGraceSecs
	s.reassignHandoverTimeoutSecs = auth.reassignHandoverTimeoutSecs
	s.runtimeLoginCheckIntervalSecs = auth.runtimeLoginCheckIntervalSecs
	s.runtimeLoginRecheckIntervalSecs = auth.runtimeLoginRecheckIntervalSecs
	s.wardenCredLifetimeSecs = auth.wardenCredLifetimeSecs
	s.outsourceMaxParallel = auth.outsourceMaxParallel
	s.docCapCharsDuty = auth.docCapCharsDuty
	s.docCapCharsInsight = auth.docCapCharsInsight
	s.docCapCharsManualSop = auth.docCapCharsManualSop
	s.docCapCharsSystemInteraction = auth.docCapCharsSystemInteraction
	s.docCapCharsBootSequence = auth.docCapCharsBootSequence
	s.docCapCharsOffboard = auth.docCapCharsOffboard
	s.loreCapCharsRole = auth.loreCapCharsRole
	s.loreCapCharsManual = auth.loreCapCharsManual
	s.loreCapCharsTitle = auth.loreCapCharsTitle
	s.loreCapCharsBody = auth.loreCapCharsBody
	s.chatBudgetChars = auth.chatBudgetChars
	s.stepNoteCapChars = auth.stepNoteCapChars
	s.backupRetain = auth.backupRetain
	s.updaterReceiveBeta = auth.updaterReceiveBeta
	s.updaterAutoUpdate = auth.updaterAutoUpdate
	s.orgName = auth.orgName
	s.ownerName = auth.ownerName
	s.pushContactEmail = auth.pushContactEmail
	s.displayTheme = auth.displayTheme
	s.displayLanguage = auth.displayLanguage
	s.displayWide = auth.displayWide
	s.suggestedRepliesReplyCard = auth.suggestedRepliesReplyCard
	s.suggestedRepliesTaskMessage = auth.suggestedRepliesTaskMessage
	s.suggestedRepliesLoreMessage = auth.suggestedRepliesLoreMessage
}

// Build identity is captured ONCE so the probes report the RUNNING code's sha:
// an autodeploy that pulls a new sha but fails to restart keeps reporting the old.
func newAPIServer(dal *DAL, hub *Hub, keys *keyring, tokenTTL int64, root assetRoot) *apiServer {
	// Assembly is a WRITE against the store (expiry sweep) and rehydrates the
	// warden-command FIFO into this hub: a second apiServer over a DAL a live one
	// is using would land the same pending command in two hubs.
	if dal != nil && hub != nil {
		hub.BindWardenCommandStore(dal)
	}
	return &apiServer{
		processSHA:                      gitSHA(),
		processTime:                     gitTime(),
		dal:                             dal,
		hub:                             hub,
		telemetry:                       newMemStore(),
		gauge:                           newMemStore(),
		machineClaims:                   newMachineClaimStore(),
		runtimeLogins:                   newRuntimeLoginStore(),
		keys:                            keys,
		ownerTokenTTL:                   tokenTTL,
		agentTokenTTL:                   defaultAgentTokenTTL,
		acceleratedGraceSecs:            acceleratedGraceSecsDefault,
		reassignHandoverTimeoutSecs:     reassignHandoverTimeoutSecsDefault,
		runtimeLoginCheckIntervalSecs:   runtimeLoginCheckIntervalSecsDefault,
		runtimeLoginRecheckIntervalSecs: runtimeLoginRecheckIntervalSecsDefault,
		wardenCredLifetimeSecs:          wardenCredLifetimeSecsDefault,
		outsourceMaxParallel:            defaultOutsourceMaxParallel,
		docCapCharsDuty:                 dutyCapCharsDefault,
		docCapCharsInsight:              contextDocMaxCharsDefault,
		docCapCharsManualSop:            contextDocMaxCharsDefault,
		docCapCharsSystemInteraction:    systemInteractionCapCharsDefault,
		docCapCharsBootSequence:         bootSequenceCapCharsDefault,
		docCapCharsOffboard:             offboardCapCharsDefault,
		loreCapCharsRole:                loreRoleCapCharsDefault,
		loreCapCharsManual:              loreManualCapCharsDefault,
		loreCapCharsTitle:               loreTitleCapCharsDefault,
		loreCapCharsBody:                loreBodyCapCharsDefault,
		chatBudgetChars:                 chatBudgetCharsDefault,
		stepNoteCapChars:                stepNoteCapCharsDefault,
		backupRetain:                    backupRetainDefault,
		suggestedRepliesReplyCard:       []string{},
		suggestedRepliesTaskMessage:     []string{},
		suggestedRepliesLoreMessage:     []string{},
		ctxHigh:                         defaultSseContextHigh(),
		root:                            root,
		binHashes:                       bindistBinaryHashesFrom(bindistFS()),
		reconcileCfg:                    defaultReconcileConfig(),
		identitySweepAt:                 map[string]float64{},
		receiptPending:                  map[string]pendingReceipt{},
		workerSpawnAt:                   map[string]float64{},
		workerSpawnTarget:               map[string]string{},
		workerSpawnAttempts:             map[string]int{},
		workerReclaimed:                 map[string]bool{},
		workerStopPending:               map[string]string{},
		workerStopLanded:                map[string]workerStopDispatch{},
		workerMachinePref:               map[string]string{},
		workerMachineBench:              map[string]float64{},
		workerTakeoverBench:             map[string]takeoverBench{},
		workerTakeoverLiftedAt:          map[string]float64{},
	}
}

func defaultRouteSpecs() []RouteSpec {
	return specsFor(newAPIServer(nil, NewHub(), nil, defaultOwnerTokenTTL, "."))
}

// Socket-level keep-alive because a peer that vanishes silently (no FIN/RST)
// leaves SSE heartbeat writes succeeding into the kernel buffer, so neither
// r.Context() nor a write deadline notices (the old ~15 min wedge that pinned a
// member online / 409-on-reconnect). ~30 s to reap; deliberately not more
// aggressive, or transient jitter reaps healthy connections.
var sseKeepAlive = net.KeepAliveConfig{
	Enable:   true,
	Idle:     15 * time.Second,
	Interval: 5 * time.Second,
	Count:    3,
}

type keepAliveConn interface {
	SetKeepAliveConfig(net.KeepAliveConfig) error
}

func applyKeepAlive(c net.Conn) {
	if kc, ok := c.(keepAliveConn); ok {
		_ = kc.SetKeepAliveConfig(sseKeepAlive)
	}
}

// http.Serve(ln, …), unlike ListenAndServe, sets no keep-alive of its own.
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

const stationShutdownTimeout = 5 * time.Second

func cmdServe(env func(string) string, noReconcile, noOutsource bool, out io.Writer) int {
	cfg, dsn, rc := announceResolution("serve", env, out)
	if rc != 0 {
		return rc
	}
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
	// Snapshot BEFORE migrations: a migration, not the binary swap, is what can
	// hurt the data. Never fatal on purpose — a failed backup must not become an outage.
	backupBeforeMigrations(db, dbPath, time.Now())
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return 1
	}
	// SQLite silently ignores a malformed pragma, so this check is the only thing
	// that would notice a typo in openSQLite's DSN. Deliberately not fatal.
	if mode, err := assertJournalMode(db, sqliteJournalMode); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARNING: %v — every request will serialise at the database again, and that is this regression's ONLY symptom (T-dd7a)\n", err)
	} else {
		fmt.Fprintf(out, "[ocserverd] journal_mode=%s (reads do not queue behind each other)\n", mode)
	}
	// Opened AFTER the migration: `mode=ro` never creates a file and a read-only
	// connection cannot recover a WAL.
	rdb, err := openSQLiteReadPool(dbPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open read pool %s: %v\n", dbPath, err)
		return 1
	}
	defer rdb.Close()
	dal := NewDALPools(db, rdb)
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
	keys, err := loadKeyring(dal, auth.secret)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: load signing keys: %v\n", err)
		return 1
	}
	api := newAPIServer(dal, NewHub(), keys, auth.ownerTokenTTL, ".")
	api.adoptSettings(auth)
	// A conformance/e2e harness seam that re-points the GitHub Releases API base;
	// normal deployments never set it.
	api.releaseAPIBase = env("OC_RELEASE_API_BASE")
	api.namespace = cfg.Server.Namespace
	api.binCacheDir = filepath.Join(filepath.Dir(dbPath), "bin")
	if n, err := api.reconcileTaskStatusesOnBoot(); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARN: task status boot reconcile: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(out, "[ocserverd] task status boot reconcile: aligned %d task(s) to derived status\n", n)
	}
	// Runs AFTER the task reconcile above, so a task it just closed is seen closed.
	if n, err := api.reconcileOrphanReplyCardsOnBoot(); err != nil {
		fmt.Fprintf(out, "[ocserverd] WARN: orphan reply-card boot reconcile: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(out, "[ocserverd] orphan reply-card boot reconcile: retired %d card(s) stranded on closed tasks\n", n)
	}
	claimToken, err := ensureFirstRunClaimToken(dal, auth.passwordHash != "", func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: claim token: %v\n", err)
		return 1
	}
	handler, err := buildAPIHandler(api, dal.GetMember)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	api.loopback = handler
	api.noReconcile = noReconcile
	api.noOutsource = noOutsource
	if noReconcile {
		fmt.Fprintln(out, "[ocserverd] --no-reconcile: reconcile producer disabled (no cadence half, no warden-command dispatch)")
	}
	if noOutsource {
		fmt.Fprintln(out, "[ocserverd] --no-outsource: outsource-assignment scheduler disabled (no cadence half, no event-driven assignment)")
	}
	api.startLifecycleCadence(time.Duration(lifecycleCadenceSecs * float64(time.Second)))
	api.startAutoUpdateCadence(autoUpdateCadence)
	api.startScheduledMessageCadence(scheduledMessageCadence)
	api.startRuntimeLoginSweep(runtimeLoginSweepPeriod)
	// The watchdog is armed synchronously and deliberately NOT hung off the backup
	// cadence: the failure it exists to catch is "the cadence never ran at all".
	api.backupHealth = armBackupHealth(dal, dbPath, time.Now())
	startBackupHealthWatchdog(api.backupHealth, backupWatchdogCadence)
	startBackupCadence(dal.wdb, dbPath, backupCadence, api.backupHealth)
	// The bind host is hardwired loopback (B2): expose via a tunnel, never a direct
	// non-loopback bind.
	addr := fmt.Sprintf("%s:%d", defaultHost, cfg.Server.Port)
	api.recoverStaleOnboarding()
	// Bind FIRST, announce second: the old order printed "serving on ..." and then
	// a FATAL on a port clash.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %s\n", bindErrorMessage(cfg.Server.Port, err))
		return 1
	}
	boundAddr := ln.Addr().String()
	api.selfBase = schemeForHost(boundAddr) + "://" + boundAddr
	fmt.Fprintf(out, "ocserverd serving on http://%s\n", boundAddr)
	if claimToken != "" {
		setupURL := firstRunSetupURL(boundAddr, claimToken)
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
	// A signal shutdown marks the cause before cancelling this context, so active
	// SSE handlers record station-shutdown rather than a peer disconnect. An upgrade
	// marks the same cause but keeps the listener alive until syscall.Exec.
	stationCtx, stationCancel := context.WithCancel(context.Background())
	api.stationCancel = stationCancel
	httpServer := &http.Server{
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return stationCtx
		},
	}
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	shutdownDone := make(chan struct{})
	go func() {
		<-signalCtx.Done()
		api.markStationShutdown()
		api.cancelStationContext()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), stationShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()
	if err := httpServer.Serve(keepAliveListener{ln}); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return 1
	}
	if signalCtx.Err() != nil {
		<-shutdownDone
	}
	return 0
}

// A loud failure is the designed behaviour: never silently bind elsewhere. The
// base URL is hardwired at both ends (launchd plists, install.sh's OC_BASE), so
// an instance that moved its port would strand every installed warden.
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
