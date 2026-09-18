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
	Requires principalClass
	// MCPExclude keeps this route OUT of the MCP tool surface (infra endpoints).
	MCPExclude bool
	// MCPTool is the explicit MCP tool name override (paths carrying a {param}).
	MCPTool string
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

// ── Who may call this route: an ARGUMENT, not a field ───────────────────────
//
// A row reaches the mux only through Public() or Gated(), and neither takes a
// RouteSpec: they take routeDef, which HAS NO Auth and NO Requires field. So
// "is this route public?" and "what is its floor?" cannot be left out, cannot
// be written twice, and cannot contradict each other — Gated demands the class
// as its first argument and Go refuses a call that omits one, while Public has
// no such argument to give.
//
// This replaces the two boot assertions that used to catch an undeclared or
// self-contradicting row at startup: they checked, at run time, facts that are
// now unsayable at compile time.

// routeDef is a row MINUS who may call it. Everything the table hand-writes
// lives here; Auth and Requires deliberately do not, so they can only ever be
// set by the two constructors below.
type routeDef struct {
	Method     string
	Path       string
	Handler    http.HandlerFunc
	MCPExclude bool
	MCPTool    string
	ShareSig   shareSigVerifier
}

// routeRow is what the table holds. Public and Gated are its only
// constructors, so a bare RouteSpec written into the table is a TYPE error
// rather than a row that silently serves with an empty auth label.
type routeRow struct{ RouteSpec }

func (d routeDef) row(auth string, requires principalClass) routeRow {
	return routeRow{RouteSpec{
		Method: d.Method, Path: d.Path, Handler: d.Handler,
		Auth: auth, Requires: requires,
		MCPExclude: d.MCPExclude, MCPTool: d.MCPTool, ShareSig: d.ShareSig,
	}}
}

// Public is a route anyone may call. It takes no class BECAUSE there is none to
// name — "public but requires owner" is not a row you can write.
func Public(d routeDef) routeRow { return d.row(authPublic, requiresPublic) }

// Gated is a route behind the auth choke. The class is the FIRST argument, so
// forgetting it is "not enough arguments in call to Gated" at compile time.
func Gated(c principalClass, d routeDef) routeRow { return d.row(authGated, c) }

