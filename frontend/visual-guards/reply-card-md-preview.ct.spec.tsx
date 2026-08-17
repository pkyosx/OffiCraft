// GUARD (T-7bc2) — 請示卡問題側附件的 .md 預覽:owner 2026-07-21 把預覽入口從
// 一顆獨立的「眼睛」按鈕(跟兩側檔案間距一樣、看不出歸屬)改成「檔名 chip 本身
// 就是預覽入口」(原生 <button>,取代下載 <a>)。jsdom 已經證過「.md 才會變成
// button、點了會開 overlay」的邏輯(見 ReplyCardBody.md-preview.test.tsx);這裡
// 補 jsdom 量不到的三件事:①真瀏覽器版面下按鈕真的可點(沒有被其他元素蓋住)
// ②Enter/Space 這個原生 <button> 預設鍵盤啟動行為 jsdom 不模擬③ overlay 面板
// 在窄版是否溢出。
//
// a11y:此按鈕刻意不設 aria-label——依 §5 教訓「aria-label 會取代連結的可及
// 名稱,不是附加」,可見的檔名文字本身就是可及名稱,不需要(也不該)再蓋一層。
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardMdPreviewStory } from "./stories/ReplyCardMdPreviewStory";

for (const width of [390, 1280]) {
  test(`width ${width}: every stored file chip opens the common popup`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    await expect(cmp.locator("button.chat__msg-file")).toHaveCount(2);
    await expect(cmp.locator("a.chat__msg-file")).toHaveCount(0);
  });

  test(`width ${width}: clicking the .md chip opens the overlay and renders the markdown body`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    await cmp.getByRole("button", { name: "design-proposal.md" }).click();
    const panel = page.locator(".md-preview__panel");
    await expect(panel).toBeVisible();
    await expect(
      // The overlay portals to `document.body` (T-76cd) — reach it via `page`.
      page.locator(".md-preview").getByRole("heading", { name: "design-proposal.md" })
    ).toBeVisible();
    // The panel must stay inside the viewport at THIS width — the owner's
    // named concern (narrow reply-card layout getting cramped).
    const box = await panel.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
  });

  // STOPPED (owner 2026-08-06, reply card rc-ebba69d22977, option 4 "把這兩條停掉").
  //
  // This assertion flaked twice within one hour on CI, both times on the very
  // first line: `Tab` then toBeFocused(). The proof it is the test and not the
  // product: on commit 1777030b the SAME suite ran in two jobs of the SAME
  // round — macos-host-gates went green while auto-beta's internal CI went red
  // on it. Identical code cannot be both.
  //
  // What is NO LONGER GUARDED, stated plainly because a skip is easy to read as
  // "there was never anything here": that this button is reachable by keyboard
  // at all, and that Enter/Space fire its native activation (jsdom does not
  // implement that default action, so the cheaper test layer cannot cover it).
  // The MOUSE path is still guarded by the sibling test above, which has never
  // flaked.
  //
  // The maintainer recommended narrowing the assertion instead (keep Enter/Space,
  // drop the "first Tab lands here" positional assumption); the owner ruled to
  // stop it. Re-enabling is a fresh decision, not a revert.
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(`width ${width}: the .md chip is keyboard-reachable (Tab) and Enter/Space both activate it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardMdPreviewStory />);
    const btn = cmp.getByRole("button", { name: "design-proposal.md" });

    // Tab order: [.md chip button][.pdf chip <a>] — the story mounts the .md
    // attachment FIRST, so the button is the FIRST tab stop now (no separate
    // preview button ahead of it anymore).
    await page.keyboard.press("Tab");
    await expect(btn).toBeFocused();

    await page.keyboard.press("Enter");
    await expect(page.locator(".md-preview__panel")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.locator(".md-preview__panel")).toHaveCount(0);

    await page.keyboard.press("Space");
    await expect(page.locator(".md-preview__panel")).toBeVisible();
  });
}

test("the .md chip's accessible name is the VISIBLE filename text (no aria-label override, §5 lesson)", async ({
  mount,
}) => {
  const cmp = await mount(<ReplyCardMdPreviewStory />);
  const btn = cmp.getByRole("button", { name: "design-proposal.md" });
  const ariaLabel = await btn.getAttribute("aria-label");
  expect(ariaLabel).toBeNull();
  await expect(
    cmp.getByRole("button", { name: "design-proposal.md" })
  ).toBeVisible();
});
