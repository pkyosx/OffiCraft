// Skeleton generated from server/ocserverd/worker_spawn.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// wsWorkerSpawnFixture is the shared start state of this file: a claimed
// server, the seeded roster, one live task and one outsource worker bound to it
// — the state the scheduler hands the wake path.
func wsWorkerSpawnFixture(t *testing.T, status string) (*apiServer, http.Handler, *DAL, string, OutsourceWorker) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", status)
	w, err := d.GetOutsourceWorker("ow-abc123")
	if err != nil || w == nil {
		t.Fatalf("GetOutsourceWorker: %v (%v)", w, err)
	}
	return api, h, d, owner, *w
}

// wsDrainWardenFrames empties machineID's warden command FIFO — the same queue
// that machine's SSE loop collects from — and decodes each frame's envelope,
// with the queue entry's own subject folded in under "subject" so a caller
// compares the WHOLE dispatch record and not only its payload.
func wsDrainWardenFrames(t *testing.T, api *apiServer, machineID string) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, cmd := range api.hub.DrainWardenCommands(machineID) {
		body, ok := strings.CutPrefix(strings.TrimSuffix(string(cmd.Frame), "\n\n"), "data: ")
		if !ok {
			t.Fatalf("warden frame is not SSE wire text: %q", cmd.Frame)
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatalf("warden frame data is not JSON (%q): %v", body, err)
		}
		frame["subject"] = cmd.Subject
		out = append(out, frame)
	}
	return out
}

// wsWantFrames asserts a drained FIFO held EXACTLY these frames, in order, each
// compared field by field the way apiWantBody compares a response body. Passing
// none asserts nothing was enqueued at all.
func wsWantFrames(t *testing.T, got []map[string]any, want ...map[string]any) {
	t.Helper()
	gotAny := make([]any, len(got))
	for i := range got {
		gotAny[i] = got[i]
	}
	wantAny := make([]any, len(want))
	for i := range want {
		wantAny[i] = want[i]
	}
	apiWantValue(t, "warden commands", any(gotAny), any(wantAny))
}

// wsWantWardenFrames is wsWantFrames straight off the queue, for a test that
// does not need the decoded frames for anything else.
func wsWantWardenFrames(t *testing.T, api *apiServer, machineID string, want ...map[string]any) {
	t.Helper()
	wsWantFrames(t, wsDrainWardenFrames(t, api, machineID), want...)
}

// wsStopFrame is the member `stop` frame every worker kill path enqueues.
func wsStopFrame(workerID string) map[string]any {
	return map[string]any{
		"subject": workerID,
		"topic":   "warden-command",
		"data": map[string]any{
			"rpc":  "stop",
			"args": map[string]any{"member_id": workerID},
		},
	}
}

// wsStartFrame is the member `start` frame the wake path enqueues. persona is
// the boot context the frame must carry verbatim; the session token is minted
// per dispatch and is asserted by using it, not by writing it down.
func wsStartFrame(workerID, persona, runtime, model, effort string) map[string]any {
	return map[string]any{
		"subject": workerID,
		"topic":   "warden-command",
		"data": map[string]any{
			"rpc": "start",
			"args": map[string]any{
				"member_id":       workerID,
				"persona_context": persona,
				"member_token":    apiAnyString,
				"role":            "outsource-worker",
				"runtime":         runtime,
				"model":           model,
				"effort":          effort,
				"session_name":    "",
			},
		},
	}
}

func TestBuildWorkerBootContext(t *testing.T) {
	t.Run("the assembled context is exactly what the cockpit's boot-context preview serves for the same worker", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v (%v)", task, err)
		}

		got, err := api.buildWorkerBootContext(w, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("boot-context preview: %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"context": got})
	})

	t.Run("neither the bound task nor the type manual reaches the text, so two workers on different tasks boot on the same words", func(t *testing.T) {
		api, _, d, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v (%v)", task, err)
		}

		base, err := api.buildWorkerBootContext(w, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		other := *task
		other.ID = "T-9"
		other.Title = "Unload the reefer"
		other.TypeKey = "customs"
		withManual, err := api.buildWorkerBootContext(w, other, &TaskManual{
			TypeKey: "customs", DisplayName: "Customs", Purpose: "clear it",
			SopMD: "# SOP\nstep one", Learnings: "watch the tariff codes",
		})
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		apiWantValue(t, "context with another task and a manual", any(withManual), any(base))

		stranger := w
		stranger.ID = "ow-def456"
		stranger.Codename = "Stevedore"
		stranger.Model = "opus"
		stranger.Effort = "high"
		forStranger, err := api.buildWorkerBootContext(stranger, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		apiWantValue(t, "context for another worker", any(forStranger), any(base))
	})

	t.Run("the worker's own runtime picks slot 4, so a codex worker is handed different words than a claude one", func(t *testing.T) {
		api, _, d, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v (%v)", task, err)
		}

		claude, err := api.buildWorkerBootContext(w, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		codexWorker := w
		codexWorker.Runtime = "codex"
		codex, err := api.buildWorkerBootContext(codexWorker, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		if codex == claude {
			t.Fatalf("the codex fold must not be the claude fold; both are %d chars", len(claude))
		}
		blankRuntime := w
		blankRuntime.Runtime = ""
		normalized, err := api.buildWorkerBootContext(blankRuntime, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		apiWantValue(t, "context for an unnamed runtime", any(normalized), any(claude))
	})

	t.Run("the owner's additive block lands in slot 2, between the shared seed and the runtime's boot sequence", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v (%v)", task, err)
		}
		before, err := api.buildWorkerBootContext(w, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/global-context", owner,
			`{"text":"Studio rule: never touch the bonded rack"}`)
		if status != 200 {
			t.Fatalf("replace global context: %d (%v)", status, data)
		}
		after, err := api.buildWorkerBootContext(w, *task, nil)
		if err != nil {
			t.Fatalf("buildWorkerBootContext: %v", err)
		}
		head, tail, found := strings.Cut(after,
			"# 使用者自訂（Owner Additions）\n\nStudio rule: never touch the bonded rack\n\n")
		if !found {
			t.Fatalf("the owner's block is not in the assembled context (%d chars)", len(after))
		}
		apiWantValue(t, "the text either side of the owner block", any(head+tail), any(before))
	})
}

func TestPickWorkerWarden(t *testing.T) {
	t.Run("an online machine that can run the worker's runtime is the machine the session boots on", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		apiWantValue(t, "picked warden", any(api.pickWorkerWarden(w, ServerSelfHost, 1000)), any(ServerSelfHost))
	})

	t.Run("a machine that cannot take the worker names nothing at all, because there is no automatic placement to fall back on", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		apiWantValue(t, "picked warden with the machine offline", any(api.pickWorkerWarden(w, ServerSelfHost, 1000)), any(""))
		apiWantValue(t, "picked warden with no machine named", any(api.pickWorkerWarden(w, "", 1000)), any(""))
	})
}

func TestResolveWorkerPlacement(t *testing.T) {
	// wantPlacement asserts BOTH halves of the verdict: the machine and the
	// sentence the cockpit prints when there is none.
	wantPlacement := func(t *testing.T, gotID, gotWhy, wantID, wantWhy string) {
		t.Helper()
		apiWantValue(t, "placement", any(gotID), any(wantID))
		apiWantValue(t, "placement reason", any(gotWhy), any(wantWhy))
	}

	t.Run("an online active warden that provides the runtime resolves, and the reason is empty exactly then", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		id, why := api.resolveWorkerPlacement(w, ServerSelfHost, 1000)
		wantPlacement(t, id, why, ServerSelfHost, "")
	})

	t.Run("no machine named at all is refused as an unmade choice rather than as an unavailable machine", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		id, why := api.resolveWorkerPlacement(w, "", 1000)
		wantPlacement(t, id, why, "", "no_machine_selected: no machine is selected for this worker — "+
			"pick one on the worker (改機器) or on the task type's 手冊 assignee; "+
			"there is no automatic placement")
	})

	t.Run("a machine id the roster does not carry is refused naming that id", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		id, why := api.resolveWorkerPlacement(w, "m-nope", 1000)
		wantPlacement(t, id, why, "", "machine_unavailable: machine 'm-nope' does not exist; "+
			"no other machine is substituted")
	})

	t.Run("a roster id that is a staff member rather than a machine is refused as not an active machine", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		id, why := api.resolveWorkerPlacement(w, apiTestPlainAgentID, 1000)
		wantPlacement(t, id, why, "", "machine_unavailable: machine 'kip' is not an active machine; "+
			"no other machine is substituted")
	})

	t.Run("a machine that exists but holds no SSE connection is refused as offline", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		id, why := api.resolveWorkerPlacement(w, ServerSelfHost, 1000)
		wantPlacement(t, id, why, "", "machine_unavailable: machine 'm-server-self' is offline; "+
			"no other machine is substituted")
	})

	t.Run("a machine benched after a failed boot of THIS worker is refused, and the bench names that as the cause", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		api.benchWorkerMachine(w.ID, ServerSelfHost, 1000)

		id, why := api.resolveWorkerPlacement(w, ServerSelfHost, 1000)
		wantPlacement(t, id, why, "", "machine_unavailable: machine 'm-server-self' was just benched "+
			"after a failed boot of this worker; no other machine is substituted")

		lapsed, why := api.resolveWorkerPlacement(w, ServerSelfHost, 1360)
		wantPlacement(t, lapsed, why, ServerSelfHost, "")
	})

	t.Run("a warden with no reported capabilities is a claude warden only, so a codex worker is refused naming the runtime", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		codexWorker := w
		codexWorker.Runtime = "codex"

		id, why := api.resolveWorkerPlacement(codexWorker, ServerSelfHost, 1000)
		wantPlacement(t, id, why, "", "machine_unavailable: machine 'm-server-self' does not provide "+
			"the 'codex' runtime; no other machine is substituted")
	})
}

func TestResolveStickyWorkerPlacement(t *testing.T) {
	wantPlacement := func(t *testing.T, gotID, gotWhy, wantID, wantWhy string) {
		t.Helper()
		apiWantValue(t, "placement", any(gotID), any(wantID))
		apiWantValue(t, "placement reason", any(gotWhy), any(wantWhy))
	}
	const missing = "machine_unavailable: machine 'm-gone' does not exist; no other machine is substituted"

	t.Run("an owner pin is hard: an unusable pin stalls the spawn and the configured machine is never tried", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = "m-gone"
		pinned.LastMachineID = ServerSelfHost

		id, why := api.resolveStickyWorkerPlacement(pinned, ServerSelfHost, 1000)
		wantPlacement(t, id, why, "", missing)
	})

	t.Run("an owner pin outranks the last landing, so a relocate moves a worker that is happily running", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost
		pinned.LastMachineID = "m-gone"

		id, why := api.resolveStickyWorkerPlacement(pinned, "m-gone", 1000)
		wantPlacement(t, id, why, ServerSelfHost, "")
	})

	t.Run("with no pin the last landing wins over the configured machine, so a rebirth stays where it ran", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		landed := w
		landed.LastMachineID = ServerSelfHost

		id, why := api.resolveStickyWorkerPlacement(landed, "m-gone", 1000)
		wantPlacement(t, id, why, ServerSelfHost, "")
	})

	t.Run("the last landing is soft: an unusable one falls through to the configured machine instead of stranding the worker", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		landed := w
		landed.LastMachineID = "m-gone"

		id, why := api.resolveStickyWorkerPlacement(landed, ServerSelfHost, 1000)
		wantPlacement(t, id, why, ServerSelfHost, "")
	})

	t.Run("an unusable last landing with nowhere else to fall reports THAT machine, not an empty configuration", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		landed := w
		landed.LastMachineID = "m-gone"

		id, why := api.resolveStickyWorkerPlacement(landed, "", 1000)
		wantPlacement(t, id, why, "", missing)

		id, why = api.resolveStickyWorkerPlacement(landed, "m-gone", 1000)
		wantPlacement(t, id, why, "", missing)
	})

	t.Run("a worker that has never landed anywhere is placed by the configuration alone, so the 手冊 governs the birthplace", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		id, why := api.resolveStickyWorkerPlacement(w, ServerSelfHost, 1000)
		wantPlacement(t, id, why, ServerSelfHost, "")

		id, why = api.resolveStickyWorkerPlacement(w, "", 1000)
		wantPlacement(t, id, why, "", "no_machine_selected: no machine is selected for this worker — "+
			"pick one on the worker (改機器) or on the task type's 手冊 assignee; "+
			"there is no automatic placement")
	})
}

