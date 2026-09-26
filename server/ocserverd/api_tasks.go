package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

func taskLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[task] "+format+"\n", args...)
}

func (s *apiServer) publishTask(t Task, trigger string) {
	// Audience is the executor (plus the owner cockpit) only — never the creator
	// or dependents (owner ruling rc-0994e949872e option ①; creators pull via
	// list_tasks). Do not add an "important events only" fan: option ② was rejected.
	s.hub.Publish("task", "patch", "task", wireOwnerID+"::"+t.ID,
		map[string]any{"id": t.ID, "status": t.Status, "priority": t.Priority},
		audienceMembers(t.ExecutorID), trigger)
}

type dispatchSpec struct {
	Runtime string
	Model   string
	Effort  string
	Machine string
}

// inheritDispatchSpec fills the fields a 發包 left unset; an explicit field
// always wins. Two cases, not a priority order (owner ruling 2026-07-26,
// server/CLAUDE.md §8): typed → the manual's outsource assignee, ad-hoc → the
// dispatcher's own spec; the dispatcher pass only fills what the manual left
// blank. Reading it as one global priority order once broke every typed dispatch.
//
// Known limitation: outsourceSpecOf pre-fills Runtime=claude, so a codex creator's
// typed task snapshots the creator's codex Machine but not its Model, and the
// worker stalls with machine_unavailable.
func inheritDispatchSpec(spec dispatchSpec, manualSpec *outsourceTypeSpec, dispatcher *Member) dispatchSpec {
	if manualSpec != nil {
		spec = fillDispatchSpecFrom(spec, dispatchSpec{
			Runtime: manualSpec.Runtime, Model: manualSpec.Model,
			Effort: manualSpec.Effort, Machine: manualSpec.Machine})
	}
	if dispatcher != nil {
		spec = fillDispatchSpecFrom(spec, dispatchSpec{
			Runtime: dispatcher.Runtime, Model: dispatcher.Model,
			Effort: dispatcher.Effort, Machine: dispatcher.DesiredMachineID})
	}
	return defaultedDispatchSpec(spec)
}

func fillDispatchSpecFrom(spec, src dispatchSpec) dispatchSpec {
	// srcRuntime != "" is required: NormalizeRuntime maps blank to claude, which
	// would pair a runtime-less source's codex model with a claude boot.
	srcRuntime := strings.TrimSpace(src.Runtime)
	if spec.Runtime == "" && srcRuntime != "" && ValidRuntime(NormalizeRuntime(srcRuntime)) {
		spec.Runtime = NormalizeRuntime(srcRuntime)
	}
	if spec.Model == "" && srcRuntime != "" && NormalizeRuntime(srcRuntime) == spec.Runtime {
		spec.Model = src.Model
	}
	if spec.Effort == "" && validEffort(src.Effort) {
		spec.Effort = src.Effort
	}
	if spec.Machine == "" {
		spec.Machine = src.Machine
	}
	return spec
}

// defaultedDispatchSpec deliberately has no machine default (owner ruling
// 2026-07-25): an unchosen placement is not a placement, and the spawn seam
// (pickWorkerWarden) fails closed with a visible reason.
func defaultedDispatchSpec(spec dispatchSpec) dispatchSpec {
	if spec.Runtime == "" {
		spec.Runtime = RuntimeClaude
	}
	if spec.Effort == "" {
		spec.Effort = "medium"
	}
	return spec
}

func (s *apiServer) publishOutsourceWorker(w OutsourceWorker, trigger string) {
	s.publishMemberPatch(memberFromWorker(w), trigger)
}

func (s *apiServer) publishTaskManual(typeKey, trigger string) {
	s.hub.Publish("task_manual", "patch", "task_manual",
		wireOwnerID+"::"+typeKey, nil, audienceOwnerOnly(), trigger)
}

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

