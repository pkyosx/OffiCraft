package main

// api_tasks_handoff.go — T-74f8「漏球」的兩半:
//
//   A. 交棒閘 (handoffGateVerdict / applyHandoffPlan) — a task whose CREATOR is
//      not its EXECUTOR may not reach a terminal status until the executor has
//      said, explicitly, where the ball goes. fail-closed.
//
//   B. dependency 真的接棒 (releaseDependentsOnClose) — when a blocker reaches a
//      terminal status, every task blocked BY it is released: it becomes
//      schedulable (outsource_sched.go skips a task with a live blocker) and
//      its executor is told, through a DURABLE chat row, not only an SSE line.
//
// WHY THE GATE SITS WHERE IT SITS (the whole point of the ticket).
// A task's status is DERIVED from its steps (T-9ca5): the instant the LAST
// non-terminal step is reported done, DeriveTaskStatus returns done,
// deriveAndPersistTask calls closeTask, closed_ts stamps — and from that
// microsecond on submit_plan answers 409 already closed and every step report
// is a 409 too. The executor's last chance to arrange a handover is therefore
// BEFORE the step write of the report that closes the task, not after the
// close. A gate inside closeTask (or anywhere downstream of it) would be asking
// a question whose only answers have already been taken away — and a gate that
// let the step write land first would leave the task with all steps done, no
// legal transition out, and no way to replan: a hard deadlock.
//
// So the gate runs in HandleUpdateTaskStepStatus…, over a PROJECTION of the
// step set ("if I applied this report, would the task derive to done?"), before
// any row is written. A refused close changes nothing: the plan is still
// editable, submit_plan still works, the executor can create the successor task
// and re-report.

import (
	"net/http"
	"strings"
)

// handoffPlan is the gate's decision, resolved BEFORE any write and applied
// after the step write lands (so a refused close touches no row at all).
type handoffPlan struct {
	Kind   string // HandoffReturnToCreator | HandoffFollowUp | HandoffNone
	Note   string
	TaskID string // the successor task ('' for HandoffNone)
	// Auto is set when the gate satisfied itself from the world rather than
	// from a declaration: a successor task already carried a dep on this task,
	// which is exactly the behaviour the gate is trying to produce. An agent
	// that did the right thing NEVER sees the gate.
	Auto bool
}

// wouldCloseTask projects the step set as it would stand AFTER stepID is
// reported done and asks the ordinary derivation whether that closes the task.
// Using DeriveTaskStatus (not a hand-rolled "is this the last step") is what
// keeps parallel groups safe: a lane finishing while its siblings still run
// derives to in_progress, so the gate never fires there.
func wouldCloseTask(steps []TaskStep, stepID string) bool {
	projected := make([]TaskStep, len(steps))
	copy(projected, steps)
	for i := range projected {
		if projected[i].ID == stepID {
			projected[i].Status = StepStatusDone
		}
	}
	return DeriveTaskStatus(projected) == TaskStatusDone
}

// handoffGateReason is the 422 body the gate answers with. It is the whole
// user-facing contract of the gate, so it names the three ways out verbatim —
// a fail-closed guard that does not tell you how to pass is just an outage.
func handoffGateReason(t Task) string {
	return "task '" + t.ID + "' (" + TaskNo(t.ID) + ") was created by '" +
		t.CreatorID + "' but executed by '" + t.ExecutorID +
		"': this report would CLOSE it, and a closed task can never be replanned " +
		"(submit_plan turns into a permanent 409). Say where the ball goes, in " +
		"THIS same update_step_status call, with one of: " +
		"handoff='" + HandoffReturnToCreator + "' (the server opens a durable " +
		"follow-up task on the creator so the work is on somebody's list, not in " +
		"a notification); handoff='" + HandoffFollowUp + "' + " +
		"handoff_task_id='<the successor task you already created>' (the server " +
		"attaches this task to it as a dependency, and closing this one releases " +
		"it); handoff='" + HandoffNone + "' + handoff_note='<why nothing " +
		"follows>' (an explicit end of the line, recorded)."
}

