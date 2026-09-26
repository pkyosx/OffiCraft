package main

import (
	"net/http"
	"strings"
)

// Single-step plan edits (T-228): insert_step, delete_step, reorder_steps.
//
// submit_plan is a WHOLESALE replace — it deletes every unfinished row and its
// working note, and re-listing a node by name mints a NEW step with a new id.
// That made a mid-task adjustment cost the executor everything already under
// way: the notes on the unfinished steps, and the gate step still waiting for
// the owner's answer (a deleted step's card never restores anything, even when
// it is answered later). T-211 is the case on the record — an outsource worker
// at 27/28 was told to fold one more piece of work in before the acceptance
// step and could not, so the work went into a note and the plan stopped
// describing the actual path.
//
// These three writes move ONE thing. Every other step keeps its id, its status,
// its note and its bound reply card, which is the entire point of having them
// beside submit_plan rather than instead of it.
//
// ⚠️ NO OVERWRITE PROTECTION, by owner ruling (rc-5160b97384c4, 2026-09-16 —
// 「什麼都不加，最後寫的人為準」): no version, no compare-and-set, no retry. Two
// writes landing together leave the later one standing and the earlier one
// silently gone. The tool descriptions say so in as many words. Adding a guard
// here needs a new ruling, not a judgement call.

// resolveTaskForStepEdit runs the three gates every single-step plan edit shares
// — the task exists, the caller drives it, the task is not closed — and hands
// back the task with its stored step rows in timeline order.
//
// callerMayDriveTask verbatim, deliberately: these are plan writes, not the
// text-only doors, so the T-52 executor-less-creator window does not widen them.
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

// POST /api/tasks/{task_id}/steps — insert ONE step in front of a named step,
// or at the end when none is named (MCP insert_step).
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
	// Same two quality gates a submitted plan carries, same wording: a step with
	// no name is unreadable and a step with no Definition of Done is
	// unverifiable. Kept identical so the rule reads the same whichever door a
	// step arrives through.
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
	// Where it lands. An unnamed anchor appends; a named one must be a step of
	// THIS task that has not finished — a terminal row is immutable history, and
	// inserting ahead of one would rewrite the record of what was already done.
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
	// Validate the shape of the timeline as it will be STORED — with the new row
	// in the position it is actually going to occupy, which for an insert is the
	// middle. Rules 1 and 3 apply to the new row alone, so a legacy group
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

// POST /api/tasks/{task_id}/steps/{step_id}/delete — remove ONE unfinished step
// (MCP delete_step).
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
	// Same refusal submit_plan gives a plan with no steps in it, same wording: a
	// planned task cannot be emptied back into 規劃中.
	if len(steps) == 1 {
		writeError(w, http.StatusBadRequest,
			"a plan must have at least one step")
		return
	}
	remaining := make([]TaskStep, 0, len(steps)-1)
	remaining = append(remaining, steps[:idx]...)
	remaining = append(remaining, steps[idx+1:]...)
	// Nothing is introduced, so only the contiguity rule can fire — deleting a
	// step from between two lanes of one group would split it. A group left with
	// a single lane is NOT refused, the same way submit_plan tolerates a legacy
	// one-lane group it did not introduce.
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

// POST /api/tasks/{task_id}/steps/reorder — reorder the UNFINISHED steps
// (MCP reorder_steps).
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
	// step_ids must be EXACTLY the unfinished set: every unfinished step named
	// once, nothing else. A partial list is refused rather than interpreted,
	// because every interpretation of "the ones you left out" is a guess about
	// where they should go, and the caller is the one who knows.
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
			// One message for "not on this task" and "already finished" would
			// hide which of the two it is, and they need different fixes.
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
	// Finished steps keep the timeline positions they already hold; the
	// unfinished ones fill what is left, in the order given.
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
	// Nothing is introduced, so only the contiguity rule can fire: a reorder
	// that pulls one lane of a parallel group away from its siblings would
	// render as two stages, which is the visual lie the rule exists to refuse.
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
