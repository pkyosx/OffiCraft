package main

// reconcile.go — the server-reconcile producer (spec/lifecycle.md §4):
//
//   - reconcileDecide: the pure per-member state machine, desired_state × observed-online → the
//     one command to dispatch (or none).
//   - the in-memory reconcile store: restart amnesia IS the contract — a lost store just resets the
//     dedupe/grace windows and the next tick re-decides from presence.
//   - dispatch: fail-closed behind the target-reachability gate (the addressed warden must itself
//     hold the live SSE downstream) and the START payload fold+mint. A refused dispatch keeps the
//     PRIOR state so the next tick retries.
//   - runReconcileTick is the reconcile half of the 30s cadence (startLifecycleCadence,
//     lifecycle_tick.go); reconcileMemberNow is the event-driven click seam, sharing the same
//     store + mutex so the cadence stays an idempotent backstop.
//
// --no-reconcile (serve flag) disables the producer wholesale — the cadence loop and every
// event-driven warden-command dispatch it owns; the paths it does NOT cover are enumerated in
// spec/lifecycle.md §4.1.

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// spec/lifecycle.md §4.4 — the defaults are contract.
type reconcileConfig struct {
	StartTimeout float64
	StopGrace    float64
	StopRetry    float64
	RecycleGrace float64
	// SoftOffboardGrace is NOT a deadline: neither soft arm is collected on a clock — 下線
	// (rc-27d1710174dd) and 重新聚焦 (rc-c540367065ad) end only at the agent's own stopped report
	// or the owner's force-stop. It is how long a close-out may say nothing before its anchor is
	// treated as residue (clearStaleStoppingOnOnline). 0 restores the timed wind-down, which is what
	// the robust-stop ladder tests drive.
	SoftOffboardGrace float64
	BackoffBase       float64
	BackoffCap        float64
	CircuitThreshold  int
	CircuitCooldown   float64
	// ZombieConfirmGrace: a START that bounced off the warden clobber-guard cannot tell a
	// presence-deaf session from one reconnecting through a network blip (a takeover STOP once raced
	// a session seconds from reconnecting), so the takeover STOP waits until the member has been
	// continuously offline this long. 2×StartTimeout covers the agent's worst honest reconnect
	// (backoff cap 15s + 45s idle-read watchdog + one 30s tick ≈ 90s, all fixed, independent of
	// StartTimeout) with a full START window of slack.
	ZombieConfirmGrace float64
}

func defaultReconcileConfig() reconcileConfig {
	return reconcileConfig{
		StartTimeout:       WakingTTLSecs,
		StopGrace:          StoppingTimeoutSecs,
		StopRetry:          90.0,
		RecycleGrace:       StoppingTimeoutSecs,
		SoftOffboardGrace:  SoftOffboardGraceSecs,
		BackoffBase:        5.0,
		BackoffCap:         300.0,
		CircuitThreshold:   5,
		CircuitCooldown:    120.0,
		ZombieConfirmGrace: 2 * WakingTTLSecs,
	}
}

// Warden command verbs. STOP is the single ROBUST stop — the warden self-escalates the kill.
const (
	reconcileCmdNone      = "none"
	reconcileCmdStart     = "start"
	reconcileCmdStop      = "stop"
	reconcileCmdUninstall = "uninstall"
	// reconcileCmdUpdate is NOT a reconcile decision: the owner's one-shot self-update verb,
	// dispatched from POST /api/machines/{id}/upgrade (spec/sse.md §7).
	reconcileCmdUpdate = "update"
	// reconcileCmdRenew is NOT a reconcile decision: it tells ONE machine to renew its credential.
	// 🔴 Its args are member_id-only (wardenTargetArgs) by owner ruling A — never put a credential
	// in them.
	reconcileCmdRenew = "renew"
)

const (
	// stopKindRecycle: the session is expected back on the SAME machine — never bench it.
	stopKindRecycle = "recycle"
	// stopKindZombieTakeover: the only kind that justifies benching the machine (slot wedged).
	stopKindZombieTakeover = "zombie_takeover"
	stopKindRelocate       = "relocate"
	stopKindWinddown       = "winddown"
	stopKindRobustResend   = "robust_resend"
)

// spawnClobberReasonPrefix must match the warden SpawnOutcome.Reason prefix
// (cli/ocwarden/spawn.go start clobber-guard) folded onto member.last_op_reason.
const spawnClobberReasonPrefix = "session_already_exists"

const (
	reconcilePhaseOffline     = "offline"
	reconcilePhaseStarting    = "starting"
	reconcilePhaseOnline      = "online"
	reconcilePhaseBackoff     = "backoff"
	reconcilePhaseCircuitOpen = "circuit_open"
	reconcilePhaseStopping    = "stopping"
)

func parseDesired(raw string) string {
	switch raw {
	case DesiredStateOnline, DesiredStateUninstall:
		return raw
	}
	return DesiredStateOffline
}

type reconcileState struct {
	Phase                string
	Attempts             int
	BackoffUntil         float64
	CircuitOpen          bool
	CircuitCooldownUntil float64
	LastCommand          string
	LastCommandAt        float64
	StopDeadline         float64
	// RobustStopPendingAt: when dispatchRobustStopNow last sent an out-of-band robust STOP
	// (force-stop, cancel-wake kill, report_stopped collect); 0 = none outstanding. That send is
	// dropped on an unreachable warden and no decide arm re-derives it, so this is its only retry.
	// 🔴 Do not re-derive it from stopped_since: 下線 → 活化 leaves a predecessor's stopped_since on
	// a fresh session, which would then be robust-stopped on its first tick.
	RobustStopPendingAt float64
	// OfflineSince feeds the zombie-takeover second-confirmation window ONLY. Restart amnesia
	// re-arms the window from zero, again the safe direction.
	OfflineSince float64
}

func newReconcileState() reconcileState {
	return reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone}
}

func (s *apiServer) reconcileStateOf(memberID string) reconcileState {
	v, ok := s.reconcileStates.Load(memberID)
	if !ok {
		return newReconcileState()
	}
	return v.(reconcileState)
}

func (s *apiServer) setReconcileState(memberID string, st reconcileState) {
	s.reconcileStates.Store(memberID, st)
}

func (s *apiServer) dropReconcileState(memberID string) {
	s.reconcileStates.Delete(memberID)
}

type memberObservation struct {
	MemberID     string
	Desired      string
	Online       bool
	RefocusSince float64
	RefocusOp    string
	// StoppingSince (member.stopping_since) anchors the owner-pressed 加速停止 grace. It must be the
	// durable row, not in-memory st.StopDeadline: the agent's notice is composed from the row, and
	// the clock must agree with it across a station re-exec.
	StoppingSince float64
	AgentStopped  bool
	LastOpKind    string
	LastOpReason  string
	// RunningMachine is the SSE machine claim (hub.MachineOf) — the desired_machine baked into the
	// boot token at spawn — and "" for a claim-less boot.
	TargetMachine   string
	RunningMachine  string
	HandoverArmable bool
}

type reconcileDecision struct {
	Command  string
	MemberID string
	Reason   string
	State    reconcileState
	// DispatchWarden routes a STOP to the warden where the session actually runs; "" routes via
	// wardenTargetOf (the desired machine). Sending a relocation STOP to the new machine's warden
	// would no-op forever — only the warden holding the session can kill it.
	DispatchWarden string
	// DispatchUnlanded: a command was decided but the warden was unreachable, so it was downgraded
	// to a no-op; the relocate handler reports it instead of a silent 200.
	DispatchUnlanded bool
	StartTimedOut    bool
	// ConvergedOnline: set only by decideUp's converged arm; read by reconcileTickMemberLocked and
	// reconcileWorkerLiveness to clear a failure receipt. decideDown / decideUninstall deliberately
	// never set it: the owner's ruling 「他回來了」 covers only the online direction.
	ConvergedOnline bool
	// StopKind names which STOP this is, so callers (reconcileWorkerLiveness) read it instead of
	// re-deriving the decider's branch conditions.
	StopKind string
	// ReasonCode: an owner-visible stall explanation (spawnReason* / placementReason* code), stamped
	// on the row. "" means nothing is owed — stamping converged states would fan a delta every tick.
	ReasonCode    string
	ArmHandoverOp string
}

func decisionNone(obs memberObservation, st reconcileState, reason string) reconcileDecision {
	return reconcileDecision{
		Command: reconcileCmdNone, MemberID: obs.MemberID, Reason: reason, State: st,
	}
}

// robustStopStep is the at-least-once judgment for one armed out-of-band robust STOP, shared by
// the member producer (RobustStopPendingAt, top of reconcileDecide) and the worker producer
// (workerStopLanded, retryUnlandedWorkerStop) — do not fork a private copy.
type robustStopStep int

