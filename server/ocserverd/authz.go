package main

// The machine rung is the FLOOR and attaches NO class choke (buildHandler skips
// requirePrincipalClass for principalMachine), but every gated route, floor rows
// included, still pays revocationRefusal's roster read in requireAuth.

import (
	"fmt"
	"net/http"
)

// principalClass is a STRUCT rather than a string so that the route table cannot
// name a class that does not exist: `Gated("superuser", …)` is a compile error.
type principalClass struct{ name string }

func (c principalClass) String() string { return c.name }

var (
	principalOwner      = principalClass{"owner"}
	principalAdminAgent = principalClass{"admin_agent"}
	principalAgent      = principalClass{"agent"}
	principalMachine    = principalClass{"machine"}
	requiresPublic      = principalClass{"public"}
)

var principalRank = map[principalClass]int{
	principalMachine:    0,
	principalAgent:      1,
	principalAdminAgent: 2,
	principalOwner:      3,
}

const (
	adminRoleKey = "assistant"
	machineKind  = "warden"
)

func isOutsourceMember(m *Member) bool {
	return m != nil && m.Kind == KindOutsource
}

func classifyMember(m *Member) principalClass {
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

func resolvePrincipal(claims map[string]any, lookup func(id string) (*Member, error)) principalClass {
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

// revocationRefusal: deleting a machine only soft-deletes the warden's member row,
// and verification is stateless HS256, so before this gate a deleted machine's token
// still answered 200 (measured). Already-issued tokens cannot be un-signed, so
// revocation is a per-request roster read at USE time — a deliberate cost: a deny
// gate that lags behind the roster is not a deny gate.
//
// FAIL-OPEN ON UNKNOWNS, DELIBERATELY — the opposite of resolvePrincipal's
// deny-by-default: revoking on a failed read turns a transient DB hiccup into a
// fleet-wide credential outage.
//
// A removed staff member or released worker is memberRemovedRefusal's: same
// fail-open rules, but that refusal carries the standing-refusal header.
//
// Two arms: a warden's token carries machine_id "" by design ("a warden carries NO
// self-binding"), so its arm keys on `sub`; an agent/worker boot token carries
// machine_id = the host it was booted on (mintAgentToken).
func revocationRefusal(claims map[string]any, lookup func(id string) (*Member, error)) string {
	if scope, _ := claims["scope"].(string); scope == "owner" {
		return "" // the owner has no roster row; the iat floor is its revocation seam
	}
	if lookup == nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	me, err := lookup(sub)
	if err != nil {
		return ""
	}
	if isRemovedMachine(me) {
		return machineRevokedMsg(sub)
	}
	machineID, _ := claims["machine_id"].(string)
	if machineID == "" || machineID == sub {
		return ""
	}
	// The `!= ""` half is NOT redundant: an EMPTY pin is AUTO placement, and then the
	// token's machine_id (the host actually picked at dispatch) is the only truthful
	// statement of where this process runs. Dropping it would leave an unpinned worker
	// running on the deleted machine unrevocable. A pin to ANOTHER machine means the
	// roster already relocated this caller; only a stale token points here.
	if me != nil && me.DesiredMachineID != "" && me.DesiredMachineID != machineID {
		return ""
	}
	host, err := lookup(machineID)
	if err != nil {
		return ""
	}
	if isRemovedMachine(host) {
		return machineRevokedMsg(machineID)
	}
	return ""
}

// A nil row is NOT removed: treating absence as revocation would revoke every
// caller the DAL cannot resolve.
func isRemovedMachine(m *Member) bool {
	return m != nil && m.Kind == machineKind && m.RosterStatus == RosterStatusRemoved
}

// memberRemovedRefusal covers dismissed staff and released outsource workers alike;
// fail-open on a lookup error or a nil row, as revocationRefusal is.
func memberRemovedRefusal(claims map[string]any, lookup func(id string) (*Member, error)) string {
	if scope, _ := claims["scope"].(string); scope == "owner" {
		return ""
	}
	if lookup == nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	m, err := lookup(sub)
	if err != nil || m == nil || m.Kind == machineKind || m.RosterStatus != RosterStatusRemoved {
		return ""
	}
	return "member '" + sub + "' has left the roster; its credentials are no longer valid"
}

// permanentCredentialRefusal confines exp-less JWTs to an active warden roster row;
// verifyJWT deliberately handles the cryptographic shape only.
func permanentCredentialRefusal(claims map[string]any, lookup func(id string) (*Member, error)) bool {
	if _, hasExpiry := claims["exp"]; hasExpiry {
		return false
	}
	if scope, _ := claims["scope"].(string); scope != "agent" {
		return true
	}
	if machineID, _ := claims["machine_id"].(string); machineID != "" {
		return true
	}
	if lookup == nil {
		return true
	}
	sub, _ := claims["sub"].(string)
	m, err := lookup(sub)
	return err != nil || m == nil || m.Kind != machineKind || m.RosterStatus != RosterStatusActive
}

// authRefusalHeader marks the 401s that are standing refusals: the agent iat floor
// and a member that has left the roster. cli/ocagent's listener retries a
// non-authoritative 401 forever, so a superseded generation would re-dial every
// ≤15s while the cockpit shows the SUCCESSOR, and a removed member's listener
// would never exit.
// It rides a RESPONSE HEADER, not the body: the body text is what agents read, and
// this must not become an instruction to a model.
// 🔴 IT IS ONLY EVER SET ON THESE REFUSALS: on any other 401 (expiry, unconfigured
// secret, bad signature) it turns a self-healing retry into a self-kill.
const (
	authRefusalHeader      = "X-OC-Auth-Refusal"
	refusalAgentSuperseded = "agent-superseded"
	refusalMemberRemoved   = "member-removed"
)

// agentIatFloorRefusal: the floor is raised by report_waking with the WAKING
// CALLER'S OWN iat, so with strictly-less-than the session that raised it is never
// refused (using now() would not have that property).
//
// 🔴 KIND == machineKind IS EXEMPT, a safety property: mintWardenToken issues
// scope="agent", and a warden refused HERE is refused on renew-credential too, so
// the recovery is a hand re-install. Warden not calling report_waking today is a
// fact about today's client, not a contract.
//
// ⚠️ NOT SOLVED: `iat` is whole seconds, so two generations starting in the same
// second are indistinguishable (owner 2026-08-28: 「先不管搶同一秒的問題好了」).
func agentIatFloorRefusal(claims map[string]any, lookup func(id string) (*Member, error)) bool {
	if scope, _ := claims["scope"].(string); scope != "agent" {
		return false
	}
	if lookup == nil {
		return false
	}
	sub, _ := claims["sub"].(string)
	m, err := lookup(sub)
	if err != nil || m == nil || m.Kind == machineKind || m.AgentIatFloor <= 0 {
		return false
	}
	iat, ok := claims["iat"].(float64)
	// No iat is refused, unlike the fail-open cases above: on a member that HAS a
	// floor, an un-datable credential is exactly what this gate exists to stop.
	if !ok {
		return true
	}
	return iat < m.AgentIatFloor
}

// The refusal deliberately states only the fact — no hint of a way around it.
// Recovery is an owner re-install from the console (owner-accepted consequence).
func machineRevokedMsg(machineID string) string {
	return "machine '" + machineID + "' has been removed from the roster; " +
		"its credentials are no longer valid"
}

func principalAtLeast(principal, minimum principalClass) bool {
	return principalRank[principal] >= principalRank[minimum]
}

func routeReachableBy(principal, requires principalClass) bool {
	if requires == requiresPublic {
		return true
	}
	minimum, declared := principalRank[requires]
	if !declared {
		return false
	}
	return principalRank[principal] >= minimum
}

func requirePrincipalClass(minimum principalClass, lookup func(id string) (*Member, error), next http.Handler) http.Handler {
	if _, ok := principalRank[minimum]; !ok {
		panic(fmt.Sprintf("unknown principal class %q", minimum))
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
