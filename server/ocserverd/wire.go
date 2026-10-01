package main

// wire.go — hand-written response DTOs. Not the generated ocapi_gen.go types:
// those carry `omitempty` on every optional field, while this wire serialises
// every declared field (null, never omitted) and conformance checks exact keys
// (e.g. bootstrap preview's `token: null`). The generated types remain the
// request-body vocabulary.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// owner_id is no longer stored (single tenant); the frozen wire still carries this constant.
const wireOwnerID = "owner"

// Synthetic sender of server-authored task messages (reassign handover
// notices), so they are not attributed to the owner. A non-roster id; the FE
// resolves it to the localized 「系統」 label (ChatArea nameOf).
const wireSystemSender = "system"

// schema_version is no longer stored per row; the frozen wire still carries this constant.
const wireSchemaVersion = 3

// MFARequired has no omitempty on purpose: the schema marks it optional, but
// this server must say `false` rather than leave the login wall to guess.
type authStatusDTO struct {
	PasswordSet bool `json:"password_set"`
	MFARequired bool `json:"mfa_required"`
}

// Secret / OtpauthURI: null = no pending secret to show. Only enroll fills them,
// and only for a pending secret — an active secret is never echoed, so a stolen
// owner token cannot clone an existing enrolment.
type mfaStateDTO struct {
	// Offered is the ship-dark flag (may the factor be SET UP); whether one is
	// armed is Enrolled. Never consulted when verifying a code.
	Offered    bool    `json:"offered"`
	Enrolled   bool    `json:"enrolled"`
	Secret     *string `json:"secret"`
	OtpauthURI *string `json:"otpauth_uri"`
}

type settingsDTO struct {
	OwnerTokenTTL int64 `json:"owner_token_ttl"`
	AgentTokenTTL int64 `json:"agent_token_ttl"`

	HandoverPct              int `json:"handover_pct"`
	NoticePct                int `json:"notice_pct"`
	CodexCompactionThreshold int `json:"codex_compaction_threshold"`
	CodexNoticeRound         int `json:"codex_notice_round"`
	MonitoringRefreshSeconds int `json:"monitoring_refresh_seconds"`
	OutsourceMaxParallel     int `json:"outsource_max_parallel"`
	// AcceleratedGraceSecs is ONE number on purpose: every clocked cause reads it
	// through recycleGraceFor, so the agent notice's countdown and the reconcile
	// tick's deadline are the same value. It says HOW LONG; winddownKindFor still
	// decides WHO is clocked.
	AcceleratedGraceSecs int `json:"accelerated_grace_secs"`

	ReassignHandoverTimeoutSecs int `json:"reassign_handover_timeout_secs"`

	RuntimeLoginCheckIntervalSecs   int `json:"runtime_login_check_interval_secs"`
	RuntimeLoginRecheckIntervalSecs int `json:"runtime_login_recheck_interval_secs"`

	// WardenCredentialLifetimeSecs drives both the warden's renewal age (two thirds
	// of it) and the minted credential's exp (api_auth.go mintWardenToken).
	// 🔴 A change here can take a machine off the network: one that does not renew
	// within the remaining third is refused and needs a hand re-install, and
	// nothing reports it.
	WardenCredentialLifetimeSecs int `json:"warden_credential_lifetime_secs"`
	// DocCapChars* are caps in CHARACTERS (runes), the unit the patch receipts and
	// the refusal message use.
	DocCapCharsDuty      int `json:"doc_cap_chars_duty"`
	DocCapCharsInsight   int `json:"doc_cap_chars_insight"`
	DocCapCharsManualSop int `json:"doc_cap_chars_manual_sop"`
	// The boot-sequence cap is shared by the claude and codex documents, each
	// measured on its own text.
	DocCapCharsSystemInteraction int `json:"doc_cap_chars_system_interaction"`
	DocCapCharsBootSequence      int `json:"doc_cap_chars_boot_sequence"`
	DocCapCharsOffboard          int `json:"doc_cap_chars_offboard"`
	// ChatBudgetChars (below) is NOT a doc cap: it bounds the wake snapshot's chat
	// block, repacked on every read, so it may be lowered as well as raised; its
	// ceiling is its own (tied to resumeChatFetch, domain.go).
	// Lore: two FOLD budgets (a role's staff boot document, get_task_manual) and two
	// ENTRY bounds (one write's title / body). 🔴 The fold budgets are never summed:
	// different readers spend them at different moments.
	LoreCapCharsRole   int `json:"lore_cap_chars_role"`
	LoreCapCharsManual int `json:"lore_cap_chars_manual"`
	LoreCapCharsTitle  int `json:"lore_cap_chars_title"`
	LoreCapCharsBody   int `json:"lore_cap_chars_body"`
	ChatBudgetChars    int `json:"chat_budget_chars"`
	// StepNoteCapChars is enforced only on write (an over-cap note still reads back
	// whole), so it is lowerable. Step note ONLY: the handover note and chat body
	// keep their 4,000-character constant (owner ruling 2026-09-06).
	StepNoteCapChars int `json:"step_note_cap_chars"`
	// BackupRetain counts VERSIONS, not days, and is PER POOL, not per directory.
	BackupRetain int `json:"backup_retain"`

	UpdaterReceiveBeta bool `json:"updater_receive_beta"`
	UpdaterAutoUpdate  bool `json:"updater_auto_update"`

	OrgName string `json:"org_name"`

	OwnerName string `json:"owner_name"`
	// PushContactEmail is the VAPID subject; "" = Web Push is not delivered at all.
	PushContactEmail string `json:"push_contact_email"`
	// DisplayTheme / DisplayLanguage: "" = never set — the frontend keeps its
	// localStorage cache / default and reconciles this value at login.
	DisplayTheme string `json:"display_theme"`

	DisplayLanguage string `json:"display_language"`

	DisplayWide bool `json:"display_wide"`
	// 🔴 SuggestedReplies* are never null on the wire (the spec types them as
	// array); settingsView normalizes them to [].
	SuggestedRepliesReplyCard   []string `json:"suggested_replies_reply_card"`
	SuggestedRepliesTaskMessage []string `json:"suggested_replies_task_message"`
	SuggestedRepliesLoreMessage []string `json:"suggested_replies_lore_message"`
	// Onboarding rides the OWNER-GATED settings read on purpose: a failed step's
	// Detail carries the raw `ocwarden install` log (local paths), so it must never
	// reach the public /api/auth/status probe. nil = onboarding never ran.
	Onboarding *onboardingReportDTO `json:"onboarding"`
}

type onboardingStepDTO struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Code is the closed failure vocabulary (onboarding.go onboardingCode*) the
	// cockpit writes the owner's sentence from; Reason is the English fallback for
	// a code the client does not know.
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

type onboardingReportDTO struct {
	State      string              `json:"state"`
	StartedAt  float64             `json:"started_at"`
	FinishedAt float64             `json:"finished_at"`
	Steps      []onboardingStepDTO `json:"steps"`
	// DismissedAt: unix seconds, 0 = never dismissed. Stored on the report, not in
	// the browser (a per-tab dismissal came back on the next tab). Rows written
	// before this field have no migration and read as 0, keeping the warning visible.
	DismissedAt float64 `json:"dismissed_at"`
}

