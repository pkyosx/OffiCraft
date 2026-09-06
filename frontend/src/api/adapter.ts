// api/adapter.ts — the typed api client seam (structural, not a class).
//
// `Api` is the ONE interface the UI programs against. Two implementations
// satisfy it: `mockApi` (mock.ts, wired in M1) and `httpApi` (http.ts, the real
// backend stub). The swap point is index.ts — the UI never imports mock/http.
//
// All methods return view-model shapes (`Member` / `ChatMessage`), never wire
// DTOs: the wire→view mapping is the adapter's job (see mappers.ts).

import type { DiffParams } from "../lib/diffLink";
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
  SigningKeyView,
  AuthStatusView,
  MfaEnrollView,
  MfaStateView,
  GlobalContextView,
  BootDocKind,
  BootDocView,
  DocumentKind,
  DocumentHistoryEntryView,
  DocumentHistoryView,
  DocumentRevisionView,
  DocumentSeedView,
  DiffPairView,
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
   * the inline ChatReplyCard label its COLLAPSED row (待回覆 / 已回覆 / 已過期)
   * WITHOUT a per-card GET.
   *
   * ⚠️ It used to say this field decides AT MOUNT whether to load eagerly
   * (waiting) or lazily (answered). It does not any more: since T-48
   * (`rc-d8844e709f42`) EVERY chat card mounts collapsed regardless of status
   * and fetches only on expand, so what this field decides is what the row SAYS
   * while nothing has been fetched. `TaskReplyCard` is unchanged.
   *
   * OPTIONAL so hand-built test fixtures stay valid (same precedent as
   * `ReplyCard.task`); the mapper always sets it (null when the wire carries
   * ""). */
  replyCardStatus?: "waiting" | "answered" | "expired" | null;
  /** The sender's DISPLAY name, resolved server-side from the roster (wire
   * `from_name`). `from` stays the ADDRESS and never changes meaning — this
   * rides ALONGSIDE it, never instead of it. `""` when the sender does not
   * resolve to a roster row: an HONEST empty, and a reader that back-fills the
   * id into it is fabricating a name. OPTIONAL so hand-built fixtures stay
   * valid (same precedent as `replyCardStatus`); the mapper always sets it. */
  fromName?: string;
  /** The addressee's DISPLAY name (wire `to_name`), resolved the same way and
   * carrying the same honest-empty rule as `fromName`. */
  toName?: string;
  /** `ts` rendered BY THE SERVER for a reader as `YYYY-MM-DD HH:MM:SS ±HH:MM`
   * in the server's own zone (wire `ts_display`).
   *
   * 🔴 The cockpit renders THIS STRING and never formats `ts` itself. A second
   * formatter here would be a second answer to "when was this" — and it would
   * be the WRONG one: the studio has no configured timezone, so a browser-side
   * format silently re-states the message in the viewer's zone while the agent
   * reading the same snapshot sees the server's. Same payload, two different
   * times. `""` outside the wake snapshot (`list_chat` does not render one). */
  tsDisplay?: string;
  /** COLLAPSE marker (wire `body_omitted_chars`): how many characters of THIS
   * message's body the wake snapshot folded away; `0` = the body is here in
   * full. The folded text is STILL ON THE SERVER — `get_chat` re-reads it.
   *
   * 🔴 This is NOT `MemberResumeSummaryView.chatEarlierOmitted`. That one
   * reports whole messages that are ABSENT from the payload. One shortened
   * message versus messages not carried at all — reading one as the other is
   * how a reader concludes it has seen a conversation it has not seen, so the
   * two must never share a word on screen. */
  bodyOmittedChars?: number;
  /** The reply card this message carries, FOLDED IN PLACE onto the message
   * that opened it (wire `card`) — so the decision reads IN the chat stream
   * rather than in a second, separately-joined card list. null ⇒ no card. */
  card?: ChatInlineReplyCardView | null;
  /** The id of the message this one is REPLYING TO (wire `reply_to`), or null
   * when it replies to nothing. It is the ONE fact about whether this message
   * is a reply, and it never goes away. It may name a message in ANOTHER
   * conversation (owner ruling, 2026-08-21). */
  replyTo?: string | null;
  /** WHAT this message is replying to (wire `reply_to_chat`) — the quoted
   * sender and a server-shortened line of what they said, rebuilt by the server
   * on every read. null when `replyTo` is null, and null ALSO when `replyTo` is
   * set but the quoted message no longer exists.
   *
   * 🔴 THE UI READS THIS AND NOTHING ELSE (T-4e95, owner ruling 2026-08-21).
   * There is no lookup, no fallback to the loaded window, no re-fetch and no
   * retry: either the snapshot is here or the original is gone, and "gone" is a
   * settled answer that renders as one fixed sentence. The shape this replaced
   * shipped the id alone and made the browser go and find the rest, which meant
   * a request that could fail, a temporary lie on screen while it had failed,
   * and a story about healing that lie later — three states that look identical
   * to the eye whether they are right or wrong. */
  replyToChat?: ChatReplyQuote | null;
}

/** The quoted message a reply carries with it (wire `ChatReplyQuoteDTO`).
 *
 * `from` / `fromName` and `to` / `toName` are `ChatMessage`'s own convention:
 * the bare id is the ADDRESS and always present, the name beside it is `""` on
 * the reads that resolve no names (everything but the wake snapshot) — so the
 * thread resolves the name from its roster exactly as it does for any other
 * participant rather than trusting this one to be filled.
 *
 * 🔴 `to` IS THE QUOTED MESSAGE'S OWN ADDRESSEE, never the peer of the thread
 * the reply is drawn in. A quote may come out of another conversation entirely
 * (owner ruling 2026-08-21), and that is exactly when the two differ — which is
 * the case the field exists for. */
export interface ChatReplyQuote {
  id: string;
  from: string;
  fromName: string;
  to: string;
  toName: string;
  /** One line of the quoted body, already whitespace-collapsed and shortened BY
   * THE SERVER (the length is defined there and nowhere else). `""` is an
   * ordinary value — an attachment-only message has no text to quote. */
  content: string;
}

/** One reply card folded onto the chat message that opened it (view model of
 * `ChatInlineReplyCardDTO`) — the DECISION and nothing else: the options as
 * they were offered, which one was picked, the free text, and when. The card's
 * summary/body/kind/attachments are deliberately NOT here (the message this
 * rides on already carries the ask). */
