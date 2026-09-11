package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func taskTestUnderCaller(t *testing.T, api *apiServer, d *DAL, token string, inspect func(r *http.Request)) {
	t.Helper()
	reached := false
	gate := requireAuth(api.keys, api.authPasswordChangedAt, d.GetMember,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			inspect(r)
		}))
	req := httptest.NewRequest("POST", "/api/tasks/T-1/plan", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("the credential never reached the predicate: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTaskLog(t *testing.T) {
	t.Run("the formatted line reaches stderr under one [task] prefix and one trailing newline", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		saved := os.Stderr
		os.Stderr = w
		taskLog("close %s: reply-card sweep failed (cards left waiting): %v",
			"T-7", errors.New("database is locked"))
		os.Stderr = saved
		if err := w.Close(); err != nil {
			t.Fatalf("close pipe: %v", err)
		}
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read pipe: %v", err)
		}
		if string(out) != "[task] close T-7: reply-card sweep failed (cards left waiting): database is locked\n" {
			t.Fatalf("stderr: got %q", out)
		}
	})

	t.Run("a format with no args is emitted verbatim rather than percent-expanded", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		saved := os.Stderr
		os.Stderr = w
		taskLog("boot-reconcile: nothing to align")
		os.Stderr = saved
		if err := w.Close(); err != nil {
			t.Fatalf("close pipe: %v", err)
		}
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read pipe: %v", err)
		}
		if string(out) != "[task] boot-reconcile: nothing to align\n" {
			t.Fatalf("stderr: got %q", out)
		}
	})
}

func TestPublishTask(t *testing.T) {
	t.Run("the delta reaches the owner cockpit and the executor as an id/status/priority hint", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.publishTask(*task, "kip")

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a task with no executor narrows the fan to the owner cockpit alone", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		unbound := *task
		unbound.ExecutorID = ""
		dashboard := apiTestListen(t, api, "")
		formerExecutor := apiTestListen(t, api, "kip")

		api.publishTask(unbound, "owner")

		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		formerExecutor.wantFrames()
	})

	t.Run("the payload carries the task's current status and priority, not the ones it was created with", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		api.publishTask(*task, "owner")

		dashboard.wantFrames(map[string]any{
			"seq":   5,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   5,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "terminated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})
}

func TestInheritDispatchSpec(t *testing.T) {
	t.Run("an explicitly dispatched spec keeps every field it named and gains nothing from either source", func(t *testing.T) {
		got := inheritDispatchSpec(
			dispatchSpec{Runtime: "codex", Model: "gpt-5", Effort: "high", Machine: "m-dispatch"},
			&outsourceTypeSpec{Runtime: "claude", Model: "opus", Effort: "low", Machine: "m-manual"},
			&Member{Runtime: "claude", Model: "sonnet", Effort: "max", DesiredMachineID: "m-creator"},
		)
		want := dispatchSpec{Runtime: "codex", Model: "gpt-5", Effort: "high", Machine: "m-dispatch"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a typed dispatch takes the manual's whole spec and never the dispatcher's", func(t *testing.T) {
		got := inheritDispatchSpec(
			dispatchSpec{},
			&outsourceTypeSpec{Runtime: "claude", Model: "opus", Effort: "low", Machine: "m-manual"},
			&Member{Runtime: "codex", Model: "gpt-5", Effort: "max", DesiredMachineID: "m-creator"},
		)
		want := dispatchSpec{Runtime: "claude", Model: "opus", Effort: "low", Machine: "m-manual"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a manual that leaves the machine blank falls through to the dispatcher's own pin", func(t *testing.T) {
		got := inheritDispatchSpec(
			dispatchSpec{},
			&outsourceTypeSpec{Runtime: "claude", Model: "opus", Effort: "low"},
			&Member{Runtime: "claude", Model: "sonnet", Effort: "max", DesiredMachineID: "m-creator"},
		)
		want := dispatchSpec{Runtime: "claude", Model: "opus", Effort: "low", Machine: "m-creator"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("an ad-hoc dispatch with no manual inherits the dispatcher's runtime, model, effort and machine", func(t *testing.T) {
		got := inheritDispatchSpec(
			dispatchSpec{}, nil,
			&Member{Runtime: "codex", Model: "gpt-5", Effort: "high", DesiredMachineID: "m-creator"},
		)
		want := dispatchSpec{Runtime: "codex", Model: "gpt-5", Effort: "high", Machine: "m-creator"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a manual naming a claude runtime drops the dispatcher's codex model and keeps its machine", func(t *testing.T) {
		got := inheritDispatchSpec(
			dispatchSpec{},
			&outsourceTypeSpec{Runtime: "claude", Effort: "medium"},
			&Member{Runtime: "codex", Model: "gpt-5", Effort: "high", DesiredMachineID: "m-codex-box"},
		)
		want := dispatchSpec{Runtime: "claude", Model: "", Effort: "medium", Machine: "m-codex-box"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("with neither a manual nor a dispatcher only the two defaults land and the machine stays unset", func(t *testing.T) {
		got := inheritDispatchSpec(dispatchSpec{}, nil, nil)
		want := dispatchSpec{Runtime: "claude", Effort: "medium"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})
}

func TestFillDispatchSpecFrom(t *testing.T) {
	t.Run("every empty slot takes the source's value and no decided field is overwritten", func(t *testing.T) {
		got := fillDispatchSpecFrom(
			dispatchSpec{Runtime: "codex", Effort: "high"},
			dispatchSpec{Runtime: "claude", Model: "opus", Effort: "low", Machine: "m-src"})
		want := dispatchSpec{Runtime: "codex", Model: "", Effort: "high", Machine: "m-src"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("an empty source leaves the spec exactly as it arrived and applies no defaults of its own", func(t *testing.T) {
		got := fillDispatchSpecFrom(dispatchSpec{}, dispatchSpec{})
		if got != (dispatchSpec{}) {
			t.Fatalf("want the zero spec, got %#v", got)
		}
	})

	t.Run("a source model rides along only when the source names the runtime being dispatched", func(t *testing.T) {
		same := fillDispatchSpecFrom(dispatchSpec{Runtime: "codex"},
			dispatchSpec{Runtime: "codex", Model: "gpt-5"})
		if want := (dispatchSpec{Runtime: "codex", Model: "gpt-5"}); same != want {
			t.Fatalf("same runtime: want %#v, got %#v", want, same)
		}
		crossed := fillDispatchSpecFrom(dispatchSpec{Runtime: "claude"},
			dispatchSpec{Runtime: "codex", Model: "gpt-5"})
		if want := (dispatchSpec{Runtime: "claude"}); crossed != want {
			t.Fatalf("crossed runtime: want %#v, got %#v", want, crossed)
		}
	})

	t.Run("a source whose runtime is blank states nothing: neither runtime nor its model is taken", func(t *testing.T) {
		got := fillDispatchSpecFrom(dispatchSpec{}, dispatchSpec{Model: "gpt-5", Machine: "m-src"})
		want := dispatchSpec{Machine: "m-src"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a source runtime outside the closed set is refused, and its model with it", func(t *testing.T) {
		got := fillDispatchSpecFrom(dispatchSpec{}, dispatchSpec{Runtime: "gemini", Model: "flash"})
		if got != (dispatchSpec{}) {
			t.Fatalf("want the zero spec, got %#v", got)
		}
	})

	t.Run("a source runtime with surrounding whitespace is normalized before it is taken", func(t *testing.T) {
		got := fillDispatchSpecFrom(dispatchSpec{}, dispatchSpec{Runtime: "  codex  ", Model: "gpt-5"})
		want := dispatchSpec{Runtime: "codex", Model: "gpt-5"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("an effort outside the closed set is refused rather than copied", func(t *testing.T) {
		got := fillDispatchSpecFrom(dispatchSpec{}, dispatchSpec{Effort: "turbo"})
		if got != (dispatchSpec{}) {
			t.Fatalf("want the zero spec, got %#v", got)
		}
	})
}

func TestDefaultedDispatchSpec(t *testing.T) {
	t.Run("an empty spec gains a claude runtime and a medium effort and still names no machine", func(t *testing.T) {
		got := defaultedDispatchSpec(dispatchSpec{})
		want := dispatchSpec{Runtime: "claude", Effort: "medium"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a spec that already decided both fields is returned untouched", func(t *testing.T) {
		got := defaultedDispatchSpec(dispatchSpec{Runtime: "codex", Model: "gpt-5", Effort: "max", Machine: "m-1"})
		want := dispatchSpec{Runtime: "codex", Model: "gpt-5", Effort: "max", Machine: "m-1"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("a model with no runtime keeps the model and defaults only the runtime around it", func(t *testing.T) {
		got := defaultedDispatchSpec(dispatchSpec{Model: "gpt-5"})
		want := dispatchSpec{Runtime: "claude", Model: "gpt-5", Effort: "medium"}
		if got != want {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})
}

func TestPublishOutsourceWorker(t *testing.T) {
	t.Run("the worker delta is fanned to the owner cockpit alone as an id/codename/status hint", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %#v", err, worker)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		theWorkerItself := apiTestListen(t, api, "ow-abc123")

		api.publishOutsourceWorker(*worker, "owner")

		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "outsource_worker",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "outsource_worker",
				"key":     "owner::ow-abc123",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{
					"id": "ow-abc123", "codename": "Contractor", "status": "assigned",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		executor.wantFrames()
		theWorkerItself.wantFrames()
	})
}

func TestPublishTaskManual(t *testing.T) {
	t.Run("the manual delta is fanned to the owner cockpit alone and carries no payload", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		agent := apiTestListen(t, api, "kip")

		api.publishTaskManual("weekly_report", "owner")

		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "task_manual",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task_manual",
				"key":     "owner::weekly_report",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		agent.wantFrames()
	})
}

func TestResolveTask(t *testing.T) {
	t.Run("an id on the roster answers the whole stored row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)

		got, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if got.CreatedTS <= 0 || got.UpdatedTS <= 0 {
			t.Fatalf("want stamped timestamps, got created %v updated %v", got.CreatedTS, got.UpdatedTS)
		}
		row := *got
		row.CreatedTS, row.UpdatedTS = 0, 0
		want := Task{
			ID: "T-1", Title: "Ship it", Description: "the whole story",
			Status: "not_started", Priority: "mid",
			ExecutorKind: "staff", ExecutorID: "kip", CreatorID: "owner",
			Inputs: map[string]any{}, OutsourceRuntime: "claude",
		}
		if !reflect.DeepEqual(row, want) {
			t.Fatalf("want %#v, got %#v", want, row)
		}
	})

	t.Run("an id no task carries answers errNotFound and no row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		got, err := api.resolveTask("T-999")
		if !errors.Is(err, errNotFound) {
			t.Fatalf("want errNotFound, got %v", err)
		}
		if got != nil {
			t.Fatalf("want no row beside the error, got %#v", got)
		}
	})
}

func TestTaskDTOOf(t *testing.T) {
	t.Run("the served view carries the steps, both dependency directions, the card status and the artifact count", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-2"]}`)
		apiJSON(t, h, "POST", "/api/tasks/T-3/deps", owner, `{"blocked_by":["T-1"]}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		steps, _ := planned["steps"].([]any)
		firstStep, _ := steps[0].(map[string]any)["id"].(string)
		secondStep, _ := steps[1].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStep+"/status", agent, `{"status":"in_progress"}`)
		_, card := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+secondStep+`"}}`)
		cardID, _ := card["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)

		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dto, err := api.taskDTOOf(*task)
		if err != nil {
			t.Fatalf("taskDTOOf: %v", err)
		}
		raw, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		apiWantValue(t, "dto", got, map[string]any{
			"id":                   "T-1",
			"task_no":              "T-1",
			"type_key":             "",
			"title":                "Ship it",
			"dedupe_key":           "",
			"inputs":               map[string]any{},
			"description":          "the whole story",
			"duplicate_of":         "",
			"status":               "waiting_owner",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"handover_note":        "",
			"handover_note_ts":     0,
			"handover_note_by":     "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{"T-2"},
			"steps": []any{
				map[string]any{
					"id":                firstStep,
					"task_id":           "T-1",
					"order_idx":         0,
					"name":              "Draft",
					"dod":               "a draft exists",
					"status":            "in_progress",
					"parallel_group":    "",
					"is_gate":           false,
					"reply_card_id":     "",
					"reply_card_status": "",
					"waiting_reason":    "",
					"note_size_chars":   0,
					"note_cap_chars":    10000,
					"started_ts":        apiAnyNumber,
					"finished_ts":       0,
				},
				map[string]any{
					"id":                secondStep,
					"task_id":           "T-1",
					"order_idx":         1,
					"name":              "Review",
					"dod":               "a review is signed off",
					"status":            "waiting_owner",
					"parallel_group":    "",
					"is_gate":           false,
					"reply_card_id":     cardID,
					"reply_card_status": "waiting",
					"waiting_reason":    "",
					"note_size_chars":   0,
					"note_cap_chars":    10000,
					"started_ts":        apiAnyNumber,
					"finished_ts":       0,
				},
			},
			"detail_level":      "summary",
			"notes_included":    false,
			"progress_done":     0,
			"progress_total":    2,
			"closeout_reported": false,
			"artifact_count":    1,
			"handoff":           "",
			"handoff_note":      "",
			"handoff_task_id":   "",
			"blocking": []any{map[string]any{
				"id": "T-3", "task_no": "T-3", "title": "Waiter", "status": "not_started",
			}},
			"frozen_by":          "",
			"forced_done_by":     "",
			"forced_done_reason": "",
		})
	})

	t.Run("a bare task carries empty collections rather than nulls", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dto, err := api.taskDTOOf(*task)
		if err != nil {
			t.Fatalf("taskDTOOf: %v", err)
		}
		raw, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		apiWantValue(t, "dto.steps", got["steps"], []any{})
		apiWantValue(t, "dto.deps", got["deps"], []any{})
		apiWantValue(t, "dto.blocking", got["blocking"], []any{})
		apiWantValue(t, "dto.artifact_count", got["artifact_count"], 0)
		apiWantValue(t, "dto.progress_total", got["progress_total"], 0)
	})
}

func TestBlockingTasksOf(t *testing.T) {
	t.Run("every non-terminal waiter comes back as a display ref, and a terminated one is dropped", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Live waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Dead waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)
		apiJSON(t, h, "POST", "/api/tasks/T-3/deps", owner, `{"blocked_by":["T-1"]}`)

		before, err := api.blockingTasksOf("T-1")
		if err != nil {
			t.Fatalf("blockingTasksOf: %v", err)
		}
		want := []taskDepRefDTO{
			{ID: "T-2", TaskNo: "T-2", Title: "Live waiter", Status: "not_started"},
			{ID: "T-3", TaskNo: "T-3", Title: "Dead waiter", Status: "not_started"},
		}
		if !reflect.DeepEqual(before, want) {
			t.Fatalf("want %#v, got %#v", want, before)
		}

		if code, data := apiJSON(t, h, "POST", "/api/tasks/T-3/mark-terminated", owner, ""); code != 200 {
			t.Fatalf("terminate: %d %v", code, data)
		}
		after, err := api.blockingTasksOf("T-1")
		if err != nil {
			t.Fatalf("blockingTasksOf: %v", err)
		}
		if !reflect.DeepEqual(after, want[:1]) {
			t.Fatalf("want %#v, got %#v", want[:1], after)
		}
	})

	t.Run("a task nobody waits on answers an empty slice rather than nil", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)

		got, err := api.blockingTasksOf("T-1")
		if err != nil {
			t.Fatalf("blockingTasksOf: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("want an empty non-nil slice, got %#v", got)
		}
	})

	t.Run("the forward edge is not the answer: the blocker of a task is not its waiter", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)

		got, err := api.blockingTasksOf("T-2")
		if err != nil {
			t.Fatalf("blockingTasksOf: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("want nothing waiting on T-2, got %#v", got)
		}
	})
}

func TestTaskArtifactDTOs(t *testing.T) {
	t.Run("a replaced deliverable reports its retained versions and the set stays oldest first", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		replacedID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+replacedID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		_, second := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #125","url":"https://example.com/pr/125"}`)
		untouchedID, _ := second["artifact_id"].(string)

		got, err := api.taskArtifactDTOs("T-1")
		if err != nil {
			t.Fatalf("taskArtifactDTOs: %v", err)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		apiWantValue(t, "artifacts", decoded, []any{
			map[string]any{
				"id":            replacedID,
				"kind":          "link",
				"attachment_id": apiAnyString,
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "text/uri-list",
				"url":           "https://example.com/pr/124",
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
				"version_count": 2,
			},
			map[string]any{
				"id":            untouchedID,
				"kind":          "link",
				"attachment_id": apiAnyString,
				"name":          "PR #125",
				"description":   "",
				"filename":      "",
				"mime":          "text/uri-list",
				"url":           "https://example.com/pr/125",
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
				"version_count": 1,
			},
		})
	})

	t.Run("a row whose blob is gone reads honest-empty and falls back to its own id for a name", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		if err := d.PutTaskArtifact(TaskArtifact{
			ID: "ta-dangling", TaskID: "T-1", Kind: "file",
			AttachmentID: "att-000000000000", CreatedTS: 1750000000, CreatedBy: "kip",
		}); err != nil {
			t.Fatalf("PutTaskArtifact: %v", err)
		}

		got, err := api.taskArtifactDTOs("T-1")
		if err != nil {
			t.Fatalf("taskArtifactDTOs: %v", err)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		apiWantValue(t, "artifacts", decoded, []any{map[string]any{
			"id":            "ta-dangling",
			"kind":          "file",
			"attachment_id": "att-000000000000",
			"name":          "#dangling",
			"description":   "",
			"filename":      "",
			"mime":          "",
			"url":           "",
			"created_ts":    float64(1750000000),
			"created_by":    "kip",
			"version_count": 1,
		}})
	})

	t.Run("a task with nothing pinned answers an empty slice rather than nil", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		got, err := api.taskArtifactDTOs("T-1")
		if err != nil {
			t.Fatalf("taskArtifactDTOs: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("want an empty non-nil slice, got %#v", got)
		}
	})
}

func TestReplyCardStatusesForSteps(t *testing.T) {
	t.Run("each bound card resolves to its live status and a repeated pointer is looked up once", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, waiting := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		waitingID, _ := waiting["id"].(string)
		_, settled := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		settledID, _ := settled["id"].(string)
		if code, data := apiJSON(t, h, "POST", "/api/reply-cards/"+settledID+"/answer", owner,
			`{"option_idxs":[0]}`); code != 200 {
			t.Fatalf("answer: %d %v", code, data)
		}

		got := api.replyCardStatusesForSteps([]TaskStep{
			{ID: "ts-1", ReplyCardID: waitingID},
			{ID: "ts-2", ReplyCardID: settledID},
			{ID: "ts-3", ReplyCardID: waitingID},
		})
		want := map[string]string{waitingID: "waiting", settledID: "answered"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("card-less steps and a dangling pointer leave the map empty rather than inventing a status", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)

		got := api.replyCardStatusesForSteps([]TaskStep{
			{ID: "ts-1"},
			{ID: "ts-2", ReplyCardID: "rc-000000000000"},
		})
		if got == nil || len(got) != 0 {
			t.Fatalf("want an empty non-nil map, got %#v", got)
		}
	})
}

func TestStepCardSettled(t *testing.T) {
	settledCases := func(t *testing.T) (*apiServer, string, string, string) {
		t.Helper()
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, waiting := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		waitingID, _ := waiting["id"].(string)
		_, answered := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		answeredID, _ := answered["id"].(string)
		if code, data := apiJSON(t, h, "POST", "/api/reply-cards/"+answeredID+"/answer", owner,
			`{"option_idxs":[0]}`); code != 200 {
			t.Fatalf("answer: %d %v", code, data)
		}
		_, expired := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要加班","options":[{"text":"加"}],"linked_task":null}`)
		expiredID, _ := expired["id"].(string)
		if code, data := apiJSON(t, h, "POST", "/api/reply-cards/"+expiredID+"/expire", agent, `{}`); code != 200 {
			t.Fatalf("expire: %d %v", code, data)
		}
		return api, waitingID, answeredID, expiredID
	}

	t.Run("a card that left waiting through an answer or an expiry reads settled", func(t *testing.T) {
		api, _, answeredID, expiredID := settledCases(t)
		for _, cardID := range []string{answeredID, expiredID} {
			got, err := api.stepCardSettled(TaskStep{ID: "ts-1", ReplyCardID: cardID})
			if err != nil {
				t.Fatalf("stepCardSettled: %v", err)
			}
			if !got {
				t.Fatalf("card %s must read settled", cardID)
			}
		}
	})

	t.Run("a still-waiting card, a card-less step and a dangling pointer all read unsettled", func(t *testing.T) {
		api, waitingID, _, _ := settledCases(t)
		for _, step := range []TaskStep{
			{ID: "ts-1", ReplyCardID: waitingID},
			{ID: "ts-2"},
			{ID: "ts-3", ReplyCardID: "rc-000000000000"},
		} {
			got, err := api.stepCardSettled(step)
			if err != nil {
				t.Fatalf("stepCardSettled(%s): %v", step.ID, err)
			}
			if got {
				t.Fatalf("step %s must read unsettled", step.ID)
			}
		}
	})
}

func TestWriteTask(t *testing.T) {
	t.Run("the read face answers the whole object — steps, deps and all — as JSON", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-2"]}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type: got %q", ct)
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, map[string]any{
			"id":                   "T-1",
			"task_no":              "T-1",
			"type_key":             "",
			"title":                "Ship it",
			"dedupe_key":           "",
			"inputs":               map[string]any{},
			"description":          "the whole story",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"handover_note":        "",
			"handover_note_ts":     0,
			"handover_note_by":     "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{"T-2"},
			"steps": []any{map[string]any{
				"id":                stepID,
				"task_id":           "T-1",
				"order_idx":         0,
				"name":              "Draft",
				"dod":               "a draft exists",
				"status":            "pending",
				"parallel_group":    "",
				"is_gate":           false,
				"reply_card_id":     "",
				"reply_card_status": "",
				"waiting_reason":    "",
				"note_size_chars":   0,
				"note_cap_chars":    10000,
				"started_ts":        0,
				"finished_ts":       0,
			}},
			"detail_level":       "summary",
			"notes_included":     false,
			"progress_done":      0,
			"progress_total":     1,
			"closeout_reported":  false,
			"artifact_count":     1,
			"handoff":            "",
			"handoff_note":       "",
			"handoff_task_id":    "",
			"blocking":           []any{},
			"frozen_by":          "",
			"forced_done_by":     "",
			"forced_done_reason": "",
		})
	})
}

func TestWriteTaskWriteReceipt(t *testing.T) {
	t.Run("the receipt reports the progress pair, the dep ids, the artifact count and the description as a size and a hash", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-2"]}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"done"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "terminated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "",
			"deps":                   []any{"T-2"},
			"progress_done":          1,
			"progress_total":         2,
			"artifact_count":         1,
			"description_size_chars": 15,
			"description_sha256":     "b383ff20e6eca765a309361d7f24a2bc029dc07d3ff7e2932791594942d42760",
		})
	})

	t.Run("the description size is counted in runes rather than bytes", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"出貨已經完成"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 6)
		apiWantValue(t, "body.description_sha256", data["description_sha256"],
			"20b2c8cce5d0d217e83169f5213aeea90d023da35c4e0255406e2c3f7047c4b1")
	})

	t.Run("a task marked duplicate reports the original it points at", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The copy","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", agent, `{"duplicate_of":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-2",
			"title":                  "The copy",
			"status":                 "duplicated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "T-1",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
	})

	t.Run("a direct receipt contains empty collections for a task with no plan, dependencies or artifacts", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"desc"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		rec := httptest.NewRecorder()
		api.writeTaskWriteReceipt(rec, *task)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantBody(t, body, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 4,
			"description_sha256":     "97864e878fe129a3d4c35681c3ad4b12743f04f7cd705643f2fa1142dfede601",
		})
	})
}

func TestWriteTaskArtifactReceipt(t *testing.T) {
	t.Run("un-pinning one of two deliverables answers the artifact just touched and the shrunken set size", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, first := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		removedID, _ := first["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #124","url":"https://example.com/pr/124"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+removedID, agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    removedID,
			"artifact_count": 1,
		})
	})

	t.Run("un-pinning the last deliverable answers an empty set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 0,
		})
	})

	t.Run("a direct receipt names the touched artifact and the current set size", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		rec := httptest.NewRecorder()
		api.writeTaskArtifactReceipt(rec, *task, artifactID)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantBody(t, body, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
		})
	})
}

