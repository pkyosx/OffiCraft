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
			Summary:  "Build identity: version + git sha + MCP catalog hash.",
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
			Summary:  "Edit settings (login TTL / handover threshold); live immediately.",
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
			// admin_agent because that is exactly the floor of the PATCH
			// /api/settings write that stores the imported theme — a caller who
			// could fetch but not store would only ever get a dead end.
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
			Summary:  "List the owner's roster (presence-derived MemberDTO[]).",
		},
		{
			Method:   "POST",
			Path:     "/api/members",
			Handler:  w.HandleHireMemberApiMembersPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Hire a member (server mints the id). Pure seam, no UI (§9.1).",
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
			Summary:  "Edit a member (name / model / effort). Blank name / bad effort → 422.",
			MCPTool:  "update_member",
		},
		{
			Method:     http.MethodPatch,
			Path:       "/api/members/{member_id}/avatar-index",
			Handler:    w.HandleUpdateMemberAvatarIndexApiMembersMemberIdAvatarIndexPatch,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Set a staff or outsource member's theme avatar index (owner only).",
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
			Summary:  "Relocate a member to a machine (placement only; never touches desired_state). Also accepts an outsource-worker id: the same move-one-agent verb relocates the worker.",
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
			Summary:  "Force-stop: robust STOP now, bypassing the graceful-stop grace.",
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
			Summary:  "Bounded LIGHT wake snapshot for a TARGET member (admin_agent+; same shape as resume_summary).",
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
			Summary:  "Post a chat message (sender = verified JWT sub; auto SSE fan-out).",
		},
		{
			Method:   "GET",
			Path:     "/api/chat",
			Handler:  w.HandleListChatApiChatGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List the chat stream (?with=<id>&limit=<n>; oldest→newest).",
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
			Summary:  "Mint a permanent single-file share link (?sig= HMAC; grants read of this one attachment only).",
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
			Summary:  "Open a reply card: an ask the owner must answer (options ≤4, [0]=AI pick). Auto-binds to your single active task's current step when unambiguous — that step (and usually the task) enters waiting_owner until the owner answers.",
			MCPTool:  "create_reply_card",
		},
		{
			Method:   "GET",
			Path:     "/api/reply-cards",
			Handler:  w.HandleListReplyCardsApiReplyCardsGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List reply cards — LIGHT rows: summary+decision digest, no body/options (?status=waiting|answered|expired; ?limit= caps; get_reply_card for full).",
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
		// T-6020 (owner 2026-07-26): the two on-server host lifecycle faces open
		// to admin_agent — installing/tearing down the server host's own warden
		// is office operations. A plain agent is still 403 (rank<2).
		{
			Method:   "POST",
			Path:     "/api/machines/{machine_id}/bootstrap-here",
			Handler:  w.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Bootstrap on server: install this machine's warden on the host.",
			MCPTool:  "install_warden_on_server_host",
		},
		{
			Method:   "POST",
			Path:     "/api/machines/{machine_id}/teardown-here",
			Handler:  w.HandleTeardownHereApiMachinesMachineIdTeardownHerePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Teardown on server: run ocwarden teardown on the server's OWN host; machine_id is not a target selector and every target is currently refused (409).",
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
			Summary:  "Whole-block replace of the user-custom additive block ({text}).",
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
		{
			Method:   "GET",
			Path:     "/api/roles",
			Handler:  w.HandleListRolesApiRolesGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "List role definitions (seed defaults + owner edits).",
			MCPTool:  "list_roles",
		},
		{
			Method:   "POST",
			Path:     "/api/roles",
			Handler:  w.HandleCreateRoleApiRolesPost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Create a custom role + its founding member (one pair per call).",
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
			Summary:  "Read a per-role insight doc (per role_key; may have a PER-ROLE factory seed).",
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
			Summary:  "Whole-doc replace of a per-role insight doc ({text}).",
			MCPTool:  "replace_insight",
		},
		{
			Method:   "POST",
			Path:     "/api/insight/{role_key}/patch",
			Handler:  w.HandlePatchInsightApiInsightRoleKeyPatchPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Patch a per-role insight doc by unique anchors ({edits:[{old,new}]}).",
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
			Summary:  "Reset a per-role insight doc to its factory seed (idempotent tombstone overlay).",
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
			Summary:  "Whole-doc replace of a per-role lessons doc ({text}).",
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
			Summary:  "Bounded LIGHT wake snapshot for the caller (identity-locked; what it carries is enumerated in the description, not here).",
			MCPTool:  "resume_summary",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary-size",
			Handler:  w.HandlePeekResumeSummarySizeApiResumeSummarySizeGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Size-only PEEK of the wake snapshot (identity-locked; overview counts/sizes + estimated_total_chars, NO content) — size resume_summary before pulling it.",
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
			Summary:  "List tasks (?executor=&type=&status=; light list items — get_task for full).",
			MCPTool:  "list_tasks",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks",
			Handler:  w.HandleCreateTaskApiTasksPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Create a task (dedupes on the manual's key; ad-hoc when type_key omitted).",
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
			// T-6020: opened to admin_agent (owner 2026-07-26). Still the only
			// non-executor status change, and still closed to plain agents —
			// an agent cannot terminate its own way out of a task.
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/terminate",
			Handler:  w.HandleTerminateTaskApiTasksTaskIdTerminatePost,
			Auth:     authGated,
			Requires: principalAdminAgent,
			Summary:  "Terminate a task (owner/admin agent; the only non-executor status change).",
			MCPTool:  "terminate_task",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/priority",
			Handler:  w.HandleSetTaskPriorityApiTasksTaskIdPriorityPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Set a task's priority (owner/admin agent any value; the executor any value on their own task — frozen included, T-6020).",
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
			Summary:  "Submit/replace the workflow plan (done steps are kept).",
			MCPTool:  "submit_plan",
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
			Summary:  "Correct a task's description (executor/admin; closed tasks included).",
			MCPTool:  "update_task_description",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/duplicate",
			Handler:  w.HandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Mark a task duplicated, pointing at the original (executor/owner; terminal).",
			MCPTool:  "mark_duplicate",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/status",
			Handler:  w.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Report a step status (pending/in_progress/done).",
			MCPTool:  "update_step_status",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/note",
			Handler:  w.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Write a step's working note (any status; wholesale replace).",
			MCPTool:  "update_step_note",
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/steps/{step_id}/gate",
			Handler:  w.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Arm a gate step: opens the reply card the owner must answer.",
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
			// task it EXECUTES (handler executor-guard, callerMayDriveTask §14);
			// owner/admin still drive any task. An outsource target still funnels
			// through the single 發包 gate (create+spawn atomicity / owner approval).
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/reassign",
			Handler:  w.HandleReassignTaskApiTasksTaskIdReassignPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Reassign a task to a member or a fresh outsource worker (the task's executor or an admin; outsource targets pass the owner-approval gate; enters the reassigning handover state).",
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
			Summary:  "Take over a reassigned task (the new executor claims it — clears the reassigning lock).",
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
			Summary:  "Register a deliverable (file/image/link) onto the task's artifact set.",
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
			Summary:  "Remove one artifact from a task's set (executor/owner/admin).",
			MCPTool:  "remove_task_artifact",
		},
		{
			Method:   "GET",
			Path:     "/api/self/task",
			Handler:  w.HandleGetMyTaskApiSelfTaskGet,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Outsource worker's claim: read the task bound to the caller.",
			MCPTool:  "get_my_task",
		},
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
			Summary:  "List task types (match by display_name/purpose; address by type_key).",
			MCPTool:  "list_task_manuals",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals",
			Handler:  w.HandleCreateTaskManualApiTaskManualsPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner/admin agent).",
			MCPTool:  "create_task_manual",
		},
		{
			Method:   "GET",
			Path:     "/api/task-manuals/{type_key}",
			Handler:  w.HandleGetTaskManualApiTaskManualsTypeKeyGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read one task manual (purpose/fields/SOP/learnings/assignee).",
			MCPTool:  "get_task_manual",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals/{type_key}",
			Handler:  w.HandleUpdateTaskManualApiTaskManualsTypeKeyPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Edit a task manual (partial; content fields agent-editable; assignee = owner/admin agent).",
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
			Summary:  "Whole-doc replace of a type's learnings (task-close write-back).",
			MCPTool:  "write_task_learnings",
		},
		{
			Method:   "POST",
			Path:     "/api/task-manuals/{type_key}/learnings/patch",
			Handler:  w.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Patch a type's learnings by unique anchors ({edits:[{old,new}]}).",
			MCPTool:  "patch_task_learnings",
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
			Summary:  "List the retained history of an editable document.",
			MCPTool:  "list_document_history",
		},
		{
			Method:   "GET",
			Path:     "/api/document-history/{kind}/{key}/seed",
			Handler:  w.HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read the shipped default of an editable document.",
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
	}
}
