package main

import (
	"encoding/json"
	"testing"
)

func TestResolveTaskTextEdit(t *testing.T) {
	t.Skip("pinned end to end: TestHandleUpdateTaskApiTasksTaskIdPost pins every " +
		"verdict this resolver reaches — the blank-title 400 that refuses the " +
		"whole body, the trimming of both fields, the unchanged-value drop " +
		"(\"a body naming neither field…\") and the per-field enrolment (\"each " +
		"field is versioned in its own series…\").")
}

func TestWriteTaskText(t *testing.T) {
	t.Skip("pinned end to end: \"a title and a description sent together land in " +
		"one write and fan one delta\" asserts both columns and a single " +
		"delta, and \"each field is versioned in its own series, and only the " +
		"field that moved is\" asserts only the changing field is enrolled in " +
		"history.")
}

func TestUpdateTaskText(t *testing.T) {
	t.Skip("pinned end to end: its guard order and every exit are pinned three " +
		"times over — through POST /api/tasks/{task_id} here, and through the " +
		"title and description doors in api_tasks_title_test.go and " +
		"api_tasks_description_test.go.")
}

func TestHandleUpdateTaskApiTasksTaskIdPost(t *testing.T) {
	t.Run("a title and a description sent together land in one write and fan one delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", agent,
			`{"title":"  Ship it, narrowed  ","description":"  new scope  "}`)
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
			"description_size_chars": 9,
			"description_sha256":     "b8f2759f57c3d5b65ba8415b767234f9e486ea9ad45d90fd83dedebd2f1ed701",
		})

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it, narrowed")
		apiWantValue(t, "task.description", task["description"], "new scope")

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

	t.Run("each field is versioned in its own series, and only the field that moved is", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", owner,
			`{"title":"Ship it, narrowed","description":"old scope"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}

		rec := apiRequest(t, h, "GET", "/api/document-history/task_title/T-1", owner, "")
		var titles []any
		if err := json.Unmarshal(rec.Body.Bytes(), &titles); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "task_title history", any(titles), []any{map[string]any{
			"id":          apiAnyNumber,
			"actor_id":    "owner",
			"created_ts":  apiAnyNumber,
			"field_chars": map[string]any{"title": 7},
			"tombstoned":  false,
		}})

		rec = apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
		var descriptions []any
		if err := json.Unmarshal(rec.Body.Bytes(), &descriptions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "task_description history", any(descriptions), []any{})
	})

	t.Run("a blank title refuses the whole body, so the good description beside it is not written either", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", agent,
			`{"title":"   ","description":"new scope"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "title must not be blank")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("a body naming neither field answers the receipt unchanged and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.title", data["title"], "Ship it")
		apiWantValue(t, "body.description_size_chars", data["description_size_chars"], 9)
		dashboard.wantFrames()
	})

	t.Run("a description alone leaves the title standing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", owner, `{"description":"new scope"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.title", data["title"], "Ship it")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		apiWantValue(t, "task.description", task["description"], "new scope")
	})

	t.Run("a closed task's text is still correctable through this door", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", owner,
			`{"title":"Ship it (dropped)","description":"what it actually was"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.status", data["status"], "terminated")
		apiWantValue(t, "body.title", data["title"], "Ship it (dropped)")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.description", task["description"], "what it actually was")
	})

	t.Run("an agent that is not the task's executor answers 403 and neither field moves", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"mira","description":"old scope"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", other,
			`{"title":"mine now","description":"mine too"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", machine, `{"title":"mine now"}`)
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

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999", owner, `{"title":"anything"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and neither field moves", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", "",
			`{"title":"mine now","description":"mine too"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})

	t.Run("a body the strict decoder refuses answers 422 and neither field moves", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1", agent,
			`{"title":"fine","status":"done"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`invalid request body: json: unknown field "status"`)

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1", agent, "{{{")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "task.title", task["title"], "Ship it")
		apiWantValue(t, "task.description", task["description"], "old scope")
		dashboard.wantFrames()
	})
}
