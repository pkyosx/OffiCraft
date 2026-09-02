package main

// reconcile.go — the server-reconcile producer (M3 step ⑥), ported from
// the retired Python service/reconcile/{machine,observation,controller,dispatch,driver,
// producer}.py against spec/lifecycle.md §4:
//
//   * reconcileDecide — the PURE per-member state machine (machine.py decide):
//     desired_state × observed-online → the ONE command to dispatch (or none),
//     with the frozen timers (§4.4): start_timeout WakingTTLSecs, stop_grace 120s,
//     stop_retry 90s, recycle_grace 120s, backoff 5/300s, circuit 5/120s.
//   * the in-memory reconcile store (lifecycle.md §3 inventory #7): per-member
//     bookkeeping keyed by member id; restart amnesia IS the contract — a lost
//     store just resets the dedupe/grace windows and the next tick re-decides
//     from presence.
//   * dispatch (§4.6): fire-and-forget frames onto the per-warden FIFO
//     (hub.EnqueueWardenCommand), fail-closed behind the target-reachability
//     gate (the addressed warden must itself hold the live SSE downstream) and
//     the fail-closed START payload fold+mint. A refused dispatch keeps the
//     PRIOR state so the next tick retries (never record an undelivered
//     command).
//   * the RECONCILE HALF of the 30s cadence tick (runReconcileTick — since
//     T-14 item 5 one loop runs both halves in order, startLifecycleCadence in
//     lifecycle_tick.go), with the four pre-decide roster
//     passes (auto-recycle stamp / recycle loop-break / stale-stopping clear /
//     offline-warden uninstall-intent consumption)
//     and the event-driven single-member tick (reconcileMemberNow — the
//     activate/deactivate/uninstall click seam, sharing the SAME store + mutex
//     so the cadence stays an idempotent backstop).
//
// --no-reconcile (serve flag) disables the producer WHOLESALE — the cadence
// loop AND every event-driven warden-command dispatch IT OWNS — while the rest
// of the server (intent writes, presence, SSE) runs unchanged. This is the
// shadow-deployment kill-switch lifecycle.md Appendix B #1 requires; the paths
// it does NOT cover are enumerated in spec/lifecycle.md §4.1.

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// ── config (spec/lifecycle.md §4.4 — defaults are contract) ──────────────────

type reconcileConfig struct {
	StartTimeout float64 // START unconfirmed → failed spawn (WakingTTLSecs)
	StopGrace    float64 // self-stop window before the robust stop
	StopRetry    float64 // STOP/UNINSTALL re-dispatch window (lost frame)
	RecycleGrace float64 // dump-stuck fallback from refocus_since
	// SoftOffboardGrace is how long a close-out may say NOTHING before its
	// anchor is treated as residue (T-7723 — it is silence, not the anchor's
	// age). It is NOT a deadline: neither soft arm is collected on a clock —
	// 下線 by the owner's ruling (rc-27d1710174dd) and 重新聚焦 by his 2026-08-19
	// one (rc-c540367065ad). Both end at the agent's own stopped report or at
	// his force-stop, and this window is what keeps that button on screen for a
	// close-out that has gone quiet (see clearStaleStoppingOnOnline); one that
	// is still reporting keeps it for as long as it takes.
	//
	// Setting it to 0 restores the pre-T-a9d6 timed wind-down wholesale, which
	// is what the tests covering the robust-stop ladder drive. It is a compile-
	// time constant today (domain.go), not an owner-facing setting.
	SoftOffboardGrace float64
	BackoffBase       float64
	BackoffCap        float64
	CircuitThreshold  int
	CircuitCooldown   float64
	// ZombieConfirmGrace (T-9adc) is the SECOND-CONFIRMATION window before the
	// zombie-takeover STOP: a START that bounced off the warden clobber-guard
	// proves a live-but-presence-deaf session squats the slot — but "presence-
	// deaf" and "reconnecting through a network blip" are indistinguishable at
	// that instant (2026-07-20 incident: the STOP raced a session that was
	// seconds from reconnecting on its own). So the takeover STOP is withheld
	// until the member has been CONTINUOUSLY offline for this long; a reconnect
	// inside the window is the liveness proof that cancels the kill (the online
	// converged arm resets OfflineSince). BOUNDED by construction: once the
	// window lapses with no reconnect the STOP fires — a true zombie is still
	// reaped, just later; this can never degrade into "never kill".
	// Sized 2×StartTimeout — DERIVED, so the seconds are deliberately not spelled
	// out here (T-20: this line said "(180s)" and StartTimeout then moved). It
	// covers the agent's worst honest reconnect (backoff cap 15s + 45s idle-read
	// watchdog + one 30s cadence tick ≈ 90s — those three ARE fixed constants,
	// independent of StartTimeout) with a full extra START-window of slack.
	ZombieConfirmGrace float64
}

func defaultReconcileConfig() reconcileConfig {
	return reconcileConfig{
		StartTimeout:      WakingTTLSecs,
		StopGrace:         StoppingTimeoutSecs,
		StopRetry:         90.0,
		RecycleGrace:      StoppingTimeoutSecs,
		SoftOffboardGrace: SoftOffboardGraceSecs,
		BackoffBase:       5.0,
		BackoffCap:        300.0,
		CircuitThreshold:  5,
		CircuitCooldown:   120.0,
		// 2×StartTimeout — see the field comment (T-9adc zombie second-confirm).
		ZombieConfirmGrace: 2 * WakingTTLSecs,
	}
}

// ── vocabulary ───────────────────────────────────────────────────────────────

// The server→warden command a decision selects (machine.py CommandKind). STOP
// is the single ROBUST stop — the warden self-escalates the kill internally.
const (
	reconcileCmdNone      = "none"
	reconcileCmdStart     = "start"
	reconcileCmdStop      = "stop"
	reconcileCmdUninstall = "uninstall"
	// reconcileCmdUpdate is NOT a reconcile decision: it is the owner-clicked
	// one-shot "kick your self-update NOW" verb (T-5f01, spec/sse.md §7),
	// dispatched straight from POST /api/machines/{id}/upgrade — listed here
	// so the warden-command verb vocabulary stays in one place.
	reconcileCmdUpdate = "update"
)

// spawnClobberReasonPrefix is the prefix of the warden SpawnOutcome.Reason
// (cli/ocwarden/spawn.go start clobber-guard) folded onto member.last_op_reason
// when a START bounced off a live-but-presence-deaf local session. decideUp
// reads it to reap that zombie instead of respawning into it forever.
// The FSM's STOP flavours (T-72dd) — see reconcileDecision.StopKind.
const (
	// stopKindRecycle: a wind-down epoch is being collected (refocus / 換手 /
	// the context thresholds). The session dies and is EXPECTED to come back on
	// the same machine — nothing about it says the machine is bad.
	stopKindRecycle = "recycle"
	// stopKindZombieTakeover: a START bounced off the warden clobber-guard, so a
	// presence-deaf session squats the slot. THIS is the one that justifies
	// benching the machine: the slot there is known-wedged.
	stopKindZombieTakeover = "zombie_takeover"
	// stopKindRelocate: the session must die because it is on the WRONG machine.
	stopKindRelocate = "relocate"
	// stopKindWinddown: desired_state=offline — the graceful 下線 / 加速停止 kill.
	stopKindWinddown = "winddown"
	// stopKindRobustResend: the out-of-band STOP that never landed, re-sent.
	stopKindRobustResend = "robust_resend"
)

const spawnClobberReasonPrefix = "session_already_exists"

// The observability phase projection (machine.py Phase).
const (
	reconcilePhaseOffline     = "offline"
	reconcilePhaseStarting    = "starting"
	reconcilePhaseOnline      = "online"
	reconcilePhaseBackoff     = "backoff"
	reconcilePhaseCircuitOpen = "circuit_open"
	reconcilePhaseStopping    = "stopping"
)

// parseDesired is the junk-safe desired_state parse (machine.py
// Desired.parse): anything unrecognised is OFFLINE — an unknown intent never
// spawns (fail-safe).
func parseDesired(raw string) string {
	switch raw {
	case DesiredStateOnline, DesiredStateUninstall:
		return raw
	}
	return DesiredStateOffline
}

// ── state + observation + decision (pure value types) ───────────────────────

// reconcileState is the per-member reconcile bookkeeping (machine.py
// ReconcileState). Passed and returned BY VALUE so a transition is a pure
// value, never shared mutation.
type reconcileState struct {
	Phase                string
	Attempts             int
	BackoffUntil         float64
	CircuitOpen          bool
	CircuitCooldownUntil float64
	LastCommand          string
	LastCommandAt        float64
	StopDeadline         float64
	// RobustStopPendingAt (T-ed79) is when an OUT-OF-BAND robust STOP was last
	// dispatched for this member by dispatchRobustStopNow — the force-stop
	// button, the cancel-wake kill, and the report_stopped collect. 0 means none
	// is outstanding.
	//
	// 🔴 WHY IT EXISTS. That dispatch is raw and best-effort: enqueueToWarden is
	// fail-closed on a warden holding no live SSE downstream, so the frame is
	// simply dropped. For the report_stopped collect that drop is terminal —
	// decideUp's recycle arm is gated on refocus_since > 0 and decideDown's soft
	// arm returns decisionNone for the whole window, so NO arm re-sends it. The
	// member is left online on a session it has already declared finished, and
	// nothing will ever kill and respawn it.
	//
	// 🔴 WHY THE ANCHOR IS THE DISPATCH AND NOT THE MEMBER'S LATCH. Re-deriving
	// "this one needs collecting" from stopped_since would re-open the harm the
	// first commit of this branch closed: 下線 → 活化 leaves a PREDECESSOR's
	// stopped_since on a brand-new session with no epoch
	// (TestWindDownKind_APredecessorsLatchDoesNotSilenceTheThresholds), and that
	// session would be robust-stopped on its first tick with no close-out. A
	// dispatch marker cannot say that: it is written only when a STOP was really
	// sent for THIS session, and it is dropped the moment the session goes
	// offline, so it can never be inherited by the next generation.
	RobustStopPendingAt float64
	// OfflineSince (T-9adc) is the first tick a desired-online member was
	// OBSERVED offline (0 while online / never observed offline). It feeds the
	// zombie-takeover second-confirmation window ONLY: the takeover STOP needs
	// proof of a SUSTAINED absence, not one offline sample. Observation-grained
	// (stamped at tick resolution, ≤30s after the actual disconnect — always in
	// the safe direction: the kill is deferred, never hastened). Restart amnesia
	// re-arms the window from zero, again the safe direction.
	OfflineSince float64
}

func newReconcileState() reconcileState {
	return reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone}
}

// memberObservation is the reconcile input for one member (machine.py
// MemberObservation): the desired intent + the live SSE-online fact + the two
// recycle markers.
type memberObservation struct {
	MemberID     string
	Desired      string // parsed (parseDesired)
	Online       bool
	RefocusSince float64
	// RefocusOp is the CAUSE of that epoch (member.refocus_op). decideUp reads
	// it for one reason only: to ask recycleGraceFor whether this epoch is on a
	// clock AT ALL. Since T-ed79 almost none are — the clocked causes are the two
	// 加速停止 arms, the SECOND context threshold (context_high) and the owner's
	// own press (accelerated_stop); 重新聚焦, 改機器, a model/runtime change,
	// restart_self, the FIRST context threshold and token expiry are all
	// collected by the agent's own stopped report or the owner's force-stop. This used to say
	// "every other cause is already a final call when it lands and gets its
	// 120s", i.e. the exact inverse of today's rule — and it sits ~100 lines
	// from recycleGraceFor, which says the correct thing. The ruling lives in
	// ONE place (winddownKindFor); this arm must never re-derive it.
	RefocusOp string
	// StoppingSince is the 下線 arm's own anchor (member.stopping_since), and it
	// is here for exactly ONE reader: the owner-pressed 加速停止 (T-ed79), whose
	// grace runs from the press. decideDown had no durable anchor before — the
	// pre-T-a9d6 timed path armed st.StopDeadline from the OBSERVATION, which is
	// process-local and forgotten on restart. That was tolerable for a fallback
	// nobody announced; it is not tolerable for a deadline the agent has been
	// TOLD, because the sentence is composed from the durable row and would
	// outlive a clock that lives only in memory. Reading the row keeps the two
	// on the same fact across a station re-exec.
	StoppingSince float64
	AgentStopped  bool // stopped_since > 0 (the graceful dump-done fact)
	// The last warden command_result folded onto this member (api_monitoring.go
	// foldCommandResult → member.last_op*): the executed op kind + its structured
	// cause. decideUp reads them to detect a START that bounced off the local
	// clobber-guard — a zombie session squatting the slot.
	LastOpKind   string
	LastOpReason string
	// The two machine facts that drive relocation (owner changed desired_machine
	// on a LIVE member): TargetMachine is the owner-pinned placement
	// (member.desired_machine_id); RunningMachine is the machine the live session
	// is ACTUALLY on — the SSE machine claim (hub.MachineOf), which is the
	// desired_machine baked into the boot token at spawn time. They diverge
	// exactly when the owner re-pins a running member. RunningMachine is "" for a
	// claim-less boot (empty desired_machine at mint) — the fail-safe that keeps
	// a claim-less/booting member out of the relocation recycle.
	TargetMachine  string
	RunningMachine string
	// HandoverArmable is "armMemberOwnerOpHandover(m, relocate) would succeed"
	// — asked by memberOwnerOpHandoverArmable, which runs the real gates against
	// a throwaway copy rather than re-listing them (T-14 #4).
	//
	// 🔴 IT IS HERE TO KEEP THE RELOCATION BACKSTOP CONVERGENT. That arm no
	// longer kills on sight; it asks the caller to open a wind-down and lets the
	// refocus arm collect it. But a wind-down can be REFUSED — a warden row has
	// no ocagent to hand anything over, and a member already on the 強制停止 rung
	// may not be walked back down the ladder — and a decision that asks for a
	// stamp nobody writes is a tick that changes nothing and re-decides
	// identically forever, leaving a session running on the wrong machine with
	// no path off it. Reading the answer BEFORE choosing the branch is what lets
	// the arm fall back to the original first-pass STOP in exactly the cases
	// where there is no hand-off to wait for.
	HandoverArmable bool
}

