// RepliesPage — the 等我回覆 page (SPEC §2, M2 reply cards B2): every member's
// pending asks in one place, two panes.
//
//   待回覆    — cards still waiting, NEWEST ASK FIRST (createdTs desc, FE
//              display sort over the server's longest-waiting-first list —
//              T-b07f); every card wears the SAME style — no longest-waiting
//              highlight (owner ruled it out, T-9ea9).
//              Each card: initiator (avatar + name + role),
//              jump-to-origin, 標為過期 (owner-only terminal, double-confirm —
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
// only POSITIVE way a card leaves 待回覆; the owner-only 標為過期 is the sole
// other exit (terminal, NOT an answer — the agent reopens a fresh card if the
// question still matters). The nav badge (waiting count) and the chat unread
// red dot are independent signals: answering here never touches the red dot.

import { useEffect, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { isHttpStatus } from "../api/errors";
import { useMembers } from "../hooks/useMembers";
import { useReplyCards } from "../hooks/useReplyCards";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { useHashRoute } from "../lib/hashRoute";
import { ReplyCardAvatarButton } from "./ReplyCardAvatarButton";
import { ChevronRightIcon } from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import { Markdown } from "./Markdown";
import { Lightbox } from "./AttachmentStrip";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardQuestionAttachments,
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

export function RepliesPage() {
  const { t } = useI18n();
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
  // A question attachment opened full-size (null = closed) — ONE page-level
  // shared Lightbox serves every card on the page.
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);

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
  const waitingSorted = [...waiting].sort((a, b) => b.createdTs - a.createdTs);

  const visibleHandled = handled.filter((c) => {
    const ts = handledTsOf(c);
    return ts !== null && nowTs - ts < HANDLED_WINDOW_SECONDS;
  });
  // The header count + zero-hide: the server counts until the lists are
  // loaded, then the client-pruned visible length (so an aging-out card drops
  // the header too while the page stays open).
  const handledShown = handledLoaded ? visibleHandled.length : handledCount;

  // Outsource askers (ow- ids) are never in the members roster — resolve their
  // codename via the lazy per-id read (works for live AND released workers) so
  // the identity row shows the same 代號 the office rail does, not the raw id.
  const codenames = useWorkerCodenames(
    [...waiting, ...handled].map((c) => c.from),
  );

  // Resolve the initiating member for a card's identity row. A card can
  // outlive its member (removed roster row) — fall back to the outsource
  // codename, then the raw id / no role, never fabricate.
  function whoOf(card: ReplyCard): { name: string; role: string } {
    const m = members.find((x) => x.id === card.from);
    if (!m) {
      const cn = codenames.get(card.from);
      return { name: cn ? t.office.outsource.label(cn) : card.from, role: "" };
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
  // reads: roster members go through #office/member/<id> (detailId), an
  // outsource asker (never in the roster) through #office/worker/<id>
  // (workerId) — OfficePage self-heals to the plain roster view if the id
  // doesn't resolve (e.g. a released worker), so this never dead-ends.
  //
  // backTo: "replies" (owner acceptance-round finding, T-a706): without it,
  // OfficePage's own 返回 button resets to its default chat view (roster[0])
  // — correct when the panel was opened FROM the office page itself, but a
  // silent wrong-room landing when opened via this cross-page deep link,
  // since there was never a chat selected to return to. The marker tells
  // OfficePage's 返回 to land back on THIS page instead.
  function openProfile(card: ReplyCard) {
    const isRosterMember = members.some((m) => m.id === card.from);
    setRoute(
      isRosterMember
        ? { page: "office", detailId: card.from, backTo: "replies" }
        : { page: "office", workerId: card.from, backTo: "replies" },
    );
  }

  // §3.6 請示 → 任務: a TASK-derived ask (card.task non-null) shows the 精簡
  // 任務資訊 row — the TYPE plus a 查看任務詳情 jump (adjudicated: never the
  // task number / 識別鍵). The route carries the task id so the tasks page can
  // locate the card (auto-expanding 已結束 / clearing hiding filters). A pure
  // chat ask renders nothing here.
  function renderTaskRef(card: ReplyCard) {
    const task = card.task;
    if (!task) return null;
    return (
      <div className="reply-card__task" data-testid="reply-task-ref">
        <span className="reply-card__task-type">
          <span className="reply-card__task-label">{t.replies.taskBadge}</span>
          {task.typeKey || t.tasks.adhoc}
        </span>
        <button
          type="button"
          className="reply-card__task-jump"
          data-testid="reply-task-jump"
          onClick={() => setRoute({ page: "tasks", taskId: task.id })}
        >
          {t.replies.viewTask}
          <ChevronRightIcon size={12} />
        </button>
      </div>
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
    return (
      <header className="reply-card__head">
        <ReplyCardAvatarButton
          onClick={() => openProfile(card)}
          kind={card.from.startsWith("ow-") ? "outsource" : "member"}
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
      <article key={card.id} className="reply-card" data-testid="waiting-card">
        {renderHead(
          card,
          // Two stamps, one column: the ABSOLUTE opened-at (date always
          // included — Seth 2026-07-13: reply-card times are absolute, no
          // relative-only display) above the existing ticking waited counter.
          <span className="reply-card__stamps">
            <span className="reply-card__opened-at" data-testid="opened-at">
              {t.replies.openedAt(formatAbsolute(card.createdTs, nowTs))}
            </span>
            <span className="reply-card__waited" data-testid="waited">
              {t.replies.waited(formatDuration(nowTs - card.createdTs))}
            </span>
          </span>,
          true
        )}

        {/* T-a20b: summary is agent-authored free text, same as body one line
         * down — it had no business rendering as plain text while its sibling
         * went through Markdown. */}
        <Markdown source={card.summary} className="reply-card__summary doc-md" />
        {card.body && (
          <Markdown source={card.body} className="reply-card__body doc-md" />
        )}
        {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the
         * body — click an image to preview in the page's lightbox. */}
        <ReplyCardQuestionAttachments card={card} onOpenImage={setLightboxSrc} />
        {renderTaskRef(card)}

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
                ? t.replies.expiredAt(formatAbsolute(ts, nowTs))
                : t.replies.answeredAt(formatAbsolute(ts, nowTs))}
            </span>
          ) : undefined
        )}

        {/* T-a20b — same free-text contract as the waiting card above. */}
        <Markdown source={card.summary} className="reply-card__summary doc-md" />
        {/* The question's attachments outlive its settling — same strip on a
         * handled card (answered/expired). */}
        <ReplyCardQuestionAttachments card={card} onOpenImage={setLightboxSrc} />
        {renderTaskRef(card)}

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

      <section className="replies__section">
        <div className="replies__section-title">
          {t.replies.waitingTitle}
          {!loading && !error && ` · ${waiting.length}`}
        </div>
        {!loading && !error && waiting.length === 0 ? (
          <div className="replies__empty" data-testid="replies-empty">
            {t.replies.empty}
          </div>
        ) : (
          <div className="replies__list">
            {waitingSorted.map((card) => renderWaitingCard(card))}
          </div>
        )}
      </section>

      {handledShown > 0 && (
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
          body={t.replies.expireConfirmBody(expireTarget.summary)}
          cancelLabel={t.common.cancel}
          confirmLabel={t.replies.expireConfirm}
          busy={expireBusy}
          danger
          onCancel={() => setExpireTarget(null)}
          onConfirm={() => void doExpire(expireTarget)}
        />
      )}

      {/* Full-size preview of a question attachment — the shared Lightbox. */}
      <Lightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
    </div>
  );
}
