// api/adapter.ts — the typed api client seam (structural, not a class).
//
// `Api` is the ONE interface the UI programs against. Two implementations
// satisfy it: `mockApi` (mock.ts, wired in M1) and `httpApi` (http.ts, the real
// backend stub). The swap point is index.ts — the UI never imports mock/http.
//
// All methods return view-model shapes (`Member` / `ChatMessage`), never wire
// DTOs: the wire→view mapping is the adapter's job (see mappers.ts).

import type { ThemeBundle } from "../lib/themeBundle";
import type {
  Member,
  MemberLifecycle,
  MemberActivateResult,
  MemberRelocateResult,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  BackupHealthView,
  GlobalContextView,
  DocumentKind,
  DocumentHistoryView,
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
} from "../types";

/**
 * A chat message in view-model form. Mirrors what the composer/thread render;
 * derived from `WireChatMessage` by the adapter. `ts` stays an epoch number
 * (presentation formats it) so we never fabricate a display string here.
 */
export interface ChatMessage {
  id: string;
  from: string;
  to: string;
  body: string;
  ts: number;
  /** The message's attachments (0..N; files + images mixed). Empty array ⇒ the
   * message carries none. Honest passthrough of the wire `attachments` list —
   * never fabricated. The old singular `attachment*` / `imageUrl` fields were
   * REMOVED (beta — no compat shims, no dual-read path). */
  attachments: ChatAttachmentView[];
  /** The reply card riding this message (`meta.reply_card_id`, stamped by the
   * server when an agent opens a card — the ask IS a chat message). Non-null ⇒
   * the thread renders the message as an inline reply card (B3); null ⇒ a
   * plain message. Honest passthrough — never fabricated. */
  replyCardId: string | null;
  /** Read-time join of the carried card's CURRENT status (`reply_card_status`):
   * `"waiting"` | `"answered"` | `"expired"`, or null when the message carries no card. Lets
   * the inline ChatReplyCard decide AT MOUNT whether to load eagerly (waiting)
   * or lazily (answered — collapse, fetch only on expand) WITHOUT a per-card
   * GET. OPTIONAL so hand-built test fixtures stay valid (same precedent as
   * `ReplyCard.task`); the mapper always sets it (null when the wire carries
   * ""). */
  replyCardStatus?: "waiting" | "answered" | "expired" | null;
}

/** ONE attachment on a chat message, in view-model form. `url` is the served
 * GET path (`/api/chat/attachment/<id>`): the server serves an image inline and
 * a non-image as a download (Content-Disposition: attachment). */
export interface ChatAttachmentView {
  /** Opaque attachment blob id. */
  id: string;
  /** Optional backing chat attachment id when `id` is a UI entity key. */
  backingAttachmentId?: string;
  /** Served GET path for the blob (gated — render via authedAttachmentUrl). */
  url: string;
  /** Original upload filename (for the download-chip label); "" when none. */
  filename: string;
  /** Stored blob mime. */
  mime: string;
  /** true ⇒ render inline `<img>`; false ⇒ render a download chip. */
  isImage: boolean;
}

/** ONE flattened gallery row (`GET /api/chat/attachments?with=<member_id>`):
 * an attachment plus its message's sender identity + send time, spanning the
 * member's WHOLE perspective — owner↔member both directions AND the member's
 * inter-agent conversations. `fromName` is the server-resolved display name
 * ("" for the owner — the UI renders its own 「我」 label — and for an
 * unresolvable sender; never fabricated). */
export interface GalleryAttachment extends ChatAttachmentView {
  /** The carrying message id (stable row key). */
  messageId: string;
  /** Verified sender id ("owner" or a member id). */
  from: string;
  /** Server-resolved sender display name; "" ⇒ owner / unresolvable. */
  fromName: string;
  /** Addressee id. */
  to: string;
  /** Message send time (epoch seconds). */
  ts: number;
}

/**
 * A per-conversation read receipt in view-model form. Mirrors one
 * `domain.ChatReadReceipt`: `readerId` has read this conversation with `peerId`
 * up to `lastReadTs` (a monotonic last-read watermark). The chat UI reads these
 * to show a "read ✓" badge on the owner's own messages once the peer's watermark
 * covers them.
 */
export interface ChatReadReceipt {
  readerId: string;
  peerId: string;
  lastReadTs: number;
}

/** The scrollback keyset cursor (T-bf82): the (ts, id) of the OLDEST message
 * the caller already holds — `listChat` with this returns the page strictly
 * OLDER than that point. Composite on purpose: `ts` (epoch REAL) can collide,
 * so the message id tie-breaks; messages are immutable, so a cursor stays
 * valid forever. */
export interface ChatCursor {
  beforeTs: number;
  beforeId: string;
}

/** A staged attachment carried on a posted chat message (a pasted image OR an
 * uploaded file). `dataB64` is a data-URI (`data:<mime>;base64,…`) OR bare
 * base64 — the server accepts either. `filename` / `mime` are optional (the
 * server sniffs/defaults an omitted mime and defaults a pasted image's name). */
export interface ChatAttachmentInput {
  dataB64: string;
  filename?: string;
  mime?: string;
}

/** Browser-owned Web Push endpoint. These are encryption keys from the
 * PushSubscription API, never owner credentials. */
export interface PushSubscriptionInput {
  endpoint: string;
  expirationTime: number | null;
  keys: { p256dh: string; auth: string };
}

/** The stored answer on an ANSWERED reply card, in view-model form.
 * `optionIdx` is null for a pure free-text answer (index into the card's
 * `options` otherwise); `attachments` are served refs into the shared
 * chat-attachment store (render like chat attachments). */
export interface ReplyCardAnswer {
  optionIdx: number | null;
  text: string;
  attachments: ChatAttachmentView[];
}

/**
 * One reply card (等我回覆卡) in view-model form. `status` is the closed set
 * `waiting` | `answered` | `expired` — the only transitions are
 * waiting→answered via an answer (the owner's positive close; no generic
 * close/skip surface anywhere) and waiting→expired via the owner/admin-agent expire
 * action (標為過期 — NOT an answer; terminal, no reopen); a revised answer
 * (重新決定) keeps `answered`. `options[0]` is ALWAYS the AI's own
 * recommendation. `chatMessageId` links the chat message the card rides in —
 * the jump-to-origin anchor (B3 uses it to locate + highlight the message in
 * the member's chat; B2 only navigates to the chat room). `answeredTs`/
 * `answer` are null unless answered; `expiredTs` is null unless expired.
 */
export interface ReplyCard {
  id: string;
  /** The initiating member id (verified JWT sub at create time). */
  from: string;
  /** "decision" (needs the owner's call) | "action" (needs the owner to act). */
  kind: string;
  summary: string;
  body: string;
  options: string[];
  status: "waiting" | "answered" | "expired";
  /** QUESTION-side attachments the initiator opened the card with (T-5e8a) —
   * served refs into the shared chat-attachment store, rendered like chat
   * attachments on every card face regardless of status. Always an array
   * ([] when none), same posture as `answer.attachments`. */
  attachments: ChatAttachmentView[];
  createdTs: number;
  answeredTs: number | null;
  /** Epoch seconds of the owner's expire action; null unless expired.
   * OPTIONAL so hand-built test fixtures stay valid (same precedent as
   * `task`); the mapper always sets it. */
  expiredTs?: number | null;
  chatMessageId: string;
  answer: ReplyCardAnswer | null;
  /** The task this ask was armed from (a task gate, wire `task` — SPEC §3.6
   * 請示 → 任務跳轉): the jump anchor id + the type for the 精簡任務資訊 row.
   * Absent/null ⇒ a pure chat ask (no task info, no jump). OPTIONAL so
   * hand-built test fixtures stay valid (same precedent as Member.roleName);
   * the mapper always sets it (null when the wire carries none). */
  task?: TaskRefView | null;
}

/** The LIGHT task reference riding a task-armed reply card (wire TaskRefDTO).
 * Deliberately narrow: `id` is ONLY the #tasks jump anchor; the UI shows the
 * TYPE (typeKey; "" ⇒ 自由代辦) — never the task number/識別鍵 (adjudicated:
 * 請示卡不露任務編號). `title` rides along for accessibility labels. */
export interface TaskRefView {
  id: string;
  typeKey: string;
  title: string;
}

/** The owner's answer to a reply card: a quick-reply `optionIdx` and/or free
 * `text`, plus optional staged `attachments` (same input shape + limits as
 * chat attachments). At least one of the three must be present — the server
 * rejects an empty answer (400). */
export interface ReplyCardAnswerInput {
  optionIdx?: number;
  text?: string;
  attachments?: ChatAttachmentInput[];
}

/** Mirrors `ReplyCardCountDTO`. `waiting` drives the nav badge; `answered` +
 * `expired` are the recently-answered / recently-expired (24h) counts the
 * 等我回覆 page sums to render its collapsed 近期已處理 header without
 * fetching the lists. */
export interface ReplyCardCounts {
  waiting: number;
  answered: number;
  expired: number;
}

// ── Tasks (M3 任務卡) view models ─────────────────────────────────────────────

/** One workflow node of a task timeline, in view-model form. `status` is the
 * closed set `pending` | `in_progress` | `waiting_owner` | `waiting_external` |
 * `done` | `superseded`. Gate
 * projection (spec §核心名詞): `isGate` with an EMPTY `replyCardId` is the
 * ANNOUNCED gate (dashed 等我回覆 — the owner sees ahead of time where a reply
 * will be needed); a NON-EMPTY `replyCardId` is the ARMED gate carrying a live
 * M2 reply card. `startedTs`/`finishedTs` are 0 until real (never fabricated —
 * the UI derives 耗時 only from non-zero stamps). */
