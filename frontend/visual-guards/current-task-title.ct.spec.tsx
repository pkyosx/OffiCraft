// HOTSPOT — the member/worker "current task title" line (T-3451, T-769).
//
// Roster ROW and chat HEADER: a long title CLAMPS to two lines with an ellipsis
// (owner: 顯示兩行，超過就…截掉) while carrying the full text on its `title`
// tooltip (hover 全文), and never forces a page-level horizontal scrollbar.
// jsdom sees none of this (no -webkit-line-clamp box, no real layout) — measured
// here in a real browser at narrow AND wide widths.
//
// Mutants (documented, each reddens a DISTINCT assertion):
//   1. drop `-webkit-line-clamp: 2` (or the `--clamp` class / the `clamp` prop
//      on either surface) → the title stops clamping → its clientHeight grows
//      to ~scrollHeight → the `clientHeight < scrollHeight` (truncated) AND the
//      `clientHeight <= ~2 lines` bounds both redden.
//   2. remove the `title={title}` attr in CurrentTaskTitle → the hover-full
//      assertion reddens.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { CurrentTaskRosterStory } from "./stories/CurrentTaskRosterStory";
import { CurrentTaskHeaderStory } from "./stories/CurrentTaskHeaderStory";
import { LONG_TITLE, VERY_LONG_TITLE } from "./stories/currentTaskFixtures";

async function expectClampedTwoLinesWithHoverFull(
  page: Page,
  loc: Locator,
  label: string,
  fullTitle: string,
) {
  await expect(loc, `${label} title visible`).toBeVisible();
  await expect(loc, `${label} title attr = full`).toHaveAttribute("title", fullTitle);

  const m = await loc.evaluate((el) => ({
    clientHeight: (el as HTMLElement).clientHeight,
    scrollHeight: (el as HTMLElement).scrollHeight,
    lineHeight: parseFloat(getComputedStyle(el).lineHeight),
  }));
  // With a title this long, a real clamp clips content (not merely short text).
  expect(m.clientHeight, `${label} clamped (clientHeight < scrollHeight)`).toBeLessThan(
    m.scrollHeight,
  );
  expect(m.clientHeight, `${label} clientHeight ~<= 2 lines`).toBeLessThanOrEqual(
    m.lineHeight * 2 + 6,
  );

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, "no page hscroll").toBeLessThanOrEqual(1);
}

for (const width of [390, 1280]) {
  test(`width ${width}: roster task-title clamps to 2 lines, keeps full text on hover, no overflow`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<CurrentTaskRosterStory />);
    await expectClampedTwoLinesWithHoverFull(
      page,
      cmp.getByTestId("outsource-task-title-ow-1"),
      "outsource roster",
      LONG_TITLE,
    );
  });
}

for (const width of [375, 390, 1280]) {
  test(`width ${width}: chat-header task-title clamps to 2 lines, keeps full text on hover, no overflow`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<CurrentTaskHeaderStory />);
    await expectClampedTwoLinesWithHoverFull(
      page,
      cmp.getByTestId("chat-header-task-title"),
      "chat header",
      VERY_LONG_TITLE,
    );
  });
}
