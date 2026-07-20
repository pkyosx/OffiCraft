package main

// outsource_gate_test.go — 節點9 the single spawn gate (④⑦) and the create_task
// 發包 path (①) after T-23cf (廢發包白名單): the gate admits ANY authenticated
// initiator (owner/admin, or any principal with a member row) — cost is bounded
// by the scheduler's global parallel cap, not per-agent authorization. The only
// deny left is an unauthenticated identity (non-owner sub with no member row).
// An admitted create dispatch LANDS AN UNASSIGNED outsource task carrying its
// target — the scheduler mints it under the global cap. No approval card, no
// pending status, no side door to an immediate spawn.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTaskAs posts a create_task request as (sub, scope) and returns the recorder.
func createTaskAs(t *testing.T, api *apiServer, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks", body, sub, scope))
	return rec
}

func liveWorkerCount(t *testing.T, api *apiServer) int {
	t.Helper()
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	return len(workers)
}

func TestOutsourceSpawnGate(t *testing.T) {
	dev := &Member{ID: "m-dev", RoleKey: "dev"}
	assistant := &Member{ID: "m-asst", RoleKey: adminRoleKey}

	cases := []struct {
		name      string
		class     string
		initiator *Member
		want      outsourceGateDecision
	}{
		// T-23cf: authentication is the only gate — any member row admits, no
		// whitelist, no per-task card.
		{"owner admits", principalOwner, nil, gateAdmitSpawn},
		{"admin admits", principalAdminAgent, assistant, gateAdmitSpawn},
		{"plain member admits without any whitelist", principalAgent, dev, gateAdmitSpawn},
		{"nil non-owner initiator is denied", principalAgent, nil, gateDeny},
		{"machine with no member row is denied", principalMachine, nil, gateDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			issuedBy := "owner"
			if tc.initiator != nil {
				issuedBy = tc.initiator.ID
			}
			res, err := api.outsourceSpawnGate(outsourceGateRequest{
				PrincipalClass: tc.class, Initiator: tc.initiator,
				TaskID: "t-x", Model: "sonnet", Effort: "high", IssuedBy: issuedBy,
			})
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if res.Decision != tc.want {
				t.Fatalf("decision = %q, want %q (reason %q)", res.Decision, tc.want, res.Reason)
			}
		})
	}
}

// Any hired subordinate's create dispatch is authorized (T-23cf: no whitelist)
// and lands an UNASSIGNED outsource task carrying its target — no approval
// card, no pending status, no worker (the scheduler mints it later under the
// cap).
func TestCreateTaskOutsourceDispatchLandsUnassignedTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // the scheduler is pinned separately — assert the landing
	if err := api.dal.PutMember(Member{
		ID: "m-dev", Name: "Dev", Kind: KindAssistant, RoleKey: "dev",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed initiator: %v", err)
	}

	rec := createTaskAs(t, api, map[string]any{
		"title":  "review the PR",
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "m-dev", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("create dispatch: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[taskCreateResultDTO](t, rec).Task
	if created.Status != TaskStatusNotStarted {
		t.Fatalf("dispatch must land not_started (no pending status), got %q", created.Status)
	}

	stored, err := api.dal.GetTask(created.ID)
	if err != nil || stored == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if stored.ExecutorKind != TaskExecutorOutsource || stored.ExecutorID != "" {
		t.Fatalf("dispatch must land an unassigned outsource task, got %+v", stored)
	}
	if stored.OutsourceModel != "sonnet" || stored.OutsourceEffort != "high" ||
		stored.OutsourceMachine != "auto" {
		t.Fatalf("the outsource target must ride the task row, got %+v", stored)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("no worker may be minted at dispatch time, got %d", n)
	}
	// No approval card — the whole point of退場ing the gate.
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("dispatch must open no approval card, got %d", len(cards))
	}
}

// An unauthenticated identity — a non-owner sub with NO member row (warden
// tokens, outsource workers) — is the one deny left after T-23cf.
func TestCreateTaskOutsourceDispatchDeniesUnauthenticatedInitiatorLeavingNoTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	// "ow-ghost" is deliberately NOT hired — no member row behind the sub.

	rec := createTaskAs(t, api, map[string]any{
		"title":  "sneaky dispatch",
		"target": map[string]any{"kind": "outsource", "model": "sonnet"},
	}, "ow-ghost", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated 發包 must be 403, got %d %s", rec.Code, rec.Body.String())
	}

	// ③ atomicity: a denied dispatch leaves NO orphan task and no worker.
	tasks, err := api.dal.ListTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("denied dispatch must persist no task, got %d", len(tasks))
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("denied dispatch must mint nothing, got %d", n)
	}
}

// ⑦ the owner's OWN dispatch traverses the same gate (no back door) and — like
// every admit now — lands unassigned for the scheduler, never an immediate spawn.
func TestCreateTaskOutsourceDispatchOwnerAdmitsThroughTheGate(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	rec := createTaskAs(t, api, map[string]any{
		"title":  "owner dispatch",
		"target": map[string]any{"kind": "outsource", "model": "opus", "effort": "medium"},
	}, wireOwnerID, "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner dispatch: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[taskCreateResultDTO](t, rec).Task
	if created.Status != TaskStatusNotStarted || created.ExecutorKind != TaskExecutorOutsource {
		t.Fatalf("admitted dispatch must land an outsource track, got %+v", created)
	}
	stored, _ := api.dal.GetTask(created.ID)
	if stored == nil || stored.ExecutorID != "" ||
		stored.OutsourceModel != "opus" || stored.OutsourceEffort != "medium" {
		t.Fatalf("owner admit must land unassigned + target, got %+v", stored)
	}
	// No immediate spawn side door, no approval card.
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("owner admit must not spawn inline, got %d workers", n)
	}
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("admitted dispatch must open no card, got %d", len(cards))
	}
}
