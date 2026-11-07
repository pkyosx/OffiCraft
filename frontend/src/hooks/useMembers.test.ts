// useMembers — reads overlap, and their responses can come back in any order.
// Only the order they were ISSUED in says which one saw the newer truth.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { SseDelta } from "../api/adapter";
import { deferred } from "../test/deferred";

interface Row {
  id: string;
  unreadCount: number;
}

const h = vi.hoisted(() => ({
  listMembers: vi.fn<() => Promise<unknown[]>>(),
  getMember: vi.fn<(id: string) => Promise<unknown>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listMembers: h.listMembers,
    getMember: h.getMember,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useMembers } from "./useMembers";

const row = (id: string, unreadCount: number): Row => ({ id, unreadCount });

// The owner read m-a's messages: one held member named ⇒ the single-member read.
const ownerReadA: SseDelta = {
  topic: "chat_read",
  names: { reader: "owner", peer: "m-a" },
  ids: ["owner", "m-a"],
};
// m-a wrote to the owner: also the single-member read.
const aWroteToOwner: SseDelta = {
  topic: "chat",
  names: { id: "c-1", from: "m-a", to: "owner" },
  ids: ["c-1", "m-a", "owner"],
};
// Names nothing ⇒ the full list.
const rosterChanged: SseDelta = { topic: "member", names: {}, ids: [] };

async function settle(work: () => void): Promise<void> {
  await act(async () => {
    work();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

const emit = (delta: SseDelta) =>
  settle(() => h.sseHandler?.(delta.topic, delta));

async function mounted(rows: Row[]) {
  h.listMembers.mockResolvedValueOnce(rows);
  const view = renderHook(() => useMembers());
  await waitFor(() => expect(view.result.current.loading).toBe(false));
  return view;
}

beforeEach(() => {
  h.listMembers.mockReset().mockResolvedValue([]);
  h.getMember.mockReset();
  h.sseHandler = null;
});

describe("useMembers read order", () => {
  it("an older list landing after a newer single read keeps that member's number and updates the others", async () => {
    const { result } = await mounted([row("m-a", 3), row("m-b", 0)]);

    const olderList = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(olderList.promise);
    await emit(rosterChanged);

    h.getMember.mockResolvedValueOnce(row("m-a", 0));
    await emit(ownerReadA);
    expect(result.current.members).toEqual([row("m-a", 0), row("m-b", 0)]);

    await settle(() => olderList.resolve([row("m-a", 3), row("m-b", 5)]));

    expect(result.current.members).toEqual([row("m-a", 0), row("m-b", 5)]);
  });

  it("an older list adds a member that was not held and drops one it no longer lists", async () => {
    const { result } = await mounted([row("m-a", 3), row("m-b", 1)]);

    const olderList = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(olderList.promise);
    await emit(rosterChanged);

    h.getMember.mockResolvedValueOnce(row("m-a", 0));
    await emit(ownerReadA);

    await settle(() => olderList.resolve([row("m-a", 3), row("m-c", 2)]));

    expect(result.current.members).toEqual([row("m-a", 0), row("m-c", 2)]);
  });

  it("an older single read landing after a newer single read is dropped", async () => {
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row>();
    h.getMember.mockReturnValueOnce(older.promise);
    await emit(aWroteToOwner);

    h.getMember.mockResolvedValueOnce(row("m-a", 0));
    await emit(ownerReadA);
    expect(result.current.members).toEqual([row("m-a", 0)]);

    await settle(() => older.resolve(row("m-a", 4)));

    expect(result.current.members).toEqual([row("m-a", 0)]);
  });

  it("an older single read landing after a newer list is dropped", async () => {
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row>();
    h.getMember.mockReturnValueOnce(older.promise);
    await emit(aWroteToOwner);

    h.listMembers.mockResolvedValueOnce([row("m-a", 0)]);
    await emit(rosterChanged);
    expect(result.current.members).toEqual([row("m-a", 0)]);

    await settle(() => older.resolve(row("m-a", 4)));

    expect(result.current.members).toEqual([row("m-a", 0)]);
  });

  it("an older list landing after a newer list is dropped whole, membership included", async () => {
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(older.promise);
    await emit(rosterChanged);

    h.listMembers.mockResolvedValueOnce([row("m-a", 0), row("m-new", 1)]);
    await emit(rosterChanged);

    await settle(() => older.resolve([row("m-a", 3), row("m-gone", 9)]));

    expect(result.current.members).toEqual([row("m-a", 0), row("m-new", 1)]);

    // The dropped list must not have moved which ids count as held either: a
    // delta naming m-gone stays a no-op, one naming m-new is a single read.
    h.getMember.mockResolvedValueOnce(row("m-new", 2));
    await emit({
      topic: "chat",
      names: { id: "c-2", from: "m-new", to: "owner" },
      ids: ["c-2", "m-new", "owner"],
    });
    expect(h.getMember).toHaveBeenCalledWith("m-new");
    const reads = h.getMember.mock.calls.length;
    await emit({
      topic: "chat",
      names: { id: "c-3", from: "m-gone", to: "owner" },
      ids: ["c-3", "m-gone", "owner"],
    });
    expect(h.getMember).toHaveBeenCalledTimes(reads);
  });

  it("an older list still lands when the newer list failed, and the view keeps moving", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(older.promise);
    await emit(rosterChanged);

    h.listMembers.mockRejectedValueOnce(new Error("network"));
    await emit(rosterChanged);

    await settle(() => older.resolve([row("m-a", 1)]));

    expect(result.current.members).toEqual([row("m-a", 1)]);
    warn.mockRestore();
  });

  it("the mount read landing after a newer event read is dropped", async () => {
    const mountRead = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(mountRead.promise);
    const { result } = renderHook(() => useMembers());

    h.listMembers.mockResolvedValueOnce([row("m-a", 0)]);
    await emit(rosterChanged);
    expect(result.current.members).toEqual([row("m-a", 0)]);

    await settle(() => mountRead.resolve([row("m-a", 3)]));

    expect(result.current.members).toEqual([row("m-a", 0)]);
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBe(false);
  });

  it("a mount read failing after a newer event read landed raises no error", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const mountRead = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(mountRead.promise);
    const { result } = renderHook(() => useMembers());

    h.listMembers.mockResolvedValueOnce([row("m-a", 0)]);
    await emit(rosterChanged);

    await settle(() => mountRead.reject(new Error("network")));

    expect(result.current.error).toBe(false);
    expect(result.current.loading).toBe(false);
    expect(result.current.members).toEqual([row("m-a", 0)]);
    warn.mockRestore();
  });

  it("a mount read failing with nothing landed raises the error", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    h.listMembers.mockRejectedValueOnce(new Error("network"));
    const { result } = renderHook(() => useMembers());

    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.error).toBe(true);
    expect(result.current.members).toEqual([]);
    warn.mockRestore();
  });

  it("an older refetch landing after a newer event read is dropped", async () => {
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row[]>();
    h.listMembers.mockReturnValueOnce(older.promise);
    let refetched!: Promise<void>;
    await settle(() => {
      refetched = result.current.refetch();
    });

    h.getMember.mockResolvedValueOnce(row("m-a", 0));
    await emit(ownerReadA);

    await settle(() => older.resolve([row("m-a", 3)]));
    await refetched;

    expect(result.current.members).toEqual([row("m-a", 0)]);
  });

  it("an older event read landing after a newer refetch is dropped", async () => {
    const { result } = await mounted([row("m-a", 3)]);

    const older = deferred<Row>();
    h.getMember.mockReturnValueOnce(older.promise);
    await emit(ownerReadA);

    h.listMembers.mockResolvedValueOnce([row("m-a", 0)]);
    await settle(() => {
      void result.current.refetch();
    });
    expect(result.current.members).toEqual([row("m-a", 0)]);

    await settle(() => older.resolve(row("m-a", 3)));

    expect(result.current.members).toEqual([row("m-a", 0)]);
  });

  it("in order, a new message raises the number and marking it read clears it", async () => {
    const { result } = await mounted([row("m-a", 3), row("m-b", 1)]);

    h.getMember.mockResolvedValueOnce(row("m-a", 4));
    await emit(aWroteToOwner);
    expect(result.current.members).toEqual([row("m-a", 4), row("m-b", 1)]);

    h.getMember.mockResolvedValueOnce(row("m-a", 0));
    await emit(ownerReadA);
    expect(result.current.members).toEqual([row("m-a", 0), row("m-b", 1)]);

    h.listMembers.mockResolvedValueOnce([row("m-a", 2), row("m-b", 1)]);
    await emit(rosterChanged);
    expect(result.current.members).toEqual([row("m-a", 2), row("m-b", 1)]);
  });
});
