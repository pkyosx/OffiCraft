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
	"io/fs"
	"net/http"
	"sync"

	"github.com/SherClockHolmes/webpush-go"
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
	// pushHTTPClient is normally the guarded client assembled in api_push.go;
	// tests can substitute a recording client without opening a real socket.
	pushHTTPClient webpush.HTTPClient
	hub            *Hub
	telemetry      *memStore
	gauge          *memStore
	// machineClaims holds the pending one-time machine claim codes (in-memory
	// only, like the observation stores — a restart voids them, which reads
	// exactly like expiry).
	machineClaims *machineClaimStore
	secret        []byte
	// settingsMu guards the LIVE settings snapshot below (passwordHash /
	// passwordChangedAt / ownerTokenTTL / agentTokenTTL / ctxhigh): the boot-time DB snapshot is
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
	ownerTokenTTL     int64
	agentTokenTTL     int64
	// outsourceMaxParallel is the global cap on concurrently live outsource
	// workers (DB task.outsource_max_parallel; M3 owner ruling ③) — read by
	// the Phase 2 assignment scheduler.
	outsourceMaxParallel int
	// docCapChars* are the live size caps on the accumulating context documents
	// (DB doc.cap_chars.{duty,insight,learning,manual_sop,manual_learnings};
	// T-3aeb owner ruling 2026-07-31, split four ways by T-ae38 owner ruling
	// 2026-08-03, and the manual's one split into two by T-30f1) — read by every
	// DocCapBlocked call site through the matching accessor (dutyCap /
	// insightCap / learningCap / manualSopCap / manualLearningsCap).
	docCapCharsDuty            int
	docCapCharsInsight         int
	docCapCharsLearning        int
	docCapCharsManualSop       int
	docCapCharsManualLearnings int
	// The two boot-context document kinds, editable since T-791e (DB
	// doc.cap_chars.{system_interaction,boot_sequence}). bootSequence is ONE cap
	// serving both runtimes. The 下線程序 document (T-c9c0) joins them with its
	// own knob, doc.cap_chars.offboard.
	docCapCharsSystemInteraction int
	docCapCharsBootSequence      int
	docCapCharsOffboard          int
	// chatBudgetChars is the live budget of the wake snapshot's chat block (DB
	// chat.budget_chars; T-c9b4). Read through chatBudget() by
	// resumeSnapshotParts — the ONE place the number enters the packer, which is
	// why resume_summary and peek_resume_summary_size cannot disagree about it.
	chatBudgetChars int
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
	// ownerName is the owner's display nickname shown in the cockpit topbar
	// profile pill (DB owner.name; T-0b41). "" = never set — the frontend falls
	// back to the localized default. Owner-writable via PATCH /api/settings so
	// the nickname syncs across the owner's devices. NOT an agent read path.
	ownerName string
	// pushContactEmail is the address handed to the push gateways as the VAPID
	// subject (DB push.contact_email; T-8a82). Owner-supplied because the server
	// sits behind a tunnel and cannot know a reachable identity for itself. "" =
	// never set, and Web Push delivery is then refused rather than attempted
	// with an address the gateways would reject.
	pushContactEmail string
	// displayTheme / displayLanguage are the owner's cockpit visual prefs (DB
	// display.theme / display.language; T-0b41-p2). Owner-writable via PATCH
	// /api/settings so they sync across devices, but because they must apply
	// BEFORE login the frontend keeps a localStorage cache and reconciles this
	// server value in at login (server = cross-device truth). "" = never set —
	// the frontend keeps its cached/default value. NOT an agent read path.
	displayTheme    string
	displayLanguage string
	// displayWide is the owner's cockpit layout width (DB display.wide; T-756f)
	// under the SAME dual-layer contract as the two prefs above. false (the
	// default) = the centred ~1040px content column the cockpit ships with; true
	// lifts that cap. NOT an agent read path.
	displayWide bool
	// displayCustomThemes is the owner's saved custom theme bundles (DB
	// display.custom_themes; T-16a1 P2). nil = none saved. Owner-writable via
	// PATCH /api/settings so the set syncs across devices; display.theme may
	// point at any id in it. NOT an agent read path.
	displayCustomThemes []ThemeBundleDTO
	// selfBase is this server's OWN loopback base URL ("http://127.0.0.1:PORT"),
	// stamped by cmdServe once the bind address is known. It exists for the ONE
	// in-process caller that needs an OC_BASE with no HTTP request to derive it
	// from: the automatic first-run onboarding (onboarding.go), which installs
	// this host's warden and must tell it where to dial back. "" outside serve
	// (tests / migrate), where onboarding never runs.
	selfBase string
	// namespace is the [server].namespace instance key ("" = main instance).
	// It leaves the server on exactly two surfaces: the install.sh install line
	// and the bootstrap/teardown-here child env (OC_NAMESPACE) — the single
	// cross-plane propagation line for same-machine multi-instance.
	namespace string
	// ctxhigh is the context-high band config the /api/events stream loop
	// evaluates each quiet tick (DB ctx.* settings; defaults when unset).
	ctxhigh                  SseContextHighConfig
	codexCompactionThreshold int // the FINAL round (handover)
	codexNoticeRound         int // the FIRST, soft notice round (T-a9d6)
	// handoverNoticed records, per agent id, the SESSION anchor (the gauge's
	// boot_ts) whose one-and-only advance handover notice has already gone out.
	// Guarded by settingsMu. See claimHandoverNotice for why the key is the
	// session anchor and not the connection.
	handoverNoticed map[string]float64
	// softEscalated records, per member id, the soft-offboard epoch whose
	// promotion to the final call has already been announced — the frame that
	// tells the agent its 120 seconds have started. Keyed by the epoch so a
	// re-armed wind-down announces again; guarded by its own mutex because it is
	// touched from the reconcile tick, not the settings path.
	softEscalated            map[string]float64
	softEscalatedMu          sync.Mutex
	monitoringRefreshSeconds int
	// root anchors the repo-file assets (seeds / prebuilt binaries / frozen
	// MCP catalog) — see assets.go.
	root assetRoot
	// binHashes fingerprints the embedded prebuilt ocwarden/ocagent (assets.go
	// bindistBinaryHashesFrom, captured ONCE at construction — the embed never
	// changes within a process). Compared against the fingerprints each warden
	// heartbeat reports to compute the machine rows' bin_status (T-5f01).
	// Empty entries (pristine .gitkeep-only bindist in tests) read as unknown.
	binHashes map[string]string
	// backupHealth is the durable backup-health verdict (backup_health.go),
	// armed by cmdServe. nil in dependency-free tables and route-shape probes —
	// the endpoint then answers an honest `unknown`, never a green light.
	backupHealth *backupHealthMonitor
	// binCacheDir is where an embed-fallback ocwarden is materialized as an
	// executable file (assets.go materializeBinary): the per-instance dir
	// beside the SQLite data file, stamped by cmdServe. "" (tests /
	// dependency-free tables) disables the fallback — exec paths answer 503.
	binCacheDir string
	// ocwardenFS is the bootstrap-here / teardown-here BINARY-RESOLUTION seam.
	// nil in production (→ bindistFS()); tests inject an fstest.MapFS so the
	// HTTP handlers can be driven END TO END without an embedded bindist.
	// The EXEC seam is the package-level runOcwarden var (T-5047) — do not add a
	// second one here.
	ocwardenFS fs.FS
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
	// ── receipt deadline state (receipt_watch.go; T-b36a step 3) ─────────────
	// receiptPending → target id (member id OR outsource worker id — the P5b
	// verbs share one namespace) → the start/stop still owed a command_result.
	// Guarded by its OWN mutex, never reconcileMu/outsourceMu: it is armed from
	// both producers and disarmed from the telemetry ingest goroutine, so
	// borrowing either producer's lock would couple them through the ingest path.
	// In-memory, restart-amnesia by design (the same posture as reconcileStates):
	// a forgotten watch just means one dispatch goes unwatched, never a false
	// receipt_missing on a member the server never dispatched to.
	receiptMu      sync.Mutex
	receiptPending map[string]pendingReceipt
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
	// to the manual preference.
	workerMachinePref map[string]string // worker id → machine id
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

