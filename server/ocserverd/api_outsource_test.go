// Skeleton generated from server/ocserverd/api_outsource.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func apiTestWorkerFixture(t *testing.T, h http.Handler, d *DAL, owner, id, status string) string {
	t.Helper()
	if code, data := apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship the crate","executor_member_id":"kip"}`); code != 200 {
		t.Fatalf("create task: %d %v", code, data)
	}
	if err := d.PutOutsourceWorker(OutsourceWorker{
		ID: id, Codename: "Contractor", TaskID: "T-1", Status: status,
		Runtime: "claude", Model: "sonnet", Effort: "medium",
	}); err != nil {
		t.Fatalf("PutOutsourceWorker: %v", err)
	}
	return id
}

// apiTestWorkerWantedOnline gives a fixture worker the desired_state every
// worker the scheduler creates carries.
func apiTestWorkerWantedOnline(t *testing.T, d *DAL, id string) {
	t.Helper()
	w, err := d.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("GetOutsourceWorker: %v (%v)", w, err)
	}
	w.DesiredState = DesiredStateOnline
	if err := d.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("PutOutsourceWorker: %v", err)
	}
}

func apiTestWorkerRow(t *testing.T, over map[string]any) map[string]any {
	t.Helper()
	row := map[string]any{
		"id": "ow-abc123", "avatar_url": "", "name": "Contractor", "kind": "outsource",
		"role_key": "", "role_name": "",
		"runtime": "claude", "model": "sonnet", "effort": "medium",
		"actual_model": "", "actual_runtime": "", "actual_effort": "",
		"actual_machine": "",
		"status":         "assigned", "task_id": "T-1", "task_title": "Ship the crate",
		"task_status": "not_started", "task_no": "T-1",
		"task_created_ts": apiAnyNumber, "unread_count": 0, "presence": "offline",
		"machine": "", "desired_machine_id": "",
		"last_op": "", "last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
		"last_op_at": 0, "creator_id": "owner",
		"refocus_since": 0, "refocus_op": "", "refocus_deadline": 0,
		"desired_state": "", "forced_stop_at": 0, "roster_status": "active",
		"owner_id": "owner", "schema_version": 3,
		"terminal_attach_command":    "tmux -L officraft attach -t member-ow-abc123",
		"runtime_login_warnings":     []any{},
		"model_call_last_success_ts": 0,
		"model_call_warnings":        []any{},
	}
	allowed := map[string]bool{
		"account": true, "banked_cost": true, "compaction_count": true,
		"context_pct": true, "cost": true, "created_ts": true,
		"delegated_by": true, "task_type_key": true, "task_type_name": true,
	}
	for k, v := range over {
		if _, named := row[k]; !named && !allowed[k] {
			t.Fatalf("apiTestWorkerRow: %q is not a field of this projection", k)
		}
		if v != nil {
			row[k] = v
		}
	}
	// The server always serves this command, even for an offline worker. Keep
	// the expected session tied to the row id when a test reads a second worker.
	if id, ok := row["id"].(string); ok {
		row["terminal_attach_command"] = "tmux -L officraft attach -t member-" + strings.ToLower(id)
	}
	return row
}

func apiTestWantWorker(t *testing.T, h http.Handler, owner, workerID string, want map[string]any) {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/members/"+workerID, owner, "")
	if status != 200 {
		t.Fatalf("read back %s: %d %v", workerID, status, data)
	}
	apiWantBody(t, data, want)
}

func apiTestWantReleasedWorker(t *testing.T, d *DAL, h http.Handler, owner, workerID string, over ...map[string]any) *OutsourceWorker {
	t.Helper()
	worker, err := d.GetOutsourceWorker(workerID)
	if err != nil || worker == nil || worker.Status != WorkerStatusReleased {
		t.Fatalf("released worker %s = %+v, err=%v", workerID, worker, err)
	}
	want := map[string]any{
		"id": workerID, "status": WorkerStatusReleased,
		"roster_status": RosterStatusRemoved, "presence": "",
	}
	for _, fields := range over {
		for key, value := range fields {
			want[key] = value
		}
	}
	apiTestWantWorker(t, h, owner, workerID, apiTestWorkerRow(t, want))
	// A released worker remains addressable as durable identity through the
	// item read, but it must no longer appear in the live roster collection.
	rec := apiRequest(t, h, http.MethodGet, "/api/members", owner, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list members: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var roster []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &roster); err != nil {
		t.Fatalf("list members returned non-JSON body: %s", rec.Body.String())
	}
	for _, row := range roster {
		if row["id"] == workerID {
			t.Fatalf("released member %s remained in the live roster: %v", workerID, row)
		}
	}
	return worker
}

func apiTestWantWorkerList(t *testing.T, h http.Handler, credential string, want ...map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/members", credential, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	got := make([]any, 0, len(all))
	for _, row := range all {
		if row["kind"] == KindOutsource {
			got = append(got, row)
		}
	}
	rows := make([]any, len(want))
	for i := range want {
		rows[i] = want[i]
	}
	apiWantValue(t, "body", got, rows)
}

func apiTestWorkerDelta(seq int, status, trigger string) map[string]any {
	return apiTestWorkerStateDelta(seq, status, "", trigger)
}

