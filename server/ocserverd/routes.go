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
	// ShareSig admits the ?sig= file-level share credential (sharesig.go) as a
	// third auth path on THIS row only (precedence: Authorization header →
	// ?token= → ?sig=). Every other row never consults sigs — a sig grants
	// exactly one blob read, nothing else.
	ShareSig bool
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
			Summary:  "Hire a member (server mints the id). runtime defaults to claude and only claude/codex are accepted; effort defaults to medium and is validated; a hire that names kind or role_key is admin-gated.",
			MCPTool:  "hire_member",
		},
		{
			Method:   "GET",
			Path:     "/api/members/{member_id}",
			Handler:  w.HandleGetMemberApiMembersMemberIdGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one roster member (removed → 404).",
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
			Summary:  "Force-stop: robust STOP now. On the offboard arm this is the ONLY thing that ever collects the member -- nothing times out.",
			MCPTool:  "force_stop_member",
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
			Summary:  "restart_self(): self-triggered recycle (online-only 409; min-liveness 429).",
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
			Summary:  "List the chat stream (?with=<id>&limit=<n>; oldest→newest). History paging: before_ts + before_id (both together) return the limit messages strictly OLDER than that keyset cursor — a history page NEVER advances the read watermark. Re-read specific messages by id: ids=<id>&ids=<id> returns those messages in full without a peer and without a cursor; the ids schema states who may read what, the per-call limit, and what an unknown id does.",
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
			ShareSig:   true,
		},
		{
			Method:   "GET",
			Path:     "/api/chat/attachments/{attachment_id}/share-link",
			Handler:  w.HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Mint a permanent single-file share link (?sig= HMAC; grants read of this one attachment only). Returns {url} as a SERVER-RELATIVE path — prefix it with the origin you reach this server on to get a link you can paste to someone. The sig carries NO identity and NO expiry: whoever holds the link reads that one blob without signing in, forever, and it cannot be revoked. Mint it for deliverables you meant to hand over; do not paste it anywhere the blob itself would not belong.",
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
			// now, and every minted link is permanent, unrevocable, and
			// credential-less (sharesig.go). Read that file before widening
			// this seam any further.
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
			Summary:    "Upload one attachment blob (raw octet-stream body; returns the light ref).",
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
			Summary:  "Open a reply card: an ask the owner must answer (options ≤4, [0]=AI pick). Auto-binds to your single active task's CURRENT step — that step (and the task) enters waiting_owner until the owner answers; several lanes of one parallel_group running at once is fine (the lowest order_idx lane carries the card, and the whole task holds either way). If that task has NO resolvable current step the call is REFUSED with 409 and no card is opened: binding the task without a step places no hold, so the task would finish underneath your question and the owner's answer would then be rejected. Fix what the error names — report the step you are on (update_step_status in_progress), use open_gate with an explicit task_id + step_id, or send bind=\"none\" if the ask is not about the task. With no single clear active task, a plain unbound 請示 opens as before. Optional attachments ride the question (same shape as post_chat: {id} from `ocagent upload` / POST /api/chat/attachments, or inline data_b64).",
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
		// 系統互動 and 啟動程序 used to be go:embed seeds with no editable
		// representation at all: one wrong sentence cost a release. They now
		// carry the same read / whole-document replace / reset-to-factory shape
		// the 使用者自訂 block above has, plus document history.
		//
		// FLOORS: read at the machine floor (an agent already reads both blocks
		// in its own boot context — nothing here is new to it); WRITE at
		// admin_agent, because this text lands in EVERY agent's boot context and
		// a broken 啟動程序 keeps them from coming online at all. That failure is
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
			Summary:  "Replace the WHOLE 系統互動 block of the boot context ({text}) — the handbook every agent reads at boot. text is REQUIRED and unknown keys are rejected; emptying a block that had content needs allow_shrink=true. The write is judged against the doc.cap_chars.system_interaction cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_system_interaction always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only.",
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
			Summary:  "Read one runtime's 啟動程序 block — the boot checklist that ends that runtime's boot context. runtime_key is 'claude' or 'codex'; they are separate documents because step 3 of the two says opposite things (claude mounts its own `ocagent listen`, codex must not — the sidecar owns it), so any other value is a 404 rather than a silent fallback to claude. Folded: the owner's edit when one exists, otherwise the shipped factory seed. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool:  "get_boot_sequence",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-sequence/{runtime_key}",
			Handler:  w.HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the WHOLE 啟動程序 block of ONE runtime ({runtime_key, text}). runtime_key is 'claude' or 'codex' and the two are separate documents whose step 3 contradicts each other, so writing the wrong one leaves those agents unable to come online — and nothing that never boots reports it. text is REQUIRED and unknown keys are rejected; emptying a block that had content needs allow_shrink=true. Judged against the doc.cap_chars.boot_sequence cap (one cap, both runtimes, each measured on its own text); the refusal tells you what you wrote, the cap, and what is stored. The shipped seed is never overwritten, so reset_boot_sequence always gets the factory text back. Owner or admin assistant only.",
			MCPTool:  "replace_boot_sequence",
		},
		{
			Method:   "POST",
			Path:     "/api/boot-sequence/{runtime_key}/reset",
			Handler:  w.HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore ONE runtime's 啟動程序 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). runtime_key is 'claude' or 'codex'; anything else is a 404. No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it, which is what makes this the recovery route when a bad edit has stopped agents from booting. The overlay being discarded is retained in the document history. Owner or admin assistant only.",
			MCPTool:  "reset_boot_sequence",
		},
		// 下線程序 (T-c9c0) — the fourth owner-editable global document, and the
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
			Summary:  "Read the 下線程序 block — the wrap-up checklist the server hands an agent at the moment it is about to collect that session. It is a SINGLETON: one document for every agent and every runtime, keyed `global` like the 系統互動 block. Folded: the owner's edit when one exists, otherwise the shipped factory seed, with is_default saying which of the two you are holding and has_seed saying a factory version exists to go back to. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool:  "get_offboard",
		},
		{
			Method:   "POST",
			Path:     "/api/offboard",
			Handler:  w.HandleReplaceOffboardApiOffboardPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Replace the WHOLE 下線程序 block ({text}) — the wrap-up checklist an agent is handed when its session is being collected. text is REQUIRED and unknown keys are rejected; emptying a block that had content needs allow_shrink=true. The write is judged against the doc.cap_chars.offboard cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_offboard always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only.",
			MCPTool:  "replace_offboard",
		},
		{
			Method:   "POST",
			Path:     "/api/offboard/reset",
			Handler:  w.HandleResetOffboardApiOffboardResetPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restore the 下線程序 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it. The overlay being discarded is retained in the document history, so the reset is itself recoverable. Owner or admin assistant only.",
			MCPTool:  "reset_offboard",
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
			Summary:  "Size-only overview of EVERY capped document on the station: each role's role definition / insight / DEFAULT lessons bucket, and each task manual's SOP / learnings, as size_chars plus the cap_chars in force for THAT segment (the five segments have five separate caps — each is reported against its own). LIMITATION: lessons is reported for the default bucket only; nothing stops a write from naming another bucket, and such a document spends the same lessons cap yet never appears here. Carries NO document text, so it costs a few hundred bytes. Use it to find which long-lived document is nearly full, then read only that one (get_role / get_insight / get_lessons / get_task_manual). It is the only way to see insight and lessons sizes in bulk — no listing reports those at any price; the manual sizes and caps are also on every list_task_manuals row, and a role definition's size and cap are already on every list_roles row.",
			MCPTool:  "peek_doc_sizes",
		},
		{
			Method:   "POST",
			Path:     "/api/roles",
			Handler:  w.HandleCreateRoleApiRolesPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Create a custom role + its founding member (one pair per call). runtime is claude/codex (absent = claude).",
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
			Path:     "/api/lessons/{role_key}/{task_type}",
			Handler:  w.HandleGetLessonsApiLessonsRoleKeyTaskTypeGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read a per-role lessons doc (per role_key; overlay ⊕ seed).",
			MCPTool:  "get_lessons",
		},
		{
			Method:  "POST",
			Path:    "/api/lessons/{role_key}/{task_type}",
			Handler: w.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost,
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
			Summary:  "Replace the WHOLE per-role lessons document. text is REQUIRED and unknown keys are rejected; only that role's agent or an admin may write it; emptying or sharply shrinking it needs allow_shrink=true; and the result is still judged against the lessons cap.",
			MCPTool:  "replace_lessons",
		},
		{
			Method:  "POST",
			Path:    "/api/lessons/{role_key}/{task_type}/patch",
			Handler: w.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost,
			Auth:    authGated,
			// T-5336: same honest floor as the whole-doc replace above (the two
			// share lessonsWriteAuthz). READ stays on the machine floor — any
			// authenticated identity may read any role's lessons.
			Requires: principalAgent,
			Summary:  "Patch a per-role lessons doc by unique anchors ({edits:[{old,new}]}).",
			MCPTool:  "patch_lessons",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary",
			Handler:  w.HandleResumeSummaryApiResumeSummaryGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Bounded LIGHT wake snapshot for the caller (identity-locked; recent chat + light open-task rows + size overview — peek sizes first, pull detail via get_task). CHAT is packed newest-first under a CHARACTER BUDGET, not a fixed message count, and stopping at the last message that still fits; each message carries from_name/to_name beside the ids and ts_display (full date + time + zone offset) beside the epoch ts, and folds in its reply card as `card` when it has one — read every ts_display against the top-level `generated_at`. TWO DIFFERENT things can be missing and they are marked DIFFERENTLY: `body_omitted_chars` > 0 means THAT message is here with that many characters COLLAPSED away (another agent's line — the owner's line and your own hand-off notes to yourself are carried in full), re-read it with get_chat; `chat_earlier_omitted` is the other kind and it is a MAYBE, not a fact: that line was cut at a read or budget limit and nothing looked past the cut, so whole messages may be missing from this payload entirely — it is raised even when there is in fact nothing older. Its hint tells you how to CHECK and fetch them. The two are asymmetric ON PURPOSE: the collapse marker is CERTAIN (that message IS here, shortened, exact count); this one is not, and only the fetch settles it. Also carries the STUDIO FLOOR you wake up onto: roster (every member and contractor, each with online/offline status, the machine it runs on, and its duty capped at 1000 chars with `…` marking a cut, the cap applied after the doc's own leading title line is removed — who to ask for help; no insight/learning by owner ruling. Contractors additionally carry their bound task's status, waiting_reason, and step progress (progress_done/progress_total) — members leave these at their zero value; a contractor's 0/0 is ambiguous (a task with no steps yet, or no task at all) and task_status is what tells them apart, non-empty vs empty) and machines (the machine list plus you_are_on, your server-recorded machine binding — never derive it from a hostname). It also carries `doc_capacity` — the long-lived capped documents in your reach that are CLOSE to full (your role documents, the boot documents, your open tasks' manuals, your open steps' notes), each with size/cap, what is left, and whether YOU can rewrite it or have to ask the person who can. The key is ABSENT when nothing is near, so its presence is the whole signal — act on it now, not when a write is refused.",
			MCPTool:  "resume_summary",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary-size",
			Handler:  w.HandlePeekResumeSummarySizeApiResumeSummarySizeGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Size-only PEEK of the wake snapshot (identity-locked; overview counts/sizes + estimated_total_chars, NO chat/task content). estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars + steps_on_answered_card_chars + doc_capacity_chars, all six reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit, the two studio-floor blocks, the named steps sitting on an answered card, and the near-cap document rows (0 unless something is close to its cap) — what pulling the snapshot actually costs. Step one of the two-step boot: call this FIRST to size resume_summary, then either call resume_summary directly (small) or hand the pull to a cheap sub-agent that returns a digest (large).",
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
		// handlers' (caller == executor, admin capability excepted — root CLAUDE.md「核心不變量／授權單一化」).
		{
			Method:   "GET",
			Path:     "/api/tasks",
			Handler:  w.HandleListTasksApiTasksGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List tasks (?executor=&type=&status=, or statuses=[…] for a SET of states — every filter given is ANDed; LIGHT list items — id/task_no/title/type_key/status/priority/executor/creator_id/progress/timestamps/deps + dep_tasks, WITHOUT steps/description/inputs). Ask for the states you actually want (`statuses: [\"not_started\", \"in_progress\"]`) instead of listing everything and filtering yourself — the whole history is a large answer. `statuses` also accepts \"reassigning\", which matches the handover LOCK rather than the status column. `dep_tasks` already carries each blocker's task_no/title/status, so a blocked task needs no follow-up get_task just to name what it is waiting for. Call get_task for a task's full detail (steps, description, inputs).",
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
			Summary:  "Read one task (steps, deps, progress, gate cards).",
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
			Summary:  "Correct THIS task's own TEXT — its title, its description, or both in one write (T-646a). Replaces `update_task_title` and `update_task_description`, which documented the same rules twice and could not be applied together: changing both meant two calls, two transactions and two SSE deltas, with room for someone else's write to land in between. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. PARTIAL: only the fields you NAME are touched, so omitting a field is a legal no-op for it that versions nothing and fans nothing. ⚠️ THE TWO FIELDS TREAT AN EXPLICIT BLANK DIFFERENTLY, and that is an owner ruling rather than an inconsistency (card rc-796541192519, 2026-08-11, option ①): a blank `title` (\"\" or whitespace-only) is REFUSED with 400 and does NOT clear the field, because create_task refuses a blank title too and an edit door looser than the create door would let a caller reach a task-list row with nothing in it; a blank `description` IS accepted and DOES clear the text, because plenty of cards legitimately have no prose. VALIDATION IS WHOLE-BODY AND HAPPENS FIRST: a request carrying a blank title alongside a perfectly good description writes NEITHER — a 400 leaves the task exactly as it was, never half-applied. Both values are trimmed of surrounding whitespace before they are stored AND before they are compared with what is there, so re-sending the same text with a stray trailing space is correctly seen as no change rather than spending one of the retained revisions saying nothing moved. ⚠️ THAT HOLDS ONLY WHILE THE STORED TEXT IS ALREADY TRIMMED. Whenever the stored description carries untrimmed whitespace, the next edit here normalises it and therefore DOES spend a revision — even when you re-send exactly what you read back. TWO things can put untrimmed text in that column, so this is not a one-time settling: create_task, which never trims the description (it does trim the title), and a RESTORE of a revision that holds untrimmed text, which is written back verbatim. Before this ticket both doors stored it raw and agreed; this tool trims and create still does not, which is a divergence awaiting a ruling rather than a promise about the system. The write is wholesale within each field: send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close: artifacts record what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters a field retains the previous value as a document version — kind `task_title` / `task_description`, key = the task id — so a correction is recoverable through list_document_history and the older wording is never simply gone.",
			MCPTool:  "update_task",
		},
		// T-e271: the ticket's own TEXT is correctable after the fact. Until
		// this row existed the tool catalogue had NO way to edit an existing
		// task's description — create_task takes one only at birth, submit_plan
		// writes steps, update_task_manual writes the TYPE's manual — so a
		// ruling to reword a card had nowhere to land. Executor-guarded like
			// every other task-driving write (callerMayDriveTask (root CLAUDE.md「核心不變量／授權單一化」)); the CREATOR
		// gets no standing from having created it (owner ruling).
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/description",
			Handler:  w.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's description — the ticket's own text (what the task IS: scope, origin, acceptance). T-e271: until this tool existed there was NO way to change a description after creation — create_task takes one only at birth, submit_plan writes steps, update_task_manual writes the TYPE's manual — so a decision to reword a card had nowhere to land. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. PARTIAL like update_task_manual: omitting `description` changes nothing (a safe no-op), while an explicit \"\" CLEARS it — absent and empty are different on purpose; unknown keys are refused rather than dropped. The write is wholesale within that field: the value replaces whatever was there, so send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close. The reason they differ: artifacts are the record of what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters the text retains the previous one as a document version (kind `task_description`, key = the task id) — list it with list_document_history, so a correction is recoverable and the older wording is never simply gone.",
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
			Summary:  "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's title — the one line the task list shows. T-2ebe: until this tool existed a title could never be changed after creation, so a card whose scope was later overturned kept advertising its first wording forever — the description could correct itself, the title could not, and whoever scanned the list saw only the stale half. If you have just corrected a description because the scope moved, ask whether the title still says the same thing. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's title now. PARTIAL like update_task_description: omitting `title` changes nothing (a safe no-op); unknown keys are refused rather than dropped. ⚠️ ONE DIFFERENCE FROM ITS DESCRIPTION TWIN: a blank title (\"\" or only whitespace) is REFUSED with 400, it does NOT clear the field — create_task refuses a blank title too, and a task with no title is a blank row on the list. Surrounding whitespace is trimmed. The write is wholesale within that field: send the full corrected title, not a fragment. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — a ticket is usually found to be worded wrongly after it closed, and freezing the text would preserve a known falsehood; its artifact set is the opposite and freezes at close. Every change that actually alters the text retains the previous one as a document version (kind `task_title`, key = the task id) — list it with list_document_history, so a correction is recoverable.",
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
			Summary:  "Write this step's working note: where the work stands and what comes next — the field the handover SOP means by 「把還在進行中的工作寫回 task step note」. WHAT TO WRITE — three things, then stop: (1) STATE — one sentence on where this step actually got to; (2) NEXT — one sentence on what whoever takes over does next; (3) EVIDENCE POINTERS — version ids, file and log paths, what you verified YOURSELF versus what you are taking on someone's word, and the limits of what was NOT done. Long narrative does not live here: reasoning and scope belong in the task description, reports and diffs belong on the task as artifacts. The note is the current state — not a report, not an append-only log. Writable in ANY step status (pending, in_progress, waiting_owner, waiting_external, done, superseded), unlike `waiting_reason`, which is locked to waiting_external. Wholesale write: `note` replaces whatever was there and \"\" clears it, so rewrite it as the work moves rather than appending; over 4,000 characters (counted in runes) is refused. Same executor/admin gate as every other task-driving write (403 otherwise). ⚠️ A task auto-closes when its last step is reported done and a closed task 409s — so write the note BEFORE the report that finishes the last step, not after. The receipt carries `size_chars` / `cap_chars`, so the room left is on every write instead of only on the 400 that refuses one; `get_task` reports the same pair per step as `note_size_chars` / `note_cap_chars`.",
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
			Summary:  "Patch this step's working note by unique anchors ({edits:[{old,new}]}) — send only the part that changed, instead of re-typing the whole note. USE THIS WHENEVER YOU ARE AMENDING A NOTE THAT ALREADY HAS CONTENT. update_step_note is a wholesale replace, so if anyone else wrote to the step between your read and your write, your copy is stale and the replace silently deletes their text — and because your stale copy is usually the LONGER one, no guard fires and nothing tells you. A patch cannot do that: a non-empty old must match the current note EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400 that names which edit failed and which tool to re-read with, zero writes), so a concurrent write turns into a refusal you can see. Edits apply in order; an empty old appends. Wiping the note, or shrinking it below a tenth, needs allow_shrink=true — for an honest rewrite from scratch use update_step_note. Same executor/admin gate, same any-step-status generality, same closed-task 409 as update_step_note. Re-read with get_task after a refusal.",
			MCPTool:  "patch_step_note",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/gate",
			Handler:  w.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Arm a gate step: opens the reply card the owner must answer. Optional attachments ride the question (same shape as post_chat: {id} from `ocagent upload` / POST /api/chat/attachments, or inline data_b64).",
			MCPTool:  "open_gate",
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
			// task it EXECUTES (handler executor-guard, callerMayDriveTask (root CLAUDE.md「核心不變量／授權單一化」));
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
			// Executor-guarded (callerMayDriveTask (root CLAUDE.md「核心不變量／授權單一化」)); status stays derived,
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
			// admin capability excepted — root CLAUDE.md「核心不變量／授權單一化」, same as the other agent write rows).
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/artifact",
			Handler:  w.HandleAddTaskArtifactApiTasksTaskIdArtifactPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Register a deliverable (file, image, or link) onto the task's artifact set — the pinned deliverables shown on the task card. Append-only and repeatable: call it again to pin more. For a file or image, first upload the bytes via the chat-attachments upload to get an attachment id, then call this with kind=file|image and that attachment_id. For a link (e.g. a PR url) call it with kind=link and url — no upload needed. label is an optional display name (a link title such as \"PR #123\"). Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
			MCPTool:  "add_task_artifact",
		},
		{
			// Un-pin — SAME permission model as add (owner ruling 2026-07-18
			// "Agent 自己應該也要可以刪除"): requires=agent + the handler's executor
			// guard (caller == executor, admin/owner excepted — root CLAUDE.md「核心不變量／授權單一化」). The agent
			// drives it through the remove_task_artifact tool; the owner through
			// the cockpit popover.
			Method:   "DELETE",
			Path:     "/api/tasks/{task_id}/artifact/{artifact_id}",
			Handler:  w.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Un-pin (remove) one artifact from a task's artifact set — the counterpart to add_task_artifact. You may remove artifacts from a task you are the executor of (the owner/assistant may remove on any task). Give the task id and the artifact id (the id returned when it was added, or from get_task's artifacts). The underlying file blob is left intact; only the pin on the card is removed. ONLY WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen in both directions — remove is refused with the same 409 as add. So swap a deliverable BEFORE you close the task, not after; after the close it can neither be removed nor put back. Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
			MCPTool:  "remove_task_artifact",
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
			// T-6020 (owner 2026-07-26) gave all four the SAME admin_agent floor
			// relocate already had in P7c — 外包對齊正職, one floor for the whole
			// worker lifecycle. Plain agents remain 403 on every one.
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/refocus",
			Handler:  w.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Refocus (換手) an outsource worker (owner/admin agent, online-only else 409).",
			MCPTool:  "refocus_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/stop",
			Handler:  w.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Stop (停止) an outsource worker: kill + hold down (owner/admin agent).",
			MCPTool:  "stop_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/restart",
			Handler:  w.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Restart (重啟) an outsource worker that has no live session (owner/admin agent; 409 only when it is actually alive).",
			MCPTool:  "restart_outsource_worker",
		},
		{
			Method:   "POST",
			Path:     "/api/outsource-workers/{id}/model",
			Handler:  w.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Change (換 model) an outsource worker's model/effort (owner/admin agent).",
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
	}
}
