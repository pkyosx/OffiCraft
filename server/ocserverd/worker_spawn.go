package main

// Outsource-worker wake / rescue / reclaim lifecycle. A worker IS a member row
// (kind='outsource') and rides the member `start`/`stop` verbs: member_id == the
// ow- id, session member-<ow-id>; the retired worker_start/worker_stop verbs and
// the worker-<ow-id> session survive only as the warden-side legacy-kill guard.
// Rescue runs through the SAME pure reconcile FSM the members use (reconcileDecide). What
// stays outsource-specific is placement and boot content.
//
// Wake: scheduler assigns (row 'assigned') → notifyWorkerSpawn (boot context +
// server-minted token) → member `start` frame on the warden's FIFO → warden boots
// tmux member-<ow-id> → the worker's first report_waking flips it 'active'; that
// flip, not a warden receipt, is the observable wake signal.
//
// Reclaim: closeTask → dismissOutsourceWorkersForTask releases the row and
// reclaims the session in the same call; a released worker whose session was
// never reclaimed is reclaimed by the cadence workerReclaimGraceSecs after
// release. The warden's stop targets exactly member-<ow-id> (plus the legacy
// worker-<ow-id> residual), nothing else.
//
// Spawn bookkeeping (workerSpawnAt / workerSpawnTarget / workerSpawnAttempts /
// workerReclaimed, reconcileStates) is in-memory: a restart costs at most one
// extra start (the warden's clobber guard refuses a live session) or one extra
// stop (a no-op on an absent session). Durable truth is the worker row.

import (
	"net/http"
	"strings"
)

const (
	// Retired worker verb, kept only to recognise receipts from old warden builds.
	legacyWardenCmdWorkerStart = "worker_start"
	// Presentation only (a worker has no role doc); mirrors cli/ocwarden
	// worker.go workerBootRole.
	workerBootRoleLabel = "outsource-worker"
	// Mirrors the reconcile start timeout: a healthy boot claims within it; a lost
	// start frame is re-pushed at its boundary.
	workerSpawnRetrySecs = WakingTTLSecs
	// Backstop only: a task close reclaims in the same call. Catches rows released
	// by other paths or reclaims that could not be delivered. Mirrors stop_grace /
	// recycle_grace (120s).
	workerReclaimGraceSecs = 120.0
	// Bench a machine gets for one worker after it failed to boot it (a refused
	// start receipt, or a zombie-takeover reap — lifted early by the target's OK
	// stop receipt). A pause, not a rotation: a worker only boots where it was
	// placed. 3× the re-dispatch pace so a known-bad boot is retried every few cycles.
	workerMachineBenchSecs = 3 * workerSpawnRetrySecs
)

// buildWorkerBootContext is the staff boot context minus the role content
// (owner-ruled): 系統互動 and 使用者自訂 byte-identical to staff's, then the 傳承
// block (everyone scope, then this member's own entries, one budget — owner card
// rc-3c24fdc61ed3), then the runtime's 啟動步驟 seed LAST (recency-authoritative).
// Nothing is written FOR outsource readers. The bound task, the type manual and
// an identity block are deliberately NOT embedded: the worker reads task and
// manual live (a spawn-time copy is stale), and identity arrives in the header
// the warden puts in front of this context.
func (s *apiServer) buildWorkerBootContext(w OutsourceWorker) (string, error) {
	head, err := s.workerSharedHead()
	if err != nil {
		return "", err
	}
	bootSeq, err := s.workerBootSequence(w.Runtime)
	if err != nil {
		return "", err
	}

	// Lore selection lives only in lore_select.go (the staff exit in assets.go
	// calls the same function); a second selection here would let one entry show
	// in one exit and not the other, silently. loreRoleCap is the one knob for
	// every member-scoped fold despite its name (see domain.go).
	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n\n")
	loreSel, err := selectMemberLore(s.dal, w.ID, s.loreRoleCap())
	if err != nil {
		return "", err
	}
	if block := renderLoreBlock(loreSel); block != "" {
		b.WriteString(block)
		b.WriteString("\n\n")
	}
	b.WriteString(bootSeq)
	b.WriteString("\n")
	return b.String(), nil
}

// pickWorkerWarden resolves the machine a worker boots on. Placement is an
// explicit owner decision (owner ruling 2026-07-25): only a concrete, currently
// dispatchable machine id; anything else returns "" and there is no fallback to
// another host. "auto" is not special-cased (migration 00035 normalized it away
// and writes reject it). Callers hold s.outsourceMu.
func (s *apiServer) pickWorkerWarden(w OutsourceWorker, preferred string, now float64) string {
	id, _ := s.resolveWorkerPlacement(w, preferred, now)
	return id
}

// resolveWorkerPlacement also returns WHY a placement was refused ("" when
// resolved); the reason is what the owner reads on the cockpit.
func (s *apiServer) resolveWorkerPlacement(w OutsourceWorker, preferred string, now float64) (string, string) {
	if preferred == "" {
		return "", placementReasonNoMachine + ": no machine is selected for this worker — " +
			"pick one on the worker (改機器) or on the task type's 手冊 assignee; " +
			"there is no automatic placement"
	}
	unavailable := func(detail string) (string, string) {
		return "", placementReasonUnavailable + ": machine '" + preferred + "' " + detail +
			"; no other machine is substituted"
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		return unavailable("could not be looked up (server error)")
	}
	for _, m := range members {
		if m.ID != preferred {
			continue
		}
		if m.Kind != KindWarden || m.RosterStatus != RosterStatusActive {
			return unavailable("is not an active machine")
		}
		if !s.hub.IsOnline(m.ID) {
			return unavailable("is offline")
		}
		if s.workerMachineBenched(w.ID, m.ID, now) {
			return unavailable("was just benched after a failed boot of this worker")
		}
		if detail := s.runtimePlacementRefusal(m.ID, w.Runtime); detail != "" {
			return unavailable(detail)
		}
		if !s.machineResolvesCodexModel(m.ID, w.Runtime, w.Model) {
			return unavailable(codexFamilyUnresolvedDetail(w.Model))
		}
		return m.ID, ""
	}
	return unavailable("does not exist")
}

// resolveStickyWorkerPlacement: the owner pin (DesiredMachineID) is HARD — if it
// cannot take the worker, the spawn STALLS with a receipt and nothing else is
// tried; the last landing (LastMachineID) is SOFT — fall through to `configured`
// so a closed laptop cannot strand the worker; `configured` (notifyWorkerSpawn's
// chain) is HARD and decides the birthplace. Owner ruling 2026-07-27: 「換手應該在原地,除非我有特別指定要換去別處」.
// When the sticky machine is down and nothing else is configured, the receipt
// names the sticky machine ("no machine selected" would be false).
//
// Open owner question (ruling pending): a relocate body without machine_id
// clears the pin; any fix belongs in relocateWorkerByID, not here.
// Callers hold s.outsourceMu.
func (s *apiServer) resolveStickyWorkerPlacement(w OutsourceWorker, configured string, now float64) (string, string) {
	if w.DesiredMachineID != "" {
		return s.resolveWorkerPlacement(w, w.DesiredMachineID, now)
	}
	if w.LastMachineID != "" {
		id, why := s.resolveWorkerPlacement(w, w.LastMachineID, now)
		if id != "" {
			return id, ""
		}
		if configured == "" || configured == w.LastMachineID {
			return "", why
		}
	}
	return s.resolveWorkerPlacement(w, configured, now)
}

// last_op_reason codes ("<code>: <detail>") a refused spawn writes, so a stalled
// worker names its cause on the cockpit. Every non-dispatch leaves a receipt.
const (
	placementReasonNoMachine   = "no_machine_selected"
	placementReasonUnavailable = "machine_unavailable"
	spawnReasonNoLiveTask      = "no_live_task"
	spawnReasonBootContext     = "boot_context_failed"
	spawnReasonNoSecret        = "no_signing_secret"
	spawnReasonTokenMint       = "token_mint_failed"
	spawnReasonFrameBuild      = "frame_build_failed"
	spawnReasonWardenLost      = "warden_unreachable"
	spawnReasonRespawnDeferred = "respawn_deferred"
	spawnReasonWakeTimeout     = "wake_timeout"
	spawnReasonNeverCollected  = "never_collected"
	spawnReasonHeldDown        = "held_down"
	// Staff-side diagnoses (decideUp); a landed START invalidates all three.
	spawnReasonCircuitOpen   = "circuit_open"
	spawnReasonBackoff       = "backoff"
	spawnReasonZombieSuspect = "zombie_suspect"
	// Receipt 喚醒 leaves on an agent that is still running, instead of a 409 —
	// staff and outsource alike (owner rulings 外包也不擋, rc-a8f7044ba92f).
	spawnReasonSessionAlive = "session_alive"
	// Known limitation (owner ruling): the receipt is a single slot, so the newest
	// code overwrites an earlier diagnosis (e.g. never_collected → wake_timeout).
)

