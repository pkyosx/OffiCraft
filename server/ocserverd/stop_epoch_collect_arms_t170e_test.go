package main

import "testing"

// stop_epoch_collect_arms_t170e_test.go — T-170e stage 2 ②, the two sites the
// first pass did not count.
//
// gracefulStopEpochOpen's own comment used to enumerate THREE things a graceful
// stop epoch entitles a session to (the sentence, the clock, the escalation).
// It was wrong by two: worker_spawn.go asks the same two-term question in two
// more places, and both had written it out by hand.
//
//   - autoHandoverWorker's 停止 arm — the collect for the cases no report can
//     ever answer (session confirmed gone, or the owner's 加速停止 deadline).
//   - workerReportStopped's 停止 arm — the collect a worker's own report drives.
//
// Both now call gracefulStopEpochOpen. These are their sentinels, and they are
// NEW: seeding the forced term out of either site left the whole selected suite
// GREEN before this file existed, so what those two copies excluded was
// decoration exactly the way the 加速停止 worker face was.
//
// What both pin is one rule: A FORCED EPOCH HAS NOTHING TO COLLECT. Force-stop
// already killed the session, so a second kill dispatched from a collect arm is
// aimed at whatever is on that host NOW. Each test carries its own POSITIVE
// CONTROL — the same fixture with the forced anchor removed must still be
// collected — so a guard that removed the arm rather than scoping it cannot
// pass as a guard that scoped it.

// TestGracefulStopEpochOpen_TheTickDoesNotCollectAForcedWorker pins
// autoHandoverWorker's 停止 arm. The branch driven here is the CONFIRMED-GONE
// one, deliberately: it does not read refocus_op at all, so it is the arm where
// the forced exclusion is the only thing standing between a cut-off session and
// a second kill.
func TestGracefulStopEpochOpen_TheTickDoesNotCollectAForcedWorker(t *testing.T) {
	const opened = 1_000.0

	// gone seeds an ACTIVE, desired-offline worker with an OPEN stop epoch whose
	// session the de-bounce has already confirmed gone, and never connects it to
	// the hub. forcedAt > 0 makes the epoch a forced one.
	gone := func(t *testing.T, id string, forcedAt float64) (*apiServer, float64) {
		t.Helper()
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		now := nowSecs()
		w := fsmWorkerFixture(t, s, id, WorkerStatusActive, now-50_000)
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = opened
		w.StoppedSince = 0
		w.ForcedStopAt = forcedAt
		putWorkerFixture(t, s, w)
		s.workerSpawnTarget[id] = ServerSelfHost
		// The de-bounce anchor, aged past the confirm grace: one instantaneous
		// offline sample is NOT what this arm fires on (T-ed79 #13).
		s.workerOfflineSince[id] = now - workerOfflineConfirmGraceSecs - 1
		s.hub.DrainWardenCommands(ServerSelfHost) // ignore fixture noise
		s.outsourceMu.Lock()
		s.autoHandoverWorker(w, now)
		s.outsourceMu.Unlock()
		return s, now
	}

	s, _ := gone(t, "ow-170e-tick-forced", opened)
	if got := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a CUT OFF worker was collected again: %d stop(s) — force-stop "+
			"already dispatched the kill, so this arm has nothing to wait for and "+
			"nothing to end. The staff-facing half of this same question is the "+
			"\"already cut off by 強制停止\" case of "+
			"TestAcceleratedStop_RefusesAMemberNobodyHasAskedToStop; both read "+
			"gracefulStopEpochOpen", got)
	}
	if got, _ := s.dal.GetOutsourceWorker("ow-170e-tick-forced"); got.StoppedSince != 0 {
		t.Fatalf("stopped_since=%v, want 0 — a forced epoch is not collected here",
			got.StoppedSince)
	}

	// POSITIVE CONTROL: the same confirmed-gone fixture, GRACEFUL, is still
	// collected. Without this the test above would also pass if the arm were
	// dead.
	t.Run("a graceful stop epoch is still collected", func(t *testing.T) {
		s, _ := gone(t, "ow-170e-tick-soft", 0)
		if got := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
			t.Fatalf("a graceful stop whose session is confirmed gone must be "+
				"collected exactly once, got %d stop(s)", got)
		}
		if got, _ := s.dal.GetOutsourceWorker("ow-170e-tick-soft"); got.StoppedSince <= 0 {
			t.Fatalf("the collect must latch stopped_since, got %v", got.StoppedSince)
		}
	})
}

// TestGracefulStopEpochOpen_AForcedWorkersOwnReportIsLatchedNotCollected pins
// workerReportStopped's 停止 arm. A report CAN arrive after a force-stop — the
// worker may have filed it in the same breath as the kill — and what it must do
// is latch the record and dispatch NOTHING.
func TestGracefulStopEpochOpen_AForcedWorkersOwnReportIsLatchedNotCollected(t *testing.T) {
	const opened = 1_000.0

	reports := func(t *testing.T, id string, forcedAt float64) *apiServer {
		t.Helper()
		s := newWorkerTestServer(t)
		connectWarden(t, s, ServerSelfHost)
		now := nowSecs()
		w := fsmWorkerFixture(t, s, id, WorkerStatusActive, now-50_000)
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = opened
		w.StoppedSince = 0
		w.ForcedStopAt = forcedAt
		putWorkerFixture(t, s, w)
		s.workerSpawnTarget[id] = ServerSelfHost
		s.hub.DrainWardenCommands(ServerSelfHost) // ignore fixture noise
		if _, _, err := s.workerReportStopped(id, triggerServer); err != nil {
			t.Fatalf("report_stopped: %v", err)
		}
		return s
	}

	s := reports(t, "ow-170e-report-forced", opened)
	if got := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a CUT OFF worker's own report dispatched %d stop(s) — the kill "+
			"for this epoch already went out; this report is a RECORD, not a "+
			"trigger", got)
	}
	if got, _ := s.dal.GetOutsourceWorker("ow-170e-report-forced"); got.StoppedSince <= 0 {
		t.Fatalf("the report must still be latched durably, got stopped_since=%v — "+
			"silence here is about the KILL, not about the record", got.StoppedSince)
	}

	// POSITIVE CONTROL: the same report on a GRACEFUL stop epoch IS the collect.
	t.Run("a graceful stop epoch's report is the collect", func(t *testing.T) {
		s := reports(t, "ow-170e-report-soft", 0)
		if got := countStops(t, s.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
			t.Fatalf("a graceful 停止 is collected by the worker's own report — "+
				"exactly one kill, got %d stop(s)", got)
		}
	})
}
