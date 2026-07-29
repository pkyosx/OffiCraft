// T-cd6f real-browser guard for the persistent theme-avatar index editor.
// It mounts the production WorkerDetailPanel at the required mobile, tablet,
// and desktop widths, exercises keyboard commit, and records evidence.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { WorkerDetailPanelTaskOrderStory } from "./stories/WorkerDetailPanelTaskOrderStory";

const SHOT_DIR =
  process.env.AVATAR_INDEX_SHOT_DIR ?? "test-results/avatar-index-editor";

async function expectNoHorizontalOverflow(page: Page, width: number) {
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);

  const identity = page.locator(".mp-identity");
  const box = await identity.boundingBox();
  expect(box, "worker identity card box").not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
}

for (const width of [390, 768, 1280]) {
  test(`width ${width}: avatar index is keyboard editable without overflow`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<WorkerDetailPanelTaskOrderStory />);
    const input = cmp.getByRole("spinbutton", { name: "頭像索引" });

    await expect(input).toHaveValue("14");
    await input.focus();
    await input.fill("27");
    await expect(
      cmp.getByRole("button", { name: "儲存頭像索引" }),
    ).toBeEnabled();
    await page.keyboard.press("Enter");
    await expect(input).toHaveValue("27");

    await expectNoHorizontalOverflow(page, width);
    await page.screenshot({
      path: `${SHOT_DIR}/avatar-index-editor-${width}.png`,
      fullPage: true,
    });
  });
}
