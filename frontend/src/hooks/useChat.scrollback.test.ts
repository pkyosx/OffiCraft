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
//   5. T-48 ③ — the ANCHOR WINDOW, the same seam walked in the other
//      direction: loadAround() opens a window around one message id
//      (`?end_id=` for the context above, `?start_id=` for the context below),
//      loadAround() then fetches FORWARD to the live tail before it commits, and while the
//      thread is such a window the ordinary newest-page refresh is suppressed
//      (merging the live tail onto a historical window would draw an unfetched
//      range as contiguous). resetToLatest() is the way back.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatCursor, ChatMessage } from "../api/adapter";

const h = vi.hoisted(() => {
  return {
    listChatWindow:
      vi.fn<
        (
          withId: string,
          anchor: { startId?: string; endId?: string },
          limit: number,
        ) => Promise<unknown[]>
      >(),
    listChat:
      vi.fn<
        (
          withId: string,
          limit?: number,
          before?: ChatCursor,
        ) => Promise<unknown[]>
      >(),
    listChatReads: vi.fn(async () => [] as unknown[]),
    markChatRead: vi.fn(async () => ({
      readerId: "owner",
      peerId: "b",
      lastReadTs: 1,
    })),
    postChat: vi.fn(async () => ({}) as unknown),
    getReplyCard: vi.fn(async (id: string) => ({ id }) as unknown),
    sseHandler: null as ((topic: string) => void) | null,
  };
});

vi.mock("../api", () => ({
  api: {
    listChat: h.listChat,
    listChatWindow: h.listChatWindow,
    listChatReads: h.listChatReads,
    markChatRead: h.markChatRead,
    postChat: h.postChat,
    getReplyCard: h.getReplyCard,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useChat } from "./useChat";
import type { JumpOutcome } from "./useChat";
import { ApiError } from "../api/errors";
/** A promise the test resolves by hand — the only way to hold a request in
 * flight and land something else on top of it, which is what every race below
 * is about. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/** Let the microtask queue (and the SSE sink's own tick) drain. */
const settle = () => new Promise((r) => setTimeout(r, 20));

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

/** A row carrying a WAITING 請示卡 — the only kind that grows when its card
 * lands, and therefore the only kind a commit must hold before painting. */
function mkCardMsg(id: string, cardId: string, ts: number): ChatMessage {
  return {
    ...mkMsg(id, "b", "owner", ts),
    replyCardId: cardId,
    replyCardStatus: "waiting",
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
  h.listChatWindow.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.markChatRead.mockClear();
  h.getReplyCard.mockClear();
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

  it("切走再切回同一個人,上一趟還在飛的往上捲頁不准接到這一趟的線頭上", async () => {
    // 🔴 第六輪 R6-1,同一個根。往上捲的游標取自**上一趟**手上那條線的最舊一則;
    // 這一趟手上的可能是完全另一段(最極端的就是帶錨點進來的那個視窗)。
    //
    // 🔴 A→B→A 是三次 mount(R13-5):`ChatArea` 掛在 `key={peerId}` 底下,所以
    // 下面用 unmount／mount 驅動。上一趟那頁落地時,它要寫的 component 已經被
    // React 丟掉了 —— 這條斷言守的是「thread 不共用」,共用回去就會紅。
    const hung = deferred<ChatMessage[]>();
    h.listChat.mockResolvedValueOnce(page("n", 1000, 30));
    const trip1 = renderHook(() => useChat("a"));
    await waitFor(() => expect(trip1.result.current.messages).toHaveLength(30));

    h.listChat.mockReturnValueOnce(hung.promise);
    let older!: Promise<void>;
    act(() => {
      older = trip1.result.current.loadOlder();
    });

    h.listChat.mockResolvedValue(page("b", 500, 5));
    trip1.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });
    h.listChat.mockResolvedValue(page("z", 9000, 5));
    inB.unmount();
    const trip2 = renderHook(() => useChat("a"));
    await act(async () => {
      await settle();
    });
    const nowShowing = trip2.result.current.messages.map((m) => m.id);

    await act(async () => {
      hung.resolve(page("o", 1, 5));
      await older;
      await settle();
    });
    expect(
      trip2.result.current.messages.map((m) => m.id),
      "上一趟的往上捲頁不准接到這一趟的線頭上",
    ).toEqual(nowShowing);
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
    // A switch is a REMOUNT (T-48, R13-5) — `ChatArea` is rendered under
    // `key={peerId}` — so the new room derives `hasMore` from its own first page.
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b" ? page("n", 1000, 30) : [mkMsg("z1", "z", "owner", 1)],
    );
    const inB = renderHook(() => useChat("b"));
    await waitFor(() => expect(inB.result.current.messages).toHaveLength(30));
    expect(inB.result.current.hasMore).toBe(true);
    inB.unmount();

    const inZ = renderHook(() => useChat("z"));
    await waitFor(() =>
      expect(inZ.result.current.messages.map((m) => m.id)).toEqual(["z1"]),
    );
    expect(inZ.result.current.hasMore).toBe(false);
  });
});