func TestWriteTaskCloseoutReceipt(t *testing.T) {
	t.Run("a task that reached done reports that status, and the repeat answers the first stamp again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows this"}`)
		apiMarkDone(t, h, "T-1", agent)

		status, first := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, first)
		}
		apiWantBody(t, first, map[string]any{
			"task_id":           "T-1",
			"task_status":       "done",
			"closeout_reported": true,
			"closeout_ts":       apiAnyNumber,
		})
		stamp, _ := first["closeout_ts"].(float64)

		status, repeat := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", agent, "")
		if status != 200 {
			t.Fatalf("repeat: want 200, got %d (%v)", status, repeat)
		}
		apiWantBody(t, repeat, map[string]any{
			"task_id":           "T-1",
			"task_status":       "done",
			"closeout_reported": true,
			"closeout_ts":       stamp,
		})
	})
}

func TestWriteTaskStepStatusReceipt(t *testing.T) {
	t.Run("a step held on an external party reports its reason beside the re-derived task status", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_external","waiting_reason":"the carrier has not answered"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "waiting_external",
			"waiting_reason": "the carrier has not answered",
			"task_status":    "waiting_external",
			"closed_ts":      nil,
			"progress_done":  0,
			"progress_total": 2,
		})
	})

	t.Run("the report that finishes the last step reports ready_for_done and NO closure stamp", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		steps, _ := planned["steps"].([]any)
		firstStep, _ := steps[0].(map[string]any)["id"].(string)
		lastStep, _ := steps[1].(map[string]any)["id"].(string)
		for _, stepID := range []string{firstStep, lastStep} {
			apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
			if stepID == firstStep {
				apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"done"}`)
			}
		}

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+lastStep+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows this"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        lastStep,
			"step_status":    "done",
			"waiting_reason": "",
			"task_status":    "ready_for_done",
			"closed_ts":      nil,
			"progress_done":  2,
			"progress_total": 2,
		})

		status, closed := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", agent, "")
		if status != 200 {
			t.Fatalf("mark-done: want 200, got %d (%v)", status, closed)
		}
		if closed["status"] != "done" || closed["closed_ts"] == nil {
			t.Fatalf("mark_task_done is what stamps the closure, got %v", closed)
		}
	})
}

func TestCallerMayDriveTask(t *testing.T) {
	t.Run("the task's own executor may drive it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if !api.callerMayDriveTask(r, *task) {
				t.Fatal("the executor must be allowed to drive its own task")
			}
		})
	})

	t.Run("owner scope and admin capability drive a task they do not execute", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		for _, token := range []string{owner, apiTestAgentToken(t, api, "mira", "")} {
			taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
				if !api.callerMayDriveTask(r, *task) {
					t.Fatalf("%s must be allowed to drive any task", currentActor(r))
				}
			})
		}
	})

	t.Run("a plain agent that is not the executor may not drive it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if api.callerMayDriveTask(r, *task) {
				t.Fatal("a non-executor plain agent must be refused")
			}
		})
	})

	t.Run("the creator of an unbound task earns no standing at this door", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks", agent, `{"title":"Contracted out","target":{"kind":"outsource"}}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		unbound := *task
		unbound.ExecutorID = ""
		if err := d.PutTask(unbound); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		if unbound.CreatorID != "kip" {
			t.Fatalf("want the ticket created by kip, got %q", unbound.CreatorID)
		}
		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			if api.callerMayDriveTask(r, unbound) {
				t.Fatal("the creator must not drive an unbound task")
			}
		})
	})
}

func TestCallerMayEditTaskText(t *testing.T) {
	t.Run("the creator of a task with no executor at all may edit its text", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks", agent, `{"title":"Contracted out","target":{"kind":"outsource"}}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		unbound := *task
		unbound.ExecutorID = ""
		if err := d.PutTask(unbound); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			if !api.callerMayEditTaskText(r, unbound) {
				t.Fatal("the creator of an unbound task must be allowed at the text doors")
			}
		})
	})

	t.Run("the moment an executor is bound the creator is back to refused", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks", agent, `{"title":"Contracted out","target":{"kind":"outsource"}}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		bound := *task
		bound.ExecutorID = "mira"
		bound.ExecutorKind = TaskExecutorStaff
		if err := d.PutTask(bound); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			if api.callerMayEditTaskText(r, bound) {
				t.Fatal("a bound task closes the creator's door")
			}
		})
	})

	t.Run("a plain agent that neither executes nor created the task is refused", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		creator := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/tasks", creator, `{"title":"Contracted out","target":{"kind":"outsource"}}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		unbound := *task
		unbound.ExecutorID = ""
		if err := d.PutTask(unbound); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if api.callerMayEditTaskText(r, unbound) {
				t.Fatal("a bystander must be refused")
			}
		})
	})

	t.Run("an unbound task with no creator at all admits nobody but admin capability", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Contracted out","target":{"kind":"outsource"}}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		orphan := *task
		orphan.CreatorID = ""
		orphan.ExecutorID = ""
		if err := d.PutTask(orphan); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if api.callerMayEditTaskText(r, orphan) {
				t.Fatal("the empty string is not an actor")
			}
		})
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) {
			if !api.callerMayEditTaskText(r, orphan) {
				t.Fatal("owner scope still passes on the drive rule alone")
			}
		})
	})
}

