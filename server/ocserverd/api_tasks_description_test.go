package main

import (
	"encoding/json"
	"testing"
)

func TestTaskDescriptionHistorySnapshot(t *testing.T) {
	for name, tc := range map[string]struct {
		description string
		want        string
	}{
		"an empty description retains no revision": {description: "", want: "{}"},
		"a non-empty description is retained as its own document": {
			description: "old scope", want: `{"description":"old scope"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := taskDescriptionHistorySnapshot(tc.description)
			if err != nil {
				t.Fatalf("taskDescriptionHistorySnapshot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("snapshot = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTaskDescriptionSnapshotIn(t *testing.T) {
	t.Run("the transaction reader returns the current task description", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

		got, err := taskDescriptionSnapshotIn("T-1")(d.rdb)
		if err != nil {
			t.Fatalf("taskDescriptionSnapshotIn: %v", err)
		}
		if got != `{"description":"old scope"}` {
			t.Fatalf("snapshot = %q, want %q", got, `{"description":"old scope"}`)
		}
	})

	t.Run("the transaction reader represents a missing task as an empty document", func(t *testing.T) {
		_, _, d, _ := newAPITestServer(t)

		got, err := taskDescriptionSnapshotIn("T-ghost")(d.rdb)
		if err != nil {
			t.Fatalf("taskDescriptionSnapshotIn: %v", err)
		}
		if got != "{}" {
			t.Fatalf("snapshot = %q, want {}", got)
		}
	})
}

func TestTaskDescriptionHistoryStream(t *testing.T) {
	_, h, d, owner := newAPITestServer(t)
	apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

	stream := taskDescriptionHistoryStream("T-1", "agent-kip")
	if stream.Kind != docKindTaskDescription || stream.Key != "T-1" || stream.ActorID != "agent-kip" {
		t.Fatalf("stream identity = {%q, %q, %q}", stream.Kind, stream.Key, stream.ActorID)
	}
	snapshot, err := stream.Snapshot(d.rdb)
	if err != nil {
		t.Fatalf("stream snapshot: %v", err)
	}
	if snapshot != `{"description":"old scope"}` {
		t.Fatalf("stream snapshot = %q, want %q", snapshot, `{"description":"old scope"}`)
	}
}

func TestWriteTaskDescription(t *testing.T) {
	t.Run("a write stores the new text and retains the replaced description", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		ok, err := api.writeTaskDescription(task, "agent-kip", "new scope")
		if err != nil || !ok {
			t.Fatalf("writeTaskDescription: ok=%v err=%v", ok, err)
		}
		if task.Description != "new scope" || task.UpdatedTS <= 0 {
			t.Fatalf("updated task = %#v", *task)
		}
		stored, err := d.GetTask("T-1")
		if err != nil || stored == nil || stored.Description != "new scope" {
			t.Fatalf("stored task = %#v err=%v", stored, err)
		}
		history, err := d.ListDocumentHistory(docKindTaskDescription, "T-1")
		if err != nil {
			t.Fatalf("ListDocumentHistory: %v", err)
		}
		if len(history) != 1 || history[0].ContentJSON != `{"description":"old scope"}` || history[0].ActorID != "agent-kip" {
			t.Fatalf("history = %#v", history)
		}
	})

	t.Run("a missing task reports false and retains no description revision", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		ok, err := api.writeTaskDescription(&Task{ID: "T-ghost"}, "agent-kip", "new scope")
		if err != nil || ok {
			t.Fatalf("writeTaskDescription: ok=%v err=%v, want false nil", ok, err)
		}
		history, err := d.ListDocumentHistory(docKindTaskDescription, "T-ghost")
		if err != nil {
			t.Fatalf("ListDocumentHistory: %v", err)
		}
		if len(history) != 0 {
			t.Fatalf("missing task history = %#v, want empty", history)
		}
	})
}

func TestHandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(t *testing.T) {
	t.Run("a corrected description is stored trimmed, answered as size and digest, and fanned to the task's watchers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent,
			`{"description":"  new scope  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
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
			"description_size_chars": 9,
			"description_sha256":     "b8f2759f57c3d5b65ba8415b767234f9e486ea9ad45d90fd83dedebd2f1ed701",
		})

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "new scope")
		apiWantValue(t, "task.title", task["title"], "Ship it")

		rec := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var retained []any
		if err := json.Unmarshal(rec.Body.Bytes(), &retained); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "document-history", any(retained), []any{map[string]any{
			"id":          apiAnyNumber,
			"actor_id":    "kip",
			"created_ts":  apiAnyNumber,
			"field_chars": map[string]any{"description": 9},
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

	t.Run("the first correction of a task that never had a description retains no revision", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", owner,
			`{"description":"the scope, at last"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 18)

		rec := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var retained []any
		if err := json.Unmarshal(rec.Body.Bytes(), &retained); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "document-history", any(retained), []any{})

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "the scope, at last")
	})

	t.Run("an explicit blank description clears the stored text", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", owner, `{"description":""}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 0)
		apiWantValue(t, "body.description_sha256", data["description_sha256"],
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "")
	})

	t.Run("a description of nothing but whitespace clears the field too", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", owner, `{"description":"   \n  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 0)

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "")
	})

	t.Run("omitting the description changes nothing, versions nothing and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 9)

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")

		rec := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var retained []any
		if err := json.Unmarshal(rec.Body.Bytes(), &retained); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "document-history", any(retained), []any{})
		dashboard.wantFrames()
	})

	t.Run("the stored text sent back with stray whitespace is no change, so nothing fans", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent,
			`{"description":"  old scope  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 9)
		dashboard.wantFrames()
	})

	t.Run("a closed task's description is still correctable", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", owner,
			`{"description":"what it actually was"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.status", data["status"], "terminated")
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 20)

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "what it actually was")
	})

	t.Run("an agent that is not the task's executor answers 403 and the text stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"mira","description":"old scope"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", other,
			`{"description":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", machine,
			`{"description":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/description", owner,
			`{"description":"anything"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and the text stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", "",
			`{"description":"mine now"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("a body the strict decoder refuses answers 422 and the text stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent,
			`{"desc":"typo"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "desc"`)

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/description", agent, "{{{")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})
}
