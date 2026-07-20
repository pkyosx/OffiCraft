// HOTSPOT — 外包 side row shows 「外包 · 代號」 (T-3ed8) and stays inside the rail.
//
// Line 1 of an outsource row widened from the bare codename ("O-30") to the
// outsource identity label ("外包 · O-30"). The rail is a fixed 264px grid
// track (.office__members); a label that spilled past its right edge, or forced
// a page-level horizontal scrollbar, is a CSS-layout regression jsdom cannot
// see. Measured here in a real browser at desktop AND phone widths.
import { test, expect } from "@playwright/experimental-ct-react";
import { OutsourceRowLabelStory } from "./stories/OutsourceRowLabelStory";

for (const width of [1280, 390]) {
  test(`width ${width}: 外包 rows show 「外包 · 代號」 and never overflow the rail`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<OutsourceRowLabelStory />);

    const rows = cmp.locator(".outsource-row");
    const n = await rows.count();
    expect(n).toBeGreaterThan(0);

    // (1) the label is the 「外包 · 代號」 form, not the bare codename — the
    // behavior this guard exists to hold.
    await expect(rows.first().locator(".outsource-row__codename")).toHaveText(
      "外包 · O-30"
    );

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
