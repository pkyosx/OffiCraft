package main

import "testing"

// T-72dd 裁定一 — reconcileWorkerLiveness's STOP arm serves TWO different
// STOPs, and only ONE of them may bench the machine.
//
// BOTH directions are asserted in one test on purpose. Asserting only "recycle
// does not bench" would stay green if benching were deleted outright, which
// would silently un-fix the O-19 ghost wedge the takeover bench exists for;
// asserting only the takeover would stay green on today's bug. The pair is the
// claim.
func TestWorkerStopArm_OnlyZombieTakeoverBenchesTheMachine_T72dd(t *testing.T) {
	// ── direction A: the RECYCLE collect must NOT bench ──────────────────────
	t.Run("recycle collect leaves the machine usable", func(t *testing.T) {
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		now := 1_000_000.0
		w := fsmWorkerFixture(t, s, "ow-rk", WorkerStatusActive, now-5_000)
		w.DesiredState = DesiredStateOnline
		w.RefocusSince = now - 5_000
		w.RefocusOp = refocusOpRefocus
		w.StoppedSince = now - 10 // the agent filed its dump-done
		putWorkerFixture(t, s, w)
		if _, err := s.hub.Connect("ow-rk", ""); err != nil {
			t.Fatalf("connect: %v", err)
		}

		s.outsourceMu.Lock()
		s.workerSpawnTarget["ow-rk"] = ServerSelfHost
		s.reconcileWorkerLiveness(w, now)
		benched := s.workerMachineCoolingOn("ow-rk", ServerSelfHost, now)
		s.outsourceMu.Unlock()

		frames := s.hub.DrainWardenCommands(ServerSelfHost)
		if got := countStops(t, frames); got != 1 {
			t.Fatalf("fixture: the recycle collect must dispatch exactly one stop, "+
				"got %d in %d frame(s)", got, len(frames))
		}
		if benched {
			t.Fatal("a 換手 collect must NOT bench its machine — the respawn is " +
				"supposed to land on the SAME machine, and cooling it means the " +
				"worker is killed and then cannot come back until the cooldown lapses")
		}
	})

	// ── direction B: the ZOMBIE TAKEOVER must STILL bench ────────────────────
	t.Run("zombie takeover still benches the wedged slot", func(t *testing.T) {
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		now := 1_000_000.0
		w := fsmWorkerFixture(t, s, "ow-zb", WorkerStatusAssigned, now-500)
		w.LastOp = reconcileCmdStart
		w.LastOpReason = spawnClobberReasonPrefix + ": live session refused clobber"
		putWorkerFixture(t, s, w)
		// Same as the two zombie-takeover fixtures in worker_spawn_test.go: the
		// value carries the receipt into the code under test, the row is planted
		// through the sole writer so it does not silently hold nothing (T-55).
		if err := s.dal.SetMemberOpReceipt("ow-zb", w.LastOp, w.LastOpOK, w.LastOpLog,
			w.LastOpReason, w.LastOpAt); err != nil {
			t.Fatalf("seed the clobber receipt: %v", err)
		}

		s.outsourceMu.Lock()
		s.workerSpawnTarget["ow-zb"] = ServerSelfHost
		s.workerReconcileStates["ow-zb"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: now - 10,
			OfflineSince:  now - s.reconcileCfg.ZombieConfirmGrace - 1,
		}
		s.reconcileWorkerLiveness(w, now)
		benched := s.workerMachineCoolingOn("ow-zb", ServerSelfHost, now)
		s.outsourceMu.Unlock()

		frames := s.hub.DrainWardenCommands(ServerSelfHost)
		if got := countStops(t, frames); got != 1 {
			t.Fatalf("fixture: the takeover must dispatch exactly one stop, got %d", got)
		}
		if !benched {
			t.Fatal("the zombie takeover MUST still bench: that slot holds a ghost " +
				"the clobber-guard refused to overwrite, so a respawn onto it would " +
				"bounce off the same ghost (the O-19 wedge)")
		}
	})
}
