package main

// task_transition_notices_t91_test.go — T-91「任務狀態變了，該知道的人不知道」.
//
// Four transitions, four owner rulings, and the rulings deliberately disagree
// with each other about the CHANNEL. Keeping them in one file is how the next
// reader sees that the disagreement is intentional rather than an inconsistency
// somebody should tidy up:
//
//	Q1 轉派  → on the TICKET (lock + reassigned_from ride the wake snapshot);
//	          the chat notice still goes out, demoted to a reminder.
//	Q2 轉派後 → the predecessor keeps ONE cell of authority (write the handover),
//	          and loses every other one. The owner refused the wide version.
//	Q3 被擋   → on the TICKET ONLY. No message, by explicit ruling.
//	Q4 關票   → a DURABLE MESSAGE, because 開機盤點 lists only tasks that have not
//	          ended — a closed ticket is absent from the list that Q3 relies on.
//
// 🔴 THESE TESTS PIN MECHANISM, NOT PROSE. Where a document is the subject, the
// assertion is on a machine identifier the agent must be handed (`claim_task`,
// `reassigning`, `lock`) or on a whole-document equality — never on a sentence,
// because these texts are owner-editable and are expected to be rewritten.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── shared fixtures ──────────────────────────────────────────────────────────

// t91Reassigned sets up the state every Q1/Q2 case needs: an ad-hoc task owned
// by m-old, handed to m-new by the OWNER (so the predecessor is not the caller
// — the case where the predecessor cannot possibly still be authorised by
// having made the call itself). Returns the re-read task row.
func t91Reassigned(t *testing.T, api *apiServer) Task {
	t.Helper()
	putActiveMember(t, api, "m-old", "Old", KindStaff)
	putActiveMember(t, api, "m-new", "New", KindStaff)
	task := createAdHocTask(t, api, "m-old")
	rec := reassign(t, api, task.ID, memberTarget("m-new"), wireOwnerID, "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign fixture must admit, got %d %s", rec.Code, rec.Body.String())
	}
	got, err := api.dal.GetTask(task.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read reassigned task: %v", err)
	}
	if got.Lock != TaskLockReassigning || got.ReassignedFrom != "m-old" {
		t.Fatalf("fixture must leave the task under the reassigning lock with the "+
			"predecessor stamped, got %+v", got)
	}
	return *got
}

// t91ResumeTasks reads one actor's wake snapshot and hands back its task rows
// as raw maps — raw so an ABSENT key and a zero value stay distinguishable,
// which is exactly the difference these projections are about.
func t91ResumeTasks(t *testing.T, api *apiServer, actor string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary for %s: %d %s", actor, rec.Code, rec.Body.String())
	}
	var snap struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode resume-summary: %v", err)
	}
	return snap.Tasks
}

// t91GetTask reads one task through the ordinary read face as the owner.
func t91GetTask(t *testing.T, api *apiServer, taskID string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, wireOwnerID, "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get_task %s: %d %s", taskID, rec.Code, rec.Body.String())
	}
	var dto map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return dto
}

// t91ChatTo returns every durable chat row addressed to one recipient.
func t91ChatTo(t *testing.T, api *apiServer, recipient string) []ChatMessage {
	t.Helper()
	all, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range all {
		if m.Recipient == recipient {
			out = append(out, m)
		}
	}
	return out
}

// ── Q1: the handover is on the ticket, not only in a message ────────────────

