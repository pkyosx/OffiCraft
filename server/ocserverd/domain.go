package main

// domain.go — the pure business-rule ring over the dal.go entities (the Go
// twin of the retired Python domain/{member,chat,chat_read,role_def,lessons,alias,
// user_context}.py). Framework-free by construction: no net/http, no SQL —
// only invariants, closed vocabularies, and the derivations/folds the service
// ring calls.
//
// Deliberate reshapes against the Python originals (single-owner decree +
// state-model spec, docs/design/state-model.md):
//   - no owner scoping and no schema_version (both gone from the Go ontology);
//   - the online fact is ALWAYS an explicit input — the Go Member stores
//     intent only (no online column), so the Python legacy `m.online`
//     fallback parameter has no Go counterpart;
//   - kind is a CLOSED set; the Python bare hire's kind="" folds to
//     "assistant" at the ingest seam (CanonicalKind).

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ── member: kind (closed set) ────────────────────────────────────────────────

// The member kind closed set (schema CHECK): an agent colleague, the
// machine executor (the warden row IS the machine), or an outsourced worker.
// Mirrors the authz literals (adminRoleKey / machineKind) — same vocabulary,
// different concern.
const (
	KindAssistant = "assistant"
	KindWarden    = "warden"
	KindOutsource = "outsource"
)

// CanonicalKind folds an incoming kind onto the closed set. The Python side's
// bare hire writes kind="" (a free-form presentation string there); the Go
// schema requires a legal kind, so blank maps to the default colleague kind
// "assistant". Anything else outside the closed set is refused.
func CanonicalKind(kind string) (string, error) {
	switch kind {
	case "":
		return KindAssistant, nil
	case KindAssistant, KindWarden, KindOutsource:
		return kind, nil
	}
	return "", fmt.Errorf("member kind %q not in {%q, %q, %q}",
		kind, KindAssistant, KindWarden, KindOutsource)
}

// ── member: desired-state vocabulary (owner intent) ──────────────────────────

// The owner's intent under desired-state reconciliation. "uninstall" is the
// machine-lifecycle verb (drives the warden's own removal, then folds back to
// "offline" on the receipt).
const (
	DesiredStateOnline    = "online"
	DesiredStateOffline   = "offline"
	DesiredStateUninstall = "uninstall"
)

// ── member: host namespace ───────────────────────────────────────────────────

// ServerSelfHost is the well-known machine id of the box running the server
// itself — the default host a member is born on. MUST equal the
// desired_machine_id column default in migrations/00001_schema.sql.
const ServerSelfHost = "m-server-self"

// legacyServerSelfHost is the retired pre-namespace-unification host string,
// folded onto ServerSelfHost wherever a stale value can still arrive.
const legacyServerSelfHost = "mbp5"

// CanonicalHost folds the retired legacy host alias onto the canonical
// server-self machine id; every other host passes through unchanged. Applied
// at the observed-host write seam so a stale self-report can never re-poison
// a healed value.
func CanonicalHost(host string) string {
	if host == legacyServerSelfHost {
		return ServerSelfHost
	}
	return host
}

// ── member: presence tri-state derivation ────────────────────────────────────

// The DERIVED presence vocabulary — projected from the live online fact plus
// the durable anchors, never stored.
const (
	MemberPresenceOffline  = "offline"
	MemberPresenceWaking   = "waking"
	MemberPresenceOnline   = "online"
	MemberPresenceStopping = "stopping"
	MemberPresenceStopped  = "stopped"
)

// WakingTTLSecs: a phase="waking" signal this old (seconds), with no online
// session having come up, falls back to offline — the wake failed. Sized at
// 3× the runtime's 30s presence heartbeat.
const WakingTTLSecs = 90.0

// StoppingTimeoutSecs: once stopping_since is set, a still-online member has
// this long to wind down before a stuck collect is force-killed.
const StoppingTimeoutSecs = 120.0

// livenessInput is the normalized input to the shared liveness kernel
// (deriveLiveness): the two actor kinds (member / outsource worker) map their
// own durable anchors onto these three facts and read back the SAME unified
// vocabulary. Keeping the projection LOGIC in one place is the P2 presence
// convergence (§3 state-model: online is a pure SSE projection, everything else
// is derived from it plus the durable intent anchors).
type livenessInput struct {
	// Online is the live SSE-connection fact — the SINGLE authority for BOTH
	// kinds (hub.IsOnline). Never a DB flag, never a warden receipt.
	Online bool
	// StopIntent is owner-explicit stop-in-effect: a graceful shutdown / hold
	// that dominates the projection so a stopping actor never latches a false
	// green while its process winds down.
	StopIntent bool
	// WakePending is a fresh, not-yet-connected wake anchor (owner wants it up
	// and the wake is still within its freshness window). Only consulted when
	// offline; a stale anchor is a failed wake and reads offline.
	WakePending bool
}

