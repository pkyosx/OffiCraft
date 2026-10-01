package main

import "net/http"

const (
	authPublic = "public"
	authGated  = "gated"
)

type RouteSpec struct {
	Method string
	// Path's {param} names must match spec/openapi.json: the generated
	// wrapper reads them via r.PathValue.
	Path string

	Handler http.HandlerFunc

	Auth string
	// Requires is the MINIMUM principal class this route admits (ladder
	// machine < agent < admin_agent < owner).
	Requires principalClass
	// MCPExclude keeps the route off the MCP tool surface and out of the
	// catalog hash. It is a discoverability fact, NOT authz: the route stays
	// callable over REST by anyone who clears Requires.
	MCPExclude bool
	// MCPTool is the explicit MCP tool name override (paths carrying a
	// {param}).
	MCPTool string
	// ShareSig admits a ?sig= share credential (sharesig.go) as a third auth
	// path on THIS row only (precedence: Authorization header → ?token= →
	// ?sig=), and IS the verifier. nil = this row never consults sigs.
	ShareSig shareSigVerifier
}

// A shareSigVerifier's subject is everything the response depends on, so a
// recipient cannot swap an address or relabel a column and still hold a
// minted signature. It takes the whole signing-key RING: a sig names no key,
// so it verifies under any key still in the ring and dies with the key that
// made it (sharesig.go).
type shareSigVerifier func(keys *keyring, r *http.Request, sig string) bool

func verifyAttachmentShareSig(keys *keyring, r *http.Request, sig string) bool {
	return verifyShareSigAnyKey(keys, r.PathValue("attachment_id"), sig)
}

// Read RAW, never trimmed: a padded address is a different address.
func verifyDiffShareSig(keys *keyring, r *http.Request, sig string) bool {
	q := r.URL.Query()
	return verifyDiffSigAnyKey(keys,
		q.Get(diffParamBefore), q.Get(diffParamAfter),
		q.Get(diffParamLabelBefor), q.Get(diffParamLabelAfter), sig)
}

type routeDef struct {
	Method     string
	Path       string
	Handler    http.HandlerFunc
	MCPExclude bool
	MCPTool    string
	ShareSig   shareSigVerifier
}

// Public and Gated are routeRow's only constructors, so a bare RouteSpec
// written into the table is a TYPE error rather than a row that silently
// serves with an empty auth label.
type routeRow struct{ RouteSpec }

func (d routeDef) row(auth string, requires principalClass) routeRow {
	return routeRow{RouteSpec{
		Method: d.Method, Path: d.Path, Handler: d.Handler,
		Auth: auth, Requires: requires,
		MCPExclude: d.MCPExclude, MCPTool: d.MCPTool, ShareSig: d.ShareSig,
	}}
}

func Public(d routeDef) routeRow { return d.row(authPublic, requiresPublic) }

func Gated(c principalClass, d routeDef) routeRow { return d.row(authGated, c) }