func TestStampWorkerOpReceipt(t *testing.T) {
	t.Run("stamping writes the whole five-column receipt onto the caller's own struct as a FAILED start", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-abc123", Codename: "Contractor", Status: WorkerStatusAssigned}

		stampWorkerOpReceipt(&w, "no_machine_selected: nothing was picked", 1234.5)

		if w.LastOpOK == nil {
			t.Fatalf("last_op_ok must be a written false, not an absent verdict: %#v", w)
		}
		apiWantValue(t, "receipt", any(map[string]any{
			"last_op": w.LastOp, "last_op_ok": *w.LastOpOK, "last_op_log": w.LastOpLog,
			"last_op_reason": w.LastOpReason, "last_op_at": w.LastOpAt,
			"id": w.ID, "codename": w.Codename, "status": w.Status,
		}), any(map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_log": "",
			"last_op_reason": "no_machine_selected: nothing was picked", "last_op_at": 1234.5,
			"id": "ow-abc123", "codename": "Contractor", "status": "assigned",
		}))
	})

	t.Run("a second stamp replaces the whole receipt, so a stale log line from the previous one cannot survive", func(t *testing.T) {
		w := OutsourceWorker{ID: "ow-abc123"}
		stampWorkerOpReceipt(&w, "machine_unavailable: offline", 100)
		w.LastOpLog = "warden said: boom"

		stampWorkerOpReceipt(&w, "wake_timeout: never came up", 200)

		apiWantValue(t, "receipt", any(map[string]any{
			"last_op": w.LastOp, "last_op_ok": *w.LastOpOK, "last_op_log": w.LastOpLog,
			"last_op_reason": w.LastOpReason, "last_op_at": w.LastOpAt,
		}), any(map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_log": "",
			"last_op_reason": "wake_timeout: never came up", "last_op_at": 200.0,
		}))
	})
}

func TestWakeTimeoutOverWardenReceipt(t *testing.T) {
	const wardenRefusal = "session_already_exists: a live session is holding the slot"
	const composed = wardenRefusal + " — the start window then lapsed, but that is NOT a " +
		"runtime failure: the previous session is still running and the warden refused " +
		"to stomp it, so nothing new was ever started. Do not go looking for a broken " +
		"runtime on that machine; deal with the live session — 重啟 this worker to " +
		"displace it, or stop it first."

	t.Run("a wake_timeout landing on a start row that carries the warden's refusal is composed onto it, keeping the warden's line in front", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "start", LastOpReason: wardenRefusal}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "wake_timeout: the runtime never reported")), any(composed))
	})

	t.Run("a second tick composing the same pair answers the row's own string, so the anti-churn compare writes nothing", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "start", LastOpReason: composed}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "wake_timeout: the runtime never reported")), any(composed))
	})

	t.Run("a reason that is not a wake_timeout passes through untouched even over the warden's refusal", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "start", LastOpReason: wardenRefusal}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "backoff: waiting out the ladder")),
			any("backoff: waiting out the ladder"))
	})

	t.Run("the bare code without its colon is not a wake_timeout stamp, so it passes through", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "start", LastOpReason: wardenRefusal}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "wake_timeout")), any("wake_timeout"))
	})

	t.Run("with no warden receipt to protect the wake_timeout is stamped exactly as before", func(t *testing.T) {
		apiWantValue(t, "reason over an empty row",
			any(wakeTimeoutOverWardenReceipt(OutsourceWorker{}, "wake_timeout: the runtime never reported")),
			any("wake_timeout: the runtime never reported"))
		apiWantValue(t, "reason over another server stamp",
			any(wakeTimeoutOverWardenReceipt(
				OutsourceWorker{LastOp: "start", LastOpReason: "backoff: waiting"},
				"wake_timeout: the runtime never reported")),
			any("wake_timeout: the runtime never reported"))
	})

	t.Run("the legacy worker_start verb is compared raw and therefore never composes, which is the documented blind spot", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "worker_start", LastOpReason: wardenRefusal}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "wake_timeout: the runtime never reported")),
			any("wake_timeout: the runtime never reported"))
	})

	t.Run("a clobber code without its colon does not trip the gate", func(t *testing.T) {
		row := OutsourceWorker{LastOp: "start", LastOpReason: "session_already_exists"}

		apiWantValue(t, "reason", any(wakeTimeoutOverWardenReceipt(row, "wake_timeout: the runtime never reported")),
			any("wake_timeout: the runtime never reported"))
	})
}

func TestStampWorkerPlacementBlocked(t *testing.T) {
	t.Run("a refused spawn writes the whole failed-start receipt onto the row and fans one worker delta", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		api.stampWorkerPlacementBlocked(&w, "no_machine_selected: nobody picked a machine", 500)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 500,
			"last_op_reason": "no_machine_selected: nobody picked a machine",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "server"))
		apiWantValue(t, "the caller's own snapshot", any(w.LastOp), any(""))
	})

	t.Run("the same cause re-stamped on the next tick changes nothing and fans nothing, so one stalled worker is not a permanent event stream", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.stampWorkerPlacementBlocked(&w, "no_machine_selected: nobody picked a machine", 500)
		dashboard := apiTestListen(t, api, "")

		api.stampWorkerPlacementBlocked(&w, "no_machine_selected: nobody picked a machine", 900)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 500,
			"last_op_reason": "no_machine_selected: nobody picked a machine",
		}))
		dashboard.wantFrames()
	})

	t.Run("a CHANGED cause is news: the whole receipt is replaced, timestamp included, and a delta goes out", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.stampWorkerPlacementBlocked(&w, "no_machine_selected: nobody picked a machine", 500)
		dashboard := apiTestListen(t, api, "")

		api.stampWorkerPlacementBlocked(&w, "machine_unavailable: machine 'm-server-self' is offline", 900)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 900,
			"last_op_reason": "machine_unavailable: machine 'm-server-self' is offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(3, "assigned", "server"))
	})

	t.Run("a worker released since the snapshot was loaded is left alone, so a stale copy cannot resurrect it", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		released := w
		released.Status = WorkerStatusReleased
		if err := api.dal.PutOutsourceWorker(released); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampWorkerPlacementBlocked(&w, "no_machine_selected: nobody picked a machine", 500)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "",
		}))
		dashboard.wantFrames()
	})

	t.Run("a worker whose row has vanished is left alone, because there is nothing left to explain", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")
		ghost := OutsourceWorker{ID: "ow-ghost", Codename: "Nobody", Status: WorkerStatusAssigned}

		api.stampWorkerPlacementBlocked(&ghost, "no_machine_selected: nobody picked a machine", 500)

		row, err := api.dal.GetOutsourceWorker("ow-ghost")
		if err != nil || row != nil {
			t.Fatalf("the stamp must not create a row: %v (%v)", row, err)
		}
		dashboard.wantFrames()
	})

	t.Run("a wake_timeout over the warden's clobber refusal is composed onto it rather than replacing it", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", nil, "",
			"session_already_exists: a live session is holding the slot", 400); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.stampWorkerPlacementBlocked(&w, "wake_timeout: the runtime never reported", 500)
		composed := "session_already_exists: a live session is holding the slot — the start " +
			"window then lapsed, but that is NOT a runtime failure: the previous session is " +
			"still running and the warden refused to stomp it, so nothing new was ever " +
			"started. Do not go looking for a broken runtime on that machine; deal with the " +
			"live session — 重啟 this worker to displace it, or stop it first."
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 500,
			"last_op_reason": composed,
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "server"))

		api.stampWorkerPlacementBlocked(&w, "wake_timeout: the runtime never reported", 900)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 500,
			"last_op_reason": composed,
		}))
		dashboard.wantFrames()
	})
}

func TestClearWorkerPlacementBlock(t *testing.T) {
	t.Run("a placement stamp is dropped down to a wordless start, keeping the timestamp that separates a stall an hour ago from one right now", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.stampWorkerPlacementBlocked(&w, "machine_unavailable: machine 'm-server-self' is offline", 500)
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerPlacementBlock("ow-abc123")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": nil, "last_op_at": 500, "last_op_reason": "",
		}))
		dashboard.wantFrames()
	})

	t.Run("a warden's own receipt is never touched, because a dispatch is an attempt and not an outcome", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", nil, "warden log line",
			"session_already_exists: a live session is holding the slot", 777); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerPlacementBlock("ow-abc123")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": nil, "last_op_at": 777,
			"last_op_log":    "warden log line",
			"last_op_reason": "session_already_exists: a live session is holding the slot",
		}))
		dashboard.wantFrames()
	})

	t.Run("a wake_timeout survives the clear, so the retry cannot erase why the previous start died", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.stampWorkerPlacementBlocked(&w, "wake_timeout: the runtime never reported", 888)

		api.clearWorkerPlacementBlock("ow-abc123")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 888,
			"last_op_reason": "wake_timeout: the runtime never reported",
		}))
	})

	t.Run("a row with nothing to clear, and an id nothing carries, are both left exactly as they were", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerPlacementBlock("ow-abc123")
		api.clearWorkerPlacementBlock("ow-nope")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})
}

func TestClearWorkerConvergedFailureReceipt(t *testing.T) {
	t.Run("a converged worker's red 最近操作 line is removed whole — all five columns — and the removal is fanned", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", nil, "warden log line",
			"wake_timeout: the runtime never reported", 888); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		snapshot, err := api.dal.GetOutsourceWorker("ow-abc123")
		if err != nil || snapshot == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", snapshot, err)
		}
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerConvergedFailureReceipt("ow-abc123", *snapshot)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "server"))
	})

	t.Run("a wordless red block — a verb and a time with no verdict — is a failure the panel paints, so it is cleared too", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", nil, "", "", 1001); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		snapshot, err := api.dal.GetOutsourceWorker("ow-abc123")
		if err != nil || snapshot == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", snapshot, err)
		}

		api.clearWorkerConvergedFailureReceipt("ow-abc123", *snapshot)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "offline",
		}))
	})

	t.Run("a SUCCESS receipt is never touched, and a second clear over an already blank row writes and fans nothing", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		succeeded := true
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", &succeeded, "", "", 999); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		snapshot, err := api.dal.GetOutsourceWorker("ow-abc123")
		if err != nil || snapshot == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", snapshot, err)
		}
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerConvergedFailureReceipt("ow-abc123", *snapshot)
		api.clearWorkerConvergedFailureReceipt("ow-abc123", OutsourceWorker{ID: "ow-abc123"})

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "offline",
			"last_op": "start", "last_op_ok": true, "last_op_at": 999,
		}))
		dashboard.wantFrames()
	})

	t.Run("a snapshot that carries no failure short-circuits before the query, so a receipt written since is left standing", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		if err := api.dal.SetMemberOpReceipt("ow-abc123", "start", nil, "",
			"wake_timeout: the runtime never reported", 888); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerConvergedFailureReceipt("ow-abc123", OutsourceWorker{ID: "ow-abc123"})

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "offline",
			"last_op": "start", "last_op_ok": nil, "last_op_at": 888,
			"last_op_reason": "wake_timeout: the runtime never reported",
		}))
		dashboard.wantFrames()
	})
}

func TestWorkerMachineCoolingOn(t *testing.T) {
	t.Run("a machine benched for this worker is cooling until the deadline, and is available again AT it", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.benchWorkerMachine("ow-abc123", ServerSelfHost, 1000)

		apiWantValue(t, "cooling at the bench", any(api.workerMachineCoolingOn("ow-abc123", ServerSelfHost, 1000)), any(true))
		apiWantValue(t, "cooling one second short", any(api.workerMachineCoolingOn("ow-abc123", ServerSelfHost, 1359)), any(true))
		apiWantValue(t, "cooling at the deadline", any(api.workerMachineCoolingOn("ow-abc123", ServerSelfHost, 1360)), any(false))
		apiWantValue(t, "cooling past the deadline", any(api.workerMachineCoolingOn("ow-abc123", ServerSelfHost, 1361)), any(false))
	})

	t.Run("the bench is per (worker, machine) pair: another worker on that host, and this worker elsewhere, are both unbenched", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		api.benchWorkerMachine("ow-abc123", ServerSelfHost, 1000)

		apiWantValue(t, "another worker on the benched host",
			any(api.workerMachineCoolingOn("ow-def456", ServerSelfHost, 1000)), any(false))
		apiWantValue(t, "this worker on another host",
			any(api.workerMachineCoolingOn("ow-abc123", "m-elsewhere", 1000)), any(false))
	})

	t.Run("a machine that was never benched is not cooling", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		apiWantValue(t, "cooling", any(api.workerMachineCoolingOn("ow-abc123", ServerSelfHost, 1000)), any(false))
	})
}