const (
	robustStopDone robustStopStep = iota
	robustStopWait
	robustStopResend
)

// alive is the caller's evidence the session THIS STOP aimed at still runs — the worker producer
// narrows it to online AND still on the addressed machine, since a respawn reuses the id.
func robustStopRetryStep(dispatchedAt float64, alive bool, stopRetry, now float64) robustStopStep {
	switch {
	case dispatchedAt <= 0.0 || !alive:
		return robustStopDone
	case now-dispatchedAt >= stopRetry:
		return robustStopResend
	default:
		return robustStopWait
	}
}

func reconcileDecide(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	if st.CircuitOpen && now >= st.CircuitCooldownUntil {
		st.CircuitOpen = false
		st.Attempts = 0
		st.BackoffUntil = 0.0
	}
	// Before the desired-state switch: the member is being collected whichever arm would own it,
	// and decideUp's converged arm would otherwise wipe the stop bookkeeping.
	if st.RobustStopPendingAt > 0.0 {
		switch robustStopRetryStep(st.RobustStopPendingAt, obs.Online, cfg.StopRetry, now) {
		case robustStopDone:
			st.RobustStopPendingAt = 0.0
		case robustStopResend:
			st.RobustStopPendingAt = now
			st.Phase = reconcilePhaseStopping
			st.LastCommand = reconcileCmdStop
			st.LastCommandAt = now
			return reconcileDecision{
				Command: reconcileCmdStop, MemberID: obs.MemberID,
				StopKind: stopKindRobustResend,
				Reason: "robust stop: re-dispatch (out-of-band STOP unlanded — " +
					"still online past stop_retry)",
				State:          st,
				DispatchWarden: obs.RunningMachine,
			}
		default:
			st.Phase = reconcilePhaseStopping
			return decisionNone(obs, st,
				"robust stop dispatched out-of-band — awaiting warden kill "+
					"(within stop_retry)")
		}
	}
	switch obs.Desired {
	case DesiredStateUninstall:
		return decideUninstall(obs, st, cfg, now)
	case DesiredStateOnline:
		return decideUp(obs, st, cfg, now)
	}
	return decideDown(obs, st, cfg, now)
}

// recycleGraceFor: whether a refocus epoch is collected on a clock at all, and how long. Only the
// two 加速停止 arms (context_high, accelerated_stop) are clocked; every other cause is collected by
// the agent's stopped report or the owner's force-stop.
//
// 🔴 Keep the bool — do not encode "no clock" as a large grace: offboardKindOf tells the agent
// whether a deadline exists from the same winddownKindFor answer, and a clock the agent was not
// told about cuts it off mid-hand-off. Putting a clock back means changing offboardKindOf too.
func recycleGraceFor(refocusOp string, cfg reconcileConfig) (grace float64, clocked bool) {
	if _, clocked := winddownKindFor(refocusOp); !clocked {
		return 0, false
	}
	return cfg.RecycleGrace, true
}

func decideUp(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	if obs.Online {
		st.OfflineSince = 0.0
	} else if st.OfflineSince == 0.0 {
		st.OfflineSince = now
	}
	if obs.Online && obs.RefocusSince > 0.0 {
		dumpDone := obs.AgentStopped
		grace, clocked := recycleGraceFor(obs.RefocusOp, cfg)
		graceExpired := clocked && now >= obs.RefocusSince+grace
		if dumpDone || graceExpired {
			firstDispatch := st.LastCommand != reconcileCmdStop
			if firstDispatch || (now-st.LastCommandAt) >= cfg.StopRetry {
				reason := "recycle: re-dispatch robust stop (still online past " +
					"stop_retry — prior STOP unlanded)"
				if firstDispatch {
					if dumpDone {
						reason = "recycle: refocus marker + agent dump done — robust stop"
					} else {
						reason = "recycle: refocus grace elapsed (dump stuck) — force stop"
					}
				}
				st.Phase = reconcilePhaseStopping
				st.LastCommand = reconcileCmdStop
				st.LastCommandAt = now
				return reconcileDecision{
					Command: reconcileCmdStop, MemberID: obs.MemberID,
					StopKind: stopKindRecycle,
					Reason:   reason, State: st,
					// A 改機器 epoch is collected here while the session still runs on the OLD machine.
					DispatchWarden: obs.RunningMachine,
				}
			}
			st.Phase = reconcilePhaseStopping
			return decisionNone(obs, st,
				"recycle: robust stop dispatched — awaiting warden kill (within stop_retry)")
		}
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st, "recycle: awaiting agent dump (stopping)")
	}
	if obs.Online {
		// RELOCATION backstop, for a divergence nobody stamped (re-pinned while offline, then booted on
		// the old machine; or a pin not written by the relocate handler). Owner ruling (2026-08-28/29):
		// relocate waits like refocus — open the same wind-down (ArmHandoverOp) and let the refocus arm
		// above collect it.
		// 🔴 STOP on the first pass when !HandoverArmable (a warden row runs no ocagent; a member on the
		// 強制停止 rung cannot be walked back): asking for a stamp nobody writes re-decides identically
		// forever. RunningMachine must be known — a claim-less/booting member must never be flapped
		// into a STOP→START loop.
		if obs.TargetMachine != "" && obs.RunningMachine != "" &&
			obs.RunningMachine != obs.TargetMachine {
			if obs.HandoverArmable {
				// st.LastCommand deliberately NOT advanced: the refocus arm's first-dispatch/stop_retry
				// bookkeeping must start clean when it takes over.
				st.Phase = reconcilePhaseStopping
				dec := decisionNone(obs, st,
					"relocate: desired_machine changed (running "+obs.RunningMachine+
						" != target "+obs.TargetMachine+") — opening a wind-down; "+
						"the refocus arm collects it on the agent's hand-off")
				dec.ArmHandoverOp = memberOpRelocate
				return dec
			}
			firstDispatch := st.LastCommand != reconcileCmdStop
			if firstDispatch || (now-st.LastCommandAt) >= cfg.StopRetry {
				reason := "relocate: re-dispatch robust stop (still on old machine " +
					"past stop_retry — prior STOP unlanded)"
				if firstDispatch {
					reason = "relocate: desired_machine changed (running " +
						obs.RunningMachine + " != target " + obs.TargetMachine +
						") — robust stop old session to recycle onto new machine"
				}
				st.Phase = reconcilePhaseStopping
				st.LastCommand = reconcileCmdStop
				st.LastCommandAt = now
				return reconcileDecision{
					Command: reconcileCmdStop, MemberID: obs.MemberID,
					StopKind: stopKindRelocate,
					Reason:   reason, State: st, DispatchWarden: obs.RunningMachine,
				}
			}
			st.Phase = reconcilePhaseStopping
			return decisionNone(obs, st,
				"relocate: robust stop dispatched — awaiting warden kill (within stop_retry)")
		}
		st.Phase = reconcilePhaseOnline
		st.Attempts = 0
		st.BackoffUntil = 0.0
		st.CircuitOpen = false
		st.CircuitCooldownUntil = 0.0
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
		st.StopDeadline = 0.0
		dec := decisionNone(obs, st, "online: converged")
		dec.ConvergedOnline = true
		return dec
	}

	startTimedOut := false
	if st.LastCommand == reconcileCmdStart {
		if obs.LastOpKind == reconcileCmdStart &&
			strings.HasPrefix(obs.LastOpReason, spawnClobberReasonPrefix) {
			// ZOMBIE TAKEOVER: our START bounced off the warden clobber-guard — a live but presence-deaf
			// session squats the slot and plain respawns bounce forever. Reap it with a robust STOP once
			// ZombieConfirmGrace rules out a reconnect.
			if now-st.OfflineSince < cfg.ZombieConfirmGrace {
				st.Phase = reconcilePhaseStarting
				dec := decisionNone(obs, st,
					"zombie suspect: START clobbered a live presence-deaf session — "+
						"withholding takeover stop inside the reconnect-confirm grace")
				dec.ReasonCode = spawnReasonZombieSuspect + ": a session for this member " +
					"is still alive on its machine but is not answering, so the start " +
					"bounced off it. The server waits to be sure it is not simply " +
					"reconnecting before it takes the slot back"
				return dec
			}
			st.Phase = reconcilePhaseStopping
			st.LastCommand = reconcileCmdStop
			st.LastCommandAt = now
			return reconcileDecision{
				Command: reconcileCmdStop, MemberID: obs.MemberID,
				StopKind: stopKindZombieTakeover,
				Reason: "zombie takeover: START clobbered a live presence-deaf " +
					"session — robust stop to reap it before respawn",
				State: st,
			}
		}
		if (now - st.LastCommandAt) <= cfg.StartTimeout {
			st.Phase = reconcilePhaseStarting
			return decisionNone(obs, st, "starting: awaiting presence")
		}
		// Silent timeout: under at-most-once delivery a lost frame is indistinguishable from a member
		// that cannot start — backoff-ONLY, never counted toward the breaker (§4.3).
		st = registerStartFailure(st, cfg, now, false)
		startTimedOut = true
	}
	if st.CircuitOpen {
		st.Phase = reconcilePhaseCircuitOpen
		dec := decisionNone(obs, st, "circuit open: respawn disabled")
		dec.ReasonCode = spawnReasonCircuitOpen + ": too many failed starts in a row, so " +
			"the server has stopped retrying this member for now — it will try again " +
			"by itself; fix what is failing on its machine, or 停止 and 活化 to start over"
		dec.StartTimedOut = startTimedOut
		return dec
	}
	if now < st.BackoffUntil {
		st.Phase = reconcilePhaseBackoff
		dec := decisionNone(obs, st, "backoff: awaiting retry window")
		dec.ReasonCode = spawnReasonBackoff + ": the last start did not come up, so the " +
			"next attempt is waiting out a back-off window — nothing is wrong with the " +
			"button you pressed, the retry has not come round yet"
		dec.StartTimedOut = startTimedOut
		return dec
	}
	st.Phase = reconcilePhaseStarting
	st.LastCommand = reconcileCmdStart
	st.LastCommandAt = now
	return reconcileDecision{
		Command: reconcileCmdStart, MemberID: obs.MemberID,
		Reason:        "spawn: desired_state online, no live session",
		State:         st,
		StartTimedOut: startTimedOut,
	}
}