func (s *apiServer) taskDTOOf(t Task) (taskDTO, error) {
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	deps, err := s.dal.ListTaskDeps(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	dto := newTaskDTO(t, steps, deps, s.replyCardStatusesForSteps(steps), s.stepNoteCap())
	artifactCount, err := s.dal.CountTaskArtifacts(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	dto.ArtifactCount = artifactCount
	blocking, err := s.tasksWaitingOn(t.ID)
	if err != nil {
		return taskDTO{}, err
	}
	dto.Blocking = blocking
	return dto, nil
}

// tasksWaitingOn: the blocker's side is delivered only by this read — no
// message is ever sent to it (owner ruling). Terminal waiters are filtered here,
// not in SQL, so the query stays the one releaseDependentsOnClose uses.
func (s *apiServer) tasksWaitingOn(taskID string) ([]taskDepRefDTO, error) {
	waiters, err := s.dal.ListTasksBlockedBy(taskID)
	if err != nil {
		return nil, err
	}
	out := []taskDepRefDTO{}
	for _, w := range waiters {
		if TaskIsTerminal(w.Status) {
			continue
		}
		out = append(out, taskDepRefDTO{
			ID: w.ID, TaskNo: TaskNo(w.ID), Title: w.Title, Status: w.Status,
		})
	}
	return out, nil
}

func (s *apiServer) taskArtifactDTOs(taskID string) ([]taskArtifactDTO, error) {
	arts, err := s.dal.ListTaskArtifacts(taskID)
	if err != nil {
		return nil, err
	}
	retained, err := s.dal.TaskArtifactHistoryCounts(taskID)
	if err != nil {
		return nil, err
	}
	out := []taskArtifactDTO{}
	for _, a := range arts {
		var att *ChatAttachment
		if a.AttachmentID != "" {
			att, err = s.dal.GetTaskArtifactBlob(a.AttachmentID)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, newTaskArtifactDTO(a, att, retained[a.ID]))
	}
	return out, nil
}

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

func (s *apiServer) writeTask(w http.ResponseWriter, t Task) {
	dto, err := s.taskDTOOf(t)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// writeTaskWriteReceipt returns the description as size + sha256 so a caller can
// confirm what landed without the text: the update writes trim, create_task does not.
func (s *apiServer) writeTaskWriteReceipt(w http.ResponseWriter, t Task) {
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	deps, err := s.dal.ListTaskDeps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if deps == nil {
		deps = []string{}
	}
	arts, err := s.dal.ListTaskArtifacts(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(steps)
	var closedTS *float64
	if t.ClosedTS > 0 {
		closedTS = &t.ClosedTS
	}
	writeJSON(w, http.StatusOK, taskWriteReceiptDTO{
		TaskID:               t.ID,
		Title:                t.Title,
		Status:               t.Status,
		ExecutorID:           t.ExecutorID,
		ExecutorKind:         t.ExecutorKind,
		Lock:                 t.Lock,
		ClosedTS:             closedTS,
		DuplicateOf:          t.DuplicateOf,
		Deps:                 deps,
		ProgressDone:         done,
		ProgressTotal:        total,
		ArtifactCount:        len(arts),
		DescriptionSizeChars: utf8.RuneCountInString(t.Description),
		DescriptionSha256:    receiptSha256(t.Description),
	})
}

func (s *apiServer) writeTaskArtifactReceipt(w http.ResponseWriter, t Task, artifactID string) {
	arts, err := s.dal.ListTaskArtifacts(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskArtifactReceiptDTO{
		TaskID: t.ID, ArtifactID: artifactID, ArtifactCount: len(arts),
	})
}

func (s *apiServer) writeTaskStepStatusReceipt(w http.ResponseWriter, t Task, step TaskStep) {
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(steps)
	var closedTS *float64
	if t.ClosedTS > 0 {
		closedTS = &t.ClosedTS
	}
	writeJSON(w, http.StatusOK, taskStepStatusReceiptDTO{
		TaskID: t.ID, StepID: step.ID, StepStatus: step.Status,
		WaitingReason: t.WaitingReason, TaskStatus: t.Status,
		ClosedTS: closedTS, ProgressDone: done, ProgressTotal: total,
	})
}

// taskActorRefusal is matched verbatim by the range tests (the error code is
// derived from the HTTP status, so the sentence is what tells guards apart).
// Reword it only here; never fork the literal at a call site or in a test.
const taskActorRefusal = "caller is not the task's executor"

func underHandover(t Task) bool {
	return t.Lock == TaskLockReassigning && t.ReassignedFrom != ""
}

// predecessorHoldsTask: released workers and dismissed members are both
// roster_status=removed. Fail-closed on a lookup error, unlike authz.go's
// fail-open revocation gate — this grants rights beyond the executor rule.
func (s *apiServer) predecessorHoldsTask(t Task) bool {
	if !underHandover(t) {
		return false
	}
	m, err := s.dal.GetMember(t.ReassignedFrom)
	return err == nil && m != nil && m.RosterStatus != RosterStatusRemoved
}

func (s *apiServer) actingExecutorOf(t Task) string {
	if !underHandover(t) {
		return t.ExecutorID
	}
	if s.predecessorHoldsTask(t) {
		return t.ReassignedFrom
	}
	return ""
}

func (s *apiServer) callerMayDriveTask(r *http.Request, t Task) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	return currentActor(r) == s.actingExecutorOf(t)
}

func (s *apiServer) callerMayClaimTask(r *http.Request, t Task) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	return currentActor(r) == t.ExecutorID
}

// callerMayEditTaskText lets the creator act as executor while nobody holds the
// executor's rights (a 發包 ticket the scheduler has not bound yet — possibly
// forever), and only at the text doors: owner ruling rc-1bb6e01c4bf7
// 「只開『改文字類』那幾道（改描述／標題／產物增刪／步驟筆記），不含凍結、撤票、改派」.
// Calling it from any other handler reverses that ruling.
func (s *apiServer) callerMayEditTaskText(r *http.Request, t Task) bool {
	if s.callerMayDriveTask(r, t) {
		return true
	}
	if underHandover(t) || t.ExecutorID != "" || t.CreatorID == "" {
		return false
	}
	return currentActor(r) == t.CreatorID
}

type taskCaller struct {
	principal principalClass
	actorID   string
	member    *Member
}

func (c taskCaller) isOutsource() bool    { return isOutsourceMember(c.member) }
func (c taskCaller) isAdminCapable() bool { return principalAtLeast(c.principal, principalAdminAgent) }

func (s *apiServer) taskCallerOf(r *http.Request) (taskCaller, error) {
	actorID := currentActor(r)
	if currentScope(r) == "owner" {
		return taskCaller{principal: principalOwner, actorID: actorID}, nil
	}
	m, err := s.dal.GetMember(actorID)
	if err != nil {
		return taskCaller{principal: principalAgent, actorID: actorID}, err
	}
	return taskCaller{principal: classifyMember(m), actorID: actorID, member: m}, nil
}

func authorizeTaskCreate(c taskCaller, willOutsource bool, manualAssigneeMemberID, executorID string) (int, string) {
	if c.isOutsource() {
		return http.StatusForbidden, "outsource workers may not create tasks"
	}
	// Before the willOutsource exit on purpose: a typed task whose manual names
	// member X is X-only even when sent to outsource (no admin exemption).
	if manualAssigneeMemberID != "" {
		if c.actorID != manualAssigneeMemberID {
			return http.StatusForbidden,
				"a typed task assigned to member '" + manualAssigneeMemberID +
					"' may only be created by that member"
		}
		return 0, ""
	}
	if willOutsource {
		return 0, ""
	}
	if !c.isAdminCapable() && c.actorID != executorID {
		return http.StatusForbidden,
			"an ad-hoc task may only name yourself as executor (or be dispatched to an outsource worker)"
	}
	return 0, ""
}

func (s *apiServer) closeTask(t *Task, status string, now float64, trigger string) error {
	t.Status = status
	t.ClosedTS = now
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	// After the terminal PutTask on purpose: the card-hold release's orphan branch
	// then leaves the closed task untouched (no resume, no UpdatedTS bump). Nothing
	// else removes these cards — the answer route 409s orphans.
	if _, err := s.expireWaitingCardsForTask(t.ID, now, trigger); err != nil {
		taskLog("close %s: reply-card sweep failed (cards left waiting): %v", t.ID, err)
	}
	// Every close dismisses the bound workers — no door opts out, force_done and
	// terminate included (owner ruling rc-571b665bc047: 「外包改在按下結案那一刻遣散」).
	fired := s.dismissOutsourceWorkersForTask(t.ID, now, trigger)
	// Sweeps the dismissed workers' unbound cards (linked_task=null), which the
	// task sweep above cannot reach. Outside dismissOutsourceWorkersForTask on
	// purpose: card writes must not run under outsourceMu.
	for _, workerID := range fired {
		if _, err := s.expireWaitingCardsByAuthor(workerID, now, trigger); err != nil {
			taskLog("close %s: card sweep for dismissed worker %s failed: %v",
				t.ID, workerID, err)
		}
	}
	s.publishTask(*t, trigger)
	s.releaseDependentsOnClose(*t, now, trigger)
	// The text comes only from the 〈任務收尾〉 document; "" (unrenderable) sends
	// nothing — do not add a Go fallback (a second source of truth). It is a
	// durable chat row, not an SSE push or a ticket note: the executor is often
	// offline, and the wake snapshot lists only open tasks.
	if sig := decideTaskCloseNudge(*t); sig != nil {
		sig.ClosedBy = trigger
		sig.Reason = s.taskNoticeText(docKindTaskCloseout, map[string]string{
			"task_no":   sig.TaskNo,
			"closed_by": sig.ClosedBy,
		})
		if sig.Reason != "" {
			s.postTaskChat(*t, wireSystemSender, sig.To, sig.Reason, trigger,
				map[string]any{"closed_by": sig.ClosedBy})
		}
	}
	return nil
}

// labelWithID fills the single party slot of 〈給接手人〉 as 「銀月（mira）」:
// the document's body tells the reader to post_chat that party, so it needs the
// id as well as the name (owner decision 「名字跟 id 不能都給嗎」).
func labelWithID(label, id string) string {
	if label == "" {
		return id
	}
	if label == id {
		return label
	}
	return label + "（" + id + "）"
}

func (s *apiServer) deriveAndPersistTask(t *Task, now float64, trigger string) error {
	if TaskIsTerminal(t.Status) {
		return nil
	}
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		return err
	}
	was := t.Status
	RecomputeTaskStatus(t, steps)
	arrived := was != TaskStatusReadyForDone && t.Status == TaskStatusReadyForDone
	if arrived {
		t.ReadyForDoneVisits++
	}
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	s.publishTask(*t, trigger)
	if arrived {
		s.postReadyForDoneNotice(*t, trigger)
	}
	return nil
}

func (s *apiServer) postReadyForDoneNotice(t Task, trigger string) {
	if t.ExecutorID == "" {
		return
	}
	notice := s.taskNoticeText(docKindTaskReadyForDone, map[string]string{
		"task_no":  TaskNo(t.ID),
		"visit_no": strconv.Itoa(t.ReadyForDoneVisits),
	})
	if notice == "" {
		return
	}
	s.postTaskChat(t, wireSystemSender, t.ExecutorID, notice, trigger, nil)
}

// reconcileTaskStatusesOnBoot sends no 〈任務可結案〉 notice and no SSE on purpose:
// it repairs drifted rows, it does not report arrivals — a notice here would post
// one row per drifted task after any derivation change.
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

const resumeTasksN = 5

// resumeTasksFor serves light rows (owner ruling T-3f31: 任務不該包含細節 — no
// step text); detail_chars is the rune size of the omitted plan text, which
// agents read to decide whether to get_task.
func (s *apiServer) resumeTasksFor(actor string, cards map[string]ReplyCard) ([]resumeTaskDTO, int, error) {
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
		currentID, currentName := CurrentStep(steps)
		detailChars := 0
		answered := []resumeAnsweredCardStepDTO{}
		for _, st := range steps {
			// The card-hold release puts an answered step back to in_progress, so only the
			// card status tells "answer arrived, not yet acted on" from active work.
			if st.Status == StepStatusInProgress && st.ReplyCardID != "" {
				if c, ok := cards[st.ReplyCardID]; ok && c.Status == replyCardStatusAnswered {
					answered = append(answered, resumeAnsweredCardStepDTO{
						StepID:   st.ID,
						StepName: st.Name,
						CardID:   st.ReplyCardID,
					})
				}
			}
			detailChars += len([]rune(st.Name)) + len([]rune(st.DoD))
		}
		done, stepTotal := TaskProgress(steps)
		blockingRefs, err := s.tasksWaitingOn(t.ID)
		if err != nil {
			return nil, 0, err
		}
		blocking := []string{}
		for _, b := range blockingRefs {
			blocking = append(blocking, b.ID)
		}
		out = append(out, resumeTaskDTO{
			ID:                 t.ID,
			TaskNo:             TaskNo(t.ID),
			TypeKey:            t.TypeKey,
			Title:              t.Title,
			Status:             t.Status,
			Priority:           t.Priority,
			WaitingReason:      t.WaitingReason,
			CurrentStepID:      currentID,
			CurrentStepName:    currentName,
			ProgressDone:       done,
			ProgressTotal:      stepTotal,
			DetailChars:        detailChars,
			UpdatedTS:          t.UpdatedTS,
			Lock:               t.Lock,
			ReassignedFrom:     t.ReassignedFrom,
			ReassignedFromKind: t.ReassignedFromKind,
			Blocking:           blocking,

			AnsweredCardSteps: answered,
		})
	}
	return out, total, nil
}