export interface TaskStepView {
  id: string;
  name: string;
  /** Definition of Done — the node's acceptance criterion. */
  dod: string;
  status: string;
  isGate: boolean;
  /** "" ⇒ announced gate / not a gate; non-empty ⇒ the bound M2 reply card. */
  replyCardId: string;
  /** Read-time join of the bound card's CURRENT status (`reply_card_status`):
   * `"waiting"` | `"answered"` | `"expired"`, or null when the step carries no card. Lets the
   * task-embedded TaskReplyCard decide AT MOUNT whether to load eagerly
   * (waiting) or lazily (answered — collapsed one-line summary, fetch on
   * expand) WITHOUT a per-card GET. OPTIONAL so hand-built test fixtures stay
   * valid; the mapper always sets it. */
  replyCardStatus?: "waiting" | "answered" | "expired" | null;
  /** One-line reason the step is parked in `waiting_external`; "" otherwise
   * (T-9ca5). The task's display `waitingReason` mirrors the highest-priority
   * waiting step's reason. OPTIONAL so hand-built fixtures stay valid (the
   * replyCardStatus precedent); the mapper always sets it. */
  waitingReason?: string;
  /** The step's free-text working note — what it got to and what comes next
   * (T-cc3e). The field the handover SOP means by "把還在進行中的工作寫回 task
   * step note"; unlike `waitingReason` it is bound to no status, so it carries
   * progress at whatever moment a handover lands. OPTIONAL so hand-built
   * fixtures stay valid (the replyCardStatus precedent); the mapper always
   * sets it. */
  note?: string;
  /** Non-empty ⇒ this leaf runs inside a parallel stage (同時進行 · N 項並行);
   * consecutive steps sharing the group render as one parallel block. */
  parallelGroup: string;
  orderIdx: number;
  startedTs: number;
  finishedTs: number;
}

/**
 * One task (M3 任務卡) in view-model form. `status` is the SERVER-DERIVED closed
 * set (`not_started` | `in_progress` | `waiting_owner` | `waiting_external` |
 * `done` | `terminated` | `duplicated`; the last three terminal) — `reassigning`
 * is NOT a status any more (it moved to the orthogonal `lock`, T-9ca5);
 * `priority` is `high` | `mid` |
 * `low` | `frozen` (凍結 is a priority, NOT a status). `executorKind`
 * "outsource" with an EMPTY `executorId` is the transient unassigned state
 * (只有外包任務會經歷). `progressDone`/`progressTotal` are the SERVER-computed
 * leaf counts — the UI never recomputes progress from steps. `deps` are
 * blocking task IDS (display markers only — a blocked task stays in_progress).
 */
/** One entry of {@link TaskView.depTasks} — a blocking task resolved to what the
 * dep row shows (wire `TaskDepRefDTO`, T-a3e4). `taskNo` is filled even for a
 * dep whose task is gone (it is a pure projection of the id); `title`/`status`
 * are "" in exactly that case, and the row then says 查無此任務 rather than
 * inventing a status. */
export interface TaskDepRefView {
  id: string;
  taskNo: string;
  title: string;
  status: string;
}

/** `GET /api/tasks/count`: the nav badge's open count + the unfiltered total
 * (T-a3e4 — what makes 目前沒有任務 an honest claim under a filtered list). */
export interface TaskCountView {
  open: number;
  total: number;
}

export interface TaskView {
  id: string;
  /** Display number (e.g. "T-7d40") — presentation only, never a lookup key. */
  taskNo: string;
  title: string;
  /** The task type / playbook key; "" ⇒ 自由代辦 (ad-hoc). */
  typeKey: string;
  description: string;
  status: string;
  /** ORTHOGONAL handover lock (T-9ca5): "" | "reassigning". A reassigned task
   * keeps its honest DERIVED `status` (e.g. in_progress) and carries
   * `lock="reassigning"` until the new executor claims it — `reassigning` is no
   * longer a `status` value. The cockpit's 轉派中 indicator keys off THIS, not
   * status. Optional-additive: the mappers always set it, but it post-dates many
   * fixtures, so it stays optional (read as `?? ""`). */
  lock?: string;
  priority: string;
  /** "member" | "outsource". */
  executorKind: string;
  /** Member id / outsource worker id; "" under kind=outsource ⇒ unassigned. */
  executorId: string;
  /** The verified token sub of whoever created the task: a member id, an
   * outsource worker id, or the literal "owner". "" on tasks created before
   * the server stored a creator (老任務) — the card renders that as "—". */
  creatorId: string;
  /** The PREDECESSOR the task was last handed over from (T-ba04 轉派交接): a
   * member id or an outsource worker id. "" when the task was never reassigned
   * — the card then shows no 前任 row. Optional-additive (T-ba04): the mappers
   * always set it, but the field post-dates many test fixtures, so it stays
   * optional to avoid a churn of unrelated fixture edits (read as `?? ""`). */
  reassignedFrom?: string;
  /** The kind of {@link reassignedFrom} ("member" | "outsource"), so the card
   * resolves the id the right way (roster name vs outsource codename). "" when
   * reassignedFrom is "". Optional-additive — see {@link reassignedFrom}. */
  reassignedFromKind?: string;
  /** The manual-derived identity key value (dedupe key); "" for ad-hoc. When
   * the value is a URL the badge renders an external link (spec 識別鍵). */
  dedupeKey: string;
  /** Blocking task IDS (被 T-xxx 擋住, 可多筆) — resolved to task_no for display. */
  deps: string[];
  /** The SERVER's resolution of every id in {@link deps} (wire `dep_tasks`,
   * T-a3e4): one entry per dep, same order, carrying what the 「等 T-xxxx
   * <標題>」 row prints. The card renders straight from this — it no longer
   * looks deps up in the loaded task list, which is why the page no longer has
   * to download the closed population just so a finished blocker can be named.
   *
   * 🔴 THREE states, and they are not interchangeable: an entry with a
   * `status` is a resolved dep; an entry whose `status` is "" is a dep whose
   * task is GONE (查無此任務); the whole field being `undefined` means the
   * SERVER did not resolve deps at all (a pre-T-a3e4 server, or a hand-built
   * fixture) — that is "cannot name it yet", NOT "does not exist". This is the
   * same honesty split `closedLoaded` used to carry, moved to where the fact
   * actually lives. Optional-additive: the mappers pass the wire field through
   * verbatim, absent stays absent. */
  depTasks?: TaskDepRefView[];
  /** One-line reason while status is waiting_external; "" otherwise. */
  waitingReason: string;
  /** The ORIGINAL task's id this one duplicates; "" unless status is
   * "duplicated". The card renders "重複於 T-xxxx" as a link that jumps to it —
   * depth-1 by construction, so the link always resolves in one hop. */
  duplicateOf: string;
  createdTs: number;
  updatedTs: number;
  /** Epoch when the task closed (done/terminated/duplicated); null while open. */
  closedTs: number | null;
  progressDone: number;
  progressTotal: number;
  steps: TaskStepView[];
  /** The task's curated deliverable set (T-3dc5), oldest→newest. Only the FULL
   * task (getTask) carries these; the LIGHT list leaves it [] (it carries only
   * `artifactCount` for the collapsed card's 「產物 N」 badge). The popover
   * hydrates the full task to render them. OPTIONAL so hand-built test fixtures
   * stay valid (the replyCardStatus precedent); the mapper always sets it. */
  artifacts?: TaskArtifactView[];
  /** Number of pinned deliverables — the collapsed card's 「產物 N」 badge (0 ⇒
   * badge hidden). On the light list it is the SERVER count (`artifact_count`);
   * on a hydrated full task it equals `artifacts.length` (kept consistent so a
   * post-hydrate card keeps the same badge). OPTIONAL so hand-built fixtures
   * stay valid; the mapper always sets it. */
  artifactCount?: number;
}

/** ONE pinned deliverable on a task's artifact set (T-3dc5), in view-model
 * form. `kind` is the closed set file|image|link. For file/image, `url` is the
 * blob serve path and `mime`/`filename`/`isImage` echo the shared
 * chat-attachment blob (render it exactly like a chat attachment). For link,
 * `url` is a bare external URL (a PR link), `attachmentId`/`mime`/`filename`
 * empty and `isImage` false. `label` is the display name (a link's title, or a
 * filename override). Honest passthrough — never fabricated. */
export interface TaskArtifactView {
  id: string;
  kind: "file" | "image" | "link";
  url: string;
  label: string;
  filename: string;
  mime: string;
  isImage: boolean;
  attachmentId: string;
  createdTs: number;
  createdBy: string;
}

/** One LIVE outsource worker (anonymous, task-bound). The tasks page resolves
 * an outsource task's 「代號 · 模型 · 投入度」 through this list; a released
 * worker drops off (closed tasks honestly fall back to the bare 外包 label). */
export interface OutsourceWorkerView {
  id: string;
  /** Personal image URL bound to this stable worker id. */
  avatarUrl?: string;
  /** Model-flavoured anonymous codename (O-7 / S-3 / H-1 …). */
  codename: string;
  /** The owner-CONFIGURED launch trio — what the settings dialog round-trips. */
  runtime?: "claude" | "codex";
  model: string;
  effort: string;
  /** Their REPORTED twins (wire `actual_*`): what the worker's own session
   * says it is running. "" = never reported. Never a fallback to the
   * configured value beside it (T-7f28). */
  actualRuntime?: "claude" | "codex" | "";
  actualModel?: string;
  actualEffort?: string;
  /** The DURABLE last-observed machine — survives the worker going offline,
   * which `machine` (the in-memory dispatch target) does not. */
  actualMachine?: string;
  /** Worker lifecycle status (assigned → active → released). OPTIONAL so
   * hand-built fixtures stay valid (taskTitle precedent); the mapper always
   * sets it (honest "" when absent). */
  status?: string;
  taskId: string;
  /** The bound task's title/status echoed on the wire (SPEC §4.1: the panel
   * row is 代號 · 任務狀態 + 任務標題 without joining the task list).
   * OPTIONAL so hand-built test fixtures stay valid (Member.roleName
   * precedent); the mapper always sets them (honest "" when absent). */
  taskTitle?: string;
  taskStatus?: string;
  /** Worker mint epoch (wire created_ts; 0 when absent) — the panel's
   * fallback sort key when the bound task cannot be resolved. */
  createdTs?: number;
  /** The bound task's display number (T-xxxx) and type — the panel row is 名稱 /
   * task type + presence 點 / 可點的 T-xxxx (owner report 2026-07-14, aligned
   * with the member card's three-line shape). WIRE FIELDS since T-a3e4
   * (`task_no` / `task_type_key`): they used to be a CLIENT-side join against
   * the unfiltered `GET /api/tasks`, i.e. the whole task history downloaded on
   * every worker/chat delta to label a handful of rows. Honest "" when the
   * bound task cannot be resolved. */
  taskNo?: string;
  taskTypeKey?: string;
  /** The bound task type's DISPLAY name (T-fa76), wire `task_type_name` since
   * T-a3e4 (was a client-side join against the manuals list); honest "" when
   * the manual is gone or names nothing — the row then falls back to the raw
   * taskTypeKey. */
  taskTypeName?: string;
  /** The bound task's createdTs (wire `task_created_ts`, T-a3e4) — the panel's
   * sort key (依任務建立時間新→舊). 0/absent ⇒ the row falls back to the
   * worker's own {@link createdTs} mint stamp, an honest proxy. */
  taskCreatedTs?: number;
  /** The owner's unread chat count for this worker's conversation (wire
   * `unread_count`, the same watermark inverse the member roster serves) —
   * the row's red badge (owner report 2026-07-14: 外包也要有未讀紅點).
   * OPTIONAL so hand-built fixtures stay valid; the mapper always sets it. */
  unreadCount?: number;

