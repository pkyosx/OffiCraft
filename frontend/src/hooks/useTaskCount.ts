// hooks/useTaskCount.ts — the 任務 nav badge's open-task count, kept live, plus
// the unfiltered task TOTAL (T-a3e4).
// Mirrors useReplyCardCount one-for-one: the badge mounts app-wide (App's nav
// bar) and must stay cheap — it rides the dedicated count endpoint
// (GET /api/tasks/count) and refetches on every "task" SSE delta, without ever
// pulling the full task list. `open` is the NON-TERMINAL total (尚未執行/進行中/
// 等我回覆/等待外部; 已完成/終止 never count — spec §1).
//
// `total` (every task, terminal included) exists for the 任務頁's empty state:
// its list fetch asks for the ticked STATUS SET, so an empty list cannot by
// itself justify 「目前沒有任務」 — that is a claim about the whole workshop.
// This is the cheap way to know it, and it is deliberately not a list fetch:
// widening the list just to word a screen is the thing T-a3e4 removed.
//
// `overdue` / `dueSoon` also change with no task event, just because time
// passes: the server says when (`dueNextChangeTs`) and a timer refetches then.


import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { TaskCountView } from "../api/adapter";

// setTimeout fires immediately for delays above 2^31-1 ms (~24.8 days).
const MAX_TIMEOUT_MS = 2_147_483_647;
// A client clock ahead of the server's sees the change moment as already past
// while the server still answers the same moment; without a floor that is a
// refetch loop.
const MIN_TIMEOUT_MS = 1_000;

const EMPTY: TaskCountView = {
  open: 0,
  total: 0,
  overdue: 0,
  dueSoon: 0,
  dueNextChangeTs: null,
};

export function useTaskCount(): TaskCountView {
  const [counts, setCounts] = useState<TaskCountView>(EMPTY);
  const refetchRef = useRef<() => void>(() => {});

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getTaskCount()
        .then((c) => {
          if (alive) setCounts(c);
        })
        .catch((e) => console.warn("useTaskCount: fetch failed", e));
    };
    refetchRef.current = refetch;

    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") refetch();
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  // Keyed on the whole answer, not just the timestamp: a clamped timer that
  // fires early gets the same timestamp back and must arm again.
  useEffect(() => {
    if (counts.dueNextChangeTs == null) return;
    const delay = Math.min(
      Math.max(MIN_TIMEOUT_MS, counts.dueNextChangeTs * 1000 - Date.now()),
      MAX_TIMEOUT_MS
    );
    const timer = setTimeout(() => refetchRef.current(), delay);
    return () => clearTimeout(timer);
  }, [counts]);

  return counts;
}
