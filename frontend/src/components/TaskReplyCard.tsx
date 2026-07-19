// TaskReplyCard — a reply card (等我回覆卡) rendered INLINE on a task card
// (SPEC §3.2 內嵌等我回覆卡, M3). A task step's ARMED gate carries
// `replyCardId`; this component refetches the SINGLE card for its full shape
// (options / status / answer) and again on every `reply_card` SSE delta —
// exactly the ChatReplyCard pattern, so answering HERE fans the same topic the
// 等我回覆 page's lists + nav badge reconcile on (回覆同步反映到等我回覆頁),
// and answering THERE flips this card in place. One task card may embed
// MULTIPLE of these (一問一卡), each answered/closed independently.
//
// The card interiors are the SHARED ReplyCardBody blocks (ReplyCardBody.tsx) —
// the same chips / AI 建議 tag / composer / 重新決定 flow as RepliesPage and
// the chat inline card. NEVER a re-implementation (spec: gate 即任務版的等我
// 回覆卡, 與 M2 打通、不是另一套).
//
// ANSWERED cards COLLAPSE to a one-line summary (owner 2026-07-14: 已回覆的卡
// 收合成一行摘要、可展開) — the summary + the standing answer + 已回覆 tag on
// one row, the chevron expands the full shared interior (重新決定 included).
// Answering IN this card keeps it expanded (you just acted — you see the
// result); a card that ARRIVES answered (or is answered elsewhere) collapses.

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { api } from "../api";
import { useHashRoute } from "../lib/hashRoute";
import { Lightbox } from "./AttachmentStrip";
import { Markdown } from "./Markdown";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardQuestionAttachments,
  ReplyCardWaitingBody,
} from "./ReplyCardBody";
import { ChevronRightIcon } from "./icons";
import "./replies.css";

