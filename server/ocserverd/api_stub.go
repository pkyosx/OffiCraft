package main

// api_stub.go — the apiServer carrier + the build-identity probes (M3 REST
// sub-batch B: the 50 sub-batch-A 501 stubs are FILLED — the business
// handlers live in the api_*.go module files beside this one; the route
// table, auth gate, and RBAC choke were untouched, exactly as sub-batch A
// promised).
//
// apiServer implements the oapi-codegen-generated ServerInterface
// (ocapi_gen.go, derived from spec/openapi.json — the frozen wire SSOT).

import (
	"net/http"
	"sync"
)

// apiServer carries the per-process state the handlers need: the durable DAL,
// the SSE hub (online/machine projection + the fan-out seam), the two
// in-memory observation stores, the auth material, and the repo-file asset
// root. Build identity is captured ONCE at construction (process start) — the
// handlers._PROCESS_SHA contract.
type apiServer struct {
	processSHA  string
	processTime string
	catalogHash string
	dal         *DAL
	hub         *Hub
	telemetry   *memStore
	gauge       *memStore
	// machineClaims holds the pending one-time machine claim codes (in-memory
	// only, like the observation stores — a restart voids them, which reads
	// exactly like expiry).
	machineClaims *machineClaimStore
	secret        []byte
	// settingsMu guards the LIVE settings snapshot below (passwordHash /
	// passwordChangedAt / tokenTTL / ctxhigh): the boot-time DB snapshot is
	// updated IN PLACE by the B3 owner endpoints (set-password /
	// change-password / PATCH /api/settings) while the SSE stream loop and the
	// reconcile cadence read it concurrently — reads go through the
	// auth*/ctxHighConfig accessors, never the bare fields.
	settingsMu sync.RWMutex
	// passwordHash is the DB-stored argon2id owner-password hash (settings.go;
	// "" = not set — every login denied until set-password / the B3 first-run
	// flow writes one).
	passwordHash string
	// passwordChangedAt (epoch secs, 0 = never changed) is the owner-session
	// revocation cut: owner-scope tokens with iat before it are refused at the
	// auth gate (requireAuth) — stamped by change-password.
	passwordChangedAt int64
	// tokenTTL is the owner/bootstrap JWT lifetime in seconds (DB
	// auth.token_ttl setting).
	tokenTTL int64
	// outsourceMaxParallel is the global cap on concurrently live outsource
	// workers (DB task.outsource_max_parallel; M3 owner ruling ③) — read by
	// the Phase 2 assignment scheduler.
	outsourceMaxParallel int
	// updaterReceiveBeta picks which GitHub releases the update check follows
	// (false = official only, true = prereleases too); updaterAutoUpdate arms
	// the background self-upgrade cadence (auto_update.go). Both default OFF
	// (DB updater.* settings).
	updaterReceiveBeta bool
	updaterAutoUpdate  bool
	// orgName is the studio display name shown in the cockpit topbar (DB
	// org.name; T-d693). "" = never set — the frontend falls back to the
	// localized default. Owner-writable via PATCH /api/settings; every agent
	// reads it back through get_global_context.
	orgName string
	// namespace is the [server].namespace instance key ("" = main instance).
	// It leaves the server on exactly two surfaces: the install.sh install line
	// and the bootstrap/teardown-here child env (OC_NAMESPACE) — the single
	// cross-plane propagation line for same-machine multi-instance.
	namespace string
	// ctxhigh is the context-high band config the /api/events stream loop
	// evaluates each quiet tick (DB ctx.* settings; defaults when unset).
	ctxhigh SseContextHighConfig
	// root anchors the repo-file assets (seeds / prebuilt binaries / frozen
	// MCP catalog) — see assets.go.
	root assetRoot
	// binHashes fingerprints the embedded prebuilt ocwarden/ocagent (assets.go
	// bindistBinaryHashesFrom, captured ONCE at construction — the embed never
	// changes within a process). Compared against the fingerprints each warden
	// heartbeat reports to compute the machine rows' bin_status (T-5f01).
	// Empty entries (pristine .gitkeep-only bindist in tests) read as unknown.
	binHashes map[string]string
	// binCacheDir is where an embed-fallback ocwarden is materialized as an
	// executable file (assets.go materializeBinary): the per-instance dir
	// beside the SQLite data file, stamped by cmdServe. "" (tests /
	// dependency-free tables) disables the fallback — exec paths answer 503.
	binCacheDir string
	// mcpTools maps tool name → route row (the non-mcp_exclude table surface;
	// stamped by specsFor) — the tools/call routing index (mcp.go).
	mcpTools map[string]RouteSpec
	// loopback is the app's own assembled mux (stamped after buildHandler);
	// tools/call re-enters it in-process so the auth gate + RBAC choke +
	// param binding run exactly as for a direct REST call. nil (not wired,
	// e.g. dependency-free test tables) → an honest -32603.
	loopback http.Handler
	// ── reconcile producer state (reconcile.go; lifecycle.md §3 inventory #7) ──
	// reconcileMu serializes the 30s cadence tick with any event-driven
	// immediate tick (the Python per-app tick lock) AND guards the store.
	reconcileMu sync.Mutex
	// reconcileStates is the in-memory per-member bookkeeping — restart
	// amnesia is contract (the next tick re-decides from presence).
	reconcileStates map[string]reconcileState
	reconcileCfg    reconcileConfig
	// noReconcile is the --no-reconcile serve flag: disables the cadence loop
	// AND every event-driven warden-command dispatch (the shadow-deployment
	// kill-switch) while the rest of the server runs unchanged.
	noReconcile bool
	// identitySweepAt (T-bb29 §3) → member id → last cross-machine identity-sweep
	// dispatch ts. The connection-edge 正身 sweep fires on every SSE first-connect
	// on the desired machine; this dedupe window keeps a steady-state reconnect
	// from re-broadcasting a (harmless, idempotent) STOP to every other warden on
	// each flap. Guarded by reconcileMu (the sweep is a reconcile-family dispatch);
	// in-memory, restart-amnesia safe (a forgotten entry just allows one extra
	// idempotent sweep).
	identitySweepAt map[string]float64
	// ── outsource assignment scheduler state (outsource_sched.go; M3 Phase 2) ──
	// outsourceMu serializes the scheduler's 30s cadence tick with the
	// event-driven create_task tick. There is no in-memory ledger to guard —
	// the outsource_worker rows are the bookkeeping (every tick recounts).
	outsourceMu sync.Mutex
	// noOutsource is the --no-outsource serve flag: disables the scheduler
	// wholesale (cadence AND the event-driven tick) — the --no-reconcile
	// mirror for the outsource-assignment producer.
	noOutsource bool
	// ── outsource worker wake/reclaim state (worker_spawn.go; M3 Phase 6) ────
	// All three maps live under outsourceMu. IN-MEMORY ONLY by design: a
	// restart forgets pacing (one extra worker_start, refused by the warden
	// clobber guard) and reclaim receipts (one extra worker_stop per released
	// worker, a clean no-op against an absent session) — the worker rows stay
	// the only durable truth.
	workerSpawnAt     map[string]float64 // worker id → last worker_start dispatch ts
	workerSpawnTarget map[string]string  // worker id → warden the spawn targeted
	workerReclaimed   map[string]bool    // worker id → a worker_stop went out
	// workerSpawnAttempts (A案 P7d) → worker id → worker_start dispatch count.
	// The former durable spawn_attempts/last_spawn_ts/last_spawn_target columns
	// did NOT survive the outsource_worker→member fold (migrations/00025):
	// spawn observability is in-memory now, the member-reconcile posture. The
	// cockpit machine cell folds from workerSpawnTarget (workerSpawnObs).
	workerSpawnAttempts map[string]int
	// workerStopPending (A案 P5a rework) → worker id → warden a REFUSED
	// worker_stop still owes a kill on. The fail-closed dispatch gate drops a
	// STOP toward an unreachable warden; a live-worker kill (owner 停止 /
	// relocate / refocus) must not be silently lost on that drop — the old
	// session would sit 殘活 when its machine reconnects. Parked here, re-fired
	// by the scheduler tick until the target drains it. In-memory like its
	// siblings (a restart forgets it — the same honest amnesia as
	// workerReclaimed; an extra or lost-after-restart stop is a no-op /
	// re-parked on the next owner action).
	workerStopPending map[string]string
	// workerMachinePref is the per-worker spawn placement override a reassign
	// carries (T-160e: the dialog picks model/effort/machine for the fresh
	// worker; scheduler-minted workers read the manual instead). In-memory
	// like its siblings: after a restart the spawn retry honestly falls back
	// to the manual preference / "auto".
	workerMachinePref map[string]string // worker id → "auto" | machine id
	// workerReconcileStates (A案 P6) → worker id → shared-FSM bookkeeping.
	// The outsource spawn/rescue path runs the SAME pure member reconcile FSM
	// (reconcileDecide: start_timeout / backoff / circuit / zombie-takeover —
	// reconcileWorkerLiveness), which retired the bespoke one-shot ghost-clear
	// (recoverStuckWorker + workerGhostKillAt). Kept as its OWN store under
	// outsourceMu (never reconcileStates/reconcileMu) so the two producers
	// stay lock-disjoint; restart amnesia is the contract, like the member
	// store — the next tick re-decides from presence.
	workerReconcileStates map[string]reconcileState
	// workerMachineCooldown (T-9ccf DoD②, 換機重試) → "<worker id>|<machine id>"
	// → cooldown-until ts. A machine that just FAILED to boot a worker (a
	// worker_start receipt refused, or a stuck-worker ghost cleared off it) is
	// benched for that worker until the stamped ts, so the very next pick skips
	// it and lands the re-spawn on a DIFFERENT warden — the "挑中壞機 → 90s 後重挑
	// 同一台恆失敗" loop (recon O-19 hypothesis 1) is broken. When EVERY online
	// warden is cooling, the pick honestly returns "" (worker waits, visible as
	// spawn_state=stuck) rather than hammering a known-bad host. In-memory like
	// its siblings — a restart forgets the bench (worst case one re-pick of a
	// still-bad machine, which re-benches on its next failure).
	workerMachineCooldown map[string]float64
	// ── software update check state (update_check.go; GitHub Releases) ───────
	// updateMu guards updateCheck — the cached result of the last GitHub
	// releases probe; /api/version reads it lock-briefly and NEVER waits on
	// the network.
	updateMu    sync.Mutex
	updateCheck updateCheckState
	// releaseAPIBase ("" = the real https://api.github.com) is a TEST SEAM:
	// tests point the release check AND the upgrade download at a local
	// httptest server.
	releaseAPIBase string
	// ── upgrade execution state (upgrade.go) ─────────────────────────────────
	// upgradeMu serializes POST /api/update/upgrade: TryLock — a second click
	// while a download/swap runs answers an honest 409, never a second swap.
	upgradeMu sync.Mutex
	// upgradeExeOverride ("" = os.Executable()) and upgradeRestart (nil = the
	// real re-exec) are TEST SEAMS: tests point the swap at a scratch file and
	// capture the restart instead of exec'ing the test process away.
	upgradeExeOverride string
	upgradeRestart     func(exePath string)
}

