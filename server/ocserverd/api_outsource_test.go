// Skeleton generated from server/ocserverd/api_outsource.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func apiTestWorkerRow(t *testing.T, over map[string]any) map[string]any {
	t.Helper()
	row := map[string]any{
		"id": "ow-abc123", "avatar_icon_id": nil, "codename": "Contractor",
		"runtime": "claude", "model": "sonnet", "effort": "medium",
		"actual_model": "", "actual_runtime": "", "actual_effort": "",
		"status": "assigned", "task_id": "T-1", "task_title": "Ship the crate",
		"task_status": "not_started", "task_no": "T-1",
		"task_created_ts": apiAnyNumber, "task_type_key": "", "task_type_name": "",
		"created_ts": 0, "unread_count": 0, "presence": "offline",
		"machine": "", "desired_machine_id": "", "actual_machine": "",
		"account": nil, "context_pct": nil, "cost": nil, "banked_cost": nil,
		"last_op": "", "last_op_ok": nil, "last_op_log": "", "last_op_reason": "",
		"last_op_at": 0, "creator_id": "owner", "delegated_by": "",
		"refocus_since": 0, "refocus_op": "", "refocus_deadline": 0,
		"desired_state":           "",
		"terminal_attach_command": "tmux -L officraft attach -t member-ow-abc123",
	}
	for k, v := range over {
		if _, named := row[k]; !named {
			t.Fatalf("apiTestWorkerRow: %q is not a field of this projection", k)
		}
		row[k] = v
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
	status, data := apiJSON(t, h, "GET", "/api/outsource-workers/"+workerID, owner, "")
	if status != 200 {
		t.Fatalf("read back %s: %d %v", workerID, status, data)
	}
	apiWantBody(t, data, want)
}

func apiTestWantWorkerList(t *testing.T, h http.Handler, credential string, want ...map[string]any) {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/outsource-workers", credential, "")
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	rows := make([]any, len(want))
	for i := range want {
		rows[i] = want[i]
	}
	apiWantValue(t, "body", got, rows)
}

