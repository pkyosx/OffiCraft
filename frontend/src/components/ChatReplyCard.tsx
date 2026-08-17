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
import { Markdown } from "./Markdown";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardQuestionAttachments,
  ReplyCardTaskRef,
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

  // 🔴 READ GENERATION. Every read of this card takes a ticket and only commits
  // if the ticket is still current; adopting a write takes one too (`adopt`
  // below). Without it, a read that was ALREADY IN FLIGHT when the owner
  // answered — a peer's `reply_card` delta a moment before the stream died is
  // exactly how one gets there, and this card fetches on every such delta while
  // it is still waiting — resolves with a PRE-WRITE card and puts the option
  // chips back. Trigger and consequence are identical to the blocker this file
  // already fixed once: the server has the answer, the card says otherwise, and
  // with the stream down nothing comes to correct it.
  //
  // ⚠️ Dropping a superseded read is the WHOLE fix here, and that is a property
  // of this surface, not a general licence: this read carries ONE card — the very
  // card the newer commit already describes — so nothing else is lost with it.
  // The 等我回覆 pane cannot do this (its snapshot carries OTHER cards, including
  // ones a peer just opened), which is why useReplyCards holds ids instead.
  const readGenRef = useRef(0);

  /** Take the newest known truth for this card: a fresh read, or the card a
   * write of ours just returned. Both invalidate every read still in flight. */
  const commitCard = useCallback((fresh: ReplyCard) => {
    ++readGenRef.current;
    statusRef.current = fresh.status;
    setCard(fresh);
    setLoadError(false);
  }, []);

  const refetch = useCallback(async () => {
    const gen = ++readGenRef.current;
    const fresh = await api.getReplyCard(replyCardId);
    // Superseded while we were in flight (a newer read, or our own write's
    // adopted response) → drop this now-stale card rather than un-answer it.
    if (gen !== readGenRef.current) return;
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

  // 🔴 One owner action = one round of refetching (T-a3e4 step 8), and the two
  // actions are NOT symmetric. Do not "tidy" them into the same shape — the
  // asymmetry is forced by the T-cdf4 guard in the SSE effect above:
  //
  //   answer  — this card is still WAITING, so the guard lets the reply_card
  //             delta through, and that delta ALSO arrives for the owner's own
  //             write (http: publishReplyCard fires after the row commits and
  //             before the response flushes; mock: emitTopic is called
  //             synchronously inside answerReplyCard). So the delta refetches
  //             this card on its own — an action-path refetch here would be a
  //             SECOND full GET whose result is downloaded and thrown away, and
  //             NOTHING on screen would show it. It ADOPTS the write's own
  //             response instead (zero requests), so the flip does not depend on
  //             that delta arriving.
  //   reanswer— this card is already ANSWERED, and the guard deliberately drops
  //             the delta for terminal cards (that is what stops 70+ historical
  //             cards each refetching on one unrelated answer). So the SSE path
  //             will NOT fire, and this refetch is the ONLY thing that updates
  //             the card. Removing it leaves 重新決定 showing the OLD answer:
  //             ReplyCardAnsweredBody closes edit mode as soon as onReanswer
  //             resolves, so the stale value is what the owner is left looking
  //             at. It stays.
  //
  // ⚠️ The old cost of dropping the answer-path refetch — "with the SSE stream
  // down the card no longer flips in place" — no longer applies: doAnswer adopts
  // the write's own response (via commitCard). What stayed dropped is the second
  // GET, which is all step 8 was ever about. refresh() remains the unconditional
  // path for a 409, where somebody ELSE's write means no delta of ours is coming.
  //
  // 🔴 STATE THE CONDITION, DO NOT GENERALISE. The previous version of this note
  // said the flip is "a property of the write, not of the stream" — full stop —
  // and that was FALSE while `refetch` had no generation guard: a read left in
  // flight by the stream's last frame landed afterwards and put these chips back.
  // The flip holds because of TWO things together: (a) doAnswer adopts the write's
  // response, and (b) that adoption invalidates every read still in flight
  // (readGenRef). Remove either and the claim is false again — and it is only a
  // claim about THIS card in THIS component; the 等我回覆 panes need their own
  // mechanism (useReplyCards' per-id holds), which is why they have one.
  async function doAnswer(input: ReplyCardAnswerInput) {
    try {
      // ADOPT-FROM-RESPONSE, not a second GET: the write already answers with
      // the fresh card, so this card flips in place even with the stream down —
      // and it still costs ZERO extra requests, so the one-round budget above is
      // untouched (the delta's refetch remains the only round).
      commitCard(await api.answerReplyCard(replyCardId, input));
      setActionError(null);
    } catch (e) {
      console.warn("ChatReplyCard: answer failed", e);
      setActionError(t.replies.answerError);
      throw e;
    }
  }

  async function doReanswer(input: ReplyCardAnswerInput) {
    try {
      // Adopt first (so a read in flight can no longer un-revise this card),
      // then still refetch — see the asymmetry note above: the SSE path does not
      // fire for a terminal card, and the one-round budget is spent HERE.
      commitCard(await api.reanswerReplyCard(replyCardId, input));
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
      {/* §3.6 請示 → 任務 (chat surface): the SAME row RepliesPage renders —
       * one component, so TITLE and 查看任務詳情 can never drift between the two
       * surfaces. Only the route is ours. It leads the card (owner 2026-08-14,
       * T-ee17:「這個不能夠放到最一開始嗎？」) — WHICH piece of work this asks
       * about should not require reading the whole card first. Both surfaces
       * moved together; a lead row on one and a trailing row on the other is
       * exactly the drift this shared component exists to prevent. */}
      {card?.task && (
        <ReplyCardTaskRef
          task={card.task}
          onJump={() => setRoute({ page: "tasks", taskId: card.task!.id })}
        />
      )}

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
        <ReplyCardQuestionAttachments card={card} />
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
    </div>
  );
}
