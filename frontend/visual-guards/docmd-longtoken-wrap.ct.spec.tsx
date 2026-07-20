// HOTSPOT — `.doc-md` document surfaces, long-token overflow (T-d451).
//
// Bug (owner, phone 2026-07-20): 角色誌 / 學習經驗 carried unbreakable long tokens
// (long URL, 40-hex sha, long English word). The `.doc-md` BASE rule
// (settings.css) declares no `overflow-wrap`, so such a token set the
// container's min-content to its full width, pushed it past the phone viewport
// and the whole PAGE gained a horizontal scrollbar — owner could drag the page
// sideways.
//
// T-4974 had already fixed this for the TASK-CARD surfaces, but with
// per-surface rules in tasks.css (`.task-card__desc`, `.task-step__dod .doc-md`,
// `.task-step__waiting-md`) — every other `.doc-md` host was left bare. T-d451
// moves the wrap to the `.doc-md` base so all present and future hosts inherit
// it by construction.
//
// jsdom is blind to this (no layout engine, offsetWidth 0), so it is a CT guard
// measured in real Chromium against the real sheets. Width is an INPUT
// dimension: 375 (owner's phone class) and 1280 (desktop must not regress).
//
// TWO-SIDED contract, deliberately:
//   (1) the PAGE must not scroll sideways, and no doc surface may overflow;
//   (2) `pre` and `table` MUST still scroll inside their own box. Over-fixing
//       (e.g. `word-break: break-all` on the base, or killing `overflow-x`)
//       would satisfy (1) while destroying the legitimate scroll sub-regions —
//       (2) is what stops that.
//
// MUTANTS (each verified red) — every rule this guard defends is load-bearing:
//   drop `overflow-wrap: anywhere` from the `.doc-md` base (settings.css)
//     → 375px page red, +555px
//   drop `overflow-wrap` from `.reply-option__text` (replies.css)
//     → 375px page red, +264px
//   drop `overflow-wrap` from `.reply-card__answer-text` (replies.css)
//     → 375px page red, +537px
//   drop `overflow-x: auto` from `.doc-md pre` (settings.css) → (2) goes red
//
// ⚠️ WHEN RE-VERIFYING THIS GUARD, watch for assertion MASKING: the page-level
// check below runs first and aborts the test, so a mutant "proves" only that
// ONE assertion works while every per-surface assertion underneath it never
// executes. To prove those, temporarily relax the page assertion (raise its
// bound) and re-run the mutant so the per-surface failure surfaces on its own.
// Same trap in reverse for a surface that stops rendering — hence the explicit
// `> -1` presence check below.
import { test, expect } from "@playwright/experimental-ct-react";
import { DocMdLongTokenStory } from "./stories/DocMdLongTokenStory";

/** Surfaces that must FIT — one entry per real render site's wrapper chain. */
const FIT_SURFACES = [
  '[data-surface="doc-detail"] .doc-card__body .doc-md',
  '[data-surface="lessons"] .doc-md',
  '[data-surface="reply-card"] .reply-card__summary',
  '[data-surface="reply-card"] .reply-card__body',
  // Non-markdown reply-card fields — they do NOT inherit the .doc-md base fix,
  // so they need their own coverage or a regression there would go unseen.
  '[data-surface="reply-card"] .reply-option__text',
  '[data-surface="reply-card"] .reply-card__answer-text',
];

async function mountAt(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<DocMdLongTokenStory />);
  await expect(cmp.locator('[data-surface="lessons"] .doc-md')).toBeVisible();
  return cmp;
}

/** scrollWidth - clientWidth for one selector; -1 when the node is ABSENT. */
async function overflowOf(page: any, sel: string): Promise<number> {
  return page.evaluate((s: string) => {
    const el = document.querySelector(s) as HTMLElement | null;
    return el ? el.scrollWidth - el.clientWidth : -1;
  }, sel);
}

async function assertContract(page: any, width: number) {
  // (1) CORE red→green — the owner's exact symptom ("手機頁面會左右滑動").
  const pageOver = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(
    pageOver,
    `[${width}px] page must have no horizontal scroll (got +${pageOver}px)`
  ).toBeLessThanOrEqual(1);

  // Each doc surface fits its own box. Pins the fix per-surface so a mutant
  // names the surface it broke.
  for (const sel of FIT_SURFACES) {
    const over = await overflowOf(page, sel);
    // A MISSING node returns -1, which would sail through a `<= 1` assertion —
    // exactly the silent-retirement trap T-c514 found in the T-4974 guard. So
    // presence is asserted separately, with a message that names the cause.
    expect(over, `[${width}px] ${sel} missing (never rendered)`).toBeGreaterThan(
      -1
    );
    expect(over, `[${width}px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }
}

/** The two legitimate horizontal-scroll sub-regions must survive the fix. */
async function assertScrollableSubRegionsSurvive(page: any, width: number) {
  for (const sel of [
    '[data-surface="doc-detail"] .doc-md pre',
    '[data-surface="doc-detail"] .doc-md table',
  ]) {
    const el = page.locator(sel).first();
    await expect(el, `[${width}px] ${sel} missing`).toBeAttached();
    const overflowX = await el.evaluate(
      (n: HTMLElement) => getComputedStyle(n).overflowX
    );
    expect(
      overflowX,
      `[${width}px] ${sel} must own its scroll, not hand it to the page`
    ).toBe("auto");
  }

  // At phone width the wide code line genuinely exceeds its box — prove the
  // scroll is REAL (content wider than the box) and not merely declared, while
  // the page above stayed unscrollable.
  const preOver = await overflowOf(
    page,
    '[data-surface="doc-detail"] .doc-md pre'
  );
  if (width <= 400) {
    expect(
      preOver,
      `[${width}px] wide code must still scroll INSIDE the pre (got +${preOver}px)`
    ).toBeGreaterThan(1);
  }
}

test("375px: long tokens in doc surfaces never scroll the page", async ({
  mount,
  page,
}) => {
  await mountAt(mount, page, 375);
  await assertContract(page, 375);
  await assertScrollableSubRegionsSurvive(page, 375);
});

test("1280px: the wrap fix does not regress the desktop layout", async ({
  mount,
  page,
}) => {
  await mountAt(mount, page, 1280);
  await assertContract(page, 1280);
  await assertScrollableSubRegionsSurvive(page, 1280);
});
