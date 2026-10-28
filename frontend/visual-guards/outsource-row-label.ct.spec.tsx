// HOTSPOT — 外包 side row shows the bare codename and stays inside the rail.
//
// The rail is a fixed 264px grid track (.office__members); a line 1 that
// spilled past its right edge, or forced a page-level horizontal scrollbar, is
// a CSS-layout regression jsdom cannot see. Measured here in a real browser at
// desktop AND phone widths.
import { test, expect } from "@playwright/experimental-ct-react";
import { OutsourceRowLabelStory } from "./stories/OutsourceRowLabelStory";

for (const width of [1280, 390]) {
  test(`width ${width}: 外包 rows show the bare codename and never overflow the rail`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<OutsourceRowLabelStory />);

    const rows = cmp.locator(".outsource-row");
    const n = await rows.count();
    expect(n).toBeGreaterThan(0);

    // (1) the tab already says 外包, so line 1 is the codename alone.
    await expect(rows.first().locator(".outsource-row__codename")).toHaveText("O-30");

    // (2) every row stays within the rail's right edge (no line-1 blow-out).
    const rail = cmp.locator(".office__members");
    const railBox = await rail.boundingBox();
    expect(railBox, "rail box").not.toBeNull();
    for (let i = 0; i < n; i++) {
      const b = await rows.nth(i).boundingBox();
      expect(b, `row ${i} box`).not.toBeNull();
      expect(
        b!.x + b!.width,
        `row ${i} right edge <= rail right edge`
      ).toBeLessThanOrEqual(railBox!.x + railBox!.width + 1);
    }

    // (3) page-level invariant: nothing widens the document past the viewport.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });
}
