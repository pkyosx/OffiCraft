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
type authStatusDTO struct {
	PasswordSet bool `json:"password_set"`
}

// settingsDTO is the owner-adjustable settings surface (GET/PATCH
// /api/settings).
type settingsDTO struct {
	TokenTTL             int64 `json:"token_ttl"`
	HandoverPct          int   `json:"handover_pct"`
	OutsourceMaxParallel int   `json:"outsource_max_parallel"`
	// UpdaterReceiveBeta / UpdaterAutoUpdate are the two software-update
	// toggles (default false): follow GitHub prereleases too / self-upgrade
	// in the background when a newer release exists.
	UpdaterReceiveBeta bool `json:"updater_receive_beta"`
	UpdaterAutoUpdate  bool `json:"updater_auto_update"`
	// OrgName is the studio display name (org.name; T-d693). "" = never set —
	// the topbar falls back to the localized default string.
	OrgName string `json:"org_name"`
}

type tokenDTO struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
	OwnerID   string `json:"owner_id"`
}

type memberDTO struct {
	ID                string  `json:"id"`
	MemberNo          string  `json:"member_no"`
	Name              string  `json:"name"`
	Kind              string  `json:"kind"`
	RoleKey           string  `json:"role_key"`
	RoleName          string  `json:"role_name"`
	Model             string  `json:"model"`
	Effort            string  `json:"effort"`
	DesiredState      string  `json:"desired_state"`
	DesiredMachineID  string  `json:"desired_machine_id"`
	Machine           string  `json:"machine"`
	Presence          string  `json:"presence"`
	RefocusSince      float64 `json:"refocus_since"`
	LastOp            string  `json:"last_op"`
	LastOpOK          *bool   `json:"last_op_ok"`
	LastOpLog         string  `json:"last_op_log"`
	LastOpReason      string  `json:"last_op_reason"`
	LastOpAt          float64 `json:"last_op_at"`
	UnreadCount       int     `json:"unread_count"`
	RosterStatus      string  `json:"roster_status"`
	OwnerID           string  `json:"owner_id"`
	SchemaVersion     int     `json:"schema_version"`
	RelocationPending *bool   `json:"relocation_pending,omitempty"` // T-8655: set only on the relocate response when the recycle STOP/START could not be delivered (move scheduled, not yet landed); nil everywhere else
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
	ClaudeVersion     *string `json:"claude_version"`
	ClaudeCredSource  *string `json:"claude_cred_source"`
	ClaudeSubReadable *bool   `json:"claude_sub_readable"`
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

type chatMessageDTO struct {
	ID   string         `json:"id"`
	From string         `json:"from"`
	To   string         `json:"to"`
	Body string         `json:"body"`
	TS   float64        `json:"ts"`
	Meta map[string]any `json:"meta"`
	// ReplyCardStatus: read-time join of the card this message carries
	// (meta.reply_card_id) — "waiting" | "answered", or "" when no card. Filled
	// by servedChatMessageDTO; the inline ChatReplyCard reads it to lazy-load
	// answered cards. See ChatMessageDTO in the spec.
	ReplyCardStatus string              `json:"reply_card_status"`
	Attachments     []chatAttachmentDTO `json:"attachments"`
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
	AgentID    string         `json:"agent_id"`
	ContextPct float64        `json:"context_pct"`
	RateLimits map[string]any `json:"rate_limits"`
	TS         float64        `json:"ts"`
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
	Cost          *float64       `json:"cost"`
	Effort        *string        `json:"effort"`
	SelfUpdate    map[string]any `json:"self_update"`
	CommandResult map[string]any `json:"command_result"`
	TS            float64        `json:"ts"`
}

type monitoringSessionDTO struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Effort     string         `json:"effort"`
	Machine    string         `json:"machine"`
	Account    string         `json:"account"`
	Presence   string         `json:"presence"`
	ContextPct *float64       `json:"context_pct"`
	Cost       *float64       `json:"cost"`
	BankedCost *float64       `json:"banked_cost"`
	Tokens     map[string]any `json:"tokens"`
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
	ClaudeVersion     *string `json:"claude_version"`
	ClaudeCredSource  *string `json:"claude_cred_source"`
	ClaudeSubReadable *bool   `json:"claude_sub_readable"`
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

type roleDefDTO struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	DefinitionMD  string `json:"definition_md"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
	IsSeed        bool   `json:"is_seed"`
}

type roleCreateResultDTO struct {
	Role   roleDefDTO `json:"role"`
	Member memberDTO  `json:"member"`
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
	RoleKey       string `json:"role_key"`
	TaskType      string `json:"task_type"`
	Text          string `json:"text"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
}

