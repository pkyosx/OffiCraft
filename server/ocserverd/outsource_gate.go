package main

// outsource_gate.go — 節點9 the SINGLE spawn-outsource choke (④⑦) plus its
// pending-landing (⑤) and the durable dispatch intent DAL (migrations/00027).
//
// EVERY 發包 path funnels through outsourceSpawnGate — create_task's new
// `target`, reassign's outsource branch, and the scheduler tick's typed-outsource
// auto-spawn — so authorization and accounting have NO side door (④). The gate
// is pure policy over the already-built layers: delegationAllowed (authz),
// ResolveDelegationPolicy (needs_per_task_card), and the accounting hook. It
// mints nothing and opens no card itself — it only DECIDES; the caller acts on
// the verdict (admit → mint+spawn, pending → landPendingOutsource, deny → 403).

import (
	"database/sql"
	"errors"
	"fmt"
)

// outsourceGateDecision is the choke's verdict (④).
type outsourceGateDecision string

const (
	gateAdmitSpawn outsourceGateDecision = "admit_spawn" // mint + spawn now
	gateDeny       outsourceGateDecision = "deny"        // 403 — not permitted to 發包
	gatePending    outsourceGateDecision = "pending"     // park for owner per-task approval
)

// outsourceGateRequest is one dispatch intent presented to the choke.
type outsourceGateRequest struct {
	PrincipalClass string   // the initiator's principal class (authz.go ladder)
	Initiator      *Member  // the initiator's member row (nil for owner scope)
	TaskID         string   // the task being dispatched
	Model          string   // target worker model
	Effort         string   // target reasoning effort
	Machine        string   // target machine placement preference
	IssuedBy       string   // the actor that dispatched (發起者) — verified token sub
	EstCost        *float64 // OPTIONAL pre-spawn cost estimate (Pass 2); nil today
}

// outsourceGateResult carries the verdict + why + the policy it resolved.
type outsourceGateResult struct {
	Decision outsourceGateDecision
	Reason   string
	Policy   DelegationPolicy
}

// outsourceSpawnGate is THE choke (④): resolve the initiator's delegation
// policy, authorize the 發包, meter it (⑦), and decide admit vs pending vs deny.
//
//   - deny: an initiator the policy does not name (delegationAllowed = false).
//     owner/admin are standing approvers and never fall here;
//   - pending: authorized but the policy demands a per-task owner card AND the
//     initiator is a SUBORDINATE (not a standing approver — the owner/admin's own
//     dispatch carries implicit approval, so it admits);
//   - admit_spawn: authorized and either no card is required or the initiator is
//     the approver themselves.
//
// ⑦ (no back door): owner/admin dispatch traverses this SAME choke and is
// metered here too — the accounting hook fires for every admitted-or-pending
// dispatch, never bypassed by scope.
func (s *apiServer) outsourceSpawnGate(req outsourceGateRequest) (outsourceGateResult, error) {
	policy, err := s.dal.ResolveDelegationPolicy(req.IssuedBy)
	if err != nil {
		return outsourceGateResult{}, err
	}
	approver := principalAtLeast(req.PrincipalClass, principalAdminAgent)
	if !approver && !delegationAllowed(req.PrincipalClass, req.Initiator, policy) {
		return outsourceGateResult{
			Decision: gateDeny, Policy: policy,
			Reason: "initiator not permitted to 發包 by the delegation policy",
		}, nil
	}
	// ⑦ accounting choke — every dispatch that clears authz is metered here,
	// owner/admin included (no back door).
	s.meterOutsourceDispatch(req, policy)
	if policy.NeedsPerTaskCard && !approver {
		return outsourceGateResult{
			Decision: gatePending, Policy: policy,
			Reason: "owner per-task approval required before spawn",
		}, nil
	}
	return outsourceGateResult{Decision: gateAdmitSpawn, Policy: policy}, nil
}

// meterOutsourceDispatch is the ⑦ accounting seam — the SINGLE point every
// authorized dispatch (owner/admin included) passes, so quota/記帳 can never be
// bypassed by scope.
//
// TODO(pass2): with a pre-spawn cost ESTIMATE (req.EstCost) bank it against the
// initiator's auto_budget_cap over budget_period and enforce on_exhaust
// (card|freeze). The 第一批 ships no estimate mechanism and runs with
// needs_per_task_card=true, so the 免卡 budget path is dormant and this is a
// deliberate no-op hook — the choke exists NOW so Pass 2 only fills the body.
func (s *apiServer) meterOutsourceDispatch(req outsourceGateRequest, policy DelegationPolicy) {
	_ = req
	_ = policy
}

