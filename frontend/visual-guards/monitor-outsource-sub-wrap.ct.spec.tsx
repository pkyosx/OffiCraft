// HOTSPOT — Monitor §3 AI 會話, an outsource row's task-title sub-line stretches
// the table (T-cf32).
//
// Owner (screenshot, DESKTOP): "版面太寬了 可能要讓他的描述換行?" — an outsource
// row's `.mon-member__sub` renders the bound task's TITLE (`worker.taskTitle`,
// MonitorPage.tsx's OutsourceSessionRow), which can be a full sentence.
// `.mon-table td { white-space: nowrap }` (monitor.css) applies to every cell
// table-wide, so with no override the browser's automatic table column-sizing
// widens the member column to fit the whole unbroken title, and the table —
// inside `.mon-table-wrap`'s `overflow-x: auto` — grows a horizontal scrollbar
// (the scrollbar the owner saw at the bottom of the table).
//
// Fix under test (monitor.css): `.mon-member__sub` — ALONE among `.mon-table
// td` — gets `white-space: normal` + `overflow-wrap: anywhere`. Every OTHER
// column (machine/account/model) stays nowrap on purpose — the SENTINEL asserts
// that. (No `min-width: 0` companion — measured inert, see the mutant note.)
//
// WHY THE WRAP, NOT THE PAGE: unlike the sibling long-token guards
// (monitor-table-longtoken, taskcard-longtoken-wrap) whose symptom is the PAGE
// scrolling sideways, here `.mon-table-wrap` owns `overflow-x: auto` BY DESIGN
// (it absorbs desktop overflow so the page never scrolls). So the owner-visible
// symptom — and this guard's mutant-catcher — is the WRAP's own overflow, not
// the page's.
//
// WHY BOTH WIDTHS ARE DESKTOP (>720px), no phone case: monitor.css's
// `@media (max-width: 720px)` card mode already sets `.mon-table td {
// white-space: normal }`, so at phone widths the title wraps regardless of this
// fix — a phone-width assertion would be VACUOUS for this fix's mutants (it
// would stay green with the fix reverted). This fix lives entirely on the
// desktop (nowrap) path, so both asserted widths are desktop. The phone card
// mode's own wrapping is already guarded by monitor-table-longtoken.ct.spec.
//
// jsdom cannot see any of this (no layout engine, no @media, offsetWidth 0) —
// real Chromium CT against the real monitor.css.
//
// MUTANTS (each verified red at both widths, one at a time; the two dropped
// min-width:0 rules were verified INERT the same way):
//   remove `.mon-member__sub { white-space: normal }`   → nothing wraps → wrap
//     overflows (pinned by the CJK/spaced part of the title).
//   remove `.mon-member__sub { overflow-wrap: anywhere }` → the unbreakable
//     ascii token (LONG_ASCII_TOKEN — a solid run, no hyphen/space break
//     opportunity) cannot break → wrap overflows.
//   a `min-width: 0` on `.mon-member__sub`/`.mon-member__body` left this guard
//     GREEN when removed → inert (overflow-wrap:anywhere already collapses the
//     cell's min-content), so both were dropped (repo discipline — same finding
//     monitor.css's card-mode note records for the identical reason).
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorOutsourceSubWrapStory } from "./stories/MonitorOutsourceSubWrapStory";

// Substrings of the story's LONG_TASK_TITLE, kept in sync BY HAND — not
// imported: importing a second named export from the same module a CT spec
// mount()s makes Playwright's CT bundler double-declare the module's component
// binding ("Identifier '...' has already been declared", a build-time error).
// A `toContainText` substring check tolerates drift as long as the prefix/token
// still match.
const TITLE_SNIPPET = "重構帳務對帳流程";
const LONG_ASCII_TOKEN =
  "reconcileCutover2026Q3Phase2BillingImporterEventDrivenRetryAlertWebhookNotifyFullRegressionCoverageAndBackfillMigrationDoNotBreakThisSingleToken";

async function mountAt(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<MonitorOutsourceSubWrapStory />);
  await expect(cmp.locator('[data-testid="mon-outsource-sub"]')).toBeVisible();
  return cmp;
}

async function wrapOverflow(page: any): Promise<number> {
  return page.evaluate(() => {
    const el = document.querySelector('[data-surface="sessions"]');
    if (!el) return -2; // wrap never rendered — scored as a failure below
    return el.scrollWidth - el.clientWidth;
  });
}

async function computedWhiteSpace(
  page: any,
  sel: string
): Promise<string | null> {
  return page.evaluate((s: string) => {
    const el = document.querySelector(s);
    return el ? getComputedStyle(el).whiteSpace : null;
  }, sel);
}

async function assertWraps(page: any, width: number) {
  // NON-VACUITY: the long title AND its unbreakable token actually rendered —
  // a fixture drift that shortened either would retire a mutant in silence.
  await expect(
    page.locator('[data-testid="mon-outsource-sub"]')
  ).toContainText(TITLE_SNIPPET);
  await expect(
    page.locator('[data-testid="mon-outsource-sub"]')
  ).toContainText(LONG_ASCII_TOKEN);

  // CORE red→green (owner's exact symptom): the sessions table wrap must not
  // grow a horizontal scrollbar on account of the long title — the title wraps
  // instead of stretching the member column.
  const over = await wrapOverflow(page);
  expect(over, `[${width}px] sessions wrap missing (never rendered)`).not.toBe(
    -2
  );
  expect(
    over,
    `[${width}px] sessions wrap forced to scroll by ${over}px — the task title did not wrap and stretched the table`
  ).toBeLessThanOrEqual(1);

  // SENTINEL — scope: only `.mon-member__sub` opted out of nowrap. Every
  // ordinary `.mon-table td` (here: the account column of BOTH rows) must STILL
  // be nowrap, proving the table-wide `.mon-table td { white-space: nowrap }`
  // was not blanket-loosened. (Note: the member row's OWN `.mon-member__sub`
  // does become white-space:normal too — the rule is not row-scoped — but that
  // is harmless for its short label and is NOT what this sentinel guards.)
  for (const sel of [
    '[data-testid="mon-member-account"]',
    '[data-testid="mon-outsource-account"]',
  ]) {
    const ws = await computedWhiteSpace(page, sel);
    expect(
      ws,
      `[${width}px] SENTINEL: ${sel} must stay nowrap — only .mon-member__sub opts out`
    ).toBe("nowrap");
  }
}

test(
  "900px desktop: a long outsource task title wraps instead of stretching the table",
  async ({ mount, page }) => {
    await mountAt(mount, page, 900);
    await assertWraps(page, 900);
  }
);

test(
  "1280px (owner's screenshot width): the desktop table no longer widens on a long task title",
  async ({ mount, page }) => {
    await mountAt(mount, page, 1280);
    await assertWraps(page, 1280);
  }
);
