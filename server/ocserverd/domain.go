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
//     "staff" at the ingest seam (CanonicalKind).

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

// ── member: kind (closed set) ────────────────────────────────────────────────

// The member kind closed set (schema CHECK): an agent colleague, the
// machine executor (the warden row IS the machine), or an outsourced worker.
// Mirrors the authz literals (adminRoleKey / machineKind) — same vocabulary,
// different concern.
const (
	KindStaff     = "staff"
	KindWarden    = "warden"
	KindOutsource = "outsource"
)

// CanonicalKind folds an incoming kind onto the closed set. The Python side's
// bare hire writes kind="" (a free-form presentation string there); the Go
// schema requires a legal kind, so blank maps to the default colleague kind
// "staff". Anything else outside the closed set is refused.
func CanonicalKind(kind string) (string, error) {
	switch kind {
	case "":
		return KindStaff, nil
	case KindStaff, KindWarden, KindOutsource:
		return kind, nil
	}
	// A caller sending the PRE-RENAME value gets told it was renamed, not just
	// that it is invalid (T-48). Without this, an older client's request fails
	// with a message that lists three values and never says "the one you sent
	// used to be one of them" — the reader's next move is to guess.
	// 🔑 The legacy value is built from runes ON PURPOSE. Spelled as a literal it
	// would be swept up by the very repo-wide "assistant"→"staff" replacement it
	// exists to explain, and this branch would silently start comparing the new
	// value against itself — unreachable, and nothing would fail.
	if kind == string([]rune{'a', 's', 's', 'i', 's', 't', 'a', 'n', 't'}) {
		return "", fmt.Errorf(
			"member kind %q was renamed to %q (T-48); the closed set is {%q, %q, %q}",
			kind, KindStaff, KindStaff, KindWarden, KindOutsource)
	}
	return "", fmt.Errorf("member kind %q not in {%q, %q, %q}",
		kind, KindStaff, KindWarden, KindOutsource)
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
// session having come up, falls back to offline — the wake failed. Sized to
// span several lifecycleCadenceSecs producer ticks, so a wake in flight is
// re-examined repeatedly before it is declared failed. Keep it a comfortable
// multiple of that cadence; the ratio is deliberately NOT written down here,
// because a number in this sentence goes stale the moment either constant
// moves (T-20: it said "3× the runtime's 30s presence heartbeat" for as long
// as this was 90.0 — and no such per-member 30s heartbeat exists: presence is
// derived from the live SSE connection, whose keepalive is 15s).
const WakingTTLSecs = 120.0

// StoppingTimeoutSecs: once stopping_since is set, a still-online member has
// this long to wind down before a stuck collect is force-killed.
const StoppingTimeoutSecs = 120.0

// SoftOffboardGraceSecs: how long a close-out may say NOTHING before its
// anchor is treated as residue (T-7723 — silence, not the anchor's age).
// The soft notice says "work the sequence, then call report_stopped yourself" and
// carries no countdown, and since 2026-08-19 NEITHER soft arm has one running
// behind it: 下線 (rc-27d1710174dd 「不要兜底：只有你按強制下線才收它」) and
// 重新聚焦 (rc-c540367065ad 「連時鐘一起拿掉」) are collected by the agent's own
// stopped report, or by the owner pressing 加速停止 (T-ed79 — that clock is his,
// and it only exists once he presses) or 強制停止, and by nothing else.
//
// 🔴 So this is NOT a deadline any more, and nothing may re-read it as one.
// What still uses it is clearStaleStoppingOnOnline — but as a SILENCE window,
// not an age: how long a close-out may say nothing before its anchor is treated
// as residue (T-7723). A member that is still filing context reports keeps its
// stopping state however long the close-out takes, which is what keeps the
// force-stop button on screen for the owner to press. Escalation is his, not a
// timer's — and it is only useful while the button is still there, which is the
// whole reason the clock had to stop being the anchor's age.
//
// Setting it to 0 restores the pre-T-a9d6 timed wind-down wholesale.
const SoftOffboardGraceSecs = 600.0

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

// PresenceState projects ANY member row's presence at now — staff and outsource
// alike (T-14: workerPresence is a thin released-row guard in front of THIS
// call, not a second projection). A thin mapping of the row's durable anchors
// onto the shared liveness kernel (deriveLiveness). Pure: online is the
// caller-supplied SSE-connection fact (the ONLY authority — never a DB flag,
// never a warden receipt: a stop receipt can lie while the process is alive and
// still answering chat, so SSE connected ⇒ never stopped).
//
// A set stopping_since (the graceful-shutdown signal) is the StopIntent of BOTH
// kinds and takes precedence over every other projection.
//
// 🔴 IT IS THE ANCHOR, NOT desired_state, FOR BOTH KINDS — AND GETTING THERE
// WAS THE WHOLE OF T-14's SECOND HALF. The outsource arm used to test
// `desired_state == offline` here instead, which is a 正職／外包 gate, and the
// identity-gate ledger's own instruction for a new one is "delete the
// difference — preferred". It could be deleted, because the two tests already
// answer the same on every reachable row: BOTH worker stop verbs (停止 and
// 強制停止) stamp stopping_since before they write the offline intent, and both
// anchors are necessarily positive — 停止 goes through stopEpochAnchor (the same
// helper the staff deactivate calls, which cannot return zero) while 強制停止
// stamps its own anchor inline from forced_stop_at. They are two code paths, not
// one: an independent review of T-14 caught this comment claiming otherwise.
// The conclusion is unchanged (the anchor is always set); the reason is not.
// No other writer puts offline on a worker row — the staff verbs that write
// offline without an anchor (HandleDismissMember is one) cannot reach a worker
// row because resolveMember refuses kind=outsource. 🔴 That kind gate is an
// UNTESTED implicit premise of this collapse: route a member verb through a
// resolver that does not filter outsource and this expression starts lying.
//
// The union was tried FIRST and is wrong — a measured mutant said so. A STAFF
// row CAN carry desired_state=offline with no anchor, and that state is the
// ORDINARY one: the out-of-box seed ships Mira and the server warden exactly so.
// Testing desired_state for everybody renders a station nobody has ever switched
// on as 「已停止」 instead of 「離線」, a lie about a member nobody ever woke.
// Pinned by TestPresenceState and the two api_chat offline-mailbox controls.
//
// So the stop fact is the ANCHOR, one expression, no kind branch. What that
// costs is a synthetic row with an offline intent and no anchor reading offline
// rather than stopped — unreachable through any writer, and the price of the
// projection being one piece of code instead of two.
//
// The waking projection needs owner intent (desired_state online) plus a fresh
// waking_since; a stale waking signal is a failed wake and reads offline. The
// intent half is load-bearing and is NOT redundant with StopIntent above: a wake
// cancelled mid-flight (T-7526) leaves the anchor standing, and without this
// test the booting process would paint a fresh green over an intent that has
// already gone offline.
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

// ValidateLessons enforces the lessons-overlay invariant: role_key IS the key
// (T-2 dropped the task_type half of the old composite), so it must be
// populated.
func ValidateLessons(l Lessons) error {
	if l.RoleKey == "" {
		return errors.New("lessons requires a non-empty role_key")
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

// ── scheduled messages (T-f059 定期訊息) ───────────────────────────────────────

// scheduledMessageCadences is the cadence closed set AS DATA, and it is the
// single place the set is written down.
//
// 🔴 It is a slice rather than a chain of ||, and that is the whole point: a
// cadence the slot arithmetic does not implement fails SILENTLY. mostRecentSlot
// answers "no slot", the tick skips the row, and a schedule that never fires
// looks exactly like one that has nothing due — which is how the previous
// bounded-lookback defects hid. With the set as data,
// TestEveryCadenceInTheClosedSetProducesASlot can walk it and demand a real
// slot from EVERY member, so adding a value here without teaching
// schedule_slot.go turns red and names the value. Adding a value to a boolean
// expression is unobservable; adding one here is not.
//
// Scope note: only the CADENCE set moved to data. The status set next door is
// deliberately untouched.
var scheduledMessageCadences = []string{
	ScheduledMessageCadenceDaily,
	ScheduledMessageCadenceWeekly,
	ScheduledMessageCadenceMonthly,
	ScheduledMessageCadenceCustom,
}

// ValidScheduledMessageCadence reports whether cadence is in the closed set.
func ValidScheduledMessageCadence(cadence string) bool {
	for _, c := range scheduledMessageCadences {
		if c == cadence {
			return true
		}
	}
	return false
}

// scheduledMessageCadenceList renders the closed set for a refusal message, so
// the message cannot come to list a different set from the one enforced.
func scheduledMessageCadenceList() string {
	quoted := make([]string, len(scheduledMessageCadences))
	for i, c := range scheduledMessageCadences {
		quoted[i] = "'" + c + "'"
	}
	return "[" + strings.Join(quoted, " ") + "]"
}

// scheduledMessageCadenceFields names, per cadence, the schedule fields that
// cadence actually reads when it computes a slot. It is the SAME statement the
// field descriptions in spec/openapi.json make ("ignored by `daily`, `weekly`
// and `custom`"), expressed once as data so a caller cannot be told a field is
// ignored and then have it move the delivery cursor anyway.
var scheduledMessageCadenceFields = map[string][]string{
	ScheduledMessageCadenceDaily:   {"hour", "minute"},
	ScheduledMessageCadenceWeekly:  {"day_of_week", "hour", "minute"},
	ScheduledMessageCadenceMonthly: {"day_of_month", "hour", "minute"},
	ScheduledMessageCadenceCustom:  {"custom_months", "custom_days", "custom_hours", "custom_minutes"},
}

// scheduledMessageCadenceReads reports whether cadence reads field. A cadence
// outside the closed set reads nothing — that row can never fire anyway, and
// answering "yes" would re-aim a cursor no slot computation will ever consult.
func scheduledMessageCadenceReads(cadence, field string) bool {
	for _, f := range scheduledMessageCadenceFields[cadence] {
		if f == field {
			return true
		}
	}
	return false
}

// allCustomMonths is the whole year, listed. It is what an OMITTED
// `custom_months` resolves to (round 2, migrations/00053) and what the
// migration backfilled every pre-existing `custom` row to.
//
// 🔴 It is a LISTED set, never a nil "means everything" sentinel. Every other
// part of this feature reads an empty set as "the caller told us nothing", and
// one column that read emptiness as "all twelve" would put the two meanings the
// 422 exists to separate back into the same value.
func allCustomMonths() []int {
	out := make([]int, 12)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// ValidateScheduledMessageCustomSets enforces the four explicit sets `custom`
// intersects (T-49e7). Applied ONLY when the cadence is `custom` — every other
// cadence ignores these columns outright.
//
// 🔴 `custom_months` is judged here exactly like the other three: 1-12, and
// EMPTY IS A 422. The rule an omitted `custom_months` gets — all twelve months
// — is applied BEFORE this function, in the handler, where "the caller sent
// []" and "the caller sent nothing" are still two different requests. By the
// time a row arrives here it always lists its months, so this function never
// has to guess which of the two it is looking at.
//
// 🔴 An EMPTY set is a 422 rather than a silent "all" or a silent "never"
// (migrations/00052): those two readings sit one keystroke apart and are
// indistinguishable on screen, so "every day" is expressed by LISTING every
// day. A set that reached the table empty would mean a writer bypassed this
// function, which is why the column's empty-string default can only ever be the
// not-custom marker.
//
// (Written as "empty-string" rather than as a pair of single quotes on purpose:
// gofmt's doc-comment formatter rewrites that pair into a curly quote, which
// turns the sentence into something a reader cannot parse — and the rewrite is
// silent.)
//
// 🔴 The LAST check is the round-2 half of the same rule: four non-empty,
// in-range sets can still describe a schedule that structurally never fires,
// because the month set can empty the intersection. months {2} × days {31} is
// the plainest one — every value is legal, the cockpit renders 每年 2 月 · 每月
// 31 號 and every word of that is true, and not one message is ever sent. That
// is the very shape migrations/00052 argues about: "never fires" must not sit
// one keystroke away from a schedule that looks perfectly ordinary.
//
// 🔴 February counts as 29 days here, ON PURPOSE. months {2} × days {29} is a
// DELIBERATE leap-year schedule that spec and design both spell out, so the
// refusal is drawn at the only line that cannot swallow it: refuse only when NO
// (month, day) pair is possible in any year at all.
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

// maxDaysInMonth is how many days month m can have in the BEST year — February
// answers 29, which is what keeps a leap-year-only schedule legal.
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

// scheduledMessageMonthDayFeasible refuses a month × day pair that no calendar
// can ever satisfy. Both sets are already known non-empty and in range.
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

// ValidateScheduledMessageWallClockPresence refuses a calendar cadence
// (daily/weekly/monthly) that was not given an hour AND a minute.
//
// 🔴 hourSent/minuteSent are "did the caller state it", not "is it non-zero".
// The wire types are pointers precisely so a `custom` schedule need not send
// two values it never reads, and the cost of that is that a MISSING hour is now
// representable — so it is refused here rather than folded to 0. A schedule
// that silently means midnight looks exactly like one that was asked to run at
// midnight, and nothing anywhere would say otherwise.
func ValidateScheduledMessageWallClockPresence(cadence string, hourSent, minuteSent bool) error {
	if cadence == ScheduledMessageCadenceCustom {
		return nil
	}
	// A cadence outside the closed set is refused by ValidScheduledMessageCadence,
	// which owns that message. Saying "hour is required when cadence is 'hourly'"
	// first would answer a question the caller is not being asked yet.
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

// ValidScheduledMessageStatus reports whether status is in the closed set (the
// enable/disable toggle domain).
func ValidScheduledMessageStatus(status string) bool {
	return status == ScheduledMessageStatusEnabled ||
		status == ScheduledMessageStatusDisabled
}

// ValidateScheduledMessageBody rejects a blank body: a schedule that delivers
// nothing is a schedule whose only observable effect is noise.
func ValidateScheduledMessageBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("body cannot be blank")
	}
	return nil
}

// ValidateScheduledMessageSlotFields enforces the wall-clock field ranges. Both
// day fields are checked REGARDLESS of cadence — the cadence is editable later,
// so storing an out-of-range day_of_month behind "monthly does not read it
// today" just defers the fault to the PATCH that flips the cadence.
//
// day_of_month is 1-31, NOT 1-28: owner ruling 2026-08-10 (卡 rc-aeef15360ab5)
// adopted the iCalendar RFC 5545 rule — a month lacking the day drops that
// occurrence from the recurrence set entirely, neither clamped nor an error. The
// documented cost (a 31st schedule fires seven times a year and never in
// February) is accepted knowingly; see docs/design/T-f059-scheduled-message.md.
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

// ValidateScheduledMessageTimezone rejects any name that does not pin the
// schedule to a stated place on Earth.
//
// 🔴 There is deliberately NO fallback here and none anywhere downstream. A name
// that will not load must fail the WRITE, loudly, while a human is still
// looking: substituting UTC (or the host's zone) would leave a schedule that
// runs perfectly and delivers at the wrong hour, and a message that arrives
// eight hours early is indistinguishable from a correct one. "Did not send" is
// discoverable; "sent at the wrong time" is not.
//
// 🔴 "Will it load?" is NOT the test, because the two most dangerous names load
// fine. time.LoadLocation("Local") returns WHATEVER ZONE THE HOST IS IN and
// time.LoadLocation("") returns UTC — both answer "when does this fire?" with a
// deployment detail rather than with the owner's intent, which is precisely the
// ambiguity this feature was built to remove. Moving the server between regions,
// or editing one machine's /etc/localtime, would then move every schedule on it,
// on time-looking messages that arrive at the wrong hour. So they are named and
// refused. `UTC` itself is a real, stated zone and stays legal.
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

// ── insight: per-role overlay ⊕ PER-ROLE file seed (T-3809 → T-e1e3) ─────────

// FoldInsight resolves a per-role insight doc: owner/agent overlay ⊕ this
// role's OWN file seed. Three states, and they are not two:
//
//	never written + this role HAS a seed → (seed text, isDefault=true)
//	never written + this role has NO seed → ("",        isDefault=true)
//	written                               → (overlay,   isDefault=false)
//
// 🔴 THE SEED IS PER-ROLE, NOT SHARED (T-e1e3, and this is the whole point).
// FoldLessons folds against ONE shared file every role reads, so every role
// inherits the same lessons out of the box. Insight must NEVER work that way:
// a role's insight is how THAT role weighs a call, and the assistant's calls
// are wrong for a tester. The caller (assets.go seedInsightMD) resolves
// `insight_<roleKey>.md`; a role with no such file keeps the genuinely-empty
// reading. Today exactly one file ships — insight_assistant.md.
//
// 🔴 WHAT is_default STILL MEANS, AND WHAT IT NO LONGER IMPLIES. It has always
// reported "this role has never written its own insight", and that is unchanged.
// What T-e1e3 breaks is the EQUIVALENCE T-3809 relied on: `is_default == true`
// used to be the same statement as `text == ""`. It now is so only for roles
// with no seed. Everything that read emptiness as "has this role moved anything
// over yet?" must read is_default instead — above all the cockpit, which
// otherwise renders factory wording as if a person had written it.
func FoldInsight(overlay *Insight, seedText string, hasSeed bool) (text string, isDefault bool) {
	if overlay != nil && !overlay.Tombstoned {
		return overlay.Text, false
	}
	if hasSeed {
		return seedText, true
	}
	return "", true
}

// FoldBootDocument resolves ONE boot-context block: owner overlay ⊕ the
// embedded seed (T-791e). Same three states FoldInsight has, and the same
// reading of is_default — "nobody has edited this block", never "the text is
// empty".
//
// 🔴 THE OVERLAY NEVER TOUCHES THE SEED, which is the property the reset route
// is built on: `hasSeed`/`seedText` come from the go:embed copy this binary was
// built with, so "restore to default" is answered from a source no editing path
// can reach. A design that wrote the edit back over the seed would look
// identical until the first reset.
func FoldBootDocument(overlay *BootDocument, seedText string, hasSeed bool) (text string, isDefault bool) {
	if overlay != nil && !overlay.Tombstoned {
		return overlay.Text, false
	}
	if hasSeed {
		return seedText, true
	}
	return "", true
}

// ── lessons: anchor-addressed patch (MCP patch_lessons, T-8327) ──────────────

// LessonsEdit is one {old, new} patch instruction: replace the UNIQUE
// occurrence of Old with New; an empty Old appends New at the end of the doc.
type LessonsEdit struct {
	Old string
	New string
}

// ApplyDocEdits applies edits IN ORDER to text and returns the resulting
// doc. ATOMIC BY CONSTRUCTION: it works on a value copy and returns an error —
// with the failing edit's index — the moment any non-empty Old is absent (0
// hits) or ambiguous (>1 hits) in the text as patched so far; the caller
// writes nothing on error. The unique-anchor requirement doubles as an
// optimistic concurrency check: a concurrent write that moved or duplicated
// the anchor turns this batch into a refusal, never a silent mis-splice.
// It also returns the number of edits that CHANGED THE TEXT THEY WERE HANDED
// (T-2d99). The receipt's applied_edits used to report len(edits) — the count
// REQUESTED, which is structurally incapable of being 0 and therefore carries
// zero information about whether anything landed. An edit that leaves the text
// it was handed untouched — a replace whose new equals its old, or an append of
// "" to a doc that is empty or already newline-terminated — does not increment
// that count, so "0 applied" becomes expressible and a silent no-op stops
// looking like a success. (Appending "" to a doc that does NOT end in a newline
// is not a no-op: the append branch below adds the separator, so the text
// really does change and the count really does rise.)
//
// It is the anchor-patch core shared by every document that takes {old,new}
// edits; rereadTool is the name of the tool the caller should re-read that
// document with when an anchor does not resolve.
//
// 🔴 THE RETURNED COUNT IS NOT A WRITE GATE, and must never be used as one.
// It measures each edit against the INTERMEDIATE result that edit was handed —
// NOT the finished document against the one that came in. The two come apart
// the moment a batch undoes itself: `anchor → middle` followed by
// `middle → anchor` is two uniquely-anchored, individually-effective edits that
// return applied == 2 over text byte-identical to the input. This comment used
// to assert the opposite ("every edit either changes the doc or increments
// nothing"), and that sentence is the exact reasoning error the three handlers
// on this engine were then built on: all three gated their write, their
// document-history retention and their SSE delta on `applied > 0`, so a
// cancelling batch persisted, burned one of the three retained versions, and
// announced a change that never happened.
//
// They now compare the TEXT — `next != current.Text`, and `next != m.Learnings`
// for a manual's learnings — in api_roles.go, api_insight.go and
// api_taskmanuals.go. Anyone asking "did this patch change the document" must
// do the same, or read the sha256 the receipt carries over the result; the
// count answers a different question and always did.
//
// 🔴 WHY THE TOOL NAME IS A PARAMETER AND NOT A CONSTANT. The anchor-miss
// message tells the caller what to do next, and "re-read (get_lessons)" is
// FALSE advice for an agent patching its insight doc: re-reading lessons will
// never show it the anchor it missed. This is the same defect class the ticket
// already ruled on for the 403 path — insightWriteAuthz exists as its own
// function rather than reusing lessonsWriteAuthz precisely because that one
// hard-codes the word "lessons" into a message served on a different document.
// A wrong instruction is worse than a vague one: it sends the reader somewhere
// with confidence.
//
// 🔴 THERE IS DELIBERATELY NO PER-DOCUMENT WRAPPER THAT BAKES THE NAME IN
// (T-2fbf). A `ApplyLessonsEdits(text, edits)` convenience used to exist, and
// the manual-learnings patch face reached for it as "the shared engine" —
// which is exactly how patch_task_learnings came to tell its callers to
// re-read get_lessons. Every call site must name its own document's read tool,
// so that getting it wrong is a visible edit rather than a default.
func ApplyDocEdits(text string, edits []LessonsEdit, rereadTool string) (string, int, error) {
	result := text
	applied := 0
	for i, edit := range edits {
		before := result
		if edit.Old == "" {
			// Append: join with a newline unless the doc is empty or already
			// newline-terminated (keeps repeated appends line-clean).
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
			// The tool name belongs here too (T-2fbf): widening an anchor means
			// looking at the doc's surrounding text, which the caller may no
			// longer have. Naming the read tool only on the 0-hit arm would read
			// as "the parameter is for that arm only" — and a caller sent to the
			// wrong doc widens against text that is not the one being patched.
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

// WholeDocWipeBlocked reports whether a whole-doc REPLACE would wipe an
// existing doc (non-blank → blank) — the LessonsShrinkBlocked posture narrowed
// to the wipe case only (T-2d99). A whole-doc replace legitimately shrinks a
// lot (that is what "rewrite it shorter" means), so the <10% shrink rule that
// guards an anchor-addressed patch would fire on honest edits here. Emptying a
// doc that had content, though, is never something a caller should reach by
// accident — and it is precisely what the dropped-unknown-key bug produced.
// Bypassed by an explicit allow_shrink=true.
func WholeDocWipeBlocked(before, after string) bool {
	return strings.TrimSpace(before) != "" && strings.TrimSpace(after) == ""
}

// lessonsShrinkGuardMinChars is the doc size above which a >90% shrink needs
// the explicit allow_shrink flag (below it, only a full wipe is guarded — a
// small doc legitimately rewrites wholesale).
const lessonsShrinkGuardMinChars = 200

// LessonsShrinkBlocked reports whether patching before → after would wipe the
// doc (non-blank → blank) or shrink a substantial doc to under a tenth of its
// size — the r-76 wipe-accident guard, bypassed only by an explicit
// allow_shrink=true (or the whole-doc replace_lessons seam).
func LessonsShrinkBlocked(before, after string) bool {
	if strings.TrimSpace(before) == "" {
		return false // nothing to protect
	}
	if strings.TrimSpace(after) == "" {
		return true
	}
	return len(before) >= lessonsShrinkGuardMinChars && len(after)*10 < len(before)
}

// ── context docs: the hard size cap (T-3351) ─────────────────────────────────

// contextDocMaxCharsDefault is the SHIPPED DEFAULT of the cap, in UTF-8
// CHARACTERS (runes), on the accumulating context documents an agent writes
// back: a role's lessons doc and a task manual's learnings / sop_md. Owner
// ruling (2026-07-27), stated in two sentences: "an update must not push the
// doc past this size", and "whatever is already over it we do NOT truncate —
// but its next update may only make it smaller".
//
// RUNES, not bytes, deliberately — the same unit as chatBodyMaxChars
// (utf8.RuneCountInString). The distribution the owner picked the original
// number from was measured with SQLite length(), which counts CHARACTERS; these
// docs are largely Chinese prose at ~2.2–3 bytes per character, so capping
// len() at the same number would be a cap more than twice as strict as the one
// the owner actually signed off on — whatever that number happens to be today.
//
// T-3aeb (owner 2026-07-31): the number is no longer a constant the code owns
// — it is a `doc.cap_chars.*` setting, adjustable at runtime, and this is only
// its default. The EFFECTIVE cap always arrives as a parameter, so there is no
// second copy for a caller to read by accident. The floor of the adjustable
// range equals its default, so a cap can only ever be RAISED: lowering it
// would turn documents that are legal today into shrink-only ones.
//
// T-ae38 (owner 2026-08-03): ONE cap became FOUR, and T-30f1 split the task
// manual's in two again. This constant is the default SHARED by Insight,
// Learning and BOTH of a task manual's capped documents (sop_md, learnings);
// Duty got its own, much smaller one below (dutyCapCharsDefault). The owner's words: 「我預期 duty
// 1000 / insight 10000 / learning 10000 但是三者都可以調整」 — quoted as the
// record of the ruling, NOT as a statement of the current numbers. He revised
// them the same day, and every one of them is a runtime setting on top of that,
// so no prose anywhere should restate a cap: read the two constants below.
// The reason the segments cannot share a number is that their deletion costs
// differ by an order of magnitude: a Duty is a standing definition that should
// stay readable in one screen, while a Learning doc is append-only environment
// Q&A.
//
// The patch receipts' `size` field speaks THIS unit too, since T-3aeb — it
// counted bytes until the owner ruled that one subject may not have two units.
const contextDocMaxCharsDefault = 15000

// dutyCapCharsDefault is the shipped default of the DUTY (role definition) cap
// — the one segment that does not share contextDocMaxCharsDefault.
//
// 🔴 THE STRUCTURAL EXCEPTION STANDS; ITS ONE INSTANCE IS GONE (T-e1e3).
// `reset_role` writes a TOMBSTONE and folds back to the FILE seed, so no cap
// check sits on the path that installs shipped content — no cap can catch a
// seed by construction, and that is still true. The practical meaning of
// "Duty ≤ 1000" is therefore "hand-written Duty ≤ 1000, with the factory seed
// structurally exempt".
// ⚠️ What has CHANGED: this comment used to say the shipped seed was 4,594
// runes and therefore over the cap out of the box. T-e1e3 retired that
// oversized seed, and T-795e replaced it again with the current factory Duty;
// the shipped Duty now sits far below this default cap, so NOTHING actually
// exercises the exemption today. Deliberately no rune count here: the seed is
// edited far more often than this comment, and a number written down here is a
// false sentence waiting to happen — read the file if you need its size. Do not
// reason from the old number, and do not go looking for an oversized seed to
// "fix".
const dutyCapCharsDefault = 1000

// systemInteractionCapCharsDefault / bootSequenceCapCharsDefault are the
// shipped defaults of the two boot-context document kinds that became
// editable in T-791e. Both are sized against the SEEDS THEY SHIP WITH, not
// picked round:
// the system-interaction seed is a long studio handbook and the two boot
// sequences are short checklists, so one shared number would either strand the
// handbook in shrink-only mode on day one or hand the checklists a budget forty
// times their own size.
//
// The boot-sequence cap is ONE knob for BOTH runtimes (claude and codex), each
// measured on its own text. They are two renderings of the same short document;
// a studio that needs more room for one needs it for the other.
//
// The 〈停止〉 cap (T-c9c0) is sized with the boot sequences rather than the
// handbook, and for the same reason: it is a short ordered checklist an agent
// has to work under time pressure (a recycle bounds it; an offboard does not,
// but the owner is waiting), not a reference text.
const (
	systemInteractionCapCharsDefault = 60000
	bootSequenceCapCharsDefault      = 15000
	offboardCapCharsDefault          = 15000
	// taskEventCapCharsDefault caps each of the four task-event procedures
	// (T-3201). Same number as the offboard sequence and for the same reason:
	// these are short procedures an agent reads mid-flight, not accumulating
	// memory, and the ceiling exists to stop one growing into a handbook.
	//
	// 🔴 A CONSTANT, NOT YET A `doc.cap_chars.*` SETTING. Every cap above is
	// adjustable at runtime through SettingsDTO, and making these six the same
	// would add fields to a wire contract — spec/openapi.json, the generated
	// MCP catalog and the cockpit's settings surface all move with it. That is
	// an interface change this ticket owes the owner a look at before it
	// happens, so the number is code until he has had it.
	taskEventCapCharsDefault = 15000
)

// min*CapChars / maxDocCapChars bound the adjustable caps. Each floor is THAT
// segment's own default by design (see above), not a coincidence to be "tidied
// up" into one shared number — putting the other segments' floor on Duty would
// make dutyCapCharsDefault unreachable from the settings surface. The same
// applies to the two boot-context document kinds: their floors are their own
// defaults, so an owner can only ever RAISE them.
const (
	minDocCapChars               = contextDocMaxCharsDefault
	minDutyCapChars              = dutyCapCharsDefault
	minSystemInteractionCapChars = systemInteractionCapCharsDefault
	minBootSequenceCapChars      = bootSequenceCapCharsDefault
	minOffboardCapChars          = offboardCapCharsDefault
	maxDocCapChars               = 100000
)

// chatBudgetCharsDefault / minChatBudgetChars / maxChatBudgetChars bound the
// wake snapshot's chat block budget (T-c9b4; the number resumeChatPackBudget
// spends, see api_chat.go). It was the hard-coded constant 8000 until this
// change made it the `chat.budget_chars` setting.
//
// 🔴 THE FLOOR IS NOT THE DEFAULT, unlike every doc.cap_chars.* above. Those
// floors equal their own defaults because LOWERING a document cap puts existing
// legal documents into shrink-only mode — a real, permanent cost. The chat
// block carries no such state: it is repacked from scratch on every read, so a
// smaller budget just returns fewer messages next time. Copying the doc-cap
// rule here would mean the knob could only ever go up, which is not "adjustable".
//
// 🔴 THE CEILING IS TIED TO resumeChatFetch, not picked round. That constant's
// own comment derives 500 as a FLOOR from this budget: the cheapest possible
// message costs 27 runes, so 500 × 27 = 13,500 runes of candidates must exceed
// the budget or the packer could run out of messages before it runs out of
// budget and silently under-fill the snapshot. 13000 keeps that guarantee with
// room to spare. To raise this ceiling past 13,500 you MUST raise
// resumeChatFetch first.
const (
	chatBudgetCharsDefault = 6000
	minChatBudgetChars     = 1000
	maxChatBudgetChars     = 13000
)

// DocCapBlocked reports whether replacing before with after must be refused by
// the hard cap. The three-line rule, boundaries included:
//
//   - after ≤ cap                      → allowed (the ordinary case);
//   - after > cap AND after < before   → allowed (an over-cap doc is free to
//     keep converging downward — this is the escape hatch the two live
//     over-cap lessons docs and four over-cap manuals depend on);
//   - after > cap AND after ≥ before   → REFUSED, EQUAL LENGTH INCLUDED. Not
//     getting shorter is not converging, and admitting equal-length rewrites
//     would let an over-cap doc be replaced wholesale forever.
//
// Existing over-cap content is never truncated or rewritten by this rule — it
// only ever refuses a WRITE. A first write (no prior doc) sees before="" and is
// therefore judged on the cap alone.
//
// It is measured on the doc the caller reads and edits (the folded overlay ⊕
// seed for lessons; the stored column for a manual) — the same `before` the
// shrink guard uses, so the two guards can never disagree about what "the
// current doc" is.
func DocCapBlocked(cap int, before, after string) bool {
	n := utf8.RuneCountInString(after)
	if n <= cap {
		return false
	}
	return n >= utf8.RuneCountInString(before)
}

// docCapRefusal is the ONE refusal text behind the cap, so the five write
// seams cannot drift into five different explanations. It names the three
// numbers a caller needs (proposed size, cap, current size) and the one legal
// way forward: make the write smaller — delete stale material in the same
// write, or in a shrinking write first.
//
// It deliberately advertises NO bypass. There is none: allow_shrink governs the
// opposite failure (shrinking too far) and does not open this gate. Naming a
// flag here would teach agents to route around a cap the owner set on purpose.
func docCapRefusal(cap int, docName, before, after string) string {
	return fmt.Sprintf(
		"the %s you are writing is %d chars, over the %d-char cap, and is not shorter "+
			"than the %d chars already stored — nothing was written. What is already "+
			"stored is never truncated, but every update must land at or under the cap, "+
			"or at least come out SHORTER than what is there now. Drop stale or "+
			"superseded material as part of this write (or in a shrinking write first), "+
			"then write again.",
		docName, utf8.RuneCountInString(after), cap,
		utf8.RuneCountInString(before))
}

// docWipeRefusal is the ONE refusal text behind the wipe guard
// (WholeDocWipeBlocked), the same way docCapRefusal is the one text behind the
// cap. It names the document and the ONE way forward, because being refused is
// otherwise the only way to learn that allow_shrink exists at all.
//
// wayOut is the seam-specific escape the caller also has — the way back to the
// factory text, which is NOT the same sentence everywhere: replace_global_context
// names a tool (reset_global_context), the boot documents name a gesture ("reset
// it to the shipped default"), and the lessons / insight seams have no second
// way out and pass "". It is a parameter rather than a fifth hardcoded sentence
// so folding these four together changed no byte any caller reads.
//
// 🔴 FOUR SEAMS, NOT FIVE. api_taskmanuals.go's replace_task_manual_learnings
// says "…the existing learnings with an empty DOC" — a different skeleton, not
// just a different name — so it is deliberately NOT folded in here. Bending it
// to fit would mean editing the sentence an agent reads, which is a text change
// wearing a refactor's clothes. Leave it where it is until someone decides, on
// purpose, to reword it.
func docWipeRefusal(docName, wayOut string) string {
	return "this would replace the existing " + docName + " with an empty one — pass allow_shrink=true " +
		"if that is intended" + wayOut + "; nothing was written"
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

// ── tasks: the handover declaration (T-74f8) ─────────────────────────────────

// The task HANDOFF closed set — "where does the ball go when this task ends".
// Declared by the executor in the SAME request that would close the task (the
// step-status report), because the close is irreversible: the instant the last
// step lands done the task derives to done, closed_ts stamps, and submit_plan
// is a permanent 409 — there is no "after" in which to arrange a handover.
//
//   - HandoffReturnToCreator — hand it back: the declaration is RECORDED on the
//     task and nothing else happens. It has been narrowed twice, both by the
//     owner on 2026-08-17 (T-f265): it used to MINT a task on the creator —
//     withdrawn because that task's own first line told an ordinary member to
//     terminate it, and terminate_task was admin-only at the time (T-b56e opened it to the executor on 2026-08-20 — the ruling below stands on its own reasoning, not on that gate) — and the durable chat
//     notice that replaced it was withdrawn too (card rc-e04adbc42574, option
//     ①), on the ruling that once work is handed over it belongs to whoever
//     holds it and the system should not report back. So this value now differs
//     from HandoffNone only in what it SAYS about where the ball went;
//   - HandoffFollowUp        — a successor task already exists; the server
//     attaches this task to it as a dep, so half B (closeTask →
//     releaseDependentsOnClose) wakes/schedules it the moment we close;
//   - HandoffNone            — explicitly nothing follows. Requires a note:
//     an un-reasoned "none" is a rubber stamp, and the note IS the audit trail
//     that distinguishes a decision from an omission.
//
// HandoffUndeclared (the empty string) is the pre-column / never-asked state.
const (
	HandoffUndeclared      = ""
	HandoffReturnToCreator = "return_to_creator"
	HandoffFollowUp        = "follow_up"
	HandoffNone            = "none"
)

// ValidHandoff reports handoff closed-set membership, EXCLUDING the undeclared
// empty (a caller declaring "" is declaring nothing — the gate's whole point).
func ValidHandoff(h string) bool {
	switch h {
	case HandoffReturnToCreator, HandoffFollowUp, HandoffNone:
		return true
	}
	return false
}

// TaskNeedsHandoffDeclaration is the GATE PREDICATE — the precise population
// the close gate asks: a task whose creator is a DIFFERENT actor from its
// executor and that has not yet declared where the ball goes.
//
// Deliberately narrow (the fail-closed blast-radius rule). It is false for:
//   - a self-created task (creator == executor) — the executor IS the asker,
//     there is nobody to hand back to. 270 of the 392 live tasks;
//   - a blank creator (pre-creator_id rows) or a blank executor — we cannot
//     name the two sides, so we must not invent an obligation. 53 live rows;
//   - an already-declared task (idempotent: a re-report never re-asks).
//
// It says nothing about WHEN to ask — the caller pairs it with "this write
// would close the task" so a mid-plan step report is never touched.
func TaskNeedsHandoffDeclaration(creatorID, executorID, handoff string) bool {
	if creatorID == "" || executorID == "" || creatorID == executorID {
		return false
	}
	return handoff == HandoffUndeclared
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
// this table: it is entered ONLY by opening a card (create_reply_card with an
// explicit linked_task) and LEFT ONLY when that card is answered — the server itself
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
// task table: the card-open path sets it (create_reply_card with an explicit
// linked_task — the handler 400s an agent report INTO it), and the answer path
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

// TaskNo is the task id, unchanged. There is no display projection any more.
//
// It SUPERSEDES kyle ruling H3, which cut the number to the first four hex
// chars ("t-72dd79b666d0" → "T-72dd") and accepted collisions because it was
// display-only. Owner 2026-08-25: 「UI 也不用特意把 task_no 縮短,該是多長就
// 該多長」「不用讓他吃短碼,讓我們顯示長碼」and 「Make it simple, no need
// complicated mechanism unless my approval」.
//
// 🔴 WHY IT IS NOT EVEN "T-" + the hex — the intermediate version this replaced.
// Lookup is `SELECT … WHERE id = ?` against `id TEXT PRIMARY KEY` with no
// COLLATE NOCASE (migrations/00004_tasks.sql), i.e. byte-exact. A number shown
// as "T-72dd79b666d0" against an id of "t-72dd79b666d0" therefore still 404s
// when pasted back — one character of re-casing was enough to buy nothing.
// Making the lookup case-insensitive would be a mechanism, which is what the
// ruling above forbids. Returning the id costs no mechanism at all, and the
// number that someone reads off the UI is then usable BECAUSE IT IS THE ID,
// not because anything maps it back.
//
// The function survives as the ONE seam every display site already calls, so
// this stays a single decision rather than 12 call sites each deciding again.
func TaskNo(taskID string) string {
	return taskID
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

// CurrentStep is the ONE definition of "which step is the task on now": the
// FIRST step, in timeline order (order_idx, id — dal.ListTaskSteps' order),
// that is not TERMINAL. A superseded row is frozen replan history and a done
// row is finished, so neither can ever be the working node (T-1aea,
// StepIsTerminal). Returns "", "" when the plan is empty or every step has
// reached a terminal state — an honest "there is no current step" that must
// never be laundered into the first row of the plan.
//
// 🔴 This exists so the rule lives in exactly one place. Both the wake snapshot
// (resumeTasksFor) and the light task list read it; dal.AllTaskCurrentStep is
// the SQL twin for the list's one grouped query — keep the three agreeing.
func CurrentStep(steps []TaskStep) (id, name string) {
	for _, st := range steps {
		if !StepIsTerminal(st.Status) {
			return st.ID, st.Name
		}
	}
	return "", ""
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
