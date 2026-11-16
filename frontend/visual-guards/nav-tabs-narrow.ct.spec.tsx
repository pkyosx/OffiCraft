// The nav BAND's box at phone and desktop widths (T-081b round 10).
//
// WHAT THIS FILE GUARDS TODAY: one thing — the band that wraps the tab strip
// keeps symmetric vertical padding and its original height. Nothing here
// asserts how much of any TAB or LABEL is on screen any more; see below.
//
// Why a real browser: the band's box is pure layout. jsdom resolves no flex,
// evaluates no @media and reports every width as 0, so a padding change is
// structurally undecidable there — it stays green across the entire vitest suite.
//
// ── WHY THE `nav strip geometry` GROUP WAS REMOVED (T-0fef, owner ruling) ──
// This file used to carry a second group asserting that the LAST tab (使用說明)
// was wide enough on screen at 320/390/414. It is gone, on purpose, and it
// should not be reinstated in that shape:
//
//   1. It pinned three things that are all free to change: the machine's font
//      metrics, `tabCount` being exactly 5, and the last tab being named
//      使用說明. Adding a sixth top-level nav item would have turned three
//      assertions red, and two of them would only have been saying that the
//      test itself had expired.
//   2. It called "the last tab is clipped" a defect, while the PRODUCT'S OWN
//      documentation calls it normal: docs/guide/mobile.md:9 says the tab
//      strip can be swiped sideways on phones where it does not fit. The strip really does scroll (`.nav-tabs__seg`,
//      `overflow-x: auto`), so the tab is reachable and tappable.
//   3. Its green on this repo's dev Mac was a PLATFORM ARTEFACT. A Han glyph
//      advances exactly 1em in nearly every CJK font; macOS `system-ui` is the
//      outlier at 0.9587em (weight 500), i.e. ~4% narrower. The ~3px of
//      headroom the assertions had was that 4%, so the guard could not hold
//      anywhere else — measured twice, in two independent font environments,
//      and BOTH times these were the only two failures out of 207 CT tests:
//        * hosted macOS runner (T-ab2a node 2): visibleLabel 35 < 36 @390,
//          visibleTab 90 ≠ 95 @414 — 205 passed. Re-checkable: that run's raw
//          log is GitHub Actions run 31003590775 (workflow tab2a-measure.yml,
//          branch measure/tab2a-macos-probe), block 2.
//        * dev Mac with Noto Sans TC pinned into the CT harness (T-0fef
//          experiment): visibleLabel 34 < 36, visibleTab 89 ≠ 95 — 205 passed.
//          ⚠️ That run's log was NOT committed here; it lives on the T-0fef
//          ticket as an artifact. If you need to re-derive it rather than trust
//          this comment, pin a 1em CJK face into playwright/index.html and run
//          `npm run test:ct` — the point is reproducible, the log is not in-tree.
//      That second measurement is also why `npm run test:ct` is now a cloud
//      gate: with this group gone, both environments ran 204 green.
//
// 🔴 READ THIS BEFORE REUSING REASON 3. Reason 3 is true but it is NOT the
// reason this group had to go — and an earlier draft of this header let it read
// that way. Two corrections, both from an independent review of the removal:
//
//   a. `frontend/playwright/index.html` (the CT harness page) loads NO webfonts,
//      while `frontend/index.html` (what ships) pulls Schibsted Grotesk + Noto
//      Sans TC from Google Fonts. theme.css declares them either way, so the
//      harness falls through to `system-ui`. ⇒ EVERY CT layout guard has always
//      measured the runner's system font, never the font a user sees. This group
//      was not the only one with a font dependence — it was the one with the
//      least headroom.
//   b. Therefore the "dev Mac with Noto Sans TC pinned in" run was not a switch
//      to some exotic face: it was the harness finally matching PRODUCTION. And
//      under production's own font these assertions FAIL (visibleLabel 34 < 36).
//      Read plainly: the user really does see fewer legible pixels than this
//      guard demanded. It was a TRUE positive, not a false one.
//
// ⇒ The load-bearing reason for removal is REASON 2 — the product defines the
// clipping as normal and the strip scrolls, so the guard was asserting against a
// documented product decision. When a guard and the product disagree about what
// "broken" means, the guard is what gives way. Do not cite reason 3 alone; it
// invites the conclusion that the CT font environment is now sound, and it
// is not. Aligning the harness with production is unfinished work.
import { test, expect } from "@playwright/experimental-ct-react";
import { NavTabsNarrowStory } from "./stories/NavTabsNarrowStory";

