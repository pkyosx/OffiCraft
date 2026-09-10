// RepliesPage — the 等我回覆 page (SPEC §2, M2 reply cards B2): every member's
// pending asks in one place, two panes.
//
//   待回覆    — cards still waiting, NEWEST ASK FIRST (createdTs desc, FE
//              display sort over the server's longest-waiting-first list —
//              T-b07f); every card wears the SAME style — no longest-waiting
//              highlight (owner ruled it out, T-9ea9).
//   近期已處理 — cards answered OR expired within 24h, merged newest-handled
//              first. The PANE is collapsed by default (vibe-clicking style)
//              and its lists are not even fetched until it is opened; not
//              persisted — every visit starts collapsed.
//
// 🔴 A CARD IS COLLAPSED UNTIL IT IS OPENED, AND OPENING ONE IS WHAT READS IT
// (owner ruling 2026-09-07). The panes are lists of LIGHT ROWS
// (`ReplyCardRow`: the ask's title, its status and its stamps — no body, no
// options, no chat anchor), because `?view=full` is gone from the wire; a row
// draws the collapsed line and NOTHING more can be drawn from it. Opening a row
// reads that ONE card (`api.getReplyCard`) and only then renders the head
// (initiator + 跳到原訊息 + 標為過期), the task ref, the question and the SHARED
// ReplyCardBody. This is the SAME mechanism the inline chat card
// (ChatReplyCard) has used since T-48 — copied deliberately, not re-invented:
// two surfaces with two different lazy-load rules is how they drift.
// A card also CLOSES again on a second click (owner 2026-09-07): before this,
// the only way out of an opened card was to answer it or mark it expired.
// The ONE exception to "starts collapsed" is the LEADING 待回覆 card, which the
// page opens for the owner — on arrival and again whenever the card he was on
// leaves the pane — until he shuts one himself (see autoOpenOffRef below).
//
// The card interiors live in ReplyCardBody.tsx, SHARED with B3's inline chat
// card (ChatReplyCard) so the two surfaces can never drift. Answering is the
// only POSITIVE way a card leaves 待回覆; 標為過期 (the owner or an admin agent here,
// and since T-1b88 the card's own author via the API) is the sole
// other exit (terminal, NOT an answer — the agent reopens a fresh card if the
// question still matters). The nav badge (waiting count) and the chat unread
// red dot are independent signals: answering here never touches the red dot.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type {
  ReplyCard,
  ReplyCardAnswerInput,
  ReplyCardRow,
} from "../api/adapter";
import { isHttpStatus } from "../api/errors";
import { useMembers } from "../hooks/useMembers";
import { useReplyCards } from "../hooks/useReplyCards";
import {
  useWorkerAvatarUrls,
  useWorkerCodenames,
} from "../hooks/useWorkerCodenames";
import { useHashRoute } from "../lib/hashRoute";
import { avatarKindForMember } from "../lib/avatarKind";
import { ReplyCardAvatarButton } from "./ReplyCardAvatarButton";
import { Avatar } from "./Avatar";
import { ChevronRightIcon } from "./icons";
import { FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
import { MultiSelectFilter, type MultiSelectOption } from "./MultiSelectFilter";
import { ConfirmModal } from "./ConfirmModal";
import { Markdown } from "./Markdown";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardQuestionAttachments,
  ReplyCardTaskRef,
  ReplyCardWaitingBody,
} from "./ReplyCardBody";
import { formatDuration } from "../lib/duration";
import { formatAbsolute } from "../lib/dateFormat";
import "./office.css"; // chat composer classes the ReplyComposer reuses
import "./replies.css";

const HANDLED_WINDOW_SECONDS = 24 * 3600;

/** A handled card's pane stamp (answeredTs / expiredTs — whichever its
 * terminal state carries). */
function handledTsOf(card: ReplyCardRow): number | null {
  return card.status === "expired"
    ? (card.expiredTs ?? null)
    : card.answeredTs;
}