// spawnBlockedReasonCodes is the closed set isSpawnBlockedReason matches, so
// a landed START clears any of them; a new "did not dispatch" code must be added
// here or its stamp outlives its cause. Deliberately absent: wake_timeout and
// never_collected (the retry that follows must not erase why the previous
// dispatch failed — dispatching is not delivery) and held_down (only a restart
// ends it, and that writes its own receipt). session_alive is in: a wake on a
// running worker dispatches nothing, so the next landed START refutes it.
var spawnBlockedReasonCodes = []string{
	placementReasonNoMachine, placementReasonUnavailable,
	spawnReasonNoLiveTask, spawnReasonBootContext, spawnReasonNoSecret,
	spawnReasonTokenMint, spawnReasonFrameBuild, spawnReasonWardenLost,
	spawnReasonRespawnDeferred,
	spawnReasonCircuitOpen, spawnReasonBackoff, spawnReasonZombieSuspect,
	spawnReasonSessionAlive,
}

const sessionAliveWakeReceipt = spawnReasonSessionAlive + ": it was already " +
	"running — 喚醒 left that session alone and dispatched nothing. Its work, and " +
	"any 加速停止 or 重新聚焦 already under way on it, are untouched. To end the " +
	"current session and start a fresh one, press 強制停止 first, then 喚醒"

// stampSessionAliveWakeReceipt is the one writer of that receipt, staff and outsource alike. It is a
// success with a note, not a refusal: leaving a running session alone is what 喚醒 is meant to do.
func stampSessionAliveWakeReceipt(m *Member, now float64) {
	stampOpNoteReceipt(&m.LastOp, &m.LastOpOK, &m.LastOpLog, &m.LastOpReason, &m.LastOpAt,
		reconcileCmdStart, sessionAliveWakeReceipt, now)
}

const sessionAliveWakeNote = " — the start window then lapsed, but that is NOT a " +
	"runtime failure: the previous session is still running and the warden refused " +
	"to stomp it, so nothing new was ever started. Do not go looking for a broken " +
	"runtime on that machine; deal with the live session — press 強制停止 to " +
	"end it, then 喚醒."

// wakeTimeoutOverWardenReceipt keeps a warden's clobber or not-logged-in refusal
// from being overwritten by a wake_timeout stamp (clearWorkerPlacementBlock
// already never touches a warden receipt); the not-logged-in one is returned
// unchanged, so the stamp writes nothing. The clobber arm is a defence, not a
// fix: no production path reaches it today, because reconcile.go returns early
// on the clobber prefix before StartTimedOut is set — an FSM reorder there would
// make it live.
// The warden's line stays verbatim and IN FRONT: reconcile.go and
// api_monitoring.go dispatch on HasPrefix(spawnClobberReasonPrefix), and losing
// the prefix silently disarms the zombie takeover. Legacy `worker_start` rows
// are deliberately not folded here. Keep it narrow: a blanket "wake_timeout
// never overwrites" would drop the stamp on rows with no warden receipt.
func wakeTimeoutOverWardenReceipt(fresh OutsourceWorker, reason string, spawnAt float64) string {
	if !strings.HasPrefix(reason, spawnReasonWakeTimeout+":") {
		return reason
	}
	if wakeTimeoutYieldsToReceipt(fresh.LastOp, fresh.LastOpReason, fresh.LastOpAt, spawnAt) {
		return fresh.LastOpReason
	}
	if fresh.LastOp != reconcileCmdStart ||
		!strings.HasPrefix(fresh.LastOpReason, spawnClobberReasonPrefix+":") {
		return reason
	}
	if strings.Contains(fresh.LastOpReason, sessionAliveWakeNote) {
		return fresh.LastOpReason
	}
	return fresh.LastOpReason + sessionAliveWakeNote
}

// stampWorkerPlacementBlocked records why a worker was not dispatched on the
// cockpit's 「最近操作」 fields. Writes only when the reason changes: the 30s
// cadence retries a blocked spawn forever, and an unconditional write would fan
// an SSE delta every tick. Decides on a re-read row because closeTask releases
// workers without outsourceMu; a released or vanished worker is left alone.
// Best-effort: a persist failure never changes the dispatch decision.
func (s *apiServer) stampWorkerPlacementBlocked(w *OutsourceWorker, reason string, now float64) {
	outsourceLog("spawn %s (%s): %s", w.ID, w.Codename, reason)
	var stamped *OutsourceWorker
	if err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getOutsourceWorkerOn(tx, w.ID)
		if err != nil || fresh == nil || fresh.Status == WorkerStatusReleased {
			return err
		}
		reason := wakeTimeoutOverWardenReceipt(*fresh, reason, s.workerSpawnAt[w.ID])
		if stopgapRetryStampYields(fresh.LastOpReason, reason) {
			return nil
		}
		if fresh.LastOp == reconcileCmdStart && fresh.LastOpReason == reason {
			return nil
		}
		stampOpReceipt(&fresh.LastOp, &fresh.LastOpOK, &fresh.LastOpLog, &fresh.LastOpReason,
			&fresh.LastOpAt, reconcileCmdStart, reason, now)
		stamped = fresh
		return setMemberLastOpOn(tx, fresh.ID, fresh.LastOp, fresh.LastOpOK, fresh.LastOpLog,
			fresh.LastOpReason, fresh.LastOpAt)
	}); err != nil {
		outsourceLog("spawn %s: placement-blocked stamp persist failed: %v", w.ID, err)
		return
	}
	if stamped != nil {
		s.publishOutsourceWorker(*stamped, triggerServer)
	}
}

// clearWorkerPlacementBlock drops a spawn-blocked stamp once a start is
// dispatched, so a later identical block re-stamps last_op_at instead of being
// swallowed by the anti-churn guard. A warden's own receipt is never touched: a
// dispatch is an attempt, not an outcome.
func (s *apiServer) clearWorkerPlacementBlock(workerID string) {
	if err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getOutsourceWorkerOn(tx, workerID)
		if err != nil || fresh == nil || fresh.LastOp != reconcileCmdStart {
			return err
		}
		if !dropSpawnBlockedNote(&fresh.LastOpLog, &fresh.LastOpReason) {
			return nil
		}
		// A refusal's false goes to nil: that verdict belonged to the note just retired, and the
		// dispatched start has none yet. nil still paints ✗ (receiptRendersAsFailure) until the
		// converged-online clear removes the line. Staff keeps the false instead; both paint the same.
		if fresh.LastOpOK != nil && !*fresh.LastOpOK {
			fresh.LastOpOK = nil
		}
		// last_op and last_op_at are written back unchanged on purpose: last_op_at is
		// what tells "stalled an hour ago" from "stalled now". Do not zero them.
		return setMemberLastOpOn(tx, fresh.ID, fresh.LastOp, fresh.LastOpOK, fresh.LastOpLog,
			fresh.LastOpReason, fresh.LastOpAt)
	}); err != nil {
		outsourceLog("spawn %s: placement-block clear failed: %v", workerID, err)
	}
}

// clearWorkerConvergedFailureReceipt removes a FAILED receipt once the worker is
// back online (owner ruling rc-f2e963132fc5 [1]: 「他回來了就把那行字直接拿掉」);
// success receipts are never touched. Judged on the re-read row — an owner
// action may have written a receipt mid-tick; the snapshot only short-circuits.
// Caller holds outsourceMu.
func (s *apiServer) clearWorkerConvergedFailureReceipt(workerID string, snapshot OutsourceWorker) {
	if !receiptRendersAsFailure(snapshot.LastOp, snapshot.LastOpAt, snapshot.LastOpOK) {
		return
	}
	var cleared *OutsourceWorker
	if err := s.dal.inTx(func(tx *writeTx) error {
		fresh, err := getOutsourceWorkerOn(tx, workerID)
		if err != nil || fresh == nil {
			return err
		}
		if !receiptRendersAsFailure(fresh.LastOp, fresh.LastOpAt, fresh.LastOpOK) {
			return nil
		}
		fresh.LastOp = ""
		fresh.LastOpOK = nil
		fresh.LastOpLog = ""
		fresh.LastOpReason = ""
		fresh.LastOpAt = 0.0
		cleared = fresh
		return setMemberLastOpOn(tx, fresh.ID, fresh.LastOp, fresh.LastOpOK, fresh.LastOpLog,
			fresh.LastOpReason, fresh.LastOpAt)
	}); err != nil {
		outsourceLog("%s: converged receipt clear failed: %v", workerID, err)
		return
	}
	if cleared != nil {
		s.publishOutsourceWorker(*cleared, triggerServer)
	}
}

