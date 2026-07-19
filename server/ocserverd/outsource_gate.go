package main

// outsource_gate.go — 節點9 the SINGLE spawn-outsource choke (④⑦): every 發包
// path (create_task's `target`, reassign's outsource branch, and the scheduler
// tick's typed-outsource auto-spawn) funnels through outsourceSpawnGate, so
// authorization and accounting have NO side door (④). The gate is pure policy
// over the already-built layers: delegationAllowed (authz) + the accounting
// hook. It mints nothing and opens no card — it only DECIDES; the caller acts on
// the verdict (admit → land the unassigned outsource task for the scheduler,
// deny → 403).
//
// T-35e0 (拆核可閘 → 外包上限自動排隊): the per-task owner-approval PENDING verdict
// is gone. An authorized 發包 no longer parks for a per-task card — it lands an
// unassigned outsource task and the existing scheduler admits it under the global
// parallel cap (滿額排隊等產能). The gate now returns only admit / deny.

// outsourceGateDecision is the choke's verdict (④).
type outsourceGateDecision string

const (
	gateAdmitSpawn outsourceGateDecision = "admit_spawn" // land unassigned → scheduler
	gateDeny       outsourceGateDecision = "deny"        // 403 — not permitted to 發包
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
// policy, authorize the 發包, meter it (⑦), and decide admit vs deny.
//
//   - deny: an initiator the policy does not name (delegationAllowed = false).
//     owner/admin are standing approvers and never fall here;
//   - admit_spawn: authorized — the task lands unassigned and the scheduler
//     mints it under the global parallel cap (T-35e0: no per-task card gate).
//
// ⑦ (no back door): owner/admin dispatch traverses this SAME choke and is
// metered here too — the accounting hook fires for every admitted dispatch,
// never bypassed by scope.
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
	return outsourceGateResult{Decision: gateAdmitSpawn, Policy: policy}, nil
}

// meterOutsourceDispatch is the ⑦ accounting seam — the SINGLE point every
// authorized dispatch (owner/admin included) passes, so quota/記帳 can never be
// bypassed by scope.
//
// TODO(pass2): with a pre-spawn cost ESTIMATE (req.EstCost) bank it against the
// initiator's auto_budget_cap over budget_period and enforce on_exhaust
// (card|freeze). No estimate mechanism ships yet, so this is a deliberate no-op
// hook — the choke exists NOW so Pass 2 only fills the body.
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
