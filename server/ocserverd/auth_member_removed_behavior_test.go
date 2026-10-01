package main

// A member that has left the roster — dismissed staff, or an outsource worker
// released by its task's close — is refused on every authenticated surface with
// the standing-refusal marker cli/ocagent's listener acts on. Every arm drives
// the full handler chain in memory with production-minted credentials.

import (
	"net/http"
	"testing"
)

func removedRefusalBody(id string) map[string]any {
	return map[string]any{"error": map[string]any{
		"code":    "unauthorized",
		"message": "member '" + id + "' has left the roster; its credentials are no longer valid",
	}}
}

// removedSurfaces are one authenticated REST read, one MCP tool call and the
// SSE handshake — the three doors a live session uses.
func removedSurfaces(taskID string) []struct{ name, method, path, body string } {
	return []struct{ name, method, path, body string }{
		{"REST get_task", "GET", "/api/tasks/" + taskID, ""},
		{"MCP tools/call get_task", "POST", "/api/mcp",
			`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_task","arguments":{"task_id":"` +
				taskID + `"}}}`},
		{"SSE /api/events", "GET", "/api/events", ""},
	}
}

func wantRemovedRefused(t *testing.T, h http.Handler, token, id, taskID string) {
	t.Helper()
	for _, s := range removedSurfaces(taskID) {
		rec := apiRequest(t, h, s.method, s.path, token, s.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s by removed %s: want 401, got %d %s", s.name, id, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("X-OC-Auth-Refusal"); got != "member-removed" {
			t.Fatalf("%s by removed %s: X-OC-Auth-Refusal = %q, want %q", s.name, id, got, "member-removed")
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), removedRefusalBody(id))
	}
}

// wantAdmitted is the contrasting positive: the same three doors answer a
// caller that is still on the roster, with no refusal marker anywhere.
func wantAdmitted(t *testing.T, api *apiServer, h http.Handler, token, id, taskID string) {
	t.Helper()
	for _, s := range removedSurfaces(taskID)[:2] {
		rec := apiRequest(t, h, s.method, s.path, token, s.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s by on-roster %s: want 200, got %d %s", s.name, id, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("X-OC-Auth-Refusal"); got != "" {
			t.Fatalf("%s by on-roster %s: X-OC-Auth-Refusal = %q, want none", s.name, id, got)
		}
		body := apiTestDecodeJSONBody(t, rec)
		if body["error"] != nil {
			t.Fatalf("%s by on-roster %s: want no error, got %v", s.name, id, body)
		}
		if result, isRPC := body["result"].(map[string]any); isRPC && result["isError"] != false {
			t.Fatalf("%s by on-roster %s: the tool must succeed, got %v", s.name, id, result)
		}
	}
	stream := apiEventsStream(t, h, token, "")
	if stream.code() != http.StatusOK {
		t.Fatalf("SSE by on-roster %s: want 200, got %d", id, stream.code())
	}
	if !api.hub.IsOnline(id) {
		t.Fatalf("SSE by on-roster %s: the connection must project it online", id)
	}
	stream.stop()
}

func TestDismissedStaffCredentialsAreRefusedEverywhere(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	api.loopback = h
	if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship it","executor_member_id":"kip"}`); status != 200 {
		t.Fatalf("create task: %d %v", status, data)
	}
	kip := apiTestAgentToken(t, api, "kip", "")
	mira := apiTestAgentToken(t, api, "mira", "")
	wantAdmitted(t, api, h, kip, "kip", "T-1")

	if status, data := apiJSON(t, h, "DELETE", "/api/members/kip", owner, ""); status != 200 {
		t.Fatalf("dismiss: want 200, got %d (%v)", status, data)
	}

	wantRemovedRefused(t, h, kip, "kip", "T-1")
	if api.hub.IsOnline("kip") {
		t.Fatal("a refused SSE handshake must not project the dismissed member online")
	}
	wantAdmitted(t, api, h, mira, "mira", "T-1")
}

