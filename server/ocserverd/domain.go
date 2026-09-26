package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// member.kind values; they must match the schema CHECK and authz.go machineKind.
const (
	KindStaff     = "staff"
	KindWarden    = "warden"
	KindOutsource = "outsource"
)

func CanonicalKind(kind string) (string, error) {
	switch kind {
	case "":
		return KindStaff, nil
	case KindStaff, KindWarden, KindOutsource:
		return kind, nil
	}
	// Built from runes on purpose: a literal would be rewritten by the repo-wide
	// "assistant"→"staff" replacement, silently turning this branch dead.
	if kind == string([]rune{'a', 's', 's', 'i', 's', 't', 'a', 'n', 't'}) {
		return "", fmt.Errorf(
			"member kind %q was renamed to %q (T-48); the closed set is {%q, %q, %q}",
			kind, KindStaff, KindStaff, KindWarden, KindOutsource)
	}
	return "", fmt.Errorf("member kind %q not in {%q, %q, %q}",
		kind, KindStaff, KindWarden, KindOutsource)
}

// DesiredStateUninstall drives the warden's own removal, then folds back to
// offline on its receipt.
const (
	DesiredStateOnline    = "online"
	DesiredStateOffline   = "offline"
	DesiredStateUninstall = "uninstall"
)

// ServerSelfHost must equal the desired_machine_id column default in
// migrations/00001_schema.sql.
const ServerSelfHost = "m-server-self"

// legacyServerSelfHost is the pre-namespace-unification host string that stale
// self-reports can still carry.
const legacyServerSelfHost = "mbp5"

func CanonicalHost(host string) string {
	if host == legacyServerSelfHost {
		return ServerSelfHost
	}
	return host
}

const (
	MemberPresenceOffline  = "offline"
	MemberPresenceWaking   = "waking"
	MemberPresenceOnline   = "online"
	MemberPresenceStopping = "stopping"
	MemberPresenceStopped  = "stopped"
)

// WakingTTLSecs must stay a comfortable multiple of lifecycleCadenceSecs so an
// in-flight wake is re-examined several times before it is declared failed.
const WakingTTLSecs = 120.0

// StoppingTimeoutSecs: past this, a still-online member's stuck collect is
// force-killed.
const StoppingTimeoutSecs = 120.0

// SoftOffboardGraceSecs is NOT a deadline: soft offboard and refocus have no
// timer (owner rc-27d1710174dd, rc-c540367065ad). Its only use,
// clearStaleStoppingOnOnline, treats it as a SILENCE window (how long a
// close-out may report nothing before its stopping anchor counts as residue),
// so a member still filing reports stays stopping and the owner keeps the
// force-stop button. 0 restores the old timed wind-down.
const SoftOffboardGraceSecs = 600.0

type presenceInput struct {
	Online      bool
	StopIntent  bool
	WakePending bool
}