// The wake snapshot is the list an agent inventories at boot. Before T-91 it
// was the ONE task projection that dropped `lock` and `reassigned_from` — the
// full taskDTO and the light list row both carried them — so a task that had
// been handed to you looked exactly like a task you had been working on, and
// the only thing that said otherwise was a chat message posted once, at a
// moment you may well have been offline for.
func TestResumeSummaryTaskRowCarriesTheReassignHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)

	rows := t91ResumeTasks(t, api, "m-new")
	var row map[string]any
	for _, r := range rows {
		if r["id"] == task.ID {
			row = r
		}
	}
	if row == nil {
		t.Fatalf("the successor's wake snapshot must list the task it just "+
			"received; rows=%v", rows)
	}
	if row["lock"] != TaskLockReassigning {
		t.Fatalf("resume-summary task row must carry lock=%q so a handover is "+
			"visible at 開機盤點; got %v (row=%v)", TaskLockReassigning, row["lock"], row)
	}
	if row["reassigned_from"] != "m-old" {
		t.Fatalf("resume-summary task row must name the PREDECESSOR to hand over "+
			"with (reassigned_from), got %v (row=%v)", row["reassigned_from"], row)
	}
	if row["reassigned_from_kind"] != TaskExecutorMember {
		t.Fatalf("resume-summary task row must say HOW to resolve reassigned_from "+
			"(member vs outsource), got %v", row["reassigned_from_kind"])
	}
}

// The light list row already carried both fields before this ticket. Pinned
// here as the POSITIVE CONTROL for the test above: if it ever went red, the
// snapshot assertion would no longer be evidence that the wake path in
// particular was the gap.
func TestListTaskRowAlreadyCarriedTheReassignHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)

	rec := httptest.NewRecorder()
	api.HandleListTasksApiTasksGet(rec,
		taskReq(t, "GET", "/api/tasks", nil, wireOwnerID, "owner"),
		HandleListTasksApiTasksGetParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, r := range rows {
		if r["id"] != task.ID {
			continue
		}
		if r["lock"] != TaskLockReassigning || r["reassigned_from"] != "m-old" {
			t.Fatalf("the LIGHT LIST row was already carrying the hold before "+
				"T-91 and must keep doing so, got %v", r)
		}
		return
	}
	t.Fatalf("list did not contain the reassigned task")
}

// ONE DOCUMENT, BOTH IDENTITIES. The owner's generalisation requirement,
// verbatim: 「轉交給外包跟轉交給一個目前離線的正職應該要有同樣一套方法」. The staff
// fold and the outsource worker's boot context are built from the SAME
// boot_sequence seed (TestWorkerBootContextIsTheStaffFoldMinusThePersona pins
// that they are byte-subtractions of one another), so the takeover instruction
// must be reachable from both — and it must be reachable because it is in the
// shared document, not because someone wrote it twice.
//
// The assertion is on MACHINE IDENTIFIERS (`claim_task`, the `reassigning` lock
// value, the `reassigned_from` field name), never on the sentence around them:
// the prose is owner-editable, the tool name and the lock value are not.
func TestBootSequenceTellsBothIdentitiesToConfirmThenClaim(t *testing.T) {
	s := newWorkerTestServer(t)
	staff, err := s.buildBootContext("", nil)
	if err != nil || staff == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-t91", Codename: "T-91", Model: "opus",
			Effort: "high", Runtime: RuntimeClaude},
		Task{ID: "t-t91t91t91t9", Title: "任務", Priority: TaskPriorityHigh}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	for _, token := range []string{"claim_task", TaskLockReassigning, "reassigned_from"} {
		if !strings.Contains(staff.Context, token) {
			t.Fatalf("the 正職 boot document must hand the agent %q — a takeover "+
				"instruction that names no tool is not an instruction", token)
		}
		if !strings.Contains(worker, token) {
			t.Fatalf("the 外包 boot document must hand the worker %q through the "+
				"SAME shared document; writing a second copy for 外包 is what the "+
				"owner ruled out", token)
		}
	}
}

// ── Q2: the narrow door ─────────────────────────────────────────────────────

// t91StepOf submits a one-step plan as the task's CURRENT executor and returns
// the step id.
func t91StepOf(t *testing.T, api *apiServer, taskID, executor string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec, taskReq(t, "POST",
		"/api/tasks/"+taskID+"/plan", map[string]any{
			"steps": []map[string]any{{"name": "做事", "dod": "做完"}},
		}, executor, "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan as %s: %d %s", executor, rec.Code, rec.Body.String())
	}
	steps, err := api.dal.ListTaskSteps(taskID)
	if err != nil || len(steps) == 0 {
		t.Fatalf("read back steps: %v", err)
	}
	return steps[0].ID
}

