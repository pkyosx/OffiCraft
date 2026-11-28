// RepliesPage — the 等我回覆 page (SPEC §2, M2 reply cards B2): every member's
// asks, one list at a time, picked by the three labels above the list:
//
//   等待回覆 — waiting cards, newest ask first unless the owner picks another
//            排序依據 under 顯示方式 (remembered per browser); every card wears
//            the SAME style — no longest-waiting highlight (owner ruled it out).
//   稍後再看 — waiting cards the owner put aside; each returns to 等待回覆 by
//            itself at its time.
//   近期已回覆 — cards answered or expired within 24h, newest-handled first.
//            Not fetched until this label is selected.
//
// The label is not remembered: every visit opens on 等待回覆. The 篩選 dialog's
// fields narrow whichever label is selected.
//
// 🔴 EVERY CARD STARTS COLLAPSED, AND OPENING ONE IS WHAT READS IT (owner
// ruling 2026-09-07, restated 2026-09-11:「要做折疊版的，預設全部折疊」). The panes
// are lists of LIGHT ROWS (`ReplyCardRow`: the ask's title, its status, its
// stamps and its task ref — no body, no options, no chat anchor), because
// `?view=full` is gone from the wire.
//
// 🔴 THE COLLAPSED ROW IS THE CARD'S OWN HEAD — NOT A SECOND LAYOUT (owner
// 2026-09-11:「折疊起來那一列的內容不要另外設計」). It draws exactly what the
// expanded card's top strip drew: avatar, name + role, 跳到原訊息, 標為過期, the
// ticking 已等你 {t} to the RIGHT of 標為過期, and the task-title row underneath.
// Every one of those comes off the light row, so a list of collapsed cards
// fetches nothing. Opening a row reads that ONE card (`api.getReplyCard`) and adds the
// QUESTION to what is already on screen — summary, body, attachments and the
// SHARED ReplyCardBody — instead of re-drawing a head that never left.
// ⚠️ NO ABSOLUTE 開卡 STAMP. owner 2026-09-11:「我不想知道絕對時間 相對時間已經
// 夠了」. Absolute time survives only on a handled row (已回覆／已過期 <time>).
// ⚠️ AND NOTHING OPENS BY ITSELF. The leading 待回覆 card used to open on arrival
// (rc-cd351785b83d, rc-fa7e4c9bce42); 預設全部折疊 is the opposite instruction, so
// that exception and its `autoOpenOffRef` latch are gone. The ONE thing that
// still opens a card unasked is a DEEP LINK to that card — an explicit request
// for it, not a default.
// The whole card is the toggle (`role="button"` + an interaction filter), the
// same mechanism `TaskCard` uses — see `onCardToggleClick` below.
//
// The card interiors live in ReplyCardBody.tsx, SHARED with B3's inline chat
// card (ChatReplyCard) so the two surfaces can never drift. Answering is the
// only POSITIVE way a card leaves 待回覆; 標為過期 (the owner or an admin agent here,
// and since T-1b88 the card's own author via the API) is the sole
// other exit (terminal, NOT an answer — the agent reopens a fresh card if the
// question still matters). The nav badge (waiting count) and the chat unread
// red dot are independent signals: answering here never touches the red dot.

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { secretCardMayStillChange } from "../lib/replyCardReceipt";
import type {
  OutsourceWorkerView,
  ReplyCard,
  ReplyCardAnswerInput,
  ReplyCardRow,
} from "../api/adapter";
import type { Member } from "../types";
import { isHttpStatus } from "../api/errors";
import { useMembers } from "../hooks/useMembers";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { MentionRosterContext, useMentionRoster } from "../hooks/useMentionComposer";
import { useReplyCards } from "../hooks/useReplyCards";
import { useDueSoonLeadMinutes } from "../hooks/useDueSoonLeadMinutes";
import { useNowSeconds } from "../hooks/useNowSeconds";
import {
  DUE_FILTER_KEYS,
  dueBucketOf,
  dueFilterLabel,
  effectiveLeadMinutes,
  nextDueChangeAmong,
  type DueFilterKey,
} from "../lib/dueDate";
import { PRIORITIES } from "../lib/taskPriority";
import { readStoredChoice, writeStoredChoice } from "../lib/storedChoice";
import {
  DEFAULT_WAITING_SORT,
  WAITING_SORTS,
  WAITING_SORT_KEY,
  type WaitingSort,
  sortWaitingRows,
} from "../lib/replyCardSort";
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
  useWorkerCurrentTasks,
} from "../hooks/useWorkerCodenames";
import { useHashRoute } from "../lib/hashRoute";
import { isSyntheticSender } from "../lib/syntheticSender";
import { docCommentRecipient } from "../lib/docComments";
import { avatarKindForMember } from "../lib/avatarKind";
import { ReplyCardAvatarButton } from "./ReplyCardAvatarButton";
import { CurrentTaskTitle } from "./CurrentTaskTitle";
import { OutsourceTaskLine } from "./OutsourcePanel";
import {
  CopyIcon,
  HourglassIcon,
  TextCursorIcon,
} from "./icons";
import {
  ExpireActionIcon,
  JumpToMessageIcon,
  SnoozeActionIcon,
  UnsnoozeActionIcon,
} from "./replyCardActionIcons";
import { FilterField, FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
import { MultiSelectFilter, type MultiSelectOption } from "./MultiSelectFilter";
import { ConfirmModal } from "./ConfirmModal";
import { DisplayOptions } from "./DisplayOptions";
import { TaskDueChip, TaskNoBadge, TaskPriorityBadge } from "./TaskChips";
import { ReplySnoozeMenu, type SnoozeOutcome } from "./ReplySnoozeMenu";
import {
  ChatSelectionModeBar,
  ChatSelectionReply,
  ReplyCardQuoteProvider,
  guardSelectionMode,
} from "./ChatSelectionReply";
import {
  ChatMessageCopyButton,
  ChatMessageCopyStatus,
  replyCardQuestionText,
  useMessageCopy,
} from "./ChatMessageCopyButton";
import {
  ChatMessagePressMenu,
  pressedReplyCard,
  type PressMenuItem,
} from "./ChatMessagePressMenu";
import { useLongPress } from "../hooks/useLongPress";
import { Markdown } from "./Markdown";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardLinkComments,
  ReplyCardQuestionAttachments,
  ReplyCardTaskRef,
  ReplyCardWaitingBody,
} from "./ReplyCardBody";
import { formatDurationShort } from "../lib/duration";
import { snoozeBackShort } from "../lib/replySnooze";
import { formatAbsolute } from "../lib/dateFormat";
import { isCardToggleClick, isCardToggleKey } from "../lib/cardToggle";
import "./office.css"; // chat composer classes the ReplyComposer reuses, AND
// the rail's `.outsource-row__*` / `.current-task-title` rules the asker's
// current-task line is drawn with (T-196) — removing this import as an
// unused-looking tidy-up unstyles that line, and no vitest test can see it.
import "./replies.css";

const HANDLED_WINDOW_SECONDS = 24 * 3600;
const SELECTING_CARD = ".reply-card--selecting";

/** Makes everything in `root` but `row` and the elements holding it inert: at
 * each level from the row up, its siblings. Returns the undo. */
function inertAround(row: Element, root: Element): () => void {
  const out: HTMLElement[] = [];
  for (let at: Element = row; at !== root && at.parentElement; at = at.parentElement) {
    for (const sibling of Array.from(at.parentElement.children)) {
      if (sibling !== at && sibling instanceof HTMLElement && !sibling.inert) out.push(sibling);
    }
  }
  for (const el of out) el.inert = true;
  return () => {
    for (const el of out) el.inert = false;
  };
}

const NO_TASK = "notask";
const PRIORITY_FILTER_KEYS = [...PRIORITIES, NO_TASK] as const;

type StatusKey = "waiting" | "snoozed" | "answered" | "expired";
type ListView = "waiting" | "snoozed" | "answered";
const LIST_VIEWS: readonly ListView[] = ["waiting", "snoozed", "answered"];
const viewOfStatus = (status: StatusKey): ListView =>
  status === "expired" ? "answered" : status;

/** A handled card's pane stamp (answeredTs / expiredTs — whichever its
 * terminal state carries). */
function handledTsOf(card: ReplyCardRow): number | null {
  return card.status === "expired"
    ? (card.expiredTs ?? null)
    : card.answeredTs;
}

function ReplyCardMentionProvider({
  members,
  workers,
  replyCardId,
  children,
}: {
  members: Member[];
  workers: OutsourceWorkerView[];
  replyCardId: string;
  children: ReactNode;
}) {
  const mentionRoster = useMentionRoster(members, workers, {
    backTo: "replies",
    backToId: replyCardId,
  });
  return (
    <MentionRosterContext.Provider value={mentionRoster}>
      {children}
    </MentionRosterContext.Provider>
  );
}

