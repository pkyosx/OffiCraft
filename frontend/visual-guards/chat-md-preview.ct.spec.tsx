// GUARD (T-7bc2) — 聊天室附件的 .md 預覽:owner 拍板把預覽入口從一顆獨立的
// 「眼睛」按鈕改成「檔名 chip 本身就是預覽入口」(原生 <button>,取代下載 <a>)。
// jsdom 已經證過 ChatArea.md-preview.test.tsx 的 click→overlay 邏輯;這裡補真
// 瀏覽器才驗得到的兩件事:①按鈕在真實版面下真的可點 ②Enter/Space 這個原生
// <button> 預設鍵盤啟動行為(review 2 指出的缺口:三處只有請示卡/任務產物有
// 真瀏覽器鍵盤測試,聊天室這處漏了)。
//
// a11y:此按鈕刻意不設 aria-label——依 §5 教訓「aria-label 會取代連結的可及
// 名稱,不是附加」,可見的檔名文字本身就是可及名稱。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatMdPreviewStory } from "./stories/ChatMdPreviewStory";

for (const width of [390, 1280]) {
  test(`width ${width}: the .md chip renders as a <button>, the .pdf chip stays an <a>`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatMdPreviewStory />);
    await expect(cmp.locator("button.chat__msg-file")).toHaveCount(1);
    await expect(cmp.locator("a.chat__msg-file")).toHaveCount(1);
  });

  test(`width ${width}: clicking the .md chip opens the overlay`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatMdPreviewStory />);
    await cmp.getByRole("button", { name: "design-proposal.md" }).click();
    const panel = cmp.locator(".md-preview__panel");
    await expect(panel).toBeVisible();
    await expect(
      cmp.getByRole("heading", { name: "design-proposal.md" })
    ).toBeVisible();
    const box = await panel.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
  });
}

// Enter-key activation only needs one width — jsdom already proved the click
// handler; this proves the element is a REAL <button> (native keyboard
// activation), which a <div onClick> mutant would silently fail.
test("narrow 390: Enter activates the .md chip (native <button> keyboard semantics)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const cmp = await mount(<ChatMdPreviewStory />);
  const chip = cmp.getByRole("button", { name: "design-proposal.md" });
  await chip.focus();
  await page.keyboard.press("Enter");
  await expect(cmp.locator(".md-preview")).toBeVisible();
});