func TestStoppedStaffOnTheRosterKeepsItsCredentials(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	api.loopback = h
	if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship it","executor_member_id":"kip"}`); status != 200 {
		t.Fatalf("create task: %d %v", status, data)
	}
	kip := apiTestAgentToken(t, api, "kip", "")
	for _, verb := range []string{"deactivate", "force-stop"} {
		if status, data := apiJSON(t, h, "POST", "/api/members/kip/"+verb, owner, ""); status != 200 {
			t.Fatalf("%s: want 200, got %d (%v)", verb, status, data)
		}
	}

	for _, s := range removedSurfaces("T-1")[:2] {
		rec := apiRequest(t, h, s.method, s.path, kip, s.body)
		if rec.Code != http.StatusOK || rec.Header().Get("X-OC-Auth-Refusal") != "" {
			t.Fatalf("%s by a stopped member: want 200 unmarked, got %d %q %s", s.name,
				rec.Code, rec.Header().Get("X-OC-Auth-Refusal"), rec.Body.String())
		}
	}
	// The stop gate still owns the stopped member's SSE: a 409, not this refusal.
	rec := apiRequest(t, h, "GET", "/api/events", kip, "")
	if rec.Code != http.StatusConflict || rec.Header().Get("X-OC-Auth-Refusal") != "" {
		t.Fatalf("SSE by a stopped member: want an unmarked 409, got %d %q %s",
			rec.Code, rec.Header().Get("X-OC-Auth-Refusal"), rec.Body.String())
	}
	apiWantError(t, apiTestDecodeJSONBody(t, rec), "conflict",
		"member 'kip' has a stop in effect (desired_state=offline) — SSE refused "+
			"(a stopped member must not re-project online; activate it to reconnect)")
}

func TestReleasedWorkerCredentialsAreRefusedAfterItsOwnMarkDone(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	api.loopback = h
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 1)
	if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"review","type_key":"review-pr"}`); status != 200 {
		t.Fatalf("create task: %d %v", status, data)
	}
	api.runOutsourceTick(1000.0)
	task, err := d.GetTask("T-1")
	if err != nil || task == nil || task.ExecutorKind != KindOutsource || task.ExecutorID == "" {
		t.Fatalf("the tick must bind a worker to T-1, got %+v (%v)", task, err)
	}
	workerID := task.ExecutorID
	worker := apiTestAgentToken(t, api, workerID, "")
	if status, data := apiJSON(t, h, "POST", "/api/self/waking", worker, `{}`); status != 200 {
		t.Fatalf("report waking: %d %v", status, data)
	}
	readyForDoneTask(t, api, h, owner, "T-1", workerID)
	_, view := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
	stepID, _ := view["steps"].([]any)[0].(map[string]any)["id"].(string)

	closeOut := []struct{ name, method, path, body string }{
		{"step note", "POST", "/api/tasks/T-1/steps/" + stepID + "/note", `{"note":"close-out notes"}`},
		{"add artifact", "POST", "/api/tasks/T-1/artifact",
			`{"kind":"link","name":"PR #1","description":"the change","url":"https://example.com/pr/1"}`},
		{"reply card", "POST", "/api/reply-cards",
			`{"kind":"decision","summary":"ship?","options":[{"text":"yes"}],"linked_task":null}`},
		{"write lore", "POST", "/api/lore", `{"title":"lesson","body":"what the review taught"}`},
	}
	for _, c := range closeOut {
		rec := apiRequest(t, h, c.method, c.path, worker, c.body)
		if rec.Code != http.StatusOK || rec.Header().Get("X-OC-Auth-Refusal") != "" {
			t.Fatalf("ready_for_done close-out %s: want 200 unmarked, got %d %q %s", c.name,
				rec.Code, rec.Header().Get("X-OC-Auth-Refusal"), rec.Body.String())
		}
	}
	wantAdmitted(t, api, h, worker, workerID, "T-1")

	status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-done", worker, "")
	if status != 200 {
		t.Fatalf("the worker's own mark_task_done: want 200, got %d (%v)", status, data)
	}
	if data["status"] != TaskStatusDone {
		t.Fatalf("mark_task_done must close the task, got %v", data)
	}
	released, err := d.GetMember(workerID)
	if err != nil || released == nil || released.RosterStatus != RosterStatusRemoved {
		t.Fatalf("the close must release the worker, got %+v (%v)", released, err)
	}

	wantRemovedRefused(t, h, worker, workerID, "T-1")
	if api.hub.IsOnline(workerID) {
		t.Fatal("a refused SSE handshake must not project the released worker online")
	}
	kip := apiTestAgentToken(t, api, "kip", "")
	wantAdmitted(t, api, h, kip, "kip", "T-1")
}
