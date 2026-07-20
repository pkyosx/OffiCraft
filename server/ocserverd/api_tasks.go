package main

// api_tasks.go — the M3 task surface: the shared read face (owner cockpit +
// agents), the owner actions (terminate / priority / task-card message), and
// the agent write face (create with dedupe, plan, the agent-reported state
// machine, gate arming, deps, the outsource worker's claim).
//
// Contract spine (M3 contract §B–§D):
//   * work progress is REPORTED by the executing agent; the server VALIDATES
//     transitions (illegal → 409) and never finishes the work for the agent —
//     it does not auto-advance a task FORWARD (in_progress → done is the
//     agent's alone). This is the surviving half of the old H4 ruling;
//   * waiting_owner is a card-lifecycle HOLD, bracketed entirely by the card:
//     it is ENTERED only by opening a card — open_gate (which IS an M2 reply
//     card, same machinery plus the task/step linkage) or a plain
//     create_reply_card auto-bound to the current step (inferCardTaskStep +
//     armStepWithCard) — and LEFT only when that card is answered, where the
//     server itself restores the task/step to in_progress
//     (releaseCardHold). The agent reports NEITHER side: a
//     report INTO waiting_owner is a 400 (not its lever), a report OUT of it a
//     409 (the answer drives the exit). This supersedes H4's "answering moves
//     nothing" — a task can no longer linger in waiting_owner behind an
//     already-answered card (T-68b7);
//   * done/terminated are terminal: closed_ts stamps, bound outsource
//     workers release, and every later agent push is a flat 409;
//   * create dedupes on (type_key, manual-derived dedupe_key) against
//     NON-terminal tasks only: a hit answers 200 + the existing task +
//     deduped:true (H1/H2 — dedupe is the normal path, never an error).

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// ── SSE fan helpers (spec/sse.md §2.2 — hint payloads, never full bodies) ────

func (s *apiServer) publishTask(t Task, trigger string) {
	// A task delta reaches its executor (the wake) and its creator (tracking
	// their own task), plus the owner cockpit (spec/sse.md §4). NOT dependents
	// — coordination is server deps-fulfill + agent pull, never eavesdropping
	// (owner 2026-07-15). A blank executor/creator narrows the set to owner.
	s.hub.Publish("task", "patch", "task", wireOwnerID+"::"+t.ID,
		map[string]any{"id": t.ID, "status": t.Status, "priority": t.Priority},
		audienceMembers(t.ExecutorID, t.CreatorID), trigger)
}

func (s *apiServer) publishOutsourceWorker(w OutsourceWorker, trigger string) {
	// No agent consumes outsource_worker on the wire (an ow- member row is kept
	// off every agent-facing roster surface); only the owner cockpit renders
	// the panel — owner-only.
	s.hub.Publish("outsource_worker", "patch", "outsource_worker",
		wireOwnerID+"::"+w.ID,
		map[string]any{"id": w.ID, "codename": w.Codename, "status": w.Status},
		audienceOwnerOnly(), trigger)
}

func (s *apiServer) publishTaskManual(typeKey, trigger string) {
	// No agent consumes task_manual on the wire (payload is null); the owner
	// cockpit renders the manuals face — owner-only.
	s.hub.Publish("task_manual", "patch", "task_manual",
		wireOwnerID+"::"+typeKey, nil, audienceOwnerOnly(), trigger)
}

// ── shared plumbing ──────────────────────────────────────────────────────────

// resolveTask returns the task for taskID (errNotFound when absent).
func (s *apiServer) resolveTask(taskID string) (*Task, error) {
	t, err := s.dal.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errNotFound
	}
	return t, nil
}

