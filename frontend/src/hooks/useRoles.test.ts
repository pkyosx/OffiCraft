// A reconcile that REJECTS must be visible (T-1170 review).
//
// Both role hooks reload on the `role_def` SSE topic — that is what makes a
// 版本紀錄 restore, an agent's write, or another tab's save land on screen. The
// INITIAL load has always set `error` when it failed; the SSE refetch dropped
// its rejection into a console.warn and left `error` false.
//
// The two failures look identical to a reader and are not the same thing: what
// is on screen is now PRE-WRITE state, presented as current, with no affordance
// suggesting otherwise — and nothing will correct it, because the signal that
// would have corrected it is the one that just failed. `console.warn` is not a
// user-visible state; `error` is the only thing either surface renders.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listRoles: vi.fn<() => Promise<unknown>>(),
  getRole: vi.fn<(key: string) => Promise<unknown>>(),
  handler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listRoles: h.listRoles,
    getRole: h.getRole,
    subscribeEvents: (handler: (topic: string) => void) => {
      h.handler = handler;
      return () => {
        h.handler = null;
      };
    },
  },
}));

import { useRole, useRoles } from "./useRoles";

const ROLE = {
  key: "assistant",
  name: "Assistant",
  definitionMd: "the persona",
  sizeChars: 11,
  capChars: 1000,
  ownerId: "owner",
  schemaVersion: 3,
  isDefault: false,
  isSeed: true,
};

beforeEach(() => {
  h.listRoles.mockReset().mockResolvedValue([ROLE]);
  h.getRole.mockReset().mockResolvedValue(ROLE);
  h.handler = null;
});
afterEach(() => vi.restoreAllMocks());

describe("useRoles", () => {
  it("reports a REJECTED SSE refetch, so a roster that stopped reconciling says so", async () => {
    const { result } = renderHook(() => useRoles());
    await act(async () => {
      await Promise.resolve();
    });
    expect(result.current.error).toBe(false);
    expect(result.current.roles).toHaveLength(1);

    h.listRoles.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      h.handler?.("role_def");
      await Promise.resolve();
      await Promise.resolve();
    });

    // The rows on screen are now the PRE-write ones and nothing else is coming.
    expect(result.current.error).toBe(true);
  });

  it("clears the error once a later refetch succeeds", async () => {
    const { result } = renderHook(() => useRoles());
    await act(async () => {
      await Promise.resolve();
    });
    h.listRoles.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      h.handler?.("role_def");
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(result.current.error).toBe(true);

    // Otherwise the fix would be a one-way latch: a single blip would mark the
    // page broken for the rest of the session.
    await act(async () => {
      h.handler?.("role_def");
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(result.current.error).toBe(false);
  });
});

describe("useRole", () => {
  it("reports a REJECTED SSE refetch, so a stale persona is not shown as current", async () => {
    const { result } = renderHook(() => useRole("assistant"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(result.current.error).toBe(false);
    expect(result.current.role).toEqual(ROLE);

    h.getRole.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      h.handler?.("role_def");
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(result.current.error).toBe(true);
    // The last good document stays on screen — dropping it would swap a stale
    // page for a blank one, which is worse and also not what happened.
    expect(result.current.role).toEqual(ROLE);
  });
});
