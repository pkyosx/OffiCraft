// api/mappers.ts — pure wire→view-model mapping (no I/O, no side effects).
//
// This is the ONE place the snake_case wire shape becomes the camelCase view
// model the components consume. Keeping it pure + centralised means the real
// backend only has to return the frozen wire shape — the UI never changes.

import type { ThemeBundle } from "../lib/themeBundle";
import type { components } from "./generated/schema";
import { DOC_CAP_CHARS_DEFAULTS } from "./docCap";
import { CHAT_BUDGET_CHARS_DEFAULT } from "./chatBudget";
import { BACKUP_RETAIN_DEFAULT } from "./backupRetain";
import type {
  LoreEntrySummaryView,
  LoreSearchView,
  LoreRevisionRowView,
  LoreEntryDetailView,
  LoreEventView,
  LoreRevisionView,
  LorePendingEntityView,
  LoreEntityGovernanceView,
  Member,
  MemberStatus,
  MemberLifecycle,
  RoleKey,
  Effort,
  MonSessionView,
  MonMachineView,
  MonAccountView,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  BackupHealthView,
  BackupHealthStatus,
  BackupHealthCode,
  GlobalContextView,
  BootDocView,
  DocumentHistoryEntryView,
  DocumentHistoryView,
  DocumentRevisionView,
  DocumentSeedView,
  DocumentKind,
  RoleSummaryView,
  RoleDefView,
  BootstrapView,
  LessonsView,
  InsightView,
  OnboardResultView,
  DeleteResultView,
  UninstallResultView,
  TeardownHereResultView,
  BootstrapResultView,
  MachineView,
  BinStatus,
  ClaudeCredSource,
  WardenShape,
  CutoverEffect,
} from "../types";
import type {
  WireMember,
  WireChatMessage,
  WireMachine,
  WireMonSession,
  WireLoreSearchResult,
  WireLoreSearchHit,
  WireLoreEntryDetail,
  WireLoreEvent,
  WireLoreRevisionRow,
  WireLoreRevision,
  WireLorePendingEntity,
  WireLoreEntityGovernance,
  WireMonMachine,
  WireMonAccount,
  WireMonitoring,
  WireVersion,
  WireReleaseCheck,
  WireBackupHealth,
  WireGlobalContext,
  WireBootDoc,
  WireDocumentHistory,
  WireDocumentHistoryVersion,
  WireDocumentHistoryRestore,
  WireDocumentSeed,
  WireRoleDef,
  WireRoleDefListItem,
  WireBootstrap,
  WireLessons,
  WireInsight,
  WireOnboardResult,
  WireDeleteResult,
  WireUninstallResult,
  WireTeardownHereResult,
  WireBootstrapResult,
  WireChatRead,
  WireChatGalleryEntry,
  WireReplyCard,
  WireReplyCardOption,
  WireWebhookEndpoint,
  WireWebhookRequestLog,
  WireScheduledMessage,
  WireServerSettings,
  WireTask,
  WireTaskListItem,
  WireTaskDepRef,
  WireTaskStep,
  WireTaskArtifact,
  WireOutsourceWorker,
  WireTaskManual,
  WireTaskManualListItem,
  WireTaskManualUpdate,
  WireTaskReassign,
  WireResumeOverview,
  WireResumeTask,
  WireResumeAnsweredCardStep,
  WireResumeSummary,
  WireResumeRosterMember,
  WireResumeMachines,
  WireChatInlineReplyCard,
} from "./wire";
import type {
  ChatMessage,
  ChatReadReceipt,
  GalleryAttachment,
  ReplyCard,
  ReplyCardOption,
  ServerSettingsView,
  OnboardingReportView,
  TaskView,
  TaskDepRefView,
  TaskStepView,
  TaskArtifactView,
  OutsourceWorkerView,
  TaskTypeView,
  TaskManualSummaryView,
  TaskManualView,
  TaskManualPatch,
  TaskReassignInput,
  ManualAssigneeView,
  WebhookEndpoint,
  WebhookRequestLog,
  ScheduledMessage,
  ResumeOverviewView,
  ResumeAnsweredCardStepView,
  ResumeTaskView,
  MemberResumeSummaryView,
  ResumeRosterMemberView,
  ResumeMachinesView,
  ChatInlineReplyCardView,
  ThemeListItem,
  ThemeWriteReceipt,
  ThemeDeleteResult,
} from "./adapter";

/** The five real presence words, as a runtime set — the type union's twin. */
const PRESENCE_STATES: readonly MemberLifecycle[] = [
  "offline",
  "waking",
  "online",
  "stopping",
  "stopped",
];

/**
 * Narrow the wire's bare `presence` STRING to the five-state union (T-59d6).
 *
 * The wire types presence as `string` (frozen spec), so this seam is the ONE
 * place an unrecognised word can be caught — and it must be caught, because
 * every presence surface downstream is exhaustive over the union: an
 * out-of-union word falls off `presenceVisual`'s no-default switch and paints
 * `lifecycle-dot--undefined` — a dot with no colour and no accessible name on a
 * `role="img"` element. `undefined` here means "no presence to project"; each
 * caller resolves it to its own honest floor (`offline` for members, whose
 * `lifecycle` is non-optional; left `undefined` for workers, where absence is
 * itself meaningful — released / never dispatched).
 *
 * NOT worker-specific despite its first caller: members and outsource workers
 * speak the SAME presence vocabulary (A案 P6), so they share this one narrower.
 */
export function toPresence(
  raw: string | undefined,
): MemberLifecycle | undefined {
  return raw !== undefined && PRESENCE_STATES.includes(raw as MemberLifecycle)
    ? (raw as MemberLifecycle)
    : undefined;
}

/**
 * Map one wire member → the view-model `Member`. Every field's source is noted.
 */
export function toMember(w: WireMember): Member {
  // Narrowed ONCE per member (T-59d6): both `status` and `lifecycle` below are
  // projections of the same presence word, so they must agree about what
  // counts as a recognised state. An unknown word → `undefined` → the honest
  // `offline` floor: every downstream comparison (`=== "online"` composer
  // lock, `=== "waking"` wake-in-flight) already read false on an unknown
  // word, so behaviour is unchanged — what changes is that the dot now renders
  // a real offline dot WITH its accessible name instead of a nameless,
  // colourless one.
  const presence = toPresence(w.presence) ?? "offline";
  return {
    id: w.id, // wire id (attribution key)
    avatarUrl: w.avatar_url ?? "",
    name: w.name, // direct
    // role_key is the wire role; view model narrows to the RoleKey union. Fall
    // back to "assistant" (the only M1 role) when the wire leaves it blank.
    role: (w.role_key || "assistant") as RoleKey,
    // The role's display TITLE resolved server-side (seed title, or the custom
    // role's own name). UI shows the i18n label for known seed keys, else this.
    roleName: w.role_name,
    // presence is the DERIVED tri-state and maps 1:1 onto the view status.
    // We read presence (NOT online) so waking is honestly surfaced. `status`
    // stays the frozen tri-state contract: the two shutdown states collapse to
    // the nearest tri-state tint (stopping→online, stopped→offline) so the
    // legacy presence dot never renders an out-of-union value; the
    // full five-state lifecycle rides on `lifecycle` below.
    status:
      presence === "stopping"
        ? "online"
        : presence === "stopped"
          ? "offline"
          : presence,
    // lifecycle carries the REAL five-state presence verbatim (backend guarantees
    // one of offline/waking/online/stopping/stopped). Honest passthrough — never
    // a fabricated value.
    lifecycle: presence,
    runtime: (w.runtime || "claude") as "claude" | "codex",
    actualModel: w.actual_model || "",
    // The reported twins of `runtime` / `effort` (T-7f28). "" = nothing has
    // ever reported one — honest-unknown, NEVER floored to the configured
    // value beside it: a substitute here is exactly what made a launch change
    // that had not taken effect look like one that had.
    actualRuntime: (w.actual_runtime || "") as "claude" | "codex" | "",
    actualEffort: w.actual_effort || "",
    model: w.model, // direct
    effort: (w.effort || "medium") as Effort, // direct (narrowed to union)
    kind: w.kind, // "assistant" | "warden" | … — office roster keeps assistants only
    desiredMachineId: w.desired_machine_id, // direct passthrough — warden↔machine resolution for teardown
    // The owner's lifecycle intent — a warden carrying "uninstall" drives the
    // machines panel's "uninstalling…" transitional state. Direct passthrough.
    desiredState: w.desired_state,

    // The OBSERVED machine — where this member is ACTUALLY running (wire `machine`,
    // resolved server-side via observed_host: SSE claim → telemetry → desired_state).
    // Distinct from `desiredMachineId` (the DESIRED binding). Honest-empty "" → null
    // so the panel renders "—", never a fabricated position.
    machine: w.machine || null,
    // The rest of the runtime telemetry has no source on the member wire (it comes
    // from the monitoring surface, not wired in M1) → null → the panel shows "—".
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,

    // tmux session name: mirror the real backend rule — reconcile.py names every
    // member-spawned session `member-<id>` (id lowercased); attach uses the
    // `officraft` socket (spawn.py DEFAULT_SOCKET). NOT the old raw-id fixture.
    tmuxSession: `member-${w.id.toLowerCase()}`,
    // The member wire carries no lessons (those come from the lessons doc, not
    // wired in M1). The initial boot prompt is NOT baked into the member view —
    // it is fetched on demand from /api/bootstrap (see api.getBootstrap).

    // refocus_since > 0 → epoch of the last refocus intent (surfaced in the
    // detail panel); 0 → null (never refocused) so the panel shows no fabricated
    // "last refocus" time. Same honest ">0 else null" rule as last_alive.
    refocusSince: w.refocus_since > 0 ? w.refocus_since : null,
    // Which operation opened that window, and the epoch it is collected by at
    // the latest ("" / null when none is in flight) — so the panel can say
    // "winding down so your change can take effect" instead of "last refocus",
    // which reads as history (T-7f28).
    refocusOp: w.refocus_op || "",
    refocusDeadline: w.refocus_deadline > 0 ? w.refocus_deadline : null,
    // The DURABLE last-observed machine. `machine` above goes blank the moment
    // the member stops running; this one survives, so a pending relocation is
    // still legible while it is offline.
    actualMachine: w.actual_machine || "",

    // fleet remote-ops stage 1: the last warden-op receipt (snake→camel passthrough).
    // last_op_at > 0 → real epoch (shown as the op time); 0 → null (no op yet) so the
    // "最近操作" block hides, never a fabricated time. last_op_ok keeps its three
    // states (null = none, true = ok, false = failed) verbatim from the wire.
    lastOp: w.last_op,
    // last_op_ok is nullable on the wire; a defaulted-away field arrives as
    // `undefined` — coalesce to null (the "no op yet" state), never fabricate.
    lastOpOk: w.last_op_ok ?? null,
    lastOpLog: w.last_op_log,
    // Structured failure cause ("<code>: <detail>" from the warden receipt,
    // server-folded onto last_op_reason). Older records never carried one —
    // a defaulted-away field coalesces to "" and the panel shows status-only.
    lastOpReason: w.last_op_reason ?? "",
    lastOpAt: w.last_op_at > 0 ? w.last_op_at : null,

    // M2-1 roster unread badge: the server-computed unread COUNT (the pure
    // inverse of the chat_read watermark, for the caller). Honest passthrough —
    // a defaulted-away wire field reads as 0, never a fabricated count.
    unreadCount: w.unread_count ?? 0,
  };
}

