package main

// authz.go — the principal ladder + table-driven route RBAC, the Go twin of
// the retired Python service/authz.py (Plan B semantics; see root CLAUDE.md「核心不變量／授權單一化」).
//
// The four principal classes form a LINEAR capability ladder; a RouteSpec's
// Requires names the MINIMUM class; enforcement is rank(principal) >=
// rank(requires). "machine" is the FLOOR (any authenticated principal) and —
// exactly like the retired Python register_routes — attaches NO extra RBAC choke.
//
// ⚠️ The "so hot paths never pay a per-request roster lookup" that used to end
// that sentence is NO LONGER TRUE, and was corrected rather than left to
// mislead: T-9cf8 put a roster read in requireAuth itself (revocationRefusal
// below), which every GATED route now pays, floor rows included. The RBAC claim
// still holds — the floor attaches no CLASS choke — but the cost claim does not,
// and a reader who trusts it will conclude a deleted machine still reaches
// handlers untouched.
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

// isOutsourceMember reports whether a caller's roster row is an outsourced
// worker (kind=="outsource" — the ow- members). A nil row (owner scope, or an
// unknown sub) is never outsource. The T-23cf phase-2 正職授權矩陣 keys the
// "outsource may not create/reassign" hard rules on this — classifyMember alone
// cannot tell a 正職 from an 外包 (both rank principalAgent), so the durable
// Member.Kind is the discriminator.
func isOutsourceMember(m *Member) bool {
	return m != nil && m.Kind == KindOutsource
}

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

// ── machine-roster credential revocation (T-9cf8) ───────────────────────────
//
// WHY THIS EXISTS: "機器清單應該由我的機器名冊決定 — 一但刪除了 該機器的 token
// 應該要失效". Deleting a machine (DELETE /api/machines/{id}) or a confirmed
// teardown-here soft-deletes the warden's member row (RosterStatusRemoved) and
// nothing else. Verification is stateless HS256, and the machine floor rows
// attach NO choke at all (buildHandler skips requirePrincipalClass for
// principalMachine), so before this gate a deleted machine's token still
// answered 200 on /api/monitoring/telemetry, /api/chat, /api/mcp,
// /api/members, /api/machines, /api/global-context — measured, not assumed.
// That is what an "orphan machine row" actually is: not stale data, but a
// machine that is still talking.
//
// WHICH LAYER — per-request roster read, NOT sign-time binding. Sign-time
// binding cannot solve this at all: warden tokens are permanent and member
// tokens can last up to 400 days, and they are already in the field on hosts
// the server can no longer reach. There is nothing to un-sign. Revocation of
// an already-issued bearer token is
// inherently a read at USE time, so the only real question is where the read
// goes, and the answer is the ONE seam that already does credential
// revocation: requireAuth, next to the change-password owner-iat floor.
//
// THE COST, STATED HONESTLY: this buys immediacy (the next request after the
// delete is refused) at one extra indexed member read per gated request —
// wardens pay one lookup, agent-scope callers pay two. The route table's
// comment about the machine floor "never paying a per-request roster lookup"
// is now false for the auth gate specifically, and that is a deliberate
// trade: a deny gate that lags behind the roster is not a deny gate.
//
// FAIL-OPEN ON UNKNOWNS, DELIBERATELY. A lookup error never revokes. This is
// the opposite of resolvePrincipal's deny-by-default, and the asymmetry is the
// point: refusing to GRANT a capability on a failed read costs one 403,
// whereas REVOKING on a failed read turns a transient DB hiccup into a
// fleet-wide credential outage — the exact shape of the incident this repo
// already has on file (one commit tightened verification, four uplinks died at
// once, CI stayed green).
//
// SCOPE — kind=="warden" ONLY, and that restriction is load-bearing, not
// laziness. RosterStatusRemoved is ALSO how a released outsource worker
// (dal_tasks.go ReleaseWorkersForTask) and a dismissed member are recorded,
// and the close-out contract deliberately keeps a released worker's session
// alive so it can write learnings and call report_task_closeout. A gate keyed
// on "roster removed" alone would silently kill every outsource close-out in
// the fleet. Machines are the ticket; the rest is another ticket.

