package main

// api_outsource.go — outsource-specific projection overlays and operations.
// The panel lists every not-yet-released worker with its one bound task; a task
// hitting a terminal state releases its worker (api_tasks.go closeTask) and the
// row drops off here — the DB row itself is the audit trail. Chat with a worker
// rides the existing chat surface (the worker id is just a chat peer); worker
// minting/assignment is the Phase 2 scheduler's.

import (
	"net/http"
	"strings"
)

func (s *apiServer) projectWorker(
	worker OutsourceWorker, task *Task, unread int, now float64,
	tele, gauge map[string]map[string]any, machines machineDirectory,
	accountDisplay func(string) string, typeNames map[string]string, calls modelCallBoard,
) memberDTO {
	// Display-only: the identity-sweep 正身 check keeps reading workerSpawnObs, so
	// no kill decision widens.
	machineObserved := s.workerObservedMachine(worker.ID, tele[worker.ID])
	loginWarnings := s.runtimeLoginWarnings(machines,
		workerLoginPairs(machines, worker, machineObserved))
	return s.newOutsourceMemberDTO(worker, task, outsourceWorkerProjection{
		cfg:         s.reconcileConfigLive(),
		unread:      unread,
		now:         now,
		online:      s.hub.IsOnline(worker.ID),
		tele:        tele[worker.ID],
		gaugeEntry:  gauge[worker.ID],
		spawnTarget: machineObserved,
		machineDisplay: func(id string) string {
			if name := machines.aliases[id]; name != "" {
				return name
			}
			return id
		},
		accountDisplay: accountDisplay,
		delegatedBy:    s.workerDelegatedName(task),
		typeDisplay:    func(key string) string { return typeNames[key] },
		// Unconditional — no online / desired-state gate; see terminal_attach.go
		// for why the empty string had to stay free.
		terminalAttach:       terminalAttachCommand(s.namespace, worker.ID),
		loginWarnings:        loginWarnings,
		modelCallWarnings:    calls.warnings(memberFromWorker(worker), loginWarnings),
		modelCallLastSuccess: calls.lastSuccess(worker.ID),
	})
}

// A manual with a blank display_name is omitted so the client's raw-key
// fallback still applies.
func (s *apiServer) taskTypeDisplayNames() map[string]string {
	out := map[string]string{}
	manuals, err := s.foldTaskManuals()
	if err != nil {
		return out
	}
	for _, m := range manuals {
		if m.DisplayName != "" {
			out[m.TypeKey] = m.DisplayName
		}
	}
	return out
}

// "" (owner, no creator, unknown member) makes the client render the owner
// label or a creator_id fallback on the 委託人 line — never a fabricated name.
func (s *apiServer) workerDelegatedName(task *Task) string {
	if task == nil || task.CreatorID == "" || task.CreatorID == wireOwnerID {
		return ""
	}
	if m, err := s.dal.GetMember(task.CreatorID); err == nil && m != nil {
		return m.Name
	}
	return ""
}

func (s *apiServer) HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(w http.ResponseWriter, r *http.Request, id string) {
	worker, err := s.dal.GetOutsourceWorker(id)
	if err != nil {
		internalError(w, err)
		return
	}
	if worker == nil {
		// "member", not "outsource worker": an ow- row IS a member row (00025).
		writeResolveError(w, errNotFound, "member", id)
		return
	}
	task, err := s.dal.GetTask(worker.TaskID)
	if err != nil {
		internalError(w, err)
		return
	}
	if task == nil {
		writeResolveError(w, errNotFound, "task", worker.TaskID)
		return
	}
	context, err := s.buildWorkerBootContext(*worker)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkerBootContextDTO{Context: context})
}

// Outsource arm of POST /api/members/{member_id}/relocate (the member handler
// branches here on kind==outsource; the worker-namespaced route is retired).
// It does NOT kill the session here: a live worker with anything to flush keeps
// running on the old machine until its own report_stopped (or a force-stop), and
// the START onto the new pin follows once it reads offline; a worker with nothing
// to flush is handed over at once. machine_id is required (owner 2026-07-27).
func (s *apiServer) HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(w http.ResponseWriter, r *http.Request, id string) {
	var body MemberRelocateDTO
	if !decodeJSONBodyRequired(w, r, &body, "machine_id") {
		return
	}
	if body.MachineId == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	s.relocateWorkerByID(w, r, id, body.MachineId)
}

