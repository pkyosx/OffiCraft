// useChat — "a load that never landed gets paid on the next event" (T-929f).
//
// THE DEFECT THESE PIN. `load()` used to end a rejection at `console.warn` and
// nothing else. There are two failure worlds and only one was covered:
//   - the connection really drops ⇒ `es.onopen` fires again ⇒ api/http.ts
//     `resyncAll()` ⇒ the thread catches up on its own (~2s). Not this file.
//   - the connection is still open in the client's eyes (esOpen=1, esError=0)
//     and just THAT ONE load fails ⇒ the delta was received, the load was lost,
//     and nothing ever tried again. Measured: 90s frozen; a synthesised `focus`
//     filled it in 0.0s.
// The whole reason the second world is invisible to the OLD tests is the
// per-conversation filter: after the failure, the very next deltas are usually
// about SOMEONE ELSE's conversation, which `touchesThisThread` correctly skips —
// correct while the thread we hold is the truth, wrong once we know it is not.
//
// WHAT IS ASSERTED. Branch taken and resulting state only — which call was
// made, how many times, and what `messages` became. Nothing here matches on log
// text or on any substring: a `console.warn` string is not the contract, the
// refetch is. (No pre-existing partial-keyword assertion was found in the files
// this change touches — useChat.test.ts / useChat.scrollback.test.ts /
// useChatUnread*.test.ts carry no `toContain` / `toMatch` — so none had to be
// removed. What that ALSO means: nothing in this package ever asserted on the
// failure path at all, which is exactly how the silent-drop shape survived.)
//
// TWO TRAPS THESE TESTS ARE BUILT AROUND, both of which make a green run lie:
//  1. The mark is a REF. A ref written only in cleanup gets stuck off forever
//     under StrictMode (setup→cleanup→setup, second setup never restores it),
//     so the hook writes it in the effect's setup body. `rerender()` between
//     the failure and the recovery event is here to keep that honest: the mark
//     must survive an ordinary re-render.
//  2. A test that only awaits, without forcing a re-render in the waiting
//     window, can go green against a hook that never repaints. The rerender()
//     below doubles as that forced commit.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage, SseDelta } from "../api/adapter";

