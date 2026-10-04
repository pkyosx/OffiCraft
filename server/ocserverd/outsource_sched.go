package main

// outsource_sched.go — the outsource-assignment scheduler (the reconcile.go
// twin). Caps are owner rulings: global cap 0 = 暫停指派 (③); a type manual's
// copies = at most N parallel TASKS of that type (H6); member tasks never count
// toward the global cap (H7). A minted worker stays 'assigned' until its first
// report_waking flips it 'active'.

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type outsourceCandidate struct {
	TaskID        string
	TypeKey       string
	Priority      string
	CreatedTS     float64
	TargetRuntime string
	TargetModel   string
	TargetEffort  string
	TargetMachine string
	// Dispatched mirrors task.outsource_dispatched: true = the Target fields are
	// an explicit 發包 target overriding the type manual; false = they are the
	// creator snapshot and fill only what the live manual leaves unset. Never
	// infer this from the fields being empty: that left manual-driven tasks with
	// no machine, so their workers could never boot (T-8a67).
	Dispatched bool
}

func (c outsourceCandidate) hasExplicitTarget() bool { return c.Dispatched }

type outsourceTypeSpec struct {
	Copies  int
	Runtime string
	Model   string
	Effort  string
	Machine string
}

type outsourceAssignment struct {
	TaskID         string
	TypeKey        string
	Runtime        string
	Model          string
	Effort         string
	Machine        string
	ExplicitTarget bool
}

func (a outsourceAssignment) hasExplicitTarget() bool { return a.ExplicitTarget }

// The reassigning lock counts as awaiting: a partially-done task reassigned to
// outsource derives to in_progress, so status alone would miss the successor.
func outsourceAwaitingAssignment(t Task) bool {
	if t.ExecutorKind != TaskExecutorOutsource || t.ExecutorID != "" ||
		t.Priority == TaskPriorityFrozen {
		return false
	}
	return t.Status == TaskStatusNotStarted || t.Lock == TaskLockReassigning
}

func taskPriorityRank(priority string) int {
	switch priority {
	case TaskPriorityHigh:
		return 0
	case TaskPriorityMid:
		return 1
	case TaskPriorityLow:
		return 2
	}
	return 3
}

func sortOutsourceQueue(cands []outsourceCandidate) []outsourceCandidate {
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := taskPriorityRank(cands[i].Priority), taskPriorityRank(cands[j].Priority)
		if ri != rj {
			return ri < rj
		}
		if cands[i].CreatedTS != cands[j].CreatedTS {
			return cands[i].CreatedTS < cands[j].CreatedTS
		}
		return cands[i].TaskID < cands[j].TaskID
	})
	return cands
}

// globalCap 0 pauses assignment (owner ruling ③); < 0 means unlimited
// (SettingsDTO -1).
func outsourceDecide(
	cands []outsourceCandidate,
	specs map[string]outsourceTypeSpec,
	liveByType map[string]int,
	liveTotal int,
	globalCap int,
) []outsourceAssignment {
	if globalCap == 0 {
		return nil
	}
	byType := make(map[string]int, len(liveByType))
	for k, v := range liveByType {
		byType[k] = v
	}
	admitted := map[string]bool{}
	var out []outsourceAssignment
	for _, c := range sortOutsourceQueue(append([]outsourceCandidate(nil), cands...)) {
		if globalCap > 0 && liveTotal >= globalCap {
			break
		}
		if c.Priority == TaskPriorityFrozen || admitted[c.TaskID] {
			continue
		}
		var spec outsourceTypeSpec
		if c.hasExplicitTarget() {
			// The per-type copies cap binds regardless of dispatch source (owner
			// ruling T-b6e9).
			spec = outsourceTypeSpec{
				Copies: 0, Runtime: NormalizeRuntime(c.TargetRuntime), Model: c.TargetModel,
				Effort: c.TargetEffort, Machine: c.TargetMachine,
			}
			if c.TypeKey != "" {
				if ts, ok := specs[c.TypeKey]; ok {
					spec.Copies = ts.Copies
				}
			}
		} else {
			var ok bool
			spec, ok = specs[c.TypeKey]
			if !ok || spec.Copies < 0 {
				continue
			}
			// The live manual decides every field it states; the creator snapshot
			// fills only what it leaves unstated — so an owner's manual edit wins on
			// the next tick, yet a silent manual never resolves to "no machine"
			// (a worker minted without one never boots).
			spec = fillTypeSpecFromSnapshot(spec, c)
		}
		if spec.Copies > 0 && byType[c.TypeKey] >= spec.Copies {
			continue
		}
		admitted[c.TaskID] = true
		byType[c.TypeKey]++
		liveTotal++
		out = append(out, outsourceAssignment{
			TaskID:         c.TaskID,
			TypeKey:        c.TypeKey,
			Runtime:        NormalizeRuntime(spec.Runtime),
			Model:          spec.Model,
			Effort:         spec.Effort,
			Machine:        spec.Machine,
			ExplicitTarget: c.hasExplicitTarget(),
		})
	}
	return out
}

