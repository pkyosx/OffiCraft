package main

import "net/http"

// GET /api/tasks/{task_id}/artifacts is the ONLY way to obtain an artifact
// row, id or name: task responses carry just artifact_count (owner ruling
// rc-15016959ad4d). It names a task, not an artifact (owner ruling
// c-f2d0fecb1168): the cockpit's panel opens the whole set at once.
func (s *apiServer) HandleListTaskArtifactsApiTasksTaskIdArtifactsGet(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	arts, err := s.taskArtifactDTOs(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	// ArtifactsDetailLevel stays "full" although nothing contrasts with it
	// now: a conformance check asserts it.
	writeJSON(w, http.StatusOK, taskArtifactListDTO{
		TaskID:               t.ID,
		ArtifactsDetailLevel: taskArtifactsDetailLevelFull,
		Artifacts:            arts,
	})
}
