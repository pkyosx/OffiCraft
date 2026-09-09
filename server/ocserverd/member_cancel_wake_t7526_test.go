package main

// member_cancel_wake_t7526_test.go — 取消喚醒 (T-7526).
//
// THE REPORTED BEHAVIOUR (owner hit it himself and assumed he had misremembered):
// pressing 取消 on a WAKING member did nothing at all. The member finished
// booting, went green, and only ~120s later did anything actually stop it.
//
// THE MECHANISM, in two layers:
//  1. deactivate writes desired_state=offline and leans on the reconcile
//     cadence. decideDown's FIRST branch is `if !obs.Online { converged }`, and a
//     waking member is BY DEFINITION not online (deriveLiveness projects waking
//     only when !Online) — so the cadence dispatched NOTHING against the process
//     the earlier START had already put on the machine.
//  2. that process then booted and called report_waking, which zeroed
//     stopping_since — erasing the only trace the cancel had left, so the panel
//     painted a live green member the owner had explicitly told to stop.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// wakingMember seeds a member in the WAKING projection: the owner wants it up,
// a START has been dispatched (fresh waking_since), and it is not connected yet.
func wakingMember(t *testing.T, s *apiServer, id, machine string) Member {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = machine
	m.WakingSince = nowSecs()
	putTestMember(t, s, m)
	if got := PresenceState(m, nowSecs(), s.hub.IsOnline(id)); got != MemberPresenceWaking {
		t.Fatalf("fixture must be in the waking projection, got %q", got)
	}
	return m
}

// TestHandleDeactivateMember_CancellingAWakeDispatchesAStop — the repro turned
// guard. Pre-fix this drains ZERO frames: nothing was ever sent, which is the
// whole defect.
func TestHandleDeactivateMember_CancellingAWakeDispatchesAStop(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	connectOnline(t, s, "mach-a")
	wakingMember(t, s, "m-wake-cancel", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-wake-cancel/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-wake-cancel")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}

	sawStop := false
	for _, f := range drainFrames(t, s, "mach-a") {
		if f.RPC == reconcileCmdStop && f.Args["member_id"] == "m-wake-cancel" {
			sawStop = true
		}
	}
	if !sawStop {
		t.Fatal("cancelling a WAKING member must dispatch a robust STOP now — " +
			"the cadence cannot, decideDown treats !online as already converged, " +
			"so the booting process would come up anyway")
	}
}

// TestHandleDeactivateMember_OnlineMemberKeepsTheGracefulGrace — the negative
// control for the fix above. A LIVE member's stop must still dispatch NOTHING
// at handler time: it has work in hand and the 120s graceful window is the whole
// point. Without this, "cancel dispatches now" could quietly be widened into
// "every stop is a force-stop".
func TestHandleDeactivateMember_OnlineMemberKeepsTheGracefulGrace(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	connectOnline(t, s, "mach-a")

	m := testAgent("m-live-stop")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, "m-live-stop", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-live-stop/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-live-stop")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}

	for _, f := range drainFrames(t, s, "mach-a") {
		if f.RPC == reconcileCmdStop && f.Args["member_id"] == "m-live-stop" {
			t.Fatal("a graceful stop of a LIVE member must dispatch nothing at " +
				"handler time — the grace window is the agent's chance to wind down")
		}
	}
}

// TestHandleDeactivateMember_OfflineMemberCollectsAndDispatchesStop covers the
// offline collection arm.
func TestHandleDeactivateMember_OfflineMemberCollectsAndDispatchesStop(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	connectOnline(t, s, "mach-a")

	m := testAgent("m-already-off")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
		taskReq(t, "POST", "/api/members/m-already-off/deactivate", map[string]any{},
			wireOwnerID, "owner"), "m-already-off")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}

	frames := drainFrames(t, s, "mach-a")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStop ||
		frames[0].Args["member_id"] != "m-already-off" {
		t.Fatalf("offline deactivate frames = %+v, want one stop for m-already-off", frames)
	}
}

// TestHandleReportWaking_KeepsTheStopTraceOfACancelledWake — layer 2. The agent
// that was already booting when the cancel landed reports waking; that report
// must not erase the owner's stop intent, or the panel paints a green member the
// owner explicitly told to stop.
func TestHandleReportWaking_KeepsTheStopTraceOfACancelledWake(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-cancelled-boot")
	m.DesiredState = DesiredStateOffline // the cancel already landed
	m.StoppingSince = nowSecs()
	m.WakingSince = nowSecs() - 5
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{},
			"m-cancelled-boot", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}

	got, _ := s.dal.GetMember("m-cancelled-boot")
	if got == nil || got.StoppingSince <= 0.0 {
		t.Fatalf("a boot report must NOT erase the stop intent of a member the "+
			"owner has already cancelled (desired_state=offline), got %+v", got)
	}
	// …and the projection agrees: with the trace kept, an agent that connects
	// reads 停止中, never a fresh green.
	if p := PresenceState(*got, nowSecs(), true); p != MemberPresenceStopping {
		t.Fatalf("want the cancelled-then-connected member to read stopping, got %q", p)
	}
}

// TestHandleReportWaking_ClearsTheStopTraceOnAnOrdinaryBoot — the negative
// control for the guard above: an ordinary wake (desired online) MUST still
// clear a stale stopping_since, or every member that was ever stopped would come
// back up looking like it is still winding down.
func TestHandleReportWaking_ClearsTheStopTraceOnAnOrdinaryBoot(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-ordinary-boot")
	m.DesiredState = DesiredStateOnline
	m.StoppingSince = nowSecs() - 900 // a stale anchor from an earlier stop
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{},
			"m-ordinary-boot", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}

	got, _ := s.dal.GetMember("m-ordinary-boot")
	if got == nil || got.StoppingSince != 0.0 {
		t.Fatalf("an ordinary boot must clear the stale stop anchor, got %+v", got)
	}
}

// TestDeactivateMember_StaysAdminGatedAfterTheCancelDispatch — the governance
// negative control. Cancelling a wake now performs a FORCE-STOP-grade dispatch
// (the immediate robust STOP that bypasses the grace clock) from inside the
// deactivate handler, so the deactivate row must not be a cheaper way to reach
// it. Pinned through the FULL wired stack: a plain agent is a flat 403 before
// the handler resolves anything, and the two rows sit at the same floor.
func TestDeactivateMember_StaysAdminGatedAfterTheCancelDispatch(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")

	req, _ := http.NewRequest("POST", srv.URL+"/api/members/mira/deactivate", nil)
	req.Header.Set("Authorization", "Bearer "+agentTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("agent on the admin-gated deactivate row: want 403 envelope, got %d %s",
			resp.StatusCode, body)
	}

	// …and the floor is the SAME one force-stop sits at: the cancel path may not
	// be reachable by anyone who could not already force-stop.
	var deactivate, forceStop *RouteSpec
	for i := range defaultRouteSpecs() {
		spec := defaultRouteSpecs()[i]
		switch spec.Path {
		case "/api/members/{member_id}/deactivate":
			s := spec
			deactivate = &s
		case "/api/members/{member_id}/force-stop":
			s := spec
			forceStop = &s
		}
	}
	if deactivate == nil || forceStop == nil {
		t.Fatal("both rows must exist in the route table")
	}
	if deactivate.Requires != forceStop.Requires {
		t.Fatalf("deactivate (%q) must not sit below force-stop (%q) — the cancel "+
			"path dispatches the same robust STOP",
			deactivate.Requires, forceStop.Requires)
	}
}