func decideDown(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	if !obs.Online {
		st.Phase = reconcilePhaseOffline
		st.Attempts = 0
		st.BackoffUntil = 0.0
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
		st.StopDeadline = 0.0
		return decisionNone(obs, st, "offline: converged")
	}
	// 🔴 下線 runs no server clock (owner ruling rc-27d1710174dd): collection is the agent's stopped
	// report (HandleReportStopped dispatches the robust STOP) or the owner's force-stop.
	// Exception: 加速停止, which the owner started and the agent was told about (offboardKindOf quotes
	// this deadline). Its grace runs from stopping_since, which the 加速停止 handler re-stamps; past
	// it this falls through to the robust stop below.
	acceleratedGrace, accelerated := recycleGraceFor(obs.RefocusOp, cfg)
	accelerated = accelerated && obs.StoppingSince > 0.0
	switch {
	case accelerated && now < obs.StoppingSince+acceleratedGrace:
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st,
			"stopping: 加速停止 — within the grace the owner opened; collection is "+
				"the agent's stopped report, or this deadline")
	case accelerated:
	case cfg.SoftOffboardGrace > 0:
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st,
			"stopping: agent is working its offboard sequence — collection is the "+
				"agent's stopped report, or the owner's force-stop")
	}
	if !accelerated && st.StopDeadline == 0.0 {
		st.Phase = reconcilePhaseStopping
		st.StopDeadline = now + cfg.StopGrace
		return decisionNone(obs, st,
			"stopping: grace window opened — awaiting agent selfstop")
	}
	if !accelerated && now < st.StopDeadline {
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st,
			"stopping: within grace window — awaiting agent selfstop")
	}
	firstDispatch := st.LastCommand != reconcileCmdStop
	if firstDispatch || (now-st.LastCommandAt) >= cfg.StopRetry {
		reason := "robust stop: re-dispatch (still online past stop_retry — " +
			"prior STOP unlanded)"
		if firstDispatch {
			reason = "robust stop: grace elapsed, still online"
			if accelerated {
				reason = "robust stop: 加速停止 grace elapsed, still online"
			}
		}
		st.Phase = reconcilePhaseStopping
		st.LastCommand = reconcileCmdStop
		st.LastCommandAt = now
		return reconcileDecision{
			Command: reconcileCmdStop, MemberID: obs.MemberID,
			StopKind: stopKindWinddown,
			Reason:   reason, State: st,
		}
	}
	st.Phase = reconcilePhaseStopping
	return decisionNone(obs, st,
		"stopping: robust stop dispatched — awaiting warden kill (within stop_retry)")
}

func decideUninstall(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	if !obs.Online {
		st.Phase = reconcilePhaseOffline
		st.Attempts = 0
		st.BackoffUntil = 0.0
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
		st.StopDeadline = 0.0
		return decisionNone(obs, st, "uninstall: converged (warden offline)")
	}
	firstDispatch := st.LastCommand != reconcileCmdUninstall
	if firstDispatch || (now-st.LastCommandAt) >= cfg.StopRetry {
		reason := "uninstall: re-dispatch (still online past stop_retry — " +
			"prior UNINSTALL unlanded)"
		if firstDispatch {
			reason = "uninstall: desired_state uninstall, warden online — dispatch uninstall"
		}
		st.Phase = reconcilePhaseStopping
		st.LastCommand = reconcileCmdUninstall
		st.LastCommandAt = now
		return reconcileDecision{
			Command: reconcileCmdUninstall, MemberID: obs.MemberID,
			Reason: reason, State: st,
		}
	}
	st.Phase = reconcilePhaseStopping
	return decisionNone(obs, st,
		"uninstall: dispatched — awaiting warden removal (within stop_retry)")
}

// registerStartFailure: only a VERIFIED hard failure is circuitEligible (no in-tree caller passes
// true today).
func registerStartFailure(
	st reconcileState, cfg reconcileConfig, now float64, circuitEligible bool,
) reconcileState {
	st.Attempts++
	// float math, not a bit shift: attempts grows unboundedly on repeated silent timeouts and must
	// saturate at the cap, never overflow.
	backoff := cfg.BackoffBase * math.Pow(2, float64(st.Attempts-1))
	if backoff > cfg.BackoffCap || math.IsInf(backoff, 1) {
		backoff = cfg.BackoffCap
	}
	st.BackoffUntil = now + backoff
	st.LastCommand = reconcileCmdNone
	st.LastCommandAt = 0.0
	st.CircuitOpen = circuitEligible && st.Attempts >= cfg.CircuitThreshold
	if st.CircuitOpen {
		st.CircuitCooldownUntil = now + cfg.CircuitCooldown
	} else {
		st.CircuitCooldownUntil = 0.0
	}
	return st
}

func reconcileLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reconcile] "+format+"\n", args...)
}

// wardenTargetOf returns "" when there is no destination. 🔴 Never fall back to the raw pin: a
// literal like "auto" then reads as an unreachable warden forever, indistinguishable from an
// offline machine.
func (s *apiServer) wardenTargetOf(memberID string) string {
	target, err := s.dal.GetMember(memberID)
	if err == nil && target != nil && target.Kind == KindWarden {
		return target.ID
	}
	host := ""
	if target != nil {
		host = target.DesiredMachineID
	}
	return s.activeWardenAt(host)
}

func (s *apiServer) enqueueWardenFrame(memberID string, frame []byte) bool {
	return s.enqueueToWarden(memberID, s.wardenTargetOf(memberID), frame)
}

func (s *apiServer) memberKillTargetWarden(memberID string) string {
	last := ""
	if m, err := s.dal.GetMember(memberID); err == nil && m != nil {
		last = m.LastMachineID
	}
	return s.namedKillTarget(memberID, killTargetSources{LastMachineID: last})
}

func (s *apiServer) enqueueToWarden(memberID, warden string, frame []byte) bool {
	if warden == "" || !s.hub.IsOnline(warden) {
		reconcileLog("%s: target warden %q NOT reachable (no live SSE downstream) — "+
			"fail-closed, not dispatching, will retry when the warden connects",
			memberID, warden)
		return false
	}
	// Tagged per subject: one machine's FIFO is shared, and a per-subject receipt may only be written
	// from a per-subject observation (hub.PendingWardenCommandsFor).
	s.hub.EnqueueWardenCommandFor(warden, memberID, frame)
	return true
}

func (s *apiServer) buildStartFrame(m Member) ([]byte, bool) {
	if m.RosterStatus != RosterStatusActive {
		return nil, false
	}
	if len(s.keys.signingSecret()) == 0 {
		return nil, false
	}
	boot, err := s.buildBootContext("", &m)
	if err != nil || boot == nil {
		if err != nil {
			reconcileLog("START fold failed for %q: %v", m.ID, err)
		}
		return nil, false
	}
	token, err := s.mintMemberToken(m, s.agentTokenTTLValue())
	if err != nil {
		reconcileLog("START mint failed for %q: %v", m.ID, err)
		return nil, false
	}
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC: reconcileCmdStart,
		Args: wardenStartArgs{
			MemberID:       m.ID,
			PersonaContext: boot.Context,
			MemberToken:    token,
			Role:           boot.RoleKey,
			Runtime:        NormalizeRuntime(m.Runtime),
			Model:          m.Model,
			Effort:         m.Effort,
			SessionName:    "",
		},
	})
	if err != nil {
		reconcileLog("START frame build failed for %q: %v", m.ID, err)
		return nil, false
	}
	return frame, true
}