// ── the four public build-identity probes ────────────────────────────────────

func (s *apiServer) health(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, healthDTO{Status: "ok"})
}

func (s *apiServer) HandleHealthHealthGet(w http.ResponseWriter, r *http.Request) {
	s.health(w)
}

func (s *apiServer) HandleHealthApiHealthGet(w http.ResponseWriter, r *http.Request) {
	s.health(w)
}

func (s *apiServer) HandleVersionApiVersionGet(w http.ResponseWriter, r *http.Request) {
	var gt *string
	if s.processTime != "" {
		t := s.processTime
		gt = &t
	}
	// Live update-check answer (update_check.go): cached, background-refreshed
	// GitHub Releases probe — honest-static (false, nil) while nothing newer
	// is known.
	available, latest := s.updateStatus()
	writeJSON(w, http.StatusOK, versionDTO{
		Version: appVersion,
		GitSHA:  s.processSHA,
		GitTime: gt,
		// Derived over the non-mcp_exclude route rows (the normative
		// handlers.current_catalog_hash algorithm) — the agent-restart signal.
		CatalogHash:     s.catalogHash,
		UpdateAvailable: available,
		LatestVersion:   latest,
	})
}

func (s *apiServer) HandleProbeVersionVersionGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, probeVersionDTO{
		Version:     appVersion,
		SHA:         s.processSHA,
		CatalogHash: s.catalogHash,
	})
}

