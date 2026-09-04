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
  OutsourceWorkerView,
} from "../api/adapter";
import { autosizeTextarea } from "../lib/autosize";
import { getChatDraft, saveChatDraft } from "../lib/chatDraftStore";
import { useChat } from "../hooks/useChat";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { formatDayLabel, splitByDay } from "../lib/dateFormat";
import {
  ATTACH_ACCEPT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import { useWindowActive } from "../hooks/useWindowActive";
import { useIsMobile } from "../hooks/useIsMobile";
import { enterShouldSend } from "../lib/composerKeys";
import { AttachmentStrip } from "./AttachmentStrip";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { ChatGalleryPanel } from "./ChatGalleryPanel";
import { ChatReplyCard } from "./ChatReplyCard";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { Markdown } from "./Markdown";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { useQuotedMessageOverlay } from "../hooks/useQuotedMessageOverlay";
import { PresenceBadge } from "./PresenceBadge";
import { CurrentTaskTitle } from "./CurrentTaskTitle";
import {
  BoltIcon,
  ChevronRightIcon,
  CloseIcon,
  ExpandIcon,
  ImageIcon,
  MoonIcon,
  PaperclipIcon,
  ReplyIcon,
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

// ⚠️ `oneLine()` USED TO LIVE HERE AND WAS DELETED 2026-08-21. It collapsed
// newlines and runs of spaces in the 「正在回覆」 banner's excerpt, and its own
// comment said the reason was that "a multi-line excerpt would push the composer
// around as the owner re-aims". That failure could not happen: office.css puts
// `white-space: nowrap` on `.chat__reply-banner__text`, which the body inherits,
// so the browser already collapses every newline to a space and lays the body
// out on one line. (The banner became TWO lines on 2026-08-22 — who on one, the
// excerpt on the next — which changes nothing here: each half is still one line
// box and `nowrap` is still what makes the body's newlines collapse.) Measured in a real Chromium at 390px — banner height
// 34px with a collapsed body and 34px with a deliberately un-collapsed one
// carrying two blank lines and a run of spaces. Mutating the function's body to
// `return body;` left all 2284 frontend tests green: it was the only surviving
// mutant in the whole frontend pass.
//
// So the layout rule has exactly one owner now, and it is the stylesheet.
// `.chat__reply-banner__text { white-space: nowrap }` is load-bearing and has a
// witness: deleting it turns the CT 「正在回覆」 banner test red, and the CT story
// feeds that banner a body WITH A NEWLINE so the collapse itself — not just the
// clipping — is what the one-line assertion measures.
//
// It fed no `title` attribute and nothing else, so nothing else moved with it.

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
  // The LIVE outsource workers, the sender-label twin of `members`. This list
  // is LIVE-ONLY by construction — HandleListOutsourceWorkersApiOutsourceWorkersGet
  // skips WorkerStatusReleased — so it is NOT what rescues a released sender;
  // `useWorkerCodenames` below is. Its two jobs here are both about live ids:
  //   1. `nameOf` reaches it only AFTER `members` failed to resolve the id, so
  //      it is the codename source for a caller that passes no roster (the
  //      prop is optional) or whose roster has not loaded yet — without it
  //      such a sender's label degrades to its raw ow- id while the left rail
  //      shows the codename.
  //   2. it is the EXCLUSION SET behind `unknownOwIds`: every ow- participant
  //      NOT in this list is handed to the lazy per-id codename read. Passing
  //      the live list is what keeps that per-id read off the live workers.
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
  // Locate + highlight this message once the thread loads. One-shot per id —
  // later SSE refetches never re-scroll.
  //
  // 🔴 THE COCKPIT ROUTES HERE AGAIN (owner 2026-08-29: 「1 跟 2 變回去原本那
  // 樣」). The 請示 page's 跳到原訊息 and the inline task card's 在聊天室回覆
  // both write #office/chat/<id>/msg/<msgId> once more; only the chat bubble's
  // 看原訊息 takes the overlay (hooks/useQuotedMessageOverlay).
  //
  // ⚠️ AND THE KNOWN COST CAME BACK WITH THEM, knowingly: this path can only
  // find a row the thread has already PAINTED. When the target is outside the
  // loaded window the search misses and the reader lands on the newest message
  // with nothing on screen saying so. The owner accepted that trade on
  // 2026-08-29 and parked the fix (「無法跳回去很久以前訊息的問題我們改天再
  // 說」). The "honest miss" below is honest in that it never fabricates a
  // location — it is not honest to the READER, and that gap is deliberate, not
  // an oversight to patch in passing.
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
  // header line under the sub — owner 圖2: the selected worker's header shows
  // the complete task title, untruncated. An outsource worker's title rides
  // OutsourceWorkerView.taskTitle. Absent/"" ⇒ nothing rendered (a released /
  // taskless peer never grows an empty line here).
  headerTaskTitle?: string;
}) {
  const { t, msg } = useI18n();
  const isOffline = member.status === "offline";
  // T-9c3c (owner 2026-07-24, "有時候離線還是沒辦法發訊息"): a REAL roster member
  // (onWake wired) can ALWAYS be messaged — the server NEVER gates on recipient
  // presence (api_chat.PutChat lands the message regardless, UnreadCounts counts
  // it, the member reads it on next boot). So the composer's ONLY lock reason is
  // "no queue path at all": a synthetic released/removed peer (read-only, T-661b
  // — it must never grow a typable composer or a false "will queue" promise) or
  // an outsource worker; both are deliberately passed NO onWake by OfficePage.
  //
  // This REVERSES T-94c1's extra lock on waking/stopping (owner 2026-07-17),
  // which was the intermittent "sometimes offline can't be messaged" bug: an
  // offline member reads lifecycle `waking` for the wake's configured TTL after ANY
  // wake attempt (the ⚡喚醒 button itself included) and `stopping` while it
  // winds down — both are transient presence states an offline member passes
  // through, and the message the "dying session could miss it" rationale worried
  // about is the SAME message the server was going to queue anyway. Locking on
  // them dropped a message the design says must always send. Presence-driven:
  // `member` comes from the SSE-refetched roster, so a lifecycle flip re-renders
  // without a reload. Reads the five-state `lifecycle`, not the collapsed
  // tri-state `status`.
  const hasQueuePath = !!onWake;
  const composerLocked = !(member.lifecycle === "online" || hasQueuePath);
  // Non-online but messageable (a live member that is offline/stopped/waking/
  // stopping): composer unlocked, with the queue notice + in-place wake row
  // above the input (owner mockup). Online needs neither; a peer with no queue
  // path is locked above and never reaches here.
  const offlineQueue = hasQueuePath && member.lifecycle !== "online";
  // Wake-click instant feedback: the activate POST only writes the wake INTENT;
  // presence flips to waking via SSE shortly after. Optimistically disable the
  // button meanwhile so a double-tap can't fire two activates.
  const [wakePending, setWakePending] = useState(false);
  // T-7fa1: the activate reported that nothing was dispatched. Distinct from
  // wakePending — "not waiting, because nothing was sent". Never both true.
  const [wakeUndispatched, setWakeUndispatched] = useState(false);
  // Reset the optimistic bridge whenever REALITY moves — the peer changes, or
  // this member's lifecycle takes a new value. Once presence reflects a fresh
  // lifecycle the local optimism has handed off to the real state (`waking`
  // drives the label below), so a dispatched-but-silently-died wake (waking→
  // offline after the configured waking TTL) clears instead of latching "喚醒中…" forever.
  // 🔴 `member.id` IS a dependency, not decoration (review r1 SHOULD-1):
  // OfficePage renders <ChatArea> WITHOUT a key (frontend/CLAUDE.md), so a peer
  // switch is a prop change, not a remount — without keying on the peer, A's
  // optimistic notice would linger on B's now-shared wake row. Keying on
  // `member.lifecycle` (T-9c3c) replaces the old `offlineQueue` dep, which no
  // longer flips across offline↔waking now that the wake row shows for both.
  useEffect(() => {
    setWakePending(false);
    setWakeUndispatched(false);
  }, [member.lifecycle, member.id]);
  // The wake row's button shows "喚醒中…" while a wake is in flight — either the
  // just-clicked optimism, or the server-confirmed `waking` presence itself.
  const wakeInFlight = wakePending || member.lifecycle === "waking";

  const {
    messages,
    messagesPeer,
    peerLastReadTs,
    send,
    markRead,
    hasMore,
    loadOlder,
    gapSuspected,
  } = useChat(member.id);

  // Released-worker codenames: an ow- participant that is NOT in the live
  // `workers` list (task closed → dropped off) still has a codename on the
  // per-id read — resolve it lazily so the label never degrades to the raw id.
  const unknownOwIds = useMemo(() => {
    const out = new Set<string>();
    for (const m of messages) {
      // 🔴 THE QUOTED SENDER IS A PARTICIPANT TOO. `m.replyToChat.from` is an
      // id this thread RENDERS (`quoteWho = nameOf(quoted.from)`), so leaving it
      // out of this set meant the codename fallback never fired for it: the very
      // same released outsource worker showed a codename on its own row and a
      // raw `ow-…` id when quoted — and the quote row's aria-label read
      // 「引用 ow-8808ccf51794」. One display path, two identities.
      for (const id of [m.from, m.to, m.replyToChat?.from ?? ""]) {
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

  // The owner's own display name, taken from the ONE place the cockpit already
  // resolved it (App's useOwnerName, handed down by OwnerNameProvider). Read
  // through context rather than by mounting the hook again: this component must
  // not fetch while it paints — ChatArea.quote-no-fetch.test.tsx asserts the api
  // client is touched zero times to render a thread.
  const ownerDisplayName = useOwnerDisplayName(t.user);
  // Resolve a participant id → display name: prefer a roster match, else the raw
  // id (never fabricate). The window's own `member` is always resolvable even if
  // it is not in the passed roster.
  const nameOf = (id: string): string => {
    if (id === member.id) return member.name;
    // 🔴 THE OWNER HAS A NAME TOO, AND IT IS THE ONE HE SET. T-4e95 is the first
    // display path that feeds the owner's OWN id into nameOf — replying to your
    // own message names the sender in the composer banner and in the quote row —
    // and without this branch it fell through to the raw id and printed
    // 「正在回覆 owner」.
    //
    // 🔴 `t.user` IS THE THEME'S DEFAULT WORD FOR THE HUMAN, NOT HIS NAME —
    // 「CEO（你）」 as shipped, 「市長（你）」 under the 仙俠 theme — and the
    // nickname he actually set lives server-side behind /api/settings
    // (hooks/useOwnerName). Printing the default here while his own profile pill
    // reads 「韓立（你）」 renders one person under two names on one screen; the
    // owner reported exactly that from the running cockpit. It is a regression
    // of this ticket, not old debt: this branch is what T-4e95 added.
    //
    // `ownerDisplayName` resolves to the stored nickname when there is one and
    // to `t.user` otherwise — INCLUDING when the settings read failed, because a
    // failure must never masquerade as "no name set" (useOwnerName's own rule).
    if (id === OWNER_ID) return ownerDisplayName;
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
    if (codename !== undefined) return msg.outsourceLabel(codename);
    return id;
  };
  // 「寄件者 → 收件者」 — the ONE spelling of a message's direction in this
  // component. The message rows have written it this way for inter-agent
  // traffic since before T-4e95; the quote row and the composer banner now use
  // the SAME join, so a reader never meets two ways of saying who-to-whom.
  const directionLabel = (from: string, to: string): string =>
    `${nameOf(from)} → ${nameOf(to)}`;
  // The shared 看原訊息 exit. Declared here rather than at the top of the
  // component because it is handed `nameOf`, and `nameOf` needs the roster
  // hooks above it. It is still an unconditional top-level hook call.
  const quotedMessage = useQuotedMessageOverlay(nameOf);
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
  // T-4e95 「回覆這則」: the message the composer is currently replying to, or
  // null in the ordinary send state. It rides the DRAFT store, not just this
  // component's state, for the same reason the text does — a 跳頁-and-back that
  // restored the words but silently dropped the reply target would send the
  // message somewhere the owner did not aim it, and look like a normal restore
  // while doing it.
  const [replyToId, setReplyToId] = useState<string | null>(
    () => getChatDraft(member.id)?.replyTo ?? null,
  );
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
  // What the in-cockpit full-view overlay is showing (null = closed). THREE
  // ways in, one surface: a stored ATTACHMENT (T-a1c4 — the overlay fetches the
  // blob, offers 下載 and a share link, so it carries the blob's id), an
  // incoming MESSAGE body (the corner 放大閱讀 button — the text is already in
  // hand, so there is nothing to fetch, download or share), or a STAGED image
  // still sitting in the composer (T-f014 — the bytes are in hand as a data:
  // URI, so 下載 is honest but no blob id exists to share). The kind is carried
  // explicitly so no branch has to be guessed from which field happens to be
  // set.
  const [mdPreview, setMdPreview] = useState<
    | {
        kind: "attachment";
        title: string;
        url: string;
        attachmentId: string;
        // Carried even though NOTHING constructs this variant today (no setter
        // in this file builds kind:"attachment"; the two that exist build
        // "message" and "staged-image"). It is here so that reviving the branch
        // cannot silently lose the attachment's TYPE: without a mime the
        // overlay falls back to markdown, and a file whose type matters would
        // be drawn as the wrong thing — with nothing going red. Keeping the
        // field costs one line; discovering the
        // omission costs a reader believing a wrong screen.
        mime?: string;
      }
    | { kind: "message"; title: string; source: string }
    | { kind: "staged-image"; title: string; imageSrc: string }
    | null
  >(null);
  // 「看原訊息」 — reading that one message and showing it whole is NOT this
  // component's business any more (T-0b78). It lives in
  // hooks/useQuotedMessageOverlay. ⚠️ The quote row on a chat bubble is now the
  // hook's ONLY caller: the 請示 page and the inline task card went back to
  // NAVIGATING (owner 2026-08-29), so do not describe this as a shared exit.
  // The hook is called below, once `nameOf` exists — it titles the overlay with
  // the roster-aware name this window already resolves.
  // M2-3 file & image gallery panel (header icon toggles it).
  const [galleryOpen, setGalleryOpen] = useState(false);
  // The attachment whose share link was just copied (transient 「已複製」
  // feedback on that one button; null = none).
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
  // Phone viewport → Enter inserts a newline instead of sending (no physical
  // keyboard, so Shift+Enter is impossible); sending is via the send button.
  const isMobile = useIsMobile();
  // A message may carry text and/or attachments — sendable when EITHER present.
  const canSend = draft.trim().length > 0 || pendingAttachments.length > 0;

  // The composer is a multi-line textarea (desktop: Enter sends, Shift+Enter
  // breaks a line; mobile: Enter breaks a line — see onKeyDown). Auto-grow to
  // the draft on EVERY draft change —
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
    // 🔴 THE TARGET MUST SWAP WITH THE PEER, and since 2026-08-21 the reason is
    // the OPPOSITE of the one that used to be written here. The server had a
    // `sameChatConversation` check and refused a cross-conversation `reply_to`
    // with a 400, so forgetting this line was noisy, visible, and left the draft
    // intact. That check is GONE (owner ruling: quoting sideways into another
    // conversation is the use case). Forgetting this line now SENDS SUCCESSFULLY
    // — a message to the new peer carrying a quote row built from the old
    // conversation, which the server faithfully assembles and shows the
    // recipient. The guard got MORE load-bearing when the refusal went away, not
    // less: do not remove it on the belief that the server still catches this.
    setReplyToId(restored?.replyTo ?? null);
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
    saveChatDraft(member.id, {
      text: draft,
      attachments: pendingAttachments,
      replyTo: replyToId ?? undefined,
    });
  }, [member.id, draft, pendingAttachments, replyToId]);

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

  // ===== T-4e95 quote resolution =====
  //
  // 🔴 THERE IS NO RESOLUTION ANY MORE, AND THAT IS THE WHOLE REDESIGN (owner
  // ruling, 2026-08-21). A reply arrives carrying `replyToChat` — the quoted
  // sender and a server-shortened line of what they said — built by the server
  // on every read, without exception. The row reads that field and stops.
  //
  // What used to be here: the wire carried the quoted ID alone, so this
  // component looked the target up in the loaded window and, failing that, went
  // and fetched it (useQuotedMessages, now deleted). That fetch could fail; a
  // failure was drawn as a placeholder that was sometimes a lie; the lie was
  // repaid on the next inbound SSE event. Each of those was a branch, and all of
  // them paint the SAME PIXELS whether they are right or wrong — which is why
  // the bugs that lived in them survived twenty rounds of review. Do not
  // reintroduce a lookup here however cheap it looks: the value of this shape is
  // not that it is fast, it is that it has exactly one behaviour.
  //
  // `messageById` survives for ONE job, and it is not the quote text: whether
  // the COMPOSER's banner can NAME the person being answered, which is a
  // question about what is loaded right now and can only be answered here.
  //
  // 🔴 IT NO LONGER GATES THE QUOTE ROW'S CONTROL. It did — the row offered the
  // control only when the target was in this map, and back then it was labelled
  // 「跳到原訊息」 because it scrolled rather than opened. The same owner ruling
  // that deleted the resolution deleted that gate too: the control is offered on
  // every reply, is labelled 「看原訊息」, and reads its one message back on click
  // (`quotedMessage.open`, hooks/useQuotedMessageOverlay). The render condition is `m.replyTo && quoted`; it
  // does not consult `messages`.
  const messageById = useMemo(
    () => new Map(messages.map((m) => [m.id, m])),
    [messages],
  );
  // The message the COMPOSER is aiming at, resolved from the loaded window
  // ALONE — no fetch, no fallback, no third state. The owner picks this target
  // by clicking a row that is on screen, so it is in the window by construction.
  // The one path that can miss is a draft restored from an earlier session whose
  // target has since scrolled out; that renders as the same fixed sentence every
  // other unshowable quote does, and SENDING IT STILL WORKS — the server
  // resolves the id, and the sent row comes back with its quote attached.
  const replyQuote = replyToId ? messageById.get(replyToId) : undefined;

  // 🔴 THERE IS NO `locateMessage` ANY MORE, AND THE OTHER JUMP IS NOT IT.
  // The quote row used to scroll the thread to the quoted row when that row
  // happened to be loaded, and show no control when it was not. Owner ruling
  // 2026-08-21 replaced that with 「撈那一則、跳 modal」
  // (hooks/useQuotedMessageOverlay — this row is its only caller since the two
  // cards went back to navigating on 2026-08-29): one behaviour for every reply
  // in THIS thread, no window-dependent affordance, and
  // no scroll — which also retired the "the jump moves the viewport but not the
  // FOCUS, so a keyboard user pressing it saw nothing happen" defect, because
  // there is nothing left to scroll.
  //
  // ⚠️ WHAT SURVIVES, AND MUST: the `jumpToMsgId` reactor below. That is the
  // hash-route jump (#office/chat/<id>/msg/<msgId>), a different entry point
  // with a different job — it lands the thread on a named row on ENTRY — and it
  // owns `highlightMsgId` and the `--located` flash. It never called
  // locateMessage. Deleting one did not touch the other, and
  // `ChatArea.unread-jump.test.tsx` plus the reply-card jump tests pin it.

  // The hash-route jump — declared BEFORE the entry-positioning reactor below so
  // the jump consumes entry positioning first (the divider/bottom scroll must
  // not fight the located message). One-shot per target id; a target outside the
  // loaded recent window falls back to the plain land-at-bottom (honest miss —
  // the thread still opens, and nothing pretends the target was found).
  //
  // ⚠️ Its callers are the 請示 page's 跳到原訊息, the inline task card's
  // 在聊天室回覆, and any URL somebody kept (bookmark, pasted link, restored
  // tab) — see the prop's note for the miss this can still land on.
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
    // The divider is the actual unread boundary.  Keeping older context above
    // it can push the first unread row outside a compact chat viewport.
    divider?.scrollIntoView({ block: "start" });
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

  // Esc handling for the full-view overlay lives inside MarkdownPreviewOverlay.

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
    const replyToSnapshot = replyToId;
    // 🔴 WHICH ROOM THIS SEND BELONGS TO. The restore below runs after an await,
    // and the owner can switch peers during it — this component is REUSED across
    // peers, so the restore landed in whoever was on screen when the failure came
    // back, and the save effect then persisted it into THAT peer's draft. The
    // reply target makes it worse than untidy, and worse than it used to be: the
    // server's `sameChatConversation` refusal was deleted on 2026-08-21, so a
    // target from another room is no longer 400'd — it is accepted, and the new
    // room's message goes out quoting a sentence from a conversation it has
    // nothing to do with, which the recipient then reads as context. The failure
    // mode flipped from a visible refusal to a silent mis-send.
    const sendPeer = member.id;
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
    // Cleared with the rest of the composer: a reply target that survived its
    // own send would silently attach itself to the NEXT message too.
    setReplyToId(null);
    try {
      await send(
        draftSnapshot,
        attachments.length > 0 ? attachments : undefined,
        replyToSnapshot ?? undefined,
      );
    } catch (e) {
      console.warn("ChatArea: send failed, restoring composer", e);
      // 🔴 RESTORE INTO THE ROOM IT WAS TYPED IN — NOT the one on screen, and
      // NOT nowhere. The first version of this guard was a bare `return` on the
      // reasoning that "that room's draft still holds it". IT DOES NOT: the
      // optimistic clear at the top of submit() runs while `member.id` is still
      // the sending room, so the save effect calls saveChatDraft(sendPeer, {all
      // empty}) — and an all-empty draft is DELETED, not stored. So the bare
      // return traded "restored into the wrong room" (ugly, visible, and the
      // words are still on screen) for "text, attachments AND reply target
      // silently gone for good, with only a console.warn". That is worse, and
      // it is exactly what the guard was added to prevent.
      //
      // Writing to the store rather than to state also covers the case where
      // this component is gone entirely (跳頁 mid-flight): setState on an
      // unmounted component discards the content just as quietly.
      //
      // FIELD BY FIELD, which is the rule the state restores below already use:
      // fill only what the room does not already hold. The first version of
      // this write was all-or-nothing on the whole draft, and a reviewer found
      // the gap that opens: go back to that room, stage one image and type
      // nothing, and the room is no longer "empty" — so the whole write was
      // skipped and the failed message's TEXT and reply target went with it.
      //
      // What this still cannot save: if the owner has retyped in that room,
      // their words win and the failed message's words are gone. Two texts
      // cannot occupy one composer, and theirs is the one they can see. Said
      // out loud rather than left to be discovered.
      const stored = getChatDraft(sendPeer);
      saveChatDraft(sendPeer, {
        text: stored && stored.text ? stored.text : draftSnapshot,
        attachments:
          stored && stored.attachments.length > 0
            ? stored.attachments
            : attachmentsSnapshot,
        replyTo:
          stored && stored.replyTo
            ? stored.replyTo
            : (replyToSnapshot ?? undefined),
      });
      // Not this room any more → the words are back where they were typed, and
      // putting them on screen here would put one room's words, and one room's
      // reply target, into another.
      if (peerIdRef.current !== sendPeer) return;
      // Restore the user's unsent content so it isn't silently lost. Only put
      // back what the user hasn't already retyped/restaged — if they started a
      // new draft while the send was in flight, don't clobber it.
      setDraft((cur) => (cur ? cur : draftSnapshot));
      restoreAttachments(attachmentsSnapshot);
      // Same rule as the text: put the target back only if the owner has not
      // already aimed at something else while the send was in flight.
      setReplyToId((cur) => (cur ? cur : replyToSnapshot));
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // The send decision (IME gate, mobile-newline, Shift+Enter) is the shared
    // enterShouldSend rule so all three composers stay in lockstep. When it
    // returns false on a mobile Enter we deliberately do NOT preventDefault, so
    // the textarea inserts a native newline.
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
      e.preventDefault();
      void submit();
    }
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
      m.to !== OWNER_ID ? directionLabel(m.from, m.to) : nameOf(m.from);
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
    // T-4e95 ① the QUOTE LINE — a thin row above the bubble saying which
    // message this one answers. It reads `m.replyToChat` and NOTHING ELSE: the
    // server built that snapshot on this very read, so there is nothing to look
    // up and nothing that can still be pending.
    //
    const quoted = m.replyToChat ?? null;
    // 🔴 WHO SAID IT **AND WHO THEY SAID IT TO**. `from` alone reads as though
    // the quoted line had been said in this thread, and since 2026-08-21 that is
    // exactly the case it gets wrong: the owner may quote a line two OTHER
    // members exchanged in order to step into it, and the quote row then named a
    // sender while silently implying the wrong listener.
    //
    // The recipient is the QUOTED MESSAGE's own (`quoted.to`, server-projected
    // on this very read) — NEVER this window's peer, which is the plausible
    // wrong answer and is wrong precisely when the quote crosses conversations.
    //
    // There is no third rendering when a name does not resolve: `nameOf` already
    // falls back to the raw id, so both halves always have characters to print.
    const quoteWho = quoted ? directionLabel(quoted.from, quoted.to) : "";
    // 🔴 TWO OUTCOMES, NO THIRD. Either the server sent the snapshot or the
    // original is gone — there is no "not yet", because nothing is in flight.
    // The gone sentence is FIXED: not retried, not refreshed, and not revisited
    // when the next event lands.
    //
    // `quoted.content` may legitimately be "" (the original carried only
    // attachments). That renders as a named quote with an empty line, which is
    // the truth; it must NOT be folded into the gone sentence, because "there
    // was nothing to quote" and "there is nothing to quote FROM" are different
    // facts about the conversation.
    const quoteText = quoted ? quoted.content : t.chat.replyQuoteGone;
    const quoteLine = !m.replyTo ? null : (
      /* 🔴 THE ONE THING THIS ROW EXISTS TO SAY HAS TO REACH THE A11Y TREE TOO.
       * Measured in a real browser on a real <ChatArea>: as a bare <div> this
       * row linearised into the reply as "Mira. Mira. 他說的. 跳到原訊息.
       * 我說的" — role null, no name. (That transcript is verbatim from the
       * measurement; the button said 「跳到原訊息」 then and says 「看原訊息」
       * now. The shape is the point, not the string.) A screen-reader user
       * could not tell
       * which sentence is the quotation and which one this person is saying
       * now, which is the whole feature. `.chat__msg-quote` is the only place
       * in this frontend that embeds someone else's sentence inside another
       * person's message, so the gap is this feature's own, not the app's.
       *
       * role="blockquote" + aria-label, NOT a visually-hidden prefix: this repo
       * has no sr-only utility (MemberCard.presence-a11y.test.tsx says so in as
       * many words), and inventing one for a single row would be a new global
       * primitive smuggled in under a quote line. The label names the quoted
       * sender when we have resolved one and stays generic when we have not —
       * the same "no quote, no name" rule the banner and `quoteWho` already
       * follow. */
      <div
        className="chat__msg-quote"
        data-testid="msg-quote"
        role="blockquote"
        aria-label={
          quoteWho ? t.chat.replyQuoteRoleWho(quoteWho) : t.chat.replyQuoteRole
        }
      >
        {/* Decorative twin of the label above it: the row already SAYS it is a
         * quote through its aria-label, so an unnamed <img> node in the tree
         * beside it is pure noise. Only this one is hidden — the rest of the
         * app's icons are a separate, pre-existing question. */}
        {/* 🔴 LINE 1 — WHO SAID IT TO WHOM, and the control. Owner ruling
         * 2026-08-22 (「換成兩行？ 一行是誰跟誰說話 一行是內容？」): the row is
         * two lines, so 「→ 收件者」 and the quoted sentence stop competing for
         * the same horizontal space. Before that ruling they shared one line and
         * the recipient half was pure loss for the excerpt: measured in the
         * running app at vw=721 (pane 347px), English, a 5-character sender —
         * `.chat__msg-quote__who` 101px, `.chat__msg-quote__body` 18px, 3 of 61
         * characters left, and 0 on the CI runner's fonts. The addressee is not
         * optional (it is what the field exists for), so the line is what gave. */}
        <div className="chat__msg-quote__head">
          <ReplyIcon
            size={11}
            className="chat__msg-quote__icon"
            aria-hidden="true"
          />
          {quoteWho && <span className="chat__msg-quote__who">{quoteWho}</span>}
          {/* The control is its own element, the way 查看任務詳情 is on a
           * task-derived ask (ReplyCardTaskRef) — owner 2026-08-20.
           *
           * 🔴 IT IS OFFERED FOR EVERY REPLY, UNCONDITIONALLY (owner ruling
           * 2026-08-21: 「全部統一就撈那一則顯示出來就好」). It used to appear only
           * when the quoted row was already in the loaded window, on the argument
           * that an affordance which scrolls nowhere is worse than none — true of
           * a control that SCROLLS. This one does not scroll: it reads that one
           * message back from the server and opens it in the full-view overlay,
           * so it works identically for a quote from ten seconds ago and one from
           * ten thousand messages ago. The window-membership question is gone, and
           * with it the row's only piece of local, disagreeable state.
           *
           * The row still shows the SERVER's 60-rune excerpt; the overlay shows
           * the whole body. Nothing here re-cuts anything.
           *
           * ⚠️ ONE CONDITION SURVIVES, AND IT IS NOT THE WINDOW ONE. `quoted` —
           * the server's snapshot — must be present. When it is absent this row is
           * printing 「這則訊息已不存在」, which is the server's own answer from
           * THIS read: the original is gone. Offering a control there would be
           * offering to open a message we have just told the reader does not
           * exist, and pressing it could only ever produce 「拿不到這則訊息」 one
           * line below. That is not the window check coming back through a side
           * door — the window is never consulted — it is the row declining to
           * contradict itself.
           *
           * 🔴 THE LABEL IS ITS OWN ELEMENT so it can be the thing that
           * DISAPPEARS. It is not trimmed and it never was made trimmable in the
           * end: nothing in office.css can ellipsise it — the button is
           * `flex: none` with `white-space: nowrap`, and the label's ONLY rule is
           * `display: none` inside `@container chat-pane (max-width: 520px)`. So
           * on a narrow pane the whole label goes and the arrow is what is left;
           * on a wide one the label renders whole. Whole or absent, never cut.
           *
           * The control used to keep its intrinsic width on the reasoning that a
           * cut 跳到原訊息 helps nobody — true of the Chinese control, which was
           * 69px. The English one at the time was "Go to the original message" at
           * 154px (both figures are the WHOLE BUTTON: label + 2px gap + 12px
           * chevron). Today the labels read 「看原訊息」 / "View the original
           * message" — `d7752781` renamed them with the behaviour — and the
           * English control measures ~151px, so the pressure is unchanged. A
           * control that cannot give way does not stay politely inside the
           * bubble: it
           * runs past the edge and under the corner buttons, which are absolutely
           * positioned and therefore painted on top of it. Measured against the
           * running app: it fails at the narrow end in BOTH languages, and again
           * just past the two-column breakpoint on an INCOMING bubble WITH A BODY
           * (`!mine && m.body` → `--acts2`) — that one reserves 56px of corner
           * where your own reserves 32. Two conditions, not one: `!mine &&
           * m.body` is what widens the corner, and `m.replyTo` is what puts a
           * quote row there to overflow. The worst case is both together. Two earlier versions of this note quoted exact ranges;
           * both were wrong, because the range moves with the bubble kind, the
           * language and the display name. The guard holds the numbers.
           * Dropping the whole label and keeping its arrow beats a control hidden
           * under another control. */}
          {m.replyTo && quoted && (
            <button
              type="button"
              className="chat__msg-quote__jump"
              data-testid="msg-quote-jump"
              /* 🔴 THE NAME CANNOT RIDE ON THE VISIBLE LABEL. That label is the
               * first thing this row gives up when the PANE runs short (see the
               * note above), and it does not shrink on the way out — below 520px
               * of pane it is `display: none` outright. A name riding on it would
               * not degrade, it would VANISH, leaving a button whose only content
               * is a decorative chevron and whose accessible name is the empty
               * string. Naming the control explicitly is what keeps it named in
               * the half of the width range where the label is not rendered at
               * all. */
              aria-label={t.chat.replyQuoteJump}
              title={t.chat.replyQuoteJump}
              onClick={() => void quotedMessage.open(m.replyTo as string)}
            >
              <span className="chat__msg-quote__jump-label">
                {t.chat.replyQuoteJump}
              </span>
              <ChevronRightIcon
                size={12}
                className="chat__msg-quote__jump-chevron"
              />
            </button>
          )}
          {/* The failure sentence comes from the hook, so it lands beside the
           * button that was pressed, and NEVER over the quote line, whose
           * sentence is a claim about whether the original EXISTS. (It used to
           * be shared with the 請示 page and the inline task card; those two
           * navigate again since 2026-08-29 and have no fetch to fail.) */}
          {quotedMessage.failureNotice(m.replyTo as string)}
        </div>
        {/* 🔴 LINE 2 — THE SENTENCE, WITH THE WHOLE ROW TO ITSELF. This is the
         * only thing on the row that says WHAT is being answered, and since the
         * two-line split nothing above it can take its width: it starts at the
         * row's left edge and runs to the right edge, still clipped to one line
         * (a quotation is not this message's to grow). */}
        <span className="chat__msg-quote__body" title={quoteText}>
          {quoteText}
        </span>
      </div>
    );

    // T-4e95 ② the REPLY ENTRY. Owner 2026-08-20 moved it INTO the bubble's
    // corner, beside 放大閱讀: out on the row it read as something belonging to
    // the thread rather than to this message.
    //
    // The reason it started on the row is still true and had to be solved
    // rather than argued with — 放大閱讀's corner exists ONLY on incoming text
    // bubbles, and the AC is 每一則. So the corner is now a SHARED ACTION SLOT
    // that every bubble reserves (own messages and attachment-only bubbles
    // included), holding one or two controls.
    //
    // 🔴 THE ONE SHAPE THAT KEEPS THE ROW ENTRY is a reply-card message: its
    // bubble is replaced by <ChatReplyCard>, a full-width surface with its own
    // header controls, and hanging a floating action over it would collide with
    // them. Stated here rather than silently: card rows are the exception.
    //
    // 🔴 OFFERED ON EVERY ROW IN THE WINDOW, INCLUDING THE ONES THE OWNER IS
    // NOT A PARTY TO. This used to be gated behind a `replyable` flag —
    // {owner, peer} rows only — because the server refused a reply_to that
    // crossed conversations, so an entry on an inter-agent row would have 400'd
    // on every press. The owner removed that refusal on 2026-08-21 FOR THIS
    // EXACT CASE: 「引用另外兩個人對話裡的一句話來介入詢問」. With the gate gone
    // the entry works there — the reply is addressed to this thread's peer as
    // always, and it quotes the line the owner pointed at. Keeping the flag
    // would have left the owner's ruling unreachable from the product.
    const replyEntry = (
      <button
        type="button"
        className="chat__msg-reply"
        aria-label={t.chat.replyAction}
        title={t.chat.replyAction}
        onClick={() => {
          setReplyToId(m.id);
          inputRef.current?.focus();
        }}
      >
        <ReplyIcon size={13} />
      </button>
    );

    const content = m.replyCardId ? (
      <ChatReplyCard
        replyCardId={m.replyCardId}
        fallbackSummary={m.body}
        initialStatus={m.replyCardStatus}
      />
    ) : (
      <div
        className={
          "chat__msg-bubble" +
          // The corner ACTION SLOT reserves its own width so a hover can never
          // reflow the text under it. Two controls need more room than one, and
          // 放大閱讀 is the one that comes and goes.
          (!mine && m.body
            ? " chat__msg-bubble--expandable chat__msg-bubble--acts2"
            : " chat__msg-bubble--acts1")
        }
      >
        {/* The bubble's corner actions (T-4e95). ONE slot, so the two controls
         * cannot drift apart into two corners:
         *   • 回覆這則 — on EVERY bubble in the window, both directions, and
         *     including the inter-agent rows the owner is not a party to. That
         *     last part is new (2026-08-21): the server used to refuse a
         *     reply_to that crossed conversations so the entry was withheld
         *     there, and the owner removed that refusal precisely so the owner
         *     could quote a line out of two other people's thread and step in.
         *   • 放大閱讀 — reopens THIS message body in the shared full-view
         *     overlay. Only on INCOMING messages with text: an agent answer is
         *     the long-form side of the thread (the owner's own line is what
         *     they just typed), and an attachment-only bubble has no body to lay
         *     out — the file chip already carries its own 預覽 action. */}
        <div className="chat__msg-actions">
          {replyEntry}
          {!mine && m.body && (
            <button
              type="button"
              className="chat__msg-expand"
              aria-label={t.chat.expandMessage}
              title={t.chat.expandMessage}
              onClick={() =>
                setMdPreview({
                  kind: "message",
                  title: senderLabel,
                  source: m.body,
                })
              }
            >
              <ExpandIcon size={12} />
            </button>
          )}
        </div>
        {quoteLine}
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
        {/* Stored attachments — one click target, opening the shared popup. */}
        <AttachmentStrip
          attachments={m.attachments}
          className="chat__msg-attachments"
          itemClassName="chat__msg-attachment"
          imageClassName="chat__msg-image chat__msg-image--clickable"
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
            <div className="chat__msg-content">
              {m.replyCardId && quoteLine}
              {content}
            </div>
            {m.replyCardId && replyEntry}
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
              <div className="chat__msg-content">
                {m.replyCardId && quoteLine}
                {content}
              </div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">{formatTime(m.ts)}</span>
              </div>
              {m.replyCardId && replyEntry}
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
              : msg.chatInterAgentExpand(group.messages.length)}
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
        {/* T-3738 / T-ea81: the header avatar's kind follows the peer's REAL
         * role — an outsource peer (ow- id) shows the theme's 外包 image, an
         * assistant the 助理 image, a 正職 peer the member image. Rendering
         * member for an outsource peer fabricated a 正職 identity. */}
        <Avatar size={38} kind={avatarKindForMember(member)} src={member.avatarUrl} />
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
              {/* 🔴 T-b0bb: THE GAP NOTICE COMES FIRST, AND IT SUPPRESSES
               * "已到最早訊息".
               *
               * `hasMore` answers one narrow question — "might there be more
               * history ABOVE the loaded window?" — and "已到最早訊息" is its
               * honest negative answer. But a reader does not read it that
               * narrowly: beside a thread with a hole punched in its MIDDLE it
               * reads as "you have the whole conversation", which is false.
               *
               * Measured on the pre-fix code: after a 40-message burst and a
               * full walk backwards, the thread was missing 10 rows in the
               * middle and `hasMore` was false — i.e. the UI actively declared
               * completeness over a hole. That is the exact shape this pair of
               * branches exists to prevent, so they are mutually exclusive by
               * construction rather than by CSS or ordering. */}
              {gapSuspected ? (
                <div className="chat__gap-notice" role="status">
                  <span>{t.chat.gapSuspected}</span>
                </div>
              ) : (
                !hasMore && (
                  <div className="chat__history-start" role="note">
                    <span>{t.chat.historyStart}</span>
                  </div>
                )
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
              {msg.chatOfflineTitle(member.name)}
            </div>
            {/* T-94c1: offline/stopped can now be messaged (queues until wake),
             * so the hint no longer says "喚醒後才能開始對話" (which contradicted
             * the unlocked composer below). The wake entry + queue notice live on
             * the composer's wake row now, not on this card. */}
            <div className="chat__offline-hint">
              {offlineQueue
                ? msg.chatOfflineQueueHint(member.name)
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
          /* T-9c3c: the composer locks ONLY for a peer with NO queue path — a
           * synthetic released/removed peer (read-only, T-661b) or an outsource
           * worker; OfficePage wires neither onWake nor a queue promise for
           * them. A live member always has a queue path (onWake), so it never
           * reaches here. A plain, non-clickable notice: there is nothing to
           * wake and no live detail panel to open for these peers. */
          <div className="chat__composer-locked" role="status">
            {msg.chatComposerOffline(member.name)}
          </div>
        ) : (
          <>
            {/* Wake row: shown for a live member in ANY non-online state
             * (offline/stopped/waking/stopping, T-9c3c) — an honest "your
             * message will queue" notice plus an in-place ⚡喚醒 button (calls
             * activateMember via onWake). Sits ABOVE the composer so the input
             * row stays full-width (owner mockup). The button is wired only when
             * the caller passes onWake (a member, not an outsource worker). */}
            {offlineQueue && (
              <div className="chat__wake-row">
                <span className="chat__wake-row__hint">
                  <MoonIcon size={14} />
                  {msg.chatWakeQueueHint(member.name)}
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
                      // path hands off to the real `waking` presence, and the
                      // lifecycle-keyed reset effect clears the optimism.
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
                    disabled={wakeInFlight}
                  >
                    <BoltIcon size={15} />
                    <span>
                      {wakeInFlight ? t.chat.wakePending : t.chat.wakeButton}
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
                onOpenImage={(att) =>
                  setMdPreview({
                    kind: "staged-image",
                    title: att.filename || t.chat.pastedImageAlt,
                    imageSrc: att.dataUri,
                  })
                }
              />
            )}
            {/* T-4e95 ③ 「正在回覆」 — the LINE-style banner ABOVE the input
             * row, naming who is being answered and quoting a slice of what
             * they said, with an x that returns the composer to the ordinary
             * send state.
             *
             * 🔴 THE x CLEARS THE TARGET AND NOTHING ELSE. Half-typed text and
             * staged attachments stay exactly as they are — cancelling a reply
             * is not cancelling the message, and a composer that emptied itself
             * here would lose work the owner never asked to throw away. */}
            {replyToId && (
              <div className="chat__reply-banner" data-testid="chat-reply-banner">
                <ReplyIcon size={13} className="chat__reply-banner__icon" />
                {/* 🔴 DO NOT NAME SOMEONE WE HAVE NOT RESOLVED. This used to
                  * fall back to the peer's name whenever the quote had not come
                  * back, which is a claim, not a placeholder: the target is by
                  * construction one of TWO people (this conversation has only
                  * two), so the fallback was a coin flip printed as a fact.
                  *
                  * There used to be a THIRD state here — a 「…」 meaning "the
                  * by-id read has not landed yet". It went with the read: nothing
                  * is in flight any more, so a spinner would never resolve.
                  *
                  * 🔴 AND IT IS NOT THE QUOTE ROW'S SENTENCE EITHER. `replyQuote`
                  * comes from `messageById` — the LOADED WINDOW — so an unresolved
                  * target here does NOT mean the message is gone. Scroll back, aim
                  * at an old row, switch peers and come back to a freshly-loaded
                  * newest page: the target is still there, the send still succeeds,
                  * and the quote comes back whole on the reply's own row. Printing
                  * 「這則訊息已不存在」 in that state is a falsifiable claim about
                  * the world made from a fact about this browser's scroll position.
                  * `replyingToEarlier` is state-independent and stays true in both
                  * cases. */}
                <span className="chat__reply-banner__text">
                  <span className="chat__reply-banner__who">
                    {/* The same 「寄件者 → 收件者」 the quote row draws, off
                      * the LOADED message this banner resolves (see above) —
                      * so aiming at a line from another conversation says whose
                      * line it was, here as well as on the sent row. */}
                    {replyQuote
                      ? t.chat.replyingTo(
                          directionLabel(replyQuote.from, replyQuote.to),
                        )
                      : t.chat.replyingToEarlier}
                  </span>
                  <span className="chat__reply-banner__body">
                    {/* Raw, not pre-collapsed: `white-space: nowrap` on the
                      * parent is what makes this one line — see the note where
                      * `oneLine()` used to be. */}
                    {replyQuote ? replyQuote.body : ""}
                  </span>
                </span>
                <button
                  type="button"
                  className="chat__reply-banner__x"
                  aria-label={t.chat.replyCancel}
                  title={t.chat.replyCancel}
                  onClick={() => {
                    setReplyToId(null);
                    // 🔴 GIVE THE FOCUS BACK. This button is about to unmount
                    // itself, and a focused element that leaves the document
                    // hands focus to <body> — so a keyboard user who cancels
                    // one reply is thrown to the top of the page and has to Tab
                    // back through the whole thread to reach the composer. The
                    // reply ENTRY already does this on the way in; the way out
                    // was missing.
                    inputRef.current?.focus();
                  }}
                >
                  <CloseIcon size={14} />
                </button>
              </div>
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
              {/* Multi-line composer. Desktop: Enter sends, Shift+Enter breaks a
               * line. Mobile: Enter breaks a line and the send button sends
               * (onKeyDown → enterShouldSend; when it doesn't send it lets the
               * keydown fall through to the textarea's native newline). Height
               * follows the draft via the autosize layout-effect above. */}
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

      {/* The ONE full-view overlay (T-a1c4, T-f014) — in-cockpit render, shared
       * with the task artifact popover. A stored attachment rides the blob url +
       * its id (the overlay fetches it and keeps its 下載 and share links); a
       * 放大閱讀 message rides the body text this component already holds; a
       * staged composer image rides its data: URI. */}
      {mdPreview &&
        (mdPreview.kind === "attachment" ? (
          <MarkdownPreviewOverlay
            title={mdPreview.title}
            url={mdPreview.url}
            attachmentId={mdPreview.attachmentId}
            mime={mdPreview.mime}
            onClose={() => setMdPreview(null)}
          />
        ) : mdPreview.kind === "staged-image" ? (
          <MarkdownPreviewOverlay
            title={mdPreview.title}
            imageSrc={mdPreview.imageSrc}
            onClose={() => setMdPreview(null)}
          />
        ) : (
          <MarkdownPreviewOverlay
            title={mdPreview.title}
            source={mdPreview.source}
            onClose={() => setMdPreview(null)}
          />
        ))}
      {/* The 看原訊息 overlay is the shared exit's own — same surface, one
       * owner for the read behind it (hooks/useQuotedMessageOverlay). */}
      {quotedMessage.overlay}
    </div>
  );
}