// t91WriteNote posts a step note as sub and returns the recorder.
func t91WriteNote(t *testing.T, api *apiServer, taskID, stepID, sub, note string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/note",
			map[string]any{"note": note}, sub, "agent"), taskID, stepID)
	return rec
}

// The system ORDERS the predecessor to write a handover onto the ticket and,
// in the same transaction, re-points executor_id away from it — so every write
// face it was told to use answered 403. This is the one cell the owner opened.
func TestPredecessorMayStillWriteTheHandoverNoteUnderTheReassignHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)
	step := t91StepOf(t, api, task.ID, "m-new")

	if rec := t91WriteNote(t, api, task.ID, step, "m-old", "做到一半，下一步是 X"); rec.Code != http.StatusOK {
		t.Fatalf("the stamped PREDECESSOR must still be able to write the handover "+
			"step note while the task is under the reassigning lock — the notice it "+
			"was sent orders exactly this write; got %d %s", rec.Code, rec.Body.String())
	}
	steps, _ := api.dal.ListTaskSteps(task.ID)
	if len(steps) == 0 || steps[0].Note != "做到一半，下一步是 X" {
		t.Fatalf("the predecessor's handover note must actually be stored, got %+v", steps)
	}
	// The patch face shares the guard chain, so it opens with it.
	rec := httptest.NewRecorder()
	api.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/steps/"+step+"/note/patch",
			map[string]any{"edits": []map[string]any{{"old": "X", "new": "Y"}}},
			"m-old", "agent"), task.ID, step)
	if rec.Code != http.StatusOK {
		t.Fatalf("the anchor-patch note face must admit the predecessor on the "+
			"same terms as the wholesale face, got %d %s", rec.Code, rec.Body.String())
	}
}