func workerMachineKey(workerID, machineID string) string {
	return workerID + "|" + machineID
}

// Callers hold s.outsourceMu.
func (s *apiServer) workerMachineBenched(workerID, machineID string, now float64) bool {
	until, ok := s.workerMachineBench[workerMachineKey(workerID, machineID)]
	return ok && now < until
}

// Callers hold s.outsourceMu.
func (s *apiServer) benchWorkerMachine(workerID, machineID string, now float64) {
	if machineID == "" {
		return
	}
	s.workerMachineBench[workerMachineKey(workerID, machineID)] = now + workerMachineBenchSecs
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// notifyWorkerSpawn dispatches one member `start` frame, paced by
// workerSpawnRetrySecs so the cadence may call it for every 'assigned' worker (a
// duplicate against a live session is refused by the warden's clobber guard).
// Every refusal past the pacing check stamps a receipt. Returns whether a frame
// was enqueued. Callers hold s.outsourceMu.
func (s *apiServer) notifyWorkerSpawn(w OutsourceWorker, now float64) bool {
	if last, ok := s.workerSpawnAt[w.ID]; ok && now-last < workerSpawnRetrySecs {
		return false
	}
	t, err := s.dal.GetTask(w.TaskID)
	if err != nil || t == nil || TaskIsTerminal(t.Status) {
		s.stampWorkerPlacementBlocked(&w, spawnReasonNoLiveTask+": bound task "+w.TaskID+
			" is missing or already closed — nothing left to boot this worker for", now)
		return false
	}
	var manual *TaskManual
	if t.TypeKey != "" {
		if m, err := s.foldTaskManual(t.TypeKey); err == nil {
			manual = m
		}
	}
	// Placement is resolved here at spawn time, from owner-authored sources only
	// (no automatic placement — owner ruling 2026-07-25). Below the pin and the
	// last landing (resolveStickyWorkerPlacement): the reassign dialog's in-memory
	// pick, then the task row and the type manual. The task row is load-bearing:
	// workerMachinePref is lost on restart, and without the row an ad-hoc 發包
	// (no manual) would stall permanently.
	machinePref := s.workerMachinePref[w.ID]
	if machinePref == "" {
		manualMachine := ""
		if manual != nil {
			if spec := outsourceSpecOf(*manual); spec != nil {
				manualMachine = spec.Machine
			}
		}
		// An explicit 發包 target (OutsourceDispatched) outranks the manual; otherwise
		// the row holds only the creator's snapshot, which loses to the live manual.
		if t.OutsourceDispatched {
			machinePref = firstNonEmpty(t.OutsourceMachine, manualMachine)
		} else {
			machinePref = firstNonEmpty(manualMachine, t.OutsourceMachine)
		}
	}
	warden, blocked := s.resolveStickyWorkerPlacement(w, machinePref, now)
	if warden == "" {
		// Nothing is dispatched, and the stall is stamped onto the worker row so the
		// cockpit says WHY.
		s.stampWorkerPlacementBlocked(&w, blocked, now)
		return false
	}
	persona, err := s.buildWorkerBootContext(w)
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonBootContext+
			": could not assemble the worker's boot context: "+err.Error(), now)
		return false
	}
	if len(s.keys.signingSecret()) == 0 {
		s.stampWorkerPlacementBlocked(&w, spawnReasonNoSecret+
			": the server has no JWT signing secret, so no worker token can be minted", now)
		return false
	}
	// Server-minted: sub == the ow- id (authz floors an unknown sub to the agent
	// class, a worker's ceiling). The token rides only this directed frame; the
	// warden lands it in a 0600 file — never a log, chat or transcript. The
	// machine_id claim is the dispatched machine, as mintMemberToken does.
	token, err := s.mintAgentToken(w.ID, warden, s.agentTokenTTLValue())
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonTokenMint+
			": minting this worker's session token failed: "+err.Error(), now)
		return false
	}
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC: reconcileCmdStart,
		Args: wardenStartArgs{
			MemberID:       w.ID,
			PersonaContext: persona,
			MemberToken:    token,
			Role:           workerBootRoleLabel,
			Runtime:        NormalizeRuntime(w.Runtime),
			Model:          w.Model,
			Effort:         w.Effort,
		},
	})
	if err != nil {
		s.stampWorkerPlacementBlocked(&w, spawnReasonFrameBuild+
			": could not build this worker's start frame: "+err.Error(), now)
		return false
	}
	if !s.enqueueToWarden(w.ID, warden, frame) {
		s.stampWorkerPlacementBlocked(&w, spawnReasonWardenLost+": machine '"+warden+
			"' went offline between the placement decision and the dispatch", now)
		return false
	}
	if s.workerStopPending[w.ID] == warden {
		// Drop the parked kill so a late re-fire cannot shoot the NEW session.
		delete(s.workerStopPending, w.ID)
	}
	if armed, ok := s.workerStopLanded[w.ID]; ok && (!armed.aimed() || armed.Target == warden) {
		// An aimed kill toward another machine stays armed (改機器: the old box still
		// owes a dead session); a fan-out is disarmed by ANY landed START. Keying the
		// fan-out on its member list was measured to kill the replacement: the
		// re-send re-resolves the chain to the new session's machine.
		// Rests on: a worker session is created ONLY through this function. If any
		// boot path bypasses it (e.g. warden auto-revive), redesign — tie the arm to
		// a session generation — no test in this package would go red.
		delete(s.workerStopLanded, w.ID)
	}
	// A landed START begins a new session: drop the old boot_ts anchor here (as
	// the member producer does). It is durable, so a leftover would be adopted by
	// the fresh session and wave through the respawn-storm floor. Also needed for
	// starts with no handover stop before them (FSM rescue after a crash).
	// restoreRefusedStartAnchor puts it back if the warden refuses the START.
	s.clearSessionBootTSForStart(w.ID)
	// Stamp waking_since at dispatch — the staff rule (stampWakeObservability):
	// PresenceState reads it for both kinds, it survives restart (workerSpawnAt
	// does not), and a worker that never boots never reports waking.
	if err := s.dal.SetMemberWakingSince(w.ID, now); err != nil {
		outsourceLog("spawn %s: waking anchor persist failed: %v", w.ID, err)
	}
	w.WakingSince = now
	s.workerSpawnAt[w.ID] = now
	s.workerSpawnTarget[w.ID] = warden
	s.armReceiptWatch(w.ID, reconcileCmdStart, warden, now)
	s.workerSpawnAttempts[w.ID]++
	// Mark the start in flight for the shared FSM; otherwise its own START would
	// bounce off the clobber guard and misread the healthy boot as a zombie.
	st := s.reconcileStateOf(w.ID)
	st.Phase = reconcilePhaseStarting
	st.LastCommand = reconcileCmdStart
	st.LastCommandAt = now
	s.setReconcileState(w.ID, st)
	s.clearWorkerPlacementBlock(w.ID)
	s.publishOutsourceWorker(w, triggerServer)
	outsourceLog("spawn %s (%s) dispatched → warden %s (task %s, attempt %d)",
		w.ID, w.Codename, warden, t.ID, s.workerSpawnAttempts[w.ID])
	return true
}

// workerSpawnObs is for callers NOT holding s.outsourceMu (projectWorker, the
// identity sweep); callers holding it read the maps directly (this would deadlock).
func (s *apiServer) workerSpawnObs(workerID string) (target string, at float64) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	return s.workerSpawnTarget[workerID], s.workerSpawnAt[workerID]
}

