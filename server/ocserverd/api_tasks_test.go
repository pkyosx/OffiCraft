// Skeleton generated from server/ocserverd/api_tasks.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestTaskLog(t *testing.T) {
	t.Skip("TODO: taskLog emits one task-lifecycle observability line to stderr.")
}

func TestPublishTask(t *testing.T) {
	t.Skip("TODO: ── SSE fan helpers (spec/sse.md §2.2 — hint payloads, never full bodies) ────")
}

func TestInheritDispatchSpec(t *testing.T) {
	t.Skip("TODO: inheritDispatchSpec fills the fields a 發包 left unset.")
}

func TestFillDispatchSpecFrom(t *testing.T) {
	t.Skip("TODO: fillDispatchSpecFrom copies ONE source's fields into the slots spec leaves empty — never over an already-decided field, and applying NO defaults of its own (defaults belong to defaultedDispatchSpec, once, after every source has had its turn; baked in here they would pre-empt a later source's runtime and then drop its model as \"another runtime's\").")
}

func TestDefaultedDispatchSpec(t *testing.T) {
	t.Skip("TODO: defaultedDispatchSpec applies the only two defaults there are: a runtime, and an effort.")
}

func TestPublishOutsourceWorker(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPublishTaskManual(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestResolveTask(t *testing.T) {
	t.Skip("TODO: ── shared plumbing ────────────────────────────────────────────────────────── resolveTask returns the task for taskID (errNotFound when absent).")
}

func TestTaskDTOOf(t *testing.T) {
	t.Skip("TODO: taskDTOOf assembles the full served view of one task (steps + deps).")
}

func TestBlockingTasksOf(t *testing.T) {
	t.Skip("TODO: blockingTasksOf resolves the REVERSE dependency edge of one task (T-91): the non-terminal tasks that name it in their own blocked_by, as the same display refs the forward direction serves.")
}

func TestTaskArtifactDTOs(t *testing.T) {
	t.Skip("TODO: taskArtifactDTOs lists one task's artifacts and projects them onto the wire, resolving the referenced chat_attachment for EVERY kind since T-92 — a link's target now lives in a text/uri-list blob, so a link row needs its blob too, and it needs the BYTES rather than only the metadata.")
}

func TestReplyCardStatusesForSteps(t *testing.T) {
	t.Skip("TODO: replyCardStatusesForSteps maps each step's bound reply_card_id → the card's live status (\"waiting\"/\"answered\") for the read-time reply_card_status the task-embedded TaskReplyCard reads to lazy-load answered cards (and the board reads to derive the H4 badge without the child round-trip).")
}

func TestStepCardSettled(t *testing.T) {
	t.Skip("TODO: stepCardSettled reports whether the step's LATEST bound reply card (the reply_card_id pointer — historical cards deliberately out of scope) exists and has left waiting through a settling action (answered / expired): the submit_plan preservation test of T-1aea.")
}

func TestWriteTask(t *testing.T) {
	t.Skip("TODO: writeTask is the common single-task response tail.")
}

func TestWriteTaskWriteReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskWriteReceipt is the common tail of the EIGHT task-driving writes that used to answer with the whole taskDTO (T-91): update_task and its title and description twins, claim, reassign, terminate, mark_duplicate and set_task_deps.")
}

func TestWriteTaskArtifactReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskArtifactReceipt is the common tail of the two artifact writes: the artifact just touched plus the resulting set size (T-a98d — these used to answer with the whole task, ~80k characters for a one-line pin).")
}

func TestWriteTaskCloseoutReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskCloseoutReceipt is the common tail of BOTH close-out exits — the first (stamping) report and the idempotent no-op repeat (T-bb70).")
}