// revocationRefusal returns a non-empty refusal message when the verified
// claims belong to a machine that is no longer on the roster, "" otherwise.
// Two arms, because a machine's credential is used by two different processes:
//
//   - the machine ITSELF: a warden's token carries machine_id "" by explicit
//     design (api_machines.go onboard: "a warden carries NO self-binding"), so
//     the warden arm has to key on `sub`.
//   - what RUNS ON the machine: an agent/worker boot token carries
//     machine_id = the host it was booted on (mintAgentToken). Machine gone ⇒
//     the thing running on it must not keep writing.
//
// The second arm is guarded against the ONE false positive worth worrying
// about — "machine deleted but the agent has since been moved elsewhere": if
// the caller's own row names a DIFFERENT desired machine, the roster has
// already relocated it and only the stale token still points at the corpse, so
// we do not revoke on that basis. (A relocation re-mints on the next START
// dispatch, so the live token names the new host.)
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
		return "" // unknown ≠ revoked (see FAIL-OPEN above)
	}
	if isRemovedMachine(me) {
		return machineRevokedMsg(sub)
	}
	machineID, _ := claims["machine_id"].(string)
	if machineID == "" || machineID == sub {
		return ""
	}
	// The `!= ""` half is NOT redundant, and the asymmetry is deliberate:
	//   - pin set to ANOTHER machine → the roster has relocated this caller, so
	//     only a stale token still points at the corpse. Do not revoke.
	//   - pin EMPTY → that is AUTO placement (and `PATCH /api/members/{id}` can
	//     clear a pin back to it), which means the roster is not claiming a
	//     home at all. Then the token's own `machine_id` — stamped at spawn
	//     with the host actually picked at dispatch time (worker_spawn.go binds
	//     the warden the scheduler chose) — is the ONLY truthful statement
	//     about where this process runs, so it decides. Dropping the `!= ""`
	//     to "close the gap" would mean an unpinned worker genuinely running on
	//     the deleted machine could never be revoked, which is the failure this
	//     ticket exists to fix. The narrow cost is the reverse case: an
	//     unpinned caller holding an OLD token that names the deleted machine
	//     gets a 401 it could argue with. Cheaper than the alternative, and it
	//     resolves itself on the next spawn (a fresh token names the new host).
	if me != nil && me.DesiredMachineID != "" && me.DesiredMachineID != machineID {
		return "" // already relocated by the roster — only the stale token points here
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

// isRemovedMachine is the single predicate both arms share: a warden row that
// the roster has soft-deleted. A nil row (unknown id) is NOT removed — an id
// with no row was never on the roster to be taken off it, and treating absence
// as revocation would revoke every caller the DAL cannot resolve.
func isRemovedMachine(m *Member) bool {
	return m != nil && m.Kind == machineKind && m.RosterStatus == RosterStatusRemoved
}

// permanentCredentialRefusal confines exp-less JWTs to the credential class
// that is allowed to be permanent: an active warden roster row. verifyJWT
// deliberately handles the cryptographic shape only, so this stateful policy
// belongs at requireAuth with the other roster checks. In particular, a
// correctly signed no-exp token for an agent, worker, owner, unknown subject,
// or removed machine must never turn into an indefinite credential.
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

// machineRevokedMsg is the refusal text. It states the FACT (this machine is
// off the roster, so this credential is dead) and deliberately stops there: no
// "retry without", no "use the other endpoint", no hint that some subset of
// routes is still reachable — a refusal that explains how to get around itself
// is not a refusal. Recovery is a re-install, which is the owner's stated and
// accepted consequence, and the owner performs it from the console, not the
// revoked machine.
func machineRevokedMsg(machineID string) string {
	return "machine '" + machineID + "' has been removed from the roster; " +
		"its credentials are no longer valid"
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