func derivePresence(in presenceInput) string {
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

// PresenceState is the one presence projection for staff and outsource rows
// (workerPresence only guards released rows before calling it). online is the
// caller's live SSE-connection fact, the only authority: never a DB flag or a
// warden receipt, since a stop receipt can lie while the process still answers.
//
// The stop fact is the stopping_since anchor for both kinds, NOT desired_state.
// Testing desired_state=offline was tried and is wrong (measured mutant, pinned
// by TestPresenceState): the seed ships staff rows (Mira, the server warden)
// offline with no anchor, which would then render 「已停止」 instead of 「離線」.
// Untested premise: both worker stop verbs stamp stopping_since before writing
// offline, and resolveMember refuses kind=outsource, so staff verbs that write
// offline without an anchor cannot reach a worker row.
//
// Waking also requires desired_state online: a wake cancelled mid-flight leaves
// waking_since standing.
func PresenceState(m Member, now float64, online bool) string {
	return derivePresence(presenceInput{
		Online:     online,
		StopIntent: m.StoppingSince > 0.0,
		WakePending: m.DesiredState == DesiredStateOnline &&
			m.WakingSince > 0.0 &&
			now-m.WakingSince <= WakingTTLSecs,
	})
}

func WakingTimedOut(m Member, now float64, online bool) bool {
	return !online &&
		m.DesiredState == DesiredStateOnline &&
		m.WakingSince > 0.0 &&
		now-m.WakingSince > WakingTTLSecs
}

func StoppingTimedOut(m Member, now float64, online bool) bool {
	return online &&
		m.StoppingSince > 0.0 &&
		now-m.StoppingSince > StoppingTimeoutSecs
}

// MemberNamePool never contains "Mira", so the seed identity stays unmistakable.
var MemberNamePool = []string{
	"Nova", "Kai", "Ravi", "Luna", "Iris", "Milo", "Zara", "Theo",
	"Aria", "Ezra", "Vera", "Nico", "Suki", "Remy", "Isla", "Otis",
	"Faye", "Juno", "Cleo", "Enzo", "Mika", "Wren", "Lyra", "Dax",
}

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

func ValidateMember(m Member) error {
	if m.ID == "" {
		return errors.New("member requires a non-empty id")
	}
	if m.Kind != KindStaff && m.Kind != KindWarden && m.Kind != KindOutsource {
		return fmt.Errorf("member %s: kind %q not in {%q, %q, %q}",
			m.ID, m.Kind, KindStaff, KindWarden, KindOutsource)
	}
	if !ValidRuntime(NormalizeRuntime(m.Runtime)) {
		return fmt.Errorf("member %s: runtime %q not in {%q, %q}",
			m.ID, m.Runtime, RuntimeClaude, RuntimeCodex)
	}
	return nil
}

func ValidateChatMessage(m ChatMessage) error {
	if m.ID == "" {
		return errors.New("chat message requires a non-empty id")
	}
	return nil
}

func ValidateChatAttachment(a ChatAttachment) error {
	if a.ID == "" {
		return errors.New("chat attachment requires a non-empty id")
	}
	return nil
}

func ValidateChatRead(r ChatRead) error {
	if r.ReaderID == "" {
		return errors.New("chat read receipt requires a non-empty reader_id")
	}
	if r.PeerID == "" {
		return errors.New("chat read receipt requires a non-empty peer_id")
	}
	return nil
}

func ValidateRoleDef(rd RoleDef) error {
	if rd.RoleKey == "" {
		return errors.New("role def requires a non-empty role_key")
	}
	return nil
}

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

// webhookEndpointIDPattern: the id doubles as the management address key, so it
// must stay URL/path safe.
var webhookEndpointIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const webhookEndpointIDMaxLen = 64

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

func ValidWebhookStatus(status string) bool {
	return status == WebhookStatusEnabled || status == WebhookStatusDisabled
}

func ValidWebhookPlatform(platform string) bool {
	return platform == WebhookPlatformGeneric ||
		platform == WebhookPlatformSlack ||
		platform == WebhookPlatformGithub
}

// scheduledMessageCadences is data (not an || chain) so a test can demand a real
// slot from every cadence: one that mostRecentSlot (schedule_slot.go) does not
// implement never fires, and looks exactly like a schedule with nothing due.
var scheduledMessageCadences = []string{
	ScheduledMessageCadenceDaily,
	ScheduledMessageCadenceWeekly,
	ScheduledMessageCadenceMonthly,
	ScheduledMessageCadenceCustom,
}

func ValidScheduledMessageCadence(cadence string) bool {
	for _, c := range scheduledMessageCadences {
		if c == cadence {
			return true
		}
	}
	return false
}

func scheduledMessageCadenceList() string {
	quoted := make([]string, len(scheduledMessageCadences))
	for i, c := range scheduledMessageCadences {
		quoted[i] = "'" + c + "'"
	}
	return "[" + strings.Join(quoted, " ") + "]"
}

// scheduledMessageCadenceFields must agree with the field descriptions in
// spec/openapi.json ("ignored by ...").
var scheduledMessageCadenceFields = map[string][]string{
	ScheduledMessageCadenceDaily:   {"hour", "minute"},
	ScheduledMessageCadenceWeekly:  {"day_of_week", "hour", "minute"},
	ScheduledMessageCadenceMonthly: {"day_of_month", "hour", "minute"},
	ScheduledMessageCadenceCustom:  {"custom_months", "custom_days", "custom_hours", "custom_minutes"},
}

func scheduledMessageCadenceReads(cadence, field string) bool {
	for _, f := range scheduledMessageCadenceFields[cadence] {
		if f == field {
			return true
		}
	}
	return false
}

// allCustomMonths is what an omitted custom_months resolves to (and what
// migrations/00053 backfilled). It is a listed set, never a nil "all" sentinel:
// everywhere else in this feature an empty set means "the caller said nothing".
func allCustomMonths() []int {
	out := make([]int, 12)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// ValidateScheduledMessageCustomSets: the handler expands an omitted
// custom_months to all twelve BEFORE this runs, while "sent []" and "sent
// nothing" are still distinguishable; here an empty set is always a 422.
func ValidateScheduledMessageCustomSets(months, days, hours, minutes []int) error {
	for _, set := range []struct {
		field  string
		vals   []int
		lo, hi int
		hint   string
	}{
		{"custom_months", months, 1, 12,
			" (to mean every month, OMIT the field entirely rather than sending [])"},
		{"custom_days", days, 1, 31, ""},
		{"custom_hours", hours, 0, 23, ""},
		{"custom_minutes", minutes, 0, 59, ""},
	} {
		if len(set.vals) == 0 {
			return fmt.Errorf("%s cannot be empty when cadence is 'custom'; "+
				"list every value that should fire (an empty set would be read as either "+
				"'always' or 'never', and those must not be one keystroke apart)%s", set.field, set.hint)
		}
		for _, v := range set.vals {
			if v < set.lo || v > set.hi {
				return fmt.Errorf("%s values must be between %d and %d; got %d",
					set.field, set.lo, set.hi, v)
			}
		}
	}
	if err := scheduledMessageMonthDayFeasible(months, days); err != nil {
		return err
	}
	return nil
}

func maxDaysInMonth(m int) int {
	switch m {
	case 2:
		return 29
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

func scheduledMessageMonthDayFeasible(months, days []int) error {
	best := 0
	for _, m := range months {
		if d := maxDaysInMonth(m); d > best {
			best = d
		}
	}
	smallest := days[0]
	for _, d := range days {
		if d < smallest {
			smallest = d
		}
	}
	if smallest <= best {
		return nil
	}
	return fmt.Errorf("custom_months %v and custom_days %v never occur together, so this "+
		"schedule could never fire: the longest of the chosen months has %d days, and the "+
		"earliest day chosen is the %d. Pick a day one of these months actually has, or add a "+
		"month that has this day. (February counts as 29 days, so February with the 29th is "+
		"allowed and fires in leap years only.)", months, days, best, smallest)
}

func ValidateScheduledMessageWallClockPresence(cadence string, hourSent, minuteSent bool) error {
	if cadence == ScheduledMessageCadenceCustom {
		return nil
	}
	if !ValidScheduledMessageCadence(cadence) {
		return nil
	}
	if !hourSent {
		return fmt.Errorf("hour is required when cadence is '%s'; only 'custom' reads "+
			"the custom_hours set instead, and an omitted hour must never be taken to mean midnight", cadence)
	}
	if !minuteSent {
		return fmt.Errorf("minute is required when cadence is '%s'; only 'custom' reads "+
			"the custom_minutes set instead, and an omitted minute must never be taken to mean 0", cadence)
	}
	return nil
}

func ValidScheduledMessageStatus(status string) bool {
	return status == ScheduledMessageStatusEnabled ||
		status == ScheduledMessageStatusDisabled
}

func ValidateScheduledMessageBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("body cannot be blank")
	}
	return nil
}

// ValidateScheduledMessageSlotFields checks both day fields regardless of
// cadence, because the cadence can be PATCHed later. day_of_month allows 1-31
// (owner rc-aeef15360ab5: RFC 5545, a month lacking the day skips it).
func ValidateScheduledMessageSlotFields(hour, minute, dayOfWeek, dayOfMonth int) error {
	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23; got %d", hour)
	}
	if minute < 0 || minute > 59 {
		return fmt.Errorf("minute must be between 0 and 59; got %d", minute)
	}
	if dayOfWeek < 0 || dayOfWeek > 6 {
		return fmt.Errorf("day_of_week must be between 0 (Sunday) and 6 (Saturday); got %d", dayOfWeek)
	}
	if dayOfMonth < 1 || dayOfMonth > 31 {
		return fmt.Errorf("day_of_month must be between 1 and 31; got %d", dayOfMonth)
	}
	return nil
}

// ValidateScheduledMessageTimezone: there is deliberately no fallback here or
// downstream. A substituted zone would send at the wrong hour, which nobody can
// detect, so an unloadable name must fail the write.
func ValidateScheduledMessageTimezone(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("timezone cannot be blank; it must be a stated IANA timezone name " +
			"such as 'Asia/Taipei' or 'UTC' — an empty name would resolve to UTC by accident rather than by choice")
	}
	if strings.EqualFold(name, "Local") {
		return errors.New("timezone 'Local' means the zone the SERVER happens to be in, " +
			"which would move every schedule when the server moves; state the schedule's own " +
			"IANA timezone name (e.g. 'Asia/Taipei', or 'UTC' if that is genuinely what is meant)")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("timezone '%s' is not a known IANA timezone name", name)
	}
	return nil
}

