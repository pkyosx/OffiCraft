package main

// api_tasks_reassign_test.go — the T-160e reassign action's server pins:
// target validation (member / outsource / junk), the reassigning handover
// state machine (only the NEW executor reports it back to in_progress), the
// waiting-card expiry + step fallback side effects, worker mint (outsource
// target) and dismissal (outsource source), the handover chat, and the SSE fan
// to BOTH the new audience and the old executor. Handlers are invoked
// directly (the admin_agent route gate lives on the route table — pinned by
// the route-row test below + the conformance auth matrix).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// putActiveMember inserts one active roster member (the reassign target pool).
func putActiveMember(t *testing.T, api *apiServer, id, name, kind string) {
	t.Helper()
	if err := api.dal.PutMember(Member{
		ID: id, Name: name, Kind: kind, Effort: "medium",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put member %s: %v", id, err)
	}
}

// reassign posts the reassign action as (sub, scope).
func reassign(t *testing.T, api *apiServer, taskID string, body map[string]any, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReassignTaskApiTasksTaskIdReassignPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/reassign", body, sub, scope),
		taskID)
	return rec
}

func memberTarget(memberID string) map[string]any {
	return map[string]any{"target": map[string]any{"kind": "member", "member_id": memberID}}
}

func TestReassignRouteRowIsAgentGatedAndMCPExposed(t *testing.T) {
	// ② the route now admits any agent (was admin_agent): an agent reassigns a
	// task it EXECUTES (the handler's executor guard), owner/admin any task; an
	// outsource target still passes the single 發包 gate. The tool stays on the
	// MCP surface (the assistant only lives there — an MCPExclude would void it).
	for _, spec := range defaultRouteSpecs() {
		if spec.Path != "/api/tasks/{task_id}/reassign" {
			continue
		}
		if spec.Method != "POST" || spec.Requires != principalAgent {
			t.Fatalf("reassign row must be POST + agent: %+v", spec)
		}
		if spec.MCPExclude || spec.MCPTool != "reassign_task" {
			t.Fatalf("reassign must surface as MCP tool reassign_task: %+v", spec)
		}
		return
	}
	t.Fatal("route table has no /api/tasks/{task_id}/reassign row")
}

// ② the route is agent-open now — the handler's executor guard is what keeps an
// agent to tasks it drives; owner/admin may drive any task.
func TestReassignAgentMayOnlyHandOverItsOwnTask(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	task := createAdHocTask(t, api, "m-exec")

	// A DIFFERENT agent (not the executor) is forbidden.
	if rec := reassign(t, api, task.ID, memberTarget("m-new"),
		"m-intruder", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-executor agent must be 403, got %d %s", rec.Code, rec.Body.String())
	}

	// The executor itself may hand its own task over to a member (no 發包 gate on
	// a member target).
	rec := reassign(t, api, task.ID, memberTarget("m-new"), "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("executor agent must hand over: %d %s", rec.Code, rec.Body.String())
	}
	bound, err := api.dal.GetTask(task.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read: %v", err)
	}
	if bound.ExecutorID != "m-new" || bound.Lock != TaskLockReassigning {
		t.Fatalf("task must re-point to m-new and enter the reassigning lock, got %+v", bound)
	}
	// The status stays DERIVED (T-9ca5) — a stepless task is not_started, never
	// the retired 'reassigning' status.
	if bound.Status != TaskStatusNotStarted {
		t.Fatalf("reassigned task status must be the derived not_started, got %s", bound.Status)
	}
}

// ④ a reassign-to-outsource is a 發包 and passes the SAME gate: a subordinate
// executor the policy does not name is denied before any handover side effect.
func TestReassignToOutsourceByUnauthorizedExecutorIsDenied(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-exec", "Exec", KindAssistant) // plain agent (no RoleKey)
	task := createAdHocTask(t, api, "m-exec")

	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "m-exec", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized 發包 via reassign must be 403, got %d %s", rec.Code, rec.Body.String())
	}
	// The gate denied BEFORE the handover: no worker minted, status untouched.
	workers, err := api.dal.ListOutsourceWorkers()
	if err != nil {
		t.Fatalf("list workers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("denied dispatch must mint nothing, got %d", len(workers))
	}
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.Lock == TaskLockReassigning {
		t.Fatalf("denied reassign must not enter the reassigning lock, got %+v", bound)
	}
}