// themeFetchResultDTO is the RAW response text on purpose: the cockpit parses it
// with the same parseImportedBundle a pasted bundle goes through, so a theme is
// parsed in exactly one place.
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
	// ForcedStopAt is deliberately NOT cleared by the next boot: it records that the
	// PREVIOUS session was cut off mid-work.
	ForcedStopAt  float64 `json:"forced_stop_at"`
	UnreadCount   int     `json:"unread_count"`
	RosterStatus  string  `json:"roster_status"`
	OwnerID       string  `json:"owner_id"`
	SchemaVersion int     `json:"schema_version"`
	// TerminalAttachCommand is the whole command composed server-side
	// (terminal_attach.go): the socket half is namespace-dependent, and a client
	// assembling its own attaches to another station's tmux server.
	TerminalAttachCommand string   `json:"terminal_attach_command"`
	CreatedTS             float64  `json:"created_ts,omitempty"`
	Status                string   `json:"status,omitempty"`
	TaskID                string   `json:"task_id,omitempty"`
	TaskTitle             string   `json:"task_title,omitempty"`
	TaskStatus            string   `json:"task_status,omitempty"`
	TaskNo                string   `json:"task_no,omitempty"`
	TaskCreatedTS         float64  `json:"task_created_ts,omitempty"`
	TaskTypeKey           string   `json:"task_type_key,omitempty"`
	TaskTypeName          string   `json:"task_type_name,omitempty"`
	Account               *string  `json:"account,omitempty"`
	ContextPct            *float64 `json:"context_pct,omitempty"`
	CompactionCount       *int     `json:"compaction_count,omitempty"`
	Cost                  *float64 `json:"cost,omitempty"`
	BankedCost            *float64 `json:"banked_cost,omitempty"`
	CreatorID             string   `json:"creator_id,omitempty"`
	DelegatedBy           string   `json:"delegated_by,omitempty"`

	RuntimeLoginWarnings []RuntimeLoginWarningDTO `json:"runtime_login_warnings"`
}

type machineDTO struct {
	MachineID   string `json:"machine_id"`
	DisplayName string `json:"display_name"`
	Online      bool   `json:"online"`
	IsSelf      bool   `json:"is_self"`

	BinStatus *string `json:"bin_status"`

	ClaudeVersion       *string                         `json:"claude_version"`
	ClaudeCredSource    *string                         `json:"claude_cred_source"`
	ClaudeSubReadable   *bool                           `json:"claude_sub_readable"`
	RuntimeCapabilities map[string]RuntimeCapabilityDTO `json:"runtime_capabilities"`
	// WardenShape is REPORTED by the warden, verbatim, and must never be computed
	// here: only the reporting process can read its own parent. nil (a build older
	// than the anchor cutover) is a different fact from a reported "unknown";
	// neither is turned into the other.
	WardenShape *string `json:"warden_shape"`
	// CutoverEffect: is the cutover in effect for the processes that CARRY agents.
	// WardenShape cannot answer that — agents hang off a tmux server that keeps its
	// original identity across a warden restart. 🔴 "unproven" is never a shade of
	// "effective"; do not collapse it into green. nil = not reported.
	CutoverEffect *string `json:"cutover_effect"`
	// 🔴 TokenKeyID is an observation THIS STATION made — the key whose HMAC verified
	// the machine's credential at the auth gate (member.token_key_id) — never
	// something a machine can assert: it gates the owner's immediate, no-grace
	// 「移除」 of a retired signing key. nil = never verified. TokenKeyCurrent is
	// compared against the live ring's signing key here so the active key id need
	// not be on the wire; nil exactly when TokenKeyID is nil.
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

type machineClaimResultDTO struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	MachineID string `json:"machine_id"`
}

// machineCredentialPolicyDTO has one field on purpose: the warden owns the whole
// renewal rule (two thirds of this); serving the derived age here would split
// one rule between station and warden with nothing checking they agree.
type machineCredentialPolicyDTO struct {
	LifetimeSecs int `json:"lifetime_secs"`
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

type machineUpgradeResultDTO struct {
	MemberID   string `json:"member_id"`
	MachineID  string `json:"machine_id"`
	Dispatched bool   `json:"dispatched"`
}

type runtimeLoginAccountDTO struct {
	Email   *string `json:"email"`
	OrgName *string `json:"org_name"`
}

type runtimeLoginDTO struct {
	LoginID   string                  `json:"login_id"`
	MachineID string                  `json:"machine_id"`
	Runtime   string                  `json:"runtime"`
	State     string                  `json:"state"`
	AuthURL   *string                 `json:"auth_url"`
	Account   *runtimeLoginAccountDTO `json:"account"`
	Reason    *string                 `json:"reason"`
	UpdatedTS float64                 `json:"updated_ts"`
}

type chatAttachmentDTO struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	IsImage  bool   `json:"is_image"`
}

type chatAttachmentUploadDTO struct {
	ID       string `json:"id"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
}

// chatInlineReplyCardDTO is the reply card folded onto the message that opened
// it. DECISION only: summary / body / kind / attachments are deliberately absent
// (the message carries the ask; get_reply_card serves the rest).
type chatInlineReplyCardDTO struct {
	Options []ReplyCardOption `json:"options"`
	// AnswerOptionIdxs is one of the AI's two read paths for an answer (the other
	// is the ocagent line): a card answered with two options must show both here.
	AnswerOptionIdxs []int   `json:"answer_option_idxs"`
	AnswerText       string  `json:"answer_text"`
	AnsweredTS       float64 `json:"answered_ts"`

	AnsweredAtDisplay string `json:"answered_at_display"`
}

type chatMessageDTO struct {
	ID   string `json:"id"`
	From string `json:"from"`
	// FromName / ToName are display names BESIDE the ids, never instead of them:
	// From/To stay the address a reply is sent to. "" when unresolved — never
	// fabricated.
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`
	Body     string `json:"body"`
	// 🔴 BodyOmittedChars = runes folded out of THIS message, which IS in the
	// payload (get_chat re-reads it). Not resumeSummaryDTO.ChatEarlierOmitted
	// (whole messages ABSENT); never let the two share wording.
	BodyOmittedChars int     `json:"body_omitted_chars"`
	TS               float64 `json:"ts"`
	// TSDisplay: server-local time WITH offset (there is no studio timezone
	// setting). The DATE IS ALWAYS WRITTEN, same-day included, so a waking agent
	// can tell 昨天 from 上週.
	TSDisplay string         `json:"ts_display"`
	Meta      map[string]any `json:"meta"`

	Card *chatInlineReplyCardDTO `json:"card,omitempty"`
	// ReplyCardStatus is a read-time join filled by servedChatMessageDTO; the
	// inline ChatReplyCard lazy-loads answered cards from it.
	ReplyCardStatus string              `json:"reply_card_status"`
	Attachments     []chatAttachmentDTO `json:"attachments"`
	// ReplyTo is stamped once at post time and never rewritten, even after the
	// target is gone. It may point into another conversation (owner ruling
	// 2026-08-21).
	ReplyTo string `json:"reply_to"`
	// 🔴 ReplyToChat is built unconditionally on every read — no "already in this
	// batch" skip, no opt-in flag (owner ruling 2026-08-21). nil with ReplyTo set
	// is the permanent answer "the original is gone"; nothing retries.
	ReplyToChat *chatReplyQuoteDTO `json:"reply_to_chat,omitempty"`
}

