// useTasks — the #tasks/<id> link jump fetches ONE task, not the archive
// (owner 2026-08-01). Black-box pins on the REQUESTS the hook actually issues.
//
// The defect: an anchor could point at a task outside the ticked statuses (a
// 已完成 one under the default filter is the everyday case), so the page dropped
// its `?statuses=` constraint and downloaded the whole population — measured on
// the live workshop at 432 KB / 706 rows — to make ONE card appear.
//
// Every assertion below reads the ARGUMENTS of the calls, never a count: "it
// fetched less" is satisfiable by a hook that fetched the wrong thing, and a
// hook that widened the list AND also fetched the single task would issue one
// more call, not fewer. The load-bearing one is `statuses` never being
// undefined while an anchor is set — that is exactly the value the old code
// sent.
//
// 🔴 NOT pinned here on purpose: an EMPTY status set (清除篩選 = 所有狀態) still
// drops the constraint and downloads everything. That view asked for the whole
// population; useTasks.statuses.test.ts pins it and must keep passing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { TaskView } from "../api/adapter";

const h = vi.hoisted(() => ({
  listTasks: vi.fn<(opts?: { statuses?: string[] }) => Promise<unknown[]>>(),
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  getTask: vi.fn<(id: string) => Promise<unknown>>(),
  // EVERY subscriber, not just the last one: the anchor keeps its own
  // subscription alongside the list's, and a single-slot handler would silently
  // drop whichever registered first.
  sseHandlers: [] as ((topic: string) => void)[],
}));

vi.mock("../api", () => ({
  api: {
    listTasks: h.listTasks,
    listOutsourceWorkers: h.listOutsourceWorkers,
    listTaskTypes: h.listTaskTypes,
    getTask: h.getTask,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandlers.push(cb);
      return () => {
        h.sseHandlers = h.sseHandlers.filter((x) => x !== cb);
      };
    },
  },
}));

import { useTasks } from "./useTasks";

const NON_TERMINAL = [
  "not_started",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  "reassigning",
];

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "t-x",
    taskNo: "T-000x",
    title: "任務",
    typeKey: "",
    description: "",
    status: "done",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1,
    updatedTs: 2,
    closedTs: 3,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  } as TaskView;
}

beforeEach(() => {
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.getTask.mockReset().mockResolvedValue(mkTask({ id: "t-closed" }));
  h.sseHandlers = [];
});

describe("useTasks single-task anchor (owner 2026-08-01)", () => {
  it("hydrates the anchored task from GET /api/tasks/{id} and NEVER drops the status constraint", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL, "t-closed"));
    await waitFor(() => expect(h.getTask).toHaveBeenCalled());

    // The single-task read really happened, against the anchored id.
    expect(h.getTask.mock.calls.map((c) => c[0])).toEqual(["t-closed"]);
    // 🔴 The load-bearing one. The old code answered the anchor by calling
    // listTasks() with NO statuses — the 432 KB request. MUTANT: send
    // `undefined` for an anchored view again and this goes red on the first
    // call, whatever else the hook does.
    expect(h.listTasks).toHaveBeenCalled();
    for (const [opts] of h.listTasks.mock.calls) {
      expect(opts?.statuses).toEqual([...NON_TERMINAL].sort());
    }
    // …and the anchored task is actually on the page.
    await waitFor(() =>
      expect(result.current.tasks.map((x) => x.id)).toEqual(["t-closed"])
    );
    expect(result.current.anchorPending).toBe(false);
  });

  it("no anchor ⇒ no single-task read at all", async () => {
    // Guards the other direction: a hook that fetched getTask unconditionally
    // would satisfy the test above while spending a request on every page open.
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 0));
    expect(h.getTask).not.toHaveBeenCalled();
  });

  it("a REJECTED hydrate resolves pending and adds no row (no stuck spinner)", async () => {
    // 補抓失敗 must terminate. anchorPending is what the page's empty state and
    // error banner both wait on, so a catch that forgot to resolve it would
    // freeze the page on a 載入中 that never ends. MUTANT: drop the setAnchor
    // from the catch → red.
    h.getTask.mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useTasks(NON_TERMINAL, "t-gone"));
    await waitFor(() => expect(result.current.anchorPending).toBe(false));
    expect(result.current.tasks).toEqual([]);
  });

  it("is PENDING until the hydrate lands — the page must not call it missing yet", async () => {
    // The distinction 「還沒載到」 vs 「不存在」. It has to be true in the very
    // first render (the page decides its empty state on the first commit), so a
    // flag set from inside an effect would already be too late.
    let release!: (v: unknown) => void;
    h.getTask.mockReturnValue(new Promise((r) => (release = r)));
    const { result } = renderHook(() => useTasks(NON_TERMINAL, "t-closed"));
    expect(result.current.anchorPending).toBe(true);
    await act(async () => {
      release(mkTask({ id: "t-closed" }));
    });
    await waitFor(() => expect(result.current.anchorPending).toBe(false));
  });

  it("prefers the LIST row when the ticked statuses already contain the anchor", async () => {
    // No duplicate card, and the surviving row is the list's — only it carries
    // the server's dep_tasks join (TaskDTO has no such field), so a merge that
    // let the single-task copy win would blank out the dep chips of any task you
    // linked to.
    h.listTasks.mockResolvedValue([
      mkTask({ id: "t-open", status: "in_progress", title: "FROM LIST" }),
    ]);
    h.getTask.mockResolvedValue(
      mkTask({ id: "t-open", status: "in_progress", title: "FROM DETAIL" })
    );
    const { result } = renderHook(() => useTasks(NON_TERMINAL, "t-open"));
    await waitFor(() => expect(result.current.anchorPending).toBe(false));
    expect(result.current.tasks.map((x) => x.title)).toEqual(["FROM LIST"]);
  });

  it("a task SSE delta re-reads the anchored task — it does not go stale", async () => {
    renderHook(() => useTasks(NON_TERMINAL, "t-closed"));
    await waitFor(() => expect(h.getTask).toHaveBeenCalledTimes(1));
    act(() => h.sseHandlers.forEach((cb) => cb("task")));
    await waitFor(() => expect(h.getTask).toHaveBeenCalledTimes(2));
    // …and the delta still did not widen the list ask.
    for (const [opts] of h.listTasks.mock.calls) {
      expect(opts?.statuses).toEqual([...NON_TERMINAL].sort());
    }
  });
});