func buildTargetFrame(rpc, memberID string) ([]byte, bool) {
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC:  rpc,
		Args: wardenTargetArgs{MemberID: memberID},
	})
	if err != nil {
		reconcileLog("%s frame build failed for %q: %v", rpc, memberID, err)
		return nil, false
	}
	return frame, true
}

type wardenTargetArgs struct {
	MemberID string `json:"member_id"`
}

// machineLacksClaudeButHasCodex is true only on positive evidence: capabilities reported, claude
// `installed` MEASURED false (cli/ocwarden/runtimeprobe.go always sends the claude key on a
// codex-only box), and codex ready. Every unknown answers false — telling the owner of a box that
// has claude to switch the member to Codex is an active misdirection.
func (s *apiServer) machineLacksClaudeButHasCodex(machineID string) bool {
	if machineID == "" {
		return false
	}
	capabilities := s.machineRuntimeCapabilities(machineID)
	if len(capabilities) == 0 {
		return false
	}
	claude, reported := capabilities[RuntimeClaude]
	if !reported || claude.Installed == nil || *claude.Installed {
		return false
	}
	return runtimeCapabilityReady(capabilities[RuntimeCodex])
}

// wakeTimeoutReason overwrites the spawn-time refusal receipt, so on a codex-only machine it must
// itself name the Codex exit.
func (s *apiServer) wakeTimeoutReason(m Member) string {
	runtime := NormalizeRuntime(m.Runtime)
	if runtime == RuntimeClaude && s.machineLacksClaudeButHasCodex(m.DesiredMachineID) {
		return wakeTimeoutReasonCode + ": the START was dispatched but the agent never " +
			"came online within the start window — machine '" + m.DesiredMachineID +
			"' reports no Claude Code installed, so this member cannot boot there. " +
			"Fix any one: set this member's 執行環境 to Codex (that machine has it " +
			"ready); or install Claude Code on that machine " +
			"(warden log: ocwarden.out.log)"
	}
	return wakeTimeoutReasonCode + ": the START was dispatched but the agent never " +
		"came online within the start window — check that " + runtime + " runs and is " +
		"logged in on the target machine (warden log: ocwarden.out.log)"
}

// runtimeCapabilityReady: deliberately NOT machineSupportsRuntime, whose claude arm is permissive
// by contract (OC_CLAUDE_CRED_CHECK=0) and would pick claude on a codex-only box.
func runtimeCapabilityReady(c RuntimeCapabilityDTO) bool {
	if c.Installed == nil || !*c.Installed {
		return false
	}
	return c.LoggedIn == nil || *c.LoggedIn
}

// resolveEmptyRuntimeForPlacement fills an UNSET runtime from what the target machine reports. It
// runs at placement because seedOutOfBox runs before any warden has reported capabilities. No
// report yet → leave it unset (unset normalizes to claude); refusing would make a freshly
// installed machine unusable.
func (s *apiServer) resolveEmptyRuntimeForPlacement(m *Member, warden string) {
	if m.Kind == KindWarden || strings.TrimSpace(m.Runtime) != "" {
		return
	}
	capabilities := s.machineRuntimeCapabilities(warden)
	if len(capabilities) == 0 {
		return
	}
	// installed:true + logged_in:false only comes from a warden older than v0.5.211-beta.1 and means
	// "no evidence", not "signed out". Codex's false is a measurement (`codex login status`) and gets
	// no such grace.
	if claude := capabilities[RuntimeClaude]; claude.Installed != nil && *claude.Installed &&
		claude.LoggedIn != nil && !*claude.LoggedIn {
		reconcileLog("%s: machine %q reports claude installed with logged_in:false — a shape only a "+
			"warden older than v0.5.211-beta.1 emits, where it means \"no credential evidence found\", "+
			"NOT \"signed out\". Declining to auto-resolve this member to codex, because persisting "+
			"that choice is irreversible and this machine may well run claude (env-carried key, "+
			"Bedrock/Vertex managed auth, or OC_CLAUDE_CRED_CHECK=0). Leaving 執行環境 unset: the "+
			"start still goes out as claude, and if it really is signed out the spawn will say so and "+
			"name the Codex exit. To choose deliberately instead: upgrade that machine's warden, or "+
			"set this member's 執行環境 by hand.", m.ID, warden)
		return
	}
	resolved := ""
	switch {
	case runtimeCapabilityReady(capabilities[RuntimeClaude]):
		resolved = RuntimeClaude
	case runtimeCapabilityReady(capabilities[RuntimeCodex]):
		resolved = RuntimeCodex
	default:
		return
	}
	// The check and the write share one transaction: a runtime the owner picked after the tick read
	// the member must stand, not be replaced by the machine's guess.
	var resolvedRow *Member
	err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getMemberOn(tx, m.ID)
		if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
			return err
		}
		if strings.TrimSpace(fresh.Runtime) != "" {
			m.Runtime = fresh.Runtime
			return nil
		}
		m.Runtime = resolved
		fresh.Runtime = resolved
		resolvedRow = fresh
		return setMemberRuntimeOn(tx, m.ID, resolved)
	})
	if err != nil {
		reconcileLog("%s: runtime resolution persist failed: %v", m.ID, err)
		return
	}
	// runtime left PutMember's DO UPDATE SET (T-55), so a whole-row write would persist nothing; the
	// member delta is re-issued explicitly or the cockpit keeps showing the unresolved runtime.
	if resolvedRow != nil {
		s.publishMemberPatch(*resolvedRow, triggerServer)
	}
}

