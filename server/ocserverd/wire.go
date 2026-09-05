package main

// wire.go — hand-written response DTOs, the byte-shape twins of
// the retired Python service/dto.py (M3 REST sub-batch B).
//
// Why hand-written next to the generated ocapi_gen.go types: the generated
// structs carry `omitempty` on every optional field and marshal keys
// alphabetically, while the Python wire ALWAYS serialises every declared field
// (null, never omitted) in Pydantic declaration order. Conformance semantic
// checks read exact keys (e.g. bootstrap preview's `token: null`), so the
// response side locks the Python shape here; the GENERATED types remain the
// request-body vocabulary (pointer fields distinguish absent from zero).
//
// The single-owner reshape kept the frozen wire: `owner_id` / `schema_version`
// no longer exist in the Go store, so they serialise as the constants the
// Python single-tenant runtime always produced ("owner" / 3).

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// wireOwnerID is the fixed single-tenant owner id (service.deps.DEFAULT_OWNER).
const wireOwnerID = "owner"

// wireSystemSender is the synthetic chat sender for SERVER-AUTHORED task
// messages (T-ba04 reassign handover notices) — the analogue of the webhook
// ingest's "hook:"+id sender (api_webhooks.go). Using it instead of the
// caller's own id (currentActor, = the owner when the owner drives a reassign)
// keeps an automated handover message from being falsely attributed to the
// owner in the chat stream. It is a NON-roster id: the owner/dashboard SSE
// connection always receives every frame regardless of audience (hub.Publish),
// and the recipient is addressed explicitly, so the fan-out is unaffected; the
// FE resolves it to the localized 「系統」 label (ChatArea nameOf).
const wireSystemSender = "system"

// wireSchemaVersion mirrors domain.base.SCHEMA_VERSION — a wire constant now
// (the Go schema dropped the per-row column; the goose version is the schema
// version).
const wireSchemaVersion = 3

// authStatusDTO is the PUBLIC first-run probe body (GET /api/auth/status).
//
// MFARequired is ALWAYS emitted (no omitempty): the schema marks it optional so
// an older client keeps working, but a server that speaks this field must say
// `false` out loud rather than leave the login wall to guess from an absence.
type authStatusDTO struct {
	PasswordSet bool `json:"password_set"`
	MFARequired bool `json:"mfa_required"`
}

// mfaStateDTO is the answer to every /api/auth/mfa/* write.
//
// Secret / OtpauthURI are pointers because null and "" are DIFFERENT facts on
// this wire: null = "there is no pending secret to show you", which is what
// activate and disable answer. They are populated ONLY by enroll, and only for
// a pending (unproven) secret — an ACTIVE secret is never echoed back, so a
// stolen owner token cannot read out an existing enrolment and clone it.
type mfaStateDTO struct {
	// Offered is the ship-dark feature flag — whether the factor may be SET UP.
	// NOT a second opinion on whether one is armed (that is Enrolled), and never
	// consulted when deciding to verify a code.
	Offered    bool    `json:"offered"`
	Enrolled   bool    `json:"enrolled"`
	Secret     *string `json:"secret"`
	OtpauthURI *string `json:"otpauth_uri"`
}

// settingsDTO is the owner-adjustable settings surface (GET/PATCH
// /api/settings).
type settingsDTO struct {
	OwnerTokenTTL int64 `json:"owner_token_ttl"`
	AgentTokenTTL int64 `json:"agent_token_ttl"`
	// The offboard points are a PAIR on each runtime's own axis (T-a9d6):
	// NoticePct / CodexNoticeRound is the SOFT notice, HandoverPct /
	// CodexCompactionThreshold the FINAL one — and the final one is also where
	// the handover itself fires, so there is no third number that could
	// disagree with it about when a session ends.
	HandoverPct              int `json:"handover_pct"`
	NoticePct                int `json:"notice_pct"`
	CodexCompactionThreshold int `json:"codex_compaction_threshold"`
	CodexNoticeRound         int `json:"codex_notice_round"`
	MonitoringRefreshSeconds int `json:"monitoring_refresh_seconds"`
	OutsourceMaxParallel     int `json:"outsource_max_parallel"`
	// AcceleratedGraceSecs is the 加速停止 grace in seconds
	// (stop.accelerated_grace_secs; T-ed79) — how long a CLOCKED wind-down waits
	// before the collection is forced. It is ONE number on purpose: every clocked
	// cause reads it through recycleGraceFor, so the countdown quoted in the
	// agent's notice and the deadline the reconcile tick collects on are the same
	// value by construction rather than by two settings that happen to agree.
	// It cannot put a clock on a soft cause — winddownKindFor still decides WHO
	// is clocked, and this only says HOW LONG.
	AcceleratedGraceSecs int `json:"accelerated_grace_secs"`
	// DocCapChars* are the live size caps on the accumulating context
	// documents, in CHARACTERS (runes) — the same unit the patch receipts and
	// the refusal message speak (T-3aeb). FIVE independent knobs: a role's
	// Duty / Insight / Learning since T-ae38, plus the task manual's two long
	// docs, which T-30f1 gave a knob EACH (keyed by type_key, so assets of a
	// task TYPE rather than of a journal). Every wire name carries its suffix
	// for the same reason the DB keys do — an unsuffixed one, or a bare
	// `manual` beside the two it was split into, reads as a global default.
	DocCapCharsDuty            int `json:"doc_cap_chars_duty"`
	DocCapCharsInsight         int `json:"doc_cap_chars_insight"`
	DocCapCharsLearning        int `json:"doc_cap_chars_learning"`
	DocCapCharsManualSop       int `json:"doc_cap_chars_manual_sop"`
	DocCapCharsManualLearnings int `json:"doc_cap_chars_manual_learnings"`
	// The two boot-context document kinds, editable since T-791e. One knob per
	// kind, and the boot-sequence one is shared by the claude and codex
	// documents (each measured on its own text).
	DocCapCharsSystemInteraction int `json:"doc_cap_chars_system_interaction"`
	DocCapCharsBootSequence      int `json:"doc_cap_chars_boot_sequence"`
	DocCapCharsOffboard          int `json:"doc_cap_chars_offboard"`
	// ChatBudgetChars is the wake snapshot's chat block budget (chat.budget_chars;
	// T-c9b4). NOT a doc cap: it bounds a block repacked on every read, so unlike
	// the seven above it may be lowered as well as raised, and its ceiling is its
	// own (tied to resumeChatFetch, see domain.go).
	ChatBudgetChars int `json:"chat_budget_chars"`
	// BackupRetain is N — how many database backup files rotation KEEPS
	// (backup.retain; T-8). Two things about this number that its type does not
	// carry, and that the settings page therefore has to say out loud:
	// it counts VERSIONS, not days, and it is PER POOL, not per directory.
	BackupRetain int `json:"backup_retain"`
	// UpdaterReceiveBeta / UpdaterAutoUpdate are the two software-update
	// toggles (default false): follow GitHub prereleases too / self-upgrade
	// in the background when a newer release exists.
	UpdaterReceiveBeta bool `json:"updater_receive_beta"`
	UpdaterAutoUpdate  bool `json:"updater_auto_update"`
	// OrgName is the studio display name (org.name; T-d693). "" = never set —
	// the topbar falls back to the localized default string.
	OrgName string `json:"org_name"`
	// OwnerName is the owner's display nickname (owner.name; T-0b41). "" = never
	// set — the topbar's profile pill falls back to the localized default label.
	OwnerName string `json:"owner_name"`
	// PushContactEmail is the address handed to the push gateways as the VAPID
	// subject (push.contact_email; T-8a82). "" = never set, and Web Push is then
	// not delivered at all.
	PushContactEmail string `json:"push_contact_email"`
	// DisplayTheme is the owner's cockpit visual theme (display.theme;
	// T-0b41-p2). "" = never set — the frontend keeps its localStorage cache /
	// default. The frontend reconciles this server value in at login.
	DisplayTheme string `json:"display_theme"`
	// DisplayLanguage is the owner's cockpit language (display.language;
	// T-0b41-p2). "" = never set — the frontend keeps its localStorage cache /
	// default. Same dual-layer contract as display_theme.
	DisplayLanguage string `json:"display_language"`
	// DisplayWide is the owner's cockpit layout width (display.wide; T-756f).
	// false (the default) = the centred ~1040px content column; true lifts that
	// cap and lets the chrome span the window (side gutters stay). Unlike the
	// two prefs above this is a plain bool with no "never set" state — false IS
	// the shipped narrow look, so an untouched install reads exactly right.
	DisplayWide bool `json:"display_wide"`
	// Onboarding (T-ba62) is the first-run onboarding report, or nil when
	// onboarding never ran on this database. It rides the OWNER-GATED settings
	// read on purpose: a failed step's Detail carries the raw `ocwarden install`
	// log (local paths), so it must never reach the PUBLIC /api/auth/status probe.
	Onboarding *onboardingReportDTO `json:"onboarding"`
}

// onboardingStepDTO / onboardingReportDTO are the wire shape of the automatic
// first-run onboarding result (T-ba62). Reason is ALWAYS populated on a failure:
// the whole point of the report is that a new owner can read WHY the assistant
// is not awake instead of staring at an unexplained grey member.
type onboardingStepDTO struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Code is the CLOSED failure vocabulary (T-0648) — see onboarding.go's
	// onboardingCode* constants. Set on every failing step, empty on success.
	// It is what lets the cockpit write the owner's sentence itself; Reason
	// stays as the English fallback for a code the client does not know.
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

type onboardingReportDTO struct {
	State      string              `json:"state"` // running | ok | failed
	StartedAt  float64             `json:"started_at"`
	FinishedAt float64             `json:"finished_at"`
	Steps      []onboardingStepDTO `json:"steps"`
	// DismissedAt is when the owner pressed 「不再顯示」 on the cockpit banner
	// (T-0648): unix seconds, 0 = never dismissed. It lives on the REPORT, not
	// in the browser, which is the whole point — a per-tab dismissal came back
	// on the next tab. Absent in the JSON means 0 means never dismissed: every
	// report row written before this field existed reads that way, and there is
	// no migration, so the honest reading of the absence is the one that keeps
	// the warning visible.
	DismissedAt float64 `json:"dismissed_at"`
}

// themeFetchResultDTO carries a link-fetched theme bundle back to the cockpit
// as the RAW response text (T-29c7). Verbatim on purpose — the cockpit feeds it
// into the same parseImportedBundle a pasted bundle goes through, so there is
// exactly one place a theme is parsed, not two that can drift apart.
type themeFetchResultDTO struct {
	Content string `json:"content"`
}

type tokenDTO struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
	OwnerID   string `json:"owner_id"`
}

type memberDTO struct {
	ID               string  `json:"id"`
	AvatarURL        string  `json:"avatar_url"`
	Name             string  `json:"name"`
	Kind             string  `json:"kind"`
	RoleKey          string  `json:"role_key"`
	RoleName         string  `json:"role_name"`
	Runtime          string  `json:"runtime"`
	Model            string  `json:"model"`
	ActualModel      string  `json:"actual_model"`
	ActualRuntime    string  `json:"actual_runtime"`
	ActualEffort     string  `json:"actual_effort"`
	ActualMachine    string  `json:"actual_machine"`
	Effort           string  `json:"effort"`
	DesiredState     string  `json:"desired_state"`
	DesiredMachineID string  `json:"desired_machine_id"`
	Machine          string  `json:"machine"`
	Presence         string  `json:"presence"`
	RefocusSince     float64 `json:"refocus_since"`
	RefocusOp        string  `json:"refocus_op"`
	RefocusDeadline  float64 `json:"refocus_deadline"`
	LastOp           string  `json:"last_op"`
	LastOpOK         *bool   `json:"last_op_ok"`
	LastOpLog        string  `json:"last_op_log"`
	LastOpReason     string  `json:"last_op_reason"`
	LastOpAt         float64 `json:"last_op_at"`
	// ForcedStopAt: unix seconds of the last force-stop, 0 when there has never
	// been one. Deliberately NOT cleared by the next boot — it is the record
	// that the PREVIOUS session was cut off mid-work (T-a9d6).
	ForcedStopAt      float64 `json:"forced_stop_at"`
	UnreadCount       int     `json:"unread_count"`
	RosterStatus      string  `json:"roster_status"`
	OwnerID           string  `json:"owner_id"`
	SchemaVersion     int     `json:"schema_version"`
	RelocationPending *bool   `json:"relocation_pending,omitempty"` // T-8655: set only on the relocate response when the recycle STOP/START could not be delivered (move scheduled, not yet landed); nil everywhere else
	// ActivationPending (T-ba62) is the activate twin of RelocationPending: set
	// only on the activate response when the decided START could not be handed
	// to the target warden (no live SSE downstream). Without it an activate into
	// an unreachable warden returns a clean 200 that is byte-indistinguishable
	// from a wake that actually started — the caller has no way to tell "waking"
	// from "nothing happened and nothing will until the cadence retries".
	ActivationPending *bool `json:"activation_pending,omitempty"`
	// RelocationDeferred (T-927a) disambiguates RelocationPending, which is true
	// for TWO different situations: a move the warden could not accept, and a
	// move deliberately held back because a graceful wind-down window was opened
	// for a live member. Only the first is a failure. Without this field the
	// cockpit cannot tell them apart, so it raised its "nothing was dispatched"
	// alert over the perfectly normal wind-down case. Set only on the relocate
	// response, and only when that window was opened; nil everywhere else.
	RelocationDeferred *bool `json:"relocation_deferred,omitempty"`
}

type machineDTO struct {
	MachineID   string `json:"machine_id"`
	DisplayName string `json:"display_name"`
	Online      bool   `json:"online"`
	IsSelf      bool   `json:"is_self"`
	// BinStatus is the server-computed binary-freshness verdict ("current" |
	// "stale"); nil when unknowable (no heartbeat fingerprints yet — an older
	// warden build — or no embedded bindist to compare against). Comparison
	// result only, never a per-machine version stamp (see binStatusFor).
	BinStatus *string `json:"bin_status"`
	// ClaudeVersion / ClaudeCredSource / ClaudeSubReadable are the machine's
	// local claude CLI probe (T-97ee), derived from the warden heartbeat's
	// `claude` telemetry (machineClaudeInfo). All nil = honest unknown (an
	// older warden that never probed) — the same backward-compat semantics as
	// BinStatus. CredSource is server-synthesized from the presence bools:
	// "file" | "keychain" | "both" | "none".
	ClaudeVersion       *string                         `json:"claude_version"`
	ClaudeCredSource    *string                         `json:"claude_cred_source"`
	ClaudeSubReadable   *bool                           `json:"claude_sub_readable"`
	RuntimeCapabilities map[string]RuntimeCapabilityDTO `json:"runtime_capabilities"`
	// WardenShape is which launchd shape this machine's warden REPORTED it is
	// running under ("anchor" | "legacy" | "unknown"), passed through verbatim.
	//
	// Unlike BinStatus next door, this is NOT computed here and must never be:
	// only the reporting process can read its own parent, so the server has no
	// second source to derive or cross-check it from. nil means the machine has
	// never reported one — a warden build older than the anchor-cutover release —
	// which is a DIFFERENT fact from the reported "unknown" (that build ran and
	// could not tell). Neither is ever turned into the other.
	WardenShape *string `json:"warden_shape"`
	// CutoverEffect is whether that cutover is actually IN EFFECT for the
	// processes that CARRY agents ("effective" | "not_effective" | "unproven"),
	// reported verbatim. WardenShape above cannot answer this: it observes
	// warden's own parent, while the agents hang off a tmux server that keeps its
	// original identity across a warden restart — so a converted machine can and
	// did show "anchor" for hours while every agent still ran under the old one.
	//
	// Three-valued on purpose. "unproven" is a reported verdict, never a shade of
	// "effective"; collapsing it into green is the exact defect being retired.
	// nil = this warden build does not report the verdict at all.
	CutoverEffect *string `json:"cutover_effect"`
	// TokenKeyID / TokenKeyCurrent are T-80's answer to the one question that
	// stands between the owner and pressing 「移除」 on a retired signing key —
	// an act with no grace period at all: has this machine come back on the
	// current key yet?
	//
	// 🔴 BOTH ARE OBSERVATIONS THIS STATION MADE, NOT ANYTHING A MACHINE SAID.
	// TokenKeyID is the id of the key whose HMAC actually verified this
	// machine's credential at the auth gate (member.token_key_id). There is no
	// claim, no header and no telemetry block a warden could use to assert it,
	// and adding one would defeat the point: the value gates a destructive,
	// immediate action, so it must not be assertable by the very machines being
	// counted. nil = this station has never verified a credential of that
	// machine's — for a machine that has not authenticated since the rotation
	// the value simply stays where it was, which is the honest reading.
	//
	// TokenKeyCurrent is that id compared against the LIVE ring's signing key,
	// computed here for hardware_stale's reason: a client doing the comparison
	// would need the active key id on the wire too, and that is a second home
	// for the same fact that can disagree with this one. nil exactly when
	// TokenKeyID is nil.
	TokenKeyID      *string `json:"token_key_id"`
	TokenKeyCurrent *bool   `json:"token_key_current"`
}

type machineOnboardResultDTO struct {
	MemberID       string `json:"member_id"`
	MachineID      string `json:"machine_id"`
	Token          string `json:"token"`
	ExpiresIn      int64  `json:"expires_in"`
	BootCommand    string `json:"boot_command"`
	ClaimCode      string `json:"claim_code"`
	ClaimExpiresIn int64  `json:"claim_expires_in"`
}

type bootCommandResultDTO struct {
	MachineID      string `json:"machine_id"`
	BootCommand    string `json:"boot_command"`
	Token          string `json:"token"`
	ExpiresIn      int64  `json:"expires_in"`
	ClaimCode      string `json:"claim_code"`
	ClaimExpiresIn int64  `json:"claim_expires_in"`
}

// machineClaimResultDTO answers POST /api/machines/claim: the one-time claim
// code redeemed for the machine's freshly minted exec-token.
type machineClaimResultDTO struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	MachineID string `json:"machine_id"`
}

type bootstrapResultDTO struct {
	MachineID string `json:"machine_id"`
	OK        bool   `json:"ok"`
	ExitCode  int    `json:"exit_code"`
	Log       string `json:"log"`
}

type machineTeardownHereResultDTO struct {
	MachineID string `json:"machine_id"`
	OK        bool   `json:"ok"`
	ExitCode  int    `json:"exit_code"`
	Log       string `json:"log"`
	Removed   bool   `json:"removed"`
}

