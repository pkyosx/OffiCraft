package main

// winddown_anchor_write_ordering_t55_test.go — T-55 batch C.
//
// The batch moves stopping_since / stopped_since / refocus_since / refocus_op out
// of PutMember's DO UPDATE SET and gives them one writer. Two things then hold the
// batch up that NOTHING ELSE IN THE SUITE WOULD NOTICE the loss of:
//
//  1. THE ORDER. persistMemberWindDownAnchors runs BEFORE the whole-row write, the
//     opposite of persistMemberOpReceipt next door, because the whole-row write
//     fans the member delta and the wind-down / recycle hook in cli/ocagent keys on
//     that delta to go read these four columns. Reverse it and the agent refetches
//     a row whose anchors have not landed. Every existing test still sees both
//     writes land, so the reversal is invisible to them.
//  2. THAT EVERY FACE PERSISTS AT ALL. Before this batch a face only had to mutate
//     the struct; the row write carried the columns for free. Now each face needs
//     its own call, and deleting any one of them breaks that face SILENTLY — the
//     request still answers 200 and the row simply keeps the old rung.
//
// FAULT INJECTION WITHOUT A SEAM: the ordering test installs a SQLite trigger on
// the real writer rather than threading a failable DAL through production code, for
// the reason receipt_write_ordering_t55_test.go spells out — a seam in the write
// path is something the next writer routes around.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWindDownAnchors_LandBeforeTheRowWriteThatFansTheDelta pins invariant 1.
//
// 停止 is the clearest face to prove it on: it moves desired_state (a column the
// whole-row write still carries) AND stopping_since (one that only the anchor
// writer can move). Block the anchor write and the request must fail with
// desired_state UNTOUCHED — which can only be true if the anchor write ran first.
// Under the reversed order the row write has already landed and desired_state is
// offline, with the member's own hook then reading a row that says "no wind-down".
func TestWindDownAnchors_LandBeforeTheRowWriteThatFansTheDelta(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-anchor-order")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)

	before, err := s.dal.GetMember("m-anchor-order")
	if err != nil || before == nil {
		t.Fatalf("read fixture: %v", err)
	}
	if before.DesiredState != DesiredStateOnline {
		t.Fatalf("fixture is blind: the member must start online, got %q", before.DesiredState)
	}

	if _, err := s.dal.wdb.Exec(`CREATE TRIGGER t55c_block_anchors
		BEFORE UPDATE OF stopping_since ON member
		BEGIN SELECT RAISE(ABORT, 't55c: anchor write blocked'); END;`); err != nil {
		t.Fatalf("install the blocking trigger: %v", err)
	}

	deactivate := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
			taskReq(t, "POST", "/api/members/m-anchor-order/deactivate", nil,
				wireOwnerID, "owner"), "m-anchor-order")
		return rec
	}

	if rec := deactivate(); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a blocked anchor write must fail the request, got %d %s",
			rec.Code, rec.Body.String())
	}

	// 🔴 THE ASSERTION THIS FILE EXISTS FOR.
	//
	// It proves the ORDER mechanically: desired_state can only still be online if
	// the whole-row write never ran, and it never ran because the anchor write
	// failed first. 下線 is used because it is the one face that moves both a
	// carried column and an anchor, which is what makes the proxy readable at all
	// — the consumer-side half of the argument (that a delta fanned ahead of the
	// anchors is a WRONG ANSWER, not just an untidy one) is pinned separately on
	// the refocus face, where the hook that reads these columns actually keys.
	got, _ := s.dal.GetMember("m-anchor-order")
	if got == nil || got.DesiredState != before.DesiredState {
		t.Fatalf("desired_state = %q after the failed anchor write, want it "+
			"UNCHANGED (%q) — the whole-row write must not have run, because the "+
			"anchors land before it",
			got.DesiredState, before.DesiredState)
	}

	if _, err := s.dal.wdb.Exec(`DROP TRIGGER t55c_block_anchors`); err != nil {
		t.Fatalf("drop the blocking trigger: %v", err)
	}

	if rec := deactivate(); rec.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-anchor-order")
	if after == nil || after.DesiredState != DesiredStateOffline || after.StoppingSince <= 0 {
		t.Fatalf("the retry must land BOTH halves, got desired_state=%q "+
			"stopping_since=%v", after.DesiredState, after.StoppingSince)
	}
}

