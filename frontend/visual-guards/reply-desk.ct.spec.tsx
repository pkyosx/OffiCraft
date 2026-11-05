// The 請示 page's card head, task row and 稍後再看 menu, measured on the real
// page in the real content column. jsdom lays nothing out, so whether a CJK
// button label folds, a task row runs out of its card, or the phone sheet
// leaves the screen is only visible here.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { ReplyDeskStory } from "./stories/ReplyDeskStory";

type Box = { left: number; right: number; top: number; bottom: number };

async function box(l: Locator): Promise<Box> {
  return l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
  });
}

/** Line boxes the element's text occupies: text rects that overlap vertically
 * (a smaller font on the same line sits a few px lower) count as one line. */
async function textLines(l: Locator): Promise<number> {
  return l.evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const rects = [...range.getClientRects()]
      .filter((r) => r.width > 0 && r.height > 0)
      .sort((a, b) => a.top - b.top);
    let lines = 0;
    let bottom = -Infinity;
    for (const r of rects) {
      if (r.top >= bottom - 1) lines += 1;
      bottom = Math.max(bottom, r.bottom);
    }
    return lines;
  });
}

const inside = (inner: Box, outer: Box, slack = 0.5) =>
  inner.left >= outer.left - slack &&
  inner.right <= outer.right + slack &&
  inner.top >= outer.top - slack &&
  inner.bottom <= outer.bottom + slack;

async function noPageOverflow(page: Page) {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(m.scroll, "the page does not scroll sideways").toBeLessThanOrEqual(m.client);
}

for (const width of [390, 1440]) {
  test(`width ${width}: the head buttons sit together on one line, each label on one line, and the task row with its chips stays inside its card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ReplyDeskStory />);
    await expect(cmp.getByTestId("waiting-card")).toHaveCount(3);

    for (const card of await cmp.getByTestId("waiting-card").all()) {
      const cardBox = await box(card);
      const buttons = await card.locator(".reply-card__head .reply-card__jump").all();
      expect(buttons.length).toBe(3);
      const tops: number[] = [];
      for (const button of buttons) {
        expect(await textLines(button), `${await button.textContent()} stays on one line`).toBe(1);
        const b = await box(button);
        expect(inside(b, cardBox)).toBe(true);
        tops.push(b.top);
      }
      expect(inside(await box(card.getByTestId("waited")), cardBox), "the waited stamp inside the card").toBe(true);
      expect(Math.max(...tops) - Math.min(...tops), "跳到原訊息, 稍後再看 and 標為過期 share one line").toBeLessThanOrEqual(1);
      const row = card.getByTestId("reply-task-ref");
      if ((await row.count()) === 0) continue;
      const rowBox = await box(row);
      expect(inside(rowBox, cardBox), "task row inside the card").toBe(true);
      expect(rowBox.bottom, "task row above the head").toBeLessThanOrEqual((await box(card.locator(".reply-card__head"))).top);
      for (const part of await row.locator(".reply-card__task-chips > *, .reply-card__task-title, .reply-card__task-jump").all()) {
        expect(inside(await box(part), rowBox), `${await part.textContent()} inside the task row`).toBe(true);
      }
      for (const chip of await row.locator(".reply-card__task-chips > *").all()) {
        expect(await textLines(chip), `${await chip.textContent()} on one line`).toBe(1);
      }
      expect(await textLines(row.getByTestId("reply-task-jump"))).toBe(1);
    }
    await noPageOverflow(page);
  });

  test(`width ${width}: the 稍後再看 menu is on screen with every preset and the custom row inside it${
    width <= 720 ? " (a bottom sheet)" : ""
  }`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ReplyDeskStory />);
    const card = cmp.getByTestId("waiting-card").first();
    await card.getByTestId("snooze-card").click();
    const menu = page.locator('[data-testid^="snooze-menu-"]');
    await expect(menu).toBeVisible();
    const menuBox = await box(menu);
    const view = { left: 0, top: 0, right: width, bottom: 900 };
    expect(inside(menuBox, view), "the menu is on screen").toBe(true);
    if (width <= 720) {
      expect(Math.round(menuBox.bottom)).toBe(900);
      expect(Math.round(menuBox.right - menuBox.left)).toBe(width);
    }
    for (const part of await menu.locator(".reply-snooze__preset, .reply-snooze__custom-row > *").all()) {
      expect(inside(await box(part), menuBox), `${await part.textContent()} inside the menu`).toBe(true);
    }
    const tops = await menu
      .locator(".reply-snooze__custom-row > *")
      .evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().top + el.getBoundingClientRect().height / 2)));
    expect(Math.max(...tops) - Math.min(...tops), "the custom row is one line").toBeLessThanOrEqual(2);
    await menu.getByTestId("snooze-amount").fill("90");
    await menu.getByTestId("snooze-unit").selectOption("day");
    expect(await textLines(menu.getByTestId("snooze-preview"))).toBe(1);
    await noPageOverflow(page);
  });
}

for (const width of [1440, 1728]) {
  for (const language of ["zh", "en"]) {
    test(`width ${width}, ${language}: 跳到原訊息 and 標為過期 sit at the same x on every waiting card whatever the waited stamp's length, and the snoozed card's return date stays inside its card`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<ReplyDeskStory />);
      const cards = cmp.getByTestId("waiting-card");
      await expect(cards).toHaveCount(3);
      const lead = language === "zh" ? "已等你" : "Waiting";
      expect(await cmp.getByTestId("waited").allTextContents()).toEqual([`${lead} 5m`, `${lead} 2h 57m`, `${lead} 12d 23h`]);

      const jumpLefts: number[] = [];
      const expireLefts: number[] = [];
      for (const card of await cards.all()) {
        jumpLefts.push((await box(card.locator(".reply-card__actions > .reply-card__jump").first())).left);
        expireLefts.push((await box(card.getByTestId("expire-card"))).left);
      }
      expect(Math.max(...jumpLefts) - Math.min(...jumpLefts), `跳到原訊息 lefts ${jumpLefts}`).toBeLessThanOrEqual(0.5);
      expect(Math.max(...expireLefts) - Math.min(...expireLefts), `標為過期 lefts ${expireLefts}`).toBeLessThanOrEqual(0.5);

      await cmp.getByTestId("snoozed-toggle").click();
      const snoozed = cmp.getByTestId("snoozed-card");
      await expect(snoozed).toHaveCount(1);
      const snoozedBox = await box(snoozed);
      for (const part of await snoozed.locator(".reply-card__head .reply-card__jump, [data-testid='snoozed-until']").all()) {
        expect(inside(await box(part), snoozedBox), `${await part.textContent()} inside the snoozed card`).toBe(true);
        expect(await textLines(part), `${await part.textContent()} on one line`).toBe(1);
      }
      await noPageOverflow(page);
    });
  }
}
