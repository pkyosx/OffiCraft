// T-3738 req4 guard: 設定 › 主題管理 的「新增」流程。點「新增」必須
//   * 以辦公室為底建立一份新的自訂主題(customThemes 多一筆),且
//   * 直接進 edit view 讓使用者改。
// 以真實 app CSS 掛載(theme.css 由 playwright/index.ts 載入),故新主題的
// 顏色是真的辦公室 :root 調色盤 —— 不是空殼。
import { test, expect } from "@playwright/experimental-ct-react";
import { ThemeSettingsAddStory } from "./stories/ThemeSettingsAddStory";

for (const width of [390, 1280]) {
  test(`width ${width}: 新增 creates an office-based theme and opens edit`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ThemeSettingsAddStory />);

    // Before: only the built-in office row, no custom tag.
    await expect(cmp.locator(".ts-list > .ts-row")).toHaveCount(1);
    await expect(cmp.locator(".ts-tag--custom")).toHaveCount(0);

    await cmp.getByRole("button", { name: "新增" }).click();

    // Jumped straight into edit, pre-named with the default theme name.
    const nameInput = cmp.locator("#ts-edit-name");
    await expect(nameInput).toHaveValue("新主題");

    // Seeded with the office BASE palette — many colour rows, not an empty shell.
    const colorRows = cmp.locator(".ts-color-row");
    expect(await colorRows.count()).toBeGreaterThan(5);

    // Back to the list: customThemes grew by one, and the new row wears the 自訂
    // badge (never a 用詞 badge).
    await cmp.getByRole("button", { name: "取消" }).click();
    await expect(cmp.locator(".ts-list > .ts-row")).toHaveCount(2);
    const customTags = cmp.locator(".ts-tag--custom");
    await expect(customTags).toHaveCount(1);
    await expect(cmp.locator(".ts-list > .ts-row").nth(1)).toContainText("新主題");
    await expect(cmp.locator(".ts-tag--wording")).toHaveCount(0);
  });
}
