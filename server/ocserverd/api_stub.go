package main

import (
	"io/fs"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"ocserverd/txguard"
)

type apiServer struct {
	processSHA  string
	processTime string
	catalogHash string
	dal         *DAL

	pushHTTPClient webpush.HTTPClient

	webPushSink func(payload webPushPayload)
	hub         *Hub
	telemetry   *memStore
	gauge       *memStore

	machineClaims *machineClaimStore
	runtimeLogins *runtimeLoginStore
	// keys is a POINTER on purpose: every gated route shares this one ring, so a
	// rotation is visible process-wide. Read keys.signingSecret() per mint —
	// never cache the []byte it returns across requests.
	keys *keyring
	// tokenKeyObs keeps work done on each authenticated request off the
	// one-connection write pool and the warden command queue; not durable because
	// losing it costs at most one redundant write and one renew per machine.
	tokenKeyObsMu txguard.Mutex
	tokenKeyObs   map[string]tokenKeyObservation

	keyRenewClock func() time.Time
	// settingsMu guards the LIVE settings fields below: owner endpoints update
	// them IN PLACE while the SSE loop and reconcile cadence read concurrently —
	// read through the accessors, never the bare fields. A writer holds it only
	// to apply values that have already committed (applySettings), never across
	// a DB write: every request's auth gate reads these fields.
	settingsMu txguard.RWMutex
	// settingsWriteMu serialises every writer of the fields below for its whole
	// body: decide, write, then apply. It is what keeps a TOTP code single-use
	// (verify and spend under one hold), what bounds change-password's argon2id
	// to one at a time, and what keeps two writers' applies in commit order. A
	// writer may read the fields while holding it without settingsMu, because
	// no field changes except under both.
	settingsWriteMu txguard.Mutex
	// passwordHash "" = not set: every login is denied until one is written.
	passwordHash string
	// passwordChangedAt is the owner-session revocation cut: owner-scope tokens
	// with iat before it are refused at the auth gate.
	passwordChangedAt int64
	// totpSecret "" = MFA off; totpLastStep is the replay floor (highest step
	// already spent).
	mfaOffered   bool
	totpSecret   string
	totpLastStep int64

	loginThrottle credentialThrottle

	credentialFailureFloor time.Duration

	authAlertMu      txguard.Mutex
	authAlertLastAt  time.Time
	authAlertPending int

	authAlertDeliver func(count int)
	ownerTokenTTL    int64
	agentTokenTTL    int64

	outsourceMaxParallel int

	docCapCharsDuty      int
	docCapCharsInsight   int
	docCapCharsManualSop int

	docCapCharsSystemInteraction int
	docCapCharsBootSequence      int
	docCapCharsOffboard          int

	loreCapCharsRole   int
	loreCapCharsManual int
	loreCapCharsTitle  int
	loreCapCharsBody   int

	chatBudgetChars int

	stepNoteCapChars int
	// backupRetain is the COCKPIT copy only (GET/PATCH /api/settings). Rotation
	// does NOT read it — backup.go reads the row itself — so a stale value here
	// can only make the settings page lie.
	backupRetain int

	updaterReceiveBeta bool
	updaterAutoUpdate  bool

	orgName string

	ownerName string

	pushContactEmail string

	displayTheme    string
	displayLanguage string

	displayWide bool
	// suggestedReplies* are REPLACED wholesale on a patch, never mutated in place,
	// so a reader holding a slice under settingsMu can keep it.
	suggestedRepliesReplyCard   []string
	suggestedRepliesTaskMessage []string
	suggestedRepliesLoreMessage []string

	selfBase string
	// namespace ("" = main instance) leaves the server on exactly three surfaces:
	// the install.sh line, the bootstrap/teardown-here child env (OC_NAMESPACE),
	// and terminal_attach_command, which names the `tmux -L` socket.
	namespace string

	ctxHigh                  SseContextHighConfig
	codexCompactionThreshold int // the FINAL round (handover)
	codexNoticeRound         int // the FIRST, soft notice round

	// handoverNoticed: agent id → the boot_ts whose notice was claimed.
	handoverNoticed sync.Map

	startClearedAnchors map[string]sessionAnchorSnapshot
	// startClearedAnchorsMu is held for the whole of clearSessionBootTS,
	// clearSessionBootTSForStart and restoreRefusedStartAnchor. Their callers may
	// hold outsourceMu or reconcileMu; under this lock only the gauge's leaf lock
	// is taken, plus DAL calls.
	startClearedAnchorsMu txguard.Mutex

	// ctxGateDiagLast: actor id → ctxGateDiagState.
	ctxGateDiagLast sync.Map

	monitoringRefreshSeconds int
	// acceleratedGraceSecs is read ONLY through reconcileConfigLive().
	acceleratedGraceSecs int

	reassignHandoverTimeoutSecs     int
	runtimeLoginCheckIntervalSecs   int
	runtimeLoginRecheckIntervalSecs int
	// wardenCredLifetimeSecs: mintWardenToken stamps exp = iat + this. An exp is
	// fixed at mint time, so lowering it shortens only future credentials.
	wardenCredLifetimeSecs int

	root assetRoot

	binHashes map[string]string

	backupHealth *backupHealthMonitor

	binCacheDir string
	// ocwardenFS is a test seam (nil in production → bindistFS()). The EXEC seam
	// is the package-level runOcwarden var — do not add a second one here.
	ocwardenFS fs.FS

	mcpTools map[string]RouteSpec
	// loopback: tools/call re-enters this mux in-process so the auth gate, RBAC
	// choke and param binding run exactly as for a direct REST call.
	loopback http.Handler
	// reconcileMu is never held at the same time as outsourceMu: the merged tick
	// takes it, drops it, and only then enters the outsource half.
	reconcileMu txguard.Mutex

	// reconcileStates: member id → reconcileState. Each access is one Load, Store
	// or Delete; a read-modify-write of one entry is serialised by the caller's
	// reconcileMu (staff) or outsourceMu (workers), never by this map.
	reconcileStates sync.Map
	reconcileCfg    reconcileConfig
	// noReconcile (--no-reconcile) skips the RECONCILE HALF of the cadence tick
	// and the producer's event-driven dispatch; not a server-wide gate (see
	// spec/lifecycle.md §4.1). Owner ruling T-941e: A SHADOW SERVER WITH THIS FLAG
	// SET STILL COMMANDS REAL WARDENS — the owner-triggered outsource-worker verbs,
	// a task terminate that dismisses its workers, and report_stopped reach
	// enqueueToWarden without consulting this field. Pressing stop on a shadow
	// cockpit kills a REAL session; this comment is the only warning there is.
	noReconcile bool
	// identitySweepAt is guarded by reconcileMu.
	identitySweepAt map[string]float64
	// receiptPending has its OWN mutex, never reconcileMu/outsourceMu: it is armed
	// from both producers and disarmed from the telemetry ingest goroutine.
	receiptMu      txguard.Mutex
	receiptPending map[string]pendingReceipt

	outsourceMu txguard.Mutex
	// noOutsource is read at the call site (runLifecycleTick) and in
	// outsourceTickNow, deliberately NOT inside runOutsourceTick: tests set it and
	// then drive the scheduler by hand, so a read inside the tick body would turn
	// them into silent no-ops.
	noOutsource bool
	// These three maps live under outsourceMu.
	workerSpawnAt     map[string]float64
	workerSpawnTarget map[string]string
	workerReclaimed   map[string]bool

	workerSpawnAttempts map[string]int
	// workerStopPending: the fail-closed dispatch gate drops a STOP toward an
	// unreachable warden, so a live-worker kill is parked here and re-fired by the
	// scheduler tick until the target drains it.
	workerStopPending map[string]string
	// workerStopLanded: a frame on a warden's FIFO is not a dead session — the
	// drain deletes the FIFO before writing it and no ack exists, so an empty
	// backlog means "collected", not "delivered". The outsource tick re-pushes the
	// STOP while the killed session is still there.
	workerStopLanded map[string]workerStopDispatch

	workerMachinePref map[string]string
	// workerMachineBench: "<worker id>|<machine id>" → cooldown-until ts.
	workerMachineBench map[string]float64

	workerTakeoverBench map[string]takeoverBench

	workerTakeoverLiftedAt map[string]float64

	// offlineConfirmSince: member/worker id → start of its current offline run
	// (sessionConfirmedGone). A sync.Map because both ticks and the SSE connect
	// edge touch it under different locks or none.
	offlineConfirmSince sync.Map

	updateMu    txguard.Mutex
	updateCheck updateCheckState

	releaseAPIBase string

	upgradeMu txguard.Mutex

	upgradeExeOverride string
	upgradeRestart     func(exePath string)
	// stationShuttingDown: a peer cancellation and a station shutdown both
	// surface as request-context cancellation, so it must be set at the server
	// boundary BEFORE that context is cancelled.
	stationShuttingDown atomic.Bool
	stationCancel       func()
}

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
	available, latest := s.updateStatus()
	// Read AFTER updateStatus: that call resets the cache on a channel flip, so
	// the stamp then describes the same state as the answer above.
	checkedOKAt := s.updateCheckedOKAt()
	writeJSON(w, http.StatusOK, versionDTO{
		Version: appVersion,
		GitSHA:  s.processSHA,
		GitTime: gt,
		// The agent-restart signal.
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

var _ ServerInterface = (*apiServer)(nil)

// applySettings moves the in-memory snapshot after its DB write committed. The
// caller holds settingsWriteMu.
func (s *apiServer) applySettings(apply func()) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	apply()
}

func (s *apiServer) authPasswordHash() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.passwordHash
}

