package main

import (
	"fmt"
	"testing"
)

// T-14 項目 6, step 1 — THE PARITY TEST FOR lifecycleTickDriverFor.
//
// 🔴 WHAT THIS FILE IS DEFENDING, said once, plainly.
//
// The merged lifecycle tick (lifecycle_tick.go) runs two halves over two row
// populations: runReconcileTick over dal.ListMembers, runOutsourceTick over
// dal.ListOutsourceWorkers. An outsource worker IS a member row
// (PutOutsourceWorker → PutMember; there is no second table), so the two
// populations OVERLAP at the source and something has to separate them.
//
// 🔴 THAT SOMETHING USED TO BE A SQL STRING — ListMembers was `FROM member WHERE
// kind != 'outsource'` (dal.go) — AND IT IS NOT ANY MORE. T-14 項目 6 deleted the
// clause; what separates the halves today is the driver guard each one asks at
// the head of its own loop. That is not a smaller wall, but it IS a deletable
// one, which is the whole reason this file and the identity-gate ledger both
// exist. With the clause lifted and no guard, one ACTIVE desired-online worker
// row was measured taking a `start` from enqueueWardenFrame AND a `start` from
// notifyWorkerSpawn in the SAME tick.
//
// lifecycleTickDriverFor re-sites that split out of the query and into a named
// total predicate. This test is what makes the re-siting worth doing: it turns
// "exactly one half owns every row" into a universal sentence that is checked
// CELL BY CELL over the whole (Kind × RosterStatus × DesiredState × Activated)
// space, and that names the offending cell — and both halves by their function
// names — when it breaks.
//
// ⚠️ THE TWO SIDES MUST STAY INDEPENDENTLY WRITTEN. The expectation below
// (driverExpectedByPopulation) is deliberately transcribed from the DATA
// SOURCES — which reader hands the row to which half — using raw string
// literals rather than the Kind* constants and without calling
// lifecycleTickDriverFor. If a future edit makes the expectation call the thing
// it is checking, this test becomes a tautology that no mutant can redden, and
// the only wall between the two FSMs goes back to being invisible.

// 🔴 WHAT THIS FILE DOES **NOT** DEFEND — say it, because the first draft of
// this PR's description said the opposite and the opposite was measured false.
//
// This test guards the PREDICATE. It has no way to see whether either half
// still ASKS it. Independent review on 2026-09-03 deleted
// reconcile.go's `if lifecycleTickDriverFor(m) != driverReconcile { continue }`
// and, separately, outsource_sched.go's twin — and the WHOLE ocserverd suite
// stayed green both times (1968/1968). Only making the predicate itself wrong
// (always returning driverReconcile) reddened anything. So the sentence "任一半
// 偷偷多擁有或少擁有一格都會當場紅" was NOT true of this file, and must not be
// re-attached to it.
//
// The property does exist now, and it is somewhere else:
// lifecycle_identity_gate_t170e_test.go registers lifecycleTickDriverFor in
// identitySeamFuncs, so BOTH call sites are ledgered by
// FILE :: FUNCTION :: EXPRESSION. Delete either guard and its ledger entry goes
// stale — TestIdentityGatesAreEachOnTheRecord then fails and prints the exact
// key that vanished (measured 2026-09-03, both directions). Read the two
// lifecycleTickDriverFor entries in identityGateLedger before touching a guard.

// lifecycleDriverCell is ONE point of the enumerated space. Every field is an
// axis the two halves are known to read: Kind picks the population, RosterStatus
// and Activated are what workerStatusFromMember folds into the worker
// vocabulary (removed⇒released, activated>0⇒active, else assigned), and
// DesiredState is the owner's hold-down.
type lifecycleDriverCell struct {
	kind      string
	roster    string
	activated float64
	desired   string
}

