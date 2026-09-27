package main

import (
	"net/http"
	"strings"
)

// Single-step plan edits: insert_step, delete_step, reorder_steps. They exist beside
// submit_plan because submit_plan is a wholesale replace (unfinished rows and their
// notes deleted, ids re-minted, a deleted step's card restores nothing); these move
// ONE step, and every other step keeps its id, status, note and bound reply card.
//
// ⚠️ No overwrite protection (last writer wins) — owner ruling rc-5160b97384c4.

// callerMayDriveTask verbatim: these are plan writes, so the executor-less-creator
// window of the text-only doors does not widen them.
func (s *apiServer) resolveTaskForStepEdit(
	w http.ResponseWriter, r *http.Request, taskID string,
) (*Task, []TaskStep, bool) {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return nil, nil, false
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return nil, nil, false
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return nil, nil, false
	}
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return nil, nil, false
	}
	return t, steps, true
}

func (s *apiServer) HandleInsertTaskStepApiTasksTaskIdStepsPost(
	w http.ResponseWriter, r *http.Request, taskId string,
) {
	var body TaskStepInsertDTO
	if !decodeJSONBodyRequired(w, r, &body, "name", "dod") {
		return
	}
	t, steps, ok := s.resolveTaskForStepEdit(w, r, taskId)
	if !ok {
		return
	}
	// Same two quality gates and wording as submit_plan.
	name := trimString(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "step name must not be blank")
		return
	}
	if strings.TrimSpace(body.Dod) == "" {
		writeError(w, http.StatusBadRequest,
			"step '"+name+"' must have a non-empty definition of done")
		return
	}
	at := len(steps)
	if before := trimmedOrEmpty(body.BeforeStepId); before != "" {
		idx := -1
		for i, st := range steps {
			if st.ID == before {
				idx = i
				break
			}
		}
		if idx < 0 {
			writeError(w, http.StatusNotFound, "step '"+before+"' not found")
			return
		}
		if StepIsTerminal(steps[idx].Status) {
			writeError(w, http.StatusConflict,
				"step '"+before+"' is already "+steps[idx].Status+
					" — a new step cannot be inserted ahead of finished work; "+
					"name an unfinished step, or omit before_step_id to append "+
					"at the end of the plan")
			return
		}
		at = idx
	}
	fresh := TaskStep{
		ID:            "ts-" + newHexID(12),
		Name:          name,
		DoD:           body.Dod,
		Status:        StepStatusPending,
		ParallelGroup: trimmedOrEmpty(body.ParallelGroup),
		IsGate:        body.IsGate != nil && *body.IsGate,
	}
	// Only the introduced rows face the gate and one-lane checks, so a legacy group
	// already on the timeline never blocks an unrelated insert.
	timeline := make([]TaskStep, 0, len(steps)+1)
	timeline = append(timeline, steps[:at]...)
	timeline = append(timeline, fresh)
	timeline = append(timeline, steps[at:]...)
	if msg := ValidatePlanParallelShape(timeline, []TaskStep{fresh}); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	stored, err := s.dal.InsertTaskStep(t.ID, at, fresh)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.deriveAndPersistTask(t, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepInsertReceiptDTO{
		TaskID: t.ID, StepID: fresh.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}

func (s *apiServer) HandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost(
	w http.ResponseWriter, r *http.Request, taskId, stepId string,
) {
	t, steps, ok := s.resolveTaskForStepEdit(w, r, taskId)
	if !ok {
		return
	}
	idx := -1
	for i, st := range steps {
		if st.ID == stepId {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	if StepIsTerminal(steps[idx].Status) {
		writeError(w, http.StatusConflict,
			"step '"+stepId+"' is already "+steps[idx].Status+
				" — finished steps are the record of what was done and cannot "+
				"be deleted")
		return
	}
	// Same refusal and wording submit_plan gives a plan with no steps.
	if len(steps) == 1 {
		writeError(w, http.StatusBadRequest,
			"a plan must have at least one step")
		return
	}
	remaining := make([]TaskStep, 0, len(steps)-1)
	remaining = append(remaining, steps[:idx]...)
	remaining = append(remaining, steps[idx+1:]...)
	if msg := ValidatePlanParallelShape(remaining, nil); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	stored, err := s.dal.DeleteTaskStep(t.ID, stepId)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.deriveAndPersistTask(t, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepMutationReceiptDTO{
		TaskID: t.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}

func (s *apiServer) HandleReorderTaskStepsApiTasksTaskIdStepsReorderPost(
	w http.ResponseWriter, r *http.Request, taskId string,
) {
	var body TaskStepReorderDTO
	if !decodeJSONBodyRequired(w, r, &body, "step_ids") {
		return
	}
	t, steps, ok := s.resolveTaskForStepEdit(w, r, taskId)
	if !ok {
		return
	}
	unfinished := map[string]bool{}
	for _, st := range steps {
		if !StepIsTerminal(st.Status) {
			unfinished[st.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, id := range body.StepIds {
		switch {
		case seen[id]:
			writeError(w, http.StatusBadRequest,
				"step '"+id+"' is listed twice in step_ids")
			return
		case unfinished[id]:
			seen[id] = true
		default:
			found := false
			for _, st := range steps {
				if st.ID == id {
					found = true
					writeError(w, http.StatusBadRequest,
						"step '"+id+"' is already "+st.Status+
							" — finished steps keep the position they already "+
							"hold and must not be listed in step_ids")
					break
				}
			}
			if !found {
				writeError(w, http.StatusBadRequest,
					"step '"+id+"' is not a step of task '"+t.ID+"'")
			}
			return
		}
	}
	if len(seen) != len(unfinished) {
		missing := make([]string, 0, len(unfinished)-len(seen))
		for _, st := range steps {
			if unfinished[st.ID] && !seen[st.ID] {
				missing = append(missing, "'"+st.ID+"'")
			}
		}
		writeError(w, http.StatusBadRequest,
			"step_ids must list every unfinished step of task '"+t.ID+
				"' exactly once; missing: "+strings.Join(missing, ", "))
		return
	}
	ordered := make([]TaskStep, len(steps))
	byID := map[string]TaskStep{}
	for _, st := range steps {
		byID[st.ID] = st
	}
	free := make([]int, 0, len(body.StepIds))
	for i, st := range steps {
		if StepIsTerminal(st.Status) {
			ordered[i] = st
			continue
		}
		free = append(free, i)
	}
	for n, id := range body.StepIds {
		ordered[free[n]] = byID[id]
	}
	if msg := ValidatePlanParallelShape(ordered, nil); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	orderedIDs := make([]string, len(ordered))
	for i, st := range ordered {
		orderedIDs[i] = st.ID
	}
	stored, err := s.dal.ReorderTaskSteps(t.ID, orderedIDs)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.deriveAndPersistTask(t, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepMutationReceiptDTO{
		TaskID: t.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}
