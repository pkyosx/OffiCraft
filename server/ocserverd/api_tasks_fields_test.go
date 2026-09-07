// Skeleton generated from server/ocserverd/api_tasks_fields.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestResolveTaskTextEdit(t *testing.T) {
	t.Skip("TODO: resolveTaskTextEdit validates the whole body against the task as it stands and reports the message for a 400, or \"\" when the body is admissible.")
}

func TestWriteTaskText(t *testing.T) {
	t.Skip("TODO: writeTaskText performs the versioned write of every field the edit names, in ONE transaction: the revisions being replaced and the new values land together.")
}

func TestUpdateTaskText(t *testing.T) {
	t.Skip("TODO: updateTaskText is the body of all three doors onto a task's text.")
}

func TestHandleUpdateTaskApiTasksTaskIdPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id} reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