func (s *apiServer) ownerTokenTTLValue() int64 {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ownerTokenTTL
}

func (s *apiServer) agentTokenTTLValue() int64 {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.agentTokenTTL
}

// outsourceParallelCap returns the live outsource-worker concurrency cap.
func (s *apiServer) outsourceParallelCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.outsourceMaxParallel
}

// dutyCap / insightCap / learningCap / manualSopCap / manualLearningsCap return
// the live cap, in runes, on each accumulating context document (T-3aeb; split
// four ways in T-ae38; the manual's one split in two by T-30f1). Every
// DocCapBlocked / docCapRefusal call site reads its OWN one HERE, at request
// time, rather than caching it: a PATCH to a setting takes effect on the next
// write with no restart, and there is no second copy to drift.
//
// FIVE accessors and no generic docCap(caller-picks-a-segment) on purpose: the
// segment a write belongs to is a property of the write seam, not a runtime
// argument, so making it a parameter would let a call site pass the wrong one
// and compile. The names are the only thing a reviewer has to check — which is
// exactly why the manual's two did NOT become manualCap(kind).
func (s *apiServer) dutyCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsDuty
}

func (s *apiServer) insightCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsInsight
}

func (s *apiServer) learningCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsLearning
}

func (s *apiServer) manualSopCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsManualSop
}

