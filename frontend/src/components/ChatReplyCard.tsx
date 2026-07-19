// ChatReplyCard — a reply card (等我回覆卡) rendered INLINE in the chat thread
// (SPEC §3, B3 聊天整合). The carrying message only holds `replyCardId`
// (meta.reply_card_id), so this component refetches the SINGLE card for its
// full shape (options / status / answer), and again on every `reply_card` SSE
// delta — that refetch IS the two-way sync: answering on the 等我回覆 page (or
// another window) flips this card to answered in place, and answering here
// fans the same topic so the page's lists + nav badge update.
//
// The card interiors are the SHARED ReplyCardBody blocks (same chips / tags /
// composer / 重新決定 flow as RepliesPage — one implementation, zero drift).
// No extra banner: the card IS the message bubble (spec: 直接出現在訊息串中).
// Answering never touches the chat unread red dot — that clears only by being
// IN the conversation (the existing listChat watermark), which is exactly
// where this card lives.

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

export function ChatReplyCard({
  replyCardId,
  fallbackSummary,
  initialStatus,
}: {
  replyCardId: string;
  /** The carrying message's body (the server posts the card's summary as the
   * message text) — shown while the card is still loading / if the card fetch
   * fails, so the ask is never a blank bubble; also the collapsed-stub label
   * for a not-yet-expanded answered card (no card fetched yet). */
  fallbackSummary: string;
  /** The carrying message's read-time `reply_card_status` hint. When it says
   * ANSWERED or EXPIRED (both terminal) the card mounts COLLAPSED and does NOT
   * fetch — owner rule 已回覆卡預設不載: the full card loads only when the
   * owner expands it, so a chat history of dozens of settled cards no longer
   * fires one getReplyCard each. A waiting hint (or null/undefined — unknown)
   * loads eagerly, exactly as before this prop existed. */
  initialStatus?: ReplyCard["status"] | null;
}) {
  const { t } = useI18n();
  const [, setRoute] = useHashRoute();
  const [card, setCard] = useState<ReplyCard | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // A question attachment opened full-size (null = closed) — the shared
  // Lightbox below.
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  // Lazy-load gate: a terminal-hinted card (answered/expired) starts COLLAPSED
  // (no fetch) and loads its full shape only on expand; every other case
  // starts expanded and loads eagerly.
  const lazyTerminal =
    initialStatus === "answered" || initialStatus === "expired";
  const [expanded, setExpanded] = useState(!lazyTerminal);
  // Latest card status, read inside the SSE callback WITHOUT re-subscribing.
  // SEEDED from the hint so a collapsed terminal card (not yet fetched) also
  // satisfies the T-cdf4 guard below — an unrelated reply_card fan-out must NOT
  // wake it into a fetch, or lazy-load is defeated on the first SSE delta.
  const statusRef = useRef<ReplyCard["status"] | null>(
    lazyTerminal ? initialStatus : null
  );

  const refetch = useCallback(async () => {
    const fresh = await api.getReplyCard(replyCardId);
    statusRef.current = fresh.status;
    setCard(fresh);
    setLoadError(false);
  }, [replyCardId]);

  // Load the card once expanded — eager on mount for a waiting/unknown card,
  // deferred to the expand click for an answered one.
  useEffect(() => {
    if (!expanded) return;
    let alive = true;
    refetch().catch((e) => {
      console.warn("ChatReplyCard: card load failed", e);
      if (alive) setLoadError(true);
    });
    return () => {
      alive = false;
    };
  }, [expanded, refetch]);

  useEffect(() => {
    // Reconcile-by-refetch (contract B): a reply_card delta — an answer from
    // the 等我回覆 page or another window — re-pulls THIS card so a still-waiting
    // card flips to answered in place. But the reply_card topic is NOT
    // per-card: any card being opened/answered fans it to every mounted card.
    // Once THIS card is answered or expired (both terminal) the only thing
    // that changes it is a local 重新決定 (answered only), which refetches
    // itself (doReanswer) — so an already-settled card (incl. a collapsed,
    // never-fetched one via the seeded statusRef) ignores the SSE delta and
    // stops the broadcast storm (70+ historical cards no longer each refetch
    // on one unrelated answer).
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "reply_card") return;
      if (statusRef.current === "answered" || statusRef.current === "expired")
        return;
      refetch().catch((e) =>
        console.warn("ChatReplyCard: SSE refetch failed", e)
      );
    });
    return unsubscribe;
  }, [refetch]);

  async function doAnswer(input: ReplyCardAnswerInput) {
    try {
      await api.answerReplyCard(replyCardId, input);
      setActionError(null);
      await refetch();
    } catch (e) {
      console.warn("ChatReplyCard: answer failed", e);
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
      console.warn("ChatReplyCard: re-answer failed", e);
      setActionError(t.replies.answerError);
      throw e;
    }
  }

  // Collapsed stub for a not-yet-expanded terminal card (owner 已回覆卡預設
  // 不載): the ask's summary (the carrying message body — no card fetched yet)
  // + the 已回覆/已過期 tag on one clickable row; expanding fetches the full
  // card and renders the shared terminal interior below.
  if (!expanded) {
    const expiredStub = statusRef.current === "expired";
    return (
      <div
        className="reply-card reply-card--chat reply-card--collapsed"
        data-testid="chat-reply-card"
        data-reply-card-id={replyCardId}
      >
        <button
          type="button"
          className="reply-card__collapsed-row"
          aria-expanded={false}
          aria-label={t.tasks.expandReply}
          title={t.tasks.expandReply}
          data-testid="chat-reply-card-expand"
          onClick={() => setExpanded(true)}
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
          <span className="reply-card__collapsed-summary">
            {fallbackSummary}
          </span>
        </button>
      </div>
    );
  }

  return (
    <div
      className="reply-card reply-card--chat"
      data-testid="chat-reply-card"
      data-reply-card-id={replyCardId}
    >
      {/* T-a20b: summary is agent-authored free text (markdown), like body. */}
      <Markdown
        source={card?.summary || fallbackSummary}
        className="reply-card__summary doc-md"
      />
      {card?.body && (
        <Markdown source={card.body} className="reply-card__body doc-md" />
      )}
      {/* QUESTION-side attachments (T-5e8a): thumbnails/chips under the body,
       * on every status — click an image to preview in the lightbox. */}
      {card && (
        <ReplyCardQuestionAttachments card={card} onOpenImage={setLightboxSrc} />
      )}

      {/* §3.6 請示 → 任務 (chat surface, same rule as RepliesPage): a
       * TASK-derived ask shows the 精簡任務資訊 row — TYPE + 查看任務詳情
       * (never the task number/識別鍵); a pure chat ask shows nothing. */}
      {card?.task && (
        <div className="reply-card__task" data-testid="reply-task-ref">
          <span className="reply-card__task-type">
            <span className="reply-card__task-label">
              {t.replies.taskBadge}
            </span>
            {card.task.typeKey || t.tasks.adhoc}
          </span>
          <button
            type="button"
            className="reply-card__task-jump"
            data-testid="reply-task-jump"
            onClick={() => setRoute({ page: "tasks", taskId: card.task!.id })}
          >
            {t.replies.viewTask}
            <ChevronRightIcon size={12} />
          </button>
        </div>
      )}

      {loadError && (
        <div className="reply-card__error">{t.replies.loadError}</div>
      )}
      {actionError && (
        <div className="reply-card__error">{actionError}</div>
      )}

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
