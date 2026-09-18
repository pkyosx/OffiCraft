package main

// ── THE shutdown dispatch — one body, both populations (T-253) ───────────────
//
// 正職 and 外包 used to reach a warden STOP through two hand-kept bodies:
// dispatchRobustStopNow (reconcile.go) for staff, and the
// enqueueWorkerStop / stopWorkerSessionOrPark pair (worker_spawn.go) for
// workers. They had drifted on four separate axes — the worker arm armed a
// receipt watch and cleared the spawn pacing, the staff arm did neither; the
// worker arm knew a second way to find the session's machine, the staff arm
// knew one. None of those four differences had a reason: server/CLAUDE.md §3
// carries the owner ruling that the two populations must not differ on
// stop / start / 換機器 at all, and names the only two deliberate exceptions
// (their OBSERVED inputs, and worker-only task state). A stop is neither.
//
// So this file holds the one body. The only thing that still differs by
// population is WHICH SOURCES can name the machine to kill on — the worker arm
// has one extra, the server's in-memory memory of where it dispatched the spawn.
// Everything after that point is identical by construction.
//
// 🔴 LOCK CONTRACT: dispatchShutdown's caller holds NEITHER outsourceMu NOR
// reconcileMu. It takes them ONE AT A TIME, in the order the T-14 owner ruling
// fixes (lifecycle_tick.go, verbatim: lock A → run A → drop → lock B → run B →
// drop): outsourceMu for the in-memory spawn observation, dropped, then
// reconcileMu for the at-least-once dispatch marker. A caller that enters still
// holding outsourceMu deadlocks on the spot rather than quietly creating this
// package's first double-holder — Go mutexes do not re-enter, and that is the
// guard. killTargetChain below is the half that takes no lock at all, which is
// what lets the tick's already-locked kill sites share the same ordering.

// shutdownDispatch reports what one shutdown actually did, for the callers that
// have to decide whether a kill is owed again (the worker park / retry
// bookkeeping, and the stopped-latch rollback).
type shutdownDispatch struct {
	// Target is the single warden the stop was addressed to, "" when the chain
	// found nothing to name and the stop went out as a broadcast (or not at all).
	Target string
	// Broadcast is true when the chain fell through to every online warden.
	Broadcast bool
	// Sent is true when at least one frame was accepted by the enqueue gate.
	Sent bool
	// Addressed is true when the chain produced at least one warden to aim at.
	// FALSE is the deferral signal: nothing was even attempted, which is the
	// only shape a caller that latched stopped_since must roll that latch back
	// for. A frame the reachability gate REFUSED is addressed-but-not-sent — it
	// is owed to a known machine and gets parked, not rolled back.
	Addressed bool
}

// killTargetSources are the per-population inputs of the kill-target chain,
// read by the caller so this half needs no lock of its own.
type killTargetSources struct {
	// SpawnTarget is the in-memory warden the last spawn was dispatched to
	// (workerSpawnTarget). Outsource only; "" for staff, which has no such map.
	SpawnTarget string
	// LastMachineID is the durable last-observed landing (dal.go): the machine a
	// CONFIRMED session of this entity last connected from.
	LastMachineID string
	// Outsource selects the worker ordering.
	Outsource bool
}

// killTargetChain is the ORDERED list of wardens a STOP for id may be addressed
// to, most-authoritative first, plus whether the answer is the broadcast last
// resort. The first NAMED source wins; a source that names an unreachable
// warden still wins, because that is a known destination and the kill is owed
// there (parked and re-fired), not somewhere else.
//
// 🔴 THE EXISTING ORDER IS UNCHANGED — the two new sources are appended, never
// interleaved (owner-approved, rc-79c50144ddf4):
//
//	外包: workerSpawnTarget → hub.MachineOf → last_machine_id → broadcast
//	正職:                     hub.MachineOf → last_machine_id → desired pin → broadcast
//
// ⚠️ last_machine_id SITS ABOVE THE STAFF PIN, and the asymmetry is the point.
// The pin is a statement about where the member should run NEXT; after a
// 換機器 it already names the destination while the session being killed is
// still on the origin. The last CONFIRMED landing is the one that describes
// where the session actually is, so it outranks the intent. (That is also why
// the pin is still consulted at all: an offline member that has never landed
// anywhere has nothing else.)
//
// ⚠️ THIS IS THE KILL CHAIN ONLY. The outsource SPAWN chain is untouched:
// server/CLAUDE.md §3 makes desired_machine_id a hard pin there — unusable
// means STALL with a machine_unavailable receipt, never a fallback.
//
// Holds no lock; the caller supplies the two row-shaped sources.
func (s *apiServer) killTargetChain(id string, src killTargetSources) (targets []string, broadcast bool) {
	if named := s.namedKillTarget(id, src); named != "" {
		return []string{named}, false
	}
	return s.onlineWardens(), true
}

// namedKillTarget is killTargetChain without the broadcast tail: the first
// source that NAMES a machine, "" when none does. Split out so the read-path
// twins (memberKillTargetWarden / resolveWorkerKillTarget) can ask the ordering
// question without paying for a roster scan they would only throw away.
func (s *apiServer) namedKillTarget(id string, src killTargetSources) string {
	var chain []string
	if src.Outsource {
		chain = []string{src.SpawnTarget, s.hub.MachineOf(id), s.activeWardenAt(src.LastMachineID)}
	} else {
		chain = []string{s.hub.MachineOf(id), s.activeWardenAt(src.LastMachineID), s.wardenTargetOf(id)}
	}
	for _, cand := range chain {
		if cand != "" {
			return cand
		}
	}
	return ""
}