func TestBenchWorkerMachine(t *testing.T) {
	t.Run("a bench records one deadline per (worker, machine) pair and a later bench pushes it out", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		api.benchWorkerMachine("ow-abc123", ServerSelfHost, 1000)
		apiWantValue(t, "bench book", any(wsBenchBook(api)),
			any(map[string]any{"ow-abc123|m-server-self": 1360.0}))

		api.benchWorkerMachine("ow-abc123", ServerSelfHost, 2000)
		api.benchWorkerMachine("ow-def456", ServerSelfHost, 1000)
		apiWantValue(t, "bench book", any(wsBenchBook(api)), any(map[string]any{
			"ow-abc123|m-server-self": 2360.0,
			"ow-def456|m-server-self": 1360.0,
		}))
	})

	t.Run("an unnamed machine benches nothing, so an unplaced worker cannot bench the empty host", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		api.benchWorkerMachine("ow-abc123", "", 1000)

		apiWantValue(t, "bench book", any(wsBenchBook(api)), any(map[string]any{}))
	})
}

// wsBenchBook is the whole bench ledger as a comparable value.
func wsBenchBook(api *apiServer) map[string]any {
	out := map[string]any{}
	for key, until := range api.workerMachineCooldown {
		out[key] = until
	}
	return out
}

func TestFirstNonEmpty(t *testing.T) {
	t.Run("the first argument that carries text is the answer, and the empty ones before it are skipped", func(t *testing.T) {
		apiWantValue(t, "one value", any(firstNonEmpty("m-a")), any("m-a"))
		apiWantValue(t, "the first of two", any(firstNonEmpty("m-a", "m-b")), any("m-a"))
		apiWantValue(t, "past two blanks", any(firstNonEmpty("", "", "m-c")), any("m-c"))
	})

	t.Run("no argument carrying text answers the empty string, including with no arguments at all", func(t *testing.T) {
		apiWantValue(t, "no arguments", any(firstNonEmpty()), any(""))
		apiWantValue(t, "all blank", any(firstNonEmpty("", "")), any(""))
	})
}

func TestNotifyWorkerSpawn(t *testing.T) {
	// wsPinned is the worker with an owner pin on the seeded server-self warden
	// — the placement every dispatching case below starts from.
	wsPinned := func(w OutsourceWorker) OutsourceWorker {
		w.DesiredMachineID = ServerSelfHost
		return w
	}

	t.Run("a placed worker with a live task is dispatched as ONE member start frame addressed to that worker on that machine", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		status, boot := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("boot-context preview: %d (%v)", status, boot)
		}
		dashboard := apiTestListen(t, api, "")

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(true))
		wsWantWardenFrames(t, api, ServerSelfHost,
			wsStartFrame("ow-abc123", boot["context"].(string), "claude", "sonnet", "medium"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"machine": "m-server-self",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "server"))
	})

	t.Run("the dispatch records the attempt, arms the receipt watch and puts the shared FSM into a start that is in flight", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "spawn observation", any(map[string]any{
			"target": api.workerSpawnTarget["ow-abc123"],
			"at":     api.workerSpawnAt["ow-abc123"],
			"tries":  float64(api.workerSpawnAttempts["ow-abc123"]),
		}), any(map[string]any{"target": "m-server-self", "at": 1000.0, "tries": 1.0}))
		state := api.workerReconcileStates["ow-abc123"]
		apiWantValue(t, "fsm state", any(map[string]any{
			"phase": state.Phase, "last_command": state.LastCommand, "at": state.LastCommandAt,
			"attempts": float64(state.Attempts), "circuit_open": state.CircuitOpen,
		}), any(map[string]any{
			"phase": "starting", "last_command": "start", "at": 1000.0,
			"attempts": 0.0, "circuit_open": false,
		}))
		api.receiptMu.Lock()
		owed := api.receiptPending["ow-abc123"]
		api.receiptMu.Unlock()
		apiWantValue(t, "owed receipt", any(map[string]any{
			"rpc": owed.RPC, "warden": owed.Warden, "deadline": owed.Deadline,
		}), any(map[string]any{"rpc": "start", "warden": "m-server-self", "deadline": 1090.0}))
	})

	t.Run("the token in the frame is this worker's own session credential, and it is the only place the token rides", func(t *testing.T) {
		api, h, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		if len(frames) != 1 {
			t.Fatalf("want one frame, got %d (%v)", len(frames), frames)
		}
		args := frames[0]["data"].(map[string]any)["args"].(map[string]any)
		token, _ := args["member_token"].(string)
		status, data := apiJSON(t, h, "POST", "/api/self/waking", token, `{}`)
		if status != 200 {
			t.Fatalf("waking as the worker: %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123", "desired_state": "", "refocus_op": "", "refocus_deadline": 0,
		})
	})

	t.Run("a landed start drops the previous session's boot anchor and the kills owed to the machine it lands on", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		api.gauge.Set("ow-abc123", map[string]any{"boot_ts": 111.0, "compaction_count": 2.0})

		api.outsourceMu.Lock()
		api.workerStopPending["ow-abc123"] = ServerSelfHost
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 5}
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "gauge", any(api.gauge.Get("ow-abc123")), any(map[string]any{}))
		apiWantValue(t, "parked kills", any(float64(len(api.workerStopPending))), any(0))
		apiWantValue(t, "armed kills", any(float64(len(api.workerStopLanded))), any(0))
	})

	t.Run("a start toward a DIFFERENT machine leaves the kills the old box still owes standing", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopPending["ow-abc123"] = "m-elsewhere"
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: "m-elsewhere", At: 5}
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		parked := api.workerStopPending["ow-abc123"]
		armed := api.workerStopLanded["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "parked kill", any(parked), any("m-elsewhere"))
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-elsewhere", "at": 5.0}))
	})

	t.Run("a dispatch clears the placement-block stamp that has now been overtaken", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		blocked := w
		api.stampWorkerPlacementBlocked(&blocked, "machine_unavailable: machine 'm-server-self' is offline", 500)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"machine": "m-server-self",
			"last_op": "start", "last_op_ok": nil, "last_op_at": 500, "last_op_reason": "",
		}))
	})

	t.Run("a re-push inside the pacing window is refused, and one full window later it goes out again", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()
		wsDrainWardenFrames(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.outsourceMu.Lock()
		early := api.notifyWorkerSpawn(wsPinned(w), 1119)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched one second short of the window", any(early), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		dashboard.wantFrames()
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"machine": "m-server-self",
		}))

		api.outsourceMu.Lock()
		onTime := api.notifyWorkerSpawn(wsPinned(w), 1120)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched at the window", any(onTime), any(true))
		apiWantValue(t, "attempts", any(float64(api.workerSpawnAttempts["ow-abc123"])), any(2))
	})

	t.Run("a worker whose bound task has closed is never booted into a void, and the stall says so on its row", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v (%v)", task, err)
		}
		task.Status = TaskStatusDone
		if err := d.PutTask(*task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "spawn observation", any(float64(len(api.workerSpawnAt))), any(0))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"task_status": "done",
			"last_op":     "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "no_live_task: bound task T-1 is missing or already closed — " +
				"nothing left to boot this worker for",
		}))
	})

	t.Run("a worker bound to a task id nothing carries stalls with that id named", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		orphan := wsPinned(w)
		orphan.TaskID = "T-404"

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(orphan, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		row, err := api.dal.GetOutsourceWorker("ow-abc123")
		if err != nil || row == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", row, err)
		}
		apiWantValue(t, "receipt reason", any(row.LastOpReason),
			any("no_live_task: bound task T-404 is missing or already closed — "+
				"nothing left to boot this worker for"))
	})

	t.Run("a worker nobody placed is not dispatched anywhere and the row names the unmade choice", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "spawn observation", any(float64(len(api.workerSpawnAt))), any(0))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "no_machine_selected: no machine is selected for this worker — " +
				"pick one on the worker (改機器) or on the task type's 手冊 assignee; " +
				"there is no automatic placement",
		}))
	})

	t.Run("a pinned machine that is offline stalls the spawn naming that machine, and nothing is enqueued anywhere", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "machine_unavailable: machine 'm-server-self' is offline; " +
				"no other machine is substituted",
		}))
	})

	t.Run("a server with no signing secret mints nothing and dispatches nothing, and the row says which half failed", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		api.keys = singleKeyring(nil)

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(wsPinned(w), 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "spawn observation", any(float64(len(api.workerSpawnAt))), any(0))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "no_signing_secret: the server has no JWT signing secret, " +
				"so no worker token can be minted",
		}))
	})

	t.Run("the frame carries the worker's OWN runtime, model and effort, so a codex worker boots as codex", func(t *testing.T) {
		api, h, d, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
			`{"model":"gpt-5-codex","runtime":"codex","effort":"high"}`); code != 200 {
			t.Fatalf("set runtime: %d %v", code, data)
		}
		stored, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || stored == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", stored, err)
		}
		codex := wsPinned(*stored)
		warden := apiTestAgentToken(t, api, ServerSelfHost, ServerSelfHost)
		if code, data := apiJSON(t, h, "POST", "/api/monitoring/telemetry", warden,
			`{"runtimes":{"codex":{"installed":true,"logged_in":true,"version":"0.4.0"}}}`); code != 200 {
			t.Fatalf("report runtimes: %d %v", code, data)
		}
		apiTestListen(t, api, ServerSelfHost)
		status, boot := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("boot-context preview: %d (%v)", status, boot)
		}

		api.outsourceMu.Lock()
		dispatched := api.notifyWorkerSpawn(codex, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(true))
		wsWantWardenFrames(t, api, ServerSelfHost,
			wsStartFrame("ow-abc123", boot["context"].(string), "codex", "gpt-5-codex", "high"))
	})

	t.Run("a warden that drops offline between the placement decision and the enqueue", func(t *testing.T) {
		t.Skip("structurally unproducible: resolveStickyWorkerPlacement and enqueueToWarden " +
			"read hub.IsOnline inside one call under s.outsourceMu, with no seam between " +
			"them a test can disconnect through — measured: hub.Disconnect before the call " +
			"produces the 'is offline' placement refusal instead, and after it the frame is " +
			"already on the FIFO, so warden_unreachable has no reachable state to observe.")
	})

	t.Run("a boot context, token mint or frame build that faults", func(t *testing.T) {
		t.Skip("structurally unproducible: boot_context_failed needs workerSharedHead or " +
			"workerBootSequence to error, and both read go:embed seeds through no injectable " +
			"seam; token_mint_failed is reachable only with an empty signing secret, which " +
			"the no_signing_secret arm above already claims first; frame_build_failed needs " +
			"json.Marshal of a struct of strings to fail — measured: every runtime string, " +
			"the empty one included, assembles a frame.")
	})
}

func TestWorkerSpawnObs(t *testing.T) {
	t.Run("a worker this run has never dispatched reads as no target and no time", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		target, at := api.workerSpawnObs("ow-abc123")
		apiWantValue(t, "spawn observation", any(map[string]any{"target": target, "at": at}),
			any(map[string]any{"target": "", "at": 0.0}))
	})

	t.Run("after a dispatch it reads that dispatch's machine and moment, taking the scheduler lock itself", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost
		api.outsourceMu.Lock()
		api.notifyWorkerSpawn(pinned, 1000)
		api.outsourceMu.Unlock()

		target, at := api.workerSpawnObs("ow-abc123")
		apiWantValue(t, "spawn observation", any(map[string]any{"target": target, "at": at}),
			any(map[string]any{"target": "m-server-self", "at": 1000.0}))

		other, otherAt := api.workerSpawnObs("ow-def456")
		apiWantValue(t, "another worker's observation",
			any(map[string]any{"target": other, "at": otherAt}),
			any(map[string]any{"target": "", "at": 0.0}))
	})
}

