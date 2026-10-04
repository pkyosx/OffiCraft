package main

import (
	"net/http"
	"testing"
)

// The robust-stop ledger is one record for both populations: these tests drive
// it only through the handlers that arm it, the receipt ingest that closes it
// and the two ticks that re-send it, and read only warden frames and rows.

// slLiveStaff is kip wanted online, pinned to the server's own warden, with
// that warden reachable and a live session there. Answers the session, the
// session's own credential and the owner's.
func slLiveStaff(t *testing.T) (*apiServer, http.Handler, *DAL, string, *hubListener, string) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	if status, data := apiJSON(t, h, "POST", "/api/members/kip/activate", owner, `{}`); status != 200 {
		t.Fatalf("activate: %d %v", status, data)
	}
	if err := d.SetMemberDesiredMachineID("kip", ServerSelfHost); err != nil {
		t.Fatalf("SetMemberDesiredMachineID: %v", err)
	}
	apiTestListen(t, api, ServerSelfHost)
	session, err := api.hub.Connect("kip", ServerSelfHost)
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(session) })
	api.hub.DrainWardenCommands(ServerSelfHost)
	return api, h, d, owner, session, apiTestAgentToken(t, api, "kip", ServerSelfHost)
}

func slNoSuchSession(t *testing.T, api *apiServer, h http.Handler, reporter, id string) {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry",
		apiTestAgentToken(t, api, reporter, ""),
		`{"command_result":{"rpc":"stop","member_id":"`+id+`","ok":true,`+
			`"reason":"no_such_session: stop was a no-op","log":"no session"}}`)
	if status != 200 {
		t.Fatalf("receipt: %d %v", status, data)
	}
}

func TestAStaffStopAimedAtAnUnreachableWardenIsParkedAndFiredWhenThatWardenConnects(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	reconcileTestPut(t, d, Member{ID: "m-box", Name: "Box", Kind: KindWarden})
	reconcileTestPut(t, d, Member{
		ID: "stranded", Name: "Stranded", Kind: KindStaff, RoleKey: "assistant",
		DesiredState: DesiredStateOnline, DesiredMachineID: "m-box",
	})
	// The session still runs on m-box, whose warden has lost its downstream.
	reconcileTestOnline(t, api, "stranded", "m-box")

	if status, data := apiJSON(t, h, "POST", "/api/members/stranded/force-stop", owner, `{}`); status != 200 {
		t.Fatalf("force-stop: %d %v", status, data)
	}
	now := nowSecs()
	api.runReconcileTick(now + 1)
	if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
		t.Fatalf("nothing can land on a dark warden, it holds %d frame(s)", n)
	}

	reconcileTestOnline(t, api, "m-box", "")
	api.runReconcileTick(now + 2)
	wsWantWardenFrames(t, api, "m-box", wsStopFrame("stranded"))

	// Fired once it landed; inside stop_retry the next tick owes nothing more.
	api.runReconcileTick(now + 3)
	wsWantWardenFrames(t, api, "m-box")
}