func fillTypeSpecFromSnapshot(spec outsourceTypeSpec, c outsourceCandidate) outsourceTypeSpec {
	resolved := defaultedDispatchSpec(fillDispatchSpecFrom(
		dispatchSpec{Runtime: spec.Runtime, Model: spec.Model,
			Effort: spec.Effort, Machine: spec.Machine},
		dispatchSpec{Runtime: c.TargetRuntime, Model: c.TargetModel,
			Effort: c.TargetEffort, Machine: c.TargetMachine}))
	spec.Runtime, spec.Model = resolved.Runtime, resolved.Model
	spec.Effort, spec.Machine = resolved.Effort, resolved.Machine
	return spec
}

func outsourceSpecOf(m TaskManual) *outsourceTypeSpec {
	assignee, err := manualAssignee(m)
	if err != nil {
		return nil
	}
	if kind, _ := assignee["kind"].(string); kind != TaskExecutorOutsource {
		return nil
	}
	spec := outsourceTypeSpec{Copies: 1, Runtime: RuntimeClaude, Effort: "medium"}
	if v, ok := assignee["runtime"].(string); ok && strings.TrimSpace(v) != "" {
		spec.Runtime = strings.TrimSpace(v)
	}
	if v, ok := assignee["model"].(string); ok {
		spec.Model = strings.TrimSpace(v)
	}
	if v, ok := assignee["effort"].(string); ok && strings.TrimSpace(v) != "" {
		spec.Effort = strings.TrimSpace(v)
	}
	if v, ok := assignee["copies"].(float64); ok {
		spec.Copies = int(v)
	}
	if v, ok := assignee["machine"].(string); ok && strings.TrimSpace(v) != "" {
		spec.Machine = strings.TrimSpace(v)
	}
	return &spec
}

func outsourceLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[outsource] "+format+"\n", args...)
}

