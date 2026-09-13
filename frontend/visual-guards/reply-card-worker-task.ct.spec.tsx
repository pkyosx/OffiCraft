// HOTSPOT — T-196: the asking outsource worker's current-task line, the THIRD
// surface of `CurrentTaskTitle` (after the office rail row and the worker chat
// header) and the first one that sits inside a flex ROW next to other controls.
//
// jsdom resolves no -webkit-line-clamp box and no layout, so every property
// below is invisible to the unit tests that pin the line's CONTENT:
//   · the clamp is real HERE too — the rail's rule reaches this surface only
//     because RepliesPage imports office.css. Someone removing that import as
//     an unused-looking tidy-up reddens nothing in vitest.
//   · a long title must not push the card into horizontal scroll, and must not
//     squeeze the head's own controls (跳到原訊息 / 已等你) out of the card.
//
// Mutants actually run against this guard when it was written:
//   1. `clamp` → `clamp={false}` in the story → clientHeight reaches
//      scrollHeight → the "content is clipped" bound reddens (2 of 3 red).
//   2. drop the `title={title}` attr in CurrentTaskTitle → the hover-full
//      assertion reddens (2 of 3 red).
// 🔴 AND ONE THAT DID **NOT** REDDEN, recorded because a guard nobody could
// make fail is worth exactly what it can be shown to catch: removing
// `min-width: 0` from `.reply-card__worker-task` left all three green, and so
// did removing `overflow-wrap: anywhere` from `.current-task-title`. The title
// shrinks regardless here (CJK wraps between characters), so assertions (3) and
// (4) below — no horizontal overflow, head controls stay inside the card — are
// NOT known to bite. They are kept as a floor against a future fixed width or
// `nowrap` on this line, not claimed as proven guards. Do not cite them as
// coverage for the two CSS properties above: those two have none.
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardWorkerTaskStory } from "./stories/ReplyCardWorkerTaskStory";
import { VERY_LONG_TITLE } from "./stories/currentTaskFixtures";

for (const width of [390, 1280]) {
  test(`width ${width}: the asker's task title clamps, keeps full text on hover, and never widens the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardWorkerTaskStory />);

    const title = cmp.getByTestId("reply-card-task-title-rc-1");
    await expect(title).toBeVisible();

    // (1) the ENTIRE title rides the native tooltip, as it does in the rail.
    await expect(title).toHaveAttribute("title", VERY_LONG_TITLE);

    // (2) the clamp is REAL: the visible box is strictly shorter than the full
    // content, i.e. the text is being clipped rather than merely being short.
    const m = await title.evaluate((el) => {
      const cs = getComputedStyle(el);
      return {
        clientHeight: (el as HTMLElement).clientHeight,
        scrollHeight: (el as HTMLElement).scrollHeight,
        lineHeight: parseFloat(cs.lineHeight),
      };
    });
    expect(
      m.clientHeight,
      "title is clipped (clamped), not merely short",
    ).toBeLessThan(m.scrollHeight);
    expect(
      m.clientHeight,
      "clamped to about two lines",
    ).toBeLessThanOrEqual(Math.ceil(m.lineHeight * 2) + 2);

    // (3) the card does not scroll sideways because of it, at either width.
    const card = cmp.getByTestId("card");
    const overflow = await card.evaluate((el) => {
      const e = el as HTMLElement;
      return e.scrollWidth - e.clientWidth;
    });
    expect(overflow, "card has no horizontal overflow").toBeLessThanOrEqual(1);

    // (4) the head's own controls are still inside the card — a long task title
    // must take height, never the space the actions live in.
    const cardBox = (await card.boundingBox())!;
    for (const sel of [".reply-card__jump", ".reply-card__waited"]) {
      const box = (await cmp.locator(sel).boundingBox())!;
      expect(
        box.x + box.width,
        `${sel} stays within the card`,
      ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
    }
  });
}

test("the task-number chip is a real, hittable control beside the title", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const cmp = await mount(<ReplyCardWorkerTaskStory />);

  const chip = cmp.getByTestId("reply-card-rc-1-task-ow-1");
  await expect(chip).toBeVisible();
  // Real layout: nothing overlaps it and no stray pointer-events:none — a
  // jsdom fireEvent.click would pass in either case.
  await chip.click({ trial: true });
});