// wsObservation is one memberObservation flattened for a whole-value compare.
func wsObservation(obs memberObservation) map[string]any {
	return map[string]any{
		"member_id": obs.MemberID, "desired": obs.Desired, "online": obs.Online,
		"refocus_since": obs.RefocusSince, "refocus_op": obs.RefocusOp,
		"stopping_since": obs.StoppingSince, "agent_stopped": obs.AgentStopped,
		"last_op_kind": obs.LastOpKind, "last_op_reason": obs.LastOpReason,
		"target_machine": obs.TargetMachine, "running_machine": obs.RunningMachine,
		"handover_armable": obs.HandoverArmable,
	}
}

func TestWorkerObservation(t *testing.T) {
	t.Run("a fresh worker row is projected as the whole FSM input, with every field the row does not carry left zero", func(t *testing.T) {
		_, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		apiWantValue(t, "observation", any(wsObservation(workerObservation(w, false))), any(map[string]any{
			"member_id": "ow-abc123", "desired": "", "online": false,
			"refocus_since": 0.0, "refocus_op": "", "stopping_since": 0.0,
			"agent_stopped": false, "last_op_kind": "", "last_op_reason": "",
			"target_machine": "", "running_machine": "", "handover_armable": false,
		}))
	})

	t.Run("the four recycle fields come off the row and the last_op verb is folded, while the machine pair and stopping anchor stay masked", func(t *testing.T) {
		_, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		w.DesiredState = DesiredStateOnline
		w.RefocusSince = 10
		w.RefocusOp = "relocate"
		w.StoppingSince = 30
		w.StoppedSince = 20
		w.LastOp = "worker_start"
		w.LastOpReason = "session_already_exists: a live session is holding the slot"
		w.DesiredMachineID = "m-pinned"
		w.LastMachineID = "m-landed"

		apiWantValue(t, "observation", any(wsObservation(workerObservation(w, true))), any(map[string]any{
			"member_id": "ow-abc123", "desired": "online", "online": true,
			"refocus_since": 10.0, "refocus_op": "relocate",
			"stopping_since": 0.0, "agent_stopped": true,
			"last_op_kind":   "start",
			"last_op_reason": "session_already_exists: a live session is holding the slot",
			"target_machine": "", "running_machine": "", "handover_armable": false,
		}))
	})

	t.Run("a worker held down is projected as desired offline, so the FSM is never told something false about the owner's intent", func(t *testing.T) {
		_, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		w.DesiredState = DesiredStateOffline

		obs := workerObservation(w, true)
		apiWantValue(t, "desired", any(obs.Desired), any("offline"))
		apiWantValue(t, "agent_stopped with no report", any(obs.AgentStopped), any(false))
	})
}

func TestCanonicalWorkerLastOp(t *testing.T) {
	t.Run("the retired worker_start verb reads as start, so clobber detection survives an old warden build", func(t *testing.T) {
		apiWantValue(t, "folded verb", any(canonicalWorkerLastOp("worker_start")), any("start"))
	})

	t.Run("every other verb, the empty one included, is handed back unchanged", func(t *testing.T) {
		apiWantValue(t, "start", any(canonicalWorkerLastOp("start")), any("start"))
		apiWantValue(t, "stop", any(canonicalWorkerLastOp("stop")), any("stop"))
		apiWantValue(t, "update", any(canonicalWorkerLastOp("update")), any("update"))
		apiWantValue(t, "no verb", any(canonicalWorkerLastOp("")), any(""))
		apiWantValue(t, "the retired stop verb", any(canonicalWorkerLastOp("worker_stop")), any("worker_stop"))
	})
}

func TestUndeliveredWorkerStart(t *testing.T) {
	// wsLoseFrame pushes one frame onto a warden's FIFO and then loses it the
	// way a stream dying mid-drain does: drained, and never written.
	wsLoseFrame := func(t *testing.T, api *apiServer, verb, workerID string) {
		t.Helper()
		frame, ok := buildTargetFrame(verb, workerID)
		if !ok {
			t.Fatalf("buildTargetFrame(%q, %q) refused", verb, workerID)
		}
		api.hub.EnqueueWardenCommandFor(ServerSelfHost, workerID, frame)
		drained := api.hub.DrainWardenCommands(ServerSelfHost)
		if requeued, dropped := api.hub.ReturnUndeliveredCommands(ServerSelfHost, drained); requeued != 0 || dropped != 1 {
			t.Fatalf("want the frame dropped, got requeued=%d dropped=%d", requeued, dropped)
		}
	}

	t.Run("a start frame drained off the FIFO and then lost is reported for a spawn anchored before the loss", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		wsLoseFrame(t, api, reconcileCmdStart, "ow-abc123")

		apiWantValue(t, "undelivered", any(undeliveredWorkerStart(api.hub, "ow-abc123", 1)), any(true))
	})

	t.Run("a loss older than THIS spawn attempt is never borrowed to explain it", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		wsLoseFrame(t, api, reconcileCmdStart, "ow-abc123")

		apiWantValue(t, "undelivered", any(undeliveredWorkerStart(api.hub, "ow-abc123", nowSecs()+1000)), any(false))
	})

	t.Run("a lost STOP is not a lost start, so the wake receipt cannot be written off somebody else's frame", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		wsLoseFrame(t, api, reconcileCmdStop, "ow-abc123")

		apiWantValue(t, "undelivered", any(undeliveredWorkerStart(api.hub, "ow-abc123", 1)), any(false))
	})

	t.Run("with no loss note at all, a zero anchor, or no worker named, the answer is no", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)

		apiWantValue(t, "no note", any(undeliveredWorkerStart(api.hub, "ow-abc123", 1)), any(false))
		wsLoseFrame(t, api, reconcileCmdStart, "ow-abc123")
		apiWantValue(t, "zero anchor", any(undeliveredWorkerStart(api.hub, "ow-abc123", 0)), any(false))
		apiWantValue(t, "no worker named", any(undeliveredWorkerStart(api.hub, "", 1)), any(false))
		apiWantValue(t, "another worker", any(undeliveredWorkerStart(api.hub, "ow-def456", 1)), any(false))
	})
}

func TestWorkerSessionConfirmedGone(t *testing.T) {
	t.Run("the first offline sample only arms the anchor; the session is a fact one full window later", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		apiWantValue(t, "first sample", any(api.workerSessionConfirmedGone("ow-abc123", 1000)), any(false))
		apiWantValue(t, "one second short of the window", any(api.workerSessionConfirmedGone("ow-abc123", 1119)), any(false))
		apiWantValue(t, "at the window", any(api.workerSessionConfirmedGone("ow-abc123", 1120)), any(true))
		apiWantValue(t, "past the window", any(api.workerSessionConfirmedGone("ow-abc123", 5000)), any(true))
	})

	t.Run("a reconnect inside the window drops the anchor, and a later disconnect starts a fresh window rather than resuming it", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.workerSessionConfirmedGone("ow-abc123", 1000)
		listener := apiTestListen(t, api, "ow-abc123")

		apiWantValue(t, "while online", any(api.workerSessionConfirmedGone("ow-abc123", 1119)), any(false))
		apiWantValue(t, "the anchor after a reconnect", any(float64(len(api.workerOfflineSince))), any(0))

		api.hub.Disconnect(listener.l)
		apiWantValue(t, "the first sample of the new window", any(api.workerSessionConfirmedGone("ow-abc123", 1200)), any(false))
		apiWantValue(t, "one second short of the new window", any(api.workerSessionConfirmedGone("ow-abc123", 1319)), any(false))
		apiWantValue(t, "at the new window", any(api.workerSessionConfirmedGone("ow-abc123", 1320)), any(true))
	})
}

func TestResolveWorkerKillTarget(t *testing.T) {
	t.Run("this run's remembered dispatch target is the machine a kill is addressed to", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = "m-spawn"
		api.outsourceMu.Unlock()

		apiWantValue(t, "kill target", any(api.resolveWorkerKillTarget("ow-abc123")), any("m-spawn"))
	})

	t.Run("with the spawn ledger empty the live SSE machine claim is used, and it loses to the ledger when both know", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		listener, err := api.hub.Connect("ow-abc123", "m-claim")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(listener) })

		apiWantValue(t, "kill target from the claim", any(api.resolveWorkerKillTarget("ow-abc123")), any("m-claim"))

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = "m-spawn"
		api.outsourceMu.Unlock()
		apiWantValue(t, "kill target once the ledger knows", any(api.resolveWorkerKillTarget("ow-abc123")), any("m-spawn"))
	})

	t.Run("neither source knowing answers the empty string rather than guessing a machine", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		apiWantValue(t, "kill target", any(api.resolveWorkerKillTarget("ow-abc123")), any(""))
	})
}

func TestObservedWorkerHost(t *testing.T) {
	t.Run("a live SSE machine claim is the observed host, and it outranks the worker's own telemetry", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		listener, err := api.hub.Connect("ow-abc123", "m-claim")
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(listener) })

		apiWantValue(t, "observed host with no telemetry",
			any(api.observedWorkerHost("ow-abc123", nil)), any("m-claim"))
		apiWantValue(t, "observed host with telemetry disagreeing",
			any(api.observedWorkerHost("ow-abc123", map[string]any{"machine": "m-tele"})), any("m-claim"))
	})

	t.Run("with no live claim the worker's self-reported machine is used, and an absent one answers empty", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		apiWantValue(t, "observed host from telemetry",
			any(api.observedWorkerHost("ow-abc123", map[string]any{"machine": "m-tele"})), any("m-tele"))
		apiWantValue(t, "observed host with no telemetry entry",
			any(api.observedWorkerHost("ow-abc123", nil)), any(""))
		apiWantValue(t, "observed host with a blank telemetry machine",
			any(api.observedWorkerHost("ow-abc123", map[string]any{"machine": ""})), any(""))
		apiWantValue(t, "observed host with a non-string telemetry machine",
			any(api.observedWorkerHost("ow-abc123", map[string]any{"machine": 7})), any(""))
	})

	t.Run("the spawn ledger is NOT consulted, so the read path never claims a host the dispatch only aimed at", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = "m-spawn"
		api.outsourceMu.Unlock()

		apiWantValue(t, "observed host", any(api.observedWorkerHost("ow-abc123", nil)), any(""))
	})
}

func TestResolveLiveWorker(t *testing.T) {
	t.Run("a live worker row is answered whole", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		w, err := api.resolveLiveWorker("ow-abc123")
		if err != nil {
			t.Fatalf("resolveLiveWorker: %v", err)
		}
		apiWantValue(t, "worker", any(map[string]any{
			"id": w.ID, "codename": w.Codename, "task_id": w.TaskID,
			"status": w.Status, "runtime": w.Runtime, "model": w.Model, "effort": w.Effort,
		}), any(map[string]any{
			"id": "ow-abc123", "codename": "Contractor", "task_id": "T-1",
			"status": "assigned", "runtime": "claude", "model": "sonnet", "effort": "medium",
		}))
	})

	t.Run("an id nothing carries and a RELEASED worker are the same answer: not found, and no row", func(t *testing.T) {
		api, _, d, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)

		row, err := api.resolveLiveWorker("ow-nope")
		if row != nil || err != errNotFound {
			t.Fatalf("want (nil, not found), got (%v, %v)", row, err)
		}

		released := w
		released.Status = WorkerStatusReleased
		if err := d.PutOutsourceWorker(released); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		row, err = api.resolveLiveWorker("ow-abc123")
		if row != nil || err != errNotFound {
			t.Fatalf("want (nil, not found), got (%v, %v)", row, err)
		}
	})
}

