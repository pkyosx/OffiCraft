package main

import (
	"errors"
	"net/http"
)

// api_tasks_fields.go — the one seam behind all three edit doors onto a task's
// text: update_task plus the two HTTP-only routes (kept for the frontend and
// existing clients, off the MCP catalogue).
//
// Owner ruling rc-0fb94a25a8a8: BOTH fields are trimmed, before storage AND before
// the unchanged-value comparison. 🔴 Blank stays asymmetric (owner ruling
// rc-796541192519, reasoning atop api_tasks_title.go): a blank title is refused
// (400), a blank description clears the text.
//
// ⚠️ "Both fields are trimmed" is a statement about THIS seam, not the system:
//   - create_task stores the description raw, so re-sending that stored text here
//     reads as a change (one retained slot plus a spurious delta). Not fixed:
//     trimming at create is a wire change for both doors or neither.
//   - The restore path (api_document_history.go) calls writeTaskTitle /
//     writeTaskDescription directly: its title arm trims, its description arm
//     writes verbatim — an unruled asymmetry that can put untrimmed text back.
//   - 🔴 Those two siblings record row-vanished in a flag and return outside the
//     transaction, so it COMMITS a revision for a task that no longer exists
//     (not fixed: a restore-path behaviour change).
//
// Trimming is strings.TrimSpace: a description whose FIRST line is an indented
// markdown code block loses the indent and stops rendering as code (fenced blocks
// are unaffected).

// errTaskRowVanished is returned from INSIDE the write function so the transaction
// rolls back instead of committing the first of two fields.
var errTaskRowVanished = errors.New("task row vanished mid-write")

// taskTextEdit already drops unchanged fields, so a no-op never spends one of the
// three retained revisions.
type taskTextEdit struct {
	setTitle       bool
	title          string
	setDescription bool
	description    string
}

func (e taskTextEdit) empty() bool { return !e.setTitle && !e.setDescription }

func resolveTaskTextEdit(t Task, title, description *string) (taskTextEdit, string) {
	var e taskTextEdit
	if title != nil {
		v := trimString(*title)
		if v == "" {
			// Same words create_task uses, deliberately.
			return taskTextEdit{}, "title must not be blank"
		}
		if v != t.Title {
			e.setTitle, e.title = true, v
		}
	}
	if description != nil {
		v := trimString(*description)
		if v != t.Description {
			e.setDescription, e.description = true, v
		}
	}
	return e, ""
}

// writeTaskText enrolls a history stream ONLY for a changing field: the retained
// set is three deep, so enrolling both would push out the untouched field's oldest
// recoverable wording.
func (s *apiServer) writeTaskText(t *Task, actor string, e taskTextEdit) (bool, error) {
	now := nowSecs()
	streams := make([]documentHistoryStream, 0, 2)
	if e.setTitle {
		streams = append(streams, taskTitleHistoryStream(t.ID, actor))
	}
	if e.setDescription {
		streams = append(streams, taskDescriptionHistoryStream(t.ID, actor))
	}
	err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		if e.setTitle {
			ok, err := SetTaskTitleOn(ex, t.ID, e.title, now)
			if err != nil {
				return err
			}
			if !ok {
				return errTaskRowVanished
			}
		}
		if e.setDescription {
			ok, err := SetTaskDescriptionOn(ex, t.ID, e.description, now)
			if err != nil {
				return err
			}
			if !ok {
				return errTaskRowVanished
			}
		}
		return nil
	})
	if errors.Is(err, errTaskRowVanished) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if e.setTitle {
		t.Title = e.title
	}
	if e.setDescription {
		t.Description = e.description
	}
	t.UpdatedTS = now
	return true, nil
}

// updateTaskText: the body is judged only after the 403 gate, so a caller without
// standing gets no critique of a body it was never entitled to submit.
// No TaskIsTerminal guard (reasoning at the terminal-state note in
// api_tasks_description.go) and no length cap, matching create_task: a cap only
// at the edit door would let an already-long value only ever shrink.
func (s *apiServer) updateTaskText(w http.ResponseWriter, r *http.Request, taskID string, title, description *string) {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return
	}
	if !callerMayEditTaskText(s.dal.GetMember, r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	edit, bad := resolveTaskTextEdit(*t, title, description)
	if bad != "" {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	if edit.empty() {
		s.writeTaskWriteReceipt(w, *t)
		return
	}
	ok, err := s.writeTaskText(t, currentActor(r), edit)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "task '"+taskID+"' not found")
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskWriteReceipt(w, *t)
}

// Body fields are nullable pointers so "absent" and "present but empty" stay
// distinct: a defaulted "" would let a body that never named the description
// erase it.
func (s *apiServer) HandleUpdateTaskApiTasksTaskIdPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskFieldsDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, body.Title, body.Description)
}