// handoffGateVerdict is the GATE. It returns (plan, 0, "") when the close may
// proceed — plan nil meaning "nothing to record" — or (nil, code, message) to
// refuse. It reads state and validates; it writes nothing.
func (s *apiServer) handoffGateVerdict(
	t Task, declared, note, followUpID string,
) (*handoffPlan, int, string) {
	if declared == "" {
		// The narrow population the gate ASKS: creator ≠ executor, both named,
		// nothing declared yet. Everyone else closes exactly as before.
		if !TaskNeedsHandoffDeclaration(t.CreatorID, t.ExecutorID, t.Handoff) {
			return nil, 0, ""
		}
		// Auto-satisfaction: the ball is already somewhere. A NON-terminal task
		// that names this one as a blocker IS the handover the owner asked for
		// ("開個 task 掛上這個 design task 作為 dependency"), so the gate records
		// it and stands aside instead of demanding a second declaration of a
		// fact already true. An agent that did the right thing never sees a 422.
		dependents, err := s.dal.ListTasksBlockedBy(t.ID)
		if err != nil {
			return nil, http.StatusInternalServerError, err.Error()
		}
		for _, d := range dependents {
			if !TaskIsTerminal(d.Status) {
				return &handoffPlan{
					Kind: HandoffFollowUp, TaskID: d.ID, Auto: true,
					Note: "successor task " + TaskNo(d.ID) + " already depends on this task",
				}, 0, ""
			}
		}
		return nil, http.StatusUnprocessableEntity, handoffGateReason(t)
	}
	// A declaration is always honoured, even on a task the gate would not have
	// asked (a self-created one): dropping it silently would be a lie in the
	// audit trail. It is just never REQUIRED there.
	if !ValidHandoff(declared) {
		return nil, http.StatusUnprocessableEntity,
			"handoff must be one of " + HandoffReturnToCreator + ", " +
				HandoffFollowUp + ", " + HandoffNone
	}
	switch declared {
	case HandoffNone:
		if strings.TrimSpace(note) == "" {
			return nil, http.StatusUnprocessableEntity,
				"handoff='" + HandoffNone + "' requires handoff_note: an " +
					"un-reasoned 'nothing follows' is indistinguishable from " +
					"forgetting, and the note is the only record that a decision " +
					"was made at all"
		}
		return &handoffPlan{Kind: HandoffNone, Note: strings.TrimSpace(note)}, 0, ""
	case HandoffFollowUp:
		id := strings.TrimSpace(followUpID)
		if id == "" {
			return nil, http.StatusUnprocessableEntity,
				"handoff='" + HandoffFollowUp + "' requires handoff_task_id — " +
					"the successor task this one hands over to (create_task first)"
		}
		if id == t.ID {
			return nil, http.StatusUnprocessableEntity,
				"handoff_task_id must not be this task itself"
		}
		succ, err := s.dal.GetTask(id)
		if err != nil {
			return nil, http.StatusInternalServerError, err.Error()
		}
		if succ == nil {
			return nil, http.StatusUnprocessableEntity,
				"unknown successor task '" + id + "'"
		}
		if TaskIsTerminal(succ.Status) {
			return nil, http.StatusUnprocessableEntity,
				"successor task '" + id + "' is already closed (" + succ.Status +
					") — it cannot carry the follow-up work"
		}
		return &handoffPlan{
			Kind: HandoffFollowUp, TaskID: id, Note: strings.TrimSpace(note),
		}, 0, ""
	default: // HandoffReturnToCreator
		m, err := s.dal.GetMember(t.CreatorID)
		if err != nil {
			return nil, http.StatusInternalServerError, err.Error()
		}
		// Fail-closed and HONEST: we cannot put a durable task on somebody who
		// is not on the roster any more (a dismissed member, a released ow-
		// worker, the pre-column blank). Say so and name the other two doors
		// rather than pretending the ball landed.
		if m == nil || m.RosterStatus != RosterStatusActive {
			return nil, http.StatusUnprocessableEntity,
				"cannot hand back to creator '" + t.CreatorID +
					"': no active roster member with that id — use handoff='" +
					HandoffFollowUp + "' with an explicit successor task, or '" +
					HandoffNone + "' with a reason"
		}
		return &handoffPlan{
			Kind: HandoffReturnToCreator, Note: strings.TrimSpace(note),
		}, 0, ""
	}
}

