// HOTSPOT — T-5e79 (owner 2026-08-04 截圖): on the role-journal Insight card the
// 「預設」 pill and the 「編輯」 button opposite it both had their two CJK glyphs
// broken onto TWO stacked lines that spilled out of the fixed-height box —
// the pill looked like a burst capsule with a character above and below it.
//
// MEASURED on the pre-fix sheet (this same story, real Chromium):
//   width  badge lines  badge text height / box 19px   編輯 lines / text height
//   320    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   360    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   375    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   390    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   414+   1            13px, inside the pill             1   16px, inside
// So this is a NARROW-WIDTH-ONLY defect: it is structurally invisible at desktop
// widths, and it is also structurally invisible to the vitest/jsdom suite, which
// applies no layout engine — `insight-status-badge` is in the DOM either way.
//
// WHAT IS ASSERTED IS GEOMETRY THE OWNER CAN SEE, not CSS property strings:
//   (1) the label occupies exactly ONE line box, and
//   (2) every one of its line boxes stays INSIDE the element's own border box.
// A `white-space` value read back from getComputedStyle would be satisfied by a
// property that is set but out-cascaded, or by a fix that stops the wrap and
// still bursts the box; these two do not.
//
// Both widths are exercised on purpose. The narrow ones are where the owner
// saw it; the wide one is the control that says the fix did not simply move the
// breakage somewhere else.
//
// MUTANT (re-measured by an independent reviewer, superseding this comment's
// first version — it said 375/390 and that undercounted):
//   · delete `white-space: nowrap` from `.set-badge` → assertion (1) for the
//     badge reddens at 320 AND 375 AND 390 (3 widths, `expected 1, received 2`).
//   · delete it from `.doc-btn` → the 編輯 assertions redden at 375/390 only.
//     320 stays green there, because the <360px wrap valve puts the button on a
//     line of its own where it has room — the valve MASKS that one width.
//   · delete the wrap valve → the header-overflow assertion reddens at 320 with
//     `received 23`.
//
// ⚠️ THREE ASSERTIONS HERE CAN NEVER RUN WHILE THE DEFECT IS PRESENT, and that
// is worth knowing before you trust them: the spill checks and the 編輯
// label-height check all sit AFTER the `lines` assertion for the same element,
// so when the bug is live the `lines` check throws first and they are never
// reached. They are not vacuous — the reviewer proved they discriminate by
// re-running the pre-fix mutant with the `lines` assertions temporarily relaxed
// (spill measured 6px above the pill, matching the reported defect). But their
// mutation coverage is contributed by the `lines` assertions, not by themselves.
// This is the "assertions shielding each other" trap the repo already records
// for the long-token guards; the technique for re-testing them is the same.
//
// The 1040 sub-test is green under EVERY mutant above. That is honest and
// intended — it is the control that answers "did the fix move the breakage to
// desktop widths", a different question — but it contributes ZERO mutation
// detection and must not be counted as coverage.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import { InsightBadgeNarrowStory } from "./stories/InsightBadgeNarrowStory";

/** Line boxes + vertical spill of an element's own text, measured with a Range
 * (one client rect per line box) against the element's border box. */
async function textGeometry(el: Locator) {
  return await el.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(node);
    const rects = Array.from(range.getClientRects());
    return {
      lines: rects.length,
      spillAbove: box.top - Math.min(...rects.map((r) => r.top)),
      spillBelow: Math.max(...rects.map((r) => r.bottom)) - box.bottom,
    };
  });
}

// 375 / 390 = the phone widths the rest of this suite treats as the owner's
// (nav-tabs-narrow, worker-detail-header-label, …) and where the defect was
// measured. 320 = the narrowest phone still in use, and the only width where
// the fix needed member-detail.css's ≤359px wrap valve to stay inside the card.
// 1040 = the desktop content column's max width — the control that says the
// fix did not move the breakage somewhere else.
for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: factory-status badge and 編輯 button keep their labels on one line, inside their own box`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<InsightBadgeNarrowStory />);

    // The badge only renders for a role with a factory insight seed; if this
    // ever stops resolving, the guard must fail loudly rather than silently
    // measure nothing.
    const badge = cmp.getByTestId("insight-status-badge");
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText("與預設內容同步");

    const badgeGeo = await textGeometry(badge);
    expect(badgeGeo.lines, "factory-status badge label line boxes").toBe(1);
    expect(badgeGeo.spillAbove, "factory-status label spilling above the pill").toBeLessThanOrEqual(0.5);
    expect(badgeGeo.spillBelow, "factory-status label spilling below the pill").toBeLessThanOrEqual(0.5);

    const editLabel = cmp.locator(".doc-btn--edit span");
    await expect(editLabel).toHaveText("編輯");
    const editGeo = await textGeometry(editLabel);
    expect(editGeo.lines, "編輯 button label line boxes").toBe(1);

    // The button's own 30px box must still contain the label it was sized for.
    const editBtn = cmp.locator(".doc-btn--edit");
    const btnBox = (await editBtn.boundingBox())!;
    const labelBox = (await editLabel.boundingBox())!;
    expect(labelBox.height, "編輯 label height vs its 30px button").toBeLessThanOrEqual(
      btnBox.height
    );

    // The no-shrink rules must not have bought the fix with an overflow
    // somewhere else: the header row, the card, and the page all stay put.
    const spill = await page.evaluate(() => {
      const head = document.querySelector(".mp-lessons__head")!;
      const card = document.querySelector(".mp-insight")!;
      return {
        head: head.scrollWidth - head.clientWidth,
        card: card.scrollWidth - card.clientWidth,
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    expect(spill.head, "header row horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.card, "insight card horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.page, "page horizontal overflow").toBeLessThanOrEqual(1);
  });
}