// AttachmentRefIDs: meta["attachments"] is the only message→attachment linkage
// (there is no FK).
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

const CustomRoleTemplateMD = `# 角色定義

## 你是誰

（待填：這個角色的身分與定位——用一兩句話說明「你是誰」、在辦公室裡站什麼位置、面對 owner 與其他成員時以什麼視角說話。）

## 你做什麼

（待填：這個角色的職責與工作方式——負責哪些事、怎麼做事、輸出長什麼樣、與 owner 及其他成員怎麼協作、什麼事不歸你管。）
`

// FoldedRoleDef.IsSeed means a file seed exists: such a role is resettable, not
// deletable, even after edits; an overlay-only custom role is deletable.
type FoldedRoleDef struct {
	Key          string
	Name         string
	DefinitionMD string
	IsDefault    bool
	IsSeed       bool
}

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

// FoldInsight: the seed is per role (assets.go seedInsightMD reads
// insight_<roleKey>.md), so isDefault no longer implies text == "". Anything
// asking "has this role written its own insight" must read isDefault.
func FoldInsight(overlay *Insight, seedText string, hasSeed bool) (text string, isDefault bool) {
	if overlay != nil && !overlay.Tombstoned {
		return overlay.Text, false
	}
	if hasSeed {
		return seedText, true
	}
	return "", true
}