// applyHandoffPlan performs the plan's side effects and stamps the declaration
// onto t (the caller persists t — closeTask's PutTask carries it). It runs
// AFTER the step write and BEFORE the derivation/close, so the successor task
// and its dep edge already exist when releaseDependentsOnClose walks them.
func (s *apiServer) applyHandoffPlan(
	t *Task, plan *handoffPlan, now float64, trigger string,
) error {
	if plan == nil {
		return nil
	}
	switch plan.Kind {
	case HandoffFollowUp:
		// Attach, never replace: the successor may already carry other blockers.
		if err := s.dal.AddTaskDep(plan.TaskID, t.ID); err != nil {
			return err
		}
		t.HandoffTaskID = plan.TaskID
	case HandoffReturnToCreator:
		followUp, err := s.mintCreatorFollowUpTask(*t, plan.Note, now, trigger)
		if err != nil {
			return err
		}
		t.HandoffTaskID = followUp.ID
	}
	t.Handoff = plan.Kind
	t.HandoffNote = plan.Note
	return nil
}

// mintCreatorFollowUpTask is option ① made durable. The complaint the ticket
// starts from is that the creator DOES get told (publishTask fans to the
// creator) and it still drops: an SSE delta is a line that scrolls away, and
// nothing in the system afterwards says "there is a ball on your side". The
// durable equivalent that already exists is a TASK — it sits on the creator's
// open list, it is counted in the resume snapshot (resumeTasksFor), it shows on
// the cockpit, and it only leaves by being worked or terminated. So that is
// what we mint: an ad-hoc task on the creator, blocked by the task that just
// finished (so half B fans the release the moment we close).
//
// It is deliberately step-LESS (status derives to not_started): the follow-up's
// first job is to decide what the follow-up work IS, which is the creator's
// call, not ours to plan for them.
func (s *apiServer) mintCreatorFollowUpTask(
	t Task, note string, now float64, trigger string,
) (Task, error) {
	no := TaskNo(t.ID)
	desc := "任務 " + no + "「" + t.Title + "」已由 " + t.ExecutorID +
		" 完成並關閉,球交回給你(這張任務的建立者)。\n\n" +
		"請決定後續:有後續工作就在這張任務上規劃步驟並執行(或轉派出去);" +
		"確認沒有後續就直接終止這張任務。\n\n" +
		"注意:" + no + " 已經是終態,它的 plan 永久凍結,後續工作只能開在這裡。"
	if note != "" {
		desc += "\n\n執行者的交棒說明:" + note
	}
	fu := Task{
		ID:           "t-" + newHexID(12),
		Title:        "接手 " + no + " 的後續:" + t.Title,
		Description:  desc,
		Status:       TaskStatusNotStarted,
		Priority:     TaskPriorityMid,
		ExecutorKind: TaskExecutorMember,
		ExecutorID:   t.CreatorID,
		// The finished task's executor is the one handing over, so it is the
		// creator of the follow-up — the ball's provenance stays readable.
		CreatorID: t.ExecutorID,
		CreatedTS: now,
		UpdatedTS: now,
	}
	if err := s.dal.PutTask(fu); err != nil {
		return Task{}, err
	}
	if err := s.dal.AddTaskDep(fu.ID, t.ID); err != nil {
		return Task{}, err
	}
	s.publishTask(fu, trigger)
	return fu, nil
}