// TestEveryWindDownFacePersistsItsAnchors pins invariant 2, one row per face.
//
// Mutant: delete the persistMemberWindDownAnchors call from any face below and
// exactly that row goes red, naming the face. Before this batch every one of these
// rows passed for free off the whole-row write, which is why none of them existed:
// the columns rode along and no face had to do anything.
//
// 🔴 EVERY ROW ASSERTS ON THE ROW READ BACK FROM THE DATABASE, never on the
// handler's response body. The body is rendered from the in-memory struct the face
// just mutated, so it reports the anchor moved whether or not anything persisted
// it — asserting on the body would pass under every mutant this test exists to
// catch.
func TestEveryWindDownFacePersistsItsAnchors(t *testing.T) {
	cases := []struct {
		name  string
		seed  func(m *Member)
		drive func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder
		want  func(m Member) (bool, string)
	}{
		{
			name: "下線 deactivate",
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
					taskReq(t, "POST", "/api/members/"+id+"/deactivate", nil, wireOwnerID, "owner"), id)
				return rec
			},
			want: func(m Member) (bool, string) { return m.StoppingSince > 0, "stopping_since > 0" },
		},
		{
			name: "強制停止 force-stop",
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
					taskReq(t, "POST", "/api/members/"+id+"/force-stop", nil, wireOwnerID, "owner"), id)
				return rec
			},
			want: func(m Member) (bool, string) { return m.StoppingSince > 0, "stopping_since > 0" },
		},
		{
			name: "加速停止 accelerated-stop",
			seed: func(m *Member) { m.DesiredState = DesiredStateOffline; m.StoppingSince = 1_000 },
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(rec,
					taskReq(t, "POST", "/api/members/"+id+"/accelerated-stop", nil, wireOwnerID, "owner"), id)
				return rec
			},
			// 🔴 NOT `stopping_since > 0`: this face is reached on a member whose
			// stop is ALREADY open, so the fixture seeds that column non-zero and
			// the obvious assertion is satisfied by the SEED — the first version of
			// this row was a false green and a mutant sweep is what caught it.
			// refocus_op is what only this face writes ("" on the seeded row), so
			// it is the one value that distinguishes "persisted" from "not".
			want: func(m Member) (bool, string) {
				return m.RefocusOp == refocusOpAcceleratedStop && m.StoppingSince != 1_000,
					"refocus_op = " + refocusOpAcceleratedStop + " and stopping_since moved off the seed"
			},
		},
		{
			name: "report_stopping (agent 自報)",
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleReportStoppingApiSelfStoppingPost(rec,
					taskReq(t, "POST", "/api/self/stopping", nil, id, "agent"))
				return rec
			},
			want: func(m Member) (bool, string) { return m.StoppingSince > 0, "stopping_since > 0" },
		},
		{
			name: "report_stopped (agent 自報)",
			seed: func(m *Member) { m.StoppingSince = 1_000 },
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleReportStoppedApiSelfStoppedPost(rec,
					taskReq(t, "POST", "/api/self/stopped", nil, id, "agent"))
				return rec
			},
			want: func(m Member) (bool, string) { return m.StoppedSince > 0, "stopped_since > 0" },
		},
		{
			// The CLEARING direction, and it is the one a fixture cannot fake: the
			// seeded anchors are non-zero and all four must come back zero. A face
			// that clears in memory and never persists leaves them standing, which
			// is what made a respawned agent read as still winding down.
			name: "report_waking 清錨點 (agent 自報)",
			seed: func(m *Member) {
				m.StoppingSince, m.StoppedSince = 2_001, 2_002
				m.RefocusSince, m.RefocusOp = 2_003, refocusOpRefocus
			},
			drive: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.HandleReportWakingApiSelfWakingPost(rec,
					taskReq(t, "POST", "/api/self/waking", nil, id, "agent"))
				return rec
			},
			want: func(m Member) (bool, string) {
				ok := m.StoppingSince == 0 && m.StoppedSince == 0 &&
					m.RefocusSince == 0 && m.RefocusOp == ""
				return ok, "all four anchors back to zero"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			putWarden(t, s, "mach-a")
			id := "m-face"
			m := testAgent(id)
			m.DesiredMachineID = "mach-a"
			if tc.seed != nil {
				tc.seed(&m)
			}
			putTestMember(t, s, m)
			connectOnline(t, s, id)

			if rec := tc.drive(t, s, id); rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body.String())
			}
			got, err := s.dal.GetMember(id)
			if err != nil || got == nil {
				t.Fatalf("re-read member: %v", err)
			}
			if ok, wanted := tc.want(*got); !ok {
				t.Fatalf("%s did not PERSIST its wind-down anchors — the row wants "+
					"%s, got stopping=%v stopped=%v refocus=%v op=%q. The face "+
					"mutates the struct; since T-55 the whole-row write no longer "+
					"carries these four columns, so the face must call "+
					"persistMemberWindDownAnchors itself",
					tc.name, wanted, got.StoppingSince, got.StoppedSince,
					got.RefocusSince, got.RefocusOp)
			}
		})
	}
}

