package main

// api_tasks_preclaim_creator_t52_test.go — T-52. A 發包票 is born with
// executor_id empty and stays empty until the scheduler binds a worker to it.
// Every task-driving write is gated on "be the executor, or be admin/owner",
// and the creator earns no standing from having created the task — so for the
// whole of that window NOBODY who is awake can fix a typo in the brief the
// contractor will read the moment it boots.
//
// 🔴 THE HALF THAT CARRIES THE TICKET IS THE CLOSING, NOT THE OPENING. Opening
// the doors passes against almost any implementation; what has to be pinned is
// that the widening ENDS at the instant a worker is bound, so the person who
// opened the ticket cannot rewrite the requirements out from under somebody who
// is already working to them. TestAssignedTaskShutsItsCreatorOutOfTheTextDoors
// is that assertion, and it is the one the mutant in the T-52 report reddens.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// unboundOutsourceTask creates a real 發包票 through create_task and returns it
// with the premise ASSERTED: executor_id empty, creator = the plain 正職 that
// opened it. The creator is deliberately NOT an admin — an admin would clear
// callerMayDriveTask on its own and every case below would pass against a
// handler that never learned about creators at all.
func unboundOutsourceTask(t *testing.T, api *apiServer, creator string) taskDTO {
	t.Helper()
	api.noOutsource = true
	putMemberRow(t, api, creator, KindAssistant, "")
	if err := api.dal.PutTaskManual(TaskManual{
		TypeKey:  "t52-brief",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"sonnet"}`,
	}); err != nil {
		t.Fatalf("seed manual: %v", err)
	}
	rec := createTaskAs(t, api, map[string]any{
		"title": "brief with a typo", "type_key": "t52-brief"}, creator, "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("create 發包票: %d %s", rec.Code, rec.Body.String())
	}
	task := decodeBody[taskCreateResultDTO](t, rec).Task
	if task.ExecutorKind != TaskExecutorOutsource || task.ExecutorID != "" {
		t.Fatalf("fixture broken: a 發包票 must be born unassigned, got %+v", task)
	}
	if got := readTask(t, api, task.ID).CreatorID; got != creator {
		t.Fatalf("fixture broken: creator = %q, want %q", got, creator)
	}
	return task
}

// bindExecutor puts a worker on the task the way the scheduler does — the one
// column this whole ticket turns on. It is written straight onto the row on
// purpose: the assertion is about the FIELD, not about which code path set it,
// and the scheduler's own assign loop is covered by outsource_sched_test.go.
func bindExecutor(t *testing.T, api *apiServer, taskID, workerID string) {
	t.Helper()
	putActiveMember(t, api, workerID, "Bound worker", KindOutsource)
	stored, err := api.dal.GetTask(taskID)
	if err != nil || stored == nil {
		t.Fatalf("reload task: %v", err)
	}
	stored.ExecutorID = workerID
	if err := api.dal.PutTask(*stored); err != nil {
		t.Fatalf("bind executor: %v", err)
	}
	if got := readTask(t, api, taskID).ExecutorID; got != workerID {
		t.Fatalf("fixture broken: executor = %q, want %q", got, workerID)
	}
}

// seedStep puts one step on a task without going through submit_plan, which is
// itself executor-gated and therefore unreachable while the task is unbound.
func seedStep(t *testing.T, api *apiServer, taskID, stepID string) {
	t.Helper()
	if err := api.dal.PutTaskStep(TaskStep{
		ID: stepID, TaskID: taskID, OrderIdx: 0, Name: "step one",
		Status: StepStatusPending, Note: "anchor line",
	}); err != nil {
		t.Fatalf("seed step: %v", err)
	}
}

// textDoor is one of the nine doors owner opened (2026-09-02, rc-1bb6e01c4bf7):
// 改描述／標題／產物增刪／步驟筆記, and the two document-history restores that
// put earlier text back. Each returns the response its caller received —
// the whole response and not just the code, because a refusal has to be
// checked for its REASON (executorGuardRefusal) and not merely for 403.
type textDoor struct {
	name string
	call func(t *testing.T, api *apiServer, task taskDTO, stepID, caller string) *httptest.ResponseRecorder
}

// textDoors is the enumeration the two halves share, so the open case and the
// shut case can never drift into testing different doors.
func textDoors() []textDoor {
	return []textDoor{
		{"update_task title+description", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleUpdateTaskApiTasksTaskIdPost(rec, taskReq(t, "POST",
				"/api/tasks/"+task.ID,
				map[string]any{"title": "corrected " + caller, "description": "corrected body"},
				caller, "agent"), task.ID)
			return rec
		}},
		{"update_task_description route", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			return writeTaskDescription(t, api, task.ID, caller, "agent",
				map[string]any{"description": "route description " + caller})
		}},
		{"update_task_title route", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			return postTaskTitle(t, api, task.ID, caller, "agent",
				map[string]any{"title": "route title " + caller})
		}},
		{"add_task_artifact", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			return addArtifact(t, api, task.ID, map[string]any{
				"kind": "link", "url": "https://example.invalid/" + caller}, caller, "agent")
		}},
		{"remove_task_artifact", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			// Removing needs something to remove, and only a permitted caller can
			// put it there — so the owner seeds it and the door under test is the
			// DELETE alone.
			rec := addArtifact(t, api, task.ID, map[string]any{
				"kind": "link", "url": "https://example.invalid/seed"}, "owner", "owner")
			if rec.Code != http.StatusOK {
				t.Fatalf("seed artifact: %d %s", rec.Code, rec.Body.String())
			}
			arts := getTaskView(t, api, task.ID).Artifacts
			if len(arts) == 0 {
				t.Fatal("seed artifact did not land")
			}
			return removeArtifact(t, api, task.ID, arts[len(arts)-1].ID, caller, "agent")
		}},
		{"update_step_note", func(t *testing.T, api *apiServer, task taskDTO, stepID, caller string) *httptest.ResponseRecorder {
			return writeStepNote(t, api, task.ID, stepID, caller, "note from "+caller)
		}},
		{"patch_step_note", func(t *testing.T, api *apiServer, task taskDTO, stepID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(rec,
				taskReq(t, "POST", "/api/tasks/"+task.ID+"/steps/"+stepID+"/note/patch",
					map[string]any{"edits": []any{edit("anchor line", "patched by "+caller)}},
					caller, "agent"), task.ID, stepID)
			return rec
		}},
		{"restore task_description", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			return restoreTaskText(t, api, docKindTaskDescription, task.ID, caller)
		}},
		{"restore task_title", func(t *testing.T, api *apiServer, task taskDTO, _, caller string) *httptest.ResponseRecorder {
			return restoreTaskText(t, api, docKindTaskTitle, task.ID, caller)
		}},
	}
}

// restoreTaskText restores the newest retained revision of one text kind, or
// fails the test when there is none to restore — an empty history would make
// the door untestable rather than green.
func restoreTaskText(t *testing.T, api *apiServer, kind, taskID, caller string) *httptest.ResponseRecorder {
	t.Helper()
	var rows []historyRow
	if kind == docKindTaskTitle {
		rows = listTaskTitleHistory(t, api, taskID, "owner", "owner")
	} else {
		rows = historyRowsFrom(t, api, kind, taskID, "owner", "owner",
			listTaskDescriptionHistory(t, api, taskID, "owner", "owner"))
	}
	if len(rows) == 0 {
		t.Fatalf("fixture broken: %s has no retained revision to restore", kind)
	}
	rec := httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		taskReq(t, "POST", "/api/document-history/"+kind+"/"+taskID+"/x/restore",
			nil, caller, "agent"),
		kind, taskID, rows[0].Id)
	return rec
}

// seedTextHistory makes both restore doors reachable: a revision only exists
// once the text has actually changed once. The owner writes it, so the history
// is not itself produced by the widening under test.
func seedTextHistory(t *testing.T, api *apiServer, taskID string) {
	t.Helper()
	// TWO description writes: the first, from a blank, retains nothing — a
	// single write would leave the restore door with no revision to aim at.
	for _, text := range []string{"seeded description", "seeded description v2"} {
		if got := writeTaskDescription(t, api, taskID, "owner", "owner",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("seed description %q: %d", text, got)
		}
	}
	if got := postTaskTitle(t, api, taskID, "owner", "owner",
		map[string]any{"title": "seeded title"}).Code; got != http.StatusOK {
		t.Fatalf("seed title: %d", got)
	}
}

// TestUnassignedTaskAdmitsItsCreatorAtTheTextDoors — DoD 1. While executor_id
// is empty the creator counts as the executor at the nine text doors.
func TestUnassignedTaskAdmitsItsCreatorAtTheTextDoors(t *testing.T) {
	for _, door := range textDoors() {
		t.Run(door.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := unboundOutsourceTask(t, api, "m-creator")
			seedStep(t, api, task.ID, "st-1")
			seedTextHistory(t, api, task.ID)
			if rec := door.call(t, api, task, "st-1", "m-creator"); rec.Code != http.StatusOK {
				t.Fatalf("creator at %s = %d %s, want 200", door.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAssignedTaskShutsItsCreatorOutOfTheTextDoors — DoD 2, and the reason this
// ticket is not "add a permission". The ONLY difference from the test above is
// that a worker is now bound; the creator is the same person and every door
// must be a flat 403 again. Delete the `t.ExecutorID != ""` arm of
// callerMayEditTaskText and this test reddens on all nine subtests.
func TestAssignedTaskShutsItsCreatorOutOfTheTextDoors(t *testing.T) {
	for _, door := range textDoors() {
		t.Run(door.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := unboundOutsourceTask(t, api, "m-creator")
			seedStep(t, api, task.ID, "st-1")
			seedTextHistory(t, api, task.ID)
			bindExecutor(t, api, task.ID, "ow-bound")
			rec := door.call(t, api, task, "st-1", "m-creator")
			if rec.Code != http.StatusForbidden ||
				!strings.Contains(rec.Body.String(), executorGuardRefusal) {
				t.Fatalf("creator at %s after assignment = %d %s, want 403 %q",
					door.name, rec.Code, rec.Body.String(), executorGuardRefusal)
			}
		})
	}
}

// TestUnassignedTaskStillRefusesAThirdParty — the negative control. The
// widening must not degrade into "an unclaimed task is editable by anyone":
// a member who is neither creator nor executor is refused while executor_id is
// empty, and refused for the stated reason rather than merely with some 4xx.
func TestUnassignedTaskStillRefusesAThirdParty(t *testing.T) {
	for _, door := range textDoors() {
		t.Run(door.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := unboundOutsourceTask(t, api, "m-creator")
			putMemberRow(t, api, "m-stranger", KindAssistant, "")
			seedStep(t, api, task.ID, "st-1")
			seedTextHistory(t, api, task.ID)
			rec := door.call(t, api, task, "st-1", "m-stranger")
			if rec.Code != http.StatusForbidden ||
				!strings.Contains(rec.Body.String(), executorGuardRefusal) {
				t.Fatalf("stranger at %s = %d %s, want 403 %q",
					door.name, rec.Code, rec.Body.String(), executorGuardRefusal)
			}
		})
	}
}

// TestUnassignedTaskCreatorReachesNoTaskDrivingDoor — the other half of the
// 射程. Owner opened 改文字類 and named what stays shut: 凍結/priority, 撤票,
// 改派, claim, mark_duplicate, plan, step status, closeout, deps. Each of these
// still runs on callerMayDriveTask (terminate on callerMayTerminateTask, which
// re-derives the same executor identity), so a creator on an unbound task is
// refused. Without this, a later hand could swap the widened predicate into any
// of them and nothing in the package would notice.
//
// 🔴 WHY EACH CELL ASSERTS THE REFUSAL SENTENCE AND NOT JUST 403. Measured
// 2026-09-02: this test used to send the reassign door a MEMBER target aimed at
// the caller itself, and swapping that door's guard to callerMayEditTaskText
// left the cell GREEN — the widened guard admitted the creator, and the 正職
// 授權矩陣 rule 7 ("only owner/admin may hand a task to another 正職") produced
// its own 403 one step later. The cell was measuring rule 7, not the guard.
// Two things fix it and both are load-bearing: the reassign cell now sends an
// OUTSOURCE target, which is the one reassign a plain 正職 is allowed to make,
// so nothing downstream stands in for the guard; and every cell pins the
// sentence, so a 403 arriving from any other rule can no longer pass for this
// one.
func TestUnassignedTaskCreatorReachesNoTaskDrivingDoor(t *testing.T) {
	shut := []struct {
		name string
		call func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder
	}{
		{"set_task_priority", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/priority", map[string]any{"priority": "frozen"},
				caller, "agent"), taskID)
			return rec
		}},
		{"terminate_task", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/terminate", nil, caller, "agent"), taskID)
			return rec
		}},
		{"reassign_task", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			// 發包, not 轉派 to a colleague: a member target would be refused by
			// the 正職授權矩陣 regardless of this door's guard (see the note
			// above), which is exactly what made this cell a false green.
			return reassign(t, api, taskID, map[string]any{
				"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "medium"},
			}, caller, "agent")
		}},
		{"claim_task", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleClaimTaskApiTasksTaskIdClaimPost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/claim", nil, caller, "agent"), taskID)
			return rec
		}},
		{"mark_duplicate", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/duplicate", map[string]any{"duplicate_of": "t-other"},
				caller, "agent"), taskID)
			return rec
		}},
		{"submit_plan", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSubmitTaskPlanApiTasksTaskIdPlanPost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/plan",
				map[string]any{"steps": []map[string]any{{"name": "one"}}},
				caller, "agent"), taskID)
			return rec
		}},
		{"update_step_status", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
				taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/st-1/status",
					map[string]any{"status": "in_progress"}, caller, "agent"), taskID, "st-1")
			return rec
		}},
		{"report_task_closeout", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/closeout", nil, caller, "agent"), taskID)
			return rec
		}},
		{"set_task_deps", func(t *testing.T, api *apiServer, taskID, caller string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec, taskReq(t, "POST",
				"/api/tasks/"+taskID+"/deps", map[string]any{"blocked_by": []string{}},
				caller, "agent"), taskID)
			return rec
		}},
	}
	for _, door := range shut {
		t.Run(door.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := unboundOutsourceTask(t, api, "m-creator")
			seedStep(t, api, task.ID, "st-1")
			rec := door.call(t, api, task.ID, "m-creator")
			if rec.Code != http.StatusForbidden ||
				!strings.Contains(rec.Body.String(), executorGuardRefusal) {
				t.Fatalf("creator at %s = %d %s, want 403 %q (this door stays shut)",
					door.name, rec.Code, rec.Body.String(), executorGuardRefusal)
			}
		})
	}
}
