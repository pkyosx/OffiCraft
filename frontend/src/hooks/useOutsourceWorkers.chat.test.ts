// useOutsourceWorkers chat-cost split (T-ec2c) black-box pins.
//
// The office 外包 panel's data hook used to re-pull workers + the whole task
// list + the task-type (manuals) list on EVERY chat delta — a company chat
// line re-downloaded hundreds of KB that a chat message never changes. Now a
// chat/chat_read delta re-pulls ONLY the small workers list (its unread badge),
// re-joining the cached tasks/types; the full join re-pull is reserved for the
// task / outsource_worker deltas that actually move it.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTasks: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  getServerSettings: vi.fn(async () => ({ outsourceMaxParallel: 3 })),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listOutsourceWorkers: h.listOutsourceWorkers,
    listTasks: h.listTasks,
    listTaskTypes: h.listTaskTypes,
    getServerSettings: h.getServerSettings,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useOutsourceWorkers } from "./useOutsourceWorkers";

beforeEach(() => {
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useOutsourceWorkers (T-ec2c)", () => {
  it("full join on mount: workers + tasks + types once each", async () => {
    renderHook(() => useOutsourceWorkers());
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1)
    );
    expect(h.listTasks).toHaveBeenCalledTimes(1);
    expect(h.listTaskTypes).toHaveBeenCalledTimes(1);
  });

  it("chat / chat_read re-pull ONLY the workers list (no tasks/manuals redownload)", async () => {
    renderHook(() => useOutsourceWorkers());
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1)
    );

    emit("chat");
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(2)
    );
    emit("chat_read");
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(3)
    );

    // LOAD-BEARING negative: the heavy task list + manuals list must NOT be
    // re-pulled on a chat delta — still exactly their single mount call.
    // MUTANT: route the chat branch back through the full `refetch()` and this
    // goes red (listTasks/listTaskTypes climb to 3).
    expect(h.listTasks).toHaveBeenCalledTimes(1);
    expect(h.listTaskTypes).toHaveBeenCalledTimes(1);
  });

  it("task / outsource_worker deltas DO re-pull the full join", async () => {
    renderHook(() => useOutsourceWorkers());
    await waitFor(() =>
      expect(h.listOutsourceWorkers).toHaveBeenCalledTimes(1)
    );
    emit("task");
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(h.listTaskTypes).toHaveBeenCalledTimes(2);
    emit("outsource_worker");
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
  });
});