func (c lifecycleDriverCell) String() string {
	act := "activated=0"
	if c.activated > 0 {
		act = "activated>0"
	}
	return fmt.Sprintf("row(kind=%s, roster=%s, %s, desired=%s)",
		c.kind, c.roster, act, c.desired)
}

func (c lifecycleDriverCell) member() Member {
	return Member{
		ID:           "m-driver-parity",
		Name:         "driver-parity",
		Kind:         c.kind,
		RosterStatus: c.roster,
		ActivatedTS:  c.activated,
		DesiredState: c.desired,
	}
}

// driverExpectedByPopulation is the SECOND, INDEPENDENT statement of the split
// — read off the two snapshot readers rather than off lifecycleTickDriverFor:
//
//   - dal.ListMembers → the WHOLE member table since T-14 項目 6, narrowed at
//     runReconcileTick's own loop head: every row whose kind is not the literal
//     "outsource" reaches the staff half's decide pass, and no row whose kind IS
//     "outsource" ever does. Read the guard, not the query — the query stopped
//     being the answer when the clause was deleted.
//   - dal.ListOutsourceWorkers → the kind='outsource' rows, ALL of them:
//     assigned, active and released, held down or not. Rows this half then
//     declines are declined inside its own switch, never handed onward.
//
// Kind is the only axis either reader consults, which is itself the finding
// worth writing down: the other three axes are enumerated because they are what
// the two halves branch on AFTER admission, and this test's job is to pin that
// none of them can ever change WHO admits the row.
func driverExpectedByPopulation(kind string) lifecycleDriver {
	if kind == "outsource" {
		return "runOutsourceTick"
	}
	return "runReconcileTick"
}

func lifecycleDriverCells() []lifecycleDriverCell {
	kinds := []string{KindStaff, KindWarden, KindOutsource}
	rosters := []string{RosterStatusActive, RosterStatusRemoved}
	desireds := []string{DesiredStateOnline, DesiredStateOffline, DesiredStateUninstall}
	activateds := []float64{0.0, 1_700_000_000.0}

	cells := make([]lifecycleDriverCell, 0, len(kinds)*len(rosters)*len(desireds)*len(activateds))
	for _, k := range kinds {
		for _, r := range rosters {
			for _, d := range desireds {
				for _, a := range activateds {
					cells = append(cells, lifecycleDriverCell{
						kind: k, roster: r, activated: a, desired: d,
					})
				}
			}
		}
	}
	return cells
}

// TestLifecycleTickDriver_EveryRowHasExactlyOneDriver is the universal sentence:
// over the whole enumerated space, exactly one half claims each row.
//
// The two claims are TRANSCRIPTIONS OF THE GUARDS AS THEY ARE WRITTEN at the
// head of each producer's row loop — reconcile.go's
// `lifecycleTickDriverFor(m) != driverReconcile { continue }` and
// outsource_sched.go's `lifecycleTickDriverFor(memberFromWorker(w)) !=
// driverOutsource { continue }`. If either guard is reworded in production, the
// transcription here has to move with it or this test stops describing the code.
func TestLifecycleTickDriver_EveryRowHasExactlyOneDriver(t *testing.T) {
	cells := lifecycleDriverCells()
	if len(cells) != 36 {
		t.Fatalf("the enumerated space is %d cells, expected 36 "+
			"(3 kinds × 2 roster states × 3 desired states × 2 activated states) — "+
			"an axis was added or dropped without this count moving, which means "+
			"the parity claim below no longer covers what it says it covers", len(cells))
	}

	for _, c := range cells {
		m := c.member()
		got := lifecycleTickDriverFor(m)

		// The guard runReconcileTick asks, verbatim.
		claimedByReconcile := got == driverReconcile
		// The guard runOutsourceTick asks, verbatim.
		claimedByOutsource := got == driverOutsource

		switch {
		case claimedByReconcile && claimedByOutsource:
			t.Errorf("%s is claimed by BOTH runReconcileTick and runOutsourceTick — "+
				"exactly one half must drive a row (lifecycleTickDriverFor, "+
				"lifecycle_roster.go). Two halves on one row is the measured "+
				"double-dispatch that dal.ListMembers' `WHERE kind != 'outsource'` "+
				"used to prevent and that THIS PREDICATE prevents now — the clause is "+
				"gone (T-14 項目 6).", c)
		case !claimedByReconcile && !claimedByOutsource:
			t.Errorf("%s is claimed by NEITHER runReconcileTick nor runOutsourceTick "+
				"(lifecycleTickDriverFor returned %q) — exactly one half must drive a "+
				"row. A row no half claims is never started, never stopped and never "+
				"collected, and nothing else in the tick would say so.",
				c, string(got))
		}

		if want := driverExpectedByPopulation(c.kind); got != want {
			t.Errorf("%s is claimed by %s, but %s must drive it — "+
				"lifecycleTickDriverFor disagrees with the population that actually "+
				"reaches each half (runReconcileTick keeps the rows this predicate "+
				"sends to driverReconcile; dal.ListOutsourceWorkers is every "+
				"kind='outsource' row). The re-siting of the old `WHERE kind != "+
				"'outsource'` was pure and is not permitted to move a row to the "+
				"other half.",
				c, string(got), string(want))
		}
	}
}

