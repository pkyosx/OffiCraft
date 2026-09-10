package main

// authz.go — the principal ladder + table-driven route RBAC, the Go twin of
// the retired Python service/authz.py (Plan B semantics; see root CLAUDE.md §5/§14).
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

// principalClass is the caller's place on the capability ladder. It is a STRUCT
// rather than a string SO THAT THE ROUTE TABLE CANNOT NAME A CLASS THAT DOES NOT
// EXIST: Gated(principalClass, routeDef) takes one of the four values below, and
// `Gated("superuser", …)` is a compile error instead of a row nothing satisfies.
// That is what replaced the deleted assertAllRoutesDeclareRequires — the check
// moved from boot to the compiler, so this type is the whole of it.
type principalClass struct{ name string }

// String is the wire/name face (log lines, error text). The name is the same
// vocabulary service.authz used.
func (c principalClass) String() string { return c.name }

// The four principal classes (closed vocabulary) + the label a PUBLIC route
// declares (no principal at all).
var (
	principalOwner      = principalClass{"owner"}
	principalAdminAgent = principalClass{"admin_agent"}
	principalAgent      = principalClass{"agent"}
	principalMachine    = principalClass{"machine"}
	requiresPublic      = principalClass{"public"}
)

// principalRank is the linear capability ladder (machine < agent < admin_agent
// < owner) — the byte-for-byte twin of service.authz.PRINCIPAL_RANK.
var principalRank = map[principalClass]int{
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

// resolvePrincipal is THE single resolver (service.authz.resolve_principal):
// verified claims → principal class. Owner scope is decided from the token
// alone; any other scope resolves the caller's member row via lookup and
// classifies it. lookup errors resolve to a plain agent (deny-by-default: a
// capability is never granted on a failed read).
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
// binding cannot solve this at all: warden tokens live up to 400 days too (and
// those minted before T-fc53 第二段 never expire at all) and member
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

// authRefusalHeader / refusalAgentSuperseded name a 401 that is a STANDING,
// authoritative refusal rather than transient bad luck, on the ONE refusal where
// the difference decides whether a live process should keep retrying or stop.
//
// 🔴 WHY A MARKER AT ALL. The agent iat floor refuses with the same
// 401 "invalid token" as a server whose secret is not loaded yet, a token that
// simply expired, or a restart in progress. cli/ocagent's listener treats a
// non-authoritative failure as "reconnect with backoff, forever" — correct for
// every one of those, and a silent orphan for this one: the superseded
// generation would re-dial every ≤15s for the rest of the machine's uptime,
// holding a tmux + model session that nothing on the cockpit can even see,
// because the member's presence now belongs to the SUCCESSOR. A client cannot
// tell the two apart from the status line alone — 401 is 401 — so the server,
// which is the only side that knows which refusal it just made, says so.
//
// It rides a RESPONSE HEADER and not the body: the body text is what agents
// read (two seeds quote "every later MCP call 401s with nothing saying why"),
// and this must not become an instruction to a model. It is transport-level
// advice to the process holding the socket.
//
// 🔴 IT IS ONLY EVER SET ON THIS ONE REFUSAL. Setting it on any other 401 —
// expiry, an unconfigured secret, a bad signature — turns a self-healing retry
// into a self-kill, which is strictly worse than the hammering it fixes. That
// direction is pinned from the client side by
// TestListener_APlain401NeverTripsFailClosed (cli/ocagent).
const (
	authRefusalHeader      = "X-OC-Auth-Refusal"
	refusalAgentSuperseded = "agent-superseded"
)

// agentIatFloorRefusal is the AGENT-side credential floor (T-14 項目 4B): an
// agent-scope token whose `iat` is STRICTLY LESS THAN its own member's
// agent_iat_floor was minted for a generation that member has already replaced,
// and is refused.
//
// 🔴 IT IS NOT requireAuth's ownerIatFloor WITH A SECOND CALLER, and could not
// be. That one is a single global number, because the owner has no roster row
// and the only thing it can be keyed on is the last password change. An agent's
// floor is PER MEMBER — each member's own last 開工 — so it is read off the
// member row, through the SAME lookup the two refusals above already use rather
// than a second roster read. Same family of seam, different shape.
//
// The floor is raised by report_waking, which stores the WAKING CALLER'S OWN
// iat. With a strictly-less-than comparison that means the session that raised
// the floor is never refused by it, whatever the gap between its mint and its
// boot — the property using now() would not have.
//
// 🔴 KIND == machineKind IS EXEMPT, and that is a safety property rather than
// an optimisation. mintWardenToken issues scope="agent" credentials for a machine
// member, so scope alone cannot tell a warden from an agent. A warden does not
// call report_waking today — but that is a fact about today's client, not a
// contract, and one added line there would raise a floor above a credential the
// machine cannot replace: a warden refused HERE is refused on
// /api/machines/renew-credential too, so the one path out is shut at the same
// instant, and the recovery is a hand re-install.
//
// ⚠️ THE STRENGTH OF THAT SENTENCE MOVED IN T-fc53 第二段, the exemption did not.
// It used to read "a credential that can never expire out of the way … would go
// dark PERMANENTLY", which was true while warden credentials carried no exp.
// They carry one again (30 days by default), so a refused machine now un-sticks
// itself when that credential expires and the host is reinstalled — the harm is
// bounded rather than infinite. It is still a machine off the fleet for as long
// as its credential lives, which is why nothing here changes. Pinned by
// TestAgentIatFloor_WardenPermanentTokenIsExempt.
//
// Everything else fails OPEN by construction: a non-agent scope, a missing
// lookup (the token-only plumbing face), an unresolvable sub, or a row with no
// floor (0 — every pre-migration row) is not refused here. This gate only ever
// refuses a token that is demonstrably older than a wake its own member
// reported; it is not a second chance to deny an unknown caller, which is what
// the refusals above are for.
//
// ⚠️ NOT SOLVED: `iat` is whole seconds, so two generations of one member that
// start inside the SAME second are indistinguishable to this comparison (owner
// 2026-08-28: 「先不管搶同一秒的問題好了」).
func agentIatFloorRefusal(claims map[string]any, lookup func(id string) (*Member, error)) bool {
	// The JWT scope vocabulary, not the ladder — same literal form as the
	// owner check in resolvePrincipal above.
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
	iat, ok := claims["iat"].(float64) // encoding/json numbers land as float64
	if !ok {
		// A token with no iat cannot be placed on either side of the floor. It
		// is refused rather than admitted: on a member that HAS a floor, an
		// un-datable credential is exactly the shape this gate exists to stop.
		return true
	}
	return iat < m.AgentIatFloor
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
func principalAtLeast(principal, minimum principalClass) bool {
	return principalRank[principal] >= principalRank[minimum]
}

// requirePrincipalClass wraps a handler with the ONE RBAC enforcement choke the
// route table attaches (service.authz.require_principal_class): the request's
// principal (resolved from the claims the auth middleware stashed + the roster
// lookup) must rank at or above minimum, or the request is a flat 403. A
// missing/invalid token never reaches here (the auth middleware already
// answered 401).
func requirePrincipalClass(minimum principalClass, lookup func(id string) (*Member, error), next http.Handler) http.Handler {
	if _, ok := principalRank[minimum]; !ok {
		panic(fmt.Sprintf("unknown principal class %q", minimum)) // unreachable: the type admits only the four values
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
