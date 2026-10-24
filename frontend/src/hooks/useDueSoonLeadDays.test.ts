import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { SseDelta } from "../api/adapter";

type Listener = (topic: string, delta?: SseDelta) => void;
const listeners: Listener[] = [];
const load = vi.fn();
const refresh = vi.fn();

vi.mock("../api", () => ({
  api: {
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));
vi.mock("./sharedServerSettings", () => ({
  loadServerSettings: () => load(),
  refreshServerSettings: () => refresh(),
}));

import { useDueSoonLeadDays } from "./useDueSoonLeadDays";

const delta = (ids: string[]): SseDelta => ({
  topic: "task",
  names: ids.length ? { id: ids[0] } : {},
  ids,
});

beforeEach(() => {
  listeners.length = 0;
  load.mockReset().mockResolvedValue({ taskDueSoonLeadDays: 1 });
  refresh.mockReset().mockResolvedValue({ taskDueSoonLeadDays: 5 });
});

describe("useDueSoonLeadDays", () => {
  it("re-reads the workspace lead on a task delta that names no task", async () => {
    const { result } = renderHook(() => useDueSoonLeadDays());
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    act(() => listeners.forEach((cb) => cb("task", delta([]))));
    await waitFor(() => expect(result.current).toBe(5));
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("leaves the lead alone on a delta that names a task or belongs to another topic", async () => {
    const { result } = renderHook(() => useDueSoonLeadDays());
    await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    act(() => {
      listeners.forEach((cb) => cb("task", delta(["T-1"])));
      listeners.forEach((cb) => cb("member", delta([])));
    });
    expect(refresh).not.toHaveBeenCalled();
    expect(result.current).toBe(1);
  });
});