// reconcileDecision is one decision: the command to dispatch (or none), a
// human reason, and the NEXT state to persist.
type reconcileDecision struct {
	Command  string
	MemberID string
	Reason   string
	State    reconcileState
	// DispatchWarden overrides the command's warden routing: "" (the default)
	// routes via wardenTargetOf (the member's DESIRED machine — correct for
	// START and every normal STOP). A relocation STOP sets it to the member's
	// RUNNING machine's warden, because the session to kill lives on the OLD
	// machine — routing that STOP to the new (desired) machine's warden would
	// no-op forever (the FIFO is keyed by warden id; only the warden holding the
	// session can kill it). Empty on every decision but the relocation STOP.
	DispatchWarden string
	// DispatchUnlanded is true when reconcileOne DECIDED a command (START / STOP /
	// UNINSTALL) but the warden was unreachable, so the command was downgraded to
	// a no-op and the next tick must retry. It lets an EVENT-DRIVEN caller (the
	// relocate handler) report "move dispatched-pending" instead of silently
	// 200-ing a relocation that never landed (T-8655 — the silent false-success
	// this ticket fixes). Never set on a genuine no-op / converged decision.
	DispatchUnlanded bool
	// StartTimedOut (T-ba62) is true on the tick that OBSERVES a dispatched START
	// lapse its start_timeout with no presence. It exists because that observation
	// used to be state-only: the decider folded it straight into exponential
	// backoff and the ONLY trace was a reconcileLog line on the server's stderr.
	// From the cockpit, "the wake was dispatched and the agent never came up" and
	// "nobody ever woke this member" were the same picture — a grey member. The
	// tick turns this flag into a durable last_op receipt so the two are
	// distinguishable in the UI.
	StartTimedOut bool
	// ConvergedOnline (T-39) is true on the tick that finds a member the owner
	// wants running ALREADY RUNNING — decideUp's "online: converged" arm, and
	// only that arm. It is the exact opposite signal to StartTimedOut, and the
	// two are mutually exclusive by construction (that one is reached only when
	// the member is NOT online).
	//
	// 🔴 IT EXISTS BECAUSE "THE FAILURE IS OVER" HAD NO WRITER. Every arm that
	// stamps a failure receipt runs while the member is broken; the only arm that
	// could clear a WARDEN receipt was gated on `Command == start`, i.e. on the
	// server having just tried AGAIN. (Narrowed on purpose: two other clearing
	// arms exist — that same arm's placement-stamp clear and worker_spawn.go's
	// clearWorkerPlacementBlock — but both are ALSO gated on a fresh dispatch and
	// neither touches a warden receipt, so the same trap holds for all three.) So a receipt could only be cleared by a further attempt,
	// never by the member simply coming back — and a member that recovered on its
	// own kept its red 「最近操作」 line for as long as it stayed healthy (field
	// evidence: 10.6 days). The recovery is a fact the DECIDER knows and nothing
	// downstream could re-derive without copying decideUp's converged conditions,
	// which is the two-copies-of-one-ruling failure StopKind's comment describes.
	// Same shape as StopKind and ArmHandoverOp: decided where the branch lives,
	// read verbatim by the caller (reconcileTickMemberLocked for staff,
	// reconcileWorkerLiveness for outsource — one flag, both producers).
	//
	// ⚠️ KNOWN REMAINING CASE, declared rather than discovered later: only
	// decideUp sets this. decideDown and decideUninstall never do, so a FAILED
	// STOP or UNINSTALL receipt on a member that has since converged OFFLINE is
	// still shown forever — the same disease on the other side of the FSM. That
	// is the owner's ruling honoured literally (「他回來了」 is the online
	// direction) and not an oversight; it needs its own ruling because "the
	// member is gone" is not obviously the moment to forget why stopping it
	// failed.
	ConvergedOnline bool
	// StopKind names WHICH of the FSM's STOPs this is (T-72dd). "" on every
	// decision that is not a STOP.
	//
	// 🔴 It exists because a caller CANNOT tell them apart from the outside
	// without re-deriving the decider's own branch conditions — and the moment a
	// caller re-derives them, there are two copies of one ruling and they drift.
	// reconcileWorkerLiveness had exactly that problem: its STOP arm was written
	// for the zombie takeover and BENCHED the target machine, which is right for
	// a ghost-squatted slot and wrong for a recycle — a 換手 is supposed to come
	// back up on the SAME machine, so benching it means the worker is killed and
	// then cannot return until the cooldown lapses. The distinction is made HERE,
	// where the branch is chosen, and read verbatim by the caller.
	StopKind string
	// ReasonCode (T-ed79 #14) is the STRUCTURED half of Reason: one of the
	// spawnReason* / placementReason* codes when this decision is a STALL the
	// owner is owed an explanation for, "" when it is not.
	//
	// 🔴 "" IS THE DEFAULT AND IT MEANS 'OWES NOBODY ANYTHING'. Most decisions
	// are converged states (online: converged, offline: converged, uninstall:
	// converged) or steps in a wind-down that is proceeding normally. Stamping a
	// receipt for those would turn every healthy member into a permanent SSE
	// event stream and make the receipt meaningless — the field exists to name
	// the cases where the member wants to be online and is NOT, and the reason
	// was previously reachable only on the server's stderr.
	ReasonCode string
	// ArmHandoverOp (T-14 #4) is the owner-op cause of a wind-down epoch the
	// DECIDER wants opened on this member's row, "" on every decision that wants
	// none. Today exactly one arm sets it: decideUp's relocation backstop, with
	// memberOpRelocate.
	//
	// 🔴 WHY A DECISION FIELD AND NOT A WRITE. reconcileDecide is pure — obs / st
	// / cfg / now in, a decision out — while opening an epoch is a durable row
	// write. The alternative shapes were both worse:
	//
	//   - have the CALLER decide whether to arm, before deciding. It would have
	//     to re-derive this arm's guard (pinned target, known running machine, an
	//     actual mismatch, no epoch already open) outside the decider, which is
	//     the two-copies-of-one-ruling failure StopKind's comment above exists to
	//     describe — and the copies would sit in different files.
	//   - a new Command kind. Command means "the frame to hand a warden";
	//     reconcileOne's switch dispatches every value of it, and a member of that
	//     set that no warden ever sees would make the field mean two things.
	//
	// This field is the same shape as StopKind and DispatchWarden: the branch is
	// chosen where the branch conditions live, and the caller reads the answer
	// verbatim. The caller is reconcileTickMemberLocked (it holds reconcileMu,
	// and the arm is a whole-row write); Command is reconcileCmdNone on the same
	// decision, so the arm never races a frame this tick also dispatched.
	ArmHandoverOp string
}

func decisionNone(obs memberObservation, st reconcileState, reason string) reconcileDecision {
	return reconcileDecision{
		Command: reconcileCmdNone, MemberID: obs.MemberID, Reason: reason, State: st,
	}
}

// ── the pure decision function (machine.py decide) ───────────────────────────

// ── the at-least-once robust STOP judgment (T-ed79, shared) ──────────────────
//
// ONE armed out-of-band robust STOP, three possible answers. The judgment behind
// them — what arms it, how long a warden gets before the frame is presumed lost,
// and what counts as "the session we killed is still running" — is the SAME for
// a roster member and for an outsource worker, so BOTH producers read this one
// function rather than each carrying a private copy of the rule. That is the
// whole point of the ticket: a second parallel implementation on the outsource
// side would be exactly the thing this change exists to delete.
//
// Producers:
//   - member: dispatchRobustStopNow arms reconcileState.RobustStopPendingAt, the
//     cadence judges it at the top of reconcileDecide (obs.Online is its `alive`).
//   - worker: stopWorkerSessionOrPark arms workerStopLanded, the outsource tick
//     judges it in retryUnlandedWorkerStop.
type robustStopStep int

const (
	// robustStopDone — the killed session can no longer be shown to be running.
	// Disarm: the marker must never outlive the session it was dispatched for.
	robustStopDone robustStopStep = iota
	// robustStopWait — still running, inside the retry window. A warden is
	// entitled to its kill ladder; re-pushing here is frame spam, not safety.
	robustStopWait
	// robustStopResend — still running past the retry window. The one thing that
	// can be true is that the kill never took: push it again.
	robustStopResend
)

// robustStopRetryStep answers for one armed dispatch. `alive` is the caller's
// evidence that the session THIS STOP was aimed at is still running — the member
// producer reads obs.Online; the worker producer narrows that to "online AND
// still claiming the machine the kill was addressed to", because a respawn puts
// the same worker id back online on a session this STOP never addressed.
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

// reconcileDecide decides the single command for one member. Pure: a function
// of (observation, state, config, now) — the dispatch IO lives in reconcileOne.
func reconcileDecide(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	// Half-open the breaker once its cooldown lapses: fresh retry budget (a
	// single post-cooldown failure does NOT immediately re-open).
	if st.CircuitOpen && now >= st.CircuitCooldownUntil {
		st.CircuitOpen = false
		st.Attempts = 0
		st.BackoffUntil = 0.0
	}
	// ── the unlanded out-of-band robust STOP (T-ed79) ────────────────────────
	// At-least-once for the ONE dispatch path that had no backstop at all. Runs
	// BEFORE the desired-state switch because the member it describes is being
	// collected regardless of which arm would otherwise own it, and because
	// decideUp's converged arm would otherwise wipe the stop bookkeeping every
	// tick.
	//
	// Cleared on the first offline observation: that is both the success signal
	// (the warden killed the session) and the guarantee that the marker can
	// never outlive the session it was dispatched for.
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
				State: st,
				// Kill where the session ACTUALLY is, exactly as
				// dispatchRobustStopNow addressed it (memberKillTargetWarden);
				// "" falls back to wardenTargetOf in reconcileOne.
				DispatchWarden: obs.RunningMachine,
			}
		default: // robustStopWait
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

// recycleGraceFor is the one place that says how long a refocus epoch waits
// before the collection is forced — and whether it is on a clock AT ALL.
//
// Almost nothing is (T-ed79). 重新聚焦, 改機器, model change, the agent's own
// restart_self, the FIRST context threshold and token expiry all have the same
// shape as 下線: the agent is shown the sequence, and the collection is its own
// stopped report or the owner pressing force-stop. Nothing collects them on
// time. The clocked causes are the TWO 加速停止 arms — the SECOND context
// threshold (context_high) and the owner's own press (accelerated_stop) — and
// they get exactly their RecycleGrace seconds, from the SAME setting, because
// winddownKindFor answers for both.
//
// 🔴 The bool is why this returns two values instead of a big number: it and
// offboardKindOf are ONE judgement read from two places — the clock and the
// sentence. Encoding "no clock" as a large grace would leave a deadline that
// still arrives, only later, while the sentence promised there was none; a
// clock nobody announces is worse than the split this ticket removed, because
// the agent takes its time and is then cut off mid-hand-off with no warning at
// all. If you ever put a clock back on this arm, offboardKindOf has to start
// saying so in the same change.
func recycleGraceFor(refocusOp string, cfg reconcileConfig) (grace float64, clocked bool) {
	// 🔴 The WHICH-ARM question is not answered here any more (T-ed79). This
	// function used to fall through to "clocked" for every op except 重新聚焦,
	// while offboardKindOf answered the same question from its own list — two
	// copies of one ruling, in two files, and the split this comment warns
	// about is what happens the day they disagree. winddownKindFor is now the
	// single read; all that is left here is HOW LONG a clocked arm gets.
	if _, clocked := winddownKindFor(refocusOp); !clocked {
		return 0, false
	}
	return cfg.RecycleGrace, true
}