func (s *apiServer) HandleListTasksApiTasksGet(w http.ResponseWriter, r *http.Request, params HandleListTasksApiTasksGetParams) {
	status := trimmedOrEmpty(params.Status)
	if status != "" && !ValidTaskStatus(status) {
		writeError(w, http.StatusBadRequest,
			"status must be one of not_started, in_progress, waiting_owner, waiting_external, reassigning, done, terminated, duplicated")
		return
	}
	statusSet, badStatus := parseTaskStatusSet(params.Statuses)
	if badStatus != "" {
		writeError(w, http.StatusBadRequest,
			"statuses must each be one of not_started, in_progress, waiting_owner, "+
				"waiting_external, reassigning, done, terminated, duplicated — got '"+
				badStatus+"'")
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
	byID := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	progressByTask, err := s.dal.AllTaskStepProgress()
	if err != nil {
		internalError(w, err)
		return
	}
	currentByTask, err := s.dal.AllTaskCurrentStep()
	if err != nil {
		internalError(w, err)
		return
	}
	depsByTask, err := s.dal.AllTaskDeps()
	if err != nil {
		internalError(w, err)
		return
	}
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
		if len(statusSet) > 0 && !taskStatusSetMatch(t, statusSet) {
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
			t, depsByTask[t.ID], p.Done, p.Total, artifactCountByTask[t.ID], byID,
			currentByTask[t.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func parseTaskStatusSet(raw *[]string) (set map[string]bool, badStatus string) {
	if raw == nil {
		return nil, ""
	}
	set = map[string]bool{}
	for _, v := range *raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !ValidTaskStatus(v) && v != TaskStatusFilterReassigning {
			return nil, v
		}
		set[v] = true
	}
	return set, ""
}

// taskStatusSetMatch accepts `reassigning` (a lock, not a status) because the
// cockpit's 狀態 dropdown lists 轉派中 and its default view ticks it. The single
// ?status= param is not widened: that is frozen wire a live client sends. The
// lock counts only while open: closeTask leaves Lock set (known residue,
// deliberately unfixed), so a terminated task can carry lock=reassigning.
func taskStatusSetMatch(t Task, set map[string]bool) bool {
	if set[t.Status] {
		return true
	}
	return set[TaskStatusFilterReassigning] && t.Lock == TaskLockReassigning &&
		!TaskIsTerminal(t.Status)
}

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
	writeJSON(w, http.StatusOK, taskCountDTO{Open: open, Total: len(tasks)})
}

func (s *apiServer) HandleGetTaskApiTasksTaskIdGet(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	s.writeTask(w, *t)
}

// callerMayTerminateTask excludes an outsource worker on purpose: the owner's
// 「執行者」 ruling (rc-b896e3f641e7) did not cover contractors; widening needs a
// new ruling.
func (s *apiServer) callerMayTerminateTask(r *http.Request, t Task) (bool, string) {
	c, err := s.taskCallerOf(r)
	if err != nil {
		return false, taskActorRefusal
	}
	if c.isAdminCapable() {
		return true, ""
	}
	// DAL.GetMember answers (nil, nil) for a missing row and isOutsource() is false
	// on nil, so without this a worker whose row was deleted could terminate its
	// own task (measured by review: 200).
	if c.member == nil {
		return false, taskActorRefusal
	}
	if c.actorID != s.actingExecutorOf(t) {
		return false, taskActorRefusal
	}
	if c.isOutsource() {
		return false, "an outsource worker may not terminate its own task; ask the owner or an admin agent"
	}
	return true, ""
}

func taskAlreadyClosedRefusal(t Task) string {
	return "task '" + t.ID + "' is already closed (" + t.Status + ")"
}

func (s *apiServer) HandleMarkTaskDoneApiTasksTaskIdMarkDonePost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayMarkTaskDone(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	if t.Status != TaskStatusReadyForDone {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is in '"+t.Status+"', not '"+TaskStatusReadyForDone+
				"' — every step has to be reported done before the task can be "+
				"closed as done (or ask the owner or an admin agent for "+
				"force_task_done)")
		return
	}
	if err := s.closeTask(t, TaskStatusDone, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTaskWriteReceipt(w, *t)
}

func (s *apiServer) callerMayMarkTaskDone(r *http.Request, t Task) bool {
	acting := s.actingExecutorOf(t)
	return acting != "" && currentActor(r) == acting
}

func (s *apiServer) HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if ok, reason := s.callerMayTerminateTask(r, *t); !ok {
		writeError(w, http.StatusForbidden, reason)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	if err := s.closeTask(t, TaskStatusTerminated, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTaskWriteReceipt(w, *t)
}

// Authz is the route floor only (routes.go: Gated(principalAdminAgent, …)) — no
// check in this body on purpose, so the task's own executor gets 403 there.
// reason is optional (owner ruling rc-a92a6252c3bd 「可以不給理由」); do not
// restore a blank-reason 422.
func (s *apiServer) HandleForceTaskDoneApiTasksTaskIdForceDonePost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskForceDoneDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	reason := trimString(strOrEmpty(body.Reason))
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	t.ForcedDoneBy = requestTrigger(r)
	t.ForcedDoneReason = reason
	if err := s.closeTask(t, TaskStatusDone, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTaskWriteReceipt(w, *t)
}

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
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	if priority == TaskPriorityFrozen {
		t.FrozenBy = requestTrigger(r)
	} else {
		t.FrozenBy = ""
	}
	t.Priority = priority
	t.UpdatedTS = nowSecs()
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	writeJSON(w, http.StatusOK, taskPriorityReceiptDTO{
		TaskID: t.ID, Priority: t.Priority, FrozenBy: t.FrozenBy,
	})
}

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
		inputs = *body.Attachments
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
	var fresh []ChatAttachment
	if len(resolved) > 0 {
		var refs []any
		refs, fresh = pendingAttachments(resolved)
		meta["attachments"] = refs
	} else if text == "" {
		writeError(w, http.StatusBadRequest,
			"message must carry text or an attachment")
		return
	}
	// Literal, not an i18n key (owner ruling rc-01a07b1b2a12): the label must read
	// the same in every locale so it can be matched; meta.task_id is the machine
	// linkage.
	msgBody := text
	if msgBody != "" {
		msgBody = "[TaskID=" + TaskNo(t.ID) + "] " + msgBody
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    currentActor(r),
		Recipient: t.ExecutorID,
		Body:      msgBody,
		TS:        nowSecs(),
		Meta:      meta,
	}
	if err := s.dal.PutChatWithAttachments(msg, fresh); err != nil {
		internalError(w, err)
		return
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), requestTrigger(r))
	writeJSON(w, http.StatusOK, chatPostReceiptOf(msg))
}