func TestCallerMayWriteHandover(t *testing.T) {
	handedOver := func(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
		t.Helper()
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		return api, h, d, owner
	}

	t.Run("the predecessor stamped on a task under the handover lock may still write it", func(t *testing.T) {
		api, _, d, _ := handedOver(t)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if task.Lock != TaskLockReassigning || task.ReassignedFrom != "kip" {
			t.Fatalf("want the reassigning lock stamped from kip, got lock %q from %q",
				task.Lock, task.ReassignedFrom)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if !api.callerMayWriteHandover(r, *task) {
				t.Fatal("the predecessor must keep the pen for the handover record")
			}
			if api.callerMayDriveTask(r, *task) {
				t.Fatal("the exception must not widen the drive guard")
			}
		})
	})

	t.Run("claiming the task closes the predecessor's window", func(t *testing.T) {
		api, h, d, _ := handedOver(t)
		successor := apiTestAgentToken(t, api, "mira", "")
		if code, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", successor, ""); code != 200 {
			t.Fatalf("claim: %d %v", code, data)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if task.Lock != "" || task.ReassignedFrom != "kip" {
			t.Fatalf("want the lock cleared with the predecessor still stamped, got lock %q from %q",
				task.Lock, task.ReassignedFrom)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			if api.callerMayWriteHandover(r, *task) {
				t.Fatal("the window closes with the lock")
			}
		})
	})

	t.Run("a third party under the same lock is refused", func(t *testing.T) {
		api, _, d, _ := handedOver(t)
		if err := d.PutMember(Member{
			ID: "rex", Name: "Rex", Kind: KindStaff, RoleKey: "engineer",
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "rex", ""), func(r *http.Request) {
			if api.callerMayWriteHandover(r, *task) {
				t.Fatal("the exception names one predecessor, not everybody")
			}
		})
	})

	t.Run("the successor and owner scope pass on the drive rule alone", func(t *testing.T) {
		api, _, d, owner := handedOver(t)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		for _, token := range []string{owner, apiTestAgentToken(t, api, "mira", "")} {
			taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
				if !api.callerMayWriteHandover(r, *task) {
					t.Fatalf("%s must pass", currentActor(r))
				}
			})
		}
	})
}

func TestTaskCallerOf(t *testing.T) {
	t.Run("an owner credential classifies as owner and needs no roster row", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) {
			c, err := api.taskCallerOf(r)
			if err != nil {
				t.Fatalf("taskCallerOf: %v", err)
			}
			if c.principal != principalOwner || c.actorID != "owner" || c.member != nil {
				t.Fatalf("got %#v", c)
			}
			if !c.isAdminCapable() || c.isOutsource() {
				t.Fatalf("owner: admin=%v outsource=%v", c.isAdminCapable(), c.isOutsource())
			}
		})
	})

	t.Run("a plain staff credential hands back the roster row and the agent class", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		token := apiTestAgentToken(t, api, "kip", "")
		taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
			c, err := api.taskCallerOf(r)
			if err != nil {
				t.Fatalf("taskCallerOf: %v", err)
			}
			if c.principal != principalAgent || c.actorID != "kip" {
				t.Fatalf("got %#v", c)
			}
			if c.member == nil || c.member.ID != "kip" || c.member.Kind != KindStaff {
				t.Fatalf("member: %#v", c.member)
			}
			if c.isAdminCapable() || c.isOutsource() {
				t.Fatalf("kip: admin=%v outsource=%v", c.isAdminCapable(), c.isOutsource())
			}
		})
	})

	t.Run("the assistant's credential classifies as admin_agent", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		token := apiTestAgentToken(t, api, "mira", "")
		taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
			c, err := api.taskCallerOf(r)
			if err != nil {
				t.Fatalf("taskCallerOf: %v", err)
			}
			if c.principal != principalAdminAgent || !c.isAdminCapable() {
				t.Fatalf("got %#v", c)
			}
		})
	})

	t.Run("an outsource worker's credential is the one that answers isOutsource", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		token := apiTestAgentToken(t, api, "ow-abc123", "")
		taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
			c, err := api.taskCallerOf(r)
			if err != nil {
				t.Fatalf("taskCallerOf: %v", err)
			}
			if c.principal != principalAgent || !c.isOutsource() || c.isAdminCapable() {
				t.Fatalf("got %#v", c)
			}
		})
	})

	t.Run("a sub with no roster row resolves to a plain agent carrying no member", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		token := apiTestAgentToken(t, api, "kip", "")
		if _, err := d.HardDeleteMember("kip"); err != nil {
			t.Fatalf("HardDeleteMember: %v", err)
		}
		taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
			c, err := api.taskCallerOf(r)
			if err != nil {
				t.Fatalf("taskCallerOf: %v", err)
			}
			if c.principal != principalAgent || c.actorID != "kip" || c.member != nil {
				t.Fatalf("got %#v", c)
			}
			if c.isOutsource() {
				t.Fatal("a missing row must not read as an outsource worker")
			}
		})
	})
}

func TestAuthorizeTaskCreate(t *testing.T) {
	outsourceCaller := taskCaller{
		principal: principalAgent, actorID: "ow-abc123",
		member: &Member{ID: "ow-abc123", Kind: KindOutsource},
	}
	staffCaller := taskCaller{
		principal: principalAgent, actorID: "kip",
		member: &Member{ID: "kip", Kind: KindStaff, RoleKey: "engineer"},
	}
	adminCaller := taskCaller{
		principal: principalAdminAgent, actorID: "mira",
		member: &Member{ID: "mira", Kind: KindStaff, RoleKey: "assistant"},
	}
	ownerCaller := taskCaller{principal: principalOwner, actorID: "owner"}

	t.Run("an outsource worker is refused before any other rule is consulted", func(t *testing.T) {
		code, reason := authorizeTaskCreate(outsourceCaller, true, "ow-abc123", "")
		if code != 403 || reason != "outsource workers may not create tasks" {
			t.Fatalf("got (%d, %q)", code, reason)
		}
	})

	t.Run("a typed task the manual assigns to someone else is refused even for the owner", func(t *testing.T) {
		want := "a typed task assigned to member 'kip' may only be created by that member"
		for _, c := range []taskCaller{ownerCaller, adminCaller} {
			code, reason := authorizeTaskCreate(c, false, "kip", "kip")
			if code != 403 || reason != want {
				t.Fatalf("%s: got (%d, %q)", c.actorID, code, reason)
			}
		}
	})

	t.Run("naming the target outsource does not slip a foreign typed task past rule 3", func(t *testing.T) {
		code, reason := authorizeTaskCreate(adminCaller, true, "kip", "")
		if code != 403 ||
			reason != "a typed task assigned to member 'kip' may only be created by that member" {
			t.Fatalf("got (%d, %q)", code, reason)
		}
	})

	t.Run("the manual's own assignee may create its typed task, dispatched or not", func(t *testing.T) {
		if code, reason := authorizeTaskCreate(staffCaller, false, "kip", "kip"); code != 0 || reason != "" {
			t.Fatalf("self-executed: got (%d, %q)", code, reason)
		}
		if code, reason := authorizeTaskCreate(staffCaller, true, "kip", ""); code != 0 || reason != "" {
			t.Fatalf("dispatched: got (%d, %q)", code, reason)
		}
	})

	t.Run("any staff caller may open an untyped 發包 ticket", func(t *testing.T) {
		for _, c := range []taskCaller{ownerCaller, adminCaller, staffCaller} {
			if code, reason := authorizeTaskCreate(c, true, "", ""); code != 0 || reason != "" {
				t.Fatalf("%s: got (%d, %q)", c.actorID, code, reason)
			}
		}
	})

	t.Run("a plain staff caller may name only itself as the executor of an ad-hoc task", func(t *testing.T) {
		if code, reason := authorizeTaskCreate(staffCaller, false, "", "kip"); code != 0 || reason != "" {
			t.Fatalf("self: got (%d, %q)", code, reason)
		}
		code, reason := authorizeTaskCreate(staffCaller, false, "", "mira")
		if code != 403 || reason != "an ad-hoc task may only name yourself as executor "+
			"(or be dispatched to an outsource worker)" {
			t.Fatalf("another member: got (%d, %q)", code, reason)
		}
	})

	t.Run("admin capability hands an ad-hoc task to any executor", func(t *testing.T) {
		for _, c := range []taskCaller{ownerCaller, adminCaller} {
			if code, reason := authorizeTaskCreate(c, false, "", "kip"); code != 0 || reason != "" {
				t.Fatalf("%s: got (%d, %q)", c.actorID, code, reason)
			}
		}
	})
}

func TestCloseTask(t *testing.T) {
	t.Run("the close stamps the status, retires the waiting card, releases the bound worker and fans all three", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"in_progress"}`)
		code, card := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0].ID+`"}}`)
		if code != 200 {
			t.Fatalf("open the card: %d %v", code, card)
		}
		cardID, _ := card["id"].(string)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		if err := api.closeTask(task, TaskStatusTerminated, 1750000000, "owner"); err != nil {
			t.Fatalf("closeTask: %v", err)
		}

		if task.Status != "terminated" || task.ClosedTS != 1750000000 || task.UpdatedTS != 1750000000 {
			t.Fatalf("returned task: %#v", *task)
		}
		stored, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if stored.Status != "terminated" || stored.ClosedTS != 1750000000 {
			t.Fatalf("stored task: %#v", *stored)
		}
		retired, err := d.GetReplyCard(cardID)
		if err != nil || retired == nil {
			t.Fatalf("GetReplyCard: %v %#v", err, retired)
		}
		if retired.Status != "expired" {
			t.Fatalf("card status: %q", retired.Status)
		}
		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %#v", err, worker)
		}
		if worker.Status != "released" {
			t.Fatalf("worker status: %q", worker.Status)
		}
		notices, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(notices) != 2 {
			t.Fatalf("want the card's companion message and the close-out notice, got %v", notices)
		}
		closeNotice := notices[1]
		if closeNotice.Recipient != "kip" || closeNotice.Meta["closed_by"] != "owner" {
			t.Fatalf("close-out notice: %#v", closeNotice)
		}

		dashboard.wantFrames(
			map[string]any{
				"seq":   7,
				"topic": "reply_card",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "reply_card",
					"key":     "owner::" + cardID,
					"epoch":   7,
					"deleted": false,
					"payload": map[string]any{"id": cardID, "from": "kip", "status": "expired"},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   8,
				"topic": "outsource_worker",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "outsource_worker",
					"key":     "owner::ow-abc123",
					"epoch":   8,
					"deleted": false,
					"payload": map[string]any{
						"id": "ow-abc123", "codename": "Contractor", "status": "released",
					},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   9,
				"topic": "task",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "task",
					"key":     "owner::T-1",
					"epoch":   9,
					"deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "terminated"},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   10,
				"topic": "chat",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "chat",
					"key":     "owner::" + closeNotice.ID,
					"epoch":   10,
					"deleted": false,
					"payload": map[string]any{
						"id": closeNotice.ID, "from": wireSystemSender, "to": "kip",
					},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
		)
	})

	t.Run("an ad-hoc task with no manual to fold learnings into is still sent the close-out notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")

		if err := api.closeTask(task, TaskStatusDone, 1750000000, "kip"); err != nil {
			t.Fatalf("closeTask: %v", err)
		}

		notices, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(notices) != 1 {
			t.Fatalf("want the one close-out notice, got %v", notices)
		}
		notice := notices[0]
		if notice.Sender != wireSystemSender || notice.Recipient != "kip" ||
			notice.Meta["task_id"] != "T-1" || notice.Meta["closed_by"] != "kip" {
			t.Fatalf("close-out notice: %#v", notice)
		}

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "done"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		noticeFrame := map[string]any{
			"seq":   3,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + notice.ID,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": notice.ID, "from": wireSystemSender, "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
	})

	t.Run("the tasks blocked by the closed one are released and told so", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"mira"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		if err := api.closeTask(task, TaskStatusDone, 1750000000, "kip"); err != nil {
			t.Fatalf("closeTask: %v", err)
		}

		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("want the release notice and the close-out notice, got %v", rows)
		}
		release := rows[0]
		if release.Recipient != "mira" || release.Meta["task_id"] != "T-2" ||
			release.Meta["task_title"] != "Waiter" {
			t.Fatalf("release notice: %#v", release)
		}
		if release.Sender != wireSystemSender {
			t.Fatalf("release notice sender: %q", release.Sender)
		}
		if rows[1].Recipient != "kip" || rows[1].Meta["task_id"] != "T-1" {
			t.Fatalf("close-out notice: %#v", rows[1])
		}

		waiter, err := api.resolveTask("T-2")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if waiter.Status != "not_started" || waiter.ClosedTS != 0 {
			t.Fatalf("the released waiter must stay open, got %#v", *waiter)
		}
	})
}

