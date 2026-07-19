// useTasks open-only default (T-2b9d) black-box pins.
//
// The 任務頁 opens on the 未結束 partition, so the list loads open-only
// (?open=true) — a handful of rows, not the whole history. The moment the page
// needs terminal rows (清除篩選 全部, a terminal status ticked, a jump anchor)
// it flips includeClosed true and the hook refetches the FULL population.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listTasks: vi.fn<(opts?: { open?: boolean }) => Promise<unknown[]>>(),
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listTasks: h.listTasks,
    listOutsourceWorkers: h.listOutsourceWorkers,
    listTaskTypes: h.listTaskTypes,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useTasks } from "./useTasks";

beforeEach(() => {
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

describe("useTasks (T-2b9d)", () => {
  it("loads open-only by default", async () => {
    renderHook(() => useTasks());
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    // LOAD-BEARING: the default page load must ask for the open-only slice.
    // MUTANT: drop the `{ open: true }` arg (fetch full) and this goes red.
    expect(h.listTasks.mock.calls[0][0]).toEqual({ open: true });
  });

  it("refetches the FULL population when includeClosed flips true", async () => {
    const { result } = renderHook(() => useTasks());
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));

    act(() => result.current.setIncludeClosed(true));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    // The full call carries NO open flag (undefined = the whole history).
    expect(h.listTasks.mock.calls[1][0]).toBeUndefined();

    // …and flips back to open-only when the view no longer needs closed rows.
    act(() => result.current.setIncludeClosed(false));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
    expect(h.listTasks.mock.calls[2][0]).toEqual({ open: true });
  });
});