  // ── T-f190: the detail-panel alignment fields (外包詳情頁對齊成員詳情) ──────
  /** REAL-liveness projection on the ONE member presence vocabulary (wire
   * `presence`, A案 P6 — replaces the retired `spawn_state`). Typed as the
   * SAME five-state union the member roster carries (`MemberLifecycle`), NOT a
   * bare string (T-59d6): the union is what makes `tsc` reject a typo'd or
   * unhandled state on every surface that paints presence. `undefined` = no
   * presence to project (released worker / older server that defaulted the
   * field away) — `presenceVisual` resolves that to the offline dot, never a
   * fabricated live green. The mapper narrows the wire string at one shared
   * seam (`toPresence`, used by the member path too); unrecognised words become
   * `undefined`, so a rail row and a detail panel can no longer disagree about
   * an unknown value. */
  presence?: MemberLifecycle;
  /** The machine the worker's session was ACTUALLY dispatched to (wire
   * `machine` — last_spawn_target resolved to its display name), NOT the
   * manual's preference. "" when never dispatched → the panel shows 「尚未分配」,
   * never a fabricated machine name. */
  machine?: string;
  /** The OWNER-PINNED placement (wire `desired_machine_id`; the relocate
   * target the picker binds): "" = unpinned, else a concrete machine id. */
  desiredMachineId?: string;
  /** Claude account / live context % / live cost — RUNTIME facts folded from
   * the SAME per-actor telemetry+gauge the member roster reads. null when the
   * worker has not reported one → the panel shows a bare dash, never a
   * fabricated value (parity with the member detail's honest gate). */
  account?: string | null;
  contextPct?: number | null;
  compactionCount?: number | null;
  cost?: number | null;
  /** The durable cumulative spend banked on every session end / kill+respawn
   * (wire `banked_cost`, migrations/00021 — member.bankedCost parity). null =
   * nothing banked yet; the panel shows live + banked summed. (T-ba6b) */
  bankedCost?: number | null;
  /** The last folded warden command receipt (worker twin of member.lastOp*),
   * surfaced as the panel's 最近操作 block. lastOpOk is three-valued (null = no
   * receipt yet); lastOpAt is null when 0 (no op → the block hides). */
  lastOp?: string;
  lastOpOk?: boolean | null;
  lastOpLog?: string;
  lastOpReason?: string;
  lastOpAt?: number | null;
  /** The RAW verified sub of the bound task's creator (a member id, the literal
   * "owner", or "" on pre-column / server-scheduled rows). Together with
   * delegatedBy this lets the panel honestly distinguish owner vs member vs
   * unassigned, replacing the former hardcoded "System owner" placeholder. */
  creatorId?: string;
  /** The RESOLVED member display name behind the task's creator (wire
   * `delegated_by`), or "" when the creator is the owner / unknown / a
   * server-scheduled row — the panel then renders the owner label or an honest
   * fallback, NEVER a fabricated name. */
  delegatedBy?: string;
  /** Epoch seconds of the in-flight context-handover stamp (wire
   * `refocus_since`, T-32e1), or null when none — a set value drives the
   * panel's 換手中 acknowledgement (parity with the member panel's refocusSince).
   * The mapper converts the wire 0 → null so the panel never shows a fake time. */
  refocusSince?: number | null;
  /** Which operation opened that window ("" when none), and the epoch it is
   * collected by at the latest (null when none) — the panel says "winding
   * down so your change can take effect" instead of "last handover". */
  refocusOp?: string;
  refocusDeadline?: number | null;
  /** Run-intent, a direct mirror of member.desiredState (wire `desired_state`,
   * T-f190): "online" (system wants it running) or "offline" (owner-explicit
   * stop — presence is then "stopping"/"stopped"). Drives the 停止/喚醒 arm of the
   * worker panel's identity action row. "" from a
   * pre-column row reads as online. */
  desiredState?: string;
}

/** One task type (任務手冊) in the LIGHT list shape the tasks page needs for
 * its type filter (所有 ／ 各手冊類型 ／ 自由代辦). The full manual editor
 * (設定 › 任務手冊) reads the FULL `TaskManualView` instead. */
export interface TaskTypeView {
  typeKey: string;
  displayName: string;
  purpose: string;
}

// ── Task manuals (設定 › 任務手冊, SPEC §5) view models ───────────────────────

/** One input field of a task manual (Q2 需要哪些資訊): name, 必填/選填, and
 * whether it is (part of) the 識別鍵 (isKey fields — possibly SEVERAL — form
 * the type's composite dedupe identity key). */
export interface TaskManualFieldView {
  name: string;
  required: boolean;
  isKey: boolean;
}

/** The type's 負責成員 setting — who executes tasks of this type. `null` ⇒
 * unset (wire `{}`). Outsource carries the server-side launch knobs: copies
 * (per-type parallel copies, H6; **0 = 無限** — unlimited, spec
 * TaskManualDTO) and machine (the machine this type's workers boot on — a
 * machine id, or `""` for "none chosen". Nothing is substituted: while no
 * machine is chosen, or the chosen one is offline, no worker of the type is
 * started and the reason is recorded on the worker row). */
export type ManualAssigneeView =
  | { kind: "member"; memberId: string }
  | {
      kind: "outsource";
      runtime?: "claude" | "codex";
      model: string;
      effort: string;
      copies: number;
      machine: string;
    }
  | null;

/** One FULL task manual (任務手冊 — a task type / playbook): the guided
 * definition (Q1 purpose / Q2 fields / Q3 SOP markdown), the accumulated
 * 學習經驗, and the 負責成員 assignee setting. NO internal filename anywhere —
 * manuals are presented as content, not files (spec §5.2 note). */
export interface TaskManualView {
  typeKey: string;
  /** Owner/agent-editable label; empty ⇒ the UI falls back to typeKey. */
  displayName: string;
  /** Q1 這是什麼任務 — the intake window's type-matching criterion. */
  purpose: string;
  /** Q2 需要哪些資訊 — the input-field list. */
  fields: TaskManualFieldView[];
  /** Q3 該怎麼做 — the SOP markdown the AI plans the workflow from. */
  sopMd: string;
  /** 學習經驗 — agent write-back on task close; owner-editable too. */
  learnings: string;
  assignee: ManualAssigneeView;
  updatedTs: number;
}

/** One product-guide doc row (使用說明 tab landing): the addressable slug +
 * its display title. The full body is fetched per-slug via getDoc. */
export interface DocSummaryView {
  slug: string;
  title: string;
}

/** One product-guide doc in full: the markdown the 使用說明 doc page renders
 * (relative image paths already rewritten to the served /api/docs/assets/
 * endpoint by the server). */
export interface DocView {
  slug: string;
  title: string;
  markdownMd: string;
}

/** Partial manual edit (only supplied fields change — mirrors
 * TaskManualUpdateDTO). `assignee: null` explicitly UNSETS it (wire `{}`);
 * an omitted `assignee` leaves it untouched. */
export interface TaskManualPatch {
  displayName?: string;
  purpose?: string;
  fields?: TaskManualFieldView[];
  sopMd?: string;
  learnings?: string;
  assignee?: ManualAssigneeView;
}

/** The owner's task-card message to the executor (POST /api/tasks/{id}/message):
 * text and/or attachments (the same input shape as chat — the card's message
 * box stages them via the shared useAttachmentStaging machine). */
export interface TaskMessageInput {
  body: string;
  attachments?: ChatAttachmentInput[];
}

/** The reassign target (`POST /api/tasks/{id}/reassign`): either an ACTIVE
 * roster member below the warden layer, or a FRESH outsource worker the server
 * mints on the spot from these knobs (blank fields are INHERITED server-side:
 * from the type manual for a typed task, else from the dispatching member
 * itself. A blank `machine` that resolves to nothing means no worker starts —
 * an offline machine is never substituted). The manual's per-type `copies` has
 * no analogue here: a reassign
 * mints exactly ONE worker for THIS task. */
export type TaskReassignTarget =
  | { kind: "member"; memberId: string }
  | {
      kind: "outsource";
      runtime?: "claude" | "codex";
      model: string;
      effort: string;
      machine: string;
    };

/** One reassign (轉派): the new executor + an optional handover note the server
 * appends to the new executor's notification chat message. The task enters
 * `reassigning` and the NEW executor reports it back to in_progress once the
 * handover is read — the FE never flips the status itself. */
export interface TaskReassignInput {
  target: TaskReassignTarget;
  note?: string;
}