// reconcileWorkerLiveness runs one non-stopped worker through the shared member
// FSM (reconcileDecide). Relocation stays masked: its event-driven handler owns
// the placement change. Returns whether a START was dispatched. Callers hold
// s.outsourceMu.
func (s *apiServer) reconcileWorkerLiveness(w OutsourceWorker, now float64) bool {
	st := s.reconcileStateOf(w.ID)
	// The stop anchor belongs to an epoch: a stop sent before this refocus epoch
	// began would otherwise de-dupe (swallow) the new epoch's collect — measured
	// with two handovers in one second.
	if st.LastCommand == reconcileCmdStop && w.RefocusSince > st.LastCommandAt {
		st.LastCommand = reconcileCmdNone
		st.LastCommandAt = 0.0
	}
	obs := workerObservation(w, s.hub.IsOnline(w.ID))
	decision := reconcileDecide(obs, st, s.reconcileConfigLive(), now)
	started := false
	switch decision.Command {
	case reconcileCmdStart:
		s.setReconcileState(w.ID, decision.State)
		delete(s.workerSpawnAt, w.ID)
		started = s.notifyWorkerSpawn(w, now)
		if !started {
			s.setReconcileState(w.ID, st)
		}
	case reconcileCmdStop:
		if decision.StopKind == stopKindRecycle {
			s.setReconcileState(w.ID, decision.State)
			s.collectWorkerHandover(&w, "fsm-recycle", triggerServer, now)
			return false
		}
		target := s.workerSpawnTarget[w.ID]
		if target == "" {
			s.setReconcileState(w.ID, st)
			return false
		}
		s.setReconcileState(w.ID, decision.State)
		s.stopWorkerSessionOrPark([]string{target}, w.ID, now)
		delete(s.workerSpawnAt, w.ID)
		// Bench only on a zombie takeover. Keep this guard even if other stop kinds
		// look unreachable: reconcileStates is shared with staff, and a robust_resend
		// reaching here was measured benching the machine. relocate is masked
		// (workerObservation leaves the machine pair empty); winddown needs desired
		// offline, which the tick never reconciles.
		if decision.StopKind != stopKindZombieTakeover {
			outsourceLog("rescue %s (%s): %s — robust stop → %s, NOT benched "+
				"(stop kind %q is not a zombie takeover)",
				w.ID, w.Codename, decision.Reason, target, decision.StopKind)
			break
		}
		// The bench keeps the next START from reaching the target before the stop
		// reaps the ghost; noteWorkerStopSucceeded lifts it on the target's OK receipt.
		s.benchWorkerMachine(w.ID, target, now)
		if lifted, ok := s.workerTakeoverLiftedAt[w.ID]; ok && now-lifted < workerMachineBenchSecs {
			delete(s.workerTakeoverBench, w.ID)
			outsourceLog("rescue %s (%s): %s — robust stop → %s, %s benched (repeat takeover, full cooldown)",
				w.ID, w.Codename, decision.Reason, target, target)
			break
		}
		s.workerTakeoverBench[w.ID] = takeoverBench{
			Machine: target,
			At:      now,
			Until:   s.workerMachineBench[workerMachineKey(w.ID, target)],
		}
		outsourceLog("rescue %s (%s): %s — robust stop → %s, %s benched until its stop receipt",
			w.ID, w.Codename, decision.Reason, target, target)
	default:
		s.setReconcileState(w.ID, decision.State)
	}
	// Read convergence off the decider; do not re-derive it here.
	if decision.ConvergedOnline {
		s.clearWorkerConvergedFailureReceipt(w.ID, w)
	}
	// Stamped AFTER the switch so a re-START in this decision has already run its
	// success-path clear.
	if decision.StartTimedOut {
		target := s.workerSpawnTarget[w.ID]
		// Tell "never collected" from "collected but did not boot" by THIS worker's
		// frame still sitting in the machine's FIFO — per subject, not queue depth
		// (a shared FIFO's depth wrongly accused healthy wardens). An empty backlog
		// means collected, not delivered: there is no ack on this path.
		switch {
		case target == "":
			s.stampWorkerPlacementBlocked(&w, s.wakeTimeoutReason(spawnReasonWakeTimeout,
				wakeTimeoutTargetForgotten, "", ""), now)
		case s.hub.PendingWardenCommandsFor(target, w.ID) > 0:
			s.stampWorkerPlacementBlocked(&w, s.wakeTimeoutReason(spawnReasonNeverCollected,
				wakeTimeoutStillQueued, target, ""), now)
		case undeliveredWorkerStart(s.hub, w.ID, s.workerSpawnAt[w.ID]):
			// Popped then lost: the stream died mid-drain and ReturnUndeliveredCommands
			// dropped it (only `update` is put back), so the backlog check misses it.
			// Known limitation: a re-START earlier in this tick deletes the evidence
			// (EnqueueWardenCommandFor clears cmdUndelivered) and the receipt falls to
			// `default`. Do not fix by moving the stamp before the switch (that undoes
			// the clear-on-success ordering); the real fix is a multi-slot receipt.
			s.stampWorkerPlacementBlocked(&w, s.wakeTimeoutReason(spawnReasonNeverCollected,
				wakeTimeoutUndelivered, target, ""), now)
		default:
			s.stampWorkerPlacementBlocked(&w, s.wakeTimeoutReason(spawnReasonWakeTimeout,
				wakeTimeoutNeverCameUp, target, w.Runtime), now)
		}
	} else if isStopgapRetryReason(decision.ReasonCode) {
		// Retry-loop codes only: a zombie_suspect stamp would overwrite the
		// session_already_exists receipt the zombie arm reads next tick.
		s.stampWorkerPlacementBlocked(&w, decision.ReasonCode, now)
	}
	return started
}

func undeliveredWorkerStart(h *Hub, workerID string, spawnAt float64) bool {
	if workerID == "" || spawnAt <= 0 {
		return false
	}
	note, lost := h.UndeliveredCommandSince(workerID, spawnAt)
	return lost && note.Verb == reconcileCmdStart
}

// workerObservation masks, on purpose: TargetMachine/RunningMachine (the
// relocation arm — 改機器 is collected by the owner-op refocus epoch; feeding the
// pair would race two collectors) and StoppingSince (decideDown's 加速停止 arm —
// would open a kill path).
func workerObservation(w OutsourceWorker, online bool) memberObservation {
	return memberObservation{
		MemberID: w.ID,

		Desired:      w.DesiredState,
		Online:       online,
		RefocusSince: w.RefocusSince,
		RefocusOp:    w.RefocusOp,
		AgentStopped: w.StoppedSince > 0.0,
		LastOpKind:   canonicalWorkerLastOp(w.LastOp),
		LastOpReason: w.LastOpReason,
	}
}

func canonicalWorkerLastOp(op string) string {
	if op == legacyWardenCmdWorkerStart {
		return reconcileCmdStart
	}
	return op
}

// Callers hold s.outsourceMu; sendStopFrames takes no scheduler lock.
func (s *apiServer) enqueueWorkerStop(target, workerID string) bool {
	if len(s.sendStopFrames(workerID, []string{target}, nowSecs())) == 0 {
		return false
	}
	if s.workerStopPending[workerID] == target {
		delete(s.workerStopPending, workerID)
	}
	return true
}

// stopWorkerSessionOrPark covers both ways a kill can fail (owner ruling: 殘活
// session 零容忍): refused → parked in workerStopPending and re-fired by the
// tick; accepted but the session outlives it → armed in workerStopLanded and
// re-sent past stop_retry (landing on a FIFO is not delivery). Only a single
// named target can be parked; a refused fan-out returns an empty outcome and the
// caller must defer. Callers hold s.outsourceMu.
func (s *apiServer) stopWorkerSessionOrPark(targets []string, workerID string, now float64) workerStopOutcome {
	if len(targets) == 0 {
		return workerStopOutcome{}
	}
	landed := []string{}
	for _, target := range targets {
		if s.enqueueWorkerStop(target, workerID) {
			landed = append(landed, target)
		}
	}
	if len(landed) > 0 {
		aimedAt := ""
		if len(targets) == 1 {
			aimedAt = targets[0]
		}
		s.armWorkerStopRetry(workerID, aimedAt, now)
		return workerStopOutcome{Landed: landed}
	}
	delete(s.workerStopLanded, workerID)
	if len(targets) != 1 {
		outsourceLog("worker_stop %s: every warden in the fan-out refused (%v) — "+
			"nothing sent and nothing parked; the caller must defer", workerID, targets)
		return workerStopOutcome{}
	}
	s.workerStopPending[workerID] = targets[0]
	outsourceLog("worker_stop %s: target %s unreachable — parked, tick will re-fire",
		workerID, targets[0])
	return workerStopOutcome{Parked: targets[0]}
}

type workerStopOutcome struct {
	Landed []string
	Parked string
}

func (o workerStopOutcome) recorded() bool {
	return len(o.Landed) > 0 || o.Parked != ""
}

type takeoverBench struct {
	Machine string
	At      float64
	Until   float64
}

