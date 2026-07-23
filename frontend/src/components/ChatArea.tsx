import {
  Fragment,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useI18n } from "../i18n";
import type { Member, MemberActivateResult } from "../types";
import type {
  ChatMessage,
  ChatAttachmentView,
  OutsourceWorkerView,
} from "../api/adapter";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { autosizeTextarea } from "../lib/autosize";
import { getChatDraft, saveChatDraft } from "../lib/chatDraftStore";
import { useChat } from "../hooks/useChat";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { formatDayLabel, splitByDay } from "../lib/dateFormat";
import {
  ATTACH_ACCEPT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import { useWindowActive } from "../hooks/useWindowActive";
import { AttachmentStrip, Lightbox } from "./AttachmentStrip";
import { Avatar } from "./Avatar";
import { ChatGalleryPanel } from "./ChatGalleryPanel";
import { ChatReplyCard } from "./ChatReplyCard";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { Markdown } from "./Markdown";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { PresenceBadge } from "./PresenceBadge";
import { CurrentTaskTitle } from "./CurrentTaskTitle";
import {
  BoltIcon,
  CheckIcon,
  ChevronRightIcon,
  CopyIcon,
  ImageIcon,
  MoonIcon,
  PaperclipIcon,
  SendIcon,
  TasksIcon,
  UserGearIcon,
} from "./icons";
import { DispatchAlert } from "./DispatchAlert";

// The owner's sender id. The real backend stamps a message's `from` from the
// verified JWT `sub`; the owner token's sub is the fixed owner id ("owner")
// ("owner"), so the owner's own messages arrive with from="owner"
// (NOT "ceo"). The mock stamps the same (MOCK_OWNER_ID) so a message reads as
// "me" (right-aligned, from=你) in BOTH mock and real mode.
const OWNER_ID = "owner";

/** A message is INTER-AGENT (agent↔agent) when NEITHER endpoint is the owner:
 * owner↔agent always has the owner as one side; agent↔agent never does. This is
 * the whole test — it needs no role lookup and matches "both sender & recipient
 * are agents, neither is owner". These messages surface in BOTH participants'
 * threads (the backend's `?with=<id>` filter is bidirectional) but render
 * COLLAPSED by default so the owner isn't flooded. */
function isInterAgent(m: ChatMessage): boolean {
  return m.from !== OWNER_ID && m.to !== OWNER_ID;
}

/** A contiguous run of same-kind messages. Consecutive inter-agent messages fold
 * into one collapsible `"inter"` group (identified by its first message id, a
 * stable collapse key); everything else is a `"normal"` run rendered inline. */
type MessageGroup =
  | { kind: "normal"; messages: ChatMessage[] }
  | { kind: "inter"; id: string; messages: ChatMessage[] };

/** Fold the flat oldest→newest stream into contiguous groups, coalescing runs of
 * inter-agent messages so each run becomes ONE collapsible block. Order and
 * membership are preserved exactly — this only partitions, never reorders. */
function groupMessages(messages: ChatMessage[]): MessageGroup[] {
  const groups: MessageGroup[] = [];
  for (const m of messages) {
    const inter = isInterAgent(m);
    const last = groups[groups.length - 1];
    if (inter && last?.kind === "inter") {
      last.messages.push(m);
    } else if (!inter && last?.kind === "normal") {
      last.messages.push(m);
    } else if (inter) {
      groups.push({ kind: "inter", id: m.id, messages: [m] });
    } else {
      groups.push({ kind: "normal", messages: [m] });
    }
  }
  return groups;
}

/** Format an epoch-second ts as a local hh:mm — never fabricate a display string. */
function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function ChatArea({
  member,
  members = [],
  workers = [],
  onOpenDetail,
  onOpenTasks,
  onOpenRoleSettings,
  onWake,
  jumpToMsgId,
  draftSeed,
  headerSub,
  headerTaskTitle,
}: {
  member: Member;
  // The full office roster, used to resolve a message's sender id → display name
  // for INTER-AGENT (agent↔agent) messages, where the sender is neither the owner
  // nor necessarily the window's `member`. Optional (defaults empty) so a caller
  // that only cares about owner↔agent threads need not thread it through.
  members?: Member[];
  // The LIVE outsource workers, the sender-label twin of `members`: an
  // outsource id is NEVER in the 正職 roster (GET /api/members excludes
  // kind='outsource' by design), so without this list an outsource sender's
  // label degraded to its raw ow- id while the left rail showed the codename.
  // Optional (defaults empty) for the same reason `members` is.
  workers?: OutsourceWorkerView[];
  // Open the member detail page. Optional: when absent the header is NOT
  // interactive (no cursor/role/tabindex) so we never advertise a dead click.
  onOpenDetail?: () => void;
  // T-dfae 任務圖示: jump to the tasks page filtered to this peer's unfinished
  // tasks. Optional — absent = no button (an outsource peer's tasks are not
  // separable from every other worker's, so the jump would lie).
  onOpenTasks?: () => void;
  // T-dfae 角色設定圖示: jump to this peer's role definition page. Optional —
  // absent = no button (an outsource peer has no role to define).
  onOpenRoleSettings?: () => void;
  // T-94c1 就地喚醒: wake this member from the chat itself (calls activateMember
  // in the parent). Optional — absent = no in-chat wake button (an outsource
  // worker is spawn/task-driven, not activate-woken, so the button would lie);
  // the offline composer then degrades to the plain "go to member panel" bar.
  //
  // 🔴 May resolve with the activate's {@link MemberActivateResult} (T-7fa1).
  // `activationPending: true` = the wake was accepted but NOTHING was
  // dispatched; the wake row must roll back its 「喚醒中…」 and say so, because
  // no lifecycle change is coming to clear it. A caller returning void keeps the
  // old silent behaviour, so the wire-up returns the adapter's result verbatim.
  onWake?: () => void | Promise<MemberActivateResult | void>;
  // B3 跳到原訊息: locate + highlight this message once the thread loads (the
  // 等我回覆 page routes here via #office/chat/<id>/msg/<msgId>). One-shot per
  // id — later SSE refetches never re-scroll. A message outside the loaded
  // recent window falls back to the normal entry positioning (honest miss).
  jumpToMsgId?: string;
  // T-e987 compose seed: a one-shot draft prefix (e.g. "[T-7d40] ") the 任務卡
  // 負責人/建立者 label routes here to (#office/chat/<id>/compose/<taskNo>) so
  // the owner starts a message already tagged with the task. Seeds ONLY an
  // empty draft (never clobbers what the owner is typing) and only once per
  // distinct seed value; the owner can freely delete it.
  draftSeed?: string;
  // Header subtitle OVERRIDE. Default (absent) = the shared PresenceBadge —
  // the single member-presence truth. An OUTSOURCE chat (M3 §4.2) passes its
  // own line instead: a worker is anonymous and task-bound, with NO member
  // presence to project — rendering the badge there would fabricate one.
  headerSub?: React.ReactNode;
  // T-3451: the peer's CURRENT task title, shown FULL (no clamp) as a third
  // header line under the sub — owner 圖2: the selected member's header shows
  // the complete task title, untruncated. A member's title is joined on the
  // office page (useMemberCurrentTasks); an outsource worker's rides
  // OutsourceWorkerView.taskTitle. Absent/"" ⇒ nothing rendered (a released /
  // taskless peer never grows an empty line here).
  headerTaskTitle?: string;
}) {
  const { t } = useI18n();
  const isOffline = member.status === "offline";
  // T-94c1 (owner 2026-07-17, reverses the M2-4 "composer usable ONLY when
  // online" spec — "keep sending message even agent offline"): the composer is
  // now ALSO usable while a REAL roster member is offline/stopped, where a sent
  // message queues in the backend (server never gated on recipient presence —
  // api_chat.PutChat lands it regardless — and UnreadCounts counts it, so the
  // member reads it on wake). That unlock is GATED ON onWake, which OfficePage
  // wires ONLY for a live roster member — deliberately NOT for a synthetic
  // released/removed peer (read-only, T-661b — it must never grow a typable
  // composer or a false "will queue" promise) nor for an outsource worker.
  // Everything else stays LOCKED: waking is a brief transient, stopping is
  // winding down (a reply typed then could land in a dying session and miss),
  // and any peer with no wake path. Presence-driven: `member` comes from the
  // SSE-refetched roster, so a lifecycle flip re-renders without a reload. Reads
  // the five-state `lifecycle`, not the collapsed tri-state `status`.
  const canQueueOffline =
    !!onWake &&
    (member.lifecycle === "offline" || member.lifecycle === "stopped");
  const composerLocked = !(member.lifecycle === "online" || canQueueOffline);
  // offline/stopped live member: composer unlocked but the member is NOT here —
  // show the queue notice + in-place wake row above the input (owner mockup).
  const offlineQueue = canQueueOffline;
  // Wake-click instant feedback: the activate POST only writes the wake INTENT;
  // presence flips to waking via SSE shortly after. Optimistically disable the
  // button meanwhile so a double-tap can't fire two activates. Reset whenever
  // lifecycle moves off offline/stopped (woke, or owner cancelled).
  const [wakePending, setWakePending] = useState(false);
  // T-7fa1: the activate reported that nothing was dispatched. Distinct from
  // wakePending — "not waiting, because nothing was sent". Never both true.
  const [wakeUndispatched, setWakeUndispatched] = useState(false);
  // 🔴 `member.id` IS a dependency, not decoration (review r1 SHOULD-1).
  // OfficePage renders <ChatArea> WITHOUT a key and frontend/CLAUDE.md states
  // the component is NOT remounted when the peer changes — so switching from
  // one offline peer to another leaves `offlineQueue` true→true, the effect
  // never re-runs, and A's notice stays on screen claiming B's wake was never
  // sent. Owner never woke B. That is the same class of on-screen lie this
  // ticket exists to remove, so the reset keys on the peer as well.
  useEffect(() => {
    setWakePending(false);
    setWakeUndispatched(false);
  }, [offlineQueue, member.id]);

  const {
    messages,
    messagesPeer,
    peerLastReadTs,
    send,
    markRead,
    hasMore,
    loadOlder,
  } = useChat(member.id);

  // Released-worker codenames: an ow- participant that is NOT in the live
  // `workers` list (task closed → dropped off) still has a codename on the
  // per-id read — resolve it lazily so the label never degrades to the raw id.
  const unknownOwIds = useMemo(() => {
    const out = new Set<string>();
    for (const m of messages) {
      for (const id of [m.from, m.to]) {
        if (
          id.startsWith("ow-") &&
          id !== member.id &&
          !workers.some((w) => w.id === id)
        ) {
          out.add(id);
        }
      }
    }
    return Array.from(out);
  }, [messages, workers, member.id]);
  const codenames = useWorkerCodenames(unknownOwIds);

  // Resolve a participant id → display name: prefer a roster match, else the raw
  // id (never fabricate). The window's own `member` is always resolvable even if
  // it is not in the passed roster.
  const nameOf = (id: string): string => {
    if (id === member.id) return member.name;
    // Server-authored messages (T-ba04 reassign handover, sender="system") are
    // not a roster member — render the synthetic sender as the localized 「系統」
    // label instead of the raw "system" id.
    if (id === "system") return t.chat.systemSender;
    const rosterName = members.find((m) => m.id === id)?.name;
    if (rosterName !== undefined) return rosterName;
    // Outsource workers live outside the 正職 roster — resolve their codename
    // (the same identity the left rail shows) before giving up on the raw id:
    // live workers from the passed list, released ones from the lazy per-id
    // cache.
    const codename =
      workers.find((w) => w.id === id)?.codename ?? codenames.get(id);
    if (codename !== undefined) return t.office.outsource.label(codename);
    return id;
  };
  // Is the owner ACTUALLY looking (window focused + tab visible)? Read side
  // effects (mark-read below) are gated on this: a backgrounded window must
  // never consume unread state (the roster badge has to survive until the
  // owner really comes back and looks).
  const windowActive = useWindowActive();
  // T-8aaa draft survival: seed the text from the per-peer draft store so a
  // 跳頁-then-return (which unmounts/remounts this component) restores what the
  // owner had typed. Lazy-init covers the FIRST mount for the initially-selected
  // peer; a later peer SWITCH (this instance is reused, not remounted) restores
  // in the peer-switch render block below. Staged attachments are restored
  // alongside (they live in useAttachmentStaging, set via its API).
  const [draft, setDraft] = useState(() => getChatDraft(member.id)?.text ?? "");
  // The staged attachments (pasted images AND/OR picked/dropped files), held
  // until the message is sent — the SHARED staging state machine
  // (useAttachmentStaging: size/count caps, paste/pick funnels, previews).
  const {
    pendingAttachments,
    attachError,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
    restoreAttachments,
  } = useAttachmentStaging();
  // An image opened full-size in the lightbox overlay (null = closed). Holds the
  // ready-to-render src (already token-authed for gated blobs, or a data: URI).
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  // A .md attachment opened in the in-cockpit preview overlay (T-a1c4; null =
  // closed) — carries the blob's display title + serve url.
  const [mdPreview, setMdPreview] = useState<{ title: string; url: string } | null>(
    null,
  );
  // M2-3 file & image gallery panel (header icon toggles it).
  const [galleryOpen, setGalleryOpen] = useState(false);
  // The attachment whose share link was just copied (transient 「已複製」
  // feedback on that one button; null = none).
  const [shareCopiedId, setShareCopiedId] = useState<string | null>(null);
  // Inter-agent (agent↔agent) groups that the owner has EXPANDED (keyed by the
  // group's first-message id). Collapsed is the default — a group is expanded
  // only once its id lands here, so the owner opts in per block.
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(
    () => new Set(),
  );
  // Expanded 判定 is membership-based (T-bf82 收折 × 分頁): a group counts as
  // expanded when ANY of its message ids is in the set — a history prepend can
  // merge a loaded older run into an existing expanded block, CHANGING the
  // group's first-message id (the collapse key); keying strictly on group.id
  // would silently collapse the block the owner had opened. Toggling open
  // still stores group.id; toggling closed removes EVERY member id so no
  // stale key keeps the merged block open.
  const groupExpanded = (group: { id: string; messages: ChatMessage[] }) =>
    expandedGroups.has(group.id) ||
    group.messages.some((m) => expandedGroups.has(m.id));
  const toggleGroup = (group: { id: string; messages: ChatMessage[] }) =>
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (
        next.has(group.id) ||
        group.messages.some((m) => next.has(m.id))
      ) {
        next.delete(group.id);
        for (const m of group.messages) next.delete(m.id);
      } else {
        next.add(group.id);
      }
      return next;
    });
  const inputRef = useRef<HTMLTextAreaElement>(null);
  // Hidden native file input the attach button triggers (the iPhone fix — no
  // Cmd+V needed; tap the paperclip → OS file/photo picker).
  const fileInputRef = useRef<HTMLInputElement>(null);
  // IME composition guard. While a CJK (中/日/韓) candidate is being composed the
  // input fires keydown with keyCode 229 and a final Enter that CONFIRMS the
  // candidate — that Enter must NOT be read as "send". We track composing in a
  // ref (not state) so the keydown handler always sees the live value with no
  // stale-closure lag. onCompositionEnd may fire slightly AFTER the confirming
  // keydown in some browsers, so keydown also checks nativeEvent.isComposing /
  // keyCode 229 as belt-and-braces.
  const isComposingRef = useRef(false);
  // A message may carry text and/or attachments — sendable when EITHER present.
  const canSend = draft.trim().length > 0 || pendingAttachments.length > 0;

  // The composer is a multi-line textarea (Enter sends, Shift+Enter breaks a
  // line — see onKeyDown). Auto-grow to the draft on EVERY draft change —
  // typing, the optimistic clear in submit(), and the failure restore all set
  // state, so sizing off the draft (not just typing events) keeps the box
  // honest in each path. CSS max-height caps the growth; past it the textarea
  // scrolls its own overflow so a long draft is always fully reachable.
  useLayoutEffect(() => {
    if (inputRef.current) autosizeTextarea(inputRef.current);
  }, [draft]);

  // Auto-scroll to the newest message (regression #6: the thread never scrolled,
  // so new messages landed below the fold). `messagesRef` is the scroll viewport
  // and `endRef` is a bottom sentinel we scroll into view. We only auto-pull when
  // the user is already near the bottom OR just sent a message — if they scrolled
  // UP to read history, a new incoming message must NOT yank them back down.
  const messagesRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const nearBottomRef = useRef(true);

  // ===== LINE/FB-style unread jump (M2 batch 19) =====
  //
  // ② ENTRY POSITIONING: entering a conversation with unread messages must land
  // on the FIRST unread message, not the bottom. The "first unread" anchor is
  // derived from `member.unreadCount` (the roster badge count) SNAPSHOT at
  // conversation entry — this is the race-free source: the server clears the
  // read watermark as a side effect of the very `listChat` this component
  // triggers ("list 即讀"), and the roster's unreadCount refetches to 0 right
  // after, so anything read *after* entry would already be wiped. The snapshot
  // happens synchronously at first render, strictly before the listChat fires.
  // unreadCount counts exactly the peer→owner messages above the watermark, so
  // the first unread = the earliest of the LAST `unreadCount` peer→owner
  // messages in the loaded thread.
  const initialUnreadRef = useRef(member.unreadCount);
  // Set once per conversation when entry positioning ran: the id of the first
  // unread message. Drives the "以下是未讀訊息" divider (kept for the whole
  // session, like LINE) and the initial scroll target.
  const [firstUnreadId, setFirstUnreadId] = useState<string | null>(null);
  // ① NEW-MESSAGE FLOATING CHIP: when the owner has scrolled UP and a new
  // message addressed to them lands below the fold, a floating "有新訊息" chip
  // appears. Its anchor = the FIRST new inbound message accumulated since the
  // chip appeared (session-tracked; no server involvement). Cleared when the
  // owner reaches the bottom (click-scroll or naturally).
  const [newMsgAnchorId, setNewMsgAnchorId] = useState<string | null>(null);
  // Ids seen on the previous messages render — the diff basis for "which
  // messages are NEW" (refetch replaces the whole array, so append detection
  // must go through ids, not length).
  const prevIdsRef = useRef<Set<string>>(new Set());
  // T-bf82 scrollback: the pre-fetch scroll-geometry snapshot an older-page
  // prepend restores from (null = no older page in flight/pending), and the
  // UI-side in-flight lock (belt-and-braces over useChat's own) so repeated
  // scroll events near the top can't re-snapshot the anchor mid-flight.
  const prependAnchorRef = useRef<{
    firstId: string;
    height: number;
    top: number;
  } | null>(null);
  const loadingOlderRef = useRef(false);
  // One-shot latch: entry positioning (bottom OR first-unread) ran for this
  // conversation.
  const initialPositionedRef = useRef(false);
  // Is the CURRENT unread run (the block below the divider) still OPEN — i.e.
  // the owner has not reached the bottom since the divider anchored? While
  // open, further arrivals belong to the SAME run (the divider stays put).
  // Once closed (bottom reached = everything seen), the next unseen inbound
  // starts a NEW run and RE-ANCHORS the divider — the chip and the divider
  // share ONE "start of the new messages" anchor (owner bug report: staying
  // in the conversation, two messages land, the chip appears, but clicking it
  // showed NO divider — the divider only ever anchored at conversation entry
  // and had no path for in-conversation arrivals).
  const unreadRunOpenRef = useRef(false);
  // Entry positioning wants the divider scrolled into view ONCE. A chip-driven
  // divider re-anchor must NOT scroll — the owner is reading history and must
  // never be yanked; they jump via the chip when they choose to. This ref
  // marks a pending ENTRY scroll for the firstUnreadId effect below.
  const entryScrollPendingRef = useRef(false);
  // B3 跳到原訊息: the jump target already consumed (one-shot per id — an SSE
  // refetch must never re-scroll) and the transient highlight on the located
  // row (cleared after the flash).
  const jumpConsumedRef = useRef<string | null>(null);
  const [highlightMsgId, setHighlightMsgId] = useState<string | null>(null);
  // T-e987 compose seed: the seed value already applied (one-shot per distinct
  // value, reset on a peer switch so the same taskNo can seed another peer).
  const seedConsumedRef = useRef<string | null>(null);

  // ChatArea is NOT remounted when the selected member changes (OfficePage
  // renders one instance) — reset the per-conversation session trackers on a
  // peer switch. Render-time state adjustment (guarded) per the React docs
  // pattern, so no stale-effect ordering.
  const peerIdRef = useRef(member.id);
  if (peerIdRef.current !== member.id) {
    peerIdRef.current = member.id;
    initialUnreadRef.current = member.unreadCount;
    initialPositionedRef.current = false;
    prevIdsRef.current = new Set();
    nearBottomRef.current = true;
    unreadRunOpenRef.current = false;
    entryScrollPendingRef.current = false;
    jumpConsumedRef.current = null;
    seedConsumedRef.current = null;
    prependAnchorRef.current = null;
    setFirstUnreadId(null);
    setNewMsgAnchorId(null);
    setHighlightMsgId(null);
    // T-8aaa: swap the composer to the NEW peer's saved draft. Render-phase
    // state adjustment (same pattern as the resets above) so the committed
    // render already carries the new peer's text+attachments — no stale frame
    // and no cross-peer mis-persist by the save effect below. Attachments go
    // through the staging API: clear first, then restore the saved list (its
    // functional set sees the just-cleared empty list and takes the snapshot).
    const restored = getChatDraft(member.id);
    setDraft(restored?.text ?? "");
    clearAttachments();
    if (restored && restored.attachments.length > 0) {
      restoreAttachments(restored.attachments);
    }
  }

  // T-8aaa: FIRST-mount attachment restore. The text is lazy-initialized above,
  // but staged attachments live in useAttachmentStaging (starts empty) and have
  // no external lazy init — replay the saved list once, before paint, so a
  // remount shows the images immediately. A peer SWITCH is handled in the block
  // above; this one-shot covers only the initial peer.
  const didMountAttachRestoreRef = useRef(false);
  useLayoutEffect(() => {
    if (didMountAttachRestoreRef.current) return;
    didMountAttachRestoreRef.current = true;
    const restored = getChatDraft(member.id);
    if (restored && restored.attachments.length > 0) {
      restoreAttachments(restored.attachments);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // T-8aaa: persist the live draft (text + staged attachments) to the per-peer
  // store on every change, so an unmount (跳頁) leaves the latest draft behind.
  // Because the peer-switch block adjusts draft+attachments during render, the
  // committed values are always consistent with `member.id` here — no stale
  // window. An empty draft deletes the entry (saveChatDraft), giving the
  // "送出 / 手動清空後歸零" behavior for free.
  useEffect(() => {
    saveChatDraft(member.id, { text: draft, attachments: pendingAttachments });
  }, [member.id, draft, pendingAttachments]);

  // T-e987 compose seed: prefill the composer once with "[<taskNo>] " when the
  // 任務卡 label routes here, but only into an EMPTY draft (never overwrite
  // what the owner is mid-typing). One-shot per distinct seed value.
  useEffect(() => {
    if (!draftSeed) return;
    if (seedConsumedRef.current === draftSeed) return;
    seedConsumedRef.current = draftSeed;
    setDraft((cur) => (cur ? cur : draftSeed));
  }, [draftSeed]);

  // ===== Scrollback — 往上捲載入更多 (T-bf82) =====
  //
  // Scrolling near the TOP of the thread loads one older history page and
  // PREPENDS it. The viewport must not jump: we snapshot the scroll geometry
  // (+ the current first message id) before the fetch, and the layout effect
  // below compensates scrollTop by the height the prepend added — before
  // paint, so the owner keeps reading the same row. The anchor's firstId also
  // tells "a prepend really landed" apart from an unrelated (appended) update.
  const NEAR_TOP_PX = 120;

  async function loadOlderAnchored() {
    if (loadingOlderRef.current || !hasMore) return;
    const el = messagesRef.current;
    if (!el || messages.length === 0 || messagesPeer !== member.id) return;
    loadingOlderRef.current = true;
    prependAnchorRef.current = {
      firstId: messages[0].id,
      height: el.scrollHeight,
      top: el.scrollTop,
    };
    try {
      await loadOlder();
    } finally {
      loadingOlderRef.current = false;
    }
  }

  // Prepend scroll compensation + session-tracker bookkeeping. useLayoutEffect
  // (not useEffect) so the scrollTop fix lands BEFORE paint — no visible jump.
  // Runs before the scroll-position reactor below (layout effects precede
  // passive effects in a commit), so registering the prepended ids into
  // prevIdsRef here keeps the reactor's "fresh message" diff honest: loaded
  // HISTORY is not fresh — it must never arm the new-message chip nor
  // re-anchor the unread divider.
  useLayoutEffect(() => {
    const anchor = prependAnchorRef.current;
    if (!anchor) return;
    if (messagesPeer !== member.id || messages.length === 0) return;
    const idx = messages.findIndex((m) => m.id === anchor.firstId);
    if (idx <= 0) {
      // idx === 0: nothing prepended (yet) — an unrelated append committed
      // while the older page is in flight; keep waiting on the anchor.
      // idx === -1: the anchor row vanished (peer data reset) — drop it.
      if (idx === -1) prependAnchorRef.current = null;
      return;
    }
    prependAnchorRef.current = null;
    for (let i = 0; i < idx; i++) prevIdsRef.current.add(messages[i].id);
    const el = messagesRef.current;
    if (el) el.scrollTop = anchor.top + (el.scrollHeight - anchor.height);
    // The one-shot entry positioning (initialPositionedRef) already ran for
    // this conversation — a prepend must never re-run it, and it doesn't:
    // the latch stays untouched here.
  }, [messages, messagesPeer, member.id]);

  // Threshold (px) within which the viewport counts as "at the bottom".
  const NEAR_BOTTOM_PX = 80;
  function onMessagesScroll() {
    const el = messagesRef.current;
    if (!el) return;
    // Near the TOP → pull one older page (no-op while one is in flight or
    // when the history is exhausted — hasMore=false renders the
    // "已到最早訊息" marker instead).
    if (el.scrollTop < NEAR_TOP_PX && hasMore) {
      void loadOlderAnchored();
    }
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    const nowNearBottom = distance <= NEAR_BOTTOM_PX;
    // Crossing into the bottom band = the owner has now read to the latest → mark
    // the newest message read (monotonic server-side; safe to fire repeatedly).
    if (nowNearBottom && !nearBottomRef.current && newestTs > 0) {
      void markRead(newestTs);
    }
    // Reaching the bottom means the "new messages" chip's content has been
    // seen → dismiss it (no-op when already null), and the current unread run
    // is CLOSED — the next unseen inbound starts a new run (divider re-anchors).
    if (nowNearBottom) {
      setNewMsgAnchorId(null);
      unreadRunOpenRef.current = false;
    }
    nearBottomRef.current = nowNearBottom;
  }

  // The newest message ts in the thread — the watermark the owner marks read up
  // to (0 when empty).
  const newestTs = messages.length > 0 ? messages[messages.length - 1].ts : 0;

  // B3 跳到原訊息 — declared BEFORE the entry-positioning reactor below so the
  // jump consumes entry positioning first (the divider/bottom scroll must not
  // fight the located message). One-shot per target id; a target outside the
  // loaded recent window falls back to the plain land-at-bottom (honest miss —
  // the thread still opens).
  useEffect(() => {
    if (!jumpToMsgId) return;
    if (messagesPeer !== member.id || messages.length === 0) return;
    if (jumpConsumedRef.current === jumpToMsgId) return;
    jumpConsumedRef.current = jumpToMsgId;
    // The jump owns the initial viewport — mark entry positioning done.
    initialPositionedRef.current = true;
    prevIdsRef.current = new Set(messages.map((m) => m.id));
    // Raw interpolation matches the chip-jump selector above — message ids
    // are server-minted (`c-<hex>`), never arbitrary strings.
    const el = messagesRef.current?.querySelector(
      `[data-msg-id="${jumpToMsgId}"]`,
    );
    if (el) {
      el.scrollIntoView({ block: "center" });
      // Located mid-thread → not at the bottom; a later arrival must not yank.
      nearBottomRef.current = false;
      setHighlightMsgId(jumpToMsgId);
      // Async content above the target (images decoding to their real height,
      // inline reply cards refetching) reflows AFTER this paint-time scroll and
      // shoves the centered row off-screen — worst on short mobile viewports.
      // A ResizeObserver on the scroll viewport never fires (its own box is
      // clamped by flex + overflow); watch the in-flow content rows, whose
      // height actually grows, and re-center until the highlight window closes.
      const scroller = messagesRef.current;
      if (scroller) {
        const ro = new ResizeObserver(() =>
          el.scrollIntoView({ block: "center" }),
        );
        for (const row of Array.from(scroller.children)) ro.observe(row);
        const settle = window.setTimeout(() => ro.disconnect(), 2600);
        return () => {
          window.clearTimeout(settle);
          ro.disconnect();
        };
      }
    } else {
      endRef.current?.scrollIntoView();
    }
  }, [jumpToMsgId, messages, messagesPeer, member.id]);

  // The jump highlight is a transient flash — clear it after the CSS pulse so
  // the row returns to the normal thread look.
  useEffect(() => {
    if (!highlightMsgId) return;
    const timer = window.setTimeout(() => setHighlightMsgId(null), 2600);
    return () => window.clearTimeout(timer);
  }, [highlightMsgId]);

  // The ONE scroll-position reactor. First load of a conversation → entry
  // positioning (② first unread when entered with a badge, else the existing
  // land-at-bottom). Subsequent updates → the existing auto-follow when near
  // the bottom, else (scrolled up) arm the ① new-message chip on the first
  // fresh inbound message.
  useEffect(() => {
    // STALE-PEER GUARD (divider-latch fix): on a peer switch this effect fires
    // for the render where `member.id` is already the NEW peer but `messages`
    // is still the PREVIOUS peer's thread — useChat clears the thread in its
    // own effect, ONE COMMIT LATER. Latching entry positioning on that stale
    // commit consumed the one-shot (initialPositionedRef) against the wrong
    // thread, so the "以下是未讀訊息" divider never rendered when entering an
    // unread room FROM a non-empty thread. `messagesPeer` is set TOGETHER with
    // `messages` (single state in useChat), so it is the honest owner of the
    // array — do nothing until the thread really belongs to this peer.
    if (messagesPeer !== member.id) return;
    if (messages.length === 0) return;
    if (!initialPositionedRef.current) {
      initialPositionedRef.current = true;
      prevIdsRef.current = new Set(messages.map((m) => m.id));
      const count = initialUnreadRef.current;
      // Unread = peer→owner only (matches the server's unread_counts rule:
      // recipient == reader; inter-agent traffic never counts).
      const inbound =
        count > 0
          ? messages.filter((m) => m.from === member.id && m.to === OWNER_ID)
          : [];
      const first = inbound.slice(-count)[0];
      if (first) {
        // Positioning happens in the firstUnreadId effect below, AFTER the
        // divider renders (it is the scroll target). Until the measurement
        // there says otherwise, we are NOT at the bottom.
        nearBottomRef.current = false;
        unreadRunOpenRef.current = true;
        entryScrollPendingRef.current = true;
        setFirstUnreadId(first.id);
      } else {
        endRef.current?.scrollIntoView();
      }
      return;
    }
    const prev = prevIdsRef.current;
    const fresh = messages.filter((m) => !prev.has(m.id));
    prevIdsRef.current = new Set(messages.map((m) => m.id));
    if (nearBottomRef.current) {
      endRef.current?.scrollIntoView();
      // Following the bottom = everything is being seen; any armed chip is
      // stale (e.g. the owner just sent a reply, which force-follows), and
      // the unread run — if one was open — is being read right now.
      setNewMsgAnchorId(null);
      unreadRunOpenRef.current = false;
      return;
    }
    // Scrolled up + a new message addressed to the owner → arm the chip. The
    // anchor stays the FIRST unseen message even as more accumulate.
    const inboundNew = fresh.find(
      (m) => m.to === OWNER_ID && m.from !== OWNER_ID,
    );
    if (inboundNew) {
      setNewMsgAnchorId((cur) => cur ?? inboundNew.id);
      // Chip/divider alignment (owner bug): the chip and the "以下是未讀訊息"
      // divider share the SAME "start of the new messages". If no unread run
      // is open (the owner had seen everything up to now), this first unseen
      // inbound STARTS one → anchor the divider here, so jumping via the chip
      // lands on a LINE-style divider. If a run is already open (e.g. the
      // entry divider's tail was never read down to), the arrival extends the
      // SAME run — the divider stays put.
      if (!unreadRunOpenRef.current) {
        unreadRunOpenRef.current = true;
        setFirstUnreadId(inboundNew.id);
      }
    }
  }, [messages, messagesPeer, member.id]);

  // ② entry scroll: once the unread divider is in the DOM, pin it to the top of
  // the viewport, then measure honestly whether that landed us at the bottom
  // anyway (short thread) so auto-follow keeps working there.
  useEffect(() => {
    if (!firstUnreadId) return;
    // ONLY the entry positioning scrolls here. A chip-driven divider re-anchor
    // (in-conversation arrival while scrolled up) must not move the viewport —
    // the owner is reading history; the chip is their opt-in jump.
    if (!entryScrollPendingRef.current) return;
    entryScrollPendingRef.current = false;
    const box = messagesRef.current;
    if (!box) return;
    const divider =
      box.querySelector(".chat__unread-divider") ??
      box.querySelector(`[data-msg-id="${firstUnreadId}"]`);
    // Seth spec (batch 19, LINE reference): land with 1–2 already-read rows
    // visible ABOVE the divider as context — anchor the scroll up to two
    // element rows earlier when they exist (an unread-from-the-top thread
    // falls back to the divider itself).
    let target = divider;
    for (let i = 0; i < 2; i++) {
      const prev = target?.previousElementSibling;
      if (!prev) break;
      target = prev;
    }
    target?.scrollIntoView({ block: "start" });
    const distance = box.scrollHeight - box.scrollTop - box.clientHeight;
    nearBottomRef.current = distance <= NEAR_BOTTOM_PX;
    // NOTE: the run deliberately stays OPEN even when a short thread lands at
    // the bottom here — every real "the owner saw it" path (a bottom-crossing
    // scroll, or an at-bottom auto-follow) closes it; closing on this
    // layout-dependent measurement would misfire under test/jsdom geometry.
  }, [firstUnreadId]);

  // ① chip click: smooth-scroll to the first unseen message. The chip itself is
  // dismissed by onMessagesScroll once the bottom is actually reached (or the
  // owner reads down to it naturally) — not by the click.
  function jumpToNewMessages() {
    if (!newMsgAnchorId) return;
    messagesRef.current
      ?.querySelector(`[data-msg-id="${newMsgAnchorId}"]`)
      ?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  // OWNER read receipt: entering the conversation (or a new message landing while
  // the owner is at the bottom) means the owner has SEEN up to the newest message
  // → mark it read. markRead is monotonic server-side (a stale ts is a no-op), so
  // firing on every settle is safe. If the owner has scrolled UP to read history
  // we still mark read: the newest message is loaded and being viewed on entry.
  //
  // Gated TWICE (badge-flash fix):
  //   • `windowActive` — "seen" requires the owner to actually be looking. A
  //     message landing while the window is backgrounded must NOT be consumed;
  //     the flip back to active re-runs this effect, so everything accumulated
  //     is marked read exactly when the owner returns.
  //   • `messagesPeer === member.id` — on a peer switch `newestTs` still comes
  //     from the PREVIOUS peer's thread for one commit; firing then would stamp
  //     the NEW peer's watermark with the OLD thread's timestamp.
  useEffect(() => {
    if (!windowActive) return;
    if (messagesPeer !== member.id) return;
    if (newestTs > 0) void markRead(newestTs);
  }, [newestTs, markRead, windowActive, messagesPeer, member.id]);

  // Lightbox Esc handling lives inside the shared <Lightbox> now.

  // Drag-drop: dropping files anywhere on the chat window stages them —
  // unless the composer is LOCKED (M2-4: an offline member can't receive a
  // reply, so nothing may be staged while locked; paste/pick are already
  // unreachable because the locked composer renders no input at all).
  function onDragOver(e: React.DragEvent<HTMLDivElement>) {
    if (composerLocked) return;
    if (e.dataTransfer.types.includes("Files")) e.preventDefault();
  }
  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    if (composerLocked) return;
    const files = Array.from(e.dataTransfer.files ?? []);
    if (files.length === 0) return;
    e.preventDefault();
    stageFiles(files);
  }

  async function submit() {
    if (!canSend) return;
    // Sending my own message always scrolls to the bottom, even if I had scrolled
    // up to read history — my just-sent message should be visible.
    nearBottomRef.current = true;
    // Snapshot the composer, then OPTIMISTICALLY clear it BEFORE the server
    // round-trip. `send()` awaits the POST + a refetch (seconds); if we only
    // cleared after that await, the draft stays populated meanwhile and a second
    // Enter re-fires submit() on the SAME draft → a duplicate send. Clearing up
    // front makes canSend false immediately, so the repeat Enter is a no-op. On
    // failure we restore the snapshot below so the user's message is never
    // silently swallowed.
    const draftSnapshot = draft;
    const attachmentsSnapshot = pendingAttachments;
    // ALL staged attachments ride the SAME message, in staged order.
    const attachments = attachmentsSnapshot.map((a) => ({
      dataB64: a.dataUri,
      // Omit an empty filename so the backend applies its default (pasted
      // images); a real picked filename passes through.
      ...(a.filename ? { filename: a.filename } : {}),
      mime: a.mime,
    }));
    setDraft("");
    clearAttachments();
    try {
      await send(
        draftSnapshot,
        attachments.length > 0 ? attachments : undefined,
      );
    } catch (e) {
      console.warn("ChatArea: send failed, restoring composer", e);
      // Restore the user's unsent content so it isn't silently lost. Only put
      // back what the user hasn't already retyped/restaged — if they started a
      // new draft while the send was in flight, don't clobber it.
      setDraft((cur) => (cur ? cur : draftSnapshot));
      restoreAttachments(attachmentsSnapshot);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Composition gate FIRST: an Enter that confirms an IME candidate must never
    // reach the send path. Guard on the native isComposing flag, the 229 keyCode
    // browsers stamp during composition, and our own ref (covers the browser
    // where compositionend precedes this keydown but isComposing is already false).
    if (
      e.nativeEvent.isComposing ||
      e.keyCode === 229 ||
      isComposingRef.current
    ) {
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  }

  // Copy ONE attachment's permanent share link (?sig= HMAC — see
  // lib/shareLink.ts) and flash 「已複製」 on that button only. Feedback fires
  // ONLY after both the fetch and the clipboard write succeeded — honest.
  async function onCopyShareLink(attachmentId: string) {
    try {
      await copyAttachmentShareLink(attachmentId);
      setShareCopiedId(attachmentId);
      window.setTimeout(
        () => setShareCopiedId((cur) => (cur === attachmentId ? null : cur)),
        2000,
      );
    } catch (e) {
      console.warn("ChatArea: copy share link failed", e);
    }
  }

  // The per-attachment 「複製分享連結」 button (thread bubbles; the gallery
  // panel renders its own twin).
  function renderShareButton(attachmentId: string) {
    const copied = shareCopiedId === attachmentId;
    const label = copied ? t.chat.shareLinkCopied : t.chat.copyShareLink;
    return (
      <button
        type="button"
        className="chat__share-btn"
        aria-label={label}
        title={label}
        onClick={(e) => {
          e.stopPropagation();
          void onCopyShareLink(attachmentId);
        }}
      >
        {copied ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
      </button>
    );
  }

  // Per-attachment hover action: 複製分享連結. T-7bc2 (owner 2026-07-21):
  // a markdown file's preview trigger moved from a separate hover-revealed
  // 眼睛 button to the chip itself (AttachmentStrip's `onPreviewMarkdown`,
  // always-visible, click-the-filename-to-preview) — one less ambiguous
  // floating icon between two file chips.
  function renderAttachmentExtras(att: ChatAttachmentView) {
    return <span className="chat__att-actions">{renderShareButton(att.id)}</span>;
  }

  // Render ONE message row (the LINE-style outgoing/incoming bubble). Extracted so
  // both the normal stream and an expanded inter-agent group render identically.
  // Incoming rows label the bubble with the message's TRUE sender (`nameOf(m.from)`)
  // — critical for inter-agent messages, where the sender is not the window's peer.
  function renderMessage(m: ChatMessage) {
    const mine = m.from === OWNER_ID;
    // Sender label. When the RECIPIENT is not the owner (an inter-agent message,
    // either direction: Mira→Kye or Kye→Mira) the sender name alone is ambiguous
    // — members message DIFFERENT agents, so the label spells out the direction:
    // "Mira → Kye". A message addressed to the owner keeps the plain sender name
    // (the recipient is implicit — it's this thread's owner side). Names resolve
    // through the roster (`nameOf`), falling back to the raw id — never blank.
    const senderLabel =
      m.to !== OWNER_ID
        ? `${nameOf(m.from)} → ${nameOf(m.to)}`
        : nameOf(m.from);
    // Per-message read state (LINE-style): every own message the peer's real
    // last-read watermark covers shows its own "已讀". Honest — driven only by a
    // recorded watermark, never fabricated.
    const read = mine && peerLastReadTs >= m.ts;
    // ONE bubble per message (owner feedback): text and attachments share the
    // SAME bubble container — text on top, attachments stacked below — one
    // rounded surface, one background, so a text+attachment message reads as a
    // single message instead of two disconnected blocks. An attachment-only
    // message is the same single bubble (just no text block). The side meta
    // (已讀/time) hangs off this whole bubble via chat__msg-line below.
    //
    // B3: a message carrying a reply-card link renders the CARD as its bubble
    // (spec §3: 請示以卡片形式直接出現在訊息串中，無額外橫幅) — the card
    // itself fetches its full shape and owns the answer / 重新決定 flow.
    const content = m.replyCardId ? (
      <ChatReplyCard
        replyCardId={m.replyCardId}
        fallbackSummary={m.body}
        initialStatus={m.replyCardStatus}
      />
    ) : (
      <div className="chat__msg-bubble">
        {/* T-84c8: the message body is the purest owner/agent free text in the
         * app (and, via webhooks, can carry text from an EXTERNAL system), so
         * it renders through the shared XSS-safe `Markdown` — same posture as
         * the reply-card body, which already renders this very field's
         * fallback as markdown. `breaks` keeps Enter meaning "new line", the
         * way the bubble's pre-wrap did before. */}
        {m.body && (
          <Markdown
            source={m.body}
            className="chat__msg-text doc-md"
            breaks
          />
        )}
        {/* Stored attachments — the SHARED AttachmentStrip (image → clickable
         * thumbnail opening the lightbox; file → download chip), each item
         * carrying the hover 複製分享連結 button. */}
        <AttachmentStrip
          attachments={m.attachments}
          className="chat__msg-attachments"
          itemClassName="chat__msg-attachment"
          imageClassName="chat__msg-image chat__msg-image--clickable"
          onOpenImage={(src) => setLightboxSrc(src)}
          onPreviewMarkdown={(att) =>
            setMdPreview({
              title: att.filename || t.chat.downloadAttachment,
              url: att.url,
            })
          }
          renderExtra={(att) => renderAttachmentExtras(att)}
        />
      </div>
    );
    return (
      <Fragment key={m.id}>
        {/* ② the "以下是未讀訊息" divider — a thin low-emphasis rule above the
         * first message that was unread at conversation entry. It renders for
         * the whole session (like LINE) even after the watermark clears. */}
        {m.id === firstUnreadId && (
          <div
            className="chat__unread-divider"
            role="separator"
            aria-label={t.chat.unreadBelow}
          >
            <span>{t.chat.unreadBelow}</span>
          </div>
        )}
        <div
          className={
            `chat__msg${mine ? " chat__msg--me" : ""}` +
            (m.replyCardId ? " chat__msg--card" : "") +
            (m.id === highlightMsgId ? " chat__msg--located" : "")
          }
          data-msg-id={m.id}
        >
          {mine ? (
          // LINE-style outgoing: a bottom-aligned meta column to the LEFT of the
          // bubble, stacking "已讀" (when read) above the send time.
          <div className="chat__msg-line">
            <div className="chat__msg-sidemeta">
              {read && <span className="chat__msg-read">{t.chat.read}</span>}
              <span className="chat__msg-time">{formatTime(m.ts)}</span>
            </div>
            <div className="chat__msg-content">{content}</div>
          </div>
        ) : (
          // LINE-style incoming: mirror of the outgoing row. The name label above
          // the bubble is `senderLabel` — the message's TRUE sender, plus the
          // recipient ("A → B") when the message is inter-agent; the send time
          // moves to a bottom-aligned meta column on the bubble's RIGHT edge.
          <>
            <div className="chat__msg-meta">
              <span className="chat__msg-name">{senderLabel}</span>
            </div>
            <div className="chat__msg-line">
              <div className="chat__msg-content">{content}</div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">{formatTime(m.ts)}</span>
              </div>
            </div>
          </>
          )}
        </div>
      </Fragment>
    );
  }

  // Render one collapsible INTER-AGENT block. Collapsed (default): a single
  // toggle row announcing "N messages between agents · expand". Expanded: the
  // toggle stays as a collapse affordance, followed by the real message rows.
  function renderInterAgentGroup(group: {
    id: string;
    messages: ChatMessage[];
  }) {
    const expanded = groupExpanded(group);
    return (
      <div
        key={`inter-${group.id}`}
        className={`chat__inter${expanded ? " chat__inter--expanded" : ""}`}
      >
        <button
          type="button"
          className="chat__inter-toggle"
          aria-expanded={expanded}
          onClick={() => toggleGroup(group)}
        >
          <ChevronRightIcon
            size={13}
            className={`chat__inter-caret${
              expanded ? " chat__inter-caret--open" : ""
            }`}
          />
          <span>
            {expanded
              ? t.chat.interAgentCollapse
              : t.chat.interAgentExpand(group.messages.length)}
          </span>
        </button>
        {expanded && (
          <div className="chat__inter-body">
            {group.messages.map((m) => renderMessage(m))}
          </div>
        )}
      </div>
    );
  }

  return (
    // Drag-drop staging surface: dropping files anywhere over the chat window
    // stages them as attachments (no-op while the composer is locked — the
    // handlers gate on composerLocked themselves).
    <div className="chat" onDragOver={onDragOver} onDrop={onDrop}>
      <header
        className={`chat__header${onOpenDetail ? " chat__header--clickable" : ""}`}
        {...(onOpenDetail
          ? {
              role: "button",
              tabIndex: 0,
              onClick: onOpenDetail,
              onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onOpenDetail();
                }
              },
            }
          : {})}
      >
        <Avatar size={38} />
        <div className="chat__header-text">
          {/* Name only — no chevron/caret glyph (owner feedback: the "Mira ›"
           * arrow was noise). The header itself stays the clickable detail
           * entry (chat__header--clickable above); its hover/focus affordance
           * carries the click hint now. */}
          <div className="chat__header-name">
            <span>{member.name}</span>
          </div>
          {/* Single presence truth: the SHARED PresenceBadge (lifecycle dot +
           * role) — same component as the roster card / monitor row / detail
           * panel. No self-drawn `role · lastSeen` (that was a second presence
           * source + the "online yet Never online" dishonesty). */}
          <div className="chat__header-sub">
            {headerSub ?? <PresenceBadge member={member} />}
          </div>
          {/* T-3451: the peer's CURRENT task title, FULL (no clamp) — owner 圖2.
           * Rendered only when present (a taskless / released peer grows no
           * empty line here; showEmpty=false). */}
          {headerTaskTitle ? (
            <div className="chat__header-task">
              <CurrentTaskTitle
                title={headerTaskTitle}
                clamp={false}
                showEmpty={false}
                testid="chat-header-task-title"
              />
            </div>
          ) : null}
        </div>
        {/* T-dfae (owner 2026-07-17, 紅框 on this corner): two jump buttons
         * beside the gallery toggle. Both are OPTIONAL — the caller wires them
         * only where the jump is real (a roster member). An outsource / released
         * peer gets NEITHER: it has no role to define, and its tasks are not
         * separable (every worker collapses to the single "outsource" executor
         * key, so a task jump would show OTHER workers' tasks too). Same
         * no-dead-click rule as onOpenDetail above — we do not advertise a jump
         * that would lie. Own classes, NOT chat__gallery-toggle: that class is a
         * querySelector handle in ChatArea.gallery.test.tsx and a second element
         * wearing it would be silently picked up instead. */}
        {onOpenTasks && (
          <button
            type="button"
            className="chat__header-action"
            aria-label={t.chat.tasksLink}
            title={t.chat.tasksLink}
            data-testid="chat-header-tasks"
            onClick={(e) => {
              e.stopPropagation();
              onOpenTasks();
            }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <TasksIcon size={17} />
          </button>
        )}
        {onOpenRoleSettings && (
          <button
            type="button"
            className="chat__header-action"
            aria-label={t.chat.roleSettingsLink}
            title={t.chat.roleSettingsLink}
            data-testid="chat-header-role-settings"
            onClick={(e) => {
              e.stopPropagation();
              onOpenRoleSettings();
            }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <UserGearIcon size={17} />
          </button>
        )}
        {/* M2-3: the conversation's file & image gallery toggle. The header
         * itself may be clickable (open detail) — stopPropagation keeps the
         * gallery click/keys from bubbling into that. */}
        <button
          type="button"
          className="chat__gallery-toggle"
          aria-label={t.chat.galleryLabel}
          title={t.chat.galleryLabel}
          aria-expanded={galleryOpen}
          onClick={(e) => {
            e.stopPropagation();
            setGalleryOpen((v) => !v);
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <ImageIcon size={17} />
        </button>
      </header>

      {galleryOpen && (
        <ChatGalleryPanel
          member={member}
          resolveSender={nameOf}
          onClose={() => setGalleryOpen(false)}
        />
      )}

      <div className="chat__body">
        {messages.length > 0 ? (
          <>
            <div
              className="chat__messages"
              ref={messagesRef}
              onScroll={onMessagesScroll}
            >
              {/* T-bf82 scrollback: once the history is exhausted
               * (hasMore=false — the last older page came back short) the
               * top of the thread says so honestly instead of silently
               * refusing to load more. */}
              {!hasMore && (
                <div className="chat__history-start" role="note">
                  <span>{t.chat.historyStart}</span>
                </div>
              )}
              {/* LINE/Slack-style day grouping: the stream splits at every
               * local-midnight crossing; each day renders a centered date
               * pill (今天/昨天/date) that is ALSO the scrolling floating
               * header — `position: sticky` inside its day-group wrapper
               * pins the pill to the viewport top while its day scrolls
               * through, and the group's end pushes it off naturally (no JS
               * scroll tracking). The label is judged against the render
               * clock; per-message times keep their existing hh:mm format. */}
              {splitByDay(messages).map((day) => {
                const dayLabel = formatDayLabel(
                  day.dayTs,
                  Date.now() / 1000,
                  t.chat,
                );
                return (
                  <div key={day.dayTs} className="chat__day-group">
                    <div
                      className="chat__day-divider"
                      role="separator"
                      aria-label={dayLabel}
                    >
                      <span className="chat__day-pill">{dayLabel}</span>
                    </div>
                    {groupMessages(day.items).map((group) =>
                      group.kind === "inter"
                        ? renderInterAgentGroup(group)
                        : group.messages.map((m) => renderMessage(m)),
                    )}
                  </div>
                );
              })}
              {/* Bottom sentinel — scrolled into view to follow new messages. */}
              <div ref={endRef} className="chat__scroll-anchor" aria-hidden />
            </div>
            {/* ① floating "有新訊息" chip — appears when a new inbound message
             * lands while the owner is scrolled up; click jumps to the first
             * unseen message; dismissed once the bottom is reached. */}
            {newMsgAnchorId && (
              <button
                type="button"
                className="chat__new-msg-chip"
                onClick={jumpToNewMessages}
              >
                {t.chat.newMessages}
              </button>
            )}
          </>
        ) : isOffline ? (
          <div className="chat__offline">
            <span className="chat__offline-icon">
              <MoonIcon size={26} />
            </span>
            <div className="chat__offline-title">
              {t.chat.offlineTitle(member.name)}
            </div>
            {/* T-94c1: offline/stopped can now be messaged (queues until wake),
             * so the hint no longer says "喚醒後才能開始對話" (which contradicted
             * the unlocked composer below). The wake entry + queue notice live on
             * the composer's wake row now, not on this card. */}
            <div className="chat__offline-hint">
              {offlineQueue
                ? t.chat.offlineQueueHint(member.name)
                : t.chat.offlineHint}
            </div>
          </div>
        ) : (
          <div className="chat__empty">
            <span>{t.chat.emptyRange}</span>
          </div>
        )}
      </div>

      <footer className="chat__composer">
        {composerLocked ? (
          /* T-94c1: only waking/stopping lock the composer now (a reply typed
           * then could land in a dying/transient session and miss). The bar is
           * the entry to the member detail panel; clickable only when the caller
           * wires onOpenDetail (same no-dead-click rule as the header),
           * otherwise it degrades to the plain notice. offline/stopped no longer
           * reach here — they take the unlocked branch with a wake row. */
          onOpenDetail ? (
            <button
              type="button"
              className="chat__composer-locked chat__composer-locked--link"
              onClick={onOpenDetail}
            >
              {t.chat.composerOfflineWake(member.name)}
            </button>
          ) : (
            <div className="chat__composer-locked" role="status">
              {t.chat.composerOffline(member.name)}
            </div>
          )
        ) : (
          <>
            {/* T-94c1 wake row: shown while offline/stopped — an honest "your
             * message will queue" notice plus an in-place ⚡喚醒 button (calls
             * activateMember via onWake). Sits ABOVE the composer so the input
             * row stays full-width (owner mockup). The button is wired only when
             * the caller passes onWake (a member, not an outsource worker). */}
            {offlineQueue && (
              <div className="chat__wake-row">
                <span className="chat__wake-row__hint">
                  <MoonIcon size={14} />
                  {t.chat.wakeQueueHint(member.name)}
                </span>
                {onWake && (
                  <button
                    type="button"
                    className="chat__wake-btn"
                    onClick={() => {
                      setWakePending(true);
                      setWakeUndispatched(false);
                      // 🔴 WHOSE wake this is (review r2 SHOULD-1). The
                      // peer-keyed reset effect above is a reset, not a CANCEL:
                      // an activate still in flight when the owner switches
                      // peers resolves AFTER the reset and writes A's verdict
                      // into a room that is already B's. `peerIdRef` is the
                      // render-time mirror of the CURRENT peer.
                      const firedFor = member.id;
                      // Revert the optimistic pending if the activate POST
                      // rejects (else the button sticks on "喚醒中…" forever) —
                      // same discipline as MemberDetailPanel's wake. The success
                      // path is cleared by the lifecycle→!offlineQueue effect.
                      //
                      // 🔴 T-7fa1: a resolved activate is NOT proof a START went
                      // out. Reading activation_pending is what stops this button
                      // from sitting on 「喚醒中…」 for a wake nobody sent.
                      Promise.resolve(onWake())
                        .then((result) => {
                          if (peerIdRef.current !== firedFor) return;
                          if (result?.activationPending) {
                            setWakePending(false);
                            setWakeUndispatched(true);
                          }
                        })
                        .catch(() => {
                          if (peerIdRef.current !== firedFor) return;
                          setWakePending(false);
                        });
                    }}
                    disabled={wakePending}
                  >
                    <BoltIcon size={15} />
                    <span>
                      {wakePending ? t.chat.wakePending : t.chat.wakeButton}
                    </span>
                  </button>
                )}
              </div>
            )}
            {/* T-7fa1: the in-chat wake has its OWN optimistic state, so it needs
                its own outcome — the same notice the detail panel raises. */}
            {offlineQueue && wakeUndispatched && (
              <DispatchAlert kind="wake" testId="chat-wake-undispatched" />
            )}
            {(pendingAttachments.length > 0 || attachError) && (
              <ComposerAttachmentPreview
                pendingAttachments={pendingAttachments}
                attachError={attachError}
                onRemove={removeAttachment}
                onOpenImage={(src) => setLightboxSrc(src)}
              />
            )}
            <div className="chat__composer-row">
              {/* Hidden native file input the attach button triggers. */}
              <input
                ref={fileInputRef}
                className="chat__file-input"
                type="file"
                accept={ATTACH_ACCEPT}
                multiple
                onChange={onPickFile}
                hidden
              />
              <button
                type="button"
                className="chat__attach"
                aria-label={t.chat.attachLabel}
                title={t.chat.attachLabel}
                onClick={() => fileInputRef.current?.click()}
              >
                <PaperclipIcon size={18} />
              </button>
              {/* Multi-line composer: Enter sends, Shift+Enter breaks a line
               * (onKeyDown only intercepts a bare Enter — a shifted one falls
               * through to the textarea's native newline). Height follows the
               * draft via the autosize layout-effect above. */}
              <textarea
                ref={inputRef}
                className="chat__input"
                rows={1}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onCompositionStart={() => {
                  isComposingRef.current = true;
                }}
                onCompositionEnd={(e) => {
                  isComposingRef.current = false;
                  // compositionend delivers the final committed text; sync the draft
                  // so the last composed chunk is never dropped (React's controlled
                  // onChange during composition is unreliable across browsers).
                  setDraft(e.currentTarget.value);
                }}
                onKeyDown={onKeyDown}
                onPaste={onPaste}
                placeholder={t.chat.inputPlaceholder(member.name)}
              />
              <button
                type="button"
                className="chat__send"
                aria-label={t.chat.send}
                onClick={() => void submit()}
                disabled={!canSend}
              >
                <SendIcon size={16} />
              </button>
            </div>
          </>
        )}
      </footer>

      {/* Full-size overlay — the SHARED Lightbox (backdrop / × / Esc close). */}
      <Lightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
      {/* .md preview overlay (T-a1c4) — in-cockpit markdown render, shared with
       * the task artifact popover; separate action from the download chip. */}
      {mdPreview && (
        <MarkdownPreviewOverlay
          title={mdPreview.title}
          url={mdPreview.url}
          onClose={() => setMdPreview(null)}
        />
      )}
    </div>
  );
}