func (s *apiServer) reconcileOne(m Member, st reconcileState, now float64) reconcileDecision {
	// BEFORE the observation is built: flipping desired_state back to online is what makes this tick
	// take decideUp and START the member (下線 → 重啟).
	s.consumeRestartAfterStop(&m, now)
	obs := memberObservation{
		MemberID:        m.ID,
		Desired:         parseDesired(m.DesiredState),
		Online:          s.hub.IsOnline(m.ID),
		RefocusSince:    m.RefocusSince,
		RefocusOp:       m.RefocusOp,
		StoppingSince:   m.StoppingSince,
		AgentStopped:    m.StoppedSince > 0.0,
		LastOpKind:      m.LastOp,
		LastOpReason:    m.LastOpReason,
		TargetMachine:   m.DesiredMachineID,
		RunningMachine:  s.hub.MachineOf(m.ID),
		HandoverArmable: s.memberOwnerOpHandoverArmable(m, memberOpRelocate),
	}
	decision := reconcileDecide(obs, st, s.reconcileConfigLive(), now)
	switch decision.Command {
	case reconcileCmdNone:
		return decision
	case reconcileCmdStart:
		warden := s.wardenTargetOf(m.ID)
		if warden == "" {
			s.stampMemberPlacementBlocked(&m, now)
			decision.Command = reconcileCmdNone
			decision.Reason = "no machine selected"
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		s.resolveEmptyRuntimeForPlacement(&m, warden)
		if m.Kind != KindWarden && !s.machineSupportsRuntime(warden, m.Runtime) {
			reconcileLog("%s: target warden %q does not report runtime %q ready — fail-closed",
				m.ID, warden, NormalizeRuntime(m.Runtime))
			decision.Command = reconcileCmdNone
			decision.Reason = "selected runtime unavailable on target machine"
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		frame, ok := s.buildStartFrame(m)
		if !ok {
			reconcileLog("%s: no START payload (persona/token) — fail-closed, not dispatching",
				m.ID)
			prior := st
			prior.Phase = reconcilePhaseOffline
			decision.Command = reconcileCmdNone
			decision.Reason = "no start payload (persona/token) — fail-closed"
			decision.State = prior
			return decision
		}
		if !s.enqueueWardenFrame(m.ID, frame) {
			decision.Command = reconcileCmdNone
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		// A landed START begins a new session; a session_already_exists refusal on the receipt puts
		// the boot_ts back.
		s.clearSessionBootTSForStart(m.ID)
		s.armReceiptWatch(m.ID, reconcileCmdStart, warden, now)
		return decision
	case reconcileCmdStop:
		// sendStopFrames (shutdown.go) takes no scheduler lock, so it is safe with reconcileMu held.
		warden := decision.DispatchWarden
		if warden == "" {
			warden = s.wardenTargetOf(m.ID)
		}
		if len(s.sendStopFrames(m.ID, []string{warden}, now)) == 0 {
			decision.Command = reconcileCmdNone
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		s.clearSessionBootTS(m.ID)
		return decision
	default:
		// 🔴 Deliberately NOT the shared stop send and NOT receipt-watched: the warden blocks on
		// delivery and refuses to self-exit without a 2xx, and the reconcile keeps re-issuing it.
		frame, ok := buildTargetFrame(decision.Command, m.ID)
		accepted := false
		if ok {
			if decision.DispatchWarden != "" {
				accepted = s.enqueueToWarden(m.ID, decision.DispatchWarden, frame)
			} else {
				accepted = s.enqueueWardenFrame(m.ID, frame)
			}
		}
		if !accepted {
			decision.Command = reconcileCmdNone
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		s.clearSessionBootTS(m.ID)
		return decision
	}
}

func (s *apiServer) reconcileTickMemberLocked(m Member, now float64) reconcileDecision {
	st := s.reconcileStateOf(m.ID)
	decision := s.reconcileOne(m, st, now)
	s.setReconcileState(m.ID, decision.State)
	reconcileLog("%s: desired=%s command=%s — %s",
		m.ID, parseDesired(m.DesiredState), decision.Command, decision.Reason)
	s.armDecidedHandover(m.ID, decision)
	s.stampWakeObservability(&m, decision, now)
	// Yields to the wake receipt: the single last_op_reason slot holds one, and "the agent never came
	// up" is more informative than "we are in back-off".
	if !decision.StartTimedOut {
		s.stampMemberOpBlocked(m.ID, decision.ReasonCode, now)
	}
	return decision
}

// Every stamp in this file judges and writes on the row inside one transaction, and writes only
// the columns it stamps: the HTTP faces (activate / relocate / deactivate) write member rows without
// holding reconcileMu, so a decision on the tick's copy, or a write carrying it, would silently
// revert a change that landed mid-tick. The delta goes out after commit, so the cli/ocagent recycle
// hook never refetches a row without the anchors.
func (s *apiServer) armDecidedHandover(memberID string, decision reconcileDecision) {
	if decision.ArmHandoverOp == "" {
		return
	}
	cfg := s.reconcileConfigLive()
	online := s.hub.IsOnline(memberID)
	var armed *Member
	err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getMemberOn(tx, memberID)
		if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
			return err
		}
		if !s.armMemberOwnerOpHandover(fresh, decision.ArmHandoverOp, cfg, online) {
			return nil
		}
		armed = fresh
		return setMemberWindDownAnchorsOn(tx, fresh.ID, fresh.StoppingSince, fresh.StoppedSince,
			fresh.RefocusSince, fresh.RefocusOp)
	})
	if err != nil {
		reconcileLog("%s: %s wind-down arm persist failed: %v", memberID, decision.ArmHandoverOp, err)
		return
	}
	if armed != nil {
		s.publishMemberPatch(*armed, triggerServer)
	}
}

// stampOpReceipt is the single source of the SERVER-AUTHORED refusal receipt ("the change was
// saved and nothing was started"): last_op_ok a non-nil FALSE, last_op_log cleared. The agent-verdict
// folds (foldCommandResult / foldWorkerCommandResult, api_monitoring.go) are a different class and
// must not be routed through here; a clear back to nil is not a receipt either. RECEIPT-CORE-AUDIT
// marks the exceptions within the refusal class.
//
// Stamping alone stores nothing: the five columns left PutMember's DO UPDATE SET (T-55), so every
// stamp must be persisted via dal.SetMemberLastOp. It takes field pointers, not a shared embedded
// struct, because scanMember/PutMember list these columns positionally.
func stampOpReceipt(lastOp *string, lastOpOK **bool, lastOpLog, lastOpReason *string,
	lastOpAt *float64, op, reason string, now float64) {
	ok := false
	*lastOp = op
	*lastOpOK = &ok
	*lastOpLog = ""
	*lastOpReason = reason
	*lastOpAt = now
}

// stampMemberOpReceipt stamps an IN-MEMORY member; the caller must persist the receipt with a
// separate dal.SetMemberLastOp. Where that write goes relative to the row write is per site — see
// HandleUpdateMember.
func stampMemberOpReceipt(m *Member, reason string, now float64) {
	stampOpReceipt(&m.LastOp, &m.LastOpOK, &m.LastOpLog, &m.LastOpReason, &m.LastOpAt,
		reconcileCmdStart, reason, now)
}

func isStopgapRetryReason(reason string) bool {
	return strings.HasPrefix(reason, spawnReasonBackoff+":") ||
		strings.HasPrefix(reason, spawnReasonCircuitOpen+":")
}

// stopgapRetryStampYields is the precedence rule both the staff and the worker op-blocked stamps
// obey: a retry-loop wait (backoff / circuit_open) must not overwrite a diagnosis of the PREVIOUS
// attempt (wake_timeout, or the worker-only never_collected).
func stopgapRetryStampYields(prior, reason string) bool {
	return isStopgapRetryReason(reason) &&
		(strings.HasPrefix(prior, wakeTimeoutReasonCode+":") ||
			strings.HasPrefix(prior, spawnReasonNeverCollected+":"))
}

// stampMemberOpBlocked never clears: clearing belongs to stampWakeObservability, and a converged
// tick blanking the row would erase the diagnosis one tick after it appeared.
func (s *apiServer) stampMemberOpBlocked(memberID, reason string, now float64) {
	if reason == "" {
		return
	}
	s.stampMemberReceiptOnRow(memberID, "op-blocked stamp", func(fresh *Member) bool {
		if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
			return false
		}
		// 🔴 Turns on the INCOMING code, not only the row: zombie_suspect and warden_unreachable are
		// fresh findings and must still overwrite a stale wake_timeout.
		if stopgapRetryStampYields(fresh.LastOpReason, reason) {
			return false
		}
		stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
			&fresh.LastOpAt, reconcileCmdStart, reason, now)
		return true
	})
}

// stampMemberReceiptOnRow reads an active member inside one transaction, lets stamp decide on and
// change that row's receipt, and writes the five receipt columns only when stamp says it changed
// them.
func (s *apiServer) stampMemberReceiptOnRow(memberID, what string, stamp func(fresh *Member) bool) {
	var stamped *Member
	err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getMemberOn(tx, memberID)
		if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
			return err
		}
		if !stamp(fresh) {
			return nil
		}
		stamped = fresh
		return persistMemberOpReceiptOn(tx, *fresh)
	})
	if err != nil {
		reconcileLog("%s: %s persist failed: %v", memberID, what, err)
		return
	}
	if stamped != nil {
		s.publishMemberPatch(*stamped, triggerServer)
	}
}

func (s *apiServer) stampMemberPlacementBlocked(m *Member, now float64) {
	s.stampMemberReceiptOnRow(m.ID, "placement-blocked stamp", func(fresh *Member) bool {
		// Names the pin on the re-read row, not the snapshot: a relocate landing mid-tick would
		// otherwise be stamped with a complaint about the old machine.
		reason := placementReasonNoMachine + ": no machine is selected for this member — " +
			"choose one (改機器) before waking it; there is no automatic placement"
		if fresh.DesiredMachineID != "" {
			reason = placementReasonUnavailable + ": machine '" + fresh.DesiredMachineID +
				"' is not an active machine — choose another one (改機器); " +
				"no other machine is substituted"
		}
		if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
			return false
		}
		stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
			&fresh.LastOpAt, reconcileCmdStart, reason, now)
		return true
	})
}

