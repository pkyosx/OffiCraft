package main

import "testing"

func TestHandleListTaskArtifactsApiTasksTaskIdArtifactsGet(t *testing.T) {
	t.Run("a task with nothing pinned answers an empty artifact set rather than a 404", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"artifacts_detail_level": "full",
			"artifacts":              []any{},
		})
		dashboard.wantFrames()
	})

	t.Run("every pinned deliverable comes back whole, oldest first", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=chart&filename=chart.png&mime=image/png",
			agent, "pretend png bytes")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=report&description=what+it+says&filename=report.md&mime=text/markdown",
			agent, "# hi")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"artifacts_detail_level": "full",
			"artifacts": []any{
				map[string]any{
					"id":            apiAnyString,
					"kind":          "image",
					"attachment_id": apiAnyString,
					"name":          "chart",
					"description":   "",
					"filename":      "chart.png",
					"mime":          "image/png",
					"url":           apiAnyString,
					"created_ts":    apiAnyNumber,
					"created_by":    "kip",
					"version_count": 1,
				},
				map[string]any{
					"id":            apiAnyString,
					"kind":          "file",
					"attachment_id": apiAnyString,
					"name":          "report",
					"description":   "what it says",
					"filename":      "report.md",
					"mime":          "text/markdown",
					"url":           apiAnyString,
					"created_ts":    apiAnyNumber,
					"created_by":    "kip",
					"version_count": 1,
				},
			},
		})
		dashboard.wantFrames()
	})

	t.Run("a link's row answers the external target as its url and the uri-list blob as its mime", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"artifacts_detail_level": "full",
			"artifacts": []any{map[string]any{
				"id":            apiAnyString,
				"kind":          "link",
				"attachment_id": apiAnyString,
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "text/uri-list",
				"url":           "https://example.com/pr/123",
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
				"version_count": 1,
			}},
		})
	})

	t.Run("a machine identity reads the set too, because this route sits on the task view's read floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"artifacts_detail_level": "full",
			"artifacts":              []any{},
		})
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999/artifacts", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an ignored request body does not change the empty artifact set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"artifacts_detail_level": "full",
			"artifacts":              []any{},
		})
	})
}
