package main

// member_stop_clears_refocus_ted79_test.go — T-ed79 parity #9: stopping a staff
// member must clear its 換手 marker, exactly as stopping a worker always has.
//
// The worker /stop handler zeroes refocus_since + refocus_op. The staff 下線 and
// 強制停止 zeroed neither, and no test looked. The code itself already named the
// harm this leaves behind (armRefocusEpoch, member_ownerop_winddown.go): a
// refocus marker that outlives its window turns the NEXT epoch on that member
// into an immediate kill, because decideUp's recycle arm reads
// refocus_since + stopped_since and robust-stops on the spot, zero grace, no
// close-out. A live activate preserves that active epoch, while an offline
// activate clears the replaced generation before starting the next one.

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// memberInAnOpenHandover: online, desired online, mid-換手 — the state every
// staff owner-verb opens (armMemberOwnerOpHandover) and the one 停止 can land on.
func memberInAnOpenHandover(t *testing.T, s *apiServer, id string) Member {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.RefocusSince = 1000.0
	m.RefocusOp = refocusOpRefocus
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, id, "mach-a")
	return m
}

func TestDeactivateMemberClearsTheHandoverMarker(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	memberInAnOpenHandover(t, s, "m-stopme")

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-stopme/deactivate", nil, wireOwnerID, "owner"),
		"m-stopme")

	after, _ := s.dal.GetMember("m-stopme")
	if after.RefocusSince != 0 {
		t.Errorf("下線 left refocus_since=%v on the row. The worker /stop has cleared it "+
			"since T-ed79's first commit, and armRefocusEpoch spells out what a marker "+
			"that outlives its window does to the NEXT epoch: an immediate kill, no "+
			"close-out.", after.RefocusSince)
	}
	if after.RefocusOp != "" {
		t.Errorf("下線 left refocus_op=%q — the cause must go with the window it "+
			"describes; a cause outliving its epoch is worse than none", after.RefocusOp)
	}
}

func TestForceStopMemberClearsTheHandoverMarker(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	memberInAnOpenHandover(t, s, "m-killme")

	rec := httptest.NewRecorder()
	s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
		taskReq(t, "POST", "/api/members/m-killme/force-stop", nil, wireOwnerID, "owner"),
		"m-killme")

	after, _ := s.dal.GetMember("m-killme")
	if after.RefocusSince != 0 {
		t.Errorf("強制停止 left refocus_since=%v. Nothing is being waited for any more — "+
			"the session is gone — which is the reason the worker force-stop zeroes it "+
			"in the same write.", after.RefocusSince)
	}
	if after.RefocusOp != "" {
		t.Errorf("強制停止 left refocus_op=%q", after.RefocusOp)
	}
	if after.ForcedStopAt <= 0 {
		t.Error("fixture check: force-stop must still stamp forced_stop_at — clearing " +
			"the handover marker may not take the durable cut-off record with it")
	}
}

// The consequence, end to end: 停止 → 活化 must not hand the fresh session the
// previous one's collect. Without the clear this is exactly the shape the code
// warned about — activate zeroes stopping/waking but NEITHER refocus_since nor
// stopped_since, so decideUp's recycle arm sees "marker present, dump done" and
// robust-stops the brand-new session on its first tick.
func TestStopThenActivateDoesNotCollectTheNextGeneration(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := memberInAnOpenHandover(t, s, "m-nextgen")
	m.StoppedSince = 1001.0 // the previous session answered its stopped-report
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-nextgen/deactivate", nil, wireOwnerID, "owner"),
		"m-nextgen")
	rec = httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-nextgen/activate", nil, wireOwnerID, "owner"),
		"m-nextgen")

	fresh, _ := s.dal.GetMember("m-nextgen")
	// The DECISION, not the dispatch: reconcileOne downgrades Command to "none"
	// when the frame cannot be enqueued, so a fixture whose warden happens to be
	// unreachable would make a Command assertion pass while the member is still
	// being collected. The reason text is what the arm decided.
	dec := s.reconcileOne(*fresh, newReconcileState(), 2000.0)
	if strings.HasPrefix(dec.Reason, "recycle:") {
		t.Fatalf("the tick after 停止 → 活化 decided %q — that is the PREVIOUS epoch's "+
			"collect landing on a session that has nothing to do with it", dec.Reason)
	}
}

func TestDeactivateOfflineMemberCollectsAndBanks(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	connectOnline(t, s, "mach-a")
	m := testAgent("m-offline-stop")
	m.DesiredMachineID = "mach-a"
	m.RefocusSince = 1000.0
	m.RefocusOp = refocusOpRefocus
	putTestMember(t, s, m)
	s.telemetry.Set(m.ID, map[string]any{"cost": 3.25})

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-offline-stop/deactivate", nil, wireOwnerID, "owner"),
		"m-offline-stop")

	if rec.Code != 200 {
		t.Fatalf("offline 下線: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember(m.ID)
	if after.StoppedSince <= 0 {
		t.Fatalf("offline 下線 must latch stopped_since: %+v", after)
	}
	if after.BankedCost != 3.25 {
		t.Fatalf("offline 下線 banked_cost = %v, want 3.25", after.BankedCost)
	}
	if _, ok := s.telemetry.Get(m.ID)["cost"]; ok {
		t.Fatal("offline 下線 left live cost telemetry behind")
	}
	frames := drainFrames(t, s, "mach-a")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStop {
		t.Fatalf("offline 下線 frames = %+v, want one stop", frames)
	}
}
