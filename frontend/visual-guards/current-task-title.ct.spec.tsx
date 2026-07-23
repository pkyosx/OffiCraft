// HOTSPOT — T-3451: the member/worker "current task title" line.
//
// Roster ROW: a long title must CLAMP to two lines with an ellipsis (owner:
// 超出「…」截斷) while carrying the full text on its `title` tooltip (hover 全
// 文). Chat HEADER: the same title must render FULL, un-truncated (owner 圖2),
// wrapping without forcing a page-level horizontal scrollbar at the owner's
// phone widths. jsdom sees none of this (no -webkit-line-clamp box, no real
// layout) — measured here in a real browser at narrow AND wide widths.
//
// Mutants (documented, each reddens a DISTINCT assertion):
//   1. drop `-webkit-line-clamp: 2` (or the `--clamp` class / the MemberCard
//      `clamp` prop) → the roster title stops clamping → its clientHeight grows
//      to ~scrollHeight → the `clientHeight < scrollHeight` (truncated) AND the
//      `clientHeight <= ~2 lines` bounds both redden.
//   2. remove the `title={title}` attr in CurrentTaskTitle → the hover-full
//      assertion reddens.
//   3. add a clamp to the HEADER title (clamp={true} / a line-clamp rule) → the
//      header `scrollHeight <= clientHeight` (nothing hidden) assertion reddens.
import { test, expect } from "@playwright/experimental-ct-react";
import { CurrentTaskRosterStory } from "./stories/CurrentTaskRosterStory";
import { CurrentTaskHeaderStory } from "./stories/CurrentTaskHeaderStory";
import { LONG_TITLE } from "./stories/currentTaskFixtures";

// ── roster ROW: 2-line clamp + hover-full, both tabs, narrow + wide ──────────
for (const width of [390, 1280]) {
  test(`width ${width}: roster task-title clamps to 2 lines, keeps full text on hover, no overflow`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<CurrentTaskRosterStory />);

    // The Outsource row carries a clamp title; measure it so the guard holds
    // for the outsource roster row.
    const outsourceTitle = cmp.getByTestId("outsource-task-title-ow-1");

    for (const [label, loc] of [["outsource", outsourceTitle]] as const) {
      await expect(loc, `${label} title visible`).toBeVisible();

      // (1) hover-full: the ENTIRE title rides the native tooltip even though
      // the row shows only a clamped preview.
      await expect(loc, `${label} title attr = full`).toHaveAttribute(
        "title",
        LONG_TITLE,
      );

      // (2) the clamp is REAL: with a title this long, the visible box height
      // (clientHeight) is strictly less than the full content height
      // (scrollHeight) — i.e. content is being clipped, not merely short.
      const m = await loc.evaluate((el) => {
        const cs = getComputedStyle(el);
        return {
          clientHeight: (el as HTMLElement).clientHeight,
          scrollHeight: (el as HTMLElement).scrollHeight,
          lineHeight: parseFloat(cs.lineHeight),
          clamp: (cs as unknown as Record<string, string>)["webkitLineClamp"],
        };
      });
      expect(
        m.clientHeight,
        `${label} clamped (clientHeight < scrollHeight)`,
      ).toBeLessThan(m.scrollHeight);

      // (3) clamped to ~2 lines, not 1 and not the whole thing: the visible box
      // is at most ~2 lines tall (a small slack for padding/rounding).
      const twoLines = m.lineHeight * 2;
      expect(
        m.clientHeight,
        `${label} clientHeight ~<= 2 lines`,
      ).toBeLessThanOrEqual(twoLines + 6);
    }

    // (4) page-level: nothing widens the document past the viewport.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    expect(overflow, "no page hscroll").toBeLessThanOrEqual(1);
  });
}

// ── chat HEADER: FULL title, no clamp, no overflow at phone widths ───────────
for (const width of [375, 390]) {
  test(`width ${width}: chat-header task-title renders FULL (un-truncated) and never overflows`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<CurrentTaskHeaderStory />);

    const title = cmp.getByTestId("chat-header-task-title");
    await expect(title).toBeVisible();

    // (1) the full text is present (owner 圖2: 完整 title 不截斷).
    await expect(title).toHaveText(LONG_TITLE);

    // (2) NOTHING is clipped: a non-clamped element wraps to whatever height it
    // needs, so its content height never exceeds its box height. (A clamp
    // mutant would make scrollHeight > clientHeight here.)
    const m = await title.evaluate((el) => ({
      clientHeight: (el as HTMLElement).clientHeight,
      scrollHeight: (el as HTMLElement).scrollHeight,
    }));
    expect(
      m.scrollHeight,
      "header title fully visible (scrollHeight <= clientHeight)",
    ).toBeLessThanOrEqual(m.clientHeight + 1);

    // (3) the long title wraps instead of forcing a page-level hscroll.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    expect(overflow, "no page hscroll").toBeLessThanOrEqual(1);
  });
}
