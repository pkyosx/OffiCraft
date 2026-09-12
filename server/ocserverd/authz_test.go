package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyMember(t *testing.T) {
	tests := []struct {
		name   string
		member *Member
		want   principalClass
	}{
		{name: "unknown row is a plain agent", member: nil, want: principalAgent},
		{name: "warden wins over admin role", member: &Member{Kind: KindWarden, RoleKey: adminRoleKey}, want: principalMachine},
		{name: "assistant staff is admin agent", member: &Member{Kind: KindStaff, RoleKey: adminRoleKey}, want: principalAdminAgent},
		{name: "assistant outsource is still admin agent", member: &Member{Kind: KindOutsource, RoleKey: adminRoleKey}, want: principalAdminAgent},
		{name: "ordinary staff is an agent", member: &Member{Kind: KindStaff, RoleKey: "engineer"}, want: principalAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyMember(tt.member); got != tt.want {
				t.Fatalf("classifyMember(%#v) = %v, want %v", tt.member, got, tt.want)
			}
		})
	}
}

func TestIsOutsourceMember(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member *Member
		want   bool
	}{
		{name: "nil member", member: nil, want: false},
		{name: "outsource row", member: &Member{Kind: KindOutsource}, want: true},
		{name: "staff row", member: &Member{Kind: KindStaff}, want: false},
		{name: "warden row", member: &Member{Kind: KindWarden}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOutsourceMember(tc.member); got != tc.want {
				t.Fatalf("isOutsourceMember(%#v) = %v, want %v", tc.member, got, tc.want)
			}
		})
	}
}

func TestResolvePrincipal(t *testing.T) {
	t.Run("owner scope resolves without a roster lookup", func(t *testing.T) {
		called := false
		lookup := func(string) (*Member, error) {
			called = true
			return &Member{Kind: KindWarden}, nil
		}
		if got := resolvePrincipal(map[string]any{"scope": "owner", "sub": "owner"}, lookup); got != principalOwner {
			t.Fatalf("resolvePrincipal(owner) = %v, want owner", got)
		}
		if called {
			t.Fatal("owner scope performed a roster lookup")
		}
	})

	tests := []struct {
		name   string
		claims map[string]any
		lookup func(string) (*Member, error)
		want   principalClass
	}{
		{name: "assistant member", claims: map[string]any{"scope": "agent", "sub": "mira"}, lookup: func(id string) (*Member, error) {
			if id != "mira" {
				return nil, errors.New("unexpected subject")
			}
			return &Member{Kind: KindStaff, RoleKey: adminRoleKey}, nil
		}, want: principalAdminAgent},
		{name: "ordinary member", claims: map[string]any{"scope": "agent", "sub": "kip"}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindStaff}, nil
		}, want: principalAgent},
		{name: "warden member", claims: map[string]any{"scope": "agent", "sub": "warden-1"}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindWarden}, nil
		}, want: principalMachine},
		{name: "lookup error denies capability", claims: map[string]any{"scope": "agent", "sub": "missing"}, lookup: func(string) (*Member, error) {
			return nil, errors.New("database unavailable")
		}, want: principalAgent},
		{name: "nil lookup is plain agent", claims: map[string]any{"scope": "agent", "sub": "mira"}, lookup: nil, want: principalAgent},
		{name: "unknown scope still uses member classification", claims: map[string]any{"scope": "other", "sub": "mira"}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindStaff, RoleKey: adminRoleKey}, nil
		}, want: principalAdminAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePrincipal(tt.claims, tt.lookup); got != tt.want {
				t.Fatalf("resolvePrincipal(%#v) = %v, want %v", tt.claims, got, tt.want)
			}
		})
	}
}

func TestRevocationRefusal(t *testing.T) {
	removedMachine := func(id string) (*Member, error) {
		if id == "mira" || id == "machine-1" {
			return &Member{ID: id, Kind: KindWarden, RosterStatus: RosterStatusRemoved}, nil
		}
		return &Member{ID: id, Kind: KindStaff, RosterStatus: RosterStatusActive}, nil
	}
	activeStaff := func(string) (*Member, error) {
		return &Member{Kind: KindStaff, RosterStatus: RosterStatusActive}, nil
	}
	lookupError := func(string) (*Member, error) {
		return nil, errors.New("temporary read failure")
	}

	tests := []struct {
		name   string
		claims map[string]any
		lookup func(string) (*Member, error)
		want   string
	}{
		{name: "owner is not roster revoked", claims: map[string]any{"scope": "owner", "sub": "owner"}, lookup: removedMachine},
		{name: "removed machine caller is refused", claims: map[string]any{"scope": "agent", "sub": "mira"}, lookup: removedMachine, want: machineRevokedMsg("mira")},
		{name: "active machine caller is allowed", claims: map[string]any{"scope": "agent", "sub": "machine-1"}, lookup: activeStaff},
		{name: "removed staff caller is not a machine refusal", claims: map[string]any{"scope": "agent", "sub": "kip"}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindStaff, RosterStatus: RosterStatusRemoved}, nil
		}},
		{name: "removed host refuses an agent token", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "machine-1"}, lookup: removedMachine, want: machineRevokedMsg("machine-1")},
		{name: "relocated member ignores stale host claim", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "machine-1"}, lookup: func(id string) (*Member, error) {
			if id == "kip" {
				return &Member{Kind: KindStaff, DesiredMachineID: "machine-2"}, nil
			}
			return &Member{Kind: KindWarden, RosterStatus: RosterStatusRemoved}, nil
		}},
		{name: "unknown host is fail-open", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "unknown"}, lookup: activeStaff},
		{name: "lookup failure is fail-open", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "machine-1"}, lookup: lookupError},
		{name: "nil lookup is fail-open", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "machine-1"}, lookup: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := revocationRefusal(tt.claims, tt.lookup); got != tt.want {
				t.Fatalf("revocationRefusal(%#v) = %q, want %q", tt.claims, got, tt.want)
			}
		})
	}
}