func (s *apiServer) runOutsourceTick(now float64) {
	defer func() {
		if r := recover(); r != nil {
			outsourceLog("tick FAULT: %v", r)
		}
	}()
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()

	tasks, err := s.dal.ListTasks()
	if err != nil {
		outsourceLog("tick: task read failed: %v", err)
		return
	}
	all, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		outsourceLog("tick: worker read failed: %v", err)
		return
	}
	var workers []OutsourceWorker
	for _, w := range all {
		// The driver guard sits at the head, on the raw roster read, before ANY
		// pass has looked at the row — the twin of runReconcileTick's guard. A
		// no-op today (ListOutsourceWorkers returns only kind='outsource' rows);
		// it exists so both halves claim rows by name for the parity test, and so
		// a row the other half drives never consumes this half's cap.
		if lifecycleTickDriverFor(memberFromWorker(w)) != driverOutsource {
			continue
		}
		workers = append(workers, w)
	}
	manuals, err := s.foldTaskManuals()
	if err != nil {
		outsourceLog("tick: manual read failed: %v", err)
		return
	}

	// Codenames fold over released rows too: a codename is never reused
	// (contract §A.4).
	typeOf := make(map[string]string, len(tasks))
	for _, t := range tasks {
		typeOf[t.ID] = t.TypeKey
	}
	liveByType := map[string]int{}
	liveTotal := 0
	codenames := make([]string, 0, len(workers))
	for _, w := range workers {
		codenames = append(codenames, w.Codename)
		if w.Status == WorkerStatusReleased {
			continue
		}
		liveTotal++
		liveByType[typeOf[w.TaskID]]++
	}

	// Must run BEFORE the assignment queue, which may be empty (early return
	// below) while workers still need care. The shared staff wind-down passes
	// run on the worker projection inside runWorkerLifecyclePasses
	// (lifecycle_roster.go); do not re-type them here.
	s.runWorkerLifecyclePasses(workers, now)

	for _, w := range workers {
		// Runs before the FSM below, so a queued 起來 spent at the converged-offline
		// edge is started in the same tick.
		s.consumeWorkerRestartAfterStop(&w, now)
		// Re-fire a parked refused kill FIRST, before any branch below can
		// re-spawn onto the same machine.
		s.retryPendingWorkerStop(w.ID, now)
		switch w.Status {
		case WorkerStatusAssigned, WorkerStatusActive:
			// The FSM is the ONLY collector of a wind-down or 停止 epoch: do not gate it
			// on RefocusSince == 0, desired_state or !online (decideUp's recycle arm needs
			// Online; decideDown collects an offline session). A healthy online worker
			// with no epoch dispatches nothing.
			s.reconcileWorkerLiveness(w, now)
		case WorkerStatusReleased:
			if w.ReleasedTS > 0 && now-w.ReleasedTS >= workerReclaimGraceSecs &&
				!s.workerReclaimed[w.ID] {
				s.reclaimWorkerSession(w)
			}
		}
	}

	// Handover-timeout reaper: a `reassigning` task whose updated time has not
	// moved (artifact writes do not move it) means the successor never called
	// claim_task, so the predecessor outsource worker would leak its session.
	// Release it by its OWN id, never by task_id — an outsource→outsource
	// takeover bound a fresh worker to the same task_id.
	timeout := float64(s.reassignHandoverTimeout())
	for _, t := range tasks {
		if t.Lock != TaskLockReassigning ||
			t.ReassignedFromKind != TaskExecutorOutsource || t.ReassignedFrom == "" {
			continue
		}
		if now-t.UpdatedTS < timeout {
			continue
		}
		released, err := s.dal.ReleaseWorkerByID(t.ReassignedFrom, now)
		if err != nil {
			outsourceLog("handover-timeout %s: release %s failed: %v",
				t.ID, t.ReassignedFrom, err)
			continue
		}
		if released != nil {
			s.publishOutsourceWorker(*released, triggerServer)
			outsourceLog("handover-timeout %s: predecessor %s never handed off "+
				"(%.0fs) — reclaimed", t.ID, t.ReassignedFrom, now-t.UpdatedTS)
		}
		if !s.workerReclaimed[t.ReassignedFrom] {
			if w, err := s.dal.GetOutsourceWorker(t.ReassignedFrom); err == nil && w != nil {
				s.reclaimWorkerSession(*w)
			}
		}
	}

	// Deps hold the 發包 queue by the blocker's LIVE status; the dependent is
	// minted when the blocker closes (closeTask → releaseDependentsOnClose →
	// outsourceTickNow).
	deps, err := s.dal.AllTaskDeps()
	if err != nil {
		outsourceLog("tick: dep read failed: %v", err)
		return
	}
	statusOf := make(map[string]string, len(tasks))
	for _, t := range tasks {
		statusOf[t.ID] = t.Status
	}
	// Deliberately ONE check, on this snapshot — no second copy at the bind
	// site; a dep landing in between is at worst one period late.
	blocked := func(taskID string) bool {
		return taskHasLiveBlocker(deps[taskID], statusOf)
	}

	var cands []outsourceCandidate
	for _, t := range tasks {
		if !outsourceAwaitingAssignment(t) {
			continue
		}
		if blocked(t.ID) {
			continue
		}
		cands = append(cands, outsourceCandidate{
			TaskID: t.ID, TypeKey: t.TypeKey,
			Priority: t.Priority, CreatedTS: t.CreatedTS,
			TargetRuntime: t.OutsourceRuntime,
			TargetModel:   t.OutsourceModel, TargetEffort: t.OutsourceEffort,
			TargetMachine: t.OutsourceMachine,
			Dispatched:    t.OutsourceDispatched,
		})
	}
	if len(cands) == 0 {
		return
	}

	specs := map[string]outsourceTypeSpec{}
	for _, m := range manuals {
		if spec := outsourceSpecOf(m); spec != nil {
			specs[m.TypeKey] = *spec
		}
	}

	decisions := outsourceDecide(cands, specs, liveByType, liveTotal,
		s.outsourceParallelCap())
	for _, d := range decisions {
		t, err := s.dal.GetTask(d.TaskID)
		if err != nil {
			outsourceLog("assign %s: re-read failed: %v", d.TaskID, err)
			continue
		}
		if t == nil || !outsourceAwaitingAssignment(*t) {
			continue
		}
		// Explicit 發包 targets were already authorized at the handler with the
		// true initiator; re-gating by the task's CREATOR would wrongly deny an
		// owner's reassign of a subordinate's task. Deny → left queued.
		if !d.hasExplicitTarget() {
			principal, initiator, perr := s.resolveDispatchInitiator(t.CreatorID)
			if perr != nil {
				outsourceLog("assign %s: initiator resolve failed: %v", t.ID, perr)
			}
			gate, err := s.outsourceSpawnGate(outsourceGateRequest{
				PrincipalClass: principal, Initiator: initiator, TaskID: t.ID,
				Runtime: d.Runtime, Model: d.Model, Effort: d.Effort, Machine: d.Machine,
				IssuedBy: t.CreatorID,
			})
			if err != nil {
				outsourceLog("assign %s: gate failed: %v", t.ID, err)
				continue
			}
			if gate.Decision == gateDeny {
				outsourceLog("assign %s: 發包 denied for creator %q — left queued",
					t.ID, t.CreatorID)
				continue
			}
		}
		worker := OutsourceWorker{
			ID:           "ow-" + newHexID(12),
			Codename:     DeriveCodename(d.Model, codenames),
			Runtime:      NormalizeRuntime(d.Runtime),
			Model:        d.Model,
			Effort:       d.Effort,
			TaskID:       t.ID,
			Status:       WorkerStatusAssigned,
			CreatedTS:    now,
			DesiredState: DesiredStateOnline,
		}
		codenames = append(codenames, worker.Codename)
		// The worker and the bind land together, on the task row as it is now: a
		// task closed or taken since the read above is left alone, and a bind
		// that fails leaves no worker behind.
		bound := false
		if err := s.dal.inTx(func(tx *writeTx) error {
			cur, err := getTaskOn(tx, t.ID)
			if err != nil || cur == nil || !outsourceAwaitingAssignment(*cur) {
				return err
			}
			if err := createMemberOn(tx, memberFromWorker(worker)); err != nil {
				return err
			}
			cur.ExecutorID = worker.ID
			cur.UpdatedTS = now
			if err := putTaskOn(tx, *cur, taskWriteUpsert); err != nil {
				return err
			}
			t, bound = cur, true
			return nil
		}); err != nil {
			outsourceLog("assign %s: worker write and task bind failed, neither landed: %v", t.ID, err)
			continue
		}
		if !bound {
			continue
		}
		if d.hasExplicitTarget() {
			s.workerMachinePref[worker.ID] = d.Machine
		}
		// No kickoff notice on bind (owner ruling rc-a4f6a7f8cd71): the worker's
		// boot context already carries this task.
		s.publishOutsourceWorker(worker, triggerServer)
		s.publishTask(*t, triggerServer)
		outsourceLog("assigned %s (%s) → task %s (type %q, model %q)",
			worker.ID, worker.Codename, t.ID, t.TypeKey, worker.Model)
		s.notifyWorkerSpawn(worker, now)
	}
}

// outsourceTickNow is the event-driven tick (create_task seam): assign a
// just-landed task now rather than up to a period later; the cadence stays an
// idempotent backstop.
func (s *apiServer) outsourceTickNow() {
	if s.noOutsource {
		return
	}
	s.runOutsourceTick(nowSecs())
}
