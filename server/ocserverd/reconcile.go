package main

// reconcile.go — the server-reconcile producer (M3 step ⑥), ported from
// the retired Python service/reconcile/{machine,observation,controller,dispatch,driver,
// producer}.py against spec/lifecycle.md §4:
//
//   * reconcileDecide — the PURE per-member state machine (machine.py decide):
//     desired_state × observed-online → the ONE command to dispatch (or none),
//     with the frozen timers (§4.4): start_timeout 90s, stop_grace 120s,
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
//   * the 30s cadence tick (runReconcileTick) with the four pre-decide roster
//     passes (auto-recycle stamp / recycle loop-break / stale-stopping clear /
//     offline-warden uninstall-intent consumption)
//     and the event-driven single-member tick (reconcileMemberNow — the
//     activate/deactivate/uninstall click seam, sharing the SAME store + mutex
//     so the cadence stays an idempotent backstop).
//
// --no-reconcile (serve flag) disables the producer WHOLESALE — the cadence
// loop AND every event-driven warden-command dispatch — while the rest of the
// server (intent writes, presence, SSE) runs unchanged. This is the shadow-
// deployment kill-switch lifecycle.md Appendix B #1 requires: a shadow server
// must never wake or kill a real agent.

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

// ── config (spec/lifecycle.md §4.4 — defaults are contract) ──────────────────

type reconcileConfig struct {
	StartTimeout     float64 // START unconfirmed → failed spawn (WakingTTLSecs)
	StopGrace        float64 // self-stop window before the robust stop
	StopRetry        float64 // STOP/UNINSTALL re-dispatch window (lost frame)
	RecycleGrace     float64 // dump-stuck fallback from refocus_since
	BackoffBase      float64
	BackoffCap       float64
	CircuitThreshold int
	CircuitCooldown  float64
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
	// Sized 2×StartTimeout (180s): covers the agent's worst honest reconnect
	// (backoff cap 15s + 45s idle-read watchdog + one 30s cadence tick ≈ 90s)
	// with a full extra START-window of slack.
	ZombieConfirmGrace float64
}

func defaultReconcileConfig() reconcileConfig {
	return reconcileConfig{
		StartTimeout:     WakingTTLSecs,
		StopGrace:        StoppingTimeoutSecs,
		StopRetry:        90.0,
		RecycleGrace:     StoppingTimeoutSecs,
		BackoffBase:      5.0,
		BackoffCap:       300.0,
		CircuitThreshold: 5,
		CircuitCooldown:  120.0,
		// 2×StartTimeout — see the field comment (T-9adc zombie second-confirm).
		ZombieConfirmGrace: 2 * WakingTTLSecs,
	}
}

// reconcileCadenceSecs is the producer tick period (§4.1) — sized to the
// runtime's 30s presence heartbeat.
const reconcileCadenceSecs = 30.0

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
	AgentStopped bool // stopped_since > 0 (the graceful dump-done fact)
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
}

func decisionNone(obs memberObservation, st reconcileState, reason string) reconcileDecision {
	return reconcileDecision{
		Command: reconcileCmdNone, MemberID: obs.MemberID, Reason: reason, State: st,
	}
}

