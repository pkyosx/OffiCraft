// useChat — THE HOLE IN THE MIDDLE (T-b0bb). GUARDS, not probes.
//
// WHAT THIS DEFECT COSTS, and why the assertions are shaped the way they are.
// `load()` / `refetch()` fetch `GET /api/chat?with=` with no cursor: the server
// answers with the newest 30 rows of the stream, a SLIDING WINDOW. Reconciling
// that by id-subtraction alone loses everything that fell between two loads
// beyond one window — measured on the pre-fix code to single-message precision:
// 30 new → no hole, 31 → 1 lost, 40 → 10 lost.
//
// 🔴 THE REASON A "no messages are missing" ASSERTION IS NOT ENOUGH. The server
// advances the reader's watermark to the newest ts OF THE PAGE IT SERVED, which
// is PAST the hole. So the lost messages are marked read: unread goes to zero,
// the "以下是未讀" divider does not point at them, nothing throws, and nothing
// renders differently. A THREAD WITH A HOLE AND A COMPLETE THREAD ARE THE SAME
// SHAPE. That is why every test below asserts on CONTINUITY against what the
// server actually holds, computed by diffing the two, rather than on a
// hand-written expected array — a hand-written expectation is exactly the thing
// that cannot tell the two apart.
//
// The `listChat` mock is a SERVER SIMULATOR written from
// server/ocserverd/api_chat.go HandleListChatApiChatGetParams:
//   · no cursor → the whole filtered stream, then `msgs[len-limit:]`
//     (chatListDefaultLimit = 30) — the newest `limit` rows, oldest→newest;
//   · before_ts + before_id → listChatBefore: the `limit` rows strictly OLDER
//     than (ts, id), still oldest→newest;
//   · sending only ONE cursor half is a 422 (the simulator throws, so a caller
//     that forgets the pair fails loudly here too).
// Its fidelity on the two facts that matter is cross-checked against the REAL
// Go handler by server/ocserverd/api_chat_gap_tb0bb_test.go.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage, ChatCursor, SseDelta } from "../api/adapter";