type machineUninstallResultDTO struct {
	MemberID   string `json:"member_id"`
	MachineID  string `json:"machine_id"`
	Dispatched bool   `json:"dispatched"`
}

type machineDeleteResultDTO struct {
	MemberID  string `json:"member_id"`
	MachineID string `json:"machine_id"`
	Removed   bool   `json:"removed"`
}

// machineUpgradeResultDTO answers POST /api/machines/{member_id}/upgrade:
// whether the `update` warden-command was actually enqueued onto the
// machine's live SSE downstream (false = warden offline, nothing commanded).
type machineUpgradeResultDTO struct {
	MemberID   string `json:"member_id"`
	MachineID  string `json:"machine_id"`
	Dispatched bool   `json:"dispatched"`
}

type chatAttachmentDTO struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	IsImage  bool   `json:"is_image"`
}

// chatAttachmentUploadDTO answers POST /api/chat/attachments: the stored
// blob's light ref, exactly the {id, mime, filename} shape post_chat accepts
// back as a reference (filename "" for an unnamed non-image blob).
type chatAttachmentUploadDTO struct {
	ID       string `json:"id"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
}

// chatInlineReplyCardDTO is one reply card FOLDED IN PLACE onto the chat
// message that opened it (chatMessageDTO.Card). It exists so the wake snapshot
// reads as ONE stream: the card already has a home in the chat (its
// ChatMessageID), so a second top-level `cards` section would carry the same
// decision twice in one payload.
//
// It carries the DECISION only — options offered, which ones were circled, the
// free text, and when. Summary / body / kind / attachments are deliberately
// absent: the message this rides on already carries the ask, and
// get_reply_card serves the rest.
type chatInlineReplyCardDTO struct {
	Options []ReplyCardOption `json:"options"`
	// AnswerOptionIdxs is the circled options' indices (deduped, ascending);
	// null when no option was circled. This is one of the AI's two read paths
	// for an answer — the other is the ocagent line — so a card answered with
	// two options must show both here.
	AnswerOptionIdxs []int   `json:"answer_option_idxs"`
	AnswerText       string  `json:"answer_text"`
	AnsweredTS       float64 `json:"answered_ts"`
	// AnsweredAtDisplay is AnsweredTS in the same full date+time+offset form as
	// chatMessageDTO.TSDisplay; "" while the card is unanswered.
	AnsweredAtDisplay string `json:"answered_at_display"`
}

type chatMessageDTO struct {
	ID   string `json:"id"`
	From string `json:"from"`
	// FromName / ToName are the DISPLAY names beside the ids, never instead of
	// them: From/To stay the ADDRESS a reply must be sent to, and a name is
	// editable and repeats across the roster. Both are carried so a reader gets
	// the human name AND the id in one row. "" when the id does not resolve to
	// a roster row — honest empty, never fabricated.
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`
	Body     string `json:"body"`
	// BodyOmittedChars is the COLLAPSE marker: how many runes of THIS body were
	// folded away, 0 when the body is whole. The folded text is still on the
	// server — get_chat re-reads the message.
	//
	// 🔴 This is NOT resumeSummaryDTO.ChatEarlierOmitted, and the two must never
	// borrow each other's wording. This one = one message that IS in the payload
	// with part of its text shortened. That one = whole messages ABSENT from the
	// payload. Before this split both showed up as a bare "…" and a reader had
	// no way to tell "I have this message, shortened" from "I do not have this
	// message" — which is exactly how an agent concludes it has read a
	// conversation it has not read.
	BodyOmittedChars int     `json:"body_omitted_chars"`
	TS               float64 `json:"ts"`
	// TSDisplay renders TS for a READER as "2006-01-02 15:04:05 +08:00" in the
	// SERVER's local zone. The offset is IN the string because the studio has no
	// timezone setting to read it from — a bare local time would be unreadable
	// by anyone who is not the server. The DATE IS ALWAYS WRITTEN, same-day
	// included: a waking agent must be able to tell 昨天 from 上週 without first
	// knowing what day the snapshot was taken, and "drop the date when it is
	// today" makes that impossible for exactly the messages a wake cares about.
	// TS (epoch seconds) is untouched and stays the machine-readable field.
	TSDisplay string         `json:"ts_display"`
	Meta      map[string]any `json:"meta"`
	// Card is the reply card this message carries, folded in place; nil (key
	// omitted) when there is none.
	Card *chatInlineReplyCardDTO `json:"card,omitempty"`
	// ReplyCardStatus: read-time join of the card this message carries
	// (meta.reply_card_id) — "waiting" | "answered", or "" when no card. Filled
	// by servedChatMessageDTO; the inline ChatReplyCard reads it to lazy-load
	// answered cards. See ChatMessageDTO in the spec.
	ReplyCardStatus string              `json:"reply_card_status"`
	Attachments     []chatAttachmentDTO `json:"attachments"`
	// ReplyTo is the id of the message this one is REPLYING TO, "" when it
	// replies to nothing. Stamped once at post time from ChatPostDTO.reply_to
	// and never rewritten. It is the ONE fact about whether this message is a
	// reply, and it never goes away — not even when the message it names does.
	//
	// It may point OUT of this conversation (owner ruling, 2026-08-21): the
	// owner replies to a line two other members exchanged in order to step in
	// and ask about it. The post-time same-conversation refusal that used to
	// live here is gone.
	ReplyTo string `json:"reply_to"`
	// ReplyToChat is the QUOTED MESSAGE, snapshotted at read time — nil (key
	// omitted) when this message replies to nothing, and nil ALSO when the
	// message it names is no longer there.
	//
	// 🔴 BUILT UNCONDITIONALLY ON EVERY READ, and the absence of any condition
	// is the design (T-4e95, owner ruling 2026-08-21, replacing the id-only
	// wire). The previous shape shipped the id alone and left the reader to
	// fetch what it named when it was not already on screen — which meant the
	// reader had a fetch that could fail, a failure it had to render, and a
	// story about healing that failure later. Every one of those was a branch,
	// and a branch here looks IDENTICAL on screen whether it is right or wrong.
	// There is deliberately no "the target is already in this batch" skip and
	// no "only if asked" flag: the cheapest thing to get right is the thing
	// that has one behaviour.
	//
	// nil-with-ReplyTo-set is a settled, permanent answer ("the original is
	// gone"), NOT a miss to retry. Nothing on either side of this wire retries.
	ReplyToChat *chatReplyQuoteDTO `json:"reply_to_chat,omitempty"`
}

// chatReplyQuoteDTO is the quoted message reduced to what a quote line draws:
// who said it, WHO THEY SAID IT TO, and a short piece of what they said
// (ChatMessageDTO.reply_to_chat).
//
// From / FromName and To / ToName are chatMessageDTO's OWN convention, copied
// rather than reinvented: the bare id is the ADDRESS and always carried, the
// Name beside it is the display name carried IN ADDITION to it and left "" on
// the reads that do not resolve names at all — exactly as chatMessageDTO's own
// pairs behave, and for the same reason (a name that is really an id is worse
// than no name). chatGalleryEntryDTO carries the same pair for the same reason.
//
// 🔴 THE ADDRESSEE IS HERE BECAUSE A QUOTE CAN COME FROM ANOTHER CONVERSATION
// (owner ruling 2026-08-21, the same ruling that removed the same-conversation
// gate on reply_to). With From alone the quote line reads as if the quoted
// sentence had been said in the thread it is drawn in — which is precisely
// wrong for the case the ruling exists for, the owner stepping into two other
// members' thread. To is the QUOTED message's own recipient; it is NOT the peer
// of the thread carrying the reply, and the two differ exactly when it matters.
type chatReplyQuoteDTO struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`
	// Content is the quoted body as ONE line, shortened HERE — see
	// chatReplyQuoteContent. "" is ordinary and legal: an attachment-only
	// message has no text to quote.
	Content string `json:"content"`
}

// chatReplyQuoteMaxChars is how much of a quoted message a quote line carries,
// in runes. It is the length the PRODUCT ships: the browser no longer cuts
// anything of its own (ChatArea's QUOTE_EXCERPT_CHARS is deleted) and the one
// line the reader sees is whatever this produces.
//
// 🔴 THIS NUMBER HAS A SECOND, BEHAVIOUR-DEFINING COPY, AND IT IS NOT DEAD.
// An earlier version of this comment said "THE ONLY DEFINITION OF THAT LENGTH
// ANYWHERE", which was false on the day it was written: frontend/src/api/mock.ts
// holds MOCK_REPLY_QUOTE_MAX_CHARS for the offline preview, which has no server
// to ask. Whoever changes this constant MUST change that one too, or offline
// preview silently cuts at a different point from the live product — the exact
// thing the mock exists to prevent.
//
// That is not left to this comment. frontend/src/api/mock.reply-to.test.ts reads
// THIS LINE out of this file and fails if the two numbers differ, the way
// errorCodes.test.ts pins the frontend to the shared error-code table. Keep the
// `chatReplyQuoteMaxChars = <n>` spelling on one line; that guard matches it.
//
// 60 is the number the deleted browser copy already used, kept so the rendered
// quote line does not change size under the owner as this ships.
const chatReplyQuoteMaxChars = 60

// chatReplyQuoteContent renders a quoted body as ONE quote line: runs of
// whitespace (newlines included) collapse to single spaces, and the result is
// cut to chatReplyQuoteMaxChars runes with an ellipsis standing in for what was
// taken. A quote is a POINTER to a message, not a rendering of it — a
// multi-line excerpt would push the reader's layout around for no gain.
//
// Cut by RUNES, never by bytes: a byte cut lands mid-codepoint on the very
// content this studio is mostly written in.
func chatReplyQuoteContent(body string) string {
	oneLine := strings.Join(strings.Fields(body), " ")
	r := []rune(oneLine)
	if len(r) <= chatReplyQuoteMaxChars {
		return oneLine
	}
	return string(r[:chatReplyQuoteMaxChars]) + "…"
}

// newChatReplyQuoteDTO projects the quoted MESSAGE into the quote line's view.
//
// names is nil on every read that does not resolve display names (the ordinary
// listing, the by-ids read, the POST echo) and FromName/ToName are then "" —
// the SAME answer chatMessageDTO's own name fields give on those reads,
// deliberately, so the quote and the message it hangs under never disagree
// about whether this payload carries names. Only the wake snapshot passes a
// map, and there the owner's special case rides along with it.
//
// Both ADDRESSES are unconditional, exactly as on chatMessageDTO: the two names
// are the optional half, never the ids.
func newChatReplyQuoteDTO(m ChatMessage, names map[string]string) *chatReplyQuoteDTO {
	fromName, toName := "", ""
	if names != nil {
		fromName = resumeDisplayName(m.Sender, names)
		toName = resumeDisplayName(m.Recipient, names)
	}
	return &chatReplyQuoteDTO{
		ID:       m.ID,
		From:     m.Sender,
		FromName: fromName,
		To:       m.Recipient,
		ToName:   toName,
		Content:  chatReplyQuoteContent(m.Body),
	}
}

type chatGalleryEntryDTO struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Filename  string  `json:"filename"`
	Mime      string  `json:"mime"`
	IsImage   bool    `json:"is_image"`
	MessageID string  `json:"message_id"`
	From      string  `json:"from"`
	FromName  string  `json:"from_name"`
	To        string  `json:"to"`
	TS        float64 `json:"ts"`
}

type chatReadDTO struct {
	ReaderID   string  `json:"reader_id"`
	PeerID     string  `json:"peer_id"`
	LastReadTS float64 `json:"last_read_ts"`
}

type agentContextDTO struct {
	AgentID         string         `json:"agent_id"`
	CompactionCount *int           `json:"compaction_count,omitempty"`
	ContextPct      float64        `json:"context_pct"`
	RateLimits      map[string]any `json:"rate_limits"`
	TS              float64        `json:"ts"`
}

type agentTelemetryDTO struct {
	AgentID       string         `json:"agent_id"`
	Machine       *string        `json:"machine"`
	Account       *string        `json:"account"`
	RateLimits    map[string]any `json:"rate_limits"`
	Tokens        map[string]any `json:"tokens"`
	Hardware      map[string]any `json:"hardware"`
	Binaries      map[string]any `json:"binaries"`
	Claude        map[string]any `json:"claude"`
	Runtime       *string        `json:"runtime"`
	Runtimes      map[string]any `json:"runtimes"`
	Cost          *float64       `json:"cost"`
	Effort        *string        `json:"effort"`
	SelfUpdate    map[string]any `json:"self_update"`
	CommandResult map[string]any `json:"command_result"`
	// WardenShape echoes the stored launchd-shape verdict so the POST response
	// round-trips what was just merged; nil when this reporter has never sent one.
	WardenShape *string `json:"warden_shape"`
	// CutoverEffect echoes the stored cutover-effect verdict, same round-trip
	// contract as WardenShape above.
	CutoverEffect *string `json:"cutover_effect"`
	TS            float64 `json:"ts"`
}

type monitoringSessionDTO struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Role            string         `json:"role"`
	Runtime         string         `json:"runtime"`
	Model           string         `json:"model"`
	Effort          string         `json:"effort"`
	Machine         string         `json:"machine"`
	Account         string         `json:"account"`
	Presence        string         `json:"presence"`
	ContextPct      *float64       `json:"context_pct"`
	CompactionCount *int           `json:"compaction_count,omitempty"`
	Cost            *float64       `json:"cost"`
	BankedCost      *float64       `json:"banked_cost"`
	Tokens          map[string]any `json:"tokens"`
}

// costResetDTO is the receipt of POST /api/members/{member_id}/cost/reset:
// WHAT WAS DESTROYED, read immediately before the write.
//
// 🔴 It carries the PRE-reset figures on purpose. Spend lives in exactly two
// accumulators and there is no per-charge ledger behind them, so once they are
// cleared the discarded amount is not recoverable from any other record — this
// response is the last moment it exists. A receipt of the post-reset state
// would say nothing at all about an irreversible operation.
//
// It is a receipt, NOT an undo: nothing is retained and no route puts the
// figure back (owner ruling rc-7dea0deefa63, option 0「最小、不可逆」).
//
// The two fields mirror monitoringSessionDTO field-for-field, null semantics
// included: null means there was nothing to clear on that half, not that zero
// was cleared. A client therefore reuses ONE summing rule rather than growing a
// second one.
type costResetDTO struct {
	MemberID          string   `json:"member_id"`
	ClearedCost       *float64 `json:"cleared_cost"`
	ClearedBankedCost *float64 `json:"cleared_banked_cost"`
}

// accountCostResetDTO is the receipt of POST /api/accounts/cost/reset: the
// ACCOUNT's own accumulated spend as it stood immediately before the write
// (owner ruling rc-5c5d7c7c6dcd 「分開：帳號卡自己一份數字，清它不動成員」).
//
// Nothing about any member appears here because nothing about any member
// changed. Null means there was nothing to clear — not that zero was cleared —
// mirroring costResetDTO and the read side so a client keeps one rule.
type accountCostResetDTO struct {
	Account     string   `json:"account"`
	ClearedCost *float64 `json:"cleared_cost"`
}

type monitoringMachineDTO struct {
	Machine     string   `json:"machine"`
	DisplayName string   `json:"display_name"`
	Agents      int      `json:"agents"`
	CpuPct      *float64 `json:"cpu_pct"`
	RamPct      *float64 `json:"ram_pct"`
	BatteryPct  *float64 `json:"battery_pct"`
	ACPower     *bool    `json:"ac_power"`
	Accounts    []string `json:"accounts"`
	// BinStatus mirrors machineDTO.BinStatus (the registry row's verdict) so
	// the monitoring fold carries the same binary-freshness signal.
	BinStatus *string `json:"bin_status"`
	// ClaudeVersion / ClaudeCredSource / ClaudeSubReadable mirror the
	// machineDTO claude probe columns (machineClaudeInfo — T-97ee).
	ClaudeVersion       *string                         `json:"claude_version"`
	ClaudeCredSource    *string                         `json:"claude_cred_source"`
	ClaudeSubReadable   *bool                           `json:"claude_sub_readable"`
	RuntimeCapabilities map[string]RuntimeCapabilityDTO `json:"runtime_capabilities"`
	// HardwareTS is WHEN the served hardware sample was measured (epoch secs),
	// nil when there is no sample or its age is unknown. Until T-b36a nothing on
	// this wire said how old a number was, so a machine that reported once and
	// went dark showed a confident CPU percentage next to an offline badge
	// forever. The stale values are now nulled (see the fold), and this stamp is
	// what keeps "expired" distinguishable from "never measured".
	HardwareTS *float64 `json:"hardware_ts"`
	// HardwareStale is the SERVER's verdict on that stamp: true = the sample is
	// past telemetryFreshSecs, which is WHY cpu/ram/battery/ac are null on this
	// row. nil = no sample was ever taken. The stamp alone is not enough for a
	// client: reading it would mean re-deriving the 90s window against the
	// client's own clock, i.e. a second home for the threshold that can disagree
	// with this one. Same shape and same window as RuntimeCapabilitiesStale, so
	// there is exactly one freshness rule on this wire and both consumers of it
	// ask the same question.
	HardwareStale *bool `json:"hardware_stale"`
	// HardwareInvalid names the DECLARED hardware keys that arrived in the
	// served sample with the WRONG VALUE TYPE (sorted; empty for a clean, a
	// stale, or an absent sample). It is the third answer to "why is that cell
	// blank", and it exists because the first two were being used to cover a
	// case neither of them describes.
	//
	// The nested telemetry blocks are permissive on purpose (owner ruling
	// rc-55861dd893c6): the ingest handler checks hardware is an OBJECT and then
	// stores its contents verbatim, so `cpu_pct: "47"` is a 200 and sits in the
	// store as a string. teleNum wants a float64, does not get one, and returns
	// nil — and null-because-unreadable was byte-for-byte the same row as
	// null-because-never-probed (measured: a string cpu_pct produced a row
	// identical to one that omitted the key entirely, hardware_ts and
	// hardware_stale included). So a warden whose CPU probe started returning a
	// string looked exactly like a machine that has no battery: nothing on the
	// wire, and nothing on screen, said a measurement had been lost.
	//
	// Deliberately NOT a rejection. Refusing the report at ingest is the same
	// fail-closed move the owner already ruled against for this block, and its
	// blast radius is the whole heartbeat (hardware + binaries + claude +
	// runtimes going null together, the a7fa594 outage). The data still lands
	// exactly as before; only the SILENCE is removed.
	//
	// Per KEY, not per row: one broken probe must not cast doubt on its
	// siblings, so a row can serve a good ram_pct while naming cpu_pct here.
	// Key NAMES only, never the offending value — that value is untrusted input
	// and has no business being rendered in the cockpit.
	//
	// THIS DTO IS THE ONE THE COCKPIT ACTUALLY READS, which is why the field
	// lives here and only here. Traced, not assumed: MonitorPage.tsx's machine
	// table is a JOIN — it iterates the REGISTRY rows (MachineDTO, for identity /
	// online / actions) but every hardware cell reads `hwByHost.get(machineId)`,
	// i.e. THIS row. MachineDTO is not missing this field by oversight: it has
	// never carried cpu_pct/ram_pct/battery_pct/ac_power at all, so it has no
	// blank hardware cell that could need explaining. (claude_* and bin_status
	// ARE mirrored across both DTOs — because both render them. The asymmetry
	// follows from who renders what, not from anyone forgetting.)
	//
	// ⚠️ COVERAGE, stated so no one reads more protection into this than exists
	// — and no LESS either, because underclaiming sends the next person to build
	// something that can never fire. The three declared blocks are protected by
	// three different mechanisms, and only one of them is this field:
	//
	//	hardware  — THIS field. Nothing is refused at ingest (owner ruling), so
	//	            the read path is the only place a wrong-typed value can be
	//	            surfaced, and it is surfaced per key.
	//	runtimes  — already fail-closed AT INGEST, and has been since before this
	//	            change: the handler type-checks installed / logged_in /
	//	            version per key and answers a flat 400. NOT in the hole. Do
	//	            not add a read-side marker here — a wrongly-typed value never
	//	            reaches the store, so the marker could never fire.
	//	claude    — the one that IS still open and still silent. `claude:
	//	            {"version": 9.9}` is a 200, stored, and read back as null
	//	            exactly as cpu_pct was, with nothing on the wire saying a
	//	            value was lost. Its only guard is a CI test over OUR OWN
	//	            producers (cli/ocwarden/telemetry_wire_test.go), so an older
	//	            or third-party warden drifting there stays invisible at
	//	            runtime. Deliberately out of scope here (owner ruling:
	//	            separate ticket) — not fixed, just known.
	HardwareInvalid []string `json:"hardware_invalid"`
	// RuntimeCapabilitiesTS / RuntimeCapabilitiesStale carry the same freshness
	// question for the capability probes. Their values are deliberately NOT
	// blanked when stale: "codex was not logged in as of 3h ago" is the only
	// surface that explains a worker parked on machine_unavailable, so the fix
	// for "shown as if current" is to mark it, not to delete it.
	RuntimeCapabilitiesTS    *float64 `json:"runtime_capabilities_ts"`
	RuntimeCapabilitiesStale *bool    `json:"runtime_capabilities_stale"`
	// WardenShape mirrors machineDTO.WardenShape (the registry row's reported
	// launchd shape) — both tables render it, the same reason bin_status and the
	// claude_* columns are mirrored. Reported, never computed; nil stays nil.
	WardenShape *string `json:"warden_shape"`
	// CutoverEffect mirrors machineDTO.CutoverEffect for the same reason.
	CutoverEffect *string `json:"cutover_effect"`
}

