package main

import "time"

// The 30 s value is the `cadence` row of spec/lifecycle.md §4.4's timers table.
const lifecycleCadenceSecs = 30.0

// runLifecycleTick is the one producer tick: the reconcile half (正職: entry
// filter → shared pre-decide formalities → receipt sweep → decide→dispatch),
// then the outsource half (外包: snapshot → the same formalities through the
// worker projection → FSM pass → decide→mint/bind/fan).
//
// 🔴 Locks are held in sequence, never both at once (owner ruling, T-14): each
// half takes its own mutex in its own body; nothing here holds either.
//
// 🔴 The halves' roster reads are disjoint only because of the driver guard
// (lifecycleTickDriverFor) at the head of runReconcileTick's candidate loop —
// ListMembers returns the whole member table, so deleting that guard makes both
// halves drive the same rows. The one shared row set is a write:
// sweepLapsedReceipts → stampReceiptMissing (reconcile half) stamps an
// outsource_worker row that the outsource half reads later in the same tick.
// The sweep stays in the reconcile half, which owns the deadline state.
//
// 🔴 The kill switches are checked here, never inside the halves' bodies. Many
// tests set noOutsource = true so no cadence races them, then drive
// runOutsourceTick by hand; a flag read inside it would make those tests silent
// no-ops, and the ones asserting "no SECOND worker appeared" would pass while
// checking nothing.
//
// 🔴 `now` is sampled once per tick and shared by both halves on purpose: the
// outsource half's deadlines are judged against a slightly early clock, so they
// can fire one tick late (costs one period) but never early (would kill a live
// session), and a receipt stamped by one half and read by the other cannot look
// written in the future. Do not re-read the clock between the halves.
func (s *apiServer) runLifecycleTick(now float64) {
	if !s.noReconcile {
		s.runReconcileTick(now)
	}
	// Each half recovers its own panic. A HANG is not recovered: a blocked
	// reconcile half stops outsource assignment entirely — no timeout, no
	// watchdog bounds this wait.
	if !s.noOutsource {
		s.runOutsourceTick(now)
	}
}

func (s *apiServer) startLifecycleCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			surviveLockInTx("lifecycle cadence", func() { s.runLifecycleTick(nowSecs()) })
		}
	}()
	reconcileLog("lifecycle cadence started (period=%gs, reconcile=%v, outsource=%v)",
		period.Seconds(), !s.noReconcile, !s.noOutsource)
}
