// hooks/useTasks.ts — the 任務頁's data: the page's lists, asked of the server
// one page at a time, the server's counts for them, the live outsource-worker
// roster and the task-type list.
//
// The server filters, counts, groups and orders (`GET /api/tasks` with the
// page's params, `GET /api/tasks/count` with the same params). The page names
// the lists it shows — "" for the ungrouped list, else each expanded lane key —
// and this hook holds the rows read so far for each, 50 at a time.
//
// Reconcile-by-refetch: an SSE delta is a reason to re-read, never a payload to
// merge. A "task" delta re-reads the rows it named (plus held rows whose dep
// join shows a named task) through `?ids=` with the page's filter, so a row
// that left the list drops out. A named row that came back where its place in
// the order may have moved — a row not held yet, or one whose sort fields
// changed — re-asks that list from the top for as many rows as it held,
// because the order is the server's and is not repeated here. A delta that
// names nothing re-reads every list. "member" refreshes the worker roster
// only — it cannot change a task row — and "task_manual" the type options.

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
  TaskPageQuery,
  TaskPageCountView,
} from "../api/adapter";
import { api } from "../api";
import { createDeltaSink } from "../lib/deltaSink";

/** Rows asked per page, and per scroll to the bottom. */
export const TASK_PAGE_SIZE = 50;
/** The server's cap on one `?limit=` and on one `?ids=`. */
const LIST_ASK_MAX = 200;
// setTimeout fires at once past this many ms.
const MAX_TIMER_MS = 2 ** 31 - 1;
// A due-boundary re-read that failed is retried no sooner than this.
const DUE_RETRY_MS = 30_000;

export interface TaskList {
  tasks: TaskView[];
  /** The server has no more rows for this list. */
  complete: boolean;
}

interface ListAsk {
  full: boolean;
  ids: Set<string>;
  more: Set<string>;
  waiters: { resolve: () => void; reject: (e: unknown) => void }[];
}

interface Held {
  /** The query key these lists and this count answer. */
  forKey: string;
  query: TaskPageQuery | null;
  lists: Map<string, TaskList>;
  count: TaskPageCountView | null;
}

interface UseTasks {
  /** The rows read so far per list key ("" = the ungrouped list, else a lane
   * key), for the query last answered. A key the page named but that has not
   * landed yet is absent. */
  lists: Map<string, TaskList>;
  /** The server's counts for the query last answered; null until one lands. */
  count: TaskPageCountView | null;
  /** The query `lists` and `count` answer — the previous one while a changed
   * query is still on its way. */
  answeredQuery: TaskPageQuery | null;
  /** True once the lists and the count answer the CURRENT query — until then
   * what is held answers the previous one and must not be read as "nothing
   * matches". */
  settled: boolean;
  /** Read the next page of a list (no-op once complete or while one is read). */
  loadMore: (key: string) => void;
  /** Keys whose next page is being read. */
  loadingMore: Set<string>;
  /** LIVE outsource workers — the 外包 executor display resolves through this. */
  workers: OutsourceWorkerView[];
  /** Task types (任務手冊) — the type filter's 各手冊類型 options. */
  taskTypes: TaskTypeView[];
  /** True until the first answer lands. */
  loading: boolean;
  /** True when the last read of the current query REJECTED (500/network; 401
   * already bounced) — so a failed load never masquerades as an empty state. */
  error: boolean;
  /** Search every task for the mark-duplicate picker. */
  searchTasks: (q: string, statuses: string[]) => Promise<TaskView[]>;
  terminate: (id: string) => Promise<void>;
  /** Force a task closed over its precondition. `reason` may be "". */
  forceDone: (id: string, reason: string) => Promise<void>;
  markDuplicate: (id: string, duplicateOf: string) => Promise<void>;
  setPriority: (id: string, priority: string) => Promise<void>;
  setDue: (id: string, dueTs: number | null, leadMinutes?: number | null) => Promise<void>;
  /** Accepted on a CLOSED task too. */
  updateDescription: (id: string, description: string) => Promise<void>;
  /** A BLANK title is refused by the server and this rejects. */
  updateTitle: (id: string, title: string) => Promise<void>;
  /** 轉派: also changes the worker roster on an outsource target. */
  reassign: (id: string, input: TaskReassignInput) => Promise<void>;
  sendMessage: (id: string, msg: TaskMessageInput) => Promise<void>;
  /** ONE task's FULL detail (steps + description) — the card's expand path. */
  getDetail: (id: string) => Promise<TaskView>;
  removeArtifact: (taskId: string, artifactId: string) => Promise<void>;
}

