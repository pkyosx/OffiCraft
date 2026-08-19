package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"unicode/utf8"
)

// ── T-cc3e 步驟備註欄 ────────────────────────────────────────────────────────
//
// Every agent's handover SOP says, verbatim: "把還在進行中的工作寫回 task step
// note（做到哪、下一步接什麼）". Until this endpoint existed that sentence named
// nothing — the step row had no general-purpose note, so the progress an agent
// wrote for its successor had nowhere to land. An outsource worker at 88%
// context said so honestly instead of pretending it had written somewhere, and
// the owner ruled (rc-15cf8df7cb7f, option 2) that the STEP layer gets its own
// note rather than folding this into the editable task description.
//
// Why neither existing note-shaped field could serve:
//   - waiting_reason is bound to ONE status: settable only entering
//     waiting_external, cleared by the status handler on the way out.
//   - the handoff_* fields live on the TASK and are read only on the report
//     that closes it.
//
// Both are moment-locked. A handover lands at an arbitrary moment, so the note
// has to be writable in ANY step status — that generality is the point, and
// TestStepNoteWritableInEveryStepStatus pins it.
//
// Its own endpoint and its own MCP tool, not another parameter on
// update_step_status: charter §14 is intent-per-tool, and writing a note is a
// different intent from reporting a transition. One field with two write paths
// would reintroduce exactly the "which one do I write?" ambiguity this ticket
// exists to remove.
//
// POST /api/tasks/{task_id}/steps/{step_id}/note — wholesale write: the body's
// note replaces whatever was there, "" clears it. Guards mirror the other
// task-driving writes (executor-or-admin 403, unknown task/step 404, terminal
// task 409) so the note is not a side door into a closed task's timeline.
func (s *apiServer) HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNoteUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "note") {
		return
	}
	note := trimString(body.Note)
	if !stepNoteWithinLimit(w, note) {
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
		TaskID: t.ID, StepID: step.ID, StepStatus: step.Status, Note: step.Note,
	})
}

// POST /api/tasks/{task_id}/steps/{step_id}/note/patch — anchor-addressed patch
// of one step's working note (T-1667; MCP patch_step_note). ApplyDocEdits is
// the SHARED engine, so the anchor/append/atomicity semantics are
// byte-identical to the three patch faces that came before it.
//
// WHY THIS EXISTS — CONCURRENT OVERWRITE, not token economy. The wholesale
// write above replaces the note, and a step note has more than one writer by
// design: a handover is precisely the moment when an outgoing session and its
// successor are both writing about the same lane. Whoever writes second from a
// copy read before the other landed deletes the other's text outright. Nothing
// catches it — the stale copy is usually the LONGER one (it was re-typed in
// full by a session that had the whole note in context), so the write does not
// even look like a deletion and no guard fires. The anchor closes that shape by
// construction rather than by locking: the caller sends only {old, new} and
// never a base copy, so "overwrite the whole note from a base I read earlier"
// is not expressible on this wire at all, and the splice is matched against the
// note as it stands when this request reads it. A concurrent write that moved or
// duplicated the anchor turns this batch into a visible 400 instead.
//
// WHAT IS STILL OPEN. Concurrent edits to DIFFERENT anchors survive TOGETHER
// once the two requests serialise — each is spliced onto whatever the note says
// at its own read. What remains is the read-then-write gap INSIDE one request,
// which is milliseconds wide rather than handover-long, but is not zero: an
// interleaving there still eats one side's edit silently.
// Concretely: resolveStepForNoteWrite reads the step from the read pool and
// storeStepNote writes it through the write pool, with no transaction spanning
// the two, no version compare, and an UPDATE that carries no old value. Two
// patch requests interleaving in the server (A reads → B reads → A writes →
// B writes) still lose A's edit silently. SetTaskStepNote being a SINGLE-column
// UPDATE is not a defence here — that is what stops the whole-row step writers
// from replaying a note they read earlier (T-e271, api_tasks_note_race_test.go)
// — because both patches compute their new text from the same base. Closing it
// needs the read and the write under one transaction, or a version/etag compare
// at the write boundary. Tracked separately. The patch_task_sop twin carries
// the same gap AND a wider one — its write is a whole-row upsert, so read that
// face's own caveat rather than assuming the two are equivalent.
//
// Guards are the wholesale write's, called through the SAME two helpers rather
// than restated — two faces onto one field must not be able to disagree about
// who may write, which task states are open, or what the note's ceiling is.
// The ceiling is applied to the RESULT of the patch: a patch face that skipped
// it would be an uncapped door onto a capped field.
func (s *apiServer) HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNotePatchDTO
	if !decodeJSONBodyStrict(w, r, &body, "edits") {
		return
	}
	if !requireNonEmptyEdits(w, body.Edits) {
		return
	}
	// Target first, content second — existence, then authz, then state, then the
	// edits themselves; that is the order the older patch faces take, and a
	// caller pointed at a task it may not touch should hear that, not a verdict
	// on its edits.
	t, step, ok := s.resolveStepForNoteWrite(w, r, taskId, stepId)
	if !ok {
		return
	}
	edits, ok := decodePatchEdits(w, body.Edits)
	if !ok {
		return
	}
	// get_task, not get_lessons: the anchor-miss message tells the caller where
	// to look next, and a step note is read back through the task view (the
	// reason ApplyDocEdits takes the tool name as a parameter).
	next, applied, err := ApplyDocEdits(step.Note, edits, "get_task")
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
	if !stepNoteWithinLimit(w, next) {
		return
	}
	// 🔴 `next` byte-identical to the stored note → there is nothing to ANNOUNCE.
	// The gate is that text comparison and NOT applied > 0: `applied` counts edits
	// that moved the INTERMEDIATE result, so a batch whose edits undo one another
	// reports applied != 0 over a note that never changed (ApplyDocEdits in
	// domain.go marks `applied > 0` as the exact reasoning error the earlier patch
	// faces were built on). A step note keeps no document history, so no retention
	// is at stake here; what an unconditional ANNOUNCEMENT costs is an SSE task
	// delta about a change that never happened — every cockpit card holding this
	// task refetches and gets back the text it already had — plus an updated_ts
	// bump that misdates the task's last real movement.
	//
	// The gate holds back the announcement ONLY, never the write: that UPDATE is
	// also the one place a step deleted by a concurrent submit_plan is noticed
	// (SetTaskStepNote affects zero rows → 404). Skipping it on a no-op would make
	// this the single path that answers 200 with a note and a sha256 for a step
	// that no longer exists — a false statement about current state, in a face
	// that exists FOR concurrency safety. It stores nothing new (the bytes are
	// byte-identical), so re-running it costs a WAL frame and buys the one check
	// worth having; two detection seams, one per path, could disagree.
	//
	// Deliberately not carried into the wholesale face: it says "the note is now
	// this" and owes the same unconditional delta as the other wholesale faces
	// (update_task_manual, write_task_learnings). Only a patch face reports a count
	// of edits, and only a patch face can report a non-zero one over a document
	// that never moved.
	if !s.storeStepNote(w, r, t, step, next, next != step.Note) {
		return
	}
	sum := sha256.Sum256([]byte(next))
	writeJSON(w, http.StatusOK, taskStepNotePatchResultDTO{
		TaskID:       t.ID,
		StepID:       step.ID,
		StepStatus:   step.Status,
		Note:         step.Note,
		AppliedEdits: applied,
		SizeChars:    utf8.RuneCountInString(next),
		CapChars:     chatBodyMaxChars,
		Sha256:       hex.EncodeToString(sum[:]),
	})
}

