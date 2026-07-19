package main

// outsource_policy.go — the 放行策略 (delegation policy) data model + read/write
// + the allowed_to_delegate authz helper for the first batch of "agent 發包給外包
// + owner 每筆核可閘" (scope ⑤⑥, migrations/00026). PURELY additive and not yet
// wired: no spawn path, route, or handler reads this — the later first-batch
// nodes that touch the 發包 path bolt it on. Mirrors the dal_tasks.go DAL
// conventions (explicit per-table Get/Put, JSON TEXT columns scanned in-struct)
// and the authz.go principal vocabulary.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// delegationPolicyDefaultID is the well-known principal_id of the GLOBAL
// default row — the policy every initiator without its own override resolves to
// (seeded by migrations/00026).
const delegationPolicyDefaultID = "__default__"

// budget_period closed set — the window auto_budget_cap is measured over
// (schema CHECK twin).
const (
	BudgetPeriodDay   = "day"
	BudgetPeriodWeek  = "week"
	BudgetPeriodMonth = "month"
	BudgetPeriodOnce  = "once"
)

// on_exhaust closed set — what happens when the 免卡 budget runs out (schema
// CHECK twin).
const (
	OnExhaustCard   = "card"   // fall back to a per-task owner card
	OnExhaustFreeze = "freeze" // freeze the delegation outright
)

// DelegationPolicy mirrors one outsource_delegation_policy row. AllowedRoles /
// AllowedMembers are the two allowed_to_delegate whitelists — role keys and
// owner-named member ids. AutoBudgetCap is nullable (nil = uncapped / money|
// null). NeedsPerTaskCard is the first-batch owner gate (default true).
type DelegationPolicy struct {
	PrincipalID      string
	AllowedRoles     []string
	AllowedMembers   []string
	NeedsPerTaskCard bool
	AutoBudgetCap    *float64
	BudgetPeriod     string // closed set BudgetPeriod*
	OnExhaust        string // closed set OnExhaust*
	UpdatedTS        float64
}

const delegationPolicyColumns = `principal_id, allowed_roles, allowed_members,
	needs_per_task_card, auto_budget_cap, budget_period, on_exhaust, updated_ts`

// defaultDelegationPolicy is the code-side fallback the resolver returns when
// even the seed row is absent — the same first-batch posture the seed encodes:
// nobody auto-delegates, every 發包 needs a per-task card.
func defaultDelegationPolicy() DelegationPolicy {
	return DelegationPolicy{
		PrincipalID:      delegationPolicyDefaultID,
		AllowedRoles:     []string{},
		AllowedMembers:   []string{},
		NeedsPerTaskCard: true,
		BudgetPeriod:     BudgetPeriodOnce,
		OnExhaust:        OnExhaustCard,
	}
}

func scanDelegationPolicy(row interface{ Scan(...any) error }) (DelegationPolicy, error) {
	var p DelegationPolicy
	var roles, members string
	var needsCard int
	var cap sql.NullFloat64
	err := row.Scan(
		&p.PrincipalID, &roles, &members, &needsCard, &cap,
		&p.BudgetPeriod, &p.OnExhaust, &p.UpdatedTS,
	)
	if err != nil {
		return DelegationPolicy{}, err
	}
	p.NeedsPerTaskCard = needsCard != 0
	if cap.Valid {
		v := cap.Float64
		p.AutoBudgetCap = &v
	}
	if err := json.Unmarshal([]byte(roles), &p.AllowedRoles); err != nil {
		return DelegationPolicy{}, fmt.Errorf("delegation policy %s: bad allowed_roles JSON: %w", p.PrincipalID, err)
	}
	if err := json.Unmarshal([]byte(members), &p.AllowedMembers); err != nil {
		return DelegationPolicy{}, fmt.Errorf("delegation policy %s: bad allowed_members JSON: %w", p.PrincipalID, err)
	}
	return p, nil
}

