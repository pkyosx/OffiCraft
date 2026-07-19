// hooks/useReplyCardCount.ts — the nav badge's waiting-card count, kept live.
// Deliberately SEPARATE from useReplyCards: the badge mounts app-wide (App's
// nav bar) and must stay cheap — it rides the dedicated count endpoint and
// refetches on every "reply_card" SSE delta, without ever pulling the card
// lists. Answered cards never count (spec: the badge counts 待回覆 only);
// the chat unread red dot is a different signal with its own clearing rule —
// the two never merge.

import { useEffect, useState } from "react";
import { api } from "../api";

export function useReplyCardCount(): number {
  const [count, setCount] = useState(0);

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getReplyCardCount()
        .then((c) => {
          if (alive) setCount(c.waiting);
        })
        .catch((e) => console.warn("useReplyCardCount: fetch failed", e));
    };

    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "reply_card") refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
