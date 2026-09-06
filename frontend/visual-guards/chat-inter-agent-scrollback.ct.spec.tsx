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
  for (let turn = 0; turn < 20; turn += 1) {
    await box.hover();
    await page.mouse.wheel(0, -900);
    await page.waitForTimeout(120);
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
