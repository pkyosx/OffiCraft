package main

import (
	"encoding/json"
	"testing"
)

func TestTaskTitleSnapshotIn(t *testing.T) {
	t.Skip("pinned end to end: " +
		"TestHandleUpdateTaskTitleApiTasksTaskIdTitlePost/\"a corrected title " +
		"is stored…\" asserts the retained revision holds the title this write " +
		"REPLACED (field_chars {\"title\": 7} for \"Ship it\" while the live " +
		"title is \"Ship it, narrowed\"), which is the whole observable " +
		"behaviour of the in-transaction re-read.")
}

func TestTaskTitleHistoryStream(t *testing.T) {
	t.Skip("pinned end to end: the same subtest asserts the retained revision's " +
		"kind/key/actor by reading it back at GET " +
		"/api/document-history/task_title/T-1 with actor_id \"kip\" — the three " +
		"fields this stream sets.")
}

func TestWriteTaskTitle(t *testing.T) {
	t.Skip("pinned end to end: " +
		"TestHandleUpdateTaskTitleApiTasksTaskIdTitlePost/\"a corrected title " +
		"is stored…\" asserts the stored title, the retained revision and the " +
		"fanned delta all landed, and the no-op and 400 subtests assert " +
		"nothing is written when the edit is empty or refused.")
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
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

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