// ── the pure decision function (machine.py decide) ───────────────────────────

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
	switch obs.Desired {
	case DesiredStateUninstall:
		return decideUninstall(obs, st, cfg, now)
	case DesiredStateOnline:
		return decideUp(obs, st, cfg, now)
	}
	return decideDown(obs, st, cfg, now)
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
		graceExpired := now >= obs.RefocusSince+cfg.RecycleGrace
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
					Reason: reason, State: st,
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
		// refocus-free member whose running machine no longer matches its target is
		// recycled exactly like refocus — a robust STOP now, desired_state stays
		// online, so the next tick's plain START re-mints the boot token on the NEW
		// machine (wardenTargetOf routes START by desired_machine). The STOP is
		// routed to the RUNNING machine's warden (DispatchWarden), not the target's,
		// because that is where the session to kill actually lives. Guarded to the
		// SAFE cases ONLY: a pinned target, a KNOWN running machine (never "" — a
		// claim-less/booting member must never be flapped into a STOP→START loop),
		// and an actual mismatch. refocus never reaches here (handled above), so the
		// two recycles never stack.
		if obs.TargetMachine != "" && obs.RunningMachine != "" &&
			obs.RunningMachine != obs.TargetMachine {
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
					Reason: reason, State: st, DispatchWarden: obs.RunningMachine,
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
		return decisionNone(obs, st, "online: converged")
	}

	// Not online. A START may be in flight — give it the start window.
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
				return decisionNone(obs, st,
					"zombie suspect: START clobbered a live presence-deaf session — "+
						"withholding takeover stop inside the reconnect-confirm grace")
			}
			st.Phase = reconcilePhaseStopping
			st.LastCommand = reconcileCmdStop
			st.LastCommandAt = now
			return reconcileDecision{
				Command: reconcileCmdStop, MemberID: obs.MemberID,
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
	}
	if st.CircuitOpen {
		st.Phase = reconcilePhaseCircuitOpen
		return decisionNone(obs, st, "circuit open: respawn disabled")
	}
	if now < st.BackoffUntil {
		st.Phase = reconcilePhaseBackoff
		return decisionNone(obs, st, "backoff: awaiting retry window")
	}
	st.Phase = reconcilePhaseStarting
	st.LastCommand = reconcileCmdStart
	st.LastCommandAt = now
	return reconcileDecision{
		Command: reconcileCmdStart, MemberID: obs.MemberID,
		Reason: "spawn: desired_state online, no live session", State: st,
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
	if st.StopDeadline == 0.0 {
		// First observation of desired_state=offline → arm the grace clock. The
		// clock arms from OBSERVING the intent, never from a dispatched command.
		st.Phase = reconcilePhaseStopping
		st.StopDeadline = now + cfg.StopGrace
		return decisionNone(obs, st,
			"stopping: grace window opened — awaiting agent selfstop")
	}
	if now < st.StopDeadline {
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
		}
		st.Phase = reconcilePhaseStopping
		st.LastCommand = reconcileCmdStop
		st.LastCommandAt = now
		return reconcileDecision{
			Command: reconcileCmdStop, MemberID: obs.MemberID,
			Reason: reason, State: st,
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
// that warden's own member id); no active warden → the raw host string as an
// inert fallback the reachability gate then fails closed on.
func (s *apiServer) wardenTargetOf(memberID string) string {
	target, err := s.dal.GetMember(memberID)
	if err == nil && target != nil && target.Kind == KindWarden {
		return target.ID
	}
	host := ""
	if target != nil {
		host = target.DesiredMachineID
	}
	if host != "" {
		cand, err := s.dal.GetMember(host)
		if err == nil && cand != nil && cand.Kind == KindWarden &&
			cand.RosterStatus == RosterStatusActive {
			return cand.ID
		}
	}
	return host
}

// enqueueWardenFrame pushes one command frame onto the target member's warden
// FIFO, fail-closed behind the target-reachability gate: the addressed warden
// must itself be online (hold the live SSE downstream that drains the frame),
// otherwise nothing is enqueued and the caller keeps prior state (no phantom
// START, no ghost STOP into a dead buffer). Returns accepted.
func (s *apiServer) enqueueWardenFrame(memberID string, frame []byte) bool {
	return s.enqueueToWarden(memberID, s.wardenTargetOf(memberID), frame)
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
	s.hub.EnqueueWardenCommand(warden, frame)
	return true
}

// buildStartFrame assembles the START wire frame server-side (producer.py
// BootstrapStartPayload): fold the persona via the shared boot core + mint the
// member JWT. FAIL-CLOSED (nil, false) on an inactive member, an unknown
// role, or a missing secret — never boot a ghost or a deaf member.
func (s *apiServer) buildStartFrame(m Member) ([]byte, bool) {
	if m.RosterStatus != RosterStatusActive {
		return nil, false
	}
	if len(s.secret) == 0 {
		return nil, false
	}
	boot, err := s.buildBootContext("", &m, "")
	if err != nil || boot == nil {
		if err != nil {
			reconcileLog("START fold failed for %q: %v", m.ID, err)
		}
		return nil, false
	}
	token, err := s.mintMemberToken(m, s.authTokenTTL())
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
			TaskType:       boot.TaskType,
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
		AgentStopped:   m.StoppedSince > 0.0,
		LastOpKind:     m.LastOp,
		LastOpReason:   m.LastOpReason,
		TargetMachine:  m.DesiredMachineID,
		RunningMachine: s.hub.MachineOf(m.ID),
	}
	decision := reconcileDecide(obs, st, s.reconcileCfg, now)
	switch decision.Command {
	case reconcileCmdNone:
		return decision
	case reconcileCmdStart:
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
			decision.State = st // keep the prior state → re-dispatch next tick
			decision.DispatchUnlanded = true
			return decision
		}
		// A landed START begins a NEW session: drop any prior session's boot_ts
		// anchor so the fresh agent's first connect re-stamps (T-8fb2 boot_ts fix).
		s.clearSessionBootTS(m.ID)
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
	return decision
}

// ── pre-decide roster passes (producer.py, run inside the cadence tick) ──────

// stampContextHighRecycle auto-stamps refocus_since on any candidate whose
// actionable context pct is in the HANDOVER band (§4.5 auto-stamp) — the
// automatic counterpart of the manual refocus button, reusing the SSE band's
// stale-pct + boot-storm guards so an unreliable gauge never auto-recycles.
// Mutates the in-slice member so the SAME tick's observation sees the marker.
func (s *apiServer) stampContextHighRecycle(members []Member, now float64) {
	ctxhigh := s.ctxHighConfig()
	for i := range members {
		m := &members[i]
		if m.RefocusSince > 0.0 {
			continue // already recycling — the marker IS the cooldown
		}
		record := s.gauge.Get(m.ID)
		pct := actionableContextPct(record, ctxhigh.StaleGuard)
		if bandFor(pct, ctxhigh.WarnPct, ctxhigh.HandoverPct) != levelHandover {
			continue
		}
		if bootStormTripped(gaugeSecsSinceBoot(record, now), ctxhigh.MinBootSecs) {
			continue // fresh boot already over the line → suppress (loop-guard)
		}
		if !s.hub.IsOnline(m.ID) {
			continue // only-online (symmetric with the manual refocus gate)
		}
		m.RefocusSince = now
		if err := s.putMember(*m, triggerServer); err != nil {
			reconcileLog("recycle: auto-stamp persist failed for %s: %v", m.ID, err)
			continue
		}
		reconcileLog("recycle: context-high auto-stamp refocus_since for %s", m.ID)
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
func (s *apiServer) clearRecycleMarkersOnRespawn(members []Member) {
	for i := range members {
		m := &members[i]
		if m.DesiredState != DesiredStateOnline {
			continue
		}
		if m.RefocusSince <= 0.0 {
			continue // plain respawn — nothing to clear
		}
		if s.hub.IsOnline(m.ID) {
			continue // still online = recycle-PENDING (dump in flight), not a respawn
		}
		m.RefocusSince = 0.0
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
// --no-reconcile like every other desired-state control write.
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

// clearStaleStoppingOnOnline is the survived-stop auto-clear (§4.5): a
// desired-online member OBSERVED online while still carrying a stopping_since
// anchor is provably past that stop — clear the anchor so it can never derive
// a phantom *stopping* forever.
func (s *apiServer) clearStaleStoppingOnOnline(members []Member) {
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

// runReconcileTick runs ONE producer tick over the roster snapshot: the three
// pre-decide passes, then decide→dispatch per candidate. Candidates (§4.1):
// every ACTIVE non-warden member, plus any ACTIVE warden whose desired_state
// is uninstall. Serialized with the event-driven ticks via reconcileMu;
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
		if m.RosterStatus != RosterStatusActive {
			continue
		}
		if m.Kind == KindWarden && parseDesired(m.DesiredState) != DesiredStateUninstall {
			continue // no warden reconciles another warden's spawn/stop
		}
		members = append(members, m)
	}
	s.stampContextHighRecycle(members, now)
	s.clearRecycleMarkersOnRespawn(members)
	s.clearStaleStoppingOnOnline(members)
	s.consumeUninstallIntentOnOffline(members)
	reconcileLog("tick: %d candidate(s)", len(members))
	for i := range members {
		s.reconcileTickMemberLocked(members[i], now)
	}
}

// startReconcileCadence mounts the always-on 30s producer loop (§4.1) — the
// Python mount_reconcile_producer twin. The first tick fires one full period
// after start (sleep-then-tick, matching the asyncio cadence). Never called
// when --no-reconcile is set.
func (s *apiServer) startReconcileCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.runReconcileTick(nowSecs())
		}
	}()
	reconcileLog("cadence started (period=%gs)", period.Seconds())
}

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
	if err != nil || m == nil || m.RosterStatus != RosterStatusActive {
		return reconcileDecision{}
	}
	if m.Kind == KindWarden && parseDesired(m.DesiredState) != DesiredStateUninstall {
		return reconcileDecision{} // a warden is never an agent-lifecycle spawn/stop candidate
	}
	reconcileLog("instant tick: member %s", memberID)
	return s.reconcileTickMemberLocked(*m, nowSecs())
}

