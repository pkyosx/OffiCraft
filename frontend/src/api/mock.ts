// api/mock.ts — the mock adapter (wired in M1 via index.ts).
//
// Members come from an in-module WIRE-shape fixture (moved here from the old
// data/members.ts) so the mock exercises the SAME wire→view mapper the real
// HTTP adapter will use. Honesty is preserved end-to-end: no telemetry, no fake
// online, no fabricated timestamps.

import type {
  Member,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  BackupHealthView,
  GlobalContextView,
  DocumentKind,
  InsightView,
  DocumentHistoryView,
  DocumentSeedView,
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
  ChatMessage,
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
  TaskManualView,
  TaskManualPatch,
  DocSummaryView,
  DocView,
  MemberResumeSummaryView,
  ResumeTaskView,
} from "./adapter";
import type {
  WireMember,
  WireMonitoring,
  WireMonSession,
  WireVersion,
  WireBackupHealth,
  WireGlobalContext,
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
} from "./wire";
import {
  toMember,
  toMonitoring,
  toVersion,
  toReleaseCheck,
  toBackupHealth,
  toGlobalContext,
  toDocumentHistory,
  toDocumentSeed,
  toRoleDef,
  toBootstrap,
  toLessons,
  toInsight,
  toOnboardResult,
  toDeleteResult,
  toUninstallResult,
  toMachine,
  toServerSettings,
} from "./mappers";
import { DOC_CAP_CHARS_DEFAULTS } from "./docCap";
import {
  MOCK_OWNER_ID,
  SEED_SYSTEM_INTERACTION_MD,
  SEED_ROLE_ASSISTANT_MD,
  SEED_LESSONS_MD,
  SEED_INSIGHT_ASSISTANT_MD,
  SEED_BOOT_SEQUENCE_MD,
} from "./seeds";
import { ApiError } from "./errors";
import {
  validateThemeBundles,
  isValidDisplayTheme,
  type ThemeBundle,
} from "../lib/themeBundle";

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
    avatar_index: 0,
    member_no: "MB-WDN000",
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
    roster_status: "active",
    owner_id: "",
    unread_count: 0,
    schema_version: 2,
  },
  {
    id: "mira",
    avatar_index: 0,
    member_no: "MB-AST001",
    name: "Mira",
    kind: "assistant", // mirror the real seed (dal/seed.py: Mira kind="assistant")
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
    avatar_index: 0,
    member_no: "MB-WDN001",
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
// it is a read-only file seed with no endpoint by construction.
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
const roleOverlays = new Map<string, WireRoleDef>();
// Owner-created CUSTOM roles (M2-2): a wire doc per minted key (is_seed=false).
// Distinct from roleOverlays (edits over a seed) — a custom role IS its doc.
const customRoles = new Map<string, WireRoleDef>();

// Mirrors the server name pool (server/ocserverd/domain.go; M2 隨機成員名 mock parity):
// the server-side pool a no-name role create draws the founding member's name
// from. Never "Mira" (the seed identity stays unmistakable).
const MOCK_MEMBER_NAME_POOL = [
  "Nova", "Kai", "Ravi", "Luna", "Iris", "Milo", "Zara", "Theo",
  "Aria", "Ezra", "Vera", "Nico", "Suki", "Remy", "Isla", "Otis",
  "Faye", "Juno", "Cleo", "Enzo", "Mika", "Wren", "Lyra", "Dax",
] as const;

/** Pick a fresh pool name, excluding every current roster name (case-
 * insensitive) — mock parity with the server name picker (domain.go). Exhausted
 * pool falls back to a numeric-suffix candidate, always returning fresh. */
function pickMockMemberName(): string {
  const taken = new Set(wireMembers.map((m) => m.name.trim().toLowerCase()));
  const available = MOCK_MEMBER_NAME_POOL.filter(
    (n) => !taken.has(n.toLowerCase())
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
// Derives the display Member-ID ("MB-XXX###") from a roster id — byte-for-byte
// the server's domain.MemberNo (server/ocserverd/domain.go:160-168): SHA-256 the
// id, take the first 8 bytes big-endian as a uint64, peel three uppercase letters
// (n%26, n/=26) then three digits (n%1000). Stateless display projection, never a
// lookup key. BigInt keeps the uint64 arithmetic exact (the value overflows Number).
export async function deriveMemberNo(memberID: string): Promise<string> {
  const digest = new Uint8Array(
    await crypto.subtle.digest("SHA-256", new TextEncoder().encode(memberID))
  );
  let n = 0n;
  for (let i = 0; i < 8; i++) n = (n << 8n) | BigInt(digest[i]);
  let letters = "";
  for (let i = 0; i < 3; i++) {
    letters += String.fromCharCode(65 + Number(n % 26n));
    n /= 26n;
  }
  return `MB-${letters}${String(Number(n % 1000n)).padStart(3, "0")}`;
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
// Lessons OVERLAY (owner overlay ⊕ seed), keyed by `${role_key}::${task_type}`. A
// save stores the overlay so the folded read is now owner-edited
// (is_default=false); absent → the folded read is the REAL seed. PER-ROLE doc
// (per-role-learnings step1): agents sharing a role share the overlay.
const lessonsOverlays = new Map<string, WireLessons>();
const lessonsKey = (roleKey: string, taskType: string) =>
  `${roleKey}::${taskType}`;

// Insight OVERLAY (T-3809), keyed by the BARE `role_key`. ⚠️ NOT the lessons
// composite: insight has no task_type axis, so there is no "::" in this key and
// nothing may derive one key format from the other. An absent entry folds
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

// In-memory read receipts, keyed `{reader}::{peer}` → the monotonic last-read
// watermark. Mirrors the BE chat_read table. In mock mode the OWNER reads its own
// messages back and marks them read (via listChat / markChatRead), and — since the
// mock never fabricates a member reply — a message the owner sends to a member is
// marked read by that member ONLY if listChat(withId) is called as that member,
// which the single-owner mock UI never does. So the mock's "read ✓" is honest: it
// reflects only real recorded watermarks, never a fabricated peer read.
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
let tasks: TaskView[] = [];
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

/** Answer-input validation shared by answer/re-answer — mirrors the server's
 * 400s: an empty answer (no option, no text, no attachments) and an
 * out-of-range option_idx are both rejected. */
function validateReplyAnswer(card: ReplyCard, answer: ReplyCardAnswerInput) {
  const hasOption = answer.optionIdx !== undefined && answer.optionIdx !== null;
  const hasText = (answer.text ?? "").trim().length > 0;
  const hasAtts = (answer.attachments ?? []).length > 0;
  if (!hasOption && !hasText && !hasAtts) {
    throw new ApiError(
      `http 400 for answer /api/reply-cards/${card.id}/answer`,
      400,
      "bad_request",
      "an answer needs an option, text, or attachments"
    );
  }
  if (hasOption) {
    const idx = answer.optionIdx as number;
    if (idx < 0 || idx >= card.options.length) {
      throw new ApiError(
        `http 400 for answer /api/reply-cards/${card.id}/answer`,
        400,
        "bad_request",
        `option_idx ${idx} out of range`
      );
    }
  }
}

/** Build the stored answer view from the input — attachments echo back as
 * data-URI refs (the mock has no served blob endpoint), the SAME rule as
 * postChat, so previews render identically in mock mode. */
function toStoredReplyAnswer(
  answer: ReplyCardAnswerInput,
  stamp: number
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
    }
  );
  return {
    optionIdx: answer.optionIdx ?? null,
    text: (answer.text ?? "").trim(),
    attachments,
  };
}

function findReplyCard(id: string): ReplyCard {
  const card = replyCards.find((c) => c.id === id);
  if (!card) {
    throw new ApiError(
      `http 404 for /api/reply-cards/${id}`,
      404,
      "not_found",
      `reply card '${id}' not found`
    );
  }
  return card;
}

/** Read-time join mirroring the server's `reply_card_status`: the CURRENT
 * status of the card a chat message / task step carries, or null when it
 * carries none (or the card is missing). Computed at read time — the mock,
 * like the server, never stores this on the message/step. */
function mockReplyCardStatusOf(
  replyCardId: string | null
): "waiting" | "answered" | "expired" | null {
  if (!replyCardId) return null;
  const card = replyCards.find((c) => c.id === replyCardId);
  return card ? card.status : null;
}

/** Terminal task statuses (spec: 已完成/終止 為終態) — shared by the mock's
 * count / terminate / priority guards (mirrors the server's closed-set rule). */
const TERMINAL_TASK_STATUSES = new Set(["done", "terminated", "duplicated"]);

// The server's task_no projection of an id (domain.go TaskNo) — the mock needs
// it for a dep whose task row is absent, exactly where the server derives it
// rather than leaving the number blank (T-a3e4).
function deriveMockTaskNo(taskId: string): string {
  return `T-${taskId.slice(2, 6)}`;
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
    throw new ApiError(
      `http 404 for /api/task-manuals/${typeKey}`,
      404,
      "not_found",
      `task type '${typeKey}' not found`
    );
  }
  return m;
}

function findTask(id: string): TaskView {
  const t = tasks.find((x) => x.id === id);
  if (!t) {
    throw new ApiError(
      `http 404 for /api/tasks/${id}`,
      404,
      "not_found",
      `task '${id}' not found`
    );
  }
  return t;
}

function markRead(reader: string, peer: string, lastReadTs: number): ChatReadReceipt {
  const key = `${reader}::${peer}`;
  const prior = chatReads.get(key);
  // Monotonic: keep the higher watermark (a stale report never rewinds it).
  if (prior && prior.lastReadTs >= lastReadTs) return prior;
  const receipt: ChatReadReceipt = { readerId: reader, peerId: peer, lastReadTs };
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
  const watermark =
    chatReads.get(`${MOCK_OWNER_ID}::${peer}`)?.lastReadTs ?? 0;
  return chatLog.filter(
    (m) => m.to === MOCK_OWNER_ID && m.from === peer && m.ts > watermark
  ).length;
}

/** Fold the user-custom block: overlay ⊕ the EMPTY seed (a structuredClone so
 * the caller can never mutate our state). Mirrors fold_user_context. */
function foldGlobalContext(): WireGlobalContext {
  const folded = structuredClone(
    globalContextOverlay ?? MOCK_WIRE_USER_CONTEXT_EMPTY
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
    roleOverlays.get(key) ?? customRoles.get(key) ?? roleSeed(key)
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
const documentHistories = new Map<string, WireDocumentHistory[]>();
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
  throw new ApiError(
    `http 400 for ${call}`,
    400,
    "bad_request",
    RETIRED_DOCUMENT_KIND_MSG
  );
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
  if (kind === "task_description") return tasks.some((t) => t.id === key);
  return documentRows.has(historySlot(kind, key));
}

/** Drop one document's retained revisions along with the document itself — the
 * server does this in the SAME transaction as the delete (dal.go DeleteRoleDef
 * / DeleteTaskManual), so a deleted document leaves no readable echo behind. */
function dropDocumentHistory(kind: DocumentKind, key: string): void {
  documentHistories.delete(historySlot(kind, key));
  documentRows.delete(historySlot(kind, key));
}

/** Every "<role>::<task_type>" lessons document of one role. Matched by an
 * explicit PREFIX, exactly like DeleteLessonsOfRole: the key is compound, so
 * dropping only `<role>::general` would leave every other task type's history
 * readable after the role is gone. */
function dropRoleLessonsHistory(roleKey: string): void {
  const prefix = `${roleKey}::`;
  for (const slot of [...documentHistories.keys()]) {
    if (slot.startsWith(historySlot("lessons", prefix))) {
      documentHistories.delete(slot);
    }
  }
  for (const slot of [...documentRows]) {
    if (slot.startsWith(historySlot("lessons", prefix))) documentRows.delete(slot);
  }
  for (const key of [...lessonsOverlays.keys()]) {
    if (key.startsWith(prefix)) lessonsOverlays.delete(key);
  }
}

/** The one insight document of one role (T-3809). 🔴 EXACT EQUALITY, not the
 * prefix match its lessons twin above uses — and the difference is load-bearing
 * in BOTH directions:
 *
 *   * the lessons key is compound, so it needs the prefix or sibling task types
 *     survive the role's deletion;
 *   * the insight key is the bare role_key with no "::" terminator, so copying
 *     that prefix match here would delete r-abcdef's retained versions while
 *     deleting r-abc. The server's cascade makes exactly this distinction
 *     (dal.go, DeleteInsightOfRole) and this mock has to agree with it.
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
  key: string
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
  documentHistories.set(slot, kept.slice(0, DOCUMENT_HISTORY_CAP));
}

/** Write a retained revision back over the live document + fan the doc's own
 * SSE topic, so every surface reading it reconciles by refetch. */
function applyDocumentHistory(
  kind: DocumentKind,
  key: string,
  content: Record<string, string>
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
      const [roleKey, taskType] = key.split("::");
      if (tombstoned) {
        lessonsOverlays.delete(key);
      } else {
        lessonsOverlays.set(key, {
          ...docSizeFields(content.text ?? "", "learning"),
          role_key: roleKey,
          task_type: taskType,
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
  }
}

/** Map a wire member → view Member, folding in the M1-only view extras. */
function mapWithExtras(w: WireMember): Member {
  return toMember(w);
}

function findWire(id: string): WireMember {
  const w = wireMembers.find((m) => m.id === id);
  if (!w) throw new Error(`mock: member not found: ${id}`);
  return w;
}

// ── mock owner credential + settings state (B3) ─────────────────────────────
// The mock boots "installed": password set (AuthGate's mock mode never shows
// the first-run page anyway), default settings. Same validation rules as the
// server so the UI's error paths are exercisable offline.
let mockPasswordSet = true;
let mockPassword = "mock-password";
const DEFAULT_MOCK_SETTINGS = {
  token_ttl: 86400,
  handover_pct: 50,
  codex_compaction_threshold: 3,
  monitoring_refresh_seconds: 5,
  // M3 global outsource cap — mirrors the server's code-side default (3).
  outsource_max_parallel: 3,
  // T-ae38 document size caps — mirror the server's shipped defaults, which
  // this module already imports rather than restating (Duty has its own,
  // smaller one; the other three share).
  doc_cap_chars_duty: DOC_CAP_CHARS_DEFAULTS.duty,
  doc_cap_chars_insight: DOC_CAP_CHARS_DEFAULTS.insight,
  doc_cap_chars_learning: DOC_CAP_CHARS_DEFAULTS.learning,
  doc_cap_chars_manual: DOC_CAP_CHARS_DEFAULTS.manual,
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
  // Custom theme bundles (T-16a1 P2) — none saved out of the box, mirroring the
  // server (display.custom_themes absent).
  custom_themes: [] as ThemeBundle[],
};

/** Mirror of the server's per-document size/cap reporting (T-3aeb). Runes, not
 * UTF-16 units — same reason docCap.ts spells it [...s].length.
 *
 * T-ae38: the cap is now per SEGMENT, so the caller names which of the four it
 * is judged by. Passing the wrong one here would make the mock disagree with
 * the server about a doc's remaining budget — the one thing this helper exists
 * to keep honest. */
function docSizeFields(
  text: string,
  cap: "duty" | "insight" | "learning" | "manual"
) {
  return {
    size_chars: [...text].length,
    cap_chars: {
      duty: mockServerSettings.doc_cap_chars_duty,
      insight: mockServerSettings.doc_cap_chars_insight,
      learning: mockServerSettings.doc_cap_chars_learning,
      manual: mockServerSettings.doc_cap_chars_manual,
    }[cap],
  };
}
let mockServerSettings = { ...DEFAULT_MOCK_SETTINGS };
const MOCK_CLAIM_TOKEN = "mock-claim-token";
const TOKEN_TTL_CHOICES = new Set([43200, 86400, 604800, 2592000]);

function mockAvatarIndex(kind: "member" | "outsource"): number {
  const active = mockServerSettings.custom_themes.find(
    (theme) => theme.id === mockServerSettings.display_theme,
  );
  const pool = active?.avatarPools?.[kind] ?? [];
  return pool.length > 0 ? Math.floor(Math.random() * pool.length) : 0;
}

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
      Math.floor(Math.random() * 36).toString(36)
    ).join("")
  );
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
    if (w.roster_status === "removed") throw new Error(`mock: member removed: ${id}`);
    // unread_count is COMPUTED here exactly as listMembers computes it — the Go
    // single-member handler runs the same `unreadCountsForRequest` as the list
    // (T-8115 review). Serving the static fixture value instead would make the
    // mock DISAGREE with the server, which is what let a badge regression ship.
    return mapWithExtras({ ...w, unread_count: unreadCountOf(id) });
  },

  async updateMemberAvatarIndex(id: string, avatarIndex: number): Promise<void> {
    if (!Number.isInteger(avatarIndex) || avatarIndex < 0) {
      throw new Error("mock: avatar_index must be a non-negative integer");
    }
    const worker = outsourceWorkers.find((item) => item.id === id);
    if (worker) {
      worker.avatarIndex = avatarIndex;
      emitTopic("outsource_worker");
      return;
    }
    const member = findWire(id);
    member.avatar_index = avatarIndex;
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

  async forceStopMember(id: string): Promise<void> {
    // Immediate kill escalation (mirror handle_force_stop_member): write
    // desired_state=offline and fall to offline. The mock has no live agent/warden to
    // SIGKILL, so — like deactivate — it simply lands offline; the real backend
    // dispatches the robust STOP to the warden immediately, bypassing the grace.
    const w = findWire(id);
    w.desired_state = "offline";
    w.presence = "offline";
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
      if (!["low", "medium", "high"].includes(patch.effort)) {
        throw new ApiError(
          `http 422 for PATCH /api/members/${id}`,
          422,
          "validation_error",
          "effort must be one of ['high', 'low', 'medium']"
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
    input: WebhookCreateInput
  ): Promise<WebhookEndpoint> {
    findWire(memberId);
    const endpointId = input.endpointId.trim();
    // Same closed charset the server enforces (422 → throw).
    if (!/^[A-Za-z0-9_-]+$/.test(endpointId)) {
      throw new ApiError(
        `http 422 for POST /api/members/${memberId}/webhooks`,
        422,
        "validation_error",
        "endpoint id may contain only letters, digits, '_' and '-'"
      );
    }
    const list = mockWebhooks.get(memberId) ?? [];
    if (list.some((e) => e.endpointId === endpointId)) {
      throw new ApiError(
        `http 409 for POST /api/members/${memberId}/webhooks`,
        409,
        "conflict",
        `a webhook endpoint '${endpointId}' already exists for this member`
      );
    }
    const platform = input.platform ?? "generic";
    const secret = input.signingSecret?.trim() ?? "";
    // slack/github require a signing secret (server 422 parity); generic ignores it.
    if ((platform === "slack" || platform === "github") && !secret) {
      throw new ApiError(
        `http 422 for POST /api/members/${memberId}/webhooks`,
        422,
        "validation_error",
        `signing_secret is required when platform is '${platform}'`
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
    patch: WebhookUpdate
  ): Promise<WebhookEndpoint> {
    const list = mockWebhooks.get(memberId) ?? [];
    const e = list.find((x) => x.endpointId === endpointId);
    if (!e) {
      throw new ApiError(
        `http 404 for PATCH /api/members/${memberId}/webhooks/${endpointId}`,
        404,
        "not_found",
        `webhook endpoint '${endpointId}' not found`
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
      throw new ApiError(
        `http 404 for DELETE /api/members/${memberId}/webhooks/${endpointId}`,
        404,
        "not_found",
        `webhook endpoint '${endpointId}' not found`
      );
    }
    mockWebhooks.set(
      memberId,
      list.filter((x) => x.endpointId !== endpointId)
    );
    mockWebhookSecrets.delete(secretKey(memberId, endpointId));
  },

  async listWebhookRequests(
    memberId: string,
    endpointId: string
  ): Promise<WebhookRequestLog[]> {
    // Server parity: the last 5 raw /in requests, newest first. Endpoints
    // without simulated traffic honestly read empty.
    const rows = mockWebhookRequests.get(`${memberId} ${endpointId}`) ?? [];
    return rows.map((r) => ({ ...r }));
  },

  async getMemberResumeSummary(
    memberId: string
  ): Promise<MemberResumeSummaryView> {
    findWire(memberId); // 404 parity: an unknown member throws

    // Mirrors the CHAT/TASKS/OVERVIEW SUBSET of the server's
    // resumeSnapshotParts(actor=memberId): a BOUNDED recent-chat window
    // involving the member, the member's NON-TERMINAL executed tasks (LIGHT
    // rows, most recently updated first), and an overview computed FROM those
    // two lists — never a separately-fabricated count, so it cannot drift from
    // what the lists actually carry (same honesty contract the real endpoint's
    // shared assembly gives). READ-ONLY: unlike listChat, this never advances a
    // read watermark.
    //
    // NOT mocked: the T-1b09 studio-floor blocks (roster / machines) and their
    // roster_chars / machines_chars sizes. Said explicitly because "mirrors the
    // server" is exactly the kind of claim a reader trusts without checking —
    // a mock that silently covers less than it says it does is worse than one
    // that admits the gap.
    const RESUME_CHAT_N = 5;
    const RESUME_TASKS_N = 5;

    const chatAll = chatLog
      .filter((m) => m.from === memberId || m.to === memberId)
      .sort((a, b) => a.ts - b.ts);
    const chat = chatAll.slice(-RESUME_CHAT_N).map((m) => ({ ...m }));

    const openTasksAll = tasks.filter(
      (t) =>
        t.executorKind === "member" &&
        t.executorId === memberId &&
        !TERMINAL_TASK_STATUSES.has(t.status)
    );
    const openTasksSorted = [...openTasksAll].sort(
      (a, b) => b.updatedTs - a.updatedTs
    );
    const tasksOut: ResumeTaskView[] = openTasksSorted
      .slice(0, RESUME_TASKS_N)
      .map((t) => {
        // First non-terminal step (superseded skipped like done — same rule
        // the workflow timeline uses); "" when the plan is empty/complete.
        const step = t.steps.find((s) => !TERMINAL_STEP_STATUSES.has(s.status));
        const detailChars = t.steps.reduce(
          (sum, s) => sum + s.name.length + s.dod.length,
          0
        );
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
        };
      });

    const cardsForMember = replyCards.filter((c) => c.from === memberId);
    const dayAgoTs = Date.now() / 1000 - 86400;
    const cardsWaiting = cardsForMember.filter(
      (c) => c.status === "waiting"
    ).length;
    const cardsAnsweredRecent = cardsForMember.filter(
      (c) => c.status === "answered" && (c.answeredTs ?? 0) >= dayAgoTs
    ).length;

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
      },
      note:
        "BOUNDED snapshot — recent chat + open tasks only; page the rest with list_chat / list_tasks / get_task.",
    };
  },

  async listChat(
    withId: string,
    limit?: number,
    before?: ChatCursor
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
          (m.ts === before.beforeTs && m.id < before.beforeId)
      );
    }
    if (limit !== undefined && limit >= 0) {
      msgs = limit === 0 ? [] : msgs.slice(-limit);
    }
    // AUTO READ-RECEIPT: listing a conversation IS reading it — advance the
    // OWNER's watermark for this peer to the newest message ts (mirrors the BE
    // handle_list_chat auto-mark; the mock caller is always the owner).
    // A HISTORY PAGE (cursor present) NEVER advances the watermark (BE
    // parity): reading old context is not reading the newest messages.
    if (!before && msgs.length > 0) {
      const newest = Math.max(...msgs.map((m) => m.ts));
      markRead(MOCK_OWNER_ID, withId, newest);
    }
    // Read-time reply_card_status join (server parity) — a copy per message so
    // callers never mutate the log.
    return msgs.map((m) => ({
      ...m,
      replyCardStatus: mockReplyCardStatusOf(m.replyCardId),
    }));
  },

  async peekChat(withId: string, limit = 30): Promise<ChatMessage[]> {
    // READ-ONLY twin of listChat: SAME filter/order/window, but NEVER advances
    // the owner's read watermark (mirrors the BE ?peek=true — filter+cap by
    // ?with= with no auto-mark). Used while the owner is not actually looking
    // (backgrounded window) so the unread badge keeps counting. Default 30
    // mirrors the http adapter (and the server default) so mock and http return
    // the same window for a bare peekChat(withId).
    let msgs = chatLog
      .filter((m) => m.from === withId || m.to === withId)
      .sort((a, b) => a.ts - b.ts);
    if (limit >= 0) {
      msgs = limit === 0 ? [] : msgs.slice(-limit);
    }
    return msgs.map((m) => ({
      ...m,
      replyCardStatus: mockReplyCardStatusOf(m.replyCardId),
    }));
  },

  async listChatAttachments(withId: string): Promise<GalleryAttachment[]> {
    // Mirrors the BE gallery query (handle_list_chat_attachments): flatten the
    // attachments of EVERY logged message the member participates in
    // (owner↔member both directions + inter-agent threads), newest→oldest,
    // each row carrying the sender id + the roster-resolved display name
    // ("" for the owner — the UI renders its own 「我」 label). READ-ONLY:
    // unlike listChat this never advances a read watermark. HONEST: derived
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
            : wireMembers.find((w) => w.id === m.from)?.name ?? "",
        to: m.to,
        ts: m.ts,
      }))
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
    const sent: ChatMessage = {
      id: `mock-${stamp}`,
      from: MOCK_OWNER_ID,
      to: msg.to,
      body: msg.body,
      ts: stamp / 1000,
      attachments,
      // Only an agent's create_reply_card ever stamps a card link — an
      // owner-posted message never carries one (mirrors the server).
      replyCardId: null,
      replyCardStatus: null,
    };
    chatLog.push(sent);
    return sent;
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
    status: "waiting" | "answered" | "expired"
  ): Promise<ReplyCard[]> {
    // Mirror the server's list contract: waiting = longest-waiting first
    // (created asc); answered = last-24h window, newest answer first; expired
    // = last-24h window keyed off expiredTs, newest first. The structuredClone
    // keeps callers from mutating mock state.
    if (status === "waiting") {
      return structuredClone(
        replyCards
          .filter((c) => c.status === "waiting")
          .sort((a, b) => a.createdTs - b.createdTs)
      );
    }
    const cutoff = Date.now() / 1000 - 24 * 3600;
    if (status === "expired") {
      return structuredClone(
        replyCards
          .filter(
            (c) =>
              c.status === "expired" &&
              (c.expiredTs ?? 0) >= cutoff
          )
          .sort((a, b) => (b.expiredTs ?? 0) - (a.expiredTs ?? 0))
      );
    }
    return structuredClone(
      replyCards
        .filter(
          (c) =>
            c.status === "answered" &&
            c.answeredTs !== null &&
            c.answeredTs >= cutoff
        )
        .sort((a, b) => (b.answeredTs ?? 0) - (a.answeredTs ?? 0))
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
          c.answeredTs >= cutoff
      ).length,
      expired: replyCards.filter(
        (c) => c.status === "expired" && (c.expiredTs ?? 0) >= cutoff
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
        m.ts >
          (chatReads.get(`${MOCK_OWNER_ID}::${m.from}`)?.lastReadTs ?? 0)
    ).length;
  },

  async answerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput
  ): Promise<ReplyCard> {
    // The one-shot close (mirrors handle_answer_reply_card): only a WAITING
    // card is answerable — already answered → 409 (revise via re-answer);
    // empty / out-of-range → 400. Any real answer — including a typed
    // counter-question — closes the card.
    const card = findReplyCard(id);
    if (card.status !== "waiting") {
      throw new ApiError(
        `http 409 for POST /api/reply-cards/${id}/answer`,
        409,
        "conflict",
        `reply card '${id}' is already answered`
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
    answer: ReplyCardAnswerInput
  ): Promise<ReplyCard> {
    // 重新決定 (mirrors handle_reanswer_reply_card): only an ANSWERED card is
    // revisable (waiting → 409 — answer it first); the answer is replaced
    // wholesale, answeredTs re-stamps, status STAYS answered (a revision never
    // reopens the card or re-counts the badge).
    const card = findReplyCard(id);
    if (card.status !== "answered") {
      throw new ApiError(
        `http 409 for PUT /api/reply-cards/${id}/answer`,
        409,
        "conflict",
        `reply card '${id}' is not answered yet`
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
    // stays null). Releasing the task/step hold mirrors the server's
    // releaseCardHold: the bound step returns to in_progress, and the task
    // follows unless another waiting card still holds it; a terminal task is
    // left untouched (the orphan exit).
    const card = findReplyCard(id);
    if (card.status !== "waiting") {
      throw new ApiError(
        `http 409 for POST /api/reply-cards/${id}/expire`,
        409,
        "conflict",
        `reply card '${id}' is already ${card.status} — only a waiting card can expire`
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
          task.steps.some((st) => st.replyCardId === c.id)
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
      // Full task carries the resolved set; count kept == length (server parity).
      artifacts: task.artifacts ?? [],
      artifactCount: (task.artifacts ?? []).length,
    };
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
    // Mirrors handle_terminate_task: the ONLY owner-side status change;
    // non-terminal only (done/terminated → 409). Stamps closedTs and releases
    // any bound outsource worker (the live list drops it — the card's 外包
    // display honestly falls back to the bare label).
    const t = findTask(id);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw new ApiError(
        `http 409 for POST /api/tasks/${id}/terminate`,
        409,
        "conflict",
        `task '${id}' is already closed`
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
      new ApiError(
        `http 409 for POST /api/tasks/${id}/duplicate`,
        409,
        "conflict",
        detail
      );
    if (!duplicateOf.trim()) {
      throw new ApiError(
        `http 422 for POST /api/tasks/${id}/duplicate`,
        422,
        "validation_error",
        "duplicate_of must not be blank"
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
      throw new ApiError(
        `http 404 for POST /api/tasks/${id}/duplicate`,
        404,
        "not_found",
        `duplicate_of task '${duplicateOf}' not found`
      );
    }
    if (original.status === "duplicated") {
      throw conflict(
        `duplicate_of task '${duplicateOf}' is itself a duplicate; point at the ` +
          `final original it duplicates (${original.duplicateOf})`
      );
    }
    if (tasks.some((x) => x.duplicateOf === id)) {
      throw conflict(
        `task '${id}' is already the original of another duplicate; it cannot ` +
          `itself be marked duplicated`
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
    description: string
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
    // The absent-vs-empty distinction lives one layer up (the wire's optional
    // `description`); this seam's argument is always a concrete string, so ""
    // means clear, exactly as the http twin sends it.
    const t = findTask(id);
    if (t.description === description) return t;
    recordDocumentHistory("task_description", id);
    t.description = description;
    t.updatedTs = Date.now() / 1000;
    emitTopic("task");
    return t;
  },

  async setTaskPriority(id: string, priority: string): Promise<TaskView> {
    // Mirrors handle_set_task_priority: closed → 409; the closed high|mid|
    // low|frozen vocabulary → 422 otherwise (freeze/unfreeze ride this knob).
    // T-0786: the server also enforces executor/frozen authz rules for
    // non-owner callers — the mock is the owner's cockpit view, so those
    // 403 faces never arise here.
    const t = findTask(id);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw new ApiError(
        `http 409 for POST /api/tasks/${id}/priority`,
        409,
        "conflict",
        `task '${id}' is closed`
      );
    }
    if (!["high", "mid", "low", "frozen"].includes(priority)) {
      throw new ApiError(
        `http 422 for POST /api/tasks/${id}/priority`,
        422,
        "validation_error",
        "priority must be one of ['frozen', 'high', 'low', 'mid']"
      );
    }
    t.priority = priority;
    t.updatedTs = Date.now() / 1000;
    emitTopic("task");
    return structuredClone(t);
  },

  async reassignTask(id: string, input: TaskReassignInput): Promise<TaskView> {
    // Mirrors handle_reassign_task (T-160e): expire the task's waiting cards,
    // rewind non-terminal steps to pending, dismiss the OLD outsource worker,
    // mint the new one when the target is 外包, move the task to `reassigning`
    // and notify BOTH member sides to hand over. The NEW executor reports the
    // task back to in_progress — the mock never flips it here either.
    const t = findTask(id);
    const badRequest = (detail: string) =>
      new ApiError(
        `http 400 for POST /api/tasks/${id}/reassign`,
        400,
        "bad_request",
        detail
      );
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw new ApiError(
        `http 409 for POST /api/tasks/${id}/reassign`,
        409,
        "conflict",
        `task '${id}' is already closed (${t.status})`
      );
    }
    if (t.priority === "frozen") {
      throw badRequest(`task '${id}' is frozen; unfreeze it before reassigning`);
    }

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
          `target member '${target.memberId}' is not an active roster member`
        );
      }
      if (m.kind === "warden") {
        throw badRequest(
          `target member '${target.memberId}' is a machine (warden) — machines never execute tasks`
        );
      }
      if (t.executorKind === "member" && t.executorId === target.memberId) {
        throw new ApiError(
          `http 409 for POST /api/tasks/${id}/reassign`,
          409,
          "conflict",
          `member '${target.memberId}' is already the task's executor`
        );
      }
      newMember = m;
    } else {
      const effort = target.effort.trim() || "medium";
      if (!["low", "medium", "high"].includes(effort)) {
        throw badRequest("target.effort must be one of low, medium, high");
      }
      // The machine preference is a SPAWN-time knob with no mock surface (no
      // scheduler here) — validated by the server, dropped honestly here.
      newWorker = {
        id: `ow-mock-${Date.now().toString(16)}`,
        avatarIndex: mockAvatarIndex("outsource"),
        codename: deriveCodename(
          target.model.trim(),
          outsourceWorkers.map((w) => w.codename)
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
    const newExecutorLabel = newMember
      ? newMember.name || newMember.id
      : `外包 ${newWorker!.codename}`;
    const newExecutorId = newMember ? newMember.id : newWorker!.id;
    if (oldExecutor) {
      chatLog.push({
        id: `mock-reassign-old-${stamp}`,
        from: "system",
        to: oldExecutor,
        body:
          `[${t.taskNo}] 此任務已轉派給 ${newExecutorLabel}（id \`${newExecutorId}\`）。` +
          `請停止推進，改為去跟他做交接：主動 post_chat 給他，回答他關於目前進度、在飛事項、要注意的坑的提問，` +
          `直到他確認交接完成。交接完成後這張任務就不再是你的了。`,
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    }
    const note = input.note?.trim();
    if (oldExecutor) {
      const predecessorLabel =
        oldKind === "outsource"
          ? `外包 ${outsourceWorkers.find((w) => w.id === oldExecutor)?.codename ?? oldExecutor}`
          : wireMembers.find((m) => m.id === oldExecutor)?.name || oldExecutor;
      chatLog.push({
        id: `mock-reassign-new-${stamp}`,
        from: "system",
        to: newExecutorId,
        body:
          `[${t.taskNo}] 你接手了任務「${t.title}」。你的前任是 ${predecessorLabel}（id \`${oldExecutor}\`）。` +
          `請先跟他確認交接完成（直接 post_chat 給他，問清楚目前進度與在飛事項），` +
          `確認後再由你自己呼叫 claim_task（認領）解除轉派鎖——只有你這個新負責人動得了；任務狀態一律照步驟推導，不必也不能自己報。` +
          (note ? `\n\n交接備註：${note}` : ""),
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    } else if (newMember) {
      chatLog.push({
        id: `mock-reassign-new-${stamp}`,
        from: "system",
        to: newMember.id,
        body:
          `[${t.taskNo}] 你接手了任務「${t.title}」。請先讀任務內容，` +
          `準備好後由你自己呼叫 claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。` +
          (note ? `\n\n交接備註：${note}` : ""),
        ts: stamp / 1000,
        attachments: [],
        replyCardId: null,
      });
    }
    emitTopic("task");
    emitTopic("outsource_worker");
    return structuredClone(t);
  },

  async removeTaskArtifact(taskId: string, artifactId: string): Promise<TaskView> {
    // Mirrors handle_remove_task_artifact (T-3dc5): the owner/admin un-pin.
    // Closed task → 409 (T-2654: the deliverable set is frozen in BOTH
    // directions, so un-pin is refused exactly like add). Unknown artifact →
    // 404, wrong-task ownership → 400. The blob is left intact (the mock has no
    // blob store to touch). The 409 must come BEFORE the artifact lookup, same
    // as the server — a mock that deletes where production refuses is worse
    // than no mock at all, since the mock cockpit is how UI changes get checked.
    const t = findTask(taskId);
    if (TERMINAL_TASK_STATUSES.has(t.status)) {
      throw new ApiError(
        `http 409 for DELETE /api/tasks/${taskId}/artifact/${artifactId}`,
        409,
        "conflict",
        `task '${taskId}' is closed (${t.status}) — its deliverables are frozen`
      );
    }
    const arts = t.artifacts ?? [];
    const art = arts.find((a) => a.id === artifactId);
    if (!art) {
      throw new ApiError(
        `http 404 for DELETE /api/tasks/${taskId}/artifact/${artifactId}`,
        404,
        "not_found",
        `artifact '${artifactId}' not found`,
      );
    }
    t.artifacts = arts.filter((a) => a.id !== artifactId);
    t.artifactCount = t.artifacts.length;
    emitTopic("task");
    return structuredClone(t);
  },

  async postTaskMessage(id: string, msg: TaskMessageInput): Promise<void> {
    // Mirrors handle_post_task_message: ONE ordinary chat message owner →
    // the task's executor (the mock pushes it into the shared chatLog, so the
    // executor's thread shows it exactly like a hand-typed message). An
    // unassigned executor is a 409; an empty message a 400.
    const t = findTask(id);
    if (!t.executorId) {
      throw new ApiError(
        `http 409 for POST /api/tasks/${id}/message`,
        409,
        "conflict",
        `task '${id}' has no executor yet`
      );
    }
    const trimmed = msg.body.trim();
    const hasBody = trimmed.length > 0;
    const hasAtts = (msg.attachments ?? []).length > 0;
    if (!hasBody && !hasAtts) {
      throw new ApiError(
        `http 400 for POST /api/tasks/${id}/message`,
        400,
        "bad_request",
        "a message needs a body or attachments"
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
      throw new ApiError(
        `http 404 for GET /api/outsource-workers/${id}`,
        404,
        "not_found",
        `outsource worker ${id} not found`
      );
    }
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async relocateWorker(
    id: string,
    machineId: string
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
      throw new ApiError(
        `http 404 for POST /api/outsource-workers/${id}/relocate`,
        404,
        "not_found",
        `outsource worker ${id} not found`
      );
    }
    w.desiredMachineId = machineId;
    if (machineId !== "") {
      const m = wireMembers.find(
        (x) => x.id === machineId && x.kind === "warden"
      );
      // A concrete pin the server would reject (unknown id) still 404s there;
      // here the picker only offers real online machines, so resolve honestly.
      w.machine = m ? m.name : machineId;
    }
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async refocusWorker(id: string): Promise<OutsourceWorkerView> {
    // 換手 (T-32e1). The mock models the server's observable outcome: online-only
    // (409 unless presence "online"), stopped → 409, unknown/released → 404. On
    // success stamp refocus_since (the panel's 換手中 acknowledgement); the actual
    // kill+respawn is server-side, invisible here, so lifecycle is left untouched.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw new ApiError(
        `http 404 for POST /api/outsource-workers/${id}/refocus`,
        404, "not_found", `outsource worker ${id} not found`
      );
    }
    if (w.desiredState === "offline") {
      throw new ApiError(
        `http 409 for POST /api/outsource-workers/${id}/refocus`,
        409, "conflict", "worker is stopped — restart it before refocusing"
      );
    }
    if (w.presence !== "online") {
      throw new ApiError(
        `http 409 for POST /api/outsource-workers/${id}/refocus`,
        409, "conflict", "refocus requires the worker to be online"
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
    // 停止 (T-f190). Held down: set desired_state offline (member parity), clear any
    // in-flight refocus, project presence "stopped". unknown/released → 404.
    // Idempotent.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw new ApiError(
        `http 404 for POST /api/outsource-workers/${id}/stop`,
        404, "not_found", `outsource worker ${id} not found`
      );
    }
    w.desiredState = "offline";
    w.refocusSince = null;
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
      throw new ApiError(
        `http 404 for POST /api/outsource-workers/${id}/restart`,
        404, "not_found", `outsource worker ${id} not found`
      );
    }
    // Mock ↔ http parity (T-7526): the guard asks whether the worker is ALIVE,
    // not whether anyone pressed 停止. A worker whose session died on its own
    // keeps desired_state=online and must still be revivable; a genuinely live
    // one is still refused.
    if (w.desiredState !== "offline" && w.presence === "online") {
      throw new ApiError(
        `http 409 for POST /api/outsource-workers/${id}/restart`,
        409, "conflict", "worker is still online — nothing to restart"
      );
    }
    w.desiredState = "online";
    w.presence = "waking";
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async setWorkerModel(
    id: string,
    patch: { model: string; effort?: string }
  ): Promise<OutsourceWorkerView> {
    // 換 model (T-f190). Persist model/effort; the respawn-to-take-effect-now is
    // server-side (invisible here). unknown/released → 404.
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w || w.status === "released") {
      throw new ApiError(
        `http 404 for POST /api/outsource-workers/${id}/model`,
        404, "not_found", `outsource worker ${id} not found`
      );
    }
    w.model = patch.model;
    if (patch.effort !== undefined && patch.effort !== "") w.effort = patch.effort;
    emitTopic("outsource_worker");
    return {
      ...withWorkerTaskJoin(structuredClone(w)),
      unreadCount: unreadCountOf(w.id),
    };
  },

  async getWorkerBootContext(id: string): Promise<string> {
    // Honest preview mirroring the backend buildWorkerBootContext order
    // (seed → 你的身分 → 你的任務 → 任務手冊), re-assembled from the CURRENT
    // mock rows — never a stored spawn-time text, never a token (T-ba6b).
    const w = outsourceWorkers.find((x) => x.id === id);
    if (!w) {
      throw new ApiError(
        `http 404 for GET /api/outsource-workers/${id}/boot-context`,
        404, "not_found", `outsource worker ${id} not found`
      );
    }
    const task = tasks.find((x) => x.id === w.taskId);
    if (!task) {
      throw new ApiError(
        `http 404 for GET /api/outsource-workers/${id}/boot-context`,
        404, "not_found", `task ${w.taskId} not found`
      );
    }
    const manual = taskManuals.find((m) => m.typeKey === task.typeKey);
    const parts = [
      "# 外包工作守則\n\n(worker_context.md seed——依目前資料即時重組的預覽。)",
      `# 你的身分\n\n- 代號：${w.codename}\n- 模型：${w.model}\n- 投入度（effort）：${w.effort}`,
      `# 你的任務（就這一張）\n\n- 編號：${task.taskNo}\n- 標題：${task.title}` +
        (task.description.trim() ? `\n\n## 任務描述\n\n${task.description.trim()}` : ""),
    ];
    if (manual) {
      parts.push(
        `# 任務手冊：${manual.displayName || manual.typeKey}` +
          (manual.purpose.trim() ? `\n\n## 這是什麼任務（Q1）\n\n${manual.purpose.trim()}` : "")
      );
    } else {
      parts.push("# 任務手冊\n\n（該類型的手冊目前不存在——照任務描述與 owner 的指示規劃。）");
    }
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

  async listTaskManuals(): Promise<TaskManualView[]> {
    return structuredClone(taskManuals);
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
      throw new ApiError(
        "http 400 for POST /api/task-manuals",
        400,
        "bad_request",
        "display_name must not be blank"
      );
    }
    const manual: TaskManualView = {
      typeKey: `tm-${Array.from({ length: 12 }, () =>
        "0123456789abcdef".charAt(Math.floor(Math.random() * 16))
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
    patch: TaskManualPatch
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
      (t) => t.typeKey === typeKey && !TERMINAL_TASK_STATUSES.has(t.status)
    );
    if (open) {
      throw new ApiError(
        `http 409 for DELETE /api/task-manuals/${typeKey}`,
        409,
        "conflict",
        `task type '${typeKey}' still has open tasks`
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
      throw new ApiError(
        `http 404 for GET /api/docs/${slug}`,
        404,
        "not_found",
        `doc '${slug}' not found`
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
    return wireMembers
      .filter((m) => m.kind === "warden" && m.roster_status !== "removed")
      .map(
        (m): WireMachine => ({
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
        })
      )
      // The server-self row is ALWAYS first (stable sort keeps the rest in order).
      .sort((a, b) => Number(b.is_self) - Number(a.is_self))
      .map(toMachine);
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
        (x) => x.machine === (w?.desired_machine_id ?? id)
      );
      if (m) m.display_name = patch.displayName;
    }
  },

  async onboardMachine(
    displayName: string,
    opts?: OnboardOptions
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
    const ttlDays = opts?.ttlDays ?? 30;
    const expiresIn = ttlDays * 86400;
    // The boot command embeds a short-lived single-use claim code, never the
    // token (mirrors the real POST /api/machines onboard shape).
    const claimCode = `mock-claim-code-${Math.random().toString(36).slice(2, 14)}`;
    const bootCommand = `curl -fsSL 'https://officraft.local/install.sh?code=${claimCode}' | bash`;

    wireMembers.push({
      id: machineId,
      avatar_index: 0,
      member_no: `MB-WDN${String(wireMembers.length).padStart(3, "0")}`,
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
      roster_status: "active",
      owner_id: MOCK_OWNER_ID,
      unread_count: 0,
      schema_version: 2,
    });

    const wire: WireOnboardResult = {
      member_id: machineId,
      machine_id: machineId,
      token,
      expires_in: expiresIn,
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
        (m) => m.machine !== machineId
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
        (m) => m.machine !== host
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

  async getAuthStatus(): Promise<boolean> {
    return mockPasswordSet;
  },

  async setPassword(password: string, claimToken: string): Promise<void> {
    // Same check order as the server: already set → 409; claim → 401; then
    // the length rule.
    if (mockPasswordSet) {
      throw new ApiError(
        "http 409 for POST /api/auth/set-password",
        409,
        "conflict",
        "a password is already set"
      );
    }
    if (claimToken !== MOCK_CLAIM_TOKEN) {
      throw new ApiError(
        "http 401 for POST /api/auth/set-password",
        401,
        "unauthorized",
        "invalid claim token"
      );
    }
    if (password.length < 8) {
      throw new ApiError(
        "http 422 for POST /api/auth/set-password",
        422,
        "validation_error",
        "password must be at least 8 characters"
      );
    }
    mockPasswordSet = true;
    mockPassword = password;
  },

  async changePassword(
    currentPassword: string,
    newPassword: string
  ): Promise<void> {
    if (newPassword.length < 8) {
      throw new ApiError(
        "http 422 for POST /api/auth/change-password",
        422,
        "validation_error",
        "new_password must be at least 8 characters"
      );
    }
    if (!mockPasswordSet || currentPassword !== mockPassword) {
      throw new ApiError(
        "http 401 for POST /api/auth/change-password",
        401,
        "unauthorized",
        "invalid password"
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
      throw new ApiError(
        "http 422 for POST /api/theme/fetch",
        422,
        "validation_error",
        "url must be an absolute http:// or https:// link"
      );
    }
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new ApiError(
        "http 422 for POST /api/theme/fetch",
        422,
        "validation_error",
        "url must be an absolute http:// or https:// link"
      );
    }
    return JSON.stringify(
      {
        id: "custom-linked",
        name: "連結匯入的主題",
        colors: { "--color-bg": "#101018", "--color-accent": "#785af0" },
      },
      null,
      2
    );
  },

  async getServerSettings(): Promise<ServerSettingsView> {
    return toServerSettings(structuredClone(mockServerSettings));
  },

  async patchServerSettings(
    patch: ServerSettingsPatch
  ): Promise<ServerSettingsView> {
    // Validate BOTH fields before writing anything (server parity).
    if (patch.tokenTtl !== undefined && !TOKEN_TTL_CHOICES.has(patch.tokenTtl)) {
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "token_ttl must be one of 43200, 86400, 604800, 2592000 seconds"
      );
    }
    if (
      patch.handoverPct !== undefined &&
      (patch.handoverPct < 40 || patch.handoverPct > 90)
    ) {
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "handover_pct must be between 40 and 90"
      );
    }
    if (patch.codexCompactionThreshold !== undefined && (patch.codexCompactionThreshold < 1 || patch.codexCompactionThreshold > 10)) {
      throw new ApiError("http 422 for PATCH /api/settings", 422, "validation_error", "codex_compaction_threshold must be between 1 and 10");
    }
    if (patch.monitoringRefreshSeconds !== undefined && (patch.monitoringRefreshSeconds < 1 || patch.monitoringRefreshSeconds > 60)) {
      throw new ApiError("http 422 for PATCH /api/settings", 422, "validation_error", "monitoring_refresh_seconds must be between 1 and 60");
    }
    if (
      patch.outsourceMaxParallel !== undefined &&
      (patch.outsourceMaxParallel < -1 || patch.outsourceMaxParallel > 20)
    ) {
      // Server parity: -1 = 無限 (unlimited), 0 = paused, 20 = ceiling.
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "outsource_max_parallel must be between -1 and 20 (-1 = unlimited)"
      );
    }
    // Server parity (T-3aeb / T-ae38): each floor IS that segment's shipped
    // default, so a document cap can only ever be raised. Duty's floor is its
    // OWN default, not the other three's — sharing one number here would make
    // the owner's Duty default unreachable through this surface. The numbers
    // are read from DOC_CAP_CHARS_DEFAULTS, never restated.
    for (const [field, wire, min] of [
      [patch.docCapCharsDuty, "doc_cap_chars_duty", DOC_CAP_CHARS_DEFAULTS.duty],
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
        patch.docCapCharsManual,
        "doc_cap_chars_manual",
        DOC_CAP_CHARS_DEFAULTS.manual,
      ],
    ] as const) {
      if (field !== undefined && (field < min || field > 100000)) {
        throw new ApiError(
          "http 422 for PATCH /api/settings",
          422,
          "validation_error",
          `${wire} must be between ${min} and 100000 characters — the floor is the shipped default, so the document cap can only be raised, never lowered`
        );
      }
    }
    if (
      patch.orgName !== undefined &&
      [...patch.orgName.trim()].length > 80
    ) {
      // Server parity: trimmed, capped at 80 runes (T-d693).
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "org_name must be at most 80 characters"
      );
    }
    if (
      patch.ownerName !== undefined &&
      [...patch.ownerName.trim()].length > 80
    ) {
      // Server parity: trimmed, capped at 80 runes (T-0b41).
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "owner_name must be at most 80 characters"
      );
    }
    // custom_themes (T-16a1 P2): validated in full before anything is written —
    // shape + theme.css token whitelist + concrete-colour grammar (the SAME
    // themeBundle validator the server mirrors). A bad bundle 422s, nothing set.
    if (patch.customThemes !== undefined) {
      const err = validateThemeBundles(patch.customThemes);
      if (err) {
        throw new ApiError(
          "http 422 for PATCH /api/settings",
          422,
          "validation_error",
          err
        );
      }
    }
    // display_theme is validated against the POST-patch custom set: "" | a
    // built-in | an existing custom id (server parity).
    const effectiveThemes = patch.customThemes ?? mockServerSettings.custom_themes;
    const effectiveThemeIds = new Set(effectiveThemes.map((t) => t.id));
    if (
      patch.displayTheme !== undefined &&
      !isValidDisplayTheme(patch.displayTheme.trim(), effectiveThemeIds)
    ) {
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        'display_theme must be "", office, or an existing custom theme id'
      );
    }
    if (
      patch.displayLanguage !== undefined &&
      patch.displayLanguage.trim() !== "" &&
      !["zh", "en"].includes(patch.displayLanguage.trim())
    ) {
      // Server parity: enum-checked, "" clears (T-0b41-p2).
      throw new ApiError(
        "http 422 for PATCH /api/settings",
        422,
        "validation_error",
        "display_language must be one of zh, en"
      );
    }
    if (patch.tokenTtl !== undefined) {
      mockServerSettings.token_ttl = patch.tokenTtl;
    }
    if (patch.handoverPct !== undefined) {
      mockServerSettings.handover_pct = patch.handoverPct;
    }
    if (patch.codexCompactionThreshold !== undefined) {
      mockServerSettings.codex_compaction_threshold = patch.codexCompactionThreshold;
    }
    if (patch.monitoringRefreshSeconds !== undefined) {
      mockServerSettings.monitoring_refresh_seconds = patch.monitoringRefreshSeconds;
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
    if (patch.docCapCharsManual !== undefined) {
      mockServerSettings.doc_cap_chars_manual = patch.docCapCharsManual;
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
      if (email && (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) || /\.(local|localhost|internal|test|invalid|example)$/i.test(email))) {
        throw new ApiError("http 422 for PATCH /api/settings", 422, "validation_error", "push_contact_email must be a public email address");
      }
      mockServerSettings.push_contact_email = email;
    }
    // custom_themes + display_theme are coupled (server parity): write the set,
    // then resolve the theme against the post-patch set — an explicit theme
    // wins; otherwise a now-dangling active custom theme is reset to "".
    if (patch.customThemes !== undefined) {
      mockServerSettings.custom_themes = structuredClone(patch.customThemes);
    }
    if (patch.displayTheme !== undefined) {
      mockServerSettings.display_theme = patch.displayTheme.trim();
    } else {
      const ids = new Set(mockServerSettings.custom_themes.map((t) => t.id));
      if (!isValidDisplayTheme(mockServerSettings.display_theme, ids)) {
        mockServerSettings.display_theme = "";
      }
    }
    if (patch.displayLanguage !== undefined) {
      mockServerSettings.display_language = patch.displayLanguage.trim();
    }
    // display_wide (T-756f) is a plain bool — nothing to validate, and an
    // omitted field never changes it (PATCH semantics, server parity).
    if (patch.displayWide !== undefined) {
      mockServerSettings.display_wide = patch.displayWide;
    }
    return toServerSettings(structuredClone(mockServerSettings));
  },

  async getPushPublicKey(): Promise<string> {
    // A valid-size P-256 public-key-shaped value keeps mock-mode consumers
    // deterministic; real browser subscriptions always use the server key.
    return "B" + "A".repeat(86);
  },

  async savePushSubscription(_subscription: PushSubscriptionInput): Promise<void> {
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
    throw new ApiError(
      "http 409 for POST /api/update/upgrade",
      409,
      "conflict",
      "no newer release is known — the running build is the latest published on GitHub"
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

  async listRoles(): Promise<RoleDefView[]> {
    // Seeds first (stable), then the owner-created custom roles — mirrors
    // handle_list_roles.
    return [
      ...MOCK_WIRE_ROLES_SEED.map((seed) => toRoleDef(foldRole(seed.key))),
      ...[...customRoles.keys()].map((key) => toRoleDef(foldRole(key))),
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
      throw new ApiError(
        `http 404 for POST /api/roles/${key}/reset`,
        404,
        "not_found",
        `role '${key}' not found`
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
      throw new ApiError(
        "http 422 for POST /api/roles",
        422,
        "validation_error",
        "role requires a name"
      );
    }
    const memberName =
      (input.memberName ?? "").trim() || pickMockMemberName();
    const effort = input.effort ?? "medium";
    if (!["low", "medium", "high"].includes(effort)) {
      // Byte-for-byte the server's message (ocserverd/api_roles.go:128-129):
      // the offending value rides along in `; got '<value>'`.
      throw new ApiError(
        "http 422 for POST /api/roles",
        422,
        "validation_error",
        `effort must be one of [high low medium]; got '${effort}'`
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
      avatar_index: mockAvatarIndex("member"),
      // Server derives member_no from the member id (ocserverd/api_helpers.go:198
      // → domain.go MemberNo): a SHA-256 of the id projected to MB-XXX###. NOT a
      // constant — deriving here keeps mock parity so two createRole calls mint
      // distinct member_no values, exactly as the server does.
      member_no: await deriveMemberNo(memberId),
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
      throw new ApiError(
        `http 403 for DELETE /api/roles/${key}`,
        403,
        "forbidden",
        `role '${key}' is a built-in seed role and cannot be deleted`
      );
    }
    if (!customRoles.has(key)) {
      throw new ApiError(
        `http 404 for DELETE /api/roles/${key}`,
        404,
        "not_found",
        `role '${key}' not found`
      );
    }
    const members = wireMembers.filter((m) => m.role_key === key);
    if (members.some((m) => m.presence !== "offline")) {
      throw new ApiError(
        `http 409 for DELETE /api/roles/${key}`,
        409,
        "conflict",
        `role '${key}' has online member(s) — stop them before deleting`
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
    // Honest preview mirroring the backend assemble_boot_context 3-block order
    // (global-context-3block-restructure step1):
    //   1. 系統互動 — the read-only file seed, FIRST;
    //   2. `# Role:` — the folded role persona;
    //   3. `# Lessons (role / task_type)` — the folded per-role lessons;
    //   4. 使用者自訂 — the owner's ADDITIVE block, SKIPPED entirely when empty;
    //   5. 啟動程序 — the read-only file seed, LAST (recency-authoritative tail).
    // NO token (a UI preview mints none).
    const taskType = "general"; // mirrors the server seed lessons task_type
    const roleDef = foldRole(role); // throws for an unknown role (≈ server 404)
    const lessons =
      lessonsOverlays.get(lessonsKey(role, taskType))?.text ?? SEED_LESSONS_MD;
    const userText = foldGlobalContext().text;
    const parts = [
      SEED_SYSTEM_INTERACTION_MD.trim(),
      `# Role: ${roleDef.name || roleDef.key}\n\n${roleDef.definition_md.trim()}`,
      `# Lessons (${role} / ${taskType})\n\n${lessons.trim()}`,
    ];
    if (userText.trim()) {
      parts.push(`# 使用者自訂（Owner Additions）\n\n${userText.trim()}`);
    }
    parts.push(SEED_BOOT_SEQUENCE_MD.trim());
    const wire: WireBootstrap = {
      role,
      name: roleDef.name,
      task_type: taskType,
      context: parts.join("\n\n") + "\n",
      token: null,
    };
    return toBootstrap(wire);
  },

  async getLessons(roleKey: string, taskType: string): Promise<LessonsView> {
    // The folded PER-ROLE lessons doc for `role_key` + `task_type`. When an
    // overlay was saved (is_default=false) the folded read is that edit; otherwise
    // it IS the REAL seed (dal/seeds/lessons.md via SEED_LESSONS_MD) →
    // is_default=true. Per-role-learnings step1: the seed is shared until a role
    // diverges (each role_key gets its own overlay slot).
    const overlay = lessonsOverlays.get(lessonsKey(roleKey, taskType));
    const wire: WireLessons = overlay ?? {
      ...docSizeFields(SEED_LESSONS_MD, "learning"),
      role_key: roleKey,
      task_type: taskType,
      text: SEED_LESSONS_MD,
      owner_id: MOCK_OWNER_ID,
      schema_version: 2,
      is_default: true,
    };
    return toLessons(wire);
  },

  async saveLessons(
    roleKey: string,
    taskType: string,
    text: string
  ): Promise<LessonsView> {
    // Whole-doc replace → store the per-role overlay; the folded read is now
    // owner-edited for THIS role_key only (a sibling role's doc is untouched).
    recordDocumentHistory("lessons", lessonsKey(roleKey, taskType));
    const wire: WireLessons = {
      ...docSizeFields(text, "learning"),
      role_key: roleKey,
      task_type: taskType,
      text,
      owner_id: MOCK_OWNER_ID,
      schema_version: 2,
      is_default: false,
    };
    lessonsOverlays.set(lessonsKey(roleKey, taskType), wire);
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
      throw new ApiError(
        `http 404 for POST /api/insight/${roleKey}/reset`,
        404,
        "not_found",
        `role '${roleKey}' has no factory insight to reset to`
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
    key: string
  ): Promise<DocumentHistoryView[]> {
    refuseRetiredDocumentKind(kind, `GET /api/document-history/${kind}/${key}`);
    // Newest first, at most DOCUMENT_HISTORY_CAP — the retention the server
    // applies, so an offline cockpit sees the same bounded list.
    const kept = documentHistories.get(historySlot(kind, key)) ?? [];
    return kept.map((h) => toDocumentHistory(structuredClone(h)));
  },

  async getDocumentSeed(
    kind: DocumentKind,
    key: string
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
            : null;
    if (content === null) {
      throw new ApiError(`http 404 for ${route}`, 404, "not_found", `document '${kind}/${key}' has no shipped default to compare against`);
    }
    const wire: WireDocumentSeed = { kind, key, content };
    return toDocumentSeed(structuredClone(wire));
  },

  async restoreDocumentHistory(
    kind: DocumentKind,
    key: string,
    id: number
  ): Promise<DocumentHistoryView> {
    refuseRetiredDocumentKind(
      kind,
      `POST /api/document-history/${kind}/${key}/${id}/restore`
    );
    const slot = historySlot(kind, key);
    const found = (documentHistories.get(slot) ?? []).find((h) => h.id === id);
    if (!found) {
      throw new ApiError(
        `http 404 for POST /api/document-history/${kind}/${key}/${id}/restore`,
        404,
        "not_found",
        "document history version not found"
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
};

// Reset hook for tests / hot-reload determinism (not used by the UI).
export function __resetMock(): void {
  wireMembers = structuredClone(MOCK_WIRE_MEMBERS);
  wireMonitoring = structuredClone(MOCK_WIRE_MONITORING);
  mockBinStatus.clear();
  mockBinStatus.set("warden-mbp5", "stale");
  globalContextOverlay = null;
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
  outsourceWorkers = [];
  taskManuals = [];
  mockPasswordSet = true;
  mockPassword = "mock-password";
  mockServerSettings = { ...DEFAULT_MOCK_SETTINGS };
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
// unread/read seam (unreadCountOf ↔ listChat auto-mark) against real log entries.
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
export function __injectMockTask(task: TaskView): void {
  tasks.push(task);
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
  over: Partial<WireMember> & Pick<WireMember, "id" | "kind">
): void {
  wireMembers.push({
    ...structuredClone(MOCK_WIRE_MEMBERS[1]),
    member_no: `MB-TEST-${wireMembers.length}`,
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
