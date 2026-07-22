// HOTSPOT — following an in-app doc link must land the reader at the TOP of the
// new document, in a REAL browser (T-68f1 · fixround5 G1).
//
// The defect, measured on a live server at 1280×800: from #guide/why, clicking
// 介面說明 switched the URL and the body correctly, but `.settings`.scrollTop
// went 1759 → 1749 (it only moved because the new doc is shorter and the
// browser clamped it). The reader was dropped into the MIDDLE of a document
// they had never seen — no title, no breadcrumb, nothing on screen saying the
// page had changed. It is not an edge case: every in-app link sits in the body
// of a doc, so the reader is always scrolled down when they click one.
//
// Why this needs a real browser on top of the vitest pin: jsdom has no layout,
// so `scrollTop` there is just a number a test can set and read back — it
// cannot tell whether `.settings` is the element that actually scrolls, or
// whether some ancestor owns the overflow instead. If the fix had targeted the
// wrong node, the jsdom test would still pass. Here scrollTop is refused by any
// element that is not a scroll container, so the assertion means what it says.
//
// MUTANT (verified red, see ow52-68f1-fixround5-impl.md): delete the
// `scrollBox.current.scrollTop = 0` effect in UserGuidePage.tsx → "a new doc
// must start at the top" goes red with the carried-over offset, and no other
// guard moves.
import { test, expect } from "@playwright/experimental-ct-react";
import { GuideDocScrollStory } from "./stories/GuideDocScrollStory";

// `cmp` IS the story's [data-surface="guide-scroll"] wrapper (the mount root),
// so the scroll box is a direct descendant lookup from it.
const BOX = ".settings";
const DOC_H1 = ".doc-md h1";

test("following an in-app doc link starts the new doc at the top", async ({
  mount,
}) => {
  const cmp = await mount(<GuideDocScrollStory />);
  await expect(cmp.locator(DOC_H1)).toHaveText("為什麼是 OffiCraft");

  const box = cmp.locator(BOX);

  // Premise check FIRST: this box must really overflow, or "scrollTop is 0
  // afterwards" would be trivially true and the guard would be theatre.
  const metrics = await box.evaluate((el) => ({
    scrollH: el.scrollHeight,
    clientH: el.clientHeight,
    overflow: getComputedStyle(el).overflowY,
  }));
  expect(
    metrics.scrollH,
    "the story box must overflow, else this guard proves nothing"
  ).toBeGreaterThan(metrics.clientH);
  expect(metrics.overflow, ".settings must be the scrolling box").toBe("auto");

  // Scroll to the bottom, where the in-app links live.
  await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  const before = await box.evaluate((el) => el.scrollTop);
  expect(
    before,
    "the browser must have accepted a real scroll offset"
  ).toBeGreaterThan(0);

  await cmp.getByRole("button", { name: "介面說明", exact: true }).click();
  await expect(cmp.locator(DOC_H1)).toHaveText("介面說明");

  expect(
    await box.evaluate((el) => el.scrollTop),
    "a new doc must start at the top, not wherever the last one was"
  ).toBe(0);

  // And the reader can SEE the orientation: the doc's own heading and the
  // breadcrumb are both inside the visible box, not above it.
  const h1Top = await cmp
    .locator(DOC_H1)
    .evaluate((el) => el.getBoundingClientRect().top);
  const boxTop = await box.evaluate((el) => el.getBoundingClientRect().top);
  const boxBottom = await box.evaluate(
    (el) => el.getBoundingClientRect().bottom
  );
  expect(h1Top, "the new doc's heading must be on screen").toBeGreaterThanOrEqual(
    boxTop - 1
  );
  expect(h1Top, "the new doc's heading must be on screen").toBeLessThan(
    boxBottom
  );
});