type monitoringAccountDTO struct {
	Account string `json:"account"`
	// AccountLabel is the reporter-supplied human label "email(org)" (T-260e).
	// OWNER-ONLY: omitted for any non-owner caller — filled from the same
	// acctLabels overlay as the display_name fold, so the privacy gate is one
	// and the same. Independent of DisplayName so an owner alias no longer
	// hides the real identity.
	AccountLabel *string     `json:"account_label,omitempty"`
	DisplayName  string      `json:"display_name"`
	Machine      string      `json:"machine"`
	Cost         *float64    `json:"cost"`
	FiveHour     *PaceWindow `json:"five_hour"`
	SevenDay     *PaceWindow `json:"seven_day"`
}

type monitoringDTO struct {
	Sessions []monitoringSessionDTO `json:"sessions"`
	Machines []monitoringMachineDTO `json:"machines"`
	Accounts []monitoringAccountDTO `json:"accounts"`
}

type aliasDTO struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
}

type globalContextDTO struct {
	Text          string `json:"text"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	// OrgName is the studio display name (org.name; T-d693) — the agent read
	// path for the topbar name the owner sets via PATCH /api/settings. NOT
	// secret; "" = the owner has not named the studio. Read-only here (writes
	// go through the owner-gated settings surface).
	OrgName string `json:"org_name"`
}

// bootDocDTO is ONE editable boot-context block on the wire (T-791e): the
// 系統互動 block, or one runtime's 啟動步驟 block.
//
// The four judgement fields are the pair the capped documents already carry
// (SizeChars/CapChars — the settings surface holding the cap is admin-only, so
// without them being refused is the only way to learn the limit) plus the pair
// the insight doc carries (IsDefault/HasSeed), and they answer DIFFERENT
// questions:
//
//	IsDefault — has anybody edited this block? (false = you are reading an edit)
//	HasSeed   — does a factory version exist to go back TO? (the reset's
//	            precondition; that route 404s when it is false)
//
// For these three documents HasSeed is true in every shipped build, which is
// exactly why it must be SERVED rather than assumed: a build whose seedsdist was
// not staged answers false, and a cockpit that offered 還原 anyway would hand the
// owner a button that 404s at the one moment it matters.
type bootDocDTO struct {
	SizeChars int    `json:"size_chars"`
	CapChars  int    `json:"cap_chars"`
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	// Text is the WHOLE stored document, marker line and all. It is what the
	// version history retains and diffs against, and what SizeChars counts.
	Text string `json:"text"`
	// ReadOnlyHead / Body are the two halves, named on the READ face only
	// (owner's ruling 2026-08-23: 「讀取有這個 key，回寫沒有這個 key」).
	//
	// 🔴 Body IS THE WRITE FACE'S FIELD, BYTE FOR BYTE. BootDocumentReplaceDTO
	// takes exactly this value back, so a caller edits what it was handed and
	// sends it — it never has to know that a marker exists, what separates the
	// halves, or how they are joined. ReadOnlyHead is the half it may look at
	// and can no longer send: "" on a document that carries none.
	//
	// Not omitempty. An empty head is the honest answer for a document with no
	// read-only half, and a reader that could not tell that apart from "this
	// build is too old to say" would have to guess which.
	ReadOnlyHead  string `json:"read_only_head"`
	Body          string `json:"body"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	HasSeed       bool   `json:"has_seed"`
	// ReadOnly is what a cockpit needs BEFORE it renders an editor: this
	// document is shown so the owner can read what an agent is sent, and every
	// write face refuses it. Without it the only way to learn that is to type
	// an edit and be told 405 on save, which is where the effort has already
	// been spent. Not omitempty — false is the answer for every editable
	// document, and a reader that cannot tell "editable" from "this build is
	// too old to know" would offer the editor either way.
	ReadOnly bool `json:"read_only"`
}

type roleDefDTO struct {
	// SizeChars / CapChars are the Duty doc's own budget (T-ae38) — the same
	// pair lessonsDTO and insightDTO have carried since T-3aeb, and for the
	// same reason: the settings surface holding the cap is admin-only, so
	// without them the only way to learn the limit is to be refused by it.
	//
	// Duty had NEITHER field until T-ae38, and the cost was concrete: an agent
	// that had just finished condensing its own role definition could not tell
	// how much room was left and had to ask someone else to measure the doc.
	// There is usually no such someone.
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	Key           string `json:"key"`
	Name          string `json:"name"`
	DefinitionMD  string `json:"definition_md"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	IsSeed        bool   `json:"is_seed"`
}

// roleDefListItemDTO is one row of GET /api/roles: everything roleDefDTO
// carries EXCEPT the persona body. The listing is where a caller CHOOSES a
// role; reading one is get_role.
//
// definition_md is ABSENT from the wire rather than served as "" — an empty
// string in the field that normally holds the persona reads as "this role has
// no definition", which is a different claim and a false one. SizeChars is
// still measured on the STORED document (see newRoleDefListItemDTO), so the
// row keeps answering "which definition is nearly full" without the text.
type roleDefListItemDTO struct {
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	Key           string `json:"key"`
	Name          string `json:"name"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	IsSeed        bool   `json:"is_seed"`
}

// newRoleDefListItemDTO projects a folded role onto the listing row. It takes
// the already-folded roleDefDTO precisely so the two faces cannot disagree
// about is_default / is_seed / the size — the same fold answers get_role.
func newRoleDefListItemDTO(d roleDefDTO) roleDefListItemDTO {
	return roleDefListItemDTO{
		SizeChars:     d.SizeChars,
		CapChars:      d.CapChars,
		Key:           d.Key,
		Name:          d.Name,
		OwnerID:       d.OwnerID,
		SchemaVersion: d.SchemaVersion,
		IsDefault:     d.IsDefault,
		IsSeed:        d.IsSeed,
	}
}

type roleCreateResultDTO struct {
	Role   roleDefDTO `json:"role"`
	Member memberDTO  `json:"member"`
}

// docSizeDTO is ONE capped document reduced to its two numbers (peek_doc_sizes).
// CapChars is the cap of THAT document's OWN segment — the five capped segments
// carry five separate doc.cap_chars.* settings, so this pair is repeated per
// document rather than hoisted to one field on the envelope. Hoisting it would
// be the exact bug the split was made to remove: one number standing in for five
// that are already allowed to differ.
type docSizeDTO struct {
	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`
}

// roleDocSizesDTO is one role's three capped documents, sizes only. Measured on
// the FOLDED doc (overlay ⊕ seed) — the same text the per-document GETs report,
// because the sizes come from the very same fold* helpers those handlers use.
// Lessons is one row per role, whole: T-2 removed the task_type axis, so there
// is no longer a second BUCKET a write could spend the same cap under while
// staying off this wire.
//
// 🔴 THAT IS NOT THE SAME AS "everything capped is on this wire", and the
// distinction is worth a line because an earlier draft of peek_doc_sizes' tool
// description collapsed the two into a promise a single call falsifies. This
// DTO is keyed by ROLE: the handler walks listRoleKeys(). The lessons write
// face never compares role_key against that roster (see replace_lessons, whose
// own description says so), so an admin or the owner can create a lessons
// document under a name no role carries — it spends the same cap and has no
// role to hang off, so it never appears here. Measured in
// TestPeekDocSizesDescriptionDoesNotPromiseCoverageItCannotGive.
type roleDocSizesDTO struct {
	RoleKey string     `json:"role_key"`
	Duty    docSizeDTO `json:"duty"`
	Insight docSizeDTO `json:"insight"`
	Lessons docSizeDTO `json:"lessons"`
}

// taskManualDocSizesDTO is one task manual's two capped documents, sizes only.
type taskManualDocSizesDTO struct {
	TypeKey   string     `json:"type_key"`
	Sop       docSizeDTO `json:"sop"`
	Learnings docSizeDTO `json:"learnings"`
}

// docSizesDTO is the station-wide capped-document size overview
// (peek_doc_sizes). It carries no document text at all, so its size is a
// function of how many roles and manuals exist and never of what they hold.
type docSizesDTO struct {
	Roles       []roleDocSizesDTO       `json:"roles"`
	TaskManuals []taskManualDocSizesDTO `json:"task_manuals"`
}

type roleDeleteResultDTO struct {
	Role                   string   `json:"role"`
	RemovedMemberIDs       []string `json:"removed_member_ids"`
	DeletedChatMessages    int      `json:"deleted_chat_messages"`
	DeletedChatAttachments int      `json:"deleted_chat_attachments"`
	DeletedChatReads       int      `json:"deleted_chat_reads"`
	DeletedLessons         int      `json:"deleted_lessons"`
}

type lessonsDTO struct {
	// SizeChars / CapChars let a caller size its NEXT edit before making it
	// (T-3aeb). Without them the only way to learn the limit is to be refused,
	// and the settings surface that holds it is admin-only — a worker cannot
	// look it up.
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	RoleKey       string `json:"role_key"`
	Text          string `json:"text"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
}

// lessonsPatchResultDTO is the patch_lessons receipt (T-8327): size
// (CHARACTERS — runes) + sha256 (hex) are verification anchors over the
// RESULTING doc text so the caller can confirm the write without re-reading the
// full doc.
//
// size counted BYTES until T-3aeb (owner 2026-07-31). It now speaks the same
// unit as the doc.cap_chars.learning cap the write was just judged against, so a caller
// can compare the two directly — which is the whole point of a receipt on a
// capped write. Two units for one subject was the defect, not the field.
// insightDTO is the per-role INSIGHT doc on the wire (T-3809) — the third block
// of the role journal, beside Duty (role_def) and Learning (lessons).
//
// Deliberately NOT lessonsDTO minus a field: the seed — added by T-e1e3 — is PER-ROLE
// (`seeds/insight_<roleKey>.md`), never lessons' one-shared-file.
//
// IsDefault means "this role has never written its own insight". 🔴 It no
// longer implies Text=="": a role WITH a seed reads the factory wording with
// IsDefault=true, and a role WITHOUT one reads "" with IsDefault=true. Anything
// that treated the two as the same statement (T-3809 did, and said so here) is
// now wrong — the cockpit must read this field, or it renders factory wording
// as if a person had written it.
type insightDTO struct {
	// SizeChars / CapChars let a caller size its NEXT edit before making it,
	// for the same reason lessonsDTO carries them: the settings surface that
	// holds the cap is admin-only, so being refused would otherwise be the
	// only way to learn the limit.
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	RoleKey       string `json:"role_key"`
	Text          string `json:"text"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	// HasSeed answers ONE question: does a factory version of THIS role's
	// insight exist to fall back to (T-6501)? It is the precondition for
	// reset_insight — that route 404s when it is false — so any surface
	// offering the reset has to gate on this and nothing else.
	//
	// 🔴 IT IS NOT IsDefault AND IT IS NOT RoleDefDTO.IsSeed, and both
	// confusions are load-bearing rather than pedantic:
	//   * IsDefault asks what has been WRITTEN; HasSeed asks what exists to
	//     fall back TO. They are independent — a seeded role that has since
	//     written its own reads HasSeed=true, IsDefault=false, which is exactly
	//     the state in which the reset is most worth offering.
	//   * RoleDefDTO.IsSeed is a fact about the role's DUTY, and it is derived
	//     from a DIFFERENT construction: seedRoleDefinitionMD gates on the
	//     factory ROLE ROSTER (seedRoleName), while seedInsightMD asks only
	//     whether the FILE exists — the insight roster is the set of files, on
	//     purpose and decoupled from the role roster (see assets.go). So a role
	//     can carry a Duty seed and no Insight seed; borrowing IsSeed here
	//     would be wrong by construction, not merely imprecise.
	// On 2026-08-04 IsSeed was read as "you are currently reading the factory
	// version" twice in a row. This field exists partly so that nobody has to
	// re-derive the distinction from the two that already misled people.
	HasSeed bool `json:"has_seed"`
}

// insightPatchResultDTO is the patch_insight receipt — the insight twin of
// lessonsPatchResultDTO. SizeChars is CHARACTERS (runes), the
// cap's unit, per the owner's 2026-07-31 ruling that a size field must carry
// its unit in its name.
type insightPatchResultDTO struct {
	RoleKey       string `json:"role_key"`
	AppliedEdits  int    `json:"applied_edits"`
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	Sha256        string `json:"sha256"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
}