// decideUp — desired_state=online: converge to a live session; recycle takes
// precedence over the converged path; back off on repeated failed starts.
func decideUp(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	// T-9adc: maintain the continuous-offline anchor FIRST, on every path. Any
	// online observation is liveness proof — it clears the anchor, so a member
	// that reconnects inside the zombie-confirm window is never taken over off
	// a stale clock. The first offline observation arms it.
	if obs.Online {
		st.OfflineSince = 0.0
	} else if st.OfflineSince == 0.0 {
		st.OfflineSince = now
	}
	if obs.Online && obs.RefocusSince > 0.0 {
		// RECYCLE (§4.5): a refocus-marked live member is robust-stopped once the
		// agent reports dump-done OR the dump-stuck grace elapses; desired_state
		// stays online the whole time, so the next tick's plain START respawns.
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
					// Kill where the session ACTUALLY is, not where it is wanted.
					// For a plain 重新聚焦 these are the same machine, so nothing
					// changes; T-b6d9 made them differ, because a 改機器 now stamps
					// refocus_since and is collected by THIS arm while the session
					// is still running on the OLD machine. "" (no live claim) falls
					// back to wardenTargetOf in reconcileOne, the prior behaviour.
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
		// RELOCATION (§ owner re-pinned a LIVE member's desired_machine): an online,
		// refocus-free member whose running machine no longer matches its target has
		// to be moved. desired_state stays online the whole time, so once the old
		// session is gone the next tick's plain START re-mints the boot token on the
		// NEW machine (wardenTargetOf routes START by desired_machine).
		//
		// 🔴 THIS ARM WAITS (T-14 #4). It did NOT until this ticket: it robust-
		// STOPped the session on the FIRST pass, with no 預告 and no chance to hand
		// anything over — the exact behaviour T-b6d9 removed from the relocate
		// HANDLER and left standing here, so the front door was graceful and the
		// backstop was not. Owner ruling (2026-08-28/29): 「refocus 怎麼做的 這邊就
		// 怎麼做」,「三種下線都是同一種方案…他們都是不急著下線，等它自然交接完」,
		// 「下線以後再挑機器與 model」. So this arm now opens the SAME wind-down the
		// handler opens (ArmHandoverOp → armMemberOwnerOpHandover, which stamps
		// refocus_since) and hands the member to the refocus arm ABOVE, which waits
		// for the agent's own stopped report and then issues the STOP addressed to
		// the running machine. A 改機器 epoch is UNCLOCKED (winddownKindFor), so the
		// wait is the agent's to end — or the owner's, via 加速停止 / 強制停止.
		//
		// It is still the BACKSTOP, for the divergence nobody stamped for: a member
		// re-pinned while offline that later booted on the old machine, or a pin
		// written by something other than that handler. What changed is only WHAT
		// the backstop does about it.
		//
		// 🔴 AND IT STILL KILLS ON THE FIRST PASS WHEN THERE IS NOTHING TO WAIT FOR.
		// A wind-down can be refused — a warden row runs no ocagent and would never
		// read the marker, and a member already on the 強制停止 rung may not be
		// walked back down the ladder — and asking for a stamp nobody writes would
		// re-decide identically every 30s forever, stranding a live session on the
		// wrong machine with no path off it. obs.HandoverArmable is that question,
		// asked by the real gates (memberOwnerOpHandoverArmable), and the immediate
		// STOP below is the answer when they say no. Waiting for a hand-off that
		// cannot happen is not gentler than killing — it is just never converging.
		//
		// The STOP is routed to the RUNNING machine's warden (DispatchWarden), not
		// the target's, because that is where the session to kill actually lives.
		// Guarded to the SAFE cases ONLY: a pinned target, a KNOWN running machine
		// (never "" — a claim-less/booting member must never be flapped into a
		// STOP→START loop), and an actual mismatch. refocus never reaches here
		// (handled above), so the two recycles never stack.
		if obs.TargetMachine != "" && obs.RunningMachine != "" &&
			obs.RunningMachine != obs.TargetMachine {
			if obs.HandoverArmable {
				// Nothing is dispatched and st.LastCommand is deliberately NOT
				// advanced: the collection is the refocus arm's, and its own
				// first-dispatch/stop_retry bookkeeping must start clean when it
				// takes over on the next tick.
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
		// Converged — reset the failure bookkeeping so the next stop starts clean.
		st.Phase = reconcilePhaseOnline
		st.Attempts = 0
		st.BackoffUntil = 0.0
		st.CircuitOpen = false
		st.CircuitCooldownUntil = 0.0
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
		st.StopDeadline = 0.0
		dec := decisionNone(obs, st, "online: converged")
		// T-39: the ONE producer of ConvergedOnline. Anything the member's row
		// still says about a FAILED operation is describing an attempt this tick
		// has just outlived; the callers clear it here and nowhere else.
		dec.ConvergedOnline = true
		return dec
	}

	// Not online. A START may be in flight — give it the start window.
	// startTimedOut rides out on whichever decision this pass returns (backoff /
	// circuit / the immediate re-START), so the caller can turn the observation
	// into a durable, owner-visible receipt (T-ba62). Purely additive: it changes
	// no decision, only what the tick is allowed to say about one.
	startTimedOut := false
	if st.LastCommand == reconcileCmdStart {
		if obs.LastOpKind == reconcileCmdStart &&
			strings.HasPrefix(obs.LastOpReason, spawnClobberReasonPrefix) {
			// ZOMBIE TAKEOVER: our START bounced off the warden clobber-guard — a
			// live but presence-deaf session (SSE-dead, process alive) squats the
			// slot, so plain respawns bounce off it forever. Dispatch the robust
			// STOP to reap it (warden kill.go stop() ladder: killpg + sweepPIDs
			// signal-0 verify); st.LastCommand flips to stop, so the next tick's
			// plain START lands on a clean slot. Covers wake AND refocus — both
			// converge on this not-online START-clobber path.
			//
			// T-9adc SECOND CONFIRMATION: "presence-deaf zombie" and "live session
			// mid-reconnect" look identical at this instant (the 2026-07-20
			// SSE-blip incident: the takeover STOP raced a session that reconnected
			// on its own — killing it would have vaporised its whole context). So
			// the STOP is withheld until the member has been continuously offline
			// ≥ ZombieConfirmGrace; a reconnect inside the window resets
			// OfflineSince (top of decideUp) and the converged arm stands the
			// takeover down. Bounded: the window lapsing with no reconnect fires
			// the STOP unconditionally — a true zombie is still reaped.
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
		// Silent timeout: under at-most-once delivery a lost frame is
		// indistinguishable from a member that cannot start — backoff-ONLY,
		// never counted toward the sticky breaker (§4.3).
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

// decideDown — desired_state=offline, the one-command model: grace window
// first (dispatch NOTHING), then the SINGLE robust stop, re-dispatched only
// past stop_retry (at-least-once over the at-most-once band).
func decideDown(
	obs memberObservation, st reconcileState, cfg reconcileConfig, now float64,
) reconcileDecision {
	if !obs.Online {
		// Converged offline. Reset the stop bookkeeping (the circuit fields are
		// deliberately left alone — machine.py parity).
		st.Phase = reconcilePhaseOffline
		st.Attempts = 0
		st.BackoffUntil = 0.0
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
		st.StopDeadline = 0.0
		return decisionNone(obs, st, "offline: converged")
	}
	// 🔴 下線 does not run a clock at all any more (owner 2026-08-16, card
	// rc-27d1710174dd option ①: 「不要兜底：只有你按強制下線才收它」).
	//
	// The button now REACHES the agent — it is shown the offboard sequence and
	// asked to work it and stop itself — and the owner's ruling is that the
	// escalation is HIS, not a timer's: 「萬一我發現他不理我，我就按下強制下線」.
	// A clock here would cut off a session that was told there is no countdown,
	// which is the shape this whole ticket exists to remove.
	//
	// What makes that safe to choose is that force-stop is genuinely reachable:
	// the cockpit offers it exactly in the `stopping` state this button puts the
	// member into, and the sweep that used to erase that state mid-close-out is
	// fixed in this same change (T-2123) — without that fix the escalation path
	// would disappear from under him.
	//
	// The collection still happens the instant the agent says it is done: its
	// stopped report dispatches the robust STOP (HandleReportStopped). What is
	// gone is the server deciding that time is up.
	// 🔴 …UNLESS THE OWNER PRESSED 加速停止 ON THIS STOP (T-ed79). Everything the
	// paragraph above says is about the SERVER starting a clock nobody asked for.
	// This one is started by the owner, on the middle rung of his own escalation
	// (停止 → 加速停止 → 強制停止), and the member has ALREADY been told about it:
	// the same winddownKindFor answer makes offboardKindOf return `final`, so the
	// notice quotes exactly this deadline. Asking one function is why the clock
	// and the sentence cannot come apart here the way they used to on the 換手
	// arm.
	//
	// The anchor is stopping_since, which the 加速停止 handler re-stamps as it
	// writes the cause — the owner's grace runs from HIS press, not from a 停止 he
	// may have pressed hours earlier. Past the deadline this falls THROUGH to the
	// robust-stop dispatch at the bottom of this function (including its
	// stop_retry de-dupe), rather than dispatching a second, parallel kill path.
	acceleratedGrace, accelerated := recycleGraceFor(obs.RefocusOp, cfg)
	accelerated = accelerated && obs.StoppingSince > 0.0
	switch {
	case accelerated && now < obs.StoppingSince+acceleratedGrace:
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st,
			"stopping: 加速停止 — within the grace the owner opened; collection is "+
				"the agent's stopped report, or this deadline")
	case accelerated:
		// deadline lapsed → fall through to the robust stop below
	case cfg.SoftOffboardGrace > 0:
		st.Phase = reconcilePhaseStopping
		return decisionNone(obs, st,
			"stopping: agent is working its offboard sequence — collection is the "+
				"agent's stopped report, or the owner's force-stop")
	}
	if !accelerated && st.StopDeadline == 0.0 {
		// The pre-T-a9d6 timed wind-down, reached by SoftOffboardGrace = 0 (a
		// compile-time constant today, not an owner-facing setting): arm the
		// grace clock from OBSERVING the intent, never from a dispatched
		// command.
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

// decideUninstall — desired_state=uninstall (warden members only, T-IUD): no
// grace window (an explicit owner action), same stop_retry dedupe/re-dispatch.
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

// registerStartFailure folds one failed start into the state: bump attempts,
// arm exponential backoff, and — ONLY when circuitEligible (a VERIFIED hard
// failure; no in-tree caller passes true today) — trip the sticky breaker.
func registerStartFailure(
	st reconcileState, cfg reconcileConfig, now float64, circuitEligible bool,
) reconcileState {
	st.Attempts++
	// float math (not a bit shift): attempts grows unboundedly on repeated
	// silent timeouts, and 2^huge must saturate at the cap, never overflow.
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

// ── logging ──────────────────────────────────────────────────────────────────

// reconcileLog emits one producer observability line to stderr (the Python
// _log_reconcile twin) — the always-on control loop must be diagnosable.
func reconcileLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reconcile] "+format+"\n", args...)
}

// ── dispatch (§4.6 — the SseWardenDispatch + make_host_of port) ──────────────

// wardenTargetOf resolves a member id → the warden member id its commands are
// enqueued under (producer.py make_host_of): a warden addresses ITSELF; an
// agent routes to the ACTIVE warden on its desired machine (the machine id IS
// that warden's own member id). A pin that resolves to no active warden — and a
// member with no pin at all — resolves to "": there is no destination.
//
// It used to return the raw pin string here instead, leaning on the downstream
// reachability gate to fail closed on it. That is how "auto" became a host: the
// literal string was handed on as a machine id, IsOnline("auto") was false
// forever, and the member sat wanting to be online while nothing was ever
// dispatched to it — a stall the gate reported as an ordinary unreachable
// warden, indistinguishable from a machine that had merely gone offline.
// Returning "" makes "nowhere to send this" a distinct, nameable answer.
func (s *apiServer) wardenTargetOf(memberID string) string {
	target, err := s.dal.GetMember(memberID)
	if err == nil && target != nil && target.Kind == KindWarden {
		return target.ID
	}
	host := ""
	if target != nil {
		host = target.DesiredMachineID
	}
	if host == "" {
		return ""
	}
	cand, err := s.dal.GetMember(host)
	if err == nil && cand != nil && cand.Kind == KindWarden &&
		cand.RosterStatus == RosterStatusActive {
		return cand.ID
	}
	return ""
}

// enqueueWardenFrame pushes one command frame onto the target member's warden
// FIFO, fail-closed behind the target-reachability gate: the addressed warden
// must itself be online (hold the live SSE downstream that drains the frame),
// otherwise nothing is enqueued and the caller keeps prior state (no phantom
// START, no ghost STOP into a dead buffer). Returns accepted.
func (s *apiServer) enqueueWardenFrame(memberID string, frame []byte) bool {
	return s.enqueueToWarden(memberID, s.wardenTargetOf(memberID), frame)
}

// memberKillTargetWarden is the member twin of resolveWorkerKillTarget: the
// warden a KILL for this member must be addressed to. A kill names a running
// session, and a session lives on the machine whose SSE claim carries it
// (hub.MachineOf) — not on the machine the member is pinned to, which is a
// statement about where it should run NEXT. Honest fallback to the pin when
// nothing claims the member (no live connection / an older agent that sends no
// machine claim), which is exactly what this used to do unconditionally.
func (s *apiServer) memberKillTargetWarden(memberID string) string {
	if running := s.hub.MachineOf(memberID); running != "" {
		return running
	}
	return s.wardenTargetOf(memberID)
}

// enqueueToWarden pushes one frame onto an EXPLICIT warden's FIFO behind the
// same fail-closed reachability gate as enqueueWardenFrame. Split out so a
// relocation STOP can address the RUNNING machine's warden directly (the
// session to kill lives there) instead of the member's desired-machine warden.
// memberID rides only the fail-closed log line so an unreachable-warden drop
// names WHICH member's dispatch stalled (r-72/fbc5280 dropped it; T-8655 re-adds
// — warden id == machine id alone can't tell you the stuck member on relocate).
// Returns accepted.
func (s *apiServer) enqueueToWarden(memberID, warden string, frame []byte) bool {
	if warden == "" || !s.hub.IsOnline(warden) {
		reconcileLog("%s: target warden %q NOT reachable (no live SSE downstream) — "+
			"fail-closed, not dispatching, will retry when the warden connects",
			memberID, warden)
		return false
	}
	// Tagged with the member/worker this frame acts on: one machine's FIFO is
	// shared by everybody placed there, and a per-subject receipt may only be
	// written from a per-subject observation (hub.PendingWardenCommandsFor).
	s.hub.EnqueueWardenCommandFor(warden, memberID, frame)
	return true
}

// buildStartFrame assembles the START wire frame server-side (producer.py
// BootstrapStartPayload): fold the persona via the shared boot core + mint the
// member JWT. FAIL-CLOSED (nil, false) on an inactive member, an unknown
// role, or a missing secret — never boot a ghost or a deaf member.
func (s *apiServer) buildStartFrame(m Member) ([]byte, loreSurfacing, bool) {
	var lore loreSurfacing
	if m.RosterStatus != RosterStatusActive {
		return nil, lore, false
	}
	if len(s.secret) == 0 {
		return nil, lore, false
	}
	boot, err := s.buildBootContext("", &m)
	if err != nil || boot == nil {
		if err != nil {
			reconcileLog("START fold failed for %q: %v", m.ID, err)
		}
		return nil, lore, false
	}
	lore = boot.Lore
	token, err := s.mintMemberToken(m, s.agentTokenTTLValue())
	if err != nil {
		reconcileLog("START mint failed for %q: %v", m.ID, err)
		return nil, lore, false
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
		return nil, lore, false
	}
	// 🔴 THE 對象目錄 RECEIPT RIDES OUT WITH THE FRAME AND IS FILED BY THE
	// DISPATCHER, NOT HERE (T-33). A built frame is not a delivered document:
	// the enqueue below this can still fail closed on an unreachable warden, and
	// the next tick simply folds again. Journalling at build time would count
	// one boot as many.
	return frame, lore, true
}

// buildTargetFrame builds the member_id-only command frame (STOP / UNINSTALL —
// dispatch.py command_frame: {"rpc": ..., "args": {"member_id": ...}}).
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

// wardenTargetArgs is the STOP/UNINSTALL args shape (spec/sse.md §7): the
// warden keys the kill/removal on member_id alone.
type wardenTargetArgs struct {
	MemberID string `json:"member_id"`
}

// machineLacksClaudeButHasCodex answers, from what the machine ITSELF reported,
// whether "install/log into claude here" is a dead end on it while Codex is a
// live option. Three separate positives are required, and every unknown answers
// false:
//
//   - the machine has reported capabilities at all (a pre-capability warden, or
//     one that has not heartbeat yet, tells us nothing);
//   - it reported a claude entry whose `installed` is MEASURED false — the shape
//     cli/ocwarden/runtimeprobe.go emits on a codex-only box, which always sends
//     the claude key. A missing entry is not a measurement;
//   - codex on that same machine is actually ready (runtimeCapabilityReady), so
//     the option we are about to name can really be taken.
//
// The asymmetry is deliberate. Telling the owner of a codex-only box to go check
// claude is a DEAD END — they can follow it forever and never arrive. Telling
// the owner of a box that does have claude to go change the member's 執行環境 is
// an ACTIVE MISDIRECTION: it asks them to abandon a runtime that is present and
// to make a roster change that was never the problem. So the Codex sentence is
// spoken only on positive evidence, and everything else keeps today's wording.
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

// wakeTimeoutReason is the sentence stampWakeObservability writes when a START
// lapsed its start window — the LAST thing an owner reads on 「最近操作」 for a
// member that will not boot.
//
// T-b3d0 taught the spawn-time refusal to name three exits (switch this member
// to Codex / install Claude Code / re-point OC_CLAUDE_BIN). That refusal is an
// EXECUTION receipt, and this stamp OVERWRITES it the moment the window lapses:
// on a codex-only machine the owner's final on-screen sentence went back to
// "check that claude runs and is logged in", a road that has no end on a box
// with no claude. Naming the third exit here is what makes the sentence the
// owner actually keeps a sentence they can act on.
//
// The runtime is named rather than hardcoded to "claude" for the same reason:
// a Codex member that fails to boot must not be sent to inspect a runtime it
// does not use. NormalizeRuntime("") is claude, so an unset member reads exactly
// as it does today.
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

// runtimeCapabilityReady is the HONEST readiness read of ONE reported runtime
// entry: installed, and not known-logged-out.
//
// Deliberately NOT machineSupportsRuntime. That gate's Claude arm is
// permissive by contract (it returns true for any reported claude entry, so the
// OC_CLAUDE_CRED_CHECK=0 escape hatch keeps working), and a codex-only box does
// report one: cli/ocwarden/runtimeprobe.go ALWAYS emits the claude key, as
// {"installed": false}. Choosing a runtime off that permissive read would pick
// claude on exactly the machines this resolution exists to serve.
func runtimeCapabilityReady(c RuntimeCapabilityDTO) bool {
	if c.Installed == nil || !*c.Installed {
		return false
	}
	return c.LoggedIn == nil || *c.LoggedIn
}

// resolveEmptyRuntimeForPlacement fills a member's UNSET runtime from what the
// target machine reports, and persists the choice on the roster row.
//
// WHY HERE. The out-of-box assistant is seeded with no runtime at all
// (seedOutOfBox), and it must not be born hard-wired to Claude: it should run
// whatever the box it is placed on actually has. The seed cannot make that
// call — seedOutOfBox runs at migrate and at serve start, before any warden has
// ever reported capabilities, so there is nothing to read. Placement is the
// first moment BOTH facts exist (which machine, and what that machine has) and
// the last moment before buildStartFrame stamps the runtime onto the wire.
//
// A member whose runtime is already set is never touched: this resolves an
// ABSENT choice, it never overrides the owner's.
//
// NO HEARTBEAT YET (len(capabilities) == 0) => LEAVE IT UNSET, i.e. keep
// today's behaviour exactly (NormalizeRuntime("") == claude, and
// machineSupportsRuntime's legacy-warden arm lets it through). The two possible
// mistakes are not equivalent. Refusing here would make a machine that was just
// installed and has not reported yet unusable for every member — a NEW way to
// be stuck. Letting it through only returns to the path that already exists
// today, and that path's spawn-time failure now names the third option
// (switch this member to Codex) instead of only telling the owner to go get
// claude.
func (s *apiServer) resolveEmptyRuntimeForPlacement(m *Member, warden string) {
	if m.Kind == KindWarden || strings.TrimSpace(m.Runtime) != "" {
		return
	}
	capabilities := s.machineRuntimeCapabilities(warden)
	if len(capabilities) == 0 {
		return
	}
	// LEGACY-WARDEN CLAUDE SHAPE => UNKNOWN, NOT "NO".
	//
	// `claude: {installed:true, logged_in:false}` is a shape no current warden
	// can produce. collectRuntimeCapabilities (cli/ocwarden/runtimeprobe.go) is
	// evidence-only for Claude: it has no login probe, only two presence checks
	// (credential file, keychain item), so finding neither OMITS the key rather
	// than asserting false. An explicit false on the claude entry therefore
	// dates the reporter — it can only come from a warden older than the
	// evidence-only fix, which shipped in v0.5.211-beta.1.
	//
	// Reading that stale guess as "this box cannot run Claude" is exactly the
	// mistake that already happened once, and the probe's own comment records
	// it: the spawn-side gate honours FOUR env-carried credential sources this
	// probe never looks at (two direct keys plus the Bedrock / Vertex
	// managed-auth flags, where no local claude login exists at all), so hosts
	// on Bedrock or Vertex reported logged_in:false and were PERSISTED as
	// codex — "a machine that could have run claude perfectly well, pinned to
	// the other runtime with no way back". Before T-ae8b that cost one member
	// (the out-of-box assistant, the only row ever born UNSET). T-ae8b makes
	// EVERY hire and EVERY new role born UNSET, so the same stale false would
	// now silently pin every future member on that machine.
	//
	// So: decline to choose, and leave the runtime unset. That is not a
	// refusal — unset normalizes to claude and machineSupportsRuntime's claude
	// arm is permissive by contract, so the START still goes out. Either it
	// launches (env-carried credential, or OC_CLAUDE_CRED_CHECK=0) and the
	// stale false is revealed as the guess it was, or it fails at spawn with
	// claude_not_logged_in, which names the escape routes including "set this
	// member's 執行環境 to Codex". A visible, reversible failure beats an
	// invisible irreversible guess; the owner keeps the choice either way.
	//
	// Codex gets no such grace and must not: `codex login status` is a real
	// command, so its false is a MEASUREMENT, not an absence of evidence.
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
		// Nothing on this box is ready. Persisting a guess would freeze the
		// wrong answer onto the roster row forever; leave it unset and let the
		// placement gate right below refuse and stamp the reason.
		return
	}
	fresh, err := s.dal.GetMember(m.ID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	if strings.TrimSpace(fresh.Runtime) != "" {
		// A concurrent owner edit picked one mid-tick; theirs wins.
		m.Runtime = fresh.Runtime
		return
	}
	m.Runtime = resolved
	fresh.Runtime = resolved
	// runtime left PutMember's DO UPDATE SET in T-55, so a whole-row write here
	// would persist nothing at all. It should not be one anyway: this runs on
	// the reconcile tick, next to HTTP faces writing member rows from their own
	// snapshots, and it only ever means to move this one column. The member
	// delta putMember used to fan for free is re-issued explicitly — without it
	// the cockpit would keep showing the unresolved runtime until some unrelated
	// write happened to land.
	if err := s.dal.SetMemberRuntime(m.ID, resolved); err != nil {
		reconcileLog("%s: runtime resolution persist failed: %v", m.ID, err)
		return
	}
	s.publishMemberPatch(*fresh, triggerServer)
}

// ── decide → dispatch (controller.py ServerReconciler.reconcile_one) ─────────

// reconcileOne runs one member's decide → dispatch. A dispatch that is not
// accepted (or a START with no assemblable payload) is DOWNGRADED to a no-op
// decision whose state is NOT advanced, so the next tick retries — the
// producer never records a command it did not deliver.
func (s *apiServer) reconcileOne(m Member, st reconcileState, now float64) reconcileDecision {
	obs := memberObservation{
		MemberID:       m.ID,
		Desired:        parseDesired(m.DesiredState),
		Online:         s.hub.IsOnline(m.ID),
		RefocusSince:   m.RefocusSince,
		RefocusOp:      m.RefocusOp,
		StoppingSince:  m.StoppingSince,
		AgentStopped:   m.StoppedSince > 0.0,
		LastOpKind:     m.LastOp,
		LastOpReason:   m.LastOpReason,
		TargetMachine:  m.DesiredMachineID,
		RunningMachine: s.hub.MachineOf(m.ID),
		// Asked here, not inside the decider, for the reason every other field on
		// this struct is: the decider is pure and this question needs the hub and
		// the row. The relocation backstop reads it to choose between opening a
		// wind-down and killing on the spot — see memberObservation.HandoverArmable.
		HandoverArmable: s.memberOwnerOpHandoverArmable(m, memberOpRelocate),
	}
	decision := reconcileDecide(obs, st, s.reconcileConfigLive(), now)
	switch decision.Command {
	case reconcileCmdNone:
		return decision
	case reconcileCmdStart:
		warden := s.wardenTargetOf(m.ID)
		if warden == "" {
			// No machine chosen, or the chosen one is no longer an active machine.
			// A member in this state wants to be online and can never be dispatched,
			// so say so on the row the cockpit reads instead of retrying in silence
			// every 30s — the worker arm of this rule lives in
			// stampWorkerPlacementBlocked.
			s.stampMemberPlacementBlocked(&m, now)
			decision.Command = reconcileCmdNone
			decision.Reason = "no machine selected"
			decision.State = st
			decision.DispatchUnlanded = true
			return decision
		}
		// The target machine is now known and the START frame is not built
		// yet: this is the last point that can still choose a runtime, and the
		// first one that knows what this box actually reports (T-b3d0).
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
		frame, lore, ok := s.buildStartFrame(m)
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
			decision.State = st // keep the prior state → re-dispatch next tick
			decision.DispatchUnlanded = true
			return decision
		}
		// The frame is on the warden's queue, so the member is really going to
		// read the directory that was folded into it: that is the moment the
		// surfacing becomes true, and the moment it is journalled (T-33).
		s.recordLoreSurfacing(lore)
		// A landed START begins a NEW session: drop any prior session's boot_ts
		// anchor so the fresh agent's first connect re-stamps (T-8fb2 boot_ts fix).
		s.clearSessionBootTS(m.ID)
		// The warden now owes a command_result for this START. Armed AFTER the
		// accepted enqueue only — an unlanded frame is already explained by its
		// own dispatch stamp (receipt_watch.go).
		s.armReceiptWatch(m.ID, reconcileCmdStart, warden, now)
		return decision
	default: // STOP / UNINSTALL — member_id-only frames, same retry discipline
		frame, ok := buildTargetFrame(decision.Command, m.ID)
		// A relocation STOP routes to the RUNNING machine's warden (DispatchWarden);
		// every other command routes via wardenTargetOf (the desired machine).
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
		// A landed STOP/UNINSTALL ends the current session (graceful desired-offline
		// stop, or the relocation STOP routed to the running machine's warden): drop
		// its boot_ts so a later respawn's first connect re-stamps (T-8fb2).
		s.clearSessionBootTS(m.ID)
		// STOP only (receipt_watch.go). UNINSTALL is deliberately NOT watched:
		// its receipt is already load-bearing on the warden side (the warden
		// blocks on delivery and refuses to self-exit without a 2xx), and the
		// reconcile keeps re-issuing it — an undelivered uninstall retries
		// rather than going quiet, which is the failure mode this watch exists
		// for.
		if decision.Command == reconcileCmdStop {
			warden := decision.DispatchWarden
			if warden == "" {
				warden = s.wardenTargetOf(m.ID)
			}
			s.armReceiptWatch(m.ID, reconcileCmdStop, warden, now)
		}
		return decision
	}
}

// reconcileTickMemberLocked reconciles ONE member against the shared store and
// persists its next state. Caller MUST hold reconcileMu.
func (s *apiServer) reconcileTickMemberLocked(m Member, now float64) reconcileDecision {
	st, ok := s.reconcileStates[m.ID]
	if !ok {
		st = newReconcileState()
	}
	decision := s.reconcileOne(m, st, now)
	s.reconcileStates[m.ID] = decision.State
	reconcileLog("%s: desired=%s command=%s — %s",
		m.ID, parseDesired(m.DesiredState), decision.Command, decision.Reason)
	s.armDecidedHandover(m.ID, decision)
	s.stampWakeObservability(&m, decision, now)
	// AFTER the wake receipt, and it yields to it: stampWakeObservability owns the
	// EXECUTION-level diagnosis ("the start went out and the agent never came
	// up"), which is strictly more informative than this tick's DISPATCH-level one
	// ("we are in back-off"). They collide on the very tick a lapse turns into
	// back-off, and the single last_op_reason slot can hold only one.
	if !decision.StartTimedOut {
		s.stampMemberOpBlocked(m.ID, decision.ReasonCode, now)
	}
	return decision
}

// armDecidedHandover executes the ONE durable write a reconcile decision can
// ask for: opening a wind-down epoch on the member's row (T-14 #4). It is the
// dispatch half of reconcileDecision.ArmHandoverOp, and it does nothing at all
// on the decisions — nearly all of them — that ask for none.
//
// It re-reads before writing, for the reason every other stamp in this file
// does: this is a whole-row write and the HTTP faces (activate / relocate /
// deactivate) write member rows without holding reconcileMu, so persisting the
// tick's snapshot would silently revert a change that landed mid-tick — here on
// desired_machine_id itself, the very field the epoch is being opened about.
//
// 🔴 A REFUSAL HERE IS SAFE, and that is a property of the arm that asked, not
// of this function. The decider only asks when obs.HandoverArmable said the
// same gates would pass; if the re-read row has moved on and they now refuse,
// nothing is written and the NEXT tick re-decides from that fresher row — which
// is the row the gates just answered about, so the two agree and the member
// takes whichever arm it now belongs on (the immediate STOP, or none at all).
// The loop cannot repeat with the same answer twice.
//
// Best-effort by the stampWakeObservability rule: a persistence failure is
// logged and changes no decision. It takes no `now`: armMemberOwnerOpHandover
// stamps from nowSecs() like every other epoch site, and threading the tick's
// clock in would create a second one. Caller MUST hold reconcileMu.
func (s *apiServer) armDecidedHandover(memberID string, decision reconcileDecision) {
	if decision.ArmHandoverOp == "" {
		return
	}
	fresh, err := s.dal.GetMember(memberID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	if !s.armMemberOwnerOpHandover(fresh, decision.ArmHandoverOp) {
		return // it logged which gate refused; the next tick re-decides
	}
	if err := s.putMember(*fresh, triggerServer); err != nil {
		reconcileLog("%s: %s wind-down arm persist failed: %v",
			memberID, decision.ArmHandoverOp, err)
	}
}

// stampOpReceipt is THE five-column receipt a failed-or-deferred op leaves on a
// row. A Member and an OutsourceWorker carry those five columns under the same
// names, so every writer of them was the same five assignments written out by
// hand; what is left at each site is which struct the columns come off, and the
// guards around the write.
//
// 🔴 IT IS NOT "THE ONLY SPELLING OF THE RECEIPT" — an earlier draft of this
// comment said that and it was false, which is the failure this paragraph exists
// to stop repeating. The draft AFTER that one then said "every production writer
// of the five columns either calls this function or carries a RECEIPT-CORE-AUDIT
// anchor", and it was false in exactly the same over-wide way: foldCommandResult
// and foldWorkerCommandResult (api_monitoring.go) each write all five, call
// nothing here, and carry no anchor. Worse, the recipe that draft handed out for
// re-checking (`OpOK = &ok` / `OpOK: &ok`) could not have found them at all —
// both spell their bool okPtr/okVal, so the recipe's reach was narrower than the
// claim it was supposed to back. A guard whose claimed range exceeds its actual
// range is the disease, not the cure.
//
// What this function IS the single source of is narrower: the SERVER-AUTHORED
// REFUSAL receipt — "the change was saved and nothing was started" — a non-nil
// FALSE the server decided on its own, with last_op_log cleared. The two folds
// are a DIFFERENT CLASS and must not be routed through here: they carry the
// AGENT's own verdict off the wire (three-valued, and a success as readily as a
// failure) together with the log that came with it. A core that hard-writes false
// and blanks the log would destroy the exact thing they exist to record.
//
// RECEIPT-CORE-AUDIT is the grep anchor for exceptions WITHIN the refusal class.
// Today exactly one carries it — stampWakeObservability, below — and its anchor
// says why.
//
// To re-check, grep `LastOpOK` over non-test .go — the COLUMN name, because the
// `&ok` spelling is incidental and the next writer is free to name its bool
// anything. Over the tree this comment lives in that is 31 hits, 2 of them inside
// this comment block (the recipe finds its own subject; the previous one did
// not). It was 27 before T-39 added the two converged clears, two lines each. Of
// the rest, the hits that WRITE the column are:
//   - seven `stampOpReceipt(&….LastOpOK, …)` call sites — reconcile.go ×3,
//     receipt_watch.go ×2, worker_spawn.go ×2: the refusal class, all of it here;
//   - stampWakeObservability below — refusal class, standing apart, anchored;
//   - api_monitoring.go ×2 — the agent-verdict class described above;
//   - seven that stamp no receipt at all: dal.go's row scan, this file's copy of
//     an already-stamped row onto a freshly re-read one, and FIVE lines that
//     clear back to nil — worker_spawn.go's clearWorkerPlacementBlock, plus the
//     two T-39 converged clears (stampWakeObservability below and
//     worker_spawn.go's clearWorkerConvergedFailureReceipt), each of which reads
//     the column to test for a FAILURE and then writes nil.
//
// 🔴 A CLEAR IS NOT A RECEIPT, and it must not be routed through this core: the
// core always leaves a non-nil FALSE with a reason, the clears leave nil with
// none. That is a different operation, not an exception to this one.
//
// Everything else the grep returns is a read or a struct field declaration. That
// enumeration is the claim, and re-running the grep is what checks it. It does
// NOT claim a future writer will announce itself: nothing here executes, so a new
// hand-written receipt can appear and this paragraph will not notice. Making the
// recipe executable is a later stage's work, not this one's.
//
// 🔴 IT TAKES FIELD POINTERS, NOT A ROW, ON PURPOSE. The obvious tidier shape —
// lift the five columns into an embedded struct both rows share — reaches the
// DAL: scanMember/PutMember and their outsource twins list these columns
// positionally, so the embedding would have to be threaded through every scan
// and put site to remove five duplicated assignments. The pointer core buys the
// single source at the cost of one long signature, and touches no persistence
// code at all.
//
// The OP VERB is a parameter, not a constant: the reconcile-side writers all
// stamp a START, and stampReceiptMissing (receipt_watch.go) stamps whichever RPC
// the lapsed watch was waiting on. That was the whole of the difference between
// those two families, and it is why folding the watch's two arms in was possible
// at all — they were the same five lines twice inside ONE function, once for the
// member row and once for the worker row.
//
// last_op_ok is three-valued on both rows (nil = nothing folded yet) and this
// always leaves a non-nil FALSE: what this function stamps is a refusal or a
// deferral — "the change was saved and nothing was started" — never a success.
// last_op_log is cleared with it, because the log belongs to the op being
// replaced and reading a fresh reason beside a stale log is worse than reading
// neither. Sentinels: one per calling site, each pinned to ABSOLUTE values
// rather than to another site's values, so a change here reddens all of them —
// TestStampMemberOpReceipt_WritesTheFiveReceiptFields,
// TestStampWorkerOpReceipt_WritesTheFiveReceiptFields, and the
// receipt_core_sites_t170e_test.go family for the stamps that persist.
func stampOpReceipt(lastOp *string, lastOpOK **bool, lastOpLog, lastOpReason *string,
	lastOpAt *float64, op, reason string, now float64) {
	ok := false
	*lastOp = op
	*lastOpOK = &ok
	*lastOpLog = ""
	*lastOpReason = reason
	*lastOpAt = now
}

// stampMemberOpReceipt writes one op receipt onto an IN-MEMORY member the caller
// is about to persist itself. It exists so an HTTP handler's explanation and the
// change it explains are ONE row write and ONE delta — the same reason
// armRefocusEpoch mutates instead of persisting. The receipt itself is
// stampOpReceipt's; this is the staff shell.
func stampMemberOpReceipt(m *Member, reason string, now float64) {
	stampOpReceipt(&m.LastOp, &m.LastOpOK, &m.LastOpLog, &m.LastOpReason, &m.LastOpAt,
		reconcileCmdStart, reason, now)
}

// isStopgapRetryReason reports whether a reason code is the retry loop
// DESCRIBING ITS OWN WAIT rather than diagnosing anything — the only class the
// single-slot precedence rule in stampMemberOpBlocked yields to. Both members
// are produced by decideUp's pacing arms and both re-derive every 30s for as
// long as the stall lasts, which is what makes them able to blank a diagnosis
// they know nothing about.
func isStopgapRetryReason(reason string) bool {
	return strings.HasPrefix(reason, spawnReasonBackoff+":") ||
		strings.HasPrefix(reason, spawnReasonCircuitOpen+":")
}

// stampMemberOpBlocked records WHY a staff member the owner wants running is not
// running, on the row the cockpit already reads — the staff twin of
// stampWorkerPlacementBlocked, and the production end of T-ed79 #14.
//
// The contract-level parts (the last_op* fields, the isPlacementBlockedReason
// clearing seam, the cockpit renderer) were ALREADY shared between the two
// sides; what staff never had was anybody writing. decideUp reached "circuit
// open", "backoff" and "zombie suspect" and reconcileLog'd each one to the
// server's stderr, where no owner has ever looked. From the cockpit all three
// were the same picture: a grey row.
//
// A BLANK code is a no-op, deliberately — see reconcileDecision.ReasonCode. This
// function never clears: clearing belongs to stampWakeObservability's landed-START
// path, which already drops any placement-blocked reason, and a converged tick
// silently blanking the row would erase the diagnosis one tick after it appeared.
//
// Written only when the cause CHANGES (the anti-churn rule the worker stamp
// documents at length: the cadence re-decides the same stall every 30s, so an
// unconditional write would re-stamp last_op_at and fan a delta on every tick).
// Re-read before the whole-row write, for the reason every other stamp here does.
// Best-effort: a persist failure is logged and changes no decision.

func (s *apiServer) stampMemberOpBlocked(memberID, reason string, now float64) {
	if reason == "" {
		return
	}
	fresh, err := s.dal.GetMember(memberID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
		return // already stamped with this exact cause — do not churn the row
	}
	// 🔴 THE ONE PRECEDENCE RULE, and it is the family's own (worker_spawn.go, the
	// codes deliberately outside spawnBlockedReasonCodes): a diagnosis of the
	// PREVIOUS attempt must not be erased by a description of the current wait.
	// wake_timeout says the start was dispatched and the agent never came up; it
	// is followed, one tick later, by exactly the back-off this function would
	// stamp — so without this rule the retry loop would blank the only sentence
	// that says what actually went wrong, every time, which is the trap 31751ae
	// fixed for workers.
	//
	// 🔴 IT TURNS ON THE INCOMING CODE, NOT ONLY ON WHAT IS ON THE ROW. The rule
	// is about the RETRY LOOP, and only backoff/circuit_open are the retry loop
	// describing its own wait. zombie_suspect and the activate handler's
	// warden_unreachable are fresh, actionable findings about what is wrong NOW —
	// strictly more informative than a wake_timeout from an attempt that has
	// already failed — and a guard that read only the row swallowed those too,
	// leaving the owner a stale sentence while the live fault went unsaid.
	if isStopgapRetryReason(reason) &&
		strings.HasPrefix(fresh.LastOpReason, wakeTimeoutReasonCode+":") {
		return
	}
	stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
		&fresh.LastOpAt, reconcileCmdStart, reason, now)
	if err := s.putMember(*fresh, triggerServer); err != nil {
		reconcileLog("%s: op-blocked stamp persist failed: %v", memberID, err)
	}
}

// stampMemberPlacementBlocked names, on the member row the cockpit reads, the one
// stall the wake path could not previously explain: the member is wanted online
// but has no machine to be sent to. Every other START failure already leaves a
// trace (a lapsed start_timeout writes a receipt; an unbuildable frame logs and
// backs off), while an unplaced member simply retried forever against nothing —
// grey and unexplained, identical to a member nobody ever woke.
//
// Written only when the cause CHANGES, because the cadence re-decides this same
// START every 30s: an unconditional write would re-stamp last_op_at and fan a
// member delta on every tick. stampWakeObservability clears the stamp when a START
// finally lands, so a block that FOLLOWS a landed start is news again instead of
// being mistaken for the still-standing first one.
//
// The row is RE-READ before writing: this is a whole-row write on the snapshot
// the cadence tick loaded, and the HTTP faces (activate / relocate / deactivate)
// write member rows without holding reconcileMu — persisting the stale copy would
// silently undo a relocate that landed mid-tick, on the very field this stall is
// about. Best-effort — a persist failure never changes the reconcile decision.
func (s *apiServer) stampMemberPlacementBlocked(m *Member, now float64) {
	fresh, err := s.dal.GetMember(m.ID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	// The reason names the pin on the row being WRITTEN, not the one the tick
	// snapshotted: a relocate landing mid-tick would otherwise stamp a row pinned
	// to the new machine with a complaint about the old one.
	reason := placementReasonNoMachine + ": no machine is selected for this member — " +
		"choose one (改機器) before waking it; there is no automatic placement"
	if fresh.DesiredMachineID != "" {
		reason = placementReasonUnavailable + ": machine '" + fresh.DesiredMachineID +
			"' is not an active machine — choose another one (改機器); " +
			"no other machine is substituted"
	}
	if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
		return
	}
	stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
		&fresh.LastOpAt, reconcileCmdStart, reason, now)
	if err := s.putMember(*fresh, triggerServer); err != nil {
		reconcileLog("%s: placement-blocked stamp persist failed: %v", m.ID, err)
	}
}

// stampWakeObservability turns two SERVER-SIDE facts about a wake into durable,
// owner-visible state (T-ba62). Both were previously invisible outside the
// server's stderr:
//
//	(a) a LANDED START stamps waking_since. Until now the ONLY writer of that
//	    anchor was the agent's own report_waking — i.e. it was stamped only by
//	    agents that successfully booted. An agent that never came up left it at
//	    zero, so PresenceState projected plain "offline": the failed wake and the
//	    member nobody ever woke rendered IDENTICALLY. Stamping at dispatch means
//	    "waking" now means what it says — the server asked, and the
//	    WakingTTLSecs window is the honest deadline. Only stamped when the frame
//	    was actually accepted by a warden: an undispatched START must not claim
//	    the member is waking.
//	(b) a START that lapsed its start_timeout writes a last_op receipt, which is
//	    what the cockpit's 「最近操作」 reads. Without it the lapse only ever
//	    existed as exponential backoff inside the reconcile state.
//	(c) a member that has CONVERGED back online CLEARS a failure receipt (T-39).
//	    The counterpart to (b), and it was missing: (b) writes while the member
//	    is broken and nothing was watching for it becoming un-broken, so the red
//	    line the owner sees had no way to end except a second failure.
//
// Best-effort by contract: a persistence failure is logged and never changes the
// reconcile decision — observability must not be able to stall the control loop.
func (s *apiServer) stampWakeObservability(m *Member, decision reconcileDecision, now float64) {
	changed := false
	if decision.StartTimedOut {
		// RECEIPT-CORE-AUDIT: deliberately NOT stampOpReceipt, and this is not an
		// oversight. This writer touches FOUR of the five receipt columns — it
		// never clears last_op_log. Routing it through the core would add that
		// clear, which is a BEHAVIOUR CHANGE (a wake that lapses would blank the
		// previous op's log instead of leaving it standing) and therefore outside
		// a convergence-only change. It also composes last_op_reason further down
		// (the T-66a2 undelivered-frame rewrite) after the stamp, so the "one
		// reason in, one reason out" shape the core assumes does not hold here.
		// Whether the log SHOULD be cleared here is a real question and a
		// behaviour decision — it belongs to a later stage, not to this one.
		ok := false
		m.LastOp = reconcileCmdStart
		m.LastOpOK = &ok
		m.LastOpAt = now
		m.LastOpReason = s.wakeTimeoutReason(*m)
		// T-66a2: the sentence above is only true when the frame actually
		// reached the machine. If the warden's stream died mid-delivery the
		// frame was dropped server-side and NOTHING on the target machine was
		// ever asked to start — telling the owner to go inspect claude there
		// sends them to the wrong machine, which is worse than silence. The
		// note is anchored to THIS wake (WakingSince), so an older loss can
		// never explain the attempt now lapsing.
		if note, lost := s.hub.UndeliveredCommandSince(m.ID, m.WakingSince); lost &&
			note.Verb == reconcileCmdStart {
			m.LastOpReason = fmt.Sprintf(
				wakeTimeoutReasonCode+": the START never reached machine %q — its SSE stream "+
					"failed mid-delivery and the frame was dropped server-side, so "+
					"nothing on that machine was ever asked to start; do not go "+
					"looking at claude there, the machine's connection is the suspect",
				note.Warden)
		}
		// The anchor is stale by construction here; clearing it lets the member
		// read plain offline again instead of a forever-"waking" lie.
		m.WakingSince = 0.0
		changed = true
	}
	// A DECIDED start is by construction a LANDED one: reconcileOne downgrades an
	// unaccepted START to reconcileCmdNone (with DispatchUnlanded set) before it
	// ever returns, so `Command == start` already means "a warden took the
	// frame". An extra `&& !DispatchUnlanded` here would read as caution but is
	// a tautology — a mutation probe proved flipping it could not change any
	// outcome — and a condition that cannot fail is worse than no condition: it
	// advertises a check nobody is performing. The invariant it leans on is
	// pinned by TestReconcile_UnlandedStartDoesNotStampWakingSince.
	if decision.Command == reconcileCmdStart {
		m.WakingSince = now
		changed = true
		// The START landed, so a PLACEMENT-blocked explanation is now history —
		// leaving it would make the next block look like the still-standing first
		// one. Only a placement stamp is cleared HERE: the wake_timeout receipt
		// written above, and a warden's own refused-start receipt, are the record
		// of why a boot failed and survive the retry that follows them — a retry
		// is not an outcome, and blanking them on dispatch would delete the only
		// sentence saying what went wrong while it is still going wrong.
		//
		// 🔴 THEY NO LONGER SURVIVE FOREVER, and this sentence used to say they
		// did by omission (T-39). What ends them is the arm below: the member
		// actually coming back. That is the difference the old shape could not
		// express — it had exactly one clearing gate, "we tried again", so
		// "cleared" and "healthy" were mutually exclusive and the receipt could
		// outlive its failure indefinitely (10.6 days, on a member that was
		// online the whole time). Dispatch still does not clear them; CONVERGENCE
		// does.
		if isPlacementBlockedReason(m.LastOpReason) {
			m.LastOpReason = ""
			m.LastOpLog = ""
		}
	}
	// T-39 — THE MEMBER CAME BACK, SO THE FAILURE RECEIPT GOES.
	//
	// 🔴 IT RETURNS. Both arms above are UNREACHABLE on a converged tick and that
	// is structural, not luck: StartTimedOut is only ever observed on the
	// not-online path, and a decision that carries ConvergedOnline is by
	// construction Command == none. So there is never a wake stamp to persist
	// alongside this clear, and the whole-row copy below — which would otherwise
	// splat this tick's SNAPSHOT of all five receipt columns onto a freshly re-read
	// row — must not run for it. The clear does its own re-read and its own put.
	if decision.ConvergedOnline {
		s.clearMemberConvergedFailureReceipt(m.ID, *m)
		return
	}
	if !changed {
		return
	}
	// Re-read before the whole-row write, for the reason the placement stamps do:
	// this is the tick's snapshot, and the HTTP faces (activate / relocate /
	// deactivate) write member rows without holding reconcileMu, so persisting the
	// snapshot silently reverts a placement that landed mid-tick. Narrowing, not
	// eliminating — the read and the write are not atomic — but the window shrinks
	// from the whole tick to a few statements.
	fresh, err := s.dal.GetMember(m.ID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	fresh.WakingSince = m.WakingSince
	fresh.LastOp = m.LastOp
	fresh.LastOpOK = m.LastOpOK
	fresh.LastOpLog = m.LastOpLog
	fresh.LastOpReason = m.LastOpReason
	fresh.LastOpAt = m.LastOpAt
	if err := s.putMember(*fresh, triggerServer); err != nil {
		reconcileLog("%s: wake observability persist failed: %v", m.ID, err)
	}
}

// receiptRendersAsFailure answers the ONE question the T-39 clears are allowed
// to turn on: WOULD THE COCKPIT PAINT THIS ROW AS A FAILED OPERATION RIGHT NOW.
//
// 🔴 IT MIRRORS THE PANEL, NOT THE COLUMN, and that is the whole point of the
// ticket. The owner's ruling (rc-f2e963132fc5 [1]) is 「他回來了就把那行字直接
// 拿掉」 — the thing to remove is the RED LINE ON THE SCREEN, not "a row whose
// last_op_ok happens to equal false". Gating on the column instead of on the
// render was the same category of mistake this ticket exists to fix, and it left
// a reachable hole: `last_op_ok = nil` with last_op/last_op_at populated is a
// WORDLESS RED BLOCK, because the panel's verdict arm is `vm.lastOpOk ? "ok" :
// "fail"` (AgentDetailPanel.tsx) and nil takes the ✗ branch. That shape is
// PRODUCED BY THIS SERVER — clearWorkerPlacementBlock deliberately writes ok
// back to nil while leaving last_op and last_op_at standing.
//
// The two halves are the panel's two conditions, verbatim:
//
//	last_op != "" && last_op_at > 0   — `hasLastOp` (AgentDetailPanel.tsx:412),
//	                                    with last_op_at 0 mapped to null in
//	                                    mappers.ts before the panel sees it;
//	!(last_op_ok == true)             — the ✗ arm, which nil falls into.
//
// ⚠️ A SUCCESS RECEIPT IS NOT A FAILURE and this returns false for it. That
// boundary does not move: the owner asked for the red line to go, and deleting a
// green one would silently widen a narrow ruling.
//
// It is also what makes the clears non-churning: after one runs, last_op is ""
// and last_op_at is 0, so every later converged tick answers false and writes
// nothing.
func receiptRendersAsFailure(lastOp string, lastOpAt float64, lastOpOK *bool) bool {
	if lastOp == "" || lastOpAt <= 0 {
		return false // the panel hides the block entirely — nothing on screen
	}
	return lastOpOK == nil || !*lastOpOK
}

// clearMemberConvergedFailureReceipt removes the cockpit's red 「最近操作」 line
// from a staff member that has converged back ONLINE (T-39). The outsource twin
// is clearWorkerConvergedFailureReceipt (worker_spawn.go); they are two functions
// because they write two different row types, and they say the same thing because
// both turn on receiptRendersAsFailure and on the one decider's ConvergedOnline.
//
// 🔴 THE RULING IS MADE ON THE RE-READ ROW, NOT ON THE TICK SNAPSHOT. This is a
// whole-row write and the HTTP faces (activate / relocate / deactivate) write
// member rows without holding reconcileMu — the same hazard every stamp in this
// file re-reads for. Judging the snapshot and then blanking the fresh row would
// be strictly worse than the bug being fixed: it would delete a receipt an owner
// action wrote microseconds ago, which is destructive rather than merely stale.
//
// The snapshot IS used, but only as a short-circuit before the query, so a
// healthy member with a clean row costs nothing on every one of its converged
// ticks (the cadence is 30s per member, forever). The two ways the two answers
// can differ are both safe, and deliberately asymmetric:
//
//   - snapshot says "nothing to clear", fresh has a receipt → skipped this tick.
//     That receipt was written DURING this tick, so it is newer than the
//     convergence and keeping it is the honest answer; the next tick sees it in
//     its own snapshot and clears it then.
//   - snapshot says "clear it", fresh disagrees → the second check refuses and
//     nothing is written.
//
// Best-effort by the stampWakeObservability rule: a persistence failure is logged
// and never changes a reconcile decision. Caller holds reconcileMu.
func (s *apiServer) clearMemberConvergedFailureReceipt(memberID string, snapshot Member) {
	if !receiptRendersAsFailure(snapshot.LastOp, snapshot.LastOpAt, snapshot.LastOpOK) {
		return
	}
	fresh, err := s.dal.GetMember(memberID)
	if err != nil || fresh == nil || fresh.RosterStatus != RosterStatusActive {
		return
	}
	if !receiptRendersAsFailure(fresh.LastOp, fresh.LastOpAt, fresh.LastOpOK) {
		return
	}
	// All five columns, because clearing only the reason leaves the block on
	// screen with nothing written in it. last_op_ok goes back to nil — its
	// three-valued "nothing folded yet" — which is invisible precisely BECAUSE
	// the other four are blank; a nil beside a populated last_op is the wordless
	// red block above.
	fresh.LastOp = ""
	fresh.LastOpOK = nil
	fresh.LastOpLog = ""
	fresh.LastOpReason = ""
	fresh.LastOpAt = 0.0
	if err := s.putMember(*fresh, triggerServer); err != nil {
		reconcileLog("%s: converged receipt clear failed: %v", memberID, err)
	}
}

// isPlacementBlockedReason reports whether a last_op_reason was written by one of
// the placement stamps (rather than by a wake lapse or a warden receipt) — the
// only kind of explanation a landed START makes obsolete.
func isPlacementBlockedReason(reason string) bool {
	for _, code := range spawnBlockedReasonCodes {
		if strings.HasPrefix(reason, code+":") {
			return true
		}
	}
	return false
}

// ── pre-decide roster passes (producer.py, run inside the cadence tick) ──────

// codexCompactionRefocusThreshold is deliberately independent of the owner
// context-percent setting: Codex compacts its own long-lived thread, so its
// useful handover signal is repeated compaction, not a transient fill gauge.
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

// 🔴 announceSoftOffboardEscalation used to live here: the frame that told an
// agent its soft 重新聚焦 had become the final call, 120 seconds out. It is gone
// with the clock that justified it (owner 2026-08-19, card rc-c540367065ad) —
// 重新聚焦 no longer escalates. Its de-duplication state (softEscalated /
// softOffboardEpoch) went with it.
//
// ⚠️ T-ed79 brought a promotion back — context_notice → context_high — and it
// deliberately did NOT bring this frame back. It is not needed: the notice
// rides EVERY write to the member row (offboardDeltaPayload), and the promotion
// IS a write, so the putMember that changes refocus_op carries the FINAL
// sentence to the agent's own stream in the same delta. A second frame would be
// a second copy of one event, de-duplicated by nothing. What the removed
// function was really compensating for was an escalation that changed NO field
// — a soft→final flip decided from the clock alone, invisible on the row. The
// promotion changes refocus_op and refocus_since, so the row says it happened.
// Pinned by TestContextThresholds_PromotionDeltaCarriesTheFinalSentence.

// canPromoteToAcceleratedStop is the ONE exception to "an in-flight epoch is its
// own cooldown": a member the FIRST context threshold put on a plain 停止 has
// crossed the SECOND, so the same wind-down becomes 加速停止.
//
// 🔴 ONE DIRECTION, ONE CAUSE. An epoch the OWNER opened (重新聚焦, 改機器,
// 換 model) or the agent opened for itself (restart_self) is never promoted, at
// any pct: the owner deliberately asked for a stop with no clock, and quietly
// turning it into one with a deadline would take that decision away from him in
// the one place he cannot see it happen. Only context_notice → context_high.
//
// The stopped-report check is not tidiness either: a member that has already
// reported stopped is collected by decideUp's recycle arm on this very tick, so
// re-stamping refocus_since would move the deadline of a wind-down that is
// already over, and the promotion notice would reach a session that has said it
// is finished.
func canPromoteToAcceleratedStop(m Member, op string) bool {
	return op == refocusOpContextHigh &&
		m.RefocusOp == refocusOpContextNotice &&
		m.StoppedSince <= 0.0
}

// shouldNoticeRefocus is the FIRST threshold's actionable signal — the soft twin
// of shouldAutoRefocus, reading the SAME gauge through the same stale guard so
// the two thresholds can never disagree about where the session is.
//
// The codex arm asks codexNoticeDue rather than inventing a second rule: a codex
// session hands over on compaction ROUNDS, so "the first threshold" means the
// notice round, 60% through it — the same predicate the SSE advance notice fires
// on. Reusing it is what keeps one runtime's two thresholds on one axis.
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

// ── the context-gate diagnostic (T-72dd 補觀測) ───────────────────────────────

// ctxGateDiagThrottleSecs bounds the gate diagnostic to ONE line per ACTOR per
// five minutes.
//
// 🔴 THE THROTTLE IS NOT POLISH, IT IS THE FEATURE. The pass runs on the 30 s
// reconcile/outsource cadence and every live actor takes one of these gates on
// almost every tick, so an unthrottled line is not observability — it is the
// 1.26-million-line serve.log this ticket was diagnosed inside, and it would
// bury the very line it exists to surface. Five minutes is the owner's number.
const ctxGateDiagThrottleSecs = 300.0

// ctxGateDiagState is one actor's throttle cell: WHEN the diagnostic last spoke
// for it, and WHICH gate it named. The gate is half the key on purpose — see
// noteContextGateSkip for why a change of gate is not made to wait.
type ctxGateDiagState struct {
	ts   float64
	gate string
}

// noteContextGateSkip emits the ONE line that tells 「這一輪跑了，這個 actor 被
// 某道 gate 擋掉」 apart from 「這個 actor 根本沒被看過」.
//
// 🔴 THIS IS THE WHOLE POINT OF THE TICKET'S LAST STEP. Every quiet path out of
// stampContextHighRecycle was a bare `continue`, so "the pass ran and decided
// nothing" and "the pass never reached this actor at all" produced byte-identical
// logs — nothing. That ambiguity is why the original symptom took as long as it
// did to localise, and no amount of reading the code afterwards replaces a line
// that says which gate was closed and on what numbers.
//
// WHAT IS ACTUALLY KEYED, precisely — the two halves are different and both
// matter. The MAP is keyed on the ACTOR: one cell, holding ONE timestamp and
// the ONE gate that timestamp belongs to. That is the memory bound, and the
// reason the prune in clearSessionBootTS is per actor. But the SILENCING test
// compares the remembered gate for EQUALITY, so what decides whether a line is
// suppressed is the actor+gate PAIR: same actor on the same gate is throttled,
// same actor on a different gate speaks immediately (the rule set out under A
// CHANGE OF GATE IS NOT THROTTLED below). The line answers "what is this
// actor's gate state right now", and a change of gate IS a change of that
// answer.
//
// That is NOT the same design as the one rejected further down. There, "keying
// the window on actor+gate" means one INDEPENDENT window per pair, each with
// its own timestamp, several alive at once — which really would multiply a
// quiet actor's budget. Here there is only ever ONE window, and a change of
// gate does not open a second one, it TAKES OVER the only one.
//
// 🔴 THIS PARAGRAPH USED TO SAY THE OPPOSITE, AND MEASUREMENT KILLED IT. It
// claimed the key was "the ACTOR, not the actor+gate pair", and that keying it
// that way was what stopped an actor drifting between two closed gates from
// doubling its own budget — "the flooding the throttle exists to stop". Neither
// half of that survives. The suppression test reads the pair, not the actor
// alone; and a drifting actor is not held to double its budget, it is held to
// NO budget — it speaks on every tick. The claim was not merely imprecise, it
// named this design as the defence against exactly the case the design does not
// defend against. The flapping bound below is the measurement that settles it,
// and it and this paragraph are ONE statement, not two competing ones.
//
// 🔴 THREE OF THIS PASS'S SEVEN QUIET PATHS ARE STILL SILENT, and the reason
// originally given for that here was WRONG. It said their state "is readable on
// the wire anyway". An independent review checked each one, and it is not:
//
//   - aRefocusStampWouldReachTheAgent — reads DesiredState. On the wire. ✅
//   - canPromoteToAcceleratedStop — reads RefocusOp (on the wire) and
//     StoppedSince (NOT on the wire). Half. ⚠️
//   - the stale stopped_since latch — reads StoppedSince and the gauge's
//     boot_ts. NEITHER is on the wire. ❌
//
// Checked, not assumed: no Go struct tag exports stopped_since (the only two
// mentions of the name in spec/openapi.json are prose inside the report_stopped
// / report_waking descriptions, not schema properties), and the gauge's boot_ts
// has no wire exit in wire.go or api_monitoring.go. And the latch guard's own
// comment (a few lines below) says a wrong verdict there excludes the member
// "from BOTH thresholds for the rest of its life" — a PERMANENT silent no-op,
// the exact class this ticket chased, on two inputs an operator cannot see.
// It is left uninstrumented only because the ticket scoped this to three gates,
// and it is a known gap, not a justified omission. A seventh path — the
// armRefocusEpoch refusal — already logs itself, so four of the seven are
// observable today.
//
// 🔴 A CHANGE OF GATE IS NOT THROTTLED. The window is per actor, but the cell
// also remembers WHICH gate it last named, and a different gate speaks
// immediately. An actor crossing from "no-actionable-pct" to "offline" does so
// once in its life and that instant is the most informative one the line will
// ever carry; making it wait out the remaining 290 s of somebody else's window
// would drop exactly the transition a reader is looking for. Steady-state cost
// is unchanged — a settled actor keeps taking the SAME gate every tick, so it
// still emits once per window — which is why this is cheaper than keying the
// window on actor+gate (that would triple the budget of every quiet actor).
//
// ⚠️ "STEADY-STATE" THERE MEANS A SETTLED ACTOR, AND ONLY A SETTLED ACTOR. The
// remembered gate is compared for EQUALITY, so an actor that FLAPS between two
// closed gates — a pct oscillating across the handover threshold while the
// boot-storm guard is still armed is the ordinary way to produce that — names a
// different gate on every tick and therefore speaks on every tick: for as long
// as the flap lasts the throttle is not reduced, it is GONE. The upper bound is
// consequently ONE LINE PER TICK PER ACTOR, i.e. the reconcile/outsource cadence
// itself (~2 lines per minute per actor at the 30 s tick), against a budget of
// one line per five minutes.
//
// Measured, not reasoned: an online actor driven across the threshold on every
// tick emitted a line on every one of them, sustained for as long as both gates
// stayed shut. Under stock settings the flap ends itself when the boot-storm
// window closes and the high pct starts being ACTED on instead of skipped, which
// held that same actor to one line per tick only until then.
//
// This is the ACCEPTED PRICE of the rule above, not an oversight. The transition
// is the most informative instant this line will ever carry, so it is
// deliberately not made to serve out a window; the cost of that choice lands
// exactly on the actor that keeps transitioning. It stays acceptable because a
// flap is self-limiting (the pct that keeps crossing either settles or the pass
// finally acts on it). If it ever stops being self-limiting, damp the flap or
// widen the key to remember MORE history — do not make the transition wait,
// because that gives back the one line the diagnostic exists to print.
//
// ⚠️ PURELY OBSERVATIONAL. It reads the gauge, asks the hub, and writes stderr.
// It must never alter what the pass decides, and it is called only on paths that
// have ALREADY decided to skip.
//
// Bound: one cell per actor, pruned on the session boundary
// (clearSessionBootTS) — the same treatment handoverNoticed gets one line over,
// and for the reason written there: not "one record per agent id alive for the
// process's lifetime". The prune doubles as the right behaviour, since a fresh
// session deserves to be described again rather than inheriting its
// predecessor's window.
func (s *apiServer) noteContextGateSkip(id, gate string, record map[string]any, now float64) {
	s.ctxGateDiagMu.Lock()
	if last, seen := s.ctxGateDiagAt[id]; seen && last.gate == gate &&
		now-last.ts < ctxGateDiagThrottleSecs {
		s.ctxGateDiagMu.Unlock()
		return
	}
	if s.ctxGateDiagAt == nil {
		s.ctxGateDiagAt = map[string]ctxGateDiagState{}
	}
	s.ctxGateDiagAt[id] = ctxGateDiagState{ts: now, gate: gate}
	s.ctxGateDiagMu.Unlock()
	reconcileLog("recycle: gate skip %s gate=%s pct=%s pct_ts=%s boot_ts=%s "+
		"boot_secs=%s online=%t", id, gate,
		gaugeNumForDiag(record, "context_pct"),
		gaugeNumForDiag(record, "context_pct_ts"),
		gaugeNumForDiag(record, "boot_ts"),
		secsSinceBootForDiag(record, now),
		s.hub.IsOnline(id))
}

// gaugeNumForDiag renders one numeric gauge key for the diagnostic line, or the
// literal "-" when the key is absent / non-numeric / the gauge entry is nil.
// "-" is not decoration: WHICH of the five inputs is missing is most of what
// the line is for (a missing context_pct_ts and a stale one fail the same gate
// but mean completely different things).
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

// secsSinceBootForDiag renders the boot-storm loop-guard's own input, through
// gaugeSecsSinceBoot so the number on the line is the number the guard saw —
// "-" when there is no usable boot_ts (the guard's fail-open case).
func secsSinceBootForDiag(record map[string]any, now float64) string {
	secs := gaugeSecsSinceBoot(record, now)
	if secs == nil {
		return "-"
	}
	return strconv.FormatFloat(*secs, 'f', 1, 64)
}

// stampContextHighRecycle auto-stamps refocus_since on any candidate whose
// runtime-specific handover signal is actionable — the
// automatic counterpart of the manual refocus button, reusing the SSE band's
// stale-pct + boot-storm guards so an unreliable gauge never auto-recycles.
// Mutates the in-slice member so the SAME tick's observation sees the marker.
//
// 🔴 BOTH thresholds open a wind-down now (T-ed79), and they open DIFFERENT
// kinds. The first (notice_pct) opens a plain 停止 — refocus_op=context_notice,
// no clock, no deadline in the sentence — because the owner's model is that an
// agent near its limit is ASKED to wind down before it is collected on one. It
// used to open nothing at all: the first threshold sent one SSE band and the
// wind-down began only at the second, so an agent that missed the frame met the
// final call with no close-out started.
//
// 🔴 …which is why the de-dup below has exactly ONE exception. `refocus_since >
// 0 ⇒ skip` is the cooldown that stops this loop re-stamping every 30 s, and
// with the first threshold now stamping, that same rule would have made the
// SECOND threshold unreachable for the rest of the session: the promotion path
// would be dead code and the notice epoch would be the last word, on an agent
// that has since gone past the line where the owner wants it collected.
func (s *apiServer) stampContextHighRecycle(members []Member, now float64) {
	ctxhigh := s.ctxHighConfig()
	codexNoticeRound := s.codexNoticeRoundSetting()
	for i := range members {
		m := &members[i]
		record := s.gauge.Get(m.ID)
		op := ""
		switch {
		case shouldAutoRefocus(m.Runtime, record, ctxhigh, s.codexCompactionThreshold):
			op = refocusOpContextHigh
		case shouldNoticeRefocus(m.Runtime, record, ctxhigh, codexNoticeRound, s.codexCompactionThreshold):
			op = refocusOpContextNotice
		default:
			// GATE 1/3 (T-72dd 補觀測): no actionable signal. This is the gate
			// that swallows a STALE pct — actionableContextPct returns nil when
			// context_pct_ts <= boot_ts — which is exactly the case the cockpit
			// still renders a number for (foldActorRuntime reads the raw key).
			s.noteContextGateSkip(m.ID, "no-actionable-pct", record, now)
			continue
		}
		// 🔴 AN AGENT THAT HAS ALREADY SAID 「我收完了」 IS NOT ASKED AGAIN.
		// report_stopped latches stopped_since on ANY staff member, epoch or not
		// (owner rc-b08d49dc3b03), and then fires ONE best-effort
		// dispatchRobustStopNow. A warden that is unreachable at that instant
		// drops it, and nothing sweeps the row while the session is still up:
		// clearRecycleMarkersOnRespawn skips anything online, and
		// clearStaleStoppingOnOnline only ever zeroes stopping_since. So the
		// member sits at desired-online ∧ online ∧ stopped_since>0 ∧ no epoch.
		//
		// Opening a wind-down there does two wrong things at once. armRefocusEpoch
		// zeroes the anchors BY DESIGN — it cannot tell a fresh report from a
		// stale latch and must keep zeroing them, because a stale one turns the
		// next epoch into an immediate kill — so the stamp DESTROYS the agent's
		// finished close-out; and the notice it fans asks a session that is
		// already done to do it all again. Before T-ed79 the first threshold
		// stamped nothing at all, so this door did not exist; giving it a
		// wind-down is what opened it.
		//
		// Same ruling canPromoteToAcceleratedStop already makes a few lines up
		// ("the promotion notice would reach a session that has said it is
		// finished") — this is the arm that was missed, not a new policy.
		//
		// 🔴 THE BOOT_TS TEST IS LOAD-BEARING, not belt-and-braces. A latch can
		// legitimately be a PREDECESSOR's: activate clears stopping_since and
		// waking_since but NOT stopped_since, so 下線 → 活化 puts a brand-new
		// session online carrying the previous generation's report with no epoch
		// — exactly the fixture
		// TestRefocusEpoch_NoStampSiteInheritsAStaleWindDownLatch pins. Skipping
		// on that would exclude the member from BOTH thresholds for the rest of
		// its life, which is a worse bug than the one this guard removes. The
		// question is therefore not "is there a latch" but "did THIS connection
		// file it", and that is the same question — and the same answer —
		// actionableContextPct's stale guard already uses one field over: a
		// predecessor session's leftover never triggers. No boot_ts means the
		// question cannot be answered, and then the pre-existing path (stamp)
		// stands.
		if bootTS, ok := gaugeBootTS(record); ok && m.StoppedSince >= bootTS {
			continue
		}
		promoting := false
		if m.RefocusSince > 0.0 {
			if !canPromoteToAcceleratedStop(*m, op) {
				continue // already winding down — the marker IS the cooldown
			}
			promoting = true
		}
		if bootStormTripped(gaugeSecsSinceBoot(record, now), ctxhigh.MinBootSecs) {
			// GATE 2/3 (T-72dd 補觀測) — fresh boot already over the line →
			// suppress (loop-guard).
			s.noteContextGateSkip(m.ID, "boot-storm", record, now)
			continue
		}
		if !s.hub.IsOnline(m.ID) {
			// GATE 3/3 (T-72dd 補觀測) — only-online (symmetric with the manual
			// refocus gate).
			s.noteContextGateSkip(m.ID, "offline", record, now)
			continue
		}
		// …and only when the server still WANTS it online (T-ccc7). hub.IsOnline
		// is a live-socket fact, not an intent: a member deactivated seconds ago
		// keeps its stream for the whole stop grace, so this loop used to stamp
		// a wind-down epoch onto a member already on its way out — invisible to
		// the agent, unread by decideDown, and not cleared by activate. See
		// aRefocusStampWouldReachTheAgent.
		if !aRefocusStampWouldReachTheAgent(*m) {
			continue
		}
		if promoting {
			// PROMOTION, not a new epoch: the member is already winding down and
			// keeps whatever it has reported. Only the kind and the clock change.
			//
			// ⚠️ RefocusSince is re-stamped, and that is the whole point. The
			// deadline is refocus_since + grace, so promoting in place would put
			// the deadline at the FIRST threshold's stamp — minutes in the past
			// on any session that took the notice seriously — and `now >=
			// refocus_since + grace` would be true on the very tick that
			// announced it. The agent would be told "your deadline is <a moment
			// already gone>" and collected in the same tick: a zero-second
			// deadline, which is the exact harm this ticket exists to remove.
			//
			// armRefocusEpoch is deliberately NOT used: it clears the wind-down
			// anchors, and here they belong to the close-out ALREADY IN FLIGHT.
			// Clearing stopped_since would be worse than untidy — it would erase
			// the agent's own "I am done" and cancel the collection this same
			// tick was about to make.
			m.RefocusSince = now
			m.RefocusOp = refocusOpContextHigh
		} else if !armRefocusEpoch(m, op, now) {
			// Unreachable as the loop stands (the guard above already skips a
			// member with a live epoch unless this is the promotion). Handled
			// anyway, and LOUDLY: the failure mode it replaces is a putMember
			// that persists nothing and a log line claiming a stamp that never
			// happened — which is the exact silent shape this whole ticket is
			// about.
			reconcileLog("recycle: auto-stamp for %s refused — %s would move the "+
				"wind-down ladder backwards from %s", m.ID, op, m.RefocusOp)
			continue
		}
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("recycle: auto-stamp persist failed for %s: %v", m.ID, err)
			continue
		}
		if promoting {
			// The FINAL sentence needs no frame of its own: offboardDeltaPayload
			// composes the notice from refocus_op on EVERY write to the row, so
			// the putMember above already carried it. (This is what the removed
			// announceSoftOffboardEscalation used to do by hand; pinned by
			// TestContextThresholds_PromotionDeltaCarriesTheFinalSentence.)
			reconcileLog("recycle: promoted %s to %s (%s)", m.ID, refocusOpContextHigh,
				NormalizeRuntime(m.Runtime))
		} else {
			reconcileLog("recycle: auto-stamp refocus_since for %s (%s, %s)", m.ID,
				NormalizeRuntime(m.Runtime), op)
		}
	}
}

// tokenExpiryLeadSecs is how long BEFORE its agent token expires a live session
// is asked to wind down (owner 2026-08-21: refocus／改 model／改機器「就是呼叫
// 軟下線，然後等他 report_stopped 以後再呼叫上線」, and token renewal is the same
// shape — the session has to end and be re-minted, so it may as well end the way
// every other cause ends). One hour is the lead the owner named.
const tokenExpiryLeadSecs = 3600.0

// tokenExpiryOf derives WHEN a live session's agent token stops working, from
// the two facts the server already stores. It returns 0 when the question
// cannot be answered, and every caller must treat 0 as "do nothing".
//
// 🔴 THIS IS A DERIVATION, NOT A RECORD, AND THE ERROR TERM IS NAMED. The token
// is minted in buildStartFrame at DISPATCH time with the TTL that was live then
// (mintJWT sets exp = mint + ttl — jwt.go); session_boot_ts is stamped later, on
// the SSE first-connect edge (anchorSessionBoot). So:
//
//   - session_boot_ts >= mint, therefore this estimate is an UPPER BOUND on the
//     real expiry, too late by however long the boot took. The trigger built on
//     it therefore fires slightly LATE — with a little under the full lead left
//     — never early. That direction is the safe one: the window shrinks, it does
//     not open before the token exists.
//   - it reads the CURRENT agent_token_ttl, not the one the token was minted
//     with. An owner who changes that setting mid-session moves this estimate
//     for sessions whose tokens did not move. Raising the TTL therefore defers
//     the wind-down past the real expiry (the session dies with no close-out —
//     the pre-existing behaviour, not a new harm); lowering it winds sessions
//     down early (a wasted, but safe, handover). Recording the real exp would
//     need a durable per-session column, which this ticket deliberately did not
//     add.
//
// ⚠️ EXACTLY ONE KIND IS EXEMPT, and the reason is a property of its credential,
// not of what it is: a WARDEN's token is minted by mintWardenToken →
// mintJWTWithoutExpiry, with NO exp claim at all, so it never expires and asking
// this question about one would invent a deadline that does not exist.
//
// 🔴 This gate used to read `Kind != KindAssistant`, which swept OUTSOURCE in
// with warden while the comment explained only the warden half — the classic
// shape of an exemption that is wider than its own justification. An outsource
// worker's session token is minted by mintAgentToken with s.agentTokenTTLValue()
// (worker_spawn.go), i.e. the SAME mint and the SAME TTL a staff member's boot
// token gets, so it expires in exactly the same way; and every step of the
// close-out — report_stopping, the lesson write, report_stopped — is an MCP call
// carrying that token. Naming the one exempt kind, rather than allow-listing the
// one included kind, is what keeps the next kind from inheriting an exemption
// nobody decided to give it.
func tokenExpiryOf(m Member, agentTokenTTL int64) float64 {
	if m.Kind == KindWarden || agentTokenTTL <= 0 || m.SessionBootTS <= 0 {
		return 0
	}
	return m.SessionBootTS + float64(agentTokenTTL)
}

// stampTokenExpiryWinddown opens a plain 停止 on any live staff session whose
// agent token is inside its last tokenExpiryLeadSecs — the same shape
// stampContextHighRecycle uses for the context thresholds, and deliberately so:
// what ends a session is one funnel, and a second one with its own guards would
// be a second chance to get the guards wrong.
//
// The guards are COPIED from stampContextHighRecycle rather than re-derived,
// cell for cell, because each of them is there for a reason that holds here
// identically:
//
//   - refocus_since > 0 → skip. The marker IS the cooldown; without it this loop
//     re-stamps every 30 s for the whole final hour, and each re-stamp would
//     destroy the close-out already in progress. There is deliberately NO
//     promotion arm (the context pair's one exception): token expiry never
//     escalates into a different kind, so there is nothing to promote to.
//   - stopped_since >= the gauge's boot_ts → skip. An agent that has already
//     said 「我收完了」 is not asked again, and armRefocusEpoch would zero the
//     anchors and destroy that report. The boot_ts test is what tells THIS
//     session's report apart from a predecessor's latch (下線 → 活化 leaves one).
//   - online-only, and only while the server still WANTS it online
//     (aRefocusStampWouldReachTheAgent). A stamp that does not reach the agent
//     is not a weaker signal, it is no signal — and it strands a marker that
//     activate does not clear.
//
// The boot-storm guard is NOT copied, and that is not an oversight: it exists to
// stop a fresh session being recycled off a gauge reading that is over the line
// the instant it boots. A token that is within an hour of expiry on a session
// that just booted is not a mis-reading — it is a session that genuinely has
// less than an hour, and suppressing it would leave exactly the case this
// trigger is for.
func (s *apiServer) stampTokenExpiryWinddown(members []Member, now float64) {
	ttl := s.agentTokenTTLValue()
	for i := range members {
		m := &members[i]
		expiry := tokenExpiryOf(*m, ttl)
		if expiry <= 0 {
			continue // not derivable (no session anchor, or not a staff session)
		}
		if now < expiry-tokenExpiryLeadSecs {
			continue // still outside the lead
		}
		if now >= expiry {
			// Past the derived expiry the token is certainly dead (the estimate
			// is an upper bound — see tokenExpiryOf), so the sequence this stamp
			// asks for could not be filed: every step of it is an MCP call on
			// that token. Opening a wind-down here would print instructions to a
			// session that can only answer 401.
			continue
		}
		if m.RefocusSince > 0.0 {
			continue // already winding down — the marker IS the cooldown
		}
		record := s.gauge.Get(m.ID)
		if bootTS, ok := gaugeBootTS(record); ok && m.StoppedSince >= bootTS {
			continue // this session has already reported it is finished
		}
		if !s.hub.IsOnline(m.ID) {
			continue
		}
		if !aRefocusStampWouldReachTheAgent(*m) {
			continue
		}
		if !armRefocusEpoch(m, refocusOpTokenExpiry, now) {
			// Same shape as the auto-stamp above: the `refocus_since > 0`
			// continue earlier in this loop already covers it, and this arm
			// exists so that a future edit loosening that check surfaces as a
			// log line instead of a write that quietly stores nothing.
			reconcileLog("recycle: token-expiry stamp for %s refused — would move "+
				"the wind-down ladder backwards from %s", m.ID, m.RefocusOp)
			continue
		}
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("recycle: token-expiry stamp persist failed for %s: %v", m.ID, err)
			continue
		}
		reconcileLog("recycle: token-expiry 停止 for %s (token estimated to expire at %.0f, "+
			"lead %.0fs)", m.ID, expiry, tokenExpiryLeadSecs)
	}
}

// bootStormTripped is the pure loop-guard signal (context_high.py): true iff
// the agent hit the HANDOVER line so soon after boot that its boot context
// itself is over the line. FAIL-SAFE: missing/negative data never trips it;
// minBootSecs <= 0 disables the guard.
func bootStormTripped(secsSinceBoot *float64, minBootSecs float64) bool {
	if minBootSecs <= 0 {
		return false
	}
	if secsSinceBoot == nil || *secsSinceBoot < 0 {
		return false
	}
	return *secsSinceBoot < minBootSecs
}

// clearRecycleMarkersOnRespawn is the server-authoritative recycle LOOP-BREAK
// (§4.5): clear the recycle markers the moment the respawn-pending state is
// observed (desired online ∧ ¬online ∧ refocus_since>0 — the kill landed), so
// a slow/never-waking respawn can never be re-killed off a stale marker.
//
// 🔴 It also clears a wind-down latch left behind with NO epoch at all. An agent
// can report_stopping / report_stopped on its own, without anybody stamping
// refocus_since — a spontaneous close-out, or one whose epoch was already
// cleared — and the arm below used to skip those rows on `refocus_since <= 0`,
// so stopped_since sat on a desired-online member forever. That latch is not
// inert: it is exactly what armRefocusEpoch documents, and it is read by the
// recycle arm of decideUp (which robust-stops on stopped_since > 0 the instant
// ANY epoch is stamped). Clearing it here is why the stamp sites can be trusted
// to open a clean epoch even against a row that has been sitting in the DB for
// days.
//
// 🔴 The SSE stop gate is NOT a second reader in this scope, and this comment
// used to name it as one. api_infra.go's gate only fires on
// `desired_state == offline`, and the first gate below `continue`s on anything
// that is not desired online — so within this function's range the gate is
// unreachable by construction. Citing a protection that cannot apply here made
// the case for clearing the latch look stronger than it is; the decideUp reader
// alone is the real reason, and it is sufficient.
//
// WHY THE `IsOnline` GATE IS SUFFICIENT here, and no close-out is cut short by
// this: the arm only fires on desired online ∧ NOT online. A member with a live
// session is never touched, so an agent working its sequence (report_stopping
// sent, report_stopped not yet) keeps its anchors for as long as it is
// connected. If its socket really is gone while desired_state is still online,
// reconcile's decideUp is already going to START a replacement session on this
// same tick — with or without the latch, nothing is waiting for that close-out
// to finish. What the latch WOULD do in that state is arm the two destructive
// readers above against the next epoch. Clearing loses nothing that is still
// being used and removes a trap; keeping it protects a close-out that no code
// path is still honouring.
func (s *apiServer) clearRecycleMarkersOnRespawn(members []Member) {
	for i := range members {
		m := &members[i]
		if m.DesiredState != DesiredStateOnline {
			continue
		}
		if m.RefocusSince <= 0.0 && m.StoppedSince <= 0.0 && m.StoppingSince <= 0.0 {
			continue // plain respawn — nothing to clear
		}
		if s.hub.IsOnline(m.ID) {
			continue // still online = recycle-PENDING (dump in flight), not a respawn
		}
		m.RefocusSince = 0.0
		m.RefocusOp = ""
		m.StoppedSince = 0.0
		m.StoppingSince = 0.0
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("recycle: loop-break persist failed for %s: %v", m.ID, err)
			continue
		}
		reconcileLog("recycle: loop-break — cleared recycle markers on respawn for %s", m.ID)
	}
}

// consumeUninstallIntentOnOffline consumes the ONE-SHOT uninstall intent
// (§4.3, owner-decided semantics): a warden observed OFFLINE while still
// carrying desired_state="uninstall" has converged — the box holds no live
// warden, which IS the uninstall goal state — so the intent is spent and the
// record folds back to "offline" (kept, re-installable). Without this a
// residual intent is a standing kill order: every future reconnect (a
// re-install) would be answered with another UNINSTALL, an infinite
// uninstall→re-install loop (real incident, 2026-07). The cadence pass is the
// restart-amnesia backstop of the event-driven consumeUninstallOnDisconnect
// edge below, and also self-heals any stale intent already sitting in the DB.
func (s *apiServer) consumeUninstallIntentOnOffline(members []Member) {
	for i := range members {
		m := &members[i]
		if m.Kind != KindWarden || parseDesired(m.DesiredState) != DesiredStateUninstall {
			continue
		}
		if s.hub.IsOnline(m.ID) {
			continue // still online → the UNINSTALL dispatch arm owns it
		}
		m.DesiredState = DesiredStateOffline
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("uninstall: intent-consume persist failed for %s: %v", m.ID, err)
			continue
		}
		reconcileLog("uninstall: consumed one-shot intent for offline warden %s "+
			"(desired_state → offline; record kept)", m.ID)
	}
}

