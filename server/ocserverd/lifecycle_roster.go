package main

// lifecycle_roster.go — the ONE place the staff/outsource difference may be
// spelled for the lifecycle tick (外包＝正職, migration 00025): lifecyclePolicyFor
// is the entry filter, lifecycleRosterPasses the one ordered list of pre-decide
// roster formalities every tick producer shares (lifecycleTickProducers is the
// authority on which producers exist). A pass for only some rows must say so in
// AppliesTo, which the parity test reads back by name. Before adding a roster
// loop to any producer, or a kind branch anywhere in this package, read the
// header of the file holding TestTickProducersHaveNoUndeclaredRosterLoop.

// LifecyclePolicy is deliberately one field wide: the retirement half (worker
// release on task close, staff dismissal) is not converged here — doing so is an
// owner-visible behaviour change. The omission is a decision.
type LifecyclePolicy struct {
	ShouldExist func() bool
}

// lifecyclePolicyFor: a warden is excluded unless it is being uninstalled — it
// is never a spawn/stop candidate, it is the thing that executes them. The staff
// arm reads roster_status, not the role table: it was extracted from the
// existing call sites and must not quietly become stricter.
func lifecyclePolicyFor(m Member) LifecyclePolicy {
	if m.Kind == KindOutsource {
		return LifecyclePolicy{ShouldExist: func() bool {
			return workerStatusFrom(m.RosterStatus, m.ActivatedTS) == WorkerStatusActive &&
				m.DesiredState != DesiredStateOffline
		}}
	}
	return LifecyclePolicy{ShouldExist: func() bool {
		if m.RosterStatus != RosterStatusActive {
			return false
		}
		if m.Kind == KindWarden && parseDesired(m.DesiredState) != DesiredStateUninstall {
			return false
		}
		return true
	}}
}

// The values are the producers' own function names, so a parity failure prints
// a greppable name.
type lifecycleDriver string

const (
	driverReconcile lifecycleDriver = "runReconcileTick"
	driverOutsource lifecycleDriver = "runOutsourceTick"
	// driverNone is never a legitimate answer: it makes a non-exhaustive
	// lifecycleTickDriverFor fail loudly in the parity test as "claimed by NEITHER
	// half".
	driverNone lifecycleDriver = ""
)

// lifecycleTickDriverFor decides which half of the merged lifecycle tick owns a
// row — exactly one, always. 🔴 Load-bearing: it replaced the
// `WHERE kind != 'outsource'` clause that used to keep a row out of both FSMs;
// without it one active desired-online worker row was measured taking a `start`
// from enqueueWardenFrame AND from notifyWorkerSpawn in the same tick.
// Deliberately not narrower than that SQL: EVERY outsource row (assigned, active,
// released, held down or not) belongs to the outsource half, which declines rows
// inside its own switch; they must never fall through to the reconcile half.
func lifecycleTickDriverFor(m Member) lifecycleDriver {
	if m.Kind == KindOutsource {
		return driverOutsource
	}
	return driverReconcile
}

const (
	lifecyclePassContextHigh     = "context_high_recycle"
	lifecyclePassTokenExpiry     = "token_expiry_winddown"
	lifecyclePassStaleStopping   = "stale_stopping_clear"
	lifecyclePassUninstallIntent = "uninstall_intent_consume"
)

type lifecycleRosterPass struct {
	Name      string
	AppliesTo func(m Member) bool
	Run       func(roster []Member, now float64)
}

func lifecycleEveryKind(Member) bool { return true }

// lifecycleRosterPasses — order is load-bearing: context thresholds BEFORE token
// expiry. Both stamp passes skip a row already carrying refocus_since; reversed,
// a session both out of context and near token expiry is stamped token_expiry,
// canPromoteToAcceleratedStop (which only promotes a context_notice epoch)
// declines it, and the second context threshold never opens its 加速停止.
func (s *apiServer) lifecycleRosterPasses() []lifecycleRosterPass {
	return []lifecycleRosterPass{
		{
			Name:      lifecyclePassContextHigh,
			AppliesTo: lifecycleEveryKind,
			Run:       s.stampContextHighRecycle,
		},
		{
			Name:      lifecyclePassTokenExpiry,
			AppliesTo: lifecycleEveryKind,
			Run:       s.stampTokenExpiryWinddown,
		},
		{
			Name:      lifecyclePassStaleStopping,
			AppliesTo: lifecycleEveryKind,
			Run:       s.clearStaleStoppingOnOnline,
		},
		{
			Name:      lifecyclePassUninstallIntent,
			AppliesTo: func(m Member) bool { return m.Kind == KindWarden },
			Run:       func(roster []Member, _ float64) { s.consumeUninstallIntentOnOffline(roster) },
		},
	}
}

// runLifecycleRosterPasses: the passes mutate their sub-slice in place and then
// persist; the copy-back makes the rest of the same tick decide off the stamped
// rows — otherwise every collect is one cadence period late.
func (s *apiServer) runLifecycleRosterPasses(roster []Member, now float64) {
	for _, p := range s.lifecycleRosterPasses() {
		idx := make([]int, 0, len(roster))
		sub := make([]Member, 0, len(roster))
		for i := range roster {
			if p.AppliesTo(roster[i]) {
				idx = append(idx, i)
				sub = append(sub, roster[i])
			}
		}
		if len(sub) == 0 {
			continue
		}
		p.Run(sub, now)
		for j, i := range idx {
			roster[i] = sub[j]
		}
	}
}

// runWorkerLifecyclePasses is the outsource producer's single door into the list
// above. Only the four wind-down fields are folded back, never the whole
// projection: workerFromMember re-derives Status from activated_ts.
//
// 🔴 The fold-back is never persisted: for the rest of the tick
// StoppingSince/StoppedSince exist ONLY on the caller's slice. Readers include
// values that travel under other names — AgentStopped (workerObservation,
// worker_spawn.go) and the positional stoppedSince that workerHasStateToFlush
// passes into hasUncollectedOnlineOwnerOpState — so grepping the field names
// misses them. The wind-down suite's helper workerTickPass re-reads the row from
// the DAL, so it never observes this fold-back: green after deleting these lines
// proves nothing.
//
// The door admits more than the worker vocabulary's "active": memberFromWorker
// stamps ActivatedTS for an active row whose ActivatedTS is 0 and keeps
// ActivatedTS>0 for an unrecognised Status, so both read ACTIVE here while the
// tick's own `switch w.Status` disagrees. Know this before widening it.
//
// Callers hold s.outsourceMu.
func (s *apiServer) runWorkerLifecyclePasses(workers []OutsourceWorker, now float64) {
	roster := make([]Member, 0, len(workers))
	index := make([]int, 0, len(workers))
	for i := range workers {
		m := memberFromWorker(workers[i])
		if !lifecyclePolicyFor(m).ShouldExist() {
			continue
		}
		roster = append(roster, m)
		index = append(index, i)
	}
	if len(roster) == 0 {
		return
	}
	s.runLifecycleRosterPasses(roster, now)
	for j, i := range index {
		workers[i].RefocusSince = roster[j].RefocusSince
		workers[i].RefocusOp = roster[j].RefocusOp
		workers[i].StoppingSince = roster[j].StoppingSince
		workers[i].StoppedSince = roster[j].StoppedSince
	}
}
