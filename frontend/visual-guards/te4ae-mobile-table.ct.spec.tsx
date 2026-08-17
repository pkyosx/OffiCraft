// HOTSPOT — T-e4ae task-card markdown tables on phone widths.
//
// The chosen C1 posture is content-aware, not "every cell gets 96px": the
// renderer measures each column's unwrapped intrinsic width, short columns
// keep that width, and longer columns receive a 96px floor at <=720px.
//
// MUTANTS (each assertion is load-bearing):
//   · remove TaskCard's tableSizing opt-in → sizing marker / table scroller and
//     one-line headers disappear;
//   · remove the phone th nowrap rule → header line count goes red;
//   · set every cell to min-width:96px → the first short # column becomes wide,
//     which the short-column assertion catches;
//   · remove the existing T-4aa0 ancestor constraint → the tasks list overflow
//     assertion goes red;
//   · clip or remove table overflow → tableOver / scrollLeft goes red.
//
// This is a real TaskCard in the real app ancestor chain, measured in Chromium;
// jsdom cannot observe any of the width or scroll contracts below.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardSixColTableStory } from "./stories/TaskCardSixColTableStory";

async function mountExpanded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardSixColTableStory />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
  await expect(
    cmp.locator('.task-card__desc table[data-md-table-sizing="content-aware"]')
  ).toBeVisible();
  await page.waitForFunction(() => {
    const cell = document.querySelector(
      '.task-card__desc table[data-md-table-sizing="content-aware"] td'
    ) as HTMLElement | null;
    return !!cell?.style.getPropertyValue("--md-table-column-min-width");
  });
  return cmp;
}

async function measure(page: any) {
  return page.evaluate(() => {
    const q = (selector: string) =>
      document.querySelector(selector) as HTMLElement | null;
    const table = q('.task-card__desc table[data-md-table-sizing="content-aware"]');
    const card = q(".task-card");
    const list = q(".tasks");
    const desc = q(".task-card__desc");
    const pageScroll = document.scrollingElement!;
    const ths = table ? (Array.from(table.querySelectorAll("th")) as HTMLElement[]) : [];
    const tds = table ? (Array.from(table.querySelectorAll("td")) as HTMLElement[]) : [];
    const lineCount = (el: HTMLElement) => {
      const cs = getComputedStyle(el);
      const lineHeight = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.2;
      const inner =
        el.getBoundingClientRect().height -
        parseFloat(cs.paddingTop) -
        parseFloat(cs.paddingBottom) -
        parseFloat(cs.borderTopWidth) -
        parseFloat(cs.borderBottomWidth);
      return Math.max(1, Math.round(inner / lineHeight));
    };
    return {
      tablePresent: !!table,
      tableOver: table ? table.scrollWidth - table.clientWidth : -2,
      tableScrollWidth: table?.scrollWidth ?? -2,
      tableClientWidth: table?.clientWidth ?? -2,
      thLines: ths.map(lineCount),
      thWidths: ths.map((th) => Math.round(th.getBoundingClientRect().width)),
      tdMaxLines: tds.length ? Math.max(...tds.map(lineCount)) : -2,
      listOver: list ? list.scrollWidth - list.clientWidth : -2,
      pageOver: pageScroll.scrollWidth - pageScroll.clientWidth,
      cardOver: card ? card.scrollWidth - card.clientWidth : -2,
      descOver: desc ? desc.scrollWidth - desc.clientWidth : -2,
    };
  });
}

for (const width of [390, 320]) {
  test(`${width}px: C1 task table stays readable and owns its scroll`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width);
    const before = await measure(page);

    expect(before.tablePresent, `[${width}px] table fixture disappeared`).toBe(true);
    expect(
      before.thLines.every((lines: number) => lines === 1),
      `[${width}px] header line counts: ${before.thLines.join(",")}`
    ).toBe(true);
    expect(
      before.tdMaxLines,
      `[${width}px] long cells were not given the C1 readable floor`
    ).toBeLessThanOrEqual(8);
    expect(
      before.thWidths[0],
      `[${width}px] short first column was widened like every-cell H1/H2`
    ).toBeLessThan(70);
    expect(
      before.thWidths.slice(1).some((width: number) => width >= 90),
      `[${width}px] no long column received the 96px floor`
    ).toBe(true);

    // Non-vacuity: the table must genuinely overflow, otherwise a fix that
    // merely leaves the current squeezed table in place would pass the outer
    // no-scroll checks while the user still cannot reach the missing columns.
    expect(before.tableOver, `[${width}px] table did not become horizontally scrollable`).toBeGreaterThan(0);
    expect(before.tableScrollWidth).toBeGreaterThan(before.tableClientWidth);

    const scrollLeft = await page.evaluate(() => {
      const table = document.querySelector(
        '.task-card__desc table[data-md-table-sizing="content-aware"]'
      ) as HTMLElement | null;
      if (!table) return -1;
      table.scrollLeft = 50;
      return table.scrollLeft;
    });
    expect(scrollLeft, `[${width}px] table scrollLeft did not retain 50`).toBe(50);

    // T-4aa0's contract remains intact: only the table is allowed to scroll.
    expect(before.listOver, `[${width}px] task list horizontal overflow`).toBeLessThanOrEqual(1);
    expect(before.pageOver, `[${width}px] page horizontal overflow`).toBeLessThanOrEqual(1);
    expect(before.cardOver, `[${width}px] card horizontal overflow`).toBeLessThanOrEqual(1);
    expect(before.descOver, `[${width}px] description escaped its card`).toBeLessThanOrEqual(1);
  });
}

test("390px: an over-floor heading stays on one line", async ({ mount, page }) => {
  await mountExpanded(mount, page, 390);
  await page.evaluate(() => {
    const heading = document.querySelector(
      '.task-card__desc table[data-md-table-sizing="content-aware"] th:last-child'
    ) as HTMLElement | null;
    if (heading) heading.textContent = "這是一個超過九十六像素的欄位標題";
  });
  const lines = await page.evaluate(() => {
    const heading = document.querySelector(
      '.task-card__desc table[data-md-table-sizing="content-aware"] th:last-child'
    ) as HTMLElement | null;
    if (!heading) return -1;
    const cs = getComputedStyle(heading);
    const lineHeight = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.2;
    const inner =
      heading.getBoundingClientRect().height -
      parseFloat(cs.paddingTop) -
      parseFloat(cs.paddingBottom) -
      parseFloat(cs.borderTopWidth) -
      parseFloat(cs.borderBottomWidth);
    return Math.max(1, Math.round(inner / lineHeight));
  });
  expect(lines, "long heading disappeared from the fixture").toBe(1);
});
