// GUARD (T-9ca5) — 轉派中 LOCK overlay badge.
//
// Contract: `reassigning` is no longer a status; it is an ORTHOGONAL lock. A
// reassigned card shows its honest DERIVED status badge (進行中) AND a separate
// 轉派中 lock overlay badge (task-lock / .task-badge--lock-reassigning) BESIDE
// it on the badge row — both visible, both inside the card, at every width.
//
// jsdom can't see this: it has no layout engine, so the badge row's geometry
// (do the two badges coexist on the row, does the lock badge stay inside the
// card) is structurally invisible to the vitest suite. This measures the real
// layout in real Chromium at 390 (phone) + 1280 (desktop).
//
// MUTANT (verified red→green): drop the `task.lock === "reassigning"` guard's
// badge in TaskCard → task-lock never mounts → assertion (1) goes red.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardLockStory } from "./stories/TaskCardLockStory";

for (const width of [1280, 390]) {
  test(`width ${width}: 轉派中 lock badge renders beside the derived status, inside the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<TaskCardLockStory />);

    // (1) both badges are present — the derived status AND the orthogonal lock.
    const lock = cmp.getByTestId("task-lock");
    const status = cmp.getByTestId("task-status");
    await expect(lock).toBeVisible();
    await expect(status).toBeVisible();
    await expect(lock).toHaveText("轉派中");
    // The status badge shows the honest derived status, NOT the lock word.
    await expect(status).toHaveText("進行中");

    // (2) the lock badge sits inside the card — never shoved past its right edge
    // (the overflow trap a badge-row addition can trip at 390).
    // The mounted component root IS the .task-card article — measure cmp itself.
    const cardBox = (await cmp.boundingBox())!;
    const lockBox = (await lock.boundingBox())!;
    expect(cardBox, "card box").not.toBeNull();
    expect(lockBox, "lock box").not.toBeNull();
    expect(lockBox.x).toBeGreaterThanOrEqual(cardBox.x - 1);
    expect(lockBox.x + lockBox.width).toBeLessThanOrEqual(
      cardBox.x + cardBox.width + 1
    );

    // (3) the page never scrolls sideways because of the extra badge.
    const overflow = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth
    );
    expect(
      overflow,
      `[${width}px] page must have no horizontal scroll (got +${overflow}px)`
    ).toBeLessThanOrEqual(1);
  });
}

test("1280px: lock and status badges share the badge row (beside, not stacked)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<TaskCardLockStory />);
  const lockBox = (await cmp.getByTestId("task-lock").boundingBox())!;
  const statusBox = (await cmp.getByTestId("task-status").boundingBox())!;
  // Vertical bands overlap ⇒ same row. Distinct x ⇒ two separate badges.
  const overlap =
    Math.min(lockBox.y + lockBox.height, statusBox.y + statusBox.height) -
    Math.max(lockBox.y, statusBox.y);
  expect(overlap, "lock/status share a row").toBeGreaterThan(4);
  expect(Math.abs(lockBox.x - statusBox.x)).toBeGreaterThan(4);
});