// chatReplyQuoteDTO: 🔴 To is the QUOTED message's own recipient, not the peer
// of the thread carrying the reply — a quote can come from another
// conversation (owner ruling 2026-08-21).
type chatReplyQuoteDTO struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`

	Content string `json:"content"`
}

// chatReplyQuoteMaxChars is the quote-line length in runes; the browser cuts
// nothing of its own. 60 is what the deleted browser copy used.
// 🔴 frontend/src/api/mock.ts MOCK_REPLY_QUOTE_MAX_CHARS is a second copy
// (offline preview): change both. mock.reply-to.test.ts reads THIS LINE, so
// keep `chatReplyQuoteMaxChars = <n>` on one line.
const chatReplyQuoteMaxChars = 60

func chatReplyQuoteContent(body string) string {
	oneLine := strings.Join(strings.Fields(body), " ")
	r := []rune(oneLine)
	if len(r) <= chatReplyQuoteMaxChars {
		return oneLine
	}
	return string(r[:chatReplyQuoteMaxChars]) + "…"
}

// names is nil except on the wake snapshot; the names then stay "", matching
// chatMessageDTO's own name fields on the same read.
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

// chatMarkReadReceiptDTO: the watermark is monotonic, so a stale report is a
// silent no-op; Advanced says whether it moved (the same bit gates the SSE
// publish).
type chatMarkReadReceiptDTO struct {
	PeerID     string  `json:"peer_id"`
	LastReadTS float64 `json:"last_read_ts"`
	Advanced   bool    `json:"advanced"`
}

// agentContextReceiptDTO: AgentID is the verified sub the gauge was filed under
// (the body carries no agent_id).
type agentContextReceiptDTO struct {
	AgentID string  `json:"agent_id"`
	TS      float64 `json:"ts"`
}

// agentTelemetryReceiptDTO: Machine is the attribution actually used — the
// token's machine_id claim first, the self-reported machine only for a
// claim-less token — so it can differ from what the reporter named.
type agentTelemetryReceiptDTO struct {
	AgentID string  `json:"agent_id"`
	Machine *string `json:"machine"`
	TS      float64 `json:"ts"`

	LoginCheckIntervalSecs   *int `json:"login_check_interval_secs,omitempty"`
	LoginRecheckIntervalSecs *int `json:"login_recheck_interval_secs,omitempty"`
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

// costResetDTO carries the PRE-reset figures: spend has no per-charge ledger,
// so this response is the last record of what was cleared. A receipt, not an
// undo (owner ruling rc-7dea0deefa63). null = nothing to clear on that half,
// as on monitoringSessionDTO.
type costResetDTO struct {
	MemberID          string   `json:"member_id"`
	ClearedCost       *float64 `json:"cleared_cost"`
	ClearedBankedCost *float64 `json:"cleared_banked_cost"`
}

// accountCostResetDTO: the ACCOUNT's own spend before the write, separate from
// any member's (owner ruling rc-5c5d7c7c6dcd). null = nothing to clear.
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

	BinStatus *string `json:"bin_status"`

	ClaudeVersion       *string                         `json:"claude_version"`
	ClaudeCredSource    *string                         `json:"claude_cred_source"`
	ClaudeSubReadable   *bool                           `json:"claude_sub_readable"`
	RuntimeCapabilities map[string]RuntimeCapabilityDTO `json:"runtime_capabilities"`
	// HardwareTS / HardwareStale: stale samples are nulled; HardwareStale (past
	// telemetryFreshSecs) is the SERVER's verdict so a client never re-derives the
	// window on its own clock — the same rule as RuntimeCapabilitiesStale. nil =
	// never sampled.
	HardwareTS *float64 `json:"hardware_ts"`

	HardwareStale *bool `json:"hardware_stale"`
	// HardwareInvalid: declared hardware keys that arrived with the WRONG VALUE
	// TYPE (sorted; key names only, never the untrusted value). Ingest stores
	// hardware verbatim (owner ruling rc-55861dd893c6: no rejection), and without
	// this a wrong-typed value served null exactly like a never-probed key. Only on
	// this DTO because MonitorPage's hardware cells read this row, not MachineDTO.
	// Coverage: runtimes are type-checked at ingest (a read-side marker could
	// never fire); claude is still unchecked and silent (known, separate ticket).
	HardwareInvalid []string `json:"hardware_invalid"`
	// RuntimeCapabilities* values are deliberately NOT blanked when stale, only
	// marked: they are the only surface explaining a worker parked on
	// machine_unavailable.
	RuntimeCapabilitiesTS    *float64 `json:"runtime_capabilities_ts"`
	RuntimeCapabilitiesStale *bool    `json:"runtime_capabilities_stale"`

	WardenShape *string `json:"warden_shape"`

	CutoverEffect *string `json:"cutover_effect"`
}

type monitoringAccountDTO struct {
	Account string `json:"account"`
	// AccountLabel is OWNER-ONLY (omitted for other callers), gated by the same
	// acctLabels overlay as display_name, and independent of it so an alias does
	// not hide the real identity.
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

	OrgName string `json:"org_name"`
}

// bootDocDTO: SizeChars/CapChars are served because the cap lives on the
// admin-only settings surface. IsDefault = nobody edited this block; HasSeed =
// a factory version exists (reset 404s without it). HasSeed is true in every
// shipped build but must be served: a build without staged seedsdist answers
// false.
type bootDocDTO struct {
	SizeChars int    `json:"size_chars"`
	CapChars  int    `json:"cap_chars"`
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	// Text is the WHOLE stored document, marker line included — what history diffs
	// and SizeChars counts.
	Text string `json:"text"`
	// ReadOnlyHead / Body exist on the READ face only (owner ruling 2026-08-23).
	// 🔴 Body is exactly what BootDocumentReplaceDTO takes back; ReadOnlyHead
	// cannot be sent ("" when there is none). Not omitempty.
	ReadOnlyHead  string `json:"read_only_head"`
	Body          string `json:"body"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	HasSeed       bool   `json:"has_seed"`
	// ReadOnly: every write face refuses this document (405); the cockpit needs to
	// know before it renders an editor. Not omitempty.
	ReadOnly bool `json:"read_only"`
}

type roleDefDTO struct {
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

// roleDefListItemDTO: definition_md is ABSENT, not "" — an empty persona
// would falsely read as "no definition". SizeChars still measures the stored
// document.
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
	RoleKey string `json:"role_key"`

	MemberID string `json:"member_id"`

	MemberName string `json:"member_name"`
}

// docSizeDTO: CapChars is the cap of THIS document's own segment (each has its
// own doc.cap_chars.* setting); do not hoist it to one envelope field.
type docSizeDTO struct {
	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`
}

// roleDocSizesDTO is keyed by ROLE (listRoleKeys), measured on the folded doc.
// 🔴 Not "everything capped": an insight document under a name no role
// carries spends the same cap and never appears here.
type roleDocSizesDTO struct {
	RoleKey string     `json:"role_key"`
	Duty    docSizeDTO `json:"duty"`
	Insight docSizeDTO `json:"insight"`
}

type taskManualDocSizesDTO struct {
	TypeKey string     `json:"type_key"`
	Sop     docSizeDTO `json:"sop"`
}

type docSizesDTO struct {
	Roles       []roleDocSizesDTO       `json:"roles"`
	TaskManuals []taskManualDocSizesDTO `json:"task_manuals"`
}

type roleDeleteResultDTO struct {
	Role             string   `json:"role"`
	RemovedMemberIDs []string `json:"removed_member_ids"`
}

// insightDTO: 🔴 IsDefault ("never written its own") does NOT imply Text=="" —
// a seeded role reads the factory wording with IsDefault=true. The cockpit
// must read this field.
type insightDTO struct {
	SizeChars     int    `json:"size_chars"`
	CapChars      int    `json:"cap_chars"`
	RoleKey       string `json:"role_key"`
	Text          string `json:"text"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	// HasSeed: a factory insight exists for THIS role — the precondition for
	// reset_insight (409 otherwise). 🔴 Not IsDefault (what was written vs what to
	// fall back to), and not RoleDefDTO.IsSeed: the duty seed is gated on the
	// factory role roster (seedRoleDefinitionMD), the insight seed only on the file
	// existing (seedInsightMD), so a role can have one without the other.
	HasSeed bool `json:"has_seed"`
}

// insightPatchResultDTO: size fields carry their unit (characters) in the name
// (owner ruling 2026-07-31).
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

// taskSopPatchResultDTO: applied_edits counts edits that changed the text THEY
// were handed, not whether the document ended up different — compare sha256.
type taskSopPatchResultDTO struct {
	TypeKey      string `json:"type_key"`
	AppliedEdits int    `json:"applied_edits"`
	SizeChars    int    `json:"size_chars"`
	CapChars     int    `json:"cap_chars"`
	Sha256       string `json:"sha256"`
}