/** Map one wire chat message → the view-model `ChatMessage`. */
export function toChatMessage(w: WireChatMessage): ChatMessage {
  // The reply-card link rides the open `meta` object (`meta.reply_card_id`,
  // stamped server-side when an agent opens a card). Meta is untyped on the
  // wire — surface the id only when it really is a non-empty string, else the
  // honest null (a plain message).
  const rawCardId = w.meta?.["reply_card_id"];
  return {
    id: w.id,
    from: w.from,
    to: w.to,
    body: w.body,
    ts: w.ts,
    replyCardId:
      typeof rawCardId === "string" && rawCardId !== "" ? rawCardId : null,
    // Read-time join the server computes (`reply_card_status`): "waiting" |
    // "answered" for a card-bearing message, "" otherwise. Surface the closed
    // set only, else the honest null (no card / unknown) — the inline card then
    // just fetches, as it did before this field existed.
    replyCardStatus:
      w.reply_card_status === "waiting" ||
      w.reply_card_status === "answered" ||
      w.reply_card_status === "expired"
        ? w.reply_card_status
        : null,
    // Generic attachments (0..N; image is the special case). Honest
    // passthrough of the server list — an absent/defaulted wire field reads as
    // an empty list, never fabricated entries.
    attachments: (w.attachments ?? []).map((a) => ({
      id: a.id,
      url: a.url,
      filename: a.filename ?? "",
      mime: a.mime ?? "",
      isImage: a.is_image ?? false,
    })),
    // Display names beside the ids, and the SERVER-rendered timestamp beside
    // the epoch one. All honest passthrough: an absent/defaulted wire field
    // reads as "" (or 0), NEVER back-filled from the id — a name the server
    // could not resolve is a name nobody has, and printing the address in its
    // place would make an unresolved sender indistinguishable from one whose
    // display name happens to equal its id.
    fromName: w.from_name ?? "",
    toName: w.to_name ?? "",
    tsDisplay: w.ts_display ?? "",
    bodyOmittedChars: w.body_omitted_chars ?? 0,
    // The reply card folded onto this message (wake snapshot only). Absent on
    // every other chat read, so the honest null — the message carries no card.
    card: w.card ? toChatInlineReplyCard(w.card) : null,
    // The quoted message's id (`reply_to`). Honest passthrough: the server
    // sends "" for a message that replies to nothing, which reads as null here.
    replyTo: w.reply_to ? w.reply_to : null,
    // WHAT was replied to (`reply_to_chat`), rebuilt server-side on every read.
    // The key is OMITTED by the server both when this message is not a reply and
    // when its original no longer exists — one null covers both, and the UI
    // tells them apart by looking at `replyTo`, which never disappears.
    //
    // The strings are taken as they arrive. `content` in particular is NOT
    // re-shortened here: its length is defined on the server and nowhere else,
    // so a second trim in the browser would be a second rule to keep in step.
    //
    // `to` rides across for the same reason `from` does: the quote line draws
    // 「寄件者 → 收件者」, and the recipient it draws is the QUOTED message's,
    // not this thread's peer — a quote may come from another conversation.
    replyToChat: w.reply_to_chat
      ? {
          id: w.reply_to_chat.id,
          from: w.reply_to_chat.from,
          fromName: w.reply_to_chat.from_name ?? "",
          to: w.reply_to_chat.to,
          toName: w.reply_to_chat.to_name ?? "",
          content: w.reply_to_chat.content ?? "",
        }
      : null,
  };
}

/** Map one wire in-place reply card (`ChatMessageDTO.card`) → the view model.
 * Pure passthrough of the DECISION fields; `answer_option_idxs` keeps its
 * three-way meaning (a list = those options were circled, null = free text only
 * or not answered yet), so an absent list is NOT coerced to an empty one — the
 * card face draws 「你選的」 off membership in this list, and an empty list and
 * a null one must not both mean "answered with nothing". */
export function toChatInlineReplyCard(
  w: WireChatInlineReplyCard,
): ChatInlineReplyCardView {
  return {
    options: (w.options ?? []).map(toReplyCardOption),
    answerOptionIdxs: Array.isArray(w.answer_option_idxs)
      ? w.answer_option_idxs
      : null,
    answerText: w.answer_text ?? "",
    answeredTs: w.answered_ts ?? 0,
    answeredAtDisplay: w.answered_at_display ?? "",
  };
}

/** Map one wire gallery row → the view-model `GalleryAttachment`. Honest
 * passthrough — `fromName` stays "" when the server could not resolve a display
 * name (the component decides the fallback label), never fabricated. */
export function toGalleryAttachment(
  w: WireChatGalleryEntry,
): GalleryAttachment {
  return {
    id: w.id,
    url: w.url,
    filename: w.filename ?? "",
    mime: w.mime ?? "",
    isImage: w.is_image ?? false,
    messageId: w.message_id,
    from: w.from,
    fromName: w.from_name ?? "",
    to: w.to,
    ts: w.ts ?? 0,
  };
}

/** Map one wire reply card → the view-model `ReplyCard`. Honest passthrough —
 * `answer` stays null unless answered (never fabricated); the wire guarantees
 * `status` ∈ {waiting, answered, expired} (the narrowing cast mirrors
 * `toMember`'s). */
/** Map one wire quick-reply option → the view model. `ai_pick` defaults FALSE
 * when the wire omits it: an unmarked option is not the AI's recommendation,
 * and there is no positional fallback to guess with. */
export function toReplyCardOption(w: WireReplyCardOption): ReplyCardOption {
  return { text: w.text, aiPick: w.ai_pick ?? false };
}

export function toReplyCard(w: WireReplyCard): ReplyCard {
  return {
    id: w.id,
    from: w.from,
    kind: w.kind,
    summary: w.summary ?? "",
    body: w.body ?? "",
    options: (w.options ?? []).map(toReplyCardOption),
    selectMode: w.select_mode,
    status: w.status as ReplyCard["status"],
    // QUESTION-side attachments (T-5e8a): honest passthrough of the served
    // refs — an absent/defaulted wire field reads as an empty list.
    attachments: (w.attachments ?? []).map((a) => ({
      id: a.id,
      url: a.url,
      filename: a.filename ?? "",
      mime: a.mime ?? "",
      isImage: a.is_image ?? false,
    })),
    createdTs: w.created_ts ?? 0,
    answeredTs: w.answered_ts,
    expiredTs: w.expired_ts,
    chatMessageId: w.chat_message_id,
    // The task the ask was armed from (SPEC §3.6 請示 → 任務): the honest null
    // for a pure chat ask — the UI then shows no task info and no jump.
    task: w.task
      ? {
          id: w.task.id,
          typeKey: w.task.type_key ?? "",
          title: w.task.title ?? "",
        }
      : null,
    answer: w.answer
      ? {
          optionIdxs: w.answer.option_idxs,
          text: w.answer.text ?? "",
          attachments: (w.answer.attachments ?? []).map((a) => ({
            id: a.id,
            url: a.url,
            filename: a.filename ?? "",
            mime: a.mime ?? "",
            isImage: a.is_image ?? false,
          })),
        }
      : null,
  };
}

/** Map one wire task step → `TaskStepView`. Honest passthrough — defaulted-away
 * wire fields read as their wire defaults ("" / false / 0), never fabricated;
 * the gate projection (announced vs armed) is carried verbatim by
 * `isGate` + `replyCardId` and derived only in the component. */
export function toTaskStep(w: WireTaskStep): TaskStepView {
  return {
    id: w.id,
    name: w.name ?? "",
    dod: w.dod ?? "",
    status: w.status,
    isGate: w.is_gate ?? false,
    replyCardId: w.reply_card_id ?? "",
    // One-line reason while the step sits in waiting_external; "" otherwise
    // (T-9ca5). Honest passthrough.
    waitingReason: w.waiting_reason ?? "",
    // The step's working note (T-cc3e) — bound to no status, unlike
    // waitingReason above. Honest passthrough.
    note: w.note ?? "",
    // Read-time join the server computes (`reply_card_status`): the bound card's
    // live status for the card-bearing step, "" otherwise. Closed set only, else
    // honest null.
    replyCardStatus:
      w.reply_card_status === "waiting" ||
      w.reply_card_status === "answered" ||
      w.reply_card_status === "expired"
        ? w.reply_card_status
        : null,
    parallelGroup: w.parallel_group ?? "",
    orderIdx: w.order_idx,
    startedTs: w.started_ts ?? 0,
    finishedTs: w.finished_ts ?? 0,
  };
}

/** Map one wire task artifact → `TaskArtifactView` (T-3dc5). Honest
 * passthrough — defaulted-away wire fields read as their wire defaults
 * (""/false/0). `kind` narrows to the closed set (an unknown value falls back
 * to "link" — the no-blob shape — rather than fabricating file/image). */
