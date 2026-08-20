package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-b56e — who may terminate a task.
//
// Owner ruling 2026-08-20 (card rc-b896e3f641e7, option 0): "開給執行者（可終止
// 自己名下的票）". Before it, terminate sat behind principalAdminAgent, so every
// cull of an agent's own ticket had to be relayed through the admin assistant.
//
// The gate is callerMayTerminateTask, and it is callerMayDriveTask MINUS the
// outsource worker on its own task. Each case below is one AC, and the negative
// ones assert the REASON, not merely "some 4xx" — a 403 that arrives for the
// wrong reason would pass a status-only assertion while the guard it names is
// gone.
func terminateAs(t *testing.T, api *apiServer, taskID, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/terminate", nil, sub, scope),
		taskID)
	return rec
}

// AC 1 — the ruling itself: a 正職 closes its OWN ticket, with nobody relayed.
func TestTerminateAdmitsTheTasksOwnMemberExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-exec", "Executor", KindAssistant)

	task := createAdHocTask(t, api, "m-exec")
	rec := terminateAs(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("executor terminate = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The receipt is not the point — the stored status is. A handler that
	// answered 200 without closing the task would pass on the code alone.
	if after := readTask(t, api, task.ID); after.Status != TaskStatusTerminated {
		t.Fatalf("status after terminate = %q, want %q", after.Status, TaskStatusTerminated)
	}
}

// AC 2 — the ruling is about YOUR OWN ticket. Standing does not spread sideways.
func TestTerminateRefusesAMemberOnSomeoneElsesTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-exec", "Executor", KindAssistant)
	putActiveMember(t, api, "m-stranger", "Stranger", KindAssistant)

	task := createAdHocTask(t, api, "m-exec")
	rec := terminateAs(t, api, task.ID, "m-stranger", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger terminate = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec, "forbidden", "caller is not the task's executor")
	if after := readTask(t, api, task.ID); after.Status == TaskStatusTerminated {
		t.Fatal("refused terminate still closed the task")
	}
}

// AC 3 — the ONE subtraction from callerMayDriveTask, and the reason it is a
// separate function. An outsource worker IS the task's executor here, so every
// other executor-gated route admits it; this one must not. The task is the
// reason that worker exists, and it leaves when the task closes — so a
// self-terminate ends the worker with no second person in the loop.
//
// ⚠️ This is NOT expressible on the principal ladder: a 正職 and an outsource
// worker both rank principalAgent. Member.Kind is the discriminator, which is
// exactly what makes it droppable — hence this case.
func TestTerminateRefusesAnOutsourceWorkerOnItsOwnTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putMemberRow(t, api, "ow-1", KindOutsource, "")

	// An outsource worker may not create a task at all (T-23cf), so the owner
	// hands it one — the only route that produces this fixture.
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "contractor task", "executor_member_id": "ow-1"},
		"owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner create for worker: %d %s", rec.Code, rec.Body.String())
	}
	task := decodeBody[taskCreateResultDTO](t, rec).Task
	// The fixture IS the premise: if the executor were not the worker, the 403
	// below would be the stranger case wearing this test's name.
	if got := readTask(t, api, task.ID); got.ExecutorID != "ow-1" {
		t.Fatalf("fixture broken: executor = %q, want ow-1", got.ExecutorID)
	}

	got := terminateAs(t, api, task.ID, "ow-1", "agent")
	if got.Code != http.StatusForbidden {
		t.Fatalf("worker self-terminate = %d, want 403: %s", got.Code, got.Body.String())
	}
	assertErrorEnvelope(t, got, "forbidden",
		"an outsource worker may not terminate its own task; ask the owner or an admin agent")
	if after := readTask(t, api, task.ID); after.Status == TaskStatusTerminated {
		t.Fatal("refused terminate still closed the worker's task")
	}
}

// AC 4 — the two callers who could already do this keep doing it. Opening a
// gate is only half the claim; the other half is that nothing that worked
// before stopped working.
func TestTerminateStillAdmitsOwnerAndAdminAgent(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-exec", "Executor", KindAssistant)
	putMemberRow(t, api, "m-mira", KindAssistant, adminRoleKey)

	byOwner := createAdHocTask(t, api, "m-exec")
	if rec := terminateAs(t, api, byOwner.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner terminate = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	byAdmin := createAdHocTask(t, api, "m-exec")
	if rec := terminateAs(t, api, byAdmin.ID, "m-mira", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("admin terminate = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// Guard order: authz is decided BEFORE the terminal-state probe, so a caller
// with no standing cannot learn whether a task is already closed by reading
// which error comes back. The handler denies before it probes state.
func TestTerminateDeniesBeforeItProbesTerminalState(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-exec", "Executor", KindAssistant)
	putActiveMember(t, api, "m-stranger", "Stranger", KindAssistant)

	task := createAdHocTask(t, api, "m-exec")
	if rec := terminateAs(t, api, task.ID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("setup terminate = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Already closed. The executor gets the 409 (it had standing); the stranger
	// still gets the 403 (it never did).
	if rec := terminateAs(t, api, task.ID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("second terminate by executor = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	rec := terminateAs(t, api, task.ID, "m-stranger", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger on a closed task = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec, "forbidden", "caller is not the task's executor")
}
