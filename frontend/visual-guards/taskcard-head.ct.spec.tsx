// Task card head line + 更多 section, measured in real layout (jsdom has none).
//
// Contract:
//   · the head keeps ONE line — 狀態 icon · #ID · 優先權 · 轉派中 and the ▸
//     expand mark share a row — at 320 / 390 / 1440, for every status. The one
//     exception is the rare 轉派中 lock at 320, which has no room (measured:
//     the four badges need 259px of a 216px row) and wraps to a second line;
//     狀態 icon · #ID · 優先權 still share the first;
//   · nothing in the head crosses the card's right edge and the page never
//     scrolls sideways;
//   · every status icon renders at the same size, in its status colour;
//   · the 更多 section, expanded at 390, keeps every row inside the card.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { TaskCardHeadStory } from "./stories/TaskCardHeadStory";

async function pageOverflow(page: Page) {
  return page.evaluate(() => {
    const de = document.documentElement;
    const list = document.querySelector(".tasks") as HTMLElement | null;
    if (!list) return { missing: true, page: -2, tasks: -2 };
    return {
      missing: false,
      page: de.scrollWidth - de.clientWidth,
      tasks: list.scrollWidth - list.clientWidth,
    };
  });
}

async function headGeometry(card: Locator) {
  return card.evaluate((el: Element) => {
    const box = (n: Element) => n.getBoundingClientRect();
    const row = el.querySelector(".task-card__badge-row")!;
    const items = [
      ...row.children,
      el.querySelector(".task-card__expand-mark")!,
    ].map((n) => box(n));
    const icon = el.querySelector(".task-card__status-icon svg")!;
    const cardBox = box(el);
    return {
      itemCount: row.children.length,
      centers: items.map((r) => r.top + r.height / 2),
      maxRight: Math.max(...items.map((r) => r.right)),
      cardRight: cardBox.right,
      cardOverflow: (el as HTMLElement).scrollWidth - (el as HTMLElement).clientWidth,
      icon: { w: box(icon).width, h: box(icon).height },
    };
  });
}

for (const width of [320, 390, 1440]) {
  test(`width ${width}: every card head stays on one line, inside the card, with a same-size status icon`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<TaskCardHeadStory />);
    const cards = cmp.locator(".task-card");
    await expect(cards).toHaveCount(8);

    const first = await headGeometry(cards.first());
    expect(first.itemCount, "status · #ID · priority · lock").toBe(4);

    for (let i = 0; i < 8; i++) {
      const g = await headGeometry(cards.nth(i));
      const lockWraps = width === 320 && i === 0;
      const sameLine = lockWraps ? g.centers.slice(0, 3) : g.centers;
      const spread = Math.max(...sameLine) - Math.min(...sameLine);
      expect(spread, `card ${i}: head items share one line`).toBeLessThanOrEqual(3);
      expect(g.maxRight, `card ${i}: head stays inside the card`).toBeLessThanOrEqual(
        g.cardRight + 0.5
      );
      expect(g.cardOverflow, `card ${i}: no sideways overflow`).toBe(0);
      expect(g.icon, `card ${i}: icon size`).toEqual({ w: 18, h: 18 });
    }

    const colours = await cmp
      .locator(".task-card__status-icon")
      .evaluateAll((els) => els.map((el) => getComputedStyle(el).color));
    expect(colours[0], "進行中 and 已完成 share the accent").toBe(colours[5]);
    expect(new Set(colours).size, "the other statuses each have their own colour").toBe(7);

    const overflow = await pageOverflow(page);
    expect(overflow.missing).toBe(false);
    expect(overflow.page).toBe(0);
    expect(overflow.tasks).toBe(0);
  });
}

test("width 390: the expanded 更多 section keeps every row inside the card", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<TaskCardHeadStory />);
  const card = cmp.locator(".task-card").first();
  await card.getByTestId("task-meta-more-toggle").click();
  await expect(card.getByTestId("task-type")).toBeVisible();
  await expect(card.getByTestId("task-artifacts-badge")).toBeVisible();
  await expect(card.getByTestId("task-creator")).toBeVisible();
  await expect(card).toHaveAttribute("aria-expanded", "false");

  const g = await card.evaluate((el) => {
    const cardBox = el.getBoundingClientRect();
    const rows = [...el.querySelectorAll(".task-card__more > *, .task-card__more .task-card__artifacts-row > *")];
    return {
      rowCount: rows.length,
      maxRight: Math.max(...rows.map((n) => n.getBoundingClientRect().right)),
      cardRight: cardBox.right,
      cardOverflow: (el as HTMLElement).scrollWidth - (el as HTMLElement).clientWidth,
    };
  });
  expect(g.rowCount).toBeGreaterThanOrEqual(6);
  expect(g.maxRight).toBeLessThanOrEqual(g.cardRight + 0.5);
  expect(g.cardOverflow).toBe(0);

  const overflow = await pageOverflow(page);
  expect(overflow.missing).toBe(false);
  expect(overflow.page).toBe(0);
  expect(overflow.tasks).toBe(0);
});