export function toTaskArtifact(w: WireTaskArtifact): TaskArtifactView {
  const kind =
    w.kind === "file" || w.kind === "image" || w.kind === "link"
      ? w.kind
      : "link";
  return {
    id: w.id,
    kind,
    url: w.url ?? "",
    label: w.label ?? "",
    filename: w.filename ?? "",
    mime: w.mime ?? "",
    isImage: w.is_image ?? false,
    attachmentId: w.attachment_id ?? "",
    createdTs: w.created_ts ?? 0,
    createdBy: w.created_by ?? "",
  };
}

/** Map one wire dep ref → `TaskDepRefView` (T-a3e4). Honest passthrough: an
 * entry with an empty `status` is a dep whose task is GONE, and the card says
 * 查無此任務 for it — this mapper never substitutes a plausible status. */
export function toTaskDepRef(w: WireTaskDepRef): TaskDepRefView {
  return {
    id: w.id,
    taskNo: w.task_no,
    title: w.title ?? "",
    status: w.status ?? "",
  };
}

/** Map one wire task → `TaskView`. Pure snake→camel passthrough. Honesty:
 * `progressDone`/`progressTotal` are the SERVER's leaf counts (never recomputed
 * from steps here); `closedTs` stays null while open; steps keep the server's
 * timeline order (order_idx asc — re-asserted here so the UI never depends on
 * response ordering). NO `depTasks`: the server's dep join (T-a3e4) rides the
 * LIGHT list item, which is the payload hot path — the card's dep rows
 * deliberately read the light row, not the hydrated detail, so hydrating a card
 * cannot blank them. */
export function toTask(w: WireTask): TaskView {
  return {
    id: w.id,
    taskNo: w.task_no,
    title: w.title ?? "",
    typeKey: w.type_key ?? "",
    description: w.description ?? "",
    status: w.status,
    // Orthogonal handover lock (T-9ca5): "" | "reassigning". Honest passthrough.
    lock: w.lock ?? "",
    priority: w.priority,
    executorKind: w.executor_kind,
    executorId: w.executor_id ?? "",
    creatorId: w.creator_id ?? "",
    reassignedFrom: w.reassigned_from ?? "",
    reassignedFromKind: w.reassigned_from_kind ?? "",
    dedupeKey: w.dedupe_key ?? "",
    deps: w.deps ?? [],
    waitingReason: w.waiting_reason ?? "",
    duplicateOf: w.duplicate_of ?? "",
    createdTs: w.created_ts ?? 0,
    updatedTs: w.updated_ts ?? 0,
    closedTs: w.closed_ts,
    progressDone: w.progress_done,
    progressTotal: w.progress_total,
    steps: (w.steps ?? [])
      .map(toTaskStep)
      .sort((a, b) => a.orderIdx - b.orderIdx),
    // Full task carries the resolved set; count kept == length so a hydrated
    // card keeps the same 「產物 N」 badge as its light-list frame.
    artifacts: (w.artifacts ?? []).map(toTaskArtifact),
    artifactCount: (w.artifacts ?? []).length,
  };
}

/** Map one LIGHT wire list item → `TaskView` (`GET /api/tasks` / list_tasks).
 * The light projection carries no `steps`/`description`/`inputs`, so those read
 * as their empty view defaults ([] / "") until the card is expanded and
 * hydrated from the full task (`getTask`). Every other field is the same honest
 * snake→camel passthrough as `toTask`; `progressDone`/`progressTotal` are the
 * server's counts (still present on the light item). */
export function toTaskListItem(w: WireTaskListItem): TaskView {
  return {
    id: w.id,
    taskNo: w.task_no,
    title: w.title ?? "",
    typeKey: w.type_key ?? "",
    description: "",
    status: w.status,
    // Light list carries the lock too (T-9ca5) — the collapsed card's 轉派中
    // overlay badge rides it without hydrating the full task.
    lock: w.lock ?? "",
    priority: w.priority,
    executorKind: w.executor_kind,
    executorId: w.executor_id ?? "",
    creatorId: w.creator_id ?? "",
    reassignedFrom: w.reassigned_from ?? "",
    reassignedFromKind: w.reassigned_from_kind ?? "",
    dedupeKey: w.dedupe_key ?? "",
    deps: w.deps ?? [],
    // dep_tasks (T-a3e4): the server's resolution of each dep, passed through
    // VERBATIM — absent stays absent. `?? []` would be a lie here: an empty
    // array says "every dep is unresolvable" (查無此任務), while absence says
    // "this server does not resolve deps", and the card renders those two
    // differently on purpose.
    depTasks: w.dep_tasks?.map(toTaskDepRef),
    waitingReason: w.waiting_reason ?? "",
    duplicateOf: w.duplicate_of ?? "",
    createdTs: w.created_ts ?? 0,
    updatedTs: w.updated_ts ?? 0,
    closedTs: w.closed_ts,
    progressDone: w.progress_done,
    progressTotal: w.progress_total,
    steps: [],
    // Light list: no artifact rows (get_task hydrates them for the popover);
    // only the server count for the collapsed card's 「產物 N」 badge.
    artifacts: [],
    artifactCount: w.artifact_count ?? 0,
  };
}

/** Map one wire outsource worker → `OutsourceWorkerView`. Identity + binding
 * for the tasks page, PLUS the wire's task echo (title/status) + mint stamp
 * for the office 外包 panel (SPEC §4.1: 代號 · 任務狀態 + 任務標題 without a
 * task-list join). Honest passthrough — defaulted-away fields read as ""/0. */
export function toOutsourceWorker(w: WireOutsourceWorker): OutsourceWorkerView {
  return {
    id: w.id,
    avatarUrl: w.avatar_url ?? "",
    codename: w.codename,
    runtime: (w.runtime || "claude") as "claude" | "codex",
    model: w.model ?? "",
    effort: w.effort ?? "",
    status: w.status ?? "",
    taskId: w.task_id,
    taskTitle: w.task_title ?? "",
    taskStatus: w.task_status ?? "",
    // The bound task's number / type / created stamp are WIRE fields since
    // T-a3e4 (they were a client-side join against the whole task list).
    // Honest "" / 0 when the server could not resolve the task — the panel
    // then prints 自由代辦 and orders by the worker's own mint stamp.
    taskNo: w.task_no ?? "",
    taskTypeKey: w.task_type_key ?? "",
    taskTypeName: w.task_type_name ?? "",
    taskCreatedTs: w.task_created_ts ?? 0,
    createdTs: w.created_ts ?? 0,
    unreadCount: w.unread_count ?? 0,

    // ── T-f190 detail-panel fold ──────────────────────────────────────────
    // presence = the REAL-liveness projection (A案 P6, member vocabulary),
    // NARROWED to the five-state union by the SAME `toPresence` the member seam
    // uses (T-59d6). Unlike a member, a worker legitimately has NO presence
    // (released / never dispatched), so `undefined` is kept rather than floored
    // to offline — `presenceVisual` still paints it as the offline dot.
    presence: toPresence(w.presence),
    // machine = the ACTUAL dispatch target (already resolved to a display name
    // server-side); "" when never dispatched → the panel shows 「尚未分配」.
    machine: w.machine ?? "",
    desiredMachineId: w.desired_machine_id ?? "",
    // See the member mapper: reported twins + durable last landing, all
    // honest-empty rather than falling back to the configured value (T-7f28).
    actualRuntime: (w.actual_runtime || "") as "claude" | "codex" | "",
    actualModel: w.actual_model ?? "",
    actualEffort: w.actual_effort ?? "",
    actualMachine: w.actual_machine ?? "",
    // Runtime facts: nullable on the wire (null = unreported). A defaulted-away
    // field arrives as undefined — coalesce to null (the honest dash), never 0.
    account: w.account ?? null,
    contextPct: w.context_pct ?? null,
    compactionCount: w.compaction_count ?? null,
    cost: w.cost ?? null,
    // banked_cost = the durable cumulative spend (member parity, T-ba6b);
    // null = nothing banked yet — the panel sums live+banked like the member
    // total, treating the absent side as no-cost-yet.
    bankedCost: w.banked_cost ?? null,
    // last_op* mirror the member fold: last_op_ok stays three-valued (null =
    // none), last_op_at > 0 → real epoch else null so the 最近操作 block hides
    // rather than showing a fabricated time.
    lastOp: w.last_op ?? "",
    lastOpOk: w.last_op_ok ?? null,
    lastOpLog: w.last_op_log ?? "",
    lastOpReason: w.last_op_reason ?? "",
    lastOpAt: w.last_op_at && w.last_op_at > 0 ? w.last_op_at : null,
    // creator_id (raw sub) + delegated_by (resolved name) replace the former
    // hardcoded "System owner"; honest "" passthrough.
    creatorId: w.creator_id ?? "",
    delegatedBy: w.delegated_by ?? "",
    // Lifecycle (T-32e1/T-f190): refocus_since 0 → null (no fabricated 換手中
    // time; member.refocus_since style); desired_state mirrors member ("" reads
    // as online — the stop/restart toggle only trips on an explicit "offline").
    refocusSince: w.refocus_since && w.refocus_since > 0 ? w.refocus_since : null,
    refocusOp: w.refocus_op ?? "",
    refocusDeadline:
      w.refocus_deadline && w.refocus_deadline > 0 ? w.refocus_deadline : null,
    desiredState: w.desired_state ?? "online",
    // Response-only, absent on every read face — passed through as-is so
    // `undefined` keeps meaning "this answer does not carry the signal"
    // (T-ed79 #5/#12). Coalescing them to false here would erase exactly the
    // distinction the three fields exist to make.
    relocationPending: w.relocation_pending ?? undefined,
    relocationDeferred: w.relocation_deferred ?? undefined,
    activationPending: w.activation_pending ?? undefined,
  };
}

/** Map one wire task manual → the LIGHT `TaskTypeView` the type filter reads.
 * DROPS fields/sop/learnings/assignee on purpose — the tasks page must not
 * grow a manual-editing surface (that is 設定 › 任務手冊's `toTaskManual`). */
export function toTaskType(
  w: WireTaskManualListItem | WireTaskManual
): TaskTypeView {
  return {
    typeKey: w.type_key,
    displayName: w.display_name ?? "",
    purpose: w.purpose ?? "",
  };
}

