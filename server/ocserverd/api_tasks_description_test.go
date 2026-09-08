package main

import (
	"encoding/json"
	"testing"
)

func TestTaskDescriptionHistorySnapshot(t *testing.T) {
	t.Skip("pinned end to end: " +
		"TestHandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost/\"a " +
		"corrected description is stored trimmed…\" asserts the revision " +
		"retained for a non-empty predecessor, and /\"the first correction of " +
		"a task that never had a description retains no revision\" asserts the " +
		"empty-predecessor branch retains nothing.")
}

func TestTaskDescriptionSnapshotIn(t *testing.T) {
	t.Skip("pinned end to end: the same two subtests read the retained set back " +
		"at GET /api/document-history/task_description/T-1 — one revision " +
		"holding the replaced text, none when there was no text to replace.")
}

func TestTaskDescriptionHistoryStream(t *testing.T) {
	t.Skip("pinned end to end: the retained revision read back in \"a corrected " +
		"description is stored trimmed…\" carries this stream's kind " +
		"(task_description), key (T-1) and actor_id (kip).")
}

func TestWriteTaskDescription(t *testing.T) {
	t.Skip("pinned end to end: the happy path asserts the stored text, the " +
		"retained revision and the fanned delta, and the omit/whitespace " +
		"subtests assert nothing is written, versioned or fanned when the " +
		"value does not move.")
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
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

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
