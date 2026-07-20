package main

// outsource_gate.go — 節點9 the SINGLE spawn-outsource choke (④⑦): every 發包
// path (create_task's `target`, reassign's outsource branch, and the scheduler
// tick's typed-outsource auto-spawn) funnels through outsourceSpawnGate, so
// authorization and accounting have NO side door (④). It mints nothing and
// opens no card — it only DECIDES; the caller acts on the verdict (admit →
// land the unassigned outsource task for the scheduler, deny → 403).
//
// T-35e0 (拆核可閘 → 外包上限自動排隊): the per-task owner-approval PENDING verdict
// is gone. An admitted 發包 lands an unassigned outsource task and the existing
// scheduler admits it under the global parallel cap (滿額排隊等產能).
//
// T-23cf (廢發包白名單): the per-agent delegation whitelist is gone too — cost
// is bounded by the scheduler's global parallel cap, so any AUTHENTICATED
// initiator (owner/admin, or any principal with a member row) may 發包. The
// only deny left is an unauthenticated identity: a non-owner sub with no
// member row (Initiator == nil), e.g. a warden token or an outsource worker.

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

// outsourceGateResult carries the verdict + why.
type outsourceGateResult struct {
	Decision outsourceGateDecision
	Reason   string
}

// outsourceSpawnGate is THE choke (④): authenticate the 發包, meter it (⑦),
// and decide admit vs deny.
//
//   - deny: an unauthenticated initiator — non-owner scope with no member row
//     (unknown sub: warden tokens, outsource workers). deny-by-default,
//     mirroring classifyMember;
//   - admit_spawn: everyone else — owner/admin, and ANY member/assistant
//     (T-23cf: no whitelist; the scheduler's global parallel cap bounds cost).
//     The task lands unassigned and the scheduler mints it under the cap.
//
// ⑦ (no back door): owner/admin dispatch traverses this SAME choke and is
// metered here too — the accounting hook fires for every admitted dispatch,
// never bypassed by scope.
func (s *apiServer) outsourceSpawnGate(req outsourceGateRequest) (outsourceGateResult, error) {
	approver := principalAtLeast(req.PrincipalClass, principalAdminAgent)
	if !approver && req.Initiator == nil {
		return outsourceGateResult{
			Decision: gateDeny,
			Reason:   "unauthenticated initiator (no member identity) may not 發包",
		}, nil
	}
	// ⑦ accounting choke — every dispatch that clears authn is metered here,
	// owner/admin included (no back door).
	s.meterOutsourceDispatch(req)
	return outsourceGateResult{Decision: gateAdmitSpawn}, nil
}

// meterOutsourceDispatch is the ⑦ accounting seam — the SINGLE point every
// admitted dispatch (owner/admin included) passes, so quota/記帳 can never be
// bypassed by scope.
//
// TODO(pass2): with a pre-spawn cost ESTIMATE (req.EstCost) bank it against a
// per-initiator budget. No estimate mechanism ships yet, so this is a
// deliberate no-op hook — the choke exists NOW so Pass 2 only fills the body.
func (s *apiServer) meterOutsourceDispatch(req outsourceGateRequest) {
	_ = req
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