// noteWorkerStopSucceeded lifts the takeover bench on the takeover target's OK
// stop receipt (a kill or no_such_session).
func (s *apiServer) noteWorkerStopSucceeded(workerID, reporter string) {
	if workerID == "" || reporter == "" {
		return
	}
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	tb, ok := s.workerTakeoverBench[workerID]
	if !ok || tb.Machine != reporter {
		return
	}
	delete(s.workerTakeoverBench, workerID)
	key := workerMachineKey(workerID, reporter)
	if until, benched := s.workerMachineBench[key]; !benched || until != tb.Until {
		return
	}
	delete(s.workerMachineBench, key)
	s.workerTakeoverLiftedAt[workerID] = tb.At
	outsourceLog("worker_stop %s: %s confirmed the takeover stop — bench lifted, "+
		"restart proceeds on the same machine", workerID, reporter)
}

type workerStopDispatch struct {
	// Target is "" for a broadcast. A fan-out deliberately records no machine
	// list: nothing reads it (the re-send re-resolves the chain).
	Target string

	At float64
}

func (d workerStopDispatch) aimed() bool { return d.Target != "" }

// Callers hold s.outsourceMu.
func (s *apiServer) armWorkerStopRetry(workerID, aimedAt string, now float64) {
	delete(s.workerStopPending, workerID)
	s.workerStopLanded[workerID] = workerStopDispatch{Target: aimedAt, At: now}
}

// noteWorkerStopNoSuchSession disarms the retry on a no_such_session receipt
// FROM THE AIMED TARGET: it proves the kill arrived and found nothing, which
// presence cannot (a stale SSE claim can keep the id looking alive). The machine
// match is load-bearing: an identity sweep broadcasts stop to every warden and
// uninvolved ones answer no_such_session routinely. reporter "" means unknown
// (receiptReporterMachine). The receipt path holds no scheduler lock.
func (s *apiServer) noteWorkerStopNoSuchSession(workerID, reporter string) {
	if workerID == "" || reporter == "" {
		return
	}
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	armed, ok := s.workerStopLanded[workerID]
	if !ok || armed.Target != reporter {
		return
	}
	delete(s.workerStopLanded, workerID)
	outsourceLog("worker_stop %s: %s reported no_such_session — the kill reached the "+
		"machine it was aimed at and found nothing to kill; retry disarmed",
		workerID, reporter)
}

// Callers hold s.outsourceMu.
func (s *apiServer) retryPendingWorkerStop(workerID string, now float64) {
	target := s.workerStopPending[workerID]
	if target == "" {
		s.retryUnlandedWorkerStop(workerID, now)
		return
	}
	if s.enqueueWorkerStop(target, workerID) {
		s.armWorkerStopRetry(workerID, target, now)
		outsourceLog("worker_stop %s: parked kill re-fired → %s", workerID, target)
	}
}

// retryUnlandedWorkerStop re-sends an accepted kill whose session is still
// running past stop_retry. An aimed kill counts "still running" only on the aimed
// machine: hub.IsOnline keys on the worker id, which is online again elsewhere
// after a restart or 改機器. Callers hold s.outsourceMu.
func (s *apiServer) retryUnlandedWorkerStop(workerID string, now float64) {
	armed, ok := s.workerStopLanded[workerID]
	if !ok {
		return
	}
	alive := s.hub.IsOnline(workerID)
	if armed.aimed() {
		alive = alive && s.hub.MachineOf(workerID) == armed.Target
	}
	switch robustStopRetryStep(armed.At, alive, s.reconcileConfigLive().StopRetry, now) {
	case robustStopDone:
		delete(s.workerStopLanded, workerID)
	case robustStopResend:
		outsourceLog("worker_stop %s: session still live past stop_retry (aimed at %q) — "+
			"the kill did not take, re-dispatching", workerID, armed.Target)
		s.stopWorkerSessionOrPark(s.resendKillTargets(workerID, armed), workerID, now)
	}
}

// Callers hold s.outsourceMu.
func (s *apiServer) resendKillTargets(workerID string, armed workerStopDispatch) []string {
	if armed.aimed() {
		return []string{armed.Target}
	}
	last := ""
	if w, err := s.dal.GetOutsourceWorker(workerID); err == nil && w != nil {
		last = w.LastMachineID
	}
	return s.workerKillTargets(workerID, last)
}

// relocateWorkerNow moves a worker to its freshly pinned desired_machine_id
// without touching lifecycle status. Order is the staff one: STOP (kill chain;
// an unreachable named machine parks the kill) → no START while the worker reads
// online → once offline, the shared FSM STARTs it on the pin. A worker with state
// to flush gets a wind-down first. The old machine is NOT benched (owner-chosen;
// he may relocate back). Must end in a dispatch or a receipt.
// Callers hold s.outsourceMu.
func (s *apiServer) relocateWorkerNow(w OutsourceWorker) ownerOpOutcome {
	return s.respawnWorkerForOwnerOp(w, ownerOpRelocate)
}

// respawnWorkerForOwnerOp is the one path for owner verbs that should leave the
// worker running (改機器, 喚醒, runtime/model). desired_state=offline dominates
// every one: nothing starts and the row says so. 喚醒 never reaches that arm —
// its handler sets DesiredState online on the row it passes by value.
// Callers hold s.outsourceMu.
func (s *apiServer) respawnWorkerForOwnerOp(w OutsourceWorker, op string) ownerOpOutcome {
	if w.DesiredState == DesiredStateOffline {
		// Queue the verb behind the stop (owner rc-bc1b029a3aa2); nothing dispatches.
		// 換 model reaches here only while the session is up — a converged stop
		// queues through its own branch in api_outsource.go; both are needed.
		now := nowSecs()
		fresh, queued, err := s.queueWorkerRestartAfterStopOnRow(w.ID, op, now)
		if err != nil {
			outsourceLog("spawn %s: queued restart-after-stop persist failed: %v", w.ID, err)
			return ownerOpOutcome{HeldDown: true}
		}
		if queued {
			s.publishOutsourceWorker(*fresh, triggerServer)
			return ownerOpOutcome{HeldDown: true}
		}
		if fresh == nil || fresh.DesiredState != DesiredStateOffline {
			return ownerOpOutcome{HeldDown: true}
		}
		s.stampWorkerPlacementBlocked(&w, spawnReasonHeldDown+": the "+op+" was saved, "+
			"but nothing was started — this worker is stopped; 喚醒 it when you want it "+
			"to run", now)
		return ownerOpOutcome{HeldDown: true}
	}
	// Every owner verb gets a wind-down chance (owner: 「我建議所有換手都可以給他機會收尾」).
	// 「正在跑就不動它」 for 喚醒 is enforced by api_outsource.go (!sessionAliveReceipt)
	// before this call; nothing in this function catches a weakened gate.
	if s.workerHasStateToFlush(w) {
		// A ladder refusal (openOwnerOpHandover false) still answers WoundDown: a
		// higher wind-down is open, and falling through would kill a session mid 加速停止.
		s.openOwnerOpHandover(w, op)
		return ownerOpOutcome{WoundDown: true}
	}
	return s.handOverWorkerNow(w, op)
}

// ownerOpOutcome is which thing an owner verb did; exactly one field is true.
// The HTTP faces need it the way staff use relocation_pending / activation_pending.
type ownerOpOutcome struct {
	Dispatched bool
	// WoundDown: deferred by design, not a failure (wind-down open, or STOP sent
	// and START awaiting offline).
	WoundDown bool

	HeldDown bool
	// AlreadyRunning: the session is already up; nothing to land, so NOT pending.
	AlreadyRunning bool
}

// Pending means "scheduled, not yet landed", as staff relocation_pending /
// activation_pending. Not !Dispatched: AlreadyRunning is finished.
func (o ownerOpOutcome) Pending() bool { return !o.Dispatched && !o.AlreadyRunning }

const (
	ownerOpRelocate     = "relocate"
	ownerOpRestart      = "restart"
	ownerOpRuntimeModel = "runtime/model" // 換 model / runtime / effort
)

// workerHasStateToFlush: the answer is shared with staff
// (hasUncollectedOnlineOwnerOpState) but deliberately carries no desired-offline
// arm — respawnWorkerForOwnerOp's held_down gate returns first. Adding one to
// "match" staff makes that gate dead code; merging the shells was measured to
// close the whole worker wind-down window. Callers hold s.outsourceMu.
func (s *apiServer) workerHasStateToFlush(w OutsourceWorker) bool {
	return hasUncollectedOnlineOwnerOpState(
		w.RefocusSince, w.StoppedSince, s.hub.IsOnline(w.ID))
}