// stepNoteWithinLimit holds a would-be note to the field's ceiling, writing the
// 400 and returning false when it is over.
//
// Same ceiling as the task-level handover note (HandleReassignTaskApi...): it
// is the same kind of writing for the same reader, so it gets the same limit
// rather than a second number to remember. Runes, not bytes — these notes are
// written in Chinese.
func stepNoteWithinLimit(w http.ResponseWriter, note string) bool {
	if n := utf8.RuneCountInString(note); n > chatBodyMaxChars {
		writeError(w, http.StatusBadRequest, "step note is "+strconv.Itoa(n)+
			" chars, over the "+strconv.Itoa(chatBodyMaxChars)+"-char limit")
		return false
	}
	return true
}

// resolveStepForNoteWrite runs the guard chain both note write faces share and
// returns the task and step they resolved to.
func (s *apiServer) resolveStepForNoteWrite(w http.ResponseWriter, r *http.Request, taskId, stepId string) (*Task, *TaskStep, bool) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return nil, nil, false
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return nil, nil, false
	}
	if TaskIsTerminal(t.Status) {
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
	// No step-status check on purpose. Writing a note is legal on a pending step
	// (recording what the lane is for before it starts), an in_progress one (the
	// common case), a step parked in waiting_external, one holding a reply card,
	// and a done or superseded one. The status machine governs the WORK; the
	// note only describes it.
	//
	// Note that the TASK-level terminal gate above still applies: once every
	// step is done the task auto-closes, so a done step is writable while its
	// task is still open and not after. That is the same line the artifact set
	// draws — a closed task's record stops moving — and the tool descriptions
	// say so rather than promising a write that would 409.
	return t, step, true
}

// storeStepNote persists the note and, when announce is set, fans the task
// delta; shared by both write faces. It mutates step.Note and t.UpdatedTS in
// place so the caller's receipt echoes what was STORED.
//
// announce=false is the patch face's no-op batch: the note is written anyway
// (identical bytes, and the row-count is how a concurrently deleted step is
// caught) but nothing is told the cockpit about a change that did not happen.
func (s *apiServer) storeStepNote(w http.ResponseWriter, r *http.Request, t *Task, step *TaskStep, note string, announce bool) bool {
	ok, err := s.dal.SetTaskStepNote(step.ID, note)
	if err != nil {
		internalError(w, err)
		return false
	}
	if !ok {
		// The step existed a moment ago and does not now — a concurrent
		// submit_plan deleted it. Honest 404 beats resurrecting the row.
		writeError(w, http.StatusNotFound, "step '"+step.ID+"' not found")
		return false
	}
	step.Note = note
	if !announce {
		return true
	}
	// Move updated_ts so the cockpit actually shows this. The SSE task delta
	// carries only id/status/priority and the list it refreshes carries no
	// steps, so a card the owner ALREADY has open re-reads its step-bearing
	// detail only when updated_ts changes. Without this bump the owner watching
	// a live handover would see nothing until he collapsed and re-expanded the
	// card — the one case this ticket exists to serve.
	now := nowSecs()
	if err := s.dal.TouchTaskUpdatedTS(t.ID, now); err != nil {
		internalError(w, err)
		return false
	}
	t.UpdatedTS = now
	s.publishTask(*t, requestTrigger(r))
	return true
}
