// HOTSPOT — T-94c1 離線喚醒列 + 全寬 composer (owner mockup, 2026-07-17).
//
// Feature: while a member is offline/stopped the composer is unlocked (you can
// type; the message queues) and a wake row (queue notice + ⚡喚醒 button) sits
// ABOVE the input. The owner's mockup deliberately puts the wake button on its
// OWN row so the composer input stays FULL-WIDTH — an earlier prototype that put
// the button INSIDE the input row crushed the textarea to ~130px on a phone.
//
// jsdom cannot reach this (no layout, @media never evaluates, flex widths never
// resolve), so the "input stays wide" invariant needs a real-browser CT guard.
//
// MUTANT (§5): move the ⚡喚醒 button from `.chat__wake-row` into
// `.chat__composer-row` (regress to the crushed-input prototype) → the input
// width at 390 drops below the threshold and assertion (3) goes red. Verified
// red→green in scratchpad o22-94c1-review notes.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatWakeRowStory } from "./stories/ChatWakeRowStory";

test("390px: wake row + full-width composer, no overflow, input not crushed", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mount(<ChatWakeRowStory />);

  const wakeRow = cmp.getByTestId("wake-row");
  const wakeBtn = cmp.getByTestId("wake-btn");
  const input = cmp.getByTestId("composer-input");
  await expect(wakeRow).toBeVisible();
  await expect(wakeBtn).toBeVisible();
  await expect(input).toBeVisible();

  // (1) page-level invariant: nothing widens the document past the viewport.
  const pageOverflow = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(pageOverflow, "page has no horizontal overflow").toBeLessThanOrEqual(1);

  // (2) the wake row itself does not overflow horizontally (hint wraps instead).
  const wakeRowOverflow = await wakeRow.evaluate(
    (el) => el.scrollWidth - el.clientWidth
  );
  expect(
    wakeRowOverflow,
    "wake row must not scroll sideways"
  ).toBeLessThanOrEqual(1);

  // (3) CORE red→green: the composer input stays WIDE. With the wake button on
  // its own row the input keeps ~250px at 390; the crushed-input regression
  // (button back inside the composer row) drops it well under 200.
  const inputWidth = await input.evaluate(
    (el) => el.getBoundingClientRect().width
  );
  expect(
    inputWidth,
    `composer input must stay full-width (got ${inputWidth}px)`
  ).toBeGreaterThan(200);

  // (4) the wake button is a real, uncrushed target (it carries icon + label).
  const btnWidth = await wakeBtn.evaluate(
    (el) => el.getBoundingClientRect().width
  );
  expect(btnWidth, "wake button is not crushed").toBeGreaterThan(48);

  // (5) the button lives in the wake row, NOT the composer row (layout contract
  // — this is what keeps the input full-width).
  const btnInWakeRow = await wakeBtn.evaluate((el) =>
    el.closest(".chat__wake-row") !== null &&
    el.closest(".chat__composer-row") === null
  );
  expect(btnInWakeRow, "wake button must sit in the wake row").toBe(true);
});