/** Narrow the wire's OPEN assignee object ({} = unset; {"kind":"member",…} or
 * {"kind":"outsource",…} otherwise) to the closed `ManualAssigneeView` union.
 * Honest: an unrecognised/empty shape maps to null (unset), never a
 * fabricated assignee. */
export function toManualAssignee(
  a: Record<string, unknown> | undefined,
): ManualAssigneeView {
  if (!a) return null;
  if (a["kind"] === "member" && typeof a["member_id"] === "string") {
    return { kind: "member", memberId: a["member_id"] };
  }
  if (a["kind"] === "outsource") {
    return {
      kind: "outsource",
      runtime: a["runtime"] === "codex" ? "codex" : "claude",
      model: typeof a["model"] === "string" ? a["model"] : "",
      effort: typeof a["effort"] === "string" ? a["effort"] : "",
      // 0 = 無限 (unlimited per-type copies — spec TaskManualDTO).
      copies: typeof a["copies"] === "number" ? a["copies"] : 1,
      // The machine this type's workers boot on; absent ⇒ "" (none chosen —
      // nothing is substituted, so no worker of the type starts).
      machine: typeof a["machine"] === "string" ? a["machine"] : "",
    };
  }
  return null;
}

/** Map one wire task manual → the FULL `TaskManualView` (設定 › 任務手冊).
 * Pure snake→camel passthrough; the open assignee object narrows through
 * `toManualAssignee` (unset {} → null). */
export function toTaskManualSummary(
  w: WireTaskManualListItem | WireTaskManual,
): TaskManualSummaryView {
  return {
    typeKey: w.type_key,
    displayName: w.display_name ?? "",
    purpose: w.purpose ?? "",
    fields: (w.fields ?? []).map((f) => ({
      name: f.name,
      required: f.required ?? false,
      isKey: f.is_key ?? false,
    })),
    assignee: toManualAssignee(w.assignee as Record<string, unknown>),
    updatedTs: w.updated_ts ?? 0,
    // The directory answer ALSO carries sop_md_chars / learnings_chars and
    // their caps. They are deliberately NOT mapped: no manual surface renders
    // a size budget today, and a view field with no reader is indistinguishable
    // from a live one. Map them when something draws them.
  };
}

/** Map one wire manual → the FULL view (`GET /{type_key}` and every write
 * echo). `sop_md` / `learnings` are optional on the wire and ABSENT from the
 * list answer since T-1170; `?? ""` here is the geometry of a manual that has
 * never been written, not a stand-in for a projection that dropped them — this
 * mapper is only ever handed a full-document response. */
export function toTaskManual(w: WireTaskManual): TaskManualView {
  return {
    ...toTaskManualSummary(w),
    sopMd: w.sop_md ?? "",
    learnings: w.learnings ?? "",
  };
}

/** Build the wire body of a manual edit from the view patch. On the wire a
 * NULL field means "leave unchanged" (the update DTO's default), so omitted
 * view fields become null; the assignee axis is three-valued — omitted → null
 * (unchanged), explicit `null` → `{}` (UNSET), a value → its wire object. */
export function fromTaskManualPatch(
  patch: TaskManualPatch,
): WireTaskManualUpdate {
  return {
    display_name: patch.displayName ?? null,
    purpose: patch.purpose ?? null,
    sop_md: patch.sopMd ?? null,
    learnings: patch.learnings ?? null,
    fields:
      patch.fields !== undefined
        ? patch.fields.map((f) => ({
            name: f.name,
            required: f.required,
            is_key: f.isKey,
          }))
        : null,
    assignee:
      patch.assignee === undefined
        ? null
        : patch.assignee === null
          ? {}
          : patch.assignee.kind === "member"
            ? { kind: "member", member_id: patch.assignee.memberId }
            : {
                kind: "outsource",
                runtime: patch.assignee.runtime ?? "claude",
                model: patch.assignee.model,
                effort: patch.assignee.effort,
                copies: patch.assignee.copies,
                // A blank machine is OMITTED, not sent as "": the wire requires a
                // non-blank machine id when the key is present, and "no machine
                // chosen" is a legal manual (no worker starts until one is).
                ...(patch.assignee.machine
                  ? { machine: patch.assignee.machine }
                  : {}),
              },
  };
}

/** Build the wire body of a reassign from the view input. The target's unused
 * axes go null rather than absent — the DTO defaults them and the server reads
 * only the ones its `kind` branch cares about. */
export function fromTaskReassignInput(
  input: TaskReassignInput
): WireTaskReassign {
  const target = input.target;
  return {
    note: input.note?.trim() || null,
    target:
      target.kind === "member"
        ? {
            kind: "member",
            member_id: target.memberId,
            runtime: null,
            model: null,
            effort: null,
            machine: null,
          }
        : {
            kind: "outsource",
            member_id: null,
            runtime: target.runtime ?? "claude",
            model: target.model,
            effort: target.effort,
            machine: target.machine,
          },
  };
}

/** Map one wire chat read receipt → the view-model `ChatReadReceipt`. */
export function toChatRead(w: WireChatRead): ChatReadReceipt {
  return {
    readerId: w.reader_id,
    peerId: w.peer_id,
    lastReadTs: w.last_read_ts,
  };
}

/**
 * Coerce an opaque wire value to `number | null` — never a fabricated default.
 * Used for the account usage dicts (`dict[str, Any]` on the wire): a missing or
 * non-numeric field passes through as null → the UI renders "—".
 */
function numOrNull(v: unknown): number | null {
  return typeof v === "number" ? v : null;
}

/** Map one wire mon session → `MonSessionView`. presence→status 1:1 (same as
 * `toMember`); null telemetry passes straight through (never fabricated). */
function toMonSession(w: WireMonSession): MonSessionView {
  return {
    id: w.id,
    name: w.name,
    role: (w.role || "assistant") as RoleKey,
    model: w.model,
    effort: w.effort || "", // live self-reported effort; "" passes through → "—"
    machine: w.machine,
    account: w.account,
    // The REPORTED runtime, honest-empty until something reports one — the wire
    // stopped serving the configured value here (T-7f28), so flooring it to
    // "claude" would just put the fabrication back one layer up.
    runtime: (w.runtime || "") as "claude" | "codex" | "",
    status: w.presence as MemberStatus,
    // Telemetry is null-until-reported on the wire; a defaulted-away field
    // arrives as `undefined` — coalesce to null so the UI renders "—", never a
    // fabricated number.
    contextPct: w.context_pct ?? null,
    compactionCount: w.compaction_count ?? null,
    cost: w.cost ?? null,
    bankedCost: w.banked_cost ?? null,
  };
}

/** Map one wire mon machine → `MonMachineView` (hardware fields pass through as
 * null — no source). */
function toMonMachine(w: WireMonMachine): MonMachineView {
  return {
    machine: w.machine,
    displayName: w.display_name, // BE guarantees non-empty (fallback=id) — map direct, no ||id
    agents: w.agents,
    // Defaulted-away wire fields arrive as `undefined`; coalesce to the honest
    // empty (empty account list, null hardware → UI "—"). Never fabricated.
    accounts: w.accounts ?? [],
    cpuPct: w.cpu_pct ?? null,
    ramPct: w.ram_pct ?? null,
    batteryPct: w.battery_pct ?? null,
    acPower: w.ac_power ?? null,
    binStatus: toBinStatus(w.bin_status),
    // Same narrowing as the registry row — the monitoring projection of a
    // machine must not disagree with the machine table about its own shape.
    wardenShape: toWardenShape(w.warden_shape),
    cutoverEffect: toCutoverEffect(w.cutover_effect),
    claudeVersion: w.claude_version ?? null,
    claudeCredSource: toClaudeCredSource(w.claude_cred_source),
    claudeSubReadable: w.claude_sub_readable ?? null,
    runtimeCapabilities: Object.fromEntries(
      Object.entries(w.runtime_capabilities ?? {}).map(([runtime, capability]) => [
        runtime,
        {
          installed: capability.installed ?? null,
          loggedIn: capability.logged_in ?? null,
          version: capability.version ?? null,
        },
      ])
    ),
    // Freshness rides the same mapper as the values it qualifies, so a row can
    // never arrive carrying readiness with no verdict about how old it is.
    runtimeCapabilitiesTs: w.runtime_capabilities_ts ?? null,
    runtimeCapabilitiesStale: w.runtime_capabilities_stale ?? null,
    hardwareTs: w.hardware_ts ?? null,
    hardwareStale: w.hardware_stale ?? null,
    // Honest-empty, never null: "no key is broken" is an answer every row can
    // give, so the component never has to branch on absence to render a blank.
    hardwareInvalid: w.hardware_invalid ?? [],
  };
}

/** Map one wire mon account → `MonAccountView`. Narrows the opaque usage dicts
 * to the shape the account card renders; every metric stays honest (null when
 * absent). Window dicts come from the backend `shape_window`
 * (domain/token_pacing.py) which emits `used_pct`/`elapsed_pct`/`pace` — the
 * card's usage bar reads used_pct, the pace marker reads elapsed_pct, and the
 * 7-day window is "overheated" when pace === "hot".
 *
 * `measured_at` (T-3b90) is when used_pct was last reported. Mapped straight
 * through with no freshness arithmetic here on purpose: the BE owns the one
 * staleness threshold (an unrefreshed snapshot simply arrives with pace null,
 * so `overheated` goes false by itself), and the card states the age it is
 * given rather than deciding for itself what counts as old. Two clients each
 * re-deriving "stale" from their own wall clock is how the threshold ends up
 * with two homes. */
function toMonAccount(w: WireMonAccount): MonAccountView {
  return {
    account: w.account,
    accountLabel: w.account_label ?? null,
    displayName: w.display_name, // BE guarantees non-empty (fallback=id) — map direct, no ||id
    machine: w.machine,
    cost: w.cost ?? null,
    fiveHour: w.five_hour
      ? {
          usagePct: numOrNull(w.five_hour.used_pct),
          timePct: numOrNull(w.five_hour.elapsed_pct),
          measuredAt: numOrNull(w.five_hour.measured_at),
        }
      : null,
    sevenDay: w.seven_day
      ? {
          usagePct: numOrNull(w.seven_day.used_pct),
          timePct: numOrNull(w.seven_day.elapsed_pct),
          measuredAt: numOrNull(w.seven_day.measured_at),
          overheated: w.seven_day.pace === "hot",
        }
      : null,
  };
}