func (s *apiServer) stampWakeObservability(m *Member, decision reconcileDecision, now float64) {
	changed := false
	if decision.StartTimedOut {
		// RECEIPT-CORE-AUDIT: deliberately NOT stampOpReceipt — this writer never clears last_op_log
		// (routing it through the core would add that clear, a behaviour change), and it rewrites
		// last_op_reason after the stamp.
		ok := false
		m.LastOp = reconcileCmdStart
		m.LastOpOK = &ok
		m.LastOpAt = now
		m.LastOpReason = s.wakeTimeoutReason(*m)
		if note, lost := s.hub.UndeliveredCommandSince(m.ID, m.WakingSince); lost &&
			note.Verb == reconcileCmdStart {
			m.LastOpReason = fmt.Sprintf(
				wakeTimeoutReasonCode+": the START never reached machine %q — its SSE stream "+
					"failed mid-delivery and the frame was dropped server-side, so "+
					"nothing on that machine was ever asked to start; do not go "+
					"looking at claude there, the machine's connection is the suspect",
				note.Warden)
		}
		m.WakingSince = 0.0
		changed = true
	}
	// Command == start already means a warden took the frame (reconcileOne downgrades an unlanded
	// START); a DispatchUnlanded check here would be a tautology.
	if decision.Command == reconcileCmdStart {
		m.WakingSince = now
		changed = true
		// Only a PLACEMENT stamp is cleared on dispatch: wake_timeout and refused-start receipts say why
		// a boot failed and survive the retry; convergence (below) is what clears them.
		if isSpawnBlockedReason(m.LastOpReason) {
			m.LastOpReason = ""
			m.LastOpLog = ""
		}
	}
	if decision.ConvergedOnline {
		s.clearMemberConvergedFailureReceipt(m.ID, *m)
		return
	}
	if !changed {
		return
	}
	// What this tick decided is applied to the row as it is inside the transaction, not copied from
	// the tick's snapshot: a receipt or a column an owner action wrote mid-tick stands. The
	// dispatch's placement-stamp clear is judged on that row too.
	var stamped *Member
	err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getMemberOn(tx, m.ID)
		if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
			return err
		}
		if decision.StartTimedOut {
			fresh.LastOp = m.LastOp
			fresh.LastOpOK = m.LastOpOK
			fresh.LastOpReason = m.LastOpReason
			fresh.LastOpAt = m.LastOpAt
		}
		if decision.Command == reconcileCmdStart && isSpawnBlockedReason(fresh.LastOpReason) {
			fresh.LastOpReason = ""
			fresh.LastOpLog = ""
		}
		fresh.WakingSince = m.WakingSince
		stamped = fresh
		if err := setMemberWakingSinceOn(tx, fresh.ID, fresh.WakingSince); err != nil {
			return err
		}
		return persistMemberOpReceiptOn(tx, *fresh)
	})
	if err != nil {
		reconcileLog("%s: wake observability persist failed: %v", m.ID, err)
		return
	}
	if stamped != nil {
		s.publishMemberPatch(*stamped, triggerServer)
	}
}

// receiptRendersAsFailure mirrors when the cockpit PAINTS a failed op (AgentDetailPanel.tsx:
// hasLastOp && !lastOpOk — nil takes the ✗ branch), not the column value. Owner ruling
// rc-f2e963132fc5 [1] removes the red line only: success receipts and the amber reason note are
// not cleared.
func receiptRendersAsFailure(lastOp string, lastOpAt float64, lastOpOK *bool) bool {
	if lastOp == "" || lastOpAt <= 0 {
		return false
	}
	return lastOpOK == nil || !*lastOpOK
}

// clearMemberConvergedFailureReceipt (outsource twin: clearWorkerConvergedFailureReceipt) judges the
// RE-READ row; the snapshot is only a short-circuit. Blanking a fresh row on the snapshot's verdict
// would delete a receipt an owner action just wrote.
func (s *apiServer) clearMemberConvergedFailureReceipt(memberID string, snapshot Member) {
	if !receiptRendersAsFailure(snapshot.LastOp, snapshot.LastOpAt, snapshot.LastOpOK) {
		return
	}
	s.stampMemberReceiptOnRow(memberID, "converged receipt clear", func(fresh *Member) bool {
		if !receiptRendersAsFailure(fresh.LastOp, fresh.LastOpAt, fresh.LastOpOK) {
			return false
		}
		fresh.LastOp = ""
		fresh.LastOpOK = nil
		fresh.LastOpLog = ""
		fresh.LastOpReason = ""
		fresh.LastOpAt = 0.0
		return true
	})
}

func isSpawnBlockedReason(reason string) bool {
	for _, code := range spawnBlockedReasonCodes {
		if strings.HasPrefix(reason, code+":") {
			return true
		}
	}
	return false
}

// The Codex threshold (codexThreshold) is deliberately independent of the owner context-percent
// setting: Codex compacts its own long-lived thread, so its useful handover signal is repeated
// compaction, not a transient fill gauge.
func shouldAutoRefocus(runtime string, record map[string]any, cfg SseContextHighConfig, codexThreshold int) bool {
	if NormalizeRuntime(runtime) == RuntimeCodex {
		count, ok := record["compaction_count"].(int)
		if codexThreshold < 1 {
			codexThreshold = defaultCodexCompactionThreshold
		}
		return ok && count >= codexThreshold
	}
	pct := actionableContextPct(record, cfg.StaleGuard)
	return bandFor(pct, cfg.HandoverPct) == levelHandover
}

// canPromoteToAcceleratedStop: only context_notice → context_high. 🔴 An epoch the owner opened
// (重新聚焦, 改機器, 換 model) or restart_self is never promoted — he chose a stop with no clock.
// A member that already reported stopped is collected this tick; re-stamping would move a finished
// wind-down's deadline.
func canPromoteToAcceleratedStop(m Member, op string) bool {
	return op == refocusOpContextHigh &&
		m.RefocusOp == refocusOpContextNotice &&
		m.StoppedSince <= 0.0
}

func shouldNoticeRefocus(
	runtime string, record map[string]any, cfg SseContextHighConfig,
	codexNoticeRound, codexThreshold int,
) bool {
	pct := actionableContextPct(record, cfg.StaleGuard)
	if NormalizeRuntime(runtime) == RuntimeCodex {
		return codexNoticeDue(record, pct, codexNoticeRound, codexThreshold)
	}
	return cfg.NoticePct > 0 && pct != nil && *pct >= float64(cfg.NoticePct)
}

// ctxGateDiagThrottleSecs: one line per actor per five minutes (the owner's number) — unthrottled,
// the 30s pass floods serve.log and buries the line it exists to surface.
const ctxGateDiagThrottleSecs = 300.0

type ctxGateDiagState struct {
	ts   float64
	gate string
}

// noteContextGateSkip logs which gate stopped stampContextHighRecycle for an actor, throttled per
// actor; a CHANGE of gate speaks immediately. An actor flapping between gates therefore logs every
// tick — measured and accepted; do not make the transition wait (damp the flap instead). Purely
// observational. The cell is pruned on the session boundary (clearSessionBootTS).
// Known gap: the stale stopped_since latch and canPromoteToAcceleratedStop skips stay silent, on
// inputs that are not on the wire.
//
// Two passes racing on one actor may each print the line: the worst case of
// the unlocked check-then-store is one extra log line.
func (s *apiServer) noteContextGateSkip(id, gate string, record map[string]any, now float64) {
	if v, seen := s.ctxGateDiagLast.Load(id); seen {
		if last := v.(ctxGateDiagState); last.gate == gate && now-last.ts < ctxGateDiagThrottleSecs {
			return
		}
	}
	s.ctxGateDiagLast.Store(id, ctxGateDiagState{ts: now, gate: gate})
	reconcileLog("recycle: gate skip %s gate=%s pct=%s pct_ts=%s boot_ts=%s "+
		"boot_secs=%s online=%t", id, gate,
		gaugeNumForDiag(record, "context_pct"),
		gaugeNumForDiag(record, "context_pct_ts"),
		gaugeNumForDiag(record, "boot_ts"),
		secsSinceBootForDiag(record, now),
		s.hub.IsOnline(id))
}