func TestEnqueueWorkerStop(t *testing.T) {
	t.Run("one member stop frame addressed to the worker lands on the target's FIFO and arms the receipt it now owes", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		enqueued := api.enqueueWorkerStop(ServerSelfHost, "ow-abc123")
		api.outsourceMu.Unlock()

		apiWantValue(t, "enqueued", any(enqueued), any(true))
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		api.receiptMu.Lock()
		owed := api.receiptPending["ow-abc123"]
		api.receiptMu.Unlock()
		apiWantValue(t, "owed receipt", any(map[string]any{"rpc": owed.RPC, "warden": owed.Warden}),
			any(map[string]any{"rpc": "stop", "warden": "m-server-self"}))
	})

	t.Run("an offline target is refused fail-closed: nothing is enqueued and nothing is owed", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		api.outsourceMu.Lock()
		enqueued := api.enqueueWorkerStop(ServerSelfHost, "ow-abc123")
		api.outsourceMu.Unlock()

		apiWantValue(t, "enqueued", any(enqueued), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		api.receiptMu.Lock()
		owed := len(api.receiptPending)
		api.receiptMu.Unlock()
		apiWantValue(t, "owed receipts", any(float64(owed)), any(0))
	})

	t.Run("a kill that goes out to the machine it was parked for drops the parking, and one parked elsewhere is left standing", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopPending["ow-abc123"] = "m-elsewhere"
		api.enqueueWorkerStop(ServerSelfHost, "ow-abc123")
		parkedElsewhere := api.workerStopPending["ow-abc123"]
		api.workerStopPending["ow-abc123"] = ServerSelfHost
		api.enqueueWorkerStop(ServerSelfHost, "ow-abc123")
		parkedHere := api.workerStopPending["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "a parking for another machine", any(parkedElsewhere), any("m-elsewhere"))
		apiWantValue(t, "the parking for this machine", any(parkedHere), any(""))
		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"), wsStopFrame("ow-abc123"))
	})
}

func TestStopWorkerSessionOrPark(t *testing.T) {
	t.Run("an accepted kill is armed against the target, so a session that outlives it can be re-pushed", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.stopWorkerSessionOrPark(ServerSelfHost, "ow-abc123", 1000)
		armed := api.workerStopLanded["ow-abc123"]
		parked := len(api.workerStopPending)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-server-self", "at": 1000.0}))
		apiWantValue(t, "parked kills", any(float64(parked)), any(0))
	})

	t.Run("a refused kill is parked instead, and the refusal supersedes an armed kill so the debt is owed from scratch", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: "m-old", At: 5}
		api.stopWorkerSessionOrPark(ServerSelfHost, "ow-abc123", 1000)
		armed := len(api.workerStopLanded)
		parked := api.workerStopPending["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
		apiWantValue(t, "parked kill", any(parked), any("m-server-self"))
	})
}

func TestNoteWorkerStopNoSuchSession(t *testing.T) {
	t.Run("the machine the kill was aimed at reporting no_such_session disarms the retry", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}

		api.noteWorkerStopNoSuchSession("ow-abc123", ServerSelfHost)

		apiWantValue(t, "armed kills", any(float64(len(api.workerStopLanded))), any(0))
	})

	t.Run("a machine that never hosted the session cannot disarm it, because an identity sweep asks every warden", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}

		api.noteWorkerStopNoSuchSession("ow-abc123", "m-bystander")

		armed := api.workerStopLanded["ow-abc123"]
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-server-self", "at": 1000.0}))
	})

	t.Run("an unidentified reporter, an unnamed worker and a worker with nothing armed all change nothing", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}

		api.noteWorkerStopNoSuchSession("ow-abc123", "")
		api.noteWorkerStopNoSuchSession("", ServerSelfHost)
		api.noteWorkerStopNoSuchSession("ow-def456", ServerSelfHost)

		armed := api.workerStopLanded["ow-abc123"]
		apiWantValue(t, "armed kill", any(map[string]any{
			"target": armed.Target, "at": armed.At, "count": float64(len(api.workerStopLanded)),
		}), any(map[string]any{"target": "m-server-self", "at": 1000.0, "count": 1.0}))
	})
}

func TestRetryPendingWorkerStop(t *testing.T) {
	t.Run("a parked kill re-fires once the target is reachable, and the re-fire is armed against it", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopPending["ow-abc123"] = ServerSelfHost
		api.retryPendingWorkerStop("ow-abc123", 2000)
		parked := len(api.workerStopPending)
		armed := api.workerStopLanded["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "parked kills", any(float64(parked)), any(0))
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-server-self", "at": 2000.0}))
	})

	t.Run("a parked kill whose target is still unreachable stays parked and nothing is armed", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)

		api.outsourceMu.Lock()
		api.workerStopPending["ow-abc123"] = ServerSelfHost
		api.retryPendingWorkerStop("ow-abc123", 2000)
		parked := api.workerStopPending["ow-abc123"]
		armed := len(api.workerStopLanded)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "parked kill", any(parked), any("m-server-self"))
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
	})

	t.Run("with nothing parked it falls through to the other half of the promise: the kill that left and did not take", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		listener, err := api.hub.Connect("ow-abc123", ServerSelfHost)
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(listener) })
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryPendingWorkerStop("ow-abc123", 1090)
		armedAt := api.workerStopLanded["ow-abc123"].At
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "the re-dispatch anchor", any(armedAt), any(1090.0))
	})
}

func TestRetryUnlandedWorkerStop(t *testing.T) {
	// wsWorkerOn puts the worker's live session on machineID.
	wsWorkerOn := func(t *testing.T, api *apiServer, machineID string) {
		t.Helper()
		listener, err := api.hub.Connect("ow-abc123", machineID)
		if err != nil {
			t.Fatalf("hub.Connect: %v", err)
		}
		t.Cleanup(func() { api.hub.Disconnect(listener) })
	}

	t.Run("a session still live on the machine the kill was aimed at, past stop_retry, is killed again", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsWorkerOn(t, api, ServerSelfHost)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryUnlandedWorkerStop("ow-abc123", 1090)
		armed := api.workerStopLanded["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-server-self", "at": 1090.0}))
	})

	t.Run("a session still live but INSIDE stop_retry is waited out, not re-pushed", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsWorkerOn(t, api, ServerSelfHost)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryUnlandedWorkerStop("ow-abc123", 1089)
		armed := api.workerStopLanded["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kill", any(map[string]any{"target": armed.Target, "at": armed.At}),
			any(map[string]any{"target": "m-server-self", "at": 1000.0}))
	})

	t.Run("a worker that is offline is proof the kill took, so the arm is disarmed and nothing is sent", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryUnlandedWorkerStop("ow-abc123", 5000)
		armed := len(api.workerStopLanded)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
	})

	t.Run("a worker online on ANOTHER machine means the addressed session is gone, so the retry is disarmed rather than aimed at the old box forever", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsWorkerOn(t, api, "m-elsewhere")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryUnlandedWorkerStop("ow-abc123", 5000)
		armed := len(api.workerStopLanded)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
	})

	t.Run("a worker with no kill armed is left alone entirely", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsWorkerOn(t, api, ServerSelfHost)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.retryUnlandedWorkerStop("ow-abc123", 5000)
		armed := len(api.workerStopLanded)
		parked := len(api.workerStopPending)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
		apiWantValue(t, "parked kills", any(float64(parked)), any(0))
	})

	t.Run("a re-push toward an unreachable target parks the kill instead of losing it", func(t *testing.T) {
		api, _, _, _, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsWorkerOn(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerStopLanded["ow-abc123"] = workerStopDispatch{Target: ServerSelfHost, At: 1000}
		api.retryUnlandedWorkerStop("ow-abc123", 1090)
		armed := len(api.workerStopLanded)
		parked := api.workerStopPending["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
		apiWantValue(t, "parked kill", any(parked), any("m-server-self"))
	})
}

// wsOnline gives the worker a live session claiming machineID, the way its own
// SSE connection does.
func wsOnline(t *testing.T, api *apiServer, workerID, machineID string) {
	t.Helper()
	listener, err := api.hub.Connect(workerID, machineID)
	if err != nil {
		t.Fatalf("hub.Connect(%q): %v", workerID, err)
	}
	t.Cleanup(func() { api.hub.Disconnect(listener) })
}

// wsOutcome is one ownerOpOutcome flattened for a whole-value compare, with the
// derived Pending() answer beside the four arms.
func wsOutcome(o ownerOpOutcome) map[string]any {
	return map[string]any{
		"dispatched": o.Dispatched, "wound_down": o.WoundDown,
		"held_down": o.HeldDown, "already_running": o.AlreadyRunning,
		"pending": o.Pending(),
	}
}

func TestWorkerHasStateToFlush(t *testing.T) {
	t.Run("a worker with no live session has nothing to flush, so the owner's verb takes effect immediately", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)

		apiWantValue(t, "has state to flush", any(api.workerHasStateToFlush(w)), any(false))
	})

	t.Run("an online worker has state to flush, including one whose epoch is open but not yet collected", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsOnline(t, api, "ow-abc123", ServerSelfHost)

		apiWantValue(t, "fresh session", any(api.workerHasStateToFlush(w)), any(true))
		open := w
		open.RefocusSince = 10
		apiWantValue(t, "epoch open, nothing reported", any(api.workerHasStateToFlush(open)), any(true))
	})

	t.Run("THIS epoch's wind-down already collected has nothing left to flush, even while the old session lingers online", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		collected := w
		collected.RefocusSince = 10
		collected.StoppedSince = 20

		apiWantValue(t, "has state to flush", any(api.workerHasStateToFlush(collected)), any(false))
	})

	t.Run("a stopped_since latched outside any epoch does NOT count as collected, so the next owner verb is not swallowed for life", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		stale := w
		stale.StoppedSince = 20

		apiWantValue(t, "has state to flush", any(api.workerHasStateToFlush(stale)), any(true))
	})
}

func TestHasUncollectedOnlineOwnerOpState(t *testing.T) {
	t.Run("an offline worker has nothing to flush even when a refocus marker remains", func(t *testing.T) {
		if got := hasUncollectedOnlineOwnerOpState(10, 0, false); got {
			t.Fatal("an offline worker must not report online owner-operation state")
		}
	})

	t.Run("an online worker whose refocus and stopped markers are both present has been collected", func(t *testing.T) {
		if got := hasUncollectedOnlineOwnerOpState(10, 20, true); got {
			t.Fatal("a worker with both close-out markers must have no state left to flush")
		}
	})
}

func TestRespawnWorkerForOwnerOp(t *testing.T) {
	t.Run("a worker the owner has held down only records the change: nothing is dispatched and the row says why", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		held := w
		held.DesiredState = DesiredStateOffline
		if err := d.PutOutsourceWorker(held); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		outcome := api.respawnWorkerForOwnerOp(held, ownerOpRelocate)
		api.outsourceMu.Unlock()

		apiWantValue(t, "outcome", any(wsOutcome(outcome)), any(map[string]any{
			"dispatched": false, "wound_down": false, "held_down": true,
			"already_running": false, "pending": true,
		}))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "offline",
			"machine": "m-server-self",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "held_down: the relocate was saved, but nothing was started — " +
				"this worker is stopped; 重啟 it when you want it to run",
		}))
	})

	t.Run("a live worker with state to flush is wound down instead of killed, and nothing goes out yet", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		running := w
		running.DesiredState = DesiredStateOnline
		if err := d.PutOutsourceWorker(running); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		outcome := api.respawnWorkerForOwnerOp(running, ownerOpRelocate)
		api.outsourceMu.Unlock()

		apiWantValue(t, "outcome", any(wsOutcome(outcome)), any(map[string]any{
			"dispatched": false, "wound_down": true, "held_down": false,
			"already_running": false, "pending": true,
		}))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": apiAnyNumber, "refocus_op": "relocate",
		}))
	})

	t.Run("a worker with nothing to flush is respawned on the spot, and the start goes out now", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost
		pinned.DesiredState = DesiredStateOnline
		api.outsourceMu.Lock()
		outcome := api.respawnWorkerForOwnerOp(pinned, ownerOpRestart)
		api.outsourceMu.Unlock()

		apiWantValue(t, "outcome", any(wsOutcome(outcome)), any(map[string]any{
			"dispatched": true, "wound_down": false, "held_down": false,
			"already_running": false, "pending": false,
		}))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		if len(frames) != 1 {
			t.Fatalf("want one start frame, got %d (%v)", len(frames), frames)
		}
		apiWantValue(t, "the dispatched verb",
			any(frames[0]["data"].(map[string]any)["rpc"]), any("start"))
	})
}

