package main

// outsource_sched.go — the M3 Phase 2 outsource-assignment scheduler, the
// second background producer (isomorphic to reconcile.go — contract §B.4):
//
//   * outsourceDecide — the PURE admission core (the reconcileDecide twin):
//     (queue snapshot, per-type manual specs, live worker counts, the global
//     cap) → the ordered assignment list. All IO lives in runOutsourceTick.
//   * the 30s cadence tick (startOutsourceCadence) + the event-driven single
//     tick (outsourceTickNow — fired when create_task lands an outsource
//     task, so an assignment never waits a full period).
//   * --no-outsource (serve flag) disables the producer WHOLESALE — cadence
//     AND the event-driven tick — the --no-reconcile mirror: a shadow server
//     must never mint workers against the production queue.
//
// Admission (contract §B.4, owner rulings ③/H6/H7):
//   * candidates: status='not_started' ∧ executor_kind='outsource' ∧
//     executor_id='' ∧ priority≠'frozen', ordered high→mid→low then
//     created_ts (FIFO within a priority band);
//   * per-type cap: the manual assignee's copies — at most N live
//     (assigned+active) workers of that type (H6: N parallel TASKS, the
//     one-worker-one-task model holds);
//   * global cap: task.outsource_max_parallel over ALL live workers
//     (0 = assignment paused; member tasks never count — H7).
//
// An assignment mints the worker as a kind='outsource' MEMBER row (A案 P7d
// fold — 外包＝正職; codename via the domain derivation over EVERY codename the
// member table has ever carried, removed rows included, so a codename is never
// reused; model/effort from the manual assignee), writes task.executor_id, and
// fans the task + outsource_worker SSE deltas. The worker then sits in
// 'assigned' until its first GET /api/self/task claim flips it 'active'.
//
// The scheduler keeps NO in-memory ledger (unlike reconcile's store): the
// worker rows ARE the bookkeeping — every tick recounts from the DB, so a
// restart forgets nothing and the cadence is an idempotent backstop of the
// event-driven tick (a task with executor_id set simply stops matching).

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// outsourceCadenceSecs is the scheduler tick period — sized like the
// reconcile cadence (§B.4: 30s scan + event-driven immediate tick).
const outsourceCadenceSecs = 30.0

// ── pure decision core (the reconcileDecide twin) ────────────────────────────

// outsourceCandidate is one unassigned outsource task in the queue — the
// decide input projection (never the full Task: the core stays IO-free).
type outsourceCandidate struct {
	TaskID    string
	TypeKey   string
	Priority  string
	CreatedTS float64
	// TargetModel / TargetEffort / TargetMachine is the task's explicit 發包
	// target (T-35e0, task.outsource_*): non-empty for a create/reassign
	// dispatch. When present, the decide uses it (global cap only, no per-type
	// copy limit — an explicit dispatch was already authorized at the handler)
	// instead of the type manual's assignee spec.
	TargetModel   string
	TargetEffort  string
	TargetMachine string
}

// hasExplicitTarget reports whether the candidate carries an explicit 發包
// target (a create/reassign dispatch). A dispatch always defaults effort→medium
// and machine→auto, so a plain manual-driven outsource task (all three empty) is
// reliably distinguished.
func (c outsourceCandidate) hasExplicitTarget() bool {
	return c.TargetModel != "" || c.TargetEffort != "" || c.TargetMachine != ""
}

// outsourceTypeSpec is one manual's outsource assignee spec: how many copies
// of the type may run at once (0 = 無限 — unlimited, spec TaskManualDTO), the
// worker template to mint from, and the spawn placement preference ("auto" or
// a machine id — carried through for the worker-spawn seam, see
// notifyWorkerSpawn; the ADMISSION decision never reads it).
type outsourceTypeSpec struct {
	Copies  int
	Model   string
	Effort  string
	Machine string // "auto" | machine id; consumed by the Phase 6 spawn seam
}

// outsourceAssignment is one decided assignment: mint a (Model, Effort)
// worker on Machine and bind it to TaskID.
type outsourceAssignment struct {
	TaskID  string
	TypeKey string
	Model   string
	Effort  string
	Machine string // "auto" | machine id — the resolved spawn placement preference
	// FromTarget marks an assignment resolved from an explicit 發包 target (vs a
	// type manual's assignee spec): the sched gate is skipped for it (already
	// authorized at the dispatch handler) and it pins its own machine preference.
	FromTarget bool
}