/** Map the wire monitoring envelope → the view model. Pure passthrough of the
 * three sections through the per-item mappers above. */
export function toMonitoring(w: WireMonitoring): MonitoringView {
  return {
    // The three sections are defaulted-empty on the wire; a defaulted-away
    // section arrives as `undefined` — treat as the empty roster (honest-empty).
    sessions: (w.sessions ?? []).map(toMonSession),
    machines: (w.machines ?? []).map(toMonMachine),
    accounts: (w.accounts ?? []).map(toMonAccount),
  };
}

/** Map the wire version → the view model. Pure field rename; every honesty
 * invariant (version "0.0.0", update_available false, no phantom version) is
 * carried straight through — the mapper never manufactures a value. */
/** WireServerSettings → ServerSettingsView. Pure passthrough (snake→camel).
 * `outsource_max_parallel` is always sent by the server (Go wire.go emits the
 * int unconditionally); the schema marks it optional only for DTO-compat, so
 * `?? 0` can only ever fire against a pre-M3 server — where 0 honestly reads
 * as "no outsource assignment". */
export function toServerSettings(w: WireServerSettings): ServerSettingsView {
  return {
    ownerTokenTtl: w.owner_token_ttl,
    agentTokenTtl: w.agent_token_ttl,
    handoverPct: w.handover_pct,
    noticePct: w.notice_pct,
    codexNoticeRound: w.codex_notice_round,
    codexCompactionThreshold: w.codex_compaction_threshold ?? 3,
    monitoringRefreshSeconds: w.monitoring_refresh_seconds ?? 5,
    // 120 is the server's shipped default (StoppingTimeoutSecs), the value an
    // install that never touched the knob runs on.
    acceleratedGraceSecs: w.accelerated_grace_secs ?? 120,
    outsourceMaxParallel: w.outsource_max_parallel ?? 0,
    // ?? that segment's shipped default, not 0: a server too old to send the
    // field still caps at it, and a 0 here would read as "no cap" to every
    // caller. Duty has its own, smaller default; every other segment shares one
    // (T-ae38) — the numbers live in DOC_CAP_CHARS_DEFAULTS, not here.
    docCapCharsDuty: w.doc_cap_chars_duty ?? DOC_CAP_CHARS_DEFAULTS.duty,
    docCapCharsInsight: w.doc_cap_chars_insight ?? DOC_CAP_CHARS_DEFAULTS.insight,
    docCapCharsLearning:
      w.doc_cap_chars_learning ?? DOC_CAP_CHARS_DEFAULTS.learning,
    docCapCharsManualSop: w.doc_cap_chars_manual_sop ?? DOC_CAP_CHARS_DEFAULTS.manualSop,
    docCapCharsManualLearnings:
      w.doc_cap_chars_manual_learnings ?? DOC_CAP_CHARS_DEFAULTS.manualLearnings,
    // T-791e boot-context caps — same rule and same reason as the five above.
    docCapCharsSystemInteraction:
      w.doc_cap_chars_system_interaction ??
      DOC_CAP_CHARS_DEFAULTS.systemInteraction,
    docCapCharsBootSequence:
      w.doc_cap_chars_boot_sequence ?? DOC_CAP_CHARS_DEFAULTS.bootSequence,
    docCapCharsOffboard:
      w.doc_cap_chars_offboard ?? DOC_CAP_CHARS_DEFAULTS.offboard,
    // T-c9b4 chat budget. Same "?? the shipped default, never 0" reasoning as
    // the caps above: against a server too old to send the field, 0 would read
    // as "no chat at all", which is the one answer that is never right.
    chatBudgetChars: w.chat_budget_chars ?? CHAT_BUDGET_CHARS_DEFAULT,
    // T-8 backup retention. Same "?? the shipped default, never 0" reasoning:
    // against a server too old to send the field, 0 would render as "keep no
    // backups", which is the one answer that is never right — and it is the
    // answer a reader would then try to raise, believing they were fixing it.
    backupRetain: w.backup_retain ?? BACKUP_RETAIN_DEFAULT,
    // The two software-update toggles (schema-optional for DTO-compat; the
    // Go wire always emits both — `?? false` only fires against an older
    // server, where OFF is exactly the honest reading).
    updaterReceiveBeta: w.updater_receive_beta ?? false,
    updaterAutoUpdate: w.updater_auto_update ?? false,
    // Studio name (T-d693; schema-optional for DTO-compat — the Go wire always
    // emits it). "" = never set; the topbar substitutes the localized default.
    orgName: w.org_name ?? "",
    // Owner nickname (T-0b41; schema-optional for DTO-compat — the Go wire
    // always emits it). "" = never set; the profile pill substitutes t.user.
    ownerName: w.owner_name ?? "",
    pushContactEmail: w.push_contact_email ?? "",
    // Cockpit display prefs (T-0b41-p2; schema-optional for DTO-compat — the Go
    // wire always emits them). "" = never set; the frontend keeps its
    // localStorage cache / default and reconciles a real value in at login.
    displayTheme: w.display_theme ?? "",
    displayLanguage: w.display_language ?? "",
    // Layout width (T-756f; schema-optional for DTO-compat — the Go wire always
    // emits it). Absent maps to false, which is exactly the honest reading: an
    // older server has no wide layout, so the cockpit stays narrow.
    displayWide: w.display_wide ?? false,
    // The first-run onboarding report (T-ba62). Absent/null is the NORMAL
    // state (onboarding never ran on this database) and maps to null — the
    // mapper never manufactures a report, so "no report" can never be
    // misread as "onboarding succeeded".
    onboarding: w.onboarding ? toOnboardingReport(w.onboarding) : null,
  };
}

// ── Themes (T-83ef) ─────────────────────────────────────────────

// The theme wire shapes are read straight off the generated schema here rather
// than through wire.ts: same single source of truth (generated/schema.ts), one
// hop shorter. If wire.ts later grows Wire* aliases for these three DTOs, point
// these at them — the shapes are identical.
type WireThemeBundle = components["schemas"]["ThemeBundleDTO"];
type WireThemeListItem = components["schemas"]["ThemeListItemDTO"];
type WireThemeWriteReceipt = components["schemas"]["ThemeWriteReceiptDTO"];
type WireThemeDeleteResult = components["schemas"]["ThemeDeleteResultDTO"];

/** ThemeBundleDTO → ThemeBundle. Every optional overlay is carried through
 * VERBATIM when present and left ABSENT when absent — never fabricated to an
 * empty object. That distinction is load-bearing: an omitted passthrough here
 * silently empties an uploaded logo / nav icon / background on every read-back
 * (reload + login), and the emptied bundle is then what theme export writes. */
export function toThemeBundle(w: WireThemeBundle): ThemeBundle {
  return {
    id: w.id,
    name: w.name,
    colors: { ...w.colors },
    // Per-language wording overlay (T-16a1 P3).
    ...(w.wording !== undefined ? { wording: w.wording } : {}),
    // Font overlay (T-16a1 P4).
    ...(w.fonts !== undefined ? { fonts: w.fonts } : {}),
    // Per-role avatar overlay (T-16a1 P5 / T-ea81).
    ...(w.avatars !== undefined ? { avatars: w.avatars } : {}),
    // Logo + nav-icon overlays (T-ea81).
    ...(w.logo !== undefined ? { logo: w.logo } : {}),
    ...(w.navIcons !== undefined ? { navIcons: w.navIcons } : {}),
    // Outer-canvas background image + its display mode (T-081b).
    ...(w.backgrounds !== undefined ? { backgrounds: w.backgrounds } : {}),
    ...(w.backgroundModes !== undefined
      ? {
          backgroundModes: w.backgroundModes as ThemeBundle["backgroundModes"],
        }
      : {}),
  };
}

/** ThemeListItemDTO → ThemeListItem. Pure passthrough — the list carries id +
 * name and nothing else, and this mapper must not invent the rest of a bundle
 * from them (an empty `colors` here would render as a theme that erases the
 * cockpit's palette). */
export function toThemeListItem(w: WireThemeListItem): ThemeListItem {
  return { id: w.id, name: w.name };
}

/** ThemeWriteReceiptDTO → ThemeWriteReceipt (snake→camel passthrough). */
export function toThemeWriteReceipt(w: WireThemeWriteReceipt): ThemeWriteReceipt {
  return {
    id: w.id,
    created: w.created,
    orderIdx: w.order_idx,
    updatedAt: w.updated_at,
  };
}

/** ThemeDeleteResultDTO → ThemeDeleteResult (snake→camel passthrough).
 * `displayThemeReset` is passed through verbatim: it is the server's report
 * that the ACTIVE theme was the one deleted, and nothing on this side may
 * re-derive it. */
export function toThemeDeleteResult(w: WireThemeDeleteResult): ThemeDeleteResult {
  return {
    id: w.id,
    deleted: w.deleted,
    displayThemeReset: w.display_theme_reset,
  };
}

/** WireSettings.onboarding → OnboardingReportView. Pure passthrough
 * (snake→camel); every field is schema-optional for DTO-compat, and each
 * fallback is the honest reading of an absent value — never a fabricated
 * success (an unknown state stays "", it does not become "ok"). */
export function toOnboardingReport(
  w: NonNullable<WireServerSettings["onboarding"]>
): OnboardingReportView {
  return {
    state: w.state,
    startedAt: w.started_at ?? 0,
    finishedAt: w.finished_at ?? 0,
    // 🔴 ABSENT = NEVER DISMISSED (T-0648). There is no migration, so every
    // report row written before the field existed arrives without it. 0 is the
    // honest reading of that absence; the other one would swallow the banner on
    // every install that predates this change.
    dismissedAt: w.dismissed_at ?? 0,
    steps: (w.steps ?? []).map((s) => ({
      name: s.name,
      ok: s.ok ?? false,
      // Absent = no code (an older server, or a success). "" then falls through
      // to `reason` in the banner, which is exactly the pre-T-0648 behaviour.
      code: s.code ?? "",
      reason: s.reason ?? "",
      detail: s.detail ?? "",
    })),
  };
}

