package main

// routes.go — the declarative route table carrier, the Go twin of
// the retired Python service/routes.py: routing is a TABLE, never an if-chain. Every
// served route is one RouteSpec row (method + path + handler + auth label +
// the MINIMUM principal class it admits + the MCP-surface flags); the mux is
// built FROM the table and the boot assertions run OVER the table.
//
// M3 REST sub-batch A: the table now carries the FULL wire surface (every
// operation of spec/openapi.json, auth/requires/mcp flags mirrored row-for-row
// from service/routes.py ROUTE_SPECS). Handlers come THROUGH the generated
// ServerInterfaceWrapper (ocapi_gen.go) over apiServer (api_stub.go): the four
// build-identity probes are real, everything else answers an honest 501 until
// sub-batch B fills the method bodies — adding behaviour never touches this
// table again.

import "net/http"

// Auth classes — the closed vocabulary every route must use (plumbing.auth
// PUBLIC/GATED on the Python side).
const (
	authPublic = "public"
	authGated  = "gated"
)

// RouteSpec is one row of the route table (service.routes.RouteSpec).
type RouteSpec struct {
	// Method is the HTTP method (GET/POST/...).
	Method string
	// Path is the URL path (e.g. "/api/version"); {param} names must match the
	// spec (the generated wrapper reads them via r.PathValue).
	Path string
	// Handler is the endpoint (thin; delegates to plumbing/domain).
	Handler http.HandlerFunc
	// Auth is "public" | "gated" — deny-by-default; must be explicit.
	Auth string
	// Requires is the MINIMUM principal class this route admits (the authz
	// ladder machine < agent < admin_agent < owner; "public" on public routes).
	// The boot assertion refuses to start when a row is undeclared or
	// contradicts its auth label.
	Requires string
	// Summary is the human/tool description (also the future MCP tool description).
	Summary string
	// MCPExclude keeps this route OUT of the MCP tool surface (infra endpoints).
	MCPExclude bool
	// MCPTool is the explicit MCP tool name override (paths carrying a {param}).
	MCPTool string
	// LoreGated marks a row as belonging to the T-33 LORE feature, whose
	// station-wide switch (settings `lore.enabled`, default OFF) decides whether
	// the row answers at all.
	//
	// 🔴 IT IS A FLAG ON THE TABLE, NOT AN `if` IN TWELVE HANDLERS. The whole
	// point of this file is that routing is a TABLE; a switch re-tested inside
	// each lore handler would be twelve chances for one of them to be added
	// later without it, and the twelfth would be a write that lands on a station
	// whose owner believes the feature is off. specsFor (server.go) wraps every
	// row carrying this flag with ONE gate, so a new lore row inherits it by
	// declaring the flag and cannot inherit it by accident anywhere else.
	LoreGated bool
	// ShareSig admits a ?sig= share credential (sharesig.go) as a third auth
	// path on THIS row only (precedence: Authorization header → ?token= →
	// ?sig=), and IS the verifier: it reads its own subject out of the request
	// and checks it against its own domain-separated key. nil = this row never
	// consults sigs, which is every row but the two below.
	ShareSig shareSigVerifier
}

// shareSigVerifier answers "does this sig authorize THIS request", for one row.
// A row's subject is whatever decides what the response says: the attachment
// blob GET's is the path's attachment_id, GET /api/diff's is both addresses and
// both column labels — the whole of what one answer depends on, so a recipient
// cannot swap an address or relabel a column and still hold a minted signature.
//
// It takes the whole signing-key RING, not one key: a sig names no key, so
// every verifier accepts one made under ANY key still in the ring and dies with
// the key that made it (sharesig.go).
type shareSigVerifier func(keys *keyring, r *http.Request, sig string) bool

// verifyAttachmentShareSig is the attachment blob GET's subject: exactly the
// one blob id in the path.
func verifyAttachmentShareSig(keys *keyring, r *http.Request, sig string) bool {
	return verifyShareSigAnyKey(keys, r.PathValue("attachment_id"), sig)
}

// verifyDiffShareSig is GET /api/diff's subject: both addresses and both
// labels, read RAW (never trimmed — a padded address is a different address).
func verifyDiffShareSig(keys *keyring, r *http.Request, sig string) bool {
	q := r.URL.Query()
	return verifyDiffSigAnyKey(keys,
		q.Get(diffParamBefore), q.Get(diffParamAfter),
		q.Get(diffParamLabelBefor), q.Get(diffParamLabelAfter), sig)
}

