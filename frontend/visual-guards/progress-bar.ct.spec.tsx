// HOTSPOT 1 — 任務卡進度條 (T-ad21 / owner 2026-07-17「進度條怎麼不見了？」).
//
// The jsdom suite (TaskCard.progress-bar.test.tsx) pins `fill.style.width ===
// "40%"` — the INLINE style React writes — and querySelector existence. It
// never touches the bar's rendered HEIGHT, which lives only in tasks.css. So a
// `height:5px→0` mutant ships green there. These assertions measure the REAL
// box in a REAL browser: the bar must have a visible height, at desktop AND
// under the ≤720px media branch (where a flex override once collapsed it to 0).
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardProgressStory } from "./stories/TaskCardProgressStory";

const MIN_BAR_HEIGHT = 4; // css says 5px; tolerance absorbs sub-pixel rounding.

test("desktop 1024: progress bar has a visible height and ~40% fill", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardProgressStory />);
  const bar = cmp.locator(".task-card__progress-bar");
  const fill = cmp.locator(".task-card__progress-fill");
  await expect(bar).toBeVisible();
  const barBox = await bar.boundingBox();
  const fillBox = await fill.boundingBox();
  expect(barBox, "bar must have a layout box").not.toBeNull();
  expect(fillBox, "fill must have a layout box").not.toBeNull();
  // INVARIANT 1 — the track is not collapsed (kills height:5px→0).
  expect(barBox!.height).toBeGreaterThanOrEqual(MIN_BAR_HEIGHT);
  // INVARIANT 2 — the fill occupies ~40% of the track's real width (not just an
  // inline style string): 2/5 with the 3 superseded nodes excluded.
  const ratio = fillBox!.width / barBox!.width;
  expect(ratio).toBeGreaterThan(0.36);
  expect(ratio).toBeLessThan(0.44);
});

test("narrow 700 (<=720 media branch): bar STILL has a visible height", async ({ mount, page }) => {
  await page.setViewportSize({ width: 700, height: 800 });
  const cmp = await mount(<TaskCardProgressStory />);
  const bar = cmp.locator(".task-card__progress-bar");
  await expect(bar).toBeVisible();
  const barBox = await bar.boundingBox();
  // INVARIANT 3 — the ≤720px flex-override regression (column flex-basis eating
  // the height). This is the exact bug owner hit on their phone.
  expect(barBox!.height).toBeGreaterThanOrEqual(MIN_BAR_HEIGHT);
});