type lessonsPatchResultDTO struct {
	RoleKey       string `json:"role_key"`
	AppliedEdits  int    `json:"applied_edits"`
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	Sha256        string `json:"sha256"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
}

// taskLearningsPatchResultDTO is the patch_task_learnings receipt (T-9ffd): the
// task-manual learnings twin of lessonsPatchResultDTO. size (CHARACTERS —
// runes, the cap's unit since T-3aeb) + sha256 (hex) are verification anchors
// over the RESULTING learnings text so the caller can confirm the write
// without re-reading the full doc;
// applied_edits is the count of edits that changed the text THEY were handed (a
// no-op does not count) — NOT a report on whether the document ended up
// different from where it started, which a batch that undoes itself does not.
// Compare sha256 to answer that. No owner_id/is_default — a manual's learnings
// is not a per-owner overlay the way a role's lessons doc is.
type taskLearningsPatchResultDTO struct {
	TypeKey      string `json:"type_key"`
	AppliedEdits int    `json:"applied_edits"`
	SizeChars    int    `json:"size_chars"`
	CapChars     int    `json:"cap_chars"`
	Sha256       string `json:"sha256"`
}

// taskSopPatchResultDTO is the patch_task_sop receipt (T-1667): the sop_md twin
// of taskLearningsPatchResultDTO, field-for-field identical because it reports
// the same three things about a different document — how many edits landed, and
// the size/sha256 of the result so the caller can confirm the write without
// re-reading the doc. cap_chars is the sop_md cap, not the learnings one.
type taskSopPatchResultDTO struct {
	TypeKey      string `json:"type_key"`
	AppliedEdits int    `json:"applied_edits"`
	SizeChars    int    `json:"size_chars"`
	CapChars     int    `json:"cap_chars"`
	Sha256       string `json:"sha256"`
}

// taskStepNotePatchResultDTO is the patch_step_note receipt (T-1667). It is the
// UNION of the two receipt shapes it sits between, and deliberately so: the
// patch family's applied_edits/size_chars/cap_chars/sha256, plus the note as
// STORED, which taskStepNoteReceiptDTO echoes for a reason that does not stop
// applying here — a step note is bounded and the whole point of the field is
// that a later session reads it back, so the write stays verifiable at the
// write. The larger documents' patch receipts omit their text because echoing
// 30k chars would defeat the purpose of patching; a step note cannot get there.
type taskStepNotePatchResultDTO struct {
	TaskID       string `json:"task_id"`
	StepID       string `json:"step_id"`
	StepStatus   string `json:"step_status"`
	Note         string `json:"note"`
	AppliedEdits int    `json:"applied_edits"`
	SizeChars    int    `json:"size_chars"`
	CapChars     int    `json:"cap_chars"`
	Sha256       string `json:"sha256"`
}

type replyCardAnswerDTO struct {
	// OptionIdxs: the circled options' indices, deduped + ascending; null when
	// the answer was free text / attachments only.
	OptionIdxs  []int               `json:"option_idxs"`
	Text        string              `json:"text"`
	Attachments []chatAttachmentDTO `json:"attachments"`
}

type replyCardDTO struct {
	ID      string            `json:"id"`
	From    string            `json:"from"`
	Kind    string            `json:"kind"`
	Summary string            `json:"summary"`
	Body    string            `json:"body"`
	Options []ReplyCardOption `json:"options"`
	// SelectMode: "single" | "multi" — how many of the options the owner may
	// circle. A separate axis from Kind (which says what the owner must DO).
	SelectMode string  `json:"select_mode"`
	Status     string  `json:"status"`
	CreatedTS  float64 `json:"created_ts"`
	// Attachments are the QUESTION-side attachments the initiator opened the
	// card with (T-5e8a) — served refs incl. download url, always an array
	// ([] when none), the same projection the answer side rides.
	Attachments   []chatAttachmentDTO `json:"attachments"`
	AnsweredTS    *float64            `json:"answered_ts"` // null unless answered
	ExpiredTS     *float64            `json:"expired_ts"`  // null unless expired
	ChatMessageID string              `json:"chat_message_id"`
	Answer        *replyCardAnswerDTO `json:"answer"` // null unless answered
	Task          *taskRefDTO         `json:"task"`   // null = plain chat 請示 (no task)
}

// replyCardListItemDTO is one LIGHT row of GET /api/reply-cards (T-3f31 owner
// ruling: 卡只需要 title+決策) — the summary (title) plus, on an answered row,
// the decision digest; NEVER the body or the full options text. The full card
// (body, options, untruncated answer, attachment refs, chat anchor) is one
// get_reply_card away.
type replyCardListItemDTO struct {
	ID         string                   `json:"id"`
	From       string                   `json:"from"`
	Kind       string                   `json:"kind"`
	Summary    string                   `json:"summary"`
	Status     string                   `json:"status"`
	CreatedTS  float64                  `json:"created_ts"`
	AnsweredTS *float64                 `json:"answered_ts"` // null unless answered
	ExpiredTS  *float64                 `json:"expired_ts"`  // null unless expired
	Answer     *replyCardAnswerBriefDTO `json:"answer"`      // null unless answered
	Task       *taskRefDTO              `json:"task"`        // null = plain chat 請示
}

// replyCardAnswerBriefDTO is the decision digest on a light answered list row:
// EVERY circled option's index + ORIGINAL wording, the answer text truncated to
// a preview, and the attachment COUNT (refs ride get_reply_card only).
type replyCardAnswerBriefDTO struct {
	OptionIdxs []int `json:"option_idxs"` // null = free text only
	// Options: the circled options' original wording, one entry per OptionIdxs
	// entry, same order.
	Options     []string `json:"options"`
	Text        string   `json:"text"` // preview-truncated
	Attachments int      `json:"attachments"`
}

type replyCardCountDTO struct {
	Waiting int `json:"waiting"`
	// Answered / Expired: recently-answered and recently-expired (24h window)
	// counts — together they let the 等我回覆 page render its collapsed
	// 近期已處理 header (and hide the pane at zero) without fetching the lists.
	Answered int `json:"answered"`
	Expired  int `json:"expired"`
}

// chatListDTO is the envelope EVERY path of GET /api/chat answers (T-48).
//
// It replaced a bare []chatMessageDTO. The array had nowhere to say "there is
// more in this direction": a caller could only infer exhaustion from a page
// shorter than `limit`, and a page is short for reasons that have nothing to do
// with exhaustion — a participant filter, `caller_only`, an unread set spread
// across senders. The inference was wrong exactly when it mattered, and wrong
// silently.
//
// NextCursor is OPAQUE (see encodeChatCursor) and omitted when the walk has
// ended — `omitempty`, so "no more" is the ABSENCE of the field rather than a
// value a client has to remember to compare against. Messages is never null:
// an empty page is `[]`, because a client that has to handle both null and []
// for the same fact will eventually handle only one.
type chatListDTO struct {
	Messages   []chatMessageDTO `json:"messages"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type chatUnreadCountDTO struct {
	Unread int `json:"unread"`
}

// resumeChatCutDTO is the CUT POINT of the wake snapshot's chat: whether
// messages exist that this payload does NOT carry, and how to go and get them.
//
// 🔴 TRUNCATION, NOT COLLAPSE. See chatMessageDTO.BodyOmittedChars for the other
// half of this split — that one is a message that is HERE, shortened; this one
// is messages that are NOT HERE. Hint must stay actionable on its own (it names
// the tool and the exact parameter pairing), because the agent reading it is
// mid-wake and has no context to look anything up with.
type resumeChatCutDTO struct {
	Omitted bool   `json:"omitted"`
	Hint    string `json:"hint"`
}

type resumeSummaryDTO struct {
	Identity *string `json:"identity"`
	// GeneratedAt is when this snapshot was assembled, with date, time AND zone
	// offset. It is the ONLY anchor in the payload for turning a ts_display into
	// 「多久以前」 — a waking agent must not assume its own wall clock agrees with
	// the server's.
	GeneratedAt        string                  `json:"generated_at"`
	Chat               []chatMessageDTO        `json:"chat"`
	ChatEarlierOmitted resumeChatCutDTO        `json:"chat_earlier_omitted"`
	Tasks              []resumeTaskDTO         `json:"tasks"`
	Roster             []resumeRosterMemberDTO `json:"roster"`
	Machines           *resumeMachinesDTO      `json:"machines"`
	Overview           resumeOverviewDTO       `json:"overview"`
	Note               string                  `json:"note"`
}

// resumeRosterMemberDTO is one entry of the studio floor a waking agent lands
// on (T-1b09; owner ruling rc-4e98c0481852, verbatim: "All members and
// contractors and their online / offline status"). The purpose is knowing WHO
// TO ASK — owner: 「目的是讓大家知道彼此在做什麼 可以去哪裡尋求協助 不是要塞爆
// 大家的 context」 — so every text field here is BOUNDED, and the block carries
// only what answers "is this the right person, and can I reach them now".
//
// 🔴 Insight and Learning are DELIBERATELY absent, and the reason matters more
// than the fact: they are NOT withheld for lack of access — role insight is
// readable by ANY authenticated identity (the same floor Duty sits on), so
// nothing technical stops this struct from carrying them. They are absent
// because the owner ruled them out on 2026-08-02 (「之後應該給 duty 就好，不要給
// insight / learning」). Anyone who later notices "we can read insight here"
// and helpfully adds it is reversing an owner decision, not filling a gap.
type resumeRosterMemberDTO struct {
	// ID is the ADDRESS. Names are editable and role names repeat, so a
	// message is only ever addressed by id — that is why it leads the row.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind separates a permanent member from a disposable contractor; a
	// contractor id is retired with the single task it was minted for, so
	// "who is this" and "how long will they exist" are the same question.
	Kind     string `json:"kind"`
	RoleName string `json:"role_name"`
	// Duty is the role's own definition text MINUS its own title line,
	// HARD-TRUNCATED at resumeDutyPreview RUNES — a flat cap on the rest of
	// the markdown, inner headings and newlines included. It is NOT one line
	// and NOT a summary: see dutyText and stripLeadingTitle, which remove a
	// syntactic prefix and make no choice about WHICH content to show.
	// Members carry it; contractors have no role and leave it "".
	Duty string `json:"duty"`
	// CurrentTask is the TITLE of the one task a contractor is bound to,
	// HARD-TRUNCATED (resumeTaskTitlePreview) — owner ruling rc-a02d8bc7fe23:
	// 正職給職責、外包給任務標題. A contractor id is minted per task, so its task
	// title IS its duty. Members leave it "": duty is stable and answers "is
	// this the right person to ask", while a member's task changes daily and
	// would churn every agent's boot for less signal.
	//
	// The truncation is not cosmetic. Measured on 2026-08-03: task titles
	// average ~99 chars and reach 147 — five untruncated contractor titles
	// alone outweigh the entire machine block.
	CurrentTask string `json:"current_task"`
	// TaskStatus / WaitingReason / ProgressDone / ProgressTotal are the bound
	// task's progress (T-925f, owner ruling rc-6935feeb293a 選①: 只補外包這一
	// 側，正職維持現狀不帶進度— see rc-a02d8bc7fe23 above for why members stay
	// bare). Both status and waiting_reason ride for FREE: contractorTaskFields
	// already loads the full Task row to build CurrentTask, so no extra query
	// buys them. progress_done/total costs exactly ONE extra query for the
	// WHOLE roster (AllTaskStepProgress, a single grouped COUNT), never one
	// per contractor. Members leave all four at their zero value.
	TaskStatus    string `json:"task_status"`
	WaitingReason string `json:"waiting_reason"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
	// Machine is the LIVE binding (which machine this member runs on right
	// now) — not LastMachineID (where it last landed) and not
	// DesiredMachineID (where the owner wants it). The three are routinely
	// different and are not interchangeable.
	Machine  string `json:"machine"`
	Presence string `json:"presence"`
}

// resumeMachineDTO is one machine in the wake snapshot's machine block.
type resumeMachineDTO struct {
	// MachineID is the STABLE id and the only safe way to name a machine:
	// our hosts report the SAME name as each other, so anything derived from
	// a hostname silently picks the wrong box and every path and dispatch
	// downstream is wrong WITHOUT erroring.
	MachineID   string `json:"machine_id"`
	DisplayName string `json:"display_name"`
	Online      bool   `json:"online"`
}

// resumeMachinesDTO is the machine block (T-1b09; owner ruling
// rc-09476f535b59: the machine LIST plus which one you are standing on). It
// deliberately does NOT group members per machine — the roster block above
// already carries each member's machine, and repeating it as a grouping would
// pay twice for one fact in a payload every agent reads on every wake.
type resumeMachinesDTO struct {
	List []resumeMachineDTO `json:"list"`
	// YouAreOn is the caller's SERVER-RECORDED machine binding; "" when the
	// caller has no binding yet (unauthenticated, or not yet landed) — never
	// an error. Never derive this from a hostname (see resumeMachineDTO).
	YouAreOn string `json:"you_are_on"`
}

// resumeOverviewDTO is the size/概要 block of the wake snapshot (T-3f31 owner
// design: peek-then-decide) — counts + character sizes so a waking agent looks
// at the SIZES first, then decides what to pull (get_task / list_reply_cards)
// and whether to hand a large digest to a sub-agent instead of loading it into
// its own context.
type resumeOverviewDTO struct {
	ChatCount           int `json:"chat_count"`            // messages in THIS snapshot
	ChatChars           int `json:"chat_chars"`            // Σ truncated body runes THIS snapshot carries
	TasksReturned       int `json:"tasks_returned"`        // light rows in THIS snapshot
	TasksOpenTotal      int `json:"tasks_open_total"`      // ALL the caller's open tasks
	TasksDetailChars    int `json:"tasks_detail_chars"`    // Σ detail_chars over the rows
	CardsWaiting        int `json:"cards_waiting"`         // the caller's waiting cards
	CardsAnsweredRecent int `json:"cards_answered_recent"` // answered in the last 24h
	// RosterChars / MachinesChars size the two studio-floor blocks THIS
	// snapshot carries (T-1b09). They are reported SEPARATELY and are
	// deliberately not folded into TasksDetailChars: that field counts text
	// the snapshot does NOT carry (the plan text a later get_task would
	// load). Mixing "what you are holding" with "what you would have to go
	// fetch" is exactly what makes a single size number un-actionable.
	RosterChars   int `json:"roster_chars"`
	MachinesChars int `json:"machines_chars"`
	// StepsOnAnsweredCard counts the answered_card_steps rows this snapshot
	// carries (T-f278) — the peek's whole point: an agent that has not pulled
	// resume_summary yet still learns from the size-only payload that N of its
	// steps are sitting on an answer nobody has picked up.
	// StepsOnAnsweredCardChars sizes the text those rows carry, and it is the
	// LAST addend of estimated_total_chars — it counts text the snapshot DOES
	// carry, like roster_chars, not text it omits like tasks_detail_chars.
	//
	// 🔴 THE SAME OMISSION HAS HAD TO BE FIXED TWICE. T-1b09 added
	// roster/machines after the peek understated the payload by the whole studio
	// floor; T-f278 added the answered-card pointers and said in this very file
	// that it was "the same mistake". The rule the two share is one line long
	// and is the only test worth writing: if the payload CARRIES the text, it is
	// an addend; if the caller would have to go and FETCH it
	// (tasks_detail_chars), it is not.
	StepsOnAnsweredCard      int `json:"steps_on_answered_card"`
	StepsOnAnsweredCardChars int `json:"steps_on_answered_card_chars"`
}

// resumeSummarySizeDTO is the size-only PEEK of the wake snapshot (T-7974
// two-step boot; peek_resume_summary_size). It carries the SAME overview
// counts a full resume_summary would report (assembled through the shared
// resumeSnapshotParts, so they can never drift) plus estimated_total_chars —
// the single number the boot threshold gates on — and a fixed guidance note.
// It carries NO chat bodies and NO task rows: peeking it costs the agent a
// few hundred bytes, not the whole payload.
type resumeSummarySizeDTO struct {
	Identity            *string           `json:"identity"`
	Overview            resumeOverviewDTO `json:"overview"`
	EstimatedTotalChars int               `json:"estimated_total_chars"`
	Note                string            `json:"note"`
}

// resumeTaskDTO is one open task the resuming caller executes (SPEC §6.2) — a
// LIGHT row (T-3f31 owner ruling: 任務不該包含細節; no steps / DoD text ride the
// wake snapshot). It names the task, its status/priority, the current node
// (id + NAME) and the progress boundary; detail_chars is the size of the plan
// text the row omits (peek-then-decide: check it before a get_task pull).
type resumeTaskDTO struct {
	ID              string  `json:"id"`
	TaskNo          string  `json:"task_no"`
	TypeKey         string  `json:"type_key"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	Priority        string  `json:"priority"`
	WaitingReason   string  `json:"waiting_reason"`
	CurrentStepID   string  `json:"current_step_id"`   // "" = no plan / all done
	CurrentStepName string  `json:"current_step_name"` // "" = no plan / all done
	ProgressDone    int     `json:"progress_done"`
	ProgressTotal   int     `json:"progress_total"`
	DetailChars     int     `json:"detail_chars"` // runes of the omitted plan text
	UpdatedTS       float64 `json:"updated_ts"`
	// Lock / ReassignedFrom / ReassignedFromKind carry the HANDOVER HOLD onto
	// the wake snapshot (T-91). The full taskDTO and the light list row have
	// carried all three for a while; this projection was the one that did not,
	// which is exactly the projection an agent reads at 開機盤點 — so a task
	// under the `reassigning` lock looked like any other open task, and the only
	// thing that said otherwise was a chat notice that is posted ONCE and, for
	// an outsource successor, is not posted at all (there is no worker id to
	// address until the scheduler mints one).
	//
	// 🔴 THE POINT IS THAT THE TICKET, NOT THE MESSAGE, IS THE PATH. The notice
	// still goes out; it is now a reminder rather than the only way to find out.
	// A member who was offline when the handover happened, and a worker minted
	// after it, both land on this row.
	Lock               string `json:"lock"`
	ReassignedFrom     string `json:"reassigned_from"`
	ReassignedFromKind string `json:"reassigned_from_kind"`
	// Blocking is the ids of the non-terminal tasks waiting on THIS one (T-91)
	// — the wake-snapshot half of taskDTO.Blocking, ids only. Always present.
	// Ids and not rows because a task id IS its task_no (T-5291), so an id names
	// the ticket without a join, and this snapshot is size-capped.
	Blocking []string `json:"blocking"`
	// AnsweredCardSteps names the steps of THIS task that sit on a reply card
	// the owner has ALREADY answered while the step itself is still
	// in_progress — the answer landed and nobody has acted on it yet (T-f278).
	//
	// 🔴 This is a POINTER, not a verdict. releaseCardHold deliberately puts a
	// held step back to in_progress when the card is answered: the server
	// releases the wait, it does not do the executor's work, and the answer is
	// just as often 不通過、改做 as it is approval. So the row says "read this
	// card, then decide"; nothing here marks the step done.
	AnsweredCardSteps []resumeAnsweredCardStepDTO `json:"answered_card_steps"`
}

// resumeAnsweredCardStepDTO is one such step: enough to go read the answer
// (card_id → get_reply_card) and to know which node of the plan it unblocks,
// without any card body riding the wake snapshot.
type resumeAnsweredCardStepDTO struct {
	StepID   string `json:"step_id"`
	StepName string `json:"step_name"`
	CardID   string `json:"card_id"`
}

// taskStepStatusReceiptDTO is the bounded confirmation returned after an agent
// reports one step. Full task detail remains available through get_task.
type taskStepStatusReceiptDTO struct {
	TaskID        string   `json:"task_id"`
	StepID        string   `json:"step_id"`
	StepStatus    string   `json:"step_status"`
	WaitingReason string   `json:"waiting_reason"`
	TaskStatus    string   `json:"task_status"`
	ClosedTS      *float64 `json:"closed_ts"`
	ProgressDone  int      `json:"progress_done"`
	ProgressTotal int      `json:"progress_total"`
}

// taskArtifactReceiptDTO is the bounded confirmation returned after pinning or
// un-pinning ONE deliverable (T-a98d). Same posture as taskStepStatusReceiptDTO:
// the write answers with what the write did — the artifact it touched and the
// resulting set size — not with the whole task. Full task detail remains
// available through get_task, and the artifact SET through list_task_artifacts
// — since T-66 get_task carries only an id+label index of the artifacts.
type taskArtifactReceiptDTO struct {
	TaskID        string `json:"task_id"`
	ArtifactID    string `json:"artifact_id"`
	ArtifactCount int    `json:"artifact_count"`
}

// taskArtifactReplaceReceiptDTO is the replace verb's receipt: the add/remove
// receipt's three fields plus how many versions the artifact now has. A
// separate type rather than an optional field on the shared one, because
// version_count is only ever meaningful for the write that MAKES a version —
// remove's answer names an artifact that no longer has any.
type taskArtifactReplaceReceiptDTO struct {
	TaskID        string `json:"task_id"`
	ArtifactID    string `json:"artifact_id"`
	ArtifactCount int    `json:"artifact_count"`
	VersionCount  int    `json:"version_count"`
}

// taskPlanReceiptDTO is the bounded confirmation returned after submit_plan.
// The caller just SENT the plan, so echoing it back is the least useful payload
// on the wire; what it cannot know is where the stored plan landed, which is
// what these counters say. Full task detail remains available through get_task.
type taskPlanReceiptDTO struct {
	TaskID        string `json:"task_id"`
	StepsTotal    int    `json:"steps_total"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
}

// taskPriorityReceiptDTO is the bounded confirmation returned after
// set_task_priority. frozen_by rides along because it is DERIVED by the write
// (stamped entering frozen, cleared leaving it), so it is exactly the part the
// caller cannot predict. Full task detail remains available through get_task.
type taskPriorityReceiptDTO struct {
	TaskID   string `json:"task_id"`
	Priority string `json:"priority"`
	FrozenBy string `json:"frozen_by"`
}

// taskCloseoutReceiptDTO is the bounded confirmation returned after
// report_task_closeout (T-bb70). BOTH exits of that handler used to answer with
// the whole task — the first (stamping) report AND the idempotent no-op repeat
// — which measured over 51,000 characters for a write whose entire news is one
// bit, so RE-reporting a close-out was the most expensive way in the system to
// be told nothing new. CloseoutTS rides along because the write DERIVES it
// (stamped by the first report, unmoved by every repeat), so it is exactly the
// part the caller cannot predict — the same reason FrozenBy rides the priority
// receipt. Full task detail remains available through get_task.
type taskCloseoutReceiptDTO struct {
	TaskID           string  `json:"task_id"`
	TaskStatus       string  `json:"task_status"`
	CloseoutReported bool    `json:"closeout_reported"`
	CloseoutTS       float64 `json:"closeout_ts"`
}

// taskStepNoteReceiptDTO is the bounded receipt for a step-note write (T-cc3e).
// It echoes the note as STORED rather than as sent: the whole point of the
// field is that a later session reads it back, so the write is verifiable at
// the write instead of needing a follow-up GET.
type taskStepNoteReceiptDTO struct {
	TaskID     string `json:"task_id"`
	StepID     string `json:"step_id"`
	StepStatus string `json:"step_status"`
	Note       string `json:"note"`
	// SizeChars / CapChars close the gap T-6bd2 measured: the PATCH face's
	// receipt has carried this pair since T-1667, and this wholesale one did
	// not — so the writer that replaces a note outright (the common case, and
	// the one that has just deleted the previous session's hand-off to make
	// room) was the one writer told nothing about how much room is left. Same
	// pair, same names, same ceiling as taskStepNotePatchResultDTO.
	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`
}

type bootstrapDTO struct {
	Role    string  `json:"role"`
	Name    string  `json:"name"`
	Context string  `json:"context"`
	Token   *string `json:"token"`
}

// ── tasks (M3) ───────────────────────────────────────────────────────────────

// taskRefDTO is the light task reference a reply card carries when it was
// armed from a task gate (請示 → 任務 jump, SPEC §3.6).
type taskRefDTO struct {
	ID      string `json:"id"`
	TypeKey string `json:"type_key"`
	Title   string `json:"title"`
}

type taskStepDTO struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	OrderIdx      int    `json:"order_idx"`
	Name          string `json:"name"`
	DoD           string `json:"dod"`
	Status        string `json:"status"`
	ParallelGroup string `json:"parallel_group"`
	IsGate        bool   `json:"is_gate"`
	ReplyCardID   string `json:"reply_card_id"`
	// ReplyCardStatus: read-time join of the bound card's live status
	// ("waiting" | "answered", or "" when no card). Filled by newTaskDTO from a
	// step→status map; the task-embedded TaskReplyCard reads it to lazy-load
	// answered cards, and the board derives the H4 badge from it. See
	// TaskStepDTO in the spec.
	ReplyCardStatus string `json:"reply_card_status"`
	// WaitingReason: non-empty only while the step is waiting_external (T-9ca5 —
	// the task-level waiting_reason moved down to the step here).
	WaitingReason string `json:"waiting_reason"`
	// 🔴 THERE IS NO `Note` FIELD HERE, AND ITS ABSENCE IS THE DELIVERABLE
	// (T-66, owner card rc-4c8065fb30a5: 「整個拿掉，做在組裝票那一層（九個介面
	// 一起瘦），座艙改成點開才抓」). The note text used to ride EVERY response
	// built from this struct — get_task, terminate, reassign, claim, duplicate,
	// deps, the create dedupe hit, description, title — nine exits carrying a
	// 4,000-rune-capped free-text field per step for callers that wanted one of
	// them or none.
	//
	// It was removed from the SCHEMA rather than left declared-and-empty on
	// purpose. A field that is present on the wire and always blank is a silent
	// lie: every existing reader keeps compiling and starts reading "" as "this
	// step has no note". Deleting it makes the cockpit's TypeScript fail to
	// build, which is the loud failure the owner asked for. Do not reinstate it
	// "for compatibility" — that IS the failure mode this removal exists to
	// prevent. The text is served by taskStepDetailDTO (GET
	// /api/tasks/{task_id}/steps/{step_id}, MCP get_task_step), one step at a
	// time.
	//
	// NoteSizeChars / NoteCapChars are the note's two numbers, the same pair
	// every other capped document on this station reports on its own read
	// (T-6bd2). Until this ticket the step note was the ONE capped document
	// whose remaining room could not be computed from any read at all: the
	// wholesale write receipt omitted them and so did this view, so an agent
	// only ever learned the number from the 400 that refused its write — the
	// single worst moment to learn it, and the cell that gets hit most often.
	//
	// They are named for the field they measure rather than the bare
	// size_chars/cap_chars the single-document DTOs use, because a step row
	// carries three texts (name, dod, note) and an unqualified pair here would
	// read as if it sized the row.
	//
	// ⚠️ NoteCapChars is REPORTED, never enforced here; the ceiling stays the
	// write face's (stepNoteWithinLimit). T-6bd2 does not move it.
	NoteSizeChars int     `json:"note_size_chars"`
	NoteCapChars  int     `json:"note_cap_chars"`
	StartedTS     float64 `json:"started_ts"`
	FinishedTS    float64 `json:"finished_ts"`
}

