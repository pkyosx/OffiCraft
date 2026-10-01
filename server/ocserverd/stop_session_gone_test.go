package main

// A 停止 whose session drops without a stopped report is collected by the tick
// once the session has stayed offline for the whole confirm window (120 s, owner
// ruling rc-dbee69264859) — the same judgement for a staff member (member tick)
// and an outsource worker (outsource tick). Every case drives the real verbs
// over HTTP and the real ticks, and reconnects through the real /api/events
// stream where the SSE connect edge is what is being exercised.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// openTestEventStream holds a real GET /api/events stream for id until the
// returned func is called, which ends it the way a dropped session does.
func openTestEventStream(t *testing.T, api *apiServer, id string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(context.WithValue(ctx, claimsContextKey,
		map[string]any{"sub": id, "scope": "agent"}))
	done := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(newFailAfterWrites(1<<30), req)
		close(done)
	}()
	closed := false
	end := func() {
		if closed {
			return
		}
		closed = true
		cancel()
		<-done
	}
	t.Cleanup(end)
	waitFor(t, id+"'s SSE stream to project it online", func() bool { return api.hub.IsOnline(id) })
	return end
}

// stoppedStaffAfterDisconnect: kip, pinned to the server's own warden (online),
// was asked to 停止 while connected and has just dropped off without reporting.
func stoppedStaffAfterDisconnect(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	if err := d.SetMemberDesiredMachineID("kip", ServerSelfHost); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	apiTestListen(t, api, ServerSelfHost)
	session, err := api.hub.Connect("kip", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
		t.Fatalf("deactivate: %d %v", status, data)
	}
	api.hub.Disconnect(session)
	wsWantWardenFrames(t, api, ServerSelfHost)
	return api, h, d, owner
}

func wantStaffStop(t *testing.T, d *DAL, label string, stoppedSince float64) {
	t.Helper()
	m := reconcileTestRow(t, d, "kip")
	apiWantValue(t, label+": desired_state", any(m.DesiredState), any(DesiredStateOffline))
	apiWantValue(t, label+": stopped_since", any(m.StoppedSince), any(stoppedSince))
}