// 🔴 THE OWNER REFUSED THE WIDE VERSION. He was offered "both sides fully
// authorised during the handover" and chose to open the 「寫交接」 cell alone, so
// 全域脈絡 §3.4 (交接完成前，不得讓兩個執行者同時推進同一份工作) is untouched.
// Every one of these is the predecessor trying to DRIVE the task, and every one
// must still be a flat 403.
//
// 🔴 THE CASE LIST IS THE PREDICATE'S OWN LIST. callerMayWriteHandover's comment
// enumerates the doors that stay shut — plan, step status, deps, priority,
// reassign, terminate, artifacts, closeout, the task's own text — and this table
// must cover ALL of them, because that comment is the only place the ruling is
// written down and a door named there but missing here can be opened without
// anything going red. Adding a name to that comment means adding a case here.
func TestPredecessorStaysLockedOutOfEveryOtherTaskWrite(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)
	step := t91StepOf(t, api, task.ID, "m-new")
	pred := "m-old"

	cases := []struct {
		what string
		call func() *httptest.ResponseRecorder
	}{
		{"submit_plan", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/plan", map[string]any{
					"steps": []map[string]any{{"name": "換掉", "dod": "換完"}},
				}, pred, "agent"), task.ID)
			return rec
		}},
		{"update_step_status", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
				taskReq(t, "POST", "/api/tasks/"+task.ID+"/steps/"+step+"/status",
					map[string]any{"status": StepStatusInProgress}, pred, "agent"),
				task.ID, step)
			return rec
		}},
		{"set_task_deps", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/deps", map[string]any{"blocked_by": []string{}},
				pred, "agent"), task.ID)
			return rec
		}},
		{"set_task_priority", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/priority", map[string]any{"priority": "high"},
				pred, "agent"), task.ID)
			return rec
		}},
		{"claim_task", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleClaimTaskApiTasksTaskIdClaimPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/claim", nil, pred, "agent"), task.ID)
			return rec
		}},
		{"update_task", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleUpdateTaskApiTasksTaskIdPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID, map[string]any{"title": "改標題"},
				pred, "agent"), task.ID)
			return rec
		}},
		// The four below complete the predicate's own list. They were the gap:
		// callerMayWriteHandover's comment named nine doors that must stay shut
		// and only five of them had a case here, so widening the predicate onto
		// terminate / closeout / artifacts / reassign was a silent change.
		{"terminate_task", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/terminate", nil, pred, "agent"), task.ID)
			return rec
		}},
		{"report_task_closeout", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(rec, taskReq(t,
				"POST", "/api/tasks/"+task.ID+"/closeout", map[string]any{}, pred,
				"agent"), task.ID)
			return rec
		}},
		{"add_task_artifact", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleAddTaskArtifactApiTasksTaskIdArtifactPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID+"/artifact",
				map[string]any{"kind": "link", "url": "https://x/pr/1"},
				pred, "agent"), task.ID)
			return rec
		}},
		{"remove_task_artifact", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(rec,
				taskReq(t, "DELETE", "/api/tasks/"+task.ID+"/artifact/ta-nope", nil,
					pred, "agent"), task.ID, "ta-nope")
			return rec
		}},
		// 🔴 THE TARGET HAS TO BE AN OUTSOURCE ONE. With a member target this case
		// is worthless as evidence: 正職授權矩陣 rule 7 refuses a member-kind
		// reassign from any non-admin caller regardless of the executor guard, so
		// the 403 arrives whether or not the handover exception has been widened
		// onto this route — measured, a mutant that swaps this handler's guard for
		// callerMayWriteHandover leaves the member-target case GREEN. 發包 is the
		// one reassign shape rule 7 lets a 一般正職 do on its own task, which makes
		// the executor guard the only thing standing between the predecessor and a
		// 200.
		{"reassign_task", func() *httptest.ResponseRecorder {
			return reassign(t, api, task.ID, map[string]any{
				"target": map[string]any{
					"kind": "outsource", "model": "sonnet", "effort": "high",
				},
			}, pred, "agent")
		}},
	}
	for _, c := range cases {
		if rec := c.call(); rec.Code != http.StatusForbidden {
			t.Fatalf("%s by the predecessor must stay a flat 403 — the owner opened "+
				"the 「寫交接」 cell and refused the wide version; got %d %s",
				c.what, rec.Code, rec.Body.String())
		}
	}
}

// The window is bounded by the LOCK, not by a clock: claim_task closes it, and
// nothing has to remember to.
func TestTheHandoverDoorClosesWhenTheSuccessorClaims(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)
	step := t91StepOf(t, api, task.ID, "m-new")

	rec := httptest.NewRecorder()
	api.HandleClaimTaskApiTasksTaskIdClaimPost(rec, taskReq(t, "POST",
		"/api/tasks/"+task.ID+"/claim", nil, "m-new", "agent"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("successor claim: %d %s", rec.Code, rec.Body.String())
	}
	if rec := t91WriteNote(t, api, task.ID, step, "m-old", "太遲了"); rec.Code != http.StatusForbidden {
		t.Fatalf("once the successor has claimed, the predecessor's handover door "+
			"must be shut again (403), got %d %s", rec.Code, rec.Body.String())
	}
}

// A member who was never this task's predecessor gets nothing from the lock.
func TestTheHandoverDoorAdmitsOnlyTheStampedPredecessor(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)
	step := t91StepOf(t, api, task.ID, "m-new")
	putActiveMember(t, api, "m-stranger", "Stranger", KindStaff)

	if rec := t91WriteNote(t, api, task.ID, step, "m-stranger", "路過"); rec.Code != http.StatusForbidden {
		t.Fatalf("the reassigning lock must open the note door for the STAMPED "+
			"predecessor only, got %d %s for a stranger", rec.Code, rec.Body.String())
	}
}

// ── Q3: the blocker's side is on the ticket, and is never a message ─────────

