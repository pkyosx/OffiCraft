// api/mappers.ts — pure wire→view-model mapping (no I/O, no side effects).
//
// This is the ONE place the snake_case wire shape becomes the camelCase view
// model the components consume. Keeping it pure + centralised means the real
// backend only has to return the frozen wire shape — the UI never changes.

import type {
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
  GlobalContextView,
  RoleDefView,
  BootstrapView,
  LessonsView,
  OnboardResultView,
  DeleteResultView,
  UninstallResultView,
  UpgradeResultView,
  TeardownHereResultView,
  BootstrapResultView,
  MachineView,
  BinStatus,
  ClaudeCredSource,
} from "../types";
import type {
  WireMember,
  WireChatMessage,
  WireMachine,
  WireMonSession,
  WireMonMachine,
  WireMonAccount,
  WireMonitoring,
  WireVersion,
  WireReleaseCheck,
  WireGlobalContext,
  WireRoleDef,
  WireBootstrap,
  WireLessons,
  WireOnboardResult,
  WireDeleteResult,
  WireUninstallResult,
  WireUpgradeResult,
  WireTeardownHereResult,
  WireBootstrapResult,
  WireChatRead,
  WireChatGalleryEntry,
  WireReplyCard,
  WireWebhookEndpoint,
  WireWebhookRequestLog,
  WireServerSettings,
  WireTask,
  WireTaskListItem,
  WireTaskStep,
  WireTaskArtifact,
  WireOutsourceWorker,
  WireTaskManual,
  WireTaskManualUpdate,
  WireTaskReassign,
} from "./wire";
import type {
  ChatMessage,
  ChatReadReceipt,
  GalleryAttachment,
  ReplyCard,
  ServerSettingsView,
  OnboardingReportView,
  TaskView,
  TaskStepView,
  TaskArtifactView,
  OutsourceWorkerView,
  TaskTypeView,
  TaskManualView,
  TaskManualPatch,
  TaskReassignInput,
  ManualAssigneeView,
  WebhookEndpoint,
  WebhookRequestLog,
} from "./adapter";

/**
 * Map one wire member → the view-model `Member`. Every field's source is noted.
 */
export function toMember(w: WireMember): Member {
  return {
    id: w.id, // wire id (attribution key)
    memberId: w.member_no, // display badge "MB-XXX###"
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
    status: (w.presence === "stopping"
      ? "online"
      : w.presence === "stopped"
        ? "offline"
        : w.presence) as MemberStatus,
    // lifecycle carries the REAL five-state presence verbatim (backend guarantees
    // one of offline/waking/online/stopping/stopped). Honest passthrough — never
    // a fabricated value.
    lifecycle: w.presence as MemberLifecycle,
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
export function toReplyCard(w: WireReplyCard): ReplyCard {
  return {
    id: w.id,
    from: w.from,
    kind: w.kind,
    summary: w.summary ?? "",
    body: w.body ?? "",
    options: w.options ?? [],
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
          optionIdx: w.answer.option_idx,
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

/** Map one wire task → `TaskView`. Pure snake→camel passthrough. Honesty:
 * `progressDone`/`progressTotal` are the SERVER's leaf counts (never recomputed
 * from steps here); `closedTs` stays null while open; steps keep the server's
 * timeline order (order_idx asc — re-asserted here so the UI never depends on
 * response ordering). */
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
    codename: w.codename,
    model: w.model ?? "",
    effort: w.effort ?? "",
    status: w.status ?? "",
    taskId: w.task_id,
    taskTitle: w.task_title ?? "",
    taskStatus: w.task_status ?? "",
    createdTs: w.created_ts ?? 0,
    unreadCount: w.unread_count ?? 0,

    // ── T-f190 detail-panel fold ──────────────────────────────────────────
    // presence = the REAL-liveness projection (A案 P6, member vocabulary); honest "" when a
    // defaulted-away wire field arrives (older server) → the panel treats it
    // as released/off-panel, never a fabricated live state.
    presence: w.presence ?? "",
    // machine = the ACTUAL dispatch target (already resolved to a display name
    // server-side); "" when never dispatched → the panel shows 「尚未分配」.
    machine: w.machine ?? "",
    desiredMachineId: w.desired_machine_id ?? "",
    // Runtime facts: nullable on the wire (null = unreported). A defaulted-away
    // field arrives as undefined — coalesce to null (the honest dash), never 0.
    account: w.account ?? null,
    contextPct: w.context_pct ?? null,
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
    desiredState: w.desired_state ?? "online",
  };
}

/** Map one wire task manual → the LIGHT `TaskTypeView` the type filter reads.
 * DROPS fields/sop/learnings/assignee on purpose — the tasks page must not
 * grow a manual-editing surface (that is 設定 › 任務手冊's `toTaskManual`). */
export function toTaskType(w: WireTaskManual): TaskTypeView {
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
      model: typeof a["model"] === "string" ? a["model"] : "",
      effort: typeof a["effort"] === "string" ? a["effort"] : "",
      // 0 = 無限 (unlimited per-type copies — spec TaskManualDTO).
      copies: typeof a["copies"] === "number" ? a["copies"] : 1,
      // Spawn placement preference; absent ⇒ "auto" (the wire default).
      machine:
        typeof a["machine"] === "string" && a["machine"] !== ""
          ? a["machine"]
          : "auto",
    };
  }
  return null;
}

