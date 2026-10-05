package main

import "slices"

// Every kill of either population resolves through killTargetChain and sends
// through sendStopFrames; every out-of-band robust STOP of either population is
// recorded in the one robust-stop ledger (stop_ledger.go, sendRobustStop) and
// re-sent only by the ticks. dispatchShutdown (directly or through
// dispatchShutdownAlsoTo) has exactly ONE caller: dispatchRobustStopNow, the
// out-of-band robust STOP every stop verb of both populations sends. The
// session-gone collects of both populations go through stopResidualSession,
// the worker's handover and takeover through sendRobustStop, all with targets their
// caller resolved under its own tick lock; reclaim sends unrecorded.
//
// 🔴 LOCK CONTRACT: dispatchShutdown's caller holds NEITHER outsourceMu NOR
// reconcileMu. It takes outsourceMu for the spawn observation and drops it
// before the send; the ledger's own lock is a leaf. A caller still holding
// outsourceMu deadlocks. killTargetChain takes no lock at all, which is what
// lets the ticks' already-locked kill sites share the chain.

type shutdownDispatch struct {
	Target    string
	Broadcast bool
	Sent      bool
	Landed    []string
	Outsource bool
	// Recorded is what the ledger made of the STOP; what a caller may conclude
	// from it is robustStopEffectOf's, never a test of its own.
	Recorded robustStopOutcome
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
// (sendRobustStop). A reclaimed worker is released, so the session
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
//
// alsoTo, when set, is a START still booting: it counts as a named target, and a
// worker's spawn memory naming that same START is dropped from the chain so it
// cannot hide where an earlier session last landed.
func (s *apiServer) resolveShutdownTargets(id, alsoTo string) (targets []string, broadcast, outsource bool) {
	m, err := s.dal.GetMember(id)
	// The kind only picks the chain. With the roster unreadable the worker chain
	// is the wider one: it still reads the in-memory spawn target, while the
	// staff chain's extra source (the pin) needs the row anyway.
	src := killTargetSources{Outsource: true}
	if err == nil && m != nil {
		src.LastMachineID = m.LastMachineID
		src.Outsource = m.Kind == KindOutsource
	}
	s.outsourceMu.Lock()
	src.SpawnTarget = s.workerSpawnTarget[id]
	delete(s.workerSpawnAt, id)
	s.outsourceMu.Unlock()
	if src.SpawnTarget == alsoTo {
		src.SpawnTarget = ""
	}
	if named := s.namedKillTarget(id, src); named != "" {
		targets = []string{named}
	} else if alsoTo == "" {
		return s.onlineWardens(), true, src.Outsource
	}
	if alsoTo != "" && !slices.Contains(targets, alsoTo) {
		targets = append(targets, alsoTo)
	}
	return targets, false, src.Outsource
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
	targets, broadcast, outsource := s.resolveShutdownTargets(id, alsoTo)
	out := shutdownDispatch{
		Broadcast: broadcast, Outsource: outsource,
	}
	if !broadcast && len(targets) == 1 {
		out.Target = targets[0]
	}
	out.Recorded = s.stopResidualSession(id, targets, broadcast, nowSecs())
	out.Landed = out.Recorded.Landed
	out.Sent = len(out.Landed) > 0
	if len(targets) == 0 {
		reconcileLog("shutdown %s (%s): no kill target — spawn memory, live claim, "+
			"last landing and pin all silent, and no warden is online", id, reason)
	}
	return out
}

// stopResidualSession is the one "stop the session this id leaves behind" of both
// populations: the STOP is recorded in the ledger, and the session boundary is
// recorded only when robustStopEffectOf allows it. Takes no scheduler lock.
func (s *apiServer) stopResidualSession(id string, targets []string, fanout bool, now float64) robustStopOutcome {
	out := s.sendRobustStop(id, targets, fanout, now)
	if robustStopEffectOf(out, "").ClearBootTS {
		s.clearSessionBootTS(id)
	}
	return out
}