export function RepliesPage({
  replyCardId,
  openerId,
}: {
  replyCardId?: string;
  openerId?: string;
}) {
  const { t, msg } = useI18n();
  // Light roster (T-cf91): the page attributes each card to its asker by
  // name + role only, so it takes the identity-only projection AND does not
  // refetch it when anyone in the company sends a chat message.
  const {
    members,
    loading: membersLoading,
  } = useMembers({ light: true });
  // The @ list in an open card's answer box also needs each outsource worker's
  // task number and title, which the light roster lacks. That full list (and
  // its member/task/chat subscriptions) is only mounted while a card is open —
  // see `OutsourceWorkersFeed` at the bottom of the page.
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const {
    waiting,
    snoozed,
    snooze,
    handled,
    handledLoaded,
    loading,
    error,
    loadHandled,
    refresh,
    answer,
    reanswer,
    expire,
    provideSecret,
  } = useReplyCards();
  const [, setRoute] = useHashRoute();

  // ── 篩選 (T-93 round 2) ─────────────────────────────────────────────────
  // ── ID 篩選 (T-118) ──────────────────────────────────────────────────────
  // The field lives in the collapsed FilterPanel's dialog (read FilterPanel.tsx's
  // header — that contract is not restated here).
  //
  // TWO STATES, STILL. `appliedId` is what the page is actually filtered by;
  // `draftId` is what the field binds to. 🔴 They are still separate, and NOT
  // because of a leftover panel: an applied id is a SERVER request
  // (`api.getReplyCard` in the lookup effect below), so committing per keystroke
  // is a fetch per keystroke — the shape owner rejected twice
  // (「每次都要全部都撈回來才濾不合理」,「按搜尋時不要再跳出新modal」). What
  // changed this round is only WHEN the draft commits: Enter or blur
  // (「按enter或是點外面就視為apply了」), where it used to be 套用篩選.
  const [appliedId, setAppliedId] = useState(replyCardId ?? "");
  const [draftId, setDraftId] = useState(replyCardId ?? "");
  // `#replies/card/<id>` still seeds the APPLIED id (unchanged from round 1):
  // a notification tap or a shared link lands on that one card without the
  // owner having to open the panel and press anything. The link names one card,
  // so every other axis is cleared: an opener ticked by an earlier
  // #replies/opener jump would otherwise hide another member's card.
  useEffect(() => {
    if (!replyCardId) return;
    setAppliedId(replyCardId);
    setDraftId(replyCardId);
    setOpenerFilter(new Set());
    setPriorityFilter(new Set());
    setDueFilter(new Set());
  }, [replyCardId]);
  const idQuery = appliedId.trim();
  const filtering = idQuery !== "";

  // 🔴 THE ID IS ASKED OF THE SERVER — it is NOT a substring scan of the rows
  // that happen to be loaded (owner 2026-09-06, option ①). Round 1 filtered the
  // panes, and the panes hold waiting cards plus whatever was answered/expired
  // in the last 24h; a card older than that came back as「沒有符合篩選條件的
  // 請示」— a sentence indistinguishable from 「這張卡不存在」. That collapse
  // fooled the owner in review and is the defect this ticket exists to remove,
  // so the three outcomes below are kept apart on purpose and must stay apart:
  //   found   → that one card, even if its pane was collapsed and unfetched;
  //   missing → 404, i.e. the SERVER says the id does not exist;
  //   failed  → we never got an answer (network / 500). It must NOT say 找不到.
  type Lookup =
    | { state: "idle" }
    | { state: "loading" }
    | { state: "found"; card: ReplyCard }
    | { state: "missing" }
    | { state: "failed" };
  const [lookup, setLookup] = useState<Lookup>({ state: "idle" });
  // Bumped after an owner action so a card fetched by id is re-read once its
  // status has changed underneath us (answering the found card must not leave
  // the stale waiting body on screen).
  const [lookupNonce, setLookupNonce] = useState(0);
  useEffect(() => {
    if (idQuery === "") {
      setLookup({ state: "idle" });
      return;
    }
    let alive = true;
    setLookup({ state: "loading" });
    api.getReplyCard(idQuery).then(
      (card) => {
        if (alive) setLookup({ state: "found", card });
      },
      (e: unknown) => {
        if (!alive) return;
        console.warn("RepliesPage: reply-card lookup failed", e);
        setLookup({ state: isHttpStatus(e, 404) ? "missing" : "failed" });
      }
    );
    return () => {
      alive = false;
    };
  }, [idQuery, lookupNonce]);

  // The lookup above reads once, so a card opened by id would keep its old state
  // until a reload. Not through lookupNonce: its loading state unmounts the box and
  // takes a half-typed value with it.
  const lookupMayChange =
    lookup.state === "found" &&
    (lookup.card.status === "waiting" || secretCardMayStillChange(lookup.card));
  useEffect(() => {
    if (!lookupMayChange) return;
    let alive = true;
    let latestRead = 0;
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "reply_card") return;
      const read = ++latestRead;
      api.getReplyCard(idQuery).then(
        (card) => {
          if (alive && read === latestRead) setLookup({ state: "found", card });
        },
        (e: unknown) => console.warn("RepliesPage: card re-read failed", e),
      );
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [idQuery, lookupMayChange]);

  // The live row wins over the fetched one when the page already holds it: the
  // panes are kept in sync by SSE + the write-adoption in useReplyCards, and a
  // one-shot GET is a snapshot from one instant.
  const foundCard: ReplyCardRow | null =
    lookup.state === "found"
      ? (waiting.find((c) => c.id === lookup.card.id) ??
        snoozed.find((c) => c.id === lookup.card.id) ??
        handled.find((c) => c.id === lookup.card.id) ??
        lookup.card)
      : null;

  // 🔴 清除篩選 — REMOVED, THEN PUT BACK BY NAME (T-118). It used to sit on the
  // 已篩選 strip; owner named the strip for removal (c-c3d681fe05da) and then
  // asked for this control back once he saw the row without it
  // (c-2423dba8b65b:「清除篩選還是要留著」). The strip STATED the filter — the
  // permanently-visible field does that now — while the button ENDS it, which
  // nothing else does in one gesture. Clearing must also drop the id from the
  // URL, or a reload would seed it straight back and the clear would look broken.
  function clearFilters() {
    setAppliedId("");
    setDraftId("");
    setOpenerFilter(new Set());
    setPriorityFilter(new Set());
    setDueFilter(new Set());
    if (replyCardId || openerId) setRoute({ page: "replies" });
  }

  // ── 開卡人 (T-118, owner 2026-09-06 c-782404ee53d8「請示卡我想多一個開卡的人
  // 的filter」) ──────────────────────────────────────────────────────────────
  // Built the way 任務頁's 負責人 axis is built, because owner said so in as many
  // words when he was shown the alternative (c-7e4374094273:「那我們先跟任務用
  // 同樣的做法就好」): the options and their counts are computed from the LOADED
  // panes, and the narrowing happens here rather than on the server.
  //
  // 🔴 THAT IS NOT AN OVERSIGHT AND IT IS NOT A CONTRADICTION OF「都不可以在前端
  // 做篩選」(c-b3f5a7fe2431). It is the resolution he chose after being shown
  // what server-side narrowing would cost HERE: the option list is derived from
  // the same rows the pane holds, so a server that returned only the picked
  // person's cards would leave that person as the ONLY option — nothing to
  // switch to, nothing to untick. He was given three ways out and answered by
  // pointing at the existing page. Moving this to the server later means
  // solving the option-list problem first; it is not a one-line change.
  //
  // An EMPTY set = 所有開卡人 (no constraint), same convention as 任務頁.
  const [openerFilter, setOpenerFilter] = useState<Set<string>>(
    () => new Set(openerId ? [openerId] : [])
  );
  const [view, setViewState] = useState<ListView>("waiting");
  const tabsId = useId();
  // Cards settled elsewhere while the owner had them open, keyed to the label
  // they were read under: they stay where he is reading them until he switches.
  const [pinned, setPinned] = useState<Map<string, ListView>>(() => new Map());
  const shownInViewRef = useRef<Set<string>>(new Set());
  const ownMoveIdsRef = useRef<Set<string>>(new Set());
  const viewRef = useRef(view);
  viewRef.current = view;
  function setView(next: ListView) {
    setViewState(next);
    setPinned((prev) => (prev.size === 0 ? prev : new Map()));
    shownInViewRef.current = new Set();
  }
  // What 清除篩選 is offered for — EITHER axis, not just the id. `filtering`
  // stays id-only on purpose: it also gates the by-id lookup's three outcomes
  // and skips the handled-list fetch, and an 開卡人 tick must not switch
  // those on (there is no id to have found, missed, or failed to reach).
  // 任務優先序 / 任務到期 — same empty-set convention; a card with no task
  // counts as 無任務 on the first and matches no option on the second.
  const [priorityFilter, setPriorityFilter] = useState<Set<string>>(() => new Set());
  const [dueFilter, setDueFilter] = useState<Set<string>>(() => new Set());
  // Seeded like 任務頁's #tasks/executor/<id>: this jump is about one person's
  // waiting cards, so every other axis starts clear (an id kept from an earlier
  // #replies/card link would otherwise narrow the list to nothing). Unlike that
  // route the hash stays, so a reload lands on the same view.
  useEffect(() => {
    if (!openerId) return;
    setOpenerFilter(new Set([openerId]));
    setView("waiting");
    setPriorityFilter(new Set());
    setDueFilter(new Set());
    setAppliedId("");
    setDraftId("");
  }, [openerId]);
  const activeFilterCount = [
    idQuery !== "",
    openerFilter.size > 0,
    priorityFilter.size > 0,
    dueFilter.size > 0,
  ].filter(Boolean).length;
  const narrowing = activeFilterCount > 0;
  // An applied id is answered by the server, so it never needs these lists.
  const wantsHandled = view === "answered" && !filtering;

  const [waitingSort, setWaitingSortState] = useState<WaitingSort>(() =>
    readStoredChoice(WAITING_SORT_KEY, WAITING_SORTS, DEFAULT_WAITING_SORT)
  );
  function setWaitingSort(next: WaitingSort) {
    setWaitingSortState(next);
    writeStoredChoice(WAITING_SORT_KEY, next);
  }
  const sortLabels: Record<WaitingSort, string> = {
    createdDesc: t.replies.sortCreatedDesc,
    createdAsc: t.replies.sortCreatedAsc,
    priority: t.replies.sortPriority,
    due: t.replies.sortDue,
  };

  /** Enter or blur on the 編號 field — the only two events that turn typed text
   * into a filter (owner 2026-09-06:「按enter或是點外面就視為apply了」). */
  function commitId(value: string) {
    const next = value.trim();
    // Blur fires on every tab-through, so committing an unchanged value must
    // cost nothing: `idQuery` drives the lookup effect, and re-setting it to
    // what it already holds would re-run `api.getReplyCard` for no reason.
    if (next === appliedId) {
      if (next !== value) setDraftId(next);
      return;
    }
    setDraftId(next);
    setAppliedId(next);
    // The hash is a SECOND source for the applied id, so leaving a stale one
    // there would re-seed the old card on the next reload.
    if (replyCardId && next !== replyCardId) setRoute({ page: "replies" });
  }

  // Ticking clock (30s): drives the live 已等你 counters AND the client-side
  // 24h prune of the handled pane while the page stays open (the server
  // already windows the lists per fetch; without the tick an aging card would
  // linger until the next SSE-driven refetch).
  const [nowTs, setNowTs] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = window.setInterval(
      () => setNowTs(Date.now() / 1000),
      30_000
    );
    return () => window.clearInterval(timer);
  }, []);

  // The 期限 filter's own clock: woken at the next moment any loaded card's
  // task crosses into 快到期 or 已逾期, the same boundary its chip changes at.
  const workspaceLeadMinutes = useDueSoonLeadMinutes();
  const dueNow = useNowSeconds(
    nextDueChangeAmong(
      [...waiting, ...snoozed, ...handled].flatMap((c) => (c.task ? [c.task] : [])),
      Date.now() / 1000,
      workspaceLeadMinutes
    )
  );

  // Transient action-failure notice (400/409/network). The composer keeps the
  // typed content; the option chips can simply be clicked again.
  const [actionError, setActionError] = useState<string | null>(null);

  // 標為過期 double-confirm: expiring is terminal with no undo, so a
  // single mis-click must never close a card — the button only OPENS this
  // modal; the modal's confirm fires the action.
  const [expireTarget, setExpireTarget] = useState<ReplyCardRow | null>(null);
  const [expireBusy, setExpireBusy] = useState(false);

  const [unsnoozeErrors, setUnsnoozeErrors] = useState<Set<string>>(() => new Set());
  const [handledLoadError, setHandledLoadError] = useState(false);
  const [settledRows, setSettledRows] = useState<Map<string, ReplyCardRow>>(
    () => new Map()
  );

  // Once per link: the lists refresh on any card's event, and landing again would
  // pull focus out of a box the owner is typing in and lose the keystrokes.
  const landedOnRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!replyCardId) {
      landedOnRef.current = undefined;
      return;
    }
    if (landedOnRef.current === replyCardId) return;
    const card = document.getElementById(`reply-card-${replyCardId}`);
    if (!card) return;
    landedOnRef.current = replyCardId;
    card.scrollIntoView({ block: "center" });
    if (!card.contains(document.activeElement)) card.focus({ preventScroll: true });
  }, [replyCardId, waiting, handled, lookup.state, view]);

  const loadHandledForPage = useCallback(async () => {
    setHandledLoadError(false);
    try {
      await loadHandled();
    } catch (e) {
      console.warn("RepliesPage: handled-list load failed", e);
      setHandledLoadError(true);
    }
  }, [loadHandled]);

  // Owner 已回覆卡預設不載: the answered/expired lists are fetched only once
  // 近期已回覆 is selected. A failed load is retried by switching to another
  // label and back, not in a loop.
  useEffect(() => {
    if (wantsHandled && !handledLoaded) void loadHandledForPage();
  }, [wantsHandled, handledLoaded, loadHandledForPage]);

  // Display order = the owner's 排序依據 (`sortWaitingRows`; default 開卡時間
  // newest first). No per-card highlight: the owner ruled the longest-waiting
  // accent ring out — every card wears the same face.
  //
  // With an id applied the panes hold exactly what the SERVER returned
  // for that id — one card, in whichever pane its status belongs to — not a
  // narrowing of the loaded rows.
  // The 24h window applies to fetched handled rows. A row just settled by a card
  // read stays visible for this page visit even when its terminal timestamp is
  // older; use the same predicate for pane visibility and 開卡人 counts.
  const withinHandledWindow = (c: ReplyCardRow) => {
    const ts = handledTsOf(c);
    return ts !== null && nowTs - ts < HANDLED_WINDOW_SECONDS;
  };
  const isVisibleHandled = (c: ReplyCardRow) =>
    settledRows.has(c.id) || withinHandledWindow(c);

  // 開卡人 predicate. An empty set is "no constraint", so an unticked axis is
  // free rather than exclusive — same convention as 任務頁's three dropdowns.
  const passesOpener = (c: ReplyCardRow) =>
    openerFilter.size === 0 || openerFilter.has(c.from);
  const priorityKeyOf = (c: ReplyCardRow) =>
    c.task ? (c.task.priority ?? "") : NO_TASK;
  const passesPriority = (c: ReplyCardRow) =>
    priorityFilter.size === 0 || priorityFilter.has(priorityKeyOf(c));
  const dueKeysOf = (c: ReplyCardRow): string[] => {
    const bucket = dueBucketOf(
      c.task?.dueTs,
      dueNow,
      effectiveLeadMinutes(c.task?.dueSoonLeadMinutes, workspaceLeadMinutes)
    );
    return bucket === "overdue" || bucket === "soon" ? [bucket] : [];
  };
  const passesDue = (c: ReplyCardRow) =>
    dueFilter.size === 0 || dueKeysOf(c).some((k) => dueFilter.has(k));
  const passesAll = (c: ReplyCardRow) =>
    passesOpener(c) && passesPriority(c) && passesDue(c);

  const displayedWaiting = waiting.filter((row) => !settledRows.has(row.id));
  const displayedSnoozed = snoozed.filter((row) => !settledRows.has(row.id));
  const handledById = new Map<string, ReplyCardRow>();
  for (const row of handled) handledById.set(row.id, row);
  for (const row of settledRows.values()) {
    const current = handledById.get(row.id);
    if (!current || (handledTsOf(row) ?? 0) > (handledTsOf(current) ?? 0)) {
      handledById.set(row.id, row);
    }
  }
  const displayedHandled = [...handledById.values()];

  // 🔴 THE 開卡人 AXIS ANDs WITH THE ID, IT DOES NOT REPLACE IT. An id names ONE
  // card and is answered by the SERVER; if that card's opener fails this axis,
  // the honest answer is the ordinary filtered-empty, not the card. Letting the
  // id win would make 「找到了，但不是這個人開的」 render as a hit, which is the
  // same class of merged-silence defect the id lookup exists to remove.
  // A card fetched by id that is not in either live list is judged snoozed by
  // its own stamp, against the same clock the provider splits on.
  const foundSnoozed =
    foundCard !== null &&
    foundCard.status === "waiting" &&
    (snoozed.some((c) => c.id === foundCard.id) ||
      (!waiting.some((c) => c.id === foundCard.id) &&
        (foundCard.snoozedUntilTs ?? 0) > Date.now() / 1000));
  const [expandedIds, setExpandedIds] = useState<Set<string>>(() => new Set());
  const foundStatus: StatusKey | null =
    foundCard === null
      ? null
      : foundCard.status === "waiting"
        ? foundSnoozed
          ? "snoozed"
          : "waiting"
        : foundCard.status;
  // An id names one card, so landing on it selects that card's label — once per
  // lookup: a card that settles while the owner reads it must not drag the
  // page to another label.
  const foundView = foundStatus === null ? null : viewOfStatus(foundStatus);
  const autoViewForRef = useRef<string | null>(null);
  useEffect(() => {
    if (!filtering) {
      autoViewForRef.current = null;
      return;
    }
    if (foundView === null || autoViewForRef.current === idQuery) return;
    autoViewForRef.current = idQuery;
    if (foundView !== viewRef.current) setView(foundView);
  }, [filtering, idQuery, foundView]);

  const visibleHandled = displayedHandled.filter(isVisibleHandled);
  const pinnedRows = (into: ListView) =>
    displayedHandled.filter((c) => pinned.get(c.id) === into);
  // An open card that moves between 等待回覆 and 稍後再看 by itself (its snooze
  // ran out, or another window snoozed it) stays where the owner is reading it
  // until he closes it or switches label. His own snooze here still moves it.
  const heldOpen = (rows: ReplyCardRow[]) =>
    rows.filter(
      (c) =>
        expandedIds.has(c.id) &&
        shownInViewRef.current.has(c.id) &&
        !ownMoveIdsRef.current.has(c.id) &&
        passesAll(c)
    );
  type ListItem = { row: ReplyCardRow; status: StatusKey };
  const asItem = (status: StatusKey) => (row: ReplyCardRow): ListItem => ({ row, status });
  const rowItem = (row: ReplyCardRow): ListItem => ({ row, status: row.status as StatusKey });
  const listItems: ListItem[] = filtering
    ? // 🔴 NO 24h PRUNE on a card fetched by id. The window is what makes the
      // unfiltered list "recent"; applying it to an answer from the server would
      // drop the card the owner asked for for being old.
      // A card that settles while shown stays under the label it was read in.
      foundCard &&
      foundStatus &&
      (foundView === view || shownInViewRef.current.has(foundCard.id)) &&
      passesAll(foundCard)
      ? [{ row: foundCard, status: foundStatus }]
      : []
    : view === "waiting"
      ? [
          ...sortWaitingRows(
            [...displayedWaiting, ...pinnedRows("waiting")].filter(passesAll),
            waitingSort
          ).map((row) => (row.status === "waiting" ? asItem("waiting")(row) : rowItem(row))),
          ...heldOpen(displayedSnoozed).map(asItem("snoozed")),
        ]
      : view === "snoozed"
        ? [
            ...displayedSnoozed.filter(passesAll).map(asItem("snoozed")),
            ...heldOpen(displayedWaiting).map(asItem("waiting")),
            ...pinnedRows("snoozed").filter(passesAll).map(rowItem),
          ]
        : visibleHandled
            .filter(passesAll)
            .sort((a, b) => (handledTsOf(b) ?? 0) - (handledTsOf(a) ?? 0))
            .map(rowItem);

  useEffect(() => {
    shownInViewRef.current = new Set(listItems.map((item) => item.row.id));
  });

  const viewCounts: Record<ListView, number | null> = filtering
    ? {
        waiting: 0,
        snoozed: 0,
        answered: 0,
        ...(foundCard && foundView && passesAll(foundCard) ? { [foundView]: 1 } : {}),
      }
    : {
        waiting: displayedWaiting.filter(passesAll).length,
        snoozed: displayedSnoozed.filter(passesAll).length,
        answered: handledLoaded ? visibleHandled.filter(passesAll).length : null,
      };
  // ── 開卡了哪一張 (owner 2026-09-07) ─────────────────────────────────────────
  // A row is a TITLE, not a card: since `?view=full` left the wire the panes
  // carry `ReplyCardRow`s, so the only thing this page can draw without asking
  // the server is the collapsed line. `expandedIds` is which rows the owner has
  // opened, `openCards` the ONE-card reads those opens produced, `openErrors`
  // the ids whose read failed (an opened row must say so rather than sit empty).
  //
  // NOT PERSISTED. Every card starts collapsed (owner 2026-09-11
  // 「預設全部折疊」); only a deep link opens one — the same posture as
  // ChatReplyCard's stub.
  const [openCards, setOpenCards] = useState<Map<string, ReplyCard>>(
    () => new Map()
  );
  const [openErrors, setOpenErrors] = useState<Set<string>>(() => new Set());
  // Cards the owner just opened whose secret box has not taken focus yet. Consumed
  // once: a card that moves between panes while open remounts its body, and that
  // is not the owner opening it.
  const secretFocusPendingRef = useRef<Set<string>>(new Set());

  // A DEEP LINK OPENS ITS CARD. `#replies/card/<id>` (a notification tap, a
  // shared link) is a request for THAT card, so landing on a collapsed line the
  // owner still has to click would be a step backwards from what the link used
  // to do. Only the routed id — everything else on the page stays closed.
  useEffect(() => {
    if (!replyCardId) return;
    secretFocusPendingRef.current.add(replyCardId);
    setExpandedIds((prev) =>
      prev.has(replyCardId) ? prev : new Set(prev).add(replyCardId)
    );
  }, [replyCardId]);

  /** Toggle one card open/closed. Closing is a real exit (owner 2026-09-07:
   * answering and 標為過期 used to be the only ways out of an opened card), and
   * it also DROPS the read below, so re-opening reads the card again rather
   * than showing whatever it said minutes ago. */
  function toggleCard(id: string) {
    ownMoveIdsRef.current.delete(id);
    if (expandedIds.has(id)) secretFocusPendingRef.current.delete(id);
    else secretFocusPendingRef.current.add(id);
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  // Whole-card toggle with the same interaction filter as `TaskCard`
  // (isCardToggleClick): 跳到原訊息 / 標為過期, the option chips, the composer and
  // its paperclip, an attachment thumbnail, a markdown link, a modal do their
  // own job and never flip the card.
  function onCardToggleClick(e: React.MouseEvent<HTMLElement>, id: string) {
    if (isCardToggleClick(e, ".reply-composer")) toggleCard(id);
  }

  /** Keyboard operability (repo convention for clickable surfaces). Only keys on
   * the card ITSELF toggle — an Enter bubbling out of the composer must not. */
  function onCardToggleKeyDown(e: React.KeyboardEvent<HTMLElement>, id: string) {
    if (isCardToggleKey(e)) toggleCard(id);
  }

  // The row's own version stamp. It is what decides that a card ALREADY READ is
  // stale — an answer from another window (or this page's own write, which
  // useReplyCards folds into the row) moves the row's status/stamps, and the
  // fetched card must follow or an opened card would keep rendering 待回覆 chips
  // for a card the server has settled.
  const rowVersion = (row: ReplyCardRow) =>
    `${row.status}:${row.answeredTs ?? 0}:${row.expiredTs ?? 0}:${row.secret?.state ?? ""}:${row.secret?.expiresTs ?? 0}`;

  const rowsById = new Map<string, ReplyCardRow>();
  for (const row of [...displayedWaiting, ...displayedSnoozed, ...displayedHandled]) {
    rowsById.set(row.id, row);
  }
  if (foundCard) rowsById.set(foundCard.id, foundCard);

  // Which version of each open id has been READ (or is being read). One entry
  // per open card, so a row that has not moved is never re-fetched — and a row
  // that HAS moved is fetched exactly once per move, which is what keeps this
  // from looping against a server that disagrees with the row.
  const readVersionsRef = useRef<Map<string, string>>(new Map());
  const pageRef = useRef<HTMLDivElement>(null);
  const readCard = useCallback(
    async (row: ReplyCardRow) => {
      const { id } = row;
      try {
        const card = await api.getReplyCard(id);
        setOpenCards((prev) => new Map(prev).set(id, card));
        setOpenErrors((prev) => {
          if (!prev.has(id)) return prev;
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
        if (row.status === "waiting" && card.status !== "waiting") {
          setSettledRows((prev) =>
            new Map(prev).set(id, {
              ...row,
              status: card.status,
              answeredTs: card.answeredTs,
              expiredTs: card.expiredTs ?? null,
            })
          );
          const into = viewRef.current;
          if (into !== "answered") setPinned((prev) => new Map(prev).set(id, into));
          try {
            await refresh();
            if (handledLoaded) setHandledLoadError(false);
          } catch (err) {
            console.warn("RepliesPage: settled-card refresh failed", err);
            if (handledLoaded) setHandledLoadError(true);
          }
        }
      } catch (e) {
        console.warn("RepliesPage: card read failed", e);
        setOpenErrors((prev) => new Set(prev).add(id));
      }
    },
    [handledLoaded, refresh]
  );

  // 🔴 THE ONLY PLACE A CARD IS READ. Opening a row does not fetch by itself —
  // it flips `expandedIds`, and this effect notices. That keeps "which cards are
  // open" and "which cards have been read" from being two rules that can
  // disagree, and it means a row moving underneath an open card re-reads it for
  // free (the version key changes).
  useEffect(() => {
    const seen = readVersionsRef.current;
    for (const id of expandedIds) {
      const row = rowsById.get(id);
      // No row means the page is not holding the card at all — the one it just
      // answered, before the handled lists have been loaded. Nothing renders it,
      // so reading it would buy a request for a card that is off screen; the
      // version check below re-reads it the moment a pane lists it again.
      if (!row) continue;
      const version = rowVersion(row);
      if (seen.get(id) === version) continue;
      seen.set(id, version);
      void readCard(row);
    }
    for (const id of [...seen.keys()]) {
      if (!expandedIds.has(id)) {
        // Closed: forget both the read and its version, so re-opening is a
        // fresh read rather than a stale card the owner cannot tell is stale.
        seen.delete(id);
        setOpenCards((prev) => {
          if (!prev.has(id)) return prev;
          const next = new Map(prev);
          next.delete(id);
          return next;
        });
        setOpenErrors((prev) => {
          if (!prev.has(id)) return prev;
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      }
    }
  });

  // ── 複製・長按選單・選取模式 (the chat room's, on this page's cards) ──────
  const messageCopy = useMessageCopy();
  const [pressMenu, setPressMenu] = useState<string | null>(null);
  const [snoozeRequestId, setSnoozeRequestId] = useState<string | null>(null);
  // A pulse: the card's menu opens in its own effect, which runs before this
  // one. Left set, a request whose card was not on the page (moved by an
  // update between the long press and the tap) would open the menu by itself
  // whenever that card next shows.
  useEffect(() => {
    if (snoozeRequestId !== null) setSnoozeRequestId(null);
  }, [snoozeRequestId]);
  const [selectingId, setSelectingId] = useState<string | null>(null);
  const openBeforeSelectingRef = useRef(false);
  const focusCardOnExitRef = useRef(false);
  const longPress = useLongPress((target) => {
    const card = pressedReplyCard(target);
    const id = card?.getAttribute("data-reply-card-id");
    return id && id !== selectingId && pageRef.current?.contains(card!) ? id : null;
  }, setPressMenu);

  function copyCard(row: ReplyCardRow) {
    void messageCopy.copy(row.id, () => replyCardQuestionText(row.id, row.summary));
  }

  const cardElementOf = (id: string) => document.getElementById(`reply-card-${id}`);

  function enterSelection(id: string) {
    openBeforeSelectingRef.current = expandedIds.has(id);
    setExpandedIds((prev) => (prev.has(id) ? prev : new Set(prev).add(id)));
    setSelectingId(id);
  }
  /** Leaves the mode; the card goes back to how it was before it, unless the
   * mode ended with 回覆 into its answer box. */
  function exitSelection(replied = false) {
    window.getSelection()?.removeAllRanges();
    const id = selectingId;
    if (id && !replied && !openBeforeSelectingRef.current) {
      setExpandedIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
    setSelectingId(null);
    messageCopy.clear();
  }
  const exitSelectionRef = useRef(exitSelection);
  exitSelectionRef.current = exitSelection;
  useEffect(() => {
    if (!selectingId) return;
    // Looked up by id on every event: a list refresh can remount the card in
    // another pane during the mode.
    const detach = guardSelectionMode(() => cardElementOf(selectingId), {
      onOutside: () => exitSelectionRef.current(),
    });
    return () => {
      detach();
      if (focusCardOnExitRef.current) {
        focusCardOnExitRef.current = false;
        cardElementOf(selectingId)
          ?.querySelector<HTMLElement>(".chat__msg-copy")
          ?.focus({ preventScroll: true });
      }
    };
  }, [selectingId]);
  // Re-run on every render: a list refreshing during the mode brings new rows,
  // or takes the card off the page (a pane that is folded), which ends the mode.
  useLayoutEffect(() => {
    const page = pageRef.current;
    if (!page || !selectingId) return;
    const row = cardElementOf(selectingId);
    if (!row) {
      exitSelectionRef.current();
      return;
    }
    return inertAround(row, page);
  });

  /** No 回覆這則, unlike the chat room's sheet: the owner's ruling, since the
   * card's own answer box is right there on this page. Do not add it back to
   * match the chat.
   *
   * A touch-only screen hides the head's buttons (replies.css), so this sheet
   * is their only way in there and must offer every one the head offers, each
   * through the same handler as its button. The card's own actions come first
   * and 複製・選取文字 last: the owner's order, unlike the chat's sheet. */
  function pressMenuItems(id: string): PressMenuItem[] {
    const row = rowsById.get(id);
    if (!row) return [];
    const opened = openCards.get(id);
    const waitingNow = row.status === "waiting" && (!opened || opened.status === "waiting");
    const snoozedNow = waitingNow && listItems.some(
      (item) => item.row.id === id && item.status === "snoozed"
    );
    const cardActions: PressMenuItem[] = [
      {
        key: "jump",
        label: t.replies.jumpToChat,
        icon: <JumpToMessageIcon size={13} />,
        onSelect: () => void jumpToChat(row),
      },
    ];
    if (waitingNow)
      cardActions.push(
        snoozedNow
          ? {
              key: "unsnooze",
              label: t.replies.unsnooze,
              icon: <UnsnoozeActionIcon size={13} />,
              onSelect: () => void doUnsnooze(id),
            }
          : {
              key: "snooze",
              label: t.replies.snooze,
              icon: <SnoozeActionIcon size={13} />,
              onSelect: () => setSnoozeRequestId(id),
            },
        {
          key: "expire",
          label: t.replies.expire,
          icon: <ExpireActionIcon size={13} />,
          onSelect: () => setExpireTarget(row),
        },
      );
    const items: PressMenuItem[] = [
      ...cardActions,
      {
        key: "copy",
        label: t.chat.copyMessage,
        icon: <CopyIcon size={13} />,
        onSelect: () => copyCard(row),
      },
      {
        key: "select",
        label: t.chat.selectText,
        icon: <TextCursorIcon size={13} />,
        onSelect: () => enterSelection(id),
      },
    ];
    return items;
  }

  // ── 開卡人 options + counts ────────────────────────────────────────────────
  // 🔴 COUNTED OVER THE PANES BEFORE THE OPENER AXIS NARROWS THEM. That is the
  // whole reason this axis stays usable: count the ALREADY-narrowed rows and a
  // ticked person becomes the only person with a non-zero count, so every other
  // name disappears and the owner cannot switch or untick. 任務頁 has the same
  // shape for the same reason (`inCountScope` there).
  //
  // Each axis counts the rows the OTHER axes let through, so ticking one never
  // zeroes its own siblings.
  const axisBasis =
    view === "waiting" ? displayedWaiting : view === "snoozed" ? displayedSnoozed : visibleHandled;
  const countBy = (keep: (c: ReplyCardRow) => boolean, keysOf: (c: ReplyCardRow) => string[]) => {
    const counts = new Map<string, number>();
    for (const c of axisBasis) {
      if (!keep(c)) continue;
      for (const k of keysOf(c)) counts.set(k, (counts.get(k) ?? 0) + 1);
    }
    return counts;
  };
  const openerCounts = countBy(
    (c) => passesPriority(c) && passesDue(c),
    (c) => [c.from]
  );
  const priorityCounts = countBy(
    (c) => passesOpener(c) && passesDue(c),
    (c) => [priorityKeyOf(c)]
  );
  const dueCounts = countBy((c) => passesOpener(c) && passesPriority(c), dueKeysOf);
  const fixedOptions = (
    keys: readonly string[],
    counts: Map<string, number>,
    labelOf: (k: string) => string
  ): MultiSelectOption[] =>
    keys.map((k) => ({ value: k, label: labelOf(k), count: counts.get(k) ?? 0 }));
  const priorityOptions = fixedOptions(PRIORITY_FILTER_KEYS, priorityCounts, (k) =>
    k === NO_TASK ? t.replies.filterPriorityNoTask : (t.tasks.priority[k] ?? k)
  );
  const dueOptions = fixedOptions(DUE_FILTER_KEYS, dueCounts, (k) =>
    dueFilterLabel(t.tasks, k as DueFilterKey)
  );
  const viewLabels: Record<ListView, string> = {
    waiting: t.replies.tabWaiting,
    snoozed: t.replies.tabSnoozed,
    answered: t.replies.tabAnswered,
  };
  // 開卡人 下拉的選項在 `whoOf` 之後才建得起來（它要用 `askerNames`），見下方。

  const handledLoading = wantsHandled && !handledLoaded && !handledLoadError;

  // Outsource askers (ow- ids) get their codename from the lazy per-id read
  // rather than from `members`. Not because they are missing from it — GET
  // /api/members does carry kind='outsource' rows — but because this page
  // must cover the RELEASED ones too, and release soft-removes the row, which
  // is exactly what the endpoint filters out. One read that works for live AND
  // released keeps the identity row on the same 代號 the office rail shows
  // instead of the raw id. `whoOf` below routes to it on `kind === "outsource"`,
  // so a live worker that IS in `members` takes this path as well.
  // A card reached by its id may sit in neither pane (a deep link to a handled
  // card whose pane was never fetched), so its asker is named here too.
  const askerIds = [
    ...displayedWaiting,
    ...displayedSnoozed,
    ...displayedHandled,
    ...(foundCard ? [foundCard] : []),
  ].map(
    (c) => c.from,
  );
  const workerIds = askerIds.filter((id) => id.startsWith("ow-"));
  // A staff asker who has since left is soft-removed from `members` the same
  // way, and the same read still serves the name. Judged only once the roster
  // has loaded: before that every live colleague looks absent and would cost a
  // read each.
  const rosterLoaded = !membersLoading;
  const departedStaffIds = rosterLoaded
    ? askerIds.filter(
        (id) =>
          !id.startsWith("ow-") &&
          id !== "" &&
          !isSyntheticSender(id) &&
          !members.some((m) => m.id === id),
      )
    : [];
  const askerNames = useWorkerCodenames([...workerIds, ...departedStaffIds]);
  const workerAvatarUrls = useWorkerAvatarUrls(workerIds);
  // T-196 (owner rc-dce285c5274c:「只在 UI 上補上顯示就好 就像在 chat 那邊 使用者
  // 列表上 outsource worker 會顯示他在進行的工作是哪一個」): the asker's CURRENT
  // task — the worker's own, NOT `row.task`, which is the task this CARD was
  // bound to and is null on a plain chat ask. Same read and same cache as the
  // codename/avatar above, so the identity row speaks with one voice.
  const workerTasks = useWorkerCurrentTasks(workerIds);

  // Resolve the initiating member for a card's identity row. A card can
  // outlive its member (removed roster row) — fall back to the name the per-id
  // read serves, then the raw id / no role, never fabricate.
  function whoOfId(fromId: string): { name: string; role: string } {
    const m = members.find((x) => x.id === fromId);
    if (!m || m.kind === "outsource") {
      const name = askerNames.get(fromId);
      if (name === undefined) return { name: fromId, role: "" };
      return {
        name: fromId.startsWith("ow-") ? msg.outsourceLabel(name) : name,
        role: "",
      };
    }
    const role =
      (t.office.role as Record<string, string>)[m.role] ??
      (m.roleName || m.role);
    return { name: m.name, role };
  }
  function whoOf(card: ReplyCardRow): { name: string; role: string } {
    return whoOfId(card.from);
  }
  const docCommentRecipientFor = (id: string) =>
    docCommentRecipient(
      id,
      whoOfId(id).name,
      members.some((m) => m.id === id),
      rosterLoaded,
    );

  // ── 開卡人 下拉的選項 ──────────────────────────────────────────────────────
  // owner 2026-09-06 (c-63f5651493f8):「可以選的人就是現在UI filter出來的那些人,
  // 並且要顯示幾張卡這個數字」— so the list is the people who actually have cards
  // here, not the whole roster. 邊界 (copied from 任務頁 deliberately): a person
  // already TICKED stays listed even at zero, or the owner could not untick them
  // and the filter would be a dead end.
  //
  // 🔴 NAMES COME FROM `whoOfId`, THE SAME RESOLVER THE CARDS USE. An earlier
  // cut read `members.find(...)?.name ?? id` here, which is a DIFFERENT rule
  // from the one the card bodies follow: a RELEASED outsource asker is soft-
  // removed from `members`, so the dropdown fell through to the raw `ow-…` id
  // while the card beside it said 「外包 · 代號」. The owner would have been asked
  // to tick a name that appears nowhere else on the page. That is also why this
  // block sits below `askerNames` rather than beside `openerCounts` — it needs
  // the lazy codename read, and hoisting it back up is a TDZ error, not a
  // tidy-up. Found by independent review of 8204de4f.
  const openerOptions: MultiSelectOption[] = [...openerCounts.keys()]
    .map((id) => ({
      value: id,
      label: whoOfId(id).name,
      count: openerCounts.get(id) ?? 0,
    }))
    .concat(
      [...openerFilter]
        .filter((id) => !openerCounts.has(id))
        .map((id) => ({
          value: id,
          label: whoOfId(id).name,
          count: 0,
        }))
    )
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));

  // Jump to the origin: the ask always comes from a chat message
  // (card.chatMessageId), so open that member's chat room WITH the message id
  // in the route — ChatArea locates + highlights the ask (B3 聊天整合).
  //
  // 🔴 THE ANCHOR IS NOT ON A LIGHT ROW. `ReplyCardRow` carries no
  // `chatMessageId` (the wire's `ReplyCardListItemDTO` does not either), and
  // since the button now rides the COLLAPSED head it can be pressed before the
  // card has ever been read. So an unread card is read HERE, on the click, and
  // only then routed — one request, made because the owner asked to go
  // somewhere, never on render. Routing without the id would land him in the
  // right room at the wrong place, which is the silent half-answer this avoids.
  async function jumpToChat(row: ReplyCardRow) {
    const known = openCards.get(row.id);
    let msgId = known?.chatMessageId;
    if (!msgId) {
      try {
        msgId = (await api.getReplyCard(row.id)).chatMessageId;
      } catch (e) {
        // Routing without the anchor is the silent half-answer this exists to
        // avoid, so the failure stays HERE: open the row instead, which runs
        // the ordinary one-card read and renders its error line.
        console.warn("RepliesPage: chat-anchor read failed", e);
        setExpandedIds((prev) => new Set(prev).add(row.id));
        return;
      }
    }
    setRoute({
      page: "office",
      chatId: row.from,
      msgId: msgId || undefined,
    });
  }

  // Avatar → member panel (owner 2026-07-21: "也要可以" — every other avatar
  // in the cockpit already opens the detail panel; this card's was the one
  // hold-out). Mirrors MemberCard's avatar-as-second-target pattern and rides
  // the SAME hash seam (frontend/src/lib/hashRoute.ts) OfficePage already
  // reads: staff askers go through #office/member/<id> (detailId), an
  // outsource asker through #office/worker/<id> (workerId) — OfficePage
  // self-heals to the plain roster view if the id doesn't resolve (e.g. a
  // released worker), so this never dead-ends. The split below keys on
  // `kind === "staff"`, NOT on absence from `members`: GET /api/members
  // carries kind='outsource' rows, so a live worker is present in the list
  // and only its kind tells the two panels apart.
  //
  // backTo + backToId: without them,
  // OfficePage's own 返回 button resets to its default chat view (roster[0])
  // — correct when the panel was opened FROM the office page itself, but a
  // silent wrong-room landing when opened via this cross-page deep link,
  // since there was never a chat selected to return to. The marker tells
  // OfficePage's 返回 to reopen this exact card instead.
  function openProfile(row: ReplyCardRow) {
    const isRosterMember = members.some(
      (m) => m.id === row.from && m.kind === "staff",
    );
    setRoute(
      isRosterMember
        ? {
            page: "office",
            detailId: row.from,
            backTo: "replies",
            backToId: row.id,
          }
        : {
            page: "office",
            workerId: row.from,
            backTo: "replies",
            backToId: row.id,
          },
    );
  }

  // §3.6 請示 → 任務: an ask that names a task shows the task row at the very
  // top of the card, above the head — #number, priority, due date, title, and
  // the jump. The route carries the task id so the tasks page can locate
  // the card. A pure chat ask renders nothing here.
  function renderTaskRef(row: ReplyCardRow) {
    const task = row.task;
    if (!task) return null;
    return (
      <ReplyCardTaskRef
        task={task}
        onJump={() => setRoute({ page: "tasks", taskId: task.id })}
        lead={
          <span className="reply-card__task-chips">
            <TaskNoBadge taskNo={task.id} testId="reply-task-no" />
            {task.priority && (
              <TaskPriorityBadge priority={task.priority} testId="reply-task-priority" />
            )}
            {task.dueTs != null && (
              <TaskDueChip
                dueTs={task.dueTs}
                leadMinutes={effectiveLeadMinutes(task.dueSoonLeadMinutes, workspaceLeadMinutes)}
                testId="reply-task-due"
              />
            )}
          </span>
        }
      />
    );
  }

  // T-4166: 409 on an answer is NOT a transient failure — it is the server
  // saying this card can never be answered again (its task closed underneath
  // it, or it is already answered/expired). Telling the owner「請稍後重試」there
  // sends them down a road that is 409 a hundred times out of a hundred. Say
  // what actually happened, and refresh the pane so the dead card leaves the
  // screen instead of sitting there looking clickable — for a card that is
  // still listed, 標為過期 in its header is the legitimate exit.
  async function reportAnswerFailure(e: unknown) {
    const stale = isHttpStatus(e, 409);
    setActionError(stale ? t.replies.answerStale : t.replies.answerError);
    if (stale) {
      // Re-read the panes: an answered/expired/orphaned card must stop
      // pretending it is still waiting for the owner.
      await refresh().catch((err) =>
        console.warn("RepliesPage: stale-card refresh failed", err)
      );
    }
  }

  async function doAnswer(id: string, input: ReplyCardAnswerInput) {
    try {
      await answer(id, input);
      setActionError(null);
      setLookupNonce((n) => n + 1);
    } catch (e) {
      console.warn("RepliesPage: answer failed", e);
      await reportAnswerFailure(e);
      throw e;
    }
  }

  async function doReanswer(id: string, input: ReplyCardAnswerInput) {
    try {
      await reanswer(id, input);
      setActionError(null);
      setLookupNonce((n) => n + 1);
    } catch (e) {
      console.warn("RepliesPage: re-answer failed", e);
      await reportAnswerFailure(e);
      throw e;
    }
  }

  async function doProvideSecret(id: string, value: string) {
    try {
      await provideSecret(id, value);
      setActionError(null);
      setLookupNonce((n) => n + 1);
    } catch (e) {
      console.warn("RepliesPage: secret provide failed");
      if (isHttpStatus(e, 409)) await reportAnswerFailure(e);
      else setActionError(t.replies.secretProvideError);
      throw e;
    }
  }

  async function doExpire(card: ReplyCardRow) {
    setExpireBusy(true);
    try {
      await expire(card.id);
      setActionError(null);
      setExpireTarget(null);
      setLookupNonce((n) => n + 1);
    } catch (e) {
      console.warn("RepliesPage: expire failed", e);
      setActionError(t.replies.expireError);
      setExpireTarget(null);
    } finally {
      setExpireBusy(false);
    }
  }

  // A 409 is the server saying the card is no longer waiting (answered or
  // expired elsewhere): say so and re-read, like a stale answer.
  async function reportStaleSnooze(message: string) {
    setActionError(message);
    await refresh().catch((err) =>
      console.warn("RepliesPage: stale-snooze refresh failed", err)
    );
  }

  async function doSnooze(id: string, untilTs: number): Promise<SnoozeOutcome> {
    ownMoveIdsRef.current.add(id);
    try {
      await snooze(id, untilTs);
      setActionError(null);
      setLookupNonce((n) => n + 1);
      return "ok";
    } catch (e) {
      console.warn("RepliesPage: snooze failed", e);
      if (!isHttpStatus(e, 409)) return "failed";
      await reportStaleSnooze(t.replies.snoozeStale);
      return "stale";
    }
  }

  async function doUnsnooze(id: string) {
    ownMoveIdsRef.current.add(id);
    setUnsnoozeErrors((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
    try {
      await snooze(id, null);
      setActionError(null);
      setLookupNonce((n) => n + 1);
    } catch (e) {
      console.warn("RepliesPage: unsnooze failed", e);
      if (isHttpStatus(e, 409)) await reportStaleSnooze(t.replies.unsnoozeStale);
      else setUnsnoozeErrors((prev) => new Set(prev).add(id));
    }
  }

  /** The asking OUTSOURCE worker's current task, under its 代號 (T-196).
   *
   * owner rc-dce285c5274c, on a card whose weight he could not judge:「我覺得只在
   * UI 上補上顯示就好 就像在 chat 那邊 使用者列表上 outsource worker 會顯示他在
   * 進行的工作是哪一個」— so this renders the office rail's own two pieces,
   * `OutsourceTaskLine` (clickable 任務編號 chip → #tasks/<id>, then the task
   * type) and `CurrentTaskTitle` (the real title, clamped + hover), NOT a second
   * rendering of the same fact. The rail's presence dot is left off, exactly as
   * the worker chat header leaves it off: presence lives in the rail, and this
   * page never grows a second presence source.
   *
   * 🔴 NOT `row.task`: that is the task the CARD is bound to, which the owner
   * explicitly ruled out of this ticket (the binding logic is untouched) and
   * which is null on a plain chat ask. This line is the WORKER's, so it shows
   * even on a card bound to nothing — and also when the card's task row names
   * the same task — and it shows the SAME task the rail does.
   *
   * A staff asker renders nothing here: `MemberDTO` carries no task at all, so
   * there is no such fact to tell for a member — see the ResumeRoster ruling
   * (正職給職責, 外包給任務標題). A worker whose id never resolved renders
   * nothing either: silence is honest, a placeholder would assert "no task" on
   * evidence we do not have. The placeholder IS shown once the worker resolved
   * and the task is genuinely empty — an unassigned worker really has none. */
  function renderAskerCurrentTask(row: ReplyCardRow) {
    const worker = workerTasks.get(row.from);
    if (!worker) return null;
    // 🔴 A RELEASED worker has no current task, and its row still CARRIES the
    // old one: release flips the status and leaves task_id alone
    // (`dal_tasks.go` ReleaseWorkerByID), while the per-id read serves released
    // rows in full — which is the very reason this page can resolve them at
    // all. So the row is not evidence of current work, and drawing it would
    // say "O-9 is on T-42" about a worker that finished and left, with a live
    // chip beside it. The rail can't make this mistake because the LIST skips
    // released workers outright (`api_outsource.go`); this surface has to say
    // so itself or the two drift in meaning while sharing the components.
    // Silence, not 「無當前任務」: the placeholder is for a worker we know holds
    // no task, and neither is this page a place to announce 已釋出 — that
    // sentence already has one home (the chat header banner) and the repo's own
    // i18n note forbids a second copy of it.
    // An ALLOWLIST, not a denylist: only the two statuses that mean "this
    // worker is on the job" draw the line. Naming the terminal one instead
    // would be a prediction — the next terminal status added server-side would
    // reach this surface as a live-looking row, which is precisely the defect
    // this check exists for. An absent status (an older server) falls on the
    // silent side too: not knowing is not evidence of current work.
    if (worker.status !== "assigned" && worker.status !== "active") return null;
    // An empty taskId is the other honest nothing: the worker holds no task, or
    // the server could not resolve the one it holds. `OutsourceTaskLine` always
    // draws its type slot, falling back to 自由代辦 — true on the rail, where a
    // listed worker always HAS a task and the fallback means "a task with no
    // type", but a plain lie here, where it would sit above 無當前任務 and the
    // same row would claim both.
    // Keyed on `taskNo`, NOT `taskId`: the server writes task_id
    // unconditionally but fills task_no / task_title / task_type_* only when it
    // could RESOLVE the task, so a task_id whose task is gone would pass a
    // taskId test and then render 自由代辦 over 無當前任務 — the contradiction
    // above, one field later.
    const bound = Boolean(worker.taskNo);
    return (
      <span className="reply-card__worker-task">
        {bound && (
          <OutsourceTaskLine
            worker={worker}
            onOpenTask={(taskId) => setRoute({ page: "tasks", taskId })}
            idPrefix={`reply-card-${row.id}`}
          />
        )}
        <CurrentTaskTitle
          title={worker.taskTitle ?? ""}
          clamp
          testid={`reply-card-task-title-${row.id}`}
        />
      </span>
    );
  }

  /** The card's head — and, since owner 2026-09-11, the COLLAPSED ROW itself:
   * 「折疊起來那一列的內容不要另外設計」. It is drawn from the LIGHT ROW, so it
   * costs no read: avatar, name + role, 跳到原訊息, 標為過期, then the stamp. */
  function renderHead(
    row: ReplyCardRow,
    stampNode?: ReactNode,
    expirable = false,
    actions?: ReactNode
  ) {
    const who = whoOf(row);
    const asker = members.find((x) => x.id === row.from);
    return (
      <header className="reply-card__head">
        <ReplyCardAvatarButton
          onClick={() => openProfile(row)}
          src={
            (asker?.kind === "outsource" ? undefined : asker?.avatarUrl) ??
            workerAvatarUrls.get(row.from)
          }
          kind={avatarKindForMember(
            asker ?? { id: row.from }
          )}
        />
        <div className="reply-card__who">
          <span className="reply-card__name" title={who.name}>
            {who.name}
          </span>
          {who.role && (
            <span className="reply-card__role" title={who.role}>
              {who.role}
            </span>
          )}
          {renderAskerCurrentTask(row)}
        </div>
        <div className="reply-card__actions">
          <span className="reply-card__copy">
            <ChatMessageCopyButton
              state={messageCopy.stateOf(row.id)}
              onCopy={() => copyCard(row)}
            />
            <ChatMessageCopyStatus state={messageCopy.stateOf(row.id)} />
          </span>
          <button
            type="button"
            className="reply-card__icon-btn"
            aria-label={t.replies.jumpToChat}
            title={t.replies.jumpToChat}
            onClick={() => void jumpToChat(row)}
          >
            <JumpToMessageIcon size={13} />
          </button>
          {actions}
          {expirable && (
            <button
              type="button"
              className="reply-card__icon-btn"
              data-testid="expire-card"
              aria-label={t.replies.expire}
              title={t.replies.expire}
              onClick={() => setExpireTarget(row)}
            >
              <ExpireActionIcon size={13} />
            </button>
          )}
        </div>
        {stampNode}
      </header>
    );
  }

  /** What an OPEN row shows while its one-card read is in flight or has failed.
   * An opened card that renders nothing at all is indistinguishable from an
   * empty ask, which is the one thing this must not look like. */
  function renderOpenState(id: string) {
    return openErrors.has(id) ? (
      <div className="reply-card__error" data-testid="card-load-error">
        {t.replies.loadError}
      </div>
    ) : (
      <div className="reply-card__hint" data-testid="card-loading">
        {t.replies.lookupLoading}
      </div>
    );
  }

  /** One card. The task row + head are ALWAYS drawn (that is the collapsed
   * line); opening adds the QUESTION under them. The whole article is the
   * toggle — see `onCardToggleClick`. */
  function renderCard(
    row: ReplyCardRow,
    opts: {
      testId: string;
      className: string;
      stamp: ReactNode;
      expirable?: boolean;
      actions?: ReactNode;
      notice?: ReactNode;
      body: (card: ReplyCard) => ReactNode;
    }
  ) {
    const open = expandedIds.has(row.id);
    const card = openCards.get(row.id);
    return (
      <article
        key={row.id}
        id={`reply-card-${row.id}`}
        // 🔴 NOT `reply-card--collapsed`: that modifier zeroes the card's
        // padding for ChatReplyCard's bare one-line stub, and this page's
        // collapsed card is a REAL card (task row + head) that keeps its box.
        className={
          opts.className +
          (row.id === selectingId ? " reply-card--selecting" : "") +
          (row.id === pressMenu ? " reply-card--pressed" : "")
        }
        data-testid={opts.testId}
        data-reply-card-id={row.id}
        {...(row.id === selectingId
          ? { tabIndex: -1 }
          : {
              role: "button",
              tabIndex: 0,
              "aria-expanded": open,
              "aria-label": open ? t.replies.collapseCard : t.replies.expandCard,
              onClick: (e: React.MouseEvent<HTMLElement>) => onCardToggleClick(e, row.id),
              onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => onCardToggleKeyDown(e, row.id),
            })}
      >
        {renderTaskRef(row)}
        {renderHead(row, opts.stamp, opts.expirable, opts.actions)}
        {opts.notice}
        {/* The ask's TITLE, drawn once and in ONE place — always visible, above
          * the fold of a collapsed card. It is the third line of the head strip
          * the collapsed row copies (owner 2026-09-11「折疊起來那一列的內容不要另外
          * 設計」), and it is what keeps a stack of asks from the same person
          * distinguishable while everything is shut. 🔴 The open body must NOT
          * print it again — 「卡片標題只出現一次」.
          * T-a20b: it is agent-authored free text like the body, so it goes
          * through Markdown; the light row carries it, so this costs no read. */}
        <ReplyCardLinkComments from={row.from} recipientFor={docCommentRecipientFor}>
          <Markdown source={row.summary} className="reply-card__summary doc-md" />
          {open &&
            (card ? (
              <ReplyCardMentionProvider
                members={members}
                workers={workers}
                replyCardId={row.id}
              >
                {opts.body(card)}
              </ReplyCardMentionProvider>
            ) : (
              renderOpenState(row.id)
            ))}
        </ReplyCardLinkComments>
      </article>
    );
  }

  function renderWaitingCard(row: ReplyCardRow, asSnoozed = false) {
    const opened = openCards.get(row.id);
    if (opened && opened.status !== "waiting") {
      return renderHandledCard({
        ...row,
        status: opened.status,
        answeredTs: opened.answeredTs,
        expiredTs: opened.expiredTs ?? null,
      });
    }
    const until = row.snoozedUntilTs ?? null;
    return renderCard(row, {
      testId: asSnoozed ? "snoozed-card" : "waiting-card",
      className: asSnoozed ? "reply-card reply-card--snoozed" : "reply-card",
      // ONE stamp, at the head's right edge, AFTER 標為過期 (owner 2026-09-11).
      // 🔴 No absolute 開卡 stamp beside it — owner saw one on this very head and
      // answered「我不想知道絕對時間 相對時間已經夠了」. A snoozed card says when it
      // comes back instead.
      // A fixed-width pill (icon + one unit, or MM/DD) on every device, the
      // full words only in its title and accessible name.
      stamp:
        asSnoozed && until !== null ? (
          <span className="reply-card__waited" data-testid="snoozed-until">
            <span
              className="reply-card__waited-pill"
              data-testid="snoozed-pill"
              role="img"
              aria-label={msg.replySnoozeBack(until)}
              title={msg.replySnoozeBack(until)}
            >
              <SnoozeActionIcon size={12} />
              <span className="reply-card__waited-short">{snoozeBackShort(until)}</span>
            </span>
          </span>
        ) : (
          <span className="reply-card__waited" data-testid="waited">
            <span
              className="reply-card__waited-pill"
              data-testid="waited-pill"
              role="img"
              aria-label={msg.replyWaitedFull(nowTs - row.createdTs)}
              title={msg.replyWaitedFull(nowTs - row.createdTs)}
            >
              <HourglassIcon size={12} />
              <span className="reply-card__waited-short">
                {formatDurationShort(nowTs - row.createdTs)}
              </span>
            </span>
          </span>
        ),
      expirable: true,
      actions: asSnoozed ? (
        <button
          type="button"
          className="reply-card__icon-btn"
          data-testid="unsnooze-card"
          aria-label={t.replies.unsnooze}
          title={t.replies.unsnooze}
          onClick={() => void doUnsnooze(row.id)}
        >
          <UnsnoozeActionIcon size={13} />
        </button>
      ) : (
        <ReplySnoozeMenu
          cardId={row.id}
          onSnooze={(ts) => doSnooze(row.id, ts)}
          openRequested={snoozeRequestId === row.id}
        />
      ),
      notice: unsnoozeErrors.has(row.id) ? (
        <div className="reply-card__error" data-testid="unsnooze-error">
          {t.replies.unsnoozeError}
        </div>
      ) : undefined,
      body: (card) => (
        <>
          {card.body && (
            <Markdown source={card.body} className="reply-card__body doc-md" />
          )}
          {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the
           * body — click an image to preview in the page's lightbox. */}
          <ReplyCardQuestionAttachments card={card} />

          {card.status === "waiting" && (
            <div className="reply-card__answer-side">
              <ReplyCardWaitingBody
                card={card}
                onAnswer={(input) => doAnswer(card.id, input)}
                onProvideSecret={(value) => doProvideSecret(card.id, value)}
                autoFocusSecret={secretFocusPendingRef.current.has(card.id)}
                onSecretAutoFocused={() => secretFocusPendingRef.current.delete(card.id)}
              />
            </div>
          )}
        </>
      ),
    });
  }

  function renderHandledCard(row: ReplyCardRow) {
    const expired = row.status === "expired";
    const ts = handledTsOf(row);
    return renderCard(row, {
      testId: expired ? "expired-card" : "answered-card",
      className: `reply-card ${
        expired ? "reply-card--expired" : "reply-card--answered"
      }`,
      // Absolute date+time (7/13 09:05) — the bare hh:mm was ambiguous the
      // moment a card aged past midnight. This is the ONE absolute stamp left
      // on the page: 「不想知道絕對時間」 was about the waiting card's 開卡 stamp,
      // and a handled card has no ticking counter to say it instead.
      stamp:
        ts !== null ? (
          <span className="reply-card__answered-at">
            {row.kind === "secret"
              ? expired
                ? t.replies.expiredTag
                : t.tasks.replyAnsweredTag
              : expired
                ? msg.replyExpiredAt(formatAbsolute(ts, nowTs))
                : msg.replyAnsweredAt(formatAbsolute(ts, nowTs))}
          </span>
        ) : undefined,
      body: (card) => (
        <>
          {card.body && (
            <Markdown source={card.body} className="reply-card__body doc-md" />
          )}
          {/* The question's attachments outlive its settling — same strip on a
           * handled card (answered/expired). */}
          <ReplyCardQuestionAttachments card={card} />

          <div className="reply-card__answer-side">
            {card.status === "expired" ? (
              <ReplyCardExpiredBody card={card} />
            ) : card.status === "answered" ? (
              <ReplyCardAnsweredBody
                card={card}
                onReanswer={(input) => doReanswer(card.id, input)}
              />
            ) : null}
          </div>
        </>
      ),
    });
  }

  const page = (
    <div
      className={"replies" + (selectingId ? " replies--selecting" : "")}
      ref={pageRef}
      {...longPress}
    >
      {error && <div className="replies__error">{t.replies.loadError}</div>}
      {actionError && (
        <div className="replies__error" data-testid="replies-action-error">
          {actionError}
        </div>
      )}

      <section className="replies__section">
        <div className="replies__head">
          <ReplyListTabs
            views={LIST_VIEWS}
            selected={view}
            labels={viewLabels}
            counts={loading || error ? null : viewCounts}
            ariaLabel={t.replies.tabsLabel}
            idPrefix={tabsId}
            onSelect={setView}
          />
          <div className="replies__head-tools">
            {view === "answered" && (
              <span className="replies__section-hint" data-testid="replies-answered-window">
                {t.replies.tabAnsweredHint}
              </span>
            )}
            <FilterPanel
              testId="replies-filter"
              clearLabel={t.replies.clearFilters}
              onClear={narrowing ? clearFilters : undefined}
              collapse={{ title: t.replies.filterDialogTitle, activeCount: activeFilterCount }}
              actions={
                <DisplayOptions
                  label={t.replies.sortLabel}
                  value={waitingSort}
                  defaultValue={DEFAULT_WAITING_SORT}
                  options={WAITING_SORTS.map((v) => ({ value: v, label: sortLabels[v] }))}
                  onChange={setWaitingSort}
                  selectTestId="replies-sort"
                />
              }
            >
              <FilterField label={t.replies.filterIdLabel} wide>
                <IdFilterInput
                  value={draftId}
                  onChange={setDraftId}
                  onCommit={commitId}
                  label={t.replies.filterIdLabel}
                  testId="filter-reply-card-id"
                  // 15 = the length of every 請示卡 id there is: api_replycards.go
                  // mints "rc-" + newHexID(12). ⚠️ Say LENGTH here, not the other
                  // word: a literal prose guard
                  // (TestRetiredTaskNoSentencesDoNotLiveAnywhereInTheTree) scans for it.
                  widthCh={15}
                />
              </FilterField>
              <FilterField label={t.replies.filterOpenerNoun}>
                <MultiSelectFilter
                  noun={t.replies.filterOpenerNoun}
                  allLabel={t.replies.filterOpenerAll}
                  options={openerOptions}
                  selected={openerFilter}
                  onChange={setOpenerFilter}
                  testId="filter-opener"
                />
              </FilterField>
              <FilterField label={t.replies.filterPriorityNoun}>
                <MultiSelectFilter
                  noun={t.replies.filterPriorityNoun}
                  allLabel={t.replies.filterPriorityAll}
                  options={priorityOptions}
                  selected={priorityFilter}
                  onChange={setPriorityFilter}
                  testId="filter-priority"
                />
              </FilterField>
              <FilterField label={t.replies.filterDueNoun}>
                <MultiSelectFilter
                  noun={t.replies.filterDueNoun}
                  allLabel={t.replies.filterDueAll}
                  options={dueOptions}
                  selected={dueFilter}
                  onChange={setDueFilter}
                  testId="filter-due"
                />
              </FilterField>
            </FilterPanel>
          </div>
        </div>
        <div
          role="tabpanel"
          id={`${tabsId}-panel`}
          aria-labelledby={`${tabsId}-tab-${view}`}
          className="replies__panel"
        >
          {handledLoadError && wantsHandled && (
            <div className="replies__error" data-testid="handled-load-error">
              {t.replies.loadError}
            </div>
          )}
          {!loading && !error && listItems.length === 0 && !(wantsHandled && handledLoadError) ? (
            /* THREE, and the split that survives is 「we have an answer」 vs
             * 「we do not」. An empty page and an empty RESULT stay separate —
             * saying 「目前沒有待處理的請示」 while six cards sit behind a filter
             * reads as "you are all caught up". And a network/500 failure keeps
             * its own line, because nothing was ever asked and 0 筆 would be a
             * claim about an unanswered question.
             *
             * 🔴 A FOURTH used to sit here: a 404 got its own sentence
             * (「找不到「X」…」). owner 2026-09-06 removed it — he saw it on the
             * trial station and answered 「為什麼要顯示這種東西 拿掉!」, then
             * 「UI不是本來就秀0筆了嗎」. A 404 now falls through to the ordinary
             * 沒有符合篩選條件的請示, with the count and the 已篩選 條件 beside it.
             * What made round 1 dishonest was never this sentence — it was that
             * the page FILTERED THE ROWS IT HAPPENED TO HOLD, so an unloaded card
             * and a non-existent one really were the same screen. The by-id
             * lookup asks the server, so the two are now different facts; the
             * sentence was belt-and-braces on top of a fix that already works.
             * Do not restore it as a bug fix. */
            lookup.state === "failed" ? (
              <div
                className="replies__error"
                data-testid="replies-lookup-failed"
              >
                {t.replies.lookupFailed}
              </div>
            ) : lookup.state === "loading" || handledLoading ? (
              <div className="replies__empty" data-testid="replies-lookup-loading">
                {t.replies.lookupLoading}
              </div>
            ) : (
              <div className="replies__empty" data-testid="replies-empty">
                {narrowing
                  ? t.replies.emptyFiltered
                  : view === "snoozed"
                    ? t.replies.emptySnoozed
                    : view === "answered"
                      ? t.replies.emptyAnswered
                      : t.replies.empty}
              </div>
            )
          ) : (
            <div className="replies__list">
              {listItems.map(({ row, status }) =>
                status === "waiting" || status === "snoozed"
                  ? renderWaitingCard(row, status === "snoozed")
                  : renderHandledCard(row)
              )}
            </div>
          )}
        </div>
      </section>

      {expireTarget && (
        <ConfirmModal
          testId="expire-confirm"
          confirmTestId="expire-confirm-btn"
          body={msg.replyExpireConfirmBody(expireTarget.summary)}
          cancelLabel={t.common.cancel}
          confirmLabel={t.replies.expireConfirm}
          busy={expireBusy}
          danger
          onCancel={() => setExpireTarget(null)}
          onConfirm={() => void doExpire(expireTarget)}
        />
      )}

      {expandedIds.size > 0 && <OutsourceWorkersFeed onWorkers={setWorkers} />}
    </div>
  );

  return (
    <ReplyCardQuoteProvider>
      {selectingId ? (
        <ChatSelectionModeBar
          root={pageRef}
          rowSelector={SELECTING_CARD}
          placement="screen"
          onCopy={(text) => void messageCopy.copy(selectingId, text)}
          copyState={messageCopy.stateOf(selectingId)}
          onCancel={() => {
            focusCardOnExitRef.current = true;
            exitSelection();
          }}
          onReplied={() => exitSelection(true)}
        />
      ) : (
        <ChatSelectionReply root={pageRef} />
      )}
      {page}
      {pressMenu && (
        <ChatMessagePressMenu
          items={pressMenuItems(pressMenu)}
          onClose={() => setPressMenu(null)}
        />
      )}
    </ReplyCardQuoteProvider>
  );
}

function OutsourceWorkersFeed({
  onWorkers,
}: {
  onWorkers: (workers: OutsourceWorkerView[]) => void;
}) {
  const { workers } = useOutsourceWorkers();
  useEffect(() => onWorkers(workers), [workers, onWorkers]);
  return null;
}

function ReplyListTabs({
  views,
  selected,
  labels,
  counts,
  ariaLabel,
  idPrefix,
  onSelect,
}: {
  views: readonly ListView[];
  selected: ListView;
  labels: Record<ListView, string>;
  /** Null while the lists load; a null entry is a list not fetched yet. */
  counts: Record<ListView, number | null> | null;
  ariaLabel: string;
  idPrefix: string;
  onSelect: (view: ListView) => void;
}) {
  const tabRefs = useRef(new Map<ListView, HTMLButtonElement>());
  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    const at = views.indexOf(selected);
    const next =
      e.key === "ArrowRight"
        ? views[(at + 1) % views.length]
        : e.key === "ArrowLeft"
          ? views[(at - 1 + views.length) % views.length]
          : e.key === "Home"
            ? views[0]
            : e.key === "End"
              ? views[views.length - 1]
              : null;
    if (!next) return;
    e.preventDefault();
    onSelect(next);
    tabRefs.current.get(next)?.focus();
  }
  return (
    <div
      className="replies__tabs"
      role="tablist"
      aria-label={ariaLabel}
      data-testid="replies-tabs"
      onKeyDown={onKeyDown}
    >
      {views.map((v) => {
        const active = v === selected;
        const count = counts?.[v] ?? null;
        return (
          <button
            key={v}
            ref={(el) => {
              if (el) tabRefs.current.set(v, el);
              else tabRefs.current.delete(v);
            }}
            type="button"
            role="tab"
            id={`${idPrefix}-tab-${v}`}
            aria-selected={active}
            aria-controls={`${idPrefix}-panel`}
            tabIndex={active ? 0 : -1}
            className={`replies__tab${active ? " replies__tab--active" : ""}`}
            data-testid={`replies-tab-${v}`}
            onClick={() => onSelect(v)}
          >
            <span className="replies__tab-label" title={labels[v]}>
              {labels[v]}
            </span>
            {count !== null && (
              <span className="replies__tab-count" data-testid={`replies-tab-count-${v}`}>
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