// TestLifecycleTickDriver_IsTotalOverTheKindVocabulary pins the OTHER way this
// can rot: a fourth member kind added to domain.go's closed set. The switch
// above enumerates the three kinds that exist; this one asserts the driver
// answers for the vocabulary itself, so a new kind cannot silently inherit
// whichever half the fall-through happens to name without somebody deciding.
func TestLifecycleTickDriver_IsTotalOverTheKindVocabulary(t *testing.T) {
	vocabulary := []string{KindStaff, KindWarden, KindOutsource}
	for _, k := range vocabulary {
		got := lifecycleTickDriverFor(Member{ID: "m-total", Kind: k})
		if got != driverReconcile && got != driverOutsource {
			t.Errorf("lifecycleTickDriverFor(kind=%s) = %q, which is neither "+
				"runReconcileTick nor runOutsourceTick — the driver must be TOTAL: "+
				"every row has exactly one half, and driverNone is a failure value, "+
				"not an answer.", k, string(got))
		}
	}
	// driverNone must never be a driver's name. Referenced here so the constant
	// cannot be quietly deleted or aliased onto a real half.
	if driverNone == driverReconcile || driverNone == driverOutsource {
		t.Fatalf("driverNone (%q) collides with a real half — the NEITHER arm of the "+
			"parity test above would then be unreachable and a non-exhaustive driver "+
			"would read as a legitimate claim.", string(driverNone))
	}
}