export interface ChatInlineReplyCardView {
  /** The frozen quick-reply choices as offered, each carrying its OWN
   * `aiPick`. Empty for a card opened without options. */
  options: ReplyCardOption[];
  /** Indices into `options` of EVERY option that was circled; null when
   * answered with free text only, or not answered yet. Deduped + ascending as
   * the server stored it. */
  answerOptionIdxs: number[] | null;
  /** The free-text answer; "" when none was given. */
  answerText: string;
  /** Epoch seconds the card was answered; 0 while still waiting. */
  answeredTs: number;
  /** `answeredTs` rendered by the SERVER in the same full date + time + offset
   * form as `ChatMessage.tsDisplay`, for the same reason. "" while unanswered.
   * The cockpit prints it as given and never re-formats `answeredTs`. */
  answeredAtDisplay: string;
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

/** The T-48 anchor window: locate ONE message by its id and page outwards from
 * it. Both ends are INCLUSIVE and both take a message id (not a (ts, id)
 * keyset), which is what makes them usable from a link that only carries an id.
 *
 * `startId` walks TOWARDS THE NEWEST — the anchor plus the `limit`-1 messages
 * that FOLLOW it. That direction is the one `before_ts`/`before_id` cannot
 * express at all, and its absence is why "跳到原訊息" used to have to guess.
 * `endId` walks TOWARDS THE OLDEST — the anchor plus the `limit`-1 before it.
 * Either answer still comes back oldest→newest.
 *
 * Given TOGETHER the pair bounds one window; `limit` still caps it and the
 * truncation happens at the `startId` (older) end, i.e. the window stays
 * anchored on `endId`. Contradictory pairs, an unknown id, mixing these with
 * `before_ts`/`before_id`, and a `limit` outside 1..200 are all errors on the
 * server — never a quietly empty page. */
export interface ChatAnchor {
  startId?: string;
  endId?: string;
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

/** ONE frozen quick-reply choice, in view-model form. `aiPick` is the ONLY
 * carrier of "the AI recommends this one" — it replaced the positional
 * `options[0]` convention, so a chip must read THIS flag and never its own
 * index. */
export interface ReplyCardOption {
  text: string;
  aiPick: boolean;
}

/** The stored answer on an ANSWERED reply card, in view-model form.
 * `optionIdxs` is null for a pure free-text answer, otherwise the deduped,
 * ascending indices into the card's `options` of EVERY circled option;
 * `attachments` are served refs into the shared chat-attachment store (render
 * like chat attachments). */
export interface ReplyCardAnswer {
  optionIdxs: number[] | null;
  text: string;
  attachments: ChatAttachmentView[];
}

/**
 * One reply card (等我回覆卡) in view-model form. `status` is the closed set
 * `waiting` | `answered` | `expired` — the only transitions are
 * waiting→answered via an answer (the owner's positive close; no generic
 * close/skip surface anywhere) and waiting→expired via the expire action, open to
 * the card's own author as well as the owner / an admin agent (標為過期 — NOT an
 * answer; terminal, no reopen; T-1b88 widened T-6020's admin floor); a revised answer
 * (重新決定) keeps `answered`. Each entry of `options` carries its own
 * `aiPick` (position means nothing) and `selectMode` says how many of them the
 * owner may circle. `chatMessageId` links the chat message the card rides in —
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
  options: ReplyCardOption[];
  /** How many options the owner may circle: "single" (at most one — a second
   * pick REPLACES the first) | "multi" (toggle any number). A separate axis
   * from `kind`, which says what the owner must DO. */
  selectMode: "single" | "multi";
  status: "waiting" | "answered" | "expired";
  /** QUESTION-side attachments the initiator opened the card with (T-5e8a) —
   * served refs into the shared chat-attachment store, rendered like chat
   * attachments on every card face regardless of status. Always an array
   * ([] when none), same posture as `answer.attachments`. */
  attachments: ChatAttachmentView[];
  createdTs: number;
  answeredTs: number | null;
  /** Epoch seconds of the expire action (whoever pressed it — the card's author,
   * the owner, or an admin agent); null unless expired.
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

/** The owner's answer to a reply card: the quick-reply `optionIdxs` and/or
 * free `text`, plus optional staged `attachments` (same input shape + limits as
 * chat attachments). At least one of the three must be present — the server
 * rejects an empty answer (400), and an EMPTY `optionIdxs` list counts as empty
 * rather than as an answer, so a caller with nothing circled omits the field
 * instead of sending `[]`. Order and duplicates do not matter to the server
 * (it stores the list deduped + ascending), but the cockpit sends it already
 * sorted so two owners who ticked the same boxes in different orders produce a
 * byte-identical body. */
export interface ReplyCardAnswerInput {
  optionIdxs?: number[];
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
  /** How many characters of working note this step has ON THE SERVER (T-66).
   *
   * 🔴 THE NOTE TEXT IS NOT ON THIS VIEW MODEL, and that is the whole shape of
   * the ticket (owner rc-4c8065fb30a5:「整個拿掉…座艙改成點開才抓」). The task
   * read no longer carries it, so this number is what the card has to work
   * from: `> 0` draws the 備註 entry, `0` draws nothing because the step
   * genuinely has none. The text arrives from `getTaskStep` when someone opens
   * it. A component that wants to SHOW a note and finds only this number is
   * being told, correctly, to go and fetch it.
   *
   * OPTIONAL so hand-built fixtures stay valid (the replyCardStatus
   * precedent); the mapper always sets it. */
  noteSizeChars?: number;
  /** Non-empty ⇒ this leaf runs inside a parallel stage (同時進行 · N 項並行);
   * consecutive steps sharing the group render as one parallel block. */
  parallelGroup: string;
  orderIdx: number;
  startedTs: number;
  finishedTs: number;
}

/** ONE step in FULL (T-66) — what `getTaskStep` answers, and the only place the
 * cockpit ever holds a step note's text. Everything a `TaskStepView` carries,
 * plus the `note` itself and the two size numbers.
 *
 * `detailLevel` is carried across from the wire rather than dropped: it is the
 * payload's own statement that this is the whole step, the mirror of the task
 * view's `"summary"`. Keeping it means a caller that somehow received the wrong
 * projection can say so instead of silently rendering a blank note. */
export interface TaskStepDetailView extends TaskStepView {
  detailLevel: string;
  note: string;
  noteSizeChars: number;
  noteCapChars: number;
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
 * dep whose task is gone — it IS the id (T-5291), so naming the dep never
 * needed the dep's row to exist; `title`/`status` are "" in exactly that case,
 * and the row then says 查無此任務 rather than inventing a status. */
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
  /** The task NUMBER, which IS `id` (T-5291): the wire sends it and the card
   * shows it verbatim, so what a human copies off the screen is exactly what
   * `#tasks/<id>` and MCP `get_task(task_id)` accept. It used to be a four-hex
   * projection ("T-7d40") that was display-only and could never be pasted
   * back; that projection is gone. Kept as its own field because it is what
   * the SERVER sends (`task_no`) — not because it differs from `id`. */
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
  /** Blocking task IDS (被依賴擋住, 可多筆). The card prints each id as-is —
   * task_no IS the id (T-5291), so there is no display conversion. */
  deps: string[];
  /** The SERVER's resolution of every id in {@link deps} (wire `dep_tasks`,
   * T-a3e4): one entry per dep, same order, carrying what the 「等 <task id> <標題>」
   * row prints. The card renders straight from this — it no longer
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
   * "duplicated". The card renders "重複於 <task id>" as a link that jumps to it —
   * depth-1 by construction, so the link always resolves in one hop. */
  duplicateOf: string;
  createdTs: number;
  updatedTs: number;
  /** Epoch when the task closed (done/terminated/duplicated); null while open. */
  closedTs: number | null;
  progressDone: number;
  progressTotal: number;
  steps: TaskStepView[];
  /** Number of pinned deliverables — the collapsed card's 「產物 N」 badge (0 ⇒
   * badge hidden), and since T-92 the ONLY thing a task response says about
   * them. BOTH projections now read it from the server's `artifact_count`: the
   * light list always did, and the full task carries the same field instead of
   * an array whose length the card had to take.
   *
   * 🔴 THERE ARE NO ARTIFACT ROWS ON THIS VIEW MODEL AT ALL (T-92, owner
   * rc-15016959ad4d:「只有 ID 好像也沒用」). T-66 had already cut them to id +
   * label; the ids went too, because a caller holding one is about to act on
   * that artifact and needs the whole row anyway. `listTaskArtifacts` answers
   * the whole ticket in one call — which is what `TaskArtifactsPopover` does the
   * moment someone opens it, and has done since before this change.
   *
   * OPTIONAL so hand-built fixtures stay valid; the mapper always sets it. */
  artifactCount?: number;
}

/** ONE pinned deliverable on a task's artifact set (T-3dc5, narrowed by T-92),
 * in view-model form. `kind` is the closed set file|image|link, and EVERY kind
 * is backed by a chat-attachment blob. For file/image, `url` is the blob serve
 * path and `mime` is that blob's content type (render it exactly like a chat
 * attachment). For link, `url` is the external address read out of that link's
 * `text/uri-list` blob, whose `mime` is `text/uri-list`. `filename`, `isImage`,
 * `attachmentId` and the old single `label` are all GONE FROM THIS ROW — and
 * for `attachmentId` read that literally: it is gone from THIS VIEW MODEL, not
 * from the wire. It came back to the wire under owner rc-91e29b576ad8 and this
 * adapter simply does not map it. The rest of the sentence is unchanged: `name`
 * replaces the label (server-derived, never empty), the filename is what that
 * derivation reads, and isImage is a prefix test on `mime`. Honest passthrough —
 * never fabricated. */
export interface TaskArtifactView {
  id: string;
  kind: "file" | "image" | "link";
  /** Where the content is — ONE meaning on all three kinds since T-92: the blob
   * serve path for a file/image, the external address for a link. */
  url: string;
  /** The display name. NEVER EMPTY on the wire — the server derives one from
   * the blob's filename, the link target, or the id when the row has no stored
   * name — so a renderer needs no fallback of its own any more. */
  name: string;
  /** The prose half of what used to be one `label`. May be empty, and MAY BE
   * LONGER than the 256-rune write cap: that cap binds new writes only and
   * never touched the migrated values, so never size anything on it. */
  description: string;
  /** The blob's content type. 🔴 LOAD-BEARING: `kind: "file"` covers .md, .pdf
   * and .zip alike, so this is the only field the preview can tell them apart
   * by — `MarkdownPreviewOverlay` makes four separate decisions with it. */
  mime: string;
  createdTs: number;
  createdBy: string;
  /** How many versions this deliverable has, the LIVE one INCLUDED (T-60) — 1
   * for one that has never been replaced, and bounded above because only the
   * most recent few replaced versions are retained.
   *
   * 0 is NOT "no versions": it is what an older server that never sends the
   * field reads as (the wire default). Both readings say the same thing to the
   * screen — there is nothing to list — so the versions entry keys on `> 1`,
   * never on `!== 1`. */
  versionCount: number;
}

/** ONE retained PREVIOUS version of a pinned deliverable (T-60), in view-model
 * form — what the artifact pointed at before a replace, newest first.
 *
 * It carries the version WHOLE (a blob id — every kind is blob-backed since
 * T-92 — plus its name and prose);
 * `id` is the
 * version's own row id and `kind` always equals the live artifact's, which
 * cannot change across versions. `url`, `mime`, `filename` and `isImage` are
 * that version's OWN facts, resolved by the server from the retained blob — a
 * file/image version's `url` is the blob serve path (never empty while the blob
 * is alive), and the mime is this version's, never the live row's.
 *
 * ⚠️ NOT the same resolution the live artifact gets, on ONE kind: for a LINK
 * version the server reads only the blob's BYTES (the uri-list target, into
 * `url`) and leaves `mime`/`filename`/`isImage` empty, whereas the live link
 * artifact does report `text/uri-list`. Never size a link comparison on those
 * three. */
export interface TaskArtifactVersionView {
  id: number;
  kind: "file" | "image" | "link";
  url: string;
  /** This version's stored display name (T-92 split the old single `label`).
   * ⚠️ UNLIKE the live artifact's `name`, this one is NOT derived and CAN be
   * empty — it is the column as it was written, and a version written before
   * names existed has none. That asymmetry is why this row still carries
   * `filename` while the live one does not. */
  name: string;
  /** This version's prose (T-92). May be empty, and may exceed the write cap
   * for the same reason the live artifact's description may. */
  description: string;
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
  /** The bound task's number and type — the panel row is 名稱 / task type +
   * presence 點 / 可點的任務編號 (owner report 2026-07-14, aligned with the
   * member card's three-line shape). The number IS the task id since T-5291
   * (it used to be a four-hex short form), so the row's chip is the string a
   * human can paste straight back into `#tasks/<id>`. WIRE FIELDS since T-a3e4
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
  /**
   * RESPONSE-ONLY signals an owner verb leaves on its own answer (T-ed79 #5/#12,
   * wire `relocation_pending` / `relocation_deferred` / `activation_pending`) —
   * the worker twins of {@link MemberRelocateResult} / {@link MemberActivateResult}.
   *
   * `undefined` on every list/GET and on every verb that has nothing to defer, so
   * "this answer does not carry the signal" stays distinguishable from "false".
   *
   * `relocationPending` is true for BOTH a deliberate deferral and a move that
   * could not be dispatched at all; `relocationDeferred` is what tells them
   * apart, and a consumer must NOT raise a "nothing was dispatched" alert while
   * it is true.
   */
  relocationPending?: boolean;
  relocationDeferred?: boolean;
  activationPending?: boolean;
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

/** ONE ROW of the manuals list (任務手冊 — a task type / playbook) WITHOUT its
 * two long documents: the guided definition's short answers (Q1 purpose / Q2
 * fields), the 負責成員 assignee setting, and how big the SOP and the 學習經驗
 * are. NO internal filename anywhere — manuals are presented as content, not
 * files (spec §5.2 note).
 *
 * 🔴 T-1170 split this off `TaskManualView`. `GET /api/task-manuals` now
 * answers what `?view=list` used to: `sop_md` / `learnings` are NOT on the
 * wire, only their char counts and the caps in force. The two sub-pages that
 * render those documents read them through `useTaskManual`
 * (`GET /api/task-manuals/{type_key}`). */
export interface TaskManualSummaryView {
  typeKey: string;
  /** Owner/agent-editable label; empty ⇒ the UI falls back to typeKey. */
  displayName: string;
  /** Q1 這是什麼任務 — the intake window's type-matching criterion. */
  purpose: string;
  /** Q2 需要哪些資訊 — the input-field list. */
  fields: TaskManualFieldView[];
  assignee: ManualAssigneeView;
  updatedTs: number;
}

/** One FULL task manual — a list row plus the two long documents it sizes
 * (Q3 SOP markdown, and the accumulated 學習經驗 agents write back on task
 * close). Answered by `GET /api/task-manuals/{type_key}` and by every manual
 * write; never by the list. */
export interface TaskManualView extends TaskManualSummaryView {
  /** Q3 該怎麼做 — the SOP markdown the AI plans the workflow from. */
  sopMd: string;
  /** 學習經驗 — agent write-back on task close; owner-editable too. */
  learnings: string;
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

// ── Themes (T-83ef 主題包自己一個資源) view models ─────────────────────

/** ONE row of `GET /api/themes` (T-83ef): a saved theme's id and its display
 * name, and NOTHING else. It is a LIST ITEM rather than the bundle on purpose
 * — a theme carries its images EMBEDDED, so a list of whole bundles runs to
 * hundreds of kilobytes or megabytes, which is exactly the payload this
 * resource exists to stop serving. `name` is what the theme list and the
 * profile picker render; `id` is what selects, reads, writes or deletes it.
 * Everything a caller might want a theme FOR is about ONE theme — and that one
 * theme is {@link Api.getTheme}. */
export interface ThemeListItem {
  id: string;
  name: string;
}

/** The receipt of a single-theme write (`PUT /api/themes/{theme_id}`, T-83ef).
 * A RECEIPT and NOT the stored bundle echoed back: echoing it would send the
 * embedded images a second time, i.e. the payload this split exists to remove.
 * `created` distinguishes a theme that did not exist before from one that was
 * replaced; `orderIdx` is its position in the owner's list, which a replace
 * KEEPS (re-colouring a theme does not move it to the bottom); `updatedAt` is
 * the epoch seconds this write landed. */
export interface ThemeWriteReceipt {
  id: string;
  created: boolean;
  orderIdx: number;
  updatedAt: number;
}

/** The receipt of `DELETE /api/themes/{theme_id}` (T-83ef).
 * `displayThemeReset` is the part a caller cannot work out on its own:
 * deleting the ACTIVE theme resets `display_theme` back to `""` in the SAME
 * request (the coupling the old whole-array settings write performed), and
 * this field says whether that happened — so the cockpit does not have to
 * re-read settings to discover its theme changed under it. */
export interface ThemeDeleteResult {
  id: string;
  deleted: boolean;
  displayThemeReset: boolean;
}

/** The owner-adjustable server settings in view-model form (`/api/settings`). */
export interface ServerSettingsView {
  /** Owner-login lifetime in seconds (one of 43200/86400/604800/2592000). */
  ownerTokenTtl: number;
  /** Member and outsource-worker lifetime in seconds. */
  agentTokenTtl: number;
  /** Context auto-handover threshold in percent (40..90). */
  handoverPct: number;
  /** The FIRST (soft) offboard point — T-a9d6. `handoverPct` is the second. */
  noticePct: number;
  /** The codex twin of `noticePct`: the SOFT-notice compaction round. */
  codexNoticeRound: number;
  /** Codex context compactions before automatic refocus (1..10). */
  codexCompactionThreshold: number;
  /** Minimum seconds between telemetry-triggered monitoring refreshes (1..60). */
  monitoringRefreshSeconds: number;
  /** 加速停止 grace in seconds (10..3600; default 120) — how long a CLOCKED
   * wind-down waits before the server forces the collection. ONE number for BOTH
   * clocked causes (the second context threshold and the owner-pressed
   * 加速停止), so the countdown an agent is quoted and the deadline the server
   * collects on cannot be two different values. It says HOW LONG, never WHO: a
   * soft cause stays uncollected at any value. */
  acceleratedGraceSecs: number;
  /** M3: the GLOBAL cap on concurrently live outsource workers (-1..20;
   * **-1 ⇒ 無限 (unlimited — no global cap)**; 0 ⇒ outsource assignment is
   * PAUSED — the panel annotates it). */
  outsourceMaxParallel: number;
  /** T-3aeb / T-ae38 / T-30f1: the independent size caps on the accumulating
   * documents, in CHARACTERS (runes) — a role's Duty (role definition),
   * Insight, Learning (the lessons doc), and a task manual's sop_md and
   * learnings, which answer to one cap EACH. The shipped defaults live in
   * `DOC_CAP_CHARS_DEFAULTS` (docCap.ts, mirroring server/ocserverd/domain.go);
   * Duty's is deliberately much smaller than every other one. Each floor IS that segment's own
   * default and the ceiling is 100000, so a cap only ever goes UP. Numbers are
   * not restated here — they are owner-adjustable settings. */
  docCapCharsDuty: number;
  docCapCharsInsight: number;
  docCapCharsLearning: number;
  docCapCharsManualSop: number;
  docCapCharsManualLearnings: number;
  /** T-791e: the two boot-context blocks' caps, on the SAME settings surface
   * and in the same rune unit. `docCapCharsBootSequence` is ONE number across
   * both runtimes — claude and codex are two documents of one block, each
   * measured on its own text. Defaults in `BOOT_DOC_CAP_CHARS_DEFAULTS`
   * (docCap.ts); same floor-is-the-default, ceiling-100000 rule as above. */
  docCapCharsSystemInteraction: number;
  docCapCharsBootSequence: number;
  /** T-c9c0: the 〈停止〉 document's cap, same surface and same rule. */
  docCapCharsOffboard: number;
  /** T-c9b4: the wake snapshot's chat block budget, in the same rune unit.
   * NOT a document cap — it bounds a block the server repacks on every read, so
   * it may be lowered as well as raised, and it has its own ceiling. Default and
   * range in `chatBudget.ts` (mirroring server/ocserverd/domain.go). */
  chatBudgetChars: number;
  /** T-8: N — how many database backup files rotation KEEPS. Everything past N
   * is DELETED from disk. Two things the number does not carry and the settings
   * copy therefore has to say: it counts VERSIONS, not days, and it is PER POOL
   * (routine vs pre-migration), so the directory holds up to 2 × N files.
   * Default and range in `backupRetain.ts` (mirroring server/ocserverd/backup.go). */
  backupRetain: number;
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
  /** The CLOSED failure vocabulary (T-0648) the cockpit translates — see
   * `onboardingReasonText`. "" on success, and on any report a server wrote
   * before the field existed; that case renders `reason` verbatim. */
  code: string;
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
  /** When the owner pressed 「不再顯示」 on the banner (unix seconds; 0 = never;
   * T-0648). It rides on the REPORT, not on the browser, which is what makes
   * the dismissal survive a new tab. A report row written before this field
   * existed has no stamp, and that absence reads as 0 — never dismissed. */
  dismissedAt: number;
}

/** Partial settings edit — only supplied fields change (server 422s a
 * owner_token_ttl or agent_token_ttl outside their whitelist / a handover_pct outside 40..90 / an
 * outsource_max_parallel outside -1..20; -1 = 無限 unlimited). */
export interface ServerSettingsPatch {
  ownerTokenTtl?: number;
  agentTokenTtl?: number;
  handoverPct?: number;
  noticePct?: number;
  codexNoticeRound?: number;
  codexCompactionThreshold?: number;
  monitoringRefreshSeconds?: number;
  /** 加速停止 grace in seconds. Must be 10..3600. */
  acceleratedGraceSecs?: number;
  outsourceMaxParallel?: number;
  /** T-ae38 document size caps, in characters. Each must be between THAT
   * segment's shipped default (`DOC_CAP_CHARS_DEFAULTS`) and 100000. */
  docCapCharsDuty?: number;
  docCapCharsInsight?: number;
  docCapCharsLearning?: number;
  docCapCharsManualSop?: number;
  docCapCharsManualLearnings?: number;
  /** T-791e boot-context caps, same range rule. Editable because the version
   * list judges an old revision against these — a cap the cockpit can read but
   * never write would leave the one number the marking depends on unreachable
   * from the only surface that edits settings. */
  docCapCharsSystemInteraction?: number;
  docCapCharsBootSequence?: number;
  docCapCharsOffboard?: number;
  /** T-c9b4 wake-snapshot chat budget; range 1000..13000 (chatBudget.ts). The
   * floor is NOT the shipped default — this one may be turned down. */
  chatBudgetChars?: number;
  /** T-8 backup retention N; range 1..20 (backupRetain.ts). Lowering it DELETES
   * the files it puts out of range on the next backup. */
  backupRetain?: number;
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
  /** Dismiss (true) or un-dismiss (false) the first-run onboarding banner
   * (T-0648) — it stamps / clears `dismissedAt` on the ONE onboarding report,
   * so 「不再顯示」 outlives the tab it was pressed in. 409 when there is no
   * banner to close: no onboarding report at all, or a report whose state is
   * not `failed`.
   *
   * 🔴 THE `running` REFUSAL IS ABOUT THE WRITE, NOT ABOUT THE STAMP. A stamp
   * laid on a still-`running` report could not silence anything anyway — both
   * paths that reach a terminal state rewrite the row with `dismissedAt` back
   * at 0. What is permanent is the write itself: server-side this is an
   * unlocked read-modify-write of the WHOLE report row, and the only writer
   * that can run CONCURRENTLY with the run (kick, finish and recoverStale are
   * three different goroutines that never run in parallel — boot, the
   * set-password request, and the goroutine that request spawns form one
   * happens-before chain), so interleaved with the run reaching its verdict it
   * writes back its pre-verdict copy — the failure is ERASED, the report is
   * stranded in `running` (non-terminal, so no banner draws) and first-run
   * onboarding never re-runs, because a report exists. */
  onboardingDismissed?: boolean;
}

/** Fields the owner may edit on a member (PATCH; every field optional).
 * `model` is a FREE string (spawn --model is a free string; "" ⇒ CLI default);
 * `effort` is the closed low/medium/high/max vocabulary (server 422s outside it).
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
 * / `effort` low|medium|high|max are the member's launch knobs (omitted ⇒ server
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

// ── 定期訊息 · scheduled messages (T-f059) ────────────────────────────────────

/** How often a scheduled message repeats. `weekly` reads `dayOfWeek`,
 * `monthly` reads `dayOfMonth`, `daily` reads neither — those three fire once a
 * day at the single wall-clock reading `hour`/`minute` names.
 *
 * `custom` (T-49e7) reads none of those four: it reads the four sets
 * `customMonths`/`customDays`/`customHours`/`customMinutes` and fires at EVERY
 * reading where all four hold at once, so it is the only cadence that can fire
 * more than once a day. Each set is EXPLICIT — "every day" means listing every
 * day; an empty set is a 422, never a silent "all" and never a silent
 * "never". */
export type ScheduleCadence = "daily" | "weekly" | "monthly" | "custom";

/** One recurring message bound to a member (view model of
 * `ScheduledMessageDTO`) — the clock-driven twin of a webhook endpoint: when a
 * wall-clock slot comes due the server delivers `body` down the ordinary chat
 * path. `status` is the reversible enable/disable toggle, NOT a lifecycle —
 * DELETE is the permanent removal.
 *
 * `lastFiredSlot` is the IDENTIFIER of the slot already delivered (e.g.
 * `2026-08-10T09:00+08:00`), never a "last run at" clock; `lastFiredTs` is the
 * human-facing time of the last ACTUAL delivery and takes no part in the
 * fire/skip decision. */
export interface ScheduledMessage {
  id: string;
  memberId: string;
  label: string;
  body: string;
  cadence: ScheduleCadence;
  /** 0=Sunday … 6=Saturday. Read only by `weekly`. */
  dayOfWeek: number;
  /** 1–31. Read only by `monthly`. A month without that day is SKIPPED whole
   * (RFC 5545), never clamped — so 29/30/31 silently miss some months. */
  dayOfMonth: number;
  hour: number;
  minute: number;
  /** Months of the year `custom` fires on (1–12), sorted and deduplicated.
   * Empty for every other cadence; never empty for `custom` — a row that
   * reaches this view model always LISTS its months, including the whole year.
   * (Absent-means-every-month is a rule of the CREATE/PATCH request only.) */
  customMonths: number[];
  /** Days of the month `custom` fires on (1–31), sorted and deduplicated.
   * EMPTY for every other cadence, and never empty for `custom`. Membership is
   * decided day by day: a listed day the month lacks is dropped for THAT DAY
   * only, so [1,15,31] in February fires on the 1st and the 15th. */
  customDays: number[];
  /** Hours of the day `custom` fires on (0–23), read in `timezone`. Empty for
   * every other cadence; never empty for `custom`. */
  customHours: number[];
  /** Minutes of the hour `custom` fires on (0–59), read in `timezone`. Empty
   * for every other cadence; never empty for `custom`. */
  customMinutes: number[];
  /** IANA zone name the wall-clock slot is computed in. */
  timezone: string;
  status: "enabled" | "disabled";
  lastFiredSlot: string;
  lastFiredTs: number;
  createdTs: number;
}

/** Create-form payload for a new scheduled message (mirrors
 * `ScheduledMessageCreateDTO`). `body`/`cadence`/`timezone` are REQUIRED
 * UNCONDITIONALLY — a defaulted timezone would sooner or later be read as
 * "wherever the server happens to run". The rest is required or ignored
 * ACCORDING TO `cadence`: `daily`/`weekly`/`monthly` need `hour`+`minute`
 * (omitting either is a 422, never a silent midnight), `custom` needs the four
 * sets instead — months being the one it may omit, see `customMonths` — and
 * never reads `hour`/`minute`/`dayOfWeek`/`dayOfMonth`. That
 * is why `hour`/`minute` are optional HERE and required in practice for the
 * calendar cadences — a `custom` schedule must not have to send two values it
 * never reads. `label` omitted = no label; `dayOfWeek` omitted = 0;
 * `dayOfMonth` omitted = 1. */
export interface ScheduledMessageCreateInput {
  label?: string;
  body: string;
  cadence: ScheduleCadence;
  dayOfWeek?: number;
  dayOfMonth?: number;
  hour?: number;
  minute?: number;
  /** 🔴 The ONE set whose ABSENCE carries a meaning: omitted on a `custom`
   * create means EVERY month (1–12), because a client written before round 2
   * never sends it and its schedules always did fire every month. An
   * explicitly EMPTY array is still a 422 — "never fires" and "always fires"
   * may not be one keystroke apart. So `undefined` and `[]` are two different
   * requests here, and nothing between this type and the wire may collapse
   * them. */
  customMonths?: number[];
  /** REQUIRED (and non-empty) when `cadence` is `custom`; ignored otherwise. */
  customDays?: number[];
  customHours?: number[];
  customMinutes?: number[];
  timezone: string;
}

/** Partial edit of a scheduled message (mirrors `ScheduledMessageUpdateDTO`):
 * only the supplied fields change. `status` flips the enable/disable toggle;
 * `id` and `memberId` are immutable and are not editable here. */
export interface ScheduledMessageUpdate {
  label?: string;
  body?: string;
  cadence?: ScheduleCadence;
  dayOfWeek?: number;
  dayOfMonth?: number;
  hour?: number;
  minute?: number;
  /** Switching a schedule TO `custom` must supply the day/hour/minute sets in
   * the SAME request unless the stored row already carries them; switching
   * AWAY leaves them stored and unread, so switching back does not lose the
   * choice.
   *
   * `customMonths` is the exception, and it is the same exception as on
   * create: omitting it on a switch-to-`custom` of a row that has never
   * carried months means EVERY month, while omitting it on a row that already
   * lists months means unchanged. `[]` remains a 422 either way. */
  customMonths?: number[];
  customDays?: number[];
  customHours?: number[];
  customMinutes?: number[];
  timezone?: string;
  status?: "enabled" | "disabled";
}

// ── Resume summary (RESUME SUMMARY panel section, T-8b0d) ─────────────────────

/** The size/概要 block of a resume snapshot (view model of
 * `ResumeOverviewDTO`) — the peek-then-decide counts/sizes: `chatCount`/
 * `chatChars` describe what THIS snapshot carries, `tasksOpenTotal` is ALL the
 * target's open tasks (may exceed the bounded `tasks` rows), `tasksDetailChars`
 * sums every returned row's `detailChars` (the plan text a full task pull would
 * load), `cardsWaiting`/`cardsAnsweredRecent` count the target's reply cards.
 * `stepsOnAnsweredCard` counts only the bounded task rows carried here; it is
 * a pointer count, not proof that a step is done. */
export interface ResumeOverviewView {
  chatCount: number;
  chatChars: number;
  tasksReturned: number;
  tasksOpenTotal: number;
  tasksDetailChars: number;
  cardsWaiting: number;
  cardsAnsweredRecent: number;
  /** Size of the roster block THIS snapshot carries (T-1b09). Reported
   * separately from `tasksDetailChars` on purpose: that one counts text the
   * snapshot does NOT carry. */
  rosterChars: number;
  /** Size of the machine block THIS snapshot carries (T-1b09). */
  machinesChars: number;
  /** Number of carried task steps whose latest card is answered while the step
   * is still in_progress. This is a pointer to work needing attention, not a
   * completion signal. */
  stepsOnAnsweredCard: number;
  /** Character cost of the answered-card pointers carried in this snapshot. */
  stepsOnAnsweredCardChars: number;
}

/** The CUT POINT of the snapshot's chat (view model of `ResumeChatCutDTO`):
 * whether whole messages exist that this payload does NOT carry, and how to go
 * and get them.
 *
 * 🔴 TRUNCATION, not collapse. `ChatMessage.bodyOmittedChars` reports a message
 * that IS here with part of its text folded away; this reports messages that
 * are NOT here at all. The panel must word the two differently — see the
 * `chatEarlierOmitted` / `bodyOmitted` i18n leaves. */
export interface ResumeChatCutView {
  /** true ⇒ at least one message involving the subject was left out. */
  omitted: boolean;
  /** How to retrieve what was cut, stated concretely by the SERVER. The panel
   * shows it VERBATIM — re-writing it here would be the cockpit inventing a
   * recovery procedure it cannot keep in step with the endpoint. "" when
   * nothing was cut. */
  hint: string;
}

/** One roster entry in the wake snapshot (view model of
 * `ResumeRosterMemberDTO`) — who else is in the studio and how to reach them.
 * `id` is what you address a message to; names are editable and roles repeat,
 * so the panel shows BOTH and never treats the name as an address. */
export interface ResumeRosterMemberView {
  id: string;
  name: string;
  /** `member` | `outsource` — permanent members vs disposable contractors. */
  kind: string;
  /** The role's display name. The wire carries NO `role_key` on this row —
   * only the name — so the panel shows what the server sends and does not
   * synthesise a key. "" for contractors, which carry no role. */
  roleName: string;
  /** The role's own definition text (capped server-side). "" for contractors,
   * which carry `currentTask` instead. */
  duty: string;
  /** The TITLE of the one task a contractor is bound to; "" for members. */
  currentTask: string;
  /** The bound task's status — contractors only. It is also what tells a
   * `0/0` progress apart: non-empty ⇒ a bound task with no steps yet, "" ⇒ no
   * bound task at all. */
  taskStatus: string;
  waitingReason: string;
  progressDone: number;
  progressTotal: number;
  /** Which machine that member runs on (live binding); "" when unbound. */
  machine: string;
  /** The online/offline status the roster block reports. */
  presence: string;
}

/** One machine in the wake snapshot's machine block (view model of
 * `ResumeMachineDTO`). `machineId` is the STABLE id — address a machine by id,
 * never by the name a host reports for itself. */
export interface ResumeMachineView {
  machineId: string;
  displayName: string;
  online: boolean;
}

/** The machine block of the wake snapshot (view model of `ResumeMachinesDTO`):
 * the machine LIST plus which one the subject is standing on. `youAreOn` is the
 * SERVER-RECORDED binding, "" when there is none yet. */
export interface ResumeMachinesView {
  list: ResumeMachineView[];
  youAreOn: string;
}

/** One pointer from a LIGHT task row to a reply card the owner has already
 * answered while the step remains in_progress. The card body is deliberately
 * absent; read it with `get_reply_card`. This never means the step is done —
 * the answer may require a change rather than approval. */
export interface ResumeAnsweredCardStepView {
  stepId: string;
  stepName: string;
  cardId: string;
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
  /** Steps whose answered reply card has not yet been acted on. Empty is the
   * honest normal case; non-empty is a pointer, never a done state. */
  answeredCardSteps: ResumeAnsweredCardStepView[];
  /** The handover hold (T-91): "" or "reassigning". The wake snapshot was the
   * one task projection that dropped it, so a ticket handed to this member
   * looked like a ticket it had been working on. */
  lock: string;
  /** The predecessor this ticket was last handed over from, "" when never
   * reassigned; `reassignedFromKind` says whether that id is a roster member
   * or an outsource worker. */
  reassignedFrom: string;
  reassignedFromKind: string;
  /** Ids of the still-open tickets waiting on THIS one (T-91) — the reverse of
   * `deps`. Nothing is messaged about it by owner ruling, so this row is the
   * whole delivery. */
  blocking: string[];
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
  /** When the snapshot was assembled, `YYYY-MM-DD HH:MM:SS ±HH:MM` in the
   * server's zone. It is the ONLY anchor that turns any `tsDisplay` in this
   * payload into "how long ago", so the panel shows it at the TOP: a reader
   * (agent or owner) has no reliable clock of its own to measure against. */
  generatedAt: string;
  /** The chat CUT POINT — whole messages this payload does not carry. */
  chatEarlierOmitted: ResumeChatCutView;
  /** Who else is in the studio (owner ruling rc-4e98c0481852: "All members and
   * contractors and their online / offline status"). Empty ⇒ the snapshot
   * carries no roster block. */
  roster: ResumeRosterMemberView[];
  /** The fleet the subject can reason about; null ⇒ no machine block. */
  machines: ResumeMachinesView | null;
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

/**
 * The health of the delta downlink itself, as the UI is allowed to see it.
 *
 * This exists because a dead downlink is otherwise INDISTINGUISHABLE from a
 * quiet one: both render as "no new anything". Publishing the state is what
 * turns a frozen cockpit from a silent lie into something the owner can see and
 * act on. See the shared-downlink block in api/http.ts for the transitions.
 *
 *   "idle"         — nobody subscribed (logged out / torn down). Not a fault.
 *   "connecting"   — no open stream right now; what is on screen may be stale.
 *   "live"         — open and delivering.
 *   "unauthorized" — the session is dead; retrying has STOPPED on purpose.
 */
/**
 * What a 成本歸零 destroyed, as the server read it immediately before the write.
 *
 * Null on a half means there was nothing to clear there — NOT that zero was
 * cleared. The distinction matters because it is the same null semantics the
 * cost READ side uses, so a caller keeps one rule for both.
 */
export type CostResetReceipt = {
  memberId: string;
  clearedCost: number | null;
  clearedBankedCost: number | null;
};

/**
 * What an ACCOUNT 歸零 destroyed: the account's OWN accumulated spend as it
 * stood immediately before the write.
 *
 * Nothing about any member appears here because nothing about any member
 * changed — the account figure and the per-member figures are cleared
 * independently (owner ruling rc-5c5d7c7c6dcd). Null means there was nothing to
 * clear, NOT that zero was cleared, the same rule the read side uses.
 */
export type AccountCostResetReceipt = {
  account: string;
  clearedCost: number | null;
};

export type SseConnectionState = "idle" | "connecting" | "live" | "unauthorized";

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
   * dispatches the robust STOP straight to the warden NOW (the warden SIGKILLs the
   * session). Not a shortcut past a countdown — the server arms none on this arm.
   * It is the LAST of the three things that end a soft offboard: the agent's own
   * report_stopped, the deadline the owner opens with acceleratedStopMember, and
   * this. Backs the cockpit's "Force stop" escalation, surfaced once a member is
   * already *stopping*. Does NOT flip online — the caller refetches; presence
   * surfaces stopped.
   */
  forceStopMember(id: string): Promise<void>;
  /**
   * 成本歸零: POST /api/members/{id}/cost/reset → clear ONE actor's estimated
   * spend, both halves at once (the durable banked figure AND the live
   * telemetry figure). Serves staff and outsource workers alike, a RELEASED
   * worker included (owner ruling rc-1344cc76a24a) — a worker that has left
   * still has a figure on screen, and the button beside it has to be able to
   * clear it. Only an id that resolves to nobody is a 404.
   *
   * It does NOT move the account card: since rc-5c5d7c7c6dcd that figure is an
   * accumulator of its own with its own button (resetAccountCost), which is why
   * the ruling above reads as being about account totals — that was true of the
   * model it was written under, one day earlier.
   *
   * 🔴 IRREVERSIBLE (owner ruling rc-7dea0deefa63). Nothing is retained and
   * there is no undo route — call it behind a confirm, never optimistically.
   *
   * Resolves with a RECEIPT of what was destroyed, read immediately before the
   * write, because that response is the last moment those numbers exist
   * anywhere. Null on a half means there was nothing to clear there, NOT that
   * zero was cleared — the same null semantics the read side uses, so the
   * caller reuses one summing rule. After the reset the 估計$ cell falls back
   * to the dash on its own; the caller refetches.
   */
  resetMemberCost(id: string): Promise<CostResetReceipt>;
  /**
   * 帳號歸零: POST /api/accounts/cost/reset → set ONE account's own accumulated
   * spend back to 0 (owner ruling rc-5c5d7c7c6dcd「分開：帳號卡自己一份數字，清它
   * 不動成員」).
   *
   * 🔴 It touches NO member or worker figure. Since that ruling the account card
   * is not a fold over the actors on the account — it is an accumulator of its
   * own, fed by the increase each telemetry report brings — so this clears the
   * card and leaves every 估計$ underneath it exactly where it was.
   *
   * IRREVERSIBLE: nothing is retained and there is no undo route, so call it
   * behind a confirm. Resolves with a RECEIPT of the figure destroyed, which is
   * the last moment it exists; null means there was nothing to clear. An account
   * nobody has reported under is not an error — the same 200 with null — so a
   * second press reads as honest rather than broken. Refetch monitoring after
   * it: the card is folded from that read.
   */
  resetAccountCost(account: string): Promise<AccountCostResetReceipt>;
  /**
   * 加速停止 (accelerated stop): POST /api/members/{id}/accelerated-stop → put a
   * wind-down that is ALREADY OPEN on the server's stop.accelerated_grace_secs
   * clock and TELL the member (the write fans an offboard notice whose sentence
   * now quotes a deadline).
   *
   * The MIDDLE rung of 停止 → 加速停止 → 強制停止 (owner 2026-08-21). It
   * ESCALATES; it does not initiate: a member nobody has asked to stop is a 409,
   * because a clock on a member that was told nothing is a deadline it never
   * heard about. So is a member with no live session, and one already cut off by
   * 強制停止. Does NOT flip online — the caller refetches.
   */
  acceleratedStopMember(id: string): Promise<void>;
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
  /** List a member's scheduled messages, oldest→newest
   * (`GET /api/members/{id}/scheduled-messages`, T-f059). The member may be an
   * assistant OR an `ow-` outsource worker — the recipient rule ordinary chat
   * uses. */
  listScheduledMessages(memberId: string): Promise<ScheduledMessage[]>;
  /** Create a scheduled message on a member
   * (`POST /api/members/{id}/scheduled-messages`). The delivery cursor is
   * seeded to the slot most recently elapsed, so a fresh schedule never fires
   * on the spot. */
  createScheduledMessage(
    memberId: string,
    input: ScheduledMessageCreateInput,
  ): Promise<ScheduledMessage>;
  /** Edit a scheduled message, including the enable/disable toggle
   * (`PATCH /api/members/{id}/scheduled-messages/{scheduleId}`). Re-aiming any
   * cadence/slot field moves the cursor to the slot most recently elapsed, so
   * an edit never fires the slot it crosses. */
  updateScheduledMessage(
    memberId: string,
    scheduleId: string,
    patch: ScheduledMessageUpdate,
  ): Promise<ScheduledMessage>;
  /** Permanently remove a scheduled message
   * (`DELETE /api/members/{id}/scheduled-messages/{scheduleId}`) — distinct
   * from `status: disabled`, which is the reversible suspend. */
  deleteScheduledMessage(memberId: string, scheduleId: string): Promise<void>;
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
   * A page shorter than `limit` means the history is exhausted.
   *
   * READ-ONLY ON EVERY PATH (T-48): listing a conversation advances NO read
   * watermark — not the newest window, not a history page. Marking a
   * conversation read is `markChatRead` and nothing else, so a caller that
   * must keep the thread fresh WITHOUT consuming the unread badge (a
   * backgrounded window) just calls this. The `peekChat` twin that used to
   * carry that case was merged into this method in T-48. */
  listChat(
    withId: string,
    limit?: number,
    before?: ChatCursor,
  ): Promise<ChatMessage[]>;
  /** One ANCHOR WINDOW of the conversation (`?start_id=` / `?end_id=`,
   * T-48 ③), oldest→newest, READ-ONLY like every other read door here.
   *
   * 🔴 WHY THIS EXISTS AND `listChat` COULD NOT DO IT. "跳到原訊息" is handed a
   * message id and nothing else. The only cursor this API used to have walks
   * BACKWARDS from a (ts, id) the caller must already hold, so a target older
   * than the loaded window was unreachable: the cockpit looked for the row in
   * the DOM, did not find it, and scrolled to the bottom — which is exactly
   * what a successful jump to a recent message looks like. The two ends here
   * are the two halves of that jump: `endId` fetches the context ABOVE the
   * target, `startId` the context BELOW it, and neither pulls the whole
   * history to get there.
   *
   * REJECTS on an id no message carries (404 — deliberately NOT an empty page,
   * because an empty page is what a real window at the end of the stream
   * returns and the two must stay distinguishable). See {@link ChatAnchor}. */
  listChatWindow(
    withId: string,
    anchor: ChatAnchor,
    limit: number,
  ): Promise<ChatMessage[]>;
  /** Read back ONE named message in full (`GET /api/chat?ids=<id>`), with NO
   * read-watermark side effect. Rejects when the id names nothing (the server
   * refuses the whole call) and when the read fails.
   *
   * 🔴 THIS IS NOT THE MACHINE THAT WAS DELETED, AND THE DIFFERENCE IS THE WHOLE
   * POINT. Until 2026-08-21 the thread carried a background REFETCHER for quoted
   * messages: it ran on its own, decided for itself which ids were still owed,
   * kept that debt across renders and peers, retried, and repaired an earlier
   * wrong answer when a later event arrived. That shape is gone and must not
   * come back — three of its states ("fetched", "missing", "not asked yet") drew
   * the same pixels, so a wrong answer looked exactly like a right one.
   *
   * What this is instead:
   *   • it happens ONLY because a person clicked something;
   *   • it asks for ONE message, once, and the answer is used immediately;
   *   • a failure is said out loud where the click happened and then forgotten
   *     — no retry, no queue, no state that outlives the click.
   * There is deliberately no batching, no cache and no id set. If you find
   * yourself adding one, you are rebuilding the deleted machine.
   *
   * "One click, one call" is pinned by `ChatArea.quote-no-fetch.test.tsx`'s
   * "a click on the quote costs exactly one request, and repainting costs none";
   * the failure half — said once, never retried — is
   * `ChatArea.reply-to.test.tsx`'s "says so, in place and once, when that one
   * read fails". (`quote-no-fetch`'s api proxy deliberately registers no failure
   * state, which is why the two halves live apart.) Together they are what stands
   * between this and the thing it replaced. */
  getChatMessage(id: string): Promise<ChatMessage>;
  /** The M2 gallery query (`GET /api/chat/attachments?with=<memberId>`): every
   * attachment of the member's conversations, flattened newest→oldest —
   * owner↔member BOTH directions AND the member's inter-agent threads — each
   * row carrying the sender id + server-resolved display name + send time.
   * READ-ONLY, like every read door on this API since T-48 — the only thing
   * that advances a read watermark is `markChatRead`. */
  listChatAttachments(withId: string): Promise<GalleryAttachment[]>;
  /** Mint the share link for one attachment
   * (`GET /api/chat/attachments/{id}/share-link`): resolves to the blob's
   * server-relative serve path carrying its `?sig=` file-level HMAC credential
   * — anyone holding the URL may read exactly this one blob, nothing else, no
   * expiry. It is NOT permanent: the sig is derived from the key that signs at
   * mint time, so removing that key from the signing-key ring voids it, along
   * with every other link that key signed (T-62). Callers prefix the page
   * origin to form the absolute, sendable URL. Unknown id → 404 (throws). */
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
    /** The id of the message this post REPLIES TO, when it is a reply. The
     * server checks it EXISTS — and nothing else: a reply may quote a message
     * out of another conversation (owner ruling, 2026-08-21). The server is the
     * only writer of the stored link; a forged `meta.reply_to` is dropped.
     * Omitted on an ordinary post. */
    replyTo?: string;
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
   * within the last 24 hours, newest answer first; `expired` returns cards that
   * were marked expired within the last 24 hours, newest first (older
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
   * LIST and/or free text (+ attachments); empty → 400 (and `optionIdxs: []`
   * IS empty), an out-of-range index → 400, more than one index on a `single`
   * card → 400, already answered → 409 (all reject as ApiError). Returns the answered
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
   * the terminal exit that is NOT an answer; its author, the owner, or an admin
   * agent may press it — T-1b88). No body, no undo, no
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
  /**
   * Fetch ONE step in full (`GET /api/tasks/{task_id}/steps/{step_id}`, T-66) —
   * the only read that carries a step's working-note TEXT.
   *
   * 🔴 It exists because `getTask` stopped carrying it. The task read reports
   * each step's `noteSizeChars` and nothing else, so the 任務卡 draws the 備註
   * entry from that number and calls this ONLY when the reader opens one —
   * owner rc-4c8065fb30a5:「座艙改成點開才抓」. Do not call it per step while
   * rendering a timeline; that is the cost the split removed.
   *
   * A step id that belongs to a different task is a 404 (ApiError), not another
   * task's step.
   */
  getTaskStep(taskId: string, stepId: string): Promise<TaskStepDetailView>;
  /**
   * Fetch ONE task's pinned deliverables in full
   * (`GET /api/tasks/{task_id}/artifacts`, T-66) — the only read that carries
   * an artifact ROW at all: kind / url / name / description / mime /
   * createdTs / createdBy / versionCount.
   *
   * 🔴 It exists because `getTask` stopped carrying them, and T-92 finished the
   * job: a task response has no `artifacts` field of any kind now, only
   * `artifactCount` (owner c-cd063427fb2f started this as an id+label index;
   * T-92 removed even the index). So anything that DRAWS an artifact — or that
   * needs one artifact's id — calls this. ONE call answers the WHOLE ticket — there is
   * deliberately no per-artifact read (owner c-f2d0fecb1168:「應該是指名任務？」),
   * because the cockpit's deliverables panel opens onto the entire set and a
   * per-artifact door would cost one call per row.
   *
   * An unknown task id is a 404 (ApiError); a task with nothing pinned is [].
   */
  listTaskArtifacts(taskId: string): Promise<TaskArtifactView[]>;
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
   * tasks are a 409 (throws). The write answers with a bounded receipt
   * (T-a98d), so nothing is returned here — refetch, or take the SSE delta.
   */
  setTaskPriority(id: string, priority: string): Promise<void>;
  /**
   * Correct one task's description (`POST /api/tasks/{id}/description`, T-e271)
   * — the ticket's own text: what the task IS (scope, origin, acceptance), as
   * opposed to a step note's "where this step is right now".
   *
   * 🔴 A CLOSED task is NOT a 409 here, unlike every other task write on this
   * interface (priority, artifacts, reassign, steps all refuse a terminal
   * task). That is the server's deliberate asymmetry, not an oversight on this
   * seam: a ticket worded wrongly is usually found to be wrong AFTER it closed,
   * and freezing the text would keep a known falsehood in the permanent record,
   * while the artifact set stays frozen because it records what the task
   * PRODUCED. Do not "align" this by refusing terminal tasks in the UI — the
   * server accepts them and the cockpit would be lying about what it can do.
   *
   * The write is wholesale within that one field: `description` replaces
   * whatever was there and `""` clears it. The stored value is TRIMMED of
   * surrounding whitespace, and the server compares AFTER trimming, so
   * re-sending a description with a stray trailing space is correctly seen as
   * no change. 🔴 That trim arrived in T-646a (owner card rc-0fb94a25a8a8,
   * option ①) — before it this field was stored raw while its title twin
   * trimmed, and closing that gap is what the ticket was for. Its CONSEQUENCE:
   * a description of only whitespace trims to `""` and therefore CLEARS.
   *
   * 🔴 The agent-facing MCP tool for this is no longer `update_task_description`
   * — since T-646a it is `update_task`, which writes title and description
   * together through one seam. This ROUTE is unchanged and stays here for the
   * cockpit; only the tool surface moved.
   *
   * Every change that actually alters the text retains the previous one as a
   * `task_description` revision keyed on the task id, readable through
   * listDocumentHistory. Returns the task after the change; the SSE `task`
   * delta also fans.
   */
  updateTaskDescription(id: string, description: string): Promise<TaskView>;
  /**
   * Correct one task's title (`POST /api/tasks/{id}/title`, T-2ebe) — the ONLY
   * cell of a task the task list renders, and so the half of a card most likely
   * to be read alone. Before this route existed a card whose scope was later
   * narrowed kept advertising its first wording forever while the description
   * corrected itself, and the two ended up contradicting each other.
   *
   * The description twin's exact shape — same executor-or-admin gate (403
   * otherwise; the creator gets no standing), same 404 on an unknown task, and
   * the same deliberate acceptance of a CLOSED task (see that method's note at
   * length; do not "align" it by refusing terminal tasks in the UI).
   *
   * 🔴 ONE DIFFERENCE, and it is a difference in kind rather than an oversight:
   * an explicit BLANK title (empty or whitespace-only) is a 400 `title must not
   * be blank`, NOT a clear. `create_task` refuses a blank title on the same
   * terms, and an edit door looser than the create door would let a caller
   * reach a task-list row with nothing in it. (Trimming used to be a second
   * difference; since T-646a both fields are trimmed, so the blank rule is the
   * only one left.)
   *
   * 🔴 The agent-facing MCP tool for this is no longer `update_task_title` —
   * since T-646a it is `update_task`. This ROUTE is unchanged and stays here for
   * the cockpit; only the tool surface moved.
   *
   * There is NO length cap, on this door or on create. Every change that
   * actually alters the text retains the previous one as a `task_title`
   * revision keyed on the task id — a series separate from `task_description`
   * over that same key. Returns the task after the change; the SSE `task` delta
   * also fans.
   */
  updateTaskTitle(id: string, title: string): Promise<TaskView>;
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
   * agent PINS via MCP but does not remove). The write answers with a bounded
   * receipt (T-a98d), so nothing is returned here — refetch, or take the SSE
   * delta. Unknown task/artifact → 404, wrong-task → 400 (both throw
   * ApiError). The live blob is left intact, but every retained version of the
   * artifact is deleted with it, along with the blobs only those versions used.
   */
  removeTaskArtifact(taskId: string, artifactId: string): Promise<void>;