func TestReassignMemberToMemberHandsOver(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	view := submitPlan(t, api, task.ID, "m-old", []map[string]any{
		{"name": "done step", "dod": "d"},
		{"name": "running step", "dod": "d"},
		{"name": "gate step", "dod": "d", "is_gate": true},
	})
	// Drive: step0 done, step1 in_progress, arm the gate on step2 (waiting).
	for _, move := range []map[string]any{
		{"id": view.Steps[0].ID, "status": "in_progress"},
		{"id": view.Steps[0].ID, "status": "done"},
		{"id": view.Steps[1].ID, "status": "in_progress"},
	} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": move["status"]},
				"m-old", "agent"), task.ID, move["id"].(string))
		if rec.Code != http.StatusOK {
			t.Fatalf("step drive: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "go?", "options": []string{"a", "b"},
		}, "m-old", "agent"), task.ID, view.Steps[2].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)

	// SSE listeners: the old executor must get the farewell fan.
	oldConn, _ := api.hub.Connect("m-old", "")
	newConn, _ := api.hub.Connect("m-new", "")

	rec = reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "member", "member_id": "m-new"},
		"note":   "分支在 kyle-160e",
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeBody[taskDTO](t, rec)
	// The reassigning hold is now a LOCK (T-9ca5); status stays DERIVED (done +
	// two pending → in_progress).
	if out.Lock != TaskLockReassigning || out.Status != TaskStatusInProgress ||
		out.ExecutorKind != TaskExecutorMember || out.ExecutorID != "m-new" {
		t.Fatalf("handed-over row wrong: %+v", out)
	}
	if out.DedupeKey != task.DedupeKey || out.TypeKey != task.TypeKey ||
		out.ID != task.ID {
		t.Fatalf("reassign must never touch identity: %+v", out)
	}

	// The waiting gate card expired (settled — replan will freeze the step).
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil || stored.Status != replyCardStatusExpired {
		t.Fatalf("waiting card must expire on reassign: %+v %v", stored, err)
	}
	// Steps: done kept; in_progress and the (released) gate step fall pending.
	steps, err := api.dal.ListTaskSteps(task.ID)
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	want := map[string]string{
		"done step":    StepStatusDone,
		"running step": StepStatusPending,
		"gate step":    StepStatusPending,
	}
	for _, st := range steps {
		if st.Status != want[st.Name] {
			t.Fatalf("step %q: want %s, got %s", st.Name, want[st.Name], st.Status)
		}
	}

	// Handover chat: one message to each side, task-linked in meta.
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var toOld, toNew *ChatMessage
	for i := range msgs {
		switch msgs[i].Recipient {
		case "m-old":
			toOld = &msgs[i]
		case "m-new":
			toNew = &msgs[i]
		}
	}
	if toOld == nil || !strings.Contains(toOld.Body, "已轉派給 Rei") ||
		!strings.Contains(toOld.Body, "交接") {
		t.Fatalf("old-executor handover message wrong: %+v", toOld)
	}
	if toNew == nil || !strings.Contains(toNew.Body, "接手了任務") ||
		!strings.Contains(toNew.Body, "reassigning") ||
		!strings.Contains(toNew.Body, "你的前任是 Ken") ||
		!strings.Contains(toNew.Body, "分支在 kyle-160e") {
		t.Fatalf("new-executor handover message wrong (predecessor + note must ride): %+v", toNew)
	}
	if toNew.Meta["task_id"] != task.ID {
		t.Fatalf("handover meta must carry the task linkage: %+v", toNew.Meta)
	}
	// T-ba04: both notices are SERVER-authored (never attributed to the owner).
	if toOld.Sender != wireSystemSender || toNew.Sender != wireSystemSender {
		t.Fatalf("handover messages must be sender=%q, got old=%q new=%q",
			wireSystemSender, toOld.Sender, toNew.Sender)
	}
	// T-ba04: the predecessor is stamped on the persisted task.
	if storedT, err := api.dal.GetTask(task.ID); err != nil || storedT == nil ||
		storedT.ReassignedFrom != "m-old" || storedT.ReassignedFromKind != TaskExecutorMember {
		t.Fatalf("predecessor stamp wrong: %+v %v", storedT, err)
	}

	// SSE fan: BOTH sides saw a task frame (the old executor left the row's
	// audience, so publishTask alone would silently drop them).
	sawTask := func(l *hubListener) bool {
		for {
			frame := l.pop()
			if frame == nil {
				return false
			}
			if strings.Contains(string(frame), `"topic":"task"`) ||
				strings.Contains(string(frame), `"topic": "task"`) {
				return true
			}
		}
	}
	if !sawTask(newConn) {
		t.Fatal("the new executor must receive the task delta")
	}
	if !sawTask(oldConn) {
		t.Fatal("the OLD executor must receive one explicit task delta")
	}
}