func apiTestWorkerStateDelta(seq int, status, desiredState, trigger string) map[string]any {
	rosterStatus := RosterStatusActive
	op := "patch"
	deleted := false
	var payload any = map[string]any{
		"id": "ow-abc123", "name": "Contractor", "status": rosterStatus,
		"desired_state": desiredState, "owner_id": "owner",
	}
	if status == WorkerStatusReleased {
		rosterStatus = RosterStatusRemoved
		op = "remove"
		deleted = true
		payload = nil
	} else {
		payload.(map[string]any)["status"] = rosterStatus
	}
	return map[string]any{
		"seq": seq, "topic": "member", "op": op,
		"data": map[string]any{
			"entity": "member", "key": "owner::ow-abc123",
			"epoch": seq, "deleted": deleted, "payload": payload,
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func apiTestHandoverDelta(seq int, desiredState string, notice any, trigger string) map[string]any {
	return map[string]any{
		"seq": seq, "topic": "member", "op": "patch",
		"data": map[string]any{
			"entity": "member", "key": "owner::ow-abc123",
			"epoch": seq, "deleted": false,
			"payload": map[string]any{
				"id": "ow-abc123", "name": "Contractor", "status": "active",
				"owner_id": "owner", "desired_state": desiredState,
				"offboard_notice": notice,
			},
		},
		"ts": apiAnyNumber, "trigger": trigger,
	}
}

func TestProjectWorker(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	worker := OutsourceWorker{
		ID: "ow-abc123", Codename: "Contractor", Runtime: "claude",
		Model: "sonnet", Effort: "medium", TaskID: "T-1",
		Status: WorkerStatusActive, CreatedTS: 12,
	}
	task := &Task{ID: "T-1", Title: "Ship the crate", Status: TaskStatusInProgress,
		TypeKey: "crate", CreatorID: wireOwnerID, CreatedTS: 10}
	tele := map[string]map[string]any{
		worker.ID: {"account": "acct-1", accountRuntimeKey: "claude", "cost": 2.5, "machine": "m-tele"},
	}
	gauge := map[string]map[string]any{worker.ID: {"context_pct": 30.0}}
	machines := newMachineDirectory([]Member{
		{ID: "m-dispatch", Name: "dispatch", Kind: KindWarden, RosterStatus: RosterStatusActive},
		{ID: "m-tele", Name: "tele", Kind: KindWarden, RosterStatus: RosterStatusActive},
		{ID: "m-pin", Name: "pin-box", Kind: KindWarden, RosterStatus: RosterStatusActive},
	}, map[string]string{"m-dispatch": "Dispatch box", "m-tele": "Telemetry box"})
	accountDisplay := func(key string) string { return "Studio " + key }
	typeNames := map[string]string{"crate": "裝箱"}

	t.Run("uses the in-memory dispatch target when one exists", func(t *testing.T) {
		api.workerSpawnTarget[worker.ID] = "m-dispatch"
		got := api.projectWorker(worker, task, 3, 1700000000, tele, gauge, machines,
			accountDisplay, typeNames, modelCallBoard{})

		if got.Machine != "Dispatch box" {
			t.Fatalf("machine: want Dispatch box, got %q", got.Machine)
		}
		if got.TaskTitle != task.Title || got.TaskTypeName != "裝箱" {
			t.Fatalf("task projection: %+v", got)
		}
		if got.UnreadCount != 3 || got.Account == nil || *got.Account != "Studio acct-1" {
			t.Fatalf("runtime projection: %+v", got)
		}
	})

	t.Run("falls back to the observed telemetry host after a restart", func(t *testing.T) {
		delete(api.workerSpawnTarget, worker.ID)
		got := api.projectWorker(worker, task, 0, 1700000000, tele, gauge, machines,
			accountDisplay, typeNames, modelCallBoard{})

		if got.Machine != "Telemetry box" {
			t.Fatalf("machine fallback: want Telemetry box, got %q", got.Machine)
		}
	})

	t.Run("under logged-out runtimes on its shown machine and on its pending destination, the warnings name both pairs", func(t *testing.T) {
		api.workerSpawnTarget[worker.ID] = "m-dispatch"
		t.Cleanup(func() { delete(api.workerSpawnTarget, worker.ID) })
		api.telemetry.Set("m-dispatch", map[string]any{"runtimes_ts": nowSecs(), "runtimes": map[string]any{
			"codex": map[string]any{"installed": true, "logged_in": false},
		}})
		api.telemetry.Set("m-pin", map[string]any{"runtimes_ts": nowSecs(), "runtimes": map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": false},
		}})
		moving := worker
		moving.ActualRuntime = "codex"
		moving.DesiredMachineID = "m-pin"

		got := api.projectWorker(moving, task, 0, 1700000000, tele, gauge, machines,
			accountDisplay, typeNames, modelCallBoard{})

		apiWantValue(t, "warnings", any(got.RuntimeLoginWarnings), any([]RuntimeLoginWarningDTO{
			{MachineId: "m-dispatch", MachineName: "Dispatch box", Pending: false, Runtime: "codex"},
			{MachineId: "m-pin", MachineName: "pin-box", Pending: true, Runtime: "claude"},
		}))
	})

	t.Run("under a worker running where its runtime is logged in, with nothing pending, no warning", func(t *testing.T) {
		api.workerSpawnTarget[worker.ID] = "m-pin"
		t.Cleanup(func() { delete(api.workerSpawnTarget, worker.ID) })
		api.telemetry.Set("m-pin", map[string]any{"runtimes_ts": nowSecs(), "runtimes": map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": true},
		}})
		settled := worker
		settled.ActualRuntime = "claude"
		settled.DesiredMachineID = "m-pin"

		got := api.projectWorker(settled, task, 0, 1700000000, tele, gauge, machines,
			accountDisplay, typeNames, modelCallBoard{})

		apiWantValue(t, "warnings", any(got.RuntimeLoginWarnings), any([]RuntimeLoginWarningDTO{}))
	})

	t.Run("under a logged-out runtime on its shown machine whose reading is stale, no warning", func(t *testing.T) {
		api.workerSpawnTarget[worker.ID] = "m-dispatch"
		t.Cleanup(func() { delete(api.workerSpawnTarget, worker.ID) })
		api.telemetry.Set("m-dispatch", map[string]any{"runtimes_ts": nowSecs() - telemetryFreshSecs - 1, "runtimes": map[string]any{
			"codex": map[string]any{"installed": true, "logged_in": false},
		}})
		stale := worker
		stale.Runtime = "codex"
		stale.ActualRuntime = "codex"

		got := api.projectWorker(stale, task, 0, 1700000000, tele, gauge, machines,
			accountDisplay, typeNames, modelCallBoard{})

		apiWantValue(t, "warnings", any(got.RuntimeLoginWarnings), any([]RuntimeLoginWarningDTO{}))
	})
}

func TestTaskTypeDisplayNames(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	for _, manual := range []TaskManual{
		{TypeKey: "crate", DisplayName: "裝箱"},
		{TypeKey: "raw-key", DisplayName: ""},
	} {
		if err := d.PutTaskManual(manual); err != nil {
			t.Fatalf("PutTaskManual(%q): %v", manual.TypeKey, err)
		}
	}

	got := api.taskTypeDisplayNames()
	if got["crate"] != "裝箱" {
		t.Fatalf("display name for crate: want 裝箱, got %q", got["crate"])
	}
	if got["builtin-role-design"] != "建立／修改角色" {
		t.Fatalf("display name for an unedited built-in: want 建立／修改角色, got %q", got["builtin-role-design"])
	}
	if _, ok := got["raw-key"]; ok {
		t.Fatalf("blank display name should be omitted: %v", got)
	}
}

func TestWorkerDelegatedName(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	if err := d.PutMember(Member{ID: "kip", Name: "Kip", Kind: KindStaff}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	for _, tc := range []struct {
		name string
		task *Task
		want string
	}{
		{name: "nil task", task: nil, want: ""},
		{name: "owner creator", task: &Task{CreatorID: wireOwnerID}, want: ""},
		{name: "empty creator", task: &Task{}, want: ""},
		{name: "known member", task: &Task{CreatorID: "kip"}, want: "Kip"},
		{name: "removed member", task: &Task{CreatorID: "ghost"}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := api.workerDelegatedName(tc.task); got != tc.want {
				t.Fatalf("workerDelegatedName: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestHandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet(t *testing.T) {
	t.Run("the preview answers the re-assembled boot text and nothing else", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"context": apiAnyString})
		dashboard.wantFrames()
	})

	t.Run("a released worker still previews, because its rows are the audit trail", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"context": apiAnyString})
	})

	t.Run("an id nothing carries answers 404 naming the worker", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-nope/boot-context", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
	})

	t.Run("a worker whose bound task is gone answers 404 naming the task", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", TaskID: "T-9",
			Status: WorkerStatusAssigned,
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-9' not found")
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a malformed body is ignored and the boot-context preview remains observable", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"context": apiAnyString})
		dashboard.wantFrames()
	})
}

func TestHandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost(t *testing.T) {
	t.Run("moving a live worker pins the machine, opens a wind-down and answers the deferred receipt", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123", "relocation_pending": true, "relocation_deferred": true,
		})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"desired_machine_id": "m-server-self",
			"refocus_since":      apiAnyNumber, "refocus_op": "relocate",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "server"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
		)
		// T-197: the worker's own stream carries BOTH frames, not one. Before the
		// convergence the dashboard read an owner-scoped worker topic and the
		// contractor read a separate handover topic, so each saw a different
		// subset; now there is one member topic and one payload, and the
		// contractor is simply another subscriber to it. What this line exists
		// to pin has not moved: the worker being relocated RECEIVES the offboard
		// notice on its own stream — that is asserted on the first frame, which
		// is the server-triggered one. The second is the owner-triggered patch
		// of the same row, carrying the same notice; a subscriber seeing its own
		// row change twice is the ordinary shape here, not a duplicate delivery.
		contractor.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "server"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
		)
		bystander.wantFrames()
		push()
	})

	t.Run("moving a worker with no live session onto a machine that is offline pins it and answers the move as pending", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		apiTestWorkerWantedOnline(t, d, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123", "relocation_pending": true})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"desired_state": "online", "desired_machine_id": "m-server-self",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "machine_unavailable: machine 'm-server-self' is offline; " +
				"no other machine is substituted",
		}))
		dashboard.wantFrames(
			apiTestWorkerStateDelta(2, "assigned", "online", "server"),
			apiTestWorkerStateDelta(3, "assigned", "online", "owner"),
		)
		bystander.wantFrames()
	})

	t.Run("the id in the path picks the worker that moves, and the other one is left where it was", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet",
			Effort: "medium", DesiredState: DesiredStateOnline,
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/ow-def456/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-def456", "relocation_pending": true})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
	})

	t.Run("an absent machine_id answers 422 and moves nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: machine_id")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a blank machine_id answers 400 refusing to clear the pin, and moves nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"machine_id must name a machine: a relocate moves an agent to a specific "+
				"machine, and no longer clears its placement")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a machine id the registry does not carry answers 404 naming it, and moves nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":"m-nope"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-nope' not found")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a released worker answers 404 naming it, because there is no session to move", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")
		// Lifecycle resolution still refuses a released worker, while the item
		// read preserves its durable identity and the live roster omits it.
		// apiTestWantReleasedWorker pins all three sides of that boundary.
		apiTestWantReleasedWorker(t, d, h, owner, "ow-abc123")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", housekeeper,
			`{"machine_id":"m-server-self"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", "",
			`{"machine_id":"m-server-self"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
	})

	t.Run("a body the decoder rejects answers 422 before the domain is reached", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		status, data = apiJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self","hostname":"loft"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "hostname"`)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	for _, shape := range windowDALShapes {
		t.Run(shape+": a wind-down whose epoch write fails leaves no epoch behind", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := windowMemberDoorStack(t, d, windowMemberDoor{worker: true, live: true})
			windowRefuse(t, d, "refuse_epoch", `BEFORE UPDATE OF refocus_since ON member WHEN NEW.id = 'ow-abc123'`,
				"the epoch write fails")

			status, data := windowJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner, `{"machine_id":"m-server-self"}`)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			if got := apiTestMemberRow(t, d, "ow-abc123"); got.RefocusSince != 0 || got.RefocusOp != "" {
				t.Fatalf("refocus_since=%v refocus_op=%q, want no epoch", got.RefocusSince, got.RefocusOp)
			}
		})

		t.Run(shape+": a queued restart whose receipt fails to land is not queued", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := windowMemberDoorStack(t, d, windowMemberDoor{worker: true,
				prepare: `UPDATE member SET desired_state = 'offline', stopping_since = 1700000000 WHERE id = 'ow-abc123'`})
			windowRefuse(t, d, "refuse_receipt", "BEFORE UPDATE OF last_op ON member", "the receipt write fails")

			status, data := windowJSON(t, h, "POST", "/api/members/ow-abc123/relocate", owner, `{"machine_id":"m-server-self"}`)

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			if got := apiTestMemberRow(t, d, "ow-abc123"); got.RestartAfterStop {
				t.Fatalf("restart_after_stop landed without its receipt")
			}
		})
	}
}