// activeWardenAt answers the warden member id hosting machine, or "" when the
// roster has no active warden there. Same roster test wardenTargetOf applies to
// a pin — a machine id is only a destination while a live warden owns it.
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

// onlineWardens is the BROADCAST last resort: every online, active warden.
//
// Killing by broadcast is safe by construction and for exactly the reason
// reclaimWorkerSession has always relied on (worker_spawn.go): the frame
// addresses the subject's OWN derived session name, so a warden that never
// hosted it no-ops. Nothing else can be killed by a stop aimed this way.
//
// Holds no lock.
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

// resolveShutdownTargets is killTargetChain with the per-population sources
// read for the caller, AND the spawn pacing dropped in the same locked region.
//
// The pacing drop rides here rather than at a call site because it is one of
// the four things the two populations had drifted on: a session that is being
// killed must never leave a throttle stamp behind that delays its replacement's
// START. It is a plain no-op for staff (nothing ever stamps a staff id into
// workerSpawnAt), which is what makes "both populations run the same body"
// true rather than merely tidy.
//
// Takes outsourceMu itself and drops it before returning: the caller holds it
// not at all.
func (s *apiServer) resolveShutdownTargets(id string) ([]string, bool) {
	m, err := s.dal.GetMember(id)
	src := killTargetSources{}
	if err == nil && m != nil {
		src.LastMachineID = m.LastMachineID
		src.Outsource = m.Kind == KindOutsource
	}
	s.outsourceMu.Lock()
	src.SpawnTarget = s.workerSpawnTarget[id]
	delete(s.workerSpawnAt, id)
	s.outsourceMu.Unlock()
	return s.killTargetChain(id, src)
}

// enqueueStopFrames is THE place in this package that builds a `stop` frame and
// hands it to a warden. Every stop the server sends — the report-stopped
// collect, the outsource tick's handover / 停止 / zombie takeover / reclaim, the
// member tick's desired-offline and relocation STOP, and the cross-machine
// identity sweep — goes through this one body. It answers the targets the
// fail-closed reachability gate ACCEPTED, in the order given.
//
// 🔴 IT TAKES NO SCHEDULER LOCK, AND THAT IS WHAT MAKES IT SHAREABLE.
// buildTargetFrame is pure and enqueueToWarden only touches the hub, so a
// caller already holding outsourceMu (the outsource tick) or reconcileMu (the
// member tick) may call it — which is the whole reason the SEND is split out of
// dispatchShutdown rather than living inside its lock dance. Anything in here
// that reached for either mutex would silently create the nested hold the T-14
// ruling forbids, at three call sites at once.
//
// What stays OUTSIDE, deliberately, is each caller's own BOOKKEEPING — the
// worker park/retry arms, the member FSM's DispatchUnlanded, the sweep's dedupe
// stamp. Those are genuinely per-caller state, not a second copy of the send.
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

// sendStopFrames is enqueueStopFrames plus the receipt watch a landed stop owes
// (receipt_watch.go): armed PER ACCEPTED TARGET and only after the enqueue was
// accepted, which is that watch's own rule. One slot per subject, so a
// broadcast leaves the watch waiting on the last warden that took it — the
// pre-existing single-slot posture of receiptPending, not a new limitation.
//
// 🔴 THE SWEEP IS THE ONE STOP THAT DELIBERATELY GOES UNWATCHED and therefore
// calls enqueueStopFrames directly: a cross-machine identity sweep fans the
// same id at the whole fleet, and every uninvolved warden answers
// no_such_session — arming a deadline there would be waiting on an answer that
// carries no information about the session we care about. UNINSTALL is not a
// stop at all and keeps its own build in reconcileOne, for the reason stated
// there.
//
// Takes no scheduler lock either (armReceiptWatch owns receiptMu).
func (s *apiServer) sendStopFrames(id string, targets []string, now float64) []string {
	landed := s.enqueueStopFrames(id, targets)
	for _, target := range landed {
		s.armReceiptWatch(id, reconcileCmdStop, target, now)
	}
	return landed
}

// dispatchShutdown sends ONE stop for id and is the whole of what "send the
// shutdown command" means for either population: resolve the target machine →
// send the frame → arm the receipt watch → drop the spawn pacing → drop the
// session boot anchor, plus the at-least-once dispatch marker that makes a
// frame the fail-closed gate refused re-sendable by the cadence.
//
// reason is a short log tag; it never reaches the wire.
//
// The send itself is sendStopFrames above — shared with both ticks.
//
// See the lock contract at the head of this file.
func (s *apiServer) dispatchShutdown(id, reason string) shutdownDispatch {
	targets, broadcast := s.resolveShutdownTargets(id)
	out := shutdownDispatch{Broadcast: broadcast, Addressed: len(targets) > 0}
	if !broadcast && len(targets) == 1 {
		out.Target = targets[0]
	}
	now := nowSecs()
	out.Sent = len(s.sendStopFrames(id, targets, now)) > 0
	if len(targets) == 0 {
		reconcileLog("shutdown %s (%s): no kill target — spawn memory, live claim, "+
			"last landing and pin all silent, and no warden is online", id, reason)
	}
	// Armed UNCONDITIONALLY, including on a fail-closed refusal — an unreachable
	// warden is exactly how a collect goes missing (T-ed79).
	s.noteRobustStopDispatched(id, now)
	// The session is over: drop its boot_ts so the respawn's first connect
	// re-stamps a fresh anchor (T-8fb2).
	s.clearSessionBootTS(id)
	return out
}
