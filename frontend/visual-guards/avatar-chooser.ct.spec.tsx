// T-cd6f real-browser guard for the persistent theme-avatar chooser.
// It mounts the production WorkerDetailPanel at the required mobile, tablet,
// and desktop widths, proves the row stays COMPACT until the owner asks for
// the pool, exercises keyboard selection, and records evidence.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { WorkerDetailPanelTaskOrderStory } from "./stories/WorkerDetailPanelTaskOrderStory";

const SHOT_DIR =
  process.env.AVATAR_CHOOSER_SHOT_DIR ?? "test-results/avatar-chooser";

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
  test(`width ${width}: the row stays compact and the pool opens on request`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<WorkerDetailPanelTaskOrderStory />);

    // 🔴 COMPACT BY DEFAULT. The pool must not be laid out inline: the owner
    // ran that shape in a trial and it stretched every roster row until the
    // member list was unreadable (owner 2026-08-12). This assertion is the
    // guard against reintroducing it.
    await expect(cmp.getByRole("radio")).toHaveCount(0);
    await expect(cmp.getByRole("spinbutton")).toHaveCount(0);
    const rowHeight = (await cmp.locator(".avatar-chooser").boundingBox())!.height;
    expect(rowHeight).toBeLessThanOrEqual(120);

    await cmp.getByRole("button", { name: "選擇圖像" }).click();
    const choices = cmp.getByRole("radiogroup", { name: "選擇頭像" });
    const radios = choices.getByRole("radio");
    expect(await radios.count()).toBeGreaterThan(1);
    await expect(radios.first()).toHaveAttribute("aria-checked", "true");

    // Keyboard reach is part of the contract, not an extra.
    await radios.nth(1).focus();
    await page.keyboard.press("Space");
    await expect(
      cmp.getByRole("button", { name: "選擇圖像" }),
    ).toBeFocused();
    await cmp.getByRole("button", { name: "選擇圖像" }).click();
    await expect(choices.getByRole("radio").nth(1)).toHaveAttribute(
      "aria-checked",
      "true",
    );

    await expectNoHorizontalOverflow(page, width);
    await page.screenshot({
      path: `${SHOT_DIR}/avatar-chooser-${width}.png`,
      fullPage: true,
    });
  });
}