// ── half B: dependency becomes a real handover ───────────────────────────────

// releaseDependentsOnClose runs at the tail of closeTask. Until T-74f8 a dep
// was a display marker and nothing else: the API contract said so out loud
// ("display markers, never a status change"), so a blocker finishing produced
// exactly nothing — the very mechanism the owner assumed would auto-start the
// next task ("設計完成以後自動轉開發") was decoration.
//
// Now the blocker's close walks its dependents. A dependent whose blockers are
// ALL terminal is released:
//   - its executor gets a DURABLE chat row (the persistent half — an SSE frame
//     alone is what failed in T-8a1e), plus the ordinary task delta;
//   - an unassigned OUTSOURCE dependent triggers an immediate scheduler tick.
//     outsource_sched.go now refuses to mint for a task with a live blocker, so
//     this tick is what actually turns "design done" into "dev worker spawned".
//
// It does NOT fake a status change: task.status is derived from steps (T-9ca5)
// and a released task honestly stays not_started until someone starts it. What
// changes is that it is now REACHABLE — schedulable and announced.
//
// Best-effort throughout: a fan/notify failure must never fail the close it
// follows (the closeTask convention).
func (s *apiServer) releaseDependentsOnClose(t Task, now float64, trigger string) {
	dependents, err := s.dal.ListTasksBlockedBy(t.ID)
	if err != nil {
		outsourceLog("deps-release %s: dependent read failed: %v", t.ID, err)
		return
	}
	tickOutsource := false
	for _, d := range dependents {
		if TaskIsTerminal(d.Status) {
			continue
		}
		blockers, err := s.dal.ListTaskDeps(d.ID)
		if err != nil {
			outsourceLog("deps-release %s: dep read of %s failed: %v", t.ID, d.ID, err)
			continue
		}
		if s.hasLiveBlocker(blockers) {
			continue // still blocked by something else — not released yet
		}
		d.UpdatedTS = now
		if err := s.dal.PutTask(d); err != nil {
			outsourceLog("deps-release %s: touch of %s failed: %v", t.ID, d.ID, err)
			continue
		}
		if d.ExecutorID != "" {
			s.postTaskChat(d, wireSystemSender, d.ExecutorID,
				"["+TaskNo(d.ID)+"] 擋住這張任務的前置任務 "+TaskNo(t.ID)+
					"「"+t.Title+"」已經"+t.Status+"了,它不再擋著你。"+
					"這張任務現在可以開始:請 get_task 讀內容、submit_plan 規劃步驟後開始執行。",
				trigger)
		}
		s.publishTask(d, trigger)
		if d.ExecutorKind == TaskExecutorOutsource && d.ExecutorID == "" {
			tickOutsource = true
		}
	}
	if tickOutsource {
		s.outsourceTickNow()
	}
}

// hasLiveBlocker reports whether any of the listed blocker ids is still a
// NON-terminal task. A blocker id that no longer resolves (the task was hard
// deleted) counts as gone, not as a permanent block — a dangling marker must
// never wedge a real task shut.
func (s *apiServer) hasLiveBlocker(blockerIDs []string) bool {
	for _, id := range blockerIDs {
		b, err := s.dal.GetTask(id)
		if err != nil {
			// Unreadable ⇒ treat as still blocking: releasing on an IO error
			// would be the unsafe direction (a spurious spawn).
			return true
		}
		if b != nil && !TaskIsTerminal(b.Status) {
			return true
		}
	}
	return false
}

// taskHasLiveBlocker is the scheduler-side twin of hasLiveBlocker, folded over
// snapshots the tick already holds (no per-candidate query). statusOf maps task
// id → status; an id missing from the map is a task that no longer exists and
// therefore does not block.
func taskHasLiveBlocker(blockerIDs []string, statusOf map[string]string) bool {
	for _, id := range blockerIDs {
		st, ok := statusOf[id]
		if ok && !TaskIsTerminal(st) {
			return true
		}
	}
	return false
}
