package main

import (
	"encoding/json"
	"testing"
)

func TestTaskTitleSnapshotIn(t *testing.T) {
	t.Run("the transaction reader returns the current task title", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip"}`)

		got, err := taskTitleSnapshotIn("T-1")(d.rdb)
		if err != nil {
			t.Fatalf("taskTitleSnapshotIn: %v", err)
		}
		if got != `{"title":"Ship it"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"title":"Ship it"}`)
		}
	})

	t.Run("the transaction reader represents a missing task as an empty document", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)

		got, err := taskTitleSnapshotIn("T-ghost")(d.rdb)
		if err != nil {
			t.Fatalf("taskTitleSnapshotIn: %v", err)
		}
		if got != "{}" {
			t.Fatalf("snapshot = %q, want {}", got)
		}
	})
}

func TestTaskTitleHistoryStream(t *testing.T) {
	_, h, d, owner := newAPITestServer(t)
	apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship it","executor_member_id":"kip"}`)

	stream := taskTitleHistoryStream("T-1", "agent-kip")
	if stream.Kind != docKindTaskTitle || stream.Key != "T-1" || stream.ActorID != "agent-kip" {
		t.Fatalf("stream identity = {%q, %q, %q}", stream.Kind, stream.Key, stream.ActorID)
	}
	snapshot, err := stream.Snapshot(d.rdb)
	if err != nil {
		t.Fatalf("stream snapshot: %v", err)
	}
	if snapshot != `{"title":"Ship it"}` {
		t.Fatalf("stream snapshot = %q, want %q", snapshot, `{"title":"Ship it"}`)
	}
}

func TestWriteTaskTitle(t *testing.T) {
	t.Run("a write stores the new title and retains the replaced title", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		ok, err := api.writeTaskTitle(task, "agent-kip", "Ship it, narrowed")
		if err != nil || !ok {
			t.Fatalf("writeTaskTitle: ok=%v err=%v", ok, err)
		}
		if task.Title != "Ship it, narrowed" || task.UpdatedTS <= 0 {
			t.Fatalf("updated task = %#v", *task)
		}
		stored, err := d.GetTask("T-1")
		if err != nil || stored == nil || stored.Title != "Ship it, narrowed" {
			t.Fatalf("stored task = %#v err=%v", stored, err)
		}
		history, err := d.ListDocumentHistory(docKindTaskTitle, "T-1")
		if err != nil {
			t.Fatalf("ListDocumentHistory: %v", err)
		}
		if len(history) != 1 || history[0].ContentJSON != `{"title":"Ship it"}` || history[0].ActorID != "agent-kip" {
			t.Fatalf("history = %#v", history)
		}
	})

	t.Run("a missing task reports false and retains no title revision", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		ok, err := api.writeTaskTitle(&Task{ID: "T-ghost"}, "agent-kip", "unclaimed")
		if err != nil || ok {
			t.Fatalf("writeTaskTitle: ok=%v err=%v, want false nil", ok, err)
		}
		history, err := d.ListDocumentHistory(docKindTaskTitle, "T-ghost")
		if err != nil {
			t.Fatalf("ListDocumentHistory: %v", err)
		}
		if len(history) != 0 {
			t.Fatalf("missing task history = %#v, want empty", history)
		}
	})
}

func TestHandleUpdateTaskTitleApiTasksTaskIdTitlePost(t *testing.T) {
	t.Run("a corrected title is stored, answered in the write receipt and fanned to the task's watchers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"desc"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent,
			`{"title":"  Ship it, narrowed  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it, narrowed",
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

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it, narrowed")
		apiWantValue(t, "task.description", task["description"], "desc")

		rec := apiRequest(t, h, "GET", "/api/document-history/task_title/T-1", owner, "")
		var retained []any
		if err := json.Unmarshal(rec.Body.Bytes(), &retained); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "document-history", any(retained), []any{map[string]any{
			"id":          apiAnyNumber,
			"actor_id":    "kip",
			"created_ts":  apiAnyNumber,
			"field_chars": map[string]any{"title": 7},
			"tombstoned":  false,
		}})

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

	t.Run("the same title sent back with stray whitespace is no change, so nothing is versioned and nothing fans", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent, `{"title":"  Ship it  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.title", data["title"], "Ship it")
		dashboard.wantFrames()

		rec := apiRequest(t, h, "GET", "/api/document-history/task_title/T-1", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var retained []any
		if err := json.Unmarshal(rec.Body.Bytes(), &retained); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "document-history", any(retained), []any{})
	})

	t.Run("omitting the title is a legal no-op that answers the receipt and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.title", data["title"], "Ship it")
		dashboard.wantFrames()
	})

	t.Run("an explicit blank title answers 400 and the stored title stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent, `{"title":"   "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "title must not be blank")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		dashboard.wantFrames()
	})

	t.Run("a closed task's title is still correctable", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", owner, `{"title":"Ship it (dropped)"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.title", data["title"], "Ship it (dropped)")
		apiWantValue(t, "body.status", data["status"], "terminated")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it (dropped)")
	})

	t.Run("an agent that is not the task's executor answers 403 and the title stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", other, `{"title":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", machine, `{"title":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/title", owner, `{"title":"anything"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and the title stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", "", `{"title":"mine now"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		dashboard.wantFrames()
	})

	t.Run("a body the strict decoder refuses answers 422 and the title stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent, `{"titel":"typo"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "titel"`)

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/title", agent, "{{{")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		dashboard.wantFrames()
	})
}