func TestNameWithIDSlot(t *testing.T) {
	t.Run("a named party carries both facts in one slot", func(t *testing.T) {
		if got := nameWithIDSlot("銀月", "mira"); got != "銀月（mira）" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a party with no name is named by its id alone", func(t *testing.T) {
		if got := nameWithIDSlot("", "mira"); got != "mira" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a label that already is the id is not repeated in a parenthesis", func(t *testing.T) {
		if got := nameWithIDSlot("mira", "mira"); got != "mira" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("an empty id under a label still composes the parenthesis", func(t *testing.T) {
		if got := nameWithIDSlot("銀月", ""); got != "銀月（）" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestDeriveAndPersistTask(t *testing.T) {
	t.Run("the task's status is re-projected from its steps, persisted and fanned", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		steps[0].Status = StepStatusWaitingExternal
		steps[0].WaitingReason = "the carrier has not answered"
		if err := d.PutTaskStep(steps[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		if err := api.deriveAndPersistTask(task, 1750000000, "kip"); err != nil {
			t.Fatalf("deriveAndPersistTask: %v", err)
		}

		if task.Status != "waiting_external" || task.WaitingReason != "the carrier has not answered" ||
			task.UpdatedTS != 1750000000 || task.ClosedTS != 0 {
			t.Fatalf("returned task: %#v", *task)
		}
		stored, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if stored.Status != "waiting_external" || stored.WaitingReason != "the carrier has not answered" {
			t.Fatalf("stored task: %#v", *stored)
		}
		dashboard.wantFrames(map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "waiting_external"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		})
	})

	t.Run("a derivation that finishes the last step lands ready_for_done and closes NOTHING", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		if err := d.PutOutsourceWorker(OutsourceWorker{
			ID: "ow-abc123", Codename: "Contractor", TaskID: "T-1",
			Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet", Effort: "medium",
		}); err != nil {
			t.Fatalf("PutOutsourceWorker: %v", err)
		}
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		steps[0].Status = StepStatusDone
		if err := d.PutTaskStep(steps[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		if err := api.deriveAndPersistTask(task, 1750000000, "kip"); err != nil {
			t.Fatalf("deriveAndPersistTask: %v", err)
		}

		if task.Status != TaskStatusReadyForDone || task.ClosedTS != 0 {
			t.Fatalf("returned task: %#v", *task)
		}
		worker, err := d.GetOutsourceWorker("ow-abc123")
		if err != nil || worker == nil {
			t.Fatalf("GetOutsourceWorker: %v %#v", err, worker)
		}
		if worker.Status != WorkerStatusAssigned {
			t.Fatalf("the derivation must not release the bound worker, got %q", worker.Status)
		}
	})

	t.Run("every ARRIVAL in ready_for_done sends 〈任務可結案〉 to the executor, numbered, and a re-derivation on a task already sitting there sends nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		finish := func(stepID string) {
			t.Helper()
			steps, err := d.ListTaskSteps("T-1")
			if err != nil {
				t.Fatalf("ListTaskSteps: %v", err)
			}
			for _, st := range steps {
				if st.ID != stepID {
					continue
				}
				st.Status = StepStatusDone
				if err := d.PutTaskStep(st); err != nil {
					t.Fatalf("PutTaskStep: %v", err)
				}
			}
			task, err := api.resolveTask("T-1")
			if err != nil {
				t.Fatalf("resolveTask: %v", err)
			}
			if err := api.deriveAndPersistTask(task, 1750000000, "kip"); err != nil {
				t.Fatalf("deriveAndPersistTask: %v", err)
			}
		}
		first, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}

		finish(first[0].ID)
		// The task has no type_key at all, so this arm also pins that the notice
		// does not depend on one — 〈任務收尾〉's body opens by reading type_key off
		// the ticket and an ad-hoc executor finds nothing to follow.
		reReadTask, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if reReadTask.TypeKey != "" {
			t.Fatalf("this arm needs an ad-hoc task, got type_key %q", reReadTask.TypeKey)
		}
		if err := api.deriveAndPersistTask(reReadTask, 1750000001, "kip"); err != nil {
			t.Fatalf("deriveAndPersistTask (re-derivation): %v", err)
		}

		second := dalTestStep("s-second", "T-1")
		second.OrderIdx = 9
		second.Status = StepStatusPending
		second.WaitingReason = ""
		second.ReplyCardID = ""
		second.IsGate = false
		second.ParallelGroup = ""
		if err := d.PutTaskStep(second); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		backToWork, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if err := api.deriveAndPersistTask(backToWork, 1750000002, "kip"); err != nil {
			t.Fatalf("deriveAndPersistTask (back to work): %v", err)
		}
		if backToWork.Status == TaskStatusReadyForDone {
			t.Fatalf("an added step must take the task back out of ready_for_done, got %q", backToWork.Status)
		}
		finish(second.ID)

		stored, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if stored.Status != TaskStatusReadyForDone || stored.ReadyForDoneVisits != 2 {
			t.Fatalf("stored task: status %q, visits %d, want ready_for_done and 2",
				stored.Status, stored.ReadyForDoneVisits)
		}
		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("chat rows = %d, want one notice per arrival", len(rows))
		}
		for i, row := range rows {
			want := api.taskNoticeText(docKindTaskReadyForDone, map[string]string{
				"task_no": "T-1", "visit_no": strconv.Itoa(i + 1),
			})
			if row.Sender != wireSystemSender || row.Recipient != "kip" || row.Body != want {
				t.Fatalf("notice %d = %#v, want the durable 〈任務可結案〉 numbered %d", i+1, row, i+1)
			}
		}
	})

	t.Run("an outsource task the scheduler has not minted a worker for yet has nobody to notify", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		unassigned := dalTestTask("T-1")
		unassigned.Status = TaskStatusInProgress
		unassigned.Lock = ""
		unassigned.ClosedTS = 0
		unassigned.ExecutorKind = TaskExecutorOutsource
		unassigned.ExecutorID = ""
		if err := d.PutTask(unassigned); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		step := dalTestStep("s-only", "T-1")
		step.OrderIdx = 1
		step.Status = StepStatusDone
		step.WaitingReason = ""
		step.ReplyCardID = ""
		step.IsGate = false
		step.ParallelGroup = ""
		if err := d.PutTaskStep(step); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		if err := api.deriveAndPersistTask(task, 1750000000, "owner"); err != nil {
			t.Fatalf("deriveAndPersistTask: %v", err)
		}

		if task.Status != TaskStatusReadyForDone || task.ReadyForDoneVisits != 1 {
			t.Fatalf("returned task: status %q, visits %d", task.Status, task.ReadyForDoneVisits)
		}
		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("chat rows = %#v, want none — there is nobody to address", rows)
		}
	})

	t.Run("an already closed task is left exactly as it was and nothing is fanned", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		before, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		task := *before
		dashboard := apiTestListen(t, api, "")

		if err := api.deriveAndPersistTask(&task, 1750000000, "kip"); err != nil {
			t.Fatalf("deriveAndPersistTask: %v", err)
		}

		if !reflect.DeepEqual(task, *before) {
			t.Fatalf("want the task untouched, got %#v", task)
		}
		after, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if !reflect.DeepEqual(*after, *before) {
			t.Fatalf("want the stored row untouched, got %#v", *after)
		}
		dashboard.wantFrames()
	})
}

func TestReconcileTaskStatusesOnBoot(t *testing.T) {
	t.Run("a drifted status is realigned, an all-done plan is left OPEN in ready_for_done, and consistent rows are left alone", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Drifted","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Finished","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Consistent","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		for _, id := range []string{"T-1", "T-2", "T-3"} {
			apiJSON(t, h, "POST", "/api/tasks/"+id+"/plan", agent,
				`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		}
		drifted, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		drifted[0].Status = StepStatusInProgress
		if err := d.PutTaskStep(drifted[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		finished, err := d.ListTaskSteps("T-2")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		finished[0].Status = StepStatusDone
		if err := d.PutTaskStep(finished[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		consistentBefore, err := api.resolveTask("T-3")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		fixed, err := api.reconcileTaskStatusesOnBoot()
		if err != nil {
			t.Fatalf("reconcileTaskStatusesOnBoot: %v", err)
		}
		if fixed != 2 {
			t.Fatalf("want two rows corrected, got %d", fixed)
		}

		realigned, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if realigned.Status != "in_progress" || realigned.ClosedTS != 0 {
			t.Fatalf("drifted task: %#v", *realigned)
		}
		// 🔴 Boot must NOT close it. Every task whose executor is still packing
		// up sits in exactly this shape, and a reconcile that closed them would
		// turn each restart into a sweep that closes every ticket waiting to be
		// closed by hand.
		finishedTask, err := api.resolveTask("T-2")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if finishedTask.Status != TaskStatusReadyForDone || finishedTask.ClosedTS != 0 {
			t.Fatalf("finished task: %#v", *finishedTask)
		}
		consistentAfter, err := api.resolveTask("T-3")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if !reflect.DeepEqual(*consistentAfter, *consistentBefore) {
			t.Fatalf("want the consistent row untouched, got %#v", *consistentAfter)
		}
	})

	t.Run("the display waiting_reason is realigned too", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Held","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		steps[0].Status = StepStatusWaitingExternal
		steps[0].WaitingReason = "the carrier has not answered"
		if err := d.PutTaskStep(steps[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}

		fixed, err := api.reconcileTaskStatusesOnBoot()
		if err != nil {
			t.Fatalf("reconcileTaskStatusesOnBoot: %v", err)
		}
		if fixed != 1 {
			t.Fatalf("want one row corrected, got %d", fixed)
		}
		got, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if got.Status != "waiting_external" || got.WaitingReason != "the carrier has not answered" {
			t.Fatalf("held task: %#v", *got)
		}
	})

	t.Run("a terminal task drifting from its steps is skipped, because its status is not derived", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Terminated","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		steps[0].Status = StepStatusInProgress
		if err := d.PutTaskStep(steps[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		before, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		fixed, err := api.reconcileTaskStatusesOnBoot()
		if err != nil {
			t.Fatalf("reconcileTaskStatusesOnBoot: %v", err)
		}
		if fixed != 0 {
			t.Fatalf("want nothing corrected, got %d", fixed)
		}
		after, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if !reflect.DeepEqual(*after, *before) {
			t.Fatalf("want the terminal row untouched, got %#v", *after)
		}
	})

	t.Run("a station whose every task already agrees with its steps corrects nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		fixed, err := api.reconcileTaskStatusesOnBoot()
		if err != nil {
			t.Fatalf("reconcileTaskStatusesOnBoot: %v", err)
		}
		if fixed != 0 {
			t.Fatalf("want nothing corrected, got %d", fixed)
		}
	})
}

func TestManualAssignee(t *testing.T) {
	t.Run("an object decodes to the map it carries", func(t *testing.T) {
		got, err := manualAssignee(TaskManual{Assignee: `{"kind":"outsource","copies":2,"machine":"m-1"}`})
		if err != nil {
			t.Fatalf("manualAssignee: %v", err)
		}
		want := map[string]any{"kind": "outsource", "copies": float64(2), "machine": "m-1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	})

	t.Run("an unset assignee decodes to an empty map rather than nil", func(t *testing.T) {
		got, err := manualAssignee(TaskManual{Assignee: ""})
		if err != nil {
			t.Fatalf("manualAssignee: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("want an empty non-nil map, got %#v", got)
		}
	})

	t.Run("the literal empty object also decodes to an empty map", func(t *testing.T) {
		got, err := manualAssignee(TaskManual{Assignee: "{}"})
		if err != nil {
			t.Fatalf("manualAssignee: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("want an empty non-nil map, got %#v", got)
		}
	})

	t.Run("assignee JSON that is not an object answers the decode error and no map", func(t *testing.T) {
		got, err := manualAssignee(TaskManual{Assignee: `["mira"]`})
		if err == nil {
			t.Fatalf("want a decode error, got %#v", got)
		}
		if got != nil {
			t.Fatalf("want no map alongside the error, got %#v", got)
		}
	})
}

func TestResumeTasksFor(t *testing.T) {
	t.Run("the row names the task, its current node and the plan text it omits", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"mira"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"done"}`)

		rows, total, err := api.resumeTasksFor("kip", nil)
		if err != nil {
			t.Fatalf("resumeTasksFor: %v", err)
		}
		if total != 1 {
			t.Fatalf("want one open task, got %d", total)
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		apiWantValue(t, "rows", got, []any{map[string]any{
			"id":                   "T-1",
			"task_no":              "T-1",
			"type_key":             "",
			"title":                "Ship it",
			"status":               "in_progress",
			"priority":             "mid",
			"waiting_reason":       "",
			"current_step_id":      steps[1].ID,
			"current_step_name":    "Review",
			"progress_done":        1,
			"progress_total":       2,
			"detail_chars":         47,
			"updated_ts":           apiAnyNumber,
			"lock":                 "",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"blocking":             []any{"T-2"},
			"answered_card_steps":  []any{},
		}})
	})

	t.Run("a step sitting on an answered card is pointed at, and one on a waiting card is not", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"in_progress"}`)
		code, card := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0].ID+`"}}`)
		if code != 200 {
			t.Fatalf("open the card: %d %v", code, card)
		}
		cardID, _ := card["id"].(string)

		waiting, err := d.GetReplyCard(cardID)
		if err != nil || waiting == nil {
			t.Fatalf("GetReplyCard: %v %#v", err, waiting)
		}
		held, _, err := api.resumeTasksFor("kip", map[string]ReplyCard{cardID: *waiting})
		if err != nil {
			t.Fatalf("resumeTasksFor: %v", err)
		}
		if len(held) != 1 || len(held[0].AnsweredCardSteps) != 0 {
			t.Fatalf("a waiting card must point at nothing, got %#v", held)
		}

		if code, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner,
			`{"option_idxs":[0]}`); code != 200 {
			t.Fatalf("answer: %d %v", code, data)
		}
		answered, err := d.GetReplyCard(cardID)
		if err != nil || answered == nil {
			t.Fatalf("GetReplyCard: %v %#v", err, answered)
		}
		released, _, err := api.resumeTasksFor("kip", map[string]ReplyCard{cardID: *answered})
		if err != nil {
			t.Fatalf("resumeTasksFor: %v", err)
		}
		if len(released) != 1 {
			t.Fatalf("want the one open task, got %#v", released)
		}
		want := []resumeAnsweredCardStepDTO{{StepID: steps[0].ID, StepName: "Draft", CardID: cardID}}
		if !reflect.DeepEqual(released[0].AnsweredCardSteps, want) {
			t.Fatalf("want %#v, got %#v", want, released[0].AnsweredCardSteps)
		}
	})

	t.Run("the block is capped at five rows while the total counts every open task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		for i := 0; i < 7; i++ {
			apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		rows, total, err := api.resumeTasksFor("kip", nil)
		if err != nil {
			t.Fatalf("resumeTasksFor: %v", err)
		}
		if len(rows) != 5 {
			t.Fatalf("want five rows, got %d", len(rows))
		}
		if total != 6 {
			t.Fatalf("want six open tasks, got %d", total)
		}
	})

	t.Run("no actor at all answers an empty block and no total", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		rows, total, err := api.resumeTasksFor("", nil)
		if err != nil {
			t.Fatalf("resumeTasksFor: %v", err)
		}
		if rows == nil || len(rows) != 0 || total != 0 {
			t.Fatalf("got %#v, %d", rows, total)
		}
	})
}