// T-35e0: a reassign-to-outsource no longer mints inline — it lands the task
// UNASSIGNED (executor_id=” + the outsource_target on the row) under the
// reassigning lock, and the scheduler mints the successor under the global cap.
func TestReassignToOutsourceLandsUnassignedTarget(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // hold the scheduler — assert the landing, then tick by hand
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")

	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{
			"kind": "outsource", "model": "sonnet", "effort": "high",
			"machine": "m-box",
		},
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeBody[taskDTO](t, rec)
	if out.Lock != TaskLockReassigning ||
		out.ExecutorKind != TaskExecutorOutsource || out.ExecutorID != "" {
		t.Fatalf("outsource hand-over must land unassigned + reassigning: %+v", out)
	}
	stored, err := api.dal.GetTask(task.ID)
	if err != nil || stored == nil {
		t.Fatalf("re-read: %v", err)
	}
	if stored.OutsourceModel != "sonnet" || stored.OutsourceEffort != "high" ||
		stored.OutsourceMachine != "m-box" {
		t.Fatalf("the outsource target must ride the task row: %+v", stored)
	}
	if n := liveWorkerCount(t, api); n != 0 {
		t.Fatalf("no worker may be minted at reassign time, got %d", n)
	}

	// The scheduler mints the successor from the target, under the global cap.
	api.runOutsourceTick(nowSecs())
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || !strings.HasPrefix(bound.ExecutorID, "ow-") {
		t.Fatalf("scheduler must mint + bind the successor: %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil {
		t.Fatalf("minted worker missing: %v %v", worker, err)
	}
	if worker.Model != "sonnet" || worker.Effort != "high" ||
		worker.TaskID != task.ID {
		t.Fatalf("worker must carry the target spec: %+v", worker)
	}
	if api.workerMachinePref[worker.ID] != "m-box" {
		t.Fatalf("machine preference must ride the target: %q",
			api.workerMachinePref[worker.ID])
	}
	if !strings.HasPrefix(worker.Codename, "S-") {
		t.Fatalf("codename must derive from the model: %q", worker.Codename)
	}
	// The reassigning lock survives the mint — cleared only by the successor's claim.
	if bound.Lock != TaskLockReassigning {
		t.Fatalf("the reassigning lock must survive the mint, got %q", bound.Lock)
	}
}

// T-35e0 RED/GREEN pin for the `|| t.Lock == TaskLockReassigning` arm of
// outsourceAwaitingAssignment: an IN-PROGRESS task (a done leaf keeps the derived
// status at in_progress even after the reassign resets the running step to
// pending) reassigned to outsource lands unassigned under the reassigning lock.
// Its status is in_progress, NOT not_started, so the status arm alone would MISS
// this successor slot — only the lock arm makes it mintable. Remove that term and
// this test must go red: the successor is never minted, a silent orphan.
// (TestReassignToOutsourceLandsUnassignedTarget covers the not_started arm — a
// bare reassign with no steps — which the status arm alone already catches.)
func TestReassignInProgressTaskToOutsourceMintsSuccessorUnderReassigningLock(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // hold the scheduler — reassign, then tick by hand
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	view := submitPlan(t, api, task.ID, "m-old", []map[string]any{
		{"name": "done step", "dod": "d"},
		{"name": "running step", "dod": "d"},
	})
	// Drive step0 → done, step1 → in_progress. The done leaf is the crux: it keeps
	// the derived status at in_progress even after reassign resets step1 to pending.
	for _, move := range []map[string]any{
		{"id": view.Steps[0].ID, "status": "in_progress"},
		{"id": view.Steps[0].ID, "status": "done"},
		{"id": view.Steps[1].ID, "status": "in_progress"},
	} {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": move["status"]},
				"m-old", "agent"), task.ID, move["id"].(string))
		if rec.Code != http.StatusOK {
			t.Fatalf("step drive: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeBody[taskDTO](t, rec)
	// The crux assertion: a done leaf keeps the reassigned task at in_progress (NOT
	// not_started), so the scheduler can find this successor slot ONLY via the lock.
	if out.Status != TaskStatusInProgress {
		t.Fatalf("a done leaf must keep the reassigned task at in_progress, got %q", out.Status)
	}
	if out.Lock != TaskLockReassigning ||
		out.ExecutorKind != TaskExecutorOutsource || out.ExecutorID != "" {
		t.Fatalf("in-progress outsource hand-over must land unassigned + reassigning: %+v", out)
	}

	// The scheduler must mint the successor — reachable ONLY through the lock arm
	// (status is in_progress, so the not_started arm cannot pick it up).
	api.runOutsourceTick(nowSecs())
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || !strings.HasPrefix(bound.ExecutorID, "ow-") {
		t.Fatalf("scheduler must mint the in-progress reassign successor via the reassigning lock: %+v", bound)
	}
	worker, err := api.dal.GetOutsourceWorker(bound.ExecutorID)
	if err != nil || worker == nil || worker.Model != "sonnet" || worker.TaskID != task.ID {
		t.Fatalf("minted successor must carry the target spec: %+v %v", worker, err)
	}
	// The lock survives the mint (cleared only by the successor's claim).
	if bound.Lock != TaskLockReassigning {
		t.Fatalf("the reassigning lock must survive the mint, got %q", bound.Lock)
	}
}

// bindOutsourceExecutor rebinds a task onto a hand-built live worker (the
// scheduler's shape) — the outsource-source setup shared by the deferred-
// dismiss pins.
func bindOutsourceExecutor(t *testing.T, api *apiServer, taskID, workerID, codename string) {
	t.Helper()
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: workerID, Codename: codename, Model: "sonnet", Effort: "medium",
		TaskID: taskID, Status: WorkerStatusActive, CreatedTS: nowSecs(),
	}); err != nil {
		t.Fatalf("put worker %s: %v", workerID, err)
	}
	stored, err := api.dal.GetTask(taskID)
	if err != nil || stored == nil {
		t.Fatalf("task: %v %v", stored, err)
	}
	stored.ExecutorKind = TaskExecutorOutsource
	stored.ExecutorID = workerID
	if err := api.dal.PutTask(*stored); err != nil {
		t.Fatalf("rebind %s: %v", workerID, err)
	}
}