func TestRelocateWorkerByID(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

	t.Run("persists a valid machine pin and answers a receipt", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/members/ow-abc123/relocate", nil)
		api.relocateWorkerByID(rec, req, "ow-abc123", "m-server-self")
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode receipt: %v", err)
		}
		if body["id"] != "ow-abc123" {
			t.Fatalf("receipt id: want ow-abc123, got %v", body["id"])
		}
		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil {
			t.Fatalf("GetOutsourceWorker: %v", err)
		}
		if worker == nil || worker.DesiredMachineID != "m-server-self" {
			t.Fatalf("persisted machine pin: %+v", worker)
		}
	})

	t.Run("refuses an unknown machine before touching the worker", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/members/ow-abc123/relocate", nil)
		api.relocateWorkerByID(rec, req, "ow-abc123", "m-nope")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		errBody, ok := body["error"].(map[string]any)
		if !ok || errBody["code"] != "not_found" {
			t.Fatalf("error code: want not_found, got %v", body["error"])
		}
	})
}

func TestHandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost(t *testing.T) {
	for _, workerStatus := range []string{WorkerStatusActive, WorkerStatusAssigned} {
		t.Run("換手 on a live "+workerStatus+" worker stamps the epoch, fans the 預告 at its own session and starts no clock", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", workerStatus)
			contractor := apiTestListen(t, api, "ow-abc123")
			dashboard := apiTestListen(t, api, "")
			bystander := apiTestListen(t, api, "kip")
			push := apiTestWebPushSink(t, api)

			status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
			apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
				"status": workerStatus, "presence": "online",
				"refocus_since": apiAnyNumber, "refocus_op": "refocus",
			}))
			// BOTH frames carry the notice: it rides EVERY write to a row whose
			// wind-down is open, not just the one that opened it (offboardDeltaPayload,
			// owner 2026-08-16) — the client de-duplicates.
			dashboard.wantFrames(
				apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
				apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
			)
			contractor.wantFrames(
				apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
				apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
			)
			bystander.wantFrames()
			push()
		})
	}

	for _, workerStatus := range []string{WorkerStatusActive, WorkerStatusAssigned} {
		t.Run("換手 on a live "+workerStatus+" worker is collected as a STOP only, and the replacement START goes out on the first tick after it reads offline", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", workerStatus)
			apiTestWorkerWantedOnline(t, d, "ow-abc123")
			if err := d.SetMemberDesiredMachineID("ow-abc123", ServerSelfHost); err != nil {
				t.Fatalf("SetMemberDesiredMachineID: %v", err)
			}
			apiTestListen(t, api, ServerSelfHost)
			session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
			if err != nil {
				t.Fatalf("hub.Connect: %v", err)
			}
			contractor := apiTestAgentToken(t, api, "ow-abc123", ServerSelfHost)

			if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); status != 200 {
				t.Fatalf("refocus: %d (%v)", status, data)
			}
			wsWantWardenFrames(t, api, ServerSelfHost)
			if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
				t.Fatalf("report_stopped: %d (%v)", status, data)
			}
			wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))

			now := nowSecs()
			api.runOutsourceTick(now)
			wsWantWardenFrames(t, api, ServerSelfHost)
			api.runOutsourceTick(now + 30)
			wsWantWardenFrames(t, api, ServerSelfHost)

			api.hub.Disconnect(session)
			status, boot := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
			if status != 200 {
				t.Fatalf("boot-context preview: %d (%v)", status, boot)
			}
			api.runOutsourceTick(now + 31)
			wsWantWardenFrames(t, api, ServerSelfHost,
				wsStartFrame("ow-abc123", boot["context"].(string), "claude", "sonnet", "medium"))
			apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
				"status": workerStatus, "presence": "waking", "desired_state": "online",
				"desired_machine_id": "m-server-self", "machine": "m-server-self",
				"refocus_since": apiAnyNumber, "refocus_op": "refocus",
			}))
		})
	}

	t.Run("換手 on a worker whose 停止 is in flight answers 200 and queues the 起來 behind it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "held_down: the refocus was saved and this member is still " +
				"being stopped — the stop in flight is honoured as-is, and it will be " +
				"started again once it is down",
		}))
		// The 停止 in flight is itself an open wind-down, so this write carries
		// the 〈停止〉 notice too (offboardDeltaPayload).
		dashboard.wantFrames(apiTestHandoverDelta(4, DesiredStateOffline, apiTestOffboardNotice, "owner"))
	})

	t.Run("換手 on a worker nobody ever asked to stop answers 409 naming 喚醒 instead", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %v", worker, err)
		}
		worker.DesiredState = DesiredStateOffline
		if err := d.PutOutsourceWorker(*worker); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		held := apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "offline", "desired_state": "offline",
		})
		apiTestWantWorker(t, h, owner, "ow-abc123", held)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus requires a live worker — this one is stopped and has never been "+
				"asked to stop, so there is no wind-down for a 起來 to be queued behind "+
				"(喚醒 it when you want it to run)")
		apiTestWantWorker(t, h, owner, "ow-abc123", held)
		dashboard.wantFrames()
	})

	t.Run("換手 with no live session answers 409 and stamps nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus requires the worker to be online (no live session to hand over)")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("換手 on a worker already in 加速停止 answers 409 rather than walking the ladder back", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, ""); code != 200 {
			t.Fatalf("accelerated-stop: %d %v", code, data)
		}
		accelerated := apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		})
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus is 停止 and this worker is already further along the wind-down "+
				"ladder (下線 → 加速 → 強制); a later stage is never replaced by an earlier one")
		apiTestWantWorker(t, h, owner, "ow-abc123", accelerated)
		dashboard.wantFrames()
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/members/ow-nope/refocus", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed body is ignored and refocus still returns its receipt and handover effects", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "refocus",
		}))
		// BOTH frames carry the notice: it rides EVERY write to a row whose
		// wind-down is open, not just the one that opened it (offboardDeltaPayload,
		// owner 2026-08-16) — the client de-duplicates.
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
		)
		bystander.wantFrames()
		push()
	})
}

func TestHandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(t *testing.T) {
	t.Run("escalating an open 換手 re-stamps the epoch under a deadline and fans the final sentence", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		refocused := apiTestMemberRow(t, d, "ow-abc123")
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		pressed := apiTestMemberRow(t, d, "ow-abc123")
		if pressed.RefocusSince <= refocused.RefocusSince {
			t.Fatalf("refocus_since=%v, want it re-stamped past the refocus at %v",
				pressed.RefocusSince, refocused.RefocusSince)
		}
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": pressed.RefocusSince, "refocus_op": "accelerated_stop",
			"refocus_deadline": pressed.RefocusSince + 120,
		}))
		// Both writes ride the open (now accelerated) wind-down, so both carry a
		// notice — the second one is not a bare roster patch.
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "", apiAnyString, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "", apiAnyString, "owner"),
		)
		bystander.wantFrames()
		push()
	})

	t.Run("escalating an open 停止 re-stamps the stop anchor and keeps the worker held down", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		stopped := apiTestMemberRow(t, d, "ow-abc123")
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		pressed := apiTestMemberRow(t, d, "ow-abc123")
		if pressed.StoppingSince <= stopped.StoppingSince {
			t.Fatalf("stopping_since=%v, want it re-stamped past the stop at %v",
				pressed.StoppingSince, stopped.StoppingSince)
		}
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"refocus_op": "accelerated_stop", "refocus_deadline": pressed.StoppingSince + 120,
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "offline", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "offline", apiAnyString, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(4, "offline", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "offline", apiAnyString, "owner"),
		)
	})

	t.Run("escalating a stop on a worker that has not reported waking yet is admitted like staff", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		pressed := apiTestMemberRow(t, d, "ow-abc123")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "assigned", "presence": "stopping", "desired_state": "offline",
			"refocus_op": "accelerated_stop", "refocus_deadline": pressed.StoppingSince + 120,
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "offline", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "offline", apiAnyString, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(4, "offline", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "offline", apiAnyString, "owner"),
		)
	})

	t.Run("pressing again on a stop already on the clock keeps its deadline anchor whichever way the grace moved", func(t *testing.T) {
		for _, grace := range []int{10, 3600} {
			t.Run("grace set to "+strconv.Itoa(grace)+" between the presses", func(t *testing.T) {
				api, h, d, owner := newAPITestServer(t)
				apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
				apiTestListen(t, api, "ow-abc123")
				if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
					t.Fatalf("stop: %d %v", code, data)
				}
				if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, ""); code != 200 {
					t.Fatalf("first press: %d %v", code, data)
				}
				first := apiTestMemberRow(t, d, "ow-abc123")
				if code, data := apiJSON(t, h, "PATCH", "/api/settings", owner,
					`{"accelerated_grace_secs":`+strconv.Itoa(grace)+`}`); code != 200 {
					t.Fatalf("settings: %d %v", code, data)
				}
				contractor := apiTestListen(t, api, "ow-abc123")

				status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
				if status != 200 {
					t.Fatalf("second press: want 200, got %d (%v)", status, data)
				}
				apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
				apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
					"status": "active", "presence": "stopping", "desired_state": "offline",
					"refocus_op": "accelerated_stop", "refocus_deadline": first.StoppingSince + float64(grace),
				}))
				contractor.wantFrames(
					apiTestHandoverDelta(6, "offline", apiAnyString, "owner"),
					apiTestHandoverDelta(7, "offline", apiAnyString, "owner"),
				)
			})
		}
	})

	t.Run("a hand-off already on the second-threshold clock keeps its anchor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		since := nowSecs() - 3600
		if err := d.SetMemberWindDownAnchors("ow-abc123", 0, 0, since, refocusOpContextHigh); err != nil {
			t.Fatalf("SetMemberWindDownAnchors: %v", err)
		}
		contractor := apiTestListen(t, api, "ow-abc123")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": since, "refocus_op": "accelerated_stop", "refocus_deadline": since + 120,
		}))
		contractor.wantFrames(
			apiTestHandoverDelta(2, "", apiAnyString, "owner"),
			apiTestHandoverDelta(3, "", apiAnyString, "owner"),
		)
	})

	t.Run("escalating a stop drops the 起來 queued behind it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		if !apiTestMemberRow(t, d, "ow-abc123").RestartAfterStop {
			t.Fatalf("setup: the refocus must have queued a 起來")
		}
		contractor := apiTestListen(t, api, "ow-abc123")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		pressed := apiTestMemberRow(t, d, "ow-abc123")
		if pressed.RestartAfterStop || pressed.RefocusOp != "accelerated_stop" {
			t.Fatalf("restart_after_stop=%v refocus_op=%q, want false and accelerated_stop",
				pressed.RestartAfterStop, pressed.RefocusOp)
		}
		contractor.wantFrames(
			apiTestHandoverDelta(5, "offline", apiAnyString, "owner"),
			apiTestHandoverDelta(6, "offline", apiAnyString, "owner"),
		)
	})

	t.Run("a force-stopped worker answers 409 saying it was force-stopped and nothing is put on a clock", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, ""); code != 200 {
			t.Fatalf("force-stop: %d %v", code, data)
		}
		forced := apiTestMemberRow(t, d, "ow-abc123")
		contractor := apiTestListen(t, api, "ow-abc123")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 has nothing to escalate — this member was already force-stopped "+
				"(強制停止): its session was cut off and no wind-down is open")
		contractor.wantFrames()
		after := apiTestMemberRow(t, d, "ow-abc123")
		if after.RefocusOp != "" || after.StoppingSince != forced.StoppingSince || forced.StoppingSince <= 0 {
			t.Fatalf("refocus_op=%q stopping_since=%v, want no cause and the force-stop anchor %v",
				after.RefocusOp, after.StoppingSince, forced.StoppingSince)
		}
	})

	t.Run("escalating a worker nobody has asked to stop answers 409 naming the rung below", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 escalates a wind-down that is already open — this member has not "+
				"been asked to stop. Press 停止 (deactivate) or 重新聚焦 (refocus) first")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
		}))
		dashboard.wantFrames()
	})

	t.Run("escalating a worker with no live session answers 409 and stamps nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 requires a live session — there is nothing to accelerate on a "+
				"member that is not connected")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a stopping worker whose task was closed answers 404 naming the id from the path and changes nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, ""); code != 200 {
			t.Fatalf("close: %d %v", code, data)
		}
		before, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || before == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", before, err)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")
		after, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || after == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", after, err)
		}
		if after.Status != WorkerStatusReleased || after.RefocusOp != "" ||
			after.StoppingSince != before.StoppingSince || after.StoppingSince <= 0 {
			t.Fatalf("status=%q refocus_op=%q stopping_since=%v, want released, no cause "+
				"and the stop anchor untouched at %v", after.Status, after.RefocusOp,
				after.StoppingSince, before.StoppingSince)
		}

		status, data = apiJSON(t, h, "POST", "/api/members/ow-nope/accelerated-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed body is ignored and accelerated-stop still returns its receipt and escalation effects", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		}))
		// Both writes ride the open (now accelerated) wind-down, so both carry a
		// notice — the second one is not a bare roster patch.
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "", apiAnyString, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestHandoverDelta(5, "", apiAnyString, "owner"),
		)
		bystander.wantFrames()
		push()
	})
}

func TestHandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost(t *testing.T) {
	t.Run("停止 holds a live worker down and asks it to work its close-out, killing nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "offline", apiTestOffboardNotice, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "offline", apiTestOffboardNotice, "owner"),
		)
		bystander.wantFrames()
		push()
	})

	t.Run("停止 on a worker with no live session takes the immediate collect", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "offline", apiTestOffboardNotice, "owner"),
		)
	})

	t.Run("停止 pressed twice keeps the worker offline and re-opens nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, ""); code != 200 {
			t.Fatalf("first stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(5, "offline", apiTestOffboardNotice, "owner"),
		)
	})

	t.Run("the id in the path picks the worker that stops, and the other one keeps running", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusActive, Runtime: "claude", Model: "sonnet",
			Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/ow-def456/deactivate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-def456"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/members/ow-nope/deactivate", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed body is ignored and stop still returns its receipt and close-out effects", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "offline", apiTestOffboardNotice, "owner"),
		)
		contractor.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestHandoverDelta(3, "offline", apiTestOffboardNotice, "owner"),
		)
		bystander.wantFrames()
		push()
	})

	for _, shape := range windowDALShapes {
		t.Run(shape+": a close-out whose latch write fails leaves no stopped latch behind and sends no kill", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			api, h, _, owner := newAPITestServerOn(t, d)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
			if err := d.SetMemberDesiredMachineID("ow-abc123", ServerSelfHost); err != nil {
				t.Fatalf("SetMemberDesiredMachineID: %v", err)
			}
			apiTestListen(t, api, ServerSelfHost)
			wsWantWardenFrames(t, api, ServerSelfHost)
			windowRefuse(t, d, "refuse_latch", `BEFORE UPDATE OF stopped_since ON member
				WHEN NEW.id = 'ow-abc123' AND NEW.stopped_since > 0`, "the latch write fails")

			status, data := windowJSON(t, h, "POST", "/api/members/ow-abc123/deactivate", owner, "")

			if status != http.StatusOK {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			if got := apiTestMemberRow(t, d, "ow-abc123"); got.StoppedSince != 0 || got.DesiredState != DesiredStateOffline {
				t.Fatalf("stopped_since=%v desired_state=%q, want 0 / offline", got.StoppedSince, got.DesiredState)
			}
			wsWantWardenFrames(t, api, ServerSelfHost)
		})
	}
}

func TestHandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost(t *testing.T) {
	t.Run("強制停止 cuts a live session off and publishes the shared offline state without an offboard notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"forced_stop_at": apiAnyNumber,
		}))
		dashboard.wantFrames(apiTestWorkerStateDelta(2, "active", "offline", "owner"))
		contractor.wantFrames(apiTestWorkerStateDelta(2, "active", "offline", "owner"))
		bystander.wantFrames()
		push()
	})

	t.Run("強制停止 on a worker whose session is already gone still holds the intent down", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
			"forced_stop_at": apiAnyNumber,
		}))
		dashboard.wantFrames(apiTestWorkerStateDelta(2, "active", "offline", "owner"))
	})

	t.Run("強制停止 pressed on an in-flight 換手 ends it down, clearing the epoch", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"forced_stop_at": apiAnyNumber,
		}))
		dashboard.wantFrames(apiTestWorkerStateDelta(4, "active", "offline", "owner"))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/members/ow-nope/force-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed body is ignored and force-stop still returns its receipt and stop effects", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"forced_stop_at": apiAnyNumber,
		}))
		dashboard.wantFrames(apiTestWorkerStateDelta(2, "active", "offline", "owner"))
		contractor.wantFrames(apiTestWorkerStateDelta(2, "active", "offline", "owner"))
		bystander.wantFrames()
		push()
	})
}

func TestHandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost(t *testing.T) {
	t.Run("喚醒 on a stopped worker records the intent and names the cause its start could not be delivered", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, ""); code != 200 {
			t.Fatalf("force-stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123", "activation_pending": true,
			"last_op_reason": "no_machine_selected: no machine is selected for this " +
				"worker — pick one on the worker (改機器) or on the task type's 手冊 " +
				"assignee; there is no automatic placement",
		})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "desired_state": "online",
			"forced_stop_at": apiAnyNumber,
			"last_op":        "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "no_machine_selected: no machine is selected for this " +
				"worker — pick one on the worker (改機器) or on the task type's 手冊 " +
				"assignee; there is no automatic placement",
		}))
		dashboard.wantFrames(
			apiTestWorkerStateDelta(3, "active", "online", "server"),
			apiTestWorkerStateDelta(4, "active", "online", "server"),
			apiTestWorkerStateDelta(5, "active", "online", "owner"),
		)
		bystander.wantFrames()
		push()
	})

	t.Run("喚醒 on a worker that is still running leaves that session alone and says so", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123",
			"last_op_reason": "session_alive: it was already running — 喚醒 left that " +
				"session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"重新聚焦 already under way on it, are untouched. To end the current " +
				"session and start a fresh one, press 強制停止 first, then 喚醒",
		})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"last_op": "start", "last_op_ok": true, "last_op_at": apiAnyNumber,
			"last_op_reason": "session_alive: it was already running — 喚醒 left that " +
				"session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"重新聚焦 already under way on it, are untouched. To end the current " +
				"session and start a fresh one, press 強制停止 first, then 喚醒",
		}))
		dashboard.wantFrames(apiTestWorkerStateDelta(2, "active", "online", "owner"))
		contractor.wantFrames(apiTestWorkerStateDelta(2, "active", "online", "owner"))
	})

	t.Run("the note 喚醒 leaves on a running worker is a success, so the online tick leaves it standing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, ""); status != 200 {
			t.Fatalf("activate: %d (%v)", status, data)
		}
		stamped, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || stamped == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", stamped, err)
		}

		api.runLifecycleTick(stamped.LastOpAt + 30)

		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"last_op": "start", "last_op_ok": true, "last_op_at": stamped.LastOpAt,
			"last_op_reason": "session_alive: it was already running — 喚醒 left that " +
				"session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"重新聚焦 already under way on it, are untouched. To end the current " +
				"session and start a fresh one, press 強制停止 first, then 喚醒",
		}))
	})

	t.Run("when the session 喚醒 left alone later ends and the worker is started again, the note is dropped and the wake stays a success", func(t *testing.T) {
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
		if status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, ""); status != 200 {
			t.Fatalf("activate: %d (%v)", status, data)
		}
		stamped, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || stamped == nil {
			t.Fatalf("GetOutsourceWorker: %v (%v)", stamped, err)
		}
		api.hub.Disconnect(session)
		status, boot := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
		if status != 200 {
			t.Fatalf("boot-context preview: %d (%v)", status, boot)
		}

		api.runOutsourceTick(stamped.LastOpAt + 30)

		wsWantWardenFrames(t, api, ServerSelfHost,
			wsStartFrame("ow-abc123", boot["context"].(string), "claude", "sonnet", "medium"))
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "waking", "desired_state": "online",
			"desired_machine_id": "m-server-self", "machine": "m-server-self",
			"last_op": "start", "last_op_ok": true, "last_op_log": "",
			"last_op_reason": "", "last_op_at": stamped.LastOpAt,
		}))
	})

	t.Run("喚醒 on a live worker leaves an 加速停止 already under way running", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/accelerated-stop", owner, ""); code != 200 {
			t.Fatalf("accelerated-stop: %d %v", code, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
			"last_op":          "start", "last_op_ok": true, "last_op_at": apiAnyNumber,
			"last_op_reason": "session_alive: it was already running — 喚醒 left that " +
				"session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"重新聚焦 already under way on it, are untouched. To end the current " +
				"session and start a fresh one, press 強制停止 first, then 喚醒",
		}))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/members/ow-nope/activate", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed activation body answers 422 before changing the worker", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		if code, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/force-stop", owner, ""); code != 200 {
			t.Fatalf("force-stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/ow-abc123/activate", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
			"forced_stop_at": apiAnyNumber,
		}))
		dashboard.wantFrames()
		bystander.wantFrames()
	})
}

func TestHandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost(t *testing.T) {
	t.Run("the three launch intents are stored on a worker with no live session, and nothing is handed over", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"model":"opus","runtime":"codex","effort":"high"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"model": "opus", "runtime": "codex", "effort": "high",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "owner"))
		bystander.wantFrames()
		push()
	})

	for _, workerStatus := range []string{WorkerStatusActive, WorkerStatusAssigned} {
		t.Run("a changed model on a live "+workerStatus+" worker opens the wind-down that carries it into the next session", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", workerStatus)
			contractor := apiTestListen(t, api, "ow-abc123")
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
				`{"model":"opus"}`)
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
			apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
				"status": workerStatus, "presence": "online", "model": "opus",
				"refocus_since": apiAnyNumber, "refocus_op": "runtime/model",
			}))
			dashboard.wantFrames(
				apiTestHandoverDelta(2, "", apiTestOffboardNotice, "server"),
				apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
			)
			contractor.wantFrames(
				apiTestHandoverDelta(2, "", apiTestOffboardNotice, "server"),
				apiTestHandoverDelta(3, "", apiTestOffboardNotice, "owner"),
			)
		})

		t.Run("a changed model on a live "+workerStatus+" worker is collected as a STOP and the replacement START carries the new model", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", workerStatus)
			apiTestWorkerWantedOnline(t, d, "ow-abc123")
			if err := d.SetMemberDesiredMachineID("ow-abc123", ServerSelfHost); err != nil {
				t.Fatalf("SetMemberDesiredMachineID: %v", err)
			}
			apiTestListen(t, api, ServerSelfHost)
			session, err := api.hub.Connect("ow-abc123", ServerSelfHost)
			if err != nil {
				t.Fatalf("hub.Connect: %v", err)
			}
			contractor := apiTestAgentToken(t, api, "ow-abc123", ServerSelfHost)

			if status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner, `{"model":"opus"}`); status != 200 {
				t.Fatalf("model: %d (%v)", status, data)
			}
			wsWantWardenFrames(t, api, ServerSelfHost)
			if status, data := apiJSON(t, h, "POST", "/api/self/stopped", contractor, `{}`); status != 200 {
				t.Fatalf("report_stopped: %d (%v)", status, data)
			}
			wsWantWardenFrames(t, api, ServerSelfHost, wsStopFrame("ow-abc123"))

			now := nowSecs()
			api.runOutsourceTick(now)
			wsWantWardenFrames(t, api, ServerSelfHost)

			api.hub.Disconnect(session)
			status, boot := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123/boot-context", owner, "")
			if status != 200 {
				t.Fatalf("boot-context preview: %d (%v)", status, boot)
			}
			api.runOutsourceTick(now + 31)
			wsWantWardenFrames(t, api, ServerSelfHost,
				wsStartFrame("ow-abc123", boot["context"].(string), "claude", "opus", "medium"))
			apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
				"status": workerStatus, "presence": "waking", "desired_state": "online",
				"desired_machine_id": "m-server-self", "machine": "m-server-self", "model": "opus",
				"refocus_since": apiAnyNumber, "refocus_op": "runtime/model",
			}))
		})
	}

	t.Run("re-saving the value the worker already runs on stores it again and opens no session", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"model":" sonnet "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
	})

	t.Run("a request that names no field at all keeps every launch intent as it stands", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames(apiTestWorkerDelta(2, "assigned", "owner"))
	})

	t.Run("a runtime outside the closed set answers 422 and stores nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"model":"opus","runtime":"gpt"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "runtime must be one of [claude codex]; got 'gpt'")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("an effort outside the closed set answers 422 and stores nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"model":"opus","effort":"turbo"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "effort must be one of [high low max medium xhigh]; got 'turbo'")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("the id in the path picks the worker that is edited, and the other one keeps its model", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet",
			Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-def456", owner,
			`{"model":"opus"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-def456"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"model":"opus"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-abc123' not found")

		status, data = apiJSON(t, h, "PATCH", "/api/members/ow-nope", owner,
			`{"model":"opus"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity may edit it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", housekeeper,
			`{"model":"opus"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"model": "opus",
		}))
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", "",
			`{"model":"opus"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
	})

	t.Run("a body the decoder rejects answers 422 before the domain is reached", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		status, data = apiJSON(t, h, "PATCH", "/api/members/ow-abc123", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "machine_id"`)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	for _, shape := range windowDALShapes {
		t.Run(shape+": a model setter that fails takes the queued restart back with it", func(t *testing.T) {
			d, _, _ := windowDAL(t, shape)
			_, h, _, owner := windowMemberDoorStack(t, d, windowMemberDoor{worker: true,
				prepare: `UPDATE member SET desired_state = 'offline', stopping_since = 1700000000 WHERE id = 'ow-abc123'`})
			before := apiTestMemberRow(t, d, "ow-abc123")
			windowRefuse(t, d, "refuse_model", "BEFORE UPDATE OF model ON member", "the model write fails")

			status, data := windowJSON(t, h, "PATCH", "/api/members/ow-abc123", owner, `{"model":"claude-opus-5"}`)

			if status != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%v)", status, data)
			}
			apiWantError(t, data, "internal_error", windowRefusal("the model write fails"))
			apiTestWantEqual(t, "the row after the failed setter", apiTestMemberRow(t, d, "ow-abc123"), before)
		})
	}
}
