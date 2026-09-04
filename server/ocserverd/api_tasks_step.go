package main

import "net/http"

// ── T-66 單一步驟讀取面 ──────────────────────────────────────────────────────
//
// GET /api/tasks/{task_id}/steps/{step_id} — MCP get_task_step.
//
// WHY IT EXISTS. Until this ticket the step working note (T-cc3e) rode EVERY
// response newTaskDTO builds: get_task, terminate, reassign, claim, duplicate,
// set_task_deps, the create dedupe hit, description and title — nine exits, each
// carrying a 4,000-rune-capped free-text field per step to callers that wanted
// one of those notes or none of them. The owner ruled (card rc-4c8065fb30a5)
// that the note comes off the shared projection entirely and the reader fetches
// it on demand, so this is the door it fetches through.
//
// WHAT IT ANSWERS. Exactly one step, in full. No task fields, no sibling steps:
// answering with the ticket's other fields "while we are here" would reinstate
// the cost this split exists to remove, one step at a time.
//
// AUTHZ IS DELIBERATELY THE READ FLOOR AND NOTHING MORE. routes.go declares
// principalMachine, the same floor GET /api/tasks/{task_id} sits on, and there
// is no executor guard here — because there is none there either. A step note
// was already readable by any authenticated principal through get_task; making
// the dedicated read STRICTER would not close anything (the wide door is the
// task view, not this one) and would only mean the note is unreachable through
// the one tool that is supposed to serve it. If that floor is ever wrong it is
// wrong for both faces and moves in routes.go for both.
//
// 🔴 THE STEP MUST BELONG TO THE TASK. A bare GetTaskStep(stepId) would serve
// any step in the database to anyone who could name a task — the task_id in the
// path would be decoration. The ownership check below is what makes the path
// mean what it reads like, and it answers 404 (not 403): a step that is not on
// this task is, from this task's point of view, absent. Pinned by
// TestGetTaskStep_ForeignStepIs404.
func (s *apiServer) HandleGetTaskStepApiTasksTaskIdStepsStepIdGet(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return
	}
	if step == nil || step.TaskID != t.ID {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	// The same read-time card join newTaskStepDTO takes, so the two faces of one
	// step cannot disagree about a bound card's live status.
	cardStatus := s.replyCardStatusesForSteps([]TaskStep{*step})
	writeJSON(w, http.StatusOK, newTaskStepDetailDTO(*step, cardStatus))
}
