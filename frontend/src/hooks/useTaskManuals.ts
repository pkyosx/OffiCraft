// hooks/useTaskManuals.ts — 設定 › 任務手冊 (SPEC §5): the full manuals list
// + owner CRUD. Reconcile-by-refetch on the "task_manual" SSE topic (an agent
// write-back of 學習經驗 on task close surfaces live); every owner mutation
// ALSO refetches directly so the mock behaves identically.
//
// 🔴 T-91: an owner mutation is a WRITE FOLLOWED BY A RE-READ, and nothing
// renders off the write's own answer any more. `update` used to hand its echo
// back so the 任務定義 / 學習經驗 sub-page could adopt it; the update receipt
// reports only the sizes of the documents that call actually wrote, so adopting
// it would blank a 16,000-character SOP the moment somebody saved it.
//
// Error split mirrors the other resource hooks: `error` is the honest
// load-failure flag (never render a dead fetch as "no manuals"); mutation
// rejections PROPAGATE to the caller — the pages surface them inline
// (duplicate 409 on create, open-tasks 409 on delete, 422s).

// T-1170: `GET /api/task-manuals` answers a DIRECTORY — `sop_md` and
// `learnings` are not on it, only their sizes. So `manuals` is
// `TaskManualSummaryView[]`, a type that cannot carry either document, and the
// two sub-pages that render them read their manual through `useTaskManual`
// below. Everything else (the list page, the hub's 用途/欄位/負責成員 cards, the
// crumb labels, the delete confirm) is served by the directory unchanged.

import { useCallback, useEffect, useState } from "react";
import type {
  TaskManualSummaryView,
  TaskManualView,
  TaskManualPatch,
} from "../api/adapter";
import { api } from "../api";

interface UseTaskManuals {
  manuals: TaskManualSummaryView[];
  loading: boolean;
  error: boolean;
  /** Re-read the list — used by 版本紀錄 restore (T-7d33), whose write lands
   * server-side and must not be assumed into local state. */
  refetch: () => Promise<void>;
  /** Create by DISPLAY NAME (T-fa76) — the server mints the tm- type_key. */
  create: (displayName: string) => Promise<TaskManualView>;
  /** Partial update. RE-READS the list; the sub-page that renders the SOP or
   * the 學習經驗 re-reads its own manual (useTaskManual.refetch). Returns
   * nothing on purpose (T-91): the update receipt reports only the documents
   * THIS call wrote, as sizes — never their text. */
  update: (typeKey: string, patch: TaskManualPatch) => Promise<void>;
  remove: (typeKey: string) => Promise<void>;
}

export function useTaskManuals(): UseTaskManuals {
  const [manuals, setManuals] = useState<TaskManualSummaryView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setManuals(await api.listTaskManuals());
    setError(false);
  }, []);

  useEffect(() => {
    let alive = true;

    refetch()
      .catch((e) => {
        console.warn("useTaskManuals: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "task_manual") return;
      refetch().catch((e) =>
        console.warn("useTaskManuals: SSE refetch failed", e)
      );
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch]);

  const create = useCallback(
    async (displayName: string) => {
      const created = await api.createTaskManual(displayName);
      await refetch();
      return created;
    },
    [refetch]
  );

  const update = useCallback(
    async (typeKey: string, patch: TaskManualPatch) => {
      await api.updateTaskManual(typeKey, patch);
      await refetch();
    },
    [refetch]
  );

  const remove = useCallback(
    async (typeKey: string) => {
      await api.deleteTaskManual(typeKey);
      await refetch();
    },
    [refetch]
  );

  return { manuals, loading, error, refetch, create, update, remove };
}

interface UseTaskManual {
  /** The manual in full, or `null` while it has not been read (loading,
   * failed, or an unknown type_key). Never a fabricated blank manual: an
   * editor seeded from one would let 完成編輯 write an empty SOP over a real
   * one. */
  manual: TaskManualView | null;
  loading: boolean;
  /** The read REJECTED — a state the list could not have reported, since the
   * list row can be present and this read still fail. */
  error: boolean;
  refetch: () => Promise<void>;
}

/**
 * ONE task manual in full (`GET /api/task-manuals/{type_key}`) — what the
 * 任務定義 and 學習經驗 sub-pages read since T-1170 took `sop_md` / `learnings`
 * off the list answer.
 *
 * Reconciles on the same "task_manual" topic the list listens on, so an
 * agent's 學習經驗 write-back on task close, a 版本紀錄 restore and another
 * tab's edit all land here — that live write-back is exactly why this page
 * cannot be a one-shot fetch.
 *
 * `typeKey` is `""` for "no manual on screen": nothing is requested and nothing
 * is subscribed.
 */
export function useTaskManual(typeKey: string): UseTaskManual {
  const [manual, setManual] = useState<TaskManualView | null>(null);
  const [loading, setLoading] = useState(typeKey !== "");
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    if (typeKey === "") return;
    setManual(await api.getTaskManual(typeKey));
    setError(false);
  }, [typeKey]);

  useEffect(() => {
    if (typeKey === "") {
      setManual(null);
      setLoading(false);
      setError(false);
      return;
    }
    let alive = true;
    // Drop the previous manual's documents at once — otherwise switching types
    // shows the previous SOP under the new heading until the fetch lands.
    setManual(null);
    setLoading(true);

    const load = (onFail: (e: unknown) => void) =>
      api
        .getTaskManual(typeKey)
        .then((next) => {
          if (alive) {
            setManual(next);
            setError(false);
          }
        })
        .catch(onFail);

    load((e) => {
      console.warn("useTaskManual: initial load failed", e);
      if (alive) setError(true);
    }).finally(() => {
      if (alive) setLoading(false);
    });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "task_manual") return;
      void load((e) => console.warn("useTaskManual: SSE refetch failed", e));
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [typeKey]);

  return { manual, loading, error, refetch };
}