// Handover flow across files: waiting cards expire (expireWaitingCards,
// api_replycards.go), open steps reset to pending, and the task enters the
// reassigning lock with the predecessor stamped; the predecessor keeps write
// rights (actingExecutorOf) until the successor's claim_task. An outsource
// target lands unassigned and outsource_sched.go mints the successor, which
// finds the task via its boot sequence (reassigning lock + reassigned_from), not
// a chat notice. A bound outsource predecessor stays live to write the handover
// and is dismissed by claim_task or the handover-timeout reaper — by worker id,
// never task_id, since the successor may share the task_id.
func (s *apiServer) HandleReassignTaskApiTasksTaskIdReassignPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskReassignDTO
	if !decodeJSONBodyRequired(w, r, &body, "target") {
		return
	}
	note := trimmedOrEmpty(body.Note)
	if n := utf8.RuneCountInString(note); n > chatBodyMaxChars {
		writeError(w, http.StatusBadRequest, "handover note is "+strconv.Itoa(n)+
			" chars, over the "+strconv.Itoa(chatBodyMaxChars)+"-char limit")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	caller, err := s.taskCallerOf(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if caller.isOutsource() {
		writeError(w, http.StatusForbidden, "outsource workers may not reassign tasks")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	// No frozen check on purpose (owner ruling 2026-08-11: 「我覺得應該移除凍結不能轉派的
	// 限制」). Outsource: outsource_sched.go refuses to mint for a frozen task. Staff:
	// nothing gates it; 全域脈絡 §3.5 tells agents what frozen means — the takeover
	// notice does not (owner removed that caveat 2026-08-22; do not re-add it).
	// Count gates with `grep -rn TaskPriorityFrozen --include=*.go server/ocserverd |
	// grep -v _test`: a prose count was once measured wrong.
	kind := trimString(body.Target.Kind)
	var newMember *Member
	var dispatch dispatchSpec
	switch kind {
	case TaskExecutorStaff:
		if !caller.isAdminCapable() {
			writeError(w, http.StatusForbidden,
				"only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead")
			return
		}
		memberID := trimmedOrEmpty(body.Target.MemberId)
		if memberID == "" {
			writeError(w, http.StatusBadRequest,
				"target.member_id is required for kind '"+TaskExecutorStaff+"'")
			return
		}
		m, err := s.dal.GetMember(memberID)
		if err != nil {
			internalError(w, err)
			return
		}
		if m == nil || m.RosterStatus != RosterStatusActive || m.Kind == KindOutsource {
			writeError(w, http.StatusBadRequest,
				"target member '"+memberID+"' is not an active roster member")
			return
		}
		if m.Kind == KindWarden {
			writeError(w, http.StatusBadRequest,
				"target member '"+memberID+"' is a machine (warden) — machines never execute tasks")
			return
		}
		if t.ExecutorKind == TaskExecutorStaff && t.ExecutorID == memberID {
			writeError(w, http.StatusConflict,
				"member '"+memberID+"' is already the task's executor")
			return
		}
		newMember = m
	case TaskExecutorOutsource:
		if body.Target.Runtime != nil {
			dispatch.Runtime = string(*body.Target.Runtime)
			if !ValidRuntime(dispatch.Runtime) {
				writeError(w, http.StatusBadRequest,
					"target.runtime must be 'claude' or 'codex'")
				return
			}
		}
		dispatch.Model = trimmedOrEmpty(body.Target.Model)
		dispatch.Effort = trimmedOrEmpty(body.Target.Effort)
		if dispatch.Effort != "" && !validEffort(dispatch.Effort) {
			writeError(w, http.StatusBadRequest,
				"target.effort must be one of low, medium, high, xhigh, max")
			return
		}
		dispatch.Machine = trimmedOrEmpty(body.Target.Machine)
		if dispatch.Machine != "" {
			if _, err := s.resolveMachine(dispatch.Machine); err != nil {
				writeResolveError(w, err, "machine", dispatch.Machine)
				return
			}
		}
		var manualSpec *outsourceTypeSpec
		if t.TypeKey != "" {
			if manual, err := s.dal.GetTaskManual(t.TypeKey); err == nil && manual != nil {
				manualSpec = outsourceSpecOf(*manual)
			}
		}
		dispatch = inheritDispatchSpec(dispatch, manualSpec, caller.member)
	default:
		// From CanonicalTaskExecutorKind so a pre-rename 'member' is told it was kind-vocab-guard:legacy
		// renamed (owner ruling rc-7574cc804dd6); do not hand-write the set here.
		_, kindErr := CanonicalTaskExecutorKind(kind)
		writeError(w, http.StatusBadRequest, "target.kind: "+kindErr.Error())
		return
	}

	now := nowSecs()
	trigger := requestTrigger(r)

	if kind == TaskExecutorOutsource {
		principal := s.principalOfRequest(r)
		var initiator *Member
		if principal != principalOwner {
			initiator, _ = s.dal.GetMember(currentActor(r))
		}
		gate, err := s.outsourceSpawnGate(outsourceGateRequest{
			PrincipalClass: principal, Initiator: initiator, TaskID: t.ID,
			Runtime: dispatch.Runtime, Model: dispatch.Model,
			Effort: dispatch.Effort, Machine: dispatch.Machine,
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

	if newMember != nil && newMember.ID == t.ReassignedFrom && s.predecessorHoldsTask(*t) {
		displaced, displacedKind := t.ExecutorID, t.ExecutorKind
		t.ExecutorKind = TaskExecutorStaff
		t.ExecutorID = newMember.ID
		t.OutsourceRuntime = RuntimeClaude
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine = "", "", ""
		t.OutsourceDispatched = false
		t.Lock = TaskLockNone
		steps, err := s.dal.ListTaskSteps(t.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		t.Status = DeriveTaskStatus(steps)
		if note != "" {
			t.HandoverNote = note
			t.HandoverNoteTS = now
			t.HandoverNoteBy = currentActor(r)
		}
		t.UpdatedTS = now
		if err := s.dal.PutTask(*t); err != nil {
			internalError(w, err)
			return
		}
		if displacedKind == TaskExecutorOutsource && displaced != "" {
			s.dismissOutsourceWorkerByID(displaced, now, trigger)
		}
		s.publishTask(*t, trigger)
		if displaced != "" {
			s.hub.Publish("task", "patch", "task", wireOwnerID+"::"+t.ID,
				map[string]any{"id": t.ID, "status": t.Status, "priority": t.Priority},
				audienceMembers(displaced), trigger)
		}
		s.writeTaskWriteReceipt(w, *t)
		return
	}

	oldKind, oldExecutor := t.ExecutorKind, t.ExecutorID
	leaving, leavingKind := oldExecutor, oldKind
	handingOver := oldExecutor
	if underHandover(*t) {
		oldKind, oldExecutor = t.ReassignedFromKind, t.ReassignedFrom
		handingOver = ""
		if s.predecessorHoldsTask(*t) {
			handingOver = oldExecutor
		}
	}

	if _, err := s.expireWaitingCardsForTask(t.ID, now, trigger); err != nil {
		internalError(w, err)
		return
	}

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
		// started_ts>0 is read system-wide as "ever entered in_progress" (migration
		// 00028's Down relies on it), so a reset step must zero it.
		st.StartedTS = 0
		st.FinishedTS = 0
		st.WaitingReason = ""
		if err := s.dal.PutTaskStep(st); err != nil {
			internalError(w, err)
			return
		}
	}

	// Re-read the row: the card pass (the card-hold release) may have rewritten it.
	t, err = s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}

	if kind == TaskExecutorStaff {
		t.ExecutorKind = TaskExecutorStaff
		t.ExecutorID = newMember.ID
		t.OutsourceRuntime = RuntimeClaude
		t.OutsourceModel, t.OutsourceEffort, t.OutsourceMachine = "", "", ""
		t.OutsourceDispatched = false
	} else {
		t.ExecutorKind = TaskExecutorOutsource
		t.ExecutorID = ""
		t.OutsourceRuntime = dispatch.Runtime
		t.OutsourceModel = dispatch.Model
		t.OutsourceEffort = dispatch.Effort
		t.OutsourceMachine = dispatch.Machine
		t.OutsourceDispatched = true
	}
	t.Lock = TaskLockReassigning
	stepsAfterReset, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	t.Status = DeriveTaskStatus(stepsAfterReset)
	t.WaitingReason = ""
	if note != "" {
		t.HandoverNote = note
		t.HandoverNoteTS = now
		t.HandoverNoteBy = currentActor(r)
	}
	if oldExecutor != "" {
		t.ReassignedFrom = oldExecutor
		t.ReassignedFromKind = oldKind
	}
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		internalError(w, err)
		return
	}
	// Nothing else reaps a displaced unclaimed successor: the handover reaper only
	// dismisses the stamped predecessor.
	if leaving != oldExecutor && leavingKind == TaskExecutorOutsource && leaving != "" {
		s.dismissOutsourceWorkerByID(leaving, now, trigger)
	}

	newExecutorID := ""
	if newMember != nil {
		newExecutorID = newMember.ID
	}
	no := TaskNo(t.ID)
	if handingOver != "" {
		if notice := s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
			"task_no": no,
		}); notice != "" {
			s.postTaskChat(*t, wireSystemSender, handingOver, notice, trigger, nil)
		}
	}
	// The handover note is not pasted into either notice (owner ruling
	// rc-0c36d8739b8f 「拿掉 —— 交接備註只留在任務上」); the successor reads it via get_task.
	if newExecutorID != "" {
		predecessor := ""
		if handingOver != "" {
			predecessor = labelWithID(s.executorLabel(oldKind, handingOver), handingOver)
		}
		if notice := s.takeoverNoticeText(no, predecessor); notice != "" {
			s.postTaskChat(*t, wireSystemSender, newExecutorID, notice, trigger, nil)
		}
	}

	s.publishTask(*t, trigger)
	if leaving != "" && leaving != t.ExecutorID {
		s.hub.Publish("task", "patch", "task", wireOwnerID+"::"+t.ID,
			map[string]any{"id": t.ID, "status": t.Status, "priority": t.Priority},
			audienceMembers(leaving), trigger)
	}

	if kind == TaskExecutorOutsource {
		s.outsourceTickNow()
	}
	s.writeTaskWriteReceipt(w, *t)
}

func (s *apiServer) HandleClaimTaskApiTasksTaskIdClaimPost(w http.ResponseWriter, r *http.Request, taskId string) {
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayClaimTask(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
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
	s.writeTaskWriteReceipt(w, *t)
}

func (s *apiServer) executorLabel(kind, id string) string {
	if id == "" {
		return ""
	}
	switch kind {
	case TaskExecutorStaff:
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

func (s *apiServer) postTaskChat(t Task, sender, recipient, body, trigger string, extra map[string]any) {
	meta := map[string]any{
		"task_id":    t.ID,
		"task_title": t.Title,
		"task_type":  t.TypeKey,
	}
	for k, v := range extra {
		meta[k] = v
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    sender,
		Recipient: recipient,
		Body:      body,
		TS:        nowSecs(),
		Meta:      meta,
	}
	if err := s.dal.PutChat(msg); err != nil {
		outsourceLog("task-chat %s: durable message to %s failed (the recipient "+
			"will NOT be told): %v", t.ID, recipient, err)
		return
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), trigger)
}

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

	// Validation stays a separate statement from the dispatch decision below, which
	// must keep reading body.Target.Kind: the authz-surface scan inventories
	// predicates on a `Kind` selector,
	// and branching on a canonical local measurably dropped this gate from it.
	if body.Target != nil {
		if k := trimString(body.Target.Kind); k != "" {
			if _, err := CanonicalTaskExecutorKind(k); err != nil {
				writeError(w, http.StatusBadRequest, "target.kind: "+err.Error())
				return
			}
		}
	}
	var outsourceTarget *TaskCreateTargetDTO
	if body.Target != nil && trimString(body.Target.Kind) == TaskExecutorOutsource {
		outsourceTarget = body.Target
	}

	typeKey := trimmedOrEmpty(body.TypeKey)
	executorKind := TaskExecutorStaff
	executorID := ""
	dedupeKey := ""
	manualAssigneeMemberID := ""
	var manualSpec *outsourceTypeSpec
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
			if f.IsKey && missing {
				writeError(w, http.StatusBadRequest,
					"identity key '"+f.Name+"' must not be empty")
				return
			}
		}
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
			executorKind = TaskExecutorOutsource
			manualSpec = outsourceSpecOf(*manual)
		case kind == TaskExecutorStaff && memberID != "":
			executorID = memberID
			manualAssigneeMemberID = memberID
		}
	}
	var dispatch dispatchSpec
	if outsourceTarget != nil {
		executorKind = TaskExecutorOutsource
		executorID = ""
		if outsourceTarget.Runtime != nil {
			dispatch.Runtime = string(*outsourceTarget.Runtime)
			if !ValidRuntime(dispatch.Runtime) {
				writeError(w, http.StatusBadRequest,
					"target.runtime must be 'claude' or 'codex'")
				return
			}
		}
		dispatch.Model = trimmedOrEmpty(outsourceTarget.Model)
		dispatch.Effort = trimmedOrEmpty(outsourceTarget.Effort)
		if dispatch.Effort != "" && !validEffort(dispatch.Effort) {
			writeError(w, http.StatusBadRequest,
				"target.effort must be one of low, medium, high, xhigh, max")
			return
		}
		dispatch.Machine = trimmedOrEmpty(outsourceTarget.Machine)
		if dispatch.Machine != "" {
			if _, err := s.resolveMachine(dispatch.Machine); err != nil {
				writeResolveError(w, err, "machine", dispatch.Machine)
				return
			}
		}
	}
	if executorKind == TaskExecutorStaff && executorID == "" {
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

	// Before dedupe on purpose: a caller who may not create must never receive the
	// existing task.
	caller, err := s.taskCallerOf(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if code, reason := authorizeTaskCreate(caller,
		executorKind == TaskExecutorOutsource, manualAssigneeMemberID, executorID); reason != "" {
		writeError(w, code, reason)
		return
	}
	if executorKind == TaskExecutorOutsource {
		dispatch = inheritDispatchSpec(dispatch, manualSpec, caller.member)
	}

	if dedupeKey != "" {
		existing, err := s.dal.FindOpenTaskByDedupe(typeKey, dedupeKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if existing != nil {
			title, status := existing.Title, existing.Status
			writeJSON(w, http.StatusOK, taskCreateResultDTO{
				TaskID:       existing.ID,
				ExecutorKind: existing.ExecutorKind,
				ExecutorID:   existing.ExecutorID,
				Deduped:      true,
				Title:        &title,
				Status:       &status,
				Warnings:     warnings,
			})
			return
		}
	}

	now := nowSecs()
	t := Task{
		TypeKey:             typeKey,
		Title:               title,
		DedupeKey:           dedupeKey,
		Inputs:              inputs,
		Description:         strOrEmpty(body.Description),
		Status:              TaskStatusNotStarted,
		Priority:            priority,
		ExecutorKind:        executorKind,
		ExecutorID:          executorID,
		OutsourceRuntime:    dispatch.Runtime,
		OutsourceModel:      dispatch.Model,
		OutsourceEffort:     dispatch.Effort,
		OutsourceMachine:    dispatch.Machine,
		OutsourceDispatched: outsourceTarget != nil,
		CreatorID:           currentActor(r),
		CreatedTS:           now,
		UpdatedTS:           now,
	}
	trigger := requestTrigger(r)

	// The gate runs inside CreateTaskMintingID's transaction (it needs the minted
	// id). Safe only because outsourceSpawnGate touches no database; anything it
	// needs from the DB must be resolved out here, not on the transaction's
	// connection.
	var gateDenied string
	var precheck func(id string) error
	if outsourceTarget != nil {
		principal := s.principalOfRequest(r)
		var initiator *Member
		if principal != principalOwner {
			initiator, _ = s.dal.GetMember(currentActor(r))
		}
		issuedBy := currentActor(r)
		precheck = func(id string) error {
			gate, err := s.outsourceSpawnGate(outsourceGateRequest{
				PrincipalClass: principal, Initiator: initiator, TaskID: id,
				Runtime: dispatch.Runtime, Model: dispatch.Model,
				Effort: dispatch.Effort, Machine: dispatch.Machine,
				IssuedBy: issuedBy,
			})
			if err != nil {
				return err
			}
			if gate.Decision == gateDeny {
				gateDenied = gate.Reason
				return errOutsourceGateDenied
			}
			return nil
		}
	}

	t, err = s.dal.CreateTaskMintingID(t, precheck)
	if err != nil {
		if errors.Is(err, errOutsourceGateDenied) {
			writeError(w, http.StatusForbidden,
				"not permitted to 發包 to an outsource worker: "+gateDenied)
			return
		}
		internalError(w, err)
		return
	}
	s.publishTask(t, trigger)
	if t.ExecutorKind == TaskExecutorOutsource {
		s.outsourceTickNow()
	}
	// No title/status on a fresh create (owner ruling 2026-09-05: 「自己發送出去的內容
	// … 不應該再回傳回來」); only the dedupe branch returns them.
	writeJSON(w, http.StatusOK, taskCreateResultDTO{
		TaskID:       t.ID,
		ExecutorKind: t.ExecutorKind,
		ExecutorID:   t.ExecutorID,
		Deduped:      false,
		Warnings:     warnings,
	})
}

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
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
		return
	}
	var fresh []TaskStep
	for _, ps := range body.Steps {
		name := trimString(ps.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "step name must not be blank")
			return
		}
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
	existing, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	// Dropped rows may still hold a waiting card; the card-hold release's step
	// guards make its later answer a no-op on the removed step.
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
	if len(kept)+len(fresh) == 0 {
		writeError(w, http.StatusBadRequest,
			"a plan must have at least one step")
		return
	}
	timeline := make([]TaskStep, 0, len(kept)+len(fresh))
	timeline = append(timeline, kept...)
	timeline = append(timeline, fresh...)
	if msg := ValidatePlanParallelShape(timeline, fresh); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	steps, err := s.dal.ReplaceTaskSteps(t.ID, retainIDs, freezeIDs, nowSecs(), fresh)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.deriveAndPersistTask(t, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	done, total := TaskProgress(steps)
	writeJSON(w, http.StatusOK, taskPlanReceiptDTO{
		TaskID: t.ID, StepsTotal: len(steps),
		ProgressDone: done, ProgressTotal: total,
	})
}

func (s *apiServer) HandleMarkTaskDuplicatedApiTasksTaskIdMarkDuplicatedPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskMarkDuplicatedDTO
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
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
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
	t.WaitingReason = ""
	if err := s.closeTask(t, TaskStatusDuplicated, nowSecs(), requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTaskWriteReceipt(w, *t)
}

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
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
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
		writeError(w, http.StatusBadRequest,
			"waiting_owner is not an agent-reportable status; a step enters it only "+
				"by opening a reply card (create_reply_card with linked_task)")
		return
	}
	if status == StepStatusSuperseded {
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
	if err := s.deriveAndPersistTask(t, now, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.writeTaskStepStatusReceipt(w, *t, *step)
}

// prepareStepHeldByCard writes nothing: the step and task must commit in the
// same transaction as the card and its companion message
// (PutReplyCardWithChatStepAndTask) — a card without its hold once answered 500 and
// made the asker open a second card.
func (s *apiServer) prepareStepHeldByCard(
	t *Task, step *TaskStep, cardID string, now float64,
) (*Task, error) {
	step.Status = StepStatusWaitingOwner
	step.ReplyCardID = cardID
	if step.StartedTS == 0 {
		step.StartedTS = now
	}
	if TaskIsTerminal(t.Status) {
		return nil, nil
	}
	steps, err := s.dal.ListTaskSteps(t.ID)
	if err != nil {
		return nil, err
	}
	for i := range steps {
		if steps[i].ID == step.ID {
			steps[i] = *step
		}
	}
	// Not deriveAndPersistTask: a waiting_owner step always derives the task to
	// waiting_owner, so no ready_for_done arrival can happen here.
	RecomputeTaskStatus(t, steps)
	t.UpdatedTS = now
	return t, nil
}

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
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict, taskAlreadyClosedRefusal(*t))
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
	s.writeTaskWriteReceipt(w, *t)
}

