package main

import (
	"net/http"
)

// ── T-e271 任務描述可編輯 ────────────────────────────────────────────────────
//
// The tool catalogue had no way to change an EXISTING task's description. Every
// neighbouring write refuses the job by construction: create_task takes a
// description only at the moment of birth, submit_plan writes steps, and
// update_task_manual writes the TYPE's manual rather than any one task. So a
// ruling to reword a card — the owner deciding the ticket says the wrong thing —
// had nowhere in the system to land. This endpoint is that place.
//
// Three owner rulings shape it, and each one is visible in the code below:
//
//  1. Only the EXECUTOR may edit (plus admin/owner). Creating a task grants no
//     standing to keep rewriting it afterwards. That is exactly what the
//     existing callerMayDriveTask already decides, so this route reuses it
//     rather than growing a second, subtly different predicate. Whoever changes
//     that gate changes this route with it — deliberately.
//
//  2. A CLOSED task (completed / terminated / duplicated) is editable too, on
//     the same terms. See the reasoned difference at the terminal-state note in
//     HandleUpdateTaskDescription... below — this route's silence about
//     TaskIsTerminal is a decision, not an omission.
//
//  3. The change leaves a trail, through the version-history mechanism that is
//     ALREADY shipped (document_history / SaveWithDocumentHistory), not a second
//     one built for tasks. Kind docKindTaskDescription, key = the task id; the
//     same list and restore routes serve it as serve the global context, role
//     definitions and task manuals.

// docKindTaskDescription is one task's description as a versioned document
// (T-e271). Its key is the TASK id — not the type key: a description belongs to
// one card, unlike the manual series which are keyed by type.
const docKindTaskDescription = "task_description"

// taskDescriptionHistorySnapshot serialises the state a description write
// replaces.
//
// An EMPTY description reads back as "{}" — "there was no document here" —
// which is what retainDocumentVersion already treats as nothing to retain. The
// other kinds express that same fact by their row being absent (a manual that
// does not exist snapshots "{}"); a task row always exists, so the emptiness has
// to be spelled here. Without it, the first edit of the many tasks that were
// created with no description at all would burn one of the three retained slots
// on a revision that says nothing.
func taskDescriptionHistorySnapshot(description string) (string, error) {
	if description == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"description": description})
}

// taskDescriptionSnapshotIn is the reader SaveWithDocumentHistory calls from
// INSIDE the write transaction. It re-reads rather than trusting the value the
// handler folded a moment earlier: the retained revision must be the state this
// write actually replaced, otherwise two callers correcting the same card both
// retain the same ancestor and whichever landed between them is unrecoverable.
func taskDescriptionSnapshotIn(taskID string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, ok, err := taskDescriptionOn(q, taskID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "{}", nil
		}
		return taskDescriptionHistorySnapshot(current)
	}
}

func taskDescriptionHistoryStream(taskID, actor string) documentHistoryStream {
	return documentHistoryStream{
		Kind: docKindTaskDescription, Key: taskID, ActorID: actor,
		Snapshot: taskDescriptionSnapshotIn(taskID),
	}
}

// writeTaskDescription performs the versioned write: the revision this text
// replaces and the text itself land in ONE transaction. Reports false when the
// task row vanished mid-write (a concurrent hard delete), which the caller turns
// into a 404 rather than reporting a write that did not happen.
func (s *apiServer) writeTaskDescription(t *Task, actor, description string) (bool, error) {
	now := nowSecs()
	wrote := false
	err := s.dal.SaveWithDocumentHistories(
		[]documentHistoryStream{taskDescriptionHistoryStream(t.ID, actor)},
		func(ex sqlExecer) error {
			ok, err := SetTaskDescriptionOn(ex, t.ID, description, now)
			wrote = ok
			return err
		})
	if err != nil || !wrote {
		return false, err
	}
	t.Description = description
	t.UpdatedTS = now
	return true, nil
}

