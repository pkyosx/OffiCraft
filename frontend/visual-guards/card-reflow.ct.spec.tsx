// HOTSPOT 2 — 卡片折行 (T-a20b always-stack 等待中原因).
//
// Contract (owner 2026-07-17 rc-91492e026e87「為什麼等待中不是都自己一行?」):
// the 等待中 label sits on line 1 and the reason renders on its OWN line below
// it, at EVERY viewport width — driven by `.task-card__waiting-md { flex: 1 1
// 100% }` inside a `flex-wrap: wrap` row. The fixture reason is 3 chars, which
// trivially fits beside the label; so if it is on line 2, the CSS put it there.
//
// tasks.css:724 says outright that this was measured with Playwright +
// getBoundingClientRect against the real app, and that jsdom "silently measures
// flex-basis: auto and reports the UNFIXED layout". This spec is that
// measurement, made permanent. The A/B control the comment documents (delete
// `flex: 1 1 100%` → the short reason goes INLINE) is exactly the mutant in §5.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardWaitingStory } from "./stories/TaskCardWaitingStory";

async function measure(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardWaitingStory />);
  const row = cmp.getByTestId("task-waiting-reason");
  await expect(row).toBeVisible();
  const label = row.locator(".task-card__waiting-label");
  const md = row.locator(".task-card__waiting-md");
  const labelBox = await label.boundingBox();
  const mdBox = await md.boundingBox();
  expect(labelBox, "label box").not.toBeNull();
  expect(mdBox, "reason md box").not.toBeNull();
  return { labelBox: labelBox!, mdBox: mdBox! };
}

for (const width of [1280, 390]) {
  test(`width ${width}: 等待中 label and reason are STACKED, not inline`, async ({ mount, page }) => {
    const { labelBox, mdBox } = await measure(mount, page, width);
    // STACKED test A — the reason's top starts below the label's top by a real
    // line's worth (the comment measured +26.75px; floor at 8px kills "inline,
    // same baseline" without pinning an exact pixel that scroll/chrome shift).
    expect(mdBox.y - labelBox.y).toBeGreaterThan(8);
    // STACKED test B — the reason begins on a NEW flex line, so its left edge is
    // at/left-of the label's, never indented to the right of it as a wrapped
    // inline continuation would be. This is what distinguishes "own line" from
    // "text-wrapped inside the shared line".
    expect(mdBox.x).toBeLessThanOrEqual(labelBox.x + 1);
  });
}
