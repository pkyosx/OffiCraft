// hooks/useTasks.ts — the 任務頁's data: the full task list + the live
// outsource-worker roster + the task-type list, kept fresh the same way as
// useReplyCards. Reconcile-by-refetch (contract B): a "task" SSE delta (create
// / plan / status / priority / terminate, from ANY entry point) → REFETCH the
// list, never merge an event payload; "outsource_worker" refreshes the worker
// roster (assignment / release) and "task_manual" the type-filter options. The
// owner actions (terminate / priority / reassign / message) also refetch
// directly so the mock behaves identically.

import { useCallback, useEffect, useState } from "react";
import type {
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
} from "../api/adapter";
import { api } from "../api";

interface UseTasks {
  /** EVERY task, unfiltered/unsorted (the page partitions + orders + filters). */
  tasks: TaskView[];
  /** LIVE outsource workers — the 外包 executor display resolves through this. */
  workers: OutsourceWorkerView[];
  /** Task types (任務手冊) — the type filter's 各手冊類型 options. */
  taskTypes: TaskTypeView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (500/network; 401 already bounced) —
   * so a failed load never masquerades as the 目前沒有任務 empty state. */
  error: boolean;
  /** Terminate (owner double-confirmed upstream), then refetch. */
  terminate: (id: string) => Promise<void>;
  /** Mark a task a duplicate of the original (T-02c9), then refetch. */
  markDuplicate: (id: string, duplicateOf: string) => Promise<void>;
  /** Priority change incl. freeze/unfreeze, then refetch. */
  setPriority: (id: string, priority: string) => Promise<void>;
  /** 轉派 (T-160e): re-point the task at a member / a freshly minted 外包, then
   * refetch — the move lands the task in `reassigning` and (on an outsource
   * target) changes the worker roster too. */
  reassign: (id: string, input: TaskReassignInput) => Promise<void>;
  /** The task-card message box send (owner → executor). */
  sendMessage: (id: string, msg: TaskMessageInput) => Promise<void>;
  /** Fetch ONE task's FULL detail (steps + description) — the per-card expand
   * hydration path, since the list itself carries only the light projection. */
  getDetail: (id: string) => Promise<TaskView>;
  /** Owner/admin un-pin of one task artifact (T-3dc5), then refetch. */
  removeArtifact: (taskId: string, artifactId: string) => Promise<void>;
  /** Whether the list currently includes terminal (已結束) tasks. Default view
   * (未結束 only) loads open-only (T-2b9d, `GET /api/tasks?open=true`); the page
   * flips this true the moment its filters could surface a terminal task (a
   * cleared/terminal status filter or a single-task jump anchor), which
   * refetches the FULL population. */
  includeClosed: boolean;
  setIncludeClosed: (v: boolean) => void;
  /** Whether `tasks` as it stands came from a closed-inclusive fetch (T-1d82).
   * Distinguishes "not in the list because it has not been loaded" from "not
   * in the list because it does not exist" — the only honest basis for telling
   * the owner a dep is 查無此任務. */
  closedLoaded: boolean;
}

export function useTasks(): UseTasks {
  const [tasks, setTasks] = useState<TaskView[]>([]);
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [taskTypes, setTaskTypes] = useState<TaskTypeView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Default: 未結束-only (the page opens on that partition). The page flips this
  // true when a view could show a terminal task, refetching the full list.
  const [includeClosed, setIncludeClosed] = useState(false);
  // T-1d82: whether the tasks CURRENTLY in state came from a closed-inclusive
  // fetch. `includeClosed` alone cannot answer that — it flips the moment the
  // page asks, while `tasks` still holds the open-only rows until the refetch
  // lands. Anything that must distinguish 「找不到，因為還沒載到」 from
  // 「找不到，因為真的不存在」 has to read THIS, or it will state the second
  // during the frames that are only the first (see TaskCard's dep rows).
  const [closedLoaded, setClosedLoaded] = useState(false);

  const refetch = useCallback(async () => {
    const [t, w] = await Promise.all([
      api.listTasks(includeClosed ? undefined : { open: true }),
      api.listOutsourceWorkers(),
    ]);
    setTasks(t);
    setWorkers(w);
    // Set from the value this fetch actually USED, together with its rows —
    // never from the live flag, which may already have moved on.
    setClosedLoaded(includeClosed);
    setError(false);
  }, [includeClosed]);

  const refetchTypes = useCallback(async () => {
    setTaskTypes(await api.listTaskTypes());
  }, []);

  useEffect(() => {
    let alive = true;

    Promise.all([refetch(), refetchTypes()])
      .catch((e) => {
        console.warn("useTasks: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task" || topic === "outsource_worker") {
        refetch().catch((e) =>
          console.warn("useTasks: SSE refetch failed", e)
        );
      } else if (topic === "task_manual") {
        refetchTypes().catch((e) =>
          console.warn("useTasks: SSE types refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch, refetchTypes]);

  const terminate = useCallback(
    async (id: string) => {
      await api.terminateTask(id);
      await refetch();
    },
    [refetch]
  );

  const markDuplicate = useCallback(
    async (id: string, duplicateOf: string) => {
      await api.markTaskDuplicate(id, duplicateOf);
      await refetch();
    },
    [refetch]
  );

  const setPriority = useCallback(
    async (id: string, priority: string) => {
      await api.setTaskPriority(id, priority);
      await refetch();
    },
    [refetch]
  );

  const reassign = useCallback(
    async (id: string, input: TaskReassignInput) => {
      await api.reassignTask(id, input);
      await refetch();
    },
    [refetch]
  );

  const sendMessage = useCallback(
    async (id: string, msg: TaskMessageInput) => {
      await api.postTaskMessage(id, msg);
    },
    []
  );

  const getDetail = useCallback((id: string) => api.getTask(id), []);

  const removeArtifact = useCallback(
    async (taskId: string, artifactId: string) => {
      await api.removeTaskArtifact(taskId, artifactId);
      await refetch();
    },
    [refetch]
  );

  return {
    tasks,
    workers,
    taskTypes,
    loading,
    error,
    terminate,
    markDuplicate,
    setPriority,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
    includeClosed,
    setIncludeClosed,
    closedLoaded,
  };
}