// lessonsPatchResultDTO is the patch_lessons receipt (T-8327): size (UTF-8
// bytes) + sha256 (hex) are verification anchors over the RESULTING doc text
// so the caller can confirm the write without re-reading the full doc.
type lessonsPatchResultDTO struct {
	RoleKey       string `json:"role_key"`
	TaskType      string `json:"task_type"`
	AppliedEdits  int    `json:"applied_edits"`
	Size          int    `json:"size"`
	Sha256        string `json:"sha256"`
	OwnerID       string `json:"owner_id"`
	SchemaVersion int    `json:"schema_version"`
	IsDefault     bool   `json:"is_default"`
}

type replyCardAnswerDTO struct {
	OptionIdx   *int                `json:"option_idx"` // null = free text only
	Text        string              `json:"text"`
	Attachments []chatAttachmentDTO `json:"attachments"`
}

type replyCardDTO struct {
	ID        string   `json:"id"`
	From      string   `json:"from"`
	Kind      string   `json:"kind"`
	Summary   string   `json:"summary"`
	Body      string   `json:"body"`
	Options   []string `json:"options"`
	Status    string   `json:"status"`
	CreatedTS float64  `json:"created_ts"`
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
// the picked option's index + ORIGINAL wording, the answer text truncated to a
// preview, and the attachment COUNT (refs ride get_reply_card only).
type replyCardAnswerBriefDTO struct {
	OptionIdx   *int   `json:"option_idx"` // null = free text only
	Option      string `json:"option"`     // the picked option's original wording
	Text        string `json:"text"`       // preview-truncated
	Attachments int    `json:"attachments"`
}

type replyCardCountDTO struct {
	Waiting int `json:"waiting"`
	// Answered / Expired: recently-answered and recently-expired (24h window)
	// counts — together they let the 等我回覆 page render its collapsed
	// 近期已處理 header (and hide the pane at zero) without fetching the lists.
	Answered int `json:"answered"`
	Expired  int `json:"expired"`
}

type chatUnreadCountDTO struct {
	Unread int `json:"unread"`
}

type resumeSummaryDTO struct {
	Identity *string           `json:"identity"`
	Chat     []chatMessageDTO  `json:"chat"`
	Tasks    []resumeTaskDTO   `json:"tasks"`
	Overview resumeOverviewDTO `json:"overview"`
	Note     string            `json:"note"`
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
}

type bootstrapDTO struct {
	Role     string  `json:"role"`
	Name     string  `json:"name"`
	TaskType string  `json:"task_type"`
	Context  string  `json:"context"`
	Token    *string `json:"token"`
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
	WaitingReason string  `json:"waiting_reason"`
	StartedTS     float64 `json:"started_ts"`
	FinishedTS    float64 `json:"finished_ts"`
}

// taskArtifactDTO is one pinned deliverable on a task's artifact set (T-3dc5).
// For a link: URL is the external url, AttachmentID/Mime/Filename empty,
// IsImage false. For file/image: URL is the blob serve path, AttachmentID/
// Mime/Filename/IsImage echo the referenced chat_attachment (empty when the
// blob is gone — resolved read-time, honest-empty, never fabricated).
type taskArtifactDTO struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	URL          string  `json:"url"`
	Label        string  `json:"label"`
	Filename     string  `json:"filename"`
	Mime         string  `json:"mime"`
	IsImage      bool    `json:"is_image"`
	AttachmentID string  `json:"attachment_id"`
	CreatedTS    float64 `json:"created_ts"`
	CreatedBy    string  `json:"created_by"`
}

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
	WaitingReason      string        `json:"waiting_reason"`
	CreatedTS          float64       `json:"created_ts"`
	UpdatedTS          float64       `json:"updated_ts"`
	ClosedTS           *float64      `json:"closed_ts"` // null while open
	Deps               []string      `json:"deps"`
	Steps              []taskStepDTO `json:"steps"`
	ProgressDone       int           `json:"progress_done"`
	ProgressTotal      int           `json:"progress_total"`
	// CloseoutReported flips true once the executor reports the close-out
	// follow-ups done (report_task_closeout; §6.3 — terminal tasks only).
	CloseoutReported bool `json:"closeout_reported"`
	// Artifacts is the task's curated deliverable set (T-3dc5), oldest→newest;
	// always present ([] when none). Optional in the spec (§12) but always
	// serialised — the FE popover reads it, the light list reads only the count.
	Artifacts []taskArtifactDTO `json:"artifacts"`
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
	ProgressDone       int      `json:"progress_done"`
	ProgressTotal      int      `json:"progress_total"`
	// ArtifactCount is the number of pinned deliverables (T-3dc5) — the collapsed
	// card's 「產物 N」 badge; 0 (the zero value) when none, so the badge hides.
	// The light list never loads the artifact rows themselves (get_task folds
	// the full set).
	ArtifactCount int `json:"artifact_count"`
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
}