// FoldBootDocument: seedText comes from the go:embed copy, which no edit path
// can reach; reset-to-default depends on the overlay never writing over it.
func FoldBootDocument(overlay *BootDocument, seedText string, hasSeed bool) (text string, isDefault bool) {
	if overlay != nil && !overlay.Tombstoned {
		return overlay.Text, false
	}
	if hasSeed {
		return seedText, true
	}
	return "", true
}

// LessonsEdit's name is historical: every anchor-patch face (insight, SOP, step
// note) uses it, and renaming it is a wire change (LessonsEditDTO in the spec).
type LessonsEdit struct {
	Old string
	New string
}

// ApplyDocEdits' applied count is NOT a write gate: each edit is measured against
// the intermediate text, so a self-cancelling batch (a→b, b→a) returns 2 over
// unchanged text. Handlers compare the text instead (api_insight.go,
// api_taskmanuals.go). rereadTool is a parameter with no per-document wrapper on
// purpose: a wrapper that baked one tool name in once sent another face's
// callers to re-read the wrong document.
func ApplyDocEdits(text string, edits []LessonsEdit, rereadTool string) (string, int, error) {
	result := text
	applied := 0
	for i, edit := range edits {
		before := result
		if edit.Old == "" {
			if result != "" && !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += edit.New
			if result != before {
				applied++
			}
			continue
		}
		switch n := strings.Count(result, edit.Old); {
		case n == 0:
			return "", 0, fmt.Errorf(
				"edits[%d]: old not found in the current doc — re-read (%s) and re-anchor; nothing was written", i, rereadTool)
		case n > 1:
			return "", 0, fmt.Errorf(
				"edits[%d]: old matches %d locations — re-read (%s) and widen the anchor until it is unique; nothing was written", i, n, rereadTool)
		}
		result = strings.Replace(result, edit.Old, edit.New, 1)
		if result != before {
			applied++
		}
	}
	return result, applied, nil
}

