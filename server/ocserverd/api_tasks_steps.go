package main

import (
	"database/sql"
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
// window of the text-only doors does not widen them. This only refuses early:
// each door judges the task and its plan again inside the transaction that
// writes them (editSteps).
func (s *apiServer) resolveTaskForStepEdit(
	w http.ResponseWriter, r *http.Request, taskID string,
) bool {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return false
	}
	if !callerMayDriveTask(s.dal.GetMember, r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return false
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return false
	}
	return true
}

// editSteps runs one plan edit in one transaction: re-read and re-judge the
// task, hand edit the plan as it stands, rewrite the task from the result. edit
// runs inside the transaction and reaches the database only through tx.
func (s *apiServer) editSteps(
	r *http.Request, taskID string,
	edit func(tx *sql.Tx, steps []TaskStep) ([]TaskStep, error),
) (Task, []TaskStep, error) {
	now := nowSecs()
	var saved Task
	var stored []TaskStep
	var arrived bool
	err := s.dal.inTx(func(tx *sql.Tx) error {
		t, err := openTaskToDriveOn(tx, r, taskID)
		if err != nil {
			return err
		}
		steps, err := listTaskStepsOn(tx, t.ID)
		if err != nil {
			return err
		}
		if stored, err = edit(tx, steps); err != nil {
			return err
		}
		if arrived, err = persistDerivedTaskOn(tx, t, now); err != nil {
			return err
		}
		saved = *t
		return nil
	})
	if err != nil {
		return Task{}, nil, err
	}
	s.announceDerivedTask(saved, arrived, requestTrigger(r))
	return saved, stored, nil
}

