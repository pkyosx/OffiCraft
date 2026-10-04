package main

// stop_ledger.go — the one record of "a robust STOP was sent for this id and is
// owed until the session is gone", for staff and outsource workers alike, and the
// one judgement over it. A frame on a warden's FIFO is not a dead session: the
// drain deletes the FIFO before writing it and no ack exists, and the fail-closed
// reachability gate drops a STOP toward a warden with no downstream. Every
// out-of-band robust STOP is therefore recorded here and re-sent by the ticks
// (stepRobustStop) until the session it aimed at is gone.
//
// 🔴 robustStopMu IS A LEAF. Hold it for the map access only — never across hub,
// dal or enqueue calls. The staff tick (reconcileMu), the outsource tick
// (outsourceMu) and handlers holding neither all reach it, so anything taken
// under it would nest the two scheduler locks the T-14 ruling keeps apart.

type robustStop struct {
	// Target is the one machine the STOP was aimed at; "" for a fan-out, which is
	// re-resolved through the kill chain on every re-send.
	Target string
	At     float64
	// Landed false means parked: the aimed warden refused the frame, and the
	// tick re-fires it there every pass until a warden takes it.
	Landed bool
}

type robustStopOutcome struct {
	Landed []string
	Parked string
}

func (o robustStopOutcome) recorded() bool {
	return len(o.Landed) > 0 || o.Parked != ""
}

// A parked stop counts: the tick re-fires it there.
func (o robustStopOutcome) reached() []string {
	if o.Parked == "" {
		return o.Landed
	}
	return append(append([]string(nil), o.Landed...), o.Parked)
}

type robustStopStep int

const (
	robustStopDone robustStopStep = iota
	robustStopWait
	robustStopResend
)

// robustStopStepOf is THE judgement for an owed robust STOP, both populations.
// aimAlive is the caller's evidence the session this STOP aimed at still runs
// (robustStopAimAlive).
func robustStopStepOf(rs robustStop, aimAlive bool, stopRetry, now float64) robustStopStep {
	switch {
	case !rs.Landed:
		return robustStopResend
	case !aimAlive:
		return robustStopDone
	case now-rs.At >= stopRetry:
		return robustStopResend
	default:
		return robustStopWait
	}
}

// robustStopAimAlive: hub.IsOnline keys on the id, which is online again
// elsewhere after a respawn or a 換機器, so an aimed STOP counts the session as
// alive only on its own machine. A claim-less connection names no machine and
// counts as alive — a staff session booted without a claim must not be judged
// stopped while it is still connected.
func (s *apiServer) robustStopAimAlive(id string, rs robustStop) bool {
	if !s.hub.IsOnline(id) {
		return false
	}
	running := s.hub.MachineOf(id)
	return rs.Target == "" || running == "" || running == rs.Target
}

func (s *apiServer) robustStopOf(id string) (robustStop, bool) {
	s.robustStopMu.Lock()
	defer s.robustStopMu.Unlock()
	rs, ok := s.robustStops[id]
	return rs, ok
}

// sendRobustStop sends a robust STOP and records it. A refused single target is
// parked; a fan-out every warden refused, or no target at all, records nothing
// and returns an empty outcome — the caller must defer or roll back. Takes no
// scheduler lock.
func (s *apiServer) sendRobustStop(id string, targets []string, now float64) robustStopOutcome {
	if len(targets) == 0 {
		return robustStopOutcome{}
	}
	landed := s.sendStopFrames(id, targets, now)
	aimed := ""
	if len(targets) == 1 {
		aimed = targets[0]
	}
	s.robustStopMu.Lock()
	defer s.robustStopMu.Unlock()
	switch {
	case len(landed) > 0:
		s.robustStops[id] = robustStop{Target: aimed, At: now, Landed: true}
		return robustStopOutcome{Landed: landed}
	case aimed == "":
		delete(s.robustStops, id)
		reconcileLog("robust stop %s: every warden in the fan-out refused (%v) — "+
			"nothing sent and nothing parked; the caller must defer", id, targets)
		return robustStopOutcome{}
	}
	s.robustStops[id] = robustStop{Target: aimed, At: now}
	reconcileLog("robust stop %s: target %s unreachable — parked, the tick re-fires it", id, aimed)
	return robustStopOutcome{Parked: aimed}
}

// stepRobustStop re-sends or retires id's owed STOP and answers whether one is
// still owed against a live session. Only the two ticks call it, once per id per
// tick; fanout re-resolves a fan-out's targets with the caller's lock already
// held. The record is cleared compare-and-clear, so a STOP a handler armed while
// this was judging is not wiped.
func (s *apiServer) stepRobustStop(id string, now float64, fanout func() []string) bool {
	rs, ok := s.robustStopOf(id)
	if !ok {
		return false
	}
	alive := s.robustStopAimAlive(id, rs)
	switch robustStopStepOf(rs, alive, s.reconcileConfigLive().StopRetry, now) {
	case robustStopDone:
		s.robustStopMu.Lock()
		if s.robustStops[id] == rs {
			delete(s.robustStops, id)
		}
		s.robustStopMu.Unlock()
		return false
	case robustStopResend:
		targets := []string{rs.Target}
		if rs.Target == "" {
			targets = fanout()
		}
		if rs.Landed {
			reconcileLog("robust stop %s: session still live past stop_retry (aimed at %q) — "+
				"the kill did not take, re-dispatching", id, rs.Target)
		}
		s.sendRobustStop(id, targets, now)
	}
	return s.robustStopOwed(id)
}

// robustStopOwed: a STOP is out and the session it aimed at still runs. The
// deciders hold START and convergence while this is true; the re-send is the
// tick's stepRobustStop, never the decider.
func (s *apiServer) robustStopOwed(id string) bool {
	rs, ok := s.robustStopOf(id)
	return ok && s.robustStopAimAlive(id, rs)
}

// closeRobustStopOnNoSuchSession: a no_such_session receipt FROM THE AIMED
// MACHINE proves the kill arrived and found nothing, which presence cannot (a
// stale SSE claim can keep the id looking alive). The machine match is
// load-bearing: an identity sweep broadcasts stop to every warden and the
// uninvolved ones answer no_such_session routinely. reporter "" is unknown.
func (s *apiServer) closeRobustStopOnNoSuchSession(id, reporter string) {
	if id == "" || reporter == "" {
		return
	}
	s.robustStopMu.Lock()
	rs, ok := s.robustStops[id]
	closed := ok && rs.Target == reporter
	if closed {
		delete(s.robustStops, id)
	}
	s.robustStopMu.Unlock()
	if closed {
		reconcileLog("robust stop %s: %s reported no_such_session — the kill reached the "+
			"machine it was aimed at and found nothing to kill; retry disarmed", id, reporter)
	}
}

// disarmRobustStopOnStart: a START landed on warden begins a new session there,
// and a later re-send would shoot it. A STOP aimed at another machine stays owed
// (換機器: the old box still owes a dead session); a fan-out is disarmed by any
// landed START, since its re-send re-resolves the chain to the new session's
// machine.
// Rests on: every session is created by a START that passes through here. A
// boot path that bypasses it (e.g. a warden auto-revive) would need the record
// tied to a session generation instead.
func (s *apiServer) disarmRobustStopOnStart(id, warden string) {
	s.robustStopMu.Lock()
	defer s.robustStopMu.Unlock()
	if rs, ok := s.robustStops[id]; ok && (rs.Target == "" || rs.Target == warden) {
		delete(s.robustStops, id)
	}
}
