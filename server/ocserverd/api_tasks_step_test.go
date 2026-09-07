// Skeleton generated from server/ocserverd/api_tasks_step.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandleGetTaskStepApiTasksTaskIdStepsStepIdGet(t *testing.T) {
	t.Run("a well-formed GET /api/tasks/{task_id}/steps/{step_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id}/steps/{step_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/tasks/{task_id}/steps/{step_id} reaches this handler with task_id, step_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id}/steps/{step_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
