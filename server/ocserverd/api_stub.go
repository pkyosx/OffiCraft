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
	"sync/atomic"
	"time"

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
	// keys is the LIVE signing-key ring (keyring.go). It is a POINTER on
	// purpose: buildHandler hands this same ring to every gated route, so a
	// rotation is visible to every signer and verifier in the process without a
	// restart. Read through keys.signingSecret() per mint — never cache the
	// []byte it returns across requests.
	keys *keyring
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
	// totpSecret is the owner's ACTIVE TOTP secret ("" = MFA off). Its presence
	// is what makes /api/login demand a second factor; totpLastStep is the
	// replay floor (the highest step already spent). Both live under settingsMu
	// with the rest of the auth snapshot — login reads them on every attempt.
	// mfaOffered is the ship-dark feature flag (settings.go). It gates whether the
	// factor can be SET UP; it is deliberately absent from every verification
	// path, so withdrawing the feature can never disarm a live factor.
	mfaOffered   bool
	totpSecret   string
	totpLastStep int64
	// loginThrottle is the in-flight gate shared by every credential-guessing
	// seam (throttle.go). In-memory and process-local: it holds no failure
	// history at all, only how many verifications are running right now.
	loginThrottle credentialThrottle
	// credentialFailureFloor overrides throttleFailureFloor for THIS server.
	// Zero — the production value — means the constant; only this package's own
	// tests ever set it, so that a test which is not about the floor does not
	// pay three seconds per refusal to walk past it. See failureFloor().
	credentialFailureFloor time.Duration
	// authAlert* throttle the 「password accepted, second factor refused」 alert
	// to the assistant: at most one message per authAlertInterval, carrying the
	// number of attempts folded into it. See noteFactorRefusedAfterCorrectPassword.
	authAlertMu      sync.Mutex
	authAlertLastAt  time.Time
	authAlertPending int
	// authAlertDeliver replaces the delivery step. nil — the production value —
	// means deliverPasswordExposedAlert. It exists so a test can prove the
	// dispatch is asynchronous; see dispatchAuthAlert.
	authAlertDeliver func(count int)
	ownerTokenTTL    int64
	agentTokenTTL    int64
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
	// serving both runtimes. The 〈停止〉 document (T-c9c0) joins them with its
	// own knob, doc.cap_chars.offboard.
	docCapCharsSystemInteraction int
	docCapCharsBootSequence      int
	docCapCharsOffboard          int
	// chatBudgetChars is the live budget of the wake snapshot's chat block (DB
	// chat.budget_chars; T-c9b4). Read through chatBudget() by
	// resumeSnapshotParts — the ONE place the number enters the packer, which is
	// why resume_summary and peek_resume_summary_size cannot disagree about it.
	chatBudgetChars int
	// backupRetain is N — how many database backup files rotation keeps PER POOL
	// (DB backup.retain; T-8). This copy exists for the COCKPIT FACE only: GET
	// /api/settings shows it and PATCH moves it.
	//
	// 🔴 It is NOT what rotation reads. backup.go holds no apiServer by design
	// and reads the row itself (liveBackupRetain) at snapshot time, so this
	// field being stale or wrong cannot change how many files get deleted — it
	// can only make the settings page lie. Both sides are bounded by
	// minBackupRetain/maxBackupRetain, which is what keeps them honest.
	backupRetain int
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
	// loreEnabled is the station-wide LORE feature switch (DB lore.enabled;
	// T-33), default false. 🔴 EVERY LORE-FACING PATH READS IT THROUGH
	// loreEnabledSnapshot() ON EVERY CALL rather than capturing it once, because
	// the owner was promised that flipping the switch applies immediately:
	// 「你一開，他們當下就寫得進去」. The ONE place that cannot honour that is a
	// boot context — it is assembled once at wake, so an already-booted agent
	// keeps the document it was handed until it boots again.
	loreEnabled bool
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
	handoverNoticedMu sync.Mutex
	// ctxGateDiagAt records, per actor id, WHEN stampContextHighRecycle last
	// emitted its gate diagnostic for that actor AND WHICH gate it named — the
	// throttle behind noteContextGateSkip (T-72dd 補觀測). Guarded by
	// ctxGateDiagMu below; pruned on the session boundary by clearSessionBootTS,
	// the same place handoverNoticed is pruned and for the same reason.
	ctxGateDiagAt map[string]ctxGateDiagState
	// ctxGateDiagMu guards the map above, and it is its OWN mutex for the same
	// reason handoverNoticedMu is: it protects one map on the reconcile tick's
	// hot path and must never make an unrelated reader wait.
	ctxGateDiagMu            sync.Mutex
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
	// reconcileMu serializes the RECONCILE HALF of the 30s cadence tick
	// (runReconcileTick, called first by runLifecycleTick — lifecycle_tick.go)
	// with any event-driven immediate tick (the Python per-app tick lock) AND
	// guards the store. It is still never held at the same time as outsourceMu:
	// the merged tick takes this lock, drops it, and only then enters the
	// outsource half.
	reconcileMu sync.Mutex
	// reconcileStates is the in-memory per-member bookkeeping — restart
	// amnesia is contract (the next tick re-decides from presence).
	reconcileStates map[string]reconcileState
	reconcileCfg    reconcileConfig
	// noReconcile is the --no-reconcile serve flag: skips the RECONCILE HALF of
	// the cadence tick AND disables the event-driven warden-command dispatch the
	// producer owns (the shadow-deployment kill-switch) while the rest of the
	// server runs unchanged. Since T-14 item 5 the cadence LOOP is mounted
	// either way — the flag is read at the call site, runLifecycleTick — so this
	// no longer means "no goroutine", it means "that half does no work". Not a
	// server-wide gate — see spec/lifecycle.md §4.1.
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
	// outsourceMu serializes the OUTSOURCE HALF of the 30s cadence tick
	// (runOutsourceTick, called second by runLifecycleTick — lifecycle_tick.go)
	// with the event-driven create_task tick. There is no in-memory ledger to
	// guard — the outsource_worker rows are the bookkeeping (every tick
	// recounts). Never held together with reconcileMu: the merged tick has
	// dropped that lock before this half is entered.
	outsourceMu sync.Mutex
	// noOutsource is the --no-outsource serve flag: skips the OUTSOURCE HALF of
	// the cadence tick AND disables the event-driven create_task tick — the
	// --no-reconcile mirror for the outsource-assignment producer.
	//
	// 🔴 WHERE THIS FIELD IS READ IS LOAD-BEARING. It is read at the call site
	// (runLifecycleTick) and in outsourceTickNow, and deliberately NOT inside
	// runOutsourceTick: 169 test sites across 34 files set it to true and then
	// drive the scheduler by hand, so a read inside the tick body would turn
	// them into silent no-ops. lifecycle_tick.go carries the full ruling and the
	// commands behind those counts; lifecycle_tick_test.go pins it both ways.
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
	// stationShuttingDown is the process-lifecycle marker used by the SSE
	// detach log. A peer cancellation and a station shutdown both surface as
	// request-context cancellation, so the marker must be set at the server
	// boundary before that context is cancelled. stationCancel is wired by
	// cmdServe for signal shutdown; tests without a live server still get the
	// marker-only seam.
	stationShuttingDown atomic.Bool
	stationCancel       func()
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