/** The owner-adjustable server settings in view-model form (`/api/settings`). */
export interface ServerSettingsView {
  /** Owner-login lifetime in seconds (one of 43200/86400/604800/2592000). */
  tokenTtl: number;
  /** Context auto-handover threshold in percent (40..90). */
  handoverPct: number;
  /** Codex context compactions before automatic refocus (1..10). */
  codexCompactionThreshold: number;
  /** Minimum seconds between telemetry-triggered monitoring refreshes (1..60). */
  monitoringRefreshSeconds: number;
  /** M3: the GLOBAL cap on concurrently live outsource workers (-1..20;
   * **-1 ⇒ 無限 (unlimited — no global cap)**; 0 ⇒ outsource assignment is
   * PAUSED — the panel annotates it). */
  outsourceMaxParallel: number;
  /** T-3aeb: the size cap on the accumulating context documents (lessons,
   * task-manual learnings + sop_md), in CHARACTERS (runes). Default 10000;
   * adjustable 10000..100000 — the floor is the default, so it only goes up. */
  docCapChars: number;
  /** Whether the GitHub-release update check also admits prereleases
   * (false = official releases only, the default). */
  updaterReceiveBeta: boolean;
  /** Whether the server self-upgrades in the background when GitHub has a
   * newer admissible release (false = manual-only, the default). */
  updaterAutoUpdate: boolean;
  /** The studio display name shown in the topbar (T-d693). "" = never set —
   * the caller falls back to the localized default (`t.orgName`). */
  orgName: string;
  /** The owner's display nickname shown in the topbar profile pill (T-0b41).
   * "" = never set — the caller falls back to the localized default (`t.user`). */
  ownerName: string;
  /** Contact email used as this deployment's Web Push VAPID identity. Empty
   * means delivery is disabled until the owner configures a public address. */
  pushContactEmail: string;
  /** The owner's cockpit visual theme (T-0b41-p2). "" = never set — the
   * frontend keeps its localStorage cache / default. Server = cross-device
   * truth, reconciled in at login (see i18n/index.tsx). */
  displayTheme: string;
  /** The owner's cockpit language (T-0b41-p2). "" = never set — same
   * dual-layer contract as displayTheme. */
  displayLanguage: string;
  /** Whether the cockpit uses the WIDE layout (T-756f): the centred ~1040px
   * content column is lifted, the side gutters stay. false = the narrow
   * centred column, the shipped default. Same dual-layer contract as
   * displayTheme, but a plain bool — there is no "never set" third state. */
  displayWide: boolean;
  /** The owner's saved custom theme bundles (T-16a1 P2); [] = none saved.
   * displayTheme may point at any bundle's id (or a built-in). */
  customThemes: ThemeBundle[];
  /** The automatic first-run onboarding report (T-ba62), or null when
   * onboarding never ran on this server (an install predating it, or a
   * database that already had a password). This is how the cockpit can say
   * WHY the assistant is not awake instead of showing an unexplained grey
   * member. Owner-gated: it rides `/api/settings`, never the public
   * first-run probe, because a failed step's detail carries local paths. */
  onboarding: OnboardingReportView | null;
}

/** One step of the automatic first-run onboarding (T-ba62). `name` is a stable
 * machine key (`install_warden` / `wake_assistant`); `reason` is ALWAYS
 * populated on a failure. `detail` is the raw tool log of a failed step. */
export interface OnboardingStepView {
  name: string;
  ok: boolean;
  reason: string;
  detail: string;
}

/** The first-run onboarding result (T-ba62). `state` is
 * `running` | `ok` | `failed`. */
export interface OnboardingReportView {
  state: string;
  startedAt: number;
  finishedAt: number;
  steps: OnboardingStepView[];
}

/** Partial settings edit — only supplied fields change (server 422s a
 * token_ttl outside its whitelist / a handover_pct outside 40..90 / an
 * outsource_max_parallel outside -1..20; -1 = 無限 unlimited). */
export interface ServerSettingsPatch {
  tokenTtl?: number;
  handoverPct?: number;
  codexCompactionThreshold?: number;
  monitoringRefreshSeconds?: number;
  outsourceMaxParallel?: number;
  /** T-3aeb document size cap, in characters. Must be 10000..100000. */
  docCapChars?: number;
  /** Also admit GitHub prereleases in update checks (default false). */
  updaterReceiveBeta?: boolean;
  /** Arm unattended background self-upgrade (default false = manual-only). */
  updaterAutoUpdate?: boolean;
  /** The studio display name (T-d693); trimmed server-side, max 80 runes, ""
   * clears it back to the localized default (server 422s anything longer). */
  orgName?: string;
  /** The owner's display nickname (T-0b41); trimmed server-side, max 80 runes,
   * "" clears it back to the localized default (server 422s anything longer). */
  ownerName?: string;
  /** Web Push VAPID contact email; empty clears it and disables delivery. */
  pushContactEmail?: string;
  /** The owner's cockpit visual theme (T-0b41-p2); "" (unset) | "office" (the
   * built-in) | an existing custom theme id. The server 422s anything else. */
  displayTheme?: string;
  /** The owner's cockpit language (T-0b41-p2); "zh" | "en" (or "" to clear).
   * The server 422s anything else. */
  displayLanguage?: string;
  /** Turn the WIDE cockpit layout on/off (T-756f). Omit to leave it
   * unchanged — a plain bool, so there is nothing to "clear" it to. */
  displayWide?: boolean;
  /** Replace the owner's custom theme bundles (T-16a1 P2). Omit to leave them
   * unchanged; [] clears them. Each bundle is validated (shape + theme.css
   * token whitelist + concrete-colour grammar); the server 422s any violation.
   * Deleting the active custom theme resets displayTheme to "". */
  customThemes?: ThemeBundle[];
}

/** Fields the owner may edit on a member (PATCH; every field optional).
 * `model` is a FREE string (spawn --model is a free string; "" ⇒ CLI default);
 * `effort` is the closed low/medium/high vocabulary (server 422s outside it).
 * Both are LAUNCH INTENTS — a change takes effect on the member's NEXT wake /
 * handover (the reconcile START payload bakes them into the launch command). */
export interface MemberPatch {
  name?: string;
  runtime?: "claude" | "codex";
  model?: string;
  effort?: string;
}

/** Fields the owner may edit on a monitoring alias (machine / account display
 * label). PATCH body carries `display_name`; the view model uses `displayName`.
 * The BE returns a narrow AliasDTO `{id, display_name, owner_id}` (NOT the whole
 * monitoring row) — so these adapter methods return `void`: the caller refetches
 * monitoring to pick up the new label, never merges a PATCH return into one row. */
export interface AliasPatch {
  displayName?: string;
}

/** Optional knobs for onboarding a machine (`onboardMachine`). `ttlDays`
 * overrides the governance token lifetime (maps to the snake_case wire body
 * `ttl_days`); an omitted field is left off the body so the server applies its
 * own default. The display name is now a REQUIRED positional argument (the
 * machine is created by display name only — there is no `host` anymore). */
export interface OnboardOptions {
  ttlDays?: number;
}

/** Fields the owner may edit on a role definition (PATCH; every field optional).
 * Mirrors `RoleDefUpdateDTO {name?, definition_md?}`. */
export interface RolePatch {
  name?: string;
  definitionMd?: string;
}

/** Create ONE custom role + its ONE founding member (M2-2 角色誌新增; mirrors
 * `RoleCreateDTO`). `name` = the role title; `memberName` is OPTIONAL — omitted
 * (the create flow's default) ⇒ the SERVER picks a fresh Mira-style name from
 * its pool, never colliding with an existing roster member; `model` free string
 * / `effort` low|medium|high are the member's launch knobs (omitted ⇒ server
 * defaults: "" / "medium"). */
export interface RoleCreateInput {
  name: string;
  memberName?: string;
  runtime?: "claude" | "codex";
  model?: string;
  effort?: string;
}

/** The created pair (mirrors `RoleCreateResultDTO`): the folded custom role doc
 * (isSeed=false, template definitionMd) + the founding member (initially
 * OFFLINE — creating never spawns; it surfaces on the roster immediately). */
export interface RoleCreateResult {
  role: RoleDefView;
  member: Member;
}

/** One webhook endpoint bound to a member (M4 回呼端點, view model of
 * `WebhookEndpointDTO`). `token` is the opaque secret the panel composes the
 * callback URL from — it arrives on this owner-facing wire only; the UI masks
 * it visually while copy yields the full URL. `endpointId` is immutable after
 * creation; `purpose` is editable; `status` is the enabled/disabled toggle. */
export interface WebhookEndpoint {
  endpointId: string;
  purpose: string;
  status: "enabled" | "disabled";
  createdTs: number;
  token: string;
  /** Verification preset fixed at creation (M4 §2). `generic` = URL-token only;
   * `slack`/`github` add the platform's challenge/HMAC verification. */
  platform: "generic" | "slack" | "github";
  /** Whether a signing secret is configured. The secret itself is NEVER echoed
   * on any wire — only this boolean (stricter than `token`). */
  hasSigningSecret: boolean;
  /** Epoch seconds of the last `/in` call that resolved to this token
   * (delivered or dropped alike); 0 = never received. */
  lastReceivedTs: number;
  /** Verified payloads delivered to the member as a chat. */
  deliveredCount: number;
  /** Silently discarded calls (failed signature / disabled / member gone).
   * Unknown-token calls have no endpoint to count against. */
  droppedCount: number;
  /** Coarse reason of the latest drop (`sig_failed` | `disabled` |
   * `member_gone`); "" = never dropped. */
  lastDropReason: string;
}

/** The verification presets a webhook endpoint may bind to (M4 §2). */
export type WebhookPlatform = "generic" | "slack" | "github";

/** One row of a webhook endpoint's /in debug ring buffer (view model of
 * `WebhookRequestLogDTO`; last 5 raw requests, newest first). `outcome` is
 * the closed classification: `delivered` | `dropped:sig_failed` |
 * `dropped:disabled` | `dropped:member_gone` | `challenge` | `ping`.
 * `headers` is the JSON-serialised request header map (≤4 KiB), `body` the
 * raw payload text (≤16 KiB); `truncated` marks that either was cut. */
export interface WebhookRequestLog {
  ts: number;
  outcome: string;
  headers: string;
  body: string;
  truncated: boolean;
}

/** Create-form payload for a new webhook endpoint. `platform` picks the
 * verification preset (default `generic`); `signingSecret` is the write-only
 * shared secret REQUIRED for `slack`/`github`, ignored for `generic` — it is
 * never echoed back on any wire. */
export interface WebhookCreateInput {
  endpointId: string;
  purpose?: string;
  platform?: WebhookPlatform;
  signingSecret?: string;
}

// ── Resume summary (RESUME SUMMARY panel section, T-8b0d) ─────────────────────

/** The size/概要 block of a resume snapshot (view model of
 * `ResumeOverviewDTO`) — the peek-then-decide counts/sizes: `chatCount`/
 * `chatChars` describe what THIS snapshot carries, `tasksOpenTotal` is ALL the
 * target's open tasks (may exceed the bounded `tasks` rows), `tasksDetailChars`
 * sums every returned row's `detailChars` (the plan text a full task pull would
 * load), `cardsWaiting`/`cardsAnsweredRecent` count the target's reply cards. */
export interface ResumeOverviewView {
  chatCount: number;
  chatChars: number;
  tasksReturned: number;
  tasksOpenTotal: number;
  tasksDetailChars: number;
  cardsWaiting: number;
  cardsAnsweredRecent: number;
}

/** One LIGHT open-task row inside a resume snapshot (view model of
 * `ResumeTaskDTO`) — NO steps/DoD text; `currentStepId`/`currentStepName` are
 * the first non-terminal step (both "" when the plan is empty or complete),
 * `detailChars` is the size of the plan text this row omits. */