type taskStepNotePatchResultDTO struct {
	TaskID       string `json:"task_id"`
	StepID       string `json:"step_id"`
	StepStatus   string `json:"step_status"`
	AppliedEdits int    `json:"applied_edits"`
	SizeChars    int    `json:"size_chars"`
	CapChars     int    `json:"cap_chars"`
	Sha256       string `json:"sha256"`
}

type replyCardAnswerDTO struct {
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

	SelectMode string  `json:"select_mode"`
	Status     string  `json:"status"`
	CreatedTS  float64 `json:"created_ts"`

	Attachments   []chatAttachmentDTO `json:"attachments"`
	AnsweredTS    *float64            `json:"answered_ts"`
	ExpiredTS     *float64            `json:"expired_ts"`
	ChatMessageID string              `json:"chat_message_id"`
	Answer        *replyCardAnswerDTO `json:"answer"`
	Task          *taskRefDTO         `json:"task"`
	TaskExecutor  string              `json:"task_executor"`
}

// replyCardListItemDTO is a LIGHT row: summary plus, when answered, the
// decision digest; never the body (owner ruling T-3f31: 卡只需要 title+決策).
type replyCardListItemDTO struct {
	ID           string                   `json:"id"`
	From         string                   `json:"from"`
	Kind         string                   `json:"kind"`
	Summary      string                   `json:"summary"`
	Status       string                   `json:"status"`
	CreatedTS    float64                  `json:"created_ts"`
	AnsweredTS   *float64                 `json:"answered_ts"`
	ExpiredTS    *float64                 `json:"expired_ts"`
	Answer       *replyCardAnswerBriefDTO `json:"answer"`
	Task         *taskRefDTO              `json:"task"`
	TaskExecutor string                   `json:"task_executor"`
}

// replyCardAnswerBriefDTO: Text is preview-truncated and attachments are a
// COUNT; the refs ride get_reply_card only.
type replyCardAnswerBriefDTO struct {
	OptionIdxs []int `json:"option_idxs"`

	Options     []string `json:"options"`
	Text        string   `json:"text"`
	Attachments int      `json:"attachments"`
}

type replyCardCountDTO struct {
	Waiting int `json:"waiting"`
	// Answered / Expired count a 24h window.
	Answered int `json:"answered"`
	Expired  int `json:"expired"`
}

// chatListDTO: NextCursor is opaque (encodeChatCursor) and omitted when the
// walk has ended. A short page does NOT mean exhaustion (filters shorten
// pages). Messages is [] when empty, never null.
type chatListDTO struct {
	Messages   []chatMessageDTO `json:"messages"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type chatUnreadCountDTO struct {
	Unread int `json:"unread"`
}

// resumeChatCutDTO: messages NOT in this payload. Hint must be actionable on
// its own (tool + exact parameter pairing): the agent reading it is mid-wake.
type resumeChatCutDTO struct {
	Omitted bool   `json:"omitted"`
	Hint    string `json:"hint"`
}

type resumeSummaryDTO struct {
	Identity *string `json:"identity"`
	// GeneratedAt (date, time, offset) is the only anchor for turning a ts_display
	// into 「多久以前」; a waking agent must not trust its own clock.
	GeneratedAt        string                  `json:"generated_at"`
	Chat               []chatMessageDTO        `json:"chat"`
	ChatEarlierOmitted resumeChatCutDTO        `json:"chat_earlier_omitted"`
	Tasks              []resumeTaskDTO         `json:"tasks"`
	Roster             []resumeRosterMemberDTO `json:"roster"`
	Machines           *resumeMachinesDTO      `json:"machines"`
	Overview           resumeOverviewDTO       `json:"overview"`
	Note               string                  `json:"note"`
}

// resumeRosterMemberDTO: every text field is BOUNDED (owner ruling
// rc-4e98c0481852: know who to ask, don't flood context). 🔴 Insight is
// deliberately absent by owner ruling (2026-08-02), not for lack of access —
// adding it reverses that decision.
type resumeRosterMemberDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Kind     string `json:"kind"`
	RoleName string `json:"role_name"`

	Duty string `json:"duty"`
	// CurrentTask: contractors only, their task title (owner ruling
	// rc-a02d8bc7fe23: 正職給職責、外包給任務標題); hard-truncated
	// (resumeTaskTitlePreview) — untruncated titles outweighed the machine block.
	CurrentTask string `json:"current_task"`
	// Task progress is contractors only (owner ruling rc-6935feeb293a); members
	// leave all four zero.
	TaskStatus    string `json:"task_status"`
	WaitingReason string `json:"waiting_reason"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
	// Machine is the LIVE binding — not LastMachineID, not DesiredMachineID.
	Machine  string `json:"machine"`
	Presence string `json:"presence"`
}

type resumeMachineDTO struct {
	// MachineID is the only safe name: our hosts report the SAME hostname, so
	// anything derived from a hostname silently picks the wrong box.
	MachineID   string `json:"machine_id"`
	DisplayName string `json:"display_name"`
	Online      bool   `json:"online"`
}

// resumeMachinesDTO (owner ruling rc-09476f535b59): the list plus where you
// stand; deliberately not grouped by member (the roster already carries it).
type resumeMachinesDTO struct {
	List []resumeMachineDTO `json:"list"`

	YouAreOn string `json:"you_are_on"`
}

type resumeOverviewDTO struct {
	ChatCount           int `json:"chat_count"`
	ChatChars           int `json:"chat_chars"`
	TasksReturned       int `json:"tasks_returned"`
	TasksOpenTotal      int `json:"tasks_open_total"`
	TasksDetailChars    int `json:"tasks_detail_chars"`
	CardsWaiting        int `json:"cards_waiting"`
	CardsAnsweredRecent int `json:"cards_answered_recent"`

	RosterChars   int `json:"roster_chars"`
	MachinesChars int `json:"machines_chars"`
	// 🔴 estimated_total_chars rule (missed twice): text the snapshot CARRIES is
	// an addend (roster_chars, machines_chars, steps_on_answered_card_chars); text
	// the caller would have to FETCH (tasks_detail_chars) is not.
	StepsOnAnsweredCard      int `json:"steps_on_answered_card"`
	StepsOnAnsweredCardChars int `json:"steps_on_answered_card_chars"`
}

// resumeSummarySizeDTO: overview built through the same resumeSnapshotParts as
// resume_summary, so they cannot drift; estimated_total_chars is what the boot
// threshold gates on.
type resumeSummarySizeDTO struct {
	Identity            *string           `json:"identity"`
	Overview            resumeOverviewDTO `json:"overview"`
	EstimatedTotalChars int               `json:"estimated_total_chars"`
	Note                string            `json:"note"`
}

// resumeTaskDTO is a LIGHT row: no steps / DoD text (owner ruling T-3f31).
type resumeTaskDTO struct {
	ID              string  `json:"id"`
	TaskNo          string  `json:"task_no"`
	TypeKey         string  `json:"type_key"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	Priority        string  `json:"priority"`
	WaitingReason   string  `json:"waiting_reason"`
	CurrentStepID   string  `json:"current_step_id"`
	CurrentStepName string  `json:"current_step_name"`
	ProgressDone    int     `json:"progress_done"`
	ProgressTotal   int     `json:"progress_total"`
	DetailChars     int     `json:"detail_chars"`
	UpdatedTS       float64 `json:"updated_ts"`
	// Lock / ReassignedFrom*: the handover hold must show on the row itself — the
	// chat notice is posted once, and not at all to an outsource successor.
	Lock               string `json:"lock"`
	ReassignedFrom     string `json:"reassigned_from"`
	ReassignedFromKind string `json:"reassigned_from_kind"`

	Blocking []string `json:"blocking"`
	// AnsweredCardSteps: in_progress steps whose card is already answered. 🔴 A
	// pointer, not a verdict: the answer may be a rejection; nothing here marks the
	// step done.
	AnsweredCardSteps []resumeAnsweredCardStepDTO `json:"answered_card_steps"`
}

type resumeAnsweredCardStepDTO struct {
	StepID   string `json:"step_id"`
	StepName string `json:"step_name"`
	CardID   string `json:"card_id"`
}

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

type taskArtifactReceiptDTO struct {
	TaskID        string `json:"task_id"`
	ArtifactID    string `json:"artifact_id"`
	ArtifactCount int    `json:"artifact_count"`
}

type taskArtifactReplaceReceiptDTO struct {
	TaskID        string `json:"task_id"`
	ArtifactID    string `json:"artifact_id"`
	ArtifactCount int    `json:"artifact_count"`
	VersionCount  int    `json:"version_count"`
}

type taskPlanReceiptDTO struct {
	TaskID        string `json:"task_id"`
	StepsTotal    int    `json:"steps_total"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
}

