// T-a1c4 .md preview overlay — real-browser layout the jsdom suite can't see:
// the centered modal panel lays out inside the viewport, the markdown BODY
// renders (a real heading box, proving Markdown.tsx ran — not the raw-source
// tab), and the 下載 action sits in the header as a separate affordance.
import { test, expect } from "@playwright/experimental-ct-react";
import { MarkdownPreviewStory } from "./stories/MarkdownPreviewStory";

test("desktop 1024: overlay panel lays out with a rendered markdown body", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<MarkdownPreviewStory />);

  const panel = cmp.locator(".md-preview__panel");
  await expect(panel).toBeVisible();
  const box = await panel.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.height).toBeGreaterThan(80);
  // Panel stays within the viewport (max-width min(760, 100%)).
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(1024 + 1);

  // The markdown BODY rendered as real elements (heading present) — the whole
  // point of the in-cockpit preview vs the browser's raw-source tab.
  await expect(cmp.getByRole("heading", { name: "產物顯示架構設計" })).toBeVisible();

  // Preview and download are two actions: the header keeps a 下載 link.
  const dl = cmp.locator(".md-preview__download");
  await expect(dl).toBeVisible();
});
