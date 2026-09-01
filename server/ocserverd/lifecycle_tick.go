package main

import "time"

// lifecycleCadenceSecs is THE producer tick period (§4.1 / §B.4) — one number
// where reconcileCadenceSecs and outsourceCadenceSecs used to be two, both
// 30.0, both meaning "how often the producer looks". It is a scan period, not
// a member-presence heartbeat.
const lifecycleCadenceSecs = 30.0

// ── THE lifecycle cadence tick (T-14 item 5) ─────────────────────────────────
//
// There used to be TWO cadence loops on the same 30s period: one mounting
// runReconcileTick (正職), one mounting runOutsourceTick (外包). That is the
// same defect shape T-170e spent three stages removing everywhere else — two
// hand-maintained producers, each reaching one population, and no structural
// reason for a formality added to one to reach the other. Two loops also meant
// the ORDER between the halves was whatever the two goroutines happened to do
// this second, so "the receipt sweep runs before the decide pass" was only ever
// true within one half.
//
// One cadence, one tick, one order.
//
// 🔴 THE LOCKS ARE HELD IN SEQUENCE, NEVER BOTH AT ONCE (owner ruling, T-14).
// Today ZERO goroutines in this package hold reconcileMu and outsourceMu at the
// same time — reconcile.go's lock-order note says so at length, and api_stub.go
// documents the two producers as lock-disjoint. If this tick ran both halves
// inside one locked region it would become the codebase's ONLY double-holder:
// deadlock-free today only because no path acquires them in the opposite order,
// and nothing at all would keep that true tomorrow. So: lock A → run A → drop →
// lock B → run B → drop. Each half takes its own mutex in its own body; nothing
// here holds either.
//
// 🔴 THE HALVES ARE NOT COUPLED, AND THIS IS WHY IT IS SAFE TO SEQUENCE THEM.
// Their inputs are disjoint row sets (the reconcile half reads ListMembers,
// which is `WHERE kind != 'outsource'` by construction — dal.go; the outsource
// half reads tasks/workers/manuals/deps), and no value computed by one half is
// read by the other: both work entirely in their own locals. The one function
// that touches BOTH populations is sweepLapsedReceipts → stampReceiptMissing,
// whose outsource arm writes a worker row. It belongs to the RECONCILE half and
// stays there — the receipt namespace is shared by design (a worker's
// start/stop rides the member verbs), and moving it would either split one
// sweep in two or hand it to a half that does not own the deadline state.
//
// 🔴 THE KILL SWITCHES GATE THE HALVES, NOT THIS ENTRY — and NOT the halves'
// own bodies. --no-reconcile and --no-outsource used to work by simply not
// mounting a cadence loop; with one loop, the skip has to happen per half, and
// WHERE it happens is load-bearing:
//
//   - runOutsourceTick itself must keep NOT reading s.noOutsource. ~98 call
//     sites across ~18 test files set api.noOutsource = true precisely so the
//     cadence cannot race them, and then drive the scheduler by calling
//     runOutsourceTick by hand. A flag read at the top of that function (or at
//     the top of runLifecycleTick, if the tests were pointed here) turns every
//     one of those into a silent no-op. The tests asserting "a worker appeared"
//     would go red and be noticed; the ones asserting "no SECOND worker
//     appeared" would go GREEN while checking nothing at all. That asymmetry —
//     half the suite failing loudly and the other half failing invisibly — is
//     what makes this the expensive place to be clever.
//
// TestLifecycleTickHalvesAreGatedAtTheCallSite pins both directions.
func (s *apiServer) runLifecycleTick(now float64) {
	// The reconcile half — THE entry filter, the shared pre-decide formalities,
	// the receipt sweep, then decide→dispatch per candidate. Holds reconcileMu
	// for its whole body and has dropped it before the next line runs.
	if !s.noReconcile {
		s.runReconcileTick(now)
	}
	// The outsource half — snapshot, the same shared formalities through the
	// worker projection, the FSM pass, then decide→mint/bind/fan. Holds
	// outsourceMu for its whole body.
	//
	// A panic in the first half does not cost the second one its tick: each half
	// recovers inside its OWN deferred func, so the fault is logged there and
	// control returns here normally.
	if !s.noOutsource {
		s.runOutsourceTick(now)
	}
}

// startLifecycleCadence mounts the always-on producer loop (§4.1) — one
// goroutine for BOTH halves, replacing startReconcileCadence and
// startOutsourceCadence, which ran the same 30s period side by side.
// Sleep-then-tick: the first tick fires one full period after start, matching
// the asyncio cadence both predecessors copied.
//
// It is mounted unconditionally. A serve run with both kill switches set gets a
// loop whose body is two `if` tests every 30s, which is cheaper than a second
// mount decision to keep correct — and it means the flags have exactly ONE
// meaning (skip that half's work), not two (skip the work, and also maybe skip
// the loop).
func (s *apiServer) startLifecycleCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.runLifecycleTick(nowSecs())
		}
	}()
	reconcileLog("lifecycle cadence started (period=%gs, reconcile=%v, outsource=%v)",
		period.Seconds(), !s.noReconcile, !s.noOutsource)
}
