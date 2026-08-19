// HOTSPOT — 寬螢幕聊天回覆卡的時間戳貼齊卡片 (T-47cd).
//
// OWNER REPORT: 在寬螢幕聊天中，時間戳被推到畫面最右側，離所屬回覆卡很遠；
// 手機版目前仍須維持卡片全寬、時間戳換行的行為。
//
// ROOT CAUSE (measured at 1440px): the real `.app__main` ancestor chain gives
// the message row a bounded column, but the reply card must consume that
// column before the timestamp is laid beside it. Keeping the card at an
// intrinsic/480px width leaves an empty row tail, so the timestamp follows
// the tail instead of the card. This CT story keeps that production ancestor
// chain, including `.app__main` padding, because jsdom cannot resolve the flex
// layout or viewport geometry.
//
// MUTANT (§5): two desktop mutants cover different guards, and the distinction
// is measured rather than inferred. M1 changes `.reply-card--chat { width: 100% }`
// back to `min(480px, 100%)`; the existing gap assertion turns red at
// `304.0625px`, so Playwright stops before evaluating the new card-width guard.
// M2 changes the card row/content widths from `100%` to `max-content`; the gap
// stays green at `6px`, while the new guard turns red (`418.96875px` received,
// `772.0625px` required). Thus the new guard's unique coverage is M2, not M1.
// The ≤720px width overrides duplicate the base `width: 100%`, so removing
// them is a no-op on this base; the wide and phone CT cases both pass (2
// passed). Keep the phone `flex-wrap: wrap` behavior. The container overflow
// assertion measures `.chat__messages` itself, not the document surface hidden
// by its `overflow-x` clamp.
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
    const messageBox = await cmp.getByTestId("chat-card-row").boundingBox();
    if (!cardBox || !timeBox) throw new Error("chat card has no layout box");

    if (viewport.layout === "wide") {
      const gap = timeBox.x - (cardBox.x + cardBox.width);
      expect(
        gap,
        `timestamp must be adjacent to the card, not the full message row (gap=${gap})`,
      ).toBeGreaterThanOrEqual(-1);
      expect(gap, "timestamp must stay attached to the card").toBeLessThanOrEqual(12);
      if (!messageBox) throw new Error("chat message has no layout box");
      expect(
        cardBox.width,
        "desktop card must fill the message column up to the timestamp",
      ).toBeGreaterThanOrEqual(messageBox.width - timeBox.width - 12);
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
