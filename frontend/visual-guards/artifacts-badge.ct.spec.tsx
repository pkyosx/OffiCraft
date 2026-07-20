// T-3dc5 artifact set — real-browser layout guards the jsdom suite can't see:
// the 「產物 N」 badge occupies a visible box in the badge row, its popover opens
// with a laid-out three-tab header that stays inside the viewport, and the
// empty-set case renders NO badge box at all (the design's load-bearing
// negative, proven in real layout — not just DOM absence).
import { test, expect } from "@playwright/experimental-ct-react";
import {
  TaskCardArtifactsStory,
  TaskCardNoArtifactsStory,
  TaskCardRaggedArtifactsStory,
} from "./stories/TaskCardArtifactsStory";
import { TaskArtifactsRightEdgeStory } from "./stories/TaskArtifactsRightEdgeStory";

test("desktop 1024: 產物 badge is visible and opens a laid-out tabbed popover", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);

  const badge = cmp.getByTestId("task-artifacts-badge");
  await expect(badge).toBeVisible();
  const badgeBox = await badge.boundingBox();
  expect(badgeBox, "badge must have a layout box").not.toBeNull();
  // Not collapsed: the count badge is a real, tappable chip.
  expect(badgeBox!.height).toBeGreaterThanOrEqual(16);
  expect(badgeBox!.width).toBeGreaterThan(24);
  await expect(badge).toContainText("3");

  // Open the popover — its three tabs must lay out (not overlap/collapse) and the
  // panel must stay inside the viewport width.
  await badge.click();
  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();
  const tabs = cmp.locator(".task-artifacts__tab");
  await expect(tabs).toHaveCount(3);
  const popBox = await popover.boundingBox();
  expect(popBox).not.toBeNull();
  expect(popBox!.height).toBeGreaterThan(60);
  expect(popBox!.x).toBeGreaterThanOrEqual(0);
  expect(popBox!.x + popBox!.width).toBeLessThanOrEqual(1024 + 1);

  // The default 檔案 tab shows the .md file row with its 預覽 action.
  await expect(cmp.getByText("design.md")).toBeVisible();
  await expect(cmp.locator('.task-artifacts [aria-label="預覽"]')).toBeVisible();
});

test("narrow 390: popover stays within the phone viewport", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();
  const box = await popover.boundingBox();
  expect(box).not.toBeNull();
  // width min(340px, 78vw) — must not spill past the 390px screen.
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390 + 1);
});

test("narrow 390: popover stays in-viewport even when its badge is pinned to the right edge (T-2ca0)", async ({ mount, page }) => {
  // The reported bug: 產物 is the rightmost badge, so on a phone its anchor sits
  // far right; the old absolute/left:0 + fixed-width popover then spilled past
  // the right edge and clipped the 連結 tab. This story forces that anchor
  // position, which the pre-existing 390 guard never did (its badge wrapped to
  // the left). Assert the panel — AND its rightmost 連結 tab — stay in-viewport.
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<TaskArtifactsRightEdgeStory />);
  await cmp.getByTestId("task-artifacts-badge").click();

  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();
  const box = await popover.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390 + 1);

  // The rightmost tab (連結) must be reachable, not clipped off-screen.
  const linksTab = cmp.locator(".task-artifacts__tab").last();
  await expect(linksTab).toBeVisible();
  const tabBox = await linksTab.boundingBox();
  expect(tabBox).not.toBeNull();
  expect(tabBox!.x + tabBox!.width).toBeLessThanOrEqual(390 + 1);
});

// T-90df — the alignment guard. This is the ONLY layer that can prove the
// owner's requirement: jsdom computes no layout, so the vitest suite can assert
// the markup and the title= but never that the chips came out equal width or
// that the buttons line up. Reverting the CSS (chip back to sizing with its
// text) reddens this and nothing else.
test("desktop 1024: a short and an overlong name give EQUAL chip widths and one action column", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardRaggedArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  // 檔案 tab (default): a.md and the overlong name.
  const chips = cmp.locator(".task-artifacts__item .task-artifacts__chip");
  await expect(chips).toHaveCount(2);
  const chipBoxes = await chips.evaluateAll((els) =>
    els.map((el) => el.getBoundingClientRect().width),
  );
  // Chip width must be name-INDEPENDENT: the short and long rows agree.
  expect(Math.abs(chipBoxes[0] - chipBoxes[1])).toBeLessThanOrEqual(1);

  // …and every name stays inside its chip (the ellipsis did its job) rather
  // than pushing the row wider than the panel.
  const panel = (await cmp.locator(".task-artifacts").boundingBox())!;
  const rowRights = await cmp
    .locator(".task-artifacts__item")
    .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().right));
  for (const right of rowRights) {
    expect(right).toBeLessThanOrEqual(panel.x + panel.width + 1);
  }

  // The trailing action columns start at the SAME x — that is 「動作鈕垂直
  // 對齊成一欄」 in measurable form.
  const actionLefts = await cmp
    .locator(".task-artifacts__item .task-artifacts__actions")
    .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().left));
  expect(actionLefts.length).toBe(2);
  expect(Math.abs(actionLefts[0] - actionLefts[1])).toBeLessThanOrEqual(1);
});

test("desktop 1024: all three tabs put their action column on the same right edge", async ({ mount, page }) => {
  // Cross-tab consistency (requirement ①). Chip widths differ by the 44px
  // thumbnail on an image row — what must agree is the RIGHT edge the action
  // buttons flush to, since that is what the eye reads as 「對齊」.
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardRaggedArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  const rights: number[] = [];
  for (const tabName of [/檔案/, /圖片/, /連結/]) {
    await cmp.getByRole("tab", { name: tabName }).click();
    const actions = cmp.locator(".task-artifacts__item .task-artifacts__actions");
    await expect(actions.first()).toBeVisible();
    const right = await actions
      .first()
      .evaluate((el) => el.getBoundingClientRect().right);
    rights.push(right);
    // Every kind renders a chip whose full name is on title= (requirement ②).
    const chip = cmp.locator(".task-artifacts__item .task-artifacts__chip").first();
    await expect(chip).toHaveAttribute("title", /.+/);
  }
  expect(Math.abs(rights[0] - rights[1])).toBeLessThanOrEqual(1);
  expect(Math.abs(rights[0] - rights[2])).toBeLessThanOrEqual(1);
});

test("empty set: NO 產物 badge renders (the load-bearing negative)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardNoArtifactsStory />);
  await expect(cmp.getByTestId("task-artifacts-badge")).toHaveCount(0);
});
