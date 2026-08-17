// HOTSPOT — 寬螢幕聊天回覆卡的時間戳貼齊卡片 (T-47cd).
//
// OWNER REPORT: 在寬螢幕聊天中，時間戳被推到畫面最右側，離所屬回覆卡很遠；
// 手機版目前仍須維持卡片全寬、時間戳換行的行為。
//
// ROOT CAUSE (measured at 1440px): the real `.app__main` ancestor chain gives
// `.chat__msg-line` the whole message-column width while the reply card stays
// capped at 480px. The timestamp then follows the row edge instead of the card.
// This CT story keeps that production ancestor chain, including `.app__main`
// padding, because jsdom cannot resolve the flex layout or viewport geometry.
//
// MUTANT (§5): change the desktop `width: max-content` declarations in
// `.chat__msg--card .chat__msg-line` / `.chat__msg-content` back to `100%`.
// The 1440px card-to-timestamp gap assertion must turn red; restoring the fix
// turns it green. The container overflow assertion measures `.chat__messages`
// itself, not the document surface hidden by its `overflow-x` clamp.
// Observed runs: normal targeted CT was 3/3 green; the desktop mutant made the
// wide assertion red (gap 304.0625) while phone stayed green. A separate mutant
// removing both <=720px `width: 100%` overrides left both timestamp tests green;
// `max-width: 100%` clamps `max-content` back into the phone container.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatCardTimestampStory } from "./stories/ChatCardTimestampStory";

for (const viewport of [
  { name: "wide", width: 1440, layout: "wide" },
  { name: "phone", width: 390, layout: "" },
]) {
  test(`${viewport.name}: the timestamp stays with its chat card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport.width, height: 900 });
    const cmp = await mount(<ChatCardTimestampStory />);
    await page.evaluate((layout) => {
      document.documentElement.dataset.layout = layout;
    }, viewport.layout);
    const messages = cmp.getByTestId("chat-messages");
    const card = cmp.locator(".reply-card--chat");
    const time = cmp.getByTestId("chat-card-time");
    await expect(messages).toBeVisible();
    await expect(card).toBeVisible();
    await expect(time).toBeVisible();

    const cardBox = await card.boundingBox();
    const timeBox = await time.boundingBox();
    if (!cardBox || !timeBox) throw new Error("chat card has no layout box");

    if (viewport.layout === "wide") {
      const gap = timeBox.x - (cardBox.x + cardBox.width);
      expect(
        gap,
        `timestamp must be adjacent to the card, not the full message row (gap=${gap})`,
      ).toBeGreaterThanOrEqual(-1);
      expect(gap, "timestamp must stay attached to the card").toBeLessThanOrEqual(12);
    } else {
      expect(timeBox.x, "phone timestamp must stay inside the card width").toBeGreaterThanOrEqual(
        cardBox.x - 1,
      );
      expect(timeBox.x + timeBox.width, "phone timestamp must not overflow the card").toBeLessThanOrEqual(
        cardBox.x + cardBox.width + 1,
      );
    }

    const chatOverflow = await messages.evaluate(
      (el) => el.scrollWidth - el.clientWidth,
    );
    expect(chatOverflow, "chat messages must not overflow their scroll box").toBeLessThanOrEqual(1);
  });
}
