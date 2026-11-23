package main

import (
	"net/http"
)

func (s *apiServer) HandleGetTaskManualSuggestedRepliesApiTaskManualSuggestedRepliesGet(w http.ResponseWriter, r *http.Request) {
	lists, err := s.listTaskManualSuggestedReplies()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TaskManualSuggestedRepliesDTO{Lists: lists})
}

func (s *apiServer) HandleUpdateTaskManualSuggestedRepliesApiTaskManualSuggestedRepliesTypeKeyPut(w http.ResponseWriter, r *http.Request, typeKey string) {
	var body TaskManualSuggestedRepliesUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "replies") {
		return
	}
	replies, msg := suggestedRepliesWriteRefusal(body.Replies, "type")
	if msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	err := s.dal.inTx(func(tx *writeTx) error {
		if !isBuiltinTaskManual(typeKey) {
			m, err := getTaskManualOn(tx, typeKey)
			if err != nil {
				return err
			}
			if FoldTaskManual(m, nil) == nil {
				return refuseInTx(http.StatusNotFound, "task manual '"+typeKey+"' not found")
			}
		}
		return replaceTaskManualSuggestedRepliesOn(tx, typeKey, replies)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishTaskManualSuggestedReplies(requestTrigger(r))
	writeJSON(w, http.StatusOK, TaskManualTypeSuggestedRepliesDTO{TypeKey: typeKey, Replies: replies})
}

// Owner-only: no agent consumes task_manual_suggested_reply. The payload is null;
// the cockpit refetches GET /api/task-manual-suggested-replies.
func (s *apiServer) publishTaskManualSuggestedReplies(trigger string) {
	s.hub.Publish("task_manual_suggested_reply", "patch", "task_manual_suggested_reply", wireOwnerID, nil, audienceOwnerOnly(), trigger)
}

func replaceTaskManualSuggestedRepliesOn(tx *writeTx, typeKey string, replies []string) error {
	if _, err := deleteTaskManualSuggestedRepliesOn(tx, typeKey); err != nil {
		return err
	}
	for i, text := range replies {
		if _, err := tx.Exec(`INSERT INTO task_manual_suggested_reply (type_key, position, text) VALUES (?, ?, ?)`,
			typeKey, i, text); err != nil {
			return err
		}
	}
	return nil
}

func (s *apiServer) listTaskManualSuggestedReplies() ([]TaskManualTypeSuggestedRepliesDTO, error) {
	rows, err := s.dal.rdb.Query(`SELECT type_key, text FROM task_manual_suggested_reply
		ORDER BY type_key, position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lists := []TaskManualTypeSuggestedRepliesDTO{}
	for rows.Next() {
		var typeKey, text string
		if err := rows.Scan(&typeKey, &text); err != nil {
			return nil, err
		}
		if n := len(lists); n == 0 || lists[n-1].TypeKey != typeKey {
			lists = append(lists, TaskManualTypeSuggestedRepliesDTO{TypeKey: typeKey, Replies: []string{}})
		}
		last := &lists[len(lists)-1]
		last.Replies = append(last.Replies, text)
	}
	return lists, rows.Err()
}

// DeleteTaskManual calls this in its transaction: a type created later under the
// same key must not inherit the deleted type's sentences.
func deleteTaskManualSuggestedRepliesOn(ex sqlExecer, typeKey string) (int64, error) {
	res, err := ex.Exec(`DELETE FROM task_manual_suggested_reply WHERE type_key = ?`, typeKey)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
