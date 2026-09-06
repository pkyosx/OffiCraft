// RepliesPage — the 等我回覆 page (SPEC §2, M2 reply cards B2): every member's
// pending asks in one place, two panes.
//
//   待回覆    — cards still waiting, NEWEST ASK FIRST (createdTs desc, FE
//              display sort over the server's longest-waiting-first list —
//              T-b07f); every card wears the SAME style — no longest-waiting
//              highlight (owner ruled it out, T-9ea9).
//              Each card: initiator (avatar + name + role),
//              jump-to-origin, 標為過期 (terminal, double-confirm; this cockpit
//              button is the owner's entry — the API also admits an admin agent
//              and, since T-1b88, the card's own author —
//              T-1aa4), 已等你 {t} (ticking, computed from createdTs), the
//              question, then the SHARED ReplyCardWaitingBody (quick-reply
//              chips + typed composer).
//   近期已處理 — cards answered OR expired within 24h, merged newest-handled
//              first: answered cards keep the SHARED ReplyCardAnsweredBody
//              (final answer tagged 你選的/AI 建議, 查看當初選項, 重新決定);
//              expired cards render the grey terminal ReplyCardExpiredBody
//              (已過期 tag; no reopen). COLLAPSED BY DEFAULT (vibe-clicking
//              style): only the title row (「近期已處理 · N」 + hint) shows;
//              the row is a toggle button that expands/collapses the list. Not
//              persisted — every visit starts collapsed.
//
// The card interiors live in ReplyCardBody.tsx, SHARED with B3's inline chat
// card (ChatReplyCard) so the two surfaces can never drift. Answering is the
// only POSITIVE way a card leaves 待回覆; 標為過期 (the owner or an admin agent here,
// and since T-1b88 the card's own author via the API) is the sole
// other exit (terminal, NOT an answer — the agent reopens a fresh card if the
// question still matters). The nav badge (waiting count) and the chat unread
// red dot are independent signals: answering here never touches the red dot.

