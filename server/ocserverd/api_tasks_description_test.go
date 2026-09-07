// Skeleton generated from server/ocserverd/api_tasks_description.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestTaskDescriptionHistorySnapshot(t *testing.T) {
	t.Skip("TODO: taskDescriptionHistorySnapshot serialises the state a description write replaces.")
}

func TestTaskDescriptionSnapshotIn(t *testing.T) {
	t.Skip("TODO: taskDescriptionSnapshotIn is the reader SaveWithDocumentHistory calls from INSIDE the write transaction.")
}

func TestTaskDescriptionHistoryStream(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestWriteTaskDescription(t *testing.T) {
	t.Skip("TODO: writeTaskDescription performs the versioned write: the revision this text replaces and the text itself land in ONE transaction.")
}

func TestHandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/description answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/description request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/description reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/description request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
