// ChatReplyCard — a reply card (等我回覆卡) rendered INLINE in the chat thread
// (SPEC §3, B3 聊天整合). The carrying message only holds `replyCardId`
// (meta.reply_card_id), so this component fetches the SINGLE card for its full
// shape (options / status / answer). Two things have since carved exceptions
// out of that "always fetches on mount", and both are load-bearing:
//   · EVERY card mounts COLLAPSED and fetches only on expand (owner
//     2026-09-04 `c-6f054c1cb481`: 待回覆的卡也跟已回覆一樣先不展開). The stub
//     needs nothing this component has to go and get — the summary is the
//     carrying message's own body and the status is its hint — so the row's
//     first painted frame is already its final height, for every card, and a
//     chat history of dozens of cards fires zero `getReplyCard`s.
// Every `reply_card` SSE delta still refetches a non-terminal card, and that
// refetch IS the two-way sync: answering on the 等我回覆 page (or another
// window) flips this card to answered in place, and answering here fans the
// same topic so the page's lists + nav badge update.
//
// The card interiors are the SHARED ReplyCardBody blocks (same chips / tags /
// composer / 重新決定 flow as RepliesPage — one implementation, zero drift).
// No extra banner: the card IS the message bubble (spec: 直接出現在訊息串中).
// Answering never touches the chat unread red dot — that clears only by being
// IN the conversation (ChatArea's explicit mark-read, which fires while the
// owner is actually looking at the thread), which is exactly where this card
// lives. Until T-48 the clearer was a side effect of the listing itself; the
// dot's rule did not change when the mechanism did.

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type {
  ReplyCard,
  ReplyCardWriteReceipt,
  ReplyCardAnswerInput,
} from "../api/adapter";
import { api } from "../api";
import { useHashRoute } from "../lib/hashRoute";
import { mergeReplyCardWrite } from "../lib/replyCardReceipt";
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
  /** The carrying message's read-time `reply_card_status` hint — what the
   * collapsed stub labels itself with (待回覆 / 已回覆 / 已過期) before any card
   * has been fetched. Null/undefined (unknown) is treated as 待回覆; the first
   * expand replaces it with the card's own status. */
  initialStatus?: ReplyCard["status"] | null;
}) {
  const { t } = useI18n();
  const [, setRoute] = useHashRoute();
  const [card, setCard] = useState<ReplyCard | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  // 🔴 EVERY CARD MOUNTS COLLAPSED, AND THAT IS WHAT REMOVED THE SHIFT (T-48,
  // owner 2026-09-04). It used to be only the terminal-hinted ones: a waiting
  // card mounted EXPANDED and empty, painted at its collapsed height, fetched,
  // and then GREW — pushing everything below it down after the scroll had
  // already landed (measured +254px at 1280 wide, +200px at 390). The fix used
  // to be a prefill cache the thread had to await before any message could
  // reach the view; collapsing the row instead makes the first painted frame
  // its final height WITHOUT fetching anything, so the cache, the await and the
  // module that enforced the await are all gone.
  const [expanded, setExpanded] = useState(false);
  // Latest known status, read inside the SSE callback WITHOUT re-subscribing.
  // SEEDED from the message hint so a collapsed, never-fetched TERMINAL card
  // satisfies the T-cdf4 guard below — an unrelated reply_card fan-out must NOT
  // wake it into a fetch, or lazy-load is defeated on the first SSE delta. A
  // collapsed WAITING card deliberately does not carry that immunity: it still
  // refetches on a delta, which is how its stub flips 待回覆 → 已回覆 in place
  // when the card is answered from another surface.
  const statusRef = useRef<ReplyCard["status"] | null>(initialStatus ?? null);

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

  /** Fold OUR OWN write's answer into the card on screen — same generation
   * bump, but a MERGE rather than a replacement (T-91).
   *
   * 🔴 The write is about to stop echoing the card. answer/re-answer/expire act
   * on a card somebody else opened; the question, its options, its attachments
   * and its task ref are not what these writes decide, so the receipt drops
   * them — and this component RENDERS them (ReplyCardBody reads
   * `card.task.title`). Replacing the card with the write's answer would blank
   * that the day the receipt lands, with nothing thrown and nothing on screen
   * saying so. So only the transition is taken from the write; the rest stays
   * as this card read it. Under today's whole-card answer the two are the same
   * value, which is why this lands safely BEFORE the server change. */
  const mergeWrite = useCallback((receipt: ReplyCardWriteReceipt) => {
    ++readGenRef.current;
    statusRef.current = receipt.status;
    // No card on screen ⇒ NOTHING TO MERGE ONTO, and the receipt is not a card:
    // it carries no question, no options, no attachments and no task ref, so
    // storing it would paint a blank card. Keep null and let the read that is
    // already on its way (this component always mounts one) supply the card.
    // Unreachable in practice — the buttons that call this only exist once the
    // card has rendered — which is exactly why it must not fabricate.
    setCard((prev) => (prev ? mergeReplyCardWrite(prev, receipt) : prev));
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

  useEffect(() => {
    // The card is fetched on EXPAND and only on expand — a collapsed row shows
    // what the carrying message already said, so there is nothing to load until
    // somebody opens it.
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
  // the write's own response (via mergeWrite). What stayed dropped is the second
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
      mergeWrite(await api.answerReplyCard(replyCardId, input));
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
      mergeWrite(await api.reanswerReplyCard(replyCardId, input));
      setActionError(null);
      await refetch();
    } catch (e) {
      console.warn("ChatReplyCard: re-answer failed", e);
      setActionError(t.replies.answerError);
      throw e;
    }
  }

  // Collapsed stub — what EVERY card shows until it is opened (owner
  // 2026-09-04): the ask's summary (the carrying message's own body) + a
  // 待回覆／已回覆／已過期 tag on one clickable row. Nothing here is fetched, so
  // the row is its final height on the first frame. A waiting card that gets
  // answered elsewhere refetches on the SSE delta, which is why the tag reads
  // `card` first and the message hint only until then.
  if (!expanded) {
    const stubStatus = card?.status ?? initialStatus ?? "waiting";
    const stubTag =
      stubStatus === "expired"
        ? { cls: "reply-tag reply-tag--expired", label: t.replies.expiredTag }
        : stubStatus === "answered"
          ? {
              cls: "reply-tag reply-tag--answered",
              label: t.tasks.replyAnsweredTag,
            }
          : { cls: "reply-tag reply-tag--waiting", label: t.tasks.replyWaitingTag };
    return (
      <div
        className="reply-card reply-card--chat reply-card--collapsed"
        data-testid="chat-reply-card"
        data-reply-card-id={replyCardId}
        data-reply-card-status={stubStatus}
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
          <span className={stubTag.cls}>{stubTag.label}</span>
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