func TestPermanentCredentialRefusal(t *testing.T) {
	activeWarden := func(string) (*Member, error) {
		return &Member{Kind: KindWarden, RosterStatus: RosterStatusActive}, nil
	}
	activeStaff := func(string) (*Member, error) {
		return &Member{Kind: KindStaff, RosterStatus: RosterStatusActive}, nil
	}
	tests := []struct {
		name   string
		claims map[string]any
		lookup func(string) (*Member, error)
		want   bool
	}{
		{name: "active warden may be permanent", claims: map[string]any{"scope": "agent", "sub": "machine-1"}, lookup: activeWarden, want: false},
		{name: "active staff may not be permanent", claims: map[string]any{"scope": "agent", "sub": "kip"}, lookup: activeStaff, want: true},
		{name: "agent token bound to a machine may not be permanent", claims: map[string]any{"scope": "agent", "sub": "kip", "machine_id": "machine-1"}, lookup: activeWarden, want: true},
		{name: "owner may not be permanent", claims: map[string]any{"scope": "owner", "sub": "owner"}, lookup: activeWarden, want: true},
		{name: "other scope may not be permanent", claims: map[string]any{"scope": "machine", "sub": "machine-1"}, lookup: activeWarden, want: true},
		{name: "missing lookup may not be permanent", claims: map[string]any{"scope": "agent", "sub": "machine-1"}, lookup: nil, want: true},
		{name: "removed warden may not be permanent", claims: map[string]any{"scope": "agent", "sub": "machine-1"}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindWarden, RosterStatus: RosterStatusRemoved}, nil
		}, want: true},
		{name: "an expiry claim makes the question irrelevant", claims: map[string]any{"scope": "owner", "exp": nil}, lookup: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permanentCredentialRefusal(tt.claims, tt.lookup); got != tt.want {
				t.Fatalf("permanentCredentialRefusal(%#v) = %v, want %v", tt.claims, got, tt.want)
			}
		})
	}
}

func TestAgentIatFloorRefusal(t *testing.T) {
	lookup := func(string) (*Member, error) {
		return &Member{Kind: KindStaff, AgentIatFloor: 1000}, nil
	}
	tests := []struct {
		name   string
		claims map[string]any
		lookup func(string) (*Member, error)
		want   bool
	}{
		{name: "older iat is refused", claims: map[string]any{"scope": "agent", "iat": 999.0}, lookup: lookup, want: true},
		{name: "same iat remains valid", claims: map[string]any{"scope": "agent", "iat": 1000.0}, lookup: lookup, want: false},
		{name: "newer iat remains valid", claims: map[string]any{"scope": "agent", "iat": 1001.0}, lookup: lookup, want: false},
		{name: "missing iat is refused when a floor exists", claims: map[string]any{"scope": "agent"}, lookup: lookup, want: true},
		{name: "warden is exempt", claims: map[string]any{"scope": "agent", "iat": 1.0}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindWarden, AgentIatFloor: 1000}, nil
		}, want: false},
		{name: "zero floor is not a refusal", claims: map[string]any{"scope": "agent", "iat": 1.0}, lookup: func(string) (*Member, error) {
			return &Member{Kind: KindStaff}, nil
		}, want: false},
		{name: "non-agent scope is ignored", claims: map[string]any{"scope": "owner", "iat": 1.0}, lookup: lookup, want: false},
		{name: "lookup error is fail-open", claims: map[string]any{"scope": "agent", "iat": 1.0}, lookup: func(string) (*Member, error) {
			return nil, errors.New("read failed")
		}, want: false},
		{name: "non-float iat is refused", claims: map[string]any{"scope": "agent", "iat": int64(1)}, lookup: lookup, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentIatFloorRefusal(tt.claims, tt.lookup); got != tt.want {
				t.Fatalf("agentIatFloorRefusal(%#v) = %v, want %v", tt.claims, got, tt.want)
			}
		})
	}
}

