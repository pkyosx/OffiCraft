// hooks/useOutsourceWorkers.ts — the office 外包 panel's data (SPEC §4): the
// LIVE outsource-worker roster (codename · 任務狀態 + 任務標題 ride the worker
// DTO), ordered 依任務建立時間新→舊, plus the global parallel cap
// (settings.outsource_max_parallel) behind the panel's 「N / 上限」 + 齒輪.
//
// Reconcile-by-refetch (contract B), split by cost (T-ec2c):
//   * "outsource_worker" (assignment / release) and "task" (the bound task's
//     status/title echo + the created_ts sort key) re-pull the FULL join —
//     workers + the unfiltered GET /api/tasks + the light task-type list — the
//     way the ordering/label join needs. These deltas are infrequent.
//   * "chat" / "chat_read" only change the row's unread badge (wire
//     unread_count). They re-pull JUST the small workers list and re-join it
//     against the CACHED tasks/types — never re-downloading the task list or
//     the manuals. A company chat line used to re-download the whole tasks +
//     task-manuals payload here on every message; now it does not.
//
// The ordering join reads the same unfiltered GET /api/tasks the tasks page
// uses — a worker whose task cannot be resolved falls back to its own mint
// stamp (honest proxy, never fabricated).
//
// The cap knob has no SSE topic — the PATCH echo is adopted directly (the same
// server-confirmed-values rule as useServerSettings).

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  OutsourceWorkerView,
  TaskView,
  TaskTypeView,
} from "../api/adapter";
import { api } from "../api";

interface UseOutsourceWorkers {
  /** LIVE workers, sorted by the bound task's created_ts DESC (新→舊). */
  workers: OutsourceWorkerView[];
  /** True until the first mount fetch settles (parity with useMembers). A
   * caller resolving a chat peer from a worker id must wait for this: an
   * `ow-` chatId that is simply not-yet-loaded is NOT a released worker, and
   * treating it as one would flash the released-peer identity before the live
   * list arrives. */
  loading: boolean;
  /** True when the mount fetch REJECTED — a failed load must never read as
   * "no outsource workers". */
  error: boolean;
  /** The global cap (0 ⇒ assignment paused); null until the settings load
   * (or when it failed) — the panel then omits the cap display honestly. */
  maxParallel: number | null;
  /** PATCH outsource_max_parallel (0..20); adopts the server echo. Rejects
   * (422/network) propagate to the caller for inline error surfacing. */
  saveMaxParallel: (n: number) => Promise<void>;
}

// joinWorkers folds the sort + label join (bound task's created_ts / T-xxxx /
// 識別鍵 / type) over a worker list against the given task/type snapshots. Pure
// — the same result whether the tasks/types came from a fresh pull (full
// refetch) or the cache (chat-only refetch).
function joinWorkers(
  workers: OutsourceWorkerView[],
  tasks: TaskView[],
  types: TaskTypeView[]
): OutsourceWorkerView[] {
  const byId = new Map(tasks.map((t) => [t.id, t]));
  // type_key → display name (T-fa76): the row shows the manual's human
  // label; a deleted manual honestly falls back to the raw key in the UI.
  const typeNames = new Map(types.map((x) => [x.typeKey, x.displayName]));
  const sortKey = (x: OutsourceWorkerView) =>
    byId.get(x.taskId)?.createdTs ?? x.createdTs ?? 0;
  return [...workers]
    .sort((a, b) => sortKey(b) - sortKey(a))
    // The panel row shows the bound task's T-xxxx + 識別鍵 chips (owner
    // ruling 2026-07-13) — joined here from the same task list the sort
    // already reads; honest "" when the task cannot be resolved.
    .map((x) => ({
      ...x,
      taskNo: byId.get(x.taskId)?.taskNo ?? "",
      dedupeKey: byId.get(x.taskId)?.dedupeKey ?? "",
      // The row's second line is the bound task's TYPE (外包沒有角色名,
      // task type 就是它的「角色」— owner report 2026-07-14); "" = ad-hoc.
      taskTypeKey: byId.get(x.taskId)?.typeKey ?? "",
      taskTypeName: typeNames.get(byId.get(x.taskId)?.typeKey ?? "") ?? "",
    }));
}

export function useOutsourceWorkers(): UseOutsourceWorkers {
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [maxParallel, setMaxParallel] = useState<number | null>(null);

  // The last task/type snapshots the label+sort join reads. Held in refs (not
  // state) so the chat-only refetch can re-join against them WITHOUT re-pulling
  // them and without re-running the effect. A chat line changes neither.
  const tasksRef = useRef<TaskView[]>([]);
  const typesRef = useRef<TaskTypeView[]>([]);

  // FULL refetch: workers + the unfiltered task list + the light type list.
  // Refreshes the join-data cache. Used on mount and on task/worker deltas.
  const refetch = useCallback(async () => {
    const [w, tasks, types] = await Promise.all([
      api.listOutsourceWorkers(),
      api.listTasks(),
      api.listTaskTypes(),
    ]);
    tasksRef.current = tasks;
    typesRef.current = types;
    setWorkers(joinWorkers(w, tasks, types));
    setError(false);
  }, []);

  // CHAT-only refetch: JUST the small workers list (its unread_count), re-joined
  // against the cached tasks/types — no task-list / manuals re-download (T-ec2c).
  const refetchWorkersOnly = useCallback(async () => {
    const w = await api.listOutsourceWorkers();
    setWorkers(joinWorkers(w, tasksRef.current, typesRef.current));
    setError(false);
  }, []);

  useEffect(() => {
    let alive = true;

    refetch()
      .catch((e) => {
        console.warn("useOutsourceWorkers: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    api
      .getServerSettings()
      .then((s) => {
        if (alive) setMaxParallel(s.outsourceMaxParallel);
      })
      .catch((e) =>
        console.warn("useOutsourceWorkers: settings load failed", e)
      );

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "outsource_worker" || topic === "task") {
        // Assignment/release or a bound-task change → refresh the join data.
        refetch().catch((e) =>
          console.warn("useOutsourceWorkers: SSE refetch failed", e)
        );
      } else if (topic === "chat" || topic === "chat_read") {
        // Only the unread badge moves — re-pull the small workers list alone,
        // re-joining cached tasks/types (T-ec2c: no tasks/manuals redownload).
        refetchWorkersOnly().catch((e) =>
          console.warn("useOutsourceWorkers: SSE unread refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch, refetchWorkersOnly]);

  const saveMaxParallel = useCallback(async (n: number) => {
    const next = await api.patchServerSettings({ outsourceMaxParallel: n });
    setMaxParallel(next.outsourceMaxParallel);
  }, []);

  return { workers, loading, error, maxParallel, saveMaxParallel };
}
