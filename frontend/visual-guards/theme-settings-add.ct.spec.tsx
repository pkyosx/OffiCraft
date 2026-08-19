// T-3738 req4 guard: 設定 › 主題管理 的「新增」流程。點「新增」必須
//   * 以辦公室為底建立一份新的自訂主題(主題清單多一筆),且
//   * 直接進 edit view 讓使用者改。
// T-83ef 之後「新增」是一次真的寫入(PUT /api/themes/{id}),伺服器收下之後
// 才會進編輯器 —— 所以清單長出那一列與編輯器打開都不再是同一個 tick 的事。
// 這裡的 expect 全部走 Playwright 的 auto-retry,不要改成即時快照。
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

    // Before: only the built-in office row, and no 自訂 group at all.
    await expect(cmp.locator(".ts-list > .ts-row")).toHaveCount(1);
    await expect(cmp.getByTestId("ts-group-custom")).toHaveCount(0);

    await cmp.getByRole("button", { name: "新增" }).click();

    // Jumped straight into edit, pre-named with the default theme name.
    const nameInput = cmp.locator("#ts-edit-name");
    await expect(nameInput).toHaveValue("新主題");

    // Seeded with the office BASE palette — many colour rows, not an empty shell.
    const colorRows = cmp.locator(".ts-color-row");
    expect(await colorRows.count()).toBeGreaterThan(5);

    // Back to the list: the theme list grew by one, and the new row lands in
    // the 自訂 group (rows carry no badge of their own).
    await cmp.getByRole("button", { name: "取消" }).click();
    await expect(cmp.locator(".ts-list > .ts-row")).toHaveCount(2);
    const customGroup = cmp.locator(".ts-list:has(#ts-group-custom)");
    await expect(customGroup.locator(".ts-row")).toHaveCount(1);
    await expect(customGroup.locator(".ts-row")).toContainText("新主題");
    await expect(cmp.locator(".ts-tag")).toHaveCount(0);
  });
}