func TestHandleListTasksApiTasksGet(t *testing.T) {
	t.Run("every task on the roster is served as a full light list row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"desc"}`)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/tasks", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{map[string]any{
			"id":                   "T-1",
			"task_no":              "T-1",
			"type_key":             "",
			"title":                "Ship it",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
		dashboard.wantFrames()
	})

	t.Run("?status= narrows the answer to the tasks in that state", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Open one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Closed one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-terminated", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?status=terminated", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var terminated []any
		if err := json.Unmarshal(rec.Body.Bytes(), &terminated); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(terminated) != 1 {
			t.Fatalf("want exactly the terminated task, got %v", terminated)
		}
		apiWantValue(t, "body[0].id", terminated[0].(map[string]any)["id"], "T-2")
		apiWantValue(t, "body[0].title", terminated[0].(map[string]any)["title"], "Closed one")

		rec = apiRequest(t, h, "GET", "/api/tasks?status=not_started", owner, "")
		var open []any
		if err := json.Unmarshal(rec.Body.Bytes(), &open); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(open) != 1 {
			t.Fatalf("want exactly the open task, got %v", open)
		}
		apiWantValue(t, "body[0].id", open[0].(map[string]any)["id"], "T-1")
	})

	t.Run("?open=true drops the closed tasks and keeps the live ones", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Open one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Closed one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-terminated", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?open=true", owner, "")
		var live []any
		if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(live) != 1 {
			t.Fatalf("want exactly the open task, got %v", live)
		}
		apiWantValue(t, "body[0].id", live[0].(map[string]any)["id"], "T-1")
	})

	t.Run("?statuses= folds a repeated param into the union of those states", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"One","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Two","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-terminated", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?statuses=not_started&statuses=terminated", owner, "")
		var both []any
		if err := json.Unmarshal(rec.Body.Bytes(), &both); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(both) != 2 {
			t.Fatalf("want both tasks, got %v", both)
		}
		rec = apiRequest(t, h, "GET", "/api/tasks?statuses=done", owner, "")
		if body := rec.Body.String(); body != "[]\n" && body != "[]" {
			t.Fatalf("want an empty list for a state nothing is in, got %q", body)
		}
	})

	t.Run("?executor= keeps only the tasks that member executes", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Mira's","executor_member_id":"mira"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=mira", owner, "")
		var mine []any
		if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(mine) != 1 {
			t.Fatalf("want exactly Mira's task, got %v", mine)
		}
		apiWantValue(t, "body[0].id", mine[0].(map[string]any)["id"], "T-2")
	})

	t.Run("?type= keeps only the tasks of that type and an ad-hoc task has none", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ad-hoc","executor_member_id":"kip"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?type=daily_report", owner, "")
		if body := rec.Body.String(); body != "[]\n" && body != "[]" {
			t.Fatalf("want an empty list, got %q", body)
		}
		rec = apiRequest(t, h, "GET", "/api/tasks", owner, "")
		var all []any
		if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(all) != 1 {
			t.Fatalf("the unfiltered list must still carry the task, got %v", all)
		}
	})

	t.Run("?executor=outsource keeps only the tasks tracked on the outsource lane", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Contracted out","target":{"kind":"outsource"}}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=outsource", owner, "")
		var contracted any
		if err := json.Unmarshal(rec.Body.Bytes(), &contracted); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", contracted, []any{map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Contracted out",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "outsource",
			"executor_id":          apiAnyString,
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
	})

	t.Run("?executor=unassigned keeps only the outsource tasks no worker has taken yet", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.noOutsource = true
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Contracted out","target":{"kind":"outsource"}}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=unassigned", owner, "")
		var waiting any
		if err := json.Unmarshal(rec.Body.Bytes(), &waiting); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", waiting, []any{map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Contracted out",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "outsource",
			"executor_id":          "",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
	})

	t.Run("a ?status= outside the vocabulary answers 400 listing the whole vocabulary", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks?status=nope", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"status must be one of not_started, in_progress, waiting_owner, waiting_external, reassigning, done, terminated, duplicated")
	})

	t.Run("a ?statuses= entry outside the vocabulary answers 400 naming the offending value", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks?statuses=not_started&statuses=nope", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"statuses must each be one of not_started, in_progress, waiting_owner, "+
				"waiting_external, reassigning, done, terminated, duplicated — got 'nope'")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestParseTaskStatusSet(t *testing.T) {
	t.Run("an absent param yields no set at all rather than an empty one", func(t *testing.T) {
		set, bad := parseTaskStatusSet(nil)
		if set != nil || bad != "" {
			t.Fatalf("want (nil, \"\"), got (%#v, %q)", set, bad)
		}
	})

	t.Run("every repeat folds into the lookup set, trimmed and deduplicated", func(t *testing.T) {
		set, bad := parseTaskStatusSet(&[]string{" in_progress ", "in_progress", "reassigning", ""})
		if bad != "" {
			t.Fatalf("want no bad status, got %q", bad)
		}
		want := map[string]bool{"in_progress": true, "reassigning": true}
		if !reflect.DeepEqual(set, want) {
			t.Fatalf("want %#v, got %#v", want, set)
		}
	})

	t.Run("a param present but empty yields an empty set, not a nil one", func(t *testing.T) {
		set, bad := parseTaskStatusSet(&[]string{})
		if bad != "" {
			t.Fatalf("want no bad status, got %q", bad)
		}
		if set == nil || len(set) != 0 {
			t.Fatalf("want an empty non-nil set, got %#v", set)
		}
	})

	t.Run("a status outside the vocabulary is named back and no set is built", func(t *testing.T) {
		set, bad := parseTaskStatusSet(&[]string{"in_progress", "frozen"})
		if bad != "frozen" {
			t.Fatalf("want the offending value back, got %q", bad)
		}
		if set != nil {
			t.Fatalf("want no set beside the refusal, got %#v", set)
		}
	})
}

func TestTaskStatusSetMatch(t *testing.T) {
	t.Run("a task whose status is in the set matches", func(t *testing.T) {
		if !taskStatusSetMatch(Task{Status: "in_progress"}, map[string]bool{"in_progress": true}) {
			t.Fatal("want a match on the status itself")
		}
	})

	t.Run("a task whose status is not in the set does not match", func(t *testing.T) {
		if taskStatusSetMatch(Task{Status: "not_started"}, map[string]bool{"in_progress": true}) {
			t.Fatal("want no match")
		}
	})

	t.Run("an open task under the handover lock matches the reassigning row", func(t *testing.T) {
		task := Task{Status: "not_started", Lock: "reassigning"}
		if !taskStatusSetMatch(task, map[string]bool{"reassigning": true}) {
			t.Fatal("want the lock to answer the reassigning row")
		}
		if taskStatusSetMatch(task, map[string]bool{"in_progress": true}) {
			t.Fatal("the lock must not answer any other row")
		}
	})

	t.Run("a terminated task still carrying the lock residue does not match reassigning", func(t *testing.T) {
		if taskStatusSetMatch(Task{Status: "terminated", Lock: "reassigning"}, map[string]bool{"reassigning": true}) {
			t.Fatal("want the residue read as residue, not as intent")
		}
	})

	t.Run("an empty set matches nothing", func(t *testing.T) {
		if taskStatusSetMatch(Task{Status: "in_progress", Lock: "reassigning"}, map[string]bool{}) {
			t.Fatal("want no match against an empty set")
		}
	})
}

func TestHandleTaskCountApiTasksCountGet(t *testing.T) {
	t.Run("the badge counts the non-terminal tasks and the whole population", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"One","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Two","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-terminated", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"open": 1, "total": 2})
		dashboard.wantFrames()
	})

	t.Run("an empty roster counts zero open and zero total", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"open": 0, "total": 0})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetTaskApiTasksTaskIdGet(t *testing.T) {
	t.Run("the id in the path selects that task and it comes back in full", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Second","executor_member_id":"kip","description":"desc"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-2", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Second",
			"dedupe_key":           "",
			"inputs":               map[string]any{},
			"description":          "desc",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"handover_note":        "",
			"handover_note_ts":     0,
			"handover_note_by":     "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"steps":                []any{},
			"detail_level":         "summary",
			"notes_included":       false,
			"progress_done":        0,
			"progress_total":       0,
			"closeout_reported":    false,
			"artifact_count":       0,
			"handoff":              "",
			"handoff_note":         "",
			"handoff_task_id":      "",
			"blocking":             []any{},
			"frozen_by":            "",
			"forced_done_by":       "",
			"forced_done_reason":   "",
		})
		dashboard.wantFrames()
	})

	t.Run("the submitted plan comes back as the task's step rows", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Planned","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.steps", data["steps"], []any{map[string]any{
			"id":                apiAnyString,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Draft",
			"dod":               "a draft exists",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
		apiWantValue(t, "body.progress_total", data["progress_total"], 1)
	})

	t.Run("a task another task blocks on names its waiter under blocking", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.blocking", data["blocking"], []any{map[string]any{
			"id":      "T-2",
			"task_no": "T-2",
			"title":   "Waiter",
			"status":  "not_started",
		}})
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"T","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestCallerMayTerminateTask(t *testing.T) {
	t.Run("owner scope and admin capability may terminate a task they do not execute", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		for _, token := range []string{owner, apiTestAgentToken(t, api, "mira", "")} {
			taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
				ok, reason := api.callerMayTerminateTask(r, *task)
				if !ok || reason != "" {
					t.Fatalf("%s: got (%v, %q)", currentActor(r), ok, reason)
				}
			})
		}
	})

	t.Run("the staff executor may terminate its own task", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			ok, reason := api.callerMayTerminateTask(r, *task)
			if !ok || reason != "" {
				t.Fatalf("got (%v, %q)", ok, reason)
			}
		})
	})

	t.Run("a plain agent that is not the executor is refused by the executor guard", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "kip", ""), func(r *http.Request) {
			ok, reason := api.callerMayTerminateTask(r, *task)
			if ok || reason != executorGuardRefusal {
				t.Fatalf("got (%v, %q)", ok, reason)
			}
		})
	})

	t.Run("an outsource worker is refused its own task with the sentence that names the way out", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		bound := *task
		bound.ExecutorID = "ow-abc123"
		bound.ExecutorKind = TaskExecutorOutsource
		if err := d.PutTask(bound); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		taskTestUnderCaller(t, api, d, apiTestAgentToken(t, api, "ow-abc123", ""), func(r *http.Request) {
			ok, reason := api.callerMayTerminateTask(r, bound)
			if ok || reason != "an outsource worker may not terminate its own task; "+
				"ask the owner or an admin agent" {
				t.Fatalf("got (%v, %q)", ok, reason)
			}
		})
	})

	t.Run("an executor whose roster row is gone is refused rather than waved through", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		token := apiTestAgentToken(t, api, "kip", "")
		if _, err := d.HardDeleteMember("kip"); err != nil {
			t.Fatalf("HardDeleteMember: %v", err)
		}
		taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
			ok, reason := api.callerMayTerminateTask(r, *task)
			if ok || reason != executorGuardRefusal {
				t.Fatalf("got (%v, %q)", ok, reason)
			}
		})
	})
}

func TestHandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost(t *testing.T) {
	t.Run("a task waiting in ready_for_done is terminated without finishing anything first", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		readyForDoneTask(t, api, h, owner, "T-1", "kip")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if data["status"] != TaskStatusTerminated {
			t.Fatalf("want terminated, got %v", data)
		}
	})

	t.Run("terminating an open task answers the write receipt, fans the task delta and the executor's close notice", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "terminated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "terminated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		noticeFrame := map[string]any{
			"seq":   3,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("terminating an already-closed task answers 409 naming the status it is in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/mark-terminated", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleSetTaskPriorityApiTasksTaskIdPriorityPost(t *testing.T) {
	t.Run("setting a priority answers the receipt and fans the task delta carrying it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "high", "frozen_by": "",
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("freezing records the actor that froze it and unfreezing clears that record", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", agent, `{"priority":"frozen"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "frozen", "frozen_by": "kip",
		})

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "low", "frozen_by": "",
		})
	})

	t.Run("a priority outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"urgent"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "priority must be one of high, mid, low, frozen")
		dashboard.wantFrames()
	})

	t.Run("a body with no priority key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: priority")
	})

	t.Run("a body carrying a key the route does not declare answers 422 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner,
			`{"priority":"high","urgency":"very"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "urgency"`)
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", other, `{"priority":"high"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", machine, `{"priority":"high"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/priority", owner, `{"priority":"high"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", "", `{"priority":"high"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandlePostTaskMessageApiTasksTaskIdMessagePost(t *testing.T) {
	t.Run("a message on the task card answers the post receipt naming the executor and fans the chat delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{"body":"any update?"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "kip",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
		messageID, _ := data["id"].(string)

		chatFrame := map[string]any{
			"seq":   2,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + messageID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": messageID, "from": "owner", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(chatFrame)
		executor.wantFrames(chatFrame)
		bystander.wantFrames()
	})

	t.Run("a message carrying an uploaded attachment answers the receipt naming that blob", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"report.txt","data_b64":"aGVsbG8="}`)
		attachmentID, _ := uploaded["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"see this","attachments":[{"id":"`+attachmentID+`"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": apiAnyString,
			"to": "kip",
			"ts": apiAnyNumber,
			"attachments": []any{map[string]any{
				"id":       attachmentID,
				"url":      "/api/chat/attachment/" + attachmentID,
				"filename": "",
				"mime":     "application/octet-stream",
				"is_image": false,
			}},
		})
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("a message carrying neither text nor an attachment answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{"body":"  "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "message must carry text or an attachment")
		dashboard.wantFrames()
	})

	t.Run("more than ten attachments answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		items := strings.TrimSuffix(strings.Repeat(`{"id":"att-nope"},`, 11), ",")
		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"hi","attachments":[`+items+`]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a message may carry at most 10 attachments")
	})

	t.Run("an attachment id the store does not carry is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"hi","attachments":[{"id":"att-nope"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment 'att-nope' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", agent, `{"body":"hi"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/message", owner, `{"body":"hi"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", "", `{"body":"hi"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReassignTaskApiTasksTaskIdReassignPost(t *testing.T) {
	t.Run("handing a task to another member answers the receipt under the reassigning lock and pairs both sides in chat", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		predecessor := apiTestListen(t, api, "kip")
		successor := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"},"note":"context is on the ticket"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "reassigning",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		predecessorNotice := map[string]any{
			"seq":   2,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		successorNotice := map[string]any{
			"seq":   3,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "mira"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		newAudienceDelta := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		oldAudienceDelta := map[string]any{
			"seq":   5,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   5,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(predecessorNotice, successorNotice, newAudienceDelta, oldAudienceDelta)
		predecessor.wantFrames(predecessorNotice, oldAudienceDelta)
		successor.wantFrames(successorNotice, newAudienceDelta)
	})

	t.Run("handing a planned task to the outsource lane resets its live steps and leaves it unassigned under the lock", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","runtime":"claude","effort":"high"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "",
			"executor_kind":          "outsource",
			"lock":                   "reassigning",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         1,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		_, handedOver := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "body.steps", handedOver["steps"], []any{map[string]any{
			"id":                stepID,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Draft",
			"dod":               "a draft exists",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
		apiWantValue(t, "body.reassigned_from", handedOver["reassigned_from"], "kip")
		apiWantValue(t, "body.reassigned_from_kind", handedOver["reassigned_from_kind"], "staff")
	})

	t.Run("an outsource target naming a machine nothing carries answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","machine":"m-nosuchhost"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-nosuchhost' not found")
	})

	t.Run("a target naming the task's current executor answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"kip"}}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "member 'kip' is already the task's executor")
		dashboard.wantFrames()
	})

	t.Run("a staff target with no member_id answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.member_id is required for kind 'staff'")
	})

	t.Run("a target member nobody carries answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"nobody"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target member 'nobody' is not an active roster member")
	})

	t.Run("a target that is a machine answers 400 saying machines never execute tasks", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"m-server-self"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"target member 'm-server-self' is a machine (warden) — machines never execute tasks")
	})

	t.Run("the pre-rename executor kind answers 400 saying it was renamed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"member","member_id":"mira"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`target.kind: task executor kind "member" was renamed to "staff" (T-101); `+
				`the closed set is {"staff", "outsource"}`)
	})

	t.Run("an outsource target with an effort outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","effort":"turbo"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.effort must be one of low, medium, high, xhigh, max")
	})

	t.Run("an outsource target with a runtime outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","runtime":"gemini"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.runtime must be 'claude' or 'codex'")
	})

	t.Run("a plain agent handing its own task to another member answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", agent,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", other,
			`{"target":{"kind":"staff","member_id":"kip"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("a body with no target key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: target")
	})

	t.Run("a handover note over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"},"note":"`+strings.Repeat("x", 4001)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "handover note is 4001 chars, over the 4000-char limit")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", machine,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", "",
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleClaimTaskApiTasksTaskIdClaimPost(t *testing.T) {
	t.Run("the successor claiming a handed-over task clears the lock and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		successorToken := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		successor := apiTestListen(t, api, "mira")
		predecessor := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", successorToken, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   6,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   6,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		}
		dashboard.wantFrames(taskFrame)
		successor.wantFrames(taskFrame)
		predecessor.wantFrames()
	})

	t.Run("claiming a task handed over from an outsource worker clears the lock and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource"}}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		successorToken := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", successorToken, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Contracted out",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
	})

	t.Run("a task under no handover lock answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is not awaiting takeover (no reassigning lock)")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/claim", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestExecutorLabel(t *testing.T) {
	t.Run("a staff executor is labelled by its display name", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.executorLabel(TaskExecutorStaff, "kip"); got != "Kip" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("an outsource executor is labelled by its codename under the 外包 prefix", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if got := api.executorLabel(TaskExecutorOutsource, "ow-abc123"); got != "外包 Contractor" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a member with no display name falls back to its own id", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		if err := d.PutMember(Member{
			ID: "rex", Kind: KindStaff, RoleKey: "engineer", RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		if got := api.executorLabel(TaskExecutorStaff, "rex"); got != "rex" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("an id on no roster at all is its own label", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.executorLabel(TaskExecutorStaff, "ghost"); got != "ghost" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a worker read as staff answers its bare codename, without the 外包 prefix", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)
		if got := api.executorLabel(TaskExecutorStaff, "ow-abc123"); got != "Contractor" {
			t.Fatalf("staff kind over a worker id: got %q", got)
		}
	})

	t.Run("a staff member read as outsource has no worker row and falls back to its id", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.executorLabel(TaskExecutorOutsource, "kip"); got != "kip" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a kind outside the two the label knows falls back to the id", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.executorLabel("member", "kip"); got != "kip" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("no executor at all is labelled with nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		if got := api.executorLabel(TaskExecutorStaff, ""); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestPostTaskChat(t *testing.T) {
	t.Run("the durable row carries the task linkage in its meta and the delta reaches both parties", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		api.postTaskChat(*task, wireSystemSender, "kip", "請把交接資訊寫上去", "owner",
			map[string]any{"closed_by": "owner"})

		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("want exactly one durable row, got %v", rows)
		}
		msg := rows[0]
		if msg.Sender != wireSystemSender || msg.Recipient != "kip" ||
			msg.Body != "請把交接資訊寫上去" || msg.TS <= 0 {
			t.Fatalf("row: %#v", msg)
		}
		if !strings.HasPrefix(msg.ID, "c-") {
			t.Fatalf("id: %q", msg.ID)
		}
		wantMeta := map[string]any{
			"task_id": "T-1", "task_title": "Ship it", "task_type": "", "closed_by": "owner",
		}
		if !reflect.DeepEqual(msg.Meta, wantMeta) {
			t.Fatalf("meta: want %#v, got %#v", wantMeta, msg.Meta)
		}

		chatFrame := map[string]any{
			"seq":   2,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + msg.ID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": msg.ID, "from": wireSystemSender, "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(chatFrame)
		recipient.wantFrames(chatFrame)
		bystander.wantFrames()
	})

	t.Run("with no extra meta the row carries the task linkage alone", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		api.postTaskChat(*task, "mira", "kip", "你被解除阻擋了", "mira", nil)

		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("want exactly one durable row, got %v", rows)
		}
		wantMeta := map[string]any{"task_id": "T-1", "task_title": "Ship it", "task_type": ""}
		if !reflect.DeepEqual(rows[0].Meta, wantMeta) {
			t.Fatalf("meta: want %#v, got %#v", wantMeta, rows[0].Meta)
		}
	})
}