export function toVersion(w: WireVersion): VersionView {
  return {
    version: w.version,
    gitSha: w.git_sha,
    gitTime: w.git_time ?? null,
    catalogHash: w.catalog_hash,
    updateAvailable: w.update_available,
    // git_time / latest_version are nullable on the wire; a defaulted-away
    // field arrives as `undefined` — coalesce to null (never fabricated).
    latestVersion: w.latest_version ?? null,
  };
}

/** WireReleaseCheck → ReleaseCheckView (the explicit 檢查更新 verdict).
 * Pure passthrough; the closed status set is validated by the generated
 * schema type — nothing is manufactured here. */
export function toReleaseCheck(w: WireReleaseCheck): ReleaseCheckView {
  return {
    // The wire types status as a bare string; anything outside the closed
    // verdict set reads as the honest degraded "unknown" (never fabricated
    // certainty from an unrecognized value).
    status:
      w.status === "up_to_date" || w.status === "update_available"
        ? w.status
        : "unknown",
    currentVersion: w.current_version,
    latestTag: w.latest_tag ?? null,
    releaseUrl: w.release_url ?? null,
  };
}

/**
 * WireBackupHealth → BackupHealthView (T-da06). Snake→camel passthrough EXCEPT
 * the two closed-vocabulary strings, which are narrowed HERE — the wire types
 * them as bare `string`, and an unrecognised value that reached the components
 * as-is would fall out of every switch silently.
 *
 * 🔴 The floor for an unrecognised `status` is `unknown`, never `healthy`: this
 * whole endpoint exists because "we cannot tell" used to look exactly like
 * "you have a retreat point". An unrecognised `code` collapses to "" (no named
 * failure) — the status still carries the alarm, and `detail` still carries the
 * server's own words, so nothing is invented and nothing is hidden.
 *
 * The nullable numbers coalesce to null (a defaulted-away wire field arrives as
 * `undefined`); nothing here manufactures a timestamp or an age.
 */
export function toBackupHealth(w: WireBackupHealth): BackupHealthView {
  const status: BackupHealthStatus =
    w.status === "healthy" || w.status === "unhealthy" ? w.status : "unknown";
  const code: BackupHealthCode =
    w.code === "never_ran" || w.code === "stale" || w.code === "failed"
      ? w.code
      : "";
  return {
    status,
    code,
    detail: w.detail,
    newestBackupTs: w.newest_backup_ts ?? null,
    newestBackupAgeSecs: w.newest_backup_age_secs ?? null,
    staleAfterSecs: w.stale_after_secs,
    sinceTs: w.since_ts ?? null,
    checkedTs: w.checked_ts ?? null,
  };
}

/** Map the wire global-context doc → the view model (snake→camel). */
export function toGlobalContext(w: WireGlobalContext): GlobalContextView {
  return {
    text: w.text,
    ownerId: w.owner_id,
    schemaVersion: w.schema_version,
    isDefault: w.is_default,
  };
}

/**
 * Map one folded boot-context block → the view model (T-791e).
 *
 * 🔴 `kind` IS PASSED STRAIGHT THROUGH, AND THAT ASSIGNMENT IS A GUARD (T-3201).
 * The wire field used to be a bare string, so this mapper carried a hand-written
 * copy of the closed set to narrow it against — a second list of the same fact,
 * and the kind of copy that goes stale silently. The spec now declares the enum,
 * so the wire type IS the closed set and this line only compiles while
 * `types.ts :: BootDocKind` still spells the same one. A kind added to the spec
 * and nowhere else reddens HERE, at the seam, before anything downstream can be
 * handed a value its `switch` has no arm for.
 */
export function toBootDoc(w: WireBootDoc): BootDocView {
  return {
    kind: w.kind,
    key: w.key,
    text: w.text,
    readOnlyHead: w.read_only_head,
    body: w.body,
    sizeChars: w.size_chars,
    capChars: w.cap_chars,
    isDefault: w.is_default,
    hasSeed: w.has_seed,
    readOnly: w.read_only,
  };
}

/** Map the RESTORE RECEIPT → the view model (snake→camel). `content` keeps the
 * kind's OWN field names verbatim — they are data, not a schema the cockpit
 * gets to rename. Since T-1170 this is the only document-history answer that
 * carries actor+time alongside text; the list carries no text and the
 * named-revision read carries no actor (see `toDocumentRevision`). */
export function toDocumentHistory(
  w: WireDocumentHistoryRestore
): DocumentHistoryView {
  return {
    id: w.id,
    content: { ...w.content },
    createdTs: w.created_ts,
    actorId: w.actor_id,
  };
}

/** Map ONE named revision's body → the view model (T-1170).
 *
 * `kind` / `key` are the address echoed back and are deliberately NOT mapped:
 * the caller supplied both, and a view field with no reader is
 * indistinguishable from a live one. What the reader needs from this read is
 * the text — everything else about the revision (when, who, tombstoned, sizes)
 * it already holds from the directory row it opened. */
export function toDocumentRevision(
  w: WireDocumentHistoryVersion
): DocumentRevisionView {
  return { id: w.id, content: { ...w.content } };
}

/**
 * Map one wire revision → the DIRECTORY row (T-1170).
 *
 * 🔴 THE WIRE NAME IS `field_chars`, the view name is `sizes`, and the rename
 * is the ONE line this mapper exists to hold. Reading a name the server does
 * not send is the failure mode this seam is built to make impossible: the map
 * would simply be absent, every size would read as 0, and NOTHING would throw
 * — the version list would draw every revision as empty and `docCapBlockedFields`
 * would clear every restore, both silently and both wrong.
 *
 * `tombstoned` is its OWN boolean on the wire, not an entry of `field_chars`:
 * it is a flag, not a field of the document, and counting the characters of
 * the string "true" would put a 4 in the size map where a reader looks for
 * content.
 *
 * The counts are the SERVER's — code points, the unit `runeLength` and the
 * server's cap both use (see api/docCap.ts). Nothing is derived here: a list
 * row carries no text to derive from, which is what makes "read the revision
 * off the list" impossible rather than merely discouraged.
 */
export function toDocumentHistoryEntry(
  w: WireDocumentHistory
): DocumentHistoryEntryView {
  return {
    id: w.id,
    createdTs: w.created_ts,
    actorId: w.actor_id,
    tombstoned: w.tombstoned,
    sizes: { ...w.field_chars },
  };
}

/** Map one wire shipped default → the view model. `kind` is narrowed to the
 * closed union rather than trusted as a bare string: the caller ASKED for a
 * kind, so the echo is a consistency check, not a new vocabulary. */
export function toDocumentSeed(w: WireDocumentSeed): DocumentSeedView {
  return {
    kind: w.kind as DocumentKind,
    key: w.key,
    content: { ...w.content },
  };
}

/** Map one wire role-def doc → the view model (snake→camel).
 *
 * KEEPS `size_chars` / `cap_chars` (T-ae38) for the same reason `toInsight`
 * does: the cap is a live setting and the settings surface is admin-only, so
 * the role-definition editor's header is where a reader learns the limit. The
 * wire marks both optional for DTO-compat; a server too old to send them
 * reports 0, which the editor renders as an honest "not known" rather than as
 * a doc of length zero. */
export function toRoleSummary(
  w: WireRoleDefListItem | WireRoleDef
): RoleSummaryView {
  return {
    sizeChars: w.size_chars ?? 0,
    capChars: w.cap_chars ?? 0,
    key: w.key,
    name: w.name,
    ownerId: w.owner_id,
    schemaVersion: w.schema_version,
    isDefault: w.is_default,
    // Older payloads omit is_seed → default TRUE (fail-safe: never offer delete
    // on a doc we can't prove is custom; the server re-enforces anyway).
    isSeed: w.is_seed ?? true,
  };
}

/** Map one wire role-def doc → the FULL view (`GET /{key}` and every write
 * echo). `definition_md` is optional on the wire and ABSENT from the roster
 * answer since T-1170; `?? ""` is the empty document, not a stand-in for a
 * projection that dropped it — this mapper is only ever handed a
 * full-document response. */
export function toRoleDef(w: WireRoleDef): RoleDefView {
  return { ...toRoleSummary(w), definitionMd: w.definition_md ?? "" };
}

/** Map bootstrap wire → view. DROPS `token` on purpose: a UI preview must never
 * surface an agent credential (the endpoint returns token=null for preview
 * requests, but we exclude it from the view model regardless). */
export function toBootstrap(w: WireBootstrap): BootstrapView {
  return {
    role: w.role,
    name: w.name,
    context: w.context,
  };
}

/** Map the onboard wire result → the view model (snake→camel). `token` +
 * `bootCommand` pass through verbatim — they are secrets the UI renders into a
 * copy control only (never logged); the mapper never fabricates either. */
export function toOnboardResult(w: WireOnboardResult): OnboardResultView {
  return {
    memberId: w.member_id,
    machineId: w.machine_id,
    token: w.token,
    expiresIn: w.expires_in,
    bootCommand: w.boot_command,
  };
}

/** Map one wire machine-registry row → the view model (snake→camel). Pure
 * rename; `online` passes through untouched (never fabricated). */
export function toMachine(w: WireMachine): MachineView {
  return {
    machineId: w.machine_id,
    displayName: w.display_name,
    online: w.online,
    isSelf: w.is_self,
    // Absent (older server) and null (unknown verdict) both read as the
    // honest unknown — the UI renders "—", never a guessed freshness.
    binStatus: toBinStatus(w.bin_status),
    // Reported, not computed: absent means the box has not received the build
    // that reports a shape — a different fact from the reported "unknown".
    wardenShape: toWardenShape(w.warden_shape),
    // Reported, not computed, for the same reason and with the same absent-vs-
    // reported distinction as the shape above.
    cutoverEffect: toCutoverEffect(w.cutover_effect),
    // The claude CLI probe columns (T-97ee): absent (older server) and null
    // (unknown — an older warden that never probed) both read as the honest
    // unknown; the UI shows only claudeVersion (table column, "—" on null).
    claudeVersion: w.claude_version ?? null,
    claudeCredSource: toClaudeCredSource(w.claude_cred_source),
    claudeSubReadable: w.claude_sub_readable ?? null,
    runtimeCapabilities: Object.fromEntries(
      Object.entries(w.runtime_capabilities ?? {}).map(([runtime, capability]) => [
        runtime,
        {
          installed: capability.installed ?? null,
          loggedIn: capability.logged_in ?? null,
          version: capability.version ?? null,
        },
      ])
    ),
  };
}