type taskManualDTO struct {
	TypeKey     string         `json:"type_key"`
	DisplayName string         `json:"display_name"`
	Purpose     string         `json:"purpose"`
	Fields      []ManualField  `json:"fields"`
	SopMD       string         `json:"sop_md"`
	Learnings   string         `json:"learnings"`
	Assignee    map[string]any `json:"assignee"`
	UpdatedTS   float64        `json:"updated_ts"`
}

type taskManualDeleteResultDTO struct {
	TypeKey string `json:"type_key"`
	Deleted bool   `json:"deleted"`
}

type outsourceWorkerDTO struct {
	ID         string  `json:"id"`
	Codename   string  `json:"codename"`
	Model      string  `json:"model"`
	Effort     string  `json:"effort"`
	Status     string  `json:"status"`
	TaskID     string  `json:"task_id"`
	TaskTitle  string  `json:"task_title"`
	TaskStatus string  `json:"task_status"`
	CreatedTS  float64 `json:"created_ts"`
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
	// Account / ContextPct / Cost are RUNTIME facts folded from the SAME
	// per-actor telemetry+gauge the member roster reads (keyed by the worker's
	// actor id). Nullable — nil serialises null → the panel shows a bare dash,
	// never a fabricated value (parity with monitoringSessionDTO's honest gate).
	Account    *string  `json:"account"`
	ContextPct *float64 `json:"context_pct"`
	Cost       *float64 `json:"cost"`
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
	DesiredState string  `json:"desired_state"`
}

// outsourceWorkerProjection carries the per-worker runtime facts the DTO folds
// on top of the durable row: the caller's unread count, wall clock, SSE
// presence, the worker's own telemetry/gauge entries (keyed by actor id — the
// SAME maps the member roster reads), a machine-id → display-name resolver, and
// the pre-resolved creator display name. Grouped into one struct so the two
// callers (list loop + single GET) share the exact same fold.
type outsourceWorkerProjection struct {
	unread         int
	now            float64
	online         bool
	tele           map[string]any      // telemetry[w.ID]; nil-safe
	gaugeEntry     map[string]any      // gauge[w.ID]; nil-safe
	machineDisplay func(string) string // machine id → registry display label
	// spawnTarget is the warden the last worker start was dispatched to — the
	// IN-MEMORY spawn observation (workerSpawnTarget; the durable
	// last_spawn_target column retired with the P7d fold). "" = never
	// dispatched this server run — the panel renders 「尚未分配」.
	spawnTarget string
	// spawnAt is the last start-dispatch timestamp (workerSpawnAt) — the wake
	// anchor of the presence projection (waking while fresh). 0 = never
	// dispatched this server run (the row's CreatedTS anchors instead).
	spawnAt float64
	// accountDisplay resolves the raw telemetry account key to its readable
	// name (alias → owner-gated reported label → "") via the SHARED
	// resolveAccountDisplay fold. "" ⇒ the DTO serves null → the panel's
	// honest dash — the raw credential hash NEVER reaches the wire (T-ba6b).
	accountDisplay func(string) string
	delegatedBy    string // resolved creator name ("" = honest fallback)
}

