// hooks/useChatUnread.ts — the 辦公室 nav unread signal: the owner's TOTAL
// unread chat count, kept live. Deliberately SEPARATE from useChat/useMembers:
// the badge mounts app-wide (App's nav bar) and must stay cheap — it rides the
// dedicated count endpoint and refetches on every "chat" / "chat_read" SSE
// delta, without ever pulling the message list or roster. The nav renders the
// count as a badge when it is > 0 (>99 → "99+"), nothing at 0. This is a
// different signal from the 等我回覆 waiting-card badge — they never merge.

import { useEffect, useState } from "react";
import { api } from "../api";

const CHAT_TOPICS = new Set(["chat", "chat_read"]);

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
      if (CHAT_TOPICS.has(topic)) refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