func apiTestWorkerDelta(seq int, status, trigger string) map[string]any {
	return map[string]any{
		"seq": seq, "topic": "outsource_worker", "op": "patch",
		"data": map[string]any{
			"entity": "outsource_worker", "key": "owner::ow-abc123",
			"epoch": seq, "deleted": false,
			"payload": map[string]any{
				"id": "ow-abc123", "codename": "Contractor", "status": status,
			},
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
	machineNames := map[string]string{"m-dispatch": "Dispatch box", "m-tele": "Telemetry box"}
	accountDisplay := func(key string) string { return "Studio " + key }
	typeNames := map[string]string{"crate": "裝箱"}

	t.Run("uses the in-memory dispatch target when one exists", func(t *testing.T) {
		api.workerSpawnTarget[worker.ID] = "m-dispatch"
		got := api.projectWorker(worker, task, 3, 1700000000, tele, gauge, machineNames,
			accountDisplay, typeNames)

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
		got := api.projectWorker(worker, task, 0, 1700000000, tele, gauge, machineNames,
			accountDisplay, typeNames)

		if got.Machine != "Telemetry box" {
			t.Fatalf("machine fallback: want Telemetry box, got %q", got.Machine)
		}
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

func TestHandleListOutsourceWorkersApiOutsourceWorkersGet(t *testing.T) {
	t.Run("every live worker is served as a full projection row joined to its bound task", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		apiTestWantWorkerList(t, h, owner, apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("an office that has hired nobody answers an empty array", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiTestWantWorkerList(t, h, owner)
		dashboard.wantFrames()
	})

	t.Run("a released worker drops off the panel while its row still reads", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)

		apiTestWantWorkerList(t, h, owner)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "",
		}))
	})

	t.Run("the bound task's type carries the manual's display label and the creator's real name", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if code, data := apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"crate","display_name":"裝箱"}`); code != 200 {
			t.Fatalf("create manual: %d %v", code, data)
		}
		kip := apiTestAgentToken(t, api, "kip", "")
		if code, data := apiJSON(t, h, "POST", "/api/tasks", kip,
			`{"title":"Ship the crate","type_key":"crate","executor_member_id":"kip"}`); code != 200 {
			t.Fatalf("create task: %d %v", code, data)
		}
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet",
			Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		apiTestWantWorkerList(t, h, owner, apiTestWorkerRow(t, map[string]any{
			"task_type_key": "crate", "task_type_name": "裝箱",
			"creator_id": "kip", "delegated_by": "Kip",
		}))
	})

	t.Run("the caller's own unread count for that worker's conversation rides its row", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestAgentToken(t, api, "ow-abc123", "")
		if code, data := apiJSON(t, h, "POST", "/api/chat", contractor,
			`{"to":"owner","body":"報告"}`); code != 200 {
			t.Fatalf("post chat: %d %v", code, data)
		}

		apiTestWantWorkerList(t, h, owner, apiTestWorkerRow(t, map[string]any{
			"status": "active", "unread_count": 1,
		}))
	})

	t.Run("a plain agent identity is served the same roster, because this row sits at the machine floor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		apiTestWantWorkerList(t, h, housekeeper, apiTestWorkerRow(t, nil))
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a malformed body is ignored and the live worker list remains fully observable", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/outsource-workers", owner, `{{{`)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{apiTestWorkerRow(t, nil)})
		dashboard.wantFrames()
	})
}

func TestHandleGetOutsourceWorkerApiOutsourceWorkersIdGet(t *testing.T) {
	t.Run("one worker is served as the identical projection the list serves", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("the id in the path picks the row, and the other worker is not what comes back", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusActive, Runtime: "codex", Model: "opus",
			Effort: "high",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-def456", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestWorkerRow(t, map[string]any{
			"id": "ow-def456", "codename": "Stevedore", "runtime": "codex",
			"model": "opus", "effort": "high", "status": "active",
		}))
	})

	t.Run("an id nothing carries answers 404 naming it and fans nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-nope", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity reads it too, because this row sits at the machine floor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123", housekeeper, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestWorkerRow(t, nil))
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("a malformed body is ignored and the worker detail remains fully observable", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/outsource-workers/ow-abc123", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})
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
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
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
			apiTestWorkerDelta(2, "active", "server"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "server"),
			apiTestWorkerDelta(4, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(3, "", apiTestOffboardNotice, "server"))
		bystander.wantFrames()
		push()
	})

	t.Run("moving a worker with no live session pins the machine and answers the bare receipt", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"desired_machine_id": "m-server-self",
			"last_op":            "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "machine_unavailable: machine 'm-server-self' is offline; " +
				"no other machine is substituted",
		}))
		dashboard.wantFrames(
			apiTestWorkerDelta(2, "assigned", "server"),
			apiTestWorkerDelta(3, "assigned", "owner"),
		)
		bystander.wantFrames()
	})

	t.Run("the id in the path picks the worker that moves, and the other one is left where it was", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-def456", Codename: "Stevedore", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet",
			Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-def456/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-def456"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
	})

	t.Run("an absent machine_id answers 422 and moves nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner, `{}`)
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "released", "presence": "",
		}))
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", housekeeper,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", "",
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/relocate", owner,
			`{"machine_id":"m-server-self","hostname":"loft"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "hostname"`)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})
}

func TestRelocateWorkerByID(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

	t.Run("persists a valid machine pin and answers a receipt", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/outsource-workers/ow-abc123/relocate", nil)
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
		req := httptest.NewRequest(http.MethodPost, "/api/outsource-workers/ow-abc123/relocate", nil)
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
	t.Run("換手 on a live worker stamps the epoch, fans the 預告 at its own session and starts no clock", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "refocus",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(3, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"))
		bystander.wantFrames()
		push()
	})

	t.Run("換手 on a worker whose 停止 is in flight answers 200 and queues the 起來 behind it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
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
		dashboard.wantFrames(apiTestWorkerDelta(4, "active", "owner"))
	})

	t.Run("換手 on a worker nobody ever asked to stop answers 409 naming 重啟 instead", func(t *testing.T) {
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"refocus requires a live worker — this one is stopped and has never been "+
				"asked to stop, so there is no wind-down for a 起來 to be queued behind "+
				"(重啟 it when you want it to run)")
		apiTestWantWorker(t, h, owner, "ow-abc123", held)
		dashboard.wantFrames()
	})

	t.Run("換手 with no live session answers 409 and stamps nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
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
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, ""); code != 200 {
			t.Fatalf("accelerated-stop: %d %v", code, data)
		}
		accelerated := apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		})
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/refocus", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", housekeeper, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", "", "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "refocus",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(3, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(2, "", apiTestOffboardNotice, "owner"))
		bystander.wantFrames()
		push()
	})
}

func TestHandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost(t *testing.T) {
	t.Run("escalating an open 換手 re-stamps the epoch under a deadline and fans the final sentence", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestWorkerDelta(5, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(4, "", apiAnyString, "owner"))
		bystander.wantFrames()
		push()
	})

	t.Run("escalating an open 停止 re-stamps the stop anchor and keeps the worker held down", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, ""); code != 200 {
			t.Fatalf("stop: %d %v", code, data)
		}
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
			"refocus_op": "accelerated_stop", "refocus_deadline": apiAnyNumber,
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "offline", apiAnyString, "owner"),
			apiTestWorkerDelta(5, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(4, "offline", apiAnyString, "owner"))
	})

	t.Run("escalating a worker nobody has asked to stop answers 409 naming the rung below", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 escalates a wind-down that is already open — this worker has not "+
				"been asked to stop. Press 停止 or 重新聚焦 first")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
		}))
		dashboard.wantFrames()
	})

	t.Run("escalating a worker with no live session answers 409 and stamps nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"加速停止 requires the worker to be online (no live session to accelerate)")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/accelerated-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", housekeeper, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", "", "")
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
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "", apiAnyString, "owner"),
			apiTestWorkerDelta(5, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(4, "", apiAnyString, "owner"))
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(3, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"))
		bystander.wantFrames()
		push()
	})

	t.Run("停止 on a worker with no live session takes the immediate collect", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(3, "active", "owner"),
		)
	})

	t.Run("停止 pressed twice keeps the worker offline and re-opens nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, ""); code != 200 {
			t.Fatalf("first stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(4, "offline", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(5, "active", "owner"),
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-def456/stop", owner, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", housekeeper, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", "", "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/stop", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(
			apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"),
			apiTestWorkerDelta(3, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(2, "offline", apiTestOffboardNotice, "owner"))
		bystander.wantFrames()
		push()
	})
}

func TestHandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost(t *testing.T) {
	t.Run("強制停止 cuts a live session off, tells it nothing and patches only the cockpit", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
		contractor.wantFrames()
		bystander.wantFrames()
		push()
	})

	t.Run("強制停止 on a worker whose session is already gone still holds the intent down", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopped", "desired_state": "offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
	})

	t.Run("強制停止 pressed on an in-flight 換手 ends it down, clearing the epoch", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(4, "active", "owner"))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/force-stop", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", housekeeper, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", "", "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, `{{{`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "stopping", "desired_state": "offline",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
		contractor.wantFrames()
		bystander.wantFrames()
		push()
	})
}

func TestHandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost(t *testing.T) {
	t.Run("喚醒 on a stopped worker records the intent and names the cause its start could not be delivered", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, ""); code != 200 {
			t.Fatalf("force-stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", owner, "")
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
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "no_machine_selected: no machine is selected for this " +
				"worker — pick one on the worker (改機器) or on the task type's 手冊 " +
				"assignee; there is no automatic placement",
		}))
		dashboard.wantFrames(
			apiTestWorkerDelta(3, "active", "server"),
			apiTestWorkerDelta(4, "active", "server"),
			apiTestWorkerDelta(5, "active", "owner"),
		)
		bystander.wantFrames()
		push()
	})

	t.Run("喚醒 on a worker that is still running leaves that session alone and says so", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": "ow-abc123",
			"last_op_reason": "session_alive: this worker was already running — 喚醒 left " +
				"that session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"換手 already under way on it, are untouched. To end the current session " +
				"and start a fresh one, press 強制停止 first, then 喚醒",
		})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "session_alive: this worker was already running — 喚醒 left " +
				"that session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"換手 already under way on it, are untouched. To end the current session " +
				"and start a fresh one, press 強制停止 first, then 喚醒",
		}))
		dashboard.wantFrames(apiTestWorkerDelta(2, "active", "owner"))
		contractor.wantFrames()
	})

	t.Run("喚醒 on a live worker leaves an 加速停止 already under way running", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/refocus", owner, ""); code != 200 {
			t.Fatalf("refocus: %d %v", code, data)
		}
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/accelerated-stop", owner, ""); code != 200 {
			t.Fatalf("accelerated-stop: %d %v", code, data)
		}

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "desired_state": "online",
			"refocus_since": apiAnyNumber, "refocus_op": "accelerated_stop",
			"refocus_deadline": apiAnyNumber,
			"last_op":          "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "session_alive: this worker was already running — 喚醒 left " +
				"that session alone and dispatched nothing. Its work, and any 加速停止 or " +
				"換手 already under way on it, are untouched. To end the current session " +
				"and start a fresh one, press 強制停止 first, then 喚醒",
		}))
	})

	t.Run("a released worker answers 404 naming the id from the path", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusReleased)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/restart", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", housekeeper, "")
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active",
		}))
	})

	t.Run("a malformed body is ignored and restart still returns its receipt and activation effects", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		if code, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/force-stop", owner, ""); code != 200 {
			t.Fatalf("force-stop: %d %v", code, data)
		}
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/restart", owner, `{{{`)
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
			"last_op": "start", "last_op_ok": false, "last_op_at": apiAnyNumber,
			"last_op_reason": "no_machine_selected: no machine is selected for this " +
				"worker — pick one on the worker (改機器) or on the task type's 手冊 " +
				"assignee; there is no automatic placement",
		}))
		dashboard.wantFrames(
			apiTestWorkerDelta(3, "active", "server"),
			apiTestWorkerDelta(4, "active", "server"),
			apiTestWorkerDelta(5, "active", "owner"),
		)
		bystander.wantFrames()
		push()
	})
}

func TestHandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost(t *testing.T) {
	t.Run("the three launch intents are stored on a worker with no live session, and nothing is handed over", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")
		push := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
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

	t.Run("a changed model on a live worker opens the wind-down that carries it into the next session", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		contractor := apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
			`{"model":"opus"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": "ow-abc123"})
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, map[string]any{
			"status": "active", "presence": "online", "model": "opus",
			"refocus_since": apiAnyNumber, "refocus_op": "runtime/model",
		}))
		dashboard.wantFrames(
			apiTestWorkerDelta(2, "active", "server"),
			apiTestHandoverDelta(3, "", apiTestOffboardNotice, "server"),
			apiTestWorkerDelta(4, "active", "owner"),
		)
		contractor.wantFrames(apiTestHandoverDelta(3, "", apiTestOffboardNotice, "server"))
	})

	t.Run("re-saving the value the worker already runs on stores it again and opens no session", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusActive)
		apiTestListen(t, api, "ow-abc123")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner, `{}`)
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-def456/model", owner,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
			`{"model":"opus"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-abc123' not found")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-nope/model", owner,
			`{"model":"opus"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "outsource worker 'ow-nope' not found")
		dashboard.wantFrames()
	})

	t.Run("a plain agent identity may edit it, because this row sits at the machine floor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", housekeeper,
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", "",
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

		status, data := apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner, `{{{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		status, data = apiJSON(t, h, "POST", "/api/outsource-workers/ow-abc123/model", owner,
			`{"machine_id":"m-server-self"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "machine_id"`)
		apiTestWantWorker(t, h, owner, "ow-abc123", apiTestWorkerRow(t, nil))
		dashboard.wantFrames()
	})
}
