// T-48 / fix11 — RENDER-COST MEASUREMENT, not a guard.
//
// 🔴 這支不斷言產品對不對。它只量一件事:把 N 則訊息載進聊天畫面,要多久、
// 吃多少記憶體、DOM 幾個節點、載完之後捲一次要多久。
//
// 為什麼要量:`ChatArea` 沒有任何 virtualization(`splitByDay(messages).map`
// → `groupMessages(...).map` → 每一列都真的 render),所以「跳轉之後一路撈到
// 最新」的成本是 N 的函數,而 repo 裡唯一一個相關數字(「8,000 則約兩分鐘、
// 2.6 GB」)只是一行沒有任何產物支撐的註解。
//
// 記憶體用 CDP 量(`Performance.getMetrics` 的 JSHeapUsedSize),因為
// `performance.memory` 的精度被瀏覽器刻意打鈍。每一格量之前先 collectGarbage。
import { test, expect } from "@playwright/experimental-ct-react";

// 🔴 這支預設不跑,而且那不是「暫時關掉」——**它不是護欄,是量測**:它一個產品
// 斷言都沒有,紅了也不代表任何東西壞掉。留在 repo 是為了「下次有人想重量,不必
// 從頭寫探針」,不是為了每一輪 CI 都花好幾分鐘去 render 八千列(N=8000 × 兩寬 ×
// 三次 = 這一支自己就比整包 CT 還久)。
//
// 要跑：  T48_RENDER_COST=1 npx playwright test -c playwright-ct.config.ts \
//           visual-guards/chat-render-cost.ct.spec.tsx
//
// ⚠️ 它量的是**這台機器**的數字。報告裡那張表是 M5 Pro／macOS 26.5 上的中位數,
// 換一台機器重跑會得到不同的絕對值——**能跨機器比的是斜率,不是那幾個毫秒。**
const MEASURE = process.env.T48_RENDER_COST === "1";
test.beforeEach(() => {
  test.skip(!MEASURE, "量測用,不是護欄;要跑帶 T48_RENDER_COST=1");
});
import { ChatRenderCostStory } from "./stories/ChatRenderCostStory";
import type { RenderCostVariant } from "./stories/ChatRenderCostStory";

const NS = [100, 500, 1000, 3000, 8000];
const WIDTHS = [
  { name: "narrow-390", px: 390 },
  { name: "wide-1280", px: 1280 },
];

type Row = Record<string, unknown>;

