// Skeleton generated from server/ocserverd/api_tasks_note.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/steps/{step_id}/note answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/note request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/steps/{step_id}/note reaches this handler with task_id, step_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/note request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/steps/{step_id}/note/patch answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/note/patch request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/steps/{step_id}/note/patch reaches this handler with task_id, step_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/note/patch request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestStepNoteWithinLimit(t *testing.T) {
	t.Skip("TODO: stepNoteWithinLimit holds a would-be note to the field's ceiling, writing the 400 and returning false when it is over.")
}

func TestResolveStepForNoteWrite(t *testing.T) {
	t.Skip("TODO: resolveStepForNoteWrite runs the guard chain both note write faces share and returns the task and step they resolved to.")
}

func TestStoreStepNote(t *testing.T) {
	t.Skip("TODO: storeStepNote persists the note and, when announce is set, fans the task delta; shared by both write faces.")
}
