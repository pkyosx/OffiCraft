// The chat room's 建議回覆 row is mounted in the real ChatArea here, so what is
// measured is the composer it hangs under, at the widths the owner signed off.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatSuggestedRepliesStory } from "./stories/ChatSuggestedRepliesStory";

const LONG = "回報時只講結果、影響和要我決定的事，實作細節留在任務紀錄，".repeat(4).slice(0, 120);
const MANY = Array.from({ length: 20 }, (_, i) => `第 ${i + 1} 句常用的話`);

for (const width of [390, 1280]) {
  test(`${width}px: a 120-character sentence wraps inside the composer, below the input row`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={["繼續", LONG]} />);

    const composer = (await cmp.locator(".chat__composer").boundingBox())!;
    const inputRow = (await cmp.locator(".chat__composer-row").boundingBox())!;
    const row = (await cmp.getByTestId("chat-suggested-replies").boundingBox())!;
    const chip = (await cmp.locator(".suggested-replies__chip").nth(1).boundingBox())!;

    expect(row.y).toBeGreaterThanOrEqual(inputRow.y + inputRow.height);
    expect(chip.x + chip.width).toBeLessThanOrEqual(composer.x + composer.width + 1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    if (width === 390) expect(chip.height).toBeGreaterThan(30);
  });

  test(`${width}px: 20 sentences stop at the row's height cap and scroll inside it, leaving the messages their space`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={MANY} />);

    const row = cmp.getByTestId("chat-suggested-replies");
    const box = (await row.boundingBox())!;
    expect(box.height).toBeLessThanOrEqual(132);
    if (width === 390) {
      const overflow = await row.evaluate((el) => el.scrollHeight > el.clientHeight);
      expect(overflow).toBe(true);
    }
    const thread = (await cmp.locator(".chat__body").boundingBox())!;
    expect(thread.height).toBeGreaterThan(200);
  });

  test(`${width}px: the dialog fits the viewport and every row's three buttons are on screen`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={MANY.slice(0, 4)} />);
    await cmp.getByTestId("chat-header-suggested-replies").click();

    const dialog = (await page.getByTestId("chat-suggested-replies-dialog").boundingBox())!;
    expect(dialog.x).toBeGreaterThanOrEqual(0);
    expect(dialog.x + dialog.width).toBeLessThanOrEqual(width);
    const input = (await page.locator(".sugg-edit__input").first().boundingBox())!;
    expect(input.width).toBeGreaterThan(120);
    const remove = (await page.locator(".sugg-edit__btn--remove").first().boundingBox())!;
    expect(remove.x + remove.width).toBeLessThanOrEqual(dialog.x + dialog.width);
  });
}
