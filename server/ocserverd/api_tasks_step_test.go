package main

import (
	"net/http"
	"testing"
)

// apiTestOnlyStepID answers the id of the single step a one-step plan minted on
// taskID — the address every step-scoped route takes, read back through the task
// view rather than out of the database.
func apiTestOnlyStepID(t *testing.T, h http.Handler, token, taskID string) string {
	t.Helper()
	status, data := apiJSON(t, h, "GET", "/api/tasks/"+taskID, token, "")
	if status != 200 {
		t.Fatalf("read back %s: %d %v", taskID, status, data)
	}
	steps, _ := data["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("want exactly one step on %s, got %v", taskID, steps)
	}
	first, _ := steps[0].(map[string]any)
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatalf("the step carries no id: %v", first)
	}
	return id
}

func TestHandleGetTaskStepApiTasksTaskIdStepsStepIdGet(t *testing.T) {
	t.Run("the step id in the path selects that step and it comes back whole, note included", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent,
			`{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"detail_level":      "full",
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
			"note":              "halfway through",
			"note_size_chars":   15,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		})
		dashboard.wantFrames()
	})

	t.Run("a step the plan has moved on answers the status and stamps it carries now", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"detail_level":      "full",
			"id":                stepID,
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
			"note":              "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        apiAnyNumber,
			"finished_ts":       0,
		})
	})

	t.Run("a step that belongs to another task is absent from this one, so 404 rather than served", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		foreign := apiTestOnlyStepID(t, h, owner, "T-1")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-2/steps/"+foreign, owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step '"+foreign+"' not found")
	})

	t.Run("a step id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/ts-nosuchstep", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step 'ts-nosuchstep' not found")
	})

	t.Run("an id no task carries answers 404 naming the task, not the step", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999/steps/ts-nosuchstep", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a machine identity reads the step too, because this route sits on the task view's read floor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.id", data["id"], stepID)
		apiWantValue(t, "body.name", data["name"], "Draft")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})

	t.Run("an ignored request body does not change the selected step", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "{{{")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.id", data["id"], stepID)
		apiWantValue(t, "body.name", data["name"], "Draft")
		apiWantValue(t, "body.status", data["status"], "pending")
	})
}