func WholeDocWipeBlocked(before, after string) bool {
	return strings.TrimSpace(before) != "" && strings.TrimSpace(after) == ""
}

const lessonsShrinkGuardMinChars = 200

func LessonsShrinkBlocked(before, after string) bool {
	if strings.TrimSpace(before) == "" {
		return false
	}
	if strings.TrimSpace(after) == "" {
		return true
	}
	return len(before) >= lessonsShrinkGuardMinChars && len(after)*10 < len(before)
}

// contextDocMaxCharsDefault is only the default of a `doc.cap_chars.*` setting
// (the effective cap always arrives as a parameter), shared by insight and a
// task manual's sop_md. Caps count runes, not bytes, deliberately: the owner
// picked the numbers from SQLite length() character counts, and len() on
// mostly-Chinese text would be more than twice as strict.
const contextDocMaxCharsDefault = 15000

// dutyCapCharsDefault: the factory Duty seed is structurally exempt, because
// reset_role tombstones and folds back to the file seed with no cap check.
const dutyCapCharsDefault = 1000

// Boot-context caps are sized against the seeds they ship with (a long handbook
// vs short checklists). bootSequenceCapCharsDefault is one knob for both
// runtimes' texts.
const (
	systemInteractionCapCharsDefault = 60000
	bootSequenceCapCharsDefault      = 15000
	offboardCapCharsDefault          = 15000
	// taskEventCapCharsDefault is a constant, not yet a `doc.cap_chars.*`
	// setting: making it one changes the wire contract, which awaits the owner.
	taskEventCapCharsDefault = 15000
)

// minDocCapChars / maxDocCapChars bound all eight adjustable document caps. The
// shared floor sits below the defaults so caps can be lowered (owner
// rc-5b66ba099e28); it is 100 rather than 0 because 0 means "no room".
const (
	minDocCapChars = 100
	maxDocCapChars = 100000
)

// Wake-snapshot chat block budget (`chat.budget_chars`, spent by
// resumeChatPackBudget in api_chat.go). maxChatBudgetChars must stay below
// resumeChatFetch × 27 (the cheapest message's runes) = 13,500, or the packer
// can run out of candidates and silently under-fill; raise resumeChatFetch
// first.
const (
	chatBudgetCharsDefault = 6000
	minChatBudgetChars     = 1000
	maxChatBudgetChars     = 13000
)

// Step-note cap bounds (`task.step_note_cap_chars`). They do not govern
// chatBodyMaxChars' other users: chat bodies and the task handover note keep
// 4000 (owner rc-c8cc527bfed3).
const (
	stepNoteCapCharsDefault = 10000
	minStepNoteCapChars     = 1000
	maxStepNoteCapChars     = 100000
)

// DocCapBlocked: callers must pass as `before` the same doc the shrink guard
// uses (folded overlay ⊕ seed for insight, the stored column for a manual).
func DocCapBlocked(capChars int, before, after string) bool {
	n := utf8.RuneCountInString(after)
	if n <= capChars {
		return false
	}
	return n >= utf8.RuneCountInString(before)
}

// docCapRefusal deliberately names no bypass: there is none (allow_shrink
// governs the opposite failure), and naming a flag would teach agents to route
// around the owner's cap.
func docCapRefusal(capChars int, docName, before, after string) string {
	return fmt.Sprintf(
		"the %s you are writing is %d chars, over the %d-char cap, and is not shorter "+
			"than the %d chars already stored — nothing was written. What is already "+
			"stored is never truncated, but every update must land at or under the cap, "+
			"or at least come out SHORTER than what is there now. Drop stale or "+
			"superseded material as part of this write (or in a shrinking write first), "+
			"then write again.",
		docName, utf8.RuneCountInString(after), capChars,
		utf8.RuneCountInString(before))
}

