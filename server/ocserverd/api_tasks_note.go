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
	if _, _, ok := s.resolveStepForNoteWrite(w, r, taskId, stepId); !ok {
		return
	}
	t, step, ok := s.storeStepNote(w, r, taskId, stepId, func(TaskStep) (string, bool, error) {
		return note, true, nil
	})
	if !ok {
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
// The edits apply to the note as it stands in the transaction that writes the
// result, so two interleaving patch requests both land (or the later one's
// anchor moved and it is a 400). The ceiling is applied to the RESULT of the
// patch.
func (s *apiServer) HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNotePatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	_, step, ok := s.resolveStepForNoteWrite(w, r, taskId, stepId)
	if !ok {
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	allowShrink := body.AllowShrink != nil && *body.AllowShrink
	limit := s.stepNoteCap()
	patch := func(cur TaskStep) (string, int, error) {
		// "get_task_step", not get_task: since T-66 get_task carries only the note's
		// SIZE, and an anchor-miss pointing there would re-anchor against nothing.
		next, applied, err := ApplyDocEdits(cur.Note, edits, "get_task_step")
		if err != nil {
			return "", 0, refuseInTx(http.StatusBadRequest, err.Error())
		}
		if !allowShrink && LessonsShrinkBlocked(cur.Note, next) {
			return "", 0, refuseInTx(http.StatusBadRequest,
				"patch would empty (or shrink to under a tenth of) the step note — pass allow_shrink=true if this is intended, or use update_step_note; nothing was written")
		}
		if refusal := stepNoteLimitRefusal(next, limit); refusal != nil {
			return "", 0, refusal
		}
		return next, applied, nil
	}
	if _, _, err := patch(*step); err != nil {
		writeTxError(w, err)
		return
	}
	var applied int
	t, step, ok := s.storeStepNote(w, r, taskId, stepId, func(cur TaskStep) (string, bool, error) {
		next, n, err := patch(cur)
		applied = n
		// Announce only when the text changed — NOT `applied > 0`, which counts
		// edits that moved an intermediate result (self-cancelling edits report
		// non-zero). The write itself still happens (see storeStepNote). The
		// wholesale face keeps its unconditional delta.
		return next, next != cur.Note, err
	})
	if !ok {
		return
	}
	next := step.Note
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
	if refusal := stepNoteLimitRefusal(note, s.stepNoteCap()); refusal != nil {
		writeTxError(w, refusal)
		return false
	}
	return true
}

func stepNoteLimitRefusal(note string, limit int) error {
	if n := utf8.RuneCountInString(note); n > limit {
		return refuseInTx(http.StatusBadRequest, "step note is "+strconv.Itoa(n)+
			" chars, over the "+strconv.Itoa(limit)+"-char limit")
	}
	return nil
}

func (s *apiServer) resolveStepForNoteWrite(w http.ResponseWriter, r *http.Request, taskId, stepId string) (*Task, *TaskStep, bool) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return nil, nil, false
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return nil, nil, false
	}
	if err := stepNoteWriteRefusal(s.dal.GetMember, r, *t, step, stepId); err != nil {
		writeTxError(w, err)
		return nil, nil, false
	}
	return t, step, true
}

// No step-status check on purpose: a handover lands at any moment, so a note is
// writable in ANY step status (gating it recreates waiting_reason's
// moment-locked hole). Only the task-level terminal gate applies.
func stepNoteWriteRefusal(member memberLookup, r *http.Request, t Task, step *TaskStep, stepID string) error {
	if !callerMayEditTaskText(member, r, t) {
		return refuseInTx(http.StatusForbidden, taskActorRefusal)
	}
	if TaskRecordReadOnly(t.Status) {
		return refuseInTx(http.StatusConflict,
			"task '"+t.ID+"' is already closed ("+t.Status+")")
	}
	if step == nil || step.TaskID != t.ID {
		return refuseInTx(http.StatusNotFound, "step '"+stepID+"' not found")
	}
	return nil
}

// storeStepNote judges the task and the step again inside the transaction that
// writes, and next computes the note from the step as it stands there
// (announce=false still writes). The delta goes out after commit.
func (s *apiServer) storeStepNote(
	w http.ResponseWriter, r *http.Request, taskID, stepID string,
	next func(cur TaskStep) (note string, announce bool, err error),
) (*Task, *TaskStep, bool) {
	var t Task
	var step TaskStep
	var announce bool
	now := nowSecs()
	err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getTaskOn(tx, taskID)
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "task '"+taskID+"' not found")
		}
		st, err := getTaskStepOn(tx, stepID)
		if err != nil {
			return err
		}
		if err := stepNoteWriteRefusal(membersOn(tx), r, *cur, st, stepID); err != nil {
			return err
		}
		note, ann, err := next(*st)
		if err != nil {
			return err
		}
		if _, err := setTaskStepNoteOn(tx, st.ID, note); err != nil {
			return err
		}
		st.Note = note
		// Bump updated_ts: the SSE task delta carries only id/status/priority, and
		// an open cockpit card re-reads its steps only when updated_ts changes.
		if ann {
			if err := touchTaskUpdatedTSOn(tx, cur.ID, now); err != nil {
				return err
			}
			cur.UpdatedTS = now
		}
		t, step, announce = *cur, *st, ann
		return nil
	})
	if err != nil {
		writeTxError(w, err)
		return nil, nil, false
	}
	if announce {
		s.publishTask(t, requestTrigger(r))
	}
	return &t, &step, true
}