type taskStepInsertReceiptDTO struct {
	TaskID        string `json:"task_id"`
	StepID        string `json:"step_id"`
	StepsTotal    int    `json:"steps_total"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
}

type taskStepMutationReceiptDTO struct {
	TaskID        string `json:"task_id"`
	StepsTotal    int    `json:"steps_total"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
}

type taskPriorityReceiptDTO struct {
	TaskID   string `json:"task_id"`
	Priority string `json:"priority"`
	FrozenBy string `json:"frozen_by"`
}

type taskStepNoteReceiptDTO struct {
	TaskID     string `json:"task_id"`
	StepID     string `json:"step_id"`
	StepStatus string `json:"step_status"`

	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`

	Sha256 string `json:"sha256"`
}

// ── Write receipts ──
//
// 🔴 Owner rule 2026-09-05 (「自己發送出去的內容 … 不應該再回傳回來」): a write
// answers with what it DID and what the caller could not know (server-minted
// ids, server stamps, derived / defaulted / silently declined values), never
// the document it was handed; read faces still serve whole objects. sha256
// replaces the text echo, taken over the text AS STORED on the document
// receipts — except taskWriteReceiptDTO (see there). IsDefault on the seeded
// documents tracks whether an edit EXISTS, not whether the text differs from
// the seed (the Fold* helpers do not compare).

type bootDocumentReceiptDTO struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`

	IsDefault bool `json:"is_default"`

	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`

	Sha256 string `json:"sha256"`
}

type chatPostReceiptDTO struct {
	ID string `json:"id"`
	// To is the same field on both chat writes (owner ruling rc-f1c0fd3cf124) — do
	// not re-split. On the task route it is the executor resolved at post time,
	// which a later read cannot recover.
	To string `json:"to"`

	TS float64 `json:"ts"`
	// Attachments: one entry per attachment that LANDED — inline uploads get their
	// id here or nowhere, and it is the only sign one silently failed. NO omitempty:
	// [] is an answer.
	Attachments []chatAttachmentDTO `json:"attachments"`
}

type globalContextReceiptDTO struct {
	// IsDefault = no row (absent or tombstoned). 🔴 This block has NO shipped seed:
	// default means the boot context skips it, not "factory text". Replacing with
	// "" still stores a row and clears it.
	IsDefault bool `json:"is_default"`

	SizeChars int `json:"size_chars"`

	Sha256 string `json:"sha256"`
}

type insightReceiptDTO struct {
	RoleKey string `json:"role_key"`

	IsDefault bool `json:"is_default"`

	HasSeed bool `json:"has_seed"`
	// SizeChars / Sha256 are of the TRIMMED stored text — the only way to notice
	// the handler's trim.
	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`

	Sha256 string `json:"sha256"`
}

type outsourceRestartReceiptDTO struct {
	ID string `json:"id"`
	// ActivationPending: decided but not delivered (no live SSE downstream); the
	// reconcile cadence retries. Here or nowhere — absent on every other read.
	ActivationPending bool `json:"activation_pending,omitempty"`

	LastOpReason string `json:"last_op_reason,omitempty"`
}

// agentLifecycleReceiptDTO: an id is the whole news. The only response fields
// the server writes without persisting are activation_pending
// (HandleActivateMember) and relocation_pending / relocation_deferred (the
// relocates), and each has its own receipt below.
type agentLifecycleReceiptDTO struct {
	ID string `json:"id"`
}

type memberActivateReceiptDTO struct {
	ID string `json:"id"`
	// ActivationPending: no START went out on this attempt (a positive check, not a
	// list of known failures). Here or nowhere — absent on every other read.
	ActivationPending bool `json:"activation_pending,omitempty"`

	LastOpReason string `json:"last_op_reason,omitempty"`
}

type agentRelocateReceiptDTO struct {
	ID string `json:"id"`
	// RelocationPending: scheduled but not landed (the pin is persisted before any
	// dispatch, so a relocate never fails on dispatch). Omitted does NOT mean the
	// agent already runs on the pin.
	RelocationPending bool `json:"relocation_pending,omitempty"`
	// RelocationDeferred: a deliberate deferral (the old session is still live),
	// not a delivery failure — hold back the "nothing was dispatched" alert.
	RelocationDeferred bool `json:"relocation_deferred,omitempty"`
}

type replyCardCreateReceiptDTO struct {
	ID string `json:"id"`

	ChatMessageID string `json:"chat_message_id"`

	CreatedTS float64 `json:"created_ts"`
	// Attachments: same contract as chatPostReceiptDTO.Attachments.
	Attachments []chatAttachmentDTO `json:"attachments"`
	HoldNote    string              `json:"hold_note"`
}

type replyCardReceiptDTO struct {
	ID string `json:"id"`
	// Status is what the card BECAME: all three verbs may decline to move an
	// already answered / expired card.
	Status string `json:"status"`

	AnsweredTS *float64 `json:"answered_ts"`
	ExpiredTS  *float64 `json:"expired_ts"`

	Answer *replyCardAnswerDTO `json:"answer"`
	// TaskID / StepID: the step this write released from waiting_owner (per step:
	// the card stores a task_step_id); empty for an unbound card.
	TaskID string `json:"task_id"`
	StepID string `json:"step_id"`
}

type roleDefReceiptDTO struct {
	Key string `json:"key"`
	// 🔴 Name is the only sign that a rename of a SEED role (IsSeed) is silently
	// ignored: the request gets 200 and no rename.
	Name string `json:"name"`

	IsDefault bool `json:"is_default"`

	IsSeed bool `json:"is_seed"`

	SizeChars int `json:"size_chars"`
	CapChars  int `json:"cap_chars"`

	Sha256 string `json:"sha256"`
}

type scheduledMessageDeleteReceiptDTO struct {
	ID       string `json:"id"`
	MemberID string `json:"member_id"`

	Deleted bool `json:"deleted"`
}

type scheduledMessageReceiptDTO struct {
	ID string `json:"id"`

	MemberID string `json:"member_id"`

	Label string `json:"label"`

	BodySizeChars int `json:"body_size_chars"`

	Cadence string `json:"cadence"`
	// CustomMonths survives here because the server RESOLVES it (omitted on a
	// custom create = all twelve, resolveCustomMonths); always emitted.
	CustomMonths []int `json:"custom_months"`
	// DayOfMonth / DayOfWeek are server-DEFAULTED (1 / 0) on create, so a later
	// PATCH to monthly / weekly has a day to land on.
	DayOfMonth int `json:"day_of_month"`
	DayOfWeek  int `json:"day_of_week"`

	Status string `json:"status"`

	LastFiredSlot string  `json:"last_fired_slot"`
	LastFiredTS   float64 `json:"last_fired_ts"`

	CreatedTS float64 `json:"created_ts"`
}

type selfReportReceiptDTO struct {
	ID string `json:"id"`
	// 🔴 DesiredState is why this receipt exists: is this boot still wanted? An
	// agent that wakes to `offline` should wind down, not start work.
	DesiredState string `json:"desired_state"`
	// RefocusOp: which rung (下線 → 加速 → 強制) is in flight; the handlers refuse
	// to walk that ladder backwards.
	RefocusOp string `json:"refocus_op"`

	RefocusDeadline float64 `json:"refocus_deadline"`
	// StopEffect: what report_stopped actually did (its outcomes were otherwise
	// byte-identical 200s). Omitted on the other three faces.
	StopEffect string `json:"stop_effect,omitempty"`
}

