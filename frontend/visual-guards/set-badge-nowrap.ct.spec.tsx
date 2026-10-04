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
// MUTANTS, measured with the header's ≤720px wrap in place:
//   · delete `white-space: nowrap` from `.set-badge` or from `.doc-btn` → every
//     test here stays GREEN. The wrap gives the badge and the 編輯 button room
//     of their own at every width tested, so these width tests no longer prove
//     the nowrap rules; they still matter wherever the pill or button sits in a
//     row that does not wrap.
//   · delete the ≤720px wrap rule → both edit-mode tests redden (title 3 line
//     boxes).
//   · let only the header wrap, not the title group → the en edit-mode test
//     reddens (title 3 line boxes).
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
 * against the element's border box. Lines are counted by distinct rect tops:
 * a label React renders as several text nodes yields one rect per node on the
 * same line. */
async function textGeometry(el: Locator) {
  return await el.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(node);
    const rects = Array.from(range.getClientRects());
    return {
      lines: new Set(rects.filter((r) => r.width > 0).map((r) => Math.round(r.top))).size,
      spillAbove: box.top - Math.min(...rects.map((r) => r.top)),
      spillBelow: Math.max(...rects.map((r) => r.bottom)) - box.bottom,
    };
  });
}

// 375 / 390 = the phone widths the rest of this suite treats as the owner's
// (nav-tabs-narrow, worker-detail-header-label, …) and where the defect was
// measured. 320 = the narrowest phone still in use.
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

// Edit mode adds 版本紀錄 / 取消 / 完成編輯, which never fit on the title's line at
// a phone width. If the header stops wrapping, the title group is squeezed to
// zero width: the title stacks one glyph per line, the count folds, and the
// badge and count paint over 版本紀錄 — overlap that no overflow check sees,
// because nothing leaves the card. en is here because its longer title, badge
// and count only stay apart when the title group wraps as well.
const EDIT_LABELS = {
  zh: { title: "判準(Insight)", modified: "已修改", history: "版本紀錄", cancel: "取消", done: "完成編輯" },
  en: { title: "Insight (judgement calls)", modified: "Modified", history: "Version history", cancel: "Cancel", done: "Done" },
};
for (const [language, label] of Object.entries(EDIT_LABELS)) {
  test(`${language}, 390px, editing an edited seeded insight: title, count, badge and buttons each keep one line and none overlap`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: 390, height: 900 });
    const cmp = await mount(<InsightBadgeNarrowStory />);

    const edit = cmp.locator(".doc-btn--edit");
    const editor = cmp.locator("textarea.doc-editor");
    const done = cmp.getByRole("button", { name: label.done, exact: true });
    await edit.click();
    await editor.fill(`${await editor.inputValue()}\n多一行`);
    await done.click();
    await edit.click();
    const badge = cmp.getByTestId("insight-status-badge");
    await expect(badge).toHaveText(label.modified);
    await expect(done).toBeVisible();

    const title = cmp.locator(".mp-lessons__title-label");
    const count = cmp.locator(".mp-insight__size");
    await expect(title).toHaveText(label.title);
    expect((await textGeometry(title)).lines, "title line boxes").toBe(1);
    expect((await textGeometry(count)).lines, "count line boxes").toBe(1);

    const parts: Record<string, Locator> = {
      title,
      count,
      badge,
      history: cmp.getByRole("button", { name: label.history, exact: true }),
      cancel: cmp.getByRole("button", { name: label.cancel, exact: true }),
      done,
    };
    const boxes = await Promise.all(
      Object.entries(parts).map(async ([name, el]) => ({ name, box: (await el.boundingBox())! }))
    );
    const overlaps: string[] = [];
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i].box;
        const b = boxes[j].box;
        const ox = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x);
        const oy = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y);
        if (ox > 0.5 && oy > 0.5) overlaps.push(`${boxes[i].name} × ${boxes[j].name}`);
      }
    }
    expect(overlaps, "header parts painting over each other").toEqual([]);

    const spill = await page.evaluate(() => {
      const head = document.querySelector(".mp-lessons__head")!;
      const card = document.querySelector(".mp-insight")!;
      return {
        head: head.scrollWidth - head.clientWidth,
        card: card.scrollWidth - card.clientWidth,
      };
    });
    expect(spill.head, "header row horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.card, "insight card horizontal overflow").toBeLessThanOrEqual(1);
  });
}
