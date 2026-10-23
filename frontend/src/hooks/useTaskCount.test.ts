import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { TaskCountView } from "../api/adapter";

const h = vi.hoisted(() => ({
  getTaskCount: vi.fn<() => Promise<TaskCountView>>(),
}));
vi.mock("../api", () => ({
  api: {
    getTaskCount: h.getTaskCount,
    subscribeEvents: () => () => {},
  },
}));

import { useTaskCount } from "./useTaskCount";

const NOW = 1792807200;

function counts(over: Partial<TaskCountView> = {}): TaskCountView {
  return {
    open: 3,
    total: 5,
    overdue: 1,
    dueSoon: 1,
    dueNextChangeTs: null,
    ...over,
  };
}

async function flush() {
  await act(async () => {});
}

async function advance(ms: number) {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers({ now: NOW * 1000 });
  h.getTaskCount.mockReset();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("useTaskCount", () => {
  it("returns the server's open, overdue and due-soon counts", async () => {
    h.getTaskCount.mockResolvedValue(counts({ overdue: 2, dueSoon: 4 }));
    const { result } = renderHook(() => useTaskCount());
    await flush();
    expect(result.current).toEqual({
      open: 3,
      total: 5,
      overdue: 2,
      dueSoon: 4,
      dueNextChangeTs: null,
    });
  });

  it("refetches at dueNextChangeTs without any task event", async () => {
    h.getTaskCount
      .mockResolvedValueOnce(counts({ dueSoon: 1, dueNextChangeTs: NOW + 60 }))
      .mockResolvedValue(counts({ overdue: 2, dueSoon: 0 }));
    const { result } = renderHook(() => useTaskCount());
    await flush();
    await advance(59_000);
    expect(h.getTaskCount).toHaveBeenCalledTimes(1);
    await advance(1_000);
    expect(h.getTaskCount).toHaveBeenCalledTimes(2);
    expect(result.current.overdue).toBe(2);
  });

  it("never refetches on a timer when nothing is due", async () => {
    h.getTaskCount.mockResolvedValue(counts({ dueNextChangeTs: null }));
    renderHook(() => useTaskCount());
    await flush();
    await advance(10 * 86_400_000);
    expect(h.getTaskCount).toHaveBeenCalledTimes(1);
  });

  it("clamps a change moment beyond setTimeout's range instead of firing at once", async () => {
    h.getTaskCount.mockResolvedValue(
      counts({ dueNextChangeTs: NOW + 40 * 86_400 })
    );
    renderHook(() => useTaskCount());
    await flush();
    await advance(1_000);
    expect(h.getTaskCount).toHaveBeenCalledTimes(1);
    await advance(2_147_483_647);
    expect(h.getTaskCount).toHaveBeenCalledTimes(2);
  });

  it("stops the timer on unmount", async () => {
    h.getTaskCount.mockResolvedValue(counts({ dueNextChangeTs: NOW + 60 }));
    const { unmount } = renderHook(() => useTaskCount());
    await flush();
    unmount();
    await advance(120_000);
    expect(h.getTaskCount).toHaveBeenCalledTimes(1);
  });
});