export function TaskReplyCard({
  replyCardId,
  initialStatus,
  fallbackSummary,
}: {
  replyCardId: string;
  /** The bound step's read-time `reply_card_status` hint. ANSWERED or EXPIRED
   * (both terminal) → the card mounts COLLAPSED and does NOT fetch (owner
   * 已回覆卡預設不載); it loads only on expand. WAITING (the live ask) or
   * null/undefined loads eagerly, as before. */
  initialStatus?: ReplyCard["status"] | null;
  /** Shown in the collapsed stub of a lazy answered card BEFORE it is fetched
   * (the step name — the only summary available without the card). Once the
   * card loads, its own summary + answer replace it. */
  fallbackSummary?: string;
}) {
  const { t } = useI18n();
  const [, setRoute] = useHashRoute();
  const [card, setCard] = useState<ReplyCard | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // A question attachment opened full-size (null = closed) — the shared
  // Lightbox below.
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  // Answered-card collapse (owner 2026-07-14): an answered card shows as a
  // one-line summary unless expanded — or answered right here (doAnswer flips
  // this true). Default collapsed.
  const [answeredOpen, setAnsweredOpen] = useState(false);
  // Lazy-load gate: an answered-hinted card does NOT fetch on mount (owner
  // 已回覆卡預設不載) — it loads only once expanded. A waiting/unknown card loads
  // eagerly, as before. Separating the fetch gate from `answeredOpen` keeps the
  // pre-existing "an eagerly-loaded answered card still collapses by default"
  // behaviour for the no-hint path (fixtures / a stale hint).
  const lazyTerminal =
    initialStatus === "answered" || initialStatus === "expired";
  const shouldLoad = !lazyTerminal || answeredOpen;
  // Loaded-once guard so re-expanding never re-fires the load (answered cards
  // are terminal; the SSE effect + local re-answer keep a live card fresh).
  const loadedRef = useRef(false);
  // Latest card status for the SSE guard. SEEDED from the hint so a collapsed
  // answered card (not yet fetched) also ignores the reply_card fan-out — else
  // an unrelated delta would wake it into a fetch and defeat lazy-load (this is
  // the T-cdf4 guard extended to the never-fetched case).
  const statusRef = useRef<ReplyCard["status"] | null>(
    lazyTerminal ? initialStatus : null
  );

  const refetch = useCallback(async () => {
    const fresh = await api.getReplyCard(replyCardId);
    statusRef.current = fresh.status;
    setCard(fresh);
    setLoadError(false);
  }, [replyCardId]);

  // Load the card once it should be shown — eager on mount for a waiting/unknown
  // card, deferred to the first expand for an answered one; exactly once.
  useEffect(() => {
    if (!shouldLoad || loadedRef.current) return;
    let alive = true;
    refetch()
      .then(() => {
        loadedRef.current = true;
      })
      .catch((e) => {
        console.warn("TaskReplyCard: card load failed", e);
        if (alive) setLoadError(true);
      });
    return () => {
      alive = false;
    };
  }, [shouldLoad, refetch]);

  useEffect(() => {
    // Reconcile-by-refetch (contract B): a reply_card delta — an answer from
    // the 等我回覆 page, the chat card, another window — re-pulls THIS card so a
    // still-waiting card flips to answered in place. But the reply_card topic
    // is NOT per-card: any card being opened/answered fans it to every mounted
    // card. Once THIS card is answered (a terminal state) the only thing that
    // changes it is a local 重新決定, which refetches itself (doReanswer) — so
    // an already-answered card (incl. a collapsed, never-fetched one via the
    // seeded statusRef) ignores the SSE delta and stops the broadcast storm
    // (70+ historical cards no longer each refetch on one unrelated answer).
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "reply_card") return;
      if (statusRef.current === "answered" || statusRef.current === "expired")
        return;
      refetch().catch((e) =>
        console.warn("TaskReplyCard: SSE refetch failed", e)
      );
    });
    return unsubscribe;
  }, [refetch]);

  async function doAnswer(input: ReplyCardAnswerInput) {
    try {
      await api.answerReplyCard(replyCardId, input);
      setActionError(null);
      setAnsweredOpen(true); // you just answered — show the result, not a stub
      await refetch();
    } catch (e) {
      console.warn("TaskReplyCard: answer failed", e);
      setActionError(t.replies.answerError);
      throw e;
    }
  }

  async function doReanswer(input: ReplyCardAnswerInput) {
    try {
      await api.reanswerReplyCard(replyCardId, input);
      setActionError(null);
      await refetch();
    } catch (e) {
      console.warn("TaskReplyCard: re-answer failed", e);
      setActionError(t.replies.answerError);
      throw e;
    }
  }

  // The ask ALWAYS rides a chat message (card.chatMessageId) — the header's
  // 在聊天室回覆 link opens that member's chat located on the ask (same
  // hashRoute contract as RepliesPage's 跳到原訊息).
  function jumpToChat() {
    if (!card) return;
    setRoute({
      page: "office",
      chatId: card.from,
      msgId: card.chatMessageId || undefined,
    });
  }

  // ── collapsed one-line summary (answered + not expanded) ───────────────────
  // Collapsed when the card is answered-ish and not expanded: either the card
  // is loaded and answered, or it mounted with an answered hint and was never
  // expanded (lazy-load, not yet fetched). Two shapes: once fetched, the rich
  // one-line summary + standing answer; before that, a generic stub that
  // fetches the full card on expand.
  const terminal =
    card?.status === "answered" || card?.status === "expired";
  if ((lazyTerminal || terminal) && !answeredOpen) {
    const expiredStub =
      card?.status === "expired" || statusRef.current === "expired";
    const answerText = card
      ? card.answer?.optionIdx != null
        ? card.options[card.answer.optionIdx]
        : (card.answer?.text ?? "")
      : "";
    return (
      <div
        className="reply-card task-reply-card task-reply-card--collapsed"
        data-testid="task-reply-card"
        data-reply-card-id={replyCardId}
      >
        <button
          type="button"
          className="task-reply-card__collapsed-row"
          aria-expanded={false}
          aria-label={t.tasks.expandReply}
          title={t.tasks.expandReply}
          data-testid="task-reply-card-expand"
          onClick={() => setAnsweredOpen(true)}
        >
          <ChevronRightIcon size={12} className="reply-card__caret" />
          <span
            className={
              expiredStub
                ? "reply-tag reply-tag--expired"
                : "reply-tag reply-tag--answered"
            }
          >
            {expiredStub ? t.replies.expiredTag : t.tasks.replyAnsweredTag}
          </span>
          {card ? (
            <>
              <span className="task-reply-card__collapsed-summary">
                {card.summary}
              </span>
              {answerText && (
                <span className="task-reply-card__collapsed-answer">
                  {answerText}
                </span>
              )}
            </>
          ) : (
            <span className="task-reply-card__collapsed-summary">
              {fallbackSummary || t.tasks.expandReply}
            </span>
          )}
        </button>
      </div>
    );
  }

  return (
    <div
      className="reply-card task-reply-card"
      data-testid="task-reply-card"
      data-reply-card-id={replyCardId}
    >
      <header className="task-reply-card__head">
        <span className="task-reply-card__title">{t.tasks.replyHeader}</span>
        {card?.status === "waiting" && (
          <span className="reply-tag reply-tag--ai">{t.tasks.replyBadge}</span>
        )}
        {card && (
          <button
            type="button"
            className="task-reply-card__jump"
            onClick={jumpToChat}
          >
            {t.tasks.replyInChat}
            <ChevronRightIcon size={12} />
          </button>
        )}
        {(card?.status === "answered" || card?.status === "expired") && (
          <button
            type="button"
            className="task-reply-card__collapse"
            aria-label={t.tasks.collapseReply}
            title={t.tasks.collapseReply}
            data-testid="task-reply-card-collapse"
            onClick={() => setAnsweredOpen(false)}
          >
            <ChevronRightIcon size={12} className="reply-card__caret reply-card__caret--open" />
          </button>
        )}
      </header>

      {/* T-a20b: summary is agent-authored free text (markdown), like body. */}
      {card?.summary && (
        <Markdown source={card.summary} className="reply-card__summary doc-md" />
      )}
      {card?.body && (
        <Markdown source={card.body} className="reply-card__body doc-md" />
      )}
      {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the body,
       * on every status — click an image to preview in the lightbox. */}
      {card && (
        <ReplyCardQuestionAttachments card={card} onOpenImage={setLightboxSrc} />
      )}

      {loadError && (
        <div className="reply-card__error">{t.replies.loadError}</div>
      )}
      {actionError && <div className="reply-card__error">{actionError}</div>}

      {card &&
        (card.status === "waiting" ? (
          <ReplyCardWaitingBody card={card} onAnswer={doAnswer} />
        ) : card.status === "expired" ? (
          <ReplyCardExpiredBody card={card} />
        ) : (
          <ReplyCardAnsweredBody card={card} onReanswer={doReanswer} />
        ))}
      <Lightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
    </div>
  );
}