func TestRelocateWorkerNow(t *testing.T) {
	t.Run("an unlanded worker is dispatched through the relocate owner-operation seam", func(t *testing.T) {
		api, _, _, _, worker := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		worker.DesiredMachineID = ServerSelfHost
		worker.DesiredState = DesiredStateOnline

		api.outsourceMu.Lock()
		outcome := api.relocateWorkerNow(worker)
		api.outsourceMu.Unlock()

		apiWantValue(t, "outcome", any(wsOutcome(outcome)), any(map[string]any{
			"dispatched": true, "wound_down": false, "held_down": false,
			"already_running": false, "pending": false,
		}))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		if len(frames) != 1 {
			t.Fatalf("want one relocate start frame, got %d (%v)", len(frames), frames)
		}
		apiWantValue(t, "dispatched verb", any(frames[0]["data"].(map[string]any)["rpc"]), any("start"))
		apiWantValue(t, "dispatched subject", any(frames[0]["subject"]), any(worker.ID))
	})
}

func TestOpenOwnerOpHandover(t *testing.T) {
	t.Run("opening a window stamps a fresh epoch, clears the stale wind-down latches and fans the 預告 at the worker's own session", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		stale := w
		stale.DesiredState = DesiredStateOnline
		stale.StoppedSince = 77
		stale.StoppingSince = 66
		if err := d.PutOutsourceWorker(stale); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		opened := api.openOwnerOpHandover(stale, ownerOpModel)
		api.outsourceMu.Unlock()

		apiWantValue(t, "opened", any(opened), any(true))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "runtime/model",
		}))
		dashboard.wantFrames(
			apiTestWorkerDelta(2, "active", "server"),
			apiTestHandoverDelta(3, "online", apiTestOffboardNotice, "server"),
		)
		contractor.wantFrames(apiTestHandoverDelta(3, "online", apiTestOffboardNotice, "server"))
	})

	t.Run("a worker already further along the ladder keeps its own deadline: nothing is stamped and nothing is fanned", func(t *testing.T) {
		api, h, d, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		accelerated := w
		accelerated.DesiredState = DesiredStateOnline
		accelerated.RefocusSince = 500
		accelerated.RefocusOp = "accelerated_stop"
		if err := d.PutOutsourceWorker(accelerated); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
		dashboard := apiTestListen(t, api, "")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		opened := api.openOwnerOpHandover(accelerated, ownerOpRelocate)
		api.outsourceMu.Unlock()

		apiWantValue(t, "opened", any(opened), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self",
		}))
		dashboard.wantFrames()
	})

	t.Run("an OFFLINE worker skips the window entirely and is collected on the spot, because no session can hear the 預告", func(t *testing.T) {
		api, _, d, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		gone := w
		gone.DesiredState = DesiredStateOnline
		if err := d.PutOutsourceWorker(gone); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		apiTestListen(t, api, ServerSelfHost)
		gone.DesiredMachineID = ServerSelfHost
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		opened := api.openOwnerOpHandover(gone, ownerOpRelocate)
		api.outsourceMu.Unlock()

		apiWantValue(t, "opened", any(opened), any(true))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		verbs := []any{}
		for _, f := range frames {
			verbs = append(verbs, f["data"].(map[string]any)["rpc"])
		}
		apiWantValue(t, "the verbs the collect dispatched", any(verbs), any([]any{"stop", "start"}))
	})
}

func TestRespawnWorkerForOwnerOpNow(t *testing.T) {
	t.Run("an ACTIVE worker with no kill target still gets its start attempted, so an owner verb never ends in silence", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost

		api.outsourceMu.Lock()
		dispatched := api.respawnWorkerForOwnerOpNow(pinned, ownerOpRestart)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(true))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		verbs := []any{}
		for _, f := range frames {
			verbs = append(verbs, f["data"].(map[string]any)["rpc"])
		}
		apiWantValue(t, "the verbs dispatched", any(verbs), any([]any{"start"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": nil, "last_op_at": apiAnyNumber,
		}))
	})

	t.Run("an ACTIVE worker with no kill target AND nowhere to boot ends with the deferral receipt standing", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)

		api.outsourceMu.Lock()
		dispatched := api.respawnWorkerForOwnerOpNow(w, ownerOpRestart)
		api.outsourceMu.Unlock()

		apiWantValue(t, "dispatched", any(dispatched), any(false))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status":  "active",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "no_machine_selected: no machine is selected for this worker — " +
				"pick one on the worker (改機器) or on the task type's 手冊 assignee; " +
				"there is no automatic placement",
		}))
	})
}

func TestRespawnWorkerNow(t *testing.T) {
	t.Run("a live worker's old session is killed and a fresh start dispatched, in that order, onto the pinned machine", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		api.gauge.Set("ow-abc123", map[string]any{"boot_ts": 5.0})
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerSpawnAt["ow-abc123"] = nowSecs()
		respawned := api.respawnWorkerNow(pinned, "relocate")
		api.outsourceMu.Unlock()

		apiWantValue(t, "respawned", any(respawned), any(true))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		verbs := []any{}
		for _, f := range frames {
			verbs = append(verbs, f["data"].(map[string]any)["rpc"])
		}
		apiWantValue(t, "the verbs dispatched", any(verbs), any([]any{"stop", "start"}))
		apiWantValue(t, "the dead session's boot anchor", any(api.gauge.Get("ow-abc123")), any(map[string]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "machine": "m-server-self",
		}))
	})

	t.Run("an ACTIVE worker with no kill target defers the WHOLE cycle — no kill, no respawn — and leaves the deferral receipt", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost
		dashboard := apiTestListen(t, api, "")

		api.outsourceMu.Lock()
		respawned := api.respawnWorkerNow(pinned, "relocate")
		api.outsourceMu.Unlock()

		apiWantValue(t, "respawned", any(respawned), any(false))
		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status":  "active",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "respawn_deferred: the relocate could not clear this worker's " +
				"previous session — it is marked active but neither the server's spawn memory " +
				"nor a live connection knows which machine it is on; retrying",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "server"))
	})

	t.Run("a worker that never claimed a session has nothing to kill, so an empty target only skips the stop", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		apiTestListen(t, api, ServerSelfHost)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost

		api.outsourceMu.Lock()
		respawned := api.respawnWorkerNow(pinned, "relocate")
		api.outsourceMu.Unlock()

		apiWantValue(t, "respawned", any(respawned), any(true))
		frames := wsDrainWardenFrames(t, api, ServerSelfHost)
		verbs := []any{}
		for _, f := range frames {
			verbs = append(verbs, f["data"].(map[string]any)["rpc"])
		}
		apiWantValue(t, "the verbs dispatched", any(verbs), any([]any{"start"}))
	})

	t.Run("an unreachable kill target parks the kill rather than losing it, and the respawn is still attempted", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		pinned := w
		pinned.DesiredMachineID = ServerSelfHost

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = "m-elsewhere"
		respawned := api.respawnWorkerNow(pinned, "relocate")
		parked := api.workerStopPending["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "respawned", any(respawned), any(true))
		apiWantValue(t, "parked kill", any(parked), any("m-elsewhere"))
		wsWantWardenFrames(t, api, ServerSelfHost)
	})
}

func TestStopWorkerNow(t *testing.T) {
	t.Run("the current session is killed with NO re-dispatch, and the pacing is cleared so a later restart is never throttled", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		api.gauge.Set("ow-abc123", map[string]any{"boot_ts": 5.0})

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerSpawnAt["ow-abc123"] = 4242
		api.stopWorkerNow(w)
		spawnPace := len(api.workerSpawnAt)
		armed := api.workerStopLanded["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "the dead session's boot anchor", any(api.gauge.Get("ow-abc123")), any(map[string]any{}))
		apiWantValue(t, "the spawn pacing", any(float64(spawnPace)), any(0))
		apiWantValue(t, "armed kill target", any(armed.Target), any("m-server-self"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "machine": "m-server-self",
		}))
	})

	t.Run("with no kill target the stop is only loud: nothing is sent, nothing is parked and no receipt is written", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.outsourceMu.Lock()
		api.stopWorkerNow(w)
		parked := len(api.workerStopPending)
		armed := len(api.workerStopLanded)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "parked kills", any(float64(parked)), any(0))
		apiWantValue(t, "armed kills", any(float64(armed)), any(0))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})
}

// wsWindDown is the fixture with the four wind-down anchors and the desired
// state persisted the way the product's own writers leave them.
func wsWindDown(t *testing.T, status, desired, refocusOp string,
	refocusSince, stoppingSince, stoppedSince float64, online bool,
) (*apiServer, http.Handler, *DAL, string, OutsourceWorker) {
	t.Helper()
	api, h, d, owner, w := wsWorkerSpawnFixture(t, status)
	w.DesiredState = desired
	w.DesiredMachineID = ServerSelfHost
	w.RefocusOp = refocusOp
	w.RefocusSince = refocusSince
	w.StoppingSince = stoppingSince
	w.StoppedSince = stoppedSince
	if err := api.persistWorkerWindDownAnchors(w); err != nil {
		t.Fatalf("persistWorkerWindDownAnchors: %v", err)
	}
	if err := d.PutOutsourceWorker(w); err != nil {
		t.Fatalf("PutOutsourceWorker: %v", err)
	}
	if online {
		wsOnline(t, api, "ow-abc123", ServerSelfHost)
	}
	return api, h, d, owner, w
}

// wsVerbs is the ordered list of RPC verbs a warden's FIFO held.
func wsVerbs(t *testing.T, api *apiServer, machineID string) []any {
	t.Helper()
	out := []any{}
	for _, frame := range wsDrainWardenFrames(t, api, machineID) {
		out = append(out, frame["data"].(map[string]any)["rpc"])
	}
	return out
}

func TestClearWorkerRefocus(t *testing.T) {
	t.Run("the loop-break zeroes the epoch AND both wind-down latches, so a stale one cannot bleed into the next handover", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 110, 120, false)
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerRefocus("ow-abc123", "respawn landed")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "server"))
	})

	t.Run("a row with nothing to clear, and an id nothing carries, write nothing and fan nothing", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		dashboard := apiTestListen(t, api, "")

		api.clearWorkerRefocus("ow-abc123", "respawn landed")
		api.clearWorkerRefocus("ow-nope", "ghost")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
		}))
		dashboard.wantFrames()
	})

	t.Run("a stopped latch with no epoch is still cleared, because all three anchors go together", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 120, false)

		api.clearWorkerRefocus("ow-abc123", "respawn landed")

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
		}))
	})
}

func TestCollectWorkerHandover(t *testing.T) {
	t.Run("the 收口 latches the dump-done marker and then kills and respawns through the worker's single kill funnel", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		collected := api.collectWorkerHandover(w, "fsm-recycle", triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "collected", any(collected), any(true))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop", "start"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": 100, "refocus_op": "relocate",
		}))
	})

	t.Run("a deferred collect on a session that is GONE rolls the WHOLE epoch back, so the ordinary FSM rescue can take over", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		collected := api.collectWorkerHandover(w, "fsm-recycle", triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "collected", any(collected), any(false))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "respawn_deferred: the fsm-recycle could not clear this worker's " +
				"previous session — it is marked active but neither the server's spawn memory " +
				"nor a live connection knows which machine it is on; retrying",
		}))
	})

	t.Run("a deferred collect on a session that is still ONLINE rolls only the latch back, so the grace arm retries", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, false)
		wsOnline(t, api, "ow-abc123", "")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		collected := api.collectWorkerHandover(w, "fsm-recycle", triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "collected", any(collected), any(false))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"refocus_since": 100, "refocus_op": "relocate",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "respawn_deferred: the fsm-recycle could not clear this worker's " +
				"previous session — it is marked active but neither the server's spawn memory " +
				"nor a live connection knows which machine it is on; retrying",
		}))
	})
}

func TestCollectWorkerStop(t *testing.T) {
	t.Run("the 停止 收口 latches the same marker and kills, and NEVER re-spawns the worker it just stopped", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.collectWorkerStop(w, "stop-session-gone", triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"machine": "m-server-self",
		}))
	})

	t.Run("a 停止 收口 with no kill target still latches the marker, and sends nothing", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.collectWorkerStop(w, "stop-session-gone", triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
		}))
	})
}