export function taskQueryKey(q: TaskPageQuery): string {
  const set = (v?: string[]) => [...new Set(v ?? [])].sort();
  return JSON.stringify([
    set(q.statuses),
    set(q.executors),
    set(q.types),
    set(q.due),
    set(q.ids),
    (q.q ?? "").trim(),
    q.groupBy ?? "none",
  ]);
}

function sortFieldsMoved(a: TaskView, b: TaskView): boolean {
  return (
    a.status !== b.status ||
    a.priority !== b.priority ||
    a.createdTs !== b.createdTs ||
    (a.closedTs ?? null) !== (b.closedTs ?? null) ||
    (a.dueTs ?? null) !== (b.dueTs ?? null) ||
    (a.dueSoonLeadMinutes ?? null) !== (b.dueSoonLeadMinutes ?? null)
  );
}

function listQuery(q: TaskPageQuery, key: string): TaskPageQuery {
  return (q.groupBy ?? "none") === "none" ? q : { ...q, lane: key };
}

async function readHead(q: TaskPageQuery, key: string, rows: number): Promise<TaskList> {
  const out: TaskView[] = [];
  for (let offset = 0; offset < rows; offset += LIST_ASK_MAX) {
    const limit = Math.min(LIST_ASK_MAX, rows - offset);
    const got = await api.listTasks({ ...listQuery(q, key), limit, offset });
    out.push(...got);
    if (got.length < limit) return { tasks: out, complete: true };
  }
  return { tasks: out, complete: false };
}

/**
 * `query` is the page's applied filter and grouping; `listKeys` the lists it
 * shows ("" alone when ungrouped, the expanded lane keys when grouped — none
 * until the count has named the lanes).
 */
