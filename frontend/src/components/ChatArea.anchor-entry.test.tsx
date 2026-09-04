// T-48 · 進房錨點優先的那條「接線」—— 以及切換對話時它不准鎖住新的那一間。
//
// 為什麼要有這一個檔案(第三輪獨立審查 R3-2):整個「進房就是進到那一則」的功能
// 靠 ChatArea 的一個參數 —— `useChat(member.id, jumpToMsgId)`。把第二個參數拿掉
// (型別合法,因為它是 optional)之後,**2600 支測試 ＋ tsc 全綠**:
//   · `useChat.scrollback.test.ts` 直接呼叫 hook,自己傳 anchor —— 測得到 hook,
//     測不到 ChatArea 有沒有傳;
//   · `ChatArea.unread-jump.test.tsx` 把 `useChat` 整個 mock 掉 —— 那裡根本沒有
//     真的 hook;
//   · e2e 只斷言終態(目標 attached / inViewport),而拿掉錨點優先之後 `loadAround`
//     照樣會把窗換上來 —— 終態一模一樣。
// 中間那條線沒有人接。所以這裡用**真的 ChatArea ＋ 真的 useChat**,只把 api seam
// 換掉,直接量「這間房發出去的第一個請求是什麼」。
//
// 第二件(R3-1)只有把兩者接在一起才量得到:切換對話是 ChatArea 換 `member` prop,
// 而被上一條對話的錨點鎖住的是 useChat 的閂。

import { StrictMode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, ReplyCard } from "../api/adapter";

const OWNER = "owner";
const A = "m-aaaaaaaaaaaa";
const B = "m-bbbbbbbbbbbb";

/** Every `GET /api/chat` this room makes, in order, split by which kind it is:
 * a PLAIN newest page (`?with=` and nothing else — the request anchor-first
 * entry exists to not make) or an anchor WINDOW (`?end_id=` / `?start_id=`). */
let plainCalls: string[] = [];
/** Every `lastReadTs` this room marked read — see the api mock. */
let markReads: number[] = [];
let windowCalls: { withId: string; anchor: { startId?: string; endId?: string } }[] =
  [];

const log: ChatMessage[] = [];
/** Holds the anchor-window pair in flight, so a conversation switch can happen
 * while A's jump is still in the air — the whole of R3-1. */
let holdWindows: null | (() => void) = null;
/** Makes the held pair end in a REJECTION rather than a page, i.e. the
 * `"unreachable"` ending of `loadAround` (a 502, a dropped connection). */
let windowsFail = false;
/** 讓第 N 通(1-based)之後的每一通 window 請求都失敗 —— 用來造「走訪撈到一半
 * 沒撈完」這個 fix12 之後唯一會留下 `hasNewer` 的狀態。0 = 不啟用。 */
let windowFailAfter = 0;
/** 讓「往新」那一頁回一整頁**已經握在手上的列**(滿頁 ⇒ `hasNewer` 仍為 true,
 * 但一列都沒有新的)。這就是重複錨點請求真的會拿回來的東西,也是自動連鎖唯一
 * 可能空轉的形狀。 */
let windowStale = false;
/** Same, for the plain newest page — so 回到最新's own fetch can be left in the
 * air across a conversation switch. */
let holdPlain: null | (() => void) = null;
/** Every `scrollIntoView` this room performs, tagged by WHAT was scrolled and
 * with which option — `block: "end"` is `scrollToLatest`'s signature and
 * nothing else in ChatArea uses it. */
let scrolls: { on: string; block: unknown }[] = [];
/** Every `getReplyCard`, with the one thing that matters: was the row carrying
 * that card ALREADY PAINTED when the read went out? (See the afterEach.) */
let cardReads: { id: string; rowPainted: number }[] = [];
/** The waiting cards a test deliberately put in the thread — the afterEach's
 * denominator. */
let seededWaitingCards: string[] = [];

const CARD: ReplyCard = {
  id: "rc-1",
  from: A,
  kind: "decision",
  summary: "要寄出嗎?",
  body: "",
  options: [
    { text: "寄出", aiPick: true },
    { text: "先不要", aiPick: false },
  ],
  selectMode: "single",
  status: "waiting",
  createdTs: 1,
  attachments: [],
  answeredTs: null,
  expiredTs: null,
  chatMessageId: "",
  answer: null,
  task: null,
};

/** One WAITING 請示卡 row, spliced into `peer`'s stream at `ts`. */
function seedWaitingCard(peer: string, id: string, cardId: string, ts: number) {
  log.push({
    id,
    from: peer,
    to: OWNER,
    body: "要寄出嗎?",
    ts,
    attachments: [],
    replyCardId: cardId,
    replyCardStatus: "waiting",
  });
  seededWaitingCards.push(cardId);
}

