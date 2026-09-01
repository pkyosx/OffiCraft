package main

// lifecycle_tick_test.go — the gate that keeps T-14 item 5's merge from
// silently disarming most of this package's outsource coverage.
//
// 🔴 WHY THIS FILE EXISTS. Before the merge, --no-outsource worked by simply
// not mounting a cadence goroutine, so runOutsourceTick never had to read the
// flag — and ~98 call sites across ~18 test files depend on exactly that: they
// set api.noOutsource = true so no cadence can race them (the test server
// mounts no cadence anyway, but the flag is what those tests declare) and then
// drive the scheduler by hand. With ONE cadence loop the skip has to move
// somewhere, and there is a cheap-looking wrong place for it: the top of
// runOutsourceTick, or the top of runLifecycleTick.
//
// Putting it there is not merely wrong, it is wrong in the way that hides:
// every "a worker was assigned" assertion goes red and gets noticed, while
// every "no SECOND worker was assigned" / "nothing changed" assertion goes
// GREEN while exercising nothing at all. A suite that half-fails loudly can
// still be repaired by making the loud half pass — and the quiet half stays
// dead. So the flag's LOCATION gets its own pins, in both directions:
//
//	(a) the halves themselves must NOT read their flag  → the "still binds"
//	    assertions below, which are what a gate-at-the-entry mutant reddens;
//	(b) runLifecycleTick must skip a flagged-off half   → the "must not bind"
//	    assertions, which are what a no-gate-at-all mutant reddens.
//
// One direction alone is satisfiable by deleting the feature.

import "testing"

// lifecycleTickGateServer builds a server that can exercise BOTH halves: the
// out-of-box seed gives the reconcile half a role to boot a persona from, and
// the task handlers give the outsource half a queue.
func lifecycleTickGateServer(t *testing.T) *apiServer {
	t.Helper()
	s := newReconcileTestServer(t)
	putOutsourceManual(t, s, "review-pr", "claude-sonnet-4-5", 1)
	return s
}

// assignedWorkerID returns the worker bound to taskID, or "" if the scheduler
// has not bound one.
func assignedWorkerID(t *testing.T, s *apiServer, taskID string) string {
	t.Helper()
	bound, err := s.dal.GetTask(taskID)
	if err != nil || bound == nil {
		t.Fatalf("re-read task %s: %+v (%v)", taskID, bound, err)
	}
	return bound.ExecutorID
}

func TestLifecycleTickHalvesAreGatedAtTheCallSite(t *testing.T) {
	// ── (a) the halves do not read their own flag ────────────────────────────
	t.Run("outsource half still binds with noOutsource set (the flag is not read inside it)", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noOutsource = true
		task := createOutsourceTask(t, s, "review-pr", "gate pin")

		s.runOutsourceTick(1000.0)

		if got := assignedWorkerID(t, s, task.ID); got == "" {
			t.Fatalf("runOutsourceTick bound no worker while api.noOutsource was set. " +
				"That flag must gate the CALL from runLifecycleTick, never this " +
				"function's body: ~98 call sites set it and then drive this tick by " +
				"hand, and a gate here turns every one of them into a no-op — the " +
				"'a worker appeared' half of the suite goes red, the 'no second " +
				"worker appeared' half goes green while asserting nothing.")
		}
	})

	t.Run("reconcile half still dispatches with noReconcile set (the flag is not read inside it)", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noReconcile = true
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)

		s.runReconcileTick(1000.0)

		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "start" {
			t.Fatalf("runReconcileTick dispatched %+v while api.noReconcile was set; "+
				"want exactly one START. The kill switch gates the CALL in "+
				"runLifecycleTick and the event-driven seams, not this body — a gate "+
				"here would silently neuter every test that sets the flag and then "+
				"ticks by hand.", frames)
		}
	})

	// ── (b) runLifecycleTick honours both flags, per half ────────────────────
	t.Run("runLifecycleTick skips the outsource half when noOutsource is set", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noOutsource = true
		task := createOutsourceTask(t, s, "review-pr", "gate pin")

		s.runLifecycleTick(1000.0)
		if got := assignedWorkerID(t, s, task.ID); got != "" {
			t.Fatalf("--no-outsource must stop the merged cadence from assigning, "+
				"got worker %q. Before the merge this was true because no scheduler "+
				"goroutine was mounted at all; the single cadence has to reproduce it "+
				"at the call site.", got)
		}

		// …and clearing it is all that stands between the queue and a worker —
		// i.e. the skip above was the flag, not a broken fixture.
		s.noOutsource = false
		s.runLifecycleTick(1001.0)
		if got := assignedWorkerID(t, s, task.ID); got == "" {
			t.Fatal("with --no-outsource cleared the merged cadence must assign the " +
				"queued task; it bound nothing, so the 'must not bind' assertion " +
				"above proves nothing about the flag.")
		}
	})

	t.Run("runLifecycleTick skips the reconcile half when noReconcile is set", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noReconcile = true
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)

		s.runLifecycleTick(1000.0)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("--no-reconcile must stop the merged cadence from commanding "+
				"wardens, got %+v", frames)
		}

		s.noReconcile = false
		s.runLifecycleTick(1001.0)
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "start" {
			t.Fatalf("with --no-reconcile cleared the merged cadence must dispatch "+
				"the START, got %+v — so the 'no frames' assertion above proves "+
				"nothing about the flag", frames)
		}
	})

	// ── one switch never silences the other half ─────────────────────────────
	//
	// This is the property the merge could most easily lose by accident: two
	// loops could not possibly have shared a kill switch, one loop can.
	t.Run("each kill switch stops only its own half", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noReconcile = true
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)
		task := createOutsourceTask(t, s, "review-pr", "gate pin")

		s.runLifecycleTick(1000.0)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("--no-reconcile: no warden command may go out, got %+v", frames)
		}
		if got := assignedWorkerID(t, s, task.ID); got == "" {
			t.Fatal("--no-reconcile must NOT stop the outsource half: the queued " +
				"task went unassigned, which means the two halves now share one " +
				"switch")
		}
	})

	// The mirror of the case above, and it is NOT redundant: the two directions
	// fail to different mutants. A `if s.noOutsource { return }` written at the
	// top of runLifecycleTick — the most natural way to "make the flag work"
	// once there is only one tick — is invisible to every assertion in this file
	// except this one, because every other case leaves noOutsource false while
	// the reconcile half is the thing under test.
	t.Run("--no-outsource leaves the reconcile half running", func(t *testing.T) {
		s := lifecycleTickGateServer(t)
		s.noOutsource = true
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)
		task := createOutsourceTask(t, s, "review-pr", "gate pin")

		s.runLifecycleTick(1000.0)
		if got := assignedWorkerID(t, s, task.ID); got != "" {
			t.Fatalf("--no-outsource: no worker may be assigned, got %q", got)
		}
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "start" {
			t.Fatalf("--no-outsource must NOT stop the reconcile half: dispatched "+
				"%+v, want one START. A flag read at the TOP of runLifecycleTick "+
				"instead of at the outsource call site looks identical to every "+
				"other assertion in this file and only reddens here.", frames)
		}
	})
}