// t91Block makes blocked depend on blocker, as blocked's executor.
func t91Block(t *testing.T, api *apiServer, blocked, blocker, executor string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec, taskReq(t, "POST",
		"/api/tasks/"+blocked+"/deps",
		map[string]any{"blocked_by": []string{blocker}}, executor, "agent"), blocked)
	if rec.Code != http.StatusOK {
		t.Fatalf("set_task_deps: %d %s", rec.Code, rec.Body.String())
	}
}

// Before T-91 the blocking side had no channel of any kind: set_task_deps
// publishes the delta of the BLOCKED task only, so the person everyone is
// queued behind was told nothing, by any route.
func TestBlockerTicketNamesTheTasksWaitingOnIt(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")

	dto := t91GetTask(t, api, blocker.ID)
	raw, ok := dto["blocking"].([]any)
	if !ok {
		t.Fatalf("the blocker's ticket must carry `blocking` — the owner ruled this "+
			"side is written on the ticket and never messaged, so an absent field is "+
			"the whole failure; got %v", dto["blocking"])
	}
	if len(raw) != 1 {
		t.Fatalf("the blocker's ticket must name the 1 task waiting on it, got %d: %v",
			len(raw), raw)
	}
	entry, _ := raw[0].(map[string]any)
	if entry["id"] != waiter.ID {
		t.Fatalf("`blocking` must name WHICH ticket is waiting, got %v", entry)
	}
	if entry["title"] != waiter.Title {
		t.Fatalf("`blocking` must resolve the waiting ticket's display facts "+
			"(title), got %v", entry)
	}

	// A ticket that blocks nobody says so honestly, as [] — never absent, never
	// null: "nobody is waiting" and "this field does not exist" must not look
	// the same to the agent reading it.
	other := t91GetTask(t, api, waiter.ID)
	if got, ok := other["blocking"].([]any); !ok || len(got) != 0 {
		t.Fatalf("a ticket nobody waits on must answer blocking=[], got %v", other["blocking"])
	}
}

// The wake snapshot carries the same fact — the boot inventory is where the
// blocker's executor actually looks, and it is the only place they will look
// because Q3's ruling means nothing is sent.
func TestResumeSummaryCarriesTheBlockingIds(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")

	for _, row := range t91ResumeTasks(t, api, "m-blocker") {
		if row["id"] != blocker.ID {
			continue
		}
		ids, ok := row["blocking"].([]any)
		if !ok || len(ids) != 1 || ids[0] != waiter.ID {
			t.Fatalf("the wake snapshot's task row must carry the ids of the "+
				"tickets waiting on it (nothing is messaged, so this is the whole "+
				"delivery), got %v", row["blocking"])
		}
		return
	}
	t.Fatalf("the blocker's wake snapshot did not list its own task")
}

