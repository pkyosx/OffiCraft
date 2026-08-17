package main

import (
	"net/http"
)

// ── T-2ebe 任務標題可編輯 ─────────────────────────────────────────────────────
//
// The description became correctable in T-e271; the title did not, and the gap
// between those two dates is what this file closes. A title is the ONLY cell of
// a task the list renders, so it is the first — and for anyone scanning, often
// the only — thing read about a card. When a ticket's scope is overturned, and
// that happens precisely because someone did the work of checking, the
// description could correct itself while the title went on advertising the
// original wording forever. The two then disagree, and a reader who trusts the
// list is reading the stale half.
//
// The observed decay is not gradual: one card was narrowed by the owner on the
// SAME DAY it was filed. The worker holding it corrected the description, said
// in writing that the narrowing was the owner's decision rather than an
// omission — exactly right — and then had to report that the title was beyond
// any tool it had. What it was forced to leave behind was a card whose title and
// description contradict each other, reconciled only by a sentence asking the
// reader to believe one of them. That is the shape this file exists to stop.
//
// The route is the description route with one field swapped, and that is
// deliberate: an owner ruling already settled who may correct a ticket's text
// (T-e271), so re-deciding it here would produce a second, subtly different
// answer to a question that has one. Hence the same callerMayDriveTask gate, the
// same partial-update semantics, the same silence about TaskIsTerminal, and the
// same already-shipped document_history mechanism rather than a second audit
// trail.
//
// ONE difference, and it is a difference in KIND rather than an exemption:
//
//	An explicit blank title is REFUSED (400), where an explicit blank
//	description CLEARS the field. A description may legitimately be empty —
//	plenty of tasks were created without one, and "this card has no prose" is a
//	true statement about a card. A title may not: create_task has always
//	refused a blank one, so a blank title is a state the system does not
//	otherwise admit. An edit door looser than its own create door does not add
//	flexibility, it adds a bypass — and the state on the far side of this
//	particular bypass is a task list row with nothing in it, which is the very
//	surface this capability exists to keep true. Owner sign-off on that
//	asymmetry: card rc-796541192519 (2026-08-11), option ①.

// docKindTaskTitle is one task's title as a versioned document (T-2ebe). Its key
// is the TASK id — the same keying as docKindTaskDescription, and for the same
// reason: a title belongs to one card, unlike the manual series which are keyed
// by type. The two kinds are separate series over the same key, so restoring a
// title never disturbs the description's own three retained revisions.
const docKindTaskTitle = "task_title"

// taskTitleHistorySnapshot serialises the state a title write replaces.
//
// Unlike its description twin this has no empty-string branch, and the absence
// is load-bearing rather than an oversight: a task cannot have a blank title.
// create_task refuses one and so does this route, so "" here would mean the row
// vanished — which taskTitleSnapshotIn reports through its own not-found path,
// not through a snapshot that says "there was no document here".
func taskTitleHistorySnapshot(title string) (string, error) {
	return historyJSON(map[string]string{"title": title})
}

// taskTitleSnapshotIn is the reader SaveWithDocumentHistory calls from INSIDE
// the write transaction. It re-reads rather than trusting the value the handler
// folded a moment earlier: the retained revision must be the state this write
// actually replaced, otherwise two callers correcting the same card both retain
// the same ancestor and whichever landed between them is unrecoverable.
func taskTitleSnapshotIn(taskID string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
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

// writeTaskTitle performs the versioned write: the revision this text replaces
// and the text itself land in ONE transaction. Reports false when the task row
// vanished mid-write (a concurrent hard delete), which the caller turns into a
// 404 rather than reporting a write that did not happen.
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

// POST /api/tasks/{task_id}/title — correct one task's title.
//
// 🔴 T-646a: this is no longer an MCP tool. The agent-facing tool is
// update_task, which writes this same field through the same code
// (updateTaskText). The route stays on the HTTP surface for the cockpit and any
// existing client.
//
// Guard order: 404 unknown task → 403 not the executor → 400 blank → write.
// The blank check sits AFTER the permission gate on purpose: a caller who may
// not touch this task should learn that, not be handed a critique of a body it
// was never entitled to submit.
//
// PARTIAL update, shaped after its description twin: the body's only field is
// title, omitting it is a legal no-op, and an unknown key is refused by the
// strict decoder rather than dropped. The field is a nullable pointer with no
// default for the same reason it is there — so "absent" and "present but blank"
// stay distinguishable — though here the second one is a 400 rather than a
// clear.
//
// NO LENGTH CAP, matching create_task, which has never capped this field. A
// ceiling applied HERE and nowhere else would mean an already-long title can
// only ever be made shorter, and a correction that does not shrink it would be
// refused at the edit door while the very same words entered freely through
// create. If a cap is ever wanted it belongs on BOTH doors at once, sized so no
// stored title is already over it — and that is a new protection over an
// existing wire, which is the owner's call rather than this ticket's.
//
// 🔴 The unchanged-value early return is not just an optimisation: without it a
// no-op write would spend one of the three retained revisions saying the title
// did not change, and fan an SSE delta for it. Compare AFTER trimming, so
// re-sending a title with a stray trailing space is correctly seen as no change.
// 🔴 T-646a: the body of this route now lives in updateTaskText, shared with
// update_task_description and with update_task, which supersedes both on the MCP
// surface. Its behaviour through this door is unchanged; what changed is that
// there is no longer a second copy of the rules to drift away from the first.
// This route stays on the HTTP surface for the frontend and any existing client.
func (s *apiServer) HandleUpdateTaskTitleApiTasksTaskIdTitlePost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskTitleDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, body.Title, nil)
}