export interface ResumeTaskView {
  id: string;
  taskNo: string;
  title: string;
  typeKey: string;
  status: string;
  priority: string;
  waitingReason: string;
  currentStepId: string;
  currentStepName: string;
  progressDone: number;
  progressTotal: number;
  updatedTs: number;
  detailChars: number;
}

/** The RESUME SUMMARY panel section's snapshot for a TARGET member — the SAME
 * bounded wake snapshot `resume_summary` returns for the caller (view model of
 * `ResumeSummaryDTO`, `GET /api/members/{member_id}/resume-summary`). */
export interface MemberResumeSummaryView {
  identity: string | null;
  chat: ChatMessage[];
  tasks: ResumeTaskView[];
  overview: ResumeOverviewView;
  note: string;
}

/** Partial edit of a webhook endpoint (status toggle, purpose, and/or a
 * signing-secret rotation). `platform` is immutable and cannot be changed here;
 * `signingSecret` (write-only) sets/rotates the secret. */
export interface WebhookUpdate {
  status?: "enabled" | "disabled";
  purpose?: string;
  signingSecret?: string;
}

/**
 * The IDENTITY-ONLY projection of an SSE delta's `payload` (spec/sse.md §2.2).
 *
 * The wire has always carried a payload; spec §2.2 forbids MERGING it, because
 * it is deliberately partial and lacks every server-derived DTO field. What it
 * does carry losslessly is WHICH entity the write touched, and that is a
 * different thing from the entity's values: naming an item lets a subscriber
 * refetch that one item instead of re-downloading its whole list, with the
 * server still the only source of the values that get rendered.
 *
 * So this type is restricted to the payload's IDENTITY fields by construction —
 * `last_read_ts`, `status`, `priority`, `codename` and friends are dropped at
 * the seam and cannot reach a hook, which makes "never merge a payload" a
 * property of the types rather than a rule to remember. Reading the wire's
 * existing fields is NOT a wire change: no frame shape, topic, or endpoint
 * moves (the freeze in root CLAUDE.md §13 stands).
 */
export interface SseDeltaNames {
  /** `member` / `task` / `reply_card` / `outsource_worker` / `chat` (message id). */
  id?: string;
  /** `chat`: the message's sender / recipient. */
  from?: string;
  to?: string;
  /** `chat_read`: WHOSE watermark advanced, and in which conversation. */
  reader?: string;
  peer?: string;
}

export interface SseDelta {
  topic: string;
  /** The identity fields the payload named — empty for a topic whose payload is
   * null (spec §2.2) and empty on a resync, which means "you may have missed
   * anything" and therefore names nothing. */
  names: SseDeltaNames;
  /** Every value in `names`, de-duplicated — the cheap "does this delta touch
   * something I am holding?" test. Empty ⇒ refetch the lot. */
  ids: string[];
}

/**
 * The typed api client. Structural type — any object with these methods is an
 * `Api`. Presence contract: `activateMember` writes desired_state=online INTENT only;
 * it never flips the member online (server presence drives that). The UI must
 * refetch after mutations rather than optimistically colouring the dot green.
 */