type myTaskDTO struct {
	Task   taskDTO        `json:"task"`
	Manual *taskManualDTO `json:"manual"` // null for an ad-hoc task
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
		WaitingReason:      t.WaitingReason,
		CreatedTS:          t.CreatedTS,
		UpdatedTS:          t.UpdatedTS,
		Deps:               deps,
		Steps:              stepDTOs,
		ProgressDone:       done,
		ProgressTotal:      total,
		CloseoutReported:   t.CloseoutTS > 0,
		// Artifacts default to [] — the handler (taskDTOOf) folds the resolved
		// set in after this pure projection, since resolving file/image blob
		// metadata needs a DAL lookup that does not belong in a pure builder.
		Artifacts: []taskArtifactDTO{},
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
func newTaskArtifactDTO(a TaskArtifact, att *ChatAttachment) taskArtifactDTO {
	dto := taskArtifactDTO{
		ID:           a.ID,
		Kind:         a.Kind,
		URL:          a.URL,
		Label:        a.Label,
		AttachmentID: a.AttachmentID,
		CreatedTS:    a.CreatedTS,
		CreatedBy:    a.CreatedBy,
	}
	if a.Kind != ArtifactKindLink && att != nil {
		dto.URL = "/api/chat/attachment/" + att.ID
		dto.Mime = att.Mime
		if att.Filename != nil {
			dto.Filename = *att.Filename
		}
		dto.IsImage = len(att.Mime) >= 6 && att.Mime[:6] == "image/"
	}
	return dto
}

// newTaskListItemDTO projects one task + its deps + pre-counted step progress
// onto the LIGHT list wire (GET /api/tasks). done/total come from
// dal.AllTaskStepProgress (a grouped COUNT) so the list never loads step rows;
// closed_ts serialises null while open, exactly like newTaskDTO.
func newTaskListItemDTO(t Task, deps []string, done, total, artifactCount int) taskListItemDTO {
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
		ProgressDone:       done,
		ProgressTotal:      total,
	}
	if t.ClosedTS > 0 {
		ts := t.ClosedTS
		dto.ClosedTS = &ts
	}
	return dto
}

// newTaskManualDTO projects one manual row onto the wire (stored JSON blobs
// parsed; a corrupt blob is an error, never a silent empty).
func newTaskManualDTO(m TaskManual) (taskManualDTO, error) {
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
		TypeKey:     m.TypeKey,
		DisplayName: m.DisplayName,
		Purpose:     m.Purpose,
		Fields:      fields,
		SopMD:       m.SopMD,
		Learnings:   m.Learnings,
		Assignee:    assignee,
		UpdatedTS:   m.UpdatedTS,
	}, nil
}

// newTaskManualListItemDTO is the ?view=list light projection (T-ec2c): the
// SAME taskManualDTO wire shape carrying only the type identity the 類型 filter
// reads (type_key / display_name / purpose + updated_ts), with the heavy
// authored blobs HONEST-EMPTY — sop_md / learnings "" (the markdown bulk),
// fields an empty list, assignee an empty object. It never parses the stored
// fields/assignee JSON, so unlike newTaskManualDTO it cannot fail on a corrupt
// blob (the light path deliberately does not touch those columns).
func newTaskManualListItemDTO(m TaskManual) taskManualDTO {
	return taskManualDTO{
		TypeKey:     m.TypeKey,
		DisplayName: m.DisplayName,
		Purpose:     m.Purpose,
		Fields:      []ManualField{},
		Assignee:    map[string]any{},
		UpdatedTS:   m.UpdatedTS,
	}
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
	account    string
	cost       *float64
	contextPct *float64
	bankedCost *float64
}