const h = vi.hoisted(() => ({
  listChat:
    vi.fn<
      (w: string, limit?: number, before?: ChatCursor) => Promise<unknown[]>
    >(),
  peekChat: vi.fn<(w: string, limit?: number) => Promise<unknown[]>>(),
  listChatReads: vi.fn(async () => [] as unknown[]),
  markChatRead: vi.fn(async () => ({}) as unknown),
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

const SERVER_DEFAULT_LIMIT = 30; // chatListDefaultLimit, api_chat.go

function mkMsg(id: string, ts: number): ChatMessage {
  return {
    id,
    from: "b",
    to: "owner",
    body: `msg ${id}`,
    ts,
    attachments: [],
    replyCardId: null,
  };
}

/** The whole conversation as the SERVER holds it, oldest→newest. */
let server: ChatMessage[] = [];
/** Every cursor request the hook made, for the "it really paged back" pins. */
let cursorCalls: ChatCursor[] = [];

function serve(limit?: number, before?: ChatCursor): ChatMessage[] {
  const lim = limit ?? SERVER_DEFAULT_LIMIT;
  if (before) {
    cursorCalls.push(before);
    const older = server.filter(
      (m) =>
        m.ts < before.beforeTs ||
        (m.ts === before.beforeTs && m.id < before.beforeId),
    );
    return older.slice(Math.max(0, older.length - lim));
  }
  return server.slice(Math.max(0, server.length - lim));
}

let hasFocusSpy: ReturnType<typeof vi.spyOn>;
let warnSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  server = [];
  cursorCalls = [];
  h.listChat.mockReset().mockImplementation(async (_w, limit, before) => {
    // Mirror the server's 422: the two cursor halves must arrive together.
    if (before && (before.beforeTs === undefined || !before.beforeId)) {
      throw new Error("422 before_ts and before_id must be supplied together");
    }
    return serve(limit, before);
  });
  h.peekChat.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.sseHandler = null;
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  hasFocusSpy.mockRestore();
  warnSpy.mockRestore();
});

const chatDelta: SseDelta = {
  topic: "chat",
  names: { id: "z", from: "b", to: "owner" },
  ids: ["z", "b", "owner"],
};

async function emit(): Promise<void> {
  await act(async () => {
    h.sseHandler?.(chatDelta.topic, chatDelta);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function push(prefix: string, n: number): void {
  const base = server.length === 0 ? 1000 : server[server.length - 1].ts + 1;
  for (let i = 0; i < n; i++) server.push(mkMsg(`${prefix}${i}`, base + i));
}

/** Every server row the thread does NOT hold. THE assertion subject. */
function missingFrom(messages: ChatMessage[]): string[] {
  const have = new Set(messages.map((m) => m.id));
  return server.filter((m) => !have.has(m.id)).map((m) => m.id);
}

describe("useChat: a burst larger than one window leaves NO hole", () => {
  // The three measured cases, by their measured numbers. 30 was already fine
  // before the fix and is kept as the CONTROL: if it ever goes red, the
  // reconciliation broke in the ordinary case, not the seam case.
  for (const n of [30, 31, 40, 95]) {
    it(`a burst of ${n} between two loads: the thread ends up complete`, async () => {
      push("a", 30);
      const { result } = renderHook(() => useChat("b"));
      await waitFor(() => expect(result.current.messages).toHaveLength(30));

      push("x", n);
      await emit();
      await waitFor(() =>
        expect(result.current.messages.length).toBeGreaterThan(30),
      );

      // 🔴 THE LOAD-BEARING LINE. Diffed against what the server holds, not
      // against a hand-written array — the whole defect is that a short thread
      // and a complete one look identical, so the expectation must come from
      // the other side of the wire.
      expect(missingFrom(result.current.messages)).toEqual([]);
      expect(result.current.messages).toHaveLength(server.length);
      // …and no gap was left marked, because none was left.
      expect(result.current.gapSuspected).toBe(false);
    });
  }

  it("CONTINUITY at the seam: the rows either side of the join are adjacent in the server's own order", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    push("x", 40);
    await emit();
    await waitFor(() =>
      expect(result.current.messages.length).toBeGreaterThan(30),
    );

    // A stronger statement than "nothing is missing": the thread, read in
    // order, IS the server's stream in order. This is the assertion that can
    // distinguish "really complete" from "incomplete but marked read", because
    // it never consults the read state at all.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      server.map((m) => m.id),
    );
    // Specifically: the pre-fix code jumped a29 → x10. It must now be a29 → x0.
    const ids = result.current.messages.map((m) => m.id);
    expect(ids[ids.indexOf("a29") + 1]).toBe("x0");
  });

  it("the backfill uses the PAIRED keyset cursor and pages backwards from the newest page's oldest row", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    cursorCalls = [];

    push("x", 40); // newest page will be x10..x39; the seam is x0..x9
    await emit();
    await waitFor(() => expect(missingFrom(result.current.messages)).toEqual([]));

    expect(cursorCalls.length).toBeGreaterThan(0);
    // Both halves, always — a lone half is a 422 server-side.
    for (const c of cursorCalls) {
      expect(typeof c.beforeTs).toBe("number");
      expect(typeof c.beforeId).toBe("string");
      expect(c.beforeId.length).toBeGreaterThan(0);
    }
    // The first cursor is the newest page's OLDEST row (x10), i.e. it pages
    // back INTO the seam — not from messages[0], which sits above it.
    const x10 = server.find((m) => m.id === "x10")!;
    expect(cursorCalls[0]).toEqual({ beforeTs: x10.ts, beforeId: x10.id });
  });

  it("no seam ⇒ no cursor request at all (the fix costs nothing in the ordinary case)", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    cursorCalls = [];

    push("x", 5); // well inside one window — the page overlaps what we hold
    await emit();
    await waitFor(() => expect(result.current.messages).toHaveLength(35));

    expect(cursorCalls).toEqual([]);
    expect(missingFrom(result.current.messages)).toEqual([]);
    expect(result.current.gapSuspected).toBe(false);
  });
});

