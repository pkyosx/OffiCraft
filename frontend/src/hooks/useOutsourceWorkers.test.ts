// useOutsourceWorkers — the panel's data is ONE small list (T-a3e4), and the
// row labels it renders come from it.
//
// History this file has now pinned twice, because the fix moved: T-ec2c stopped a
// chat line from re-downloading the whole task list + the manuals list by
// splitting the refetch into "full join" vs "workers only". That split was a
// workaround for a client-side JOIN which should never have been the client's:
// the panel needed the bound task's created_ts (sort) and its 任務編號 / type
// labels, so it pulled the ENTIRE unfiltered task history on every
// task/member delta. T-a3e4 folded those fields into the worker projection,
// so the join — and the two-path refetch that existed to dodge it — are gone.
//
// The assertions therefore have to be TWO-SIDED: "no task list is fetched" alone
// would be satisfied by a panel that lost its labels, which is exactly the
// failure mode worth guarding.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { OutsourceWorkerView, SseDelta } from "../api/adapter";
import { deferred } from "../test/deferred";

const h = vi.hoisted(() => ({
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  getOutsourceWorker: vi.fn<(id: string) => Promise<unknown>>(),
  listTasks: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  getServerSettings: vi.fn(async () => ({ outsourceMaxParallel: 3 })),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listOutsourceWorkers: h.listOutsourceWorkers,
    getOutsourceWorker: h.getOutsourceWorker,
    listTasks: h.listTasks,
    listTaskTypes: h.listTaskTypes,
    getServerSettings: h.getServerSettings,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useOutsourceWorkers } from "./useOutsourceWorkers";

function worker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return {
    id: "ow-1",
    codename: "O-7",
    model: "claude-opus-5",
    effort: "medium",
    taskId: "t-1111aaaabbbb",
    ...over,
  };
}

beforeEach(() => {
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.getOutsourceWorker.mockReset();
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useOutsourceWorkers (T-a3e4)", () => {
  it("mount pulls the workers list ONLY — no task list, no manuals list", async () => {
    renderHook(() => useOutsourceWorkers());
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1)
    );
    // MUTANT: put `api.listTasks()` back into refetch → red. On e7120c5 both of
    // these were 1, so this assertion is NOT true of the code being replaced.
    expect(h.listTasks).not.toHaveBeenCalled();
    expect(h.listTaskTypes).not.toHaveBeenCalled();
  });

  it("EVERY delta (task / worker / chat / chat_read) re-pulls just that list", async () => {
    renderHook(() => useOutsourceWorkers());
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1)
    );
    for (const [i, topic] of [
      "task",
      "member",
      "chat",
      "chat_read",
    ].entries()) {
      emit(topic);
      await waitFor(() =>
        expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(i + 2)
      );
    }
    // The task-list download that T-ec2c had to route around is not reachable
    // from any branch any more, so there is nothing left to route around.
    expect(h.listTasks).not.toHaveBeenCalled();
    expect(h.listTaskTypes).not.toHaveBeenCalled();
  });

  it("orders rows by the WIRE task_created_ts, falling back to the mint stamp", async () => {
    // The other half of the contract: dropping the task-list fetch must not cost
    // the ordering. Newest bound task first; a worker whose task did not resolve
    // (task_created_ts 0) sorts on its own created_ts — honest, never fabricated.
    h.listOutsourceWorkers.mockResolvedValue([
      worker({ id: "ow-old", taskCreatedTs: 1000, createdTs: 9999 }),
      worker({ id: "ow-unresolved", taskCreatedTs: 0, createdTs: 1500 }),
      worker({ id: "ow-new", taskCreatedTs: 2000, createdTs: 1 }),
    ]);
    const { result } = renderHook(() => useOutsourceWorkers());
    await waitFor(() => expect(result.current.workers).toHaveLength(3));
    expect(result.current.workers.map((w) => w.id)).toEqual([
      "ow-new",
      "ow-unresolved",
      "ow-old",
    ]);
  });

  it("keeps the row labels the panel renders — straight off the worker DTO", async () => {
    h.listOutsourceWorkers.mockResolvedValue([
      worker({
        taskNo: "T-1111",
        taskTypeKey: "tm-review",
        taskTypeName: "程式碼審查",
        taskCreatedTs: 5,
      }),
    ]);
    const { result } = renderHook(() => useOutsourceWorkers());
    await waitFor(() => expect(result.current.workers).toHaveLength(1));
    const w = result.current.workers[0];
    // Values, not presence: a hook that blanked these would still have "a"
    // taskNo field on the object.
    expect(w.taskNo).toBe("T-1111");
    expect(w.taskTypeKey).toBe("tm-review");
    expect(w.taskTypeName).toBe("程式碼審查");
  });
});