export function RepliesPage({ replyCardId }: { replyCardId?: string }) {
  const { t, msg } = useI18n();
  // Light roster (T-cf91): the page attributes each card to its asker by
  // name + role only, so it takes the identity-only projection AND does not
  // refetch the roster when anyone in the company sends a chat message.
  const { members } = useMembers({ light: true });
  const {
    waiting,
    handled,
    handledCount,
    handledLoaded,
    loading,
    error,
    loadHandled,
    refresh,
    answer,
    reanswer,
    expire,
  } = useReplyCards();
  const [, setRoute] = useHashRoute();

  // ── 篩選 (T-93 round 2) ─────────────────────────────────────────────────
  // ── ID 篩選 (T-118) ──────────────────────────────────────────────────────
  // The field is permanently visible above the list — no funnel, no panel, no
  // 取消/套用篩選, no 「N 筆 · 已篩選」 strip, and no 「請示卡」 sub-title above it.
  // owner 2026-09-06 20:07 (c-c3d681fe05da):「不要多filter那一層了,全部拉出來
  //…也不用再顯示14筆已篩選跟那一行跟案件那個子標了,案件跟請示卡都一樣」, and
  // again at 20:19 (c-38c7759e6377):「請示卡跟任務都要改成一樣的呈現方式,一樣
  // 請示卡的子標題拿掉」. The shell is still the shared FilterPanel (read
  // FilterPanel.tsx's header — that contract is not restated here); it is now a
  // row rather than an expander, so both pages got the new shape at once.
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
  // owner having to open the panel and press anything.
  useEffect(() => {
    if (!replyCardId) return;
    setAppliedId(replyCardId);
    setDraftId(replyCardId);
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

  // The live row wins over the fetched one when the page already holds it: the
  // panes are kept in sync by SSE + the write-adoption in useReplyCards, and a
  // one-shot GET is a snapshot from one instant.
  const foundCard: ReplyCardRow | null =
    lookup.state === "found"
      ? (waiting.find((c) => c.id === lookup.card.id) ??
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
    if (replyCardId) setRoute({ page: "replies" });
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
    () => new Set()
  );
  // What 清除篩選 is offered for — EITHER axis, not just the id. `filtering`
  // stays id-only on purpose: it also gates the by-id lookup's three outcomes
  // and the handled pane's force-expand, and an 開卡人 tick must not switch
  // those on (there is no id to have found, missed, or failed to reach).
  const anyFilter = idQuery !== "" || openerFilter.size > 0;

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

  // Transient action-failure notice (400/409/network). The composer keeps the
  // typed content; the option chips can simply be clicked again.
  const [actionError, setActionError] = useState<string | null>(null);

  // 標為過期 double-confirm (T-1aa4): expiring is terminal with no undo, so a
  // single mis-click must never close a card — the button only OPENS this
  // modal; the modal's confirm fires the action.
  const [expireTarget, setExpireTarget] = useState<ReplyCard | null>(null);
  const [expireBusy, setExpireBusy] = useState(false);

  // 近期已處理 collapses by default (vibe-clicking style) — the handled pane
  // is reference material, not work to do, so it must not crowd the 待回覆
  // pane. Plain component state on purpose: NOT persisted, every visit starts
  // collapsed. Owner 已回覆卡預設不載: the lists are NOT fetched while
  // collapsed — expanding pulls them (loadHandled); the header 「· N」 comes
  // from the counts.
  const [handledOpen, setHandledOpen] = useState(false);

  // Round 1 had a latched auto-load here: with a filter on, the collapsed
  // 近期已處理 pane had to be FETCHED before the filter could find a card in
  // it, or the page answered "no match" for a card sitting right there. That is
  // gone with the by-id lookup — the found card comes from the server, so
  // nothing about reaching it depends on which pane happens to be loaded. What
  // DID survive is the requirement behind it: a card the server returned must
  // never be invisible because a pane is collapsed. The handled pane is
  // therefore FORCE-EXPANDED while a filter is applied (see handledExpanded).

  useEffect(() => {
    if (!replyCardId) return;
    const card = document.getElementById(`reply-card-${replyCardId}`);
    if (!card) return;
    card.scrollIntoView({ block: "center" });
    card.focus({ preventScroll: true });
  }, [replyCardId, waiting, handled, handledOpen]);

  function toggleHandled() {
    setHandledOpen((wasOpen) => {
      const open = !wasOpen;
      if (open && !handledLoaded) loadHandled();
      return open;
    });
  }

  // Display order = 開卡時間 newest first (stable sort over the server's
  // longest-waiting-first list). No per-card highlight: the owner ruled the
  // longest-waiting accent ring out (T-9ea9) — every card wears the same face.
  //
  // With a filter applied the two panes hold exactly what the SERVER returned
  // for that id — one card, in whichever pane its status belongs to — not a
  // narrowing of the loaded rows.
  // The 24h window, in ONE place. It decides two things that must never drift
  // apart: which handled cards are VISIBLE, and which ones the 開卡人 counts are
  // computed over. Two copies of this predicate would let a future edit narrow
  // one and not the other, and the symptom — a name offered with a count no
  // list can produce — would look like a counting bug rather than a window one.
  const withinHandledWindow = (c: ReplyCardRow) => {
    const ts = handledTsOf(c);
    return ts !== null && nowTs - ts < HANDLED_WINDOW_SECONDS;
  };

  // 開卡人 predicate. An empty set is "no constraint", so an unticked axis is
  // free rather than exclusive — same convention as 任務頁's three dropdowns.
  const passesOpener = (c: ReplyCardRow) =>
    openerFilter.size === 0 || openerFilter.has(c.from);

  // 🔴 THE 開卡人 AXIS ANDs WITH THE ID, IT DOES NOT REPLACE IT. An id names ONE
  // card and is answered by the SERVER; if that card's opener fails this axis,
  // the honest answer is the ordinary filtered-empty, not the card. Letting the
  // id win would make 「找到了，但不是這個人開的」 render as a hit, which is the
  // same class of merged-silence defect the id lookup exists to remove.
  const waitingSorted = filtering
    ? foundCard && foundCard.status === "waiting" && passesOpener(foundCard)
      ? [foundCard]
      : []
    : [...waiting]
        .filter(passesOpener)
        .sort((a, b) => b.createdTs - a.createdTs);

  const visibleHandled = filtering
    ? // 🔴 NO 24h PRUNE on a card fetched by id. The window is what makes the
      // unfiltered pane a "recent" list; applying it to an answer from the
      // server would re-create the exact hole this round removes — the owner
      // asks for a card, the server hands it over, and the page drops it for
      // being old.
      foundCard && foundCard.status !== "waiting" && passesOpener(foundCard)
      ? [foundCard]
      : []
    : handled.filter((c) => passesOpener(c) && withinHandledWindow(c));
  // ── 開卡了哪一張 (owner 2026-09-07) ─────────────────────────────────────────
  // A row is a TITLE, not a card: since `?view=full` left the wire the panes
  // carry `ReplyCardRow`s, so the only thing this page can draw without asking
  // the server is the collapsed line. `expandedIds` is which rows the owner has
  // opened, `openCards` the ONE-card reads those opens produced, `openErrors`
  // the ids whose read failed (an opened row must say so rather than sit empty).
  //
  // NOT PERSISTED. The 待回覆 pane's leading card opens by itself (see below);
  // everything else starts closed — the same posture as the 近期已處理 pane's
  // own toggle, and as ChatReplyCard's stub.
  const [expandedIds, setExpandedIds] = useState<Set<string>>(() => new Set());
  const [openCards, setOpenCards] = useState<Map<string, ReplyCard>>(
    () => new Map()
  );
  const [openErrors, setOpenErrors] = useState<Set<string>>(() => new Set());

  // A DEEP LINK OPENS ITS CARD. `#replies/card/<id>` (a notification tap, a
  // shared link) is a request for THAT card, so landing on a collapsed line the
  // owner still has to click would be a step backwards from what the link used
  // to do. Only the routed id — everything else on the page stays closed.
  useEffect(() => {
    if (!replyCardId) return;
    setExpandedIds((prev) =>
      prev.has(replyCardId) ? prev : new Set(prev).add(replyCardId)
    );
  }, [replyCardId]);

  const autoOpenOffRef = useRef(false);

  /** Toggle one card open/closed. Closing is a real exit (owner 2026-09-07:
   * answering and 標為過期 used to be the only ways out of an opened card), and
   * it also DROPS the read below, so re-opening reads the card again rather
   * than showing whatever it said minutes ago. */
  function toggleCard(id: string) {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
        // Shutting a 待回覆 card is the owner saying「這張先放著」. From here on
        // the page stops opening cards for him (see autoOpenOffRef).
        if (waitingSorted.some((row) => row.id === id))
          autoOpenOffRef.current = true;
      } else next.add(id);
      return next;
    });
  }

  // ── 一張接一張 (owner rc-cd351785b83d [0], rc-fa7e4c9bce42 [0]) ────────────
  // The LEADING 待回覆 card opens by itself: on arrival, and again each time the
  // one the owner was working on leaves the pane (answered or 標為過期), so a
  // sitting of asks is worked through without a click between cards.
  //
  // 🔴 UNTIL HE SHUTS ONE HIMSELF. `autoOpenOffRef` latches on that gesture and
  // stays latched for the rest of the visit — collapsing a card IS「先放著」,
  // and popping the next one open in its place is the page arguing with him.
  // It is a REF, not derived state: the whole point is that it survives every
  // later recompute of who is open. It resets only by leaving the page (this
  // component unmounting), same as `expandedIds` itself — a fresh visit is a
  // fresh sitting, so the leading card opens again.
  //
  // The condition is "no card in the pane is open", not "the first one is
  // open": that single test covers arrival, the card leaving after an answer,
  // and the deep-link case (a routed card narrows the pane to itself, so it is
  // already open and nothing else is chosen for him).
  useEffect(() => {
    if (autoOpenOffRef.current) return;
    const lead = waitingSorted[0];
    if (!lead) return;
    if (waitingSorted.some((row) => expandedIds.has(row.id))) return;
    setExpandedIds((prev) =>
      prev.has(lead.id) ? prev : new Set(prev).add(lead.id)
    );
  });

  // The row's own version stamp. It is what decides that a card ALREADY READ is
  // stale — an answer from another window (or this page's own write, which
  // useReplyCards folds into the row) moves the row's status/stamps, and the
  // fetched card must follow or an opened card would keep rendering 待回覆 chips
  // for a card the server has settled.
  const rowVersion = (row: ReplyCardRow) =>
    `${row.status}:${row.answeredTs ?? 0}:${row.expiredTs ?? 0}`;

  const rowsById = new Map<string, ReplyCardRow>();
  for (const row of [...waiting, ...handled]) rowsById.set(row.id, row);
  if (foundCard) rowsById.set(foundCard.id, foundCard);

  // Which version of each open id has been READ (or is being read). One entry
  // per open card, so a row that has not moved is never re-fetched — and a row
  // that HAS moved is fetched exactly once per move, which is what keeps this
  // from looping against a server that disagrees with the row.
  const readVersionsRef = useRef<Map<string, string>>(new Map());
  const readCard = useCallback(async (id: string) => {
    try {
      const card = await api.getReplyCard(id);
      setOpenCards((prev) => new Map(prev).set(id, card));
      setOpenErrors((prev) => {
        if (!prev.has(id)) return prev;
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    } catch (e) {
      console.warn("RepliesPage: card read failed", e);
      setOpenErrors((prev) => new Set(prev).add(id));
    }
  }, []);

  // 🔴 THE ONLY PLACE A CARD IS READ. Opening a row does not fetch by itself —
  // it flips `expandedIds`, and this effect notices. That keeps "which cards are
  // open" and "which cards have been read" from being two rules that can
  // disagree, and it means a row moving underneath an open card re-reads it for
  // free (the version key changes).
  useEffect(() => {
    const seen = readVersionsRef.current;
    for (const id of expandedIds) {
      const row = rowsById.get(id);
      // No row means this pane is not showing the card at all — the one it just
      // answered, before the 近期已處理 pane has been loaded. Nothing renders it,
      // so reading it would buy a request for a card that is off screen; the
      // version check below re-reads it the moment a pane lists it again.
      if (!row) continue;
      const version = rowVersion(row);
      if (seen.get(id) === version) continue;
      seen.set(id, version);
      void readCard(id);
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

  // ── 開卡人 options + counts ────────────────────────────────────────────────
  // 🔴 COUNTED OVER THE PANES BEFORE THE OPENER AXIS NARROWS THEM. That is the
  // whole reason this axis stays usable: count the ALREADY-narrowed rows and a
  // ticked person becomes the only person with a non-zero count, so every other
  // name disappears and the owner cannot switch or untick. 任務頁 has the same
  // shape for the same reason (`inCountScope` there).
  //
  // The basis is 待回覆 ∪ 近期已處理-within-24h — i.e. what this page actually
  // holds. It deliberately does NOT include a card fetched by id: that card can
  // be older than the window, so counting it would make one person's number
  // jump by one for as long as an unrelated id is applied.
  const openerBasis = [...waiting, ...handled.filter(withinHandledWindow)];
  const openerCounts = new Map<string, number>();
  for (const c of openerBasis) {
    openerCounts.set(c.from, (openerCounts.get(c.from) ?? 0) + 1);
  }
  // 開卡人 下拉的選項在 `whoOf` 之後才建得起來（它要用 `codenames`），見下方。

  // The header count + zero-hide: the server counts until the lists are
  // loaded, then the client-pruned visible length (so an aging-out card drops
  // the header too while the page stays open).
  // 🔴 THE ZERO HERE IS LOAD-BEARING: the section below is hidden when this is
  // 0, so a 0 that means "not fetched yet" makes the whole 近期已處理 pane —
  // and the only handle for opening it — VANISH. An earlier cut of this filter
  // read `handledLoaded || idQuery !== ""` and did exactly that on the most
  // common path of all: a link to a card that IS in 待回覆 leaves the pane
  // unfetched, so 0 rows matched something nobody had loaded (independent
  // review, 2026-09-05). Only a LOADED list may narrow this number; unloaded
  // falls back to the server's whole-pane count, exactly as it does with no
  // filter at all.
  //
  // ⚠️ THE FALLBACK NUMBER IGNORES THE 開卡人 AXIS. `handledCount` is the
  // server's whole-pane total, so while the pane is still unloaded the title can
  // say a number larger than the tick would allow, and it drops to the filtered
  // one the moment the pane unfolds. That is the pre-existing fallback behaving
  // as designed (an unloaded list cannot narrow anything), but the opener axis
  // makes it much easier to notice than the id axis did — the id path sets
  // `filtering` and takes the first branch, an opener tick does not.
  const handledShown = filtering
    ? visibleHandled.length
    : handledLoaded
      ? visibleHandled.length
      : handledCount;
  // 🔴 A card the server returned must be ON SCREEN, not behind a collapsed
  // pane: 「找到了但畫面上沒有」 is the same silent nothing as a false empty.
  // While a filter is applied the handled pane is open and renders the fetched
  // card directly — it does not wait for `handledLoaded`, because with a filter
  // the pane's content is the lookup's answer, not the deferred 24h list.
  // Two flags, not one: `handledExpanded` is the AFFORDANCE (what the caret and
  // aria-expanded say), `handledListShown` is whether there are rows to draw.
  // They differ for one frame on an ordinary unfold — the toggle reads expanded
  // while loadHandled is still in flight — and folding them together would make
  // the caret lie for the length of that request.
  const handledExpanded = filtering || handledOpen;
  const handledListShown = filtering || (handledOpen && handledLoaded);

  // Outsource askers (ow- ids) get their codename from the lazy per-id read
  // rather than from `members`. Not because they are missing from it — GET
  // /api/members does carry kind='outsource' rows — but because this page
  // must cover the RELEASED ones too, and release soft-removes the row, which
  // is exactly what the endpoint filters out. One read that works for live AND
  // released keeps the identity row on the same 代號 the office rail shows
  // instead of the raw id. `whoOf` below routes to it on `kind === "outsource"`,
  // so a live worker that IS in `members` takes this path as well.
  const workerIds = [...waiting, ...handled].map((c) => c.from);
  const codenames = useWorkerCodenames(workerIds);
  const workerAvatarUrls = useWorkerAvatarUrls(workerIds);

  // Resolve the initiating member for a card's identity row. A card can
  // outlive its member (removed roster row) — fall back to the outsource
  // codename, then the raw id / no role, never fabricate.
  function whoOfId(fromId: string): { name: string; role: string } {
    const m = members.find((x) => x.id === fromId);
    if (!m || m.kind === "outsource") {
      const cn = codenames.get(fromId);
      return { name: cn ? msg.outsourceLabel(cn) : fromId, role: "" };
    }
    const role =
      (t.office.role as Record<string, string>)[m.role] ??
      (m.roleName || m.role);
    return { name: m.name, role };
  }
  function whoOf(card: ReplyCardRow): { name: string; role: string } {
    return whoOfId(card.from);
  }

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
  // block sits below `codenames` rather than beside `openerCounts` — it needs
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
  function jumpToChat(card: ReplyCard) {
    setRoute({
      page: "office",
      chatId: card.from,
      msgId: card.chatMessageId || undefined,
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
  // backTo: "replies" (owner acceptance-round finding, T-a706): without it,
  // OfficePage's own 返回 button resets to its default chat view (roster[0])
  // — correct when the panel was opened FROM the office page itself, but a
  // silent wrong-room landing when opened via this cross-page deep link,
  // since there was never a chat selected to return to. The marker tells
  // OfficePage's 返回 to land back on THIS page instead.
  function openProfile(card: ReplyCard) {
    const isRosterMember = members.some(
      (m) => m.id === card.from && m.kind === "staff",
    );
    setRoute(
      isRosterMember
        ? { page: "office", detailId: card.from, backTo: "replies" }
        : { page: "office", workerId: card.from, backTo: "replies" },
    );
  }

  // §3.6 請示 → 任務: a TASK-derived ask (card.task non-null) shows the 精簡
  // 任務資訊 row — the task's own TITLE + a 查看任務詳情 jump (adjudicated:
  // still never the task number / 識別鍵, and since T-ee17 acceptance not the
  // typeKey either). It renders directly under the card head, ahead of the
  // summary (owner 2026-08-14:「這個不能夠放到最一開始嗎？」) — at the bottom
  // the owner had to read the whole card to learn which work it is about.
  // The row itself lives in ReplyCardBody, shared with the inline chat card
  // (which moved in the same breath); only the route is ours.
  // The route carries the task id so the tasks page can locate the card
  // (auto-expanding 已結束 / clearing hiding filters). A pure chat ask renders
  // nothing here.
  function renderTaskRef(card: ReplyCard) {
    const task = card.task;
    if (!task) return null;
    return (
      <ReplyCardTaskRef
        task={task}
        onJump={() => setRoute({ page: "tasks", taskId: task.id })}
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

  async function doExpire(card: ReplyCard) {
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

  function renderHead(
    card: ReplyCard,
    waitedNode?: ReactNode,
    expirable = false
  ) {
    const who = whoOf(card);
    const asker = members.find((x) => x.id === card.from);
    return (
      <header className="reply-card__head">
        <ReplyCardAvatarButton
          onClick={() => openProfile(card)}
          src={
            (asker?.kind === "outsource" ? undefined : asker?.avatarUrl) ??
            workerAvatarUrls.get(card.from)
          }
          kind={avatarKindForMember(
            asker ?? { id: card.from }
          )}
        />
        <div className="reply-card__who">
          <span className="reply-card__name">{who.name}</span>
          {who.role && <span className="reply-card__role">{who.role}</span>}
        </div>
        <button
          type="button"
          className="reply-card__jump"
          onClick={() => jumpToChat(card)}
        >
          {t.replies.jumpToChat}
        </button>
        {/* T-1aa4: 標為過期 shares .reply-card__jump with 跳到原訊息 — one
            outlined style for both header actions, so they can never drift. */}
        {expirable && (
          <button
            type="button"
            className="reply-card__jump"
            data-testid="expire-card"
            onClick={() => setExpireTarget(card)}
          >
            {t.replies.expire}
          </button>
        )}
        {waitedNode}
      </header>
    );
  }

  /** The collapsed row — what a line draws with NO read at all: the asker's
   * face and name, their role, the task the ask hangs off, the ask's title, and
   * the stamp its status carries. All of it rides `ReplyCardRow` (`from` +
   * `task`), so the row still costs no read.
   *
   * 候選 E-B 「大頭像 · 左欄身分」. Owner 2026-09-10:「不對 你不用硬擠在一行」.
   * This cut gives the asker a real gutter — a 40px avatar down the left — and
   * stacks the three facts to its right, in the order a person actually reads a
   * list of asks: WHO first, then WHAT they are asking, then WHICH work it is
   * about. The shape is a mail/chat list row, not a compressed table line.
   *
   *   left   the avatar, at a size that is recognisable rather than decorative
   *   1      the asker's name, with their role beside it as its own badge
   *   2      the ask's title, 15px/600, WRAPPING rather than clipped
   *   3      the task, its FULL title, on its own soft band
   *
   * ABSENT FACTS ARE STILL NOT DRAWN. An outsource asker carries no role
   * (`whoOf` returns ""), a chat ask no task, and 外包 × 純聊天 would otherwise
   * be two empty cells; each is simply omitted, and nothing joins the cells, so
   * there is no orphaned separator to clean up either.
   *
   * OPEN, the whole block steps aside — the expanded card renders the identity
   * (renderHead), the task row (renderTaskRef) and the same title sentence, so
   * repeating any of it here would be the duplicate T-170 exists to remove. */
  function renderCollapsedRow(row: ReplyCardRow, stamps: ReactNode) {
    const open = expandedIds.has(row.id);
    const who = whoOf(row);
    const asker = members.find((x) => x.id === row.from);
    return (
      <button
        type="button"
        className={`reply-card__collapsed-row reply-card__collapsed-row--tight${
          open ? " reply-card__collapsed-row--open" : ""
        }`}
        aria-expanded={open}
        onClick={() => toggleCard(row.id)}
        data-testid="reply-card-toggle"
        data-reply-card-id={row.id}
      >
        <ChevronRightIcon
          size={12}
          className={`reply-card__caret${open ? " reply-card__caret--open" : ""}`}
        />
        {open ? (
          <span className="reply-card__collapsed-spacer" />
        ) : (
          <>
            {/* A GLYPH, NOT ReplyCardAvatarButton: this row IS a button, and a
                button inside a button is invalid HTML that browsers reflow out
                of the row. The avatar-as-second-target (T-a706) still lives on
                the expanded card's header, one click away. */}
            <span className="reply-card__collapsed-avatar" aria-hidden="true">
              <Avatar
                size={40}
                kind={avatarKindForMember(asker ?? { id: row.from })}
                src={
                  (asker?.kind === "outsource" ? undefined : asker?.avatarUrl) ??
                  workerAvatarUrls.get(row.from)
                }
              />
            </span>
            <span className="reply-card__collapsed-main">
              <span className="reply-card__collapsed-meta">
                <span
                  className="reply-card__collapsed-name"
                  data-testid="row-name"
                >
                  {who.name}
                </span>
                {who.role && (
                  <span
                    className="reply-card__collapsed-role"
                    data-testid="row-role"
                  >
                    {who.role}
                  </span>
                )}
              </span>
              <span className="reply-card__collapsed-summary">
                {row.summary}
              </span>
              {row.task?.title && (
                <span
                  className="reply-card__collapsed-task"
                  data-testid="row-task"
                >
                  {row.task.title}
                </span>
              )}
            </span>
          </>
        )}
        {stamps}
      </button>
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

  function renderWaitingCard(row: ReplyCardRow) {
    const card = openCards.get(row.id);
    return (
      <article
        key={row.id}
        id={`reply-card-${row.id}`}
        tabIndex={-1}
        className={`reply-card${
          expandedIds.has(row.id) ? "" : " reply-card--collapsed"
        }`}
        data-testid="waiting-card"
      >
        {renderCollapsedRow(
          row,
          // Two stamps, one column: the ABSOLUTE opened-at (date always
          // included — Seth 2026-07-13: reply-card times are absolute, no
          // relative-only display) above the existing ticking waited counter.
          // ONE right-hugging pill rather than a two-line column. The
          // ABSOLUTE opened-at stays (Seth 2026-07-13: reply-card times are
          // absolute, no relative-only display) with the ticking waited counter
          // beside it.
          <span className="reply-card__stamp-pill">
            <span className="reply-card__opened-at" data-testid="opened-at">
              {msg.replyOpenedAt(formatAbsolute(row.createdTs, nowTs))}
            </span>
            <span className="reply-card__waited" data-testid="waited">
              {msg.replyWaited(formatDuration(nowTs - row.createdTs))}
            </span>
          </span>
        )}
        {expandedIds.has(row.id) &&
          (card ? (
            <>
              {renderHead(card, undefined, card.status === "waiting")}
              {renderTaskRef(card)}

              {/* T-a20b: summary is agent-authored free text, same as body one
               * line down — it had no business rendering as plain text while
               * its sibling went through Markdown. */}
              <Markdown
                source={card.summary}
                className="reply-card__summary doc-md"
              />
              {card.body && (
                <Markdown source={card.body} className="reply-card__body doc-md" />
              )}
              {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the
               * body — click an image to preview in the page's lightbox. */}
              <ReplyCardQuestionAttachments card={card} />

              {card.status === "waiting" && (
                <ReplyCardWaitingBody
                  card={card}
                  onAnswer={(input) => doAnswer(card.id, input)}
                />
              )}
            </>
          ) : (
            renderOpenState(row.id)
          ))}
      </article>
    );
  }

  function renderHandledCard(row: ReplyCardRow) {
    const expired = row.status === "expired";
    const ts = handledTsOf(row);
    const card = openCards.get(row.id);
    return (
      <article
        key={row.id}
        id={`reply-card-${row.id}`}
        tabIndex={-1}
        className={`reply-card ${
          expired ? "reply-card--expired" : "reply-card--answered"
        }${expandedIds.has(row.id) ? "" : " reply-card--collapsed"}`}
        data-testid={expired ? "expired-card" : "answered-card"}
      >
        {renderCollapsedRow(
          row,
          // Absolute date+time (7/13 09:05) — the bare hh:mm was ambiguous
          // the moment a card aged past midnight.
          ts !== null ? (
            <span className="reply-card__stamp-pill reply-card__answered-at">
              {expired
                ? msg.replyExpiredAt(formatAbsolute(ts, nowTs))
                : msg.replyAnsweredAt(formatAbsolute(ts, nowTs))}
            </span>
          ) : undefined
        )}
        {expandedIds.has(row.id) &&
          (card ? (
            <>
              {renderHead(card)}
              {renderTaskRef(card)}

              {/* T-a20b — same free-text contract as the waiting card above. */}
              <Markdown
                source={card.summary}
                className="reply-card__summary doc-md"
              />
              {/* The question's attachments outlive its settling — same strip on
               * a handled card (answered/expired). */}
              <ReplyCardQuestionAttachments card={card} />

              {card.status === "expired" ? (
                <ReplyCardExpiredBody card={card} />
              ) : card.status === "answered" ? (
                <ReplyCardAnsweredBody
                  card={card}
                  onReanswer={(input) => doReanswer(card.id, input)}
                />
              ) : null}
            </>
          ) : (
            renderOpenState(row.id)
          ))}
      </article>
    );
  }

  return (
    <div className="replies">
      {error && <div className="replies__error">{t.replies.loadError}</div>}
      {actionError && (
        <div className="replies__error" data-testid="replies-action-error">
          {actionError}
        </div>
      )}

      {/* ── 篩選 (T-118) ──────────────────────────────────────────────────
        * The shell is FilterPanel's; only the FIELD is ours. Nothing here is a
        * modal and nothing here expands — it is a row above the list, because
        * owner ruled out the overlay (c-3b5a0aa66550) and then the expander
        * itself (c-c3d681fe05da). `testId` is namespaced so this panel's
        * controls never collide with the 任務頁's. */}
      <FilterPanel
        testId="replies-filter"
        clearLabel={t.replies.clearFilters}
        onClear={anyFilter ? clearFilters : undefined}
      >
        <IdFilterInput
          value={draftId}
          onChange={setDraftId}
          onCommit={commitId}
          label={t.replies.filterIdLabel}
          testId="filter-reply-card-id"
          // 15 = the length of every 請示卡 id there is: api_replycards.go:283
          // mints "rc-" + newHexID(12). owner 2026-09-06 spotted that the old
          // fixed 200px was picked with no reference to that, and it read as
          // too wide because it is. This number tracks the id's LENGTH, so it
          // moves if that shape ever does.
          // ⚠️ Say LENGTH here, not the other word. A prose guard
          // (TestRetiredTaskNoSentencesDoNotLiveAnywhereInTheTree) retires the
          // phrasing that says a value comes out of the id, because TaskNo
          // returns the id UNCHANGED and such wording becomes a second,
          // contradictory account of that field. It is a literal scan, so it
          // caught this comment TWICE: once for the original wording, and again
          // for the note that quoted the banned phrase in order to explain it.
          widthCh={15}
        />
        <MultiSelectFilter
          noun={t.replies.filterOpenerNoun}
          allLabel={t.replies.filterOpenerAll}
          options={openerOptions}
          selected={openerFilter}
          onChange={setOpenerFilter}
          testId="filter-opener"
        />
      </FilterPanel>

      <section className="replies__section">
        <div className="replies__section-title">
          {t.replies.waitingTitle}
          {!loading && !error && ` · ${waitingSorted.length}`}
        </div>
        {!loading && !error && waitingSorted.length === 0 ? (
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
          ) : lookup.state === "loading" ? (
            <div className="replies__empty" data-testid="replies-lookup-loading">
              {t.replies.lookupLoading}
            </div>
          ) : (
            <div className="replies__empty" data-testid="replies-empty">
              {/* 🔴 `anyFilter`, NOT `filtering`. `filtering` is id-only, and it
                * has a second job (gating the by-id lookup's three outcomes)
                * that an 開卡人 tick must not switch on. But the sentence below
                * is not about the id — it is about whether ANY axis is hiding
                * rows. With `filtering` here, ticking only 開卡人 and matching
                * nothing printed 「✓ 目前沒有待處理的請示」 while other people's
                * cards sat behind the filter: precisely the 「you are all caught
                * up」 lie the comment above this block forbids. Found by
                * independent review of 8204de4f. */}
              {anyFilter ? t.replies.emptyFiltered : t.replies.empty}
            </div>
          )
        ) : (
          <div className="replies__list">
            {waitingSorted.map((card) => renderWaitingCard(card))}
          </div>
        )}
      </section>

      {/* Zero-hide, EXCEPT while a filter is on. With a filter the section is
        * the answer to a question the owner asked, so it has to stay on screen
        * and say 0 — hiding it there removes the only handle for opening the
        * pane and makes "no match" indistinguishable from "nothing exists"
        * (independent review, 2026-09-05).
        *
        * 🔴 `anyFilter`, NOT `filtering` — the 2026-09-05 defect above was
        * reopened by the 開卡人 axis, because `filtering` only counts the id.
        * Ticking a person made this whole section vanish along with the only
        * handle for opening it, which is the same「no match looks like nothing
        * exists」failure, one axis over. Found by independent review of
        * 8204de4f. */}
      {(handledShown > 0 || anyFilter) && (
        <section className="replies__section">
          {/* The whole title row IS the toggle (collapsed by default): the
           * handled pane only unfolds on demand, vibe-clicking style — and the
           * lists are fetched only on that first unfold (loadHandled). The
           * 「· N」 comes from the count endpoint (answered + expired), so the
           * header + zero-hide hold without the lists. */}
          <button
            type="button"
            className="replies__section-toggle"
            aria-expanded={handledExpanded}
            onClick={toggleHandled}
            data-testid="answered-toggle"
          >
            <ChevronRightIcon
              size={13}
              className={`reply-card__caret${
                handledExpanded ? " reply-card__caret--open" : ""
              }`}
            />
            {`${t.replies.handledTitle} · ${handledShown}`}
            <span className="replies__section-hint">
              {t.replies.handledHint}
            </span>
          </button>
          {handledListShown && (
            <div className="replies__list">
              {visibleHandled.map((card) => renderHandledCard(card))}
            </div>
          )}
        </section>
      )}

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

    </div>
  );
}
