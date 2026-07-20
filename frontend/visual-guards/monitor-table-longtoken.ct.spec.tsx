// HOTSPOT — monitor tables, phone card mode, long-token overflow (T-d451).
//
// The SECOND root cause behind owner's "手機頁面會左右滑動" report, and the one
// the `.doc-md` base fix cannot reach: these tables render no markdown.
//
// `monitor.css`'s `@media (max-width: 720px)` block converts each row into a
// card and deliberately drops the desktop wrap's `overflow-x: auto` (no phantom
// scrollbar inside a card). That leaves nothing to absorb a too-wide cell: an
// unbreakable value pushes the PAGE sideways. Measured at 375px before the fix:
// machines +448px, sessions +436px.
//
// Fix under test (monitor.css, inside the same @media block): `.mon-table td`
// gets `overflow-wrap: anywhere` (on the CELL, so it inherits into bare-text
// values too), and `.mon-machine-id` gives up its desktop `nowrap`. NOT
// "restore overflow-x: auto" — that would silence the page scroll by regrowing
// the phantom scrollbar the card mode exists to remove.
//
// DESKTOP IS A CONTROL, not an afterthought: at 1280px the wrap must STILL own
// a real horizontal scroll (that is the desktop design). A fix that made the
// desktop table wrap instead of scroll would pass a page-only assertion.
//
// MUTANTS (each verified red) — both rules are
// load-bearing, and each is pinned by a DIFFERENT fixture in the story:
//   drop `.mon-table td { overflow-wrap }`        → 375px page red, +259px
//     (pinned by LONG_SESSION, a bare-text cell with no break opportunity)
//   drop `.mon-machine-id { white-space: normal }` → 375px page red, +66px
//     (pinned by LONG_MACHINE_ID inside the id chip)
// Removing either fixture would silently retire the matching mutant, so keep
// both when editing the story.
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorTableLongTokenStory } from "./stories/MonitorTableLongTokenStory";

async function mountAt(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<MonitorTableLongTokenStory />);
  await expect(cmp.locator('[data-surface="machines"]')).toBeVisible();
  return cmp;
}

async function pageOverflow(page: any): Promise<number> {
  return page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
}

test("375px: long ids/model names in card mode never scroll the page", async ({
  mount,
  page,
}) => {
  await mountAt(mount, page, 375);

  const over = await pageOverflow(page);
  expect(
    over,
    `[375px] page must have no horizontal scroll (got +${over}px)`
  ).toBeLessThanOrEqual(1);

  // The card mode must NOT have regrown a scroller to hide the overflow — the
  // value has to actually break. This is what separates the real fix from
  // "put overflow-x: auto back".
  for (const sel of ['[data-surface="machines"]', '[data-surface="sessions"]']) {
    const el = page.locator(sel);
    await expect(el, `[375px] ${sel} missing`).toBeAttached();
    const ox = await el.evaluate((n: HTMLElement) => getComputedStyle(n).overflowX);
    expect(
      ox,
      `[375px] ${sel} must stay scroll-free in card mode (phantom scrollbar)`
    ).toBe("visible");
  }
});

test("1280px: the desktop table keeps its own horizontal scroll", async ({
  mount,
  page,
}) => {
  await mountAt(mount, page, 1280);

  const over = await pageOverflow(page);
  expect(
    over,
    `[1280px] page must have no horizontal scroll (got +${over}px)`
  ).toBeLessThanOrEqual(1);

  // Desktop control: the wrap owns the scroll. If this flips to `visible` the
  // desktop table silently lost its scroller.
  const ox = await page
    .locator('[data-surface="machines"]')
    .evaluate((n: HTMLElement) => getComputedStyle(n).overflowX);
  expect(ox, "[1280px] desktop wrap must own its horizontal scroll").toBe(
    "auto"
  );
});