func TestOpenWorkerHandoverGrace(t *testing.T) {
	t.Run("a live session is fanned the member-topic 預告 at its own connection and NOTHING is killed", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, true)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, apiTestPlainAgentID)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.openWorkerHandoverGrace(w, triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		contractor.wantFrames(apiTestHandoverDelta(2, "online", apiTestOffboardNotice, "server"))
		dashboard.wantFrames(apiTestHandoverDelta(2, "online", apiTestOffboardNotice, "server"))
		bystander.wantFrames()
	})

	t.Run("an OFFLINE worker skips the window and is collected as a handover: killed and respawned", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.openWorkerHandoverGrace(w, triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop", "start"}))
	})

	t.Run("an OFFLINE worker the owner has held down is collected as a STOP instead, so 停止 never re-spawns", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.openWorkerHandoverGrace(w, triggerServer)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
			"machine": "m-server-self",
		}))
	})
}

func TestAutoHandoverWorker(t *testing.T) {
	t.Run("a held-down worker is collected once its session is CONFIRMED gone, not on the first offline sample", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 1000)
		firstSample := wsVerbs(t, api, ServerSelfHost)
		api.autoHandoverWorker(w, 1119)
		insideWindow := wsVerbs(t, api, ServerSelfHost)
		api.autoHandoverWorker(w, 1120)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the first sample", any(firstSample), any([]any{}))
		apiWantValue(t, "one second short of the window", any(insideWindow), any([]any{}))
		apiWantValue(t, "at the window", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
			"machine": "m-server-self",
		}))
	})

	t.Run("an owner-pressed 加速停止 is collected on ITS clock, counted from the press", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "accelerated_stop", 0, 200, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 319)
		early := wsVerbs(t, api, ServerSelfHost)
		api.autoHandoverWorker(w, 320)
		api.outsourceMu.Unlock()

		apiWantValue(t, "one second short of the deadline", any(early), any([]any{}))
		apiWantValue(t, "at the deadline", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
	})

	t.Run("a plain 停止 runs no clock at all, so a live worker is waited on indefinitely", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 999999)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
	})

	t.Run("a held-down worker that has already reported stopped is waiting for nothing, so the arm stays silent", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 250, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 1000)
		api.autoHandoverWorker(w, 5000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
	})

	t.Run("a session that booted AFTER the epoch was stamped breaks the loop: the whole epoch is cleared and nothing is dispatched", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 110, 120, true)
		api.gauge.Set("ow-abc123", map[string]any{"boot_ts": 200.0})
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self",
		}))
	})

	t.Run("an epoch whose session booted BEFORE the stamp is mid-flight: the epoch stands and nothing is collected here", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 300, 0, 0, true)
		api.gauge.Set("ow-abc123", map[string]any{"boot_ts": 200.0})
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": 300, "refocus_op": "relocate",
		}))
	})

	t.Run("a desired-online worker with no epoch open is left entirely alone, because the threshold arm is gone", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.autoHandoverWorker(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self",
		}))
		dashboard.wantFrames()
	})
}

// wsWakingDelta is the member delta a worker's first boot report fans — the
// wake shape, which carries no offboard notice.
func wsWakingDelta(seq int) map[string]any {
	return map[string]any{
		"seq": seq, "topic": "member", "op": "patch",
		"data": map[string]any{
			"entity": "member", "key": "owner::ow-abc123",
			"epoch": seq, "deleted": false,
			"payload": map[string]any{
				"id": "ow-abc123", "name": "Contractor", "status": "active",
				"owner_id": "owner", "desired_state": "online",
			},
		},
		"ts": apiAnyNumber, "trigger": "server",
	}
}

// wsAnchors is a returned member's wind-down anchors, flattened for a
// whole-value compare.
func wsAnchors(m *Member) map[string]any {
	return map[string]any{
		"id": m.ID, "desired_state": m.DesiredState, "refocus_op": m.RefocusOp,
		"refocus_since_set": m.RefocusSince > 0, "stopping_since_set": m.StoppingSince > 0,
		"stopped_since_set": m.StoppedSince > 0, "actual_model": m.ActualModel,
	}
}

func TestWorkerReportWaking(t *testing.T) {
	t.Run("the first boot verb claims the worker: assigned flips active, the recycle markers are cleared and the model is stored", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusAssigned, DesiredStateOnline, "relocate", 100, 110, 120, true)
		dashboard := apiTestListen(t, api, "")
		model := "claude-opus-5"

		m, err := api.workerReportWaking("ow-abc123", &model, triggerServer)
		if err != nil {
			t.Fatalf("workerReportWaking: %v", err)
		}

		apiWantValue(t, "answer", any(wsAnchors(m)), any(map[string]any{
			"id": "ow-abc123", "desired_state": "online", "refocus_op": "",
			"refocus_since_set": false, "stopping_since_set": false,
			"stopped_since_set": false, "actual_model": "claude-opus-5",
		}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self", "actual_model": "claude-opus-5",
		}))
		dashboard.wantFrames(
			wsWakingDelta(2),
			apiTestWorkerDelta(3, "active", "server"),
		)
	})

	t.Run("a repeat report on an already-active worker fans no worker delta and leaves the stored model alone", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusAssigned, DesiredStateOnline, "", 0, 0, 0, true)
		model := "claude-opus-5"
		if _, err := api.workerReportWaking("ow-abc123", &model, triggerServer); err != nil {
			t.Fatalf("workerReportWaking: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		m, err := api.workerReportWaking("ow-abc123", nil, triggerServer)
		if err != nil {
			t.Fatalf("workerReportWaking: %v", err)
		}

		apiWantValue(t, "the model the answer carries", any(m.ActualModel), any("claude-opus-5"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self", "actual_model": "claude-opus-5",
		}))
		dashboard.wantFrames(wsWakingDelta(4))
	})

	t.Run("a caller with no live worker row is refused not-found and nothing is written", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusAssigned, DesiredStateOnline, "", 0, 0, 0, false)
		dashboard := apiTestListen(t, api, "")

		m, err := api.workerReportWaking("ow-nope", nil, triggerServer)
		if m != nil || err != errNotFound {
			t.Fatalf("want (nil, not found), got (%v, %v)", m, err)
		}
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "assigned", "desired_state": "online",
		}))
		dashboard.wantFrames()
	})
}

func TestWorkerReportStopping(t *testing.T) {
	t.Run("a stopping report anchors the wind-down and the cockpit may flip, but nothing is killed", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		m, err := api.workerReportStopping("ow-abc123", triggerServer)
		if err != nil {
			t.Fatalf("workerReportStopping: %v", err)
		}

		apiWantValue(t, "answer", any(wsAnchors(m)), any(map[string]any{
			"id": "ow-abc123", "desired_state": "online", "refocus_op": "",
			"refocus_since_set": false, "stopping_since_set": true,
			"stopped_since_set": false, "actual_model": "",
		}))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "online",
			"machine": "m-server-self",
		}))
	})

	t.Run("a second report leaves the first anchor exactly where it was", func(t *testing.T) {
		api, _, d, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		if _, err := api.workerReportStopping("ow-abc123", triggerServer); err != nil {
			t.Fatalf("workerReportStopping: %v", err)
		}
		first, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || first == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", first, err)
		}

		if _, err := api.workerReportStopping("ow-abc123", triggerServer); err != nil {
			t.Fatalf("workerReportStopping: %v", err)
		}

		second, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || second == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", second, err)
		}
		apiWantValue(t, "stopping anchor", any(second.StoppingSince), any(first.StoppingSince))
	})

	t.Run("a caller with no live worker row is refused not-found", func(t *testing.T) {
		api, _, _, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)

		m, err := api.workerReportStopping("ow-nope", triggerServer)
		if m != nil || err != errNotFound {
			t.Fatalf("want (nil, not found), got (%v, %v)", m, err)
		}
	})
}

func TestWorkerReportStopped(t *testing.T) {
	t.Run("a report inside a handover only LATCHES: the next tick collects, and no kill goes out here", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()

		m, effect, err := api.workerReportStopped("ow-abc123", triggerServer)
		if err != nil {
			t.Fatalf("workerReportStopped: %v", err)
		}

		apiWantValue(t, "effect", any(effect), any("latched_for_collect"))
		apiWantValue(t, "answer", any(wsAnchors(m)), any(map[string]any{
			"id": "ow-abc123", "desired_state": "online", "refocus_op": "relocate",
			"refocus_since_set": true, "stopping_since_set": false,
			"stopped_since_set": true, "actual_model": "",
		}))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": 100, "refocus_op": "relocate",
		}))
	})

	t.Run("a repeat report does nothing whatsoever and says so", func(t *testing.T) {
		api, _, _, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		if _, _, err := api.workerReportStopped("ow-abc123", triggerServer); err != nil {
			t.Fatalf("workerReportStopped: %v", err)
		}

		_, effect, err := api.workerReportStopped("ow-abc123", triggerServer)
		if err != nil {
			t.Fatalf("workerReportStopped: %v", err)
		}

		apiWantValue(t, "effect", any(effect), any("already_reported"))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
	})

	t.Run("a report inside a 停止 epoch is COLLECTED on the spot: the session is killed and never re-spawned", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOffline, "", 0, 200, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()

		_, effect, err := api.workerReportStopped("ow-abc123", triggerServer)
		if err != nil {
			t.Fatalf("workerReportStopped: %v", err)
		}

		apiWantValue(t, "effect", any(effect), any("collected"))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"machine": "m-server-self",
		}))
	})

	t.Run("a report outside any epoch is only recorded: nothing is owed and nothing is dispatched", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)

		_, effect, err := api.workerReportStopped("ow-abc123", triggerServer)
		if err != nil {
			t.Fatalf("workerReportStopped: %v", err)
		}

		apiWantValue(t, "effect", any(effect), any("recorded_only"))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self",
		}))
	})

	t.Run("a caller with no live worker row is refused not-found and carries no effect", func(t *testing.T) {
		api, _, _, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)

		m, effect, err := api.workerReportStopped("ow-nope", triggerServer)
		if m != nil || effect != "" || err != errNotFound {
			t.Fatalf("want (nil, \"\", not found), got (%v, %q, %v)", m, effect, err)
		}
	})
}

func TestWorkerRestartSelf(t *testing.T) {
	t.Run("the agent's own restart stamps a restart_self epoch and opens the graceful window, killing nothing", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		contractor := apiTestListen(t, api, "ow-abc123")
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()

		m, err := api.workerRestartSelf("ow-abc123", 4242, triggerServer)
		if err != nil {
			t.Fatalf("workerRestartSelf: %v", err)
		}

		apiWantValue(t, "answer", any(wsAnchors(m)), any(map[string]any{
			"id": "ow-abc123", "desired_state": "online", "refocus_op": "restart_self",
			"refocus_since_set": true, "stopping_since_set": false,
			"stopped_since_set": false, "actual_model": "",
		}))
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": 4242, "refocus_op": "restart_self",
		}))
		contractor.wantFrames(
			apiTestHandoverDelta(3, "online", apiTestOffboardNotice, "server"),
		)
	})

	t.Run("an agent already further along the ladder is refused, so it cannot talk back the deadline it was given", func(t *testing.T) {
		api, h, _, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "accelerated_stop", 500, 0, 0, true)
		apiTestListen(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		m, err := api.workerRestartSelf("ow-abc123", 4242, triggerServer)
		if m != nil || err != errWindDownLadderBackwards {
			t.Fatalf("want (nil, ladder backwards), got (%v, %v)", m, err)
		}
		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine":       "m-server-self",
			"refocus_since": 500, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		}))
		dashboard.wantFrames()
	})

	t.Run("an OFFLINE agent's restart skips the window and its epoch is collected immediately", func(t *testing.T) {
		api, _, _, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()

		if _, err := api.workerRestartSelf("ow-abc123", 4242, triggerServer); err != nil {
			t.Fatalf("workerRestartSelf: %v", err)
		}

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
	})

	t.Run("a caller with no live worker row is refused not-found", func(t *testing.T) {
		api, _, _, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)

		m, err := api.workerRestartSelf("ow-nope", 4242, triggerServer)
		if m != nil || err != errNotFound {
			t.Fatalf("want (nil, not found), got (%v, %v)", m, err)
		}
	})
}