export interface Api {
  /**
   * The roster. `opts.light` (T-cf91) requests the server's identity-only
   * projection (`GET /api/members?fields=light`): only id / name / role are
   * meaningful — presence, machine, and unread_count come back HONEST-EMPTY
   * (the server skips the whole-chat unread scan the full view runs). Use it
   * ONLY from a surface that renders name + role and nothing else (the 請示卡頁),
   * paired with a hook that does NOT refetch on chat deltas.
   */
  listMembers(opts?: { light?: boolean }): Promise<Member[]>;
  getMember(id: string): Promise<Member>;
  /** Owner-only personal avatar mutation; raw PNG/JPEG/WEBP, max 64 KiB. */
  updateMemberAvatar(id: string, file: File): Promise<string>;
  /** Owner-only removal; returns the member to the theme/glyph fallback. */
  removeMemberAvatar(id: string): Promise<void>;
  /**
   * Write desired_state=online INTENT — and, when `machineId` is given, BIND the agent
   * to that machine (sent as `{machine_id}` in the activate body; the field was
   * renamed from the prior `host`). This is the spawn/wake path AND the permanent
   * "move agent" rebind — passing a new `machineId` sticks the agent to that
   * machine. Omitting `machineId` sends `{}` (no machine override → server
   * default). Does NOT flip online — no optimistic green; the caller refetches.
   *
   * 🔴 RETURNS {@link MemberActivateResult} — do NOT widen this back to
   * `Promise<void>` (T-7fa1). An activate always answers 200 because the intent
   * is persisted before dispatch, so the resolve/reject axis cannot tell the
   * caller whether a START actually reached a warden. `activationPending` is
   * the only signal that distinguishes them, and a `void` signature deletes it
   * at the type level: that is exactly how every wake surface ended up showing a
   * permanent 「喚醒中…」 for a wake that was never sent.
   */
  activateMember(id: string, machineId?: string): Promise<MemberActivateResult>;
  /**
   * Relocate a member to a machine (`POST /api/members/{id}/relocate` {machine_id}).
   * The owner cockpit's 改機器 for a roster member — PLACEMENT ONLY: it writes the
   * owner-pinned desired_machine_id and runs the server's event-driven reconcile
   * (a LIVE member auto-migrates onto the chosen machine; an offline member just
   * re-pins for the next wake), but it NEVER touches desired_state — unlike
   * `activateMember`, a relocate is not a wake. Does NOT flip online; the caller
   * refetches. Distinct from `activateMember(id, machineId)`, which is the
   * spawn/wake path (force-revive desired_state=online + machine bind).
   *
   * 🔴 RETURNS {@link MemberRelocateResult} for the same reason activateMember
   * does (T-7fa1): a relocate whose recycle STOP/START never reached a warden
   * answers a clean 200, and `relocation_pending` is the only thing that says
   * "scheduled, not landed".
   */
  relocateMember(id: string, machineId: string): Promise<MemberRelocateResult>;
  /**
   * List the machine registry (`GET /api/machines`). Each row carries the stable
   * `machineId` (activate/rebind + teardown target), the renamable `displayName`,
   * and `online` (warden reachability). The machine picker reads the ONLINE ones;
   * address by `machineId`, only ever DISPLAY `displayName`. Honest passthrough —
   * `online` is never fabricated.
   */
  listMachines(): Promise<MachineView[]>;
  /**
   * Graceful STOP (handover 層3): write desired_state=offline + stamp stopping_since,
   * RETAINING the roster row (status stays active — re-spawnable). Backs the
   * "Stop" (online) and "Cancel" (waking) actions. Does NOT flip online — the
   * warden tears the session down and presence reports stopping→stopped back;
   * the caller refetches (no optimistic state change).
   */
  deactivateMember(id: string): Promise<void>;
  /**
   * Force-stop (immediate kill): POST /api/members/{id}/force-stop → the server
   * dispatches the robust STOP straight to the warden NOW, bypassing the 120s
   * graceful-stop grace (the warden SIGKILLs the session). Backs the cockpit's
   * "Force stop" escalation, surfaced once a member is already *stopping*. Does
   * NOT flip online — the caller refetches; presence surfaces stopped.
   */
  forceStopMember(id: string): Promise<void>;
  /**
   * Dismiss (soft delete): DELETE the member → status=removed + desired_state=offline.
   * PURE SEAM, no UI entry — the 解散 button was removed from MemberDetailPanel
   * per owner acceptance; the backend route (and this client mirror) stays.
   */
  dismissMember(id: string): Promise<void>;
  patchMember(id: string, patch: MemberPatch): Promise<Member>;
  /** Refocus context (online-only server-side). */
  refocusMember(id: string): Promise<void>;
  /** List a member's webhook endpoints (`GET /api/members/{id}/webhooks`),
   * oldest→newest. Each row carries the opaque token for URL composition. */
  listWebhooks(memberId: string): Promise<WebhookEndpoint[]>;
  /** Create a webhook endpoint on a member (`POST /api/members/{id}/webhooks`).
   * The server mints the token and returns it on the endpoint. Rejects (throws)
   * on a blank/duplicate/invalid endpoint_id (409/422). */
  createWebhook(
    memberId: string,
    input: WebhookCreateInput,
  ): Promise<WebhookEndpoint>;
  /** Toggle status / edit purpose of a webhook endpoint
   * (`PATCH /api/members/{id}/webhooks/{endpointId}`). endpoint_id is immutable. */
  updateWebhook(
    memberId: string,
    endpointId: string,
    patch: WebhookUpdate,
  ): Promise<WebhookEndpoint>;
  /** Permanently revoke a webhook endpoint
   * (`DELETE /api/members/{id}/webhooks/{endpointId}`) — the token dies. */
  deleteWebhook(memberId: string, endpointId: string): Promise<void>;
  /** The endpoint's /in debug ring buffer, newest first (last 5 raw requests;
   * `GET /api/members/{id}/webhooks/{endpointId}/requests`, owner/admin-agent). */
  listWebhookRequests(
    memberId: string,
    endpointId: string,
  ): Promise<WebhookRequestLog[]>;
  /** The target member's bounded wake snapshot (RESUME SUMMARY panel section,
   * T-8b0d) — the SAME `resumeSnapshotParts` assembly `resume_summary` uses for
   * the caller, here for `memberId`
   * (`GET /api/members/{member_id}/resume-summary`, owner/admin-agent only —
   * an ordinary agent token → 403). LAZY by contract: the panel calls this
   * only when its RESUME SUMMARY section is expanded, never on panel mount. */
  getMemberResumeSummary(memberId: string): Promise<MemberResumeSummaryView>;
  /** List the conversation with `withId`, oldest→newest. `limit` mirrors the
   * server's `?limit=` param: omitted → the server's recent window (default
   * 30); `-1` → the WHOLE history (the M2-3 gallery's full-history path — the
   * gallery aggregates every attachment of a conversation, so it must not be
   * truncated to the recent window).
   *
   * `before` (T-bf82 scrollback) is the composite keyset cursor
   * (`?before_ts=&before_id=`, both together): the page becomes the `limit`
   * messages strictly OLDER than that (ts, id) point, still oldest→newest.
   * A page shorter than `limit` means the history is exhausted. A HISTORY
   * PAGE NEVER ADVANCES THE READ WATERMARK — the "list 即讀" auto-mark fires
   * only on a cursorless list of the newest window. */
  listChat(
    withId: string,
    limit?: number,
    before?: ChatCursor,
  ): Promise<ChatMessage[]>;
  /** READ-ONLY view of the same conversation: identical shape/order/window as
   * `listChat`, but WITHOUT the "list 即讀" read-watermark side effect. Used
   * when the thread must stay fresh while the owner is NOT actually looking
   * (window backgrounded / tab hidden) — the unread badge must keep counting
   * until the owner really reads. Rides the EXISTING wire surface only:
   * `GET /api/chat` with NO `?with=` never advances a watermark (the server's
   * auto-mark fires only when a specific conversation is requested), so the
   * http adapter fetches the unfiltered stream (`limit=-1`) and applies the
   * same participant filter + recent-window cap client-side. */
  peekChat(withId: string, limit?: number): Promise<ChatMessage[]>;
  /** The M2 gallery query (`GET /api/chat/attachments?with=<memberId>`): every
   * attachment of the member's conversations, flattened newest→oldest —
   * owner↔member BOTH directions AND the member's inter-agent threads — each
   * row carrying the sender id + server-resolved display name + send time.
   * READ-ONLY (no read-watermark side effect, unlike listChat's auto-mark). */
  listChatAttachments(withId: string): Promise<GalleryAttachment[]>;
  /** Mint the PERMANENT share link for one attachment
   * (`GET /api/chat/attachments/{id}/share-link`): resolves to the blob's
   * server-relative serve path carrying its `?sig=` file-level HMAC credential
   * — anyone holding the URL may read exactly this one blob, nothing else, no
   * expiry. Callers prefix the page origin to form the absolute, sendable URL.
   * Unknown id → 404 (throws). */
  getChatAttachmentShareLink(attachmentId: string): Promise<string>;
  /** Post a chat message. May carry text and/or MULTIPLE generic `attachments`
   * (pasted images AND/OR uploaded files, mixed), sent to the server as the
   * `attachments` list of `{data_b64, filename?, mime?}` objects — all riding
   * the SAME message. Empty (no body AND no attachments) is rejected by the
   * server (400); over the server's count cap (10) is a 400 too. */
  postChat(msg: {
    to: string;
    body: string;
    attachments?: ChatAttachmentInput[];
  }): Promise<ChatMessage>;
  /** Mark a conversation (with `peer`) read up to `lastReadTs` — the caller's own
   * read watermark (reader = the verified JWT sub server-side; anti-spoof). The
   * watermark is monotonic; a stale ts is a no-op. Returns the effective receipt. */
  markChatRead(mark: {
    peer: string;
    lastReadTs: number;
  }): Promise<ChatReadReceipt>;
  /** List read receipts for a `peer` conversation (`GET /api/chat/reads?with=`).
   * The UI reads the PEER's receipt to know how far the peer has read the owner's
   * messages (drives the per-message "read ✓" badge). */
  listChatReads(peer: string): Promise<ChatReadReceipt[]>;
  /**
   * List reply cards (`GET /api/reply-cards?status=`). `waiting` returns every
   * card still waiting for the owner, LONGEST-WAITING FIRST (created_ts
   * ascending — the 待回覆 pane order); `answered` returns cards answered
   * within the last 24 hours, newest answer first; `expired` returns cards the
   * owner marked expired within the last 24 hours, newest first (older
   * answered/expired cards drop off these lists but live forever in chat
   * history).
   */
  listReplyCards(
    status: "waiting" | "answered" | "expired",
  ): Promise<ReplyCard[]>;
  /**
   * Read ONE reply card in full (`GET /api/reply-cards/{card_id}`). B3's
   * inline chat card fetches the card this way — a chat message only carries
   * `replyCardId`, so the thread refetches the single card for the options /
   * status / answer, and again on every `reply_card` SSE delta (the
   * chat↔replies two-way sync). Unknown id → 404 (throws ApiError).
   */
  getReplyCard(id: string): Promise<ReplyCard>;
  /** Reply-card counts behind `GET /api/reply-cards/count`. `waiting` is the nav
   * badge (answered cards never count it). `answered` is the recently-answered
   * (24h) count the 等我回覆 page uses to render its collapsed 「近期已回覆 · N」
   * header (and hide the pane at zero) WITHOUT fetching the answered list. Kept
   * as its own cheap endpoint so both refetch on every `reply_card` SSE delta
   * without pulling the lists. */
  getReplyCardCount(): Promise<ReplyCardCounts>;
  /** The owner's TOTAL chat unread count behind the 辦公室 nav red dot
   * (`GET /api/chat/unread-count`). A dot shows when > 0 (the nav renders a
   * plain red dot, not the number). Kept as its own cheap endpoint so the dot
   * can refetch on every `chat` / `chat_read` SSE delta without pulling the
   * roster. */
  getChatUnreadCount(): Promise<number>;
  /**
   * Answer a WAITING card (`POST /api/reply-cards/{id}/answer`) — the ONLY way
   * a card ever closes (no close/skip verb exists). The answer is an option
   * and/or free text (+ attachments); empty → 400, out-of-range optionIdx →
   * 400, already answered → 409 (all reject as ApiError). Returns the answered
   * card; the caller refetches lists + count (the SSE delta also fans).
   */
  answerReplyCard(id: string, answer: ReplyCardAnswerInput): Promise<ReplyCard>;
  /**
   * Revise an ANSWERED card's answer (`PUT /api/reply-cards/{id}/answer` —
   * 重新決定, the owner changing their OWN answer). Same body + validation as
   * POST; a waiting card is a 409 (answer it with POST). The answer is replaced
   * wholesale, answeredTs re-stamps, and status STAYS `answered` — a revision
   * never reopens the card or re-counts the badge.
   */
  reanswerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput,
  ): Promise<ReplyCard>;
  /**
   * Mark a WAITING card expired (`POST /api/reply-cards/{id}/expire` — 標為過期,
   * the owner/admin-agent terminal exit that is NOT an answer). No body, no undo, no
   * reopen; answered/expired → 409, unknown id → 404 (thrown as ApiError). The
   * server releases any bound task/step hold exactly like a first answer — an
   * orphaned card on a closed task is still expirable (its only exit). Returns
   * the expired card; the caller refetches lists + count (the SSE delta also
   * fans).
   */
  expireReplyCard(id: string): Promise<ReplyCard>;
  // ── Tasks (M3 任務頁 + 任務卡) ──────────────────────────────────────────────
  /**
   * List tasks as LIGHT list items (the collapsed card's fields +
   * server-computed progress + deps, WITHOUT the heavy steps/description/inputs
   * — those hydrate on expand via getTask; the returned TaskView carries
   * `steps: []` and `description: ""` until then). Partitioning (未結束/已結束),
   * priority ordering AND the page's 篩選列 are applied CLIENT-SIDE.
   *
   * `opts.open` (T-2b9d) sends `GET /api/tasks?open=true` — the server drops
   * the terminal (done/terminated/duplicated) rows so the DEFAULT 任務頁 view,
   * which only shows the 未結束 partition, pulls a handful of rows instead of
   * the whole history. Omit it (the default) for the full population — the
   * 清除篩選 全部 view needs every task, and that call is byte-for-byte the
   * unfiltered list as before.
   *
   * `opts.statuses` (T-a3e4) sends one repeated `?statuses=` per state — ASK
   * FOR WHAT IS TICKED. `open=true` only removes the archive; the page was
   * still downloading every live task and then hiding most of them in the
   * browser. Pass the 狀態 dropdown's set verbatim, `reassigning` included (the
   * server matches that one against the handover lock, the same rule the
   * client-side predicate uses). An empty/omitted set means no constraint.
   */
  listTasks(opts?: {
    open?: boolean;
    statuses?: string[];
  }): Promise<TaskView[]>;
  /**
   * Fetch ONE task's FULL detail (`GET /api/tasks/{id}`): steps, description
   * and the rest of the heavy payload the light list omits. The 任務卡 calls
   * this the first time a card is expanded (and re-calls it when the task's
   * updatedTs moves while expanded) to hydrate the workflow timeline.
   */
  getTask(id: string): Promise<TaskView>;
  /** The task counts behind the nav badge (`GET /api/tasks/count`) — a cheap
   * dedicated endpoint so the badge can refetch on every "task" SSE delta
   * without pulling the list. `open` = non-terminal (the badge). `total` = every
   * task, terminal included (T-a3e4): since the list fetch now asks for a STATUS
   * SET, an empty list cannot by itself justify 目前沒有任務, and this is the
   * cheap way to know — never a widened list fetch. */
  getTaskCount(): Promise<TaskCountView>;
  /**
   * Terminate a task (`POST /api/tasks/{id}/terminate`) — the ONLY owner-side
   * status change (spec §3.7). Non-terminal only (done/terminated → 409,
   * thrown as ApiError). The double-confirm lives in the UI; the server
   * releases any bound outsource worker. Returns the terminated task; the
   * caller refetches (the SSE delta also fans).
   */
  terminateTask(id: string): Promise<TaskView>;
  /**
   * Mark a task duplicated (`POST /api/tasks/{id}/duplicate`), pointing at the
   * ORIGINAL it duplicates — so whoever spots the duplicate closes it instead of
   * the owner terminating each shell by hand (T-02c9). `duplicated` is a third
   * terminal status. `duplicateOf` must name an existing task that is not this
   * one, is not itself duplicated, and is not already an original of another
   * duplicate (all 409, thrown as ApiError); an already-closed task is a 409.
   * Returns the duplicated task; the SSE delta also fans.
   */
  markTaskDuplicate(id: string, duplicateOf: string): Promise<TaskView>;
  /**
   * Owner priority change (`POST /api/tasks/{id}/priority`): `high` | `mid` |
   * `low` | `frozen` — freeze/unfreeze ride the same knob (spec §3.3). Closed
   * tasks are a 409 (throws). Returns the task after the change.
   */
  setTaskPriority(id: string, priority: string): Promise<TaskView>;
  /**
   * Reassign a task (`POST /api/tasks/{id}/reassign`) — owner + 特助 only
   * (the server gates it; a member/worker caller is a 403). The server expires
   * the task's waiting cards, rewinds non-terminal steps to pending, dismisses
   * the OLD outsource worker, mints the new one when the target is 外包, moves
   * the task to `reassigning` and notifies BOTH sides to hand over. A closed
   * task is a 409, a frozen one a 400, and an inactive/warden/already-executor
   * member target a 400/409 (all throw ApiError). Returns the task after the
   * move; the caller refetches (the SSE delta also fans).
   */
  reassignTask(id: string, input: TaskReassignInput): Promise<TaskView>;
  /**
   * Un-pin one artifact from a task's set (`DELETE /api/tasks/{id}/artifact/
   * {artifactId}`) — the owner/admin cockpit action (T-3dc5; the executing
   * agent PINS via MCP but does not remove). Returns the task after the
   * removal (its `artifacts` folded fresh). Unknown task/artifact → 404,
   * wrong-task → 400 (both throw ApiError). The referenced blob is left intact.
   */
  removeTaskArtifact(taskId: string, artifactId: string): Promise<TaskView>;
  /**
   * The task-card message box (`POST /api/tasks/{id}/message`): the server
   * posts ONE ordinary chat message owner → the task's executor with the task
   * context auto-attached in meta. An unassigned executor is a 409 (the UI
   * disables the box); an empty message is a 400. Both throw ApiError.
   */
  postTaskMessage(id: string, msg: TaskMessageInput): Promise<void>;
  /**
   * List LIVE (not-yet-released) outsource workers
   * (`GET /api/outsource-workers`): codename / model / effort + the bound
   * task id. The task card resolves its 外包 executor display through this;
   * released workers drop off, so a CLOSED outsource task honestly renders
   * the bare 外包 label instead of a fabricated codename.
   */
  listOutsourceWorkers(): Promise<OutsourceWorkerView[]>;
  /** Read ONE live worker (`GET /api/outsource-workers/{id}`) — the SAME
   * projection the list serves, for the detail panel's post-relocate refresh.
   * A released / unknown worker → 404 (throws ApiError). (T-f190) */
  getOutsourceWorker(id: string): Promise<OutsourceWorkerView>;
  /** Relocate a worker to a machine (`POST /api/outsource-workers/{id}/relocate`
   * {machine_id}, admin-gated since P7c — the member relocate floor) — the
   * cockpit's 改機器, the worker twin of the
   * member machine-bind. Writes the owner-pinned placement, kills the current
   * session, and clears pacing so the next tick re-spawns on the chosen machine
   * (no lifecycle change). machineId = a concrete machine id that must resolve,
   * or "" (clear the pin). Returns the freshly-projected
   * worker; the caller can also lean on the outsource_worker SSE refetch. (T-f190) */
  relocateWorker(id: string, machineId: string): Promise<OutsourceWorkerView>;
  /** Refocus a worker (`POST /api/outsource-workers/{id}/refocus`, owner/admin-agent) —
   * the cockpit's 換手, the worker twin of refocusMember. Kills the current
   * session and re-spawns a fresh worker onto the SAME task. ONLINE-ONLY (409
   * otherwise); stopped → 409; unknown/released → 404. Returns the freshly
   * projected worker. (T-32e1) */
  refocusWorker(id: string): Promise<OutsourceWorkerView>;
  /** Stop a worker (`POST /api/outsource-workers/{id}/stop`, owner/admin-agent) — kill
   * the session and hold it down (presence "stopping"/"stopped"); no auto-revival. The
   * bound task stays put. Idempotent; unknown/released → 404. (T-f190) */
  stopWorker(id: string): Promise<OutsourceWorkerView>;
  /** WAKE a worker with no live session (`POST /api/outsource-workers/{id}/restart`,
   * owner/admin-agent) — clear the stop and re-dispatch. ⚠️ The owner-facing word
   * is 喚醒 since T-7526 (「重啟」 retired, one verb across both panels); the
   * ENDPOINT keeps its frozen name, so the adapter method does too. The guard is LIVENESS,
   * not intent (T-7526): 409 only when the worker is BOTH not held down and
   * currently online, so a worker whose session died on its own is revivable.
   * unknown/released → 404. (T-f190) */
  restartWorker(id: string): Promise<OutsourceWorkerView>;
  /** Change a worker's model/effort (`POST /api/outsource-workers/{id}/model`,
   * owner/admin-agent) — active+online → kill+respawn to take effect now, otherwise
   * persist for the next spawn. Returns the freshly projected worker. (T-f190) */
  setWorkerModel(
    id: string,
    patch: { runtime?: "claude" | "codex"; model: string; effort?: string },
  ): Promise<OutsourceWorkerView>;
  /** Read a worker's boot-context PREVIEW (`GET
   * /api/outsource-workers/{id}/boot-context`, owner/admin-agent) — the worker twin
   * of getBootstrap's role preview: the server re-assembles the persona text
   * (seed + identity + bound task + manual) from the CURRENT DB rows, no
   * token. HONEST: today's re-assembly, not a verbatim spawn-time record.
   * Unknown worker / gone task → 404 (throws ApiError). (T-ba6b) */
  getWorkerBootContext(id: string): Promise<string>;
  /** List task types (`GET /api/task-manuals`) in the light {typeKey, purpose}
   * shape — the tasks page's type-filter options (各手冊類型). Read-only here;
   * the manual editor reads the FULL manuals below. */
  listTaskTypes(): Promise<TaskTypeView[]>;
  // ── Task manuals (設定 › 任務手冊, SPEC §5) ────────────────────────────────
  /** List the FULL manuals (`GET /api/task-manuals`) — the 任務手冊 list page
   * (type cards: 類型名 + 用途摘要). 出廠不含任何類型 (honest empty list). */
  listTaskManuals(): Promise<TaskManualView[]>;
  /** Read ONE manual in full (`GET /api/task-manuals/{type_key}`) — the detail
   * page's 任務定義/學習經驗 tabs + 負責成員 card. Unknown → 404 (throws). */
  getTaskManual(typeKey: string): Promise<TaskManualView>;
  /** Create one task type as a BLANK manual (`POST /api/task-manuals`
   * {type_key}). Duplicate type_key → 409, blank → 422 (both throw ApiError).
   * Returns the created (empty) manual; the caller refetches the list (the
   * task_manual SSE delta also fans). */
  /** Create a task type from its DISPLAY NAME (T-fa76): the server mints the
   * `tm-` type_key (returned on the view) — the id is the system's, the text
   * is the human's. Blank name → 400/422 (throws ApiError). */
  createTaskManual(displayName: string): Promise<TaskManualView>;
  /** Partial manual edit (`POST /api/task-manuals/{type_key}`) — only supplied
   * fields change; `assignee: null` unsets (wire `{}`). Returns the manual
   * after the edit. Unknown → 404 (throws). */
  updateTaskManual(
    typeKey: string,
    patch: TaskManualPatch,
  ): Promise<TaskManualView>;
  /** Delete a task type (`DELETE /api/task-manuals/{type_key}`). OPEN
   * (non-terminal) tasks of the type → 409 (throws — the UI surfaces the
   * human-readable 先讓任務結束 message); unknown → 404. */
  deleteTaskManual(typeKey: string): Promise<void>;
  // ── Product guide (the 使用說明 nav tab) ──────────────────────────────────
  /** List the product-guide docs (`GET /api/docs`) — the 使用說明 landing
   * (slug + title cards). The same embed Mira reads via get_doc. */
  listDocs(): Promise<DocSummaryView[]>;
  /** Read ONE product-guide doc in full (`GET /api/docs/{slug}`) — the markdown
   * the 使用說明 doc page renders. Unknown slug → 404 (throws). */
  getDoc(slug: string): Promise<DocView>;

  /** Monitoring telemetry (three sections). Honest null/empty where no source. */
  getMonitoring(): Promise<MonitoringView>;
  /** Rename an account's display label. Blank/whitespace → server 422 (throws).
   * Returns void — the caller refetches monitoring for the new label. */
  patchAccount(id: string, patch: AliasPatch): Promise<void>;
  /** Rename a machine's display label. Blank/whitespace → server 422 (throws).
   * Returns void — the caller refetches monitoring for the new label. */
  patchMachine(id: string, patch: AliasPatch): Promise<void>;

  /**
   * Onboard a machine (`POST /api/machines`) → mint a warden member + a boot
   * command. The machine is created by `displayName` ONLY (there is no `host`
   * anymore — the server owns the opaque machine id); `opts` carries the optional
   * token TTL. Returns the view result whose `machineId` is the new stable id and
   * whose `bootCommand` (embedding a short-lived, single-use claim code — never
   * the token itself) the owner copies to the machine to bring the warden online. Owner/mira governance token required (401
   * if missing). SECURITY: the caller renders `bootCommand` into a copy control
   * ONLY and never logs it. After onboarding, refetch machines — the machine
   * surfaces (online) once its warden reports in.
   */
  onboardMachine(
    displayName: string,
    opts?: OnboardOptions,
  ): Promise<OnboardResultView>;
  /**
   * DELETE a machine (`DELETE /api/machines/{member_id}`, T-IUD) — the PURE
   * roster soft-delete verb (delete ≠ uninstall ≠ stop). `memberId` is the
   * warden member id (the machineId). It flips the record to status="removed" and
   * dispatches NO warden command — it removes the machine from the roster, it does
   * NOT tear the warden off the box (that is `uninstallMachine`). Returns
   * `{memberId, host, removed}` (no command string). The caller refetches
   * afterwards (the row drops).
   */
  deleteMachine(memberId: string): Promise<DeleteResultView>;

  /**
   * UNINSTALL a machine (`POST /api/machines/{member_id}/uninstall`, T-IUD) — the
   * MACHINE-lifecycle verb: write the owner intent desired_state="uninstall" so the
   * server reconcile arm drives the single `uninstall` RPC to the warden (which
   * runs `ocwarden uninstall` on its box). The record is KEPT (re-installable) —
   * the row does NOT drop (contrast deleteMachine). Returns `{memberId, host,
   * dispatched}`: `dispatched` is TRUE when the warden was online (the RPC will be
   * driven → the machine goes offline once it reports the receipt), FALSE when it
   * was already offline (treated as already uninstalled — nothing commanded).
   * ONLINE-ONLY semantics live in the UI (an offline machine has nothing to
   * uninstall). The caller refetches afterwards to pick up the new online state.
   */
  uninstallMachine(memberId: string): Promise<UninstallResultView>;

  /**
   * Re-fetch a machine's copy-paste install command anytime (`GET
   * /api/machines/{machineId}/boot-command`) → re-mints a fresh governance token
   * + a one-time claim code and returns the ready-to-run `boot_command` string
   * (the same operator string onboard produced, embedding the short-lived CODE —
   * never the token). Owner-gated (401 if missing).
   * SECURITY: the returned string is a secret — the caller renders it into a copy
   * control ONLY and never logs it. Unlike onboard, this creates no machine — it
   * just re-issues the command for an EXISTING machine the owner already has.
   */
  getMachineBootCommand(machineId: string): Promise<string>;
  /**
   * Install THIS machine's warden on the SERVER host in one click (`POST
   * /api/machines/{machineId}/bootstrap-here`, owner/admin-agent). A HOST-mutating action
   * — the caller CONFIRMS first (like teardown). Returns the view result:
   * `ok` + `exitCode` + `log`. On `ok === false` the `log` carries the reason
   * (e.g. the one-warden guard message); the caller MUST surface it (never
   * swallow). The promise resolves for both ok/!ok (a failed install is a real
   * result, not a thrown error) — only a transport/gate failure rejects.
   */
  bootstrapOnServer(machineId: string): Promise<BootstrapResultView>;
  /**
   * Tear THIS machine's warden down on the SERVER host in one click (`POST
   * /api/machines/{machineId}/teardown-here`, owner/admin-agent). The symmetric inverse of
   * `bootstrapOnServer`. A HOST-mutating action — the caller CONFIRMS first. Returns
   * the view result: `ok` + `exitCode` + `log` + `removed`. CONFIRM-THEN-REMOVE: the
   * warden member is soft-deleted server-side ONLY when the daemon is confirmed torn
   * down (`removed === ok`); on `ok === false` the `log` carries the reason and the
   * machine row STAYS (the caller must NOT drop the row unless `removed === true`).
   * The promise resolves for both ok/!ok (a failed teardown is a real result, not a
   * thrown error) — only a transport/gate failure rejects.
   */
  teardownOnServer(machineId: string): Promise<TeardownHereResultView>;

  // ── Settings: build identity + role journal (§3.9 / §3.4 #20–25) ──────────
  /** Build identity for the software-update card. Honest: a self-build's
   * version stays "0.0.0"; update_available mirrors the server's cached
   * GitHub Releases check — no phantom newer version. */
  getVersion(): Promise<VersionView>;
  /**
   * Explicit 檢查更新 (`GET /api/release/check`): the server asks GitHub
   * Releases synchronously and answers the fresh verdict — up_to_date /
   * update_available (with the tag + release link) / unknown (GitHub
   * unreachable — graceful degradation, never a thrown transport error from
   * the server side).
   */
  checkRelease(): Promise<ReleaseCheckView>;
  /**
   * Backup health (`GET /api/backup-health`, T-da06): is the SCHEDULED database
   * backup still producing retreat points? Its own small endpoint on purpose —
   * the topbar indicator is mounted app-wide, so hanging it on the large
   * monitoring fold would move a big payload for a three-value answer. Honest by
   * construction: a watchdog that has not evaluated yet answers `unknown`, never
   * `healthy`.
   */
  getBackupHealth(): Promise<BackupHealthView>;
  /** The folded global-context doc (owner overlay ⊕ file seed). */
  getGlobalContext(): Promise<GlobalContextView>;
  /** Whole-doc replace of the global context → returns the folded doc
   * (`isDefault` flips false). */
  saveGlobalContext(text: string): Promise<GlobalContextView>;
  /** Reset the global context to seed (idempotent tombstone → `isDefault` true). */
  resetGlobalContext(): Promise<GlobalContextView>;
  /** List the folded role definitions (seed defaults + owner edits). */
  listRoles(): Promise<RoleDefView[]>;
  /** The folded role definition for `key`. */
  getRole(key: string): Promise<RoleDefView>;
  /** Partial edit of a role definition → returns the folded doc. */
  saveRole(key: string, patch: RolePatch): Promise<RoleDefView>;
  /** Reset a role definition to seed (idempotent tombstone → `isDefault` true). */
  resetRole(key: string): Promise<RoleDefView>;
  /**
   * Create ONE custom role + its ONE founding member (`POST /api/roles`, M2-2).
   * The server mints both ids; the role doc starts from the 「你是誰 / 你做什麼」
   * fill-me template; the member starts OFFLINE (never spawns) with the given
   * model/effort launch knobs. 422 (throws) on a blank name/memberName or an
   * effort outside low/medium/high.
   */
  createRole(input: RoleCreateInput): Promise<RoleCreateResult>;
  /**
   * HARD-delete a CUSTOM role + its members + their conversations / receipts /
   * lessons (`DELETE /api/roles/{key}`, M2-2). Server-side 防線 (not UI-only):
   * a seed role → 403; ANY member of the role online → 409 (the caller surfaces
   * 「有成員在線上，無法刪除」); unknown → 404. All three reject (throw). On
   * success the role row, its members and their chat/receipts/lessons are
   * PHYSICALLY gone — the caller refetches roles + members.
   */
  deleteRole(key: string): Promise<void>;

  /**
   * Preview a member's initial boot prompt — the assembled persona (role
   * definition ⊕ global context ⊕ lessons) from /api/bootstrap. Pass the ROLE
   * key (NOT a member_id) so the server mints NO token: a UI preview must never
   * receive an agent credential (§3.4 #29 — member_id is the warden-spawn path).
   */
  getBootstrap(role: string): Promise<BootstrapView>;
  /**
   * The folded PER-ROLE lessons doc for a `roleKey` + `task_type` (the single
   * fixed task_type key is "general"). Per-role-learnings step1: scoped to a
   * role_key — agents sharing a role share the accumulated lessons.
   */
  getLessons(roleKey: string, taskType: string): Promise<LessonsView>;
  /**
   * Whole-doc replace of the PER-ROLE lessons for a `roleKey` + `task_type` →
   * returns the folded doc (`isDefault` flips false). Backend contract is POST
   * (NOT the PUT/DELETE the global-context save uses). WRITE authz is per-role
   * and keyed on the PRINCIPAL CLASS, not the token scope (T-5336): a caller at
   * or above admin_agent — the owner (this UI's scope) and the admin agent —
   * may write ANY role; every other agent may write only its own role.
   */
  saveLessons(
    roleKey: string,
    taskType: string,
    text: string,
  ): Promise<LessonsView>;
  /**
   * The folded PER-ROLE insight doc for a `roleKey` (T-3809) — the role
   * journal's third block, beside Duty and Learning. No `task_type` axis and no
   * file seed, so an untouched doc reads as genuinely empty.
   *
   * READ IS UNRESTRICTED and that is deliberate: any authenticated identity may
   * read ANY role's insight. Insight is SEPARATE, not private — this release
   * narrowed WRITE only. Do not build a surface that implies otherwise.
   */
  getInsight(roleKey: string): Promise<InsightView>;
  /**
   * Whole-doc replace of the PER-ROLE insight doc → returns the folded doc
   * (`isDefault` flips false). WRITE authz is per-role and keyed on the
   * PRINCIPAL CLASS: a caller at or above admin_agent — the owner (this UI's
   * scope) and the admin agent — may write ANY role; every other agent may
   * write only its own role, and the 403 names `insight` rather than borrowing
   * the lessons wording.
   */
  saveInsight(roleKey: string, text: string): Promise<InsightView>;
  /**
   * The retained revisions of ONE editable long-form document, newest first
   * (`GET /api/document-history/{kind}/{key}`). At most 3 are kept — the
   * server prunes, the cockpit never has to.
   */
  listDocumentHistory(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentHistoryView[]>;
  /**
   * Restore ONE retained revision over the LIVE document (destructive — the
   * current text becomes just another retained revision). Returns the restored
   * revision DTO; the caller re-reads the document itself, which is the only
   * honest source for what is now on screen.
   */
  restoreDocumentHistory(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentHistoryView>;
  /**
   * PUBLIC first-run probe (`GET /api/auth/status`): true once an owner
   * password is set. AuthGate branches first-run setup vs login on it.
   */
  getAuthStatus(): Promise<boolean>;
  /**
   * First-run owner-password claim (`POST /api/auth/set-password`). The
   * claim token comes from the server's local serve log / installer banner.
   * On success the minted owner token is persisted (the caller is logged
   * in). Rejects: 401 wrong claim token, 409 already set, 422 short password.
   */
  setPassword(password: string, claimToken: string): Promise<void>;
  /**
   * Change the owner password (`POST /api/auth/change-password`). Verifies
   * the current password (401 on a mismatch — surfaced inline, NEVER via the
   * auth-expired bounce) and persists the fresh owner token the server mints
   * (every pre-change owner session is revoked server-side).
   */
  changePassword(currentPassword: string, newPassword: string): Promise<void>;
  /** Read the owner-adjustable server settings (`GET /api/settings`). */
  getServerSettings(): Promise<ServerSettingsView>;
  /**
   * Partial settings edit (`PATCH /api/settings`) — durable and live
   * immediately (token_ttl from the next login, handover_pct from the next
   * context report). Returns the settings after the change.
   */
  patchServerSettings(patch: ServerSettingsPatch): Promise<ServerSettingsView>;
  /** Read the VAPID public key used by PushManager.subscribe. */
  getPushPublicKey(): Promise<string>;
  /** Save or refresh this browser's Web Push subscription. */
  savePushSubscription(subscription: PushSubscriptionInput): Promise<void>;
  /** Remove a browser endpoint after it has been disabled locally. */
  removePushSubscription(endpoint: string): Promise<void>;
  /**
   * Owner's EXPLICIT upgrade trigger (`POST /api/update/upgrade`) — the
   * software-update card's button (the OPT-IN auto-update setting runs the
   * same verified body unattended). A resolved call means the verified
   * binary swap already LANDED and the server is restarting into the new
   * build (watch /api/version for the new git_sha). Rejects honestly: 409
   * no newer release known; 502 download-verify-swap failures (the old
   * build keeps serving) — the caller surfaces the server message.
   */
  triggerUpgrade(): Promise<void>;
  /**
   * Subscribe to the SSE topic stream. `onTopic` fires with a topic name
   * (e.g. "members" / "presence"); the caller reconciles BY REFETCH (never by
   * merging an event payload). Returns an unsubscribe function.
   *
   * The second argument names WHICH item the delta touched (see `SseDelta`) so
   * a subscriber can refetch that one item instead of its whole list. It is
   * OPTIONAL on purpose: a resync fans deltas that name nothing, and a
   * transport (the mock, an older producer) may not supply it at all — an
   * absent delta MUST be read as "something in this topic changed, refetch the
   * lot", never as "nothing changed".
   */
  subscribeEvents(
    onTopic: (topic: string, delta?: SseDelta) => void
  ): () => void;
}