// resolveDispatchInitiator classifies a dispatch initiator from its actor id
// (the verified token sub / a task's creator) where no *http.Request is in hand
// (the scheduler tick's typed-outsource auto-spawn): the owner literal → owner
// scope with a nil member; else the caller's member row → classifyMember. A
// missing row is a plain agent with a nil member (deny-by-default at the gate).
func (s *apiServer) resolveDispatchInitiator(actorID string) (string, *Member, error) {
	if actorID == "" || actorID == wireOwnerID {
		return principalOwner, nil, nil
	}
	m, err := s.dal.GetMember(actorID)
	if err != nil {
		return principalAgent, nil, err
	}
	return classifyMember(m), m, nil
}

// landPendingOutsource lands a gated-PENDING dispatch (⑤): open the owner
// approval reply card (bound to the task), persist the OutsourceIntent, then flip
// the task to pending_outsource_approval. The card is created FIRST so a mid-way
// fault leaves the task on its prior (dispatchable) status rather than pending
// with no card behind it. The caller must already have verified
// CanDispatchOutsource(t.Status) (the no-double-dispatch guard). The owner's
// answer → mint is Pass 2's job; here the flow stops at the open card.
func (s *apiServer) landPendingOutsource(t *Task, model, effort, machine, issuedBy string, now float64, trigger string) error {
	if !CanOutsourceApprovalTransition(t.Status, TaskStatusPendingOutsourceApproval) {
		return fmt.Errorf("task %s cannot dispatch for approval from status %q", t.ID, t.Status)
	}
	version := int64(1)
	prev, err := s.dal.GetOutsourceIntent(t.ID)
	if err != nil {
		return err
	}
	if prev != nil {
		version = prev.Version + 1
	}
	modelLabel := model
	if modelLabel == "" {
		modelLabel = "預設模型"
	}
	summary := fmt.Sprintf("外包發包待核可:任務「%s」擬發給外包(model %s, effort %s)。核准後才會開工。",
		t.Title, modelLabel, effort)
	if _, problem, err := s.openReplyCard(issuedBy, ReplyCardCreateDTO{
		Kind:    replyCardKindDecision,
		Summary: summary,
		Options: []string{"核准發包", "取消"},
	}, t.ID, ""); err != nil {
		return err
	} else if problem != "" {
		return fmt.Errorf("open approval card: %s", problem)
	}
	if err := s.dal.PutOutsourceIntent(OutsourceIntent{
		TaskID: t.ID, Version: version, Model: model, Effort: effort,
		Machine: machine, IssuedBy: issuedBy,
	}, now); err != nil {
		return err
	}
	t.Status = TaskStatusPendingOutsourceApproval
	t.WaitingReason = ""
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	s.publishTask(*t, trigger)
	return nil
}

// mintOutsourceWorker mints a fresh outsource worker bound to taskID and
// persists it — the admit-path helper for the LOCK-FREE callers (create_task's
// dispatch; NOT the scheduler tick, which already holds s.outsourceMu and mints
// inline). Takes s.outsourceMu itself. On a persist fault it returns the error
// leaving NO bound worker (fail-closed — the caller has not yet persisted the
// task, so no orphan results, ③). Returns the minted worker so the caller
// re-points the task's executor and dispatches the spawn.
func (s *apiServer) mintOutsourceWorker(taskID, model, effort, machine string, now float64) (*OutsourceWorker, error) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()
	return s.mintOutsourceWorkerLocked(taskID, model, effort, machine, now)
}

// mintOutsourceWorkerLocked is the lock-FREE mint core — the caller already
// holds s.outsourceMu (the approve-spawn settle path, which mints + spawns under
// one lock exactly like the scheduler tick).
func (s *apiServer) mintOutsourceWorkerLocked(taskID, model, effort, machine string, now float64) (*OutsourceWorker, error) {
	existing, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		return nil, err
	}
	codenames := make([]string, 0, len(existing))
	for _, ww := range existing {
		codenames = append(codenames, ww.Codename)
	}
	worker := OutsourceWorker{
		ID:           "ow-" + newHexID(12),
		Codename:     DeriveCodename(model, codenames),
		Model:        model,
		Effort:       effort,
		TaskID:       taskID,
		Status:       WorkerStatusAssigned,
		CreatedTS:    now,
		DesiredState: DesiredStateOnline,
	}
	if err := s.dal.PutOutsourceWorker(worker); err != nil {
		return nil, err
	}
	s.workerMachinePref[worker.ID] = machine
	return &worker, nil
}