/** Map one wire task manual → the FULL `TaskManualView` (設定 › 任務手冊).
 * Pure snake→camel passthrough; the open assignee object narrows through
 * `toManualAssignee` (unset {} → null). */
export function toTaskManual(w: WireTaskManual): TaskManualView {
  return {
    typeKey: w.type_key,
    displayName: w.display_name ?? "",
    purpose: w.purpose ?? "",
    fields: (w.fields ?? []).map((f) => ({
      name: f.name,
      required: f.required ?? false,
      isKey: f.is_key ?? false,
    })),
    sopMd: w.sop_md ?? "",
    learnings: w.learnings ?? "",
    assignee: toManualAssignee(w.assignee as Record<string, unknown>),
    updatedTs: w.updated_ts ?? 0,
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
                model: patch.assignee.model,
                effort: patch.assignee.effort,
                copies: patch.assignee.copies,
                machine: patch.assignee.machine,
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
            model: null,
            effort: null,
            machine: null,
          }
        : {
            kind: "outsource",
            member_id: null,
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
    status: w.presence as MemberStatus,
    // Telemetry is null-until-reported on the wire; a defaulted-away field
    // arrives as `undefined` — coalesce to null so the UI renders "—", never a
    // fabricated number.
    contextPct: w.context_pct ?? null,
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
    claudeVersion: w.claude_version ?? null,
    claudeCredSource: toClaudeCredSource(w.claude_cred_source),
    claudeSubReadable: w.claude_sub_readable ?? null,
  };
}

