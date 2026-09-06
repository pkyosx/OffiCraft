// T-122 — the 建議回覆 entry fields on 設定 › 參數調整 must be WIDE ENOUGH TO
// READ, not the narrow box the numeric knobs use.
//
// 🔴 THE BUG THIS EXISTS FOR, MEASURED ON A REAL BROWSER: the input wears
// `.param-input` (for its chrome) and `.sugg-edit__input` (for its width), and
// both selectors are specificity (0,1,0). `.param-input { width: 88px }` is
// declared later in settings.css, so it won. 88px shows about SEVEN Chinese
// characters. The owner types sentences of up to 120 here — and on the
// over-cap path he was shown "this sentence is 141 characters, over the 120
// limit" beside a box displaying its first seven. The DOM value was right the
// whole time; the field was simply unusable.
//
// 🔴 WHY THIS CANNOT LIVE IN JSDOM. jsdom has no layout engine: every
// `getBoundingClientRect()` is zero and every computed width is the literal
// declaration, so the losing side of a cascade collision is invisible there.
// The four jsdom guards on these rows (SettingsPage.suggested-replies-t122)
// stayed green through the entire bug. A width assertion added there would be a
// permanent false green.
//
// WHAT IT ASSERTS, and what it deliberately does not: not a pixel value — that
// would redden whenever anyone touches the settings layout for unrelated
// reasons. It asserts (a) the field is not stuck at the numeric width, and (b)
// it is wide enough for a real sentence, by comparing it against a numeric
// field measured ON THE SAME PAGE rather than against a number typed in here.
import { test, expect } from "@playwright/experimental-ct-react";
import { SettingsSuggestedRepliesStory } from "./stories/SettingsSuggestedRepliesStory";

/** The narrow box the numeric knobs want, and the width this field must NOT
 * inherit. Read off the page, never hard-coded: if someone legitimately changes
 * `.param-input`, this guard follows them instead of going stale. */
async function openParams(
  cmp: { getByTestId: (id: string) => { click: () => Promise<void> } },
  page: { waitForTimeout: (ms: number) => Promise<void> }
) {
  await cmp.getByTestId("seed").click();
  await cmp.getByTestId("settings-params-entry").click();
  await page.waitForTimeout(300);
}

for (const viewport of [390, 1280]) {
  test(`${viewport}px: a 建議回覆 entry field is far wider than a numeric knob`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 1400 });
    const cmp = await mount(<SettingsSuggestedRepliesStory />);
    await openParams(cmp, page);

    // A numeric knob on the SAME page — the width the buggy build gave the
    // sentence field.
    const numeric = page.locator("#param-backup-retain");
    await numeric.scrollIntoViewIfNeeded();
    const numericBox = (await numeric.boundingBox())!;

    const entry = page.locator("#param-suggested-reply-card-0");
    await entry.scrollIntoViewIfNeeded();
    const entryBox = (await entry.boundingBox())!;

    // (a) It is NOT the numeric width. The regression makes these equal.
    expect(entryBox.width).toBeGreaterThan(numericBox.width * 2);

    // (b) It is wide enough to actually read a sentence in — measured against
    // the row it sits in rather than a literal pixel count, so a legitimate
    // layout change moves both together. The row also holds three 30px icon
    // buttons and their gaps, so "most of the row" is the honest bar.
    const row = page.locator(".sugg-edit__row").first();
    const rowBox = (await row.boundingBox())!;
    expect(entryBox.width).toBeGreaterThan(rowBox.width * 0.6);

    // (c) AND THE NUMERIC KNOBS ARE UNTOUCHED. The tempting fix was to widen
    // `.param-input` itself, which would have quietly re-sized every TTL,
    // threshold and retention field on this page — a blast radius far outside
    // this ticket. This asserts the fix stayed local: the numeric box is still
    // the narrow one. Not pinned to the exact 88 so the constant can move;
    // pinned to "still narrow, still not a sentence field".
    expect(numericBox.width).toBeLessThan(120);
  });
}
