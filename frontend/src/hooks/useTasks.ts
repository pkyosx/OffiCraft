// hooks/useTasks.ts — the 任務頁's data: the full task list + the live
// outsource-worker roster + the task-type list, kept fresh the same way as
// useReplyCards. Reconcile-by-refetch (contract B): a "task" SSE delta (create
// / plan / status / priority / terminate, from ANY entry point) → REFETCH the
// list, never merge an event payload; "outsource_worker" refreshes the worker
// roster (assignment / release) and "task_manual" the type-filter options. The
// owner actions (terminate / priority / reassign / message) also refetch
// directly so the mock behaves identically.
//
// T-a3e4: the list is fetched by STATUS SET (`setStatuses`), not by a boolean
// "also include closed". The page hands down what its 狀態 dropdown has ticked
// and the server answers with those rows only, so an SSE-triggered refetch
// costs the ~20 rows on screen instead of the whole task history. `interface
// UseTasks` documents why the boolean had to go rather than be extended.
//
// A #tasks/<id> link-jump does NOT widen that ask. `anchorTaskId` fetches
// exactly that one task (GET /api/tasks/{id}) and MERGES it into `tasks` — see
// the anchor block below for why "just drop the status constraint" was the
// wrong shape for a one-row need.

import { useCallback, useEffect, useState } from "react";
import type {
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
} from "../api/adapter";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { createDeltaSink } from "../lib/deltaSink";

interface UseTasks {
  /** The tasks the LAST fetch asked for — every status when no set was given,
   * otherwise exactly the ticked ones (T-a3e4) — PLUS the single anchored task
   * when one was asked for and the status ask did not already contain it. Still
   * unordered/unpartitioned: the page partitions + orders, and applies the
   * executor/type axes, which stay client-side (they are not what made this
   * payload 400 KB). */
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
  /** Correct the task's description (T-e271), then refetch. Accepted on a
   * CLOSED task too — see the adapter's note on why this one write does not
   * refuse a terminal task. */
  updateDescription: (id: string, description: string) => Promise<void>;
  /** Correct the task's title (T-2ebe), then refetch. Closed tasks are accepted
   * here too; a BLANK title is not — the server answers 400 and this rejects,
   * so the caller can surface it. */
  updateTitle: (id: string, title: string) => Promise<void>;
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
  /** Ask the server for EXACTLY these statuses (T-a3e4): the page hands down
   * the set its 狀態 dropdown has ticked and the fetch sends one `?statuses=`
   * per state. `undefined` (or an empty array) = no status constraint, i.e. the
   * full population — what 清除篩選 全部 and a single-task jump anchor need.
   *
   * This REPLACED a boolean `includeClosed` (T-2b9d's `?open=true`). open-only
   * removed the archive but still shipped every live task regardless of the
   * filter, so the default view downloaded rows it had already decided not to
   * render; and the flag had to be re-derived from the rendered list, which is
   * what made it latch. Asking for what is ticked needs no derivation. */
  setStatuses: (statuses: string[] | undefined) => void;
  /** True while the anchored task's single fetch is still in flight — i.e. its
   * absence from `tasks` means 「還沒載到」, not 「不存在」. The page's empty
   * state MUST wait on this, or every jump to a task outside the ticked
   * statuses would print 沒有符合篩選條件的任務 in the frames before its fetch
   * lands. Resolves either way: a REJECTED fetch (404 / 500 / offline) also
   * clears it, so a failed hydrate ends in a message rather than a blank page
   * or a spinner that never stops. */
  anchorPending: boolean;
  /** True when the anchored task's fetch failed for a reason that is NOT
   * 「這張不存在」 — a 500, an offline browser, a proxy error page.
   *
   * 🔴 This exists because the two outcomes are indistinguishable in `tasks`
   * (both leave the anchor absent) and the RIGHT WORDS FOR THEM ARE OPPOSITE.
   * A 404 means nothing matched, and 沒有符合篩選條件的任務 is true. A 500
   * means the page could not ASK — printing 沒有符合篩選條件的任務 there is a
   * claim about a question that was never answered, and the owner would read a
   * broken server as a deleted task. The page shows its load error instead.
   *
   * The split keys off `ApiError.status === 404`, the one status the server
   * uses for 「這個 id 沒有對應的任務」; every other rejection, typed or not
   * (a TypeError from a dropped connection included), counts as a failure. */
  anchorFailed: boolean;
}