// taskDTOOf assembles the full served view of one task (steps + deps).
func (s *apiServer) taskDTOOf(t Task) (taskDTO, error) {
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	deps, err := s.dal.ListTaskDeps(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	dto := newTaskDTO(t, steps, deps, s.replyCardStatusesForSteps(steps))
	artifacts, err := s.taskArtifactDTOs(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	dto.Artifacts = artifacts
	return dto, nil
}

// taskArtifactDTOs lists one task's artifacts and projects them onto the wire,
// resolving the referenced chat_attachment blob metadata for file/image kinds
// (link kinds carry a bare url, no blob). A missing blob resolves to nil →
// the DTO's mime/filename/is_image stay honest-empty (never fabricated); the
// artifact row is still shown (its label/url survive a GC'd blob).
func (s *apiServer) taskArtifactDTOs(taskID string) ([]taskArtifactDTO, error) {
	arts, err := s.dal.ListTaskArtifacts(taskID)
	if err != nil {
		return nil, err
	}
	out := []taskArtifactDTO{}
	for _, a := range arts {
		var att *ChatAttachment
		if a.Kind != ArtifactKindLink && a.AttachmentID != "" {
			att, err = s.dal.GetChatAttachment(a.AttachmentID)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, newTaskArtifactDTO(a, att))
	}
	return out, nil
}

// replyCardStatusesForSteps maps each step's bound reply_card_id → the card's
// live status ("waiting"/"answered") for the read-time reply_card_status the
// task-embedded TaskReplyCard reads to lazy-load answered cards (and the board
// reads to derive the H4 badge without the child round-trip). Best-effort: a
// lookup miss/error just leaves that id out of the map → reply_card_status "".
func (s *apiServer) replyCardStatusesForSteps(steps []TaskStep) map[string]string {
	out := map[string]string{}
	for _, st := range steps {
		if st.ReplyCardID == "" {
			continue
		}
		if _, seen := out[st.ReplyCardID]; seen {
			continue
		}
		if c, err := s.dal.GetReplyCard(st.ReplyCardID); err == nil && c != nil {
			out[st.ReplyCardID] = c.Status
		}
	}
	return out
}

// stepCardSettled reports whether the step's LATEST bound reply card (the
// reply_card_id pointer — historical cards deliberately out of scope) exists
// and has left waiting through the owner side (answered / expired): the
// submit_plan preservation test of T-1aea. A card-less step, a still-waiting
// card, or a dangling pointer all read false — replaced as before.
func (s *apiServer) stepCardSettled(st TaskStep) (bool, error) {
	if st.ReplyCardID == "" {
		return false, nil
	}
	c, err := s.dal.GetReplyCard(st.ReplyCardID)
	if err != nil {
		return false, err
	}
	return c != nil && (c.Status == replyCardStatusAnswered ||
		c.Status == replyCardStatusExpired), nil
}

// writeTask is the common single-task response tail.
func (s *apiServer) writeTask(w http.ResponseWriter, t Task) {
	dto, err := s.taskDTOOf(t)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// callerMayDriveTask enforces the executor guard on the agent report routes
// (plan / status / step status / gate / deps): the caller must BE the task's
// executor — the caller-identity convention (root CLAUDE.md §14: a non-admin
// agent only ever operates itself; admin capability — owner or admin agent —
// may act on any task). False → the caller writes the 403.
func (s *apiServer) callerMayDriveTask(r *http.Request, t Task) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	return currentActor(r) == t.ExecutorID
}

// closeTask applies the terminal-status side effects (done AND terminated):
// stamp closed_ts, release every bound outsource worker (the panel row
// disappears; the row itself is the audit trail) and fan their deltas.
func (s *apiServer) closeTask(t *Task, status string, now float64, trigger string) error {
	t.Status = status
	t.ClosedTS = now
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	released, err := s.dal.ReleaseWorkersForTask(t.ID, now)
	if err != nil {
		return err
	}
	for _, w := range released {
		s.publishOutsourceWorker(w, trigger)
	}
	// The worker SESSION is deliberately NOT reclaimed here (SPEC §6.3): the
	// released worker keeps its session to run the close-out duties (learnings
	// write-back, temp cleanup, the close-out report). The reclaim fires from
	// the close-out hook (worker_spawn.go dismissOutsourceWorkersForTask — the
	// seam the close-out report handler calls) or, when no report ever
	// arrives, from the scheduler's workerReclaimGraceSecs backstop.
	s.publishTask(*t, trigger)
	// Task-close nudge band (spec/sse.md §8): remind the executor down its own
	// SSE connection to fold this run's learnings back into the type's manual.
	// Typed tasks only (ad-hoc has no manual); done AND terminated both nudge.
	// Best-effort — a fan failure must never fail the close it follows.
	// The nudge sentence carries the manual's DISPLAY label (best-effort
	// lookup — a deleted manual honestly falls back to the raw key inside
	// decideTaskCloseNudge); the MCP addressing string in the same sentence
	// stays the raw type_key (T-fa76).
	manualLabel := ""
	if t.TypeKey != "" {
		if m, err := s.dal.GetTaskManual(t.TypeKey); err == nil && m != nil {
			manualLabel = manualDisplayLabel(m.DisplayName, t.TypeKey)
		}
	}
	if sig := decideTaskCloseNudge(*t, manualLabel); sig != nil {
		if frame, err := directedFrameText(taskCloseTopic, sig); err == nil {
			s.hub.PushDirected(t.ExecutorID, frame)
		}
	}
	return nil
}

// deriveAndPersistTask is the DERIVATION SEAM (T-9ca5 "任務狀態全推導"): the single
// call every step-mutation path funnels through to re-project the task's status
// (and display waiting_reason) from its steps, persist it, and fan the delta. It
// mutates t in place. When the derivation lands on done (every step done) it
// runs the full close (closeTask: release workers, stamp closed_ts, learnings
// nudge) — that is how a task reaches done now, NOT an agent status report.
// Already-closed tasks are left untouched. The lock (task.lock) is orthogonal
// and never read here.
func (s *apiServer) deriveAndPersistTask(t *Task, now float64, trigger string) error {
	if TaskIsTerminal(t.Status) {
		return nil
	}
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		return err
	}
	if DeriveTaskStatus(steps) == TaskStatusDone {
		return s.closeTask(t, TaskStatusDone, now, trigger)
	}
	RecomputeTaskStatus(t, steps) // status + display waiting_reason
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	s.publishTask(*t, trigger)
	return nil
}

// reconcileTaskStatusesOnBoot aligns every non-terminal task's stored status
// with what its steps derive to (owner T-9ca5 ⑤: 上線時既有不一致一次對齊) — a
// one-shot at startup after task status became fully derived. Terminal tasks are
// skipped (their status is not derived). Only rows whose status or display
// waiting_reason actually drift are written; a task whose steps are all done is
// properly closed. Returns the number of tasks it corrected, for the boot log.
// No SSE fan matters here (boot has no subscribers yet).
func (s *apiServer) reconcileTaskStatusesOnBoot() (int, error) {
	tasks, err := s.dal.ListTasks()
	if err != nil {
		return 0, err
	}
	now := nowSecs()
	fixed := 0
	for i := range tasks {
		t := tasks[i]
		if TaskIsTerminal(t.Status) {
			continue
		}
		steps, err := s.dal.ListTaskSteps(t.ID)
		if err != nil {
			return fixed, err
		}
		derived := DeriveTaskStatus(steps)
		reason := ""
		for _, st := range steps {
			if st.Status == StepStatusWaitingExternal {
				reason = st.WaitingReason
				break
			}
		}
		if derived == t.Status && reason == t.WaitingReason {
			continue // already consistent
		}
		if derived == TaskStatusDone {
			if err := s.closeTask(&t, TaskStatusDone, now, "boot-reconcile"); err != nil {
				return fixed, err
			}
			fixed++
			continue
		}
		t.Status = derived
		t.WaitingReason = reason
		t.UpdatedTS = now
		if err := s.dal.PutTask(t); err != nil {
			return fixed, err
		}
		fixed++
	}
	return fixed, nil
}

// manualAssignee decodes a manual's assignee JSON ({} = unset → nil map).
func manualAssignee(m TaskManual) (map[string]any, error) {
	out := map[string]any{}
	if m.Assignee == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(m.Assignee), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── §6.2 the resume-summary task block ───────────────────────────────────────

// resumeTasksN caps the resume-summary task block (most recently updated
// first) — the wake snapshot stays bounded; page the rest with list_tasks.
const resumeTasksN = 5

// resumeTasksFor assembles the bounded task block of the wake snapshot
// (SPEC §6.2 — a handover resumes in-flight tasks, not just chat) as LIGHT
// rows (T-3f31 owner ruling: 任務不該包含細節 — no steps/DoD text ride the
// snapshot; each row names the task, its status/priority and the current node
// id + NAME, current = the first non-done step). detail_chars is the rune
// size of the plan text the row omits (Σ step name + DoD) — the
// peek-then-decide signal: the agent checks it BEFORE a get_task pull and may
// hand a large digest to a sub-agent. The second return is the caller's TOTAL
// open-task count (the overview's tasks_open_total — the rows may be fewer).
func (s *apiServer) resumeTasksFor(actor string) ([]resumeTaskDTO, int, error) {
	out := []resumeTaskDTO{}
	if actor == "" {
		return out, 0, nil
	}
	tasks, err := s.dal.ListOpenTasksByExecutor(actor, resumeTasksN)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.dal.CountOpenTasksByExecutor(actor)
	if err != nil {
		return nil, 0, err
	}
	for _, t := range tasks {
		steps, err := s.dal.ListTaskSteps(t.ID)
		if err != nil {
			return nil, 0, err
		}
		currentID, currentName := "", ""
		detailChars := 0
		for _, st := range steps {
			// current = the first non-TERMINAL step: a superseded row is
			// frozen replan history, never the working node (T-1aea).
			if currentID == "" && !StepIsTerminal(st.Status) {
				currentID, currentName = st.ID, st.Name
			}
			detailChars += len([]rune(st.Name)) + len([]rune(st.DoD))
		}
		done, stepTotal := TaskProgress(steps)
		out = append(out, resumeTaskDTO{
			ID:              t.ID,
			TaskNo:          TaskNo(t.ID),
			TypeKey:         t.TypeKey,
			Title:           t.Title,
			Status:          t.Status,
			Priority:        t.Priority,
			WaitingReason:   t.WaitingReason,
			CurrentStepID:   currentID,
			CurrentStepName: currentName,
			ProgressDone:    done,
			ProgressTotal:   stepTotal,
			DetailChars:     detailChars,
			UpdatedTS:       t.UpdatedTS,
		})
	}
	return out, total, nil
}

// ── C.1 the read face ────────────────────────────────────────────────────────

// GET /api/tasks — full task DTOs, optionally filtered (?executor= an
// executor id | "outsource" | "unassigned"; ?type= a type_key; ?status= the
// closed set). Partitioning/ordering stays the FE's (wire serves data).
//
// ?open=true (T-2b9d) is the ADDITIVE cheap-default filter: the 任務頁 opens on
// the 未結束 partition (a handful of rows) yet the unfiltered list ships the
// whole history (every done/terminated/duplicated task ever). open=true drops
// the terminal rows server-side so the default page load pulls only the tasks
// it renders; the 清除篩選 全部 view just omits the param and gets the full
// list back, byte-for-byte as before. Any value other than the literal "true"
// (including absent) leaves the full list untouched — no consumer that omits
// the param sees a behaviour change.
func (s *apiServer) HandleListTasksApiTasksGet(w http.ResponseWriter, r *http.Request, params HandleListTasksApiTasksGetParams) {
	status := trimmedOrEmpty(params.Status)
	if status != "" && !ValidTaskStatus(status) {
		writeError(w, http.StatusBadRequest,
			"status must be one of not_started, in_progress, waiting_owner, waiting_external, reassigning, done, terminated, duplicated")
		return
	}
	executor := trimmedOrEmpty(params.Executor)
	typeKey := trimmedOrEmpty(params.Type)
	openOnly := trimmedOrEmpty(params.Open) == "true"
	tasks, err := s.dal.ListTasks()
	if err != nil {
		internalError(w, err)
		return
	}
	// The light list skips the AllTaskSteps full-row scan (steps carry the
	// heavy dod/name text the collapsed card never shows) — progress is a
	// grouped COUNT instead; deps stay (light id markers the card renders).
	progressByTask, err := s.dal.AllTaskStepProgress()
	if err != nil {
		internalError(w, err)
		return
	}
	depsByTask, err := s.dal.AllTaskDeps()
	if err != nil {
		internalError(w, err)
		return
	}
	// The 「產物 N」 badge count — a grouped COUNT like progress, so the light
	// list never loads the artifact rows (get_task folds the full set).
	artifactCountByTask, err := s.dal.AllTaskArtifactCounts()
	if err != nil {
		internalError(w, err)
		return
	}
	out := []taskListItemDTO{}
	for _, t := range tasks {
		if openOnly && TaskIsTerminal(t.Status) {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		if typeKey != "" && t.TypeKey != typeKey {
			continue
		}
		switch executor {
		case "":
		case TaskExecutorOutsource:
			if t.ExecutorKind != TaskExecutorOutsource {
				continue
			}
		case "unassigned":
			if t.ExecutorKind != TaskExecutorOutsource || t.ExecutorID != "" {
				continue
			}
		default:
			if t.ExecutorID != executor {
				continue
			}
		}
		p := progressByTask[t.ID]
		out = append(out, newTaskListItemDTO(
			t, depsByTask[t.ID], p.Done, p.Total, artifactCountByTask[t.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/tasks/count — the tasks nav badge: non-terminal tasks only.
func (s *apiServer) HandleTaskCountApiTasksCountGet(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.dal.ListTasks()
	if err != nil {
		internalError(w, err)
		return
	}
	open := 0
	for _, t := range tasks {
		if !TaskIsTerminal(t.Status) {
			open++
		}
	}
	writeJSON(w, http.StatusOK, taskCountDTO{Open: open})
}

// GET /api/tasks/{task_id} — one task in full.
func (s *apiServer) HandleGetTaskApiTasksTaskIdGet(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	s.writeTask(w, *t)
}

// ── C.2 owner actions ────────────────────────────────────────────────────────

// POST /api/tasks/{task_id}/terminate — the ONLY owner-side status change
// (SPEC §3.7). Non-terminal only; the FE owns the double-confirm.
func (s *apiServer) HandleTerminateTaskApiTasksTaskIdTerminatePost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	if err := s.closeTask(t, TaskStatusTerminated, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id}/priority — high|mid|low|frozen (freeze/unfreeze
// ride the same knob; frozen is a priority, never a status — SPEC §3.3).
// T-0786: the owner keeps every value; the task's executor (and admin
// capability, §14) may set high|mid|low — but the frozen knob stays the
// owner's alone, on BOTH sides (a non-owner may neither freeze nor touch a
// frozen task, since leaving frozen IS unfreezing). Guard order: 400
// closed-set → 404 → 403 authz → 409 terminal (deny before state probing).
func (s *apiServer) HandleSetTaskPriorityApiTasksTaskIdPriorityPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskPriorityUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "priority") {
		return
	}
	priority := trimString(body.Priority)
	if !ValidTaskPriority(priority) {
		writeError(w, http.StatusBadRequest,
			"priority must be one of high, mid, low, frozen")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if s.principalOfRequest(r) != principalOwner {
		if !s.callerMayDriveTask(r, *t) {
			writeError(w, http.StatusForbidden, "caller is not the task's executor")
			return
		}
		if priority == TaskPriorityFrozen {
			writeError(w, http.StatusForbidden,
				"frozen is the owner's knob")
			return
		}
		if t.Priority == TaskPriorityFrozen {
			writeError(w, http.StatusForbidden,
				"task is frozen; only the owner may unfreeze")
			return
		}
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	t.Priority = priority
	t.UpdatedTS = nowSecs()
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id}/message — the task-card message box (owner ruling
// ②): the server posts one ORDINARY chat message owner → executor with the
// task context auto-attached in meta ({task_id, task_title, task_type}) — the
// reply-card companion-message mirror. Unassigned executor → 409.
func (s *apiServer) HandlePostTaskMessageApiTasksTaskIdMessagePost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskMessageDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if t.ExecutorID == "" {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' has no executor yet (awaiting assignment)")
		return
	}
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		for _, a := range *body.Attachments {
			if strOrEmpty(a.DataB64) != "" || trimmedOrEmpty(a.Id) != "" {
				inputs = append(inputs, a)
			}
		}
	}
	if len(inputs) > chatAttachmentsMaxCount {
		writeError(w, http.StatusBadRequest,
			"a message may carry at most 10 attachments")
		return
	}
	resolved, status, problem := s.resolveChatAttachmentInputs(inputs)
	if problem != "" {
		writeError(w, status, problem)
		return
	}
	meta := map[string]any{
		"task_id":    t.ID,
		"task_title": t.Title,
		"task_type":  t.TypeKey,
	}
	text := trimmedOrEmpty(body.Body)
	if len(resolved) > 0 {
		refs, err := s.storeResolvedAttachments(resolved)
		if err != nil {
			internalError(w, err)
			return
		}
		meta["attachments"] = refs
	} else if text == "" {
		writeError(w, http.StatusBadRequest,
			"message must carry text or an attachment")
		return
	}
	// Prefix the visible body with the task's display number so the executor's
	// chat message is self-identifying — which task this owner ruling is about
	// (owner 2026-07-14: 回覆訊息發給負責人時要看得出對應的 task ID). meta.task_id
	// stays the machine linkage; this is the human-facing label. An
	// attachment-only message (empty text) carries no prefix.
	msgBody := text
	if msgBody != "" {
		msgBody = "[" + TaskNo(t.ID) + "] " + msgBody
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    currentActor(r),
		Recipient: t.ExecutorID,
		Body:      msgBody,
		TS:        nowSecs(),
		Meta:      meta,
	}
	if err := s.dal.PutChat(msg); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), requestTrigger(r))
	writeJSON(w, http.StatusOK, s.servedChatMessageDTO(msg))
}

// POST /api/tasks/{task_id}/reassign — the owner/admin handover action
// (T-160e; MCP reassign_task, requires admin_agent — the owner and the
// assistant both drive it, the assistant only lives on the MCP face). Hands
// the task to a NEW executor: a roster member, or an UNASSIGNED outsource slot
// the scheduler mints a fresh worker for under the global parallel cap (T-35e0:
// no inline mint at reassign — the task lands unassigned + the reassigning lock;
// the dialog's model/effort/machine ride the task's outsource_target for the mint).
//
// Effects, in order:
//  1. every WAITING reply card of the task expires (the ask was the OLD
//     executor's; expired counts as settled, so a later replan freezes the
//     step as superseded history — T-1aea);
//  2. non-terminal steps fall back to pending (the new executor replans or
//     re-drives them); done/superseded rows stay untouched;
//  3. a previously bound outsource worker is dismissed (release + session
//     reclaim — the close-out hook reused);
//  4. the executor re-points and the task enters the `reassigning` handover
//     hold; ONLY the new executor leaves it (reassigning → in_progress on
//     the agent report table, executor-guarded);
//  5. each MEMBER side gets a handover chat message (the old executor is
//     told to stop + leave a handover summary; the new one to read up and
//     flip the status back — `note` rides that message); a fresh worker gets
//     the task through its boot context instead;
//  6. the task delta fans to the NEW audience via publishTask AND once,
//     explicitly, to the OLD executor (publishTask reads the row's current
//     executor, which would silently drop the person just unassigned).
//
// Identity is untouched: type/inputs/dedupe_key/task id/deps never change.
// Guards: 404 unknown task; 409 terminal or target == current executor; 400
// frozen task or an invalid target (unknown/inactive member, a warden,
// missing member_id, a bad effort).
func (s *apiServer) HandleReassignTaskApiTasksTaskIdReassignPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskReassignDTO
	if !decodeJSONBodyRequired(w, r, &body, "target") {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	// ② the route now admits any agent (was admin-only); the handover itself is
	// executor-guarded — an agent may only reassign a task it EXECUTES (owner /
	// admin capability may drive any task, callerMayDriveTask §14).
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	if t.Priority == TaskPriorityFrozen {
		writeError(w, http.StatusBadRequest,
			"task '"+taskId+"' is frozen; unfreeze it before reassigning")
		return
	}

	kind := trimString(body.Target.Kind)
	var newMember *Member
	model, effort, machine := "", "", ""
	switch kind {
	case TaskExecutorMember:
		memberID := trimmedOrEmpty(body.Target.MemberId)
		if memberID == "" {
			writeError(w, http.StatusBadRequest,
				"target.member_id is required for kind 'member'")
			return
		}
		m, err := s.dal.GetMember(memberID)
		if err != nil {
			internalError(w, err)
			return
		}
		if m == nil || m.RosterStatus != RosterStatusActive || m.Kind == KindOutsource {
			// kind=outsource is refused too (P7d fold parity): an outsource
			// member is never a 'member'-kind reassign target — outsource
			// executors are minted fresh by the outsource arm below.
			writeError(w, http.StatusBadRequest,
				"target member '"+memberID+"' is not an active roster member")
			return
		}
		if m.Kind == KindWarden {
			writeError(w, http.StatusBadRequest,
				"target member '"+memberID+"' is a machine (warden) — machines never execute tasks")
			return
		}
		if t.ExecutorKind == TaskExecutorMember && t.ExecutorID == memberID {
			writeError(w, http.StatusConflict,
				"member '"+memberID+"' is already the task's executor")
			return
		}
		newMember = m
	case TaskExecutorOutsource:
		model = trimmedOrEmpty(body.Target.Model)
		effort = trimmedOrEmpty(body.Target.Effort)
		if effort == "" {
			effort = "medium"
		}
		if !validEffort(effort) {
			writeError(w, http.StatusBadRequest,
				"target.effort must be one of low, medium, high")
			return
		}
		machine = trimmedOrEmpty(body.Target.Machine)
		if machine == "" {
			machine = "auto"
		}
	default:
		writeError(w, http.StatusBadRequest,
			"target.kind must be 'member' or 'outsource'")
		return
	}

	now := nowSecs()
	trigger := requestTrigger(r)

	// ④ an outsource target is a 發包 — it funnels through the SAME spawn gate as
	// create_task and the scheduler (no side door). owner/admin admit (their
	// reassign carries implicit approval); a subordinate agent the policy does not
	// name is denied (403) BEFORE any of the handover side effects below run. An
	// admit falls through to the handover flow, which lands an UNASSIGNED outsource
	// task (executor_id='' + outsource_target); the scheduler mints the successor
	// under the global parallel cap (T-35e0: no inline mint, no per-task card).
	if kind == TaskExecutorOutsource {
		principal := s.principalOfRequest(r)
		var initiator *Member
		if principal != principalOwner {
			initiator, _ = s.dal.GetMember(currentActor(r))
		}
		gate, err := s.outsourceSpawnGate(outsourceGateRequest{
			PrincipalClass: principal, Initiator: initiator, TaskID: t.ID,
			Model: model, Effort: effort, Machine: machine, IssuedBy: currentActor(r),
		})
		if err != nil {
			internalError(w, err)
			return
		}
		if gate.Decision == gateDeny {
			writeError(w, http.StatusForbidden,
				"not permitted to 發包 to an outsource worker: "+gate.Reason)
			return
		}
	}

	oldKind, oldExecutor := t.ExecutorKind, t.ExecutorID

	// 1. Expire every waiting card bound to the task — the exact semantics of
	// the owner's expire route (status flip + releaseCardHold + delta), run
	// server-side: the question was addressed to the OLD executor, so its
	// eventual answer is no longer reliable; the new executor opens a fresh
	// card if the question still matters.
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		internalError(w, err)
		return
	}
	for _, c := range cards {
		if c.TaskID != t.ID || c.Status != replyCardStatusWaiting {
			continue
		}
		c.Status = replyCardStatusExpired
		c.ExpiredTS = now
		if err := s.dal.PutReplyCard(c); err != nil {
			internalError(w, err)
			return
		}
		if err := s.releaseCardHold(c, trigger); err != nil {
			internalError(w, err)
			return
		}
		s.publishReplyCard(c, trigger)
	}

	// 2. Non-terminal steps fall back to pending — the new executor either
	// re-drives them or replans (submit_plan then keeps done/settled rows and
	// replaces these). Terminal rows (done / superseded) are history and stay.
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	for _, st := range steps {
		if StepIsTerminal(st.Status) || st.Status == StepStatusPending {
			continue
		}
		st.Status = StepStatusPending
		// A pending step must read as never-started: started_ts>0 is the
		// system-wide "ever entered in_progress" oracle (00028's Down recovers
		// pre-push statuses from it), so leaving it stamped would mint the
		// dirty "pending but started_ts>0" state. finished_ts can't be set on
		// a non-terminal row in the current model — zeroing it here is the
		// same never-started semantic, defensively. waiting_reason belongs to
		// waiting_external only (update_step_status clears it on every exit
		// from that state — this fallback is one more exit).
		st.StartedTS = 0
		st.FinishedTS = 0
		st.WaitingReason = ""
		if err := s.dal.PutTaskStep(st); err != nil {
			internalError(w, err)
			return
		}
	}

	// 3. The OLD outsource worker is NO LONGER dismissed HERE (T-ba04). It used
	// to be released + session-reclaimed at reassign time, which killed the
	// predecessor BEFORE any handover dialogue with the successor could happen
	// (the `reassigning` hold exists precisely to host that dialogue). Instead
	// the predecessor stays live through the hold and is fired the moment the
	// successor claims the task (claim_task handler),
	// or when the timeout reaper gives up on that report — dismissed by its own
	// WORKER ID (dismissOutsourceWorkerByID), never by task_id, so an
	// outsource→outsource takeover does not kill the fresh worker minted below
	// onto the SAME task_id. A member predecessor was never dismissed and still
	// is not — it lives on its own member lifecycle and can hand over in chat.

	// Re-read the row: the card pass (releaseCardHold) may have rewritten it.
	t, err = s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}

	// 4. Re-point the executor + enter the reassigning handover hold. A member
	// target binds directly; an outsource target lands UNASSIGNED (executor_id=''
	// + the outsource_target on the row) and the scheduler mints the successor
	// under the global parallel cap (T-35e0 — no inline mint here). The successor's
	// boot context folds the same reassigning takeover instruction, so a headless
	// worker learns whom to hand over WITH even though it is minted later.
	if kind == TaskExecutorMember {
		t.ExecutorKind = TaskExecutorMember
		t.ExecutorID = newMember.ID
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine = "", "", ""
	} else {
		t.ExecutorKind = TaskExecutorOutsource
		t.ExecutorID = ""
		t.OutsourceModel = model
		t.OutsourceEffort = effort
		t.OutsourceMachine = machine
	}
	// Enter the reassigning LOCK (T-9ca5) — orthogonal to status, which stays
	// DERIVED. The reassign reset non-terminal steps to pending above, so the
	// derived status is the honest not_started / in_progress alongside the
	// reassigning lock badge.
	t.Lock = TaskLockReassigning
	rsteps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	t.Status = DeriveTaskStatus(rsteps)
	t.WaitingReason = ""
	// Stamp the PREDECESSOR (T-ba04): the executor the task just moved AWAY from
	// — persisted so the successor's boot context / chat pairing message and the
	// cockpit 任務卡 can name who to hand over WITH, and so the takeover dismiss
	// knows which specific outsource worker to fire. Only when there WAS a prior
	// executor (a not_started task with none leaves it blank).
	if oldExecutor != "" {
		t.ReassignedFrom = oldExecutor
		t.ReassignedFromKind = oldKind
	}
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}

	// 5. Handover PAIRING messages (T-ba04). Both notices are SERVER-authored
	// (sender = wireSystemSender, not currentActor): an automated handover must
	// not read as an owner DM. They pair the two sides into a DIALOGUE —
	// predecessor: "go hand over TO the successor"; successor: "your
	// predecessor is X, confirm the handover WITH them, THEN flip the status
	// yourself". Meta carries the task linkage the task-message route
	// established. The predecessor notice fires for a member OR an outsource
	// predecessor (the outsource one is kept live through the hold, so it can
	// answer). An outsource SUCCESSOR is not minted here anymore (T-35e0 — the
	// scheduler mints it later under the cap), so there is no worker id to DM
	// yet; its boot context folds the same takeover instruction, so the successor
	// chat notice is a member-only step below.
	newExecutorLabel, newExecutorID := "", ""
	if newMember != nil {
		newExecutorID = newMember.ID
		newExecutorLabel = newMember.Name
		if newExecutorLabel == "" {
			newExecutorLabel = newMember.ID
		}
	} else {
		newExecutorLabel = "外包（待排程指派）"
	}
	no := TaskNo(t.ID)
	if oldExecutor != "" {
		s.postTaskChat(*t, wireSystemSender, oldExecutor,
			"["+no+"] 此任務已轉派給 "+newExecutorLabel+"。"+
				"請停止推進，改為去跟接手人做交接：對方接手後會主動 post_chat 找你，"+
				"回答他關於目前進度、在飛事項、要注意的坑的提問，直到他確認交接完成。交接完成後這張任務就不再是你的了。",
			trigger)
	}
	if oldExecutor != "" && newExecutorID != "" {
		predecessorLabel := s.executorLabel(oldKind, oldExecutor)
		msg := "[" + no + "] 你接手了任務「" + t.Title + "」。你的前任是 " +
			predecessorLabel + "（id `" + oldExecutor + "`）。請先跟他確認交接完成" +
			"（直接 post_chat 給他，問清楚目前進度與在飛事項），確認後再由你自己呼叫 claim_task" +
			"（認領）解除轉派鎖——只有你這個新負責人動得了；任務狀態一律照步驟推導，不必也不能自己報。"
		if note := trimmedOrEmpty(body.Note); note != "" {
			msg += "\n\n交接備註：" + note
		}
		s.postTaskChat(*t, wireSystemSender, newExecutorID, msg, trigger)
	} else if newMember != nil {
		// A not_started task with no prior executor (no predecessor to hand over
		// with) — the plain "you are now the executor" notice, member side only
		// (a fresh worker learns it through the boot context).
		msg := "[" + no + "] 你接手了任務「" + t.Title +
			"」。請先讀任務內容，準備好後由你自己呼叫 claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。"
		if note := trimmedOrEmpty(body.Note); note != "" {
			msg += "\n\n交接備註：" + note
		}
		s.postTaskChat(*t, wireSystemSender, newMember.ID, msg, trigger)
	}

	// 6. Fan the task delta: publishTask reaches the NEW executor + creator +
	// owner; the OLD executor just left that audience, so fan them once more
	// explicitly — their cockpit/agent view must learn the task moved away.
	s.publishTask(*t, trigger)
	if oldExecutor != "" && oldExecutor != t.ExecutorID {
		s.hub.Publish("task", "patch", "task", wireOwnerID+"::"+t.ID,
			map[string]any{"id": t.ID, "status": t.Status, "priority": t.Priority},
			audienceMembers(oldExecutor), trigger)
	}

	// An outsource target landed the task unassigned — fire the event-driven
	// scheduler tick so the successor is minted NOW (subject to the global cap)
	// rather than up to a cadence period later, exactly like create_task's seam.
	if kind == TaskExecutorOutsource {
		s.outsourceTickNow()
	}
	s.writeTask(w, *t)
}

// HandleClaimTaskApiTasksTaskIdClaimPost — the NEW executor takes over a
// reassigned task (MCP claim_task; T-9ca5). It CLEARS the reassigning lock and
// fires the predecessor outsource worker — the takeover that
// update_task_status's reassigning→in_progress used to do before reassigning
// became a lock (status is DERIVED, never set here). Executor-guarded: only the
// task's current executor (the successor the reassign re-pointed to) may claim;
// owner/admin may drive any task. A task not under the reassigning lock → 409
// (nothing to claim). Idempotent side effects: the predecessor dismiss is by
// its OWN worker id, never by task_id (the successor may be a fresh worker on
// the same task_id).
func (s *apiServer) HandleClaimTaskApiTasksTaskIdClaimPost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if t.Lock != TaskLockReassigning {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is not awaiting takeover (no reassigning lock)")
		return
	}
	now := nowSecs()
	trigger := requestTrigger(r)
	predecessorWorker := ""
	if t.ReassignedFromKind == TaskExecutorOutsource {
		predecessorWorker = t.ReassignedFrom
	}
	t.Lock = TaskLockNone
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, trigger)
	if predecessorWorker != "" {
		s.dismissOutsourceWorkerByID(predecessorWorker, now, trigger)
	}
	s.writeTask(w, *t)
}

// executorLabel resolves a human-facing label for a task executor given its
// kind + id (T-ba04 handover pairing): a member's display name (falling back to
// its id), or "外包 <codename>" for an outsource worker (falling back to its
// id). Best-effort — a lookup miss/error degrades to the raw id, never a blank
// or a fabricated name.
func (s *apiServer) executorLabel(kind, id string) string {
	if id == "" {
		return ""
	}
	switch kind {
	case TaskExecutorMember:
		if m, err := s.dal.GetMember(id); err == nil && m != nil && m.Name != "" {
			return m.Name
		}
	case TaskExecutorOutsource:
		if w, err := s.dal.GetOutsourceWorker(id); err == nil && w != nil && w.Codename != "" {
			return "外包 " + w.Codename
		}
	}
	return id
}

// postTaskChat posts one server-authored task-context chat message (the
// reassign handover notices — the task-message route's meta shape: task_id /
// task_title / task_type ride along for the client linkage). Best-effort on
// the fan; the durable write failing is the caller's internal error.
func (s *apiServer) postTaskChat(t Task, sender, recipient, body, trigger string) {
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    sender,
		Recipient: recipient,
		Body:      body,
		TS:        nowSecs(),
		Meta: map[string]any{
			"task_id":    t.ID,
			"task_title": t.Title,
			"task_type":  t.TypeKey,
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		outsourceLog("reassign %s: handover chat to %s failed: %v", t.ID, recipient, err)
		return
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), trigger)
}

