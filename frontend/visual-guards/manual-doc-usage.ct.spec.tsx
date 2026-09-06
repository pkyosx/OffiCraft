// T-100 — the 「已用 / 上限」 readouts on the two task-manual documents, in a
// REAL browser, at phone and desktop widths.
//
// 🔴 WHAT ONLY A BROWSER CAN ANSWER HERE.
//   · The SOP readout is a new item in `.manual-sec__head`, a flex ROW that
//     already carries a number, a CJK title, a CJK hint and the block's 編輯
//     switch. Whether it FITS is a layout question; jsdom applies no CSS at all
//     and answers "the span is in the DOM" either way.
//   · The failure this class of change produces is not a missing element — it
//     is a row that wraps to a second line, or a page that scrolls sideways on
//     a phone. Both are invisible to every assertion in the vitest suite.
//   · `.doc-card__usage` is styled by `settings.css`. If that sheet ever stops
//     reaching this page, the readout is still in the DOM and every vitest
//     assertion stays green — only computed style says so, and only here.
//
// ⚠️ WHAT THE COMPUTED-STYLE CHECK BELOW DOES NOT PROVE, measured rather than
// assumed: deleting `DocUsage`'s own `import "./settings.css"` leaves all four
// of these tests GREEN, because the sheet still arrives through another module
// in this page's import graph. That is precisely the free-riding
// styleOwnership.test.ts exists for — and `settings.css` is not in its
// OWNED_SHEETS list, so nothing mechanically holds that import in place. The
// import is kept because the convention is right, not because anything checks
// it. Do not read the tabular-nums assertion as an ownership guard.

import { test, expect } from "@playwright/experimental-ct-react";
import { ManualDocUsageStory } from "./stories/ManualDocUsageStory";

const WIDTHS = [
  { name: "narrow-390", px: 390 },
  { name: "wide-1280", px: 1280 },
];

const DOCS = [
  { doc: "definition" as const, testId: "manual-sop-usage", text: "15796 / 18000" },
  {
    doc: "learnings" as const,
    testId: "manual-learnings-usage",
    text: "16999 / 17000",
  },
];

for (const w of WIDTHS) {
  for (const d of DOCS) {
    test(`${d.doc} 的已用/上限看得到、不換行、不把頁面撐橫 — ${w.name}`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width: Math.max(w.px, 400), height: 900 });
      await mount(<ManualDocUsageStory doc={d.doc} widthPx={w.px} />);

      const usage = page.getByTestId(d.testId);
      // ON SCREEN, not merely in the DOM — a readout with no box is the same as
      // no readout, and it is the state a lost stylesheet import produces.
      await expect(usage).toBeVisible();
      await expect(usage).toBeInViewport();
      await expect(usage).toHaveText(d.text);

      const box = await usage.boundingBox();
      expect(box, "讀數要有實際的框").not.toBeNull();
      expect(box!.width).toBeGreaterThan(20);
      // ONE line. Its own font-size is the ceiling on a single line box; two
      // lines would be roughly double, which is what a shrunk flex item does to
      // a five-digit pair beside CJK text.
      const fontSize = await usage.evaluate(
        (el) => parseFloat(getComputedStyle(el).fontSize)
      );
      expect(box!.height).toBeLessThan(fontSize * 1.8);

      // The style really is applied — tabular figures are what stop the number
      // jittering as the owner types. This says the sheet REACHED the page; see
      // the header for what it deliberately does not say about who imported it.
      const variant = await usage.evaluate(
        (el) => getComputedStyle(el).fontVariantNumeric
      );
      expect(variant).toContain("tabular-nums");

      // 🔴 THE PAGE DOES NOT SCROLL SIDEWAYS. Measured on the real scroll
      // container as well as the document: an ancestor with `overflow-y: auto`
      // can absorb the overflow into its own scroll box, so asking
      // documentElement alone answers "clean" on a broken phone layout.
      const hScroll = await page.evaluate(() => {
        const doc = document.documentElement;
        const main = document.querySelector(".app__main") as HTMLElement | null;
        return {
          docOverflow: doc.scrollWidth - doc.clientWidth,
          mainOverflow: main ? main.scrollWidth - main.clientWidth : 0,
        };
      });
      expect(hScroll.docOverflow).toBeLessThanOrEqual(0);
      expect(hScroll.mainOverflow).toBeLessThanOrEqual(0);
    });
  }
}