// The caps bind NEW writes only (owner ruling c-0d0a576f68af: 「舊資料不截斷」):
// migrated rows can hold descriptions over 256 and empty names (the wire name is
// derived at read time).
const (
	artifactNameMaxChars        = 48
	artifactDescriptionMaxChars = 256
)

func artifactTextOrError(w http.ResponseWriter, namePtr, descPtr *string) (string, string, bool) {
	name := trimmedOrEmpty(namePtr)
	if n := utf8.RuneCountInString(name); n > artifactNameMaxChars {
		writeError(w, http.StatusBadRequest, "artifact name is "+
			strconv.Itoa(n)+" chars, over the "+
			strconv.Itoa(artifactNameMaxChars)+"-char limit")
		return "", "", false
	}
	description := trimmedOrEmpty(descPtr)
	if n := utf8.RuneCountInString(description); n > artifactDescriptionMaxChars {
		writeError(w, http.StatusBadRequest, "artifact description is "+
			strconv.Itoa(n)+" chars, over the "+
			strconv.Itoa(artifactDescriptionMaxChars)+"-char limit")
		return "", "", false
	}
	return name, description, true
}

func mintLinkTargetBlob(url string) (string, *ChatAttachment) {
	att := &ChatAttachment{
		ID:   "att-" + newHexID(12),
		Mime: linkTargetMime,
		Data: []byte(url),
	}
	return att.ID, att
}

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
	name, description, ok := artifactTextOrError(w, &body.Name, body.Description)
	if !ok {
		return
	}
	if name == "" {
		writeError(w, http.StatusBadRequest,
			"name is required: give this deliverable a short display name")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayEditTaskText(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskRecordReadOnly(t.Status) {
		writeError(w, http.StatusConflict, taskFrozenDeliverablesRefusal(*t))
		return
	}
	art := TaskArtifact{
		ID:          "ta-" + newHexID(12),
		TaskID:      t.ID,
		Kind:        kind,
		Name:        name,
		Description: description,
		CreatedTS:   nowSecs(),
		CreatedBy:   currentActor(r),
	}
	var minted *ChatAttachment
	if kind == ArtifactKindLink {
		url := trimmedOrEmpty(body.Url)
		if url == "" {
			writeError(w, http.StatusBadRequest,
				"url is required for a link artifact")
			return
		}
		if refusal := artifactLinkURLRefusal(url); refusal != "" {
			writeError(w, http.StatusBadRequest, refusal)
			return
		}
		art.AttachmentID, minted = mintLinkTargetBlob(url)
	} else {
		attID := trimmedOrEmpty(body.AttachmentId)
		if attID == "" {
			writeError(w, http.StatusBadRequest,
				"attachment_id is required for a "+kind+" artifact")
			return
		}
		if isMemberAvatarAttachmentID(attID) {
			writeError(w, http.StatusBadRequest,
				"attachment '"+attID+"' is reserved for a member avatar")
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
	if err := s.dal.PutTaskArtifactMintingBlob(art, minted); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskArtifactReceipt(w, *t, art.ID)
}

// DeleteTaskArtifact also drops every retained version and the blobs only they
// referenced; the live blob is kept (it may be shared with a chat message).
func (s *apiServer) HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(w http.ResponseWriter, r *http.Request, taskId, artifactId string) {
	t, _, ok := s.artifactOnTask(w, r, taskId, artifactId, artifactWrite)
	if !ok {
		return
	}
	if _, err := s.dal.DeleteTaskArtifact(artifactId); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskArtifactReceipt(w, *t, artifactId)
}

func taskFrozenDeliverablesRefusal(t Task) string {
	return "task '" + t.ID + "' is closed (" + t.Status +
		") — its deliverables are frozen"
}

// artifactAccess: the zero value is artifactWrite on purpose, so a call site that
// declares nothing gets the strict rules.
type artifactAccess int

const (
	artifactWrite artifactAccess = iota
	artifactRead
)

// artifactOnTask: artifactRead skips both the executor guard and the freeze on
// purpose (owner ruling, T-60) — list_task_artifacts already serves every row to
// any caller, so gating history would make two doors disagree. The write freeze
// binds admin/owner too (owner ruling 2026-07-25).
func (s *apiServer) artifactOnTask(
	w http.ResponseWriter, r *http.Request, taskID, artifactID string, access artifactAccess,
) (*Task, *TaskArtifact, bool) {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return nil, nil, false
	}
	if access == artifactWrite && !s.callerMayEditTaskText(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return nil, nil, false
	}
	if access == artifactWrite && TaskRecordReadOnly(t.Status) {
		writeError(w, http.StatusConflict, taskFrozenDeliverablesRefusal(*t))
		return nil, nil, false
	}
	art, err := s.dal.GetTaskArtifact(artifactID)
	if err != nil {
		internalError(w, err)
		return nil, nil, false
	}
	if art == nil {
		writeError(w, http.StatusNotFound, "artifact '"+artifactID+"' not found")
		return nil, nil, false
	}
	if art.TaskID != t.ID {
		writeError(w, http.StatusBadRequest,
			"artifact '"+artifactID+"' does not belong to task '"+taskID+"'")
		return nil, nil, false
	}
	return t, art, true
}

func (s *apiServer) HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost(
	w http.ResponseWriter, r *http.Request, taskId, artifactId string,
) {
	var body TaskArtifactReplaceInputDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	t, art, ok := s.artifactOnTask(w, r, taskId, artifactId, artifactWrite)
	if !ok {
		return
	}
	if kind := trimmedOrEmpty(body.Kind); kind != "" && kind != art.Kind {
		writeError(w, http.StatusBadRequest, artifactKindRefusal(art.Kind, kind))
		return
	}
	// Caps are checked only on a value actually sent, so a content swap never
	// refuses a migrated row whose stored text exceeds a cap. Some clients serialise
	// "" as omitted, so nothing here relies on "send blank to clear".
	name, description := art.Name, art.Description
	if body.Name != nil {
		v, _, ok := artifactTextOrError(w, body.Name, nil)
		if !ok {
			return
		}
		if v == "" {
			writeError(w, http.StatusBadRequest,
				"name cannot be blank: omit it to keep the name this deliverable already has")
			return
		}
		name = v
	}
	if body.Description != nil {
		_, v, ok := artifactTextOrError(w, nil, body.Description)
		if !ok {
			return
		}
		description = v
	}
	next := TaskArtifact{
		ID:          art.ID,
		TaskID:      art.TaskID,
		Kind:        art.Kind,
		Name:        name,
		Description: description,
		CreatedTS:   nowSecs(),
		CreatedBy:   currentActor(r),
	}
	var minted *ChatAttachment
	url, attID := trimmedOrEmpty(body.Url), trimmedOrEmpty(body.AttachmentId)
	if art.Kind == ArtifactKindLink {
		if attID != "" {
			writeError(w, http.StatusBadRequest,
				artifactKindRefusal(art.Kind, ArtifactKindFile))
			return
		}
		if url == "" {
			writeError(w, http.StatusBadRequest,
				"url is required for a link artifact")
			return
		}
		if refusal := artifactLinkURLRefusal(url); refusal != "" {
			writeError(w, http.StatusBadRequest, refusal)
			return
		}
		next.AttachmentID, minted = mintLinkTargetBlob(url)
	} else {
		if url != "" {
			writeError(w, http.StatusBadRequest,
				artifactKindRefusal(art.Kind, ArtifactKindLink))
			return
		}
		if attID == "" {
			writeError(w, http.StatusBadRequest,
				"attachment_id is required for a "+art.Kind+" artifact")
			return
		}
		if isMemberAvatarAttachmentID(attID) {
			writeError(w, http.StatusBadRequest,
				"attachment '"+attID+"' is reserved for a member avatar")
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
		next.AttachmentID = attID
	}
	replaced, err := s.dal.ReplaceTaskArtifactMintingBlob(next, minted)
	if err != nil {
		internalError(w, err)
		return
	}
	if !replaced {
		writeError(w, http.StatusNotFound, "artifact '"+artifactId+"' not found")
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskArtifactReplaceReceipt(w, *t, art.ID)
}

func (s *apiServer) writeTaskArtifactReplaceReceipt(w http.ResponseWriter, t Task, artifactID string) {
	versions, err := s.dal.ListTaskArtifactHistory(artifactID)
	if err != nil {
		internalError(w, err)
		return
	}
	count, err := s.dal.CountTaskArtifacts(t.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskArtifactReplaceReceiptDTO{
		TaskID: t.ID, ArtifactID: artifactID, ArtifactCount: count,
		VersionCount: len(versions) + 1,
	})
}

const artifactURLMaxChars = 2048

// artifactLinkURLSchemes is a whitelist because the cockpit renders the url as an
// href the owner clicks. `http` is load-bearing: production held 6 live http link
// artifacts (measured 2026-09-06), and replace re-validates the url on every call,
// so dropping it would make those rows uneditable. Re-measure before tightening.
var artifactLinkURLSchemes = []string{"https://", "http://"}

// artifactLinkURLRefusal is deliberately not in the DAL: task_artifact_history's
// INSERT carries the stored AttachmentID DB→DB, so a DAL guard would block
// retaining a legacy row's version. Existing rows are not backfilled, so a read
// can return a url a write now refuses.
func artifactLinkURLRefusal(url string) string {
	lower := strings.ToLower(url)
	ok := false
	for _, scheme := range artifactLinkURLSchemes {
		if strings.HasPrefix(lower, scheme) {
			ok = true
			break
		}
	}
	if !ok {
		return "url must start with https:// or http://"
	}
	if n := utf8.RuneCountInString(url); n > artifactURLMaxChars {
		return "url is " + strconv.Itoa(n) + " chars, over the " +
			strconv.Itoa(artifactURLMaxChars) + "-char limit"
	}
	return ""
}

func artifactKindRefusal(currentKind, askedKind string) string {
	return "artifact kind cannot change across versions: this artifact is a " +
		currentKind + " and the replacement asks for a " + askedKind +
		" — un-pin it and register a new artifact instead"
}

func (s *apiServer) HandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet(
	w http.ResponseWriter, r *http.Request, taskId, artifactId string,
) {
	_, art, ok := s.artifactOnTask(w, r, taskId, artifactId, artifactRead)
	if !ok {
		return
	}
	versions, err := s.dal.ListTaskArtifactHistory(art.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	out := make([]taskArtifactVersionDTO, 0, len(versions))
	for _, v := range versions {
		var att *ChatAttachment
		if v.AttachmentID != "" {
			att, err = s.dal.GetTaskArtifactBlob(v.AttachmentID)
			if err != nil {
				internalError(w, err)
				return
			}
		}
		out = append(out, newTaskArtifactVersionDTO(v, att))
	}
	writeJSON(w, http.StatusOK, out)
}