// authMFAOffered gates SET-UP only. Never consult it when deciding whether to
// VERIFY a code, so withdrawing the feature can never disarm a live factor.
func (s *apiServer) authMFAOffered() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.mfaOffered
}

// 🔴 There is deliberately NO read-only accessor handing out the secret and the
// replay floor together: a read-then-write pair lets two concurrent logins with
// the SAME code both pass. Verify and spend live in one seam under settingsWriteMu,
// verifyAndSpendTOTP.
func (s *apiServer) authMFAEnrolled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.totpSecret != ""
}

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

func (s *apiServer) wardenCredLifetimeValue() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	if s.wardenCredLifetimeSecs <= 0 {
		return wardenCredLifetimeSecsDefault
	}
	return s.wardenCredLifetimeSecs
}

// reconcileConfigLive: EVERY read of s.reconcileCfg goes through here.
// reconcileConfig is a VALUE copied when read, so a PATCH that wrote the field
// would leave already-copied configs quoting the old grace; and the cadence
// goroutine reads s.reconcileCfg with no lock, so writing it from a handler would
// be a data race.
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

func (s *apiServer) reassignHandoverTimeout() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.reassignHandoverTimeoutSecs
}

func (s *apiServer) runtimeLoginCheckInterval() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.runtimeLoginCheckIntervalSecs
}

