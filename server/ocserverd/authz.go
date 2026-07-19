package main

// authz.go — the principal ladder + table-driven route RBAC, the Go twin of
// the retired Python service/authz.py (Plan B semantics; see root CLAUDE.md §5/§14).
//
// The four principal classes form a LINEAR capability ladder; a RouteSpec's
// Requires names the MINIMUM class; enforcement is rank(principal) >=
// rank(requires). "machine" is the FLOOR (any authenticated principal) and —
// exactly like the retired Python register_routes — attaches NO extra choke, so
// hot paths never pay a per-request roster lookup.
//
// M3 sub-batch B: the member-row classification is LIVE — resolvePrincipal
// classifies owner scope from the token alone (the owner has no roster row)
// and every other scope from the caller's member row (kind=="warden" →
// machine, role_key=="assistant" → admin_agent; classify_member on the Python
// side). Deny-by-default: an unknown sub is a plain agent, never a capability.

import (
	"fmt"
	"net/http"
)

// The four principal classes (closed vocabulary) + the requires label a PUBLIC
// route declares (no principal at all).
const (
	principalOwner      = "owner"
	principalAdminAgent = "admin_agent"
	principalAgent      = "agent"
	principalMachine    = "machine"
	requiresPublic      = "public"
)

// principalRank is the linear capability ladder (machine < agent < admin_agent
// < owner) — the byte-for-byte twin of service.authz.PRINCIPAL_RANK.
var principalRank = map[string]int{
	principalMachine:    0,
	principalAgent:      1,
	principalAdminAgent: 2,
	principalOwner:      3,
}

// The role_key / kind literals the M3 member classification will key on —
// defined now so the constants live in ONE place from day 1 (mirrors
// ADMIN_ROLE_KEY / MACHINE_KIND in service/authz.py).
const (
	adminRoleKey = "assistant"
	machineKind  = "warden"
)

// classifyMember classifies an AGENT-scoped caller's member row into its
// principal class (service.authz.classify_member). Derived entirely from the
// durable fields: kind=="warden" wins first (a warden is a machine regardless
// of role_key), then role_key=="assistant" → admin_agent, else agent — a nil
// row (unknown sub) is a plain agent, never a capability.
func classifyMember(m *Member) string {
	if m == nil {
		return principalAgent
	}
	if m.Kind == machineKind {
		return principalMachine
	}
	if m.RoleKey == adminRoleKey {
		return principalAdminAgent
	}
	return principalAgent
}

// resolvePrincipal is THE single resolver (service.authz.resolve_principal):
// verified claims → principal class. Owner scope is decided from the token
// alone; any other scope resolves the caller's member row via lookup and
// classifies it. lookup errors resolve to a plain agent (deny-by-default: a
// capability is never granted on a failed read).
func resolvePrincipal(claims map[string]any, lookup func(id string) (*Member, error)) string {
	if scope, _ := claims["scope"].(string); scope == "owner" {
		return principalOwner
	}
	sub, _ := claims["sub"].(string)
	if lookup == nil {
		return principalAgent
	}
	m, err := lookup(sub)
	if err != nil {
		return principalAgent
	}
	return classifyMember(m)
}

// principalAtLeast reports whether principal ranks at or above minimum.
func principalAtLeast(principal, minimum string) bool {
	return principalRank[principal] >= principalRank[minimum]
}

// requirePrincipalClass wraps a handler with the ONE RBAC enforcement choke the
// route table attaches (service.authz.require_principal_class): the request's
// principal (resolved from the claims the auth middleware stashed + the roster
// lookup) must rank at or above minimum, or the request is a flat 403. A
// missing/invalid token never reaches here (the auth middleware already
// answered 401).
func requirePrincipalClass(minimum string, lookup func(id string) (*Member, error), next http.Handler) http.Handler {
	if _, ok := principalRank[minimum]; !ok {
		panic(fmt.Sprintf("unknown principal class %q", minimum)) // programmer error, caught by the boot assertion first
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r.Context())
		if claims == nil || !principalAtLeast(resolvePrincipal(claims, lookup), minimum) {
			writeError(w, http.StatusForbidden, "principal not permitted")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// assertAllRoutesDeclareRequires is the fail-closed boot assertion
// (service.authz.assert_all_routes_declare_requires, app.py spirit): EVERY
// route row must declare a KNOWN requires class, consistent with its auth
// label (auth=="public" ⟺ requires=="public"). The server refuses to start
// otherwise — an undeclared/contradictory row is a misconfiguration, never a
// served route.
func assertAllRoutesDeclareRequires(specs []RouteSpec) error {
	for _, spec := range specs {
		where := spec.Method + " " + spec.Path
		_, known := principalRank[spec.Requires]
		if !known && spec.Requires != requiresPublic {
			return fmt.Errorf(
				"route %s declares unknown requires=%q (expected one of the principal ladder or %q)",
				where, spec.Requires, requiresPublic)
		}
		if (spec.Auth == authPublic) != (spec.Requires == requiresPublic) {
			return fmt.Errorf(
				"route %s: auth=%q and requires=%q disagree (public ⟺ requires='public')",
				where, spec.Auth, spec.Requires)
		}
	}
	return nil
}

// assertAllRoutesLabelled is the deny-by-default auth-label boot assertion
// (plumbing.auth.assert_all_routes_labelled): every route must carry a KNOWN
// auth label; anything else refuses to start.
func assertAllRoutesLabelled(specs []RouteSpec) error {
	for _, spec := range specs {
		if spec.Auth != authPublic && spec.Auth != authGated {
			return fmt.Errorf(
				"route %s %s carries invalid auth label %q; must be %q or %q (deny-by-default, fail closed)",
				spec.Method, spec.Path, spec.Auth, authPublic, authGated)
		}
	}
	return nil
}
