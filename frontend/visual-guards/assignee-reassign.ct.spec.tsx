// HOTSPOT 3 — 負責人 row + 轉派 icon reflow (T-160e, owner 2026-07-18).
//
// Contract: the 轉派 icon button sits to the RIGHT of the 負責人 name and shares
// its grid value cell (.task-card__assignee-cell, inline-flex). A long assignee
// name must ellipse (the chip shrinks) so the icon stays on the same row and
// stays fully inside the card — it must NEVER be clipped or pushed to a new
// line. The failure mode this pins is the flex:1 trap tasks.css calls out:
// giving the chip flex:1 makes it grow to the full row and shove the flex:none
// icon off the edge. jsdom cannot see this (it measures the unfixed layout), so
// the check lives here as a real-browser boundingBox measurement.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardAssigneeStory } from "./stories/TaskCardAssigneeStory";

for (const width of [1280, 390]) {
  test(`width ${width}: 負責人 name ellipses so the 轉派 icon stays on-row and un-clipped`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<TaskCardAssigneeStory />);

    // task-card is the mounted ROOT (the article itself), so scope it from the
    // page — cmp.getByTestId only searches descendants and would miss the root.
    const card = page.getByTestId("task-card");
    const chip = cmp.getByTestId("task-assignee-link");
    const icon = cmp.getByTestId("task-reassign");
    await expect(icon).toBeVisible();

    const cardBox = await card.boundingBox();
    const chipBox = await chip.boundingBox();
    const iconBox = await icon.boundingBox();
    expect(cardBox, "card box").not.toBeNull();
    expect(chipBox, "assignee chip box").not.toBeNull();
    expect(iconBox, "reassign icon box").not.toBeNull();

    // 1. The icon is fully inside the card's right edge — not clipped, not
    //    overflowing (the flex:1-trap regression pushes it past this edge).
    expect(iconBox!.x + iconBox!.width).toBeLessThanOrEqual(
      cardBox!.x + cardBox!.width + 1
    );
    // 2. The name chip itself stays inside the card — a long name ellipses
    //    rather than overflowing the card to make room.
    expect(chipBox!.x + chipBox!.width).toBeLessThanOrEqual(
      cardBox!.x + cardBox!.width + 1
    );
    // 3. The icon sits to the RIGHT of the name chip (owner's placement).
    expect(iconBox!.x).toBeGreaterThanOrEqual(chipBox!.x + chipBox!.width - 1);
    // 4. The icon shares the chip's ROW — vertical overlap proves it did not
    //    wrap onto a second line when the name got long.
    const sharesRow =
      iconBox!.y < chipBox!.y + chipBox!.height &&
      chipBox!.y < iconBox!.y + iconBox!.height;
    expect(sharesRow, "icon shares the 負責人 row").toBe(true);
  });
}