func (s *apiServer) relocateWorkerByID(w http.ResponseWriter, r *http.Request, id, machineID string) {
	// "auto" is not exempt: it pins the worker to a pseudo-machine dispatch can
	// never reach. "" no longer clears the pin (owner 2026-07-27); both callers
	// refuse it first, this keeps the core fail-closed on its own.
	if machineID == "" {
		writeError(w, http.StatusBadRequest, relocateNeedsMachineMsg)
		return
	}
	if _, err := s.resolveMachine(machineID); err != nil {
		writeResolveError(w, err, "machine", machineID)
		return
	}

	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	var worker *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		if _, err := resolveMachineOn(tx, machineID); err != nil {
			return machineResolveRefusal(err, machineID)
		}
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		worker.DesiredMachineID = machineID
		// PutOutsourceWorker is PutMember, whose SET list no longer carries
		// desired_machine_id: without this sole-writer call the relocate would answer
		// 200 and move nothing.
		if err := setMemberDesiredMachineIDOn(tx, worker.ID, machineID); err != nil {
			return err
		}
		return putMemberOn(tx, memberFromWorker(*worker))
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	outcome := s.relocateWorkerNow(*worker)
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	// The same bounded receipt the staff relocate answers (it forwards ow- ids
	// here): the two flags separate a wind-down opened by design from a move that
	// could not be dispatched, which used to answer the same clean 200.
	writeJSON(w, http.StatusOK, agentRelocateReceiptDTO{
		ID:                 worker.ID,
		RelocationPending:  outcome.Pending(),
		RelocationDeferred: outcome.WoundDown,
	})
}

// Outsource arm of POST /api/members/{member_id}/refocus — the cockpit's 換手.
// Stamps refocus_since, fans the SOP 預告 at the worker's own session and
// returns; the stop belongs to the worker's report_stopped (or a force-stop).
// There is NO grace deadline: refocusOpRefocus runs no clock (winddownKindFor).
// The refocus_since marker doubles as the tick's auto-handover cooldown and is
// cleared by the loop-break once the respawn lands.
func (s *apiServer) HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost(w http.ResponseWriter, r *http.Request, id string) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	online := s.hub.IsOnline(id)
	var worker *OutsourceWorker
	queued := false
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		// Offline: queue a 起來 behind the existing stop instead of refusing (owner
		// rc-bc1b029a3aa2) — the stop keeps its stage and anchors. The 409 remains
		// only for a worker nobody ever asked to stop (aStopWasEverAskedFor).
		if worker.DesiredState == DesiredStateOffline {
			if !s.queueWorkerRestartAfterStop(worker, refocusOpRefocus, nowSecs()) {
				return refuseInTx(http.StatusConflict,
					"refocus requires a live worker — this one is stopped and has never "+
						"been asked to stop, so there is no wind-down for a 起來 to be "+
						"queued behind (喚醒 it when you want it to run)")
			}
			queued = true
			return persistWorkerRestartIntentOn(tx, *worker)
		}
		queued = false
		if worker.Status != WorkerStatusActive || !online {
			return refuseInTx(http.StatusConflict,
				"refocus requires the worker to be online (no live session to hand over)")
		}
		// The wind-down ladder only goes forward (owner 2026-08-24). 換手 does not go
		// through respawnWorkerForOwnerOp, so this site needs its own guard: the
		// shared armRefocusEpoch on the member projection, folding back only the four
		// fields it mutates — a hand-written copy drifts from the shared decision.
		proj := memberFromWorker(*worker)
		if !armRefocusEpoch(&proj, refocusOpRefocus, nowSecs()) {
			return refuseInTx(http.StatusConflict,
				"refocus is 停止 and this worker is already further along the "+
					"wind-down ladder (下線 → 加速 → 強制); a later stage is never "+
					"replaced by an earlier one")
		}
		worker.RefocusSince = proj.RefocusSince
		worker.RefocusOp = proj.RefocusOp
		worker.StoppingSince = proj.StoppingSince
		worker.StoppedSince = proj.StoppedSince
		return persistWorkerRowOn(tx, *worker)
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	if queued {
		s.publishOutsourceWorker(*worker, requestTrigger(r))
		unlockMu()
		// AFTER the unlock: the tick takes outsourceMu itself. Spends a queued
		// start at once when the stop has already converged.
		s.outsourceTickNow()
		if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
			worker = fresh
		}
		writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
		return
	}
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
}