// deriveLiveness is the ONE shared liveness kernel for both actor kinds. Unified
// vocabulary: online / waking / stopping / stopped / offline. The exit
// (owner-explicit stop) semantics overlay as a lifecycle mode ON TOP of the raw
// online fact:
//
//   - StopIntent dominates: online ⇒ stopping (still collecting), !online ⇒
//     stopped (shutdown done).
//   - else online ⇒ online.
//   - else a fresh WakePending ⇒ waking (the wake is in flight).
//   - else offline.
//
// Pure. Callers: PresenceState (member) and workerPresence (outsource — the
// A案 P6 convergence, owner-gated rc-25d6557629b5: the former spawn_state
// starting/stuck projection is retired; both actor kinds read back this ONE
// vocabulary).
func deriveLiveness(in livenessInput) string {
	if in.StopIntent {
		if in.Online {
			return MemberPresenceStopping
		}
		return MemberPresenceStopped
	}
	if in.Online {
		return MemberPresenceOnline
	}
	if in.WakePending {
		return MemberPresenceWaking
	}
	return MemberPresenceOffline
}

// PresenceState projects a member's presence at now — a thin mapping of the
// member's durable anchors onto the shared liveness kernel (deriveLiveness).
// Pure: online is the caller-supplied SSE-connection fact (the ONLY authority —
// never a DB flag, never a warden receipt: a stop receipt can lie while the
// process is alive and still answering chat, so SSE connected ⇒ never stopped).
//
// A set stopping_since (the graceful-shutdown signal) is the member's StopIntent
// and takes precedence over every other projection. The waking projection needs
// owner intent (desired_state online) plus a fresh waking_since; a stale waking
// signal is a failed wake and reads offline.
func PresenceState(m Member, now float64, online bool) string {
	return deriveLiveness(livenessInput{
		Online:     online,
		StopIntent: m.StoppingSince > 0.0,
		WakePending: m.DesiredState == DesiredStateOnline &&
			m.WakingSince > 0.0 &&
			now-m.WakingSince <= WakingTTLSecs,
	})
}

// WakingTimedOut reports a waking member whose startup window lapsed with no
// online session (failed wake → should fall to offline). Pure.
func WakingTimedOut(m Member, now float64, online bool) bool {
	return !online &&
		m.DesiredState == DesiredStateOnline &&
		m.WakingSince > 0.0 &&
		now-m.WakingSince > WakingTTLSecs
}

// StoppingTimedOut reports a stopping member whose shutdown grace lapsed
// (collect stuck → force-kill). Pure; a reconciliation trigger only — it
// never changes the presence projection.
func StoppingTimedOut(m Member, now float64, online bool) bool {
	return online &&
		m.StoppingSince > 0.0 &&
		now-m.StoppingSince > StoppingTimeoutSecs
}

// ── member: display Member-ID projection ─────────────────────────────────────

// MemberNo derives the display Member-ID ("MB-XXX###") for a roster id: a
// stable SHA-256 of the id mapped to three uppercase letters and three
// digits. Deterministic and stateless — a display label only, never a lookup
// key. Byte-for-byte the Python domain.member.member_no derivation.
func MemberNo(memberID string) string {
	sum := sha256.Sum256([]byte(memberID))
	n := binary.BigEndian.Uint64(sum[:8])
	letters := make([]byte, 3)
	for i := range letters {
		letters[i] = byte('A' + n%26)
		n /= 26
	}
	return fmt.Sprintf("MB-%s%03d", letters, n%1000)
}

// ── member: random founding-member name pool ─────────────────────────────────

// MemberNamePool holds the Mira-style short English given names a role-create
// with no member_name picks from — never "Mira" itself, so the seed identity
// stays unmistakable.
var MemberNamePool = []string{
	"Nova", "Kai", "Ravi", "Luna", "Iris", "Milo", "Zara", "Theo",
	"Aria", "Ezra", "Vera", "Nico", "Suki", "Remy", "Isla", "Otis",
	"Faye", "Juno", "Cleo", "Enzo", "Mika", "Wren", "Lyra", "Dax",
}

