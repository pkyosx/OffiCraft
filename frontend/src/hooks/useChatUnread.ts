// hooks/useChatUnread.ts — the 辦公室 nav unread signal: the owner's TOTAL
// unread chat count, kept live. Deliberately SEPARATE from useChat/useMembers:
// the badge mounts app-wide (App's nav bar) and must stay cheap — it rides the
// dedicated count endpoint and refetches on the deltas that can MOVE that total,
// without ever pulling the message list or roster. The nav renders the count as
// a badge when it is > 0 (>99 → "99+"), nothing at 0. This is a different signal
// from the 等我回覆 waiting-card badge — they never merge.

import { useEffect, useState } from "react";
import { api } from "../api";

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

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getChatUnreadCount()
        .then((n) => {
          if (alive) setCount(n);
        })
        .catch((e) => console.warn("useChatUnread: fetch failed", e));
    };

    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (OFFICE_TOTAL_TOPICS.has(topic)) refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