/** Narrow the wire `bin_status` to the closed BinStatus vocabulary; anything
 * absent/unrecognized reads as the honest unknown (null), never a verdict. */
function toBinStatus(v: string | null | undefined): BinStatus {
  return v === "current" || v === "stale" ? v : null;
}

/** Narrow the wire `warden_shape` to the closed WardenShape vocabulary.
 *
 * Note what "the honest fallback" is here, because this field has FOUR states
 * and one of them is a word that also sounds like a fallback. `"unknown"` is a
 * REPORTED value — it means the anchor-cutover build is on that box and could
 * not read its own parent. It is NOT the landing spot for a string the FE does
 * not recognise: narrowing garbage to `"unknown"` would assert that build is
 * running, which we have no evidence for. So, exactly like `toBinStatus`,
 * absent/null/unrecognised all fall to `null` — "this machine has told us
 * nothing we understand about its shape". */
function toWardenShape(v: string | null | undefined): WardenShape {
  return v === "anchor" || v === "legacy" || v === "unknown" ? v : null;
}

/** Narrow the wire `cutover_effect` to the closed CutoverEffect vocabulary.
 *
 * The same trap as `toWardenShape`, one notch sharper: "unproven" sounds like a
 * fallback and is not one. It is a REPORTED verdict — the machine ran the check
 * and could not settle it — so narrowing an unrecognised string to it would
 * assert a check that may never have run. Absent/null/unrecognised all fall to
 * `null`, and nothing here ever narrows toward "effective". */
function toCutoverEffect(v: string | null | undefined): CutoverEffect {
  return v === "effective" || v === "not_effective" || v === "unproven"
    ? v
    : null;
}

/** Narrow the wire `claude_cred_source` to the closed vocabulary; anything
 * absent/unrecognized reads as the honest unknown (null), never a verdict. */
function toClaudeCredSource(v: string | null | undefined): ClaudeCredSource {
  return v === "file" || v === "keychain" || v === "both" || v === "none"
    ? v
    : null;
}

/** Map the bootstrap-on-server wire result → the view model (snake→camel). Pure
 * rename; `ok` / `exit_code` / `log` pass through verbatim — the mapper never
 * fabricates a success, and it NEVER drops `log` (the failure reason on !ok). */
export function toBootstrapResult(w: WireBootstrapResult): BootstrapResultView {
  return {
    ok: w.ok,
    exitCode: w.exit_code,
    log: w.log,
  };
}

/** Map the DELETE wire result → the view model (snake→camel). Pure rename; a
 * PURE soft-delete carries no command string — just the removal outcome. */
export function toDeleteResult(w: WireDeleteResult): DeleteResultView {
  return {
    memberId: w.member_id,
    machineId: w.machine_id,
    removed: w.removed,
  };
}

/** Map the uninstall wire result → the view model (snake→camel). Pure rename;
 * `dispatched` passes through verbatim (whether the uninstall RPC was driven). */
export function toUninstallResult(w: WireUninstallResult): UninstallResultView {
  return {
    memberId: w.member_id,
    machineId: w.machine_id,
    dispatched: w.dispatched,
  };
}

/** Map the teardown-on-server wire result → the view model (snake→camel). Pure
 * rename; `ok` / `exit_code` / `log` / `removed` pass through verbatim — the mapper
 * never fabricates a success, never drops `log` (the failure reason on !ok), and
 * `removed` honestly reports whether the member row was soft-deleted (iff `ok`). */
export function toTeardownHereResult(
  w: WireTeardownHereResult,
): TeardownHereResultView {
  return {
    ok: w.ok,
    exitCode: w.exit_code,
    log: w.log,
    removed: w.removed,
  };
}

/** Map the wire lessons doc → the view model (snake→camel). DROPS `owner_id` /
 * `schema_version` on purpose (the view needs neither, and carries no owner
 * credential). Pure passthrough of `text` — never fabricated; an empty seed
 * stays empty so the UI can show an honest empty state. */
export function toLessons(w: WireLessons): LessonsView {
  return {
    // T-ae38: KEPT, not dropped. The wire has carried these since T-3aeb and
    // this mapper threw them away, so the Learning card was the one journal
    // block whose usage an agent could not see — it learned its limit by being
    // refused. Same fields, same reason, as toInsight.
    sizeChars: w.size_chars ?? 0,
    capChars: w.cap_chars ?? 0,
    roleKey: w.role_key,
    text: w.text,
    isDefault: w.is_default,
  };
}

/** Map the folded PER-ROLE insight doc (T-3809) → the view model. Unlike
 * `toLessons` this KEEPS `size_chars` / `cap_chars`: the cap is the live
 * `doc.cap_chars.insight` setting and the settings surface is admin-only, so the card
 * header is where the owner reads it. Dropping them here would make the card's
 * one honest number un-renderable. */
export function toInsight(w: WireInsight): InsightView {
  return {
    roleKey: w.role_key,
    text: w.text,
    isDefault: w.is_default,
    sizeChars: w.size_chars,
    capChars: w.cap_chars,
    // Older payloads omit has_seed → default FALSE. 🔴 The fail-safe direction
    // is the OPPOSITE of toRoleDef's is_seed: there the safe answer is "assume
    // a seed exists, don't offer delete"; here the field gates whether the
    // 初始版本 reset row is drawn at all, so an unproven true would draw a row
    // that 404s. Absent evidence, offer nothing.
    hasSeed: w.has_seed ?? false,
  };
}

/** Map one wire webhook endpoint → the view model. `token` is passed through
 * verbatim (the panel composes the callback URL from it). */
export function toWebhookEndpoint(w: WireWebhookEndpoint): WebhookEndpoint {
  return {
    endpointId: w.endpoint_id,
    purpose: w.purpose ?? "",
    status: w.status === "disabled" ? "disabled" : "enabled",
    createdTs: w.created_ts ?? 0,
    token: w.token,
    platform: w.platform ?? "generic",
    hasSigningSecret: w.has_signing_secret ?? false,
    lastReceivedTs: w.last_received_ts ?? 0,
    deliveredCount: w.delivered_count ?? 0,
    droppedCount: w.dropped_count ?? 0,
    lastDropReason: w.last_drop_reason ?? "",
  };
}

/** Map one wire webhook request-log row → the view model (pure passthrough:
 * headers/body are raw debug text, never fabricated or prettified here). */
export function toWebhookRequestLog(
  w: WireWebhookRequestLog,
): WebhookRequestLog {
  return {
    ts: w.ts,
    outcome: w.outcome,
    headers: w.headers,
    body: w.body,
    truncated: w.truncated,
  };
}

/** Map one wire scheduled message → the view model (T-f059). `timezone` is NOT
 * defaulted to anything local: the wire always carries an explicit IANA name
 * (the DTO requires it precisely so nobody reads a blank as "the server's
 * zone"), so an absent one stays honestly empty rather than borrowing this
 * browser's. */
export function toScheduledMessage(w: WireScheduledMessage): ScheduledMessage {
  return {
    id: w.id,
    memberId: w.member_id,
    label: w.label ?? "",
    body: w.body,
    cadence: w.cadence,
    dayOfWeek: w.day_of_week ?? 0,
    dayOfMonth: w.day_of_month ?? 1,
    hour: w.hour,
    minute: w.minute,
    // The four `custom` sets are additive-optional on the wire (a reader
    // written before T-49e7 must keep working), so an absent one lands as the
    // EMPTY set — which is exactly what every non-custom cadence carries.
    //
    // 🔴 The absent-means-every-month rule of `custom_months` belongs to the
    // REQUEST, not to the response: the server resolves it in the handler and
    // every row it serves lists its own months. Repeating the rule here would
    // make a truly month-less row (there is no such thing) read as the whole
    // year, and would put the two meanings the 422 exists to separate back
    // into one value.
    customMonths: w.custom_months ?? [],
    customDays: w.custom_days ?? [],
    customHours: w.custom_hours ?? [],
    customMinutes: w.custom_minutes ?? [],
    timezone: w.timezone ?? "",
    status: w.status === "disabled" ? "disabled" : "enabled",
    lastFiredSlot: w.last_fired_slot ?? "",
    lastFiredTs: w.last_fired_ts ?? 0,
    createdTs: w.created_ts ?? 0,
  };
}

// ── Resume summary (RESUME SUMMARY panel section, T-8b0d) ─────────────────────

const EMPTY_RESUME_OVERVIEW: ResumeOverviewView = {
  chatCount: 0,
  chatChars: 0,
  tasksReturned: 0,
  tasksOpenTotal: 0,
  tasksDetailChars: 0,
  cardsWaiting: 0,
  cardsAnsweredRecent: 0,
  rosterChars: 0,
  machinesChars: 0,
  stepsOnAnsweredCard: 0,
  stepsOnAnsweredCardChars: 0,
};

/** Map one wire resume-snapshot overview block → the view model (pure
 * passthrough — every field is server-computed). */
export function toResumeOverview(w: WireResumeOverview): ResumeOverviewView {
  return {
    chatCount: w.chat_count,
    chatChars: w.chat_chars,
    tasksReturned: w.tasks_returned,
    tasksOpenTotal: w.tasks_open_total,
    tasksDetailChars: w.tasks_detail_chars,
    cardsWaiting: w.cards_waiting,
    cardsAnsweredRecent: w.cards_answered_recent,
    // The two studio-floor block sizes (T-1b09). Optional on the wire, so an
    // older server reads as 0 — that is "this snapshot carries no such block",
    // which is exactly what a 0-length block means.
    rosterChars: w.roster_chars ?? 0,
    machinesChars: w.machines_chars ?? 0,
    // These are optional for older servers. Zero means this snapshot carries
    // no answered-card pointers; it never means every task is clear.
    stepsOnAnsweredCard: w.steps_on_answered_card ?? 0,
    stepsOnAnsweredCardChars: w.steps_on_answered_card_chars ?? 0,
  };
}