// The band's vertical padding must stay SYMMETRIC, and the band must stay the
// height it has always been (T-081b round 10, owner ruling). It shipped as
// `2px 22px 12px`, which sat the rounded .nav-tabs__seg frame 10px high inside
// its band — invisible under the built-in dark theme, where the band is the same
// colour as the page, and glaring under a light pack that gives --color-nav-bg a
// colour of its own. 7px + 7px is the same 14px total, so nothing below moves.
//
// Measured in the real browser rather than asserted from the stylesheet text:
// the band's box is what the two @media blocks, the flex layout and the
// scrollbar-free strip actually produce.
for (const width of [390, 1280]) {
  test(`nav band padding is symmetric and its height is unchanged @${width}`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const cmp = await mount(<NavTabsNarrowStory />);
    await expect(cmp.locator(".nav-tabs__seg")).toBeVisible();

    const m = await cmp.locator(".nav-tabs").evaluate((band: HTMLElement) => {
      const seg = band.querySelector(".nav-tabs__seg") as HTMLElement;
      const b = band.getBoundingClientRect();
      const s = seg.getBoundingClientRect();
      const cs = getComputedStyle(band);
      return {
        padTop: parseFloat(cs.paddingTop),
        padBottom: parseFloat(cs.paddingBottom),
        above: Math.round((s.top - b.top) * 100) / 100,
        below: Math.round((b.bottom - s.bottom) * 100) / 100,
        bandH: Math.round(b.height * 100) / 100,
        segH: Math.round(s.height * 100) / 100,
      };
    });
    console.log(`@${width} nav band ` + JSON.stringify(m));

    expect(m.padTop, "the band's top and bottom padding must be equal").toBe(
      m.padBottom
    );
    // The frame's own gap above and below — the thing the eye reads — not just
    // the declared padding.
    expect(m.above, "the rounded frame must sit centred in its band").toBeCloseTo(
      m.below,
      1
    );
    // 2px + 12px = 7px + 7px: the band keeps its height, so nothing below it
    // moves. A future 8/8 would be symmetric AND wrong.
    expect(
      m.padTop + m.padBottom,
      "the band's total vertical padding must stay 14px"
    ).toBe(14);
    expect(m.bandH, "band height is exactly its frame plus that padding").toBe(
      m.segH + 14
    );
  });
}

// 🔴 NOTHING TYPECHECKS THIS DIRECTORY. `frontend/tsconfig.json` is
// `"include": ["src"]`, and CI's typecheck step is `npm run typecheck` →
// `tsc --noEmit` against that same config (bin/ci.sh:349), so no .ct.spec.tsx
// or story under visual-guards/ is ever type-checked by anything. The evidence
// is this file's own history: it once shipped a real TS2459 on a `type { Locator }`
// import (that import went with the removed group, so it is no longer here to
// look at) and CI stayed green — a type-only import is erased before runtime, so
// Playwright ran every guard regardless. The check was never bypassed; it simply
// does not reach here.
//
// What the next person has to do, and what to expect: adding "visual-guards"
// to that `include` is the fix, but it is NOT a free one-line change — with the
// directory included, tsc reports 11 further errors across 4 other files
// (software-update-status.ct.spec.tsx, and the OfficeSidebar / TaskArtifacts-
// Overflow / TaskCardArtifacts stories), all pre-existing and none of them
// this pack's. Measured, not estimated: a tsconfig with `["src",
// "visual-guards"]` was run against this tree. So the work is "fix 11 errors,
// then widen the include", which is why it was left out of a fourth-round
// doc-truth pack rather than tacked on. No ticket is tracking this; this
// comment is the only record.