// ---------------------------------------------------------------------------
// T-14 項目 6 — the EVENT-DRIVEN door asks the driver question too.
//
// runReconcileTick asks lifecycleTickDriverFor at the head of its candidate
// loop; reconcileMemberNow — the activate / deactivate / relocate / refocus /
// accelerated-stop / uninstall seam — did not. That door reads GetMember, not
// ListMembers, so deleting the `WHERE kind != 'outsource'` clause did not widen
// it. But nothing ever narrowed it either: the only reason a contractor never
// reached the member FSM through it is that all seven non-test callers happen
// to hand it a staff row (api_members ×5 through resolveMember(…, staffOnly);
// api_machines through resolveMachine, which demands kind==warden; onboarding
// with the seed assistant's own id). The guard lived in seven argument lists
// across two other files — and api_members.go:790 / api_machines.go:1280
// already pass anyMember elsewhere, so a future caller widening this one is a
// precedent that already exists, not a hypothetical.
//
// This test asserts the property on the FUNCTION. It is deliberately a
// direct call, bypassing the handlers, because the handlers are exactly the
// layer whose guarantee is being moved inward.
func TestReconcileMemberNow_AContractorIsNotDrivenByTheMemberFSM(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, ServerSelfHost)
	connectOnline(t, s, ServerSelfHost)

	// ── the positive control, planted FIRST ──────────────────────────────────
	// A staff row in the shape the contractor row below is about to copy. If
	// this one does not get a START, the negative half proves nothing: a
	// silent no-command would look identical whether the guard stopped it or
	// the fixture never asked for anything.
	staff := testAgent("m-staff-twin")
	putTestMember(t, s, staff)
	if dec := s.reconcileMemberNow("m-staff-twin"); dec.Command != reconcileCmdStart {
		t.Fatalf("POSITIVE CONTROL FAILED: an ACTIVE desired-online STAFF row on a "+
			"reachable warden must take a START from the member FSM, got %q (%s). "+
			"Until this passes, the contractor assertion below is not evidence.",
			dec.Command, dec.Reason)
	}

	// ── the row under test ───────────────────────────────────────────────────
	// An ACTIVE, desired-online contractor: the exact shape whose double-drive
	// was MEASURED when the clause was lifted (lifecycle_roster.go — one row
	// taking a `start` from enqueueWardenFrame AND from notifyWorkerSpawn in
	// the same tick). It is planted through PutOutsourceWorker, its only real
	// writer, so the member row is the one production would have.
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-twin", Codename: "T-1", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: "t-1", Status: WorkerStatusActive,
		CreatedTS: 1.0, DesiredState: DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
	}); err != nil {
		t.Fatalf("seed contractor: %v", err)
	}
	// The fixture is only worth anything if the row really did land as a
	// kind='outsource' member the member-FSM door can see.
	got, err := s.dal.GetMember("ow-twin")
	if err != nil || got == nil {
		t.Fatalf("the contractor must be readable through GetMember — that is the "+
			"very read reconcileMemberNow performs: %v", err)
	}
	if got.Kind != KindOutsource || got.RosterStatus != RosterStatusActive ||
		got.DesiredState != DesiredStateOnline {
		t.Fatalf("fixture did not land the shape under test: kind=%q roster=%q "+
			"desired=%q, want outsource/active/online", got.Kind, got.RosterStatus,
			got.DesiredState)
	}
	// And the shape must be one the ENTRY FILTER would let through, or the
	// driver guard is not what is doing the work here.
	if !lifecyclePolicyFor(*got).ShouldExist() {
		t.Fatalf("this contractor is rejected by the entry filter, so this test " +
			"would pass with the driver guard deleted — pick a row the entry " +
			"filter accepts")
	}

	// The zero decision — "" — is what this door returns for a member it does
	// not act on (its doc comment: a gated-off / skipped / faulted member yields
	// the zero decision, no command, not unlanded). reconcileCmdNone ("none") is
	// a DIFFERENT answer: it means the FSM ran and converged. Asserting on ""
	// therefore also pins that the row was declined BEFORE the FSM, not by it.
	dec := s.reconcileMemberNow("ow-twin")
	if dec.Command != "" {
		t.Errorf("reconcileMemberNow drove a kind='outsource' row through the MEMBER "+
			"FSM and decided %q (%s). The outsource half owns this row "+
			"(lifecycleTickDriverFor → driverOutsource) and dispatches its own start "+
			"through notifyWorkerSpawn; a second start from this door is the measured "+
			"double-drive, and nothing logs it.", dec.Command, dec.Reason)
	}
	// A row this half declined must leave NO trace in the member half's store:
	// a decision suppressed at the dispatch is not the same as a row never
	// claimed, and only the latter is what the driver split promises.
	if st, ok := s.reconcileStates[dec.MemberID]; ok && dec.MemberID != "" {
		t.Errorf("the member FSM recorded state %+v for a contractor it does not "+
			"drive — the guard must decline BEFORE reconcileTickMemberLocked, not "+
			"inside it", st)
	}
	if _, ok := s.reconcileStates["ow-twin"]; ok {
		t.Errorf("the member FSM left reconcile state on ow-twin — a row driven by " +
			"the outsource half must never appear in reconcileStates, which is the " +
			"other half of the measured double-drive (state in BOTH stores)")
	}
}

