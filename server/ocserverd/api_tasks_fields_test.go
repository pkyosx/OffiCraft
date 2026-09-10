package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveTaskTextEdit(t *testing.T) {
	t.Run("an omitted body leaves both fields unset", func(t *testing.T) {
		edit, bad := resolveTaskTextEdit(Task{Title: "Ship it", Description: "old scope"}, nil, nil)
		if bad != "" {
			t.Fatalf("validation error = %q, want empty", bad)
		}
		if !edit.empty() {
			t.Fatalf("edit = %#v, want empty", edit)
		}
	})

	t.Run("surrounding whitespace is removed before both changed values are returned", func(t *testing.T) {
		title, description := "  Ship it, narrowed  ", "  new scope  "
		edit, bad := resolveTaskTextEdit(Task{Title: "Ship it", Description: "old scope"}, &title, &description)
		if bad != "" {
			t.Fatalf("validation error = %q, want empty", bad)
		}
		want := taskTextEdit{setTitle: true, title: "Ship it, narrowed", setDescription: true, description: "new scope"}
		if edit != want {
			t.Fatalf("edit = %#v, want %#v", edit, want)
		}
	})

	t.Run("a value equal after trimming is dropped as a no-op", func(t *testing.T) {
		title, description := "  Ship it  ", " old scope\n"
		edit, bad := resolveTaskTextEdit(Task{Title: "Ship it", Description: "old scope"}, &title, &description)
		if bad != "" {
			t.Fatalf("validation error = %q, want empty", bad)
		}
		if !edit.empty() {
			t.Fatalf("edit = %#v, want empty", edit)
		}
	})

	t.Run("a blank title rejects the whole body before a description can be selected", func(t *testing.T) {
		title, description := " \n\t ", "new scope"
		edit, bad := resolveTaskTextEdit(Task{Title: "Ship it", Description: "old scope"}, &title, &description)
		if bad != "title must not be blank" {
			t.Fatalf("validation error = %q, want title blank refusal", bad)
		}
		if edit != (taskTextEdit{}) {
			t.Fatalf("edit = %#v, want empty after refusal", edit)
		}
	})

	t.Run("whitespace-only description is a real clear", func(t *testing.T) {
		description := " \n\t "
		edit, bad := resolveTaskTextEdit(Task{Title: "Ship it", Description: "old scope"}, nil, &description)
		if bad != "" {
			t.Fatalf("validation error = %q, want empty", bad)
		}
		want := taskTextEdit{setDescription: true, description: ""}
		if edit != want {
			t.Fatalf("edit = %#v, want %#v", edit, want)
		}
	})
}

func TestWriteTaskText(t *testing.T) {
	t.Run("one write changes both fields and retains each predecessor", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: task=%#v err=%v", task, err)
		}

		ok, err := api.writeTaskText(task, "agent-kip", taskTextEdit{
			setTitle: true, title: "Ship it, narrowed",
			setDescription: true, description: "new scope",
		})
		if err != nil || !ok {
			t.Fatalf("writeTaskText: ok=%v err=%v", ok, err)
		}
		if task.Title != "Ship it, narrowed" || task.Description != "new scope" || task.UpdatedTS <= 0 {
			t.Fatalf("updated task = %#v", *task)
		}

		stored, err := d.GetTask("T-1")
		if err != nil || stored == nil {
			t.Fatalf("GetTask after write: task=%#v err=%v", stored, err)
		}
		if stored.Title != "Ship it, narrowed" || stored.Description != "new scope" {
			t.Fatalf("stored task = %#v", *stored)
		}
		for kind, want := range map[string]string{
			docKindTaskTitle:       `{"title":"Ship it"}`,
			docKindTaskDescription: `{"description":"old scope"}`,
		} {
			history, err := d.ListDocumentHistory(kind, "T-1")
			if err != nil {
				t.Fatalf("ListDocumentHistory(%q): %v", kind, err)
			}
			if len(history) != 1 || history[0].ContentJSON != want || history[0].ActorID != "agent-kip" {
				t.Fatalf("history %q = %#v, want one %q by agent-kip", kind, history, want)
			}
		}
	})

	t.Run("a task row that vanishes returns false without retaining a revision", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		missing := &Task{ID: "T-ghost"}
		ok, err := api.writeTaskText(missing, "agent-kip", taskTextEdit{setTitle: true, title: "unclaimed"})
		if err != nil || ok {
			t.Fatalf("writeTaskText: ok=%v err=%v, want false nil", ok, err)
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

func TestUpdateTaskText(t *testing.T) {
	t.Run("the shared body writes both requested fields and returns their receipt", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		title, description := "  Ship it, narrowed  ", "  new scope  "

		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			rec := httptest.NewRecorder()
			api.updateTaskText(rec, r, "T-1", &title, &description)
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("non-JSON body: %s", rec.Body.String())
			}
			apiWantBody(t, body, map[string]any{
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
		})

		stored, err := d.GetTask("T-1")
		if err != nil || stored == nil {
			t.Fatalf("GetTask after update: task=%#v err=%v", stored, err)
		}
		if stored.Title != "Ship it, narrowed" || stored.Description != "new scope" {
			t.Fatalf("stored task = %#v", *stored)
		}
	})

	t.Run("a blank title refuses a paired description before either field moves", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"old scope"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		title, description := "  ", "new scope"
		before, err := d.GetTask("T-1")
		if err != nil || before == nil {
			t.Fatalf("GetTask before update: task=%#v err=%v", before, err)
		}

		taskTestUnderCaller(t, api, d, agent, func(r *http.Request) {
			rec := httptest.NewRecorder()
			api.updateTaskText(rec, r, "T-1", &title, &description)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("non-JSON body: %s", rec.Body.String())
			}
			apiWantError(t, body, "validation_error", "title must not be blank")
		})

		after, err := d.GetTask("T-1")
		if err != nil || after == nil {
			t.Fatalf("GetTask after refused update: task=%#v err=%v", after, err)
		}
		if after.Title != before.Title || after.Description != before.Description {
			t.Fatalf("refused update changed task from %#v to %#v", *before, *after)
		}
	})
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
