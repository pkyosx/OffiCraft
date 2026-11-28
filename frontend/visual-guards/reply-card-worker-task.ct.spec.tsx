// HOTSPOT — T-196: the asking outsource worker's current-task line, the THIRD
// surface of `CurrentTaskTitle` (after the office rail row and the worker chat
// header), under a reply card's 開卡人 chip.
//
// jsdom resolves no -webkit-line-clamp box, no ellipsis and no layout, so every
// property below is invisible to the unit tests that pin the line's CONTENT:
//   · wider than a phone the title clamps to two lines HERE too — the rail's
//     rule reaches this surface only because RepliesPage imports office.css.
//     Someone removing that import as an unused-looking tidy-up reddens
//     nothing in vitest.
//   · on a phone (≤720px) the card cuts it to one line ending in 「…」
//     (replies.css).
//   · a long title must not push the card into horizontal scroll, and must not
//     push the head's own controls (跳到原訊息 / 已等你) out of the card.
//
// Mutants run against this guard on the current code:
//   1. `clamp` → `clamp={false}` in the story → the 1280 "clipped (clamped)"
//      bound reddens (a phone head cuts the title to one line either way).
//   2. drop the `title={title}` attr in CurrentTaskTitle → the hover-full
//      assertion reddens at 390 and 1280.
//   3. drop `text-overflow: ellipsis` from the phone rule in replies.css → "the
//      cut end is drawn as 「…」" reddens at 390.
// 🔴 AND TWO THAT DID **NOT** REDDEN, recorded because a guard nobody could
// make fail is worth exactly what it can be shown to catch: removing
// `min-width: 0` from `.reply-card__worker-task`, and removing
// `overflow-wrap: anywhere` from `.current-task-title`, left this file green.
// Assertions (3) and (4) below — no horizontal overflow, head controls stay
// inside the card — are NOT known to bite; they are kept as a floor, not
// claimed as coverage for those two properties.
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardWorkerTaskStory } from "./stories/ReplyCardWorkerTaskStory";
import { VERY_LONG_TITLE } from "./stories/currentTaskFixtures";

for (const width of [390, 1280]) {
  test(`width ${width}: the asker's task title ${width <= 720 ? "keeps to one line cut with an ellipsis" : "clamps"}, keeps full text on hover, and never widens the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardWorkerTaskStory />);

    const title = cmp.getByTestId("reply-card-task-title-rc-1");
    await expect(title).toBeVisible();

    // (1) the ENTIRE title rides the native tooltip, as it does in the rail.
    await expect(title).toHaveAttribute("title", VERY_LONG_TITLE);

    // (2) the text is really cut, not merely short: on a phone one line whose
    // end is drawn as 「…」 (the same box with the ellipsis turned off paints
    // differently), wider than that two clamped lines.
    const m = await title.evaluate((el) => {
      const e = el as HTMLElement;
      return {
        clientHeight: e.clientHeight,
        scrollHeight: e.scrollHeight,
        clientWidth: e.clientWidth,
        scrollWidth: e.scrollWidth,
        lineHeight: parseFloat(getComputedStyle(el).lineHeight),
      };
    });
    if (width <= 720) {
      expect(m.clientWidth, "title is cut, not merely short").toBeLessThan(m.scrollWidth);
      expect(m.clientHeight, "one line").toBeLessThanOrEqual(Math.ceil(m.lineHeight) + 1);
      const drawn = await title.screenshot();
      await title.evaluate((el) => ((el as HTMLElement).style.textOverflow = "clip"));
      const clipped = await title.screenshot();
      await title.evaluate((el) => ((el as HTMLElement).style.textOverflow = ""));
      expect(drawn.equals(clipped), "the cut end is drawn as 「…」").toBe(false);
    } else {
      expect(
        m.clientHeight,
        "title is clipped (clamped), not merely short",
      ).toBeLessThan(m.scrollHeight);
      expect(
        m.clientHeight,
        "clamped to about two lines",
      ).toBeLessThanOrEqual(Math.ceil(m.lineHeight * 2) + 2);
    }

    // (3) the card does not scroll sideways because of it, at either width.
    const card = cmp.getByTestId("card");
    const overflow = await card.evaluate((el) => {
      const e = el as HTMLElement;
      return e.scrollWidth - e.clientWidth;
    });
    expect(overflow, "card has no horizontal overflow").toBeLessThanOrEqual(1);

    // (4) the head's own controls are still inside the card — a long task title
    // never takes the space the actions live in.
    const cardBox = (await card.boundingBox())!;
    for (const [what, control] of [
      ["跳到原訊息", cmp.getByRole("button", { name: "跳到原訊息", exact: true })],
      ["the waited stamp", cmp.locator(".reply-card__waited")],
    ] as const) {
      const box = (await control.boundingBox())!;
      expect(
        box.x + box.width,
        `${what} stays within the card`,
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