describe("useOutsourceWorkers read order", () => {
  const row = (id: string, unreadCount: number, taskCreatedTs = 1) =>
    worker({ id, unreadCount, taskCreatedTs });
  const rows = (workers: OutsourceWorkerView[]) =>
    workers.map((w) => ({ id: w.id, unreadCount: w.unreadCount }));

  const ownerReadW1: SseDelta = {
    topic: "chat_read",
    names: { reader: "owner", peer: "ow-1" },
    ids: ["owner", "ow-1"],
  };
  const w1WroteToOwner: SseDelta = {
    topic: "chat",
    names: { id: "c-1", from: "ow-1", to: "owner" },
    ids: ["c-1", "ow-1", "owner"],
  };
  const workersChanged: SseDelta = { topic: "member", names: {}, ids: [] };

  async function settle(work: () => void): Promise<void> {
    await act(async () => {
      work();
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
  }
  const emitDelta = (delta: SseDelta) =>
    settle(() => h.sseHandler?.(delta.topic, delta));

  async function mounted(initial: OutsourceWorkerView[]) {
    h.listOutsourceWorkers.mockResolvedValueOnce(initial);
    const view = renderHook(() => useOutsourceWorkers());
    await waitFor(() => expect(view.result.current.loading).toBe(false));
    return view;
  }

  it("an older list landing after a newer single read keeps that row's number, updates the others and keeps the order", async () => {
    const { result } = await mounted([row("ow-1", 3, 2), row("ow-2", 0, 1)]);

    const olderList = deferred<OutsourceWorkerView[]>();
    h.listOutsourceWorkers.mockReturnValueOnce(olderList.promise);
    await emitDelta(workersChanged);

    h.getOutsourceWorker.mockResolvedValueOnce(row("ow-1", 0, 2));
    await emitDelta(ownerReadW1);

    await settle(() =>
      olderList.resolve([row("ow-2", 5, 1), row("ow-1", 3, 2)])
    );

    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
      { id: "ow-2", unreadCount: 5 },
    ]);
  });

  it("an older single read landing after a newer single read is dropped", async () => {
    const { result } = await mounted([row("ow-1", 3)]);

    const older = deferred<OutsourceWorkerView>();
    h.getOutsourceWorker.mockReturnValueOnce(older.promise);
    await emitDelta(w1WroteToOwner);

    h.getOutsourceWorker.mockResolvedValueOnce(row("ow-1", 0));
    await emitDelta(ownerReadW1);

    await settle(() => older.resolve(row("ow-1", 4)));

    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
    ]);
  });

  it("an older list landing after a newer list is dropped whole", async () => {
    const { result } = await mounted([row("ow-1", 3)]);

    const older = deferred<OutsourceWorkerView[]>();
    h.listOutsourceWorkers.mockReturnValueOnce(older.promise);
    await emitDelta(workersChanged);

    h.listOutsourceWorkers.mockResolvedValueOnce([row("ow-1", 0)]);
    await emitDelta(workersChanged);

    await settle(() => older.resolve([row("ow-1", 3), row("ow-gone", 9)]));

    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
    ]);
  });

  it("the mount read landing after a newer event read is dropped", async () => {
    const mountRead = deferred<OutsourceWorkerView[]>();
    h.listOutsourceWorkers.mockReturnValueOnce(mountRead.promise);
    const { result } = renderHook(() => useOutsourceWorkers());

    h.listOutsourceWorkers.mockResolvedValueOnce([row("ow-1", 0)]);
    await emitDelta(workersChanged);

    await settle(() => mountRead.resolve([row("ow-1", 3)]));

    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
    ]);
    expect(result.current.loading).toBe(false);
  });

  it("a mount read failing after a newer event read landed raises no error; with nothing landed it does", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const mountRead = deferred<OutsourceWorkerView[]>();
    h.listOutsourceWorkers.mockReturnValueOnce(mountRead.promise);
    const landed = renderHook(() => useOutsourceWorkers());

    h.listOutsourceWorkers.mockResolvedValueOnce([row("ow-1", 0)]);
    await emitDelta(workersChanged);
    await settle(() => mountRead.reject(new Error("network")));

    expect(landed.result.current.error).toBe(false);
    expect(rows(landed.result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
    ]);
    landed.unmount();

    h.listOutsourceWorkers.mockRejectedValueOnce(new Error("network"));
    const failed = renderHook(() => useOutsourceWorkers());
    await waitFor(() => expect(failed.result.current.loading).toBe(false));

    expect(failed.result.current.error).toBe(true);
    warn.mockRestore();
  });

  it("in order, a new message raises the number and marking it read clears it", async () => {
    const { result } = await mounted([row("ow-1", 3)]);

    h.getOutsourceWorker.mockResolvedValueOnce(row("ow-1", 4));
    await emitDelta(w1WroteToOwner);
    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 4 },
    ]);

    h.getOutsourceWorker.mockResolvedValueOnce(row("ow-1", 0));
    await emitDelta(ownerReadW1);
    expect(rows(result.current.workers)).toEqual([
      { id: "ow-1", unreadCount: 0 },
    ]);
  });
});