// hasTarget reports whether the assignment came from an explicit 發包 target.
func (a outsourceAssignment) hasTarget() bool { return a.FromTarget }

// outsourceAwaitingAssignment reports whether a task is an UNASSIGNED outsource
// slot the scheduler should mint for: an outsource track with no bound executor,
// not frozen, and either not_started (a fresh create / a fully-reset reassign) OR
// held under the reassigning lock (a handover successor slot — a partially-done
// task reassigned to outsource derives to in_progress, so status alone would miss
// it; the lock is the honest "awaiting the successor mint" signal). T-35e0.
func outsourceAwaitingAssignment(t Task) bool {
	if t.ExecutorKind != TaskExecutorOutsource || t.ExecutorID != "" ||
		t.Priority == TaskPriorityFrozen {
		return false
	}
	return t.Status == TaskStatusNotStarted || t.Lock == TaskLockReassigning
}

// taskPriorityRank orders the queue: high before mid before low; frozen (and
// any junk) sorts last AND is skipped by the decide loop — a frozen task is
// never assigned (SPEC §3.3: the scheduler skips frozen wholesale).
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

// sortOutsourceQueue orders candidates priority-then-created_ts (task id as
// the deterministic tie-break). In place; returns its argument for chaining.
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

// outsourceDecide is the PURE admission function: walk the queue in order and
// admit every candidate that fits BOTH caps, folding each admission into the
// running counts so one call never over-assigns (same-tick idempotence).
//
//   - globalCap == 0 pauses assignment outright (owner ruling ③: 0 = 暫停指派);
//     globalCap < 0 means 無限 — UNLIMITED, no global cap (spec SettingsDTO: -1);
//   - liveByType / liveTotal count the ALREADY-live (assigned+active) workers;
//   - a candidate with no outsource spec for its type is skipped (the manual
//     was edited away from outsource after the task was created — the task
//     stays queued, honestly unassignable until governance resolves it);
//   - a per-type spec with Copies == 0 is 無限 (unlimited copies of the type;
//     spec TaskManualDTO) — only the global cap can then stop it;
//   - a duplicate task id in the queue is admitted at most once.
//
// liveByType is treated as read-only (the fold rides a local copy).
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
			break // the global cap gates EVERY type — nothing further can fit
		}
		if c.Priority == TaskPriorityFrozen || admitted[c.TaskID] {
			continue
		}
		var spec outsourceTypeSpec
		if c.hasExplicitTarget() {
			// An explicit 發包 target (create/reassign dispatch) rides the GLOBAL
			// cap only — no per-type copy limit (it was authorized at the handler,
			// not a type-copy admission). Copies == 0 = 無限 per-type.
			spec = outsourceTypeSpec{
				Copies: 0, Model: c.TargetModel,
				Effort: c.TargetEffort, Machine: c.TargetMachine,
			}
		} else {
			var ok bool
			spec, ok = specs[c.TypeKey]
			if !ok || spec.Copies < 0 {
				continue
			}
			// Copies == 0 is 無限 (unlimited per-type copies) — never capped here.
			if spec.Copies > 0 && byType[c.TypeKey] >= spec.Copies {
				continue
			}
		}
		admitted[c.TaskID] = true
		byType[c.TypeKey]++
		liveTotal++
		out = append(out, outsourceAssignment{
			TaskID:     c.TaskID,
			TypeKey:    c.TypeKey,
			Model:      spec.Model,
			Effort:     spec.Effort,
			Machine:    spec.Machine,
			FromTarget: c.hasExplicitTarget(),
		})
	}
	return out
}

// outsourceSpecOf extracts a manual's outsource assignee spec: nil for {} /
// a member assignee / undecodable JSON. copies defaults to 1 when absent
// (validateManualAssignee enforces >= 0 on write; 0 = 無限/unlimited); effort
// defaults to the schema's 'medium'; machine defaults to "auto" (spec
// TaskManualDTO — the placement preference rides through to the spawn seam).
func outsourceSpecOf(m TaskManual) *outsourceTypeSpec {
	assignee, err := manualAssignee(m)
	if err != nil {
		return nil
	}
	if kind, _ := assignee["kind"].(string); kind != TaskExecutorOutsource {
		return nil
	}
	spec := outsourceTypeSpec{Copies: 1, Effort: "medium", Machine: "auto"}
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

// ── logging ──────────────────────────────────────────────────────────────────

func outsourceLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[outsource] "+format+"\n", args...)
}