// TestFixtureSeeds_ReallyPlantTheAnchors is a tripwire on THE FIX, not on the
// product code.
//
// Fixtures across this suite — the shared helpers and a long tail of direct call
// sites — now plant the four anchors through their sole writer, because a
// whole-row fixture write can no longer move them. (No count is given: the first
// version of this comment said "nineteen fixtures plus the two shared helpers"
// and both numbers were wrong within the same change — there is a THIRD helper of
// that shape, putGateMember, and an independent review is what found it. An
// unenforced count in a comment is a claim nobody re-checks.) Those seeds have the SAME failure mode as the thing they
// repair: get a field wrong, drop one, let a later write clobber them, and the
// residue is 0/"" — which is indistinguishable from never having seeded at all.
// The tests that depend on them would go green having exercised the wrong state.
//
// So the seeds get their own guard, with FOUR DISTINCT probe values: identical
// values would let a helper that transposed two arguments read correct.
//
// Mutant: empty out seedMemberAnchors, seedWorkerAnchors, or either shared
// fixture helper's anchor write, and the matching subtest goes red.
func TestFixtureSeeds_ReallyPlantTheAnchors(t *testing.T) {
	const (
		wantStopping = 9_001.0
		wantStopped  = 9_002.0
		wantRefocus  = 9_003.0
		wantOp       = "seed-probe-op"
	)
	assertPlanted := func(t *testing.T, s *apiServer, id, via string) {
		t.Helper()
		got, err := s.dal.GetMember(id)
		if err != nil || got == nil {
			t.Fatalf("%s: re-read: %v", via, err)
		}
		if got.StoppingSince != wantStopping || got.StoppedSince != wantStopped ||
			got.RefocusSince != wantRefocus || got.RefocusOp != wantOp {
			t.Fatalf("%s planted nothing (or planted the wrong columns): "+
				"stopping=%v want %v, stopped=%v want %v, refocus=%v want %v, "+
				"op=%q want %q. A seed that silently does nothing leaves 0/\"\", "+
				"which is exactly what an unseeded row looks like — every fixture "+
				"relying on this helper is then testing a state it never created",
				via, got.StoppingSince, wantStopping, got.StoppedSince, wantStopped,
				got.RefocusSince, wantRefocus, got.RefocusOp, wantOp)
		}
	}

	t.Run("putTestMember re-seeding a row that already exists", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := testAgent("m-seed")
		putTestMember(t, s, m) // the row exists from here on: the upsert can no
		// longer move the four, so the helper's own anchor write is the only path
		m.StoppingSince, m.StoppedSince = wantStopping, wantStopped
		m.RefocusSince, m.RefocusOp = wantRefocus, wantOp
		putTestMember(t, s, m)
		assertPlanted(t, s, "m-seed", "putTestMember")
	})

	t.Run("putWorkerFixture re-seeding a worker row that already exists", func(t *testing.T) {
		s := newWorkerTestServer(t)
		w := fsmWorkerFixture(t, s, "ow-reseed", WorkerStatusAssigned, 1_000)
		w.StoppingSince, w.StoppedSince = wantStopping, wantStopped
		w.RefocusSince, w.RefocusOp = wantRefocus, wantOp
		putWorkerFixture(t, s, w)
		assertPlanted(t, s, "ow-reseed", "putWorkerFixture")
	})

	t.Run("seedMemberAnchors beside a whole-row fixture write", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := testAgent("m-seed2")
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed row: %v", err)
		}
		m.StoppingSince, m.StoppedSince = wantStopping, wantStopped
		m.RefocusSince, m.RefocusOp = wantRefocus, wantOp
		if err := s.dal.PutMember(m); err != nil { // the shape the 19 sites have
			t.Fatalf("whole-row fixture write: %v", err)
		}
		seedMemberAnchors(t, s, m)
		assertPlanted(t, s, "m-seed2", "seedMemberAnchors")
	})

	t.Run("seedWorkerAnchors on the outsource face of the same row", func(t *testing.T) {
		s := newWorkerTestServer(t)
		w := fsmWorkerFixture(t, s, "ow-seed", WorkerStatusAssigned, 1_000)
		w.StoppingSince, w.StoppedSince = wantStopping, wantStopped
		w.RefocusSince, w.RefocusOp = wantRefocus, wantOp
		if err := s.dal.PutOutsourceWorker(w); err != nil {
			t.Fatalf("whole-row fixture write: %v", err)
		}
		seedWorkerAnchors(t, s, w)
		// Read through the MEMBER face on purpose: a worker row IS a member row,
		// and the seed writes member columns. If those two ever disagree the seed
		// is planting somewhere nothing reads.
		assertPlanted(t, s, "ow-seed", "seedWorkerAnchors")
	})
}