// consumeUninstallOnDisconnect is the EVENT-DRIVEN twin of the pass above,
// fired from the SSE disconnect edge (api_infra.go): the instant a warden
// drops its stream while desired_state=="uninstall", the intent is observed
// converged and consumed — no 30s cadence window in which a fast re-install
// could reconnect into the standing kill order. Best-effort; gated OFF by
// --no-reconcile like the producer's other desired-state control writes.
func (s *apiServer) consumeUninstallOnDisconnect(memberID string) {
	if s.noReconcile {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil || m.Kind != KindWarden {
		return
	}
	if parseDesired(m.DesiredState) != DesiredStateUninstall || s.hub.IsOnline(m.ID) {
		return
	}
	m.DesiredState = DesiredStateOffline
	if err := s.putMember(*m, triggerServer); err != nil {
		reconcileLog("uninstall: disconnect-edge intent-consume persist failed for %s: %v",
			m.ID, err)
		return
	}
	reconcileLog("uninstall: consumed one-shot intent on warden %s disconnect "+
		"(desired_state → offline; record kept)", m.ID)
}

// quietSince answers "when did this member last say anything of its own?" for
// the stale-stopping sweep: the later of the close-out anchor and the gauge's
// report ts.
//
// The gauge's ts is written by the member's OWN live session (ocagent's
// context-report, keyed on the verified token sub). Nothing on the member ROW
// moves when a member works: chat, tasks and every other MCP call write no
// member row at all, and last_op_at belongs to the WARDEN, not to the member.
//
// 🔴 It is NOT a heartbeat, and nothing may read it as one. The 30s in
// contextreport.go is a THROTTLE CEILING ("at most one report burst per 30s
// window"), not a timer: on Claude the reporter is wired as the statusLine
// command (cli/ocwarden/spawn.go), so it fires when the status line redraws; on
// codex it rides reportTokenUsage, which answers a tokenUsage-updated event.
// Both are driven by the agent's activity, not by a clock.
//
// What that costs: this discriminator sees a member that is still producing
// activity, and a member blocked inside one long call produces none — so a
// close-out that spends the whole window waiting on a single sub-agent is still
// swept. This is a strict improvement over dating the sweep from the anchor,
// not a complete fix, and it must not be described as one.
//
// ⚠️ And do NOT read the paragraph above as "no clock-driven signal exists".
// One does, for codex only: codex_session.go runs an identityHeartbeat ticker
// (30s, in the session's select loop) whose reportIdentity POSTs to
// /api/monitoring/telemetry under the member's own token, and the server stamps
// ts there — a DIFFERENT store (s.telemetry, not s.gauge). It keeps ticking
// through a long tool call, so it could close exactly the gap named above.
// Reading it here is a behaviour change with its own trade-offs and belongs to
// the follow-up, not to this sweep; what must not happen is the next reader
// concluding from these comments that nothing of the sort is available.
//
// 🔴 A missing or unusable record means NO OPINION, not "silent" — the gauge is
// in-memory and volatile by contract (hub.go), so a station re-exec blanks it
// for the whole fleet at once. Reading that as fleet-wide silence would sweep
// every close-out in flight; instead the anchor's own age decides, which is
// exactly the pre-T-7723 rule. The worst this can do is behave like it used to.
// Same fail-open shape as gaugeSecsSinceBoot's loop guard.
func quietSince(m Member, gauge map[string]any) float64 {
	quiet := m.StoppingSince
	if ts, ok := asNumber(gauge["ts"]); ok && ts > quiet {
		quiet = ts
	}
	return quiet
}

// clearStaleStoppingOnOnline is the survived-stop auto-clear (§4.5): a
// desired-online member OBSERVED online while still carrying a stopping_since
// anchor is provably past that stop — clear the anchor so it can never derive
// a phantom *stopping* forever.
func (s *apiServer) clearStaleStoppingOnOnline(members []Member, now float64) {
	for i := range members {
		m := &members[i]
		if m.DesiredState != DesiredStateOnline {
			continue // desired-offline winding-down is the honest graceful stop
		}
		if m.StoppingSince <= 0.0 {
			continue
		}
		if !s.hub.IsOnline(m.ID) {
			continue // may be a genuine stopped terminal — leave it
		}
		// 🔴 …and not while a close-out could still be in progress (T-2123).
		// This sweep exists for an anchor left by a stop the member SURVIVED,
		// but "survived a stop" and "is working its offboard sequence right
		// now" look identical from here: both are online, both are wanted
		// online, both carry the anchor. Clearing the fresh one erased the
		// owner's only signal that the session had begun closing out — the
		// cockpit went back to green while the agent was writing its hand-off,
		// which is exactly what he reported seeing.
		//
		// 🔴 T-7723: the anchor's AGE was the wrong clock. It bought exactly
		// SoftOffboardGraceSecs of visibility, and the one thing an offboard is
		// told to do first — collect the sub-agents still running — is the one
		// thing that routinely takes longer than that. The owner watched a
		// member report 「開始收尾」 and asked TWICE, 22 minutes later, why it never
		// had; the anchor had been swept at minute 10 while the member was still
		// working. So the clock is now QUIET TIME, not anchor age: a member that
		// is still filing context reports is still saying something, and a
		// close-out that says nothing for the whole window is the residue this
		// sweep was written for.
		//
		// What it costs, said plainly: a member that reports stopping, abandons
		// the close-out and goes back to ordinary work now reads *stopping* for
		// as long as that session lives, where before it flipped green after the
		// window. That trade is deliberate — the two errors are not equal. The
		// old one HID a member that really was closing out, and the owner read it
		// as idle. The new one shows a member that said it was stopping and never
		// finished, which is literally true, and it has two cheap exits that both
		// already exist: report_stopped, or any reboot (report_waking clears the
		// anchor). It cannot outlive the session.
		if now-quietSince(*m, s.gauge.Get(m.ID)) < SoftOffboardGraceSecs {
			continue
		}
		m.StoppingSince = 0.0
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("revive: stale-stopping clear persist failed for %s: %v", m.ID, err)
			continue
		}
		reconcileLog("revive: auto-cleared stale stopping_since on observed-online %s "+
			"(survived stop / SSE reconnect)", m.ID)
	}
}

