// lib/ownerUnread.ts — the ONE predicate that answers "could this delta move
// any unread number the cockpit renders?" (T-b17f).
//
// 🔴 THE INVARIANT IT ENCODES. Every unread number in the cockpit — the roster
// card badge (`MemberDTO.unread_count`), the 外包 rail badge
// (`MemberDTO.unread_count` for kind=outsource) and the nav total
// (`GET /api/chat/unread-count`) — is the same fold: `UnreadCounts`
// (server/ocserverd/domain.go:411-425), which counts a message ONLY when
// `m.Recipient == reader`, against a watermark map filled ONLY from receipts
// whose `ReaderID == reader`. Its own doc comment spells out both halves:
// "Messages between two other participants never count, and neither do the
// reader's own sends".
//
// The cockpit's reader is always the OWNER: all four call sites pass
// `currentActor(r)` (`api_chat.go:873`, `api_outsource.go:136/199/358`,
// `api_helpers.go:322`), and `currentActor` is the verified token `sub`, which
// for an owner token IS the `wireOwnerID` literal (`api_helpers.go:38`).
//
// ⇒ For a `chat` delta, the ONLY way any of those numbers moves is
//   `to === owner`. For a `chat_read` delta, the ONLY way is `reader === owner`.
//   Everything else is a write the cockpit cannot see, and refetching for it
//   buys literally nothing — not a smaller answer, the SAME answer.
//
// ⚠️ THIS IS THE TIGHT FORM, and the two halves it tightened over the earlier
// loose version ("is the owner at EITHER end") are not symmetric noise:
//   - `chat` with `from === owner` — a message the owner SENT. The recipient is
//     the member/worker, so the owner's count for them does not move.
//   - `chat_read` with `peer === owner` — somebody read the OWNER's messages.
//     That advances THEIR watermark, and watermarks are per-reader.
// Both used to cost one wasted `GET /{id}` per delta.
//
// ⚠️ THE TWO TOPICS CARRY DIFFERENT FIELD NAMES — `chat` has from/to,
// `chat_read` has reader/peer. Checking only one pair answers "no owner here"
// for every delta of the other topic, which SKIPS REAL WORK. There is a test
// per topic; the mutant that drops the `chat_read` arm reddens them.
//
// ⚠️ Any other topic answers FALSE, and that is only safe because every caller
// applies this predicate ONLY after establishing that the topics it actually
// reconciles on in this burst are all badge-only. A `member` /
// `member` / `task` delta absolutely can change what these views
// render, and each caller keeps its own full-refetch path for those.
//
// 🔴 PER-DELTA, NOT PER-BURST. This answers a question about ONE delta. The
// held-id narrowing (`narrowToHeld`) answers a question about the whole BURST's
// id union. Mixing the two up is the trap `sseFanout.test.tsx` spends 35 lines
// on: 一陣 ≠ 一則. A caller must skip only when NO delta in the burst passes
// this, and must otherwise hand the FULL burst to its existing branches — never
// filter the burst down to the passing deltas.

import type { SseDelta } from "../api/adapter";

/** The owner's fixed wire id — the `sub` of the owner token. The same literal
 * ChatArea / ChatGalleryPanel / actorLabel / useMembers use: it is the wire's
 * value, not a new name to invent. */
export const OWNER_ID = "owner";

/** The topics whose ONLY effect on any unread number is via `UnreadCounts`. */
export const UNREAD_ONLY_TOPICS = new Set(["chat", "chat_read"]);

/**
 * Could THIS ONE delta move any unread number the owner's cockpit renders?
 *
 * The identity needed to decide is already on the delta (`toSseDelta` keeps
 * from/to/reader/peer), so this costs no request to answer.
 */
export function couldMoveOwnerUnread(d: SseDelta): boolean {
  if (d.topic === "chat") return d.names.to === OWNER_ID;
  if (d.topic === "chat_read") return d.names.reader === OWNER_ID;
  return false;
}

/**
 * Is this whole burst a no-op for the owner's unread numbers?
 *
 * True ⇒ the caller may issue ZERO requests. It requires all three of:
 *  - every topic this consumer reconciles on in this burst is unread-only
 *    (the caller supplies that list — only it knows its own topic set),
 *  - nothing in the burst was unnamed (an unnamed delta is the honest "you may
 *    have missed anything" and can never be answered by reasoning about names),
 *  - and NO delta in the burst could move an owner unread number.
 *
 * The `deltas.length > 0` guard is fail-safe, not decoration: an empty list
 * would make `.some()` false and skip. Today that is unreachable (a batch with
 * no delta object sets `unnamed`), but the failure mode would be a silently
 * stale view, so it is spelled out.
 */
export function burstMovesNoOwnerUnread(
  batch: { deltas: SseDelta[]; unnamed: boolean },
  relevantTopics: string[]
): boolean {
  if (batch.unnamed) return false;
  if (relevantTopics.length === 0) return false;
  if (!relevantTopics.every((t) => UNREAD_ONLY_TOPICS.has(t))) return false;
  return batch.deltas.length > 0 && !batch.deltas.some(couldMoveOwnerUnread);
}
