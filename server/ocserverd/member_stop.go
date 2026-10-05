package main

import "net/http"

// The stop verbs — 停止 (deactivate), 加速停止, 強制停止, the agent's own stopped report
// and the STOP that cancels a wake — are one body each for both populations. What a
// stopPopulation carries is the only part that differs: which lock guards the
// population, how its row is read, and the population's own FSM door.
//
// 🔴 LOCKS: a verb holds at most ONE scheduler lock at a time and none across the
// kill (dispatchShutdown takes outsourceMu itself). reconcileMu and outsourceMu are
// never held together anywhere in the package (lifecycle_tick.go).
type stopPopulation struct {
	// lockRows guards the row write. The outsource tick read-modify-writes worker
	// rows and the spawn maps under outsourceMu, so a worker's stop writes under it
	// too. A staff press takes nothing: it must never wait on the reconcile tick.
	lockRows func() (unlock func())
	// lockScheduler serialises a read-modify-write of this population's
	// reconcileStates entry (reconcileMu for staff, outsourceMu for workers).
	lockScheduler func() (unlock func())
	loadOn        func(q sqlRowQuerier, id string) (*Member, error)
	// reconcileNow runs the population's FSM at once; it takes its own lock, so the
	// caller must hold none.
	reconcileNow func(id string)
	// noteCollectKill runs after a stopped report's kill went out, holding no lock.
	noteCollectKill func(id string, now float64)
	// producerOff is --no-reconcile. The outsource verbs have never consulted it
	// (api_stub.go): a shadow server still stops real workers.
	producerOff bool
}

func noLock() (unlock func()) { return func() {} }

func (s *apiServer) stopPopulationOf(m Member) stopPopulation {
	if m.Kind == KindOutsource {
		return s.workerStopPopulation()
	}
	return s.staffStopPopulation()
}

func (s *apiServer) staffStopPopulation() stopPopulation {
	return stopPopulation{
		lockRows:      noLock,
		lockScheduler: s.reconcileMu.Acquire,
		loadOn: func(q sqlRowQuerier, id string) (*Member, error) {
			return resolveMemberOn(q, id, anyMember)
		},
		reconcileNow:    func(id string) { s.reconcileMemberNow(id) },
		noteCollectKill: func(string, float64) {},
		producerOff:     s.noReconcile,
	}
}

func (s *apiServer) workerStopPopulation() stopPopulation {
	return stopPopulation{
		lockRows:      s.outsourceMu.Acquire,
		lockScheduler: s.outsourceMu.Acquire,
		loadOn: func(q sqlRowQuerier, id string) (*Member, error) {
			w, err := resolveLiveWorkerOn(q, id)
			if err != nil {
				return nil, err
			}
			m := memberFromWorker(*w)
			return &m, nil
		},
		reconcileNow: func(id string) {
			unlock := s.outsourceMu.Acquire()
			defer unlock()
			if w, err := s.resolveLiveWorker(id); err == nil {
				s.reconcileWorkerNow(*w, nowSecs())
			}
		},
		// The worker FSM reads the stop as in flight from its own state, not from
		// the ledger alone. Staff cannot take reconcileMu here: the report's kill
		// must never wait on the tick.
		noteCollectKill: func(id string, now float64) {
			unlock := s.outsourceMu.Acquire()
			defer unlock()
			if w, err := s.resolveLiveWorker(id); err == nil {
				s.noteWorkerShutdownDispatched(*w, now)
			}
		},
	}
}