// ── the cadence tick + the event-driven seams ─────────────────────────────────

// runReconcileTick runs ONE producer tick over the roster snapshot: THE entry
// filter, THE shared pre-decide formalities (lifecycle_roster.go — the same
// list the outsource producer runs), the receipt sweep, then decide→dispatch
// per candidate. Candidates (§4.1): every ACTIVE non-warden member, plus any
// ACTIVE warden whose desired_state is uninstall — which is what
// lifecyclePolicyFor's staff arm says. Serialized with the event-driven ticks via reconcileMu;
// best-effort — a fault is logged, never raised into the cadence loop.
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
		// THE entry filter (lifecycle_roster.go). It used to be written out here
		// by hand, and again in reconcileMemberNow, and a third time — in its
		// outsource dialect — in runOutsourceTick. One question, one answer.
		if !lifecyclePolicyFor(m).ShouldExist() {
			continue
		}
		members = append(members, m)
	}
	// THE pre-decide formalities, in THE order, from THE list
	// (lifecycle_roster.go lifecycleRosterPasses). There is no second list: the
	// outsource producer runs this same one through runWorkerLifecyclePasses, so
	// a formality added here reaches a worker by construction and one that must
	// not has to say so as its own AppliesTo.
	s.runLifecycleRosterPasses(members, now)
	// The receipt deadline (receipt_watch.go) — swept BEFORE the decide pass so
	// a start/stop armed by THIS tick always gets a full window, never a same-
	// tick sweep. Covers workers too: their start/stop rides the member verbs
	// and their receipts key on the same id namespace.
	s.sweepLapsedReceipts(now)
	reconcileLog("tick: %d candidate(s)", len(members))
	for i := range members {
		s.reconcileTickMemberLocked(members[i], now)
	}
}