  /**
   * List the retained PREVIOUS versions of one pinned deliverable, newest
   * first (`GET /api/tasks/{taskId}/artifact/{artifactId}/history`, T-60) —
   * cockpit-only, and deliberately not an MCP tool.
   *
   * READ-ONLY BY DESIGN: there is no restore face anywhere on this seam. An
   * older version comes back by replacing FORWARD with it, which is the
   * executing agent's write, not the cockpit's.
   *
   * An artifact that has never been replaced answers with an empty list — the
   * honest "nothing has been replaced here". Unknown task/artifact → 404,
   * wrong-task → 400 (both throw through the shared envelope).
   */
  listTaskArtifactVersions(
    taskId: string,
    artifactId: string,
  ): Promise<TaskArtifactVersionView[]>;
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
  /** Stop a worker (`POST /api/outsource-workers/{id}/stop`, owner/admin-agent) — the
   * FIRST rung of 停止 → 加速停止 → 強制停止 and, since T-ed79, a GRACEFUL
   * CLOSE-OUT rather than a kill (owner 2026-08-21 「往正職靠：外包那顆改成優雅
   * 停止」): it holds the worker down (desired offline, presence
   * "stopping"/"stopped", no auto-revival), shows it the 〈停止〉 and WAITS for
   * its own report_stopped. No deadline unless the owner escalates. The bound
   * task stays put. Idempotent; unknown/released → 404. (T-f190, T-ed79) */
  stopWorker(id: string): Promise<OutsourceWorkerView>;
  /** 加速停止 a worker (`POST /api/outsource-workers/{id}/accelerated-stop`,
   * owner/admin-agent) — the MIDDLE rung. Puts the wind-down that is ALREADY open
   * (a 停止 or a 換手) on the server's `stop.accelerated_grace_secs` clock and
   * TELLS the worker; it is not a kill, so the worker can still finish early.
   * 409 when nothing is winding down, when the worker is offline/released, or
   * when it was force-stopped. (T-ed79) */
  acceleratedStopWorker(id: string): Promise<OutsourceWorkerView>;
  /** 強制停止 a worker (`POST /api/outsource-workers/{id}/force-stop`,
   * owner/admin-agent) — the THIRD rung, and the body /stop used to have: kill the
   * session NOW and hold it down. It says NOTHING to the worker (the recipient is
   * about to stop existing). Idempotent; unknown/released → 404. (T-ed79) */
  forceStopWorker(id: string): Promise<OutsourceWorkerView>;
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
  /** List the manuals as a DIRECTORY (`GET /api/task-manuals`) — the 任務手冊
   * list page (type cards: 類型名 + 用途摘要). 出廠不含任何類型 (honest empty
   * list). T-1170: `sop_md` / `learnings` are NOT in this answer, only their
   * sizes; the body comes from `getTaskManual`. */
  listTaskManuals(): Promise<TaskManualSummaryView[]>;
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
  /** GET /api/auth/signing-keys — the ring, oldest first (T-62, owner-gated). */
  getSigningKeys(): Promise<SigningKeyView[]>;
  /** POST /api/auth/signing-keys/rotate — add a key and hand signing to it.
   * Nothing is revoked; every existing key keeps verifying. Answers the ring
   * AFTER the rotation, so no caller re-fetches to learn where it stands. */
  rotateSigningKey(): Promise<SigningKeyView[]>;
  /** POST /api/auth/signing-keys/{keyId}/remove — REVOKE everything that key
   * signed. No undo, no grace period. Answers the ring after the removal. */
  removeSigningKey(keyId: string): Promise<SigningKeyView[]>;
  /** The folded global-context doc (owner overlay ⊕ file seed). */
  getGlobalContext(): Promise<GlobalContextView>;
  /** Whole-doc replace of the global context → returns the folded doc
   * (`isDefault` flips false). */
  saveGlobalContext(text: string): Promise<GlobalContextView>;
  /** Reset the global context to seed (idempotent tombstone → `isDefault` true). */
  resetGlobalContext(): Promise<GlobalContextView>;
  /**
   * The folded boot-context / lifecycle document (T-791e, widened by T-3201),
   * addressed by (kind, key). Every kind serves exactly one key, "global",
   * except `boot_sequence`, which serves "claude" and "codex".
   *
   * 🔴 The two boot_sequence keys are DIFFERENT DOCUMENTS whose third step
   * means opposite things. `key` is required rather than defaulted for exactly
   * that reason: there is no "the boot sequence", so there is nothing sensible
   * for a default to pick, and an omitted key would silently address one
   * runtime while the caller meant the other.
   *
   * `readOnly` on the answer says the server SHOWS this document but refuses
   * every write to it (405). It is a property of the document, read here — the
   * cockpit keeps no list of which ones those are.
   */
  getBootDoc(kind: BootDocKind, key: string): Promise<BootDocView>;
  /** Replace the EDITABLE HALF of ONE boot-context block → the folded doc
   * (`isDefault` flips false).
   *
   * 🔴 IT TAKES `body`, NOT THE DOCUMENT (T-3201). The read-only head is not
   * something this call can get wrong — there is no field for it, and the
   * server joins the shipped one back on. Send back the `body` the read gave
   * you, changed; never `text`.
   *
   * Rejects with a 400 ApiError when the STORED result is over that kind's
   * `cap_chars` and not getting shorter — the cockpit blocks first, this is the
   * server's own floor — and with a 405 for a read-only document. Requires
   * admin or above. */
  saveBootDoc(
    kind: BootDocKind,
    key: string,
    body: string
  ): Promise<BootDocView>;
  /**
   * Restore ONE boot-context block to its FACTORY version → the folded doc
   * (`isDefault` true).
   *
   * 🔴 This is the recovery path for the failure this whole surface risks: a
   * broken boot sequence means agents never attach to SSE, so they never come
   * online, so there is nobody online to fix it from. It must stay reachable
   * from the cockpit without a successful read and without any agent being up.
   */
  resetBootDoc(kind: BootDocKind, key: string): Promise<BootDocView>;
  /** List the role roster as a DIRECTORY (seed defaults + owner edits).
   * T-1170: `definition_md` is NOT in this answer, only its size and the cap;
   * the persona body comes from `getRole`. */
  listRoles(): Promise<RoleSummaryView[]>;
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
   * effort outside low/medium/high/max.
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
   * Preview a member's initial boot prompt from /api/bootstrap — 系統互動 ⊕
   * global context ⊕ role definition ⊕ insight ⊕ lessons ⊕ 啟動步驟, every
   * document FOLDED (the owner's edit wins, the seed is what an unedited
   * installation folds to). Pass the ROLE key (NOT a member_id) so the server
   * mints NO token: a UI preview must never receive an agent credential
   * (§3.4 #29 — member_id is the warden-spawn path). ⚠️ That same omission is
   * why the reply carries the CLAUDE 啟動步驟 whatever runtime the member on
   * screen runs: with no member the server has no runtime to resolve (T-30e4).
   */
  getBootstrap(role: string): Promise<BootstrapView>;
  /**
   * The folded PER-ROLE lessons doc for a `roleKey`. `roleKey` is the WHOLE
   * address — T-2 removed the `task_type` axis. Agents sharing a role share
   * the accumulated lessons.
   */
  getLessons(roleKey: string): Promise<LessonsView>;
  /**
   * Whole-doc replace of the PER-ROLE lessons for a `roleKey` →
   * returns the folded doc (`isDefault` flips false). Backend contract is POST
   * (NOT the PUT/DELETE the global-context save uses). WRITE authz is per-role
   * and keyed on the PRINCIPAL CLASS, not the token scope (T-5336): a caller at
   * or above admin_agent — the owner (this UI's scope) and the admin agent —
   * may write ANY role; every other agent may write only its own role.
   */
  saveLessons(roleKey: string, text: string): Promise<LessonsView>;
  /**
   * The folded PER-ROLE insight doc for a `roleKey` (T-3809) — the role
   * journal's third block, beside Duty and Learning. No file seed, so an
   * untouched doc reads as genuinely empty.
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
   * Reset the PER-ROLE insight doc back to its factory seed (T-6501) —
   * idempotent tombstone → the folded read is `seeds/insight_<role_key>.md`
   * again and `isDefault` flips true. The counterpart of `resetRole` on Duty.
   *
   * A role with NO seed file 404s (throws): there must be a factory version to
   * reset TO. The cockpit only offers this where a seed exists, so the guard is
   * about keeping every implementation of this port honest, not about a path
   * the UI walks.
   */
  resetInsight(roleKey: string): Promise<InsightView>;
  /**
   * The retained revisions of ONE editable long-form document as a DIRECTORY,
   * newest first (`GET /api/document-history/{kind}/{key}`). At most 3 are
   * kept — the server prunes, the cockpit never has to.
   *
   * 🔴 T-1170: no `content`. Identity, actor, timestamp, the tombstone flag,
   * and each field's size — enough to draw the picker and to mark a revision
   * the server would refuse, and nothing more. Reading a revision means naming
   * it: `getDocumentRevision`.
   */
  listDocumentHistory(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentHistoryEntryView[]>;
  /**
   * The BODY of ONE named retained revision (T-1170). This is the only read
   * that carries a revision's text, and it is deliberately per-revision: the
   * reader opens exactly one at a time, so downloading three documents to show
   * one was paying for two nobody asked for.
   *
   * It answers `content` and nothing else about the revision — actor, time,
   * tombstone flag and sizes come from the directory row the reader opened.
   *
   * A pruned / unknown id rejects (404) — the caller says so rather than
   * rendering the revision as empty, which is a different and false claim.
   */
  getDocumentRevision(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentRevisionView>;
  /**
   * The document's SHIPPED DEFAULT — the version list's 初始版本 row
   * (`GET /api/document-history/{kind}/{key}/seed`, T-40f0).
   *
   * READ-ONLY: this is what makes 初始版本 comparable BEFORE its restore, which
   * is the one restore in the list that throws away everything the owner ever
   * wrote. Rejects with a 404 `ApiError` for a document that has no default
   * (a custom role, a task manual, per-role lessons) — the same documents whose
   * reset the server 404s, and the same ones whose 初始版本 row is not drawn.
   */
  getDocumentSeed(kind: DocumentKind, key: string): Promise<DocumentSeedView>;
  /**
   * BOTH SIDES of one comparison, in ONE answer (`GET /api/diff`, T-59).
   *
   * The compare screen is addressed by a URL now, not by an attachment, and
   * this is the read behind it: hand it the two addresses the URL spelled and
   * it answers each side's text, the heading for its column, and whether the
   * address resolved to nothing at all.
   *
   * ONE call, not two, and no per-side resolution on this side of the wire:
   * a reader that resolved "current" itself would be a second authority on
   * what a side IS, and the two would drift.
   *
   * `params.sig` is the server-minted signature the EXTERNAL flavour of the URL
   * carries; with it the call is answered with no session at all, which is why
   * its 401 must not be read as an expired login (see api/diff.ts).
   */
  getDiff(params: DiffParams): Promise<DiffPairView>;
  /**
   * Mint the EXTERNAL link to one comparison (`GET /api/diff/share-link`,
   * T-59) — the same `/diff` page url plus the server's `?sig=`, which opens it
   * for a reader who has no account at all.
   *
   * Server-RELATIVE, exactly like `getChatAttachmentShareLink`: only the
   * browser knows the public origin, so the caller absolutizes
   * (`lib/shareLink.ts`).
   *
   * `params.sig` is IGNORED — a signature is what this call produces, never an
   * input to it. Requires a session: it is gated like every other route here,
   * which is why the control that calls it is only ever drawn where one is
   * certain (see components/DiffShareLinkButton.tsx).
   */
  getDiffShareLink(params: DiffParams): Promise<string>;
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
   * PUBLIC pre-auth probe (`GET /api/auth/status`). `passwordSet` branches
   * first-run setup vs login; `mfaRequired` tells the login wall whether to
   * collect a TOTP code.
   *
   * It returns BOTH bits rather than just the first because the wall has to
   * render the right fields before anyone holds a token. The alternative — a
   * distinguishable "password ok, code missing" refusal from /api/login —
   * would leak strictly more (it confirms a correct password).
   */
  getAuthStatus(): Promise<AuthStatusView>;
  /**
   * Read the owner's second-factor state (`GET /api/auth/mfa`, owner-gated):
   * whether this server OFFERS the factor, and whether one is armed.
   *
   * Deliberately NOT a field on `getSettings`: that route's floor is
   * admin_agent and its GET is an MCP tool, so the owner's credential posture
   * would be readable by every agent in the office.
   */
  getMfaState(): Promise<MfaStateView>;
  /**
   * Turn the second-factor FEATURE on or off (`POST /api/auth/mfa/offer`,
   * owner-gated). A rollout switch only: turning it off hides the set-up path
   * but never disarms an armed factor, never stops login demanding a code, and
   * never blocks `disableMfa`.
   */
  setMfaOffered(offered: boolean): Promise<MfaStateView>;
  /**
   * Begin TOTP enrolment (`POST /api/auth/mfa/enroll`, owner-gated). Returns
   * the PENDING secret + otpauth URI once; nothing is armed until
   * `activateMfa` proves a code from it. Rejects 409 if a factor is already
   * active (rotation must disable first).
   */
  enrollMfa(): Promise<MfaEnrollView>;
  /**
   * Arm the second factor (`POST /api/auth/mfa/activate`, owner-gated) by
   * proving BOTH the current password and a code from the pending secret.
   *
   * The password is required because ARMING is as destructive as removing: a
   * stolen owner token alone could otherwise enrol a secret the attacker
   * controls and activate it, locking the real owner out until someone runs
   * `ocserverd mfa-disable` on the host. Rejects 401 on a wrong password OR
   * code — indistinguishably, so callers must name both — and 409 when a factor
   * is already active or nothing is pending.
   */
  activateMfa(password: string, code: string): Promise<void>;
  /**
   * Disarm the second factor (`POST /api/auth/mfa/disable`, owner-gated).
   * Requires BOTH the current password and a live code — an owner-gated
   * session alone must not be able to strip the factor. Rejects 401 on either,
   * 409 when nothing is armed.
   */
  disableMfa(password: string, code: string): Promise<void>;
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
   * immediately (owner_token_ttl from the next login, agent_token_ttl from the
   * next agent or worker mint, handover_pct from the next
   * context report). Returns the settings after the change.
   */
  patchServerSettings(patch: ServerSettingsPatch): Promise<ServerSettingsView>;
  /**
   * Fetch a theme bundle from a LINK (`POST /api/theme/fetch`) — T-29c7.
   * Returns the RAW bundle text, which the caller feeds to
   * `parseImportedBundle`, the same validator a pasted or file-picked bundle
   * goes through. Doing the parse here would create a second import path that
   * could accept different things from the paste box.
   *
   * The server checks the ADDRESS for format only (absolute http/https) and
   * checks the ANSWER properly (JSON + the shared theme-bundle validator).
   * 🔴 It deliberately does NOT constrain where the link points — no host
   * allowlist, no private-address refusal — per an explicit owner ruling
   * (2026-08-03) made after the risk was spelled out. Do not "align" this by
   * adding a client-side origin check: the cockpit would refuse links the
   * server accepts, and the owner would meet a rule nobody decided.
   *
   * Rejects (all throw ApiError, message surfaced inline): 422 for a malformed
   * url, an over-size body, or content that is not a valid theme; 502 when the
   * link itself cannot be reached or answers non-200.
   */
  fetchThemeFromLink(url: string): Promise<string>;
  /**
   * List the owner's saved custom themes (`GET /api/themes`) — T-83ef.
   *
   * 🔴 id + name ONLY, never the bundles. A theme embeds its images, so the
   * whole set is hundreds of KB to MB; serving that from a second door would
   * reproduce the very problem this resource was split out to fix. Anything
   * about ONE theme — applying it, editing it, exporting it — goes through
   * {@link getTheme}.
   */
  listThemes(): Promise<ThemeListItem[]>;
  /**
   * Read ONE saved custom theme IN FULL (`GET /api/themes/{theme_id}`) — the
   * per-item read that makes "edit one theme" possible without pulling every
   * bundle and every embedded image with it.
   *
   * Unknown id → 404, thrown as an `ApiError` (the same convention every other
   * per-id read on this seam follows — e.g. getTaskManual / getTask). It never
   * answers null: "no such theme" is a rejection, not an empty value.
   */
  getTheme(id: string): Promise<ThemeBundle>;
  /**
   * Create or replace ONE custom theme (`PUT /api/themes/{theme_id}`) — the
   * write this split exists to make expressible: before it, changing a single
   * colour meant re-sending EVERY theme with EVERY embedded image.
   *
   * The bundle is filed under its OWN `id`, which is what this method puts in
   * the path — so the server's "path id must equal the bundle's id" rule
   * (422 otherwise) cannot be violated from here by construction. A replace
   * KEEPS the theme's position in the owner's list.
   *
   * Answers the small {@link ThemeWriteReceipt}, NOT the bundle echoed back.
   * Rejects (throw `ApiError`, nothing written): 422 when the bundle fails the
   * shared validator (shape / theme.css token whitelist / concrete-colour
   * grammar / image gates), or when CREATING while the saved set is already at
   * its cap (a replace is not capped). The ONE thing that is not a 422 is an
   * unrecognised `wording` code — the server DROPS it and the write succeeds.
   */
  putTheme(bundle: ThemeBundle): Promise<ThemeWriteReceipt>;
  /**
   * Delete ONE custom theme (`DELETE /api/themes/{theme_id}`).
   *
   * Unknown id → 404, thrown as an `ApiError` — same convention as
   * {@link getTheme}. Deleting the ACTIVE theme resets `display_theme` to `""`
   * in the SAME request; the receipt's `displayThemeReset` reports that, so the
   * caller learns its theme changed without re-reading settings.
   */
  deleteTheme(id: string): Promise<ThemeDeleteResult>;
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
  /**
   * Watch the health of the delta downlink (see `SseConnectionState`). Fires
   * IMMEDIATELY with the current state, then on every change. Returns an
   * unsubscribe function.
   *
   * The UI's contract with this is the point of the whole method: when the
   * downlink is not live, SAY SO. A transport that cannot go down (the mock)
   * reports "live" once and never calls back — a subscriber must therefore work
   * from a single synchronous call and never wait for a second one.
   */
  subscribeConnection(
    onState: (state: SseConnectionState) => void
  ): () => void;
}
