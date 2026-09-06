// T-124 — 載入那一頁落地的時候,讀的人不能被搬走(owner c-c9cd7fefe19f:
// 「載入時 我正在開的位置或手機手指指的位置都會跑掉的感覺」)。
//
// 🔴 WHAT ONLY A BROWSER CAN ANSWER. 這是「畫面上那一列還在不在原來的位置」的問
// 題:要有真的版面(每一列有真的高度)、真的捲動、以及一段真的等待時間 —— 請求在
// 飛的時候讀的人還在滑,而被還原掉的正是那一段。jsdom 沒有版面,scrollTop 是測試
// 自己寫上去的,量不到「那一列被搬走了幾像素」。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatThreadLoadingStory } from "./stories/ChatThreadLoadingStory";

/** 落地前後都問同一個問題:這一列離視窗頂端幾像素。 */
const OFFSET_OF = (id: string) => {
  const el = document.querySelector<HTMLElement>(".chat__messages")!;
  const row = el.querySelector<HTMLElement>(`[data-msg-id="${id}"]`);
  if (!row) return null;
  return row.getBoundingClientRect().top - el.getBoundingClientRect().top;
};

// 2.5 秒:一段真的有寬度的等待,讓「觸發 → 讀的人繼續滑 → 落地」三件事分得開,
// 量測不必跟落地賽跑。
const LATENCY_MS = 2500;

async function armAndKeepScrolling(page: import("@playwright/test").Page) {
  const box = page.locator(".chat__messages");
  await box.hover();
  // 滑進觸發帶(還沒到頂),載入出發。
  await page.mouse.wheel(0, -1200);
  await page.waitForTimeout(60);
  // 它還在飛的時候,手指/慣性繼續往上帶到最頂。
  for (let i = 0; i < 6; i += 1) {
    await page.mouse.wheel(0, -60);
    await page.waitForTimeout(40);
  }
}

test("請求在飛的時候讀的人還在滑,那一頁落地不會把他搬回去", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mount(
    <ChatThreadLoadingStory
      entrance="plain"
      widthPx={1280}
      latencyMs={LATENCY_MS}
    />,
  );

  const rows = page.locator("[data-msg-id]");
  await expect.poll(async () => await rows.count()).toBe(30);
  await page.waitForTimeout(400);

  await armAndKeepScrolling(page);

  // 落地之前:記下畫面最上面那一列是誰、它離視窗頂端幾像素。
  const before = await page.locator(".chat__messages").evaluate((el) => {
    const top = el.getBoundingClientRect().top;
    for (const row of el.querySelectorAll<HTMLElement>("[data-msg-id]")) {
      const r = row.getBoundingClientRect();
      if (r.bottom > top) return { id: row.dataset.msgId!, offset: r.top - top };
    }
    return { id: "", offset: 0 };
  });
  expect(before.id, "落地前應該還看得到某一列").not.toBe("");
  expect(await rows.count(), "這一格必須在那一頁落地之前量到").toBe(30);

  await expect.poll(async () => await rows.count()).toBe(60);
  await page.waitForTimeout(200);

  const after = await page.evaluate(OFFSET_OF, before.id);
  expect(after, `落地後找不到落地前那一列 ${before.id}`).not.toBeNull();
  // 同一列,同一個位置。舊作法會把讀的人搬回請求送出時的位置 —— 實量差 79px。
  expect(
    Math.abs(after! - before.offset),
    `那一列被搬了 ${Math.round(after! - before.offset)}px`,
  ).toBeLessThanOrEqual(2);
});

// 🔴 這一支釘的是「多出來的高度是量出來的,不是用高度快照減出來的」。
// 用快照相減只有在「飛行期間沒有別的東西改變高度」時才等於那一頁的高度 —— 圖片
// 載完、字型換掉、SSE 進來一則新訊息都會打破它,而打破的樣子跟上面那個 bug 一模
// 一樣:讀的人被搬走,沒有任何東西會紅(Kyle c-e7503b079bc0 要求釘住這一格)。
test("飛行期間別的東西改變了高度,讀的人一樣不會被搬走", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mount(
    <ChatThreadLoadingStory
      entrance="plain"
      widthPx={1280}
      latencyMs={LATENCY_MS}
    />,
  );

  const rows = page.locator("[data-msg-id]");
  await expect.poll(async () => await rows.count()).toBe(30);
  await page.waitForTimeout(400);

  await armAndKeepScrolling(page);

  const before = await page.locator(".chat__messages").evaluate((el) => {
    const top = el.getBoundingClientRect().top;
    for (const row of el.querySelectorAll<HTMLElement>("[data-msg-id]")) {
      const r = row.getBoundingClientRect();
      if (r.bottom > top) return { id: row.dataset.msgId!, offset: r.top - top };
    }
    return { id: "", offset: 0 };
  });
  expect(before.id).not.toBe("");

  // 飛行期間,讀的人「下面」某一列長高了 200px —— 一張圖載完就是這個形狀。它在
  // 讀的人下方,所以不該讓他移動一個像素;而高度快照相減會把這 200px 也算成那一
  // 頁的高度。
  await page.locator(".chat__messages").evaluate((el) => {
    const all = el.querySelectorAll<HTMLElement>("[data-msg-id]");
    const grown = all[all.length - 1];
    grown.style.minHeight = `${grown.getBoundingClientRect().height + 200}px`;
  });
  expect(await rows.count(), "這一格必須在那一頁落地之前量到").toBe(30);

  await expect.poll(async () => await rows.count()).toBe(60);
  await page.waitForTimeout(200);

  const after = await page.evaluate(OFFSET_OF, before.id);
  expect(after).not.toBeNull();
  expect(
    Math.abs(after! - before.offset),
    `那一列被搬了 ${Math.round(after! - before.offset)}px`,
  ).toBeLessThanOrEqual(2);
});