func TestAStoppedStaffMemberWhoseSessionDroppedIsCollectedAfterTheConfirmWindow(t *testing.T) {
	t.Run("inside the window nothing is collected or sent; at the window the close-out is latched and the residual session is stopped, once", func(t *testing.T) {
		api, _, d, _ := stoppedStaffAfterDisconnect(t)
		t0 := nowSecs()

		api.runReconcileTick(t0)
		api.runReconcileTick(t0 + 119)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "one second short of the window", 0)
		apiWantValue(t, "phase inside the window", any(reconcileTestState(api, "kip").Phase), any(reconcilePhaseStopping))

		dashboard := apiTestListen(t, api, "")
		api.runReconcileTick(t0 + 120)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wantStaffStop(t, d, "at the window", t0+120)
		payload := apiTestMemberPayload("kip", "Kip", "active", "offline")
		payload["offboard_notice"] = apiAnyString
		dashboard.wantFrames(apiTestMemberFrame(2, "patch", "kip", payload, "server"))

		api.runReconcileTick(t0 + 300)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "a later tick", t0+120)
		apiWantValue(t, "phase after the collect", any(reconcileTestState(api, "kip").Phase), any(reconcilePhaseOffline))
	})

	t.Run("a reconnect inside the window is a blip: the window starts again from the next disconnect", func(t *testing.T) {
		api, _, d, _ := stoppedStaffAfterDisconnect(t)
		t0 := nowSecs()

		api.runReconcileTick(t0)
		back := apiTestListen(t, api, "kip")
		api.runReconcileTick(t0 + 60)
		api.hub.Disconnect(back.l)
		api.runReconcileTick(t0 + 130)
		api.runReconcileTick(t0 + 249)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "one second short of the new window", 0)

		api.runReconcileTick(t0 + 250)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wantStaffStop(t, d, "at the new window", t0+250)
	})

	t.Run("a reconnect and drop over the SSE stream with no tick in between starts a fresh window", func(t *testing.T) {
		api, _, d, _ := stoppedStaffAfterDisconnect(t)
		t0 := nowSecs()

		api.runReconcileTick(t0)
		end := openTestEventStream(t, api, "kip")
		end()
		api.runReconcileTick(t0 + 120)
		api.runReconcileTick(t0 + 239)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "one second short of a window from the new disconnect", 0)

		api.runReconcileTick(t0 + 240)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wantStaffStop(t, d, "a window from the new disconnect", t0+240)
	})

	t.Run("a member the owner force-stopped after it dropped is not collected by the window: force-stop already sent its kill", func(t *testing.T) {
		api, h, d, owner := stoppedStaffAfterDisconnect(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`); status != 200 {
			t.Fatalf("force-stop: %d %v", status, data)
		}
		wsDrainWardenFrames(t, api, ServerSelfHost)
		t0 := nowSecs()

		api.runReconcileTick(t0)
		api.runReconcileTick(t0 + 500)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "well past the window", 0)
	})

	t.Run("a latch that does not land sends no STOP; the next tick collects", func(t *testing.T) {
		api, _, d, _ := stoppedStaffAfterDisconnect(t)
		t0 := nowSecs()
		api.runReconcileTick(t0)
		apiTestFailStoppedAnchorWrite(t, d, "kip")

		api.runReconcileTick(t0 + 120)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "the failed latch", 0)

		apiTestRestoreStoppedAnchorWrite(t, d)
		api.runReconcileTick(t0 + 121)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wantStaffStop(t, d, "the retry", t0+121)
	})

	t.Run("an anchor from an earlier stop does not survive 活化 and a reconnect: the next stop waits a full window from its own disconnect", func(t *testing.T) {
		api, h, d, owner := stoppedStaffAfterDisconnect(t)
		t0 := nowSecs()
		api.runReconcileTick(t0)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		end := openTestEventStream(t, api, "kip")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/deactivate", owner, `{}`); status != 200 {
			t.Fatalf("second deactivate: %d %v", status, data)
		}
		end()
		wsDrainWardenFrames(t, api, ServerSelfHost)

		api.runReconcileTick(t0 + 130)
		api.runReconcileTick(t0 + 249)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantStaffStop(t, d, "one second short of a window from the new disconnect", 0)

		api.runReconcileTick(t0 + 250)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wantStaffStop(t, d, "a window from the new disconnect", t0+250)
	})
}

// stoppedWorkerAfterDisconnect is the outsource twin of stoppedStaffAfterDisconnect.
func stoppedWorkerAfterDisconnect(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
	if err := d.SetMemberDesiredMachineID("ow-abc123", ServerSelfHost); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	apiTestListen(t, api, ServerSelfHost)
	session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); status != 200 {
		t.Fatalf("deactivate: %d %v", status, data)
	}
	api.hub.Disconnect(session)
	wsWantWardenFrames(t, api, ServerSelfHost)
	return api, h, d, owner
}

func wantWorkerStop(t *testing.T, d *DAL, label string, collected bool) {
	t.Helper()
	m := apiTestMemberRow(t, d, "ow-abc123")
	apiWantValue(t, label+": desired_state", any(m.DesiredState), any(DesiredStateOffline))
	apiWantValue(t, label+": collected", any(m.StoppedSince > 0), any(collected))
}

func TestAStoppedWorkerWhoseSessionDroppedIsCollectedAfterTheConfirmWindow(t *testing.T) {
	t.Run("a reconnect inside the window is a blip: the window starts again from the next disconnect", func(t *testing.T) {
		api, _, d, _ := stoppedWorkerAfterDisconnect(t)
		t0 := nowSecs()

		api.runOutsourceTick(t0)
		back := apiTestListen(t, api, "ow-abc123")
		api.runOutsourceTick(t0 + 60)
		api.hub.Disconnect(back.l)
		api.runOutsourceTick(t0 + 130)
		api.runOutsourceTick(t0 + 249)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantWorkerStop(t, d, "one second short of the new window", false)

		api.runOutsourceTick(t0 + 250)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		wantWorkerStop(t, d, "at the new window", true)
	})

	t.Run("a worker the owner force-stopped after it dropped is not collected by the window: force-stop already sent its kill", func(t *testing.T) {
		api, h, d, owner := stoppedWorkerAfterDisconnect(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, ""); status != 200 {
			t.Fatalf("force-stop: %d %v", status, data)
		}
		wsDrainWardenFrames(t, api, ServerSelfHost)
		t0 := nowSecs()

		api.runOutsourceTick(t0)
		api.runOutsourceTick(t0 + 500)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantWorkerStop(t, d, "well past the window", false)
	})

	t.Run("an anchor from an earlier stop does not survive 喚醒 and a reconnect: the next stop waits a full window from its own disconnect", func(t *testing.T) {
		api, h, d, owner := stoppedWorkerAfterDisconnect(t)
		t0 := nowSecs()
		api.runOutsourceTick(t0)
		if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, `{}`); status != 200 {
			t.Fatalf("activate: %d %v", status, data)
		}
		end := openTestEventStream(t, api, "ow-abc123")
		if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); status != 200 {
			t.Fatalf("second deactivate: %d %v", status, data)
		}
		end()
		wsDrainWardenFrames(t, api, ServerSelfHost)

		api.runOutsourceTick(t0 + 130)
		api.runOutsourceTick(t0 + 249)
		wsWantWardenFrames(t, api, ServerSelfHost)
		wantWorkerStop(t, d, "one second short of a window from the new disconnect", false)

		api.runOutsourceTick(t0 + 250)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		wantWorkerStop(t, d, "a window from the new disconnect", true)
	})
}