// foldActorRuntime folds one actor's telemetry entry, gauge entry, and durable
// banked cost. Nil-map-safe: an actor with no entries folds all-empty.
func foldActorRuntime(tele, gauge map[string]any, banked float64) actorRuntimeFold {
	f := actorRuntimeFold{}
	if a, ok := tele["account"].(string); ok {
		f.account = a
	}
	if c, ok := tele["cost"].(float64); ok {
		f.cost = &c
	}
	if pct, ok := gauge["context_pct"].(float64); ok {
		f.contextPct = &pct
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
		ID:          w.ID,
		Codename:    w.Codename,
		Model:       w.Model,
		Effort:      w.Effort,
		Status:      w.Status,
		TaskID:      w.TaskID,
		CreatedTS:   w.CreatedTS,
		UnreadCount: p.unread,
		Presence:    workerPresence(w, p.now, p.online, p.spawnAt),
		// Machine = the REAL dispatch target resolved to a display label; "" when
		// never dispatched this server run (in-memory spawn target empty since
		// the P7d fold) — the panel renders 「尚未分配」.
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
	rt := foldActorRuntime(p.tele, p.gaugeEntry, w.BankedCost)
	dto.Cost = rt.cost
	dto.ContextPct = rt.contextPct
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
	}
	// refocus_since passes through as epoch seconds (0.0 = unset; the FE maps 0→null
	// so the panel never renders a fabricated time); desired_state echoes the run
	// intent ("" from a pre-column/never-set row reads as online client-side).
	dto.RefocusSince = w.RefocusSince
	dto.DesiredState = w.DesiredState
	return dto
}

// workerPresence projects a worker's liveness onto the ONE member presence
// vocabulary (deriveLiveness — A案 P6; see the DTO field doc for the closed
// set). PURE function of the row + wall clock + the caller-supplied SSE-presence
// fact (online == hub.IsOnline(w.ID) — the SAME single online authority
// PresenceState reads for members; a worker holds its SSE via `ocagent listen`,
// so a died-after-claim session flips offline exactly like a member's would) +
// the in-memory last-start-dispatch anchor (spawnAt; 0 = never dispatched this
// server run, the row's birth anchors the wake window instead).
func workerPresence(w OutsourceWorker, now float64, online bool, spawnAt float64) string {
	if w.Status == WorkerStatusReleased {
		return "" // released / off-panel — never a live row
	}
	anchor := spawnAt
	if anchor <= 0 {
		anchor = w.CreatedTS
	}
	return deriveLiveness(livenessInput{
		Online: online,
		// Owner-explicit stop intent dominates (desired_state mirrors member):
		// stopping while the session winds down, stopped once it is gone —
		// never a fake-green latch (T-f190 lifecycle).
		StopIntent: w.DesiredState == DesiredStateOffline,
		// A fresh wake in flight: the last start dispatch (or the just-minted
		// row awaiting placement) within the member waking TTL. Stale ⇒ the
		// wake failed ⇒ offline — the exact member posture; the FSM rescue
		// (reconcileWorkerLiveness), not the projection, owns recovery.
		WakePending: anchor > 0 && now-anchor <= WakingTTLSecs,
	})
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
	}
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
		options = []string{}
	}
	dto := replyCardDTO{
		ID:            c.ID,
		From:          c.FromMember,
		Kind:          c.Kind,
		Summary:       c.Summary,
		Body:          c.Body,
		Options:       options,
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
			OptionIdx:   c.AnswerOptionIdx,
			Text:        c.AnswerText,
			Attachments: attachmentDTOsFromRefs(c.AnswerAttachments),
		}
	}
	return dto
}

// webhookEndpointDTO is the response shape for one webhook_endpoint (M4 回呼端點,
// §1). The `token` is the opaque secret — it rides ONLY this authenticated
// owner-facing wire (the panel renders the callback URL from it, masking the
// token visually while the copy button yields the full URL). It is NEVER on any
// public or agent-facing wire.
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
