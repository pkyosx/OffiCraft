// The search results list as laid out by chat-search.css in a real browser:
// jsdom cannot tell a bottom sheet from a side panel.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ChatSuggestedRepliesStory } from "./stories/ChatSuggestedRepliesStory";

const now = Math.floor(Date.now() / 1000);
const thread = Array.from({ length: 30 }, (_, i) => ({
  id: `c-sheet-${i}`,
  from: i % 3 ? "rd" : "owner",
  to: i % 3 ? "owner" : "rd",
  body: `第 ${i + 1} 次 deploy 的紀錄`,
  ts: now - (i + 1) * 600,
  attachments: [],
  replyCardId: null,
}));

async function openResults(page: Page) {
  await page.getByTestId("chat-header-search").click();
  const input = page.locator(".chat-search__input");
  await input.fill("deploy");
  await input.press("Enter");
  await page.getByRole("button", { name: "搜尋結果列表" }).click();
  await page.locator(".chat-search__item").first().waitFor();
}

test("390px: the search text is 16px so iOS does not zoom on focus; the results rise from the bottom edge, full width, at most 70% tall, and the list scrolls inside", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<ChatSuggestedRepliesStory name="GF Eva Slack Inbox" thread={thread} />);
  await openResults(page);
  await expect(page.locator(".chat-search__input")).toHaveCSS("font-size", "16px");

  const sheet = (await page.locator(".chat-search__sheet").boundingBox())!;
  expect(sheet.x).toBe(0);
  expect(sheet.width).toBe(390);
  expect(sheet.y + sheet.height).toBeCloseTo(844, 0);
  expect(sheet.height).toBeLessThanOrEqual(844 * 0.7 + 0.5);
  const backdrop = (await page.getByTestId("chat-search-sheet-backdrop").boundingBox())!;
  expect(backdrop).toEqual({ x: 0, y: 0, width: 390, height: 844 });
  const list = page.locator(".chat-search__sheet .chat-search__list");
  expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);

  await page.mouse.click(195, 40);
  await expect(page.locator(".chat-search__sheet")).toHaveCount(0);
});

test("1280px: the search text stays 14px and the results stay a side panel that leaves the search box uncovered", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory name="GF Eva Slack Inbox" thread={thread} />);
  await openResults(page);
  await expect(page.locator(".chat-search__input")).toHaveCSS("font-size", "14px");

  const host = (await cmp.boundingBox())!;
  const box = (await page.locator(".chat-search__box").boundingBox())!;
  const panel = (await page.locator(".chat-search__panel").boundingBox())!;
  expect(panel.width).toBe(340);
  expect(panel.x + panel.width).toBeCloseTo(host.x + host.width, 0);
  expect(panel.y).toBeGreaterThanOrEqual(box.y + box.height);
  await expect(page.locator(".chat-search__sheet")).toHaveCount(0);
});