async function measure(
  page: import("@playwright/test").Page,
  mount: (c: JSX.Element) => Promise<unknown>,
  n: number,
  widthPx: number,
  variant: RenderCostVariant,
): Promise<Row> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  await cdp.send("HeapProfiler.enable");
  await cdp.send("HeapProfiler.collectGarbage");
  const before = await cdp.send("Performance.getMetrics");
  const pick = (m: { metrics: { name: string; value: number }[] }, k: string) =>
    m.metrics.find((x) => x.name === k)?.value ?? -1;

  await mount(
    <ChatRenderCostStory n={n} widthPx={widthPx} variant={variant} />,
  );

  // 「N 列全部進 DOM」的那一刻,由頁面自己戳,不從 node 端猜。
  await page.waitForFunction(
    (want) => {
      const rows = document.querySelectorAll(".chat__msg").length;
      const w = window as unknown as Record<string, unknown>;
      if (rows >= want && w.__rcDone === undefined) {
        w.__rcDone = performance.now();
      }
      return rows >= want;
    },
    n,
    { timeout: 180_000 },
  );

  const timing = await page.evaluate(() => {
    const w = window as unknown as Record<string, number>;
    const rc = (window as unknown as { __rc: { t0: number } }).__rc;
    return { t0: rc.t0, done: w.__rcDone };
  });

  const rowCount = await page.locator(".chat__msg").count();
  // 證明寬度旋鈕真的有作用(forward-walk story 的 flex 版本量到的是「怎麼調都
  // 273px」)。這個數字進報告,否則「窄寬都量了」是一句沒有證據的話。
  const paneWidth = await page.evaluate(
    () =>
      Math.round(
        (document.querySelector(".chat__messages") as HTMLElement)
          .getBoundingClientRect().width,
      ),
  );
  const extras = await page.evaluate(() => ({
    cards: document.querySelectorAll(".chat__msg--card").length,
    imgs: document.querySelectorAll(".chat__msg-image").length,
    domEls: document.querySelectorAll("*").length,
  }));
  const scrollHeight = await page.evaluate(
    () => (document.querySelector(".chat__messages") as HTMLElement).scrollHeight,
  );

  // 記憶體 / 節點數在任何互動探針之前就量,否則探針自己買進來的那一頁會混進來。
  await cdp.send("HeapProfiler.collectGarbage");
  const after = await cdp.send("Performance.getMetrics");

  // 「畫面還能不能用」:載完之後強制一次真的捲動 + 一次同步的版面讀取,
  // 量這一次互動的往返。落點刻意選中段 —— 捲到最頂會觸發 loadOlder,那是下一格。
  const scrollMs = await page.evaluate(() => {
    const el = document.querySelector(".chat__messages") as HTMLElement;
    const t = performance.now();
    el.scrollTop = Math.floor(el.scrollHeight * 0.5);
    void el.scrollHeight; // 強制版面
    el.dispatchEvent(new Event("scroll"));
    void el.scrollTop;
    return performance.now() - t;
  });

  // 增量成本:這條線已經有 N 列時,再 commit 一頁 100 列要多久。
  // 「跳轉之後一路撈到最新」就是這個動作重複 ⌈N/99⌉ 次,所以這一格才是那條
  // 走訪真正的單位成本。
  const incrMs = await page.evaluate(async (want) => {
    const el = document.querySelector(".chat__messages") as HTMLElement;
    const t = performance.now();
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll"));
    const deadline = t + 120_000;
    for (;;) {
      if (document.querySelectorAll(".chat__msg").length >= want) break;
      if (performance.now() > deadline) return -1;
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    }
    return performance.now() - t;
  }, rowCount + 100);

  const row: Row = {
    n,
    width: widthPx,
    variant,
    paneW: paneWidth,
    cards: extras.cards,
    imgs: extras.imgs,
    domEls: extras.domEls,
    scrollH: scrollHeight,
    rows: rowCount,
    loadMs: Math.round(timing.done - timing.t0),
    scrollMs: Math.round(scrollMs * 100) / 100,
    incrMs: Math.round(incrMs),
    heapMB:
      Math.round(((pick(after, "JSHeapUsedSize") as number) / 1048576) * 10) /
      10,
    heapDeltaMB:
      Math.round(
        (((pick(after, "JSHeapUsedSize") as number) -
          (pick(before, "JSHeapUsedSize") as number)) /
          1048576) *
          10,
      ) / 10,
    nodes: pick(after, "Nodes"),
    layoutMs:
      Math.round(
        ((pick(after, "LayoutDuration") as number) -
          (pick(before, "LayoutDuration") as number)) *
          1000,
      ),
    styleMs:
      Math.round(
        ((pick(after, "RecalcStyleDuration") as number) -
          (pick(before, "RecalcStyleDuration") as number)) *
          1000,
      ),
    scriptMs:
      Math.round(
        ((pick(after, "ScriptDuration") as number) -
          (pick(before, "ScriptDuration") as number)) *
          1000,
      ),
    taskMs:
      Math.round(
        ((pick(after, "TaskDuration") as number) -
          (pick(before, "TaskDuration") as number)) *
          1000,
      ),
  };
  console.log("RCROW " + JSON.stringify(row));
  return row;
}

for (const w of WIDTHS) {
  for (const n of NS) {
    test(`render-cost N=${n} ${w.name}`, async ({ mount, page }) => {
      test.setTimeout(240_000);
      await page.setViewportSize({ width: Math.max(w.px, 400), height: 800 });
      const row = await measure(page, mount, n, w.px, "plain");
      expect(row.rows).toBe(n);
    });
  }
}

