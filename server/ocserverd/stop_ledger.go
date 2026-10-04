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
	// Target is the one machine the kill chain named; "" for a fan-out, which is
	// re-resolved through the kill chain on every re-send. A named stop that also
	// reaches a booting START's machine has two targets and is recorded as a
	// fan-out too: no_such_session no longer retires it and any landed START does.
	Target string
	At     float64
	// Landed false means parked: no warden took the frame (the named one refused,
	// or every one a fan-out reached did, or there was none to reach), and the
	// tick re-fires it every pass until a warden takes it.
	Landed bool
	// LoggedAt is when this park was last logged: a parked STOP is re-fired every
	// tick, possibly for hours, and must not log every tick.
	LoggedAt float64
}

const robustStopParkLogSecs = 60.0

type robustStopOutcome struct {
	Landed []string
	// Parked is the named machine a refused STOP is parked on. A parked fan-out
	// leaves it "": the STOP is owed, but no machine has it yet.
	Parked string
}

// reached: the machines that have the STOP or are owed it by name. Empty means
// the session was not killed now and nobody yet knows where it will be — the
// ledger still re-fires it, but a caller about to start a replacement must not
// count it as stopped.
func (o robustStopOutcome) reached() []string {
	if o.Parked == "" {
		return o.Landed
	}
	return append(append([]string(nil), o.Landed...), o.Parked)
}

// robustStopEffect is what a caller may conclude from a robust STOP it just sent.
// The collect is not in it: a close-out collect stands whatever the outcome, for
// staff and outsource workers alike, since the ledger keeps re-firing the STOP.
type robustStopEffect struct {
	// ClearBootTS: a session boundary may be recorded. With nothing reached no
	// session ended yet, and dropping boot_ts would fail restart_self's
	// minimum-liveness gate and the boot-storm guard OPEN.
	ClearBootTS bool
	// SupersedeStart: the deciders may stop waiting on the in-flight START.
	SupersedeStart bool
}

// robustStopEffectOf is THE judgement over a robust STOP's outcome, both
// populations. startTarget is the in-flight START's machine, "" for none.
func robustStopEffectOf(out robustStopOutcome, startTarget string) robustStopEffect {
	reached := out.reached()
	return robustStopEffect{
		ClearBootTS:    len(reached) > 0,
		SupersedeStart: stopReachedStart(startTarget, reached),
	}
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

// sendRobustStop sends a robust STOP and records it, whatever became of it: a
// STOP that left the server's hands owes the session's death until the session
// is gone, and the ledger is the only thing that remembers. fanout says targets
// is the kill chain's broadcast rather than the one machine it named — even a
// broadcast that happened to list a single online warden. Takes no scheduler
// lock.
func (s *apiServer) sendRobustStop(id string, targets []string, fanout bool, now float64) robustStopOutcome {
	return s.sendRobustStopOver(id, targets, fanout, now, nil)
}

// sendRobustStopOver with over non-nil is the tick's re-send of the record it
// read: written only if that record is still the one on file, so a STOP a
// handler recorded meanwhile (or a START's disarm) is not clobbered.
func (s *apiServer) sendRobustStopOver(id string, targets []string, fanout bool, now float64, over *robustStop) robustStopOutcome {
	aimed := ""
	if !fanout && len(targets) == 1 {
		aimed = targets[0]
	}
	send := targets
	if over != nil {
		// A re-send skips wardens that would refuse anyway: the fail-closed gate
		// logs every refusal, and a parked STOP is re-fired every tick.
		send = s.onlineOnly(targets)
	}
	landed := s.sendStopFrames(id, send, now)
	rec := robustStop{Target: aimed, At: now, Landed: len(landed) > 0}
	logPark := !rec.Landed
	if logPark && over != nil && !over.Landed && over.Target == aimed {
		rec.LoggedAt = over.LoggedAt
		logPark = now-over.LoggedAt >= robustStopParkLogSecs
	}
	if logPark {
		rec.LoggedAt = now
	}
	s.robustStopMu.Lock()
	if cur, ok := s.robustStops[id]; over == nil || (ok && cur == *over) {
		s.robustStops[id] = rec
	}
	s.robustStopMu.Unlock()
	switch {
	case rec.Landed:
		return robustStopOutcome{Landed: landed}
	case aimed == "":
		if logPark {
			reconcileLog("robust stop %s: no warden took the fan-out (targets %v) — parked, "+
				"the tick re-resolves the kill chain and re-fires it", id, targets)
		}
		return robustStopOutcome{}
	}
	if logPark {
		reconcileLog("robust stop %s: target %s unreachable — parked, the tick re-fires it", id, aimed)
	}
	return robustStopOutcome{Parked: aimed}
}

func (s *apiServer) onlineOnly(targets []string) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if s.hub.IsOnline(t) {
			out = append(out, t)
		}
	}
	return out
}

// stepRobustStop re-sends or retires id's owed STOP and answers whether one is
// still owed against a live session. Only the two ticks call it, once per id per
// tick; fanout re-resolves a fan-out's targets with the caller's lock already
// held. The record is cleared and rewritten compare-and-set, so a STOP a handler
// armed while this was judging is neither wiped nor overwritten.
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
		fan := rs.Target == ""
		targets := []string{rs.Target}
		if fan {
			targets = fanout()
		}
		if rs.Landed {
			reconcileLog("robust stop %s: session still live past stop_retry (aimed at %q) — "+
				"the kill did not take, re-dispatching", id, rs.Target)
		}
		s.sendRobustStopOver(id, targets, fan, now, &rs)
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