// Outsource arm of POST /api/members/{member_id}/accelerated-stop — the middle
// rung of 停止 → 加速停止 → 強制停止.
func (s *apiServer) HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(w http.ResponseWriter, r *http.Request, id string) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	online := s.hub.IsOnline(id)
	var worker *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		if !online {
			return refuseInTx(http.StatusConflict, acceleratedStopNeedsALiveSessionMsg)
		}
		proj := memberFromWorker(*worker)
		if err := accelerateMemberStop(&proj, nowSecs()); err != nil {
			return err
		}
		worker.StoppingSince = proj.StoppingSince
		worker.RefocusSince = proj.RefocusSince
		worker.RefocusOp = proj.RefocusOp
		worker.RestartAfterStop = proj.RestartAfterStop
		return persistWorkerRowOn(tx, *worker)
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	// The only fan-out of the final sentence to the worker: publishOutsourceWorker
	// is the owner-only cockpit patch and never reaches the worker's stream.
	// Without this, autoHandoverWorker's deadline clock starts while the worker
	// last heard the 停止 SOFT sentence.
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
}

// Outsource arm of POST /api/members/{member_id}/deactivate — the cockpit's 停止,
// a graceful close-out rather than a kill (owner 2026-08-21).
//   - NO forced_stop_at: that anchor keeps the notice silent, and this verb
//     needs offboardKindOf's SOFT 〈停止〉 notice (read off stopping_since) to
//     arrive.
//   - NO kill: the 收口 is the worker's own report_stopped. No deadline unless
//     the owner presses 加速停止 (rc-27d1710174dd 「不要兜底」).
//   - Refocus is cleared for a mechanical reason: autoHandoverWorker's in-flight
//     arm collects a refocus epoch by kill+RESPAWN, which would revive a worker
//     the owner just held down.
//
// The bound task stays in its own status.
func (s *apiServer) HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost(w http.ResponseWriter, r *http.Request, id string) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	var worker *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		// The row writes are applyStopVerbRow's (shared with the staff deactivate).
		// memberFromWorker only supplies the PRE-stop anchors; the result lands on the
		// WORKER row through stopVerbRowOfWorker's pointers, not on the projection.
		applyStopVerbRow(stopVerbRowOfWorker(worker), memberFromWorker(*worker), nowSecs())
		return persistWorkerRowOn(tx, *worker)
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	// Online: 預告 + wait. Offline: immediate kill (nothing can hear it).
	// openWorkerHandoverGrace re-reads liveness itself, so a disconnect racing
	// this handler cannot end in a respawn.
	s.openWorkerHandoverGrace(*worker, requestTrigger(r))
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
}

// Outsource arm of POST /api/members/{member_id}/force-stop — the third rung.
// Stamps forced_stop_at AND stopping_since: forcedEpochLive requires
// forced_stop_at >= stopping_since, so stamping one alone leaves a worker that
// announced its own wind-down reading as "working its close-out", the arm that
// speaks. It sends NOTHING; forced_stop_at is what keeps it silent. No online
// gate: a worker whose session is gone still needs its intent held down.
func (s *apiServer) HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost(w http.ResponseWriter, r *http.Request, id string) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	var worker *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		worker.DesiredState = DesiredStateOffline
		worker.RefocusSince = 0.0
		worker.RefocusOp = ""
		clearWorkerRestartIntent(worker)
		forcedAt := nowSecs()
		worker.ForcedStopAt = forcedAt
		if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt {
			worker.StoppingSince = forcedAt
		}
		return persistWorkerRowOn(tx, *worker)
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	s.stopWorkerNow(*worker)
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
}

// Outsource arm of POST /api/members/{member_id}/activate — the cockpit's 喚醒
// (owner rc-1f591528a6d0 圈 [0]: 正在跑就不動它):
//   - session ALREADY RUNNING → record the intent, dispatch NOTHING, kill
//     NOTHING, answer a session_alive receipt (as 喚醒 does for staff).
//   - session NOT running → clean sheet, re-dispatch.
//
// Never 409s and has no desired-offline gate.
func (s *apiServer) HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost(w http.ResponseWriter, r *http.Request, id string) {
	s.handleRestartOutsourceWorker(w, r, id, MemberActivateDTO{})
}