// stop_effect enum. decideStoppedReport now produces only collected /
// already_reported; latched_for_collect and recorded_only remain in the wire
// enum but are no longer produced.
const (
	stopEffectCollected = "collected"

	stopEffectLatchedForCollect = "latched_for_collect"

	stopEffectRecordedOnly = "recorded_only"
	// stopEffectAlreadyReported: this call did NOTHING (a stopped-report was
	// already anchored); do not read it as "stopped".
	stopEffectAlreadyReported = "already_reported"
)

// taskManualReceiptDTO: sop_md_* are pointers because they are present ONLY
// when this call wrote the SOP — 0 would be indistinguishable from an empty SOP
// that was written.
type taskManualReceiptDTO struct {
	TypeKey string `json:"type_key"`

	UpdatedTS float64 `json:"updated_ts"`

	IsDefault bool `json:"is_default"`
	IsSeed    bool `json:"is_seed"`

	SopMdChars    *int    `json:"sop_md_chars,omitempty"`
	SopMdCapChars *int    `json:"sop_md_cap_chars,omitempty"`
	SopMdSha256   *string `json:"sop_md_sha256,omitempty"`
}

type taskWriteReceiptDTO struct {
	// No task_no: TaskNo(taskID) returns taskID.
	TaskID string `json:"task_id"`

	Title string `json:"title"`

	Status string `json:"status"`

	ExecutorID string `json:"executor_id"`

	ExecutorKind string `json:"executor_kind"`
	// Lock is `reassigning` while a transfer waits (only reassign sets it, only
	// claim clears it); Status does NOT move when a lock is placed.
	Lock string `json:"lock"`
	// ClosedTS: terminate and mark_task_duplicated can DECLINE to close, so this
	// is the answer to "did it close".
	ClosedTS *float64 `json:"closed_ts"`

	DuplicateOf string `json:"duplicate_of"`

	Deps []string `json:"deps"`

	ProgressDone  int `json:"progress_done"`
	ProgressTotal int `json:"progress_total"`
	ArtifactCount int `json:"artifact_count"`
	// 🔴 DescriptionSizeChars / DescriptionSha256 are taken from the in-memory Task
	// the handler just assigned (writeTaskDescription), NOT a read-back: they
	// confirm the handler's trim, not what storage wrote. Measured: making
	// SetTaskDescriptionOn persist a different string keeps this receipt and
	// conformance green.
	DescriptionSizeChars int    `json:"description_size_chars"`
	DescriptionSha256    string `json:"description_sha256"`
}

func receiptSha256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

type bootstrapDTO struct {
	Role    string  `json:"role"`
	Name    string  `json:"name"`
	Context string  `json:"context"`
	Token   *string `json:"token"`
}

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

	ReplyCardStatus string `json:"reply_card_status"`

	WaitingReason string `json:"waiting_reason"`
	// 🔴 No Note field, deliberately (owner ruling rc-4c8065fb30a5): the text is
	// served one step at a time by taskStepDetailDTO. Removed from the schema
	// rather than left empty so readers fail to build instead of reading "" as
	// "no note" — do not reinstate it for compatibility.
	NoteSizeChars int     `json:"note_size_chars"`
	NoteCapChars  int     `json:"note_cap_chars"`
	StartedTS     float64 `json:"started_ts"`
	FinishedTS    float64 `json:"finished_ts"`
}

// taskStepDetailDTO is a SEPARATE type on purpose: one struct with a
// sometimes-filled Note makes "" ambiguous again. No task fields, no sibling
// steps.
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

const (
	taskDetailLevelSummary = "summary"
	taskDetailLevelFull    = "full"
)

// taskArtifactsDetailLevelFull has no counterpart any more, but
// artifacts_detail_level stays: conformance/test_rest_happy.py asserts it ==
// "full".
const taskArtifactsDetailLevelFull = "full"

// taskArtifactDTO: URL means one thing on every kind — where the content is
// (blob serve path for file/image, the external address for a link). Name is
// never empty (artifactDisplayName). IsImage is deliberately off (Mime's
// prefix). 🔴 Filename is not Name: Name may be a sentence without an
// extension, and the cockpit's preview needs the blob's filename when Mime is
// `application/octet-stream` — dropping it broke .md previews in production.
type taskArtifactDTO struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// AttachmentID is served as stored, even when the blob is gone. Kept by owner
	// ruling rc-91e29b576ad8: `ocagent diff` takes this id (system_interaction
	// §2.1), and a link's URL contains no blob id.
	AttachmentID string `json:"attachment_id"`
	Name         string `json:"name"`

	Filename    string  `json:"filename"`
	Description string  `json:"description"`
	URL         string  `json:"url"`
	Mime        string  `json:"mime"`
	CreatedTS   float64 `json:"created_ts"`
	CreatedBy   string  `json:"created_by"`

	VersionCount int `json:"version_count"`
}

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

type taskArtifactListDTO struct {
	TaskID               string            `json:"task_id"`
	ArtifactsDetailLevel string            `json:"artifacts_detail_level"`
	Artifacts            []taskArtifactDTO `json:"artifacts"`
}

type taskDTO struct {
	ID           string         `json:"id"`
	TaskNo       string         `json:"task_no"`
	TypeKey      string         `json:"type_key"`
	Title        string         `json:"title"`
	DedupeKey    string         `json:"dedupe_key"`
	Inputs       map[string]any `json:"inputs"`
	Description  string         `json:"description"`
	DuplicateOf  string         `json:"duplicate_of"`
	Status       string         `json:"status"`
	Lock         string         `json:"lock"`
	Priority     string         `json:"priority"`
	ExecutorKind string         `json:"executor_kind"`
	ExecutorID   string         `json:"executor_id"`
	CreatorID    string         `json:"creator_id"`

	ReassignedFrom     string        `json:"reassigned_from"`
	ReassignedFromKind string        `json:"reassigned_from_kind"`
	HandoverNote       string        `json:"handover_note"`
	HandoverNoteTS     float64       `json:"handover_note_ts"`
	HandoverNoteBy     string        `json:"handover_note_by"`
	WaitingReason      string        `json:"waiting_reason"`
	CreatedTS          float64       `json:"created_ts"`
	UpdatedTS          float64       `json:"updated_ts"`
	ClosedTS           *float64      `json:"closed_ts"`
	Deps               []string      `json:"deps"`
	Steps              []taskStepDTO `json:"steps"`
	// DetailLevel / NotesIncluded are always "summary" / false — constants, not a
	// mode (the full counterpart is get_task_step). 🔴 No "step list may be cut"
	// marker on purpose: ListTaskSteps has no LIMIT, so it could never fire.
	DetailLevel   string `json:"detail_level"`
	NotesIncluded bool   `json:"notes_included"`
	ProgressDone  int    `json:"progress_done"`
	ProgressTotal int    `json:"progress_total"`
	// ArtifactCount: a count, no rows (owner ruling rc-15016959ad4d);
	// list_task_artifacts serves them.
	ArtifactCount int `json:"artifact_count"`
	// Blocking: NON-TERMINAL tasks naming this one in their blocked_by; always
	// present. 🔴 This field and resumeTaskDTO.Blocking are the whole delivery —
	// the owner ruled it is never a message; do not add a notification.
	Blocking []taskDepRefDTO `json:"blocking"`

	FrozenBy string `json:"frozen_by"`

	ForcedDoneBy     string `json:"forced_done_by"`
	ForcedDoneReason string `json:"forced_done_reason"`
}

