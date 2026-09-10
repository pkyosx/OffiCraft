package main

import (
	"strings"
	"testing"
)

func TestHandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(t *testing.T) {
	t.Run("the note replaces what the step held, is echoed as size and digest, and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent,
			`{"note":"  halfway through  "}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":     "T-1",
			"step_id":     stepID,
			"step_status": "pending",
			"size_chars":  15,
			"cap_chars":   10000,
			"sha256":      "e7ea19f08e8f2b6076240af5bb9642397941d4d96a565df7fbb53f5011113e75",
		})

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		apiWantValue(t, "step.note_size_chars", step["note_size_chars"], 15)

		taskFrame := map[string]any{
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
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an empty note clears the step's note", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":""}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.size_chars", data["size_chars"], 0)
		apiWantValue(t, "body.sha256", data["sha256"],
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "")
	})

	t.Run("a note lands on a step in any status the plan has put it in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"reviewed"}]}`)
		_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		steps, _ := task["steps"].([]any)
		first, _ := steps[0].(map[string]any)["id"].(string)
		second, _ := steps[1].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+first+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+first+"/status", agent, `{"status":"done"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+second+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+second+"/status", agent,
			`{"status":"waiting_external","waiting_reason":"the vendor has not replied"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+first+"/note", agent,
			`{"note":"what the draft settled"}`)
		if status != 200 {
			t.Fatalf("want 200 on a done step, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.step_status", data["step_status"], "done")

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+second+"/note", agent,
			`{"note":"waiting on the vendor"}`)
		if status != 200 {
			t.Fatalf("want 200 on a waiting step, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.step_status", data["step_status"], "waiting_external")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+first, owner, "")
		apiWantValue(t, "step.note", step["note"], "what the draft settled")
		_, step = apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+second, owner, "")
		apiWantValue(t, "step.note", step["note"], "waiting on the vendor")
	})

	t.Run("a note over the cap answers 400 naming both numbers and the stored note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)
		dashboard := apiTestListen(t, api, "")

		oversized := `{"note":"` + strings.Repeat("x", 10001) + `"}`
		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, oversized)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step note is 10001 chars, over the 10000-char limit")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "first pass")
		dashboard.wantFrames()
	})

	t.Run("a step that belongs to another task answers 404 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		foreign := apiTestOnlyStepID(t, h, owner, "T-1")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/steps/"+foreign+"/note", agent,
			`{"note":"wrong ticket"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step '"+foreign+"' not found")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+foreign, owner, "")
		apiWantValue(t, "step.note", step["note"], "")
		dashboard.wantFrames()
	})

	t.Run("a closed task answers 409 and the note stands, because the timeline stopped moving", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent,
			`{"note":"one more thought"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "first pass")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		mira := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", mira,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", mira, `{"note":"first pass"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", other,
			`{"note":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "first pass")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", machine,
			`{"note":"mine now"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "")
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming the task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/steps/ts-nosuchstep/note", owner,
			`{"note":"anything"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", "",
			`{"note":"mine now"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "first pass")
		dashboard.wantFrames()
	})

	t.Run("a body that never names note answers 422 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"first pass"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: note")

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, "{{{")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "first pass")
		dashboard.wantFrames()
	})
}

func TestHandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(t *testing.T) {
	t.Run("an anchored splice rewrites just that span, counts the edit and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent,
			`{"note":"halfway through the draft"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"step_id":       stepID,
			"step_status":   "pending",
			"applied_edits": 1,
			"size_chars":    33,
			"cap_chars":     10000,
			"sha256":        "f99d5c6977708a5b259c14f9cfc579a7e4e9077d12397c909526d8eeecc115c1",
		})

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "most of the way through the draft")

		taskFrame := map[string]any{
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
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an anchor the note does not carry answers 400 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"nowhere in the note","new":"x"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"edits[0]: old not found in the current doc — re-read (get_task_step) and re-anchor; nothing was written")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("a batch that leaves the text byte-identical reports no applied edits and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway","new":"halfway"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.applied_edits", data["applied_edits"], 0)
		apiWantValue(t, "body.size_chars", data["size_chars"], 15)

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("a patch that would empty the note is refused until allow_shrink says so", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway through","new":""}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"patch would empty (or shrink to under a tenth of) the step note — pass allow_shrink=true if this is intended, or use update_step_note; nothing was written")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway through","new":""}],"allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.applied_edits", data["applied_edits"], 1)
		apiWantValue(t, "body.size_chars", data["size_chars"], 0)

		_, step = apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "")
	})

	t.Run("a patch that grows the note past the cap answers 400 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway","new":"`+strings.Repeat("x", 10000)+`"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step note is 10008 chars, over the 10000-char limit")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("a closed task answers 409 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403 before its edits are read", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		mira := apiTestAgentToken(t, api, "mira", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", mira,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", mira, `{"note":"halfway through"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", other,
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", machine,
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("a step id the task does not carry answers 404 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-nosuchstep/note/patch", agent,
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step 'ts-nosuchstep' not found")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming the task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/steps/ts-nosuchstep/note/patch", owner,
			`{"edits":[{"old":"a","new":"b"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", "",
			`{"edits":[{"old":"halfway","new":"most of the way"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})

	t.Run("a body with no edits to apply answers 422 and the note stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note", agent, `{"note":"halfway through"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: edits")

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent,
			`{"edits":[]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "edits requires at least one {old, new} entry")

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/note/patch", agent, "{{{")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"invalid request body: invalid character '{' looking for beginning of object key string")

		_, step := apiJSON(t, h, "GET", "/api/tasks/T-1/steps/"+stepID, owner, "")
		apiWantValue(t, "step.note", step["note"], "halfway through")
		dashboard.wantFrames()
	})
}
