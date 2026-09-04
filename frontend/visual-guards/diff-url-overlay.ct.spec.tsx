// GUARD (T-59) — the comparison's geometry INSIDE the studio's modal, and the
// two behaviours that only a real browser can settle.
//
// What this covers that nothing else does: `DiffView` already has two real
// browser guards (`diff-view.ct.spec.tsx`, `diff-view-split.ct.spec.tsx`), but
// both mount it in the version-history context. T-59 gave it a SECOND host —
// the preview overlay panel, which is narrower, portalled to <body>, and has
// its own scroll container. A comparison table is the widest thing this panel
// will ever hold, and "does it burst the panel sideways" is exactly the class
// jsdom is blind to: it applies no CSS, so every width it reports is 0.
//
// Colour is deliberately NOT re-asserted here. This package added no colour
// rules, and the two sibling guards already measure DiffView's resolved fills
// against the theme; repeating that would be a second copy that can disagree.
//
// The narrow width matters most: at 390 the two-column table cannot fit, so
// the contract is that it scrolls INSIDE `.diff-view__scroll` rather than
// pushing the page or the panel. Both are asserted, because a panel that stays
// put while the document scrolls sideways is still a broken screen.
import { test, expect } from "@playwright/experimental-ct-react";
import { DiffUrlOverlayStory } from "./stories/DiffUrlOverlayStory";

for (const width of [390, 1280]) {
  test(`width ${width}: the comparison stays inside the panel and its overflow stays reachable`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });

    await mount(<DiffUrlOverlayStory />);

    // The overlay portals to document.body — reach it through `page`.
    const diff = page.getByTestId("diff-screen");
    await expect(diff).toBeVisible();
    // A real comparison of the two RESOLVED sides, in the right direction.
    await expect(
      diff.locator('[data-kind="removed"] .diff-view__text').first(),
    ).toHaveText("bravo");
    await expect(
      diff.locator('[data-kind="added"] .diff-view__text').first(),
    ).toHaveText("BRAVO");

    const overflow = await page.evaluate(() => {
      const de = document.documentElement;
      const panel = document.querySelector(".md-preview__panel") as HTMLElement | null;
      return {
        page: de.scrollWidth - de.clientWidth,
        panel: panel ? panel.scrollWidth - panel.clientWidth : -1,
        panelRight: panel ? panel.getBoundingClientRect().right : -1,
      };
    });
    expect(overflow.page, "the document must not scroll sideways").toBe(0);
    expect(overflow.panel, "the panel must not scroll sideways").toBe(0);
    expect(overflow.panelRight).toBeLessThanOrEqual(width + 1);

    // 🔴 THE TWO ZEROES ABOVE ARE NOT ENOUGH, AND THE MUTANT IS WHY. Dropping
    // `overflow-x: auto` off `.diff-view__scroll` leaves both of them at 0 —
    // an ancestor is `overflow: hidden`, so the table simply gets CLIPPED
    // (measured: 1883px of table inside a 282px box) and the reader silently
    // loses the right-hand columns with no way to reach them. A guard that
    // only asks "did anything burst" calls that a pass.
    //
    // So assert REACHABILITY instead of tidiness: when the table is wider than
    // its box, scrolling that box must actually move it.
    const reach = await page.evaluate(() => {
      const box = document.querySelector(".diff-view__scroll") as HTMLElement | null;
      if (!box) return null;
      const overflowing = box.scrollWidth - box.clientWidth;
      box.scrollLeft = box.scrollWidth;
      return { overflowing, moved: box.scrollLeft };
    });
    expect(reach, ".diff-view__scroll must exist").not.toBeNull();
    if (reach!.overflowing > 0) {
      expect(
        reach!.moved,
        "a table wider than its box must be SCROLLABLE, not clipped",
      ).toBeGreaterThan(0);
    }
  });
}

// Same geometry contract, but with the headings the READER writes when the url
// carried none — which is the normal case for a document side, because the
// server deliberately sends no label for one. 「目前存檔內容（讀取於 …，之後會不一
// 樣）」 is several times wider than 「改動前」, and it is the widest thing a column
// header will ever hold. The
// claim this exists to convert into evidence is "the package added no CSS, so
// there is no theme or layout risk": true about the stylesheet, and not by
// itself an argument about a heading that did not exist before.
for (const width of [390, 1280]) {
  test(`width ${width}: the reader's own long headings do not burst the panel`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });

    await mount(<DiffUrlOverlayStory variant="reader-labels" />);

    await expect(page.getByTestId("diff-screen")).toBeVisible();
    // The live side is marked AND dated, in the real rendered DOM.
    await expect(
      page.getByText(/目前存檔內容（讀取於 .+，之後會不一樣）/),
    ).toBeVisible();

    const overflow = await page.evaluate(() => {
      const de = document.documentElement;
      const panel = document.querySelector(".md-preview__panel") as HTMLElement | null;
      return {
        page: de.scrollWidth - de.clientWidth,
        panel: panel ? panel.scrollWidth - panel.clientWidth : -1,
      };
    });
    expect(overflow.page, "the document must not scroll sideways").toBe(0);
    expect(overflow.panel, "the panel must not scroll sideways").toBe(0);
  });
}