import { useEffect, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
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
import { ChevronRightIcon } from "./icons";
import { FilterPanel } from "./FilterPanel";
import { IdFilterInput } from "./IdFilterInput";
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
function handledTsOf(card: ReplyCard): number | null {
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
  // Round 1 put an always-visible 篩選列 here that narrowed the ALREADY-LOADED
  // cards on every keystroke. owner 2026-09-06 rejected that shape twice:
  //「很常我們要找一張任務或票而已，但每次都要全部都撈回來才濾不合理 你可以設計
  //  成要給完搜尋條件要再按 search 的版本嗎」and, on being shown an overlay,
  //「按搜尋時不要再跳出新modal」。So the fields live inside the shared
  // FilterPanel shell (FilterPanel.tsx — read its header before touching this):
  // an in-page panel that expands above the list, Cancel / 套用篩選 at the
  // bottom, and a 「N 筆 · 已篩選：<chip ×> · 清除全部」 strip once collapsed.
  //
  // TWO STATES, NOT ONE. `appliedId` is what the page is actually filtered by;
  // `draftId` is what the field binds to. Typing changes NOTHING until Apply —
  // that is the whole answer to 「每次都要全部都撈回來才濾不合理」, because
  // nothing is fetched or narrowed per keystroke.
  const [appliedId, setAppliedId] = useState(replyCardId ?? "");
  const [draftId, setDraftId] = useState(replyCardId ?? "");
  const [filterOpen, setFilterOpen] = useState(false);
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
  const foundCard =
    lookup.state === "found"
      ? (waiting.find((c) => c.id === lookup.card.id) ??
        handled.find((c) => c.id === lookup.card.id) ??
        lookup.card)
      : null;

  function clearFilters() {
    setAppliedId("");
    setDraftId("");
    // Clearing must also drop the id from the URL, or a reload would seed it
    // straight back and the clear would look broken.
    if (replyCardId) setRoute({ page: "replies" });
  }

  function applyFilters() {
    const next = draftId.trim();
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
  const waitingSorted = filtering
    ? foundCard && foundCard.status === "waiting"
      ? [foundCard]
      : []
    : [...waiting].sort((a, b) => b.createdTs - a.createdTs);

  const visibleHandled = filtering
    ? // 🔴 NO 24h PRUNE on a card fetched by id. The window is what makes the
      // unfiltered pane a "recent" list; applying it to an answer from the
      // server would re-create the exact hole this round removes — the owner
      // asks for a card, the server hands it over, and the page drops it for
      // being old.
      foundCard && foundCard.status !== "waiting"
      ? [foundCard]
      : []
    : handled.filter((c) => {
        const ts = handledTsOf(c);
        return ts !== null && nowTs - ts < HANDLED_WINDOW_SECONDS;
      });
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
  function whoOf(card: ReplyCard): { name: string; role: string } {
    const m = members.find((x) => x.id === card.from);
    if (!m || m.kind === "outsource") {
      const cn = codenames.get(card.from);
      return { name: cn ? msg.outsourceLabel(cn) : card.from, role: "" };
    }
    const role =
      (t.office.role as Record<string, string>)[m.role] ??
      (m.roleName || m.role);
    return { name: m.name, role };
  }

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

  function renderWaitingCard(card: ReplyCard) {
    return (
      <article key={card.id} id={`reply-card-${card.id}`} tabIndex={-1} className="reply-card" data-testid="waiting-card">
        {renderHead(
          card,
          // Two stamps, one column: the ABSOLUTE opened-at (date always
          // included — Seth 2026-07-13: reply-card times are absolute, no
          // relative-only display) above the existing ticking waited counter.
          <span className="reply-card__stamps">
            <span className="reply-card__opened-at" data-testid="opened-at">
              {msg.replyOpenedAt(formatAbsolute(card.createdTs, nowTs))}
            </span>
            <span className="reply-card__waited" data-testid="waited">
              {msg.replyWaited(formatDuration(nowTs - card.createdTs))}
            </span>
          </span>,
          true
        )}
        {renderTaskRef(card)}

        {/* T-a20b: summary is agent-authored free text, same as body one line
         * down — it had no business rendering as plain text while its sibling
         * went through Markdown. */}
        <Markdown source={card.summary} className="reply-card__summary doc-md" />
        {card.body && (
          <Markdown source={card.body} className="reply-card__body doc-md" />
        )}
        {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the
         * body — click an image to preview in the page's lightbox. */}
        <ReplyCardQuestionAttachments card={card} />

        <ReplyCardWaitingBody
          card={card}
          onAnswer={(input) => doAnswer(card.id, input)}
        />
      </article>
    );
  }

  function renderHandledCard(card: ReplyCard) {
    const expired = card.status === "expired";
    const ts = handledTsOf(card);
    return (
      <article
        key={card.id}
        id={`reply-card-${card.id}`}
        tabIndex={-1}
        className={`reply-card ${
          expired ? "reply-card--expired" : "reply-card--answered"
        }`}
        data-testid={expired ? "expired-card" : "answered-card"}
      >
        {renderHead(
          card,
          // Absolute date+time (7/13 09:05) — the bare hh:mm was ambiguous
          // the moment a card aged past midnight.
          ts !== null ? (
            <span className="reply-card__answered-at">
              {expired
                ? msg.replyExpiredAt(formatAbsolute(ts, nowTs))
                : msg.replyAnsweredAt(formatAbsolute(ts, nowTs))}
            </span>
          ) : undefined
        )}
        {renderTaskRef(card)}

        {/* T-a20b — same free-text contract as the waiting card above. */}
        <Markdown source={card.summary} className="reply-card__summary doc-md" />
        {/* The question's attachments outlive its settling — same strip on a
         * handled card (answered/expired). */}
        <ReplyCardQuestionAttachments card={card} />

        {expired ? (
          <ReplyCardExpiredBody card={card} />
        ) : (
          <ReplyCardAnsweredBody
            card={card}
            onReanswer={(input) => doReanswer(card.id, input)}
          />
        )}
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

      {/* ── 篩選 (T-93 round 2) ────────────────────────────────────────────
        * The shell is FilterPanel's; only the FIELDS are ours. Nothing here is
        * a modal — it expands in the page and pushes the list down, because
        * owner ruled out the overlay after seeing it. `testId` is namespaced so
        * this panel's controls never collide with the 任務頁's. */}
      <FilterPanel
        title={t.replies.filterTitle}
        count={loading ? null : waitingSorted.length + visibleHandled.length}
        open={filterOpen}
        onOpenChange={(next) => {
          // Opening RESEEDS the draft from what is applied: the panel must open
          // showing the filter that is actually in force, not whatever was left
          // in the field by a Cancel three minutes ago.
          if (next) setDraftId(appliedId);
          setFilterOpen(next);
        }}
        chips={
          filtering
            ? [
                {
                  key: "id",
                  label: t.replies.chipId(idQuery),
                  onRemove: clearFilters,
                },
              ]
            : []
        }
        onClearAll={clearFilters}
        onApply={applyFilters}
        // Cancel commits nothing: put the field back on the applied value so
        // the abandoned draft cannot leak into the next open.
        onCancel={() => setDraftId(appliedId)}
        testId="replies-filter"
      >
        <IdFilterInput
          value={draftId}
          onChange={setDraftId}
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
      </FilterPanel>

      <section className="replies__section">
        <div className="replies__section-title">
          {t.replies.waitingTitle}
          {!loading && !error && ` · ${waitingSorted.length}`}
        </div>
        {!loading && !error && waitingSorted.length === 0 ? (
          /* 🔴 FOUR COPIES, AND THEY MAY NEVER BE COLLAPSED INTO ONE. The
           * first two are the split the 任務頁 already makes — an empty page
           * and an empty RESULT are different news, and saying 「目前沒有待處
           * 理的請示」 while six cards sit behind a filter reads as "you are
           * all caught up". The last two are what round 2 adds, and they are
           * the whole point of asking the server: a 404 is the server SAYING
           * the id does not exist, while a network/500 failure means we never
           * got an answer at all. Round 1 had one sentence for all of it, and
           * that is exactly how a card the page simply had not loaded came out
           * looking identical to a card that does not exist. */
          lookup.state === "failed" ? (
            <div
              className="replies__error"
              data-testid="replies-lookup-failed"
            >
              {t.replies.lookupFailed}
            </div>
          ) : lookup.state === "missing" ? (
            <div
              className="replies__empty replies__empty--missing"
              data-testid="replies-lookup-missing"
            >
              {t.replies.lookupMissing(idQuery)}
            </div>
          ) : lookup.state === "loading" ? (
            <div className="replies__empty" data-testid="replies-lookup-loading">
              {t.replies.lookupLoading}
            </div>
          ) : (
            <div className="replies__empty" data-testid="replies-empty">
              {filtering ? t.replies.emptyFiltered : t.replies.empty}
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
        * (independent review, 2026-09-05). */}
      {(handledShown > 0 || filtering) && (
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