func TestHandleCreateTaskApiTasksPost(t *testing.T) {
	t.Run("an ad-hoc task naming its executor answers the create receipt and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
		})

		taskFrame := map[string]any{
			"seq":   1,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an explicit priority rides onto the created task's delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","priority":"high"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("the created task's ticket numbers run in the order they were opened", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, first := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"First","executor_member_id":"kip"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, first)
		}
		apiWantBody(t, first, map[string]any{
			"task_id": "T-1", "executor_kind": "staff", "executor_id": "kip", "deduped": false,
		})
		status, second := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Second","executor_member_id":"kip"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, second)
		}
		apiWantBody(t, second, map[string]any{
			"task_id": "T-2", "executor_kind": "staff", "executor_id": "kip", "deduped": false,
		})
	})

	t.Run("a typed task the manual assigns takes its executor and dedupe identity from that manual", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true},{"name":"note"}]}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report","type_key":"daily_report","inputs":{"day":"mon","stray":"x"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
			"warnings": []any{
				"unknown input field 'stray' (not defined in manual 'daily_report')",
			},
		})

		status, again := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report again","type_key":"daily_report","inputs":{"day":"mon"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, again)
		}
		apiWantBody(t, again, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       true,
			"title":         "Monday report",
			"status":        "not_started",
		})
	})

	t.Run("a typed task missing a required input answers 400 naming the field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true}]}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report","type_key":"daily_report","inputs":{}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "required input 'day' is missing")
	})

	t.Run("a typed task the manual assigns to somebody else answers 403 naming that member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true}]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Monday report","type_key":"daily_report","inputs":{"day":"mon"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"a typed task assigned to member 'kip' may only be created by that member")
	})

	t.Run("an explicit outsource dispatch lands the task unassigned on the outsource lane", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","runtime":"claude","effort":"high"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "outsource",
			"executor_id":   "",
			"deduped":       false,
		})
		dashboard.wantFrames(
			map[string]any{
				"seq":   1,
				"topic": "task",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "task",
					"key":     "owner::T-1",
					"epoch":   1,
					"deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   2,
				"topic": "outsource_worker",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "outsource_worker",
					"key":     apiAnyString,
					"epoch":   2,
					"deleted": false,
					"payload": map[string]any{"id": apiAnyString, "codename": "X-1", "status": "assigned"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
			map[string]any{
				"seq":   3,
				"topic": "task",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "task",
					"key":     "owner::T-1",
					"epoch":   3,
					"deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
			map[string]any{
				"seq":   4,
				"topic": "outsource_worker",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "outsource_worker",
					"key":     apiAnyString,
					"epoch":   4,
					"deleted": false,
					"payload": map[string]any{"id": apiAnyString, "codename": "X-1", "status": "assigned"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
		)
	})

	t.Run("an outsource dispatch with a runtime outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","runtime":"gemini"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.runtime must be 'claude' or 'codex'")
	})

	t.Run("an outsource dispatch with an effort outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","effort":"turbo"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.effort must be one of low, medium, high, xhigh, max")
	})

	t.Run("an outsource dispatch naming a machine nothing carries answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","machine":"m-nosuchhost"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-nosuchhost' not found")
	})

	t.Run("a body with no title key answers 422 naming the missing field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: title")
		dashboard.wantFrames()
	})

	t.Run("a blank title answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"   "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "title must not be blank")
	})

	t.Run("an ad-hoc task with no executor answers 400 saying the field is required", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "executor_member_id is required for an ad-hoc task")
	})

	t.Run("a priority outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","priority":"urgent"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "priority must be one of high, mid, low, frozen")
	})

	t.Run("the pre-rename executor kind answers 400 saying it was renamed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","target":{"kind":"member"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`target.kind: task executor kind "member" was renamed to "staff" (T-101); `+
				`the closed set is {"staff", "outsource"}`)
	})

	t.Run("a type_key no manual carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","type_key":"daily_report","executor_member_id":"kip"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'daily_report' not found")
	})

	t.Run("a plain agent naming somebody else as executor answers 403", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Ship it","executor_member_id":"mira"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"an ad-hoc task may only name yourself as executor (or be dispatched to an outsource worker)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", machine,
			`{"title":"Ship it","executor_member_id":"kip"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", "",
			`{"title":"Ship it","executor_member_id":"kip"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleSubmitTaskPlanApiTasksTaskIdPlanPost(t *testing.T) {
	t.Run("a fresh plan answers the plan receipt and fans the re-derived task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 2, "progress_done": 0, "progress_total": 2,
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a task in ready_for_done is still plannable, and the new step reopens it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows"}`)
		_, ready := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if ready["status"] != "ready_for_done" {
			t.Fatalf("setup: want ready_for_done, got %v", ready["status"])
		}

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		_, replanned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if replanned["status"] != "in_progress" {
			t.Fatalf("the fresh step must re-derive the status, got %v", replanned["status"])
		}
	})

	t.Run("a replan of a task closed with mark_task_done answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows"}`)
		apiMarkDone(t, h, "T-1", agent)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
	})

	t.Run("a replan over an unfinished plan replaces the pending steps wholesale", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Rewrite","dod":"the rewrite lands"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 1, "progress_done": 0, "progress_total": 1,
		})

		_, replanned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "body.steps", replanned["steps"], []any{map[string]any{
			"id":                apiAnyString,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Rewrite",
			"dod":               "the rewrite lands",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
	})

	t.Run("a replan keeps a finished step in place and appends only the steps it did not already carry", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks", agent, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", agent, "")
		firstStepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"done"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Rewrite","dod":"the rewrite lands"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 2, "progress_done": 1, "progress_total": 2,
		})

		_, replanned := apiJSON(t, h, "GET", "/api/tasks/T-1", agent, "")
		apiWantValue(t, "body.steps", replanned["steps"], []any{
			map[string]any{
				"id":                firstStepID,
				"task_id":           "T-1",
				"order_idx":         0,
				"name":              "Draft",
				"dod":               "a draft exists",
				"status":            "done",
				"parallel_group":    "",
				"is_gate":           false,
				"reply_card_id":     "",
				"reply_card_status": "",
				"waiting_reason":    "",
				"note_size_chars":   0,
				"note_cap_chars":    10000,
				"started_ts":        apiAnyNumber,
				"finished_ts":       apiAnyNumber,
			},
			map[string]any{
				"id":                apiAnyString,
				"task_id":           "T-1",
				"order_idx":         1,
				"name":              "Rewrite",
				"dod":               "the rewrite lands",
				"status":            "pending",
				"parallel_group":    "",
				"is_gate":           false,
				"reply_card_id":     "",
				"reply_card_status": "",
				"waiting_reason":    "",
				"note_size_chars":   0,
				"note_cap_chars":    10000,
				"started_ts":        0,
				"finished_ts":       0,
			},
		})
	})

	t.Run("a parallel group holding a single lane answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists","parallel_group":"g1"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"parallel_group 'g1' holds only one step — running in parallel takes at least two; "+
				"drop the parallel_group to keep the step sequential")
	})

	t.Run("a replan that would close a task somebody else opened is refused until the ball is declared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		firstStepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"done"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"task 'T-1' was created by 'owner' but executed by 'kip': this plan leaves EVERY step "+
				"done, which FINISHES the task — it lands in ready_for_done, one mark_task_done "+
				"away from a close that can never be undone. A plan "+
				"carries no handoff declaration, so hand the ball over first, one of two ways: "+
				"(1) keep ONE unfinished step in this plan, then declare the handover on the "+
				"update_step_status report that finishes it (handoff='return_to_creator' | "+
				"'follow_up' + handoff_task_id | 'none' + handoff_note) — this route always works, "+
				"and the server adds the dependency edge itself; or (2) create the successor task "+
				"(create_task) and point its blocked_by at this task (set_task_deps) — this gate "+
				"then stands aside by itself, and closing this task releases the successor. NOTE: "+
				"set_task_deps requires you to be the successor's executor (or an owner), so this "+
				"route only works when the successor is assigned to you; otherwise it answers 403 "+
				"and you want route (1).")
		dashboard.wantFrames()
	})

	t.Run("a plan with no steps at all answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner, `{"steps":[]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a plan must have at least one step")
		dashboard.wantFrames()
	})

	t.Run("a step with a blank name answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"  ","dod":"a draft exists"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step name must not be blank")
	})

	t.Run("a step with a blank definition of done answers 400 naming the step", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"Draft","dod":"   "}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step 'Draft' must have a non-empty definition of done")
	})

	t.Run("a body with no steps key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: steps")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", other,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", machine,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/plan", owner,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", "",
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleMarkTaskDuplicatedApiTasksTaskIdMarkDuplicatedPost(t *testing.T) {
	t.Run("a task waiting in ready_for_done can still turn out to be a copy", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The copy","executor_member_id":"kip"}`)
		readyForDoneTask(t, api, h, owner, "T-2", "kip")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", owner,
			`{"duplicate_of":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if data["status"] != TaskStatusDuplicated || data["duplicate_of"] != "T-1" {
			t.Fatalf("want duplicated pointing at T-1, got %v", data)
		}
	})

	t.Run("marking a task duplicated closes it pointing at the original, fans the delta and the executor's close notice", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-2",
			"title":                  "The twin",
			"status":                 "duplicated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "T-1",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-2",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-2", "priority": "mid", "status": "duplicated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		noticeFrame := map[string]any{
			"seq":   4,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("pointing a task at itself answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a task cannot be marked a duplicate of itself")
		dashboard.wantFrames()
	})

	t.Run("pointing at a task that is itself a duplicate answers 409 naming the final original", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The third","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-3/mark-duplicated", owner, `{"duplicate_of":"T-2"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"duplicate_of task 'T-2' is itself a duplicate; point at the final original it duplicates (T-1)")
	})

	t.Run("a task already cited as an original cannot itself be marked duplicated", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The third","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-duplicated", owner, `{"duplicate_of":"T-3"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is already the original of another duplicate; it cannot itself be marked duplicated")
	})

	t.Run("pointing at an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-duplicated", owner, `{"duplicate_of":"T-999"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "duplicate_of task 'T-999' not found")
	})

	t.Run("a blank duplicate_of answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-duplicated", owner, `{"duplicate_of":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "duplicate_of must not be blank")
	})

	t.Run("a body with no duplicate_of key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-duplicated", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: duplicate_of")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", other, `{"duplicate_of":"T-1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-2' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", machine, `{"duplicate_of":"T-1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/mark-duplicated", owner, `{"duplicate_of":"T-1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/mark-duplicated", "", `{"duplicate_of":"T-1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(t *testing.T) {
	t.Run("reporting a step in progress answers the step receipt and fans the re-derived task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"in_progress"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "in_progress",
			"waiting_reason": "",
			"task_status":    "in_progress",
			"closed_ts":      nil,
			"progress_done":  0,
			"progress_total": 2,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "in_progress"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("entering waiting_external records the reason on both the step and the task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_external","waiting_reason":"the vendor has not answered"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "waiting_external",
			"waiting_reason": "the vendor has not answered",
			"task_status":    "waiting_external",
			"closed_ts":      nil,
			"progress_done":  0,
			"progress_total": 1,
		})
	})

	t.Run("entering waiting_external with no reason answers 422", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_external"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"waiting_reason is required when entering waiting_external")
		dashboard.wantFrames()
	})

	t.Run("the report that finishes the last step lands ready_for_done, and mark_task_done is what closes it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows this"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "done",
			"waiting_reason": "",
			"task_status":    "ready_for_done",
			"closed_ts":      nil,
			"progress_done":  1,
			"progress_total": 1,
		})

		readyFrame := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "ready_for_done"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		readyNoticeFrame := map[string]any{
			"seq":   5,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   5,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(readyFrame, readyNoticeFrame)
		executor.wantFrames(readyFrame, readyNoticeFrame)
		bystander.wantFrames()

		rows, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		wantNotice := api.taskNoticeText(docKindTaskReadyForDone,
			map[string]string{"task_no": "T-1", "visit_no": "1"})
		if len(rows) != 1 || rows[0].Sender != wireSystemSender ||
			rows[0].Recipient != "kip" || rows[0].Body != wantNotice {
			t.Fatalf("ready-for-done notice = %#v, want the durable 〈任務可結案〉 to kip", rows)
		}

		apiMarkDone(t, h, "T-1", agent)

		doneFrame := map[string]any{
			"seq":   6,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   6,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "done"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		noticeFrame := map[string]any{
			"seq":   7,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   7,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(doneFrame, noticeFrame)
		executor.wantFrames(doneFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("the report that would close a task somebody else opened is refused until the ball is declared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"task 'T-1' was created by 'owner' but executed by 'kip': this report would FINISH it — "+
				"every step done — and it is then one call (mark_task_done) from a close that can "+
				"never be undone or replanned. Say where the ball goes, in THIS same "+
				"update_step_status call, with one of: "+
				"handoff='return_to_creator' (recorded on this task and nothing else — no task is "+
				"opened and nobody is notified); handoff='follow_up' + handoff_task_id='<the "+
				"successor task you already created>' (the server attaches this task to it as a "+
				"dependency, and closing this one releases it); handoff='none' + "+
				"handoff_note='<why nothing follows>' (an explicit end of the line, recorded).")
		dashboard.wantFrames()
	})

	t.Run("a transition the step machine does not allow answers 409 naming both ends", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "illegal step transition 'pending' -> 'done'")
	})

	t.Run("reporting waiting_owner answers 400 because only a reply card opens that hold", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_owner"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"waiting_owner is not an agent-reportable status; a step enters it only "+
				"by opening a reply card (create_reply_card with linked_task)")
	})

	t.Run("reporting superseded answers 400 because only a replan freezes a step", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"superseded"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"superseded is not agent-reportable; the server freezes a replaced "+
				"step itself when a new plan is submitted (submit_plan)")
	})

	t.Run("a status outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"halfway"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"status must be one of pending, in_progress, waiting_owner, waiting_external, done, superseded")
	})

	t.Run("a step id this task does not carry answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step 'ts-whatever' not found")
	})

	t.Run("a body with no status key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: status")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", other,
			`{"status":"in_progress"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", machine,
			`{"status":"in_progress"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", "",
			`{"status":"in_progress"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestArmStepWithCard(t *testing.T) {
	t.Run("the armed step enters waiting_owner carrying the card, and the task follows it", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")

		step := steps[0]
		if err := api.armStepWithCard(task, &step, "rc-abc123def456", "kip"); err != nil {
			t.Fatalf("armStepWithCard: %v", err)
		}

		if step.Status != "waiting_owner" || step.ReplyCardID != "rc-abc123def456" || step.StartedTS <= 0 {
			t.Fatalf("returned step: %#v", step)
		}
		if task.Status != "waiting_owner" {
			t.Fatalf("returned task status: %q", task.Status)
		}
		stored, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if stored[0].Status != "waiting_owner" || stored[0].ReplyCardID != "rc-abc123def456" ||
			stored[0].StartedTS != step.StartedTS {
			t.Fatalf("stored armed step: %#v", stored[0])
		}
		if stored[1].Status != "pending" || stored[1].ReplyCardID != "" {
			t.Fatalf("the sibling step must be untouched: %#v", stored[1])
		}
		reread, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if reread.Status != "waiting_owner" || reread.ClosedTS != 0 {
			t.Fatalf("stored task: %#v", *reread)
		}

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "waiting_owner"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
	})

	t.Run("a step already under way keeps its first touch and only the card pointer moves", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"in_progress"}`)
		started, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if started[0].StartedTS <= 0 {
			t.Fatalf("want a stamped first touch, got %#v", started[0])
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		step := started[0]
		if err := api.armStepWithCard(task, &step, "rc-abc123def456", "kip"); err != nil {
			t.Fatalf("armStepWithCard: %v", err)
		}

		if step.StartedTS != started[0].StartedTS {
			t.Fatalf("started_ts moved: was %v, now %v", started[0].StartedTS, step.StartedTS)
		}
		stored, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if stored[0].Status != "waiting_owner" || stored[0].ReplyCardID != "rc-abc123def456" {
			t.Fatalf("stored step: %#v", stored[0])
		}
	})

	t.Run("a step inside a parallel group flips the whole task too", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists","parallel_group":"g1"},`+
				`{"name":"Review","dod":"a review is signed off","parallel_group":"g1"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if steps[0].ParallelGroup != "g1" {
			t.Fatalf("want a lane step, got %#v", steps[0])
		}
		task, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}

		step := steps[0]
		if err := api.armStepWithCard(task, &step, "rc-abc123def456", "kip"); err != nil {
			t.Fatalf("armStepWithCard: %v", err)
		}

		reread, err := api.resolveTask("T-1")
		if err != nil {
			t.Fatalf("resolveTask: %v", err)
		}
		if reread.Status != "waiting_owner" {
			t.Fatalf("stored task status: %q", reread.Status)
		}
	})
}

func TestHandleSetTaskDepsApiTasksTaskIdDepsPost(t *testing.T) {
	t.Run("replacing the blocking list answers the write receipt carrying it and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-2",
			"title":                  "Waiter",
			"status":                 "not_started",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{"T-1"},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-2",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-2", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an empty list clears the blocking edges a task already carried", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":[]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.deps", data["deps"], []any{})
	})

	t.Run("a repeated blocker is stored once", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner,
			`{"blocked_by":["T-1","T-1"]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.deps", data["deps"], []any{"T-1"})
	})

	t.Run("a task naming itself as a blocker answers 422", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-1"]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a task cannot block on itself")
		dashboard.wantFrames()
	})

	t.Run("a blocker no task carries answers 422 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-999"]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "unknown blocking task 'T-999'")
	})

	t.Run("a body with no blocked_by key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: blocked_by")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", other, `{"blocked_by":[]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":[]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", machine, `{"blocked_by":[]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/deps", owner, `{"blocked_by":[]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", "", `{"blocked_by":[]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(t *testing.T) {
	t.Run("the first close-out report stamps the moment, answers the receipt and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":           "T-1",
			"task_status":       "terminated",
			"closeout_reported": true,
			"closeout_ts":       apiAnyNumber,
		})

		taskFrame := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "terminated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a repeat report answers the original stamp again and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		_, first := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		firstStamp, _ := first["closeout_ts"].(float64)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":           "T-1",
			"task_status":       "terminated",
			"closeout_reported": true,
			"closeout_ts":       firstStamp,
		})
		dashboard.wantFrames()
	})

	t.Run("a task that is still open answers 409 naming the status it is in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is still open (not_started) — close-out is reported after the task ends")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/closeout", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestArtifactTextOrError(t *testing.T) {
	t.Run("both fields trim and pass, writing nothing to the response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rawName, rawDescription := "  the report  ", "  what it says  "
		name, description, ok := artifactTextOrError(rec, &rawName, &rawDescription)
		if !ok || name != "the report" || description != "what it says" {
			t.Fatalf("got (%q, %q, %v)", name, description, ok)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("want an untouched response, got %q", rec.Body.String())
		}
	})

	t.Run("absent pointers read as not sent and yield the empty pair", func(t *testing.T) {
		rec := httptest.NewRecorder()
		name, description, ok := artifactTextOrError(rec, nil, nil)
		if !ok || name != "" || description != "" {
			t.Fatalf("got (%q, %q, %v)", name, description, ok)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("want an untouched response, got %q", rec.Body.String())
		}
	})

	t.Run("a name one rune over the cap writes the 400 itself and returns the empty pair", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rawName, rawDescription := strings.Repeat("a", 49), "kept"
		name, description, ok := artifactTextOrError(rec, &rawName, &rawDescription)
		if ok || name != "" || description != "" {
			t.Fatalf("got (%q, %q, %v)", name, description, ok)
		}
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantError(t, body, "validation_error", "artifact name is 49 chars, over the 48-char limit")
	})

	t.Run("a name exactly at the cap passes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rawName := strings.Repeat("a", 48)
		name, _, ok := artifactTextOrError(rec, &rawName, nil)
		if !ok || name != rawName {
			t.Fatalf("got (%q, %v)", name, ok)
		}
	})

	t.Run("a description one rune over the cap writes its own 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rawName, rawDescription := "fine", strings.Repeat("字", 257)
		name, description, ok := artifactTextOrError(rec, &rawName, &rawDescription)
		if ok || name != "" || description != "" {
			t.Fatalf("got (%q, %q, %v)", name, description, ok)
		}
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantError(t, body, "validation_error", "artifact description is 257 chars, over the 256-char limit")
	})
}

