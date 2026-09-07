// Skeleton generated from server/ocserverd/api_tasks_artifact_upload.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/artifacts/upload answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifacts/upload request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/artifacts/upload reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifacts/upload request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload reaches this handler with task_id, artifact_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestReadArtifactUploadBody(t *testing.T) {
	t.Skip("TODO: readArtifactUploadBody reads the raw body under the same size caps and the same mime resolution the chat-attachment upload uses — ONE upload mechanism, not two — and mints the blob without storing it (the caller's transaction does that, together with the pin).")
}

func TestArtifactKindOfBlob(t *testing.T) {
	t.Skip("TODO: artifactKindOfBlob decides file vs image from the blob's mime — the same read taskArtifactDTO's consumers make.")
}