const h = vi.hoisted(() => ({
  listChat: vi.fn<(withId: string, limit?: number) => Promise<unknown[]>>(),
  peekChat: vi.fn<(withId: string, limit?: number) => Promise<unknown[]>>(),
  listChatReads: vi.fn(async () => [] as unknown[]),
  markChatRead: vi.fn(async () => ({})) as unknown as ReturnType<typeof vi.fn>,
  postChat: vi.fn(async () => ({}) as unknown),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listChat: h.listChat,
    peekChat: h.peekChat,
    listChatReads: h.listChatReads,
    markChatRead: h.markChatRead,
    postChat: h.postChat,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
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

// A chat line in THIS thread (peer "b" ↔ owner) — the filter lets it through.
const inThisThread: SseDelta = {
  topic: "chat",
  names: { id: "m1", from: "b", to: "owner" },
  ids: ["m1", "b", "owner"],
};
// A chat line between two OTHER participants — the filter skips it. This is the
// shape that made the defect invisible: after the failed load, the traffic that
// keeps arriving is other people's.
const elsewhere: SseDelta = {
  topic: "chat",
  names: { id: "m2", from: "x", to: "y" },
  ids: ["m2", "x", "y"],
};

// A read receipt naming a THIRD party as its reader, in a burst that carries NO
// `chat` topic at all. This is what the owner opening any OTHER conversation
// fans at this client (GET /api/chat?with= advances a watermark and echoes a
// `chat_read`), so it is an ordinary — and common — recovery channel.
const foreignRead: SseDelta = {
  topic: "chat_read",
  names: { reader: "x", peer: "y" },
  ids: ["x", "y"],
};

let hasFocusSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.peekChat.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.sseHandler = null;
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  hasFocusSpy.mockRestore();
});

// The deltaSink coalesces a burst into ONE microtask, so an emit is only
// observable after the microtask queue drains.
async function emit(delta: SseDelta): Promise<void> {
  await act(async () => {
    h.sseHandler?.(delta.topic, delta);
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useChat: a failed load is marked and paid on the next relevant event", () => {
  it("a load that REJECTS leaves the thread unchanged and fires no retry of its own", async () => {
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    h.listChat.mockRejectedValueOnce(new Error("network"));
    await emit(inThisThread);
    expect(h.listChat).toHaveBeenCalledTimes(2);

    // The mandated shape: MARK, DON'T RETRY. Nothing may fire on a timer or a
    // backoff — the count must stay put until an EVENT arrives.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.listChat).toHaveBeenCalledTimes(2);
  });

  it("after a failed load, a delta about a DIFFERENT conversation still reloads THIS thread", async () => {
    const { result, rerender } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));
    expect(result.current.messages).toEqual([]);

    // 1. the load for our own delta is lost.
    h.listChat.mockRejectedValueOnce(new Error("network"));
    await emit(inThisThread);
    expect(h.listChat).toHaveBeenCalledTimes(2);
    expect(result.current.messages).toEqual([]);

    // 2. a plain re-render happens in the waiting window (StrictMode-ref trap +
    //    "no repaint during the wait" trap — see the header).
    rerender();

    // 3. the next traffic is somebody ELSE's conversation. The ordinary filter
    //    would skip it; the mark must force the missing page through.
    h.listChat.mockResolvedValueOnce([mkMsg("c9", "b", "owner", 2000)]);
    await emit(elsewhere);

    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(3));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["c9"]),
    );
    expect(result.current.messagesPeer).toBe("b");
  });

  it("once the catch-up load lands, the mark is cleared — the next foreign delta is skipped again", async () => {
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    h.listChat.mockRejectedValueOnce(new Error("network"));
    await emit(inThisThread);
    h.listChat.mockResolvedValueOnce([mkMsg("c9", "b", "owner", 2000)]);
    await emit(elsewhere);
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(h.listChat).toHaveBeenCalledTimes(3);

    // The debt is paid. A foreign delta is once again none of our business —
    // this is the guard against "fix" = delete the per-conversation filter,
    // which would resurrect the T-8115 self-drive (a marking listChat fans a
    // chat_read echo straight back at us, once per chat line company-wide).
    await emit(elsewhere);
    expect(h.listChat).toHaveBeenCalledTimes(3);
  });

  it("with NO failed load, a delta about a different conversation never reloads this thread", async () => {
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    await emit(elsewhere);
    expect(h.listChat).toHaveBeenCalledTimes(1);
    expect(h.peekChat).not.toHaveBeenCalled();
  });

  it("a burst carrying ONLY chat_read pays the debt — the mark is not narrowed to chat lines", async () => {
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    // 1. the load for our own delta is lost.
    h.listChat.mockRejectedValueOnce(new Error("network"));
    await emit(inThisThread);
    expect(h.listChat).toHaveBeenCalledTimes(2);
    expect(result.current.messages).toEqual([]);

    // 2. the next burst is a read receipt for SOMEBODY ELSE's conversation and
    //    carries no `chat` topic. The relevance gate at the top of the sink
    //    (CHAT_TOPICS) already admitted it; the mark must then force the missing
    //    page through, because a debt is a debt whichever relevant topic wakes
    //    us. Pinning this because the alternative reading — "only a `chat` burst
    //    may pay" — is a live mutation of the same expression that no other case
    //    in this suite can tell apart.
    h.listChat.mockResolvedValueOnce([mkMsg("c9", "b", "owner", 2000)]);
    await emit(foreignRead);

    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(3));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["c9"]),
    );
  });

  it("a peer switch does not inherit the previous conversation's debt", async () => {
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b" ? [mkMsg("c1", "b", "owner", 1000)] : [],
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(1));

    h.listChat.mockRejectedValueOnce(new Error("network"));
    await emit(inThisThread);

    // Switching peers re-runs the effect, which loads unconditionally — the new
    // conversation starts square. (The mark is reset in the effect's SETUP
    // body, which is also what keeps it alive under StrictMode.)
    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    const afterSwitch = h.listChat.mock.calls.length;

    // "z" owes nothing, so a foreign delta must be skipped, not loaded.
    await emit(elsewhere);
    expect(h.listChat).toHaveBeenCalledTimes(afterSwitch);
  });

  it("the PREVIOUS peer's load rejecting AFTER the switch writes no debt onto the new conversation", async () => {
    // The case above rejects BEFORE the switch, so the successor's setup body
    // clears the mark afterwards and the leak cannot show. Here the rejection
    // lands AFTER that setup body has already run: without an `alive` guard on
    // the catch arm, a dead effect instance's failure marks its SUCCESSOR as
    // owing a page it never lost — and "z" then pays a debt that was "b"'s,
    // firing a load for traffic that is none of its business (exactly the
    // T-8115 self-drive the per-conversation filter exists to prevent).
    let rejectB!: (e: unknown) => void;
    const stuck = new Promise<unknown[]>((_, reject) => {
      rejectB = reject;
    });

    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    // "b" fires a load that neither resolves nor rejects yet.
    h.listChat.mockImplementationOnce(() => stuck);
    await emit(inThisThread);
    expect(h.listChat).toHaveBeenCalledTimes(2);

    // Switch peers while that load is still in flight. "z" loads once, cleanly.
    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    const afterSwitch = h.listChat.mock.calls.length;

    // NOW the dead instance's load fails.
    await act(async () => {
      rejectB(new Error("late network"));
      await Promise.resolve();
      await Promise.resolve();
    });

    // "z" never lost a page, so a foreign delta is still none of its business.
    await emit(elsewhere);
    expect(h.listChat).toHaveBeenCalledTimes(afterSwitch);
    expect(result.current.messagesPeer).toBe("z");
  });
});