// Row ORDER is a wire fact: the MCP tools/list order mirrors this table,
// spec/openapi.json's x-mcp.order and conformance/routes_manifest.json, and
// conformance asserts all three agree element-wise. x-mcp.order must be the
// consecutive range 0..N-1, so a new tool is APPENDED at the end — inserting
// one mid-table renumbers every tool after it.
func routeSpecs(w *ServerInterfaceWrapper) []RouteSpec {
	rows := []routeRow{
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/health",
			Handler:    w.HandleHealthApiHealthGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			// On the MCP surface as get_version by owner ruling
			// rc-2089ff8e34bf: every member settles "has my change shipped?"
			// against the running git_sha. Deliberately not merged with
			// check_release (a different question with a different data
			// source).
			Method:  "GET",
			Path:    "/api/version",
			Handler: w.HandleVersionApiVersionGet,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/health",
			Handler:    w.HandleHealthHealthGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/version",
			Handler:    w.HandleProbeVersionVersionGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "POST",
			Path:       "/api/login",
			Handler:    w.HandleLoginApiLoginPost,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/mint",
			Handler: w.HandleMintApiMintPost,
			// Owner ruling T-6020: stays owner-only — issuing an identity is
			// self-escalation.
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/auth/status",
			Handler:    w.HandleAuthStatusApiAuthStatusGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/auth/set-password",
			Handler: w.HandleSetPasswordApiAuthSetPasswordPost,
			// Public: the one-shot claim token IS the gate (lifecycle.md
			// §1.3).
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/auth/change-password",
			Handler: w.HandleChangePasswordApiAuthChangePasswordPost,
			// Owner ruling T-6020: stays owner-only — the owner's PERSONAL
			// account credential.
			MCPExclude: true,
		}),
		// Owner second factor: principalOwner + MCPExclude — these decide how
		// the OWNER authenticates, and an admin_agent reaching them could
		// weaken the credential that governs it.
		Gated(principalOwner, routeDef{
			Method:     "GET",
			Path:       "/api/auth/mfa",
			Handler:    w.HandleMfaStateApiAuthMfaGet,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/offer",
			Handler:    w.HandleMfaOfferApiAuthMfaOfferPost,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/enroll",
			Handler:    w.HandleMfaEnrollApiAuthMfaEnrollPost,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/activate",
			Handler:    w.HandleMfaActivateApiAuthMfaActivatePost,
			MCPExclude: true,
		}),
		// Signing-key ring: principalOwner + MCPExclude — the ring
		// authenticates every caller; an admin_agent reaching it could rotate
		// the key that governs it, or remove the key its own credential is
		// signed under.
		Gated(principalOwner, routeDef{
			Method:     "GET",
			Path:       "/api/auth/signing-keys",
			Handler:    w.HandleSigningKeysApiAuthSigningKeysGet,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/rotate",
			Handler:    w.HandleSigningKeyRotateApiAuthSigningKeysRotatePost,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/{key_id}/remove",
			Handler:    w.HandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost,
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/disable",
			Handler:    w.HandleMfaDisableApiAuthMfaDisablePost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/settings",
			Handler: w.HandleGetSettingsApiSettingsGet,
			MCPTool: "get_settings",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "PATCH",
			Path:    "/api/settings",
			Handler: w.HandleUpdateSettingsApiSettingsPatch,
			MCPTool: "update_settings",
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodGet, Path: "/api/push/public-key", Handler: w.HandleGetPushPublicKeyApiPushPublicKeyGet,
			// Owner ruling T-6020: stays owner-only — browser Web Push is the
			// owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodPost, Path: "/api/push/subscription", Handler: w.HandleCreatePushSubscriptionApiPushSubscriptionPost,
			// Same T-6020 ruling.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodDelete, Path: "/api/push/subscription", Handler: w.HandleDeletePushSubscriptionApiPushSubscriptionDelete,
			// Same T-6020 ruling.
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/release/check",
			Handler: w.HandleCheckReleaseApiReleaseCheckGet,
			MCPTool: "check_release",
		}),
		Gated(principalAdminAgent, routeDef{
			// The floor must stay equal to PUT /api/themes/{theme_id}'s: a
			// caller who could fetch but not store would only ever reach a
			// dead end.
			Method:     "POST",
			Path:       "/api/theme/fetch",
			Handler:    w.HandleFetchThemeApiThemeFetchPost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/update/upgrade",
			Handler: w.HandleUpgradeApiUpdateUpgradePost,
			MCPTool: "upgrade_station",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/events",
			Handler:    w.HandleEventsApiEventsGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/mcp",
			Handler:    w.HandleMcpApiMcpPost,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/members",
			Handler: w.HandleListMembersApiMembersGet,
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/members",
			Handler: w.HandleHireMemberApiMembersPost,
			MCPTool: "hire_member",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}",
			Handler: w.HandleGetMemberApiMembersMemberIdGet,
			MCPTool: "get_member",
		}),
		// Owner ruling T-5336: update_member stays at the machine floor
		// (roster members trust each other; an edit is reversible). The
		// asymmetry with dismiss_member's admin_agent floor is accepted, not
		// an oversight. Raising this row needs a fresh owner ruling.
		Gated(principalMachine, routeDef{
			Method:  "PATCH",
			Path:    "/api/members/{member_id}",
			Handler: w.HandleUpdateMemberApiMembersMemberIdPatch,
			MCPTool: "update_member",
		}),
		Gated(principalOwner, routeDef{
			Method:  http.MethodPut,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandlePutMemberAvatarApiMembersMemberIdAvatarPut,
			// Owner ruling T-c826: owner-only and off the MCP surface — a
			// personal avatar is owner-managed member identity.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  http.MethodDelete,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete,
			// Same T-c826 ruling as PUT.
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/activate",
			Handler: w.HandleActivateMemberApiMembersMemberIdActivatePost,
			MCPTool: "activate_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/relocate",
			Handler: w.HandleRelocateMemberApiMembersMemberIdRelocatePost,
			MCPTool: "relocate_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/deactivate",
			Handler: w.HandleDeactivateMemberApiMembersMemberIdDeactivatePost,
			MCPTool: "deactivate_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/force-stop",
			Handler: w.HandleForceStopMemberApiMembersMemberIdForceStopPost,
			MCPTool: "force_stop_member",
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/cost/reset",
			Handler: w.HandleResetCostApiMembersMemberIdCostResetPost,
			// Owner ruling rc-7dea0deefa63: principalOwner and MCP-excluded,
			// unlike its admin_agent neighbours — it irreversibly destroys
			// the owner's own spend record.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/accounts/cost/reset",
			Handler: w.HandleResetAccountCostApiAccountsCostResetPost,
			// Same owner-only ruling as the per-actor row above. Owner ruling
			// rc-5c5d7c7c6dcd: it touches NO actor — it writes the account
			// accumulator and nothing else. The account key rides in the
			// BODY, not the path: it contains '/' and '@', and an encoded
			// slash a proxy decodes would silently retarget an irreversible
			// call.
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/waking",
			Handler: w.HandleReportWakingApiSelfWakingPost,
			MCPTool: "report_waking",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/stopping",
			Handler: w.HandleReportStoppingApiSelfStoppingPost,
			MCPTool: "report_stopping",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/stopped",
			Handler: w.HandleReportStoppedApiSelfStoppedPost,
			MCPTool: "report_stopped",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/refocus",
			Handler: w.HandleRestartSelfApiSelfRefocusPost,
			MCPTool: "restart_self",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/refocus",
			Handler: w.HandleRefocusMemberApiMembersMemberIdRefocusPost,
			MCPTool: "refocus_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/members/{member_id}",
			Handler: w.HandleDismissMemberApiMembersMemberIdDelete,
			MCPTool: "dismiss_member",
		}),
		// Owner ruling T-5336: all four webhook verbs at admin_agent, reads
		// included — every response carries WebhookEndpointDTO's PLAINTEXT
		// token, the whole credential of the public /in inlet.
		Gated(principalAdminAgent, routeDef{
			Method:     "GET",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleListWebhooksApiMembersMemberIdWebhooksGet,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleCreateWebhookApiMembersMemberIdWebhooksPost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "PATCH",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "DELETE",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
			Handler: w.HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet,
			MCPTool: "list_webhook_requests",
		}),
		// On the MCP surface by owner ruling (2026-08-19), unlike the webhook
		// rows: a scheduled message exists to WAKE an AI member, so the admin
		// assistant must be able to set one up. The floor stays
		// principalAdminAgent.
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/scheduled-messages",
			Handler: w.HandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet,
			MCPTool: "list_scheduled_messages",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/scheduled-messages",
			Handler: w.HandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost,
			MCPTool: "create_scheduled_message",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "PATCH",
			Path:    "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler: w.HandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch,
			MCPTool: "update_scheduled_message",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler: w.HandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete,
			MCPTool: "delete_scheduled_message",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/resume-summary",
			Handler: w.HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet,
			MCPTool: "get_member_resume_summary",
		}),
		// Webhook inlet — PUBLIC: identity is the ?t= token alone; accepted
		// and ignored calls answer the same silent 200 so existence never
		// leaks.
		Public(routeDef{
			Method:     "POST",
			Path:       "/in",
			Handler:    w.HandleReceiveWebhookInPost,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/chat",
			Handler: w.HandlePostChatApiChatPost,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat",
			Handler: w.HandleListChatApiChatGet,
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/chat/attachment/{attachment_id}",
			Handler:    w.HandleGetChatAttachmentApiChatAttachmentAttachmentIdGet,
			MCPExclude: true,
			MCPTool:    "get_chat_attachment",
			ShareSig:   verifyAttachmentShareSig,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat/attachments/{attachment_id}/share-link",
			Handler: w.HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet,
			// On the MCP surface on purpose: an agent must be able to hand
			// over a usable link. Minted links carry no credential and no
			// expiry, and die only with their signing key (a coarse
			// revocation) — read sharesig.go before widening this seam.
			MCPTool: "get_chat_attachment_share_link",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat/attachments",
			Handler: w.HandleListChatAttachmentsApiChatAttachmentsGet,
		}),
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/chat/attachments",
			Handler:    w.HandleUploadChatAttachmentApiChatAttachmentsPost,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/chat/mark-read",
			Handler: w.HandleMarkChatReadApiChatMarkReadPost,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat/reads",
			Handler: w.HandleListChatReadsApiChatReadsGet,
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/reply-cards",
			Handler: w.HandleCreateReplyCardApiReplyCardsPost,
			MCPTool: "create_reply_card",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/reply-cards",
			Handler: w.HandleListReplyCardsApiReplyCardsGet,
			MCPTool: "list_reply_cards",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/reply-cards/count",
			Handler:    w.HandleReplyCardCountApiReplyCardsCountGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/chat/unread-count",
			Handler:    w.HandleChatUnreadCountApiChatUnreadCountGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/diff",
			Handler: w.HandleGetDiffApiDiffGet,

			MCPExclude: true,

			ShareSig: verifyDiffShareSig,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/diff/share-link",
			Handler: w.HandleGetDiffShareLinkApiDiffShareLinkGet,
			MCPTool: "get_diff_share_link",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/reply-cards/{card_id}",
			Handler: w.HandleGetReplyCardApiReplyCardsCardIdGet,
			MCPTool: "get_reply_card",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/reply-cards/{card_id}/answer",
			Handler: w.HandleAnswerReplyCardApiReplyCardsCardIdAnswerPost,
			MCPTool: "answer_reply_card",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "PUT",
			Path:    "/api/reply-cards/{card_id}/answer",
			Handler: w.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut,
			MCPTool: "reanswer_reply_card",
		}),
		// Owner ruling rc-3ff94b116970: the owner, an admin_agent or the
		// card's OWN author may expire it. The author exception is a per-card
		// fact, so it lives in the handler and the floor is principalAgent.
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/reply-cards/{card_id}/expire",
			Handler: w.HandleExpireReplyCardApiReplyCardsCardIdExpirePost,
			MCPTool: "expire_reply_card",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/agent/context",
			Handler: w.HandleIngestAgentContextApiAgentContextPost,
			MCPTool: "ingest_agent_context",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/monitoring/telemetry",
			Handler: w.HandleIngestTelemetryApiMonitoringTelemetryPost,
			MCPTool: "ingest_telemetry",
		}),
		// principalMachine so the warden clears the floor; the handler admits only
		// the login's own machine.
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/monitoring/runtime-login",
			Handler:    w.HandleReportRuntimeLoginApiMonitoringRuntimeLoginPost,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/monitoring",
			Handler: w.HandleGetMonitoringApiMonitoringGet,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/backup-health",
			Handler: w.HandleGetBackupHealthApiBackupHealthGet,
			// Deliberately NOT an MCP tool: a cockpit indicator. Publishing
			// it as a tool is a separate owner decision.
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "PATCH",
			Path:    "/api/accounts/{account_id}",
			Handler: w.HandleUpdateAccountApiAccountsAccountIdPatch,
			MCPTool: "update_account",
		}),
		Gated(principalMachine, routeDef{
			Method:  "PATCH",
			Path:    "/api/machines/{machine_id}",
			Handler: w.HandleUpdateMachineApiMachinesMachineIdPatch,
			MCPTool: "update_machine",
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/install.sh",
			Handler:    w.HandleInstallScriptInstallShGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/machines",
			Handler: w.HandleListMachinesApiMachinesGet,
			MCPTool: "list_machines",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/machines",
			Handler:    w.HandleOnboardMachineApiMachinesPost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "GET",
			Path:       "/api/machines/{machine_id}/boot-command",
			Handler:    w.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/machines/claim",
			Handler: w.HandleClaimMachineTokenApiMachinesClaimPost,
			// Public: the one-time claim code IS the gate (lifecycle.md
			// §1.3).
			MCPExclude: true,
		}),
		// principalMachine, not principalAgent: the warden ranks machine, so
		// a higher floor would lock it out. The warden-only property lives in
		// the handler.
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/machines/renew-credential",
			Handler:    w.HandleRenewMachineCredentialApiMachinesRenewCredentialPost,
			MCPExclude: true,
		}),
		// Same floor as the renew seam above: the same warden asks it one
		// poll earlier.
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/machines/credential-policy",
			Handler:    w.HandleMachineCredentialPolicyApiMachinesCredentialPolicyGet,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{machine_id}/bootstrap-here",
			Handler: w.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost,
			MCPTool: "install_warden_on_server_host",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{machine_id}/teardown-here",
			Handler: w.HandleTeardownHereApiMachinesMachineIdTeardownHerePost,
			MCPTool: "uninstall_warden_on_server_host",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{member_id}/uninstall",
			Handler: w.HandleUninstallMachineApiMachinesMemberIdUninstallPost,
			MCPTool: "uninstall_machine",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{member_id}/upgrade",
			Handler: w.HandleUpgradeMachineApiMachinesMemberIdUpgradePost,
			MCPTool: "upgrade_warden",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/machines/{machine_id}/runtime-login",
			Handler:    w.HandleStartRuntimeLoginApiMachinesMachineIdRuntimeLoginPost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "GET",
			Path:       "/api/machines/{machine_id}/runtime-login/{login_id}",
			Handler:    w.HandleGetRuntimeLoginApiMachinesMachineIdRuntimeLoginLoginIdGet,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/machines/{machine_id}/runtime-login/{login_id}/code",
			Handler:    w.HandleSubmitRuntimeLoginCodeApiMachinesMachineIdRuntimeLoginLoginIdCodePost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/machines/{machine_id}/runtime-login/{login_id}/cancel",
			Handler:    w.HandleCancelRuntimeLoginApiMachinesMachineIdRuntimeLoginLoginIdCancelPost,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/machines/{member_id}",
			Handler: w.HandleDeleteMachineApiMachinesMemberIdDelete,
			MCPTool: "delete_machine",
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/warden/binary",
			Handler:    w.HandleWardenBinaryApiWardenBinaryGet,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/agent/binary",
			Handler:    w.HandleAgentBinaryApiAgentBinaryGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/global-context",
			Handler: w.HandleGetGlobalContextApiGlobalContextGet,
			MCPTool: "get_global_context",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/global-context",
			Handler: w.HandleReplaceGlobalContextApiGlobalContextPost,
			MCPTool: "replace_global_context",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/global-context/reset",
			Handler: w.HandleResetGlobalContextApiGlobalContextResetPost,
			MCPTool: "reset_global_context",
		}),
		// Boot-context documents: WRITE at admin_agent because this text
		// lands in EVERY agent's boot context, and a broken 啟動步驟 fails
		// silently (an agent that never boots cannot report it) — which is
		// also why the reset route must work from the cockpit alone.
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/system-interaction",
			Handler: w.HandleGetSystemInteractionApiSystemInteractionGet,
			MCPTool: "get_system_interaction",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/system-interaction",
			Handler: w.HandleReplaceSystemInteractionApiSystemInteractionPost,
			MCPTool: "replace_system_interaction",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/system-interaction/reset",
			Handler: w.HandleResetSystemInteractionApiSystemInteractionResetPost,
			MCPTool: "reset_system_interaction",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/boot-sequence/{runtime_key}",
			Handler: w.HandleGetBootSequenceApiBootSequenceRuntimeKeyGet,
			MCPTool: "get_boot_sequence",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-sequence/{runtime_key}",
			Handler: w.HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost,
			MCPTool: "replace_boot_sequence",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-sequence/{runtime_key}/reset",
			Handler: w.HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost,
			MCPTool: "reset_boot_sequence",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/offboard",
			Handler: w.HandleGetOffboardApiOffboardGet,
			MCPTool: "get_offboard",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/offboard",
			Handler: w.HandleReplaceOffboardApiOffboardPost,
			MCPTool: "replace_offboard",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/offboard/reset",
			Handler: w.HandleResetOffboardApiOffboardResetPost,
			MCPTool: "reset_offboard",
		}),
		// Owner ruling rc-88e4ab40fe1d: one GENERIC route instead of a named
		// trio per document (each trio adds tools to every agent's list).
		// Floors are the named routes' floors. Read-only documents are
		// refused on the write path (bootDocReadOnlyRefusal, 405), not by a
		// floor — no principal can pass it.
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/boot-docs/{kind}/{key}",
			Handler: w.HandleGetBootDocApiBootDocsKindKeyGet,
			MCPTool: "get_boot_doc",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-docs/{kind}/{key}",
			Handler: w.HandleReplaceBootDocApiBootDocsKindKeyPost,
			MCPTool: "replace_boot_doc",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-docs/{kind}/{key}/reset",
			Handler: w.HandleResetBootDocApiBootDocsKindKeyResetPost,
			MCPTool: "reset_boot_doc",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/roles",
			Handler: w.HandleListRolesApiRolesGet,
			MCPTool: "list_roles",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/doc-sizes",
			Handler: w.HandlePeekDocSizesApiDocSizesGet,
			MCPTool: "peek_doc_sizes",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles",
			Handler: w.HandleCreateRoleApiRolesPost,
			MCPTool: "create_role",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/roles/{role}",
			Handler: w.HandleGetRoleApiRolesRoleGet,
			MCPTool: "get_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles/{role}",
			Handler: w.HandleUpdateRoleApiRolesRolePost,
			MCPTool: "update_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles/{role}/reset",
			Handler: w.HandleResetRoleApiRolesRoleResetPost,
			MCPTool: "reset_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/roles/{role}",
			Handler: w.HandleDeleteRoleApiRolesRoleDelete,
			MCPTool: "delete_role",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/insight/{role_key}",
			Handler: w.HandleGetInsightApiInsightRoleKeyGet,
			// Owner ruling rc-dc171587220c: READ stays on the machine floor —
			// insight is separate, not private.
			MCPTool: "get_insight",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/insight/{role_key}",
			Handler: w.HandleReplaceInsightApiInsightRoleKeyPost,
			// principalAgent: per-ROLE authz is in the handler
			// (insightWriteAuthz), and the row must not declare a floor lower
			// than that gate.
			MCPTool: "replace_insight",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/insight/{role_key}/patch",
			Handler: w.HandlePatchInsightApiInsightRoleKeyPatchPost,
			MCPTool: "patch_insight",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/insight/{role_key}/reset",
			Handler: w.HandleResetInsightApiInsightRoleKeyResetPost,
			// Same floor and handler gate as the other insight writes —
			// deliberately not reset_role's admin_agent floor.
			MCPTool: "reset_insight",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/resume-summary",
			Handler: w.HandleResumeSummaryApiResumeSummaryGet,
			MCPTool: "resume_summary",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/resume-summary-size",
			Handler: w.HandlePeekResumeSummarySizeApiResumeSummarySizeGet,
			MCPTool: "peek_resume_summary_size",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/bootstrap",
			Handler:    w.HandleBootstrapApiBootstrapPost,
			MCPExclude: true,
		}),
		// Task agent-write rows: the floor is only a floor — the real gate is
		// the handler's executor guard (caller == executor, admin capability
		// excepted, §14).
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks",
			Handler: w.HandleListTasksApiTasksGet,
			MCPTool: "list_tasks",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks",
			Handler: w.HandleCreateTaskApiTasksPost,
			MCPTool: "create_task",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/tasks/count",
			Handler:    w.HandleTaskCountApiTasksCountGet,
			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks/{task_id}",
			Handler: w.HandleGetTaskApiTasksTaskIdGet,
			MCPTool: "get_task",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-done",
			Handler: w.HandleMarkTaskDoneApiTasksTaskIdMarkDonePost,
			MCPTool: "mark_task_done",
		}),
		Gated(principalAgent, routeDef{
			// Owner ruling rc-b896e3f641e7: a staff member may terminate its
			// OWN task; the real gate is callerMayTerminateTask.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-terminated",
			Handler: w.HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost,
			MCPTool: "mark_task_terminated",
		}),
		Gated(principalAdminAgent, routeDef{
			// The ONLY one of the four whose floor gates by itself: the
			// task's own executor is a 403 on purpose, or it would have
			// mark_task_done with no precondition.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/force-done",
			Handler: w.HandleForceTaskDoneApiTasksTaskIdForceDonePost,
			MCPTool: "force_task_done",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/priority",
			Handler: w.HandleSetTaskPriorityApiTasksTaskIdPriorityPost,
			MCPTool: "set_task_priority",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/message",
			Handler: w.HandlePostTaskMessageApiTasksTaskIdMessagePost,
			MCPTool: "post_task_message",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/plan",
			Handler: w.HandleSubmitTaskPlanApiTasksTaskIdPlanPost,
			MCPTool: "submit_plan",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}",
			Handler: w.HandleUpdateTaskApiTasksTaskIdPost,
			MCPTool: "update_task",
		}),
		// Executor-guarded; the task's CREATOR gets no standing from having
		// created it (owner ruling).
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/description",
			Handler: w.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost,
			// Folded into update_task: the ROUTE stays for the frontend and
			// existing HTTP clients, the TOOL does not.
			MCPExclude: true,
		}),
		// A blank title is a 400, not a clear, as create_task refuses one too
		// (owner ruling rc-796541192519).
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/title",
			Handler: w.HandleUpdateTaskTitleApiTasksTaskIdTitlePost,

			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-duplicated",
			Handler: w.HandleMarkTaskDuplicatedApiTasksTaskIdMarkDuplicatedPost,
			MCPTool: "mark_task_duplicated",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/status",
			Handler: w.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost,
			MCPTool: "update_step_status",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/note",
			Handler: w.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost,
			MCPTool: "update_step_note",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/note/patch",
			Handler: w.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost,
			MCPTool: "patch_step_note",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks/{task_id}/steps/{step_id}",
			Handler: w.HandleGetTaskStepApiTasksTaskIdStepsStepIdGet,
			MCPTool: "get_task_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/deps",
			Handler: w.HandleSetTaskDepsApiTasksTaskIdDepsPost,
			MCPTool: "set_task_deps",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/reassign",
			Handler: w.HandleReassignTaskApiTasksTaskIdReassignPost,
			MCPTool: "reassign_task",
		}),
		Gated(principalAgent, routeDef{
			// Guarded by callerMayClaimTask: the SUCCESSOR, not the current
			// executor.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/claim",
			Handler: w.HandleClaimTaskApiTasksTaskIdClaimPost,
			MCPTool: "claim_task",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/artifact",
			Handler: w.HandleAddTaskArtifactApiTasksTaskIdArtifactPost,
			MCPTool: "add_task_artifact",
		}),
		Gated(principalAgent, routeDef{
			// Same permission model as add (owner ruling 2026-07-18).
			Method:  "DELETE",
			Path:    "/api/tasks/{task_id}/artifact/{artifact_id}",
			Handler: w.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete,
			MCPTool: "remove_task_artifact",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks/{task_id}/artifacts",
			Handler: w.HandleListTaskArtifactsApiTasksTaskIdArtifactsGet,
			MCPTool: "list_task_artifacts",
		}),
		Gated(principalAgent, routeDef{
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/artifacts/upload",
			Handler:    w.HandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost,
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/artifact/{artifact_id}/replace/upload",
			Handler:    w.HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost,
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/artifact/{artifact_id}/replace",
			Handler: w.HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost,
			MCPTool: "replace_task_artifact",
		}),
		Gated(principalAgent, routeDef{
			// MCPExclude by decision (T-60). Deliberately carries NEITHER the
			// writes' executor guard NOR their terminal-task 409 (owner
			// ruling, argued at artifactOnTask in api_tasks.go — read it
			// before "fixing" this door).
			Method:     "GET",
			Path:       "/api/tasks/{task_id}/artifact/{artifact_id}/history",
			Handler:    w.HandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet,
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/outsource-workers/{id}/boot-context",
			Handler: w.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet,
			MCPTool: "get_outsource_worker_boot_context",
		}),
		// Owner ruling rc-376a41719e62: a worker verb sits at the SAME floor
		// as its staff twin (mira's admin_agent rank is for acts the owner
		// delegates). So worker verbs have no rows of their own: the model
		// edit, /deactivate, /force-stop, /accelerated-stop, /refocus,
		// /relocate and /activate ride the staff {member_id} rows, which also
		// accept a worker id (ow-…). Only the model edit's floor moved; do
		// not "finish the job" by lowering refocus / relocate / stop /
		// restart.
		//
		// Task manuals: the assignee field and delete are GOVERNANCE
		// (admin_agent); the in-handler assignee gate answers 403 below that
		// floor.
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/task-manuals",
			Handler: w.HandleListTaskManualsApiTaskManualsGet,
			MCPTool: "list_task_manuals",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals",
			Handler: w.HandleCreateTaskManualApiTaskManualsPost,
			MCPTool: "create_task_manual",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleGetTaskManualApiTaskManualsTypeKeyGet,
			MCPTool: "get_task_manual",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleUpdateTaskManualApiTaskManualsTypeKeyPost,
			MCPTool: "update_task_manual",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete,
			MCPTool: "delete_task_manual",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}/reset",
			Handler: w.HandleResetTaskManualApiTaskManualsTypeKeyResetPost,
			MCPTool: "reset_task_manual",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}/sop/patch",
			Handler: w.HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost,
			MCPTool: "patch_task_sop",
		}),
		// Document history: READING is an MCP tool, restoring is not (owner
		// rulings rc-b5fd1135e2dd, rc-b7d29de0eb9c).
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}",
			Handler: w.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet,
			MCPTool: "list_document_history",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}/seed",
			Handler: w.HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet,
			MCPTool: "get_document_seed",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}/{id}",
			Handler: w.HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet,
			MCPTool: "get_document_version",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/document-history/{kind}/{key}/{id}/restore",
			Handler: w.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost,

			MCPExclude: true,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/docs",
			Handler: w.HandleListDocsApiDocsGet,
			MCPTool: "list_docs",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/docs/{slug}",
			Handler: w.HandleGetDocApiDocsSlugGet,
			MCPTool: "get_doc",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/docs/assets/{name}",
			Handler:    w.HandleGetDocAssetApiDocsAssetsNameGet,
			MCPExclude: true,
		}),
		// Custom themes: on the MCP surface by owner ruling rc-32ed1bfba080 —
		// and that ruling is why the list answers {id, name} only. A
		// bundle carries its images; restoring whole bundles in the list
		// would undo what made these tools callable.
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/themes",
			Handler: w.HandleListThemesApiThemesGet,
			MCPTool: "list_themes",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandleGetThemeApiThemesThemeIdGet,
			MCPTool: "get_theme",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  http.MethodPut,
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandlePutThemeApiThemesThemeIdPut,
			MCPTool: "put_theme",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandleDeleteThemeApiThemesThemeIdDelete,
			MCPTool: "delete_theme",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/accelerated-stop",
			Handler: w.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost,
			MCPTool: "accelerated_stop_member",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore",
			Handler: w.HandleWriteLoreEntryApiLorePost,
			MCPTool: "write_lore_entry",
		}),
		Gated(principalAgent, routeDef{
			Method:  "GET",
			Path:    "/api/lore",
			Handler: w.HandleListLoreEntriesApiLoreGet,
			MCPTool: "list_lore_entries",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/state",
			Handler: w.HandleSetLoreEntryStateApiLoreEntryIdStatePost,
			MCPTool: "set_lore_entry_state",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/bump",
			Handler: w.HandleBumpLoreEntryApiLoreEntryIdBumpPost,
			MCPTool: "bump_lore_entry",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps",
			Handler: w.HandleInsertTaskStepApiTasksTaskIdStepsPost,
			MCPTool: "insert_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/delete",
			Handler: w.HandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost,
			MCPTool: "delete_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/reorder",
			Handler: w.HandleReorderTaskStepsApiTasksTaskIdStepsReorderPost,
			MCPTool: "reorder_steps",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/scope",
			Handler: w.HandleSetLoreEntryScopeApiLoreEntryIdScopePost,
			MCPTool: "set_lore_entry_scope",
		}),
		Gated(principalAdminAgent, routeDef{
			// Same floor as the outsource preview: the text embeds the full
			// role definition and the member's 傳承.
			Method:  "GET",
			Path:    "/api/members/{member_id}/boot-context",
			Handler: w.HandleGetMemberBootContextApiMembersMemberIdBootContextGet,
			MCPTool: "get_member_boot_context",
		}),
	}
	out := make([]RouteSpec, len(rows))
	for i, r := range rows {
		out[i] = r.RouteSpec
	}
	return out
}
