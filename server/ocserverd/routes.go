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
			Method:     "GET",
			Path:       "/api/version",
			Handler:    w.HandleVersionApiVersionGet,
			Auth:       authPublic,
			Requires:   requiresPublic,
			Summary:    "Build identity: version + git sha + MCP catalog hash.",
			MCPExclude: true, // a build-identity probe, not an agent tool
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
		{
			Method:     "POST",
			Path:       "/api/mint",
			Handler:    w.HandleMintApiMintPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Owner-gated mint of a long-lived agent JWT for a member (TTL capped).",
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
			Method:     "POST",
			Path:       "/api/auth/change-password",
			Handler:    w.HandleChangePasswordApiAuthChangePasswordPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Change the owner password (verifies the current one).",
			MCPExclude: true, // the owner's credential, never an agent tool
		},
		{
			Method:     "GET",
			Path:       "/api/settings",
			Handler:    w.HandleGetSettingsApiSettingsGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Read the owner-adjustable settings.",
			MCPExclude: true, // the owner's cockpit settings, not an agent tool
		},
		{
			Method:     "PATCH",
			Path:       "/api/settings",
			Handler:    w.HandleUpdateSettingsApiSettingsPatch,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Edit settings (login TTL / handover threshold); live immediately.",
			MCPExclude: true, // the owner's cockpit settings, not an agent tool
		},
		{
			Method:     "GET",
			Path:       "/api/release/check",
			Handler:    w.HandleCheckReleaseApiReleaseCheckGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Check GitHub Releases for a newer official OffiCraft version.",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
		},
		{
			Method:     "POST",
			Path:       "/api/update/upgrade",
			Handler:    w.HandleUpgradeApiUpdateUpgradePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Trigger a software upgrade to the latest GitHub release.",
			MCPExclude: true, // the owner's explicit action — NEVER an agent tool (no self-upgrade)
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
		// Owner-facing config CRUD (the machine floor, like the members CRUD).
		// MCPExclude: UI-driven config, not an agent tool — kept off the MCP
		// surface (and out of the catalog hash) entirely.
		{
			Method:     "GET",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleListWebhooksApiMembersMemberIdWebhooksGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "List a member's webhook endpoints (WebhookEndpointDTO[]).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleCreateWebhookApiMembersMemberIdWebhooksPost,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Create a webhook endpoint (server mints the token).",
			MCPExclude: true,
		},
		{
			Method:     "PATCH",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Toggle status / edit purpose of a webhook endpoint.",
			MCPExclude: true,
		},
		{
			Method:     "DELETE",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Delete (permanently revoke) a webhook endpoint.",
			MCPExclude: true,
		},
		// Debug ring buffer — raw external payloads; owner-only (agents never
		// see another channel's unverified input through this side door).
		{
			Method:     "GET",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
			Handler:    w.HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Last 5 raw /in requests of one webhook endpoint (debug).",
			MCPExclude: true,
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
			Method:     "GET",
			Path:       "/api/chat/attachments/{attachment_id}/share-link",
			Handler:    w.HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet,
			Auth:       authGated,
			Requires:   principalMachine,
			Summary:    "Mint a permanent single-file share link (?sig= HMAC; grants read of this one attachment only).",
			MCPExclude: true, // a UI convenience seam, not an agent tool
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
		{
			Method:     "POST",
			Path:       "/api/reply-cards/{card_id}/answer",
			Handler:    w.HandleAnswerReplyCardApiReplyCardsCardIdAnswerPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Answer a waiting reply card — the only positive close.",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
		},
		{
			Method:     "PUT",
			Path:       "/api/reply-cards/{card_id}/answer",
			Handler:    w.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Revise an answered card's answer (重新決定): stays answered.",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
		},
		{
			Method:     "POST",
			Path:       "/api/reply-cards/{card_id}/expire",
			Handler:    w.HandleExpireReplyCardApiReplyCardsCardIdExpirePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Mark a waiting card expired (標為過期): terminal, not an answer.",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
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
		{
			Method:     "POST",
			Path:       "/api/machines/{machine_id}/bootstrap-here",
			Handler:    w.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Bootstrap on server: install this machine's warden on the host.",
			MCPExclude: true, // a privileged host action, not an agent tool
		},
		{
			Method:     "POST",
			Path:       "/api/machines/{machine_id}/teardown-here",
			Handler:    w.HandleTeardownHereApiMachinesMachineIdTeardownHerePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Teardown on server: tear this machine's warden down on the host.",
			MCPExclude: true, // a privileged host action, not an agent tool
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
			Method:     "POST",
			Path:       "/api/machines/{member_id}/upgrade",
			Handler:    w.HandleUpgradeMachineApiMachinesMemberIdUpgradePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Upgrade a machine: kick its warden's self-update NOW.",
			MCPExclude: true, // a cockpit host-lifecycle click, not an agent tool
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
			Method:   "GET",
			Path:     "/api/lessons/{role_key}/{task_type}",
			Handler:  w.HandleGetLessonsApiLessonsRoleKeyTaskTypeGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Read a per-role lessons doc (per role_key; overlay ⊕ seed).",
			MCPTool:  "get_lessons",
		},
		{
			Method:   "POST",
			Path:     "/api/lessons/{role_key}/{task_type}",
			Handler:  w.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Whole-doc replace of a per-role lessons doc ({text}).",
			MCPTool:  "replace_lessons",
		},
		{
			Method:   "POST",
			Path:     "/api/lessons/{role_key}/{task_type}/patch",
			Handler:  w.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Patch a per-role lessons doc by unique anchors ({edits:[{old,new}]}).",
			MCPTool:  "patch_lessons",
		},
		{
			Method:   "GET",
			Path:     "/api/resume-summary",
			Handler:  w.HandleResumeSummaryApiResumeSummaryGet,
			Auth:     authGated,
			Requires: principalMachine,
			Summary:  "Bounded LIGHT wake snapshot for the caller (identity-locked; recent chat + light task rows + size overview).",
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
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/terminate",
			Handler:    w.HandleTerminateTaskApiTasksTaskIdTerminatePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Terminate a task (owner; the only owner status change).",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
		},
		{
			Method:   "POST",
			Path:     "/api/tasks/{task_id}/priority",
			Handler:  w.HandleSetTaskPriorityApiTasksTaskIdPriorityPost,
			Auth:     authGated,
			Requires: principalAgent,
			Summary:  "Set a task's priority (owner any value; the executor high|mid|low on their own task; frozen stays owner-only).",
			MCPTool:  "set_task_priority",
		},
		{
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/message",
			Handler:    w.HandlePostTaskMessageApiTasksTaskIdMessagePost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Message the task's executor (owner; task context auto-attached).",
			MCPExclude: true, // the owner's cockpit action, not an agent tool
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
			// preview's worker twin; no token minted). Owner-only: the text
			// embeds the full task + manual. A cockpit read face → MCPExclude.
			Method:     "GET",
			Path:       "/api/outsource-workers/{id}/boot-context",
			Handler:    w.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Read an outsource worker's boot-context preview (owner-only).",
			MCPExclude: true,
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
			// All owner-only + MCPExclude (relocate above dropped to admin_agent
			// in P7c; these stay owner-only until their own alignment ruling).
			Method:     "POST",
			Path:       "/api/outsource-workers/{id}/refocus",
			Handler:    w.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Refocus (換手) an outsource worker (owner-only, online-only else 409).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/outsource-workers/{id}/stop",
			Handler:    w.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Stop (停止) an outsource worker: kill + hold down (owner-only).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/outsource-workers/{id}/restart",
			Handler:    w.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Restart (重啟) a stopped outsource worker (owner-only; 409 if not stopped).",
			MCPExclude: true,
		},
		{
			Method:     "POST",
			Path:       "/api/outsource-workers/{id}/model",
			Handler:    w.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Change (換 model) an outsource worker's model/effort (owner-only).",
			MCPExclude: true,
		},
		// ── Task manuals (M3) — agents create manuals + edit the CONTENT fields
		// (purpose / fields / SOP / learnings); the assignee face and delete
		// stay owner-only governance (the in-handler assignee gate answers 403)
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
			Summary:  "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner-only).",
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
			Summary:  "Edit a task manual (partial; content fields agent-editable; assignee = owner-only).",
			MCPTool:  "update_task_manual",
		},
		{
			Method:     "DELETE",
			Path:       "/api/task-manuals/{type_key}",
			Handler:    w.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete,
			Auth:       authGated,
			Requires:   principalOwner,
			Summary:    "Delete a task type (open tasks of the type → 409).",
			MCPExclude: true, // the owner's settings action, not an agent tool
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
	}
}