// T-ba04: the outsource SOURCE is NO LONGER dismissed at reassign time — it
// stays live through the `reassigning` hold so the successor can hand over WITH
// it, and is fired only when the successor reports the takeover
// (reassigning→in_progress). This is the RED/GREEN pin the owner's design turns
// on: the pre-T-ba04 behaviour (release at reassign) would leave nobody alive to
// hand over with.
func TestReassignDefersOutsourceSourceDismissUntilTakeover(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	bindOutsourceExecutor(t, api, task.ID, "ow-live", "S-1")

	rec := reassign(t, api, task.ID, memberTarget("m-new"), "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	// The predecessor worker must STILL be live right after the reassign — the
	// dismiss is deferred to the takeover.
	worker, err := api.dal.GetOutsourceWorker("ow-live")
	if err != nil || worker == nil {
		t.Fatalf("worker row: %v %v", worker, err)
	}
	if worker.Status == WorkerStatusReleased {
		t.Fatalf("predecessor outsource worker must NOT be dismissed at reassign: %+v", worker)
	}
	// Predecessor is stamped (outsource kind), so the takeover knows whom to fire.
	stored, err := api.dal.GetTask(task.ID)
	if err != nil || stored == nil ||
		stored.ReassignedFrom != "ow-live" || stored.ReassignedFromKind != TaskExecutorOutsource {
		t.Fatalf("predecessor stamp wrong: %+v %v", stored, err)
	}

	// The successor (m-new) claims the task → NOW the predecessor is fired.
	if rec := claimTask(t, api, task.ID, "m-new"); rec.Code != http.StatusOK {
		t.Fatalf("takeover claim: %d %s", rec.Code, rec.Body.String())
	}
	worker, err = api.dal.GetOutsourceWorker("ow-live")
	if err != nil || worker == nil {
		t.Fatalf("worker row after takeover: %v %v", worker, err)
	}
	if worker.Status != WorkerStatusReleased {
		t.Fatalf("predecessor must be released once the successor takes over: %+v", worker)
	}
}

// T-ba04: the takeover dismiss fires the predecessor by its OWN worker id, not
// by task_id — an outsource→outsource takeover binds the NEW worker to the SAME
// task_id, so a by-task release would kill the successor too. This is the
// RED/GREEN pin for ReleaseWorkerByID.
func TestReassignOutsourceToOutsourceTakeoverSparesTheNewWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true // mint the successor by an explicit tick
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	bindOutsourceExecutor(t, api, task.ID, "ow-old", "S-1")

	// Reassign to outsource — the task lands unassigned; the scheduler mints the
	// fresh successor (on the same task_id) on the next tick.
	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "medium"},
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	api.runOutsourceTick(nowSecs())
	bound, _ := api.dal.GetTask(task.ID)
	newWorkerID := ""
	if bound != nil {
		newWorkerID = bound.ExecutorID
	}
	if newWorkerID == "ow-old" || !strings.HasPrefix(newWorkerID, "ow-") {
		t.Fatalf("a fresh successor worker must be minted, got %q", newWorkerID)
	}
	// Both live on the same task_id right after the mint.
	if w, _ := api.dal.GetOutsourceWorker("ow-old"); w == nil || w.Status == WorkerStatusReleased {
		t.Fatalf("predecessor must stay live pre-takeover")
	}

	// The NEW worker claims the takeover (it is the task's executor now).
	if rec := claimTask(t, api, task.ID, newWorkerID); rec.Code != http.StatusOK {
		t.Fatalf("outsource takeover claim: %d %s", rec.Code, rec.Body.String())
	}
	// Predecessor released, successor SPARED (the by-worker-id release must not
	// catch the new worker bound to the same task_id).
	if w, _ := api.dal.GetOutsourceWorker("ow-old"); w == nil || w.Status != WorkerStatusReleased {
		t.Fatalf("predecessor must be released on takeover")
	}
	if w, _ := api.dal.GetOutsourceWorker(newWorkerID); w == nil || w.Status == WorkerStatusReleased {
		t.Fatalf("the successor worker must NOT be released by the takeover dismiss: %+v", w)
	}
}