// taskStepDetailDTO is ONE step served IN FULL (T-66) — the other half of the
// split taskStepDTO's missing Note opens. It is deliberately a SEPARATE type
// rather than taskStepDTO plus a field, because the two are answers to two
// different questions and one struct with a sometimes-filled Note is exactly
// the shape that makes "" ambiguous again.
//
// It carries NO task fields and NO sibling steps. A caller that wanted one
// note and got the ticket is what this ticket is about; answering with the
// task's other 30 fields "while we are here" reinstates the cost on a smaller
// scale.
//
// DetailLevel is the self-description AC: a reader tells this response apart
// from taskDTO's steps by what the payload SAYS, not by inspecting which fields
// happen to be present.
type taskStepDetailDTO struct {
	DetailLevel     string  `json:"detail_level"`
	ID              string  `json:"id"`
	TaskID          string  `json:"task_id"`
	OrderIdx        int     `json:"order_idx"`
	Name            string  `json:"name"`
	DoD             string  `json:"dod"`
	Status          string  `json:"status"`
	ParallelGroup   string  `json:"parallel_group"`
	IsGate          bool    `json:"is_gate"`
	ReplyCardID     string  `json:"reply_card_id"`
	ReplyCardStatus string  `json:"reply_card_status"`
	WaitingReason   string  `json:"waiting_reason"`
	Note            string  `json:"note"`
	NoteSizeChars   int     `json:"note_size_chars"`
	NoteCapChars    int     `json:"note_cap_chars"`
	StartedTS       float64 `json:"started_ts"`
	FinishedTS      float64 `json:"finished_ts"`
}

// taskDetailLevelSummary / taskDetailLevelFull are the two values of the
// self-description pair. They are constants rather than literals at the two
// build sites so the pairing cannot drift into three spellings.
const (
	taskDetailLevelSummary = "summary"
	taskDetailLevelFull    = "full"
)

// taskArtifactsDetailLevelFull is what list_task_artifacts says about itself:
// every artifact row it carries is complete.
//
// ⚠️ SINCE T-92 IT HAS NO COUNTERPART. It used to be half of a pair — the task
// projection declared "index" for its id+label rows — and that projection now
// carries a COUNT and no rows at all, so there is nothing left to contrast with.
// It is kept as a self-description without an opposite, the shape
// `notes_included` already has, so a reader holding this payload does not have
// to know which server version produced it to know the rows are whole. The
// "index" constant is gone with the rows it described.
const taskArtifactsDetailLevelFull = "full"

// taskArtifactDTO is one pinned deliverable on a task's artifact set (T-3dc5,
// reshaped by T-92). URL has ONE meaning on every kind — where to go for this
// deliverable's content: the blob serve path for a file/image, the external
// address for a link (read out of that link's text/uri-list blob). The blob id
// is no longer a field of its own; it is the tail of URL.
//
// 🔴 Name is NEVER EMPTY here even though the COLUMN usually is: it is derived
// read-time (see artifactDisplayName). Description is the prose half of the old
// label and may be empty AND may exceed the 256-rune write cap.
//
// Mime survives the narrowing on purpose: it is the only field that separates a
// .md from a .pdf from a .zip, which Kind cannot do, and the cockpit's preview
// decides four things with it. IsImage went because it is Mime's prefix, and
// Filename went because Name derives from it.
type taskArtifactDTO struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	URL         string  `json:"url"`
	Mime        string  `json:"mime"`
	CreatedTS   float64 `json:"created_ts"`
	CreatedBy   string  `json:"created_by"`
	// VersionCount counts the versions of this deliverable WITH the live one
	// (T-60), so a never-replaced artifact reads 1 rather than 0 — the reader
	// asks "how many versions are there", and there is always this one.
	VersionCount int `json:"version_count"`
}

// taskArtifactVersionDTO is one RETAINED previous version of an artifact. It
// carries the version whole rather than a size summary the way
// DocumentHistoryDTO does: an artifact version is a pointer plus a label, so
// there is no prose a listing would have to hold back.
type taskArtifactVersionDTO struct {
	ID           int64   `json:"id"`
	Kind         string  `json:"kind"`
	URL          string  `json:"url"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Filename     string  `json:"filename"`
	Mime         string  `json:"mime"`
	IsImage      bool    `json:"is_image"`
	AttachmentID string  `json:"attachment_id"`
	CreatedTS    float64 `json:"created_ts"`
	CreatedBy    string  `json:"created_by"`
}

// newTaskArtifactVersionDTO projects one retained version onto the wire. att is
// the resolved chat_attachment for a file/image version (nil for a link, or
// when the blob is gone) — its url/mime/filename/is_image ride along through
// artifactBlobFacts, the SAME resolution the live projection does, honest-empty
// when absent and never fabricated.
//
// 🔴 The url has to be rewritten here, not copied. `task_artifact.url` is the
// external link for a link kind and the EMPTY STRING for a file/image, so a
// version that carried the row's url handed the cockpit nothing to fetch and
// every file version read as gone.
//
// 🔴 The filename is here because a reader deciding whether a version's bytes
// are TEXT asks the name when the mime cannot say, and `application/octet-stream`
// is what the agent upload path says about the .md reports this journal mostly
// holds. Without it a version whose label is empty has no name at all, and that
// deliverable class could never reach the diff.
func newTaskArtifactVersionDTO(h TaskArtifactHistory, att *ChatAttachment) taskArtifactVersionDTO {
	dto := taskArtifactVersionDTO{
		ID:           h.ID,
		Kind:         h.Kind,
		Name:         h.Name,
		Description:  h.Description,
		AttachmentID: h.AttachmentID,
		CreatedTS:    h.CreatedTS,
		CreatedBy:    h.CreatedBy,
	}
	if h.Kind == ArtifactKindLink {
		dto.URL = linkTargetOf(att)
	}
	if b, ok := artifactBlobFacts(att); ok && h.Kind != ArtifactKindLink {
		dto.URL, dto.Mime, dto.Filename, dto.IsImage = b.url, b.mime, b.filename, b.isImage
	}
	return dto
}

// taskArtifactListDTO is the answer of GET /api/tasks/{task_id}/artifacts: one
// task's artifact set IN FULL, oldest→newest. It is a wrapped list rather than
// a bare array so the response can say what it is — ArtifactsDetailLevel here
// is "full", the counterpart of the "index" the task projection declares, the
// same way taskStepDetailDTO answers "full" against taskDTO's "summary".
type taskArtifactListDTO struct {
	TaskID               string            `json:"task_id"`
	ArtifactsDetailLevel string            `json:"artifacts_detail_level"`
	Artifacts            []taskArtifactDTO `json:"artifacts"`
}

// 🔴 ArtifactsDetailLevel STAYS, and not as a matter of taste:
// conformance/test_rest_happy.py asserts `d["artifacts_detail_level"] == "full"`
// on this very response, so removing it turns that check red. What T-92 changed
// is its DEFINITION, not its presence — see taskArtifactsDetailLevelFull.

type taskDTO struct {
	ID           string         `json:"id"`
	TaskNo       string         `json:"task_no"`
	TypeKey      string         `json:"type_key"`
	Title        string         `json:"title"`
	DedupeKey    string         `json:"dedupe_key"`
	Inputs       map[string]any `json:"inputs"`
	Description  string         `json:"description"`
	DuplicateOf  string         `json:"duplicate_of"` // '' unless status=duplicated
	Status       string         `json:"status"`
	Lock         string         `json:"lock"` // '' | 'reassigning' — orthogonal system hold (T-9ca5)
	Priority     string         `json:"priority"`
	ExecutorKind string         `json:"executor_kind"`
	ExecutorID   string         `json:"executor_id"`
	CreatorID    string         `json:"creator_id"`
	// ReassignedFrom / ReassignedFromKind: the predecessor the task was last
	// handed over from (T-ba04); "" / "" when never reassigned.
	ReassignedFrom     string        `json:"reassigned_from"`
	ReassignedFromKind string        `json:"reassigned_from_kind"`
	HandoverNote       string        `json:"handover_note"`
	HandoverNoteTS     float64       `json:"handover_note_ts"`
	HandoverNoteBy     string        `json:"handover_note_by"`
	WaitingReason      string        `json:"waiting_reason"`
	CreatedTS          float64       `json:"created_ts"`
	UpdatedTS          float64       `json:"updated_ts"`
	ClosedTS           *float64      `json:"closed_ts"` // null while open
	Deps               []string      `json:"deps"`
	Steps              []taskStepDTO `json:"steps"`
	// DetailLevel / NotesIncluded are the response DESCRIBING ITSELF (T-66).
	// The AC is verbatim「成功的回應不得看起來像完整的 task」: a caller must be
	// able to tell FROM THE PAYLOAD that something was left out, without
	// knowing which fields a full task used to carry.
	//
	// Always "summary" / false — constants on THIS type, not a mode switch.
	// There is no ?detail=full and there is not meant to be one: the counterpart
	// read is get_task_step, whose taskStepDetailDTO answers "full".
	//
	// 🔴 THERE IS NO third "the step LIST may be cut" marker, and that is an
	// executor judgement backed by evidence, not an oversight. resume_summary
	// carries exactly such a pair (resumeChatCutDTO{Omitted, Hint}) because its
	// chat block IS budget-packed, so the marker has a trigger. This face has
	// none: taskDTOOf's steps come from DAL.ListTaskSteps — one unbounded
	// `SELECT ... WHERE task_id = ? ORDER BY order_idx, id`, no LIMIT, no
	// cursor, no caller-supplied cap — and newTaskDTO appends every row it is
	// handed. A marker here would be a guard that can never fire, and a guard
	// that can never fire reads exactly like a green one. The completeness is
	// stated in the tool description instead, where a caller can act on it.
	DetailLevel   string `json:"detail_level"`
	NotesIncluded bool   `json:"notes_included"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
	// CloseoutReported flips true once the executor reports the close-out
	// follow-ups done (report_task_closeout; §6.3 — terminal tasks only).
	CloseoutReported bool `json:"closeout_reported"`
	// ArtifactCount is HOW MANY deliverables are pinned — and since T-92 it is
	// ALL this response says about them. T-66 had already cut the rows down to
	// id + label; the owner's original ruling on this ticket was that even the
	// id earns nothing (rc-15016959ad4d:「只有 ID 好像也沒用」), because a caller
	// holding an id is a caller about to act on that artifact, which needs the
	// row anyway. list_task_artifacts answers the whole ticket in one call.
	//
	// 🔴 IT IS THE SAME FIELD taskListItemDTO has carried since T-3dc5, and that
	// is the point: the light list and the full read now agree instead of
	// disagreeing about what a task says about its deliverables.
	//
	// The count is exact, uncapped and never truncated — 0 means the task
	// genuinely has nothing pinned, the same promise NoteSizeChars makes.
	ArtifactCount int `json:"artifact_count"`
	// Handoff / HandoffNote / HandoffTaskID: the DECLARED destination of the
	// ball at close (T-74f8). "" = never declared (every task whose creator IS
	// its executor, and every pre-column row); otherwise return_to_creator |
	// follow_up | none. Served so the declaration is auditable — a gate whose
	// answer is invisible is indistinguishable from no gate.
	Handoff       string `json:"handoff"`
	HandoffNote   string `json:"handoff_note"`
	HandoffTaskID string `json:"handoff_task_id"`
	// Blocking is the REVERSE of Deps (T-91): the NON-TERMINAL tasks that name
	// THIS task in their own blocked_by. Always present ([] when nobody waits).
	//
	// 🔴 IT EXISTS BECAUSE THE BLOCKING SIDE HAD NO CHANNEL AT ALL. set_task_deps
	// publishes the delta of the BLOCKED task only, so hanging a ticket off
	// someone else's told that someone else nothing — not a message, not a
	// field, not a badge. The owner ruled the fix is written ON THE TICKET and
	// is NEVER a message (deliberately the opposite of the close notice), so
	// this field and its wake-snapshot twin (resumeTaskDTO.Blocking) are the
	// whole delivery: there is no notification to look for, and adding one
	// would be reversing the ruling rather than completing it.
	//
	// Terminal waiters are dropped: a closed ticket is not waiting for anything,
	// and a blocker's executor reading "3 tickets are waiting" wants the 3 that
	// still are.
	Blocking []taskDepRefDTO `json:"blocking"`
	// FrozenBy names WHO put this task into the frozen priority (T-6020):
	// "owner" for the owner's own click, else the member / outsource-worker id.
	// "" whenever priority != frozen (and on pre-column rows). Served because
	// frozen is no longer a single-actor knob — owner, admin_agent and the
	// task's executor may all freeze and unfreeze — so the owner needs to read
	// off a frozen ticket whether the 喊停 was theirs.
	FrozenBy string `json:"frozen_by"`
}

