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
import type { Locator } from "@playwright/test";

/** The open card's divider: right under `above` (its last meta row, or the
 * title on a chat card with none) and the full width of the card's content, a
 * visible 1px line, with the question starting further below it than the
 * card's own 12px gap. */
async function expectDivider(card: Locator, above: Locator, tag: string) {
  const divider = card.locator(".reply-card__divider");
  await expect(divider, `${tag}: one divider`).toHaveCount(1);
  const g = await divider.evaluate((el, aboveEl) => {
    const r = (e: Element) => {
      const b = e.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const cardEl = el.closest(".reply-card")!;
    const cs = getComputedStyle(cardEl);
    const c = r(cardEl);
    return {
      line: r(el),
      prev: el.previousElementSibling === aboveEl,
      above: r(aboveEl as Element),
      next: el.nextElementSibling ? r(el.nextElementSibling) : null,
      contentLeft: c.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft),
      contentRight: c.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight),
      bg: getComputedStyle(el).backgroundColor,
    };
  }, await above.elementHandle());
  expect(g.prev, `${tag}: the divider comes right after the meta`).toBe(true);
  expect(g.line.bottom - g.line.top, `${tag}: a 1px line`).toBeCloseTo(1, 0);
  expect(g.line.top - g.above.bottom, `${tag}: space between the meta and the line`).toBeCloseTo(14, 0);
  expect(g.line.left, `${tag}: the line starts at the content's left`).toBeCloseTo(g.contentLeft, 0);
  expect(g.line.right, `${tag}: the line ends at the content's right`).toBeCloseTo(g.contentRight, 0);
  expect(g.bg, `${tag}: the line is drawn`).not.toMatch(/^rgba\(\d+, \d+, \d+, 0\)$|^transparent$/);
  expect(g.next, `${tag}: something under the line`).not.toBeNull();
  expect(g.next!.top - g.line.bottom, `${tag}: the question starts 18px under the line`).toBeCloseTo(18, 0);
}

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

for (const width of [390, 1280]) {
  test.describe(() => {
    test.use({ hasTouch: width <= 720 });
    test(`${width}px${width <= 720 ? ", touch" : ""}: the chat-card expiry icon button (${width <= 720 ? "26" : "22"}px) sits on the card's first line just before the chevron at its right end, inside the card above a summary with an unbreakable code block, and its confirmation fits the viewport`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ChatCardTimestampStory />);
      if (width === 1280) {
        await page.evaluate(() => {
          document.documentElement.dataset.layout = "wide";
        });
      }

      await cmp.getByTestId("chat-reply-card-expand").click();
      const card = cmp.getByTestId("chat-reply-card");
      const expire = card.getByRole("button", { name: "標為過期", exact: true });
      await expect(expire).toBeVisible();
      await expect(expire).toHaveAttribute("title", "標為過期");
      expect(
        await expire.evaluate((el) => el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight),
        "the expiry icon fits inside its button",
      ).toBe(true);
      const cardBox = await card.boundingBox();
      const expireBox = await expire.boundingBox();
      if (!cardBox || !expireBox) throw new Error("chat-card expiry action has no layout box");
      expect(await page.evaluate(() => matchMedia("(hover: none)").matches), "control: a touch screen").toBe(width <= 720);
      const side = width <= 720 ? 26 : 22;
      expect(expireBox.width, `the expiry button ${side}px wide`).toBeCloseTo(side, 0);
      expect(expireBox.height, `the expiry button ${side}px tall`).toBeCloseTo(side, 0);
      expect(expireBox.x).toBeGreaterThanOrEqual(cardBox.x - 1);
      expect(expireBox.x + expireBox.width).toBeLessThanOrEqual(
        cardBox.x + cardBox.width + 1,
      );
      const summaryBox = await card.locator(".reply-card__summary").boundingBox();
      const cardPaddingRight = await card.evaluate((el) =>
        parseFloat(getComputedStyle(el).paddingRight),
      );
      if (!summaryBox) throw new Error("chat-card summary has no layout box");
      expect(expireBox.y + expireBox.height, "expiry on the first line, above the summary").toBeLessThanOrEqual(
        summaryBox.y + 1,
      );
      const foldBox = (await card.locator(".reply-card__head .reply-card__fold").boundingBox())!;
      expect(Math.abs(expireBox.y + expireBox.height / 2 - (foldBox.y + foldBox.height / 2)), "expiry on the chevron's line").toBeLessThanOrEqual(1);
      expect(foldBox.x - (expireBox.x + expireBox.width), "expiry just before the chevron").toBeLessThanOrEqual(9);
      expect(
        cardBox.x + cardBox.width - (foldBox.x + foldBox.width),
        "the chevron is flush with the card's right padding",
      ).toBeLessThanOrEqual(cardPaddingRight + 2);
      const codeOver = await card
        .locator(".reply-card__summary pre")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(codeOver, "the code block is wider than the card and scrolls inside itself").toBeGreaterThan(0);
      const messagesOver = await cmp
        .getByTestId("chat-messages")
        .evaluate((el) => el.scrollWidth - el.clientWidth);
      expect(messagesOver, "the thread must not scroll sideways").toBeLessThanOrEqual(1);

      await expire.click();
      const dialog = cmp.getByTestId("expire-confirm");
      await expect(dialog).toBeVisible();
      const dialogBox = await dialog.boundingBox();
      if (!dialogBox) throw new Error("expiry confirmation has no layout box");
      expect(dialogBox.x).toBeGreaterThanOrEqual(0);
      expect(dialogBox.x + dialogBox.width).toBeLessThanOrEqual(width + 1);
      await cmp.getByTestId("expire-confirm-btn").click();
      await expect(expire).toHaveCount(0);
    });
  });
}

for (const width of [320, 390, 1440]) {
  for (const language of ["zh", "en"]) {
    test(`${width}px ${language}: the open chat card draws a line under its title, or under 處理時間 on an answered card, and its question starts below it; the folded row has none`, async ({
      mount,
      page,
    }) => {
      for (const answered of [false, true]) {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
        const cmp = await mount(<ChatCardTimestampStory answered={answered} />);
        const tag = `${width} ${language} ${answered ? "answered" : "waiting"}`;
        const expand = cmp.getByTestId("chat-reply-card-expand");
        await expect(expand).toBeVisible();
        await expect(cmp.locator(".reply-card__divider"), `${tag}: folded`).toHaveCount(0);
        await expand.click();
        const card = cmp.getByTestId("chat-reply-card");
        await expect(card.locator(".reply-card__body")).toBeVisible();
        await expectDivider(
          card,
          card.locator(answered ? ".reply-card__meta" : ".reply-card__summary"),
          tag,
        );
        await cmp.unmount();
      }
    });
  }
}