// stopMember is the cockpit's 停止, a graceful close-out rather than a kill
// (owner 2026-08-21):
//   - NO forced_stop_at: that anchor keeps the notice silent, and this verb needs
//     offboardKindOf's SOFT 〈停止〉 notice (read off stopping_since) to arrive.
//   - a live session gets the notice and no kill: the 收口 is its own
//     report_stopped, with no deadline unless the owner presses 加速停止
//     (rc-27d1710174dd 「不要兜底」).
//   - Refocus is cleared for a mechanical reason: the FSM's recycle arm collects a
//     refocus epoch by kill+RESPAWN, which would revive what the owner just held down.
func (s *apiServer) stopMember(m Member, trigger string) (Member, error) {
	pop := s.stopPopulationOf(m)
	sessionAlive := s.hub.IsOnline(m.ID)
	arm := stopArmSoftWindow
	var stopped, saved Member
	unlockRows := pop.lockRows()
	defer unlockRows()
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := pop.loadOn(tx, m.ID)
		if err != nil {
			return err
		}
		before := *cur
		arm = stopVerbArmOf(*cur, sessionAlive, nowSecs())
		applyStopVerbRow(stopVerbRowOfMember(cur), *cur, nowSecs())
		stopped = *cur
		// A session already gone is collected right here: the stopped latch lands
		// with the stop, so a failure leaves neither and a retry is a first collect.
		if arm == stopArmCollectNow {
			collectWindDownRow(windDownAnchorRowOfMember(cur), nowSecs())
		}
		saved = *cur
		return persistMemberRowOn(tx, before, *cur)
	})
	if err != nil {
		return Member{}, err
	}
	s.publishMemberPatch(stopped, trigger)
	if arm == stopArmCollectNow {
		s.publishMemberPatch(saved, trigger)
	}
	if arm != stopArmSoftWindow {
		s.bankLiveCost(saved.ID)
	}
	unlockRows()
	switch arm {
	case stopArmCollectNow:
		s.dispatchRobustStopNow(pop, saved.ID)
	case stopArmCancelWake:
		// Not widened to the online case: a live session gets the soft window and is
		// collected by its own report_stopped or the owner's 加速停止 / 強制停止.
		s.dispatchRobustStopPastBootingStart(pop, saved.ID)
	}
	// Arms no clock (owner ruling). Still run after a cancel: the dispatch above
	// touches the reconcile store only to retire a START its stop reached.
	pop.reconcileNow(saved.ID)
	return saved, nil
}

// forceStopMember is the third rung of 停止 → 加速停止 → 強制停止. It stamps
// forced_stop_at AND stopping_since: forcedEpochLive requires forced_stop_at >=
// stopping_since, so stamping one alone leaves a member that announced its own
// wind-down reading as "working its close-out", the arm that speaks. No online
// gate: a member whose session is gone still needs its intent held down.
func (s *apiServer) forceStopMember(m Member, trigger string) (Member, error) {
	pop := s.stopPopulationOf(m)
	var saved Member
	unlockRows := pop.lockRows()
	defer unlockRows()
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := pop.loadOn(tx, m.ID)
		if err != nil {
			return err
		}
		before := *cur
		cur.DesiredState = DesiredStateOffline
		clearMemberHandoverMarker(cur)
		clearRestartIntent(cur)
		forcedAt := nowSecs()
		if cur.StoppingSince <= 0.0 || cur.StoppingSince > forcedAt {
			cur.StoppingSince = forcedAt
		}
		// Force-stop sends no notice, so this record is the only trace a session was
		// cut off; forward-only, so a stale snapshot cannot erase it.
		cur.ForcedStopAt = forcedAt
		if err := persistMemberRowOn(tx, before, *cur); err != nil {
			return err
		}
		// Not fatal, and a failed statement does not end the transaction: the kill
		// below is the point, and "force-stop failed" would be false.
		if err := setMemberForcedStopAtOn(tx, cur.ID, cur.ForcedStopAt); err != nil {
			taskLog("force-stop %s: forced_stop_at not recorded: %v", cur.ID, err)
		}
		saved = *cur
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	s.publishMemberPatch(saved, trigger)
	// Bank before the kill; bankLiveCost pops, so the later disconnect edge is
	// idempotent.
	s.bankLiveCost(saved.ID)
	unlockRows()
	s.dispatchRobustStopNow(pop, saved.ID)
	return saved, nil
}

const (
	acceleratedStopNeedsAnOpenWindDownMsg = "加速停止 escalates a wind-down that is " +
		"already open — this member has not been asked to stop. Press 停止 (deactivate) " +
		"or 重新聚焦 (refocus) first"
	acceleratedStopNeedsALiveSessionMsg = "加速停止 requires a live session — there is " +
		"nothing to accelerate on a member that is not connected"
	acceleratedStopAlreadyForcedMsg = "加速停止 has nothing to escalate — this member was " +
		"already force-stopped (強制停止): its session was cut off and no wind-down is open"
)