// initialStatuses is the status set the CALLER's filter opens on — the page's
// own default, passed in rather than duplicated here. It matters for exactly one
// frame and that frame is the expensive one: the mount fetch happens before any
// effect can state the filter, so a hook that defaulted to "no constraint" would
// download the whole archive on every page open and then immediately re-ask for
// the twenty rows it renders. Pass [] to genuinely open on 所有狀態.
//
// anchorTaskId is the ONE task a #tasks/<id> link jump has to put on screen even
// though the status ask would not return it (a 已完成 task under the default
// filter is the everyday case). It is fetched on its own and merged in.
export function useTasks(
  initialStatuses: string[],
  anchorTaskId?: string
): UseTasks {
  const [tasks, setTasks] = useState<TaskView[]>([]);
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [taskTypes, setTaskTypes] = useState<TaskTypeView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // The status set the page is currently asking for (T-a3e4). undefined until
  // the page states its filter, and undefined again whenever the view genuinely
  // needs every status (清除篩選 全部 / a single-task anchor that may point at a
  // closed task). Held as a joined KEY, not an array, so that a re-render
  // producing an equal-but-new array does not re-fire the fetch — the page
  // rebuilds its Set on every render, and a raw array in the dep list would
  // refetch the list on every keystroke elsewhere on the page.
  const [statusKey, setStatusKey] = useState<string | undefined>(() =>
    initialStatuses.length === 0
      ? undefined
      : [...initialStatuses].sort().join(",")
  );
  const setStatuses = useCallback((next: string[] | undefined) => {
    setStatusKey(
      next === undefined || next.length === 0
        ? undefined
        : [...next].sort().join(",")
    );
  }, []);

  const refetch = useCallback(async () => {
    const [t, w] = await Promise.all([
      api.listTasks(
        statusKey === undefined ? undefined : { statuses: statusKey.split(",") }
      ),
      api.listOutsourceWorkers(),
    ]);
    setTasks(t);
    setWorkers(w);
    setError(false);
  }, [statusKey]);

  const refetchTypes = useCallback(async () => {
    setTaskTypes(await api.listTaskTypes());
  }, []);

  // ── the single-task anchor (#tasks/<id>) ──────────────────────────────────
  // A link jump needs ONE task on screen. Before this it was expressed as
  // 「拿掉狀態限制」 — the fetch dropped its `?statuses=` and pulled the entire
  // history (measured on the live workshop: 432 KB / 706 rows) so that one row
  // would be somewhere inside it. The size of the ask has nothing to do with the
  // size of the need: `GET /api/tasks/{id}` already answers exactly this
  // question, so the anchor rides that and the LIST keeps answering the ticked
  // statuses like every other view.
  // 🔴 Deliberately NOT extended to 清除篩選 (an empty status set still fetches
  // everything): there the owner is asking for the whole population, so the
  // whole population is the right answer — that is not this defect.
  // The result is held together with the id it belongs to, so a pending fetch is
  // `anchor.id !== anchorId` — derivable in the SAME render the id changes in,
  // which a separate boolean state could not be (an effect-set flag is false for
  // one commit too many, and the page's empty state renders in exactly that
  // commit — it would print 沒有符合篩選條件的任務 over a task that is simply
  // still on its way).
  const anchorId = anchorTaskId ?? "";
  const [anchor, setAnchor] = useState<{
    id: string;
    task: TaskView | null;
    failed: boolean;
  }>({ id: "", task: null, failed: false });
  useEffect(() => {
    if (anchorId === "") {
      setAnchor({ id: "", task: null, failed: false });
      return;
    }
    let alive = true;
    // Resolve the SAME way on success and failure — with the id, and with the
    // task set or null. A rejected hydrate (deleted task, 500, offline) must
    // still land, otherwise the page waits on `anchorPending` forever.
    // What DOES differ between the two failures is `failed`: a 404 is an
    // answer (「沒有這張」), anything else is the absence of one, and the page
    // has to say different things about them. See `anchorFailed`.
    const load = () =>
      api
        .getTask(anchorId)
        .then((task) => {
          if (alive) setAnchor({ id: anchorId, task, failed: false });
        })
        .catch((e) => {
          console.warn("useTasks: anchor task fetch failed", e);
          const notFound = e instanceof ApiError && e.status === 404;
          if (alive)
            setAnchor({ id: anchorId, task: null, failed: !notFound });
        });
    void load();
    // The anchored row is not in the list fetch, so the list's SSE refetch does
    // not keep it fresh — it needs its own subscription to the same delta.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") void load();
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [anchorId]);
  const anchorPending = anchorId !== "" && anchor.id !== anchorId;
  // Only ever true once the fetch has SETTLED on this id: while it is still
  // pending the page must say nothing at all about why the task is missing.
  const anchorFailed = !anchorPending && anchorId !== "" && anchor.failed;
  // Merge, never replace: when the ticked statuses DO include the anchored task
  // the list row is the better one (it carries the server's `dep_tasks` join,
  // which TaskDTO has no field for), so the fetched copy only fills a gap.
  const anchorTask = anchor.task;
  const tasksWithAnchor =
    anchorTask !== null && !tasks.some((x) => x.id === anchorTask.id)
      ? [...tasks, anchorTask]
      : tasks;

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

    // 🔴 NO per-task refetch here, and the reason is a wire fact, not a
    // preference: `GET /api/tasks/{id}` carries no `dep_tasks`. The frozen spec
    // puts that server-side dep join on `TaskListItemDTO` ONLY (see
    // api/dtoParity.ts), and `TaskCard` renders "nobody resolved this dep"
    // differently from "查無此任務" on purpose — so swapping a list row for a
    // full task would silently degrade every dep row on that card to a bare
    // short id (T-a3e4's 「已結案的 dep 仍講得出標題」 lost). Re-pulling the list
    // is ONE GET either way; only the payload is bigger.
    //
    // What T-8115 still buys here is the COALESCING below: a resync fans 13
    // topics synchronously and three of them land in this hook, which used to be
    // two identical list re-pulls plus a types re-pull for one reconnect.
    const full = () =>
      refetch().catch((e) => console.warn("useTasks: SSE refetch failed", e));

    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (batch.topics.has("task_manual")) {
          refetchTypes().catch((e) =>
            console.warn("useTasks: SSE types refetch failed", e)
          );
        }
        const listTopics = [...batch.topics].filter(
          (t) => t === "task" || t === "outsource_worker"
        );
        if (listTopics.length === 0) return;
        void full();
      })
    );

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

  const updateDescription = useCallback(
    async (id: string, description: string) => {
      await api.updateTaskDescription(id, description);
      await refetch();
    },
    [refetch]
  );

  const updateTitle = useCallback(
    async (id: string, title: string) => {
      await api.updateTaskTitle(id, title);
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
    tasks: tasksWithAnchor,
    workers,
    taskTypes,
    loading,
    error,
    terminate,
    markDuplicate,
    setPriority,
    updateDescription,
    updateTitle,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
    setStatuses,
    anchorPending,
    anchorFailed,
  };
}
