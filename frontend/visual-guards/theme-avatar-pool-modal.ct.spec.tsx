import { expect, test } from "@playwright/experimental-ct-react";
import { ThemeAvatarPoolModalStory } from "./stories/ThemeAvatarPoolModalStory";

for (const [width, expectedColumns] of [
  [390, 2],
  [768, 4],
  [1280, 4],
] as const) {
  test(`width ${width}: populated avatar grid wraps into ${expectedColumns} columns without overflow`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<ThemeAvatarPoolModalStory />);
    const dialog = page.getByRole("dialog", { name: "正職頭像" });
    const grid = dialog.locator(".ts-avatar-grid");

    await expect(grid.locator(".ts-avatar-grid__item")).toHaveCount(7);
    await expect(grid.getByRole("button", { name: "新增圖片" })).toBeVisible();

    const columns = await grid.evaluate((element) =>
      getComputedStyle(element).gridTemplateColumns.split(" ").filter(Boolean),
    );
    expect(columns).toHaveLength(expectedColumns);

    const geometry = await grid.evaluate((element) => {
      const viewportWidth = document.documentElement.clientWidth;
      const cards = Array.from(
        element.querySelectorAll<HTMLElement>(".ts-avatar-grid__item"),
      ).map((card) => {
        const rect = card.getBoundingClientRect();
        return { left: rect.left, right: rect.right };
      });
      return {
        viewportWidth,
        pageOverflow:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
        gridOverflow: element.scrollWidth - element.clientWidth,
        cards,
      };
    });

    expect(geometry.pageOverflow).toBeLessThanOrEqual(1);
    expect(geometry.gridOverflow).toBeLessThanOrEqual(1);
    for (const card of geometry.cards) {
      expect(card.left).toBeGreaterThanOrEqual(-1);
      expect(card.right).toBeLessThanOrEqual(geometry.viewportWidth + 1);
    }
  });
}
