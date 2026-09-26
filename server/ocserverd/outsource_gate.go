package main

// Every 發包 path (create_task's `target`, reassign's outsource branch, the
// scheduler tick's typed-outsource auto-spawn) must funnel through
// outsourceSpawnGate so authorization and accounting have no side door.
//
// No per-agent whitelist and no per-task owner approval (T-23cf, T-35e0): any
// authenticated initiator may 發包; cost is bounded by the scheduler's global
// parallel cap.

type outsourceGateDecision string

const (
	gateAdmitSpawn outsourceGateDecision = "admit_spawn"
	gateDeny       outsourceGateDecision = "deny"
)

type outsourceGateRequest struct {
	PrincipalClass principalClass
	Initiator      *Member
	TaskID         string
	Runtime        string
	Model          string
	Effort         string
	Machine        string
	IssuedBy       string
	EstCost        *float64
}

type outsourceGateResult struct {
	Decision outsourceGateDecision
	Reason   string
}

func (s *apiServer) outsourceSpawnGate(req outsourceGateRequest) (outsourceGateResult, error) {
	approver := principalAtLeast(req.PrincipalClass, principalAdminAgent)
	if !approver && req.Initiator == nil {
		return outsourceGateResult{
			Decision: gateDeny,
			Reason:   "unauthenticated initiator (no member identity) may not 發包",
		}, nil
	}
	s.meterOutsourceDispatch(req)
	return outsourceGateResult{Decision: gateAdmitSpawn}, nil
}

// meterOutsourceDispatch is a deliberate no-op accounting hook (owner/admin
// pass it too); Pass 2 banks req.EstCost here once an estimate mechanism ships.
func (s *apiServer) meterOutsourceDispatch(req outsourceGateRequest) {
	_ = req
}

func (s *apiServer) resolveDispatchInitiator(actorID string) (principalClass, *Member, error) {
	if actorID == "" || actorID == wireOwnerID {
		return principalOwner, nil, nil
	}
	m, err := s.dal.GetMember(actorID)
	if err != nil {
		return principalAgent, nil, err
	}
	return classifyMember(m), m, nil
}