func gaugeNumForDiag(record map[string]any, key string) string {
	if record == nil {
		return "-"
	}
	v, ok := asNumber(record[key])
	if !ok {
		return "-"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func secsSinceBootForDiag(record map[string]any, now float64) string {
	secs := gaugeSecsSinceBoot(record, now)
	if secs == nil {
		return "-"
	}
	return strconv.FormatFloat(*secs, 'f', 1, 64)
}

// stampContextHighRecycle mutates the in-slice member so the SAME tick's observation sees the
// marker. Both thresholds open a wind-down, of different kinds: notice_pct a plain 停止
// (context_notice) — which is why the `refocus_since > 0 ⇒ skip` cooldown has exactly one
// exception, the promotion.
func (s *apiServer) stampContextHighRecycle(members []Member, now float64) {
	ctxhigh := s.ctxHighConfig()
	codexNoticeRound := s.codexNoticeRoundSetting()
	for i := range members {
		m := &members[i]
		read := *m
		record := s.gauge.Get(m.ID)
		op := ""
		switch {
		case shouldAutoRefocus(m.Runtime, record, ctxhigh, s.codexCompactionThreshold):
			op = refocusOpContextHigh
		case shouldNoticeRefocus(m.Runtime, record, ctxhigh, codexNoticeRound, s.codexCompactionThreshold):
			op = refocusOpContextNotice
		default:
			// Swallows a STALE pct (context_pct_ts <= boot_ts), which the cockpit still renders a number
			// for (foldActorRuntime reads the raw key).
			s.noteContextGateSkip(m.ID, "no-actionable-pct", record, now)
			continue
		}
		// 🔴 An agent that already reported stopped is not asked again: armRefocusEpoch zeroes the
		// anchors and would destroy its finished close-out. The boot_ts test is load-bearing — 下線 →
		// 活化 leaves a predecessor's stopped_since, and skipping on that would exclude the member from
		// both thresholds for life. No boot_ts → stamp.
		if bootTS, ok := gaugeBootTS(record); ok && m.StoppedSince >= bootTS {
			continue
		}
		promoting := false
		if m.RefocusSince > 0.0 {
			if !canPromoteToAcceleratedStop(*m, op) {
				continue
			}
			promoting = true
		}
		if bootStormTripped(gaugeSecsSinceBoot(record, now), ctxhigh.MinBootSecs) {
			s.noteContextGateSkip(m.ID, "boot-storm", record, now)
			continue
		}
		if !s.hub.IsOnline(m.ID) {
			s.noteContextGateSkip(m.ID, "offline", record, now)
			continue
		}
		// hub.IsOnline is a live-socket fact, not intent: a member deactivated seconds ago keeps its
		// stream for the whole stop grace.
		if !aRefocusStampWouldReachTheAgent(*m) {
			continue
		}
		if promoting {
			// RefocusSince is re-stamped: the deadline is refocus_since + grace, and keeping the first
			// threshold's stamp would announce a deadline already past (a zero-second deadline).
			// armRefocusEpoch is deliberately NOT used: it clears the wind-down anchors, which here
			// belong to the close-out already in flight (it would erase the agent's stopped_since).
			m.RefocusSince = now
			m.RefocusOp = refocusOpContextHigh
		} else if !armRefocusEpoch(m, op, now) {
			reconcileLog("recycle: auto-stamp for %s refused — %s would move the "+
				"wind-down ladder backwards from %s", m.ID, op, m.RefocusOp)
			continue
		}
		if !s.persistRosterStamp(read, m, "recycle: auto-stamp") {
			continue
		}
		if promoting {
			// No frame needed: offboardDeltaPayload composes the FINAL notice from refocus_op on every
			// row write, so the putMember above already carried it.
			reconcileLog("recycle: promoted %s to %s (%s)", m.ID, refocusOpContextHigh,
				NormalizeRuntime(m.Runtime))
		} else {
			reconcileLog("recycle: auto-stamp refocus_since for %s (%s, %s)", m.ID,
				NormalizeRuntime(m.Runtime), op)
		}
	}
}

// tokenExpiryLeadSecs: a live session is asked to wind down one hour (the owner's number) before
// its agent token expires.
const tokenExpiryLeadSecs = 3600.0

// tokenExpiryOf estimates when a live session's agent token expires; 0 = unknown, and callers do
// nothing. The token is minted at dispatch (buildStartFrame) and session_boot_ts is stamped later, so
// this is an upper bound — the trigger fires slightly late, never early. It reads the CURRENT
// agent_token_ttl, not the one the token was minted with.
// Only wardens are exempt: their credential is a different mint (mintWardenToken) with a different
// lifetime and anchor, and renews itself (cli/ocwarden/renew.go). Outsource is NOT exempt — same
// mint and TTL as staff.
func tokenExpiryOf(m Member, agentTokenTTL int64) float64 {
	if m.Kind == KindWarden || agentTokenTTL <= 0 || m.SessionBootTS <= 0 {
		return 0
	}
	return m.SessionBootTS + float64(agentTokenTTL)
}

// stampTokenExpiryWinddown uses stampContextHighRecycle's guards, with no promotion arm, and
// deliberately without the boot-storm guard: a just-booted session within an hour of expiry is not
// a gauge mis-reading.
func (s *apiServer) stampTokenExpiryWinddown(members []Member, now float64) {
	ttl := s.agentTokenTTLValue()
	for i := range members {
		m := &members[i]
		read := *m
		expiry := tokenExpiryOf(*m, ttl)
		if expiry <= 0 {
			continue
		}
		if now < expiry-tokenExpiryLeadSecs {
			continue
		}
		if now >= expiry {
			// Past the (upper-bound) expiry the token is dead: every step of the close-out is an MCP call on
			// it, so a wind-down here could only be answered with 401.
			continue
		}
		if m.RefocusSince > 0.0 {
			continue
		}
		record := s.gauge.Get(m.ID)
		if bootTS, ok := gaugeBootTS(record); ok && m.StoppedSince >= bootTS {
			continue
		}
		if !s.hub.IsOnline(m.ID) {
			continue
		}
		if !aRefocusStampWouldReachTheAgent(*m) {
			continue
		}
		if !armRefocusEpoch(m, refocusOpTokenExpiry, now) {
			reconcileLog("recycle: token-expiry stamp for %s refused — would move "+
				"the wind-down ladder backwards from %s", m.ID, m.RefocusOp)
			continue
		}
		if !s.persistRosterStamp(read, m, "recycle: token-expiry stamp") {
			continue
		}
		reconcileLog("recycle: token-expiry 停止 for %s (token estimated to expire at %.0f, "+
			"lead %.0fs)", m.ID, expiry, tokenExpiryLeadSecs)
	}
}

// persistRosterStamp lands a roster pass's wind-down stamp — the four anchor
// columns and nothing else — in one transaction, and only while the row still
// holds what the pass decided on (read, the tick's roster copy). When it does
// not — an owner verb, a report or another writer landed since that read —
// nothing is written, the slice entry goes back to the row as read, and the
// next tick decides again on the row as it is then.
func (s *apiServer) persistRosterStamp(read Member, stamped *Member, what string) bool {
	var cur *Member
	landed := false
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if cur, err = getMemberOn(tx, read.ID); err != nil || cur == nil {
			return err
		}
		if !sameWindDownInputs(*cur, read) {
			return nil
		}
		cur.StoppingSince, cur.StoppedSince = stamped.StoppingSince, stamped.StoppedSince
		cur.RefocusSince, cur.RefocusOp = stamped.RefocusSince, stamped.RefocusOp
		landed = true
		return setMemberWindDownAnchorsOn(tx, cur.ID, cur.StoppingSince, cur.StoppedSince,
			cur.RefocusSince, cur.RefocusOp)
	})
	if err != nil || !landed {
		if err != nil {
			reconcileLog("%s persist failed for %s: %v", what, read.ID, err)
		} else {
			reconcileLog("%s for %s not written — the row changed since this tick read it; "+
				"the next tick decides again", what, read.ID)
		}
		*stamped = read
		return false
	}
	s.publishMemberPatch(*cur, triggerServer)
	return true
}

// sameWindDownInputs: the columns the roster passes decide a stamp on.
func sameWindDownInputs(a, b Member) bool {
	return a.DesiredState == b.DesiredState && a.RosterStatus == b.RosterStatus &&
		a.StoppingSince == b.StoppingSince && a.StoppedSince == b.StoppedSince &&
		a.RefocusSince == b.RefocusSince && a.RefocusOp == b.RefocusOp &&
		a.ForcedStopAt == b.ForcedStopAt && a.SessionBootTS == b.SessionBootTS
}

func bootStormTripped(secsSinceBoot *float64, minBootSecs float64) bool {
	if minBootSecs <= 0 {
		return false
	}
	if secsSinceBoot == nil || *secsSinceBoot < 0 {
		return false
	}
	return *secsSinceBoot < minBootSecs
}

// consumeUninstallIntentOnOffline consumes the ONE-SHOT uninstall intent (§4.3): a warden offline
// while still desired=uninstall has converged, so the record folds back to offline. Left standing,
// every re-install reconnect would be answered with another UNINSTALL (a real loop, 2026-07).
func (s *apiServer) consumeUninstallIntentOnOffline(members []Member) {
	for i := range members {
		m := &members[i]
		if m.Kind != KindWarden || parseDesired(m.DesiredState) != DesiredStateUninstall {
			continue
		}
		if s.hub.IsOnline(m.ID) {
			continue
		}
		folded, _, err := s.foldUninstallIntentOnRow(m.ID)
		if err != nil {
			reconcileLog("uninstall: intent-consume persist failed for %s: %v", m.ID, err)
			continue
		}
		if folded == nil {
			continue
		}
		*m = *folded
		s.publishMemberPatch(*m, triggerServer)
		reconcileLog("uninstall: consumed one-shot intent for offline warden %s "+
			"(desired_state → offline; record kept)", m.ID)
	}
}

