package main

import "net/http"

// GET /api/tasks/{task_id}/steps/{step_id} — MCP get_task_step. Serves exactly one
// step and no task fields: step notes came off the task projection by owner ruling
// (rc-4c8065fb30a5) and are fetched here on demand.
//
// Authz is deliberately the same read floor as GET /api/tasks/{task_id} (routes.go):
// a stricter rule here closes nothing while get_task serves the same note.
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
	// Same read-time card join as newTaskStepDTO, so the two faces of one step agree.
	cardStatus := s.replyCardStatusesForSteps([]TaskStep{*step})
	writeJSON(w, http.StatusOK, newTaskStepDetailDTO(*step, cardStatus, s.stepNoteCap()))
}