// taskListItemDTO is the LIGHT list projection served by GET /api/tasks (and
// MCP list_tasks): the fields the 任務清單 card renders collapsed. It DROPS the
// heavy per-task detail the full taskDTO carries — steps, description, inputs —
// which the list never shows until a card is expanded (the FE then fetches the
// full task via GET /api/tasks/{id}). progress_done/total still ride along,
// counted in SQL (dal.AllTaskStepProgress) rather than from loaded steps.
type taskListItemDTO struct {
	ID           string `json:"id"`
	TaskNo       string `json:"task_no"`
	TypeKey      string `json:"type_key"`
	Title        string `json:"title"`
	DedupeKey    string `json:"dedupe_key"`
	DuplicateOf  string `json:"duplicate_of"` // '' unless status=duplicated
	Status       string `json:"status"`
	Lock         string `json:"lock"` // '' | 'reassigning' — orthogonal system hold (T-9ca5)
	Priority     string `json:"priority"`
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	CreatorID    string `json:"creator_id"`
	// ReassignedFrom / ReassignedFromKind: the predecessor the task was last
	// handed over from (T-ba04); "" / "" when never reassigned.
	ReassignedFrom     string   `json:"reassigned_from"`
	ReassignedFromKind string   `json:"reassigned_from_kind"`
	WaitingReason      string   `json:"waiting_reason"`
	CreatedTS          float64  `json:"created_ts"`
	UpdatedTS          float64  `json:"updated_ts"`
	ClosedTS           *float64 `json:"closed_ts"` // null while open
	Deps               []string `json:"deps"`
	// DepTasks carries the DISPLAY facts of every id in Deps (T-a3e4), resolved
	// against the whole task table by the ONE ListTasks read the handler already
	// does — one entry per dep, same order. The card's 「等 <task id> <標題>」 row
	// renders straight from this, so a dep that has already CLOSED no longer
	// forces the client to download the closed population to name it. Never nil
	// (an empty list is honest for a task with no deps); a dep whose task is
	// gone still gets an entry, with Status/Title "".
	DepTasks      []taskDepRefDTO `json:"dep_tasks"`
	ProgressDone  int             `json:"progress_done"`
	ProgressTotal int             `json:"progress_total"`
	// CurrentStepID / CurrentStepName point at the step the task is ON right
	// now — the FIRST non-terminal step in timeline order (domain.CurrentStep,
	// the same rule the wake snapshot's resumeTaskDTO uses). Both are "" when
	// the plan is empty or every step has finished; that empty is honest and
	// must not be read as "the first step". The pair is an id and a name and
	// nothing else about the step — where this belongs on the light list is
	// still open (owner c-2823f0ff85b5:「我覺得這不屬於 list task 的範疇」;
	// c-1648d14be429:「先不動這個 之後要調再說」), so nothing here or in the tool
	// description recommends it as a route. The light list still carries no
	// step ROWS (no dod text) — only these two display fields.
	CurrentStepID   string `json:"current_step_id"`
	CurrentStepName string `json:"current_step_name"`
	// ArtifactCount is the number of pinned deliverables (T-3dc5) — the collapsed
	// card's 「產物 N」 badge; 0 (the zero value) when none, so the badge hides.
	// The light list never loads the artifact rows themselves (get_task folds
	// the full set).
	ArtifactCount int `json:"artifact_count"`
}

// taskDepRefDTO is one entry of taskListItemDTO.DepTasks: a dep id resolved to
// what the row actually prints (T-a3e4). TaskNo IS the id (T-5291 — no
// transform at all), so it is filled even when the dep's task row is GONE
// (naming the dep never required loading it) — Status/Title are
// "" in exactly that case, which is the client's honest 查無此任務 row. Nothing
// is ever defaulted to a plausible-looking status: the absence of one IS the
// signal.
type taskDepRefDTO struct {
	ID     string `json:"id"`
	TaskNo string `json:"task_no"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type taskCreateResultDTO struct {
	Task    taskDTO `json:"task"`
	Deduped bool    `json:"deduped"`
	// Warnings: non-blocking advisories on a typed create — input field names
	// the manual does not define, or ambiguous keys that fold onto another.
	// Omitted when none (optional, back-compatible — §12 DTO convention).
	Warnings []string `json:"warnings,omitempty"`
}

type taskCountDTO struct {
	Open int `json:"open"`
	// Total is every task, terminal included (T-a3e4). The nav badge only wants
	// Open; Total exists so the 任務頁 can word its empty screen honestly now
	// that the list endpoint answers a STATUS SET — an empty list alone cannot
	// tell 「什麼都沒有」 from 「這幾個狀態裡沒有」, and 目前沒有任務 is a claim
	// about the whole workshop. It is a count, not a list: nobody has to widen a
	// list fetch to find this out.
	Total int `json:"total"`
}

type taskManualDTO struct {
	// Per-CAPPED-DOCUMENT sizes AND, since T-30f1, per-capped-document caps:
	// learnings and sop_md are judged by two independent settings, so a single
	// cap number could only ever be right for one of them.
	//
	// CapChars is the DEPRECATED pre-split field. It is kept on the wire (its
	// removal is a separate, owner-approved step) and carries the LEARNINGS cap
	// — the segment that actually accumulates, and the one every pre-split
	// caller was watching. A client reading it about sop_md gets a number that
	// is merely stale rather than absent, which is why the split fields are the
	// ones the descriptions point at.
	LearningsChars    int            `json:"learnings_chars"`
	SopMDChars        int            `json:"sop_md_chars"`
	LearningsCapChars int            `json:"learnings_cap_chars"`
	SopMDCapChars     int            `json:"sop_md_cap_chars"`
	CapChars          int            `json:"cap_chars"`
	TypeKey           string         `json:"type_key"`
	DisplayName       string         `json:"display_name"`
	Purpose           string         `json:"purpose"`
	Fields            []ManualField  `json:"fields"`
	SopMD             string         `json:"sop_md"`
	Learnings         string         `json:"learnings"`
	Assignee          map[string]any `json:"assignee"`
	UpdatedTS         float64        `json:"updated_ts"`
}

// taskManualListItemDTO is one row of GET /api/task-manuals: the type's
// identity, its input fields and its assignee setting — plus the SIZES of the
// two long documents and the cap each is judged against.
//
// sop_md and learnings are ABSENT from the wire, not served as "". They are the
// bulk that made this listing unreadable, and an empty string in the field that
// normally holds the SOP reads as "this type has no SOP". The sizes are still
// measured on the STORED row (see newTaskManualListItemDTO), because a zero that
// looks like a measurement is worse than the omission it describes.
type taskManualListItemDTO struct {
	LearningsChars    int            `json:"learnings_chars"`
	SopMDChars        int            `json:"sop_md_chars"`
	LearningsCapChars int            `json:"learnings_cap_chars"`
	SopMDCapChars     int            `json:"sop_md_cap_chars"`
	CapChars          int            `json:"cap_chars"`
	TypeKey           string         `json:"type_key"`
	DisplayName       string         `json:"display_name"`
	Purpose           string         `json:"purpose"`
	Fields            []ManualField  `json:"fields"`
	Assignee          map[string]any `json:"assignee"`
	UpdatedTS         float64        `json:"updated_ts"`
}

type taskManualDeleteResultDTO struct {
	TypeKey string `json:"type_key"`
	Deleted bool   `json:"deleted"`
}

// themeListItemDTO is one row of GET /api/themes (T-83ef).
//
// 🔴 IT IS NOT THE BUNDLE, AND THAT IS THE WHOLE POINT OF THE ENDPOINT. A theme
// carries its images embedded, so listing whole bundles is the several-hundred-
// kilobyte answer that made GET /api/settings unusable in the first place —
// serving it again from a new path would have moved the problem, not fixed it.
// These two fields are what the cockpit's theme list and the profile picker
// actually render; applying, editing and exporting are all about ONE theme and
// go to GET /api/themes/{theme_id}.
type themeListItemDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// themeWriteReceiptDTO answers PUT /api/themes/{theme_id} (T-83ef).
//
// 🔴 IT IS A RECEIPT RATHER THAN THE STORED BUNDLE, AND THAT IS THE POINT. A
// bundle carries its images embedded — one of the themes this ticket moved is
// 953 KB on its own — so echoing the write back would send that payload a
// SECOND time, in the direction the split exists to unburden. Everything here
// is something the caller cannot already know.
//
// Created separates "this id had no row" from "an existing theme was replaced";
// OrderIdx is the theme's place in the owner's list, which a replace KEEPS, so
// re-colouring a theme does not move it to the bottom.
type themeWriteReceiptDTO struct {
	ID        string  `json:"id"`
	Created   bool    `json:"created"`
	OrderIdx  int     `json:"order_idx"`
	UpdatedAt float64 `json:"updated_at"`
}

// themeDeleteResultDTO answers DELETE /api/themes/{theme_id} (T-83ef).
//
// DisplayThemeReset is the field worth having: deleting the ACTIVE theme resets
// display.theme back to "" in the same request — the coupling the whole-array
// settings write used to perform — and saying so here is what stops the cockpit
// having to re-read settings to discover its theme changed underneath it.
type themeDeleteResultDTO struct {
	ID                string `json:"id"`
	Deleted           bool   `json:"deleted"`
	DisplayThemeReset bool   `json:"display_theme_reset"`
}

// docSummaryDTO is one row of GET /api/docs — a product-guide doc's addressable
// slug + its display title (the first "# " heading, or the slug when the doc
// carries no heading). The full body is fetched per-slug via GET /api/docs/{slug}.
type docSummaryDTO struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// docDTO is GET /api/docs/{slug} — one product-guide doc in full. MarkdownMD is
// the embedded markdown with relative image paths (`](assets/…)`) rewritten to
// the served `/api/docs/assets/…` endpoint, so both the cockpit renderer and an
// MCP reader resolve images against the same origin.
type docDTO struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	MarkdownMD string `json:"markdown_md"`
}

type outsourceWorkerDTO struct {
	ID        string `json:"id"`
	AvatarURL string `json:"avatar_url"`
	Codename  string `json:"codename"`
	Runtime   string `json:"runtime"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	// Actual* are the REPORTED twins of the three configured launch fields
	// above, read off the same roster row the member DTO serves. "" = nothing
	// has ever reported one; they never fall back to the configured value, so
	// the panel can tell "you changed this, it has not taken effect yet" from
	// "this is what it is running" (T-7f28).
	ActualModel   string `json:"actual_model"`
	ActualRuntime string `json:"actual_runtime"`
	ActualEffort  string `json:"actual_effort"`
	Status        string `json:"status"`
	TaskID        string `json:"task_id"`
	TaskTitle     string `json:"task_title"`
	TaskStatus    string `json:"task_status"`
	// The bound task's display number / created stamp / type — what the office
	// 外包 row prints and orders by (T-a3e4). They were a CLIENT-side join
	// against the unfiltered GET /api/tasks (the whole task history pulled on
	// every worker/chat delta to label a handful of rows); the server owns the
	// join now. task_type_name is the manual's human label for task_type_key,
	// "" when the manual is gone — the client then shows the raw key, the same
	// honest fallback it had when it held the manuals list itself. All four are
	// zero/"" when the bound task cannot be resolved.
	TaskNo        string  `json:"task_no"`
	TaskCreatedTS float64 `json:"task_created_ts"`
	TaskTypeKey   string  `json:"task_type_key"`
	TaskTypeName  string  `json:"task_type_name"`
	CreatedTS     float64 `json:"created_ts"`
	// The caller's unread count for this worker's chat — the SAME chat_read
	// watermark inverse the member roster serves (UnreadCounts); the office
	// 外包 row's red badge (owner report 2026-07-14: 外包也要有未讀紅點).
	UnreadCount int `json:"unread_count"`
	// Presence is the REAL-liveness projection (A案 P6 — the ONE member liveness
	// vocabulary, deriveLiveness; it replaces the retired spawn_state closed set
	// starting/stuck/online/stopped). Distinct from the lifecycle Status so the
	// cockpit never renders a worker whose session is not actually up as a live
	// green row (O-19). Closed set (the member presence vocabulary):
	//   "online"   — the worker holds a live SSE connection (hub.IsOnline) —
	//                the SAME presence authority the member roster uses;
	//   "waking"   — not online, with a fresh wake in flight (the last start
	//                dispatch — or the row's birth while placement is pending —
	//                within WakingTTLSecs);
	//   "offline"  — not online and no fresh wake (a failed/silent spawn, or a
	//                session that died after claiming — the states the retired
	//                spawn_state called "stuck"; the FSM rescue owns recovery);
	//   "stopping" / "stopped" — owner-explicit stop (desired_state=="offline"):
	//                held down, no auto-revival — stopping while the session
	//                still winds down, stopped once it is gone;
	//   ""         — released (filtered off the panel; never rendered).
	Presence string `json:"presence"`
	// ── T-f190: the detail-panel alignment fields (外包詳情頁對齊成員詳情) ──────
	// Machine is the ACTUAL dispatch target (the in-memory spawn target
	// resolved to its registry display name — P7d moved the observation off the
	// durable row), NOT the manual's placement preference: "" when the worker
	// was never dispatched this server run (未分配 — the panel shows 「尚未分配」,
	// never a fabricated machine name). DesiredMachineID is the owner-pinned placement
	// (relocate target; the picker's bound machine) — raw id, resolved FE-side.
	Machine          string `json:"machine"`
	DesiredMachineID string `json:"desired_machine_id"`
	// ActualMachine is the DURABLE last-observed machine (last_machine_id), the
	// offline-surviving twin of Machine: a relocation stays legible as pending
	// while the worker is down, which the in-memory Machine cannot express.
	ActualMachine string `json:"actual_machine"`
	// Account / ContextPct / Cost are RUNTIME facts folded from the SAME
	// per-actor telemetry+gauge the member roster reads (keyed by the worker's
	// actor id). Nullable — nil serialises null → the panel shows a bare dash,
	// never a fabricated value (parity with monitoringSessionDTO's honest gate).
	Account         *string  `json:"account"`
	ContextPct      *float64 `json:"context_pct"`
	CompactionCount *int     `json:"compaction_count,omitempty"`
	Cost            *float64 `json:"cost"`
	// BankedCost mirrors member banked_cost (T-ba6b, migrations/00021): the
	// durable cumulative spend banked on every session end / kill+respawn.
	// nil when zero (nothing banked yet) → the panel adds nothing; the view
	// sums live + banked, the member presentation.
	BankedCost *float64 `json:"banked_cost"`
	// last_op* mirror the member last_op* fold (durable since T-9ccf 00017): the
	// last warden command receipt, surfaced as the panel's 「最近操作」 block.
	// LastOpOK is three-valued (nil = no receipt folded yet).
	LastOp       string  `json:"last_op"`
	LastOpOK     *bool   `json:"last_op_ok"`
	LastOpLog    string  `json:"last_op_log"`
	LastOpReason string  `json:"last_op_reason"`
	LastOpAt     float64 `json:"last_op_at"`
	// CreatorID is the RAW verified sub of the bound task's creator (a member id,
	// the literal "owner", or "" on pre-column/server-scheduled rows); DelegatedBy
	// is the RESOLVED member display name (or "" — the owner and unknown cases
	// carry no member name). Together they let the client honestly distinguish
	// owner vs member vs unassigned, replacing the former unconditional hardcoded
	// "System owner" placeholder (T-f190 item 2).
	CreatorID   string `json:"creator_id"`
	DelegatedBy string `json:"delegated_by"`
	// RefocusSince is the in-flight context-handover stamp (T-32e1), epoch seconds
	// mirroring member.refocus_since: 0.0 = unset, >0 = stamp time (the mapper
	// converts 0→null so the panel shows no 換手中 line rather than a fabricated
	// time). DesiredState mirrors member.desired_state ("online"/"offline"): the
	// run-intent the stop/restart toggle drives; spawn_state is "stopped" while
	// "offline".
	RefocusSince float64 `json:"refocus_since"`
	// RefocusOp names which operation opened that window ("" when none is in
	// flight); RefocusDeadline is the epoch it is force-collected by. Together
	// they let the panel say "winding down so your change can take effect, by
	// HH:MM" instead of "last handover", which reads as history (T-7f28).
	RefocusOp       string  `json:"refocus_op"`
	RefocusDeadline float64 `json:"refocus_deadline"`
	DesiredState    string  `json:"desired_state"`
	// The three RESPONSE-ONLY pending signals an owner verb owes its caller
	// (T-ed79 #5/#12) — the worker twins of the MemberDTO fields of the same
	// names, and pointers-with-omitempty for the same reason: they are absent on
	// every read face and on every verb that has nothing to defer, so a consumer
	// can tell "this answer does not carry the signal" from "the signal is false".
	//
	// RelocationPending/RelocationDeferred appear only on the relocate response,
	// ActivationPending only on the restart response. The panel-parity doc listed
	// their absence as A9 「外包端根本沒有訊號可顯示」.
	RelocationPending  *bool `json:"relocation_pending,omitempty"`
	RelocationDeferred *bool `json:"relocation_deferred,omitempty"`
	ActivationPending  *bool `json:"activation_pending,omitempty"`
}

// outsourceWorkerProjection carries the per-worker runtime facts the DTO folds
// on top of the durable row: the caller's unread count, wall clock, SSE
// presence, the worker's own telemetry/gauge entries (keyed by actor id — the
// SAME maps the member roster reads), a machine-id → display-name resolver, and
// the pre-resolved creator display name. Grouped into one struct so the two
// callers (list loop + single GET) share the exact same fold.
type outsourceWorkerProjection struct {
	unread int
	now    float64
	online bool
	// cfg is the SAME reconcile config the tick collects this worker on, so the
	// deadline on the wire and the deadline that actually kills come from one
	// source (T-fe5e). Carried rather than derived: a second copy of the grace
	// here is exactly how the two drifted apart the first time.
	cfg            reconcileConfig
	tele           map[string]any      // telemetry[w.ID]; nil-safe
	gaugeEntry     map[string]any      // gauge[w.ID]; nil-safe
	machineDisplay func(string) string // machine id → registry display label
	// spawnTarget is the worker's OBSERVED host: the warden the last start was
	// dispatched to (workerSpawnTarget, in-memory since the P7d fold), or —
	// when a re-exec forgot that dispatch (T-c23a) — the restart-proof
	// observed host (live SSE machine claim → telemetry `machine`,
	// observedWorkerHost). "" = nothing observed — the panel renders
	// 「尚未分配」.
	spawnTarget string
	// accountDisplay resolves the raw telemetry account key to its readable
	// name (alias → owner-gated reported label → "") via the SHARED
	// resolveAccountDisplay fold. "" ⇒ the DTO serves null → the panel's
	// honest dash — the raw credential hash NEVER reaches the wire (T-ba6b).
	accountDisplay func(string) string
	delegatedBy    string // resolved creator name ("" = honest fallback)
	// typeDisplay resolves the bound task's type_key to the manual's human
	// label (T-a3e4) — the panel's second line. nil or a "" result leaves
	// task_type_name empty and the client falls back to the raw key, exactly
	// as it did when it looked the manuals up itself.
	typeDisplay func(string) string
}