// TestRefocusFansNoDeltaWhenItsAnchorWriteFails is the CONSUMER-SIDE half, and it
// is on the face where the argued race is real.
//
// The ordering test above proves the two writes happen in the stated order. This
// one proves WHY that order matters, and it has to be a different face to do it:
// the wind-down hook (cli/ocagent shouldWindDown) refetches only desired_state,
// which still rides the whole-row write — so on 下線 the order is invisible to the
// agent. The RECYCLE hook is the one that refetches the row and reads
// refocus_since, and refocus is its face. Fan a member delta for a refocus whose
// epoch has not landed and that hook reads 0, concludes nothing was armed, and
// the owner's 重新聚焦 evaporates with a 200 already sent.
//
// So: block the anchor write, drive refocus, and assert NO member delta was
// fanned. Under the reversed order the row write runs, the delta goes out, and
// the agent is told to go look at an epoch that is not there.
//
// The retry at the end is the positive control — without it "no frame" would also
// be satisfied by a listener that never receives anything.
func TestRefocusFansNoDeltaWhenItsAnchorWriteFails(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-refocus-order")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	l := connectOnline(t, s, "m-refocus-order")
	drainHubFrames(l)

	if _, err := s.dal.wdb.Exec(`CREATE TRIGGER t55c_block_refocus
		BEFORE UPDATE OF refocus_since ON member
		BEGIN SELECT RAISE(ABORT, 't55c: anchor write blocked'); END;`); err != nil {
		t.Fatalf("install the blocking trigger: %v", err)
	}

	refocus := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
			taskReq(t, "POST", "/api/members/m-refocus-order/refocus", nil,
				wireOwnerID, "owner"), "m-refocus-order")
		return rec
	}

	if rec := refocus(); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a blocked anchor write must fail the request, got %d %s",
			rec.Code, rec.Body.String())
	}
	assertNoFrame(t, l, "a refocus whose epoch never landed")
	if got, _ := s.dal.GetMember("m-refocus-order"); got == nil || got.RefocusSince != 0 {
		t.Fatalf("fixture is blind: the blocked write must have left refocus_since at 0, got %v",
			got.RefocusSince)
	}

	if _, err := s.dal.wdb.Exec(`DROP TRIGGER t55c_block_refocus`); err != nil {
		t.Fatalf("drop the blocking trigger: %v", err)
	}

	// POSITIVE CONTROL: the same call, unblocked, must land the epoch AND fan.
	if rec := refocus(); rec.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-refocus-order")
	if after == nil || after.RefocusSince <= 0 || after.RefocusOp != refocusOpRefocus {
		t.Fatalf("the retry must arm the epoch, got since=%v op=%q",
			after.RefocusSince, after.RefocusOp)
	}
	if l.pop() == nil {
		t.Fatal("control broken: a SUCCESSFUL refocus fanned no frame either, so " +
			"the no-frame assertion above proved nothing about ordering")
	}
}
