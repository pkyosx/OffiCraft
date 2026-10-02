package main

import "slices"

// Every kill of either population resolves through killTargetChain and sends
// through sendStopFrames. dispatchShutdown (directly or through
// dispatchShutdownAlsoTo) has exactly TWO callers: the staff out-of-band robust
// STOP and the worker's stopped-report conclusion. The
// worker's other kills (handover, held-down stop, reclaim) enter at
// stopWorkerSessionOrPark, which uses the same chain and sender but keeps its
// OWN ledger.
//
// 🔴 LOCK CONTRACT: dispatchShutdown's caller holds NEITHER outsourceMu NOR
// reconcileMu. It takes them ONE AT A TIME, as the T-14 owner ruling fixes
// (lock A → run A → drop → lock B → run B → drop): outsourceMu for the spawn
// observation, then reconcileMu for the at-least-once dispatch marker. A caller
// still holding outsourceMu deadlocks. killTargetChain takes no lock at all,
// which is what lets the tick's already-locked kill sites share the ordering.

type shutdownDispatch struct {
	Target    string
	Broadcast bool
	Sent      bool
	Landed    []string
	Outsource bool
	// Addressed FALSE is the deferral signal: nothing was attempted, the only
	// shape a caller that latched stopped_since must roll back. A frame the
	// reachability gate REFUSED is addressed-but-not-sent — owed to a known
	// machine, parked, not rolled back.
	Addressed bool
}

type killTargetSources struct {
	SpawnTarget   string
	LastMachineID string
	Outsource     bool
}

// killTargetChain: the first NAMED source wins, even when its warden is
// unreachable — that is a known destination and the kill is owed there (parked
// and re-fired), not somewhere else. Order (owner-approved, rc-79c50144ddf4):
//
//	外包: workerSpawnTarget → hub.MachineOf → last_machine_id → broadcast
//	正職:                     hub.MachineOf → last_machine_id → desired pin → broadcast
//
// ⚠️ last_machine_id SITS ABOVE THE STAFF PIN on purpose: after a 換機器 the pin
// already names the destination while the session being killed is still on the
// origin; the last CONFIRMED landing is where the session actually is. The pin
// is still consulted because an offline member that never landed has nothing
// else.
//
// ⚠️ Reclaiming a released worker does NOT use last_machine_id: reclaimKillTargets
// passes no LastMachineID and fans out instead (worker_spawn.go).
//
// ⚠️ THIS IS THE KILL CHAIN ONLY. For the outsource SPAWN chain
// desired_machine_id is a hard pin (server/AGENTS.md §3): unusable means STALL
// with a machine_unavailable receipt, never a fallback.
func (s *apiServer) killTargetChain(id string, src killTargetSources) (targets []string, broadcast bool) {
	if named := s.namedKillTarget(id, src); named != "" {
		return []string{named}, false
	}
	return s.onlineWardens(), true
}

func (s *apiServer) namedKillTarget(id string, src killTargetSources) string {
	for _, cand := range s.killTargetCandidates(id, src) {
		return cand
	}
	return ""
}

func (s *apiServer) killTargetCandidates(id string, src killTargetSources) []string {
	var chain []string
	if src.Outsource {
		chain = []string{src.SpawnTarget, s.hub.MachineOf(id), s.activeWardenAt(src.LastMachineID)}
	} else {
		chain = []string{s.hub.MachineOf(id), s.activeWardenAt(src.LastMachineID), s.wardenTargetOf(id)}
	}
	out := make([]string, 0, len(chain))
	for _, cand := range chain {
		if cand != "" {
			out = append(out, cand)
		}
	}
	return out
}

// 🔴 The reachability filter belongs to reclaimWorkerSession ONLY. Everywhere
// else a NAMED-but-offline machine still wins and the kill is parked there
// (stopWorkerSessionOrPark). A reclaimed worker is released, so the session
// must die wherever it is; parking on a dark machine while another may hold the
// session is the wrong trade.
func (s *apiServer) reachableKillTarget(id string, src killTargetSources) string {
	for _, cand := range s.killTargetCandidates(id, src) {
		if s.hub.IsOnline(cand) {
			return cand
		}
	}
	return ""
}

func (s *apiServer) activeWardenAt(machineID string) string {
	if machineID == "" {
		return ""
	}
	cand, err := s.dal.GetMember(machineID)
	if err != nil || cand == nil || cand.Kind != KindWarden ||
		cand.RosterStatus != RosterStatusActive {
		return ""
	}
	return cand.ID
}

// Broadcast is safe: the frame addresses the subject's OWN derived session
// name, so a warden that never hosted it no-ops.
func (s *apiServer) onlineWardens() []string {
	members, err := s.dal.ListMembers()
	if err != nil {
		return nil
	}
	targets := []string{}
	for _, m := range members {
		if m.Kind == KindWarden && m.RosterStatus == RosterStatusActive && s.hub.IsOnline(m.ID) {
			targets = append(targets, m.ID)
		}
	}
	return targets
}

