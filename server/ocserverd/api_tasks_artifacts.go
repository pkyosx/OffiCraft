package main

import "net/http"

// ── T-66 產物完整讀取面 ──────────────────────────────────────────────────────
//
// GET /api/tasks/{task_id}/artifacts — MCP list_task_artifacts.
//
// WHY IT EXISTS. Until T-66 every response newTaskDTO builds carried the FULL
// artifact row — url, filename, mime, kind, is_image, attachment_id, created_by,
// created_ts — on all nine of its exits (get_task, terminate, reassign, claim,
// duplicate, set_task_deps, the create dedupe hit, description and title). The
// owner ruled the default shrinks to a title and an id (c-cd063427fb2f:「我覺得
// 任務產物，只需要預設給標題跟ID, 有需要再透過另一隻去拿就好了」), so this is the
// 「另一隻」.
//
// 🔴 SINCE T-92 THE DEFAULT IS SMALLER STILL: a task response carries
// `artifact_count` and no rows at all — not even the id — on the owner's later
// ruling (rc-15016959ad4d:「只有 ID 好像也沒用」). So this route is no longer the
// "full" half of a pair; it is the ONLY way to obtain an artifact row, an
// artifact id, or an artifact name. Anything that draws an artifact, and
// anything that needs an id in order to act on one, comes here.
//
// 🔴 WHY IT NAMES A TASK AND NOT AN ARTIFACT. The symmetric-looking design —
// get_task_artifact(artifact_id), one row per call, exactly like get_task_step —
// is the one the owner rejected (c-f2d0fecb1168:「應該是指名任務？」). A step note
// is read ONE at a time, which is what makes the per-step door cheap; the
// cockpit's artifact panel opens onto the whole set at once, so a per-artifact
// door would turn one panel-open into one call per pinned row. The two splits
// have different shapes because the two reads have different shapes.
//
// AUTHZ IS THE READ FLOOR AND NOTHING MORE. routes.go declares principalMachine
// — the same floor GET /api/tasks/{task_id} and GET /api/tasks/{task_id}/steps/
// {step_id} sit on — and there is no executor guard here, because there is none
// on the task view either. This endpoint moves NO field to a stricter door than
// the one it was already served through: every field below rode get_task until
// this commit. A tighter floor here would close nothing (the task view is the
// wide door, not this one) and would only put the data out of reach of the tool
// that exists to serve it. If that floor is ever wrong it is wrong for all three
// faces and moves in routes.go for all three. 404 for an unknown task, from the
// same resolveTask/writeResolveError pair the task view uses.
func (s *apiServer) HandleListTaskArtifactsApiTasksTaskIdArtifactsGet(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	// Blob facts are resolved read-time and are honest-empty when the blob is
	// gone. Since T-92 EVERY kind resolves one — a link's target is read out of
	// its own text/uri-list blob — so this is the only projector left that
	// touches the blob store at all.
	arts, err := s.taskArtifactDTOs(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	// ArtifactsDetailLevel stays "full" and now has nothing to contrast with —
	// the task side used to answer "index". It is kept because a conformance
	// check asserts it, and because a reader holding this payload should not have
	// to know which server version produced it to know the rows are whole.
	writeJSON(w, http.StatusOK, taskArtifactListDTO{
		TaskID:               t.ID,
		ArtifactsDetailLevel: taskArtifactsDetailLevelFull,
		Artifacts:            arts,
	})
}