// 🔴 A CLOSED WAITER IS NOT A WAITER. Q3's whole delivery is this one field, so
// its VALUE is the deliverable, not merely its presence: the blocker's executor
// reads "3 tickets are waiting on me" and acts on it, and the only useful
// reading of that sentence is "3 tickets are STILL waiting". The dependency row
// survives the waiter being terminated (nothing rewrites blocked_by on close),
// so without the terminal filter in blockingTasksOf the count only ever grows
// and every ticket the executor was ever behind stays on the list forever —
// which is precisely the signal-quality problem this ticket exists to fix.
//
// Both faces are asserted because they are two projections of the one helper
// and each is somebody's only view: the ticket for the human, the wake snapshot
// for the agent at 開機盤點.
func TestBlockingSkipsWaitersThatHaveAlreadyClosed(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	live := createAdHocTask(t, api, "m-waiter")
	dead := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, live.ID, blocker.ID, "m-waiter")
	t91Block(t, api, dead.ID, blocker.ID, "m-waiter")

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+dead.ID+"/terminate", nil, "owner", "owner"),
		dead.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate the waiter: %d %s", rec.Code, rec.Body.String())
	}
	// Positive control for the whole test: the dependency edge must OUTLIVE the
	// close. If it did not, the assertions below would pass for a reason that has
	// nothing to do with the terminal filter.
	stillBlocked, err := api.dal.ListTasksBlockedBy(blocker.ID)
	if err != nil {
		t.Fatalf("read back the reverse dep edge: %v", err)
	}
	if len(stillBlocked) != 2 {
		t.Fatalf("closing a waiter must NOT delete its blocked_by row — this test "+
			"has no subject unless the terminated waiter is still on the edge; got %d",
			len(stillBlocked))
	}

	dto := t91GetTask(t, api, blocker.ID)
	raw, ok := dto["blocking"].([]any)
	if !ok {
		t.Fatalf("the blocker's ticket must carry `blocking`, got %v", dto["blocking"])
	}
	if len(raw) != 1 {
		t.Fatalf("`blocking` must name only the tickets STILL waiting — a closed "+
			"waiter is nobody's blocker and must be filtered out, or the field grows "+
			"monotonically and stops meaning anything; want 1, got %d: %v",
			len(raw), raw)
	}
	if entry, _ := raw[0].(map[string]any); entry["id"] != live.ID {
		t.Fatalf("`blocking` must keep the LIVE waiter (%s) and drop the terminated "+
			"one (%s), got %v", live.ID, dead.ID, raw[0])
	}

	for _, row := range t91ResumeTasks(t, api, "m-blocker") {
		if row["id"] != blocker.ID {
			continue
		}
		ids, ok := row["blocking"].([]any)
		if !ok || len(ids) != 1 || ids[0] != live.ID {
			t.Fatalf("the wake snapshot's blocking ids must exclude the terminated "+
				"waiter too — it is the same helper, and the agent's boot inventory is "+
				"the only place Q3's ruling lets it learn this; want [%s], got %v",
				live.ID, row["blocking"])
		}
		return
	}
	t.Fatalf("the blocker's wake snapshot did not list its own task")
}

// 🔴 THE OWNER RULED THIS ONE THE OPPOSITE WAY FROM Q4, ON PURPOSE: 只寫在票上，
// 不發訊息. A future reader who "completes" the feature by adding a notification
// here is reversing a decision, not filling a gap.
func TestBindingADependencySendsTheBlockerExecutorNothing(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")

	before := len(t91ChatTo(t, api, "m-blocker"))
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")
	if after := len(t91ChatTo(t, api, "m-blocker")); after != before {
		t.Fatalf("hanging a ticket off a blocker must send its executor NO message "+
			"(owner ruling: 只寫在票上，不發訊息); chat rows went %d → %d", before, after)
	}
}

// ── Q4: the close notice is durable, wider, and says who closed it ──────────

// The two removed gates, as a pure decision. Both asked "does this task have
// learnings worth folding into a manual?" — the wrong question for a notice
// whose real content is "your ticket is closed, every write you make now 409s".
func TestCloseNudgeNoLongerSkipsDuplicateOrAdHocTasks(t *testing.T) {
	for _, c := range []struct {
		what string
		task Task
	}{
		{"a DUPLICATED task", Task{ID: "t-dup", Status: TaskStatusDuplicated,
			TypeKey: "review-pr", ExecutorID: "m-x"}},
		{"an AD-HOC task (no type)", Task{ID: "t-adhoc", Status: TaskStatusDone,
			TypeKey: "", ExecutorID: "m-x"}},
	} {
		if decideTaskCloseNudge(c.task) == nil {
			t.Fatalf("%s must still nudge its executor: the executor of a closed "+
				"ticket needs to know it is closed regardless of whether there is a "+
				"manual to write learnings into", c.what)
		}
	}
	// The one gate that stays, and the reason it stays is addressing, not
	// judgement: an unassigned task has no recipient.
	if decideTaskCloseNudge(Task{ID: "t-none", Status: TaskStatusDone,
		TypeKey: "review-pr", ExecutorID: ""}) != nil {
		t.Fatalf("an UNASSIGNED task must stay silent — there is nobody to address")
	}
	// And an open task is not a close.
	if decideTaskCloseNudge(Task{ID: "t-open", Status: TaskStatusInProgress,
		TypeKey: "review-pr", ExecutorID: "m-x"}) != nil {
		t.Fatalf("a non-terminal task must not nudge")
	}
}

