package main

import "testing"

// TestResolveDelegationPolicy covers the seeded global default (first-batch
// posture), per-principal override precedence, and the fall-through back to the
// default when an initiator has no override.
func TestResolveDelegationPolicy(t *testing.T) {
	d := newTestDAL(t)

	// The migration seed row IS the first-batch default: per-task card required,
	// nobody auto-delegates, once/card, uncapped.
	def, err := d.ResolveDelegationPolicy(delegationPolicyDefaultID)
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if !def.NeedsPerTaskCard {
		t.Fatalf("first-batch default must require a per-task card, got %+v", def)
	}
	if len(def.AllowedRoles) != 0 || len(def.AllowedMembers) != 0 {
		t.Fatalf("default whitelists must be empty, got %+v", def)
	}
	if def.AutoBudgetCap != nil {
		t.Fatalf("default cap must be uncapped (nil), got %v", *def.AutoBudgetCap)
	}
	if def.BudgetPeriod != BudgetPeriodOnce || def.OnExhaust != OnExhaustCard {
		t.Fatalf("default period/on_exhaust must be once/card, got %q/%q", def.BudgetPeriod, def.OnExhaust)
	}

	// An initiator with no override resolves TO the default.
	got, err := d.ResolveDelegationPolicy("m-nobody")
	if err != nil || got.NeedsPerTaskCard != true || got.PrincipalID != delegationPolicyDefaultID {
		t.Fatalf("unknown initiator must resolve to the default, got %+v (%v)", got, err)
	}

	// A per-principal override wins over the default.
	if err := d.PutDelegationPolicy(DelegationPolicy{
		PrincipalID:      "m-lead",
		AllowedRoles:     []string{"lead"},
		NeedsPerTaskCard: false,
	}); err != nil {
		t.Fatalf("put override: %v", err)
	}
	got, err = d.ResolveDelegationPolicy("m-lead")
	if err != nil || got.PrincipalID != "m-lead" || got.NeedsPerTaskCard {
		t.Fatalf("override must win, got %+v (%v)", got, err)
	}
}

// TestDelegationPolicyRoundTrip pins the DAL upsert: whitelists, the nullable
// cap, and the enums survive a Put/Get, and a re-Put updates in place.
func TestDelegationPolicyRoundTrip(t *testing.T) {
	d := newTestDAL(t)

	if p, err := d.GetDelegationPolicy("m-x"); err != nil || p != nil {
		t.Fatalf("absent policy must be (nil, nil), got (%v, %v)", p, err)
	}

	cap := 250.0
	want := DelegationPolicy{
		PrincipalID:      "m-x",
		AllowedRoles:     []string{"lead", "scrum_master"},
		AllowedMembers:   []string{"m-42"},
		NeedsPerTaskCard: false,
		AutoBudgetCap:    &cap,
		BudgetPeriod:     BudgetPeriodWeek,
		OnExhaust:        OnExhaustFreeze,
		UpdatedTS:        12.5,
	}
	if err := d.PutDelegationPolicy(want); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := d.GetDelegationPolicy("m-x")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.AutoBudgetCap == nil || *got.AutoBudgetCap != cap {
		t.Fatalf("cap must round-trip %v, got %v", cap, got.AutoBudgetCap)
	}
	gotCopy := *got
	gotCopy.AutoBudgetCap = want.AutoBudgetCap // pointer aside, value checked above
	if len(gotCopy.AllowedRoles) != 2 || gotCopy.AllowedRoles[1] != "scrum_master" ||
		len(gotCopy.AllowedMembers) != 1 || gotCopy.AllowedMembers[0] != "m-42" {
		t.Fatalf("whitelists must round-trip, got %+v", gotCopy)
	}
	if gotCopy.BudgetPeriod != BudgetPeriodWeek || gotCopy.OnExhaust != OnExhaustFreeze {
		t.Fatalf("enums must round-trip, got %q/%q", gotCopy.BudgetPeriod, gotCopy.OnExhaust)
	}

	// Re-Put with a nil cap clears the ceiling back to NULL and updates in place.
	want.AutoBudgetCap = nil
	want.AllowedRoles = nil
	if err := d.PutDelegationPolicy(want); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err = d.GetDelegationPolicy("m-x")
	if err != nil || got == nil || got.AutoBudgetCap != nil || len(got.AllowedRoles) != 0 {
		t.Fatalf("upsert must clear cap/roles, got %+v (%v)", got, err)
	}
}

// TestDelegationPolicyBudgetPeriodCheckRejectsUnknown pins the schema CHECK on
// the two enum columns.
func TestDelegationPolicyBudgetPeriodCheckRejectsUnknown(t *testing.T) {
	d := newTestDAL(t)
	if _, err := d.db.Exec(
		`INSERT INTO outsource_delegation_policy (principal_id, budget_period) VALUES ('m-bad', 'fortnight')`,
	); err == nil {
		t.Fatal("unknown budget_period must be rejected by the CHECK")
	}
	if _, err := d.db.Exec(
		`INSERT INTO outsource_delegation_policy (principal_id, on_exhaust) VALUES ('m-bad2', 'explode')`,
	); err == nil {
		t.Fatal("unknown on_exhaust must be rejected by the CHECK")
	}
}

// TestDelegationAllowed covers the allowed_to_delegate helper's branches: owner
// is always allowed, a role-whitelisted or member-named initiator is allowed,
// everyone else (including a nil member) is denied.
func TestDelegationAllowed(t *testing.T) {
	policy := DelegationPolicy{
		AllowedRoles:   []string{"lead"},
		AllowedMembers: []string{"m-picked"},
	}
	lead := &Member{ID: "m-1", RoleKey: "lead"}
	picked := &Member{ID: "m-picked", RoleKey: "assistant"}
	plain := &Member{ID: "m-2", RoleKey: "assistant"}

	cases := []struct {
		name           string
		principalClass string
		member         *Member
		want           bool
	}{
		{"owner always allowed even with nil member", principalOwner, nil, true},
		{"role-whitelisted agent allowed", principalAgent, lead, true},
		{"owner-named member allowed", principalAgent, picked, true},
		{"plain agent denied", principalAgent, plain, false},
		{"nil non-owner denied", principalAgent, nil, false},
		{"machine denied", principalMachine, plain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegationAllowed(tc.principalClass, tc.member, policy); got != tc.want {
				t.Fatalf("delegationAllowed = %v, want %v", got, tc.want)
			}
		})
	}

	// An empty policy authorizes no agent (deny-by-default) but still lets the
	// owner through.
	empty := DelegationPolicy{}
	if delegationAllowed(principalAgent, lead, empty) {
		t.Fatal("empty policy must deny a non-owner initiator")
	}
	if !delegationAllowed(principalOwner, nil, empty) {
		t.Fatal("empty policy must still allow the owner")
	}
}
