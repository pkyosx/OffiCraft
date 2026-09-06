// T-122 — the 建議回覆 row must stay INSIDE the box it hangs under, in both of
// the shapes the owner's setting can take.
//
// WHY A REAL BROWSER: this is layout. The jsdom tests
// (SuggestedReplies.test.tsx and the two box guards) prove the chips are
// RENDERED and that a click fills the input; they say nothing about whether
// the chips FIT. Every assertion below is a measured rectangle.
//
// WHAT THIS FILE DOES NOT MEASURE, said out loud so nobody reads it as wider
// than it is: it mounts the row inside the reply card's width chain, not the
// live composer, so it certifies the ROW's geometry, not that either host
// mounted it under its input. That half is DOM order, and it is pinned in
// ReplyComposer.suggested-replies.test.tsx / TaskCardMessageBox.suggested-
// replies.test.tsx.
import { test, expect } from "@playwright/experimental-ct-react";
import { SuggestedRepliesStory } from "./stories/SuggestedRepliesStory";

// Phone and desktop: a row that fits a desktop card can still burst a phone
// one, and a long sentence that wraps on a phone can still widen a wide card.
const WIDTHS = [320, 1280];

for (const viewport of WIDTHS) {
  test(`${viewport}px: one very long suggestion wraps inside the card instead of widening it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<SuggestedRepliesStory />);

    const card = cmp.getByTestId("card-long");
    const cardBox = (await card.boundingBox())!;
    const right = cardBox.x + cardBox.width;

    const row = cmp.getByTestId("row-long");
    const rowBox = (await row.boundingBox())!;
    expect(rowBox.x + rowBox.width).toBeLessThanOrEqual(right + 1);

    // MEASURE THE CHIP, not only the row: the row is `width: 100%`, so its own
    // box is pinned to the card whatever the text inside it does — a guard
    // that stopped at the row would certify nothing about the wrapping.
    const chip = row.locator(".suggested-replies__chip").first();
    const chipBox = (await chip.boundingBox())!;
    expect(chipBox.x + chipBox.width).toBeLessThanOrEqual(right + 1);
    // …and where the sentence genuinely cannot fit — a phone card — it really
    // is a wrapped paragraph rather than a clipped single line. At 1280 the
    // same sentence fits on one line, so asserting a wrap there would be
    // asserting the viewport, not the rule.
    if (viewport === 320) expect(chipBox.height).toBeGreaterThan(30);
  });

  test(`${viewport}px: many suggestions wrap onto further lines and the row bounds its own height`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<SuggestedRepliesStory />);

    const card = cmp.getByTestId("card-many");
    const cardBox = (await card.boundingBox())!;
    const right = cardBox.x + cardBox.width;

    const row = cmp.getByTestId("row-many");
    const chips = await row.locator(".suggested-replies__chip").all();
    expect(chips.length).toBe(12);

    // Every chip inside the card, at both widths.
    const tops = new Set<number>();
    for (const chip of chips) {
      const box = (await chip.boundingBox())!;
      expect(box.x + box.width).toBeLessThanOrEqual(right + 1);
      tops.add(Math.round(box.y));
    }

    // The row is ALWAYS capped — that is a rule, not a consequence of the
    // viewport — so it can never push the composer down the page.
    const rowBox = (await row.boundingBox())!;
    expect(rowBox.height).toBeLessThanOrEqual(132);

    // The rest is only true where twelve chips genuinely do not fit one line.
    // At 1280 this card is wide enough to hold them all, so asserting a wrap
    // or a scrollbar there would be asserting the viewport, not the rule.
    if (viewport !== 320) return;

    // They occupy more than one line — a row that refused to wrap would put
    // them all on one and overflow the card.
    expect(tops.size).toBeGreaterThan(1);
    // Past the cap the row scrolls, and the overflow is REACHABLE: nothing is
    // hidden behind a clip.
    const scroll = await row.evaluate((el) => ({
      client: el.clientHeight,
      content: el.scrollHeight,
    }));
    expect(scroll.content).toBeGreaterThan(scroll.client);
    const moved = await row.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
      return el.scrollTop;
    });
    expect(moved).toBeGreaterThan(0);
  });

  test(`${viewport}px: the row sits BELOW the input it belongs to`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<SuggestedRepliesStory />);
    const card = cmp.getByTestId("card-many");
    const inputBox = (await card.locator(".chat__input").boundingBox())!;
    const rowBox = (await cmp.getByTestId("row-many").boundingBox())!;
    expect(rowBox.y).toBeGreaterThanOrEqual(inputBox.y + inputBox.height);
  });
}
