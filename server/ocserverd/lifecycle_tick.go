package main

import "time"

// lifecycleCadenceSecs is THE producer tick period (spec/lifecycle.md §4.1; the
// 30 s value itself is the `cadence` row of the §4.4 timers table) — one number
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
// 🔴 THE HALVES SHARE NO LOCALS AND NO LOCKS — AND THERE IS EXACTLY ONE SHARED
// ROW SET, WHICH IS NAMED HERE. Their ROSTER READS are disjoint: the reconcile
// half reads ListMembers and drops every row lifecycleTickDriverFor sends to the
// other half; the outsource half reads tasks/workers/manuals/deps. ⚠️ THE
// DISJOINTNESS IS NO LONGER A PROPERTY OF THE QUERY. ListMembers used to be
// `WHERE kind != 'outsource'` and is now the whole member table (T-14 項目 6), so
// what keeps these two reads disjoint is the driver guard at the head of
// runReconcileTick's candidate loop — delete it and the halves stop being
// disjoint at all, which is the measured double-drive on lifecycleTickDriverFor. No value
// computed by one half is read by the other — both work entirely in their own
// locals, and the only thing passed between them is `now` (see runLifecycleTick
// below).
//
// "Their inputs are disjoint row sets" would be FALSE as a flat statement, so
// it is not made. The exception is a WRITE, not a read, and it is exactly one:
// sweepLapsedReceipts → stampReceiptMissing runs in the RECONCILE half, and its
// outsource arm stamps an outsource_worker row — a row the outsource half then
// reads later in the same tick. What is true is narrower and sufficient: the
// halves share no in-memory state and no mutex, and the single shared row set is
// now touched in a FIXED order (reconcile stamps, then outsource reads).
//
// 🔴 THAT OVERLAP IS INHERITED FROM main, NOT INTRODUCED BY THIS MERGE. Before
// the merge the two goroutines reached that same row set with no defined order
// at all; sequencing them makes the order deterministic rather than creating
// the coupling. The sweep belongs to the RECONCILE half and stays there — the
// receipt namespace is shared by design (a worker's start/stop rides the member
// verbs), and moving it would either split one sweep in two or hand it to a half
// that does not own the deadline state.
//
// 🔴 THE KILL SWITCHES GATE THE HALVES, NOT THIS ENTRY — and NOT the halves'
// own bodies. --no-reconcile and --no-outsource used to work by simply not
// mounting a cadence loop; with one loop, the skip has to happen per half, and
// WHERE it happens is load-bearing:
//
//   - runOutsourceTick itself must keep NOT reading s.noOutsource. The numbers
//     below are MEASURED, not estimated — run from server/ocserverd/ on this
//     branch, and re-runnable by anyone who doubts them:
//
//     169 EXECUTABLE sites set `noOutsource = true` in this package's tests
//     $ grep -rn 'noOutsource *= *true' --include='*_test.go' . \
//     | grep -vE ':[0-9]+:[[:space:]]*//' | wc -l
//     34 test files hold at least one of them
//     $ grep -rn 'noOutsource *= *true' --include='*_test.go' . \
//     | grep -vE ':[0-9]+:[[:space:]]*//' | cut -d: -f1 | sort -u | wc -l
//     28 top-level test funcs set the flag AND call runOutsourceTick( in the
//     same body — the ones provably driving the scheduler by hand
//     $ awk '/^func /{fn=FILENAME"::"$0}
//     {l=$0; sub(/^[ \t]+/,"",l)}
//     l !~ /^\/\// && /noOutsource[ ]*=[ ]*true/ {flag[fn]=1}
//     l !~ /^\/\// && /runOutsourceTick\(/      {drive[fn]=1}
//     END{for(f in flag) if (f in drive) n++; print n}' *_test.go
//
//     The `grep -vE` is not decoration: without it the count picks up the
//     comment lines that DESCRIBE the count (this block, and this file's twin
//     in lifecycle_tick_test.go), which is how the earlier "~98" grew a
//     spurious extra. One further site sets the flag to FALSE and is correctly
//     outside the 169.
//
//     Read 28 as a FLOOR, not the blast radius: the other 141 sites set the
//     flag so no cadence can race them and then reach the scheduler through
//     handlers, so a flag read inside the tick body changes their behaviour
//     too — 28 is only what is mechanically provable from a single function
//     body.
//
//     A flag read at the top of that function (or at the top of
//     runLifecycleTick, if the tests were pointed here) turns every
//     one of those into a silent no-op. The tests asserting "a worker appeared"
//     would go red and be noticed; the ones asserting "no SECOND worker
//     appeared" would go GREEN while checking nothing at all. That asymmetry —
//     half the suite failing loudly and the other half failing invisibly — is
//     what makes this the expensive place to be clever.
//
// TestLifecycleTickHalvesAreGatedAtTheCallSite pins both directions.
//
// 🔴 ONE CLOCK READ FOR BOTH HALVES, AND THAT IS DELIBERATE. `now` is sampled
// ONCE — in startLifecycleCadence, before this function is entered — and the
// same value is handed to both halves. Before the merge each goroutine called
// nowSecs() for itself, so the two halves saw timestamps that differed by
// however long the reconcile half had been running. Sharing it is a real
// behaviour change, and the direction it moves in is the conservative one:
// every deadline in the outsource half (grace windows, stop retries, token
// expiry leads, reclaim backstops) is now evaluated against a clock that is
// SLIGHTLY EARLIER than wall time rather than later, so a deadline can only be
// judged not-yet-lapsed one tick sooner than before — never lapsed one tick
// early. Firing late costs one 30 s period; firing early would kill a live
// session. The skew is bounded by one reconcile half's runtime, which is far
// below every timer in §4.4.
//
// It is ALSO what makes "one tick" mean anything: the two halves stamp the same
// instant, so a receipt the reconcile half stamps and the outsource half reads
// in this tick cannot appear to have been written in the future. Do not "fix"
// this by re-reading the clock between the halves.
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
	//
	// 🔴 A PANIC IS RECOVERED; A HANG IS NOT. This is the availability cost the
	// merge buys the ordering with, and it is new: the outsource half's real
	// period is now `lifecycleCadenceSecs + however long the reconcile half
	// took`, so a reconcile half that blocks (a wedged DAL call, a lock held by
	// something else) stops outsource ASSIGNMENT entirely, where two goroutines
	// would have kept it running. Nothing here bounds that wait — no timeout, no
	// watchdog. If the reconcile half ever grows a call that can block for an
	// unbounded time, this is where that becomes an outage.
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
			// ONE nowSecs() per tick, shared by both halves on purpose — the
			// reasoning is on runLifecycleTick above. Do not move this read
			// inside the halves.
			s.runLifecycleTick(nowSecs())
		}
	}()
	reconcileLog("lifecycle cadence started (period=%gs, reconcile=%v, outsource=%v)",
		period.Seconds(), !s.noReconcile, !s.noOutsource)
}
