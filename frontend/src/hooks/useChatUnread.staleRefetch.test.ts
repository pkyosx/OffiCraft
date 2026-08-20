// useChatUnread — "a count fetch that never landed gets paid on the next event"
// (T-929f). Same defect, same shape as useChat's `load()`: the experiment that
// found the frozen chat thread ALSO measured the task/office badge freezing
// alongside it, because both hooks end a rejected refetch at a `console.warn`.
//
// Why the badge could sit stale for 90s while the connection looked healthy:
// the ordinary gate (`burstMovesNoOwnerUnread`) skips agent↔agent traffic
// because "the server would hand back the number we already hold". That is true
// only while the number we hold IS the server's. After a failed fetch it is
// not, so every skip strands the stale value — and agent↔agent traffic is the
// ordinary case here.
//
// WHAT IS ASSERTED: which api call was made, how many times, and what the
// rendered count became. No log-string matching anywhere.
//
// The rerender() in the recovery window is deliberate on two counts: the mark
// is a ref (a cleanup-only ref write dies under StrictMode's
// setup→cleanup→setup, so the hook writes it in the setup body), and a test
// that merely awaits without forcing a commit can go green against a hook that
// never repaints.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode, createElement } from "react";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { SseDelta } from "../api/adapter";

const h = vi.hoisted(() => ({
  getChatUnreadCount: vi.fn<() => Promise<number>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    getChatUnreadCount: h.getChatUnreadCount,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useChatUnread } from "./useChatUnread";

// Addressed TO the owner ⇒ it can move the total ⇒ the gate lets it through.
const toOwner: SseDelta = {
  topic: "chat",
  names: { id: "m1", from: "a", to: "owner" },
  ids: ["m1", "a", "owner"],
};
// Agent↔agent ⇒ cannot move the owner's total ⇒ the gate skips it. This is the
// shape that kept the stale badge invisible.
const agentToAgent: SseDelta = {
  topic: "chat",
  names: { id: "m2", from: "a", to: "b" },
  ids: ["m2", "a", "b"],
};

beforeEach(() => {
  h.getChatUnreadCount.mockReset().mockResolvedValue(0);
  h.sseHandler = null;
});

async function emit(delta: SseDelta): Promise<void> {
  await act(async () => {
    h.sseHandler?.(delta.topic, delta);
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useChatUnread: a failed count fetch is marked and paid on the next event", () => {
  it("a fetch that REJECTS keeps the last rendered count and fires no retry of its own", async () => {
    h.getChatUnreadCount.mockResolvedValue(3);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(3));

    h.getChatUnreadCount.mockRejectedValueOnce(new Error("network"));
    await emit(toOwner);
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(2);
    expect(result.current).toBe(3);

    // MARK, DON'T RETRY: nothing on a timer or a backoff.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(2);
  });

  it("after a failed fetch, an agent↔agent delta the gate would skip still refetches", async () => {
    h.getChatUnreadCount.mockResolvedValue(3);
    const { result, rerender } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(3));

    h.getChatUnreadCount.mockRejectedValueOnce(new Error("network"));
    await emit(toOwner);
    expect(result.current).toBe(3); // stale, and the hook knows it

    rerender(); // forced commit in the waiting window (see the header)

    h.getChatUnreadCount.mockResolvedValue(7);
    await emit(agentToAgent);

    await waitFor(() => expect(h.getChatUnreadCount).toHaveBeenCalledTimes(3));
    await waitFor(() => expect(result.current).toBe(7));
  });

  it("once the catch-up fetch lands, the mark is cleared — the next agent↔agent delta is skipped again", async () => {
    h.getChatUnreadCount.mockResolvedValue(3);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(3));

    h.getChatUnreadCount.mockRejectedValueOnce(new Error("network"));
    await emit(toOwner);
    h.getChatUnreadCount.mockResolvedValue(7);
    await emit(agentToAgent);
    await waitFor(() => expect(result.current).toBe(7));
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(3);

    // Debt paid ⇒ the T-b17f saving is back in force. Without this, "fix" could
    // mean deleting the gate, restoring one full `ListChat()` table scan per
    // agent↔agent line for a number that cannot change.
    await emit(agentToAgent);
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(3);
  });

  it("with NO failed fetch, an agent↔agent delta never refetches", async () => {
    h.getChatUnreadCount.mockResolvedValue(3);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(3));
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1);

    await emit(agentToAgent);
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1);
  });

  it("an irrelevant topic does NOT pay the debt — the mark widens the gate, it does not open it", async () => {
    h.getChatUnreadCount.mockResolvedValue(3);
    renderHook(() => useChatUnread());
    await waitFor(() => expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1));

    h.getChatUnreadCount.mockRejectedValueOnce(new Error("network"));
    await emit(toOwner);
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(2);

    // "monitoring" is not in OFFICE_TOTAL_TOPICS: it is not a relevant event,
    // debt or no debt.
    await emit({ topic: "monitoring", names: {}, ids: [] });
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(2);
  });

  it("a fetch rejecting AFTER its effect instance was torn down writes no debt onto the successor", async () => {
    // The mark is a ref, so it OUTLIVES the effect instance that wrote it while
    // staying SHARED with that instance's successor. StrictMode (which is how
    // main.tsx mounts the app) is the ordinary way to get two instances out of
    // one component: setup → cleanup → setup. If the first setup's fetch
    // rejects after the second setup body has already cleared the mark, an
    // unguarded catch hands the live instance a debt it never incurred — and it
    // then pays for it with a full `ListChat()` scan on the very agent↔agent
    // traffic the T-b17f gate exists to skip.
    let rejectDead!: (e: unknown) => void;
    const stuck = new Promise<number>((_, reject) => {
      rejectDead = reject;
    });

    h.getChatUnreadCount
      .mockImplementationOnce(() => stuck) // the torn-down instance's fetch
      .mockResolvedValue(3); // the surviving instance's fetch

    const { result } = renderHook(() => useChatUnread(), {
      wrapper: ({ children }) => createElement(StrictMode, null, children),
    });
    await waitFor(() => expect(result.current).toBe(3));
    const afterMount = h.getChatUnreadCount.mock.calls.length;

    // NOW the dead instance's fetch fails.
    await act(async () => {
      rejectDead(new Error("late network"));
      await Promise.resolve();
      await Promise.resolve();
    });

    // The live instance owes nothing, so the gate still skips agent↔agent.
    await emit(agentToAgent);
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(afterMount);
    expect(result.current).toBe(3);
  });
});
