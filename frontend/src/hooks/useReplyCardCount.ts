// hooks/useReplyCardCount.ts — the nav badge's waiting-card count.
//
// T-e862 同源化: this badge used to ride its OWN count-endpoint fetch on its
// OWN SSE subscription, DELIBERATELY separate from the page's list. That
// separation was exactly the crack behind "badge 2 / list 1": two independent
// state paths landing on two different snapshots from two different instants.
// The badge is now literally the length of the SAME authoritative waiting
// array the 等我回覆 page renders (owned by <ReplyCardsProvider>, mounted
// app-wide above the nav bar) — one source, one subscription, so the badge and
// the list can never disagree. Answered cards never count (spec: the badge
// counts 待回覆 only); the chat unread red dot is a different signal with its
// own clearing rule — the two never merge.

import { useReplyCardWaitingCount } from "./useReplyCards";

export function useReplyCardCount(): number {
  return useReplyCardWaitingCount();
}