func TestANoSuchSessionReceiptFromTheAimedWardenEndsAStaffStopsResends(t *testing.T) {
	t.Run("the aimed warden answering no_such_session ends the re-sends even while the connection lingers", func(t *testing.T) {
		api, h, _, owner, _, _ := slLiveStaff(t)
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`); status != 200 {
			t.Fatalf("force-stop: %d %v", status, data)
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		slNoSuchSession(t, api, h, ServerSelfHost, "kip")

		api.runReconcileTick(nowSecs() + api.reconcileConfigLive().StopRetry + 1)
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("CONTROL: a bystander's no_such_session does not, and past stop_retry the stop goes out again", func(t *testing.T) {
		api, h, d, owner, _, _ := slLiveStaff(t)
		shutdownWarden(t, api, d, "m-bystander")
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/force-stop", owner, `{}`); status != 200 {
			t.Fatalf("force-stop: %d %v", status, data)
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		slNoSuchSession(t, api, h, "m-bystander", "kip")

		api.runReconcileTick(nowSecs() + api.reconcileConfigLive().StopRetry + 1)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("kip"))
		wsWantWardenFrames(t, api, "m-bystander")
	})
}

func TestAWorkerOwingARobustStopIsNotStartedOrBenchedWhileItsSessionLingersOnTheAimedMachine(t *testing.T) {
	api, h, d, owner, session, contractor := apiTestLiveWorker(t)
	if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
		t.Fatalf("stopped: %d %v", status, data)
	}
	wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
	now := nowSecs()
	retry := api.reconcileConfigLive().StopRetry

	api.runOutsourceTick(now + 1)
	wsWantWardenFrames(t, api, ServerSelfHost)

	api.runOutsourceTick(now + retry + 1)
	wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
	row := apiTestWorkerRecord(t, d, "ow-abc123")
	apiWantValue(t, "the worker row", any(map[string]any{
		"status": row.Status, "desired_state": row.DesiredState, "last_op_reason": row.LastOpReason,
	}), any(map[string]any{"status": "active", "desired_state": "online", "last_op_reason": ""}))

	// The session finally goes: the same machine is started at once, so nothing
	// was benched and nothing is owed any more.
	api.hub.Disconnect(session)
	api.runOutsourceTick(now + retry + 2)
	wsWantWardenFrames(t, api, ServerSelfHost,
		wsStartFrame("ow-abc123", apiTestWorkerBootContext(t, h, owner), "claude", "sonnet", "medium"))
}

func TestStaffAndWorkerGetTheSameFramesWhenTheSessionLingersPastStopRetryAfterAStoppedReport(t *testing.T) {
	type tick func(now float64)
	run := func(t *testing.T, id, token string, h http.Handler, api *apiServer, tk tick) [][]map[string]any {
		t.Helper()
		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", token, `{}`); status != 200 {
			t.Fatalf("stopped: %d %v", status, data)
		}
		now := nowSecs()
		retry := api.reconcileConfigLive().StopRetry
		out := [][]map[string]any{wsDrainWardenFrames(t, api, ServerSelfHost)}
		for _, at := range []float64{now + 1, now + retry + 1, now + retry + 2, now + 2*retry + 2} {
			tk(at)
			out = append(out, wsDrainWardenFrames(t, api, ServerSelfHost))
		}
		for _, frames := range out {
			for _, f := range frames {
				f["subject"] = "<subject>"
				f["data"].(map[string]any)["args"].(map[string]any)["member_id"] = "<subject>"
			}
		}
		return out
	}
	staffAPI, staffH, _, _, _, staffToken := slLiveStaff(t)
	staff := run(t, "kip", staffToken, staffH, staffAPI, staffAPI.runReconcileTick)
	workerAPI, workerH, _, _, _, workerToken := apiTestLiveWorker(t)
	worker := run(t, "ow-abc123", workerToken, workerH, workerAPI, workerAPI.runOutsourceTick)

	stop := wsStopFrame("<subject>")
	want := []any{
		[]any{stop},
		[]any{},
		[]any{stop},
		[]any{},
		[]any{stop},
	}
	toAny := func(in [][]map[string]any) []any {
		out := []any{}
		for _, frames := range in {
			row := []any{}
			for _, f := range frames {
				row = append(row, f)
			}
			out = append(out, row)
		}
		return out
	}
	apiWantValue(t, "staff frames", any(toAny(staff)), any(want))
	apiWantValue(t, "worker frames", any(toAny(worker)), any(want))
}

func TestAWorkersOwedStopIsClosedOnlyByTheAimedMachinesNoSuchSession(t *testing.T) {
	// slReported is the live worker after its stopped report: a STOP aimed at the
	// server's own warden, the session still connected there.
	slReported := func(t *testing.T) (*apiServer, http.Handler, *DAL, float64) {
		t.Helper()
		api, h, d, _, _, contractor := apiTestLiveWorker(t)
		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
			t.Fatalf("stopped: %d %v", status, data)
		}
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		return api, h, d, nowSecs() + api.reconcileConfigLive().StopRetry + 1
	}

	t.Run("the aimed machine's no_such_session ends the re-sends", func(t *testing.T) {
		api, h, _, late := slReported(t)
		slNoSuchSession(t, api, h, ServerSelfHost, "ow-abc123")
		api.runOutsourceTick(late)
		wsWantWardenFrames(t, api, ServerSelfHost)
	})

	t.Run("CONTROL: a bystander's no_such_session, or one from an unidentified reporter, ends nothing", func(t *testing.T) {
		api, h, d, late := slReported(t)
		shutdownWarden(t, api, d, "m-bystander")
		slNoSuchSession(t, api, h, "m-bystander", "ow-abc123")
		api.foldCommandResult(map[string]any{
			"member_id": "ow-abc123", "rpc": "stop", "ok": true,
			"reason": "no_such_session: stop was a no-op",
		}, "telemetry", "")
		api.runOutsourceTick(late)
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, "m-bystander")
	})

	t.Run("a broadcast is closed by no receipt at all: every machine it reached is a bystander", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestWorkerWantedOnline(t, d, "ow-abc123")
		shutdownWarden(t, api, d, "m-one")
		shutdownWarden(t, api, d, "m-two")
		contractor := apiTestAgentToken(t, api, "ow-abc123", "")
		if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
			t.Fatalf("stopped: %d %v", status, data)
		}
		late := nowSecs() + api.reconcileConfigLive().StopRetry + 1
		wsVerbs(t, api, "m-one")
		wsVerbs(t, api, "m-two")
		slNoSuchSession(t, api, h, "m-one", "ow-abc123")
		slNoSuchSession(t, api, h, "m-two", "ow-abc123")

		if _, err := api.hub.Connect("ow-abc123", ""); err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		api.runOutsourceTick(late)
		wsWantWardenFrames(t, api, "m-one", wsStopFrame("ow-abc123"))
		wsWantWardenFrames(t, api, "m-two", wsStopFrame("ow-abc123"))
	})
}

func TestAWorkerBackOnAnotherMachineRetiresTheStopAimedAtTheOldOne(t *testing.T) {
	api, h, d, _, session, contractor := apiTestLiveWorker(t)
	shutdownWarden(t, api, d, "m-elsewhere")
	if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
		t.Fatalf("stopped: %d %v", status, data)
	}
	wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
	late := nowSecs() + api.reconcileConfigLive().StopRetry + 1

	// The id is online again, but on another machine: the session the STOP was
	// aimed at is gone.
	api.hub.Disconnect(session)
	moved, err := api.hub.Connect("ow-abc123", "m-elsewhere")
	if err != nil {
		t.Fatalf("hub.Connect: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(moved) })
	api.runOutsourceTick(late)
	wsWantWardenFrames(t, api, ServerSelfHost)
	wsWantWardenFrames(t, api, "m-elsewhere")
}

func TestAStaffWakeStartRetiresTheStopItSentAheadOfIt(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	reconcileTestPut(t, d, Member{ID: "m-box", Name: "Box", Kind: KindWarden})
	api.telemetry.Set("m-box", map[string]any{"runtimes": map[string]any{
		"claude": map[string]any{"installed": true, "logged_in": true},
	}})
	reconcileTestPut(t, d, Member{
		ID: "sleeper", Name: "Sleeper", Kind: KindStaff, RoleKey: "assistant",
		DesiredState: DesiredStateOffline, DesiredMachineID: "m-box", LastMachineID: "m-box",
	})
	shutdownBootable(t, d, "sleeper", "m-box")
	reconcileTestOnline(t, api, "m-box", "")

	// 喚醒 clears whatever the last landing may still hold, then starts.
	if status, data := apiJSON(t, h, "POST", "/api/members/sleeper/activate", owner, `{}`); status != 200 {
		t.Fatalf("activate: %d %v", status, data)
	}
	apiWantValue(t, "the warden's queue", any(wsVerbs(t, api, "m-box")), any([]any{"stop", "start"}))

	// The new session comes up on that machine and lives past stop_retry: the stop
	// that went ahead of its START is not owed against it.
	reconcileTestOnline(t, api, "sleeper", "m-box")
	api.runReconcileTick(nowSecs() + api.reconcileConfigLive().StopRetry + 1)
	apiWantValue(t, "later frames", any(wsVerbs(t, api, "m-box")), any([]any{}))
}