// newTaskStepDTO projects one step row onto the wire. cardStatus maps a bound
// reply_card_id → its live status ("waiting"/"answered"); a step with no card
// (or an id absent from the map) serialises reply_card_status "".
func newTaskStepDTO(st TaskStep, cardStatus map[string]string) taskStepDTO {
	return taskStepDTO{
		ID:              st.ID,
		TaskID:          st.TaskID,
		OrderIdx:        st.OrderIdx,
		Name:            st.Name,
		DoD:             st.DoD,
		Status:          st.Status,
		ParallelGroup:   st.ParallelGroup,
		IsGate:          st.IsGate,
		ReplyCardID:     st.ReplyCardID,
		ReplyCardStatus: cardStatus[st.ReplyCardID],
		WaitingReason:   st.WaitingReason,
		// 🔴 st.Note is measured here and NOT carried (T-66). The size is the
		// whole statement the summary row makes about the note: a caller reads
		// note_size_chars and decides whether to spend a get_task_step.
		NoteSizeChars: utf8.RuneCountInString(st.Note),
		NoteCapChars:  chatBodyMaxChars,
		StartedTS:     st.StartedTS,
		FinishedTS:    st.FinishedTS,
	}
}

// newTaskStepDetailDTO projects ONE step onto the single-step wire (T-66),
// note text included. cardStatus is the same read-time join newTaskStepDTO
// takes, so the two faces of a step can never disagree about a bound card.
func newTaskStepDetailDTO(st TaskStep, cardStatus map[string]string) taskStepDetailDTO {
	return taskStepDetailDTO{
		DetailLevel:     taskDetailLevelFull,
		ID:              st.ID,
		TaskID:          st.TaskID,
		OrderIdx:        st.OrderIdx,
		Name:            st.Name,
		DoD:             st.DoD,
		Status:          st.Status,
		ParallelGroup:   st.ParallelGroup,
		IsGate:          st.IsGate,
		ReplyCardID:     st.ReplyCardID,
		ReplyCardStatus: cardStatus[st.ReplyCardID],
		WaitingReason:   st.WaitingReason,
		Note:            st.Note,
		NoteSizeChars:   utf8.RuneCountInString(st.Note),
		NoteCapChars:    chatBodyMaxChars,
		StartedTS:       st.StartedTS,
		FinishedTS:      st.FinishedTS,
	}
}

// newTaskDTO projects one task + its steps/deps onto the wire: task_no and
// the leaf progress derive here; closed_ts serialises null while open.
// cardStatus carries each bound card's live status for reply_card_status (nil
// when there are no steps to enrich — e.g. the create result).
func newTaskDTO(t Task, steps []TaskStep, deps []string, cardStatus map[string]string) taskDTO {
	if deps == nil {
		deps = []string{}
	}
	stepDTOs := []taskStepDTO{}
	for _, st := range steps {
		stepDTOs = append(stepDTOs, newTaskStepDTO(st, cardStatus))
	}
	done, total := TaskProgress(steps)
	inputs := t.Inputs
	if inputs == nil {
		inputs = map[string]any{}
	}
	dto := taskDTO{
		ID:                 t.ID,
		TaskNo:             TaskNo(t.ID),
		TypeKey:            t.TypeKey,
		Title:              t.Title,
		DedupeKey:          t.DedupeKey,
		Inputs:             inputs,
		Description:        t.Description,
		DuplicateOf:        t.DuplicateOf,
		Status:             t.Status,
		Lock:               t.Lock,
		Priority:           t.Priority,
		ExecutorKind:       t.ExecutorKind,
		ExecutorID:         t.ExecutorID,
		CreatorID:          t.CreatorID,
		ReassignedFrom:     t.ReassignedFrom,
		ReassignedFromKind: t.ReassignedFromKind,
		HandoverNote:       t.HandoverNote,
		HandoverNoteTS:     t.HandoverNoteTS,
		HandoverNoteBy:     t.HandoverNoteBy,
		WaitingReason:      t.WaitingReason,
		CreatedTS:          t.CreatedTS,
		UpdatedTS:          t.UpdatedTS,
		Deps:               deps,
		Steps:              stepDTOs,
		// T-66: every exit built through here says what it is. Nine responses
		// share this builder (get_task, terminate, reassign, claim, duplicate,
		// deps, the create dedupe hit, description, title), so the declaration
		// lands on all of them at once — which is the point of doing the
		// slimming HERE rather than in each handler.
		DetailLevel:   taskDetailLevelSummary,
		NotesIncluded: false,
		// T-66 / owner c-cd063427fb2f: the artifact rows are an INDEX on every
		// one of those same nine responses. EXECUTOR JUDGEMENT, not an owner
		// ruling: the owner said what the default payload should carry, not
		// which layer should do the slimming. It is done HERE, on the shared
		// builder, for the same reason the step note was — a per-handler
		// projection is nine copies of one rule, and the copy nobody watches is
		// the one that keeps serving the fat rows.
		ProgressDone:     done,
		ProgressTotal:    total,
		CloseoutReported: t.CloseoutTS > 0,
		// ArtifactCount defaults to 0 — the handler (taskDTOOf) folds the real
		// count in after this pure projection, since counting is a DAL read that
		// does not belong in a pure builder. ⚠️ Unlike the [] this replaces, 0 is
		// a CLAIM rather than an empty container, and it is true of the one
		// caller that skips taskDTOOf: the create result, whose task cannot yet
		// have a deliverable pinned to it.
		// Blocking defaults to [] for the same reason Artifacts does: resolving
		// the reverse edge is a DAL read, and this builder is pure. taskDTOOf
		// folds the real set in; a projection built without it (the create
		// result) honestly says "nobody is waiting", which is true of a task
		// that was born one line ago.
		Blocking:      []taskDepRefDTO{},
		Handoff:       t.Handoff,
		HandoffNote:   t.HandoffNote,
		HandoffTaskID: t.HandoffTaskID,
		FrozenBy:      t.FrozenBy,
	}
	if t.ClosedTS > 0 {
		ts := t.ClosedTS
		dto.ClosedTS = &ts
	}
	return dto
}

// newTaskArtifactDTO projects one artifact row onto the wire. att is the
// resolved chat_attachment for a file/image kind (nil for link, or when the
// referenced blob is gone) — its mime/filename/is_image ride along honest-empty
// when absent, never fabricated. A link's url is the row's own external url; a
// file/image's url is the blob serve path (the chatAttachmentDTO convention).
// versionCount is the retained-version count of THIS artifact plus the live
// row (the caller counts the history rows; the +1 is here so no caller can
// forget it).
func newTaskArtifactDTO(a TaskArtifact, att *ChatAttachment, retained int) taskArtifactDTO {
	dto := taskArtifactDTO{
		VersionCount: retained + 1,
		ID:           a.ID,
		Kind:         a.Kind,
		Description:  a.Description,
		CreatedTS:    a.CreatedTS,
		CreatedBy:    a.CreatedBy,
	}
	if a.Kind == ArtifactKindLink {
		dto.URL = linkTargetOf(att)
	}
	if b, ok := artifactBlobFacts(att); ok && a.Kind != ArtifactKindLink {
		dto.URL, dto.Mime = b.url, b.mime
	}
	if att != nil && a.Kind == ArtifactKindLink {
		dto.Mime = att.Mime
	}
	dto.Name = artifactDisplayName(a, att)
	return dto
}

// linkTargetOf reads a link artifact's target out of its text/uri-list blob.
// Empty when the blob is gone — honest-empty, the same rule the file/image side
// follows, and never the row's own id dressed up as a url.
func linkTargetOf(att *ChatAttachment) string {
	if att == nil {
		return ""
	}
	return strings.TrimRight(string(att.Data), "\r\n")
}

// artifactDisplayName is the read-time derivation that makes taskArtifactDTO.Name
// non-empty (T-92, spec v6 §4.1). Order: the stored name, then the blob's own
// filename for a file/image, then the link target, then "#" + the id without its
// "ta-" prefix.
//
// 🔴 THIS IS A NEW BEHAVIOUR, NOT ONE MOVED FROM SOMEWHERE. Before T-92 the
// server handed out `Label` verbatim and the fallback chain lived in the
// frontend (TaskArtifactsPopover's `a.filename || a.label`). T-92 removes
// `filename` from the wire, so the chain HAS to move here — leave it out and
// every row whose name column is empty, which is nearly every migrated
// file/image row, renders with no name at all.
//
// The derivation is deliberately NOT written back to the column: copying a
// filename into the name would go stale the moment the content is replaced, and
// it would do so silently.
func artifactDisplayName(a TaskArtifact, att *ChatAttachment) string {
	if a.Name != "" {
		return a.Name
	}
	if a.Kind == ArtifactKindLink {
		if t := linkTargetOf(att); t != "" {
			return t
		}
	} else if att != nil && att.Filename != nil && *att.Filename != "" {
		return *att.Filename
	}
	return "#" + strings.TrimPrefix(a.ID, "ta-")
}

// artifactBlobFields is the half of an artifact projection that comes from the
// referenced blob rather than from the row.
type artifactBlobFields struct {
	url      string
	mime     string
	filename string
	isImage  bool
}

// artifactBlobFacts resolves that half: the serve path, the mime, the blob's
// own name and whether it is an image. ok is false for a link kind and for a
// file/image whose blob is gone, and the caller then keeps the row's own values
// — honest-empty, never fabricated.
//
// 🔴 IT IS SHARED BECAUSE THE TWO PROJECTIONS ARE ONE FACT. The live artifact
// and a retained version of it are the same deliverable read at two moments; a
// reader that can open one must be able to open the other. When the version
// side had its own (shorter) answer it served the ROW's url, which for a
// file/image is the empty string by construction — so every file version was
// unreachable and unreadable on the real wire, while both sides' tests passed
// against fixtures that carried a url of their own.
// The artifact-kind test deliberately lives at each CALL SITE rather than in
// here. The identity scanners (authz_surface_gate_test's mentionsIdentity and
// lifecycle_identity_gate_t170e) recognise a `.Kind` SELECTOR inside a
// comparison and are blind to a bare `kind` ident, so folding the predicate
// into this helper deleted it from both ledgers with nothing going red — the
// exact reshape this package's own gate header forbids. Visibility to the
// scanners beats saving the repeated line.
func artifactBlobFacts(att *ChatAttachment) (artifactBlobFields, bool) {
	if att == nil {
		return artifactBlobFields{}, false
	}
	b := artifactBlobFields{
		url:     "/api/chat/attachment/" + att.ID,
		mime:    att.Mime,
		isImage: len(att.Mime) >= 6 && att.Mime[:6] == "image/",
	}
	if att.Filename != nil {
		b.filename = *att.Filename
	}
	return b, true
}

// newTaskListItemDTO projects one task + its deps + pre-counted step progress
// + its pre-resolved current step onto the LIGHT list wire (GET /api/tasks).
// done/total come from dal.AllTaskStepProgress (a grouped COUNT) and current
// from dal.AllTaskCurrentStep (one grouped window query, id/name only), so the
// list still never loads step rows;
// closed_ts serialises null while open, exactly like newTaskDTO.
//
// byID is the caller's map of the WHOLE task population (the handler builds it
// from the single ListTasks read it already does) — it resolves each dep into
// the display facts the card's 「等 <task id>」 row needs. Pass nil ONLY where the
// population is genuinely not in hand; deps then serve as unresolvable entries,
// which the client reads as 查無此任務. There is deliberately no per-dep lookup
// here: this endpoint is the payload/latency hot path, so dep resolution must
// cost zero extra queries (T-a3e4).
func newTaskListItemDTO(
	t Task, deps []string, done, total, artifactCount int, byID map[string]Task,
	current TaskCurrentStep,
) taskListItemDTO {
	if deps == nil {
		deps = []string{}
	}
	dto := taskListItemDTO{
		ArtifactCount:      artifactCount,
		ID:                 t.ID,
		TaskNo:             TaskNo(t.ID),
		TypeKey:            t.TypeKey,
		Title:              t.Title,
		DedupeKey:          t.DedupeKey,
		DuplicateOf:        t.DuplicateOf,
		Status:             t.Status,
		Lock:               t.Lock,
		Priority:           t.Priority,
		ExecutorKind:       t.ExecutorKind,
		ExecutorID:         t.ExecutorID,
		CreatorID:          t.CreatorID,
		ReassignedFrom:     t.ReassignedFrom,
		ReassignedFromKind: t.ReassignedFromKind,
		WaitingReason:      t.WaitingReason,
		CreatedTS:          t.CreatedTS,
		UpdatedTS:          t.UpdatedTS,
		Deps:               deps,
		DepTasks:           newTaskDepRefDTOs(deps, byID),
		ProgressDone:       done,
		ProgressTotal:      total,
		// current comes from dal.AllTaskCurrentStep (one grouped query for the
		// whole population) — its zero value IS "no current step", which is the
		// right answer for an empty or fully-finished plan.
		CurrentStepID:   current.ID,
		CurrentStepName: current.Name,
	}
	if t.ClosedTS > 0 {
		ts := t.ClosedTS
		dto.ClosedTS = &ts
	}
	return dto
}

// newTaskDepRefDTOs resolves each dep id against an already-loaded task
// population. Never nil. A dep missing from byID still carries its TaskNo (it
// is the id, T-5291) and leaves Title/Status "" — the client's 查無此任務 row;
// inventing a status here
// would launder "this task is gone" into "this task has not started".
func newTaskDepRefDTOs(deps []string, byID map[string]Task) []taskDepRefDTO {
	out := make([]taskDepRefDTO, 0, len(deps))
	for _, id := range deps {
		ref := taskDepRefDTO{ID: id, TaskNo: TaskNo(id)}
		if dep, ok := byID[id]; ok {
			ref.Title = dep.Title
			ref.Status = dep.Status
		}
		out = append(out, ref)
	}
	return out
}

// newTaskManualDTO projects one manual row onto the wire (stored JSON blobs
// parsed; a corrupt blob is an error, never a silent empty).
func newTaskManualDTO(m TaskManual, sopCapChars, learningsCapChars int) (taskManualDTO, error) {
	fields, err := ParseManualFields(m.Fields)
	if err != nil {
		return taskManualDTO{}, err
	}
	if fields == nil {
		fields = []ManualField{}
	}
	assignee := map[string]any{}
	if m.Assignee != "" {
		if err := json.Unmarshal([]byte(m.Assignee), &assignee); err != nil {
			return taskManualDTO{}, fmt.Errorf(
				"task_manual %s: bad assignee JSON: %w", m.TypeKey, err)
		}
	}
	return taskManualDTO{
		LearningsChars:    utf8.RuneCountInString(m.Learnings),
		SopMDChars:        utf8.RuneCountInString(m.SopMD),
		LearningsCapChars: learningsCapChars,
		SopMDCapChars:     sopCapChars,
		CapChars:          learningsCapChars,
		TypeKey:           m.TypeKey,
		DisplayName:       m.DisplayName,
		Purpose:           m.Purpose,
		Fields:            fields,
		SopMD:             m.SopMD,
		Learnings:         m.Learnings,
		Assignee:          assignee,
		UpdatedTS:         m.UpdatedTS,
	}, nil
}

// newTaskManualListItemDTO is the ONLY projection GET /api/task-manuals serves:
// the type identity the 類型 filter reads (type_key / display_name / purpose +
// updated_ts), the input fields, the assignee setting, and the SIZES + caps of
// the two long documents it omits.
//
// fields and assignee are the REAL parsed values, not the honest-empty
// placeholders the old ?view=list row carried: they are small, bounded, and
// they are what a caller choosing or dispatching a type actually needs, so
// blanking them only forced a second round trip per row. That is why this
// parses the stored JSON blobs and, like newTaskManualDTO, fails loudly on a
// corrupt one rather than answering with a silent empty.
//
// The sizes are measured on the STORED row, not on the omitted wire fields: a
// zero that looks like a measurement is worse than the omission it describes.
func newTaskManualListItemDTO(m TaskManual, sopCapChars, learningsCapChars int) (taskManualListItemDTO, error) {
	fields, err := ParseManualFields(m.Fields)
	if err != nil {
		return taskManualListItemDTO{}, err
	}
	if fields == nil {
		fields = []ManualField{}
	}
	assignee := map[string]any{}
	if m.Assignee != "" {
		if err := json.Unmarshal([]byte(m.Assignee), &assignee); err != nil {
			return taskManualListItemDTO{}, fmt.Errorf(
				"task_manual %s: bad assignee JSON: %w", m.TypeKey, err)
		}
	}
	return taskManualListItemDTO{
		LearningsChars:    utf8.RuneCountInString(m.Learnings),
		SopMDChars:        utf8.RuneCountInString(m.SopMD),
		LearningsCapChars: learningsCapChars,
		SopMDCapChars:     sopCapChars,
		CapChars:          learningsCapChars,
		TypeKey:           m.TypeKey,
		DisplayName:       m.DisplayName,
		Purpose:           m.Purpose,
		Fields:            fields,
		Assignee:          assignee,
		UpdatedTS:         m.UpdatedTS,
	}, nil
}

// actorRuntimeFold carries the per-actor telemetry/gauge runtime facts BOTH
// read paths serve — the member monitoring-session row (api_monitoring.go) and
// the outsource-worker DTO below (P7b read-path convergence: one fold, two
// wires). account is the RAW telemetry key ("" when unreported): each caller
// applies its own display resolution and serialisation on top (session row →
// resolved string, worker DTO → nullable resolved pointer), so neither wire
// shape changes. cost / contextPct / bankedCost are nil when unreported /
// zero → serialise null → the panel's honest dash, never a fabricated value.
type actorRuntimeFold struct {
	account         string
	cost            *float64
	contextPct      *float64
	compactionCount *int
	bankedCost      *float64
}