// PickMemberName picks a random display name colliding with none in taken
// (trimmed, case-insensitive). When the whole pool is taken it falls back to
// "<PoolName>-<n>" numeric-suffix candidates until one is free — it always
// returns a fresh name. rng is injectable for deterministic tests (nil → a
// fresh system-seeded source).
func PickMemberName(taken []string, rng *rand.Rand) string {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	takenFold := make(map[string]bool, len(taken))
	for _, t := range taken {
		takenFold[strings.ToLower(strings.TrimSpace(t))] = true
	}
	var available []string
	for _, n := range MemberNamePool {
		if !takenFold[strings.ToLower(n)] {
			available = append(available, n)
		}
	}
	if len(available) > 0 {
		return available[rng.IntN(len(available))]
	}
	for {
		candidate := fmt.Sprintf("%s-%d",
			MemberNamePool[rng.IntN(len(MemberNamePool))], 2+rng.IntN(998))
		if !takenFold[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// ── entity invariants (the Python __post_init__ checks, sans owner scoping) ──

// ValidateMember enforces the member entity invariants: a non-empty id (the
// roster identity and attribution key) and a kind on the closed set (blank is
// an ingest-seam concern — CanonicalKind — never a stored value).
func ValidateMember(m Member) error {
	if m.ID == "" {
		return errors.New("member requires a non-empty id")
	}
	if m.Kind != KindAssistant && m.Kind != KindWarden && m.Kind != KindOutsource {
		return fmt.Errorf("member %s: kind %q not in {%q, %q, %q}",
			m.ID, m.Kind, KindAssistant, KindWarden, KindOutsource)
	}
	return nil
}

// ValidateChatMessage enforces the chat-message invariant: a non-empty id.
func ValidateChatMessage(m ChatMessage) error {
	if m.ID == "" {
		return errors.New("chat message requires a non-empty id")
	}
	return nil
}

// ValidateChatAttachment enforces the attachment invariant: a non-empty id.
func ValidateChatAttachment(a ChatAttachment) error {
	if a.ID == "" {
		return errors.New("chat attachment requires a non-empty id")
	}
	return nil
}

// ValidateChatRead enforces the read-receipt invariants: a watermark is
// meaningless without both conversation participants.
func ValidateChatRead(r ChatRead) error {
	if r.ReaderID == "" {
		return errors.New("chat read receipt requires a non-empty reader_id")
	}
	if r.PeerID == "" {
		return errors.New("chat read receipt requires a non-empty peer_id")
	}
	return nil
}

// ValidateRoleDef enforces the role-overlay invariant: a non-empty role key.
func ValidateRoleDef(rd RoleDef) error {
	if rd.RoleKey == "" {
		return errors.New("role def requires a non-empty role_key")
	}
	return nil
}

// ValidateLessons enforces the lessons-overlay invariants: the composite
// (role_key, task_type) key must be fully populated.
func ValidateLessons(l Lessons) error {
	if l.RoleKey == "" {
		return errors.New("lessons requires a non-empty role_key")
	}
	if l.TaskType == "" {
		return errors.New("lessons requires a non-empty task_type")
	}
	return nil
}

// ValidateAccountAlias / ValidateMachineAlias enforce the overlay invariant:
// an alias without its stable dedupe key labels nothing.
func ValidateAccountAlias(a AccountAlias) error {
	if a.Account == "" {
		return errors.New("account alias requires a non-empty account")
	}
	return nil
}

func ValidateMachineAlias(a MachineAlias) error {
	if a.MachineID == "" {
		return errors.New("machine alias requires a non-empty machine_id")
	}
	return nil
}

// webhookEndpointIDPattern is the closed character set for a user-chosen
// endpoint id: ASCII letters/digits/underscore/hyphen only — no whitespace, no
// special chars (SPEC 核心名詞: 不允特殊符號, 不能含有空白等等). It doubles as the
// management address key, so it must be URL/path safe.
var webhookEndpointIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// webhookEndpointIDMaxLen caps the id length (a display chip / address key, not
// free text).
const webhookEndpointIDMaxLen = 64

// ValidateWebhookEndpointID enforces the endpoint-id invariant at the create
// seam: non-empty, within the length cap, closed character set.
func ValidateWebhookEndpointID(endpointID string) error {
	if endpointID == "" {
		return errors.New("endpoint id cannot be blank")
	}
	if len(endpointID) > webhookEndpointIDMaxLen {
		return fmt.Errorf("endpoint id must be at most %d characters", webhookEndpointIDMaxLen)
	}
	if !webhookEndpointIDPattern.MatchString(endpointID) {
		return errors.New("endpoint id may contain only letters, digits, '_' and '-' (no spaces or special characters)")
	}
	return nil
}

// ValidWebhookStatus reports whether status is in the closed set (the toggle
// domain).
func ValidWebhookStatus(status string) bool {
	return status == WebhookStatusEnabled || status == WebhookStatusDisabled
}

// ValidWebhookPlatform reports whether platform is in the closed verification
// preset set (generic/slack/github — migrations/00012).
func ValidWebhookPlatform(platform string) bool {
	return platform == WebhookPlatformGeneric ||
		platform == WebhookPlatformSlack ||
		platform == WebhookPlatformGithub
}

// ── chat: attachment refs (the only message→blob linkage) ────────────────────

// AttachmentRefIDs extracts the attachment blob ids a message's meta refs —
// meta["attachments"] is BY DECREE the single source of truth for the
// message→attachment linkage (no FK edge). Non-conforming meta (free-form
// JSON) yields no refs; blank ids are skipped.
func AttachmentRefIDs(meta map[string]any) []string {
	refs, _ := meta["attachments"].([]any)
	var out []string
	for _, r := range refs {
		ref, _ := r.(map[string]any)
		if id, _ := ref["id"].(string); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// ── chat_read: unread counts (the pure watermark inverse) ────────────────────

// UnreadCounts derives per-peer unread message counts for reader — the pure
// inverse of the read watermark. A message counts when it is ADDRESSED TO the
// reader and newer than the reader's last-read watermark for that peer (no
// receipt ⇒ watermark 0 ⇒ every addressed message counts). Messages between
// two other participants never count, and neither do the reader's own sends
// (both by the recipient==reader scope). Watermarks are per-reader: another
// reader's receipt never clears anything.
func UnreadCounts(messages []ChatMessage, receipts []ChatRead, reader string) map[string]int {
	watermark := map[string]float64{}
	for _, r := range receipts {
		if r.ReaderID == reader {
			watermark[r.PeerID] = r.LastReadTS
		}
	}
	counts := map[string]int{}
	for _, m := range messages {
		if m.Recipient == reader && m.TS > watermark[m.Sender] {
			counts[m.Sender]++
		}
	}
	return counts
}

// ── role_def: overlay ⊕ seed fold + custom-role template ─────────────────────

// CustomRoleTemplateMD is the role-definition scaffold a freshly created
// CUSTOM role starts from: two fixed fill-me sections (identity / duties) so
// the owner edits a scaffold instead of a blank page. Content, not a file
// seed.
const CustomRoleTemplateMD = `# 角色定義

## 你是誰

（待填：這個角色的身分與定位——用一兩句話說明「你是誰」、在辦公室裡站什麼位置、面對 owner 與其他成員時以什麼視角說話。）

## 你做什麼

（待填：這個角色的職責與工作方式——負責哪些事、怎麼做事、輸出長什麼樣、與 owner 及其他成員怎麼協作、什麼事不歸你管。）
`

// FoldedRoleDef is the effective role definition a fold yields: IsDefault
// marks an untouched seed (no live overlay); IsSeed keys on whether a FILE
// SEED exists for the role — an edited seed role stays a seed role
// (resettable, not deletable), a custom role (overlay only) is deletable.
type FoldedRoleDef struct {
	Key          string
	Name         string
	DefinitionMD string
	IsDefault    bool
	IsSeed       bool
}

// FoldRoleDef folds one role definition: owner overlay ⊕ file seed. The
// overlay is SELF-CONTAINED (full name + definition_md, never a partial
// patch), so a live overlay wins whole; a tombstoned overlay (the reset seam)
// reads as absent and falls back to the seed. Neither a seed nor a live
// overlay → nil (unknown role; the caller fails closed).
func FoldRoleDef(key string, overlay *RoleDef, seedName, seedMD string, hasSeed bool) *FoldedRoleDef {
	if overlay != nil && !overlay.Tombstoned {
		return &FoldedRoleDef{
			Key:          key,
			Name:         overlay.Name,
			DefinitionMD: overlay.DefinitionMD,
			IsDefault:    false,
			IsSeed:       hasSeed,
		}
	}
	if !hasSeed {
		return nil
	}
	return &FoldedRoleDef{
		Key:          key,
		Name:         seedName,
		DefinitionMD: seedMD,
		IsDefault:    true,
		IsSeed:       true,
	}
}

// ── lessons: per-role overlay ⊕ shared seed fold ─────────────────────────────

// FoldLessons folds a per-role lessons doc: owner overlay ⊕ file seed.
// Lessons are PER-ROLE (agents sharing a role share one overlay), but every
// role falls back to the SAME shared seed text until its own overlay diverges
// it; a tombstoned overlay (reset) reads as absent.
func FoldLessons(overlay *Lessons, seedText string) (text string, isDefault bool) {
	if overlay == nil || overlay.Tombstoned {
		return seedText, true
	}
	return overlay.Text, false
}

// ── user_context: the ADDITIVE user-custom block fold ────────────────────────

// FoldUserContext folds the owner's user-custom ADDITIVE boot-context block.
// Its seed is EMPTY: no row (or a tombstoned one) folds to ""/default and the
// assembled boot context skips the block entirely — the owner's text only
// ever appends its own section, never replaces the read-only seed blocks.
func FoldUserContext(row *UserContext) (text string, isDefault bool) {
	if row == nil || row.Tombstoned {
		return "", true
	}
	return row.Text, false
}

// ── tasks: closed vocabularies (M3 task system) ──────────────────────────────

// The task status closed set: the eight-state machine (SPEC 核心名詞 seven +
// reassigning, T-160e). done/terminated/duplicated are TERMINAL. This set is
// enforced in code alone (ValidTaskStatus) — migrations/00011 dropped the
// DB-level status CHECK so a new state costs zero schema churn (owner-approved
// design, T-02c9 point 4). duplicated is reached ONLY through mark_duplicate;
// reassigning is entered ONLY through the owner/admin reassign action (POST
// /api/tasks/{id}/reassign) — the handover hold while the NEW executor reads
// up; the new executor alone leaves it (reassigning → in_progress on the
// report table below, executor-guarded).
const (
	TaskStatusNotStarted      = "not_started"
	TaskStatusInProgress      = "in_progress"
	TaskStatusWaitingOwner    = "waiting_owner"
	TaskStatusWaitingExternal = "waiting_external"
	TaskStatusReassigning     = "reassigning"
	TaskStatusDone            = "done"
	TaskStatusTerminated      = "terminated"
	TaskStatusDuplicated      = "duplicated"
)

// The task LOCK closed set — an ORTHOGONAL dimension to status (T-9ca5). Since
// the owner's "任務狀態全推導" ruling, task.status is PURELY derived from the
// steps (DeriveTaskStatus); a lock is a SYSTEM hold layered on TOP that the
// derivation never sets nor clears. reassigning — the handover hold while a NEW
// executor reads up — used to BE a status (freezing the derived work state); it
// is now this lock, so the cockpit shows the honest derived status (e.g.
// in_progress) AND the reassigning lock badge together. A lock is entered by the
// reassign action and left ONLY by the new executor's dedicated claim action
// (POST /api/tasks/{id}/claim — clears the lock, never a status report).
const (
	TaskLockNone        = ""
	TaskLockReassigning = "reassigning"
)

// ValidTaskLock reports task.lock closed-set membership (the write-path guard).
func ValidTaskLock(l string) bool {
	switch l {
	case TaskLockNone, TaskLockReassigning:
		return true
	}
	return false
}

// The task priority closed set. Frozen is a PRIORITY (pause-pushing, sorts
// last), deliberately not a status (SPEC §3.3).
const (
	TaskPriorityHigh   = "high"
	TaskPriorityMid    = "mid"
	TaskPriorityLow    = "low"
	TaskPriorityFrozen = "frozen"
)

// The executor-track closed set. "Unassigned" is NOT a kind: an outsource
// task awaiting the scheduler is Kind=outsource with ExecutorID == "".
const (
	TaskExecutorMember    = "member"
	TaskExecutorOutsource = "outsource"
)

// The task_step status closed set (five states; SPEC 狀態徽章). done and
// superseded are the step's terminal states; a terminated TASK still freezes
// its steps as they stand. superseded (T-1aea) is minted by submit_plan alone:
// a replan freezes a step whose latest bound reply card was already
// answered/expired — the question-and-answer history must survive the replan —
// unless the fresh plan re-lists the node by name (then the live row simply
// continues). It is never agent-reportable and never re-armable.
const (
	StepStatusPending      = "pending"
	StepStatusInProgress   = "in_progress"
	StepStatusWaitingOwner = "waiting_owner"
	// The step is blocked on the outside world (a third party, a time window).
	// waiting_external moves DOWN to the step level (T-9ca5): the agent reports
	// it via update_step_status with a waiting_reason, exactly as the old
	// task-level waiting_external worked, and the task status is DERIVED from it
	// (DeriveTaskStatus). Unlike waiting_owner it IS agent-reportable (no card
	// lifecycle owns it), so it sits on agentStepTransitions below.
	StepStatusWaitingExternal = "waiting_external"
	StepStatusDone            = "done"
	StepStatusSuperseded      = "superseded"
)

// The outsource worker lifecycle closed set — a DERIVED projection over the
// member row since the P7d fold (roster_status + activated_ts; see
// dal_tasks.go workerStatusFromMember), no longer a stored column. The wire
// vocabulary is frozen (outsourceWorkerDTO.status), so the set stays.
const (
	WorkerStatusAssigned = "assigned"
	WorkerStatusActive   = "active"
	WorkerStatusReleased = "released"
)

// The task_artifact kind closed set (schema CHECK; T-3dc5). file/image
// reference a chat_attachment blob (one blob mechanism, not two); link is a
// bare URL with no blob (the part the chat-attachment model cannot express).
const (
	ArtifactKindFile  = "file"
	ArtifactKindImage = "image"
	ArtifactKindLink  = "link"
)

// ValidArtifactKind reports task_artifact.kind closed-set membership (the
// add_task_artifact 400 guard).
func ValidArtifactKind(k string) bool {
	switch k {
	case ArtifactKindFile, ArtifactKindImage, ArtifactKindLink:
		return true
	}
	return false
}

// ValidTaskStatus / ValidTaskPriority / ValidStepStatus report closed-set
// membership (the handlers' 400 guards).
func ValidTaskStatus(s string) bool {
	switch s {
	// reassigning is NO LONGER a status (T-9ca5): it moved to task.lock, an
	// orthogonal dimension.
	case TaskStatusNotStarted, TaskStatusInProgress, TaskStatusWaitingOwner,
		TaskStatusWaitingExternal, TaskStatusDone,
		TaskStatusTerminated, TaskStatusDuplicated:
		return true
	}
	return false
}

func ValidTaskPriority(p string) bool {
	switch p {
	case TaskPriorityHigh, TaskPriorityMid, TaskPriorityLow, TaskPriorityFrozen:
		return true
	}
	return false
}

func ValidStepStatus(s string) bool {
	switch s {
	case StepStatusPending, StepStatusInProgress, StepStatusWaitingOwner,
		StepStatusWaitingExternal, StepStatusDone, StepStatusSuperseded:
		return true
	}
	return false
}

// StepIsTerminal reports the two step terminal states (done / superseded):
// no current-step candidacy, no gate re-arm, no agent transition in or out —
// every consumer treats a terminal step as immutable history (T-1aea).
func StepIsTerminal(status string) bool {
	return status == StepStatusDone || status == StepStatusSuperseded
}

// TaskIsTerminal reports the three terminal statuses (dedupe scope + the 409
// write guard: no agent push, no plan, no gate lands on a closed task).
func TaskIsTerminal(status string) bool {
	return status == TaskStatusDone || status == TaskStatusTerminated ||
		status == TaskStatusDuplicated
}

// ── tasks: agent-reported state machine (contract §B.1) ──────────────────────

// agentTaskTransitions is the CLOSED legal-transition set of the agent report
// path (POST /api/tasks/{id}/status). waiting_owner is NOT on either side of
// this table: it is entered ONLY by opening a card (open_gate / create_reply_card
// auto-bind) and LEFT ONLY when that card is answered — the server itself
// restores the task to in_progress on answer (releaseCardHold).
// So the agent neither reports INTO waiting_owner (the handler 400s that, not
// its lever) nor OUT of it (a report from waiting_owner is a 409 — the card
// lifecycle owns that exit, the agent cannot bail out unilaterally). Row 8
// (→ terminated) is the owner's terminate alone. Any other move outside the set
// is a 409 at the handler.
var agentTaskTransitions = map[[2]string]bool{
	{TaskStatusNotStarted, TaskStatusInProgress}:      true, // start executing
	{TaskStatusInProgress, TaskStatusWaitingExternal}: true, // blocked on the outside world
	{TaskStatusWaitingExternal, TaskStatusInProgress}: true, // the external condition landed
	{TaskStatusInProgress, TaskStatusDone}:            true, // wrapped up (terminal)
	// The reassign takeover (reassigning → in_progress) is GONE from this table
	// (T-9ca5): reassigning is now task.lock, not a status, so the new executor's
	// takeover is the dedicated claim action (POST /api/tasks/{id}/claim, which
	// clears the lock) — never a status report. status stays derived throughout.
}

// CanAgentTaskTransition reports whether the agent report path may move a
// task from → to. Pure; the caller supplies the 409 on false.
func CanAgentTaskTransition(from, to string) bool {
	return agentTaskTransitions[[2]string{from, to}]
}

// agentStepTransitions is the step twin (contract §B.2): pending →
// in_progress → done. waiting_owner is NOT on either side, exactly like the
// task table: the card-open paths set it (open_gate / create_reply_card
// auto-bind — the handler 400s an agent report INTO it), and the answer path
// restores the step to in_progress (releaseCardHold — a report
// OUT of it is a 409). After the server restores the step, the agent advances
// it in_progress → done as usual; if the answer did NOT settle the question the
// agent opens a fresh card and the step re-enters waiting_owner.
var agentStepTransitions = map[[2]string]bool{
	{StepStatusPending, StepStatusInProgress}: true,
	{StepStatusInProgress, StepStatusDone}:    true,
	// waiting_external is the step's own "blocked on the outside world" lever
	// (T-9ca5), mirroring the retired task-level pair. Unlike waiting_owner it is
	// agent-reportable: the agent parks the step here with a waiting_reason and
	// resumes when the external condition lands. done is reached only from
	// in_progress, so a waiting step returns to in_progress first.
	{StepStatusInProgress, StepStatusWaitingExternal}: true,
	{StepStatusWaitingExternal, StepStatusInProgress}: true,
}

// CanAgentStepTransition reports whether the step report path may move a step
// from → to.
func CanAgentStepTransition(from, to string) bool {
	return agentStepTransitions[[2]string{from, to}]
}

// ── tasks: display projections ────────────────────────────────────────────────

// TaskNo derives the display number ("T-XXXX") from a task id ("t-<hex12>"):
// the first four hex chars after the prefix (kyle ruling H3 — display-only,
// collisions possible, never a lookup key).
func TaskNo(taskID string) string {
	hex := strings.TrimPrefix(taskID, "t-")
	if len(hex) > 4 {
		hex = hex[:4]
	}
	return "T-" + hex
}

// TaskProgress counts the flattened leaf progress (SPEC §3.1: every step row
// is one leaf — parallel items are separate rows, so no extra flattening).
// superseded rows are pure history (T-1aea): neither a to-do nor an
// achievement, so they count toward neither side (dal.AllTaskStepProgress is
// the SQL twin — keep them agreeing).
func TaskProgress(steps []TaskStep) (done, total int) {
	for _, st := range steps {
		if st.Status == StepStatusSuperseded {
			continue
		}
		total++
		if st.Status == StepStatusDone {
			done++
		}
	}
	return done, total
}

// DeriveTaskStatus computes a task's status PURELY from its steps — the single
// rule, zero exceptions (owner T-9ca5: "任務狀態要照實呈現，不應該有例外"). It
// returns ONLY the five derived work states; it never returns a lock
// (reassigning / waiting_capacity live in task.lock, orthogonal) nor an explicit
// terminal (terminated / duplicated are owner/system decisions, not derivable
// from steps — the caller keeps those and only applies this to non-terminal,
// unlocked tasks). superseded steps are pure history and count on NEITHER side,
// exactly as TaskProgress. Priority (SPEC §3, owner-ordered):
// waiting_owner > waiting_external > done > (nothing started) > in_progress.
func DeriveTaskStatus(steps []TaskStep) string {
	active := 0
	anyWaitingOwner, anyWaitingExternal := false, false
	allDone, allPending := true, true
	for _, st := range steps {
		if st.Status == StepStatusSuperseded {
			continue
		}
		active++
		switch st.Status {
		case StepStatusWaitingOwner:
			anyWaitingOwner = true
		case StepStatusWaitingExternal:
			anyWaitingExternal = true
		}
		if st.Status != StepStatusDone {
			allDone = false
		}
		if st.Status != StepStatusPending {
			allPending = false
		}
	}
	switch {
	case active == 0:
		return TaskStatusNotStarted // zero steps or all superseded — 尚未執行
	case anyWaitingOwner:
		return TaskStatusWaitingOwner
	case anyWaitingExternal:
		return TaskStatusWaitingExternal
	case allDone:
		return TaskStatusDone
	case allPending:
		return TaskStatusNotStarted // nothing started yet
	default:
		return TaskStatusInProgress
	}
}

// RecomputeTaskStatus is the DERIVATION OWNER (T-9ca5): the single place every
// step-mutation seam calls to re-project a task's status (and its display
// waiting_reason) from its steps, so the cockpit never shows a status the steps
// contradict. It mutates t in place. It leaves the two kinds of state the
// derivation MUST NOT own untouched:
//   - explicit terminals (terminated / duplicated) — owner/system decisions,
//     frozen once set (done is derivable and IS recomputed).
//
// The lock (task.lock) is orthogonal and never touched here — a reassigning task
// keeps its honestly-derived status alongside the lock badge. The display
// waiting_reason mirrors the first waiting_external step's reason (empty when no
// step is waiting_external), replacing the retired task-level waiting_reason.
func RecomputeTaskStatus(t *Task, steps []TaskStep) {
	switch t.Status {
	case TaskStatusTerminated, TaskStatusDuplicated:
		return
	}
	t.Status = DeriveTaskStatus(steps)
	reason := ""
	for _, st := range steps {
		if st.Status == StepStatusWaitingExternal {
			reason = st.WaitingReason
			break
		}
	}
	t.WaitingReason = reason
}

// ── tasks: parallel (fork-join) plan shape ───────────────────────────────────

// ValidatePlanParallelShape guards the submit_plan write seam against
// parallel-group shapes the timeline cannot honestly render (the FE folds
// CONSECUTIVE steps sharing a non-empty parallel_group into ONE stage):
//  1. a gate must not sit inside a parallel group — an armed gate flips the
//     WHOLE task to waiting_owner, which would lie while sibling lanes are
//     still running; the gate belongs after the group's join step;
//  2. steps sharing a parallel_group must be consecutive — a split group
//     silently renders as two stages (a visual lie), so the write seam
//     refuses it instead of tolerating it;
//  3. a group the fresh plan uses must hold at least two steps overall — a
//     one-lane "parallel" stage is noise (drop the group key instead).
//
// kept is the task's preserved prefix (submit_plan keeps done AND
// answered-card steps — frozen to superseded or re-listed alive — ahead of
// the fresh plan; dal.ReplaceTaskPlan), fresh the submitted steps; checks
// 2/3 run over the COMBINED timeline exactly as it will be stored and
// rendered, while 1 and the rule-3 trigger look only at fresh so a legacy
// kept-only group never blocks a legitimate replan. Returns "" when the
// shape is legal, else the human 400 message.
func ValidatePlanParallelShape(kept, fresh []TaskStep) string {
	for _, st := range fresh {
		if st.IsGate && st.ParallelGroup != "" {
			return "step '" + st.Name + "': a gate step cannot sit inside a parallel group — " +
				"put the gate on its own step after the group's join step"
		}
	}
	combined := make([]TaskStep, 0, len(kept)+len(fresh))
	combined = append(combined, kept...)
	combined = append(combined, fresh...)
	lastIdx := map[string]int{}
	count := map[string]int{}
	for i, st := range combined {
		g := st.ParallelGroup
		if g == "" {
			continue
		}
		if prev, seen := lastIdx[g]; seen && prev != i-1 {
			return "steps sharing parallel_group '" + g + "' must sit next to each other — " +
				"move them together, or give the later run a different group key"
		}
		lastIdx[g] = i
		count[g]++
	}
	for _, st := range fresh {
		if g := st.ParallelGroup; g != "" && count[g] < 2 {
			return "parallel_group '" + g + "' holds only one step — running in parallel takes " +
				"at least two; drop the parallel_group to keep the step sequential"
		}
	}
	return ""
}

// ── tasks: outsource codename derivation (Phase 2 scheduler consumes) ────────

// CodenamePrefix maps a model name onto the codename letter (SPEC 核心名詞:
// O-xx Opus / S-xx Sonnet / H-xx Haiku). An unrecognised model gets the
// honest "X" marker rather than masquerading as a known family.
func CodenamePrefix(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return "O"
	case strings.Contains(m, "sonnet"):
		return "S"
	case strings.Contains(m, "haiku"):
		return "H"
	}
	return "X"
}

// DeriveCodename mints the next codename for a model given every codename
// ever issued: <prefix>-<MAX+1> over the SAME prefix (a globally ascending
// per-family sequence — never reused, single-writer SQLite makes MAX+1 safe).
func DeriveCodename(model string, existing []string) string {
	prefix := CodenamePrefix(model)
	max := 0
	for _, c := range existing {
		rest, ok := strings.CutPrefix(c, prefix+"-")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(rest); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s-%d", prefix, max+1)
}

// ── tasks: manual fields + dedupe-key derivation ─────────────────────────────

// ManualField is one "需要哪些資訊" input field of a task manual (the
// task_manual.fields JSON element).
type ManualField struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	IsKey    bool   `json:"is_key"`
}

// ParseManualFields decodes the stored fields JSON. Non-conforming JSON is an
// error (the write path validates, so a bad blob is corruption, not input).
func ParseManualFields(blob string) ([]ManualField, error) {
	if blob == "" {
		return nil, nil
	}
	var out []ManualField
	if err := json.Unmarshal([]byte(blob), &out); err != nil {
		return nil, fmt.Errorf("task_manual fields: bad JSON: %w", err)
	}
	return out, nil
}

// normalizeFieldKey folds an input/field key for MATCHING: lowercased and
// outer-trimmed. Inner whitespace is deliberately preserved ("PR  Link" with a
// double space stays distinct from "PR Link") — the fold only forgives the two
// mismatches actually seen in the wild (case + surrounding space) and never
// over-merges. Manual field names are stored outer-trimmed but never
// case-folded, and caller-supplied input keys carry arbitrary case, so both
// sides must pass through this before comparison (required-check AND dedupe use
// the same fold — they must never diverge, or a task can pass one and fail the
// other).
func normalizeFieldKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NormalizeInputs re-keys the create-time inputs by normalizeFieldKey so
// manual-field lookups (required, is_key, dedupe) are case/space insensitive.
// Iteration is over SORTED original keys so the first-wins outcome is
// deterministic: when two original keys fold to the same normalized key (e.g.
// "PR Link" and "pr link" both present), the first in sort order is kept and
// every later collider's ORIGINAL name is returned so the caller can warn about
// the ambiguity. A nil/empty map yields an empty (non-nil) map and no
// collisions.
func NormalizeInputs(inputs map[string]any) (map[string]any, []string) {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	norm := make(map[string]any, len(inputs))
	var collisions []string
	for _, k := range keys {
		nk := normalizeFieldKey(k)
		if _, seen := norm[nk]; seen {
			collisions = append(collisions, k)
			continue
		}
		norm[nk] = inputs[k]
	}
	return norm, collisions
}

// InputValueMissing reports whether a manual field has no usable create-time
// value: absent, JSON null, or a string that is empty after trimming. Non-string
// values (numbers, bools, objects) always count as present — mirroring
// DedupeKeyValue, which renders them as JSON literals. This is the single
// emptiness notion the required-input, is_key (K1), and dedupe checks all share.
func InputValueMissing(v any, ok bool) bool {
	if !ok || v == nil {
		return true
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// DedupeKeyValue derives a task's identity-key VALUE from the manual's field
// definitions + the create-time inputs: the is_key fields' values in the
// manual's declaration order, unit-separator-joined (composite keys cannot
// collide across boundaries). Field↔input matching is normalized
// (normalizeFieldKey — case/space insensitive) so "PR Link" and "pr link" hit
// the same value. Non-string values render as their JSON literal. No key fields
// (or no values at all) → "" (no dedupe basis). The VALUE itself is only
// trimmed, never case-folded — values can be case-sensitive (URLs, paths).
func DedupeKeyValue(fields []ManualField, inputs map[string]any) string {
	normInputs, _ := NormalizeInputs(inputs)
	var parts []string
	any := false
	for _, f := range fields {
		if !f.IsKey {
			continue
		}
		v, ok := normInputs[normalizeFieldKey(f.Name)]
		part := ""
		if ok && v != nil {
			if s, isStr := v.(string); isStr {
				part = strings.TrimSpace(s)
			} else if raw, err := json.Marshal(v); err == nil {
				part = string(raw)
			}
		}
		if part != "" {
			any = true
		}
		parts = append(parts, part)
	}
	if !any {
		return ""
	}
	return strings.Join(parts, "\x1f")
}

// ── alias: display-name overlay fold ─────────────────────────────────────────

// DisplayName folds an alias overlay over a stable id (an account tag or a
// machine id): the overlay label when one is set, else the id itself. Purely
// additive — the id stays the dedupe key; only the presented label changes.
// names is the dal fold input (AccountDisplayNames / MachineDisplayNames);
// an empty label reads as no overlay.
func DisplayName(id string, names map[string]string) string {
	if name := names[id]; name != "" {
		return name
	}
	return id
}