function threadOf(peer: string): ChatMessage[] {
  return log.filter((m) => m.from === peer || m.to === peer);
}

vi.mock("../api", () => ({
  api: {
    listChat: async (
      withId: string,
      limit?: number,
      cursor?: { beforeTs: number; beforeId: string },
    ) => {
      if (!cursor) plainCalls.push(withId);
      if (!cursor && holdPlain) {
        await new Promise<void>((r) => {
          const prev = holdPlain;
          holdPlain = () => {
            prev?.();
            r();
          };
        });
      }
      const all = threadOf(withId);
      const size = limit ?? 30;
      if (cursor) {
        return all
          .filter(
            (m) =>
              m.ts < cursor.beforeTs ||
              (m.ts === cursor.beforeTs && m.id < cursor.beforeId),
          )
          .slice(-size);
      }
      return all.slice(-size);
    },
    listChatWindow: async (
      withId: string,
      anchor: { startId?: string; endId?: string },
      limit: number,
    ) => {
      windowCalls.push({ withId, anchor });
      if (holdWindows) {
        await new Promise<void>((r) => {
          const prev = holdWindows;
          holdWindows = () => {
            prev?.();
            r();
          };
        });
      }
      if (windowsFail) throw new Error("listChatWindow: 502");
      if (windowFailAfter > 0 && windowCalls.length > windowFailAfter) {
        throw new Error("listChatWindow: 502 (walk page)");
      }
      const all = threadOf(withId);
      const at = all.findIndex(
        (m) => m.id === (anchor.endId ?? anchor.startId),
      );
      if (at < 0) return [];
      if (windowStale && anchor.startId) {
        return all.slice(Math.max(0, at - limit + 1), at + 1);
      }
      // Inclusive both ways, mirroring the server: `end_id` is the context
      // ABOVE the anchor, `start_id` the context BELOW.
      return anchor.endId
        ? all.slice(Math.max(0, at - limit + 1), at + 1)
        : all.slice(at, at + limit);
    },
    // 🔴 THE WHOLE OF GUARD B (T-48). Every read of a card records whether that
    // card's ROW WAS ALREADY IN THE DOM when the read went out. A row in the DOM
    // means the thread carrying it has already been COMMITTED — so a read taken
    // at that moment is one the reader is watching happen, and its answer arrives
    // as a card that GROWS under everything below it. `useChat` may not commit a
    // thread carrying a WAITING card until that card is in hand, so on every
    // commit path this must be 0.
    getReplyCard: async (id: string): Promise<ReplyCard> => {
      cardReads.push({
        id,
        rowPainted: document.querySelectorAll(
          `[data-reply-card-id="${id}"]`,
        ).length,
      });
      // 🔴 THE RESPONSE LANDS ON A MACROTASK, AND WITHOUT THAT THIS GUARD
      // MEASURES NOTHING. An `async` mock that returns immediately settles in the
      // SAME microtask drain as the commit that started it, so React has not
      // painted yet either way and `await prefill(…)` vs `void prefill(…)` are
      // literally indistinguishable — measured: the `void` mutant passed all 12
      // tests. No real response can arrive that fast (a fetch resolves from the
      // task queue), so a zero-delay mock is not a simplification of the network,
      // it is a different machine.
      await new Promise((r) => setTimeout(r, 0));
      return { ...CARD, id };
    },
    listChatReads: async () => [],
    /** Every mark-read this room sent, with the watermark it claimed. This is
     * the ONE thing a front-end flag cannot fake: the server's unread count is
     * whatever the newest of these says. */
    markChatRead: async (b: { peer: string; lastReadTs: number }) => {
      markReads.push(b.lastReadTs);
    },
    postChat: async () => ({}),
    subscribeEvents: () => () => {},
    getOutsourceWorker: async () => ({}),
  },
}));

