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
	"encoding/json"
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
	displayTheme string
	// avatarSelMu guards the avatar caches below. They are read on every member
	// DTO, so the roster must not issue one query — and one bundle parse — per
	// row. Both are keyed on avatarSelTheme and reload together.
	avatarSelMu sync.RWMutex
	// avatarSelTheme is the theme id the two caches were loaded for. It is
	// compared against the live display.theme, so switching themes reloads
	// rather than serving another theme's choices.
	avatarSelTheme string
	// avatarSelLoaded separates "loaded and empty" from "never loaded". Without
	// it a fleet where nobody has chosen an image reloads on every single row.
	avatarSelLoaded bool
	// avatarSel maps member id -> chosen icon id for avatarSelTheme. A member
	// ABSENT from this map has made no choice, which is what the wire sends as
	// null and what the client renders as the pool's first image.
	avatarSel map[string]string
	// avatarPools holds avatarSelTheme's ordered pools by kind, parsed once.
	avatarPools     map[string][]ThemeIconDTO
	displayLanguage string
	// displayWide is the owner's cockpit layout width (DB display.wide; T-756f)
	// under the SAME dual-layer contract as the two prefs above. false (the
	// default) = the centred ~1040px content column the cockpit ships with; true
	// lifts that cap. NOT an agent read path.
	displayWide bool
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
	// Guarded by handoverNoticedMu below. See claimHandoverNotice for why the
	// key is the session anchor and not the connection.
	handoverNoticed map[string]float64
	// handoverNoticedMu guards the map above. It is its OWN mutex, and the
	// reason is load-bearing: the claim
	// gate now reads and writes SQLite on a cache miss, and holding the shared
	// settings lock across that I/O would stall every unrelated settings reader
	// on a database round-trip. This lock protects one map and nothing else.
	handoverNoticedMu        sync.Mutex
	monitoringRefreshSeconds int
	// acceleratedGraceSecs is the live 加速停止 grace in seconds
	// (stop.accelerated_grace_secs; T-ed79), guarded by settingsMu like every
	// other owner-adjustable number here. NOTHING reads this field directly:
	// the ONE reader is reconcileConfigLive(), which folds it onto
	// reconcileConfig.RecycleGrace so that every clocked cause keeps reaching it
	// through the single recycleGraceFor pair. A second direct reader would be a
	// second opinion about the same number, which is the split T-ed79 removed.
	acceleratedGraceSecs int
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
	// AND the event-driven warden-command dispatch the producer owns (the
	// shadow-deployment kill-switch) while the rest of the server runs
	// unchanged. Not a server-wide gate — see spec/lifecycle.md §4.1.
	//
	// 🔴 SAY THE TRUE THING WHERE THE FALSE ONE STOOD (owner ruling, T-941e
	// 2026-08-18). A SHADOW SERVER WITH THIS FLAG SET STILL COMMANDS REAL
	// WARDENS: the owner-triggered outsource-worker verbs (stop, restart, model
	// change, relocate, refocus), a task terminate that dismisses its workers,
	// and the worker's own report_stopped all reach enqueueToWarden without
	// consulting this field. Pressing stop on a shadow cockpit kills a REAL
	// session. Whoever runs a rehearsal has to know which buttons are live;
	// nothing in the code stops them, and this comment is the only warning
	// there is — the four sentences that used to promise otherwise were the
	// reason nobody looked.
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
	// workerStopLanded (T-ed79 #6) → worker id → the worker_stop a warden
	// ACCEPTED, and when. The sibling above covers the kill that never LEFT;
	// this one covers the kill that left and never took EFFECT. A frame on a
	// warden's FIFO is not a dead session — the drain deletes the whole FIFO
	// before writing it and no ack exists anywhere on that path, so an empty
	// backlog means "collected", not "delivered". The outsource tick judges the
	// entry with the SAME robustStopRetryStep the member cadence uses and
	// re-pushes the STOP while the killed session is still there. In-memory like
	// its siblings: a restart forgets it (a lost retry is a no-op, and the next
	// owner action re-arms).
	workerStopLanded map[string]workerStopDispatch
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
	// workerOfflineSince (T-ed79 #13) → worker id → the ts of the FIRST offline
	// observation in the current continuous-offline run; absent = last seen
	// online. It is the de-bounce anchor for the wind-down collect arms, the
	// worker twin of reconcileState.OfflineSince — kept here rather than on the
	// worker row because it describes what the SERVER has observed, not
	// something the worker did, and a restart must forget it (the next tick
	// re-arms and the worker gets the full window again, which errs toward
	// waiting). See workerOfflineConfirmGraceSecs.
	workerOfflineSince map[string]float64
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
	// Read the success stamp AFTER updateStatus: that call is what resets the
	// cache on a channel flip, so reading second keeps the stamp describing
	// the same state the answer above came from. A failed check never moves
	// it, so `false` + an old/absent stamp reads as "we do not actually know".
	checkedOKAt := s.updateCheckedOKAt()
	writeJSON(w, http.StatusOK, versionDTO{
		Version: appVersion,
		GitSHA:  s.processSHA,
		GitTime: gt,
		// Derived over the non-mcp_exclude route rows (the normative
		// handlers.current_catalog_hash algorithm) — the agent-restart signal.
		CatalogHash:       s.catalogHash,
		UpdateAvailable:   available,
		LatestVersion:     latest,
		UpdateCheckedOKAt: checkedOKAt,
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

// reconcileConfigLive is the reconcile config as it stands RIGHT NOW: the
// boot-time struct with the one owner-adjustable number folded in fresh on
// every read.
//
// 🔴 EVERY read of s.reconcileCfg goes through here, and that is the whole
// mechanism. reconcileConfig is a VALUE, copied at whatever moment it is read,
// so a PATCH that only wrote the field would leave every already-copied config
// quoting the old grace — the clock and the sentence would then disagree about
// members that are mid-wind-down, which is exactly the failure this ticket
// exists to make unreachable. Reading through one function means the deadline
// the cockpit renders, the deadline quoted in the agent's notice and the
// deadline the tick collects on are three reads of the same live number.
//
// It also keeps the PATCH write off s.reconcileCfg itself: that struct is read
// from the cadence goroutine with no lock, and mutating it from an HTTP handler
// would be a data race. The settings snapshot has its own lock and this is the
// only place the two meet.
func (s *apiServer) reconcileConfigLive() reconcileConfig {
	cfg := s.reconcileCfg
	s.settingsMu.RLock()
	grace := s.acceleratedGraceSecs
	s.settingsMu.RUnlock()
	if grace > 0 {
		cfg.RecycleGrace = float64(grace)
	}
	return cfg
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

// avatarPoolKindFor maps a member kind onto the theme pool that serves it.
// Anything else (warden, owner) has no pool and renders its built-in glyph.
func avatarPoolKindFor(kind string) string {
	switch kind {
	case KindAssistant:
		return "member"
	case KindOutsource:
		return "outsource"
	}
	return ""
}

// themeAvatarPools parses one stored bundle into its ordered pools by kind.
// A bundle that no longer parses yields no pools rather than an error: the
// caller's answer for "no pool" is already the built-in glyph, and a theme the
// owner can still see must not make the roster 500.
func themeAvatarPools(bundle string) map[string][]ThemeIconDTO {
	var parsed ThemeBundleDTO
	if err := json.Unmarshal([]byte(bundle), &parsed); err != nil {
		return nil
	}
	if err := normalizeThemeAvatarPools(&parsed, "stored theme"); err != nil {
		return nil
	}
	assignThemeIconIDs(parsed.AvatarPools)
	if parsed.AvatarPools == nil {
		return nil
	}
	return *parsed.AvatarPools
}

// themeIconIDs returns the set of icon ids each stored theme can resolve, as
// theme id -> icon id -> true. The theme write and delete paths prune
// associations against exactly this, so a deleted theme or a removed pool
// image can not leave a selection behind.
//
// ⚠️ IT RETURNS AN ERROR ON PURPOSE. The prune reads this set as the WHOLE
// truth about what is still live, so a theme missing from it is a theme that no
// longer exists and every association row pointing at it is deleted. An empty
// set does not mean "nothing to clean up" — it means "keep nothing", which is
// the entire table. "The read failed" and "there are no themes" must therefore
// never share one value: this used to answer nil for a failed read, which
// turned a single unlucky ListCustomThemes error into a total, silent wipe of
// every member's chosen face — reported as success, because the prune really
// did succeed at deleting everything. A caller that cannot get this set must
// SKIP the prune, never run it against a guess (see PruneMemberThemeAvatars,
// which refuses a nil set outright).
func (s *apiServer) themeIconIDs() (map[string]map[string]bool, error) {
	themes, err := s.dal.ListCustomThemes()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]bool{}
	for _, theme := range themes {
		icons := map[string]bool{}
		for _, pool := range themeAvatarPools(theme.Bundle) {
			for _, icon := range pool {
				if icon.Id != nil {
					icons[*icon.Id] = true
				}
			}
		}
		out[theme.ID] = icons
	}
	return out, nil
}

// themeIconIDsFor returns one theme's resolvable icon ids. Used by the write
// face, which must resolve against the NAMED theme rather than the active one.
func (s *apiServer) themeIconIDsFor(themeID string) map[string]bool {
	theme, err := s.dal.GetCustomTheme(themeID)
	if err != nil || theme == nil {
		return nil
	}
	icons := map[string]bool{}
	for _, pool := range themeAvatarPools(theme.Bundle) {
		for _, icon := range pool {
			if icon.Id != nil {
				icons[*icon.Id] = true
			}
		}
	}
	return icons
}

// invalidateAvatarSelections drops the cached selections and pools. Call it
// after ANY write that can change them: an owner pick, a member removal, or a
// theme write or delete.
func (s *apiServer) invalidateAvatarSelections() {
	s.avatarSelMu.Lock()
	s.avatarSelLoaded = false
	s.avatarSel = nil
	s.avatarPools = nil
	s.avatarSelTheme = ""
	s.avatarSelMu.Unlock()
}

// loadAvatarCaches fills both caches for the ACTIVE theme, once per theme. A
// read error yields empty caches: a missing selection renders the pool's first
// image, which is what a member with no choice already sees, so a transient
// failure degrades to the documented default instead of a 500 on the roster.
func (s *apiServer) loadAvatarCaches() (map[string]string, map[string][]ThemeIconDTO) {
	theme := s.displayThemeSnapshot()
	s.avatarSelMu.RLock()
	if s.avatarSelLoaded && s.avatarSelTheme == theme {
		sel, pools := s.avatarSel, s.avatarPools
		s.avatarSelMu.RUnlock()
		return sel, pools
	}
	s.avatarSelMu.RUnlock()

	sel, err := s.dal.MemberThemeAvatars(theme)
	if err != nil {
		sel = map[string]string{}
	}
	var pools map[string][]ThemeIconDTO
	if stored, err := s.dal.GetCustomTheme(theme); err == nil && stored != nil {
		pools = themeAvatarPools(stored.Bundle)
	}
	s.avatarSelMu.Lock()
	s.avatarSel, s.avatarPools, s.avatarSelTheme, s.avatarSelLoaded = sel, pools, theme, true
	s.avatarSelMu.Unlock()
	return sel, pools
}

// memberAvatarIconID resolves what one member's DTO puts on the wire. It is
// nil in three cases that the client renders identically (the pool's first
// image, or the built-in glyph when the pool is empty): the member never chose
// in this theme, the theme has no pool for its kind, or the image it chose was
// removed from the pool. Only the first case is expected to persist — a pruned
// association is deleted on the theme write path — but resolving against the
// live pool here means a race can not put a dangling id on the wire.
func (s *apiServer) memberAvatarIconID(memberID, kind string) *string {
	poolKind := avatarPoolKindFor(kind)
	if poolKind == "" {
		return nil
	}
	sel, pools := s.loadAvatarCaches()
	iconID, chose := sel[memberID]
	if !chose {
		return nil
	}
	for _, icon := range pools[poolKind] {
		if icon.Id != nil && *icon.Id == iconID {
			return &iconID
		}
	}
	return nil
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
//
// 🔴 T-6ebc — THE MAP IS A CACHE, THE COLUMN IS THE AUTHORITY. A process-local
// map cannot answer "has this session been told?" across a station re-exec: the
// map empties and the AGENTS DO NOT — they reconnect, restore the same anchor,
// and were told the same "this is the ONLY notice you get" a second time, which
// is the sentence this gate exists to keep true. So a map MISS is not an
// answer; it falls through to member.handover_noticed_ts and only a miss THERE
// grants the claim. The map's whole job is to keep the steady state off the
// database: once an agent is in the high band, this runs on every quiet tick
// and every one of those calls after the first is a refusal.
//
// Both stores are written together and cleared together (clearSessionBootTS),
// so the only drift a bug could produce is a map that has forgotten a claim the
// column still holds — and that direction is caught by the read below rather
// than turning into a second notice.
func (s *apiServer) claimHandoverNotice(agentID string, record map[string]any) bool {
	bootTS, ok := gaugeBootTS(record)
	if !ok || bootTS <= 0 {
		return false
	}
	if s.cachedHandoverClaim(agentID) == bootTS {
		return false
	}
	// Cache miss. Ask the durable half before granting anything — and do it
	// WITHOUT holding the lock: this runs on the SSE tick for every agent in the
	// high band, and a database round-trip under a shared lock would serialise
	// them all behind each other.
	if m, err := s.dal.GetMember(agentID); err == nil && m != nil {
		if m.HandoverNoticedTS == bootTS {
			// Already sent for THIS anchor, by a previous process. Warm the
			// cache so the remaining ticks of this high band cost nothing.
			s.rememberHandoverClaim(agentID, bootTS)
			return false
		}
	}
	// A failed read falls through to granting the claim. That is the deliberate
	// direction: the alternative is an agent that silently never gets its one
	// notice because the database hiccuped once, and the notice is what makes it
	// hand over cleanly. A duplicate notice costs a repeated sentence, and a
	// repeated sentence gets reported while silence never does.
	//
	// 🔴 Changing this to fail silent is a JUDGEMENT about which error is
	// cheaper, not a cleanup — it needs a ruling, not a refactor. Guarded by
	// TestHandoverNotice_ADatabaseFailureFallsTowardSending, so the change
	// argues with a test rather than with a comment nobody has to read.
	if !s.rememberHandoverClaim(agentID, bootTS) {
		// Someone else claimed this same anchor while we were reading the
		// database. Exactly one of us may send.
		return false
	}
	if err := s.dal.SetMemberHandoverNoticedTS(agentID, bootTS); err != nil {
		// Non-fatal, but say so: the claim is now cache-only, so the next
		// station re-exec will re-notify this session.
		taskLog("handover notice %s: claim not persisted: %v", agentID, err)
	}
	return true
}

// cachedHandoverClaim reports the anchor this process has already claimed for
// agentID, or 0 when it holds none.
func (s *apiServer) cachedHandoverClaim(agentID string) float64 {
	s.handoverNoticedMu.Lock()
	defer s.handoverNoticedMu.Unlock()
	return s.handoverNoticed[agentID]
}

// rememberHandoverClaim records agentID's claim on bootTS and reports whether
// THIS caller is the one that took it. A false means someone else got there
// first while the caller was off reading the database — the double-check that
// keeps "exactly once" true when two ticks race through the cache miss.
func (s *apiServer) rememberHandoverClaim(agentID string, bootTS float64) bool {
	s.handoverNoticedMu.Lock()
	defer s.handoverNoticedMu.Unlock()
	if s.handoverNoticed == nil {
		s.handoverNoticed = map[string]float64{}
	}
	// Deleting this branch makes every racing caller a winner — guarded by
	// TestHandoverNotice_TwoRacingClaimsOnlyOneSends, which is why this
	// function returns a bool rather than nothing.
	if s.handoverNoticed[agentID] == bootTS {
		return false
	}
	s.handoverNoticed[agentID] = bootTS
	return true
}

// handoverNoticeSettled reports that THIS tick cannot possibly emit the
// once-per-session handover notice, using only reads that cost nothing: the
// gauge record already in hand and the process-local claim cache.
//
// 🔴 WHY IT EXISTS — MEASURED, NOT ASSUMED. Once an agent crosses its notice
// point, decideHandoverNotice returns a signal on EVERY quiet tick (250ms) for
// the rest of the session; the once-per-session gate is claimHandoverNotice,
// which runs AFTER the signal (and therefore after its offboard / doc-capacity
// closures) has already been composed. So the "fires once" fact never bounded
// the COST of composing it. The independent review measured the branch before
// this guard at 21.3µs → 574.2µs per tick once the doc-capacity closure joined
// the offboard one (26.9×). Re-measured here on this fix, silent tick vs silent
// tick, 400 ticks each: 246ns guarded vs 374µs unguarded on an EMPTY station,
// 199ns vs 701µs with all nine documents near their caps. Calling this FIRST is
// what makes the cost match the frequency.
//
// It is deliberately the CACHE ONLY, never the durable column: a cache HIT is
// a definitive "this process already sent it", so short-circuiting on it can
// only skip ticks that claimHandoverNotice would have refused anyway. A cache
// MISS still falls all the way through to claimHandoverNotice, which is the
// half that consults member.handover_noticed_ts — so the T-6ebc rule (the map
// is a cache, the column is the authority) is untouched.
//
// No usable anchor is also settled: claimHandoverNotice refuses a record with
// no boot_ts, so composing a signal for one could never have emitted either.
func (s *apiServer) handoverNoticeSettled(agentID string, record map[string]any) bool {
	bootTS, ok := gaugeBootTS(record)
	if !ok || bootTS <= 0 {
		return true
	}
	return s.cachedHandoverClaim(agentID) == bootTS
}