func (s *apiServer) runtimeLoginRecheckInterval() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.runtimeLoginRecheckIntervalSecs
}

func (s *apiServer) outsourceParallelCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.outsourceMaxParallel
}

// One accessor per segment and no generic docCap(segment) on purpose: a
// parameter would let a call site pass the wrong segment and still compile.
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

func (s *apiServer) manualSopCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.docCapCharsManualSop
}

// bootSequenceCap is ONE budget serving BOTH runtimes' documents.
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

// taskEventCap is deliberately a constant, not a setting, until the owner has
// seen that interface change (see taskEventCapCharsDefault).
func (s *apiServer) taskEventCap() int {
	return taskEventCapCharsDefault
}

// ⚠️ loreRoleCap IS THE MEMBER BUDGET (scopes collapsed to two, rc-a43100fd0486
// [0]); the name and its settings key stay because renaming a live key is the
// owner's call. It and loreManualCap are independent and NEVER ADDED.
func (s *apiServer) loreRoleCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.loreCapCharsRole
}

func (s *apiServer) loreManualCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.loreCapCharsManual
}

// loreTitleCap / loreBodyCap, unlike the doc caps, may be lowered: an entry has
// no edit path, so a smaller cap can never strand a stored one.
func (s *apiServer) loreTitleCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.loreCapCharsTitle
}

