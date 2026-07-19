// hooks/useTaskCount.ts — the 任務 nav badge's open-task count, kept live.
// Mirrors useReplyCardCount one-for-one: the badge mounts app-wide (App's nav
// bar) and must stay cheap — it rides the dedicated count endpoint
// (GET /api/tasks/count) and refetches on every "task" SSE delta, without ever
// pulling the full task list. The count is the NON-TERMINAL task total (尚未
// 執行/進行中/等我回覆/等待外部; 已完成/終止 never count — spec §1).

import { useEffect, useState } from "react";
import { api } from "../api";

export function useTaskCount(): number {
  const [count, setCount] = useState(0);

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getTaskCount()
        .then((n) => {
          if (alive) setCount(n);
        })
        .catch((e) => console.warn("useTaskCount: fetch failed", e));
    };

    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