func TestWriteTaskStepStatusReceipt(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestCallerMayDriveTask(t *testing.T) {
	t.Skip("TODO: callerMayDriveTask enforces the executor guard on the agent report routes (plan / status / step status / gate / deps): the caller must BE the task's executor — the caller-identity convention (root CLAUDE.md §14: a non-admin agent only ever operates itself; admin capability — owner or admin agent — may act on any task).")
}

func TestCallerMayEditTaskText(t *testing.T) {
	t.Skip("TODO: callerMayEditTaskText is callerMayDriveTask widened by exactly one structural fact: while a task has NO executor at all (executor_id == \"\"), its CREATOR counts as the executor — but only at the text-only doors (T-52).")
}

func TestCallerMayWriteHandover(t *testing.T) {
	t.Skip("TODO: callerMayWriteHandover is callerMayDriveTask PLUS one narrow, time-boxed exception (T-91): while a task sits under the `reassigning` lock, the PREDECESSOR stamped on it may still write the handover record.")
}

func TestTaskCallerOf(t *testing.T) {
	t.Skip("TODO: taskCallerOf resolves the caller's facets from the verified claims (the twin of resolvePrincipal that also hands back the member row).")
}

func TestAuthorizeTaskCreate(t *testing.T) {
	t.Skip("TODO: authorizeTaskCreate is the caller-side create gate of the 正職授權矩陣 (T-23cf phase 2).")
}

func TestCloseTask(t *testing.T) {
	t.Skip("TODO: closeTask applies the terminal-status side effects (done AND terminated): stamp closed_ts, retire every waiting reply card still bound to the task, release every bound outsource worker (the panel row disappears; the row itself is the audit trail) and fan their deltas.")
}

func TestNameWithIDSlot(t *testing.T) {
	t.Skip("TODO: nameWithIDSlot composes the ONE slot that has to carry TWO facts: 「銀月（mira）」.")
}

func TestDeriveAndPersistTask(t *testing.T) {
	t.Skip("TODO: deriveAndPersistTask is the DERIVATION SEAM (T-9ca5 \"任務狀態全推導\"): the single call every step-mutation path funnels through to re-project the task's status (and display waiting_reason) from its steps, persist it, and fan the delta.")
}

func TestReconcileTaskStatusesOnBoot(t *testing.T) {
	t.Skip("TODO: reconcileTaskStatusesOnBoot aligns every non-terminal task's stored status with what its steps derive to (owner T-9ca5 ⑤: 上線時既有不一致一次對齊) — a one-shot at startup after task status became fully derived.")
}

func TestManualAssignee(t *testing.T) {
	t.Skip("TODO: manualAssignee decodes a manual's assignee JSON ({} = unset → nil map).")
}

func TestResumeTasksFor(t *testing.T) {
	t.Skip("TODO: resumeTasksFor assembles the bounded task block of the wake snapshot (SPEC §6.2 — a handover resumes in-flight tasks, not just chat) as LIGHT rows (T-3f31 owner ruling: 任務不該包含細節 — no steps/DoD text ride the snapshot; each row names the task, its status/priority and the current node id + NAME, current = the first non-done step).")
}

func TestHandleListTasksApiTasksGet(t *testing.T) {
	t.Run("a well-formed GET /api/tasks answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/tasks reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestParseTaskStatusSet(t *testing.T) {
	t.Skip("TODO: parseTaskStatusSet folds the repeatable ?statuses= param into a lookup set.")
}

func TestTaskStatusSetMatch(t *testing.T) {
	t.Skip("TODO: taskStatusSetMatch reports whether one task belongs to a ?statuses= set.")
}

func TestHandleTaskCountApiTasksCountGet(t *testing.T) {
	t.Run("a well-formed GET /api/tasks/count answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/count request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/tasks/count reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/count request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetTaskApiTasksTaskIdGet(t *testing.T) {
	t.Run("a well-formed GET /api/tasks/{task_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/tasks/{task_id} reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestCallerMayTerminateTask(t *testing.T) {
	t.Skip("TODO: ── C.2 owner actions ──────────────────────────────────────────────────────── callerMayTerminateTask is the terminate gate.")
}

func TestHandleTerminateTaskApiTasksTaskIdTerminatePost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/terminate answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/terminate request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/terminate reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/terminate request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleSetTaskPriorityApiTasksTaskIdPriorityPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/priority answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/priority request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/priority reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/priority request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandlePostTaskMessageApiTasksTaskIdMessagePost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/message answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/message request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/message reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/message request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReassignTaskApiTasksTaskIdReassignPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/reassign answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/reassign request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/reassign reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/reassign request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleClaimTaskApiTasksTaskIdClaimPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/claim answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/claim request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/claim reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/claim request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestExecutorLabel(t *testing.T) {
	t.Skip("TODO: executorLabel resolves a human-facing label for a task executor given its kind + id (T-ba04 handover pairing): a member's display name (falling back to its id), or \"外包 <codename>\" for an outsource worker (falling back to its id).")
}

func TestPostTaskChat(t *testing.T) {
	t.Skip("TODO: postTaskChat posts one server-authored task-context chat message (the reassign handover notices — the task-message route's meta shape: task_id / task_title / task_type ride along for the client linkage).")
}

func TestHandleCreateTaskApiTasksPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleSubmitTaskPlanApiTasksTaskIdPlanPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/plan answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/plan request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/plan reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/plan request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/duplicate answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/duplicate request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/duplicate reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/duplicate request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/steps/{step_id}/status answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/status request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/steps/{step_id}/status reaches this handler with task_id, step_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/steps/{step_id}/status request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestArmStepWithCard(t *testing.T) {
	t.Skip("TODO: armStepWithCard applies the card→step waiting state machine behind the ONE card-open path — create_reply_card carrying an explicit linked_task {task_id, step_id} (T-18 collapsed the two entrances into it): the step enters waiting_owner carrying the CURRENT card (reply_card_id points at the latest ask; the card's own task/step birth marks keep the full history), started_ts stamps on first touch, and the task follows into waiting_owner — UNLESS the step sits inside a parallel group, where flipping the WHOLE task would lie while sibling lanes still run (the ValidatePlanParallelShape rationale).")
}

func TestHandleSetTaskDepsApiTasksTaskIdDepsPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/deps answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/deps request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/deps reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/deps request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/closeout answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/closeout request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/closeout reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/closeout request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestArtifactTextOrError(t *testing.T) {
	t.Skip("TODO: artifactTextOrError validates the pair and writes the 400 itself, returning ok=false when it did.")
}

func TestMintLinkTargetBlob(t *testing.T) {
	t.Skip("TODO: mintLinkTargetBlob turns a link target into the blob that will hold it, and returns the id to point the artifact at.")
}

func TestHandleAddTaskArtifactApiTasksTaskIdArtifactPost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/artifact answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/artifact reaches this handler with task_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(t *testing.T) {
	t.Run("a well-formed DELETE /api/tasks/{task_id}/artifact/{artifact_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/tasks/{task_id}/artifact/{artifact_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to DELETE /api/tasks/{task_id}/artifact/{artifact_id} reaches this handler with task_id, artifact_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a DELETE /api/tasks/{task_id}/artifact/{artifact_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestTaskFrozenDeliverablesRefusal(t *testing.T) {
	t.Skip("TODO: taskFrozenDeliverablesRefusal is the ONE sentence all three artifact verbs refuse a closed task with.")
}

func TestArtifactOnTask(t *testing.T) {
	t.Skip("TODO: artifactOnTask resolves the (task, artifact) pair the per-artifact routes address and answers every guard they share, in the ONE order the wire documents for the WRITE verbs: 404 task → 403 not the executor (admin excepted, §14) → 409 the task is closed → 404 artifact → 400 the artifact belongs to a different task.")
}

func TestHandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost(t *testing.T) {
	t.Run("a well-formed POST /api/tasks/{task_id}/artifact/{artifact_id}/replace answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact/{artifact_id}/replace request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/tasks/{task_id}/artifact/{artifact_id}/replace reaches this handler with task_id, artifact_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/tasks/{task_id}/artifact/{artifact_id}/replace request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestWriteTaskArtifactReplaceReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskArtifactReplaceReceipt is the bounded answer BOTH replace doors give — the JSON one and the raw-body one.")
}

func TestArtifactLinkURLRefusal(t *testing.T) {
	t.Skip("TODO: artifactLinkURLRefusal validates a link artifact's url and answers the refusal sentence, or \"\" when the url passes.")
}

func TestArtifactKindRefusal(t *testing.T) {
	t.Skip("TODO: artifactKindRefusal is the one sentence every cross-kind replacement is refused with — written once so the three ways to ask for one (an explicit kind, a url on a file, an attachment_id on a link) cannot answer differently about the same rule.")
}

func TestHandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet(t *testing.T) {
	t.Run("a well-formed GET /api/tasks/{task_id}/artifact/{artifact_id}/history answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id}/artifact/{artifact_id}/history request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/tasks/{task_id}/artifact/{artifact_id}/history reaches this handler with task_id, artifact_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/tasks/{task_id}/artifact/{artifact_id}/history request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