// openOwnerOpHandover opens a graceful wind-down for an owner verb: stamp a fresh
// refocus epoch via armRefocusEpoch (never by hand — its ladder 下線 → 加速 → 強制
// must not move backwards, or a worker in 加速停止 loses its deadline) and fan
// the SOP 預告. NO kill goes out here. relocate and runtime/model run no clock
// (recycleGraceFor), so the 收口 is only report_stopped or the owner's 加速停止 —
// there is no 120 s ceiling. A persist fault falls back to the immediate path.
// Returns false when the ladder refuses; not a failure — the change is already on
// the row. Callers hold s.outsourceMu.
//
// The epoch is decided on the row as it is inside the transaction, not on the
// caller's copy, and only the four anchor columns are written: a stop or
// release that landed after the caller read the row stands, and no other
// column goes back to the caller's copy. The copy still carries the rest of w
// onward — a runtime/model change reaches here before its new values are
// stored, and a start that follows must use them.
func (s *apiServer) openOwnerOpHandover(w OutsourceWorker, op string) bool {
	armed := false
	var fresh OutsourceWorker
	var proj Member
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getOutsourceWorkerOn(tx, w.ID)
		if err != nil || cur == nil {
			return err
		}
		fresh = *cur
		if cur.Status == WorkerStatusReleased || cur.DesiredState == DesiredStateOffline {
			return nil
		}
		proj = memberFromWorker(*cur)
		if !armRefocusEpoch(&proj, op, nowSecs()) {
			return nil
		}
		armed = true
		return setMemberWindDownAnchorsOn(tx, cur.ID, proj.StoppingSince, proj.StoppedSince,
			proj.RefocusSince, proj.RefocusOp)
	})
	if err != nil {
		outsourceLog("%s %s (%s): refocus stamp failed (%v) — falling back to an "+
			"immediate handover so the owner's action is not lost", op, w.ID, w.Codename, err)
		s.handOverWorkerNow(w, op)
		return true
	}
	if !armed {
		outsourceLog("%s %s (%s): wind-down NOT re-opened — this worker is stopped, "+
			"released, or already further along the ladder (下線 → 加速 → 強制) at %q; "+
			"the change is saved and what is open keeps its own deadline",
			op, w.ID, w.Codename, fresh.RefocusOp)
		return false
	}
	w.RefocusSince = proj.RefocusSince
	w.RefocusOp = proj.RefocusOp
	w.StoppingSince = proj.StoppingSince
	w.StoppedSince = proj.StoppedSince
	s.openWorkerHandoverGrace(w, triggerServer)
	if grace, clocked := recycleGraceFor(op, s.reconcileConfigLive()); clocked {
		outsourceLog("%s %s (%s): wind-down opened — collect on stopped-report or +%.0fs",
			op, w.ID, w.Codename, grace)
	} else {
		outsourceLog("%s %s (%s): wind-down opened — collect on stopped-report ONLY "+
			"(this op runs no clock)", op, w.ID, w.Codename)
	}
	return true
}

// persistWorkerRowOn is persistMemberRowOn for a worker row (PutOutsourceWorker's
// write: no ValidateMember).
func persistWorkerRowOn(tx *writeTx, w OutsourceWorker) error {
	if err := setMemberWindDownAnchorsOn(tx, w.ID, w.StoppingSince, w.StoppedSince,
		w.RefocusSince, w.RefocusOp); err != nil {
		return err
	}
	return putMemberOn(tx, memberFromWorker(w))
}

// Callers hold s.outsourceMu.
func (s *apiServer) handOverWorkerNow(w OutsourceWorker, op string) ownerOpOutcome {
	now := nowSecs()
	s.stopWorkerSessionForHandover(w, op, now)
	return s.reconcileWorkerNow(w, now)
}

// Callers hold s.outsourceMu.
func (s *apiServer) resolveWorkerKillTarget(workerID, lastMachine string) string {
	return s.namedKillTarget(workerID, killTargetSources{
		SpawnTarget:   s.workerSpawnTarget[workerID],
		LastMachineID: lastMachine,
		Outsource:     true,
	})
}

// Callers hold s.outsourceMu.
func (s *apiServer) workerKillTargets(workerID, lastMachine string) []string {
	targets, _ := s.killTargetChain(workerID, killTargetSources{
		SpawnTarget:   s.workerSpawnTarget[workerID],
		LastMachineID: lastMachine,
		Outsource:     true,
	})
	return targets
}

// observedWorkerHost is the cockpit machine cell's fallback when in-memory spawn
// observation is empty (after a re-exec). Read-only — never feeds a kill/sweep
// decision.
func (s *apiServer) observedWorkerHost(workerID string, tele map[string]any) string {
	if host := s.hub.MachineOf(workerID); host != "" {
		return host
	}
	if m, _ := tele["machine"].(string); m != "" {
		return m
	}
	return ""
}

// Callers hold s.outsourceMu.
func (s *apiServer) stopWorkerSessionForHandover(w OutsourceWorker, reason string, now float64) bool {
	targets := s.workerKillTargets(w.ID, w.LastMachineID)
	out := s.stopWorkerSessionOrPark(targets, w.ID, now)
	if !out.recorded() && w.Status == WorkerStatusActive {
		outsourceLog("%s deferred %s (%s): the kill is on no warden's FIFO and parked "+
			"nowhere (targets %v); nothing stopped — tick retries",
			reason, w.ID, w.Codename, targets)
		s.stampWorkerPlacementBlocked(&w, spawnReasonRespawnDeferred+": the "+reason+
			" could not clear this worker's previous session — it is marked active, but "+
			"the stop reached no machine: either nothing the server knows names one "+
			"(its spawn memory, a live connection, the worker's last landing) and no "+
			"warden is online, or every warden it was aimed at refused it; retrying", now)
		return false
	}
	outsourceLog("handover %s (%s): reason=%s — stopping session on %v (task %s); "+
		"the replacement starts once the worker reads offline",
		w.ID, w.Codename, reason, targets, w.TaskID)
	s.bankLiveCost(w.ID)
	s.clearSessionBootTS(w.ID)
	delete(s.workerSpawnAt, w.ID)
	st := s.reconcileStateOf(w.ID)
	st.Phase = reconcilePhaseStopping
	st.LastCommand = reconcileCmdStop
	st.LastCommandAt = now
	s.setReconcileState(w.ID, st)
	return true
}

// reconcileWorkerNow uses the caller's w, which may carry intent not yet on the
// row. Callers hold s.outsourceMu.
func (s *apiServer) reconcileWorkerNow(w OutsourceWorker, now float64) ownerOpOutcome {
	if w.Status == WorkerStatusReleased || w.DesiredState == DesiredStateOffline {
		return ownerOpOutcome{}
	}
	if s.reconcileWorkerLiveness(w, now) {
		return ownerOpOutcome{Dispatched: true}
	}
	if s.hub.IsOnline(w.ID) {
		return ownerOpOutcome{WoundDown: true}
	}
	return ownerOpOutcome{}
}

// stopWorkerNow is the owner's 停止: the caller has already set
// desired_state=offline, so this only kills. Callers hold s.outsourceMu.
func (s *apiServer) stopWorkerNow(w OutsourceWorker) {
	targets := s.workerKillTargets(w.ID, w.LastMachineID)
	s.bankLiveCost(w.ID)
	out := s.stopWorkerSessionOrPark(targets, w.ID, nowSecs())
	if !out.recorded() {
		outsourceLog("stop %s (%s): the kill is on no warden's FIFO and parked "+
			"nowhere (targets %v) — held down anyway", w.ID, w.Codename, targets)
		// Returning here keeps boot_ts: no kill is on any FIFO, so no session
		// ended. (dispatchShutdown uses a weaker test and still clears it.)
		delete(s.workerSpawnAt, w.ID)
		return
	}
	s.clearSessionBootTS(w.ID)
	delete(s.workerSpawnAt, w.ID)
	outsourceLog("stop %s (%s): session killed on %v, held down (no re-spawn)",
		w.ID, w.Codename, targets)
}

