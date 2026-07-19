// useMembers light-mode (T-cf91) black-box pins.
//
// The 請示卡頁 attributes each card to its asker by name + role only. Its roster
// hook takes the identity-only projection AND — the load-bearing half — does
// NOT re-pull the roster when anyone in the company sends a chat line (a
// message changes no name or role). The FULL roster (office) keeps its
// chat-driven refetch, because its unread badge derives from the chat stream.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listMembers:
    vi.fn<(opts?: { light?: boolean }) => Promise<unknown[]>>(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listMembers: h.listMembers,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useMembers } from "./useMembers";

beforeEach(() => {
  h.listMembers.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useMembers full (default)", () => {
  it("refetches the roster on a chat delta (unread badge liveness)", async () => {
    renderHook(() => useMembers());
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));
    // Default call carries no light flag.
    expect(h.listMembers.mock.calls[0][0]).toBeUndefined();
    emit("chat");
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(2));
    emit("chat_read");
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(3));
  });
});

describe("useMembers light (T-cf91)", () => {
  it("fetches the light projection on mount", async () => {
    renderHook(() => useMembers({ light: true }));
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));
    expect(h.listMembers.mock.calls[0][0]).toEqual({ light: true });
  });

  it("does NOT refetch on chat / chat_read deltas", async () => {
    renderHook(() => useMembers({ light: true }));
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));
    // LOAD-BEARING negative: a company chat line must not touch the light
    // roster. MUTANT: put "chat"/"chat_read" back into the light topic set (or
    // point the light hook at ROSTER_TOPICS) and this goes red.
    emit("chat");
    emit("chat_read");
    // Give any errant refetch a chance to land, then assert it did not.
    await Promise.resolve();
    expect(h.listMembers).toHaveBeenCalledTimes(1);
  });

  it("still refetches on member / role_def deltas", async () => {
    renderHook(() => useMembers({ light: true }));
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));
    emit("member");
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(2));
    emit("role_def");
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(3));
    // And each of those refetches keeps the light flag.
    expect(h.listMembers.mock.calls[2][0]).toEqual({ light: true });
  });
});
