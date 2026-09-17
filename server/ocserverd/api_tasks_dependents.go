package main

// releaseDependentsOnClose runs at the tail of closeTask: the blocker's close
// walks its dependents. A dependent whose blockers are
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
		// T-51b0: BOTH executor kinds take this one notice again. T-e77f had
		// routed an outsource dependent through a separate kickoff seam; that
		// seam was withdrawn wholesale (owner 2026-08-15, card
		// rc-a4f6a7f8cd71), so this is the pre-T-e77f behaviour restored, not a
		// new decision — the alternative, telling a member and saying nothing to
		// a contractor, would silently make the release invisible to exactly the
		// executor least able to notice it on its own.
		//
		// ⚠️ What the withdrawn seam DID check and this line does not: a
		// dependent that is still FROZEN is told it can start (the release
		// removed only one of its two reasons to wait), and a worker that has
		// already left is still addressed. Both were true before T-e77f too.
		//
		// 🔴 THE NOTICE IS THE 〈解除阻擋〉 DOCUMENT NOW (T-3201), and that is
		// what puts the owner's approved rewrite on the wire. He approved
		// replacing the old single sentence 「這張任務現在可以開始:請 get_task
		// 讀內容、submit_plan 規劃步驟後開始執行。」 with three branches
		// (rc-8c0045ef7c38) — it assumed a released ticket had not started, and
		// there is live evidence of it saying so to one already in progress. The
		// rewrite landed in the seed and this line kept sending the old sentence,
		// so the approval was on disk and not on the wire until the document
		// became the notice. "" means it could not be rendered — post nothing
		// rather than a sentence with {blocker_title} still in it.
		if d.ExecutorID != "" {
			if notice := s.taskNoticeText(docKindTaskUnblocked, map[string]string{
				"blocked_task_no": TaskNo(d.ID),
			}); notice != "" {
				s.postTaskChat(d, wireSystemSender, d.ExecutorID, notice, trigger, nil)
			}
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