type taskListItemDTO struct {
	ID           string `json:"id"`
	TaskNo       string `json:"task_no"`
	TypeKey      string `json:"type_key"`
	Title        string `json:"title"`
	DedupeKey    string `json:"dedupe_key"`
	DuplicateOf  string `json:"duplicate_of"`
	Status       string `json:"status"`
	Lock         string `json:"lock"`
	Priority     string `json:"priority"`
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	CreatorID    string `json:"creator_id"`

	ReassignedFrom     string   `json:"reassigned_from"`
	ReassignedFromKind string   `json:"reassigned_from_kind"`
	WaitingReason      string   `json:"waiting_reason"`
	CreatedTS          float64  `json:"created_ts"`
	UpdatedTS          float64  `json:"updated_ts"`
	ClosedTS           *float64 `json:"closed_ts"`
	Deps               []string `json:"deps"`

	DepTasks      []taskDepRefDTO `json:"dep_tasks"`
	ProgressDone  int             `json:"progress_done"`
	ProgressTotal int             `json:"progress_total"`
	// CurrentStepID / CurrentStepName: the first non-terminal step
	// (domain.CurrentStep, same rule as resumeTaskDTO); "" = empty or finished
	// plan, not "the first step". Its place on the light list is still open with
	// the owner (c-1648d14be429) — do not recommend it as a route.
	CurrentStepID   string `json:"current_step_id"`
	CurrentStepName string `json:"current_step_name"`

	ArtifactCount int `json:"artifact_count"`
}

type taskDepRefDTO struct {
	ID     string `json:"id"`
	TaskNo string `json:"task_no"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type taskCreateResultDTO struct {
	TaskID string `json:"task_id"`
	// No task_no (owner ruling rc-f1c0fd3cf124): TaskNo(taskID) returns taskID.
	// ExecutorKind / ExecutorID are what the SERVER decided (a typed create takes
	// the manual's assignee). ExecutorID "" is an answer (a fresh outsource
	// create awaits the scheduler), so no omitempty.
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	// Deduped: true = every other field describes the EXISTING ticket, not what
	// was sent.
	Deduped bool `json:"deduped"`
	// Title / Status appear ONLY on a dedupe hit (pointers, so absence is not a
	// blank title). On a fresh create they would echo the caller (owner rule
	// 2026-09-05).
	Title  *string `json:"title,omitempty"`
	Status *string `json:"status,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

type taskCountDTO struct {
	Open int `json:"open"`

	Total int `json:"total"`
}

type taskManualDTO struct {
	SopMDChars    int           `json:"sop_md_chars"`
	SopMDCapChars int           `json:"sop_md_cap_chars"`
	TypeKey       string        `json:"type_key"`
	DisplayName   string        `json:"display_name"`
	Purpose       string        `json:"purpose"`
	Fields        []ManualField `json:"fields"`
	SopMD         string        `json:"sop_md"`
	// Lore is its own field, not folded into sop_md (owner ruling 2026-09-07);
	// LoreChars counts it.
	Lore      string         `json:"lore"`
	LoreChars int            `json:"lore_chars"`
	Assignee  map[string]any `json:"assignee"`
	UpdatedTS float64        `json:"updated_ts"`
	IsSeed    bool           `json:"is_seed"`
	IsDefault bool           `json:"is_default"`
}

// taskManualListItemDTO: sop_md is ABSENT, not "" (an empty SOP is a
// different claim). The size is measured on the served (folded) document.
type taskManualListItemDTO struct {
	SopMDChars    int            `json:"sop_md_chars"`
	SopMDCapChars int            `json:"sop_md_cap_chars"`
	TypeKey       string         `json:"type_key"`
	DisplayName   string         `json:"display_name"`
	Purpose       string         `json:"purpose"`
	Fields        []ManualField  `json:"fields"`
	Assignee      map[string]any `json:"assignee"`
	UpdatedTS     float64        `json:"updated_ts"`
	IsSeed        bool           `json:"is_seed"`
	IsDefault     bool           `json:"is_default"`
}

type taskManualDeleteResultDTO struct {
	TypeKey string `json:"type_key"`
	Deleted bool   `json:"deleted"`
}

// themeListItemDTO is NOT the bundle: a theme embeds its images, and listing
// bundles is the several-hundred-KB answer that made GET /api/settings
// unusable.
type themeListItemDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// themeWriteReceiptDTO: a replace KEEPS OrderIdx, so re-colouring a theme does
// not move it.
type themeWriteReceiptDTO struct {
	ID        string  `json:"id"`
	Created   bool    `json:"created"`
	OrderIdx  int     `json:"order_idx"`
	UpdatedAt float64 `json:"updated_at"`
}

// themeDeleteResultDTO: deleting the ACTIVE theme resets display.theme to ""
// in the same request; DisplayThemeReset says so.
type themeDeleteResultDTO struct {
	ID                string `json:"id"`
	Deleted           bool   `json:"deleted"`
	DisplayThemeReset bool   `json:"display_theme_reset"`
}

type docSummaryDTO struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type docDTO struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	MarkdownMD string `json:"markdown_md"`
}

// Outsource workers differ from members only in: creation by the capped
// scheduler, one-task binding (codename, release), and worker-only
// task/delegator controls in the cockpit. Identity, lifecycle, projection
// transport and SSE are member mechanisms.
type outsourceWorkerProjection struct {
	unread int
	now    float64
	online bool
	// cfg is the SAME reconcile config the tick collects this worker on, carried
	// (not re-derived) so the deadline on the wire and the one that kills agree.
	cfg            reconcileConfig
	tele           map[string]any
	gaugeEntry     map[string]any
	machineDisplay func(string) string

	spawnTarget string

	accountDisplay func(string) string
	delegatedBy    string

	typeDisplay func(string) string

	terminalAttach string

	loginWarnings []RuntimeLoginWarningDTO
}

// noteCap is the caller's s.stepNoteCap() — the ceiling writes are refused
// against, so the reported and enforced numbers match.
func newTaskStepDTO(st TaskStep, cardStatus map[string]string, noteCap int) taskStepDTO {
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

		NoteSizeChars: utf8.RuneCountInString(st.Note),
		NoteCapChars:  noteCap,
		StartedTS:     st.StartedTS,
		FinishedTS:    st.FinishedTS,
	}
}

func newTaskStepDetailDTO(st TaskStep, cardStatus map[string]string, noteCap int) taskStepDetailDTO {
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
		NoteCapChars:    noteCap,
		StartedTS:       st.StartedTS,
		FinishedTS:      st.FinishedTS,
	}
}

func newTaskDTO(t Task, steps []TaskStep, deps []string, cardStatus map[string]string, noteCap int) taskDTO {
	if deps == nil {
		deps = []string{}
	}
	stepDTOs := []taskStepDTO{}
	for _, st := range steps {
		stepDTOs = append(stepDTOs, newTaskStepDTO(st, cardStatus, noteCap))
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

		DetailLevel:   taskDetailLevelSummary,
		NotesIncluded: false,
		ProgressDone:  done,
		ProgressTotal: total,
		// ArtifactCount and Blocking are folded in by taskDTOOf (DAL reads) after this
		// pure projection.
		Blocking:         []taskDepRefDTO{},
		FrozenBy:         t.FrozenBy,
		ForcedDoneBy:     t.ForcedDoneBy,
		ForcedDoneReason: t.ForcedDoneReason,
	}
	if t.ClosedTS > 0 {
		ts := t.ClosedTS
		dto.ClosedTS = &ts
	}
	return dto
}

func newTaskArtifactDTO(a TaskArtifact, att *ChatAttachment, retained int) taskArtifactDTO {
	dto := taskArtifactDTO{
		VersionCount: retained + 1,
		ID:           a.ID,
		Kind:         a.Kind,
		AttachmentID: a.AttachmentID,
		Description:  a.Description,
		CreatedTS:    a.CreatedTS,
		CreatedBy:    a.CreatedBy,
	}
	if a.Kind == ArtifactKindLink {
		dto.URL = linkTargetOf(att)
	}
	if b, ok := artifactBlobFacts(att); ok && a.Kind != ArtifactKindLink {
		dto.URL, dto.Mime, dto.Filename = b.url, b.mime, b.filename
	}
	if att != nil && a.Kind == ArtifactKindLink {
		dto.Mime = att.Mime
	}
	dto.Name = artifactDisplayName(a, att)
	return dto
}