func (s *apiServer) handleRestartOutsourceWorker(w http.ResponseWriter, r *http.Request, id string, body MemberActivateDTO) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	// This removes the one-press way to end a wedged session, as a named trade:
	// 強制停止 then 喚醒 is the escape hatch; the one-press 「強制重來」 the owner
	// mentioned is deferred and does not exist yet.
	//
	// The session_alive receipt is in spawnBlockedReasonCodes: this arm
	// dispatches nothing, so a later landed START genuinely refutes it.
	sessionAliveReceipt := s.hub.IsOnline(id)
	var worker *OutsourceWorker
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		if body.MachineId != nil && *body.MachineId != "" {
			if _, err := resolveMachineOn(tx, *body.MachineId); err != nil {
				return machineResolveRefusal(err, *body.MachineId)
			}
		}
		if body.MachineId != nil {
			worker.DesiredMachineID = *body.MachineId
			if err := setMemberDesiredMachineIDOn(tx, worker.ID, worker.DesiredMachineID); err != nil {
				return err
			}
		}
		if sessionAliveReceipt {
			// Stamped onto the in-memory row, not via stampWorkerPlacementBlocked:
			// that helper re-reads and writes on its own and would race this
			// handler's write. The receipt columns land through setMemberLastOpOn below.
			stampSessionAliveWakeReceipt((*Member)(worker), nowSecs())
		}
		worker.DesiredState = DesiredStateOnline
		// Cleared on BOTH arms, as the staff 喚醒 does (api_members.go):
		// stopping_since is the 下線 this verb answers; waking_since is the stale
		// 喚醒中 badge — the live arm dispatches nothing that would restamp it.
		worker.StoppingSince = 0.0
		worker.WakingSince = 0.0
		// The other three anchors are cleared ONLY when a new session starts:
		//   * NOT RUNNING — they date the session being replaced. A stale pair
		//     (refocus > 0 ∧ stopped > 0) is read by workerHasStateToFlush as an
		//     already-collected wind-down, which shoots the next 改機器 / 換 model
		//     with no close-out; the epoch scoping cannot heal a stale PAIR.
		//   * ALREADY RUNNING — they describe a 加速停止 or 換手 mid-flight on the
		//     live session; clearing them would cancel it silently. Staff 喚醒 does
		//     not touch them either.
		//   * forced_stop_at is KEPT on both arms, as staff activate does: it
		//     describes the session BEFORE (dal.go, migrations/00057), and its max()
		//     upsert would fight a clear anyway.
		if !sessionAliveReceipt {
			worker.RefocusSince = 0.0
			worker.RefocusOp = ""
			worker.StoppedSince = 0.0
		}
		// 後蓋前: this handler spends the queued 起來 right now; leaving it armed
		// would fire a SECOND start after the next 下線.
		clearWorkerRestartIntent(worker)
		// Which columns the row write no longer carries lives only in
		// singleColumnOwnedFields.
		if err := persistWorkerRowOn(tx, *worker); err != nil {
			return err
		}
		// BEFORE the respawn: respawnWorkerForOwnerOp writes receipts of its own
		// (stampWorkerPlacementBlocked, stopWorkerSessionForHandover), and this
		// request-start snapshot written after it would bury the newer sentence on a
		// 200. ⚠️ No test holds this order — an independent review moved it and the
		// suite stayed green.
		if sessionAliveReceipt {
			return setMemberLastOpOn(tx, worker.ID, worker.LastOp, worker.LastOpOK,
				worker.LastOpLog, worker.LastOpReason, worker.LastOpAt)
		}
		return nil
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	// The kill lives in respawnWorkerForOwnerOp; not calling it is what makes
	// 喚醒 a no-op on a live worker. Built by hand: the zero outcome answers
	// Pending()==true, which would show a false pending badge.
	outcome := ownerOpOutcome{AlreadyRunning: true}
	if !sessionAliveReceipt {
		outcome = s.respawnWorkerForOwnerOp(*worker, ownerOpRestart)
	}
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	// activation_pending (omitted when the restart landed) flags a restart that
	// was decided but not delivered; the cause is on last_op_reason.
	writeJSON(w, http.StatusOK, outsourceRestartReceiptDTO{
		ID:                worker.ID,
		ActivationPending: outcome.Pending(),
		LastOpReason:      worker.LastOpReason,
	})
}

// Outsource arm of PATCH /api/members/{member_id}. Its floor is update_member's
// machine floor, not admin_agent (owner rc-376a41719e62 「正職跟外包一樣」; see
// routes.go). A live, active worker whose launch intent changed is handed over;
// otherwise the next spawn bakes the new values in.
func (s *apiServer) HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost(w http.ResponseWriter, r *http.Request, id string) {
	var body MemberUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.handleSetOutsourceWorkerModel(w, r, id, body)
}

