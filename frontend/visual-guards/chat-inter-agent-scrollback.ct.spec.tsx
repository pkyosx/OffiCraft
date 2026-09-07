// T-124 — 往上捲要載得到更舊的訊息,即使目前這一頁幾乎整頁都是「成員間對話」。
//
// 🔴 WHAT ONLY A BROWSER CAN ANSWER. 載入更舊那一頁只由 scroll 事件觸發,而一個
// 內容比視窗還短的捲動盒**永遠不會發出 scroll 事件** —— 滾輪轉多用力都一樣。
// 一頁 30 則裡有 28 則是成員間對話時,那 28 列被摺成三條一行的塊,整頁畫出來比
// 面板還短,於是使用者往上滑,什麼都不會發生。jsdom 沒有版面(每個盒子都是 0px)
// 也沒有真的滾輪,兩半都看不到。
//
// 這支 guard 用的是 owner 在 c-fc47d568cc14 拍到的那個組成:14 成員間 + 1 + 12
// 成員間 + 1 + 2 成員間。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatInterAgentScrollbackStory } from "./stories/ChatInterAgentScrollbackStory";
import { NORMAL_TOTAL } from "./stories/chatInterAgentScrollbackFixtures";

test("一頁幾乎全是成員間對話時,往上捲仍然載得到更舊的訊息", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mount(<ChatInterAgentScrollbackStory />);

  const box = page.locator(".chat__messages");
  await expect(box).toBeVisible();
  // 開場就是那個組成:三條摺疊塊 + 兩則 owner↔成員的訊息。
  await expect(page.locator(".chat__inter-toggle")).toHaveCount(3);
  const bubbles = page.locator(".chat__msg");
  await expect(bubbles).toHaveCount(2);

  // 量一件事實,不是猜的:這一頁畫出來比面板還短 ⇒ 捲不動 ⇒ 不會有 scroll 事件。
  const geom = await box.evaluate((el) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }));

  // 照使用者的方式轉滾輪:往上,連續轉。這裡沒有任何一行用手寫 scrollTop ——
  // 手寫 scrollTop 會自己製造出那個「不存在」的 scroll 事件,等於把要測的東西
  // 先幫它做掉。
  // 每一「次」滑動之間留一段安靜的間隔 —— 一次手勢一頁(owner 圈定
  // rc-3bceed6d9e0a),所以連續不斷的滾輪事件算同一次手勢,只買一頁。
  for (let turn = 0; turn < 20; turn += 1) {
    await box.hover();
    await page.mouse.wheel(0, -900);
    await page.waitForTimeout(300);
  }

  const loaded = await bubbles.count();
  expect(
    loaded,
    `往上捲 20 次之後只載到 ${loaded} 則 owner↔成員訊息 ` +
      `(scrollHeight=${geom.scrollHeight}, clientHeight=${geom.clientHeight})`,
  ).toBe(NORMAL_TOTAL);

  // …而且它會停:歷史撈完之後畫的是「已到最早訊息」,不是繼續空轉。
  await expect(page.locator(".chat__history-start")).toBeVisible();
});

// 同一個死路的另一半:手機／平板(owner 圈定 rc-b5b3c307b90d 要一起補)。
//
// 🔴 手指從來不會發出滾輪事件,而在一個捲不動的面板上也不會發出捲動事件 ——
// 量過的:同一個手機尺寸下,一般對話用同樣的手勢可以從 30 則走到 120 則(走的是
// 捲動那道門),這一種組成卻是從頭到尾都停在 2 則。所以桌面那道門修好之後,手機
// 仍然是原本那條死路。
test.describe("手機／平板", () => {
  test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 780 } });

  test("一頁幾乎全是成員間對話時,手指往下滑仍然載得到更舊的訊息", async ({
    mount,
    page,
  }) => {
    await mount(<ChatInterAgentScrollbackStory widthPx={390} />);

    const box = page.locator(".chat__messages");
    await expect(box).toBeVisible();
    const bubbles = page.locator(".chat__msg");
    await expect(bubbles).toHaveCount(2);

    // 真的觸控輸入,經由 CDP 送進瀏覽器 —— 不是在頁面裡自己 dispatch 一個假的
    // TouchEvent(那種假事件不會有原生捲動,等於在測自己造的東西)。
    const b = (await box.boundingBox())!;
    const client = await page.context().newCDPSession(page);
    const x = b.x + b.width / 2;
    for (let swipe = 0; swipe < 8; swipe += 1) {
      await client.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x, y: b.y + 80 }],
      });
      for (let step = 1; step <= 8; step += 1) {
        await client.send("Input.dispatchTouchEvent", {
          type: "touchMove",
          touchPoints: [{ x, y: b.y + 80 + step * 70 }],
        });
      }
      await client.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await page.waitForTimeout(150);
    }

    const loaded = await bubbles.count();
    expect(loaded, `手指往下滑八次之後只載到 ${loaded} 則 owner↔成員訊息`).toBe(
      NORMAL_TOTAL,
    );
    await expect(page.locator(".chat__history-start")).toBeVisible();
  });
});

// 一次手勢一頁(owner 圈定 rc-3bceed6d9e0a)。他在試用站上滑一次,一口氣多了不只
// 30 則 —— 一次滑動在瀏覽器眼裡是一串事件,而這種摺疊對話載完幾乎不長高、畫面
// 一直停在最上面,於是那一串裡的每一個事件都各買一頁(實量:一次滑動買走三頁)。
test("一次滑動只買一頁,不管那一次滑動送出幾個滾輪事件", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mount(<ChatInterAgentScrollbackStory />);

  const box = page.locator(".chat__messages");
  const bubbles = page.locator(".chat__msg");
  await expect(bubbles).toHaveCount(2);
  await box.hover();

  // 一次觸控板滑動 = 一串幾乎沒有間隔的事件。這些 wheel 呼叫要一起送出、最後才
  // 一起等 —— 一個一個 await 的話,每一次來回本身就是幾十毫秒的停頓,量到的會是
  // 「連續好幾次滑動」而不是一次(實量:一起送 ⇒ 事件間隔 0–17ms)。
  const flick = async () => {
    const jobs: Promise<void>[] = [];
    for (let i = 0; i < 12; i += 1) jobs.push(page.mouse.wheel(0, -120));
    await Promise.all(jobs);
    await page.waitForTimeout(1200);
  };

  // 一頁 = 30 則 = 這個組成裡的 2 則 owner↔成員訊息。
  await flick();
  const loaded = await bubbles.count();
  expect(loaded, `一次滑動之後載到 ${loaded} 則 owner↔成員訊息`).toBe(4);

  // 而且它只是「一次一頁」,不是「只有一頁」:下一次滑動照樣買得到。
  await flick();
  expect(await bubbles.count()).toBe(6);
});
