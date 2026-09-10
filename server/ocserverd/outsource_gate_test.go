// Skeleton generated from server/ocserverd/outsource_gate.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestOutsourceSpawnGate(t *testing.T) {
	api := &apiServer{}
	cases := []struct {
		name      string
		principal string
		member    *Member
		want      outsourceGateDecision
		reason    string
	}{
		{name: "owner", principal: "owner", want: gateAdmitSpawn},
		{name: "assistant", principal: "admin_agent", want: gateAdmitSpawn},
		{name: "authenticated member", principal: "agent", member: &Member{ID: "kip"}, want: gateAdmitSpawn},
		{name: "unknown member identity", principal: "agent", want: gateDeny,
			reason: "unauthenticated initiator (no member identity) may not 發包"},
	}

	principals := map[string]principalClass{
		"owner":       principalOwner,
		"admin_agent": principalAdminAgent,
		"agent":       principalAgent,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := api.outsourceSpawnGate(outsourceGateRequest{
				PrincipalClass: principals[tc.principal],
				Initiator:      tc.member,
			})
			if err != nil {
				t.Fatalf("outsourceSpawnGate: %v", err)
			}
			if got.Decision != tc.want {
				t.Fatalf("decision = %q, want %q", got.Decision, tc.want)
			}
			if got.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", got.Reason, tc.reason)
			}
		})
	}
}

func TestMeterOutsourceDispatch(t *testing.T) {
	api := &apiServer{}
	api.meterOutsourceDispatch(outsourceGateRequest{
		PrincipalClass: principalOwner,
		TaskID:         "T-125",
		Runtime:        RuntimeCodex,
		Model:          "gpt-5",
		Effort:         "high",
		Machine:        "m-server-self",
		IssuedBy:       wireOwnerID,
	})
}

func TestResolveDispatchInitiator(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	cases := []struct {
		name          string
		actorID       string
		wantPrincipal string
		wantMemberID  string
	}{
		{name: "empty actor is owner", actorID: "", wantPrincipal: "owner"},
		{name: "owner actor is owner", actorID: wireOwnerID, wantPrincipal: "owner"},
		{name: "ordinary member", actorID: apiTestPlainAgentID, wantPrincipal: "agent", wantMemberID: apiTestPlainAgentID},
		{name: "assistant member", actorID: seedMiraID, wantPrincipal: "admin_agent", wantMemberID: seedMiraID},
		{name: "warden member", actorID: ServerSelfHost, wantPrincipal: "machine", wantMemberID: ServerSelfHost},
		{name: "unknown actor is an unauthenticated agent", actorID: "ghost", wantPrincipal: "agent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPrincipal, gotMember, err := api.resolveDispatchInitiator(tc.actorID)
			if err != nil {
				t.Fatalf("resolveDispatchInitiator(%q): %v", tc.actorID, err)
			}
			if gotPrincipal.String() != tc.wantPrincipal {
				t.Fatalf("principal = %q, want %q", gotPrincipal, tc.wantPrincipal)
			}
			if tc.wantMemberID == "" {
				if gotMember != nil {
					t.Fatalf("member = %+v, want nil", gotMember)
				}
				return
			}
			if gotMember == nil || gotMember.ID != tc.wantMemberID {
				t.Fatalf("member = %+v, want id %q", gotMember, tc.wantMemberID)
			}
		})
	}

	if _, err := d.GetMember("ghost"); err != nil {
		t.Fatalf("verify unknown actor lookup: %v", err)
	}
}