func (s *apiServer) handleSetOutsourceWorkerModel(w http.ResponseWriter, r *http.Request, id string, body MemberUpdateDTO) {
	unlockMu := s.outsourceMu.Acquire()
	defer unlockMu()
	online := s.hub.IsOnline(id)
	var worker *OutsourceWorker
	launchIntentChanged, respawn := false, false
	var wantModel, wantRuntime, wantEffort string
	// Each intent lands through its sole writer (PutOutsourceWorker no longer
	// carries them).
	setIntents := func(tx *writeTx) error {
		if body.Model != nil {
			if err := setMemberModelOn(tx, id, wantModel); err != nil {
				return err
			}
		}
		if body.Runtime != nil {
			// Normalised, matching memberFromWorker's stored form, so the sole
			// writer never stores a second form on the same column.
			if err := setMemberRuntimeOn(tx, id, wantRuntime); err != nil {
				return err
			}
		}
		if body.Effort != nil {
			if err := setMemberEffortOn(tx, id, wantEffort); err != nil {
				return err
			}
		}
		return nil
	}
	err := s.dal.inTx(func(tx *writeTx) error {
		var err error
		if worker, err = resolveLiveWorkerOn(tx, id); err != nil {
			return err
		}
		if launchIntentChanged, err = applyWorkerLaunchIntent(worker, body); err != nil {
			return err
		}
		wantModel, wantRuntime, wantEffort = worker.Model, NormalizeRuntime(worker.Runtime), worker.Effort
		if err := putMemberOn(tx, memberFromWorker(*worker)); err != nil {
			return err
		}
		// Whether the owner wants it running is NOT re-asked here —
		// respawnWorkerForOwnerOp owns that branch for all three owner verbs.
		respawn = launchIntentChanged && worker.Status == WorkerStatusActive && online
		if !respawn && launchIntentChanged && worker.DesiredState == DesiredStateOffline {
			// A converged stop never enters the funnel (no active worker, no live
			// session), so the queued restart is stamped here; 改機器 has no such
			// gate. Owner 2026-08-30: 「change model / machine 只是帶起來的方式不一樣而已」.
			if s.queueWorkerRestartAfterStop(worker, ownerOpRuntimeModel, nowSecs()) {
				if err := persistWorkerRestartIntentOn(tx, *worker); err != nil {
					return err
				}
			}
		}
		if respawn {
			return nil
		}
		return setIntents(tx)
	})
	if err != nil {
		unlockMu()
		writeResolveTxError(w, err, "member", id)
		return
	}
	if respawn {
		// The funnel runs BEFORE the intents land, in a transaction of their own:
		// the tick starts the replacement only once the worker reads offline, by
		// which time they have landed; if the session drops between gate and
		// funnel, the funnel starts it from *worker, which already carries the new
		// values. Store first and a failure leaves the new value with no wind-down,
		// and the retry compares against the already-stored value and opens none
		// either. This order fails convergently. outsourceMu keeps the collect
		// from landing between the two writes.
		s.respawnWorkerForOwnerOp(*worker, ownerOpRuntimeModel)
		if err := s.dal.inTx(setIntents); err != nil {
			unlockMu()
			internalError(w, err)
			return
		}
	}
	if fresh, ferr := s.dal.GetOutsourceWorker(id); ferr == nil && fresh != nil {
		worker = fresh
	}
	s.publishOutsourceWorker(*worker, requestTrigger(r))
	unlockMu()

	writeJSON(w, http.StatusOK, agentLifecycleReceiptDTO{ID: worker.ID})
}

// applyWorkerLaunchIntent patches a worker's launch intents in place and reports
// whether one ACTUALLY changed (the staff HandleUpdateMember rule: the cockpit
// dialog re-saves unchanged values on every save), compared on the same trimmed
// form that gets persisted; a 422 comes back as a txRefusal.
func applyWorkerLaunchIntent(worker *OutsourceWorker, body MemberUpdateDTO) (bool, error) {
	launchIntentChanged := false
	if body.Model != nil {
		model := strings.TrimSpace(*body.Model) // blank ⇒ launcher default
		launchIntentChanged = launchIntentChanged || model != worker.Model
		worker.Model = model
	}
	if body.Runtime != nil {
		runtime := string(*body.Runtime)
		if !ValidRuntime(runtime) {
			return false, refuseInTx(http.StatusUnprocessableEntity,
				"runtime must be one of [claude codex]; got '"+runtime+"'")
		}
		launchIntentChanged = launchIntentChanged || runtime != worker.Runtime
		worker.Runtime = runtime
	}
	if body.Effort != nil {
		effort := strings.TrimSpace(*body.Effort)
		if !validEffort(effort) {
			return false, refuseInTx(http.StatusUnprocessableEntity,
				"effort must be one of [high low max medium xhigh]; got '"+effort+"'")
		}
		launchIntentChanged = launchIntentChanged || effort != worker.Effort
		worker.Effort = effort
	}
	return launchIntentChanged, nil
}