// ── the tick (snapshot → decide → mint/bind/fan) ─────────────────────────────

// runOutsourceTick runs ONE scheduler tick: snapshot the queue + the live
// worker counts from the DB, decide, then mint/bind/fan each assignment.
// Serialized with the event-driven tick via outsourceMu; best-effort — a
// fault is logged, never raised into the cadence loop.
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
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		outsourceLog("tick: worker read failed: %v", err)
		return
	}
	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		outsourceLog("tick: manual read failed: %v", err)
		return
	}

	// Live (assigned+active) worker counts — per type via the bound task, plus
	// the global total. Codenames fold over EVERY row ever minted (released
	// included): a codename is never reused (contract §A.4).
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

	// Worker lifecycle passes (Phase 6, worker_spawn.go) — BEFORE the
	// assignment queue (which may be empty while workers still need care):
	//   * a worker stuck in 'assigned' gets its worker_start (re)dispatched,
	//     paced by workerSpawnRetrySecs (a lost frame / offline warden heals
	//     here — the cadence is the retry loop, exactly like reconcile);
	//   * a RELEASED worker whose session was never reclaimed (no close-out
	//     report arrived) is force-reclaimed after workerReclaimGraceSecs —
	//     the grace exists so a released worker can finish its §6.3 close-out
	//     duties before the session is taken.
	for _, w := range workers {
		// A refused live-worker kill (owner 停止/relocate toward a warden that
		// dropped offline) is parked, never lost — re-fire it FIRST, before any
		// branch below can re-spawn onto the same machine (P5a rework).
		s.retryPendingWorkerStop(w.ID)
		switch w.Status {
		case WorkerStatusAssigned:
			// Owner-explicit stop dominates every auto-revival (desired_state
			// mirrors member: an offline worker is held down, never reconciled
			// back up), so the FSM rescue below must NOT quietly bring it back.
			// Skip it wholesale — the member-parity guard.
			if w.DesiredState == DesiredStateOffline {
				continue
			}
			// A案 P6: the spawn/rescue path runs through the SHARED member
			// reconcile FSM (start_timeout / backoff / zombie-takeover) — the
			// paced re-dispatch, the ghost-clear (retired recoverStuckWorker),
			// and the lost-frame heal are all its decisions now.
			s.reconcileWorkerLiveness(w, now)
		case WorkerStatusActive:
			// T-32e1: the context-high auto-handover — an ACTIVE worker whose
			// gauge crosses the HANDOVER band is refocused (kill+respawn) using
			// the SAME ctxHighConfig the members use; also runs the refocus
			// loop-break so a completed (manual OR auto) handover clears itself.
			// autoHandoverWorker self-guards a stopped worker (its row re-read
			// returns early on StoppedSince>0), so owner-explicit stop dominates
			// here too — no separate guard needed (that would only mask the
			// internal one).
			s.autoHandoverWorker(w, now)
			// A案 P6: an ACTIVE worker whose session DIED (claimed once, SSE gone
			// — the old spawn_state=stuck latch) is rescued by the same FSM:
			// respawn with backoff, zombie-takeover on a clobbered START. Masked
			// while a handover is in flight (refocus owns the respawn there) and
			// while owner-stopped (held down).
			if w.DesiredState != DesiredStateOffline && w.RefocusSince == 0.0 &&
				!s.hub.IsOnline(w.ID) {
				s.reconcileWorkerLiveness(w, now)
			}
		case WorkerStatusReleased:
			if w.ReleasedTS > 0 && now-w.ReleasedTS >= workerReclaimGraceSecs &&
				!s.workerReclaimed[w.ID] {
				s.reclaimWorkerSession(w)
			}
		}
	}

	// T-ba04 handover-timeout reaper: a task left in `reassigning` past
	// reassignHandoverTimeoutSecs means the successor never reported the
	// takeover (reassigning→in_progress), so the PREDECESSOR outsource worker —
	// kept live at reassign time to host the handover dialogue rather than being
	// dismissed up front — would otherwise leak its session indefinitely.
	// Release + reclaim that predecessor here, by its OWN id (never by task_id:
	// an outsource→outsource takeover bound a fresh worker to the same task_id).
	// The task stays `reassigning` (the owner still owns recovering it); only the
	// orphaned predecessor session is reclaimed. A member predecessor is never
	// reclaimed (it lives on its own member lifecycle) — outsource only.
	for _, t := range tasks {
		if t.Lock != TaskLockReassigning ||
			t.ReassignedFromKind != TaskExecutorOutsource || t.ReassignedFrom == "" {
			continue
		}
		if now-t.UpdatedTS < reassignHandoverTimeoutSecs {
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

	var cands []outsourceCandidate
	for _, t := range tasks {
		if !outsourceAwaitingAssignment(t) {
			continue
		}
		cands = append(cands, outsourceCandidate{
			TaskID: t.ID, TypeKey: t.TypeKey,
			Priority: t.Priority, CreatedTS: t.CreatedTS,
			TargetModel: t.OutsourceModel, TargetEffort: t.OutsourceEffort,
			TargetMachine: t.OutsourceMachine,
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
		// Re-read the task before binding: a handler write (terminate, freeze)
		// may have raced the snapshot — never bind a worker to a task that no
		// longer qualifies.
		t, err := s.dal.GetTask(d.TaskID)
		if err != nil {
			outsourceLog("assign %s: re-read failed: %v", d.TaskID, err)
			continue
		}
		if t == nil || !outsourceAwaitingAssignment(*t) {
			continue
		}
		// ④ authz choke. An explicit 發包 target (create/reassign dispatch) was
		// ALREADY authorized at the handler where the true initiator principal was
		// known — re-gating it here (by the task's CREATOR, who may differ from the
		// reassigner) would wrongly deny an owner's reassign of a subordinate's
		// task. So the gate runs ONLY for a plain manual-driven outsource task,
		// whose implicit 發包 is by its CREATOR (deny → left queued; admit → mint).
		if !d.hasTarget() {
			principal, initiator, perr := s.resolveDispatchInitiator(t.CreatorID)
			if perr != nil {
				outsourceLog("assign %s: initiator resolve failed: %v", t.ID, perr)
			}
			gate, err := s.outsourceSpawnGate(outsourceGateRequest{
				PrincipalClass: principal, Initiator: initiator, TaskID: t.ID,
				Model: d.Model, Effort: d.Effort, Machine: d.Machine,
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
			Model:        d.Model,
			Effort:       d.Effort,
			TaskID:       t.ID,
			Status:       WorkerStatusAssigned,
			CreatedTS:    now,
			DesiredState: DesiredStateOnline, // system intends a fresh worker running
		}
		codenames = append(codenames, worker.Codename) // same-tick MAX+1 keeps advancing
		if err := s.dal.PutOutsourceWorker(worker); err != nil {
			outsourceLog("assign %s: worker write failed: %v", t.ID, err)
			continue
		}
		if d.hasTarget() {
			// An explicit 發包 target carries its own placement — the task has no
			// type manual to fall back to, so pin the spawn-seam preference here.
			s.workerMachinePref[worker.ID] = d.Machine
		}
		t.ExecutorID = worker.ID
		t.UpdatedTS = now
		if err := s.dal.PutTask(*t); err != nil {
			outsourceLog("assign %s: task bind failed: %v", t.ID, err)
			continue
		}
		s.publishOutsourceWorker(worker, triggerServer)
		s.publishTask(*t, triggerServer)
		outsourceLog("assigned %s (%s) → task %s (type %q, model %q)",
			worker.ID, worker.Codename, t.ID, t.TypeKey, worker.Model)
		s.notifyWorkerSpawn(worker, now)
	}
}

// notifyWorkerSpawn (the former Phase 6 seam) now lives in worker_spawn.go:
// it assembles the worker boot context, server-mints the ow- token, and
// pushes a worker_start frame onto an online warden's command FIFO.

// ── the cadence + the event-driven seam ──────────────────────────────────────

// startOutsourceCadence mounts the 30s scheduler loop (sleep-then-tick, the
// startReconcileCadence twin). Never called when --no-outsource is set.
func (s *apiServer) startOutsourceCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.runOutsourceTick(nowSecs())
		}
	}()
	outsourceLog("cadence started (period=%gs)", period.Seconds())
}

// outsourceTickNow is the EVENT-DRIVEN immediate tick — the create_task seam
// (an outsource task just landed unassigned; assign it now rather than up to
// a full period later). Shares runOutsourceTick's mutex + DB recount, so the
// cadence stays an idempotent backstop. Gated OFF wholesale by --no-outsource.
func (s *apiServer) outsourceTickNow() {
	if s.noOutsource {
		return
	}
	s.runOutsourceTick(nowSecs())
}
