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
	// Summary is the human/tool description (also the future MCP tool description).
	Summary string
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
	Summary    string
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
		Auth: auth, Requires: requires, Summary: d.Summary,
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
			Summary:    `Liveness probe — 200 {"status":"ok"}.`,
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
			Summary: "Read the build identity this station is RUNNING: version, git sha, git time and the MCP catalog hash, plus the cached update status and `update_checked_ok_at`, the time that update check last SUCCEEDED (absent = it never has, so `update_available: false` is not evidence of being up to date). Settle whether something is deployed by git sha ancestry, never by the version string.",
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/health",
			Handler:    w.HandleHealthHealthGet,
			Summary:    `Deploy probe: liveness — 200 {"status":"ok"}.`,
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/version",
			Handler:    w.HandleProbeVersionVersionGet,
			Summary:    "Deploy probe: version + git sha (autodeploy sha compare).",
			MCPExclude: true,
		}),
		// ── Credential seams ─────────────────────────────────────────────────
		Public(routeDef{
			Method:     "POST",
			Path:       "/api/login",
			Handler:    w.HandleLoginApiLoginPost,
			Summary:    "Owner login: exchange the password for an owner-scoped JWT.",
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
			Summary: "Owner-gated mint of a long-lived agent JWT for a member (TTL capped).",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — issuing an identity equals self-escalation.
			MCPExclude: true,
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/auth/status",
			Handler:    w.HandleAuthStatusApiAuthStatusGet,
			Summary:    "First-run probe: has the owner password been set?",
			MCPExclude: true, // the login wall's branch bit, not an agent tool
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/auth/set-password",
			Handler: w.HandleSetPasswordApiAuthSetPasswordPost,
			// the one-shot claim token IS the gate (lifecycle.md §1.3)
			Summary:    "First-run: set the owner password (one-shot claim token gate).",
			MCPExclude: true, // a credential seam, never an agent tool
		}),
		Gated(principalOwner, routeDef{
			Method:  "POST",
			Path:    "/api/auth/change-password",
			Handler: w.HandleChangePasswordApiAuthChangePasswordPost,
			Summary: "Change the owner password (verifies the current one).",
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
			Summary:    "Read the owner's second-factor state (offered + enrolled).",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/offer",
			Handler:    w.HandleMfaOfferApiAuthMfaOfferPost,
			Summary:    "Turn the second-factor feature on or off for this server.",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/enroll",
			Handler:    w.HandleMfaEnrollApiAuthMfaEnrollPost,
			Summary:    "Begin TOTP enrolment: mint a pending secret + otpauth URI.",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/activate",
			Handler:    w.HandleMfaActivateApiAuthMfaActivatePost,
			Summary:    "Arm the second factor by proving a code from the pending secret.",
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
			Summary:    "List the signing keys: id, when it was made, which one signs.",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/rotate",
			Handler:    w.HandleSigningKeyRotateApiAuthSigningKeysRotatePost,
			Summary:    "Mint a new signing key and hand signing over to it; the old one stays, verifying.",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/signing-keys/{key_id}/remove",
			Handler:    w.HandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost,
			Summary:    "Remove a retired key, revoking everything it signed. Refuses the signing key.",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:     "POST",
			Path:       "/api/auth/mfa/disable",
			Handler:    w.HandleMfaDisableApiAuthMfaDisablePost,
			Summary:    "Turn the second factor off (password + live code required).",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26) — running the
			// office needs the office's own knobs.
			Method:  "GET",
			Path:    "/api/settings",
			Handler: w.HandleGetSettingsApiSettingsGet,
			Summary: "Read the org-adjustable settings (owner/admin agent).",
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
			Summary: "Edit the org-adjustable settings (owner/admin agent) — only the fields you send change, and the change is live immediately. This tool's input schema is the field list; read the current values with get_settings first.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `codex_compaction_threshold`: Codex context-compaction threshold, 1 through 10.\n- `backup_retain`: How many database backup files rotation KEEPS — N; everything past N is DELETED from disk (it used to be moved to a trash/ nobody emptied). Must be between 1 and 20. Two things it does NOT mean: N counts VERSIONS, not DAYS — the calendar depth depends on how many backups those days produced; and N is PER POOL, not per directory — routine (scheduled + manual) and pre-migration backups hold separate quotas, so 5 keeps up to 10 files. Read the current value from get_settings rather than assuming a number.\n- `chat_budget_chars`: The wake snapshot's chat block budget, in CHARACTERS (Unicode code points). It is what bounds resume_summary's chat block AND the estimated_total_chars peek_resume_summary_size reports — one number, one code path, so the two can never disagree. Must be between 1000 and 13000. Unlike the doc_cap_chars_* knobs it may be LOWERED as well as raised (the block is repacked on every read, not stored). Read the current value from get_settings rather than assuming a number.\n- `step_note_cap_chars`: The size cap on one task step's working note, in CHARACTERS (Unicode code points). Must be between 1000 and 100000. Unlike the doc_cap_chars_* knobs it may be LOWERED as well as raised, because it is enforced only when a note is WRITTEN: a note already stored above a lowered cap stays readable in full and only becomes uneditable. It does NOT govern the task-level handover note or a chat message body, which keep their own 4,000-character constant. Read the current value from get_settings rather than assuming a number.\n- `doc_cap_chars_boot_sequence`: The size cap on a 啟動步驟 block of the boot context, in CHARACTERS (Unicode code points). ONE knob for BOTH runtimes (claude and codex), each document measured on its own text. Must be at least this document's shipped default and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `doc_cap_chars_duty`: The size cap on a role's DUTY doc (the role definition), in CHARACTERS (Unicode code points). Must be at least this segment's own shipped default — which is deliberately much smaller than the other three segments' — and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `doc_cap_chars_insight`: The size cap on a role's INSIGHT doc, in CHARACTERS (Unicode code points). Must be at least this segment's shipped default and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `doc_cap_chars_manual_sop`: The size cap on a TASK MANUAL's sop_md doc, in CHARACTERS (Unicode code points). Must be at least this segment's shipped default and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `doc_cap_chars_offboard`: The size cap on the 〈停止〉 block, in CHARACTERS (Unicode code points). Must be at least this document's shipped default and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `doc_cap_chars_system_interaction`: The size cap on the 系統互動 block of the boot context, in CHARACTERS (Unicode code points). Must be at least this document's shipped default and at most 100000. Read the current values from get_settings rather than assuming a number.\n- `display_language`: The owner's cockpit language (T-0b41-p2) — trimmed; \"\" clears it back to unset. Must be one of zh, en (or \"\"); anything else is a 422.\n- `display_theme`: The owner's cockpit visual theme (T-0b41-p2) — trimmed; \"\" clears it back to unset. Must be one of office, xian (or \"\"); anything else is a 422.\n- `display_wide`: Turn the WIDE cockpit layout on/off (T-756f) — true lifts the centred ~1040px content column (the side gutters stay), false restores it. A plain boolean with no unset state: omit the field to leave it unchanged.\n- `handover_pct`: The SECOND offboard point: the FINAL notice, and where the automatic handover fires. 40..90, and strictly greater than notice_pct (the pair is validated together against the POST-patch values, so either one may be sent alone).\n- `notice_pct`: The FIRST offboard point (T-a9d6): the SOFT notice, where the agent is asked to work the offboard sequence and then call report_stopped itself. 1..89, and strictly below handover_pct.\n- `codex_notice_round`: The codex SOFT-notice compaction round (T-a9d6). 1..10, and strictly below codex_compaction_threshold.\n- `monitoring_refresh_seconds`: Minimum interval between monitoring and machine refreshes, in seconds (1 through 60).\n- `accelerated_grace_secs`: 加速停止 grace, in seconds. Must be 10 through 3600. Applies to every CLOCKED wind-down cause at once (the second context threshold and the owner-pressed 加速停止); it can never put a clock on a soft cause.\n- `warden_credential_lifetime_secs`: How long a MACHINE (warden) credential is meant to live, in seconds. Must be 86400 through 34560000 (one day through 400 days). A warden renews its own credential once that credential is two thirds of this old, plus a per-machine stagger of up to one hour so that LOWERING this value does not put the whole fleet on the mint endpoint inside one poll. The floor is one day because the last third of the lifetime is the retry window: at the 15-minute poll a one-day lifetime still leaves about 32 attempts. Wardens pick a change up within one poll interval. It is ALSO the expiry stamped into the credential (`exp = iat + this`, T-fc53), so a machine that misses its whole retry window needs a hand re-install; lowering the value never shortens a credential already issued, because an `exp` is fixed at mint time. Read the current value from get_settings rather than assuming a number.\n- `org_name`: The studio display name (T-d693) — trimmed, max 80 runes; \"\" clears it back to the localized default. A value longer than 80 runes is a 422.\n- `owner_name`: The owner's display nickname (T-0b41) — trimmed, max 80 runes; \"\" clears it back to the localized default. A value longer than 80 runes is a 422.\n- `push_contact_email`: The push contact address (T-8a82) — trimmed, max 254 runes; \"\" clears it back to unset and stops all Web Push delivery. A value must be a single `local@domain` address whose domain is a real public one: a malformed address, or one on a reserved suffix (.local, .localhost, .internal, .test, .invalid, .example), is a 422 — those are exactly the values the push gateways reject with BadJwtToken, which would take push down silently.\n- `suggested_replies_reply_card`: Replace the 建議回覆 list offered under a 請示卡 reply box (T-122) wholesale — one sentence per entry, the owner's own writing. At most 20 entries, each trimmed and at most 120 runes (Unicode code points); over either bound is a 422 that writes NOTHING, and the list is never silently truncated. An EXPLICIT EMPTY ARRAY IS LEGAL and means \"offer no suggestions there\" — unlike the scheduled-message custom_* sets, where [] is a 422. Blank entries are dropped. Read the current list from get_settings before sending: this replaces it, it does not append. 🔴 null is NOT \"clear\": an omitted field and an explicit null both mean LEAVE THIS LIST UNCHANGED, so an agent that sends null to empty the list gets a 200 and no change at all. To clear it, send [].\n- `suggested_replies_task_message`: Replace the 建議回覆 list offered under a 任務 message box (T-122) wholesale. Same bounds as suggested_replies_reply_card — at most 20 entries, each trimmed and at most 120 runes, over either is a 422 that writes nothing, and an explicit empty array is legal — but a SEPARATE list by owner ruling: answering a 請示卡 and writing to a task in progress are different conversations, and patching one list never touches the other. Read the current list from get_settings before sending: this replaces it, it does not append. 🔴 null is NOT \"clear\": an omitted field and an explicit null both mean LEAVE THIS LIST UNCHANGED, so an agent that sends null to empty the list gets a 200 and no change at all. To clear it, send [].\n- `suggested_replies_lore_message`: Replace the 建議回覆 list offered under a 傳承 entry's message box (T-33) wholesale — that box writes to the person who WROTE the entry. Same bounds as suggested_replies_reply_card — at most 20 entries, each trimmed and at most 120 runes, over either is a 422 that writes nothing, and an explicit empty array is legal — but a SEPARATE list by owner ruling: asking about a 傳承 entry, answering a 請示卡 and writing to a task in progress are three different conversations, and patching one list never touches another. Read the current list from get_settings before sending: this replaces it, it does not append. 🔴 null is NOT \"clear\": an omitted field and an explicit null both mean LEAVE THIS LIST UNCHANGED, so an agent that sends null to empty the list gets a 200 and no change at all. To clear it, send [].\n- `lore_cap_chars_role`: How many characters of 傳承 one member's boot document carries (T-33) — staff and outsource alike, spent by every boot of that member. ``everyone`` entries ride inside this same budget, ahead of the member's own (T-236). INDEPENDENT of lore_cap_chars_manual; the two are never summed, because they are paid by different readers at different moments. Unlike the doc_cap_chars_* knobs this one may be LOWERED as well as raised. Those floors equal their own shipped defaults because lowering one strands an existing legal document in shrink-only mode; a 傳承 entry's title and body can never be edited, so a smaller cap cannot strand anything already stored — it binds the next write and nothing else. The adjustable range is 100..100000.\n- `lore_cap_chars_manual`: How many characters of 傳承 get_task_manual carries for a type (T-33) — spent by whoever opens that manual, staff and outsource alike, since this fold enters no boot document. INDEPENDENT of lore_cap_chars_role. Unlike the doc_cap_chars_* knobs this one may be LOWERED as well as raised. Those floors equal their own shipped defaults because lowering one strands an existing legal document in shrink-only mode; a 傳承 entry's title and body can never be edited, so a smaller cap cannot strand anything already stored — it binds the next write and nothing else. The adjustable range is 100..100000.\n- `lore_cap_chars_title`: The longest title ONE 傳承 entry may be written with, in characters (T-33). An over-cap write is refused whole — nothing partial is stored and nothing is truncated. Unlike the doc_cap_chars_* knobs this one may be LOWERED as well as raised. Those floors equal their own shipped defaults because lowering one strands an existing legal document in shrink-only mode; a 傳承 entry's title and body can never be edited, so a smaller cap cannot strand anything already stored — it binds the next write and nothing else. The adjustable range is 10..10000.\n- `lore_cap_chars_body`: The longest body ONE 傳承 entry may be written with, in characters (T-33). An over-cap write is refused whole; half a lesson is not a shorter lesson. Unlike the doc_cap_chars_* knobs this one may be LOWERED as well as raised. Those floors equal their own shipped defaults because lowering one strands an existing legal document in shrink-only mode; a 傳承 entry's title and body can never be edited, so a smaller cap cannot strand anything already stored — it binds the next write and nothing else. The adjustable range is 10..10000.",
			MCPTool: "update_settings",
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodGet, Path: "/api/push/public-key", Handler: w.HandleGetPushPublicKeyApiPushPublicKeyGet,
			Summary: "Read the VAPID public key for this owner's browser.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodPost, Path: "/api/push/subscription", Handler: w.HandleCreatePushSubscriptionApiPushSubscriptionPost,
			Summary: "Save this owner's browser Web Push subscription.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method: http.MethodDelete, Path: "/api/push/subscription", Handler: w.HandleDeletePushSubscriptionApiPushSubscriptionDelete,
			Summary: "Remove this owner's browser Web Push subscription.",
			// T-6020: owner 2026-07-26 explicitly declined to open this to
			// admin_agent — browser Web Push is the owner's personal device.
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:  "GET",
			Path:    "/api/release/check",
			Handler: w.HandleCheckReleaseApiReleaseCheckGet,
			Summary: "Check GitHub Releases for a newer official OffiCraft version.",
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
			Summary:    "Fetch a theme bundle from a link (owner/admin agent).",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// runs software upgrades; a PLAIN agent still cannot self-upgrade
			// the server (the admin_agent choke keeps rank<2 out).
			Method:  "POST",
			Path:    "/api/update/upgrade",
			Handler: w.HandleUpgradeApiUpdateUpgradePost,
			Summary: "Trigger a software upgrade to the latest GitHub release.",
			MCPTool: "upgrade_station",
		}),
		// ── Gated infra seams ────────────────────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/events",
			Handler:    w.HandleEventsApiEventsGet,
			Summary:    "SSE delta stream (owner-scoped fan-out; reconcile-by-refetch).",
			MCPExclude: true, // a live stream is not a callable tool
		}),
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/mcp",
			Handler:    w.HandleMcpApiMcpPost,
			Summary:    "MCP JSON-RPC transport (tools/list + tools/call over the routes).",
			MCPExclude: true, // the MCP endpoint is the transport, not a tool
		}),
		// ── Members — roster + presence + lifecycle ──────────────────────────
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/members",
			Handler: w.HandleListMembersApiMembersGet,
			Summary: "List every member that has not been removed, including outsource members and warden rows (kind=warden, one per machine) by default (presence-derived MemberDTO[]). A warden row is not a chat address: post_chat to its id is a 404. fields=light returns an identity-only projection that preserves kind.",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/members",
			Handler: w.HandleHireMemberApiMembersPost,
			Summary: "Hire a member (server mints the id). An omitted runtime is stored UNSET and resolved from the target host's reported runtime capabilities at first placement (a codex-only host grows a codex member) rather than written as claude; only claude/codex are accepted when you do name one; effort defaults to medium and is validated; a hire that names kind or role_key is admin-gated. This door hires STAFF ONLY — any other kind is a 422 that names where that kind is really born (a warden through ``POST /api/machines``, an outsource worker by the outsource scheduler when a task is handed out); a STAFF hire requires a currently available seed or custom role_key: an unknown or removed role answers 422 before any roster write, while a role-less staff hire is also 422; create a role + member through POST /api/roles. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "hire_member",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}",
			Handler: w.HandleGetMemberApiMembersMemberIdGet,
			Summary: "Read one member row — STAFF OR OUTSOURCE. Released outsource rows remain readable so durable chat, task and lore attribution keeps the worker codename; dismissed staff and removed wardens answer 404. The write verbs on this same {member_id} take an active ow- id too -- update, activate, deactivate, force-stop, accelerated-stop and refocus each dispatch to the worker body; release remains task-bound and dismiss refuses an ow- id with 404.",
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
			Summary: "Partially update a member's name / runtime / model / effort. Blank name, invalid runtime or invalid effort → 422, and changing a launch-intent field arms a graceful handover. Also accepts an outsource-worker id: the same edit changes the worker's runtime / model / effort, but a worker's codename is task-bound, so naming one is a 422. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "update_member",
		}),
		Gated(principalOwner, routeDef{
			Method:  http.MethodPut,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandlePutMemberAvatarApiMembersMemberIdAvatarPut,
			// T-c826 owner 2026-07-27 explicitly chose owner-only: a personal
			// avatar is owner-managed member identity/presentation, not an
			// operational capability an agent may change for itself or peers.
			Summary:    "Upload or replace a member's personal avatar (owner only).",
			MCPExclude: true,
		}),
		Gated(principalOwner, routeDef{
			Method:  http.MethodDelete,
			Path:    "/api/members/{member_id}/avatar",
			Handler: w.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete,
			// Same T-c826 ruling as PUT: removal changes the owner's chosen
			// member identity and therefore stays off the AI-callable surface.
			Summary:    "Remove a member's personal avatar (owner only).",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/activate",
			Handler: w.HandleActivateMemberApiMembersMemberIdActivatePost,
			Summary: "Activate: write desired_state=online intent (does NOT flip online). A live member clears stopping_since/waking_since and consumes restart_after_stop while preserving its active refocus/stopped epoch; it updates the owner roster only without killing/reconciling or sending a lifecycle notice. An offline generation clears its old wind-down, banks live cost, and uses stop-before-start. Also accepts an outsource-worker id: the same wake verb restarts the worker (one that is still running is LEFT ALONE, neither restarted nor refused). Answers with a bounded receipt (``id``, ``activation_pending``, ``last_op_reason``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "activate_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/relocate",
			Handler: w.HandleRelocateMemberApiMembersMemberIdRelocatePost,
			Summary: "Relocate a member to a machine (placement only; never touches desired_state). Also accepts an outsource-worker id: the same move-one-agent verb relocates the worker. machine_id is REQUIRED (owner 2026-07-27): a relocate NAMES the destination machine and no longer doubles as an unpin — an absent key is a 422, an explicit null or \"\" is a 400. Answers with a bounded receipt (``id``, ``relocation_pending``, ``relocation_deferred``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "relocate_member", // owner-cockpit 改機器 + admin-agent 工具 (T-8655): Mira 可經 MCP 把 member 搬機; 權限仍 principalAdminAgent (一般 agent 擋)。P7c: member_id 也吃 worker id (ow-…) — handler falls through to the worker relocate core (外包對齊正職)
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/deactivate",
			Handler: w.HandleDeactivateMemberApiMembersMemberIdDeactivatePost,
			Summary: "Deactivate: desired_state=offline + stamp stopping_since (retains row); with no live session, immediately collect/bank/dispatch the stop. Also accepts an outsource-worker id: the same verb is the worker's GRACEFUL close-out -- it asks the worker to work its stop document and waits for the worker's own report_stopped; only an OFFLINE worker takes the immediate kill. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "deactivate_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/force-stop",
			Handler: w.HandleForceStopMemberApiMembersMemberIdForceStopPost,
			Summary: "Force-stop: robust STOP now. On the offboard arm the server starts no clock of its own -- collection is the agent's report_stopped, the deadline the owner opens with 加速停止, or this. Also accepts an outsource-worker id: the same verb kills the worker's session NOW and holds it down, saying nothing to it. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
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
			Summary: "Reset one actor's estimated spend to zero (owner-only, irreversible): clears the durable banked figure AND the live telemetry figure.",
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
			Summary: "Reset ONE account's own accumulated spend (owner-only, irreversible): writes that account's accumulator to 0 and touches no member or worker figure.",
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
			Summary: "report_waking(): stamp the caller's waking + clear recycle markers. Answers with a bounded receipt (``id``, ``desired_state``, ``refocus_op``, ``refocus_deadline``), not the member row — call ``get_member`` when you need the rest.",
			MCPTool: "report_waking",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/stopping",
			Handler: w.HandleReportStoppingApiSelfStoppingPost,
			Summary: "report_stopping(): stamp the caller's stopping_since (graceful stop). Answers with a bounded receipt (``id``, ``desired_state``, ``refocus_op``, ``refocus_deadline``), not the member row — call ``get_member`` when you need the rest.",
			MCPTool: "report_stopping",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/stopped",
			Handler: w.HandleReportStoppedApiSelfStoppedPost,
			Summary: "report_stopped(): tell the server you have FINISHED your close-out. 🔴 THIS CALL DOES NOT, BY ITSELF, END YOUR SESSION, and it does not always cause anything to end it — which of the four things happened is in the receipt's ``stop_effect``, and it is the only way to tell them apart:\n\n* ``collected`` — a kill was dispatched by this call. You are being collected.\n* ``latched_for_collect`` — nothing was sent yet, but the next reconcile tick collects you off the latch this call wrote. You are being collected, one tick later.\n* ``recorded_only`` — 🔴 the end of this session was RECORDED AND NOTHING ELSE. No wind-down is open and nothing is holding you down, so NO KILL FOLLOWS and you will be started again. You have not been stopped, you have been noted. If you meant to stay down, someone with the authority to set your desired state has to do that — reporting again will not.\n* ``already_reported`` — you had already reported stopped, so THIS CALL DID NOTHING AT ALL. Whatever your first report set in motion, or failed to, still stands. Calling a third time changes nothing either.\n\nThe rest of the receipt is ``id``, ``desired_state``, ``refocus_op`` and ``refocus_deadline``, not the member row — call ``get_member`` when you need the rest.",
			MCPTool: "report_stopped",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/self/refocus",
			Handler: w.HandleRestartSelfApiSelfRefocusPost,
			Summary: "restart_self(): ask for your own session to be stopped and started again. This call only records the request; it stops nothing itself. Your session is woken with the 〈停止〉 document, and the server stops and respawns the session only after you work that procedure and call ``report_stopped``. If the owner or an admin agent escalates to 加速停止 while you are working it, a deadline starts, you are sent the 〈加速停止〉 document naming it, and the server collects the session at that deadline even if ``report_stopped`` has not been called. The owner or an admin agent can also end the session at once with 強制停止 (``force_stop_member``); that stop cancels the restart and leaves you down, with your desired state set to offline. Refused with 409 when you have no live event-stream connection or your desired state is not online, with 429 while the current session is younger than the minimum-liveness floor the refusal states, and with 409 when you are already further along the wind-down ladder (下線 → 加速 → 強制). Answers with a bounded receipt (``id``, ``desired_state``, ``refocus_op``, ``refocus_deadline``), not the member row — call ``get_member`` when you need the rest.",
			MCPTool: "restart_self",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/refocus",
			Handler: w.HandleRefocusMemberApiMembersMemberIdRefocusPost,
			Summary: "Refocus a member's context (online-only, else 409). Also accepts an outsource-worker id: the same handover verb hands the worker over. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
			MCPTool: "refocus_member",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/members/{member_id}",
			Handler: w.HandleDismissMemberApiMembersMemberIdDelete,
			Summary: "Dismiss a member (soft delete). Pure seam, no UI (§9.1). Staff only -- an outsource-worker id is a 404: a worker leaves by being RELEASED with its task, not by being dismissed. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
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
			Summary:    "List a member's webhook endpoints (WebhookEndpointDTO[]).",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/members/{member_id}/webhooks",
			Handler:    w.HandleCreateWebhookApiMembersMemberIdWebhooksPost,
			Summary:    "Create a webhook endpoint (server mints the token).",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "PATCH",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleUpdateWebhookApiMembersMemberIdWebhooksEndpointIdPatch,
			Summary:    "Toggle status / edit purpose of a webhook endpoint.",
			MCPExclude: true,
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "DELETE",
			Path:       "/api/members/{member_id}/webhooks/{endpoint_id}",
			Handler:    w.HandleDeleteWebhookApiMembersMemberIdWebhooksEndpointIdDelete,
			Summary:    "Delete (permanently revoke) a webhook endpoint.",
			MCPExclude: true,
		}),
		// Debug ring buffer — raw external payloads. T-6020 (owner 2026-07-26)
		// opened it to admin_agent; a PLAIN agent still cannot see another
		// channel's unverified input through this side door.
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
			Handler: w.HandleListWebhookRequestsApiMembersMemberIdWebhooksEndpointIdRequestsGet,
			Summary: "Last 5 raw /in requests of one webhook endpoint, newest first (debug; owner or admin agent only). /in answers 200 {\"status\":\"ok\"} whether or not anything was delivered, except that a Slack URL-verification handshake gets its challenge echoed, a payload over the size limit is a 413 that states the limit, a request that repeats the ``t`` query parameter is a 422 that is never logged, and a database failure while looking up the endpoint, resolving its member or storing the message is a 500 (a failure to update the endpoint row's counters is ignored and the answer stays 200), so the caller of /in cannot see the outcome. The endpoint row keeps only counts, the last drop reason and when a request last reached it; this log shows the outcome of each request it records: ``delivered``, ``challenge``, ``ping`` (a verified GitHub ping, not delivered), or ``dropped:<reason>`` carrying the same reason the endpoint row records as its last drop reason. A request with a missing or unknown token matches no endpoint and is logged nowhere, whatever its size. A request that ends in that 500 is not logged either, and a log write that fails is skipped without an error, so a request can be missing from this log.",
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
			Summary: "List one member's scheduled messages — 定期訊息, the wall-clock alarm that wakes that member with a chat message on a repeating cadence. admin_agent floor: the owner, or an admin assistant setting these up on the owner's behalf; an ordinary agent gets 403 even for its own member_id. Rows come oldest→newest and each carries the whole schedule — label, body, cadence, the slot fields `hour`/`minute`/`day_of_week`/`day_of_month`, the four `custom` sets, timezone, and the enabled/disabled toggle — plus the delivery cursor `last_fired_slot`/`last_fired_ts`. Read this before update_scheduled_message: that call is a partial edit against these stored values, and it re-aims the cursor only for a slot field whose value actually CHANGES. 404 if the member is absent or soft-removed.",
			MCPTool: "list_scheduled_messages",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/members/{member_id}/scheduled-messages",
			Handler: w.HandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost,
			Summary: "Create a scheduled message on one member — 定期訊息, the mechanism for waking a member on a repeating wall-clock slot: at each due slot the server delivers `body` verbatim down the ORDINARY chat path, from the synthetic sender `sched:<schedule_id>`. admin_agent floor: the owner, or an admin assistant setting one up on the owner's behalf; an ordinary agent gets 403 even for its own member_id. The recipient follows chat's rule, so an `ow-` outsource worker is a legal target as well as a staff member. `body`, `cadence` and `timezone` are always required; `hour`/`minute` are required by `daily`/`weekly`/`monthly` and ignored by `custom` FOR SCHEDULING — their range is still checked under every cadence, so `hour: 99` is a 422 even for `custom`, which instead requires `custom_days`/`custom_hours`/`custom_minutes` (`custom_months` may be omitted to mean all twelve; an explicit empty set is a 422). Those conditional rules are NOT expressible in this schema — a wrong combination comes back as a 422 rather than folding into a silent midnight. TWO fields are the exception and they fail SILENTLY: `day_of_week` (used by `weekly`) and `day_of_month` (used by `monthly`) are NOT required — omit either one and the create returns 200 having defaulted it to 0 (Sunday) and 1 (the first of the month). 'Every Friday at 09:00' sent without `day_of_week` is a Sunday alarm and nothing reports it, so send the field explicitly whenever the cadence reads it. `timezone` must NAME A PLACE: `Local` and the empty string are refused with 422 even though they resolve, because they hand \"what time is it\" to wherever the server happens to run. Missed slots are never backfilled — only the slot most recently elapsed is ever considered — and the cursor starts at creation time, so a `daily` 09:00 schedule created at 10:00 does not fire today. 404 if the member is absent or soft-removed. Answers with a bounded receipt (``id``, ``member_id``, ``label``, ``body_size_chars``, ``cadence``, ``custom_months``, ``day_of_month``, ``day_of_week``, ``status``, ``last_fired_slot``, ``last_fired_ts``, ``created_ts``), not the schedule — call ``list_scheduled_messages`` when you need the rest.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `cadence`: How often the message repeats. `weekly` reads `day_of_week`, `monthly` reads `day_of_month`, `daily` reads neither; those three all fire once a day at the single wall-clock reading `hour`/`minute` names. `custom` (T-49e7) reads none of those four: it reads the FOUR sets `custom_months`, `custom_days`, `custom_hours` and `custom_minutes` and fires at EVERY wall-clock reading where all four hold at once, so it is the only cadence that can fire more than once a day. `custom_months` (T-49e7 round 2) is the only one of the four that may be omitted, and an omitted `custom_months` means all twelve months. DAYLIGHT-SAVING BEHAVIOUR, which `custom` enlarges because it names many readings a day rather than one: a reading the zone SKIPPED (spring forward) is simply not fired for `custom` — it is NOT moved forward to the next existing reading the way a once-a-day cadence's single reading is, because moving it would land on a neighbouring selected reading, produce the same slot identifier and silently merge two deliveries into one; a reading the zone repeats (autumn back) fires ONCE. WHICH of the two instants that single delivery lands on is whatever Go's `time.Date` resolves the ambiguous reading to — deterministic for a given zone and reading, but implementation-defined and NOT guaranteed to be the earlier one (measured: `America/New_York` resolves to the earlier instant, `Europe/London` and `Africa/Cairo` to the later). The invariant this contract states is only that ONE wall-clock reading yields ONE slot, hence one delivery. The `daily`/`weekly`/`monthly` behaviour is unchanged in both directions. Every set's range is enforced server-side (a 422), not by this schema — `custom_minutes: [0,20,40]` with every hour and every day listed is \"every 20 minutes\", and `custom_minutes: [15]` with every hour and every day listed is \"15 minutes past every hour\".\n- `custom_days`: Days of the month `custom` fires on, 1-31, as an EXPLICIT set. \"Every day\" means listing every day; an empty set is a 422 rather than a silent \"all\" or a silent \"never\", because a schedule that always fires and one that never fires must not be one keystroke apart. Duplicates collapse and the set is stored sorted, so two orderings of the same choice compare equal — which is what stops a caller that sends the whole form back on every save from re-aiming the cursor. Membership is decided DAY BY DAY, not month by month: a listed day the month does not contain is dropped for THAT DAY only and the month's other listed days are unaffected, so [1,15,31] in February fires on the 1st and the 15th and simply has no 31st. (`monthly` names a single day, so for it the same rule reads as \"the whole month is skipped\" — that phrasing does NOT carry over to a set and reading it across was the review finding this sentence exists to prevent.) The day is never clamped to the month's last day, the same RFC 5545 rule `monthly` follows. Range is enforced server-side (a 422), not by this schema. REQUIRED when `cadence` is `custom` (a 422 otherwise); ignored by every other cadence.\n- `custom_minutes`: Minutes of the hour `custom` fires on, 0-59, read in `timezone`, as an EXPLICIT set. Same rules as `custom_days`: an empty set is a 422, duplicates collapse and the set is stored sorted. An interval that does not divide an hour can only be approximated by naming wall-clock minutes — [0,7,14,21,28,35,42,49,56] leaves a 4-minute gap across the hour boundary, not 7 — which is a property of naming readings rather than a defect. REQUIRED when `cadence` is `custom` (a 422 otherwise); ignored by every other cadence. The closed set stays 0-59 whatever the cockpit chooses to OFFER: a picker that shows only multiples of five is a presentation decision and does not narrow this contract.\n- `custom_months`: Months of the year `custom` fires in, 1-12 (1 = January), as an EXPLICIT set — the fourth dimension of the intersection, added in T-49e7 round 2. A `custom` schedule fires at a reading only when `custom_months`, `custom_days`, `custom_hours` and `custom_minutes` ALL hold at once. It is the ONE set of the four that may be OMITTED: an omitted or null `custom_months` on a `custom` schedule means ALL TWELVE MONTHS, which is what makes every client written before this field kept working unchanged — it never sent the field, and \"every month\" is exactly what those schedules already meant. An EXPLICIT EMPTY ARRAY is still a 422, the same as the other three sets: \"never fires\" and \"fires every month\" must not be one keystroke apart, and the omitted case is answered by the field being ABSENT rather than by it being present and empty. Duplicates collapse and the set is stored sorted. Membership is decided MONTH BY MONTH against the calendar date, so it composes with `custom_days` exactly as the other sets do: months [2] with days [29] fires only in a February that HAS a 29th — a leap year — and is simply absent in the other three years, never clamped to the 28th. Range is enforced server-side (a 422), not by this schema.\n- `day_of_month`: Day of month for `monthly` cadence, 1-31. A month that does not contain the day is skipped entirely rather than clamped — the iCalendar RFC 5545 rule for invalid recurrence dates — so a schedule on day 31 fires seven times a year and never in February. Owner decision 2026-08-10, card rc-aeef15360ab5: match the common standard rather than cap the range. Omitted or null means 1. Ignored by `daily`, `weekly` and `custom`.\n- `timezone`: IANA timezone name the wall-clock slot is computed in (e.g. `Asia/Taipei`). Must name a place: `Local` and the empty string are REFUSED with 422 even though they resolve, because they mean `wherever this server runs` and `UTC by accident` rather than a stated zone. `UTC` itself is accepted. The two ways a wall-clock reading can be absent are treated differently. A DATE that is not there — a 31st in a month that has none, or a calendar day the zone deleted outright at a date-line move — is an occurrence that does not happen, per RFC 5545's treatment of an invalid recurrence date. A TIME the date does not have, because the zone skipped it springing forward, is answered DIFFERENTLY BY CADENCE. For `daily`, `weekly` and `monthly` — the three that name one reading a day — it still happens: it moves forward to the next reading the zone does have — a 02:30 slot fires at 03:00, and where the skipped stretch runs to midnight the slot lands at the start of the following date — while the next occurrence returns to the stated time, so the shift never accumulates. That forward search is bounded by the following date: if the zone deleted that date outright too, the occurrence is skipped like an absent date rather than searched for any further. For `custom` the OPPOSITE holds: a skipped reading is NOT moved forward, it is simply not fired, because moving it would land on a neighbouring selected reading and silently merge two deliveries into one. See `cadence` for the full statement of that divergence.",
			MCPTool: "create_scheduled_message",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "PATCH",
			Path:    "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler: w.HandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch,
			Summary: "Update one scheduled message, including the enabled/disabled toggle (`status`) — 定期訊息, the wall-clock wake-up for one member. admin_agent floor: the owner, or an admin assistant acting on the owner's behalf; an ordinary agent gets 403 even for its own member_id. PATCH semantics: only the fields you send change, and `id`/`member_id` are immutable. The create-side validation applies unchanged — `hour`/`minute` required by `daily`/`weekly`/`monthly` and ignored by `custom` for scheduling though still range-checked under every cadence, the custom sets never empty, `timezone` never `Local` or the empty string — all 422. Editing a timing field to a DIFFERENT value re-aims the delivery cursor to the slot most recently elapsed, so the edit never retroactively fires the slot it crossed; re-sending a value the schedule already holds moves nothing, which is what makes a whole-form save safe. `disabled` suspends firing and is reversible — it is not a lifecycle state; delete_scheduled_message is the permanent removal. 404 if the member or the schedule is absent. Answers with a bounded receipt (``id``, ``member_id``, ``label``, ``body_size_chars``, ``cadence``, ``custom_months``, ``day_of_month``, ``day_of_week``, ``status``, ``last_fired_slot``, ``last_fired_ts``, ``created_ts``), not the schedule — call ``list_scheduled_messages`` when you need the rest.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `cadence`: How often the message repeats. `weekly` reads `day_of_week`, `monthly` reads `day_of_month`, `daily` reads neither; those three all fire once a day at the single wall-clock reading `hour`/`minute` names. `custom` (T-49e7) reads none of those four: it reads `custom_months`, `custom_days`, `custom_hours` and `custom_minutes` and fires at every reading where all four hold at once. Switching a schedule TO `custom` in this PATCH must supply `custom_days`, `custom_hours` and `custom_minutes` in the SAME request unless the stored row already carries them (a 422 otherwise, never a schedule that has a cadence it has no times for); `custom_months` is exempt from that requirement — a switch that names no months, on a row carrying none, lands as all twelve. Switching AWAY from `custom` leaves the stored sets in place, unread, so switching back does not lose the choice.\n- `custom_months`: Months of the year `custom` fires in, 1-12 (1 = January), as an explicit set — the fourth dimension of the intersection (T-49e7 round 2). Same empty-set and same-value rules as `custom_days`: an explicit empty array is a 422, and supplying a set whose sorted, deduplicated form equals the stored one changes nothing, cursor included. OMITTING it here means \"leave the stored months alone\", the ordinary PATCH rule — it does NOT re-assert all twelve. The all-twelve reading of an omitted field belongs to a request that has to produce a WHOLE `custom` row from nothing: a create, or a PATCH that switches the cadence TO `custom` on a row carrying no months. Everywhere else, absent means unchanged.\n- `timezone`: IANA timezone name the wall-clock slot is computed in (e.g. `Asia/Taipei`). Must name a place: `Local` and the empty string are REFUSED with 422 even though they resolve, because they mean `wherever this server runs` and `UTC by accident` rather than a stated zone. `UTC` itself is accepted. The two ways a wall-clock reading can be absent are treated differently. A DATE that is not there — a 31st in a month that has none, or a calendar day the zone deleted outright at a date-line move — is an occurrence that does not happen, per RFC 5545's treatment of an invalid recurrence date. A TIME the date does not have, because the zone skipped it springing forward, is answered DIFFERENTLY BY CADENCE. For `daily`, `weekly` and `monthly` — the three that name one reading a day — it still happens: it moves forward to the next reading the zone does have — a 02:30 slot fires at 03:00, and where the skipped stretch runs to midnight the slot lands at the start of the following date — while the next occurrence returns to the stated time, so the shift never accumulates. That forward search is bounded by the following date: if the zone deleted that date outright too, the occurrence is skipped like an absent date rather than searched for any further. For `custom` the OPPOSITE holds: a skipped reading is NOT moved forward, it is simply not fired, because moving it would land on a neighbouring selected reading and silently merge two deliveries into one. See `cadence` for the full statement of that divergence.",
			MCPTool: "update_scheduled_message",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/members/{member_id}/scheduled-messages/{schedule_id}",
			Handler: w.HandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete,
			Summary: "Delete one scheduled message — 定期訊息, permanent and not undoable. admin_agent floor: the owner, or an admin assistant acting on the owner's behalf; an ordinary agent gets 403 even for its own member_id. When the schedule should merely STOP firing, call update_scheduled_message with `status: disabled` instead — that is the reversible half and this one is not. 404 if the member or the schedule is absent. Answers with a bounded receipt (``id``, ``member_id``, ``deleted``), not the deleted row — call ``list_scheduled_messages`` when you need the rest.",
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
			Summary: "The SAME bounded wake snapshot as resume_summary, for a TARGET member (member_id) instead of the caller — control-others, admin_agent+ only (owner-scope or role=assistant); an ordinary agent gets 403. Same identity/chat/light-task-rows/roster/machines/overview/note shape, assembled by the identical resumeSnapshotParts function (so the roster and machine blocks cannot drift from what that member would get on waking; note that machines.you_are_on resolves for the TARGET member, not for you); resume_summary itself is unchanged and still identity-locked to the caller.",
			MCPTool: "get_member_resume_summary",
		}),
		// ── Webhook inlet — PUBLIC (M4 §2) ─────────────────────────────────────
		// Token-only identity (?t=); the path carries nothing else. Accepted and
		// ignored calls answer the same silent 200 so it never leaks existence.
		Public(routeDef{
			Method:     "POST",
			Path:       "/in",
			Handler:    w.HandleReceiveWebhookInPost,
			Summary:    "Public webhook inlet — token-only (member/endpoint/purpose) delivery.",
			MCPExclude: true,
		}),
		// ── Chat ─────────────────────────────────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/chat",
			Handler: w.HandlePostChatApiChatPost,
			Summary: "Post a chat message (sender = verified JWT sub; auto SSE fan-out). ``to`` must name the owner or an active staff or outsource member; any other id is a 404, including a removed member, a warden (machine) id and a sender address the server generates for webhook, scheduled or system messages. Presence is not a gate: an offline member keeps its durable mailbox. Answers with a bounded receipt (``id``, ``ts``, ``to``, ``attachments``), not the message — call ``get_chat`` when you need the rest.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `meta`: Free-form metadata carried alongside the message. The map is copied through wholesale with NO validation of keys or values, so a misspelled key is stored rather than refused and simply never read by anything. TWO keys are exceptions to that, and they are exceptions in OPPOSITE directions. The first is meta.attachments, which is not simply taken from you: it is the ONLY link a message has to its blobs. The handler overwrites it with what it computed - but ONLY when this call actually carried attachments. Send none, and whatever you put under that key is stored as this message's attachment refs; it also satisfies the 'must carry text or an attachment' check, so a message with no text and no real attachment gets through with a 200. If you are not sending attachments, do not put an attachments key in meta. The second exception is meta.reply_to, and it is the reverse: the handler DELETES it before it stores anything, silently and unconditionally, so you get a 200 and the key is simply gone when you read the message back. The reply link has a typed field of its own (``reply_to``, above) and the server is its only writer — a link smuggled through meta would be one nothing validated, pointing anywhere. Send the typed field.\n- `reply_to`: OPTIONAL — the id of the message this one is REPLYING TO (a quote-reply, LINE-style). Omit it and nothing about the post changes.\n\nThe referenced message must EXIST; an id that names nothing is a 400, because that is a mistake in this request rather than a state of the world.\n\nIT DOES NOT HAVE TO BE IN THE CONVERSATION YOU ARE POSTING INTO (owner ruling, 2026-08-21). Quoting a line out of two other people's thread in order to step in and ask about it is the use case, not an abuse of one; the earlier same-conversation refusal is gone. Nothing is smuggled by doing so — a by-id read already reaches exactly as far as the ordinary listing does, so the quoted text was readable before the quote existed.\n\nTHE SERVER IS THE ONLY WRITER OF THIS LINK: a ``meta.reply_to`` you send yourself is DISCARDED before the message is stored, so the relation cannot be forged from the client — this parameter is the only door.\n\nNOTHING IS STORED TWICE. The stored message carries the id alone. What the quoted message SAID comes back on every read in ``ChatMessageDTO.reply_to_chat``, rebuilt from the original each time, so a reader never has to fetch it and a stale copy can never exist.",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat",
			Handler: w.HandleListChatApiChatGet,
			Summary: "List the chat stream (?with=<id>&limit=<n>; oldest→newest). Answers an OBJECT {messages, next_cursor}, never a bare array: next_cursor is opaque, send it back as cursor= for the next page, and its ABSENCE — not a short page — is the only 'nothing more' signal. Your unread backfill: unread=true returns the OLDEST unread addressed to you, judged against the per-sender watermark, and still marks nothing read. Narrow either side with sender= / recipient=. Window by message id: start_id walks TOWARDS THE NEWEST, end_id TOWARDS THE OLDEST, both inclusive. The older before_ts + before_id cursor still works but is deprecated. Re-read specific messages by id: ids=<id>&ids=<id>. THIS ROUTE NEVER MARKS ANYTHING READ (T-48) — to mark a conversation read, call mark_read explicitly.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `before_ts`: DEPRECATED (T-48) — prefer start_id/end_id. Paging cursor, the ts of the OLDEST message you already hold. Must be sent TOGETHER with before_id - supplying only one is refused with 422, which is the one mistake here that announces itself. The pair is otherwise not validated against anything real: a cursor pointing nowhere in the stream (a fabricated value, or the right instant expressed in the wrong unit - these timestamps are epoch SECONDS, not milliseconds) is not an error, it just answers 200 with an EMPTY page. An empty page is the same answer you get at the true start of history, so it cannot tell you 'you have reached the beginning' apart from 'your cursor was garbage'. Copy both values from a message you actually received rather than constructing them.\n- `cursor`: Opaque continuation token — copy the previous response's ``next_cursor`` back VERBATIM. It encodes a ``(ts, id)`` POSITION in the stream, not an offset and not a row count, so a message posted while you are paging can neither displace a row you have not read yet nor hide one. The DIRECTION it continues in belongs to the path that minted it — TOWARDS THE OLDER for the default listing, TOWARDS THE NEWER for ``unread=true`` — and sending one to the other path is 422 rather than a silently wrong page. Sending it with ``before_ts``/``before_id`` is 422 (one keyset walk per request), and so is sending it with ``start_id``/``end_id``, which mint no cursor at all. Never construct or edit one; an unreadable token is 422, naming the parameter.\n- `ids`: RE-READ SPECIFIC MESSAGES BY ID — repeatable (``?ids=<id>&ids=<id>``), returning those messages IN FULL (whole body, attachment refs and all), oldest→newest.\n\nThis is what makes the wake snapshot's fold marker honest (T-a828). ``ChatMessageDTO.body_omitted_chars`` > 0 says THIS message is here with part of its text folded away and tells the reader to re-read it with get_chat — but until this parameter existed get_chat took only a peer plus a paging cursor, so there was NO WAY TO NAME THE FOLDED MESSAGE. The promise beside the fold was not keepable, which made folding a silent drop.\n\nANSWERED ON ITS OWN: when ``ids`` is present, ``with``, ``limit``, ``before_ts``/``before_id``, ``start_id``/``end_id``, ``cursor``, ``unread``, ``sender`` and ``recipient`` are NOT consulted, the answer carries no ``next_cursor``, and a by-ids read returns them untouched — as of T-48 NO read on this route advances a watermark at all. Blank entries are dropped and duplicates collapse; an all-blank or empty set behaves exactly as if the parameter had not been sent.\n\nSAME REACH AS THE ORDINARY LISTING (T-4e95, owner ruling). A by-ids read is NOT narrowed to the caller's own conversations. It used to refuse with 403 any id whose ``sender`` and ``recipient`` were both someone else — and that bound guarded nothing, because the ordinary listing filters on ``with`` — a PARTICIPANT — not on the caller, so the very same message was already readable by asking for that peer's line (designed behaviour, not a leak). What the stricter rule actually produced was two doors onto the same rows disagreeing about who may open them, which cost an honest caller the ability to follow a ``reply_to`` while costing a dishonest one nothing. This door now states the listing's rule rather than a stricter one of its own. Narrowing a read to yourself is ``sender``/``recipient`` naming your own id — the ``caller_only`` flag that used to do it is gone, and unlike the flag those two say WHICH SIDE.\n\nALL OR NOTHING ON AN UNKNOWN ID: an id no message carries refuses the WHOLE call with 404 and names it. Deliberately not 'skip it and return the rest': a short array is indistinguishable from the fold this parameter exists to undo, so a caller could not tell a deleted message from one it simply did not ask for.\n\nAT MOST 20 DISTINCT IDS per call (counted after blanks are dropped); more is a 400 that states the limit. The cap is the response bound: 20 × the 4,000-rune body cap is the worst case one call can emit. It is not 'unfold a whole snapshot in one call' — name the ones that matter and call again.\n- `limit`: How many of the most recent messages to return. It is applied as a plain truncation with NO range validation — in SQL since T-48 rather than by slicing an already-fetched list, with the semantics below deliberately untouched, and the two out-of-range values fail in OPPOSITE directions without either being reported: 0 answers 200 with an EMPTY list (which reads exactly like 'this conversation has nothing in it'), while a NEGATIVE value skips truncation entirely and hands back everything that was fetched. Neither is refused, so a limit computed from arithmetic that slipped to 0 or below silently returns the wrong end of the range. Omit it for the default of 30. ⚠️ On the start_id/end_id path this is DIFFERENT: there the limit MUST be 1..200 and anything outside that is a 422 rather than a silent wrong answer. The forgiving behaviour described above is the LEGACY paths only.\n- `start_id`: Window anchor, INCLUSIVE, walking TOWARDS THE NEWEST: return this message and the ``limit``-1 that follow it, oldest→newest. This is the direction ``before_ts``/``before_id`` cannot express — those only ever walk back, so a caller that jumped to one specific message could not load what came after it. An id no message carries is 404, not an empty page: an empty page is what a real window at the end of the stream returns, and the two must not be indistinguishable. Sending it alongside ``before_ts``/``before_id`` is 422. Sending it with an ``end_id`` that is strictly OLDER than it is 422. On this path ``limit`` must be 1..200 or the call is 422 — the legacy paths keep their own semantics.\n- `unread`: ``true`` (the STRING) selects the unread backfill: only messages addressed to the verified caller, newer than the caller's read watermark FOR THAT SENDER. ``chat_read`` is per ``(reader, peer)``, and this path compares each message against ITS OWN sender's row — never against one global watermark, which would silently drop everything older from a peer you have never opened. Answered OLDEST FIRST and ``limit`` takes the OLDEST batch, because a backfill is re-read in the order it was said; ``next_cursor`` therefore continues TOWARDS THE NEWER. What you sent to SOMEBODY ELSE is not here — ``recipient`` decides that, and it is the caller. What you addressed to YOURSELF is: it is a message sitting in your own inbox, and whether a reader wants to be shown its own note is a question about printing, answered by the reader (``ocagent`` declines to print it and files the receipt anyway). This door only answers one question: newer than my watermark for whoever sent it. Reading this CLEARS NOTHING — no path on this route writes a watermark; ``POST /api/chat/mark-read`` does. Refused with 422 alongside ``before_ts``/``before_id`` or ``start_id``/``end_id``. Any other value reads as not sent. ``limit`` keeps this route's legacy semantics on this path — 0 is an empty page, a NEGATIVE limit is uncapped and mints no cursor — not the window path's 1..200 bound.",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/chat/attachment/{attachment_id}",
			Handler:    w.HandleGetChatAttachmentApiChatAttachmentAttachmentIdGet,
			Summary:    "Serve a chat attachment blob (owner-gated; raw bytes + stored mime).",
			MCPExclude: true,
			MCPTool:    "get_chat_attachment",
			ShareSig:   verifyAttachmentShareSig,
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat/attachments/{attachment_id}/share-link",
			Handler: w.HandleGetChatAttachmentShareLinkApiChatAttachmentsAttachmentIdShareLinkGet,
			Summary: "Mint a single-file share link (?sig= HMAC; grants read of this one attachment only). Returns {url} as a SERVER-RELATIVE path — prefix it with the origin you reach this server on to get a link you can paste to someone. The sig carries NO identity and NO expiry: whoever holds the link reads that one blob without signing in, for as long as the key that signed it is still in the server's signing-key ring. No single link can be withdrawn; the only way to void one is to remove that key (POST /api/auth/signing-keys/{key_id}/remove), which voids every link it signed at once. Mint it for deliverables you meant to hand over; do not paste it anywhere the blob itself would not belong.",
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
			Summary: "List every attachment of a member's conversations " +
				"(?with=<member_id>; flattened, sender-labelled, newest→oldest).",
		}),
		Gated(principalMachine, routeDef{
			Method:     "POST",
			Path:       "/api/chat/attachments",
			Handler:    w.HandleUploadChatAttachmentApiChatAttachmentsPost,
			Summary:    "Upload one attachment blob (raw octet-stream body; returns the light ref). ?filename= is capped at 128 characters (Unicode runes, not bytes); a longer one is refused with a 400 rather than truncated.",
			MCPExclude: true, // a binary ingest seam like the blob GET, not a tool
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/chat/mark-read",
			Handler: w.HandleMarkChatReadApiChatMarkReadPost,
			Summary: "Mark a conversation read up to a watermark (reader = verified sub). Answers with a bounded receipt (``peer_id``, ``last_read_ts``, ``advanced``), not the stored entry echoed back — call ``get_chat_reads`` when you need the rest.",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/chat/reads",
			Handler: w.HandleListChatReadsApiChatReadsGet,
			Summary: "List chat read receipts (?with=<peer>; per-conversation watermark).",
		}),
		// ── Reply cards (等我回覆卡) ─────────────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/reply-cards",
			Handler: w.HandleCreateReplyCardApiReplyCardsPost,
			Summary: "Open a reply card: an ask the owner must answer (at least one option; options ≤4 on a single card, ≤20 on a multi card, each carrying its own ai_pick flag; select_mode single|multi). linked_task is REQUIRED and has no default — every card must SAY whether it is about a task, because the server no longer infers one. Send linked_task={\"task_id\": ..., \"step_id\": ...} to bind the ask to the step it is about: that step (and its task) enters waiting_owner until the owner answers. Send linked_task=null when the ask is not about a task — it opens as a plain unbound 請示. BOTH ids are required in the object form: a task_id with NO step_id is a 400, because a card bound to a task but to no step places no 等我回覆 hold, so the task would finish underneath your question and the owner's answer would then be rejected for good. Omitting linked_task entirely is a 400 that names both legal shapes. With both ids present the bind is still refused, before anything is written, when the task does not exist (404), when you are not the task's executor and not the owner or an admin agent (403), when the task is not in_progress or waiting_owner, for example not_started or ready_for_done (409), when the step does not belong to that task (404), and when the step is already done or superseded (409). Optional attachments ride the question, up to a capped count that a refusal names (same shape as post_chat: {id} from `ocagent upload` / POST /api/chat/attachments, or inline data_b64). Answers with a bounded receipt (``id``, ``chat_message_id``, ``created_ts``, ``attachments``), not the card — call ``get_reply_card`` when you need the rest.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `body`: The question's full text - the part that lets the owner decide without going and looking things up. It is stored as sent with NO validation, so OMITTING it is not an error: the card is created with an empty body and answers 200, and what reaches the owner is a summary line and some options with no reasoning attached. Nothing warns you; the card simply arrives thinner than you meant it to.\n- `linked_task`: REQUIRED — declare it, the server never infers it. ``null`` = this ask is NOT about a task (a plain unbound 請示). ``{\"task_id\": ..., \"step_id\": ...}`` binds the ask to that step, which enters waiting_owner until the owner answers. BOTH ids are required: a task_id with no step_id is a 400, because binding a task without a step places no 等我回覆 hold and the task would run past your question. Omitting the field is a 400 that names both legal shapes.\n- `select_mode`: How many options the owner may circle: single (the default - at most one, and at most one option may carry ai_pick) or multi (any number). It is a SEPARATE axis from kind: kind says what the owner must DO (decide / act), select_mode says how many choices the answer may carry. A single card refuses an answer carrying two indices, and refuses a create marking two options ai_pick.",
			MCPTool: "create_reply_card",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/reply-cards",
			Handler: w.HandleListReplyCardsApiReplyCardsGet,
			Summary: "List light reply-card rows (summary and decision digest, without the full body/options). status is waiting (the default, longest-waiting first), answered (the last 24 hours) or expired (the last 24 hours); a positive limit is applied after each pane is ordered. Every pane covers every card on the station, not only the cards you opened. Read one card in full with get_reply_card.",
			MCPTool: "list_reply_cards",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/reply-cards/count",
			Handler:    w.HandleReplyCardCountApiReplyCardsCountGet,
			Summary:    "Waiting reply-card count (the cockpit badge).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/chat/unread-count",
			Handler:    w.HandleChatUnreadCountApiChatUnreadCountGet,
			Summary:    "Total chat unread count (the 辦公室 nav red dot).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		// ── Comparisons: a URL, not an attachment (T-59) ─────────────────────
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/diff",
			Handler: w.HandleGetDiffApiDiffGet,
			Summary: "Resolve both sides of one comparison (?before=&after=; optional labels and ?sig=). Each side carries its text, its column heading, and an honest gone marker when the address resolves to nothing.",
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
			Summary: "Mint the EXTERNAL link to one before/after comparison (?sig= HMAC over both addresses AND both column labels). Returns {url} as a SERVER-RELATIVE path — prefix it with the origin you reach this server on to get a link you can paste to someone. The sig carries NO identity and NO expiry: whoever holds the link sees that one comparison without signing in, for as long as the key that signed it is still in the server's signing-key ring. No single link can be withdrawn; the only way to void one is to remove that key (POST /api/auth/signing-keys/{key_id}/remove), which voids every comparison link and every file link it signed at once. YOU USUALLY DO NOT NEED THIS: the INTERNAL link is the same /diff?before=…&after=… page with no sig, any signed-in reader opens it, and `ocagent diff` prints it without asking the server anything. Mint this one only for a reader who has no account. A side is a stored attachment id (att-…) or doc:<kind>/<key>/<at>/<field> — `ocagent diff --help` is the authority on the spelling.",
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
			Summary: "Read one reply card (full context: options, status, answer). status is waiting, answered or expired; an expired card was closed without an answer. Any authenticated caller may read any card by id, not only a card it opened or one bound to its own task; an unknown id is a 404.",
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
			Summary: "Answer a waiting reply card — the only positive close. Answers with a bounded receipt (``id``, ``status``, ``answered_ts``, ``expired_ts``, ``answer``, ``task_id``, ``step_id``), not the whole card — call ``get_reply_card`` when you need the rest.",
			MCPTool: "answer_reply_card",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "PUT",
			Path:    "/api/reply-cards/{card_id}/answer",
			Handler: w.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut,
			Summary: "Revise an answered card's answer (重新決定): stays answered. Answers with a bounded receipt (``id``, ``status``, ``answered_ts``, ``expired_ts``, ``answer``, ``task_id``, ``step_id``), not the whole card — call ``get_reply_card`` when you need the rest.",
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
			Summary: "Mark a waiting card expired (標為過期): its author, owner, or admin agent; terminal, not an answer. Answers with a bounded receipt (``id``, ``status``, ``answered_ts``, ``expired_ts``, ``answer``, ``task_id``, ``step_id``), not the whole card — call ``get_reply_card`` when you need the rest.",
			MCPTool: "expire_reply_card",
		}),
		// ── Agent context gauge + monitoring ─────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/agent/context",
			Handler: w.HandleIngestAgentContextApiAgentContextPost,
			Summary: "Ingest an agent's context gauge (in-memory; bad body → 400). Answers with a bounded receipt (``agent_id``, ``ts``), not the stored entry echoed back — call ``get_monitoring`` when you need the rest.",
			MCPTool: "ingest_agent_context",
		}),
		Gated(principalMachine, routeDef{
			Method:  "POST",
			Path:    "/api/monitoring/telemetry",
			Handler: w.HandleIngestTelemetryApiMonitoringTelemetryPost,
			Summary: "Ingest warden telemetry (hardware/limits/tokens/cost/self_update). Answers with a bounded receipt (``agent_id``, ``machine``, ``ts``), not the stored entry echoed back — call ``get_monitoring`` when you need the rest.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `account`: Which billing account this session is drawing on. It is READ ONLY IN COMPANY WITH runtime, and the failure is worse than a dropped field: send a perfectly valid account WITHOUT runtime and the server does not merely ignore it, it CLEARS the account pairing already recorded for this entry and returns 200. A previously correct display therefore goes blank because of a report that carried the right value. A non-string value is dropped silently to empty by the same path. Always send runtime alongside account.\n- `account_label`: Human-readable label for the account. It rides the same path as account - read only in company with runtime, dropped silently when the value is not a string - and it is HARDER to notice going wrong than account is, because the ingest response does not echo it back at all. There is no field in the reply to compare against what you sent, so a dropped label is invisible from the call site; the only way to see it is to read the monitoring view afterwards.\n- `binaries`: Warden heartbeats only — the content fingerprint of the binaries this host is ACTUALLY running: an object keyed by binary name (ocwarden, ocagent, officraft) whose values are that live file's sha256 12-hex prefix. The server diffs them against the build it has embedded to derive the machine's bin_status (current / stale / absent = honest unknown), so a heartbeat that omits this leaves the machine's upgrade state unknowable rather than current. Deliberately a content hash, not a version number.\n- `claude`: Warden heartbeats only — the host's local claude CLI probe. Presence-only, NEVER a secret value. All sub-fields optional: version (string — the resolved claude binary's --version first token; absent = unresolved or probe failed), cred_file (bool — ~/.claude/.credentials.json exists), sub_readable (bool — that file exists AND its claudeAiOauth.subscriptionType is non-blank, the exact readability the account-key derivation needs), keychain (bool — macOS login-keychain item present; ABSENT, not false, on non-darwin where the probe cannot answer). Send it so the roster can tell whether this host can actually run Claude.\n- `command_result`: Receipt for a command this reporter was asked to run. The only check is that the value is an object. Everything inside is best-effort: an unknown member id, an unknown worker, or a receipt that matches no outstanding command is written to the server's stderr and then swallowed, and the call answers 200 either way. A 200 here means the report was accepted, NOT that it was matched to anything - if you need to know the result was recorded, read the state it should have changed.\n- `cost`: Accumulated cost for this session. The ONLY check is that the value is a number - there is no sign check, no unit check and no upper or lower bound, so a negative figure, a value in the wrong currency, or one off by a factor of a thousand is stored verbatim and answers 200. It becomes what the monitoring view reports, so the number is trusted exactly as far as the caller computing it.\n- `effort`: The reasoning-effort level this session is actually running at. The ONLY check is that the value is a string: unlike hire_member and update_member, which validate against the known effort levels, this path deliberately does NOT, so any string at all is accepted and overwrites the recorded effort. A typo is not refused, it is displayed - the monitoring view will report whatever you sent as though it were a real level.\n- `hardware`: Hardware readings for this machine. Nothing is validated AT INGEST: keys the schema does not declare are stored and then never read by anything, and a declared key carrying the wrong type is accepted here and only surfaces much later, on the READ side, as an entry in hardware_invalid. So a 200 from this call says nothing about whether the readings will be usable - the report and the complaint are separated by a whole round trip.\n- `machine`: Which machine this telemetry is about. It is CONSULTED ONLY IF the caller's token carries no machine claim: when the token does carry one, this field is not compared, not validated and not reported on - it is simply never read, and the call answers 200 exactly as if it had been honoured. So a wrong value here is undetectable from the response, and a right value is redundant. Send it only when reporting from a caller whose token is not already bound to a machine.\n- `model`: The model this session is actually running. The only check is that the value is a string, and there is no allow-list - an unknown or misspelled model is stored and displayed as though real. An EMPTY string is a silent no-op: it is accepted, changes nothing, and answers 200 like any other report, so 'I reported the model' and 'the model was recorded' are not the same statement.\n- `rate_limits`: Rate-limit state for this account. The ONLY check is that the value is an object; every key and value inside is stored unvalidated, so a misspelled key or a wrong unit is accepted and answers 200. What is stored here drives the pacing figures a reader sees, so a wrong shape does not fail loudly - it produces a confident-looking number that is not true.\n- `self_update`: Self-update status for the reporting binary. The ONLY check is that the value is an object - anything else is a flat 400, 'self_update must be an object' - and nothing inside it is checked. What you send is kept only on this reporter's IN-MEMORY telemetry entry, plus one summary line on the SERVER's stderr that prints the reporter, binary, old_hash, new_hash and at, substituting '?' for whatever is missing. Nothing reads it back: the bounded receipt (agent_id, machine, ts) does not echo it, and neither get_monitoring nor any other read tool returns it, so a misspelled sub-key goes unnoticed by the caller. The entry is gone the moment the server re-execs, and because this endpoint MERGES, omitting self_update does not clear the previous value.\n- `tokens`: Token counters for this session. The ONLY check is that the value is an object - the shape INSIDE it is not validated at all, so a misspelled counter name, a missing field or a string where a number belongs is stored as sent and answers 200. Nothing downstream will tell you the shape was wrong; the counters simply do not add up wherever they are read.",
			MCPTool: "ingest_telemetry",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/monitoring",
			Handler: w.HandleGetMonitoringApiMonitoringGet,
			Summary: "Monitoring telemetry (roster + context + warden push; honest — else).",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/backup-health",
			Handler: w.HandleGetBackupHealthApiBackupHealthGet,
			Summary: "Backup health: is the scheduled backup still producing retreat points?",
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
			Summary: "Set an account's display name (id = stable tag). Blank name → 422.",
			MCPTool: "update_account",
		}),
		Gated(principalMachine, routeDef{
			Method:  "PATCH",
			Path:    "/api/machines/{machine_id}",
			Handler: w.HandleUpdateMachineApiMachinesMachineIdPatch,
			Summary: "Set a machine's display name (id = stable host). Blank name → 422.",
			MCPTool: "update_machine",
		}),
		// ── Installer + machine onboard / teardown ───────────────────────────
		Public(routeDef{
			Method:     "GET",
			Path:       "/install.sh",
			Handler:    w.HandleInstallScriptInstallShGet,
			Summary:    "One-line remote warden installer (curl|bash; token+id in URL query).",
			MCPExclude: true, // a bash installer script, not an agent tool
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/machines",
			Handler: w.HandleListMachinesApiMachinesGet,
			Summary: "List machines (active wardens): machine_id/display_name/online.",
			MCPTool: "list_machines",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/machines",
			Handler:    w.HandleOnboardMachineApiMachinesPost,
			Summary:    "Onboard a machine: new warden member (id == machine id) + exec-token.",
			MCPExclude: true, // a credential-mint seam (like /api/mint), not an agent tool
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "GET",
			Path:       "/api/machines/{machine_id}/boot-command",
			Handler:    w.HandleMachineBootCommandApiMachinesMachineIdBootCommandGet,
			Summary:    "Re-fetch a machine's boot command anytime (re-mints its exec-token).",
			MCPExclude: true, // a credential-mint seam (like onboard), not an agent tool
		}),
		Public(routeDef{
			Method:  "POST",
			Path:    "/api/machines/claim",
			Handler: w.HandleClaimMachineTokenApiMachinesClaimPost,
			// the one-time claim code IS the gate (lifecycle.md §1.3)
			Summary:    "Exchange a one-time claim code for the machine's exec-token.",
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
			Summary:    "Renew the CALLER's own machine credential. Takes no body and names no target — the machine acted on is the caller's verified sub, so one machine cannot renew another's.",
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
			Summary:    "Read how long a machine credential is meant to live, in seconds. Names no target and returns the same answer to every caller; a warden polls it to know when to renew its own credential.",
			MCPExclude: true, // fleet plumbing a warden polls, not a verb an agent has any use for
		}),
		// T-6020 (owner 2026-07-26): the two on-server host lifecycle faces open
		// to admin_agent — installing/tearing down the server host's own warden
		// is office operations. A plain agent is still 403 (rank<2).
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{machine_id}/bootstrap-here",
			Handler: w.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost,
			Summary: "Bootstrap on server: runs `ocwarden install --force` on the SERVER's own host. machine_id is NOT a target — this verb has no way to reach another machine, and naming one is refused (409); the server-local machine is the only value it accepts, and the install overwrites the existing one, which is how you repair this host's warden. To install a different machine, fetch that machine's own boot command with GET /api/machines/{machine_id}/boot-command and run it on that host.",
			MCPTool: "install_warden_on_server_host",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{machine_id}/teardown-here",
			Handler: w.HandleTeardownHereApiMachinesMachineIdTeardownHerePost,
			Summary: "Teardown on server: runs `ocwarden teardown` on the SERVER's own host. machine_id is NOT a target — this verb has no way to reach another machine, and naming one is refused (409). The server-local machine is refused too (retiring it revokes credentials fleet-wide). To retire another machine use uninstall_machine then delete_machine; to repair the server host's own warden use install_warden_on_server_host, which runs `install --force` over the existing install.",
			MCPTool: "uninstall_warden_on_server_host",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/machines/{member_id}/uninstall",
			Handler: w.HandleUninstallMachineApiMachinesMemberIdUninstallPost,
			Summary: "Uninstall a machine: drive the uninstall RPC to its warden.",
			MCPTool: "uninstall_machine",
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26) — same floor as
			// uninstall_machine right above, which was already admin_agent.
			Method:  "POST",
			Path:    "/api/machines/{member_id}/upgrade",
			Handler: w.HandleUpgradeMachineApiMachinesMemberIdUpgradePost,
			Summary: "Upgrade a machine: kick its warden's self-update NOW.",
			MCPTool: "upgrade_warden",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/machines/{member_id}",
			Handler: w.HandleDeleteMachineApiMachinesMemberIdDelete,
			Summary: "Delete a machine: soft-delete its warden record (no command sent).",
			MCPTool: "delete_machine",
		}),
		// ── Prebuilt binary downloads (secret-free artifacts) ────────────────
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/warden/binary",
			Handler:    w.HandleWardenBinaryApiWardenBinaryGet,
			Summary:    "Download the prebuilt ocwarden binary (octet-stream) for a machine.",
			MCPExclude: true, // a binary download, not an agent tool
		}),
		Public(routeDef{
			Method:     "GET",
			Path:       "/api/agent/binary",
			Handler:    w.HandleAgentBinaryApiAgentBinaryGet,
			Summary:    "Download the prebuilt ocagent binary (octet-stream) for an agent.",
			MCPExclude: true, // a binary download, not an agent tool
		}),
		// ── User context / roles / bootstrap ────────────────────────────────
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/global-context",
			Handler: w.HandleGetGlobalContextApiGlobalContextGet,
			Summary: "Read the user-custom additive context block (empty = is_default).",
			MCPTool: "get_global_context",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/global-context",
			Handler: w.HandleReplaceGlobalContextApiGlobalContextPost,
			Summary: "Whole-block replace of the user-custom additive block ({text}). text is REQUIRED; unknown keys are rejected. Replacing existing content with an empty block needs allow_shrink=true (or use reset_global_context). Answers with a bounded receipt (``is_default``, ``size_chars``, ``sha256``), not the block — call ``get_global_context`` when you need the rest.",
			MCPTool: "replace_global_context",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/global-context/reset",
			Handler: w.HandleResetGlobalContextApiGlobalContextResetPost,
			Summary: "Reset the user-custom block to empty (idempotent tombstone). Answers with a bounded receipt (``is_default``, ``size_chars``, ``sha256``), not the block — call ``get_global_context`` when you need the rest.",
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
			Summary: "Read the 系統互動 block of the boot context — the shared studio handbook every agent reads at boot. Folded: the owner's edit when one exists, otherwise the shipped factory seed, with is_default saying which of the two you are holding and has_seed saying a factory version exists to go back to. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool: "get_system_interaction",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/system-interaction",
			Handler: w.HandleReplaceSystemInteractionApiSystemInteractionPost,
			Summary: "Replace the EDITABLE HALF of the 系統互動 block of the boot context ({body}) — the handbook every agent reads at boot. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. This document carries NO read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.system_interaction cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_system_interaction always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_system_interaction`` when you need the rest.",
			MCPTool: "replace_system_interaction",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/system-interaction/reset",
			Handler: w.HandleResetSystemInteractionApiSystemInteractionResetPost,
			Summary: "Restore the 系統互動 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it. The overlay being discarded is retained in the document history, so the reset is itself recoverable. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_system_interaction`` when you need the rest.",
			MCPTool: "reset_system_interaction",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/boot-sequence/{runtime_key}",
			Handler: w.HandleGetBootSequenceApiBootSequenceRuntimeKeyGet,
			Summary: "Read one runtime's 啟動步驟 block — the boot checklist that ends that runtime's boot context. runtime_key is 'claude' or 'codex'; they are separate documents because step 3 of the two says opposite things (claude mounts its own `ocagent listen`, codex must not — the sidecar owns it), so any other value is a 404 rather than a silent fallback to claude. Folded: the owner's edit when one exists, otherwise the shipped factory seed. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool: "get_boot_sequence",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-sequence/{runtime_key}",
			Handler: w.HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost,
			Summary: "Replace the EDITABLE HALF of the 啟動步驟 block of ONE runtime ({runtime_key, body}). runtime_key is 'claude' or 'codex' and the two are separate documents whose step 3 contradicts each other, so writing the wrong one leaves those agents unable to come online — and nothing that never boots reports it. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. Neither runtime's document carries a read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.boot_sequence cap (one cap, both runtimes, each measured on its own text); the refusal tells you what you wrote, the cap, and what is stored. The shipped seed is never overwritten, so reset_boot_sequence always gets the factory text back. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_boot_sequence`` when you need the rest.",
			MCPTool: "replace_boot_sequence",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-sequence/{runtime_key}/reset",
			Handler: w.HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost,
			Summary: "Restore ONE runtime's 啟動步驟 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). runtime_key is 'claude' or 'codex'; anything else is a 404. No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it, which is what makes this the recovery route when a bad edit has stopped agents from booting. The overlay being discarded is retained in the document history. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_boot_sequence`` when you need the rest.",
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
			Summary: "Read the 〈停止〉 block — the wrap-up checklist the server hands an agent at the moment it is about to collect that session. It is a SINGLETON: one document for every agent and every runtime, keyed `global` like the 系統互動 block. Folded: the owner's edit when one exists, otherwise the shipped factory seed, with is_default saying which of the two you are holding and has_seed saying a factory version exists to go back to. The reply carries size_chars/cap_chars (this document's own size limit, in characters) and is_default/has_seed, so a caller can size an edit before making it and can tell an edited block from the shipped one.",
			MCPTool: "get_offboard",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/offboard",
			Handler: w.HandleReplaceOffboardApiOffboardPost,
			Summary: "Replace the EDITABLE HALF of the 〈停止〉 block ({body}) — the wrap-up checklist an agent is handed when its session is being collected. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. This document carries NO read-only head today (T-6f44, owner's decision 4 removed it), so the body IS the whole document; the head machinery still exists for the kinds that do carry one, and there is no way to write a head on any face. The stored result is judged against the doc.cap_chars.offboard cap unconditionally, and the refusal tells you what you wrote, the cap, and what is already stored. The shipped seed is never overwritten, so reset_offboard always gets the factory text back; the version this write replaces is retained in the document history (a save that changes nothing retains nothing). Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_offboard`` when you need the rest.",
			MCPTool: "replace_offboard",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/offboard/reset",
			Handler: w.HandleResetOffboardApiOffboardResetPost,
			Summary: "Restore the 〈停止〉 block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap is applied on this path — the factory text is part of the product, so no setting can block the way back to it. The overlay being discarded is retained in the document history, so the reset is itself recoverable. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_offboard`` when you need the rest.",
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
			Summary: "Read one block of the boot context by kind/key, folded (the owner's edit ⊕ the shipped seed). Carries size_chars/cap_chars so an edit can be sized before it is made, is_default/has_seed to tell an edited block from the shipped one, and read_only for the blocks that are shown but may never be edited. An unknown kind or key is a 404 that names the keys that exist.",
			MCPTool: "get_boot_doc",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-docs/{kind}/{key}",
			Handler: w.HandleReplaceBootDocApiBootDocsKindKeyPost,
			Summary: "Replace the EDITABLE HALF of one boot-context block ({kind, key, body}) — text every agent reads at boot, or is sent when a lifecycle event happens to it. body is REQUIRED and unknown keys are rejected; emptying a body that had content needs allow_shrink=true. The stored result is judged against that block's own cap. A read-only block refuses with 405 for every caller. The read-only head is NOT sent and cannot be: the server joins the shipped head back on, so no caller has any way to write it. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_boot_doc`` when you need the rest.",
			MCPTool: "replace_boot_doc",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/boot-docs/{kind}/{key}/reset",
			Handler: w.HandleResetBootDocApiBootDocsKindKeyResetPost,
			Summary: "Restore one boot-context block to the FACTORY text shipped with this build (idempotent tombstone of the overlay). No length cap applies on this path — the way back to factory text is never blocked by a setting, which is what makes it the recovery route after an edit that stopped agents from booting. The discarded overlay is retained in the document history. Owner or admin assistant only. Answers with a bounded receipt (``kind``, ``key``, ``is_default``, ``size_chars``, ``cap_chars``, ``sha256``), not the document — call ``get_boot_doc`` when you need the rest.",
			MCPTool: "reset_boot_doc",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/roles",
			Handler: w.HandleListRolesApiRolesGet,
			Summary: "List role definitions (seed defaults + owner edits) WITHOUT the persona bodies: each row is the role identity plus its definition size and cap, never definition_md itself. Read the one role you want with get_role.",
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
			Summary: "Size-only overview of the capped documents on the station: each role's role definition / insight, and each task manual's SOP, as size_chars plus the cap_chars in force for THAT segment (the three segments have three separate caps — each is reported against its own). THE LISTING IS KEYED BY ROLE, AND THAT IS ITS LIMIT: nothing validates a role_key against the roster on the INSIGHT write face, so an admin or the owner can write insight under a role_key no role carries; such a document spends the insight cap and, having no role to hang off, never appears here. list_roles is the roster this listing is derived from — a document under a name that is not on it is not on this page either. Carries NO document text, so it costs a few hundred bytes. Use it to find which long-lived document is nearly full, then read only that one (get_role / get_insight / get_task_manual). It is the only way to see insight sizes in bulk — no listing reports those at any price; the manual sizes and caps are also on every list_task_manuals row, and a role definition's size and cap are already on every list_roles row.",
			MCPTool: "peek_doc_sizes",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles",
			Handler: w.HandleCreateRoleApiRolesPost,
			Summary: "Create a custom role + its founding member (one pair per call). runtime is claude/codex; absent = stored UNSET and resolved at the founding member's first placement from the host's reported capabilities, not written as claude. Answers with a bounded receipt (``role_key``, ``member_id``, ``member_name``), not the role and member objects — call ``get_role`` when you need the rest.",
			MCPTool: "create_role",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/roles/{role}",
			Handler: w.HandleGetRoleApiRolesRoleGet,
			Summary: "Read one role definition (unknown → 404).",
			MCPTool: "get_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles/{role}",
			Handler: w.HandleUpdateRoleApiRolesRolePost,
			Summary: "Edit a role definition ({name?, definition_md?}; locked names skip). Answers with a bounded receipt (``key``, ``name``, ``is_default``, ``is_seed``, ``size_chars``, ``cap_chars``, ``sha256``), not the duty document — call ``get_role`` when you need the rest.",
			MCPTool: "update_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "POST",
			Path:    "/api/roles/{role}/reset",
			Handler: w.HandleResetRoleApiRolesRoleResetPost,
			Summary: "Reset a role definition to seed (idempotent tombstone overlay). Answers with a bounded receipt (``key``, ``name``, ``is_default``, ``is_seed``, ``size_chars``, ``cap_chars``, ``sha256``), not the duty document — call ``get_role`` when you need the rest.",
			MCPTool: "reset_role",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/roles/{role}",
			Handler: w.HandleDeleteRoleApiRolesRoleDelete,
			Summary: "Hard-delete a custom role + its members (seed → 403; online → 409).",
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
			Summary: "Read a per-role insight doc - this role's accumulated judgement calls and trade-offs (per role_key). A role may ship with a factory seed, and that seed is PER-ROLE (seeds/insight_<role_key>.md) - today only the assistant has one; a role without one reads genuinely empty until it writes. is_default=true means THIS ROLE has never written its own, whether what you are reading is the factory wording or nothing at all. Reading is unrestricted: any authenticated identity may read ANY role's insight - it is SEPARATE, not private.",
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
			Summary: "Whole-doc replace of a per-role insight doc ({text}). text is REQUIRED; unknown keys are rejected. Replacing existing content with an empty doc needs allow_shrink=true. Only the role's own agents (and admin) may WRITE it. Answers with a bounded receipt (``role_key``, ``is_default``, ``has_seed``, ``size_chars``, ``cap_chars``, ``sha256``), not the folded doc — call ``get_insight`` when you need the rest.",
			MCPTool: "replace_insight",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/insight/{role_key}/patch",
			Handler: w.HandlePatchInsightApiInsightRoleKeyPatchPost,
			Summary: "Patch a per-role insight doc by unique anchors ({edits:[{old,new}]}). Only the role's own agents (and admin) may WRITE it.",
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
			Summary: "Reset a per-role insight doc back to its factory seed (idempotent tombstone of the overlay) - the counterpart of reset_role on the Duty block. A role with NO seed file (seeds/insight_<role_key>.md) returns 404: there must be a factory version to reset TO. No length cap is applied on this path, matching reset_role - the factory text is part of the product. The overlay you are discarding is retained as a document-history revision, so the reset is recoverable. Only the role's own agents (and admin) may do it. Answers with a bounded receipt (``role_key``, ``is_default``, ``has_seed``, ``size_chars``, ``cap_chars``, ``sha256``), not the folded doc — call ``get_insight`` when you need the rest.",
			MCPTool: "reset_insight",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/resume-summary",
			Handler: w.HandleResumeSummaryApiResumeSummaryGet,
			Summary: "Bounded LIGHT wake snapshot for the caller (identity-locked; recent chat + light open-task rows + size overview — peek sizes first, pull detail via get_task). CHAT is packed newest-first under a CHARACTER BUDGET, not a fixed message count, and stopping at the last message that still fits; each message carries from_name/to_name beside the ids and ts_display (full date + time + zone offset) beside the epoch ts, and folds in its reply card as `card` when it has one — read every ts_display against the top-level `generated_at`. TWO DIFFERENT things can be missing and they are marked DIFFERENTLY: `body_omitted_chars` > 0 means THAT message is here with that many characters COLLAPSED away (another agent's line — the owner's line and your own hand-off notes to yourself are carried in full), re-read it with get_chat; `chat_earlier_omitted` is the other kind and it is a MAYBE, not a fact: that line was cut at a read or budget limit and nothing looked past the cut, so whole messages may be missing from this payload entirely — it is raised even when there is in fact nothing older. Its hint tells you how to CHECK and fetch them. The two are asymmetric ON PURPOSE: the collapse marker is CERTAIN (that message IS here, shortened, exact count); this one is not, and only the fetch settles it. Also carries the STUDIO FLOOR you wake up onto: roster (every member and contractor, each with online/offline status, the machine it runs on, and its duty capped at 1000 chars with `…` marking a cut, the cap applied after the doc's own leading title line is removed — who to ask for help; no insight by owner ruling. Contractors additionally carry their bound task's status, waiting_reason, and step progress (progress_done/progress_total) — members leave these at their zero value; a contractor's 0/0 is ambiguous (a task with no steps yet, or no task at all) and task_status is what tells them apart, non-empty vs empty) and machines (the machine list plus you_are_on, your server-recorded machine binding — never derive it from a hostname).",
			MCPTool: "resume_summary",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/resume-summary-size",
			Handler: w.HandlePeekResumeSummarySizeApiResumeSummarySizeGet,
			Summary: "Size-only PEEK of the wake snapshot (identity-locked; overview counts/sizes + estimated_total_chars, NO chat/task content). estimated_total_chars is exactly chat_chars + tasks_detail_chars + roster_chars + machines_chars + steps_on_answered_card_chars, all five reported in overview: the WHOLE chat block as the snapshot renders it (chat_chars is the rendered block's cost, NOT the sum of the message bodies), plus the plan text its task rows omit, the two studio-floor blocks, and the named steps sitting on an answered card — what pulling the snapshot actually costs. Step one of the two-step boot: call this FIRST to size resume_summary, then either call resume_summary directly (small) or hand the pull to a cheap sub-agent that returns a digest (large).",
			MCPTool: "peek_resume_summary_size",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:     "POST",
			Path:       "/api/bootstrap",
			Handler:    w.HandleBootstrapApiBootstrapPost,
			Summary:    "Assemble an agent boot context + mint the member JWT (spawn seam).",
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
			Summary: "List tasks (?executor=&type=&status=, or statuses=[…] for a SET of states — every filter given is ANDed; LIGHT list items — id/task_no/title/type_key/status/priority/executor/creator_id/progress/timestamps/deps + dep_tasks + current_step_id/current_step_name, WITHOUT steps/description/inputs). Ask for the states you actually want (`statuses: [\"not_started\", \"in_progress\"]`) instead of listing everything and filtering yourself — the whole history is a large answer. `statuses` also accepts \"reassigning\", which matches the handover LOCK rather than the status column. `dep_tasks` already carries each blocker's task_no/title/status, so a blocked task needs no follow-up get_task just to name what it is waiting for. `current_step_id`/`current_step_name` name the step each task is ON right now: the FIRST step in plan order that is neither done nor superseded — the same step the wake snapshot points at. BOTH ARE THE EMPTY STRING in exactly two cases — the task has no plan yet (no steps at all), or every step has finished — and that empty means THERE IS NO CURRENT STEP; never read it as \"the first step\". The two fields are that step's id and that step's name, and nothing else about the step. The list still carries NO step rows (no dod text) — only those two fields; call get_task for a task's full detail (steps, description, inputs).",
			MCPTool: "list_tasks",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks",
			Handler: w.HandleCreateTaskApiTasksPost,
			Summary: "Create a task (dedupes on the manual's key; ad-hoc when type_key omitted). Pass target.kind=outsource to drop the task as an unassigned outsource task (發包); target.runtime is claude/codex (absent = claude). The existing outsource scheduler then spawns workers against the global concurrency cap (outsourceParallelCap) — below the cap it starts immediately, at the cap it queues for capacity and is picked up automatically when a slot frees. No owner-approval card and no per-task approval; the owner may reassign a still-queued task at any time. Caller authorization (正職授權矩陣, T-23cf): an outsource worker may never create a task; a 發包 create is open to any 正職 (owner/admin included); a typed task the manual assigns to member X may be created only by X (owner/admin NOT exempt); an ad-hoc task with a member executor may name only the caller itself unless the caller is owner/admin (a 一般正職 may self-execute or 發包, never assign another member). Answers with a bounded receipt (``task_id``, ``executor_kind``, ``executor_id``, ``deduped``, ``title``, ``status``, ``warnings``), not the task — call ``get_task`` when you need the rest. ``task_no`` is GONE from this answer (owner ruling rc-f1c0fd3cf124): it was the same string as ``task_id``, byte for byte, and the sibling task writes had already dropped it for that reason. The executor pair is what the SERVER chose — on a typed create it comes from the manual's assignee, so a caller that sent only ``type_key`` learns its placement here; an empty ``executor_id`` under ``outsource`` means the scheduler has not minted the worker yet.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `target`: Dispatch target. The kind is a CLOSED SET of two values, 'staff' and 'outsource' (surrounding whitespace is trimmed, case is not folded). Since T-101 every other spelling - 'outsourced', 'Outsource', a typo, or the pre-rename 'member' - is a 400 naming the field, and 'member' specifically is answered with a message saying it was RENAMED to 'staff', rather than one that merely lists the set. kind-vocab-guard:legacy Only 'outsource' dispatches: 'staff', and an omitted target, both create the task down the ordinary staff path. ONE quiet case survives the rename: an EMPTY kind (blank or whitespace only) is not validated and not warned about - the whole target block is discarded and the task is created down the ordinary staff path, answering 200 with an ordinary staff create. So a 發包 assembled from a variable that turned out empty can come back as a staff-assigned task - but the RECEIPT says so: it carries executor_kind and executor_id, the placement the SERVER chose, so one call is enough to catch the downgrade on a FRESH create. On a dedupe hit those two describe the EXISTING ticket this call folded onto, so `staff` there can mean either a downgraded target or a ticket that was always a staff member's - read `deduped` first. Check the created task's executor_kind whenever the kind you sent was not a literal. The MACHINE is the other quiet one, and it bites even when the kind is spelled right. Leaving target.machine out is not an error and usually costs nothing: the field is filled from the type manual's outsource assignee, and then from the machine YOU are pinned to as the creator - though an owner-scope caller has no roster row to lend and contributes nothing here. When none of those three names a machine the field simply stays empty: runtime and effort have defaults, a placement deliberately does not, and nothing else invents one. The task is still created with 200 and reads as normal, and the worker minted for it later does not boot at all until somebody names a machine on it - until then it just sits there with a no_machine_selected receipt on the WORKER row, which is the only place this ever surfaces.",
			MCPTool: "create_task",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/tasks/count",
			Handler:    w.HandleTaskCountApiTasksCountGet,
			Summary:    "Open task count (the tasks nav badge).",
			MCPExclude: true, // a UI badge convenience, not an agent tool
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/tasks/{task_id}",
			Handler: w.HandleGetTaskApiTasksTaskIdGet,
			Summary: "Read one task — and read it knowing it is a SUMMARY, not the whole of it: the response says so itself (``detail_level`` = ``summary``, ``notes_included`` = false). WHAT IS COMPLETE HERE: the task's own fields, its deps, its progress counts, its gate cards, and EVERY ONE of its steps. The step list has no cap, no paging and no truncation of any kind — the rows you get back are all the rows there are, so a step that is not here does not exist on this task. WHAT IS OMITTED, AND EXACTLY HOW MUCH OF IT: each step's working-note TEXT (T-66). In its place every step carries ``note_size_chars`` — the EXACT number of characters of note sitting on the server for that step, where 0 means that step genuinely has no note — and ``note_cap_chars``, the ceiling. A positive ``note_size_chars`` is a precise promise that that many characters are waiting for you, and ``get_task_step(task_id, step_id)`` is the one call that returns them, one step at a time. Read the sizes first, then fetch only the notes you actually need. THE PINNED DELIVERABLES ARE OMITTED THE SAME WAY, AND SINCE T-92 THERE IS NOT EVEN AN INDEX OF THEM: ``artifact_count`` is the only thing said about them here — an EXACT, un-truncated, un-capped count, 0 meaning the task genuinely has nothing pinned. No array, no ids, no names: ``list_task_artifacts(task_id)`` returns every artifact on the ticket, complete, in ONE call, and there is deliberately no per-artifact read. Ask for that list when you are going to USE an artifact; a count is what you need to know one exists. Unknown id → 404.",
			MCPTool: "get_task",
		}),
		Gated(principalAgent, routeDef{
			// T-182. The floor is principalAgent and the real gate is
			// callerMayMarkTaskDone — the task's OWN executor, outsource worker
			// included (unlike mark-terminated, which subtracts it).
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/mark-done",
			Handler: w.HandleMarkTaskDoneApiTasksTaskIdMarkDonePost,
			Summary: "Close this task as done — the action ``ready_for_done`` waits for. A task whose every step is reported done NO LONGER CLOSES ITSELF: it settles in ``ready_for_done`` and stays there, which is the one window in which the close-out can actually happen (pin the deliverables, finish the step notes, write the learnings back) — every one of those writes is refused once the task is terminal, and until this ticket the task went terminal in the same call that reported the last step. THIS call is what ends it. WHO: the task's OWN executor, staff member and outsource worker alike — the close-out is the executor's work, so whoever does it must be able to say it is finished. The owner and the admin assistant do not share this door; theirs is ``force_task_done``, which records that it was forced. PRECONDITION: the task must be in ``ready_for_done``. Anything else is a 409 that NAMES the status the task is actually in, because the two ways to fail here need different answers — a task still in a work state has a step nobody has reported, and a terminal one is already closed. NOBODY IS CHASED AND THERE IS NO TIMER: a task nobody closes simply stays in ``ready_for_done``. That is the deliberate price of never closing a task underneath the agent that is still packing it up; ``force_task_done`` is the way out. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest. THE BOUND OUTSOURCE WORKER IS DISMISSED HERE, and here is the only place it happens: the row releases AND the session is reclaimed at once, on all four closes alike. It used to wait for a separate close-out report, which is gone — the close-out now happens while the task is still open, so once a close lands there is nothing the worker is still allowed to do on it.",
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
			Summary: "Close this task as terminated — the work is being abandoned, not finished. Terminating is the one status change that never goes through the task's own step reports, so it requires no step to be in any particular state. WHO: the owner, an admin agent, or the task's OWN executor when that executor is a 正職 member (T-b56e, owner 2026-08-20 card rc-b896e3f641e7). A member terminating SOMEONE ELSE's task is a flat 403. An OUTSOURCE worker is refused HERE even on its own task — the owner's ruling named 執行者 and did not reach the contractor lifecycle, so this door stays shut until one does. ⚠️ THAT IS A FACT ABOUT THIS ROUTE, NOT A SYSTEM-WIDE GUARANTEE that a worker cannot close its own task: ``mark_task_duplicated`` sits at the same principalAgent floor, gates on callerMayDriveTask with no such subtraction, and reaches the same close (measured 2026-08-20 against this route's predecessor: 200 duplicated). Shutting that door too needs its own ruling. ANY NON-TERMINAL STATUS IS ACCEPTED, ``ready_for_done`` INCLUDED — giving up does not require the work to be complete first. Already done, terminated or duplicated is a 409. Stamps closed_ts and releases any bound outsource worker. REPLACES ``terminate_task``, REMOVED in the same release: the old name said what you were doing to the task, this one says the status the task lands in. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest. THE BOUND OUTSOURCE WORKER IS DISMISSED HERE, and here is the only place it happens: the row releases AND the session is reclaimed at once, on all four closes alike. It used to wait for a separate close-out report, which is gone — the close-out now happens while the task is still open, so once a close lands there is nothing the worker is still allowed to do on it.",
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
			Summary: "Close this task as done OVER its own precondition — the exit for a task that is never going to be closed by the agent holding it. Two shapes reach it: one sitting in ``ready_for_done`` whose executor is gone, and one still mid-plan whose remaining steps will never be reported. WHO: the OWNER and the ADMIN ASSISTANT only. Every other principal is a 403, the task's own executor INCLUDED — an executor that can force its own task simply has ``mark_task_done`` without a precondition, and the precondition is the whole point. ``reason`` is OPTIONAL — omitted, blank or whitespace-only all close the task (owner ruling rc-a92a6252c3bd). A forced close is the one close nobody can reconstruct afterwards from the steps, because the steps do not agree that the work is finished, so a reason is worth giving: it is asked for, not demanded. The forcing principal and the reason are recorded on the task and come back on every read as ``forced_done_by`` / ``forced_done_reason``, so a done task always says whether it got there by itself. ANY NON-TERMINAL STATUS IS ACCEPTED. Already terminal is a 409 — this forces the precondition, not the terminal wall. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest. THE BOUND OUTSOURCE WORKER IS DISMISSED HERE, and here is the only place it happens: the row releases AND the session is reclaimed at once, on all four closes alike. It used to wait for a separate close-out report, which is gone — the close-out now happens while the task is still open, so once a close lands there is nothing the worker is still allowed to do on it.",
			MCPTool: "force_task_done",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/priority",
			Handler: w.HandleSetTaskPriorityApiTasksTaskIdPriorityPost,
			Summary: "Set a task's priority (owner/admin agent any value on any task; the task's own executor any value on their task — frozen INCLUDED, and whoever may freeze may unfreeze, T-6020). The actor who sets frozen is recorded on the task as frozen_by and the field clears when the task leaves frozen. Anyone else is a flat 403. Answers with a bounded receipt (task_id, priority, frozen_by), not the whole task — use get_task when you need the rest.",
			MCPTool: "set_task_priority",
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26) — the admin 助理
			// pings a task's executor. Plain agents still 403.
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/message",
			Handler: w.HandlePostTaskMessageApiTasksTaskIdMessagePost,
			Summary: "Message the task's executor (owner/admin agent; task context auto-attached). Answers with a bounded receipt (``id``, ``ts``, ``to``, ``attachments``), not the message — call ``get_chat`` when you need the rest. ``to`` is the executor the server delivered to: you did not name it (you named a task), and a later read cannot recompute it, because the executor can change between two calls.",
			MCPTool: "post_task_message",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/plan",
			Handler: w.HandleSubmitTaskPlanApiTasksTaskIdPlanPost,
			Summary: "Submit/replace the workflow plan. ⚠️ Resubmitting permanently deletes every unfinished step that is not kept (see below), together with its working note; deleted notes cannot be recovered. If what you want is to change ONE step, do not resubmit the plan: ``insert_step``, ``delete_step`` and ``reorder_steps`` each touch the single row you name and leave every other step's note, status and bound card exactly as they are. Done steps, superseded steps, and unfinished steps whose most recently opened reply card is answered or expired are kept in their original order, and every step of the new plan is placed after them. Names match exactly and case-sensitively after surrounding whitespace is trimmed from each submitted name. Relisting a done step or an answered/expired-card step under the same name keeps that existing row (same id, note and status) and drops the relisted entry, so nothing in it (dod, is_gate, parallel_group) is written. Relisting any other unfinished step under the same name creates a new pending step with a new id, so copy any note you need before resubmitting; relisting a superseded step's name likewise creates a new pending step and leaves the superseded row as history. An answered/expired-card step whose name the new plan leaves out is frozen as superseded instead of deleted, so leaving its name out keeps its history without continuing the step. A plan is a step-set write and the task status is DERIVED from the step set, so a plan that leaves at least one step that is not superseded, and every such step done, FINISHES the task: it lands in ``ready_for_done``, exactly as the final step report does. Neither closes the task: a task in ``ready_for_done`` closes only through ``mark_task_done``, ``force_task_done``, ``mark_task_terminated`` or ``mark_task_duplicated``, and a close cannot be undone. Answers with a bounded receipt (task_id, steps_total, progress_done, progress_total), not the plan you just sent — use get_task to read the stored step rows back.",
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
			Summary: "Correct THIS task's own TEXT — its title, its description, or both in one write (T-646a). Replaces `update_task_title` and `update_task_description`, which documented the same rules twice and could not be applied together: changing both meant two calls, two transactions and two SSE deltas, with room for someone else's write to land in between. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or any of the four closes. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL: only the fields you NAME are touched, so omitting a field is a legal no-op for it that versions nothing and fans nothing. ⚠️ THE TWO FIELDS TREAT AN EXPLICIT BLANK DIFFERENTLY, and that is an owner ruling rather than an inconsistency (card rc-796541192519, 2026-08-11, option ①): a blank `title` (\"\" or whitespace-only) is REFUSED with 400 and does NOT clear the field, because create_task refuses a blank title too and an edit door looser than the create door would let a caller reach a task-list row with nothing in it; a blank `description` IS accepted and DOES clear the text, because plenty of cards legitimately have no prose. VALIDATION IS WHOLE-BODY AND HAPPENS FIRST: a request carrying a blank title alongside a perfectly good description writes NEITHER — a 400 leaves the task exactly as it was, never half-applied. Both values are trimmed of surrounding whitespace before they are stored AND before they are compared with what is there, so re-sending the same text with a stray trailing space is correctly seen as no change rather than spending one of the retained revisions saying nothing moved. ⚠️ THAT HOLDS ONLY WHILE THE STORED TEXT IS ALREADY TRIMMED. Whenever the stored description carries untrimmed whitespace, the next edit here normalises it and therefore DOES spend a revision — even when you re-send exactly what you read back. TWO things can put untrimmed text in that column, so this is not a one-time settling: create_task, which never trims the description (it does trim the title), and a RESTORE of a revision that holds untrimmed text, which is written back verbatim. Before this ticket both doors stored it raw and agreed; this tool trims and create still does not, which is a divergence awaiting a ruling rather than a promise about the system. The write is wholesale within each field: send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close: artifacts record what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters a field retains the previous value as a document version — kind `task_title` / `task_description`, key = the task id — so a correction is recoverable through list_document_history and the older wording is never simply gone. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest.",
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
			Summary: "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's description — the ticket's own text (what the task IS: scope, origin, acceptance). T-e271: until this tool existed there was NO way to change a description after creation — create_task takes one only at birth, submit_plan writes steps, update_task_manual writes the TYPE's manual — so a decision to reword a card had nowhere to land. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's text now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or any of the four closes. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL like update_task_manual: omitting `description` changes nothing (a safe no-op), while an explicit \"\" CLEARS it — absent and empty are different on purpose; unknown keys are refused rather than dropped. The write is wholesale within that field: the value replaces whatever was there, so send the full corrected text, not a fragment. ⚠️ Division of labour with update_step_note: the DESCRIPTION says what this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, handover-facing) — do not put progress here. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — unlike its artifact set, which freezes at close. The reason they differ: artifacts are the record of what the task PRODUCED and must stop moving, while a ticket worded wrongly is usually found to be wrong after it closed, and freezing the text would preserve a known falsehood in the permanent record. Every change that actually alters the text retains the previous one as a document version (kind `task_description`, key = the task id) — list it with list_document_history, so a correction is recoverable and the older wording is never simply gone.",
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
			Summary: "🔴 SINCE T-646a THIS ROUTE IS NO LONGER AN MCP TOOL — the agent-facing tool is `update_task`, which writes this same field through the same code. The route stays here for the cockpit and any existing HTTP client. What follows is why the capability exists and how it behaves; it is still accurate about the behaviour. Correct THIS task's title — the one line the task list shows. T-2ebe: until this tool existed a title could never be changed after creation, so a card whose scope was later overturned kept advertising its first wording forever — the description could correct itself, the title could not, and whoever scanned the list saw only the stale half. If you have just corrected a description because the scope moved, ask whether the title still says the same thing. WHO: the task's own executor, or an admin/owner; anyone else is a flat 403. Creating a task grants NO standing to keep rewriting it — if you handed the task over, it is the new executor's title now. ⚠️ ONE STRUCTURAL EXCEPTION (T-52, owner 2026-09-02): while the task has NO executor AT ALL (`executor_id` empty — where a 發包票 sits between create_task and the moment the scheduler binds a worker to it), its CREATOR may correct the text here, because otherwise nobody who is awake could fix the brief the contractor reads on arrival and that window has no upper bound. It SHUTS the instant an executor is bound — from then on the creator is a flat 403 again, even though it opened the ticket. TEXT ONLY: the same window opens add_task_artifact, remove_task_artifact, replace_task_artifact, update_step_note, patch_step_note and the task_title / task_description restores, and nothing else — never freeze, terminate, reassign, claim, plan, step status, deps or any of the four closes. `replace_task_artifact` sits in the same window as add/remove by owner ruling (card rc-09367ed77bc2, 2026-09-03, option [0]), given with these facts in front of him: replace OVERWRITES in place what someone else pinned, and remove_task_artifact deletes that artifact's every retained version together with their blobs. PARTIAL like update_task_description: omitting `title` changes nothing (a safe no-op); unknown keys are refused rather than dropped. ⚠️ ONE DIFFERENCE FROM ITS DESCRIPTION TWIN: a blank title (\"\" or only whitespace) is REFUSED with 400, it does NOT clear the field — create_task refuses a blank title too, and a task with no title is a blank row on the list. Surrounding whitespace is trimmed. The write is wholesale within that field: send the full corrected title, not a fragment. A CLOSED task (completed / terminated / duplicated) is STILL editable, on the same terms — a ticket is usually found to be worded wrongly after it closed, and freezing the text would preserve a known falsehood; its artifact set is the opposite and freezes at close. Every change that actually alters the text retains the previous one as a document version (kind `task_title`, key = the task id) — list it with list_document_history, so a correction is recoverable.",
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
			Summary: "Close this task as duplicated, pointing at the existing FINAL original it copies. ``duplicated`` is a terminal status alongside done and terminated, reachable only through this dedicated action — task status is otherwise derived from the steps and is never agent-reported. WHO: the task's executor; the owner and the admin assistant may act on any task. ``duplicate_of`` is REQUIRED (422 when blank) and must name an EXISTING task (404) that is not this one (409 self-reference) and is not itself already ``duplicated`` (409 — point at the FINAL original, the server never chases a chain); a task already named as an original cannot itself be marked duplicated (409). ANY NON-TERMINAL STATUS IS ACCEPTED, ``ready_for_done`` INCLUDED — a task can turn out to be a copy of another one at any point, the close-out window included. Already terminal is a 409. No dependency is added. REPLACES ``mark_duplicate``, REMOVED in the same release: same behaviour, a name that says which status the task lands in. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest. THE BOUND OUTSOURCE WORKER IS DISMISSED HERE, and here is the only place it happens: the row releases AND the session is reclaimed at once, on all four closes alike. It used to wait for a separate close-out report, which is gone — the close-out now happens while the task is still open, so once a close lands there is nothing the worker is still allowed to do on it.",
			MCPTool: "mark_task_duplicated",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/status",
			Handler: w.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost,
			Summary: "Report a step status (pending/in_progress/waiting_external/done). Reporting waiting_owner or superseded is a 400 (neither is agent-reportable), a transition the step state machine does not allow is a 409, and entering waiting_external requires a non-blank waiting_reason (422 otherwise); the task status is derived from its steps. A report that leaves every step that is not superseded done lands the task in ``ready_for_done``, which does not close it: a task closes only through ``mark_task_done``, ``force_task_done``, ``mark_task_terminated`` or ``mark_task_duplicated``, and once the task is closed it can never be replanned (submit_plan becomes a permanent 409).",
			MCPTool: "update_step_status",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/note",
			Handler: w.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost,
			Summary: "Write this step's working note: where the work stands and what comes next — the field the handover SOP means by 「把還在進行中的工作寫回 task step note」. WHAT TO WRITE — three things, then stop: (1) STATE — one sentence on where this step actually got to; (2) NEXT — one sentence on what whoever takes over does next; (3) EVIDENCE POINTERS — version ids, file and log paths, what you verified YOURSELF versus what you are taking on someone's word, and the limits of what was NOT done. Long narrative does not live here: reasoning and scope belong in the task description, reports and diffs belong on the task as artifacts. The note is the current state — not a report, not an append-only log. Writable in ANY step status (pending, in_progress, waiting_owner, waiting_external, done, superseded), unlike `waiting_reason`, which is locked to waiting_external. Wholesale write: `note` replaces whatever was there and \"\" clears it, so rewrite it as the work moves rather than appending; a note over the step note cap (counted in runes) is refused — that ceiling is the `task.step_note_cap_chars` setting, and every face that carries a note reports the live value as `note_cap_chars` — read it rather than assuming a number, because the setting is adjustable and this sentence is not regenerated when it moves. Same executor/admin gate as every other task-driving write (403 otherwise). ⚠️ Notes stay writable through ``ready_for_done`` — a task whose last step is reported done no longer closes itself, and that window is where the close-out writing belongs. What still 409s is a task someone has actually closed (``mark_task_done``/``mark_task_terminated``/``mark_task_duplicated``/``force_task_done``), so write the note before that call, not after. The receipt carries `size_chars` / `cap_chars`, so the room left is on every write instead of only on the 400 that refuses one; `get_task` reports the same pair per step as `note_size_chars` / `note_cap_chars`, but since T-66 it no longer carries the note TEXT — read a note back with `get_task_step(task_id, step_id)`, which answers that one step in full.",
			MCPTool: "update_step_note",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/note/patch",
			Handler: w.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost,
			// T-1667: the anchor-patch twin of the wholesale write above. Same
			// executor-or-admin gate — the handler shares it verbatim, so the two
			// faces onto one field can never disagree about who may write.
			Summary: "Patch this step's working note by unique anchors ({edits:[{old,new}]}) — send only the part that changed, instead of re-typing the whole note. USE THIS WHENEVER YOU ARE AMENDING A NOTE THAT ALREADY HAS CONTENT. update_step_note is a wholesale replace, so if anyone else wrote to the step between your read and your write, your copy is stale and the replace silently deletes their text — and because your stale copy is usually the LONGER one, no guard fires and nothing tells you. A patch sends no base copy, so it cannot overwrite from a stale read: each edit is applied to the note as it stands when the server reads it for this request, and a non-empty old must match that note EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400 that names which edit failed and which tool to re-read with, zero writes), so a concurrent write that removed or duplicated your anchor turns into a refusal you can see. It does not cover two writes processed at the same moment: the read and the write of one request share no transaction and no version check, so a write that lands between them is lost and both calls answer 200. Edits apply in order; an empty old appends. Wiping the note, or shrinking it below a tenth, needs allow_shrink=true — for an honest rewrite from scratch use update_step_note. Same executor/admin gate, same any-step-status generality, same closed-task 409 as update_step_note. Re-read with get_task_step after a refusal — get_task reports each step's note SIZE (note_size_chars) but since T-66 no longer carries its text. Answers with a bounded receipt (``task_id``, ``step_id``, ``step_status``, ``applied_edits``, ``size_chars``, ``cap_chars``, ``sha256``), not the note — call ``get_task_step`` when you need the rest.",
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
			Summary: "Read ONE step of one task IN FULL — the companion read to ``get_task``, which answers a SUMMARY. This response declares ``detail_level`` = ``full`` and carries that single step's ENTIRE working note (``note``) alongside its ``note_size_chars`` / ``note_cap_chars``, its status, DoD, ``waiting_reason``, gate flags, ``parallel_group``, bound ``reply_card_id`` and that card's live ``reply_card_status``. It carries NOTHING about the task itself and NOTHING about any other step, and that is the point: ``get_task`` tells you WHICH steps have a note (``note_size_chars`` > 0) and exactly how big it is, and this tool fetches one of them without dragging the whole ticket along. Same read floor as ``get_task`` — any authenticated principal may read any task's step; there is no executor gate on a READ. 404 for an unknown task, and 404 for a step id that exists but belongs to a DIFFERENT task: a step is only ever readable through its own task, so a wrong task_id never leaks somebody else's step.",
			MCPTool: "get_task_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/deps",
			Handler: w.HandleSetTaskDepsApiTasksTaskIdDepsPost,
			Summary: "Replace the blocking-deps list wholesale. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest.",
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
			Summary: "Reassign a task to a staff member or a fresh outsource worker (executor-guarded: a plain agent may reassign only a task it executes; owner/admin drive any task). Caller authorization (正職授權矩陣, T-23cf): owner/admin may hand a task to any active member or 發包 it to a fresh outsource worker; a 一般正職 may only turn its own task into a 發包 (a staff target is 403); an outsource worker may not reassign at all. An outsource target uses target.runtime claude/codex (absent = claude), lands the task unassigned for the scheduler to spawn under the global parallel cap, and enters the reassigning handover state. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest.",
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
			Summary: "Take over a reassigned task (the new executor claims it): clears the reassigning lock and fires the predecessor worker. The task status stays derived from its steps; only the lock is cleared. 409 if the task is not under the reassigning lock. Answers with a bounded receipt (``artifact_count``, ``closed_ts``, ``deps``, ``description_sha256``, ``description_size_chars``, ``duplicate_of``, ``executor_id``, ``executor_kind``, ``lock``, ``progress_done``, ``progress_total``, ``status``, ``task_id``, ``title``), not the task — call ``get_task`` when you need the rest.",
			MCPTool: "claim_task",
		}),
		Gated(principalAgent, routeDef{
			// The executing agent pins deliverables onto its own task card
			// (requires=agent; the handler's executor guard — caller == executor,
			// admin capability excepted — §14, same as the other agent write rows).
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/artifact",
			Handler: w.HandleAddTaskArtifactApiTasksTaskIdArtifactPost,
			Summary: "Register a deliverable (file, image, or link) onto the task's artifact set — the pinned deliverables shown on the task card. This verb only ADDS, and is repeatable: call it again to pin one more. To change what an ALREADY-PINNED deliverable points at, use replace_task_artifact instead of remove+add: it keeps the artifact id. THIS IS THE DOOR YOU HAVE FOR A LOCAL FILE, and it takes two steps: put the bytes in the store first (the chat-attachment upload), then pin that id here with kind=file|image + attachment_id. There IS a one-call route that stores and pins in the same transaction (POST /api/tasks/{task_id}/artifacts/upload, raw body), but nothing you can call reaches it today — it is excluded from the MCP tool set and no CLI subcommand drives it, so it is there for an HTTP client written directly against the REST API. Mind the gap the two steps leave: an upload with no pin after it leaves a blob nothing points at, which nothing goes looking for either. Use THIS call for a link (kind=link + url), or to pin a blob that is ALREADY in the store — an attachment someone sent you in chat, a file you pinned elsewhere — with kind=file|image + attachment_id, which is what that field is for now: reusing an existing blob rather than uploading a second copy of the same bytes. name is REQUIRED and is the display name (a link title such as \"PR #123\", a report's title), capped at 48 characters — Unicode runes, so 48 CJK characters fit — and a blank one is refused. description is optional prose about what this deliverable IS and why it is worth opening, capped at 256 runes; it is what the next reader has to go on, because a task response carries only a COUNT of artifacts. Both caps refuse rather than truncate, and both bind NEW writes only — artifacts pinned before they existed keep whatever they have. For a link, the url must begin with https:// or http:// and be at most 2048 characters (Unicode runes); anything else is refused with a 400 and never truncated, because the cockpit renders this string as a link the owner clicks. Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
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
			Summary: "Un-pin (remove) one artifact from a task's artifact set — the counterpart to add_task_artifact. You may remove artifacts from a task you are the executor of (the owner/assistant may remove on any task). Give the task id and the artifact id — the id returned when it was added, or from list_task_artifacts, which since T-92 is where artifact ids come from: get_task answers a count and carries none. The LIVE blob of a FILE or IMAGE is left intact, and on such an artifact that was never replaced only the pin on the card is removed. ⚠️ A LINK IS THE EXCEPTION and it is the one that can lose content: its ``text/uri-list`` blob is USUALLY its own — but not by construction: migration 00086 deduped identical targets, so 705 live link rows share 642 distinct blobs and two artifacts CAN point at the same one. Un-pinning hands it to the collector because a link does not QUALIFY for that exemption — the exemption exists for uploaded blobs that may also be riding a chat message, which a uri-list blob never is — and because sharing is real, the verdict has to be the collector's rather than settled at the un-pin (owner rc-27107ca914a7). It is not deleted outright — it joins the candidate list and survives if anything still-stored still references it — but do not read ``only the pin is removed`` as covering a link. BUT IF YOU HAD REPLACED IT, un-pinning also destroys its past: every retained version of this artifact is deleted in the same breath, and the files only those versions pointed at go with them, unrecoverably. ONLY WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen in every direction — remove is refused with the same 409 as add and replace. So swap a deliverable BEFORE you close the task, not after; after the close it can neither be removed nor put back. Answers with a bounded receipt (task_id, artifact_id, artifact_count), not the whole task.",
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
			Summary: "Read one task's pinned deliverables IN FULL — and since T-92 the ONLY call that returns an artifact row at all: ``get_task`` answers ``artifact_count`` and nothing else, no ids and no names. Answers ``{task_id, artifacts_detail_level, artifacts}`` where every artifact on the task is present, oldest→newest, complete: ``id``, ``kind`` (file|image|link), ``name`` (never empty — derived read-time from the blob's filename or the link target when the row has no stored name), ``description`` (the prose, possibly empty and possibly longer than the 256-rune write cap), ``url`` (where to go for the content — the blob serve path for a file/image, the external address for a link), ``mime`` (the blob's own content type — the authoritative answer to what the bytes are, which ``kind`` cannot give, since file covers .md and .pdf and .zip alike), ``filename`` (the BLOB'S OWN name, NOT the display name — ``name`` is that: it is what separates a .md from a .pdf from a .zip when ``mime`` says ``application/octet-stream``, which is what the agent upload path says about most of the reports pinned here, and it is empty for a link and for a file whose blob is gone), ``created_ts``, ``created_by``, ``version_count`` and ``attachment_id`` (the row's own blob id — the address ``ocagent diff`` takes, so a member can compare a deliverable without re-uploading it; for a LINK it is the ``text/uri-list`` blob holding the target, which ``url`` does not expose at all). ⚠️ This call is where that id COMES FROM: ``get_task`` answers a count, so this is the only place a member can pick one up in the first place. (One other response carries it — the artifact-history read, for RETAINED PREVIOUS versions rather than the live row — but it is ``x-mcp: include=false``, so it is not on the tool surface at all.). ONE call answers the WHOLE ticket, and that is deliberate — there is no per-artifact read, because whoever opens a task's deliverables wants the set (a 32-artifact ticket would otherwise cost 32 calls), whereas a step note is read one at a time and ``get_task_step`` is per-step for exactly that reason. Blob metadata is resolved read-time and is honest-empty when the underlying blob is gone — never fabricated. A task with nothing pinned answers ``artifacts: []``, not a 404; an unknown task id is a 404. Same read floor as ``get_task``: any authenticated principal may read any task's artifacts, and no field here was behind a stricter door before.",
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
			Summary:    "Pin a LOCAL file or image onto this task as a deliverable in ONE call (T-92, owner card rc-210fc77beea1): the raw request body IS the bytes (``application/octet-stream``; NOT base64, NOT multipart), the server stores the blob AND registers the artifact in the same transaction, and the answer is the ordinary add receipt \u2014 the new artifact's id plus the resulting count. THIS IS THE ONE-CALL PATH for bytes on disk \u2014 though no MCP tool and no CLI subcommand drives it today, so only a client written directly against this REST API can take it. The reason it exists is not convenience: upload-then-bind is TWO steps with a gap in the middle, and a caller who takes the first and not the second leaves a blob that nothing references and that nothing goes looking for \u2014 the collector runs when a retained version falls off the end, not as a sweep. One call has no such gap. ``?name=`` is REQUIRED (48 runes, refused not truncated, blank refused) and ``?description=`` optional (256 runes); ``?filename=`` and ``?mime=`` describe the BLOB exactly as they do on the chat-attachment upload, with an omitted mime falling back to a magic-byte image sniff and then ``application/octet-stream``. The request ``Content-Type`` header is deliberately IGNORED \u2014 clients default it to ``application/octet-stream``, indistinguishable from a real declaration; ``?mime=`` is the explicit channel. ``kind`` is not a parameter: an image mime pins ``image``, anything else pins ``file``. Size caps are the chat upload's exactly (one mechanism, not two): 20 MB for an ``image/*`` blob, 100 MB otherwise, with an over-cap or empty body a flat 400. Permission and freeze are add's exactly: the task's executor (admin excepted), 409 on a terminal task. Excluded from the MCP tool surface \u2014 a binary ingest seam like the chat-attachment upload, not a tool; ``add_task_artifact`` remains the JSON door for a link, or for reusing a blob already in the store.",
			MCPExclude: true,
		}),
		Gated(principalAgent, routeDef{
			// T-92 — the raw-body twin of replace, same reasoning as the add-side
			// upload above. It refuses a LINK artifact rather than converting it:
			// the kind is immutable across versions.
			Method:     "POST",
			Path:       "/api/tasks/{task_id}/artifact/{artifact_id}/replace/upload",
			Handler:    w.HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost,
			Summary:    "Replace a pinned file/image deliverable's content from a LOCAL file in ONE call (T-92) — the raw-body twin of ``replace_task_artifact``, keeping the artifact id exactly as that verb does. The request body IS the new bytes (``application/octet-stream``), the server stores the blob and swaps the live row in the same transaction, and the answer is the ordinary replace receipt (task_id, artifact_id, artifact_count, version_count). It exists for the same reason the add-side upload does: upload-then-replace leaves an unreferenced blob behind whenever the second step does not happen. ``?name=`` and ``?description=`` are OPTIONAL and an omitted one is CARRIED FORWARD, exactly as on the JSON replace; ``?filename=``/``?mime=`` describe the new blob. THE KIND CANNOT CHANGE: this route refuses a LINK artifact with a 400 rather than converting it, and the sniffed image/file distinction must match what is pinned. Permission, freeze, retention and blob collection are the JSON replace's exactly. Excluded from the MCP tool surface — a binary ingest seam, not a tool.",
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
			Summary: "Replace the CONTENT of one already-pinned deliverable while its artifact id stays exactly the same — the card keeps pointing at the same artifact and what sits behind it changes. Use this instead of remove+add whenever you are shipping a corrected version of something you already pinned: remove+add mints a NEW id, so anyone holding the old one is left pointing at nothing. For a file/image whose new bytes are on disk the one-call door is the task-scoped upload (POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload, raw body); use THIS call to point a file/image at a blob already in the store (attachment_id), or to change a link's target (url). THE KIND CANNOT CHANGE ACROSS VERSIONS: a file artifact stays a file artifact, so sending a url for one (or an attachment_id for a link, or an explicit kind that differs from what is pinned) is a 400 — un-pin it and register a new artifact if the kind is what you meant to change. name and description are optional here and an omitted one is CARRIED FORWARD: a replacement is a corrected version of the same deliverable, so you never re-type either just to swap the content. Sending one replaces it, and the length caps (48 runes for name, 256 for description) are checked ONLY against a value you actually send — omit the field and whatever is stored stands, however long it is. A blank name is refused, because every deliverable has a name; a blank description clears it. ⚠️ Some clients serialise an empty string as an omitted field, so \"omit to keep\" is reliable and \"send blank to clear\" is not — do not build on the latter. The version you replaced is KEPT and readable, but only the most recent few are retained: the oldest falls off the end for good when a newer one arrives, and the file it pointed at is deleted with it, so a version that has scrolled off is not recoverable from anywhere. ONLY WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen in every direction — replace is refused with the same 409 as add and remove, and admin/owner are not exempt. For a link, the url must begin with https:// or http:// and be at most 2048 characters (Unicode runes), refused with a 400 otherwise; and UNLIKE name and description it is re-validated on EVERY call - it has no carry-forward - so a caller that only means to change the name must still send back a url that passes. Answers with a bounded receipt (task_id, artifact_id, artifact_count, version_count), not the whole task.",
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
			Summary:    "List the retained previous versions of one pinned deliverable, newest first — what it pointed at before each replace. Read-only, cockpit-only, and only the most recent few are kept.",
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
			Summary: "Read an outsource worker's boot-context preview (owner/admin agent).",
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
			Summary: "List task types WITHOUT their long document: each row is the type identity (type_key / display_name / purpose), its input fields and its assignee setting, plus the SIZE of sop_md and the cap it is judged against. The SOP text is not on this answer at all — read the one type you picked with get_task_manual.",
			MCPTool: "list_task_manuals",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals",
			Handler: w.HandleCreateTaskManualApiTaskManualsPost,
			Summary: "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner/admin agent). An outsource assignee may select runtime claude/codex; absent = claude. Answers with a bounded receipt (``type_key``, ``updated_ts``, ``sop_md_chars``, ``sop_md_cap_chars``, ``sop_md_sha256``), not the manual — call ``get_task_manual`` when you need the rest.",
			MCPTool: "create_task_manual",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleGetTaskManualApiTaskManualsTypeKeyGet,
			Summary: "Read one task manual (purpose/fields/SOP/assignee). The SOP is judged against sop_md_cap_chars.",
			MCPTool: "get_task_manual",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleUpdateTaskManualApiTaskManualsTypeKeyPost,
			Summary: "Edit a task manual (partial; content fields agent-editable; assignee = owner/admin agent). An outsource assignee may select runtime claude/codex; absent = claude. Only the fields you name change, so omitting a field is safe — but unknown keys are rejected rather than dropped. The SOP is judged against sop_md_cap_chars. Answers with a bounded receipt (``type_key``, ``updated_ts``, ``sop_md_chars``, ``sop_md_cap_chars``, ``sop_md_sha256``), not the manual — call ``get_task_manual`` when you need the rest.",
			MCPTool: "update_task_manual",
		}),
		Gated(principalAdminAgent, routeDef{
			// T-6020: opened to admin_agent (owner 2026-07-26).
			Method:  "DELETE",
			Path:    "/api/task-manuals/{type_key}",
			Handler: w.HandleDeleteTaskManualApiTaskManualsTypeKeyDelete,
			Summary: "Delete a task type (open tasks of the type → 409).",
			MCPTool: "delete_task_manual",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/task-manuals/{type_key}/sop/patch",
			Handler: w.HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost,
			// T-1667: the anchor-patch twin of update_task_manual's sop_md field.
			// Same agent floor as every other manual CONTENT face; assignee (the
			// one governance field) is not reachable from here at all.
			Summary: "Patch a type's SOP (sop_md) by unique anchors ({edits:[{old,new}]}) — send only the section that changed, instead of re-typing the whole SOP. USE THIS WHENEVER YOU ARE AMENDING AN SOP THAT ALREADY HAS CONTENT. update_task_manual{sop_md} is a wholesale replace, so if anyone else edited the SOP between your read and your write, your copy is stale and the replace silently deletes their section — and because your stale copy is usually the LONGER one, no guard fires and nothing tells you. A patch cannot do that: a non-empty old must match the current sop_md EXACTLY ONCE (0 or >1 hits reject the WHOLE batch with a 400 that names which edit failed and which tool to re-read with, zero writes), so a concurrent write turns into a refusal you can see. Edits apply in order; an empty old appends. Wiping the doc, or shrinking it below a tenth, needs allow_shrink=true — for an honest rewrite from scratch use update_task_manual. The sop_md cap is judged on the RESULT and allow_shrink is not a bypass. Re-read with get_task_manual after a refusal.",
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
			Summary: "READ the CATALOGUE of retained versions of one editable document: which versions exist, when each was replaced and by whom, whether each was a tombstone, and HOW LONG each of its fields was. It does NOT carry the versions themselves — a version list is how you CHOOSE one, and choosing does not need the prose; fetch the one you picked with get_document_version. Read-only, newest first, and only the most recent few are kept — HOW MANY is per-document and is not stated here, because it differs by kind and this sentence would go stale silently; what you get back is the answer. Putting a version BACK is deliberately not an agent tool — the owner does that from the cockpit — so this cannot change anything.\n\nWHICH DOCUMENTS THIS COVERS, AND WHAT `key` LOOKS LIKE FOR EACH, ARE DELIBERATELY NOT LISTED HERE. A list of kinds — or of key shapes — written into a description goes stale the moment a new editable document ships, and NOTHING turns red when it does: this description used to enumerate six kinds and a key shape per kind, and both had already gone stale before the lists were taken out. Two rules you can actually execute replace them.\n\nADDRESSING: `kind` and `key` are validated by the same server-side gate that answers get_document_seed, so whatever that tool can address, this one can too, and the two can never silently disagree. A `kind` this server does not know is refused with 400; a retired kind is refused with 400 naming the series that replaced it. Some kinds also police the shape of `key` before answering — a key this kind does not serve, or one that fails that kind's required shape, is refused with 400 naming the problem. Neither is something to guess at: ask and read the answer.\n\nCOVERAGE: a syntactically valid `key` that simply has no retained versions yet is not an error — it returns an empty list, the honest 'nothing has been saved here', not a gap to work around.",
			MCPTool: "list_document_history",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/document-history/{kind}/{key}/seed",
			Handler: w.HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet,
			Summary: "READ the SHIPPED DEFAULT of one editable document — the text a reset would put back, i.e. the 初始版本 entry of that document's version list. Read-only: this tool writes nothing, so reading the default can never replace the live document. Putting the default BACK is deliberately not an agent tool — the owner does that from the cockpit — exactly as with list_document_history. ``content`` carries the SAME field names a retained version carries, so the same reader can compare a default against the live document.\n\nWHICH DOCUMENTS THIS COVERS IS DELIBERATELY NOT LISTED HERE. A list of kinds written into a description goes stale the moment a new editable document ships and NOTHING turns red when it does — this one had gone wrong about three kinds before the list was taken out. Two rules you can actually execute replace it.\n\nADDRESSING: ``kind`` and ``key`` name a document exactly as they do for list_document_history — the same server-side gate answers both routes, so whatever that tool addresses is addressable here, and a ``kind`` this server does not know is refused with 400 while a ``key`` that names no document of that kind is refused with 404 that names it. Neither is something to guess at: ask and read the answer.\n\nCOVERAGE: whether THAT document ships a default is answered by asking for it. 200 means it does, and ``content`` is that text. 404 means it has none at all — a role the owner created, a task manual — which is the same set whose reset the server also 404s, so it is the honest 'there is nothing to go back to', not a gap to work around. 400 on a retired kind names the series that replaced it.",
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
			Summary: "READ the BODY of one named retained version of an editable document — the ``content`` map that version was stored with, exactly as it was stored. Read-only: this fetches text, it never puts it back; restoring stays out of the agent tool surface, as it does for list_document_history.\n\nTHIS IS THE SECOND HALF OF A PAIR. list_document_history answers WHICH versions exist and how big each field of each one is, and carries no prose at all; this answers WHAT ONE OF THEM SAID. Name the ``id`` you read off that list. Asking for every version's text is the cost that pairing exists to remove, so fetch the one you actually mean to read.\n\nADDRESSING: ``kind`` and ``key`` name a document exactly as they do for list_document_history — the same server-side gate answers all three routes, so whatever that tool can address, this one can too, and they can never silently disagree. A ``kind`` this server does not know is refused with 400; a retired kind is refused with 400 naming the series that replaced it; a ``key`` that fails its kind's required shape is refused with 400 naming the problem. An ``id`` that is not a retained version of THAT document is a 404 — including an id that belongs to some other document, which is why the address is the whole triple and not the id alone.",
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
			Summary: "Restore one retained document version as a new write.",
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
			Summary: "List the product-guide docs (slug + title).",
			MCPTool: "list_docs",
		}),
		Gated(principalMachine, routeDef{
			Method:  "GET",
			Path:    "/api/docs/{slug}",
			Handler: w.HandleGetDocApiDocsSlugGet,
			Summary: "Read one product-guide doc in full (markdown; unknown slug → 404).",
			MCPTool: "get_doc",
		}),
		Gated(principalMachine, routeDef{
			Method:     "GET",
			Path:       "/api/docs/assets/{name}",
			Handler:    w.HandleGetDocAssetApiDocsAssetsNameGet,
			Summary:    "Serve a product-guide image asset (referenced by a doc's markdown).",
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
			Summary: "List the saved custom themes — id and name only, in list order (owner/admin agent).",
			MCPTool: "list_themes",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "GET",
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandleGetThemeApiThemesThemeIdGet,
			Summary: "Read one saved custom theme (unknown id → 404).",
			MCPTool: "get_theme",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  http.MethodPut,
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandlePutThemeApiThemesThemeIdPut,
			Summary: "Create or replace ONE custom theme; the bundle's id must match the path (owner/admin agent).\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `wording`: Optional per-language wording overrides (T-16a1 P3). Outer key = UI language (`zh` / `en` only); inner map = an i18n message-key (an internal code path such as `nav.tasks`, drawn from the generated messageKeys whitelist) -> the owner's replacement PLAIN TEXT. Values are plain text only (trimmed, <=200 runes, control chars / newlines rejected, per-language entry count capped); no HTML/CSS. Absent = no overrides. The server 422s any wording that violates the language set or the value rules. A code OUTSIDE the messageKeys whitelist is NOT a 422: it is DROPPED from the overlay and the request succeeds, so only the surviving codes are stored - and NOTHING IN THE RESPONSE SAYS WHICH. This used to read 'the response echoes a pruned `wording`', which was true while put_theme answered the theme; T-91 made it answer ThemeWriteReceiptDTO (`created`, `id`, `order_idx`, `updated_at`), so the pruned overlay is no longer visible on the write. GET the theme back to see which codes survived (an already-imported theme pack stays usable when the whitelist shrinks; the accepted cost is that a typo'd code silently does nothing).\n- `avatars`: Optional per-role avatar images (T-16a1 P5; extended per role in T-ea81). Keys are the closed set `staff` (一般正職 staff; this key was called `member` until T-57 renamed it. The rename is a HARD CUT: a bundle exported before it carries `member` and is REFUSED with a 422 naming the rename, never silently defaulted to the built-in glyph) / `outsource` (外包 outsource worker) / `owner` (the human CEO / owner) / `assistant` (a member whose role is `assistant`, e.g. Mira). Each value is an EMBEDDED image encoded as a base64 `data:` URI so the image travels inside the bundle on export/import. The value is NOT arbitrary: only a `data:image/<mime>;base64,<...>` URI whose mime is a whitelisted RASTER format (`image/png` / `image/jpeg` / `image/webp`) is accepted. SVG (`image/svg+xml`) is REJECTED (it can carry script/onload — XSS). The base64 must decode, the decoded byte size is capped (<=64 KiB) and the string length capped, and the leading magic bytes must match the declared mime (PNG `89 50 4E 47`, JPEG `FF D8 FF`, WEBP `RIFF....WEBP`) — a value that declares one mime but carries another is rejected. Absent = that role falls back to the built-in avatar glyph (office never degrades). The server 422s any avatars that violates the key set, the mime whitelist, the size caps, the base64, or the magic-byte check.\n- `logo`: Optional studio logo image (T-ea81). A single EMBEDDED image encoded as a base64 `data:` URI that replaces the built-in logo mark shown in the top bar. Same strict image gate as `avatars`: only a `data:image/<mime>;base64,<...>` URI whose mime is a whitelisted RASTER format (`image/png` / `image/jpeg` / `image/webp`) is accepted; SVG (`image/svg+xml`) is REJECTED (script/onload XSS); the base64 must decode, the decoded byte size is capped (<=64 KiB) and the string length capped, and the leading magic bytes must match the declared mime (PNG `89 50 4E 47`, JPEG `FF D8 FF`, WEBP `RIFF....WEBP`). Absent = the built-in logo mark is used (office never degrades). The server 422s any logo that violates the mime whitelist, the size caps, the base64, or the magic-byte check.\n- `navIcons`: Optional per-tab navigation icon images (T-ea81). Keys are the closed set of the five nav tabs `office` (辦公室) / `replies` (請示) / `tasks` (任務) / `monitor` (監控) / `guide` (使用說明); each value is an EMBEDDED image encoded as a base64 `data:` URI that replaces that tab's built-in icon. Same strict image gate as `avatars`: only a `data:image/<mime>;base64,<...>` URI whose mime is a whitelisted RASTER format (`image/png` / `image/jpeg` / `image/webp`) is accepted; SVG (`image/svg+xml`) is REJECTED (script/onload XSS); the base64 must decode, the decoded byte size is capped (<=64 KiB) and the string length capped, and the leading magic bytes must match the declared mime (PNG `89 50 4E 47`, JPEG `FF D8 FF`, WEBP `RIFF....WEBP`). Absent = that tab falls back to its built-in icon (office never degrades). The server 422s any navIcons that violates the key set, the mime whitelist, the size caps, the base64, or the magic-byte check.\n- `backgrounds`: Optional background images for the cockpit's chrome (T-081b). Keys are a CLOSED set holding exactly one zone today: `canvas` — the OUTERMOST canvas, i.e. the page background behind the centred content column. In `tile` / `sides` mode only the area BESIDE that column shows it, which is nothing on a phone, in a narrow window, or in the wide layout (all of which leave no side gutter); in `cover` mode the image fills the viewport and shows through wherever the theme gives the chrome zone colours translucent values. The zone tokens above it (`--color-topbar-bg` / `--color-nav-bg` / `--color-main-bg`) deliberately take NO image: they sit under text, and text over a busy pattern has no readability guarantee. Each value is an EMBEDDED image encoded as a base64 `data:` URI laid on top of the solid `--color-bg`, which still shows through wherever the image does not cover; HOW it is laid down is `backgroundModes` (default `tile`). Same strict image gate as `avatars`: only a `data:image/<mime>;base64,<...>` URI whose mime is a whitelisted RASTER format (`image/png` / `image/jpeg` / `image/webp`) is accepted; SVG (`image/svg+xml`) is REJECTED (script/onload XSS); the base64 must decode, and the leading magic bytes must match the declared mime (PNG `89 50 4E 47`, JPEG `FF D8 FF`, WEBP `RIFF....WEBP`). The SIZE caps are the one place backgrounds differ from `avatars`: the decoded byte size is capped at <=512 KiB (with the raw data-URI string capped above the base64-inflated form of that), NOT the <=64 KiB that applies to `avatars` / `logo` / `navIcons`. A background is stretched or tiled across the whole viewport, where the glyph-sized 64 KiB cap read as visibly blurry; that cap — and the T-081b ruling that a background must never exceed it — was overturned by the owner on 2026-08-03, and the small-glyph fields keep 64 KiB. Absent = the outer canvas is the plain `--color-bg` colour, exactly as before this field existed. The server 422s any backgrounds that violates the key set, the mime whitelist, the size caps, the base64, or the magic-byte check.\n- `backgroundModes`: Optional per-zone DISPLAY MODE for the images in `backgrounds` (T-081b). Same CLOSED zone-key set as `backgrounds` (today: `canvas`); each value is one of a CLOSED set of three: `tile` — repeat the image in both axes over the whole canvas (the ONLY behaviour before this field existed, and the default for any zone this map omits, so an older bundle renders identically); `sides` — do NOT repeat at all: pin ONE copy of the image against the LEFT viewport edge and one against the RIGHT, both at natural size, aligned to the viewport bottom, with `--color-bg` filling whatever the image does not reach; `cover` — ONE copy scaled (`background-size: cover`) to fill the whole viewport, centred. `sides` exists for art that reads as a pair of standing objects (e.g. a tree either side) rather than a texture; the product does NOT mirror the right-hand copy — a theme wanting left/right symmetry bakes it into the image (owner 2026-07-27). `sides` and `cover` are pinned to the VIEWPORT (`background-attachment: fixed`) because the canvas background otherwise scrolls with the document, which would bring a second copy into view down a long page. `cover` is only VISIBLE where the theme also gives the chrome zone colours (`--color-topbar-bg` / `--color-nav-bg` / `--color-main-bg`) translucent values — the colour grammar already admits `#RRGGBBAA` and `rgba()` — and that is also where its risk lives: those zones sit under text, so the image's contrast against that text is the theme's own responsibility (owner accepted this trade-off on 2026-07-27). A mode for a zone that carries no image is a 422 (a mode alone paints nothing, so it is a mistake worth naming rather than ignoring). Absent = every zone tiles, exactly as before this field existed. NOTE both modes are invisible at viewport widths where the content column leaves no gutter (phones, narrow windows) — that is a property of the outer canvas, not of the mode.",
			MCPTool: "put_theme",
		}),
		Gated(principalAdminAgent, routeDef{
			Method:  "DELETE",
			Path:    "/api/themes/{theme_id}",
			Handler: w.HandleDeleteThemeApiThemesThemeIdDelete,
			Summary: `Delete one custom theme; deleting the active one resets display_theme to "".`,
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
			Summary: "加速停止: put an ALREADY-OPEN wind-down on the stop.accelerated_grace_secs clock and tell the member. 409 if nothing is winding down -- press 停止 first. Middle rung of 停止 -> 加速停止 -> 強制停止. Also accepts an outsource-worker id: the same verb puts the worker's ALREADY-OPEN wind-down (a stop or a handover) on that clock and tells it; 409 when none is open. Answers with a bounded receipt (``id``), not the roster row — call ``get_member`` when you need the rest.",
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
			Summary: "Write ONE 傳承 entry (its title and body are never editable afterwards). ``task_id`` picks the scope, and there is ALWAYS somewhere for it to land: a task that carries a TYPE files under that type's manual; no task at all, OR a task with no type (臨時任務), files into your OWN boot document -- an ``agent`` scope keyed to you, staff and outsource alike. A write never files to ``everyone`` (所有人): only an admin's set_lore_entry_scope moves an entry there. The untyped-task case answers a ``scope_note`` saying where it actually went, because you asked for a manual and did not get one. Only a caller with no roster row at all is a 400 -- there is no boot document to file into. Title and body are each capped in characters (Unicode code points) by settings the owner or an admin agent can change (``lore_cap_chars_title`` and ``lore_cap_chars_body`` in ``update_settings``); an over-cap title or body is a 400 that writes nothing and states the cap in force.",
			MCPTool: "write_lore_entry",
		}),
		Gated(principalAgent, routeDef{
			Method:  "GET",
			Path:    "/api/lore",
			Handler: w.HandleListLoreEntriesApiLoreGet,
			Summary: "List 傳承 entries, filtered SERVER-SIDE and paged in the fixed order pinned -> active -> retired, newest first inside each group. The order is not configurable; the filter is. Nothing is narrowed by who is calling: with no author or scope filter, every caller sees every entry.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `scope_kinds`: REPEATABLE scope-kind set (``?scope_kinds=agent&scope_kinds=manual``). Accepted values: ``agent``, ``manual``, ``everyone`` (所有人 — its entries carry an empty scope_key, so filter it without a scope_key); ANY other element is a 400 that NAMES the offending value — never a silently dropped one, because 「查無資料」 and 「你打錯字」 look identical on the wire. 🔴 ``role`` IS NOW ONE OF THOSE REFUSED VALUES. It was the third scope until 2026-09-07 (owner, card rc-a43100fd0486 [0]) and every client written before then knows it, so it is the one wrong value likely to arrive from a real caller — answering 200-with-no-rows would tell them their 傳承 had been deleted rather than that their vocabulary is old. Refusing it does NOT hide the legacy rows the migration deliberately left at ``role``: those still come back on any page that does not constrain this axis. 🔴 PLURAL WINS. When this and its singular twin are BOTH sent, this one is the filter and the singular is ignored — they are neither ANDed nor unioned. Absent, or present but all-blank, falls back to the singular; both empty means no constraint on this axis. NOTE the 上限線: ``cap_chars`` / ``first_dropped_id`` are answered only when the EFFECTIVE scope_kind set holds exactly ONE value AND the effective scope_key set holds exactly ONE — a budget belongs to a scope, so a page spanning two or more has no single one to report and answers 0 / \"\". additive-optional.\n- `scope_keys`: REPEATABLE scope-key set (``?scope_keys=m-1a2b&scope_keys=tm-review``) — the multi-select twin of ``scope_key``. The keys are free-form (a member id or a manual's type_key, depending on the kind beside them), so there is no closed set to check against and no 400: a key nobody carries answers 200 with no rows for that key. 🔴 PLURAL WINS. When this and its singular twin are BOTH sent, this one is the filter and the singular is ignored — they are neither ANDed nor unioned. Absent, or present but all-blank, falls back to the singular; both empty means no constraint on this axis. NOTE the 上限線: ``cap_chars`` / ``first_dropped_id`` are answered only when the EFFECTIVE scope_kind set holds exactly ONE value AND the effective scope_key set holds exactly ONE — a budget belongs to a scope, so a page spanning two or more has no single one to report and answers 0 / \"\". additive-optional.\n- `entry_ids`: REPEATABLE 傳承編號 set (``?entry_ids=L-12&entry_ids=L-30``) — the multi-select twin of ``entry_id``, and the axis behind the 傳承編號 search box the design calls for (LORE_SPEC.md §6). 🔴 IT MATCHES THE WHOLE ID, EXACTLY — never a prefix and never a substring. Owner 2026-09-08 asked for it 「跟 task 一樣」, and 任務頁 resolves a committed id by asking for THAT ONE id (``useTasks.ts:201`` ``api.getTask(anchorId)``) and pairs it to a row by equality (``TasksPage.tsx:458`` ``x.id === appliedId``); nothing there ever compares part of an id. A substring axis would also interact badly with paging: ``L-1`` would drag L-10…L-19 into a batch that limit/offset then cuts, pushing the entry actually asked for off the end. 🔴 IT IS APPLIED IN SQL, WITH THE PAGE — like every other axis here, and for the reason the whole filter exists: this list is scroll-to-load, so an id narrowed client-side would make 「捲到底沒有了」 and 「真的沒有了」 the same picture and would draw the 上限線 in the wrong place. An id is an OPEN identifier space, so there is NO closed set to check and NO 400 — the same call ``scope_keys``/``author_ids`` make. An id no entry carries answers 200 with no rows, which is the true answer, and no ``L-`` + digits shape is enforced: it would refuse only the ids that could never match while still answering an empty page for ``L-99999``, the likelier miss. 🔴 PLURAL WINS. When this and its singular twin are BOTH sent, this one is the filter and the singular is ignored — they are neither ANDed nor unioned. Absent, or present but all-blank, falls back to the singular; both empty means no constraint on this axis, which is IDENTICAL to not sending the parameter at all (an empty set is 「do not narrow」, never 「match nothing」). NOTE the 上限線 is unaffected: ``cap_chars`` / ``first_dropped_id`` still depend only on the effective scope_kind and scope_key sets holding exactly one value each — a budget belongs to a scope, and naming one entry does not name a scope. additive-optional.",
			MCPTool: "list_lore_entries",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/state",
			Handler: w.HandleSetLoreEntryStateApiLoreEntryIdStatePost,
			Summary: "Move one 傳承 entry to active / pinned / retired. 置頂 and un-置頂 are ADMIN-ONLY (owner ruling): a pinned entry sorts ahead of every other entry in its scope and so survives the cap at the others' expense. 失效 and 生效 are open to the entry's own AUTHOR -- anyone else is a 403 -- and admin capability is unrestricted. A PINNED entry is outside its author's reach as well: until an admin un-pins it, every state change a non-admin requests on it, the author's included, is a 403. The author can still move it to the top of its group with ``bump_lore_entry``. Retiring is not deleting: the entry keeps its id and can be moved back; ``retire_reason`` is stored only with retired and cleared by the other two.",
			MCPTool: "set_lore_entry_state",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/lore/{entry_id}/bump",
			Handler: w.HandleBumpLoreEntryApiLoreEntryIdBumpPost,
			Summary: "提到最新: set one 傳承 entry's ``effective_ts`` to now so it sorts to the front of its group. Only the entry's own AUTHOR may bump it (admin capability is unrestricted) -- a bump moves an entry ahead of other people's under a shared cap, so it spends somebody else's room. ``created_ts`` is NOT touched, which is what makes this reversible.",
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
			Summary: "Insert ONE step into this task's plan, in front of a step you name. WHY IT EXISTS: submit_plan is a WHOLESALE replace — it permanently deletes every unfinished step together with its working note — so adding one step mid-task used to mean destroying the steps already under way, the notes on them, and the gate step still waiting for the owner's answer. This call moves nothing else: every other step keeps its id, its status, its note and its bound reply card, so a step sitting in waiting_owner goes on waiting and the owner's answer still lands on it. WHERE IT LANDS: `before_step_id` names the step the new one goes IN FRONT OF; omit it (or send \"\") to append at the END of the timeline. An id that names no step on this task is a 404, and one that names a FINISHED step (done / superseded) is a 409 — finished work is history and nothing is inserted into it. `name` and `dod` must both be non-blank (400), the same quality gate a submitted plan carries. The resulting timeline must still satisfy the parallel-group shape rules (400): steps sharing a parallel_group sit consecutively and number at least two, and a gate step carries no parallel_group. WHO: the task's own executor, or an admin/owner — anyone else is a flat 403; a CLOSED task is a 409. ⚠️ THERE IS NO OVERWRITE PROTECTION, and that is an owner ruling (rc-5160b97384c4, 2026-09-16): this call carries no version, compares nothing and retries nothing. When two writes to the same plan land at the same moment THE LATER WRITE WINS and the earlier one is simply gone — no error, no signal, and nothing to read afterwards that says it happened. Answers with a bounded receipt (`task_id`, `step_id` — the new step's id — `steps_total`, `progress_done`, `progress_total`), not the plan; call get_task to read the step rows back.",
			MCPTool: "insert_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/{step_id}/delete",
			Handler: w.HandleDeleteTaskStepApiTasksTaskIdStepsStepIdDeletePost,
			Summary: "Delete ONE unfinished step from this task's plan, leaving every other step exactly as it is — id, status, working note and bound reply card all untouched. WHY IT EXISTS: the only way to drop a step used to be submit_plan, which replaces the whole plan and permanently deletes every unfinished step's note along the way. WHAT IT REFUSES: a step id that names no step on this task is a 404; a FINISHED step (done / superseded) is a 409, because terminal rows are immutable history; deleting the last remaining step is a 400, because a planned task cannot have zero steps. WHO: the task's own executor, or an admin/owner — anyone else is a flat 403; a CLOSED task is a 409. Removing the last UNFINISHED step leaves every remaining step done, which FINISHES the work — the task lands in ready_for_done, one mark_task_done away from a close that can never be undone. ⚠️ A step CAN be deleted while it is holding a reply card the owner has not answered — waiting_owner is not a finished state, and this call does not look at the card. The card stays in the owner's queue with no step behind it and the later answer lands as a safe no-op, which is exactly what submit_plan has always done to a replaced waiting-card step; this door just makes it cheaper to reach one step at a time. If the question no longer matters, expire the card yourself rather than leaving it sitting there. ⚠️ THERE IS NO OVERWRITE PROTECTION, and that is an owner ruling (rc-5160b97384c4, 2026-09-16): this call carries no version, compares nothing and retries nothing. When two writes to the same plan land at the same moment THE LATER WRITE WINS and the earlier one is simply gone — no error, no signal, and nothing to read afterwards that says it happened. Answers with a bounded receipt (`task_id`, `steps_total`, `progress_done`, `progress_total`), not the plan; call get_task to read the step rows back.",
			MCPTool: "delete_step",
		}),
		Gated(principalAgent, routeDef{
			Method:  "POST",
			Path:    "/api/tasks/{task_id}/steps/reorder",
			Handler: w.HandleReorderTaskStepsApiTasksTaskIdStepsReorderPost,
			Summary: "Reorder this task's UNFINISHED steps. Nothing is rebuilt: every step keeps its id, its status, its working note and its bound reply card — only the positions change. That is the whole difference from submit_plan, where re-listing a step under the same name mints a NEW step with a new id and the old note is gone. `step_ids` is the COMPLETE ordered list of this task's unfinished step ids: all of them, in the order you want them, and nothing else. FINISHED steps (done / superseded) do not appear in it and do not move — they keep the timeline positions they already hold, and the unfinished steps fill the positions that are left, in the order given. A `step_ids` that is not exactly the set of this task's unfinished steps is a 400 — one missing, one repeated, one that names no step on this task, or one that names a finished step, each refuses the whole call and nothing is written. The resulting timeline must still satisfy the parallel-group shape rules (400). WHO: the task's own executor, or an admin/owner — anyone else is a flat 403; a CLOSED task is a 409. ⚠️ THERE IS NO OVERWRITE PROTECTION, and that is an owner ruling (rc-5160b97384c4, 2026-09-16): this call carries no version, compares nothing and retries nothing. When two writes to the same plan land at the same moment THE LATER WRITE WINS and the earlier one is simply gone — no error, no signal, and nothing to read afterwards that says it happened. Answers with a bounded receipt (`task_id`, `steps_total`, `progress_done`, `progress_total`), not the plan; call get_task to read the step rows back.",
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
			Summary: "Move one 傳承 entry to a different scope — that is, change who receives it. ADMIN-ONLY: the owner or an admin agent; anyone else, the entry's own author included, is a 403. You send only the target ``scope_kind``, never a ``scope_key`` — the server derives the key. ``agent`` keys the entry to its AUTHOR, so it rides that member's own boot document; it is a 400 when the author has no roster row. ``manual`` keys it to the entry's TASK TYPE, so it rides ``get_task_manual``: the type_key of the entry's ``source_task_id`` when it has one (a 臨時任務 with no type gives none); only when it has NO source task and the author is an outsource member, the type_key of the task that member was bound to. ``manual`` on an entry with no derivable type is a 400. ``everyone`` (所有人) keys it to \"\" and puts it in the boot document of EVERY member — staff, outsource and mira alike; this call is the only way an entry reaches ``everyone``, because write_lore_entry never files there. Any other ``scope_kind`` is a 400; an unknown entry is a 404. The entry's ``scope_options`` (list_lore_entries) names exactly the kinds this call accepts for it. Asking for the scope the entry already has is a 200 that changes nothing. A legacy ``role`` entry can be moved to any offered kind, never back to ``role``. Nothing else about the entry moves: ``state``, ``effective_ts``, title and body stay as they are. The author is NOT notified and NO change history is kept; the move takes effect the next time a member boots. Answers with a bounded receipt (``id``, ``scope_kind``, ``scope_key``, ``state``, ``effective_ts``, ``updated_ts``), not the entry; call list_lore_entries for the rest of it.\n\nPARAMETER NOTES. In the input schema the parameters below carry only a short summary; these are their full rules.\n- `scope_kind`: The TARGET scope, one of ``agent``, ``manual``, ``everyone``. Anything else — the retired ``role`` included — is a 400 that names it. ``manual`` is also a 400 when the entry has no derivable task type, and ``agent`` when its author has no roster row; either is then absent from the entry's ``scope_options``. There is no ``scope_key`` field: the server derives the key itself (the author's member id / the task type_key / \"\").",
			MCPTool: "set_lore_entry_scope",
		}),
	}
	out := make([]RouteSpec, len(rows))
	for i, r := range rows {
		out[i] = r.RouteSpec
	}
	return out
}
