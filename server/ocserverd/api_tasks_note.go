package main

import (
	"net/http"
	"strconv"
	"unicode/utf8"
)

// Step notes (T-cc3e): the handover SOP's "把還在進行中的工作寫回 task step note"
// needs a place to land; the owner ruled (rc-15cf8df7cb7f, option 2) that the
// STEP layer gets its own note rather than folding it into the task
// description. Its own endpoint and MCP tool, not a parameter on
// update_step_status (charter §14: intent-per-tool).
func (s *apiServer) HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNoteUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "note") {
		return
	}
	note := trimString(body.Note)
	if !s.stepNoteWithinLimit(w, note) {
		return
	}
	t, step, ok := s.resolveStepForNoteWrite(w, r, taskId, stepId)
	if !ok {
		return
	}
	if !s.storeStepNote(w, r, t, step, note, true) {
		return
	}
	writeJSON(w, http.StatusOK, taskStepNoteReceiptDTO{
		TaskID: t.ID, StepID: step.ID, StepStatus: step.Status,
		SizeChars: utf8.RuneCountInString(step.Note), CapChars: s.stepNoteCap(),
		Sha256: receiptSha256(step.Note),
	})
}

// The patch face exists for CONCURRENT OVERWRITE: at a handover two sessions
// write the same note, and a wholesale write from a stale (usually longer) copy
// deletes the other's text with no guard firing. Sending only {old, new} makes
// "overwrite from an old base" inexpressible; a moved anchor becomes a 400.
// Still open: the read (read pool) and write (write pool) share no transaction,
// so two interleaving patch requests lose one edit silently. The ceiling is
// applied to the RESULT of the patch.
func (s *apiServer) HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNotePatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	t, step, ok := s.resolveStepForNoteWrite(w, r, taskId, stepId)
	if !ok {
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	// "get_task_step", not get_task: since T-66 get_task carries only the note's
	// SIZE, and an anchor-miss pointing there would re-anchor against nothing.
	next, applied, err := ApplyDocEdits(step.Note, edits, "get_task_step")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	if !allowShrink && LessonsShrinkBlocked(step.Note, next) {
		writeError(w, http.StatusBadRequest,
			"patch would empty (or shrink to under a tenth of) the step note — pass allow_shrink=true if this is intended, or use update_step_note; nothing was written")
		return
	}
	if !s.stepNoteWithinLimit(w, next) {
		return
	}
	// Announce only when the text changed — NOT `applied > 0`, which counts edits
	// that moved an intermediate result (self-cancelling edits report non-zero).
	// The write itself still happens (see storeStepNote). The wholesale face
	// keeps its unconditional delta.
	if !s.storeStepNote(w, r, t, step, next, next != step.Note) {
		return
	}
	writeJSON(w, http.StatusOK, taskStepNotePatchResultDTO{
		TaskID:       t.ID,
		StepID:       step.ID,
		StepStatus:   step.Status,
		AppliedEdits: applied,
		SizeChars:    utf8.RuneCountInString(next),
		CapChars:     s.stepNoteCap(),
		Sha256:       receiptSha256(next),
	})
}

// stepNoteWithinLimit: the ceiling is the task.step_note_cap_chars setting
// (owner ruling rc-c8cc527bfed3); the handover note and chat bodies deliberately
// keep the fixed constant. Enforced only on write, so a lowered cap leaves
// longer stored notes readable but not editable until shortened.
func (s *apiServer) stepNoteWithinLimit(w http.ResponseWriter, note string) bool {
	limit := s.stepNoteCap()
	if n := utf8.RuneCountInString(note); n > limit {
		writeError(w, http.StatusBadRequest, "step note is "+strconv.Itoa(n)+
			" chars, over the "+strconv.Itoa(limit)+"-char limit")
		return false
	}
	return true
}

func (s *apiServer) resolveStepForNoteWrite(w http.ResponseWriter, r *http.Request, taskId, stepId string) (*Task, *TaskStep, bool) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return nil, nil, false
	}
	if !s.callerMayEditTaskText(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return nil, nil, false
	}
	if TaskRecordReadOnly(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return nil, nil, false
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return nil, nil, false
	}
	if step == nil || step.TaskID != taskId {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return nil, nil, false
	}
	// No step-status check on purpose: a handover lands at any moment, so a
	// note is writable in ANY step status (gating it recreates waiting_reason's
	// moment-locked hole). Only the task-level terminal gate above applies.
	return t, step, true
}

// storeStepNote: announce=false (a no-op patch) still writes — the zero-row
// UPDATE is how a step deleted by a concurrent submit_plan is caught (→ 404).
func (s *apiServer) storeStepNote(w http.ResponseWriter, r *http.Request, t *Task, step *TaskStep, note string, announce bool) bool {
	ok, err := s.dal.SetTaskStepNote(step.ID, note)
	if err != nil {
		internalError(w, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "step '"+step.ID+"' not found")
		return false
	}
	step.Note = note
	if !announce {
		return true
	}
	// Bump updated_ts: the SSE task delta carries only id/status/priority, and
	// an open cockpit card re-reads its steps only when updated_ts changes.
	now := nowSecs()
	if err := s.dal.TouchTaskUpdatedTS(t.ID, now); err != nil {
		internalError(w, err)
		return false
	}
	t.UpdatedTS = now
	s.publishTask(*t, requestTrigger(r))
	return true
}
