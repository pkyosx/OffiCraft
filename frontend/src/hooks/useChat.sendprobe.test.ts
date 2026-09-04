// PROBE — NOT a guard. Read-only recon for t-b0bb04a499f5 step 2, item 3:
// what the OWNER ACTUALLY SEES when POST /api/chat succeeds and the GET that
// follows it fails, on TODAY's main.
//
// Nothing here is a desired behaviour; every assertion records what happens now.
//
// ⚠️ OWNER RULING 2026-08-31: this behaviour STAYS. The optimistic-append fix
// (adopt postChat's return value) was scoped out — owner chose to fix only the
// "a whole stretch of messages goes missing" defect. So the assertions below
// are not a wish list; they record a deliberately accepted trade, and a future
// reader should not "repair" them without a new ruling.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage, SseDelta } from "../api/adapter";

const h = vi.hoisted(() => ({
  listChat: vi.fn<(w: string, limit?: number) => Promise<unknown[]>>(),
  listChatReads: vi.fn(async () => [] as unknown[]),
  markChatRead: vi.fn(async () => ({}) as unknown),
  postChat: vi.fn<(m: unknown) => Promise<unknown>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listChat: h.listChat,
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
  return { id, from, to, body: `msg ${id}`, ts, attachments: [], replyCardId: null };
}

/** What the server hands back from POST /api/chat: the stored message. */
const SENT = mkMsg("sent-1", "owner", "b", 2000);

let hasFocusSpy: ReturnType<typeof vi.spyOn>;
let warnSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.postChat.mockReset().mockResolvedValue(SENT);
  h.listChatReads.mockClear();
  h.sseHandler = null;
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  hasFocusSpy.mockRestore();
  warnSpy.mockRestore();
});

async function emit(delta: SseDelta): Promise<void> {
  await act(async () => {
    h.sseHandler?.(delta.topic, delta);
    await Promise.resolve();
    await Promise.resolve();
  });
}

const inThisThread: SseDelta = {
  topic: "chat",
  names: { id: "r1", from: "b", to: "owner" },
  ids: ["r1", "b", "owner"],
};
const elsewhere: SseDelta = {
  topic: "chat",
  names: { id: "m2", from: "x", to: "y" },
  ids: ["m2", "x", "y"],
};

describe("PROBE t-b0bb: POST succeeds, the follow-up GET fails", () => {
  it("the sent message is NOT on screen, and send() does NOT reject", async () => {
    h.listChat.mockResolvedValueOnce([mkMsg("old-1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));

    // The post-send GET blows up.
    h.listChat.mockRejectedValueOnce(new Error("boom"));
    let rejected = false;
    await act(async () => {
      await result.current.send("hello").catch(() => {
        rejected = true;
      });
    });

    // eslint-disable-next-line no-console
    console.log(
      `[PROBE] postChat returned id=${SENT.id}; send() rejected=${rejected}; ` +
        `messages on screen after send = ${JSON.stringify(result.current.messages.map((m) => m.id))}`,
    );
    expect(h.postChat).toHaveBeenCalledTimes(1);
    expect(rejected).toBe(false); // send() swallows the refetch failure today
    expect(result.current.messages.map((m) => m.id)).toEqual(["old-1"]); // the sent line is invisible
  });

  it("a chat delta about ANOTHER conversation does not fill it in", async () => {
    h.listChat.mockResolvedValueOnce([mkMsg("old-1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));

    h.listChat.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      await result.current.send("hello");
    });
    const callsAfterSend = h.listChat.mock.calls.length;

    h.listChat.mockResolvedValue([mkMsg("old-1", "b", "owner", 1000), SENT]);
    await emit(elsewhere);
    // eslint-disable-next-line no-console
    console.log(
      `[PROBE] after a delta about x↔y: listChat calls ${callsAfterSend} -> ${h.listChat.mock.calls.length}; ` +
        `messages = ${JSON.stringify(result.current.messages.map((m) => m.id))}`,
    );
    expect(h.listChat.mock.calls.length).toBe(callsAfterSend); // no load fired
    expect(result.current.messages.map((m) => m.id)).toEqual(["old-1"]);
  });

  it("it appears only when a delta about THIS thread arrives — i.e. the agent's reply", async () => {
    h.listChat.mockResolvedValueOnce([mkMsg("old-1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));

    h.listChat.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      await result.current.send("hello");
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(["old-1"]);

    const reply = mkMsg("r1", "b", "owner", 3000);
    h.listChat.mockResolvedValue([mkMsg("old-1", "b", "owner", 1000), SENT, reply]);
    await emit(inThisThread);
    // eslint-disable-next-line no-console
    console.log(
      `[PROBE] after the agent's own reply arrives: messages = ` +
        `${JSON.stringify(result.current.messages.map((m) => m.id))}`,
    );
    // The owner's own line and the agent's reply appear IN THE SAME PAINT.
    expect(result.current.messages.map((m) => m.id)).toEqual(["old-1", "sent-1", "r1"]);
  });

  it("CONTROL: when the POST itself fails, send() DOES reject", async () => {
    h.listChat.mockResolvedValueOnce([]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalled());
    h.postChat.mockRejectedValueOnce(new Error("post boom"));
    let rejected = false;
    await act(async () => {
      await result.current.send("hello").catch(() => {
        rejected = true;
      });
    });
    // eslint-disable-next-line no-console
    console.log(`[PROBE] CONTROL post-failure: send() rejected=${rejected}`);
    expect(rejected).toBe(true);
  });
});