/** Map one answered-card pointer. It carries only a step/card address; the
 * answer itself remains behind `get_reply_card`, and this pointer never means
 * the step is complete. */
export function toResumeAnsweredCardStep(
  w: WireResumeAnsweredCardStep,
): ResumeAnsweredCardStepView {
  return {
    stepId: w.step_id,
    stepName: w.step_name,
    cardId: w.card_id,
  };
}

/** Map one wire resume-snapshot LIGHT task row → the view model. */
export function toResumeTask(w: WireResumeTask): ResumeTaskView {
  return {
    id: w.id,
    taskNo: w.task_no,
    title: w.title ?? "",
    typeKey: w.type_key ?? "",
    status: w.status,
    priority: w.priority,
    waitingReason: w.waiting_reason ?? "",
    currentStepId: w.current_step_id ?? "",
    currentStepName: w.current_step_name ?? "",
    progressDone: w.progress_done,
    progressTotal: w.progress_total,
    updatedTs: w.updated_ts ?? 0,
    detailChars: w.detail_chars,
    // An absent optional list is the normal, honest no-pointer case.
    answeredCardSteps: (w.answered_card_steps ?? []).map(
      toResumeAnsweredCardStep,
    ),
    // T-91 — the handover hold and the reverse dependency edge. The ?? ""
    // arms are the ordinary additive-optional posture, not a guess: a server
    // that predates these fields honestly means "no hold, nobody waiting".
    lock: w.lock ?? "",
    reassignedFrom: w.reassigned_from ?? "",
    reassignedFromKind: w.reassigned_from_kind ?? "",
    blocking: w.blocking ?? [],
  };
}

/** Map one wire roster row of the wake snapshot → the view model. Pure
 * passthrough; every "" is the server's own honest empty (a contractor carries
 * no role, a member carries no bound task) and is never substituted. */
export function toResumeRosterMember(
  w: WireResumeRosterMember,
): ResumeRosterMemberView {
  return {
    id: w.id,
    name: w.name,
    kind: w.kind,
    roleName: w.role_name,
    duty: w.duty,
    currentTask: w.current_task,
    taskStatus: w.task_status,
    waitingReason: w.waiting_reason,
    progressDone: w.progress_done,
    progressTotal: w.progress_total,
    machine: w.machine,
    presence: w.presence,
  };
}

/** Map the wire machine block of the wake snapshot → the view model. */
export function toResumeMachines(
  w: WireResumeMachines,
): ResumeMachinesView {
  return {
    list: (w.list ?? []).map((m) => ({
      machineId: m.machine_id,
      displayName: m.display_name,
      online: m.online,
    })),
    youAreOn: w.you_are_on ?? "",
  };
}

/** Map a target member's wire resume snapshot → the view model (RESUME
 * SUMMARY panel section). `identity`/`overview` are optional on the wire only
 * to keep old hand-built fixtures valid — a real snapshot always sets them;
 * `overview` falls back to all-zero counts rather than `undefined` so the
 * panel never has to null-check the size figures it renders.
 *
 * 🔴 EVERY section the server assembles is carried across. This mapper used to
 * drop `roster` and `machines` on the floor — the payload had them, the view
 * model did not declare them, and nothing was red — so the cockpit could not
 * show a snapshot section the agent reading the same payload could see. The
 * whole point of this panel is that the two line up section by section, and a
 * seam that silently narrows the payload defeats it before the component is
 * ever reached. `machines` stays NULLABLE (the wire distinguishes "no machine
 * block" from "a block with an empty list"); `roster` collapses an absent list
 * to `[]`, which is the same statement the wire's empty list makes. */
export function toMemberResumeSummary(
  w: WireResumeSummary,
): MemberResumeSummaryView {
  return {
    identity: w.identity ?? null,
    chat: (w.chat ?? []).map(toChatMessage),
    tasks: (w.tasks ?? []).map(toResumeTask),
    overview: w.overview ? toResumeOverview(w.overview) : EMPTY_RESUME_OVERVIEW,
    note: w.note ?? "",
    generatedAt: w.generated_at ?? "",
    chatEarlierOmitted: {
      omitted: w.chat_earlier_omitted?.omitted ?? false,
      hint: w.chat_earlier_omitted?.hint ?? "",
    },
    // The SIZE marker. An absent block maps to all-zero / false — the same
    // statement the server makes for a snapshot inside its budget — so the
    // panel never has to distinguish "no marker" from "marker down".
    roster: (w.roster ?? []).map(toResumeRosterMember),
    machines: w.machines ? toResumeMachines(w.machines) : null,
  };
}

// ── T-33 傳承 (lore) ────────────────────────────────────────────────────────

/** Map one search hit → the summary view (snake→camel, nothing derived).
 *
 * Every honesty marker the wire carries is carried through: `tier` WITHOUT
 * `applied.tiered_by` means something different from what it says, and
 * `trust_fell_back` is what separates a class the table KNEW from one that was
 * guessed by failing closed. Dropping either here would leave the screen
 * unable to tell the two apart, and nothing would throw. */
export function toLoreEntrySummary(
  w: WireLoreSearchHit
): LoreEntrySummaryView {
  return {
    entryId: w.entry_id,
    trigger: w.trigger,
    content: w.content,
    subjects: [...w.subjects],
    actions: [...w.actions],
    origin: w.origin,
    tier: w.tier,
    tierNote: w.tier_note,
    trustScope: w.trust_scope,
    trustFellBack: w.trust_fell_back,
  };
}

/** Map one search answer → the view model.
 *
 * 🔴 `total` is the server's count BEFORE the cap and is NOT re-derived from
 * `entries.length`: a caller that read the page length as the total would show
 * a capped page as the whole store, and the cap is silent. `subject_resolved`
 * likewise survives instead of being folded into an empty list — 「this subject
 * has nothing on it」 and 「this subject does not exist」 are different answers,
 * and the second one is a typo somebody needs to see. */
export function toLoreSearch(w: WireLoreSearchResult): LoreSearchView {
  return {
    entries: w.entries.map(toLoreEntrySummary),
    total: w.total,
    truncated: w.truncated,
    subjectResolved: w.subject_resolved,
    unresolvedSubject: w.unresolved_subject,
    applied: {
      subject: w.applied.subject,
      actions: [...w.applied.actions],
      query: w.applied.query,
      queryMatch: w.applied.query_match,
      limit: w.applied.limit,
      tieredBy: [...w.applied.tiered_by],
    },
    unmappedActions: [...w.unmapped_actions],
  };
}

/** Map one revision catalogue line → the view model. `shrink_chars` is the
 * field the whole 版本時間軸 exists for; it is a COUNT from the server and is
 * never derived here, because the catalogue carries no text to derive from. */
export function toLoreRevisionRow(
  w: WireLoreRevisionRow
): LoreRevisionRowView {
  return {
    revisionId: w.revision_id,
    createdTs: w.created_ts,
    actorId: w.actor_id,
    sha256: w.sha256,
    shrinkChars: w.shrink_chars,
  };
}

/** Map one event → the view model. 人／地／物 are carried through EXACTLY as the
 * wire sent them, empty string included: this is the seam where a placeholder
 * would be cheapest to add and most damaging, because 「nobody could find out
 * who」 and 「nobody has looked yet」 would render as the same word from here on
 * and nothing downstream could tell them apart again. */
export function toLoreEvent(w: WireLoreEvent): LoreEventView {
  return {
    happenedTs: w.happened_ts,
    what: w.what,
    actor: w.actor,
    place: w.place,
    object: w.object,
  };
}

/** Map one entry detail → the view model.
 *
 * Every body cell is mapped verbatim, empty string included. 第 1、2 格
 * (`trigger` / `content`) cannot be blank — the write path refuses them at the
 * upsert seam — so a blank one here means the entry predates 五格. 第 3、4 格
 * are optional and a blank one is ordinary, which is exactly why it is mapped
 * rather than dropped.
 *
 * The surface prints an empty cell WITH its name; substituting a placeholder
 * here would make 「the writer left this blank」 and 「this entry has no such
 * section」 render identically.
 *
 * 🔴 `events` is mapped in the order the wire sent it, which is the order the
 * events HAPPENED. Re-sorting here (by write order, say) would silently answer
 * a different question than the one the route answers. */
export function toLoreEntryDetail(
  w: WireLoreEntryDetail
): LoreEntryDetailView {
  return {
    entryId: w.entry_id,
    trigger: w.trigger,
    content: w.content,
    retireWhen: w.retire_when,
    problem: w.problem,
    events: w.events.map(toLoreEvent),
    subjects: [...w.subjects],
    actions: [...w.actions],
    origin: w.origin,
    status: w.status,
    original: w.original,
    sha256: w.sha256,
    supersedes: w.supersedes,
    writtenBy: w.written_by,
    revisions: w.revisions.map(toLoreRevisionRow),
  };
}

/** Map one revision's full text → the view model. */
export function toLoreRevision(w: WireLoreRevision): LoreRevisionView {
  return {
    revisionId: w.revision_id,
    entryId: w.entry_id,
    body: w.body,
    sha256: w.sha256,
    createdTs: w.created_ts,
    actorId: w.actor_id,
    shrinkChars: w.shrink_chars,
  };
}

/** 待審一列。`suggestion` 是空字串就原樣留空 —— 不在這裡補一個預設建議。 */
export function toLorePendingEntity(
  w: WireLorePendingEntity
): LorePendingEntityView {
  return {
    entityId: w.entity_id,
    canonical: w.canonical,
    type: w.type,
    name: w.name,
    createdTs: w.created_ts,
    entries: w.entries,
    suggestion: w.suggestion,
    mergeTarget: w.merge_target,
    similar: (w.similar ?? []).map((r) => ({
      entityId: r.entity_id,
      canonical: r.canonical,
      reason: r.reason,
    })),
    sampleShort: w.sample_short,
  };
}

/** 核可／合併的收據。 */
export function toLoreEntityGovernance(
  w: WireLoreEntityGovernance
): LoreEntityGovernanceView {
  return {
    entityId: w.entity_id,
    canonical: w.canonical,
    pending: w.pending,
    mergedInto: w.merged_into,
    kind: w.kind,
    reason: w.reason,
    actorId: w.actor_id,
    createdTs: w.created_ts,
  };
}