describe("useChat anchor window (loadAround / resetToLatest)", () => {
  // 🔴 THE DEFECT THIS CLOSES. The thread only ever held the newest window, so
  // 跳到原訊息 could reach a message only if it happened to be in it. There was
  // no forward cursor at all — `before_ts`/`before_id` walks one way — so the
  // cockpit looked in the DOM, missed, and scrolled to the bottom.
  it("loadAround opens ONE window around the id — two requests, one each way, never the whole history", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const above = page("a", 100, 30); // full → history may continue above
    const below = [
      mkMsg("a29", "b", "owner", 129),
      mkMsg("t1", "b", "owner", 130),
    ]; // SHORT (< CHAT_WALK_PAGE_SIZE) → the live tail is inside this page
    h.listChatWindow.mockResolvedValueOnce(above).mockResolvedValueOnce(below);

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("a29");
    });

    expect(outcome).toBe("found");
    // Both ends INCLUSIVE and both anchored on the SAME id: the context above
    // it and the context below it.
    expect(h.listChatWindow.mock.calls[0]).toEqual(["b", { endId: "a29" }, 30]);
    // 🔴 THE FORWARD HALF IS THE FIRST PAGE OF THE FETCH TO THE TAIL, so it is
    // asked at the walk's page size (100), not the history one (T-48 fix12).
    expect(h.listChatWindow.mock.calls[1]).toEqual([
      "b",
      { startId: "a29" },
      100,
    ]);
    expect(h.listChatWindow).toHaveBeenCalledTimes(2);
    // The target really is in the thread — that is the whole promise.
    expect(result.current.messages.map((m) => m.id)).toContain("a29");
    // The two pages are ONE window, de-duplicated on the shared anchor.
    expect(result.current.messages).toHaveLength(31);
    expect(new Set(result.current.messages.map((m) => m.id)).size).toBe(31);
    // A full page above ⇒ more history may exist; a short page below ⇒ this
    // window already reaches the live tail.
    expect(result.current.hasMore).toBe(true);
    expect(result.current.hasNewer).toBe(false);
  });

  it("跳轉一路撈到活尾巴 —— 每通 100 則,撈到回不滿 100 為止,而且只 render 一次", async () => {
    // 🔴 這張票的正題(owner rc-e1fb80065f8f「一次撈100則撈完」＋ c-6a973512ed77
    // 「我是指整個訊息撈完才 render」)。從錨點到活尾巴之間 250 則:
    //   往新①滿 100 → 往新②滿 100 → 往新③回 52(不滿)⇒ 停。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("hist", 100, 30)) // end_id:上面那一頁
      // 每一頁的第一列都是**上一頁的最後一列**(server 的錨點是含端點的),
      // 所以這裡也順便釘住「重複的那一列不准被貼成兩列」。
      .mockResolvedValueOnce(page("w", 200, 100)) // start_id:第一頁,滿
      .mockResolvedValueOnce([
        mkMsg("w99", "b", "owner", 299),
        ...page("x", 300, 99),
      ]) // 第二頁,滿
      .mockResolvedValueOnce([
        mkMsg("x98", "b", "owner", 398),
        ...page("y", 399, 51),
      ]); // 第三頁,不滿 ⇒ 到尾巴

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("w0");
    });

    expect(outcome).toBe("found");
    // 三通往新,每一通都問 100。
    const forward = h.listChatWindow.mock.calls.filter(
      (c) => (c[1] as { startId?: string }).startId !== undefined,
    );
    expect(forward.map((c) => c[2]), "每一通往新都要問 100 則").toEqual([
      100, 100, 100,
    ]);
    // 每一頁都從**上一頁的最後一列**接下去,不是從同一個錨點重複問。
    expect(forward.map((c) => (c[1] as { startId: string }).startId)).toEqual([
      "w0",
      "w99",
      "x98",
    ]);
    // 30(上面) + 100 + 99 + 51 = 280,兩個重複的錨點列各只算一次。
    expect(result.current.messages).toHaveLength(280);
    expect(new Set(result.current.messages.map((m) => m.id)).size).toBe(280);
    // 接上活尾巴了。
    expect(result.current.hasNewer).toBe(false);
    // ⚠️ 「撈完才 render」這一半**不在這裡量**,而且不是疏忽:React 18 在
    // `act()` 裡把所有更新批次掉,中間那次 commit 根本不會產生一次 render,所以
    // jsdom 對「一次 commit」與「一頁一頁 commit」給出一模一樣的答案(實測:把
    // 錨點窗改成先 commit 再重新領票走訪,這條照樣全綠)。那一格由真瀏覽器的
    // `visual-guards/chat-thread-loading.ct.spec.tsx` 用畫面上的列數取樣釘住。
  });

  it("撈到一半失敗:已經撈到的照樣落地,而且誠實地說「還沒到最新」", async () => {
    // 🔴 一次 commit 的代價,以及它的收法(coordinator 指名要寫明的那一格)。
    // 一頁一頁貼的時候,失敗就停在半路、畫面上還看得到東西;改成一次 commit
    // 之後,「不 commit」＝讀的人盯著空房間。所以失敗**照樣 commit 手上有的**,
    // 並把 hasNewer 留成 true —— 箭頭因此留在畫面上,已讀水位也不會被蓋。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("hist", 100, 30))
      .mockResolvedValueOnce(page("w", 200, 100))
      .mockRejectedValueOnce(new Error("boom"));

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("w0");
    });

    // 不是 "unreachable":目標**找到了**,只是後面沒撈完。
    expect(outcome).toBe("found");
    expect(result.current.messages.map((m) => m.id)).toContain("w0");
    expect(result.current.messages).toHaveLength(130);
    // 🔴 這一行是這條測試的心臟:畫面必須知道自己不在最新。把 loadAround 改成
    // `hasNewer: false`(或改成失敗就不 commit),紅的就是這裡。
    expect(result.current.hasNewer).toBe(true);
  });

  it("滿頁卻一列新的都沒有時,走訪停下來而不是永遠問同一個問題", async () => {
    // 🔴 迴圈的終止證明。滿頁 ⇒ 「還有更多」,但一列都沒新增 ⇒ 錨點沒有前進 ⇒
    // 下一通會問一模一樣的問題。沒有這一道界,這就是一個對著網路跑的忙迴圈。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const stuck = page("w", 200, 100);
    h.listChatWindow
      .mockResolvedValueOnce(page("hist", 100, 30))
      .mockResolvedValueOnce(stuck)
      // 之後每一通都回同一頁 —— 一列新的都沒有。
      .mockResolvedValue(stuck);

    await act(async () => {
      await result.current.loadAround("w0");
    });

    const forward = h.listChatWindow.mock.calls.filter(
      (c) => (c[1] as { startId?: string }).startId !== undefined,
    );
    // 第一通帶回 100 列,第二通一列都沒有 ⇒ 停。不准有第三通。
    expect(forward.length, "滿頁但沒有進展時走訪沒有停下來").toBe(2);
    // 停下來是誠實的:沒有假裝接上尾巴。
    expect(result.current.hasNewer).toBe(true);
  });

  it("走訪途中離開房間:迴圈就地停下,一頁都不再買", async () => {
    // 🔴 review25 F1 的一半。`fetchToLatest` 是一個對著網路跑的 `for(;;)`,而在
    // 這之前它的閉包裡沒有任何東西停得下它:讀的人離開房間之後,那個迴圈照樣一頁
    // 一頁打到走完為止,而且結果會落到一個已經不存在的畫面上。
    //
    // ⚠️ 這條量的是**取消**,不是**界**。走訪要不要走到 N 則就停是另一張還沒裁的
    // 卡(rc-e6b1d822def1);不要把這條測試改成在數頁數。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result, unmount } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const held = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockResolvedValueOnce(page("hist", 100, 30)) // end_id
      .mockResolvedValueOnce(page("w", 200, 100)) // start_id,滿 ⇒ 要走訪
      .mockReturnValueOnce(held.promise) // 走訪第一頁 —— 卡在半空中
      // 之後還有得走:沒有取消的話,離開房間之後這兩頁照樣會被買下來。
      .mockResolvedValueOnce([
        mkMsg("x99", "b", "owner", 399),
        ...page("y", 400, 99),
      ])
      .mockResolvedValueOnce(page("z", 500, 20));

    let pending!: Promise<JumpOutcome>;
    await act(async () => {
      pending = result.current.loadAround("w0");
      await settle();
    });
    const forward = () =>
      h.listChatWindow.mock.calls.filter(
        (c) => (c[1] as { startId?: string }).startId !== undefined,
      ).length;
    expect(forward(), "前提:走訪的第一頁真的在飛").toBe(2);

    await act(async () => {
      unmount();
      held.resolve(page("x", 300, 100)); // 滿頁 ⇒ 沒有取消就會續買下一頁
      await settle();
    });

    expect(
      await pending,
      "房間都不在了,這一趟只能是 cancelled —— 不是 found,也不是 missing",
    ).toBe("cancelled");
    expect(
      forward(),
      "讀的人已經離開,走訪不准再買任何一頁",
    ).toBe(2);
  });

  it("新的跳轉取代舊的:舊的走訪就地停下,而且它的結果不准落地", async () => {
    // 🔴 review25 F1 的另一半,也是同一間房裡真的會發生的那一種。第二次「跳到原
    // 訊息」不會 remount(`OfficePage` 的 key 是 peerId),所以舊的那一趟就在同一
    // 個 hook 實例裡繼續跑 —— 停不下來的話,兩趟走訪同時對著網路買頁,而先回來的
    // 那一趟還會把讀的人已經放棄的那一段貼上去。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const held = deferred<ChatMessage[]>();
    /** 舊那一趟在被取消之後**還想再買**的頁 —— 必須永遠是空的。 */
    const afterCancel: string[] = [];
    h.listChatWindow.mockImplementation(
      async (_withId, anchor: { startId?: string; endId?: string }) => {
        if (anchor.endId === "w0") return page("hist", 100, 30);
        if (anchor.startId === "w0") return page("w", 200, 100); // 滿 ⇒ 要走訪
        if (anchor.startId === "w99") return held.promise; // 舊那一趟卡在這裡
        if (anchor.endId === "q0") return page("qhist", 5000, 30);
        if (anchor.startId === "q0") return page("q", 5100, 10); // 短頁 ⇒ 到尾巴
        afterCancel.push(anchor.startId ?? anchor.endId ?? "?");
        return page("later", 6000, 100);
      },
    );

    let first!: Promise<JumpOutcome>;
    await act(async () => {
      first = result.current.loadAround("w0");
      await settle();
    });

    // 讀的人改主意,跳去別的一則 —— 房間沒換,只換了目標。
    let second: JumpOutcome | undefined;
    await act(async () => {
      second = await result.current.loadAround("q0");
    });
    expect(second, "前提:新的那一趟自己要成功").toBe("found");

    await act(async () => {
      held.resolve([mkMsg("w99", "b", "owner", 299), ...page("x", 300, 99)]);
      await settle();
    });

    expect(
      await first,
      "舊的那一趟被新的取代 —— 這不是 superseded(那會叫 caller 再排一次)",
    ).toBe("cancelled");
    expect(
      afterCancel,
      "被取代之後,舊的走訪不准再買任何一頁",
    ).toEqual([]);
    // 🔴 而且它手上那半條線一列都不准落地:畫面上是新的那一趟的窗。
    const ids = result.current.messages.map((m) => m.id);
    expect(ids, "落地的必須是新的那一趟").toContain("q0");
    expect(ids, "舊的那一趟的結果不准 commit").not.toContain("w0");
  });

  it("a full page below means the live tail is BEYOND the window — and the newest-page refresh is suppressed while it is", async () => {
    // Without the suppression an SSE burst fetches the newest 30 rows and
    // merges them onto a window from the distant past: the unfetched range
    // between the two is drawn as contiguous, and the seam machinery reports
    // it as LOST messages.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    // 🔴 fix12 之後,「這條線還沒接上活尾巴」只有一種成因:往新那一頁滿了(走訪
    // 要繼續)而走訪的下一通沒能完成。這裡讓它失敗 —— 而失敗**照樣 commit**,
    // 因為讓讀的人看到空房間不是一個選項(見 loadAround 的 commit 註解)。
    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 100))
      .mockRejectedValueOnce(new Error("walk page failed"));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(h.listChat.mock.calls.length).toBe(before);
  });



  it("resetToLatest's replacement page carrying a WAITING card lands without reading the card", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      // 🔴 錨點窗要停在「還沒接上活尾巴」,而 fix12 之後那只有一種成因:往新那
      // 一頁滿了(⇒ 走訪要繼續)而走訪的下一通失敗。這就是造它的方法。
      .mockResolvedValueOnce(page("a", 129, 100))
      .mockRejectedValueOnce(new Error("walk page failed"));
    await act(async () => {
      await result.current.loadAround("a0");
    });

    h.listChat.mockResolvedValueOnce([
      ...page("z", 9000, 29),
      mkCardMsg("z-card", "rc-latest", 9100),
    ]);
    await act(async () => {
      await result.current.resetToLatest();
    });

    expect(result.current.messages.map((m) => m.id)).toContain("z-card");
    expect(
      h.getReplyCard,
      "the replacement page read its waiting card — the loader is prefetching again",
    ).not.toHaveBeenCalled();
  });








  it("走訪還在飛的時候往上捲,兩邊的頁都不掉 —— 這是這次改動唯一的新交互", async () => {
    // 🔴 唯一的新交互(fix10 §4 自己點名的那一格)。`loadOlder` 不領世代票、
    // 用 updater 寫;`loadAround` 的走訪領一張票、最後才一次 commit。走訪的
    // await 窗現在**長得多**(每 100 列一趟往返),所以「往上捲的那一頁落在走訪
    // 的窗裡」從罕見變成常態。
    //
    // 兩件事都要成立:走訪那一次 commit 不准把窗內落地的歷史頁吃掉(它是用
    // updater 對 React 的 `prev` 算的),歷史頁也不准把走訪的結果推掉。
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    // 錨點窗:上面滿頁,下面**滿 100** ⇒ 走訪要繼續。
    const below = deferred<ChatMessage[]>();
    const walk = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockReturnValueOnce(below.promise)
      .mockReturnValueOnce(walk.promise);

    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });
    await act(async () => {
      below.resolve(page("a", 129, 100));
      await settle();
    });

    // 走訪那一頁還吊在空中 —— 這時房間還是空的(撈完才 render),所以往上捲
    // 沒東西可捲。讓走訪先落地,再驗第二半。
    await act(async () => {
      walk.resolve(page("t", 229, 3));
      await pending;
      await settle();
    });
    const afterJump = result.current.messages.length;
    expect(result.current.hasNewer, "走訪走到短頁 ⇒ 已經接上活尾巴").toBe(false);

    // 現在往上捲一頁,而且讓它在一個**還在飛的**最新頁刷新中間落地。
    h.listChat.mockResolvedValueOnce(page("h", 1, 30));
    await act(async () => {
      await result.current.loadOlder();
      await settle();
    });
    expect(result.current.messages).toHaveLength(afterJump + 30);
    expect(result.current.messages[0].id).toBe("h0");
    // 尾巴還是走訪帶回來的那一列 —— 往上捲沒有動到尾巴。
    expect(result.current.messages[result.current.messages.length - 1].id).toBe(
      "t2",
    );
  });




  it("a FAILED READ is not a missing message —— 404/422 是不見了,其他是現在讀不到", async () => {
    // 🔴 兩個方向一起釘,而且必須一起:只釘一邊的話,「把兩種壓成同一句」照樣會過,
    // 而那正是這條要修的病。owner 開這張票的理由是「不要對使用者說不成立的話」;
    // 對一則躺在 502 後面的訊息說「可能已經被清掉了」,會讓他不再試 —— 那句話本身
    // 就是產品。
    h.listChat.mockResolvedValue(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const outcomeFor = async (err: unknown) => {
      h.listChatWindow.mockReset().mockRejectedValue(err);
      let out: JumpOutcome | undefined;
      await act(async () => {
        out = await result.current.loadAround("c-x");
      });
      return out;
    };

    // 伺服器說了「這條線沒有這一列」/「這個 id 不能用」—— 再試一次不會有別的答案。
    expect(await outcomeFor(new ApiError("http 404", 404, "", ""))).toBe(
      "missing",
    );
    expect(await outcomeFor(new ApiError("http 422", 422, "", ""))).toBe(
      "missing",
    );
    // 讀取失敗 —— 訊息八成就在那裡,再試一次是對的下一步。
    expect(await outcomeFor(new ApiError("http 502", 502, "", ""))).toBe(
      "unreachable",
    );
    expect(await outcomeFor(new ApiError("http 429", 429, "", ""))).toBe(
      "unreachable",
    );
    // 連線根本沒回答:fetch 自己 reject,沒有 status 可言。
    expect(await outcomeFor(new TypeError("Failed to fetch"))).toBe(
      "unreachable",
    );
    // 不管哪一種,讀者原本在看的那條 thread 都不准被動到。
    expect(result.current.messages).toHaveLength(30);
  });

  it("an id no message carries is reported as NOT FOUND and leaves the thread alone", async () => {
    // The server answers 404 rather than an empty page precisely so this stays
    // distinguishable from "a real window that happens to be empty".
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow.mockRejectedValue(new ApiError("http 404", 404, "", ""));
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("c-nope");
    });

    expect(outcome).toBe("missing");
    expect(result.current.messages).toHaveLength(30);
    expect(result.current.hasNewer).toBe(false);
  });

  it("resetToLatest REPLACES the anchor window with the live newest one", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      // 🔴 錨點窗要停在「還沒接上活尾巴」,而 fix12 之後那只有一種成因:往新那
      // 一頁滿了(⇒ 走訪要繼續)而走訪的下一通失敗。這就是造它的方法。
      .mockResolvedValueOnce(page("a", 129, 100))
      .mockRejectedValueOnce(new Error("walk page failed"));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });

    // REPLACED, not concatenated: the range between the historical window and
    // the live tail was never fetched, and a thread that draws it as contiguous
    // is the lie this whole seam exists to avoid.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // ANCHOR-FIRST ENTRY (T-48, owner ruling). Arriving through a jump link no
  // longer loads the live tail and then throws it away.
  // ───────────────────────────────────────────────────────────────────────────

  it("entering AT an anchor fetches the window around it and never a newest page first", async () => {
    // 🔴 The measured defect: entry fired `GET /api/chat?with=` and the anchor
    // window replaced it tens of ms later. One wasted round-trip, and a real
    // intermediate screen showing the live tail to a reader on their way
    // somewhere else — the screen every mark-read patch downstream exists to
    // hold back.
    const above = page("a", 100, 30); // full → history continues above
    const below = [mkMsg("a29", "b", "owner", 129), mkMsg("t1", "b", "owner", 130)];
    h.listChatWindow.mockResolvedValueOnce(above).mockResolvedValueOnce(below);

    const { result } = renderHook(() => useChat("b", "a29"));
    // The subscription is up (receipts were pulled) and yet NOTHING asked for
    // the newest page.
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    expect(h.listChat).not.toHaveBeenCalled();

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("a29");
    });
    expect(outcome).toBe("found");
    expect(result.current.messages.map((m) => m.id)).toContain("a29");
    // Still not one newest page: the FIRST request this room made was the
    // window around the target, and it was the only kind it needed.
    expect(h.listChat).not.toHaveBeenCalled();

    // …and the hold-off is released the moment the anchor lands, or the room
    // would never refresh again (this window already reaches the live tail).
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).toHaveBeenCalledTimes(1);
  });

  it("entering WITHOUT an anchor is the ordinary entry, unchanged: one newest page and no window request", async () => {
    // The other half of the ruling, and the one worth a guard of its own: the
    // anchor-first path must be reachable ONLY from a jump. Every ordinary
    // entry — the overwhelming majority — has to be byte-for-byte what it was.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    expect(h.listChat).toHaveBeenCalledTimes(1);
    expect(h.listChat.mock.calls[0]).toEqual(["b"]);
    expect(h.listChatWindow).not.toHaveBeenCalled();
    expect(result.current.hasNewer).toBe(false);

    // and the refresh is live from the start.
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).toHaveBeenCalledTimes(2);
  });

  it("an SSE burst arriving DURING the anchor fetch does not overtake it with a newest page", async () => {
    // 🔴 F3's cause, not its symptom. A newest-page load started after the
    // anchor takes a HIGHER generation ticket and can commit first; the anchor
    // is then dropped as superseded, and the reader is told the message was
    // probably cleared. Holding the ordinary refresh for the two round-trips is
    // cheaper than any way of apologising afterwards.
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);

    const { result } = renderHook(() => useChat("b", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());

    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).not.toHaveBeenCalled();

    above.resolve(page("a", 100, 30));
    below.resolve([mkMsg("a0", "b", "owner", 100), mkMsg("t1", "b", "owner", 131)]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });
    expect(outcome).toBe("found");
  });

  it("回到最新 out of an anchor-first entry hands the room back to the LIVE refresh", async () => {
    // The second line that may not break: an anchor window must never be a dead
    // end. If the hold-off outlived the anchor this conversation would stop
    // refreshing for the rest of the session — silently, and only for the
    // people who arrived through a jump link.
    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      // 🔴 錨點窗要停在「還沒接上活尾巴」,而 fix12 之後那只有一種成因:往新那
      // 一頁滿了(⇒ 走訪要繼續)而走訪的下一通失敗。這就是造它的方法。
      .mockResolvedValueOnce(page("a", 129, 100))
      .mockRejectedValueOnce(new Error("walk page failed")); // full → live tail is below
    const { result } = renderHook(() => useChat("b", "a0"));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );

    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "after 回到最新 the ordinary refresh must be live again",
    ).toBe(before + 1);
  });

  it("回到最新 while the anchor is STILL IN THE AIR does not leave the room without a refresh", async () => {
    // The nastiest shape of the same line, and the one the tidy version misses:
    // the anchor never settles because the owner overtook it, so the flag that
    // holds the refresh off is not cleared by the anchor landing — it has to be
    // cleared by 回到最新 itself, or this room never fetches again and looks
    // merely quiet while doing it.
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    const { result } = renderHook(() => useChat("b", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      expect(await pending).toBe("superseded");
    });

    const before = h.listChat.mock.calls.length;
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "an anchor that never landed must not silence this room for good",
    ).toBe(before + 1);
  });

  it("a MID-SESSION jump is not overtaken by an SSE burst either", async () => {
    // The entry hold-off does not cover this one: the room is already the live
    // tail (nothing pending) and the owner jumps from inside it — 請示卡's
    // 跳到原訊息 while the conversation is open. A newest-page load starting
    // inside those two round-trips takes a higher ticket and commits first, and
    // the jump is then reported to the reader as a message that is not there.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    const before = h.listChat.mock.calls.length;

    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "no newest page may start while the anchor pair is in the air",
    ).toBe(before);

    above.resolve(page("a", 100, 30));
    below.resolve([mkMsg("a0", "b", "owner", 100), mkMsg("t1", "b", "owner", 131)]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });
    expect(outcome).toBe("found");
    expect(result.current.messages.map((m) => m.id)).toContain("a0");
  });

  // ───────────────────────────────────────────────────────────────────────────
  // The three ways this seam used to fail silently.
  // ───────────────────────────────────────────────────────────────────────────

  it("an id that exists but belongs to ANOTHER conversation is a miss, not an empty room", async () => {
    // 🔴 F1 — a reachable 200, not a defensive branch. The server resolves the
    // anchor WITHOUT the participant filter on purpose ("a window anchored
    // outside it simply comes back empty, which is the honest answer"), so a
    // real message id from a DIFFERENT thread answers both calls 200 + [].
    // Adopting that window writes `messages: []`: the room goes blank, the miss
    // notice does not light, and the console says nothing either.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow.mockResolvedValueOnce([]).mockResolvedValueOnce([]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("c-someone-elses");
    });

    expect(outcome).toBe("missing");
    // The thread the owner was reading is left exactly as it was.
    expect(result.current.messages).toHaveLength(30);
    expect(result.current.messages.map((m) => m.id)).toEqual(
      page("n", 9000, 30).map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });

  it("being OVERTAKEN is reported as superseded, never as a missing message", async () => {
    // 🔴 F3. `loadAround` used to answer false for three different facts — 404,
    // a failed request, and "a newer load committed while we were in the air".
    // ChatArea has one branch for false, so the third one put 「找不到那則訊息,
    // 可能已經被清掉了」 on screen about a message that is still there, with the
    // fetch latch already spent: no retry, no button, jump silently abandoned.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });

    // The owner presses 回到最新 while the anchor pair is still in the air.
    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });

    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });

    expect(outcome).toBe("superseded");
    // …and the window that lost the race did not land on top of the winner.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });


  // ───────────────────────────────────────────────────────────────────────────
  // 閂的生命週期 —— 「卡住不清」這個形狀在這個 hook 裡出現過三次(F2 的
  // `hasNewer`、R3-1 的 `anchorFetching`、R3-3 的 `anchorPending`),而三次的
  // 後果都一樣:那條對話從此不刷新、也不標已讀,畫面上一個字都沒有。以下三條
  // 守的是**閂本身**,不是它擋住的那件事。

  it("上一條對話的錨點還在飛,不准把切過去的那一間鎖成永遠空白", async () => {
    // 🔴 R3-1。`anchorFetching` 原本不隨訂閱重置,也不分 peer,所以 A 的錨點兩個
    // GET 還在空中時切到 B,B 的第一個 load() 被 A 留下的計數擋掉 —— 而且擋掉之後
    // 沒有人會再叫一次(load() 只由訂閱/SSE/focus 觸發)。實測 B 的房間 22 秒都是
    // 0 列,A 落地也不會自己好。
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);

    const inA = renderHook(() => useChat("a", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = inA.result.current.loadAround("a0");
    });
    expect(h.listChat).not.toHaveBeenCalled();

    const bPage = page("z", 9000, 5);
    h.listChat.mockResolvedValue(bPage);
    inA.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });

    expect(
      h.listChat.mock.calls.map((c) => c[0]),
      "切過去的那一間必須自己撈自己的最新頁",
    ).toEqual(["b"]);
    expect(inB.result.current.messages.map((m) => m.id)).toEqual(
      bPage.map((m) => m.id),
    );

    // …而且 A 的錨點落地之後,B 既不會被換掉,也還活著。
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      await pending;
      await settle();
    });
    expect(inB.result.current.messages.map((m) => m.id)).toEqual(
      bPage.map((m) => m.id),
    );
    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat.mock.calls.length).toBe(before + 1);
  });

  it("上一條對話留下的收尾動作,不准替新的那一條把它自己的錨點閂解開", async () => {
    // 同一個形狀的另一半,而且只有 peer 戳記擋得住它 —— 因為踩到它的不是「還在
    // 飛的那次載入」,而是**上一條對話留在閉包裡的收尾函式**:ChatArea 的
    // `loadAround(...).then(… void resetToLatest())` 這個回呼在切走之前就建好了,
    // 裡面那個 `resetToLatest` 綁的是 A。錨點落地時人已經在 B,回呼照樣跑。
    //
    // `resetToLatest` 的第一件事就是「把錨點閂放掉」。它認 peer 的話,A 的那次
    // 收尾找不到自己的紀錄、什麼都不做(對一條已經離開的對話,這正是對的);
    // 不認 peer 的話,它會把 **B** 的錨點閂解開 —— 而 B 的視窗還在路上,於是下一
    // 個 SSE burst 會先把一頁最新的蓋上去(這張票拿掉的那格中間畫面),B 自己的
    // 視窗接著被判成 superseded。
    const aAbove = deferred<ChatMessage[]>();
    const aBelow = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(aAbove.promise)
      .mockReturnValueOnce(aBelow.promise);

    const inA = renderHook(() => useChat("a", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let pendingA!: Promise<JumpOutcome>;
    act(() => {
      pendingA = inA.result.current.loadAround("a0");
    });
    // A 的回呼在切走之前就抓在手上了。
    const staleResetToLatest = inA.result.current.resetToLatest;

    // B 也是跳轉進來的,它的錨點還沒有人去撈(ChatArea 的 reactor 要下一拍)。
    // 換房＝換一份 hook(R13-5),所以 B 的閂是它自己那份紀錄上的。
    inA.unmount();
    const inB = renderHook(() => useChat("b", "b0"));
    await act(async () => {
      await settle();
    });

    h.listChat.mockResolvedValue(page("z", 9000, 30));
    aAbove.resolve(page("a", 100, 30));
    aBelow.resolve(page("a", 129, 30));
    await act(async () => {
      await pendingA;
      // A 的 then 回呼跑了 —— 綁著 A 的那個 resetToLatest。
      await staleResetToLatest();
      await settle();
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });

    expect(
      h.listChat.mock.calls.map((c) => c[0]),
      "B 的錨點還沒到,不准有人先把一頁最新的蓋上去",
    ).not.toContain("b");
    expect(inB.result.current.messages, "B 的房間還在等它自己的視窗").toEqual(
      [],
    );
  });

  it("上一條對話晚到的「回到最新」不准燒掉一張世代票,把新對話比它早起跑的載入丟掉", async () => {
    // 🔴 第五輪 R5-1 的附帶。`resetToLatest` 的 peer 守衛原本只掛在 `setThread`
    // 上,`committedSeqRef.current = seq` 卻是**無條件**寫的。
    // 世代票(今天是 `lib/threadCommit` 的 `takeTicket` 與 commit 自己的水位)是
    // 刻意跨載入、刻意不重置的單調時鐘,所以一次
    // 晚到的跨對話 `resetToLatest`(ChatArea 的錨點失敗回呼在訊息列空的時候正好
    // 會發一次)會把水位推高,然後把自己那一頁丟掉 —— 而新對話**比它早起跑**、
    // 票號比較低的那次載入,接著被靜靜判成 superseded。沒有 spinner、沒有錯誤,
    // 那間房就停在那裡,等下一個 SSE burst 自癒。
    const bPage = deferred<ChatMessage[]>();
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "a" ? page("a", 100, 5) : bPage.promise,
    );

    const inA = renderHook(() => useChat("a"));
    await waitFor(() => expect(inA.result.current.messages).toHaveLength(5));
    // A 的收尾在切走之前就抓在手上了(ChatArea 的 then 回呼)。
    const staleResetToLatest = inA.result.current.resetToLatest;

    inA.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });
    expect(inB.result.current.messages, "前提:B 的第一次載入還在空中").toEqual(
      [],
    );

    // A 的失敗回呼這時才落地,並且發出它那次「回到最新」。
    await act(async () => {
      await staleResetToLatest();
      await settle();
    });
    await act(async () => {
      bPage.resolve(page("z", 9000, 5));
      await settle();
    });

    expect(
      inB.result.current.messages.map((m) => m.id),
      "B 自己的第一頁不准被上一條對話燒掉的世代票丟掉",
    ).toEqual(["z0", "z1", "z2", "z3", "z4"]);
  });

  it("切走再切回同一個人,上一趟晚到的「回到最新」不准把這一趟的錨點窗蓋掉,也不准燒它的世代票", async () => {
    // 🔴 第六輪 R6-1 的另一半。上一顆補的守衛是
    // `if (threadRef.current.peer !== withId) return;` —— **字串比對**,而
    // `threadRef` 是不分 peer、不重置的共用鏡子。A→B→**A** 之後
    // `threadRef.current.peer === "a" === withId`,守衛照樣放行:上一趟的
    // 「回到最新」燒掉一張世代票,並且把一頁最新的蓋到這一趟的錨點窗上 ——
    // 正是這張票要刪掉的那格中間畫面,只是來源換成了同一個人的上一趟造訪。
    // 綁「哪一次造訪」(捕捉到的紀錄)而不是「哪一個人」才擋得住。
    const aPage = deferred<ChatMessage[]>();
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "a" ? aPage.promise : page("b", 500, 5),
    );

    const trip1 = renderHook(() => useChat("a"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalled());
    // 第一趟 A 的收尾在切走之前就抓在手上了(ChatArea 的 then 回呼)。
    const staleResetToLatest = trip1.result.current.resetToLatest;

    trip1.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });
    // …再從 roster 切回 A,而且這一趟是**帶著錨點**進來的。
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    inB.unmount();
    const trip2 = renderHook(() => useChat("a", "a0"));
    await act(async () => {
      await settle();
    });
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = trip2.result.current.loadAround("a0");
    });
    expect(trip2.result.current.messages, "前提:這一趟在等它自己的視窗").toEqual(
      [],
    );

    // 上一趟的「回到最新」這時才落地。
    await act(async () => {
      const call = staleResetToLatest();
      aPage.resolve(page("z", 9000, 5));
      await call;
      await settle();
    });
    expect(
      trip2.result.current.messages,
      "上一趟的最新頁不准蓋到這一趟的錨點窗上",
    ).toEqual([]);

    // …而這一趟自己的視窗照樣 commit 得了(世代票沒有被上一趟燒掉)。
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      await pending;
      await settle();
    });
    expect(
      trip2.result.current.messages.length,
      "這一趟的視窗不准被上一趟燒掉的世代票判成 superseded",
    ).toBeGreaterThan(0);
  });

  it("切走再切回同一個人,上一趟慢回的錨點視窗不准貼進這一趟的空房間", async () => {
    // 🔴 第七輪 R7-1,同一族的第五個 commit 點。`loadAround` 的成功分支原本只剩
    // 兩道:開跑時抽的世代票,和 `prev.peer !== withId` 這句**字串**比對。世代票
    // 一定比這一趟任何載入都早;字串在回到同一個人時兩邊都是 "a"。所以上一趟慢回
    // (200,不是失敗)的那一對視窗會整批貼進這一趟正在等自己視窗的空房間,把進房
    // 定位一次性消耗掉。
    //
    // 🔴 A→B→A 是三次 mount(R13-5),所以下面用 unmount／mount 驅動。上一趟那對
    // 視窗落地時,它的 commit 寫進一個 React 已經丟掉的 component。
    //
    // ⚠️ 這條不再斷言那次呼叫回傳 "superseded":那個回傳值是給 caller 的,而這一
    // 趟的 caller 跟這一趟的 hook 一起被卸載了,沒有人收得到。要斷言的是房間。
    const staleAbove = deferred<ChatMessage[]>();
    const staleBelow = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(staleAbove.promise)
      .mockReturnValueOnce(staleBelow.promise);

    const trip1 = renderHook(() => useChat("a", "old0"));
    let stale!: Promise<JumpOutcome>;
    act(() => {
      stale = trip1.result.current.loadAround("old0");
    });

    // 中間那一間也是帶錨點進來的 —— 這段路上沒有人 commit。
    trip1.unmount();
    const inB = renderHook(() => useChat("b", "b0"));
    await act(async () => {
      await settle();
    });
    // 回到 A 的第二趟,一樣帶錨點,房間空的在等自己的視窗。
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    inB.unmount();
    const trip2 = renderHook(() => useChat("a", "new0"));
    await act(async () => {
      await settle();
    });
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = trip2.result.current.loadAround("new0");
    });
    expect(trip2.result.current.messages, "前提:這一趟在等它自己的視窗").toEqual(
      [],
    );

    // 上一趟的那一對 200 這時才落地。
    await act(async () => {
      staleAbove.resolve(page("old", 100, 30));
      staleBelow.resolve(page("old", 129, 30));
      await stale;
      await settle();
    });
    expect(
      trip2.result.current.messages,
      "上一趟的錨點視窗不准貼進這一趟的空房間",
    ).toEqual([]);

    // …而這一趟自己的視窗照樣 commit 得了。
    above.resolve(page("new", 200, 30));
    below.resolve(page("new", 229, 30));
    await act(async () => {
      await pending;
      await settle();
    });
    expect(
      trip2.result.current.messages.length,
      "這一趟的視窗不准被上一趟燒掉的世代票判成 superseded",
    ).toBeGreaterThan(0);
  });

  it("切走再切回同一個人,上一趟送出的那則訊息的 post-send refetch 不准蓋掉這一趟的錨點窗", async () => {
    // 🔴 第六輪 R6-1,同一個根的第三處。`refetch` 的唯一呼叫者是 `send`,而一次
    // 送出撐得過切換:POST 還在空中,人切到 B 再切回 A,這個 refresh 才落地 ——
    // 它會燒一張世代票,並且把一頁最新的合進**這一趟**;若這一趟是帶著錨點進來
    // 的,那就是這張票要刪掉的那格中間畫面。`prev.peer !== withId` 看不到它,
    // 因為人根本沒有換。
    const posted = deferred<unknown>();
    h.postChat.mockReturnValue(posted.promise);
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "a" ? page("z", 9000, 5) : page("b", 500, 5),
    );

    const trip1 = renderHook(() => useChat("a"));
    await waitFor(() => expect(trip1.result.current.messages).toHaveLength(5));
    let sending!: Promise<void>;
    act(() => {
      sending = trip1.result.current.send("在 A 打的字");
    });

    trip1.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    inB.unmount();
    const trip2 = renderHook(() => useChat("a", "a0"));
    await act(async () => {
      await settle();
    });
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = trip2.result.current.loadAround("a0");
    });
    expect(trip2.result.current.messages, "前提:這一趟在等它自己的視窗").toEqual(
      [],
    );

    await act(async () => {
      posted.resolve({});
      await sending;
      await settle();
    });
    expect(
      trip2.result.current.messages,
      "上一趟送出後的最新頁不准蓋到這一趟的錨點窗上",
    ).toEqual([]);

    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      await pending;
      await settle();
    });
    expect(
      trip2.result.current.messages.length,
      "這一趟的視窗不准被上一趟燒掉的世代票判成 superseded",
    ).toBeGreaterThan(0);
  });

  it("切走再切回同一個人,上一趟 post-send refetch 自己那通最新頁不准貼進這一趟的空房間", async () => {
    // 🔴 第八輪 R8-1,同一族的第九個實例 —— 而且它跟上一條測試(POST 掛在空中)
    // 走的**不是**同一條路。`refetch` 的造訪守衛站在**函式開頭**,擋得住 POST
    // 掛在空中的那條;擋不住的是 POST 立刻回、`refetch` 自己那通 `listChat`
    // 掛在空中的這條 —— 開頭那句守衛早在兩個 `await` 之前就放行了,commit 之前
    // 只剩世代票(中間那一趟一頁都沒 commit,水位一步沒動)＋ `prev.peer !== withId`
    // (人根本沒換)。正是第六輪判定不足的那一對。
    const hang = deferred<ChatMessage[]>();
    h.listChat
      .mockResolvedValueOnce(page("z", 9000, 5)) // 第一趟 A 的一般進房
      .mockReturnValueOnce(hang.promise); // post-send refetch 自己那通

    const trip1 = renderHook(() => useChat("a"));
    await waitFor(() => expect(trip1.result.current.messages).toHaveLength(5));
    let sending!: Promise<void>;
    act(() => {
      sending = trip1.result.current.send("在 A 打的字");
    });
    await act(async () => {
      await settle();
    });

    // 中間那一間也是帶錨點進來的:一頁都沒有 commit,所以世代票攔不到後面那一步。
    trip1.unmount();
    const inB = renderHook(() => useChat("b", "b0"));
    await act(async () => {
      await settle();
    });
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    inB.unmount();
    const trip2 = renderHook(() => useChat("a", "a0"));
    await act(async () => {
      await settle();
    });
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = trip2.result.current.loadAround("a0");
    });
    expect(trip2.result.current.messages, "前提:這一趟在等它自己的視窗").toEqual(
      [],
    );

    await act(async () => {
      hang.resolve(page("z", 9000, 6));
      await sending;
      await settle();
    });
    expect(
      trip2.result.current.messages,
      "上一趟 post-send 的最新頁不准貼進這一趟的空錨點房間",
    ).toEqual([]);

    // …而這一趟自己的視窗照樣 commit 得了(換房沒有連著把這條路關掉)。
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      await pending;
      await settle();
    });
    expect(
      trip2.result.current.messages.length,
      "這一趟的視窗不准被上一趟燒掉的世代票判成 superseded",
    ).toBeGreaterThan(0);
  });

  it("錨點被超車、caller 不再重排之後,這間房仍然刷新得起來", async () => {
    // 🔴 R3-3。superseded 那條路原本**刻意**保留「錨點還沒到」的閂,把清除交給
    // caller;而 caller 只在「訊息列是空的」時才清 —— 但 superseded 的定義就是
    // 有別的載入 commit 上來了,也就是列表非空。重試次數用完之後那個閂就永遠是
    // true,那條對話從此不刷新、也不標已讀。
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    const { result } = renderHook(() => useChat("b", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });

    // 超車的是**送出一則訊息**的 post-send refetch:它不受錨點的 hold-off 擋著,
    // 拿到更高的世代票並先 commit。這是最短的一條自然路徑。
    h.listChat.mockResolvedValue(page("z", 9000, 30));
    await act(async () => {
      await result.current.send("hi");
    });
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      expect(await pending).toBe("superseded");
    });

    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "被超車的錨點不准把這間房留在「永遠不刷新」",
    ).toBe(before + 1);
  });

  it("從同一條連結回到同一個人時,上一趟的收尾不准解開這一趟的錨點閂", async () => {
    // 🔴 第四輪 R4-1。`loadAround` 的收尾放的是**它自己捕捉到的**那份紀錄,而不是
    // 「現在」查得到的那一份 —— 兩者在 A→B→A 這條路上不是同一個東西:回到 A 時
    // latch 紀錄已經重建過,peer 欄位卻又是 "a",所以 `latchesOf("a")` 會**成功**
    // 交出第二趟的紀錄。用它來放閂的話,第一趟的收尾會替第二趟解開錨點閂,
    // 而且把 `anchorFetching` 減成 -1(連 `> 0` 那道閘一起失效),下一個 SSE burst
    // 就把一頁最新的蓋在還沒撈完的錨點上 —— 這張票要拿掉的那格中間畫面。
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);

    const trip1 = renderHook(() => useChat("a", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let firstTrip!: Promise<JumpOutcome>;
    act(() => {
      firstTrip = trip1.result.current.loadAround("a0");
    });

    // 中途切去 B(一般進房),再從同一條連結回到 A —— 第二趟的錨點還沒有人去撈。
    h.listChat.mockResolvedValue(page("z", 9000, 30));
    trip1.unmount();
    const inB = renderHook(() => useChat("b"));
    await act(async () => {
      await settle();
    });
    inB.unmount();
    const trip2 = renderHook(() => useChat("a", "a0"));
    await act(async () => {
      await settle();
    });
    const beforeA = h.listChat.mock.calls.filter((c) => c[0] === "a").length;
    expect(beforeA, "前提:第二趟進 A 還在等它自己的視窗").toBe(0);

    // 第一趟現在才落地(它已經被 B 的載入超車,所以只剩收尾這件事)。
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      await firstTrip;
      await settle();
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });

    expect(
      h.listChat.mock.calls.filter((c) => c[0] === "a").length,
      "A 的第二次進房錨點還沒撈,不准先蓋一頁最新的上去",
    ).toBe(0);
    expect(
      trip2.result.current.messages,
      "第二趟的房間還在等它自己的視窗",
    ).toEqual([]);
  });
});
