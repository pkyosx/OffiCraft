// api/mock.ts — the mock adapter (wired in M1 via index.ts).
//
// Members come from an in-module WIRE-shape fixture (moved here from the old
// data/members.ts) so the mock exercises the SAME wire→view mapper the real
// HTTP adapter will use. Honesty is preserved end-to-end: no telemetry, no fake
// online, no fabricated timestamps.

import type {
  LoreEntryDetailView,
  LoreEventView,
  LorePendingEntityView,
  LoreEntityGovernanceView,
  LoreEntrySummaryView,
  LoreRevisionView,
  LoreSearchView,
  Member,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  BackupHealthView,
  SigningKeyView,
  AuthStatusView,
  MfaEnrollView,
  MfaStateView,
  GlobalContextView,
  BootDocKind,
  BootDocView,
  DocumentKind,
  InsightView,
  DocumentHistoryEntryView,
  DocumentHistoryView,
  DocumentRevisionView,
  DocumentSeedView,
  DiffPairView,
  RoleSummaryView,
  RoleDefView,
  BootstrapView,
  LessonsView,
  OnboardResultView,
  DeleteResultView,
  UninstallResultView,
  BootstrapResultView,
  TeardownHereResultView,
  MachineView,
  MemberActivateResult,
  MemberRelocateResult,
} from "../types";
import type {
  Api,
  ChatCursor,
  ChatAnchor,
  ChatMessage,
  ChatReplyQuote,
  ChatReadReceipt,
  ChatAttachmentInput,
  PushSubscriptionInput,
  ChatAttachmentView,
  GalleryAttachment,
  MemberPatch,
  WebhookEndpoint,
  WebhookCreateInput,
  WebhookUpdate,
  WebhookRequestLog,
  ScheduledMessage,
  ScheduleCadence,
  ScheduledMessageCreateInput,
  ScheduledMessageUpdate,
  ReplyCard,
  ReplyCardAnswerInput,
  ReplyCardCounts,
  RolePatch,
  RoleCreateInput,
  RoleCreateResult,
  AliasPatch,
  OnboardOptions,
  ServerSettingsView,
  ServerSettingsPatch,
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
  TaskCountView,
  TaskStepDetailView,
  TaskArtifactView,
  TaskManualSummaryView,
  TaskManualView,
  TaskManualPatch,
  DocSummaryView,
  DocView,
  MemberResumeSummaryView,
  ResumeRosterMemberView,
  ResumeMachinesView,
  ResumeAnsweredCardStepView,
  ResumeTaskView,
  ThemeListItem,
  ThemeWriteReceipt,
  ThemeDeleteResult,
  SseConnectionState,
  LoreSearchInput,
  AccountCostResetReceipt,
  CostResetReceipt,
  TaskArtifactVersionView,
} from "./adapter";
import type {
  WireMember,
  WireMonitoring,
  WireMonSession,
  WireVersion,
  WireBackupHealth,
  WireSigningKeys,
  WireGlobalContext,
  WireBootDoc,
  WireDocumentHistory,
  WireDocumentSeed,
  WireRoleDef,
  WireBootstrap,
  WireLessons,
  WireInsight,
  WireOnboardResult,
  WireDeleteResult,
  WireUninstallResult,
  WireMachine,
  WireServerSettings,
} from "./wire";
import {
  toMember,
  toMonitoring,
  toVersion,
  toReleaseCheck,
  toBackupHealth,
  toSigningKeys,
  toGlobalContext,
  toBootDoc,
  toDocumentHistory,
  toDocumentHistoryEntry,
  toDocumentSeed,
  toRoleDef,
  toRoleSummary,
  toBootstrap,
  toLessons,
  toInsight,
  toOnboardResult,
  toDeleteResult,
  toUninstallResult,
  toMachine,
  toServerSettings,
} from "./mappers";
import {
  DOC_CAP_CHARS_DEFAULTS,
  BOOT_DOC_CAP_CHARS_DEFAULTS,
  BOOT_DOC_HISTORY_KEPT,
  TASK_EVENT_CAP_CHARS_DEFAULT,
  contentSizes,
  docCapBlocked,
  wholeDocWipeBlocked,
} from "./docCap";
import { docJoinHeadBody, docSplitHeadBody } from "./docSplit";
import {
  CHAT_BUDGET_CHARS_DEFAULT,
  CHAT_BUDGET_CHARS_MAX,
  CHAT_BUDGET_CHARS_MIN,
} from "./chatBudget";
import {
  BACKUP_RETAIN_DEFAULT,
  BACKUP_RETAIN_MAX,
  BACKUP_RETAIN_MIN,
} from "./backupRetain";
import {
  MOCK_OWNER_ID,
  SEED_SYSTEM_INTERACTION_MD,
  SEED_ROLE_ASSISTANT_MD,
  SEED_LESSONS_MD,
  SEED_INSIGHT_ASSISTANT_MD,
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
  SEED_OFFBOARD_MD,
  SEED_ACCELERATED_STOP_MD,
  SEED_TASK_CLOSEOUT_MD,
  SEED_TASK_REASSIGN_PREDECESSOR_MD,
  SEED_TASK_TAKEOVER_WITH_PREDECESSOR_MD,
  SEED_TASK_TAKEOVER_FRESH_MD,
  SEED_TASK_UNBLOCKED_MD,
} from "./seeds";
import { mockApiError } from "./errorCodes";
import { formatDiffUrl, type DiffParams } from "../lib/diffLink";

/** The offline cockpit's compare fixture — two texts that differ by one edited
 * line and one added line, so both the line-level rows and the character-level
 * tint have something real to draw. */
const MOCK_DIFF_BEFORE = [
  "# 專案說明",
  "",
  "第一段沒有改。",
  "這一行的用字改過了。",
  "最後一段沒有改。",
].join("\n");
const MOCK_DIFF_AFTER = [
  "# 專案說明",
  "",
  "第一段沒有改。",
  "這一行的措辭改過了。",
  "新增的一行。",
  "最後一段沒有改。",
].join("\n");
import {
  validateThemeBundle,
  isValidDisplayTheme,
  MAX_CUSTOM_THEMES,
} from "../lib/themeBundle";
import type { ThemeBundle } from "../lib/themeBundle";

// The always-present server-self machine id (mirrors the server seed):
// the warden for the host running the server itself — listed FIRST, is_self, NOT
// deletable, in-place Install.
const MOCK_SERVER_SELF_ID = "m-server-self";

// ── Fixture: out-of-box Mira, in WIRE shape (mirrors what /api/members returns).
// Offline, never online (last_alive 0 → honest "尚未上線"), no telemetry.
const MOCK_WIRE_MEMBERS: WireMember[] = [
  // The server-self warden — the host running the officraft server itself. It
  // ALWAYS exists (mirrors the backend seed), surfaces FIRST in the machine panel
  // (is_self=true via listMachines), and is NOT deletable. Offline until it reports.
  {
    id: MOCK_SERVER_SELF_ID,
    name: "伺服器這一台",
    kind: "warden",
    role_key: "",
    role_name: "",
    runtime: "claude",
    model: "",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    desired_state: "offline",
    desired_machine_id: MOCK_SERVER_SELF_ID,
    machine: "", // OBSERVED position: offline → nothing observed → honest "—"
    presence: "offline",
    refocus_since: 0,
    last_op: "",
    last_op_ok: null,
    last_op_log: "",
    last_op_reason: "",
    last_op_at: 0,
    forced_stop_at: 0,
    roster_status: "active",
    owner_id: "",
    unread_count: 0,
    schema_version: 2,
  },
  {
    id: "mira",
    name: "Mira",
    kind: "staff", // mirror the real seed (dbseed.go: Mira kind=KindStaff)
    role_key: "assistant",
    role_name: "",
    runtime: "claude",
    model: "claude-sonnet-4.5",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    desired_state: "offline",
    // `desired_machine_id` carries the machine BINDING id (the machine_id an activate
    // binds to; the server renamed the activate field host → machine_id). We bind Mira
    // to the seed warden's id below so the machine picker can default to her
    // currently-bound machine (shown disabled/offline until that warden reports).
    desired_machine_id: "warden-mbp5",
    machine: "", // OBSERVED position: offline → nothing observed → honest "—"
    presence: "offline",
    refocus_since: 0,
    last_op: "",
    last_op_ok: null,
    last_op_log: "",
    last_op_reason: "",
    last_op_at: 0,
    forced_stop_at: 0,
    roster_status: "active",
    owner_id: "",
    unread_count: 0,
    schema_version: 2,
  },
  // A warden member bound to mbp5 (kind="warden") — the machine-layer telemetry
  // daemon that runs ON the host. It is filtered out of the office roster and the
  // "AI 會話" list (warden≠LLM), but it is what a machine-row TEARDOWN targets:
  // the monitoring machine DTO carries no member_id, so a teardown resolves the
  // warden member for that host (kind==="warden" + desired_machine_id===machine) and DELETEs by
  // its id. Offline / never-online — no fabricated telemetry.
  {
    id: "warden-mbp5",
    name: "Warden · mbp5",
    kind: "warden",
    role_key: "assistant",
    role_name: "",
    runtime: "claude",
    model: "",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    desired_state: "offline",
    desired_machine_id: "mbp5",
    machine: "", // OBSERVED position: offline → nothing observed → honest "—"
    presence: "offline",
    refocus_since: 0,
    last_op: "",
    last_op_ok: null,
    last_op_log: "",
    last_op_reason: "",
    last_op_at: 0,
    forced_stop_at: 0,
    roster_status: "active",
    owner_id: "",
    unread_count: 0,
    schema_version: 2,
  },
  // A LIVE outsource worker, carried in the roster fixture because the roster
  // ENDPOINT carries one: GET /api/members answers over the WHOLE member table,
  // contractors included (the P7 convergence; since T-14 項目 6 that is the only
  // roster query there is — `dal.ListMembers`, with no kind clause), so a
  // cockpit that runs against a mock with no `ow-` row is running against a
  // roster the server never serves.
  //
  // 🔴 WHAT ITS ABSENCE COST (T-26): the roster hands an `ow-` id to the
  // held-id mirror, a chat delta naming that one id takes the per-item fast
  // path, and GET /api/members/{ow-} answered 404 until 2026-08-28 — one
  // guaranteed failed request plus a whole-roster refetch on every contractor
  // chat line. The offline mock could not reproduce ANY of that, so the whole
  // path was invisible to every frontend test. The item door is open now, and
  // this row is what keeps a future re-narrowing from being silent here.
  //
  // The office roster, the task assignee picker and the reassign dialog all
  // filter `kind === "staff"`, so this row changes no panel — it changes
  // what the DATA looks like, which is the point.
  {
    id: "ow-7d8ad859dd9b",
    name: "O-179", // a worker reads by its codename, never a personal name
    kind: "outsource",
    role_key: "", // contractors carry no role — their duty is the bound task
    role_name: "",
    runtime: "claude",
    model: "claude-sonnet-4.5",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    desired_state: "online",
    desired_machine_id: "warden-mbp5",
    machine: "warden-mbp5",
    presence: "online",
    refocus_since: 0,
    last_op: "",
    last_op_ok: null,
    last_op_log: "",
    last_op_reason: "",
    last_op_at: 0,
    forced_stop_at: 0,
    roster_status: "active",
    owner_id: "",
    unread_count: 0,
    schema_version: 2,
  },
];

// ── Fixture: per-machine binary-freshness verdicts (bin_status). Mirrors the
// server comparing warden-heartbeat fingerprints against its embedded
// prebuilts: the seed remote warden last reported OLD fingerprints before it
// went offline (→ "stale"); server-self never reported any (→ absent = honest
// null "—"). Nothing on the client converges this any more: the one-click
// upgrade affordance is gone, and a machine self-updates on its own.
const mockBinStatus = new Map<string, "current" | "stale">([
  ["warden-mbp5", "stale"],
]);

// ── Fixture: per-machine reported launchd shape (warden_shape). Unlike
// bin_status this is REPORTED, not computed, so the mock stores what a warden
// said rather than deriving it: server-self runs the cutover build and reports
// "anchor"; the seed remote warden is on an old build that says nothing at all
// (→ absent, which is the "not reported" face — NOT "unknown"). Display-only,
// nothing mutates it, so no __resetMock entry.
const mockWardenShape = new Map<string, "anchor" | "legacy" | "unknown">([
  [MOCK_SERVER_SELF_ID, "anchor"],
]);

// ── Fixture: per-machine cutover-effect verdict (cutover_effect). Reported like
// the shape above, and deliberately NOT derived from it — the pair being able to
// disagree ("anchor" + "not_effective") is the whole reason the second field
// exists, and it is the state the incident actually had. server-self therefore
// carries that pair; the seed remote warden reports nothing (→ absent).
const mockCutoverEffect = new Map<
  string,
  "effective" | "not_effective" | "unproven"
>([[MOCK_SERVER_SELF_ID, "not_effective"]]);

// ── Fixture: per-machine claude CLI probe columns (T-97ee). Mirrors the
// server synthesizing the warden heartbeat's `claude` probe into the machine
// registry rows: the seed remote warden last probed a version (→ the claude
// column's populated face); server-self never probed (→ absent = honest
// all-null, the column shows "—"). Display-only — nothing mutates it, so no
// __resetMock entry.
const mockClaudeInfo = new Map<
  string,
  {
    version: string | null;
    cred_source: "file" | "keychain" | "both" | "none";
    sub_readable: boolean;
  }
>([
  [
    "warden-mbp5",
    { version: "2.1.211", cred_source: "keychain", sub_readable: false },
  ],
]);

// ── Fixture: monitoring telemetry, in WIRE shape (mirrors /api/monitoring).
// HONEST: one real session (Mira, offline) + one real machine (mbp5, 1 agent);
// EVERY telemetry field is null (context/cost/tokens/hardware) and accounts is
// empty — NOT the mockup's illustrative numbers ($13.93 / 18.4% / 28% / …).
// The mock always constructs and mutates ALL THREE sections; the wire type marks
// them optional (defaulted-empty on the BE), so we pin the mock's internal
// fixture to a fully-populated shape. This keeps the mock mutations
// (patchAccount / rename / delete) type-safe without `!` at every use site.
type MockMonitoring = Required<WireMonitoring>;
const MOCK_WIRE_MONITORING: MockMonitoring = {
  sessions: [
    {
      id: "mira",
      name: "Mira",
      role: "assistant",
      runtime: "claude",
      // honest-empty, for the SAME reason as effort/account below: this column
      // serves the REPORTED model (the roster row's actual_model) for staff and
      // outsource rows alike, and mock Mira has reported nothing. It used to
      // carry "claude-sonnet-4.5" here while her member row's actual_model was
      // "" — that is the configured value, which the server no longer falls back
      // to for either kind, so leaving it would make the mock the only place the
      // old two-meanings-per-column behaviour still existed.
      model: "",
      effort: "", // honest-empty — mock Mira has no telemetry
      machine: "mbp5",
      account: "", // honest-empty — mock Mira has no telemetry
      presence: "offline",
      context_pct: null,
      cost: null,
      banked_cost: null,
      tokens: null,
    },
  ],
  machines: [
    {
      machine: "mbp5",
      display_name: "mbp5", // BE fallback = id; owner may rename inline
      agents: 1,
      accounts: [],
      cpu_pct: null,
      ram_pct: null,
      battery_pct: null,
      ac_power: null,
    },
  ],
  // One demo account so the AccountCard renders in the mock (lets the owner
  // exercise inline-rename). HONEST: cost/window telemetry stays null — a real
  // value needs the warden telemetry slice; this fixture is display-only.
  accounts: [
    {
      account: "acct-demo",
      // HONEST: acct-demo reports no telemetry, so there is no reporter label —
      // the detail modal must render its email/org rows as "—", never invent.
      account_label: null,
      display_name: "acct-demo", // BE fallback = id; owner may rename inline
      machine: "mbp5",
      cost: null,
      five_hour: null,
      seven_day: null,
    },
  ],
};

// ── Fixture: build identity, in WIRE shape (mirrors /api/version).
// HONEST (VersionDTO contract): `version` stays "0.0.0" (no stable release yet);
// the UI composes its unified label v<yymmdd>-<hhmm>-<shortsha> from git_sha +
// git_time (T-e9d1 round 3 — this fixture renders v260704-0854-f6f5e1c), so both
// fields must stay REAL and parity with the Go wire. `update_available` is static
// false — the running build IS the latest ("已是最新版", so latest_version null /
// no phantom newer version). These are the running build's REAL identity (git
// HEAD f6f5e1c, committed 2026-07-04) — NOT the mockup's v1.2.0.
const MOCK_WIRE_VERSION: WireVersion = {
  version: "0.0.0",
  git_sha: "f6f5e1c",
  git_time: "2026-07-04T08:54:28+08:00",
  catalog_hash: "mock",
  update_available: false,
  latest_version: null,
};

// ── Fixture: backup health, in WIRE shape (T-da06). The mock world's scheduled
// backup is HEALTHY and recent — the cockpit's default demo state is a studio
// that HAS a retreat point. Deliberately a wire-shaped literal run through the
// same `toBackupHealth` mapper as http: mock and http can then never disagree
// about how a wire value is read.
const MOCK_WIRE_BACKUP_HEALTH: WireBackupHealth = {
  status: "healthy",
  code: "",
  // A healthy server sends an EMPTY detail (it has no failure to explain) and
  // a window of backupStaleFactor(2) x backupInterval(6h) = 43200s. Inventing
  // other numbers here would make the mock world teach a shape production
  // cannot produce — the mock exists to be indistinguishable, not decorative.
  detail: "",
  newest_backup_ts: 1785600000,
  newest_backup_age_secs: 720,
  stale_after_secs: 43200,
  since_ts: null,
  checked_ts: 1785600720,
};

// ── Fixture: role-journal seeds, in WIRE shape (mirrors the folded GETs).
// is_default=true → the response IS the seed (UI labels it "預設"). The text is
// the REAL seed content (imported verbatim), never the mockup's illustrative
// copy. owner_id mirrors the out-of-box owner.

// /api/global-context now carries ONLY the 使用者自訂 (user-custom) ADDITIVE
// block of the 3-block boot context (global-context-3block-restructure) — its
// seed is EMPTY (never written → text=""/is_default=true, mirrors
// fold_user_context). The system-interaction text is NOT served here anymore:
// it is its own document behind /api/system-interaction. "Not served here" is
// not "not editable" — it has been owner-editable since T-791e.
const MOCK_WIRE_USER_CONTEXT_EMPTY: WireGlobalContext = {
  text: "",
  owner_id: MOCK_OWNER_ID,
  schema_version: 3,
  is_default: true,
  // Stamped from the live studio name at fold time (foldGlobalContext); the
  // literal placeholder just satisfies the required wire field.
  org_name: "",
};

const MOCK_WIRE_ROLES_SEED: WireRoleDef[] = [
  {
    key: "assistant",
    // The seed role name is "Assistant" (the server seed roster) — the honest
    // DTO.name. (The office roster shows the i18n label 助理; the role-journal
    // surfaces the real doc name.)
    name: "Assistant",
    definition_md: SEED_ROLE_ASSISTANT_MD,
    // T-ae38 Duty budget. Spelled out rather than via docSizeFields because
    // this literal is evaluated at MODULE LOAD, before `mockServerSettings`
    // exists — and `foldRole` re-derives both numbers from the live setting on
    // every read anyway, so these two are only here to satisfy the wire type.
    //
    // The structural exemption stands, but it has NO instance today: a factory
    // doc is something no cap can catch (reset_role folds back to the file
    // seed, a path with no cap check on it), yet the shipped Duty seed sits far
    // BELOW the default cap, so the mock shows the same within-budget reading a
    // fresh install shows.
    // ⚠️ This block used to say the seed was deliberately OVER the 1000-char
    // default. That was retired with the oversized seed (T-e1e3, then T-795e
    // replaced it again) and this copy was missed — the server-side copies of
    // the same paragraph (domain.go, api_doc_caps_tae38_test.go, server
    // CLAUDE.md) were all corrected then. Deliberately no rune count here: read
    // seeds/role_def_assistant.md if you need its size.
    size_chars: [...SEED_ROLE_ASSISTANT_MD].length,
    cap_chars: DOC_CAP_CHARS_DEFAULTS.duty,
    owner_id: MOCK_OWNER_ID,
    schema_version: 2,
    is_default: true,
    is_seed: true, // out-of-box seed role — resettable, NOT deletable
  },
];

// Mutable in-memory state, seeded from the fixture (structuredClone so mutations
// like activate/patch don't bleed into the frozen seed).
let wireMembers: WireMember[] = structuredClone(MOCK_WIRE_MEMBERS);

// Mutable monitoring state so inline-rename (patchAccount/patchMachine) persists
// across getMonitoring calls (the frozen fixture stays untouched).
let wireMonitoring: MockMonitoring = structuredClone(MOCK_WIRE_MONITORING);

// Role-journal OVERLAY state (§6.2: owner overlay ⊕ seed). Each entry, when
// present, is the owner's self-contained edit; absent → the folded read falls
// back to the seed (is_default=true). Reset deletes the overlay (idempotent).
// For the user-custom block the "seed" is the EMPTY block above.
let globalContextOverlay: WireGlobalContext | null = null;

// ── the three boot-context blocks (T-791e) ─────────────────────────────────
// Three INDEPENDENT overlay streams, keyed "<kind>/<key>" — the same slot
// spelling the history uses, so one document never reaches another's storage.
// Absent overlay = the block is following its factory seed (is_default=true).
//
// 🔴 `boot_sequence/claude` and `boot_sequence/codex` are two DOCUMENTS, not
// two renderings of one. Their third step means opposite things, so there is
// deliberately no shared cell here for anything to fall back into: a lookup
// miss on one key resolves to that key's OWN seed, never to the other's text.
const BOOT_DOC_SEEDS: Record<string, string> = {
  "system_interaction/global": SEED_SYSTEM_INTERACTION_MD.trim(),
  "boot_sequence/claude": SEED_BOOT_SEQUENCE_MD.trim(),
  "boot_sequence/codex": SEED_BOOT_SEQUENCE_CODEX_MD.trim(),
  // T-c9c0 — a singleton keyed "global", like system_interaction.
  "offboard/global": SEED_OFFBOARD_MD.trim(),
  // T-3201 — the six lifecycle procedures, every one a singleton keyed
  // "global". The ORDER of this map mirrors bootDocRegistry
  // (server/ocserverd/api_bootdocs.go), which is the order these documents are
  // declared and listed in everywhere else; a mock that invented an order of
  // its own would make every ordered comparison against it meaningless.
  // (It used to be described as "the order GET /api/boot-docs answers in" —
  // that endpoint is gone; see __mockBootDocAddresses below.)
  "accelerated_stop/global": SEED_ACCELERATED_STOP_MD.trim(),
  "task_closeout/global": SEED_TASK_CLOSEOUT_MD.trim(),
  "task_reassign_predecessor/global": SEED_TASK_REASSIGN_PREDECESSOR_MD.trim(),
  "task_takeover_with_predecessor/global":
    SEED_TASK_TAKEOVER_WITH_PREDECESSOR_MD.trim(),
  "task_takeover_fresh/global": SEED_TASK_TAKEOVER_FRESH_MD.trim(),
  "task_unblocked/global": SEED_TASK_UNBLOCKED_MD.trim(),
};

/** The documents the server SHOWS but refuses every write to.
 *
 * 🔴 EMPTY SINCE T-6f44, AND KEPT RATHER THAN DELETED. The owner ruled on
 * 2026-08-24 that 〈新任務〉 and 〈擋著你手上任務的票解開了〉 — the two that used to
 * be in here — become editable like the other eight. His earlier ruling
 * (「以前 global context 是固定內容 我們也是會顯示 只是不給改」) was 照舊, a
 * carry-over from when the boot context was fixed text, NOT a statement about
 * these two documents' content; and 〈新任務〉 is one half of the same event as
 * 〈任務轉派 · 給接手人〉, which he could always edit.
 *
 * The set stays because the 405 machinery it drives is still real and still
 * reachable: a future document may ship read-only, and an empty set is how the
 * mock says "none today" without the cockpit's refusal path becoming code that
 * cannot be exercised at all. bootDocRegistry (server) is the truth source;
 * this mirrors it so demo mode cannot validate a screen the real server
 * refuses. */
let BOOT_DOC_READ_ONLY: ReadonlySet<string> = new Set<string>();

/** TEST-ONLY: make a document read-only for one test.
 *
 * 🔴 WHY THIS SEAM EXISTS. Since T-6f44 no SHIPPED document is read-only, so
 * every assertion about how a read-only document renders — the body without an
 * editor, the note, the absent version list — lost the only subject it could
 * be written against. Deleting those assertions would have retired a live
 * refusal path into code nothing exercises: the machinery is still reachable
 * the day a document ships read-only again, and that is precisely the day
 * nobody would notice it had rotted.
 *
 * Reset to empty by __resetMock, so a test that forgets to clean up cannot
 * leak a read-only document into the next one. */
export function __setBootDocReadOnly(keys: readonly string[]): void {
  BOOT_DOC_READ_ONLY = new Set(keys);
}

/** The server's own name for a document, as its refusals spell it
 * (bootDocRegistry's DocName). The listing carries it, so a caller reading a
 * rejection and a caller reading the listing see the same words. */
const BOOT_DOC_NAMES: Record<string, string> = {
  "system_interaction/global": "system interaction block",
  "boot_sequence/claude": "boot steps (claude)",
  "boot_sequence/codex": "boot steps (codex)",
  "offboard/global": "Stop document",
  "accelerated_stop/global": "accelerated stop sequence",
  "task_closeout/global": "task close-out procedure",
  "task_reassign_predecessor/global":
    "task reassignment document (to the predecessor)",
  "task_takeover_with_predecessor/global":
    "task reassignment document (to the successor)",
  "task_takeover_fresh/global": "new task document",
  "task_unblocked/global": "dependency-released notice",
};
const bootDocOverlays = new Map<string, string>();

/** The SSE topic a boot-block write fans. `global_context`, NOT a topic named
 * after the block: spec/sse.md §3.1 is a CLOSED set and the server drops
 * anything outside it at the publish seam, so an invented topic would fan
 * nothing while looking entirely correct here. Kept in step with TOPIC_OF in
 * hooks/useDocumentHistory.ts — the mock fanning a different topic than the
 * hook listens on is the one way this stays silent under test too. */
const BOOT_DOC_TOPIC = "global_context";

/** The seed text for one block, or null when (kind, key) names no such
 * document — which is what makes an unknown runtime key a 404 rather than a
 * silently empty page. */
function bootDocSeed(kind: BootDocKind, key: string): string | null {
  return BOOT_DOC_SEEDS[`${kind}/${key}`] ?? null;
}

function foldBootDoc(kind: BootDocKind, key: string): WireBootDoc {
  const seed = bootDocSeed(kind, key);
  if (seed === null) {
    throw mockApiError(
      `http 404 for GET /api/boot-docs/${kind}/${key}`,
      404,
      `boot document '${kind}/${key}' does not exist`,
    );
  }
  const overlay = bootDocOverlays.get(`${kind}/${key}`);
  const text = overlay ?? seed;
  // The two halves the READ face names (T-3201). The mock splits because it is
  // standing in for the server here; the cockpit never does — it is handed
  // these. A stored text with no marker is all body, the same lenient reading
  // the server's bootDocBodyOf takes.
  const { head, body, split } = docSplitHeadBody(text);
  return {
    kind,
    key,
    text,
    read_only_head: split ? head : "",
    body: split ? body : text,
    owner_id: MOCK_OWNER_ID,
    schema_version: 3,
    size_chars: [...text].length,
    // The LIVE setting, not the shipped default: the server enforces this
    // read's `cap_chars` against `doc.cap_chars.<kind>`, so a mock pinned to
    // the default would keep answering 60000/15000 after the owner moved the
    // knob — and the page sizes its edits against exactly this number.
    cap_chars: bootDocCap(kind),
    is_default: overlay === undefined,
    // Every one of the ten ships a seed, so the 還原出廠版 path is always
    // real here. The field is still reported rather than hardcoded true at the
    // call site: it is the flag the cockpit gates that affordance on, and a
    // block added later without a seed must be able to say so.
    has_seed: true,
    read_only: BOOT_DOC_READ_ONLY.has(`${kind}/${key}`),
  };
}

/** Which cap judges this document — the LIVE setting where the server reads
 * one, and `taskEventCapCharsDefault` for the four task events, which is a
 * constant on the server too. 加速停止 shares 〈停止〉's setting, mirroring the
 * registry row that calls `offboardCap()` for both. */
function bootDocCap(kind: BootDocKind): number {
  switch (kind) {
    case "system_interaction":
      return mockServerSettings.doc_cap_chars_system_interaction;
    case "boot_sequence":
      return mockServerSettings.doc_cap_chars_boot_sequence;
    case "offboard":
    case "accelerated_stop":
      return mockServerSettings.doc_cap_chars_offboard;
    case "task_closeout":
    case "task_reassign_predecessor":
    case "task_takeover_with_predecessor":
    case "task_takeover_fresh":
    case "task_unblocked":
      return TASK_EVENT_CAP_CHARS_DEFAULT;
  }
}

/** The 405 both write faces answer for a read-only document. `suffix` is the
 * route tail so the thrown message names the call that was refused. */
function refuseReadOnlyBootDoc(
  kind: BootDocKind,
  key: string,
  suffix: string,
): void {
  if (!BOOT_DOC_READ_ONLY.has(`${kind}/${key}`)) return;
  throw mockApiError(
    `http 405 for POST /api/boot-docs/${kind}/${key}${suffix}`,
    405,
    `the ${BOOT_DOC_NAMES[`${kind}/${key}`] ?? kind} is a read-only document — ` +
      "it is shown so you can see what agents are told, but no caller may edit " +
      "it and there is no version of it other than the shipped one; nothing was written",
  );
}

/** The (kind, key) pairs this mock serves — parsed back out of BOOT_DOC_SEEDS
 * rather than restated, so it cannot name a document it would then 404.
 *
 * 🔴 EXPORTED FOR ONE TEST, not for the cockpit (T-3201). `GET /api/boot-docs`
 * is gone: which documents exist is the frozen spec's `BootDocKind` enum, and
 * the cockpit's row table is indexed by it, so a missing row is a compile error
 * rather than a listing to compare against. What still needs measuring is that
 * this MOCK serves the same set — it stands in for the server in every other
 * frontend test, and a document missing here makes those tests pass on a fleet
 * that does not exist. api/mock.boot-doc-registry.test.ts is the only caller. */
export function __mockBootDocAddresses(): { kind: BootDocKind; key: string }[] {
  return Object.keys(BOOT_DOC_SEEDS).map((slot) => {
    const cut = slot.lastIndexOf("/");
    return {
      kind: slot.slice(0, cut) as BootDocKind,
      key: slot.slice(cut + 1),
    };
  });
}
const roleOverlays = new Map<string, WireRoleDef>();
// Owner-created CUSTOM roles (M2-2): a wire doc per minted key (is_seed=false).
// Distinct from roleOverlays (edits over a seed) — a custom role IS its doc.
const customRoles = new Map<string, WireRoleDef>();

// Mirrors the server name pool (server/ocserverd/domain.go; M2 隨機成員名 mock parity):
// the server-side pool a no-name role create draws the founding member's name
// from. Never "Mira" (the seed identity stays unmistakable).
const MOCK_MEMBER_NAME_POOL = [
  "Nova",
  "Kai",
  "Ravi",
  "Luna",
  "Iris",
  "Milo",
  "Zara",
  "Theo",
  "Aria",
  "Ezra",
  "Vera",
  "Nico",
  "Suki",
  "Remy",
  "Isla",
  "Otis",
  "Faye",
  "Juno",
  "Cleo",
  "Enzo",
  "Mika",
  "Wren",
  "Lyra",
  "Dax",
] as const;

/** Pick a fresh pool name, excluding every current roster name (case-
 * insensitive) — mock parity with the server name picker (domain.go). Exhausted
 * pool falls back to a numeric-suffix candidate, always returning fresh. */