// 🔴 THE DELIVERY GUARANTEE IS THE ACCEPTANCE CRITERION, not the send. The old
// path was hub.PushDirected — at-most-once down a live SSE connection, with the
// function's own comment admitting "an offline executor simply misses the
// reminder". This asserts the notice survives the recipient being absent: it is
// a DURABLE row, addressed to the executor, and therefore readable at its next
// wake. Nothing here connects an SSE client, which is the point.
func TestTaskCloseNudgeIsADurableChatRowTheExecutorReadsAtItsNextWake(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-exec", "Exec", KindStaff)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec, taskReq(t, "POST",
		"/api/tasks/"+task.ID+"/terminate", nil, wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner terminate: %d %s", rec.Code, rec.Body.String())
	}

	rows := t91ChatTo(t, api, "m-exec")
	var notice *ChatMessage
	for i, m := range rows {
		if id, _ := m.Meta["task_id"].(string); id == task.ID {
			notice = &rows[i]
		}
	}
	if notice == nil {
		t.Fatalf("closing a task must leave a DURABLE chat row for its executor — "+
			"an SSE push reaches nobody who was offline when the task closed; "+
			"rows to m-exec = %d", len(rows))
	}
	if notice.Sender != wireSystemSender {
		t.Fatalf("the close notice is server-authored, got sender %q", notice.Sender)
	}
	// The durability proof: the row is in the store, so the wake path that
	// reads the store hands it back — with no live connection anywhere in this
	// test.
	found := false
	for _, m := range mustResumeChat(t, api, "m-exec") {
		if m.ID == notice.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the close notice must be folded into the executor's WAKE " +
			"snapshot — 'the server sent it' is not the guarantee this ticket asks " +
			"for; 'the recipient sees it at its next boot' is")
	}
}

// mustResumeChat returns the chat block of one actor's wake snapshot.
func mustResumeChat(t *testing.T, api *apiServer, actor string) []ChatMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	var snap struct {
		Chat []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"chat"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]ChatMessage, 0, len(snap.Chat))
	for _, c := range snap.Chat {
		out = append(out, ChatMessage{ID: c.ID, Body: c.Body})
	}
	return out
}

// "My last step report finished it" and "somebody terminated it under me" are
// opposite situations, and the notice used to render them identically. The
// closer is a DECLARED document variable so the sentence around it stays
// owner-editable — this asserts the value is carried and filled, not the words.
func TestTaskCloseNudgeNamesWhoClosedIt(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-exec", "Exec", KindStaff)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec, taskReq(t, "POST",
		"/api/tasks/"+task.ID+"/terminate", nil, wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner terminate: %d %s", rec.Code, rec.Body.String())
	}
	var notice *ChatMessage
	rows := t91ChatTo(t, api, "m-exec")
	for i, m := range rows {
		if id, _ := m.Meta["task_id"].(string); id == task.ID {
			notice = &rows[i]
		}
	}
	if notice == nil {
		t.Fatalf("no close notice was written")
	}
	if by, _ := notice.Meta["closed_by"].(string); by != wireOwnerID {
		t.Fatalf("the close notice must carry WHO closed the task as a field "+
			"(closed_by), got %v", notice.Meta["closed_by"])
	}
	if !strings.Contains(notice.Body, wireOwnerID) {
		t.Fatalf("the closer must reach the agent in the notice it READS, not only "+
			"in meta — the document declares {closed_by}; body=%q", notice.Body)
	}
	// The document is still the only source of the words: a kind that declares
	// a variable the code does not fill renders to "" and sends nothing, so the
	// non-empty body above is also the proof that the slot is wired.
	if strings.Contains(notice.Body, "{closed_by}") {
		t.Fatalf("the {closed_by} slot reached the agent unrendered: %q", notice.Body)
	}
}
