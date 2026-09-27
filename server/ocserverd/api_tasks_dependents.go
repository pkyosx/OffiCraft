package main

// releaseDependentsOnClose runs at the tail of closeTask. A dependent whose
// blockers are ALL terminal gets a DURABLE chat row for its executor (an SSE
// frame alone is not enough) plus the task delta; an unassigned OUTSOURCE
// dependent triggers an immediate scheduler tick — outsource_sched.go refuses to
// mint for a task with a live blocker, so this tick is what spawns the worker.
// No status is faked: task.status is derived from steps, so a released task
// stays not_started. Best-effort: a failure here must never fail the close.
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
			continue
		}
		d.UpdatedTS = now
		if err := s.dal.PutTask(d); err != nil {
			outsourceLog("deps-release %s: touch of %s failed: %v", t.ID, d.ID, err)
			continue
		}
		// BOTH executor kinds take this one notice (owner rc-a4f6a7f8cd71 withdrew
		// the separate outsource kickoff seam). ⚠️ Not checked here: a still-FROZEN
		// dependent is told it can start, and a worker that already left is still
		// addressed. The notice is the 〈解除阻擋〉 document; "" means it could not
		// be rendered — post nothing rather than a sentence with a raw {slot}.
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

func (s *apiServer) hasLiveBlocker(blockerIDs []string) bool {
	for _, id := range blockerIDs {
		b, err := s.dal.GetTask(id)
		if err != nil {
			// Unreadable ⇒ still blocking: releasing on an IO error is the unsafe
			// direction (a spurious spawn). A deleted blocker (nil) does not block.
			return true
		}
		if b != nil && !TaskIsTerminal(b.Status) {
			return true
		}
	}
	return false
}

// taskHasLiveBlocker is the scheduler-side twin of hasLiveBlocker over the
// tick's snapshots; an id missing from statusOf no longer exists and does not
// block.
func taskHasLiveBlocker(blockerIDs []string, statusOf map[string]string) bool {
	for _, id := range blockerIDs {
		st, ok := statusOf[id]
		if ok && !TaskIsTerminal(st) {
			return true
		}
	}
	return false
}
