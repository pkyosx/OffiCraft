// GUARD (T-7bc2) — 請示卡問題側附件的 .md 預覽:入口存在性、可及名稱、鍵盤可達、
// 排版(390 手機寬 + 1280 桌面寬)。jsdom 已經證過「.md 才有按鈕、點了會開
// overlay」的邏輯(見 ReplyCardBody.md-preview.test.tsx);這裡補 jsdom 量不到
// 的三件事:①真瀏覽器版面下按鈕真的可點(沒有被其他元素蓋住)②Enter/Space
// 這個原生 <button> 預設鍵盤啟動行為 jsdom 不模擬③ overlay 面板在窄版是否溢出。
//
// MUTANT 前提(依 T-a706 的教訓 —— getByRole name 可能被 title fallback 混
// 過):下面的 aria-label 斷言直接讀 attribute,不只靠 getByRole name。
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardMdPreviewStory } from "./stories/ReplyCardMdPreviewStory";
import { zh } from "../src/i18n/locales/zh";

for (const width of [390, 1280]) {
  test(`width ${width}: .md attachment shows a 預覽 button, .pdf does not`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    await expect(cmp.locator(".reply-card__preview-btn")).toHaveCount(1);
  });

  test(`width ${width}: clicking 預覽 opens the overlay and renders the markdown body`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    await cmp.locator(".reply-card__preview-btn").click();
    const panel = cmp.locator(".md-preview__panel");
    await expect(panel).toBeVisible();
    await expect(
      cmp.getByRole("heading", { name: "design-proposal.md" })
    ).toBeVisible();
    // The panel must stay inside the viewport at THIS width — the owner's
    // named concern (narrow reply-card layout getting cramped).
    const box = await panel.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
  });

  test(`width ${width}: the preview button is keyboard-reachable (Tab) and Enter/Space both activate it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    const btn = cmp.locator(".reply-card__preview-btn");

    // Tab order: [.md download chip][.md 預覽 button][.pdf download chip] —
    // the story mounts a .md attachment BEFORE the .pdf one, and the download
    // chip <a> is itself tabbable, so the preview button is the SECOND stop.
    await page.keyboard.press("Tab");
    await page.keyboard.press("Tab");
    await expect(btn).toBeFocused();

    await page.keyboard.press("Enter");
    await expect(cmp.locator(".md-preview__panel")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(cmp.locator(".md-preview__panel")).toHaveCount(0);

    await page.keyboard.press("Space");
    await expect(cmp.locator(".md-preview__panel")).toBeVisible();
  });
}

test("the preview button carries an aria-label (checked as a raw attribute, not just getByRole name — T-a706)", async ({
  mount,
}) => {
  const cmp = await mount(<ReplyCardMdPreviewStory />);
  const ariaLabel = await cmp
    .locator(".reply-card__preview-btn")
    .getAttribute("aria-label");
  expect(ariaLabel).toBe(zh.chat.mdPreview.action);
  // Independent confirmation the real accessibility tree resolves the SAME
  // name.
  await expect(
    cmp.getByRole("button", { name: zh.chat.mdPreview.action })
  ).toBeVisible();
});