export function useTasks(query: TaskPageQuery, listKeys: string[]): UseTasks {
  const queryKey = taskQueryKey(query);
  const keysKey = JSON.stringify([...listKeys].sort());
  const [held, setHeld] = useState<Held>({
    forKey: "",
    query: null,
    lists: new Map(),
    count: null,
  });
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [taskTypes, setTaskTypes] = useState<TaskTypeView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [loadingMore, setLoadingMore] = useState<Set<string>>(() => new Set());

  const queryRef = useRef({ q: query, key: queryKey });
  queryRef.current = { q: query, key: queryKey };
  const keysRef = useRef(listKeys);
  keysRef.current = listKeys;
  const heldRef = useRef(held);
  const fullFailedRef = useRef(false);
  const [fullFailures, setFullFailures] = useState(0);
  // At most one read in flight; asks that arrive meanwhile are folded into
  // `next` and served by ONE follow-up, sent after them.
  const loaderRef = useRef<{ busy: boolean; next: ListAsk | null }>({
    busy: false,
    next: null,
  });

  const land = useCallback((next: Held) => {
    heldRef.current = next;
    setHeld(next);
  }, []);

  const runAsk = useCallback(
    async (ask: ListAsk) => {
      const { q, key } = queryRef.current;
      const keys = keysRef.current;
      const cur = heldRef.current;
      const stillAsked = () => queryRef.current.key === key;

      if (ask.full || cur.forKey !== key || fullFailedRef.current) {
        const fresh = cur.forKey !== key;
        const grouped = (q.groupBy ?? "none") !== "none";
        const rowsFor = (k: string) =>
          fresh
            ? TASK_PAGE_SIZE
            : Math.max(TASK_PAGE_SIZE, cur.lists.get(k)?.tasks.length ?? 0);
        const lists = new Map<string, TaskList>();
        let count: TaskPageCountView;
        try {
          const [c, flatList, w] = await Promise.all([
            api.getTaskPageCount(q),
            grouped ? null : readHead(q, "", rowsFor("")),
            api.listOutsourceWorkers(),
          ]);
          count = c;
          setWorkers(w);
          if (flatList !== null) lists.set("", flatList);
          else {
            // Lanes the new count no longer has are not asked: under a changed
            // grouping the held keys name the old grouping's lanes.
            const live = keys.filter((k) => c.lanes.some((l) => l.key === k));
            const read = await Promise.all(live.map((k) => readHead(q, k, rowsFor(k))));
            live.forEach((k, i) => lists.set(k, read[i]));
          }
        } catch (e) {
          // What is held may now be stale, so whatever asks next re-reads it all.
          fullFailedRef.current = true;
          setFullFailures((n) => n + 1);
          setError(true);
          throw e;
        }
        fullFailedRef.current = false;
        setFullFailures(0);
        if (!stillAsked()) return;
        land({ forKey: key, query: q, lists, count });
        setError(false);
        return;
      }

      const lists = new Map<string, TaskList>();
      for (const k of keys) {
        const have = cur.lists.get(k);
        if (have) lists.set(k, have);
      }
      const reads: Promise<void>[] = [];

      for (const k of keys) {
        if (!lists.has(k)) {
          reads.push(readHead(q, k, TASK_PAGE_SIZE).then((l) => void lists.set(k, l)));
        }
      }

      for (const k of ask.more) {
        const have = lists.get(k);
        if (!have || have.complete) continue;
        reads.push(
          api
            .listTasks({ ...listQuery(q, k), limit: TASK_PAGE_SIZE, offset: have.tasks.length })
            .then((got) => {
              const seen = new Set(have.tasks.map((t) => t.id));
              lists.set(k, {
                tasks: [...have.tasks, ...got.filter((t) => !seen.has(t.id))],
                complete: got.length < TASK_PAGE_SIZE,
              });
            })
        );
      }

      await Promise.all(reads);
      reads.length = 0;

      let count = cur.count;
      if (ask.ids.size > 0) {
        const pinned = new Set(q.ids ?? []);
        const named = [...ask.ids].filter((id) => pinned.size === 0 || pinned.has(id));
        reads.push(
          api.getTaskPageCount(q).then((c) => {
            count = c;
          })
        );
        for (const [k, have] of [...lists]) {
          if (named.length === 0) continue;
          reads.push(
            (async () => {
              const chunks: string[][] = [];
              for (let i = 0; i < named.length; i += LIST_ASK_MAX) {
                chunks.push(named.slice(i, i + LIST_ASK_MAX));
              }
              const answers = await Promise.all(
                chunks.map((ids) => api.listTasks({ ...listQuery(q, k), ids }))
              );
              const fresh = new Map(answers.flat().map((t) => [t.id, t]));
              const heldById = new Map(have.tasks.map((t) => [t.id, t]));
              // A row leaving shifts every later offset too, so a page read
              // since then may have skipped the row at the old boundary.
              const shifted =
                named.some((id) => heldById.has(id) && !fresh.has(id)) ||
                [...fresh.values()].some((t) => {
                  const before = heldById.get(t.id);
                  return before === undefined || sortFieldsMoved(before, t);
                });
              if (shifted) {
                lists.set(
                  k,
                  await readHead(q, k, Math.max(TASK_PAGE_SIZE, have.tasks.length))
                );
                return;
              }
              lists.set(k, {
                tasks: have.tasks.map((t) => fresh.get(t.id) ?? t),
                complete: have.complete,
              });
            })()
          );
        }
      }

      await Promise.all(reads);
      if (!stillAsked()) return;
      land({ forKey: key, query: q, lists, count });
    },
    [land]
  );

  const askList = useCallback(
    (what: { full?: boolean; ids?: Iterable<string>; more?: string }): Promise<void> =>
      new Promise((resolve, reject) => {
        const l = loaderRef.current;
        const ask = (l.next ??= { full: false, ids: new Set(), more: new Set(), waiters: [] });
        if (what.full) ask.full = true;
        for (const id of what.ids ?? []) ask.ids.add(id);
        if (what.more !== undefined) ask.more.add(what.more);
        ask.waiters.push({ resolve, reject });
        if (l.busy) return;
        l.busy = true;
        void (async () => {
          try {
            while (l.next) {
              const job = l.next;
              l.next = null;
              try {
                await runAsk(job);
                for (const w of job.waiters) w.resolve();
              } catch (e) {
                for (const w of job.waiters) w.reject(e);
              }
            }
          } finally {
            l.busy = false;
          }
        })();
      }),
    [runAsk]
  );

  const refreshTasks = useCallback(
    (ids: Iterable<string>) => {
      const named = new Set(ids);
      for (const list of heldRef.current.lists.values()) {
        for (const t of list.tasks) {
          if (t.deps.some((d) => named.has(d))) named.add(t.id);
        }
      }
      return askList({ ids: named });
    },
    [askList]
  );

  const refetch = useCallback(() => askList({ full: true }), [askList]);

  const loadMore = useCallback(
    (key: string) => {
      const have = heldRef.current.lists.get(key);
      if (!have || have.complete) return;
      setLoadingMore((cur) => (cur.has(key) ? cur : new Set(cur).add(key)));
      askList({ more: key })
        .catch((e) => console.warn("useTasks: next page failed", e))
        .finally(() =>
          setLoadingMore((cur) => {
            const next = new Set(cur);
            next.delete(key);
            return next;
          })
        );
    },
    [askList]
  );

  // A changed query re-reads everything; a newly named list key reads its first
  // page. Both go through the one loader, which compares against what is held.
  useEffect(() => {
    let alive = true;
    const ask = heldRef.current.forKey === queryKey ? askList({}) : refetch();
    ask
      .then(() => {
        if (alive) setError(false);
      })
      .catch((e) => {
        console.warn("useTasks: list load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [queryKey, keysKey, askList, refetch]);

  // The list changes on its own when a task crosses a due boundary; the server
  // says when, on its own clock, so the wait is measured from its `now`.
  const wakeAt = held.forKey === queryKey ? held.count?.listDueNextChangeTs ?? null : null;
  const wakeFrom = held.count?.now ?? 0;
  useEffect(() => {
    if (wakeAt === null) return;
    const due = Math.max(0, (wakeAt - wakeFrom) * 1000) + 500;
    const ms = Math.min(MAX_TIMER_MS, fullFailures > 0 ? Math.max(due, DUE_RETRY_MS) : due);
    const timer = window.setTimeout(() => {
      refetch().catch((e) => console.warn("useTasks: due-boundary refetch failed", e));
    }, ms);
    return () => window.clearTimeout(timer);
  }, [wakeAt, wakeFrom, fullFailures, refetch]);

  const refetchTypes = useCallback(async () => {
    setTaskTypes(await api.listTaskTypes());
  }, []);

  useEffect(() => {
    refetchTypes().catch((e) => {
      console.warn("useTasks: types load failed", e);
      setError(true);
    });
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (batch.topics.has("task_manual")) {
          refetchTypes().catch((e) => console.warn("useTasks: SSE types refetch failed", e));
        }
        const taskDeltas = batch.deltas.filter((d) => d.topic === "task");
        const arrivedWithoutDelta =
          batch.unnamed && !batch.deltas.some((d) => d.ids.length === 0);
        const full =
          batch.topics.has("task") &&
          (arrivedWithoutDelta ||
            taskDeltas.length === 0 ||
            taskDeltas.some((d) => d.ids.length === 0));
        if (batch.topics.has("member") && !full) {
          api
            .listOutsourceWorkers()
            .then(setWorkers)
            .catch((e) => console.warn("useTasks: SSE workers refetch failed", e));
        }
        if (!batch.topics.has("task")) return;
        const read = full ? refetch() : refreshTasks(taskDeltas.flatMap((d) => d.ids));
        read.catch((e) => console.warn("useTasks: SSE refetch failed", e));
      })
    );
    return unsubscribe;
  }, [refetch, refetchTypes, refreshTasks]);

  // A write has landed whatever the re-read after it does; its failure is not
  // the write's failure.
  const afterWrite = useCallback(
    async (ids: string[], what: string) => {
      try {
        await refreshTasks(ids);
      } catch (e) {
        console.warn(`useTasks: post-${what} refetch failed (the write landed)`, e);
      }
    },
    [refreshTasks]
  );

  const terminate = useCallback(
    async (id: string) => {
      await api.terminateTask(id);
      await afterWrite([id], "terminate");
    },
    [afterWrite]
  );

  const forceDone = useCallback(
    async (id: string, reason: string) => {
      await api.forceTaskDone(id, reason);
      await afterWrite([id], "force-done");
    },
    [afterWrite]
  );

  const markDuplicate = useCallback(
    async (id: string, duplicateOf: string) => {
      await api.markTaskDuplicate(id, duplicateOf);
      await afterWrite([id, duplicateOf], "mark-duplicate");
    },
    [afterWrite]
  );

  const setPriority = useCallback(
    async (id: string, priority: string) => {
      await api.setTaskPriority(id, priority);
      await afterWrite([id], "priority");
    },
    [afterWrite]
  );

  const setDue = useCallback(
    async (id: string, dueTs: number | null, leadMinutes?: number | null) => {
      if (leadMinutes === undefined) await api.setTaskDue(id, dueTs);
      else await api.setTaskDue(id, dueTs, leadMinutes);
      await afterWrite([id], "due");
    },
    [afterWrite]
  );

  // The editor rejects only on the write: a failed re-read must not send it
  // back into edit mode holding text the server already has.
  const updateDescription = useCallback(
    async (id: string, description: string) => {
      await api.updateTaskDescription(id, description);
      await afterWrite([id], "description");
    },
    [afterWrite]
  );

  const updateTitle = useCallback(
    async (id: string, title: string) => {
      await api.updateTaskTitle(id, title);
      await afterWrite([id], "title");
    },
    [afterWrite]
  );

  const reassign = useCallback(
    async (id: string, input: TaskReassignInput) => {
      await api.reassignTask(id, input);
      // A failed re-read would otherwise invite a second reassign.
      try {
        const [, w] = await Promise.all([refreshTasks([id]), api.listOutsourceWorkers()]);
        setWorkers(w);
      } catch (e) {
        console.warn("useTasks: post-reassign refetch failed (the task was reassigned)", e);
      }
    },
    [refreshTasks]
  );

  const sendMessage = useCallback(async (id: string, msg: TaskMessageInput) => {
    await api.postTaskMessage(id, msg);
  }, []);

  const getDetail = useCallback((id: string) => api.getTask(id), []);

  const removeArtifact = useCallback(
    async (taskId: string, artifactId: string) => {
      await api.removeTaskArtifact(taskId, artifactId);
      await afterWrite([taskId], "remove-artifact");
    },
    [afterWrite]
  );

  const searchTasks = useCallback(
    (q: string, statuses: string[]) =>
      api.listTasks({ q, statuses, limit: TASK_PAGE_SIZE }),
    []
  );

  const settled = held.forKey === queryKey && !loading;
  return {
    lists: held.lists,
    count: held.count,
    answeredQuery: held.query,
    settled,
    loadMore,
    loadingMore,
    workers,
    taskTypes,
    loading,
    error,
    searchTasks,
    terminate,
    forceDone,
    markDuplicate,
    setPriority,
    setDue,
    updateDescription,
    updateTitle,
    reassign,
    sendMessage,
    getDetail,
    removeArtifact,
  };
}
