package main

// outsource_gate_test.go — 節點9 the single spawn gate (④⑦) and its two
// verdicts on the create_task 發包 path (①): the pure choke's admit/pending/deny
// decision table, and the create_task dispatch integration (pending lands the
// approval card + parks the intent + spawns NOTHING; owner's own dispatch admits
// through the SAME gate; an unauthorized initiator is denied with no orphan task).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// setDefaultPolicy overwrites the seeded global-default delegation policy.
func setDefaultPolicy(t *testing.T, api *apiServer, needsCard bool, roles, members []string) {
	t.Helper()
	if err := api.dal.PutDelegationPolicy(DelegationPolicy{
		PrincipalID:      delegationPolicyDefaultID,
		AllowedRoles:     roles,
		AllowedMembers:   members,
		NeedsPerTaskCard: needsCard,
	}); err != nil {
		t.Fatalf("put default policy: %v", err)
	}
}

// createTaskAs posts a create_task request as (sub, scope) and returns the recorder.
func createTaskAs(t *testing.T, api *apiServer, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks", body, sub, scope))
	return rec
}

// armPendingDispatch sets up a whitelisted subordinate (m-dev) under the
// per-task-card default, dispatches a create_task outsource target, and returns
// the parked task + its live owner-approval card.
func armPendingDispatch(t *testing.T, api *apiServer) (taskDTO, ReplyCard) {
	t.Helper()
	if m, _ := api.dal.GetMember("m-dev"); m == nil {
		if err := api.dal.PutMember(Member{
			ID: "m-dev", Name: "Dev", Kind: KindAssistant, RoleKey: "dev",
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed initiator: %v", err)
		}
	}
	setDefaultPolicy(t, api, true, nil, []string{"m-dev"})
	rec := createTaskAs(t, api, map[string]any{
		"title":  "review the PR",
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "m-dev", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("arm pending: %d %s", rec.Code, rec.Body.String())
	}
	task := decodeBody[taskCreateResultDTO](t, rec).Task
	if task.Status != TaskStatusPendingOutsourceApproval {
		t.Fatalf("task must park pending, got %q", task.Status)
	}
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	for _, c := range cards {
		if c.TaskID == task.ID && c.Status == replyCardStatusWaiting {
			return task, c
		}
	}
	t.Fatalf("no waiting approval card for task %s", task.ID)
	return taskDTO{}, ReplyCard{}
}

func liveWorkerCount(t *testing.T, api *apiServer) int {
	t.Helper()
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	return len(workers)
}

func TestOutsourceApprovalApproveSpawnsExactlyOnce(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, card := armPendingDispatch(t, api)

	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": outsourceApproveOptionIdx}); rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}

	// Exactly one worker minted + bound; task left pending, intent consumed.
	if n := liveWorkerCount(t, api); n != 1 {
		t.Fatalf("approve must mint exactly one worker, got %d", n)
	}
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if bound.Status != TaskStatusNotStarted || bound.ExecutorKind != TaskExecutorOutsource || bound.ExecutorID == "" {
		t.Fatalf("approved task must land not_started + bound outsource, got %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil || worker.Model != "sonnet" || worker.Effort != "high" {
		t.Fatalf("minted worker must mirror the intent: %+v (%v)", worker, err)
	}
	if intent, _ := api.dal.GetOutsourceIntent(task.ID); intent != nil {
		t.Fatalf("intent must be consumed on approve, got %+v", intent)
	}
}

func TestOutsourceApprovalIsIdempotentAcrossReplayedSettle(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, card := armPendingDispatch(t, api)
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("get card: %v", err)
	}

	// A replayed settle (concurrent/duplicate approve) must spawn only once: the
	// consumed intent is the exactly-once guard.
	for i := 0; i < 3; i++ {
		if err := api.settleOutsourceApproval(*stored, true, nowSecs(), "test"); err != nil {
			t.Fatalf("settle %d: %v", i, err)
		}
	}
	if n := liveWorkerCount(t, api); n != 1 {
		t.Fatalf("replayed approve must mint exactly one worker, got %d", n)
	}
	_ = task
}

func TestOutsourceApprovalOnNonPendingTaskIsNoop(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	_, card := armPendingDispatch(t, api)
	stored, _ := api.dal.GetReplyCard(card.ID)

	// The dispatch is cancelled first (task leaves pending, intent consumed).
	if err := api.settleOutsourceApproval(*stored, false, nowSecs(), "test"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// A late approval of the same card is a CAS no-op — nothing spawns.
	if err := api.settleOutsourceApproval(*stored, true, nowSecs(), "test"); err != nil {
		t.Fatalf("late approve: %v", err)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("late approve on a non-pending task must spawn nothing, got %d", n)
	}
}

func TestOutsourceApprovalCancelReturnsTaskToNotStarted(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, card := armPendingDispatch(t, api)

	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": outsourceCancelOptionIdx}); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.Status != TaskStatusNotStarted {
		t.Fatalf("cancelled dispatch must return to not_started, got %+v", bound)
	}
	if bound.ExecutorID != "" {
		t.Fatalf("cancelled dispatch must not bind an executor, got %+v", bound)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("cancel must spawn nothing, got %d", n)
	}
	if intent, _ := api.dal.GetOutsourceIntent(task.ID); intent != nil {
		t.Fatalf("intent must be invalidated on cancel, got %+v", intent)
	}
}

func TestOutsourceApprovalTTLExpiryReturnsTaskToNotStarted(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, card := armPendingDispatch(t, api)

	rec := httptest.NewRecorder()
	api.HandleExpireReplyCardApiReplyCardsCardIdExpirePost(rec,
		taskReq(t, "POST", "/api/reply-cards/"+card.ID+"/expire", nil, wireOwnerID, "owner"), card.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.Status != TaskStatusNotStarted {
		t.Fatalf("expired approval card must return the task to not_started, got %+v", bound)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("TTL expiry must spawn nothing, got %d", n)
	}
	if intent, _ := api.dal.GetOutsourceIntent(task.ID); intent != nil {
		t.Fatalf("intent must be invalidated on expiry, got %+v", intent)
	}
}

func TestOutsourceApprovalReassignMemberDuringPendingBlocksLateApproval(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	task, card := armPendingDispatch(t, api)

	// The owner reassigns the pending task to a MEMBER — it leaves pending, the
	// approval card expires, the intent is invalidated (⑧).
	if rec := reassign(t, api, task.ID, memberTarget("m-new"), wireOwnerID, "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign member: %d %s", rec.Code, rec.Body.String())
	}
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.ExecutorID != "m-new" || bound.Lock != TaskLockReassigning {
		t.Fatalf("task must re-point to m-new + reassigning lock, got %+v", bound)
	}
	if intent, _ := api.dal.GetOutsourceIntent(task.ID); intent != nil {
		t.Fatalf("reassign must invalidate the pending intent, got %+v", intent)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("member reassign must spawn no outsource worker, got %d", n)
	}
	// A late answer of the (now expired) approval card is refused, spawns nothing.
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": outsourceApproveOptionIdx}); rec.Code != http.StatusConflict {
		t.Fatalf("late answer of an expired card must 409, got %d %s", rec.Code, rec.Body.String())
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("late approval must not spawn, got %d", n)
	}
}

func TestOutsourceApprovalSpawnsFreshAfterInitiatorLeaves(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, card := armPendingDispatch(t, api)

	// The dispatching agent leaves the roster — the parked intent is independent
	// of the initiator (⑪ orphan take-over), so the owner can still approve.
	if err := api.dal.PutMember(Member{
		ID: "m-dev", Name: "Dev", Kind: KindAssistant, RoleKey: "dev",
		RosterStatus: RosterStatusRemoved,
	}); err != nil {
		t.Fatalf("retire initiator: %v", err)
	}
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": outsourceApproveOptionIdx}); rec.Code != http.StatusOK {
		t.Fatalf("owner approve after initiator left: %d %s", rec.Code, rec.Body.String())
	}
	if n := liveWorkerCount(t, api); n != 1 {
		t.Fatalf("owner approval must still spawn a fresh worker, got %d", n)
	}
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.ExecutorID == "" {
		t.Fatalf("approved task must bind a fresh worker, got %+v", bound)
	}
}