/** Map one wire mon account → `MonAccountView`. Narrows the opaque usage dicts
 * to the shape the account card renders; every metric stays honest (null when
 * absent). Window dicts come from the backend `shape_window`
 * (domain/token_pacing.py) which emits `used_pct`/`elapsed_pct`/`pace` — the
 * card's usage bar reads used_pct, the pace marker reads elapsed_pct, and the
 * 7-day window is "overheated" when pace === "hot". */
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
        }
      : null,
    sevenDay: w.seven_day
      ? {
          usagePct: numOrNull(w.seven_day.used_pct),
          timePct: numOrNull(w.seven_day.elapsed_pct),
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
    tokenTtl: w.token_ttl,
    handoverPct: w.handover_pct,
    outsourceMaxParallel: w.outsource_max_parallel ?? 0,
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
    // Cockpit display prefs (T-0b41-p2; schema-optional for DTO-compat — the Go
    // wire always emits them). "" = never set; the frontend keeps its
    // localStorage cache / default and reconciles a real value in at login.
    displayTheme: w.display_theme ?? "",
    displayLanguage: w.display_language ?? "",
    // Custom theme bundles (T-16a1 P2; schema-optional for DTO-compat — the Go
    // wire always emits an array). Absent maps to [] — never a fabricated theme.
    customThemes: (w.custom_themes ?? []).map((b) => ({
      id: b.id,
      name: b.name,
      colors: { ...b.colors },
      // Per-language wording overlay (T-16a1 P3) — optional; carried through
      // verbatim when present (never fabricated to an empty object).
      ...(b.wording !== undefined ? { wording: b.wording } : {}),
      // Font overlay (T-16a1 P4) — optional; carried through verbatim when
      // present (never fabricated to an empty object).
      ...(b.fonts !== undefined ? { fonts: b.fonts } : {}),
      // Avatar overlay (bb2e3b4) — optional; carried through verbatim when
      // present (never fabricated to an empty object).
      ...(b.avatars !== undefined ? { avatars: b.avatars } : {}),
      // Logo + nav-icon overlays (T-ea81) — optional; carried through verbatim
      // when present. Omitting these dropped uploaded logo/nav icons on every
      // read-back (reload + login), which also emptied them from theme export.
      ...(b.logo !== undefined ? { logo: b.logo } : {}),
      ...(b.navIcons !== undefined ? { navIcons: b.navIcons } : {}),
    })),
    // The first-run onboarding report (T-ba62). Absent/null is the NORMAL
    // state (onboarding never ran on this database) and maps to null — the
    // mapper never manufactures a report, so "no report" can never be
    // misread as "onboarding succeeded".
    onboarding: w.onboarding ? toOnboardingReport(w.onboarding) : null,
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
    steps: (w.steps ?? []).map((s) => ({
      name: s.name,
      ok: s.ok ?? false,
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

/** Map the wire global-context doc → the view model (snake→camel). */
export function toGlobalContext(w: WireGlobalContext): GlobalContextView {
  return {
    text: w.text,
    ownerId: w.owner_id,
    schemaVersion: w.schema_version,
    isDefault: w.is_default,
  };
}

/** Map one wire role-def doc → the view model (snake→camel). */
export function toRoleDef(w: WireRoleDef): RoleDefView {
  return {
    key: w.key,
    name: w.name,
    definitionMd: w.definition_md,
    ownerId: w.owner_id,
    schemaVersion: w.schema_version,
    isDefault: w.is_default,
    // Older payloads omit is_seed → default TRUE (fail-safe: never offer delete
    // on a doc we can't prove is custom; the server re-enforces anyway).
    isSeed: w.is_seed ?? true,
  };
}

/** Map bootstrap wire → view. DROPS `token` on purpose: a UI preview must never
 * surface an agent credential (the endpoint returns token=null for preview
 * requests, but we exclude it from the view model regardless). */
export function toBootstrap(w: WireBootstrap): BootstrapView {
  return {
    role: w.role,
    name: w.name,
    taskType: w.task_type,
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
    // The claude CLI probe columns (T-97ee): absent (older server) and null
    // (unknown — an older warden that never probed) both read as the honest
    // unknown; the UI shows only claudeVersion (table column, "—" on null).
    claudeVersion: w.claude_version ?? null,
    claudeCredSource: toClaudeCredSource(w.claude_cred_source),
    claudeSubReadable: w.claude_sub_readable ?? null,
  };
}

/** Narrow the wire `bin_status` to the closed BinStatus vocabulary; anything
 * absent/unrecognized reads as the honest unknown (null), never a verdict. */
function toBinStatus(v: string | null | undefined): BinStatus {
  return v === "current" || v === "stale" ? v : null;
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

/** Map the upgrade wire result → the view model (snake→camel). Pure rename;
 * `dispatched` passes through verbatim (whether the update RPC was enqueued). */
export function toUpgradeResult(w: WireUpgradeResult): UpgradeResultView {
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
    roleKey: w.role_key,
    taskType: w.task_type,
    text: w.text,
    isDefault: w.is_default,
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