// The 30s cadence that used to mount runReconcileTick on its own goroutine
// (startReconcileCadence) is gone: T-14 item 5 merged it with the outsource
// producer's identical loop into the single startLifecycleCadence
// (lifecycle_tick.go), which runs this half first and the outsource half
// after, each under its own lock and never both at once.

// reconcileMemberNow is the EVENT-DRIVEN immediate reconcile for ONE member —
// the activate/deactivate/uninstall click seam (producer.py
// dispatch_member_now). Shares the cadence's store + mutex, so a START
// dispatched here makes the next cadence tick an idempotent no-op (no double
// spawn). Best-effort: every fault is swallowed (the cadence re-decides from
// presence next tick). Gated OFF wholesale by --no-reconcile.
// Returns the dispatch decision so an event-driven caller (the relocate handler)
// can observe DispatchUnlanded — a decided-but-undelivered move — instead of
// reporting a silent success (T-8655). A gated-off / skipped / faulted member
// yields the zero decision (no command, not unlanded).
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
	// THE entry filter, the same one the cadence asks (lifecycle_roster.go).
	// This used to be a hand-copy of the cadence's two conditions.
	if !lifecyclePolicyFor(*m).ShouldExist() {
		return reconcileDecision{}
	}
	reconcileLog("instant tick: member %s", memberID)
	return s.reconcileTickMemberLocked(*m, nowSecs())
}