// dispatchRobustStopNow dispatches ONE robust STOP to the member's warden
// RIGHT NOW — bypassing BOTH the cadence tick AND the machine's grace clock
// (handlers._dispatch_robust_stop_now: the force-stop endpoint + the
// event-driven recycle kill). Raw dispatch: it does not touch the reconcile
// store — the cadence STOP arm is the idempotent backstop. Best-effort +
// fire-and-forget; gated OFF wholesale by --no-reconcile.
func (s *apiServer) dispatchRobustStopNow(memberID string) {
	if s.noReconcile {
		return
	}
	frame, ok := buildTargetFrame(reconcileCmdStop, memberID)
	if !ok {
		return
	}
	s.enqueueWardenFrame(memberID, frame)
	// The robust kill (force-stop, report_stopped recycle, relocate) ends the
	// current session: drop its boot_ts so the respawn's first connect re-stamps
	// a fresh anchor (T-8fb2 boot_ts fix).
	s.clearSessionBootTS(memberID)
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
// wholesale by --no-reconcile (like every other warden-command dispatch).
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
//     dispatched the last start to (workerSpawnTarget — "auto" placement has no
//     durable pin). Restart amnesia / never-dispatched reads "" → no sweep
//     (fail-safe: an unverifiable 正身 never initiates a kill). This closes the
//     2026-07-19 seth-m1 hole: a spawn retry's live doppelganger on another
//     machine is reaped the moment the 正身 connects on the dispatched machine.
//
// Best-effort; a read fault or a warden sub is a clean no-op. Gated OFF by
// --no-reconcile. Lock order: outsourceMu (worker target read) strictly BEFORE
// reconcileMu — the one place both are held; nothing takes them reversed.
func (s *apiServer) identitySweepOnConnect(memberID, machineClaim string) {
	if s.noReconcile || memberID == "" || machineClaim == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil || m.Kind == KindWarden {
		return
	}
	expected := m.DesiredMachineID
	if m.Kind == KindOutsource {
		if expected == "" || expected == "auto" {
			expected, _ = s.workerSpawnObs(memberID)
		}
	}
	if parseDesired(m.DesiredState) != DesiredStateOnline ||
		expected == "" || expected != machineClaim {
		return // not the 正身 on its expected machine — do not initiate a sweep
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	s.dispatchIdentitySweepNow(memberID, machineClaim, nowSecs())
}
