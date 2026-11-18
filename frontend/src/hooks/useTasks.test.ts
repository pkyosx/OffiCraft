// useTasks — black-box pins on the REQUESTS the hook issues and the lists it
// holds. The assertions read the ARGUMENT VALUES, not call counts: "it fetched
// less" is satisfiable by a hook that asks for the wrong rows.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type {
  SseDelta,
  TaskPageCountView,
  TaskPageQuery,
  TaskView,
} from "../api/adapter";

type ListOpts = TaskPageQuery & { limit?: number; offset?: number };

const h = vi.hoisted(() => ({
  listTasks: vi.fn<(opts?: ListOpts) => Promise<unknown[]>>(),
  getTaskPageCount: vi.fn<(q: TaskPageQuery) => Promise<unknown>>(),
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listTasks: h.listTasks,
    getTaskPageCount: h.getTaskPageCount,
    listOutsourceWorkers: h.listOutsourceWorkers,
    listTaskTypes: h.listTaskTypes,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useTasks } from "./useTasks";

const OPEN_QUERY: TaskPageQuery = {
  statuses: ["in_progress", "not_started"],
  executors: [],
  types: [],
  due: [],
  ids: [],
  groupBy: "none",
};

function count(over: Partial<TaskPageCountView> = {}): TaskPageCountView {
  return {
    open: 0,
    total: 0,
    overdue: 0,
    dueSoon: 0,
    dueNextChangeTs: null,
    executorCounts: {},
    lanes: [],
    listTotal: 0,
    now: 1000,
    listDueNextChangeTs: null,
    ...over,
  };
}

function row(id: string, over: Partial<TaskView> = {}): TaskView {
  return {
    id,
    title: id,
    status: "in_progress",
    priority: "mid",
    createdTs: 1,
    closedTs: null,
    dueTs: null,
    dueSoonLeadMinutes: null,
    deps: [],
    ...over,
  } as unknown as TaskView;
}

const rows = (n: number, from = 0) =>
  Array.from({ length: n }, (_, i) => row(`t-${from + i}`));

function taskDelta(...ids: string[]): SseDelta {
  return { topic: "task", names: { id: ids[0] }, ids };
}

const listArgs = () => h.listTasks.mock.calls.map((c) => c[0]);
const ids = (list: { tasks: TaskView[] } | undefined) => list?.tasks.map((t) => t.id);

beforeEach(() => {
  h.listTasks.mockReset().mockResolvedValue([]);
  h.getTaskPageCount.mockReset().mockResolvedValue(count());
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useTasks", () => {
  it("the mount asks the count and the first page of the list with the page's query, nothing wider", async () => {
    h.listTasks.mockResolvedValueOnce([row("t-a"), row("t-b")]);
    h.getTaskPageCount.mockResolvedValueOnce(count({ total: 9, listTotal: 2 }));
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));

    expect(listArgs()).toEqual([{ ...OPEN_QUERY, limit: 50, offset: 0 }]);
    expect(h.getTaskPageCount.mock.calls).toEqual([[OPEN_QUERY]]);
    expect([...result.current.lists]).toEqual([
      ["", { tasks: [row("t-a"), row("t-b")], complete: true }],
    ]);
    expect(result.current.count).toEqual(count({ total: 9, listTotal: 2 }));
  });

  it("loadMore reads the next page from the held length until a short page says it is complete", async () => {
    h.listTasks.mockResolvedValueOnce(rows(50));
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(50));
    expect(result.current.lists.get("")?.complete).toBe(false);

    h.listTasks.mockResolvedValueOnce(rows(20, 50));
    act(() => result.current.loadMore(""));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(70));
    expect(result.current.lists.get("")?.complete).toBe(true);
    expect(listArgs()[1]).toEqual({ ...OPEN_QUERY, limit: 50, offset: 50 });

    act(() => result.current.loadMore(""));
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(2);
  });

  it("a changed query restarts from the first page and replaces what was held", async () => {
    h.listTasks.mockResolvedValueOnce(rows(50)).mockResolvedValueOnce(rows(50, 50));
    let query = OPEN_QUERY;
    const { result, rerender } = renderHook(() => useTasks(query, [""]));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(50));
    act(() => result.current.loadMore(""));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(100));

    h.listTasks.mockResolvedValueOnce([row("t-done", { status: "done" })]);
    query = { ...OPEN_QUERY, statuses: ["done"] };
    rerender();
    expect(result.current.settled).toBe(false);
    await waitFor(() => expect(result.current.settled).toBe(true));

    expect(listArgs()[2]).toEqual({ ...OPEN_QUERY, statuses: ["done"], limit: 50, offset: 0 });
    expect(h.getTaskPageCount.mock.calls[1]).toEqual([{ ...OPEN_QUERY, statuses: ["done"] }]);
    expect(ids(result.current.lists.get(""))).toEqual(["t-done"]);
  });

  it("an equal-but-new query does not re-ask", async () => {
    let query = OPEN_QUERY;
    const { result, rerender } = renderHook(() => useTasks(query, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));
    query = { ...OPEN_QUERY, statuses: [...OPEN_QUERY.statuses!].reverse() };
    rerender();
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(1);
    expect(h.getTaskPageCount).toHaveBeenCalledTimes(1);
  });

  it("grouped, each named lane key is asked as a lane, and a key named later reads only its own first page", async () => {
    const grouped: TaskPageQuery = { ...OPEN_QUERY, groupBy: "due" };
    let keys: string[] = [];
    h.getTaskPageCount.mockResolvedValue(
      count({ lanes: [{ key: "overdue", count: 1 }, { key: "nodue", count: 2 }] })
    );
    const { result, rerender } = renderHook(() => useTasks(grouped, keys));
    await waitFor(() => expect(result.current.count?.lanes).toHaveLength(2));
    expect(h.listTasks).not.toHaveBeenCalled();

    h.listTasks.mockResolvedValueOnce([row("t-late")]);
    keys = ["overdue"];
    rerender();
    await waitFor(() => expect(ids(result.current.lists.get("overdue"))).toEqual(["t-late"]));

    h.listTasks.mockResolvedValueOnce([row("t-x"), row("t-y")]);
    keys = ["overdue", "nodue"];
    rerender();
    await waitFor(() => expect(ids(result.current.lists.get("nodue"))).toEqual(["t-x", "t-y"]));

    expect(listArgs()).toEqual([
      { ...grouped, lane: "overdue", limit: 50, offset: 0 },
      { ...grouped, lane: "nodue", limit: 50, offset: 0 },
    ]);
    expect(ids(result.current.lists.get("overdue"))).toEqual(["t-late"]);
  });

  it("a task delta re-reads the named rows (plus held dependents) with the query: rows replaced in place, dropped when gone, the count re-read", async () => {
    h.listTasks.mockResolvedValueOnce([
      row("t-a"),
      row("t-b"),
      row("t-c", { deps: ["t-a"] }),
      row("t-d"),
    ]);
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(4));

    h.listTasks.mockResolvedValueOnce([row("t-c", { deps: ["t-a"], title: "t-c renamed" })]);
    h.getTaskPageCount.mockResolvedValueOnce(count({ listTotal: 2 }));
    act(() => h.sseHandler?.("task", taskDelta("t-a", "t-b")));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toEqual(["t-c", "t-d"]));

    expect(listArgs()[1]).toEqual({ ...OPEN_QUERY, ids: ["t-a", "t-b", "t-c"] });
    expect(result.current.lists.get("")?.tasks[0].title).toBe("t-c renamed");
    expect(result.current.count?.listTotal).toBe(2);
    expect(h.listTasks).toHaveBeenCalledTimes(2);
    expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1);
  });

  it("a task delta whose row came back with a moved sort field, or that the list did not hold, re-asks the list from the top for as many rows as it held", async () => {
    h.listTasks.mockResolvedValueOnce(rows(60).slice(0, 50)).mockResolvedValueOnce(rows(10, 50));
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(50));
    act(() => result.current.loadMore(""));
    await waitFor(() => expect(ids(result.current.lists.get(""))).toHaveLength(60));

    h.listTasks
      .mockResolvedValueOnce([row("t-3", { priority: "high" })])
      .mockResolvedValueOnce([row("t-3", { priority: "high" }), ...rows(60).filter((t) => t.id !== "t-3")]);
    act(() => h.sseHandler?.("task", taskDelta("t-3")));
    await waitFor(() => expect(ids(result.current.lists.get(""))?.[0]).toBe("t-3"));
    expect(listArgs().slice(2)).toEqual([
      { ...OPEN_QUERY, ids: ["t-3"] },
      { ...OPEN_QUERY, limit: 60, offset: 0 },
    ]);

    h.listTasks
      .mockResolvedValueOnce([row("t-new")])
      .mockResolvedValueOnce([row("t-new"), ...rows(59)]);
    act(() => h.sseHandler?.("task", taskDelta("t-new")));
    await waitFor(() => expect(ids(result.current.lists.get(""))?.[0]).toBe("t-new"));
    expect(listArgs().slice(4)).toEqual([
      { ...OPEN_QUERY, ids: ["t-new"] },
      { ...OPEN_QUERY, limit: 60, offset: 0 },
    ]);
  });

  it("with an id applied, a delta re-reads only that id — never the ids the delta named beside it", async () => {
    const pinned = { ...OPEN_QUERY, ids: ["t-pin"] };
    h.listTasks.mockResolvedValueOnce([row("t-pin")]);
    const { result } = renderHook(() => useTasks(pinned, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));

    h.listTasks.mockResolvedValueOnce([row("t-pin", { title: "renamed" })]);
    act(() => h.sseHandler?.("task", taskDelta("t-other", "t-pin")));
    await waitFor(() => expect(result.current.lists.get("")?.tasks[0].title).toBe("renamed"));
    expect(listArgs()[1]).toEqual({ ...OPEN_QUERY, ids: ["t-pin"] });
  });

  it("a task delta naming more than 200 tasks asks in chunks of at most 200", async () => {
    h.listTasks.mockResolvedValueOnce([row("t-0")]);
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));
    const named = Array.from({ length: 450 }, (_, i) => `t-${i}`);

    act(() => h.sseHandler?.("task", taskDelta(...named)));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(4));

    expect(listArgs().slice(1).map((a) => a?.ids)).toEqual([
      named.slice(0, 200),
      named.slice(200, 400),
      named.slice(400),
    ]);
  });

  it("task deltas arriving while a read is in flight → no second concurrent read, then ONE follow-up asking for all of them", async () => {
    h.listTasks.mockResolvedValueOnce([row("t-1"), row("t-2"), row("t-3")]);
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));
    let land!: (rows: unknown[]) => void;
    h.listTasks.mockImplementationOnce(() => new Promise((resolve) => (land = resolve)));

    act(() => h.sseHandler?.("task", taskDelta("t-1")));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    act(() => h.sseHandler?.("task", taskDelta("t-2")));
    await Promise.resolve();
    act(() => h.sseHandler?.("task", taskDelta("t-3")));
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(2);

    h.listTasks.mockResolvedValueOnce([row("t-2"), row("t-3")]);
    await act(async () => land([row("t-1")]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
    expect(listArgs().map((a) => a?.ids)).toEqual([[], ["t-1"], ["t-2", "t-3"]]);
  });

  it("after the mount read failed, a named task delta → a FULL read, whose rows replace the list and clear the error", async () => {
    h.listTasks.mockRejectedValueOnce(new Error("500"));
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(result.current.error).toBe(true));

    h.listTasks.mockResolvedValueOnce([row("t-a"), row("t-b")]);
    act(() => h.sseHandler?.("task", taskDelta("t-b")));
    await waitFor(() => expect(result.current.error).toBe(false));

    expect(listArgs()).toEqual([
      { ...OPEN_QUERY, limit: 50, offset: 0 },
      { ...OPEN_QUERY, limit: 50, offset: 0 },
    ]);
    expect(ids(result.current.lists.get(""))).toEqual(["t-a", "t-b"]);
  });

  it("a member delta refreshes the worker roster and reads no task list", async () => {
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1));
    h.listOutsourceWorkers.mockResolvedValueOnce([{ id: "ow-9" }]);

    act(() =>
      h.sseHandler?.("member", { topic: "member", names: { id: "ow-9" }, ids: ["ow-9"] })
    );
    await waitFor(() => expect(result.current.workers).toEqual([{ id: "ow-9" }]));

    expect(h.listTasks).toHaveBeenCalledTimes(1);
    expect(h.getTaskPageCount).toHaveBeenCalledTimes(1);
  });

  it("re-reads everything when the server's next due boundary passes, timed from the server's now", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    h.getTaskPageCount.mockResolvedValueOnce(count({ now: 1000, listDueNextChangeTs: 1060 }));
    const { result } = renderHook(() => useTasks(OPEN_QUERY, [""]));
    await waitFor(() => expect(result.current.settled).toBe(true));
    expect(h.listTasks).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(59_000);
    });
    expect(h.listTasks).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(listArgs()[1]).toEqual({ ...OPEN_QUERY, limit: 50, offset: 0 });
    expect(h.getTaskPageCount).toHaveBeenCalledTimes(2);
  });
});
