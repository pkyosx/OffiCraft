// useTasks — 勾什麼就問什麼 (T-a3e4) black-box pins.
//
// REPLACES useTasks.open.test.ts, which pinned T-2b9d's boolean fast path
// (`?open=true` by default, the FULL population once `includeClosed` flipped).
// That contract is gone, and keeping its tests would have pinned the payload bug
// itself: open-only removed the archive but still shipped every live task, and
// the flag was re-derived from the rendered list (TasksPage's needClosed), which
// is what let one dep-carrying task widen every subsequent fetch to the whole
// history. The hook now asks for the STATUS SET the page states.
//
// What each test would have to see fail: the assertions read the ARGUMENT VALUES
// (which statuses), not the call count — "it refetched less" is satisfiable by a
// hook that asks for the wrong rows.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { SseDelta, TaskView } from "../api/adapter";

const h = vi.hoisted(() => ({
  listTasks: vi.fn<
    (opts?: {
      open?: boolean;
      statuses?: string[];
      ids?: string[];
    }) => Promise<unknown[]>
  >(),
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listTasks: h.listTasks,
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

const NON_TERMINAL = [
  "not_started",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  "reassigning",
];

beforeEach(() => {
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

function row(id: string, status = "in_progress", deps: string[] = []): TaskView {
  return { id, status, title: id, deps } as unknown as TaskView;
}

function taskDelta(...ids: string[]): SseDelta {
  return { topic: "task", names: { id: ids[0] }, ids };
}

function statusesOf(call: number): string[] | undefined {
  return h.listTasks.mock.calls[call][0]?.statuses;
}

describe("useTasks (T-a3e4)", () => {
  it("the MOUNT fetch already carries the caller's default set", async () => {
    // The expensive frame: the mount fetch precedes every effect, so a hook that
    // opened unconstrained would pull the whole archive once per page open and
    // only then narrow. MUTANT: default statusKey to undefined → red.
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    expect(statusesOf(0)).toEqual([...NON_TERMINAL].sort());
    // …and it is NOT the old open-only flag: that shipped every live task
    // regardless of what the 狀態 dropdown had ticked.
    expect(h.listTasks.mock.calls[0][0]?.open).toBeUndefined();
  });

  it("asks for EXACTLY the set it is given, and re-asks when the set changes", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));

    act(() => result.current.setStatuses(["done", "in_progress"]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(statusesOf(1)).toEqual(["done", "in_progress"]);

    act(() => result.current.setStatuses(["waiting_owner"]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
    expect(statusesOf(2)).toEqual(["waiting_owner"]);
  });

  it("an EMPTY set (清除篩選 = 所有狀態) drops the constraint entirely", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => result.current.setStatuses([]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    // undefined opts = the unfiltered list, byte-for-byte the old full fetch —
    // the ONE view that genuinely needs it must still be able to ask.
    expect(h.listTasks.mock.calls[1][0]).toBeUndefined();
    act(() => result.current.setStatuses(undefined));
    // Same ask ⇒ no refetch: the key is what changed, not the array identity.
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(2);
  });

  it("an SSE task delta re-asks with the SAME set — it never widens", async () => {
    // 🔴 The regression this hook exists to prevent: the old flag was derived
    // from the loaded rows, so one dep-carrying task turned every delta into a
    // full-history download (measured 408 KB vs 17 KB).
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => h.sseHandler?.("task"));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(statusesOf(1)).toEqual([...NON_TERMINAL].sort());
    expect(
      h.listTasks.mock.calls.every((c) => c[0]?.statuses !== undefined)
    ).toBe(true);
  });

  it("a task delta naming tasks → one ids ask (named + held dependents) ANDed with the set; rows replaced, inserted and dropped", async () => {
    h.listTasks.mockResolvedValueOnce([
      row("t-a"),
      row("t-b"),
      row("t-c", "in_progress", ["t-a"]),
      row("t-d"),
    ]);
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(result.current.tasks).toHaveLength(4));

    h.listTasks.mockResolvedValueOnce([
      { ...row("t-c", "in_progress", ["t-a"]), title: "t-c renamed" },
      row("t-new"),
    ]);
    act(() => h.sseHandler?.("task", taskDelta("t-a", "t-b", "t-new")));
    await waitFor(() => expect(result.current.tasks).toHaveLength(3));

    expect(h.listTasks.mock.calls[1][0]).toEqual({
      statuses: [...NON_TERMINAL].sort(),
      ids: ["t-a", "t-b", "t-new", "t-c"],
    });
    expect(result.current.tasks.map((t) => [t.id, t.title])).toEqual([
      ["t-c", "t-c renamed"],
      ["t-d", "t-d"],
      ["t-new", "t-new"],
    ]);
    expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1);
  });

  it("a task delta naming more than 200 tasks → asks in chunks of at most 200", async () => {
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    const ids = Array.from({ length: 450 }, (_, i) => `t-${i}`);

    act(() => h.sseHandler?.("task", taskDelta(...ids)));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(4));

    expect(h.listTasks.mock.calls.slice(1).map((c) => c[0]?.ids)).toEqual([
      ids.slice(0, 200),
      ids.slice(200, 400),
      ids.slice(400),
    ]);
  });

  it("task deltas arriving while a list read is in flight → no second concurrent read, then ONE follow-up asking for all of them", async () => {
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    let land!: (rows: unknown[]) => void;
    h.listTasks.mockImplementationOnce(
      () => new Promise((resolve) => (land = resolve))
    );

    act(() => h.sseHandler?.("task", taskDelta("t-1")));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    act(() => h.sseHandler?.("task", taskDelta("t-2")));
    await Promise.resolve();
    act(() => h.sseHandler?.("task", taskDelta("t-3")));
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(2);

    await act(async () => land([]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
    expect(h.listTasks.mock.calls.map((c) => c[0]?.ids)).toEqual([
      undefined,
      ["t-1"],
      ["t-2", "t-3"],
    ]);
  });

  it("after the mount read failed, a named task delta → a FULL read of the set, whose rows replace the list and clear the error", async () => {
    h.listTasks.mockRejectedValueOnce(new Error("500"));
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(result.current.error).toBe(true));

    h.listTasks.mockResolvedValueOnce([row("t-a"), row("t-b")]);
    act(() => h.sseHandler?.("task", taskDelta("t-b")));
    await waitFor(() => expect(result.current.error).toBe(false));

    expect(h.listTasks.mock.calls.map((c) => c[0])).toEqual([
      { statuses: [...NON_TERMINAL].sort() },
      { statuses: [...NON_TERMINAL].sort() },
    ]);
    expect(result.current.tasks.map((t) => t.id)).toEqual(["t-a", "t-b"]);
  });

  it("after a status-set change's read failed, a named task delta → a FULL read of the NEW set, whose rows replace the old set's and clear the error", async () => {
    h.listTasks.mockResolvedValueOnce([row("t-open")]);
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(result.current.tasks).toHaveLength(1));

    h.listTasks.mockRejectedValueOnce(new Error("offline"));
    act(() => result.current.setStatuses(["done"]));
    await waitFor(() => expect(result.current.error).toBe(true));
    expect(result.current.tasks.map((t) => t.id)).toEqual(["t-open"]);

    h.listTasks.mockResolvedValueOnce([row("t-done", "done")]);
    act(() => h.sseHandler?.("task", taskDelta("t-done")));
    await waitFor(() => expect(result.current.error).toBe(false));

    expect(h.listTasks.mock.calls.map((c) => c[0])).toEqual([
      { statuses: [...NON_TERMINAL].sort() },
      { statuses: ["done"] },
      { statuses: ["done"] },
    ]);
    expect(result.current.tasks.map((t) => t.id)).toEqual(["t-done"]);
  });

  it("a member delta → refreshes the worker roster and reads no task list", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1));
    h.listOutsourceWorkers.mockResolvedValueOnce([{ id: "ow-9" }]);

    act(() =>
      h.sseHandler?.("member", { topic: "member", names: { id: "ow-9" }, ids: ["ow-9"] })
    );
    await waitFor(() => expect(result.current.workers).toEqual([{ id: "ow-9" }]));

    expect(h.listTasks).toHaveBeenCalledTimes(1);
  });

  it("an equal-but-new array does not re-fire the fetch", async () => {
    // The page rebuilds its Set on every render (a 30s clock tick is enough), so
    // identity-based deps would re-download the list for nothing.
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => result.current.setStatuses([...NON_TERMINAL]));
    act(() => result.current.setStatuses([...NON_TERMINAL].reverse()));
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(1);
  });
});