function mkMember(id: string, name: string): Member {
  return {
    id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

const alice = mkMember(A, "Alice");
const bruno = mkMember(B, "Bruno");

// 🔴 MOUNTED THE WAY `OfficePage` MOUNTS IT (T-48, R13-5): `key={peerId}`, so a
// room switch below is an unmount + a mount, and a jump WITHIN a room (the same
// peer, a different `jumpToMsgId`) is a prop change on the same instance —
// exactly the two lifetimes the app has.
function view(m: Member, jumpToMsgId?: string) {
  return (
    <I18nProvider>
      <ChatArea
        key={m.id}
        member={m}
        members={[alice, bruno]}
        workers={[]}
        jumpToMsgId={jumpToMsgId}
      />
    </I18nProvider>
  );
}

function bubbles(container: HTMLElement): (string | null)[] {
  return Array.from(container.querySelectorAll(".chat__msg-bubble")).map(
    (n) => n.textContent,
  );
}

/** `count` messages from `peer` to the owner, ids `<peer-tag><i>`. */
function seed(peer: string, tag: string, count: number, tsStart: number) {
  for (let i = 0; i < count; i++) {
    log.push({
      id: `${tag}${i}`,
      from: peer,
      to: OWNER,
      body: `${tag}${i}`,
      ts: tsStart + i,
      attachments: [],
      replyCardId: null,
    });
  }
}

beforeEach(() => {
  log.length = 0;
  plainCalls = [];
  windowCalls = [];
  holdWindows = null;
  holdPlain = null;
  windowsFail = false;
  windowFailAfter = 0;
  markReads = [];
  windowStale = false;
  scrolls = [];
  cardReads = [];
  seededWaitingCards = [];
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn(function (
    this: Element,
    opt?: boolean | ScrollIntoViewOptions,
  ) {
    scrolls.push({
      on:
        this.getAttribute("data-msg-id") ??
        (typeof this.className === "string" ? this.className : ""),
      block: typeof opt === "object" ? opt?.block : undefined,
    });
  });
  document.hasFocus = () => true;
});

/** 🔴 THE INVARIANT THIS WHOLE GROUP IS HELD TO (T-48, guard B).
 *
 * 請示卡 ride the chat stream as ordinary messages carrying only a card id; the
 * card itself is a SEPARATE fetch, and a card that fetched after its row was
 * painted GREW under everything below it — a waiting card above a scroll target
 * pushed that target down after the jump had landed on it (+254px at 1280).
 *
 * The fix used to be "commit must hold the card first". It is now "the row does
 * not need the card at all": EVERY card mounts COLLAPSED, at its final height,
 * from what the carrying message already says (owner 2026-09-04). So the rule
 * this group is held to is the STRONGER one, and it needs no ordering to state:
 * rendering a thread — waiting cards included — must fire NO `getReplyCard` at
 * all. Nothing can grow late if nothing is ever read.
 *
 * ⚠️ It carries its own DENOMINATOR. An invariant over an empty set is green for
 * the wrong reason, and most tests in this file seed no cards at all — so a test
 * that DID seed a waiting card must be seen to have painted it, collapsed, or
 * the guard says so instead of passing. */
afterEach(() => {
  expect(
    cardReads,
    "a card was fetched just by rendering the thread — the row can now grow under the reader, which is exactly the +254px shift. Every card renders COLLAPSED and reads nothing until it is expanded.",
  ).toEqual([]);
  for (const id of seededWaitingCards) {
    // The denominator: this test really did drive a waiting card onto the
    // screen, so the emptiness above is a result and not an absence.
    const rows = document.querySelectorAll(`[data-reply-card-id="${id}"]`);
    expect(
      rows.length,
      `the seeded waiting card ${id} never reached the screen`,
    ).toBeGreaterThan(0);
    expect(
      rows[0].classList.contains("reply-card--collapsed"),
      `the seeded waiting card ${id} was painted EXPANDED — an expanded card is one that has to fetch, and the fetch is what grows the row`,
    ).toBe(true);
  }
});

describe("ChatArea 進房錨點優先(useChat 的 anchor 參數)", () => {
  it("帶著跳轉目標進房時,這間房發出去的第一個請求就是那一則的視窗,一次最新頁都不打", async () => {
    // 🔴 R3-2 的護欄。ChatArea 沒有把 `jumpToMsgId` 交給 `useChat` 的話,hook 會
    // 照舊在訂閱時撈一頁最新的 —— 一次白跑的 round-trip,以及一格「先看到活尾巴」
    // 的中間畫面給一個正要去別的地方的讀者。而那正是這張票拿掉的東西。
    // 這一條量的是**請求本身**,不是終態:終態(落在那一則)在兩種寫法下都成立。
    seed(A, "a", 80, 100);
    const targetId = "a3"; // 遠比最新 30 則舊

    const { container } = render(view(alice, targetId));
    await waitFor(() =>
      expect(
        container.querySelector(`[data-msg-id="${targetId}"]`),
      ).not.toBeNull(),
    );

    expect(
      plainCalls,
      "帶著錨點進房不准打最新頁 —— 有的話就是 ChatArea 沒把 jumpToMsgId 交給 useChat",
    ).toEqual([]);
    // …而且真的有人去撈那個視窗(兩端各一個,兩端都指著同一個 id)。
    expect(windowCalls.map((c) => c.withId)).toEqual([A, A]);
    expect(windowCalls.map((c) => c.anchor)).toEqual([
      { endId: targetId },
      { startId: targetId },
    ]);
    // 落點對:目標在畫面上。
    expect(bubbles(container)).toContain(targetId);
    // 🔴 而且這一趟**一路撈到了活尾巴**(T-48 fix12,owner rc-e1fb80065f8f)。
    // 這一行以前是它的反面(「最新那一則不准在 DOM 裡」),那是「一次手勢一頁」
    // 的字面斷言;走訪拿掉之後最新那一則必須在,否則讀的人得自己走回去,而那條
    // 路已經沒有了。
    expect(bubbles(container)).toContain("a79");
    // 往新的那幾通都問 100 —— 80 則一趟就撈完,所以只有一通。
    const forward = windowCalls.filter((c) => c.anchor.startId !== undefined);
    expect(forward.length, "80 則的距離,一通 100 就撈完了").toBe(1);
  });

  it("🔴 走訪把中間幾百則載進來,一則都不准被標成已讀 —— 按下回到最新才標", async () => {
    // 🔴 這張票最容易靜默做錯的一格,而且它是 fix12 **自己造出來**的風險。
    //
    // 以前:跳轉停在錨點窗,`hasNewer` 為 true 一路擋著 mark-read,而讓 hasNewer
    // 變 false 的每一步都是讀的人親手捲出來的 —— 所以「握得到活尾巴」和「看過活
    // 尾巴」是同一件事,`hasNewer` 一個旗標同時代表兩者。
    //
    // 現在:`loadAround` 自己撈到活尾巴,`hasNewer` 在落地那一瞬間就是 false,而
    // 讀的人還停在半年前那一則上。進房那支 mark-read effect **完全不看視窗位置**
    // (ChatArea 的 `newestTs` effect),所以它會立刻把水位蓋到最新那一則 ——
    // 中間幾百則一眼都沒看過,未讀歸零,「以下是未讀」分隔線失效。
    // owner 裁定逐字:mark-read 表達的意圖是「我看過了」,不是「我跳過來過」。
    //
    // 守它的是 `tailSeen`。把 `mayMarkRead` 的 `&& tailSeen` 拿掉(或把
    // `setTailSeen(false)` 那一行拿掉),紅的就是下面第一個 expect。
    seed(A, "a", 300, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    // 給它時間去做錯事 —— mark-read 是 fire-and-forget,不等就等於沒量到。
    await act(async () => {
      await new Promise((r) => setTimeout(r, 60));
    });

    // 前提①:走訪真的把整條線撈進來了(否則擋 mark-read 的是 hasNewer,
    // 這一格就綠在舊的那個守衛上,而不是在 tailSeen 上)。
    expect(bubbles(container), "前提:走訪真的一路撈到最新").toContain("a299");
    // 前提②:讀的人真的還停在錨點那一則,不是被捲到底。
    expect(scrolls.map((x) => x.on)).toContain("a3");

    // 🔴 心臟。整條線都在記憶體裡、hasNewer 已經是 false,而未讀水位一步都不准動。
    expect(
      markReads,
      "跳過去不等於看過 —— 走訪載進來的幾百則不准被標成已讀",
    ).toEqual([]);

    // 🔴 另一個方向,而且不能省:只釘上面那一半的話,「mark-read 整條路壞掉、
    // 永遠不標」也會過,那本身就是另一個靜默失敗。按下回到最新 → 才標,
    // 而且標在**真正最新**那一則的 ts 上。
    const arrow = container.querySelector<HTMLButtonElement>(
      '[data-testid="chat-jump-latest"]',
    );
    expect(arrow, "前提:停在錨點上時箭頭要在").not.toBeNull();
    await act(async () => {
      fireEvent.click(arrow!);
      await new Promise((r) => setTimeout(r, 30));
    });
    expect(
      markReads.at(-1),
      "回到最新之後要標,而且要標在最新那一則上",
      // seed 的 ts 從 100 起算,第 300 則是 399。
    ).toBe(399);
  });

  it("跳轉目標上方的等待中請示卡是收合的,一次都不去撈它", async () => {
    // 🔴 GUARD B 的分母,也是整包東西的理由(T-48)。訊息串跟請示卡本來是**兩次**
    // 抓取:卡片晚到,而「等待中」的卡片一到就長高(選項、chips、輸入框)。
    // 只要它坐在跳轉目標的**上面**,目標就會在跳轉已經落地之後被往下推 ——
    // 1280 寬實測 +254px。
    //
    // owner 2026-09-04 之後,待回覆的卡跟已回覆的一樣先收合:第一格畫出來就是
    // 最終高度,而且沒有第二次抓取可以讓它長高。這一條把那個形狀擺出來 ——
    // 錨點在 a40,卡片在 a35(目標上方)—— 然後交給上面那個共用的 afterEach 去
    // 問唯一重要的問題:整段 render 有沒有讀過任何一張卡?jsdom 量不到 254px
    // (它沒有版面),但它量得到「有沒有去抓」,而那就是因;像素那一半由
    // chat-jump-card-shift.ct.spec.tsx 在真的 Chromium、1280 寬上量。
    seed(A, "a", 80, 100);
    seedWaitingCard(A, "a35-card", "rc-1", 135.5);
    log.sort((x, y) => x.ts - y.ts);

    const { container } = render(view(alice, "a40"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a40"]')).not.toBeNull(),
    );
    // 分母的另一半:那張卡真的在目標**上面**,不是隨便畫在哪裡。
    const rows = Array.from(
      container.querySelectorAll("[data-msg-id]"),
    ).map((n) => n.getAttribute("data-msg-id"));
    expect(rows.indexOf("a35-card")).toBeGreaterThanOrEqual(0);
    expect(rows.indexOf("a35-card")).toBeLessThan(rows.indexOf("a40"));
    // 而且它是**收合的待回覆**卡:標籤說待回覆(不是已回覆),但沒有選項、沒有
    // 輸入框 —— 也就是沒有東西在等著長出來。
    const card = container.querySelector('[data-reply-card-id="rc-1"]')!;
    expect(card.getAttribute("data-reply-card-status")).toBe("waiting");
    expect(card.textContent).toContain("待回覆");
    expect(card.querySelectorAll(".reply-option")).toHaveLength(0);
    expect(card.querySelector(".reply-composer")).toBeNull();
  });


  it("走訪撈到一半失敗、停在半路時,回到最新的箭頭仍然在", async () => {
    // 🔴 fix12 之後這條的地形換了,但它守的東西沒換,而且更承重了。
    //
    // 以前:跳轉一定停在錨點窗,箭頭天天都在。現在:跳轉會一路撈到活尾巴,所以
    // 「還沒到最新」變成**例外狀態** —— 走訪撈到一半沒撈完。例外狀態最容易被
    // 順手刪掉(「hasNewer 現在不是永遠 false 嗎?」),而刪掉的代價是:讀的人
    // 停在半路,畫面上**沒有任何東西**告訴他下面還壓著幾十則。
    //
    // 風險不在 `chatBottomAffordance` 那個純函式(它有自己的測試),在 **ChatArea
    // 有沒有繼續把 `hasNewer` 餵給它**:那條線斷掉,函式照樣全綠。jsdom 的每一個
    // rect 都是 0,所以 `isLatestRowInView` 答**真** —— 撐著箭頭的只有
    // `windowHasNewer` 那一條線。
    seed(A, "a", 300, 100);
    // 錨點在最前面 ⇒ 往新那一頁一定滿 100 ⇒ 走訪要繼續;讓走訪的下一通失敗。
    windowFailAfter = 2;
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });

    // 前提:走訪真的沒走完 —— 否則 `hasNewer` 是 false,這條就在量一件不存在的事。
    expect(bubbles(container)).not.toContain("a299");
    // 而且畫面上沒有預覽列 —— 兩者互斥,預覽列在的話箭頭本來就該讓位,
    // 那樣這條斷言會綠在錯的東西上。
    expect(
      container.querySelector('[data-testid="chat-new-msg-preview"]'),
    ).toBeNull();

    expect(
      container.querySelector('[data-testid="chat-jump-latest"]'),
      "走訪停在半路、下面還有幾百則沒載 —— 箭頭是唯一還在說這件事的東西",
    ).not.toBeNull();
  });

  it("走到活尾巴之後,新訊息照樣 auto-follow —— 不跟的只有走訪還沒走完的錨點視窗", async () => {
    // 🔴 上一條的另一半,而且它是唯一擋得住「乾脆把 auto-follow 拿掉」的東西。
    // 不跟的條件是 `hasNewer`,不是「有人跳過」:一般進房、活尾巴、自己剛送出的
    // 那一則,全部照跟。
    seed(A, "a", 10, 100);
    const { container } = render(view(alice));
    await waitFor(() => expect(bubbles(container)).toContain("a9"));

    scrolls = [];
    await act(async () => {
      log.push({
        id: "a10",
        from: A,
        to: OWNER,
        body: "a10",
        ts: 200,
        attachments: [],
        replyCardId: null,
      });
      window.dispatchEvent(new Event("focus"));
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() => expect(bubbles(container)).toContain("a10"));
    expect(
      scrolls.map((s) => s.on),
      "活尾巴的新訊息必須照舊跟著捲",
    ).toContain("chat__scroll-anchor");
  });






  it("沒有跳轉目標的一般進房,照舊只打一頁最新的,一個視窗請求都沒有", async () => {
    // 另一半:錨點優先只能從跳轉進得去。把它變成無條件的,每一次進房都會多兩個
    // 請求,而且第一個畫面會是歷史。
    seed(A, "a", 40, 100);

    const { container } = render(view(alice));
    await waitFor(() => expect(bubbles(container)).toContain("a39"));

    expect(plainCalls).toEqual([A]);
    expect(windowCalls).toEqual([]);
  });

  it("轉圈是一個狀態,三條路都要有:一般進房、第一次跳到原訊息、房間開著時的第二次跳轉", async () => {
    // 🔴 owner 逐字要的東西(c-de666642e77b「做個轉圈圈的動畫,不管是進聊天室,
    // 或點選原訊息都是這樣」),而第三條路曾經完全拿不到它(review25 F2)。
    //
    // `OfficePage` 用 `key={peerId}` 掛 ChatArea,所以「房間已經開著、只換
    // `route.msgId`」**不會 remount**:同一位成員的第二次跳到原訊息、瀏覽器上一
    // 頁、使用者留存的舊連結,都是這一條。舊的 `initialLoading` 答的是「這條對話
    // 的第一次載入」,那時 `messages.length > 0` ⇒ 它恆為 false ⇒ 整段走訪畫面停
    // 在舊內容、零指示 —— 而走訪正是這個功能最長的一段等待。
    //
    // 🔴 三條路共用一個狀態、一處 render(owner c-d24ebd7f8d78「照理說應該只有
    // 改一個地方吧?」)。把 ChatArea 那一處的 `initialLoading` 拿掉,下面三個
    // expect **同時**紅;只紅一個就表示轉圈又被做成了兩份。三個轉圈斷言用
    // `expect.soft` 正是為了這個:第一個紅了之後後面兩條還要繼續量,否則
    // 「三條路都紅」與「只有第一條紅」在輸出上長得一模一樣。
    seed(A, "a", 300, 100);
    seed(B, "b", 300, 9000);

    // ① 一般進房 —— 最新那一頁還在飛。
    holdPlain = () => {};
    const { container, rerender } = render(view(alice));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect.soft(
      container.querySelector(".chat__loading"),
      "① 一般進房:最新那一頁還沒回來,畫面上要有轉圈",
    ).not.toBeNull();
    await act(async () => {
      const release = holdPlain;
      holdPlain = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });
    expect(
      container.querySelector(".chat__loading"),
      "內容一到就要收掉",
    ).toBeNull();

    // ③ 房間**已經開著**,只換 jumpToMsgId —— 這一格的前提是畫面上本來就有東西。
    expect(bubbles(container), "前提:③ 起跳時房間已經有內容").toContain("a299");
    holdWindows = () => {};
    await act(async () => {
      rerender(view(alice, "a3"));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 300));
    });
    expect.soft(
      container.querySelector(".chat__loading"),
      "③ 同一間房的第二次跳轉:走訪期間畫面不能停在舊內容、一個指示都沒有",
    ).not.toBeNull();
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 60));
    });
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    expect(container.querySelector(".chat__loading")).toBeNull();

    // ② 帶著跳轉目標進房(第一次),走訪還在飛。
    holdWindows = () => {};
    const second = render(view(bruno, "b3"));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect.soft(
      second.container.querySelector(".chat__loading"),
      "② 帶著跳轉目標進房:錨點窗＋走訪都還沒回來,畫面上要有轉圈",
    ).not.toBeNull();
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 60));
    });
    await waitFor(() =>
      expect(
        second.container.querySelector('[data-msg-id="b3"]'),
      ).not.toBeNull(),
    );
    expect(second.container.querySelector(".chat__loading")).toBeNull();
  });

  it("上一條對話的錨點還在飛的時候切過去,新的那一間照樣載得起來", async () => {
    // 🔴 R3-1 的護欄(hook 層在 useChat.scrollback.test.ts,這裡量的是真的手勢:
    // ChatArea 換 member prop)。A 的錨點是兩個平行 GET,伺服器一忙就是數百毫秒
    // 到數秒;在那段時間內點另一個人的 roster row —— 量到的原始症狀是 B 的房間
    // 22 秒都還是 0 列,而且 A 落地之後也不會自己好。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));
    expect(bubbles(container), "前提:A 的錨點還沒落地,房間是空的").toEqual([]);

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });

    // B 是一般進房,它的最新頁必須真的被撈回來。
    expect(plainCalls).toEqual([B]);
    await waitFor(() => expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]));

    // …而且 A 的錨點落地之後,不准把 B 的房間換掉。
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });
    expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]);
  });

  it("上一條對話的錨點抓失敗,不准把它的橫幅貼到切過去的那一間,也不准把那一間捲到底", async () => {
    // 🔴 第五輪 R5-1。這一族的第五個實例:`setJumpNotice` / `setJumpRetry` 是
    // React state,`endRef` 是 DOM ref —— 兩者都只認**現行**那一間房。
    // `unreachable`(5xx / 連線斷)與 `missing`(404)兩條結局都在 superseded 檢查
    // 之前就 return,所以切對話之後照樣走得到:A 的失敗回呼會在 B 的房間裡掛一條
    // 不屬於 B 的「讀不到那則訊息」橫幅(附一顆按了沒反應的重試鈕,因為 B 沒有
    // 跳轉目標),然後把 B 捲到底。
    // 真人版:從連結進 A 的一則舊訊息 → 那一對 window 請求吃到 502 → 在它回來
    // 之前點 roster 切到 B。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    windowsFail = true;
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() =>
      expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]),
    );

    scrolls = [];
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector(".chat__jump-miss"),
      "B 的房間不該出現 A 的跳轉失敗通知",
    ).toBeNull();
    expect(
      scrolls.map((s) => s.on),
      "A 的失敗回呼不准去捲 B 的 viewport",
    ).not.toContain("chat__scroll-anchor");
    // …而 B 的內容本身沒有被動到。
    expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]);
  });

  it("切走再切回同一個人,上一趟的錨點失敗不准把橫幅貼到這一趟,也不准把這一趟捲到底", async () => {
    // 🔴 第六輪 R6-1。這一族的第六個實例,而且它指出了前五個共同的根:
    // **身分被寫成「是哪一個人」,而不變量是「是哪一次造訪」**。當時補的兩道
    // 防線綁的都是 `member.id` 這個字串,A→B→**A** 回到同一個人時字串相等,
    // 兩道同時放行:上一趟的「現在讀不到那則訊息」橫幅(附一顆按了沒反應的重試
    // 鈕,因為這一趟沒有 jumpToMsgId)貼進這一趟,而且這一趟被捲到底。
    //
    // 🔴 R13-5 把「哪一次造訪」交還給 React:A→B→A 是三次 mount,上一趟的回呼
    // 寫的是一個已經被丟掉的 component。這條斷言的是同一個結果,不是同一句守衛。
    // 真人版:從連結進 A 的一則舊訊息 → 那一對 window 請求吃到 502 → 在它回來
    // 之前切到 B,再從 roster 切回 A。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    windowsFail = true;
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() =>
      expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]),
    );

    // 再切回 A —— 這一趟是一般進房(沒有錨點),所以它要的是最新一頁。
    await act(async () => {
      rerender(view(alice));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() => expect(bubbles(container)).toContain("a79"));

    scrolls = [];
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector(".chat__jump-miss"),
      "回到 A 的這一趟不該出現上一趟的跳轉失敗通知",
    ).toBeNull();
    expect(
      scrolls.map((s) => s.on),
      "上一趟的失敗回呼不准去捲這一趟的 viewport",
    ).not.toContain("chat__scroll-anchor");
    // …而這一趟的內容本身沒有被動到。
    expect(bubbles(container)).toContain("a79");
  });

  it("上一條對話按下「回到最新」留下的待辦,不准把帶著錨點進來的新對話捲到活尾巴", async () => {
    // 🔴 第五輪 R5-3 的護欄。`pendingLatestScroll` 這一輪從跨 peer 的 ref 改判
    // 進紀錄,但當時**一條會紅的測試都沒有** —— 把它改回跨 peer,src/components/
    // 1472 支全綠。這條把它釘住。
    // 形狀:A 按下「回到最新」而且必須先抓活尾巴(走訪沒走完 ⇒ hasNewer) → 那一
    // 頁還在空中就切到 B,而 B 正是**帶著錨點**進來的 → B 的錨點窗落地時,A 留下
    // 的待辦會把 B 捲到活尾巴,也就是這張票要拿掉的那格中間畫面。
    //
    // fix12 之後「A 是一個沒接上尾巴的窗」只有一種造法:走訪撈到一半失敗。
    seed(A, "a", 300, 100);
    seed(B, "b", 80, 9000);
    windowFailAfter = 2;

    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30));
    });
    // B 的錨點窗不准被 A 的失敗設定波及。
    windowFailAfter = 0;

    // A 的房間停在半路 ⇒ 圓形箭頭在,而且按下去必須先抓活尾巴。
    holdPlain = () => {};
    const arrow = container.querySelector<HTMLButtonElement>(
      '[data-testid="chat-jump-latest"]',
    );
    expect(arrow, "前提:錨點窗底下該有「回到最新」的箭頭").not.toBeNull();
    await act(async () => {
      fireEvent.click(arrow!);
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(plainCalls, "前提:回到最新真的去抓了活尾巴").toEqual([A]);

    scrolls = [];
    await act(async () => {
      rerender(view(bruno, "b3"));
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="b3"]')).not.toBeNull(),
    );

    expect(
      scrolls.filter((s) => s.block === "end"),
      "B 是帶著錨點進來的 —— 不准被上一條對話的待辦捲到活尾巴",
    ).toEqual([]);
    // 落點仍然是 B 自己的錨點。
    expect(scrolls.some((s) => s.on === "b3" && s.block === "center")).toBe(
      true,
    );
  });

  it("帶錨點進房、視窗還沒落地的時候,不准出現新訊息預覽列,也不准把未讀分隔線錨在任何一列上", async () => {
    // 🔴 第八輪 R8-7 —— 這一族到今天為止 `ChatArea` 這一層的第一張網。
    //
    // 前七輪的護欄全都釘在**某一句守衛**上(hook 層:上一趟的資料有沒有進到
    // `messages`)。那個策略八輪找出九個源頭,每一個都是「又一條沒人守的 async
    // 路徑」;而只要任何一個源頭漏掉,同一串下游後果就整套復活,卻沒有任何一條
    // 測試會紅。這一條反過來斷言**結果**:這一趟是帶著錨點進來的,在它自己的視窗
    // 落地之前,房間必須是空的、沒有新訊息預覽列、沒有未讀分隔線 —— 不管污染是
    // 從哪一條路徑來的。
    //
    // 走的路是 R8-1(第九個實例):A 的 post-send refetch 自己那通最新頁掛在空中,
    // 人切到 B(帶錨點,一頁都沒 commit)再帶著錨點切回 A。那一頁落地時,跳轉
    // 反應器已經把 `initialPositioned` 消耗掉、`prevIds` 設成空集合,所以整批
    // stale 列都算「剛到的」⇒ 預覽列與分隔線一起錨在**上一趟的活尾巴**上,
    // 在一間本該只顯示錨點視窗的房間裡。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    const { container, rerender } = render(view(alice));
    await waitFor(() => expect(bubbles(container)).toContain("a79"));

    // A 的 post-send refetch 那通最新頁留在空中。
    holdPlain = () => {};
    const box = container.querySelector(".chat__input") as HTMLTextAreaElement;
    await act(async () => {
      fireEvent.change(box, { target: { value: "在 A 打的字" } });
      fireEvent.click(container.querySelector(".chat__send") as HTMLElement);
      await new Promise((r) => setTimeout(r, 10));
    });

    // 中間那一間也是帶錨點進來的:一頁都沒有 commit,世代票的水位一步都沒動。
    holdWindows = () => {};
    await act(async () => {
      rerender(view(bruno, "b3"));
      await new Promise((r) => setTimeout(r, 20));
    });
    // 回到 A 的第二趟,一樣帶錨點,房間空的在等自己的視窗。
    await act(async () => {
      rerender(view(alice, "a1"));
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(bubbles(container), "前提:這一趟在等它自己的錨點視窗").toEqual([]);

    await act(async () => {
      const release = holdPlain;
      holdPlain = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector('[data-testid="chat-new-msg-preview"]'),
      "錨點視窗還沒落地,不准冒出一條指著上一趟活尾巴的新訊息預覽列",
    ).toBeNull();
    expect(
      container.querySelector(".chat__unread-divider"),
      "未讀分隔線不准錨在上一趟的列上",
    ).toBeNull();
    expect(bubbles(container), "這一趟的房間仍然只等它自己的視窗").toEqual([]);

    // …而這一趟自己的視窗照樣落得下來。
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a1"]')).not.toBeNull(),
    );
  });

  it("StrictMode 的 setup→cleanup→setup 之後,錨點進的那間房照樣刷新得起來", async () => {
    // 🔴 第四輪 R4-2。閂的紀錄本來是**每次 effect 跑**就整份重建一次,而
    // `main.tsx` 用的就是 `<StrictMode>` —— 掛載時是 setup → cleanup → setup。
    // 第一次 setup 之後 `loadAround` 就發車了,收尾放的是它捕捉到的那一份;第二次
    // setup 又把 `anchorPending` 設回 true,而 `jumpFetchedRef` 已經記下這個 id,
    // reactor 直接 early-return —— 沒有第二次 `loadAround` 會來清它。那間房從此
    // 不刷新(SSE burst / focus / visibilitychange 全被擋),畫面看起來卻完全正常。
    // 錨點選在靠近活尾巴的一則:`start_id` 那頁回得短 ⇒ `hasNewer === false`,
    // 所以 `load()` 不會被 `hasNewer` 那道閘擋著,量到的就是 `anchorPending` 本身。
    seed(A, "a", 10, 100);
    const targetId = "a7";

    const { container } = render(<StrictMode>{view(alice, targetId)}</StrictMode>);
    await waitFor(() =>
      expect(
        container.querySelector(`[data-msg-id="${targetId}"]`),
      ).not.toBeNull(),
    );

    plainCalls = [];
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
      await new Promise((r) => setTimeout(r, 20));
    });

    expect(
      plainCalls,
      "錨點落地之後這間房必須回到一般的刷新 —— 空的就是 anchorPending 被留在 true",
    ).toEqual([A]);
  });
});