// dispatchRobustStopNow dispatches ONE robust STOP to the member's warden
// RIGHT NOW — bypassing the cadence tick
// (handlers._dispatch_robust_stop_now: the force-stop endpoint + the
// event-driven recycle kill). Raw dispatch: it does not touch the reconcile
// store. Best-effort + fire-and-forget; gated OFF wholesale by --no-reconcile.
//
// What it skips differs by caller, and only one of them has a clock to skip:
//   - recycle kill: skips the remaining recycleGraceFor window, and the cadence
//     STOP arm is still the idempotent backstop if this frame is lost.
//   - force-stop / offboard: there is NO clock here to skip — decideDown returns
//     decisionNone for the whole soft window.
//
// 🔴 "Best-effort" no longer means "one shot" (T-ed79). Every dispatch from here
// arms RobustStopPendingAt, so a frame the fail-closed enqueue gate drops on an
// unreachable warden is re-sent by the cadence once the member is still online
// past stop_retry. Before that, the report_stopped collect in particular had no
// backstop at ALL: no arm of the decider re-derives it, so a single lost frame
// parked the member online forever on a session it had already closed out.
func (s *apiServer) dispatchRobustStopNow(memberID string) {
	if s.noReconcile {
		return
	}
	frame, ok := buildTargetFrame(reconcileCmdStop, memberID)
	if !ok {
		return
	}
	// Addressed to the warden of the machine the session is ACTUALLY on, falling
	// back to the desired machine when nothing claims it (the prior behaviour).
	// Identical for a member sitting on its own pin; it diverges only after a
	// T-b6d9 改機器 wind-down, where the pin has already moved to the DESTINATION
	// while the session being collected still runs on the origin — addressing the
	// destination there would leave the old session alive forever.
	s.enqueueToWarden(memberID, s.memberKillTargetWarden(memberID), frame)
	// Record the dispatch so the cadence can re-send it if it never lands
	// (T-ed79). Armed UNCONDITIONALLY — including on the fail-closed refusal
	// above, which is the case that needs it most: an unreachable warden is
	// exactly how a collect goes missing. See reconcileState.RobustStopPendingAt
	// and the arm at the top of reconcileDecide.
	s.noteRobustStopDispatched(memberID, nowSecs())
	// The robust kill (force-stop, report_stopped recycle, relocate) ends the
	// current session: drop its boot_ts so the respawn's first connect re-stamps
	// a fresh anchor (T-8fb2 boot_ts fix).
	s.clearSessionBootTS(memberID)
}