// routeSpecs builds the route table over the generated wrapper (which binds
// path/query params, then dispatches into apiServer). Row order, auth labels,
// requires classes, and MCP flags mirror service/routes.py ROUTE_SPECS.
func routeSpecs(w *ServerInterfaceWrapper) []RouteSpec {
	return []RouteSpec{
		// ── Build identity + deploy probes ──────────────────────────────────
		{
			Method:     "GET",
			Path:       "/api/health",
			Handler:    w.HandleHealthApiHealthGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    `Liveness probe — 200 {"status":"ok"}.`,
			MCPExclude: true, // an ops liveness probe, not an agent tool
		},
		{
			// T-77b4: on the MCP surface as `get_version` (owner 2026-08-07,
			// rc-2089ff8e34bf option ①). It used to carry MCPExclude with the
			// reason "a build-identity probe, not an agent tool" — and that
			// reason was answering a different question than the one agents
			// actually have. The dev SOP settles "has my change shipped?" by an
			// ancestry test against the station's running git_sha, so EVERY
			// member needs to read this, while the boot context forbids
			// hand-rolling curl at the server's own API. The row is
			// requiresPublic, so being on the surface admits any authenticated
			// caller — that IS the capability being granted. Note the direction
			// of that grant, because "same gate as the REST route" understates
			// it (T-6535 independent review): this row is authPublic, so the
			// REST face applies NEITHER gate, while /api/mcp itself is
			// authGated — so the tool face is a PROPER SUBSET of who could
			// already read this, and listing it CONVERGES reach rather than
			// widening it. Deliberately NOT
			// merged with check_release (requires=admin_agent): that one asks
			// GitHub whether a NEWER release exists, a different question with a
			// different data source; one field answering both would be
			// ambiguous.
			Method:   "GET",
			Path:     "/api/version",
			Handler:  w.HandleVersionApiVersionGet,
			Auth:     authPublic,
			Requires: requiresPublic,
			Summary:  "Read the build identity this station is RUNNING: version, git sha, git time and the MCP catalog hash, plus the cached update status and `update_checked_ok_at`, the time that update check last SUCCEEDED (absent = it never has, so `update_available: false` is not evidence of being up to date). Settle whether something is deployed by git sha ancestry, never by the version string.",
		},
		{
			Method:     "GET",
			Path:       "/health",
			Handler:    w.HandleHealthHealthGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    `Deploy probe: liveness — 200 {"status":"ok"}.`,
			MCPExclude: true,
		},
		{
			Method:     "GET",
			Path:       "/version",
			Handler:    w.HandleProbeVersionVersionGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Deploy probe: version + git sha (autodeploy sha compare).",
			MCPExclude: true,
		},
		// ── Credential seams ─────────────────────────────────────────────────
		{
			Method:     "POST",
			Path:       "/api/login",
			Handler:    w.HandleLoginApiLoginPost,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Owner login: exchange the password for an owner-scoped JWT.",
			MCPExclude: true,
		},
		// ── T-6020 governance ruling (owner, 2026-07-26) ─────────────────────
		// The owner opened 19 previously owner-only operational routes to the
		// admin_agent class (see each row's T-6020 note) so an admin 助理 can
		// actually run the office. FIVE rows were deliberately NOT opened and
		// STAY principalOwner + MCPExclude. This is a decision, not an
		// oversight — do not "finish the job" by lowering them:
		//
		//   POST /api/mint                  — minting an identity IS
		//       self-escalation: an admin_agent that can mint an owner-scoped
		//       (or any) token can hand itself every remaining gate, which
		//       would make the whole ladder decorative.
		//   POST /api/auth/change-password  — the owner's personal account
		//       credential; changing it locks the human out of their own
		//       cockpit.
		//   GET  /api/push/public-key       — Web Push is the owner's own
		//   POST /api/push/subscription       BROWSER, not an office capability;
		//   DELETE /api/push/subscription     an agent has no browser to
		//       subscribe and nothing legitimate to do with the owner's.
		//
		// Each of the five carries its own one-line reminder below.
		{
			Method:   "POST",
			Path:     "/api/mint",
			Handler:  w.HandleMintApiMintPost,
			Auth:     authGated,
			Requires: principalOwner,
			Summary:  "Owner-gated mint of a long-lived agent JWT for a member (TTL capped).",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — issuing an identity equals self-escalation.
			MCPExclude: true,
		},
		{
			Method:     "GET",
			Path:       "/api/auth/status",
			Handler:    w.HandleAuthStatusApiAuthStatusGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "First-run probe: has the owner password been set?",
			MCPExclude: true, // the login wall's branch bit, not an agent tool
		},
		{
			Method:     "POST",
			Path:       "/api/auth/set-password",
			Handler:    w.HandleSetPasswordApiAuthSetPasswordPost,
			Auth:       authPublic, // the one-shot claim token IS the gate (lifecycle.md §1.3)
			Requires:   requiresPublic,
			Summary:    "First-run: set the owner password (one-shot claim token gate).",
			MCPExclude: true, // a credential seam, never an agent tool
		},
		{
			Method:   "POST",
			Path:     "/api/auth/change-password",
			Handler:  w.HandleChangePasswordApiAuthChangePasswordPost,
			Auth:     authGated,
			Requires: principalOwner,
			Summary:  "Change the owner password (verifies the current one).",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — the owner's PERSONAL account credential.
			MCPExclude: true, // the owner's credential, never an agent tool
		},
		// ── Owner second factor (TOTP) ───────────────────────────────────────
		// All three are principalOwner + MCPExclude, for the same reason the two
		// password rows above are: these endpoints decide how the OWNER
		// authenticates. An admin_agent that could reach them could weaken the
		// credential that governs it, and arming or disarming the owner's factor
		// is never something an agent does on the owner's behalf.
		{
			Method:     "GET",
			Path:       "/api/auth/mfa",
			Handler:    w.HandleMfaStateApiAuthMfaGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Read the owner's second-factor state (offered + enrolled).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/mfa/offer",
			Handler:    w.HandleMfaOfferApiAuthMfaOfferPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Turn the second-factor feature on or off for this server.",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/mfa/enroll",
			Handler:    w.HandleMfaEnrollApiAuthMfaEnrollPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Begin TOTP enrolment: mint a pending secret + otpauth URI.",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/mfa/activate",
			Handler:    w.HandleMfaActivateApiAuthMfaActivatePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Arm the second factor by proving a code from the pending secret.",
			MCPExclude: true,
		},
		// ── Signing-key ring (T-62) ──────────────────────────────────────────
		// principalOwner + MCPExclude for the same reason the password and
		// second-factor rows above are: these routes govern the key that
		// authenticates EVERY caller, the calling agent included. An
		// admin_agent that could reach them could rotate the key that governs
		// it, or remove the key its own credential is signed under.
		{
			Method:     "GET",
			Path:       "/api/auth/signing-keys",
			Handler:    w.HandleSigningKeysApiAuthSigningKeysGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "List the signing keys: id, when it was made, which one signs.",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/rotate",
			Handler:    w.HandleSigningKeyRotateApiAuthSigningKeysRotatePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Mint a new signing key and hand signing over to it; the old one stays, verifying.",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/{key_id}/remove",
			Handler:    w.HandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Remove a retired key, revoking everything it signed. Refuses the signing key.",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/auth/mfa/disable",
			Handler:    w.HandleMfaDisableApiAuthMfaDisablePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Turn the second factor off (password + live code required).",
			MCPExclude: true,
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26) — running the
			// office needs the office's own knobs.
			Method:   "GET",
			Path:     "/api/settings",
			Handler:  w.HandleGetSettingsApiSettingsGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Read the org-adjustable settings (owner/admin agent).",
			MCPTool:  "get_settings",
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:   "PATCH",
			Path:     "/api/settings",
			Handler:  w.HandleUpdateSettingsApiSettingsPatch,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Edit settings (owner and agent token TTLs / handover threshold); live immediately.",
			MCPTool:  "update_settings",
		},
		{
			Method: http.MethodGet, Path: "/api/push/public-key", Handler: w.HandleGetPushPublicKeyApiPushPublicKeyGet,
			Auth: authGated, Requires: principalOwner, Summary: "Read the VAPID public key for this owner's browser.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		},
		{
			Method: http.MethodPost, Path: "/api/push/subscription", Handler: w.HandleCreatePushSubscriptionApiPushSubscriptionPost,
			Auth: authGated, Requires: principalOwner, Summary: "Save this owner's browser Web Push subscription.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		},
		{
			Method: http.MethodDelete, Path: "/api/push/subscription", Handler: w.HandleDeletePushSubscriptionApiPushSubscriptionDelete,
			Auth: authGated, Requires: principalOwner, Summary: "Remove this owner's browser Web Push subscription.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:   "GET",
			Path:     "/api/release/check",
			Handler:  w.HandleCheckReleaseApiReleaseCheckGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Check GitHub Releases for a newer official OffiCraft version.",
			MCPTool:  "check_release",
		},
		{
			// T-29c7: the cockpit's theme import box takes a LINK. Floor is
			// admin_agent because that is exactly the floor of the write that
			// stores the imported theme — a caller who could fetch but not store
			// would only ever get a dead end. (That write was the PATCH
			// /api/settings custom_themes array until T-83ef; it is PUT
			// /api/themes/{theme_id} now. Same floor, so the reasoning stands —
			// but the two must be kept equal deliberately, not by luck.)
			// MCPExclude: this is the cockpit's paste-a-link seam, not an agent
			// tool (an agent that HAS a theme bundle already holds the JSON; it
			// has no reason to ask the server to go read one back).
			Method:     "POST",
			Path:       "/api/theme/fetch",
			Handler:    w.HandleFetchThemeApiThemeFetchPost,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Fetch a theme bundle from a link (owner/admin agent).",
			MCPExclude: true,
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// runs software upgrades; a PLAIN agent still cannot self-upgrade
			// the server (the admin_agent choke keeps rank<2 out).
			Method:   "POST",
			Path:     "/api/update/upgrade",
			Handler:  w.HandleUpgradeApiUpdateUpgradePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Trigger a software upgrade to the latest GitHub release.",
			MCPTool:  "upgrade_station",
		},
		// ── Gated infra seams ────────────────────────────────────────────────
		{
			Method:     "GET",
			Path:       "/api/events",
			Handler:    w.HandleEventsApiEventsGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "SSE delta stream (owner-scoped fan-out; reconcile-by-refetch).",
			MCPExclude: true, // a live stream is not a callable tool
		},
		{
			Method:     "POST",
			Path:       "/api/mcp",
			Handler:    w.HandleMcpApiMcpPost,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "MCP JSON-RPC transport (tools/list + tools/call over the routes).",
			MCPExclude: true, // the MCP endpoint is the transport, not a tool
		},
		// ── Members — roster + presence + lifecycle ──────────────────────────
		{
			Method:   "GET",
			Path:     "/api/members",
			Handler:  w.HandleListMembersApiMembersGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List every member that has not been removed, including outsource members by default (presence-derived MemberDTO[]). fields=light returns an identity-only projection that preserves kind.",
		},
		{
			Method:   "POST",
			Path:     "/api/members",
			Handler:  w.HandleHireMemberApiMembersPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Hire a member (server mints the id). An omitted runtime is stored UNSET and resolved from the target host's reported runtime capabilities at first placement (a codex-only host grows a codex member) rather than written as claude; only claude/codex are accepted when you do name one; effort defaults to medium and is validated; a hire that names kind or role_key is admin-gated.",
			MCPTool:  "hire_member",
		},
		{
			Method:   "GET",
			Path:     "/api/members/{member_id}",
			Handler:  w.HandleGetMemberApiMembersMemberIdGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one member row — STAFF OR OUTSOURCE, matching what GET /api/members already lists (removed → 404). It answered 404 for an ow- id until 2026-08-28, which cost the cockpit one guaranteed failed request plus a whole-roster refetch on every contractor chat line; the write verbs on this same {member_id} keep refusing outsource and say so themselves.",
			MCPTool:  "get_member",
		},
		// ⚠️ update_member sits at the machine FLOOR **deliberately** — owner
		// 2026-07-27 (T-5336) looked at exactly this row and ruled "keep it".
		// The premise is that roster members TRUST EACH OTHER: editing a
		// colleague's display name / model / effort is office housekeeping, not
		// a governance act, so it is not worth a choke.
		//
		// The asymmetry with DELETE on the SAME {member_id} — dismiss_member is
		// principalAdminAgent, so "edit him" is easier than "fire him" — is
		// KNOWN and ACCEPTED, not an oversight. Dismissal is irreversible and
		// removes someone from the office; an edit is reversible by the next
		// caller. They are different acts and they get different floors.
		//
		// This note exists so the NEXT permission audit does not re-open the
		// question: it was asked, it was ruled on, and the answer was no change.
		// Raising this row needs a fresh owner ruling, not a tidy-up commit.
		{
			Method:   "PATCH",
			Path:     "/api/members/{member_id}",
			Handler:  w.HandleUpdateMemberApiMembersMemberIdPatch,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Partially update a member's name / runtime / model / effort. Blank name, invalid runtime or invalid effort → 422, and changing a launch-intent field arms a graceful handover.",
			MCPTool:  "update_member",
		},
		{
			Method:  http.MethodPut,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandlePutMemberAvatarApiMembersMemberIdAvatarPut,
			Auth:    authGated,
			// T-c826 owner 2026-07-27 explicitly chose owner-only: a personal
			// avatar is owner-managed member identity/presentation, not an
			// operational capability an agent may change for itself or peers.
			Requires:   principalOwner,
			Summary:    "Upload or replace a member's personal avatar (owner only).",
			MCPExclude: true,
		},
		{
			Method:  http.MethodDelete,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete,
			Auth:    authGated,
			// Same T-c826 ruling as PUT: removal changes the owner's chosen
			// member identity and therefore stays off the AI-callable surface.
			Requires:   principalOwner,
			Summary:    "Remove a member's personal avatar (owner only).",
			MCPExclude: true,
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/activate",
			Handler:  w.HandleActivateMemberApiMembersMemberIdActivatePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Activate: write desired_state=online intent (does NOT flip online).",
			MCPTool:  "activate_member",
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/relocate",
			Handler:  w.HandleRelocateMemberApiMembersMemberIdRelocatePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Relocate a member to a machine (placement only; never touches desired_state). Also accepts an outsource-worker id: the same move-one-agent verb relocates the worker. machine_id is REQUIRED (owner 2026-07-27): a relocate NAMES the destination machine and no longer doubles as an unpin — an absent key is a 422, an explicit null or \"\" is a 400.",
			MCPTool:  "relocate_member", // owner-cockpit 改機器 + admin-agent 工具 (T-8655): Mira 可經 MCP 把 member 搬機; 權限仍 principalAdminAgent (一般 agent 擋)。P7c: member_id 也吃 worker id (ow-…) — handler falls through to the worker relocate core (外包對齊正職)
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/deactivate",
			Handler:  w.HandleDeactivateMemberApiMembersMemberIdDeactivatePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Deactivate: desired_state=offline + stamp stopping_since (retains row).",
			MCPTool:  "deactivate_member",
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/force-stop",
			Handler:  w.HandleForceStopMemberApiMembersMemberIdForceStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Force-stop: robust STOP now. On the offboard arm the server starts no clock of its own -- collection is the agent's report_stopped, the deadline the owner opens with 加速停止, or this.",
			MCPTool:  "force_stop_member",
		},
		{
			Method:  "POST",
			Path:    "/api/members/{member_id}/cost/reset",
			Handler: w.HandleResetCostApiMembersMemberIdCostResetPost,
			Auth:    authGated,
			// principalOwner, NOT the admin_agent floor its neighbours sit on,
			// and that gap is the decision rather than an oversight (T-53,
			// owner ruling rc-7dea0deefa63). The rows above control a member;
			// this one destroys the owner's own spend record, irreversibly and
			// with nothing else in the system holding a copy. An admin agent
			// deciding that on his behalf is not a thing he asked for.
			Requires: principalOwner,
			Summary:  "Reset one actor's estimated spend to zero (owner-only, irreversible): clears the durable banked figure AND the live telemetry figure.",
			// Owner-only cockpit surface, so MCP-excluded on the same reasoning
			// as the mint / credential / avatar rows: an agent has nothing
			// legitimate to do with the owner's spend record.
			MCPExclude: true,
		},
		{
			Method:  "POST",
			Path:    "/api/accounts/cost/reset",
			Handler: w.HandleResetAccountCostApiAccountsCostResetPost,
			Auth:    authGated,
			// Same owner-only floor, same reasoning, as the per-actor row
			// above: an irreversible write to a figure the owner watches.
			//
			// 🔴 IT TOUCHES NO ACTOR. An earlier shape of this route did clear
			// every actor on the account (rc-efae958cef40); the owner then
			// ruled the two clearings SEPARATE (rc-5c5d7c7c6dcd, 2026-09-02),
			// so the account card became an accumulator of its own (migration
			// 00069) and this route writes that one row and nothing else.
			Requires: principalOwner,
			Summary:  "Reset ONE account's own accumulated spend (owner-only, irreversible): writes that account's accumulator to 0 and touches no member or worker figure.",
			// The account key rides in the BODY, not the path: a real key is a
			// compound free string containing '/' and '@', and an encoded
			// slash that a proxy decodes would silently retarget an
			// irreversible call. See the spec entry.
			MCPExclude: true,
		},
		{
			Method:   "POST",
			Path:     "/api/self/waking",
			Handler:  w.HandleReportWakingApiSelfWakingPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "report_waking(): stamp the caller's waking + clear recycle markers.",
			MCPTool:  "report_waking",
		},
		{
			Method:   "POST",
			Path:     "/api/self/stopping",
			Handler:  w.HandleReportStoppingApiSelfStoppingPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "report_stopping(): stamp the caller's stopping_since (graceful stop).",
			MCPTool:  "report_stopping",
		},
		{
			Method:   "POST",
			Path:     "/api/self/stopped",
			Handler:  w.HandleReportStoppedApiSelfStoppedPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "report_stopped(): anchor the caller's stopped; fire recycle kill.",
			MCPTool:  "report_stopped",
		},
		{
			Method:   "POST",
			Path:     "/api/self/refocus",
			Handler:  w.HandleRestartSelfApiSelfRefocusPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "restart_self(): self-triggered recycle (online-only 409; min-liveness 429; wind-down-ladder 409).",
			MCPTool:  "restart_self",
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/refocus",
			Handler:  w.HandleRefocusMemberApiMembersMemberIdRefocusPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Refocus a member's context (online-only, else 409).",
			MCPTool:  "refocus_member",
		},
		{
			Method:   "DELETE",
			Path:     "/api/members/{member_id}",
			Handler:  w.HandleDismissMemberApiMembersMemberIdDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Dismiss a member (soft delete). Pure seam, no UI (§9.1).",
			MCPTool:  "dismiss_member",
		},
		// ── Webhooks — a member's 回呼端點 (M4) ─────────────────────────────────
		// Owner-facing config CRUD. T-5336 (owner 2026-07-27) moved ALL FOUR
		// verbs from the machine floor to principalAdminAgent.
		//
		// ⚠️ WHY all four, read/list included: every one of these responses
		// carries WebhookEndpointDTO, and that DTO carries the endpoint's
		// PLAINTEXT `token` — the whole credential of the public `/in` inlet.
		// A read is therefore not "less dangerous" than a write here: LIST
		// hands out every one of a member's inlet secrets in one call, and
		// anybody holding one can inject synthetic chat into that member.
		//
		// ⚠️ MCPExclude is NOT the boundary and never was. It keeps the rows
		// off the MCP tool surface (and out of the catalog hash) — that is a
		// discoverability fact about one client, not an authz fact. Any holder
		// of an agent token could always call these over plain REST; before
		// T-5336 the floor let them through. The wire.go / spec description of
		// `token` claimed it "is NEVER on any public or agent-facing wire",
		// which was simply false on the machine floor — that sentence was
		// rewritten in the same change rather than left as a comment that
		// argued the code was safe.
		{
			Method:     "GET",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleListWebhooksApiMembersMemberIdWebhooksGet,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "List a member's webhook endpoints (WebhookEndpointDTO[]).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleCreateWebhookApiMembersMemberIdWebhooksPost,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Create a webhook endpoint (server mints the token).",
			MCPExclude: true,
		},
		{
			Method:     "PATCH",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Toggle status / edit purpose of a webhook endpoint.",
			MCPExclude: true,
		},
		{
			Method:     "DELETE",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Delete (permanently revoke) a webhook endpoint.",
			MCPExclude: true,
		},
		// Debug ring buffer — raw external payloads. T-6020 (owner 2026-07-26)
		// opened it to admin_agent; a PLAIN agent still cannot see another
		// channel's unverified input through this side door.
		{
			Method:   "GET",
			Path:     "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
			Handler:  w.HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Last 5 raw /in requests of one webhook endpoint (debug).",
			MCPTool:  "list_webhook_requests",
		},
		// T-f059 定期訊息 — the clock-driven twin of the webhook CRUD above:
		// same four verbs, same admin_agent floor, trigger swapped from an
		// inbound call to a recurring wall-clock slot. requires=admin_agent for
		// CONSISTENCY with the neighbour, NOT because a secret rides the wire:
		// ScheduledMessageDTO carries no credential at all (the webhook DTO's
		// plaintext token is what forced that one). Same level as the
		// neighbouring config CRUD means the owner and the admin assistant set
		// these up, which is what the design asks for.
		//
		// 🔴 ALL FOUR CARRY AN MCP TOOL (T-63bf). They shipped MCPExclude, on
		// the webhook precedent, with the reason written out as "configuration
		// CRUD belongs in the cockpit, not the tool catalogue — only the
		// debugging read (list_webhook_requests) ever earned a tool". That rule
		// is REVERSED here, and only here, for a reason that is specific to
		// this feature rather than a general loosening of it:
		//
		//   * owner ruling, 2026-08-19, verbatim: 「助理應該能夠代替我設定這些
		//     東西 在我的授權下進行」 — the admin assistant is meant to be able
		//     to set these up on his behalf.
		//   * and the precedent did not actually transfer. A webhook endpoint is
		//     a door for something OUTSIDE the office to speak in; a scheduled
		//     message exists to WAKE AN AI MEMBER. Leaving the only way to set
		//     one up on a surface no AI can reach is a design deadlock: an alarm
		//     clock whose whole purpose is to wake the agent, that only a human
		//     can ever set.
		//
		// What did NOT change: Requires stays principalAdminAgent on every row.
		// Opening the entrance is not widening the gate — an ordinary agent
		// calling any of these four still gets 403, and that is the correct
		// answer, not a gap. routes_t63bf_scheduled_message_mcp_test.go pins
		// both halves (the tools exist AND the floor did not move).
		{
			Method:   "GET",
			Path:     "/api/members/{member_id}/scheduled-messages",
			Handler:  w.HandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "List one member's scheduled messages — 定期訊息, the wall-clock alarm that wakes that member with a chat message on a repeating cadence. admin_agent floor: the owner, or an admin assistant setting these up on the owner's behalf; an ordinary agent gets 403 even for its own member_id. Rows come oldest→newest and each carries the whole schedule — label, body, cadence, the slot fields `hour`/`minute`/`day_of_week`/`day_of_month`, the four `custom` sets, timezone, and the enabled/disabled toggle — plus the delivery cursor `last_fired_slot`/`last_fired_ts`. Read this before update_scheduled_message: that call is a partial edit against these stored values, and it re-aims the cursor only for a slot field whose value actually CHANGES. 404 if the member is absent or soft-removed.",
			MCPTool:  "list_scheduled_messages",
		},
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/scheduled-messages",
			Handler:  w.HandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Create a scheduled message on one member — 定期訊息, the mechanism for waking a member on a repeating wall-clock slot: at each due slot the server delivers `body` verbatim down the ORDINARY chat path, from the synthetic sender `sched:<schedule_id>`. admin_agent floor: the owner, or an admin assistant setting one up on the owner's behalf; an ordinary agent gets 403 even for its own member_id. The recipient follows chat's rule, so an `ow-` outsource worker is a legal target as well as a staff member. `body`, `cadence` and `timezone` are always required; `hour`/`minute` are required by `daily`/`weekly`/`monthly` and ignored by `custom` FOR SCHEDULING — their range is still checked under every cadence, so `hour: 99` is a 422 even for `custom`, which instead requires `custom_days`/`custom_hours`/`custom_minutes` (`custom_months` may be omitted to mean all twelve; an explicit empty set is a 422). Those conditional rules are NOT expressible in this schema — a wrong combination comes back as a 422 rather than folding into a silent midnight. TWO fields are the exception and they fail SILENTLY: `day_of_week` (used by `weekly`) and `day_of_month` (used by `monthly`) are NOT required — omit either one and the create returns 200 having defaulted it to 0 (Sunday) and 1 (the first of the month). 'Every Friday at 09:00' sent without `day_of_week` is a Sunday alarm and nothing reports it, so send the field explicitly whenever the cadence reads it. `timezone` must NAME A PLACE: `Local` and the empty string are refused with 422 even though they resolve, because they hand \"what time is it\" to wherever the server happens to run. Missed slots are never backfilled — only the slot most recently elapsed is ever considered — and the cursor starts at creation time, so a `daily` 09:00 schedule created at 10:00 does not fire today. 404 if the member is absent or soft-removed.",
			MCPTool:  "create_scheduled_message",
		},
		{
			Method:   "PATCH",
			Path:     "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler:  w.HandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Update one scheduled message, including the enabled/disabled toggle (`status`) — 定期訊息, the wall-clock wake-up for one member. admin_agent floor: the owner, or an admin assistant acting on the owner's behalf; an ordinary agent gets 403 even for its own member_id. PATCH semantics: only the fields you send change, and `id`/`member_id` are immutable. The create-side validation applies unchanged — `hour`/`minute` required by `daily`/`weekly`/`monthly` and ignored by `custom` for scheduling though still range-checked under every cadence, the custom sets never empty, `timezone` never `Local` or the empty string — all 422. Editing a timing field to a DIFFERENT value re-aims the delivery cursor to the slot most recently elapsed, so the edit never retroactively fires the slot it crossed; re-sending a value the schedule already holds moves nothing, which is what makes a whole-form save safe. `disabled` suspends firing and is reversible — it is not a lifecycle state; delete_scheduled_message is the permanent removal. 404 if the member or the schedule is absent.",
			MCPTool:  "update_scheduled_message",
		},
		{
			Method:   "DELETE",
			Path:     "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler:  w.HandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Delete one scheduled message — 定期訊息, permanent and not undoable. admin_agent floor: the owner, or an admin assistant acting on the owner's behalf; an ordinary agent gets 403 even for its own member_id. When the schedule should merely STOP firing, call update_scheduled_message with `status: disabled` instead — that is the reversible half and this one is not. 404 if the member or the schedule is absent.",
			MCPTool:  "delete_scheduled_message",
		},
		// T-8b0d (owner 2026-08-02): the SAME bounded wake snapshot as
		// /api/resume-summary, for a TARGET member instead of the caller
		// (control-others; member_id is a target param, never the caller's
		// own identity -- Sec14). Assembled by the identical, unmodified
		// resumeSnapshotParts(actor) called with actor=member_id -- no
		// near-copy of the assembly. requires=admin_agent: only an
		// owner-scoped token OR an admin-role (assistant) member may pull
		// another member's resume snapshot; a plain agent -> 403.
		// /api/resume-summary and its identity lock are unchanged by this
		// addition.
		{
			Method:   "GET",
			Path:     "/api/members/{member_id}/resume-summary",
			Handler:  w.HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "The SAME bounded wake snapshot as resume_summary, for a TARGET member (member_id) instead of the caller — control-others, admin_agent+ only (owner-scope or role=assistant); an ordinary agent gets 403. Same identity/chat/light-task-rows/roster/machines/overview/note shape, assembled by the identical resumeSnapshotParts function (so the roster and machine blocks cannot drift from what that member would get on waking; note that machines.you_are_on resolves for the TARGET member, not for you); resume_summary itself is unchanged and still identity-locked to the caller.",
			MCPTool:  "get_member_resume_summary",
		},
		// ── Webhook inlet — PUBLIC (M4 §2) ─────────────────────────────────────
		// Token-only identity (?t=); the path carries nothing else. Silent 200
		// for every case (accepted OR ignored) so it never leaks existence.
		{
			Method:     "POST",
			Path:       "/in",
			Handler:    w.HandleReceiveWebhookInPost,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Public webhook inlet — token-only (member/endpoint/purpose) delivery.",
			MCPExclude: true,
		},
		// ── Chat ─────────────────────────────────────────────────────────────
		{
			Method:   "POST",
			Path:     "/api/chat",
			Handler:  w.HandlePostChatApiChatPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Post a chat message (sender = verified JWT sub; auto SSE fan-out). ``to`` must name the owner or an active AI member; unknown, removed, and machine ids are rejected. Presence is not a gate: an offline member keeps its durable mailbox.",
		},
		{
			Method:   "GET",
			Path:     "/api/chat",
			Handler:  w.HandleListChatApiChatGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List the chat stream (?with=<id>&limit=<n>; oldest→newest). Answers an OBJECT {messages, next_cursor}, never a bare array: next_cursor is opaque, send it back as cursor= for the next page, and its ABSENCE — not a short page — is the only 'nothing more' signal. Your unread backfill: unread=true returns the OLDEST unread addressed to you, judged against the per-sender watermark, and still marks nothing read. Narrow either side with sender= / recipient=. Window by message id: start_id walks TOWARDS THE NEWEST, end_id TOWARDS THE OLDEST, both inclusive. The older before_ts + before_id cursor still works but is deprecated. Re-read specific messages by id: ids=<id>&ids=<id>. THIS ROUTE NEVER MARKS ANYTHING READ (T-48) — to mark a conversation read, call mark_read explicitly.",
		},
		{
			Method:     "GET",
			Path:       "/api/chat/attachment/{attachment_id}",
			Handler:    w.HandleGetChatAttachmentApiChatAttachmentAttachmentIdGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Serve a chat attachment blob (owner-gated; raw bytes + stored mime).",
			MCPExclude: true,
			MCPTool:    "get_chat_attachment",
			ShareSig:   verifyAttachmentShareSig,
		},
		{
			Method:   "GET",
			Path:     "/api/chat/attachments/{attachment_id}/share-link",
			Handler:  w.HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Mint a single-file share link (?sig= HMAC; grants read of this one attachment only). Returns {url} as a SERVER-RELATIVE path — prefix it with the origin you reach this server on to get a link you can paste to someone. The sig carries NO identity and NO expiry: whoever holds the link reads that one blob without signing in, for as long as the key that signed it is still in the server's signing-key ring. No single link can be withdrawn; the only way to void one is to remove that key (POST /api/auth/signing-keys/{key_id}/remove), which voids every link it signed at once. Mint it for deliverables you meant to hand over; do not paste it anywhere the blob itself would not belong.",
			// This row used to read `MCPExclude: true, // a UI convenience
			// seam, not an agent tool`. That call is REVERSED here, on
			// purpose: minting is an agent seam too. An agent that produces a
			// deliverable uploads the blob and pins it, and the only way its
			// reader then gets the bytes is to sign in to the cockpit and copy
			// the link by hand — the agent could make the file but never a
			// usable link to it, which is the one thing it needed to hand over.
			//
			// The authz floor is UNCHANGED (machine): every authenticated
			// principal already reached this route over REST, so no caller
			// gains a capability it lacked. What changes is discoverability —
			// and that is not risk-neutral: minting will happen far more often
			// now, and every minted link is credential-less and carries no
			// expiry (sharesig.go). Since T-62 it is not unrevocable: a link
			// dies when the key that signed it leaves the signing-key ring —
			// which is a COARSE revocation (it takes every link that key
			// signed with it) and not a per-link one. Read that file before
			// widening this seam any further.
			MCPTool: "get_chat_attachment_share_link",
		},
		{
			Method:   "GET",
			Path:     "/api/chat/attachments",
			Handler:  w.HandleListChatAttachmentsApiChatAttachmentsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary: "List every attachment of a member's conversations " +
				"(?with=<member_id>; flattened, sender-labelled, newest→oldest).",
		},
		{
			Method:     "POST",
			Path:       "/api/chat/attachments",
			Handler:    w.HandleUploadChatAttachmentApiChatAttachmentsPost,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Upload one attachment blob (raw octet-stream body; returns the light ref). ?filename= is capped at 128 characters (Unicode runes, not bytes); a longer one is refused with a 400 rather than truncated.",
			MCPExclude: true, // a binary ingest seam like the blob GET, not a tool
		},
		{
			Method:   "POST",
			Path:     "/api/chat/mark-read",
			Handler:  w.HandleMarkChatReadApiChatMarkReadPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Mark a conversation read up to a watermark (reader = verified sub).",
		},
		{
			Method:   "GET",
			Path:     "/api/chat/reads",
			Handler:  w.HandleListChatReadsApiChatReadsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List chat read receipts (?with=<peer>; per-conversation watermark).",
		},
		// ── Reply cards (等我回覆卡) ─────────────────────────────────────────
		{
			Method:   "POST",
			Path:     "/api/reply-cards",
			Handler:  w.HandleCreateReplyCardApiReplyCardsPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Open a reply card: an ask the owner must answer (options ≤4 on a single card, ≤20 on a multi card, each carrying its own ai_pick flag; select_mode single|multi). linked_task is REQUIRED and has no default — every card must SAY whether it is about a task, because the server no longer infers one. Send linked_task={\"task_id\": ..., \"step_id\": ...} to bind the ask to the step it is about: that step (and its task) enters waiting_owner until the owner answers. Send linked_task=null when the ask is not about a task — it opens as a plain unbound 請示. BOTH ids are required in the object form: a task_id with NO step_id is a 400, because a card bound to a task but to no step places no 等我回覆 hold, so the task would finish underneath your question and the owner's answer would then be rejected for good. Omitting linked_task entirely is a 400 that names both legal shapes. Optional attachments ride the question (same shape as post_chat: {id} from `ocagent upload` / POST /api/chat/attachments, or inline data_b64).",
			MCPTool:  "create_reply_card",
		},
		{
			Method:   "GET",
			Path:     "/api/reply-cards",
			Handler:  w.HandleListReplyCardsApiReplyCardsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List light reply-card rows (summary and decision digest, without the full body/options). status is waiting (the default, longest-waiting first), answered (the last 24 hours) or expired (the last 24 hours); a positive limit is applied after each pane is ordered. Read one card in full with get_reply_card.",
			MCPTool:  "list_reply_cards",
		},
		{
			Method:     "GET",
			Path:       "/api/reply-cards/count",
			Handler:    w.HandleReplyCardCountApiReplyCardsCountGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Waiting reply-card count (the cockpit badge).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		},
		{
			Method:     "GET",
			Path:       "/api/chat/unread-count",
			Handler:    w.HandleChatUnreadCountApiChatUnreadCountGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Total chat unread count (the 辦公室 nav red dot).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		},
		// ── Comparisons: a URL, not an attachment (T-59) ─────────────────────
		{
			Method:   "GET",
			Path:     "/api/diff",
			Handler:  w.HandleGetDiffApiDiffGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Resolve both sides of one comparison (?before=&after=; optional labels and ?sig=). Each side carries its text, its column heading, and an honest gone marker when the address resolves to nothing.",
			// The DATA seam behind the /diff page, like the attachment blob GET
			// it sits beside — an agent hands over a LINK, it does not fetch the
			// pair and re-narrate it.
			MCPExclude: true,
			// The unauthenticated path, and the ONLY one: a credential-less
			// request may present ?sig=, verified over both addresses and both
			// labels (verifyDiffShareSig). There is no second bypass anywhere.
			ShareSig: verifyDiffShareSig,
		},
		{
			Method:   "GET",
			Path:     "/api/diff/share-link",
			Handler:  w.HandleGetDiffShareLinkApiDiffShareLinkGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Mint the EXTERNAL link to one before/after comparison (?sig= HMAC over both addresses AND both column labels). Returns {url} as a SERVER-RELATIVE path — prefix it with the origin you reach this server on to get a link you can paste to someone. The sig carries NO identity and NO expiry: whoever holds the link sees that one comparison without signing in, for as long as the key that signed it is still in the server's signing-key ring. No single link can be withdrawn; the only way to void one is to remove that key (POST /api/auth/signing-keys/{key_id}/remove), which voids every comparison link and every file link it signed at once. YOU USUALLY DO NOT NEED THIS: the INTERNAL link is the same /diff?before=…&after=… page with no sig, any signed-in reader opens it, and `ocagent diff` prints it without asking the server anything. Mint this one only for a reader who has no account. A side is a stored attachment id (att-…) or doc:<kind>/<key>/<at>/<field> — `ocagent diff --help` is the authority on the spelling.",
			// On the agent surface for the same reason the attachment share
			// link is: an agent that produces a comparison can otherwise only
			// hand it to someone who can already sign in. Minting is where the
			// permanence lives — read sharesig.go before widening this.
			MCPTool: "get_diff_share_link",
		},
		{
			Method:   "GET",
			Path:     "/api/reply-cards/{card_id}",
			Handler:  w.HandleGetReplyCardApiReplyCardsCardIdGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one reply card (full context: options, status, answer).",
			MCPTool:  "get_reply_card",
		},
		// T-6020 (owner 2026-07-26): the three card-closing faces open to
		// admin_agent — the admin 助理 answers on the owner's behalf. A plain
		// agent still cannot close its own card (rank<2 → 403), so "no agent
		// self-answers its own 請示" survives.
		{
			Method:   "POST",
			Path:     "/api/reply-cards/{card_id}/answer",
			Handler:  w.HandleAnswerReplyCardApiReplyCardsCardIdAnswerPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Answer a waiting reply card — the only positive close.",
			MCPTool:  "answer_reply_card",
		},
		{
			Method:   "PUT",
			Path:     "/api/reply-cards/{card_id}/answer",
			Handler:  w.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Revise an answered card's answer (重新決定): stays answered.",
			MCPTool:  "reanswer_reply_card",
		},
		// T-1b88 (owner 2026-08-07, card rc-3ff94b116970) REVISES the T-6020
		// ruling for THIS row only: 「應該是owner(我)，或是開卡的人，都可以標為過期？」
		// — the same verb, two kinds of caller. The floor therefore drops to
		// principalAgent and the caller check moves IN-HANDLER (the author
		// exception is a per-card fact the route table cannot express):
		// HandleExpireReplyCardApiReplyCardsCardIdExpirePost admits owner /
		// admin_agent, or the card's OWN author (ReplyCard.FromMember ==
		// current actor), and 403s every other agent. The two answer rows above
		// are untouched — closing someone else's ask with an ANSWER is still
		// governance, and an already-answered card can no longer be EXPIRED by
		// anyone, the owner included (a decision must not be overwritten by an
		// answerless terminal — but the owner may still REPLACE the answer via
		// the PUT row above). Because the floor no longer
		// says who may call this, routes_t6020_governance_test.go keeps this row
		// in a SEPARATE named table (t6020Revised) rather than dropping it: the
		// 2026-07-26 ruling and its 2026-08-07 revision both stay on the record.
		{
			Method:   "POST",
			Path:     "/api/reply-cards/{card_id}/expire",
			Handler:  w.HandleExpireReplyCardApiReplyCardsCardIdExpirePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Mark a waiting card expired (標為過期): its author, owner, or admin agent; terminal, not an answer.",
			MCPTool:  "expire_reply_card",
		},
		// ── Agent context gauge + monitoring ─────────────────────────────────
		{
			Method:   "POST",
			Path:     "/api/agent/context",
			Handler:  w.HandleIngestAgentContextApiAgentContextPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Ingest an agent's context gauge (in-memory; bad body → 400).",
			MCPTool:  "ingest_agent_context",
		},
		{
			Method:   "POST",
			Path:     "/api/monitoring/telemetry",
			Handler:  w.HandleIngestTelemetryApiMonitoringTelemetryPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Ingest warden telemetry (hardware/limits/tokens/cost/self_update).",
			MCPTool:  "ingest_telemetry",
		},
		{
			Method:   "GET",
			Path:     "/api/monitoring",
			Handler:  w.HandleGetMonitoringApiMonitoringGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Monitoring telemetry (roster + context + warden push; honest — else).",
		},
		{
			Method:   "GET",
			Path:     "/api/backup-health",
			Handler:  w.HandleGetBackupHealthApiBackupHealthGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Backup health: is the scheduled backup still producing retreat points?",
			// T-da06: deliberately NOT an MCP tool. The consumer is the cockpit's
			// permanently mounted indicator (and its monitor card), i.e. a UI
			// seam; the backup engine's own outcomes already reach the server log
			// for anyone reading a machine. Publishing it as a tool is a separate
			// owner decision — the point of this ticket was to reach the HUMAN,
			// who does not read tool output either.
			MCPExclude: true,
		},
		// ── Display-name overlays ────────────────────────────────────────────
		{
			Method:   "PATCH",
			Path:     "/api/accounts/{account_id}",
			Handler:  w.HandleUpdateAccountApiAccountsAccountIdPatch,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Set an account's display name (id = stable tag). Blank name → 422.",
			MCPTool:  "update_account",
		},
		{
			Method:   "PATCH",
			Path:     "/api/machines/{machine_id}",
			Handler:  w.HandleUpdateMachineApiMachinesMachineIdPatch,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Set a machine's display name (id = stable host). Blank name → 422.",
			MCPTool:  "update_machine",
		},
		// ── Installer + machine onboard / teardown ───────────────────────────
		{
			Method:     "GET",
			Path:       "/install.sh",
			Handler:    w.HandleInstallScriptInstallShGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "One-line remote warden installer (curl|bash; token+id in URL query).",
			MCPExclude: true, // a bash installer script, not an agent tool
		},
		{
			Method:   "GET",
			Path:     "/api/machines",
			Handler:  w.HandleListMachinesApiMachinesGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List machines (active wardens): machine_id/display_name/online.",
			MCPTool:  "list_machines",
		},
		{
			Method:     "POST",
			Path:       "/api/machines",
			Handler:    w.HandleOnboardMachineApiMachinesPost,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Onboard a machine: new warden member (id == machine id) + exec-token.",
			MCPExclude: true, // a credential-mint seam (like /api/mint), not an agent tool
		},
		{
			Method:     "GET",
			Path:       "/api/machines/{machine_id}/boot-command",
			Handler:    w.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Re-fetch a machine's boot command anytime (re-mints its exec-token).",
			MCPExclude: true, // a credential-mint seam (like onboard), not an agent tool
		},
		{
			Method:     "POST",
			Path:       "/api/machines/claim",
			Handler:    w.HandleClaimMachineTokenApiMachinesClaimPost,
			Auth:       authPublic, // the one-time claim code IS the gate (lifecycle.md §1.3)
			Requires:   requiresPublic,
			Summary:    "Exchange a one-time claim code for the machine's exec-token.",
			MCPExclude: true, // a credential-exchange seam (like /api/login), not an agent tool
		},
		// The renew seam a warden drives itself, so a credential that is about
		// to expire does not need anybody to go and reinstall that machine.
		// principalMachine is the LOWEST rank and an ordinary agent clears it —
		// but principalAgent would lock the WARDEN out (machine ranks BELOW
		// agent), so the warden-only property lives in the handler, not here.
		{
			Method:     "POST",
			Path:       "/api/machines/renew-credential",
			Handler:    w.HandleRenewMachineCredentialApiMachinesRenewCredentialPost,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Renew the CALLER's own machine credential. Takes no body and names no target — the machine acted on is the caller's verified sub, so one machine cannot renew another's.",
			MCPExclude: true, // a credential-mint seam (like claim/onboard), not an agent tool
		},
		// T-6020 (owner 2026-07-26): the two on-server host lifecycle faces open
		// to admin_agent — installing/tearing down the server host's own warden
		// is office operations. A plain agent is still 403 (rank<2).
		{
			Method:   "POST",
			Path:     "/api/machines/{machine_id}/bootstrap-here",
			Handler:  w.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Bootstrap on server: runs `ocwarden install --force` on the SERVER's own host. machine_id is NOT a target — this verb has no way to reach another machine, and naming one is refused (409); the server-local machine is the only value it accepts, and the install overwrites the existing one, which is how you repair this host's warden. To install a different machine, fetch that machine's own boot command with GET /api/machines/{machine_id}/boot-command and run it on that host.",
			MCPTool:  "install_warden_on_server_host",
		},
		{
			Method:   "POST",
			Path:     "/api/machines/{machine_id}/teardown-here",
			Handler:  w.HandleTeardownHereApiMachinesMachineIdTeardownHerePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Teardown on server: runs `ocwarden teardown` on the SERVER's own host. machine_id is NOT a target — this verb has no way to reach another machine, and naming one is refused (409). The server-local machine is refused too (retiring it revokes credentials fleet-wide). To retire another machine use uninstall_machine then delete_machine; to repair the server host's own warden use install_warden_on_server_host, which runs `install --force` over the existing install.",
			MCPTool:  "uninstall_warden_on_server_host",
		},
		{
			Method:   "POST",
			Path:     "/api/machines/{member_id}/uninstall",
			Handler:  w.HandleUninstallMachineApiMachinesMemberIdUninstallPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Uninstall a machine: drive the uninstall RPC to its warden.",
			MCPTool:  "uninstall_machine",
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26) — same floor as
			// uninstall_machine right above, which was already admin_agent.
			Method:   "POST",
			Path:     "/api/machines/{member_id}/upgrade",
			Handler:  w.HandleUpgradeMachineApiMachinesMemberIdUpgradePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Upgrade a machine: kick its warden's self-update NOW.",
			MCPTool:  "upgrade_warden",
		},
		{
			Method:   "DELETE",
			Path:     "/api/machines/{member_id}",
			Handler:  w.HandleDeleteMachineApiMachinesMemberIdDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Delete a machine: soft-delete its warden record (no command sent).",
			MCPTool:  "delete_machine",
		},
		// ── Prebuilt binary downloads (secret-free artifacts) ────────────────
		{
			Method:     "GET",
			Path:       "/api/warden/binary",
			Handler:    w.HandleWardenBinaryApiWardenBinaryGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Download the prebuilt ocwarden binary (octet-stream) for a machine.",
			MCPExclude: true, // a binary download, not an agent tool
		},
		{
			Method:     "GET",
			Path:       "/api/agent/binary",
			Handler:    w.HandleAgentBinaryApiAgentBinaryGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Download the prebuilt ocagent binary (octet-stream) for an agent.",
			MCPExclude: true, // a binary download, not an agent tool
		},
		// ── User context / roles / lessons / bootstrap ───────────────────────
		{
			Method:   "GET",
			Path:     "/api/global-context",
			Handler:  w.HandleGetGlobalContextApiGlobalContextGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read the user-custom additive context block (empty = is_default).",
			MCPTool:  "get_global_context",
		},
		{
			Method:   "POST",
			Path:     "/api/global-context",
			Handler:  w.HandleReplaceGlobalContextApiGlobalContextPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Whole-block replace of the user-custom additive block ({text}). text is REQUIRED; unknown keys are rejected. Replacing existing content with an empty block needs allow_shrink=true (or use reset_global_context).",
			MCPTool:  "replace_global_context",
		},
		{
			Method:   "POST",
			Path:     "/api/global-context/reset",
			Handler:  w.HandleResetGlobalContextApiGlobalContextResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Reset the user-custom block to empty (idempotent tombstone).",
			MCPTool:  "reset_global_context",
		},
		// ── the two boot-context document kinds that became editable (T-791e) ──
		//
		// 系統互動 and 啟動步驟 used to be go:embed seeds with no editable
		// representation at all: one wrong sentence cost a release. They now
		// carry the same read / whole-document replace / reset-to-factory shape
		// the 使用者自訂 block above has, plus document history.
		//
		// FLOORS: read at the machine floor (an agent already reads both blocks
		// in its own boot context — nothing here is new to it); WRITE at
		// admin_agent, because this text lands in EVERY agent's boot context and
		// a broken 啟動步驟 keeps them from coming online at all. That failure is
		// silent: an agent that never boots is never there to report it, which is
		// also why the reset route has to work from the cockpit alone, with no
		// live agent and no MCP client anywhere in the path.
		{
			Method:   "GET",
			Path:     "/api/system-interaction",
			Handler:  w.HandleGetSystemInteractionApiSystemInteractionGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read the 系統互動 block of the boot context — the shared studio handbook every agent reads at boot. Folded: the owner's edit when one exists, otherwise the shipped factory seed, with is_default saying which of the two you are holding and has_seed saying a factory version exists to go back to. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool:  "get_system_interaction",
		},
		{
			Method:   "POST",
			Path:     "/api/system-interaction",
			Handler:  w.HandleReplaceSystemInteractionApiSystemInteractionPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the EDITABLE HALF of the 系統互動 block of the boot context ({body}) — the handbook every agent reads at boot. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. This document carries NO read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.system_interaction cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_system_interaction always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only.",
			MCPTool:  "replace_system_interaction",
		},
		{
			Method:   "POST",
			Path:     "/api/system-interaction/reset",
			Handler:  w.HandleResetSystemInteractionApiSystemInteractionResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore the 系統互動 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it. The overlay being discarded is retained in the document history, so the reset is itself recoverable. Owner or admin assistant only.",
			MCPTool:  "reset_system_interaction",
		},
		{
			Method:   "GET",
			Path:     "/api/boot-sequence/{runtime_key}",
			Handler:  w.HandleGetBootSequenceApiBootSequenceRuntimeKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one runtime's 啟動步驟 block — the boot checklist that ends that runtime's boot context. runtime_key is 'claude' or 'codex'; they are separate documents because step 3 of the two says opposite things (claude mounts its own `ocagent listen`, codex must not — the sidecar owns it), so any other value is a 404 rather than a silent fallback to claude. Folded: the owner's edit when one exists, otherwise the shipped factory seed. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool:  "get_boot_sequence",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-sequence/{runtime_key}",
			Handler:  w.HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the EDITABLE HALF of the 啟動步驟 block of ONE runtime ({runtime_key, body}). runtime_key is 'claude' or 'codex' and the two are separate documents whose step 3 contradicts each other, so writing the wrong one leaves those agents unable to come online — and nothing that never boots reports it. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. Neither runtime's document carries a read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.boot_sequence cap (one cap, both runtimes, each measured on its own text); the refusal tells you what you wrote, the cap, and what is stored. The shipped seed is never overwritten, so reset_boot_sequence always gets the factory text back. Owner or admin assistant only.",
			MCPTool:  "replace_boot_sequence",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-sequence/{runtime_key}/reset",
			Handler:  w.HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore ONE runtime's 啟動步驟 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). runtime_key is 'claude' or 'codex'; anything else is a 404. No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it, which is what makes this the recovery route when a bad edit has stopped agents from booting. The overlay being discarded is retained in the document history. Owner or admin assistant only.",
			MCPTool:  "reset_boot_sequence",
		},
		// 〈停止〉 (T-c9c0) — the fourth owner-editable global document, and the
		// same three-row shape as the 系統互動 block above, floors included: read
		// at the machine floor (every agent is handed this text when its session
		// is collected), write at admin_agent (it is the last instruction an
		// agent gets, with nobody online afterwards to correct it).
		{
			Method:   "GET",
			Path:     "/api/offboard",
			Handler:  w.HandleGetOffboardApiOffboardGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read the 〈停止〉 block — the wrap-up checklist the server hands an agent at the moment it is about to collect that session. It is a SINGLETON: one document for every agent and every runtime, keyed `global` like the 系統互動 block. Folded: the owner's edit when one exists, otherwise the shipped factory seed, with is_default saying which of the two you are holding and has_seed saying a factory version exists to go back to. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool:  "get_offboard",
		},
		{
			Method:   "POST",
			Path:     "/api/offboard",
			Handler:  w.HandleReplaceOffboardApiOffboardPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the EDITABLE HALF of the 〈停止〉 block ({body}) — the wrap-up checklist an agent is handed when its session is being collected. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. This document carries NO read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.offboard cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_offboard always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only.",
			MCPTool:  "replace_offboard",
		},
		{
			Method:   "POST",
			Path:     "/api/offboard/reset",
			Handler:  w.HandleResetOffboardApiOffboardResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore the 〈停止〉 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it. The overlay being discarded is retained in the document history, so the reset is itself recoverable. Owner or admin assistant only.",
			MCPTool:  "reset_offboard",
		},
		// ── The GENERIC face of the same documents (T-3201) ─────────────────
		// Six more of these documents shipped with the task-event procedures,
		// and three named routes each would have been eighteen more rows here
		// and eighteen more tools in EVERY agent's tool list — a permanent 15%
		// growth of a surface most agents never touch. The owner chose the
		// generic route (rc-88e4ab40fe1d) once the argument against it turned
		// out to rest on a premise nobody had checked: the floors were said to
		// be per-document, and they are the same sentence copied for each one.
		//
		// FLOORS, therefore, are the named routes' floors verbatim: read at the
		// machine floor (an agent already reads these documents — the boot fold
		// hands them over), WRITE at admin_agent because this text lands in
		// every agent's boot context or in the notice an agent is collected
		// with, and a broken one is read by everybody and reported by nobody.
		//
		// 🔴 WHAT IS NOT EXPRESSED HERE: read-only documents. A read-only
		// document may never be edited by anyone, and that refusal is NOT an
		// authz floor — no principal can pass it, so declaring it here would
		// name a rank nobody holds. It lives on the write path
		// (bootDocReadOnlyRefusal, 405) where it can say what the document IS
		// rather than what the caller lacks.
		//
		// ⚠️ T-6f44 (owner's decision 2): NO SHIPPED DOCUMENT IS READ-ONLY TODAY.
		// This used to open "Two of the ten may never be edited by anyone" and
		// that sentence outlived the fact. The refusal is kept, not deleted —
		// bootDocRegistry is the truth source and a future document may ship
		// read-only — but nothing in the registry sets the flag right now, and
		// TestBootDocRegistry_NoDocumentIsReadOnly is what keeps that a
		// statement rather than a hole.
		{
			Method:   "GET",
			Path:     "/api/boot-docs/{kind}/{key}",
			Handler:  w.HandleGetBootDocApiBootDocsKindKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one block of the boot context by kind/key, folded (the owner's edit ⊕ the shipped seed). Carries size_chars/cap_chars so an edit can be sized before it is made, is_default/has_seed to tell an edited block from the shipped one, and read_only for the blocks that are shown but may never be edited. An unknown kind or key is a 404 that names the keys that exist.",
			MCPTool:  "get_boot_doc",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-docs/{kind}/{key}",
			Handler:  w.HandleReplaceBootDocApiBootDocsKindKeyPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the EDITABLE HALF of one boot-context block ({kind, key, body}) — text every agent reads at boot, or is sent when a lifecycle event happens to it. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. The stored result is judged against that block's own cap. A read-only block refuses with 405 for every caller. The read-only head is NOT sent and cannot be: the server joins the shipped head back on, so no caller has any way to write it. Owner or admin assistant only.",
			MCPTool:  "replace_boot_doc",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-docs/{kind}/{key}/reset",
			Handler:  w.HandleResetBootDocApiBootDocsKindKeyResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore one boot-context block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap applies on this path — the way back to factory text is never blocked by a setting, which is what makes it the recovery route after an edit that stopped agents from booting. The discarded overlay is retained in the document history. Owner or admin assistant only.",
			MCPTool:  "reset_boot_doc",
		},
		{
			Method:   "GET",
			Path:     "/api/roles",
			Handler:  w.HandleListRolesApiRolesGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List role definitions (seed defaults + owner edits) WITHOUT the persona bodies: each row is the role identity plus its definition size and cap, never definition_md itself. Read the one role you want with get_role.",
			MCPTool:  "list_roles",
		},
		{
			Method:  "GET",
			Path:    "/api/doc-sizes",
			Handler: w.HandlePeekDocSizesApiDocSizesGet,
			Auth:    authGated,
			// The machine floor, matching the READ face of every document it
			// sizes (list_roles / get_insight / get_lessons / list_task_manuals
			// are all machine). It cannot leak more than those already do — it
			// carries strictly less than any of them.
			Requires: principalMachine,
			Summary:  "Size-only overview of the capped documents on the station: each role's role definition / insight / lessons, and each task manual's SOP / learnings, as size_chars plus the cap_chars in force for THAT segment (the five segments have five separate caps — each is reported against its own). THE LISTING IS KEYED BY ROLE, AND THAT IS ITS LIMIT. T-2 removed the lessons task_type axis, so a role now has exactly ONE lessons document and it is the one reported here — the old 'default bucket only' gap is gone. What remains is narrower still and it is now INSIGHT-ONLY: nothing validates a role_key against the roster on the INSIGHT write face, so an admin or the owner can write insight under a role_key no role carries; such a document spends the insight cap and, having no role to hang off, never appears here. The LESSONS write face no longer has that gap — replace_lessons and patch_lessons refuse with 404 any role_key that nothing could read: neither a role that folds (which is what this listing walks, and what every boot loads) nor a member carrying that role_key (which cannot boot, but can be minted a token that reads the doc). A role_key on neither list now fails instead of silently producing an unreachable document. list_roles is the roster this listing is derived from — a document under a name that is not on it is not on this page either. Carries NO document text, so it costs a few hundred bytes. Use it to find which long-lived document is nearly full, then read only that one (get_role / get_insight / get_lessons / get_task_manual). It is the only way to see insight and lessons sizes in bulk — no listing reports those at any price; the manual sizes and caps are also on every list_task_manuals row, and a role definition's size and cap are already on every list_roles row.",
			MCPTool:  "peek_doc_sizes",
		},
		{
			Method:   "POST",
			Path:     "/api/roles",
			Handler:  w.HandleCreateRoleApiRolesPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Create a custom role + its founding member (one pair per call). runtime is claude/codex; absent = stored UNSET and resolved at the founding member's first placement from the host's reported capabilities, not written as claude.",
			MCPTool:  "create_role",
		},
		{
			Method:   "GET",
			Path:     "/api/roles/{role}",
			Handler:  w.HandleGetRoleApiRolesRoleGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one role definition (unknown → 404).",
			MCPTool:  "get_role",
		},
		{
			Method:   "POST",
			Path:     "/api/roles/{role}",
			Handler:  w.HandleUpdateRoleApiRolesRolePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Edit a role definition ({name?, definition_md?}; locked names skip).",
			MCPTool:  "update_role",
		},
		{
			Method:   "POST",
			Path:     "/api/roles/{role}/reset",
			Handler:  w.HandleResetRoleApiRolesRoleResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Reset a role definition to seed (idempotent tombstone overlay).",
			MCPTool:  "reset_role",
		},
		{
			Method:   "DELETE",
			Path:     "/api/roles/{role}",
			Handler:  w.HandleDeleteRoleApiRolesRoleDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Hard-delete a custom role + its members (seed → 403; online → 409).",
			MCPTool:  "delete_role",
		},
		{
			Method:  "GET",
			Path:    "/api/insight/{role_key}",
			Handler: w.HandleGetInsightApiInsightRoleKeyGet,
			Auth:    authGated,
			// T-3809. READ stays on the machine floor, matching Duty and
			// Learning: the owner ruled on 2026-08-02 (rc-dc171587220c, option
			// ①, verbatim 「包含 Insight：這一輪不關任何讀取」) that this release
			// closes nothing on the read face. Insight is SEPARATE, not
			// private — say it in every surface a reader can reach, because
			// the word "insight" implies confidentiality that nobody promised.
			Requires: principalMachine,
			Summary:  "Read a per-role insight doc - this role's accumulated judgement calls and trade-offs (per role_key). A role may ship with a factory seed, and that seed is PER-ROLE (seeds/insight_<role_key>.md) - today only the assistant has one; a role without one reads genuinely empty until it writes. is_default=true means THIS ROLE has never written its own, whether what you are reading is the factory wording or nothing at all. Separate from the lessons doc on purpose: lessons record what happened and what to do next time, insight records how this role weighs a call. Like lessons, reading is unrestricted: any authenticated identity may read ANY role's insight - it is SEPARATE, not private.",
			MCPTool:  "get_insight",
		},
		{
			Method:  "POST",
			Path:    "/api/insight/{role_key}",
			Handler: w.HandleReplaceInsightApiInsightRoleKeyPost,
			Auth:    authGated,
			// principalAgent is the honest floor, for the reason spelled out at
			// the lessons write rows below: per-ROLE authz cannot be expressed
			// by the ladder, so it lives in the handler (insightWriteAuthz) and
			// the row must not declare a floor lower than the gate it actually
			// has. A warden-kind member is ranked machine regardless of
			// role_key (classifyMember), so it cannot write insight even if it
			// carries one — the same delineation the lessons rows document.
			Requires: principalAgent,
			Summary:  "Whole-doc replace of a per-role insight doc ({text}). text is REQUIRED; unknown keys are rejected. Replacing existing content with an empty doc needs allow_shrink=true. Only the role's own agents (and admin) may WRITE it.",
			MCPTool:  "replace_insight",
		},
		{
			Method:   "POST",
			Path:     "/api/insight/{role_key}/patch",
			Handler:  w.HandlePatchInsightApiInsightRoleKeyPatchPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Patch a per-role insight doc by unique anchors ({edits:[{old,new}]}). Only the role's own agents (and admin) may WRITE it.",
			MCPTool:  "patch_insight",
		},
		{
			Method:  "POST",
			Path:    "/api/insight/{role_key}/reset",
			Handler: w.HandleResetInsightApiInsightRoleKeyResetPost,
			Auth:    authGated,
			// T-6501. Same floor as the other two insight WRITE rows, for the
			// same reason: per-ROLE authz cannot be expressed by the ladder, so
			// it lives in the handler (insightWriteAuthz) and this row must not
			// declare a floor lower than the gate it actually has. Deliberately
			// NOT reset_role's admin_agent floor — reset_role has no per-role
			// gate to fall back on, insight does, and a role's own agent may
			// already replace this document wholesale.
			Requires: principalAgent,
			Summary:  "Reset a per-role insight doc back to its factory seed (idempotent tombstone of the overlay) - the counterpart of reset_role on the Duty block. A role with NO seed file (seeds/insight_<role_key>.md) returns 404: there must be a factory version to reset TO. No length cap is applied on this path, matching reset_role - the factory text is part of the product. The overlay you are discarding is retained as a document-history revision, so the reset is recoverable. Only the role's own agents (and admin) may do it.",
			MCPTool:  "reset_insight",
		},
		{
			Method:   "GET",
			Path:     "/api/lessons/{role_key}",
			Handler:  w.HandleGetLessonsApiLessonsRoleKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read a per-role lessons doc (per role_key; overlay ⊕ seed).",
			MCPTool:  "get_lessons",
		},
		{
			Method:  "POST",
			Path:    "/api/lessons/{role_key}",
			Handler: w.HandleReplaceLessonsApiLessonsRoleKeyPost,
			Auth:    authGated,
			// T-5336: the two lessons WRITE rows sat on the machine FLOOR while
			// 100% of their RBAC lived in the handler (buildHandler skips
			// requirePrincipalClass for principalMachine) — the route table
			// declared "any authenticated principal" on rows whose real gate it
			// could not see. principalAgent is the honest floor.
			// ⚠️ THIS IS A REAL NARROWING, NOT A NO-OP — say it plainly, because
			// an earlier draft of this comment claimed the opposite and an
			// independent review disproved it by building the row. Both
			// PRODUCTION warden creation points leave role_key empty
			// (api_machines.go onboard, dbseed.go), and such a warden was
			// already 403'd by the handler's self-role compare. But a warden row
			// CAN carry a role_key: POST /api/members takes kind and role_key in
			// the SAME body and cross-checks neither (a privilege-bearing hire —
			// owner/admin only). That row's token is agent-scoped like every
			// member token, so the old self-role compare matched and it could
			// write its OWN role_key's lessons; measured across the two commits
			// that same request went 200 → 403 (classifyMember ranks
			// kind=="warden" as machine regardless of role_key). Nothing this
			// office builds for itself loses a write; a deliberately hired
			// role-bearing warden loses exactly one CAPABILITY — writing its own
			// role's lessons — which it loses across BOTH of these rows at once
			// (replace and patch share lessonsWriteAuthz), so counted as requests
			// it is two. (Whether that kind/role_key
			// combination should be refused at ingest at all is a separate
			// question, deliberately not answered here.)
			// Per-ROLE authz stays in the handler (lessonsWriteAuthz) — the
			// ladder cannot express "own role only".
			Requires: principalAgent,
			Summary:  "Replace the WHOLE per-role lessons document. text is REQUIRED and unknown keys are rejected; only that role's agent or an admin may write it; role_key must be addressable — a role that folds (list_roles), or a member carrying that role_key (list_members) — or the write is refused 404, so a lessons doc can no longer be created under a name nothing on this station could ever read; emptying or sharply shrinking it needs allow_shrink=true; and the result is still judged against the lessons cap.",
			MCPTool:  "replace_lessons",
		},
		{
			Method:  "POST",
			Path:    "/api/lessons/{role_key}/patch",
			Handler: w.HandlePatchLessonsApiLessonsRoleKeyPatchPost,
			Auth:    authGated,
			// T-5336: same honest floor as the whole-doc replace above (the two
			// share lessonsWriteAuthz). READ stays on the machine floor — any
			// authenticated identity may read any role's lessons.
			Requires: principalAgent,
			Summary:  "Patch a per-role lessons doc by unique anchors ({edits:[{old,new}]}). role_key must be addressable — a role that folds (list_roles), or a member carrying that role_key (list_members) — or the patch is refused 404.",
			MCPTool:  "patch_lessons",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary",
			Handler:  w.HandleResumeSummaryApiResumeSummaryGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Bounded LIGHT wake snapshot for the caller (identity-locked; recent chat + light open-task rows + size overview — peek sizes first, pull detail via get_task). CHAT is packed newest-first under a CHARACTER BUDGET, not a fixed message count, and stopping at the last message that still fits; each message carries from_name/to_name beside the ids and ts_display (full date + time + zone offset) beside the epoch ts, and folds in its reply card as `card` when it has one — read every ts_display against the top-level `generated_at`. TWO DIFFERENT things can be missing and they are marked DIFFERENTLY: `body_omitted_chars` > 0 means THAT message is here with that many characters COLLAPSED away (another agent's line — the owner's line and your own hand-off notes to yourself are carried in full), re-read it with get_chat; `chat_earlier_omitted` is the other kind and it is a MAYBE, not a fact: that line was cut at a read or budget limit and nothing looked past the cut, so whole messages may be missing from this payload entirely — it is raised even when there is in fact nothing older. Its hint tells you how to CHECK and fetch them. The two are asymmetric ON PURPOSE: the collapse marker is CERTAIN (that message IS here, shortened, exact count); this one is not, and only the fetch settles it. Also carries the STUDIO FLOOR you wake up onto: roster (every member and contractor, each with online/offline status, the machine it runs on, and its duty capped at 1000 chars with `…` marking a cut, the cap applied after the doc's own leading title line is removed — who to ask for help; no insight/learning by owner ruling. Contractors additionally carry their bound task's status, waiting_reason, and step progress (progress_done/progress_total) — members leave these at their zero value; a contractor's 0/0 is ambiguous (a task with no steps yet, or no task at all) and task_status is what tells them apart, non-empty vs empty) and machines (the machine list plus you_are_on, your server-recorded machine binding — never derive it from a hostname).",
			MCPTool:  "resume_summary",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary-size",
			Handler:  w.HandlePeekResumeSummarySizeApiResumeSummarySizeGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Size-only PEEK of the wake snapshot (identity-locked; overview counts/sizes + estimated_total_chars, NO chat/task content). estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars + steps_on_answered_card_chars, all five reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit, the two studio-floor blocks, and the named steps sitting on an answered card — what pulling the snapshot actually costs. Step one of the two-step boot: call this FIRST to size resume_summary, then either call resume_summary directly (small) or hand the pull to a cheap sub-agent that returns a digest (large).",
			MCPTool:  "peek_resume_summary_size",
		},
		{
			Method:     "POST",
			Path:       "/api/bootstrap",
			Handler:    w.HandleBootstrapApiBootstrapPost,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Assemble an agent boot context + mint the member JWT (spawn seam).",
			MCPExclude: true, // the credential-mint seam (like /api/login), not a tool
		},
		// ── Tasks (M3) — read face + agent state machine + owner actions ────
		// The agent write rows are the FIRST requires=agent uses: the RBAC
		// ladder places agent(1) above machine/warden(0), so a warden can
		// never write tasks; the executor guard on the report rows is the
		// handlers' (caller == executor, admin capability excepted — §14).
		{
			Method:   "GET",
			Path:     "/api/tasks",
			Handler:  w.HandleListTasksApiTasksGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List tasks (?executor=&type=&status=, or statuses=[…] for a SET of states — every filter given is ANDed; LIGHT list items — id/task_no/title/type_key/status/priority/executor/creator_id/progress/timestamps/deps + dep_tasks + current_step_id/current_step_name, WITHOUT steps/description/inputs). Ask for the states you actually want (`statuses: [\"not_started\", \"in_progress\"]`) instead of listing everything and filtering yourself — the whole history is a large answer. `statuses` also accepts \"reassigning\", which matches the handover LOCK rather than the status column. `dep_tasks` already carries each blocker's task_no/title/status, so a blocked task needs no follow-up get_task just to name what it is waiting for. `current_step_id`/`current_step_name` name the step each task is ON right now: the FIRST step in plan order that is neither done nor superseded — the same step the wake snapshot points at. BOTH ARE THE EMPTY STRING in exactly two cases — the task has no plan yet (no steps at all), or every step has finished — and that empty means THERE IS NO CURRENT STEP; never read it as \"the first step\". The two fields are that step's id and that step's name, and nothing else about the step. The list still carries NO step rows (no dod text) — only those two fields; call get_task for a task's full detail (steps, description, inputs).",
			MCPTool:  "list_tasks",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks",
			Handler:  w.HandleCreateTaskApiTasksPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Create a task (dedupes on the manual's key; ad-hoc when type_key omitted). Pass target.kind=outsource to drop the task as an unassigned outsource task (發包); target.runtime is claude/codex (absent = claude). The existing outsource scheduler then spawns workers against the global concurrency cap (outsourceParallelCap) — below the cap it starts immediately, at the cap it queues for capacity and is picked up automatically when a slot frees. No owner-approval card and no per-task approval; the owner may reassign a still-queued task at any time. Caller authorization (正職授權矩陣, T-23cf): an outsource worker may never create a task; a 發包 create is open to any 正職 (owner/admin included); a typed task the manual assigns to member X may be created only by X (owner/admin NOT exempt); an ad-hoc task with a member executor may name only the caller itself unless the caller is owner/admin (a 一般正職 may self-execute or 發包, never assign another member).",
			MCPTool:  "create_task",
		},
		{
			Method:     "GET",
			Path:       "/api/tasks/count",
			Handler:    w.HandleTaskCountApiTasksCountGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Open task count (the tasks nav badge).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		},
		{
			Method:   "GET",
			Path:     "/api/tasks/{task_id}",
			Handler:  w.HandleGetTaskApiTasksTaskIdGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one task — and read it knowing it is a SUMMARY, not the whole of it: the response says so itself (``detail_level`` = ``summary``, ``notes_included`` = false). WHAT IS COMPLETE HERE: the task's own fields, its deps, its progress counts, its gate cards, and EVERY ONE of its steps. The step list has no cap, no paging and no truncation of any kind — the rows you get back are all the rows there are, so a step that is not here does not exist on this task. WHAT IS OMITTED, AND EXACTLY HOW MUCH OF IT: each step's working-note TEXT (T-66). In its place every step carries ``note_size_chars`` — the EXACT number of characters of note sitting on the server for that step, where 0 means that step genuinely has no note — and ``note_cap_chars``, the ceiling. A positive ``note_size_chars`` is a precise promise that that many characters are waiting for you, and ``get_task_step(task_id, step_id)`` is the one call that returns them, one step at a time. Read the sizes first, then fetch only the notes you actually need. ALSO OMITTED, AND EXACTLY WHAT IS LEFT IN ITS PLACE: the ``artifacts`` rows are an INDEX of the task's pinned deliverables, not the deliverables (T-66). Every entry carries ONLY ``id`` and ``label`` — the deliverable's title, and the handle every other artifact call takes. Its ``kind``, ``url``, ``filename``, ``mime``, ``is_image``, ``attachment_id``, ``created_ts``, ``created_by`` and ``version_count`` are NOT here: ``list_task_artifacts(task_id)`` returns them, for EVERY artifact on the ticket, in ONE call — there is deliberately no per-artifact read. The response says which of the two it is: ``artifacts_detail_level`` = ``index`` here, ``full`` there. The artifact LIST itself is not abridged — every pinned deliverable has a row here, so its length is the true count. Unknown id → 404.",
			MCPTool:  "get_task",
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26). T-b56e (owner
			// 2026-08-20, card rc-b896e3f641e7 option 0) opened it further, to
			// a 正職 member acting on ITS OWN task — so the floor here is
			// principalAgent and the real gate is callerMayTerminateTask.
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/terminate",
			Handler:  w.HandleTerminateTaskApiTasksTaskIdTerminatePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Terminate a task — close it as terminated, the only status change that does not go through the task's own step reports. WHO: the owner, an admin agent, or the task's OWN executor when that executor is a 正職 member (T-b56e, owner 2026-08-20 card rc-b896e3f641e7). A member terminating SOMEONE ELSE's task is a flat 403. An OUTSOURCE worker is refused HERE even on its own task — the owner's ruling named 執行者 and did not reach the contractor lifecycle, so this door stays shut until one does. ⚠️ THAT IS A FACT ABOUT THIS ROUTE, NOT A SYSTEM-WIDE GUARANTEE that a worker cannot close its own task: mark_duplicate sits at the same principalAgent floor, gates on callerMayDriveTask with no such subtraction, and reaches the same closeTask — measured 2026-08-20, 200 duplicated. Shutting that door too needs its own ruling. Non-terminal only (already closed → 409).",
			MCPTool:  "terminate_task",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/priority",
			Handler:  w.HandleSetTaskPriorityApiTasksTaskIdPriorityPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Set a task's priority (owner/admin agent any value on any task; the task's own executor any value on their task — frozen INCLUDED, and whoever may freeze may unfreeze, T-6020). The actor who sets frozen is recorded on the task as frozen_by and the field clears when the task leaves frozen. Anyone else is a flat 403. Answers with a bounded receipt (task_id, priority, frozen_by), not the whole task — use get_task when you need the rest.",
			MCPTool:  "set_task_priority",
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// pings a task's executor. Plain agents still 403.
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/message",
			Handler:  w.HandlePostTaskMessageApiTasksTaskIdMessagePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Message the task's executor (owner/admin agent; task context auto-attached).",
			MCPTool:  "post_task_message",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/plan",
			Handler:  w.HandleSubmitTaskPlanApiTasksTaskIdPlanPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Submit/replace the workflow plan (done and answered-card steps are kept). T-74f8 交棒閘 (second door): a plan is a step-set write and the task status is DERIVED from the step set, so a plan that leaves EVERY step done CLOSES the task — the same irreversible close the final step report performs. If that task's creator is not its executor and no handover is declared or already real, the replan is refused with 422 BEFORE anything is written (the plan stays fully editable). A plan carries no handoff field, so the way out is to hand over first: create the successor task and point its ``blocked_by`` at this task (the gate then stands aside by itself), or keep one unfinished step and declare the handover on the ``update_step_status`` report that closes it. A replan that still leaves work in the plan is never gated. Answers with a bounded receipt (task_id, steps_total, progress_done, progress_total), not the plan you just sent — use get_task to read the stored step rows back.",
			MCPTool:  "submit_plan",
		},
		// T-646a: the one door onto a task's own TEXT. Supersedes the
		// update_task_description / update_task_title rows below, which stay
		// REGISTERED for the frontend and any existing HTTP client but are off
		// the MCP catalogue (x-mcp include:false), so an agent sees one tool
		// rather than three. All three share updateTaskText, so a rule corrected
		// once is corrected everywhere — the drift between two hand-kept copies
		// of the same rules is what this ticket was about. Same executor gate,
		// same closed-task editability, same document-history series as the two
		// routes it folds in. POST sits on the GET row's PATH deliberately: this
		// is a partial update of the task resource, not a fourth sub-path.
		//
		// 🔴 Its POSITION IN THIS TABLE is a wire fact, not cosmetics. The MCP
		// catalogue's element-wise order mirrors this table, and conformance
		// asserts the two agree, so this row sits where the two it folds in sit
		// — not next to the GET row it shares a path with. Moving it moves the
		// tool in tools/list.
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}",
			Handler:  w.HandleUpdateTaskApiTasksTaskIdPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Correct THIS task's own TEXT — its title, its description, or both in one write (T-646a). Replaces `update_task_title` and `update_task_description`, which documented the same rules twice and could not be applied together: changing both meant two calls, two transactions and two SSE deltas, with room for someone else's write to land in between. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or closeout. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL: only the fields you NAME are touched, so omitting a field is a legal no-op for it that versions nothing and fans nothing. ⚠️ THE TWO FIELDS TREAT AN EXPLICIT BLANK DIFFERENTLY, and that is an owner ruling rather than an inconsistency (card rc-796541192519, 2026-08-11, option ①): a blank `title` (\"\" or whitespace-only) is REFUSED with 400 and does NOT clear the field, because create_task refuses a blank title too and an edit door looser than the create door would let a caller reach a task-list row with nothing in it; a blank `description` IS accepted and DOES clear the text, because plenty of cards legitimately have no prose. VALIDATION IS WHOLE-BODY AND HAPPENS FIRST: a request carrying a blank title alongside a perfectly good description writes NEITHER — a 400 leaves the task exactly as it was, never half-applied. Both values are trimmed of surrounding whitespace before they are stored AND before they are compared with what is there, so re-sending the same text with a stray trailing space is correctly seen as no change rather than spending one of the retained revisions saying nothing moved. ⚠️ THAT HOLDS ONLY WHILE THE STORED TEXT IS ALREADY TRIMMED. Whenever the stored description carries untrimmed whitespace, the next edit here normalises it and therefore DOES spend a revision — even when you re-send exactly what you read back. TWO things can put untrimmed text in that column, so this is not a one-time settling: create_task, which never trims the description (it does trim the title), and a RESTORE of a revision that holds untrimmed text, which is written back verbatim. Before this ticket both doors stored it raw and agreed; this tool trims and create still does not, which is a divergence awaiting a ruling rather than a promise about the system. The write is wholesale within each field: send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close: artifacts record what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters a field retains the previous value as a document version — kind `task_title` / `task_description`, key = the task id — so a correction is recoverable through list_document_history and the older wording is never simply gone.",
			MCPTool:  "update_task",
		},
		// T-e271: the ticket's own TEXT is correctable after the fact. Until
		// this row existed the tool catalogue had NO way to edit an existing
		// task's description — create_task takes one only at birth, submit_plan
		// writes steps, update_task_manual writes the TYPE's manual — so a
		// ruling to reword a card had nowhere to land. Executor-guarded like
		// every other task-driving write (callerMayDriveTask §14); the CREATOR
		// gets no standing from having created it (owner ruling).
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/description",
			Handler:  w.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's description — the ticket's own text (what the task IS: scope, origin, acceptance). T-e271: until this tool existed there was NO way to change a description after creation — create_task takes one only at birth, submit_plan writes steps, update_task_manual writes the TYPE's manual — so a decision to reword a card had nowhere to land. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or closeout. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL like update_task_manual: omitting `description` changes nothing (a safe no-op), while an explicit \"\" CLEARS it — absent and empty are different on purpose; unknown keys are refused rather than dropped. The write is wholesale within that field: the value replaces whatever was there, so send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close. The reason they differ: artifacts are the record of what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters the text retains the previous one as a document version (kind `task_description`, key = the task id) — list it with list_document_history, so a correction is recoverable and the older wording is never simply gone.",
			// T-646a: folded into update_task; the ROUTE stays for the
			// frontend and existing HTTP clients, the TOOL does not.
			MCPExclude: true,
		},
		// T-2ebe: the same correctability for the ONE field the task list
		// actually shows. T-e271 gave the description a way to catch up with
		// reality and left the title frozen at its first wording, so a card
		// whose scope was later overturned went on advertising the original on
		// the list while contradicting it inside. Same executor gate, same
		// closed-task editability, same document-history series as the row above
		// — deliberately not a second answer to a question T-e271 already
		// settled. The one difference is at the door: a blank title is a 400,
		// not a clear, because create_task refuses one too (owner card
		// rc-796541192519, option ①). Kept adjacent to its twin because the MCP
		// catalogue's element-wise order mirrors this table.
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/title",
			Handler:  w.HandleUpdateTaskTitleApiTasksTaskIdTitlePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's title — the one line the task list shows. T-2ebe: until this tool existed a title could never be changed after creation, so a card whose scope was later overturned kept advertising its first wording forever — the description could correct itself, the title could not, and whoever scanned the list saw only the stale half. If you have just corrected a description because the scope moved, ask whether the title still says the same thing. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's title now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or closeout. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL like update_task_description: omitting `title` changes nothing (a safe no-op); unknown keys are refused rather than dropped. ⚠️ ONE DIFFERENCE FROM ITS DESCRIPTION TWIN: a blank title (\"\" or only whitespace) is REFUSED with 400, it does NOT clear the field — create_task refuses a blank title too, and a task with no title is a blank row on the list. Surrounding whitespace is trimmed. The write is wholesale within that field: send the full corrected title, not a fragment. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — a ticket is usually found to be worded wrongly after it closed, and freezing the text would preserve a known falsehood; its artifact set is the opposite and freezes at close. Every change that actually alters the text retains the previous one as a document version (kind `task_title`, key = the task id) — list it with list_document_history, so a correction is recoverable.",
			// T-646a: folded into update_task; the ROUTE stays for the
			// frontend and existing HTTP clients, the TOOL does not.
			MCPExclude: true,
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/duplicate",
			Handler:  w.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Mark a not-yet-terminal task duplicated, pointing at an existing final original (executor/owner). A blank original, an original that cannot be found, a self-reference, a chained duplicate and a target that is already pointed at are all refused. Closing across executors creates a handoff_follow_up, and no dependency is added.",
			MCPTool:  "mark_duplicate",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/status",
			Handler:  w.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Report a step status (pending/in_progress/waiting_external/done). Entering waiting_external requires a non-blank waiting_reason (422 otherwise); the task status is derived from its steps. T-74f8 交棒閘: if this report would CLOSE the task (every step done) AND the task's creator is not its executor, the call is REFUSED with 422 unless you say where the ball goes IN THIS SAME CALL — handoff='return_to_creator' (recorded on the task and nothing else — no task is opened and nobody is notified), handoff='follow_up' + handoff_task_id=<a successor task you already created> (the server hangs this task off it as a dependency, and closing this one releases it), or handoff='none' + handoff_note=<why nothing follows>. The gate stands aside by itself when a non-terminal task already depends on this one — you never see it if the handover is already real. It refuses BEFORE writing anything, so a refused report leaves the plan fully editable: create the successor task, then re-send this same report with the declaration. This is your LAST chance — once the task closes it can never be replanned (submit_plan becomes a permanent 409).",
			MCPTool:  "update_step_status",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/note",
			Handler:  w.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Write this step's working note: where the work stands and what comes next — the field the handover SOP means by 「把還在進行中的工作寫回 task step note」. WHAT TO WRITE — three things, then stop: (1) STATE — one sentence on where this step actually got to; (2) NEXT — one sentence on what whoever takes over does next; (3) EVIDENCE POINTERS — version ids, file and log paths, what you verified YOURSELF versus what you are taking on someone's word, and the limits of what was NOT done. Long narrative does not live here: reasoning and scope belong in the task description, reports and diffs belong on the task as artifacts. The note is the current state — not a report, not an append-only log. Writable in ANY step status (pending, in_progress, waiting_owner, waiting_external, done, superseded), unlike `waiting_reason`, which is locked to waiting_external. Wholesale write: `note` replaces whatever was there and \"\" clears it, so rewrite it as the work moves rather than appending; over 4,000 characters (counted in runes) is refused. Same executor/admin gate as every other task-driving write (403 otherwise). ⚠️ A task auto-closes when its last step is reported done and a closed task 409s — so write the note BEFORE the report that finishes the last step, not after. The receipt carries `size_chars` / `cap_chars`, so the room left is on every write instead of only on the 400 that refuses one; `get_task` reports the same pair per step as `note_size_chars` / `note_cap_chars`, but since T-66 it no longer carries the note TEXT — read a note back with `get_task_step(task_id, step_id)`, which answers that one step in full.",
			MCPTool:  "update_step_note",
		},
		{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/note/patch",
			Handler: w.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost,
			Auth:    authGated,
			// T-1667: the anchor-patch twin of the wholesale write above. Same
			// executor-or-admin gate — the handler shares it verbatim, so the two
			// faces onto one field can never disagree about who may write.
			Requires: principalAgent,
			Summary:  "Patch this step's working note by unique anchors ({edits:[{old,new}]}) — send only the part that changed, instead of re-typing the whole note. USE THIS WHENEVER YOU ARE AMENDING A NOTE THAT ALREADY HAS CONTENT. update_step_note is a wholesale replace, so if anyone else wrote to the step between your read and your write, your copy is stale and the replace silently deletes their text — and because your stale copy is usually the LONGER one, no guard fires and nothing tells you. A patch cannot do that: a non-empty old must match the current note EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400 that names which edit failed and which tool to re-read with, zero writes), so a concurrent write turns into a refusal you can see. Edits apply in order; an empty old appends. Wiping the note, or shrinking it below a tenth, needs allow_shrink=true — for an honest rewrite from scratch use update_step_note. Same executor/admin gate, same any-step-status generality, same closed-task 409 as update_step_note. Re-read with get_task_step after a refusal — get_task reports each step's note SIZE (note_size_chars) but since T-66 no longer carries its text.",
			MCPTool:  "patch_step_note",
		},
		// T-66: the READ half of the step-note split. Its POSITION here is a wire
		// fact, not tidiness — the MCP catalogue's element-wise order mirrors
		// this table (conformance asserts the two agree), so this row sits where
		// x-mcp.order 79 puts the tool: after patch_step_note, before
		// set_task_deps. Moving it moves get_task_step in tools/list.
		//
		// principalMachine, NOT principalAgent: this is a READ, and it carries
		// the same floor GET /api/tasks/{task_id} carries. The note was already
		// readable by any authenticated principal through the task view; a
		// stricter floor here would close nothing and would only make the note
		// unreachable through the tool that exists to serve it.
		{
			Method:   "GET",
			Path:     "/api/tasks/{task_id}/steps/{step_id}",
			Handler:  w.HandleGetTaskStepApiTasksTaskIdStepsStepIdGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read ONE step of one task IN FULL — the companion read to ``get_task``, which answers a SUMMARY. This response declares ``detail_level`` = ``full`` and carries that single step's ENTIRE working note (``note``) alongside its ``note_size_chars`` / ``note_cap_chars``, its status, DoD, ``waiting_reason``, gate flags, ``parallel_group``, bound ``reply_card_id`` and that card's live ``reply_card_status``. It carries NOTHING about the task itself and NOTHING about any other step, and that is the point: ``get_task`` tells you WHICH steps have a note (``note_size_chars`` > 0) and exactly how big it is, and this tool fetches one of them without dragging the whole ticket along. Same read floor as ``get_task`` — any authenticated principal may read any task's step; there is no executor gate on a READ. 404 for an unknown task, and 404 for a step id that exists but belongs to a DIFFERENT task: a step is only ever readable through its own task, so a wrong task_id never leaks somebody else's step.",
			MCPTool:  "get_task_step",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/deps",
			Handler:  w.HandleSetTaskDepsApiTasksTaskIdDepsPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Replace the blocking-deps list wholesale.",
			MCPTool:  "set_task_deps",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/closeout",
			Handler:  w.HandleReportTaskCloseoutApiTasksTaskIdCloseoutPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Report the task's close-out follow-ups done (terminal tasks only; idempotent).",
			MCPTool:  "report_task_closeout",
		},
		{
			// ② opened to agent (was admin_agent): an agent reassigns/hands over a
			// task it EXECUTES (handler executor-guard, callerMayDriveTask §14);
			// owner/admin still drive any task. An outsource target still funnels
			// through the single 發包 gate (create+spawn atomicity / owner approval).
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/reassign",
			Handler:  w.HandleReassignTaskApiTasksTaskIdReassignPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Reassign a task to a member or a fresh outsource worker (executor-guarded: a plain agent may reassign only a task it executes; owner/admin drive any task). Caller authorization (正職授權矩陣, T-23cf): owner/admin may hand a task to any active member or 發包 it to a fresh outsource worker; a 一般正職 may only turn its own task into a 發包 (a member target is 403); an outsource worker may not reassign at all. An outsource target uses target.runtime claude/codex (absent = claude), lands the task unassigned for the scheduler to spawn under the global parallel cap, and enters the reassigning handover state.",
			MCPTool:  "reassign_task",
		},
		{
			// T-9ca5: the NEW executor takes over a reassigned task — clears the
			// reassigning LOCK and fires the predecessor worker (the takeover the
			// retired task-status report used to perform on the successor's
			// reassigning→in_progress before reassigning became a lock).
			// Executor-guarded (callerMayDriveTask §14); status stays derived,
			// never set here.
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/claim",
			Handler:  w.HandleClaimTaskApiTasksTaskIdClaimPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Take over a reassigned task (the new executor claims it): clears the reassigning lock and fires the predecessor worker. The task status stays derived from its steps; only the lock is cleared. 409 if the task is not under the reassigning lock.",
			MCPTool:  "claim_task",
		},
		{
			// The executing agent pins deliverables onto its own task card
			// (requires=agent; the handler's executor guard — caller == executor,
			// admin capability excepted — §14, same as the other agent write rows).
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/artifact",
			Handler:  w.HandleAddTaskArtifactApiTasksTaskIdArtifactPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Register a deliverable (file, image, or link) onto the task's artifact set — the pinned deliverables shown on the task card. This verb only ADDS, and is repeatable: call it again to pin one more. To change what an ALREADY-PINNED deliverable points at, use replace_task_artifact instead of remove+add: it keeps the artifact id. For a file or image, first upload the bytes via the chat-attachments upload to get an attachment id, then call this with kind=file|image and that attachment_id. For a link (e.g. a PR url) call it with kind=link and url — no upload needed. label is an optional display name (a link title such as \"PR #123\"), capped at 128 characters — Unicode runes, so 128 CJK characters fit; a longer label is refused with a 400, never truncated. Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
			MCPTool:  "add_task_artifact",
		},
		{
			// Un-pin — SAME permission model as add (owner ruling 2026-07-18
			// "Agent 自己應該也要可以刪除"): requires=agent + the handler's executor
			// guard (caller == executor, admin/owner excepted — §14). The agent
			// drives it through the remove_task_artifact tool; the owner through
			// the cockpit popover.
			Method:   "DELETE",
			Path:     "/api/tasks/{task_id}/artifact/{artifact_id}",
			Handler:  w.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Un-pin (remove) one artifact from a task's artifact set — the counterpart to add_task_artifact. You may remove artifacts from a task you are the executor of (the owner/assistant may remove on any task). Give the task id and the artifact id (the id returned when it was added, or from get_task's artifacts). The LIVE file blob is left intact, and on an artifact that was never replaced only the pin on the card is removed. BUT IF YOU HAD REPLACED IT, un-pinning also destroys its past: every retained version of this artifact is deleted in the same breath, and the files only those versions pointed at go with them, unrecoverably. ONLY WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen in every direction — remove is refused with the same 409 as add and replace. So swap a deliverable BEFORE you close the task, not after; after the close it can neither be removed nor put back. Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
			MCPTool:  "remove_task_artifact",
		},
		{
			// T-66 (owner c-cd063427fb2f / c-f2d0fecb1168): the full-artifact read
			// the shared task projection stopped carrying. It sits here, directly
			// after the two artifact WRITES, because x-mcp.order must be the
			// consecutive range and conformance asserts this table agrees with it —
			// moving this row moves list_task_artifacts in tools/list.
			//
			// principalMachine, NOT principalAgent: this is a READ carrying the same
			// floor GET /api/tasks/{task_id} carries, and every field it serves rode
			// that response until this ticket. A stricter floor here would close
			// nothing and would only make the artifacts unreachable through the tool
			// that exists to serve them.
			Method:   "GET",
			Path:     "/api/tasks/{task_id}/artifacts",
			Handler:  w.HandleListTaskArtifactsApiTasksTaskIdArtifactsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one task's pinned deliverables IN FULL — the companion read to ``get_task``, whose ``artifacts`` rows carry only ``id`` and ``label``. Answers ``{task_id, artifacts_detail_level, artifacts}`` where ``artifacts_detail_level`` is ``full`` (against the task view's ``index``) and every artifact on the task is present, oldest→newest, complete: ``kind`` (file|image|link), ``url`` (the blob serve path for a file/image, the external link for a link), ``label``, ``filename``, ``mime``, ``is_image``, ``attachment_id``, ``created_ts``, ``created_by`` and ``version_count``. ONE call answers the WHOLE ticket, and that is deliberate — there is no per-artifact read, because whoever opens a task's deliverables wants the set (a 32-artifact ticket would otherwise cost 32 calls), whereas a step note is read one at a time and ``get_task_step`` is per-step for exactly that reason. File/image metadata is resolved read-time and is honest-empty when the underlying blob is gone — never fabricated. A task with nothing pinned answers ``artifacts: []``, not a 404; an unknown task id is a 404. Same read floor as ``get_task``: any authenticated principal may read any task's artifacts, and no field here was behind a stricter door before.",
			MCPTool:  "list_task_artifacts",
		},
		{
			// T-60 replace — the THIRD verb on the same set, so it carries the
			// same permission model as add and remove (requires=agent + the
			// handler's executor guard, admin/owner excepted) and the same
			// terminal-task freeze. A replace verb without that 409 would be the
			// freeze's back door: the content behind a frozen deliverable could
			// be swapped for anything while the card claimed nothing had moved.
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/artifact/{artifact_id}/replace",
			Handler:  w.HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Replace the CONTENT of one already-pinned deliverable while its artifact id stays exactly the same — the card keeps pointing at the same artifact and what sits behind it changes. Use this instead of remove+add whenever you are shipping a corrected version of something you already pinned: remove+add mints a NEW id, so anyone holding the old one is left pointing at nothing. Give the task id, the artifact id and the replacement — attachment_id for a file/image artifact (upload the bytes first via the chat-attachments upload), url for a link artifact; label is optional: omit it and the deliverable KEEPS the label it already has — you never have to re-type the display name just to swap the content — send one to replace it, send an explicit blank to clear it. THE KIND CANNOT CHANGE ACROSS VERSIONS: a file artifact stays a file artifact, so sending a url for one (or an attachment_id for a link, or an explicit kind that differs from what is pinned) is a 400 — un-pin it and register a new artifact if the kind is what you meant to change. The version you replaced is KEPT and readable, but only the most recent few are retained: the oldest falls off the end for good when a newer one arrives, and the file it pointed at is deleted with it, so a version that has scrolled off is not recoverable from anywhere. ONLY WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen in every direction — replace is refused with the same 409 as add and remove, and admin/owner are not exempt. Answers with a bounded receipt (task_id, artifact_id, artifact_count, version_count), not the whole task.",
			MCPTool:  "replace_task_artifact",
		},
		{
			// The version list behind the cockpit's artifact popover. MCPExclude
			// by decision (T-60): the agent that replaced a deliverable already
			// knows what it replaced, and the reader this list exists for is the
			// human looking at the card. This route runs artifactRead, so it
			// carries NEITHER the writes' executor guard NOR their terminal-task
			// 409: any caller who can read the task can read what its
			// deliverables used to be. That asymmetry is deliberate (owner
			// ruling) and is argued out at artifactOnTask in api_tasks.go — read
			// that comment before "fixing" this door to match the writes.
			Method:     "GET",
			Path:       "/api/tasks/{task_id}/artifact/{artifact_id}/history",
			Handler:    w.HandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet,
			Auth:       authGated,
			Requires:   principalAgent,
			Summary:    "List the retained previous versions of one pinned deliverable, newest first — what it pointed at before each replace. Read-only, cockpit-only, and only the most recent few are kept.",
			MCPExclude: true,
		},
		// T-4595: GET /api/self/task (get_my_task) is RETIRED — see the note in
		// api_tasks.go. A worker reads its task through get_task like everyone
		// else, and reports its wake through report_waking like everyone else.
		// ── Outsource panel (M3) ─────────────────────────────────────────────
		{
			Method:   "GET",
			Path:     "/api/outsource-workers",
			Handler:  w.HandleListOutsourceWorkersApiOutsourceWorkersGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List live outsource workers (codename, model, effort, task).",
			MCPTool:  "list_outsource_workers",
		},
		{
			// T-f190: single-worker read for the detail panel's post-relocate
			// refresh. A cockpit read face, not an agent tool → MCPExclude.
			Method:     "GET",
			Path:       "/api/outsource-workers/{id}",
			Handler:    w.HandleGetOutsourceWorkerApiOutsourceWorkersIdGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Read one outsource worker by id (detail-panel refresh).",
			MCPExclude: true,
		},
		{
			// T-ba6b: the detail panel's initial-prompt preview — a live
			// re-assembly of the worker boot context (the member /api/bootstrap
			// preview's worker twin; no token minted). The text embeds the full
			// task + manual, so the floor is admin_agent, never plain agent.
			// T-6020 (owner 2026-07-26) dropped it from owner-only.
			Method:   "GET",
			Path:     "/api/outsource-workers/{id}/boot-context",
			Handler:  w.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Read an outsource worker's boot-context preview (owner/admin agent).",
			MCPTool:  "get_outsource_worker_boot_context",
		},
		{
			// T-f190 改機器; P7c (gate rc-2786636f30e5) drops the floor to
			// admin_agent — 外包對齊正職, the exact member relocate floor, so an
			// admin 助理 can move a worker too. STAYS MCPExclude on purpose: the
			// MCP channel is the EXISTING relocate_member tool (its handler falls
			// through to the worker table for an ow-… id), so no worker-specific
			// tool grows here (P7d 合表後此 route 自然消失) and the catalog hash
			// (non-exclude METHOD+path set) is unchanged.
			Method:     "POST",
			Path:       "/api/outsource-workers/{id}/relocate",
			Handler:    w.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost,
			Auth:       authGated,
			Requires:   principalAdminAgent,
			Summary:    "Relocate an outsource worker to a machine (admin-gated).",
			MCPExclude: true,
		},
		{
			// T-32e1/T-f190 worker lifecycle ops — owner mental model "外包只是
			// 系統會幫我產生跟刪除的正職員工", so each reuses a member mechanism.
			// T-6020 (owner 2026-07-26) put FOUR of them at the SAME admin_agent
			// floor relocate already had in P7c — 外包對齊正職, one floor for the
			// worker lifecycle. Plain agents remain 403 on those.
			//
			// ⚠️ "all four" is what this note used to say, and since T-ed79 it is
			// FALSE: /model left that floor (owner 2026-08-21, rc-376a41719e62 —
			// the full ruling is on that row below). THREE of the T-6020 four are
			// still here: refocus, stop, restart. /relocate is the fifth worker
			// lifecycle row and it sits at admin_agent too, but it got there in
			// P7c, not from this ruling.
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/refocus",
			Handler:  w.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Refocus (換手) an outsource worker (owner/admin agent). Needs a live session, 409 otherwise — EXCEPT on a worker whose stop is in flight or has landed, where it answers 200 and QUEUES the restart (restart_after_stop); the stop itself is honoured as-is. A worker nobody ever asked to stop is still a 409.",
			MCPTool:  "refocus_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/stop",
			Handler:  w.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Stop (停止) an outsource worker: ask it to work its 〈停止〉 document and wait for its own report_stopped -- no kill, no deadline (owner/admin agent).",
			MCPTool:  "stop_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/restart",
			Handler:  w.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restart (重啟) an outsource worker (owner/admin agent; a live worker is displaced, not refused).",
			MCPTool:  "restart_outsource_worker",
		},
		// ⚠️ set_outsource_worker_model sits at the machine FLOOR since T-ed79,
		// and it is the ONE T-6020 row that left the admin_agent floor. owner
		// 2026-08-21 (rc-376a41719e62) was asked whether changing a worker's
		// model is governance, and ruled, VERBATIM:
		//
		//	「如果原本正職可以改 model 外包就應該可以改，如果只有 mira 可以改，那就
		//	 不變，正職跟外包一樣，mira 是特殊的意義，他代替 owner 執行高權限動作。」
		//
		// So the test is not "how dangerous does this look" — it is "what floor
		// does the STAFF face of the same act sit at". PATCH /api/members/{id}
		// (update_member) is at principalMachine and was itself examined and KEPT
		// there by owner 2026-07-27 (T-5336, the note above that row). Changing a
		// model is therefore office housekeeping on BOTH sides, and mira's
		// admin_agent rank is reserved for acts the owner delegates, which this
		// is not.
		//
		// 🔴 ONLY THIS ROW MOVED. refocus / relocate / stop / restart were already
		// at the same floor as their staff twins before this ruling, and the
		// ruling did not touch them — do not "finish the job" by lowering them.
		//
		// This note exists so the NEXT permission audit does not re-open the
		// question, the way this one had to re-open T-5336's. Raising this row
		// needs a fresh owner ruling, not a tidy-up commit.
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/model",
			Handler:  w.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Change (換 model) an outsource worker's model/effort (same floor as the staff model edit). On a worker whose stop is IN FLIGHT OR HAS LANDED it ALSO queues the restart (restart_after_stop), so the worker comes back up ON THE NEW MODEL once the stop converges — an edit is no longer only a save. A worker nobody ever asked to stop is still only persisted.",
			MCPTool:  "set_outsource_worker_model",
		},
		// ── Task manuals (M3) — agents create manuals + edit the CONTENT fields
		// (purpose / fields / SOP / learnings); the assignee face and delete are
		// GOVERNANCE, floor admin_agent since T-6020 (owner 2026-07-26; the
		// in-handler assignee gate answers 403 below that floor)
		{
			Method:   "GET",
			Path:     "/api/task-manuals",
			Handler:  w.HandleListTaskManualsApiTaskManualsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List task types WITHOUT their long documents: each row is the type identity (type_key / display_name / purpose), its input fields and its assignee setting, plus the SIZES of sop_md and learnings and the cap each is judged against. The SOP and the learnings text are not on this answer at all — read the one type you picked with get_task_manual.",
			MCPTool:  "list_task_manuals",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals",
			Handler:  w.HandleCreateTaskManualApiTaskManualsPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner/admin agent). An outsource assignee may select runtime claude/codex; absent = claude.",
			MCPTool:  "create_task_manual",
		},
		{
			Method:   "GET",
			Path:     "/api/task-manuals/{type_key}",
			Handler:  w.HandleGetTaskManualApiTaskManualsTypeKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one task manual (purpose/fields/SOP/learnings/assignee). The SOP and the learnings are judged by two SEPARATE caps: read sop_md_cap_chars and learnings_cap_chars. The older cap_chars is DEPRECATED — it carries the LEARNINGS cap only and says nothing about sop_md, so read sop_md_cap_chars for the SOP.",
			MCPTool:  "get_task_manual",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals/{type_key}",
			Handler:  w.HandleUpdateTaskManualApiTaskManualsTypeKeyPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Edit a task manual (partial; content fields agent-editable; assignee = owner/admin agent). An outsource assignee may select runtime claude/codex; absent = claude. Only the fields you name change, so omitting a field is safe — but unknown keys are rejected rather than dropped: the learnings doc goes in learnings (NOT text — that is write_task_learnings' field name). The SOP and the learnings are judged by two SEPARATE caps: read sop_md_cap_chars and learnings_cap_chars. The older cap_chars is DEPRECATED — it carries the LEARNINGS cap only and says nothing about sop_md, so read sop_md_cap_chars for the SOP.",
			MCPTool:  "update_task_manual",
		},
		{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:   "DELETE",
			Path:     "/api/task-manuals/{type_key}",
			Handler:  w.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Delete a task type (open tasks of the type → 409).",
			MCPTool:  "delete_task_manual",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals/{type_key}/learnings",
			Handler:  w.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Whole-doc replace of a type's learnings (task-close write-back). The doc text goes in text (NOT learnings — that is update_task_manual's field name); text is REQUIRED and unknown keys are rejected. Wiping existing learnings needs allow_shrink=true.",
			MCPTool:  "write_task_learnings",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals/{type_key}/learnings/patch",
			Handler:  w.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Patch a type's learnings by unique anchors ({edits:[{old,new}]}) — the learnings twin of patch_lessons, so the write cost scales with the CHANGE, not the whole (30k-char) doc, and re-typing the whole doc can no longer silently drop content. Edits apply in order; a non-empty old must match the current learnings EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400, zero writes — the unique anchor also acts as an optimistic lock); an empty old appends. Wiping the doc, or shrinking it below a tenth, needs allow_shrink=true.",
			MCPTool:  "patch_task_learnings",
		},
		{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}/sop/patch",
			Handler: w.HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost,
			Auth:    authGated,
			// T-1667: the anchor-patch twin of update_task_manual's sop_md field.
			// Same agent floor as every other manual CONTENT face; assignee (the
			// one governance field) is not reachable from here at all.
			Requires: principalAgent,
			Summary:  "Patch a type's SOP (sop_md) by unique anchors ({edits:[{old,new}]}) — send only the section that changed, instead of re-typing the whole SOP. USE THIS WHENEVER YOU ARE AMENDING AN SOP THAT ALREADY HAS CONTENT. update_task_manual{sop_md} is a wholesale replace, so if anyone else edited the SOP between your read and your write, your copy is stale and the replace silently deletes their section — and because your stale copy is usually the LONGER one, no guard fires and nothing tells you. A patch cannot do that: a non-empty old must match the current sop_md EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400 that names which edit failed and which tool to re-read with, zero writes), so a concurrent write turns into a refusal you can see. Edits apply in order; an empty old appends. Wiping the doc, or shrinking it below a tenth, needs allow_shrink=true — for an honest rewrite from scratch use update_task_manual. The sop_md cap is judged on the RESULT and allow_shrink is not a bypass. Re-read with get_task_manual after a refusal.",
			MCPTool:  "patch_task_sop",
		},
		// ── Retained history of the editable documents above ────────────────
		// One read + one restore for EVERY overwritable long-form document
		// (global context, role definition, lessons, task manual), which is why
		// they sit after the last of those write faces instead of inside any one
		// group. Restore is a write, so it takes the agent floor.
		{
			Method:   "GET",
			Path:     "/api/document-history/{kind}/{key}",
			Handler:  w.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "READ the CATALOGUE of retained versions of one editable document: which versions exist, when each was replaced and by whom, whether each was a tombstone, and HOW LONG each of its fields was. It does NOT carry the versions themselves — a version list is how you CHOOSE one, and choosing does not need the prose; fetch the one you picked with get_document_version. Read-only, newest first, and only the most recent few are kept — HOW MANY is per-document and is not stated here, because it differs by kind and this sentence would go stale silently; what you get back is the answer. Putting a version BACK is deliberately not an agent tool — the owner does that from the cockpit — so this cannot change anything.\n\nWHICH DOCUMENTS THIS COVERS, AND WHAT `key` LOOKS LIKE FOR EACH, ARE DELIBERATELY NOT LISTED HERE. A list of kinds — or of key shapes — written into a description goes stale the moment a new editable document ships, and NOTHING turns red when it does: this description used to enumerate six kinds and a key shape per kind, and both had already gone stale before the lists were taken out. Two rules you can actually execute replace them.\n\nADDRESSING: `kind` and `key` are validated by the same server-side gate that answers get_document_seed, so whatever that tool can address, this one can too, and the two can never silently disagree. A `kind` this server does not know is refused with 400; a retired kind is refused with 400 naming the series that replaced it. Some kinds also police the shape of `key` before answering — a key this kind does not serve, or one that fails that kind's required shape, is refused with 400 naming the problem. Neither is something to guess at: ask and read the answer.\n\nCOVERAGE: a syntactically valid `key` that simply has no retained versions yet is not an error — it returns an empty list, the honest 'nothing has been saved here', not a gap to work around.",
			MCPTool:  "list_document_history",
		},
		{
			Method:   "GET",
			Path:     "/api/document-history/{kind}/{key}/seed",
			Handler:  w.HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "READ the SHIPPED DEFAULT of one editable document — the text a reset would put back, i.e. the 初始版本 entry of that document's version list. Read-only: this tool writes nothing, so reading the default can never replace the live document. Putting the default BACK is deliberately not an agent tool — the owner does that from the cockpit — exactly as with list_document_history. ``content`` carries the SAME field names a retained version carries, so the same reader can compare a default against the live document.\n\nWHICH DOCUMENTS THIS COVERS IS DELIBERATELY NOT LISTED HERE. A list of kinds written into a description goes stale the moment a new editable document ships and NOTHING turns red when it does — this one had gone wrong about three kinds before the list was taken out. Two rules you can actually execute replace it.\n\nADDRESSING: ``kind`` and ``key`` name a document exactly as they do for list_document_history — the same server-side gate answers both routes, so whatever that tool addresses is addressable here, and a ``kind`` this server does not know is refused with 400 while a ``key`` that names no document of that kind is refused with 404 that names it. Neither is something to guess at: ask and read the answer.\n\nCOVERAGE: whether THAT document ships a default is answered by asking for it. 200 means it does, and ``content`` is that text. 404 means it has none at all — a role the owner created, a task manual, per-role lessons — which is the same set whose reset the server also 404s, so it is the honest 'there is nothing to go back to', not a gap to work around. 400 on a retired kind names the series that replaced it.",
			// A TOOL, by owner ruling rc-b7d29de0eb9c ("開放,照你 7/30 那句話
			// 一律給"). This row first landed MCPExclude, argued from "an agent
			// gains nothing here" — a role definition's seed is the very text
			// boot injects into that role's persona. The owner overruled that
			// against the SAME 2026-07-30 policy the restore row below cites
			// (rc-b5fd1135e2dd): reading is an agent tool, writing one back is
			// not. The split is the VERB, and the policy deliberately does not
			// re-litigate per route how much each read is worth — an exclusion
			// argued from "not useful enough" would put that vote back.
			// Nothing about the FLOOR moves: reading stays at the sibling
			// list_document_history's level (machine), and this row has no
			// write verb to open — restore and reset keep their own gates.
			MCPTool: "get_document_seed",
		},
		{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}/{id}",
			Handler: w.HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet,
			Auth:    authGated,
			// Same floor as the two reads above, and for the same reason: this
			// is the BODY of a version the sibling listing already names. The
			// listing stopped carrying the prose (a single answer had a
			// structural ceiling in the hundreds of thousands of characters),
			// so this row is what makes the retained text reachable at all —
			// one named revision at a time. Raising the floor here would put
			// the text behind a gate the catalogue that advertises it is not
			// behind, which is the shape that makes a listing useless.
			Requires: principalMachine,
			Summary:  "READ the BODY of one named retained version of an editable document — the ``content`` map that version was stored with, exactly as it was stored. Read-only: this fetches text, it never puts it back; restoring stays out of the agent tool surface, as it does for list_document_history.\n\nTHIS IS THE SECOND HALF OF A PAIR. list_document_history answers WHICH versions exist and how big each field of each one is, and carries no prose at all; this answers WHAT ONE OF THEM SAID. Name the ``id`` you read off that list. Asking for every version's text is the cost that pairing exists to remove, so fetch the one you actually mean to read.\n\nADDRESSING: ``kind`` and ``key`` name a document exactly as they do for list_document_history — the same server-side gate answers all three routes, so whatever that tool can address, this one can too, and they can never silently disagree. A ``kind`` this server does not know is refused with 400; a retired kind is refused with 400 naming the series that replaced it; a ``key`` that fails its kind's required shape is refused with 400 naming the problem. An ``id`` that is not a retained version of THAT document is a 404 — including an id that belongs to some other document, which is why the address is the whole triple and not the id alone.",
			// A TOOL, on the same owner ruling the seed row cites
			// (rc-b7d29de0eb9c, and the 2026-07-30 policy rc-b5fd1135e2dd
			// behind it): READING a document's history is an agent tool,
			// writing one back is not. The split is the VERB. Restore below
			// keeps its MCPExclude.
			MCPTool: "get_document_version",
		},
		{
			Method:   "POST",
			Path:     "/api/document-history/{kind}/{key}/{id}/restore",
			Handler:  w.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Restore one retained document version as a new write.",
			// owner ruling 2026-07-30 (rc-b5fd1135e2dd, option 1): READING the
			// history is an agent tool; WRITING one back is not. Restoring is
			// how a document returns to an earlier state, and that is the
			// owner's call from the cockpit (or an assistant's), not something
			// an agent reaches for on its own. The route itself is unchanged —
			// the cockpit and the governance path still call it over REST.
			MCPExclude: true,
		},
		// ── Product guide (docs/guide embed) — one source, three consumers ───
		// The 座艙's 使用說明 nav tab renders these; the machine-floor read
		// tools let an assistant agent read the same bytes to answer feature /
		// field questions (get_global_context's flag — assistant classifies as
		// admin_agent ≥ machine, so it can call them). The asset route serves the
		// referenced images and is not a callable tool.
		{
			Method:   "GET",
			Path:     "/api/docs",
			Handler:  w.HandleListDocsApiDocsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List the product-guide docs (slug + title).",
			MCPTool:  "list_docs",
		},
		{
			Method:   "GET",
			Path:     "/api/docs/{slug}",
			Handler:  w.HandleGetDocApiDocsSlugGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one product-guide doc in full (markdown; unknown slug → 404).",
			MCPTool:  "get_doc",
		},
		{
			Method:     "GET",
			Path:       "/api/docs/assets/{name}",
			Handler:    w.HandleGetDocAssetApiDocsAssetsNameGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Serve a product-guide image asset (referenced by a doc's markdown).",
			MCPExclude: true, // a binary image, not a callable tool
		},
		// 🔴 PLACED LAST ON PURPOSE, and not next to /api/theme/fetch where they
		// read better. The MCP tool surface has ONE order shared by three files:
		// this table, spec/openapi.json's x-mcp.order, and
		// conformance/routes_manifest.json — tools/list is served from the frozen
		// catalog and conformance asserts all three agree element-wise. x-mcp.order
		// must also be the consecutive range 0..N-1, so inserting a tool in the
		// middle renumbers every tool after it. Appending costs four numbers;
		// grouping them by subject would have cost a hundred.
		// ── Custom themes (T-83ef) — themes used to ride GET/PATCH /api/settings
		// as one custom_themes array, so "change one theme" meant re-sending every
		// theme with every embedded image. These four give them their own door and
		// make the unit of work ONE theme. Floor is admin_agent: the same floor the
		// settings write they replace carried, so nothing gained or lost authority
		// in the move.
		//
		// 🔴 THEY ARE ON THE MCP SURFACE BY AN OWNER RULING (rc-32ed1bfba080,
		// 2026-08-18), against the recommendation on that card. The card proposed
		// MCPExclude for all four — the list read is the several-hundred-kilobyte
		// payload this ticket exists to take away from agents, and a write with no
		// read is only a blind overwrite. He chose to keep themes usable by AI
		// members instead, and that is the decision of record. What was reported to
		// him alongside it, so it is not rediscovered as a surprise: a bundle
		// carries its images, so a list of BUNDLES would have been the same order
		// of magnitude as the `get_settings` payload the tool layer already
		// refuses today. Splitting themes out of settings fixed SETTINGS; it did
		// not make themes small.
		//
		// 🔴 AND THAT REPORT IS WHY THE LIST BELOW IS `{id, name}` ONLY. He
		// answered it the same day, in chat: list everything with just the title
		// and whatever else the UI actually shows. So the metadata-only shape is
		// not a possible future fix for a payload problem — it is the ruling that
		// made ruling #1 usable, and the two must be read together. A later reader
		// who "restores" whole bundles here to make the list richer would be
		// undoing the half that made agents able to call it at all.
		{
			Method:   "GET",
			Path:     "/api/themes",
			Handler:  w.HandleListThemesApiThemesGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "List the saved custom themes — id and name only, in list order (owner/admin agent).",
			MCPTool:  "list_themes",
		},
		{
			Method:   "GET",
			Path:     "/api/themes/{theme_id}",
			Handler:  w.HandleGetThemeApiThemesThemeIdGet,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Read one saved custom theme (unknown id → 404).",
			MCPTool:  "get_theme",
		},
		{
			Method:   http.MethodPut,
			Path:     "/api/themes/{theme_id}",
			Handler:  w.HandlePutThemeApiThemesThemeIdPut,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Create or replace ONE custom theme; the bundle's id must match the path (owner/admin agent).",
			MCPTool:  "put_theme",
		},
		{
			Method:   "DELETE",
			Path:     "/api/themes/{theme_id}",
			Handler:  w.HandleDeleteThemeApiThemesThemeIdDelete,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  `Delete one custom theme; deleting the active one resets display_theme to "".`,
			MCPTool:  "delete_theme",
		},
		// 🔴 APPENDED HERE, NOT BESIDE THE VERBS THEY ESCALATE (T-ed79). They read
		// better next to /force-stop and /stop, and that is exactly where the first
		// version of them was — which put accelerated_stop_member at route position
		// 13 while its x-mcp.order was 118, and broke the ONE order shared by this
		// table, spec/openapi.json's x-mcp.order and conformance/routes_manifest.json
		// (test_tools_list_equals_frozen_snapshot_elementwise +
		// test_catalog_hash_keys_off_tool_surface_only both go red on it, measured).
		// The rule is stated in full at the custom-themes block above: x-mcp.order
		// must be the consecutive range 0..N-1, so a NEW tool is appended or every
		// tool after it is renumbered. The escalation ladder is a reading order for
		// the OWNER, and it lives in the cockpit row (MemberActionButtons), not here.
		{
			Method:   "POST",
			Path:     "/api/members/{member_id}/accelerated-stop",
			Handler:  w.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "加速停止: put an ALREADY-OPEN wind-down on the stop.accelerated_grace_secs clock and tell the member. 409 if nothing is winding down -- press 停止 first. Middle rung of 停止 -> 加速停止 -> 強制停止.",
			MCPTool:  "accelerated_stop_member",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/accelerated-stop",
			Handler:  w.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "加速停止 an outsource worker: put its ALREADY-OPEN wind-down (a 停止 or a 換手) on the stop.accelerated_grace_secs clock and tell it. 409 if none is open.",
			MCPTool:  "accelerated_stop_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/force-stop",
			Handler:  w.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "強制停止 an outsource worker: kill the session NOW and hold it down; says nothing to it. Third rung of 停止 -> 加速停止 -> 強制停止.",
			MCPTool:  "force_stop_outsource_worker",
		},
		// ── T-33 lore 對象審核 (entity review) ────────────────────────────────
		// 🔴 THESE THREE ROWS ARE THE EXIT FROM A QUEUE THAT HAD NONE. Every
		// subject key an agent writes and nothing recognises is MINTED and parked
		// `pending = 1` — deliberately, because gating the write is what pushes an
		// agent into forcing a near-miss key onto an existing subject. The parked
		// column was written on every such write and READ by nothing: the boot
		// subject directory filters `pending = 0`, so an entry filed only against a
		// pending subject exists, answers a direct read, and is invisible to every
		// agent's wake. That is the ticket's own disease, one layer down.
		//
		// 🔴 THE FLOOR IS principalAdminAgent ON ALL THREE, AND ON THE TWO ACTS IT
		// IS THE OWNER'S OWN WORDS (rc-139a5ab99a19): 「待審，我跟 mira 有 admin
		// 權限的才行」. Approving publishes a name into EVERY agent's boot
		// directory and merging rewrites which subject an entry belongs to; neither
		// is an agent curating what it knows, which is what put retire at
		// principalAgent. The owner outranks admin_agent on the ladder, so one row
		// says both halves of his sentence.
		//
		// ⚠️ THE READ IS HELD TO THE SAME FLOOR, AND THAT HALF IS A READING RATHER
		// THAN HIS WORDS — the precedent it follows is GET /api/settings, whose
		// read sits at principalAdminAgent beside the PATCH that edits it (and GET
		// /api/members/{member_id}/scheduled-messages, same shape). This station
		// puts an admin-gated console's READ at the console's floor, not below it.
		// The lore retrieval rows are NOT the precedent: they serve an agent its
		// own store, whereas this list is a work queue whose every row names an act
		// only an admin may perform. ⚠️ If that reading is wrong the cost is one
		// 403 an ordinary agent can argue with; the reverse — the whole fleet
		// reading a queue of names nobody has approved — is the thing the `pending`
		// filter on the boot directory exists to prevent.
		//
		// 🔴 THERE IS NO REJECT ROW, AND ITS ABSENCE IS DELIBERATE. The owner has
		// ruled on 核可 and on 合併. What becomes of a minted name nobody wants has
		// never been decided, and shipping that exit would decide it here — in the
		// direction that destroys rows.
		{
			Method:    "GET",
			Path:      "/api/lore/entities/pending",
			LoreGated: true,
			Handler:   w.HandleListPendingLoreEntitiesApiLoreEntitiesPendingGet,
			Auth:      authGated,
			Requires:  principalAdminAgent,
			Summary:   "List the subject entities parked for review, each with the homework already done — the `type:name` key it was minted under, its type, its name, when it was created, WHO MINTED IT (`created_by`), HOW MANY lore entries are filed under it (`entries`) and how many were EVER filed under it including retired ones (`entries_ever`), EVERY one of those entries by id and `heading` (`entry_refs`), a SAMPLE of the first one's `content` (`content`; the wire field is still called `sample_short`), and the existing subjects it resembles WITH the reason each was offered. 🔴 A pending entity is a name an agent INVENTED while writing lore: minting is deliberately ungated (gating it is what pushes a writer into forcing a near-miss key onto an existing subject), so this queue is the only place a typo like `repo:offcraft` is caught before it becomes part of the ontology. `entries` is counted with the SAME predicate the boot subject directory and `search_lore_entries` use — retired entries are not counted — and `entry_refs` is exactly the list that count counted. 🔴 `entries: 0` MEANS TWO OPPOSITE THINGS AND `entries_ever` IS WHAT SEPARATES THEM: `0`/`0` is a name minted once and never used again, which is the shape of a typo the writer corrected on its next attempt; `0`/`2` is a name that was genuinely used and has since been emptied by retirement, which says nothing about the name at all. 🔴 THIS QUEUE OFFERS EVIDENCE AND NO VERDICT. It also carried `suggestion` / `merge_target` — a mechanical rule's answer to 「which button should I press」 — until the owner removed them on 2026-09-05: that rule's strongest signal was two names differing only in case, full/half width or `_`/`-`, and the writers minting these names do not in fact make that mistake. A judgement that EXPLAINS ITSELF and that a reviewer can send back for another pass replaces it in a later ticket; until then, reading `similar`, `entries`, `entries_ever`, `entry_refs` and `created_by` is the reviewer's own job, which is what they are all there for. Nothing here approves or merges anything — both acts stay behind the owner/admin floor and the verdict is the reviewer's. 🔴 A pending entity is INVISIBLE to the boot subject directory until it is approved, so a queue nobody works is a set of lore entries no agent can reach by subject.",
			MCPTool:   "list_pending_lore_entities",
		},
		{
			Method:    "POST",
			Path:      "/api/lore/entities/{entity_id}/approve",
			LoreGated: true,
			Handler:   w.HandleApproveLoreEntityApiLoreEntitiesEntityIdApprovePost,
			Auth:      authGated,
			Requires:  principalAdminAgent,
			Summary:   "Approve ONE pending subject entity — owner or admin agent only (owner ruling rc-139a5ab99a19: 「待審，我跟 mira 有 admin 權限的才行」). The entity stops being `pending` and starts appearing in the boot subject directory, which is what makes the lore entries filed under it reachable by subject at all. 404 when no entity carries that id; 409 when the entity is not pending, because answering `done` would confirm a belief about its state that is wrong. 🔴 THERE IS DELIBERATELY NO REJECT ROUTE BESIDE THIS ONE: nothing has been ruled about whether a pending name may be thrown away, and inventing that exit here would decide it. `reason` is optional prose recorded in the governance journal beside the approval.",
			MCPTool:   "approve_lore_entity",
		},
		{
			// The per-target refusals (unknown / still pending / already merged /
			// itself) live in MergeLoreEntity, not here. `Requires` answers WHO may
			// ask; which targets are legal is a fact about the rows, and a copy of
			// it in this table would be a second answer that drifts the first time
			// one of them is added.
			Method:    "POST",
			Path:      "/api/lore/entities/{entity_id}/merge",
			LoreGated: true,
			Handler:   w.HandleMergeLoreEntityApiLoreEntitiesEntityIdMergePost,
			Auth:      authGated,
			Requires:  principalAdminAgent,
			Summary:   "Fold ONE pending subject entity into an existing APPROVED one — owner or admin agent only (owner ruling rc-139a5ab99a19). This is the repair approve cannot make: two names for one thing. The source keeps existing (nothing in this schema deletes) with `merged_into` pointing at the survivor, and its `type:name` key is registered as an ALIAS of the survivor — so every later write and search naming the old key resolves onto the surviving subject instead of minting it a second time. 404 when either id names nothing; 409 when the source is not pending; 422 when the target is itself still pending, has itself been merged away, or IS the source — each refused BY NAME rather than silently succeeding, because a merge into a subject the directory also hides parks the source somewhere no reader can follow. `reason` is optional prose recorded in the governance journal.",
			MCPTool:   "merge_lore_entity",
		},
		// ── T-33 lore governance ─────────────────────────────────────────────
		// 🔴 THESE TWO ROWS ARE WHY loreRetireNeedsOwner IS A GATE RATHER THAN A
		// DESCRIPTION OF ONE. The DAL shipped the three reasons, the owner split
		// and the journal with a full test suite and NOTHING calling it — so the
		// only thing driving it was the tests that assert it.
		//
		// 🔴 THE FLOOR IS principalAgent, NOT principalAdminAgent, AND THAT IS
		// THE OWNER'S RULING SHOWING UP IN THE TABLE (ta-c568dfd29844 D11). The
		// general rule for governance acts on this surface is admin_agent+; this
		// row is the written exception, because 'expired' and 'merged' claim
		// nothing about truth — they are tidying, and if even those had to wait
		// for the owner the tidying would never happen and the store would only
		// ever grow, which is the exact opposite of 「精而非多」.
		//
		// 🔴 WHAT THE FLOOR CANNOT SAY, AND WHY IT IS NOT ASKED TO: the SAME
		// route admits 'expired' from an ordinary agent and refuses 'falsified'
		// from that same caller. `Requires` has no vocabulary for "it depends on
		// a body field", so the per-reason half stays where it already lives —
		// loreRetireNeedsOwner — and this table declares only the floor. Do not
		// "finish the job" by raising this row to admin_agent: that would take
		// the tidying away from every agent while leaving the falsified gate
		// exactly where it is.
		//
		// ⚠️ The floor still earns its place: a machine (a warden) is not a
		// governance principal, and this row is what refuses it at the door.
		{
			Method:    "POST",
			Path:      "/api/lore/entries/{entry_id}/retire",
			LoreGated: true,
			Handler:   w.HandleRetireLoreEntryApiLoreEntriesEntryIdRetirePost,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Stop retrieving one lore entry and record WHY. Retirement is NOT a delete — the row stays and `revive_lore_entry` brings it back. `reason` is one of `expired` (the situation changed; it may come back), `merged` (folded into another entry — name it in `replaced_by`) or `falsified` (the claim was never true; it should not come back). An ordinary agent may file `expired` and `merged` itself; `falsified` is a judgement about truth and is refused 403 for anyone but the owner. An unrecognised reason is refused 422 rather than defaulted, so a typo cannot retire an entry as if it were merely stale. The reason is written to the governance journal, never onto the entry, because one entry can be retired, revived and retired again for a different reason and a column would only ever remember the last one.",
			MCPTool:   "retire_lore_entry",
		},
		{
			// principalOwner: reviving asserts the entry holds after all, which
			// is the same class of judgement as overturning one. ⚠️ That is a
			// DERIVATION, not the owner's words — recorded as such in
			// dal_lore_governance.go, where the same rule is enforced a second
			// time so the function is safe for callers this table does not know
			// about.
			//
			// 🔴 MCPExclude, AND IT FOLLOWS FROM THE FLOOR RATHER THAN BEING A
			// SECOND DECISION. Every owner-floor row this station serves is off
			// the tool surface, because the owner does not drive MCP tools — the
			// cockpit does, over REST. An owner-only tool in tools/list would be
			// a name every agent can read and no agent can use, which is exactly
			// the 「看得到、其實不存在」 this ticket exists to end. The owner's
			// path to this route is the Lore tab's button (詳細設計 §6.4).
			Method:     "POST",
			Path:       "/api/lore/entries/{entry_id}/revive",
			LoreGated:  true,
			Handler:    w.HandleReviveLoreEntryApiLoreEntriesEntryIdRevivePost,
			Auth:       authGated,
			Requires:   principalOwner,
			MCPExclude: true,
			Summary:    "Bring a retired lore entry back into retrieval — owner only, and it is what makes retirement reversible rather than a delete. 404 when no entry carries that id; 409 when the entry is not retired, because answering `done` would confirm a belief about its state that is wrong. `reason` is optional prose recorded in the governance journal beside the revival.",
		},
		// ── T-33 lore write ──────────────────────────────────────────────────
		// 🔴 THE ROUTE THE OTHER TWO WERE WAITING FOR. Retire and revive shipped
		// first and could only ever act on entries a test had seeded, because
		// this station served no way to create one. The visible symptom was not
		// an error: the subject directory was empty, an empty directory is not
		// rendered at all, and so the whole feature was invisible to every
		// member while looking, from the outside, exactly like a feature nobody
		// had used yet.
		//
		// 🔴 principalAgent, AND THE FLOOR IS THE POINT OF THE WHOLE TICKET.
		// Writing lore is what an agent does with what it just learned; putting
		// it any higher would put the owner back in the path of every write,
		// which is the load this ticket exists to take OFF him (his ruling of
		// 2026-09-01: 「可以先寫入 但是審核可以事後」). A machine is still
		// refused at the door — a warden has no experience to record.
		//
		// ⚠️ WHAT THE FLOOR DOES NOT DO, said plainly: there is no review gate
		// here. 欄位這一層有門檻——`symptoms`／`short`／`falsify`／`instance` 空白
		// 一律拒絕（2026-09-02 裁定 rc-714eea33c6ed）——但擋的是欄位，不是內容：
		// 沒有任何東西能分辨一格是真的填出來的還是硬掰的。審核仍然發生在寫入之後。
		{
			Method:    "POST",
			Path:      "/api/lore/entries",
			LoreGated: true,
			Handler:   w.HandleWriteLoreEntryApiLoreEntriesPost,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Write ONE lore entry — 五格, the subjects it is filed under, and the FULL ORIGINAL that outlives every later rewrite, all in one transaction. The five cells are `heading` (標題 — 發生了什麼，也是讀者找到它的那一軸), `content` (內容), `retire_when` (什麼時候不需要了), `impact` (沒有這條記憶的人最糟會發生什麼 — owner 2026-09-06 逐字；問的不是寫的人，被當場擋下來的一樣寫得出來) and `events` (相關的完整資訊 — 0..N 筆 時／事／人／地／物); `impact_stars` hangs off `impact` as YOUR proposed severity — REQUIRED, and 0 is refused (owner 2026-09-06「不允許給 0」), and `reviewed` — the flag that says somebody stamped it — is NOT writable here at all. 🔴 `heading` and `content` are the two that are REQUIRED: `content` is the only cell that ever enters a boot context, and `heading` is BOTH the line a human reads in a list AND the axis a reader finds the entry by — an entry missing either is not thin, it is either unreachable or indistinguishable from a finished one. ⚠️ THERE IS NO `trigger` CELL ANY MORE. v8 first pulled the 標題 out of it into its own `heading`; then owner ruling rc-9002654dd81c (2026-09-06), verbatim 「合併成 heading 一格（同時把搜尋改成掃 heading＋內容、待審畫面改顯示 heading）」 merged the two back into one. The split was making the same memory introduce itself with a different sentence on each screen — the list showed `heading`, the review queue showed `trigger`, and `query` scanned `trigger` only, so the one line a user could actually see was the one line search could not find. 🔴 `heading` IS CAPPED AT 140 CHARACTERS and the cap is in RUNES, not bytes — a Chinese character counts as 1 (owner ruling 2026-09-05, verbatim: 「我們標題規定 140 字元好了」). Over the cap the WHOLE write is refused 422, naming the cell, the cap and the length you sent; it is NEVER truncated, and no entry already stored is touched. The same cap closes the proposal route — filing one is refused, and so is accepting one, because accepting writes the 標題 back onto the entry. Measured 2026-09-05: the longest of the 24 headings rewritten to the v8 format is 130 runes, so nothing on the station is refused today and the longest one sits 10 runes under the cap. `retire_when` and `impact` are OPTIONAL and nothing is invented for them; `impact` is optional as a field while being the substance of the entry, because a hard requirement pushes a writer who genuinely has none into inventing one and an invented case reads exactly like a real one. ⚠️ Owner ruling rc-714eea33c6ed (2026-09-02), which made `falsify` and `instance` purely required, has NO LANDING PLACE in this format — neither cell exists any more. It was not overturned; the format change left it with no field to apply to, and whether 五格 should carry a cell meaning either of those things is the owner's call. `events` is `events`: every event needs `happened_ts` (when it HAPPENED, not when it was written down) and `what` (active voice, so the 人 is always the one doing it), while `actor` / `place` / `object` are sent only when you actually know them and are NEVER back-filled with 「未知」 — 「查不出是誰」 and 「還沒有人去查」 must not end up looking the same. Zero events is legal, and a bad event refuses the WHOLE write rather than leaving an entry half-written. `subjects` are subject keys shaped `type:name` (`repo:officraft`, `agent:Kyle`): an alias resolves, a merged-away subject follows to the survivor, an unapproved type prefix is refused BY NAME, and a key nobody has used yet MINTS a new subject parked for review and names it back to you in `pending_entities` — so a typo surfaces in this response instead of in the ontology a month later. `origin` says WHOSE knowledge this is (`human:Seth` for something the owner told you) and is not the same question as who is writing: the actor is taken from your verified token and cannot be asserted here. `supersedes` names the entry this one takes over from: it is re-statused `superseded` and the act is written to the governance journal, while an id that names nothing refuses the WHOLE write rather than leaving a pointer into empty space. ⚠️ There is NO `degraded` flag on the receipt any more: owner ruling rc-1e32c690018d (2026-09-03) removed it, because 標題格 is already a hard refusal at the door and a second, softer quality mark behind it earns nothing.",
			MCPTool:   "write_lore_entry",
		},
		// ── T-33 lore retrieval ──────────────────────────────────────────────
		// 🔴 EVERY SELECTION CONDITION IS IN THE BODY, AND THE REASON IS NOT THE
		// VERB. This router ignores an undeclared QUERY parameter on every route
		// it serves and answers 200 — pinned by a test that fires a real
		// request. The body decoder refuses an undeclared key with a 422 naming
		// it. So `POST …?typo=1` is exactly as silent as the GET would be: what
		// protects this hop is which side the conditions sit on, and moving one
		// to the query string would remove that while leaving the verb, and
		// every test, unchanged.
		//
		// 🔴 principalAgent. Retrieval is what an agent does with the directory
		// it woke up holding; a floor above that would mean an agent cannot read
		// its own store, which is the whole point of the store. A machine is
		// still refused at the door — a warden has nothing to recall.
		//
		// ⚠️ WHAT THIS ROUTE CANNOT DO, said here because a summary is where
		// people look: 第 3、4、5 格 (`retire_when`, `problem` and the events)
		// are NEITHER searched NOR returned on a hit — a hit carries `heading`
		// and the rest, `content` included, is read with `get_lore_entry`. There is
		// no table, no index and no parameter for them here, and
		// de-duplication and conflict-finding both run on `impact` (`problem`)
		// — so neither is reachable through this route today. That is a known
		// gap, not an oversight.
		//
		// ⚠️ This paragraph used to name `symptoms`, a 六格 cell that 五格
		// removed. The cell is gone, the gap is not: it moved onto `problem`.
		{
			Method:    "POST",
			Path:      "/api/lore/search",
			LoreGated: true,
			Handler:   w.HandleSearchLoreEntriesApiLoreSearchPost,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Retrieve lore entries — hop ② of the design: you have seen the subject directory at wake and now want what is actually filed under one of those subjects. 🔴 EVERY SELECTION CONDITION GOES IN THE REQUEST BODY AND NONE IN THE QUERY STRING, and that is load-bearing rather than stylistic: an undeclared body key is refused 422 by name, while an undeclared QUERY parameter is silently ignored on every route this station serves — so a mistyped condition on the query side would hand you a plausible answer that is not the one you asked for, and nothing would report it. All fields are optional; sending none asks for everything still retrievable. `subject` is a subject key (`repo:officraft`); an alias resolves and a merged-away subject follows to the survivor, and a key that names NOTHING comes back as `subject_resolved: false` rather than as an empty result — 「this subject has nothing on it」 and 「this subject does not exist」 are different answers and you need to tell them apart. 🔴 THERE IS EXACTLY ONE RETRIEVAL AXIS: `subject`. Owner ruled the 「活動」 (action) axis away on 2026-09-05 — 「只有subject 沒有 action因為後者太多可能性」 — because it was an OPEN set every writer minted into freely and no reader ever filtered on, so it never converged and was therefore not an index. The T1/T2 tier went with it: the tier was 「matched every axis you asked on」, and with one axis that can only ever answer T1 — a field with one possible value reads like a judgement while making none. ⚠️ Gone with it, said plainly so you do not go looking: the trust/method/cognitive class, `trust_fell_back`, `unmapped_actions`, `force_trust_analogy`, and the cross-subject wall that used to withhold trust-class entries from analogies. Every one of those was derived either from action names or from the analogy tier. Nothing guards that distinction today. `query` is a LITERAL, case-insensitive substring over 標題格 (`heading`) and 內容格 (`content`) and `applied.query_match` says so: it is not semantic, and two entries describing the same situation in different words will not find each other. 🔴 第 3、4、5 格 (`retire_when`, `impact` and the events) are NEITHER searched NOR returned on a hit — a hit carries `heading` (plus `impact_stars`, `origin` and `subjects`), and the rest, `content` included, is read with `get_lore_entry`. ⚠️ 掃描面以前是 `trigger`＋`content` while the list showed `heading`, so the very line a searcher was reading matched nothing; owner ruling rc-9002654dd81c (2026-09-06), verbatim 「合併成 heading 一格（同時把搜尋改成掃 heading＋內容、待審畫面改顯示 heading）」 merged the two cells and pointed `query` at `heading`. There is no parameter, table or index for them here, which is why de-duplication and conflict-finding cannot be done through this route yet.",
			MCPTool:   "search_lore_entries",
		},
		// ── T-33 lore, hop ③: reading the original back ──────────────────────
		// 🔴 THESE TWO ROWS ARE THE TICKET'S OWN OPENING SENTENCE. The owner
		// asked for 「原始資訊可以保留讓我們可以重新判定一些東西」. The original
		// was already being kept — entry and first revision are one transaction
		// — and until these rows existed NO PATH SERVED IT. That state satisfies
		// the database and no reader: every count agrees, and not one agent can
		// act on any of it.
		//
		// 🔴 GET, AND EVERY ADDRESS IS A PATH PARAMETER. There is no `?revision=`
		// and there must never be: an undeclared query parameter is silently
		// ignored on every route this station serves, so that spelling would let
		// a caller ask for one revision and quietly receive another, with the
		// response looking exactly right. A path that does not match is a 404.
		//
		// 🔴 A RETIRED ENTRY IS STILL READABLE HERE. `retired` means "no longer
		// RETRIEVED" — search and the boot directory exclude it — and nothing
		// more. Refusing it here too would make retirement a delete through the
		// back door, and the only path that can answer "what did the thing we
		// stopped using actually say" would be the one that refuses.
		{
			Method:    "GET",
			Path:      "/api/lore/entries/{entry_id}",
			LoreGated: true,
			Handler:   w.HandleGetLoreEntryApiLoreEntriesEntryIdGet,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Read ONE lore entry in full, together with the ORIGINAL that was preserved beside it — hop ③ of the design, and the reason 「原始資訊可以保留讓我們可以重新判定」 is a mechanism rather than a sentence. `content` (`content`) is the compressed line that enters a boot context; `original` is the complete text of the entry as it was last written — all four cells AND the `events:` block, each named, blank ones included — so an agent that has stopped believing the compressed version has somewhere to go, and so that `events` is inside `sha256` too. `events` comes back in the order the events HAPPENED (`happened_ts`), not in the order anybody wrote them down, and 人／地／物 that nobody knew come back EMPTY rather than filled in with 「未知」. `sha256` digests that original, so a reader can tell that what it is holding is what was stored. `revisions` is a CATALOGUE — id, when, who, and how many characters that write REMOVED — and carries no text at all, because a list is how you choose a revision and choosing does not need the prose; fetch one by id from `/api/lore/entries/{entry_id}/revisions/{revision_id}`. 🔴 ADDRESSING IS ENTIRELY IN THE PATH AND THERE ARE NO QUERY PARAMETERS, deliberately: an undeclared query parameter is silently ignored on every route this station serves, so `?revision=3` would have been a way to ask for a specific revision and quietly receive the latest one. A wrong path is a 404, which is loud. 404 when no entry carries that id.",
			MCPTool:   "get_lore_entry",
		},
		{
			// The revision lookup is SCOPED to the entry in the path, and that is
			// enforced in the DAL rather than here: revision ids are global, so an
			// unscoped read would serve any entry's text through any entry's
			// address and a mistyped entry id would hand back somebody else's
			// original with nothing to signal it.
			Method:    "GET",
			Path:      "/api/lore/entries/{entry_id}/revisions/{revision_id}",
			LoreGated: true,
			Handler:   w.HandleGetLoreRevisionApiLoreEntriesEntryIdRevisionsRevisionIdGet,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Read ONE revision of a lore entry in full — the exact text that was stored at that moment, plus its `sha256`. `shrink_chars` says how many characters that write removed compared with the one before it, which is how a compression that quietly hollowed an entry out becomes visible at all (the entry count does not move when an entry is emptied). 🔴 THE ENTRY ID IN THE PATH IS A CONSTRAINT, NOT DECORATION: revision ids are global, so a revision that belongs to a DIFFERENT entry is a 404 rather than being served through this address — a mistyped entry id must not hand you somebody else's text with nothing to signal it. 404 when the entry does not exist, or when it does and that revision is not one of its own.",
			MCPTool:   "get_lore_revision",
		},
		// ── T-33 lore, 回饋與提案 ──────────────────────────────────────────────
		// 🔴 APPENDED AT THE END OF THE TABLE, like every tool before them, and
		// for the reason stated in full at the custom-themes block above: the MCP
		// tool surface has ONE order shared by this table, spec/openapi.json's
		// x-mcp.order and conformance/routes_manifest.json, and x-mcp.order must
		// be the consecutive range 0..N-1. Inserting these beside the other lore
		// rows — where they read better — would renumber every tool after them.
		//
		// 🔴 principalAgent ON BOTH, AND THAT IS THE POINT OF THE PAIR. Saying
		// 「這條幫倒忙，我認為它應該長這樣」 is an agent's own act on knowledge it
		// was handed; a floor above that would mean the only members who ever
		// USE the store cannot report what it did to them, and the owner's ruling
		// of 2026-09-01 (「可以先寫入 但是審核可以事後」) already put review after
		// the act rather than in front of it. Nothing here decides anything: a
		// proposal changes no entry, so an agent floor grants no authority over
		// the store, only the ability to ask.
		//
		// ⚠️ THE READ IS AT THE SAME FLOOR AS THE WRITE, deliberately. A proposer
		// has to be able to see whether somebody already filed the same thing,
		// and — more to the point — whether his own proposal has gone stale since
		// he filed it. Putting the read above the write would make a proposal
		// something an agent can send and never see again.
		//
		// ⚠️ WHAT THESE TWO ROWS DO NOT DO: there is no accept, no decline, and
		// no cross-entry queue. 仲裁 is separate work. What this pair owes it is
		// the base digest, and that is recorded, checked at submit and recomputed
		// on every read.
		{
			Method:    "POST",
			Path:      "/api/lore/entries/{entry_id}/proposals",
			LoreGated: true,
			Handler:   w.HandleProposeLoreChangeApiLoreEntriesEntryIdProposalsPost,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "Propose a change to ONE lore entry — a WHOLE replacement version, not a patch, plus the account of why. 🔴 YOU SEND THE FULL NEW VERSION AND THE DIFF IS COMPUTED FROM IT (owner ruling, 2026-09-02: 「讓 agent submit new full version 即可 / diff view 我們自己產出」). A patch would leave two artefacts — what you said you were changing and what applying it actually produces — and the gap between them looks completely normal to a reviewer. With a whole version there is no second artefact: the difference a reviewer reads is the bytes that would land. 🔴 `base_sha256` IS THE VERSION YOU ACTUALLY READ, taken from `sha256` on `GET /api/lore/entries/{entry_id}`, and it is REQUIRED. If the entry has been rewritten since you read it the proposal is refused 409 naming both digests — filing against the older text would silently discard whoever changed it, which is exactly the failure a stale pull request causes and it looks correct from every side. Re-read the entry and rebuild your version on what is there now. A proposal that was fine when filed and went stale AFTERWARDS is not refused — it comes back from the list route with `stale: true`, because at that point the reviewer, not you, is the one who has to know. 🔴 THE THREE ACCOUNT FIELDS ARE ALL REQUIRED, for the same reason a write refuses a blank cell instead of defaulting it: `encountered` says what you were doing when this entry reached you, `fault` says which of three things is wrong with it (`stale` — it was right and is not any more; `never-true` — the claim never held; `misled` — it is retrieved for situations it does not describe and it sent you the wrong way, so its `heading` wants fixing), and `evidence` is what you actually SAW. ⚠️ The cost is the same one the write path accepts and has not solved: nothing here can tell a real account from an invented one; an empty cell is all it can refuse. `kind` is `update` (the body cells — `heading`, `content`, `retire_when`, `impact` — PLUS `impact_stars` and `events` carry the whole new version and are held to the SAME rules a write is, so a proposal nobody could ever accept is refused now rather than sitting in the queue looking acceptable; ⚠️ 這一段先前寫著標題與星等「帶不動」，那句話自 00084 給 lore_proposal 補上 heading 與 impact_stars 兩欄起就是假的 —— 核可換掉的是整條的本體格與整份`events`。⚠️ 而 `trigger` 這一格已經不存在了：owner ruling rc-9002654dd81c (2026-09-06), verbatim 「合併成 heading 一格（同時把搜尋改成掃 heading＋內容、待審畫面改顯示 heading）」。) or `remove` (you are proposing this entry stop being retrieved and you send NO body cells and NO events; a removal that carried a version would put text on the reviewer's screen that no accept would ever write). 🔴 A PROPOSAL CARRIES `events` TOO, AND `events` IS REQUIRED ON AN `update` — the WHOLE list as it should stand afterwards, not a set of additions, because accepting replaces the entry's events wholesale (owner ruling rc-e5c34500face, 2026-09-03: 「改得動 —— 提案就該帶完整的新版本，包含所有事件」). The reasoning he overturned was 「`events`是機器串出來的事實，提案只是意見」, and its hole is that WHEN THE MACHINE STRINGS IT TOGETHER WRONG NOTHING CAN REPAIR IT: re-deriving washes away whatever a person filled in by hand, so a proposal that moves events is the only road that repairs one. Send `[]` to claim the entry should carry no events; OMITTING the key is a 422, never a shorthand for 「維持現狀」 — one forgotten field must not clear `events` where no reviewer can see it. Each event is held to the same rules a write is (時 and 事 required; 人／地／物 checked only when non-empty), and the order you send them in does not change the digest. Removal is not deletion — the existing act is `retire`, and `revive_lore_entry` undoes it. 🔴 NOTHING HERE ACCEPTS ANYTHING: this route files a proposal and no more.",
			MCPTool:   "propose_lore_change",
		},
		{
			Method:    "GET",
			Path:      "/api/lore/entries/{entry_id}/proposals",
			LoreGated: true,
			Handler:   w.HandleListLoreProposalsApiLoreEntriesEntryIdProposalsGet,
			Auth:      authGated,
			Requires:  principalAgent,
			Summary:   "List the change proposals filed against ONE lore entry, newest first, each carrying the WHOLE proposed version rather than a description of it — so what a reviewer compares is the bytes that would land. Each proposal carries 四格 AND its own `events`, so each `body` was rendered against THE PROPOSAL'S events — the bytes that would actually land, since accepting replaces the entry's events wholesale (owner ruling rc-e5c34500face, 2026-09-03). 🔴 `events_added` / `events_removed` ON EVERY ROW SAY WHICH EVENTS THAT PROPOSAL MOVES, so a reviewer does not have to diff two lists by eye — and a deletion, which shows up only as an absence, is the half he would otherwise miss. Both sides are still on the wire (`events` per row, `current_events` on the response) so the difference can be recomputed rather than trusted; like `stale`, it is computed on every read and never stored. `current_sha256` and `current_revision_id` say what the entry stands at RIGHT NOW, and every proposal carries the `base_sha256` it was written against. 🔴 `stale: true` MEANS THE ENTRY WAS REWRITTEN AFTER THIS PROPOSAL WAS FILED — its author argued against text that is no longer there, and applying it would discard whoever changed it in between. It is COMPUTED on every read by comparing the two digests, never stored: a stored flag would be right the day it was written and wrong every day after. The digest it was compared against travels in the same response so the comparison can be checked rather than trusted. 🔴 THIS ROUTE DECIDES NOTHING. There is no accept or decline here; a proposal is a request for review and the verdict is a separate act.",
			MCPTool:   "list_lore_proposals",
		},
		// ── T-33 lore 提案的核可 ───────────────────────────────────────────────
		// 🔴 THIS ROW IS THE RULING ARRIVING, AND IT IS WHY THE PAIR ABOVE SAYS
		// 「NOTHING HERE DECIDES ANYTHING」 AND THIS ONE DOES. ApplyLoreProposal
		// has been able to land a proposal since the DAL shipped, with a full test
		// suite and NO route — deliberately, because 「誰有資格核可」 is a policy
		// and the DAL is not where a policy can be written. The owner closed that
		// on rc-a896af93d4f9 by choosing 「你 ＋ 銀月（沿用現有前例）」, and
		// principalAdminAgent below is that sentence: the owner outranks
		// admin_agent on the ladder, so one row says both halves of it.
		//
		// 🔴 THE PRECEDENT HE 沿用 IS THE ENTITY REVIEW QUEUE (rc-139a5ab99a19),
		// three blocks up. That is the same shape of act — a reviewer publishing
		// something into what every agent reads — and accepting is the stronger
		// of the two: it REWRITES an entry another agent wrote, and replaces 第 5
		// 格 wholesale. An agent floor here would mean any member could rewrite
		// any other member's memory by filing a proposal and accepting it himself.
		//
		// 🔴 THE FLOOR IS ABOVE THE PROPOSE/LIST PAIR, AND THAT ASYMMETRY IS THE
		// WHOLE DESIGN. 提案 stays at principalAgent because asking changes
		// nothing; 核可 is the act, and the two floors are what makes 「先寫入、
		// 審核事後」 a review rather than a formality.
		//
		// 🔴 THERE IS NO DECLINE ROW AND NO 裁決紀錄, and both absences are
		// deliberate in exactly the way the entity block's missing reject row is:
		// the owner has ruled on WHO may accept and on nothing else. What a 退回
		// looks like, and whether a verdict earns a journal row of its own, are
		// still his to decide — shipping either here would decide it, in the
		// direction that destroys somebody's filed proposal.
		{
			Method:    "POST",
			Path:      "/api/lore/entries/{entry_id}/proposals/{proposal_id}/accept",
			LoreGated: true,
			Handler:   w.HandleAcceptLoreProposalApiLoreEntriesEntryIdProposalsProposalIdAcceptPost,
			Auth:      authGated,
			Requires:  principalAdminAgent,
			Summary:   "Accept ONE filed proposal and write it onto its lore entry — owner or admin agent only (owner ruling rc-a896af93d4f9: 「你 ＋ 銀月（沿用現有前例）」, the same floor the subject-entity review queue already carries). The four cells are replaced, `events` is replaced WHOLESALE by the events the proposal carried — not merged, because a merge would let a proposal add events and never remove one, and repairing an event the machine strung together wrongly is the reason this road exists — and ONE new revision is written carrying the EXACT BYTES the proposal stored rather than a fresh rendering, with `actor_id` = YOU, the accepter, not the proposer. 🔴 THE ADDRESS IS THE WHOLE PAIR AND BOTH HALVES ARE CHECKED: proposal ids are global, so a proposal that belongs to a DIFFERENT entry is a 404 saying so by name rather than being applied through this address — a mistyped entry id must not rewrite somebody else's entry with nothing to signal it. 404 when no proposal carries that id, and 404 when it carries it under another entry. 409 when the proposal is a `remove` — it proposes no version to write at all, and the act it asks for is `retire_lore_entry`. 409, naming BOTH digests, when the entry was rewritten after the proposal was filed: accepting then would discard whoever changed it in between, silently, and the fix is to re-read the entry and have the version rebuilt on what is there now. That check is made HERE, at the moment you press accept, not only when the list was read — the entry can move between the two. 422 when the proposed version is identical to the one it was written against, because there is nothing to review. 🔴 THERE IS DELIBERATELY NO DECLINE ROUTE BESIDE THIS ONE, AND NO ARBITRATION RECORD BEYOND THAT NEW REVISION'S `actor_id`: the owner has ruled on WHO may accept and on nothing else, so inventing a 退回 exit or a verdict journal here would decide for him what he has not decided.",
			MCPTool:   "accept_lore_proposal",
		},
	}
}
