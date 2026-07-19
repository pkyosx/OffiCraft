// useChat — "reading requires looking" (badge-flash fix) black-box pins.
//
// The chat thread must stay fresh through BOTH windows states, but only an
// ACTIVE window (tab visible + OS focus) may take the side-effectful
// "list 即讀" route (listChat ?with= → the server advances the owner's read
// watermark). A backgrounded window loads through the READ-ONLY peekChat —
// messages keep flowing, unread keeps counting — and returning to the
// foreground re-runs the marking listChat so the badge clears exactly then.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage } from "../api/adapter";

const h = vi.hoisted(() => {
  return {
    listChat: vi.fn<(withId: string, limit?: number) => Promise<unknown[]>>(),
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
  return { id, from, to, body: `msg ${id}`, ts, attachments: [], replyCardId: null };
}

let hasFocusSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.peekChat.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.markChatRead.mockClear();
  h.sseHandler = null;
  // jsdom is "visible" by default; drive activity through hasFocus.
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  hasFocusSpy.mockRestore();
});

describe("useChat load routing (active vs background)", () => {
  it("an ACTIVE window loads through the marking listChat", async () => {
    h.listChat.mockResolvedValue([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(h.listChat).toHaveBeenCalledWith("b");
    expect(h.peekChat).not.toHaveBeenCalled();
    expect(result.current.messagesPeer).toBe("b");
  });

  it("a BACKGROUNDED window loads through the read-only peekChat — messages still flow", async () => {
    hasFocusSpy.mockReturnValue(false);
    h.peekChat.mockResolvedValue([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    // The thread stayed fresh WITHOUT the watermark side effect.
    expect(h.peekChat).toHaveBeenCalledWith("b");
    expect(h.listChat).not.toHaveBeenCalled();
  });

  it("an SSE 'chat' event while backgrounded peeks; while active it lists", async () => {
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    // Background → the SSE-driven refetch must NOT consume the unread state.
    hasFocusSpy.mockReturnValue(false);
    h.peekChat.mockResolvedValue([mkMsg("c2", "b", "owner", 2000)]);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() => expect(h.peekChat).toHaveBeenCalledTimes(1));
    expect(h.listChat).toHaveBeenCalledTimes(1); // unchanged
    // The new inbound message still landed (訊息更新不能斷).
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.messages[0].id).toBe("c2");

    // Foreground again → the SSE refetch is the marking list once more.
    hasFocusSpy.mockReturnValue(true);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(2));
    expect(h.peekChat).toHaveBeenCalledTimes(1); // unchanged
  });

  it("returning to the foreground re-runs the MARKING list (badge clears on real look)", async () => {
    hasFocusSpy.mockReturnValue(false);
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.peekChat).toHaveBeenCalledTimes(1));
    expect(h.listChat).not.toHaveBeenCalled();

    hasFocusSpy.mockReturnValue(true);
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));
    expect(h.listChat).toHaveBeenCalledWith("b");
  });

  it("a blur that leaves the window inactive does NOT trigger a marking list", async () => {
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    hasFocusSpy.mockReturnValue(false);
    act(() => {
      window.dispatchEvent(new Event("blur"));
      document.dispatchEvent(new Event("visibilitychange"));
    });
    // No additional load of either kind was fired by the deactivation itself.
    expect(h.listChat).toHaveBeenCalledTimes(1);
    expect(h.peekChat).not.toHaveBeenCalled();
  });

  it("switching peers resets the thread and re-loads for the new peer", async () => {
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b"
        ? [mkMsg("c1", "b", "owner", 1000)]
        : [mkMsg("c9", "z", "owner", 2000)],
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.messagesPeer).toBe("b");

    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["c9"]),
    );
  });
});
