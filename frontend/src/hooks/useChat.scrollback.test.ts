// useChat scrollback (T-bf82) — black-box pins on the history-paging seam.
//
//   1. loadOlder() pages backwards with the composite keyset cursor (the
//      current OLDEST message's (ts, id)) and PREPENDS the page; a short page
//      flips hasMore to false and further calls are no-ops.
//   2. Concurrency lock: overlapping loadOlder calls fire ONE cursor request.
//   3. SSE/refetch reconciliation MERGES the refetched newest page into the
//      thread by id (loaded history stays in front) — never a whole-array
//      replace, which would eat the scrollback the owner just loaded.
//   4. hasMore derives honestly from the FIRST landed page too: a thread
//      shorter than one page has no history to load.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatCursor, ChatMessage } from "../api/adapter";

const h = vi.hoisted(() => {
  return {
    listChat:
      vi.fn<
        (
          withId: string,
          limit?: number,
          before?: ChatCursor,
        ) => Promise<unknown[]>
      >(),
    peekChat: vi.fn<(withId: string, limit?: number) => Promise<unknown[]>>(),
    listChatReads: vi.fn(async () => [] as unknown[]),
    markChatRead: vi.fn(async () => ({
      readerId: "owner",
      peerId: "b",
      lastReadTs: 1,
    })),
    postChat: vi.fn(async () => ({}) as unknown),
    sseHandler: null as ((topic: string) => void) | null,
  };
});

vi.mock("../api", () => ({
  api: {
    listChat: h.listChat,
    peekChat: h.peekChat,
    listChatReads: h.listChatReads,
    markChatRead: h.markChatRead,
    postChat: h.postChat,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useChat } from "./useChat";

function mkMsg(id: string, from: string, to: string, ts: number): ChatMessage {
  return {
    id,
    from,
    to,
    body: `msg ${id}`,
    ts,
    attachments: [],
    replyCardId: null,
  };
}

/** `count` messages b↔owner with ids `${prefix}0..` and ascending ts from
 * `tsStart` — a full server page when count === 30. */
function page(prefix: string, tsStart: number, count: number): ChatMessage[] {
  return Array.from({ length: count }, (_, i) =>
    mkMsg(`${prefix}${i}`, "b", "owner", tsStart + i),
  );
}

let hasFocusSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.peekChat.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.markChatRead.mockClear();
  h.sseHandler = null;
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  hasFocusSpy.mockRestore();
});

describe("useChat scrollback (loadOlder / hasMore)", () => {
  it("loadOlder pages back from the oldest (ts, id) and prepends; a short page ends the history", async () => {
    const newest = page("n", 1000, 30);
    h.listChat.mockResolvedValueOnce(newest);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    expect(result.current.hasMore).toBe(true); // a full page → may be more

    // The older page (short: 2 < 30) — history exhausted after this.
    const older = [mkMsg("o1", "b", "owner", 500), mkMsg("o2", "owner", "b", 600)];
    h.listChat.mockResolvedValueOnce(older);
    await act(async () => {
      await result.current.loadOlder();
    });

    // The cursor is the pre-load OLDEST message's (ts, id), page size 30.
    expect(h.listChat).toHaveBeenLastCalledWith("b", 30, {
      beforeTs: 1000,
      beforeId: "n0",
    });
    // Prepended in front, order intact.
    expect(result.current.messages.slice(0, 3).map((m) => m.id)).toEqual([
      "o1",
      "o2",
      "n0",
    ]);
    expect(result.current.messages).toHaveLength(32);
    expect(result.current.hasMore).toBe(false);

    // Exhausted → a further loadOlder never hits the wire.
    const calls = h.listChat.mock.calls.length;
    await act(async () => {
      await result.current.loadOlder();
    });
    expect(h.listChat.mock.calls.length).toBe(calls);
  });

  it("a FIRST page shorter than the window means no history (hasMore=false)", async () => {
    h.listChat.mockResolvedValueOnce([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.hasMore).toBe(false);
  });

  it("overlapping loadOlder calls are concurrency-locked to ONE cursor request", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 1000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    let release!: (v: ChatMessage[]) => void;
    h.listChat.mockImplementationOnce(
      () => new Promise((res) => (release = res)),
    );
    await act(async () => {
      const first = result.current.loadOlder();
      const second = result.current.loadOlder(); // in-flight → no-op
      await second;
      release([mkMsg("o1", "b", "owner", 1)]);
      await first;
    });
    // Initial load + exactly ONE cursor page.
    expect(h.listChat).toHaveBeenCalledTimes(2);
    expect(result.current.messages[0].id).toBe("o1");
  });

  it("an SSE refetch MERGES the newest page — loaded history survives in front", async () => {
    const newest = page("n", 1000, 30);
    h.listChat.mockResolvedValueOnce(newest);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChat.mockResolvedValueOnce([mkMsg("o1", "b", "owner", 1)]);
    await act(async () => {
      await result.current.loadOlder();
    });
    expect(result.current.messages).toHaveLength(31);

    // A new message lands → SSE "chat" → the refetched newest page slides
    // (n1..n29 + fresh). The prepended o1 (and the slid-out n0) must survive.
    const slid = [...newest.slice(1), mkMsg("fresh", "b", "owner", 2000)];
    h.listChat.mockResolvedValueOnce(slid);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() =>
      expect(
        result.current.messages[result.current.messages.length - 1].id,
      ).toBe("fresh"),
    );
    const ids = result.current.messages.map((m) => m.id);
    expect(ids).toHaveLength(32); // o1 + n0 + n1..n29 + fresh — nothing eaten
    expect(ids[0]).toBe("o1");
    expect(ids[1]).toBe("n0");
    // No duplicates from the merge.
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("switching peers resets the scrollback window (hasMore re-derives)", async () => {
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b" ? page("n", 1000, 30) : [mkMsg("z1", "z", "owner", 1)],
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    expect(result.current.hasMore).toBe(true);

    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["z1"]),
    );
    expect(result.current.hasMore).toBe(false);
  });
});