func TestReclaimWorkerSession(t *testing.T) {
	t.Run("the machine the spawn targeted is fired at, the worker is marked reclaimed and its FSM bookkeeping retired", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{Phase: reconcilePhaseStarting}
		api.reclaimWorkerSession(w)
		reclaimed := api.workerReclaimed["ow-abc123"]
		states := len(api.workerReconcileStates)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "reclaimed", any(reclaimed), any(true))
		apiWantValue(t, "FSM states left", any(float64(states)), any(0))
	})

	t.Run("with no remembered target the kill is broadcast to EVERY online warden, and a warden without that session no-ops", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.reclaimWorkerSession(w)
		reclaimed := api.workerReclaimed["ow-abc123"]
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiWantValue(t, "reclaimed", any(reclaimed), any(true))
	})

	t.Run("with no online warden at all nothing is enqueued and the worker is NOT marked reclaimed, so the backstop retries", func(t *testing.T) {
		api, _, _, _, w := wsWorkerSpawnFixture(t, WorkerStatusActive)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.reclaimWorkerSession(w)
		reclaimed := len(api.workerReclaimed)
		api.outsourceMu.Unlock()

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiWantValue(t, "reclaimed", any(float64(reclaimed)), any(0))
	})
}

func TestDismissOutsourceWorkersForTask(t *testing.T) {
	t.Run("every worker bound to the task is released and its session reclaimed on the spot", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()
		dashboard := apiTestListen(t, api, "")

		api.dismissOutsourceWorkersForTask("T-1", 7777, triggerServer)

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "", "machine": "m-server-self",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "released", "server"))
	})

	t.Run("a second dismissal is a no-op: the row is already released and the session already reclaimed", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		api.dismissOutsourceWorkersForTask("T-1", 7777, triggerServer)
		wsDrainWardenFrames(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.dismissOutsourceWorkersForTask("T-1", 8888, triggerServer)

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "",
		}))
		dashboard.wantFrames()
	})

	t.Run("a task with no workers bound to it fires nothing and touches no other worker", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.dismissOutsourceWorkersForTask("T-404", 7777, triggerServer)

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})
}

func TestDismissOutsourceWorkerByID(t *testing.T) {
	t.Run("the named worker alone is released and reclaimed, and the other worker on the same task is left running", func(t *testing.T) {
		api, h, d, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusActive, Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		apiTestListen(t, api, ServerSelfHost)
		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.outsourceMu.Unlock()

		api.dismissOutsourceWorkerByID("ow-abc123", 7777, triggerServer)

		wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "", "machine": "m-server-self",
		}))
		apiTestWantWorker(t, h, owner, "ow-def456", apiTestWorkerRow(t, map[string]any{
			"id": "ow-def456", "codename": "Stevedore", "status": "active",
		}))
	})

	t.Run("an id nothing carries releases nothing and fires nothing", func(t *testing.T) {
		api, h, _, owner, _ := wsWorkerSpawnFixture(t, WorkerStatusActive)
		apiTestListen(t, api, ServerSelfHost)
		dashboard := apiTestListen(t, api, "")

		api.dismissOutsourceWorkerByID("ow-nope", 7777, triggerServer)

		wsWantWardenFrames(t, api, ServerSelfHost)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})
}

// wsFSMState is one reconcileState flattened for a whole-value compare.
func wsFSMState(st reconcileState) map[string]any {
	return map[string]any{
		"phase": st.Phase, "last_command": st.LastCommand, "last_command_at": st.LastCommandAt,
		"attempts": float64(st.Attempts), "offline_since": st.OfflineSince,
		"circuit_open": st.CircuitOpen,
	}
}

// wsWithReceipt stamps a warden receipt on the fixture worker and answers the
// re-read row with the placement pin the spawn path needs.
func wsWithReceipt(t *testing.T, api *apiServer, d *DAL, verb, reason string) OutsourceWorker {
	t.Helper()
	if err := d.SetMemberOpReceipt("ow-abc123", verb, nil, "", reason, 400); err != nil {
		t.Fatalf("SetMemberOpReceipt: %v", err)
	}
	fresh, err := d.GetOutsourceWorker("ow-abc123")
	if err != nil || fresh == nil {
		t.Fatalf("GetOutsourceWorker: %v (%v)", fresh, err)
	}
	fresh.DesiredMachineID = ServerSelfHost
	return *fresh
}

func TestReconcileWorkerLiveness(t *testing.T) {
	t.Run("a worker that should be running and is not gets a START, and the FSM records it as in flight", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusAssigned, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"start"}))
		apiWantValue(t, "fsm state", any(wsFSMState(state)), any(map[string]any{
			"phase": "starting", "last_command": "start", "last_command_at": 1000.0,
			"attempts": 0.0, "offline_since": 1000.0, "circuit_open": false,
		}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "assigned", "desired_state": "online", "machine": "m-server-self",
		}))
	})

	t.Run("an online worker converges and its stale failure receipt is taken off the cockpit", func(t *testing.T) {
		api, h, d, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, true)
		w := wsWithReceipt(t, api, d, "start", "wake_timeout: the runtime never reported")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiWantValue(t, "fsm state", any(wsFSMState(state)), any(map[string]any{
			"phase": "online", "last_command": "none", "last_command_at": 0.0,
			"attempts": 0.0, "offline_since": 0.0, "circuit_open": false,
		}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"machine": "m-server-self",
		}))
	})

	t.Run("an online worker whose agent has filed its dump-done is COLLECTED: killed and respawned, and the stop anchor is restored", func(t *testing.T) {
		api, _, _, _, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "relocate", 100, 0, 120, true)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop", "start"}))
		apiWantValue(t, "the restored stop anchor", any(map[string]any{
			"last_command": state.LastCommand, "last_command_at": state.LastCommandAt,
		}), any(map[string]any{"last_command": "stop", "last_command_at": 1000.0}))
	})

	t.Run("a START that timed out with no remembered destination says only what is known, and names no machine", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: 500,
		}
		api.reconcileWorkerLiveness(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "wake_timeout: the start window elapsed with no session, and " +
				"this server no longer has a record of which machine the start was sent to " +
				"(the spawn ledger is in-memory and a server restart clears it) — retry 改機器 " +
				"to place it again",
		}))
	})

	t.Run("a START that was collected and produced no session points at the runtime on that machine", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: 500,
		}
		api.reconcileWorkerLiveness(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "wake_timeout: the start was collected by machine " +
				"'m-server-self' but this worker never came online within the start window — " +
				"check that the 'claude' runtime actually runs and is logged in on that " +
				"machine (warden log: ocwarden.out.log)",
		}))
	})

	t.Run("a START still sitting on that machine's queue says nobody picked it up, so the runtime is not the suspect", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)
		frame, ok := buildTargetFrame(reconcileCmdStart, "ow-abc123")
		if !ok {
			t.Fatalf("buildTargetFrame refused")
		}

		api.outsourceMu.Lock()
		api.hub.EnqueueWardenCommandFor(ServerSelfHost, "ow-abc123", frame)
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: 500,
		}
		api.reconcileWorkerLiveness(w, 1000)
		api.outsourceMu.Unlock()

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "never_collected: the start frame for this worker is still " +
				"queued for machine 'm-server-self' — that machine's warden has not picked it " +
				"up, so nothing has tried to boot yet; check that ocwarden is running and " +
				"holding its connection there",
		}))
	})

	t.Run("a START drained off the queue and then LOST names the machine's connection, not the runtime on it", func(t *testing.T) {
		api, h, _, owner, w := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		apiTestListen(t, api, ServerSelfHost)
		frame, ok := buildTargetFrame(reconcileCmdStart, "ow-abc123")
		if !ok {
			t.Fatalf("buildTargetFrame refused")
		}
		api.hub.EnqueueWardenCommandFor(ServerSelfHost, "ow-abc123", frame)
		api.hub.ReturnUndeliveredCommands(ServerSelfHost, api.hub.DrainWardenCommands(ServerSelfHost))

		api.outsourceMu.Lock()
		api.workerSpawnAt["ow-abc123"] = 1
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: 500,
		}
		api.reconcileWorkerLiveness(w, 1000)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "never_collected: the start frame for this worker never reached " +
				"machine 'm-server-self' — that machine's SSE stream failed mid-delivery and " +
				"the frame was dropped server-side, so nothing there was ever asked to boot; " +
				"the machine's connection is the suspect, not the runtime on it",
		}))
	})

	t.Run("a START that bounced off the warden's clobber guard, past the confirm window, reaps the ghost and benches that machine", func(t *testing.T) {
		api, h, d, owner, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		w := wsWithReceipt(t, api, d, "start", "session_already_exists: a live session is holding the slot")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: 500, OfflineSince: 100,
		}
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		bench := wsBenchBook(api)
		pace := len(api.workerSpawnAt)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{"stop"}))
		apiWantValue(t, "bench book", any(bench), any(map[string]any{"ow-abc123|m-server-self": 1360.0}))
		apiWantValue(t, "the spawn pacing", any(float64(pace)), any(0))
		apiWantValue(t, "fsm state", any(map[string]any{
			"phase": state.Phase, "last_command": state.LastCommand, "last_command_at": state.LastCommandAt,
		}), any(map[string]any{"phase": "stopping", "last_command": "stop", "last_command_at": 1000.0}))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": nil, "last_op_at": 400,
			"last_op_reason": "session_already_exists: a live session is holding the slot",
		}))
	})

	t.Run("the same bounce INSIDE the reconnect-confirm window kills nothing and benches nothing", func(t *testing.T) {
		api, _, d, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		w := wsWithReceipt(t, api, d, "start", "session_already_exists: a live session is holding the slot")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerSpawnTarget["ow-abc123"] = ServerSelfHost
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: 500, OfflineSince: 950,
		}
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		bench := wsBenchBook(api)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiWantValue(t, "bench book", any(bench), any(map[string]any{}))
		apiWantValue(t, "fsm state", any(map[string]any{
			"phase": state.Phase, "last_command": state.LastCommand, "last_command_at": state.LastCommandAt,
		}), any(map[string]any{"phase": "starting", "last_command": "start", "last_command_at": 500.0}))
	})

	t.Run("a takeover with no known target keeps the PRIOR state so the next tick retries, and benches nothing", func(t *testing.T) {
		api, _, d, _, _ := wsWindDown(t, WorkerStatusActive, DesiredStateOnline, "", 0, 0, 0, false)
		w := wsWithReceipt(t, api, d, "start", "session_already_exists: a live session is holding the slot")
		apiTestListen(t, api, ServerSelfHost)

		api.outsourceMu.Lock()
		api.workerReconcileStates["ow-abc123"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: 500, OfflineSince: 100,
		}
		api.reconcileWorkerLiveness(w, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		bench := wsBenchBook(api)
		api.outsourceMu.Unlock()

		apiWantValue(t, "the verbs dispatched", any(wsVerbs(t, api, ServerSelfHost)), any([]any{}))
		apiWantValue(t, "bench book", any(bench), any(map[string]any{}))
		apiWantValue(t, "fsm state", any(map[string]any{
			"phase": state.Phase, "last_command": state.LastCommand, "last_command_at": state.LastCommandAt,
			"offline_since": state.OfflineSince,
		}), any(map[string]any{
			"phase": "starting", "last_command": "start", "last_command_at": 500.0,
			"offline_since": 100.0,
		}))
	})

	t.Run("a START the placement refuses leaves the PRIOR FSM state standing, so the next tick decides afresh", func(t *testing.T) {
		api, h, _, owner, w := wsWorkerSpawnFixture(t, WorkerStatusAssigned)
		unplaced := w
		unplaced.DesiredState = DesiredStateOnline

		api.outsourceMu.Lock()
		api.workerReconcileStates["ow-abc123"] = reconcileState{Phase: reconcilePhaseOffline}
		api.reconcileWorkerLiveness(unplaced, 1000)
		state := api.workerReconcileStates["ow-abc123"]
		api.outsourceMu.Unlock()

		apiWantValue(t, "fsm phase", any(state.Phase), any("offline"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"last_op": "start", "last_op_ok": false, "last_op_at": 1000,
			"last_op_reason": "no_machine_selected: no machine is selected for this worker — " +
				"pick one on the worker (改機器) or on the task type's 手冊 assignee; " +
				"there is no automatic placement",
		}))
	})
}