describe("useChat: giving up on a seam is NEVER silent", () => {
  it("a seam too large for the backfill budget raises gapSuspected", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    // MAX_BACKFILL_PAGES(6) × CHAT_BACKFILL_PAGE_SIZE(100) = 600 rows of reach.
    // A burst of 900 leaves a seam the walk cannot close.
    push("x", 900);
    await emit();
    await waitFor(() => expect(result.current.gapSuspected).toBe(true));

    // The honest pair: we admit the gap AND we still say what is missing is
    // unknown — the thread is not silently presented as complete.
    expect(missingFrom(result.current.messages).length).toBeGreaterThan(0);
  });

  it("a FAILING backfill request raises gapSuspected and still adopts the newest page", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    push("x", 40);
    // The newest page lands; every cursor page blows up.
    h.listChat.mockImplementation(async (_w, limit, before) => {
      if (before) throw new Error("backfill boom");
      return serve(limit);
    });
    await emit();
    await waitFor(() => expect(result.current.gapSuspected).toBe(true));

    // The newest page was NOT thrown away with the failed backfill — a marked
    // hole beats a stale thread.
    expect(result.current.messages.some((m) => m.id === "x39")).toBe(true);
  });

  it("gapSuspected is STICKY: a later clean load does not un-lose the messages", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    push("x", 900);
    await emit();
    await waitFor(() => expect(result.current.gapSuspected).toBe(true));

    // An ordinary, perfectly-joining load afterwards.
    push("y", 2);
    await emit();
    await waitFor(() =>
      expect(result.current.messages.some((m) => m.id === "y1")).toBe(true),
    );
    expect(result.current.gapSuspected).toBe(true);
  });

  it("switching peers clears the flag (it describes ONE conversation)", async () => {
    push("a", 30);
    const { result, rerender } = renderHook(({ p }) => useChat(p), {
      initialProps: { p: "b" },
    });
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    push("x", 900);
    await emit();
    await waitFor(() => expect(result.current.gapSuspected).toBe(true));

    server = [];
    rerender({ p: "c" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("c"));
    expect(result.current.gapSuspected).toBe(false);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// MULTI-PAGE BACKFILL (review B1). Everything above this line feeds the walk a
// seam that ONE page closes (31→1 missing, 40→10, 95→65 — all ≤ 100, the
// backfill page size), so the while-loop's own machinery — unshifting each page
// into the right place, advancing the cursor, the 6-page budget — is never
// exercised by an assertion. Measured: with those tests alone, reversing the
// page assembly, freezing the cursor, and cutting the budget to 1 page are ALL
// invisible (21/21 green). A burst of 250 leaves a 220-row seam ⇒ 3 cursor
// pages, which is the smallest case where each of those three is load-bearing.
describe("useChat: a seam that takes MORE THAN ONE backfill page", () => {
  it("a burst of 250 (a 220-row seam, 3 cursor pages): the thread IS the server's stream, in the server's order", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    push("x", 250); // newest window = x220..x249; seam = x0..x219
    await emit();
    // Wait for the COMMIT, not for the answer we want: the walk commits exactly
    // once, whether it closed the seam or gave up, so waiting on "more than the
    // 30 we started with" lets a broken walk reach the assertions below instead
    // of dying in a waitFor timeout that says nothing about what went wrong.
    await waitFor(() =>
      expect(result.current.messages.length).toBeGreaterThan(30),
    );

    // 🔴 ORDER, not just membership. "Nothing is missing" cannot see a walk
    // that collects every row and assembles the pages backwards: the count and
    // the id-set are identical either way, and ChatArea's groupMessages "only
    // partitions, never reorders", so this array IS what the owner reads.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      server.map((m) => m.id),
    );
    expect(missingFrom(result.current.messages)).toEqual([]);
    expect(result.current.gapSuspected).toBe(false);
  });

  it("the walk really ADVANCES: a 220-row seam takes several cursor requests and each one is strictly older than the last", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    cursorCalls = [];

    push("x", 250);
    await emit();
    await waitFor(() =>
      expect(result.current.messages.length).toBeGreaterThan(30),
    );

    // More than one page, or the seam was never multi-page and this whole
    // describe block is testing nothing.
    expect(cursorCalls.length).toBeGreaterThan(1);
    // And the cursor moved BACKWARDS every time. A walk that re-sends the same
    // cursor burns the whole budget on one page and then gives up — 120 rows
    // lost behind a warning — while every count-based assertion stays green.
    for (let i = 1; i < cursorCalls.length; i++) {
      const prev = cursorCalls[i - 1];
      const cur = cursorCalls[i];
      const strictlyOlder =
        cur.beforeTs < prev.beforeTs ||
        (cur.beforeTs === prev.beforeTs && cur.beforeId < prev.beforeId);
      expect({ i, cur, prev, strictlyOlder }).toEqual({
        i,
        cur,
        prev,
        strictlyOlder: true,
      });
    }
    // The budget was enough for this seam: it closed without admitting a gap.
    expect(cursorCalls.length).toBeLessThanOrEqual(6);
    expect(result.current.gapSuspected).toBe(false);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// OVERLAPPING LOADS (review B2). The backfill turned a load from "fetch, then
// setThread with nothing in between" into "fetch, up to 6 round-trips, THEN
// setThread" — and it opens that window only during a burst, i.e. exactly when
// the peer is still talking and another load is most likely to start and finish
// inside it. Measured on the unguarded code: 75 rows, none missing, none
// duplicated, and the newest 5 rendered at the TOP of the conversation.
describe("useChat: a load that started EARLIER may not commit over one that already landed", () => {
  it("🔴 a stalled backfill does not reorder the thread when a newer load lands first", async () => {
    push("a", 30);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    // Hold the FIRST cursor request open: load A is now parked mid-backfill.
    let releaseA: (() => void) | null = null;
    let sawFirstCursor = false;
    const aParked = new Promise<void>((res) => {
      releaseA = res;
    });
    h.listChat.mockImplementation(async (_w, limit, before) => {
      if (before && !sawFirstCursor) {
        sawFirstCursor = true;
        await aParked;
      }
      return serve(limit, before);
    });

    push("x", 40); // seam of 10 for load A
    await emit(); // load A: newest page landed, backfill blocked
    await waitFor(() => expect(sawFirstCursor).toBe(true));

    push("y", 5); // the peer keeps talking while A is parked
    await emit(); // load B: fetches, backfills, COMMITS a complete thread
    await waitFor(() =>
      expect(result.current.messages.some((m) => m.id === "y4")).toBe(true),
    );

    // Now let the older load finish. It carries a newest page (x10..x39) that
    // is a PREFIX of what B already committed, plus its own backfill.
    await act(async () => {
      releaseA?.();
      await new Promise((r) => setTimeout(r, 0));
      await new Promise((r) => setTimeout(r, 0));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    // 🔴 THE NAMED ASSERTION. Membership is NOT the subject here — the
    // unguarded code loses nothing and duplicates nothing. Order is.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      server.map((m) => m.id),
    );
    expect(missingFrom(result.current.messages)).toEqual([]);
    // Concretely: the newest rows are last, not first.
    const ids = result.current.messages.map((m) => m.id);
    expect(ids[ids.length - 1]).toBe("y4");
    expect(ids[0]).toBe("a0");
  });
});