func docWipeRefusal(docName, wayOut string) string {
	return "this would replace the existing " + docName + " with an empty one — pass allow_shrink=true " +
		"if that is intended" + wayOut + "; nothing was written"
}

func FoldUserContext(row *UserContext) (text string, isDefault bool) {
	if row == nil || row.Tombstoned {
		return "", true
	}
	return row.Text, false
}

// Task statuses are enforced in code only (migrations/00011 dropped the DB
// CHECK). TaskStatusFilterReassigning is not a storable status; it survives only as a
// list-filter name for TaskLockReassigning.
const (
	TaskStatusNotStarted        = "not_started"
	TaskStatusInProgress        = "in_progress"
	TaskStatusWaitingOwner      = "waiting_owner"
	TaskStatusWaitingExternal   = "waiting_external"
	TaskStatusFilterReassigning = "reassigning"
	TaskStatusReadyForDone      = "ready_for_done"
	TaskStatusDone              = "done"
	TaskStatusTerminated        = "terminated"
	TaskStatusDuplicated        = "duplicated"
)

// task.lock is a system hold orthogonal to the derived status. reassigning is
// entered by the reassign action and left ONLY through claim_task (successor or
// owner/admin); while it is on, the stamped predecessor keeps the executor's
// write rights (actingExecutorOf).
const (
	TaskLockNone        = ""
	TaskLockReassigning = "reassigning"
)

func ValidTaskLock(l string) bool {
	switch l {
	case TaskLockNone, TaskLockReassigning:
		return true
	}
	return false
}

const (
	TaskPriorityHigh   = "high"
	TaskPriorityMid    = "mid"
	TaskPriorityLow    = "low"
	TaskPriorityFrozen = "frozen"
)

// Executor kinds spell the same values as member.kind, with no alias for the
// old one (owner rc-5471a679bd22 / rc-7574cc804dd6). warden is absent: machines
// never execute tasks. Unassigned is not a kind: it is outsource with
// ExecutorID == "".
const (
	TaskExecutorStaff     = "staff"
	TaskExecutorOutsource = "outsource"
)

// CanonicalTaskExecutorKind, unlike CanonicalKind, rejects "": the create seam
// handles an omitted kind before calling, so defaulting here would make an empty
// kind from any other seam look valid.
func CanonicalTaskExecutorKind(kind string) (string, error) {
	switch kind {
	case TaskExecutorStaff, TaskExecutorOutsource:
		return kind, nil
	}
	// Runes on purpose; see CanonicalKind.
	if kind == string([]rune{'m', 'e', 'm', 'b', 'e', 'r'}) {
		return "", fmt.Errorf(
			"task executor kind %q was renamed to %q (T-101); the closed set is {%q, %q}",
			kind, TaskExecutorStaff, TaskExecutorStaff, TaskExecutorOutsource)
	}
	return "", fmt.Errorf("task executor kind %q not in {%q, %q}",
		kind, TaskExecutorStaff, TaskExecutorOutsource)
}

// superseded is minted only by submit_plan: a replan freezes a step whose bound
// reply card was already answered/expired, so its Q&A history survives. It is
// never agent-reportable or re-armable.
const (
	StepStatusPending         = "pending"
	StepStatusInProgress      = "in_progress"
	StepStatusWaitingOwner    = "waiting_owner"
	StepStatusWaitingExternal = "waiting_external"
	StepStatusDone            = "done"
	StepStatusSuperseded      = "superseded"
)

// Worker statuses are derived from the member row (dal_tasks.go
// workerStatusFrom); the set stays because the wire vocabulary
// (memberDTO.status) is frozen.
const (
	WorkerStatusAssigned = "assigned"
	WorkerStatusActive   = "active"
	WorkerStatusReleased = "released"
)