func linkTargetOf(att *ChatAttachment) string {
	if att == nil {
		return ""
	}
	return strings.TrimRight(string(att.Data), "\r\n")
}

// artifactDisplayName derives Name at read time and is deliberately NOT written
// back to the column: a copied filename would go stale silently when the
// content is replaced.
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

type artifactBlobFields struct {
	url      string
	mime     string
	filename string
	isImage  bool
}

// 🔴 The artifact-kind test stays at each CALL SITE: the identity scanners
// (authz_surface_behavior_test, lifecycle_identity_behavior_test) only see a
// `.Kind` selector in a comparison, so folding it in here silently drops it
// from both ledgers.
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

// byID is the whole task population the handler already loaded: no per-dep
// query here — this list is the payload/latency hot path.
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

		CurrentStepID:   current.ID,
		CurrentStepName: current.Name,
	}
	if t.ClosedTS > 0 {
		ts := t.ClosedTS
		dto.ClosedTS = &ts
	}
	return dto
}

// A dep missing from byID keeps its id / TaskNo with Title/Status "" (the
// client's 查無此任務 row); never invent a status — that launders "gone" into
// "not started".
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

func newTaskManualDTO(m TaskManual, sopCapChars int) (taskManualDTO, error) {
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
		SopMDChars:    utf8.RuneCountInString(m.SopMD),
		SopMDCapChars: sopCapChars,
		TypeKey:       m.TypeKey,
		DisplayName:   m.DisplayName,
		Purpose:       m.Purpose,
		Fields:        fields,
		SopMD:         m.SopMD,
		Assignee:      assignee,
		UpdatedTS:     m.UpdatedTS,
		IsSeed:        m.IsSeed,
		IsDefault:     m.IsDefault,
	}, nil
}

func newTaskManualListItemDTO(m TaskManual, sopCapChars int) (taskManualListItemDTO, error) {
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
		SopMDChars:    utf8.RuneCountInString(m.SopMD),
		SopMDCapChars: sopCapChars,
		TypeKey:       m.TypeKey,
		DisplayName:   m.DisplayName,
		Purpose:       m.Purpose,
		Fields:        fields,
		Assignee:      assignee,
		UpdatedTS:     m.UpdatedTS,
		IsSeed:        m.IsSeed,
		IsDefault:     m.IsDefault,
	}, nil
}

// actorRuntimeFold is shared by the member monitoring-session row
// (api_monitoring.go) and the outsource worker DTO. account is the RAW
// telemetry key; each caller applies its own display resolution.
type actorRuntimeFold struct {
	account         string
	cost            *float64
	contextPct      *float64
	compactionCount *int
	bankedCost      *float64
}

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

func (s *apiServer) newOutsourceMemberDTO(w OutsourceWorker, task *Task, p outsourceWorkerProjection) memberDTO {
	m := memberFromWorker(w)
	dto := newMemberDTO(m, "", "", p.unread,
		workerPresence(w, p.now, p.online),
		winddownDeadlineOf(m, p.cfg), p.terminalAttach)
	dto.Status = w.Status
	dto.TaskID = w.TaskID
	dto.CreatedTS = w.CreatedTS
	dto.DelegatedBy = p.delegatedBy
	if p.spawnTarget != "" && p.machineDisplay != nil {
		dto.Machine = p.machineDisplay(p.spawnTarget)
	}
	rt := foldActorRuntime(p.tele, p.gaugeEntry, w.BankedCost, w.Runtime)
	dto.Cost = rt.cost
	dto.ContextPct = rt.contextPct
	dto.CompactionCount = rt.compactionCount
	dto.BankedCost = rt.bankedCost
	// 🔴 Only the RESOLVED name is served; the raw account key (a credential hash)
	// never reaches the wire.
	if rt.account != "" && p.accountDisplay != nil {
		if display := p.accountDisplay(rt.account); display != "" {
			dto.Account = &display
		}
	}
	if task != nil {
		dto.TaskTitle = task.Title
		dto.TaskStatus = task.Status
		dto.CreatorID = task.CreatorID
		dto.TaskNo = TaskNo(task.ID)
		dto.TaskCreatedTS = task.CreatedTS
		dto.TaskTypeKey = task.TypeKey
		if p.typeDisplay != nil {
			dto.TaskTypeName = p.typeDisplay(task.TypeKey)
		}
	}
	// RefocusSince 0 = unset (the FE maps 0 → null); DesiredState "" (pre-column
	// row) reads as online client-side.
	dto.RefocusSince = w.RefocusSince
	dto.RefocusOp = w.RefocusOp
	// 🔴 winddownDeadlineOf is the SAME function MemberDTO reads (api_helpers.go),
	// covering both the 換手 and 下線 axes; do not re-inline one arm — the 下線
	// countdown was once invisible to cockpit and agent. 0 = not collected on a
	// clock (owner 2026-08-19: 重新聚焦 runs no clock for outsource workers). A
	// ceiling: the tick may collect earlier.
	dto.RefocusDeadline = winddownDeadlineOf(memberFromWorker(w), p.cfg)
	dto.DesiredState = w.DesiredState
	dto.TerminalAttachCommand = p.terminalAttach
	if p.loginWarnings != nil {
		dto.RuntimeLoginWarnings = p.loginWarnings
	}
	return dto
}

func workerPresence(w OutsourceWorker, now float64, online bool) string {
	if w.Status == WorkerStatusReleased {
		return ""
	}
	return PresenceState(memberFromWorker(w), now, online)
}

func attachmentDTOsFromRefs(refs []any) []chatAttachmentDTO {
	attachments := []chatAttachmentDTO{}
	for _, r := range refs {
		ref, _ := r.(map[string]any)
		id, _ := ref["id"].(string)
		if id == "" {
			continue
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

// 🔴 Safe only because HandlePostChatApiChatPost deletes any caller-supplied
// reply_to before writing its own; meta is otherwise copied through wholesale,
// so this would serve a forged link.
func replyToFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	id, _ := meta[chatReplyToMetaKey].(string)
	return id
}

func replyCardIDFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	id, _ := meta["reply_card_id"].(string)
	return id
}

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

// webhookEndpointDTO: `token` is the plaintext /in credential. What keeps plain
// agents off this DTO is the route floor in routes.go, not MCPExclude. The
// signing secret itself is never echoed (HasSigningSecret only), and the
// observability counters never appear on the public /in response (防探測).
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
	// custom_* are ALWAYS emitted ([] for other cadences). custom_months too, even
	// though a request may omit it: the handler resolves the omission (migration
	// 00053 backfilled the rest).
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

func scheduledMessageReceiptOf(m ScheduledMessage) scheduledMessageReceiptDTO {
	return scheduledMessageReceiptDTO{
		ID:            m.ID,
		MemberID:      m.MemberID,
		Label:         m.Label,
		BodySizeChars: utf8.RuneCountInString(m.Body),
		Cadence:       m.Cadence,
		CustomMonths:  intSetOrEmpty(m.CustomMonths),
		DayOfMonth:    m.DayOfMonth,
		DayOfWeek:     m.DayOfWeek,
		Status:        m.Status,
		LastFiredSlot: m.LastFiredSlot,
		LastFiredTS:   m.LastFiredTS,
		CreatedTS:     m.CreatedTS,
	}
}

func intSetOrEmpty(vals []int) []int {
	sorted := sortedIntSet(vals)
	if sorted == nil {
		return []int{}
	}
	return sorted
}

// webhookRequestLogDTO: owner-only wire — raw external payloads never reach an
// agent-facing surface.
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
