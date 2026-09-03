// GUARD (T-59) — the compare attachment's geometry INSIDE the preview overlay.
//
// What this covers that nothing else does: `DiffView` already has two real
// browser guards (`diff-view.ct.spec.tsx`, `diff-view-split.ct.spec.tsx`), but
// both mount it in the version-history context. T-59 gave it a SECOND host —
// the preview overlay panel, which is narrower, portalled to <body>, and has
// its own scroll container. A comparison table is the widest thing this panel
// will ever hold, and "does it burst the panel sideways" is exactly the class
// jsdom is blind to: it applies no CSS, so every width it reports is 0.
//
// Colour is deliberately NOT re-asserted here. This package added no CSS, and
// the two sibling guards already measure DiffView's resolved fills against the
// theme; repeating that would be a second copy that can disagree.
//
// The narrow width matters most: at 390 the two-column table cannot fit, so
// the contract is that it scrolls INSIDE `.diff-view__scroll` rather than
// pushing the page or the panel. Both are asserted, because a panel that stays
// put while the document scrolls sideways is still a broken screen.
import { test, expect } from "@playwright/experimental-ct-react";
import { DiffAttachmentOverlayStory } from "./stories/DiffAttachmentOverlayStory";

const BEFORE = ["alpha", "bravo", "charlie", "delta ".repeat(40)].join("\n");
const AFTER = ["alpha", "BRAVO", "charlie", "delta ".repeat(40)].join("\n");

for (const width of [390, 1280]) {
  test(`width ${width}: the comparison stays inside the panel and its overflow stays reachable`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.route("**/api/chat/attachment/**", (route) => {
      const url = route.request().url();
      const body = url.includes("att-0123456789ab") ? BEFORE : AFTER;
      return route.fulfill({ status: 200, contentType: "text/plain", body });
    });

    await mount(<DiffAttachmentOverlayStory />);

    // The overlay portals to document.body — reach it through `page`.
    const diff = page.getByTestId("md-preview-diff");
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

// T-59 second round. Same geometry contract, but with the headings the reader
// writes for itself when a side names a document and carries no label of its
// own — 「目前存檔內容（讀取於 …，之後會不一樣）」 is several times wider than
// 「改動前」, and it is the widest thing a column header will ever hold. The
// claim this exists to convert into evidence is "the package added no CSS, so
// there is no theme or layout risk": true about the stylesheet, and not by
// itself an argument about a heading that did not exist before.
for (const width of [390, 1280]) {
  test(`width ${width}: the reader's own long headings do not burst the panel`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.route("**/api/chat/attachment/**", (route) =>
      route.fulfill({ status: 200, contentType: "text/plain", body: BEFORE }),
    );

    await mount(<DiffAttachmentOverlayStory variant="docs" />);

    const diff = page.getByTestId("md-preview-diff");
    await expect(diff).toBeVisible();
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