// ── the durable dispatch intent (migrations/00027) ───────────────────────────

// GetOutsourceIntent returns the parked dispatch proposal for a task, or nil.
func (d *DAL) GetOutsourceIntent(taskID string) (*OutsourceIntent, error) {
	row := d.db.QueryRow(
		`SELECT task_id, version, model, effort, machine, issued_by
			FROM outsource_intent WHERE task_id = ?`, taskID)
	var oi OutsourceIntent
	err := row.Scan(&oi.TaskID, &oi.Version, &oi.Model, &oi.Effort, &oi.Machine, &oi.IssuedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &oi, nil
}

// PutOutsourceIntent upserts one parked dispatch proposal (one row per task_id;
// a re-dispatch overwrites in place, carrying the bumped version). created_ts is
// stamped on first insert; updated_ts on every write.
func (d *DAL) PutOutsourceIntent(oi OutsourceIntent, now float64) error {
	_, err := d.db.Exec(`
		INSERT INTO outsource_intent
			(task_id, version, model, effort, machine, issued_by, created_ts, updated_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id) DO UPDATE SET
			version = excluded.version,
			model = excluded.model,
			effort = excluded.effort,
			machine = excluded.machine,
			issued_by = excluded.issued_by,
			updated_ts = excluded.updated_ts`,
		oi.TaskID, oi.Version, oi.Model, oi.Effort, oi.Machine, oi.IssuedBy, now, now)
	return err
}

// DeleteOutsourceIntent removes the parked proposal for a task — the CONSUME
// step of the approve/cancel settle (its absence is the exactly-once guard: a
// second settle finds no intent and no-ops). Idempotent (deleting an absent row
// is a no-op).
func (d *DAL) DeleteOutsourceIntent(taskID string) error {
	_, err := d.db.Exec(`DELETE FROM outsource_intent WHERE task_id = ?`, taskID)
	return err
}

// ── 節點10 the owner-approval settle (⑧⑨⑩⑪⑫) ─────────────────────────────────

// the outsource-approval reply card's decision options (landPendingOutsource
// opens the card with exactly these two, in this order).
const (
	outsourceApproveOptionIdx = 0 // 核准發包 → mint + spawn
	outsourceCancelOptionIdx  = 1 // 取消 → un-dispatch back to not_started
)

// approvalDecisionFromAnswer maps an owner's card answer to approve|cancel: the
// 核准 option (idx 0) approves; the 取消 option, any other option, or a bare
// text/attachment answer are all read as CANCEL — a fresh worker never spawns on
// an ambiguous answer (G1: owner 答卡＝核可/否決).
func approvalDecisionFromAnswer(optionIdx *int) bool {
	return optionIdx != nil && *optionIdx == outsourceApproveOptionIdx
}

// isOutsourceApprovalPending reports whether a card is the LIVE owner-approval
// card of a still-pending dispatch: bound to a task parked at
// pending_outsource_approval with a persisted intent behind it. One intent +
// one such card per task, so this identifies the approval card without a stored
// marker.
func (s *apiServer) isOutsourceApprovalPending(card ReplyCard) (bool, *Task, *OutsourceIntent, error) {
	if card.TaskID == "" {
		return false, nil, nil, nil
	}
	t, err := s.dal.GetTask(card.TaskID)
	if err != nil || t == nil || t.Status != TaskStatusPendingOutsourceApproval {
		return false, t, nil, err
	}
	intent, err := s.dal.GetOutsourceIntent(t.ID)
	if err != nil || intent == nil {
		return false, t, nil, err
	}
	return true, t, intent, nil
}

// settleOutsourceApproval is the card-answer / expiry seam for a pending
// dispatch (⑧⑨⑩⑪⑫). It is a cheap no-op for any card that is not the live
// approval card of a pending task (a normal gate card, an already-settled or
// re-dispatched one), so it is safe to call from every card-settle path.
//
//   - ⑨ CAS: mint only while the task is STILL pending AND the live intent
//     version is unchanged (CanApproveOutsourceIntent). A card whose task moved
//     on (cancelled / reassigned / re-dispatched) no-ops — nothing spawns;
//   - ⑩ exactly-once: the intent is CONSUMED (deleted) before the mint, so a
//     concurrent or PUT-replayed settle finds it gone and no-ops
//     (OutsourceIntentIdempotencyKey(taskID, version) is the logical dedupe key);
//   - approve → mint a FRESH worker + spawn, landing the task exactly as the
//     create_task admit path does (not_started + bound executor, no predecessor);
//   - cancel / 否決 / TTL-expiry → un-dispatch back to not_started, no spawn (⑪);
//   - ⑫ a spawn-time re-estimation runs at the accounting seam before the mint.
func (s *apiServer) settleOutsourceApproval(card ReplyCard, approve bool, now float64, trigger string) error {
	if card.TaskID == "" {
		return nil
	}
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()

	t, err := s.dal.GetTask(card.TaskID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != TaskStatusPendingOutsourceApproval {
		return nil // no longer a pending dispatch — CAS no-op (⑧⑨)
	}
	intent, err := s.dal.GetOutsourceIntent(t.ID)
	if err != nil {
		return err
	}
	if intent == nil {
		return nil // already consumed — exactly-once no-op (⑩)
	}
	// ⑨ CAS over the live intent: expected == the version this card was opened
	// against (a re-dispatch would have expired this card + rewritten the row, so
	// a stale approval cannot reach here); the predicate also re-asserts pending.
	if !CanApproveOutsourceIntent(t.Status, intent.Version, intent.Version) {
		return nil
	}
	// ⑩ CONSUME first — a concurrent/replayed settle now finds no intent.
	if err := s.dal.DeleteOutsourceIntent(t.ID); err != nil {
		return err
	}

	if !approve {
		// ⑧⑪ cancel / 否決 / TTL: un-dispatch to the backlog, spawn nothing.
		if !CanOutsourceApprovalTransition(t.Status, TaskStatusNotStarted) {
			return fmt.Errorf("task %s cannot un-dispatch from status %q", t.ID, t.Status)
		}
		t.Status = TaskStatusNotStarted
		t.WaitingReason = ""
		t.UpdatedTS = now
		if err := s.dal.PutTask(*t); err != nil {
			return err
		}
		s.publishTask(*t, trigger)
		return nil
	}

	// ⑫ spawn-time re-estimation at the accounting seam, then mint EXACTLY ONCE.
	s.meterOutsourceSpawn(*t, *intent, now)
	worker, err := s.mintOutsourceWorkerLocked(t.ID, intent.Model, intent.Effort, intent.Machine, now)
	if err != nil {
		return err
	}
	// Land exactly as the create_task admit path (a fresh worker, no predecessor):
	// not_started + bound executor; the worker claims and reports not_started →
	// in_progress itself. The scheduler never re-picks it (executor_id is set).
	t.ExecutorKind = TaskExecutorOutsource
	t.ExecutorID = worker.ID
	t.Status = TaskStatusNotStarted
	t.WaitingReason = ""
	t.UpdatedTS = now
	if err := s.dal.PutTask(*t); err != nil {
		return err
	}
	s.publishOutsourceWorker(*worker, trigger)
	s.publishTask(*t, trigger)
	s.notifyWorkerSpawn(*worker, now)
	return nil
}

// meterOutsourceSpawn is the ⑫ spawn-time re-estimation + accounting seam — the
// point the approve→spawn actually WALKS THROUGH before minting (the ⑦ "no back
// door" continued at spawn time). It re-reads the placement viability at spawn
// time (warden availability for the intent's machine preference) and logs the
// spawn's model/effort/machine so a divergence from what the owner saw on the
// card surfaces. The FIRST batch caps nothing (needs_per_task_card=true, so the
// 免卡 budget path is dormant); the LIVE per-worker cost is banked by the
// existing telemetry → bankLiveCost machinery once the worker reports. Kept
// deliberately thin: no pre-spawn cost oracle exists yet, so this is the honest
// re-estimation hook, not a fabricated number.
func (s *apiServer) meterOutsourceSpawn(t Task, intent OutsourceIntent, now float64) {
	machine := intent.Machine
	if machine == "" {
		machine = "auto"
	}
	warden := s.pickWorkerWarden(OutsourceWorker{ID: "estimate-" + t.ID, Model: intent.Model}, machine, now)
	outsourceLog("approve-spawn estimate task=%s model=%q effort=%q machine=%q warden=%q (idempotency-key %s)",
		t.ID, intent.Model, intent.Effort, machine,
		warden, OutsourceIntentIdempotencyKey(t.ID, intent.Version))
}
