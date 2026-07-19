// hooks/useTaskManuals.ts — 設定 › 任務手冊 (SPEC §5): the full manuals list
// + owner CRUD. Reconcile-by-refetch on the "task_manual" SSE topic (an agent
// write-back of 學習經驗 on task close surfaces live); every owner mutation
// ALSO refetches directly so the mock behaves identically.
//
// Error split mirrors the other resource hooks: `error` is the honest
// load-failure flag (never render a dead fetch as "no manuals"); mutation
// rejections PROPAGATE to the caller — the pages surface them inline
// (duplicate 409 on create, open-tasks 409 on delete, 422s).

import { useCallback, useEffect, useState } from "react";
import type { TaskManualView, TaskManualPatch } from "../api/adapter";
import { api } from "../api";

interface UseTaskManuals {
  manuals: TaskManualView[];
  loading: boolean;
  error: boolean;
  /** Create by DISPLAY NAME (T-fa76) — the server mints the tm- type_key. */
  create: (displayName: string) => Promise<TaskManualView>;
  update: (typeKey: string, patch: TaskManualPatch) => Promise<TaskManualView>;
  remove: (typeKey: string) => Promise<void>;
}

export function useTaskManuals(): UseTaskManuals {
  const [manuals, setManuals] = useState<TaskManualView[]>([]);
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
      const next = await api.updateTaskManual(typeKey, patch);
      await refetch();
      return next;
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

  return { manuals, loading, error, create, update, remove };
}
