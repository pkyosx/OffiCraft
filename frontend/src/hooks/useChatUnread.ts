// hooks/useChatUnread.ts — the 辦公室 nav unread signal: the owner's TOTAL
// unread chat count, kept live. Deliberately SEPARATE from useChat/useMembers:
// the badge mounts app-wide (App's nav bar) and must stay cheap — it rides the
// dedicated count endpoint and refetches on the deltas that can MOVE that total,
// without ever pulling the message list or roster. The nav renders the count as
// a badge when it is > 0 (>99 → "99+"), nothing at 0. This is a different signal
// from the 等我回覆 waiting-card badge — they never merge.

import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { createDeltaSink } from "../lib/deltaSink";
import { burstMovesNoOwnerUnread } from "../lib/ownerUnread";

// The SSE topics that can change the office total — the SINGLE source of truth
// for "what makes this badge move". The server total is Σ unread over the LIVE
// set = non-removed members ∪ live outsource workers (api_chat.go
// HandleChatUnreadCount's live[] filter). So the total moves on a new message /
// read (chat / chat_read) AND when the live SET itself changes — a member
// removed/added ("member") or a worker spawned/released ("outsource_worker").
// Missing either lifecycle topic left the parent badge stale behind the 正職/
// 外包 sub-tabs (which useMembers/useOutsourceWorkers DO subscribe to) until a
// manual reload — the bug in T-b86c. This is exported so the test asserts the
// wiring against THIS set (fail-closed: adding a topic here is one edit and the
// test picks it up), not a hand-copied list. NOTE (T-b86c residual, tracked
// separately): a NEW backend topic that changes the live set but is not added
// here would re-stale this badge silently — no test on either side goes red.
export const OFFICE_TOTAL_TOPICS = new Set([
  "chat",
  "chat_read",
  "member",
  "outsource_worker",
]);

export function useChatUnread(): number {
  const [count, setCount] = useState(0);
  // 🔴 "THE LAST REFETCH NEVER LANDED" (T-929f). Same defect, same shape as
  // useChat's load(): a count fetch that REJECTS used to be a lone console.warn
  // and nothing else, so the badge kept rendering the number from before the
  // failure with no path back to the truth. It is NOT covered by the reconnect
  // resync: that only runs when the transport itself drops (es.onopen fires
  // again). Measured: with the EventSource still open (esOpen=1, esError=0) the
  // delta arrives, this fetch fails, and the badge sits frozen for 90s+ —
  // synthesising one `focus` fixes it in 0.0s, which is the proof that nothing
  // in the hook was ever going to retry.
  //
  // The fix is deliberately the SMALLEST one that closes it: mark, don't retry.
  // No timer, no backoff, no retry loop — just a flag saying "this consumer is
  // holding a number it knows is stale", which forces the NEXT relevant burst
  // through to a fetch even when the ordinary filter would have skipped it.
  //
  // ⚠️ THE GAP THIS LEAVES OPEN, VERBATIM AND ON PURPOSE:
  // 「下一個事件來就補」意味著:如果那條線之後再也沒有任何事件,就還是不會補。
  // If no further chat / chat_read / member / outsource_worker delta ever
  // arrives on this connection, the stale count STAYS stale until something
  // else remounts or reconnects. That residual is a KNOWN, ACCEPTED trade made
  // by the owner in exchange for a smaller change (2026-08-20). Do not read the
  // flag below as "the stale-badge bug is fixed"; read it as "the badge now
  // self-heals on the next event instead of never".
  //
  // ⚠️ StrictMode: this ref is (re)initialised in the effect's SETUP body, not
  // only in cleanup. A ref that is only ever written on the way out gets stuck
  // off forever under StrictMode's setup→cleanup→setup double-invoke.
  const staleRef = useRef(false);

  useEffect(() => {
    let alive = true;
    // Setup-body write (see the StrictMode note above): a fresh subscription
    // starts by fetching unconditionally, so it owes nothing yet.
    staleRef.current = false;

    const refetch = () => {
      api
        .getChatUnreadCount()
        .then((n) => {
          if (!alive) return;
          // Landed ⇒ whatever we owed is paid off.
          staleRef.current = false;
          setCount(n);
        })
        .catch((e) => {
          // Same guard as the .then arm, and for a sharper reason: this ref
          // OUTLIVES the effect instance. A fetch belonging to a torn-down
          // instance can reject AFTER the next setup body has already cleared
          // the mark, and without this line it would write its debt onto its
          // SUCCESSOR — which under StrictMode's setup→cleanup→setup is the
          // ordinary case, not an exotic one.
          if (!alive) return;
          // Do NOT retry here. Just record the debt; the sink below pays it on
          // the next relevant burst.
          staleRef.current = true;
          console.warn("useChatUnread: fetch failed", e);
        });
    };

    refetch();
    // This total is ONE number over the whole live set, so there is no "just the
    // item that changed" variant of it — but there ARE two things to remove.
    //
    // (a) DUPLICATES: a resync fans all four of these topics at once, which used
    //     to be four identical count requests for one reconnect. One decision
    //     per burst.
    //
    // (b) 🔴 REQUESTS THAT CANNOT CHANGE THE ANSWER (T-b17f). This total is
    //     Σ `UnreadCounts(…, owner)` over the live set (api_chat.go:873), so a
    //     `chat` line NOT addressed to the owner, or a `chat_read` receipt whose
    //     READER is not the owner, cannot move it by a single unit — the server
    //     would hand back the number we already hold. Agent↔agent traffic is
    //     ordinary here, and before this line every such message cost one
    //     `GET /api/chat/unread-count`, which runs a full `ListChat()` table
    //     scan plus a members and a workers list read. See `lib/ownerUnread.ts`
    //     for why the predicate is exactly `to` / `reader`.
    //
    // The gate is deliberately narrow: it fires only when EVERY topic of ours in
    // this burst is chat/chat_read. `member` / `outsource_worker` change the
    // LIVE SET itself (a removed member drops their leftovers out of the sum),
    // so a burst carrying either still refetches whatever else it also carried.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        const mine = [...batch.topics].filter((t) =>
          OFFICE_TOTAL_TOPICS.has(t)
        );
        if (mine.length === 0) return;
        // 🔴 T-929f: the ordinary "this burst cannot move the answer" gate is
        // sound ONLY while the number we hold IS the answer. Once a fetch has
        // failed, the number we hold is not the server's — so the reasoning
        // "the server would hand back the number we already hold" no longer
        // applies, and skipping would strand the stale value. A relevant burst
        // therefore forces a fetch through the gate exactly when we owe one.
        // (`mine.length === 0` above still stands: a topic this badge does not
        // reconcile on is not a relevant event, debt or no debt.)
        if (!staleRef.current && burstMovesNoOwnerUnread(batch, mine)) return;
        refetch();
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