func (s *apiServer) manualLearningsCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsManualLearnings
}

// systemInteractionCap / bootSequenceCap are the same accessor shape for the
// two boot-context document kinds that became editable in T-791e.
// bootSequenceCap is ONE number serving BOTH runtimes — each document is
// measured on its own text, but the budget is shared, because the two are the
// same short checklist rendered for two runtimes.
func (s *apiServer) systemInteractionCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsSystemInteraction
}

func (s *apiServer) bootSequenceCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsBootSequence
}

func (s *apiServer) offboardCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsOffboard
}

// chatBudget is the live wake-snapshot chat budget (chat.budget_chars;
// T-c9b4). Read at request time like every cap above, so a PATCH takes effect
// on the next snapshot with no restart.
//
// 🔴 It has exactly ONE caller — resumeSnapshotParts — and that is the point:
// GET /api/resume-summary, GET /api/resume-summary-size and
// GET /api/members/{id}/resume-summary are all assembled by that one function,
// so the peek's estimated_total_chars and the snapshot's own chat_chars are
// bounded by the same read of the same setting. Adding a second call site is
// how the two faces start disagreeing.
func (s *apiServer) chatBudget() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.chatBudgetChars
}

// orgNameSnapshot returns the live studio display name (org.name; T-d693).
// "" = never set — callers decide the fallback (the topbar's localized default
// lives frontend-side; agents see the empty name as "studio unnamed").
func (s *apiServer) orgNameSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.orgName
}

// ownerNameSnapshot returns the live owner display nickname (owner.name;
// T-0b41). "" = never set — the topbar's profile pill shows the localized
// default (frontend-side).
func (s *apiServer) ownerNameSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ownerName
}

// displayThemeSnapshot / displayLanguageSnapshot return the live cockpit display
// prefs (display.theme / display.language; T-0b41-p2). "" = never set — the
// frontend keeps its localStorage cache / default.
func (s *apiServer) displayThemeSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.displayTheme
}

func (s *apiServer) displayLanguageSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.displayLanguage
}

// displayWideSnapshot returns the live cockpit layout width (display.wide;
// T-756f). false = the narrow centred column (the default).
func (s *apiServer) displayWideSnapshot() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.displayWide
}

// displayCustomThemesSnapshot returns a copy of the live custom theme bundles
// (display.custom_themes; T-16a1 P2). Always non-nil for the wire (an empty
// array, never null), and a copy so a caller can never mutate the snapshot.
func (s *apiServer) displayCustomThemesSnapshot() []ThemeBundleDTO {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	out := make([]ThemeBundleDTO, len(s.displayCustomThemes))
	copy(out, s.displayCustomThemes)
	return out
}

// ctxHighConfig returns the live context-high band config (by value — one
// coherent snapshot per call site).
func (s *apiServer) ctxHighConfig() SseContextHighConfig {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ctxhigh
}

// codexNoticeRoundSetting returns the codex FIRST-notice round under the same
// lock as ctxHighConfig — the two are read together on every quiet tick and a
// torn pair would notify against one setting and hand over against the other.
func (s *apiServer) codexNoticeRoundSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.codexNoticeRound
}

// codexCompactionThresholdSetting is its FINAL-round twin, read under the same
// lock for the same reason.
func (s *apiServer) codexCompactionThresholdSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.codexCompactionThreshold
}

// claimHandoverNotice is the once-per-SESSION gate on the advance handover
// notice (T-c382, owner: 「只通知一次」). It returns true exactly once per agent
// session and false every tick after; the caller sends only on true.
//
// 🔴 The key is the gauge's boot_ts, i.e. the SESSION anchor — NOT the
// connection. The distinction is the whole requirement: boot_ts is stamped once
// per session and RESTORED from the durable member row when an SSE stream
// flaps, so a reconnect finds the notice already claimed and stays quiet, while
// a genuinely new session brings a new anchor and is entitled to its own
// notice. Per-connection state would re-nudge on every network blip, which is
// the bombardment this ticket exists to remove — just wearing a different hat,
// and invisible in any test that only ever opens one connection.
//
// Fail-safe: no usable boot_ts (missing gauge, or amnesia after a server
// restart) → refuse to claim, so no notice fires off an anchor we cannot
// recognise again. That errs toward SILENCE, which is the cheap direction here:
// the agent still gets the handover SOP at the handover itself.
func (s *apiServer) claimHandoverNotice(agentID string, record map[string]any) bool {
	bootTS, ok := gaugeBootTS(record)
	if !ok || bootTS <= 0 {
		return false
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.handoverNoticed == nil {
		s.handoverNoticed = map[string]float64{}
	}
	if sent, seen := s.handoverNoticed[agentID]; seen && sent == bootTS {
		return false
	}
	s.handoverNoticed[agentID] = bootTS
	return true
}