// foldActorRuntime folds one actor's telemetry entry, gauge entry, and durable
// banked cost. Nil-map-safe: an actor with no entries folds all-empty. Account
// provenance is checked through the shared telemetryAccount accessor.
func foldActorRuntime(tele, gauge map[string]any, banked float64, actorRuntime string) actorRuntimeFold {
	f := actorRuntimeFold{}
	f.account = telemetryAccount(tele, actorRuntime)
	if c, ok := tele["cost"].(float64); ok {
		f.cost = &c
	}
	if pct, ok := gauge["context_pct"].(float64); ok {
		f.contextPct = &pct
	}
	if count, ok := gauge["compaction_count"].(int); ok && count >= 0 {
		f.compactionCount = &count
	}
	if banked != 0 {
		b := banked
		f.bankedCost = &b
	}
	return f
}

// newOutsourceWorkerDTO projects one worker + its bound task onto the panel
// wire (nil task = honest empty title/status; the row still lists). unread is
// the caller's watermark-inverse count for this worker's conversation (the
// handler computes it with the same UnreadCounts fold the member roster uses).
func newOutsourceWorkerDTO(w OutsourceWorker, task *Task, p outsourceWorkerProjection) outsourceWorkerDTO {
	dto := outsourceWorkerDTO{
		ID:            w.ID,
		AvatarURL:     memberAvatarURL(w.AvatarAttachmentID),
		Codename:      w.Codename,
		Runtime:       NormalizeRuntime(w.Runtime),
		Model:         w.Model,
		Effort:        w.Effort,
		ActualModel:   w.ActualModel,
		ActualRuntime: w.ActualRuntime,
		ActualEffort:  w.ActualEffort,
		ActualMachine: w.LastMachineID,
		Status:        w.Status,
		TaskID:        w.TaskID,
		CreatedTS:     w.CreatedTS,
		UnreadCount:   p.unread,
		Presence:      workerPresence(w, p.now, p.online),
		// Machine = the worker's OBSERVED host (dispatch target, or the
		// restart-proof fallback folded upstream in projectWorker — T-c23a)
		// resolved to a display label; "" when nothing is observed — the panel
		// renders 「尚未分配」.
		DesiredMachineID: w.DesiredMachineID,
		LastOp:           w.LastOp,
		LastOpOK:         w.LastOpOK,
		LastOpLog:        w.LastOpLog,
		LastOpReason:     w.LastOpReason,
		LastOpAt:         w.LastOpAt,
		DelegatedBy:      p.delegatedBy,
	}
	if p.spawnTarget != "" && p.machineDisplay != nil {
		dto.Machine = p.machineDisplay(p.spawnTarget)
	}
	// Runtime facts fold from the worker's OWN telemetry/gauge entry (keyed by
	// actor id) via the SAME foldActorRuntime the member session loop reads.
	// Absent → nil → serialises null → honest dash, never fabricated (parity
	// with the member fold's `awake && … || dash` gate).
	rt := foldActorRuntime(p.tele, p.gaugeEntry, w.BankedCost, w.Runtime)
	dto.Cost = rt.cost
	dto.ContextPct = rt.contextPct
	dto.CompactionCount = rt.compactionCount
	dto.CompactionCount = rt.compactionCount
	dto.BankedCost = rt.bankedCost
	// Account serves the RESOLVED readable name only (owner alias → owner-
	// gated reported label). No readable name → null → the panel's dash;
	// the raw key itself never reaches the wire (T-ba6b — the panel used
	// to render credential hashes verbatim).
	if rt.account != "" && p.accountDisplay != nil {
		if display := p.accountDisplay(rt.account); display != "" {
			dto.Account = &display
		}
	}
	if task != nil {
		dto.TaskTitle = task.Title
		dto.TaskStatus = task.Status
		dto.CreatorID = task.CreatorID
		// T-a3e4: the panel's sort key + row labels ride the worker DTO now.
		// They used to be a client-side join against the UNFILTERED
		// GET /api/tasks — the whole task history downloaded on every worker /
		// chat delta to order and label a handful of rows. Honest zero/"" when
		// the task cannot be resolved (task == nil): the client then falls back
		// to the worker's own mint stamp for ordering and prints 自由代辦.
		dto.TaskNo = TaskNo(task.ID)
		dto.TaskCreatedTS = task.CreatedTS
		dto.TaskTypeKey = task.TypeKey
		if p.typeDisplay != nil {
			dto.TaskTypeName = p.typeDisplay(task.TypeKey)
		}
	}
	// refocus_since passes through as epoch seconds (0.0 = unset; the FE maps 0→null
	// so the panel never renders a fabricated time); desired_state echoes the run
	// intent ("" from a pre-column/never-set row reads as online client-side).
	dto.RefocusSince = w.RefocusSince
	dto.RefocusOp = w.RefocusOp
	// The grace the tick ACTUALLY collects this epoch on, and 0 when nothing
	// collects it on a clock at all (owner 2026-08-19: 重新聚焦 runs no clock for
	// outsource workers either — the cockpit maps 0 → null → renders nothing).
	// Reading StoppingTimeoutSecs straight reported a ceiling for every op,
	// including the one arm that is not on a clock.
	//
	// 🔴 winddownDeadlineOf, THE SAME FUNCTION MemberDTO READS (api_helpers.go),
	// on the SAME projection workerPresence already goes through. It used to be
	// refocusDeadlineOf(w.RefocusSince, …) — only the 換手 axis — so the 下線
	// axis, whose clock anchors on stopping_since, reported 0 for an epoch the
	// tick DOES collect: the stop arm of runOutsourceTick fires at
	// StoppingSince+grace on recycleGraceFor + gracefulStopEpochOpen — which is
	// what winddownDeadlineOf evaluates — plus that site's own StoppedSince<=0
	// term, and a session already confirmed gone collects EARLIER still. Both
	// only make the tick stricter than this ceiling, which is why a ceiling is
	// the honest word for it. An owner pressing 加速停止 on
	// a worker started a countdown that neither the cockpit nor the agent's own
	// notice was told about; staff never had the gap because they always read
	// the two-axis expression. Do NOT re-inline the 下線 arm here — one
	// expression, two callers, is the whole point.
	dto.RefocusDeadline = winddownDeadlineOf(memberFromWorker(w), p.cfg)
	dto.DesiredState = w.DesiredState
	return dto
}

// workerPresence answers 「喚醒中／上線中／停止中…」 for an outsource worker by
// calling PresenceState — the SAME function the staff roster calls, on the SAME
// row (memberFromWorker is the projection, not a copy of the rules). T-14: this
// used to assemble its own livenessInput from the in-memory spawn anchor, which
// is how the two kinds came to answer 「喚醒中」 differently — a re-exec forgot
// that map, so a long-lived worker mid-wake fell to 「離線」.
//
// What is left here is the ONE thing that is genuinely outsource-only and is not
// a presence rule at all: a released worker is off-panel, so it has no presence
// word rather than a wrong one. Everything below that line is the member's.
//
// PURE function of the row + wall clock + the caller-supplied SSE-presence fact
// (online == hub.IsOnline(w.ID) — the SAME single online authority PresenceState
// reads for members; a worker holds its SSE via `ocagent listen`, so a
// died-after-claim session flips offline exactly like a member's would).
func workerPresence(w OutsourceWorker, now float64, online bool) string {
	if w.Status == WorkerStatusReleased {
		return "" // released / off-panel — never a live row
	}
	return PresenceState(memberFromWorker(w), now, online)
}

// ── builders ─────────────────────────────────────────────────────────────────

// attachmentDTOsFromRefs builds served attachment views from light
// [{id, mime, filename}] refs — the single message→blob / answer→blob
// projection (chat meta["attachments"] and reply_card answer_attachments
// share the ref shape and the blob store).
func attachmentDTOsFromRefs(refs []any) []chatAttachmentDTO {
	attachments := []chatAttachmentDTO{}
	for _, r := range refs {
		ref, _ := r.(map[string]any)
		id, _ := ref["id"].(string)
		if id == "" {
			continue // never fabricate a serve URL for a ref with no id
		}
		mime, _ := ref["mime"].(string)
		filename, _ := ref["filename"].(string)
		attachments = append(attachments, chatAttachmentDTO{
			ID:       id,
			URL:      "/api/chat/attachment/" + id,
			Filename: filename,
			Mime:     mime,
			IsImage:  len(mime) >= 6 && mime[:6] == "image/",
		})
	}
	return attachments
}

// newChatMessageDTO builds the served chat-message view from a stored row —
// attachments derived entirely from the light meta["attachments"] refs
// (ChatMessageDTO.from_domain).
func newChatMessageDTO(m ChatMessage) chatMessageDTO {
	meta := m.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	refs, _ := meta["attachments"].([]any)
	attachments := attachmentDTOsFromRefs(refs)
	return chatMessageDTO{
		ID:          m.ID,
		From:        m.Sender,
		To:          m.Recipient,
		Body:        m.Body,
		TS:          m.TS,
		Meta:        meta,
		Attachments: attachments,
		ReplyTo:     replyToFromMeta(meta),
	}
}

// replyToFromMeta returns the id of the message this one replies to ("" when
// it replies to nothing). It reads the same open meta map every other client
// can write to, so it is a READ of a value only the POST handler is allowed to
// put there: HandlePostChatApiChatPost deletes any caller-supplied reply_to
// before validation and writes its own. Without that deletion this getter
// would happily serve a forged link — the meta map is copied through wholesale.
func replyToFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	id, _ := meta[chatReplyToMetaKey].(string)
	return id
}

// replyCardIDFromMeta returns the reply_card_id a chat message carries in its
// open meta ("" when the message carries no card).
func replyCardIDFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	id, _ := meta["reply_card_id"].(string)
	return id
}

// newReplyCardDTO projects one reply card onto the wire: answered_ts / answer
// serialise as null unless answered; expired_ts as null unless expired.
func newReplyCardDTO(c ReplyCard) replyCardDTO {
	options := c.Options
	if options == nil {
		options = []ReplyCardOption{}
	}
	selectMode := c.SelectMode
	if selectMode == "" {
		selectMode = replyCardSelectModeSingle
	}
	dto := replyCardDTO{
		ID:            c.ID,
		From:          c.FromMember,
		Kind:          c.Kind,
		Summary:       c.Summary,
		Body:          c.Body,
		Options:       options,
		SelectMode:    selectMode,
		Status:        c.Status,
		CreatedTS:     c.CreatedTS,
		Attachments:   attachmentDTOsFromRefs(c.Attachments),
		ChatMessageID: c.ChatMessageID,
	}
	if c.Status == replyCardStatusExpired {
		ts := c.ExpiredTS
		dto.ExpiredTS = &ts
	}
	if c.Status == replyCardStatusAnswered {
		ts := c.AnsweredTS
		dto.AnsweredTS = &ts
		dto.Answer = &replyCardAnswerDTO{
			OptionIdxs:  c.AnswerOptionIdxs,
			Text:        c.AnswerText,
			Attachments: attachmentDTOsFromRefs(c.AnswerAttachments),
		}
	}
	return dto
}

// webhookEndpointDTO is the response shape for one webhook_endpoint (M4 回呼端點,
// §1). The `token` is the opaque secret — it rides this authenticated wire (the
// panel renders the callback URL from it, masking the token visually while the
// copy button yields the full URL). It is NEVER on any PUBLIC wire.
//
// ⚠️ This comment used to end "It is NEVER on any public or agent-facing wire",
// and on the machine floor that half was FALSE: the four webhook CRUD rows were
// requires=machine, so any agent token read this DTO — token and all — straight
// off the REST wire. MCPExclude only kept the rows out of the MCP tool list; it
// is a discoverability flag, never an authz gate. T-5336 (owner 2026-07-27)
// raised all four rows to requires=admin_agent, which is what now keeps a plain
// agent off this DTO. The claim is enforced by the route table, NOT by this
// comment — see the T-5336 note in routes.go and routes_t5336_webhook_authz_test.go.
// Platform is the fixed verification preset (generic/slack/github).
// HasSigningSecret exposes ONLY whether a secret is configured — the secret
// itself is NEVER echoed on any wire (stricter than token, which the
// owner-facing panel still receives).
// The observability tail (last_received_ts / delivered_count / dropped_count /
// last_drop_reason, migrations/00014) is spec-optional but always emitted:
// last_received_ts==0 means "never received"; last_drop_reason=="" means "never
// dropped". These counters ride ONLY this owner-facing wire — the public /in
// response never reflects them (防探測 invariant).
type webhookEndpointDTO struct {
	EndpointID       string  `json:"endpoint_id"`
	Purpose          string  `json:"purpose"`
	Status           string  `json:"status"`
	CreatedTS        float64 `json:"created_ts"`
	Token            string  `json:"token"`
	Platform         string  `json:"platform"`
	HasSigningSecret bool    `json:"has_signing_secret"`
	LastReceivedTS   float64 `json:"last_received_ts"`
	DeliveredCount   int64   `json:"delivered_count"`
	DroppedCount     int64   `json:"dropped_count"`
	LastDropReason   string  `json:"last_drop_reason"`
}

// scheduledMessageDTO is the response shape for one scheduled_message (T-f059
// 定期訊息). Unlike its webhook twin it carries NO secret: the trigger is a clock,
// not a bearer token, so nothing here is a credential. The admin_agent floor on
// the four routes is for consistency with the neighbouring CRUD, not because a
// secret rides this wire.
//
// last_fired_slot is the delivery CURSOR — the identifier of the slot already
// sent, not a clock reading. It is on the wire for callers that need to reason
// about the cursor itself (it is the only field that answers "has this slot gone
// out?"), NOT for display: the cockpit card renders last_fired_ts beside it as a
// human-readable last-delivered line, because `2026-08-10T09:00+08:00` answers a
// question a person is not asking.
type scheduledMessageDTO struct {
	ID         string `json:"id"`
	MemberID   string `json:"member_id"`
	Label      string `json:"label"`
	Body       string `json:"body"`
	Cadence    string `json:"cadence"`
	DayOfWeek  int    `json:"day_of_week"`
	DayOfMonth int    `json:"day_of_month"`
	Hour       int    `json:"hour"`
	Minute     int    `json:"minute"`
	// The four `custom` sets (T-49e7). ALWAYS emitted, as an honest-empty
	// array for every other cadence — never omitted. A field that appears only
	// sometimes forces every reader to distinguish "this schedule has no set"
	// from "this server does not know about sets", and those are two different
	// answers to two different questions.
	//
	// 🔴 custom_months is emitted the same way even though the REQUEST side lets
	// it be omitted. The two asymmetries are not in tension: on the way IN, an
	// absent field is how a caller says "every month"; on the way OUT there is
	// nothing to be coy about, because the row always lists its months (the
	// handler resolved the omission, migrations/00053 backfilled the rest). A
	// reader therefore never has to infer "all twelve" from an absence.
	CustomMonths  []int   `json:"custom_months"`
	CustomDays    []int   `json:"custom_days"`
	CustomHours   []int   `json:"custom_hours"`
	CustomMinutes []int   `json:"custom_minutes"`
	Timezone      string  `json:"timezone"`
	Status        string  `json:"status"`
	LastFiredSlot string  `json:"last_fired_slot"`
	LastFiredTS   float64 `json:"last_fired_ts"`
	CreatedTS     float64 `json:"created_ts"`
}

func newScheduledMessageDTO(m ScheduledMessage) scheduledMessageDTO {
	return scheduledMessageDTO{
		ID:            m.ID,
		MemberID:      m.MemberID,
		Label:         m.Label,
		Body:          m.Body,
		Cadence:       m.Cadence,
		DayOfWeek:     m.DayOfWeek,
		DayOfMonth:    m.DayOfMonth,
		Hour:          m.Hour,
		Minute:        m.Minute,
		CustomMonths:  intSetOrEmpty(m.CustomMonths),
		CustomDays:    intSetOrEmpty(m.CustomDays),
		CustomHours:   intSetOrEmpty(m.CustomHours),
		CustomMinutes: intSetOrEmpty(m.CustomMinutes),
		Timezone:      m.Timezone,
		Status:        m.Status,
		LastFiredSlot: m.LastFiredSlot,
		LastFiredTS:   m.LastFiredTS,
		CreatedTS:     m.CreatedTS,
	}
}

// intSetOrEmpty renders a set on the wire in sorted, deduplicated form and
// never as JSON null: a nil []int would serialise to `null`, and this feature's
// three set fields mean "no values", which is `[]`.
func intSetOrEmpty(vals []int) []int {
	sorted := sortedIntSet(vals)
	if sorted == nil {
		return []int{}
	}
	return sorted
}

// webhookRequestLogDTO is one row of an endpoint's /in debug ring buffer
// (GET .../webhooks/{endpoint_id}/requests, newest→oldest, ≤5 rows). headers
// is the JSON-serialised request header map (≤4 KiB), body the raw payload
// text (≤16 KiB); truncated marks that either was cut. Owner-only wire —
// raw external payloads never reach any agent-facing surface.
type webhookRequestLogDTO struct {
	TS        float64 `json:"ts"`
	Outcome   string  `json:"outcome"`
	Headers   string  `json:"headers"`
	Body      string  `json:"body"`
	Truncated bool    `json:"truncated"`
}

func newWebhookRequestLogDTO(l WebhookRequestLog) webhookRequestLogDTO {
	return webhookRequestLogDTO{
		TS:        l.TS,
		Outcome:   l.Outcome,
		Headers:   l.Headers,
		Body:      l.Body,
		Truncated: l.Truncated,
	}
}

func newWebhookEndpointDTO(e WebhookEndpoint) webhookEndpointDTO {
	platform := e.Platform
	if platform == "" {
		platform = WebhookPlatformGeneric
	}
	return webhookEndpointDTO{
		EndpointID:       e.EndpointID,
		Purpose:          e.Purpose,
		Status:           e.Status,
		CreatedTS:        e.CreatedTS,
		Token:            e.Token,
		Platform:         platform,
		HasSigningSecret: e.SigningSecret != "",
		LastReceivedTS:   e.LastReceivedTS,
		DeliveredCount:   e.DeliveredCount,
		DroppedCount:     e.DroppedCount,
		LastDropReason:   e.LastDropReason,
	}
}
