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

import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
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

  // ── ID 篩選 (T-93) ──────────────────────────────────────────────────────
  // owner asked for the SAME thing the 任務頁 filters are, and for a link to
  // do nothing more than pre-fill it (rc-2085e5ec60be, 2026-09-05):
  //「只是連過去幫忙帶篩選參數而已」。So `#replies/card/<id>` seeds this field
  // and nothing else — no by-id fetch, no locate notice.
  //
  // 🔴 THE COST OWNER TOOK KNOWINGLY (2026-09-05, c-0e183a7fbb10, verbatim
  // 「已回覆已經標示24 hrs才有資料，所以真的列不出來也沒關係」): the panes are
  // what the server sent, so a card ANSWERED OR EXPIRED more than 24h ago is
  // not on this page and this filter therefore cannot show it. He was told
  // and accepted it because the pane already says it only holds 24h. Do NOT
  // quietly add a by-id fetch to "fix" this — it is a decision, not a gap.
  // (The 待回覆 pane carries NO time window: every unanswered card is here
  // however old, so those are always reachable — server's waitingReplyCards.)
  const [idFilter, setIdFilter] = useState(replyCardId ?? "");
  useEffect(() => {
    if (replyCardId) setIdFilter(replyCardId);
  }, [replyCardId]);
  const idQuery = idFilter.trim().toLowerCase();
  const matchesId = (card: ReplyCard) =>
    idQuery === "" || card.id.toLowerCase().includes(idQuery);
  function clearFilters() {
    setIdFilter("");
    // Clearing the field must also drop the id from the URL, or a reload
    // would seed it straight back and the clear would look broken.
    if (replyCardId) setRoute({ page: "replies" });
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

  // A notification tap carries the card id in the hash.  Waiting cards are
  // already loaded; a handled one needs its collapsed pane fetched and opened
  // before it can be located.  Keeping this in the URL makes the destination
  // refresh-safe and works equally for an existing or newly opened PWA window.
  // Widened from the notification tap to ANY active ID 篩選 (T-93): a filter
  // that silently ignored the collapsed pane would answer 「沒有符合篩選條件的
  // 請示」 for a card that is sitting right there, unfetched — a false empty,
  // which is the one failure this control must not have.
  //
  // 🔴 AT MOST ONCE PER VISIT. `loadHandled` is two requests with no in-flight
  // de-duplication and `handledLoaded` only flips when they come back, so
  // without this latch every keystroke that matches nothing fires another
  // pair — a pasted 15-character id becomes ~30 requests, and a failing fetch
  // never settles so the amplification lasts the whole visit (independent
  // review, 2026-09-05). The pane's content does not depend on the query, so
  // fetching it once is all the filter ever needs.
  const handledAutoloadTried = useRef(false);
  useEffect(() => {
    // 🔴 `waiting` is [] until the first fetch lands, and an empty list at
    // mount means "not known yet", NOT "nothing matched". Firing then burns
    // the latch AND loads the handled pane on the one path that never needed
    // it — the link-to-a-waiting-card path this whole ticket exists for.
    if (loading || idQuery === "") return;
    if (handledLoaded || handledAutoloadTried.current) return;
    if (waiting.some((card) => matchesId(card))) return;
    handledAutoloadTried.current = true;
    setHandledOpen(true);
    void loadHandled();
  }, [loading, idQuery, waiting, handledLoaded, loadHandled]);

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
  const waitingSorted = [...waiting]
    .filter(matchesId)
    .sort((a, b) => b.createdTs - a.createdTs);

  const visibleHandled = handled.filter((c) => {
    if (!matchesId(c)) return false;
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
  const handledShown = handledLoaded ? visibleHandled.length : handledCount;

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

      {/* ── 篩選列 (T-93): one ID field, and the same 清除篩選 affordance the
        * 任務頁 filter row has. */}
      <div className="replies__filters">
        <IdFilterInput
          value={idFilter}
          onChange={setIdFilter}
          label={t.replies.filterIdLabel}
          testId="filter-reply-card-id"
          // 15 = the length of every 請示卡 id there is: api_replycards.go:283
          // mints "rc-" + newHexID(12). owner 2026-09-06 spotted that the old
          // fixed 200px was picked with no reference to that, and it read as
          // too wide because it is. This number tracks the id's LENGTH, so it
          // moves if that shape ever does.
          // (Wording note: "derived from the id" is a retired sentence in this
          // tree — a prose guard rejects it because TaskNo returns the id
          // UNCHANGED and any "derived" phrasing becomes a second, contradictory
          // account of that field. It caught this comment. Say "length".)
          widthCh={15}
        />
        {/* 🔴 The hash counts as an active filter even when the FIELD is empty.
          * Gate this on `idQuery` alone and the owner can delete the value by
          * hand, lose the button, and be left on `#replies/card/<id>` with no
          * control on screen that clears it — a reload, a share or a Back then
          * seeds the filter straight back. 任務頁 never had this hole: its
          * `anyFilter` names `taskIdFilter` explicitly, and both pages' docs
          * promise the same behaviour. */}
        {(idQuery !== "" || replyCardId) && (
          <button
            type="button"
            className="replies__clear-filters"
            data-testid="clear-filters"
            onClick={clearFilters}
          >
            {t.replies.clearFilters}
          </button>
        )}
      </div>

      <section className="replies__section">
        <div className="replies__section-title">
          {t.replies.waitingTitle}
          {!loading && !error && ` · ${waitingSorted.length}`}
        </div>
        {!loading && !error && waitingSorted.length === 0 ? (
          <div className="replies__empty" data-testid="replies-empty">
            {/* Two copies, the same split the 任務頁 already makes: an empty
              * page and an empty RESULT are different news. Saying 「目前沒有
              * 待處理的請示」 while six cards sit behind a filter would read as
              * "you are all caught up". */}
            {idQuery === "" ? t.replies.empty : t.replies.emptyFiltered}
          </div>
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
      {(handledShown > 0 || idQuery !== "") && (
        <section className="replies__section">
          {/* The whole title row IS the toggle (collapsed by default): the
           * handled pane only unfolds on demand, vibe-clicking style — and the
           * lists are fetched only on that first unfold (loadHandled). The
           * 「· N」 comes from the count endpoint (answered + expired), so the
           * header + zero-hide hold without the lists. */}
          <button
            type="button"
            className="replies__section-toggle"
            aria-expanded={handledOpen}
            onClick={toggleHandled}
            data-testid="answered-toggle"
          >
            <ChevronRightIcon
              size={13}
              className={`reply-card__caret${
                handledOpen ? " reply-card__caret--open" : ""
              }`}
            />
            {`${t.replies.handledTitle} · ${handledShown}`}
            <span className="replies__section-hint">
              {t.replies.handledHint}
            </span>
          </button>
          {handledOpen && handledLoaded && (
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