// Every artifact kind references a chat_attachment blob (a link is stored as a
// text/uri-list blob); kind, not blob presence, tells them apart.
const (
	ArtifactKindFile  = "file"
	ArtifactKindImage = "image"
	ArtifactKindLink  = "link"
)

func ValidArtifactKind(k string) bool {
	switch k {
	case ArtifactKindFile, ArtifactKindImage, ArtifactKindLink:
		return true
	}
	return false
}

func ValidTaskStatus(s string) bool {
	switch s {
	case TaskStatusNotStarted, TaskStatusInProgress, TaskStatusWaitingOwner,
		TaskStatusWaitingExternal, TaskStatusReadyForDone, TaskStatusDone,
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

func StepIsTerminal(status string) bool {
	return status == StepStatusDone || status == StepStatusSuperseded
}

// TaskIsTerminal deliberately excludes ready_for_done: that state is the open
// close-out window, and adding it would shut every write path (close-out writes
// and mark_task_terminated included), release dependents early and let the
// task's manual be deleted.
func TaskIsTerminal(status string) bool {
	return status == TaskStatusDone || status == TaskStatusTerminated ||
		status == TaskStatusDuplicated
}

// TaskRecordReadOnly equals TaskIsTerminal today but is a separate predicate on
// purpose: the record-freezing doors (artifact add/change/remove/upload, step
// note) ask a different question, and ready_for_done is where the two could
// diverge.
func TaskRecordReadOnly(status string) bool {
	return status == TaskStatusDone || status == TaskStatusTerminated ||
		status == TaskStatusDuplicated
}

// Agent-reported transitions (409 outside the set). waiting_owner is on neither
// side, for tasks or steps: it is entered only by opening a reply card on the
// task (create_reply_card with linked_task) and left only when the server
// restores in_progress on the answer. → terminated is the owner's alone.
var agentTaskTransitions = map[[2]string]bool{
	{TaskStatusNotStarted, TaskStatusInProgress}:      true,
	{TaskStatusInProgress, TaskStatusWaitingExternal}: true,
	{TaskStatusWaitingExternal, TaskStatusInProgress}: true,
	{TaskStatusInProgress, TaskStatusDone}:            true,
}

func CanAgentTaskTransition(from, to string) bool {
	return agentTaskTransitions[[2]string{from, to}]
}

var agentStepTransitions = map[[2]string]bool{
	{StepStatusPending, StepStatusInProgress}:         true,
	{StepStatusInProgress, StepStatusDone}:            true,
	{StepStatusInProgress, StepStatusWaitingExternal}: true,
	{StepStatusWaitingExternal, StepStatusInProgress}: true,
}

func CanAgentStepTransition(from, to string) bool {
	return agentStepTransitions[[2]string{from, to}]
}

// TaskNo returns the id unchanged (owner 2026-08-25: show the full id, no
// mechanism). Lookup is byte-exact (id TEXT PRIMARY KEY, no NOCASE), so any
// reformatting, even "T-" casing, makes a displayed number 404 when pasted back.
// It stays as the one seam every display site calls.
func TaskNo(taskID string) string {
	return taskID
}

// TaskProgress: dal_tasks.go AllTaskStepProgress is the SQL twin; keep them
// agreeing.
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

// CurrentStep expects steps in timeline order (order_idx, id). dal_tasks.go
// AllTaskCurrentStep is the SQL twin; keep them agreeing.
func CurrentStep(steps []TaskStep) (id, name string) {
	for _, st := range steps {
		if !StepIsTerminal(st.Status) {
			return st.ID, st.Name
		}
	}
	return "", ""
}

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
		return TaskStatusNotStarted
	case anyWaitingOwner:
		return TaskStatusWaitingOwner
	case anyWaitingExternal:
		return TaskStatusWaitingExternal
	case allDone:
		return TaskStatusReadyForDone
	case allPending:
		return TaskStatusNotStarted
	default:
		return TaskStatusInProgress
	}
}