function pickMockMemberName(): string {
  const taken = new Set(wireMembers.map((m) => m.name.trim().toLowerCase()));
  const available = MOCK_MEMBER_NAME_POOL.filter(
    (n) => !taken.has(n.toLowerCase()),
  );
  if (available.length > 0) {
    return available[Math.floor(Math.random() * available.length)];
  }
  for (;;) {
    const base =
      MOCK_MEMBER_NAME_POOL[
        Math.floor(Math.random() * MOCK_MEMBER_NAME_POOL.length)
      ];
    const candidate = `${base}-${2 + Math.floor(Math.random() * 998)}`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}
// Mirrors the server custom-role template (server/ocserverd/domain.go; the 兩 section
// 待填說明 scaffold a fresh custom role starts from).
const CUSTOM_ROLE_TEMPLATE_MD = `# 角色定義

## 你是誰

（待填：這個角色的身分與定位——用一兩句話說明「你是誰」、在辦公室裡站什麼位置、\
面對 owner 與其他成員時以什麼視角說話。）

## 你做什麼

（待填：這個角色的職責與工作方式——負責哪些事、怎麼做事、輸出長什麼樣、\
與 owner 及其他成員怎麼協作、什麼事不歸你管。）
`;
// Lessons OVERLAY (owner overlay ⊕ seed), keyed by the BARE `role_key` (T-2
// removed the `task_type` half of the old composite key). A
// save stores the overlay so the folded read is now owner-edited
// (is_default=false); absent → the folded read is the REAL seed. PER-ROLE doc:
// agents sharing a role share the overlay.
const lessonsOverlays = new Map<string, WireLessons>();

// Insight OVERLAY (T-3809), keyed by the BARE `role_key` — the same shape
// lessons uses since T-2. An absent entry folds
// against INSIGHT_SEEDS below (T-e1e3).
const insightOverlays = new Map<string, WireInsight>();

// The PER-ROLE insight file seeds (T-e1e3) — the mock's mirror of the server's
// `seeds/insight_<roleKey>.md` lookup. 🔴 A MAP, not a single constant: the
// lessons seed is one shared file every role reads, and doing that to insight
// would ship the assistant's judgement calls to every role out of the box.
// A role absent from this map has NO seed and folds to "" — that is the
// intended reading for every role but `assistant` today.
const INSIGHT_SEEDS: Record<string, string> = {
  assistant: SEED_INSIGHT_ASSISTANT_MD,
};

// In-memory chat log. HONEST HARD LINE: this stores ONLY messages the owner
// actually sends (postChat). The mock NEVER fabricates a reply from Mira (or any
// member) — an offline member does not answer, and real replies arrive
// asynchronously from a spawned agent over SSE. So after sending, the thread
// shows only the owner's own message. Fabricating an assistant reply here would
// be as dishonest as a fake lastSeen for a never-online member.
let chatLog: ChatMessage[] = [];
// T-4e95: chat message ids used to be `mock-${Date.now()}` alone, and two posts
// inside the same millisecond therefore got the SAME id — reproduced, not
// theorised. That was survivable while nothing pointed AT a message; a reply
// carries the quoted message's id and nothing else, so an ambiguous id makes
// the quote resolve to whichever row happens to be first (and collides React's
// keys besides). The real server mints `c-` + 12 random hex; this counter is the
// mock's cheapest way to keep the same promise — one id, one message.
//
// Deliberately NOT reset by __resetMock: a counter that restarted could hand a
// fresh message the id of one a test still holds a reference to, which is the
// very ambiguity it exists to remove.
let mockChatSeq = 0;

// In-memory read receipts, keyed `{reader}::{peer}` → the monotonic last-read
// watermark. Mirrors the BE chat_read table. In mock mode the OWNER marks its own
// messages read through markChatRead, and — since the mock never fabricates a
// member reply — a message the owner sends to a member is never marked read by
// that member at all. So the mock's "read ✓" is honest: it reflects only real
// recorded watermarks, never a fabricated peer read.
const chatReads = new Map<string, ChatReadReceipt>();

// In-memory reply cards (等我回覆卡). HONEST HARD LINE (same as chatLog): the
// mock NEVER fabricates an agent's ask — a real card is opened by a live agent
// through the MCP tool, so out of the box this is empty (the page shows its
// honest ✓ empty state). Tests inject cards via __injectMockReplyCard to
// exercise the answer / re-answer seam.
let replyCards: ReplyCard[] = [];

// In-memory tasks (M3 任務卡) + live outsource workers + task manuals. SAME
// honest hard line as chatLog / replyCards: a real task is created by an agent
// through MCP, so tasks/workers start empty (the tasks page shows its honest
// 目前沒有任務 state); manuals start empty because 出廠不含任何類型 (spec
// §5.1) — the owner creates every type. Tests inject via __injectMockTask /
// __injectMockOutsourceWorker / __injectMockTaskType to exercise the
// list / filter / terminate / priority / message / manual seams.
// 🔴 THE STORE HOLDS EACH ARTIFACT WHOLE, which is why the row type is not
// plain `TaskView`. T-66 narrowed `TaskView.artifacts` to an id+label INDEX,
// but an index is a READ SHAPE, not what a store keeps: the server's store
// holds the full deliverable and its two reads project from it (`get_task` →
// the index, `list_task_artifacts` → the full rows). A mock whose store held
// only the index could not answer the second read at all — which is exactly
// how it came to `return []` and tell a reader 「還沒有產物」 about a task whose
// badge had just said N.
export type MockTaskRow = Omit<TaskView, "artifacts"> & { artifacts?: TaskArtifactView[] };
let tasks: MockTaskRow[] = [];

// The retained PREVIOUS versions of a pinned deliverable (T-60), keyed by
// artifact id — the mock's stand-in for `task_artifact_history`. Nothing in the
// cockpit WRITES here (replace is the executing agent's MCP verb, not an owner
// action), so the only producer is __injectMockArtifactVersions; un-pinning an
// artifact drops its versions the way the server's transaction does.
let artifactVersions = new Map<string, TaskArtifactVersionView[]>();
let outsourceWorkers: OutsourceWorkerView[] = [];
let taskManuals: TaskManualView[] = [];

// Product-guide docs (the 使用說明 nav tab) — a representative fixture so mock-mode
// (dev screenshots / vitest) renders the same list→doc flow the real embed
// serves. NOT the authoritative content (that is docs/guide/, embedded
// server-side); real-SHAPED docs keep the mock honest about the shape.
//
// T-68f1 widened this from one link-free doc to three, because the shape the
// page must now handle is CROSS-DOC LINKS, and a single doc with no links can
// neither exercise nor regress them. The slugs mirror real embedded ones, the
// list is slug-sorted like listDocsFrom, and the link targets deliberately
// cover all four classes the renderer distinguishes:
//   • `interface.md` / `why.md`  — embedded → an in-app doc button
//   • `../dev/agent-env.md`      — a real repo path that is NOT shipped → the
//                                  literal-text fallback (the 404 that isn't)
//   • an https:// target         — the external anchor, unchanged
//   • `javascript:`              — the scheme that must never become clickable
//   • `interface.md` FROM interface — a link to the doc you are already on, so
//                                  the page's `next === slug` self-link guard
//                                  has a fixture with discriminating power
//                                  (before this, removing that guard left the
//                                  whole suite green — review3 §2.5)
// plus a `> [!NOTE]` alert AND a plain blockquote in the same doc, so both the
// marker-stripping and "an alert must not look like an ordinary quote" have a
// fixture (the latter is only decidable in a real browser — see the CT spec).
const mockDocs: DocView[] = [
  {
    slug: "install",
    title: "安裝、升級與移除",
    markdownMd:
      "# 安裝、升級與移除\n\n" +
      "一行指令裝好,服務常駐在背景。\n\n" +
      "> [!NOTE]\n" +
      "> 控制台只綁 loopback(`127.0.0.1`)。\n\n" +
      "下載頁 → [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases)\n\n" +
      "**agent 的環境變數怎麼設** → [../dev/agent-env.md](../dev/agent-env.md)\n\n" +
      "> 一般引言,沒有 alert marker。\n" +
      "> 它和上面那個提示框必須看起來不一樣。\n",
  },
  {
    slug: "interface",
    title: "介面說明",
    markdownMd:
      "# 介面說明\n\n" +
      "控制台的主導覽有辦公室、請示、任務、監控、使用說明五個分頁,設定在右上角的齒輪裡。\n\n" +
      "想知道為什麼這樣設計 → [為什麼是 OffiCraft](why.md)\n\n" +
      "你正在看的就是這一份 → [介面說明(本頁)](interface.md)\n\n" +
      "專案首頁 → [GitHub](https://github.com/pkyosx/OffiCraft)\n",
  },
  {
    slug: "why",
    title: "為什麼是 OffiCraft",
    markdownMd:
      "# 為什麼是 OffiCraft\n\n" +
      "OffiCraft 是一間跑在你自己 Mac 上的 AI 工作室。\n\n" +
      "## 使用說明\n\n" +
      "在主導覽最右邊的「使用說明」分頁裡閱讀各項功能的說明。\n\n" +
      "- 介面上的欄位是什麼意思 → [介面說明](interface.md)\n" +
      "- 同一份、從 repo root 看的路徑 → [介面說明(長路徑)](docs/guide/interface.md)\n" +
      "- 完整安裝、升級與移除 → [安裝、升級與移除](install.md)\n" +
      "- 不該點的東西 → [別點我](javascript:alert(1))\n",
  },
];

// Mock topic fan-out. The mock has no real SSE stream, but the reply-card
// surface has TWO independent live consumers (the nav badge's count hook and
// the page's list hook) that reconcile on the "reply_card" topic — without a
// local fan-out the badge would go stale the moment the page answers a card
// in mock mode (the http adapter gets this for free from the server's SSE).
// Scope: mutations whose mounted hooks reconcile from SSE (reply cards, tasks,
// outsource workers, and member avatars) emit the matching production topic.
// The mock deliberately passes NO SseDelta (the second argument stays absent):
// it has no wire frame to project, and an absent delta is the honest "something
// in this topic changed, refetch the lot" — the mock's behaviour is unchanged by
// the one-item refetch the http adapter can now name (T-8115).
const topicSubscribers = new Set<(topic: string) => void>();
function emitTopic(topic: string): void {
  for (const cb of [...topicSubscribers]) cb(topic);
}

/** The circled indices as the SERVER stores them: deduped + ascending, so
 * `[2,0]` and `[0,2]` are the same stored answer. */
function storedOptionIdxs(idxs: number[]): number[] {
  return [...new Set(idxs)].sort((a, b) => a - b);
}

/** Answer-input validation shared by answer/re-answer — mirrors the server's
 * 400s: an empty answer (no option, no text, no attachments) is rejected, and
 * an EMPTY `optionIdxs` list counts as empty rather than as an answer (the
 * server's len() guard — a nil check would let `[]` through and close the card
 * on nothing); an out-of-range index is rejected; and a `single` card given
 * more than one index is rejected. */
function validateReplyAnswer(card: ReplyCard, answer: ReplyCardAnswerInput) {
  const idxs = answer.optionIdxs ?? [];
  const hasOption = idxs.length > 0;
  const hasText = (answer.text ?? "").trim().length > 0;
  const hasAtts = (answer.attachments ?? []).length > 0;
  if (!hasOption && !hasText && !hasAtts) {
    throw mockApiError(
      `http 400 for answer /api/reply-cards/${card.id}/answer`,
      400,
      "an answer needs an option, text, or attachments",
    );
  }
  for (const idx of idxs) {
    if (idx < 0 || idx >= card.options.length) {
      throw mockApiError(
        `http 400 for answer /api/reply-cards/${card.id}/answer`,
        400,
        `option_idxs out of range: ${idx}`,
      );
    }
  }
  if (card.selectMode !== "multi" && storedOptionIdxs(idxs).length > 1) {
    throw mockApiError(
      `http 400 for answer /api/reply-cards/${card.id}/answer`,
      400,
      "this card takes at most one option",
    );
  }
}

/** Build the stored answer view from the input — attachments echo back as
 * data-URI refs (the mock has no served blob endpoint), the SAME rule as
 * postChat, so previews render identically in mock mode. */
function toStoredReplyAnswer(
  answer: ReplyCardAnswerInput,
  stamp: number,
): NonNullable<ReplyCard["answer"]> {
  const attachments: ChatAttachmentView[] = (answer.attachments ?? []).map(
    (att, i) => {
      const dataUriMime = att.dataB64.startsWith("data:")
        ? att.dataB64.slice(5, att.dataB64.indexOf(";"))
        : "";
      const mime = att.mime || dataUriMime || "application/octet-stream";
      return {
        id: `mock-rc-att-${stamp}-${i}`,
        url: att.dataB64,
        filename: att.filename || "",
        mime,
        isImage: mime.startsWith("image/"),
      };
    },
  );
  const idxs = answer.optionIdxs ?? [];
  return {
    optionIdxs: idxs.length > 0 ? storedOptionIdxs(idxs) : null,
    text: (answer.text ?? "").trim(),
    attachments,
  };
}

function findReplyCard(id: string): ReplyCard {
  const card = replyCards.find((c) => c.id === id);
  if (!card) {
    throw mockApiError(
      `http 404 for /api/reply-cards/${id}`,
      404,
      `reply card '${id}' not found`,
    );
  }
  return card;
}

/** Read-time join mirroring the server's `reply_card_status`: the CURRENT
 * status of the card a chat message / task step carries, or null when it
 * carries none (or the card is missing). Computed at read time — the mock,
 * like the server, never stores this on the message/step. */
function mockReplyCardStatusOf(
  replyCardId: string | null,
): "waiting" | "answered" | "expired" | null {
  if (!replyCardId) return null;
  const card = replyCards.find((c) => c.id === replyCardId);
  return card ? card.status : null;
}

/** How much of a quoted message a quote line carries. A MIRROR of the server's
 * `chatReplyQuoteMaxChars` (server/ocserverd/wire.go) — the mock has no server to
 * ask, so the number has to be written twice.
 *
 * 🔴 TWO COPIES, ONE GUARD. Both sides used to assert 60 against their own
 * hard-coded literal (Go: `wantQuoteRunes`; here: the 61-rune expectation in
 * mock.reply-to.test.ts), so moving the server's constant and updating only the
 * Go test left this side green and cutting at the old length — offline preview
 * disagreeing with the live product, which is the one failure the mock exists to
 * prevent. `mock.reply-to.test.ts` now READS wire.go and fails when the two
 * numbers differ; it is the reason a comment is enough here. */
const MOCK_REPLY_QUOTE_MAX_CHARS = 60;

/** Read-time join mirroring the server's `reply_to_chat`: the message a reply
 * quotes, snapshotted as the read happens.
 *
 * 🔴 UNCONDITIONAL, exactly like the server (T-4e95, owner ruling 2026-08-21).
 * No "is it already in this window" check, no cache. null when the message
 * replies to nothing AND null when it replies to something the log no longer
 * carries — the caller tells the two apart by `replyTo`, which never
 * disappears. Mock mode exists so an offline preview behaves like the real
 * thing; a mock that only sometimes attached the quote would preview a bug the
 * server does not have. */
function mockReplyToChatOf(
  replyTo: string | null | undefined,
): ChatReplyQuote | null {
  if (!replyTo) return null;
  const quoted = chatLog.find((m) => m.id === replyTo);
  if (!quoted) return null;
  const oneLine = quoted.body.split(/\s+/).filter(Boolean).join(" ");
  // Cut by CODE POINT, not by `.length`. The server counts runes, and
  // String.prototype.length counts UTF-16 units — they agree on the CJK this
  // studio is mostly written in and disagree on anything above the BMP (an
  // emoji is two units and one rune), which is exactly the kind of quiet
  // divergence a mock exists not to have.
  const runes = [...oneLine];
  return {
    id: quoted.id,
    from: quoted.from,
    // "" like the server on every read that resolves no display names — which
    // is every read the browser makes.
    fromName: "",
    // The QUOTED message's own recipient — the mock joins it off the same log
    // row the server joins it off, so an offline preview draws the same
    // 「寄件者 → 收件者」 a live one does, cross-conversation quotes included.
    to: quoted.to,
    toName: "",
    content:
      runes.length > MOCK_REPLY_QUOTE_MAX_CHARS
        ? runes.slice(0, MOCK_REPLY_QUOTE_MAX_CHARS).join("") + "\u2026"
        : oneLine,
  };
}

/** One logged message as a READ serves it: a copy (so callers never mutate the
 * log) carrying both read-time joins. Every mock chat read goes through this,
 * for the same reason every server read goes through servedChatMessageDTO. */
function mockServedChatMessage(m: ChatMessage): ChatMessage {
  return {
    ...m,
    replyCardStatus: mockReplyCardStatusOf(m.replyCardId),
    replyToChat: mockReplyToChatOf(m.replyTo),
  };
}

/** Terminal task statuses (spec: 已完成/終止 為終態) — shared by the mock's
 * count / terminate / priority guards (mirrors the server's closed-set rule). */
const TERMINAL_TASK_STATUSES = new Set(["done", "terminated", "duplicated"]);

// The server's task_no for an id (domain.go TaskNo) — the mock needs it for a
// dep whose task row is absent, exactly where the server fills the number
// rather than leaving it blank (T-a3e4). The number IS the id (T-5291), so
// there is nothing to derive; the old `slice(2, 6)` here was a THIRD copy of a
// projection that no longer exists.
function deriveMockTaskNo(taskId: string): string {
  return taskId;
}

// withWorkerTaskJoin fills the bound-task fields the SERVER now folds into the
// worker DTO (T-a3e4: task_no / task_created_ts / task_type_key /
// task_type_name). Mock parity matters here in a specific way: the panel used
// to compute these itself from a full task-list download, so if the mock kept
// leaving them blank, the hook change would look correct in tests while the
// real panel lost its labels. Honest ""/0 when the task cannot be resolved.
function withWorkerTaskJoin(w: OutsourceWorkerView): OutsourceWorkerView {
  const task = tasks.find((t) => t.id === w.taskId);
  const typeKey = task?.typeKey ?? "";
  return {
    ...w,
    taskNo: task?.taskNo ?? "",
    taskCreatedTs: task?.createdTs ?? 0,
    taskTypeKey: typeKey,
    taskTypeName:
      taskManuals.find((m) => m.typeKey === typeKey)?.displayName ?? "",
  };
}

/** Terminal STEP statuses (done = finished, superseded = re-plan history) — a
 * reassign rewinds every OTHER step to pending, mirroring the server. */
const TERMINAL_STEP_STATUSES = new Set(["done", "superseded"]);

/** The next codename for `model`: `<prefix>-<MAX+1>` over the SAME family
 * prefix (mirrors DeriveCodename / CodenamePrefix — a per-family ascending
 * sequence, never reused). */
function deriveCodename(model: string, existing: string[]): string {
  const m = model.toLowerCase();
  const prefix = m.includes("opus")
    ? "O"
    : m.includes("sonnet")
      ? "S"
      : m.includes("haiku")
        ? "H"
        : "X";
  let max = 0;
  for (const c of existing) {
    if (!c.startsWith(`${prefix}-`)) continue;
    const n = Number(c.slice(prefix.length + 1));
    if (Number.isInteger(n) && n > max) max = n;
  }
  return `${prefix}-${max + 1}`;
}

function findTaskManual(typeKey: string): TaskManualView {
  const m = taskManuals.find((x) => x.typeKey === typeKey);
  if (!m) {
    throw mockApiError(
      `http 404 for /api/task-manuals/${typeKey}`,
      404,
      `task type '${typeKey}' not found`,
    );
  }
  return m;
}

function findTask(id: string): MockTaskRow {
  const t = tasks.find((x) => x.id === id);
  if (!t) {
    throw mockApiError(
      `http 404 for /api/tasks/${id}`,
      404,
      `task '${id}' not found`,
    );
  }
  return t;
}

function markRead(
  reader: string,
  peer: string,
  lastReadTs: number,
): ChatReadReceipt {
  const key = `${reader}::${peer}`;
  const prior = chatReads.get(key);
  // Monotonic: keep the higher watermark (a stale report never rewinds it).
  if (prior && prior.lastReadTs >= lastReadTs) return prior;
  const receipt: ChatReadReceipt = {
    readerId: reader,
    peerId: peer,
    lastReadTs,
  };
  chatReads.set(key, receipt);
  return receipt;
}

/** The OWNER's live unread COUNT for `peer` — the SAME rule the backend applies
 * (the server unread fold, domain.go): how many messages ADDRESSED TO the owner
 * from `peer` carry a ts newer than the owner's watermark for that
 * conversation. Agent↔agent messages never count (recipient ≠ owner) and the
 * count is independent of the member's presence. HONEST: the mock never
 * fabricates a member reply, so this is 0 in normal mock use — it counts only
 * when a member→owner message really lands in the log (tests inject one via
 * __injectMockChat). */
function unreadCountOf(peer: string): number {
  const watermark = chatReads.get(`${MOCK_OWNER_ID}::${peer}`)?.lastReadTs ?? 0;
  return chatLog.filter(
    (m) => m.to === MOCK_OWNER_ID && m.from === peer && m.ts > watermark,
  ).length;
}

/** Fold the user-custom block: overlay ⊕ the EMPTY seed (a structuredClone so
 * the caller can never mutate our state). Mirrors fold_user_context. */
function foldGlobalContext(): WireGlobalContext {
  const folded = structuredClone(
    globalContextOverlay ?? MOCK_WIRE_USER_CONTEXT_EMPTY,
  );
  // The studio name is a settings-tier value the agent reads back here (T-d693);
  // stamp the live mock org name so mock global-context matches the server.
  folded.org_name = mockServerSettings.org_name;
  return folded;
}

/** The seed role for `key`, or throw (mirrors a 404 for an unknown role). */
function roleSeed(key: string): WireRoleDef {
  const seed = MOCK_WIRE_ROLES_SEED.find((r) => r.key === key);
  if (!seed) throw new Error(`mock: role not found: ${key}`);
  return seed;
}

/** Fold one role's overlay ⊕ custom doc ⊕ seed (structuredClone; never leaks
 * state). A custom role IS its stored doc; an edit rides roleOverlays like a
 * seed edit does. */
function foldRole(key: string): WireRoleDef {
  const folded = structuredClone(
    roleOverlays.get(key) ?? customRoles.get(key) ?? roleSeed(key),
  );
  // T-ae38: size/cap are DERIVED from the folded text and the live setting, the
  // way the server derives them in foldRoleDefDTO — never carried along on the
  // stored overlay. An overlay written before the owner raised the Duty cap
  // would otherwise keep reporting the old ceiling forever.
  return { ...folded, ...docSizeFields(folded.definition_md ?? "", "duty") };
}

// ── retained document revisions (T-7d33) ───────────────────────────────────
// Mirrors server/ocserverd/api_document_history.go: every write to an editable
// long-form doc first RETAINS the state it replaced, newest first, capped at
// DOCUMENT_HISTORY_CAP. The retained `content` uses the kind's OWN field names
// (the wire contract), including the `tombstoned` flag the overlay kinds carry
// — restoring a tombstoned revision must put the doc back on the seed, not
// write the folded seed text back as an owner edit.
const DOCUMENT_HISTORY_CAP = 3;

/** T-791e: the three boot-context blocks keep TEN revisions, not three.
 *
 * The owner's ruling, and the reason is the workflow this surface is for: these
 * blocks are where pasted proposals land, one section at a time, so a single
 * afternoon can be a dozen small saves. Three slots would mean the version
 * before the afternoon started is gone before it ends. Retention is counted in
 * WRITES, not in time — ten saves is ten saves whether they took a minute or a
 * month — which is exactly why the surface has to say so on screen: nothing
 * else would tell an owner that tapping save five times just consumed half of
 * what he could go back to. 還原出廠版 is never consumed by any of this. */
const BOOT_DOC_HISTORY_CAP = BOOT_DOC_HISTORY_KEPT;

/** How many revisions this kind retains. One place, so the two numbers cannot
 * end up meaning different things at the write door and the read door. */
function historyCapFor(kind: DocumentKind): number {
  return kind === "system_interaction" ||
    kind === "boot_sequence" ||
    kind === "offboard"
    ? BOOT_DOC_HISTORY_CAP
    : DOCUMENT_HISTORY_CAP;
}
/** ONE retained revision as the mock STORES it — the whole snapshot, because a
 * restore has to be able to write the text back. The three reads project it:
 * the list through `directoryRow` (no text), the named read and the restore
 * receipt through their own mappers. Deliberately NOT `WireDocumentHistory`:
 * since T-1170 that DTO is the catalogue row and carries no `content`, so
 * typing the store as it would make the store unable to hold what it is for. */
interface StoredRevision {
  id: number;
  content: Record<string, string>;
  created_ts: number;
  actor_id: string;
}

/** The stored revision as the CATALOGUE ROW the server serves — `field_chars`
 * measured off the snapshot in code points, `tombstoned` lifted out as its own
 * boolean. Going through the wire shape (rather than building the view row
 * directly) is what keeps the mock honest: it exercises the same mapper the
 * http adapter does, so a rename on either side of that seam breaks both. */
function directoryRow(h: StoredRevision): WireDocumentHistory {
  return {
    id: h.id,
    created_ts: h.created_ts,
    actor_id: h.actor_id,
    tombstoned: h.content["tombstoned"] === "true",
    field_chars: contentSizes(h.content),
  };
}

const documentHistories = new Map<string, StoredRevision[]>();
let nextDocumentHistoryId = 1;

const historySlot = (kind: DocumentKind, key: string) => `${kind}/${key}`;

/** The RETIRED four-field bundle. `documentHistoryAllowed` (api_document_history.go)
 * answers 400 for it on BOTH routes, naming the two replacements — and the mock
 * is the adapter every frontend test runs against, so a mock that answered 200
 * here would hide exactly the class of bug the server refusal exists to catch:
 * a surface still addressing the dead kind looks alive under test and 400s in
 * production. Message kept verbatim in step with `legacyTaskManualKindMsg`. */
const RETIRED_DOCUMENT_KIND_MSG =
  'document history kind "task_manual" was retired: ' +
  'use "task_manual_sop" or "task_manual_learnings"';

function refuseRetiredDocumentKind(kind: DocumentKind, call: string): void {
  if (kind !== "task_manual") return;
  throw mockApiError(`http 400 for ${call}`, 400, RETIRED_DOCUMENT_KIND_MSG);
}

// The overlay kinds whose document ROW exists only once something has been
// written: dal.go's SaveWithDocumentHistory retains a revision only when the
// in-transaction snapshot is non-empty, and the snapshot readers return "{}"
// when the row is absent. So the FIRST customization of a seed/default document
// replaces nothing and retains NOTHING — history starts at the second write.
// (A reset is a write too: it persists a tombstoned row, which the next write
// then retains.) task_manual is not tracked here — its row is the manual
// itself, created by createTaskManual.
const documentRows = new Set<string>();

const markDocumentRow = (kind: DocumentKind, key: string) =>
  documentRows.add(historySlot(kind, key));

const MANUAL_KINDS: readonly DocumentKind[] = [
  "task_manual",
  "task_manual_sop",
  "task_manual_learnings",
];

function hasDocumentRow(kind: DocumentKind, key: string): boolean {
  if (MANUAL_KINDS.includes(kind)) {
    return taskManuals.some((m) => m.typeKey === key);
  }
  // T-e271: a task description's "row" is the TASK row — it exists from the
  // moment the task is created, so the very first correction already replaces
  // something. Whether that something is worth keeping is snapshotDocument's
  // call (an empty description retains nothing), exactly as on the server.
  // T-2ebe: the title's row is the same TASK row, for the same reason.
  if (kind === "task_description" || kind === "task_title") {
    return tasks.some((t) => t.id === key);
  }
  return documentRows.has(historySlot(kind, key));
}

/** Drop one document's retained revisions along with the document itself — the
 * server does this in the SAME transaction as the delete (dal.go DeleteRoleDef
 * / DeleteTaskManual), so a deleted document leaves no readable echo behind. */
function dropDocumentHistory(kind: DocumentKind, key: string): void {
  documentHistories.delete(historySlot(kind, key));
  documentRows.delete(historySlot(kind, key));
}

/** The lessons document of one role. EXACT match on the bare role_key, mirroring
 * DeleteLessonsForRole on the server: T-2 collapsed the compound
 * "<role>::<task_type>" key to the role_key alone, and a prefix match on a key
 * with no terminator would reach a neighbouring role whose key merely starts
 * with this one. */
function dropRoleLessonsHistory(roleKey: string): void {
  const target = historySlot("lessons", roleKey);
  for (const slot of [...documentHistories.keys()]) {
    if (slot === target) {
      documentHistories.delete(slot);
    }
  }
  for (const slot of [...documentRows]) {
    if (slot === target) documentRows.delete(slot);
  }
  lessonsOverlays.delete(roleKey);
}

/** The one insight document of one role (T-3809). EXACT EQUALITY, the same
 * shape its lessons twin above now uses: the key is the bare role_key with no
 * "::" terminator, so a prefix match would delete r-abcdef's retained versions
 * while deleting r-abc. (Until T-2 the lessons key was compound and DID need a
 * prefix match; that is why the two used to differ.)
 *
 * 🔴 WHY THIS FUNCTION EXISTS AT ALL rather than a line added above: adding
 * "insight" to DocumentKind produces NO error anywhere in this file — the type
 * is used as a parameter type here, never in an exhaustive switch, so the
 * compiler cannot notice a missing branch. Before this, deleting a role left the
 * mock still serving that role's insight history while the server had dropped
 * it, and no test in the repo would have gone red. */
function dropRoleInsightHistory(roleKey: string): void {
  documentHistories.delete(historySlot("insight", roleKey));
  documentRows.delete(historySlot("insight", roleKey));
  insightOverlays.delete(roleKey);
}

/** The document's CURRENT persisted state as a history content map, or null
 * when there is no such document (the server 404s / no-ops there). */
function snapshotDocument(
  kind: DocumentKind,
  key: string,
): Record<string, string> | null {
  switch (kind) {
    case "global_context": {
      const overlay = globalContextOverlay;
      return {
        text: overlay?.text ?? "",
        tombstoned: String(overlay === null),
      };
    }
    case "role_definition": {
      const overlay = roleOverlays.get(key) ?? customRoles.get(key);
      if (overlay) {
        return {
          name: overlay.name,
          definition_md: overlay.definition_md,
          tombstoned: "false",
        };
      }
      const seed = MOCK_WIRE_ROLES_SEED.find((r) => r.key === key);
      if (!seed) return null;
      // 🔴 EMPTY, not the seed text (T-40f0 node 11). A tombstone means "this
      // document is following its shipped default"; the server's reset writes
      // `RoleDef{RoleKey: role, Tombstoned: true}` (api_roles.go), so the
      // retained snapshot's text column holds the ZERO VALUE. Filling in the
      // seed here made the mock more generous than the server, and the cost was
      // not academic: the display-layer defect this node fixes was structurally
      // ABSENT from every mock-built fixture, so anyone writing a test off the
      // mock would have written a permanently-green assertion. The name goes
      // `name` is OMITTED rather than blanked: roleDefHistorySnapshot leaves it
      // out entirely, and `applyDocumentHistory`'s `content.name ?? current.name`
      // then leaves the live name standing — which is the server's own rule
      // (a restore puts the TEXT back, it does not rename the role).
      return { definition_md: "", tombstoned: "true" };
    }
    case "lessons": {
      const overlay = lessonsOverlays.get(key);
      // Same server parity as role_definition above: a tombstoned row stores
      // "" and the seed text is what the FOLD supplies, not what the revision
      // holds. (No route can produce a tombstoned lessons row today — there is
      // no reset_lessons — so this arm is parity for its own sake.)
      return {
        text: overlay?.text ?? "",
        tombstoned: String(overlay === undefined),
      };
    }
    case "insight": {
      // No seed to fall back to, so an absent overlay snapshots as the honest
      // empty doc rather than as seed text.
      const overlay = insightOverlays.get(key);
      return {
        text: overlay?.text ?? "",
        tombstoned: String(overlay === undefined),
      };
    }
    case "task_manual": {
      const manual = taskManuals.find((m) => m.typeKey === key);
      if (!manual) return null;
      return {
        purpose: manual.purpose,
        fields: JSON.stringify(manual.fields),
        sop_md: manual.sopMd,
        learnings: manual.learnings,
      };
    }
    // The two SPLIT series (T-1f39): one field each, and an EMPTY field is
    // "nothing worth retaining" — taskManualSopHistorySnapshot answers "{}"
    // there, which SaveWithDocumentHistories drops. Without that, the first SOP
    // a blank manual is ever given would burn a version slot on emptiness.
    case "task_manual_sop": {
      const manual = taskManuals.find((m) => m.typeKey === key);
      if (!manual || manual.sopMd === "") return null;
      return { sop_md: manual.sopMd };
    }
    case "task_manual_learnings": {
      const manual = taskManuals.find((m) => m.typeKey === key);
      if (!manual || manual.learnings === "") return null;
      return { learnings: manual.learnings };
    }
    // T-e271. Same "empty is nothing worth retaining" rule as the two split
    // manual series above, and for the same reason — most tasks are created
    // with no description at all, so the first correction would otherwise spend
    // one of the three kept slots recording emptiness. Server twin:
    // taskDescriptionHistorySnapshot answers "{}" for "".
    case "task_description": {
      const task = tasks.find((t) => t.id === key);
      if (!task || task.description === "") return null;
      return { description: task.description };
    }
    // T-2ebe. NO empty-string branch, and its absence is load-bearing rather
    // than a copy-paste slip: a task cannot have a blank title (create_task
    // refuses one and so does this route), so "" would mean the row vanished —
    // which the not-found arm above reports, not a snapshot claiming there was
    // no document here. Server twin: taskTitleHistorySnapshot.
    case "task_title": {
      const task = tasks.find((t) => t.id === key);
      if (!task) return null;
      return { title: task.title };
    }
    // T-791e. Same overlay shape as global_context: a tombstoned row stores the
    // ZERO VALUE, never the seed text — restoring it must put the block back
    // ON the factory version rather than write the factory text in as an owner
    // edit (they read identically today and diverge the moment the seed file
    // changes under a restore).
    case "system_interaction":
    case "boot_sequence":
    case "offboard":
    // T-3201: the six lifecycle documents are the SAME overlay shape. The two
    // read-only ones never reach here in practice (nothing writes them, so no
    // version is ever recorded), and they are listed rather than special-cased
    // because this switch is total over DocumentKind and an arm that refuses
    // them would be a second answer to "may this be edited" living where the
    // wire's own `read_only` already answers.
    case "accelerated_stop":
    case "task_closeout":
    case "task_reassign_predecessor":
    case "task_takeover_with_predecessor":
    case "task_takeover_fresh":
    case "task_unblocked": {
      if (bootDocSeed(kind, key) === null) return null;
      const overlay = bootDocOverlays.get(`${kind}/${key}`);
      return {
        text: overlay ?? "",
        tombstoned: String(overlay === undefined),
      };
    }
  }
}

/** Retain the state a write is about to replace. Called BEFORE the mutation,
 * exactly like SaveWithDocumentHistory's in-transaction snapshot. */
function recordDocumentHistory(kind: DocumentKind, key: string): void {
  // This write persists the document's row; whether it retains anything depends
  // on a row having been there ALREADY (see documentRows).
  const replacesARow = hasDocumentRow(kind, key);
  markDocumentRow(kind, key);
  if (!replacesARow) return;
  const content = snapshotDocument(kind, key);
  if (content === null) return;
  const slot = historySlot(kind, key);
  const kept = documentHistories.get(slot) ?? [];
  kept.unshift({
    id: nextDocumentHistoryId++,
    content,
    created_ts: Date.now() / 1000,
    actor_id: MOCK_OWNER_ID,
  });
  documentHistories.set(slot, kept.slice(0, historyCapFor(kind)));
}

/** Write a retained revision back over the live document + fan the doc's own
 * SSE topic, so every surface reading it reconciles by refetch. */
function applyDocumentHistory(
  kind: DocumentKind,
  key: string,
  content: Record<string, string>,
): void {
  const tombstoned = content.tombstoned === "true";
  switch (kind) {
    case "global_context":
      globalContextOverlay = tombstoned
        ? null
        : {
            text: content.text ?? "",
            owner_id: MOCK_OWNER_ID,
            schema_version: 3,
            is_default: false,
            org_name: "",
          };
      emitTopic("global_context");
      return;
    case "role_definition": {
      const isSeed = MOCK_WIRE_ROLES_SEED.some((r) => r.key === key);
      if (tombstoned && isSeed) {
        roleOverlays.delete(key);
      } else {
        const current = foldRole(key);
        roleOverlays.set(key, {
          ...current,
          name: content.name ?? current.name,
          definition_md: content.definition_md ?? current.definition_md,
          is_default: false,
        });
      }
      emitTopic("role_def");
      return;
    }
    case "lessons": {
      // The key IS the role_key since T-2 — nothing to split.
      if (tombstoned) {
        lessonsOverlays.delete(key);
      } else {
        lessonsOverlays.set(key, {
          ...docSizeFields(content.text ?? "", "learning"),
          role_key: key,
          text: content.text ?? "",
          owner_id: MOCK_OWNER_ID,
          schema_version: 2,
          is_default: false,
        });
      }
      emitTopic("lessons");
      return;
    }
    case "insight": {
      // The key IS the role_key — nothing to split out of it.
      if (tombstoned) {
        insightOverlays.delete(key);
      } else {
        insightOverlays.set(key, {
          ...docSizeFields(content.text ?? "", "insight"),
          role_key: key,
          text: content.text ?? "",
          has_seed: key in INSIGHT_SEEDS,
          owner_id: MOCK_OWNER_ID,
          schema_version: 3,
          is_default: false,
        });
      }
      // 🔴 The topic the server's publishDocumentHistoryRestore fans for this
      // kind. Getting it wrong here is silent in exactly the way it is silent
      // there: the restore still lands, and every other open surface just never
      // hears about it.
      emitTopic("insight");
      return;
    }
    case "task_manual": {
      const manual = taskManuals.find((m) => m.typeKey === key);
      if (!manual) return;
      manual.purpose = content.purpose ?? manual.purpose;
      manual.sopMd = content.sop_md ?? manual.sopMd;
      manual.learnings = content.learnings ?? manual.learnings;
      if (content.fields !== undefined) {
        // The retained value is the serialised field list; a value this mock
        // cannot parse leaves the live fields alone rather than wiping them.
        try {
          manual.fields = JSON.parse(content.fields);
        } catch {
          /* keep the live fields */
        }
      }
      manual.updatedTs = Date.now() / 1000;
      emitTopic("task_manual");
      return;
    }
    // restoreTaskManualField (T-1f39): exactly the one field this series
    // versions goes back, and every other field of the manual is left as it
    // stands — restoring a SOP must not resurrect the 用途 it was written under.
    case "task_manual_sop":
    case "task_manual_learnings": {
      const manual = taskManuals.find((m) => m.typeKey === key);
      if (!manual) return;
      if (kind === "task_manual_sop") manual.sopMd = content.sop_md ?? "";
      else manual.learnings = content.learnings ?? "";
      manual.updatedTs = Date.now() / 1000;
      emitTopic("task_manual");
      return;
    }
    // T-e271: the restore writes back the ONE field this series versions and
    // touches nothing else on the task — restoring a description must not drag
    // back the status or priority the task had when it was written.
    case "task_description": {
      const task = tasks.find((t) => t.id === key);
      if (!task) return;
      task.description = content.description ?? "";
      task.updatedTs = Date.now() / 1000;
      emitTopic("task");
      return;
    }
    // T-2ebe: the title's own single-column write. Separate series over the
    // same key, so restoring a title leaves the description exactly as it is.
    case "task_title": {
      const task = tasks.find((t) => t.id === key);
      if (!task) return;
      task.title = content.title ?? "";
      task.updatedTs = Date.now() / 1000;
      emitTopic("task");
      return;
    }
    // T-791e. The tombstoned arm drops the overlay so the block goes back to
    // following its factory seed — writing the seed text in as an owner edit
    // would leave `is_default` false and the 預設 badge off for a document that
    // IS the default.
    case "system_interaction":
    case "boot_sequence":
    case "offboard": {
      if (bootDocSeed(kind, key) === null) return;
      if (tombstoned) bootDocOverlays.delete(`${kind}/${key}`);
      else bootDocOverlays.set(`${kind}/${key}`, content.text ?? "");
      emitTopic(BOOT_DOC_TOPIC);
      return;
    }
  }
}

/** Map a wire member → view Member, folding in the M1-only view extras. */
function mapWithExtras(w: WireMember): Member {
  return toMember(w);
}

/** 加速停止 stamps the SAME two-armed clock on a member and on an outsource
 * worker, so the two mocks must not drift apart either — server-side both DTOs
 * now read the ONE `winddownDeadlineOf` (T-14 item 3), and before that fix the
 * worker mock's silence about `refocus_deadline` was accidentally right because
 * the server also answered 0. It is this change that made it a divergence, on
 * exactly the field the change is about, in the file a cockpit developer reads
 * first — so the arithmetic lives in one place here rather than being spelled
 * twice.
 *
 * `since === null` means "leave the existing stamp alone": the 下線 arm is
 * anchored on the stop that is ALREADY open (`desired_state=offline`), so it
 * only adds the ceiling. The 換手 arm opens the window here and stamps both.
 * The grace mirrors the server's shipped default (StoppingTimeoutSecs), the
 * same literal both arms used before this helper existed. */
function acceleratedStopStamps(windingDownToOffline: boolean): {
  since: number | null;
  deadline: number;
} {
  const graceSecs = 120;
  const now = Date.now() / 1000;
  return windingDownToOffline
    ? { since: null, deadline: now + graceSecs }
    : { since: now, deadline: now + graceSecs };
}

function findWire(id: string): WireMember {
  const w = wireMembers.find((m) => m.id === id);
  if (!w) throw new Error(`mock: member not found: ${id}`);
  return w;
}

/** Resume-summary target parity with the server (T-4595): the ONE member verb
 * the owner released to workers resolves member ∪ outsource — the server's
 * `resolveResumeSummaryTarget` is deliberately `resolveMember` WITHOUT the
 * kind='outsource' fold, so an `ow-` id is a 200 here, not a 404.
 *
 * Same shape as `findScheduleRecipient` (the other verb with this resolver) on
 * purpose: two resolvers for one rule already invites drift; a third spelling
 * would guarantee it.
 *
 * 🔴 A NARROWER RESOLVER IS NOT THE ONLY THING THIS PARITY NEEDS. Letting the
 * id through is worth nothing while the snapshot's own task filter still
 * excludes the worker's rows — the card would just trade "讀取喚醒快照失敗" for
 * an empty task list, which is a second wrong picture wearing the first one's
 * clothes. See the executor filter in getMemberResumeSummary. */
function findResumeSummaryTarget(memberId: string): void {
  if (wireMembers.some((m) => m.id === memberId)) return;
  if (outsourceWorkers.some((w) => w.id === memberId)) return;
  throw mockApiError(
    `http 404 for /api/members/${memberId}/resume-summary`,
    404,
    `member '${memberId}' not found`,
  );
}

/** Render an epoch second the way the SERVER renders `ts_display` /
 * `generated_at`: `YYYY-MM-DD HH:MM:SS ±HH:MM`, date ALWAYS written (a reader
 * of a hand-off must be able to tell 昨天 from 上週 without knowing what day
 * the snapshot was taken), and the zone offset carried IN the string because
 * the studio has no configured timezone setting.
 *
 * 🔴 This lives in the mock — the API seam — precisely so the COMPONENT never
 * grows one. The panel prints the string it is given; the only formatter in
 * the cockpit is the one that stands in for the server. */
function mockTsDisplay(ts: number): string {
  const d = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  const offMin = -d.getTimezoneOffset();
  const sign = offMin < 0 ? "-" : "+";
  const abs = Math.abs(offMin);
  return (
    `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
    `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())} ` +
    `${sign}${p(Math.floor(abs / 60))}:${p(abs % 60)}`
  );
}

/** Resolve a chat participant id to its DISPLAY name the way the server's
 * `from_name` / `to_name` do: through the roster, ANY status (a dismissed
 * member still reads by name). Returns "" when the id resolves to nothing —
 * the HONEST empty. It deliberately does NOT fall back to the id: a reader
 * could then not tell "no name on file" from "the name really is that id". */
function resumeDisplayNameOf(id: string): string {
  const m = wireMembers.find((w) => w.id === id);
  if (m) return m.name;
  const w = outsourceWorkers.find((o) => o.id === id);
  if (w) return w.codename;
  return "";
}

// ── mock owner credential + settings state (B3) ─────────────────────────────
// The mock boots "installed": password set (AuthGate's mock mode never shows
// the first-run page anyway), default settings. Same validation rules as the
// server so the UI's error paths are exercisable offline.
let mockPasswordSet = true;
let mockPassword = "mock-password";
// The mock's TOTP state, mirroring the server's three settings keys. It boots
// with NO factor armed, which is what a fresh install looks like.
//
// The mock cannot compute real TOTP codes (no crypto here, and a test that had
// to generate one would be testing HMAC, not the UI). It accepts one fixed
// code instead — the same substitution the mock already makes for the claim
// token. What the UI needs to be exercisable offline is the STATE MACHINE
// (pending → armed → off) and the refusals, and those are faithful.
// The ship-dark feature flag. Boots OFF, exactly like a real install that has
// never opted in — so the mock cockpit shows what an untouched studio shows.
let mockMfaOffered = false;
let mockMfaActive = false;
let mockMfaPending = false;
const MOCK_TOTP_CODE = "123456";
const MOCK_TOTP_SECRET = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
const DEFAULT_MOCK_SETTINGS = {
  owner_token_ttl: 86400,
  agent_token_ttl: 604800,
  handover_pct: 50,
  notice_pct: 40,
  codex_compaction_threshold: 3,
  codex_notice_round: 2,
  monitoring_refresh_seconds: 5,
  // 加速停止 grace — mirrors the server's shipped default (StoppingTimeoutSecs).
  accelerated_grace_secs: 120,
  // M3 global outsource cap — mirrors the server's code-side default (3).
  outsource_max_parallel: 3,
  // T-ae38 document size caps — mirror the server's shipped defaults, which
  // this module already imports rather than restating (Duty has its own,
  // smaller one; the other three share).
  doc_cap_chars_duty: DOC_CAP_CHARS_DEFAULTS.duty,
  doc_cap_chars_insight: DOC_CAP_CHARS_DEFAULTS.insight,
  doc_cap_chars_learning: DOC_CAP_CHARS_DEFAULTS.learning,
  doc_cap_chars_manual_sop: DOC_CAP_CHARS_DEFAULTS.manualSop,
  doc_cap_chars_manual_learnings: DOC_CAP_CHARS_DEFAULTS.manualLearnings,
  // T-791e added the two boot-context caps to the SAME settings surface, so the
  // mock has to serve them or it is answering a settings DTO the server does
  // not send. They mirror the same shipped defaults foldBootDoc reports as
  // `cap_chars` — one number per kind, and the boot-sequence one is ONE cap
  // across both runtimes. `capForKind` routes to them, so the version list's
  // un-restorable marking is judged against these.
  doc_cap_chars_system_interaction:
    BOOT_DOC_CAP_CHARS_DEFAULTS.system_interaction,
  doc_cap_chars_boot_sequence: BOOT_DOC_CAP_CHARS_DEFAULTS.boot_sequence,
  doc_cap_chars_offboard: BOOT_DOC_CAP_CHARS_DEFAULTS.offboard,
  // T-c9b4 wake-snapshot chat budget. Served here for the same reason as the
  // caps above — a settings DTO missing a field the server always sends is a
  // mock the page can go green against while the real one breaks.
  chat_budget_chars: CHAT_BUDGET_CHARS_DEFAULT,
  // T-8 backup retention N, served for the same reason as everything above: a
  // settings DTO missing a field the server always sends is a mock the page can
  // go green against while the real one breaks.
  backup_retain: BACKUP_RETAIN_DEFAULT,
  // The two software-update toggles — both OFF out of the box, mirroring the
  // server (updates come from GitHub Releases; there is no updater server to
  // configure any more).
  updater_receive_beta: false,
  updater_auto_update: false,
  // Studio name (T-d693) — "" out of the box, mirroring the server (the topbar
  // shows the localized default until the owner names the studio).
  org_name: "",
  // Owner nickname (T-0b41) — "" out of the box, mirroring the server (the
  // profile pill shows the localized default until the owner sets a nickname).
  owner_name: "",
  push_contact_email: "",
  // Cockpit display prefs (T-0b41-p2) — "" out of the box, mirroring the server
  // (the frontend keeps its localStorage cache / default until the owner picks).
  display_theme: "",
  display_language: "",
  // Layout width (T-756f) — OFF out of the box, mirroring the server (the
  // cockpit ships with the narrow centred column).
  display_wide: false,
  // T-33: the lore feature ships OFF, and the mock ships the same default so a
  // cockpit developed against it sees what a fresh station shows.
  lore_enabled: false,
  // The first-run onboarding report (T-ba62). Null in the mock and staying
  // that way: mock mode is a healthy studio, and a seeded FAILED report would
  // hang the "your studio is broken" banner over every mock page. Declared
  // rather than omitted so `onboarding_dismissed` below has something real to
  // write to when a caller does seed one.
  onboarding: null as WireServerSettings["onboarding"],
};

// ── Themes (T-83ef): their OWN store, keyed by id ────────────────────────────
//
// A Map, not an array: the endpoints are all per-id, and Map keeps INSERTION
// order — which is what makes "a replace keeps the theme's position in the
// list, a create appends" fall out for free instead of being re-implemented
// (and re-broken) here. `order_idx` on the write receipt is the key's index in
// that order. Themes no longer ride on settings at all: `custom_themes` is
// gone from SettingsDTO, so the mock must not keep a second copy of them
// there either — one store, or the two would disagree.
const mockThemes = new Map<string, ThemeBundle>();

/** The saved theme ids, for the `display_theme` check ("" | office | a saved
 * id). Reads the theme STORE, never a settings field — after T-83ef there is
 * no settings field left to read. */
function mockThemeIds(): Set<string> {
  return new Set(mockThemes.keys());
}

/** Mirror of the server's per-document size/cap reporting (T-3aeb). Runes, not
 * UTF-16 units — same reason docCap.ts spells it [...s].length.
 *
 * T-ae38, widened by T-30f1: the cap is per SEGMENT, so the caller names which
 * of the five it is judged by. Passing the wrong one here would make the mock
 * disagree with the server about a doc's remaining budget — the one thing this
 * helper exists to keep honest. */
function docSizeFields(
  text: string,
  cap: "duty" | "insight" | "learning" | "manualSop" | "manualLearnings",
) {
  return {
    size_chars: [...text].length,
    cap_chars: {
      duty: mockServerSettings.doc_cap_chars_duty,
      insight: mockServerSettings.doc_cap_chars_insight,
      learning: mockServerSettings.doc_cap_chars_learning,
      manualSop: mockServerSettings.doc_cap_chars_manual_sop,
      manualLearnings: mockServerSettings.doc_cap_chars_manual_learnings,
    }[cap],
  };
}
let mockServerSettings = { ...DEFAULT_MOCK_SETTINGS };
const MOCK_CLAIM_TOKEN = "mock-claim-token";
const TOKEN_TTL_CHOICES = new Set([43200, 86400, 604800, 2592000]);

// M4 回呼端點 — an in-memory store keyed by member id. Seeded with one endpoint
// on mira (the mockup's `pr-event`) so the panel renders a populated section.
const mockWebhooks = new Map<string, WebhookEndpoint[]>([
  [
    "mira",
    [
      {
        endpointId: "pr-event",
        purpose: "回報 PR 結果",
        status: "enabled",
        createdTs: Date.now() / 1000 - 3600,
        token: "mock-webhook-token-pr-event-000000000000",
        platform: "generic",
        hasSigningSecret: false,
        // Simulated observability counters so the panel's per-row stats line
        // renders a populated state in mock mode (server parity: /in counts).
        lastReceivedTs: Date.now() / 1000 - 300,
        deliveredCount: 12,
        droppedCount: 2,
        lastDropReason: "sig_failed",
      },
    ],
  ],
]);

/** Write-only signing-secret vault, keyed `${memberId}\u0000${endpointId}`. The
 * plaintext lives HERE only — it is NEVER placed on a returned WebhookEndpoint
 * (mirrors the server, which exposes only `has_signing_secret`).
 *
 * The separator is written as the ESCAPE `\u0000`, never as a literal NUL
 * byte in this file. Identical at runtime, but a literal NUL makes grep treat
 * the whole 118 KB file as binary and report ZERO matches with exit 1 — no
 * "Binary file matches" line, no warning. That silent false negative has
 * already cost two people a search each. Keep it escaped. */
const mockWebhookSecrets = new Map<string, string>();
function secretKey(memberId: string, endpointId: string): string {
  return `${memberId}\u0000${endpointId}`;
}

/** Simulated /in debug ring buffer, keyed `"<memberId> <endpointId>"` (server
 * parity: last 5 raw requests, newest first). Only the seeded endpoint carries
 * traffic; fresh endpoints honestly read empty. */
const mockWebhookRequests = new Map<string, WebhookRequestLog[]>([
  [
    "mira pr-event",
    [
      {
        ts: Date.now() / 1000 - 300,
        outcome: "delivered",
        headers: JSON.stringify({
          "Content-Type": ["application/json"],
          "User-Agent": ["GitHub-Hookshot/8d2e6a1"],
          "X-Github-Event": ["pull_request"],
        }),
        body: '{"action":"closed","number":42,"pull_request":{"merged":true,"title":"Fix login redirect"}}',
        truncated: false,
      },
      {
        ts: Date.now() / 1000 - 2100,
        outcome: "dropped:sig_failed",
        headers: JSON.stringify({
          "Content-Type": ["application/json"],
          "User-Agent": ["curl/8.6.0"],
        }),
        body: '{"probe":true}',
        truncated: false,
      },
      {
        ts: Date.now() / 1000 - 5400,
        outcome: "delivered",
        headers: JSON.stringify({
          "Content-Type": ["application/json"],
          "User-Agent": ["GitHub-Hookshot/8d2e6a1"],
          "X-Github-Event": ["pull_request"],
        }),
        body:
          '{"action":"opened","number":42,"pull_request":{"title":"Fix login redirect","body":"' +
          "Long description ".repeat(40) +
          '"}}',
        truncated: true,
      },
      {
        ts: Date.now() / 1000 - 9000,
        outcome: "dropped:disabled",
        headers: JSON.stringify({
          "Content-Type": ["application/json"],
          "User-Agent": ["GitHub-Hookshot/8d2e6a1"],
        }),
        body: '{"action":"synchronize","number":41}',
        truncated: false,
      },
      {
        ts: Date.now() / 1000 - 12600,
        outcome: "ping",
        headers: JSON.stringify({
          "Content-Type": ["application/json"],
          "User-Agent": ["GitHub-Hookshot/8d2e6a1"],
          "X-Github-Event": ["ping"],
        }),
        body: '{"zen":"Keep it logically awesome.","hook_id":512001}',
        truncated: false,
      },
    ],
  ],
]);

function mockWebhookToken(): string {
  return (
    "mock-" +
    Array.from({ length: 32 }, () =>
      Math.floor(Math.random() * 36).toString(36),
    ).join("")
  );
}

// T-f059 定期訊息 — an in-memory store keyed by member id, the webhook store's
// clock-driven twin. Seeded with one schedule on mira so the panel's section
// renders populated offline.
const mockScheduledMessages = new Map<string, ScheduledMessage[]>([
  [
    "mira",
    [
      {
        id: "sch-0a1b2c3d4e5f",
        memberId: "mira",
        label: "每日巡檢",
        body: "早安,請看一下昨天的 CI 有沒有紅的,有的話開一張票。",
        cadence: "daily",
        dayOfWeek: 0,
        dayOfMonth: 1,
        hour: 9,
        minute: 0,
        // Empty for every non-custom cadence (server parity, T-49e7).
        customMonths: [],
        customDays: [],
        customHours: [],
        customMinutes: [],
        timezone: "Asia/Taipei",
        status: "enabled",
        lastFiredSlot: "2026-08-10T09:00+08:00",
        lastFiredTs: Date.now() / 1000 - 7200,
        createdTs: Date.now() / 1000 - 86400,
      },
    ],
  ],
]);

function mockScheduleId(): string {
  return (
    "sch-" +
    Array.from({ length: 12 }, () =>
      Math.floor(Math.random() * 16).toString(16),
    ).join("")
  );
}

/** The delivery cursor a freshly created mock schedule carries.
 *
 * ⚠️ The mock runs NO tick loop, so this value is never compared against
 * anything and cannot decide a delivery — it exists only so the row's shape is
 * populated the way the server's is (the DTO says the cursor is never empty for
 * a live schedule). It is deliberately NOT the server's "most recently elapsed
 * slot" computation: reproducing that here would imply a fidelity the mock does
 * not have. */
function mockScheduleSlot(s: {
  hour?: number;
  minute?: number;
  timezone: string;
}): string {
  const hh = String(s.hour ?? 0).padStart(2, "0");
  const mm = String(s.minute ?? 0).padStart(2, "0");
  let day = new Date().toISOString().slice(0, 10);
  try {
    // sv-SE renders as YYYY-MM-DD, so the zone's own calendar day comes out
    // without any hand-rolled formatting.
    day = new Intl.DateTimeFormat("sv-SE", { timeZone: s.timezone }).format(
      new Date(),
    );
  } catch {
    // unknown zone — the create path below already rejects it; fall back to the
    // UTC day rather than inventing an offset
  }
  return `${day}T${hh}:${mm}`;
}

/** Server parity for all four `custom` sets: duplicates collapse and the set
 * is stored sorted, so two orderings of the same choice compare equal — which
 * is what stops a caller that sends the whole form back on every save from
 * re-aiming the delivery cursor. */
function sortedSet(values: number[]): number[] {
  return [...new Set(values)].sort((a, b) => a - b);
}

/** The whole year, listed — mock twin of the server's `allCustomMonths`. It is
 * a LISTED set and never a nil "means everything" sentinel, for the same reason
 * the server refuses to have one: every other set reads emptiness as "the
 * caller said nothing". */
function allMockMonths(): number[] {
  return Array.from({ length: 12 }, (_, i) => i + 1);
}

/** Mock twin of the server's `resolveCustomMonths` — the ONE place in this file
 * where an ABSENT field carries a meaning, and the three questions are asked in
 * the same order as there:
 *
 *   sent given        honour it VERBATIM, `[]` included — validation refuses
 *                     that, and substituting all twelve for it would erase the
 *                     difference the 422 exists to state.
 *   not custom        no months are read; keep whatever is stored (nothing on a
 *                     create, the parked set on a PATCH that left `custom`).
 *   stored non-empty  an existing choice the caller did not touch.
 *
 * Only when all three fall through does the all-twelve default apply: a
 * `custom` create, or a PATCH that switches a never-custom row to `custom`.
 * That is what keeps a client written before round 2 working — it never sends
 * the field, and its schedules always did mean every month. */
function resolveMockMonths(
  cadence: string,
  stored: number[],
  sent: number[] | undefined,
): number[] {
  if (sent !== undefined) return sent;
  if (cadence !== "custom") return stored;
  if (stored.length > 0) return stored;
  return allMockMonths();
}

/** Recipient parity with the server (T-f059): a schedule may bind to an
 * assistant OR an `ow-` outsource worker — the recipient rule ordinary chat
 * uses, NOT `resolveMember`, which excludes outsource. */
function findScheduleRecipient(memberId: string): void {
  if (wireMembers.some((m) => m.id === memberId)) return;
  if (outsourceWorkers.some((w) => w.id === memberId)) return;
  throw mockApiError(
    `http 404 for /api/members/${memberId}/scheduled-messages`,
    404,
    `member '${memberId}' not found`,
  );
}

/** Server 422 parity for the create/patch slot fields. */
function validateSchedulePart(
  memberId: string,
  part: {
    body?: string;
    cadence?: string;
    dayOfWeek?: number;
    dayOfMonth?: number;
    hour?: number;
    minute?: number;
    customMonths?: number[];
    customDays?: number[];
    customHours?: number[];
    customMinutes?: number[];
    timezone?: string;
  },
  // The cadence the row ends up on. On create that is `part.cadence`; on patch
  // the stored one unless this request changes it. It matters because the
  // empty-set rule is cadence-scoped on the server — see below.
  cadenceInEffect?: string,
): void {
  const bad = (detail: string) => {
    throw mockApiError(
      `http 422 for /api/members/${memberId}/scheduled-messages`,
      422,
      detail,
    );
  };
  if (part.body !== undefined && part.body.trim() === "")
    bad("body must not be blank");
  if (
    part.cadence !== undefined &&
    !["daily", "weekly", "monthly", "custom"].includes(part.cadence)
  )
    bad("cadence must be one of daily / weekly / monthly / custom");
  // All four `custom` sets are EXPLICIT: an empty set is a 422 rather than a
  // silent "all" or a silent "never", because a schedule that always fires and
  // one that never fires must not be one keystroke apart.
  //
  // 🔴 That refusal is CADENCE-SCOPED, and the mock must scope it the same way
  // or it is stricter than the server it stands in for. The server folds an
  // empty array to nil on the way in (intSliceOrNil) and only judges the sets
  // when the cadence is `custom`, so `{"cadence":"daily","custom_days":[]}` is
  // accepted there. A mock that refuses it teaches the caller a rule the wire
  // does not have.
  const cadence = cadenceInEffect ?? part.cadence;
  const set = (
    name: string,
    values: number[] | undefined,
    lo: number,
    hi: number,
    hint = "",
  ) => {
    if (values === undefined) return;
    if (values.length === 0 && cadence === "custom")
      bad(`${name} must not be empty${hint}`);
    for (const v of values) {
      if (!Number.isInteger(v) || v < lo || v > hi)
        bad(`${name} must be ${lo}-${hi}`);
    }
  };
  // 🔴 By the time a set reaches here the absent-months rule has already been
  // applied (resolveMockMonths), so an empty months array can only mean the
  // caller really sent `[]` — which is the 422 the server states, hint and all.
  set(
    "custom_months",
    part.customMonths,
    1,
    12,
    " (to mean every month, OMIT the field entirely rather than sending [])",
  );
  set("custom_days", part.customDays, 1, 31);
  set("custom_hours", part.customHours, 0, 23);
  set("custom_minutes", part.customMinutes, 0, 59);
  if (part.hour !== undefined && (part.hour < 0 || part.hour > 23))
    bad("hour must be 0-23");
  if (part.minute !== undefined && (part.minute < 0 || part.minute > 59))
    bad("minute must be 0-59");
  if (
    part.dayOfWeek !== undefined &&
    (part.dayOfWeek < 0 || part.dayOfWeek > 6)
  )
    bad("day_of_week must be 0-6");
  if (
    part.dayOfMonth !== undefined &&
    (part.dayOfMonth < 1 || part.dayOfMonth > 31)
  )
    bad("day_of_month must be 1-31");
  if (part.timezone !== undefined) {
    if (part.timezone.trim() === "") bad("timezone must not be blank");
    try {
      new Intl.DateTimeFormat("en-US", { timeZone: part.timezone });
    } catch {
      bad(`unknown timezone '${part.timezone}'`);
    }
  }
}

/** Server parity for the round-2 half of "a schedule must be able to fire"
 * (T-49e7): four non-empty, in-range sets can still name a date no calendar
 * ever has. months {2} × days {31} passes every other check, renders as a
 * perfectly ordinary card, and delivers nothing for the rest of time — the same
 * failure the empty-set 422 exists to refuse, reached through the month set.
 *
 * 🔴 February counts as 29, so months {2} × days {29} — a deliberate leap-year
 * schedule — stays legal. The refusal fires only when NO (month, day) pair is
 * possible in any year at all. Mirrors `scheduledMessageMonthDayFeasible`. */
function requireAPossibleDate(
  memberId: string,
  months: number[],
  days: number[],
): void {
  if (months.length === 0 || days.length === 0) return;
  const longest = Math.max(
    ...months.map((m) => (m === 2 ? 29 : [4, 6, 9, 11].includes(m) ? 30 : 31)),
  );
  const earliest = Math.min(...days);
  if (earliest <= longest) return;
  throw mockApiError(
    `http 422 for /api/members/${memberId}/scheduled-messages`,
    422,
    `custom_months [${months.join(", ")}] and custom_days [${days.join(", ")}] never occur ` +
      `together, so this schedule could never fire: the longest of the chosen months has ` +
      `${longest} days, and the earliest day chosen is the ${earliest}. Pick a day one of ` +
      `these months actually has, or add a month that has this day. (February counts as 29 ` +
      `days, so February with the 29th is allowed and fires in leap years only.)`,
  );
}

/** The CONDITIONAL half of the create/patch 422 (T-49e7): which fields a
 * cadence cannot do without. `daily`/`weekly`/`monthly` fire at the single
 * reading `hour`/`minute` names, so omitting either is a 422 and never a silent
 * midnight; `custom` fires where the four sets intersect, so it needs
 * `custom_days`/`custom_hours`/`custom_minutes` (from the request, or already
 * stored on the row being patched).
 *
 * 🔴 Months are NOT among them, and this function does not check them at all.
 * They arrive already resolved (resolveMockMonths), so an omitted
 * `custom_months` is the whole year and an explicit `[]` has already been
 * refused by validateSchedulePart — a months branch here could not be reached
 * from either caller. A guard with no discriminating power reads like a second
 * layer of protection that is not there. */
function requireCadenceFields(
  memberId: string,
  cadence: ScheduleCadence,
  have: {
    hour?: number;
    minute?: number;
    customDays?: number[];
    customHours?: number[];
    customMinutes?: number[];
  },
): void {
  const bad = (detail: string) => {
    throw mockApiError(
      `http 422 for /api/members/${memberId}/scheduled-messages`,
      422,
      detail,
    );
  };
  if (cadence === "custom") {
    if (!have.customDays?.length)
      bad("custom_days is required for cadence custom");
    if (!have.customHours?.length)
      bad("custom_hours is required for cadence custom");
    if (!have.customMinutes?.length)
      bad("custom_minutes is required for cadence custom");
    return;
  }
  if (have.hour === undefined) bad(`hour is required for cadence ${cadence}`);
  if (have.minute === undefined)
    bad(`minute is required for cadence ${cadence}`);
}

// T-7fa1 staged *_pending responses. The mock has no wardens, so it can never
// PRODUCE a real undelivered dispatch — but the UI branch that consumes one has
// to be reachable, both from vitest and from a dev-server screenshot run. These
// two flags are the honest seam: OFF by default (the mock's normal, landing
// behaviour), flipped only by an explicit test/dev hook.
let activationPendingNext = false;
let relocationPendingNext = false;
// Mock parity for the wind-down half of the relocate verdict (T-927a). The mock
// has no live agent to wind down, so this is staged rather than derived — same
// shape as relocationPendingNext, and equally sticky.
let relocationDeferredNext = false;

// ── T-33 傳承 (lore) fixtures ──────────────────────────────────────────────
//
// These five are the entries actually written into the trial station on
// 2026-09-01 (exported as task artifact `ta-e37e44623f9e`), text unchanged.
// They are here rather than invented because the whole tab is about telling a
// real entry from a plausible one — a mock full of lorem ipsum would make the
// screens look right while proving nothing about what they render.
//
// 🔴 `actions` is EMPTY on every one of them, and that is the export, not a
// shortcut: the write path those five went through never carried an action
// name. So the mock cannot demonstrate action-axis tiering, and no screen may
// pretend it can.
//
// 🔴 `events` (第 5 格) IS EMPTY ON EVERY ONE OF THEM FOR THE SAME REASON, and
// this one is worth stating out loud because it is tempting to fix: these five
// were written on 2026-09-01, against 六格, through a write path that had no
// events at all. Inventing a plausible 時／事／人／地／物 for them would make the
// events surface look exercised while proving nothing — and a fabricated event
// reads exactly like a recorded one, which is the single failure this whole
// ticket exists to prevent. The empty case still renders (the section prints
// itself and says it is empty), so the surface is visible; what is not
// demonstrable here is a POPULATED event list, and no screen may pretend it is.
//
// 🔴 The 六格 cells that 五格 dropped (`label` / `falsify` / `residual_risk`) are
// GONE from this fixture rather than parked in an unused field: `symptoms` →
// `trigger`, `short` → `content`, `instance` → `impact`（v8 之前這一格叫
// `problem`）, and `retire_when` is
// a cell nobody had when these were written, so it is blank. Keeping the dropped
// text around in a field no route serves would put words on a screen the station
// cannot produce.
interface MockLoreEntry {
  entryId: string;
  /** 第 1 格, and the entry's title — no length cap. */
  trigger: string;
  /** 第 2 格 — the only cell that enters a boot context. */
  content: string;
  /** 第 3 格 — free text. Blank on all five: the cell did not exist yet. */
  retireWhen: string;
  /** 第 4 格. */
  impact: string;
  /** 第 5 格 — see the note above on why every one of these is empty. */
  events: LoreEventView[];
  subjects: string[];
  actions: string[];
  origin: string;
  status: string;
  supersedes: string;
  writtenBy: string;
}

const MOCK_LORE_ENTRIES: MockLoreEntry[] = [
  {
    entryId: "lore-31274bbb892c",
    trigger: "整套測試回 PASS／ok，而我正要拿這個結果去說「這一包沒問題」。",
    content:
      "綠燈只證明「它看得到的那些東西沒問題」。go test -run 配一個匹配不到任何東西的正則，輸出跟真的全部通過逐字相同（唯一訊號 no tests to run 常被 grep 濾掉）。⇒ 跑完之後要問的是「這一次的量法，看得到的範圍是什麼」——而且要在跑過之後問，不是在寫的時候。",
    retireWhen: "",
    impact:
      '2026-09-01：-run 的正則打錯字，26 顆 mutant 一顆都沒跑，回報 PASS。分母改成可驗的做法是逐一 grep -c "^func <name>("。',
    events: [],
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:O-197",
    status: "active",
    supersedes: "",
    writtenBy: "agent:O-197",
  },
  {
    entryId: "lore-3a8f02e14c10",
    trigger:
      "我正要拿「它有自動備份／有守衛／有檢查」去對別人保證這一次是安全的。",
    content:
      "機制存在只證明那條路上有那段碼，不證明這一次走的是那條路。實例：升級前備份只掛在 serve 開機那條路（backupBeforeMigrations 全樹一個呼叫者），而 bin/migrate 走的是沒有它的那條 ⇒ 用 migrate 升級的人沒有退路，而畫面上跟有退路一模一樣。⇒ 保證要講「這一次」，不是「有機制」。",
    retireWhen: "",
    impact:
      "2026-09-01 分站換版：因此改成走 serve 開機而不是 migrate，並另外手拍一份驗過的備份。",
    events: [],
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:O-197",
    status: "active",
    supersedes: "",
    writtenBy: "agent:O-197",
  },
  {
    entryId: "lore-b97ced3313a6",
    trigger: "我剛驗完一台機器的狀態，正要把結果當成「現在就是這樣」回報出去。",
    content:
      "對一台會自己動的機器（有更新器、有排程、有 KeepAlive），驗證是瞬時的而狀態不是。⇒ 驗完要多問一句「有什麼東西會在我不看的時候改變它」，並把答案變成可觀察的（掛一個定期查、或關掉那個會動的東西）。",
    retireWhen: "",
    impact:
      "2026-09-01：我回報 trial 站跑 feab5437，90 秒後它自己 [upgrade] 換成 v0.5.281。成因是我複製的 DB 帶著 updater.auto_update=true。",
    events: [],
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:O-197",
    status: "active",
    supersedes: "",
    writtenBy: "agent:O-197",
  },
  {
    entryId: "lore-dab4e84475b4",
    trigger:
      "我把一個站的 DB 複製到另一個站，然後預期新站會照我在新站上做的設定跑。",
    content:
      "OffiCraft 的站台設定存在 DB 的 setting 表裡（updater.auto_update、receive_beta、JWT 簽章金鑰等），所以複製 DB 會一起搬過去。後果一：新站會照舊站的自動更新設定把你剛裝的 binary 換掉。後果二：舊站簽出來的 token 在新站也通。⇒ 複製 DB 之後、開機之前，先把那些跟「這台該怎麼行為」有關的設定改掉。",
    retireWhen: "",
    impact:
      "2026-09-01：分站換版後 90 秒自己升級（auto_update 跟著 DB 過去）；另外我主站的 agent token 打分站 /api/members 回 200，改一個字元回 401 ⇒ 簽章金鑰也跟著過去了。",
    events: [],
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:O-197",
    status: "active",
    supersedes: "",
    writtenBy: "agent:O-197",
  },
  {
    entryId: "lore-76fba702e52a",
    trigger: "我剛說完「我收回那句話」，覺得這件事已經處理完了。",
    content:
      "收回只對聽到的人生效，幾秒鐘；真正的工作是把那句話從每一個會被再讀一次的地方拔掉（記憶檔、步驟筆記、票面、已送出的卡、產物、PR 描述）。⇒ 真正會發生的失敗不是不願意更正，是只做了便宜的那一半，而做完那半的人主觀上覺得自己已經更正過了。",
    retireWhen: "",
    impact:
      "2026-09-01：Kyle 收回一句關於部署路徑的錯誤結論，而那句話已經被我寫進步驟筆記（下一代開機第一件要讀的東西）。掃描結果：步驟筆記命中 1、卡零、產物零、waiting_reason 零。",
    events: [],
    subjects: ["repo:officraft"],
    actions: [],
    origin: "agent:O-197",
    status: "active",
    supersedes: "",
    writtenBy: "agent:O-197",
  },
];

/** Render one entry's L0 原文 — the mock's copy of the station's
 * `loreRevisionBody` (server/ocserverd/dal_lore_write.go).
 *
 * 🔴 THIS IS A DELIBERATE SECOND COPY OF A RENDERER AND THE SHAPE HAS TO MATCH
 * THE FIRST ONE BYTE FOR BYTE. The tab's whole point is that 原文 is what an
 * agent falls back to when it stops believing 第 2 格; a mock that rendered its
 * own convenient shape would let the 原文 pane look right in design mode while
 * the real one looked nothing like it, and the difference is invisible on a
 * screenshot. The station's shape is: each named cell as `name:\n<value>\n\n`
 * in a FIXED order, then `events:` — printed even when there is not one event,
 * because 「no events」 and 「the events were lost in a rewrite」 must not hash to
 * the same bytes — then one tab-separated row per event and a closing newline.
 *
 * Events are sorted by (happened_ts, what, actor, place, object) rather than by
 * the order they were handed in, the same as the station: the same set of
 * events sent in a different order has to render identically, or `base_sha256`
 * would report 「stale」 over a difference nobody can see.
 *
 * ⚠️ `sha256` is NOT computed from this. The mock has no digest of a real
 * stored blob, so it serves an EMPTY digest rather than a plausible-looking hex
 * string nothing derived — the surface says 「這份回應沒有帶摘要」 and that is
 * the true statement here. */
function mockLoreOriginal(e: MockLoreEntry): string {
  let body = "";
  for (const [name, value] of [
    ["trigger", e.trigger],
    ["content", e.content],
    ["retire_when", e.retireWhen],
    ["impact", e.impact],
  ] as const) {
    body += `${name}:\n${value}\n\n`;
  }
  body += "events:\n";
  const sorted = [...e.events].sort(
    (a, b) =>
      a.happenedTs - b.happenedTs ||
      cmp(a.what, b.what) ||
      cmp(a.actor, b.actor) ||
      cmp(a.place, b.place) ||
      cmp(a.object, b.object),
  );
  for (const ev of sorted) {
    // 人／地／物 empty stays empty: two adjacent tabs IS the record that
    // nothing was known, and it must survive into the digested text.
    body += [
      String(ev.happenedTs),
      ev.what,
      ev.actor,
      ev.place,
      ev.object,
    ].join("\t");
    body += "\n";
  }
  body += "\n";
  return body;
}

function cmp(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** mock 的待審佇列。刻意把**每一種形狀**都擺一個,因為這一塊的守衛全都掛在
 * 「兩列長得不一樣」上面:
 *   ① 折疊後跟既有的完全一樣 ⇒ 建議合併
 *   ② 沒有任何相似對象、而且底下有記憶 ⇒ 建議核可
 *   ③ 只有模糊相似、但底下有記憶 ⇒ **建議是空字串**(算不出來就留白)
 *   ④ 底下 0 條、而且**從來沒有過** ＋ 只差一個字 ⇒ 建議合併(打錯字的形狀,
 *      而且併過去搬不動任何記憶,因為根本沒有記憶)
 *   ⑤ 底下 0 條、但**曾經有 2 條**(都退役了) ⇒ 那跟名字對不對無關
 * ④ 跟 ⑤ 在舊的線上長得一模一樣 —— 兩列都寫「底下還沒有記憶」,而處置完全相
 * 反。owner 2026-09-04 逐字:「為什麼核可的可見內容這麼少 我根本無從審核起」。 */
let mockPendingEntities: LorePendingEntityView[] = [
  {
    entityId: "en-mock-1",
    canonical: "repo:OffiCraft",
    type: "repo",
    name: "OffiCraft",
    createdTs: 1788330000,
    createdBy: "m-o197",
    entries: 2,
    entriesEver: 2,
    entryRefs: [
      {
        entryId: "le-mock-a1",
        trigger: "我要判斷一個綠燈能不能當證據",
        status: "active",
      },
      {
        entryId: "le-mock-a2",
        trigger: "我要在地端跑測試",
        status: "superseded",
      },
    ],
    similar: [
      {
        entityId: "en-mock-live",
        canonical: "repo:officraft",
        reason: "same_normalized",
      },
    ],
    sampleShort: "綠燈只證明「它看得到的那些東西沒問題」。",
  },
  {
    entityId: "en-mock-2",
    canonical: "tool:sqlite",
    type: "tool",
    name: "sqlite",
    createdTs: 1788330100,
    createdBy: "m-kyle",
    entries: 3,
    entriesEver: 4,
    entryRefs: [
      {
        entryId: "le-mock-b1",
        trigger: "我要複製一份資料庫來測",
        status: "active",
      },
      {
        entryId: "le-mock-b2",
        trigger: "我要改 schema",
        status: "active",
      },
      {
        entryId: "le-mock-b3",
        trigger: "我要開一個新的連線池",
        status: "underspecified",
      },
    ],
    similar: [],
    sampleShort: "複製資料庫等於複製設定 —— 開關存在資料裡,跟著資料走。",
  },
  {
    entityId: "en-mock-3",
    canonical: "tool:goose",
    type: "tool",
    name: "goose",
    createdTs: 1788330200,
    createdBy: "m-kyle",
    entries: 1,
    entriesEver: 1,
    entryRefs: [
      {
        entryId: "le-mock-c1",
        trigger: "我要加一個 migration",
        status: "active",
      },
    ],
    // 只有模糊相似,而且**底下真的有記憶** ⇒ 伺服器算不出明確結論 ⇒ 這一格就是
    // 空的。合併會把那條記憶搬到另一個名字底下,一個字的證據不夠付這個代價。
    similar: [
      {
        entityId: "en-mock-4",
        canonical: "tool:grep",
        reason: "edit_distance_2",
      },
    ],
    sampleShort: "",
  },
  {
    entityId: "en-mock-5",
    canonical: "repo:offcraft",
    type: "repo",
    name: "offcraft",
    createdTs: 1788330300,
    createdBy: "m-o197",
    // 鑄出來以後再也沒被用過 ＋ 只差一個字 ⇒ 打錯字的形狀。併過去搬不動任何記
    // 憶(沒有記憶可搬),買到的是一個別名,擋掉同一個錯字明天再被鑄一次。
    entries: 0,
    entriesEver: 0,
    entryRefs: [],
    similar: [
      {
        entityId: "en-mock-live",
        canonical: "repo:officraft",
        reason: "edit_distance_1",
      },
    ],
    sampleShort: "",
  },
  {
    entityId: "en-mock-6",
    canonical: "human:Mira",
    type: "human",
    name: "Mira",
    createdTs: 1788330400,
    createdBy: "",
    // 底下 0 條,但**曾經有 2 條**,都退役了。跟上面那一列在舊線上長得一模一
    // 樣,處置完全相反 —— 這個名字被真的用過,0 不是它的錯。
    entries: 0,
    entriesEver: 2,
    entryRefs: [],
    similar: [],
    sampleShort: "",
  },
];
// The id shape is PRODUCTION'S, not a short stand-in: the server mints
// "k-" + 16 hex (keyring.go newKeyID). A mock that models a narrower row than
// the real one is a mock that hides layout defects from every guard mounted on
// it — which is exactly what happened the first time this fixture was written
// with "k-mock0". `created_ts: 0` is likewise the real convention, not a
// placeholder: it is how an install that predates the ring reports a key whose
// creation time was never recorded, so the card's "unknown" branch is exercised
// by default.
const MOCK_WIRE_SIGNING_KEYS: WireSigningKeys["keys"] = [
  { key_id: "k-a1b2c3d4e5f60718", created_ts: 0, is_signing: true },
];
let mockSigningKeys: WireSigningKeys["keys"] = structuredClone(
  MOCK_WIRE_SIGNING_KEYS,
);

export const mockApi: Api = {
  async listMembers(_opts?: { light?: boolean }): Promise<Member[]> {
    // Mirror the backend roster: dismissed (status="removed") rows are excluded.
    // `unread_count` is COMPUTED live per member (the same watermark-inverse
    // rule as handle_list_members), overriding the fixture's static 0 — so the
    // mock and http adapters agree by construction.
    //
    // The `light` flag (T-cf91) is a SERVER-SIDE payload/CPU optimisation
    // (honest-empty presence/unread); the mock has no such cost, so it returns
    // the same full view either way — a light response is a SUBSET of these
    // fields and the only light consumer (請示卡頁) reads just name/role, so
    // the extra fields are harmless. The behavioural half of T-cf91 (a light
    // hook not refetching on chat) lives in the hook, not here.
    return wireMembers
      .filter((m) => m.roster_status !== "removed")
      .map((m) => mapWithExtras({ ...m, unread_count: unreadCountOf(m.id) }));
  },

  async getMember(id: string): Promise<Member> {
    // A removed member reads as 404 (mirror handle_get_member).
    const w = findWire(id);
    if (w.roster_status === "removed")
      throw new Error(`mock: member removed: ${id}`);
    // unread_count is COMPUTED here exactly as listMembers computes it — the Go
    // single-member handler runs the same `unreadCountsForRequest` as the list
    // (T-8115 review). Serving the static fixture value instead would make the
    // mock DISAGREE with the server, which is what let a badge regression ship.
    return mapWithExtras({ ...w, unread_count: unreadCountOf(id) });
  },

  async updateMemberAvatar(id: string, file: File): Promise<string> {
    const url = URL.createObjectURL(file);
    const worker = outsourceWorkers.find((item) => item.id === id);
    if (worker) {
      if (worker.avatarUrl?.startsWith("blob:")) {
        URL.revokeObjectURL(worker.avatarUrl);
      }
      worker.avatarUrl = url;
      emitTopic("outsource_worker");
      return url;
    }
    const member = findWire(id);
    if (member.avatar_url?.startsWith("blob:")) {
      URL.revokeObjectURL(member.avatar_url);
    }
    member.avatar_url = url;
    emitTopic("member");
    return url;
  },

  async removeMemberAvatar(id: string): Promise<void> {
    const worker = outsourceWorkers.find((item) => item.id === id);
    if (worker) {
      if (worker.avatarUrl?.startsWith("blob:")) {
        URL.revokeObjectURL(worker.avatarUrl);
      }
      worker.avatarUrl = "";
      emitTopic("outsource_worker");
      return;
    }
    const member = findWire(id);
    if (member.avatar_url?.startsWith("blob:")) {
      URL.revokeObjectURL(member.avatar_url);
    }
    member.avatar_url = "";
    emitTopic("member");
  },

  async activateMember(
    id: string,
    machineId?: string,
  ): Promise<MemberActivateResult> {
    // Presence contract: write desired_state=online INTENT and enter WAKING. When a
    // machineId is given, BIND the agent to that machine (persist it on
    // `desired_machine_id`, which carries the machine binding id) — the spawn/wake path
    // and the "move agent" rebind both land here. Without a real agent there is nothing
    // to report presence back, so the mock stays waking (honest) — it never
    // fabricates an online session.
    //
    // T-7fa1: the mock has no wardens, so it can never observe a real undelivered
    // START — the honest default is `activationPending: false` (which is exactly
    // what today's mock behaviour, presence→waking, asserts). `__setMockActivationPending`
    // stages the OTHER branch so the failure UI is reachable without a broken
    // machine.
    const w = findWire(id);
    if (activationPendingNext) {
      // Nothing was dispatched: do NOT move presence. A mock that flipped to
      // waking here would reproduce the very lie this ticket removes.
      if (machineId !== undefined) w.desired_machine_id = machineId;
      return { activationPending: true };
    }
    w.desired_state = "online";
    w.presence = "waking"; // never optimistic-green — honest waking, not online
    if (machineId !== undefined) w.desired_machine_id = machineId; // permanent rebind
    return { activationPending: false };
  },

  async relocateMember(
    id: string,
    machineId: string,
  ): Promise<MemberRelocateResult> {
    // 改機器 (mirror handle_relocate_member): PLACEMENT ONLY — re-pin
    // `desired_machine_id` and NOTHING else. Unlike activateMember it never
    // touches `desired_state`/presence (a relocate is not a wake). The real
    // backend then reconciles a live member onto the pin; the mock has no live
    // session to migrate, so the re-pin is the whole honest effect.
    const w = findWire(id);
    w.desired_machine_id = machineId;
    // T-7fa1: same honest default as activateMember — the mock has no warden to
    // fail to reach, so the re-pin always "lands" unless a test stages otherwise.
    return {
      relocationPending: relocationPendingNext,
      relocationDeferred: relocationDeferredNext,
    };
  },

  async deactivateMember(id: string): Promise<void> {
    // Graceful STOP intent: write desired_state=offline. The mock has no live agent to
    // wind down (it is never online), so there is no honest `stopping`/
    // `stopped` phase to enter — a stop / wake-cancel simply falls back to
    // offline. The real backend derives stopping→stopped from a live session's
    // shutdown; the mock never fabricates one.
    const w = findWire(id);
    w.desired_state = "offline";
    w.presence = "offline";
  },

  async resetMemberCost(id: string): Promise<CostResetReceipt> {
    // 成本歸零 (mirror handle_reset_cost): clear BOTH halves of the actor's spend
    // and answer with what was destroyed. Cost lives on the monitoring session
    // row here — MemberDTO carries none — which is also where the real backend's
    // two halves surface, so the mutation lands where the cockpit reads it.
    // An id with no session row clears nothing and honestly reports nulls.
    const row = wireMonitoring.sessions.find((s) => s.id === id);
    const clearedCost = row?.cost ?? null;
    const clearedBankedCost = row?.banked_cost ?? null;
    if (row) {
      row.cost = null;
      row.banked_cost = null;
    }
    // The figure is rendered from TWO stores here — the monitoring row and, for
    // an outsource actor, its worker row — so clearing one and not the other
    // leaves the panel showing the number the reset just destroyed (found by
    // independent review, T-56). The real server has one figure per actor and
    // fans a delta on both topics; the mock has to keep its two copies in step
    // by hand or it stops being a rehearsal of that.
    const worker = outsourceWorkers.find((w) => w.id === id);
    if (worker) {
      worker.cost = null;
      worker.bankedCost = null;
      emitTopic("outsource_worker");
    }
    // The production route fans a `monitoring` signal so the cockpit refetches.
    // Without it here the mock reports success and nothing on screen moves.
    emitTopic("monitoring");
    return { memberId: id, clearedCost, clearedBankedCost };
  },

  async resetAccountCost(account: string): Promise<AccountCostResetReceipt> {
    // 帳號歸零 (mirror handle_reset_account_cost): zero the ACCOUNT's own
    // accumulated figure and touch NO member — the separation owner ruling
    // rc-5c5d7c7c6dcd asked for. The account row is where the cockpit reads it,
    // so the mutation lands there and the session rows are left alone.
    // An account nobody reports under clears nothing and honestly answers null.
    const row = wireMonitoring.accounts.find((a) => a.account === account);
    const clearedCost = row?.cost ?? null;
    if (row) {
      row.cost = null;
    }
    // NO member or worker store is touched — that separation is the ruling this
    // route exists for. Only the monitoring signal fans, the same one the
    // production route publishes so the cockpit refetches the card.
    emitTopic("monitoring");
    return { account, clearedCost };
  },

  async forceStopMember(id: string): Promise<void> {
    // Immediate kill escalation (mirror handle_force_stop_member): write
    // desired_state=offline and fall to offline. The mock has no live agent/warden to
    // SIGKILL, so — like deactivate — it simply lands offline; the real backend
    // dispatches the robust STOP to the warden immediately, bypassing the grace.
    const w = findWire(id);
    w.desired_state = "offline";
    w.presence = "offline";
  },

  async acceleratedStopMember(id: string): Promise<void> {
    // 加速停止 (mirror handle_accelerated_stop_member): the MIDDLE rung. It
    // ESCALATES a wind-down that is already open — the mock reproduces the 409
    // gate rather than the clock, because the gate is the part a cockpit can get
    // wrong (offering the button where the server would refuse it) and the clock
    // is server-side arithmetic the mock has no session to run.
    const w = findWire(id);
    // `stopping_since` is deliberately NOT on the wire, so the mock reads the
    // projection the server derives from it — which is also the only thing the
    // cockpit itself can see, and therefore the right basis for a mock whose job
    // is to catch a cockpit offering a button the server would refuse.
    const windingDown = w.presence === "stopping" || (w.refocus_since ?? 0) > 0;
    if (!windingDown) {
      throw mockApiError(
        "http 409 for POST /api/members/{id}/accelerated-stop",
        409,
        "加速停止 escalates a wind-down that is already open — this member has not been asked to stop. Press 停止 (deactivate) or 重新聚焦 (refocus) first",
      );
    }
    w.refocus_op = "accelerated_stop";
    const stamps = acceleratedStopStamps(w.desired_state === "offline");
    if (stamps.since !== null) w.refocus_since = stamps.since;
    w.refocus_deadline = stamps.deadline;
  },

  async dismissMember(id: string): Promise<void> {
    // Soft delete (mirror handle_dismiss_member): flip status=removed + intent
    // desired_state=offline. listMembers / getMember filter removed rows, so the
    // member drops from the roster (getMember then 404s) — never a hard delete.
    const w = findWire(id);
    w.roster_status = "removed";
    w.desired_state = "offline";
  },

  async patchMember(id: string, patch: MemberPatch): Promise<Member> {
    const w = findWire(id);
    if (patch.name !== undefined) w.name = patch.name;
    // model/effort launch intents (M2-2) — same closed effort vocabulary the
    // server enforces (422 → throw), model stays a free string.
    if (patch.effort !== undefined) {
      if (!["low", "medium", "high", "max"].includes(patch.effort)) {
        throw mockApiError(
          `http 422 for PATCH /api/members/${id}`,
          422,
          "effort must be one of ['high', 'low', 'max', 'medium']",
        );
      }
      w.effort = patch.effort;
    }
    if (patch.model !== undefined) w.model = patch.model;
    return mapWithExtras(w);
  },

  async refocusMember(id: string): Promise<void> {
    // Server-side refocus is online-only; the mock member is never online, so
    // this is a no-op that simply records the intent timestamp.
    const w = findWire(id);
    w.refocus_since = Date.now() / 1000;
  },

  async listWebhooks(memberId: string): Promise<WebhookEndpoint[]> {
    findWire(memberId); // 404 parity: an unknown member throws
    return (mockWebhooks.get(memberId) ?? []).map((e) => ({ ...e }));
  },

  async createWebhook(
    memberId: string,
    input: WebhookCreateInput,
  ): Promise<WebhookEndpoint> {
    findWire(memberId);
    const endpointId = input.endpointId.trim();
    // Same closed charset the server enforces (422 → throw).
    if (!/^[A-Za-z0-9_-]+$/.test(endpointId)) {
      throw mockApiError(
        `http 422 for POST /api/members/${memberId}/webhooks`,
        422,
        "endpoint id may contain only letters, digits, '_' and '-'",
      );
    }
    const list = mockWebhooks.get(memberId) ?? [];
    if (list.some((e) => e.endpointId === endpointId)) {
      throw mockApiError(
        `http 409 for POST /api/members/${memberId}/webhooks`,
        409,
        `a webhook endpoint '${endpointId}' already exists for this member`,
      );
    }
    const platform = input.platform ?? "generic";
    const secret = input.signingSecret?.trim() ?? "";
    // slack/github require a signing secret (server 422 parity); generic ignores it.
    if ((platform === "slack" || platform === "github") && !secret) {
      throw mockApiError(
        `http 422 for POST /api/members/${memberId}/webhooks`,
        422,
        `signing_secret is required when platform is '${platform}'`,
      );
    }
    const hasSecret = platform !== "generic" && secret !== "";
    if (hasSecret) {
      mockWebhookSecrets.set(secretKey(memberId, endpointId), secret);
    }
    const created: WebhookEndpoint = {
      endpointId,
      purpose: input.purpose ?? "",
      status: "enabled",
      createdTs: Date.now() / 1000,
      token: mockWebhookToken(),
      platform,
      hasSigningSecret: hasSecret,
      // A fresh endpoint has never been called (server parity: all-zero).
      lastReceivedTs: 0,
      deliveredCount: 0,
      droppedCount: 0,
      lastDropReason: "",
    };
    mockWebhooks.set(memberId, [...list, created]);
    // Never echo the secret — return the view model only (has_signing_secret).
    return { ...created };
  },

  async updateWebhook(
    memberId: string,
    endpointId: string,
    patch: WebhookUpdate,
  ): Promise<WebhookEndpoint> {
    const list = mockWebhooks.get(memberId) ?? [];
    const e = list.find((x) => x.endpointId === endpointId);
    if (!e) {
      throw mockApiError(
        `http 404 for PATCH /api/members/${memberId}/webhooks/${endpointId}`,
        404,
        `webhook endpoint '${endpointId}' not found`,
      );
    }
    if (patch.status !== undefined) e.status = patch.status;
    if (patch.purpose !== undefined) e.purpose = patch.purpose;
    // Signing-secret rotation: store the new plaintext in the vault (never on
    // the view model) and flip has_signing_secret. `platform` is immutable here.
    if (patch.signingSecret !== undefined) {
      const secret = patch.signingSecret.trim();
      if (secret) {
        mockWebhookSecrets.set(secretKey(memberId, endpointId), secret);
        e.hasSigningSecret = true;
      } else {
        mockWebhookSecrets.delete(secretKey(memberId, endpointId));
        e.hasSigningSecret = false;
      }
    }
    return { ...e };
  },

  async deleteWebhook(memberId: string, endpointId: string): Promise<void> {
    const list = mockWebhooks.get(memberId) ?? [];
    if (!list.some((x) => x.endpointId === endpointId)) {
      throw mockApiError(
        `http 404 for DELETE /api/members/${memberId}/webhooks/${endpointId}`,
        404,
        `webhook endpoint '${endpointId}' not found`,
      );
    }
    mockWebhooks.set(
      memberId,
      list.filter((x) => x.endpointId !== endpointId),
    );
    mockWebhookSecrets.delete(secretKey(memberId, endpointId));
  },

  async listWebhookRequests(
    memberId: string,
    endpointId: string,
  ): Promise<WebhookRequestLog[]> {
    // Server parity: the last 5 raw /in requests, newest first. Endpoints
    // without simulated traffic honestly read empty.
    const rows = mockWebhookRequests.get(`${memberId} ${endpointId}`) ?? [];
    return rows.map((r) => ({ ...r }));
  },

  async listScheduledMessages(memberId: string): Promise<ScheduledMessage[]> {
    findScheduleRecipient(memberId); // 404 parity
    return (mockScheduledMessages.get(memberId) ?? []).map((s) => ({ ...s }));
  },

  async createScheduledMessage(
    memberId: string,
    input: ScheduledMessageCreateInput,
  ): Promise<ScheduledMessage> {
    findScheduleRecipient(memberId);
    // Months are resolved BEFORE anything judges them — a create has no prior
    // row, so `stored` is empty and an omitted field becomes the whole year.
    const months = resolveMockMonths(input.cadence, [], input.customMonths);
    validateSchedulePart(memberId, {
      body: input.body,
      cadence: input.cadence,
      dayOfWeek: input.dayOfWeek,
      dayOfMonth: input.dayOfMonth,
      hour: input.hour,
      minute: input.minute,
      customMonths: months,
      customDays: input.customDays,
      customHours: input.customHours,
      customMinutes: input.customMinutes,
      timezone: input.timezone,
    });
    requireCadenceFields(memberId, input.cadence, input);
    if (input.cadence === "custom")
      requireAPossibleDate(memberId, months, input.customDays ?? []);
    const created: ScheduledMessage = {
      id: mockScheduleId(),
      memberId,
      label: input.label?.trim() ?? "",
      body: input.body,
      cadence: input.cadence,
      // The DTO's stated omitted-value behaviour: day_of_week → 0 (Sunday),
      // day_of_month → 1.
      dayOfWeek: input.dayOfWeek ?? 0,
      dayOfMonth: input.dayOfMonth ?? 1,
      hour: input.hour ?? 0,
      minute: input.minute ?? 0,
      // Months come from the resolver, not from this ternary: a `custom` row
      // always LISTS its months (the whole year when the caller named none),
      // and a non-custom row keeps whatever the caller stated — which the
      // resolver has already reduced to the empty set when that was nothing.
      customMonths: sortedSet(months),
      // Empty for every cadence but `custom` — the sets are that cadence's
      // whole schedule, and a non-custom row must not carry a stale one.
      customDays:
        input.cadence === "custom" ? sortedSet(input.customDays ?? []) : [],
      customHours:
        input.cadence === "custom" ? sortedSet(input.customHours ?? []) : [],
      customMinutes:
        input.cadence === "custom" ? sortedSet(input.customMinutes ?? []) : [],
      timezone: input.timezone,
      status: "enabled",
      lastFiredSlot: mockScheduleSlot(input),
      // A fresh schedule has never actually delivered (server parity: 0).
      lastFiredTs: 0,
      createdTs: Date.now() / 1000,
    };
    mockScheduledMessages.set(memberId, [
      ...(mockScheduledMessages.get(memberId) ?? []),
      created,
    ]);
    return { ...created };
  },

  async updateScheduledMessage(
    memberId: string,
    scheduleId: string,
    patch: ScheduledMessageUpdate,
  ): Promise<ScheduledMessage> {
    const list = mockScheduledMessages.get(memberId) ?? [];
    const s = list.find((x) => x.id === scheduleId);
    if (!s) {
      throw mockApiError(
        `http 404 for PATCH /api/members/${memberId}/scheduled-messages/${scheduleId}`,
        404,
        `scheduled message '${scheduleId}' not found`,
      );
    }
    // 🔴 Resolved against the cadence this row will HAVE, not the one it had:
    // that is what lets a PATCH switch a never-custom row to `custom` without
    // naming months and still land the all-twelve meaning it has always had.
    const cadenceAfter = patch.cadence ?? s.cadence;
    const months = resolveMockMonths(
      cadenceAfter,
      s.customMonths,
      patch.customMonths,
    );
    validateSchedulePart(
      memberId,
      { ...patch, customMonths: months },
      cadenceAfter,
    );
    // Switching TO custom must arrive with the four sets in the SAME request
    // unless the stored row already carries them — a cadence with no times for
    // it is exactly what the conditional 422 exists to refuse.
    if (patch.cadence !== undefined) {
      requireCadenceFields(memberId, patch.cadence, {
        hour: patch.hour ?? s.hour,
        minute: patch.minute ?? s.minute,
        customDays: patch.customDays ?? s.customDays,
        customHours: patch.customHours ?? s.customHours,
        customMinutes: patch.customMinutes ?? s.customMinutes,
      });
    }
    // Judged on the WHOLE row the way the server judges it, not on what this
    // request happened to state: narrowing the months alone is how an ordinary
    // schedule becomes one that can never fire again.
    if (cadenceAfter === "custom")
      requireAPossibleDate(memberId, months, patch.customDays ?? s.customDays);
    if (patch.label !== undefined) s.label = patch.label;
    if (patch.body !== undefined) s.body = patch.body;
    if (patch.cadence !== undefined) s.cadence = patch.cadence;
    if (patch.dayOfWeek !== undefined) s.dayOfWeek = patch.dayOfWeek;
    if (patch.dayOfMonth !== undefined) s.dayOfMonth = patch.dayOfMonth;
    if (patch.hour !== undefined) s.hour = patch.hour;
    if (patch.minute !== undefined) s.minute = patch.minute;
    // Switching AWAY from custom leaves the stored sets in place, unread, so
    // switching back does not lose the choice.
    // Months are assigned unconditionally because the resolver has already
    // answered "absent means unchanged" for them — assigning only on
    // `!== undefined` would drop the all-twelve default a switch-to-custom
    // depends on.
    s.customMonths = sortedSet(months);
    if (patch.customDays !== undefined)
      s.customDays = sortedSet(patch.customDays);
    if (patch.customHours !== undefined)
      s.customHours = sortedSet(patch.customHours);
    if (patch.customMinutes !== undefined)
      s.customMinutes = sortedSet(patch.customMinutes);
    if (patch.timezone !== undefined) s.timezone = patch.timezone;
    if (patch.status !== undefined) s.status = patch.status;
    // Re-aim the cursor only when a CADENCE/SLOT field moved (design §實作時才
    // 浮出來的三個決定 #1): editing label/body/status must leave it alone, or
    // disable-then-enable would silently swallow the next delivery.
    const reAimed =
      patch.cadence !== undefined ||
      patch.dayOfWeek !== undefined ||
      patch.dayOfMonth !== undefined ||
      patch.hour !== undefined ||
      patch.minute !== undefined ||
      patch.customMonths !== undefined ||
      patch.customDays !== undefined ||
      patch.customHours !== undefined ||
      patch.customMinutes !== undefined ||
      patch.timezone !== undefined;
    if (reAimed) s.lastFiredSlot = mockScheduleSlot(s);
    return { ...s };
  },

  async deleteScheduledMessage(
    memberId: string,
    scheduleId: string,
  ): Promise<void> {
    const list = mockScheduledMessages.get(memberId) ?? [];
    if (!list.some((x) => x.id === scheduleId)) {
      throw mockApiError(
        `http 404 for DELETE /api/members/${memberId}/scheduled-messages/${scheduleId}`,
        404,
        `scheduled message '${scheduleId}' not found`,
      );
    }
    mockScheduledMessages.set(
      memberId,
      list.filter((x) => x.id !== scheduleId),
    );
  },

  async getMemberResumeSummary(
    memberId: string,
  ): Promise<MemberResumeSummaryView> {
    // 404 parity: an unknown id throws — but an `ow-` id is NOT unknown here
    // (this is the one member verb released to workers). See
    // findResumeSummaryTarget.
    findResumeSummaryTarget(memberId);

    // Mirrors the CHAT/TASKS/OVERVIEW SUBSET of the server's
    // resumeSnapshotParts(actor=memberId): a BOUNDED recent-chat window
    // involving the member, the member's NON-TERMINAL executed tasks (LIGHT
    // rows, most recently updated first), and an overview computed FROM those
    // two lists — never a separately-fabricated count, so it cannot drift from
    // what the lists actually carry (same honesty contract the real endpoint's
    // shared assembly gives). READ-ONLY: this never advances a read watermark —
    // true of every read door here since T-48, listChat included.
    //
    // The T-1b09 studio-floor blocks (roster / machines) ARE mocked now, and
    // so are their roster_chars / machines_chars sizes — see below. They used
    // to be the admitted gap in this comment; the panel now renders them, and
    // a mock that does not carry a section the panel draws would make the
    // offline cockpit disagree with the real one exactly where this section's
    // whole purpose is that the two agree.
    const RESUME_CHAT_N = 5;
    const RESUME_TASKS_N = 5;

    const chatAll = chatLog
      .filter((m) => m.from === memberId || m.to === memberId)
      .sort((a, b) => a.ts - b.ts);
    // The cut point: whole messages this payload does NOT carry. TRUNCATION —
    // a different thing from a message that IS here with its body folded, and
    // the hint is the SERVER's own recovery instruction, carried verbatim.
    const chatCut = chatAll.slice(
      0,
      Math.max(0, chatAll.length - RESUME_CHAT_N),
    );
    const chatWindow = chatAll.slice(-RESUME_CHAT_N);
    const oldestCarried = chatWindow[0];
    const chatEarlierOmitted = {
      omitted: chatCut.length > 0,
      hint:
        chatCut.length > 0 && oldestCarried
          ? `call get_chat with with='${oldestCarried.from === memberId ? oldestCarried.to : oldestCarried.from}' and BOTH before_ts=${oldestCarried.ts} and before_id='${oldestCarried.id}' (sending only one is a 422)`
          : "",
    };

    // Display names beside the ids, and a rendered timestamp beside the epoch
    // one — the same two-fields-not-one shape the server serves. `fromName` /
    // `toName` resolve through the roster; an id that resolves to nothing keeps
    // the HONEST "" (the panel then shows the id alone rather than a name it
    // invented).
    //
    // The QUOTE rides along too, and it is the ONE read that carries display
    // names inside it. The server's snapshot path joins `reply_to_chat` through
    // the same helper every other read uses but hands it the roster, so the wake
    // payload's quote says 「名字 → 名字」 where a browser read says 「"" → ""」
    // (api_chat.go resumeChatMessageDTO). The snapshot is ALSO the read that
    // BILLS those characters against the chat budget, so a mock that dropped the
    // quote here would preview a card cheaper and emptier than the real one.
    const chat = chatWindow.map((m) => {
      const quote = mockReplyToChatOf(m.replyTo);
      return {
        ...m,
        fromName: resumeDisplayNameOf(m.from),
        toName: resumeDisplayNameOf(m.to),
        tsDisplay: mockTsDisplay(m.ts),
        bodyOmittedChars: m.bodyOmittedChars ?? 0,
        card: m.card ?? null,
        replyToChat: quote
          ? {
              ...quote,
              fromName: resumeDisplayNameOf(quote.from),
              toName: resumeDisplayNameOf(quote.to),
            }
          : null,
      };
    });

    // EXECUTOR MATCH IS BY ID ALONE — no executorKind gate. That mirrors the
    // server exactly: `resumeTasksFor` → `ListOpenTasksByExecutor` filters on
    // `executor_id = ?` and nothing else (dal_tasks.go), because a task's
    // executorKind and executorId always move together (assignment and
    // reassign write both), so the kind adds no discrimination for a member —
    // it only ever SUBTRACTS the rows of an `ow-` worker. With the id now let
    // through above, keeping it would hand every worker an empty task list
    // while claiming to mirror the server.
    const openTasksAll = tasks.filter(
      (t) => t.executorId === memberId && !TERMINAL_TASK_STATUSES.has(t.status),
    );
    const openTasksSorted = [...openTasksAll].sort(
      (a, b) => b.updatedTs - a.updatedTs,
    );
    const tasksOut: ResumeTaskView[] = openTasksSorted
      .slice(0, RESUME_TASKS_N)
      .map((t) => {
        // First non-terminal step (superseded skipped like done — same rule
        // the workflow timeline uses); "" when the plan is empty/complete.
        const step = t.steps.find((s) => !TERMINAL_STEP_STATUSES.has(s.status));
        const detailChars = t.steps.reduce(
          (sum, s) => sum + s.name.length + s.dod.length,
          0,
        );
        const answeredCardSteps: ResumeAnsweredCardStepView[] = t.steps
          .filter(
            (s) =>
              s.status === "in_progress" &&
              s.replyCardId !== "" &&
              mockReplyCardStatusOf(s.replyCardId) === "answered",
          )
          .map((s) => ({
            stepId: s.id,
            stepName: s.name,
            cardId: s.replyCardId,
          }));
        return {
          id: t.id,
          taskNo: t.taskNo,
          title: t.title,
          typeKey: t.typeKey,
          status: t.status,
          priority: t.priority,
          waitingReason: t.waitingReason,
          currentStepId: step?.id ?? "",
          currentStepName: step?.name ?? "",
          progressDone: t.progressDone,
          progressTotal: t.progressTotal,
          updatedTs: t.updatedTs,
          detailChars,
          answeredCardSteps,
          // T-91 — the mock mirrors the server's own projection: the hold is
          // copied off the task row, and `blocking` is the REVERSE dependency
          // edge computed the same way the server computes it (every
          // non-terminal task that names this one in its blockedBy).
          lock: t.lock ?? "",
          reassignedFrom: t.reassignedFrom ?? "",
          reassignedFromKind: t.reassignedFromKind ?? "",
          blocking: tasks
            .filter(
              (o) =>
                !["completed", "terminated", "duplicated"].includes(o.status) &&
                (o.deps ?? []).includes(t.id),
            )
            .map((o) => o.id),
        };
      });

    const answeredCardSteps = tasksOut.flatMap((t) => t.answeredCardSteps);

    const cardsForMember = replyCards.filter((c) => c.from === memberId);
    const dayAgoTs = Date.now() / 1000 - 86400;
    const cardsWaiting = cardsForMember.filter(
      (c) => c.status === "waiting",
    ).length;
    const cardsAnsweredRecent = cardsForMember.filter(
      (c) => c.status === "answered" && (c.answeredTs ?? 0) >= dayAgoTs,
    ).length;

    // ── studio-floor blocks (T-1b09) ────────────────────────────────────────
    // roster: every member AND every contractor, with the presence the ruling
    // rc-4e98c0481852 asks for. Members carry `duty` and leave `currentTask`
    // empty; contractors carry the bound task's TITLE and its progress and
    // leave `duty` empty (正職給職責、外包給任務標題) — the same asymmetry the
    // server serves, so a reader that learns the rule here reads the real one.
    const roster: ResumeRosterMemberView[] = [
      ...wireMembers
        .filter((m) => m.kind !== "warden")
        .map((m) => ({
          id: m.id,
          name: m.name,
          kind: "member",
          roleName: m.role_name ?? "",
          duty: m.role_key ? `職責定義：${m.role_key}` : "",
          currentTask: "",
          taskStatus: "",
          waitingReason: "",
          progressDone: 0,
          progressTotal: 0,
          machine: m.machine ?? "",
          presence: m.presence ?? "offline",
        })),
      ...outsourceWorkers.map((w) => {
        const bound = tasks.find((t) => t.executorId === w.id);
        return {
          id: w.id,
          name: w.codename,
          kind: "outsource",
          roleName: "",
          duty: "",
          currentTask: bound?.title ?? "",
          taskStatus: bound?.status ?? "",
          waitingReason: bound?.waitingReason ?? "",
          progressDone: bound?.progressDone ?? 0,
          progressTotal: bound?.progressTotal ?? 0,
          machine: w.machine ?? "",
          presence: w.presence ?? "offline",
        };
      }),
    ];

    // machines: the fleet, keyed by the STABLE machine id (never the name a
    // host reports for itself). `youAreOn` is the subject's SERVER-RECORDED
    // binding — "" when it has none, never guessed from anything else.
    const machineRows = wireMembers.filter(
      (m) => m.kind === "warden" && m.roster_status !== "removed",
    );
    const machines: ResumeMachinesView = {
      list: machineRows.map((m) => ({
        machineId: m.id,
        displayName: m.name,
        online: m.presence === "online",
      })),
      youAreOn:
        wireMembers.find((m) => m.id === memberId)?.machine ??
        outsourceWorkers.find((w) => w.id === memberId)?.machine ??
        "",
    };

    return {
      identity: memberId,
      chat,
      tasks: tasksOut,
      overview: {
        chatCount: chat.length,
        chatChars: chat.reduce((sum, m) => sum + m.body.length, 0),
        tasksReturned: tasksOut.length,
        tasksOpenTotal: openTasksAll.length,
        tasksDetailChars: tasksOut.reduce((sum, t) => sum + t.detailChars, 0),
        cardsWaiting,
        cardsAnsweredRecent,
        // Sized FROM the blocks actually returned, never fabricated — the same
        // honesty contract the counts above already keep.
        rosterChars: roster.reduce(
          (sum, r) =>
            sum +
            r.id.length +
            r.name.length +
            r.duty.length +
            r.currentTask.length +
            r.roleName.length,
          0,
        ),
        machinesChars: machines.list.reduce(
          (sum: number, m) => sum + m.machineId.length + m.displayName.length,
          0,
        ),
        stepsOnAnsweredCard: answeredCardSteps.length,
        stepsOnAnsweredCardChars: answeredCardSteps.reduce(
          (sum, s) =>
            sum +
            [...s.stepId].length +
            [...s.stepName].length +
            [...s.cardId].length,
          0,
        ),
      },
      generatedAt: mockTsDisplay(Math.floor(Date.now() / 1000)),
      chatEarlierOmitted,
      roster,
      machines,
      note: "BOUNDED snapshot — recent chat + open tasks only; page the rest with list_chat / list_tasks / get_task.",
    };
  },

  async listChat(
    withId: string,
    limit?: number,
    before?: ChatCursor,
  ): Promise<ChatMessage[]> {
    // The conversation with `withId`: messages to or from that member, ascending
    // by (ts, id) — the same total order the BE pages by, so equal-ts messages
    // never straddle a page boundary. Only ever the owner's own sent messages
    // (see chatLog note) — the mock never fabricates a member reply. `limit`
    // mirrors the BE param's semantics where it matters: a POSITIVE limit
    // keeps the most recent N, a NEGATIVE limit (-1, the M2-3 gallery
    // full-history path) returns all. Omitted stays "all" (the mock log is
    // tiny — no 30-cap needed). `before` (T-bf82 scrollback) applies the BE's
    // keyset predicate (ts < beforeTs OR (ts == beforeTs AND id < beforeId))
    // BEFORE the recent-window cap — the history page.
    let msgs = chatLog
      .filter((m) => m.from === withId || m.to === withId)
      .sort((a, b) => a.ts - b.ts || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
    if (before) {
      msgs = msgs.filter(
        (m) =>
          m.ts < before.beforeTs ||
          (m.ts === before.beforeTs && m.id < before.beforeId),
      );
    }
    if (limit !== undefined && limit >= 0) {
      msgs = limit === 0 ? [] : msgs.slice(-limit);
    }
    // NO READ RECEIPT (T-48, BE parity): listing a conversation is NOT reading
    // it. GET /api/chat used to advance the owner's watermark on a cursorless
    // list; the owner ruled that marking read must be an explicit intent, so
    // the write moved out of this door entirely. The mock mirrors that — the
    // only thing that marks anything read here is markChatRead().
    // Read-time joins (server parity) — a copy per message so callers never
    // mutate the log.
    return msgs.map(mockServedChatMessage);
  },

  async listChatWindow(
    withId: string,
    anchor: ChatAnchor,
    limit: number,
  ): Promise<ChatMessage[]> {
    // Mock twin of the T-48 anchor window (`?start_id=` / `?end_id=`). Same
    // total order the BE pages by, both ends INCLUSIVE, answered oldest→newest.
    //
    // THROWS on an id this conversation does not carry, matching the server's
    // 404 — an unknown anchor must never look like "a real window that happens
    // to be empty", which is the exact confusion the whole feature exists to
    // remove.
    const msgs = chatLog
      .filter((m) => m.from === withId || m.to === withId)
      .sort((a, b) => a.ts - b.ts || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
    const indexOf = (id: string) => {
      const i = msgs.findIndex((m) => m.id === id);
      // A REAL 404, not a bare Error (T-48): the cockpit now tells "no such
      // message" apart from "the read failed" by the status, and a statusless
      // throw here would make the offline cockpit say 「現在讀不到,可以再試」 about
      // an id that genuinely does not exist.
      if (i < 0)
        throw mockApiError(
          `http 404 for GET /api/chat?with=${withId}`,
          404,
          `no message carries id ${id}`,
        );
      return i;
    };
    let lo = 0;
    let hi = msgs.length - 1;
    if (anchor.startId !== undefined) lo = indexOf(anchor.startId);
    if (anchor.endId !== undefined) hi = indexOf(anchor.endId);
    if (lo > hi)
      throw mockApiError(
        `http 422 for GET /api/chat?with=${withId}`,
        422,
        "start_id is newer than end_id",
      );
    let window = msgs.slice(lo, hi + 1);
    // Truncate at the START (older) end when both anchors are given — the
    // window stays anchored on `end_id`. With only `start_id` the anchor is the
    // OLD end, so the cap keeps the FIRST `limit`.
    window =
      anchor.endId !== undefined
        ? window.slice(-limit)
        : window.slice(0, limit);
    return window.map(mockServedChatMessage);
  },

  async getChatMessage(id: string): Promise<ChatMessage> {
    // Mock twin of GET /api/chat?ids=<id> — ONE named message in full, no
    // read-watermark side effect. Caller-blind, exactly like the server: the
    // by-ids door reaches as far as the ordinary listing does.
    //
    // THROWS on an unknown id, matching the server's all-or-nothing 404. That is
    // not pedantry here: the quote row's whole design rests on a failure being
    // said out loud instead of drawn as a plausible-looking blank, and a mock
    // that resolved to `null` would let an offline session build a UI branch the
    // real adapter can never reach.
    const found = chatLog.find((m) => m.id === id);
    if (!found) throw new Error(`no message carries id ${id}`);
    return mockServedChatMessage(found);
  },

  async listChatAttachments(withId: string): Promise<GalleryAttachment[]> {
    // Mirrors the BE gallery query (handle_list_chat_attachments): flatten the
    // attachments of EVERY logged message the member participates in
    // (owner↔member both directions + inter-agent threads), newest→oldest,
    // each row carrying the sender id + the roster-resolved display name
    // ("" for the owner — the UI renders its own 「我」 label). READ-ONLY: this
    // never advances a read watermark, as no read door here has since T-48.
    // HONEST: derived
    // solely from real logged messages — never a fabricated entry.
    const involved = chatLog
      .filter((m) => m.from === withId || m.to === withId)
      .sort((a, b) => b.ts - a.ts);
    return involved.flatMap((m) =>
      m.attachments.map((att) => ({
        ...att,
        messageId: m.id,
        from: m.from,
        fromName:
          m.from === MOCK_OWNER_ID
            ? ""
            : (wireMembers.find((w) => w.id === m.from)?.name ?? ""),
        to: m.to,
        ts: m.ts,
      })),
    );
  },

  async getChatAttachmentShareLink(attachmentId: string): Promise<string> {
    // Mock face: the same URL SHAPE as the BE (serve path + ?sig=) with a
    // deterministic fake sig — never a verifiable credential (no secret in
    // mock mode; the copy-share-link UI just needs a resolvable string).
    return `/api/chat/attachment/${attachmentId}?sig=mock-share-sig`;
  },

  async postChat(msg: {
    to: string;
    body: string;
    attachments?: ChatAttachmentInput[];
    replyTo?: string;
  }): Promise<ChatMessage> {
    // Record the owner's message into the in-memory log and echo it back. The
    // sender is MOCK_OWNER_ID ("owner") — matching the real backend, which
    // stamps `from` from the owner JWT sub (the fixed owner id "owner"), so the
    // owner's message reads as "me" in the UI in both modes. HONEST: we store
    // ONLY this message — no auto-generated reply. Every generic attachment
    // (image OR file) echoes back as its own data-URI `url` (the mock has no
    // served blob endpoint), so previews/downloads still render in mock mode —
    // the SAME list-per-message rule as the http adapter.
    const stamp = Date.now();
    const attachments = (msg.attachments ?? []).map((att, i) => {
      // Derive isImage from the explicit mime, else the data-URI's own mime prefix.
      const dataUriMime = att.dataB64.startsWith("data:")
        ? att.dataB64.slice(5, att.dataB64.indexOf(";"))
        : "";
      const mime = att.mime || dataUriMime || "application/octet-stream";
      return {
        id: `mock-att-${stamp}-${i}`,
        url: att.dataB64,
        filename: att.filename || "",
        mime,
        isImage: mime.startsWith("image/"),
      };
    });
    // The quote link (T-4e95). The mock enforces the SAME refusal the server
    // does, and only that one: the target must EXIST. The same-conversation
    // refusal that used to sit here went with the server's on 2026-08-21 —
    // quoting a line out of another conversation is the use case now, and a
    // mock that still refused it would make offline preview disagree with the
    // real thing about the very behaviour this change exists to add.
    if (msg.replyTo && !chatLog.some((m) => m.id === msg.replyTo)) {
      throw new Error(`reply_to names no message (${msg.replyTo})`);
    }
    const sent: ChatMessage = {
      id: `mock-${stamp}-${++mockChatSeq}`,
      from: MOCK_OWNER_ID,
      to: msg.to,
      body: msg.body,
      ts: stamp / 1000,
      attachments,
      // Only an agent's create_reply_card ever stamps a card link — an
      // owner-posted message never carries one (mirrors the server).
      replyCardId: null,
      replyCardStatus: null,
      replyTo: msg.replyTo ?? null,
    };
    chatLog.push(sent);
    // Echoed through the SAME read projection the listing uses, because the
    // server echoes through servedChatMessageDTO: a reply's quote is on the POST
    // response too, and a mock that left it off would have the thread flicker
    // between "quoted" and "not quoted" offline and not online.
    return mockServedChatMessage(sent);
  },

  async markChatRead(mark: {
    peer: string;
    lastReadTs: number;
  }): Promise<ChatReadReceipt> {
    // Record the OWNER's read watermark for this peer conversation (reader =
    // MOCK_OWNER_ID, matching the BE's verified-sub stamp). Monotonic.
    return markRead(MOCK_OWNER_ID, mark.peer, mark.lastReadTs);
  },

  async listChatReads(peer: string): Promise<ChatReadReceipt[]> {
    // Receipts for this peer conversation (peer_id === peer). HONEST: the mock
    // only ever records the OWNER's own watermark (no fabricated member reply →
    // no fabricated member read), so a member's "read ✓" appears only if a real
    // watermark was recorded for it — which the single-owner mock never is.
    return [...chatReads.values()].filter((r) => r.peerId === peer);
  },

  async listReplyCards(
    status: "waiting" | "answered" | "expired",
  ): Promise<ReplyCard[]> {
    // Mirror the server's list contract: waiting = longest-waiting first
    // (created asc); answered = last-24h window, newest answer first; expired
    // = last-24h window keyed off expiredTs, newest first. The structuredClone
    // keeps callers from mutating mock state.
    if (status === "waiting") {
      return structuredClone(
        replyCards
          .filter((c) => c.status === "waiting")
          .sort((a, b) => a.createdTs - b.createdTs),
      );
    }
    const cutoff = Date.now() / 1000 - 24 * 3600;
    if (status === "expired") {
      return structuredClone(
        replyCards
          .filter((c) => c.status === "expired" && (c.expiredTs ?? 0) >= cutoff)
          .sort((a, b) => (b.expiredTs ?? 0) - (a.expiredTs ?? 0)),
      );
    }
    return structuredClone(
      replyCards
        .filter(
          (c) =>
            c.status === "answered" &&
            c.answeredTs !== null &&
            c.answeredTs >= cutoff,
        )
        .sort((a, b) => (b.answeredTs ?? 0) - (a.answeredTs ?? 0)),
    );
  },

  async getReplyCard(id: string): Promise<ReplyCard> {
    // The single-card read behind B3's inline chat card (mirrors
    // handle_get_reply_card): unknown id → 404. Clone so callers never mutate
    // mock state.
    return structuredClone(findReplyCard(id));
  },

  async getReplyCardCount(): Promise<ReplyCardCounts> {
    // Same rule as the server's count endpoint: `waiting` is the nav badge
    // (answered never counts it); `answered` is the recently-answered (24h)
    // count the 等我回覆 page uses for its collapsed header, matching the
    // listReplyCards("answered") window.
    const cutoff = Date.now() / 1000 - 24 * 3600;
    return {
      waiting: replyCards.filter((c) => c.status === "waiting").length,
      answered: replyCards.filter(
        (c) =>
          c.status === "answered" &&
          c.answeredTs !== null &&
          c.answeredTs >= cutoff,
      ).length,
      expired: replyCards.filter(
        (c) => c.status === "expired" && (c.expiredTs ?? 0) >= cutoff,
      ).length,
    };
  },

  async getChatUnreadCount(): Promise<number> {
    // The owner's TOTAL unread across every peer — computed live, same
    // watermark inverse unreadCountOf / the roster's unread_count uses (a
    // message counts when it is addressed to the owner and newer than the
    // owner's read watermark for that peer).
    return chatLog.filter(
      (m) =>
        m.to === MOCK_OWNER_ID &&
        m.ts > (chatReads.get(`${MOCK_OWNER_ID}::${m.from}`)?.lastReadTs ?? 0),
    ).length;
  },

  async answerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput,
  ): Promise<ReplyCard> {
    // The one-shot close (mirrors handle_answer_reply_card): only a WAITING
    // card is answerable — already answered → 409 (revise via re-answer);
    // empty / out-of-range → 400. Any real answer — including a typed
    // counter-question — closes the card.
    const card = findReplyCard(id);
    if (card.status !== "waiting") {
      throw mockApiError(
        `http 409 for POST /api/reply-cards/${id}/answer`,
        409,
        `reply card '${id}' is already answered`,
      );
    }
    validateReplyAnswer(card, answer);
    const stamp = Date.now();
    card.status = "answered";
    card.answeredTs = stamp / 1000;
    card.answer = toStoredReplyAnswer(answer, stamp);
    emitTopic("reply_card");
    return structuredClone(card);
  },

  async reanswerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput,
  ): Promise<ReplyCard> {
    // 重新決定 (mirrors handle_reanswer_reply_card): only an ANSWERED card is
    // revisable (waiting → 409 — answer it first); the answer is replaced
    // wholesale, answeredTs re-stamps, status STAYS answered (a revision never
    // reopens the card or re-counts the badge).
    const card = findReplyCard(id);
    if (card.status !== "answered") {
      throw mockApiError(
        `http 409 for PUT /api/reply-cards/${id}/answer`,
        409,
        `reply card '${id}' is not answered yet`,
      );
    }
    validateReplyAnswer(card, answer);
    const stamp = Date.now();
    card.answeredTs = stamp / 1000;
    card.answer = toStoredReplyAnswer(answer, stamp);
    emitTopic("reply_card");
    return structuredClone(card);
  },

  async expireReplyCard(id: string): Promise<ReplyCard> {
    // 標為過期 (mirrors handle_expire_reply_card): only a WAITING card can
    // expire — answered/expired → 409; terminal, NOT an answer (the answer
    // stays null). ⚠️ The server has one rung this mock does not: since T-1b88
    // it refuses a caller who is not the card's author with 403 BEFORE it looks
    // at the status. It is not mirrored because the rung is unreachable from
    // here — the cockpit is always the owner, and this mock has no caller
    // identity to refuse. Adding an identity concept to express it would be
    // inventing behaviour the cockpit does not have; the 403 is pinned
    // server-side instead. Releasing the task/step hold mirrors the server's
    // releaseCardHold: the bound step returns to in_progress, and the task
    // follows unless another waiting card still holds it; a terminal task is
    // left untouched (the orphan exit).
    const card = findReplyCard(id);
    if (card.status !== "waiting") {
      throw mockApiError(
        `http 409 for POST /api/reply-cards/${id}/expire`,
        409,
        `reply card '${id}' is already ${card.status} — only a waiting card can expire`,
      );
    }
    card.status = "expired";
    card.expiredTs = Date.now() / 1000;
    for (const task of tasks) {
      const step = task.steps.find((st) => st.replyCardId === id);
      if (!step) continue;
      if (TERMINAL_TASK_STATUSES.has(task.status)) break; // orphan: closed task untouched
      if (step.status === "waiting_owner") step.status = "in_progress";
      const anotherWaiting = replyCards.some(
        (c) =>
          c.id !== id &&
          c.status === "waiting" &&
          task.steps.some((st) => st.replyCardId === c.id),
      );
      if (task.status === "waiting_owner" && !anotherWaiting) {
        task.status = "in_progress";
      }
      emitTopic("task");
      break;
    }
    emitTopic("reply_card");
    return structuredClone(card);
  },

  async listTasks(opts?: {
    open?: boolean;
    statuses?: string[];
  }): Promise<TaskView[]> {
    // The LIGHT list (mirrors GET /api/tasks — partitioning / ordering /
    // filtering are the FE's). Strip the heavy steps/description the real light
    // projection omits, so the mock exercises the same "hydrate on expand via
    // getTask" path as the server. Clone so callers never mutate. `open`
    // (T-2b9d) drops the terminal rows, mirroring ?open=true byte-for-byte in
    // behaviour so mock and http agree.
    // `statuses` (T-a3e4) is the SET form, ANDed with `open` exactly as the
    // server ANDs them — including the one rule that is not a plain status
    // comparison: `reassigning` in the set matches the handover LOCK (T-9ca5),
    // never the status column. Getting that wrong here would let a mock-backed
    // test go green against a server that hides every 轉派中 row.
    const wanted = new Set(opts?.statuses ?? []);
    const rows = tasks.filter((t) => {
      if (opts?.open && TERMINAL_TASK_STATUSES.has(t.status)) return false;
      if (wanted.size === 0) return true;
      if (wanted.has(t.status)) return true;
      // The lock counts only while the task is OPEN — terminate never clears it
      // (T-a3e4). Same rule as the server's taskStatusSetMatch and TasksPage's
      // matchesStatus; all three are deliberately identical.
      return (
        wanted.has("reassigning") &&
        t.lock === "reassigning" &&
        !TERMINAL_TASK_STATUSES.has(t.status)
      );
    });
    // dep_tasks (T-a3e4): resolved against the WHOLE mock population, like the
    // server's single-query join — so a filtered list still names a dep the
    // filter excluded. A dep with no task keeps its derived number and stays
    // title/status-less (the card's 查無此任務 row).
    const byId = new Map(tasks.map((t) => [t.id, t]));
    return structuredClone(rows).map((t) => ({
      ...t,
      steps: [],
      description: "",
      depTasks: (t.deps ?? []).map((id) => {
        const dep = byId.get(id);
        return {
          id,
          taskNo: dep?.taskNo ?? deriveMockTaskNo(id),
          title: dep?.title ?? "",
          status: dep?.status ?? "",
        };
      }),
      // Light list parity (T-3dc5): no artifact rows, only the count (the
      // server's grouped COUNT) — the collapsed card's 「產物 N」 badge.
      artifacts: [],
      artifactCount: (t.artifacts ?? []).length,
    }));
  },

  async getTask(id: string): Promise<TaskView> {
    // Mirrors GET /api/tasks/{id}: the FULL task (steps + description) the
    // light list omits — the per-card expand hydration path. reply_card_status
    // is a read-time join per step (server parity), never stored.
    const task = structuredClone(findTask(id));
    return {
      ...task,
      // TaskDTO carries no dep_tasks (T-a3e4 put the dep join on the LIGHT list
      // only) — drop it so a mock-mode detail cannot be richer than the wire's.
      depTasks: undefined,
      steps: task.steps.map((st) => ({
        ...st,
        replyCardStatus: mockReplyCardStatusOf(st.replyCardId || null),
      })),
      // Full task carries the artifact INDEX (T-66: id + label per deliverable);
      // count kept == length (server parity). The full rows are listTaskArtifacts.
      //
      // 🔴 PROJECTED, not passed through. The store row holds each artifact
      // whole, and a `TaskArtifactView` is structurally a `TaskArtifactRefView`
      // too — so handing the stored row straight back type-checks and would
      // quietly make mock mode the ONE place a task read carries url / mime /
      // filename. The cockpit would then render from the task read here and
      // 404 against a real server. The mapper is the guard, so the mock has to
      // narrow exactly like it does.
      artifacts: (task.artifacts ?? []).map((a) => ({ id: a.id, label: a.label })),
      artifactCount: (task.artifacts ?? []).length,
    };
  },

  async getTaskStep(taskId: string, stepId: string): Promise<TaskStepDetailView> {
    // Mirrors GET /api/tasks/{task_id}/steps/{step_id} (T-66): ONE step, note
    // text included, and NOTHING of the task.
    //
    // 🔴 A step that is not on the named task is a NOT-FOUND here too, not the
    // other task's step. The server answers 404 for it, and a mock that happily
    // resolved a step id against the whole store would let a cockpit bug that
    // mixes task and step ids look correct in mock mode and 404 only in front
    // of a real server.
    //
    // The mock task fixtures carry no step notes (they never have), so `note`
    // is "" and `noteSizeChars` 0 — which is why no 備註 entry renders in mock
    // mode. That is the honest projection of the fixtures, not a stub: a mock
    // that invented note text would make the fetch-on-open path look exercised
    // when the fixtures say there is nothing to open.
    const task = findTask(taskId);
    const step = task.steps.find((s) => s.id === stepId);
    if (!step) {
      throw mockApiError(
        `http 404 for /api/tasks/${taskId}/steps/${stepId}`,
        404,
        `step '${stepId}' not found`
      );
    }
    return {
      ...structuredClone(step),
      replyCardStatus: mockReplyCardStatusOf(step.replyCardId || null),
      detailLevel: "full",
      note: "",
      noteSizeChars: 0,
      noteCapChars: 4000,
    };
  },

  async listTaskArtifacts(taskId: string): Promise<TaskArtifactView[]> {
    // Mirrors GET /api/tasks/{task_id}/artifacts (T-66): the WHOLE ticket's
    // deliverables, each in full, in one call.
    //
    // 🔴 THE ROWS COME OFF THE TASK, and a `[]` here would be a lie the reader
    // can see. This used to `return []` under a comment claiming the mock
    // fixtures never carry artifacts — false in this very file: `getTask`
    // reads `task.artifacts`, `removeTaskArtifact` writes it, and
    // `__injectMockTask` lands whole sets. The visible effect of the lie was
    // the one thing TaskArtifactsPopover says it must never do: the badge
    // saying 「產物 N」 over a panel saying 「還沒有產物」.
    //
    // Deliberately UNFILTERED and unpaged: the server's handler answers the
    // whole ticket's set in one call, which is the shape the panel opens onto.
    // Cloned so a caller mutating a row cannot reach into the store.
    //
    // `findTask` still runs, so an unknown task id is a not-found here exactly
    // as it is against a real server — never a silent [].
    const task = findTask(taskId);
    return structuredClone(task.artifacts ?? []);
  },

  async getTaskCount(): Promise<TaskCountView> {
    // Open (non-terminal) count — computed live, same rule as the server's
    // count endpoint (done/terminated never count) — plus the unfiltered total
    // (T-a3e4), which is every task INCLUDING the terminal ones.
    return {
      open: tasks.filter((t) => !TERMINAL_TASK_STATUSES.has(t.status)).length,
      total: tasks.length,
    };
  },

  async terminateTask(id: string): Promise<TaskView> {
    // Mirrors handle_terminate_task: the only status change that does not go
    // through the task's own step reports; non-terminal only (done/terminated →
    // 409). Stamps closedTs and releases any bound outsource worker (the live
    // list drops it — the card's 外包 display honestly falls back to the bare
    // label).
    //
    // ⚠️ NOT owner-only since T-b56e (owner 2026-08-20, card rc-b896e3f641e7):
    // the task's own executor may terminate it too, unless that executor is an
    // outsource worker. This mock has no caller identity, so it cannot model
    // the gate — it answers as the cockpit's owner token always did.
    const t = findTask(id);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw mockApiError(
        `http 409 for POST /api/tasks/${id}/terminate`,
        409,
        `task '${id}' is already closed`,
      );
    }
    t.status = "terminated";
    t.closedTs = Date.now() / 1000;
    t.updatedTs = t.closedTs;
    outsourceWorkers = outsourceWorkers.filter((w) => w.taskId !== id);
    emitTopic("task");
    emitTopic("outsource_worker");
    return structuredClone(t);
  },

  async markTaskDuplicate(id: string, duplicateOf: string): Promise<TaskView> {
    // Mirrors handle_mark_task_duplicate (T-02c9): mark the task a duplicate of
    // the original and close it. Keeps the depth-1 graph — the target must
    // exist, not be itself, not be itself duplicated, and this task must not
    // already be an original of another duplicate. Non-terminal only.
    const t = findTask(id);
    const conflict = (detail: string) =>
      mockApiError(`http 409 for POST /api/tasks/${id}/duplicate`, 409, detail);
    if (!duplicateOf.trim()) {
      throw mockApiError(
        `http 422 for POST /api/tasks/${id}/duplicate`,
        422,
        "duplicate_of must not be blank",
      );
    }
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw conflict(`task '${id}' is already closed (${t.status})`);
    }
    if (duplicateOf === id) {
      throw conflict("a task cannot be marked a duplicate of itself");
    }
    const original = tasks.find((x) => x.id === duplicateOf);
    if (!original) {
      throw mockApiError(
        `http 404 for POST /api/tasks/${id}/duplicate`,
        404,
        `duplicate_of task '${duplicateOf}' not found`,
      );
    }
    if (original.status === "duplicated") {
      throw conflict(
        `duplicate_of task '${duplicateOf}' is itself a duplicate; point at the ` +
          `final original it duplicates (${original.duplicateOf})`,
      );
    }
    if (tasks.some((x) => x.duplicateOf === id)) {
      throw conflict(
        `task '${id}' is already the original of another duplicate; it cannot ` +
          `itself be marked duplicated`,
      );
    }
    t.status = "duplicated";
    t.duplicateOf = duplicateOf;
    t.waitingReason = "";
    t.closedTs = Date.now() / 1000;
    t.updatedTs = t.closedTs;
    outsourceWorkers = outsourceWorkers.filter((w) => w.taskId !== id);
    emitTopic("task");
    emitTopic("outsource_worker");
    return structuredClone(t);
  },

  async updateTaskDescription(
    id: string,
    description: string,
  ): Promise<TaskView> {
    // Mirrors HandleUpdateTaskDescription... (T-e271) rule for rule, because a
    // mock that is more permissive than the server lets a component pass here
    // and fail in production — and one that is stricter invents a refusal the
    // owner will never actually meet:
    //
    //   * unknown task -> 404 (findTask);
    //   * NO terminal check, deliberately. A closed task is editable and this
    //     is the one place the asymmetry with the artifact set is easy to
    //     "tidy up" by accident. Adding a 409 here would make the cockpit's
    //     tests agree with each other and disagree with the server.
    //   * an unchanged text is a no-op: nothing versioned, no `task` delta.
    //     The server compares before writing for the same reason.
    //
    //   * the value is TRIMMED before it is stored, and the unchanged
    //     comparison runs AFTER the trim — so re-sending a description with a
    //     stray trailing space is no change: nothing versioned, no `task`
    //     delta. 🔴 T-646a (owner card rc-0fb94a25a8a8, option ①) added this;
    //     before it, this field was stored raw and the title's twin trimmed,
    //     which is the drift that ticket existed to remove. A CONSEQUENCE worth
    //     knowing before you "simplify" it: a description of only whitespace
    //     trims to "" and therefore CLEARS.
    //
    // The absent-vs-empty distinction lives one layer up (the wire's optional
    // `description`); this seam's argument is always a concrete string, so ""
    // means clear, exactly as the http twin sends it.
    const t = findTask(id);
    const trimmed = description.trim();
    if (t.description === trimmed) return t;
    recordDocumentHistory("task_description", id);
    t.description = trimmed;
    t.updatedTs = Date.now() / 1000;
    emitTopic("task");
    return t;
  },

  async updateTaskTitle(id: string, title: string): Promise<TaskView> {
    // Mirrors HandleUpdateTaskTitle... (T-2ebe) rule for rule, in the same
    // guard ORDER the server uses — 404 → (403, which never arises in the
    // owner's cockpit) → 400 blank → write:
    //
    //   * unknown task -> 404 (findTask);
    //   * NO terminal check, deliberately — a closed task is editable, exactly
    //     as on the description twin;
    //   * a BLANK title (empty or whitespace-only) -> 400 `title must not be
    //     blank`. 🔴 This is the one rule that parts company with the twin, in
    //     which "" CLEARS the field. A mock that cleared here would let the
    //     cockpit ship a wipe the server refuses;
    //   * the value is TRIMMED before it is stored, matching create_task, and
    //     the unchanged comparison runs AFTER the trim — so re-sending a title
    //     with a stray trailing space is no change: nothing versioned, no
    //     `task` delta.
    const t = findTask(id);
    const trimmed = title.trim();
    if (trimmed === "") {
      throw mockApiError(
        `http 400 for POST /api/tasks/${id}/title`,
        400,
        "title must not be blank",
      );
    }
    if (t.title === trimmed) return t;
    recordDocumentHistory("task_title", id);
    t.title = trimmed;
    t.updatedTs = Date.now() / 1000;
    emitTopic("task");
    return t;
  },

  async setTaskPriority(id: string, priority: string): Promise<void> {
    // Mirrors handle_set_task_priority: closed → 409; the closed high|mid|
    // low|frozen vocabulary → 422 otherwise (freeze/unfreeze ride this knob).
    // T-0786: the server also enforces executor/frozen authz rules for
    // non-owner callers — the mock is the owner's cockpit view, so those
    // 403 faces never arise here.
    const t = findTask(id);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw mockApiError(
        `http 409 for POST /api/tasks/${id}/priority`,
        409,
        `task '${id}' is closed`,
      );
    }
    if (!["high", "mid", "low", "frozen"].includes(priority)) {
      throw mockApiError(
        `http 422 for POST /api/tasks/${id}/priority`,
        422,
        "priority must be one of ['frozen', 'high', 'low', 'mid']",
      );
    }
    t.priority = priority;
    t.updatedTs = Date.now() / 1000;
    emitTopic("task");
  },

  async reassignTask(id: string, input: TaskReassignInput): Promise<TaskView> {
    // Mirrors handle_reassign_task (T-160e): expire the task's waiting cards,
    // rewind non-terminal steps to pending, dismiss the OLD outsource worker,
    // mint the new one when the target is 外包, move the task to `reassigning`
    // and notify BOTH member sides to hand over. The NEW executor reports the
    // task back to in_progress — the mock never flips it here either.
    const t = findTask(id);
    const badRequest = (detail: string) =>
      mockApiError(`http 400 for POST /api/tasks/${id}/reassign`, 400, detail);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw mockApiError(
        `http 409 for POST /api/tasks/${id}/reassign`,
        409,
        `task '${id}' is already closed (${t.status})`,
      );
    }
    // 🔴 NO frozen guard here, mirroring the server (owner ruling 2026-08-11,
    // T-b9f6). The mock used to 400 with 「is frozen; unfreeze it before
    // reassigning」 exactly as the handler did; both were removed together —
    // a mock that keeps a refusal the server dropped makes the cockpit's tests
    // green against a server that no longer exists.

    const target = input.target;
    let newMember: WireMember | undefined;
    let newWorker: OutsourceWorkerView | undefined;
    if (target.kind === "member") {
      if (!target.memberId.trim()) {
        throw badRequest("target.member_id is required for kind 'member'");
      }
      const m = wireMembers.find((x) => x.id === target.memberId);
      if (!m || m.roster_status !== "active") {
        throw badRequest(
          `target member '${target.memberId}' is not an active roster member`,
        );
      }
      if (m.kind === "warden") {
        throw badRequest(
          `target member '${target.memberId}' is a machine (warden) — machines never execute tasks`,
        );
      }
      if (t.executorKind === "member" && t.executorId === target.memberId) {
        throw mockApiError(
          `http 409 for POST /api/tasks/${id}/reassign`,
          409,
          `member '${target.memberId}' is already the task's executor`,
        );
      }
      newMember = m;
    } else {
      const effort = target.effort.trim() || "medium";
      if (!["low", "medium", "high", "max"].includes(effort)) {
        throw badRequest("target.effort must be one of low, medium, high, max");
      }
      // The machine preference is a SPAWN-time knob with no mock surface (no
      // scheduler here) — validated by the server, dropped honestly here.
      newWorker = {
        id: `ow-mock-${Date.now().toString(16)}`,
        codename: deriveCodename(
          target.model.trim(),
          outsourceWorkers.map((w) => w.codename),
        ),
        model: target.model.trim(),
        effort,
        status: "assigned",
        taskId: t.id,
        taskTitle: t.title,
        // The worker's task-status echo is the DERIVED status (T-9ca5); the
        // reassigning handover rides task.lock, not this field.
        taskStatus: "in_progress",
        createdTs: Date.now() / 1000,
      };
    }

    const stamp = Date.now();
    const oldKind = t.executorKind;
    const oldExecutor = t.executorId;

    // The question was addressed to the OLD executor, so its eventual answer is
    // no longer reliable: expire every waiting card the task holds (the new
    // executor re-opens one if it still matters). The mock's card→task linkage
    // rides the steps' replyCardId.
    for (const c of replyCards) {
      if (c.status !== "waiting") continue;
      if (!t.steps.some((st) => st.replyCardId === c.id)) continue;
      c.status = "expired";
      c.expiredTs = stamp / 1000;
      emitTopic("reply_card");
    }
    for (const st of t.steps) {
      if (TERMINAL_STEP_STATUSES.has(st.status)) continue;
      st.status = "pending";
    }
    // T-ba04: the OLD outsource worker is NO LONGER dismissed here — it stays
    // live through the `reassigning` hold so the successor can hand over WITH
    // it; the server fires it only when the successor reports the takeover
    // (reassigning→in_progress) or the timeout reaper gives up. The FE cockpit
    // has no takeover action (agents flip the status via MCP), so the mock has
    // no surface to model that dismiss — the predecessor simply persists here,
    // which is exactly the reassigning-window state.
    if (newWorker) {
      outsourceWorkers.push(newWorker);
      t.executorKind = "outsource";
      t.executorId = newWorker.id;
    } else if (newMember) {
      t.executorKind = "member";
      t.executorId = newMember.id;
    }
    // T-9ca5: `reassigning` is now an ORTHOGONAL LOCK, not a status. The task
    // keeps an honest DERIVED status (every live step just rewound to pending →
    // the task is in flight through the handover = in_progress) and carries the
    // reassigning lock until the new executor claims it (no cockpit surface for
    // that claim, so the mock leaves the lock standing — the reassigning-window
    // state). Leaving waiting_external clears its reason.
    t.lock = "reassigning";
    t.status = "in_progress";
    t.waitingReason = "";
    // Stamp the PREDECESSOR (T-ba04) so the 任務卡 can render the 前任 row and the
    // successor knows who to hand over with. Only when there WAS a prior executor.
    if (oldExecutor) {
      t.reassignedFrom = oldExecutor;
      t.reassignedFromKind = oldKind;
    }
    t.updatedTs = stamp / 1000;

    // Handover PAIRING notices (T-ba04): SERVER-authored (from="system", not the
    // owner), pairing predecessor and successor into a handover DIALOGUE. The
    // predecessor notice fires for a member OR outsource predecessor (the
    // outsource one is now kept live), the successor notice for a member OR a
    // freshly-minted worker (whose boot context ALSO folds the same instruction).
    // 🔴 THE SUCCESSOR'S LABEL IS GONE (T-6f44). The predecessor notice no longer
    // names who took the task, so there is nothing left to label — and that is
    // what killed the fabricated 「外包（待排程指派）」 placeholder: an outsource
    // successor is minted by the scheduler LATER, so at reassign time there was
    // nobody to name and a hardcoded status string sat in a person's grammatical
    // slot. Only the id survives, and only to address the message.
    const newExecutorId = newMember ? newMember.id : newWorker!.id;
    if (oldExecutor) {
      chatLog.push({
        id: `mock-reassign-old-${stamp}`,
        from: "system",
        to: oldExecutor,
        // 🔴 T-6f44：逐字跟著 seeds/task_reassign_predecessor.md。owner 2026-08-24
        // 拿掉了接手人的身分（「讓他自己去查」「不管是不是 outsource」），本體也
        // 改成「先把交接寫到票上，對接是 nice-to-have」—— 因為外包接手人是排程器
        // 之後才生的，那段對話可能永遠不會發生，唯一留得住的是寫在票上的字。
        body:
          `[${t.taskNo}] 此任務已轉派給新的接手人。` +
          `請停止推進，先把交接資訊寫到這張任務上：目前進度、進行中的事項、有哪些雷要注意。` +
          `**這一步不能省，它是接手人唯一保證讀得到的東西** —— 接手人可能還沒被建出來，也可能你已經下線了才輪到他。\n\n` +
          `寫完就算交出去了。如果接手人剛好在線上來找你，就順便當面補齊；沒有的話不用等，也不用去找他。`,
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    }
    // `input.note` is deliberately NOT read here any more (T-6f44 / rc-0c36d8739b8f):
    // the handover note lives on the TASK and rides its DTO; stapling a copy under
    // the notice was the second one, and it was that copy which made these two
    // documents unsplittable.
    if (oldExecutor) {
      const predecessorLabel =
        oldKind === "outsource"
          ? `外包 ${outsourceWorkers.find((w) => w.id === oldExecutor)?.codename ?? oldExecutor}`
          : wireMembers.find((m) => m.id === oldExecutor)?.name || oldExecutor;
      chatLog.push({
        id: `mock-reassign-new-${stamp}`,
        from: "system",
        to: newExecutorId,
        // 🔴 T-6f44：這段逐字跟著 seeds/task_takeover_with_predecessor.md 走。
        // {title} 走了（票號已經指名那張票），前任的名字與 id 併成一個 slot，
        // 而 {note} 早就不再附在通知裡（交接備註只留在任務上）。這裡是每個前端
        // 測試看到的替身，它演的形狀跟出貨的不一樣，那些測試就是在一個不存在的
        // 世界裡綠 —— 上一版正是這樣，舊句子在這裡活到了 T-6f44 的收官掃描。
        body:
          `[${t.taskNo}] 你接手了這張任務，你的前任是 ${predecessorLabel}（${oldExecutor}）。` +
          `請先跟他確認交接完成（直接 post_chat 給他，問清楚目前進度與進行中的事項），` +
          `確認後再由你自己呼叫 claim_task（認領）解除轉派鎖——只有你這個新負責人動得了；任務狀態一律照步驟推導，不必也不能自己報。`,
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    } else if (newMember) {
      chatLog.push({
        id: `mock-reassign-new-${stamp}`,
        from: "system",
        to: newMember.id,
        // Same ruling as its sibling above (T-6f44): no {title}, no {note}.
        body:
          `[${t.taskNo}] 你接手了這張任務。請先讀任務內容，` +
          `準備好後由你自己呼叫 claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。`,
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    }
    emitTopic("task");
    emitTopic("outsource_worker");
    return structuredClone(t);
  },

  async removeTaskArtifact(taskId: string, artifactId: string): Promise<void> {
    // Mirrors handle_remove_task_artifact (T-3dc5): the owner/admin un-pin.
    // Closed task → 409 (T-2654: the deliverable set is frozen in EVERY
    // direction, so un-pin is refused exactly like add and replace). Unknown artifact →
    // 404, wrong-task ownership → 400. The blob is left intact (the mock has no
    // blob store to touch). The 409 must come BEFORE the artifact lookup, same
    // as the server — a mock that deletes where production refuses is worse
    // than no mock at all, since the mock cockpit is how UI changes get checked.
    const t = findTask(taskId);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw mockApiError(
        `http 409 for DELETE /api/tasks/${taskId}/artifact/${artifactId}`,
        409,
        `task '${taskId}' is closed (${t.status}) — its deliverables are frozen`,
      );
    }
    const arts = t.artifacts ?? [];
    const art = arts.find((a) => a.id === artifactId);
    if (!art) {
      throw mockApiError(
        `http 404 for DELETE /api/tasks/${taskId}/artifact/${artifactId}`,
        404,
        `artifact '${artifactId}' not found`,
      );
    }
    t.artifacts = arts.filter((a) => a.id !== artifactId);
    t.artifactCount = t.artifacts.length;
    // Server parity (T-60): un-pinning deletes the artifact's retained versions
    // in the SAME transaction. A mock that kept them would let a version list
    // outlive the artifact it belongs to — a state production cannot reach.
    artifactVersions.delete(artifactId);
    emitTopic("task");
  },

  async listTaskArtifactVersions(
    taskId: string,
    artifactId: string,
  ): Promise<TaskArtifactVersionView[]> {
    // Mirrors handle_list_task_artifact_history (T-60): the retained PREVIOUS
    // versions, newest first. An artifact that has never been replaced answers
    // [] (the honest "nothing has been replaced here"), never a 404. Read-only:
    // there is no restore verb to pair with it.
    //
    // KNOWN DIVERGENCE from the server's guard ladder: the server answers
    // unknown task 404 → unknown artifact 404 → artifact-on-a-different-task
    // 400, while this mock only ever looks inside the named task's own
    // artifacts, so a real artifact id belonging to ANOTHER task comes back 404
    // here and 400 there. No mock artifact verb models that 400, so do not
    // build a test of the 400 branch on this stub.
    const t = findTask(taskId);
    const art = (t.artifacts ?? []).find((a) => a.id === artifactId);
    if (!art) {
      throw mockApiError(
        `http 404 for GET /api/tasks/${taskId}/artifact/${artifactId}/history`,
        404,
        `artifact '${artifactId}' not found`,
      );
    }
    return structuredClone(artifactVersions.get(artifactId) ?? []);
  },

  async postTaskMessage(id: string, msg: TaskMessageInput): Promise<void> {
    // Mirrors handle_post_task_message: ONE ordinary chat message owner →
    // the task's executor (the mock pushes it into the shared chatLog, so the
    // executor's thread shows it exactly like a hand-typed message). An
    // unassigned executor is a 409; an empty message a 400.
    const t = findTask(id);
    if (!t.executorId) {
      throw mockApiError(
        `http 409 for POST /api/tasks/${id}/message`,
        409,
        `task '${id}' has no executor yet`,
      );
    }
    const trimmed = msg.body.trim();
    const hasBody = trimmed.length > 0;
    const hasAtts = (msg.attachments ?? []).length > 0;
    if (!hasBody && !hasAtts) {
      throw mockApiError(
        `http 400 for POST /api/tasks/${id}/message`,
        400,
        "a message needs a body or attachments",
      );
    }
    const stamp = Date.now();
    // Server parity: the stored body is the TRIMMED text prefixed with the
    // task's display number so the executor sees which task the ruling is
    // about (an attachment-only message keeps the empty body — no prefix).
    const body = hasBody ? `[${t.taskNo}] ${trimmed}` : trimmed;
    chatLog.push({
      id: `mock-task-msg-${stamp}`,
      from: MOCK_OWNER_ID,
      to: t.executorId,
      body,
      ts: stamp / 1000,
      attachments: [],
      replyCardId: null,
    });
  },

  async listOutsourceWorkers(): Promise<OutsourceWorkerView[]> {
    // LIVE workers only (a terminate releases + drops the bound worker above).
    // unreadCount is computed LIVE with the same watermark-inverse rule the
    // roster's unread_count uses (http parity: the server injects it on the
    // wire DTO) — tests inject a worker→owner message via __injectMockChat.
    return structuredClone(outsourceWorkers).map((w) => ({
      ...withWorkerTaskJoin(w),
      unreadCount: unreadCountOf(w.id),
    }));
  },

  async getOutsourceWorker(id: string): Promise<OutsourceWorkerView> {
    // The single-worker read (T-f190) — the SAME projection the list serves.
    // Unknown → 404, matching the http adapter (the panel self-heals to the
    // roster). Live unread is computed the same way as the list.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w) {
      throw mockApiError(
        `http 404 for GET /api/outsource-workers/${id}`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async relocateWorker(
    id: string,
    machineId: string,
  ): Promise<OutsourceWorkerView> {
    // 改機器 (T-f190). The mock has no scheduler, so it models the SERVER's
    // observable outcome honestly: write the owner-pinned desired_machine_id and,
    // for a CONCRETE machine id, reflect it as the new `machine` (the dispatch
    // the server would perform), resolving the id to its display name from the
    // machine registry. "" carries no target → `machine` is left untouched (never
    // fabricated). A released / unknown worker → 404, matching
    // the owner-only server handler.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/relocate`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    w.desiredMachineId = machineId;
    if (machineId !== "") {
      const m = wireMembers.find(
        (x) => x.id === machineId && x.kind === "warden",
      );
      // A concrete pin the server would reject (unknown id) still 404s there;
      // here the picker only offers real online machines, so resolve honestly.
      w.machine = m ? m.name : machineId;
    }
    emitTopic("outsource_worker");
    // Mock ↔ http parity (T-ed79 #5): a LIVE worker's move is deferred to its
    // 收口, so the answer says "scheduled, not landed" AND says which of the two
    // kinds of not-landed it is. A worker with no live session is dispatched now
    // and carries neither flag.
    const deferred = w.presence === "online";
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
      ...(deferred
        ? { relocationPending: true, relocationDeferred: true }
        : {}),
    };
  },

  async refocusWorker(id: string): Promise<OutsourceWorkerView> {
    // 換手 (T-32e1). The mock models the server's observable outcome: online-only
    // (409 unless presence "online"), stopped → 409, unknown/released → 404. On
    // success stamp refocus_since (the panel's 換手中 acknowledgement); the actual
    // kill+respawn is server-side, invisible here, so lifecycle is left untouched.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/refocus`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    if (w.desiredState === "offline") {
      throw mockApiError(
        `http 409 for POST /api/outsource-workers/${id}/refocus`,
        409,
        "worker is stopped — restart it before refocusing",
      );
    }
    if (w.presence !== "online") {
      throw mockApiError(
        `http 409 for POST /api/outsource-workers/${id}/refocus`,
        409,
        "refocus requires the worker to be online",
      );
    }
    w.refocusSince = Date.now() / 1000;
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async stopWorker(id: string): Promise<OutsourceWorkerView> {
    // 停止 (T-f190; a GRACEFUL close-out since T-ed79). Held down: desired_state
    // offline (member parity) and the in-flight refocus cleared — but NO kill.
    // The worker is shown its 〈停止〉 and keeps its session until it reports
    // stopped, so a worker that was online projects "stopping" here, not
    // "stopped". unknown/released → 404. Idempotent.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/stop`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    w.desiredState = "offline";
    w.refocusSince = null;
    w.refocusOp = undefined;
    w.presence = w.presence === "online" ? "stopping" : "stopped";
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async acceleratedStopWorker(id: string): Promise<OutsourceWorkerView> {
    // 加速停止 (T-ed79) — the MIDDLE rung. It escalates a wind-down that is
    // ALREADY open, so its refusal is what makes it an escalation rather than a
    // second stop button; the message names the rungs below it, mirroring the
    // server's acceleratedStopWorkerNeedsAnOpenWindDownMsg.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/accelerated-stop`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    const windingDown =
      (w.desiredState === "offline" && w.presence === "stopping") ||
      (w.refocusSince ?? 0) > 0;
    if (w.presence !== "online" && w.presence !== "stopping") {
      throw mockApiError(
        `http 409 for POST /api/outsource-workers/${id}/accelerated-stop`,
        409,
        "加速停止 requires the worker to be online (no live session to accelerate)",
      );
    }
    if (!windingDown) {
      throw mockApiError(
        `http 409 for POST /api/outsource-workers/${id}/accelerated-stop`,
        409,
        "加速停止 escalates a wind-down that is already open — this worker has not " +
          "been asked to stop. Press 停止 or 重新聚焦 first",
      );
    }
    w.refocusOp = "accelerated_stop";
    // The SAME two arms as the member mock, through the same helper — the
    // server's worker DTO reads the shared `winddownDeadlineOf` since T-14
    // item 3, so a mock that stayed silent here would show the cockpit no
    // countdown where the real wire now carries one.
    const stamps = acceleratedStopStamps(w.desiredState === "offline");
    if (stamps.since !== null) w.refocusSince = stamps.since;
    w.refocusDeadline = stamps.deadline;
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async forceStopWorker(id: string): Promise<OutsourceWorkerView> {
    // 強制停止 (T-ed79) — the THIRD rung, and the body /stop used to have: the
    // session is killed on the spot, so the worker lands in "stopped" directly.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/force-stop`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    w.desiredState = "offline";
    w.refocusSince = null;
    w.refocusOp = undefined;
    w.presence = "stopped";
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async restartWorker(id: string): Promise<OutsourceWorkerView> {
    // 喚醒 (T-f190; the word since T-7526 — the path stays /restart). Inverse of stop: set desired_state back online + re-dispatch.
    // 409 only when the worker is actually ALIVE (T-7526 — see the guard below);
    // unknown/released → 404. The mock reflects the observable re-spawn as presence
    // "waking" (the server re-dispatches; boots afresh).
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/restart`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    // Mock ↔ http parity (T-ed79 #10, owner 2026-08-21 「往正職靠：外包也不擋」):
    // the over-spawn guard is GONE. A live worker is DISPLACED, not refused —
    // the same shape 活化 has always had for a staff member — and the fact that
    // the press found a live session is a receipt on the row instead of a 409.
    if (w.presence === "online") {
      w.lastOp = "start";
      w.lastOpOk = false;
      w.lastOpLog = "";
      w.lastOpReason =
        "session_alive: this worker was still running — 重啟 is replacing that " +
        "session, not starting a first one. If it does not come back, its " +
        "previous session was still holding the slot";
      w.lastOpAt = Date.now() / 1000;
    }
    w.desiredState = "online";
    w.presence = "waking";
    emitTopic("outsource_worker");
    // Mock ↔ http parity (T-ed79 #12): the mock always "dispatches", so it never
    // reports activation_pending. The flag is deliberately NOT faked here — a
    // mock that invents a pending state teaches the panel a story the server
    // only tells in a condition this mock cannot reach (no kill target /
    // unreachable warden).
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async setWorkerModel(
    id: string,
    patch: { model: string; effort?: string },
  ): Promise<OutsourceWorkerView> {
    // 換 model (T-f190). Persist model/effort; the respawn-to-take-effect-now is
    // server-side (invisible here). unknown/released → 404.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw mockApiError(
        `http 404 for POST /api/outsource-workers/${id}/model`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    w.model = patch.model;
    if (patch.effort !== undefined && patch.effort !== "")
      w.effort = patch.effort;
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async getWorkerBootContext(id: string): Promise<string> {
    // Honest preview mirroring the backend buildWorkerBootContext (T-ba6b),
    // never a stored spawn-time text and never a token.
    //
    // 🔴 T-4595 rewrote this. It used to assemble 外包工作守則 → 你的身分 →
    // 你的任務 → 任務手冊, and NONE of those exist any more: a worker's boot
    // context is the STAFF fold (getBootstrap above) MINUS the persona slot —
    // 系統互動 + 使用者自訂 + the boot sequence for the worker's OWN runtime,
    // with not one word written for outsource readers. A mock that keeps the
    // old shape is worse than no mock: the cockpit's tests go green against a
    // fake server that still ships a document the real one deleted.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w) {
      throw mockApiError(
        `http 404 for GET /api/outsource-workers/${id}/boot-context`,
        404,
        `outsource worker ${id} not found`,
      );
    }
    const task = tasks.find((x) => x.id === w.taskId);
    if (!task) {
      throw mockApiError(
        `http 404 for GET /api/outsource-workers/${id}/boot-context`,
        404,
        `task ${w.taskId} not found`,
      );
    }
    // The worker and its bound task are still resolved above, because the two
    // 404s are the contract that tells the panel the row is stale — but the
    // assembled text does not depend on either of them.
    const userText = foldGlobalContext().text;
    // FOLDED, like the staff preview and like the server (T-30e4). This path
    // and getBootstrap were the SAME defect written twice — worth saying out
    // loud, because fixing only the one the ticket named would have left an
    // identical preview lying next door. Unlike the staff preview, the runtime
    // here is real: a spawn names its worker, so buildWorkerBootContext really
    // does branch, and so does this.
    const parts = [foldBootDoc("system_interaction", "global").text.trim()];
    if (userText.trim()) {
      parts.push(`# 使用者自訂（Owner Additions）\n\n${userText.trim()}`);
    }
    parts.push(
      foldBootDoc(
        "boot_sequence",
        w.runtime === "codex" ? "codex" : "claude",
      ).text.trim(),
    );
    return parts.join("\n\n") + "\n";
  },

  async listTaskTypes(): Promise<TaskTypeView[]> {
    // 出廠不含任何類型 (spec §5.1) — honest empty until injected/created.
    // The LIGHT narrowing of the manuals store (same source of truth as the
    // manual editor — mirrors the http adapter reading the same endpoint).
    return taskManuals.map((m) => ({
      typeKey: m.typeKey,
      displayName: m.displayName,
      purpose: m.purpose,
    }));
  },

  async listTaskManuals(): Promise<TaskManualSummaryView[]> {
    // T-1170: the DIRECTORY. The two long documents are DROPPED here, the way
    // the server drops them — a mock that kept serving them would let the
    // manual sub-pages keep reading a list row and stay green.
    return taskManuals.map(({ sopMd: _sop, learnings: _learn, ...row }) =>
      structuredClone(row),
    );
  },

  async getTaskManual(typeKey: string): Promise<TaskManualView> {
    return structuredClone(findTaskManual(typeKey));
  },

  async createTaskManual(displayName: string): Promise<TaskManualView> {
    // Mirrors HandleCreateTaskManualApiTaskManualsPost's T-fa76 system-key
    // path: blank display name → 400; the type_key is MINTED server-side
    // ("tm-"+hex12 — never the user's text), and the created manual is BLANK
    // (spec §5.1). Display names are deliberately NOT unique (role-name
    // parity), so there is no duplicate 409 on this path.
    const name = displayName.trim();
    if (name === "") {
      throw mockApiError(
        "http 400 for POST /api/task-manuals",
        400,
        "display_name must not be blank",
      );
    }
    const manual: TaskManualView = {
      typeKey: `tm-${Array.from({ length: 12 }, () =>
        "0123456789abcdef".charAt(Math.floor(Math.random() * 16)),
      ).join("")}`,
      displayName: name,
      purpose: "",
      fields: [],
      sopMd: "",
      learnings: "",
      assignee: null,
      updatedTs: Date.now() / 1000,
    };
    taskManuals.push(manual);
    emitTopic("task_manual");
    return structuredClone(manual);
  },

  async updateTaskManual(
    typeKey: string,
    patch: TaskManualPatch,
  ): Promise<TaskManualView> {
    // Mirrors handle_update_task_manual: partial — only supplied fields
    // change; assignee is three-valued (omitted = unchanged, null = unset).
    const manual = findTaskManual(typeKey);
    // T-1f39 — SOP and 學習經驗 are versioned INDEPENDENTLY, and only when this
    // write actually changes them; 用途／識別鍵／display_name／assignee are not
    // versioned at all, so a write touching only those retains nothing anywhere
    // (taskManualHistoryStreams). The legacy `task_manual` bundle is retired:
    // nothing writes it, migration 00044 deleted its rows, and both
    // document-history routes now refuse it with 400 (server and mock alike).
    if (patch.sopMd !== undefined && patch.sopMd !== manual.sopMd) {
      recordDocumentHistory("task_manual_sop", typeKey);
    }
    if (patch.learnings !== undefined && patch.learnings !== manual.learnings) {
      recordDocumentHistory("task_manual_learnings", typeKey);
    }
    if (patch.displayName !== undefined) manual.displayName = patch.displayName;
    if (patch.purpose !== undefined) manual.purpose = patch.purpose;
    if (patch.sopMd !== undefined) manual.sopMd = patch.sopMd;
    if (patch.learnings !== undefined) manual.learnings = patch.learnings;
    if (patch.fields !== undefined) {
      manual.fields = structuredClone(patch.fields);
    }
    if (patch.assignee !== undefined) {
      manual.assignee = structuredClone(patch.assignee);
    }
    manual.updatedTs = Date.now() / 1000;
    emitTopic("task_manual");
    return structuredClone(manual);
  },

  async deleteTaskManual(typeKey: string): Promise<void> {
    // Mirrors handle_delete_task_manual: OPEN (non-terminal) tasks of the
    // type block the delete with a 409 (spec §5.1 需先讓那些任務結束).
    findTaskManual(typeKey);
    const open = tasks.some(
      (t) => t.typeKey === typeKey && !TERMINAL_TASK_STATUSES.has(t.status),
    );
    if (open) {
      throw mockApiError(
        `http 409 for DELETE /api/task-manuals/${typeKey}`,
        409,
        `task type '${typeKey}' still has open tasks`,
      );
    }
    taskManuals = taskManuals.filter((m) => m.typeKey !== typeKey);
    // All THREE series go with the manual, the legacy bundle included: a
    // readable revision of a deleted document makes 「永久移除」 false.
    for (const kind of MANUAL_KINDS) dropDocumentHistory(kind, typeKey);
    emitTopic("task_manual");
  },

  async listDocs(): Promise<DocSummaryView[]> {
    return mockDocs.map((d) => ({ slug: d.slug, title: d.title }));
  },

  async getDoc(slug: string): Promise<DocView> {
    const doc = mockDocs.find((d) => d.slug === slug);
    if (!doc) {
      throw mockApiError(
        `http 404 for GET /api/docs/${slug}`,
        404,
        `doc '${slug}' not found`,
      );
    }
    return structuredClone(doc);
  },

  async getMonitoring(): Promise<MonitoringView> {
    // Same seam as members: go through the wire→view mapper so the mock and the
    // real HTTP adapter map identically. Honest null/empty passes through.
    return toMonitoring(structuredClone(wireMonitoring));
  },

  async listMachines(): Promise<MachineView[]> {
    // (bin_status parity: see mockBinStatus below — the registry row carries
    // the same server-computed freshness verdict the real /api/machines does.)
    // The machine registry, DERIVED from the warden members in the roster so it
    // stays consistent with the mock members: each active warden IS a machine.
    // machine_id = the warden member id (the activate/rebind + teardown target);
    // display_name = the warden's display name; online = derived from the warden
    // member's presence. HONEST: it mirrors the member — the mock never fabricates
    // a reachable machine (the seed warden is offline → the picker's 0-online path).
    return (
      wireMembers
        .filter((m) => m.kind === "warden" && m.roster_status !== "removed")
        .map((m): WireMachine => ({
          machine_id: m.id,
          display_name: m.name,
          online: m.presence === "online",
          is_self: m.id === MOCK_SERVER_SELF_ID,
          bin_status: mockBinStatus.get(m.id) ?? null,
          warden_shape: mockWardenShape.get(m.id) ?? null,
          cutover_effect: mockCutoverEffect.get(m.id) ?? null,
          // claude probe columns (T-97ee): same honest-null contract as
          // bin_status — no probe fixture reads as the all-null unknown.
          claude_version: mockClaudeInfo.get(m.id)?.version ?? null,
          claude_cred_source: mockClaudeInfo.get(m.id)?.cred_source ?? null,
          claude_sub_readable: mockClaudeInfo.get(m.id)?.sub_readable ?? null,
        }))
        // The server-self row is ALWAYS first (stable sort keeps the rest in order).
        .sort((a, b) => Number(b.is_self) - Number(a.is_self))
        .map(toMachine)
    );
  },

  async patchAccount(id: string, patch: AliasPatch): Promise<void> {
    // Mutate the demo fixture's display_name so a subsequent refetch shows the
    // new label (mirrors the BE AliasDTO rename; return void, caller refetches).
    if (patch.displayName !== undefined) {
      const a = wireMonitoring.accounts.find((x) => x.account === id);
      if (a) a.display_name = patch.displayName;
    }
  },

  async patchMachine(id: string, patch: AliasPatch): Promise<void> {
    // Rename a machine by machine_id (== the warden member id). The machine
    // registry derives its display name from the warden member, so we update that
    // member's name; a subsequent listMachines reflects the new label. We also
    // keep any monitoring row keyed by this machine's host in sync (harmless if
    // absent). Mirrors the BE AliasDTO rename; return void, caller refetches.
    if (patch.displayName !== undefined) {
      const w = wireMembers.find((m) => m.id === id);
      if (w) w.name = patch.displayName;
      const m = wireMonitoring.machines.find(
        (x) => x.machine === (w?.desired_machine_id ?? id),
      );
      if (m) m.display_name = patch.displayName;
    }
  },

  async onboardMachine(
    displayName: string,
    _opts?: OnboardOptions,
  ): Promise<OnboardResultView> {
    // Fake onboard: the machine is created by DISPLAY NAME ONLY (no host) — the
    // server owns the opaque machine_id. We mint a stable id and push a warden
    // member under it (id === machine_id) so the machine surfaces via listMachines
    // and a later teardown/rename can address it by machine_id. The warden is
    // offline until it reports in (honest — never a fabricated online machine).
    // SECURITY: the token lives only in the returned string; we NEVER console.log
    // it or stash it anywhere the UI would leak.
    const name = displayName.trim();
    const machineId = `m-${Math.random().toString(36).slice(2, 10)}`;
    const token = `mock-warden-token-${Math.random().toString(36).slice(2, 14)}`;
    // Warden exec credentials are permanent on the real server. Keep the
    // legacy request option in the adapter signature for wire compatibility,
    // but do not let it fabricate a finite expiry in the mock.
    // The boot command embeds a short-lived single-use claim code, never the
    // token (mirrors the real POST /api/machines onboard shape).
    const claimCode = `mock-claim-code-${Math.random().toString(36).slice(2, 14)}`;
    const bootCommand = `curl -fsSL 'https://officraft.local/install.sh?code=${claimCode}' | bash`;

    wireMembers.push({
      id: machineId,
      name: name || machineId,
      kind: "warden",
      role_key: "assistant",
      role_name: "",
      runtime: "claude",
      model: "",
      actual_model: "",
      actual_runtime: "",
      actual_effort: "",
      actual_machine: "",
      refocus_op: "",
      refocus_deadline: 0,
      effort: "medium",
      desired_state: "offline",
      desired_machine_id: machineId,
      machine: "", // OBSERVED position: freshly onboarded warden, offline → "—"
      presence: "offline",
      refocus_since: 0,
      last_op: "",
      last_op_ok: null,
      last_op_log: "",
      last_op_reason: "",
      last_op_at: 0,
      forced_stop_at: 0,
      roster_status: "active",
      owner_id: MOCK_OWNER_ID,
      unread_count: 0,
      schema_version: 2,
    });

    const wire: WireOnboardResult = {
      member_id: machineId,
      machine_id: machineId,
      token,
      expires_in: 0,
      boot_command: bootCommand,
      claim_code: claimCode,
      claim_expires_in: 600,
    };
    return toOnboardResult(wire);
  },

  async deleteMachine(memberId: string): Promise<DeleteResultView> {
    // Fake DELETE — a PURE roster soft-delete (delete ≠ uninstall ≠ stop): drop
    // the warden member + its machine row so a refetch reflects the removal. No
    // warden command is dispatched and there is NO teardown_command anymore
    // (mirrors the real MachineDeleteResultDTO {member_id, machine_id, removed}).
    const w = wireMembers.find((m) => m.id === memberId);
    const machineId = w?.desired_machine_id ?? "";
    wireMembers = wireMembers.filter((m) => m.id !== memberId);
    if (machineId) {
      wireMonitoring.machines = wireMonitoring.machines.filter(
        (m) => m.machine !== machineId,
      );
    }
    const wire: WireDeleteResult = {
      member_id: memberId,
      machine_id: machineId,
      removed: true,
    };
    return toDeleteResult(wire);
  },

  async uninstallMachine(memberId: string): Promise<UninstallResultView> {
    // Fake uninstall — write the intent + report `dispatched` honestly by the
    // warden's live online flag (TRUE when online → the real reconcile arm would
    // drive the uninstall RPC; FALSE when already offline → nothing to command).
    // The record is KEPT (re-installable): we do NOT drop the member row. On a
    // dispatched uninstall we flip the warden offline so a refetch shows the
    // machine going offline (mirrors the fold converging to offline on the ok
    // receipt). Mirrors MachineUninstallResultDTO {member_id, machine_id, dispatched}.
    const w = wireMembers.find((m) => m.id === memberId);
    const machineId = w?.desired_machine_id ?? "";
    const dispatched = w?.presence === "online";
    if (w && dispatched) {
      w.presence = "offline";
    }
    const wire: WireUninstallResult = {
      member_id: memberId,
      machine_id: machineId,
      dispatched,
    };
    return toUninstallResult(wire);
  },

  async getMachineBootCommand(_machineId: string): Promise<string> {
    // Fake re-fetch of a machine's boot command: mint a FRESH one-time claim
    // code and return the same operator string format the onboard mock produces
    // (mirrors the real GET /api/machines/{id}/boot-command re-minting a claim
    // code). No machine is created — this re-issues the command for an existing
    // machine. The one-liner carries only the short-lived code, never a token.
    const claimCode = `mock-claim-code-${Math.random().toString(36).slice(2, 14)}`;
    return `curl -fsSL 'https://officraft.local/install.sh?code=${claimCode}' | bash`;
  },

  async bootstrapOnServer(_machineId: string): Promise<BootstrapResultView> {
    // Fake one-click server install: the mock host has no real installer to run,
    // so it reports an honest fixed success (never a fabricated failure). The real
    // backend returns ok=false + the reason in `log` (e.g. the one-warden guard)
    // when the install is refused; the UI surfaces that path unchanged.
    return { ok: true, exitCode: 0, log: "(mock) warden installed on server" };
  },

  async teardownOnServer(machineId: string): Promise<TeardownHereResultView> {
    // Fake one-click server teardown: the mock host has no real launchd to bootout,
    // so it reports an honest fixed success (never a fabricated failure). CONFIRM-
    // THEN-REMOVE: only on ok do we drop the warden member + its machine row (the
    // real backend soft-deletes server-side only when the daemon is confirmed torn
    // down). The real backend returns ok=false + the reason in `log` + removed=false
    // when the local teardown fails; the UI surfaces that path unchanged.
    const w = wireMembers.find((m) => m.id === machineId);
    const host = w?.desired_machine_id ?? "";
    wireMembers = wireMembers.filter((m) => m.id !== machineId);
    if (host) {
      wireMonitoring.machines = wireMonitoring.machines.filter(
        (m) => m.machine !== host,
      );
    }
    return {
      ok: true,
      exitCode: 0,
      log: "(mock) warden torn down on server",
      removed: true,
    };
  },

  async getVersion(): Promise<VersionView> {
    // Honest build identity — the same seam (wire→mapper) as everything else.
    return toVersion(structuredClone(MOCK_WIRE_VERSION));
  },

  async getBackupHealth(): Promise<BackupHealthView> {
    // Same seam (wire fixture → shared mapper) as everything else. The two
    // timestamps are anchored to the CALLING clock so the card's "landed N
    // ago" reads sanely in a long-lived mock session instead of counting up
    // from the epoch.
    const now = Math.floor(Date.now() / 1000);
    const wire = structuredClone(MOCK_WIRE_BACKUP_HEALTH);
    wire.newest_backup_ts = now - (wire.newest_backup_age_secs ?? 0);
    wire.checked_ts = now;
    return toBackupHealth(wire);
  },

  async getSigningKeys(): Promise<SigningKeyView[]> {
    return toSigningKeys({ keys: mockSigningKeys });
  },

  async rotateSigningKey(): Promise<SigningKeyView[]> {
    // A real rotation: ADD a key, move the signing mark, drop nothing — so the
    // mock cannot make the card look right while the server behaviour it
    // stands in for would be wrong.
    for (const k of mockSigningKeys) k.is_signing = false;
    mockSigningKeys.push({
      key_id: `k-${mockSigningKeys.length}${"0123456789abcdef".repeat(2).slice(0, 15)}`,
      created_ts: Math.floor(Date.now() / 1000),
      is_signing: true,
    });
    return toSigningKeys({ keys: mockSigningKeys });
  },

  async removeSigningKey(keyId: string): Promise<SigningKeyView[]> {
    const target = mockSigningKeys.find((k) => k.key_id === keyId);
    // 🔴 THE SAME ENVELOPE THE WIRE RETURNS, not a plain Error. A mock that
    // throws bare prose makes `e.message` carry the reason, so a caller reading
    // the wrong field looks correct in mock mode and shows `http 409 for POST …`
    // against the real server. That is exactly what happened here, and
    // frontend/.claude/rules/data-layer.md requires this envelope for the reason
    // this comment exists.
    if (!target) {
      throw mockApiError(
        `http 404 for POST /api/auth/signing-keys/${keyId}/remove`,
        404,
        `no signing key '${keyId}'`,
      );
    }
    if (target.is_signing) {
      throw mockApiError(
        `http 409 for POST /api/auth/signing-keys/${keyId}/remove`,
        409,
        `key '${keyId}' is the one currently signing and cannot be removed — rotate first, then remove it`,
      );
    }
    mockSigningKeys = mockSigningKeys.filter((k) => k.key_id !== keyId);
    return toSigningKeys({ keys: mockSigningKeys });
  },

  async getAuthStatus(): Promise<AuthStatusView> {
    return { passwordSet: mockPasswordSet, mfaRequired: mockMfaActive };
  },

  async getMfaState(): Promise<MfaStateView> {
    return { offered: mockMfaOffered, enrolled: mockMfaActive };
  },

  async setMfaOffered(offered: boolean): Promise<MfaStateView> {
    // A rollout switch: it never touches mockMfaActive, mirroring the server.
    mockMfaOffered = offered;
    return { offered: mockMfaOffered, enrolled: mockMfaActive };
  },

  async enrollMfa(): Promise<MfaEnrollView> {
    // Same check order as the server: the feature gate first, then an active
    // factor is a 409 before any secret is minted (rotation must disable first).
    if (!mockMfaOffered) {
      throw mockApiError(
        "http 403 for POST /api/auth/mfa/enroll",
        403,
        "the second factor is not enabled on this server",
      );
    }
    if (mockMfaActive) {
      throw mockApiError(
        "http 409 for POST /api/auth/mfa/enroll",
        409,
        "a second factor is already active; disable it first",
      );
    }
    mockMfaPending = true;
    return {
      secret: MOCK_TOTP_SECRET,
      otpauthUri: `otpauth://totp/OffiCraft:owner?secret=${MOCK_TOTP_SECRET}&issuer=OffiCraft&algorithm=SHA1&digits=6&period=30`,
    };
  },

  async activateMfa(password: string, code: string): Promise<void> {
    if (!mockMfaOffered) {
      throw mockApiError(
        "http 403 for POST /api/auth/mfa/activate",
        403,
        "the second factor is not enabled on this server",
      );
    }
    if (mockMfaActive) {
      throw mockApiError(
        "http 409 for POST /api/auth/mfa/activate",
        409,
        "a second factor is already active",
      );
    }
    if (!mockMfaPending) {
      throw mockApiError(
        "http 409 for POST /api/auth/mfa/activate",
        409,
        "no pending enrolment; call /api/auth/mfa/enroll first",
      );
    }
    // BOTH factors, ONE indistinguishable refusal — the server's shape. The
    // pending secret SURVIVES either way: a typo must not force a fresh QR scan.
    if (
      password !== mockPassword ||
      code.replace(/[\s-]/g, "") !== MOCK_TOTP_CODE
    ) {
      throw mockApiError(
        "http 401 for POST /api/auth/mfa/activate",
        401,
        "invalid password or code",
      );
    }
    mockMfaPending = false;
    mockMfaActive = true;
  },

  async disableMfa(password: string, code: string): Promise<void> {
    if (!mockMfaActive) {
      throw mockApiError(
        "http 409 for POST /api/auth/mfa/disable",
        409,
        "no second factor is active",
      );
    }
    // BOTH factors, ONE indistinguishable refusal — the server's shape.
    if (
      password !== mockPassword ||
      code.replace(/[\s-]/g, "") !== MOCK_TOTP_CODE
    ) {
      throw mockApiError(
        "http 401 for POST /api/auth/mfa/disable",
        401,
        "invalid password or code",
      );
    }
    mockMfaActive = false;
    mockMfaPending = false;
  },

  async setPassword(password: string, claimToken: string): Promise<void> {
    // Same check order as the server: already set → 409; claim → 401; then
    // the length rule.
    if (mockPasswordSet) {
      throw mockApiError(
        "http 409 for POST /api/auth/set-password",
        409,
        "a password is already set",
      );
    }
    if (claimToken !== MOCK_CLAIM_TOKEN) {
      throw mockApiError(
        "http 401 for POST /api/auth/set-password",
        401,
        "invalid claim token",
      );
    }
    if (password.length < 8) {
      throw mockApiError(
        "http 422 for POST /api/auth/set-password",
        422,
        "password must be at least 8 characters",
      );
    }
    mockPasswordSet = true;
    mockPassword = password;
  },

  async changePassword(
    currentPassword: string,
    newPassword: string,
  ): Promise<void> {
    if (newPassword.length < 8) {
      throw mockApiError(
        "http 422 for POST /api/auth/change-password",
        422,
        "new_password must be at least 8 characters",
      );
    }
    if (!mockPasswordSet || currentPassword !== mockPassword) {
      throw mockApiError(
        "http 401 for POST /api/auth/change-password",
        401,
        "invalid password",
      );
    }
    mockPassword = newPassword;
  },

  async fetchThemeFromLink(url: string): Promise<string> {
    // Mirrors HandleFetchThemeApiThemeFetchPost (T-29c7) on the only half a
    // mock CAN mirror: the FORMAT refusal. There is no network here, so a
    // well-formed link answers with a canned bundle — a mock that failed every
    // link would make the import box untestable offline, and one that accepted
    // a malformed link would let a component pass here and 422 in production.
    //
    // Like the server, this checks format ONLY and says nothing about where
    // the link points (owner ruling 2026-08-03). The trimmed-and-parsed shape
    // is the same rule: absolute, http/https.
    let parsed: URL;
    try {
      parsed = new URL(url.trim());
    } catch {
      throw mockApiError(
        "http 422 for POST /api/theme/fetch",
        422,
        "url must be an absolute http:// or https:// link",
      );
    }
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw mockApiError(
        "http 422 for POST /api/theme/fetch",
        422,
        "url must be an absolute http:// or https:// link",
      );
    }
    return JSON.stringify(
      {
        id: "custom-linked",
        name: "連結匯入的主題",
        colors: { "--color-bg": "#101018", "--color-accent": "#785af0" },
      },
      null,
      2,
    );
  },

  async listThemes(): Promise<ThemeListItem[]> {
    // GET /api/themes -> id + name ONLY (T-83ef). The mock answers the same
    // narrow row the server does: a mock that handed back whole bundles here
    // would let a component read `colors` off a list row and still pass, then
    // find it undefined in production.
    return [...mockThemes.values()].map((t) => ({ id: t.id, name: t.name }));
  },

  async getTheme(id: string): Promise<ThemeBundle> {
    const found = mockThemes.get(id);
    if (!found) {
      throw mockApiError(
        `http 404 for GET /api/themes/${id}`,
        404,
        "theme not found",
      );
    }
    return structuredClone(found);
  },

  async putTheme(bundle: ThemeBundle): Promise<ThemeWriteReceipt> {
    // Server parity, in the server's order: the path key is the bundle's own
    // id here (the adapter takes only the bundle), so the mismatch 422 is
    // checked against that same identity — it can only fire on a bundle whose
    // id is missing/not a string, which the validator names anyway.
    const err = validateThemeBundle(bundle, "theme");
    if (err) {
      throw mockApiError(
        `http 422 for PUT /api/themes/${bundle?.id}`,
        422,
        err,
      );
    }
    const existing = mockThemes.has(bundle.id);
    if (!existing && mockThemes.size >= MAX_CUSTOM_THEMES) {
      // Creating past the cap is a 422; REPLACING is not capped (server parity).
      //
      // 🔴 The WORDING is parity too, and it is not the array validator's line.
      // `custom_themes must hold at most N themes` is what the whole-array
      // validator says — still reachable through link import, which hands it an
      // array. The per-theme endpoint counts rows instead and speaks to a person
      // (api_themes.go), naming no field: the row it would have to name does not
      // exist on the wire any more. This message is the one the cockpit actually
      // renders when the cap is hit on a device that did not know it was full
      // (the local pre-check catches the ordinary case), so a mock that made up
      // its own phrasing would put words on screen in a test that the owner can
      // never be shown.
      throw mockApiError(
        `http 422 for PUT /api/themes/${bundle.id}`,
        422,
        `at most ${MAX_CUSTOM_THEMES} custom themes may be saved — delete one first`,
      );
    }
    // Map.set on an EXISTING key keeps its insertion position — which is
    // exactly the "a replace does not move the theme to the bottom" rule, so
    // nothing here has to re-implement it.
    mockThemes.set(bundle.id, structuredClone(bundle));
    return {
      id: bundle.id,
      created: !existing,
      orderIdx: [...mockThemes.keys()].indexOf(bundle.id),
      updatedAt: Math.floor(Date.now() / 1000),
    };
  },

  async deleteTheme(id: string): Promise<ThemeDeleteResult> {
    if (!mockThemes.has(id)) {
      throw mockApiError(
        `http 404 for DELETE /api/themes/${id}`,
        404,
        "theme not found",
      );
    }
    mockThemes.delete(id);
    // The coupling the old whole-array settings write used to perform: the
    // ACTIVE theme just stopped existing, so display_theme goes back to "" in
    // this same call and the receipt SAYS so — that flag is the only way the
    // caller learns its theme changed without re-reading settings.
    const displayThemeReset = mockServerSettings.display_theme === id;
    if (displayThemeReset) mockServerSettings.display_theme = "";
    return { id, deleted: true, displayThemeReset };
  },

  async getServerSettings(): Promise<ServerSettingsView> {
    return toServerSettings(structuredClone(mockServerSettings));
  },

  async patchServerSettings(
    patch: ServerSettingsPatch,
  ): Promise<ServerSettingsView> {
    // Validate BOTH fields before writing anything (server parity).
    if (
      patch.ownerTokenTtl !== undefined &&
      !TOKEN_TTL_CHOICES.has(patch.ownerTokenTtl)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "owner_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds",
      );
    }
    if (
      patch.agentTokenTtl !== undefined &&
      !TOKEN_TTL_CHOICES.has(patch.agentTokenTtl)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "agent_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds",
      );
    }
    if (
      patch.handoverPct !== undefined &&
      (patch.handoverPct < 40 || patch.handoverPct > 90)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "handover_pct must be between 40 and 90",
      );
    }
    if (
      patch.codexCompactionThreshold !== undefined &&
      (patch.codexCompactionThreshold < 1 ||
        patch.codexCompactionThreshold > 10)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "codex_compaction_threshold must be between 1 and 10",
      );
    }
    if (
      patch.noticePct !== undefined &&
      (patch.noticePct < 1 || patch.noticePct > 89)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "notice_pct must be between 1 and 89",
      );
    }
    if (
      patch.codexNoticeRound !== undefined &&
      (patch.codexNoticeRound < 1 || patch.codexNoticeRound > 10)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "codex_notice_round must be between 1 and 10",
      );
    }
    // The pair is checked against the POST-PATCH values, exactly like the
    // server: either number may be sent on its own, and what must hold is that
    // the soft notice still lands strictly before the final one.
    {
      const notice = patch.noticePct ?? mockServerSettings.notice_pct;
      const final = patch.handoverPct ?? mockServerSettings.handover_pct;
      if (notice >= final) {
        throw mockApiError(
          "http 422 for PATCH /api/settings",
          422,
          "notice_pct must be strictly below handover_pct",
        );
      }
      const noticeRound =
        patch.codexNoticeRound ?? mockServerSettings.codex_notice_round;
      const finalRound =
        patch.codexCompactionThreshold ??
        mockServerSettings.codex_compaction_threshold;
      if (noticeRound >= finalRound) {
        throw mockApiError(
          "http 422 for PATCH /api/settings",
          422,
          "codex_notice_round must be strictly below codex_compaction_threshold",
        );
      }
    }
    if (
      patch.acceleratedGraceSecs !== undefined &&
      (patch.acceleratedGraceSecs < 10 || patch.acceleratedGraceSecs > 3600)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "accelerated_grace_secs must be between 10 and 3600 seconds",
      );
    }
    if (
      patch.monitoringRefreshSeconds !== undefined &&
      (patch.monitoringRefreshSeconds < 1 ||
        patch.monitoringRefreshSeconds > 60)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "monitoring_refresh_seconds must be between 1 and 60",
      );
    }
    if (
      patch.outsourceMaxParallel !== undefined &&
      (patch.outsourceMaxParallel < -1 || patch.outsourceMaxParallel > 20)
    ) {
      // Server parity: -1 = 無限 (unlimited), 0 = paused, 20 = ceiling.
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "outsource_max_parallel must be between -1 and 20 (-1 = unlimited)",
      );
    }
    // Server parity (T-3aeb / T-ae38 / T-30f1): each floor IS that segment's
    // shipped default, so a document cap can only ever be raised. Duty's floor
    // is its OWN default, not the others' — sharing one number here would make
    // the owner's Duty default unreachable through this surface. The numbers
    // are read from DOC_CAP_CHARS_DEFAULTS, never restated.
    for (const [field, wire, min] of [
      [
        patch.docCapCharsDuty,
        "doc_cap_chars_duty",
        DOC_CAP_CHARS_DEFAULTS.duty,
      ],
      [
        patch.docCapCharsInsight,
        "doc_cap_chars_insight",
        DOC_CAP_CHARS_DEFAULTS.insight,
      ],
      [
        patch.docCapCharsLearning,
        "doc_cap_chars_learning",
        DOC_CAP_CHARS_DEFAULTS.learning,
      ],
      [
        patch.docCapCharsManualSop,
        "doc_cap_chars_manual_sop",
        DOC_CAP_CHARS_DEFAULTS.manualSop,
      ],
      [
        patch.docCapCharsManualLearnings,
        "doc_cap_chars_manual_learnings",
        DOC_CAP_CHARS_DEFAULTS.manualLearnings,
      ],
      [
        patch.docCapCharsSystemInteraction,
        "doc_cap_chars_system_interaction",
        DOC_CAP_CHARS_DEFAULTS.systemInteraction,
      ],
      [
        patch.docCapCharsBootSequence,
        "doc_cap_chars_boot_sequence",
        DOC_CAP_CHARS_DEFAULTS.bootSequence,
      ],
      [
        patch.docCapCharsOffboard,
        "doc_cap_chars_offboard",
        DOC_CAP_CHARS_DEFAULTS.offboard,
      ],
    ] as const) {
      if (field !== undefined && (field < min || field > 100000)) {
        throw mockApiError(
          "http 422 for PATCH /api/settings",
          422,
          `${wire} must be between ${min} and 100000 characters — the floor is the shipped default, so the document cap can only be raised, never lowered`,
        );
      }
    }
    // T-8: its own check. Its unit is FILES, not characters, and it is the only
    // knob on this endpoint whose value causes DELETION — so it cannot sit in a
    // table whose shared message talks about characters and floors.
    if (
      patch.backupRetain !== undefined &&
      (patch.backupRetain < BACKUP_RETAIN_MIN ||
        patch.backupRetain > BACKUP_RETAIN_MAX)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        `backup_retain must be between ${BACKUP_RETAIN_MIN} and ${BACKUP_RETAIN_MAX} backups per pool`,
      );
    }
    // T-c9b4: checked on its own, NOT as a row above — it has its own ceiling,
    // and the message above ("the floor is the shipped default … can only be
    // raised") would be a lie about a knob that may be turned down.
    if (
      patch.chatBudgetChars !== undefined &&
      (patch.chatBudgetChars < CHAT_BUDGET_CHARS_MIN ||
        patch.chatBudgetChars > CHAT_BUDGET_CHARS_MAX)
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        `chat_budget_chars must be between ${CHAT_BUDGET_CHARS_MIN} and ${CHAT_BUDGET_CHARS_MAX} characters`,
      );
    }
    if (patch.orgName !== undefined && [...patch.orgName.trim()].length > 80) {
      // Server parity: trimmed, capped at 80 runes (T-d693).
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "org_name must be at most 80 characters",
      );
    }
    if (
      patch.ownerName !== undefined &&
      [...patch.ownerName.trim()].length > 80
    ) {
      // Server parity: trimmed, capped at 80 runes (T-0b41).
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "owner_name must be at most 80 characters",
      );
    }
    // display_theme is validated against the THEME STORE: "" | a built-in | an
    // id that exists in /api/themes (server parity). T-83ef: settings no
    // longer carries the bundles, so there is no "post-patch custom set" to
    // resolve against — the store IS the set.
    if (
      patch.displayTheme !== undefined &&
      !isValidDisplayTheme(patch.displayTheme.trim(), mockThemeIds())
    ) {
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        'display_theme must be "", office, or an existing custom theme id',
      );
    }
    if (
      patch.displayLanguage !== undefined &&
      patch.displayLanguage.trim() !== "" &&
      !["zh", "en"].includes(patch.displayLanguage.trim())
    ) {
      // Server parity: enum-checked, "" clears (T-0b41-p2).
      throw mockApiError(
        "http 422 for PATCH /api/settings",
        422,
        "display_language must be one of zh, en",
      );
    }
    if (patch.ownerTokenTtl !== undefined) {
      mockServerSettings.owner_token_ttl = patch.ownerTokenTtl;
    }
    if (patch.agentTokenTtl !== undefined) {
      mockServerSettings.agent_token_ttl = patch.agentTokenTtl;
    }
    if (patch.handoverPct !== undefined) {
      mockServerSettings.handover_pct = patch.handoverPct;
    }
    if (patch.codexCompactionThreshold !== undefined) {
      mockServerSettings.codex_compaction_threshold =
        patch.codexCompactionThreshold;
    }
    if (patch.noticePct !== undefined) {
      mockServerSettings.notice_pct = patch.noticePct;
    }
    if (patch.codexNoticeRound !== undefined) {
      mockServerSettings.codex_notice_round = patch.codexNoticeRound;
    }
    if (patch.acceleratedGraceSecs !== undefined) {
      mockServerSettings.accelerated_grace_secs = patch.acceleratedGraceSecs;
    }
    if (patch.monitoringRefreshSeconds !== undefined) {
      mockServerSettings.monitoring_refresh_seconds =
        patch.monitoringRefreshSeconds;
    }
    if (patch.outsourceMaxParallel !== undefined) {
      mockServerSettings.outsource_max_parallel = patch.outsourceMaxParallel;
    }
    if (patch.docCapCharsDuty !== undefined) {
      mockServerSettings.doc_cap_chars_duty = patch.docCapCharsDuty;
    }
    if (patch.docCapCharsInsight !== undefined) {
      mockServerSettings.doc_cap_chars_insight = patch.docCapCharsInsight;
    }
    if (patch.docCapCharsLearning !== undefined) {
      mockServerSettings.doc_cap_chars_learning = patch.docCapCharsLearning;
    }
    if (patch.docCapCharsManualSop !== undefined) {
      mockServerSettings.doc_cap_chars_manual_sop = patch.docCapCharsManualSop;
    }
    if (patch.docCapCharsManualLearnings !== undefined) {
      mockServerSettings.doc_cap_chars_manual_learnings =
        patch.docCapCharsManualLearnings;
    }
    if (patch.docCapCharsSystemInteraction !== undefined) {
      mockServerSettings.doc_cap_chars_system_interaction =
        patch.docCapCharsSystemInteraction;
    }
    if (patch.docCapCharsBootSequence !== undefined) {
      mockServerSettings.doc_cap_chars_boot_sequence =
        patch.docCapCharsBootSequence;
    }
    if (patch.docCapCharsOffboard !== undefined) {
      mockServerSettings.doc_cap_chars_offboard = patch.docCapCharsOffboard;
    }
    if (patch.chatBudgetChars !== undefined) {
      mockServerSettings.chat_budget_chars = patch.chatBudgetChars;
    }
    if (patch.backupRetain !== undefined) {
      mockServerSettings.backup_retain = patch.backupRetain;
    }
    if (patch.updaterReceiveBeta !== undefined) {
      mockServerSettings.updater_receive_beta = patch.updaterReceiveBeta;
    }
    if (patch.updaterAutoUpdate !== undefined) {
      mockServerSettings.updater_auto_update = patch.updaterAutoUpdate;
    }
    if (patch.orgName !== undefined) {
      mockServerSettings.org_name = patch.orgName.trim();
    }
    if (patch.ownerName !== undefined) {
      mockServerSettings.owner_name = patch.ownerName.trim();
    }
    if (patch.pushContactEmail !== undefined) {
      const email = patch.pushContactEmail.trim();
      if (
        email &&
        (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ||
          /\.(local|localhost|internal|test|invalid|example)$/i.test(email))
      ) {
        throw mockApiError(
          "http 422 for PATCH /api/settings",
          422,
          "push_contact_email must be a public email address",
        );
      }
      mockServerSettings.push_contact_email = email;
    }
    // display_theme (T-83ef): an explicit value wins, and an omitted one changes
    // NOTHING — no sweep of a now-dangling active theme.
    //
    // 🔴 THE ABSENCE IS THE PARITY. Both sides used to sweep here, back when
    // this endpoint also wrote the bundles and could therefore orphan the active
    // theme itself. It cannot any more: DELETE /api/themes/{id} performs the
    // reset and reports it in its receipt. api_settings.go says exactly why it
    // dropped its sweep — "a second opinion about a fact that endpoint already
    // settled, and the two would drift" — and this mock kept sweeping, so the
    // two that drifted were the server and this file. That comment predicted its
    // own situation and nothing noticed.
    //
    // What it cost: a settings row naming a theme with no row heals ITSELF here
    // on the next unrelated PATCH, and never heals in production. A mock that is
    // kinder than the server lets a test go green on a state the owner would
    // still be sitting in.
    if (patch.displayTheme !== undefined) {
      mockServerSettings.display_theme = patch.displayTheme.trim();
    }
    if (patch.displayLanguage !== undefined) {
      mockServerSettings.display_language = patch.displayLanguage.trim();
    }
    // display_wide (T-756f) is a plain bool — nothing to validate, and an
    // omitted field never changes it (PATCH semantics, server parity).
    if (patch.displayWide !== undefined) {
      mockServerSettings.display_wide = patch.displayWide;
    }
    if (patch.loreEnabled !== undefined) {
      mockServerSettings.lore_enabled = patch.loreEnabled;
    }
    // onboarding_dismissed (T-0648) — server parity: the stamp lives ON the
    // report row, and only a `failed` report has a banner up to close. Every
    // other state is refused with 409, exactly as setOnboardingDismissed does:
    // no report at all (mock mode's standing state until a test seeds one via
    // __injectMockOnboardingReport), or a run still `running`. What the
    // `running` refusal buys server-side is not the stamp's visibility — a
    // stamp there is wiped by whichever terminal path lands next — but the
    // WRITE: the server's dismissal is an unlocked read-modify-write of the
    // whole report row and the only writer that can run CONCURRENTLY with the
    // run, so writing back a pre-verdict copy ERASES the failure and strands
    // the report in `running`, where no banner draws and onboarding never
    // re-runs. The mock has no concurrent writer to reproduce that, so it
    // copies the REFUSAL, which is the part callers can observe. Like the
    // server, this runs LAST — the settings fields above are already applied
    // when the refusal is thrown.
    if (patch.onboardingDismissed !== undefined) {
      if (mockServerSettings.onboarding?.state !== "failed") {
        // This sentence is a SECOND hand copy of errNoOnboardingBanner's, and
        // it stays one: NOT ONE cross-language string contract in this repo
        // reaches error-envelope text. Every `drift-*` gate in the Makefile
        // regenerates from a source that carries something else — schema
        // descriptions (spec/openapi.json), UI wording (locales/en.ts),
        // --color-* names (styles/theme.css), --font-* names and safe-family
        // stacks (themeFonts.source.json). Binding these two copies would mean
        // inventing a generator for one string. Nothing depends on the wording
        // either: the tests on both sides assert the STATUS (and the code
        // derived from it), and the banner's only caller discards the rejection
        // entirely — it puts itself back up rather than showing the server's
        // sentence.
        throw mockApiError(
          "http 409 for PATCH /api/settings",
          409,
          "no onboarding banner is up to dismiss — the first-run report is absent or not in a failed state",
        );
      }
      mockServerSettings.onboarding = {
        ...mockServerSettings.onboarding,
        dismissed_at: patch.onboardingDismissed ? Date.now() / 1000 : 0,
      };
    }
    return toServerSettings(structuredClone(mockServerSettings));
  },

  async getPushPublicKey(): Promise<string> {
    // A valid-size P-256 public-key-shaped value keeps mock-mode consumers
    // deterministic; real browser subscriptions always use the server key.
    return "B" + "A".repeat(86);
  },

  async savePushSubscription(
    _subscription: PushSubscriptionInput,
  ): Promise<void> {
    // Mock mode intentionally has no push gateway or durable browser targets.
  },

  async removePushSubscription(_endpoint: string): Promise<void> {
    // Idempotent, matching the production DELETE endpoint.
  },

  async checkRelease(): Promise<ReleaseCheckView> {
    // Server parity: the mock world has no GitHub to ask, so the honest fresh
    // verdict is "up to date at the running version" (never a phantom newer
    // release, never a fabricated failure).
    return toReleaseCheck({
      status: "up_to_date",
      current_version: MOCK_WIRE_VERSION.version,
      latest_tag: null,
      release_url: null,
    });
  },

  async triggerUpgrade(): Promise<void> {
    // Server parity: no newer GitHub release is ever known in mock mode → the
    // honest 409 precondition answer.
    throw mockApiError(
      "http 409 for POST /api/update/upgrade",
      409,
      "no newer release is known — the running build is the latest published on GitHub",
    );
  },

  async getGlobalContext(): Promise<GlobalContextView> {
    return toGlobalContext(foldGlobalContext());
  },

  async saveGlobalContext(text: string): Promise<GlobalContextView> {
    recordDocumentHistory("global_context", "global");
    // Whole-BLOCK replace of the user-custom additive block → store the overlay;
    // the folded read is now owner-edited (is_default=false).
    globalContextOverlay = {
      text,
      owner_id: MOCK_OWNER_ID,
      schema_version: 3,
      is_default: false,
      // Overwritten by foldGlobalContext with the live studio name.
      org_name: "",
    };
    emitTopic("global_context");
    return toGlobalContext(foldGlobalContext());
  },

  async resetGlobalContext(): Promise<GlobalContextView> {
    recordDocumentHistory("global_context", "global");
    // Idempotent tombstone: drop the overlay → the folded read is EMPTY again
    // (text=""/is_default=true; the assembled boot context skips the block).
    globalContextOverlay = null;
    emitTopic("global_context");
    return toGlobalContext(foldGlobalContext());
  },

  async getBootDoc(kind: BootDocKind, key: string): Promise<BootDocView> {
    return toBootDoc(foldBootDoc(kind, key));
  },

  async saveBootDoc(
    kind: BootDocKind,
    key: string,
    body: string,
  ): Promise<BootDocView> {
    // 404 BEFORE anything is written: foldBootDoc is the one place that knows
    // whether (kind, key) names a document, and a save that created a fourth
    // stream out of a typo'd runtime key would be the mock inventing a
    // document the server has no route for.
    const before = foldBootDoc(kind, key);
    // 405, not 403: no principal may edit a read-only document, so pointing at
    // authz would send an owner looking for a role to grant. The message is the
    // server's own bootDocReadOnlyRefusal, shortened to the same claim.
    refuseReadOnlyBootDoc(kind, key, "");
    // 🔴 THE HEAD IS PUT BACK HERE, exactly as replaceBootDoc does it. The mock
    // takes a BODY because the wire does; storing the body alone would make the
    // demo mode the one place a headless document can still be created — the
    // hazard the body-only wire exists to remove.
    const text = before.read_only_head
      ? docJoinHeadBody(before.read_only_head, body)
      : body;
    // The wipe guard the server has carried since T-2d99, on the unit the
    // server judges it on: the BODY. The head survives every write, so a guard
    // measured on the stored document would never see an emptying again.
    if (wholeDocWipeBlocked(before.body, body)) {
      throw mockApiError(
        `http 400 for POST /api/boot-docs/${kind}/${key}`,
        400,
        "this would replace the existing document with an empty one",
      );
    }
    // The server's floor, mirrored — and on the STORED document, which is what
    // size_chars/cap_chars describe. The cockpit blocks first (it has the
    // numbers on screen), so reaching this is a stale page or a non-cockpit
    // caller.
    if (docCapBlocked(before.cap_chars, before.text, text)) {
      throw mockApiError(
        `http 400 for POST /api/boot-docs/${kind}/${key}`,
        400,
        `document is ${[...text].length} characters, over the ${before.cap_chars} character limit`,
      );
    }
    // 🔴 Identical content retains NO version (owner ruling). Ten slots sound
    // generous until the surface is used the way it is meant to be — paste,
    // look, paste again — and a save that changed nothing spending one of them
    // is how the version worth going back to disappears. Nothing is written at
    // all in that case, so `is_default` is not flipped either: re-saving a
    // document that is still the factory text must not make it stop saying so.
    if (before.text === text) return toBootDoc(before);
    recordDocumentHistory(kind, key);
    bootDocOverlays.set(`${kind}/${key}`, text);
    emitTopic(BOOT_DOC_TOPIC);
    return toBootDoc(foldBootDoc(kind, key));
  },

  async resetBootDoc(kind: BootDocKind, key: string): Promise<BootDocView> {
    // Existence check first, same as the save — and NO cap check: going back to
    // the factory version can only ever be the shipped size, and refusing it on
    // length would take away the recovery path exactly when the document is at
    // its worst.
    foldBootDoc(kind, key);
    refuseReadOnlyBootDoc(kind, key, "/reset");
    recordDocumentHistory(kind, key);
    bootDocOverlays.delete(`${kind}/${key}`);
    emitTopic(BOOT_DOC_TOPIC);
    return toBootDoc(foldBootDoc(kind, key));
  },

  async listRoles(): Promise<RoleSummaryView[]> {
    // Seeds first (stable), then the owner-created custom roles — mirrors
    // handle_list_roles. T-1170: the DIRECTORY — `toRoleSummary` is what makes
    // the mock stop handing out `definition_md` on this route, which is the
    // half that keeps the tests honest about the new server.
    return [
      ...MOCK_WIRE_ROLES_SEED.map((seed) => toRoleSummary(foldRole(seed.key))),
      ...[...customRoles.keys()].map((key) => toRoleSummary(foldRole(key))),
    ];
  },

  async getRole(key: string): Promise<RoleDefView> {
    return toRoleDef(foldRole(key));
  },

  async saveRole(key: string, patch: RolePatch): Promise<RoleDefView> {
    // Self-contained overlay (§6.1): merge the patch onto the current folded doc
    // so the stored overlay carries the FULL effective name + definition_md.
    // Name-lock parity with handle_update_role (owner M2 定案): ONLY a CUSTOM
    // role's name applies — a seed role IGNORES a supplied name (ignore, not
    // reject). A custom rename also updates its members' resolved role_name
    // (the server re-folds it per list; the mock stores it on the wire row).
    recordDocumentHistory("role_definition", key);
    const current = foldRole(key);
    const nameEditable = customRoles.has(key);
    const nextName =
      nameEditable && patch.name !== undefined ? patch.name : current.name;
    roleOverlays.set(key, {
      ...current,
      name: nextName,
      definition_md:
        patch.definitionMd !== undefined
          ? patch.definitionMd
          : current.definition_md,
      is_default: false,
    });
    if (nameEditable && patch.name !== undefined) {
      for (const m of wireMembers) {
        if (m.role_key === key) m.role_name = nextName;
      }
    }
    emitTopic("role_def");
    return toRoleDef(foldRole(key));
  },

  async resetRole(key: string): Promise<RoleDefView> {
    // Reset restores the FILE SEED — only a seed role has one. A custom (or
    // unknown) key 404s, matching handle_reset_role (verified live: the server
    // refuses and the custom doc stays untouched). The UI offers no reset on
    // custom roles; this guard keeps the mock honest for parity tests.
    if (!MOCK_WIRE_ROLES_SEED.some((r) => r.key === key)) {
      throw mockApiError(
        `http 404 for POST /api/roles/${key}/reset`,
        404,
        `role '${key}' not found`,
      );
    }
    // Idempotent tombstone: drop the overlay → the folded read is the seed again.
    recordDocumentHistory("role_definition", key);
    roleOverlays.delete(key);
    emitTopic("role_def");
    return toRoleDef(foldRole(key));
  },

  async createRole(input: RoleCreateInput): Promise<RoleCreateResult> {
    // Mirrors handle_create_role: mint both ids, template doc, member OFFLINE.
    // memberName omitted/blank ⇒ pick a fresh pool name (server 隨機成員名 parity):
    // the pool mirrors the server name pool (domain.go); existing roster names are
    // excluded case-insensitively.
    const name = input.name.trim();
    if (!name) {
      throw mockApiError(
        "http 422 for POST /api/roles",
        422,
        "role requires a name",
      );
    }
    const memberName = (input.memberName ?? "").trim() || pickMockMemberName();
    const effort = input.effort ?? "medium";
    if (!["low", "medium", "high", "max"].includes(effort)) {
      // Byte-for-byte the server's message (ocserverd/api_roles.go:128-129):
      // the offending value rides along in `; got '<value>'`.
      throw mockApiError(
        "http 422 for POST /api/roles",
        422,
        `effort must be one of [high low max medium]; got '${effort}'`,
      );
    }
    const hex = () =>
      Math.random().toString(16).slice(2, 8) +
      Math.random().toString(16).slice(2, 8);
    const roleKey = `r-${hex()}`;
    // A custom role IS its own document row from the moment it is created, so
    // its first EDIT already has a previous version to retain (server parity:
    // handle_create_role writes the role_def row).
    markDocumentRow("role_definition", roleKey);
    customRoles.set(roleKey, {
      key: roleKey,
      name,
      definition_md: CUSTOM_ROLE_TEMPLATE_MD,
      ...docSizeFields(CUSTOM_ROLE_TEMPLATE_MD, "duty"),
      owner_id: MOCK_OWNER_ID,
      schema_version: 3,
      is_default: false,
      is_seed: false,
    });
    const memberId = `m-${hex()}`;
    const wireMember: WireMember = {
      id: memberId,
      name: memberName,
      kind: "",
      role_key: roleKey,
      role_name: name,
      runtime: input.runtime ?? "claude",
      model: (input.model ?? "").trim(),
      actual_model: "",
      actual_runtime: "",
      actual_effort: "",
      actual_machine: "",
      refocus_op: "",
      refocus_deadline: 0,
      effort,
      desired_state: "offline",
      desired_machine_id: MOCK_SERVER_SELF_ID,
      machine: "",
      presence: "offline",
      refocus_since: 0,
      last_op: "",
      last_op_ok: null,
      last_op_log: "",
      last_op_reason: "",
      last_op_at: 0,
      forced_stop_at: 0,
      roster_status: "active",
      owner_id: MOCK_OWNER_ID,
      unread_count: 0,
      schema_version: 3,
    };
    wireMembers.push(wireMember);
    return {
      role: toRoleDef(foldRole(roleKey)),
      member: mapWithExtras(wireMember),
    };
  },

  async deleteRole(key: string): Promise<void> {
    // Mirrors handle_delete_role's 防線 + hard cascade — thrown as the SAME
    // ApiError the http client throws (status/code off the unified error
    // envelope, docs/design/api-error-envelope.md), so a caller branching on
    // e.status (SettingsPage's isHttpStatus) behaves identically on mock.
    // Seed role → 403.
    if (MOCK_WIRE_ROLES_SEED.some((r) => r.key === key)) {
      throw mockApiError(
        `http 403 for DELETE /api/roles/${key}`,
        403,
        `role '${key}' is a built-in seed role and cannot be deleted`,
      );
    }
    if (!customRoles.has(key)) {
      throw mockApiError(
        `http 404 for DELETE /api/roles/${key}`,
        404,
        `role '${key}' not found`,
      );
    }
    const members = wireMembers.filter((m) => m.role_key === key);
    if (members.some((m) => m.presence !== "offline")) {
      throw mockApiError(
        `http 409 for DELETE /api/roles/${key}`,
        409,
        `role '${key}' has online member(s) — stop them before deleting`,
      );
    }
    const ids = new Set(members.map((m) => m.id));
    wireMembers = wireMembers.filter((m) => !ids.has(m.id));
    chatLog = chatLog.filter((c) => !ids.has(c.from) && !ids.has(c.to));
    for (const k of [...chatReads.keys()]) {
      const [reader, peer] = k.split("::");
      if (ids.has(reader) || ids.has(peer)) chatReads.delete(k);
    }
    // The role's documents go with it, retained revisions included.
    dropRoleLessonsHistory(key);
    dropRoleInsightHistory(key);
    dropDocumentHistory("role_definition", key);
    roleOverlays.delete(key);
    customRoles.delete(key);
  },

  async getBootstrap(role: string): Promise<BootstrapView> {
    // Honest preview mirroring the backend buildBootContext slot order
    // (spec/lifecycle.md §2.2, as re-ordered by T-4595):
    //   1. 系統互動 — FOLDED, FIRST (T-30e4). The owner's edit wins and the
    //      seed is what an installation that never edited it folds to, exactly
    //      as the server does it (T-791e, buildBootContext →
    //      systemInteractionText → foldBootDocDTO). Until T-30e4 this slot read
    //      the seed constant straight, so the one screen built to show what an
    //      agent will read was the one place the owner's edit was invisible;
    //   2. 使用者自訂 — the owner's ADDITIVE block, SKIPPED entirely when empty;
    //   3. `# Role:` + `# Insight (role)` + `# Lessons (role)` —
    //      the persona (Duty → Insight → Learning, the order the three blocks
    //      are defined in), and the ONLY slot an outsource worker has nothing
    //      in (see getWorkerBootContext below). The Insight section is SKIPPED
    //      ENTIRELY when the folded text is blank, exactly like the owner block
    //      — the gate is the TEXT, never is_default/has_seed (those answer
    //      different questions and would emit an orphan header);
    //   4. 啟動步驟 — FOLDED, LAST (recency-authoritative tail), and always the
    //      CLAUDE document. 🔴 The missing runtime parameter is DELIBERATE, not
    //      the other half of the T-30e4 gap: the real request carries `{role}`
    //      and no member_id ON PURPOSE (http.ts getBootstrap — a UI preview must
    //      never be handed an agent JWT), so server-side `member == nil` →
    //      memberRuntime "" → bootSequenceDocKey("") → the claude key. Teaching
    //      this mock about runtime would make it disagree with the endpoint it
    //      stands in for. The worker path (getWorkerBootContext) DOES branch on
    //      runtime because a spawn really does name its worker.
    // The owner block moved from below the persona to above it so the two
    // assemblies line up: a
    // worker's boot context is this list minus slot 3, and with the owner block
    // wedged between the lessons and the boot sequence it could not be.
    // NO token (a UI preview mints none).
    const roleDef = foldRole(role); // throws for an unknown role (≈ server 404)
    const lessons = lessonsOverlays.get(role)?.text ?? SEED_LESSONS_MD;
    const userText = foldGlobalContext().text;
    const parts = [foldBootDoc("system_interaction", "global").text.trim()];
    if (userText.trim()) {
      parts.push(`# 使用者自訂（Owner Additions）\n\n${userText.trim()}`);
    }
    parts.push(
      `# Role: ${roleDef.name || roleDef.key}\n\n${roleDef.definition_md.trim()}`,
    );
    const insightText =
      insightOverlays.get(role)?.text ?? INSIGHT_SEEDS[role] ?? "";
    if (insightText.trim()) {
      parts.push(`# Insight (${role})\n\n${insightText.trim()}`);
    }
    // The title is injected IDEMPOTENTLY, mirroring buildBootContext (T-8327):
    // a generation that treats its boot segment as the document base and writes
    // it back turns the title into document content, and a naive re-prepend
    // would then stack one title per generation. Strip any leading copies of
    // the EXACT title line first, so an already-poisoned document self-heals in
    // the assembled preview instead of showing the drift the server does not.
    // TWO titles are stripped, not one: a document poisoned BEFORE T-2 carries
    // the old "# Lessons (role / general)" wording, and the server strips both
    // (assets.go). Mirroring that here is what keeps this preview honest about
    // what the agent will actually read.
    const lessonsTitle = `# Lessons (${role})`;
    const legacyLessonsTitle = `# Lessons (${role} / general)`;
    let lessonsBody = lessons.trim();
    for (;;) {
      let stripped = false;
      for (const title of [lessonsTitle, legacyLessonsTitle]) {
        while (lessonsBody.startsWith(title)) {
          const rest = lessonsBody.slice(title.length);
          // A title that is merely the PREFIX of a longer line is not a
          // duplicate title line — stop, or the next heading gets eaten.
          if (rest !== "" && !rest.startsWith("\n")) break;
          lessonsBody = rest.trim();
          stripped = true;
        }
      }
      if (!stripped) break;
    }
    parts.push(
      `${lessonsTitle}\n\n${lessonsBody}`,
      foldBootDoc("boot_sequence", "claude").text.trim(),
    );
    const wire: WireBootstrap = {
      role,
      name: roleDef.name,
      context: parts.join("\n\n") + "\n",
      token: null,
    };
    return toBootstrap(wire);
  },

  async getLessons(roleKey: string): Promise<LessonsView> {
    // The folded PER-ROLE lessons doc for `role_key`. When an
    // overlay was saved (is_default=false) the folded read is that edit; otherwise
    // it IS the REAL seed (dal/seeds/lessons.md via SEED_LESSONS_MD) →
    // is_default=true. The seed is shared until a role diverges (each role_key
    // gets its own overlay slot).
    const overlay = lessonsOverlays.get(roleKey);
    const wire: WireLessons = overlay ?? {
      ...docSizeFields(SEED_LESSONS_MD, "learning"),
      role_key: roleKey,
      text: SEED_LESSONS_MD,
      owner_id: MOCK_OWNER_ID,
      schema_version: 2,
      is_default: true,
    };
    return toLessons(wire);
  },

  async saveLessons(roleKey: string, text: string): Promise<LessonsView> {
    // Whole-doc replace → store the per-role overlay; the folded read is now
    // owner-edited for THIS role_key only (a sibling role's doc is untouched).
    recordDocumentHistory("lessons", roleKey);
    const wire: WireLessons = {
      ...docSizeFields(text, "learning"),
      role_key: roleKey,
      text,
      owner_id: MOCK_OWNER_ID,
      schema_version: 2,
      is_default: false,
    };
    lessonsOverlays.set(roleKey, wire);
    emitTopic("lessons");
    return toLessons(wire);
  },

  async getInsight(roleKey: string): Promise<InsightView> {
    // The folded PER-ROLE insight doc: overlay ⊕ this role's OWN file seed
    // (T-e1e3). 🔴 PER-ROLE, mirroring seedInsightMD on the server — `assistant`
    // folds against seeds/insight_assistant.md, EVERY OTHER ROLE STILL READS "".
    // Copying the lessons shape (one shared seed for all roles) here would hide
    // the exact defect the server test is guarding against, because the cockpit
    // would then look correct against a mock that is wrong in the same way.
    //
    // is_default stays "this role has never written" in both branches; it is no
    // longer the same statement as text === "".
    const seed = INSIGHT_SEEDS[roleKey];
    const wire: WireInsight = insightOverlays.get(roleKey) ?? {
      ...docSizeFields(seed ?? "", "insight"),
      role_key: roleKey,
      text: seed ?? "",
      has_seed: roleKey in INSIGHT_SEEDS,
      owner_id: MOCK_OWNER_ID,
      schema_version: 3,
      is_default: true,
    };
    // 🔴 has_seed is a fact about the SEED ROSTER, not about the stored
    // overlay, so BOTH branches above carry it — a written overlay must still
    // report true (T-6501). Baking it into only the default branch would make
    // the cockpit hide the reset row the moment a role saved an edit.
    return toInsight(wire);
  },

  async saveInsight(roleKey: string, text: string): Promise<InsightView> {
    // Whole-doc replace → store the per-role overlay. Keyed on the bare
    // role_key; a sibling role's insight is untouched.
    recordDocumentHistory("insight", roleKey);
    const wire: WireInsight = {
      ...docSizeFields(text, "insight"),
      role_key: roleKey,
      text,
      has_seed: roleKey in INSIGHT_SEEDS,
      owner_id: MOCK_OWNER_ID,
      schema_version: 3,
      is_default: false,
    };
    insightOverlays.set(roleKey, wire);
    emitTopic("insight");
    return toInsight(wire);
  },

  async resetInsight(roleKey: string): Promise<InsightView> {
    // Reset restores the PER-ROLE FILE SEED — only a role that ships one has
    // anything to reset TO, so a role with no seed 404s, mirroring
    // HandleResetInsightApiInsightRoleKeyResetPost. 🔴 The membership test is
    // INSIGHT_SEEDS, not the seed-role roster: on the server the presence of
    // `seeds/insight_<role_key>.md` IS the roster, and a role can have a Duty
    // seed without an Insight one.
    if (!(roleKey in INSIGHT_SEEDS)) {
      throw mockApiError(
        `http 404 for POST /api/insight/${roleKey}/reset`,
        404,
        `role '${roleKey}' has no factory insight to reset to`,
      );
    }
    // The discarded overlay is retained as a revision BEFORE it is dropped —
    // the server does the same inside the write transaction, and a reset that
    // kept no history would be the one destructive write with no way back.
    recordDocumentHistory("insight", roleKey);
    insightOverlays.delete(roleKey);
    emitTopic("insight");
    return await mockApi.getInsight(roleKey);
  },

  async listDocumentHistory(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentHistoryEntryView[]> {
    refuseRetiredDocumentKind(kind, `GET /api/document-history/${kind}/${key}`);
    // Newest first, at most DOCUMENT_HISTORY_CAP — the retention the server
    // applies, so an offline cockpit sees the same bounded list.
    //
    // 🔴 T-1170: the DIRECTORY, and the mock serves it as the new server does
    // — `sizes` + `tombstoned`, and the text NOT on the answer. That is the
    // point of changing the mock at all: a mock that kept handing the text back
    // would let every test in this repo pass against a fake server the real one
    // no longer resembles, and the surfaces that read a revision off the list
    // would stay green while being broken.
    const kept = documentHistories.get(historySlot(kind, key)) ?? [];
    return kept.map((h) => toDocumentHistoryEntry(directoryRow(h)));
  },

  async getDocumentRevision(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentRevisionView> {
    const route = `GET /api/document-history/${kind}/${key}/${id}`;
    refuseRetiredDocumentKind(kind, route);
    // Named revision → its content, the only read that carries text (T-1170).
    // A pruned or unknown id 404s exactly where the restore of that id would:
    // the reader must be able to say "this version could not be read" instead
    // of drawing an empty document next to a destructive button.
    const kept = documentHistories.get(historySlot(kind, key)) ?? [];
    const found = kept.find((h) => h.id === id);
    if (!found) {
      throw mockApiError(
        `http 404 for ${route}`,
        404,
        `document revision ${id} is no longer retained`,
      );
    }
    return { id: found.id, content: structuredClone(found.content) };
  },

  async getDiff(params: DiffParams): Promise<DiffPairView> {
    // A FIXTURE, and it says so: the mock cockpit has no blob store to read a
    // side out of and no signature to check, so it answers a small, obviously
    // synthetic pair rather than pretending to resolve an address. What it DOES
    // reproduce faithfully is the shape the screen branches on — a heading per
    // side, and the `missing` marker, which is the one state a fixture that
    // always succeeds would leave permanently unreachable offline.
    //
    // The reserved address `att-000000000000` is how the offline cockpit
    // reaches that state; every other address resolves.
    //
    // The labels are ECHOED, never invented: a side the url gave no heading
    // comes back with none, exactly as the server answers it, so the reader's
    // own 「目前存檔內容」/「初始版本」/「版本 #12」 path is reachable offline too.
    const side = (address: string, label: string | undefined, text: string) =>
      address === "att-000000000000"
        ? { address, text: "", label, gone: true, goneReason: "mock: reserved gone address" }
        : { address, text, label, gone: false };
    return {
      before: side(params.before, params.labelBefore, MOCK_DIFF_BEFORE),
      after: side(params.after, params.labelAfter, MOCK_DIFF_AFTER),
    };
  },

  async getDiffShareLink(params: DiffParams): Promise<string> {
    // Mock face: the same URL SHAPE as the BE (the /diff page path + ?sig=)
    // with a deterministic fake sig — never a verifiable credential (no secret
    // in mock mode; the copy-link UI just needs a resolvable string). Any sig
    // the caller happened to be holding is dropped, exactly as the real route
    // drops it: the signature is what this call MINTS.
    return formatDiffUrl({
      before: params.before,
      after: params.after,
      labelBefore: params.labelBefore,
      labelAfter: params.labelAfter,
      sig: "mock-diff-share-sig",
    });
  },

  async getDocumentSeed(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentSeedView> {
    const route = `GET /api/document-history/${kind}/${key}/seed`;
    refuseRetiredDocumentKind(kind, route);
    // Mirrors api_document_history.go's documentSeedContent, INCLUDING which
    // documents have no default at all: the global block's default is the empty
    // document, a seed role's is its file seed, a role with an INSIGHT seed
    // file gets that, and everything else 404s — exactly where
    // resetGlobalContext / resetRole / resetInsight would also refuse. Reading
    // writes nothing here either: no recordDocumentHistory, no overlay touched.
    //
    // 🔴 THE `insight` BRANCH WAS MISSING and it mattered (T-40f0 node 11). The
    // server has had `case "insight"` since T-6501 and `POST
    // /api/insight/{role_key}/reset` sits right there in the route table, so
    // 404ing here was the mock being STINGIER than the server — the direction
    // frontend/CLAUDE.md warns about, just less famous than the generous one.
    // Its cost was concrete: the InsightCard's 初始版本 row could not be read
    // offline, and a tombstoned insight revision could only ever swap one wrong
    // screen for a differently wrong one. 🔴 The roster is INSIGHT_SEEDS (the
    // set of seeds/insight_<role_key>.md files), never the seed-ROLE roster —
    // a role can carry a Duty seed and no Insight one, which is the same
    // distinction resetInsight above is careful about.
    const content: Record<string, string> | null =
      kind === "global_context"
        ? { text: "", tombstoned: "true" }
        : kind === "role_definition" &&
            MOCK_WIRE_ROLES_SEED.some((r) => r.key === key)
          ? { definition_md: roleSeed(key).definition_md, tombstoned: "true" }
          : kind === "insight" && key in INSIGHT_SEEDS
            ? { text: INSIGHT_SEEDS[key], tombstoned: "true" }
            : // T-791e: all three boot-context blocks ship a factory version,
              // and it is the SEED TEXT (unlike global_context, whose default
              // is the empty document) — so 初始版本 can be read and diffed
              // before anyone decides to go back to it. `tombstoned` marks it
              // as "follow the seed", which is what restoring it must do.
              (kind === "system_interaction" || kind === "boot_sequence") &&
                bootDocSeed(kind, key) !== null
              ? { text: bootDocSeed(kind, key)!, tombstoned: "true" }
              : null;
    if (content === null) {
      throw mockApiError(
        `http 404 for ${route}`,
        404,
        `document '${kind}/${key}' has no shipped default to compare against`,
      );
    }
    const wire: WireDocumentSeed = { kind, key, content };
    return toDocumentSeed(structuredClone(wire));
  },

  async restoreDocumentHistory(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentHistoryView> {
    refuseRetiredDocumentKind(
      kind,
      `POST /api/document-history/${kind}/${key}/${id}/restore`,
    );
    const slot = historySlot(kind, key);
    const found = (documentHistories.get(slot) ?? []).find((h) => h.id === id);
    if (!found) {
      throw mockApiError(
        `http 404 for POST /api/document-history/${kind}/${key}/${id}/restore`,
        404,
        "document history version not found",
      );
    }
    // The restore is itself a write: the state it overwrites becomes the
    // newest retained revision (server parity — SaveWithDocumentHistory).
    recordDocumentHistory(kind, key);
    applyDocumentHistory(kind, key, found.content);
    return toDocumentHistory(structuredClone(found));
  },

  subscribeEvents(onTopic: (topic: string) => void): () => void {
    // No live stream in the mock — emitTopic provides the matching local
    // reconciliation signal for the mutation faces that use SSE in production.
    topicSubscribers.add(onTopic);
    return () => {
      topicSubscribers.delete(onTopic);
    };
  },

  // ── T-33 傳承 (lore), read side ──────────────────────────────────────────
  async searchLore(input: LoreSearchInput = {}): Promise<LoreSearchView> {
    // The selection is applied HERE rather than returning the whole fixture,
    // so a screen that forgets to pass its filter looks wrong in mock mode
    // too. `limit` is REFUSED out of range exactly as the route refuses it —
    // a mock that silently clamped would hide the one failure the caller
    // most needs to see.
    if (input.limit !== undefined && (input.limit < 1 || input.limit > 100)) {
      throw mockApiError(
        "http 422 for /api/lore/search",
        422,
        "limit must be between 1 and 100",
      );
    }
    const subject = input.subject ?? "";
    const query = input.query ?? "";
    const needle = query.toLowerCase();
    const known = new Set(MOCK_LORE_ENTRIES.flatMap((e) => e.subjects));
    // A subject key that names nothing is NOT an empty result: it comes back
    // unresolved with the key echoed, so a typo is visible instead of reading
    // as 「this subject has nothing filed under it」.
    const subjectResolved = subject === "" || known.has(subject);
    const matched = !subjectResolved
      ? []
      : MOCK_LORE_ENTRIES.filter((e) => {
          if (subject !== "" && !e.subjects.includes(subject)) return false;
          if (needle === "") return true;
          // The station's literal matcher scans 第 1、2 格 and nothing else
          // (loreEntryMatchesLiteral). 第 3、4 格 are deliberately NOT scanned
          // there, so scanning them here would make the mock answer a wider
          // question than the route does.
          return (
            e.trigger.toLowerCase().includes(needle) ||
            e.content.toLowerCase().includes(needle)
          );
        });
    const limit = input.limit ?? 20;
    const entries: LoreEntrySummaryView[] = matched
      .slice(0, limit)
      .map((e) => ({
        entryId: e.entryId,
        // 🔴 A hit carries 第 1、2 格 ONLY, exactly as the route serves it.
        // 第 3、4、5 格 are reached with getLoreEntry — a mock that handed the
        // list everything would let a screen render events it can never get.
        trigger: e.trigger,
        content: e.content,
        subjects: [...e.subjects],
        actions: [...e.actions],
        origin: e.origin,
        // Every fixture entry matches on the axes actually asked about, so
        // nothing here reaches the caller across an axis it did not ask for.
        tier: "T1",
        tierNote: "",
        trustScope: "method",
        trustFellBack: false,
      }));
    return {
      entries,
      total: matched.length,
      truncated: matched.length > entries.length,
      subjectResolved,
      unresolvedSubject: subjectResolved ? "" : subject,
      applied: {
        subject,
        actions: input.actions ? [...input.actions] : [],
        query,
        queryMatch: "literal-substring",
        limit,
        // No fixture entry carries an action, so no tiering axis was ever
        // exercised — reporting one would be the mock inventing evidence.
        tieredBy: [],
      },
      unmappedActions: [],
    };
  },

  async getLoreEntry(entryId: string): Promise<LoreEntryDetailView> {
    const e = MOCK_LORE_ENTRIES.find((x) => x.entryId === entryId);
    if (!e) {
      throw mockApiError(
        `http 404 for /api/lore/entries/${entryId}`,
        404,
        "lore entry not found",
      );
    }
    const original = mockLoreOriginal(e);
    return {
      entryId: e.entryId,
      trigger: e.trigger,
      content: e.content,
      retireWhen: e.retireWhen,
      impact: e.impact,
      events: e.events.map((ev) => ({ ...ev })),
      subjects: [...e.subjects],
      actions: [...e.actions],
      origin: e.origin,
      status: e.status,
      original,
      sha256: "",
      supersedes: e.supersedes,
      writtenBy: e.writtenBy,
      // One revision each: these five were written once and never rewritten,
      // so there is no shrink to show. A fabricated second revision would put
      // an 「entry hollowed out」 signal on screen that never happened.
      revisions: [
        {
          revisionId: 1,
          createdTs: 0,
          actorId: e.writtenBy,
          sha256: "",
          shrinkChars: 0,
        },
      ],
    };
  },

  async getLoreRevision(
    entryId: string,
    revisionId: number,
  ): Promise<LoreRevisionView> {
    const detail = await mockApi.getLoreEntry(entryId);
    const row = detail.revisions.find((r) => r.revisionId === revisionId);
    if (!row) {
      throw mockApiError(
        `http 404 for /api/lore/entries/${entryId}/revisions/${revisionId}`,
        404,
        "lore revision not found",
      );
    }
    return {
      revisionId: row.revisionId,
      entryId: detail.entryId,
      body: detail.original,
      sha256: row.sha256,
      createdTs: row.createdTs,
      actorId: row.actorId,
      shrinkChars: row.shrinkChars,
    };
  },

  // ── T-33 對象審核 ────────────────────────────────────────────────────────
  async listPendingLoreEntities(): Promise<LorePendingEntityView[]> {
    return mockPendingEntities.map((e) => ({
      ...e,
      similar: e.similar.map((r) => ({ ...r })),
    }));
  },

  async approveLoreEntity(
    entityId: string,
    reason = "",
  ): Promise<LoreEntityGovernanceView> {
    const row = mockPendingEntities.find((e) => e.entityId === entityId);
    if (!row) {
      throw mockApiError(
        `http 404 for /api/lore/entities/${entityId}/approve`,
        404,
        "lore entity not found",
      );
    }
    // 核可之後它就不在佇列裡了 —— 佇列是待審的,不是全部對象的清單。
    mockPendingEntities = mockPendingEntities.filter(
      (e) => e.entityId !== entityId,
    );
    return {
      entityId: row.entityId,
      canonical: row.canonical,
      pending: false,
      mergedInto: "",
      kind: "approve",
      reason,
      actorId: "owner",
      createdTs: 0,
    };
  },

  async mergeLoreEntity(
    entityId: string,
    into: string,
    reason = "",
  ): Promise<LoreEntityGovernanceView> {
    const row = mockPendingEntities.find((e) => e.entityId === entityId);
    if (!row) {
      throw mockApiError(
        `http 404 for /api/lore/entities/${entityId}/merge`,
        404,
        "lore entity not found",
      );
    }
    if (into.trim() === "") {
      throw mockApiError(
        `http 422 for /api/lore/entities/${entityId}/merge`,
        422,
        "into is required",
      );
    }
    mockPendingEntities = mockPendingEntities.filter(
      (e) => e.entityId !== entityId,
    );
    return {
      entityId: row.entityId,
      canonical: row.canonical,
      pending: false,
      mergedInto: into,
      kind: "merge",
      reason,
      actorId: "owner",
      createdTs: 0,
    };
  },

  subscribeConnection(
    onState: (state: SseConnectionState) => void,
  ): () => void {
    // The mock has no transport, so it has no transport to lose: report "live"
    // once and never call back. This is the honest answer, not a stub — there
    // is nothing here that can go stale behind the owner's back, so the
    // connection banner must stay off in mock mode. Anything else (starting at
    // "connecting", say) would put a permanent "reconnecting" bar on a demo
    // that is not reconnecting to anything.
    onState("live");
    return () => {};
  },
};

// Reset hook for tests / hot-reload determinism (not used by the UI).
export function __resetMock(): void {
  // The ring is MUTATED by rotate/remove, so it belongs here: without this a
  // test that rotates leaves a two-key ring for whatever runs next, and the
  // failure lands on the innocent test.
  mockSigningKeys = structuredClone(MOCK_WIRE_SIGNING_KEYS);
  wireMembers = structuredClone(MOCK_WIRE_MEMBERS);
  wireMonitoring = structuredClone(MOCK_WIRE_MONITORING);
  mockBinStatus.clear();
  mockBinStatus.set("warden-mbp5", "stale");
  globalContextOverlay = null;
  BOOT_DOC_READ_ONLY = new Set<string>();
  bootDocOverlays.clear();
  roleOverlays.clear();
  customRoles.clear();
  lessonsOverlays.clear();
  insightOverlays.clear();
  documentHistories.clear();
  documentRows.clear();
  nextDocumentHistoryId = 1;
  chatLog = [];
  chatReads.clear();
  replyCards = [];
  tasks = [];
  artifactVersions = new Map();
  outsourceWorkers = [];
  taskManuals = [];
  mockPasswordSet = true;
  mockPassword = "mock-password";
  mockMfaOffered = false;
  mockMfaActive = false;
  mockMfaPending = false;
  mockServerSettings = { ...DEFAULT_MOCK_SETTINGS };
  // T-83ef: themes are their own store now, so resetting settings no longer
  // clears them — the reset hook has to name the store explicitly or a theme
  // saved in one test leaks into the next.
  mockThemes.clear();
  activationPendingNext = false;
  relocationPendingNext = false;
  relocationDeferredNext = false;
}

// Test-only hook: put the mock into the FIRST-RUN shape (no password set), the
// way a fresh install boots — so tests can exercise the first-run setup page
// against the same adapter the UI uses. Returns the claim token the mock
// accepts.
export function __setMockFirstRun(): string {
  mockPasswordSet = false;
  mockPassword = "";
  return MOCK_CLAIM_TOKEN;
}

// Test-only hook: land an INBOUND message (e.g. member → owner) in the mock log,
// the way a real agent reply would arrive server-side. The mock UI itself never
// fabricates one (see the chatLog note) — this exists so tests can exercise the
// unread/read seam (unreadCountOf ↔ markChatRead) against real log entries.
export function __injectMockChat(msg: ChatMessage): void {
  chatLog.push(msg);
}

// Test-only hook: land a reply card in the mock store, the way a live agent's
// create_reply_card would arrive server-side. The mock UI itself never
// fabricates an agent's ask (see the replyCards note) — this exists so tests
// can exercise the answer / re-answer / badge seam against real store entries.
export function __injectMockReplyCard(card: ReplyCard): void {
  replyCards.push(card);
  emitTopic("reply_card");
}

// Test-only hook: land a task in the mock store, the way a live agent's MCP
// create_task would arrive server-side. The mock UI itself never fabricates a
// task (see the tasks note) — this exists so tests can exercise the tasks
// page's list / filter / terminate / priority / message seams.
export function __injectMockTask(task: TaskView | MockTaskRow): void {
  // A plain `TaskView` is accepted because most callers only care about the
  // list / filter / terminate seams and pass no artifacts at all. The cast is
  // the one place the two artifact shapes meet: a caller that DOES pass rows is
  // passing STORE rows (whole deliverables), which is what `listTaskArtifacts`
  // hands back — pass index-only rows here and that read answers index-only
  // rows, which is the honest consequence of what was put in.
  tasks.push(task as MockTaskRow);
  emitTopic("task");
}

// Test-only hook: land the retained PREVIOUS versions of one pinned deliverable
// (T-60), newest first — what a sequence of `replace_task_artifact` calls would
// have left behind. There is no cockpit write that can produce them.
export function __injectMockArtifactVersions(
  artifactId: string,
  versions: TaskArtifactVersionView[],
): void {
  artifactVersions.set(artifactId, structuredClone(versions));
  emitTopic("task");
}

// Test-only hook: land one live telemetry session row, the way an agent's
// statusLine report would surface it under /api/monitoring. Members AND
// outsource workers (`ow-` id) ride the same array — this is what lets a test
// give a worker a REPORTED model/effort distinct from its configured one.
export function __injectMockMonitoringSession(s: WireMonSession): void {
  wireMonitoring.sessions = [
    ...wireMonitoring.sessions.filter((x) => x.id !== s.id),
    s,
  ];
  emitTopic("monitoring");
}

// Test-only hook: land a LIVE outsource worker (codename/model/effort bound to
// one task), the way the server's assignment would surface it.
export function __injectMockOutsourceWorker(w: OutsourceWorkerView): void {
  outsourceWorkers.push(w);
  emitTopic("outsource_worker");
}

// Test-only hook: inject a row as GET /api/members would return it.  Keeping
// this separate from __injectMockOutsourceWorker lets roster consumers test an
// outsource worker arriving through the newly-inclusive member-list contract.
export function __injectMockMember(
  over: Partial<WireMember> & Pick<WireMember, "id" | "kind">,
): void {
  wireMembers.push({
    ...structuredClone(MOCK_WIRE_MEMBERS[1]),
    name: over.id,
    ...over,
  });
  emitTopic("member");
}

// Test-only hook: register a task type (任務手冊) so the type filter offers it.
// Grows a full (blank-bodied) manual in the store — the type filter reads the
// light narrowing, the manual editor the full shape, one source of truth.
export function __injectMockTaskType(t: TaskTypeView): void {
  taskManuals.push({
    typeKey: t.typeKey,
    displayName: t.displayName,
    purpose: t.purpose,
    fields: [],
    sopMd: "",
    learnings: "",
    assignee: null,
    updatedTs: Date.now() / 1000,
  });
  emitTopic("task_manual");
}

// Test-only hook: land a FULL manual (fields/SOP/learnings/assignee) so tests
// can exercise the 設定 › 任務手冊 editor against a populated store entry.
export function __injectMockTaskManual(m: TaskManualView): void {
  taskManuals.push(structuredClone(m));
  emitTopic("task_manual");
}

// Test-only hook: seed the ONE first-run onboarding report, the way the server's
// own kick / finish / recover would have left the row on disk. Mock mode ships
// with no report at all (see the `onboarding: null` note on
// DEFAULT_MOCK_SETTINGS) — this is the seeding caller that note is waiting for.
// Without it the only reachable `onboarding_dismissed` branch is "no report",
// so the half of the guard that REFUSES a stamp on a run still `running` — the
// half the server comment calls the whole point — has nothing standing on it.
// Cleared by __resetMock along with the rest of the settings blob.
export function __injectMockOnboardingReport(
  report: NonNullable<WireServerSettings["onboarding"]>,
): void {
  mockServerSettings.onboarding = structuredClone(report);
}

// Test-only hook: flip a mock member's presence projection, the way the real
// hub's SSE connect/disconnect would. Exists so tests can exercise the M2-2
// delete-role 409 防線 (「有成員在線上，無法刪除」) — the mock UI itself never
// fabricates an online member.
export function __setMockMemberOnline(id: string, online: boolean): void {
  const w = wireMembers.find((m) => m.id === id);
  if (!w) throw new Error(`mock: no member ${id}`);
  w.presence = online ? "online" : "offline";
}

// Test/dev-only hook (T-7fa1): stage the "nothing was dispatched" answer so the
// wake-failure UI is reachable without an actually-unreachable warden. Sticky
// until flipped back or __resetMock — the condition it models (a machine whose
// warden is not listening) is itself sticky.
export function __setMockActivationPending(pending: boolean): void {
  activationPendingNext = pending;
}

// The relocate twin of __setMockActivationPending (T-7fa1).
export function __setMockRelocationPending(pending: boolean): void {
  relocationPendingNext = pending;
}

// Stage the DEFERRED half: pending says "not landed", this says "on purpose".
export function __setMockRelocationDeferred(deferred: boolean): void {
  relocationDeferredNext = deferred;
}

// Dev-only browser seam (T-160e UI screenshots). The __inject* hooks above are
// module-scoped, so a Playwright session driving the RUNNING dev app can't seed
// a task the way vitest does. Under Vite dev ONLY (import.meta.env.DEV), mirror
// the task-seeding hooks onto window so an automated 390px screenshot run can
// stage the exact fixtures the owner reviewed (a 轉派中 card, the reassign
// dialog) against real store entries. Stripped from any production build — the
// guard is a compile-time constant, so the whole block dead-code-eliminates.
if (import.meta.env.DEV) {
  (window as unknown as { __mockSeed?: unknown }).__mockSeed = {
    injectTask: __injectMockTask,
    injectOutsourceWorker: __injectMockOutsourceWorker,
    injectChat: __injectMockChat,
    setMemberOnline: __setMockMemberOnline,
    setActivationPending: __setMockActivationPending,
    setRelocationPending: __setMockRelocationPending,
    setRelocationDeferred: __setMockRelocationDeferred,
    reset: __resetMock,
  };
}