func TestOutsourceSpawnGate(t *testing.T) {
	whitelisted := &Member{ID: "m-dev", RoleKey: "dev"}
	plain := &Member{ID: "m-plain", RoleKey: "dev"}
	assistant := &Member{ID: "m-asst", RoleKey: adminRoleKey}

	cases := []struct {
		name      string
		class     string
		initiator *Member
		needsCard bool
		members   []string
		want      outsourceGateDecision
	}{
		{"owner admits even when a card is required", principalOwner, nil, true, nil, gateAdmitSpawn},
		{"admin admits even when a card is required", principalAdminAgent, assistant, true, nil, gateAdmitSpawn},
		{"whitelisted agent pends when a card is required", principalAgent, whitelisted, true, []string{"m-dev"}, gatePending},
		{"whitelisted agent admits when no card is required", principalAgent, whitelisted, false, []string{"m-dev"}, gateAdmitSpawn},
		{"unlisted agent is denied", principalAgent, plain, true, []string{"m-dev"}, gateDeny},
		{"nil non-owner initiator is denied", principalAgent, nil, true, nil, gateDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			setDefaultPolicy(t, api, tc.needsCard, nil, tc.members)
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

func TestCreateTaskOutsourceDispatchPendsAndOpensCard(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // no scheduler race — the gate alone decides
	// A whitelisted subordinate: authorized to 發包 but the default per-task-card
	// posture parks the dispatch for owner approval.
	if err := api.dal.PutMember(Member{
		ID: "m-dev", Name: "Dev", Kind: KindAssistant, RoleKey: "dev",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed initiator: %v", err)
	}
	setDefaultPolicy(t, api, true, nil, []string{"m-dev"})

	rec := createTaskAs(t, api, map[string]any{
		"title":  "review the PR",
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "m-dev", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("create dispatch: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[taskCreateResultDTO](t, rec).Task
	if created.Status != TaskStatusPendingOutsourceApproval {
		t.Fatalf("task must park pending approval, got %q", created.Status)
	}

	// No worker was minted — the whole point of the gate.
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("pending dispatch must spawn nothing, got %d workers", len(workers))
	}

	// An owner approval card opened, bound to the task.
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	var card *ReplyCard
	for i := range cards {
		if cards[i].TaskID == created.ID {
			card = &cards[i]
		}
	}
	if card == nil || card.Status != replyCardStatusWaiting {
		t.Fatalf("a waiting owner card must be bound to the task, got %+v", card)
	}

	// The dispatch intent is parked for the Pass-2 approve handler.
	intent, err := api.dal.GetOutsourceIntent(created.ID)
	if err != nil || intent == nil {
		t.Fatalf("intent must be persisted: %v %v", intent, err)
	}
	if intent.Model != "sonnet" || intent.Effort != "high" || intent.IssuedBy != "m-dev" {
		t.Fatalf("intent must carry the dispatch target + initiator: %+v", intent)
	}
}

func TestCreateTaskOutsourceDispatchDeniesUnauthorizedInitiatorLeavingNoTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	if err := api.dal.PutMember(Member{
		ID: "m-plain", Name: "Plain", Kind: KindAssistant, RoleKey: "dev",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed initiator: %v", err)
	}
	// Default policy names nobody — deny-by-default for a subordinate.

	rec := createTaskAs(t, api, map[string]any{
		"title":  "sneaky dispatch",
		"target": map[string]any{"kind": "outsource", "model": "sonnet"},
	}, "m-plain", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized 發包 must be 403, got %d %s", rec.Code, rec.Body.String())
	}

	// ③ atomicity: a denied dispatch leaves NO orphan task and no worker.
	tasks, err := api.dal.ListTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("denied dispatch must persist no task, got %d", len(tasks))
	}
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("denied dispatch must mint nothing, got %d", len(workers))
	}
}

func TestCreateTaskOutsourceDispatchOwnerAdmitsThroughTheGate(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	// ⑦ the owner's OWN dispatch traverses the same gate (no back door): the
	// per-task card default still stands, yet the owner is a standing approver so
	// the dispatch admits and a worker is minted+bound (no pending, no card).
	rec := createTaskAs(t, api, map[string]any{
		"title":  "owner dispatch",
		"target": map[string]any{"kind": "outsource", "model": "opus", "effort": "medium"},
	}, wireOwnerID, "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner dispatch: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[taskCreateResultDTO](t, rec).Task
	if created.Status != TaskStatusNotStarted || created.ExecutorKind != TaskExecutorOutsource {
		t.Fatalf("admitted dispatch must bind an outsource executor, got %+v", created)
	}
	if created.ExecutorID == "" {
		t.Fatalf("admitted dispatch must mint + bind a worker, got %+v", created)
	}
	worker, err := api.dal.GetOutsourceWorker(created.ExecutorID)
	if err != nil || worker == nil {
		t.Fatalf("minted worker missing: %v", err)
	}
	if worker.Model != "opus" || worker.Effort != "medium" || worker.TaskID != created.ID {
		t.Fatalf("worker must mirror the dispatch target: %+v", worker)
	}
	// No approval card for an admitted dispatch.
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("admitted dispatch must open no card, got %d", len(cards))
	}
}