func (s *apiServer) loreBodyCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.loreCapCharsBody
}

// chatBudget has exactly ONE caller, resumeSnapshotParts, which assembles the
// resume-summary faces and the size peek alike; a second call site is how they
// start disagreeing.
func (s *apiServer) chatBudget() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.chatBudgetChars
}

// stepNoteCap is the ONLY source of that number: both write faces measure
// against it and every read face reports it, so a second literal on either side
// is how they start disagreeing.
func (s *apiServer) stepNoteCap() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.stepNoteCapChars
}

func (s *apiServer) backupRetainSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.backupRetain
}

func (s *apiServer) orgNameSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.orgName
}

func (s *apiServer) ownerNameSnapshot() string {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ownerName
}

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

func (s *apiServer) displayWideSnapshot() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.displayWide
}

func (s *apiServer) ctxHighConfig() SseContextHighConfig {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.ctxHigh
}

func (s *apiServer) codexNoticeRoundSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.codexNoticeRound
}

func (s *apiServer) codexCompactionThresholdSetting() int {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.codexCompactionThreshold
}

// claimHandoverNotice: true exactly once per agent SESSION (owner: 「只通知一次」,
// T-c382). The key is the gauge's boot_ts — the session anchor, restored from the
// member row when an SSE stream flaps — NOT the connection, or every reconnect
// would re-nudge.
//
// The map is a CACHE; member.handover_noticed_ts is the AUTHORITY: a station
// re-exec empties the map while agents reconnect with the same anchor, so a map
// miss falls through to the column.
func (s *apiServer) claimHandoverNotice(agentID string, record map[string]any) bool {
	bootTS, ok := gaugeBootTS(record)
	if !ok || bootTS <= 0 {
		return false
	}
	if s.cachedHandoverClaim(agentID) == bootTS {
		return false
	}
	if m, err := s.dal.GetMember(agentID); err == nil && m != nil {
		if m.HandoverNoticedTS == bootTS {
			s.rememberHandoverClaim(agentID, bootTS)
			return false
		}
	}
	// A failed read falls through to GRANTING the claim: a duplicate notice gets
	// reported, silence never does. Changing this to fail silent needs a ruling,
	// not a refactor.
	if !s.rememberHandoverClaim(agentID, bootTS) {
		return false
	}
	if err := s.dal.SetMemberHandoverNoticedTS(agentID, bootTS); err != nil {
		taskLog("handover notice %s: claim not persisted: %v", agentID, err)
	}
	return true
}

func (s *apiServer) cachedHandoverClaim(agentID string) float64 {
	if v, ok := s.handoverNoticed.Load(agentID); ok {
		return v.(float64)
	}
	return 0
}

// rememberHandoverClaim is true for exactly one of any number of concurrent
// callers claiming the same bootTS: the Swap that replaced something else.
func (s *apiServer) rememberHandoverClaim(agentID string, bootTS float64) bool {
	prev, loaded := s.handoverNoticed.Swap(agentID, bootTS)
	return !loaded || prev.(float64) != bootTS
}

// handoverNoticeSettled is called FIRST, before the signal is composed: once
// past its notice point an agent yields a signal on every quiet tick, and
// composing it cost 374µs vs 246ns guarded (measured, empty station). It reads
// the CACHE only, so it can only skip ticks claimHandoverNotice would refuse.
func (s *apiServer) handoverNoticeSettled(agentID string, record map[string]any) bool {
	bootTS, ok := gaugeBootTS(record)
	if !ok || bootTS <= 0 {
		return true
	}
	return s.cachedHandoverClaim(agentID) == bootTS
}