func TestMachineRevokedMsg(t *testing.T) {
	if got, want := machineRevokedMsg("machine-1"), "machine 'machine-1' has been removed from the roster; its credentials are no longer valid"; got != want {
		t.Fatalf("machineRevokedMsg() = %q, want %q", got, want)
	}
}

func TestRequirePrincipalClass(t *testing.T) {
	lookup := func(id string) (*Member, error) {
		switch id {
		case "mira":
			return &Member{Kind: KindStaff, RoleKey: adminRoleKey}, nil
		case "kip":
			return &Member{Kind: KindStaff, RoleKey: "engineer"}, nil
		case "machine-1":
			return &Member{Kind: KindWarden}, nil
		default:
			return nil, errors.New("not found")
		}
	}

	tests := []struct {
		name    string
		minimum principalClass
		claims  map[string]any
		status  int
		called  bool
	}{
		{name: "owner reaches admin route", minimum: principalAdminAgent, claims: map[string]any{"scope": "owner"}, status: http.StatusNoContent, called: true},
		{name: "admin agent reaches admin route", minimum: principalAdminAgent, claims: map[string]any{"scope": "agent", "sub": "mira"}, status: http.StatusNoContent, called: true},
		{name: "plain agent is denied by admin route", minimum: principalAdminAgent, claims: map[string]any{"scope": "agent", "sub": "kip"}, status: http.StatusForbidden, called: false},
		{name: "machine reaches machine floor", minimum: principalMachine, claims: map[string]any{"scope": "agent", "sub": "machine-1"}, status: http.StatusNoContent, called: true},
		{name: "missing claims are denied", minimum: principalMachine, claims: nil, status: http.StatusForbidden, called: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			wrapped := requirePrincipalClass(tt.minimum, lookup, next)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, tt.claims))
			}
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if called != tt.called {
				t.Fatalf("next called = %v, want %v", called, tt.called)
			}
			if !tt.called && !strings.Contains(rec.Body.String(), `"message":"principal not permitted"`) {
				t.Fatalf("denial body = %s, want principal-not-permitted message", rec.Body.String())
			}
		})
	}

	t.Run("unknown minimum panics before serving", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("requirePrincipalClass did not panic for an unknown class")
			}
		}()
		requirePrincipalClass(principalClass{name: "unknown"}, lookup, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	})
}

func TestPrincipalAtLeast(t *testing.T) {
	for _, tc := range []struct {
		name               string
		principal, minimum principalClass
		want               bool
	}{
		{name: "machine reaches machine floor", principal: principalMachine, minimum: principalMachine, want: true},
		{name: "agent does not reach admin agent", principal: principalAgent, minimum: principalAdminAgent, want: false},
		{name: "admin agent reaches agent floor", principal: principalAdminAgent, minimum: principalAgent, want: true},
		{name: "owner reaches every capability floor", principal: principalOwner, minimum: principalMachine, want: true},
		{name: "machine does not reach agent", principal: principalMachine, minimum: principalAgent, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := principalAtLeast(tc.principal, tc.minimum); got != tc.want {
				t.Fatalf("principalAtLeast(%v, %v) = %v, want %v", tc.principal, tc.minimum, got, tc.want)
			}
		})
	}
}

func TestRouteReachableBy(t *testing.T) {
	for _, tc := range []struct {
		name               string
		principal, minimum principalClass
		want               bool
	}{
		{name: "agent does not reach an admin agent row", principal: principalAgent, minimum: principalAdminAgent, want: false},
		{name: "admin agent reaches an admin agent row", principal: principalAdminAgent, minimum: principalAdminAgent, want: true},
		{name: "machine does not reach an agent row", principal: principalMachine, minimum: principalAgent, want: false},
		{name: "owner reaches an admin agent row", principal: principalOwner, minimum: principalAdminAgent, want: true},
		{name: "the capability floor reaches a public row", principal: principalMachine, minimum: requiresPublic, want: true},
		{name: "a plain agent reaches a public row", principal: principalAgent, minimum: requiresPublic, want: true},
		{name: "an undeclared floor is refused rather than treated as the ladder's bottom",
			principal: principalOwner, minimum: principalClass{"superuser"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeReachableBy(tc.principal, tc.minimum); got != tc.want {
				t.Fatalf("routeReachableBy(%v, %v) = %v, want %v", tc.principal, tc.minimum, got, tc.want)
			}
		})
	}

	t.Run("public stays off the rank ladder so a public row cannot be written as a class choke", func(t *testing.T) {
		// requirePrincipalClass panics on a class principalRank does not hold,
		// and that panic is the whole reason Gated(requiresPublic, …) cannot
		// ship a chokeless gated row. Teaching the map about "public" to make
		// routeReachableBy simpler would silently disarm it.
		if _, found := principalRank[requiresPublic]; found {
			t.Fatal("requiresPublic was added to principalRank — requirePrincipalClass would stop refusing it")
		}
	})
}
