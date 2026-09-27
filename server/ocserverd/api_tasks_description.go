package main

import (
	"net/http"
)

const docKindTaskDescription = "task_description"

// An EMPTY description snapshots as "{}", which retainDocumentVersion treats as
// nothing to retain; otherwise the first edit of every task created without a
// description would burn one of the three retained slots on an empty revision.
func taskDescriptionHistorySnapshot(description string) (string, error) {
	if description == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"description": description})
}

func taskDescriptionSnapshotIn(taskID string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, ok, err := taskDescriptionOn(q, taskID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "{}", nil
		}
		return taskDescriptionHistorySnapshot(current)
	}
}

func taskDescriptionHistoryStream(taskID, actor string) documentHistoryStream {
	return documentHistoryStream{
		Kind: docKindTaskDescription, Key: taskID, ActorID: actor,
		Snapshot: taskDescriptionSnapshotIn(taskID),
	}
}

func (s *apiServer) writeTaskDescription(t *Task, actor, description string) (bool, error) {
	now := nowSecs()
	wrote := false
	err := s.dal.SaveWithDocumentHistories(
		[]documentHistoryStream{taskDescriptionHistoryStream(t.ID, actor)},
		func(ex sqlExecer) error {
			ok, err := SetTaskDescriptionOn(ex, t.ID, description, now)
			wrote = ok
			return err
		})
	if err != nil || !wrote {
		return false, err
	}
	t.Description = description
	t.UpdatedTS = now
	return true, nil
}

// POST /api/tasks/{task_id}/description — HTTP-only (the MCP tool is update_task);
// the body lives in updateTaskText.
//
// 🔴 TERMINAL STATE (owner ruling: a CLOSED task is editable on the same terms).
// A closed task's ARTIFACT SET is frozen because it is the record of what the task
// produced; the description is the ticket's own TEXT, and a wrongly worded ticket
// is usually found out after it closed. Both rules keep the closed record TRUE —
// freezing the text would preserve a known falsehood.
//
// NO LENGTH CAP, deliberately: create_task has never capped this field, so a cap
// only here would mean an already-long description can only ever be made shorter
// (DocCapBlocked lets over-cap text through only when it shrinks) — a correction
// that does not shrink it would be refused while create accepts the same words. A
// cap, if ever wanted, is the owner's call and belongs on BOTH doors, sized so no
// stored description is already over it.
func (s *apiServer) HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskDescriptionDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, nil, body.Description)
}