/* ── 兩份都要是連結 (owner 2026-09-03, c-944088dceab0) ────────────────────────
 *
 * What a compare url names has been two ADDRESSES from the start — that is why
 * nothing is copied and no side can drift. What the owner asked for is the
 * other half of that sentence: from the comparison, be able to go and read one
 * side on its own.
 *
 * Asserted here rather than in jsdom because the thing that must be true is
 * "the reader ends up looking at that side's text": the switch is a real click
 * on a real heading inside the real overlay, and the text that comes up has to
 * be the SAME bytes the diff was drawn from — not a re-read, which could answer
 * differently a second later and leave the two screens disagreeing about what
 * "this side" says.
 *
 * MUTANT (run, verified red): pass `pair.after` where the single-side view
 * reads `pair.before` → "should be showing the BEFORE side alone" fails on the
 * BRAVO/bravo pair. The two sides differ by case only on purpose — a mutant
 * that swaps them must not be able to hide behind similar-looking text. */
test("each side heading opens THAT side on its own, and comes back", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });

  await mount(<DiffUrlOverlayStory />);
  await expect(page.getByTestId("diff-screen")).toBeVisible();

  await page.getByTestId("diff-view-side-before").click();
  const side = page.getByTestId("diff-screen-side");
  await expect(side).toBeVisible();
  await expect(page.getByTestId("diff-screen-side-title")).toHaveText("改動前");
  // The heading alone would pass on the wrong side's text; the CONTENT is what
  // says which side the reader is actually looking at.
  await expect(side.locator("pre")).toContainText("bravo");
  await expect(side.locator("pre")).not.toContainText("BRAVO");
  // The comparison is gone while one side is open — two diff surfaces on screen
  // at once would leave "which one am I reading" to the reader.
  await expect(page.getByTestId("diff-screen")).toHaveCount(0);

  await page.getByTestId("diff-screen-side-back").click();
  await expect(page.getByTestId("diff-screen")).toBeVisible();

  await page.getByTestId("diff-view-side-after").click();
  await expect(page.getByTestId("diff-screen-side-title")).toHaveText("改動後");
  await expect(page.getByTestId("diff-screen-side").locator("pre")).toContainText("BRAVO");
});

/* Coming back must land in the layout the reader LEFT. Opening a side unmounts
 * DiffView, so a mode kept inside it comes back as 單欄 — measured, not
 * theorised: the first cut of this feature did exactly that and the browser
 * check caught it. It is the same shape of quiet wrongness as the off-screen
 * column: the control you pressed silently stops being pressed.
 *
 * MUTANT (run, verified red): drop `mode`/`onModeChange` from the DiffView call
 * in DiffScreen (back to component-local state) → "expected data-mode=split,
 * got unified". */
test("returning from a side keeps the comparison layout the reader chose", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });

  await mount(<DiffUrlOverlayStory />);
  const diff = page.getByTestId("diff-screen");
  await expect(diff).toHaveAttribute("data-mode", "unified");

  await page.getByTestId("diff-view-mode-split").click();
  await expect(diff).toHaveAttribute("data-mode", "split");

  await page.getByTestId("diff-view-side-before").click();
  await expect(page.getByTestId("diff-screen-side")).toBeVisible();
  await page.getByTestId("diff-screen-side-back").click();

  await expect(page.getByTestId("diff-screen")).toHaveAttribute("data-mode", "split");
});

/* Esc offers the SAME way out the 「回到比較」 button does. The stylesheet beside
 * that button already argues why closing is wrong here — the reader would have
 * to re-open the link and find their layout again — and a keyboard user was
 * getting exactly that, because Esc went straight to onClose.
 *
 * It is also the one thing about the compare screen's Esc layer that only the
 * real DOM can settle: which layer wins is read off DOM CONTAINMENT, and the
 * side pane is nested inside the overlay by the portal, not by the JSX.
 *
 * MUTANT (run, verified red): drop the `useEscapeLayer` call in DiffScreen →
 * the single-side pane stays open and the comparison never comes back. */
test("Esc from a single side goes BACK to the comparison, not out", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });

  await mount(<DiffUrlOverlayStory />);
  await page.getByTestId("diff-view-side-before").click();
  await expect(page.getByTestId("diff-screen-side")).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(page.getByTestId("diff-screen")).toBeVisible();
  await expect(page.getByTestId("diff-screen-side")).toHaveCount(0);
});