// ── C.3 the agent write face ─────────────────────────────────────────────────

// POST /api/tasks — create a task. With a type: the manual drives required-
// input checking, the dedupe key, and the executor (assignee member → bound
// directly; outsource → unassigned, the scheduler's queue); a NON-terminal
// dedupe hit answers the EXISTING task + deduped:true (H1/H2). Without a type
// (ad-hoc): an explicit executor_member_id is mandatory.
func (s *apiServer) HandleCreateTaskApiTasksPost(w http.ResponseWriter, r *http.Request) {
	var body TaskCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "title") {
		return
	}
	title := trimString(body.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title must not be blank")
		return
	}
	priority := trimmedOrEmpty(body.Priority)
	if priority == "" {
		priority = TaskPriorityMid
	}
	if !ValidTaskPriority(priority) {
		writeError(w, http.StatusBadRequest,
			"priority must be one of high, mid, low, frozen")
		return
	}
	inputs := map[string]any{}
	if body.Inputs != nil {
		inputs = *body.Inputs
	}

	// An explicit outsource dispatch target (① agent 發包給外包) overrides the
	// manual/executor_member resolution: the task is created outsource-tracked
	// and routed through the single spawn gate below. kind absent / 'member'
	// keeps the current semantics.
	var dispatchTarget *TaskCreateTargetDTO
	if body.Target != nil && trimString(body.Target.Kind) == TaskExecutorOutsource {
		dispatchTarget = body.Target
	}

	typeKey := trimmedOrEmpty(body.TypeKey)
	executorKind := TaskExecutorMember
	executorID := ""
	dedupeKey := ""
	// Warnings ride the 200 answer (typed tasks only); they never block. nil for
	// ad-hoc — an ad-hoc task has no manual, so it has no "undefined" fields.
	var warnings []string
	if typeKey != "" {
		manual, err := s.dal.GetTaskManual(typeKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if manual == nil {
			writeError(w, http.StatusNotFound,
				"task manual '"+typeKey+"' not found")
			return
		}
		fields, err := ParseManualFields(manual.Fields)
		if err != nil {
			internalError(w, err)
			return
		}
		// Field↔input matching is normalized (case/space insensitive) and the
		// required-check, the K1 is_key-mandatory check, and the dedupe key all
		// read the SAME normalized inputs, so they can never disagree on whether
		// a field has a value.
		normInputs, keyCollisions := NormalizeInputs(inputs)
		knownFieldNorms := make(map[string]bool, len(fields))
		for _, f := range fields {
			knownFieldNorms[normalizeFieldKey(f.Name)] = true
			v, ok := normInputs[normalizeFieldKey(f.Name)]
			missing := InputValueMissing(v, ok)
			if f.Required && missing {
				writeError(w, http.StatusBadRequest,
					"required input '"+f.Name+"' is missing")
				return
			}
			// K1: an identity-key field with no usable value has no dedupe basis
			// (the second root cause of duplicate tasks, independent of the
			// name-case fold) — mandatory regardless of the field's own required.
			if f.IsKey && missing {
				writeError(w, http.StatusBadRequest,
					"identity key '"+f.Name+"' must not be empty")
				return
			}
		}
		// Warn (never block) about inputs the manual does not define, so a
		// silently-ignored field is surfaced rather than vanishing; and about
		// ambiguous keys that fold onto an already-provided field.
		var unknown []string
		for k := range inputs {
			if !knownFieldNorms[normalizeFieldKey(k)] {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		for _, k := range unknown {
			warnings = append(warnings,
				"unknown input field '"+k+"' (not defined in manual '"+typeKey+"')")
		}
		for _, k := range keyCollisions {
			warnings = append(warnings,
				"duplicate input field '"+k+"' (folds onto another provided field; ignored)")
		}
		dedupeKey = DedupeKeyValue(fields, inputs)
		assignee, err := manualAssignee(*manual)
		if err != nil {
			internalError(w, err)
			return
		}
		kind, _ := assignee["kind"].(string)
		memberID, _ := assignee["member_id"].(string)
		switch {
		case kind == TaskExecutorOutsource:
			executorKind = TaskExecutorOutsource // unassigned; the scheduler picks
		case kind == TaskExecutorMember && memberID != "":
			executorID = memberID
		}
		// Dedupe only where an identity key EXISTS (a keyless type has no
		// dedupe basis) and only against non-terminal tasks (H2).
		if dedupeKey != "" {
			existing, err := s.dal.FindOpenTaskByDedupe(typeKey, dedupeKey)
			if err != nil {
				internalError(w, err)
				return
			}
			if existing != nil {
				dto, err := s.taskDTOOf(*existing)
				if err != nil {
					internalError(w, err)
					return
				}
				writeJSON(w, http.StatusOK,
					taskCreateResultDTO{Task: dto, Deduped: true, Warnings: warnings})
				return
			}
		}
	}
	// The dispatch target forces the outsource track (its model/effort/machine
	// drive the gate + worker below), overriding any manual assignee.
	dispatchModel, dispatchEffort, dispatchMachine := "", "", ""
	if dispatchTarget != nil {
		executorKind = TaskExecutorOutsource
		executorID = ""
		dispatchModel = trimmedOrEmpty(dispatchTarget.Model)
		dispatchEffort = trimmedOrEmpty(dispatchTarget.Effort)
		if dispatchEffort == "" {
			dispatchEffort = "medium"
		}
		if !validEffort(dispatchEffort) {
			writeError(w, http.StatusBadRequest,
				"target.effort must be one of low, medium, high")
			return
		}
		dispatchMachine = trimmedOrEmpty(dispatchTarget.Machine)
		if dispatchMachine == "" {
			dispatchMachine = "auto"
		}
	}
	if executorKind == TaskExecutorMember && executorID == "" {
		executorID = trimmedOrEmpty(body.ExecutorMemberId)
		if executorID == "" {
			what := "an ad-hoc task"
			if typeKey != "" {
				what = "a type with no manual assignee"
			}
			writeError(w, http.StatusBadRequest,
				"executor_member_id is required for "+what)
			return
		}
	}
	now := nowSecs()
	t := Task{
		ID:           "t-" + newHexID(12),
		TypeKey:      typeKey,
		Title:        title,
		DedupeKey:    dedupeKey,
		Inputs:       inputs,
		Description:  strOrEmpty(body.Description),
		Status:       TaskStatusNotStarted,
		Priority:     priority,
		ExecutorKind: executorKind,
		ExecutorID:   executorID,
		// The explicit 發包 target rides on the task row (T-35e0): the scheduler
		// mints from it. Empty for a member/manual-outsource create.
		OutsourceModel:   dispatchModel,
		OutsourceEffort:  dispatchEffort,
		OutsourceMachine: dispatchMachine,
		// §14 caller-identity: the creator is the verified token sub, never a
		// request parameter — a member agent, an outsource worker, or "owner".
		CreatorID: currentActor(r),
		CreatedTS: now,
		UpdatedTS: now,
	}
	trigger := requestTrigger(r)

	// ① explicit 發包 (target.kind=outsource): every dispatch funnels through the
	// SINGLE spawn gate (④) — no side door. Run it BEFORE any PutTask so a deny
	// leaves NO orphan task (③). On admit the task lands UNASSIGNED carrying its
	// outsource_target; the event-driven scheduler tick below mints the worker
	// under the global parallel cap (T-35e0: no inline mint, no per-task card).
	if dispatchTarget != nil {
		principal := s.principalOfRequest(r)
		var initiator *Member
		if principal != principalOwner {
			initiator, _ = s.dal.GetMember(currentActor(r))
		}
		gate, err := s.outsourceSpawnGate(outsourceGateRequest{
			PrincipalClass: principal, Initiator: initiator, TaskID: t.ID,
			Model: dispatchModel, Effort: dispatchEffort, Machine: dispatchMachine,
			IssuedBy: currentActor(r),
		})
		if err != nil {
			internalError(w, err)
			return
		}
		if gate.Decision == gateDeny {
			writeError(w, http.StatusForbidden,
				"not permitted to 發包 to an outsource worker: "+gate.Reason)
			return
		}
	}

	if err := s.dal.PutTask(t); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(t, trigger)
	if t.ExecutorKind == TaskExecutorOutsource {
		// The event-driven scheduler seam (outsource_sched.go, contract §B.4):
		// an unassigned outsource task just landed — assign it NOW rather than
		// up to a cadence period later. The response deliberately serves the
		// created (unassigned) row; the assignment rides the task /
		// outsource_worker SSE deltas (reconcile-by-refetch).
		s.outsourceTickNow()
	}
	writeJSON(w, http.StatusOK,
		taskCreateResultDTO{Task: newTaskDTO(t, nil, nil, nil), Deduped: false, Warnings: warnings})
}

// POST /api/tasks/{task_id}/plan — submit/replace the plan: every
// non-preserved step is replaced wholesale; fresh steps open pending. The
// preserved prefix keeps its place ahead of the fresh plan — history is
// never rewritten: done steps (as before), already-superseded history, and
// (T-1aea) steps whose latest bound reply card was answered/expired — those
// freeze into the superseded terminal state unless the fresh plan re-lists
// them by name (then the live row continues, no copy). Step count > 0 is what
// flips the card from 規劃中 to the timeline (a projection, no stored bit).
func (s *apiServer) HandleSubmitTaskPlanApiTasksTaskIdPlanPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskPlanDTO
	if !decodeJSONBodyRequired(w, r, &body, "steps") {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	var fresh []TaskStep
	for _, ps := range body.Steps {
		name := trimString(ps.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "step name must not be blank")
			return
		}
		// Quality gate: a plan step with no Definition of Done is unverifiable —
		// the seed rule ("每個節點都要有明確 DoD") is now server-enforced, not
		// just guidance. The schema requires the key present; this requires it
		// non-blank.
		if strings.TrimSpace(ps.Dod) == "" {
			writeError(w, http.StatusBadRequest,
				"step '"+name+"' must have a non-empty definition of done")
			return
		}
		isGate := ps.IsGate != nil && *ps.IsGate
		fresh = append(fresh, TaskStep{
			ID:            "ts-" + newHexID(12),
			Name:          name,
			DoD:           ps.Dod,
			Status:        StepStatusPending,
			ParallelGroup: trimmedOrEmpty(ps.ParallelGroup),
			IsGate:        isGate,
		})
	}
	// Parallel (fork-join) shape gate: validate over the timeline exactly as
	// it will be stored — the kept prefix plus the fresh plan (domain.go
	// ValidatePlanParallelShape: no gate inside a group, groups consecutive,
	// at least two lanes per fresh group).
	existing, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	// Partition the current timeline (T-1aea). Preserved rows, in original
	// order:
	//   done             — kept as-is (unchanged behaviour);
	//   answered-card    — the LATEST bound reply card is answered/expired:
	//                      the step carries a settled question-and-answer the
	//                      replan must not erase. If the fresh plan re-lists
	//                      the node by name it is that SAME node continuing
	//                      (the row stays alive untouched — no superseded
	//                      copy); otherwise the row freezes into the
	//                      superseded terminal state (dal.ReplaceTaskPlan
	//                      stamps finished_ts = the freeze moment).
	// Everything else — pending rows AND card-less / waiting-card rows — is
	// replaced wholesale as before: a still-waiting ask keeps living in
	// chat/Ask, and releaseCardHold's step guards make the owner's later
	// answer a safe no-op on the removed step.
	freshNames := map[string]bool{}
	for _, st := range fresh {
		freshNames[st.Name] = true
	}
	var kept []TaskStep
	keptNames := map[string]bool{}
	var retainIDs, freezeIDs []string
	for _, st := range existing {
		switch {
		case st.Status == StepStatusDone:
			kept = append(kept, st)
			keptNames[st.Name] = true
		case st.Status == StepStatusSuperseded:
			// Already-frozen history from an earlier replan survives every
			// later replan too (terminal — never re-frozen, never revived).
			// Deliberately NOT in the dedupe names: unlike done, superseded
			// means the work was NOT completed, so a later plan may honestly
			// re-introduce the node — a fresh pending row with its own id
			// (the frozen row stays as history beside it).
			kept = append(kept, st)
		default:
			settled, err := s.stepCardSettled(st)
			if err != nil {
				internalError(w, err)
				return
			}
			if !settled {
				continue
			}
			kept = append(kept, st)
			keptNames[st.Name] = true
			if freshNames[st.Name] {
				retainIDs = append(retainIDs, st.ID)
			} else {
				freezeIDs = append(freezeIDs, st.ID)
			}
		}
	}
	// Whole-replace-but-keep: an executor re-listing the WHOLE plan back
	// naturally repeats the kept nodes by name (the plan wire carries no id).
	// Those nodes are preserved from the kept prefix — a fresh entry whose
	// name matches one is that same node, not a new step, so it is dropped
	// rather than appended as a duplicate pending twin (the 5→9 replan bug;
	// same name-only match for done and answered-card nodes — one ruler).
	if len(keptNames) > 0 {
		deduped := fresh[:0]
		for _, st := range fresh {
			if keptNames[st.Name] {
				continue
			}
			deduped = append(deduped, st)
		}
		fresh = deduped
	}
	// Quality gate: the stored timeline (kept prefix + fresh plan) must not
	// be empty — a task cannot be planned into zero steps (the 空殼 case). A
	// replan that keeps a prefix but adds no fresh steps still passes (the
	// rare "nothing left to do" tidy-up before a done report).
	if len(kept)+len(fresh) == 0 {
		writeError(w, http.StatusBadRequest,
			"a plan must have at least one step")
		return
	}
	if msg := ValidatePlanParallelShape(kept, fresh); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	steps, err := s.dal.ReplaceTaskPlan(t.ID, retainIDs, freezeIDs, nowSecs(), fresh)
	if err != nil {
		internalError(w, err)
		return
	}
	// task status is DERIVED (T-9ca5): a fresh plan changes the step set, so
	// re-project the task status from it (and auto-close if the plan is all-done).
	if err := s.deriveAndPersistTask(t, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	deps, err := s.dal.ListTaskDeps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newTaskDTO(*t, steps, deps, s.replyCardStatusesForSteps(steps)))
}

// POST /api/tasks/{task_id}/duplicate — mark a task duplicated, pointing at the
// ORIGINAL it duplicates (MCP mark_duplicate; T-02c9). A DEDICATED action, not
// the agent status-report path: whoever executes a duplicate shell closes it
// themselves rather than leaving the owner to terminate each by hand. duplicated
// is a third terminal status (closeTask stamps closed_ts + releases bound
// outsource workers) but it does NOT nudge the learnings write-back — a
// duplicate has no lessons (decideTaskCloseNudge excludes it). The executor
// guard applies (owner/admin may act on any task). Validation keeps the
// duplicate graph DEPTH-1 so the cockpit "重複於 T-xxxx" link resolves in one hop:
//   - the task must be non-terminal (else 409 — already closed);
//   - duplicate_of is required (422) and must be an EXISTING task (404);
//   - it may not point at itself (409);
//   - it may not point at a task that is ITSELF duplicated (409 — point at the
//     final original; the server never chases a chain);
//   - a task already pointed at as an original cannot be marked duplicated (409).
func (s *apiServer) HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskMarkDuplicateDTO
	if !decodeJSONBodyRequired(w, r, &body, "duplicate_of") {
		return
	}
	originalID := trimString(body.DuplicateOf)
	if originalID == "" {
		writeError(w, http.StatusUnprocessableEntity, "duplicate_of must not be blank")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	if originalID == t.ID {
		writeError(w, http.StatusConflict,
			"a task cannot be marked a duplicate of itself")
		return
	}
	original, err := s.dal.GetTask(originalID)
	if err != nil {
		internalError(w, err)
		return
	}
	if original == nil {
		writeError(w, http.StatusNotFound,
			"duplicate_of task '"+originalID+"' not found")
		return
	}
	if original.Status == TaskStatusDuplicated {
		writeError(w, http.StatusConflict,
			"duplicate_of task '"+originalID+"' is itself a duplicate; point at the "+
				"final original it duplicates ("+original.DuplicateOf+")")
		return
	}
	// Chain guard (T-02c9 point 3): a task already cited as an original cannot
	// itself be marked duplicated — this, with the target-not-duplicated guard
	// above, keeps the graph depth-1.
	pointedAt, err := s.dal.CountTasksDuplicatingOriginal(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if pointedAt > 0 {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already the original of another duplicate; it "+
				"cannot itself be marked duplicated")
		return
	}
	t.DuplicateOf = originalID
	t.WaitingReason = "" // duplicated is terminal; no lingering wait reason
	if err := s.closeTask(t, TaskStatusDuplicated, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id}/steps/{step_id}/status — the agent-reported step
// machine (§B.2): pending → in_progress → done, and a gate resumes
// waiting_owner → in_progress | done after the owner's answer. waiting_owner is
// not an agent-reportable status — reporting it is a 400 (the card lifecycle
// owns that entry). Timestamps stamp on the edges.
func (s *apiServer) HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepStatusUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "status") {
		return
	}
	status := trimString(body.Status)
	if !ValidStepStatus(status) {
		writeError(w, http.StatusBadRequest,
			"status must be one of pending, in_progress, waiting_owner, waiting_external, done, superseded")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return
	}
	if step == nil || step.TaskID != taskId {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	if status == StepStatusWaitingOwner {
		// The step twin of the task guard above: waiting_owner is not an
		// agent-reportable step status. A step enters it only when a reply card
		// binds onto it (open_gate / create_reply_card auto-bind) — never by a
		// separate status report. A 400 (not the state-machine 409) says this is
		// not the agent's lever.
		writeError(w, http.StatusBadRequest,
			"waiting_owner is not an agent-reportable status; a step enters it only "+
				"by opening a reply card (open_gate or create_reply_card)")
		return
	}
	if status == StepStatusSuperseded {
		// Same 400 family as waiting_owner: superseded is not the agent's
		// lever either — the server freezes a replaced answered-card step
		// itself, inside submit_plan (T-1aea). Not a state-machine 409: the
		// report is categorically outside the agent's vocabulary.
		writeError(w, http.StatusBadRequest,
			"superseded is not agent-reportable; the server freezes a replaced "+
				"step itself when a new plan is submitted (submit_plan)")
		return
	}
	if !CanAgentStepTransition(step.Status, status) {
		writeError(w, http.StatusConflict,
			"illegal step transition '"+step.Status+"' -> '"+status+"'")
		return
	}
	// waiting_external is the step's own "blocked on the outside world" lever
	// (T-9ca5, moved DOWN from the task level): entering it REQUIRES a non-blank
	// waiting_reason (422); leaving it clears the reason.
	if status == StepStatusWaitingExternal {
		reason := trimmedOrEmpty(body.WaitingReason)
		if reason == "" {
			writeError(w, http.StatusUnprocessableEntity,
				"waiting_reason is required when entering waiting_external")
			return
		}
		step.WaitingReason = reason
	} else {
		step.WaitingReason = ""
	}
	now := nowSecs()
	step.Status = status
	if status == StepStatusInProgress && step.StartedTS == 0 {
		step.StartedTS = now
	}
	if status == StepStatusDone {
		step.FinishedTS = now
	}
	if err := s.dal.PutTaskStep(*step); err != nil {
		internalError(w, err)
		return
	}
	// The task status is DERIVED from the steps now — this seam re-projects it
	// (and auto-closes on all-done). No agent task-status report is involved.
	if err := s.deriveAndPersistTask(t, now, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id}/steps/{step_id}/gate — arm a gate (contract §D):
// the SAME reply-card create machinery (validation, companion chat message,
// deltas) plus the task linkage: step → waiting_owner + reply_card_id, task →
// waiting_owner. The ONLY entry into waiting_owner. The owner answers through
// the existing reply-card answer route, where the server releases the hold and
// restores the task/step to in_progress (releaseCardHold) — it
// still never advances the work FORWARD; the agent reports done itself. A
// second gate may arm while another still waits (SPEC §3.2: one task, many
// cards) — the task leaves waiting_owner only once the LAST bound card is
// answered.
func (s *apiServer) HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body ReplyCardCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "kind", "summary", "options") {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if t.Status != TaskStatusInProgress && t.Status != TaskStatusWaitingOwner {
		writeError(w, http.StatusConflict,
			"a gate can only arm on an in_progress or waiting_owner task (is "+t.Status+")")
		return
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return
	}
	if step == nil || step.TaskID != taskId {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	// A plain (is_gate=false) step is armable too: open_gate on the current node
	// is a legitimate ad-hoc 請示, the explicit twin of create_reply_card's
	// auto-bind, which already arms whatever step is current without an is_gate
	// check. is_gate is a plan-declared property (submit_plan) — arming does not
	// rewrite it; the step becomes a card-carrying plain step (get_task's step
	// view carries the reply_card_id). Only a terminal step is refused: done
	// (nothing waits any more) and superseded (frozen replan history — its
	// bound card pointer is part of the audit trail and must not be re-armed).
	if StepIsTerminal(step.Status) {
		writeError(w, http.StatusConflict,
			"step '"+stepId+"' is already "+step.Status)
		return
	}
	card, problem, err := s.openReplyCard(currentActor(r), body, t.ID, step.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if err := s.armStepWithCard(t, step, card.ID, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeReplyCard(w, *card)
}

// armStepWithCard applies the card→step waiting state machine shared by the
// TWO card-open paths — the explicit open_gate arming and create_reply_card's
// auto binding (inferCardTaskStep): the step enters waiting_owner carrying
// the CURRENT card (reply_card_id points at the latest ask; the card's own
// task/step birth marks keep the full history), started_ts stamps on first
// touch, and the task follows into waiting_owner — UNLESS the step sits
// inside a parallel group, where flipping the WHOLE task would lie while
// sibling lanes still run (the ValidatePlanParallelShape rationale; fresh
// gates can never be grouped, so only legacy data and auto-bound plain steps
// hit that branch). The owner's later answer releases this hold —
// releaseCardHold restores the step (and task) to in_progress;
// from there the agent reports the step forward itself.
func (s *apiServer) armStepWithCard(t *Task, step *TaskStep, cardID, trigger string) error {
	now := nowSecs()
	step.Status = StepStatusWaitingOwner
	step.ReplyCardID = cardID
	if step.StartedTS == 0 {
		step.StartedTS = now
	}
	if err := s.dal.PutTaskStep(*step); err != nil {
		return err
	}
	// task status is DERIVED (T-9ca5): a step in waiting_owner derives the task
	// to waiting_owner (priority head), for a lane step too — the old
	// parallel-group carve-out is gone (owner ruling: any step 等我回覆 → task
	// 等我回覆).
	return s.deriveAndPersistTask(t, now, trigger)
}

// POST /api/tasks/{task_id}/deps — replace the blocking-deps list wholesale.
// Pure display markers (SPEC §3.5): the status never moves. Self-reference /
// unknown ids → 422.
func (s *apiServer) HandleSetTaskDepsApiTasksTaskIdDepsPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskDepsDTO
	if !decodeJSONBodyRequired(w, r, &body, "blocked_by") {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	seen := map[string]bool{}
	var blockedBy []string
	for _, raw := range body.BlockedBy {
		id := trimString(raw)
		if id == "" || seen[id] {
			continue
		}
		if id == t.ID {
			writeError(w, http.StatusUnprocessableEntity,
				"a task cannot block on itself")
			return
		}
		blocker, err := s.dal.GetTask(id)
		if err != nil {
			internalError(w, err)
			return
		}
		if blocker == nil {
			writeError(w, http.StatusUnprocessableEntity,
				"unknown blocking task '"+id+"'")
			return
		}
		seen[id] = true
		blockedBy = append(blockedBy, id)
	}
	if err := s.dal.ReplaceTaskDeps(t.ID, blockedBy); err != nil {
		internalError(w, err)
		return
	}
	t.UpdatedTS = nowSecs()
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id}/closeout — the executor reports the task's
// close-out follow-ups DONE (SPEC §6.3 step 1: learnings written back +
// scratch cleaned). TERMINAL tasks only (an open task has nothing to close
// out → 409); executor-guarded like every agent report row. IDEMPOTENT: the
// first report stamps closeout_ts and fans a task delta; a repeat is a 200
// no-op (no write, no fan).
//
// SPEC §6.3 step 2 (the former worker-lifecycle SEAM, now WIRED): the FIRST
// successful report also dismisses the outsource worker(s) bound to this task
// — dismissOutsourceWorkersForTask (worker_spawn.go) releases any lingering
// row and pushes the EXACT worker_stop so the session is reclaimed NOW rather
// than waiting out the workerReclaimGraceSecs backstop. Idempotent and a
// no-op for member-executed tasks (no worker rows), so it rides the stamp
// path unconditionally; the repeat-report path never re-fires it.
func (s *apiServer) HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if !TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is still open ("+t.Status+
				") — close-out is reported after the task ends")
		return
	}
	if t.CloseoutTS > 0 {
		s.writeTask(w, *t) // already reported — idempotent no-op
		return
	}
	now := nowSecs()
	t.CloseoutTS = now
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	// §6.3 step 2: the close-out is durable — fire the bound outsource
	// worker(s) NOW (release any lingering row + reclaim the session EXACTLY).
	// Idempotent; member-executed tasks have no worker rows → no-op.
	s.dismissOutsourceWorkersForTask(t.ID, now, requestTrigger(r))
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// ── C.4 artifact set (T-3dc5) ────────────────────────────────────────────────

// POST /api/tasks/{task_id}/artifact — the executing agent pins one deliverable
// onto the task's artifact set (MCP add_task_artifact). Append-only and
// repeatable. file/image reference a chat_attachment blob (attachment_id from a
// prior POST /api/chat/attachments — one blob mechanism, not two); link carries
// a bare url (no upload). Guard order: 400 closed-set kind → 404 task → 403 not
// the executor (admin excepted, §14) → 409 terminal → 400 missing/dangling ref.
func (s *apiServer) HandleAddTaskArtifactApiTasksTaskIdArtifactPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskArtifactInputDTO
	if !decodeJSONBodyRequired(w, r, &body, "kind") {
		return
	}
	kind := trimString(body.Kind)
	if !ValidArtifactKind(kind) {
		writeError(w, http.StatusBadRequest,
			"kind must be one of file, image, link")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is closed ("+t.Status+") — its deliverables are frozen")
		return
	}
	art := TaskArtifact{
		ID:        "ta-" + newHexID(12),
		TaskID:    t.ID,
		Kind:      kind,
		Label:     trimmedOrEmpty(body.Label),
		CreatedTS: nowSecs(),
		// §14 caller-identity: the registrar is the verified token sub.
		CreatedBy: currentActor(r),
	}
	if kind == ArtifactKindLink {
		url := trimmedOrEmpty(body.Url)
		if url == "" {
			writeError(w, http.StatusBadRequest,
				"url is required for a link artifact")
			return
		}
		art.URL = url
	} else {
		attID := trimmedOrEmpty(body.AttachmentId)
		if attID == "" {
			writeError(w, http.StatusBadRequest,
				"attachment_id is required for a "+kind+" artifact")
			return
		}
		att, err := s.dal.GetChatAttachment(attID)
		if err != nil {
			internalError(w, err)
			return
		}
		if att == nil {
			writeError(w, http.StatusBadRequest,
				"attachment '"+attID+"' not found (upload it first via POST /api/chat/attachments)")
			return
		}
		art.AttachmentID = attID
	}
	if err := s.dal.PutTaskArtifact(art); err != nil {
		internalError(w, err)
		return
	}
	// The artifact set rides the EXISTING task topic (recon §4: no 13th SSE
	// topic) — the read face folds artifacts, so a plain task patch re-hydrates
	// the card's count + popover. The task row itself is unchanged (artifacts
	// are their own rows), so updated_ts is deliberately NOT bumped.
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// DELETE /api/tasks/{task_id}/artifact/{artifact_id} — un-pin one artifact (MCP
// remove_task_artifact). SAME permission model as add (owner ruling 2026-07-18):
// the task's executor may remove its own deliverables, admin/owner may remove on
// any task (§14). Guard order: 404 task → 403 not the executor → 404 artifact →
// 400 the artifact belongs to a different task. The referenced blob is left
// intact (it may be shared with a chat message; the blob store has no delete path).
func (s *apiServer) HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(w http.ResponseWriter, r *http.Request, taskId, artifactId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	art, err := s.dal.GetTaskArtifact(artifactId)
	if err != nil {
		internalError(w, err)
		return
	}
	if art == nil {
		writeError(w, http.StatusNotFound, "artifact '"+artifactId+"' not found")
		return
	}
	if art.TaskID != t.ID {
		writeError(w, http.StatusBadRequest,
			"artifact '"+artifactId+"' does not belong to task '"+taskId+"'")
		return
	}
	if _, err := s.dal.DeleteTaskArtifact(artifactId); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// GET /api/self/task — the outsource worker's claim (identity-locked, the
// resume-summary pattern: the caller's JWT sub IS the worker id). The first
// claim flips assigned → active. Any caller with no live worker row — every
// roster member included — is a 404.
func (s *apiServer) HandleGetMyTaskApiSelfTaskGet(w http.ResponseWriter, r *http.Request) {
	sub := currentActor(r)
	worker, err := s.dal.GetOutsourceWorker(sub)
	if err != nil {
		internalError(w, err)
		return
	}
	if worker == nil || worker.Status == WorkerStatusReleased {
		writeError(w, http.StatusNotFound,
			"no outsource task is bound to the caller")
		return
	}
	t, err := s.dal.GetTask(worker.TaskID)
	if err != nil {
		internalError(w, err)
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound,
			"no outsource task is bound to the caller")
		return
	}
	if worker.Status == WorkerStatusAssigned {
		worker.Status = WorkerStatusActive
		if err := s.dal.PutOutsourceWorker(*worker); err != nil {
			internalError(w, err)
			return
		}
		s.publishOutsourceWorker(*worker, requestTrigger(r))
	}
	taskView, err := s.taskDTOOf(*t)
	if err != nil {
		internalError(w, err)
		return
	}
	out := myTaskDTO{Task: taskView}
	if t.TypeKey != "" {
		manual, err := s.dal.GetTaskManual(t.TypeKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if manual != nil {
			dto, err := newTaskManualDTO(*manual)
			if err != nil {
				internalError(w, err)
				return
			}
			out.Manual = &dto
		}
	}
	writeJSON(w, http.StatusOK, out)
}
