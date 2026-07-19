// T-3dc5 artifact set — real-browser layout guards the jsdom suite can't see:
// the 「產物 N」 badge occupies a visible box in the badge row, its popover opens
// with a laid-out three-tab header that stays inside the viewport, and the
// empty-set case renders NO badge box at all (the design's load-bearing
// negative, proven in real layout — not just DOM absence).
import { test, expect } from "@playwright/experimental-ct-react";
import {
  TaskCardArtifactsStory,
  TaskCardNoArtifactsStory,
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

test("empty set: NO 產物 badge renders (the load-bearing negative)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardNoArtifactsStory />);
  await expect(cmp.getByTestId("task-artifacts-badge")).toHaveCount(0);
});
