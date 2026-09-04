import {
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useI18n } from "../i18n";
import type { Member, MemberActivateResult } from "../types";
import type {
  ChatMessage,
  OutsourceWorkerView,
} from "../api/adapter";
import { autosizeTextarea } from "../lib/autosize";
import {
  getChatDraft,
  saveChatDraftText,
  subscribeChatDraft,
  updateChatDraftAttachments,
} from "../lib/chatDraftStore";
import { useChat } from "../hooks/useChat";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { formatDayLabel, splitByDay } from "../lib/dateFormat";
import {
  ATTACH_ACCEPT,
  CHAT_MAX_ATTACHMENTS,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import { useWindowActive } from "../hooks/useWindowActive";
import { useIsMobile } from "../hooks/useIsMobile";
import { enterShouldSend } from "../lib/composerKeys";
import { chatBottomAffordance } from "../lib/chatBottomAffordance";
import { scrollToLatest, isLatestRowInView } from "../lib/scrollToLatest";
import { AttachmentStrip } from "./AttachmentStrip";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { ChatGalleryPanel } from "./ChatGalleryPanel";
import { ChatJumpLatestButton } from "./ChatJumpLatestButton";
import { ChatThreadLoading } from "./ChatThreadLoading";
import { ChatNewMsgPreview } from "./ChatNewMsgPreview";
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

/** 🔴 EVERYTHING THIS COMPONENT TRACKS FOR ONE CONVERSATION, IN ONE RECORD.
 *
 * 🔴 IT IS PER MOUNT, AND SINCE R13-5 THAT IS THE SAME THING AS PER
 * CONVERSATION. `OfficePage` mounts this component under `key={peerId}`, so a
 * switch UNMOUNTS it and the next conversation gets a new component with a new
 * record — React's own machinery, not a second copy of it built here.
 *
 * This record used to be rebuilt in place by `useKeyedRecord`, keyed on the
 * peer, because the component was reused across conversations. Every field
 * below is still per-conversation; nothing about what it holds changed. What
 * went away is the rebuilding: twelve reviews' worth of "is this still my
 * visit?" machinery existed to make one component instance behave like one
 * instance per conversation, which is what a `key` already means (R13-5).
 *
 * ⚠️ WHAT DOES NOT BELONG IN HERE: DOM refs (`inputRef`, `messagesRef`, …) and
 * anything mirroring live browser state (`isComposingRef`). Each of those is
 * annotated where it is declared. */
type ChatSession = {
  /** ② ENTRY POSITIONING: entering a conversation with unread messages must
   * land on the FIRST unread message, not the bottom. The anchor is derived
   * from `member.unreadCount` (the roster badge count) SNAPSHOT at
   * conversation entry — the race-free source. Since T-48 the LISTING no
   * longer writes a watermark, but the window that opens on it does: the
   * read-receipt effect fires the moment the first page lands and the roster's
   * unreadCount refetches to 0 right after. The clearer moved from the
   * server's side effect to this component's own explicit write; the race did
   * not go away, so neither does the snapshot. unreadCount counts exactly the
   * peer→owner messages above the watermark, so the first unread = the
   * earliest of the LAST `unreadCount` peer→owner messages in the thread. */
  initialUnread: number;
  /** Is the scroll viewport near its bottom? A new incoming message may only
   * pull the view down when it is — if the owner scrolled UP to read history,
   * an arrival must NOT yank them back. */
  nearBottom: boolean;
  /** Ids seen on the previous messages render — the diff basis for "which
   * messages are NEW" (a refetch replaces the whole array, so append detection
   * must go through ids, not length). */
  prevIds: Set<string>;
  /** T-bf82 scrollback: the pre-fetch scroll-geometry snapshot an older-page
   * prepend restores from (null = no older page in flight/pending). */
  prependAnchor: { firstId: string; height: number; top: number } | null;
  /** The UI-side in-flight lock over `useChat`'s own, so repeated scroll
   * events near the top cannot re-snapshot `prependAnchor` mid-flight.
   *
   * 🔴 IT IS IN THIS RECORD BECAUSE IT USED TO BE A CROSS-PEER MODULE-ISH REF
   * (fourth-review R4-3), and the argument for leaving it that way was "the
   * try/finally releases it after one request either way". That is only true
   * of a promise that SETTLES: `api.listChat` has neither a timeout nor an
   * AbortController (http.ts gives a deadline to the SSE probe and to nothing
   * else), so one hung GET froze scrollback in EVERY conversation for the rest
   * of the session, with no spinner and no error. In this record a hung
   * request strands only the conversation it was started on. */
  loadingOlder: boolean;
  /** One-shot: entry positioning (bottom OR first-unread) ran here. */
  initialPositioned: boolean;
  /** Is the CURRENT unread run (the block below the divider) still OPEN — i.e.
   * the owner has not reached the bottom since the divider anchored? While
   * open, further arrivals belong to the SAME run (the divider stays put).
   * Once closed (bottom reached = everything seen), the next unseen inbound
   * starts a NEW run and RE-ANCHORS the divider — the chip and the divider
   * share ONE "start of the new messages" anchor. */
  unreadRunOpen: boolean;
  /** Entry positioning wants the divider scrolled into view ONCE. A
   * chip-driven divider re-anchor must NOT scroll — the owner is reading
   * history and must never be yanked. */
  entryScrollPending: boolean;
  /** B3 跳到原訊息: the jump target already consumed (one-shot per id — an SSE
   * refetch must never re-scroll). */
  jumpConsumed: string | null;
  /** The target this component has ALREADY spent an anchor-window fetch on
   * (T-48 ③). Separate from `jumpConsumed` on purpose: the fetch is what makes
   * the jump possible, so it happens BEFORE the jump is consumed, and this is
   * what stops the effect firing a second pair of requests on every re-render
   * while the first pair is still in flight. */
  jumpFetched: string | null;
  /** 🔴 THE BUDGET IS NOT THE TRIGGER (T-48, R3-5). `jumpRetry` state exists
   * only to re-run the reactor, so it can never go back down —
   * `setJumpRetry(0)` from an already-0 state re-renders nothing and the retry
   * button would do NOTHING AT ALL. The budget therefore lives here, and the
   * button resets it: a person who asks for another try gets a full one, not
   * the remains of the automatic ones. */
  autoJumpRetries: number;
  /** T-e987 compose seed: the seed value already applied (one-shot per
   * distinct value, so the same taskNo can seed another peer). */
  seedConsumed: string | null;
  /** Set when 回到最新 had to FETCH the live tail first (T-48 ③) — consumed by
   * the settle effect once the replacement thread has rendered.
   *
   * 🔴 IN THIS RECORD, not cross-peer: inherited by the next conversation it
   * would scroll a room the owner just entered AT AN ANCHOR straight to the
   * live tail — this ticket's own failure shape, arriving from the previous
   * conversation's button press. */
  pendingLatestScroll: boolean;
};

function freshChatSession(unreadCount: number): ChatSession {
  return {
    initialUnread: unreadCount,
    nearBottom: true,
    prevIds: new Set(),
    prependAnchor: null,
    loadingOlder: false,
    initialPositioned: false,
    unreadRunOpen: false,
    entryScrollPending: false,
    jumpConsumed: null,
    jumpFetched: null,
    autoJumpRetries: 0,
    seedConsumed: null,
    pendingLatestScroll: false,
  };
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
  // 🔴 THE PER-CONVERSATION MUTABLE STATE (T-48). One record for this mount,
  // and this mount IS one conversation: `OfficePage` renders this component
  // under `key={peerId}` (R13-5), so entering another room builds a new
  // component and a new record, and an async job started by the room the owner
  // left settles into an orphan nobody reads.
  //
  // The entry unread snapshot lives in it, taken synchronously at the first
  // render, strictly before any effect runs.
  const sessionRef = useRef<ChatSession | null>(null);
  if (sessionRef.current === null) {
    sessionRef.current = freshChatSession(member.unreadCount);
  }
  const session = sessionRef.current;

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
  //
  // Per conversation ("A's optimistic notice must not linger on B's now-shared
  // wake row"), which a plain `useState` says on its own now that the component
  // is remounted per conversation (R13-5). It used to need a reset effect keyed
  // on `member.id`, and that effect ran one commit AFTER the frame that already
  // showed the previous conversation's 「喚醒中…」.
  const [wakePending, setWakePending] = useState(false);
  // T-7fa1: the activate reported that nothing was dispatched. Distinct from
  // wakePending — "not waiting, because nothing was sent". Never both true.
  const [wakeUndispatched, setWakeUndispatched] = useState(false);
  // The OTHER thing that clears the optimistic bridge: reality moving on this
  // member. Once presence reflects a fresh lifecycle the local optimism has
  // handed off to the real state (`waking` drives the label below), so a
  // dispatched-but-silently-died wake (waking→offline after the configured
  // waking TTL) clears instead of latching 「喚醒中…」 forever. The peer half of
  // this effect's old dependency list is gone — the record does it, earlier.
  useEffect(() => {
    setWakePending(false);
    setWakeUndispatched(false);
  }, [member.lifecycle]);
  // The wake row's button shows "喚醒中…" while a wake is in flight — either the
  // just-clicked optimism, or the server-confirmed `waking` presence itself.
  const wakeInFlight = wakePending || member.lifecycle === "waking";

  // Threshold (px) within which the viewport counts as "at the bottom" for
  // auto-follow and the read watermark.
  const NEAR_BOTTOM_PX = 80;

  // 🔴 HAS THE READER LOOKED AT THE LIVE TAIL OF THIS THREAD? See `mayMarkRead`
  // for why this exists and why `hasNewer` cannot answer it any more. React
  // state rather than a `session` field on purpose: `mayMarkRead` is computed
  // during render and the read-receipt effect depends on it, so a change has to
  // re-render to be seen.
  const [tailSeen, setTailSeen] = useState(true);

  // The scroll viewport.
  const messagesRef = useRef<HTMLDivElement>(null);

  const {
    messages,
    peerLastReadTs,
    send,
    markRead,
    initialLoading,
    hasMore,
    loadOlder,
    gapSuspected,
    hasNewer,
    loadAround,
    resetToLatest,
    // 🔴 ANCHOR-FIRST ENTRY (T-48, owner ruling). The target is named at
    // SUBSCRIPTION time, so a room entered through 跳到原訊息 / a kept link never
    // loads the live tail first and then throws it away — see useChat's note.
    // The fetch itself still happens below, in the jump reactor, because the
    // viewport, the highlight and the miss notice are this component's business.
  } = useChat(member.id, jumpToMsgId);


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
  // T-8aaa draft survival: seed the text from the per-peer draft store. Both
  // ways back into a room are the same event now — a 跳頁-then-return and a
  // conversation switch both MOUNT this component (R13-5), so one lazy init
  // covers both and there is no second restore path to keep consistent with it.
  const [draft, setDraft] = useState(() => getChatDraft(member.id)?.text ?? "");
  // 🔴 THE TEXT IS SUBSCRIBED TOO, NOT JUST READ ONCE (T-48, R14-1.3). The
  // staged files have been a subscription since R13-2; the text was still a
  // lazy init, and a lazy init is a place a message can WAIT but never a place
  // it can ARRIVE. What arrives is a FAILED SEND: submit() clears the composer
  // optimistically, and when the POST comes back with an error the failure arm
  // writes the words back into this room's draft — deliberately into the store
  // rather than into state, because the owner may have walked out. Walk out and
  // back in before the failure lands (a switch to another room and back is
  // enough — that is a fresh mount) and the store held the words while the
  // composer showed an empty box; the next keystroke persisted the empty box
  // over them. The message was gone, and the owner never saw it at all.
  //
  // ONLY INTO AN EMPTY COMPOSER, which is the same field-by-field rule the
  // failure arm itself uses: what the owner can see and is typing wins, because
  // two texts cannot occupy one composer.
  const storedDraftText = useSyncExternalStore(
    useCallback(
      (cb: () => void) => subscribeChatDraft(member.id, cb),
      [member.id],
    ),
    useCallback(() => getChatDraft(member.id)?.text ?? "", [member.id]),
  );
  useEffect(() => {
    if (storedDraftText === "") return;
    setDraft((cur) => (cur === "" ? storedDraftText : cur));
  }, [storedDraftText]);
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
  //
  // 🔴 THIS ROOM'S FILES ARE THIS ROOM'S DRAFT (T-48, R13-2). Naming the peer
  // here names the slot in `chatDraftStore` the rows live in, and the hook
  // reads that slot through `useSyncExternalStore`. A `FileReader` started here
  // commits into this peer's slot whatever is on screen when it finishes —
  // another room, or no room at all because the page was left — and this
  // composer repaints the moment anything is written into the slot it is
  // showing. That is R9-1, R10-4 and R11-2 all at once, with nothing to
  // remember and nothing to restore.
  const {
    pendingAttachments,
    attachError,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging(member.id);
  // What the in-cockpit full-view overlay is showing (null = closed). TWO ways
  // in, one surface: an incoming MESSAGE body (the corner 放大閱讀 button — the
  // text is already in hand, so there is nothing to fetch, download or share),
  // or a STAGED image still sitting in the composer (T-f014 — the bytes are in
  // hand as a data: URI, so 下載 is honest but no blob id exists to share). The
  // kind is carried explicitly so no branch has to be guessed from which field
  // happens to be set.
  //
  // ⚠️ THERE USED TO BE A THIRD, `kind: "attachment"` (a STORED blob), and it
  // had no caller (T-48, R11-1). A stored attachment's chip is rendered by
  // `AttachmentStrip`, which mounts its OWN `MarkdownPreviewOverlay` — so the
  // branch was dead code that read like the live path, and the tenth review's
  // measurement of a leaking document preview was filed against this state
  // while the overlay it measured was the strip's. Deleted rather than left as
  // documentation of an intention.
  // 🔴 IT MUST NOT OUTLIVE THE CONVERSATION (T-48, R10-1). It once carried a
  // written exemption — `.md-preview`'s full-screen backdrop blocks every
  // gesture that could change the peer — and the premise was false: the site
  // routes on the hash (`OfficePage`'s `useHashRoute`, whose `route.chatId` IS
  // the selected peer), so back/forward and any link into another conversation
  // swapped `member` without the backdrop being touched. Measured: open A's
  // document preview, switch to B — the header said Bruno while the overlay
  // still showed A's filename and A's content.
  //
  // Remounting per conversation (R13-5) is also what makes the unguarded async
  // landing points inside `MarkdownPreviewOverlay` structurally safe: the
  // overlay cannot outlive the conversation, so none of its writers can either.
  const [mdPreview, setMdPreview] = useState<
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
  //
  // 🔴 IT MUST NOT OUTLIVE THE CONVERSATION EITHER (T-48, R9-2). The exemption
  // it once shared with `mdPreview` ("the overlay covers the page, so the
  // switch gesture is blocked") was doubly false here: `.chat__gallery` is
  // `position: absolute; right: 0; width: min(340px, 100%)` — a side panel
  // inside the chat column with no backdrop, and the roster is fully clickable
  // beside it. Measured: open A's gallery, click B in the roster, and the
  // header said Bruno while the panel still showed A's files labelled with A's
  // sender name.
  const [galleryOpen, setGalleryOpen] = useState(false);
  // The attachment whose share link was just copied (transient 「已複製」
  // feedback on that one button; null = none).
  // Inter-agent (agent↔agent) groups that the owner has EXPANDED (keyed by the
  // group's first-message id). Collapsed is the default — a group is expanded
  // only once its id lands here, so the owner opts in per block.
  //
  // 🔴 IT HOLDS MESSAGE IDS, SO IT MUST NOT OUTLIVE THE CONVERSATION (T-48,
  // R11-9). `groupExpanded` asks `has(m.id)` of whatever is on screen, and the
  // only thing that ever stood between this set and a wrongly-expanded block in
  // another room was that message ids happen to be globally unique today — a
  // property of the data, not of this structure, and the set never shrinks.
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
  // ⚠️ NOT in the session record: it mirrors a LIVE DOM event pair
  // (compositionstart/compositionend) rather than anything about the
  // conversation, and clearing it from outside would desync it from the
  // browser's own composition state.
  const isComposingRef = useRef(false);
  // Phone viewport → Enter inserts a newline instead of sending (no physical
  // keyboard, so Shift+Enter is impossible); sending is via the send button.
  const isMobile = useIsMobile();
  // 🔴 OVER THE COUNT CAP THE COMPOSER REFUSES THE SEND ITSELF (T-48, R11-3).
  // A draft is allowed to hold more than CHAT_MAX_ATTACHMENTS — that is R10-3's
  // fix, and files waiting in a draft are somebody's data, not a rule to
  // enforce. Sending them is a different act, and it is the one the cap is
  // about. The server refuses an over-cap send with a 400, but this app has no
  // toast, no error row behind `submit()`'s catch and no global rejection
  // reporter, so the refusal was invisible: the send button stayed lit and
  // every press did nothing at all, with no hint that two files had to go.
  // Refusing here is the same notice staging already raises, on the surface the
  // owner is looking at, BEFORE a message can be lost to a silent 400.
  const overAttachmentCap = pendingAttachments.length > CHAT_MAX_ATTACHMENTS;
  // A message may carry text and/or attachments — sendable when EITHER present.
  const canSend =
    (draft.trim().length > 0 || pendingAttachments.length > 0) &&
    !overAttachmentCap;

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
  const endRef = useRef<HTMLDivElement>(null);

  // ===== LINE/FB-style unread jump (M2 batch 19) =====
  //
  // The per-visit session record is declared at the top of this component (it
  // is also the visit token every state and guard below is bound to).
  // Set once per conversation when entry positioning ran: the id of the first
  // unread message. Drives the "以下是未讀訊息" divider (kept for the whole
  // session, like LINE) and the initial scroll target.
  const [firstUnreadId, setFirstUnreadId] = useState<string | null>(null);
  // ① IS THE NEWEST MESSAGE IN THE VIEWPORT? The round 回到最新訊息 arrow's
  // ONLY condition (owner card rc-72054864ff88) — not "scrolled more than a
  // screen", not "a new message arrived". Measured from the scroll viewport's
  // own geometry in `onMessagesScroll` and wherever this component moves the
  // viewport itself. Starts true: every entry path lands at the bottom or
  // measures honestly before this can be read.
  const [latestInView, setLatestInView] = useState(true);
  // ② THE NEW-MESSAGE PREVIEW STRIP's content — the LATEST unseen inbound
  // message (sender + body), or null when there is nothing waiting.
  //
  // 🔴 THE LATEST, NOT THE FIRST, AND IT IS REPLACED RATHER THAN QUEUED. The
  // pill this replaces said a constant sentence and so had nothing to update;
  // a strip that names a sender and quotes a line must show the CURRENT one,
  // and there must only ever be one of it (owner screenshot). The FIRST unseen
  // message keeps its own job — anchoring the 「以下是未讀訊息」 divider below —
  // which is why the two are tracked separately.
  const [newMsgPreview, setNewMsgPreview] = useState<{
    id: string;
    from: string;
    body: string;
  } | null>(null);
  // 🔴 T-48: the jump target the server has NO RECORD OF ("missing"), and — a
  // DIFFERENT fact that used to be collapsed into it — an anchor fetch that was
  // repeatedly OVERTAKEN by newer loads ("interrupted"). The fallback (open at
  // the bottom) is indistinguishable from a jump that worked, which is the very
  // silence this ticket exists to remove — so the outcome is state, and state is
  // rendered. A `console.warn` is not a user-visible thing.
  const [jumpNotice, setJumpNotice] = useState<
    null | "missing" | "unreachable" | "interrupted"
  >(null);
  // How many times a jump may be re-scheduled after being overtaken. `load()`
  // is held off for the duration of the anchor fetch, so losing the race even
  // once takes a deliberate 回到最新 or a send; three is a ceiling on a loop,
  // not a budget anybody is expected to spend.
  const MAX_JUMP_RETRIES = 3;
  // 🔴 T-48 (F3): the anchor fetch was OVERTAKEN, which is not the same fact as
  // "the message is gone" and must not be reported as one. Bumping this state
  // re-runs the reactor below — a ref alone would not, and the retry would sit
  // there until some unrelated render happened to carry it. BOUNDED: without a
  // ceiling a load that keeps winning the race turns "retry" into an unbounded
  // fetch loop, which is a worse failure than the one being fixed.
  const [jumpRetry, setJumpRetry] = useState(0);
  // The transient highlight on the row a jump located (cleared after the flash).
  const [highlightMsgId, setHighlightMsgId] = useState<string | null>(null);

  // T-8aaa: persist the TYPED half of the draft to the per-peer store on every
  // change, so an unmount (跳頁 or a switch to another room) leaves the latest
  // draft behind. An all-empty draft deletes the entry, giving the "送出 /
  // 手動清空後歸零" behaviour for free.
  //
  // 🔴 IT CANNOT TOUCH THE STAGED FILES, AND THAT IS THE POINT (T-48, R13-2 —
  // R11-2's fix made structural). This effect used to write text AND
  // attachments together, from this component's own state, so a file filed into
  // the draft by a read that finished after this composer had read it was
  // destroyed by the next keystroke. The files are no longer this component's
  // to write back: they live in the store, and `saveChatDraftText` leaves them
  // exactly where they are.
  useEffect(() => {
    saveChatDraftText(member.id, draft, replyToId ?? undefined);
  }, [member.id, draft, replyToId]);

  // T-e987 compose seed: prefill the composer once with "[<taskNo>] " when the
  // 任務卡 label routes here, but only into an EMPTY draft (never overwrite
  // what the owner is mid-typing). One-shot per distinct seed value.
  useEffect(() => {
    if (!draftSeed) return;
    if (session.seedConsumed === draftSeed) return;
    session.seedConsumed = draftSeed;
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
    if (session.loadingOlder || !hasMore) return;
    const el = messagesRef.current;
    if (!el || messages.length === 0) return;
    session.loadingOlder = true;
    session.prependAnchor = {
      firstId: messages[0].id,
      height: el.scrollHeight,
      top: el.scrollTop,
    };
    try {
      await loadOlder();
    } finally {
      session.loadingOlder = false;
    }
  }

  // Prepend scroll compensation + session-tracker bookkeeping. useLayoutEffect
  // (not useEffect) so the scrollTop fix lands BEFORE paint — no visible jump.
  // Runs before the scroll-position reactor below (layout effects precede
  // passive effects in a commit), so registering the prepended ids into
  // session.prevIds here keeps the reactor's "fresh message" diff honest: loaded
  // HISTORY is not fresh — it must never arm the new-message chip nor
  // re-anchor the unread divider.
  useLayoutEffect(() => {
    const anchor = session.prependAnchor;
    if (!anchor) return;
    if (messages.length === 0) return;
    const idx = messages.findIndex((m) => m.id === anchor.firstId);
    if (idx <= 0) {
      // idx === 0: nothing prepended (yet) — an unrelated append committed
      // while the older page is in flight; keep waiting on the anchor.
      // idx === -1: the anchor row vanished (peer data reset) — drop it.
      if (idx === -1) session.prependAnchor = null;
      return;
    }
    session.prependAnchor = null;
    for (let i = 0; i < idx; i++) session.prevIds.add(messages[i].id);
    const el = messagesRef.current;
    if (el) el.scrollTop = anchor.top + (el.scrollHeight - anchor.height);
    // The one-shot entry positioning (session.initialPositioned) already ran for
    // this conversation — a prepend must never re-run it, and it doesn't:
    // the latch stays untouched here.
  }, [messages]);

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
    // 🔴 THERE IS NO 「捲到底再撈一頁」 BRANCH HERE ANY MORE (T-48 fix12). The
    // mirror of the top branch used to live at this exact spot: near the bottom
    // of an anchor window, buy ONE forward page, one page per gesture. The jump
    // now arrives already joined to the live tail (`loadAround` fetches through
    // before it commits), so there is no window to walk out of — and the whole
    // apparatus that made 「一次手勢一頁」 true (a clamp-direction test on
    // `session.lastScrollTop`, and in `useChat` a retry clock, a trailing
    // replay, a sight gate, a served-anchor marker) went with it.
    //
    // ⚠️ WHEN THE THREAD *IS* STILL SHORT OF THE TAIL — the fetch above gave up
    // part-way — scrolling to the bottom does NOT buy a page any more. The way
    // on is the 回到最新 arrow, which `hasNewer` keeps on screen for exactly
    // that case. That is deliberate: an affordance that is always visible beats
    // a gesture a scroller pinned at its limit cannot even emit (measured in
    // Chromium, review #20).
    // ① The arrow's condition, and it is a DIFFERENT question from
    // `nowNearBottom`: auto-follow may forgive 80px, but the owner asked for
    // the arrow whenever the NEWEST MESSAGE is not in the viewport. So it is
    // measured on that row, not on `distance` — this box also contains the flex
    // gap and the `endRef` sentinel that sit BELOW the newest row, and counting
    // those as "content still below the fold" is what kept the arrow on screen
    // after the jump had already put the newest message fully in view. See
    // `isLatestRowInView`.
    setLatestInView(isLatestRowInView(el));
    // Crossing into the bottom band = the owner has now read to the latest → mark
    // the newest message read (monotonic server-side; safe to fire repeatedly).
    //
    // 🔴 `mayMarkRead` because in an ANCHOR WINDOW the bottom of the BOX is not
    // the bottom of the THREAD — see where it is derived. There is no forward
    // walk to resume from any more (T-48: `loadAround` fetches through to the
    // live tail before it commits), so the case this guards is the one where
    // that fetch could NOT finish and the thread was committed short anyway.
    if (nowNearBottom && !session.nearBottom && newestTs > 0 && mayMarkRead) {
      void markRead(newestTs);
    }
    // Reaching the bottom means the preview strip's message has been seen →
    // drop it (no-op when already null), and the current unread run is CLOSED —
    // the next unseen inbound starts a new run (divider re-anchors).
    if (nowNearBottom) {
      setNewMsgPreview(null);
      session.unreadRunOpen = false;
      // 🔴 THE READER IS AT THE BOTTOM OF THE THREAD — the one thing that
      // honestly ends a post-jump watermark block (see `mayMarkRead`). Guarded
      // so a wheel resting in the band does not re-render on every event.
      if (!tailSeen) setTailSeen(true);
    }
    session.nearBottom = nowNearBottom;
  }

  // The newest message ts in the thread — the watermark the owner marks read up
  // to (0 when empty).
  const newestTs = messages.length > 0 ? messages[messages.length - 1].ts : 0;

  // 🔴 T-48 — MAY THE OWNER'S READ WATERMARK BE MOVED RIGHT NOW? Two ways it
  // must not be, and both are the same mistake: stamping "seen" on messages
  // nobody has looked at (owner ruling — mark-read is 「我看過了」, not 「我跳過
  // 來過」).
  //
  //   • `hasNewer` — the thread is an ANCHOR WINDOW from the middle of the
  //     history. `newestTs` is that window's last row and everything between it
  //     and the live tail is unfetched, unseen material.
  //   • 🔴 `tailSeen` — THE READER HAS NOT LOOKED AT THE LIVE TAIL (T-48
  //     fix12). This is the half that used to be carried by `hasNewer` and can
  //     no longer be: a jump used to leave the thread SHORT of the tail and
  //     walk home one reader gesture at a time, so 「握得到活尾巴」 and 「看過活
  //     尾巴」 were the same fact and `hasNewer` stood for both. `loadAround`
  //     now fetches through to the tail before it commits, so the moment the
  //     jump lands `hasNewer` is FALSE while the reader is parked hundreds of
  //     rows above — and the effect below, which does not look at the viewport
  //     at all, would stamp the watermark at the newest message. Every message
  //     between the anchor and the tail would be marked 「我看過了」 by an owner
  //     who has seen none of them (owner ruling: mark-read is 「我看過了」, not
  //     「我跳過來過」).
  //
  //     So the guard asks the honest question directly instead of through a
  //     proxy that used to correlate with it: has the reader been at the bottom
  //     of this thread at all? It starts TRUE (an ordinary entry lands at the
  //     tail and the existing behaviour is unchanged), goes FALSE when a jump
  //     starts fetching, and comes back TRUE on each of the three things that
  //     mean the reader really is at the latest — crossing into the bottom
  //     band, pressing 回到最新 / the preview strip, and a jump that MISSED and
  //     fell back to the tail.
  //   • a jump still PENDING — arriving through 跳到原訊息 / a kept link mounts
  //     the thread on the NEWEST window first, and the anchor fetch replaces it
  //     a moment later. That first window is on screen for no time at all and
  //     the reader is on their way somewhere else entirely; marking it read
  //     would consume the whole unread run before the jump has even landed,
  //     which is worse than the anchor-window case, not milder.
  //
  // Nothing is lost by waiting — the watermark is monotonic. Walking forward,
  // the 回到最新 arrow, and a jump that finishes (landed OR missed — both consume
  // the latch) each end the block, which is exactly when the owner really is
  // looking at the latest.
  const jumpPending =
    jumpToMsgId !== undefined && session.jumpConsumed !== jumpToMsgId;
  const mayMarkRead = !hasNewer && !jumpPending && tailSeen;

  // 🔴 THE RETRY THE READER CAN ACTUALLY PRESS (T-48). A failed read is the one
  // ending of a jump that is worth trying again, and an ending with no way to
  // try again is just a politer dead end — the same shape as F3, where the
  // fetch latch was spent and nothing could ask for a second attempt.
  //
  // Why a BUTTON and not the silent re-schedule the superseded path uses first:
  // that path retries because something else demonstrably moved (a newer load
  // committed), so the next attempt has a reason to go differently. A dropped
  // connection has no such signal — an automatic retry would fire straight back
  // into the same failure, and a loop of them is exactly what the retry cap
  // exists to prevent. The person watching knows when the office is back; the
  // button hands them that decision.
  //
  // 🔴 AND IT IS THE ONLY WAY BACK FROM *interrupted* TOO (R3-5). That notice
  // used to read 「再點一次連結可以重試」 while the jump latch was already spent
  // and the hash had not changed — no `hashchange`, no re-render, the reactor's
  // top guard returning immediately. The sentence asked the reader to do
  // something that could not work, which is precisely the class of silent lie
  // this ticket exists to delete. Both endings that a retry can change now get
  // the same button.
  //
  // ⚠️ THREE latches are released, and that is the whole of it: `session.jumpFetched`
  // alone would leave the jump CONSUMED (the reactor's top guard returns early
  // and nothing happens), `session.jumpConsumed` alone would leave the fetch marked
  // as already spent, and leaving the auto-retry budget spent would make the
  // button a one-shot on a path whose whole failure mode is losing races.
  function retryJump() {
    if (jumpToMsgId === undefined) return;
    session.jumpFetched = null;
    session.jumpConsumed = null;
    session.autoJumpRetries = 0;
    setJumpNotice(null);
    setJumpRetry((n) => n + 1);
  }

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
    // 🔴 AN EMPTY THREAD IS THE NORMAL ENTRY NOW, NOT A REASON TO WAIT (T-48).
    // With the anchor named at subscription time useChat fetches NOTHING on
    // entry, so waiting for `messages.length > 0` here would wait forever and
    // the room would never fill. An empty thread has nothing in the DOM, which
    // sends this straight down the fetch branch below — which is the point:
    // the FIRST request the room makes is the window around the target.
    if (session.jumpConsumed === jumpToMsgId) return;
    // Raw interpolation matches the chip-jump selector above — message ids
    // are server-minted (`c-<hex>`), never arbitrary strings.
    const el = messagesRef.current?.querySelector(
      `[data-msg-id="${jumpToMsgId}"]`,
    );
    // 🔴 A TARGET OUTSIDE THE LOADED WINDOW IS NOW FETCHED, NOT GUESSED AT
    // (T-48 ③, owner: 「跳到原訊息…都可以正確定位到該訊息」). This branch used
    // to be `endRef.scrollIntoView()` — the thread opened at the bottom and
    // NOTHING said the target had not been found, which is indistinguishable
    // from a successful jump to a recent message. `loadAround` pages OUTWARDS
    // from the id (one window each way, never the whole history); when it
    // lands, `messages` changes and this effect runs again with the row in the
    // DOM. The one-shot latch is NOT consumed yet — consuming it here would
    // eat the jump the fetch is about to make possible.
    if (!el) {
      if (session.jumpFetched !== jumpToMsgId) {
        session.jumpFetched = jumpToMsgId;
        // The jump owns the viewport FROM THE MOMENT IT STARTS FETCHING, not
        // from the moment it lands. Without these three the thread spends the
        // in-flight window doing its ordinary entry positioning — landing at
        // the bottom, i.e. the exact wrong place, and then being scrolled again
        // when the anchor window arrives. Registering the current ids as
        // already-seen also keeps that in-flight commit from mistaking the
        // thread it is replacing for a burst of new arrivals.
        session.initialPositioned = true;
        session.prevIds = new Set(messages.map((m) => m.id));
        session.nearBottom = false;
        // 🔴 THE JUMP IS LEAVING THE TAIL, AND THE WATERMARK MUST GO WITH IT
        // (see `mayMarkRead`). Set BEFORE the fetch, not after it lands: the
        // whole window in which the anchor pair + the walk to the tail are in
        // the air is a window in which nobody has looked at the newest message.
        setTailSeen(false);
        void loadAround(jumpToMsgId).then((outcome) => {
          // 🔴 THE OWNER MAY HAVE LEFT WHILE THE PAIR WAS IN THE AIR (T-48,
          // R5-1), AND LEAVING NOW MEANS UNMOUNTING (R13-5). Two of the endings
          // below reach outside this callback — `setJumpNotice` paints a banner
          // and `endRef.scrollIntoView()` moves a viewport — and neither is
          // addressed to a conversation. This used to need an explicit
          // `visitRef.current !== firedFor` line because the component was
          // reused between rooms: a 502 on A's anchor pair, answered after the
          // owner clicked B in the roster, hung A's 「讀不到那則訊息」 banner in
          // B's room with a retry button that did nothing. Under `key={peerId}`
          // this instance is gone by then — the setter writes into a dead
          // component and React drops it, and `endRef.current` has been nulled
          // by the unmount — so there is nothing left for a line to check.
          //
          // ⚠️ THAT IS A PROPERTY OF THE MOUNT, NOT OF THIS FILE. Rendering
          // `ChatArea` without a key puts every one of these back, silently.
          // `lint-chat-area-key` is what keeps it from happening.
          // 🔴 「cancelled」 IS THE ONE ENDING THAT WANTS NOTHING DONE (T-48
          // fix14, review25 F1). The room was left, or a NEWER jump replaced
          // this one — and that newer jump is already fetching. Treating it as
          // 「superseded」 (re-arm and go round again) would put a second walk
          // in the air for a target the reader has moved on from.
          if (outcome === "found" || outcome === "cancelled") return;
          if (
            outcome === "superseded" &&
            session.autoJumpRetries < MAX_JUMP_RETRIES
          ) {
            // 🔴 NOT A MISS (T-48, F3). Another load committed on top of ours,
            // so our window was dropped to keep the thread in order — the
            // message is still there. Saying 「找不到那則訊息」 here accused the
            // server of losing a message it still has, and because the fetch
            // latch had already been spent there was no retry and no button to
            // ask for one. Re-arm and go round again; if the owner has mean-
            // while asked for the live tail (回到最新 spends the jump latch),
            // the guard at the top of this effect ends it instead.
            session.autoJumpRetries += 1;
            session.jumpFetched = null;
            setJumpRetry((n) => n + 1);
            return;
          }
          // Genuinely unreachable (the id names nothing, the id belongs to
          // ANOTHER conversation — both windows answer 200 + empty — or the
          // request failed). Fall back to the bottom — the thread still opens —
          // and SAY SO ON SCREEN. The console line stays for the developer; the
          // notice is what stops the fallback reading as a successful jump.
          console.warn(
            `ChatArea: jump target ${jumpToMsgId} could not be located`,
            outcome,
          );
          // Three different facts, three different sentences. Collapsing any two
          // of them is the defect this ticket exists to remove, one layer up:
          // 「已經被清掉了」 tells a reader whose message is behind a 502 to stop
          // trying.
          setJumpNotice(
            outcome === "superseded"
              ? "interrupted"
              : outcome === "unreachable"
                ? "unreachable"
                : "missing",
          );
          session.jumpConsumed = jumpToMsgId;
          session.nearBottom = true;
          // The fallback lands on the live tail, so the reader IS at the latest
          // and the watermark block ends with the jump that failed.
          setTailSeen(true);
          // ⚠️ ANCHOR-FIRST ENTRY LEAVES THE ROOM EMPTY UNTIL SOMEBODY FILLS IT
          // (T-48). On this path nobody has: useChat skipped its entry load
          // because an anchor was named, and the anchor is not there. "Fall
          // back to the bottom" therefore has to fetch the bottom first, or the
          // owner is left staring at an empty conversation with a notice on it.
          // Only when the thread really is empty — a miss with history already
          // loaded still just lands where it always did.
          if (messages.length === 0) void resetToLatest();
          endRef.current?.scrollIntoView();
        });
      }
      return;
    }
    session.jumpConsumed = jumpToMsgId;
    setJumpNotice(null);
    // The jump owns the initial viewport — mark entry positioning done.
    session.initialPositioned = true;
    session.prevIds = new Set(messages.map((m) => m.id));
    el.scrollIntoView({ block: "center" });
    // Located mid-thread → not at the bottom; a later arrival must not yank.
    session.nearBottom = false;
    // …and the newest message is somewhere below, so the arrow belongs here.
    setLatestInView(false);
    setHighlightMsgId(jumpToMsgId);
    // ⚠️ CONTENT ABOVE THE TARGET THAT LOADS LATE WILL PUSH IT OFF SCREEN,
    // and nothing here corrects for that any more (T-48, owner
    // rc-6c27f486ef9d 「拿掉。圖片／卡片展開把目標擠走我接受」). A
    // ResizeObserver used to re-centre for 2.6s; measured cost of removing
    // it: the target ends up ~400-419px below the fold on a 394-433px
    // viewport — a whole screen — and the highlight pulse goes with it.
    // The owner named that cost and took it.
    // ⚠️ BUT THE TWO SOURCES THAT RULING NAMES ARE BOTH CLOSED AT SOURCE NOW,
    // not compensated for here: a thumbnail has a fixed 220px box (office.css
    // `.chat__msg-image`) and a reply card renders COLLAPSED, at its final
    // height, from what the carrying message already says — so it has nothing
    // to fetch and nothing to grow into (T-48, owner 2026-09-04). What is still
    // accepted is late content of every OTHER kind.
    // Do not add a compensator.
  }, [jumpToMsgId, messages, loadAround, resetToLatest, jumpRetry]);

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
    if (messages.length === 0) return;
    if (!session.initialPositioned) {
      session.initialPositioned = true;
      session.prevIds = new Set(messages.map((m) => m.id));
      const count = session.initialUnread;
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
        session.nearBottom = false;
        session.unreadRunOpen = true;
        session.entryScrollPending = true;
        setFirstUnreadId(first.id);
      } else {
        endRef.current?.scrollIntoView();
      }
      return;
    }
    const prev = session.prevIds;
    const fresh = messages.filter((m) => !prev.has(m.id));
    session.prevIds = new Set(messages.map((m) => m.id));
    if (session.nearBottom) {
      // 🔴 AN ANCHOR WINDOW WITH MORE BELOW IS NOT FOLLOWED (T-48, owner
      // rc-d2e1b69edc66 ①). `hasNewer` true means this thread is a window from
      // the MIDDLE of the history, and the page that just landed is the one the
      // reader asked for by scrolling — content they are walking INTO, not a
      // live tail arriving behind them. Following it would drag them past the
      // page instantly, and in a real browser `scrollIntoView` fires a scroll
      // event of its own, which re-enters the forward branch at `distance: 0`
      // and fetches the next page with no gesture at all — the level-triggered
      // corridor, resurrected through the follow. Leaving the viewport still is
      // what makes 「one gesture, one page」 true rather than decorative.
      // Every OTHER follow — the live tail, a message the owner just sent,
      // entry positioning — is unchanged.
      if (!hasNewer) endRef.current?.scrollIntoView();
      // Following the bottom = everything is being seen; any strip up is stale
      // (e.g. the owner just sent a reply, which force-follows), the newest
      // message is on screen, and the unread run — if one was open — is being
      // read right now.
      setNewMsgPreview(null);
      // Measured, not assumed: the scroll above is what makes the newest row
      // visible, so the row itself is the one thing worth asking (T-48).
      const box = messagesRef.current;
      setLatestInView(box ? isLatestRowInView(box) : true);
      session.unreadRunOpen = false;
      return;
    }
    // Scrolled up + new messages addressed to the owner. Two different anchors
    // come out of the SAME arrival batch and they are deliberately different
    // ends of it:
    //   • the preview strip shows the LAST one — it is a preview of what just
    //     came in, and it replaces whatever it was showing (never stacks);
    //   • the 「以下是未讀訊息」 divider anchors at the FIRST one — it marks
    //     where the unread block STARTS, and it is what stays behind after the
    //     jump lands the reader at the end of that block.
    const inboundNew = fresh.filter(
      (m) => m.to === OWNER_ID && m.from !== OWNER_ID,
    );
    if (inboundNew.length > 0) {
      const latest = inboundNew[inboundNew.length - 1];
      setNewMsgPreview({ id: latest.id, from: latest.from, body: latest.body });
      // Appended below the fold ⇒ the newest message is, by construction, not
      // in the viewport. Say so without waiting for a scroll event that may
      // never come (the owner is reading, not scrolling).
      setLatestInView(false);
      // Strip/divider alignment (owner bug): if no unread run is open (the
      // owner had seen everything up to now), this first unseen inbound STARTS
      // one → anchor the divider here. If a run is already open (e.g. the entry
      // divider's tail was never read down to), the arrival extends the SAME
      // run — the divider stays put.
      if (!session.unreadRunOpen) {
        session.unreadRunOpen = true;
        setFirstUnreadId(inboundNew[0].id);
      }
    }
  }, [messages]);

  // ② entry scroll: once the unread divider is in the DOM, pin it to the top of
  // the viewport, then measure honestly whether that landed us at the bottom
  // anyway (short thread) so auto-follow keeps working there.
  useEffect(() => {
    if (!firstUnreadId) return;
    // ONLY the entry positioning scrolls here. A chip-driven divider re-anchor
    // (in-conversation arrival while scrolled up) must not move the viewport —
    // the owner is reading history; the chip is their opt-in jump.
    if (!session.entryScrollPending) return;
    session.entryScrollPending = false;
    const box = messagesRef.current;
    if (!box) return;
    const divider =
      box.querySelector(".chat__unread-divider") ??
      box.querySelector(`[data-msg-id="${firstUnreadId}"]`);
    // The divider is the actual unread boundary.  Keeping older context above
    // it can push the first unread row outside a compact chat viewport.
    // Direction (F-C): entry positioning only — `entryScrollPending` is set on
    // the first commit of a conversation, before any walk can have been armed.
    divider?.scrollIntoView({ block: "start" });
    const distance = box.scrollHeight - box.scrollTop - box.clientHeight;
    session.nearBottom = distance <= NEAR_BOTTOM_PX;
    // Landing on the divider usually leaves the newest message below the fold —
    // that is the whole point of landing there — so the arrow must be able to
    // come up immediately, without waiting for the owner to scroll first.
    setLatestInView(isLatestRowInView(box));
    // NOTE: the run deliberately stays OPEN even when a short thread lands at
    // the bottom here — every real "the owner saw it" path (a bottom-crossing
    // scroll, or an at-bottom auto-follow) closes it; closing on this
    // layout-dependent measurement would misfire under test/jsdom geometry.
  }, [firstUnreadId]);

  // ③ THE ONE JUMP BEHIND BOTH BOTTOM AFFORDANCES: go to the LATEST message.
  //
  // 🔴 IT USED TO GO TO THE FIRST UNSEEN ONE, and that was the bug (reproduced
  // in the isolated environment: ten messages injected, the jump landed on
  // message 1 with five still below the fold). The first-unseen position is
  // still marked — by the 「以下是未讀訊息」 divider, which stays where it is —
  // so nothing was lost by moving the landing to the end of the block.
  //
  // ⚠️ THE LANDING IS NOT CORRECTED AFTERWARDS (T-48, owner rc-6c27f486ef9d).
  // `scrollToLatest` scrolls once and returns nothing; content above that grows
  // late will push the row back out of view, and that is the signed trade-off.
  function jumpToLatest() {
    const el = messagesRef.current;
    if (!el) return;
    // The strip's message is exactly what we are going to look at.
    setNewMsgPreview(null);
    // 🔴 NO OPTIMISTIC `setLatestInView(true)` HERE, and that absence is a fix
    // (T-48). Guessing the answer before the scroll happened made the arrow
    // blink out for 10–40ms and then come back when the scroll event measured
    // the truth — which is how the e2e assertion for "the arrow is gone"
    // sometimes passed against a product where it never went away. The answer
    // is now taken from the layout below, after the landing, and nowhere else.
    session.nearBottom = true;
    session.unreadRunOpen = false;
    // 「帶我去最新」, said by the owner — which is also the end of the post-jump
    // watermark block (see `mayMarkRead`).
    setTailSeen(true);
    // 🔴 THE ARROW / THE PREVIEW STRIP ENDS AN IN-FLIGHT JUMP (T-48). Both mean
    // "take me to the newest message", said by the owner, and they are the one
    // thing allowed to overtake the anchor fetch. Spending the jump latch here
    // is what makes the overtake DELIBERATE rather than a race: `loadAround`
    // comes back "superseded", the reactor's own top guard ends it without a
    // retry and without a notice, and `mayMarkRead` opens because the owner
    // really is on their way to the live tail.
    if (jumpToMsgId !== undefined) session.jumpConsumed = jumpToMsgId;
    // 🔴 SCROLLING IS NOT ENOUGH WHEN THE THREAD IS AN ANCHOR WINDOW (T-48 ③).
    // `scrollToLatest` lands on the last row IN THE DOM; after a jump to an old
    // message that row is nowhere near the newest one, so the arrow would move
    // the viewport and leave the owner still in the past — a fresh instance of
    // exactly the lie this ticket exists to remove. Fetch the live window first
    // and scroll when it lands.
    if (hasNewer) {
      session.pendingLatestScroll = true;
      void resetToLatest();
      return;
    }
    scrollToLatest(el);
    setLatestInView(isLatestRowInView(el));
  }
  // Declared AFTER the scroll-position reactor above so it runs last in the
  // commit: the reactor's own at-bottom auto-follow scrolls the zero-height
  // sentinel, and this replaces it with a landing on the last message ROW.
  useEffect(() => {
    if (!session.pendingLatestScroll) return;
    if (messages.length === 0) return;
    session.pendingLatestScroll = false;
    const el = messagesRef.current;
    if (!el) return;
    scrollToLatest(el);
    setLatestInView(isLatestRowInView(el));
    session.nearBottom = true;
  }, [messages]);

  // 🔴 THE ARROW HAS TO SURVIVE A REFLOW, AND UNTIL THIS EFFECT IT DID NOT.
  // This is the guardrail that had to ship WITH the deletion of the two
  // correction loops, not after it.
  //
  // EVERY OTHER `setLatestInView` IN THIS FILE IS 「有人主動捲動」—— the scroll
  // handler, entry positioning, the jump, the arrow, the preview strip; grep the
  // name to see the current set, and if a new write site is NOT one of those,
  // this effect's reason has changed and needs re-reading rather than trusting.
  // The point is the shape of that set, not its size: not one of them is a
  // REFLOW, and a reflow is exactly what moves the newest row without anybody
  // touching the scroller. While the settle loop existed that did not show: the
  // loop kept re-scrolling the newest row flush with the bottom for 2.6s, so the
  // stale `true` happened to stay correct. The loop was taking the bullet for
  // the staleness.
  //
  // Measured on the deleted code, the plainest path there is — scroll up, press
  // 回到最新, three images above still decoding:
  //     landed : lastRowBottomGap 0       inView true
  //     +3.5s  : lastRowBottomGap 418.31  inView false   arrowBack=false
  // The reader pressed 回到最新, ended up a full screen away from the newest
  // message (the viewport is 433px), and NOTHING on screen said so. 「箭頭說謊」
  // is the reason this ticket exists at all — a displacement the owner signed
  // for is not the same thing as the interface lying about where you are.
  //
  // ⚠️ IT IS PURE READ. It re-answers 「最新那一列還在視窗裡嗎」 and writes
  // nothing else — no `scrollTop`, no `nearBottom`, no scroll of any kind. That
  // is the whole reason it is allowed to exist where three correction loops
  // were rejected: it never contends with the reader or with the browser's own
  // anchoring, so it needs none of the reader-versus-programmatic telling-apart
  // that killed them. Adding a scroll here re-creates exactly what was deleted.
  //
  // Same target as the loops watched, and for the same reason: a RO on the
  // viewport never fires (its own box is clamped by flex + overflow), so the
  // in-flow children — whose height is what actually grows — are what to watch.
  useEffect(() => {
    const el = messagesRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setLatestInView(isLatestRowInView(el)));
    for (const child of Array.from(el.children)) ro.observe(child);
    return () => ro.disconnect();
  }, [messages]);

  // ①② WHICH bottom affordance is on screen — at most ONE, ever (owner: the
  // preview strip 讓位 rule). Derived in one place so the exclusion is a single
  // fact rather than two booleans that have to agree; see the module's note for
  // why writing `!latestInView` inline is the mistake this shape prevents.
  const bottomAffordance = chatBottomAffordance({
    latestInView,
    hasNewMsgPreview: newMsgPreview !== null,
    windowHasNewer: hasNewer,
  });

  // OWNER read receipt: entering the conversation (or a new message landing while
  // the owner is at the bottom) means the owner has SEEN up to the newest message
  // → mark it read. markRead is monotonic server-side (a stale ts is a no-op), so
  // firing on every settle is safe. If the owner has scrolled UP to read history
  // we still mark read: the newest message is loaded and being viewed on entry.
  //
  // Gated THREE ways:
  //   • `windowActive` — "seen" requires the owner to actually be looking. A
  //     message landing while the window is backgrounded must NOT be consumed;
  //     the flip back to active re-runs this effect, so everything accumulated
  //     is marked read exactly when the owner returns.
  //   • 🔴 `mayMarkRead` — the thread must be the LIVE TAIL and no jump may be
  //     in flight (T-48; see where it is derived for both halves and why).
  useEffect(() => {
    if (!windowActive) return;
    if (!mayMarkRead) return;
    if (newestTs > 0) void markRead(newestTs);
  }, [newestTs, markRead, windowActive, mayMarkRead]);

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
    session.nearBottom = true;
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
      // 🔴 RESTORE INTO THE ROOM IT WAS TYPED IN — NOT nowhere. An earlier
      // version of this arm was a bare `return` on the reasoning that "that
      // room's draft still holds it". IT DOES NOT: the optimistic clear at the
      // top of submit() empties both halves of the draft, and an all-empty
      // draft is DELETED from the store, not stored blank. So the bare return
      // traded a visible mistake for "text, attachments AND reply target
      // silently gone for good, with only a console.warn". That is worse, and
      // it is exactly what this arm exists to prevent.
      //
      // Writing to the store rather than to state also covers the case where
      // this component is gone entirely (跳頁 or a switch mid-flight): setState
      // on an unmounted component discards the content just as quietly.
      //
      // FIELD BY FIELD, which is the rule the state restores below also use:
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
      saveChatDraftText(
        sendPeer,
        stored && stored.text ? stored.text : draftSnapshot,
        stored && stored.replyTo ? stored.replyTo : (replyToSnapshot ?? undefined),
      );
      updateChatDraftAttachments(sendPeer, (prev) =>
        prev.length > 0 ? prev : attachmentsSnapshot,
      );
      // 🔴 THE FILES NEED NO SECOND RESTORE, AND THE TEXT'S IS A NO-OP WHEN
      // NOBODY IS LOOKING (T-48, R13-2/R13-5). The staged rows ARE the write
      // above: this composer reads that slot, so if it is still on screen the
      // files are already back. The text and the reply target live in component
      // state, so they are put back here — and if the owner has left, this
      // component is unmounted (`key={peerId}`) and React drops both writes,
      // which is why there is no "is this still my room?" line: there is no
      // other room this component can be showing.
      setDraft((cur) => (cur ? cur : draftSnapshot));
      // Put the target back only if the owner has not already aimed at
      // something else while the send was in flight.
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
    // 🔴 A BARE NUMBER IS SAFE AGAIN (T-48, R13-1). This used to be
    // `peerLastRead.tsFor(member.id)`: the watermark carried the room it was
    // for, because `useChat` was reused across rooms and could hand back the
    // PREVIOUS room's reading for one commit — which lit a 已讀 tick off
    // somebody else's watermark (R8-2). `useChat` is now mounted per room, so
    // the only reading it can hand back is this room's.
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
        {/* 🔴 THE ONE PLACE THE SPINNER IS RENDERED, AND THE ONE INPUT IT
         * READS (T-48 fix12, owner c-d24ebd7f8d78). `initialLoading` is
         * `useChat`'s single answer to 「這條對話的內容還沒到,或正在走訪」 for
         * EVERY entrance — an ordinary entry waiting on `load()`, a 跳到原訊息
         * entry waiting on `loadAround` (which since fix12 does not commit until
         * it has fetched through to the live tail), and 跳到原訊息 AGAIN in a
         * room that is already open. A further entrance needs no line here.
         *
         * 🔴 IT IS ASKED BEFORE `messages.length` ON PURPOSE (fix14, review25
         * F2). Asked after, this branch is unreachable for every wait that
         * starts with content already on screen — which is exactly the second
         * jump, and exactly the longest wait there is. The reader then sits in
         * front of the OLD thread for the whole walk with nothing saying so.
         *
         * ⚠️ IT ALSO SITS ABOVE THE OFFLINE CARD ON PURPOSE. 「離線」 is a fact
         * about the member and 「還沒載完」 is a fact about this pane; while
         * the second is true we do not yet know whether the thread is empty,
         * and painting the offline card first would mean flashing 「還沒有訊
         * 息」 at a conversation that is about to have some. */}
        {initialLoading ? (
          <ChatThreadLoading />
        ) : messages.length > 0 ? (
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
            {/* ① the round 回到最新訊息 arrow, bottom-right of the pane and
             * therefore directly above the composer. It is NOT rendered
             * whenever the preview strip is (see bottomAffordance). */}
            {bottomAffordance === "arrow" && (
              <ChatJumpLatestButton onClick={jumpToLatest} />
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
        {/* 🔴 T-48: the jump could not find its target. Pinned above the
         * composer rather than dropped into the stream, because the fallback
         * has just scrolled the thread to the BOTTOM — a notice placed in the
         * stream would land wherever the missing message would have been, i.e.
         * off screen, which is another way of not saying it. It outlives the
         * fallback scroll on purpose (the reader has to be able to look up and
         * find out why they are where they are) and is cleared by the x, by a
         * peer switch, or by a jump that later succeeds. */}
        {jumpNotice && (
          <div className="chat__jump-miss" role="status">
            <span>
              {jumpNotice === "interrupted"
                ? t.chat.jumpTargetInterrupted
                : jumpNotice === "unreachable"
                  ? t.chat.jumpTargetUnreachable
                  : t.chat.jumpTargetMissing}
            </span>
            {/* The two endings a retry can change get the button; 「找不到」
             * does not, because the server has answered and the answer will
             * not differ. See retryJump for why *interrupted* is one of them
             * (its old copy pointed at a link that could not re-fire). */}
            {jumpNotice !== "missing" && (
              <button
                type="button"
                className="chat__jump-miss__retry"
                data-testid="jump-miss-retry"
                onClick={retryJump}
              >
                {t.chat.jumpTargetRetry}
              </button>
            )}
            <button
              type="button"
              className="chat__jump-miss__x"
              aria-label={t.chat.jumpTargetMissingDismiss}
              title={t.chat.jumpTargetMissingDismiss}
              onClick={() => setJumpNotice(null)}
            >
              ×
            </button>
          </div>
        )}
        {/* ② the new-message preview strip. FIRST child of the composer, so it
         * sits above the 「正在回覆」 banner (owner's requirement) and above the
         * wake row and the attachment previews as well — and it is outside the
         * locked/unlocked fork on purpose: a read-only peer's thread still
         * receives messages, and the owner still has to be told.
         *
         * The whole strip is one jump target; the x drops it without moving the
         * viewport, after which the round arrow takes its place (the newest
         * message is still not on screen — dismissing a preview is not reading
         * it). */}
        {bottomAffordance === "preview" && newMsgPreview && (
          <ChatNewMsgPreview
            who={nameOf(newMsgPreview.from)}
            body={newMsgPreview.body}
            onJump={jumpToLatest}
            onDismiss={() => setNewMsgPreview(null)}
          />
        )}
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
                      // 🔴 WHOSE wake this is (review r2 SHOULD-1). An activate
                      // still in flight when the owner switches peers used to
                      // resolve into a room that was already B's, because this
                      // component was reused between rooms. It is unmounted with
                      // its conversation now (R13-5), so both arms below write
                      // into a dead component and React drops them.
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
                          if (result?.activationPending) {
                            setWakePending(false);
                            setWakeUndispatched(true);
                          }
                        })
                        .catch(() => {
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
                attachError={
                  attachError ??
                  (overAttachmentCap
                    ? t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS)
                    : null)
                }
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

      {/* The full-view overlay this component opens (T-f014) — a 放大閱讀
       * message rides the body text this component already holds; a staged
       * composer image rides its data: URI. A STORED attachment is not here:
       * its chip is rendered by `AttachmentStrip`, which owns that overlay. */}
      {mdPreview &&
        (mdPreview.kind === "staged-image" ? (
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