// The compile-time seal: apiServer must cover the WHOLE generated interface —
// a spec regen that adds an operation fails the build here until its
// implementation exists.
var _ ServerInterface = (*apiServer)(nil)

// ── live settings snapshot accessors (settingsMu) ────────────────────────────

// authPasswordHash returns the current owner-password hash ("" = not set).
func (s *apiServer) authPasswordHash() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.passwordHash
}

// authPasswordChangedAt returns the owner-token iat floor (0 = no cut).
func (s *apiServer) authPasswordChangedAt() int64 {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.passwordChangedAt
}

// authTokenTTL returns the owner/bootstrap JWT lifetime in seconds.
func (s *apiServer) authTokenTTL() int64 {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.tokenTTL
}

// outsourceParallelCap returns the live outsource-worker concurrency cap.
func (s *apiServer) outsourceParallelCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.outsourceMaxParallel
}

// orgNameSnapshot returns the live studio display name (org.name; T-d693).
// "" = never set — callers decide the fallback (the topbar's localized default
// lives frontend-side; agents see the empty name as "studio unnamed").
func (s *apiServer) orgNameSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.orgName
}

// ctxHighConfig returns the live context-high band config (by value — one
// coherent snapshot per call site).
func (s *apiServer) ctxHighConfig() SseContextHighConfig {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ctxhigh
}