// Dropping workerSpawnAt here: a killed session must never leave a throttle
// stamp that delays its replacement's START.
func (s *apiServer) resolveShutdownTargets(id string) (targets []string, broadcast, outsource bool) {
	m, err := s.dal.GetMember(id)
	// 🔴 FAIL-CLOSED TO "WORKER" WHEN THE ROSTER CANNOT BE READ. Guessing
	// "staff" is the F1 defect: a worker handed RobustStopPendingAt has its START
	// suppressed and its machine benched as a zombie takeover. Guessing "worker"
	// costs one staff kill its cadence re-send.
	src := killTargetSources{Outsource: true}
	if err == nil && m != nil {
		src.LastMachineID = m.LastMachineID
		src.Outsource = m.Kind == KindOutsource
	}
	s.outsourceMu.Lock()
	src.SpawnTarget = s.workerSpawnTarget[id]
	delete(s.workerSpawnAt, id)
	s.outsourceMu.Unlock()
	targets, broadcast = s.killTargetChain(id, src)
	return targets, broadcast, src.Outsource
}

// enqueueStopFrames is THE place in this package that builds a `stop` frame and
// hands it to a warden. UNINSTALL is not a stop and keeps its own build in
// reconcileOne.
//
// 🔴 IT TAKES NO SCHEDULER LOCK: the outsource tick (holding outsourceMu) and
// the member tick (holding reconcileMu) call it. Reaching for either mutex in
// here would create the nested hold the T-14 ruling forbids.
func (s *apiServer) enqueueStopFrames(id string, targets []string) []string {
	frame, ok := buildTargetFrame(reconcileCmdStop, id)
	if !ok {
		return nil
	}
	landed := make([]string, 0, len(targets))
	for _, target := range targets {
		if s.enqueueToWarden(id, target, frame) {
			landed = append(landed, target)
		}
	}
	return landed
}

// receiptPending has one slot per subject, so a broadcast leaves the watch
// waiting on the last warden that took it.
//
// 🔴 The cross-machine identity sweep deliberately calls enqueueStopFrames
// directly, unwatched: every uninvolved warden answers no_such_session, which
// says nothing about the session we care about.
func (s *apiServer) sendStopFrames(id string, targets []string, now float64) []string {
	landed := s.enqueueStopFrames(id, targets)
	for _, target := range landed {
		s.armReceiptWatch(id, reconcileCmdStop, target, now)
	}
	return landed
}

func (s *apiServer) dispatchShutdown(id, reason string) shutdownDispatch {
	return s.dispatchShutdownAlsoTo(id, reason, "")
}

// dispatchShutdownAlsoTo also aims the stop at alsoTo when the kill chain does not already: a
// machine the caller knows holds a session the chain cannot name, such as a still-booting START.
// It goes last, so when it lands the receipt watch's single slot waits on it.
func (s *apiServer) dispatchShutdownAlsoTo(id, reason, alsoTo string) shutdownDispatch {
	targets, broadcast, outsource := s.resolveShutdownTargets(id)
	if alsoTo != "" && !slices.Contains(targets, alsoTo) {
		targets = append(targets, alsoTo)
	}
	out := shutdownDispatch{
		Broadcast: broadcast, Outsource: outsource, Addressed: len(targets) > 0,
	}
	if !broadcast && len(targets) == 1 {
		out.Target = targets[0]
	}
	now := nowSecs()
	out.Landed = s.sendStopFrames(id, targets, now)
	out.Sent = len(out.Landed) > 0
	if len(targets) == 0 {
		reconcileLog("shutdown %s (%s): no kill target — spawn memory, live claim, "+
			"last landing and pin all silent, and no warden is online", id, reason)
	}
	// 🔴 STAFF ONLY. reconcileStates is shared by both populations and
	// reconcileWorkerLiveness feeds the same reconcileDecide: a worker given
	// RobustStopPendingAt has its due START suppressed, then gets a STOP its path
	// reads as a ZOMBIE TAKEOVER and benches the machine. Workers re-send through
	// workerStopLanded / workerStopPending. Armed for staff even on a fail-closed
	// refusal, because an unreachable warden is exactly how a collect goes missing.
	if !out.Outsource {
		s.noteRobustStopDispatched(id, now)
	}
	// 🔴 Only when something was aimed at: otherwise nothing was sent and the
	// session may still be running, and dropping its boot_ts would make
	// restart_self's minimum-liveness gate and the boot-storm guard fail OPEN.
	if out.Addressed {
		s.clearSessionBootTS(id)
	}
	return out
}