// consumeUninstallOnDisconnect is the event-driven twin, fired from the SSE disconnect edge
// (api_infra.go), so a fast re-install cannot reconnect into the standing kill order within a
// cadence window.
func (s *apiServer) consumeUninstallOnDisconnect(memberID string) {
	if s.noReconcile || s.hub.IsOnline(memberID) {
		return
	}
	folded, _, err := s.foldUninstallIntentOnRow(memberID)
	if err != nil {
		reconcileLog("uninstall: disconnect-edge intent-consume persist failed for %s: %v",
			memberID, err)
		return
	}
	if folded == nil {
		return
	}
	s.publishMemberPatch(*folded, triggerServer)
	reconcileLog("uninstall: consumed one-shot intent on warden %s disconnect "+
		"(desired_state → offline; record kept)", memberID)
}

// foldUninstallIntentOnRow folds a warden's standing uninstall intent back to offline on the row as
// it is inside one transaction, so a delete or any other write that landed after a caller's read
// stands. folded is the written row, nil when the row no longer holds the intent; fresh is the row
// as read either way (nil when absent).
func (s *apiServer) foldUninstallIntentOnRow(id string) (folded, fresh *Member, err error) {
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, id)
		if err != nil || cur == nil {
			return err
		}
		fresh = cur
		if cur.Kind != KindWarden || parseDesired(cur.DesiredState) != DesiredStateUninstall {
			return nil
		}
		cur.DesiredState = DesiredStateOffline
		folded = cur
		return writeMemberOn(tx, *cur)
	})
	if err != nil {
		return nil, nil, err
	}
	return folded, fresh, nil
}

// quietSince is the later of stopping_since and the gauge ts. The gauge ts is written by the
// member's own session and is activity-driven, NOT a heartbeat (statusLine redraw on Claude,
// tokenUsage on codex), so a close-out blocked in one long call is still swept. (codex's
// clock-driven identityHeartbeat lands in s.telemetry, not s.gauge.) A missing record means no
// opinion: the gauge is volatile, and a station re-exec must not read as fleet-wide silence.
func quietSince(m Member, gauge map[string]any) float64 {
	quiet := m.StoppingSince
	if ts, ok := asNumber(gauge["ts"]); ok && ts > quiet {
		quiet = ts
	}
	return quiet
}

func (s *apiServer) clearStaleStoppingOnOnline(members []Member, now float64) {
	for i := range members {
		m := &members[i]
		if m.DesiredState != DesiredStateOnline {
			continue
		}
		if m.StoppingSince <= 0.0 {
			continue
		}
		if !s.hub.IsOnline(m.ID) {
			continue
		}
		// 🔴 Not while a close-out could still be in progress: "survived a stop" and "working its
		// offboard" look identical here. The clock is QUIET time, not anchor age — collecting
		// sub-agents routinely outlasts the window. Cost, accepted: an abandoned close-out reads
		// stopping until report_stopped or a reboot.
		if now-quietSince(*m, s.gauge.Get(m.ID)) < SoftOffboardGraceSecs {
			continue
		}
		read := *m
		m.StoppingSince = 0.0
		if !s.persistRosterStamp(read, m, "revive: stale-stopping clear") {
			continue
		}
		reconcileLog("revive: auto-cleared stale stopping_since on observed-online %s "+
			"(survived stop / SSE reconnect)", m.ID)
	}
}

func (s *apiServer) runReconcileTick(now float64) {
	defer func() {
		if r := recover(); r != nil {
			reconcileLog("tick FAULT: %v", r)
		}
	}()
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	all, err := s.dal.ListMembers()
	if err != nil {
		reconcileLog("tick: roster read failed: %v", err)
		return
	}
	var members []Member
	for _, m := range all {
		// 🔴 ListMembers includes contractor rows; this line is the only thing keeping them out of the
		// member FSM (else one row takes a `start` from both halves in the same tick).
		if lifecycleTickDriverFor(m) != driverReconcile {
			continue
		}
		if !lifecyclePolicyFor(m).ShouldExist() {
			continue
		}
		members = append(members, m)
	}
	// The ONE list of pre-decide formalities (lifecycle_roster.go), shared with the outsource
	// producer; a formality that must not reach workers says so as its own AppliesTo.
	s.runLifecycleRosterPasses(members, now)
	// Swept BEFORE the decide pass, so a start/stop armed by THIS tick always gets a full receipt
	// window.
	s.sweepLapsedReceipts(now)
	reconcileLog("tick: %d candidate(s)", len(members))
	for i := range members {
		s.reconcileTickMemberLocked(members[i], now)
	}
}

func (s *apiServer) reconcileMemberNow(memberID string) reconcileDecision {
	if s.noReconcile {
		return reconcileDecision{}
	}
	defer func() {
		if r := recover(); r != nil {
			reconcileLog("instant tick FAULT for %s: %v", memberID, r)
		}
	}()
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		return reconcileDecision{}
	}
	// 🔴 Not a no-op: today's callers happen to pass staff rows, but a future anyMember caller would
	// put a contractor under both halves.
	if lifecycleTickDriverFor(*m) != driverReconcile {
		return reconcileDecision{}
	}
	if !lifecyclePolicyFor(*m).ShouldExist() {
		return reconcileDecision{}
	}
	reconcileLog("instant tick: member %s", memberID)
	return s.reconcileTickMemberLocked(*m, nowSecs())
}

func (s *apiServer) dispatchRobustStopNow(memberID string) {
	if s.noReconcile {
		return
	}
	// The --no-reconcile gate stays at THIS caller: it is the producer kill switch, and the outsource
	// verbs have never consulted it (api_stub.go).
	s.dispatchShutdown(memberID, "robust-stop")
}

// noteRobustStopDispatched takes reconcileMu itself: every caller is an HTTP handler that holds no
// reconcile lock.
func (s *apiServer) noteRobustStopDispatched(memberID string, now float64) {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	st := s.reconcileStateOf(memberID)
	st.RobustStopPendingAt = now
	s.setReconcileState(memberID, st)
}

// identitySweepDedupeSecs reuses the stop_retry pace, so a sweep re-fires no faster than a STOP.
const identitySweepDedupeSecs = 90.0

// dispatchIdentitySweepNow: once a member's 正身 is CONFIRMED live on its desired machine, robust-STOP
// member-<id> on every OTHER online warden. keepWarden is excluded, so the 正身 is never swept (owner
// safety gate, rc-2230cb0158e8). Caller MUST hold reconcileMu.
func (s *apiServer) dispatchIdentitySweepNow(memberID, keepWarden string, now float64) {
	if s.noReconcile || memberID == "" {
		return
	}
	if last, ok := s.identitySweepAt[memberID]; ok && now-last < identitySweepDedupeSecs {
		return
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		return
	}
	targets := []string{}
	for _, m := range members {
		if m.Kind != KindWarden || m.RosterStatus != RosterStatusActive {
			continue
		}
		if m.ID == keepWarden || !s.hub.IsOnline(m.ID) {
			continue
		}
		targets = append(targets, m.ID)
	}
	// Unwatched on purpose: these wardens never hosted the session and routinely answer
	// no_such_session, so a receipt deadline would wait on an answer that says nothing.
	swept := s.enqueueStopFrames(memberID, targets)
	for _, warden := range swept {
		reconcileLog("identity-sweep: %s confirmed on desired machine %s — "+
			"robust stop residual session on %s", memberID, keepWarden, warden)
	}
	if len(swept) > 0 {
		s.identitySweepAt[memberID] = now
	}
}

// identitySweepOnConnect fires the sweep only when this connection is the 正身 on the expected
// machine; a wanderer does not initiate one — it is the TARGET of the real 正身's sweep.
// Never holds both locks: workerSpawnObs takes and releases outsourceMu before reconcileMu is
// locked here, and no path in the package acquires one while holding the other.
func (s *apiServer) identitySweepOnConnect(memberID, machineClaim string) {
	if s.noReconcile || memberID == "" || machineClaim == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil || m.Kind == KindWarden {
		return
	}
	if parseDesired(m.DesiredState) != DesiredStateOnline ||
		!s.connectionIsTheGenuineArticle(*m, machineClaim) {
		return
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	s.dispatchIdentitySweepNow(memberID, machineClaim, nowSecs())
}

// connectionIsTheGenuineArticle: machineClaim is server-minted and unforgeable, so the test is
// whether it equals the expected machine — staff: desired_machine_id; outsource: the pin, else
// where the server actually dispatched the last start (workerSpawnObs; task-level or manual
// placement leaves no pin). "" on either side is unverifiable ⇒ false (fail-safe).
func (s *apiServer) connectionIsTheGenuineArticle(m Member, machineClaim string) bool {
	if machineClaim == "" {
		return false
	}
	expected := m.DesiredMachineID
	if m.Kind == KindOutsource && expected == "" {
		expected, _ = s.workerSpawnObs(m.ID)
	}
	return expected != "" && expected == machineClaim
}