for (const variant of ["cards", "images"] as RenderCostVariant[]) {
  test(`render-cost N=1000 wide-1280 variant=${variant}`, async ({
    mount,
    page,
  }) => {
    test.setTimeout(240_000);
    await page.setViewportSize({ width: 1280, height: 800 });
    const row = await measure(page, mount, 1000, 1280, variant);
    expect(row.rows).toBe(1000);
  });
}

// 🔴 走訪的真正成本是累積的,不是單頁的。
// 「跳轉之後一路撈到最新」= 對同一條線 commit ⌈N/100⌉ 次,每一次 React 都要
// 重跑整條(沒有 virtualization、沒有 memo),所以總成本是 N 的二次式而不是
// 線性。這一格直接量:從 100 列開始,一次 100 列地長到 N,總共花多久。
// (用 loadOlder 當載具 —— 貼在頭或貼在尾對 render 成本是同一件事。)
for (const target of [1000, 3000, 8000]) {
  test(`walk-cost 100→${target} wide-1280`, async ({ mount, page }) => {
    test.setTimeout(600_000);
    await page.setViewportSize({ width: 1280, height: 800 });
    await mount(<ChatRenderCostStory n={100} widthPx={1280} variant="plain" />);
    await page.waitForFunction(
      () => document.querySelectorAll(".chat__msg").length >= 100,
    );
    const out = await page.evaluate(async (want) => {
      const el = document.querySelector(".chat__messages") as HTMLElement;
      const t0 = performance.now();
      let pages = 0;
      let worstMs = 0;
      for (;;) {
        const have = document.querySelectorAll(".chat__msg").length;
        if (have >= want) break;
        const p0 = performance.now();
        el.scrollTop = 0;
        el.dispatchEvent(new Event("scroll"));
        const deadline = performance.now() + 60_000;
        for (;;) {
          if (document.querySelectorAll(".chat__msg").length > have) break;
          if (performance.now() > deadline) return { pages, totalMs: -1, worstMs };
          await new Promise((r) => requestAnimationFrame(() => r(null)));
        }
        const dt = performance.now() - p0;
        if (dt > worstMs) worstMs = dt;
        pages += 1;
      }
      return {
        pages,
        totalMs: Math.round(performance.now() - t0),
        worstMs: Math.round(worstMs),
      };
    }, target);
    const rows = await page.locator(".chat__msg").count();
    console.log(
      "RCWALK " + JSON.stringify({ target, rows, ...out }),
    );
    expect(out.totalMs).toBeGreaterThan(0);
  });
}

// 🔴 「從按下跳轉到畫面出現」 —— owner 要看的那個數字。
// 撈完才 render,所以使用者盯著載入畫面的秒數 = ⌈N/99⌉ 通往返 + 一次 render。
// 兩種網路:0ms(純 render 下限)與 40ms/通(一個實測過的往返量級)。
for (const lat of [0, 40]) {
  for (const n of [500, 1000, 3000, 8000]) {
    test(`jump-to-paint N=${n} latency=${lat}ms wide-1280`, async ({
      mount,
      page,
    }) => {
      test.setTimeout(300_000);
      await page.setViewportSize({ width: 1280, height: 800 });
      await mount(
        <ChatRenderCostStory
          n={n}
          widthPx={1280}
          anchorIndex={0}
          windowLatencyMs={lat}
        />,
      );
      const out = await page.evaluate(async (want) => {
        const t0 = (window as unknown as { __rc: { t0: number } }).__rc.t0;
        const deadline = performance.now() + 240_000;
        for (;;) {
          if (document.querySelectorAll(".chat__msg").length >= want) break;
          if (performance.now() > deadline) return { ms: -1, spinner: false };
          await new Promise((r) => requestAnimationFrame(() => r(null)));
        }
        return { ms: Math.round(performance.now() - t0), spinner: false };
      }, n);
      const rows = await page.locator(".chat__msg").count();
      console.log(
        "RCJUMP " +
          JSON.stringify({ n, latency: lat, pages: Math.ceil(n / 99), rows, ...out }),
      );
      expect(rows).toBe(n);
    });
  }
}
