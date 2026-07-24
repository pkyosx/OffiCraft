// HOTSPOT — the 使用說明 LIST page must sit FLUSH-LEFT with every other settings
// page (T-9aa5).
//
// Bug (owner, desktop, v0.5.22): the 使用說明 list content sat ~48px to the RIGHT
// of 軟體更新 / 角色 / 參數. Root cause: the v0.5.19/T-23df reading measure block
// (`.guide-view { padding: 20px }` + `.guide-view .set-entries/.settings__title
// { max-width: 900px; margin-inline: auto }`) was scoped to `.guide-view`, which
// the LIST inherits as well as the doc view — so the list menu was centred in a
// 900px band: 20px surface gutter + 28px centring margin = 48px right-shift. The
// fix scopes that measure to `.guide-view--doc` only; the bare-`.guide-view`
// list falls back to the plain `.settings` flex column and stretches flush-left.
//
// Why a real browser (not the jsdom vitest suite): the shift is pure layout —
// `.settings` is `display:flex` (column), `margin-inline:auto` on a flex ITEM
// collapses it to content width, and `max-width` only bites once the column
// exceeds it. jsdom resolves no flex and no @media, so "is the left edge 48px
// off" is structurally undecidable there. And per the T-23df false-green
// (guide-doc-overflow.ct.spec.tsx): this must NOT be measured at the document /
// page scrollWidth layer — the 48px shift is a CENTRED narrow band, the page
// never scrolls. We measure the ACTUAL rendered content left edge:
// `.set-entries` getBoundingClientRect().left.
//
// Contract: mount the guide list AND a bare-`.settings` reference in the SAME
// `.app__main` column; their `.set-entries` left edges must be equal (±1px), and
// both must sit at the `.app__main` inner edge (app__main.left + 22px padding).
// Measured at desktop 1280 and phone 390.
//
// MUTANT (verified red — see /tmp/9aa5-review.md): restore the bug measure onto
// `.guide-view` (`.guide-view { padding: 20px }` + `.guide-view .set-entries,
// .guide-view .settings__title { max-width: 900px; margin-inline: auto }`) →
// the guide list `.set-entries.left` jumps ~48px right of the reference and off
// the app__main inner edge; the "flush-left" assertion below goes red, and it is
// that left-edge assertion (not the page-scroll one) that fires.
import { test, expect } from "@playwright/experimental-ct-react";
import { GuideListFlushLeftStory } from "./stories/GuideListFlushLeftStory";
import { GuideDocOverflowStory } from "./stories/GuideDocOverflowStory";

/** The rendered left edges that decide flush-left alignment. */
async function measure(page: any) {
  return page.evaluate(() => {
    const px = (sel: string) => {
      const el = document.querySelector(sel) as HTMLElement | null;
      return el ? Math.round(el.getBoundingClientRect().left) : -1;
    };
    const main = document.querySelector(".app__main") as HTMLElement;
    const mainCS = getComputedStyle(main);
    const mainRect = main.getBoundingClientRect();
    return {
      guideLeft: px('[data-testid="guide-set-entries"]'),
      refLeft: px('[data-testid="ref-set-entries"]'),
      guideTitleLeft: px(".guide-view .settings__title"),
      refTitleLeft: px('[data-testid="settings-ref"] .settings__title'),
      // app__main inner edge = its box left + its left padding.
      mainInnerLeft: Math.round(mainRect.left + parseFloat(mainCS.paddingLeft)),
      pageOver:
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth,
    };
  });
}

for (const width of [1280, 390]) {
  test(`@${width}: 使用說明 list content is flush-left with the other settings pages`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<GuideListFlushLeftStory />);

    const m = await measure(page);
    // Printed verbatim so the review can quote a measurement, not an assertion.
    console.log(`[guide-flush-left] @${width} ` + JSON.stringify(m));

    expect(m.guideLeft, "guide set-entries must have rendered").toBeGreaterThan(0);
    expect(m.refLeft, "reference set-entries must have rendered").toBeGreaterThan(0);

    // THE contract: the guide list content left edge == the other settings
    // pages' content left edge. The 48px bug makes this delta ~48.
    expect(
      Math.abs(m.guideLeft - m.refLeft),
      `[${width}px] guide list set-entries.left (${m.guideLeft}) must match the bare-.settings reference (${m.refLeft}) — the 48px right-shift bug widens this delta`
    ).toBeLessThanOrEqual(1);

    // And both must hug the .app__main inner edge — pins the ABSOLUTE position
    // (a mutant that shifted BOTH equally would still be caught here).
    expect(
      Math.abs(m.guideLeft - m.mainInnerLeft),
      `[${width}px] guide list set-entries.left (${m.guideLeft}) must sit at the app__main inner edge (${m.mainInnerLeft})`
    ).toBeLessThanOrEqual(1);

    // Titles align too (the fix also drops .settings__title from the measure).
    expect(
      Math.abs(m.guideTitleLeft - m.refTitleLeft),
      `[${width}px] guide title.left (${m.guideTitleLeft}) must match the reference title.left (${m.refTitleLeft})`
    ).toBeLessThanOrEqual(1);
  });
}

// ── doc view NO-REGRESSION: the reading measure stays on `.guide-view--doc` ──
// The T-9aa5 fix moved the 900px cap off the list — but it must REMAIN on the
// doc view (owner never complained about the doc; the long-form measure is
// deliberate). guide-doc-overflow.ct.spec.tsx already pins the phone-width
// no-sideways-scroll; this pins the DESKTOP retention the fix could silently
// have dropped: at 1280 the doc-card must still be capped to its 900px measure,
// NOT stretched to the ~1040px column.
const DOC_MD = [
  "# 介面說明",
  "",
  "這是一段長篇說明文字，用來驗證文件內頁在桌面寬度下仍保留 900px 的閱讀量測寬度。",
  "".padEnd(4, ""),
  "- 條列一",
  "- 條列二",
].join("\n");

test("@1280: guide DOC view keeps its 900px reading measure (no T-9aa5 regression)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  // GuideDocOverflowStory already supplies its own `.app › .app__main` shell.
  await mount(
    <GuideDocOverflowStory title="介面說明" markdown={DOC_MD} assets={{}} />
  );

  const docWidth = await page.evaluate(() => {
    const card = document.querySelector(".guide-view--doc .doc-card") as HTMLElement;
    return Math.round(card.getBoundingClientRect().width);
  });
  console.log(`[guide-flush-left] @1280 doc-card width=${docWidth}px (cap 900)`);
  // box-sizing is border-box globally, so max-width:900px IS the box width; the
  // ~1040px column would give ~996 without the cap. ≤ 901 with 1px slack.
  expect(
    docWidth,
    `doc-card must stay capped at its 900px measure at 1280 (got ${docWidth}px)`
  ).toBeLessThanOrEqual(901);
});
