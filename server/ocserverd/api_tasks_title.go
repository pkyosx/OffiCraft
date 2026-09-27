package main

import (
	"net/http"
)

const docKindTaskTitle = "task_title"

func taskTitleHistorySnapshot(title string) (string, error) {
	return historyJSON(map[string]string{"title": title})
}

// Re-reads inside the write transaction rather than trusting the handler's value:
// otherwise concurrent writers retain the same ancestor and lose the one between.
func taskTitleSnapshotIn(taskID string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, ok, err := taskTitleOn(q, taskID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "{}", nil
		}
		return taskTitleHistorySnapshot(current)
	}
}

func taskTitleHistoryStream(taskID, actor string) documentHistoryStream {
	return documentHistoryStream{
		Kind: docKindTaskTitle, Key: taskID, ActorID: actor,
		Snapshot: taskTitleSnapshotIn(taskID),
	}
}

func (s *apiServer) writeTaskTitle(t *Task, actor, title string) (bool, error) {
	now := nowSecs()
	wrote := false
	err := s.dal.SaveWithDocumentHistories(
		[]documentHistoryStream{taskTitleHistoryStream(t.ID, actor)},
		func(ex sqlExecer) error {
			ok, err := SetTaskTitleOn(ex, t.ID, title, now)
			wrote = ok
			return err
		})
	if err != nil || !wrote {
		return false, err
	}
	t.Title = title
	t.UpdatedTS = now
	return true, nil
}

// POST /api/tasks/{task_id}/title — HTTP only (cockpit and existing clients), not an
// MCP tool: agents correct the title through update_task, which shares updateTaskText.
func (s *apiServer) HandleUpdateTaskTitleApiTasksTaskIdTitlePost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskTitleDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, body.Title, nil)
}