// POST /api/tasks/{task_id}/description — correct one task's description.
//
// 🔴 T-646a: this is no longer an MCP tool. The agent-facing tool is
// update_task, which writes this same field through the same code
// (updateTaskText). The route stays on the HTTP surface for the cockpit and any
// existing client.
//
// PARTIAL update, shaped after update_task_manual: the body's only field is
// description, omitting it is a legal no-op, and an unknown key is refused by
// the strict decoder rather than dropped. An explicit "" DOES clear the text —
// that asymmetry between "absent" and "empty" is why the DTO field is a nullable
// pointer with no default; a defaulted "" would let a body that never mentioned
// the description erase it.
//
// 🔴 TERMINAL STATE — why this route has no TaskIsTerminal guard while its
// neighbours do, stated as a difference in KIND and not as an exemption:
//
//	A closed task's ARTIFACT SET is frozen in both directions (add and remove,
//	admin and owner included — see HandleRemoveTaskArtifact...). What that
//	freeze protects is the OUTCOME: the artifacts are the record of what this
//	task actually produced, and a closed task's account of its own deliverables
//	must stop moving, or "what did this task ship" has no answer that stays put.
//
//	The description is not an outcome. It is the ticket's own TEXT — what the
//	task IS: scope, origin, acceptance. Correcting it changes nothing about what
//	was produced; it changes what the card SAYS it was for. And the need is at
//	its sharpest exactly where the freeze would bite: a ticket that was worded
//	wrongly is usually discovered to be wrong after it closed, and a rule that
//	only lets it be fixed while open leaves the wrong words standing forever, in
//	the permanent record, with no way to correct them.
//
//	So the two rules point opposite ways for the same reason — the closed record
//	should be TRUE. Freezing the deliverable set keeps it true; freezing the
//	description would preserve a known falsehood. The permission ladder does not
//	move either way: the same executor-or-admin gate applies whether the task is
//	open or closed, which is ruling 2 verbatim.
//
// NO LENGTH CAP, deliberately — this is a decision, not an omission, and it is
// written down because an absent guard and a forgotten guard look identical to
// whoever reads this next.
//
//	CORRECTION (T-e271 review round 5). An earlier draft of this comment argued
//	the cap would make every already-long description "permanently uneditable".
//	That reason was FALSE and is recorded here rather than deleted, because a
//	decision record that quietly swaps its reasoning teaches the next reader
//	nothing. DocCapBlocked (domain.go) refuses only when the new text is over
//	the cap AND is not shorter than what is stored — shrinking is an explicit,
//	advertised escape hatch, so an over-cap description could always still be
//	edited downward.
//
//	The reason that does survive is narrower: a ceiling applied HERE and nowhere
//	else would mean an already-long description can only ever be made shorter.
//	A correction that does not shrink it — fixing a wrong date, adding the one
//	clause that makes the scope unambiguous — would be refused at the edit door,
//	while the very same words entered freely through create_task, which has
//	never capped this field. That is this route's own failure mode (the text
//	cannot be fixed) reintroduced one door further in, just in a smaller form
//	than the earlier draft claimed.
//
// Adding the first-ever cap on this field is also a NEW protection over an
// existing wire, which is the owner's call to make, not this ticket's. If a cap
// is ever wanted it belongs on BOTH doors at once (create and edit), sized so no
// stored description is already over it.
//
// Guard order: 404 unknown task → 403 not the executor → write. There is no
// 409 anywhere on this route.
// 🔴 T-646a: the body of this route now lives in updateTaskText, shared with
// update_task_title and with update_task, which supersedes both on the MCP
// surface. ONE BEHAVIOUR CHANGED HERE and it is deliberate: the description is
// now TRIMMED of surrounding whitespace, before storage and before the
// unchanged-value comparison, which is what the title has always done. Owner
// ruling, card rc-0fb94a25a8a8 (2026-08-16), option ①. Leaving this door
// untrimmed while the new one trims would have been the same defect the ticket
// set out to remove, one door further in. Consequence worth naming: a
// description of nothing but whitespace now trims to "" and therefore CLEARS,
// where before it was stored as-is. This route stays on the HTTP surface for the
// frontend and any existing client.
func (s *apiServer) HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskDescriptionDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, nil, body.Description)
}