// T-35e0: reassigning to an outsource target still notifies the PREDECESSOR to
// hand over (the successor is minted later by the scheduler and learns the
// takeover via its boot context, so no chat is posted to it at reassign time).
func TestReassignToOutsourcePostsPredecessorHandoverNotice(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")

	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}

	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var toPredecessor *ChatMessage
	for i := range msgs {
		if msgs[i].Recipient == "m-old" {
			toPredecessor = &msgs[i]
		}
	}
	if toPredecessor == nil {
		t.Fatalf("the predecessor must receive a handover notice")
	}
	if toPredecessor.Sender != wireSystemSender {
		t.Fatalf("handover notice must be system-authored, got %q", toPredecessor.Sender)
	}
	if !strings.Contains(toPredecessor.Body, "轉派給") {
		t.Fatalf("handover notice must tell the predecessor to hand over: %q", toPredecessor.Body)
	}
}

func TestReassignTakeoverIsTheNewExecutorsAlone(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	if rec := reassign(t, api, task.ID, memberTarget("m-new"),
		"owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	// The takeover is the claim action now (T-9ca5). The old executor is no
	// longer the executor — its claim is a flat 403.
	if rec := claimTask(t, api, task.ID, "m-old"); rec.Code != http.StatusForbidden {
		t.Fatalf("old executor claim must 403: %d %s", rec.Code, rec.Body.String())
	}
	// Only the new executor may claim — it clears the reassigning lock; the
	// status stays DERIVED (a stepless task is not_started).
	rec := claimTask(t, api, task.ID, "m-new")
	if rec.Code != http.StatusOK {
		t.Fatalf("new executor claim: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeBody[taskDTO](t, rec)
	if out.Lock != TaskLockNone {
		t.Fatalf("claim must clear the reassigning lock: %+v", out)
	}
	if out.Status != TaskStatusNotStarted {
		t.Fatalf("claimed stepless task status must be the derived not_started: %+v", out)
	}
	// The lock is cleared — a second claim now 409s (no reassigning hold left).
	if rec := claimTask(t, api, task.ID, "m-new"); rec.Code != http.StatusConflict {
		t.Fatalf("claiming an unlocked task must 409: %d %s", rec.Code, rec.Body.String())
	}
}

// T-ba04 e2e: an outsource successor claims the handed-over task via
// get_my_task (assigned→active), sees the reassigning status + the stamped
// predecessor on the DTO it reads, then reports the takeover
// (reassigning→in_progress) itself. The whole "外包經 get_my_task 認領後翻狀態"
// path the pre-T-ba04 suite never exercised.
func TestReassignOutsourceSuccessorClaimsViaGetMyTaskThenTakesOver(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	task := createAdHocTask(t, api, "m-old")

	// Reassign the member's task to outsource — the scheduler mints the successor.
	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "medium"},
	}, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	api.runOutsourceTick(nowSecs())
	bound, _ := api.dal.GetTask(task.ID)
	if bound == nil || bound.ExecutorID == "" {
		t.Fatalf("scheduler must mint the successor: %+v", bound)
	}
	workerID := bound.ExecutorID

	// The worker claims via get_my_task: it must land assigned→active and the
	// task it reads must be `reassigning` with the predecessor stamped.
	claimRec := httptest.NewRecorder()
	api.HandleGetMyTaskApiSelfTaskGet(claimRec,
		taskReq(t, "GET", "/api/self/task", nil, workerID, "agent"))
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", claimRec.Code, claimRec.Body.String())
	}
	claimed := decodeBody[myTaskDTO](t, claimRec)
	if claimed.Task.Lock != TaskLockReassigning {
		t.Fatalf("claimed task must carry the reassigning lock: %+v", claimed.Task)
	}
	if claimed.Task.ReassignedFrom != "m-old" ||
		claimed.Task.ReassignedFromKind != TaskExecutorMember {
		t.Fatalf("claim DTO must carry the predecessor stamp: %+v", claimed.Task)
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w == nil || w.Status != WorkerStatusActive {
		t.Fatalf("first claim must flip assigned→active")
	}

	// The successor claims the takeover itself (only it may) — clearing the lock.
	if rec := claimTask(t, api, task.ID, workerID); rec.Code != http.StatusOK {
		t.Fatalf("takeover claim by outsource successor: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := api.dal.GetTask(task.ID)
	if stored == nil || stored.Lock != TaskLockNone {
		t.Fatalf("takeover claim must clear the reassigning lock: %+v", stored)
	}
}

func TestReassignClearsTheWaitingExternalReason(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	task := createAdHocTask(t, api, "m-old")
	view := submitPlan(t, api, task.ID, "m-old", []map[string]any{
		{"name": "integrate", "dod": "d"},
	})
	startFirstStep(t, api, task.ID, "m-old")
	// The step (and hence the derived task) parks in waiting_external with a reason.
	if rec := reportStepStatus(t, api, task.ID, view.Steps[0].ID, "m-old",
		"waiting_external", "等第三方開通"); rec.Code != http.StatusOK {
		t.Fatalf("waiting_external: %d %s", rec.Code, rec.Body.String())
	}
	rec := reassign(t, api, task.ID, memberTarget("m-new"), "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeBody[taskDTO](t, rec)
	// Reassign resets non-terminal steps to pending, so no step is waiting_external
	// and the derived display reason clears; the reassigning lock is set.
	if out.Lock != TaskLockReassigning || out.WaitingReason != "" {
		t.Fatalf("the old executor's waiting reason must clear under the reassigning lock: %+v", out)
	}
}

func TestReassignGuards(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	putActiveMember(t, api, "m-warden", "box", KindWarden)
	putActiveMember(t, api, "m-gone", "Gone", KindAssistant)
	if err := api.dal.PutMember(Member{
		ID: "m-gone", Name: "Gone", Kind: KindAssistant, Effort: "medium",
		RosterStatus: RosterStatusRemoved,
	}); err != nil {
		t.Fatalf("dismiss m-gone: %v", err)
	}
	// P7d fold: an outsource member row is never a 'member'-kind target —
	// outsource executors are minted fresh by the outsource arm.
	if err := api.dal.PutMember(Member{
		ID: "ow-guard", Name: "S-guard", Kind: KindOutsource, Effort: "medium",
		Codename: "S-guard", RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put outsource member: %v", err)
	}
	task := createAdHocTask(t, api, "m-old")

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing member_id", map[string]any{"target": map[string]any{"kind": "member"}}, 400},
		{"unknown member", memberTarget("m-nobody"), 400},
		{"dismissed member", memberTarget("m-gone"), 400},
		{"warden target", memberTarget("m-warden"), 400},
		{"outsource member target", memberTarget("ow-guard"), 400},
		{"same executor", memberTarget("m-old"), 409},
		{"junk kind", map[string]any{"target": map[string]any{"kind": "team"}}, 400},
		{"bad effort", map[string]any{"target": map[string]any{
			"kind": "outsource", "effort": "extreme"}}, 400},
	}
	for _, tc := range cases {
		if rec := reassign(t, api, task.ID, tc.body, "owner", "owner"); rec.Code != tc.want {
			t.Fatalf("%s: want %d, got %d %s", tc.name, tc.want, rec.Code, rec.Body.String())
		}
	}

	// Frozen task → 400 (unfreeze first).
	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"priority": "frozen"},
			"owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}
	if rec := reassign(t, api, task.ID, memberTarget("m-new"),
		"owner", "owner"); rec.Code != http.StatusBadRequest {
		t.Fatalf("frozen task must 400: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown task → 404; terminal task → 409.
	if rec := reassign(t, api, "t-none", memberTarget("m-new"),
		"owner", "owner"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task must 404: %d", rec.Code)
	}
	closed := createAdHocTask(t, api, "m-old")
	rec = httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), closed.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := reassign(t, api, closed.ID, memberTarget("m-new"),
		"owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task must 409: %d %s", rec.Code, rec.Body.String())
	}
}