// RecomputeTaskStatus is what every step-mutation seam calls. It never touches
// terminal statuses: done is no longer derivable, so re-deriving a
// mark_task_done task would silently reopen it as ready_for_done.
func RecomputeTaskStatus(t *Task, steps []TaskStep) {
	switch t.Status {
	case TaskStatusDone, TaskStatusTerminated, TaskStatusDuplicated:
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

// ValidatePlanParallelShape: the FE folds CONSECUTIVE steps sharing a
// parallel_group into one stage, and an armed gate flips the whole task to
// waiting_owner. timeline is the full step list exactly as it will be stored;
// fresh is the subset this write adds. Do not rebuild timeline as kept+fresh:
// insert_step puts its row in the middle.
func ValidatePlanParallelShape(timeline, fresh []TaskStep) string {
	for _, st := range fresh {
		if st.IsGate && st.ParallelGroup != "" {
			return "step '" + st.Name + "': a gate step cannot sit inside a parallel group — " +
				"put the gate on its own step after the group's join step"
		}
	}
	lastIdx := map[string]int{}
	count := map[string]int{}
	for i, st := range timeline {
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

// DeriveCodename: existing must include every codename ever issued (removed
// workers too); MAX+1 is safe only because SQLite has a single writer.
func DeriveCodename(model string, existing []string) string {
	prefix := CodenamePrefix(model)
	maxN := 0
	for _, c := range existing {
		rest, ok := strings.CutPrefix(c, prefix+"-")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(rest); err == nil && n > maxN {
			maxN = n
		}
	}
	return fmt.Sprintf("%s-%d", prefix, maxN+1)
}

type ManualField struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	IsKey    bool   `json:"is_key"`
}

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

// normalizeFieldKey keeps inner whitespace on purpose. The required-input check
// and dedupe must both use this fold, or a task can pass one and fail the other.
func normalizeFieldKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

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

func InputValueMissing(v any, ok bool) bool {
	if !ok || v == nil {
		return true
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func DedupeKeyValue(fields []ManualField, inputs map[string]any) string {
	normInputs, _ := NormalizeInputs(inputs)
	var parts []string
	anySet := false
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
			anySet = true
		}
		parts = append(parts, part)
	}
	if !anySet {
		return ""
	}
	return strings.Join(parts, "\x1f")
}

func DisplayName(id string, names map[string]string) string {
	if name := names[id]; name != "" {
		return name
	}
	return id
}

// Lore write scopes. 'role' was removed (owner rc-a43100fd0486; migrations/00100
// rekeyed its entries onto the role's single member, and the owner accepted that
// two members under one role would no longer share lore), but the DB CHECK still
// admits 'role' on purpose so ambiguous orphans stay storable; do not tighten it.
// A write whose effective related task carries a type lands in manual (keyed by
// type_key); anything else, 臨時任務 included, lands in agent (keyed by the
// writer's member id). agent is never a fallback for manual. everyone is
// read-side only (entered via set_lore_entry_scope).
const (
	LoreScopeAgent    = "agent"
	LoreScopeManual   = "manual"
	LoreScopeEveryone = "everyone"
)

// retired entries leave every reader-facing fold but are not deleted; pinned
// ones sort ahead of active so they survive the cap.
const (
	LoreStateActive  = "active"
	LoreStatePinned  = "pinned"
	LoreStateRetired = "retired"
)

func ValidLoreState(s string) bool {
	switch s {
	case LoreStateActive, LoreStatePinned, LoreStateRetired:
		return true
	}
	return false
}

// loreRoleCapCharsDefault is the MEMBER fold's budget; the name (and its
// settings key lore.cap_chars.role) predates the member rekey and stays because
// renaming would change a key the owner has already set. The member and manual
// budgets are spent by different readers and are never summed.
const (
	loreRoleCapCharsDefault   = 10000
	loreManualCapCharsDefault = 10000
	loreTitleCapCharsDefault  = 80
	loreBodyCapCharsDefault   = 500
)

// Lore cap floors are non-zero because 0 means "no room" (selectLoreForScope).
const (
	minLoreFoldCapChars  = 100
	maxLoreFoldCapChars  = maxDocCapChars
	minLoreEntryCapChars = 10
	maxLoreEntryCapChars = 10000
)