// autoHandoverWorker drives only the 停止 epoch (desired_state=offline): the
// shared FSM never sees a desired-offline worker, so this is that intent's only
// driver. Handover collection and context thresholds live in the shared FSM and
// stampContextHighRecycle. Owner ruling rc-10cc6f9b2572: a refocus epoch closes
// only when the replacement calls report_waking. Callers hold s.outsourceMu.
func (s *apiServer) autoHandoverWorker(w OutsourceWorker, now float64) {
	// A 停止 is normally collected by the worker's report_stopped. Here: the
	// session is confirmed gone, or the owner's 加速停止 deadline passed. A plain
	// 停止 has no clock and waits indefinitely (owner ruling rc-27d1710174dd).
	if w.DesiredState == DesiredStateOffline {
		sessionGone := s.sessionConfirmedGone(w.ID, now)
		if stopAwaitsCollect(memberFromWorker(w)) {
			if sessionGone {
				s.collectWorkerStop(w, "stop-session-gone", triggerServer)
			} else if grace, clocked := recycleGraceFor(
				w.RefocusOp, s.reconcileConfigLive()); clocked &&
				now >= w.StoppingSince+grace {
				s.collectWorkerStop(w, "stop-accelerated-deadline", triggerServer)
			}
		}
		return
	}
}

// openWorkerHandoverGrace fans the member-topic 預告 at the worker's own session
// (its ocagent recycleHook refetches GET /api/members/<self> and prints the
// handover wake) and returns; the kill belongs to the FSM's recycle arm. An
// offline worker skips the window and is collected at once. This entry sample is
// deliberately NOT de-bounced (unlike autoHandoverWorker): it runs once at the
// owner's press, same as the staff twin. Callers hold s.outsourceMu and have
// already persisted the refocus stamp.
func (s *apiServer) openWorkerHandoverGrace(w OutsourceWorker, trigger string) {
	if !s.hub.IsOnline(w.ID) {
		if w.DesiredState == DesiredStateOffline {
			s.collectWorkerStop(w, "stop-offline", trigger)
			return
		}
		now := nowSecs()
		s.collectWorkerHandover(&w, "handover-offline", trigger, now)
		s.reconcileWorkerNow(w, now)
		return
	}
	s.hub.Publish("member", "patch", "member", wireOwnerID+"::"+w.ID,
		s.offboardDeltaPayload(memberFromWorker(w)), audienceMembers(w.ID), trigger)
	if grace, clocked := recycleGraceFor(w.RefocusOp, s.reconcileConfigLive()); clocked {
		outsourceLog("handover %s (%s): grace opened — SOP nudge fanned, collect on "+
			"stopped-report or +%.0fs", w.ID, w.Codename, grace)
	} else {
		outsourceLog("handover %s (%s): grace opened — SOP nudge fanned, collect on "+
			"stopped-report ONLY (%s runs no clock)", w.ID, w.Codename, w.RefocusOp)
	}
}

// collectWorkerHandover latches stopped_since (the durable dump-done marker every
// collect driver keys its once-only check on) then stops the session; a deferred
// stop rolls the latch back. Callers hold s.outsourceMu and pass a freshly read
// row with refocus_since>0.
func (s *apiServer) collectWorkerHandover(w *OutsourceWorker, reason, trigger string, now float64) bool {
	_, prior := collectWindDownRow(windDownAnchorRowOfWorker(w), now)
	stopped, _ := s.stopCollectedWorkerForHandover(w, prior, reason, trigger, now)
	return stopped
}

// Callers hold s.outsourceMu.
func (s *apiServer) stopCollectedWorkerForHandover(w *OutsourceWorker, prior float64, reason, trigger string, now float64) (bool, error) {
	if err := s.latchWorkerStopped(w, prior, reason, trigger); err != nil {
		return false, err
	}
	if !s.stopWorkerSessionForHandover(*w, reason, now) {
		s.restoreWorkerStoppedLatch(w, prior, reason)
		return false, nil
	}
	return true, nil
}

// latchWorkerStopped writes stopped_since — the one column the collect
// changed — and nothing else from the caller's copy, which is usually the
// tick's list read: every other column, the other three anchors included, stays
// as the row has it. On failure the in-memory latch is put back to prior.
// Callers hold s.outsourceMu.
func (s *apiServer) latchWorkerStopped(w *OutsourceWorker, prior float64, reason, trigger string) error {
	var fresh *OutsourceWorker
	if err := s.dal.inTx(func(tx *writeTx) error {
		if err := setMemberStoppedSinceOn(tx, w.ID, w.StoppedSince); err != nil {
			return err
		}
		var err error
		if fresh, err = getOutsourceWorkerOn(tx, w.ID); err == nil && fresh == nil {
			err = errNotFound
		}
		return err
	}); err != nil {
		outsourceLog("collect %s (%s): stopped latch failed, nothing landed — the retry "+
			"is a first report again: %v", w.ID, reason, err)
		w.StoppedSince = prior
		return err
	}
	s.publishMemberPatch(memberFromWorker(*fresh), trigger)
	return nil
}

// restoreWorkerStoppedLatch writes the latch column only: the stopped-report
// path drops outsourceMu across the kill, so any wider write would revert
// concurrent changes. Callers hold s.outsourceMu.
func (s *apiServer) restoreWorkerStoppedLatch(w *OutsourceWorker, prior float64, reason string) {
	w.StoppedSince = prior
	if err := setMemberStoppedSinceOn(s.dal.wdb, w.ID, prior); err != nil {
		outsourceLog("collect %s (%s): latch-rollback ANCHOR write failed: %v",
			w.ID, reason, err)
	}
}

// collectWorkerStop is the 收口 of a 停止 epoch: kill via stopWorkerNow, never the
// handover funnel, which would let the FSM restart a worker the owner stopped.
// Callers hold s.outsourceMu.
func (s *apiServer) collectWorkerStop(w OutsourceWorker, reason, trigger string) error {
	_, prior := collectWindDownRow(windDownAnchorRowOfWorker(&w), nowSecs())
	if err := s.latchWorkerStopped(&w, prior, reason, trigger); err != nil {
		return err
	}
	s.stopWorkerNow(w)
	outsourceLog("stop collect %s (%s): close-out collected — session killed, held down",
		w.ID, reason)
	return nil
}

// Callers hold s.outsourceMu.
func (s *apiServer) resolveLiveWorker(id string) (*OutsourceWorker, error) {
	return resolveLiveWorkerOn(s.dal.rdb, id)
}

func resolveLiveWorkerOn(q sqlRowQuerier, id string) (*OutsourceWorker, error) {
	w, err := getOutsourceWorkerOn(q, id)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Status == WorkerStatusReleased {
		return nil, errNotFound
	}
	return w, nil
}

// workerReportWaking: waking_since is deliberately NOT stamped here (it is
// stamped at dispatch, the staff rule). This is the only assigned → active write
// point; Status is a projection of activated_ts, so putMember persists the flip.
// stampFloor, when set, raises the caller's credential floor in the same
// transaction: the floor lands with the wake or not at all
// (HandleReportWakingApiSelfWakingPost).
func (s *apiServer) workerReportWaking(id string, model *string, trigger string, stampFloor func(sqlExecer) error) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	var m Member
	err := s.dal.inTx(func(tx *writeTx) error {
		if stampFloor != nil {
			if err := stampFloor(tx); err != nil {
				return err
			}
		}
		w, err := resolveLiveWorkerOn(tx, id)
		if err != nil {
			return err
		}
		if w.Status == WorkerStatusAssigned {
			w.Status = WorkerStatusActive
		}
		clearWindDownRowOnWake(windDownAnchorRowOfWorker(w), w.DesiredState)
		m = memberFromWorker(*w)
		if model != nil {
			m.ActualModel = *model
		}
		return persistMemberRowOn(tx, m)
	})
	if err != nil {
		return nil, err
	}
	s.publishMemberPatch(m, trigger)
	return &m, nil
}

// workerReportStopping stamps stopping_since if unset; the server never kills on
// it (member parity).
func (s *apiServer) workerReportStopping(id, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	var m Member
	err := s.dal.inTx(func(tx *writeTx) error {
		w, err := resolveLiveWorkerOn(tx, id)
		if err != nil {
			return err
		}
		openWindDownRow(windDownAnchorRowOfWorker(w), nowSecs())
		m = memberFromWorker(*w)
		return persistMemberRowOn(tx, m)
	})
	if err != nil {
		return nil, err
	}
	s.publishMemberPatch(m, trigger)
	return &m, nil
}