// ---------------------------------------------------------------------------
// T-14 項目 6 — the CADENCE door, behaviourally.
//
// The ledger (TestIdentityGatesAreEachOnTheRecord) already fails BY NAME when
// runReconcileTick's driver guard is deleted, and that was the whole tooth: no
// test in this package drove runReconcileTick over a roster containing a
// contractor. A structural guard is a good tooth, but it says nothing about
// what the owner would experience — it only says a line moved. This one asserts
// the CONSEQUENCE: with the guard gone, a live contractor takes a `start` from
// the member half, on top of the one the outsource half dispatches through
// notifyWorkerSpawn. That is the measured double-drive lifecycle_roster.go
// records, and until now nothing in this package would have gone red for it.
func TestRunReconcileTick_AContractorTakesNoStartFromTheMemberHalf(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, ServerSelfHost)
	connectOnline(t, s, ServerSelfHost)

	// A live contractor, and — deliberately — a staff row of the SAME shape
	// sitting next to it in the SAME roster snapshot. One tick, two rows: the
	// staff frame is what proves the tick ran and reached its dispatch at all,
	// so a missing contractor frame cannot be explained away by "the fixture
	// never produced any frames".
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-cadence", Codename: "C-1", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: "t-1", Status: WorkerStatusActive,
		CreatedTS: 1.0, DesiredState: DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
	}); err != nil {
		t.Fatalf("seed contractor: %v", err)
	}
	putTestMember(t, s, testAgent("m-cadence-twin"))

	// The roster read the tick performs must genuinely CONTAIN the contractor,
	// or this test is asserting on a population the guard never had to filter.
	// This is the assertion that fails first if `WHERE kind != 'outsource'`
	// ever comes back — in which case the guard is untested again, silently.
	roster, err := s.dal.ListMembers()
	if err != nil {
		t.Fatalf("roster read: %v", err)
	}
	sawContractor := false
	for _, m := range roster {
		if m.ID == "ow-cadence" {
			sawContractor = true
		}
	}
	if !sawContractor {
		t.Fatalf("dal.ListMembers() did not return the contractor, so the driver " +
			"guard in runReconcileTick is filtering an empty set and this test " +
			"proves nothing. The merged roster read (T-14 項目 6) is the premise.")
	}

	s.runReconcileTick(nowSecs())

	frames := drainFrames(t, s, ServerSelfHost)
	var staffStarts, contractorStarts int
	for _, f := range frames {
		if f.RPC != "start" {
			continue
		}
		switch f.Args["member_id"] {
		case "m-cadence-twin":
			staffStarts++
		case "ow-cadence":
			contractorStarts++
		}
	}
	if staffStarts != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: the staff row next to the contractor took "+
			"%d starts from this tick, want exactly 1 (frames: %+v). Until this "+
			"holds, a contractor start count of 0 is not evidence of anything.",
			staffStarts, frames)
	}
	if contractorStarts != 0 {
		t.Errorf("runReconcileTick dispatched %d `start` frame(s) for a kind='outsource' "+
			"row. The outsource half already starts it through notifyWorkerSpawn, so "+
			"this is the SECOND start on the same row in the same tick — the measured "+
			"double-drive recorded on lifecycleTickDriverFor (lifecycle_roster.go). "+
			"It is silent: two starts look like one retry.", contractorStarts)
	}
	if st, ok := s.reconcileStates["ow-cadence"]; ok {
		t.Errorf("the member half left reconcile state %+v on a contractor. The "+
			"outsource half keeps its own state in workerReconcileStates; a row in "+
			"BOTH stores is the other half of the same measured defect, and it "+
			"persists after the tick rather than only during it", st)
	}
}