// authMFAOffered reports whether this server offers the second factor for
// SET-UP. Never consult it when deciding whether to VERIFY a code — that is
// driven by the presence of a secret and nothing else.
func (s *apiServer) authMFAOffered() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.mfaOffered
}

// authMFAEnrolled reports whether the second factor is armed.
//
// 🔴 There is deliberately NO read-only accessor that hands out the secret and
// the replay floor together for a caller to verify against. Verification MUST
// advance the floor, and a read-then-write pair is a replay window: two
// concurrent logins presenting the SAME code would both read the old floor and
// both pass, which is precisely the attack the floor exists to stop. The verify
// and the spend live in one write-locked seam instead — verifyAndSpendTOTP
// (api_auth_mfa.go).
func (s *apiServer) authMFAEnrolled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.totpSecret != ""
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

// taskEventCap is the ceiling on each of the four task-event procedures
// (T-3201). No lock and no settings read: it is a constant until the owner has
// seen the interface change that turning it into a `doc.cap_chars.*` setting
// would be (see taskEventCapCharsDefault). The accessor exists anyway so the
// registry reads caps through ONE shape and the day it does become a setting
// costs one function body, not nine call sites.
func (s *apiServer) taskEventCap() int {
	return taskEventCapCharsDefault
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

// backupRetainSetting is the live cockpit view of N (backup.retain; T-8).
// Read at request time like the caps above, so a PATCH shows up on the next GET
// with no restart. See the field for why rotation does not go through here.
func (s *apiServer) backupRetainSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.backupRetain
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

// loreEnabledSnapshot returns the LIVE station-wide lore switch (lore.enabled;
// T-33). false (the default) = the lore feature does not exist for an agent:
// no route answers, no boot-context directory, no cockpit tab.
//
// 🔴 CALL IT PER REQUEST, NEVER ONCE AT BOOT. PATCH /api/settings writes the DB
// and then updates this field under the same settingsMu, so a read taken here
// is the value as of this call — which is the whole reason the owner can turn
// the feature on and have the next write land.
func (s *apiServer) loreEnabledSnapshot() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.loreEnabled
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