// noteRobustStopDispatched arms the at-least-once retry for one out-of-band
// robust STOP. Takes reconcileMu itself: every caller is an HTTP handler that
// holds no reconcile lock. A member with no store entry yet gets a fresh state
// — the marker is the only thing the entry needs to carry.
func (s *apiServer) noteRobustStopDispatched(memberID string, now float64) {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	st, ok := s.reconcileStates[memberID]
	if !ok {
		st = newReconcileState()
	}
	st.RobustStopPendingAt = now
	s.reconcileStates[memberID] = st
}

// identitySweepDedupeSecs is the window a member's cross-machine identity sweep
// is not re-broadcast within (T-bb29 §3). Reuses the stop_retry pace so a sweep
// re-fires on the same cadence a robust STOP would, no faster.
const identitySweepDedupeSecs = 90.0

// dispatchIdentitySweepNow enforces the cross-machine single-session invariant
// (T-bb29 §1-2, owner-approved rc-2230cb0158e8): once a member's 正身 is CONFIRMED
// live on its desired machine, broadcast a robust STOP for member-<id> to every
// OTHER online warden, reaping any residual same-id session left on a non-desired
// machine (the "relocate copied, didn't move" failure). The desired machine's
// warden (keepWarden) is EXCLUDED from the target set, so the just-confirmed 正身
// is NEVER swept — this is the never-zero-live-session invariant by construction
// (owner's hard safety gate: we only ever kill sessions on machines OTHER than
// the one that just came up healthy; §3 structural exclusion).
//
// Reuses the existing `stop` verb (idempotent — a warden without member-<id>
// no-ops, spec/sse.md §7) over the existing warden-command band: ZERO warden
// change, ZERO wire change. Deduped per identitySweepDedupeSecs so a steady-state
// reconnect flap does not re-spam. Caller MUST hold reconcileMu. Gated OFF
// wholesale by --no-reconcile, like the producer's other warden-command
// dispatches.
func (s *apiServer) dispatchIdentitySweepNow(memberID, keepWarden string, now float64) {
	if s.noReconcile || memberID == "" {
		return
	}
	if last, ok := s.identitySweepAt[memberID]; ok && now-last < identitySweepDedupeSecs {
		return // swept recently — a reconnect flap must not re-broadcast
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		return
	}
	frame, ok := buildTargetFrame(reconcileCmdStop, memberID)
	if !ok {
		return
	}
	swept := false
	for _, m := range members {
		if m.Kind != KindWarden || m.RosterStatus != RosterStatusActive {
			continue
		}
		if m.ID == keepWarden || !s.hub.IsOnline(m.ID) {
			continue // never the 正身's own machine; only reachable wardens
		}
		if s.enqueueToWarden(memberID, m.ID, frame) {
			swept = true
			reconcileLog("identity-sweep: %s confirmed on desired machine %s — "+
				"robust stop residual session on %s", memberID, keepWarden, m.ID)
		}
	}
	if swept {
		s.identitySweepAt[memberID] = now
	}
}

// identitySweepOnConnect is the SSE first-connect trigger for the cross-machine
// single-session sweep (T-bb29 §1). It fires the sweep ONLY when this connection
// is the 正身 on the expected machine: desired_state online AND the connection's
// machine claim (server-minted, unforgeable) == the member's expected machine.
// A wanderer whose claim != expected (an old instance from before a relocate /
// a stale spawn retry) does NOT initiate a sweep — it is itself the TARGET of
// the real 正身's sweep from the correct machine (§1 wanderer case).
//
// The expected machine per kind:
//   - staff (kind=assistant): the owner-pinned desired_machine_id;
//   - outsource (A案 P6 — the former KindOutsource exclusion is REMOVED now the
//     P5b naming convergence lets a member-verb stop target member-<ow-id>):
//     the owner pin when concrete, else the machine the server ACTUALLY
//     dispatched the last start to (workerSpawnTarget — a task-level or manual
//     placement leaves no durable pin on the worker row). Restart amnesia /
//     never-dispatched reads "" → no sweep
//     (fail-safe: an unverifiable 正身 never initiates a kill). This closes the
//     2026-07-19 seth-m1 hole: a spawn retry's live doppelganger on another
//     machine is reaped the moment the 正身 connects on the dispatched machine.
//
// Best-effort; a read fault or a warden sub is a clean no-op. Gated OFF by
// --no-reconcile.
//
// 🔴 CORRECTION (T-170e stage 3 — this comment used to say something false).
// It read:
//
//	"Lock order: outsourceMu (worker target read) strictly BEFORE reconcileMu
//	 — the one place both are held; nothing takes them reversed."
//
// The last clause was true; the middle one was not, and it is the half that
// got quoted. THIS FUNCTION NEVER HOLDS BOTH LOCKS. The worker target read
// goes through workerSpawnObs (worker_spawn.go), which takes s.outsourceMu and
// releases it with `defer` inside its OWN body — so by the time control
// returns here and s.reconcileMu.Lock() below runs, outsourceMu is already
// gone. The neighbouring comments said so all along and contradicted this one:
// workerSpawnObs's own doc ("…the identity-sweep 正身 check, which never hold
// s.outsourceMu") and connectionIsTheGenuineArticle's ("Both callers reach
// this WITHOUT holding s.outsourceMu").
//
// Re-measured over every non-test .go in the package (over-approximate call
// graph: any identifier matching a declared name counts as an edge, so method
// values passed without parens — `Run: s.stampContextHighRecycle` — count):
// ZERO paths in either direction acquire one of {reconcileMu, outsourceMu}
// while the other is held. There is therefore no ordering edge between them at
// all, in either direction — not an order to obey, and not a deadlock to fear.
//
// The record is kept rather than the sentence silently swapped because the
// false version was cited as a hard technical obstacle ("merging the two ticks
// would invert a documented lock order") in T-170e stage 3's first write-up.
// It was not one.
//
// ✅ THE TICKS HAVE SINCE BEEN MERGED (T-14 item 5, lifecycle_tick.go), and
// this paragraph's forecast held. The constraint that actually bit was the one
// named here — SELF-deadlock, not inversion — and the merge avoided it by not
// creating the situation at all: runLifecycleTick holds NEITHER mutex, and each
// half takes its own inside its own body and has dropped it before the next
// line runs. So there is still no goroutine in this package holding both, and
// the "ZERO paths in either direction" measurement above still describes the
// code as it stands. A future merged region that held outsourceMu across a
// helper which takes it again would still self-deadlock; that is why the halves
// are sequenced rather than wrapped.
//
// 🔴 SECOND CORRECTION (same stage, next pass — the paragraph you are reading
// shipped its OWN false sentence in the round that wrote the correction
// above). It used to end with a five-name hazard list:
//
//	"— workerSpawnObs, workerReportStopping, dismissOutsourceWorkersForTask,
//	 dismissOutsourceWorkerByID, noteWorkerStopNoSuchSession."
//
// That was not the hazard set. It omitted workerReportStopped,
// workerReportWaking and workerRestartSelf — the three siblings sitting beside
// workerReportStopping in the same file, called from the same handlers, taking
// the same lock — and foldWorkerCommandResult, stampReportedLaunchFacts and
// relocateWorkerByID besides. For a "do not call these" list, omission is the
// dangerous direction, and this one drifted inside a single stage with nothing
// able to report it.
//
// So it is not re-typed, and no successor list is offered. THE HAZARD SET IS
// WHAT THIS GREP RETURNS, minus runOutsourceTick itself (the tick that would
// be the one holding the lock):
//
//	grep -rn 'outsourceMu\.Lock()' --include='*.go' . | grep -v _test.go
//
// outsourceMu is a plain sync.Mutex (api_stub.go), so Lock is the only acquire
// form and that grep cannot miss one. Run it before you merge; do not trust a
// count written here — at the time of writing it was 18 sites in 18 distinct
// functions, one of which is runOutsourceTick.
//
// The safety claim is unchanged and never rested on the list: runReconcileTick's
// call tree reaches NONE of those acquirers today — all of them, not merely the
// five that happened to be named.
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
		return // not the 正身 on its expected machine — do not initiate a sweep
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	s.dispatchIdentitySweepNow(memberID, machineClaim, nowSecs())
}

// connectionIsTheGenuineArticle answers the ONE question both connect-edge folds
// turn on: is the session that just opened this stream the 正身 the server
// actually dispatched to THIS machine, or a wanderer whose claim carries no
// authority? machineClaim is server-minted and unforgeable, so the whole test is
// whether it equals the machine the server EXPECTED this member on:
//
//   - staff (kind=assistant): the owner-pinned desired_machine_id;
//   - outsource: the owner pin when concrete, else the machine the server
//     ACTUALLY dispatched the last start to (workerSpawnTarget — a task-level or
//     manual placement leaves no durable pin on the worker row).
//
// "" on either side ⇒ UNVERIFIABLE ⇒ false. Restart amnesia / never-dispatched
// reads "" and is deliberately fail-safe: an unverifiable connection neither
// initiates a kill (identitySweepOnConnect) nor rewrites where the worker lives
// (stampLandedMachine). Both callers reach this WITHOUT holding s.outsourceMu
// (workerSpawnObs takes it).
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