func (s *apiServer) workerReportStopped(id, trigger string) (*Member, string, error) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	now := nowSecs()
	var w *OutsourceWorker
	collect, stopEffect, prior := false, "", 0.0
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if w, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		collect, stopEffect, prior = decideStoppedReport(windDownAnchorRowOfWorker(w), now)
		if !collect {
			return nil
		}
		// The latch lands with the row or not at all: a latch left behind a failed
		// write would make every retry read "already reported" and send no kill.
		return persistMemberRowOn(tx, memberFromWorker(*w))
	})
	if err != nil {
		unlockMu()
		outsourceLog("collect %s (stopped-report): stopped latch failed, nothing landed: %v", id, err)
		return nil, "", err
	}
	if !collect {
		unlockMu()
		m := memberFromWorker(*w)
		return &m, stopEffect, nil
	}
	s.publishMemberPatch(memberFromWorker(*w), trigger)
	s.bankLiveCost(w.ID)
	row := *w
	unlockMu()

	// Drop the lock BEFORE the kill (owner ruling T-14): dispatchShutdown re-takes
	// outsourceMu (self-deadlock) and, on the staff arm, reconcileMu.
	out := s.dispatchShutdown(id, "stopped-report")

	fresh := s.concludeWorkerStoppedReport(id, prior, out, now)
	if fresh == nil {
		m := memberFromWorker(row)
		return &m, stopEffect, nil
	}
	m := memberFromWorker(*fresh)
	return &m, stopEffect, nil
}

// concludeWorkerStoppedReport takes an id, not a row: outsourceMu was open across
// the kill, so the caller's row is stale and must not be decided on or written
// back. Takes s.outsourceMu itself.
func (s *apiServer) concludeWorkerStoppedReport(
	id string, prior float64, out shutdownDispatch, now float64,
) *OutsourceWorker {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	fresh, err := s.resolveLiveWorker(id)
	if err != nil {
		return nil
	}
	rec := s.noteWorkerShutdownDispatched(*fresh, out, now)
	// recorded(), not out.Addressed: a fan-out every warden refused is addressed
	// but recorded nowhere.
	if rec.recorded() || fresh.Status != WorkerStatusActive {
		return s.rereadWorker(id, fresh)
	}
	// Only a worker coming back is deferred. A held-down worker keeps its collect:
	// rolling back would make the row read 「retrying」 while the wire said 「collected」.
	if fresh.DesiredState == DesiredStateOffline {
		outsourceLog("stop collect %s (%s): the kill is on no warden's FIFO and parked "+
			"nowhere — kill skipped, held down", fresh.ID, fresh.Codename)
		return s.rereadWorker(id, fresh)
	}
	s.stampWorkerPlacementBlocked(fresh, spawnReasonRespawnDeferred+": the "+
		"stopped-report could not clear this worker's previous session — it is "+
		"marked active but neither the server's spawn memory, a live connection, "+
		"its last landing nor any online warden knows which machine it is on; "+
		"retrying", now)
	s.restoreWorkerStoppedLatch(fresh, prior, "stopped-report")
	return s.rereadWorker(id, fresh)
}

// Callers hold s.outsourceMu.
func (s *apiServer) rereadWorker(id string, fallback *OutsourceWorker) *OutsourceWorker {
	if reread, err := s.resolveLiveWorker(id); err == nil {
		return reread
	}
	return fallback
}

// noteWorkerShutdownDispatched records the kill in the worker's retry ledger
// (staff use RobustStopPendingAt, armed inside dispatchShutdown). Callers hold
// s.outsourceMu.
func (s *apiServer) noteWorkerShutdownDispatched(
	w OutsourceWorker, out shutdownDispatch, now float64,
) workerStopOutcome {
	rec := workerStopOutcome{}
	switch {
	case out.Sent:
		// out.Target is "" for a fan-out.
		s.armWorkerStopRetry(w.ID, out.Target, now)
		rec.Landed = out.Landed
	case out.Target != "":
		delete(s.workerStopLanded, w.ID)
		s.workerStopPending[w.ID] = out.Target
		rec.Parked = out.Target
		outsourceLog("worker_stop %s: target %s unreachable — parked, tick will re-fire",
			w.ID, out.Target)
	}
	if w.DesiredState == DesiredStateOffline {
		return rec
	}
	st := s.reconcileStateOf(w.ID)
	st.Phase = reconcilePhaseStopping
	st.LastCommand = reconcileCmdStop
	st.LastCommandAt = now
	s.setReconcileState(w.ID, st)
	return rec
}

// workerRestartSelf: the caller's online/min-liveness gates have already passed.
// The ladder guard lives here because HandleRestartSelfApiSelfRefocusPost returns
// early for outsource callers, before its staff armRefocusEpoch check.
func (s *apiServer) workerRestartSelf(id string, now float64, trigger string) (*Member, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	var w *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if w, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		proj := memberFromWorker(*w)
		if !aRefocusStampWouldReachTheAgent(proj) {
			return refuseInTx(http.StatusConflict, restartSelfNeedsALiveSessionMsg)
		}
		if !armRefocusEpoch(&proj, refocusOpRestartSelf, now) {
			return errWindDownLadderBackwards
		}
		w.RefocusSince = proj.RefocusSince
		w.RefocusOp = proj.RefocusOp
		w.StoppingSince = proj.StoppingSince
		w.StoppedSince = proj.StoppedSince
		return persistWorkerRowOn(tx, *w)
	})
	if err != nil {
		return nil, err
	}
	s.openWorkerHandoverGrace(*w, trigger)
	m := memberFromWorker(*w)
	return &m, nil
}

// Callers hold s.outsourceMu.
func (s *apiServer) reclaimWorkerSession(w OutsourceWorker) {
	// Unlike other kill sites (first NAMED target, park if dark), reclaim takes the
	// first REACHABLE one: a released worker has no future to park a kill for.
	targets := s.reclaimKillTargets(w)
	if len(targets) == 0 {
		outsourceLog("reclaim %s (%s): no online warden — will retry", w.ID, w.Codename)
		return
	}
	enqueued := false
	for _, warden := range targets {
		if s.enqueueWorkerStop(warden, w.ID) {
			enqueued = true
		}
	}
	if !enqueued {
		return
	}
	s.workerReclaimed[w.ID] = true
	s.dropReconcileState(w.ID)
	outsourceLog("reclaim %s (%s) dispatched → warden(s) %s",
		w.ID, w.Codename, strings.Join(targets, ","))
}

// reclaimKillTargets deliberately ignores last_machine_id (history, not an
// observation): with no live claim or spawn memory the residual could be
// anywhere, so it fans out (owner ruling: 殘活 session 零容忍). Callers hold
// s.outsourceMu.
func (s *apiServer) reclaimKillTargets(w OutsourceWorker) []string {
	if t := s.reachableKillTarget(w.ID, killTargetSources{
		SpawnTarget: s.workerSpawnTarget[w.ID],
		Outsource:   true,
	}); t != "" {
		return []string{t}
	}
	return s.onlineWardens()
}

// dismissOutsourceWorkersForTask takes outsourceMu itself — call it WITHOUT the
// scheduler lock. It returns the fired ids instead of sweeping their cards: card
// writes make outward calls that must not run under outsourceMu (closeTask
// sweeps them unlocked; dismissOutsourceWorkerByID sweeping inside the lock is
// debt, not the shape to copy).
func (s *apiServer) dismissOutsourceWorkersForTask(taskID string, now float64, trigger string) []string {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	released, err := s.dal.ReleaseWorkersForTask(taskID, now)
	if err != nil {
		outsourceLog("dismiss task %s: release failed: %v", taskID, err)
		return nil
	}
	fired := []string{}
	for _, w := range released {
		fired = append(fired, w.ID)
		s.publishOutsourceWorker(w, trigger)
	}
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		outsourceLog("dismiss task %s: worker read failed: %v", taskID, err)
		return fired
	}
	for _, w := range workers {
		if w.TaskID == taskID && !s.workerReclaimed[w.ID] {
			s.reclaimWorkerSession(w)
		}
	}
	return fired
}

// dismissOutsourceWorkerByID fires one worker by WORKER ID, never task_id: an
// outsource→outsource takeover already bound the successor to the same task_id.
// Takes outsourceMu itself — call it WITHOUT the scheduler lock held.
func (s *apiServer) dismissOutsourceWorkerByID(workerID string, now float64, trigger string) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	released, err := s.dal.ReleaseWorkerByID(workerID, now)
	if err != nil {
		outsourceLog("dismiss worker %s: release failed: %v", workerID, err)
		return
	}
	if released != nil {
		s.publishOutsourceWorker(*released, trigger)
	}
	if !s.workerReclaimed[workerID] {
		if w, err := s.dal.GetOutsourceWorker(workerID); err == nil && w != nil {
			s.reclaimWorkerSession(*w)
		}
	}
	if _, err := s.expireWaitingCardsByAuthor(workerID, now, trigger); err != nil {
		outsourceLog("dismiss worker %s: card sweep failed: %v", workerID, err)
	}
}