func TestMintLinkTargetBlob(t *testing.T) {
	t.Run("the minted blob carries the url as its bytes under the uri-list mime and the id points at it", func(t *testing.T) {
		id, att := mintLinkTargetBlob("https://example.com/pr/123")
		if att == nil {
			t.Fatal("want a blob")
		}
		if id != att.ID {
			t.Fatalf("the returned id %q does not address the blob %q", id, att.ID)
		}
		if !strings.HasPrefix(att.ID, "att-") || len(att.ID) != len("att-")+12 {
			t.Fatalf("blob id: got %q", att.ID)
		}
		if att.Mime != "text/uri-list" {
			t.Fatalf("mime: got %q", att.Mime)
		}
		if string(att.Data) != "https://example.com/pr/123" {
			t.Fatalf("bytes: got %q", att.Data)
		}
	})

	t.Run("two mints of the same url are two distinct blobs", func(t *testing.T) {
		first, _ := mintLinkTargetBlob("https://example.com/pr/123")
		second, _ := mintLinkTargetBlob("https://example.com/pr/123")
		if first == second {
			t.Fatalf("want two ids, got %q twice", first)
		}
	})
}

func TestHandleAddTaskArtifactApiTasksTaskIdArtifactPost(t *testing.T) {
	t.Run("pinning a link answers the artifact receipt with the new set size and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 1,
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("pinning a second deliverable answers the grown set size", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #124","url":"https://example.com/pr/124"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 2,
		})
	})

	t.Run("pinning an uploaded file answers the artifact receipt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"report.txt","data_b64":"aGVsbG8="}`)
		attachmentID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+attachmentID+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 1,
		})
	})

	t.Run("an attachment_id reserved for a member avatar is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"image","name":"the face","attachment_id":"ava-kip"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'ava-kip' is reserved for a member avatar")
	})

	t.Run("a kind outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"video","name":"PR #123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "kind must be one of file, image, link")
		dashboard.wantFrames()
	})

	t.Run("a blank name answers 400 asking for a display name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"  ","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name is required: give this deliverable a short display name")
	})

	t.Run("a name over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"`+strings.Repeat("x", 49)+`","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "artifact name is 49 chars, over the 48-char limit")
	})

	t.Run("a description over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","description":"`+strings.Repeat("x", 257)+
				`","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact description is 257 chars, over the 256-char limit")
	})

	t.Run("a link with no url answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is required for a link artifact")
	})

	t.Run("a link url outside the scheme whitelist answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"javascript:alert(1)"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url must start with https:// or http://")
	})

	t.Run("a link url over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/`+strings.Repeat("x", 2029)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is 2049 chars, over the 2048-char limit")
	})

	t.Run("a file with no attachment_id answers 400 naming the kind", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"file","name":"the report"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment_id is required for a file artifact")
	})

	t.Run("an attachment_id the store does not carry is refused with the upload instruction", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"file","name":"the report","attachment_id":"att-nosuchblob"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'att-nosuchblob' not found (upload it first via POST /api/chat/attachments)")
	})

	t.Run("a body with no kind key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: kind")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", other,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", machine,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", "",
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(t *testing.T) {
	t.Run("un-pinning a deliverable answers the shrunk set size and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 0,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-nosucharttifact", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
		dashboard.wantFrames()
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-2/artifact/"+artifactID, agent, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-999/artifact/ta-whatever", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestTaskFrozenDeliverablesRefusal(t *testing.T) {
	t.Run("the sentence names the task and the terminal status that froze it", func(t *testing.T) {
		got := taskFrozenDeliverablesRefusal(Task{ID: "T-7", Status: "done"})
		if got != "task 'T-7' is closed (done) — its deliverables are frozen" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("a terminated task is refused with the same sentence carrying its own status", func(t *testing.T) {
		got := taskFrozenDeliverablesRefusal(Task{ID: "T-9", Status: "terminated"})
		if got != "task 'T-9' is closed (terminated) — its deliverables are frozen" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestArtifactOnTask(t *testing.T) {
	pinned := func(t *testing.T) (*apiServer, http.Handler, string, string) {
		t.Helper()
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, artifact := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := artifact["artifact_id"].(string)
		return api, h, owner, artifactID
	}

	t.Run("a task id nothing carries answers 404 before anything else is looked at", func(t *testing.T) {
		_, h, owner, artifactID := pinned(t)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-999/artifact/"+artifactID, owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a caller who is neither the executor nor admin is refused before the task's state is probed", func(t *testing.T) {
		api, h, owner, artifactID := pinned(t)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		bystander := apiTestAgentToken(t, api, "mira", "")
		if _, err := api.dal.HardDeleteMember("mira"); err != nil {
			t.Fatalf("HardDeleteMember: %v", err)
		}

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, bystander, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", executorGuardRefusal)
	})

	t.Run("a closed task answers the freeze even for the owner and even for an artifact id it never carried", func(t *testing.T) {
		_, h, owner, _ := pinned(t)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-nosuchthing", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, owner, _ := pinned(t)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-nosuchthing", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosuchthing' not found")
	})

	t.Run("an artifact pinned to another task answers 400 naming both", func(t *testing.T) {
		_, h, owner, artifactID := pinned(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Another","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-2/artifact/"+artifactID, owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("the read face runs neither guard: a bystander reads a closed task's version history", func(t *testing.T) {
		api, h, owner, artifactID := pinned(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")
		bystander := apiTestAgentToken(t, api, "mira", "")
		if _, err := api.dal.HardDeleteMember("mira"); err != nil {
			t.Fatalf("HardDeleteMember: %v", err)
		}

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", bystander, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		rows, ok := versions.([]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("want the one retained version, got %v", versions)
		}
	})

	t.Run("a write caller receives the addressed task and artifact pair", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			rec := httptest.NewRecorder()
			task, artifact, ok := api.artifactOnTask(rec, r, "T-1", artifactID, artifactWrite)
			if !ok || task == nil || artifact == nil {
				t.Fatalf("artifactOnTask: ok=%v task=%#v artifact=%#v", ok, task, artifact)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
			}
			if task.ID != "T-1" || artifact.ID != artifactID || artifact.TaskID != "T-1" {
				t.Fatalf("resolved pair = task %#v, artifact %#v", *task, *artifact)
			}
		})
	})
}

func TestHandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost(t *testing.T) {
	t.Run("swapping a link's target answers the replace receipt with the grown version count and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an omitted name carries the pinned one forward and an explicit one replaces it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/125","name":"PR #125","description":"the follow-up"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{
			map[string]any{
				"id":            apiAnyNumber,
				"kind":          "link",
				"url":           "https://example.com/pr/124",
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "",
				"is_image":      false,
				"attachment_id": apiAnyString,
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
			},
			map[string]any{
				"id":            apiAnyNumber,
				"kind":          "link",
				"url":           "https://example.com/pr/123",
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "",
				"is_image":      false,
				"attachment_id": apiAnyString,
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
			},
		})
	})

	t.Run("swapping a file's blob answers the replace receipt and the previous blob is retained as a version", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, firstBlob := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		firstBlobID, _ := firstBlob["id"].(string)
		_, secondBlob := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v2.txt","data_b64":"d29ybGQ="}`)
		secondBlobID, _ := secondBlob["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+firstBlobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"`+secondBlobID+`","description":"the corrected numbers"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{map[string]any{
			"id":            apiAnyNumber,
			"kind":          "file",
			"url":           "/api/chat/attachment/" + firstBlobID,
			"name":          "the report",
			"description":   "",
			"filename":      "",
			"mime":          "application/octet-stream",
			"is_image":      false,
			"attachment_id": firstBlobID,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
		}})
	})

	t.Run("a url on a file artifact answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a file and the "+
				"replacement asks for a link — un-pin it and register a new artifact instead")
	})

	t.Run("a file replacement with no attachment_id answers 400 naming the kind", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment_id is required for a file artifact")
	})

	t.Run("a file replacement naming an attachment reserved for a member avatar is refused", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"ava-kip"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'ava-kip' is reserved for a member avatar")
	})

	t.Run("a file replacement naming an attachment the store does not carry is refused", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"att-nosuchblob"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'att-nosuchblob' not found (upload it first via POST /api/chat/attachments)")
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("a name over the character cap answers 400 naming both numbers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","name":"`+strings.Repeat("x", 49)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "artifact name is 49 chars, over the 48-char limit")
	})

	t.Run("a description over the character cap answers 400 naming both numbers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","description":"`+strings.Repeat("x", 257)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact description is 257 chars, over the 256-char limit")
	})

	t.Run("an explicit kind that differs from the pinned one answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"kind":"file","attachment_id":"att-whatever"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a link and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")
		dashboard.wantFrames()
	})

	t.Run("an attachment_id on a link answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"att-whatever"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a link and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")
	})

	t.Run("a blank name answers 400 telling the caller to omit it instead", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","name":"  "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name cannot be blank: omit it to keep the name this deliverable already has")
	})

	t.Run("a link replacement with no url answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is required for a link artifact")
	})

	t.Run("a link replacement url outside the scheme whitelist answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"ftp://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url must start with https:// or http://")
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-nosucharttifact/replace",
			owner, `{"url":"https://example.com/pr/124"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", other,
			`{"url":"https://example.com/pr/124"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", owner,
			`{"url":"https://example.com/pr/124"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", machine,
			`{"url":"https://example.com/pr/124"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/artifact/ta-whatever/replace", owner,
			`{"url":"https://example.com/pr/124"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", "",
			`{"url":"https://example.com/pr/124"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestWriteTaskArtifactReplaceReceipt(t *testing.T) {
	t.Run("the receipt counts the whole set while the version count belongs to the one artifact replaced", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		replacedID, _ := pinned["artifact_id"].(string)
		_, other := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #124","url":"https://example.com/pr/124"}`)
		untouchedID, _ := other["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+replacedID+"/replace", agent,
			`{"url":"https://example.com/pr/125"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+replacedID+"/replace", agent,
			`{"url":"https://example.com/pr/126"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    replacedID,
			"artifact_count": 2,
			"version_count":  3,
		})

		status, untouched := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+untouchedID+"/replace", agent,
			`{"url":"https://example.com/pr/127"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, untouched)
		}
		apiWantBody(t, untouched, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    untouchedID,
			"artifact_count": 2,
			"version_count":  2,
		})
	})

	t.Run("the raw-body replace door answers the same receipt shape", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			agent, "# v2\n")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v3.md&mime=text/markdown",
			agent, "# v3\n")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  3,
		})
	})

	t.Run("a direct receipt reports the retained history for the addressed artifact", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		rec := httptest.NewRecorder()
		api.writeTaskArtifactReplaceReceipt(rec, *task, artifactID)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantBody(t, body, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})
	})
}

func TestArtifactLinkURLRefusal(t *testing.T) {
	t.Run("both whitelisted schemes pass with no refusal", func(t *testing.T) {
		if got := artifactLinkURLRefusal("https://example.com/pr/123"); got != "" {
			t.Fatalf("https: got %q", got)
		}
		if got := artifactLinkURLRefusal("http://example.com/pr/123"); got != "" {
			t.Fatalf("http: got %q", got)
		}
	})

	t.Run("the scheme is matched case-insensitively", func(t *testing.T) {
		if got := artifactLinkURLRefusal("HTTPS://example.com/pr/123"); got != "" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("any other scheme is refused by the sentence naming the two that pass", func(t *testing.T) {
		for _, url := range []string{"javascript:alert(1)", "data:text/plain,hi", "ftp://example.com", "example.com", ""} {
			if got := artifactLinkURLRefusal(url); got != "url must start with https:// or http://" {
				t.Fatalf("%q: got %q", url, got)
			}
		}
	})

	t.Run("a url exactly at the cap passes and one rune over is refused by its measured length", func(t *testing.T) {
		prefix := "https://example.com/"
		atCap := prefix + strings.Repeat("a", 2048-len(prefix))
		if got := artifactLinkURLRefusal(atCap); got != "" {
			t.Fatalf("at the cap: got %q", got)
		}
		if got := artifactLinkURLRefusal(atCap + "a"); got != "url is 2049 chars, over the 2048-char limit" {
			t.Fatalf("one over: got %q", got)
		}
	})

	t.Run("the length is counted in runes, so a wide url under the cap is not refused for its bytes", func(t *testing.T) {
		wide := "https://example.com/" + strings.Repeat("字", 1000)
		if got := artifactLinkURLRefusal(wide); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestArtifactKindRefusal(t *testing.T) {
	t.Run("the sentence names the pinned kind, the asked kind and the way out", func(t *testing.T) {
		got := artifactKindRefusal("link", "file")
		if got != "artifact kind cannot change across versions: this artifact is a link "+
			"and the replacement asks for a file — un-pin it and register a new artifact instead" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("the other direction reads the same sentence with the two kinds swapped", func(t *testing.T) {
		got := artifactKindRefusal("image", "link")
		if got != "artifact kind cannot change across versions: this artifact is a image "+
			"and the replacement asks for a link — un-pin it and register a new artifact instead" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestHandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet(t *testing.T) {
	t.Run("the retained versions come back newest first with the target each one pointed at", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{map[string]any{
			"id":            apiAnyNumber,
			"kind":          "link",
			"url":           "https://example.com/pr/123",
			"name":          "PR #123",
			"description":   "the change itself",
			"filename":      "",
			"mime":          "",
			"is_image":      false,
			"attachment_id": apiAnyString,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
		}})
		dashboard.wantFrames()
	})

	t.Run("an artifact nobody has replaced yet has an empty history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{})
	})

	t.Run("a closed task still serves its deliverable's history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions []any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(versions) != 1 {
			t.Fatalf("want the one retained version, got %v", versions)
		}
		apiWantValue(t, "body[0].url", versions[0].(map[string]any)["url"], "https://example.com/pr/123")
	})

	t.Run("an agent that is not the task's executor still reads the history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		other := apiTestAgentToken(t, api, "mira", "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", other, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{})
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-nosucharttifact/history", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-2/artifact/"+artifactID+"/history", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-whatever/history", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999/artifact/ta-whatever/history", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-whatever/history", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

// readyForDoneTask plans one step, reports it done and hands back the executor's
// token — the state the four close actions are all judged from.
func readyForDoneTask(t *testing.T, api *apiServer, h http.Handler, owner, taskID, executor string) string {
	t.Helper()
	agent := apiTestAgentToken(t, api, executor, "")
	apiJSON(t, h, "POST", "/api/tasks/"+taskID+"/plan", agent,
		`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
	_, planned := apiJSON(t, h, "GET", "/api/tasks/"+taskID, owner, "")
	stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
	apiJSON(t, h, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
	apiJSON(t, h, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/status", agent,
		`{"status":"done","handoff":"none","handoff_note":"nothing follows"}`)
	_, view := apiJSON(t, h, "GET", "/api/tasks/"+taskID, owner, "")
	if view["status"] != TaskStatusReadyForDone {
		t.Fatalf("setup: want %s, got %v", TaskStatusReadyForDone, view["status"])
	}
	return agent
}

func TestCallerMayMarkTaskDone(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	task := Task{ID: "T-1", ExecutorID: "kip", Status: TaskStatusReadyForDone}

	t.Run("the task's own executor may close it", func(t *testing.T) {
		if !api.callerMayMarkTaskDone(taskReq(t, "POST", "/x", nil, "kip", "agent"), task) {
			t.Fatal("the executor must be able to close its own task")
		}
	})

	t.Run("an outsource executor may close its own task, unlike at mark_task_terminated", func(t *testing.T) {
		worker := Task{ID: "T-1", ExecutorID: "ow-1", Status: TaskStatusReadyForDone}
		if !api.callerMayMarkTaskDone(taskReq(t, "POST", "/x", nil, "ow-1", "agent"), worker) {
			t.Fatal("an outsource worker must be able to close the task it executes")
		}
	})

	t.Run("the owner and an admin agent are refused — their door is force_task_done", func(t *testing.T) {
		for _, id := range []struct{ sub, scope string }{
			{"owner", "owner"},
			{"m-admin", "agent"},
		} {
			if api.callerMayMarkTaskDone(taskReq(t, "POST", "/x", nil, id.sub, id.scope), task) {
				t.Fatalf("%s must not reach mark_task_done: admin capability closing a task "+
					"here is force_task_done with the reason and forced_done_by thrown away", id.sub)
			}
		}
	})

	t.Run("a foreign agent and an unbound task are both refused", func(t *testing.T) {
		if api.callerMayMarkTaskDone(taskReq(t, "POST", "/x", nil, "stranger", "agent"), task) {
			t.Fatal("a caller that is not the executor must be refused")
		}
		unbound := Task{ID: "T-2", ExecutorID: "", Status: TaskStatusReadyForDone}
		if api.callerMayMarkTaskDone(taskReq(t, "POST", "/x", nil, "", "agent"), unbound) {
			t.Fatal("a task with no executor has nobody who may close it")
		}
	})
}

func TestHandleMarkTaskDoneApiTasksTaskIdMarkDonePost(t *testing.T) {
	t.Run("a task in ready_for_done is closed by its executor, stamping closed_ts", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := readyForDoneTask(t, api, h, owner, "T-1", "kip")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if data["status"] != TaskStatusDone || data["closed_ts"] == nil {
			t.Fatalf("the receipt must report the closed task, got %v", data)
		}
		_, view := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if view["status"] != TaskStatusDone {
			t.Fatalf("the close must be durable, got %v", view["status"])
		}
		// Nothing forced it, so the record says so — that is what makes
		// forced_done_by readable as evidence rather than as noise.
		if view["forced_done_by"] != "" || view["forced_done_reason"] != "" {
			t.Fatalf("an ordinary close must leave the forced fields empty, got %v", view)
		}
	})

	t.Run("a task still in a work state is a 409 that names the status it is actually in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", agent, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		msg, _ := data["error"].(map[string]any)["message"].(string)
		if !strings.Contains(msg, "'not_started'") {
			t.Fatalf("the refusal must name the status the task is in, got %q", msg)
		}
	})

	t.Run("an already closed task is a 409 that names WHICH close happened", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := readyForDoneTask(t, api, h, owner, "T-1", "kip")
		apiMarkDone(t, h, "T-1", agent)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", agent, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
	})

	t.Run("a task that reached ready_for_done with the ball undeclared is refused at the handoff gate and stays open", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		// Reaching ready_for_done WITHOUT passing the step-report door is what
		// leaves handoff undeclared here — boot-reconcile is the route the
		// gate's own door list names.
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		steps[0].Status = StepStatusDone
		if err := d.PutTaskStep(steps[0]); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		if _, err := api.reconcileTaskStatusesOnBoot(); err != nil {
			t.Fatalf("reconcileTaskStatusesOnBoot: %v", err)
		}

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", agent, "")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		msg, _ := data["error"].(map[string]any)["message"].(string)
		if !strings.Contains(msg, "force_task_done") {
			t.Fatalf("the refusal must name the way out of a door that carries no "+
				"declaration field, got %q", msg)
		}
		_, view := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if view["status"] != TaskStatusReadyForDone {
			t.Fatalf("a refused close must leave the task open, got %v", view["status"])
		}
	})

	t.Run("a caller that is not the executor is a 403, the owner included", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		readyForDoneTask(t, api, h, owner, "T-1", "kip")
		stranger := apiTestAgentToken(t, api, "nosy", "")

		for _, tc := range []struct{ name, token string }{
			{"a foreign agent", stranger},
			{"the owner", owner},
		} {
			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", tc.token, "")
			if status != 403 {
				t.Fatalf("%s: want 403, got %d (%v)", tc.name, status, data)
			}
		}
	})
}

func TestHandleForceTaskDoneApiTasksTaskIdForceDonePost(t *testing.T) {
	// forcedTask stands a task up with a plan NOBODY finished — the shape
	// force_task_done exists for, and the one mark_task_done refuses.
	forcedTask := func(t *testing.T, api *apiServer, h http.Handler, owner string) string {
		t.Helper()
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		return agent
	}

	t.Run("the owner closes a task mid-plan and the forcing principal and reason are on every later read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		forcedTask(t, api, h, owner)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/force-done", owner,
			`{"reason":"kip is gone and the work shipped anyway"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if data["status"] != TaskStatusDone || data["closed_ts"] == nil {
			t.Fatalf("the receipt must report the closed task, got %v", data)
		}
		_, view := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if view["forced_done_by"] != wireOwnerID ||
			view["forced_done_reason"] != "kip is gone and the work shipped anyway" {
			t.Fatalf("the forced close must be readable off the task: %v", view)
		}
	})

	t.Run("the admin assistant may force, and a plain agent and the executor may not", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		if err := d.PutMember(Member{
			ID: "mira", Name: "Mira", Kind: KindStaff, RoleKey: adminRoleKey,
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember: %v", err)
		}
		executor := forcedTask(t, api, h, owner)
		stranger := apiTestAgentToken(t, api, "nosy", "")

		for _, tc := range []struct {
			name  string
			token string
			want  int
		}{
			{"the task's own executor", executor, 403},
			{"a plain agent", stranger, 403},
			{"the admin assistant", apiTestAgentToken(t, api, "mira", ""), 200},
		} {
			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/force-done", tc.token,
				`{"reason":"the executor is never coming back"}`)
			if status != tc.want {
				t.Fatalf("%s: want %d, got %d (%v)", tc.name, tc.want, status, data)
			}
		}
	})

	t.Run("a blank or missing reason is a 422 and the task stays open", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		forcedTask(t, api, h, owner)

		for _, body := range []string{`{"reason":"   "}`, `{}`} {
			status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/force-done", owner, body)
			if status != 422 {
				t.Fatalf("body %s: want 422, got %d (%v)", body, status, data)
			}
		}
		_, view := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if view["status"] == TaskStatusDone {
			t.Fatalf("a refused force must leave the task open, got %v", view["status"])
		}
	})

	t.Run("an already closed task is a 409 — this forces the precondition, not the terminal wall", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/force-done", owner,
			`{"reason":"try again anyway"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})
}