// accelerateStopMember is the middle rung of 停止 → 加速停止 → 強制停止 (owner
// 2026-08-21). 🔴 It escalates, never initiates (409 otherwise): a member never
// asked to stop would get a deadline it never heard about. It does not reopen
// rc-27d1710174dd: that ruling forbids a clock the SERVER starts; this one is the
// owner's press.
// A 換手 not yet on the clock is re-stamped: promoting in place would put the
// deadline at the ORIGINAL stamp, already past, collecting the member on the tick
// that announced it. A force-stopped epoch is refused (no reader).
func (s *apiServer) accelerateStopMember(m Member, trigger string) (Member, error) {
	pop := s.stopPopulationOf(m)
	// The notice travels on the member's own stream; a clock nobody hears is a
	// silent deadline.
	if !s.hub.IsOnline(m.ID) {
		return Member{}, refuseInTx(http.StatusConflict, acceleratedStopNeedsALiveSessionMsg)
	}
	var saved Member
	unlockRows := pop.lockRows()
	defer unlockRows()
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := pop.loadOn(tx, m.ID)
		if err != nil {
			return err
		}
		before := *cur
		if err := accelerateMemberStop(cur, nowSecs()); err != nil {
			return err
		}
		saved = *cur
		return persistMemberRowOn(tx, before, *cur)
	})
	if err != nil {
		return Member{}, err
	}
	// The final sentence rides this delta to the member's own stream; without it
	// the deadline clock (decideDown) starts while the member last heard the 停止
	// SOFT sentence.
	s.publishMemberPatch(saved, trigger)
	unlockRows()
	pop.reconcileNow(saved.ID)
	reconcileLog("加速停止: %s on the %s arm (collect at %.0f or on the stopped report)",
		saved.ID, saved.DesiredState, winddownDeadlineOf(saved, s.reconcileConfigLive()))
	return saved, nil
}

// accelerateMemberStop puts the open wind-down on the clock from now, or refuses
// (409) a member that has none open. A wind-down already on the clock keeps its
// anchor, so pressing again does not move its deadline.
func accelerateMemberStop(m *Member, now float64) error {
	_, alreadyClocked := winddownKindFor(m.RefocusOp)
	switch {
	case m.DesiredState == DesiredStateOffline:
		// STOP-EPOCH-TERM-AUDIT: forcedEpochLive only picks which refusal to word; the
		// gate itself is gracefulStopEpochOpen.
		if forcedEpochLive(*m) {
			return refuseInTx(http.StatusConflict, acceleratedStopAlreadyForcedMsg)
		}
		if !gracefulStopEpochOpen(*m) {
			return refuseInTx(http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
		}
		// Other anchors untouched: zeroing stopped_since would erase the agent's
		// 「我收完了」 and cancel a collection it already earned.
		if !alreadyClocked {
			m.StoppingSince = now
		}
	case m.RefocusSince > 0.0:
		if !alreadyClocked {
			m.RefocusSince = now
		}
	default:
		return refuseInTx(http.StatusConflict, acceleratedStopNeedsAnOpenWindDownMsg)
	}
	m.RefocusOp = refocusOpAcceleratedStop
	// On the 換手 arm desired_state stays ONLINE (a hurried handover, not a stop);
	// deliberately not widened — outside the owner's [0] ruling.
	clearRestartIntent(m)
	return nil
}

// reportMemberStopped is the agent's own 「我收完了」. 🔴 A stopped-report is ALWAYS
// collected (owner, rc-b08d49dc3b03 option ①); the latch lands with the row or not
// at all: a latch left behind a failed write would make every retry read "already
// reported" and dispatch nothing, forever.
func (s *apiServer) reportMemberStopped(m Member, trigger string) (Member, string, error) {
	pop := s.stopPopulationOf(m)
	now := nowSecs()
	var saved Member
	collect, stopEffect := false, ""
	unlockRows := pop.lockRows()
	defer unlockRows()
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := pop.loadOn(tx, m.ID)
		if err != nil {
			return err
		}
		before := *cur
		collect, stopEffect = decideStoppedReport(windDownAnchorRowOfMember(cur), now)
		saved = *cur
		return persistMemberRowOn(tx, before, *cur)
	})
	if err != nil {
		return Member{}, "", err
	}
	if !collect {
		return saved, stopEffect, nil
	}
	// Before the kill: the delta is what the agent's RecycleHook reads.
	s.publishMemberPatch(saved, trigger)
	s.bankLiveCost(saved.ID)
	unlockRows()
	s.dispatchRobustStopNow(pop, saved.ID)
	pop.noteCollectKill(saved.ID, now)
	// The lock was open across the kill, so the row may have moved (the owner can
	// hold the member down meanwhile); the receipt answers the row as it is now.
	if fresh, err := pop.loadOn(s.dal.rdb, saved.ID); err == nil {
		saved = *fresh
	}
	return saved, stopEffect, nil
}

func decideStoppedReport(row windDownAnchorRow, now float64) (collect bool, stopEffect string) {
	if latched, _ := collectWindDownRow(row, now); !latched {
		return false, stopEffectAlreadyReported
	}
	return true, stopEffectCollected
}
