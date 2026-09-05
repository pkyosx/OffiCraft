// lib/replyCardReceipt.ts — fold a reply-card WRITE answer into the card that
// is already on screen (T-91).
//
// answer / re-answer / expire act on a card somebody ELSE opened. The question,
// its options, its attachments and its task reference are unchanged by the act,
// so the write is about to stop echoing them (spec ReplyCardWriteReceiptDTO:
// what the write decides is `status`, `answer`, `answered_ts` / `expired_ts`,
// plus the task/step it released). The cockpit used to REPLACE the rendered card
// with whatever the write returned — ReplyCardBody renders `card.task.title` off
// exactly that object — so the day the echo shrinks the card would blank itself
// with no error to show for it.
//
// So the two adoption points MERGE instead of replacing: the transition comes
// from the write, everything the write does not decide stays as it was read.
// Under today's whole-card answer the two are the same value, which is why this
// change is safe to land BEFORE the server shrinks the response.
import type { ReplyCard } from "../api/adapter";

/** `before` (the card on screen, read from the server) with ONLY the transition
 * the write just performed folded in. */
export function mergeReplyCardWrite(
  before: ReplyCard,
  receipt: ReplyCard
): ReplyCard {
  return {
    ...before,
    status: receipt.status,
    answer: receipt.answer,
    answeredTs: receipt.answeredTs,
    expiredTs: receipt.expiredTs,
  };
}