// GetDelegationPolicy returns one policy row by principal_id, or nil if absent
// (no override / default not seeded). Raw fetch — ResolveDelegationPolicy is
// the override-or-default read most callers want.
func (d *DAL) GetDelegationPolicy(principalID string) (*DelegationPolicy, error) {
	row := d.db.QueryRow(
		`SELECT `+delegationPolicyColumns+
			` FROM outsource_delegation_policy WHERE principal_id = ?`, principalID)
	p, err := scanDelegationPolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ResolveDelegationPolicy returns the policy in force for an initiator: its own
// per-principal override when present, else the global default row, else the
// code-side default (never a zero value). The first batch runs on the global
// default alone.
func (d *DAL) ResolveDelegationPolicy(principalID string) (DelegationPolicy, error) {
	if principalID != "" && principalID != delegationPolicyDefaultID {
		override, err := d.GetDelegationPolicy(principalID)
		if err != nil {
			return DelegationPolicy{}, err
		}
		if override != nil {
			return *override, nil
		}
	}
	def, err := d.GetDelegationPolicy(delegationPolicyDefaultID)
	if err != nil {
		return DelegationPolicy{}, err
	}
	if def != nil {
		return *def, nil
	}
	return defaultDelegationPolicy(), nil
}

// PutDelegationPolicy upserts one policy row (the '__default__' global default
// or a per-principal override). Nil whitelists store as '[]'; a nil
// AutoBudgetCap stores as NULL; empty period / on_exhaust fall to their
// first-batch defaults so a partial write never trips the schema CHECK.
func (d *DAL) PutDelegationPolicy(p DelegationPolicy) error {
	roles := p.AllowedRoles
	if roles == nil {
		roles = []string{}
	}
	members := p.AllowedMembers
	if members == nil {
		members = []string{}
	}
	rolesBlob, err := json.Marshal(roles)
	if err != nil {
		return err
	}
	membersBlob, err := json.Marshal(members)
	if err != nil {
		return err
	}
	needsCard := 0
	if p.NeedsPerTaskCard {
		needsCard = 1
	}
	var capArg any
	if p.AutoBudgetCap != nil {
		capArg = *p.AutoBudgetCap
	}
	period := p.BudgetPeriod
	if period == "" {
		period = BudgetPeriodOnce
	}
	onExhaust := p.OnExhaust
	if onExhaust == "" {
		onExhaust = OnExhaustCard
	}
	_, err = d.db.Exec(`
		INSERT INTO outsource_delegation_policy (`+delegationPolicyColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (principal_id) DO UPDATE SET
			allowed_roles = excluded.allowed_roles,
			allowed_members = excluded.allowed_members,
			needs_per_task_card = excluded.needs_per_task_card,
			auto_budget_cap = excluded.auto_budget_cap,
			budget_period = excluded.budget_period,
			on_exhaust = excluded.on_exhaust,
			updated_ts = excluded.updated_ts`,
		p.PrincipalID, string(rolesBlob), string(membersBlob), needsCard,
		capArg, period, onExhaust, p.UpdatedTS,
	)
	return err
}

// delegationAllowed reports whether a delegation INITIATOR is authorized to
// 發包 under policy. Owner scope is always allowed (the top of the authz.go
// ladder); every other initiator is allowed ONLY when the policy names it — its
// role_key in the role whitelist, or its member id in the owner-named member
// whitelist. A nil member (unknown sub) is never authorized (deny-by-default,
// mirroring classifyMember). PURE helper — deliberately NOT wired into any
// route/handler/spawn choke; the later first-batch node does the wiring.
func delegationAllowed(principalClass string, m *Member, policy DelegationPolicy) bool {
	if principalClass == principalOwner {
		return true
	}
	if m == nil {
		return false
	}
	for _, rk := range policy.AllowedRoles {
		if rk != "" && rk == m.RoleKey {
			return true
		}
	}
	for _, id := range policy.AllowedMembers {
		if id != "" && id == m.ID {
			return true
		}
	}
	return false
}