// routeSpecs builds the route table over the generated wrapper (which binds
// path/query params, then dispatches into apiServer). Row order, auth labels,
// requires classes, and MCP flags mirror service/routes.py ROUTE_SPECS.
func routeSpecs(w *ServerInterfaceWrapper) []RouteSpec {
	rows := []routeRow{
		// ── Build identity + deploy probes ──────────────────────────────────
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/health",
			Handler:    w.HandleHealthApiHealthGet,
			MCPExclude: true, // an ops liveness probe, not an agent tool
		}),
		Public(routeDef{
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
		// ── Credential seams ─────────────────────────────────────────────────
		Public(routeDef{
			Method:     "POST",
			Path:       "/api/login",
			Handler:    w.HandleLoginApiLoginPost,
			MCPExclude: true,
		}),
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
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/mint",
			Handler: w.HandleMintApiMintPost,
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — issuing an identity equals self-escalation.
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/auth/status",
			Handler:    w.HandleAuthStatusApiAuthStatusGet,
			MCPExclude: true, // the login wall's branch bit, not an agent tool
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/auth/set-password",
			Handler: w.HandleSetPasswordApiAuthSetPasswordPost,
			// the one-shot claim token IS the gate (lifecycle.md §1.3)
			MCPExclude: true, // a credential seam, never an agent tool
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/auth/change-password",
			Handler: w.HandleChangePasswordApiAuthChangePasswordPost,
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — the owner's PERSONAL account credential.
			MCPExclude: true, // the owner's credential, never an agent tool
		}),
		// ── Owner second factor (TOTP) ───────────────────────────────────────
		// All three are principalOwner + MCPExclude, for the same reason the two
		// password rows above are: these endpoints decide how the OWNER
		// authenticates. An admin_agent that could reach them could weaken the
		// credential that governs it, and arming or disarming the owner's factor
		// is never something an agent does on the owner's behalf.
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
		// ── Signing-key ring (T-62) ──────────────────────────────────────────
		// principalOwner + MCPExclude for the same reason the password and
		// second-factor rows above are: these routes govern the key that
		// authenticates EVERY caller, the calling agent included. An
		// admin_agent that could reach them could rotate the key that governs
		// it, or remove the key its own credential is signed under.
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
			// T-6020: opened to admin_agent (owner 2026-07-26) — running the
			// office needs the office's own knobs.
			Method:  "GET",
			Path:    "/api/settings",
			Handler: w.HandleGetSettingsApiSettingsGet,
			MCPTool: "get_settings",
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:  "PATCH",
			Path:    "/api/settings",
			Handler: w.HandleUpdateSettingsApiSettingsPatch,
			// 🔴 DO NOT ENUMERATE FIELDS HERE. This line used to name three of
			// them ("owner and agent token TTLs / handover threshold") and it
			// went stale the moment a field was added — T-122 added two and
			// nothing went red, because prose has no gate. It also rides to the
			// MCP face as update_settings's description, so a partial list is an
			// agent being told what this endpoint can change. The input schema is
			// generated from the DTO and cannot go stale; point at it instead.
			MCPTool: "update_settings",
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodGet, Path: "/api/push/public-key", Handler: w.HandleGetPushPublicKeyApiPushPublicKeyGet,
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodPost, Path: "/api/push/subscription", Handler: w.HandleCreatePushSubscriptionApiPushSubscriptionPost,
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodDelete, Path: "/api/push/subscription", Handler: w.HandleDeletePushSubscriptionApiPushSubscriptionDelete,
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:  "GET",
			Path:    "/api/release/check",
			Handler: w.HandleCheckReleaseApiReleaseCheckGet,
			MCPTool: "check_release",
		}),
		Gated(principalAdminAgent, routeDef{
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
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// runs software upgrades; a PLAIN agent still cannot self-upgrade
			// the server (the admin_agent choke keeps rank<2 out).
			Method:  "POST",
			Path:    "/api/update/upgrade",
			Handler: w.HandleUpgradeApiUpdateUpgradePost,
			MCPTool: "upgrade_station",
		}),
		// ── Gated infra seams ────────────────────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/events",
			Handler:    w.HandleEventsApiEventsGet,
			MCPExclude: true, // a live stream is not a callable tool
		}),
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/mcp",
			Handler:    w.HandleMcpApiMcpPost,
			MCPExclude: true, // the MCP endpoint is the transport, not a tool
		}),
		// ── Members — roster + presence + lifecycle ──────────────────────────
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
			// T-c826 owner 2026-07-27 explicitly chose owner-only: a personal
			// avatar is owner-managed member identity/presentation, not an
			// operational capability an agent may change for itself or peers.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  http.MethodDelete,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete,
			// Same T-c826 ruling as PUT: removal changes the owner's chosen
			// member identity and therefore stays off the AI-callable surface.
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
			MCPTool: "relocate_member", // owner-cockpit 改機器 + admin-agent 工具 (T-8655): Mira 可經 MCP 把 member 搬機; 權限仍 principalAdminAgent (一般 agent 擋)。P7c: member_id 也吃 worker id (ow-…) — handler falls through to the worker relocate core (外包對齊正職)
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
			// principalOwner, NOT the admin_agent floor its neighbours sit on,
			// and that gap is the decision rather than an oversight (T-53,
			// owner ruling rc-7dea0deefa63). The rows above control a member;
			// this one destroys the owner's own spend record, irreversibly and
			// with nothing else in the system holding a copy. An admin agent
			// deciding that on his behalf is not a thing he asked for.
			// Owner-only cockpit surface, so MCP-excluded on the same reasoning
			// as the mint / credential / avatar rows: an agent has nothing
			// legitimate to do with the owner's spend record.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/accounts/cost/reset",
			Handler: w.HandleResetAccountCostApiAccountsCostResetPost,
			// Same owner-only floor, same reasoning, as the per-actor row
			// above: an irreversible write to a figure the owner watches.
			//
			// 🔴 IT TOUCHES NO ACTOR. An earlier shape of this route did clear
			// every actor on the account (rc-efae958cef40); the owner then
			// ruled the two clearings SEPARATE (rc-5c5d7c7c6dcd, 2026-09-02),
			// so the account card became an accumulator of its own (migration
			// 00069) and this route writes that one row and nothing else.
			// The account key rides in the BODY, not the path: a real key is a
			// compound free string containing '/' and '@', and an encoded
			// slash that a proxy decodes would silently retarget an
			// irreversible call. See the spec entry.
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
		// Debug ring buffer — raw external payloads. T-6020 (owner 2026-07-26)
		// opened it to admin_agent; a PLAIN agent still cannot see another
		// channel's unverified input through this side door.
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
			Handler: w.HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet,
			MCPTool: "list_webhook_requests",
		}),
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
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/resume-summary",
			Handler: w.HandleGetMemberResumeSummaryApiMembersMemberIdResumeSummaryGet,
			MCPTool: "get_member_resume_summary",
		}),
		// ── Webhook inlet — PUBLIC (M4 §2) ─────────────────────────────────────
		// Token-only identity (?t=); the path carries nothing else. Accepted and
		// ignored calls answer the same silent 200 so it never leaks existence.
		Public(routeDef{
			Method:     "POST",
			Path:       "/in",
			Handler:    w.HandleReceiveWebhookInPost,
			MCPExclude: true,
		}),
		// ── Chat ─────────────────────────────────────────────────────────────
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
			MCPExclude: true, // a binary ingest seam like the blob GET, not a tool
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
		// ── Reply cards (等我回覆卡) ─────────────────────────────────────────
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
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/chat/unread-count",
			Handler:    w.HandleChatUnreadCountApiChatUnreadCountGet,
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		// ── Comparisons: a URL, not an attachment (T-59) ─────────────────────
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/diff",
			Handler: w.HandleGetDiffApiDiffGet,
			// The DATA seam behind the /diff page, like the attachment blob GET
			// it sits beside — an agent hands over a LINK, it does not fetch the
			// pair and re-narrate it.
			MCPExclude: true,
			// The unauthenticated path, and the ONLY one: a credential-less
			// request may present ?sig=, verified over both addresses and both
			// labels (verifyDiffShareSig). There is no second bypass anywhere.
			ShareSig: verifyDiffShareSig,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/diff/share-link",
			Handler: w.HandleGetDiffShareLinkApiDiffShareLinkGet,
			// On the agent surface for the same reason the attachment share
			// link is: an agent that produces a comparison can otherwise only
			// hand it to someone who can already sign in. Minting is where the
			// permanence lives — read sharesig.go before widening this.
			MCPTool: "get_diff_share_link",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/reply-cards/{card_id}",
			Handler: w.HandleGetReplyCardApiReplyCardsCardIdGet,
			MCPTool: "get_reply_card",
		}),
		// T-6020 (owner 2026-07-26): the three card-closing faces open to
		// admin_agent — the admin 助理 answers on the owner's behalf. A plain
		// agent still cannot close its own card (rank<2 → 403), so "no agent
		// self-answers its own 請示" survives.
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
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/reply-cards/{card_id}/expire",
			Handler: w.HandleExpireReplyCardApiReplyCardsCardIdExpirePost,
			MCPTool: "expire_reply_card",
		}),
		// ── Agent context gauge + monitoring ─────────────────────────────────
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
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/monitoring",
			Handler: w.HandleGetMonitoringApiMonitoringGet,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/backup-health",
			Handler: w.HandleGetBackupHealthApiBackupHealthGet,
			// T-da06: deliberately NOT an MCP tool. The consumer is the cockpit's
			// permanently mounted indicator (and its monitor card), i.e. a UI
			// seam; the backup engine's own outcomes already reach the server log
			// for anyone reading a machine. Publishing it as a tool is a separate
			// owner decision — the point of this ticket was to reach the HUMAN,
			// who does not read tool output either.
			MCPExclude: true,
		}),
		// ── Display-name overlays ────────────────────────────────────────────
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
		// ── Installer + machine onboard / teardown ───────────────────────────
		Public(routeDef{
			Method:     "GET",
			Path:       "/install.sh",
			Handler:    w.HandleInstallScriptInstallShGet,
			MCPExclude: true, // a bash installer script, not an agent tool
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
			MCPExclude: true, // a credential-mint seam (like /api/mint), not an agent tool
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "GET",
			Path:       "/api/machines/{machine_id}/boot-command",
			Handler:    w.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet,
			MCPExclude: true, // a credential-mint seam (like onboard), not an agent tool
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/machines/claim",
			Handler: w.HandleClaimMachineTokenApiMachinesClaimPost,
			// the one-time claim code IS the gate (lifecycle.md §1.3)
			MCPExclude: true, // a credential-exchange seam (like /api/login), not an agent tool
		}),
		// The renew seam a warden drives itself, so a credential that is about
		// to expire does not need anybody to go and reinstall that machine.
		// principalMachine is the LOWEST rank and an ordinary agent clears it —
		// but principalAgent would lock the WARDEN out (machine ranks BELOW
		// agent), so the warden-only property lives in the handler, not here.
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/machines/renew-credential",
			Handler:    w.HandleRenewMachineCredentialApiMachinesRenewCredentialPost,
			MCPExclude: true, // a credential-mint seam (like claim/onboard), not an agent tool
		}),
		// The number a warden needs in order to decide, for itself, that its own
		// credential is old enough to replace. It is on the SAME floor as the renew
		// seam above because the same caller asks it, one poll before asking that
		// one — a higher floor would lock the warden out of the question it exists
		// to answer.
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/machines/credential-policy",
			Handler:    w.HandleMachineCredentialPolicyApiMachinesCredentialPolicyGet,
			MCPExclude: true, // fleet plumbing a warden polls, not a verb an agent has any use for
		}),
		// T-6020 (owner 2026-07-26): the two on-server host lifecycle faces open
		// to admin_agent — installing/tearing down the server host's own warden
		// is office operations. A plain agent is still 403 (rank<2).
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
			// T-6020: opened to admin_agent (owner 2026-07-26) — same floor as
			// uninstall_machine right above, which was already admin_agent.
			Method:  "POST",
			Path:    "/api/machines/{member_id}/upgrade",
			Handler: w.HandleUpgradeMachineApiMachinesMemberIdUpgradePost,
			MCPTool: "upgrade_warden",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/machines/{member_id}",
			Handler: w.HandleDeleteMachineApiMachinesMemberIdDelete,
			MCPTool: "delete_machine",
		}),
		// ── Prebuilt binary downloads (secret-free artifacts) ────────────────
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/warden/binary",
			Handler:    w.HandleWardenBinaryApiWardenBinaryGet,
			MCPExclude: true, // a binary download, not an agent tool
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/agent/binary",
			Handler:    w.HandleAgentBinaryApiAgentBinaryGet,
			MCPExclude: true, // a binary download, not an agent tool
		}),
		// ── User context / roles / bootstrap ────────────────────────────────
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
		// 〈停止〉 (T-c9c0) — the fourth owner-editable global document, and the
		// same three-row shape as the 系統互動 block above, floors included: read
		// at the machine floor (every agent is handed this text when its session
		// is collected), write at admin_agent (it is the last instruction an
		// agent gets, with nobody online afterwards to correct it).
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
			// The machine floor, matching the READ face of every document it
			// sizes (list_roles / get_insight / list_task_manuals
			// are all machine). It cannot leak more than those already do — it
			// carries strictly less than any of them.
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
			// T-3809. READ stays on the machine floor, matching Duty: the
			// owner ruled on 2026-08-02 (rc-dc171587220c, option
			// ①, verbatim 「包含 Insight：這一輪不關任何讀取」) that this release
			// closes nothing on the read face. Insight is SEPARATE, not
			// private — say it in every surface a reader can reach, because
			// the word "insight" implies confidentiality that nobody promised.
			MCPTool: "get_insight",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/insight/{role_key}",
			Handler: w.HandleReplaceInsightApiInsightRoleKeyPost,
			// principalAgent is the honest floor: per-ROLE authz cannot be
			// expressed by the ladder, so it lives in the handler
			// (insightWriteAuthz) and the row must not declare a floor lower
			// than the gate it actually has. A warden-kind member is ranked
			// machine regardless of role_key (classifyMember), so it cannot
			// write insight even if it carries one.
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
			// T-6501. Same floor as the other two insight WRITE rows, for the
			// same reason: per-ROLE authz cannot be expressed by the ladder, so
			// it lives in the handler (insightWriteAuthz) and this row must not
			// declare a floor lower than the gate it actually has. Deliberately
			// NOT reset_role's admin_agent floor — reset_role has no per-role
			// gate to fall back on, insight does, and a role's own agent may
			// already replace this document wholesale.
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
			MCPExclude: true, // the credential-mint seam (like /api/login), not a tool
		}),
		// ── Tasks (M3) — read face + agent state machine + owner actions ────
		// The agent write rows are the FIRST requires=agent uses: the RBAC
		// ladder places agent(1) above machine/warden(0), so a warden can
		// never write tasks; the executor guard on the report rows is the
		// handlers' (caller == executor, admin capability excepted — §14).
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
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks/{task_id}",
			Handler: w.HandleGetTaskApiTasksTaskIdGet,
			MCPTool: "get_task",
		}),
		Gated(principalAgent, routeDef{
			// T-182. The floor is principalAgent and the real gate is
			// callerMayMarkTaskDone — the task's OWN executor, outsource worker
			// included (unlike mark-terminated, which subtracts it).
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-done",
			Handler: w.HandleMarkTaskDoneApiTasksTaskIdMarkDonePost,
			MCPTool: "mark_task_done",
		}),
		Gated(principalAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26). T-b56e (owner
			// 2026-08-20, card rc-b896e3f641e7 option 0) opened it further, to
			// a 正職 member acting on ITS OWN task — so the floor here is
			// principalAgent and the real gate is callerMayTerminateTask.
			// T-182 renamed the route and the tool (was /terminate +
			// terminate_task); permissions and behaviour are unchanged, and the
			// accepted set now includes ready_for_done, which is not terminal.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-terminated",
			Handler: w.HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost,
			MCPTool: "mark_task_terminated",
		}),
		Gated(principalAdminAgent, routeDef{
			// T-182. The ONLY one of the four whose floor does the gating by
			// itself: owner and admin assistant, nobody else — the task's own
			// executor is a 403 here on purpose, because an executor that can
			// force its own task simply has mark_task_done with no precondition.
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
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// pings a task's executor. Plain agents still 403.
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
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}",
			Handler: w.HandleUpdateTaskApiTasksTaskIdPost,
			MCPTool: "update_task",
		}),
		// T-e271: the ticket's own TEXT is correctable after the fact. Until
		// this row existed the tool catalogue had NO way to edit an existing
		// task's description — create_task takes one only at birth, submit_plan
		// writes steps, update_task_manual writes the TYPE's manual — so a
		// ruling to reword a card had nowhere to land. Executor-guarded like
		// every other task-driving write (callerMayDriveTask §14); the CREATOR
		// gets no standing from having created it (owner ruling).
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/description",
			Handler: w.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost,
			// T-646a: folded into update_task; the ROUTE stays for the
			// frontend and existing HTTP clients, the TOOL does not.
			MCPExclude: true,
		}),
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
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/title",
			Handler: w.HandleUpdateTaskTitleApiTasksTaskIdTitlePost,
			// T-646a: folded into update_task; the ROUTE stays for the
			// frontend and existing HTTP clients, the TOOL does not.
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			// T-182 renamed the route and the tool (was /duplicate +
			// mark_duplicate); same behaviour, a name that says which status the
			// task lands in.
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
			// T-1667: the anchor-patch twin of the wholesale write above. Same
			// caller gate (callerMayEditTaskText) — the handler shares it
			// verbatim, so the two faces onto one field can never disagree about
			// who may write.
			MCPTool: "patch_step_note",
		}),
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
			// ② opened to agent (was admin_agent): an agent reassigns/hands over a
			// task it EXECUTES (handler executor-guard, callerMayDriveTask §14);
			// owner/admin still drive any task. An outsource target still funnels
			// through the single 發包 gate (create+spawn atomicity / owner approval).
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/reassign",
			Handler: w.HandleReassignTaskApiTasksTaskIdReassignPost,
			MCPTool: "reassign_task",
		}),
		Gated(principalAgent, routeDef{
			// T-9ca5: the NEW executor takes over a reassigned task — clears the
			// reassigning LOCK and fires the predecessor worker (the takeover the
			// retired task-status report used to perform on the successor's
			// reassigning→in_progress before reassigning became a lock).
			// Guarded by callerMayClaimTask (the successor, not the
			// predecessor); status stays derived, never set here.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/claim",
			Handler: w.HandleClaimTaskApiTasksTaskIdClaimPost,
			MCPTool: "claim_task",
		}),
		Gated(principalAgent, routeDef{
			// The executing agent pins deliverables onto its own task card
			// (requires=agent; the handler's executor guard — caller == executor,
			// admin capability excepted — §14, same as the other agent write rows).
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/artifact",
			Handler: w.HandleAddTaskArtifactApiTasksTaskIdArtifactPost,
			MCPTool: "add_task_artifact",
		}),
		Gated(principalAgent, routeDef{
			// Un-pin — SAME permission model as add (owner ruling 2026-07-18
			// "Agent 自己應該也要可以刪除"): requires=agent + the handler's executor
			// guard (caller == executor, admin/owner excepted — §14). The agent
			// drives it through the remove_task_artifact tool; the owner through
			// the cockpit popover.
			Method:  "DELETE",
			Path:    "/api/tasks/{task_id}/artifact/{artifact_id}",
			Handler: w.HandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete,
			MCPTool: "remove_task_artifact",
		}),
		Gated(principalMachine, routeDef{
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
			Method:  "GET",
			Path:    "/api/tasks/{task_id}/artifacts",
			Handler: w.HandleListTaskArtifactsApiTasksTaskIdArtifactsGet,
			MCPTool: "list_task_artifacts",
		}),
		Gated(principalAgent, routeDef{
			// T-92 — the ONE-CALL door: raw bytes in, a pinned deliverable out.
			// MCPExclude for the same reason POST /api/chat/attachments carries
			// it: the body is binary, which cannot ride inside a JSON tool call.
			// It is not a second way to do what add_task_artifact does — it is
			// the way that has no gap between storing and binding, and the gap is
			// where unreferenced blobs come from.
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/artifacts/upload",
			Handler:    w.HandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost,
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			// T-92 — the raw-body twin of replace, same reasoning as the add-side
			// upload above. It refuses a LINK artifact rather than converting it:
			// the kind is immutable across versions.
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/artifact/{artifact_id}/replace/upload",
			Handler:    w.HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost,
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			// T-60 replace — the THIRD verb on the same set, so it carries the
			// same permission model as add and remove (requires=agent + the
			// handler's executor guard, admin/owner excepted) and the same
			// terminal-task freeze. A replace verb without that 409 would be the
			// freeze's back door: the content behind a frozen deliverable could
			// be swapped for anything while the card claimed nothing had moved.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/artifact/{artifact_id}/replace",
			Handler: w.HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost,
			MCPTool: "replace_task_artifact",
		}),
		Gated(principalAgent, routeDef{
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
			MCPExclude: true,
		}),
		// T-4595: GET /api/self/task (get_my_task) is RETIRED — see the note in
		// api_tasks.go. A worker reads its task through get_task like everyone
		// else, and reports its wake through report_waking like everyone else.
		// ── Outsource-only boot preview ──────────────────────────────────────
		Gated(principalAdminAgent, routeDef{
			// T-ba6b: the detail panel's initial-prompt preview — a live
			// re-assembly of the worker boot context (the member /api/bootstrap
			// preview's worker twin; no token minted). The text embeds the full
			// task + manual, so the floor is admin_agent, never plain agent.
			// T-6020 (owner 2026-07-26) dropped it from owner-only.
			Method:  "GET",
			Path:    "/api/outsource-workers/{id}/boot-context",
			Handler: w.HandleGetWorkerBootContextApiOutsourceWorkersIdBootContextGet,
			MCPTool: "get_outsource_worker_boot_context",
		}),
		// ⚠️ THE WORKER MODEL EDIT NO LONGER HAS A ROW OF ITS OWN, and the ruling
		// below is why that is the right outcome rather than a lost floor. Until
		// T-197 it was `POST /api/outsource-workers/{id}/model`
		// (set_outsource_worker_model) at the machine FLOOR since T-ed79 — the ONE
		// T-6020 row that left the admin_agent floor. T-197 folded it into
		// `PATCH /api/members/{member_id}` (update_member), which the ruling itself
		// names as the floor this act must match, so the two are now the SAME row
		// and cannot drift apart. The other worker verbs folded the same way
		// (/deactivate, /force-stop, /accelerated-stop, /refocus, /relocate,
		// /activate), each onto the staff row it was already aligned with.
		//
		// owner 2026-08-21 (rc-376a41719e62) was asked whether changing a worker's
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
		// 🔴 ONLY THAT ROW MOVED. refocus / relocate / stop / restart were already
		// at the same floor as their staff twins before this ruling, and the
		// ruling did not touch them — do not "finish the job" by lowering them.
		//
		// This note exists so the NEXT permission audit does not re-open the
		// question, the way this one had to re-open T-5336's. Raising this row
		// needs a fresh owner ruling, not a tidy-up commit.
		// ── Task manuals (M3) — agents create manuals + edit the CONTENT fields
		// (purpose / fields / SOP); the assignee face and delete are
		// GOVERNANCE, floor admin_agent since T-6020 (owner 2026-07-26; the
		// in-handler assignee gate answers 403 below that floor)
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
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:  "DELETE",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete,
			MCPTool: "delete_task_manual",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}/sop/patch",
			Handler: w.HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost,
			// T-1667: the anchor-patch twin of update_task_manual's sop_md field.
			// Same agent floor as every other manual CONTENT face; assignee (the
			// one governance field) is not reachable from here at all.
			MCPTool: "patch_task_sop",
		}),
		// ── Retained history of the editable documents above ────────────────
		// One read + one restore for EVERY overwritable long-form document
		// (global context, role definition, task manual), which is why
		// they sit after the last of those write faces instead of inside any one
		// group. Restore is a write, so it takes the agent floor.
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
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}/{id}",
			Handler: w.HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet,
			// Same floor as the two reads above, and for the same reason: this
			// is the BODY of a version the sibling listing already names. The
			// listing stopped carrying the prose (a single answer had a
			// structural ceiling in the hundreds of thousands of characters),
			// so this row is what makes the retained text reachable at all —
			// one named revision at a time. Raising the floor here would put
			// the text behind a gate the catalogue that advertises it is not
			// behind, which is the shape that makes a listing useless.
			// A TOOL, on the same owner ruling the seed row cites
			// (rc-b7d29de0eb9c, and the 2026-07-30 policy rc-b5fd1135e2dd
			// behind it): READING a document's history is an agent tool,
			// writing one back is not. The split is the VERB. Restore below
			// keeps its MCPExclude.
			MCPTool: "get_document_version",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/document-history/{kind}/{key}/{id}/restore",
			Handler: w.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost,
			// owner ruling 2026-07-30 (rc-b5fd1135e2dd, option 1): READING the
			// history is an agent tool; WRITING one back is not. Restoring is
			// how a document returns to an earlier state, and that is the
			// owner's call from the cockpit (or an assistant's), not something
			// an agent reaches for on its own. The route itself is unchanged —
			// the cockpit and the governance path still call it over REST.
			MCPExclude: true,
		}),
		// ── Product guide (docs/guide embed) — one source, three consumers ───
		// The 座艙's 使用說明 nav tab renders these; the machine-floor read
		// tools let an assistant agent read the same bytes to answer feature /
		// field questions (get_global_context's flag — assistant classifies as
		// admin_agent ≥ machine, so it can call them). The asset route serves the
		// referenced images and is not a callable tool.
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
			MCPExclude: true, // a binary image, not a callable tool
		}),
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
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/accelerated-stop",
			Handler: w.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost,
			MCPTool: "accelerated_stop_member",
		}),
		// ── 傳承 (T-33) ─────────────────────────────────────────────────────
		// These rows are appended to preserve the shared route/MCP order. The
		// handlers and the generated OpenAPI surface are supplied by mainline;
		// keep the PR's Gated() ownership spelling here so the route table remains
		// the single source of truth for authentication and principal class.
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
		// ── 計畫步驟的單筆編輯 (T-228) ──────────────────────────────────────
		// APPENDED, not placed beside the other /api/tasks/{task_id}/steps rows
		// they read next to. The MCP tool surface has ONE order shared by this
		// table, spec/openapi.json's x-mcp.order and
		// conformance/routes_manifest.json, and x-mcp.order must be the
		// consecutive range 0..N-1 — so a new tool is appended or every tool
		// after it is renumbered. The rule is stated in full at the custom-themes
		// block above.
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
		// ── 傳承 scope move (T-236) ──────────────────────────────────────────
		// Appended for the same x-mcp.order reason as the step rows above. Every
		// transition this door performs is admin-only, so unlike the state door
		// the whole floor sits on the route.
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/scope",
			Handler: w.HandleSetLoreEntryScopeApiLoreEntryIdScopePost,
			MCPTool: "set_lore_entry_scope",
		}),
	}
	out := make([]RouteSpec, len(rows))
	for i, r := range rows {
		out[i] = r.RouteSpec
	}
	return out
}