func (s *apiServer) HandleInsertTaskStepApiTasksTaskIdStepsPost(
	w http.ResponseWriter, r *http.Request, taskId string,
) {
	var body TaskStepInsertDTO
	if !decodeJSONBodyRequired(w, r, &body, "name", "dod") {
		return
	}
	if !s.resolveTaskForStepEdit(w, r, taskId) {
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
	fresh := TaskStep{
		ID:            "ts-" + newHexID(12),
		Name:          name,
		DoD:           body.Dod,
		Status:        StepStatusPending,
		ParallelGroup: trimmedOrEmpty(body.ParallelGroup),
		IsGate:        body.IsGate != nil && *body.IsGate,
	}
	before := trimmedOrEmpty(body.BeforeStepId)
	t, stored, err := s.editSteps(r, taskId, func(tx *sql.Tx, steps []TaskStep) ([]TaskStep, error) {
		at, err := stepInsertPosition(steps, before, fresh)
		if err != nil {
			return nil, err
		}
		return insertTaskStepOn(tx, taskId, at, fresh)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepInsertReceiptDTO{
		TaskID: t.ID, StepID: fresh.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}

func stepInsertPosition(steps []TaskStep, before string, fresh TaskStep) (int, error) {
	at := len(steps)
	if before != "" {
		idx := -1
		for i, st := range steps {
			if st.ID == before {
				idx = i
				break
			}
		}
		if idx < 0 {
			return 0, refuseInTx(http.StatusNotFound, "step '"+before+"' not found")
		}
		if StepIsTerminal(steps[idx].Status) {
			return 0, refuseInTx(http.StatusConflict,
				"step '"+before+"' is already "+steps[idx].Status+
					" — a new step cannot be inserted ahead of finished work; "+
					"name an unfinished step, or omit before_step_id to append "+
					"at the end of the plan")
		}
		at = idx
	}
	// Only the introduced rows face the gate and one-lane checks, so a legacy group
	// already on the timeline never blocks an unrelated insert.
	timeline := make([]TaskStep, 0, len(steps)+1)
	timeline = append(timeline, steps[:at]...)
	timeline = append(timeline, fresh)
	timeline = append(timeline, steps[at:]...)
	if msg := ValidatePlanParallelShape(timeline, []TaskStep{fresh}); msg != "" {
		return 0, refuseInTx(http.StatusBadRequest, msg)
	}
	return at, nil
}

func (s *apiServer) HandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost(
	w http.ResponseWriter, r *http.Request, taskId, stepId string,
) {
	if !s.resolveTaskForStepEdit(w, r, taskId) {
		return
	}
	t, stored, err := s.editSteps(r, taskId, func(tx *sql.Tx, steps []TaskStep) ([]TaskStep, error) {
		if err := stepDeleteRefusal(steps, stepId); err != nil {
			return nil, err
		}
		return deleteTaskStepOn(tx, taskId, stepId)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepMutationReceiptDTO{
		TaskID: t.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}

func stepDeleteRefusal(steps []TaskStep, stepID string) error {
	idx := -1
	for i, st := range steps {
		if st.ID == stepID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return refuseInTx(http.StatusNotFound, "step '"+stepID+"' not found")
	}
	if StepIsTerminal(steps[idx].Status) {
		return refuseInTx(http.StatusConflict,
			"step '"+stepID+"' is already "+steps[idx].Status+
				" — finished steps are the record of what was done and cannot "+
				"be deleted")
	}
	// Same refusal and wording submit_plan gives a plan with no steps.
	if len(steps) == 1 {
		return refuseInTx(http.StatusBadRequest,
			"a plan must have at least one step")
	}
	remaining := make([]TaskStep, 0, len(steps)-1)
	remaining = append(remaining, steps[:idx]...)
	remaining = append(remaining, steps[idx+1:]...)
	if msg := ValidatePlanParallelShape(remaining, nil); msg != "" {
		return refuseInTx(http.StatusBadRequest, msg)
	}
	return nil
}

func (s *apiServer) HandleReorderTaskStepsApiTasksTaskIdStepsReorderPost(
	w http.ResponseWriter, r *http.Request, taskId string,
) {
	var body TaskStepReorderDTO
	if !decodeJSONBodyRequired(w, r, &body, "step_ids") {
		return
	}
	if !s.resolveTaskForStepEdit(w, r, taskId) {
		return
	}
	t, stored, err := s.editSteps(r, taskId, func(tx *sql.Tx, steps []TaskStep) ([]TaskStep, error) {
		orderedIDs, err := reorderedStepIDs(taskId, steps, body.StepIds)
		if err != nil {
			return nil, err
		}
		return reorderTaskStepsOn(tx, taskId, orderedIDs)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	done, total := TaskProgress(stored)
	writeJSON(w, http.StatusOK, taskStepMutationReceiptDTO{
		TaskID: t.ID, StepsTotal: len(stored),
		ProgressDone: done, ProgressTotal: total,
	})
}

func reorderedStepIDs(taskID string, steps []TaskStep, stepIDs []string) ([]string, error) {
	unfinished := map[string]bool{}
	for _, st := range steps {
		if !StepIsTerminal(st.Status) {
			unfinished[st.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, id := range stepIDs {
		switch {
		case seen[id]:
			return nil, refuseInTx(http.StatusBadRequest,
				"step '"+id+"' is listed twice in step_ids")
		case unfinished[id]:
			seen[id] = true
		default:
			for _, st := range steps {
				if st.ID == id {
					return nil, refuseInTx(http.StatusBadRequest,
						"step '"+id+"' is already "+st.Status+
							" — finished steps keep the position they already "+
							"hold and must not be listed in step_ids")
				}
			}
			return nil, refuseInTx(http.StatusBadRequest,
				"step '"+id+"' is not a step of task '"+taskID+"'")
		}
	}
	if len(seen) != len(unfinished) {
		missing := make([]string, 0, len(unfinished)-len(seen))
		for _, st := range steps {
			if unfinished[st.ID] && !seen[st.ID] {
				missing = append(missing, "'"+st.ID+"'")
			}
		}
		return nil, refuseInTx(http.StatusBadRequest,
			"step_ids must list every unfinished step of task '"+taskID+
				"' exactly once; missing: "+strings.Join(missing, ", "))
	}
	ordered := make([]TaskStep, len(steps))
	byID := map[string]TaskStep{}
	for _, st := range steps {
		byID[st.ID] = st
	}
	free := make([]int, 0, len(stepIDs))
	for i, st := range steps {
		if StepIsTerminal(st.Status) {
			ordered[i] = st
			continue
		}
		free = append(free, i)
	}
	for n, id := range stepIDs {
		ordered[free[n]] = byID[id]
	}
	if msg := ValidatePlanParallelShape(ordered, nil); msg != "" {
		return nil, refuseInTx(http.StatusBadRequest, msg)
	}
	orderedIDs := make([]string, len(ordered))
	for i, st := range ordered {
		orderedIDs[i] = st.ID
	}
	return orderedIDs, nil
}
